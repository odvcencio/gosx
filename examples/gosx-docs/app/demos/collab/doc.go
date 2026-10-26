package collab

import (
	"html"
	"sync"
	"time"

	"m31labs.dev/gosx/content"
)

const (
	defaultDocText = "# A shared page\n\nWrite here, then open this page in another tab. Both editors follow the same document.\n\n## Try it\n\n- Change this paragraph in one tab.\n- Watch the preview update in both tabs.\n- Move your cursor and look for it in the other editor.\n\n> The last accepted edit wins when two people type at once.\n"
	maxDocBytes    = 64 * 1024 // cap at 64 KB to prevent abuse
)

// DocState is the wire-format snapshot of the document.
type DocState struct {
	Text      string `json:"text"`
	HTML      string `json:"html"`
	Version   uint64 `json:"version"`
	UpdatedAt int64  `json:"updatedAt"` // unix millis
}

// Doc is an in-memory LWW document store.
type Doc struct {
	mu       sync.RWMutex
	text     string
	version  uint64
	html     string
	utf16Len int
}

// NewDoc creates a new document seeded with the given text at version 0.
func NewDoc(seed string) *Doc {
	return &Doc{text: seed, html: documentPreview(seed), utf16Len: utf16Length(seed)}
}

// State returns a consistent snapshot of the document.
func (d *Doc) State() DocState {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.stateLocked()
}

func (d *Doc) stateLocked() DocState {
	return DocState{
		Text:      d.text,
		HTML:      d.html,
		Version:   d.version,
		UpdatedAt: time.Now().UnixMilli(),
	}
}

// UTF16Len matches native textarea selection coordinates, including non-BMP
// characters. Cache it when content changes, not for every cursor broadcast.
func (d *Doc) UTF16Len() int {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.utf16Len
}

func utf16Length(text string) int {
	n := 0
	for _, r := range text {
		n++
		if r > 0xffff {
			n++
		}
	}
	return n
}

func documentPreview(text string) string {
	doc, err := content.ParseDocumentWithOptions("collab", "document.md", text, content.LoadOptions{})
	if err == nil {
		if rendered, renderErr := doc.HTML(content.MDPPOptions{SourcePositions: true, Sanitize: true}); renderErr == nil {
			return rendered
		}
	}
	return "<pre>" + html.EscapeString(text) + "</pre>"
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
		return d.State(), false
	}
	d.mu.RLock()
	stale := incomingVersion < d.version
	d.mu.RUnlock()
	if stale {
		return d.State(), false
	}
	// Parse outside the document lock. The second version check below protects
	// against a different writer committing while the preview was rendered.
	preview := documentPreview(incomingText)
	length := utf16Length(incomingText)

	d.mu.Lock()
	defer d.mu.Unlock()

	if incomingVersion < d.version {
		// Stale — reject, return current so caller can re-sync client.
		return d.stateLocked(), false
	}

	// Accept.
	d.text = incomingText
	d.html = preview
	d.utf16Len = length
	d.version++
	return d.stateLocked(), true
}
