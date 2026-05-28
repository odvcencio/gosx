// Package surface — VM-side context host receiver bridge (v0.23.1).
//
// ContextHostReceiver is the companion to CanvasHostReceiver: it
// services the `ctx.*` calls a bytecode-lowered surface handler makes,
// most notably `ctx.PropsInto(&props)` which decodes the per-mount
// props JSON into the surface's typed props struct.
//
// # The handoff chain
//
//  1. The hydrate path receives a propsJSON string from the JS layer
//     (sycamore's __gosx_hydrate_engine_surface entry — PR #19).
//  2. Bridge wraps it via NewContextHostReceiver(propsJSON) and binds
//     under "ctx" alongside the canvas receiver bound under "c".
//  3. The handler's `ctx.PropsInto(&props)` lowers to
//     OpHostCall("ctx.PropsInto", [&props]). The VM dispatches into
//     ContextHostReceiver.Call("PropsInto", [target]).
//  4. PropsInto unmarshals the propsJSON and writes each top-level
//     key into target.Fields by reference (Y.C in-place mutation +
//     Y.G eager struct zero-init guarantees target.Fields is non-nil).

package surface

import (
	"encoding/json"
	"fmt"

	"m31labs.dev/gosx/client/vm"
	"m31labs.dev/gosx/island/program"
)

// ContextHostReceiver bridges per-mount surface context state (props,
// in particular) to the vm.HostReceiver interface so bytecode handlers
// can resolve `ctx.PropsInto(&p)` and future ctx.* calls via
// OpHostCall.
//
// Construct one per surface mount via NewContextHostReceiver and bind
// it under the surface author's chosen identifier ("ctx" by
// convention):
//
//	recv := surface.NewContextHostReceiver([]byte(propsJSON))
//	machine.BindHost("ctx", recv)
type ContextHostReceiver struct {
	propsJSON []byte
}

// NewContextHostReceiver wraps the per-mount propsJSON as a
// vm.HostReceiver. propsJSON may be nil or empty — in that case
// PropsInto leaves the target untouched and returns nil (matching the
// Go default-zero-value contract authors expect from
// `var p T; ctx.PropsInto(&p)`).
func NewContextHostReceiver(propsJSON []byte) *ContextHostReceiver {
	return &ContextHostReceiver{propsJSON: propsJSON}
}

// BindContext is the convenience wire-once entry point mirroring
// BindCanvas. Binds a fresh ContextHostReceiver under name
// (conventionally "ctx") on the VM and unbinds it when ctx is torn
// down. ctx may be nil — callers owning their own teardown can omit
// the watcher.
func BindContext(machine *vm.VM, name string, propsJSON []byte, ctx *Context) *ContextHostReceiver {
	recv := NewContextHostReceiver(propsJSON)
	if machine != nil {
		machine.BindHost(name, recv)
	}
	if ctx != nil {
		go func() {
			<-ctx.Done()
			if machine != nil {
				machine.BindHost(name, nil)
			}
		}()
	}
	return recv
}

// Call satisfies vm.HostReceiver. Method name comes from the
// source-level `ctx.<Method>` call as lowered by Y.E's host-call path.
//
// Today only PropsInto is dispatched. Future ctx.* methods (logging,
// time, RNG seeding) add one case each in the switch.
func (r *ContextHostReceiver) Call(method string, args []vm.Value) (vm.Value, error) {
	if method == "PropsInto" {
		return r.propsInto(args)
	}
	return vm.ZeroValue(0), fmt.Errorf("unknown context method %q", method)
}

// propsInto unmarshals propsJSON and writes top-level keys into the
// target ObjectVal's Fields map by reference. Y.E's `&x` pass-through +
// Y.G's eager struct zero-init contract means target.Fields is a
// non-nil map shared by reference with the caller's local — writes
// here propagate to the handler's `props` variable directly.
//
// Behavior:
//   - 0 args / wrong arg count → error (host_call_error diagnostic)
//   - nil target.Fields → error (props must declare a struct type so
//     scanStructTypes registers it; primitive props aren't supported)
//   - empty/nil propsJSON → no-op success (handler sees zero struct,
//     matching `var p T` without any PropsInto call)
//   - malformed propsJSON → error
//   - well-formed → mutates target.Fields in-place, returns zero value
func (r *ContextHostReceiver) propsInto(args []vm.Value) (vm.Value, error) {
	if len(args) != 1 {
		return vm.ZeroValue(0), fmt.Errorf("PropsInto expects 1 arg, got %d", len(args))
	}
	target := args[0]
	if target.Fields == nil {
		return vm.ZeroValue(0), fmt.Errorf("PropsInto target has nil Fields (declare props as a struct type so the lowerer's eager zero-init runs)")
	}
	if len(r.propsJSON) == 0 {
		return vm.ZeroValue(0), nil
	}

	var raw map[string]any
	if err := json.Unmarshal(r.propsJSON, &raw); err != nil {
		return vm.ZeroValue(0), fmt.Errorf("PropsInto: malformed propsJSON: %w", err)
	}

	for key, val := range raw {
		v := jsonToVMValue(val)
		target.Fields[key] = v
		if pascal := pascalCase(key); pascal != key {
			target.Fields[pascal] = v
		}
	}
	return vm.ZeroValue(0), nil
}

// pascalCase upper-cases the first rune of s. Used to map a JSON key
// (lowercase by convention) to the PascalCase Go field name the
// bytecode VM reads through OpFieldGet. Go encoding/json uses
// struct-tag mappings to round-trip lowercase JSON to PascalCase
// fields, but the bytecode VM has no struct-tag awareness — so we
// dual-key the Fields map with both forms here, letting OpFieldGet
// resolve either way.
func pascalCase(s string) string {
	if s == "" {
		return s
	}
	c := s[0]
	if c >= 'a' && c <= 'z' {
		return string(c-32) + s[1:]
	}
	return s
}

// jsonToVMValue converts a decoded JSON value (from encoding/json's
// any-target unmarshal) into the closest vm.Value shape. Recurses for
// arrays and objects so nested struct/slice props materialize as
// ObjectVal/Items chains the surface handler can read with the usual
// OpFieldGet / OpIndex opcodes.
func jsonToVMValue(v any) vm.Value {
	switch x := v.(type) {
	case nil:
		return vm.ZeroValue(0)
	case bool:
		return vm.BoolVal(x)
	case float64:
		return vm.FloatVal(x)
	case string:
		return vm.StringVal(x)
	case []any:
		items := make([]vm.Value, len(x))
		for i, e := range x {
			items[i] = jsonToVMValue(e)
		}
		return vm.Value{Type: program.TypeAny, Items: items}
	case map[string]any:
		fields := make(map[string]vm.Value, len(x)*2)
		for k, val := range x {
			child := jsonToVMValue(val)
			fields[k] = child
			if pascal := pascalCase(k); pascal != k {
				fields[pascal] = child
			}
		}
		return vm.ObjectVal(fields)
	default:
		return vm.ZeroValue(0)
	}
}
