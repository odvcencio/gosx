// Slice Y.E.2 — Host-receiver dispatch for OpHostCall.
//
// Engine-surface handlers call methods on *surface.Canvas and
// *surface.Context. These objects live host-side (they wrap CSS
// pixels and the browser's CanvasRenderingContext2D — neither can be
// expressed as a pure Value transformation). The VM exposes a binding
// mechanism so the surface bootstrap can hand each VM a concrete
// receiver per source-level identifier ("c", "ctx", ...) and the
// lowered OpHostCall opcodes dispatch into it at evaluation time.
//
// Design choice (Y.E exit report):
//
//   - **Single OpHostCall opcode**, not one-opcode-per-canvas-method.
//     33+ canvas methods would explode the opcode ladder and force a
//     program-wire-format bump every time a method is added. The
//     dispatch table lives in user code (the HostReceiver
//     implementation), where it belongs.
//
//   - **Per-VM binding**, not a global registry. Each engine-surface
//     instance has its own canvas/context. A global registry would
//     force the bootstrap to thread a "which surface am I" parameter
//     into every host method, which is exactly what BindHost avoids.
//
//   - **Receiver-name addressing**, not type-name addressing. The
//     lowered call carries `<receiver>.<MethodName>` so the dispatch
//     respects the author's source identifier. Two parameters of the
//     same type bind independently if needed (rare but legal).
//
// Discrimination from OpCall (stdlib intrinsics): OpCall hits the
// global intrinsics registry (math.Sin, strings.Join — pure functions
// from the standard library). OpHostCall hits the per-VM host
// receivers (canvas/context/etc. — capability-bearing objects from
// the surface bootstrap). The lowerer disambiguates by checking the
// receiver identifier against the file's import list: imports →
// OpCall; non-imports → OpHostCall.

package vm

import (
	"fmt"
	"strings"

	"m31labs.dev/gosx/island/program"
)

// HostReceiver is the contract a host-bound object must satisfy to
// receive OpHostCall dispatches. The single `Call` method takes the
// source-level method name and the evaluated argument list; the
// receiver is free to look up the method in any way it likes
// (reflect-based dispatch, hand-rolled switch, JS-bridge passthrough).
//
// Methods that don't return a value should return the zero Value;
// errors surface as `host_call_error` diagnostics on the VM (the
// receiver itself stays panic-free per the engine-surface contract).
type HostReceiver interface {
	Call(method string, args []Value) (Value, error)
}

// BindHost registers a host receiver under the given identifier. The
// identifier is the source-level parameter name the author used
// (typically "c" for the canvas and "ctx" for the context). The
// lowerer emits OpHostCall opcodes whose Value is "<identifier>.<Method>";
// the VM looks up the identifier in the binding table at dispatch
// time.
//
// Re-binding the same name replaces the previous receiver — useful
// for tests that swap a recorder in mid-evaluation. A nil receiver
// clears the binding.
func (vm *VM) BindHost(name string, recv HostReceiver) {
	vm.hostsMu.Lock()
	defer vm.hostsMu.Unlock()
	if vm.hosts == nil {
		vm.hosts = make(map[string]HostReceiver)
	}
	if recv == nil {
		delete(vm.hosts, name)
		return
	}
	vm.hosts[name] = recv
}

// LookupHost returns the host receiver bound to name (mainly for
// tests). The second return is false when no binding exists.
func (vm *VM) LookupHost(name string) (HostReceiver, bool) {
	vm.hostsMu.RLock()
	defer vm.hostsMu.RUnlock()
	recv, ok := vm.hosts[name]
	return recv, ok
}

// evalHostCallExpr dispatches OpHostCall into the bound host receiver.
// The Value carries "<receiver>.<MethodName>"; we split on the first
// dot to recover the binding key and the method name. Args are
// evaluated left-to-right before the host call fires.
//
// Errors from the host receiver are recorded as diagnostics and
// surface as the zero Any value — the VM stays panic-free even when
// the host receiver misbehaves. Unbound receivers (no BindHost call
// for the given identifier) record a `host_unbound` diagnostic and
// likewise yield the zero value so handler bodies continue to
// evaluate the rest of their statements.
func (vm *VM) evalHostCallExpr(e program.Expr) (Value, bool) {
	if e.Op != program.OpHostCall {
		return Value{}, false
	}
	dot := strings.IndexByte(e.Value, '.')
	if dot <= 0 || dot == len(e.Value)-1 {
		vm.recordExprDiagnostic(
			"host_call_invalid",
			fmt.Sprintf("OpHostCall Value %q must be \"<receiver>.<Method>\"", e.Value),
			e.Op,
			e.Value,
		)
		return ZeroValue(program.TypeAny), true
	}
	recvName := e.Value[:dot]
	methodName := e.Value[dot+1:]
	vm.hostsMu.RLock()
	recv, ok := vm.hosts[recvName]
	vm.hostsMu.RUnlock()
	if !ok {
		vm.recordExprDiagnostic(
			"host_unbound",
			fmt.Sprintf("OpHostCall: no host bound under name %q (method %q)", recvName, methodName),
			e.Op,
			e.Value,
		)
		return ZeroValue(program.TypeAny), true
	}
	args := make([]Value, len(e.Operands))
	for i, op := range e.Operands {
		args[i] = vm.Eval(op)
	}
	result, err := recv.Call(methodName, args)
	if err != nil {
		vm.recordExprDiagnostic(
			"host_call_error",
			fmt.Sprintf("host call %s: %v", e.Value, err),
			e.Op,
			e.Value,
		)
		return ZeroValue(program.TypeAny), true
	}
	return result, true
}

// --- Test helper: HostRecorder ---

// HostRecorder is a HostReceiver that records every call it receives
// (method name + evaluated args). Tests use it to assert that the
// lowerer emitted the right OpHostCall sequence and that the VM
// evaluated arguments in source order. Not part of the production
// dispatch path — production surfaces bind a real *surface.Canvas
// adapter that forwards to the host CanvasImpl.
type HostRecorder struct {
	Calls []HostCall
	// ReturnFor, if set, returns the configured Value for the given
	// method name. Unset methods return the zero Value. Lets tests
	// stub query-style methods (Width, Height, PropsInto) without
	// inventing a per-test receiver implementation.
	ReturnFor map[string]Value
}

// HostCall is one entry in HostRecorder.Calls.
type HostCall struct {
	Method string
	Args   []Value
}

// NewHostRecorder constructs an empty recorder.
func NewHostRecorder() *HostRecorder {
	return &HostRecorder{}
}

// Call satisfies HostReceiver. Records the call then either returns
// the configured ReturnFor value or the zero Value.
func (r *HostRecorder) Call(method string, args []Value) (Value, error) {
	// Defensive copy so callers can't mutate the recorder's history.
	argsCopy := make([]Value, len(args))
	copy(argsCopy, args)
	r.Calls = append(r.Calls, HostCall{Method: method, Args: argsCopy})
	if r.ReturnFor != nil {
		if v, ok := r.ReturnFor[method]; ok {
			return v, nil
		}
	}
	return ZeroValue(program.TypeAny), nil
}

// Reset clears the call history. Useful for tests that exercise
// multiple phases against the same recorder.
func (r *HostRecorder) Reset() {
	r.Calls = nil
}
