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

// Language returns the GoSX tree-sitter language, generating it on first call.
func Language() (*gotreesitter.Language, error) {
	gosxLangOnce.Do(func() {
		g := GosxGrammar()
		gosxLangCached, _, gosxLangErr = GenerateLanguageAndBlob(g)
		if gosxLangErr == nil && gosxLangCached != nil {
			gosxLangCached.ExternalScanner = &jsxAttributeScanner{lang: gosxLangCached}
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
		return nil, fmt.Errorf("lower: %w", err)
	}

	// Run validation
	diags := ir.Validate(prog)
	for _, d := range diags {
		// For now, treat all diagnostics as errors
		return nil, fmt.Errorf("validation: %s", d)
	}

	return prog, nil
}
