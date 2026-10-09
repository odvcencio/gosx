//go:build !tinygo && !js

package ir

import (
	"fmt"
	"go/constant"
	"slices"
	"strings"

	"m31labs.dev/gosx/island/aot"
	"m31labs.dev/gosx/island/program"
)

// LowerIslandAOT retains the VM artifact and proves its expression graph with
// host-only Go type evidence. Incomplete source or shape evidence stays on VM.
func LowerIslandAOT(src *Program, index int) (aot.Unit, error) {
	if src == nil || index < 0 || index >= len(src.Components) {
		return aot.Unit{}, fmt.Errorf("invalid island component index")
	}
	comp := src.Components[index]
	for _, declarations := range []map[string]string{comp.PropsFields, comp.PropsPaths} {
		for _, typ := range declarations {
			if src.aotScalarShadows[typ] {
				return aot.Unit{}, aotSourceError(comp, "source_type", "a scalar type name is shadowed")
			}
		}
	}
	identity := src.PackagePath + "." + comp.Name
	if src.PackagePath == "" {
		return aot.Unit{}, aotSourceError(comp, "component_identity", "an import path is required")
	}
	names, err := aotWalkSource(src, comp)
	if err != nil {
		return aot.Unit{}, aotSourceError(comp, "source_graph", err.Error())
	}
	graph, err := LowerIsland(src, index)
	if err != nil {
		return aot.Unit{}, aotSourceError(comp, "source_graph", err.Error())
	}
	if err := aotGraphLimits(graph); err != nil {
		return aot.Unit{}, aotSourceError(comp, "source_graph", err.Error())
	}
	checked, err := aotCheckSource(src)
	if err != nil {
		return aot.Unit{}, aotSourceError(comp, "source_type", err.Error())
	}
	if err := checked.candidateError(names); err != nil {
		return aot.Unit{}, aotSourceError(comp, "source_type", err.Error())
	}
	evidence, err := newAOTEvidence(checked, src, comp)
	if err != nil {
		return aot.Unit{}, aotSourceError(comp, "source_type", err.Error())
	}
	shared := evidence.shared
	for _, state := range graph.Signals {
		shared = shared || strings.HasPrefix(state.Name, "$")
	}
	if shared {
		return aot.Unit{}, aotSourceError(comp, "source_type", "shared_signal_profile: page-wide state has no type registry")
	}
	if err := evidence.validateSourceRoots(src, names); err != nil {
		return aot.Unit{}, aotSourceError(comp, "source_type", err.Error())
	}
	p, err := lowerIslandWithEvidence(src, index, evidence.pair)
	if err != nil {
		return aot.Unit{}, aotSourceError(comp, "source_type", err.Error())
	}
	limits := aot.ProfileLimits()
	if len(p.Nodes) > int(limits.Nodes) || len(p.Exprs) > int(limits.Expressions) {
		return aot.Unit{}, aotSourceError(comp, "source_graph", "graph_limit: scalar profile counts")
	}
	c := aot.ScalarContract{Version: 1, Component: identity, Expressions: []aot.ExpressionContract{}, Inputs: []aot.InputContract{}, Signals: []aot.StateContract{}, Computeds: []aot.StateContract{}}
	kinds := make([]aot.ScalarKind, len(p.Exprs))
	constants := make([]constant.Value, len(p.Exprs))
	for id, expr := range p.Exprs {
		proof, ok := evidence.proofs[program.ExprID(id)]
		if !ok {
			return aot.Unit{}, aotSourceError(comp, "source_type", "evidence_shape_mismatch: emitted expression has no checked node")
		}
		kinds[id], constants[id] = proof.kind, proof.value
		args := make([]aot.ScalarKind, len(expr.Operands))
		for i, operand := range expr.Operands {
			if int(operand) >= len(p.Exprs) {
				return aot.Unit{}, aotSourceError(comp, "source_graph", "invalid expression graph")
			}
			args[i] = evidence.proofs[operand].kind
		}
		if err := aotScalarOperands(expr, args); err != nil {
			return aot.Unit{}, aotSourceError(comp, "source_type", err.Error())
		}
		c.Expressions = append(c.Expressions, aot.ExpressionContract{Expr: program.ExprID(id), Kind: proof.kind, Pure: proof.pure})
		if proof.input != nil {
			input := *proof.input
			input.Exprs = []program.ExprID{program.ExprID(id)}
			c.Inputs = append(c.Inputs, input)
		}
	}
	if err := aotScalarRoots(p, kinds, constants, evidence.stateKinds); err != nil {
		return aot.Unit{}, aotSourceError(comp, "source_type", err.Error())
	}
	for slot, sig := range p.Signals {
		c.Signals = append(c.Signals, aot.StateContract{Slot: uint32(slot), Name: sig.Name, Kind: evidence.stateKinds[sig.Name]})
	}
	for slot, computed := range p.Computeds {
		c.Computeds = append(c.Computeds, aot.StateContract{Slot: uint32(slot), Name: computed.Name, Kind: evidence.stateKinds[computed.Name]})
	}
	c.Inputs = aotInternInputs(c.Inputs)
	if len(c.Inputs) > int(limits.Inputs) {
		return aot.Unit{}, aotSourceError(comp, "source_graph", "graph_limit: input count")
	}
	c.Bindings, err = aot.ContractBindings(p)
	if err != nil {
		return aot.Unit{}, aotSourceError(comp, "source_graph", err.Error())
	}
	return aot.NewUnit(identity, p, c)
}

func aotConstantFits(value constant.Value, kind aot.ScalarKind) bool {
	if value == nil || kind != aot.Int && kind != aot.Int32 {
		return false
	}
	n, exact := constant.Int64Val(value)
	return exact && n >= -1<<31 && n <= 1<<31-1
}
func aotSourceError(comp Component, code, message string) error {
	return NewDiagnosticsError("island-aot", []Diagnostic{{Span: comp.Span, Code: "aot_" + code, Message: message}})
}

func aotInternInputs(inputs []aot.InputContract) []aot.InputContract {
	compare := func(a, b aot.InputContract) int {
		if n := strings.Compare(a.Source, b.Source); n != 0 {
			return n
		}
		if n := strings.Compare(a.Root, b.Root); n != 0 {
			return n
		}
		if n := slices.Compare(a.Path, b.Path); n != 0 {
			return n
		}
		return strings.Compare(string(a.Kind), string(b.Kind))
	}
	slices.SortFunc(inputs, compare)
	result := []aot.InputContract{}
	for _, input := range inputs {
		if len(result) > 0 && compare(result[len(result)-1], input) == 0 {
			result[len(result)-1].Exprs = append(result[len(result)-1].Exprs, input.Exprs...)
		} else {
			input.ID = uint32(len(result))
			result = append(result, input)
		}
	}
	for i := range result {
		slices.Sort(result[i].Exprs)
	}
	return result
}
