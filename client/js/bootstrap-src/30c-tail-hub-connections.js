// 30c — realtime hub transport.
//
// Chunks: bootstrap.js, bootstrap-feature-hubs.js.
// Holds the framework part of a hub: URL building, binding application,
// anonymous client identity, socket setup, message routing and reconnect.
// 30f closes what this file opens.
//
// The fighting-game input controllers moved to 30c1 and the procedural synth
// moved to 30c2. Keep this file free of one application's vocabulary. The
// bootstrap-size test asserts that.
// 30c — realtime hub connections: socket setup, reconnect policy, signal
// binding, and inbound message routing.
//
// Chunks: bootstrap.js, bootstrap-feature-hubs.js.
// 30f closes what this file opens.
  // --------------------------------------------------------------------------
  // Hub connections
  // --------------------------------------------------------------------------

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

  function applyHubBindings(record, message) {
    const entry = record.entry;
    if (!entry.bindings || entry.bindings.length === 0) return;

    for (let i = 0; i < entry.bindings.length; i++) {
      applyHubBinding(record, entry.bindings[i], message);
    }
  }

  function applyHubBinding(record, binding, message) {
    const entry = record.entry;
    if (binding && binding.direction === "out") return;
    // Welcome frames are transport metadata unless a binding names the
    // welcome event explicitly.
    if (!binding || binding.event !== message.event) return;
    if (binding.signal) {
      try {
        const result = setSharedSignalJSON(binding.signal, JSON.stringify(message.data));
        if (typeof result === "string" && result !== "") {
          console.error(`[gosx] hub binding error (${entry.id}/${binding.signal}):`, result);
        }
      } catch (e) {
        console.error(`[gosx] hub binding error (${entry.id}/${binding.signal}):`, e);
      }
    }
    if (binding.refresh) scheduleHubRefresh(record, binding);
  }

  function hubNavigationFetchEpoch(navigation) {
    if (!navigation || typeof navigation.getFetchEpoch !== "function") return null;
    try {
      const snapshot = navigation.getFetchEpoch();
      if (!snapshot || typeof snapshot !== "object") return null;
      const started = Number(snapshot.started);
      const applied = Number(snapshot.applied);
      return Number.isFinite(started) && Number.isFinite(applied)
        ? { started: started, applied: applied }
        : null;
    } catch (_) {
      return null;
    }
  }

  function scheduleHubRefresh(record, binding) {
    const pending = record.refreshTimer != null;
    if (pending) clearTimeout(record.refreshTimer);
    const preserveScroll = binding.refreshPreserveScroll !== false;
    record.refreshPreserveScroll = pending
      ? record.refreshPreserveScroll !== false && preserveScroll
      : preserveScroll;
    record.refreshEvent = binding.event;
    const navigation = window.__gosx.navigation || window.__gosx_page_nav;
    const fetchEpoch = hubNavigationFetchEpoch(navigation);
    record.refreshFetchEpoch = fetchEpoch ? fetchEpoch.started : null;
    const delay = Math.max(0, Math.floor(Number(binding.refreshDebounceMs || 0)));
    const run = function() {
      record.refreshTimer = null;
      if (window.__gosx.hubs.get(record.entry.id) !== record) return;
      const liveNavigation = window.__gosx.navigation || window.__gosx_page_nav;
      if (!liveNavigation || typeof liveNavigation.revalidate !== "function") return;
      let navigationPending = false;
      try {
        const state = typeof liveNavigation.getState === "function" ? liveNavigation.getState() : null;
        navigationPending = !!state && state.phase === "pending";
      } catch (_) {}
      if (navigationPending) {
        record.refreshTimer = setTimeout(run, 32);
        return;
      }
      const refreshPreserveScroll = record.refreshPreserveScroll !== false;
      const refreshEvent = record.refreshEvent;
      const refreshFetchEpoch = record.refreshFetchEpoch;
      record.refreshPreserveScroll = null;
      record.refreshEvent = null;
      record.refreshFetchEpoch = null;
      const liveFetchEpoch = hubNavigationFetchEpoch(liveNavigation);
      if (refreshFetchEpoch != null && liveFetchEpoch && liveFetchEpoch.applied > refreshFetchEpoch) return;
      Promise.resolve().then(function() {
        return liveNavigation.revalidate({ preserveScroll: refreshPreserveScroll });
      }).catch(function(error) {
        console.error(`[gosx] hub refresh error (${record.entry.id}/${refreshEvent}):`, error);
      });
    };
    record.refreshTimer = setTimeout(run, delay);
  }

  function initializeClientIdentity(config) {
    const cfg = normalizeClientIdentityConfig(config);
    if (!cfg) return null;
    const current = window.__gosx.identity;
    if (current && current.configKey === cfg.configKey) {
      return current;
    }
    const clientId = ensureClientIdentity(cfg);
    const identity = {
      clientId: clientId,
      headerName: cfg.headerName,
      cookieName: cfg.cookieName,
      configKey: cfg.configKey,
      applyHeaders: function(headers) {
        const next = Object.assign({}, headers || {});
        if (cfg.headerName) next[cfg.headerName] = clientId;
        return next;
      },
    };
    window.__gosx.identity = identity;
    if (cfg.globalName && /^[A-Za-z_$][A-Za-z0-9_$]*$/.test(cfg.globalName)) {
      window[cfg.globalName] = identity;
    }
    return identity;
  }

  function normalizeClientIdentityConfig(raw) {
    if (!raw || typeof raw !== "object") return null;
    const cookieName = String(raw.cookieName || "gosx_client_id").trim();
    const storageKey = String(raw.storageKey || cookieName).trim();
    const headerName = String(raw.headerName || "X-GoSX-Client-ID").trim();
    if (!cookieName || !storageKey) return null;
    const legacy = Array.isArray(raw.legacyCookieNames)
      ? raw.legacyCookieNames.map(function(value) { return String(value || "").trim(); }).filter(Boolean)
      : [];
    const maxAge = Math.max(60, Math.floor(hubInputNumber(raw.maxAgeSeconds, 31536000)));
    return {
      cookieName: cookieName,
      legacyCookieNames: legacy,
      storageKey: storageKey,
      headerName: headerName,
      globalName: String(raw.globalName || "").trim(),
      prefix: String(raw.prefix || "gosx-"),
      maxAgeSeconds: maxAge,
      sameSite: String(raw.sameSite || "Lax").trim() || "Lax",
      configKey: [cookieName, storageKey, headerName].join("|"),
    };
  }

  function ensureClientIdentity(config) {
    const id = normalizeClientIdentity(readIdentityCookie(config))
      || normalizeClientIdentity(readIdentityStorage(config.storageKey))
      || randomClientIdentity(config.prefix);
    writeIdentityStorage(config.storageKey, id);
    writeIdentityCookie(config, id);
    return id;
  }

  function normalizeClientIdentity(value) {
    const id = String(value || "").trim();
    return /^[A-Za-z0-9_-]{6,96}$/.test(id) ? id : "";
  }

  function readIdentityCookie(config) {
    const cookieText = String(document && document.cookie || "");
    if (!cookieText) return "";
    const names = [config.cookieName].concat(config.legacyCookieNames || []);
    const parts = cookieText.split(";");
    for (const name of names) {
      const prefix = name + "=";
      for (const part of parts) {
        const item = String(part || "").trim();
        if (item.indexOf(prefix) !== 0) continue;
        try {
          return decodeURIComponent(item.slice(prefix.length));
        } catch (_e) {
          return "";
        }
      }
    }
    return "";
  }

  function writeIdentityCookie(config, id) {
    if (!document) return;
    try {
      document.cookie = config.cookieName + "=" + encodeURIComponent(id)
        + "; Path=/; Max-Age=" + config.maxAgeSeconds
        + "; SameSite=" + config.sameSite;
    } catch (_e) {}
  }

  function readIdentityStorage(key) {
    try {
      return window.localStorage ? window.localStorage.getItem(key) || "" : "";
    } catch (_e) {
      return "";
    }
  }

  function writeIdentityStorage(key, id) {
    try {
      if (window.localStorage) window.localStorage.setItem(key, id);
    } catch (_e) {}
  }

  function randomClientIdentity(prefix) {
    const safePrefix = String(prefix || "gosx-");
    if (window.crypto && typeof window.crypto.randomUUID === "function") {
      return safePrefix + window.crypto.randomUUID().replace(/-/g, "");
    }
    const bytes = new Uint8Array(16);
    if (window.crypto && typeof window.crypto.getRandomValues === "function") {
      window.crypto.getRandomValues(bytes);
    } else {
      for (let i = 0; i < bytes.length; i++) bytes[i] = Math.floor(Math.random() * 256);
    }
    return safePrefix + Array.prototype.map.call(bytes, function(byte) {
      return byte.toString(16).padStart(2, "0");
    }).join("");
  }

  function gosxClientIdentity() {
    return window.__gosx && window.__gosx.identity ? window.__gosx.identity : null;
  }

  function gosxClientID() {
    const identity = gosxClientIdentity();
    if (identity && identity.clientId) return String(identity.clientId);
    const feral = window.__feralIdentity;
    return feral && feral.clientId ? String(feral.clientId) : "";
  }

  function gosxIdentityHeaders(headers) {
    const identity = gosxClientIdentity();
    if (identity && typeof identity.applyHeaders === "function") {
      return identity.applyHeaders(headers);
    }
    const feral = window.__feralIdentity;
    if (feral && typeof feral.applyHeaders === "function") {
      return feral.applyHeaders(headers);
    }
    return Object.assign({}, headers || {});
  }
  function hubInputNumber(value, fallback) {
    const next = Number(value);
    return Number.isFinite(next) ? next : fallback;
  }

  function gamepadPressed(pad, index) {
    const button = pad && pad.buttons && pad.buttons[index];
    return Boolean(button && (button.pressed || hubInputNumber(button.value, 0) > 0.55));
  }

  function hubInputCapturesKey(event) {
    const code = String(event && event.code || "");
    const key = String(event && event.key || "").toLowerCase();
    return code === "KeyW" || code === "KeyA" || code === "KeyS" || code === "KeyD"
      || code === "KeyU" || code === "KeyI" || code === "KeyJ" || code === "KeyK" || code === "KeyL"
      || code === "ArrowUp" || code === "ArrowDown" || code === "ArrowLeft" || code === "ArrowRight"
      || code === "Space"
      || key === "w" || key === "a" || key === "s" || key === "d"
      || key === "u" || key === "i" || key === "j" || key === "k" || key === "l"
      || key === " ";
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

  function bindHubOutputs(record) {
    record.outputUnsubscribers = record.outputUnsubscribers || [];
    if (record.outputUnsubscribers.length > 0) return;
    const bindings = record.entry && record.entry.bindings;
    if (!bindings || !bindings.length) return;
    for (let bi = 0; bi < bindings.length; bi++) {
      const b = bindings[bi];
      if (!b || b.direction !== "out" || !b.signal || !b.event) continue;
      (function(binding) {
        let lastSentAt = 0;
        let debounceTimer = null;
        const sendValue = function(value) {
          const socket = record.socket;
          if (socket && (socket.readyState === 1 || socket.readyState == null)) {
            socket.send(JSON.stringify({ event: binding.event, data: value || {} }));
          }
        };
        const fn = function(value) {
          if (binding.throttleMs > 0) {
            const now = Date.now();
            if (now - lastSentAt >= binding.throttleMs) {
              lastSentAt = now;
              sendValue(value);
            }
          } else if (binding.debounceMs > 0) {
            if (debounceTimer != null) clearTimeout(debounceTimer);
            debounceTimer = setTimeout(function() {
              debounceTimer = null;
              sendValue(value);
            }, binding.debounceMs);
          } else {
            sendValue(value);
          }
        };
        const unsub = gosxSubscribeSharedSignal(binding.signal, fn, { immediate: false });
        record.outputUnsubscribers.push(unsub);
      })(b);
    }
  }

  function attachHubSocketHandlers(record) {
    const entry = record.entry;
    const socket = record.socket;
    record.inputController = createHubInputController(record);
    try {
      socket.binaryType = "arraybuffer";
    } catch (_e) {
      // Some test doubles and embedded runtimes expose binaryType as read-only.
    }
    socket.onopen = function() {
      if (record.inputController && typeof record.inputController.flush === "function") {
        record.inputController.flush();
      }
      bindHubOutputs(record);
    };
    socket.onmessage = function(evt) {
      const decoded = decodeHubMessage(entry, evt.data);
      if (decoded && typeof decoded.then === "function") {
        decoded.then(function(message) {
          dispatchHubMessage(record, message);
        });
        return;
      }
      dispatchHubMessage(record, decoded);
    };

    socket.onclose = function() {
      scheduleHubReconnect(record);
    };

    socket.onerror = function(e) {
      console.error(`[gosx] hub connection error for ${entry.id}:`, e);
    };
  }

  function dispatchHubMessage(record, message) {
    if (!message) return;
    const entry = record.entry;
    applyHubBindings(record, message);
    if (record.inputController && typeof record.inputController.onMessage === "function") {
      try {
        record.inputController.onMessage(message);
      } catch (e) {
        console.error(`[gosx] hub input message error for ${entry.id}:`, e);
      }
    }
    emitHubEvent(entry, message);
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
    initializeClientIdentity(manifest && manifest.clientIdentity);
    if (!manifest || !manifest.hubs || manifest.hubs.length === 0) return;
    for (const entry of manifest.hubs) {
      connectHub(entry);
    }
  }
