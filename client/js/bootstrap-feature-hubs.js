(function() {
  "use strict";

  const registerFeature = window.__gosx_register_bootstrap_feature;
  if (typeof registerFeature !== "function") {
    console.error("[gosx] runtime bootstrap feature registry missing");
    return;
  }

  registerFeature("hubs", function(api) {
    const setSharedSignalJSON = api.setSharedSignalJSON;
    const gosxNotifySharedSignal = api.gosxNotifySharedSignal;

  function hubURL(path) {
    if (!path) return "";
    if (isAbsoluteHubURL(path)) {
      return path;
    }
    return hubOrigin() + normalizeHubPath(path);
  }

  function isAbsoluteHubURL(path) {
    return path.startsWith("ws://") || path.startsWith("wss://");
  }

  function hubOrigin() {
    return hubScheme() + hubHost();
  }

  function hubScheme() {
    return window.location && window.location.protocol === "https:" ? "wss://" : "ws://";
  }

  function hubHost() {
    return window.location && window.location.host ? window.location.host : "";
  }

  function normalizeHubPath(path) {
    return path.startsWith("/") ? path : "/" + path;
  }

  function applyHubBindings(entry, message) {
    if (!entry.bindings || entry.bindings.length === 0) return;

    for (const binding of entry.bindings) {
      applyHubBinding(entry, binding, message);
    }
  }

  function applyHubBinding(entry, binding, message) {
    if (!binding || binding.event !== message.event || !binding.signal) return;
    try {
      const result = setSharedSignalJSON(binding.signal, JSON.stringify(message.data));
      if (typeof result === "string" && result !== "") {
        console.error(`[gosx] hub binding error (${entry.id}/${binding.signal}):`, result);
      }
    } catch (e) {
      console.error(`[gosx] hub binding error (${entry.id}/${binding.signal}):`, e);
    }
  }

  function connectHub(entry) {
    if (!canConnectHub(entry)) return;

    window.__gosx_disconnect_hub(entry.id);
    const record = createHubRecord(entry);
    window.__gosx.hubs.set(entry.id, record);
    attachHubSocketHandlers(record);
  }

  function canConnectHub(entry) {
    return Boolean(entry && entry.id && entry.path && typeof WebSocket === "function");
  }

  function createHubRecord(entry) {
    return {
      entry: entry,
      socket: new WebSocket(hubURL(entry.path)),
      reconnectTimer: null,
    };
  }

  function attachHubSocketHandlers(record) {
    const entry = record.entry;
    const socket = record.socket;
    try {
      socket.binaryType = "arraybuffer";
    } catch (_e) {
    }
    socket.onmessage = function(evt) {
      const decoded = decodeHubMessage(entry, evt.data);
      if (decoded && typeof decoded.then === "function") {
        decoded.then(function(message) {
          if (!message) return;
          applyHubBindings(entry, message);
          emitHubEvent(entry, message);
        });
        return;
      }
      const message = decoded;
      if (!message) return;

      applyHubBindings(entry, message);
      emitHubEvent(entry, message);
    };

    socket.onclose = function() {
      scheduleHubReconnect(record);
    };

    socket.onerror = function(e) {
      console.error(`[gosx] hub connection error for ${entry.id}:`, e);
    };
  }

  function decodeHubMessage(entry, raw) {
    if (typeof raw === "string") {
      return parseHubMessage(entry, raw, false);
    }
    if (raw instanceof ArrayBuffer || ArrayBuffer.isView(raw)) {
      return null;
    }
    if (raw && typeof raw.text === "function") {
      return raw.text().then(function(text) {
        return parseHubMessage(entry, text, true);
      }, function() {
        return null;
      });
    }
    return null;
  }

  function parseHubMessage(entry, raw, quietNonJSON) {
    const text = String(raw == null ? "" : raw);
    const trimmed = text.trim();
    if (quietNonJSON && trimmed && trimmed[0] !== "{" && trimmed[0] !== "[") {
      return null;
    }
    try {
      return JSON.parse(text);
    } catch (e) {
      console.error(`[gosx] failed to decode hub message for ${entry.id}:`, e);
      return null;
    }
  }

  function emitHubEvent(entry, message) {
    if (typeof document.dispatchEvent !== "function" || typeof CustomEvent !== "function") {
      return;
    }
    document.dispatchEvent(new CustomEvent("gosx:hub:event", {
      detail: {
        hubID: entry.id,
        hubName: entry.name,
        event: message.event,
        data: message.data,
      },
    }));
  }

  function scheduleHubReconnect(record) {
    const entry = record.entry;
    const socket = record.socket;
    const current = window.__gosx.hubs.get(entry.id);
    if (!current || current.socket !== socket) return;
    current.reconnectTimer = setTimeout(function() {
      connectHub(entry);
    }, 1000);
  }

  async function connectAllHubs(manifest) {
    if (!manifest.hubs || manifest.hubs.length === 0) return;
    for (const entry of manifest.hubs) {
      connectHub(entry);
    }
  }

  window.__gosx_disconnect_hub = function(hubID) {
    const record = window.__gosx.hubs.get(hubID);
    if (!record) return;

    if (record.reconnectTimer) {
      clearTimeout(record.reconnectTimer);
      record.reconnectTimer = null;
    }
    if (record.socket && typeof record.socket.close === "function") {
      try {
        record.socket.close();
      } catch (e) {
        console.error(`[gosx] disconnect error for hub ${hubID}:`, e);
      }
    }

    window.__gosx.hubs.delete(hubID);
  };

    return {
      runtimeReady(manifest) {
        return connectAllHubs(manifest);
      },
      disposePage() {
        for (const hubID of Array.from(window.__gosx.hubs.keys())) {
          window.__gosx_disconnect_hub(hubID);
        }
      },
      disconnectHub: window.__gosx_disconnect_hub,
    };
  });
})();
//# sourceMappingURL=bootstrap-feature-hubs.js.map
