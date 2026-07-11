package scene

import (
	"encoding/json"
	"errors"
)

// MountCommandsEvent is dispatched on a Scene3D mount to apply a revisioned
// command batch. Revisions are strictly increasing per mount; stale or replayed
// batches are ignored by the browser runtime.
const MountCommandsEvent = "gosx:scene3d:commands"

// MountInputEvent is emitted by a Scene3D mount for semantic renderer input
// such as a pick. Page-owned engines can route the same input into their
// semantic DOM controls without depending on renderer internals.
const MountInputEvent = "gosx:scene3d:input"

// MountCommandsAppliedEvent is emitted after a batch has been accepted and its
// synchronous or asynchronous renderer work has completed.
const MountCommandsAppliedEvent = "gosx:scene3d:commands-applied"

// MountCommandBatch is the typed payload accepted by MountCommandsEvent.
// Revision must be positive and strictly increase for each mounted surface.
type MountCommandBatch struct {
	Revision uint64    `json:"revision"`
	Commands []Command `json:"commands"`
}

// Marshal validates and encodes a mount command batch for CustomEvent.detail.
func (b MountCommandBatch) Marshal() ([]byte, error) {
	if b.Revision == 0 {
		return nil, errors.New("scene: mount command revision must be positive")
	}
	if b.Commands == nil {
		b.Commands = []Command{}
	}
	return json.Marshal(b)
}
