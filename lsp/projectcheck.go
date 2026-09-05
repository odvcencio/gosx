package lsp

import (
	"context"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"sort"

	"m31labs.dev/gosx/ir"
	"m31labs.dev/gosx/strictcheck"
)

type ownedDiagnostics struct {
	URI         string
	Diagnostics []Diagnostic
}

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
	var out []Diagnostic
	for _, owned := range analyzeProjectOwned(path) {
		out = append(out, owned.Diagnostics...)
	}
	return out
}

func analyzeProjectOwned(path string) []ownedDiagnostics {
	var warnings []ir.Diagnostic
	err := strictcheck.CheckFileWithOptions(context.Background(), path, strictcheck.Options{Warnings: &warnings})

	// A non-DiagnosticsError err (a strict-syntax Go compiler failure, an
	// import-resolution failure) is out of gosx#249's own scope and is
	// silently not turned into a Diagnostic here -- Analyze's fast,
	// per-keystroke path already reports every parse/lower error this
	// package surfaces today, and widening what didSave reports is a
	// separate change from the one this function exists for.
	byPath := make(map[string][]ir.Diagnostic)
	var diagErr *ir.DiagnosticsError
	if errors.As(err, &diagErr) {
		for _, raw := range diagErr.Diagnostics {
			owner := diagnosticOwnerPath(path, raw.Span)
			byPath[owner] = append(byPath[owner], raw)
		}
	}
	for _, diag := range warnings {
		owner := diagnosticOwnerPath(path, diag.Span)
		byPath[owner] = append(byPath[owner], diag)
	}
	owners := make([]string, 0, len(byPath))
	for owner := range byPath {
		owners = append(owners, owner)
	}
	sort.Strings(owners)

	out := make([]ownedDiagnostics, 0, len(owners))
	for _, owner := range owners {
		raws := byPath[owner]
		source, _ := os.ReadFile(owner)
		diags := make([]Diagnostic, 0, len(raws))
		for _, raw := range raws {
			diags = append(diags, Diagnostic{
				Range:    rangeFromSpanInSource(source, raw.Span),
				Severity: severityFromIR(raw.Severity),
				Source:   diagnosticSource(owner),
				Message:  diagnosticMessage(raw),
			})
		}
		out = append(out, ownedDiagnostics{
			URI:         pathToURI(owner),
			Diagnostics: diags,
		})
	}
	return out
}

func diagnosticOwnerPath(requested string, span ir.Span) string {
	if span.File == "" {
		return requested
	}
	if filepath.IsAbs(span.File) {
		return span.File
	}
	return filepath.Join(filepath.Dir(requested), span.File)
}

func pathToURI(path string) string {
	if path == "" {
		return ""
	}
	return (&url.URL{Scheme: "file", Path: path}).String()
}
