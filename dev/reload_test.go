package dev

import (
	"encoding/json"
	"testing"
)

// TriggerReload is the public entry point for callers that watch sources the
// built-in watcher ignores (e.g. a deck server watching deck.md) and need to
// drive a full reload through the same SSE pipeline the file watcher uses. It
// must broadcast a "reload" event carrying the given reason.
func TestTriggerReloadBroadcastsReload(t *testing.T) {
	s := &Server{Dir: t.TempDir()}
	events := captureEvents(t, s)

	s.TriggerReload("deck.md changed")

	got := drainEvents(events)
	ev, ok := got["reload"]
	if !ok {
		t.Fatalf("TriggerReload must emit a reload event; got %v", keys(got))
	}
	var payload struct {
		Reason string `json:"reason"`
	}
	if err := json.Unmarshal([]byte(ev.Data), &payload); err != nil {
		t.Fatalf("reload payload not JSON: %v (%s)", err, ev.Data)
	}
	if payload.Reason != "deck.md changed" {
		t.Fatalf("reload reason = %q, want %q", payload.Reason, "deck.md changed")
	}
}
