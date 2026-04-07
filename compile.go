package gosx

import (
	"fmt"
	"sync"

	"github.com/odvcencio/gosx/ir"
	gotreesitter "github.com/odvcencio/gotreesitter"
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
func SetGrammarBlob(data []byte) error {
	var err error
	gosxLangOnce.Do(func() {
		gosxLangCached, gosxLangErr = LoadLanguageBlob(data)
		if gosxLangErr == nil && gosxLangCached != nil {
			gosxLangCached.ExternalScanner = &gsxScanner{lang: gosxLangCached}
		}
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
			gosxLangCached, gosxLangErr = LoadLanguageBlob(embeddedGrammarBlob)
		} else {
			g := GosxGrammar()
			gosxLangCached, _, gosxLangErr = GenerateLanguageAndBlob(g)
		}
		if gosxLangErr == nil && gosxLangCached != nil {
			gosxLangCached.ExternalScanner = &gsxScanner{lang: gosxLangCached}
		}
	})
	return gosxLangCached, gosxLangErr
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
