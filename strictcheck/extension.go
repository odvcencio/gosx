package strictcheck

import (
	"fmt"
	"strings"
	"sync"

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
// alters a built-in strictcheck finding — checkPackage runs every built-in
// stage to completion before any Lint.Check call happens (see checkPackage
// in check.go), so this is a structural guarantee, not just a convention.
// Check must not modify file.Program even so: the same *ir.Program a lint
// receives also feeds the built-in projection for as long as any built-in
// stage is still running, and a later minor version may reorder or
// parallelize lints against builtins in a way a mutating Check would not
// survive. Treat file.Program as read-only.
type Lint struct {
	// Name identifies this lint in diagnostics and in containment messages
	// (a panic, or a Check with a nil function; see runOneLint). Keep it
	// short and stable, for example a rule-catalog name such as "gsxmail".
	// An empty Name is accepted: containment messages fall back to this
	// lint's position in Options.ExtraLints (for example "lint #2") so a
	// misconfigured or anonymous lint still identifies itself.
	Name string

	// Check inspects one compiled .gsx file and reports zero or more
	// findings by calling report. Check must not retain file or
	// file.Program past the call, and must not modify file.Program: the
	// same Program object also feeds strictcheck's own built-in projection
	// while a built-in stage is running (see checkPackage's ordering
	// guarantee above), and after Check returns strictcheck may reuse or
	// discard file and file.Program for the next lint or the next file.
	//
	// report is safe to call from multiple goroutines concurrently (it is
	// internally synchronized), but it does not retain a call made after
	// Check has already returned to its caller — such a call is dropped
	// with no effect and does not panic, block, or appear anywhere in the
	// check result. A lint that fans work out to goroutines must join them
	// (for example with sync.WaitGroup.Wait) before Check returns if it
	// wants every report call to count.
	//
	// A nil Check is rejected: the lint is skipped and reported as one
	// diagnostic explaining why, rather than being invoked and producing a
	// misleading "panicked" diagnostic for what is actually a configuration
	// mistake.
	//
	// A panic inside Check is recovered by strictcheck: it is reported as
	// one diagnostic naming this lint, and the check run continues over the
	// remaining files and lints rather than crashing (fail closed on the
	// finding, contained on the failure). This containment covers an
	// ordinary panic on the goroutine that called Check only. It does not
	// and cannot cover runtime.Goexit (which unwinds that goroutine without
	// being a recoverable panic), os.Exit (which terminates the process
	// immediately, running no defers at all), or a panic on a goroutine
	// Check itself spawned (Go recovers a panic only on the same goroutine
	// where it occurred) — any of those still crashes or exits the whole
	// process. Diagnostic volume from Check is uncapped: a lint is trusted
	// in-process code, and strictcheck applies no limit to how many
	// diagnostics one Check call may report.
	Check func(file LintFile, report func(ir.Diagnostic))
}

// LintFile is the per-file context strictcheck gives to one Lint.Check
// call: the source path and the compiled IR for exactly that file.
type LintFile struct {
	// Path is the path of the .gsx source file, exactly as passed to
	// strictcheck (CheckFile, CheckPackage, or CheckTree).
	Path string

	// Program is the compiled IR for Path. It is never nil. Lints run only
	// after every file in the containing package has compiled: a package
	// with a compile error in any one file (gosx.Compile / transpile.
	// LoadPackage) never reaches checkPackage at all, so it yields no lint
	// findings for any of its files, healthy or not.
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
		for idx, lint := range lints {
			diags = append(diags, runOneLint(idx, lf, lint)...)
		}
	}
	return ir.NewDiagnosticsError("strictcheck extension", diags)
}

// runOneLint invokes one lint over one file, containing any panic as a
// diagnostic that names the lint instead of letting it crash the check run,
// and rejecting a nil Check outright instead of invoking it. report is
// mutex-guarded so concurrent calls during Check are race-free (gosx#186
// M2), and it stops retaining findings the instant Check returns to us, so
// a call arriving later from a goroutine Check left running is silently and
// safely dropped (see the Lint.Check doc for both rules).
func runOneLint(idx int, file LintFile, lint Lint) []ir.Diagnostic {
	if lint.Check == nil {
		return []ir.Diagnostic{{
			Span:    ir.Span{File: file.Path},
			Message: fmt.Sprintf("third-party lint %s has no Check function and was skipped", lintLabel(lint.Name, idx)),
		}}
	}

	var (
		mu     sync.Mutex
		diags  []ir.Diagnostic
		closed bool
	)
	report := func(d ir.Diagnostic) {
		if d.Span.File == "" {
			// Only an unset Span.File is filled in. A lint may deliberately
			// attribute a finding to a different file than the one it was
			// handed (gosx#186 n2) -- report trusts it, the same way it
			// trusts everything else about in-process caller code, and
			// leaves an explicitly set Span.File untouched.
			d.Span.File = file.Path
		}
		// gosx#186 n1: a lint-supplied Code or Message containing a
		// newline could otherwise inject what looks like an extra
		// diagnostic line into the joined error text.
		d.Code = sanitizeDiagnosticText(d.Code)
		d.Message = sanitizeDiagnosticText(d.Message)

		mu.Lock()
		defer mu.Unlock()
		if closed {
			return
		}
		diags = append(diags, d)
	}

	func() {
		defer func() {
			if r := recover(); r != nil {
				mu.Lock()
				diags = append(diags, ir.Diagnostic{
					Span:    ir.Span{File: file.Path},
					Message: fmt.Sprintf("third-party lint %s panicked and was skipped: %v", lintLabel(lint.Name, idx), r),
				})
				mu.Unlock()
			}
		}()
		lint.Check(file, report)
	}()

	mu.Lock()
	closed = true
	result := diags
	mu.Unlock()
	return result
}

// lintLabel names a lint for a containment-style diagnostic: its own Name
// when set, or its position in Options.ExtraLints (for example "#2") when
// Name is empty (gosx#186 n3), so an anonymous or misconfigured lint still
// identifies itself instead of printing an empty pair of quotes.
func lintLabel(name string, idx int) string {
	name = strings.TrimSpace(name)
	if name != "" {
		return fmt.Sprintf("%q", name)
	}
	return fmt.Sprintf("#%d", idx)
}

// sanitizeDiagnosticText replaces a newline or carriage return with a
// visible "\n" escape so a lint-supplied Code or Message cannot inject what
// looks like an extra diagnostic line into the joined error text (gosx#186
// n1). Text with no newline is returned unchanged.
func sanitizeDiagnosticText(s string) string {
	if !strings.ContainsAny(s, "\r\n") {
		return s
	}
	replacer := strings.NewReplacer("\r\n", "\\n", "\n", "\\n", "\r", "\\n")
	return replacer.Replace(s)
}
