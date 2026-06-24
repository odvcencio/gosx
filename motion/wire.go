package motion

// Flat little-endian binary serialization of MotionIR for crossing the WASM↔JS
// boundary.
//
// TinyGo-clean: NO reflect, NO encoding/json. Only encoding/binary's reflect-free
// bit-shift helpers (PutUint*/Uint*) plus math (Float64bits/frombits) and fmt for
// error messages. binary.Read/Write (struct-reflect) are NEVER used.
//
// All integers are little-endian. Layout is fully self-describing via leading
// counts/lengths so decode is a defensive exact mirror of encode.

import (
	"encoding/binary"
	"fmt"
	"math"
)

// wireMagic prefixes every encoded program. The current version is "MOT3"
// (adds GenOscillator generator fields). "MOT2" (cubicspline tangents) and
// "MOT1" (no tangents) are still accepted on decode for backward compatibility.
var wireMagic = [4]byte{'M', 'O', 'T', '3'}

// wireMagicV2 is the magic that introduced per-key cubicspline tangents but no
// oscillator generator fields.
var wireMagicV2 = [4]byte{'M', 'O', 'T', '2'}

// wireMagicV1 is the legacy magic with no per-key tangent fields.
var wireMagicV1 = [4]byte{'M', 'O', 'T', '1'}

// ---------------------------------------------------------------------------
// Encoder primitives
// ---------------------------------------------------------------------------

// putU8 appends a single byte.
func putU8(b []byte, v uint8) []byte { return append(b, v) }

// putBool appends a bool as one byte (0/1).
func putBool(b []byte, v bool) []byte {
	if v {
		return append(b, 1)
	}
	return append(b, 0)
}

// putU32 appends a uint32 little-endian.
func putU32(b []byte, v uint32) []byte {
	var tmp [4]byte
	binary.LittleEndian.PutUint32(tmp[:], v)
	return append(b, tmp[:]...)
}

// putI32 appends an int32 (from int) little-endian.
// Track.TargetID, PropID, and Loop are serialized as int32; sequential interner
// IDs are far below the int32 ceiling (~2 billion), so truncation never occurs.
func putI32(b []byte, v int) []byte {
	return putU32(b, uint32(int32(v)))
}

// putF64 appends a float64 as its IEEE-754 bit pattern, little-endian.
func putF64(b []byte, v float64) []byte {
	var tmp [8]byte
	binary.LittleEndian.PutUint64(tmp[:], math.Float64bits(v))
	return append(b, tmp[:]...)
}

// putString appends uint32 length + raw UTF-8 bytes.
func putString(b []byte, s string) []byte {
	b = putU32(b, uint32(len(s)))
	return append(b, s...)
}

// putStringList appends uint32 count + each string.
func putStringList(b []byte, list []string) []byte {
	b = putU32(b, uint32(len(list)))
	for _, s := range list {
		b = putString(b, s)
	}
	return b
}

// putFloatSlice appends uint32 count + each float64.
func putFloatSlice(b []byte, fs []float64) []byte {
	b = putU32(b, uint32(len(fs)))
	for _, f := range fs {
		b = putF64(b, f)
	}
	return b
}

// putFixed3 appends exactly 3 float64 (no count).
func putFixed3(b []byte, fs [3]float64) []byte {
	b = putF64(b, fs[0])
	b = putF64(b, fs[1])
	return putF64(b, fs[2])
}

// putFixed4 appends exactly 4 float64 (no count).
func putFixed4(b []byte, fs [4]float64) []byte {
	b = putF64(b, fs[0])
	b = putF64(b, fs[1])
	b = putF64(b, fs[2])
	return putF64(b, fs[3])
}

// putValue appends uint8 arity + 4 float64 (always 4, trailing zeros included).
func putValue(b []byte, v Value) []byte {
	b = putU8(b, uint8(v.Arity))
	for i := 0; i < 4; i++ {
		b = putF64(b, v.F[i])
	}
	return b
}

func putEase(b []byte, e Ease) []byte {
	b = putU8(b, uint8(e.Kind))
	return putFloatSlice(b, e.Args)
}

func putSpring(b []byte, s Spring) []byte {
	b = putF64(b, s.Mass)
	b = putF64(b, s.Stiffness)
	b = putF64(b, s.Damping)
	return putF64(b, s.Velocity)
}

func putTarget(b []byte, t Target) []byte {
	b = putU8(b, uint8(t.Kind))
	return putString(b, t.Ref)
}

func putPosition(b []byte, p Position) []byte {
	b = putU8(b, uint8(p.Kind))
	b = putF64(b, p.Val)
	return putString(b, p.Label)
}

// putOptValue appends a presence byte and, when present, the Value.
func putOptValue(b []byte, v *Value) []byte {
	if v == nil {
		return putBool(b, false)
	}
	b = putBool(b, true)
	return putValue(b, *v)
}

func putKey(b []byte, k Key) []byte {
	b = putF64(b, k.T)
	b = putValue(b, k.Value)
	// optional Ease: presence byte
	if k.Ease == nil {
		b = putBool(b, false)
	} else {
		b = putBool(b, true)
		b = putEase(b, *k.Ease)
	}
	// optional cubicspline tangents (MOT2): in-tangent then out-tangent, each
	// with its own presence byte.
	b = putOptValue(b, k.InTangent)
	return putOptValue(b, k.OutTangent)
}

func putGenerator(b []byte, g *Generator) []byte {
	// presence byte
	if g == nil {
		return putBool(b, false)
	}
	b = putBool(b, true)
	b = putU8(b, uint8(g.Kind))
	b = putValue(b, g.Base)
	b = putFixed3(b, g.Spin)
	b = putSpring(b, g.Spring)
	b = putFixed3(b, g.Drift)
	b = putFixed3(b, g.DriftSpeed)
	b = putFixed3(b, g.DriftPhase)
	// MOT3: GenOscillator fields (arity + 4 per-component arrays).
	b = putU8(b, uint8(g.OscArity))
	b = putFixed4(b, g.OscBase)
	b = putFixed4(b, g.OscAmp)
	b = putFixed4(b, g.OscFreq)
	return putFixed4(b, g.OscPhase)
}

func putTrack(b []byte, tr *Track) []byte {
	b = putTarget(b, tr.Target)
	b = putString(b, tr.Prop)
	// Keys
	b = putU32(b, uint32(len(tr.Keys)))
	for i := range tr.Keys {
		b = putKey(b, tr.Keys[i])
	}
	// optional Gen (own presence byte)
	b = putGenerator(b, tr.Gen)
	b = putU8(b, uint8(tr.Interp))
	b = putEase(b, tr.Ease)
	b = putI32(b, tr.TargetID)
	return putI32(b, tr.PropID)
}

func putPositioned(b []byte, p Positioned) []byte {
	b = putPosition(b, p.At)
	// discriminator: 0=Track, 1=Sub
	if p.Sub != nil {
		b = putU8(b, 1)
		return putTimeline(b, p.Sub)
	}
	b = putU8(b, 0)
	// Guard: if Track is nil (neither Track nor Sub set), encode a zero-value
	// Track{} so encode/decode stays symmetric and never panics.
	tr := p.Track
	if tr == nil {
		tr = &Track{}
	}
	return putTrack(b, tr)
}

func putTimeline(b []byte, tl *Timeline) []byte {
	b = putString(b, tl.ID)
	b = putU32(b, uint32(len(tl.Children)))
	for i := range tl.Children {
		b = putPositioned(b, tl.Children[i])
	}
	b = putI32(b, int(tl.Loop))
	b = putBool(b, tl.Alternate)
	b = putF64(b, tl.Speed)
	return putBool(b, tl.Autoplay)
}

// EncodeProgram serializes a timeline plus the interner ref tables (id->ref) into
// a flat binary blob.
//
// Layout: magic "MOT1" (4 bytes) | targetRefs (string list) | propRefs (string
// list) | timeline.
func EncodeProgram(tl *Timeline, targetRefs, propRefs []string) []byte {
	b := make([]byte, 0, 256)
	b = append(b, wireMagic[:]...)
	b = putStringList(b, targetRefs)
	b = putStringList(b, propRefs)
	if tl == nil {
		tl = &Timeline{}
	}
	return putTimeline(b, tl)
}

// ---------------------------------------------------------------------------
// Decoder
// ---------------------------------------------------------------------------

// reader is a defensive cursor over an encoded blob. Every read is bounds-checked;
// on truncation a wrapped error is returned and the cursor is left so subsequent
// reads keep failing rather than panicking.
type reader struct {
	b   []byte
	off int
	err error
	// version is the wire version derived from the magic: 1 for "MOT1" (no
	// per-key tangents), 2 for "MOT2" (cubicspline tangents present), 3 for
	// "MOT3" (GenOscillator generator fields present).
	version int
}

func (r *reader) fail(what string) {
	if r.err == nil {
		r.err = fmt.Errorf("motion wire: truncated at %s (off=%d len=%d)", what, r.off, len(r.b))
	}
}

func (r *reader) need(n int, what string) bool {
	if r.err != nil {
		return false
	}
	if n < 0 || r.off+n > len(r.b) {
		r.fail(what)
		return false
	}
	return true
}

func (r *reader) u8(what string) uint8 {
	if !r.need(1, what) {
		return 0
	}
	v := r.b[r.off]
	r.off++
	return v
}

func (r *reader) boolean(what string) bool {
	return r.u8(what) != 0
}

func (r *reader) u32(what string) uint32 {
	if !r.need(4, what) {
		return 0
	}
	v := binary.LittleEndian.Uint32(r.b[r.off : r.off+4])
	r.off += 4
	return v
}

func (r *reader) i32(what string) int {
	return int(int32(r.u32(what)))
}

func (r *reader) f64(what string) float64 {
	if !r.need(8, what) {
		return 0
	}
	v := math.Float64frombits(binary.LittleEndian.Uint64(r.b[r.off : r.off+8]))
	r.off += 8
	return v
}

func (r *reader) str(what string) string {
	n := r.u32(what)
	if r.err != nil {
		return ""
	}
	// guard against absurd lengths before slicing.
	if !r.need(int(n), what) {
		return ""
	}
	s := string(r.b[r.off : r.off+int(n)])
	r.off += int(n)
	return s
}

func (r *reader) stringList(what string) []string {
	n := r.u32(what)
	if r.err != nil {
		return nil
	}
	// Sanity bound: each string costs >=4 bytes (its length prefix), so a count
	// larger than the remaining bytes is impossible — reject early to avoid huge
	// allocations on garbage input.
	if int(n) > len(r.b)-r.off {
		r.fail(what + " count")
		return nil
	}
	if n == 0 {
		return nil
	}
	out := make([]string, n)
	for i := range out {
		out[i] = r.str(what)
		if r.err != nil {
			return nil
		}
	}
	return out
}

func (r *reader) floatSlice(what string) []float64 {
	n := r.u32(what)
	if r.err != nil {
		return nil
	}
	// each float costs 8 bytes; reject impossible counts early.
	if int(n) > (len(r.b)-r.off)/8 {
		r.fail(what + " count")
		return nil
	}
	if n == 0 {
		return nil
	}
	out := make([]float64, n)
	for i := range out {
		out[i] = r.f64(what)
		if r.err != nil {
			return nil
		}
	}
	return out
}

func (r *reader) fixed3(what string) [3]float64 {
	var a [3]float64
	a[0] = r.f64(what)
	a[1] = r.f64(what)
	a[2] = r.f64(what)
	return a
}

func (r *reader) fixed4(what string) [4]float64 {
	var a [4]float64
	a[0] = r.f64(what)
	a[1] = r.f64(what)
	a[2] = r.f64(what)
	a[3] = r.f64(what)
	return a
}

func (r *reader) value(what string) Value {
	var v Value
	v.Arity = ValueArity(r.u8(what))
	for i := 0; i < 4; i++ {
		v.F[i] = r.f64(what)
	}
	return v
}

func (r *reader) ease(what string) Ease {
	var e Ease
	e.Kind = EaseKind(r.u8(what))
	e.Args = r.floatSlice(what)
	return e
}

func (r *reader) spring(what string) Spring {
	var s Spring
	s.Mass = r.f64(what)
	s.Stiffness = r.f64(what)
	s.Damping = r.f64(what)
	s.Velocity = r.f64(what)
	return s
}

func (r *reader) target() Target {
	var t Target
	t.Kind = TargetKind(r.u8("target.kind"))
	t.Ref = r.str("target.ref")
	return t
}

func (r *reader) position() Position {
	var p Position
	p.Kind = PositionKind(r.u8("position.kind"))
	p.Val = r.f64("position.val")
	p.Label = r.str("position.label")
	return p
}

// optValue reads a presence byte and, when set, a Value.
func (r *reader) optValue(what string) *Value {
	if !r.boolean(what + ".present") {
		return nil
	}
	v := r.value(what)
	return &v
}

func (r *reader) key() Key {
	var k Key
	k.T = r.f64("key.t")
	k.Value = r.value("key.value")
	if r.boolean("key.ease.present") {
		e := r.ease("key.ease")
		k.Ease = &e
	}
	// Cubicspline tangents exist only in MOT2+; MOT1 keys have none.
	if r.version >= 2 {
		k.InTangent = r.optValue("key.intangent")
		k.OutTangent = r.optValue("key.outtangent")
	}
	return k
}

func (r *reader) generator() *Generator {
	if !r.boolean("gen.present") {
		return nil
	}
	g := &Generator{}
	g.Kind = GeneratorKind(r.u8("gen.kind"))
	g.Base = r.value("gen.base")
	g.Spin = r.fixed3("gen.spin")
	g.Spring = r.spring("gen.spring")
	g.Drift = r.fixed3("gen.drift")
	g.DriftSpeed = r.fixed3("gen.driftspeed")
	g.DriftPhase = r.fixed3("gen.driftphase")
	// MOT3: GenOscillator fields. Absent in MOT1/MOT2 (zero value → no oscillation).
	if r.version >= 3 {
		g.OscArity = ValueArity(r.u8("gen.oscarity"))
		g.OscBase = r.fixed4("gen.oscbase")
		g.OscAmp = r.fixed4("gen.oscamp")
		g.OscFreq = r.fixed4("gen.oscfreq")
		g.OscPhase = r.fixed4("gen.oscphase")
	}
	return g
}

func (r *reader) track() *Track {
	tr := &Track{}
	tr.Target = r.target()
	tr.Prop = r.str("track.prop")
	n := r.u32("track.keys.count")
	if r.err != nil {
		return tr
	}
	// each key is at least: 8 (T) + 1+32 (Value) + 1 (ease presence) = 42 bytes;
	// MOT2 adds 2 tangent presence bytes → 44.
	minKey := 42
	if r.version >= 2 {
		minKey = 44
	}
	if int(n) > (len(r.b)-r.off)/minKey {
		r.fail("track.keys count")
		return tr
	}
	if n > 0 {
		tr.Keys = make([]Key, n)
		for i := range tr.Keys {
			tr.Keys[i] = r.key()
			if r.err != nil {
				return tr
			}
		}
	}
	tr.Gen = r.generator()
	tr.Interp = Interp(r.u8("track.interp"))
	tr.Ease = r.ease("track.ease")
	tr.TargetID = r.i32("track.targetid")
	tr.PropID = r.i32("track.propid")
	return tr
}

func (r *reader) positioned() Positioned {
	var p Positioned
	p.At = r.position()
	disc := r.u8("positioned.disc")
	if r.err != nil {
		return p
	}
	switch disc {
	case 0:
		p.Track = r.track()
	case 1:
		p.Sub = r.timeline()
	default:
		r.fail(fmt.Sprintf("positioned.disc=%d", disc))
	}
	return p
}

func (r *reader) timeline() *Timeline {
	tl := &Timeline{}
	tl.ID = r.str("timeline.id")
	n := r.u32("timeline.children.count")
	if r.err != nil {
		return tl
	}
	// each child is at least: position(>=13) + disc(1) bytes.
	if int(n) > (len(r.b)-r.off)/14 {
		r.fail("timeline.children count")
		return tl
	}
	if n > 0 {
		tl.Children = make([]Positioned, n)
		for i := range tl.Children {
			tl.Children[i] = r.positioned()
			if r.err != nil {
				return tl
			}
		}
	}
	tl.Loop = LoopMode(r.i32("timeline.loop"))
	tl.Alternate = r.boolean("timeline.alternate")
	tl.Speed = r.f64("timeline.speed")
	tl.Autoplay = r.boolean("timeline.autoplay")
	return tl
}

// DecodeProgram is the exact inverse of EncodeProgram. It returns an error on
// truncation or bad magic and never panics on malformed input.
func DecodeProgram(b []byte) (tl *Timeline, targetRefs, propRefs []string, err error) {
	if len(b) < 4 {
		return nil, nil, nil, fmt.Errorf("motion wire: too short for magic (len=%d)", len(b))
	}
	var version int
	switch {
	case b[0] == wireMagic[0] && b[1] == wireMagic[1] && b[2] == wireMagic[2] && b[3] == wireMagic[3]:
		version = 3
	case b[0] == wireMagicV2[0] && b[1] == wireMagicV2[1] && b[2] == wireMagicV2[2] && b[3] == wireMagicV2[3]:
		version = 2
	case b[0] == wireMagicV1[0] && b[1] == wireMagicV1[1] && b[2] == wireMagicV1[2] && b[3] == wireMagicV1[3]:
		version = 1
	default:
		return nil, nil, nil, fmt.Errorf("motion wire: bad magic %q", string(b[:4]))
	}
	r := &reader{b: b, off: 4, version: version}
	targetRefs = r.stringList("targetRefs")
	propRefs = r.stringList("propRefs")
	tl = r.timeline()
	if r.err != nil {
		return nil, nil, nil, r.err
	}
	return tl, targetRefs, propRefs, nil
}
