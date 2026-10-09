//go:build !tinygo

package ir

import (
	"fmt"
	"go/constant"
	"testing"

	"m31labs.dev/gosx/island/aot"
	"m31labs.dev/gosx/island/program"
)

func TestIslandAOTAggregateCannotHideInOperandConsumers(t *testing.T) {
	for _, op := range []program.OpCode{program.OpSeq, program.OpConcat, program.OpFormat, program.OpToString, program.OpCall} {
		t.Run(fmt.Sprintf("opcode_%d", op), func(t *testing.T) {
			// A scalar final operand must not hide an earlier aggregate.
			e := program.Expr{Op: op, Operands: []program.ExprID{0, 1}}
			if err := aotScalarOperands(e, []aot.ScalarKind{aot.SelectorPath, aot.String}); err == nil {
				t.Fatal("aggregate escaped through a scalar result")
			}
		})
	}
}

func TestIslandAOTAggregateRejectedAtEveryProgramRoot(t *testing.T) {
	for _, tc := range []struct {
		name string
		p    program.Program
	}{
		{"DOM text", program.Program{Nodes: []program.Node{{Kind: program.NodeExpr}}}},
		{"attribute", program.Program{Nodes: []program.Node{{Attrs: []program.Attr{{Kind: program.AttrExpr}}}}}},
		{"conditional", program.Program{Nodes: []program.Node{{Kind: program.NodeConditional}}}},
		{"iteration", program.Program{Nodes: []program.Node{{Kind: program.NodeForEach}}}},
		{"signal", program.Program{Signals: []program.SignalDef{{Name: "value"}}}},
		{"computed", program.Program{Computeds: []program.ComputedDef{{Name: "value"}}}},
		{"handler", program.Program{Handlers: []program.Handler{{Body: []program.ExprID{0}}}}},
		{"function", program.Program{Funcs: []program.FuncDef{{Body: []program.ExprID{0}}}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			states := map[string]aot.ScalarKind{"value": aot.String}
			constants := []constant.Value{nil}
			if err := aotScalarRoots(&tc.p, []aot.ScalarKind{aot.SelectorPath}, constants, states); err == nil {
				t.Fatal("aggregate root admitted")
			}
			if err := aotScalarRoots(&tc.p, []aot.ScalarKind{aot.String}, constants, states); err != nil {
				t.Fatalf("scalar root rejected: %v", err)
			}
		})
	}
}
