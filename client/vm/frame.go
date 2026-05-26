// Frames hold per-evaluation locals introduced by Slice X.A's statement
// sequencing opcodes (OpSeq + OpLocalDecl + OpLocalGet/OpLocalSet +
// OpAssign). The shared-VM evaluator stays panic-free; missing-local
// reads produce a structured diagnostic and a zero value, matching the
// existing missing-signal behavior.
//
// A frame is pushed implicitly on the first OpLocalDecl encountered by
// VM.Eval and popped when that evaluation returns. Nested OpSeq blocks
// do NOT introduce new frames — Go's lexical scoping for `{ ... }`
// statement bodies is intentionally flattened to the calling handler,
// matching how the lowerer (Slice X.C) chains statements with OpSeq.
// Locals declared inside a nested block remain visible to subsequent
// siblings in the enclosing OpSeq; this matches Go's `if x := ...; cond`
// pattern where x is in scope for the if/else body but not after.
// Block-scoped narrowing is handled by the lowerer (X.C) rewriting the
// emitted local names so collisions never occur.

package vm

// frame is a per-evaluation locals table. The map keeps the
// implementation simple; switching to a slot-index encoding is a future
// optimization once the lowerer emits stable indices.
type frame struct {
	locals map[string]Value
}

// newFrame returns an empty frame ready for OpLocalDecl writes.
func newFrame() *frame {
	return &frame{locals: map[string]Value{}}
}

// has reports whether name has been declared in this frame.
func (f *frame) has(name string) bool {
	if f == nil {
		return false
	}
	_, ok := f.locals[name]
	return ok
}

// get returns the current value of name and whether it was declared.
// Reads of undeclared locals return the zero Value with ok=false so
// the caller can surface a missing_local diagnostic.
func (f *frame) get(name string) (Value, bool) {
	if f == nil {
		return Value{}, false
	}
	v, ok := f.locals[name]
	return v, ok
}

// set writes value to name. Pre-declares name if OpLocalDecl was
// skipped (e.g. for `x := ...` lowered as a single OpAssign).
func (f *frame) set(name string, value Value) {
	if f == nil {
		return
	}
	f.locals[name] = value
}

// declare reserves a slot for name with the zero Value, matching Go's
// `var x T` semantics. Subsequent OpAssign / OpLocalSet writes update it.
func (f *frame) declare(name string) {
	if f == nil {
		return
	}
	if _, ok := f.locals[name]; ok {
		// Re-declaration in the same frame is a no-op — the lowerer
		// guarantees unique names per block, and overwriting an existing
		// slot would silently zero a still-live local in pathological
		// cases.
		return
	}
	f.locals[name] = Value{}
}
