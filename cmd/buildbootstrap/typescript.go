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
//
// TSX is not a supported extension yet. The bootstrap runtime configures no
// JSX factory, so an esbuild TSX transform would emit React.createElement
// calls into a bundle that ships no React. TSX support returns once a JSX
// factory is configured for the bootstrap runtime.
type sourceLanguage uint8

const (
	sourceJavaScript sourceLanguage = iota
	sourceTypeScript
)

func languageForSource(src source) (sourceLanguage, error) {
	switch strings.ToLower(filepath.Ext(src.rel)) {
	case ".js", ".mjs", ".cjs":
		return sourceJavaScript, nil
	case ".ts", ".mts", ".cts":
		return sourceTypeScript, nil
	default:
		return sourceJavaScript, fmt.Errorf("source %s has an unsupported browser source extension", src.rel)
	}
}

func languageForOutput(entry output) (sourceLanguage, error) {
	language := sourceJavaScript
	for _, src := range entry.sources {
		next, err := languageForSource(src)
		if err != nil {
			return sourceJavaScript, err
		}
		if next > language {
			language = next
		}
	}
	return language, nil
}

func (language sourceLanguage) esbuildLoader() esbuild.Loader {
	if language == sourceTypeScript {
		return esbuild.LoaderTS
	}
	return esbuild.LoaderJS
}

func esbuildLoaderForOutput(entry output) (esbuild.Loader, error) {
	language, err := languageForOutput(entry)
	if err != nil {
		return esbuild.LoaderNone, err
	}
	return language.esbuildLoader(), nil
}

// validateTypedSource validates a standalone TypeScript authority while keeping
// diagnostics tied to its original file and source range. Bootstrap sources may
// also be intentional prefix/suffix fragments, so bundle construction validates
// their concatenated chunk through esbuild rather than parsing each fragment.
func validateTypedSource(src source, body []byte) error {
	language, err := languageForSource(src)
	if err != nil {
		return err
	}
	if language == sourceJavaScript {
		return nil
	}

	grammar := grammars.TypescriptLanguage()
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
		return nil
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
