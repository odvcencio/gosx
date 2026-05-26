// gosx cross-frame relay — postMessage transport for $preview.* shared signals.
//
// Loaded only on pages that opt into preview-mode bootstrap (storefront
// rendered inside the editor iframe). Implements the JS side of the
// architecture defined in ADR 0009 (decisions/0009-iframe-transport-postmessage-relay.md)
// and plan section B of plans/2026-05-26-iframe-cross-frame-signal-transport.md.
//
// Wire contract:
//   - Outbound: WASM-side calls window.__gosx_relay_send(name, valueJSON)
//     which posts {type: "gosx:shared-signal", name, valueJSON, origin}
//     to every registered peer.
//   - Inbound: window message listener filters by type, validates origin and
//     prefix against the registered relay configs, then invokes
//     window.__gosx_relay_dispatch_inbound(name, valueJSON, origin) which the
//     WASM-side hooks to its Bridge.DispatchInboundSignal.
//
// Peer discovery:
//   - In the iframe (window.parent !== window): the only peer is parent.
//   - In the editor (parent of preview iframe): peers are auto-discovered
//     from window.frames after the iframe loads, OR registered explicitly
//     via window.__gosx.relay.registerPeer(target, origin).
//
// Origin validation: configs are pushed via window.__gosx.relay.configure([
//   {prefix, allowedOrigin}, ...]) by the wasm-side at startup. "*" allows
//   any origin (dev mode) and emits a console warning.

(function() {
  "use strict";

  if (typeof window === "undefined") {
    return;
  }

  const MESSAGE_TYPE = "gosx:shared-signal";

  function ensureRelayState() {
    if (!window.__gosx) {
      // Created normally by 00-textlayout.js; fall back to a minimal shim if
      // the relay loaded earlier (preview-mode bootstrap ordering allows
      // either order).
      window.__gosx = {};
    }
    if (!window.__gosx.relay) {
      window.__gosx.relay = {
        configs: [],
        peers: [],
        configured: false,
        listening: false,
      };
    }
    return window.__gosx.relay;
  }

  // configure pushes the relay configurations from the WASM-side. Idempotent
  // per (prefix, allowedOrigin) pair. Emits a console warning for any
  // dev-mode "*" origin so production deployments can audit.
  function configure(configs) {
    const state = ensureRelayState();
    if (!Array.isArray(configs)) {
      return;
    }
    for (const cfg of configs) {
      if (!cfg || typeof cfg.prefix !== "string" || cfg.prefix === "") {
        continue;
      }
      const allowedOrigin = typeof cfg.allowedOrigin === "string" ? cfg.allowedOrigin : "*";
      const existing = state.configs.find((c) => c.prefix === cfg.prefix && c.allowedOrigin === allowedOrigin);
      if (existing) {
        continue;
      }
      state.configs.push({ prefix: cfg.prefix, allowedOrigin: allowedOrigin });
      if (allowedOrigin === "*") {
        // Loud warning so production deployments audit and replace.
        try {
          console.warn(
            "[gosx/relay] dev-mode wildcard origin in use for prefix",
            cfg.prefix + ".",
            "Pin allowedOrigin to your editor origin before shipping.",
          );
        } catch (_e) {
          // console may not exist in some hosts.
        }
      }
    }
    state.configured = true;
    autoRegisterPeers(state);
    ensureMessageListener(state);
  }

  function matchPrefix(state, name) {
    if (typeof name !== "string" || name === "") {
      return null;
    }
    for (const cfg of state.configs) {
      if (name.indexOf(cfg.prefix) === 0) {
        return cfg;
      }
    }
    return null;
  }

  function originAllowed(cfg, origin) {
    if (!cfg) {
      return false;
    }
    if (cfg.allowedOrigin === "*") {
      return true;
    }
    return cfg.allowedOrigin === origin;
  }

  function autoRegisterPeers(state) {
    // Storefront-iframe-side: parent is the editor.
    try {
      if (window.parent && window.parent !== window) {
        registerPeerInternal(state, window.parent, null);
      }
    } catch (_e) {
      // Cross-origin parent access can throw — non-fatal; the explicit
      // registerPeer API handles this case.
    }
    // Editor-side: scan window.frames for child iframes. Their
    // contentWindow is accessed lazily on send (the iframe may not have
    // loaded yet at relay-configure time).
    try {
      const frames = window.frames;
      if (frames && typeof frames.length === "number") {
        for (let i = 0; i < frames.length; i++) {
          const frame = frames[i];
          if (frame && frame !== window) {
            registerPeerInternal(state, frame, null);
          }
        }
      }
    } catch (_e) {
      // Same caveat as above.
    }
  }

  function registerPeerInternal(state, target, origin) {
    if (!target) {
      return;
    }
    for (const peer of state.peers) {
      if (peer.target === target) {
        if (origin && !peer.origin) {
          peer.origin = origin;
        }
        return;
      }
    }
    state.peers.push({ target: target, origin: origin || null });
  }

  // registerPeer is the explicit API for callers that need precise control
  // (e.g. after iframe load events, multi-iframe editors).
  function registerPeer(target, origin) {
    const state = ensureRelayState();
    registerPeerInternal(state, target, typeof origin === "string" ? origin : null);
  }

  // send is invoked by the WASM-side when a relayed-prefix signal write
  // happens locally. Posts to every registered peer with the configured
  // allowed origin (or "*" in dev mode).
  function send(name, valueJSON) {
    const state = ensureRelayState();
    const cfg = matchPrefix(state, name);
    if (!cfg) {
      return; // not a relayed signal; nothing to do
    }
    const message = {
      type: MESSAGE_TYPE,
      name: name,
      valueJSON: typeof valueJSON === "string" ? valueJSON : "null",
      origin: (window.location && window.location.origin) || "",
    };
    const targetOrigin = cfg.allowedOrigin || "*";
    for (const peer of state.peers) {
      try {
        peer.target.postMessage(message, targetOrigin);
      } catch (err) {
        try {
          console.error("[gosx/relay] postMessage failed:", err);
        } catch (_e2) {
          // console-less hosts.
        }
      }
    }
  }

  function ensureMessageListener(state) {
    if (state.listening) {
      return;
    }
    if (typeof window.addEventListener !== "function") {
      return;
    }
    window.addEventListener("message", function(event) {
      if (!event || !event.data || event.data.type !== MESSAGE_TYPE) {
        return;
      }
      const name = event.data.name;
      const valueJSON = event.data.valueJSON;
      const originatingOrigin = event.origin || event.data.origin || "";
      const cfg = matchPrefix(state, name);
      if (!cfg) {
        // Unknown prefix — drop silently (could be a different gosx app).
        return;
      }
      if (!originAllowed(cfg, originatingOrigin)) {
        try {
          console.warn(
            "[gosx/relay] dropped message from disallowed origin",
            originatingOrigin,
            "for signal",
            name,
          );
        } catch (_e) {
          // console-less hosts.
        }
        return;
      }
      const dispatch = window.__gosx_relay_dispatch_inbound;
      if (typeof dispatch !== "function") {
        // WASM-side not ready yet — buffer briefly via deferred queue.
        const buffer = state.inboundBuffer || (state.inboundBuffer = []);
        buffer.push({ name: name, valueJSON: valueJSON, origin: originatingOrigin });
        return;
      }
      try {
        dispatch(name, valueJSON, originatingOrigin);
      } catch (err) {
        try {
          console.error("[gosx/relay] inbound dispatch failed:", err);
        } catch (_e) {
          // console-less hosts.
        }
      }
    });
    state.listening = true;
  }

  // flushInboundBuffer is called by the WASM-side after it registers
  // __gosx_relay_dispatch_inbound, to deliver any messages that arrived
  // before the WASM Bridge was ready.
  function flushInboundBuffer() {
    const state = ensureRelayState();
    if (!state.inboundBuffer || state.inboundBuffer.length === 0) {
      return;
    }
    const dispatch = window.__gosx_relay_dispatch_inbound;
    if (typeof dispatch !== "function") {
      return;
    }
    const pending = state.inboundBuffer;
    state.inboundBuffer = [];
    for (const item of pending) {
      try {
        dispatch(item.name, item.valueJSON, item.origin);
      } catch (err) {
        try {
          console.error("[gosx/relay] buffered inbound dispatch failed:", err);
        } catch (_e) {
          // console-less hosts.
        }
      }
    }
  }

  const state = ensureRelayState();
  state.configure = configure;
  state.registerPeer = registerPeer;
  state.send = send;
  state.flushInboundBuffer = flushInboundBuffer;

  // Expose stable globals for the WASM-side syscall/js bridge.
  window.__gosx_relay_send = send;
  window.__gosx_relay_configure = configure;
  window.__gosx_relay_register_peer = registerPeer;
  window.__gosx_relay_flush_inbound = flushInboundBuffer;

  // Mark relay as enabled so the host bootstrap knows the JS side is live.
  window.__gosx_relay_enabled = true;
})();
