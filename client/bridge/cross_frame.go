package bridge

import (
	"fmt"
	"strings"
)

// CrossFrameRelayConfig describes one registered cross-frame relay binding.
//
// See ADR 0009 (decisions/0009-iframe-transport-postmessage-relay.md) for the
// architectural rationale: editor-side $preview.* shared-signal writes need
// to reach the storefront iframe's Bridge, but js.Global() is per-frame.
// The relay opt-in lets a Bridge announce "signals matching <prefix> should
// also cross the postMessage boundary to peers running my allowed origin."
//
// The Prefix is treated as a literal prefix match against signal names; the
// canonical use is "$preview." so that $preview.block.<key>.visible,
// $preview.field.<name>, etc., all relay through a single registration.
//
// AllowedOrigin gates inbound traffic: messages whose originating frame
// origin does not match are dropped. The literal "*" wildcard is permitted
// for dev-mode use only — call sites should audit and DevModeOrigin()
// returns true in that case so the JS-side bootstrap can emit a console
// warning.
type CrossFrameRelayConfig struct {
	Prefix        string
	AllowedOrigin string
}

// DevModeOrigin reports whether this relay was registered with the wildcard
// origin. Production deployments should always pin the peer origin.
func (c CrossFrameRelayConfig) DevModeOrigin() bool {
	return strings.TrimSpace(c.AllowedOrigin) == "*"
}

// EnableCrossFrameRelay opts this Bridge into shared-signal relay with peer
// Bridges in other frames. Signal names matching prefix (e.g. "$preview.")
// cross the postMessage boundary on write; inbound messages from peers are
// routed back into the local store, fanning out to local subscribers via the
// existing shared-signal infrastructure.
//
// allowedOrigin is the expected peer origin (e.g. "https://editor.example.com");
// use "*" only in development. Idempotent for the same (prefix, allowedOrigin)
// pair — repeated calls do not duplicate the registration. Multiple distinct
// prefixes may be registered on one Bridge.
//
// An empty prefix is rejected silently — relaying every signal would break
// frame-local semantics for non-preview namespaces.
//
// See ADR 0009 (decisions/0009-iframe-transport-postmessage-relay.md) and
// plan section A of plans/2026-05-26-iframe-cross-frame-signal-transport.md.
func (b *Bridge) EnableCrossFrameRelay(prefix, allowedOrigin string) {
	if b == nil {
		return
	}
	prefix = strings.TrimSpace(prefix)
	if prefix == "" {
		// Refuse to relay every signal — frame-local namespaces stay local.
		return
	}
	for _, existing := range b.crossFrameRelays {
		if existing.Prefix == prefix && existing.AllowedOrigin == allowedOrigin {
			return // idempotent
		}
	}
	b.crossFrameRelays = append(b.crossFrameRelays, CrossFrameRelayConfig{
		Prefix:        prefix,
		AllowedOrigin: strings.TrimSpace(allowedOrigin),
	})
}

// CrossFrameRelays returns a copy of the registered relay configurations.
// Used by the wasm-side message listener to expose them to JS for outbound
// peer selection and inbound origin validation.
func (b *Bridge) CrossFrameRelays() []CrossFrameRelayConfig {
	if b == nil || len(b.crossFrameRelays) == 0 {
		return nil
	}
	out := make([]CrossFrameRelayConfig, len(b.crossFrameRelays))
	copy(out, b.crossFrameRelays)
	return out
}

// relayMatches reports whether the given signal name matches a registered
// relay prefix. Helper used by both the outbound dispatch path
// (notifySharedSignal → outbound relay) and the inbound dispatch path
// (DispatchInboundSignal — verify the prefix is one we agreed to receive).
func (b *Bridge) relayMatches(name string) bool {
	if b == nil || len(b.crossFrameRelays) == 0 {
		return false
	}
	for _, cfg := range b.crossFrameRelays {
		if strings.HasPrefix(name, cfg.Prefix) {
			return true
		}
	}
	return false
}

// relayMatchForOrigin returns the relay config matching the given signal
// name AND accepting the given origin. Used by DispatchInboundSignal to
// gate both prefix and origin.
func (b *Bridge) relayMatchForOrigin(name, origin string) (CrossFrameRelayConfig, bool) {
	if b == nil {
		return CrossFrameRelayConfig{}, false
	}
	for _, cfg := range b.crossFrameRelays {
		if !strings.HasPrefix(name, cfg.Prefix) {
			continue
		}
		if cfg.AllowedOrigin == "*" || cfg.AllowedOrigin == origin {
			return cfg, true
		}
	}
	return CrossFrameRelayConfig{}, false
}

// SetCrossFrameRelaySendCallback registers the function called when a
// shared-signal write needs to relay to a peer frame. The wasm-side
// registers this callback to invoke window.__gosx_relay_send(name, valueJSON)
// which dispatches a postMessage to the peer.
//
// The callback fires AFTER local fan-out for matching names; non-matching
// names are not relayed. Inbound writes (via DispatchInboundSignal) do NOT
// trigger this callback — see relayInboundDepth to prevent loops.
func (b *Bridge) SetCrossFrameRelaySendCallback(fn func(name, valueJSON string)) {
	if b == nil {
		return
	}
	b.relaySendFn = fn
}

// DispatchInboundSignal accepts a signal write that arrived from a peer frame
// (via postMessage). It validates the prefix + origin, then routes the value
// into the local store — fanning out to local subscribers via the existing
// shared-signal infrastructure. The store observer's outbound relay path is
// suppressed during this call so the message does not echo back to the peer.
//
// Returns an error and drops the write if:
//   - name does not match any registered relay prefix
//   - origin does not match the allowed origin for the matching prefix
//
// The wasm-side message listener calls this from inside its js.FuncOf
// handler.
func (b *Bridge) DispatchInboundSignal(name, valueJSON, originatingOrigin string) error {
	if b == nil {
		return fmt.Errorf("bridge is nil")
	}
	if _, ok := b.relayMatchForOrigin(name, originatingOrigin); !ok {
		// Determine which gate failed for a clearer error.
		if !b.relayMatches(name) {
			return fmt.Errorf("cross-frame relay: signal %q does not match any registered prefix", name)
		}
		return fmt.Errorf("cross-frame relay: origin %q is not allowed for signal %q", originatingOrigin, name)
	}
	b.relayInboundDepth++
	defer func() {
		if b.relayInboundDepth > 0 {
			b.relayInboundDepth--
		}
	}()
	return b.SetSharedSignalJSON(name, valueJSON)
}

// relaySharedSignal is called by the store observer (alongside
// notifySharedSignal) when a shared signal value changes. If the write
// originated locally AND the name matches a registered relay prefix, the
// outbound relay callback fires so the wasm-side can postMessage to peers.
//
// Inbound writes (driven by DispatchInboundSignal) bump relayInboundDepth so
// the observer skips this outbound step — preventing a write-from-peer from
// looping back to the peer.
func (b *Bridge) relaySharedSignal(name, valueJSON string) {
	if b == nil || b.relaySendFn == nil {
		return
	}
	if b.relayInboundDepth > 0 {
		return // suppress echo of inbound writes
	}
	if !b.relayMatches(name) {
		return
	}
	b.relaySendFn(name, valueJSON)
}
