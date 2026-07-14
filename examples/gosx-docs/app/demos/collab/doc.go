package collab

import (
	"sync"
	"time"
)

const (
	defaultDocText = "# Welcome to the GoSX Hub Sync demo\n\nThis is a deliberately small **last-write-wins synchronization** example.\n\nOpen this page in two tabs and start typing — edits sync through one hub with 100ms debounce.\n\n## What it proves\n\n- Live preview rendered in the browser\n- Snapshot sync over a GoSX WebSocket hub\n- Explicit connected, pending, and synced states\n\n## Deliberate limits\n\n- One shared in-memory document\n- No persistence or rooms\n- No cursors or presence\n- No CRDT or conflict merging: the last accepted snapshot wins\n\n> Try it: open two tabs side by side and watch the versioned snapshots converge.\n"
	maxDocBytes    = 64 * 1024 // cap at 64 KB to prevent abuse
)

// DocState is the wire-format snapshot of the document.
type DocState struct {
	Text      string `json:"text"`
	Version   uint64 `json:"version"`
	UpdatedAt int64  `json:"updatedAt"` // unix millis
}

// Doc is an in-memory LWW document store.
type Doc struct {
	mu      sync.RWMutex
	text    string
	version uint64
}

// NewDoc creates a new document seeded with the given text at version 0.
func NewDoc(seed string) *Doc {
	return &Doc{text: seed}
}

// State returns a consistent snapshot of the document.
func (d *Doc) State() DocState {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return DocState{
		Text:      d.text,
		Version:   d.version,
		UpdatedAt: time.Now().UnixMilli(),
	}
}

// Apply attempts to apply an incoming edit using LWW semantics.
//
// The edit is accepted if:
//   - incomingVersion >= d.version (fresh or equal — accept and advance)
//
// The edit is rejected if:
//   - incomingVersion < d.version (stale — caller should broadcast current state)
//   - len(incomingText) > maxDocBytes (oversized)
//
// Returns the resulting DocState (current after apply) and whether the edit
// was accepted.
func (d *Doc) Apply(incomingText string, incomingVersion uint64) (DocState, bool) {
	if len(incomingText) > maxDocBytes {
		d.mu.RLock()
		state := DocState{
			Text:      d.text,
			Version:   d.version,
			UpdatedAt: time.Now().UnixMilli(),
		}
		d.mu.RUnlock()
		return state, false
	}

	d.mu.Lock()
	defer d.mu.Unlock()

	if incomingVersion < d.version {
		// Stale — reject, return current so caller can re-sync client.
		return DocState{
			Text:      d.text,
			Version:   d.version,
			UpdatedAt: time.Now().UnixMilli(),
		}, false
	}

	// Accept.
	d.text = incomingText
	d.version++
	return DocState{
		Text:      d.text,
		Version:   d.version,
		UpdatedAt: time.Now().UnixMilli(),
	}, true
}
