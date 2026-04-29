package vm

import (
	"fmt"

	"github.com/odvcencio/gosx/island/program"
)

// DiagnosticSeverity classifies an island VM diagnostic.
type DiagnosticSeverity string

const (
	// DiagnosticError marks malformed programs or evaluations that forced the
	// VM onto a zero-value fallback path.
	DiagnosticError DiagnosticSeverity = "error"
	// DiagnosticWarning marks recoverable VM states that are usually useful
	// while debugging but do not necessarily mean the program is malformed.
	DiagnosticWarning DiagnosticSeverity = "warning"
)

// Diagnostic is a structured VM evaluation diagnostic. Eval still returns a
// zero value for malformed expressions; diagnostics expose why that fallback
// was used without changing the long-standing panic-free contract.
type Diagnostic struct {
	Severity DiagnosticSeverity `json:"severity"`
	Code     string             `json:"code"`
	Message  string             `json:"message"`
	ExprID   *program.ExprID    `json:"exprId,omitempty"`
	NodeID   *program.NodeID    `json:"nodeId,omitempty"`
	Op       program.OpCode     `json:"op,omitempty"`
	Value    string             `json:"value,omitempty"`
}

// DiagnosticSink receives diagnostics as they are recorded.
type DiagnosticSink func(Diagnostic)

// SetDiagnosticSink registers an optional callback for VM diagnostics.
func (vm *VM) SetDiagnosticSink(sink DiagnosticSink) {
	vm.diagnosticSink = sink
}

// Diagnostics returns a snapshot of diagnostics recorded so far.
func (vm *VM) Diagnostics() []Diagnostic {
	return append([]Diagnostic(nil), vm.diagnostics...)
}

// ClearDiagnostics clears diagnostics recorded by previous evaluations.
func (vm *VM) ClearDiagnostics() {
	vm.diagnostics = vm.diagnostics[:0]
}

// EvalWithDiagnostics evaluates an expression and returns only the diagnostics
// recorded during that evaluation.
func (vm *VM) EvalWithDiagnostics(id program.ExprID) (Value, []Diagnostic) {
	before := len(vm.diagnostics)
	value := vm.Eval(id)
	return value, append([]Diagnostic(nil), vm.diagnostics[before:]...)
}

func (vm *VM) recordDiagnostic(d Diagnostic) {
	if d.Severity == "" {
		d.Severity = DiagnosticError
	}
	vm.diagnostics = append(vm.diagnostics, d)
	if vm.diagnosticSink != nil {
		vm.diagnosticSink(d)
	}
}

func (vm *VM) recordExprDiagnostic(code, message string, op program.OpCode, value string) {
	vm.recordDiagnostic(Diagnostic{
		Severity: DiagnosticError,
		Code:     code,
		Message:  message,
		Op:       op,
		Value:    value,
	})
}

func (vm *VM) recordMissingOperands(e program.Expr, want int) {
	vm.recordExprDiagnostic(
		"missing_operands",
		fmt.Sprintf("opcode %d requires at least %d operands, got %d", e.Op, want, len(e.Operands)),
		e.Op,
		e.Value,
	)
}

func (vm *VM) requireOperands(e program.Expr, want int) bool {
	if len(e.Operands) >= want {
		return true
	}
	vm.recordMissingOperands(e, want)
	return false
}

func diagnosticExprID(id program.ExprID) *program.ExprID {
	v := id
	return &v
}

func diagnosticNodeID(id program.NodeID) *program.NodeID {
	v := id
	return &v
}
