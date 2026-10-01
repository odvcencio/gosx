package typeoracle

import (
	"errors"
	"go/token"
	"go/types"

	"m31labs.dev/gosx/ir"
)

// diagnosticFromTypesError converts one go/types error into an
// ir.Diagnostic positioned in .gsx source, through the projection's "//line"
// directives (see the package doc's Source mapping section).
//
// go/types reports most errors as a types.Error value, and a few (an
// argument to a call, for example) as a *types.ArgumentError wrapping one —
// errors.As unwraps that automatically, since ArgumentError implements
// Unwrap. Anything else (defensive; go/types documents only these two
// shapes for Config.Error) is reported with no position rather than
// dropped, so a caller printing Diagnostics never silently loses a finding.
func diagnosticFromTypesError(err error) ir.Diagnostic {
	var terr types.Error
	if errors.As(err, &terr) {
		return ir.Diagnostic{
			Span:    spanFromPosition(terr.Fset.Position(terr.Pos)),
			Message: terr.Msg,
		}
	}
	return ir.Diagnostic{Message: err.Error()}
}

// spanFromPosition converts a go/token.Position — already resolved through
// any "//line" directive in effect at that position — into an ir.Span. A
// zero or negative column is go/token's "unknown" value, which a directive
// with no explicit column produces for every line past its own (see the
// package doc's Source mapping section); column 1 is reported instead of 0,
// the same fallback strictcheck/collision.go's declSpan uses for the
// identical reason, so a diagnostic always names a column an editor can
// place a caret on.
func spanFromPosition(pos token.Position) ir.Span {
	column := pos.Column
	if column <= 0 {
		column = 1
	}
	return ir.Span{
		File:      pos.Filename,
		StartLine: pos.Line,
		StartCol:  column,
		EndLine:   pos.Line,
		EndCol:    column,
	}
}
