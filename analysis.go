//go:build !tinygo

package gosx

import (
	"errors"
	"time"

	gotreesitter "github.com/odvcencio/gotreesitter"
	"m31labs.dev/gosx/ir"
)

// AnalysisPhase identifies a host compiler stage, in execution order.
type AnalysisPhase string

const (
	AnalysisParse    AnalysisPhase = "parse"
	AnalysisLower    AnalysisPhase = "lower"
	AnalysisValidate AnalysisPhase = "validate"
)

// AnalysisEvent is a value-only observation of a finished compiler stage.
// An observer cannot mutate compiler state through this event. Duration includes
// the stage's work, but excludes the observer. Error is empty on success.
type AnalysisEvent struct {
	Phase      AnalysisPhase `json:"phase"`
	Duration   time.Duration `json:"durationNs"`
	Components int           `json:"components"`
	Nodes      int           `json:"nodes"`
	Error      string        `json:"error,omitempty"`
}

// AnalysisOptions controls host-side inspection; it never changes language rules.
type AnalysisOptions struct {
	IncludeWarnings bool
	// Observe runs synchronously once after each attempted stage, including a
	// failed stage. No later stage runs after failure. There is no global hook
	// registry. Callers sharing an observer across goroutines synchronize it.
	// Observer panics propagate to the caller.
	Observe func(AnalysisEvent)
}

// Analysis exposes the artifacts reached by one source-file analysis. Tree and
// Language are available after parsing, even for recoverable syntax errors.
// Program is available after successful lowering, even if validation fails;
// it must not be rendered when Analyze returns an error. Diagnostics contains
// lowering/validation findings and optional warnings; syntax errors retain the
// existing ParseError type in the returned error.
//
// Keep source unchanged while inspecting Tree. Artifacts are for inspection,
// not a supported in-place compiler transformation protocol. Analysis performs
// no project I/O, package type checking, island bytecode emission, or rendering.
// Like Parse, this API is host tooling and is unavailable under TinyGo.
type Analysis struct {
	Tree        *gotreesitter.Tree
	Language    *gotreesitter.Language
	Program     *ir.Program
	Diagnostics []ir.Diagnostic
	Phase       AnalysisPhase
}

// Analyze runs the same parse/lower/validate path as Compile, retaining artifacts
// for editors, studio inspectors, and tooling. The result is always non-nil,
// including on failure. Warnings never make an otherwise valid source fail.
func Analyze(source []byte, options AnalysisOptions) (*Analysis, error) {
	result := &Analysis{}
	finish := func(phase AnalysisPhase, started time.Time, err error) {
		result.Phase = phase
		if options.Observe == nil {
			return
		}
		event := AnalysisEvent{Phase: phase, Duration: time.Since(started)}
		if result.Program != nil {
			event.Components = len(result.Program.Components)
			event.Nodes = len(result.Program.Nodes)
		}
		if err != nil {
			event.Error = err.Error()
		}
		options.Observe(event)
	}

	started := time.Now()
	var err error
	result.Tree, result.Language, err = Parse(source)
	if err == nil {
		root := result.Tree.RootNode()
		err = DescribeParseError(root, source, result.Language)
		if err == nil {
			err = requirePackageClause(root, result.Language)
		}
	}
	finish(AnalysisParse, started, err)
	if err != nil {
		return result, err
	}

	started = time.Now()
	result.Program, err = ir.Lower(result.Tree.RootNode(), source, result.Language)
	if err != nil {
		var diagnostics *ir.DiagnosticsError
		if errors.As(err, &diagnostics) {
			result.Diagnostics = append(result.Diagnostics, diagnostics.Diagnostics...)
		}
	}
	finish(AnalysisLower, started, err)
	if err != nil {
		return result, err
	}

	started = time.Now()
	result.Diagnostics = ir.Validate(result.Program)
	err = ir.NewDiagnosticsError("validation", result.Diagnostics)
	if options.IncludeWarnings {
		result.Diagnostics = append(result.Diagnostics, ir.ValidateWarnings(result.Program)...)
	}
	finish(AnalysisValidate, started, err)
	return result, err
}
