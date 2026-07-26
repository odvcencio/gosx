package surface

import (
	"strings"
	"testing"

	"m31labs.dev/gosx/client/vm"
)

func TestContextHostReceiver_PropsIntoPopulatesFields(t *testing.T) {
	propsJSON := `{"Name":"hello","Count":42,"Active":true}`
	recv := NewContextHostReceiver([]byte(propsJSON))

	// Mirror Y.G's eager zero-init: a struct local arrives with non-nil
	// Fields but no entries. PropsInto must populate it by reference.
	target := vm.ObjectVal(map[string]vm.Value{})

	out, err := recv.Call("PropsInto", []vm.Value{target})
	if err != nil {
		t.Fatalf("PropsInto: %v", err)
	}
	if out.Text() != "" || out.Number() != 0 {
		t.Fatalf("PropsInto should return zero Value, got %+v", out)
	}
	if got := target.Map()["Name"].Text(); got != "hello" {
		t.Fatalf("Name = %q, want %q", got, "hello")
	}
	if got := target.Map()["Count"].Number(); got != 42 {
		t.Fatalf("Count = %v, want 42", got)
	}
	if got := target.Map()["Active"].Truth(); got != true {
		t.Fatalf("Active = %v, want true", got)
	}
}

func TestContextHostReceiver_PropsIntoHandlesNestedArrays(t *testing.T) {
	propsJSON := `{"Nodes":[{"ID":"a"},{"ID":"b"}],"Tags":["x","y","z"]}`
	recv := NewContextHostReceiver([]byte(propsJSON))
	target := vm.ObjectVal(map[string]vm.Value{})

	if _, err := recv.Call("PropsInto", []vm.Value{target}); err != nil {
		t.Fatalf("PropsInto: %v", err)
	}

	nodes := target.Map()["Nodes"]
	if len(nodes.List()) != 2 {
		t.Fatalf("Nodes length = %d, want 2", len(nodes.List()))
	}
	if got := nodes.List()[0].Map()["ID"].Text(); got != "a" {
		t.Fatalf("Nodes[0].ID = %q, want %q", got, "a")
	}
	if got := nodes.List()[1].Map()["ID"].Text(); got != "b" {
		t.Fatalf("Nodes[1].ID = %q, want %q", got, "b")
	}

	tags := target.Map()["Tags"]
	if len(tags.List()) != 3 {
		t.Fatalf("Tags length = %d, want 3", len(tags.List()))
	}
	if tags.List()[2].Text() != "z" {
		t.Fatalf("Tags[2] = %q, want %q", tags.List()[2].Text(), "z")
	}
}

func TestContextHostReceiver_PropsIntoEmptyJSONIsNoOp(t *testing.T) {
	recv := NewContextHostReceiver(nil)
	target := vm.ObjectVal(map[string]vm.Value{})

	if _, err := recv.Call("PropsInto", []vm.Value{target}); err != nil {
		t.Fatalf("empty propsJSON should be no-op success, got: %v", err)
	}
	if len(target.Map()) != 0 {
		t.Fatalf("target Fields should stay empty, got %d entries", len(target.Map()))
	}
}

func TestContextHostReceiver_PropsIntoRejectsMalformedJSON(t *testing.T) {
	recv := NewContextHostReceiver([]byte("{not json"))
	target := vm.ObjectVal(map[string]vm.Value{})

	_, err := recv.Call("PropsInto", []vm.Value{target})
	if err == nil {
		t.Fatalf("malformed propsJSON should error, got nil")
	}
	if !strings.Contains(err.Error(), "malformed") {
		t.Fatalf("error should mention malformed, got %q", err.Error())
	}
}

func TestContextHostReceiver_PropsIntoRejectsNilFieldsTarget(t *testing.T) {
	recv := NewContextHostReceiver([]byte(`{"X":1}`))
	target := vm.Value{} // no Fields map — simulates a primitive arg by mistake

	_, err := recv.Call("PropsInto", []vm.Value{target})
	if err == nil {
		t.Fatalf("nil-Fields target should error, got nil")
	}
	if !strings.Contains(err.Error(), "nil Fields") {
		t.Fatalf("error should mention nil Fields, got %q", err.Error())
	}
}

func TestContextHostReceiver_PropsIntoRejectsWrongArgCount(t *testing.T) {
	recv := NewContextHostReceiver([]byte(`{"X":1}`))

	_, err := recv.Call("PropsInto", nil)
	if err == nil {
		t.Fatalf("0 args should error")
	}

	_, err = recv.Call("PropsInto", []vm.Value{vm.ObjectVal(nil), vm.ObjectVal(nil)})
	if err == nil {
		t.Fatalf("2 args should error")
	}
}

func TestContextHostReceiver_UnknownMethodRejects(t *testing.T) {
	recv := NewContextHostReceiver(nil)

	_, err := recv.Call("BogusMethod", nil)
	if err == nil {
		t.Fatalf("unknown method should error")
	}
	if !strings.Contains(err.Error(), "unknown") {
		t.Fatalf("error should mention unknown, got %q", err.Error())
	}
}

func TestContextHostReceiver_MutationPropagatesByReference(t *testing.T) {
	// Critical: PropsInto mutates target.Fields in place. The caller's
	// Value (passed by Go value semantics) must see the writes because
	// Fields is a reference type. This pins Y.C's in-place-mutation
	// contract — if it ever regresses, hyphae's graph_surface.go Mount
	// will see empty props and the whole bytecode hydration arc breaks.
	recv := NewContextHostReceiver([]byte(`{"NodeCount":7}`))

	original := vm.ObjectVal(map[string]vm.Value{})
	copyOfOriginal := original // Go value copy of the Value struct

	if _, err := recv.Call("PropsInto", []vm.Value{copyOfOriginal}); err != nil {
		t.Fatalf("PropsInto: %v", err)
	}

	// The mutation went through the shared Fields map, so the original
	// sees it too.
	if got := original.Map()["NodeCount"].Number(); got != 7 {
		t.Fatalf("by-reference propagation failed: original NodeCount = %v, want 7", got)
	}
}
