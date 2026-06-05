//go:build !tinygo

package gosx_test

import (
	"testing"

	"m31labs.dev/gosx"
	"m31labs.dev/gosx/client/vm"
	"m31labs.dev/gosx/ir"
)

func reactIsland(t *testing.T, decls, body string) *vm.Island {
	t.Helper()
	src := []byte("package main\n\n//gosx:island\nfunc D(props any) Node {\n" + decls + "\treturn " + body + "\n}\n")
	prog, err := gosx.Compile(src)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	for i, c := range prog.Components {
		if c.IsIsland {
			p, err := ir.LowerIsland(prog, i)
			if err != nil {
				t.Fatalf("lower: %v", err)
			}
			return vm.NewIsland(p, "{}")
		}
	}
	t.Fatal("no island")
	return nil
}

// TestEvalTreeMergesAdjacentText proves a static-text + {expr} pair resolves to a
// single text node (mirroring the browser's parse of the server's contiguous
// HTML), so a signal change produces ONE SetText replacing the merged node — not
// an appended text node (the "count is 34" hydration bug).
func TestEvalTreeMergesAdjacentText(t *testing.T) {
	isl := reactIsland(t, "\tn := signal.New(0)\n\tinc := func() { n.Set(n.Get() + 1) }\n\t_ = inc\n",
		`<span>count is {n.Get()}</span>`)
	ps := isl.Dispatch("inc", "{}")
	if len(ps) != 1 || ps[0].Kind != vm.PatchSetText || ps[0].Text != "count is 1" {
		t.Fatalf("want single SetText 'count is 1', got %+v", ps)
	}
}

// TestConditionalAtTailMountsUnmounts proves a trailing {cond && <jsx>} mounts on
// toggle-on and removes on toggle-off (structural reactivity).
func TestConditionalAtTailMountsUnmounts(t *testing.T) {
	isl := reactIsland(t, "\topen := signal.New(false)\n\ttog := func() { open.Set(!open.Get()) }\n\t_ = tog\n",
		"<div><button onClick={tog}>x</button>{open.Get() && <span>S</span>}</div>")
	on := isl.Dispatch("tog", "{}")
	created := false
	for _, p := range on {
		if p.Kind == vm.PatchCreateElement && p.Tag == "span" {
			created = true
		}
	}
	if !created {
		t.Fatalf("toggle-on did not create span: %+v", on)
	}
	off := isl.Dispatch("tog", "{}")
	removed := false
	for _, p := range off {
		if p.Kind == vm.PatchRemoveElement {
			removed = true
		}
	}
	if !removed {
		t.Fatalf("toggle-off did not remove: %+v", off)
	}
}

// TestConditionalInMiddleMountsAndUpdatesSibling proves a conditional toggling
// in the MIDDLE of siblings inserts its node in place AND lets following
// siblings keep updating — no cascade/full-recreate. This is the identity-aware
// positional diff (insert anywhere, not just the tail).
func TestConditionalInMiddleMountsAndUpdatesSibling(t *testing.T) {
	isl := reactIsland(t, "\topen := signal.New(false)\n\ttog := func() { open.Set(!open.Get()) }\n\t_ = tog\n",
		"<div><button onClick={tog}>x</button>{open.Get() && <span>S</span>}<b>{open.Get() ? \"Y\" : \"N\"}</b></div>")
	ps := isl.Dispatch("tog", "{}")
	createdSpan, setY, removes := false, false, 0
	for _, p := range ps {
		if p.Kind == vm.PatchCreateElement && p.Tag == "span" {
			createdSpan = true
		}
		if p.Kind == vm.PatchSetText && p.Text == "Y" {
			setY = true
		}
		if p.Kind == vm.PatchRemoveElement {
			removes++
		}
	}
	if !createdSpan {
		t.Fatalf("conditional did not insert span: %+v", ps)
	}
	if !setY {
		t.Fatalf("following sibling <b> did not update to Y: %+v", ps)
	}
	if removes != 0 {
		t.Fatalf("expected no removals (clean middle insert), got %d: %+v", removes, ps)
	}
}
