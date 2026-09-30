// @ts-check
// GoSX browser host: realtime hub transport.
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

  function hubMonotonicNow() {
    return typeof performance !== "undefined" && typeof performance.now === "function"
      ? performance.now()
      : Date.now();
  }

  function sendHubRoundTrip(record) {
    const config = record.roundTripConfig;
    if (!config || !record.socket || record.socket.readyState !== 1) return;
    record.roundTripSequence++;
    record.roundTripSentAt = hubMonotonicNow();
    record.socket.send(JSON.stringify({ event: config.pingEvent, data: { sequence: record.roundTripSequence } }));
  }

  function startHubRoundTrip(record) {
    stopHubRoundTrip(record);
    const config = record.entry && record.entry.roundTrip;
    if (!config || !config.signal) return;
    record.roundTripConfig = config;
    record.roundTripSequence = 0;
    sendHubRoundTrip(record);
    record.roundTripTimer = setInterval(function() { sendHubRoundTrip(record); }, config.intervalMs);
  }

  function stopHubRoundTrip(record) {
    if (!record) return;
    if (record.roundTripTimer != null) {
      clearInterval(record.roundTripTimer);
      record.roundTripTimer = null;
    }
    record.roundTripSentAt = 0;
  }

  function observeHubRoundTrip(record, message) {
    const config = record && record.roundTripConfig;
    if (!config || !message || message.event !== config.pongEvent || !record.roundTripSentAt) return false;
    const sequence = Number(message.data && message.data.sequence);
    if (!Number.isSafeInteger(sequence) || sequence !== record.roundTripSequence) return false;
    const elapsed = Math.max(0, hubMonotonicNow() - record.roundTripSentAt);
    record.roundTripSentAt = 0;
    try {
      setSharedSignalJSON(config.signal, JSON.stringify(Math.round(elapsed * 100) / 100));
    } catch (_e) {}
    return true;
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
    if (binding.sceneCommands) {
      dispatchHubSceneCommands(record, binding, message.data);
    }
    if (binding.refresh) scheduleHubRefresh(record, binding);
  }

  function dispatchHubSceneCommands(record, binding, data) {
    const mountID = String(binding && binding.sceneMountId || "").trim();
    if (!mountID || !data || typeof data !== "object") return;
    const revision = Number(data.revision);
    if (!Number.isSafeInteger(revision) || revision <= 0 || !Array.isArray(data.commands)) return;
    record.lastSceneCommandRevision = record.lastSceneCommandRevision || new Map();
    const lastRevision = record.lastSceneCommandRevision.get(mountID) || 0;
    if (revision <= lastRevision) return;
    record.lastSceneCommandRevision.set(mountID, revision);
    const mount = document.getElementById(mountID);
    const detail = { revision: revision, commands: data.commands };
    if (mount && mount.getAttribute("data-gosx-scene3d-command-ready") === "true") {
      emitHubSceneCommands(mount, detail);
      return;
    }

    // Hub sockets can open before the Scene3D renderer has finished loading.
    // Keep only the newest revision per mount, then apply it as soon as the
    // renderer sets its command-ready attribute. A snapshot is a full diff
    // from the initial scene, so older queued batches are unnecessary.
    record.pendingSceneCommands = record.pendingSceneCommands || new Map();
    record.pendingSceneCommands.set(mountID, detail);
    watchHubSceneCommandMount(record, mountID);
  }

  function watchHubSceneCommandMount(record, mountID) {
    if (typeof MutationObserver !== "function" || !document.documentElement) return;
    record.sceneCommandObservers = record.sceneCommandObservers || new Map();
    if (record.sceneCommandObservers.has(mountID)) return;
    const observer = new MutationObserver(function() {
      const mount = document.getElementById(mountID);
      if (!mount || mount.getAttribute("data-gosx-scene3d-command-ready") !== "true") return;
      observer.disconnect();
      record.sceneCommandObservers.delete(mountID);
      const detail = record.pendingSceneCommands && record.pendingSceneCommands.get(mountID);
      if (record.pendingSceneCommands) record.pendingSceneCommands.delete(mountID);
      if (detail) emitHubSceneCommands(mount, detail);
    });
    observer.observe(document.documentElement, {
      attributes: true,
      attributeFilter: ["data-gosx-scene3d-command-ready"],
      childList: true,
      subtree: true,
    });
    record.sceneCommandObservers.set(mountID, observer);
  }

  function emitHubSceneCommands(mount, detail) {
    if (!mount || typeof mount.dispatchEvent !== "function") return;
    const event = typeof CustomEvent === "function"
      ? new CustomEvent("gosx:scene3d:commands", { detail: detail })
      : { type: "gosx:scene3d:commands", detail: detail };
    mount.dispatchEvent(event);
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
    return /^(?:Key[WASDUIJKL]|Arrow(?:Up|Down|Left|Right)|Space)(?![\s\S])/.test(code)
      || (key.length === 1 && "wasduijkl ".includes(key));
  }

  // connectHub's optional attempt parameter is the reconnect attempt
  // number this connection is retrying (0 for a fresh, first-ever
  // connection): scheduleHubReconnect below passes its own
  // current.reconnectAttempt through so the NEW record it creates keeps
  // counting up the same exponential-backoff sequence instead of
  // resetting to attempt 0 on every retry, which would otherwise pin
  // every reconnect delay back to reconnectDelayMs(0) forever.
  function connectHub(entry, attempt) {
    if (!canConnectHub(entry)) return;

    if (typeof gosxHost.hubs?.disconnect === "function") {
      gosxHost.hubs.disconnect(entry.id);
    }
    const record = createHubRecord(entry, attempt);
    window.__gosx.hubs.set(entry.id, record);
    attachHubSocketHandlers(record);
  }

  function canConnectHub(entry) {
    return Boolean(entry && entry.id && entry.path && typeof WebSocket === "function");
  }

  function createHubRecord(entry, attempt) {
    return {
      entry: entry,
      socket: new WebSocket(hubURL(entry.path)),
      reconnectTimer: null,
      reconnectAttempt: attempt || 0,
    };
  }

  function bindHubOutputs(record) {
    record.outputUnsubscribers = record.outputUnsubscribers || [];
    if (record.outputUnsubscribers.length > 0) return;
    const bindings = record.entry && record.entry.bindings;
    if (!bindings || !bindings.length) return;
    for (let bi = 0; bi < bindings.length; bi++) {
      const b = bindings[bi];
      if (!b || b.direction !== "out" || !b.event || (!b.signal && !b.sceneInput)) continue;
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
        if (binding.signal) {
          const unsub = gosxSubscribeSharedSignal(binding.signal, fn, { immediate: false });
          record.outputUnsubscribers.push(unsub);
        }
        if (binding.sceneInput) {
          const mountID = String(binding.sceneMountId || "").trim();
          if (!mountID || typeof document.addEventListener !== "function") return;
          const onSceneInput = function(event) {
            const mount = document.getElementById(mountID);
            if (!mount || event.target !== mount) return;
            const detail = event && event.detail && typeof event.detail === "object" ? event.detail : null;
            if (!detail) return;
            if (binding.sceneInput !== "all" && detail.kind !== binding.sceneInput) return;
            const payload = Object.assign({}, detail);
            if (binding.sceneInputKind) payload.kind = binding.sceneInputKind;
            fn(payload);
          };
          document.addEventListener("gosx:scene3d:input", onSceneInput);
          record.outputUnsubscribers.push(function() {
            document.removeEventListener("gosx:scene3d:input", onSceneInput);
          });
        }
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
      // A successful open proves the connection is healthy again, so the
      // exponential backoff resets: the NEXT unexpected close starts back
      // at reconnectDelayMs(0), not wherever this connection's own retry
      // count left off.
      record.reconnectAttempt = 0;
      if (record.inputController && typeof record.inputController.flush === "function") {
        record.inputController.flush();
      }
      bindHubOutputs(record);
      startHubRoundTrip(record);
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
      stopHubRoundTrip(record);
      scheduleHubReconnect(record);
    };

    socket.onerror = function(e) {
      console.error(`[gosx] hub connection error for ${entry.id}:`, e);
    };
  }

  function dispatchHubMessage(record, message) {
    if (!message) return;
    const entry = record.entry;
    observeHubRoundTrip(record, message);
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

  // reconnectDelayMs computes a hub's next reconnect delay: exponential
  // backoff from a 500ms base, doubling per attempt (0-indexed: attempt 0
  // is the first retry after the original connection), capped at 15s, plus
  // up to 25 percent random jitter on top of whichever of those two is
  // smaller — the jitter spreads out a thundering-herd reconnect storm
  // (every hub client on a restarted server retrying at the exact same
  // moment) without ever pushing a delay past the 15s cap itself. random
  // is an injection point for a deterministic test; production callers
  // omit it and get Math.random().
  function reconnectDelayMs(attempt, random) {
    var base = Math.min(15000, 500 * Math.pow(2, Math.max(0, attempt)));
    var jitter = typeof random === "number" ? random : Math.random();
    return Math.round(Math.min(15000, base + base * 0.25 * jitter));
  }

  function scheduleHubReconnect(record) {
    const entry = record.entry;
    const current = window.__gosx.hubs.get(entry.id);
    // current.reconnectTimer != null means a reconnect is already
    // scheduled for this hub id — a second close of the same (already
    // closing) socket, or a close racing an in-flight reconnect, must
    // never schedule a second, overlapping retry on top of the first.
    if (!current || current.socket !== record.socket || current.reconnectTimer != null) return;
    current.reconnectAttempt = (current.reconnectAttempt || 0) + 1;
    current.reconnectDelay = reconnectDelayMs(current.reconnectAttempt - 1);
    current.reconnectTimer = setTimeout(function() {
      current.reconnectTimer = null;
      connectHub(entry, current.reconnectAttempt);
    }, current.reconnectDelay);
  }

  async function connectAllHubs(manifest) {
    initializeClientIdentity(manifest && manifest.clientIdentity);
    if (!manifest || !manifest.hubs || manifest.hubs.length === 0) return;
    for (const entry of manifest.hubs) {
      if (!window.__gosx_scene3d_hub_policy?.defer(entry, connectHub)) connectHub(entry);
    }
  }

  // revalidateHubConnections repairs sockets the page lost while it was not
  // running. A page restored from the back-forward cache resumes with every
  // WebSocket already torn down — Chrome reports "WebSocket connection
  // failed: Page entered Back-Forward Cache" on freeze. The socket's close
  // event is the ONLY thing that schedules a reconnect (see
  // scheduleHubReconnect), and a frozen page is not guaranteed to deliver
  // it, which leaves every hub permanently dead: live regions and hub-bound
  // signals stop updating until the reader reloads by hand. Check the actual
  // socket state on pageshow instead of trusting the event to arrive.
  //
  // Deliberately not gated on event.persisted: the same repair is correct for
  // any resume that dropped a socket without dispatching close, and it is a
  // no-op when every socket is live (a first load has no hubs connected yet).
  function revalidateHubConnections() {
    const hubs = window.__gosx && window.__gosx.hubs;
    if (!hubs || typeof hubs.forEach !== "function") return;
    const stale = [];
    hubs.forEach(function(record) {
      if (!record || !record.entry) return;
      const socket = record.socket;
      // WebSocket.CLOSING === 2, CLOSED === 3. A null readyState is a test
      // double that does not model the lifecycle; bindHubOutputs already
      // treats that as live, so leave it alone.
      const state = socket ? socket.readyState : 3;
      if (state === 2 || state === 3) {
        stale.push(record.entry);
      }
    });
    for (const entry of stale) {
      connectHub(entry);
    }
  }

  if (typeof window.addEventListener === "function") {
    window.addEventListener("pageshow", revalidateHubConnections);
  }

  Object.assign(gosxHost.hubs ||= {}, {
    connect: connectHub,
    connectAll: connectAllHubs,
    revalidate: revalidateHubConnections,
    reconnectDelayMs: reconnectDelayMs,
  });
