package ir

import (
	"fmt"
	"strings"
)

// DiagnosticsError reports one or more source diagnostics for a compiler phase.
type DiagnosticsError struct {
	Phase       string
	Diagnostics []Diagnostic
}

// Error formats the diagnostic set into a human-readable compiler error.
func (e *DiagnosticsError) Error() string {
	if e == nil || len(e.Diagnostics) == 0 {
		return ""
	}
	lines := make([]string, 0, len(e.Diagnostics)+1)
	phase := strings.TrimSpace(e.Phase)
	if phase == "" {
		phase = "compiler"
	}
	lines = append(lines, fmt.Sprintf("%s diagnostics:", phase))
	for _, diag := range e.Diagnostics {
		lines = append(lines, diag.String())
	}
	return strings.Join(lines, "\n")
}

// NewDiagnosticsError wraps a non-empty diagnostic set as an error.
func NewDiagnosticsError(phase string, diagnostics []Diagnostic) error {
	if len(diagnostics) == 0 {
		return nil
	}
	cloned := make([]Diagnostic, len(diagnostics))
	copy(cloned, diagnostics)
	return &DiagnosticsError{
		Phase:       phase,
		Diagnostics: cloned,
	}
}
