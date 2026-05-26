package bridge

import (
	"strings"
	"testing"
)

// TestEnableCrossFrameRelayRegistersPrefix verifies that EnableCrossFrameRelay
// records the prefix + allowed origin and that the public CrossFrameRelays()
// accessor surfaces them for inspection by the wasm-side message listener.
//
// Plan A.1 / A.2 — see plans/2026-05-26-iframe-cross-frame-signal-transport.md.
func TestEnableCrossFrameRelayRegistersPrefix(t *testing.T) {
	b := New()
	b.EnableCrossFrameRelay("$preview.", "https://editor.example")

	relays := b.CrossFrameRelays()
	if len(relays) != 1 {
		t.Fatalf("expected 1 relay, got %d", len(relays))
	}
	if relays[0].Prefix != "$preview." {
		t.Fatalf("unexpected prefix: %q", relays[0].Prefix)
	}
	if relays[0].AllowedOrigin != "https://editor.example" {
		t.Fatalf("unexpected origin: %q", relays[0].AllowedOrigin)
	}
}

// TestEnableCrossFrameRelayMatchPrefix verifies prefix-matching semantics.
func TestEnableCrossFrameRelayMatchPrefix(t *testing.T) {
	b := New()
	b.EnableCrossFrameRelay("$preview.", "*")

	if !b.relayMatches("$preview.block.hero.visible") {
		t.Fatal("expected $preview.block.* to match $preview. prefix")
	}
	if b.relayMatches("$selection.block.hero") {
		t.Fatal("$selection.* should not match a $preview. prefix")
	}
}

// TestEnableCrossFrameRelayEmptyPrefixIsRejected ensures the empty prefix is
// rejected — it would relay every signal write and break frame-local
// semantics.
func TestEnableCrossFrameRelayEmptyPrefixIsRejected(t *testing.T) {
	b := New()
	b.EnableCrossFrameRelay("", "*")

	if len(b.CrossFrameRelays()) != 0 {
		t.Fatalf("expected empty prefix to be rejected, got %d relays", len(b.CrossFrameRelays()))
	}
}

// TestEnableCrossFrameRelayDevModeOriginWarns verifies that the "*" origin
// (dev-mode wildcard) is permitted but flagged so callers can audit.
//
// Plan A.5 / ADR 0009 — origin validation is non-optional; "*" is dev-only.
func TestEnableCrossFrameRelayDevModeOriginAllowed(t *testing.T) {
	b := New()
	b.EnableCrossFrameRelay("$preview.", "*")
	if len(b.CrossFrameRelays()) != 1 {
		t.Fatal("dev-mode origin should still register the relay")
	}
	if !b.CrossFrameRelays()[0].DevModeOrigin() {
		t.Fatal("expected DevModeOrigin() to flag the wildcard")
	}
}

// TestDispatchInboundSignalRoutesToStore verifies that an inbound signal
// (from a peer frame) writes into the local store so subscribers see it.
//
// Plan A.3 / A.4.
func TestDispatchInboundSignalRoutesToStore(t *testing.T) {
	b := New()
	b.EnableCrossFrameRelay("$preview.", "https://editor.example")

	err := b.DispatchInboundSignal("$preview.block.hero.visible", `true`, "https://editor.example")
	if err != nil {
		t.Fatalf("DispatchInboundSignal: %v", err)
	}

	raw, err := b.GetSharedSignalJSON("$preview.block.hero.visible")
	if err != nil {
		t.Fatalf("GetSharedSignalJSON: %v", err)
	}
	if raw != "true" {
		t.Fatalf("expected inbound write to land in store, got %q", raw)
	}
}

// TestDispatchInboundSignalRejectsBadOrigin verifies that a message from an
// origin NOT matching the allowed origin is dropped.
//
// Plan B.5 / B.6 — origin validation is non-optional in production.
func TestDispatchInboundSignalRejectsBadOrigin(t *testing.T) {
	b := New()
	b.EnableCrossFrameRelay("$preview.", "https://editor.example")

	err := b.DispatchInboundSignal("$preview.foo", `1`, "https://attacker.example")
	if err == nil {
		t.Fatal("expected DispatchInboundSignal to reject mismatched origin")
	}
	if !strings.Contains(err.Error(), "origin") {
		t.Fatalf("expected origin-related error, got %v", err)
	}

	raw, _ := b.GetSharedSignalJSON("$preview.foo")
	if raw != "null" {
		t.Fatalf("rejected message should NOT land in store, got %q", raw)
	}
}

// TestDispatchInboundSignalDevModeAcceptsAny verifies that "*" allowed origin
// accepts any origin (dev mode).
func TestDispatchInboundSignalDevModeAcceptsAny(t *testing.T) {
	b := New()
	b.EnableCrossFrameRelay("$preview.", "*")

	if err := b.DispatchInboundSignal("$preview.foo", `1`, "https://anywhere.example"); err != nil {
		t.Fatalf("dev-mode relay should accept any origin: %v", err)
	}
}

// TestDispatchInboundSignalRejectsUnmatchedPrefix verifies that signals not
// matching any registered prefix are dropped — non-relayed signals stay
// frame-local.
//
// Plan A.4 — prefix gating is non-optional.
func TestDispatchInboundSignalRejectsUnmatchedPrefix(t *testing.T) {
	b := New()
	b.EnableCrossFrameRelay("$preview.", "*")

	err := b.DispatchInboundSignal("$selection.block.hero", `"sel"`, "https://editor.example")
	if err == nil {
		t.Fatal("expected unmatched-prefix to be rejected")
	}
	if !strings.Contains(err.Error(), "prefix") {
		t.Fatalf("expected prefix-related error, got %v", err)
	}
}

// TestRelayOutboundCallbackFiresOnMatch verifies that when a relayed signal
// is written locally, the outbound relay callback fires (so the wasm-side
// can postMessage to the peer frame). Non-matching writes do NOT fire the
// relay callback (they only fan out locally).
//
// Plan A.5 / A.6 — outbound relay hook.
func TestRelayOutboundCallbackFiresOnMatch(t *testing.T) {
	b := New()
	b.EnableCrossFrameRelay("$preview.", "*")

	type event struct {
		name      string
		valueJSON string
	}
	var sent []event
	b.SetCrossFrameRelaySendCallback(func(name, valueJSON string) {
		sent = append(sent, event{name, valueJSON})
	})

	if err := b.SetSharedSignalJSON("$preview.block.hero.visible", `true`); err != nil {
		t.Fatalf("SetSharedSignalJSON: %v", err)
	}
	if err := b.SetSharedSignalJSON("$selection.block.hero", `"sel"`); err != nil {
		t.Fatalf("SetSharedSignalJSON: %v", err)
	}

	if len(sent) != 1 {
		t.Fatalf("expected 1 relay send (only $preview.*), got %d: %+v", len(sent), sent)
	}
	if sent[0].name != "$preview.block.hero.visible" || sent[0].valueJSON != "true" {
		t.Fatalf("unexpected relay send: %+v", sent[0])
	}
}

// TestRelayOutboundCallbackDoesNotEchoInboundWrites verifies that signal
// writes that arrived FROM a peer (via DispatchInboundSignal) do not
// trigger an outbound relay back to the peer — otherwise we'd loop.
//
// Plan A.7 / A.8 — bidirectional relay must not echo.
func TestRelayOutboundCallbackDoesNotEchoInboundWrites(t *testing.T) {
	b := New()
	b.EnableCrossFrameRelay("$preview.", "*")

	var sent []string
	b.SetCrossFrameRelaySendCallback(func(name, valueJSON string) {
		sent = append(sent, name)
	})

	if err := b.DispatchInboundSignal("$preview.from.peer", `1`, "https://peer.example"); err != nil {
		t.Fatalf("DispatchInboundSignal: %v", err)
	}

	if len(sent) != 0 {
		t.Fatalf("inbound signal should NOT echo back as outbound relay, got %d sends: %v", len(sent), sent)
	}
}
