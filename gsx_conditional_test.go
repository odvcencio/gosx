//go:build !tinygo

package gosx_test

import (
	"testing"

	"m31labs.dev/gosx"
	"m31labs.dev/gosx/ir"
	"m31labs.dev/gosx/island/program"
)

// lowerIslandBody compiles a one-island source whose body is `body` and returns
// the lowered island program.
func lowerIslandBody(t *testing.T, body string) *program.Program {
	t.Helper()
	src := []byte("package main\n\n//gosx:island\nfunc D(props any) Node {\n\tx := signal.New(0)\n\treturn " + body + "\n}\n")
	prog, err := gosx.Compile(src)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	for i, c := range prog.Components {
		if c.IsIsland {
			isl, err := ir.LowerIsland(prog, i)
			if err != nil {
				t.Fatalf("lower island: %v", err)
			}
			return isl
		}
	}
	t.Fatal("no island component")
	return nil
}

func hasConditional(p *program.Program) bool {
	for _, n := range p.Nodes {
		if n.Kind == program.NodeConditional {
			return true
		}
	}
	return false
}

// TestConditionalRenderAndJSX proves `{cond && <jsx>}` lowers to a conditional
// subtree (not a raw expression hole the DSL would reject).
func TestConditionalRenderAndJSX(t *testing.T) {
	p := lowerIslandBody(t, `<div>{x.Get() == 0 && <span>zero</span>}</div>`)
	if !hasConditional(p) {
		t.Fatal("{cond && <jsx>} did not lower to a NodeConditional")
	}
}

// TestConditionalRenderTernaryJSX proves `{cond ? <a> : <b>}` lowers to
// conditional subtrees for both branches.
func TestConditionalRenderTernaryJSX(t *testing.T) {
	p := lowerIslandBody(t, `<div>{x.Get() == 0 ? <span>zero</span> : <span>more</span>}</div>`)
	if !hasConditional(p) {
		t.Fatal("{cond ? <a> : <b>} did not lower to a NodeConditional")
	}
}

// TestPlainTernaryStillHole proves a value ternary (no JSX) is unaffected — it
// stays an expression hole the DSL evaluates, not a conditional.
func TestPlainTernaryStillHole(t *testing.T) {
	p := lowerIslandBody(t, `<span>{x.Get() == 0 ? "a" : "b"}</span>`)
	if hasConditional(p) {
		t.Fatal("plain value ternary should not lower to a NodeConditional")
	}
}
