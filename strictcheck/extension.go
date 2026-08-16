package strictcheck

import (
	"fmt"

	"m31labs.dev/gosx/ir"
	"m31labs.dev/gosx/transpile"
)

// Lint is one third-party, per-file check that a consumer registers through
// Options.ExtraLints.
//
// EXPERIMENTAL (gosx#186): this extension point is library-level and
// in-process only. There is no plugin loading, no CLI flag surface, and no
// global registry — a consumer imports strictcheck, builds its own []Lint
// values, and passes them through Options on every call it makes. The
// shape of Lint and LintFile may still change in a later minor version as
// real consumers exercise it; pin an exact gosx version until it settles.
//
// Compatibility posture: a zero-value or nil Options.ExtraLints leaves
// strictcheck's behavior unchanged from a build with no extension point at
// all. Registering lints only adds diagnostics; it never suppresses or
// alters a built-in strictcheck finding.
type Lint struct {
	// Name identifies this lint in diagnostics and in panic-containment
	// messages. Keep it short and stable, for example a rule-catalog name
	// such as "gsxmail".
	Name string

	// Check inspects one compiled .gsx file and reports zero or more
	// findings by calling report. Check must not retain file or file.Program
	// past the call; strictcheck may reuse or discard them afterward.
	//
	// A panic inside Check is recovered by strictcheck: it is reported as
	// one diagnostic naming this lint, and the check run continues over the
	// remaining files and lints rather than crashing (fail closed on the
	// finding, contained on the failure).
	Check func(file LintFile, report func(ir.Diagnostic))
}

// LintFile is the per-file context strictcheck gives to one Lint.Check
// call: the source path and the compiled IR for exactly that file.
type LintFile struct {
	// Path is the path of the .gsx source file, exactly as passed to
	// strictcheck (CheckFile, CheckPackage, or CheckTree).
	Path string

	// Program is the compiled IR for Path. It is never nil.
	Program *ir.Program
}

// runExtraLints runs every registered lint over every file in files and
// returns their findings as one diagnostics error, or nil when there were
// none (including when lints is empty, so callers pay no cost and see no
// behavior change with no extra lints registered). A panic inside one
// Lint.Check call is contained and reported as a diagnostic rather than
// propagated, and does not stop the remaining files or lints from running.
func runExtraLints(files []transpile.PackageFile, lints []Lint) error {
	if len(lints) == 0 {
		return nil
	}
	var diags []ir.Diagnostic
	for _, file := range files {
		if file.Program == nil {
			continue
		}
		lf := LintFile{Path: file.Path, Program: file.Program}
		for _, lint := range lints {
			diags = append(diags, runOneLint(lf, lint)...)
		}
	}
	return ir.NewDiagnosticsError("strictcheck extension", diags)
}

// runOneLint invokes one lint over one file, containing any panic as a
// diagnostic that names the lint instead of letting it crash the check run.
func runOneLint(file LintFile, lint Lint) (diags []ir.Diagnostic) {
	defer func() {
		if r := recover(); r != nil {
			diags = append(diags, ir.Diagnostic{
				Span:    ir.Span{File: file.Path},
				Message: fmt.Sprintf("third-party lint %q panicked and was skipped: %v", lint.Name, r),
			})
		}
	}()
	report := func(d ir.Diagnostic) {
		if d.Span.File == "" {
			d.Span.File = file.Path
		}
		diags = append(diags, d)
	}
	lint.Check(file, report)
	return diags
}
