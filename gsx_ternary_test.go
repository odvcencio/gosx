//go:build !tinygo

package gosx_test

import (
	"testing"

	"m31labs.dev/gosx"
	"m31labs.dev/gosx/ir"
)

// TestTextChildTernaryParses proves the GSX grammar accepts a ternary
// conditional inside a text-child {...} container. Go has no ternary, but the
// island expression DSL does — and attribute {...} values already accepted it
// via the external scanner. This closes the asymmetry so `{cond ? a : b}` works
// as element content, not just in attributes.
func TestTextChildTernaryParses(t *testing.T) {
	src := []byte("package main\n\n//gosx:island\nfunc Demo() Node {\n\tc := signal.New(false)\n\treturn <span>{c.Get() ? \"a\" : \"b\"}</span>\n}\n")
	tree, lang, err := gosx.Parse(src)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if derr := gosx.DescribeParseError(tree.RootNode(), src, lang); derr != nil {
		t.Fatalf("text-child ternary rejected: %v", derr)
	}
}

// TestTextChildTernaryLowers proves the ternary flows end-to-end: compile +
// LowerIsland succeed, so the island DSL parser evaluates it (OpCond).
func TestTextChildTernaryLowers(t *testing.T) {
	src := []byte("package main\n\n//gosx:island\nfunc Demo() Node {\n\tc := signal.New(false)\n\treturn <span style={\"o:\" + (c.Get() ? \"1\" : \"0\")}>{c.Get() ? \"yes\" : \"no\"}</span>\n}\n")
	prog, err := gosx.Compile(src)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if _, err := ir.LowerIsland(prog, 0); err != nil {
		t.Fatalf("lower island (DSL eval of ternary): %v", err)
	}
}
