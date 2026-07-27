package scene3d

import (
	"fmt"

	"m31labs.dev/gosx/crdt"
	"m31labs.dev/gosx/hub"
)

// Target names what one document write touches. A Guard reads it to decide
// whether the client may perform that write.
//
// Exactly one of ObjectID, Slot, and Index is set:
//
//   - ObjectID names a per-object write, for example a moved mesh.
//   - Slot names a whole-collection write, for example "camera".
//   - Index is true when the write only extends the object index. The index
//     entry carries the object ID, so ObjectID is also set for an index write
//     and a Guard can refuse both together.
type Target struct {
	// Namespace is the Scene3D namespace the write lands in.
	Namespace string
	// ObjectID is the scene object the write changes. It is empty for a
	// whole-collection write.
	ObjectID string
	// Field is the per-object field code the write changes: "c" for the create
	// payload, "t" for a transform, "m" for a material, "l" for a light, and
	// "x" for the removal flag. It is empty for a whole-collection write.
	Field string
	// Slot is the whole collection the write replaces. It is empty for a
	// per-object write.
	Slot string
	// Index is true when the write appends to the object index.
	Index bool
}

// Guard reports whether client may perform the write named by target. Return
// false to refuse the write.
//
// The hub refuses the WHOLE inbound frame when a Guard refuses any write in
// it, because a CRDT change is atomic: applying half of it would leave the
// document in a state no peer ever produced.
type Guard func(client *hub.Client, target Target) bool

// AllowAll accepts every write. Use it to replicate a scene with no per-client
// authority, and note that a client may then move any object.
func AllowAll(*hub.Client, Target) bool { return true }

// ChangeGate returns a hub.BinaryChangeAuthorizer that enforces guard over the
// document registered under docName.
//
// The gate fails closed inside the namespace: it refuses a write whose key it
// cannot parse, because an unparsable key inside the namespace is either a bug
// or an attempt to write past the schema. It passes a write OUTSIDE the
// namespace to next, so several features can share one document. A nil next
// accepts those writes.
//
// A client that pushes only sync metadata, with no change, is always accepted.
// The hub already documents that behavior; the gate does not tighten it,
// because refusing a metadata frame would stall the sync round.
func ChangeGate(docName string, d *Doc, guard Guard, next hub.BinaryChangeAuthorizer) hub.BinaryChangeAuthorizer {
	if guard == nil {
		guard = AllowAll
	}
	return func(client *hub.Client, name string, changes []crdt.Change) error {
		if name != docName {
			if next == nil {
				return nil
			}
			return next(client, name, changes)
		}
		var foreign []crdt.Change
		for _, change := range changes {
			mine, outside := d.splitOps(change)
			if len(outside) > 0 {
				copied := change
				copied.Ops = outside
				foreign = append(foreign, copied)
			}
			for _, target := range mine {
				if guard(client, target) {
					continue
				}
				return fmt.Errorf("scene3d: client may not write %s", target)
			}
		}
		if len(foreign) == 0 || next == nil {
			return nil
		}
		return next(client, name, foreign)
	}
}

// String renders a Target for an error message and a log line.
func (t Target) String() string {
	switch {
	case t.Index:
		return fmt.Sprintf("index entry %q in namespace %q", t.ObjectID, t.Namespace)
	case t.Slot != "":
		return fmt.Sprintf("collection %q in namespace %q", t.Slot, t.Namespace)
	case t.ObjectID != "":
		return fmt.Sprintf("object %q field %q in namespace %q", t.ObjectID, t.Field, t.Namespace)
	default:
		return fmt.Sprintf("unrecognized key in namespace %q", t.Namespace)
	}
}

// splitOps sorts the operations of one change into the targets inside this
// namespace and the operations outside it.
//
// An operation on the object index list is reported with the inserted object
// ID, so a Guard that refuses an object also refuses its index entry. Without
// that, a client could not create an object but could still grow the index.
func (d *Doc) splitOps(change crdt.Change) (targets []Target, outside []crdt.Op) {
	for _, op := range change.Ops {
		switch op.Obj {
		case crdt.Root:
			key, ok := d.parseKey(op.Prop)
			if !ok {
				if !isNamespaced(string(op.Prop), d.ns) {
					outside = append(outside, op)
					continue
				}
				// Inside the namespace but off-schema. Fail closed.
				targets = append(targets, Target{Namespace: d.ns})
				continue
			}
			if key.index {
				// The root key that declares the index list itself carries no
				// object, so it needs no per-object check.
				continue
			}
			targets = append(targets, Target{
				Namespace: d.ns,
				ObjectID:  key.objectID,
				Field:     key.field,
				Slot:      key.slot,
			})
		case d.indexObj:
			targets = append(targets, Target{
				Namespace: d.ns,
				ObjectID:  op.Value.Str,
				Index:     true,
			})
		default:
			outside = append(outside, op)
		}
	}
	return targets, outside
}

// isNamespaced reports whether key belongs to this namespace at all. A root key
// that starts with the namespace and a slash is ours even when the rest of the
// key is off-schema.
func isNamespaced(key, namespace string) bool {
	return len(key) > len(namespace) && key[:len(namespace)] == namespace && key[len(namespace)] == '/'
}

// Serve registers d for binary sync on h under docName and installs the write
// gate. Call it once per hub.
//
// The hub holds ONE change authorizer, so a second Serve call on the same hub
// replaces the first gate. Build a chain with ChangeGate and install it with
// hub.Hub.SetBinaryChangeAuthorizer when one hub carries several documents.
//
// Serve gates INBOUND writes only. The hub never gates server-to-client sync,
// so a client that may not write still receives live state. Install a
// hub.BinaryReadAuthorizer when a client must not even read the scene.
func Serve(h *hub.Hub, docName string, d *Doc, guard Guard) error {
	if h == nil {
		return fmt.Errorf("scene3d: nil hub")
	}
	if d == nil {
		return fmt.Errorf("scene3d: nil document")
	}
	if docName == "" {
		return fmt.Errorf("scene3d: empty document name")
	}
	h.SetBinaryChangeAuthorizer(ChangeGate(docName, d, guard, nil))
	h.SyncDoc(docName, d.Doc())
	return nil
}
