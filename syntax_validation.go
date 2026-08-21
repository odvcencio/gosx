//go:build !tinygo

package gosx

import (
	gotreesitter "github.com/odvcencio/gotreesitter"
	sharedsyntax "m31labs.dev/gosx/internal/syntax"
)

// ValidateMarkupTree applies the shared source-level GSX structural validator
// and adapts its located diagnostic to GoSX's public ParseError type. Keeping
// this adapter in the root package lets every source consumer report the same
// line/column/snippet contract without creating an import cycle in the shared
// validator.
func ValidateMarkupTree(root *gotreesitter.Node, source []byte, lang *gotreesitter.Language) error {
	err := sharedsyntax.ValidateTree(root, source, lang)
	if err == nil {
		return nil
	}
	if located, ok := err.(*sharedsyntax.LocatedError); ok {
		return &ParseError{
			Line:    located.Line,
			Column:  located.Column,
			Message: located.Message,
			Snippet: located.Snippet,
		}
	}
	return err
}
