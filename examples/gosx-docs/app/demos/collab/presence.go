package collab

import (
	"hash/fnv"
	"sync"

	"m31labs.dev/gosx/examples/gosx-docs/app/demos/democtl"
)

// Identity is the stable display name + accent color assigned to one
// connected editor for the lifetime of its hub connection.
type Identity struct {
	Name  string `json:"name"`
	Color string `json:"color"`
}

// PresenceEvent is broadcast whenever the set of connected editors changes.
type PresenceEvent struct {
	Count int `json:"count"`
}

// CursorEvent is broadcast whenever a connected editor's caret or selection
// moves. Offset/SelEnd are character offsets into the shared document text
// (matching the JS textarea's selectionStart/selectionEnd indexing).
type CursorEvent struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Color  string `json:"color"`
	Offset int    `json:"offset"`
	SelEnd int    `json:"selEnd"`
}

// CursorLeaveEvent is broadcast when an editor disconnects, so peers can drop
// its caret marker immediately instead of waiting for a client-side
// staleness timeout.
type CursorLeaveEvent struct {
	ID string `json:"id"`
}

// identityForClientID derives a deterministic Identity from a hub client ID
// by hashing it into democtl's shared name/color pools. The hub assigns a
// fresh random client ID per connection (see hub.generateClientID), so this
// gives every connection a stable-for-its-lifetime, id-derived identity
// without introducing any new shared mutable state or coordinating with
// democtl itself — this package only consumes democtl's exported pools, it
// does not modify them.
func identityForClientID(clientID string) Identity {
	names := democtl.NamePool()
	colors := democtl.ColorPool()
	if len(names) == 0 || len(colors) == 0 {
		return Identity{}
	}

	sum := fnv.New32a()
	_, _ = sum.Write([]byte(clientID))
	h := sum.Sum32()

	// Split the hash into two independent-ish indices so name and color
	// don't covary in lockstep for adjacent/similar client IDs.
	nameIdx := int(h % uint32(len(names)))
	colorIdx := int((h / uint32(len(names))) % uint32(len(colors)))
	return Identity{Name: names[nameIdx], Color: colors[colorIdx]}
}

// cursorState is one tracked editor's last known caret/selection.
type cursorState struct {
	Offset int
	SelEnd int
}

// roster tracks connected editors: their deterministic Identity and their
// last known cursor position. It is the collab demo's own presence
// bookkeeping, kept separate from hub.Hub's internal Presence tracker (which
// only counts client IDs — it has no notion of identity or cursor).
//
// roster is safe for concurrent use.
type roster struct {
	mu      sync.RWMutex
	members map[string]Identity
	cursors map[string]cursorState
}

func newRoster() *roster {
	return &roster{
		members: make(map[string]Identity),
		cursors: make(map[string]cursorState),
	}
}

// join assigns and records an Identity for a newly connected client,
// returning it so the caller can tell the client about itself.
func (r *roster) join(clientID string) Identity {
	id := identityForClientID(clientID)
	r.mu.Lock()
	r.members[clientID] = id
	r.mu.Unlock()
	return id
}

// leave forgets a disconnected client's identity and cursor.
func (r *roster) leave(clientID string) {
	r.mu.Lock()
	delete(r.members, clientID)
	delete(r.cursors, clientID)
	r.mu.Unlock()
}

// count returns the number of tracked (currently connected) editors.
func (r *roster) count() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.members)
}

// updateCursor records a client's latest cursor position and returns the
// CursorEvent to broadcast. ok is false if the client hasn't joined (e.g. a
// cursor:update arriving after a race with disconnect) — callers should
// silently drop the event in that case.
func (r *roster) updateCursor(clientID string, offset, selEnd int) (evt CursorEvent, ok bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	id, known := r.members[clientID]
	if !known {
		return CursorEvent{}, false
	}
	r.cursors[clientID] = cursorState{Offset: offset, SelEnd: selEnd}
	return CursorEvent{
		ID:     clientID,
		Name:   id.Name,
		Color:  id.Color,
		Offset: offset,
		SelEnd: selEnd,
	}, true
}

// snapshot returns the current cursor of every tracked editor that has
// reported one. Used to seed a newly joined client with the peers it can't
// otherwise learn about until their next caret move.
func (r *roster) snapshot() []CursorEvent {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]CursorEvent, 0, len(r.cursors))
	for id, cur := range r.cursors {
		identity := r.members[id]
		out = append(out, CursorEvent{
			ID:     id,
			Name:   identity.Name,
			Color:  identity.Color,
			Offset: cur.Offset,
			SelEnd: cur.SelEnd,
		})
	}
	return out
}

// clampInt clamps v into the inclusive range [lo, hi].
func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
