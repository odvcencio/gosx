// Slice Y.D failure-mode tests — diagnostics for unsupported user-
// function call shapes. The supported subset is bounded:
//
//   - In-package calls to a registered FuncDecl: supported.
//   - Calls to undeclared identifiers: legacy diagnostic with the
//     escape-hatch suggestion (so a typo'd callee gets a clear pointer
//     instead of a silent OpIndirectCall to a nonexistent FuncDef).
//   - Cross-package calls into unregistered intrinsics: unchanged
//     "not in the supported intrinsic set" diagnostic from Slice X.B.
//   - Method calls on user types: still unsupported (the VM lacks a
//     receiver-binding contract).
//
// Each test asserts the diagnostic still surfaces with the right
// context after Y.D's registry pre-pass takes effect.

package golower

import "testing"

// TestY_D_UnregisteredCalleeStillDiagnoses verifies that a typo'd or
// missing callee identifier still produces the legacy
// "calls to user-defined function" diagnostic, NOT a silent
// OpIndirectCall to a nonexistent FuncDef. The check is important
// because Y.D's registry probe is the only thing that distinguishes
// these two paths.
func TestY_D_UnregisteredCalleeStillDiagnoses(t *testing.T) {
	src := []byte(`package handlers

func F() int {
	return mistypedCallee(42)
}`)
	_, err := LowerFile(src)
	requireLowerError(t, err, "mistypedCallee", 0)
	requireLowerError(t, err, "ADR 0006", 0)
}

// TestY_D_MethodCallOnUserTypeStillDiagnoses verifies that method
// receivers remain unsupported even with the user-function registry
// in place. graph_surface.go itself uses `c.DrawLine(...)` (Y.E
// territory), but receiver method dispatch in general is not Y.D's
// scope.
func TestY_D_MethodCallOnUserTypeStillDiagnoses(t *testing.T) {
	src := []byte(`package handlers

type box struct {
	V int
}

func (b box) Get() int { return b.V }

func F() int {
	b := box{V: 7}
	return b.Get()
}`)
	_, err := LowerFile(src)
	requireLowerError(t, err, "ADR 0006", 0)
}
