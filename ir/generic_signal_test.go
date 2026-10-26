package ir_test

import (
	"fmt"
	"testing"

	"m31labs.dev/gosx/ir"
)

func TestLowerGenericSignalMetadata(t *testing.T) {
	const sigImport = `import sig "m31labs.dev/gosx/signal"`
	const dotImport = `import . "m31labs.dev/gosx/signal"`
	for _, tc := range []struct {
		name, prelude, declaration string
		signal                     *ir.SignalInfo
		computed                   *ir.ComputedInfo
	}{
		{"dot constructor", dotImport, "x := New[int](0)", &ir.SignalInfo{Name: "x", Local: "x", InitExpr: "0", TypeHint: "int", SourceType: "int"}, nil},
		{"qualified type", sigImport + "\nimport \"time\"", "x := sig.New[time.Duration](0)", &ir.SignalInfo{Name: "x", Local: "x", InitExpr: "0", TypeHint: "int", SourceType: "time.Duration"}, nil},
		{"var declaration", sigImport, "var x = sig.New[int](0)", &ir.SignalInfo{Name: "x", Local: "x", InitExpr: "0", TypeHint: "int", SourceType: "int"}, nil},
		{"derive", sigImport, "x := sig.Derive[int](func() int { return 1 })", nil, &ir.ComputedInfo{Name: "x", BodyExpr: "1", ReturnType: "int"}},
		{"computed alias", sigImport, "x := sig.Computed[int](func() int { return 1 })", nil, &ir.ComputedInfo{Name: "x", BodyExpr: "1", ReturnType: "int"}},
		{"commented operand", sigImport, "x := sig.New[int](/* c */ 0)", &ir.SignalInfo{Name: "x", Local: "x", InitExpr: "0", TypeHint: "int", SourceType: "int"}, nil},
		{"commented argument", sigImport, `x := sig.NewShared[int](/* c */ "x", /* c */ 0)`, &ir.SignalInfo{Name: "$x", Local: "x", InitExpr: "0", TypeHint: "int", SourceType: "int"}, nil},
		{"other package", `import other "example.com/other"`, "x := other.New[int](0)", nil, nil},
		{"value index", "", "x := m[k](1)", nil, nil},
		{"dot generic conversion", dotImport + "\ntype Box[T any] struct { Value T }", "x := Box[int](b)", nil, nil},
		{"slice conversion", "", "x := []byte(s)", nil, nil},
		{"literal index", "var signal struct { New []func(int) int }", "x := signal.New[0](1)", nil, nil},
		{"call index", "var signal struct { New []func(int) int }", "x := signal.New[f()](1)", nil, nil},
		{"binary index", "var signal struct { New []func(int) int }", "x := signal.New[1 + 1](1)", nil, nil},
		{"computed conversion", sigImport, "x := sig.Computed[int](*d)", nil, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			prog, err := parse(t, []byte(fmt.Sprintf(`package main
%s
//gosx:island
func Example() Node {
    %s
    return <div />
}`, tc.prelude, tc.declaration)))
			if err != nil {
				t.Fatal(err)
			}
			scope := prog.Components[0].Scope
			var signals []ir.SignalInfo
			var computeds []ir.ComputedInfo
			if scope != nil {
				signals, computeds = scope.Signals, scope.Computeds
			}
			if tc.signal == nil {
				if len(signals) != 0 {
					t.Fatalf("unexpected SignalInfo: %+v", signals)
				}
			} else if len(signals) != 1 || signals[0] != *tc.signal {
				t.Fatalf("SignalInfo = %+v, want %+v", signals, *tc.signal)
			}
			if tc.computed == nil {
				if len(computeds) != 0 {
					t.Fatalf("unexpected ComputedInfo: %+v", computeds)
				}
			} else if len(computeds) != 1 || computeds[0] != *tc.computed {
				t.Fatalf("ComputedInfo = %+v, want %+v", computeds, *tc.computed)
			}
		})
	}
}
