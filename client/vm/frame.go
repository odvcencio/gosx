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
//
// captured, when non-nil, is a Slice Y.G closure bridge. Reads / writes
// for any name in captured.captured forward through captured.frame
// instead of the local map — that's how Go's capture-by-reference
// semantics are implemented: the closure's body sees and mutates the
// enclosing scope's same `*frame`.
type frame struct {
	locals   map[string]Value
	captured *closureRef
}

// newFrame returns an empty frame ready for OpLocalDecl writes.
func newFrame() *frame {
	return &frame{locals: map[string]Value{}}
}

// newClosureFrame returns a fresh frame that bridges captured-name
// reads/writes through the supplied closureRef. The bridge is name-
// gated (only names in cr.captured forward to cr.frame); local-only
// names (params + body decls) stay in this frame's own map.
func newClosureFrame(cr *closureRef) *frame {
	return &frame{
		locals:   map[string]Value{},
		captured: cr,
	}
}

// has reports whether name has been declared in this frame.
// For closure frames, captured names report as declared whenever the
// captured frame has them, so OpLocalGet hits the captured-bridge
// path instead of producing a missing-frame diagnostic.
func (f *frame) has(name string) bool {
	if f == nil {
		return false
	}
	if _, ok := f.locals[name]; ok {
		return true
	}
	if f.captured != nil && f.captured.captured[name] && f.captured.frame != nil {
		return f.captured.frame.has(name)
	}
	return false
}

// get returns the current value of name and whether it was declared.
// Reads of undeclared locals return the zero Value with ok=false so
// the caller can surface a missing_local diagnostic. For closure
// frames, names in the captured set forward to the captured frame.
func (f *frame) get(name string) (Value, bool) {
	if f == nil {
		return Value{}, false
	}
	// Local-first lookup. If the name was declared in this frame
	// (e.g. a param or a fresh := inside the closure body) the local
	// shadows any captured name, matching Go's lexical scoping.
	if v, ok := f.locals[name]; ok {
		return v, true
	}
	if f.captured != nil && f.captured.captured[name] && f.captured.frame != nil {
		return f.captured.frame.get(name)
	}
	return Value{}, false
}

// set writes value to name. Pre-declares name if OpLocalDecl was
// skipped (e.g. for `x := ...` lowered as a single OpAssign).
// For closure frames, writes to a captured name forward through the
// bridge so the enclosing scope sees the mutation — Go's capture-by-
// reference contract.
func (f *frame) set(name string, value Value) {
	if f == nil {
		return
	}
	// Captured names route through the bridge IFF the closure does
	// NOT have a local of the same name (Go's shadowing rule: a
	// fresh `:=` inside the closure body shadows the captured local
	// at the body's scope).
	if f.captured != nil && f.captured.captured[name] {
		if _, isLocal := f.locals[name]; !isLocal && f.captured.frame != nil {
			f.captured.frame.set(name, value)
			return
		}
	}
	f.locals[name] = value
}

// declare reserves a slot for name with the zero Value, matching Go's
// `var x T` semantics. Subsequent OpAssign / OpLocalSet writes update it.
// For closure frames, an explicit declare ALWAYS allocates a fresh
// local that shadows any captured name — Go's `var` semantics.
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
