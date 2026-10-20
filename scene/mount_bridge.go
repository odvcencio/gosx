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

// MountFrameSubmittedEvent acknowledges a requested complete renderer submission.
// It does not acknowledge browser compositor presentation or GPU completion.
const MountFrameSubmittedEvent = "gosx:scene3d:frame-submitted"

// MountFrameCancelEvent cancels a pending frame request on its owning mount.
const MountFrameCancelEvent = "gosx:scene3d:frame-cancel"

// MountFrameRequest asks for one submission containing every visible primitive
// of the specified imported model instances (for example "hero/slot-0").
// A new request supersedes the previous request. Pending shader compilation,
// partial frames, shadow-only draws, and absent models never acknowledge it.
type MountFrameRequest struct {
	ID       string   `json:"id"`
	ModelIDs []string `json:"modelIDs"`
}

// MountFrameCancel is dispatched as MountFrameCancelEvent. Only the matching
// pending request is cancelled; ID must not be empty.
type MountFrameCancel struct {
	ID string `json:"id"`
}

// MountFrameSubmission identifies the snapshot actually submitted. Revision
// identifies the requesting batch; CommandRevision may include later pose or
// camera commands. HydrationGeneration must still belong to the current mount.
type MountFrameSubmission struct {
	RequestID           string   `json:"requestID"`
	Revision            uint64   `json:"revision"`
	CommandRevision     uint64   `json:"commandRevision"`
	HydrationGeneration uint64   `json:"hydrationGeneration"`
	FrameSerial         uint64   `json:"frameSerial"`
	Renderer            string   `json:"renderer"`
	ModelIDs            []string `json:"modelIDs"`
	ObjectIDs           []string `json:"objectIDs"`
}

// MountCommandBatch is the typed payload accepted by MountCommandsEvent.
// Revision must be positive and strictly increase for each mounted surface.
type MountCommandBatch struct {
	Revision     uint64             `json:"revision"`
	Commands     []Command          `json:"commands"`
	FrameRequest *MountFrameRequest `json:"frameRequest,omitempty"`
}

// Marshal validates and encodes a mount command batch for CustomEvent.detail.
func (b MountCommandBatch) Marshal() ([]byte, error) {
	if b.Revision == 0 {
		return nil, errors.New("scene: mount command revision must be positive")
	}
	if b.FrameRequest != nil {
		if b.FrameRequest.ID == "" || len(b.FrameRequest.ModelIDs) == 0 || len(b.FrameRequest.ModelIDs) > 64 {
			return nil, errors.New("scene: frame request needs an ID and 1–64 model instance IDs")
		}
		for _, id := range b.FrameRequest.ModelIDs {
			if id == "" {
				return nil, errors.New("scene: frame request model instance ID must not be empty")
			}
		}
	}
	if b.Commands == nil {
		b.Commands = []Command{}
	}
	return json.Marshal(b)
}
