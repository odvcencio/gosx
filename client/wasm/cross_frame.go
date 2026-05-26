//go:build js && wasm

package main

import (
	"strings"
	"syscall/js"

	"m31labs.dev/gosx/client/bridge"
)

// registerCrossFrameRelay wires the bridge's cross-frame relay opt-in
// (Bridge.EnableCrossFrameRelay) to the JS-side postMessage transport
// installed by client/js/relay.js. See ADR 0009
// (decisions/0009-iframe-transport-postmessage-relay.md) and plan section A
// of plans/2026-05-26-iframe-cross-frame-signal-transport.md.
//
// Wire contract:
//   - __gosx_enable_cross_frame_relay(prefix, allowedOrigin): WASM-side
//     entry point the bootstrap calls (after detecting preview-mode via
//     query param). Registers the prefix on the Bridge and pushes the
//     configuration into the JS relay so its message listener begins
//     accepting matching traffic.
//   - __gosx_relay_dispatch_inbound(name, valueJSON, origin): called by
//     the JS message listener when an inbound message arrives.
//     Delegates to Bridge.DispatchInboundSignal which validates and
//     routes into the local store.
//   - Outbound (Bridge → JS): registers a Bridge relay-send callback
//     that invokes window.__gosx_relay_send(name, valueJSON) so the JS
//     side can postMessage to peer frames.
func registerCrossFrameRelay(b *bridge.Bridge) {
	if b == nil {
		return
	}

	b.SetCrossFrameRelaySendCallback(func(name, valueJSON string) {
		fn := js.Global().Get("__gosx_relay_send")
		if fn.Type() != js.TypeFunction {
			return
		}
		fn.Invoke(name, valueJSON)
	})

	setRuntimeFunc("__gosx_enable_cross_frame_relay", enableCrossFrameRelayFunc(b))
	setRuntimeFunc("__gosx_relay_dispatch_inbound", relayDispatchInboundFunc(b))
	maybeAutoEnableCrossFrameRelay(b)
}

func enableCrossFrameRelayFunc(b *bridge.Bridge) js.Func {
	return js.FuncOf(func(this js.Value, args []js.Value) any {
		if len(args) < 2 {
			return jsErrorf("need 2 args (prefix, allowedOrigin)")
		}
		prefix := args[0].String()
		allowedOrigin := args[1].String()
		b.EnableCrossFrameRelay(prefix, allowedOrigin)
		// Push the configuration into the JS relay so its message
		// listener begins gating on it. Done eagerly so the JS side
		// can flush any buffered inbound messages immediately.
		configure := js.Global().Get("__gosx_relay_configure")
		if configure.Type() == js.TypeFunction {
			cfg := js.Global().Get("Object").New()
			cfg.Set("prefix", prefix)
			cfg.Set("allowedOrigin", allowedOrigin)
			arr := js.Global().Get("Array").New(cfg)
			configure.Invoke(arr)
		}
		// Flush any inbound messages that arrived before this call.
		flush := js.Global().Get("__gosx_relay_flush_inbound")
		if flush.Type() == js.TypeFunction {
			flush.Invoke()
		}
		return js.Null()
	})
}

func relayDispatchInboundFunc(b *bridge.Bridge) js.Func {
	return js.FuncOf(func(this js.Value, args []js.Value) any {
		if len(args) < 3 {
			return jsErrorf("need 3 args (name, valueJSON, origin)")
		}
		name := args[0].String()
		valueJSON := args[1].String()
		origin := args[2].String()
		if err := b.DispatchInboundSignal(name, valueJSON, origin); err != nil {
			return jsError(err)
		}
		return js.Null()
	})
}

// maybeAutoEnableCrossFrameRelay checks the current page URL for the
// preview-mode query parameter and, if present, opts the Bridge into the
// relay automatically. This lets the storefront layout in muddy-noni
// add `?gosx-preview=1&gosx-preview-origin=<editor-origin>` to the
// iframe URL without any storefront-side wiring beyond the layout
// opt-in (island.EnablePreviewBootstrap()).
//
// Query parameter contract:
//   - gosx-preview=1 → enable relay for the default $preview. prefix.
//   - gosx-preview-origin=<editor-origin> → expected peer origin. Falls
//     back to "*" (dev-mode wildcard) when absent.
//
// Storefronts that need finer control (multiple prefixes, explicit
// per-iframe origin pinning) can call __gosx_enable_cross_frame_relay
// directly from their bootstrap.
func maybeAutoEnableCrossFrameRelay(b *bridge.Bridge) {
	location := js.Global().Get("location")
	if !location.Truthy() {
		return
	}
	search := location.Get("search")
	if !search.Truthy() {
		return
	}
	searchStr := strings.TrimSpace(search.String())
	if searchStr == "" {
		return
	}
	prefix, origin, ok := parsePreviewModeQuery(searchStr)
	if !ok {
		return
	}
	b.EnableCrossFrameRelay(prefix, origin)
	configure := js.Global().Get("__gosx_relay_configure")
	if configure.Type() == js.TypeFunction {
		cfg := js.Global().Get("Object").New()
		cfg.Set("prefix", prefix)
		cfg.Set("allowedOrigin", origin)
		arr := js.Global().Get("Array").New(cfg)
		configure.Invoke(arr)
	}
	flush := js.Global().Get("__gosx_relay_flush_inbound")
	if flush.Type() == js.TypeFunction {
		flush.Invoke()
	}
}

// parsePreviewModeQuery is implemented in cross_frame_parse.go (no build
// tag) so non-js builds can unit-test it.
