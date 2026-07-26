package crdt

import (
	"encoding/binary"
	"fmt"
	"math"
	"sort"
	"time"

	enc "m31labs.dev/gosx/crdt/encoding"
)

// Compact binary document snapshot.
//
// The old snapshot was JSON. A 5000-rune text document cost 1,132,616 bytes,
// which is 226 bytes for every stored character. Two things caused that:
//
//   - Every element repeated the 32-character actor identity two or three times,
//     once for its own identity, once for its reference, once for the identity
//     that last set its visibility.
//   - The snapshot embedded the whole change log a second time, as JSON.
//
// The binary form fixes both. It interns every actor identity and every object
// identity in a table, writes integers as ULEB128 varints, delta-encodes element
// counters against the previous element, and stores the common cases as flag
// bits instead of bytes. Tombstones travel as run lengths.
//
// Change hashes are not stored. Load recomputes each hash from the canonical
// change encoding, in table order, and resolves dependencies through table
// indexes. A dependency that does not appear earlier in the table falls back to
// a raw 32-byte hash.
//
// A legacy JSON body starts with '{'. The binary body starts with a version
// byte, so Load accepts both.

const snapshotVersion2 byte = 0x02

// Element flag bits.
const (
	elemFlagNoAfter    = 1 << 0 // the element sits at the head of the sequence
	elemFlagAfterPrev  = 1 << 1 // the reference is the previous element
	elemFlagVisIsID    = 1 << 2 // the visibility identity equals the element identity
	elemFlagSameActor  = 1 << 3 // the actor matches the previous element
	elemFlagPlainValue = 1 << 4 // the value is a string; only its bytes follow
)

// Change flag bits.
const (
	changeFlagDefaultGroup = 1 << 0 // the group identity is the actor and sequence pair
	changeFlagHasMessage   = 1 << 1
)

// Object kind codes.
const (
	objKindMapCode  byte = 0
	objKindListCode byte = 1
	objKindTextCode byte = 2
)

// Value kind codes.
const (
	valNull byte = iota
	valBool
	valInt
	valUint
	valFloat
	valString
	valBytes
	valCounter
	valTimestamp
	valMap
	valList
	valText
	valVector
)

// --- writer ---

type snapWriter struct {
	buf        []byte
	actors     map[string]uint64
	actorList  []string
	objects    map[ObjID]uint64
	objectList []ObjID
	strings    map[string]uint64
	stringList []string
}

func newSnapWriter() *snapWriter {
	return &snapWriter{
		actors:  make(map[string]uint64),
		objects: make(map[ObjID]uint64),
		strings: make(map[string]uint64),
	}
}

func (w *snapWriter) uint(v uint64) { w.buf = enc.AppendULEB128(w.buf, v) }

func (w *snapWriter) int(v int64) { w.uint(zigzagEncode(v)) }

func (w *snapWriter) byteVal(b byte) { w.buf = append(w.buf, b) }

func (w *snapWriter) bytes(b []byte) {
	w.uint(uint64(len(b)))
	w.buf = append(w.buf, b...)
}

func (w *snapWriter) str(s string) {
	w.uint(uint64(len(s)))
	w.buf = append(w.buf, s...)
}

func (w *snapWriter) actorRef(actor string) uint64 {
	if index, ok := w.actors[actor]; ok {
		return index
	}
	index := uint64(len(w.actorList))
	w.actors[actor] = index
	w.actorList = append(w.actorList, actor)
	return index
}

func (w *snapWriter) objectRef(obj ObjID) uint64 {
	if index, ok := w.objects[obj]; ok {
		return index
	}
	index := uint64(len(w.objectList))
	w.objects[obj] = index
	w.objectList = append(w.objectList, obj)
	return index
}

func (w *snapWriter) stringRef(s string) uint64 {
	if index, ok := w.strings[s]; ok {
		return index
	}
	index := uint64(len(w.stringList))
	w.strings[s] = index
	w.stringList = append(w.stringList, s)
	return index
}

func zigzagEncode(v int64) uint64 { return uint64(v<<1) ^ uint64(v>>63) }

func zigzagDecode(v uint64) int64 { return int64(v>>1) ^ -int64(v&1) }

// --- reader ---

type snapReader struct {
	buf     []byte
	pos     int
	actors  []string
	objects []ObjID
	strings []string
}

func (r *snapReader) uint() (uint64, error) {
	value, n, err := enc.ReadULEB128(r.buf[r.pos:])
	if err != nil {
		return 0, err
	}
	r.pos += n
	return value, nil
}

func (r *snapReader) int() (int64, error) {
	value, err := r.uint()
	if err != nil {
		return 0, err
	}
	return zigzagDecode(value), nil
}

func (r *snapReader) byteVal() (byte, error) {
	if r.pos >= len(r.buf) {
		return 0, fmt.Errorf("snapshot truncated")
	}
	b := r.buf[r.pos]
	r.pos++
	return b, nil
}

func (r *snapReader) raw(n int) ([]byte, error) {
	if n < 0 || r.pos+n > len(r.buf) {
		return nil, fmt.Errorf("snapshot truncated")
	}
	out := r.buf[r.pos : r.pos+n]
	r.pos += n
	return out, nil
}

func (r *snapReader) bytes() ([]byte, error) {
	length, err := r.uint()
	if err != nil {
		return nil, err
	}
	if length > uint64(len(r.buf)-r.pos) {
		return nil, fmt.Errorf("snapshot length %d exceeds body", length)
	}
	out, err := r.raw(int(length))
	if err != nil {
		return nil, err
	}
	return append([]byte(nil), out...), nil
}

func (r *snapReader) str() (string, error) {
	b, err := r.bytes()
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func (r *snapReader) actor(index uint64) (string, error) {
	if index >= uint64(len(r.actors)) {
		return "", fmt.Errorf("actor index %d out of range", index)
	}
	return r.actors[index], nil
}

func (r *snapReader) object(index uint64) (ObjID, error) {
	if index >= uint64(len(r.objects)) {
		return "", fmt.Errorf("object index %d out of range", index)
	}
	return r.objects[index], nil
}

func (r *snapReader) interned(index uint64) (string, error) {
	if index >= uint64(len(r.strings)) {
		return "", fmt.Errorf("string index %d out of range", index)
	}
	return r.strings[index], nil
}

// --- value codec ---

func (w *snapWriter) writeValue(v Value) {
	switch v.Kind {
	case ValueKindNull, "":
		w.byteVal(valNull)
	case ValueKindBool:
		w.byteVal(valBool)
		if v.Bool {
			w.byteVal(1)
		} else {
			w.byteVal(0)
		}
	case ValueKindInt:
		w.byteVal(valInt)
		w.int(v.Int)
	case ValueKindUint:
		w.byteVal(valUint)
		w.uint(v.Uint)
	case ValueKindFloat:
		w.byteVal(valFloat)
		w.writeFloat64(v.Float)
	case ValueKindString:
		w.byteVal(valString)
		w.str(v.Str)
	case ValueKindBytes:
		w.byteVal(valBytes)
		w.bytes(v.Bytes)
	case ValueKindCounter:
		w.byteVal(valCounter)
		w.int(v.Counter)
	case ValueKindTimestamp:
		w.byteVal(valTimestamp)
		w.writeTime(v.Time)
	case ValueKindMap:
		w.byteVal(valMap)
		w.uint(w.objectRef(v.Obj))
	case ValueKindList:
		w.byteVal(valList)
		w.uint(w.objectRef(v.Obj))
	case ValueKindText:
		w.byteVal(valText)
		w.uint(w.objectRef(v.Obj))
	case ValueKindVector:
		w.byteVal(valVector)
		w.bytes(v.VectorPacked)
		w.writeFloat32(v.VectorNorm)
		w.int(int64(v.VectorDim))
		w.int(int64(v.VectorBits))
	default:
		// An unknown kind keeps its name so the round trip stays lossless.
		w.byteVal(0xff)
		w.str(string(v.Kind))
		w.str(v.Str)
	}
}

func (w *snapWriter) writeFloat64(f float64) {
	var scratch [8]byte
	binary.LittleEndian.PutUint64(scratch[:], math.Float64bits(f))
	w.buf = append(w.buf, scratch[:]...)
}

func (w *snapWriter) writeFloat32(f float32) {
	var scratch [4]byte
	binary.LittleEndian.PutUint32(scratch[:], math.Float32bits(f))
	w.buf = append(w.buf, scratch[:]...)
}

// writeTime stores a timestamp as seconds plus nanoseconds so the whole time
// range survives the round trip. A nil time writes a zero marker.
func (w *snapWriter) writeTime(t *time.Time) {
	if t == nil {
		w.byteVal(0)
		return
	}
	w.byteVal(1)
	utc := t.UTC()
	w.int(utc.Unix())
	w.uint(uint64(utc.Nanosecond()))
}

func (r *snapReader) readValue() (Value, error) {
	kind, err := r.byteVal()
	if err != nil {
		return Value{}, err
	}
	switch kind {
	case valNull:
		return NullValue(), nil
	case valBool:
		b, err := r.byteVal()
		if err != nil {
			return Value{}, err
		}
		return BoolValue(b != 0), nil
	case valInt:
		n, err := r.int()
		if err != nil {
			return Value{}, err
		}
		return IntValue(n), nil
	case valUint:
		n, err := r.uint()
		if err != nil {
			return Value{}, err
		}
		return UintValue(n), nil
	case valFloat:
		f, err := r.readFloat64()
		if err != nil {
			return Value{}, err
		}
		return FloatValue(f), nil
	case valString:
		s, err := r.str()
		if err != nil {
			return Value{}, err
		}
		return StringValue(s), nil
	case valBytes:
		b, err := r.bytes()
		if err != nil {
			return Value{}, err
		}
		return Value{Kind: ValueKindBytes, Bytes: b}, nil
	case valCounter:
		n, err := r.int()
		if err != nil {
			return Value{}, err
		}
		return CounterValue(n), nil
	case valTimestamp:
		t, err := r.readTime()
		if err != nil {
			return Value{}, err
		}
		return Value{Kind: ValueKindTimestamp, Time: t}, nil
	case valMap, valList, valText:
		index, err := r.uint()
		if err != nil {
			return Value{}, err
		}
		obj, err := r.object(index)
		if err != nil {
			return Value{}, err
		}
		switch kind {
		case valMap:
			return MapValue(obj), nil
		case valList:
			return ListValue(obj), nil
		default:
			return TextValue(obj), nil
		}
	case valVector:
		packed, err := r.bytes()
		if err != nil {
			return Value{}, err
		}
		norm, err := r.readFloat32()
		if err != nil {
			return Value{}, err
		}
		dim, err := r.int()
		if err != nil {
			return Value{}, err
		}
		bits, err := r.int()
		if err != nil {
			return Value{}, err
		}
		return Value{
			Kind:         ValueKindVector,
			VectorPacked: packed,
			VectorNorm:   norm,
			VectorDim:    int(dim),
			VectorBits:   int(bits),
		}, nil
	case 0xff:
		name, err := r.str()
		if err != nil {
			return Value{}, err
		}
		text, err := r.str()
		if err != nil {
			return Value{}, err
		}
		return Value{Kind: ValueKind(name), Str: text}, nil
	default:
		return Value{}, fmt.Errorf("unknown value code %d", kind)
	}
}

func (r *snapReader) readFloat64() (float64, error) {
	raw, err := r.raw(8)
	if err != nil {
		return 0, err
	}
	return math.Float64frombits(binary.LittleEndian.Uint64(raw)), nil
}

func (r *snapReader) readFloat32() (float32, error) {
	raw, err := r.raw(4)
	if err != nil {
		return 0, err
	}
	return math.Float32frombits(binary.LittleEndian.Uint32(raw)), nil
}

func (r *snapReader) readTime() (*time.Time, error) {
	present, err := r.byteVal()
	if err != nil {
		return nil, err
	}
	if present == 0 {
		return nil, nil
	}
	sec, err := r.int()
	if err != nil {
		return nil, err
	}
	nsec, err := r.uint()
	if err != nil {
		return nil, err
	}
	value := time.Unix(sec, int64(nsec)).UTC()
	return &value, nil
}

// --- document body ---

// encodeSnapshotV2 writes the compact body for one document state.
func (d *Doc) encodeSnapshotV2() []byte {
	w := newSnapWriter()

	// Reserve table slots for every identity the body references. The tables are
	// written last, so growing them while encoding is safe.
	body := newSnapWriter()
	body.actors = w.actors
	body.objects = w.objects
	body.strings = w.strings

	docActor := body.actorRef(d.actorID.String())
	body.uint(docActor)
	body.uint(d.seq)
	body.uint(d.maxOp)

	body.uint(uint64(len(d.deps)))
	for _, dep := range d.deps {
		body.buf = append(body.buf, dep[:]...)
	}

	ids := make([]ObjID, 0, len(d.objects))
	for id := range d.objects {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })

	body.uint(uint64(len(ids)))
	for _, id := range ids {
		body.uint(body.objectRef(id))
		body.writeObject(d.objects[id])
	}

	changeIndex := make(map[ChangeHash]int, len(d.changes))
	for i, change := range d.changes {
		changeIndex[change.Hash] = i
	}
	body.uint(uint64(len(d.changes)))
	var previousTime int64
	for i, change := range d.changes {
		previousTime = body.writeChange(change, i, changeIndex, previousTime)
	}

	// Header: version, tables, then the body.
	w.byteVal(snapshotVersion2)
	w.uint(uint64(len(body.actorList)))
	for _, actor := range body.actorList {
		w.str(actor)
	}
	w.uint(uint64(len(body.objectList)))
	for _, obj := range body.objectList {
		w.str(string(obj))
	}
	w.uint(uint64(len(body.stringList)))
	for _, s := range body.stringList {
		w.str(s)
	}
	w.buf = append(w.buf, body.buf...)
	return w.buf
}

func (w *snapWriter) writeObject(obj *object) {
	switch obj.Kind {
	case objectKindMap:
		w.byteVal(objKindMapCode)
		keys := make([]string, 0, len(obj.Map))
		for key := range obj.Map {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		w.uint(uint64(len(keys)))
		for _, key := range keys {
			entry := obj.Map[key]
			w.str(key)
			w.uint(w.actorRef(entry.ID.Actor))
			w.uint(entry.ID.Counter)
			if entry.Deleted {
				w.byteVal(1)
			} else {
				w.byteVal(0)
			}
			w.writeValue(entry.Value)
		}
	case objectKindList, objectKindText:
		if obj.Kind == objectKindText {
			w.byteVal(objKindTextCode)
		} else {
			w.byteVal(objKindListCode)
		}
		w.writeElems(obj)
	default:
		// Preserve an unknown kind by name so a round trip stays lossless.
		w.byteVal(0xff)
		w.str(string(obj.Kind))
	}
}

// writeElems encodes the element column. Counters are delta-encoded against the
// previous element, and tombstones travel as run lengths.
func (w *snapWriter) writeElems(obj *object) {
	var elems []listElem
	if obj.seq != nil {
		elems = obj.seq.elems()
	}
	w.uint(uint64(len(elems)))

	var (
		previousCounter uint64
		previousActor   string
		previousID      OpID
		havePrevious    bool
	)
	for i := range elems {
		elem := elems[i]
		afterID, hasAfter, err := parseOpIDRef(elem.After)
		var flags byte
		if err != nil || !hasAfter {
			flags |= elemFlagNoAfter
		} else if havePrevious && afterID == previousID {
			flags |= elemFlagAfterPrev
		}
		visID := elem.VisibilityID
		if visID.Actor == "" || visID == elem.ID {
			flags |= elemFlagVisIsID
		}
		if havePrevious && elem.ID.Actor == previousActor {
			flags |= elemFlagSameActor
		}
		if elem.Value.Kind == ValueKindString {
			flags |= elemFlagPlainValue
		}
		w.byteVal(flags)
		w.int(int64(elem.ID.Counter) - int64(previousCounter))
		if flags&elemFlagSameActor == 0 {
			w.uint(w.actorRef(elem.ID.Actor))
		}
		if flags&(elemFlagNoAfter|elemFlagAfterPrev) == 0 {
			w.uint(w.actorRef(afterID.Actor))
			w.int(int64(afterID.Counter) - int64(elem.ID.Counter))
		}
		if flags&elemFlagVisIsID == 0 {
			w.uint(w.actorRef(visID.Actor))
			w.int(int64(visID.Counter) - int64(elem.ID.Counter))
		}
		if flags&elemFlagPlainValue != 0 {
			w.str(elem.Value.Str)
		} else {
			w.writeValue(elem.Value)
		}

		previousCounter = elem.ID.Counter
		previousActor = elem.ID.Actor
		previousID = elem.ID
		havePrevious = true
	}

	// Tombstones as run lengths: pairs of (visible run, deleted run).
	runs := make([]uint64, 0, 8)
	current := false
	count := uint64(0)
	for i := range elems {
		if elems[i].Deleted == current {
			count++
			continue
		}
		runs = append(runs, count)
		current = !current
		count = 1
	}
	if count > 0 {
		runs = append(runs, count)
	}
	w.uint(uint64(len(runs)))
	for _, run := range runs {
		w.uint(run)
	}
}

func (w *snapWriter) writeChange(change Change, position int, index map[ChangeHash]int, previousTime int64) int64 {
	var flags byte
	defaultGroup := fmt.Sprintf("%s:%d", change.ActorID, change.Seq)
	if change.ChangeGroupID == defaultGroup {
		flags |= changeFlagDefaultGroup
	}
	if change.Message != "" {
		flags |= changeFlagHasMessage
	}
	w.byteVal(flags)
	w.uint(w.actorRef(change.ActorID))
	w.uint(change.Seq)
	w.uint(change.StartOp)

	utc := change.Time.UTC()
	seconds := utc.Unix()
	w.int(seconds - previousTime)
	w.uint(uint64(utc.Nanosecond()))

	if flags&changeFlagHasMessage != 0 {
		w.uint(w.stringRef(change.Message))
	}
	if flags&changeFlagDefaultGroup == 0 {
		w.uint(w.stringRef(change.ChangeGroupID))
	}

	w.uint(uint64(len(change.Deps)))
	for _, dep := range change.Deps {
		if at, ok := index[dep]; ok && at < position {
			w.uint(uint64(at) + 1)
			continue
		}
		w.uint(0)
		w.buf = append(w.buf, dep[:]...)
	}

	w.uint(uint64(len(change.Ops)))
	for _, op := range change.Ops {
		w.writeOp(op)
	}
	return seconds
}

// Operation flag bits.
const (
	opFlagHasProp    = 1 << 0
	opFlagHasAfter   = 1 << 1
	opFlagHasRun     = 1 << 2
	opFlagHasDeletes = 1 << 3
	opFlagHasValue   = 1 << 4
	// opFlagPropRef marks a Prop that names an element rather than a map key.
	// The reference costs one actor index and one counter delta, not a 36-byte
	// "counter@actor" string.
	opFlagPropRef = 1 << 5
	// opFlagAfterRef marks an After field that names an element.
	opFlagAfterRef = 1 << 6
)

func (w *snapWriter) writeOp(op Op) {
	var flags byte
	propRef, propIsRef, err := parseOpIDRef(string(op.Prop))
	propIsRef = err == nil && propIsRef && formatOpIDRef(propRef, true) == string(op.Prop)
	afterRef, afterIsRef, err := parseOpIDRef(op.After)
	afterIsRef = err == nil && afterIsRef && formatOpIDRef(afterRef, true) == op.After

	if op.Prop != "" {
		flags |= opFlagHasProp
		if propIsRef {
			flags |= opFlagPropRef
		}
	}
	if op.After != "" {
		flags |= opFlagHasAfter
		if afterIsRef {
			flags |= opFlagAfterRef
		}
	}
	if op.Run != "" {
		flags |= opFlagHasRun
	}
	if len(op.DeleteRuns) > 0 {
		flags |= opFlagHasDeletes
	}
	if op.Value.Kind != "" {
		flags |= opFlagHasValue
	}
	w.byteVal(flags)
	w.uint(w.actorRef(op.ID.Actor))
	w.uint(op.ID.Counter)
	w.uint(w.stringRef(op.Action))
	w.uint(w.objectRef(op.Obj))
	if flags&opFlagHasProp != 0 {
		if propIsRef {
			w.uint(w.actorRef(propRef.Actor))
			w.int(int64(propRef.Counter) - int64(op.ID.Counter))
		} else {
			w.uint(w.stringRef(string(op.Prop)))
		}
	}
	if flags&opFlagHasValue != 0 {
		w.writeValue(op.Value)
	}
	if flags&opFlagHasAfter != 0 {
		if afterIsRef {
			w.uint(w.actorRef(afterRef.Actor))
			w.int(int64(afterRef.Counter) - int64(op.ID.Counter))
		} else {
			w.uint(w.stringRef(op.After))
		}
	}
	if flags&opFlagHasRun != 0 {
		w.str(op.Run)
	}
	if flags&opFlagHasDeletes != 0 {
		w.uint(uint64(len(op.DeleteRuns)))
		for _, run := range op.DeleteRuns {
			w.uint(w.actorRef(run.Actor))
			w.uint(run.Start)
			w.uint(run.Count)
		}
	}
}

// decodeSnapshotV2 rebuilds a document from the compact body.
func decodeSnapshotV2(body []byte) (*Doc, error) {
	r := &snapReader{buf: body, pos: 1}

	count, err := r.uint()
	if err != nil {
		return nil, err
	}
	if count > uint64(len(body)) {
		return nil, fmt.Errorf("actor table length %d exceeds body", count)
	}
	r.actors = make([]string, 0, count)
	for i := uint64(0); i < count; i++ {
		actor, err := r.str()
		if err != nil {
			return nil, err
		}
		r.actors = append(r.actors, actor)
	}

	count, err = r.uint()
	if err != nil {
		return nil, err
	}
	if count > uint64(len(body)) {
		return nil, fmt.Errorf("object table length %d exceeds body", count)
	}
	r.objects = make([]ObjID, 0, count)
	for i := uint64(0); i < count; i++ {
		obj, err := r.str()
		if err != nil {
			return nil, err
		}
		r.objects = append(r.objects, ObjID(obj))
	}

	count, err = r.uint()
	if err != nil {
		return nil, err
	}
	if count > uint64(len(body)) {
		return nil, fmt.Errorf("string table length %d exceeds body", count)
	}
	r.strings = make([]string, 0, count)
	for i := uint64(0); i < count; i++ {
		s, err := r.str()
		if err != nil {
			return nil, err
		}
		r.strings = append(r.strings, s)
	}

	actorIndex, err := r.uint()
	if err != nil {
		return nil, err
	}
	actorHex, err := r.actor(actorIndex)
	if err != nil {
		return nil, err
	}
	actor, err := ParseActorID(actorHex)
	if err != nil {
		return nil, err
	}
	doc := newDocWithActor(actor)

	if doc.seq, err = r.uint(); err != nil {
		return nil, err
	}
	if doc.maxOp, err = r.uint(); err != nil {
		return nil, err
	}

	depCount, err := r.uint()
	if err != nil {
		return nil, err
	}
	if depCount > uint64(len(body)) {
		return nil, fmt.Errorf("dependency count %d exceeds body", depCount)
	}
	for i := uint64(0); i < depCount; i++ {
		raw, err := r.raw(32)
		if err != nil {
			return nil, err
		}
		var hash ChangeHash
		copy(hash[:], raw)
		doc.deps = append(doc.deps, hash)
	}

	objCount, err := r.uint()
	if err != nil {
		return nil, err
	}
	if objCount > uint64(len(body)) {
		return nil, fmt.Errorf("object count %d exceeds body", objCount)
	}
	doc.objects = make(map[ObjID]*object, objCount)
	for i := uint64(0); i < objCount; i++ {
		index, err := r.uint()
		if err != nil {
			return nil, err
		}
		id, err := r.object(index)
		if err != nil {
			return nil, err
		}
		obj, err := r.readObject()
		if err != nil {
			return nil, err
		}
		doc.objects[id] = obj
	}
	if _, ok := doc.objects[Root]; !ok {
		doc.objects[Root] = newMapObject()
	}

	changeCount, err := r.uint()
	if err != nil {
		return nil, err
	}
	if changeCount > uint64(len(body)) {
		return nil, fmt.Errorf("change count %d exceeds body", changeCount)
	}
	var previousTime int64
	for i := uint64(0); i < changeCount; i++ {
		change, seconds, err := r.readChange(doc.changes, previousTime)
		if err != nil {
			return nil, err
		}
		previousTime = seconds
		// Recompute the hash from the canonical change encoding. The snapshot
		// does not store hashes, so the cost of a load buys back 32 bytes per
		// change in the file.
		_, hash, err := EncodeChangeChunk(change)
		if err != nil {
			return nil, err
		}
		change.Hash = hash
		doc.changes = append(doc.changes, change)
		doc.changeIndex[hash.String()] = change
	}
	return doc, nil
}

func (r *snapReader) readObject() (*object, error) {
	kind, err := r.byteVal()
	if err != nil {
		return nil, err
	}
	switch kind {
	case objKindMapCode:
		obj := newMapObject()
		count, err := r.uint()
		if err != nil {
			return nil, err
		}
		if count > uint64(len(r.buf)) {
			return nil, fmt.Errorf("map entry count %d exceeds body", count)
		}
		for i := uint64(0); i < count; i++ {
			key, err := r.str()
			if err != nil {
				return nil, err
			}
			actorIndex, err := r.uint()
			if err != nil {
				return nil, err
			}
			actor, err := r.actor(actorIndex)
			if err != nil {
				return nil, err
			}
			counter, err := r.uint()
			if err != nil {
				return nil, err
			}
			deleted, err := r.byteVal()
			if err != nil {
				return nil, err
			}
			value, err := r.readValue()
			if err != nil {
				return nil, err
			}
			obj.Map[key] = mapEntry{
				Value:   value,
				ID:      OpID{Counter: counter, Actor: actor},
				Deleted: deleted != 0,
			}
		}
		return obj, nil
	case objKindListCode, objKindTextCode:
		listKind := objectKindList
		if kind == objKindTextCode {
			listKind = objectKindText
		}
		obj := newListObject(listKind)
		elems, err := r.readElems()
		if err != nil {
			return nil, err
		}
		obj.seq.reset(elems)
		return obj, nil
	case 0xff:
		name, err := r.str()
		if err != nil {
			return nil, err
		}
		return &object{Kind: objectKind(name)}, nil
	default:
		return nil, fmt.Errorf("unknown object code %d", kind)
	}
}

func (r *snapReader) readElems() ([]listElem, error) {
	count, err := r.uint()
	if err != nil {
		return nil, err
	}
	if count > uint64(len(r.buf)) {
		return nil, fmt.Errorf("element count %d exceeds body", count)
	}
	elems := make([]listElem, 0, count)

	var (
		previousCounter uint64
		previousActor   string
		previousID      OpID
		havePrevious    bool
	)
	for i := uint64(0); i < count; i++ {
		flags, err := r.byteVal()
		if err != nil {
			return nil, err
		}
		delta, err := r.int()
		if err != nil {
			return nil, err
		}
		counter := uint64(int64(previousCounter) + delta)
		actor := previousActor
		if flags&elemFlagSameActor == 0 {
			actorIndex, err := r.uint()
			if err != nil {
				return nil, err
			}
			if actor, err = r.actor(actorIndex); err != nil {
				return nil, err
			}
		} else if !havePrevious {
			return nil, fmt.Errorf("element 0 claims the previous actor")
		}
		id := OpID{Counter: counter, Actor: actor}

		after := ""
		switch {
		case flags&elemFlagNoAfter != 0:
			after = ""
		case flags&elemFlagAfterPrev != 0:
			if !havePrevious {
				return nil, fmt.Errorf("element 0 claims the previous reference")
			}
			after = previousID.String()
		default:
			actorIndex, err := r.uint()
			if err != nil {
				return nil, err
			}
			afterActor, err := r.actor(actorIndex)
			if err != nil {
				return nil, err
			}
			afterDelta, err := r.int()
			if err != nil {
				return nil, err
			}
			after = OpID{Counter: uint64(int64(counter) + afterDelta), Actor: afterActor}.String()
		}

		visID := id
		if flags&elemFlagVisIsID == 0 {
			actorIndex, err := r.uint()
			if err != nil {
				return nil, err
			}
			visActor, err := r.actor(actorIndex)
			if err != nil {
				return nil, err
			}
			visDelta, err := r.int()
			if err != nil {
				return nil, err
			}
			visID = OpID{Counter: uint64(int64(counter) + visDelta), Actor: visActor}
		}

		var value Value
		if flags&elemFlagPlainValue != 0 {
			text, err := r.str()
			if err != nil {
				return nil, err
			}
			value = StringValue(text)
		} else if value, err = r.readValue(); err != nil {
			return nil, err
		}

		elems = append(elems, listElem{ID: id, After: after, Value: value, VisibilityID: visID})
		previousCounter = counter
		previousActor = actor
		previousID = id
		havePrevious = true
	}

	runCount, err := r.uint()
	if err != nil {
		return nil, err
	}
	if runCount > uint64(len(r.buf)) {
		return nil, fmt.Errorf("tombstone run count %d exceeds body", runCount)
	}
	position := 0
	deleted := false
	for i := uint64(0); i < runCount; i++ {
		run, err := r.uint()
		if err != nil {
			return nil, err
		}
		if run > uint64(len(elems)-position) {
			return nil, fmt.Errorf("tombstone run %d exceeds element count", run)
		}
		if deleted {
			for j := uint64(0); j < run; j++ {
				elems[position+int(j)].Deleted = true
			}
		}
		position += int(run)
		deleted = !deleted
	}
	return elems, nil
}

func (r *snapReader) readChange(existing []Change, previousTime int64) (Change, int64, error) {
	flags, err := r.byteVal()
	if err != nil {
		return Change{}, 0, err
	}
	actorIndex, err := r.uint()
	if err != nil {
		return Change{}, 0, err
	}
	actor, err := r.actor(actorIndex)
	if err != nil {
		return Change{}, 0, err
	}
	seq, err := r.uint()
	if err != nil {
		return Change{}, 0, err
	}
	startOp, err := r.uint()
	if err != nil {
		return Change{}, 0, err
	}
	secondsDelta, err := r.int()
	if err != nil {
		return Change{}, 0, err
	}
	seconds := previousTime + secondsDelta
	nanos, err := r.uint()
	if err != nil {
		return Change{}, 0, err
	}

	change := Change{
		ActorID: actor,
		Seq:     seq,
		StartOp: startOp,
		Time:    time.Unix(seconds, int64(nanos)).UTC(),
	}
	if flags&changeFlagHasMessage != 0 {
		index, err := r.uint()
		if err != nil {
			return Change{}, 0, err
		}
		if change.Message, err = r.interned(index); err != nil {
			return Change{}, 0, err
		}
	}
	if flags&changeFlagDefaultGroup != 0 {
		change.ChangeGroupID = fmt.Sprintf("%s:%d", actor, seq)
	} else {
		index, err := r.uint()
		if err != nil {
			return Change{}, 0, err
		}
		if change.ChangeGroupID, err = r.interned(index); err != nil {
			return Change{}, 0, err
		}
	}

	depCount, err := r.uint()
	if err != nil {
		return Change{}, 0, err
	}
	if depCount > uint64(len(r.buf)) {
		return Change{}, 0, fmt.Errorf("dependency count %d exceeds body", depCount)
	}
	if depCount > 0 {
		change.Deps = make([]ChangeHash, 0, depCount)
	}
	for i := uint64(0); i < depCount; i++ {
		ref, err := r.uint()
		if err != nil {
			return Change{}, 0, err
		}
		if ref == 0 {
			raw, err := r.raw(32)
			if err != nil {
				return Change{}, 0, err
			}
			var hash ChangeHash
			copy(hash[:], raw)
			change.Deps = append(change.Deps, hash)
			continue
		}
		at := int(ref - 1)
		if at >= len(existing) {
			return Change{}, 0, fmt.Errorf("dependency index %d out of range", at)
		}
		change.Deps = append(change.Deps, existing[at].Hash)
	}

	opCount, err := r.uint()
	if err != nil {
		return Change{}, 0, err
	}
	if opCount > uint64(len(r.buf)) {
		return Change{}, 0, fmt.Errorf("operation count %d exceeds body", opCount)
	}
	if opCount > 0 {
		change.Ops = make([]Op, 0, opCount)
	}
	for i := uint64(0); i < opCount; i++ {
		op, err := r.readOp()
		if err != nil {
			return Change{}, 0, err
		}
		change.Ops = append(change.Ops, op)
	}
	return change, seconds, nil
}

// readOpIDRef decodes an element reference stored as an actor index plus a
// counter delta relative to base.
func (r *snapReader) readOpIDRef(base uint64) (OpID, error) {
	actorIndex, err := r.uint()
	if err != nil {
		return OpID{}, err
	}
	actor, err := r.actor(actorIndex)
	if err != nil {
		return OpID{}, err
	}
	delta, err := r.int()
	if err != nil {
		return OpID{}, err
	}
	return OpID{Counter: uint64(int64(base) + delta), Actor: actor}, nil
}

func (r *snapReader) readOp() (Op, error) {
	flags, err := r.byteVal()
	if err != nil {
		return Op{}, err
	}
	actorIndex, err := r.uint()
	if err != nil {
		return Op{}, err
	}
	actor, err := r.actor(actorIndex)
	if err != nil {
		return Op{}, err
	}
	counter, err := r.uint()
	if err != nil {
		return Op{}, err
	}
	actionIndex, err := r.uint()
	if err != nil {
		return Op{}, err
	}
	action, err := r.interned(actionIndex)
	if err != nil {
		return Op{}, err
	}
	objIndex, err := r.uint()
	if err != nil {
		return Op{}, err
	}
	obj, err := r.object(objIndex)
	if err != nil {
		return Op{}, err
	}

	op := Op{
		ID:     OpID{Counter: counter, Actor: actor},
		Action: action,
		Obj:    obj,
	}
	if flags&opFlagHasProp != 0 {
		if flags&opFlagPropRef != 0 {
			ref, err := r.readOpIDRef(counter)
			if err != nil {
				return Op{}, err
			}
			op.Prop = Prop(ref.String())
		} else {
			index, err := r.uint()
			if err != nil {
				return Op{}, err
			}
			prop, err := r.interned(index)
			if err != nil {
				return Op{}, err
			}
			op.Prop = Prop(prop)
		}
	}
	if flags&opFlagHasValue != 0 {
		if op.Value, err = r.readValue(); err != nil {
			return Op{}, err
		}
	}
	if flags&opFlagHasAfter != 0 {
		if flags&opFlagAfterRef != 0 {
			ref, err := r.readOpIDRef(counter)
			if err != nil {
				return Op{}, err
			}
			op.After = ref.String()
		} else {
			index, err := r.uint()
			if err != nil {
				return Op{}, err
			}
			if op.After, err = r.interned(index); err != nil {
				return Op{}, err
			}
		}
	}
	if flags&opFlagHasRun != 0 {
		if op.Run, err = r.str(); err != nil {
			return Op{}, err
		}
	}
	if flags&opFlagHasDeletes != 0 {
		count, err := r.uint()
		if err != nil {
			return Op{}, err
		}
		if count > uint64(len(r.buf)) {
			return Op{}, fmt.Errorf("delete run count %d exceeds body", count)
		}
		op.DeleteRuns = make([]OpIDRun, 0, count)
		for i := uint64(0); i < count; i++ {
			runActorIndex, err := r.uint()
			if err != nil {
				return Op{}, err
			}
			runActor, err := r.actor(runActorIndex)
			if err != nil {
				return Op{}, err
			}
			start, err := r.uint()
			if err != nil {
				return Op{}, err
			}
			runCount, err := r.uint()
			if err != nil {
				return Op{}, err
			}
			op.DeleteRuns = append(op.DeleteRuns, OpIDRun{Actor: runActor, Start: start, Count: runCount})
		}
	}
	return op, nil
}
