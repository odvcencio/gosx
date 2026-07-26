// FuzzVMEvalNeverPanics pins the VM's "panic-free contract" (see
// vm.go's Eval doc comment) with a native Go fuzz target.
//
// The VM is the one package in this repository that promises never
// to panic. Every other opcode evaluator degrades to a zero Value
// plus a structured Diagnostic instead. Yet before this file
// existed, no fuzz target exercised that promise. An ad-hoc version
// of exactly this harness found the SliceVal/SubstringVal
// negative-end bounds bug (see value_test.go's
// TestSliceValNegativeEndDoesNotPanic) in seconds across 400,000
// random programs.
//
// The fuzzer builds a random but acyclic program.Expr graph. Node
// i's operands only ever reference nodes 0..i-1. So the graph is a
// DAG by construction, and Eval can never recurse into a cycle
// through it. (Self-referential *values*, as opposed to expression
// graphs, are a separate concern.
// TestValueStringSelfReferentialArrayDoesNotOverflow and its
// neighbors in value_test.go and json_test.go cover that case.)
// Every opcode the VM defines gets picked with equal probability.
// So a newly added opcode is automatically included without this
// file changing.

package vm

import (
	"fmt"
	"math/rand"
	"testing"

	"m31labs.dev/gosx/island/program"
	"m31labs.dev/gosx/signal"
)

// fuzzMaxNodes bounds how large a single generated program can be.
// This keeps one fuzz iteration fast, even combined with Eval's own
// depth cap (maxEvalDepth) and the loop step budget (loops.go).
const fuzzMaxNodes = 64

// fuzzMaxOperandsPerExpr bounds how many operands a single generated
// Expr can carry. The real opcodes need at most 4 (OpFor), so this
// covers every shape while keeping the DAG shallow enough to stay
// fast.
const fuzzMaxOperandsPerExpr = 4

func FuzzVMEvalNeverPanics(f *testing.F) {
	f.Add(int64(1), 8, "9e999")
	f.Add(int64(2), 20, "-1")
	f.Add(int64(3), 0, "")
	f.Add(int64(4), 50, "NaN")
	f.Add(int64(5), 12, "1e300")
	f.Add(int64(6), 3, "struct:Fuzz")
	f.Add(int64(7), 40, "slice")

	f.Fuzz(func(t *testing.T, seed int64, rawNodeCount int, litSeed string) {
		if len(litSeed) > 64 {
			litSeed = litSeed[:64] // bound pathologically huge literal strings
		}
		rng := rand.New(rand.NewSource(seed ^ int64(rawNodeCount)))
		prog := fuzzBuildProgram(rng, rawNodeCount, litSeed)

		m := NewVM(prog, map[string]Value{"fuzzProp": IntVal(1)})
		m.SetSignal("fuzzSignal", signal.New(IntVal(0)))
		m.SetForCap(1000) // keep OpFor/OpForRange bodies bounded and fast

		for id := range prog.Exprs {
			evalFuzzNode(t, m, id, prog.Exprs[id])
		}
	})
}

// evalFuzzNode evaluates one node. It turns any panic into a
// t.Fatal that carries the offending opcode. So a fuzz failure's
// corpus entry comes with an immediately actionable message,
// instead of just a raw stack trace. go test's native fuzzing still
// treats this as a failure. It still saves and minimizes the
// corpus entry, because t.Fatal marks the (*testing.T) as failed
// either way.
func evalFuzzNode(t *testing.T, m *VM, id int, e program.Expr) {
	t.Helper()
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("Eval(%d) panicked on opcode %d (value %q, operands %v): %v", id, e.Op, e.Value, e.Operands, r)
		}
	}()
	_ = m.Eval(program.ExprID(id))
}

// fuzzBuildProgram builds a random program.Program whose Exprs form
// a DAG. Every Expr's operands reference strictly earlier indices.
// So there is no cycle for Eval to recurse through.
func fuzzBuildProgram(rng *rand.Rand, rawNodeCount int, litSeed string) *program.Program {
	n := rawNodeCount % fuzzMaxNodes
	if n < 0 {
		n = -n
	}
	n++ // always at least 1 node

	exprs := make([]program.Expr, n)
	for i := 0; i < n; i++ {
		exprs[i] = fuzzRandomExpr(rng, i, litSeed)
	}
	return &program.Program{
		Name:  "fuzz",
		Exprs: exprs,
		// A bounded, non-default MaxCallDepth keeps any OpIndirectCall
		// fast to fail. The generator may produce one, but it never
		// resolves, since this fuzzer builds no Funcs. It still
		// exercises the unknown_user_function diagnostic path.
		MaxCallDepth: 32,
	}
}

// fuzzOpCodeCount is the number of opcodes program.go defines,
// computed once from the contiguous iota range [OpLitString,
// OpClosure]. Every opcode is reachable through fuzzRandomExpr with
// equal probability. A newly added opcode extends this range
// automatically, as long as it keeps the iota block contiguous.
const fuzzOpCodeCount = int(program.OpClosure) + 1

// fuzzExprTypeCount mirrors fuzzOpCodeCount for program.ExprType.
const fuzzExprTypeCount = int(program.TypeAny) + 1

func fuzzRandomExpr(rng *rand.Rand, idx int, litSeed string) program.Expr {
	e := program.Expr{
		Op:    program.OpCode(rng.Intn(fuzzOpCodeCount)),
		Type:  program.ExprType(rng.Intn(fuzzExprTypeCount)),
		Value: fuzzRandomValue(rng, litSeed),
	}

	maxOperands := idx
	if maxOperands > fuzzMaxOperandsPerExpr {
		maxOperands = fuzzMaxOperandsPerExpr
	}
	if maxOperands <= 0 {
		return e
	}
	count := rng.Intn(maxOperands + 1)
	if count == 0 {
		return e
	}
	e.Operands = make([]program.ExprID, count)
	for i := range e.Operands {
		// Strictly less than idx: this is what makes the graph acyclic.
		e.Operands[i] = program.ExprID(rng.Intn(idx))
	}
	return e
}

// fuzzRandomValue returns e.Value for a generated node. The pool
// mixes several categories:
//
//   - Numeric and boolean edge cases that stress literal parsing
//     and float-to-int casts. These are the exact shape that caused
//     the SliceVal / SubstringVal crashes in value.go.
//   - Composite-literal kind tags. OpComposite dispatches on
//     e.Value for these.
//   - Collection-scoped identifier names: _item, _index, and _key.
//   - The fuzzer-controlled litSeed. This lets Go's built-in
//     mutator explore literal strings that neither this list nor a
//     human would think to write by hand.
func fuzzRandomValue(rng *rand.Rand, litSeed string) string {
	choices := []string{
		"", "0", "-1", "1", "true", "false",
		"9e999", "-9e999", "1e300", "-1e300",
		"NaN", "Inf", "+Inf", "-Inf", "3.14", "-3.14",
		"slice", "map", "struct:Fuzz",
		"x", "i", "_item", "_index", "_key", "n",
		litSeed,
	}
	return choices[rng.Intn(len(choices))]
}

// TestFuzzVMEvalNeverPanicsSeedCorpusReplays runs the fuzz
// function's own seed corpus as a regular test. So `go test` (with
// no -fuzz flag) still exercises these specific edge cases on every
// ordinary CI run. It does not depend on someone remembering to run
// `make test-fuzz-smoke`.
func TestFuzzVMEvalNeverPanicsSeedCorpusReplays(t *testing.T) {
	seeds := []struct {
		seed      int64
		nodeCount int
		lit       string
	}{
		{1, 8, "9e999"},
		{2, 20, "-1"},
		{3, 0, ""},
		{4, 50, "NaN"},
		{5, 12, "1e300"},
		{6, 3, "struct:Fuzz"},
		{7, 40, "slice"},
	}
	for i, s := range seeds {
		t.Run(fmt.Sprintf("seed_%d", i), func(t *testing.T) {
			rng := rand.New(rand.NewSource(s.seed ^ int64(s.nodeCount)))
			prog := fuzzBuildProgram(rng, s.nodeCount, s.lit)
			m := NewVM(prog, map[string]Value{"fuzzProp": IntVal(1)})
			m.SetSignal("fuzzSignal", signal.New(IntVal(0)))
			m.SetForCap(1000)
			for id := range prog.Exprs {
				evalFuzzNode(t, m, id, prog.Exprs[id])
			}
		})
	}
}
