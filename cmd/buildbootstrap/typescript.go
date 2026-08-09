package main

import (
	"fmt"
	"path/filepath"
	"strings"

	esbuild "github.com/evanw/esbuild/pkg/api"
	gotreesitter "github.com/odvcencio/gotreesitter"
	"github.com/odvcencio/gotreesitter/grammars"
)

// sourceLanguage describes the syntax accepted by one concatenated output.
// Existing all-JavaScript outputs keep LoaderJS. One typed source promotes the
// output to TypeScript, which also accepts the JavaScript sources beside it.
type sourceLanguage uint8

const (
	sourceJavaScript sourceLanguage = iota
	sourceTypeScript
	sourceTSX
)

func languageForSource(src source) (sourceLanguage, error) {
	switch strings.ToLower(filepath.Ext(src.rel)) {
	case ".js", ".mjs", ".cjs":
		return sourceJavaScript, nil
	case ".ts", ".mts", ".cts":
		return sourceTypeScript, nil
	case ".tsx":
		return sourceTSX, nil
	default:
		return sourceJavaScript, fmt.Errorf("source %s has an unsupported browser source extension", src.rel)
	}
}

func languageForOutput(entry output) (sourceLanguage, error) {
	language := sourceJavaScript
	var typeScriptSources []string
	var tsxSources []string
	for _, src := range entry.sources {
		next, err := languageForSource(src)
		if err != nil {
			return sourceJavaScript, err
		}
		switch next {
		case sourceTypeScript:
			typeScriptSources = append(typeScriptSources, src.rel)
		case sourceTSX:
			tsxSources = append(tsxSources, src.rel)
		}
		if next > language {
			language = next
		}
	}
	if len(typeScriptSources) > 0 && len(tsxSources) > 0 {
		return sourceJavaScript, fmt.Errorf(
			"chunk %s mixes TypeScript and TSX sources (%s with %s); split the chunk or use one typed source family",
			entry.name,
			strings.Join(typeScriptSources, ", "),
			strings.Join(tsxSources, ", "),
		)
	}
	return language, nil
}

func (language sourceLanguage) esbuildLoader() esbuild.Loader {
	switch language {
	case sourceTypeScript:
		return esbuild.LoaderTS
	case sourceTSX:
		return esbuild.LoaderTSX
	default:
		return esbuild.LoaderJS
	}
}

func esbuildLoaderForOutput(entry output) (esbuild.Loader, error) {
	language, err := languageForOutput(entry)
	if err != nil {
		return esbuild.LoaderNone, err
	}
	return language.esbuildLoader(), nil
}

// validateTypedSource keeps TypeScript syntax diagnostics tied to the original
// file. Validation happens before concatenation and compaction can erase the
// file boundary or move the reported range.
func validateTypedSource(src source, body []byte) error {
	language, err := languageForSource(src)
	if err != nil {
		return err
	}
	if language == sourceJavaScript {
		return nil
	}

	var grammar *gotreesitter.Language
	if language == sourceTSX {
		grammar = grammars.TsxLanguage()
	} else {
		grammar = grammars.TypescriptLanguage()
	}
	if grammar == nil {
		return fmt.Errorf("parse %s: gotreesitter TypeScript grammar is unavailable", src.rel)
	}

	tree, err := gotreesitter.NewParser(grammar).Parse(body)
	if err != nil {
		return fmt.Errorf("parse %s: %w", src.rel, err)
	}
	root := tree.RootNode()
	if root == nil {
		return fmt.Errorf("parse %s: gotreesitter returned no syntax tree", src.rel)
	}
	if !root.IsError() && !root.HasError() {
		return validateClassicScriptBoundary(src, grammar, root)
	}

	bad := firstSyntaxError(root)
	if bad == nil {
		bad = root
	}
	start := bad.StartPoint()
	end := bad.EndPoint()
	kind := bad.Type(grammar)
	if bad.IsMissing() {
		kind = "missing " + kind
	}
	return fmt.Errorf(
		"parse %s:%d:%d-%d:%d: TypeScript syntax error (%s)",
		src.rel,
		start.Row+1,
		start.Column+1,
		end.Row+1,
		end.Column+1,
		kind,
	)
}

func validateClassicScriptBoundary(src source, grammar *gotreesitter.Language, root *gotreesitter.Node) error {
	module := firstModuleSyntax(root, grammar)
	if module == nil {
		return nil
	}
	start := module.StartPoint()
	return fmt.Errorf(
		"parse %s:%d:%d: module syntax %q is not allowed in buildbootstrap runtime sources; use classic-script declarations",
		src.rel,
		start.Row+1,
		start.Column+1,
		module.Type(grammar),
	)
}

func firstModuleSyntax(node *gotreesitter.Node, grammar *gotreesitter.Language) *gotreesitter.Node {
	if node == nil {
		return nil
	}
	switch node.Type(grammar) {
	case "import_statement", "export_statement", "import_alias":
		return node
	}
	for index := 0; index < node.NamedChildCount(); index++ {
		if bad := firstModuleSyntax(node.NamedChild(index), grammar); bad != nil {
			return bad
		}
	}
	return nil
}

func firstSyntaxError(node *gotreesitter.Node) *gotreesitter.Node {
	if node == nil {
		return nil
	}
	if node.IsError() || node.IsMissing() {
		return node
	}
	for index := 0; index < node.ChildCount(); index++ {
		child := node.Child(index)
		if child == nil || (!child.IsError() && !child.IsMissing() && !child.HasError()) {
			continue
		}
		if bad := firstSyntaxError(child); bad != nil {
			return bad
		}
	}
	if node.HasError() {
		return node
	}
	return nil
}

// transpileTypedChunk erases types before JavaScript-only closure analysis or
// the optional tdewolff minifier runs. All-JavaScript chunks return unchanged.
func transpileTypedChunk(entry output, code string) (string, error) {
	language, err := languageForOutput(entry)
	if err != nil {
		return "", err
	}
	if language == sourceJavaScript {
		return code, nil
	}

	result := esbuild.Transform(code, esbuild.TransformOptions{
		Charset:       esbuild.CharsetUTF8,
		LegalComments: esbuild.LegalCommentsNone,
		Loader:        language.esbuildLoader(),
		Sourcefile:    entry.name,
		Target:        esbuild.ES2020,
	})
	if len(result.Errors) > 0 {
		return "", fmt.Errorf("transpile %s: %s", entry.name, result.Errors[0].Text)
	}
	return string(result.Code), nil
}
