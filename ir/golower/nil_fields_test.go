// Slice Y.G — `&x` pass-through on nil-Fields receivers.
//
// graph_surface.go's Mount handler declares `var props GraphProps`
// then calls `ctx.PropsInto(&props)`. Y.E's `&x` pass-through
// (expr.go's `case token.AND`) lowers `&props` as `OpLocalGet(props)`,
// which reads the local's current Value. Pre-Y.G, OpLocalDecl reserves
// the slot with the bare zero Value{} — Fields map is nil — and the
// host receives a Value-by-value copy whose Fields map is also nil.
// Any host-side `target.Fields[key] = ...` write either panics (writing
// to a nil map) or, with a workaround that allocates the map, lands in
// the host's copy and never propagates back to the caller's local.
//
// Y.G's fix: when lowerDeclStmt sees `var x T` and T is a known struct
// type (via Y.A's scanStructTypes registry), eagerly emit an
// OpComposite zero-init right after the OpLocalDecl so the local
// starts with a non-nil Fields map. Subsequent `&x` reads return a
// Value sharing the same Fields reference (Go map = reference type),
// so host-side mutations propagate naturally without any new opcode
// or VM hook.
//
// This matches Go's `var x T` semantics: "x is the zero value of T
// from the moment of declaration." For struct T, that zero value
// has all-zero fields — which is exactly what an empty Fields map
// gives us under the VM's IndexVal-returns-zero-on-missing-key
// contract.

package golower

import (
	"testing"

	"m31labs.dev/gosx/client/vm"
)

// TestNilFieldsHostPopulatesLocal is the canonical Mount-bootstrap
// scenario: a struct local declared with `var`, passed by `&` to a
// host method, which writes the local's fields and the writes survive
// the host call so subsequent reads from the local see the data.
func TestNilFieldsHostPopulatesLocal(t *testing.T) {
	src := []byte(`package handlers

type GraphProps struct {
	Center string
}

func Mount() string {
	var props GraphProps
	_ = host.PropsInto(&props)
	return props.Center
}`)
	prog, err := LowerFile(src)
	if err != nil {
		t.Fatalf("LowerFile: %v", err)
	}
	handler := findHandler(t, prog.Handlers, "Mount")
	machine := vm.NewVM(prog, nil)

	// HostRecorder doesn't itself populate — write a tiny custom
	// host that pretends to be PropsInto, populating Center.
	machine.BindHost("host", &propsIntoHost{value: "ALPHA"})

	got := machine.EvalWithFrame(handler.Body[0])
	if got.Str != "ALPHA" {
		t.Errorf("props.Center = %q, want \"ALPHA\"\n(host wrote Center to the Fields map but caller didn't observe the write — `&x` pass-through on a nil-Fields receiver dropped the update)", got.Str)
	}
}

// TestNilFieldsZeroInitOnDecl verifies the lower-time eager
// allocation: a `var x T` for a known struct type T results in a
// local whose Fields map exists (even if empty) immediately after
// OpLocalDecl. Without this, the host-side write target is nil and
// the bootstrap pattern fails silently.
func TestNilFieldsZeroInitOnDecl(t *testing.T) {
	src := []byte(`package handlers

type Box struct {
	Name string
}

func F() string {
	var b Box
	b.Name = "set"
	return b.Name
}`)
	prog, err := LowerFile(src)
	if err != nil {
		t.Fatalf("LowerFile: %v", err)
	}
	handler := findHandler(t, prog.Handlers, "F")
	machine := vm.NewVM(prog, nil)
	got := machine.EvalWithFrame(handler.Body[0])
	if got.Str != "set" {
		t.Errorf("F() = %q, want \"set\" (b.Name = \"set\" must land in the local's Fields map even when b was declared via `var b Box`)", got.Str)
	}
}

// propsIntoHost is a tiny HostReceiver that simulates the gosx
// surface.Context.PropsInto semantic: it receives the props Value as
// the (only) argument and writes user-specified data into its Fields
// map. The test asserts that those writes are observable through the
// caller's local — which only works when the local's Fields map was
// allocated BEFORE the call.
type propsIntoHost struct {
	value string
}

func (h *propsIntoHost) Call(method string, args []vm.Value) (vm.Value, error) {
	if method != "PropsInto" {
		return vm.ZeroValue(0), nil
	}
	if len(args) != 1 {
		return vm.ZeroValue(0), nil
	}
	target := args[0]
	if target.Fields == nil {
		// Pre-Y.G this is the failure point — the host has no map
		// to write into, and even allocating one locally would not
		// propagate back to the caller's local.
		return vm.ZeroValue(0), nil
	}
	target.Fields["Center"] = vm.StringVal(h.value)
	target.Fields["Name"] = vm.StringVal(h.value)
	return vm.ZeroValue(0), nil
}
