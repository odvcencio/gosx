// Slice Y.E failure-mode tests. Pin diagnostic strings for the
// Y.E-introduced rejection paths so a future refactor that changes
// the wording fires a visible regression instead of silently swapping
// out the author-facing message.

package golower

import (
	"strings"
	"testing"
)

// TestFailureMode_MakeChannelType verifies that `make(chan T)` is
// rejected with a diagnostic that points at the supported subset.
// Channels are explicitly outside the engine-surface authoring
// contract per the package-level golower.go doc.
func TestFailureMode_MakeChannelType(t *testing.T) {
	src := []byte(`package handlers

func F() {
	ch := make(chan int)
	_ = ch
}`)
	_, err := LowerFile(src)
	if err == nil {
		t.Fatal("LowerFile: expected diagnostic for make(chan int), got nil")
	}
	if !strings.Contains(err.Error(), "make") {
		t.Errorf("diagnostic should mention make; got: %v", err)
	}
}

// TestFailureMode_MakeNamedType verifies that `make(MyType)` is
// rejected — only []T and map[K]V types are supported.
func TestFailureMode_MakeNamedType(t *testing.T) {
	src := []byte(`package handlers

type MyMap map[string]float64

func F() {
	m := make(MyMap)
	_ = m
}`)
	_, err := LowerFile(src)
	if err == nil {
		t.Fatal("LowerFile: expected diagnostic for make(MyMap), got nil")
	}
}

// TestFailureMode_MakeWithoutArgs verifies the empty-args form
// records the right diagnostic.
func TestFailureMode_MakeWithoutArgs(t *testing.T) {
	src := []byte(`package handlers

func F() {
	x := make
	_ = x
}`)
	// "x := make" parses as a function-value assignment, not a call;
	// the lowerer will reject it via lowerIdent → OpLocalGet which
	// returns the zero value. The test pins that the case doesn't
	// panic; an actual call without args is syntactically invalid in
	// Go itself so the parser catches it before us.
	_, _ = LowerFile(src)
}

// TestFailureMode_ArrayTypeCastWithFunkyElement verifies that
// `[]float64(s)` (a slice cast that isn't []rune/[]byte) is rejected.
func TestFailureMode_ArrayTypeCastWithFunkyElement(t *testing.T) {
	src := []byte(`package handlers

func F(s string) int {
	return len([]float64(s))
}`)
	_, err := LowerFile(src)
	if err == nil {
		t.Fatal("LowerFile: expected diagnostic for []float64(s), got nil")
	}
	if !strings.Contains(err.Error(), "rune") {
		t.Errorf("diagnostic should suggest rune/byte; got: %v", err)
	}
}

// TestFailureMode_SwitchFallthrough verifies that a switch case with
// `fallthrough` records a diagnostic. The body still lowers (the
// VM stays panic-free) but the author gets a clear pointer.
func TestFailureMode_SwitchFallthrough(t *testing.T) {
	src := []byte(`package handlers

func F(t string) int {
	switch t {
	case "a":
		fallthrough
	case "b":
		return 1
	default:
		return 0
	}
}`)
	_, err := LowerFile(src)
	if err == nil {
		t.Fatal("LowerFile: expected fallthrough diagnostic, got nil")
	}
	if !strings.Contains(err.Error(), "fallthrough") {
		t.Errorf("diagnostic should mention fallthrough; got: %v", err)
	}
}

// TestFailureMode_SliceExprThreeIndex verifies the three-index
// slice form `s[i:j:k]` is rejected (cap is meaningless in the VM).
func TestFailureMode_SliceExprThreeIndex(t *testing.T) {
	src := []byte(`package handlers

func F(s []int) []int {
	return s[1:3:5]
}`)
	_, err := LowerFile(src)
	if err == nil {
		t.Fatal("LowerFile: expected diagnostic for s[i:j:k], got nil")
	}
	if !strings.Contains(err.Error(), "three-index") {
		t.Errorf("diagnostic should mention three-index; got: %v", err)
	}
}

// TestFailureMode_HostCallOnUnboundReceiverIsLoweringClean verifies
// that lowering an OpHostCall succeeds even when no host is bound.
// The runtime diagnostic (`host_unbound`) fires at evaluation time;
// the lowerer doesn't need to know the host bindings.
func TestFailureMode_HostCallOnUnboundReceiverIsLoweringClean(t *testing.T) {
	src := []byte(`package handlers

import "m31labs.dev/gosx/engine/surface"

func F(c *surface.Canvas) {
	c.Pretend()
}`)
	prog, err := LowerFile(src)
	if err != nil {
		t.Fatalf("LowerFile should succeed; got %v", err)
	}
	if prog == nil {
		t.Fatal("expected a non-nil Program")
	}
}
