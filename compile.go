//go:build !tinygo

package gosx

import (
	"bytes"
	"fmt"
	"sync"

	gotreesitter "github.com/odvcencio/gotreesitter"
	"m31labs.dev/gosx/ir"
)

// Cached language to avoid regenerating on every compilation.
var (
	gosxLangOnce   sync.Once
	gosxLangCached *gotreesitter.Language
	gosxLangErr    error
)

// SetGrammarBlob preloads the GoSX grammar from a pre-compiled binary blob.
// Call this before any Compile/Parse calls to skip the 40s grammar generation.
// The gosx library embeds a default blob, and callers may provide an override.
//
// An override that does not match the blob embedded in this gosx version is
// refused with an error and nothing is loaded, so a later Language() call
// falls back to the embedded blob. Parse tables from a different grammar
// version can mis-parse real source with no error node to point at
// (gosx#139: an app-staged dist/gosx-grammar.blob from an older checkout
// failed context-dependently on a multi-line self-closing svg path). The
// trivial smoke parse cannot catch that class, so the byte fingerprint is
// the gate. A rejected override costs nothing: the embedded blob carries the
// same grammar this module compiles against.
func SetGrammarBlob(data []byte) error {
	if len(embeddedGrammarBlob) > 0 && !bytes.Equal(data, embeddedGrammarBlob) {
		return fmt.Errorf(
			"grammar blob override (%d bytes) does not match the blob embedded in this gosx version (%d bytes); ignoring the override — regenerate it with `go run ./cmd/gosx-grammar-blob` or delete it to use the embedded grammar",
			len(data), len(embeddedGrammarBlob))
	}
	var err error
	gosxLangOnce.Do(func() {
		gosxLangCached, gosxLangErr = loadGSXLanguageBlob(data)
	})
	if gosxLangErr != nil {
		err = gosxLangErr
	}
	return err
}

// Language returns the GoSX tree-sitter language, generating it on first call.
// If SetGrammarBlob was called first, returns the preloaded language instantly.
// Otherwise it loads the embedded library blob before falling back to generation.
func Language() (*gotreesitter.Language, error) {
	gosxLangOnce.Do(func() {
		if len(embeddedGrammarBlob) > 0 {
			gosxLangCached, gosxLangErr = loadGSXLanguageBlob(embeddedGrammarBlob)
		} else {
			gosxLangCached, _, gosxLangErr = GenerateLanguageAndBlob(GosxGrammar())
		}
		if gosxLangErr == nil && gosxLangCached == nil {
			gosxLangCached, _, gosxLangErr = GenerateLanguageAndBlob(GosxGrammar())
		}
		if gosxLangErr == nil && gosxLangCached != nil && gosxLangCached.ExternalScanner == nil {
			gosxLangCached.ExternalScanner = newGSXScanner(gosxLangCached)
		}
	})
	return gosxLangCached, gosxLangErr
}

func loadGSXLanguageBlob(data []byte) (*gotreesitter.Language, error) {
	lang, err := LoadLanguageBlob(data)
	if err != nil {
		return nil, err
	}
	if lang != nil {
		lang.ExternalScanner = newGSXScanner(lang)
		if err := validateGSXLanguage(lang); err == nil {
			return lang, nil
		}
	}
	lang, _, err = GenerateLanguageAndBlob(GosxGrammar())
	if err != nil {
		return nil, err
	}
	if lang != nil {
		lang.ExternalScanner = newGSXScanner(lang)
	}
	return lang, nil
}

func validateGSXLanguage(lang *gotreesitter.Language) error {
	const smoke = "package app\nfunc Page() Node {\n\treturn <div>ok</div>\n}\n"
	parser := gotreesitter.NewParser(lang)
	tree, err := parser.Parse([]byte(smoke))
	if err != nil {
		return err
	}
	if root := tree.RootNode(); root == nil || root.HasError() {
		return fmt.Errorf("embedded gosx grammar blob failed smoke parse")
	}
	return nil
}

// Parse parses GoSX source into a tree-sitter tree.
func Parse(source []byte) (*gotreesitter.Tree, *gotreesitter.Language, error) {
	lang, err := Language()
	if err != nil {
		return nil, nil, fmt.Errorf("generate gosx language: %w", err)
	}

	parser := gotreesitter.NewParser(lang)
	tree, err := parser.Parse(source)
	if err != nil {
		return nil, nil, fmt.Errorf("parse: %w", err)
	}

	return tree, lang, nil
}

// Compile parses GoSX source and produces the component IR.
func Compile(source []byte) (*ir.Program, error) {
	tree, lang, err := Parse(source)
	if err != nil {
		return nil, err
	}

	root := tree.RootNode()
	if root.HasError() {
		return nil, DescribeParseError(root, source, lang)
	}
	if err := requirePackageClause(root, lang); err != nil {
		return nil, err
	}

	prog, err := ir.Lower(root, source, lang)
	if err != nil {
		return nil, err
	}

	// Run validation
	diags := ir.Validate(prog)
	if len(diags) > 0 {
		return nil, ir.NewDiagnosticsError("validation", diags)
	}

	return prog, nil
}
