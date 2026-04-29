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

  function normalizeHubInputConfig(entry) {
    const input = entry && entry.input;
    if (!input || typeof input !== "object") return null;
    const every = Math.max(8, Math.min(100, hubInputNumber(input.sendEveryMs, 16)));
    return {
      mode: String(input.mode || "").trim().toLowerCase(),
      event: String(input.event || "input"),
      readyEvent: String(input.readyEvent || "ready"),
      trainingEvent: String(input.trainingEvent || "training"),
      signal: String(input.signal || ""),
      trainingSignal: String(input.trainingSignal || ""),
      touchRoot: String(input.touchRoot || ""),
      player: Math.max(1, Math.min(2, Math.floor(hubInputNumber(input.player, 1)))),
      local: Boolean(input.local),
      slotToken: String(input.slotToken || ""),
      sendEveryMS: every,
      root: String(input.root || ""),
      username: String(input.username || ""),
      fightPath: String(input.fightPath || "/fight"),
      cpuEndpoint: String(input.cpuEndpoint || "/api/cpu-match/start"),
      localEndpoint: String(input.localEndpoint || "/api/local-match/start"),
      fightCurrentEndpoint: String(input.fightCurrentEndpoint || "/api/fight/current"),
      minLocalGamepads: Math.max(0, Math.floor(hubInputNumber(input.minLocalGamepads, 2))),
      attractSignal: String(input.attractSignal || "$attract"),
      lobbySignal: String(input.lobbySignal || "$lobby"),
      vsSignal: String(input.vsSignal || "$vs"),
    };
  }

  function createHubInputController(record) {
    const config = normalizeHubInputConfig(record && record.entry);
    if (!config) return null;
    if (config.mode === "arcade-select") {
      return createArcadeSelectHubController(record, config);
    }

    const keys = Object.create(null);
    const touch = { up: false, down: false, left: false, right: false, lp: false, hp: false, lk: false, hk: false, guard: false };
    const touchCounts = Object.create(null);
    const activePointers = new Map();
    const listeners = [];
    let disposed = false;
    let timer = 0;
    let readySent = false;
    let trainingVisible = false;
    let lastCue = "";
    let lastFeedbackSeq = 0;

    function addListener(target, type, listener, options) {
      if (!target || typeof target.addEventListener !== "function") return;
      target.addEventListener(type, listener, options);
      listeners.push([target, type, listener, options]);
    }

    function disposeListeners() {
      for (const binding of listeners) {
        binding[0].removeEventListener(binding[1], binding[2], binding[3]);
      }
      listeners.length = 0;
    }

    function socketOpen() {
      const socket = record && record.socket;
      return Boolean(socket && typeof socket.send === "function" && (socket.readyState === 1 || socket.readyState == null));
    }

    function send(event, data) {
      if (!socketOpen()) return false;
      try {
        record.socket.send(JSON.stringify({ event: event, data: data || {} }));
        return true;
      } catch (e) {
        console.error(`[gosx] hub input send error for ${record.entry.id}:`, e);
        return false;
      }
    }

    function clientID() {
      return gosxClientID();
    }

    function basePayload(player) {
      const payload = { player: player || config.player };
      if (config.slotToken) payload.slotToken = config.slotToken;
      const id = clientID();
      if (id) payload.clientId = id;
      return payload;
    }

    function sendReady() {
      if (readySent || !socketOpen()) return;
      readySent = send(config.readyEvent, basePayload(config.player));
    }

    function publishJSON(signal, data) {
      if (!signal) return;
      try {
        const result = setSharedSignalJSON(signal, JSON.stringify(data || {}));
        if (typeof result === "string" && result !== "") {
          console.error(`[gosx] hub input signal error (${record.entry.id}/${signal}):`, result);
        }
      } catch (e) {
        console.error(`[gosx] hub input signal error (${record.entry.id}/${signal}):`, e);
      }
    }

    function publishTrainingState(extra) {
      publishJSON(config.trainingSignal, Object.assign({
        enabled: trainingVisible,
        paused: false,
        recording: false,
      }, extra || {}));
    }

    function sendTraining(action) {
      trainingVisible = true;
      publishTrainingState({ action: action });
      const payload = basePayload(config.player);
      payload.action = action;
      send(config.trainingEvent, payload);
    }

    function setKey(event, active) {
      if (!event) return;
      if (event.code) keys[event.code] = active;
      if (event.key) keys[String(event.key).toLowerCase()] = active;
      if (!active) return;

      if (event.code === "F2") {
        trainingVisible = !trainingVisible;
        publishTrainingState();
        event.preventDefault();
        return;
      }
      if (event.code === "F3") {
        event.preventDefault();
        sendTraining("pause");
        return;
      }
      if (event.code === "F4") {
        event.preventDefault();
        sendTraining("step");
        return;
      }
      if (event.code === "F5") {
        event.preventDefault();
        sendTraining("dummy");
        return;
      }
      if (hubInputCapturesKey(event)) {
        event.preventDefault();
      }
    }

    function keyDown() {
      for (let i = 0; i < arguments.length; i++) {
        const name = arguments[i];
        if (keys[name] || keys[String(name).toLowerCase()]) return true;
      }
      return false;
    }

    function touchControl(event) {
      const target = event && event.target;
      const node = target && target.closest ? target : target && target.parentElement;
      const control = node && node.closest ? node.closest("[data-dir],[data-btn]") : null;
      if (!control) return null;
      if (config.touchRoot && (!control.closest || !control.closest(config.touchRoot))) {
        return null;
      }
      return control;
    }

    function touchKey(control) {
      if (!control || !control.dataset) return "";
      if (control.dataset.dir) return "dir:" + control.dataset.dir;
      if (control.dataset.btn) return "btn:" + control.dataset.btn;
      return "";
    }

    function updateTouch(key, active) {
      if (!key) return;
      const next = Math.max(0, (touchCounts[key] || 0) + (active ? 1 : -1));
      touchCounts[key] = next;
      const value = next > 0;
      const parts = key.split(":");
      if (parts[0] === "dir" && Object.prototype.hasOwnProperty.call(touch, parts[1])) {
        touch[parts[1]] = value;
      } else if (parts[0] === "btn" && Object.prototype.hasOwnProperty.call(touch, parts[1])) {
        touch[parts[1]] = value;
      }
    }

    function onPointerDown(event) {
      const control = touchControl(event);
      if (!control) return;
      const key = touchKey(control);
      if (!key) return;
      activePointers.set(event.pointerId, key);
      updateTouch(key, true);
      if (control.setPointerCapture && event.pointerId != null) {
        try {
          control.setPointerCapture(event.pointerId);
        } catch (_e) {
        }
      }
      event.preventDefault();
    }

    function onPointerUp(event) {
      const fallback = touchKey(touchControl(event));
      const key = activePointers.get(event.pointerId) || fallback;
      activePointers.delete(event.pointerId);
      updateTouch(key, false);
      if (key) event.preventDefault();
    }

    function onBlur() {
      for (const key of Object.keys(keys)) keys[key] = false;
      for (const key of Object.keys(touchCounts)) {
        touchCounts[key] = 0;
        updateTouch(key, false);
      }
      activePointers.clear();
    }

    function gamepads() {
      const nav = window.navigator;
      if (!nav || typeof nav.getGamepads !== "function") return [];
      try {
        return Array.prototype.slice.call(nav.getGamepads() || []).filter(Boolean);
      } catch (_e) {
        return [];
      }
    }

    function readDirection(pad, includePrimary, player) {
      let up = includePrimary && (keyDown("KeyW", "w", "ArrowUp", "arrowup") || touch.up);
      let down = includePrimary && (keyDown("KeyS", "s", "ArrowDown", "arrowdown") || touch.down);
      let left = includePrimary && (keyDown("KeyA", "a", "ArrowLeft", "arrowleft") || touch.left);
      let right = includePrimary && (keyDown("KeyD", "d", "ArrowRight", "arrowright") || touch.right);

      if (pad) {
        up = up || gamepadPressed(pad, 12);
        down = down || gamepadPressed(pad, 13);
        left = left || gamepadPressed(pad, 14);
        right = right || gamepadPressed(pad, 15);
        const axes = Array.isArray(pad.axes) ? pad.axes : [];
        if (hubInputNumber(axes[1], 0) < -0.5) up = true;
        if (hubInputNumber(axes[1], 0) > 0.5) down = true;
        if (hubInputNumber(axes[0], 0) < -0.5) left = true;
        if (hubInputNumber(axes[0], 0) > 0.5) right = true;
      }

      const p2 = Number(player) === 2;
      const forward = p2 ? left : right;
      const back = p2 ? right : left;

      if (up && forward) return 2;
      if (up && back) return 8;
      if (down && forward) return 4;
      if (down && back) return 6;
      if (up) return 1;
      if (forward) return 3;
      if (down) return 5;
      if (back) return 7;
      return 0;
    }

    function readButtons(pad, includePrimary) {
      let buttons = 0;
      if (includePrimary) {
        if (keyDown("KeyU", "u") || touch.lp) buttons |= 1;
        if (keyDown("KeyI", "i") || touch.hp) buttons |= 2;
        if (keyDown("KeyJ", "j") || touch.lk) buttons |= 4;
        if (keyDown("KeyK", "k") || touch.hk) buttons |= 8;
        if (keyDown("KeyL", "l", "Space", " ") || touch.guard) buttons |= 16;
      }
      if (pad) {
        if (gamepadPressed(pad, 0)) buttons |= 1;
        if (gamepadPressed(pad, 1)) buttons |= 2;
        if (gamepadPressed(pad, 2)) buttons |= 4;
        if (gamepadPressed(pad, 3)) buttons |= 8;
        if (gamepadPressed(pad, 4) || gamepadPressed(pad, 5)) buttons |= 16;
      }
      return buttons;
    }

    function readInput(pad, includePrimary, player) {
      return {
        dir: readDirection(pad, includePrimary, player),
        btn: readButtons(pad, includePrimary),
      };
    }

    function sendInput(player, input) {
      const payload = basePayload(player);
      payload.dir = input.dir;
      payload.btn = input.btn;
      send(config.event, payload);
    }

    function publishCue(pads) {
      const connected = pads.length > 0;
      const cue = {
        connected: connected,
        active: true,
        pads: pads.length,
        padCount: pads.length,
        player: config.player,
        state: connected ? "pad" : "touch",
        title: connected ? "GAMEPAD LINKED" : "GRAB A GAMEPAD",
        copy: connected ? "Pad mapped: A/B/X/Y, shoulders guard." : "Keyboard and touch are live until a pad is connected.",
        mode: config.local ? "LOCAL VS" : "ONLINE",
        perf: "",
      };
      const signature = JSON.stringify(cue);
      if (signature === lastCue) return;
      lastCue = signature;
      publishJSON(config.signal, cue);
    }

    function onHubMessage(message) {
      if (!message || message.event !== "tick") return;
      const data = message.data || {};
      const event = data.event || {};
      const seq = Math.floor(hubInputNumber(event.seq, 0));
      if (!seq || seq === lastFeedbackSeq) return;
      lastFeedbackSeq = seq;
      const kind = String(event.kind || "");
      if (!kind || kind === "none") return;

      const damage = Math.max(0, hubInputNumber(event.damage, 0));
      const blocked = Boolean(event.blocked) || kind === "block";
      const special = Boolean(event.counter || event.punish || event.launcher || event.guardCancel || event.justGuard || event.armor)
        || kind === "throw" || kind === "throw_tech";
      let intensity = blocked ? 0.18 : Math.min(0.85, 0.24 + damage / 260);
      if (special) intensity = Math.min(1, intensity + 0.22);

      let feedback = "hit";
      if (kind === "block") feedback = "block";
      if (kind === "just_guard" || kind === "guard_cancel") feedback = "guard";
      if (kind === "armor") feedback = "armor";
      if (kind === "throw" || kind === "throw_tech") feedback = "throw";
      playArcadeSFX(feedback);
      vibrateGamepads(feedback, intensity, special ? 130 : 75);
    }

    function pump() {
      if (disposed) return;
      sendReady();
      const pads = gamepads();
      publishCue(pads);
      if (socketOpen()) {
        if (config.local) {
          sendInput(1, readInput(pads[0], true, 1));
          sendInput(2, readInput(pads[1], false, 2));
        } else {
          sendInput(config.player, readInput(pads[0], true, config.player));
        }
      }
    }

    function tick() {
      if (disposed) return;
      pump();
      timer = setTimeout(tick, config.sendEveryMS);
    }

    addListener(document, "keydown", function(event) { setKey(event, true); }, { passive: false });
    addListener(document, "keyup", function(event) { setKey(event, false); }, { passive: false });
    addListener(document, "pointerdown", onPointerDown, { passive: false });
    addListener(document, "pointerup", onPointerUp, { passive: false });
    addListener(document, "pointercancel", onPointerUp, { passive: false });
    addListener(window, "blur", onBlur);
    publishTrainingState();
    tick();

    return {
      flush: pump,
      onMessage: onHubMessage,
      dispose: function() {
        disposed = true;
        if (timer) {
          clearTimeout(timer);
          timer = 0;
        }
        disposeListeners();
        onBlur();
        publishJSON(config.signal, { connected: false, active: false, pads: 0 });
        publishJSON(config.trainingSignal, { enabled: false, paused: false, recording: false });
      },
    };
  }

  function createArcadeSelectHubController(record, config) {
    const root = controllerQuery(config.root || ".landing") || document.body || document.documentElement;
    const state = {
      selectedChar: 0,
      selectedAction: "cpu",
      inputMode: "touch",
      padState: "touch",
      padTitle: "TAP START",
      padCopy: "Gamepad recommended",
      padStatus: "TOUCH READY",
      localSub: "2 PADS",
      selectVisible: false,
      actionState: "ready",
      actionTitle: "READY",
      actionCopy: "Pick fast. Fight faster.",
      onlineLabel: "FIND MATCH",
      onlineSub: "ONLINE",
      queued: false,
      busy: false,
      prompt: "PICK A FIGHTER",
      pressStart: "TAP START",
    };
    const attract = { phase: "title", paradeIndex: 0, active: true };
    const vs = {
      active: false,
      left: { name: "FIGHTER", beast: "SURGE FORM", accent: "#f25f5c" },
      right: { name: "FIGHTER", beast: "SURGE FORM", accent: "#5ce1e6" },
    };
    const actionOrder = ["cpu", "local", "online"];
    const listeners = [];
    const timers = [];
    const intervals = [];
    let disposed = false;
    let readySent = false;
    let previousButtons = Object.create(null);
    let previousDirection = 0;
    let paradeInterval = 0;

    function addListener(target, type, listener, options) {
      if (!target || typeof target.addEventListener !== "function") return;
      target.addEventListener(type, listener, options);
      listeners.push([target, type, listener, options]);
    }

    function schedule(fn, ms) {
      const timer = setTimeout(function() {
        const index = timers.indexOf(timer);
        if (index >= 0) timers.splice(index, 1);
        if (!disposed) fn();
      }, ms);
      timers.push(timer);
      return timer;
    }

    function every(fn, ms) {
      const timer = setInterval(function() {
        if (!disposed) fn();
      }, ms);
      intervals.push(timer);
      return timer;
    }

    function clearParadeInterval() {
      if (!paradeInterval) return;
      clearInterval(paradeInterval);
      const index = intervals.indexOf(paradeInterval);
      if (index >= 0) intervals.splice(index, 1);
      paradeInterval = 0;
    }

    function publish(signal, value) {
      publishSharedJSON(signal, value, record.entry.id);
    }

    function publishState() {
      publish(config.signal || "$landing", state);
      publish(config.attractSignal, attract);
      publish(config.vsSignal, vs);
      applyArcadeDOMState(root, state, attract);
    }

    function publishLobby(players, queueSize) {
      publish(config.lobbySignal, { players: players || 0, queue: { size: queueSize || 0 } });
    }

    function socketOpen() {
      const socket = record && record.socket;
      return Boolean(socket && typeof socket.send === "function" && (socket.readyState === 1 || socket.readyState == null));
    }

    function send(event, data) {
      if (!socketOpen()) return false;
      try {
        record.socket.send(JSON.stringify({ event: event, data: data || {} }));
        return true;
      } catch (e) {
        console.error(`[gosx] arcade-select send error for ${record.entry.id}:`, e);
        return false;
      }
    }

    function basePayload() {
      const payload = { clientId: gosxClientID() || "local-player" };
      if (config.username) payload.name = config.username;
      return payload;
    }

    function sendReady() {
      if (readySent || !socketOpen()) return;
      readySent = send(config.readyEvent || "join", basePayload());
    }

    function actionStatus(action) {
      const confirm = state.inputMode === "gamepad" ? "A" : "Tap";
      if (action === "local" && connectedGamepads().length < config.minLocalGamepads) {
        return { title: "2 PADS NEEDED", copy: "Local versus is gamepad-only." };
      }
      if (action === "local") return { title: "VERSUS READY", copy: confirm + " starts same-screen versus." };
      if (action === "online") return { title: "ONLINE READY", copy: confirm + " searches for a match." };
      return { title: "CPU READY", copy: confirm + " starts a solo fight." };
    }

    function setActionStatus(kind, title, copy) {
      state.actionState = kind || "ready";
      state.actionTitle = title || "READY";
      state.actionCopy = copy || "";
      state.prompt = state.actionTitle;
      publishState();
    }

    function updateReadyStatus() {
      if (state.busy || state.queued) return;
      const status = actionStatus(state.selectedAction);
      setActionStatus("ready", status.title, status.copy);
    }

    function setQueued(queued) {
      state.queued = Boolean(queued);
      state.onlineLabel = state.queued ? "CANCEL QUEUE" : "FIND MATCH";
      state.onlineSub = state.queued ? "SEARCHING" : "ONLINE";
      state.busy = false;
      publishState();
    }

    function setBusy(busy) {
      state.busy = Boolean(busy);
      publishState();
    }

    function setInputMode(mode, pads) {
      const nextPads = pads || connectedGamepads();
      state.inputMode = mode === "gamepad" ? "gamepad" : "touch";
      state.pressStart = state.inputMode === "gamepad" ? "PRESS START" : "TAP START";
      const count = nextPads.length;
      state.padState = count ? "ready" : "touch";
      state.padTitle = count > 1 ? count + " PADS READY" : (count === 1 ? "PAD 1 READY" : "TAP START");
      state.padCopy = count > 1 ? "LOCAL 1V1" : (count === 1 ? "A / START" : "Gamepad recommended");
      state.padStatus = count > 1 ? count + " PADS READY" : (count === 1 ? "PAD 1 READY" : "TOUCH READY");
      state.localSub = count > 1 ? "VERSUS" : "2 PADS";
      publishState();
      updateReadyStatus();
    }

    function updateInputModeFromGamepads() {
      const pads = connectedGamepads();
      setInputMode(pads.length ? "gamepad" : "touch", pads);
    }

    function characterIDs() {
      const ids = [];
      for (const card of controllerQueryAll(".select-screen .char-card")) {
        const id = Number(card.dataset && card.dataset.char);
        if (Number.isFinite(id)) ids.push(id);
      }
      return ids.length ? ids : [0, 1, 2, 3];
    }

    function selectCharacter(charID) {
      const id = Math.max(0, Math.floor(hubInputNumber(charID, 0)));
      const changed = state.selectedChar !== id;
      state.selectedChar = id;
      publishState();
      if (changed) playArcadeSFX("move");
    }

    function cycleCharacter(delta) {
      const ids = characterIDs();
      let index = ids.indexOf(state.selectedChar);
      if (index < 0) index = 0;
      selectCharacter(ids[(index + delta + ids.length) % ids.length]);
    }

    function setSelectedAction(action) {
      if (actionOrder.indexOf(action) < 0) return;
      const changed = state.selectedAction !== action;
      state.selectedAction = action;
      if (changed) playArcadeSFX("move");
      publishState();
      updateReadyStatus();
    }

    function cycleAction(delta) {
      let index = actionOrder.indexOf(state.selectedAction);
      if (index < 0) index = 0;
      setSelectedAction(actionOrder[(index + delta + actionOrder.length) % actionOrder.length]);
    }

    function cancelQueue() {
      if (!state.queued) return false;
      send(config.trainingEvent || "dequeue", { clientId: gosxClientID() || "local-player" });
      setQueued(false);
      setActionStatus("ready", "QUEUE CANCELED", "Pick another fight.");
      return true;
    }

    function breakAttract() {
      if (!attract.active) return;
      playArcadeSFX("confirm");
      attract.active = false;
      state.selectVisible = true;
      selectCharacter(0);
      setSelectedAction("cpu");
      publishState();
      updateReadyStatus();
      sendReady();
    }

    function setAttractPhase(phase) {
      attract.phase = phase;
      publishState();
    }

    function setParadeIndex(index) {
      attract.paradeIndex = index;
      publishState();
    }

    function startAttractLoop() {
      clearParadeInterval();
      setAttractPhase("title");
      schedule(function() {
        if (!attract.active) return;
        setAttractPhase("parade");
        setParadeIndex(0);
        paradeInterval = every(function() {
          if (!attract.active || attract.phase !== "parade") return;
          setParadeIndex((attract.paradeIndex + 1) % 4);
        }, 2600);
        schedule(function() {
          if (!attract.active) return;
          clearParadeInterval();
          setAttractPhase("pressstart");
          schedule(startAttractLoop, 2600);
        }, 10400);
      }, 2600);
    }

    function onAttractInput(event) {
      if (!attract.active) return;
      event.__gosxArcadeConsumed = true;
      setInputMode("touch");
      if (event && typeof event.preventDefault === "function") event.preventDefault();
      breakAttract();
    }

    function onSelectKey(event) {
      if (event.__gosxArcadeConsumed || attract.active || state.busy) return;
      const target = event.target;
      if (target && /^(input|textarea|select)$/i.test(target.tagName || "")) return;
      let handled = true;
      switch (event.code) {
        case "ArrowLeft":
        case "KeyA":
          cycleCharacter(-1);
          break;
        case "ArrowRight":
        case "KeyD":
          cycleCharacter(1);
          break;
        case "ArrowUp":
        case "KeyW":
          cycleAction(-1);
          break;
        case "ArrowDown":
        case "KeyS":
          cycleAction(1);
          break;
        case "Digit1":
          selectCharacter(0);
          break;
        case "Digit2":
          selectCharacter(1);
          break;
        case "Digit3":
          selectCharacter(2);
          break;
        case "Digit4":
          selectCharacter(3);
          break;
        case "KeyC":
          setSelectedAction("cpu");
          break;
        case "KeyL":
          setSelectedAction("local");
          break;
        case "KeyO":
          setSelectedAction("online");
          break;
        case "Enter":
        case "Space":
          triggerAction(state.selectedAction);
          break;
        case "Escape":
          handled = cancelQueue();
          break;
        default:
          handled = false;
      }
      if (handled && event && typeof event.preventDefault === "function") event.preventDefault();
    }

    function onRootClick(event) {
      const target = event && event.target;
      const card = closestElement(target, ".char-card");
      if (card && card.dataset && card.dataset.char != null) {
        selectCharacter(Number(card.dataset.char));
        return;
      }
      const action = closestElement(target, "[data-action]");
      if (action && action.dataset && action.dataset.action) {
        triggerAction(action.dataset.action);
      }
    }

    function onRootFocus(event) {
      const action = closestElement(event && event.target, "[data-action]");
      if (action && action.dataset && action.dataset.action) {
        setSelectedAction(action.dataset.action);
      }
    }

    function triggerAction(action) {
      if (state.busy) return;
      if (action === "local" && connectedGamepads().length < config.minLocalGamepads) {
        setSelectedAction("local");
        setActionStatus("error", "2 PADS NEEDED", "Local versus is gamepad-only.");
        playArcadeSFX("move");
        return;
      }
      if (action === "online") {
        playArcadeSFX("confirm");
        if (state.queued) {
          if (socketOpen()) {
            setBusy(true);
            send(config.trainingEvent || "dequeue", { clientId: gosxClientID() || "local-player" });
          } else {
            setQueued(false);
            setActionStatus("ready", "QUEUE CANCELED", "Pick another fight.");
          }
          return;
        }
        if (socketOpen()) {
          setBusy(true);
          setActionStatus("loading", "JOINING QUEUE", "Opening the lobby channel.");
          send(config.event || "queue", Object.assign(basePayload(), { characterId: state.selectedChar }));
        } else {
          setActionStatus("error", "LOBBY CONNECTING", "Try online again in a moment.");
        }
        return;
      }
      if (action === "local") {
        startAPIMatch(config.localEndpoint, {
          playerId: gosxClientID() || "local-player",
          playerName: config.username || "Fighter",
          p1CharacterId: state.selectedChar,
          p2CharacterId: localOpponentChar(),
        }, "STARTING LOCAL 1V1", "Loading both fighters.");
        return;
      }
      startAPIMatch(config.cpuEndpoint, {
        playerId: gosxClientID() || "local-player",
        playerName: config.username || "Fighter",
        characterId: state.selectedChar,
      }, "STARTING CPU FIGHT", "Locking in the matchup.");
    }

    function startAPIMatch(endpoint, payload, title, copy) {
      playArcadeSFX("confirm");
      setBusy(true);
      setActionStatus("loading", title, copy);
      fetch(endpoint, {
        method: "POST",
        headers: gosxIdentityHeaders({ "Content-Type": "application/json" }),
        body: JSON.stringify(payload),
      }).then(function(response) {
        if (!response || !response.ok) throw new Error("match start failed");
        return response.json();
      }).then(startFightTransition, function() {
        setBusy(false);
        setActionStatus("error", "START FAILED", "Try again.");
      });
    }

    function startFightTransition(data) {
      const payload = data || {};
      setBusy(true);
      setActionStatus("loading", "MATCH LOCKED", "Entering the arena.");
      const playerNo = payload.playerNo || 1;
      let p1Char = payload.p1CharId;
      let p2Char = payload.p2CharId;
      if (p1Char == null) p1Char = playerNo === 2 ? (payload.opponentCharId || 0) : state.selectedChar;
      if (p2Char == null) p2Char = playerNo === 2 ? state.selectedChar : (payload.opponentCharId || 0);
      const opponentChar = playerNo === 2 ? p1Char : p2Char;
      const current = {
        clientId: gosxClientID() || "local-player",
        matchId: payload.matchId || "",
        mode: payload.mode || "online",
        playerNo: playerNo,
        slotToken: payload.slotToken || payload.token || "",
        p1CharId: p1Char,
        p2CharId: p2Char,
      };
      fetch(config.fightCurrentEndpoint, {
        method: "POST",
        headers: gosxIdentityHeaders({ "Content-Type": "application/json" }),
        body: JSON.stringify(current),
      }).catch(function() {}).finally(function() {
        showVS(opponentChar);
        schedule(function() {
          navigateManaged(config.fightPath || "/fight");
        }, 760);
      });
    }

    function showVS(opponentChar) {
      vs.left = characterMeta(state.selectedChar);
      vs.right = characterMeta(opponentChar || 0);
      vs.active = true;
      publishState();
      schedule(function() {
        vs.active = false;
        publishState();
      }, 740);
    }

    function localOpponentChar() {
      const ids = characterIDs();
      let index = ids.indexOf(state.selectedChar);
      if (index < 0) index = 0;
      return ids[(index + 1 + ids.length) % ids.length];
    }

    function characterMeta(charID) {
      const card = controllerQuery('.select-screen .char-card[data-char="' + String(charID) + '"]');
      if (!card || !card.dataset) {
        return { name: "FIGHTER", beast: "SURGE FORM", accent: "#f25f5c" };
      }
      return {
        name: card.dataset.name || "FIGHTER",
        beast: card.dataset.beast || "SURGE FORM",
        accent: card.dataset.accent || "#f25f5c",
      };
    }

    function onHubMessage(message) {
      if (!message) return;
      const data = message.data || {};
      if (message.event === "lobby_state") {
        publishLobby(data.count || data.playerCount || 0, data.queueSize || 0);
      } else if (message.event === "match_found") {
        startFightTransition(data);
      } else if (message.event === "queued") {
        setQueued(true);
        setActionStatus("queued", "MATCHMAKING", "Waiting for a challenger.");
        publishLobby(data.count || data.playerCount || 0, data.queueSize || 0);
      } else if (message.event === "dequeued") {
        setQueued(false);
        setActionStatus("ready", "QUEUE CANCELED", "Pick another fight.");
        publishLobby(0, 0);
      }
    }

    function pollGamepad() {
      const pads = connectedGamepads();
      if (!pads.length) {
        setInputMode("touch", pads);
        previousButtons = Object.create(null);
        previousDirection = 0;
        return;
      }
      const pad = pads[0];
      setInputMode("gamepad", pads);
      if (attract.active) {
        if (gamepadButtonEdge(pad, 9) || gamepadButtonEdge(pad, 0)) breakAttract();
        return;
      }
      const dir = gamepadDirectionEdge(pad);
      if (dir === 7) cycleCharacter(-1);
      if (dir === 3) cycleCharacter(1);
      if (dir === 1) cycleAction(-1);
      if (dir === 5) cycleAction(1);
      if (gamepadButtonEdge(pad, 4)) cycleCharacter(-1);
      if (gamepadButtonEdge(pad, 5)) cycleCharacter(1);
      if (gamepadButtonEdge(pad, 6)) cycleAction(-1);
      if (gamepadButtonEdge(pad, 7)) cycleAction(1);
      if (gamepadButtonEdge(pad, 1) || gamepadButtonEdge(pad, 8)) cancelQueue();
      if (gamepadButtonEdge(pad, 0) || gamepadButtonEdge(pad, 9)) triggerAction(state.selectedAction);
    }

    function gamepadButtonEdge(pad, index) {
      const key = String(pad && pad.index != null ? pad.index : 0) + ":" + index;
      const pressed = gamepadPressed(pad, index);
      const wasPressed = Boolean(previousButtons[key]);
      previousButtons[key] = pressed;
      return pressed && !wasPressed;
    }

    function gamepadDirectionEdge(pad) {
      const dir = gamepadMenuDirection(pad);
      const edge = dir && dir !== previousDirection ? dir : 0;
      previousDirection = dir;
      return edge;
    }

    addListener(document, "keydown", onAttractInput, { passive: false });
    addListener(document, "pointerdown", onAttractInput, { passive: false });
    addListener(document, "touchstart", onAttractInput, { passive: false });
    addListener(document, "keydown", onSelectKey, { passive: false });
    addListener(root, "click", onRootClick);
    addListener(root, "focus", onRootFocus, true);
    addListener(window, "gamepadconnected", updateInputModeFromGamepads);
    addListener(window, "gamepaddisconnected", updateInputModeFromGamepads);
    every(pollGamepad, 90);
    updateInputModeFromGamepads();
    startAttractLoop();
    publishState();
    sendReady();

    return {
      flush: sendReady,
      onMessage: onHubMessage,
      dispose: function() {
        disposed = true;
        clearParadeInterval();
        for (const timer of timers.splice(0)) clearTimeout(timer);
        for (const timer of intervals.splice(0)) clearInterval(timer);
        for (const binding of listeners.splice(0)) {
          binding[0].removeEventListener(binding[1], binding[2], binding[3]);
        }
        stopArcadeSFX();
      },
    };
  }

  function publishSharedJSON(signal, value, scope) {
    if (!signal) return;
    try {
      const result = setSharedSignalJSON(signal, JSON.stringify(value || {}));
      if (typeof result === "string" && result !== "") {
        console.error(`[gosx] shared signal error (${scope || "runtime"}/${signal}):`, result);
      }
    } catch (e) {
      console.error(`[gosx] shared signal error (${scope || "runtime"}/${signal}):`, e);
    }
  }

  function controllerQuery(selector) {
    if (!selector || !document || typeof document.querySelector !== "function") return null;
    try {
      return document.querySelector(selector);
    } catch (_e) {
      return null;
    }
  }

  function controllerQueryAll(selector) {
    if (!selector || !document || typeof document.querySelectorAll !== "function") return [];
    try {
      return Array.from(document.querySelectorAll(selector) || []);
    } catch (_e) {
      return [];
    }
  }

  function closestElement(target, selector) {
    let current = target && target.nodeType === 1 ? target : null;
    while (current) {
      if (elementMatches(current, selector)) return current;
      current = current.parentNode && current.parentNode.nodeType === 1 ? current.parentNode : null;
    }
    return null;
  }

  function elementMatches(element, selector) {
    if (!element || !selector) return false;
    if (typeof element.matches === "function") {
      try {
        return element.matches(selector);
      } catch (_e) {
        return false;
      }
    }
    if (selector === ".char-card") {
      return String(element.getAttribute && element.getAttribute("class") || "").split(/\s+/).includes("char-card");
    }
    if (selector === "[data-action]") {
      return Boolean(element.dataset && element.dataset.action);
    }
    return false;
  }

  function applyArcadeDOMState(root, state, attract) {
    const target = root || controllerQuery(".landing");
    if (target && target.dataset) {
      target.dataset.inputMode = state.inputMode;
      target.dataset.padStatus = state.padState;
    }
    const backdrop = controllerQuery("#attract-backdrop");
    if (backdrop && backdrop.classList) {
      backdrop.classList.toggle("dimmed", !attract.active);
    }
  }

  function connectedGamepads() {
    const nav = window.navigator;
    if (!nav || typeof nav.getGamepads !== "function") return [];
    try {
      return Array.prototype.slice.call(nav.getGamepads() || []).filter(function(pad) {
        return pad && pad.connected !== false;
      });
    } catch (_e) {
      return [];
    }
  }

  function vibrateGamepads(kind, intensity, durationMS) {
    const pads = connectedGamepads();
    if (!pads.length) return;
    const duration = Math.max(20, Math.min(160, Math.floor(hubInputNumber(durationMS, 75))));
    const strong = Math.max(0, Math.min(1, hubInputNumber(intensity, 0.25)));
    let weak = Math.max(0.06, strong * 0.45);
    if (kind === "block" || kind === "guard") weak = Math.min(0.8, strong * 0.85);
    if (kind === "armor" || kind === "throw") weak = Math.min(0.7, strong * 0.55);
    for (const pad of pads) {
      const actuator = gamepadActuator(pad);
      if (actuator && typeof actuator.playEffect === "function") {
        try {
          actuator.playEffect("dual-rumble", {
            duration: duration,
            strongMagnitude: strong,
            weakMagnitude: weak,
          });
          continue;
        } catch (_e) {
        }
      }
      const pulse = pad && pad.hapticActuators && pad.hapticActuators[0];
      if (pulse && typeof pulse.pulse === "function") {
        try {
          pulse.pulse(strong, duration);
        } catch (_e) {}
      }
    }
  }

  function gamepadActuator(pad) {
    if (!pad) return null;
    if (pad.vibrationActuator) return pad.vibrationActuator;
    if (pad.hapticActuators && pad.hapticActuators[0]) return pad.hapticActuators[0];
    return null;
  }

  function gamepadMenuDirection(pad) {
    let x = 0;
    let y = 0;
    const axes = Array.isArray(pad && pad.axes) ? pad.axes : [];
    if (hubInputNumber(axes[0], 0) < -0.45) x = -1;
    if (hubInputNumber(axes[0], 0) > 0.45) x = 1;
    if (hubInputNumber(axes[1], 0) < -0.45) y = -1;
    if (hubInputNumber(axes[1], 0) > 0.45) y = 1;
    if (gamepadPressed(pad, 14)) x = -1;
    if (gamepadPressed(pad, 15)) x = 1;
    if (gamepadPressed(pad, 12)) y = -1;
    if (gamepadPressed(pad, 13)) y = 1;
    if (x < 0) return 7;
    if (x > 0) return 3;
    if (y < 0) return 1;
    if (y > 0) return 5;
    return 0;
  }

  function navigateManaged(url) {
    if (window.__gosx_page_nav && typeof window.__gosx_page_nav.navigate === "function") {
      window.__gosx_page_nav.navigate(url);
      return;
    }
    window.location.href = url;
  }

  const arcadeAudioState = { context: null, active: [] };

  function arcadeAudioContext() {
    const Ctor = window.AudioContext || window.webkitAudioContext;
    if (!Ctor) return null;
    if (!arcadeAudioState.context) {
      try {
        arcadeAudioState.context = new Ctor();
      } catch (_e) {
        arcadeAudioState.context = null;
      }
    }
    return arcadeAudioState.context;
  }

  function playArcadeSFX(kind) {
    const audio = arcadeAudioContext();
    if (!audio || typeof audio.createOscillator !== "function" || typeof audio.createGain !== "function") return;
    if (typeof audio.resume === "function") audio.resume();
    if (kind === "confirm") {
      arcadeTone(audio, 220, 0.055, 0.08, "square");
      arcadeTone(audio, 880, 0.09, 0.08, "square", 18);
      return;
    }
    if (kind === "hit") {
      arcadeTone(audio, 96, 0.045, 0.08, "square");
      arcadeTone(audio, 640, 0.035, 0.06, "triangle", 8);
      arcadeTone(audio, 1380, 0.025, 0.04, "square", 18);
      return;
    }
    if (kind === "block") {
      arcadeTone(audio, 150, 0.035, 0.055, "square");
      arcadeTone(audio, 270, 0.04, 0.035, "triangle", 10);
      return;
    }
    if (kind === "guard") {
      arcadeTone(audio, 420, 0.04, 0.05, "triangle");
      arcadeTone(audio, 980, 0.035, 0.045, "square", 14);
      return;
    }
    if (kind === "armor") {
      arcadeTone(audio, 72, 0.08, 0.075, "square");
      arcadeTone(audio, 144, 0.06, 0.05, "sawtooth", 18);
      return;
    }
    if (kind === "throw") {
      arcadeTone(audio, 110, 0.065, 0.08, "square");
      arcadeTone(audio, 520, 0.05, 0.055, "square", 12);
      return;
    }
    arcadeTone(audio, 440, 0.035, 0.045, "square");
    arcadeTone(audio, 660, 0.04, 0.035, "triangle", 12);
  }

  function arcadeTone(audio, freq, duration, volume, type, detuneMS) {
    const now = audio.currentTime || 0;
    const osc = audio.createOscillator();
    const gain = audio.createGain();
    osc.type = type || "square";
    osc.frequency.value = freq;
    gain.gain.setValueAtTime(0.0001, now);
    gain.gain.exponentialRampToValueAtTime(Math.max(0.0001, volume), now + 0.006);
    gain.gain.exponentialRampToValueAtTime(0.0001, now + duration + 0.04);
    osc.connect(gain);
    gain.connect(audio.destination);
    const record = { source: osc, nodes: [osc, gain] };
    arcadeAudioState.active.push(record);
    osc.onended = function() {
      releaseArcadeAudio(record, false);
    };
    while (arcadeAudioState.active.length > 16) {
      releaseArcadeAudio(arcadeAudioState.active[0], true);
    }
    osc.start(now + Math.max(0, hubInputNumber(detuneMS, 0)) / 1000);
    osc.stop(now + duration + 0.08 + Math.max(0, hubInputNumber(detuneMS, 0)) / 1000);
  }

  function releaseArcadeAudio(record, stop) {
    if (!record) return;
    const index = arcadeAudioState.active.indexOf(record);
    if (index >= 0) arcadeAudioState.active.splice(index, 1);
    if (record.source) {
      record.source.onended = null;
      if (stop && typeof record.source.stop === "function") {
        try {
          record.source.stop(0);
        } catch (_e) {}
      }
    }
    for (const node of record.nodes || []) {
      if (node && typeof node.disconnect === "function") {
        try {
          node.disconnect();
        } catch (_e) {}
      }
    }
  }

  function stopArcadeSFX() {
    arcadeAudioState.active.slice().forEach(function(record) {
      releaseArcadeAudio(record, true);
    });
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

  function attachHubSocketHandlers(record) {
    const entry = record.entry;
    const socket = record.socket;
    record.inputController = createHubInputController(record);
    try {
      socket.binaryType = "arraybuffer";
    } catch (_e) {
    }
    socket.onopen = function() {
      if (record.inputController && typeof record.inputController.flush === "function") {
        record.inputController.flush();
      }
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
    applyHubBindings(entry, message);
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

  window.__gosx_disconnect_hub = function(hubID) {
    const record = window.__gosx.hubs.get(hubID);
    if (!record) return;

    if (record.reconnectTimer) {
      clearTimeout(record.reconnectTimer);
      record.reconnectTimer = null;
    }
    if (record.inputController && typeof record.inputController.dispose === "function") {
      record.inputController.dispose();
      record.inputController = null;
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
