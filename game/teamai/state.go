package teamai

import (
	"encoding/binary"
	"errors"
	"fmt"

	"m31labs.dev/gosx/det"
)

// stateVersion is the version byte of the executor snapshot layout.
const stateVersion = 1

// agentRecordSize is the encoded size of one agent: see appendAgent.
const agentRecordSize = 2*3 + 1 + 1 + 8 + 4*2 + 2*2 + 4 + 3*2 + 8*2 + 8*MemorySlots +
	2 + 1 + 4*2 + 8*2*2 + 8 + 8*2*2 + 8 + 8*2

// StateSize returns the size in bytes of AppendState's output for agents
// agents and teams teams.
func StateSize(agents, teams int) int {
	return 1 + 2 + 2 + 2*teams + agents*agentRecordSize
}

// AppendState appends the executor's state to dst in a fixed little-endian
// layout and returns the extended slice: every float as its IEEE 754 bits, so
// two executors in the same state give the same bytes on every platform.
// Include it in the simulation snapshot that prediction and replay restore.
// AppendState does not allocate when dst has StateSize bytes of room.
func (e *Executor) AppendState(dst []byte) []byte {
	le := binary.LittleEndian
	dst = append(dst, stateVersion)
	dst = le.AppendUint16(dst, uint16(len(e.agents)))
	dst = le.AppendUint16(dst, uint16(len(e.teams)))
	for i := range e.teams {
		dst = le.AppendUint16(dst, uint16(e.teams[i].formation))
	}
	for i := range e.agents {
		dst = appendAgent(dst, &e.agents[i])
	}
	return dst
}

func appendF(dst []byte, f float64) []byte {
	return binary.LittleEndian.AppendUint64(dst, det.Float64bits(f))
}

func appendV(dst []byte, v Vec2) []byte { return appendF(appendF(dst, v.X), v.Y) }

func appendRef(dst []byte, r Ref) []byte {
	return binary.LittleEndian.AppendUint16(append(dst, byte(r.Kind)), uint16(r.Index))
}

func appendAgent(dst []byte, st *agentState) []byte {
	le := binary.LittleEndian
	dst = le.AppendUint16(dst, uint16(st.assignment))
	dst = le.AppendUint16(dst, uint16(st.step))
	dst = le.AppendUint16(dst, uint16(st.slot))
	var flags byte
	if st.begun {
		flags |= 1
	}
	if st.contact {
		flags |= 2
	}
	dst = append(dst, flags, byte(st.pending))
	dst = appendF(dst, st.sign)
	dst = le.AppendUint32(dst, st.stepTicks)
	dst = le.AppendUint32(dst, st.assignTicks)
	dst = le.AppendUint16(dst, st.pendingTicks)
	dst = le.AppendUint16(dst, st.fired)
	dst = le.AppendUint32(dst, st.marks)
	dst = appendRef(dst, st.target)
	dst = appendRef(dst, st.assigned)
	dst = appendV(dst, st.point)
	for _, m := range st.mem {
		dst = appendF(dst, m)
	}
	p := &st.path
	dst = le.AppendUint16(dst, uint16(p.seg))
	var pb byte
	if p.begun {
		pb = 1
	}
	dst = append(dst, pb)
	dst = le.AppendUint32(dst, p.ticks)
	dst = le.AppendUint32(dst, p.segTicks)
	dst = appendV(dst, p.origin)
	dst = appendV(dst, p.fwd)
	dst = appendF(dst, p.sign)
	dst = appendV(dst, p.start)
	dst = appendV(dst, p.end)
	dst = appendF(dst, p.progress)
	dst = appendV(dst, p.coast)
	return dst
}

// stateReader decodes the fixed layout.
type stateReader struct {
	b   []byte
	bad bool
}

func (r *stateReader) take(n int) []byte {
	if len(r.b) < n {
		r.bad = true
		return make([]byte, n)
	}
	out := r.b[:n]
	r.b = r.b[n:]
	return out
}

func (r *stateReader) u8() byte    { return r.take(1)[0] }
func (r *stateReader) u16() uint16 { return binary.LittleEndian.Uint16(r.take(2)) }
func (r *stateReader) u32() uint32 { return binary.LittleEndian.Uint32(r.take(4)) }
func (r *stateReader) f() float64 {
	v := det.Float64frombits(binary.LittleEndian.Uint64(r.take(8)))
	if !finite(v) {
		r.bad = true
	}
	return v
}
func (r *stateReader) v() Vec2 { x := r.f(); return Vec2{X: x, Y: r.f()} }
func (r *stateReader) ref() Ref {
	k := RefKind(r.u8())
	if k > RefLandmark {
		r.bad = true
	}
	return Ref{Kind: k, Index: int16(r.u16())}
}

// ErrState reports a snapshot LoadState cannot use.
var ErrState = errors.New("teamai: bad executor state")

// LoadState restores state written by AppendState. It checks the layout and
// that every index fits the executor's Book, and on error leaves the executor
// unchanged. It does not allocate when the snapshot has the agent and team
// counts of the last Reset; otherwise it resizes the executor.
func (e *Executor) LoadState(src []byte) error {
	r := stateReader{b: src}
	if v := r.u8(); v != stateVersion {
		return fmt.Errorf("%w: version %d, want %d", ErrState, v, stateVersion)
	}
	agents, teams := int(r.u16()), int(r.u16())
	if len(src) != StateSize(agents, teams) {
		return fmt.Errorf("%w: %d bytes for %d agents and %d teams, want %d", ErrState, len(src), agents, teams, StateSize(agents, teams))
	}
	// Validate everything before changing the executor.
	check := r
	for i := 0; i < teams; i++ {
		if f := FormationID(int16(check.u16())); f != NoFormation && e.book.Formation(f) == nil {
			return fmt.Errorf("%w: team %d formation %d", ErrState, i, f)
		}
	}
	var st agentState
	for i := 0; i < agents; i++ {
		readAgent(&check, &st)
		if check.bad {
			return fmt.Errorf("%w: agent %d", ErrState, i)
		}
		if err := e.checkAgent(&st); err != nil {
			return fmt.Errorf("%w: agent %d: %v", ErrState, i, err)
		}
	}
	if agents != len(e.agents) || teams != len(e.teams) {
		e.Reset(agents, teams)
	}
	for i := 0; i < teams; i++ {
		e.teams[i].formation = FormationID(int16(r.u16()))
		e.teams[i].resolved = false
	}
	for i := 0; i < agents; i++ {
		readAgent(&r, &e.agents[i])
	}
	return nil
}

func readAgent(r *stateReader, st *agentState) {
	st.assignment = int16(r.u16())
	st.step = int16(r.u16())
	st.slot = int16(r.u16())
	flags := r.u8()
	st.begun, st.contact = flags&1 != 0, flags&2 != 0
	st.pending = int8(r.u8())
	st.sign = r.f()
	st.stepTicks = r.u32()
	st.assignTicks = r.u32()
	st.pendingTicks = r.u16()
	st.fired = r.u16()
	st.marks = r.u32()
	st.target = r.ref()
	st.assigned = r.ref()
	st.point = r.v()
	for k := range st.mem {
		st.mem[k] = r.f()
	}
	p := &st.path
	p.seg = int16(r.u16())
	p.begun = r.u8() != 0
	p.ticks = r.u32()
	p.segTicks = r.u32()
	p.origin = r.v()
	p.fwd = r.v()
	p.sign = r.f()
	p.start = r.v()
	p.end = r.v()
	p.progress = r.f()
	p.coast = r.v()
}

func (e *Executor) checkAgent(st *agentState) error {
	if st.sign != 1 && st.sign != -1 {
		return fmt.Errorf("side %v", st.sign)
	}
	if st.slot < -1 || st.slot >= maxSlots {
		return fmt.Errorf("slot %d", st.slot)
	}
	if st.pending < -1 || int(st.pending) >= maxInterrupts {
		return fmt.Errorf("pending interrupt %d", st.pending)
	}
	if st.assignment == -1 {
		return nil
	}
	a := e.book.Assignment(AssignmentID(st.assignment))
	if a == nil {
		return fmt.Errorf("assignment %d", st.assignment)
	}
	if st.step < -1 || int(st.step) >= len(a.steps) {
		return fmt.Errorf("step %d of %d", st.step, len(a.steps))
	}
	if st.step >= 0 {
		if s := &a.steps[st.step]; s.kind == StepPath {
			if p := e.book.Path(s.path); p == nil || st.path.seg < -1 || int(st.path.seg) >= len(p.segs) {
				return fmt.Errorf("path segment %d", st.path.seg)
			}
		}
	}
	return nil
}

// Hash returns the FNV-1a hash of AppendState's output. Two executors with the
// same state have the same hash on every platform. It does not allocate after
// the first call.
func (e *Executor) Hash() uint64 {
	e.buf = e.AppendState(e.buf[:0])
	h := uint64(14695981039346656037)
	for _, c := range e.buf {
		h ^= uint64(c)
		h *= 1099511628211
	}
	return h
}
