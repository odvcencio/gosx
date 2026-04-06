package crdt

import (
	"fmt"
	"sort"
	"strconv"
	stdsync "sync"
	"time"

	enc "github.com/odvcencio/gosx/crdt/encoding"
	crdtsync "github.com/odvcencio/gosx/crdt/sync"
)

type objectKind string

const (
	objectKindMap  objectKind = "map"
	objectKindList objectKind = "list"
	objectKindText objectKind = "text"
)

type mapEntry struct {
	Value   Value `json:"value"`
	ID      OpID  `json:"id"`
	Deleted bool  `json:"deleted,omitempty"`
}

type listElem struct {
	ID      OpID   `json:"id"`
	After   string `json:"after,omitempty"`
	Value   Value  `json:"value"`
	Deleted bool   `json:"deleted,omitempty"`
}

type object struct {
	Kind objectKind          `json:"kind"`
	Map  map[string]mapEntry `json:"map,omitempty"`
	List []listElem          `json:"list,omitempty"`
}

type snapshot struct {
	ActorID string           `json:"actorId"`
	Seq     uint64           `json:"seq"`
	MaxOp   uint64           `json:"maxOp"`
	Deps    []ChangeHash     `json:"deps"`
	Objects map[ObjID]object `json:"objects"`
	Changes []Change         `json:"changes"`
}

type Doc struct {
	mu             stdsync.RWMutex
	actorID        ActorID
	objects        map[ObjID]*object
	changes        []Change
	changeIndex    map[string]Change
	deps           []ChangeHash
	seq            uint64
	maxOp          uint64
	pending        []Op
	pendingPatches []Patch
	changeHooks    []func([]Patch)
}

func NewDoc() *Doc {
	actor, err := NewActorID()
	if err != nil {
		panic(err)
	}
	return newDocWithActor(actor)
}

func newDocWithActor(actor ActorID) *Doc {
	return &Doc{
		actorID:     actor,
		objects:     map[ObjID]*object{Root: newMapObject()},
		changeIndex: make(map[string]Change),
	}
}

func Load(data []byte) (*Doc, error) {
	body, err := enc.DecodeDocument(data)
	if err != nil {
		return nil, err
	}
	var snap snapshot
	if err := unmarshalJSON(body, &snap); err != nil {
		return nil, fmt.Errorf("decode document snapshot: %w", err)
	}

	actor, err := ParseActorID(snap.ActorID)
	if err != nil {
		return nil, err
	}

	doc := newDocWithActor(actor)
	doc.seq = snap.Seq
	doc.maxOp = snap.MaxOp
	doc.deps = append([]ChangeHash(nil), snap.Deps...)
	doc.objects = make(map[ObjID]*object, len(snap.Objects))
	for id, value := range snap.Objects {
		obj := value
		if obj.Kind == objectKindMap && obj.Map == nil {
			obj.Map = make(map[string]mapEntry)
		}
		doc.objects[id] = &obj
	}
	doc.changes = append([]Change(nil), snap.Changes...)
	for i, change := range doc.changes {
		if change.Hash == (ChangeHash{}) {
			_, hash, err := EncodeChangeChunk(change)
			if err != nil {
				return nil, err
			}
			change.Hash = hash
			doc.changes[i] = change
		}
		doc.changeIndex[change.Hash.String()] = change
	}
	if _, ok := doc.objects[Root]; !ok {
		doc.objects[Root] = newMapObject()
	}
	return doc, nil
}

func (d *Doc) Save() ([]byte, error) {
	hooks, patches, err := d.flushPendingForSnapshot()
	if err != nil {
		return nil, err
	}
	fireHooks(hooks, patches)

	d.mu.RLock()
	defer d.mu.RUnlock()

	snap := snapshot{
		ActorID: d.actorID.String(),
		Seq:     d.seq,
		MaxOp:   d.maxOp,
		Deps:    append([]ChangeHash(nil), d.deps...),
		Objects: make(map[ObjID]object, len(d.objects)),
		Changes: append([]Change(nil), d.changes...),
	}
	for id, obj := range d.objects {
		snap.Objects[id] = *cloneObject(obj)
	}
	body, err := marshalJSON(snap)
	if err != nil {
		return nil, fmt.Errorf("encode document snapshot: %w", err)
	}
	return enc.EncodeDocument(body), nil
}

func (d *Doc) Put(obj ObjID, prop Prop, val Value) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	op := d.newLocalOpLocked("put", obj, prop, val)
	patch, err := d.applyOpLocked(op)
	if err != nil {
		return err
	}
	d.pending = append(d.pending, op)
	d.pendingPatches = append(d.pendingPatches, patch)
	return nil
}

func (d *Doc) Delete(obj ObjID, prop Prop) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	op := d.newLocalOpLocked("delete", obj, prop, NullValue())
	patch, err := d.applyOpLocked(op)
	if err != nil {
		return err
	}
	d.pending = append(d.pending, op)
	d.pendingPatches = append(d.pendingPatches, patch)
	return nil
}

func (d *Doc) Increment(obj ObjID, prop Prop, n int64) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	op := d.newLocalOpLocked("increment", obj, prop, CounterValue(n))
	patch, err := d.applyOpLocked(op)
	if err != nil {
		return err
	}
	d.pending = append(d.pending, op)
	d.pendingPatches = append(d.pendingPatches, patch)
	return nil
}

func (d *Doc) Get(obj ObjID, prop Prop) (Value, ObjID, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()
	target, err := d.objectLocked(obj)
	if err != nil {
		return Value{}, "", err
	}
	switch target.Kind {
	case objectKindMap:
		entry, ok := target.Map[string(prop)]
		if !ok || entry.Deleted {
			return Value{}, "", fmt.Errorf("prop %q not found on %s", prop, obj)
		}
		return entry.Value.Clone(), entry.Value.Obj, nil
	case objectKindList, objectKindText:
		index, err := strconv.Atoi(string(prop))
		if err != nil {
			return Value{}, "", fmt.Errorf("list prop %q is not an index", prop)
		}
		value, err := d.listValueLocked(target, index)
		if err != nil {
			return Value{}, "", err
		}
		return value.Clone(), value.Obj, nil
	default:
		return Value{}, "", fmt.Errorf("unknown object kind %q", target.Kind)
	}
}

func (d *Doc) MakeMap(obj ObjID, prop Prop) (ObjID, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	child := d.newObjectIDLocked()
	op := d.newLocalOpLocked("put", obj, prop, MapValue(child))
	patch, err := d.applyOpLocked(op)
	if err != nil {
		return "", err
	}
	d.pending = append(d.pending, op)
	d.pendingPatches = append(d.pendingPatches, patch)
	return child, nil
}

func (d *Doc) MakeList(obj ObjID, prop Prop) (ObjID, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	child := d.newObjectIDLocked()
	op := d.newLocalOpLocked("put", obj, prop, ListValue(child))
	patch, err := d.applyOpLocked(op)
	if err != nil {
		return "", err
	}
	d.pending = append(d.pending, op)
	d.pendingPatches = append(d.pendingPatches, patch)
	return child, nil
}

func (d *Doc) MakeText(obj ObjID, prop Prop) (ObjID, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	child := d.newObjectIDLocked()
	op := d.newLocalOpLocked("put", obj, prop, TextValue(child))
	patch, err := d.applyOpLocked(op)
	if err != nil {
		return "", err
	}
	d.pending = append(d.pending, op)
	d.pendingPatches = append(d.pendingPatches, patch)
	return child, nil
}

func (d *Doc) InsertAt(list ObjID, index uint64, val Value) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	target, err := d.objectLocked(list)
	if err != nil {
		return err
	}
	if target.Kind != objectKindList && target.Kind != objectKindText {
		return fmt.Errorf("%s is not a list-like object", list)
	}
	after, err := d.afterIDLocked(target, int(index))
	if err != nil {
		return err
	}
	op := d.newLocalOpLocked("insert", list, Prop(strconv.FormatUint(index, 10)), val)
	op.After = after
	patch, err := d.applyOpLocked(op)
	if err != nil {
		return err
	}
	d.pending = append(d.pending, op)
	d.pendingPatches = append(d.pendingPatches, patch)
	return nil
}

func (d *Doc) DeleteAt(list ObjID, index uint64) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	target, err := d.objectLocked(list)
	if err != nil {
		return err
	}
	if target.Kind != objectKindList && target.Kind != objectKindText {
		return fmt.Errorf("%s is not a list-like object", list)
	}
	visible := visibleElems(target.List)
	if int(index) >= len(visible) {
		return fmt.Errorf("list index %d out of range (length %d)", index, len(visible))
	}
	elemID := visible[index].ID
	op := d.newLocalOpLocked("delete", list, Prop(elemID.String()), NullValue())
	patch, err := d.applyOpLocked(op)
	if err != nil {
		return err
	}
	d.pending = append(d.pending, op)
	d.pendingPatches = append(d.pendingPatches, patch)
	return nil
}

func (d *Doc) TextToString(text ObjID) (string, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()
	target, err := d.objectLocked(text)
	if err != nil {
		return "", err
	}
	if target.Kind != objectKindText && target.Kind != objectKindList {
		return "", fmt.Errorf("%s is not a text or list object", text)
	}
	visible := visibleElems(target.List)
	var buf []byte
	for _, elem := range visible {
		buf = append(buf, elem.Value.Str...)
	}
	return string(buf), nil
}

func (d *Doc) ListLen(list ObjID) (int, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()
	target, err := d.objectLocked(list)
	if err != nil {
		return 0, err
	}
	if target.Kind != objectKindList && target.Kind != objectKindText {
		return 0, fmt.Errorf("%s is not a list-like object", list)
	}
	return len(visibleElems(target.List)), nil
}

func (d *Doc) Commit(msg string) (ChangeHash, error) {
	hooks, patches, hash, err := d.commitPending(msg)
	if err != nil {
		return ChangeHash{}, err
	}
	fireHooks(hooks, patches)
	return hash, nil
}

func (d *Doc) Merge(other *Doc) error {
	other.mu.RLock()
	changes := append([]Change(nil), other.changes...)
	other.mu.RUnlock()

	sort.Slice(changes, func(i, j int) bool {
		if changes[i].StartOp != changes[j].StartOp {
			return changes[i].StartOp < changes[j].StartOp
		}
		return changes[i].Hash.String() < changes[j].Hash.String()
	})

	var patches []Patch
	var hooks []func([]Patch)

	d.mu.Lock()
	for _, change := range changes {
		if _, ok := d.changeIndex[change.Hash.String()]; ok {
			continue
		}
		applied, err := d.applyRemoteChangeLocked(change)
		if err != nil {
			d.mu.Unlock()
			return err
		}
		patches = append(patches, applied...)
	}
	hooks = append([]func([]Patch){}, d.changeHooks...)
	d.mu.Unlock()

	fireHooks(hooks, patches)
	return nil
}

func (d *Doc) Fork() (*Doc, error) {
	saved, err := d.Save()
	if err != nil {
		return nil, err
	}
	return Load(saved)
}

func (d *Doc) GenerateSyncMessage(state *crdtsync.State) ([]byte, bool) {
	if state == nil {
		return nil, false
	}

	d.mu.RLock()
	defer d.mu.RUnlock()

	heads := make([][32]byte, len(d.deps))
	for i, dep := range d.deps {
		heads[i] = [32]byte(dep)
	}

	var (
		changes [][]byte
		hashes  [][32]byte
	)
	for _, change := range d.changes {
		hash := [32]byte(change.Hash)
		if state.HasKnown(hash) || state.HasSent(hash) {
			continue
		}
		chunk, _, err := EncodeChangeChunk(change)
		if err != nil {
			continue
		}
		changes = append(changes, chunk)
		hashes = append(hashes, hash)
	}

	if !state.ShouldSend(heads, len(changes) > 0) {
		return nil, false
	}

	data, err := crdtsync.EncodeMessage(crdtsync.Message{
		Version: crdtsync.MessageTypeV1,
		Heads:   heads,
		Changes: changes,
	})
	if err != nil {
		return nil, false
	}
	for _, hash := range hashes {
		state.MarkSent(hash)
	}
	state.NoteHeads(heads)
	return data, true
}

func (d *Doc) ReceiveSyncMessage(state *crdtsync.State, msg []byte) error {
	if state == nil {
		return fmt.Errorf("sync state required")
	}
	decoded, err := crdtsync.DecodeMessage(msg)
	if err != nil {
		return err
	}

	var patches []Patch
	var hooks []func([]Patch)

	d.mu.Lock()
	for _, chunk := range decoded.Changes {
		change, err := DecodeChangeChunk(chunk)
		if err != nil {
			d.mu.Unlock()
			return err
		}
		state.MarkKnown([32]byte(change.Hash))
		if _, ok := d.changeIndex[change.Hash.String()]; ok {
			continue
		}
		applied, err := d.applyRemoteChangeLocked(change)
		if err != nil {
			d.mu.Unlock()
			return err
		}
		patches = append(patches, applied...)
	}
	for _, head := range decoded.Heads {
		state.MarkKnown(head)
	}
	hooks = append([]func([]Patch){}, d.changeHooks...)
	d.mu.Unlock()

	fireHooks(hooks, patches)
	return nil
}

func (d *Doc) OnChange(fn func(patches []Patch)) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.changeHooks = append(d.changeHooks, fn)
}

func (d *Doc) ActorID() ActorID {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.actorID
}

func (d *Doc) flushPendingForSnapshot() ([]func([]Patch), []Patch, error) {
	hooks, patches, _, err := d.commitPending("")
	return hooks, patches, err
}

func (d *Doc) commitPending(msg string) ([]func([]Patch), []Patch, ChangeHash, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if len(d.pending) == 0 {
		return nil, nil, ChangeHash{}, nil
	}

	change := Change{
		ActorID: d.actorID.String(),
		Seq:     d.seq + 1,
		Deps:    append([]ChangeHash(nil), d.deps...),
		StartOp: d.pending[0].ID.Counter,
		Time:    time.Now().UTC(),
		Message: msg,
		Ops:     append([]Op(nil), d.pending...),
	}
	_, hash, err := EncodeChangeChunk(change)
	if err != nil {
		return nil, nil, ChangeHash{}, fmt.Errorf("encode change chunk: %w", err)
	}
	change.Hash = hash
	d.seq = change.Seq
	d.changes = append(d.changes, change)
	d.changeIndex[hash.String()] = change
	d.deps = []ChangeHash{hash}

	patches := append([]Patch(nil), d.pendingPatches...)
	d.pending = nil
	d.pendingPatches = nil
	hooks := append([]func([]Patch){}, d.changeHooks...)
	return hooks, patches, hash, nil
}

func (d *Doc) applyRemoteChangeLocked(change Change) ([]Patch, error) {
	if change.Hash == (ChangeHash{}) {
		_, hash, err := EncodeChangeChunk(change)
		if err != nil {
			return nil, err
		}
		change.Hash = hash
	}

	var patches []Patch
	for _, op := range change.Ops {
		patch, err := d.applyOpLocked(op)
		if err != nil {
			return nil, err
		}
		patches = append(patches, patch)
		if op.ID.Counter > d.maxOp {
			d.maxOp = op.ID.Counter
		}
	}
	d.changes = append(d.changes, change)
	d.changeIndex[change.Hash.String()] = change
	d.deps = d.mergeHeadsLocked(change)
	return patches, nil
}

func (d *Doc) mergeHeadsLocked(change Change) []ChangeHash {
	heads := make(map[string]ChangeHash, len(d.deps)+1)
	for _, head := range d.deps {
		heads[head.String()] = head
	}
	for _, dep := range change.Deps {
		delete(heads, dep.String())
	}
	heads[change.Hash.String()] = change.Hash

	out := make([]ChangeHash, 0, len(heads))
	for _, head := range heads {
		out = append(out, head)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].String() < out[j].String()
	})
	return out
}

func (d *Doc) newLocalOpLocked(action string, obj ObjID, prop Prop, value Value) Op {
	d.maxOp++
	return Op{
		ID: OpID{
			Counter: d.maxOp,
			Actor:   d.actorID.String(),
		},
		Action: action,
		Obj:    obj,
		Prop:   prop,
		Value:  value.Clone(),
	}
}

func (d *Doc) newObjectIDLocked() ObjID {
	d.maxOp++
	return ObjID(fmt.Sprintf("%s-%d", d.actorID.String(), d.maxOp))
}

func (d *Doc) objectLocked(obj ObjID) (*object, error) {
	target, ok := d.objects[obj]
	if !ok {
		return nil, fmt.Errorf("object %s not found", obj)
	}
	return target, nil
}

func (d *Doc) ensureObjectForValueLocked(value Value) {
	if !value.IsObject() {
		return
	}
	if _, ok := d.objects[value.Obj]; ok {
		return
	}
	switch value.Kind {
	case ValueKindMap:
		d.objects[value.Obj] = newMapObject()
	case ValueKindList:
		d.objects[value.Obj] = newListObject(objectKindList)
	case ValueKindText:
		d.objects[value.Obj] = newListObject(objectKindText)
	}
}

func (d *Doc) applyOpLocked(op Op) (Patch, error) {
	target, err := d.objectLocked(op.Obj)
	if err != nil {
		return Patch{}, err
	}
	d.ensureObjectForValueLocked(op.Value)

	switch op.Action {
	case "put":
		return d.applyPutLocked(target, op)
	case "delete":
		return d.applyDeleteLocked(target, op)
	case "increment":
		return d.applyIncrementLocked(target, op)
	case "insert":
		return d.applyInsertLocked(target, op)
	default:
		return Patch{}, fmt.Errorf("unknown op action %q", op.Action)
	}
}

func (d *Doc) applyPutLocked(target *object, op Op) (Patch, error) {
	switch target.Kind {
	case objectKindMap:
		current := target.Map[string(op.Prop)]
		if current.ID.Actor == "" || op.ID.Greater(current.ID) {
			target.Map[string(op.Prop)] = mapEntry{Value: op.Value.Clone(), ID: op.ID}
		}
		return Patch{Obj: op.Obj, Prop: op.Prop, Action: "put", Value: op.Value.Clone()}, nil
	default:
		return Patch{}, fmt.Errorf("put is only supported on map objects")
	}
}

func (d *Doc) applyDeleteLocked(target *object, op Op) (Patch, error) {
	switch target.Kind {
	case objectKindMap:
		current := target.Map[string(op.Prop)]
		if current.ID.Actor == "" || op.ID.Greater(current.ID) {
			target.Map[string(op.Prop)] = mapEntry{ID: op.ID, Deleted: true}
		}
		return Patch{Obj: op.Obj, Prop: op.Prop, Action: "delete"}, nil
	case objectKindList, objectKindText:
		elemIDStr := string(op.Prop)
		for i := range target.List {
			if target.List[i].ID.String() == elemIDStr {
				target.List[i].Deleted = true
				break
			}
		}
		return Patch{Obj: op.Obj, Prop: op.Prop, Action: "delete"}, nil
	default:
		return Patch{}, fmt.Errorf("delete is not supported on %s objects", target.Kind)
	}
}

func (d *Doc) applyIncrementLocked(target *object, op Op) (Patch, error) {
	switch target.Kind {
	case objectKindMap:
		current := target.Map[string(op.Prop)]
		next := current
		next.ID = op.ID
		if current.Value.Kind == ValueKindCounter {
			next.Value = CounterValue(current.Value.Counter + op.Value.Counter)
		} else if current.Value.Kind == ValueKindInt {
			next.Value = IntValue(current.Value.Int + op.Value.Counter)
		} else {
			next.Value = CounterValue(op.Value.Counter)
		}
		next.Deleted = false
		target.Map[string(op.Prop)] = next
		return Patch{Obj: op.Obj, Prop: op.Prop, Action: "increment", Value: next.Value.Clone()}, nil
	default:
		return Patch{}, fmt.Errorf("increment is only supported on map objects")
	}
}

func (d *Doc) applyInsertLocked(target *object, op Op) (Patch, error) {
	if target.Kind != objectKindList && target.Kind != objectKindText {
		return Patch{}, fmt.Errorf("insert is only supported on list-like objects")
	}
	insertPos := len(target.List)
	if op.After != "" {
		insertPos = d.findInsertPositionLocked(target, op.After, op.ID)
	} else {
		insertPos = d.findInsertPositionLocked(target, "", op.ID)
	}
	elem := listElem{
		ID:    op.ID,
		After: op.After,
		Value: op.Value.Clone(),
	}
	target.List = append(target.List, listElem{})
	copy(target.List[insertPos+1:], target.List[insertPos:])
	target.List[insertPos] = elem

	index := d.visibleIndexLocked(target, op.ID.String())
	return Patch{
		Obj:    op.Obj,
		Prop:   Prop(strconv.Itoa(index)),
		Action: "insert",
		Value:  op.Value.Clone(),
	}, nil
}

func (d *Doc) afterIDLocked(target *object, index int) (string, error) {
	if index < 0 {
		return "", fmt.Errorf("negative list index")
	}
	visible := visibleElems(target.List)
	if index == 0 {
		return "", nil
	}
	if index > len(visible) {
		return "", fmt.Errorf("list index %d out of range", index)
	}
	return visible[index-1].ID.String(), nil
}

func (d *Doc) findInsertPositionLocked(target *object, after string, id OpID) int {
	base := -1
	for i := range target.List {
		if target.List[i].ID.String() == after {
			base = i
			break
		}
	}
	pos := base + 1
	for pos < len(target.List) {
		current := target.List[pos]
		if current.After != after {
			break
		}
		if !current.ID.Less(id) {
			break
		}
		pos++
	}
	return pos
}

func (d *Doc) visibleIndexLocked(target *object, opID string) int {
	visible := visibleElems(target.List)
	for i, elem := range visible {
		if elem.ID.String() == opID {
			return i
		}
	}
	return len(visible)
}

func (d *Doc) listValueLocked(target *object, index int) (Value, error) {
	visible := visibleElems(target.List)
	if index < 0 || index >= len(visible) {
		return Value{}, fmt.Errorf("list index %d out of range", index)
	}
	return visible[index].Value.Clone(), nil
}

func newMapObject() *object {
	return &object{
		Kind: objectKindMap,
		Map:  make(map[string]mapEntry),
	}
}

func newListObject(kind objectKind) *object {
	return &object{
		Kind: kind,
		List: make([]listElem, 0),
	}
}

func visibleElems(list []listElem) []listElem {
	out := make([]listElem, 0, len(list))
	for _, elem := range list {
		if elem.Deleted {
			continue
		}
		out = append(out, elem)
	}
	return out
}

func cloneObject(obj *object) *object {
	out := &object{
		Kind: obj.Kind,
	}
	if obj.Map != nil {
		out.Map = make(map[string]mapEntry, len(obj.Map))
		for key, value := range obj.Map {
			value.Value = value.Value.Clone()
			out.Map[key] = value
		}
	}
	if obj.List != nil {
		out.List = make([]listElem, len(obj.List))
		for i, value := range obj.List {
			value.Value = value.Value.Clone()
			out.List[i] = value
		}
	}
	return out
}

func fireHooks(hooks []func([]Patch), patches []Patch) {
	if len(hooks) == 0 || len(patches) == 0 {
		return
	}
	cloned := append([]Patch(nil), patches...)
	for _, hook := range hooks {
		hook(cloned)
	}
}

func marshalJSON(value any) ([]byte, error) {
	return jsonMarshal(value)
}

func unmarshalJSON(data []byte, value any) error {
	return jsonUnmarshal(data, value)
}
