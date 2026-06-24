package motion

import "fmt"

// motionProgram holds a decoded program and its reusable write buffer.
type motionProgram struct {
	tl         *Timeline
	targetRefs []string
	propRefs   []string
	buf        *WriteBuf
}

// Runtime is the host-testable motion runtime. It manages a set of loaded programs
// keyed by integer handles. This is the pure-Go logic that the future WASM exports
// (motionLoad/motionTick) will wrap — no syscall/js here.
//
// TinyGo-clean: no reflect, no encoding/json.
type Runtime struct {
	programs map[int]*motionProgram
	next     int
}

// NewRuntime allocates an empty Runtime.
func NewRuntime() *Runtime {
	return &Runtime{
		programs: make(map[int]*motionProgram),
		next:     1,
	}
}

// Load decodes a wire-format program, stores it under a fresh handle (>=1), and
// allocates a reusable WriteBuf for it. Returns the handle.
func (r *Runtime) Load(irBytes []byte) (handle int, err error) {
	tl, targetRefs, propRefs, err := DecodeProgram(irBytes)
	if err != nil {
		return 0, err
	}
	p := &motionProgram{
		tl:         tl,
		targetRefs: targetRefs,
		propRefs:   propRefs,
		buf:        NewWriteBuf(64),
	}
	handle = r.next
	r.next++
	r.programs[handle] = p
	return handle, nil
}

// Tick evaluates program `handle` at time t, packing writes into `out`.
// Returns the TOTAL number of floats produced; if it exceeds len(out), out holds only
// the first len(out) values (truncated) and the caller should grow the buffer.
// Returns an error for an unknown handle.
//
// Zero-heap-alloc after the first (warmup) tick — the map lookup, Policy value, Eval,
// and copy are all alloc-free once the program's WriteBuf has grown to capacity.
func (r *Runtime) Tick(handle int, t float64, reduced bool, out []float64) (n int, err error) {
	p, ok := r.programs[handle]
	if !ok {
		return 0, fmt.Errorf("motion: unknown handle %d", handle)
	}
	p.buf.Reset()
	Eval(p.tl, t, Policy{ReducedMotion: reduced}, p.buf)
	w := p.buf.Writes()
	copy(out, w)
	return len(w), nil
}

// Unload frees a handle. After Unload, Tick and Refs calls for that handle return errors.
func (r *Runtime) Unload(handle int) {
	delete(r.programs, handle)
}

// TargetRefs returns the id→ref table for the target interning layer.
// Returns (nil, false) for unknown handles.
func (r *Runtime) TargetRefs(handle int) ([]string, bool) {
	p, ok := r.programs[handle]
	if !ok {
		return nil, false
	}
	return p.targetRefs, true
}

// PropRefs returns the id→ref table for the property interning layer.
// Returns (nil, false) for unknown handles.
func (r *Runtime) PropRefs(handle int) ([]string, bool) {
	p, ok := r.programs[handle]
	if !ok {
		return nil, false
	}
	return p.propRefs, true
}
