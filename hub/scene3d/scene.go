// Package scene3d replicates one Scene3D scene across peers through a single
// CRDT document, and keeps the server authoritative over every write.
//
// Three GoSX parts already worked alone before this package existed:
//
//   - Package scene turns a before state and an after state into typed
//     mutation commands (scene.DiffCommands and friends).
//   - Package crdt converges a document under partition.
//   - Package hub replicates a document over a socket and gates inbound
//     writes through three authorizer hooks.
//
// This package is the edge between them. It writes a command stream into a
// document, recovers a command stream from a document change, and enforces
// server authority with the hub gate that already exists.
//
// # Why a separate package
//
// Package hub is a generic realtime primitive. It knows about sockets, clients,
// and documents, and it must not know about Scene3D, or every hub user would
// carry the whole scene graph. This package sits above both and depends on
// both, so the dependency points the right way.
//
// A second reason was mechanical when this package was written: package scene
// reached package hub through package physics, so a bridge inside package hub
// closed an import cycle. Keep the bridge here even after that path changes.
//
// # Representation
//
// The document holds scene STATE in a keyed map, not a command LOG. Every
// write lands on one root map key:
//
//	<ns>/i                  the object index, a CRDT list of object IDs
//	<ns>/o/<objectID>/c     the create payload for one object
//	<ns>/o/<objectID>/t     the transform patch for one object
//	<ns>/o/<objectID>/m     the material patch for one object
//	<ns>/o/<objectID>/l     the light patch for one object
//	<ns>/o/<objectID>/x     the removal flag for one object
//	<ns>/s/<slot>           one whole collection, for example "camera"
//
// Every value is the exact JSON of the command Data field, so the round trip
// back to a command is byte-exact.
//
// A CRDT map applies last-writer-wins per key. Two peers that edit DIFFERENT
// objects touch different keys, so both edits survive. That is the property a
// whole-scene snapshot loses. Two peers that edit the SAME object and the same
// field race on one key, and the greater crdt.OpID wins: the greater counter,
// and on equal counters the lexicographically greater actor ID. Both peers
// compute the same winner, because both compare the same two OpID values.
//
// # What a command log would do differently
//
// A CRDT list of serialized commands would keep BOTH concurrent edits to the
// same object, and replay would pick the one that lands later in the list.
// That is also deterministic, and it preserves an audit history. It costs two
// things this representation does not pay. First, the document grows with the
// number of edits instead of the number of objects, so a long editing session
// never stops growing. Second, a reader must replay the whole log to learn the
// current scene, so a joining peer pays for history it will never show. The
// map wins for an editor, because an editor cares about the current scene.
// Choose the log when you need the history itself.
//
// # Order
//
// The map does not keep the author order of the object slices. A materialized
// View sorts objects, labels, sprites, HTML records, and lights by ascending
// object ID. The browser runtime keys those five collections by ID, so order
// carries no meaning there. Post effects DO have a semantic order, which is
// why the post-FX chain stays one whole-collection value instead of one key
// per effect.
package scene3d

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"

	"m31labs.dev/gosx/crdt"
	crdtsync "m31labs.dev/gosx/crdt/sync"
	"m31labs.dev/gosx/scene"
)

// Field codes name the per-object keys. They are one byte so a large scene
// pays as little key text as possible; the object ID already dominates.
const (
	fieldCreate    = "c"
	fieldTransform = "t"
	fieldMaterial  = "m"
	fieldLight     = "l"
	fieldGone      = "x"
)

// Slot names name the whole-collection keys. Each name is stable on the wire
// because a saved document outlives the process that wrote it.
const (
	slotCamera             = "camera"
	slotEnvironment        = "environment"
	slotMaterials          = "materials"
	slotModels             = "models"
	slotParticles          = "particles"
	slotInstancedMeshes    = "instancedMeshes"
	slotInstancedGLBMeshes = "instancedGLBMeshes"
	slotAnimations         = "animations"
	slotPostEffects        = "postEffects"
	slotPostUniforms       = "postUniforms"
)

// collectionOrder lists the whole-collection slots in the order a bootstrap
// command stream must emit them. Materials come first because a node may
// reference a material by name or index, and the environment follows, so a
// renderer has its shared tables before the first object arrives.
var collectionOrder = []struct {
	slot string
	kind scene.CommandKind
}{
	{slotMaterials, scene.CommandSetMaterials},
	{slotEnvironment, scene.CommandSetEnvironment},
	{slotCamera, scene.CommandSetCamera},
	{slotModels, scene.CommandSetModels},
	{slotParticles, scene.CommandSetParticles},
	{slotInstancedMeshes, scene.CommandSetInstancedMeshes},
	{slotInstancedGLBMeshes, scene.CommandSetInstancedGLBMeshes},
	{slotAnimations, scene.CommandSetAnimations},
	{slotPostEffects, scene.CommandSetPostEffects},
	{slotPostUniforms, scene.CommandSetPostUniforms},
}

// slotForKind maps every whole-collection command kind to its slot.
var slotForKind = map[scene.CommandKind]string{
	scene.CommandSetCamera:             slotCamera,
	scene.CommandSetEnvironment:        slotEnvironment,
	scene.CommandSetMaterials:          slotMaterials,
	scene.CommandSetModels:             slotModels,
	scene.CommandSetParticles:          slotParticles,
	scene.CommandSetInstancedMeshes:    slotInstancedMeshes,
	scene.CommandSetInstancedGLBMeshes: slotInstancedGLBMeshes,
	scene.CommandSetAnimations:         slotAnimations,
	scene.CommandSetPostEffects:        slotPostEffects,
	scene.CommandSetPostUniforms:       slotPostUniforms,
}

// fieldForKind maps every per-object patch command kind to its field code.
// A create and a removal are not in this table, because both change the
// object's presence and need extra work. See encodeCommands.
var fieldForKind = map[scene.CommandKind]string{
	scene.CommandSetTransform: fieldTransform,
	scene.CommandSetMaterial:  fieldMaterial,
	scene.CommandSetLight:     fieldLight,
}

// kindForField reverses fieldForKind so a document read can rebuild the
// command that produced a patch field.
var kindForField = map[string]scene.CommandKind{
	fieldTransform: scene.CommandSetTransform,
	fieldMaterial:  scene.CommandSetMaterial,
	fieldLight:     scene.CommandSetLight,
}

// patchFieldOrder fixes the order of the per-object patch commands a bootstrap
// stream emits, so two peers that hold the same document produce the same
// stream byte for byte.
var patchFieldOrder = []string{fieldTransform, fieldMaterial, fieldLight}

// ErrEmptyObjectID reports a command that changes one object but names none.
// The diff API always fills ObjectID for a create or a removal; a hand-built
// command may not. Refusing the command is deliberate. Dropping it would lose
// a scene mutation without a trace, which is the failure this package exists
// to prevent.
var ErrEmptyObjectID = errors.New("scene3d: command needs a non-empty objectId")

// Doc binds one crdt.Doc to one Scene3D namespace inside that document. Bind
// several namespaces to one document to replicate several scenes over one
// socket.
//
// A Doc adds no lock of its own for the document. Every read and every write
// goes through crdt.Doc, which holds its own mutex. The mutex here guards only
// the watcher list.
type Doc struct {
	doc *crdt.Doc
	ns  string

	indexKey crdt.Prop
	indexObj crdt.ObjID
	objPfx   string
	slotPfx  string

	mu       sync.Mutex
	watchers []func([]scene.Command)
	hooked   bool
}

// Bind returns the Scene3D view of doc under namespace. The namespace must be
// non-empty and must not contain a slash, because a slash separates the parts
// of every key this package writes; a namespace with a slash could collide
// with an object ID from another namespace.
func Bind(doc *crdt.Doc, namespace string) (*Doc, error) {
	if doc == nil {
		return nil, errors.New("scene3d: nil document")
	}
	if namespace == "" {
		return nil, errors.New("scene3d: empty namespace")
	}
	if strings.Contains(namespace, "/") {
		return nil, fmt.Errorf("scene3d: namespace %q contains a slash", namespace)
	}
	return &Doc{
		doc:      doc,
		ns:       namespace,
		indexKey: crdt.Prop(namespace + "/i"),
		indexObj: crdt.ObjID(namespace + "/i"),
		objPfx:   namespace + "/o/",
		slotPfx:  namespace + "/s/",
	}, nil
}

// Doc returns the underlying document. Pass it to hub.Hub.SyncDoc to
// replicate the scene, or to crdt.Doc.Save to persist it.
func (d *Doc) Doc() *crdt.Doc { return d.doc }

// Namespace returns the namespace this Doc writes under.
func (d *Doc) Namespace() string { return d.ns }

// objectKey builds the root map key for one field of one object.
func (d *Doc) objectKey(objectID, field string) crdt.Prop {
	return crdt.Prop(d.objPfx + objectID + "/" + field)
}

// slotKey builds the root map key for one whole collection.
func (d *Doc) slotKey(slot string) crdt.Prop {
	return crdt.Prop(d.slotPfx + slot)
}

// parsedKey is the meaning of one root map key inside this namespace.
type parsedKey struct {
	objectID string
	field    string
	slot     string
	index    bool
}

// parseKey decodes one root map key. It reports false for a key that belongs
// to another namespace or to another feature sharing the document.
//
// The object ID may itself contain slashes, so the field code is taken from
// the LAST slash. A field code never contains a slash, so the split is exact.
func (d *Doc) parseKey(prop crdt.Prop) (parsedKey, bool) {
	key := string(prop)
	if key == string(d.indexKey) {
		return parsedKey{index: true}, true
	}
	if rest, ok := strings.CutPrefix(key, d.slotPfx); ok {
		if !isKnownSlot(rest) {
			return parsedKey{}, false
		}
		return parsedKey{slot: rest}, true
	}
	rest, ok := strings.CutPrefix(key, d.objPfx)
	if !ok {
		return parsedKey{}, false
	}
	cut := strings.LastIndexByte(rest, '/')
	if cut <= 0 {
		return parsedKey{}, false
	}
	objectID, field := rest[:cut], rest[cut+1:]
	switch field {
	case fieldCreate, fieldTransform, fieldMaterial, fieldLight, fieldGone:
	default:
		return parsedKey{}, false
	}
	return parsedKey{objectID: objectID, field: field}, true
}

func isKnownSlot(slot string) bool {
	for _, entry := range collectionOrder {
		if entry.slot == slot {
			return true
		}
	}
	return false
}

// kindForSlot is the inverse of slotForKind. It exists so parseKey can reject
// an unknown slot name instead of accepting a key no reader understands.
func kindForSlot(slot string) scene.CommandKind {
	for _, entry := range collectionOrder {
		if entry.slot == slot {
			return entry.kind
		}
	}
	return scene.CommandKind(-1)
}

// -----------------------------------------------------------------------------
// Write path: scene mutation to CRDT change
// -----------------------------------------------------------------------------

// DiffScene diffs two lowered scene payloads, writes the resulting commands
// into the document, and commits one change. It returns the commands it wrote
// so a caller can also push them straight to a mounted surface.
//
// An empty diff writes nothing and returns a zero hash. Committing an empty
// change would grow the history and wake every peer for no reason.
func (d *Doc) DiffScene(previous, next scene.SceneIR, message string) ([]scene.Command, crdt.ChangeHash, error) {
	commands := scene.DiffCommands(previous, next)
	hash, err := d.Apply(commands, message)
	return commands, hash, err
}

// DiffProps lowers two typed Scene3D props values and applies the diff.
func (d *Doc) DiffProps(previous, next scene.Props, message string) ([]scene.Command, crdt.ChangeHash, error) {
	return d.DiffScene(previous.SceneIR(), next.SceneIR(), message)
}

// DiffIR applies the canonical IR camera, environment, and material tables.
// scene.DiffIRCommands covers only those three fields, so DiffIR does not
// replace DiffScene; call both when a scene carries both payloads.
func (d *Doc) DiffIR(previous, next scene.IR, message string) ([]scene.Command, crdt.ChangeHash, error) {
	commands := scene.DiffIRCommands(previous, next)
	hash, err := d.Apply(commands, message)
	return commands, hash, err
}

// Apply writes commands into the document and commits them as one change, so
// a peer either sees the whole mutation or none of it.
//
// Apply returns an error before it writes anything when any command is
// malformed. The document is left untouched in that case, because Apply
// validates and encodes the whole stream first.
func (d *Doc) Apply(commands []scene.Command, message string) (crdt.ChangeHash, error) {
	if len(commands) == 0 {
		return crdt.ChangeHash{}, nil
	}
	writes, err := d.encodeCommands(commands)
	if err != nil {
		return crdt.ChangeHash{}, err
	}
	if len(writes) == 0 {
		return crdt.ChangeHash{}, nil
	}
	if err := d.ensureIndex(); err != nil {
		return crdt.ChangeHash{}, err
	}
	for _, write := range writes {
		if err := write(); err != nil {
			return crdt.ChangeHash{}, err
		}
	}
	return d.doc.Commit(message)
}

// ensureIndex makes the object index list exist. The list object ID is derived
// from the namespace, not from the actor, so two peers that create the index
// while partitioned create the SAME list and their inserts merge. An
// actor-allocated ID from crdt.Doc.MakeList would give each peer a different
// list, and one of the two would be lost to last-writer-wins on the root key.
func (d *Doc) ensureIndex() error {
	if _, _, err := d.doc.Get(crdt.Root, d.indexKey); err == nil {
		return nil
	}
	return d.doc.Put(crdt.Root, d.indexKey, crdt.ListValue(d.indexObj))
}

// encodeCommands turns a command stream into an ordered list of document
// writes. It validates every command first, so a malformed stream cannot leave
// a half-written document behind.
//
// The stream is folded once before encoding. The diff API replaces a changed
// record with a removal followed by a create, so a removal whose object is
// created again later in the SAME stream is a replacement, not a removal. It
// must not raise the removal flag, or a concurrent peer would see the object
// disappear. See scene.DiffCommands for why the diff replaces rather than
// patches.
func (d *Doc) encodeCommands(commands []scene.Command) ([]func() error, error) {
	recreated := make(map[string]bool)
	for _, command := range commands {
		if command.Kind == scene.CommandCreateObject && command.ObjectID != "" {
			recreated[command.ObjectID] = true
		}
	}

	// Read the object index ONCE, not once per command. A per-command
	// membership scan costs one read per index entry, so building a scene of n
	// objects cost n squared reads. Measured at 5000 objects that was over six
	// seconds. See BenchmarkDiffAndApplyFullScene.
	indexed, err := d.indexSet()
	if err != nil {
		return nil, err
	}

	writes := make([]func() error, 0, len(commands)+2)
	for _, command := range commands {
		payload, err := encodeData(command.Data)
		if err != nil {
			return nil, fmt.Errorf("scene3d: encode command kind %d: %w", command.Kind, err)
		}
		switch command.Kind {
		case scene.CommandCreateObject:
			id := command.ObjectID
			if id == "" {
				return nil, fmt.Errorf("%w (create)", ErrEmptyObjectID)
			}
			if !indexed[id] {
				indexed[id] = true
				writes = append(writes, d.appendIndex(id))
			}
			writes = append(writes, d.putWrite(d.objectKey(id, fieldCreate), payload))
			// A create must clear a removal flag that an earlier change
			// raised, or the object would stay invisible. Reading the local
			// flag first keeps the common case at zero extra operations.
			if d.removed(id) {
				writes = append(writes, d.boolWrite(d.objectKey(id, fieldGone), false))
			}
		case scene.CommandRemoveObject:
			id := command.ObjectID
			if id == "" {
				return nil, fmt.Errorf("%w (remove)", ErrEmptyObjectID)
			}
			if recreated[id] {
				continue // replacement, not a removal
			}
			writes = append(writes, d.boolWrite(d.objectKey(id, fieldGone), true))
		default:
			if field, ok := fieldForKind[command.Kind]; ok {
				id := command.ObjectID
				if id == "" {
					return nil, fmt.Errorf("%w (kind %d)", ErrEmptyObjectID, command.Kind)
				}
				if !indexed[id] {
					indexed[id] = true
					writes = append(writes, d.appendIndex(id))
				}
				writes = append(writes, d.putWrite(d.objectKey(id, field), payload))
				continue
			}
			slot, ok := slotForKind[command.Kind]
			if !ok {
				return nil, fmt.Errorf("scene3d: unknown command kind %d", command.Kind)
			}
			writes = append(writes, d.putWrite(d.slotKey(slot), payload))
		}
	}
	return writes, nil
}

func (d *Doc) putWrite(key crdt.Prop, payload string) func() error {
	return func() error { return d.doc.Put(crdt.Root, key, crdt.StringValue(payload)) }
}

func (d *Doc) boolWrite(key crdt.Prop, value bool) func() error {
	return func() error { return d.doc.Put(crdt.Root, key, crdt.BoolValue(value)) }
}

// appendIndex adds objectID to the end of the object index. The index is
// grow-only: a removal never shortens it, because a removed object may come back
// and because a shrinking index would need a second convergence rule.
//
// A duplicate entry is harmless. Two partitioned peers that both create the same
// object produce two entries, and ObjectIDs deduplicates on read.
func (d *Doc) appendIndex(objectID string) func() error {
	return func() error {
		length, err := d.doc.ListLen(d.indexObj)
		if err != nil {
			return err
		}
		return d.doc.InsertAt(d.indexObj, uint64(length), crdt.StringValue(objectID))
	}
}

// indexSet reads the object index into a membership set.
func (d *Doc) indexSet() (map[string]bool, error) {
	length, err := d.doc.ListLen(d.indexObj)
	if err != nil {
		return map[string]bool{}, nil // index not created yet
	}
	set := make(map[string]bool, length)
	for i := 0; i < length; i++ {
		value, _, err := d.doc.Get(d.indexObj, crdt.Prop(strconv.Itoa(i)))
		if err != nil {
			return nil, fmt.Errorf("scene3d: read object index at %d: %w", i, err)
		}
		if value.Str != "" {
			set[value.Str] = true
		}
	}
	return set, nil
}

// removed reports whether the removal flag for objectID is currently raised.
func (d *Doc) removed(objectID string) bool {
	value, _, err := d.doc.Get(crdt.Root, d.objectKey(objectID, fieldGone))
	return err == nil && value.Kind == crdt.ValueKindBool && value.Bool
}

// encodeData renders a command Data field as the JSON the browser runtime
// receives. A nil Data becomes "null", and decodeData turns "null" back into a
// nil Data, so the round trip is byte-exact for a command with no payload.
func encodeData(data any) (string, error) {
	if data == nil {
		return "null", nil
	}
	raw, err := json.Marshal(data)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

// decodeData reverses encodeData. It returns a json.RawMessage so re-marshaling
// the recovered command reproduces the original bytes exactly, including the
// key order the original writer chose.
func decodeData(payload string) any {
	if payload == "" || payload == "null" {
		return nil
	}
	return json.RawMessage(payload)
}

// -----------------------------------------------------------------------------
// Read path: CRDT state back to scene commands
// -----------------------------------------------------------------------------

// Commands returns the command stream that turns an EMPTY scene into the scene
// the document now describes. Send it to a peer that just joined.
//
// The order is fixed: whole collections in collectionOrder, then one create per
// present object sorted by ID, then the transform, material, and light patches
// for those objects. Two peers holding the same document return the same bytes.
//
// A removed object contributes nothing. A peer that starts empty has nothing to
// remove. Use Watch for the incremental stream, which does carry removals.
func (d *Doc) Commands() ([]scene.Command, error) {
	var commands []scene.Command
	for _, entry := range collectionOrder {
		payload, ok := d.slotValue(entry.slot)
		if !ok {
			continue
		}
		commands = append(commands, scene.Command{Kind: entry.kind, Data: decodeData(payload)})
	}

	ids, err := d.presentObjectIDs()
	if err != nil {
		return nil, err
	}
	for _, id := range ids {
		payload, ok := d.objectValue(id, fieldCreate)
		if !ok {
			continue
		}
		commands = append(commands, scene.Command{
			Kind:     scene.CommandCreateObject,
			ObjectID: id,
			Data:     decodeData(payload),
		})
	}
	for _, field := range patchFieldOrder {
		kind := kindForField[field]
		for _, id := range ids {
			payload, ok := d.objectValue(id, field)
			if !ok {
				continue
			}
			commands = append(commands, scene.Command{
				Kind:     kind,
				ObjectID: id,
				Data:     decodeData(payload),
			})
		}
	}
	return commands, nil
}

// ObjectIDs returns every object ID the index carries, sorted, including
// removed ones. Use PresentObjectIDs for the visible scene.
func (d *Doc) ObjectIDs() ([]string, error) {
	length, err := d.doc.ListLen(d.indexObj)
	if err != nil {
		return nil, nil // no index yet, so no objects
	}
	seen := make(map[string]struct{}, length)
	ids := make([]string, 0, length)
	for i := 0; i < length; i++ {
		value, _, err := d.doc.Get(d.indexObj, crdt.Prop(strconv.Itoa(i)))
		if err != nil {
			return nil, fmt.Errorf("scene3d: read object index at %d: %w", i, err)
		}
		id := value.Str
		if id == "" {
			continue
		}
		// Two partitioned peers can both append the same new object ID, so
		// the merged index holds it twice. Deduplicate on read.
		if _, dup := seen[id]; dup {
			continue
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids, nil
}

// PresentObjectIDs returns the sorted IDs of the objects the scene now shows:
// the object has a create payload and its removal flag is not raised.
func (d *Doc) PresentObjectIDs() ([]string, error) { return d.presentObjectIDs() }

func (d *Doc) presentObjectIDs() ([]string, error) {
	all, err := d.ObjectIDs()
	if err != nil {
		return nil, err
	}
	present := make([]string, 0, len(all))
	for _, id := range all {
		if d.removed(id) {
			continue
		}
		if _, ok := d.objectValue(id, fieldCreate); !ok {
			continue
		}
		present = append(present, id)
	}
	return present, nil
}

func (d *Doc) objectValue(objectID, field string) (string, bool) {
	value, _, err := d.doc.Get(crdt.Root, d.objectKey(objectID, field))
	if err != nil || value.Kind != crdt.ValueKindString {
		return "", false
	}
	return value.Str, true
}

func (d *Doc) slotValue(slot string) (string, bool) {
	value, _, err := d.doc.Get(crdt.Root, d.slotKey(slot))
	if err != nil || value.Kind != crdt.ValueKindString {
		return "", false
	}
	return value.Str, true
}

// -----------------------------------------------------------------------------
// Read path: CRDT change back to scene commands
// -----------------------------------------------------------------------------

// Watch registers fn to receive the command stream implied by every document
// change, local or replicated. Feed the stream to a mounted Scene3D surface
// through its applyCommands bridge, and the surface tracks the shared scene.
//
// A create always arrives as a removal followed by a create. The browser
// runtime MERGES a create payload into the record it already holds (see
// applySceneCreateCommand in client/js/bootstrap-src/10-runtime-scene-core.js),
// so a field the new record omits would keep its old value. The leading
// removal clears the record first. Removing an object the peer never had is a
// no-op in the runtime, which deletes from five maps by key.
//
// fn runs on the goroutine that committed or merged the change, after the
// document lock is released, so fn may read this Doc. Do not block in fn.
func (d *Doc) Watch(fn func([]scene.Command)) {
	if fn == nil {
		return
	}
	d.mu.Lock()
	d.watchers = append(d.watchers, fn)
	install := !d.hooked
	d.hooked = true
	d.mu.Unlock()
	if !install {
		return
	}
	d.doc.OnChange(func(patches []crdt.Patch) {
		commands := d.CommandsForPatches(patches)
		if len(commands) == 0 {
			return
		}
		d.mu.Lock()
		watchers := append([]func([]scene.Command){}, d.watchers...)
		d.mu.Unlock()
		for _, watcher := range watchers {
			watcher(commands)
		}
	})
}

// CommandsForPatches translates one batch of document patches into the scene
// commands that reproduce the change. A patch that belongs to another
// namespace, or to another feature sharing the document, is skipped.
//
// Call it directly when you already receive crdt patches from your own
// crdt.Doc.OnChange hook and do not want a second hook.
func (d *Doc) CommandsForPatches(patches []crdt.Patch) []scene.Command {
	var commands []scene.Command
	for _, patch := range patches {
		if patch.Obj != crdt.Root {
			continue
		}
		key, ok := d.parseKey(patch.Prop)
		if !ok || key.index {
			continue
		}
		if key.slot != "" {
			kind := kindForSlot(key.slot)
			if kind < 0 {
				continue
			}
			switch patch.Action {
			case "put":
				commands = append(commands, scene.Command{Kind: kind, Data: decodeData(patch.Value.Str)})
			}
			continue
		}
		switch key.field {
		case fieldCreate:
			if patch.Action != "put" {
				commands = append(commands, scene.RemoveObjectCommand(key.objectID))
				continue
			}
			if d.removed(key.objectID) {
				// A concurrent removal outranks this create, so the object is
				// not visible. Emitting the create would show an object the
				// converged document says is gone.
				commands = append(commands, scene.RemoveObjectCommand(key.objectID))
				continue
			}
			commands = append(commands,
				scene.RemoveObjectCommand(key.objectID),
				scene.Command{
					Kind:     scene.CommandCreateObject,
					ObjectID: key.objectID,
					Data:     decodeData(patch.Value.Str),
				})
		case fieldGone:
			if patch.Action == "put" && patch.Value.Kind == crdt.ValueKindBool && !patch.Value.Bool {
				// The object came back. Replay its stored create payload,
				// because the create itself is older than this patch.
				if payload, ok := d.objectValue(key.objectID, fieldCreate); ok && !d.removed(key.objectID) {
					commands = append(commands,
						scene.RemoveObjectCommand(key.objectID),
						scene.Command{
							Kind:     scene.CommandCreateObject,
							ObjectID: key.objectID,
							Data:     decodeData(payload),
						})
					continue
				}
			}
			commands = append(commands, scene.RemoveObjectCommand(key.objectID))
		case fieldTransform, fieldMaterial, fieldLight:
			if patch.Action != "put" {
				continue
			}
			commands = append(commands, scene.Command{
				Kind:     kindForField[key.field],
				ObjectID: key.objectID,
				Data:     decodeData(patch.Value.Str),
			})
		}
	}
	return commands
}

// -----------------------------------------------------------------------------
// Materializing the scene
// -----------------------------------------------------------------------------

// View is the scene one document describes. Compare two peers' Views to prove
// convergence; comparing document bytes proves far less, because two converged
// documents can hold the same state with a different change order.
type View struct {
	// IR carries every collection whose records decode into a typed scene
	// value. Objects, labels, sprites, HTML records, and lights are sorted by
	// ascending ID. See the package comment for why order is normalized.
	IR scene.SceneIR
	// Camera is the canonical IR camera the camera slot holds.
	Camera scene.IRCamera
	// Materials is the named material table the materials slot holds.
	Materials []scene.IRMaterial
	// PostEffects is the ordered post-FX chain, as raw JSON. It is NOT decoded,
	// because scene.PostEffectIR is an interface and encoding/json cannot
	// decode into it. See the report note on scene/postfx_ir.go.
	PostEffects json.RawMessage
	// PostUniforms is the named CustomPost uniform patch list, as raw JSON.
	PostUniforms json.RawMessage
	// Transforms, MaterialPatches, and LightPatches hold the per-object patch
	// payloads by object ID. The diff API never emits these three kinds, so
	// they stay raw; a caller that builds them by hand knows their shape.
	Transforms      map[string]json.RawMessage
	MaterialPatches map[string]json.RawMessage
	LightPatches    map[string]json.RawMessage
	// Slots keeps the exact bytes of every whole-collection value, so a caller
	// can compare a slot without trusting the typed decode above.
	Slots map[string]json.RawMessage
	// Removed lists the sorted IDs of objects the scene once had and no longer
	// shows. It proves a removal replicated instead of never arriving.
	Removed []string
}

// Empty reports whether the view shows no object and no collection. A
// convergence test must assert this is false. Two peers that both threw the
// scene away also agree, and a test that only compares them passes while
// proving nothing.
func (v View) Empty() bool {
	return len(v.IR.Objects) == 0 && len(v.IR.Labels) == 0 && len(v.IR.Sprites) == 0 &&
		len(v.IR.HTML) == 0 && len(v.IR.Lights) == 0 && len(v.Slots) == 0 &&
		len(v.Transforms) == 0 && len(v.MaterialPatches) == 0 && len(v.LightPatches) == 0
}

// Canonical renders the view as deterministic JSON. Two views compare equal
// exactly when their bytes match.
func (v View) Canonical() ([]byte, error) { return json.Marshal(v) }

// Visible returns the view without its removal history.
//
// Removed is history, not scene state. A document built by one diff from an
// empty scene never learned that an object once existed, while a document built
// by a sequence of diffs did. Both show the same scene. Compare Visible when the
// question is "do these two documents describe the same scene"; compare the
// whole View when the two documents must also share the same history.
func (v View) Visible() View {
	v.Removed = nil
	return v
}

// View materializes the scene the document describes.
//
// It returns an error when an object carries a create payload whose kind no
// typed record covers. The five kinds the diff API emits are an object, a
// label, a sprite, an HTML overlay, and a light. A loud error is deliberate:
// a silently skipped object is the defect class this package exists to close.
// Commands still round-trips such an object without loss, because it re-emits
// the stored bytes.
func (d *Doc) View() (View, error) {
	view := View{
		Transforms:      map[string]json.RawMessage{},
		MaterialPatches: map[string]json.RawMessage{},
		LightPatches:    map[string]json.RawMessage{},
		Slots:           map[string]json.RawMessage{},
	}

	for _, entry := range collectionOrder {
		payload, ok := d.slotValue(entry.slot)
		if !ok {
			continue
		}
		view.Slots[entry.slot] = json.RawMessage(payload)
	}
	if err := view.decodeSlots(); err != nil {
		return View{}, err
	}

	all, err := d.ObjectIDs()
	if err != nil {
		return View{}, err
	}
	for _, id := range all {
		if d.removed(id) {
			if _, ok := d.objectValue(id, fieldCreate); ok {
				view.Removed = append(view.Removed, id)
			}
			continue
		}
		payload, ok := d.objectValue(id, fieldCreate)
		if !ok {
			continue
		}
		if err := view.addCreate(id, payload); err != nil {
			return View{}, err
		}
		if raw, ok := d.objectValue(id, fieldTransform); ok {
			view.Transforms[id] = json.RawMessage(raw)
		}
		if raw, ok := d.objectValue(id, fieldMaterial); ok {
			view.MaterialPatches[id] = json.RawMessage(raw)
		}
		if raw, ok := d.objectValue(id, fieldLight); ok {
			view.LightPatches[id] = json.RawMessage(raw)
		}
	}
	return view, nil
}

// createEnvelope is the wire shape of scene.CommandPayload. It is redeclared
// here with a raw Props field so a decode keeps the exact record bytes.
type createEnvelope struct {
	Kind     string          `json:"kind,omitempty"`
	Geometry string          `json:"geometry,omitempty"`
	Props    json.RawMessage `json:"props,omitempty"`
}

func (v *View) addCreate(objectID, payload string) error {
	var envelope createEnvelope
	if err := json.Unmarshal([]byte(payload), &envelope); err != nil {
		return fmt.Errorf("scene3d: decode create payload for %q: %w", objectID, err)
	}
	switch envelope.Kind {
	case "":
		var record scene.ObjectIR
		if err := json.Unmarshal(envelope.Props, &record); err != nil {
			return fmt.Errorf("scene3d: decode object %q: %w", objectID, err)
		}
		v.IR.Objects = append(v.IR.Objects, record)
	case "label":
		var record scene.LabelIR
		if err := json.Unmarshal(envelope.Props, &record); err != nil {
			return fmt.Errorf("scene3d: decode label %q: %w", objectID, err)
		}
		v.IR.Labels = append(v.IR.Labels, record)
	case "sprite":
		var record scene.SpriteIR
		if err := json.Unmarshal(envelope.Props, &record); err != nil {
			return fmt.Errorf("scene3d: decode sprite %q: %w", objectID, err)
		}
		v.IR.Sprites = append(v.IR.Sprites, record)
	case "html":
		var record scene.HTMLIR
		if err := json.Unmarshal(envelope.Props, &record); err != nil {
			return fmt.Errorf("scene3d: decode html %q: %w", objectID, err)
		}
		v.IR.HTML = append(v.IR.HTML, record)
	case "light":
		var record scene.LightIR
		if err := json.Unmarshal(envelope.Props, &record); err != nil {
			return fmt.Errorf("scene3d: decode light %q: %w", objectID, err)
		}
		v.IR.Lights = append(v.IR.Lights, record)
	default:
		return fmt.Errorf("scene3d: object %q has unsupported create kind %q", objectID, envelope.Kind)
	}
	return nil
}

func (v *View) decodeSlots() error {
	if raw, ok := v.Slots[slotCamera]; ok {
		if err := json.Unmarshal(raw, &v.Camera); err != nil {
			return fmt.Errorf("scene3d: decode camera slot: %w", err)
		}
	}
	if raw, ok := v.Slots[slotMaterials]; ok {
		var payload struct {
			Materials []scene.IRMaterial `json:"materials"`
		}
		if err := json.Unmarshal(raw, &payload); err != nil {
			return fmt.Errorf("scene3d: decode materials slot: %w", err)
		}
		v.Materials = payload.Materials
	}
	if raw, ok := v.Slots[slotEnvironment]; ok {
		// The diff API sends two different types on this one command kind:
		// scene.EnvironmentIR from DiffCommands and scene.IREnvironment from
		// DiffIRCommands. Decoding the SceneIR shape keeps every shared field
		// and ignores the rest. Slots keeps the exact bytes either way.
		var payload struct {
			Environment scene.EnvironmentIR `json:"environment"`
		}
		if err := json.Unmarshal(raw, &payload); err != nil {
			return fmt.Errorf("scene3d: decode environment slot: %w", err)
		}
		v.IR.Environment = payload.Environment
	}
	if raw, ok := v.Slots[slotModels]; ok {
		var payload struct {
			Models []scene.ModelIR `json:"models"`
		}
		if err := json.Unmarshal(raw, &payload); err != nil {
			return fmt.Errorf("scene3d: decode models slot: %w", err)
		}
		v.IR.Models = payload.Models
	}
	if raw, ok := v.Slots[slotParticles]; ok {
		var payload struct {
			Points           []scene.PointsIR           `json:"points"`
			ComputeParticles []scene.ComputeParticlesIR `json:"computeParticles"`
			WaterSystems     []scene.WaterSystemIR      `json:"waterSystems"`
		}
		if err := json.Unmarshal(raw, &payload); err != nil {
			return fmt.Errorf("scene3d: decode particles slot: %w", err)
		}
		v.IR.Points = payload.Points
		v.IR.ComputeParticles = payload.ComputeParticles
		v.IR.WaterSystems = payload.WaterSystems
	}
	if raw, ok := v.Slots[slotInstancedMeshes]; ok {
		var payload struct {
			InstancedMeshes []scene.InstancedMeshIR `json:"instancedMeshes"`
		}
		if err := json.Unmarshal(raw, &payload); err != nil {
			return fmt.Errorf("scene3d: decode instancedMeshes slot: %w", err)
		}
		v.IR.InstancedMeshes = payload.InstancedMeshes
	}
	if raw, ok := v.Slots[slotInstancedGLBMeshes]; ok {
		var payload struct {
			InstancedGLBMeshes []scene.InstancedGLBMeshIR `json:"instancedGLBMeshes"`
		}
		if err := json.Unmarshal(raw, &payload); err != nil {
			return fmt.Errorf("scene3d: decode instancedGLBMeshes slot: %w", err)
		}
		v.IR.InstancedGLBMeshes = payload.InstancedGLBMeshes
	}
	if raw, ok := v.Slots[slotAnimations]; ok {
		var payload struct {
			Animations []scene.AnimationClipIR `json:"animations"`
		}
		if err := json.Unmarshal(raw, &payload); err != nil {
			return fmt.Errorf("scene3d: decode animations slot: %w", err)
		}
		v.IR.Animations = payload.Animations
	}
	if raw, ok := v.Slots[slotPostEffects]; ok {
		var payload struct {
			PostEffects     json.RawMessage `json:"postEffects"`
			PostFXMaxPixels int             `json:"postFXMaxPixels"`
		}
		if err := json.Unmarshal(raw, &payload); err != nil {
			return fmt.Errorf("scene3d: decode postEffects slot: %w", err)
		}
		v.PostEffects = payload.PostEffects
		v.IR.PostFXMaxPixels = payload.PostFXMaxPixels
	}
	if raw, ok := v.Slots[slotPostUniforms]; ok {
		var payload struct {
			Effects json.RawMessage `json:"effects"`
		}
		if err := json.Unmarshal(raw, &payload); err != nil {
			return fmt.Errorf("scene3d: decode postUniforms slot: %w", err)
		}
		v.PostUniforms = payload.Effects
	}
	return nil
}

// -----------------------------------------------------------------------------
// Server authority repair
// -----------------------------------------------------------------------------

// Rebase builds a fresh document from an authoritative sync frame and replays
// keep on top of it. Use it after the server refuses a change.
//
// A refused change cannot be repaired in place, and the reason is causal, not
// cosmetic. A CRDT change carries the hashes it depends on, and
// crdt.Doc.ReceiveSyncMessage holds a change back until every dependency
// arrives. Every local change a peer makes AFTER a refused one names the refused
// change as a dependency. The server therefore refuses the whole chain behind
// it, and a peer that only overwrites the refused VALUE stays stuck: its repair
// change depends on the refused change too. The peer must abandon that history.
//
// bootstrap is the sync frame the hub sends, with the one-byte document prefix
// already removed. A reconnecting client has exactly that frame. Pass nil to
// rebase onto an empty scene.
//
// Rebase returns the new Doc and a matching sync state. The state already marks
// every bootstrap change known, so the first frame the peer generates carries
// only the replayed commands. Discard the old document and the old state.
func Rebase(namespace string, bootstrap []byte, keep []scene.Command, message string) (*Doc, *crdtsync.State, error) {
	doc := crdt.NewDoc()
	state := crdtsync.NewState()
	if len(bootstrap) > 0 {
		if err := doc.ReceiveSyncMessage(state, bootstrap); err != nil {
			return nil, nil, fmt.Errorf("scene3d: rebase from sync frame: %w", err)
		}
	}
	bound, err := Bind(doc, namespace)
	if err != nil {
		return nil, nil, err
	}
	if _, err := bound.Apply(keep, message); err != nil {
		return nil, nil, err
	}
	return bound, state, nil
}
