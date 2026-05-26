// Stdlib intrinsics for the shared VM. Slice X.B exposes a curated subset
// of Go's standard library to the AST-compiler initiative's lowered
// handlers: math, math/rand, strings, strconv, and sort.Slice. Each
// family registers its functions under qualified names ("math.Sin",
// "strings.Split", ...) so the X.C lowerer can emit OpCall with the
// literal qualified name in Expr.Value and the argument expressions in
// Expr.Operands.
//
// The registry is process-global and read-only after package init. State
// that needs per-VM scoping (rand source seeding, sort comparator
// dispatch) lives on the VM struct, not here.
//
// Adding a new intrinsic family means:
//   1. Create intrinsics_<family>.go with an init() that calls
//      RegisterIntrinsic(name, fn) for each function in the family.
//   2. Add the family to the X.C lowerer's known-stdlib lookup so call
//      expressions resolve correctly.
//   3. Add tests in intrinsics_<family>_test.go that exercise each
//      function via LookupIntrinsic.
//
// Errors from intrinsics surface as structured diagnostics through the
// existing recordExprDiagnostic path and yield the zero value of
// TypeAny, preserving the VM's panic-free contract.

package vm

import (
	"fmt"
	"sync"

	"m31labs.dev/gosx/island/program"
)

// Intrinsic is a stdlib-style function exposed to the VM. The args slice
// is positional — order matches the Go signature. Intrinsics must not
// panic; signal errors via the returned error.
type Intrinsic func(args []Value) (Value, error)

// IntrinsicRegistry maps qualified names ("pkg.Func") to Intrinsic
// implementations. Registration is package-level and happens during
// init(); lookup is safe for concurrent use.
type IntrinsicRegistry struct {
	mu     sync.RWMutex
	byName map[string]Intrinsic
}

// globalIntrinsics is the process-wide registry. Each intrinsics_*.go
// file's init() calls RegisterIntrinsic against this instance.
var globalIntrinsics = &IntrinsicRegistry{byName: map[string]Intrinsic{}}

// Register adds fn under qualifiedName ("math.Sin"). Re-registration
// overwrites; the lowerer never emits duplicate names so this only
// happens in tests that want to stub a function.
func (r *IntrinsicRegistry) Register(qualifiedName string, fn Intrinsic) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.byName[qualifiedName] = fn
}

// Lookup returns the Intrinsic registered under qualifiedName, or
// (nil, false) when no entry exists. The Boolean lets callers
// distinguish "not registered" from "registered as nil" without paying
// the map-zero-value indistinguishability tax.
func (r *IntrinsicRegistry) Lookup(qualifiedName string) (Intrinsic, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	fn, ok := r.byName[qualifiedName]
	return fn, ok
}

// Names returns the set of registered qualified names, sorted, for
// diagnostics and the X.C lowerer's known-stdlib lookup. Cheap enough
// to call once at lowerer init; not on a hot path.
func (r *IntrinsicRegistry) Names() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	names := make([]string, 0, len(r.byName))
	for name := range r.byName {
		names = append(names, name)
	}
	// Sort here so the caller doesn't need its own import.
	sortStrings(names)
	return names
}

// RegisterIntrinsic is a package-level convenience for the init()
// blocks in each intrinsics_*.go file. Keeping the call site terse
// helps the family files stay focused on the function bodies.
func RegisterIntrinsic(qualifiedName string, fn Intrinsic) {
	globalIntrinsics.Register(qualifiedName, fn)
}

// LookupIntrinsic returns the global Intrinsic for qualifiedName. Used
// by VM call evaluation and by tests.
func LookupIntrinsic(qualifiedName string) (Intrinsic, bool) {
	return globalIntrinsics.Lookup(qualifiedName)
}

// IntrinsicNames returns the sorted list of all globally-registered
// intrinsic names. The X.C lowerer calls this once at init to build
// its known-stdlib lookup.
func IntrinsicNames() []string {
	return globalIntrinsics.Names()
}

// callIntrinsic dispatches an OpCall whose Value resolves to a
// registered intrinsic name. Returns (value, true) when the call was
// dispatched (whether or not the intrinsic errored — errors are
// surfaced via diagnostics), or (zero, false) when no intrinsic is
// registered under the name.
func (vm *VM) callIntrinsic(e program.Expr) (Value, bool) {
	fn, ok := LookupIntrinsic(e.Value)
	if !ok {
		return Value{}, false
	}
	args := make([]Value, len(e.Operands))
	for i, op := range e.Operands {
		args[i] = vm.Eval(op)
	}
	result, err := fn(args)
	if err != nil {
		vm.recordExprDiagnostic(
			"intrinsic_error",
			fmt.Sprintf("intrinsic %s: %v", e.Value, err),
			e.Op,
			e.Value,
		)
		return ZeroValue(program.TypeAny), true
	}
	return result, true
}

// sortStrings is a tiny dependency-free sort to avoid pulling in
// "sort" at the registry level (where it would create a cycle once the
// sort intrinsic registers itself).
func sortStrings(ss []string) {
	for i := 1; i < len(ss); i++ {
		j := i
		for j > 0 && ss[j-1] > ss[j] {
			ss[j-1], ss[j] = ss[j], ss[j-1]
			j--
		}
	}
}
