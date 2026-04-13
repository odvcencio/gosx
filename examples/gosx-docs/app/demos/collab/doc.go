package collab

import (
	"sync"
	"time"
)

const (
	defaultDocText = "# Welcome to the GoSX Collab demo\n\nThis is a **real-time collaborative** markdown editor.\n\nOpen this page in two tabs and start typing — edits sync instantly via a hub using a _last-write-wins_ model with 100ms debounce.\n\n## Features\n\n- Live preview rendered in the browser\n- LWW sync over a WebSocket hub\n- No persistence — everything is in memory\n- No cursors, no CRDT — just simple and fast\n\n## How it works\n\n1. You type in the `SOURCE` pane\n2. The client debounces 100ms, then sends `doc:edit` to the hub\n3. The server applies the edit (LWW: last version wins)\n4. The server broadcasts `doc:update` to all connected clients\n5. Each client renders the preview\n\n> Try it: open two tabs side by side.\n"
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
