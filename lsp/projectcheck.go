package lsp

import (
	"context"
	"errors"

	"m31labs.dev/gosx/ir"
	"m31labs.dev/gosx/strictcheck"
)

// AnalyzeProject runs the whole-project checks (gosx#249's form-action,
// required-reachability, and data-loader-key checks) against the package
// containing path, reading whatever "*.server.go" siblings and
// "public/*.css" files are on disk. Unlike Analyze, this performs file
// I/O and must never run on every keystroke -- see the didSave handler in
// server.go, the one cadence this package affords it, and Analyze's own
// doc comment for the fast, no-I/O half of the split.
//
// The route-mount check (gosx#249 check 3) is deliberately absent here:
// it needs every "*.gsx" file and every router.AddDir call in the whole
// project tree, not just the one package path names, and re-walking a
// whole project on every save is a cost this package does not think an
// editor should pay. It still runs at `gosx check`/build-gate time
// through strictcheck.CheckTreeWithOptions.
func AnalyzeProject(path string) []Diagnostic {
	var warnings []ir.Diagnostic
	err := strictcheck.CheckFileWithOptions(context.Background(), path, strictcheck.Options{Warnings: &warnings})

	// A non-DiagnosticsError err (a strict-syntax Go compiler failure, an
	// import-resolution failure) is out of gosx#249's own scope and is
	// silently not turned into a Diagnostic here -- Analyze's fast,
	// per-keystroke path already reports every parse/lower error this
	// package surfaces today, and widening what didSave reports is a
	// separate change from the one this function exists for.
	diags := make([]Diagnostic, 0, len(warnings))
	var diagErr *ir.DiagnosticsError
	if errors.As(err, &diagErr) {
		for _, raw := range diagErr.Diagnostics {
			diags = append(diags, Diagnostic{
				Range:    rangeFromSpan(raw.Span),
				Severity: SeverityError,
				Source:   diagnosticSource(path),
				Message:  diagnosticMessage(raw),
			})
		}
	}
	for _, diag := range warnings {
		diags = append(diags, Diagnostic{
			Range:    rangeFromSpan(diag.Span),
			Severity: SeverityWarning,
			Source:   diagnosticSource(path),
			Message:  diagnosticMessage(diag),
		})
	}
	return diags
}
