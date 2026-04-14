(function() {
  "use strict";

  var runtimeApi = window.__gosx_runtime_api || {};
  var setAttrValue = runtimeApi.setAttrValue || function() {};
  var setStyleValue = runtimeApi.setStyleValue || function() {};
  var gosxSubscribeSharedSignal = runtimeApi.gosxSubscribeSharedSignal || function() { return function() {}; };
  var setSharedSignalValue = runtimeApi.setSharedSignalValue || function() {};
  var gosxTextLayoutRevision = runtimeApi.gosxTextLayoutRevision || function() { return 0; };
  var normalizeTextLayoutOverflow = runtimeApi.normalizeTextLayoutOverflow || function() { return "ellipsis"; };
  var layoutBrowserText = runtimeApi.layoutBrowserText || function() { return null; };
  var applyTextLayoutPresentation = runtimeApi.applyTextLayoutPresentation || function() {};
  var onTextLayoutInvalidated = runtimeApi.onTextLayoutInvalidated || function() { return function() {}; };

  let pendingManifest = null;

  function runtimeReady() {
    return (
      typeof window.__gosx_hydrate === "function" ||
      typeof window.__gosx_action === "function" ||
      typeof window.__gosx_set_shared_signal === "function"
    );
  }

  function loadManifest() {
    const el = document.getElementById("gosx-manifest");
    if (!el) return null;

    try {
      return JSON.parse(el.textContent);
    } catch (e) {
      console.error("[gosx] failed to parse manifest:", e);
      if (window.__gosx && typeof window.__gosx.reportIssue === "function") {
        window.__gosx.reportIssue({
          scope: "bootstrap",
          type: "manifest",
          source: "gosx-manifest",
          element: el,
          message: "failed to parse gosx manifest",
          error: e,
          fallback: "server",
        });
      }
      return null;
    }
  }

  async function loadRuntime(runtimeRef) {
    if (typeof Go === "undefined") {
      console.error("[gosx] wasm_exec.js must be loaded before bootstrap.js");
      if (window.__gosx && typeof window.__gosx.reportIssue === "function") {
        window.__gosx.reportIssue({
          scope: "bootstrap",
          type: "runtime",
          source: runtimeRef && runtimeRef.path,
          ref: runtimeRef && runtimeRef.path,
          message: "wasm_exec.js must be loaded before bootstrap.js",
          fallback: "server",
        });
      }
      return;
    }

    const go = new Go();

    try {
      const response = await fetchRuntimeResponse(runtimeRef);
      const result = await instantiateRuntimeModule(response, go.importObject);
      go.run(result.instance);
    } catch (e) {
      console.error("[gosx] failed to load WASM runtime:", e);
      if (window.__gosx && typeof window.__gosx.reportIssue === "function") {
        window.__gosx.reportIssue({
          scope: "bootstrap",
          type: "runtime",
          source: runtimeRef && runtimeRef.path,
          ref: runtimeRef && runtimeRef.path,
          message: "failed to load wasm runtime",
          error: e,
          fallback: "server",
        });
      }
    }
  }

  async function fetchRuntimeResponse(runtimeRef) {
    const response = await fetch(runtimeRef.path);
    if (!response.ok) {
      throw new Error("runtime fetch failed with status " + response.status);
    }
    return response;
  }

  async function instantiateRuntimeModule(response, importObject) {
    if (supportsInstantiateStreaming()) {
      return instantiateRuntimeStreaming(response, importObject);
    }
    return instantiateRuntimeBytes(response, importObject);
  }

  function supportsInstantiateStreaming() {
    return typeof WebAssembly.instantiateStreaming === "function";
  }

  async function instantiateRuntimeStreaming(response, importObject) {
    try {
      return await WebAssembly.instantiateStreaming(response.clone(), importObject);
    } catch (streamErr) {
      return instantiateRuntimeBytes(response, importObject);
    }
  }

  async function instantiateRuntimeBytes(response, importObject) {
    const bytes = await response.arrayBuffer();
    return WebAssembly.instantiate(bytes, importObject);
  }

  async function fetchProgram(programRef, programFormat) {
    try {
      const resp = await fetch(programRef);
      if (!resp.ok) {
        console.error(`[gosx] failed to fetch program ${programRef}: ${resp.status}`);
        return null;
      }

      if (programFormat === "wasm" || programFormat === "bin") {
        return new Uint8Array(await resp.arrayBuffer());
      }
      return await resp.text();
    } catch (e) {
      console.error(`[gosx] error fetching program ${programRef}:`, e);
      return null;
    }
  }

  function inferProgramFormat(entry) {
    if (entry.programFormat) return entry.programFormat;
    if (typeof entry.programRef === "string" && entry.programRef.endsWith(".gxi")) {
      return "bin";
    }
    return "json";
  }

  const loadedScriptTags = new Map();

  function loadScriptTag(src) {
    if (!src) return Promise.resolve();
    if (loadedScriptTags.has(src)) {
      return loadedScriptTags.get(src);
    }
    const promise = new Promise(function(resolve, reject) {
      const script = document.createElement("script");
      script.src = src;
      script.onload = resolve;
      script.onerror = function() {
        reject(new Error("failed to load script: " + src));
      };
      (document.head || document.documentElement).appendChild(script);
    });
    loadedScriptTags.set(src, promise);
    return promise;
  }

  function engineFrame(callback) {
    if (typeof window.requestAnimationFrame === "function") {
      return window.requestAnimationFrame(callback);
    }
    return setTimeout(function() {
      callback(Date.now());
    }, 16);
  }

  function cancelEngineFrame(handle) {
    if (typeof window.cancelAnimationFrame === "function") {
      window.cancelAnimationFrame(handle);
      return;
    }
    clearTimeout(handle);
  }

  function gosxInputState() {
    if (!window.__gosx.input) {
      window.__gosx.input = {
        pending: null,
        frameHandle: 0,
        providers: Object.create(null),
      };
    }
    return window.__gosx.input;
  }

  function queueInputSignal(name, value) {
    if (!name) return;
    const state = gosxInputState();
    if (!state.pending) {
      state.pending = Object.create(null);
    }
    state.pending[name] = value;
    scheduleInputFlush();
  }

  function scheduleInputFlush() {
    const state = gosxInputState();
    if (state.frameHandle) return;
    state.frameHandle = engineFrame(function() {
      state.frameHandle = 0;
      flushInputSignals();
    });
  }

  function flushInputSignals() {
    const state = gosxInputState();
    const payload = state.pending;
    state.pending = null;
    if (!payload) return;

    const setInputBatch = window.__gosx_set_input_batch;
    if (typeof setInputBatch !== "function") {
      for (const [name, value] of Object.entries(payload)) {
        setSharedSignalValue(name, value);
      }
      return;
    }

    try {
      const result = setInputBatch(JSON.stringify(payload));
      if (typeof result === "string" && result !== "") {
        console.error("[gosx] input batch error:", result);
      }
    } catch (e) {
      console.error("[gosx] input batch error:", e);
    }
  }

  function capabilityList(entry) {
    return Array.isArray(entry && entry.capabilities) ? entry.capabilities : [];
  }

  function activateInputProviders(entry) {
    for (const capability of capabilityList(entry)) {
      activateInputProvider(capability, entry);
    }
  }

  function activateInputProvider(capability, entry) {
    const state = gosxInputState();
    const current = state.providers[capability];
    if (current) {
      current.refCount += 1;
      return;
    }

    const provider = createInputProvider(capability, entry);
    if (!provider) {
      return;
    }

    provider.refCount = 1;
    state.providers[capability] = provider;
  }

  function releaseInputProviders(record) {
    for (const capability of capabilityList(record)) {
      releaseInputProvider(capability);
    }
  }

  function releaseInputProvider(capability) {
    const state = gosxInputState();
    const provider = state.providers[capability];
    if (!provider) return;

    provider.refCount -= 1;
    if (provider.refCount > 0) {
      return;
    }

    if (typeof provider.dispose === "function") {
      provider.dispose();
    }
    delete state.providers[capability];
  }

  function createInputProvider(capability, entry) {
    switch (capability) {
      case "keyboard":
        return createKeyboardInputProvider();
      case "pointer":
        return createPointerInputProvider();
      case "gamepad":
        return createGamepadInputProvider();
      case "text-input":
        return createTextInputProvider(entry);
      default:
        return null;
    }
  }

  function createKeyboardInputProvider() {
    const pressed = new Set();

    function onKey(event) {
      const key = normalizeKeyName(event);
      if (!key) return;
      const active = event.type === "keydown";
      if (active) {
        pressed.add(key);
      } else {
        pressed.delete(key);
      }
      queueInputSignal("$input.key." + key, active);
    }

    function onBlur() {
      for (const key of Array.from(pressed)) {
        queueInputSignal("$input.key." + key, false);
      }
      pressed.clear();
    }

    return bindInputProviderListeners([
      [document, "keydown", onKey],
      [document, "keyup", onKey],
      [window, "blur", onBlur],
    ]);
  }

  function createPointerInputProvider() {
    const state = { lastX: null, lastY: null };

    function publishPointer(event) {
      publishPointerSignals(resolvePointerSample(event, state), event);
    }

    function onBlur() {
      resetPointerSignals();
    }

    return bindInputProviderListeners([
      [document, "pointermove", publishPointer],
      [document, "pointerdown", publishPointer],
      [document, "pointerup", publishPointer],
      [window, "blur", onBlur],
    ]);
  }

  function createGamepadInputProvider() {
    let active = true;
    let frameHandle = 0;

    function pollGamepad() {
      if (!active) return;
      const navigatorRef = window.navigator;
      if (navigatorRef && typeof navigatorRef.getGamepads === "function") {
        const pads = navigatorRef.getGamepads() || [];
        const pad = pads[0];
        if (pad) {
          publishGamepadSignals(pad);
        } else {
          queueInputSignal("$input.gamepad0.connected", false);
        }
      }
      frameHandle = engineFrame(pollGamepad);
    }

    frameHandle = engineFrame(pollGamepad);

    return {
      dispose() {
        active = false;
        if (frameHandle) {
          cancelEngineFrame(frameHandle);
          frameHandle = 0;
        }
      },
    };
  }

  function createTextInputProvider(entry) {
    var inputEl = null;
    var mount = null;
    var unsubCursorRect = null;
    var unsubClipboard = null;
    var viewportListener = null;

    var mountID = entry && (entry.mountId || entry.id);
    mount = mountID ? document.getElementById(mountID) : null;
    if (!mount) {
      mount = document.body;
    }

    inputEl = document.createElement("div");
    inputEl.contentEditable = "true";
    inputEl.setAttribute("role", "textbox");
    inputEl.setAttribute("aria-multiline", "true");
    inputEl.style.cssText = "position:absolute;opacity:0;width:1px;height:1em;overflow:hidden;white-space:pre;pointer-events:none;z-index:-1";
    if (!mount.style.position || mount.style.position === "static") {
      mount.style.position = "relative";
    }
    mount.appendChild(inputEl);

    function focusInput(e) {
      if (e.target !== inputEl) inputEl.focus();
    }

    mount.addEventListener("mousedown", focusInput);
    mount.addEventListener("touchstart", focusInput);

    inputEl.addEventListener("beforeinput", function(e) {
      var type = e.inputType;
      if (type === "insertText" || type === "insertReplacementText") {
        e.preventDefault();
        if (e.data) queueInputSignal("$input.text.inserted", e.data);
      } else if (type === "insertFromPaste") {
        e.preventDefault();
        var text = e.dataTransfer ? e.dataTransfer.getData("text/plain") : "";
        if (text) queueInputSignal("$input.clipboard.paste", text);
      } else if (type === "deleteContentBackward") {
        e.preventDefault();
        queueInputSignal("$input.command", "delete_backward");
      } else if (type === "deleteContentForward") {
        e.preventDefault();
        queueInputSignal("$input.command", "delete_forward");
      } else if (type === "insertLineBreak" || type === "insertParagraph") {
        e.preventDefault();
        queueInputSignal("$input.command", "newline");
      }
      requestAnimationFrame(function() { if (inputEl) inputEl.textContent = ""; });
    });

    inputEl.addEventListener("compositionstart", function() {
      queueInputSignal("$input.text.composition_active", true);
    });
    inputEl.addEventListener("compositionupdate", function(e) {
      queueInputSignal("$input.text.composing", e.data || "");
    });
    inputEl.addEventListener("compositionend", function(e) {
      queueInputSignal("$input.text.composition_active", false);
      queueInputSignal("$input.text.composing", "");
      if (e.data) queueInputSignal("$input.text.inserted", e.data);
    });

    inputEl.addEventListener("keydown", function(e) {
      if (e.isComposing) return;
      var mod = e.metaKey || e.ctrlKey;
      var shift = e.shiftKey;
      var command = null;

      switch (e.key) {
        case "ArrowUp":    command = shift ? "select_up" : "move_up"; break;
        case "ArrowDown":  command = shift ? "select_down" : "move_down"; break;
        case "ArrowLeft":  command = shift ? "select_left" : "move_left"; break;
        case "ArrowRight": command = shift ? "select_right" : "move_right"; break;
        case "Home":       command = shift ? "select_line_start" : "move_line_start"; break;
        case "End":        command = shift ? "select_line_end" : "move_line_end"; break;
        case "Tab":        command = shift ? "dedent" : "indent"; break;
        case "Escape":     command = "escape"; break;
      }

      if (!command && mod) {
        switch (e.key.toLowerCase()) {
          case "z": command = shift ? "redo" : "undo"; break;
          case "a": command = "select_all"; break;
          case "s": command = "save"; break;
          case "b": command = "bold"; break;
          case "i": command = "italic"; break;
        }
      }

      if (command) {
        e.preventDefault();
        queueInputSignal("$input.command", command);
      }
    });

    mount.addEventListener("dragover", function(e) { e.preventDefault(); });
    mount.addEventListener("drop", function(e) {
      e.preventDefault();
      var files = e.dataTransfer ? e.dataTransfer.files : null;
      if (files && files.length > 0) {
        var file = files[0];
        if (file.type.startsWith("image/")) {
          var reader = new FileReader();
          reader.onload = function() {
            queueInputSignal("$editor.file_drop", JSON.stringify({
              name: file.name, type: file.type, size: file.size, data: reader.result
            }));
          };
          reader.readAsDataURL(file);
        }
      }
    });

    if (window.visualViewport) {
      viewportListener = function() {
        var kh = window.innerHeight - window.visualViewport.height;
        queueInputSignal("$input.keyboard_height", Math.max(0, kh));
      };
      window.visualViewport.addEventListener("resize", viewportListener, { passive: true });
    }

    unsubCursorRect = gosxSubscribeSharedSignal("$editor.cursor_rect", function(rect) {
      if (!inputEl) return;
      var r = typeof rect === "string" ? JSON.parse(rect) : rect;
      if (r) {
        inputEl.style.left = (r.x || 0) + "px";
        inputEl.style.top = (r.y || 0) + "px";
        inputEl.style.height = (r.height || 20) + "px";
      }
    });

    unsubClipboard = gosxSubscribeSharedSignal("$editor.clipboard_content", function(text) {
      if (!inputEl || !text) return;
      inputEl.textContent = text;
      var range = document.createRange();
      range.selectNodeContents(inputEl);
      var sel = window.getSelection();
      sel.removeAllRanges();
      sel.addRange(range);
    });

    inputEl.focus();

    return {
      dispose: function() {
        if (unsubCursorRect) { unsubCursorRect(); unsubCursorRect = null; }
        if (unsubClipboard) { unsubClipboard(); unsubClipboard = null; }
        if (viewportListener && window.visualViewport) {
          window.visualViewport.removeEventListener("resize", viewportListener);
          viewportListener = null;
        }
        if (mount) {
          mount.removeEventListener("mousedown", focusInput);
          mount.removeEventListener("touchstart", focusInput);
        }
        if (inputEl && inputEl.parentNode) {
          inputEl.parentNode.removeChild(inputEl);
        }
        inputEl = null;
        mount = null;
      },
    };
  }

  function bindInputProviderListeners(bindings) {
    for (const binding of bindings) {
      binding[0].addEventListener(binding[1], binding[2]);
    }
    return {
      dispose() {
        for (const binding of bindings) {
          binding[0].removeEventListener(binding[1], binding[2]);
        }
      },
    };
  }

  function normalizeKeyName(event) {
    const raw = event && (event.key || event.code);
    if (!raw) return "";
    return String(raw).trim().toLowerCase();
  }

  function resolvePointerSample(event, state) {
    const previousX = state.lastX == null ? 0 : state.lastX;
    const previousY = state.lastY == null ? 0 : state.lastY;
    const x = sceneNumber(event && event.clientX, previousX);
    const y = sceneNumber(event && event.clientY, previousY);
    const sample = {
      x,
      y,
      deltaX: sceneNumber(event && event.movementX, state.lastX == null ? 0 : x - previousX),
      deltaY: sceneNumber(event && event.movementY, state.lastY == null ? 0 : y - previousY),
      buttons: event && typeof event.buttons !== "undefined" ? sceneNumber(event.buttons, 0) : null,
      button: event && typeof event.button === "number" ? event.button : null,
      active: event ? event.type !== "pointerup" : false,
    };
    state.lastX = x;
    state.lastY = y;
    return sample;
  }

  function publishPointerSignals(sample, event) {
    queueInputSignal("$input.pointer.x", sample.x);
    queueInputSignal("$input.pointer.y", sample.y);
    queueInputSignal("$input.pointer.deltaX", sample.deltaX);
    queueInputSignal("$input.pointer.deltaY", sample.deltaY);
    if (sample.buttons != null) {
      queueInputSignal("$input.pointer.buttons", sample.buttons);
    }
    if (sample.button != null) {
      queueInputSignal("$input.pointer.button" + sample.button, sample.active);
    }
  }

  function resetPointerSignals() {
    queueInputSignal("$input.pointer.deltaX", 0);
    queueInputSignal("$input.pointer.deltaY", 0);
    queueInputSignal("$input.pointer.buttons", 0);
  }

  function publishGamepadSignals(pad) {
    const axes = Array.isArray(pad.axes) ? pad.axes : [];
    queueInputSignal("$input.gamepad0.connected", true);
    queueInputSignal("$input.gamepad0.leftX", sceneNumber(axes[0], 0));
    queueInputSignal("$input.gamepad0.leftY", sceneNumber(axes[1], 0));
    queueInputSignal("$input.gamepad0.rightX", sceneNumber(axes[2], 0));
    queueInputSignal("$input.gamepad0.rightY", sceneNumber(axes[3], 0));
    queueInputSignal("$input.gamepad0.buttonA", gamepadButtonPressed(pad, 0));
    queueInputSignal("$input.gamepad0.buttonB", gamepadButtonPressed(pad, 1));
  }

  function gamepadButtonPressed(pad, index) {
    return Boolean(pad && pad.buttons && pad.buttons[index] && pad.buttons[index].pressed);
  }

  function sceneNumber(value, fallback) {
    const num = Number(value);
    return Number.isFinite(num) ? num : fallback;
  }

  function sceneSegmentResolution(value) {
    const segments = Math.round(sceneNumber(value, 12));
    return Math.max(6, Math.min(24, segments));
  }

  function sceneBool(value, fallback) {
    if (typeof value === "boolean") return value;
    if (typeof value === "string") {
      const lowered = value.trim().toLowerCase();
      if (lowered === "true") return true;
      if (lowered === "false") return false;
    }
    return fallback;
  }

  function defaultSceneObjects() {
    return [
      {
        kind: "cube",
        size: 1.8,
        x: -1.1,
        y: 0.3,
        z: 0,
        color: "#8de1ff",
        spinX: 0.42,
        spinY: 0.74,
        spinZ: 0.16,
      },
      {
        kind: "cube",
        size: 1.1,
        x: 1.6,
        y: -0.7,
        z: 1.4,
        color: "#ffd48f",
        spinX: -0.24,
        spinY: 0.48,
        spinZ: 0.12,
      },
    ];
  }

  function rawSceneObjects(props) {
    const scene = sceneProps(props);
    return sceneObjectList(scene && scene.objects) || sceneObjectList(props && props.objects) || defaultSceneObjects();
  }

  function rawSceneLabels(props) {
    const scene = sceneProps(props);
    if (scene && Array.isArray(scene.labels)) {
      return scene.labels;
    }
    return props && Array.isArray(props.labels) ? props.labels : [];
  }

  function rawSceneSprites(props) {
    const scene = sceneProps(props);
    if (scene && Array.isArray(scene.sprites)) {
      return scene.sprites;
    }
    return props && Array.isArray(props.sprites) ? props.sprites : [];
  }

  function rawSceneLights(props) {
    const scene = sceneProps(props);
    if (scene && Array.isArray(scene.lights)) {
      return scene.lights;
    }
    return props && Array.isArray(props.lights) ? props.lights : [];
  }

  function rawSceneEnvironment(props) {
    const scene = sceneProps(props);
    if (scene && scene.environment && typeof scene.environment === "object") {
      return scene.environment;
    }
    return props && props.environment && typeof props.environment === "object" ? props.environment : null;
  }

  function rawSceneModels(props) {
    const scene = sceneProps(props);
    if (scene && Array.isArray(scene.models)) {
      return scene.models;
    }
    return props && Array.isArray(props.models) ? props.models : [];
  }

  function rawScenePoints(props) {
    const scene = sceneProps(props);
    if (scene && Array.isArray(scene.points)) {
      return scene.points;
    }
    return props && Array.isArray(props.points) ? props.points : [];
  }

  function rawSceneInstancedMeshes(props) {
    const scene = sceneProps(props);
    if (scene && Array.isArray(scene.instancedMeshes)) {
      return scene.instancedMeshes;
    }
    return props && Array.isArray(props.instancedMeshes) ? props.instancedMeshes : [];
  }

  function rawSceneComputeParticles(props) {
    const scene = sceneProps(props);
    if (scene && Array.isArray(scene.computeParticles)) {
      return scene.computeParticles;
    }
    return props && Array.isArray(props.computeParticles) ? props.computeParticles : [];
  }

  function sceneProps(props) {
    return props && props.scene && typeof props.scene === "object" ? props.scene : null;
  }

  function sceneObjectList(value) {
    return Array.isArray(value) && value.length > 0 ? value : null;
  }

  function sceneCloneData(value) {
    if (Array.isArray(value)) {
      return value.map(sceneCloneData);
    }
    if (typeof ArrayBuffer !== "undefined" && ArrayBuffer.isView && ArrayBuffer.isView(value)) {
      return typeof value.slice === "function" ? value.slice() : value;
    }
    if (!value || typeof value !== "object") {
      return value;
    }
    const clone = {};
    const keys = Object.keys(value);
    for (let i = 0; i < keys.length; i += 1) {
      const key = keys[i];
      clone[key] = sceneCloneData(value[key]);
    }
    return clone;
  }

  function sceneIsPlainObject(value) {
    return Boolean(value) && typeof value === "object" && !Array.isArray(value) && !(typeof ArrayBuffer !== "undefined" && ArrayBuffer.isView && ArrayBuffer.isView(value));
  }

  function sceneTransitionMetadataKey(key) {
    return key === "transition" || key === "inState" || key === "outState" || key === "live" || (typeof key === "string" && key.charAt(0) === "_");
  }

  function normalizeSceneEasing(value) {
    switch (String(value || "").trim().toLowerCase()) {
      case "linear":
        return "linear";
      case "ease-in":
        return "ease-in";
      case "ease-out":
        return "ease-out";
      case "ease-in-out":
        return "ease-in-out";
      default:
        return "";
    }
  }

  function normalizeSceneTransitionTiming(raw, fallback) {
    const base = sceneIsPlainObject(fallback) ? fallback : {};
    const source = sceneIsPlainObject(raw) ? raw : {};
    return {
      duration: Math.max(0, Math.round(sceneNumber(source.duration, sceneNumber(base.duration, 0)))),
      easing: normalizeSceneEasing(source.easing || base.easing),
    };
  }

  function normalizeSceneTransition(raw, fallback) {
    const base = sceneIsPlainObject(fallback) ? fallback : {};
    const source = sceneIsPlainObject(raw) ? raw : {};
    return {
      in: normalizeSceneTransitionTiming(source.in, base.in),
      out: normalizeSceneTransitionTiming(source.out, base.out),
      update: normalizeSceneTransitionTiming(source.update, base.update),
    };
  }

  function sceneNormalizeLive(value, fallback) {
    const source = Array.isArray(value) ? value : (Array.isArray(fallback) ? fallback : []);
    if (!source.length) {
      return [];
    }
    const seen = new Set();
    const out = [];
    for (let i = 0; i < source.length; i += 1) {
      const next = typeof source[i] === "string" ? source[i].trim() : "";
      if (!next || seen.has(next)) {
        continue;
      }
      seen.add(next);
      out.push(next);
    }
    return out;
  }

  function sceneNormalizeLifecycle(item, fallback) {
    const current = sceneIsPlainObject(fallback) ? fallback : {};
    const source = sceneIsPlainObject(item) ? item : {};
    const hasInState = Object.prototype.hasOwnProperty.call(source, "inState");
    const hasOutState = Object.prototype.hasOwnProperty.call(source, "outState");
    const hasLive = Object.prototype.hasOwnProperty.call(source, "live");
    return {
      transition: normalizeSceneTransition(source.transition, current._transition),
      inState: sceneCloneData(hasInState ? source.inState : current._inState),
      outState: sceneCloneData(hasOutState ? source.outState : current._outState),
      live: sceneNormalizeLive(hasLive ? source.live : undefined, current._live),
    };
  }

  function sceneLinePoint(value) {
    if (Array.isArray(value)) {
      return {
        x: sceneNumber(value[0], 0),
        y: sceneNumber(value[1], 0),
        z: sceneNumber(value[2], 0),
      };
    }
    const item = value && typeof value === "object" ? value : {};
    return {
      x: sceneNumber(item.x, 0),
      y: sceneNumber(item.y, 0),
      z: sceneNumber(item.z, 0),
    };
  }

  function sceneLinePoints(value) {
    const list = Array.isArray(value) ? value : [];
    return list.map(sceneLinePoint);
  }

  function sceneLineSegmentValue(value) {
    function sceneLineIndex(entry) {
      const index = Math.floor(sceneNumber(entry, -1));
      return Number.isFinite(index) ? index : -1;
    }
    if (Array.isArray(value)) {
      return [sceneLineIndex(value[0]), sceneLineIndex(value[1])];
    }
    const item = value && typeof value === "object" ? value : {};
    return [
      sceneLineIndex(item.from !== undefined ? item.from : item.a),
      sceneLineIndex(item.to !== undefined ? item.to : item.b),
    ];
  }

  function sceneLineSegments(value, pointCount) {
    const list = Array.isArray(value) ? value : [];
    const out = [];
    for (const item of list) {
      const pair = sceneLineSegmentValue(item);
      if (!Number.isFinite(pair[0]) || !Number.isFinite(pair[1])) {
        continue;
      }
      if (pair[0] < 0 || pair[1] < 0 || pair[0] === pair[1]) {
        continue;
      }
      if (pair[0] >= pointCount || pair[1] >= pointCount) {
        continue;
      }
      out.push(pair);
    }
    if (out.length === 0 && pointCount > 1) {
      for (let index = 0; index + 1 < pointCount; index += 1) {
        out.push([index, index + 1]);
      }
    }
    return out;
  }

  function sceneNormalizeMeshFloatArray(value, tupleSize) {
    const typed = sceneTypedFloatArray(value);
    const safeTupleSize = Math.max(1, Math.floor(sceneNumber(tupleSize, 1)));
    const count = Math.floor(typed.length / safeTupleSize);
    if (!count) {
      return new Float32Array(0);
    }
    if (typed.length === count * safeTupleSize) {
      return typed;
    }
    return typed.slice(0, count * safeTupleSize);
  }

  function sceneNormalizeMeshVertexData(value) {
    const item = value && typeof value === "object" ? value : {};
    const positions = sceneNormalizeMeshFloatArray(item.positions, 3);
    if (!positions.length) {
      return null;
    }
    const inferredCount = Math.floor(positions.length / 3);
    const count = Math.max(0, Math.min(
      inferredCount,
      Math.floor(sceneNumber(item.count, inferredCount)),
    ));
    if (!count) {
      return null;
    }
    const normals = sceneNormalizeMeshFloatArray(item.normals, 3);
    const uvs = sceneNormalizeMeshFloatArray(item.uvs, 2);
    const tangents = sceneNormalizeMeshFloatArray(item.tangents, 4);
    const joints = sceneNormalizeMeshFloatArray(item.joints, 4);
    const weights = sceneNormalizeMeshFloatArray(item.weights, 4);
    return {
      positions: count * 3 === positions.length ? positions : positions.slice(0, count * 3),
      normals: normals.length >= count * 3 ? normals.slice(0, count * 3) : new Float32Array(0),
      uvs: uvs.length >= count * 2 ? uvs.slice(0, count * 2) : new Float32Array(0),
      tangents: tangents.length >= count * 4 ? tangents.slice(0, count * 4) : new Float32Array(0),
      joints: joints.length >= count * 4 ? joints.slice(0, count * 4) : new Float32Array(0),
      weights: weights.length >= count * 4 ? weights.slice(0, count * 4) : new Float32Array(0),
      count,
    };
  }

  function sceneLineGeometryMetrics(points) {
    if (!Array.isArray(points) || points.length === 0) {
      return null;
    }
    let minX = points[0].x;
    let minY = points[0].y;
    let minZ = points[0].z;
    let maxX = points[0].x;
    let maxY = points[0].y;
    let maxZ = points[0].z;
    for (let i = 1; i < points.length; i += 1) {
      const point = points[i];
      minX = Math.min(minX, point.x);
      minY = Math.min(minY, point.y);
      minZ = Math.min(minZ, point.z);
      maxX = Math.max(maxX, point.x);
      maxY = Math.max(maxY, point.y);
      maxZ = Math.max(maxZ, point.z);
    }
    return {
      width: Math.max(0.0001, maxX - minX),
      height: Math.max(0.0001, maxY - minY),
      depth: Math.max(0.0001, maxZ - minZ),
      radius: Math.max(0.0001, Math.max(maxX - minX, maxY - minY, maxZ - minZ) / 2),
    };
  }

  function normalizeSceneObject(object, index, fallback) {
    const current = sceneIsPlainObject(fallback) ? fallback : {};
    const item = sceneIsPlainObject(object) ? object : {};
    const scaleSource = sceneIsPlainObject(item.scale) ? item.scale : (sceneIsPlainObject(current.scale) ? current.scale : null);
    const vertices = sceneNormalizeMeshVertexData(item.vertices);
    const kind = normalizeSceneKind(item.kind || current.kind);
    const size = sceneNumber(item.size, sceneNumber(current.size, 1.2));
    const points = kind === "lines"
      ? sceneLinePoints(Object.prototype.hasOwnProperty.call(item, "points") ? item.points : current.points)
      : [];
    const lineMetrics = kind === "lines" ? sceneLineGeometryMetrics(points) : null;
    const materialKind = normalizeSceneMaterialKind(sceneObjectMaterialKindValue(item) || current.materialKind);
    const materialColor = sceneObjectMaterialHasValue(item, "color") ? sceneObjectMaterialValue(item, "color") : current.color;
    const textureValue = sceneObjectMaterialHasValue(item, "texture") ? sceneObjectMaterialValue(item, "texture") : current.texture;
    const texture = typeof textureValue === "string" ? textureValue.trim() : "";
    const opacity = clamp01(sceneNumber(sceneObjectMaterialValue(item, "opacity"), sceneNumber(current.opacity, sceneDefaultMaterialOpacity(materialKind))));
    const blendMode = normalizeSceneMaterialBlendMode(
      sceneObjectBlendModeHasValue(item) ? sceneObjectBlendModeValue(item) : current.blendMode,
      materialKind,
      opacity,
    );
    const lifecycle = sceneNormalizeLifecycle(item, current);
    const normalized = {
      id: item.id || current.id || ("scene-object-" + index),
      kind,
      size,
      width: sceneNumber(item.width, sceneNumber(current.width, lineMetrics ? lineMetrics.width : size)),
      height: sceneNumber(item.height, sceneNumber(current.height, lineMetrics ? lineMetrics.height : size)),
      depth: sceneNumber(item.depth, sceneNumber(current.depth, kind === "plane" ? sceneNumber(item.height, size) : (lineMetrics ? lineMetrics.depth : size))),
      radius: sceneNumber(item.radius, sceneNumber(current.radius, lineMetrics ? lineMetrics.radius : (size / 2))),
      segments: sceneSegmentResolution(Object.prototype.hasOwnProperty.call(item, "segments") ? item.segments : current.segments),
      points,
      lineSegments: kind === "lines" ? sceneLineSegments(Array.isArray(item.lineSegments) ? item.lineSegments : (Array.isArray(current.lineSegments) ? current.lineSegments : item.segments), points.length) : [],
      vertices: vertices || current.vertices || null,
      x: sceneNumber(item.x, sceneNumber(current.x, 0)),
      y: sceneNumber(item.y, sceneNumber(current.y, 0)),
      z: sceneNumber(item.z, sceneNumber(current.z, 0)),
      materialKind,
      color: typeof materialColor === "string" && materialColor ? materialColor : (typeof current.color === "string" && current.color ? current.color : "#8de1ff"),
      texture,
      opacity: clamp01(sceneNumber(sceneObjectMaterialValue(item, "opacity"), sceneNumber(current.opacity, sceneDefaultMaterialOpacity(materialKind)))),
      emissive: clamp01(sceneNumber(sceneObjectMaterialValue(item, "emissive"), sceneNumber(current.emissive, sceneDefaultMaterialEmissive(materialKind)))),
      blendMode,
      renderPass: normalizeSceneMaterialRenderPass(
        sceneObjectMaterialHasValue(item, "renderPass") ? sceneObjectMaterialValue(item, "renderPass") : current.renderPass,
        blendMode,
        opacity,
      ),
      wireframe: sceneBool(
        sceneObjectMaterialHasValue(item, "wireframe") ? sceneObjectMaterialValue(item, "wireframe") : current.wireframe,
        texture === "",
      ),
      pickable: Object.prototype.hasOwnProperty.call(item, "pickable") ? sceneBool(item.pickable, false) : current.pickable,
      rotationX: sceneNumber(item.rotationX, sceneNumber(current.rotationX, 0)),
      rotationY: sceneNumber(item.rotationY, sceneNumber(current.rotationY, 0)),
      rotationZ: sceneNumber(item.rotationZ, sceneNumber(current.rotationZ, 0)),
      scaleX: sceneNumber(item.scaleX, sceneNumber(scaleSource ? scaleSource.x : undefined, sceneNumber(current.scaleX, 1))),
      scaleY: sceneNumber(item.scaleY, sceneNumber(scaleSource ? scaleSource.y : undefined, sceneNumber(current.scaleY, 1))),
      scaleZ: sceneNumber(item.scaleZ, sceneNumber(scaleSource ? scaleSource.z : undefined, sceneNumber(current.scaleZ, 1))),
      spinX: sceneNumber(item.spinX, sceneNumber(current.spinX, 0)),
      spinY: sceneNumber(item.spinY, sceneNumber(current.spinY, 0)),
      spinZ: sceneNumber(item.spinZ, sceneNumber(current.spinZ, 0)),
      shiftX: sceneNumber(item.shiftX, sceneNumber(current.shiftX, 0)),
      shiftY: sceneNumber(item.shiftY, sceneNumber(current.shiftY, 0)),
      shiftZ: sceneNumber(item.shiftZ, sceneNumber(current.shiftZ, 0)),
      driftSpeed: sceneNumber(item.driftSpeed, sceneNumber(current.driftSpeed, 0)),
      driftPhase: sceneNumber(item.driftPhase, sceneNumber(current.driftPhase, 0)),
      lineWidth: sceneNumber(item.lineWidth, sceneNumber(current.lineWidth, 0)),
      viewCulled: sceneBool(Object.prototype.hasOwnProperty.call(item, "viewCulled") ? item.viewCulled : current.viewCulled, false),
      castShadow: sceneBool(Object.prototype.hasOwnProperty.call(item, "castShadow") ? item.castShadow : current.castShadow, false),
      receiveShadow: sceneBool(Object.prototype.hasOwnProperty.call(item, "receiveShadow") ? item.receiveShadow : current.receiveShadow, false),
      doubleSided: sceneBool(Object.prototype.hasOwnProperty.call(item, "doubleSided") ? item.doubleSided : current.doubleSided, false),
      depthWrite: Object.prototype.hasOwnProperty.call(item, "depthWrite") ? sceneBool(item.depthWrite, true) : current.depthWrite,
      skin: item.skin && typeof item.skin === "object" ? item.skin : (current.skin && typeof current.skin === "object" ? current.skin : null),
      _transition: lifecycle.transition,
      _inState: lifecycle.inState,
      _outState: lifecycle.outState,
      _live: lifecycle.live,
    };
    normalized.static = sceneBool(
      Object.prototype.hasOwnProperty.call(item, "static") ? item.static : current.static,
      !sceneObjectAnimated(normalized),
    );
    return normalized;
  }

  function normalizeSceneLightKind(value) {
    const kind = typeof value === "string" ? value.trim().toLowerCase() : "";
    switch (kind) {
      case "ambient":
        return "ambient";
      case "directional":
      case "sun":
        return "directional";
      case "point":
        return "point";
      default:
        return "";
    }
  }

  function sceneDefaultLightIntensity(kind) {
    switch (normalizeSceneLightKind(kind)) {
      case "ambient":
        return 0.28;
      case "directional":
        return 1;
      case "point":
        return 1.1;
      default:
        return 1;
    }
  }

  function normalizeSceneLight(light, index, fallback) {
    const current = sceneIsPlainObject(fallback) ? fallback : {};
    const item = sceneIsPlainObject(light) ? light : {};
    const kind = normalizeSceneLightKind(item.kind || item.lightKind || current.kind);
    if (!kind) {
      return null;
    }
    const lifecycle = sceneNormalizeLifecycle(item, current);
    const normalized = {
      id: (typeof item.id === "string" && item.id) || current.id || ("scene-light-" + index),
      kind,
      color: typeof item.color === "string" && item.color ? item.color : (typeof current.color === "string" && current.color ? current.color : "#f3fbff"),
      groundColor: typeof item.groundColor === "string" && item.groundColor ? item.groundColor : (typeof current.groundColor === "string" ? current.groundColor : ""),
      intensity: Math.max(0, Math.min(6, sceneNumber(item.intensity, sceneNumber(current.intensity, sceneDefaultLightIntensity(kind))))),
      x: sceneNumber(item.x, sceneNumber(current.x, 0)),
      y: sceneNumber(item.y, sceneNumber(current.y, 0)),
      z: sceneNumber(item.z, sceneNumber(current.z, 0)),
      directionX: sceneNumber(item.directionX, sceneNumber(current.directionX, 0)),
      directionY: sceneNumber(item.directionY, sceneNumber(current.directionY, 0)),
      directionZ: sceneNumber(item.directionZ, sceneNumber(current.directionZ, 0)),
      angle: Math.max(0, Math.min(Math.PI, sceneNumber(item.angle, sceneNumber(current.angle, 0)))),
      penumbra: sceneClamp(sceneNumber(item.penumbra, sceneNumber(current.penumbra, 0)), 0, 1),
      range: Math.max(0, Math.min(256, sceneNumber(item.range, sceneNumber(current.range, kind === "point" ? 6.5 : 0)))),
      decay: Math.max(0.1, Math.min(8, sceneNumber(item.decay, sceneNumber(current.decay, kind === "point" ? 1.35 : 1)))),
      castShadow: sceneBool(Object.prototype.hasOwnProperty.call(item, "castShadow") ? item.castShadow : current.castShadow, false),
      shadowBias: sceneNumber(item.shadowBias, sceneNumber(current.shadowBias, 0)),
      shadowSize: Math.max(0, Math.floor(sceneNumber(item.shadowSize, sceneNumber(current.shadowSize, 0)))),
      _transition: lifecycle.transition,
      _inState: lifecycle.inState,
      _outState: lifecycle.outState,
      _live: lifecycle.live,
    };
    if (normalized.kind === "directional" && normalized.directionX === 0 && normalized.directionY === 0 && normalized.directionZ === 0) {
      normalized.directionX = 0.35;
      normalized.directionY = -1;
      normalized.directionZ = -0.4;
    }
    if (typeof hashLightContent === "function") {
      normalized._lightHash = hashLightContent(normalized);
    }
    return normalized;
  }

  function normalizeSceneLabel(label, index, fallback) {
    const current = sceneIsPlainObject(fallback) ? fallback : {};
    const item = sceneIsPlainObject(label) ? label : {};
    const lifecycle = sceneNormalizeLifecycle(item, current);
    return {
      id: item.id || current.id || ("scene-label-" + index),
      text: typeof item.text === "string" ? item.text : (typeof current.text === "string" ? current.text : ""),
      className: sceneLabelClassName(item) || sceneLabelClassName(current),
      x: sceneNumber(item.x, sceneNumber(current.x, 0)),
      y: sceneNumber(item.y, sceneNumber(current.y, 0)),
      z: sceneNumber(item.z, sceneNumber(current.z, 0)),
      priority: sceneNumber(item.priority, sceneNumber(current.priority, 0)),
      shiftX: sceneNumber(item.shiftX, sceneNumber(current.shiftX, 0)),
      shiftY: sceneNumber(item.shiftY, sceneNumber(current.shiftY, 0)),
      shiftZ: sceneNumber(item.shiftZ, sceneNumber(current.shiftZ, 0)),
      driftSpeed: sceneNumber(item.driftSpeed, sceneNumber(current.driftSpeed, 0)),
      driftPhase: sceneNumber(item.driftPhase, sceneNumber(current.driftPhase, 0)),
      maxWidth: Math.max(48, sceneNumber(item.maxWidth, sceneNumber(current.maxWidth, 180))),
      maxLines: Math.max(0, Math.floor(sceneNumber(item.maxLines, sceneNumber(current.maxLines, 0)))),
      overflow: normalizeTextLayoutOverflow(item.overflow || current.overflow),
      font: typeof item.font === "string" && item.font ? item.font : (typeof current.font === "string" && current.font ? current.font : '600 13px "IBM Plex Sans", "Segoe UI", sans-serif'),
      lineHeight: Math.max(12, sceneNumber(item.lineHeight, sceneNumber(current.lineHeight, 18))),
      color: typeof item.color === "string" && item.color ? item.color : (typeof current.color === "string" && current.color ? current.color : "#ecf7ff"),
      background: typeof item.background === "string" && item.background ? item.background : (typeof current.background === "string" && current.background ? current.background : "rgba(8, 21, 31, 0.82)"),
      borderColor: typeof item.borderColor === "string" && item.borderColor ? item.borderColor : (typeof current.borderColor === "string" && current.borderColor ? current.borderColor : "rgba(141, 225, 255, 0.24)"),
      offsetX: sceneNumber(item.offsetX, sceneNumber(current.offsetX, 0)),
      offsetY: sceneNumber(item.offsetY, sceneNumber(current.offsetY, -14)),
      anchorX: Math.max(0, Math.min(1, sceneNumber(item.anchorX, sceneNumber(current.anchorX, 0.5)))),
      anchorY: Math.max(0, Math.min(1, sceneNumber(item.anchorY, sceneNumber(current.anchorY, 1)))),
      collision: normalizeSceneLabelCollision(item.collision || current.collision),
      occlude: sceneBool(Object.prototype.hasOwnProperty.call(item, "occlude") ? item.occlude : current.occlude, false),
      whiteSpace: normalizeSceneLabelWhiteSpace(item.whiteSpace || current.whiteSpace),
      textAlign: normalizeSceneLabelAlign(item.textAlign || current.textAlign),
      _transition: lifecycle.transition,
      _inState: lifecycle.inState,
      _outState: lifecycle.outState,
      _live: lifecycle.live,
    };
  }

  function normalizeSceneSpriteFit(value) {
    const mode = typeof value === "string" ? value.trim().toLowerCase() : "";
    switch (mode) {
      case "cover":
        return "cover";
      case "stretch":
      case "fill":
        return "fill";
      default:
        return "contain";
    }
  }

  function normalizeSceneSprite(sprite, index, fallback) {
    const current = sceneIsPlainObject(fallback) ? fallback : {};
    const item = sceneIsPlainObject(sprite) ? sprite : {};
    const width = Math.max(0.05, sceneNumber(item.width, sceneNumber(current.width, 1.25)));
    const height = Math.max(0.05, sceneNumber(item.height, sceneNumber(current.height, width)));
    const scale = Math.max(0.05, sceneNumber(item.scale, sceneNumber(current.scale, 1)));
    const lifecycle = sceneNormalizeLifecycle(item, current);
    return {
      id: item.id || current.id || ("scene-sprite-" + index),
      src: typeof item.src === "string" ? item.src.trim() : (typeof current.src === "string" ? current.src : ""),
      className: sceneLabelClassName(item) || sceneLabelClassName(current),
      x: sceneNumber(item.x, sceneNumber(current.x, 0)),
      y: sceneNumber(item.y, sceneNumber(current.y, 0)),
      z: sceneNumber(item.z, sceneNumber(current.z, 0)),
      priority: sceneNumber(item.priority, sceneNumber(current.priority, 0)),
      shiftX: sceneNumber(item.shiftX, sceneNumber(current.shiftX, 0)),
      shiftY: sceneNumber(item.shiftY, sceneNumber(current.shiftY, 0)),
      shiftZ: sceneNumber(item.shiftZ, sceneNumber(current.shiftZ, 0)),
      driftSpeed: sceneNumber(item.driftSpeed, sceneNumber(current.driftSpeed, 0)),
      driftPhase: sceneNumber(item.driftPhase, sceneNumber(current.driftPhase, 0)),
      width: width,
      height: height,
      scale: scale,
      opacity: clamp01(sceneNumber(item.opacity, sceneNumber(current.opacity, 1))),
      offsetX: sceneNumber(item.offsetX, sceneNumber(current.offsetX, 0)),
      offsetY: sceneNumber(item.offsetY, sceneNumber(current.offsetY, 0)),
      anchorX: sceneClamp(sceneNumber(item.anchorX, sceneNumber(current.anchorX, 0.5)), 0, 1),
      anchorY: sceneClamp(sceneNumber(item.anchorY, sceneNumber(current.anchorY, 0.5)), 0, 1),
      occlude: sceneBool(Object.prototype.hasOwnProperty.call(item, "occlude") ? item.occlude : current.occlude, false),
      fit: normalizeSceneSpriteFit(item.fit || current.fit),
      _transition: lifecycle.transition,
      _inState: lifecycle.inState,
      _outState: lifecycle.outState,
      _live: lifecycle.live,
    };
  }

  function sceneLabelClassName(item) {
    if (!item || typeof item !== "object") {
      return "";
    }
    if (typeof item.className === "string" && item.className.trim()) {
      return item.className.trim();
    }
    if (typeof item.class === "string" && item.class.trim()) {
      return item.class.trim();
    }
    return "";
  }

  function normalizeSceneKind(value) {
    const kind = typeof value === "string" ? value.trim().toLowerCase() : "";
    switch (kind) {
      case "box":
      case "lines":
      case "plane":
      case "pyramid":
      case "sphere":
      case "gltf-mesh":
        return kind;
      default:
        return "cube";
    }
  }

  function sceneObjects(props) {
    return rawSceneObjects(props).map(function(object, index) {
      return normalizeSceneObject(object, index, null);
    });
  }

  function normalizeSceneModel(item, index) {
    const current = item && typeof item === "object" ? item : {};
    const scaleSource = current.scale && typeof current.scale === "object" ? current.scale : null;
    const hasStatic = Object.prototype.hasOwnProperty.call(current, "static");
    const hasPickable = Object.prototype.hasOwnProperty.call(current, "pickable");
    const override = {};
    const materialKind = sceneObjectMaterialKindValue(current);
    if (materialKind) {
      override.materialKind = normalizeSceneMaterialKind(materialKind);
    }
    if (sceneObjectMaterialHasValue(current, "color")) {
      override.color = sceneObjectMaterialValue(current, "color");
    }
    if (sceneObjectMaterialHasValue(current, "texture")) {
      override.texture = sceneObjectMaterialValue(current, "texture");
    }
    if (sceneObjectMaterialHasValue(current, "opacity")) {
      override.opacity = sceneObjectMaterialValue(current, "opacity");
    }
    if (sceneObjectMaterialHasValue(current, "emissive")) {
      override.emissive = sceneObjectMaterialValue(current, "emissive");
    }
    if (sceneObjectBlendModeHasValue(current)) {
      override.blendMode = sceneObjectBlendModeValue(current);
    }
    if (sceneObjectMaterialHasValue(current, "renderPass")) {
      override.renderPass = sceneObjectMaterialValue(current, "renderPass");
    }
    if (sceneObjectMaterialHasValue(current, "wireframe")) {
      override.wireframe = sceneObjectMaterialValue(current, "wireframe");
    }
    return {
      id: typeof current.id === "string" && current.id.trim() ? current.id.trim() : ("scene-model-" + index),
      src: typeof current.src === "string" && current.src.trim() ? current.src.trim() : "",
      x: sceneNumber(current.x, 0),
      y: sceneNumber(current.y, 0),
      z: sceneNumber(current.z, 0),
      rotationX: sceneNumber(current.rotationX, 0),
      rotationY: sceneNumber(current.rotationY, 0),
      rotationZ: sceneNumber(current.rotationZ, 0),
      scaleX: sceneNumber(current.scaleX, sceneNumber(scaleSource && scaleSource.x, sceneNumber(current.scale, 1))),
      scaleY: sceneNumber(current.scaleY, sceneNumber(scaleSource && scaleSource.y, sceneNumber(current.scale, 1))),
      scaleZ: sceneNumber(current.scaleZ, sceneNumber(scaleSource && scaleSource.z, sceneNumber(current.scale, 1))),
      pickable: hasPickable ? sceneBool(current.pickable, false) : undefined,
      static: hasStatic ? sceneBool(current.static, false) : null,
      materialOverride: Object.keys(override).length > 0 ? override : null,
    };
  }

  function sceneModels(props) {
    return rawSceneModels(props)
      .map(function(model, index) {
        return normalizeSceneModel(model, index);
      })
      .filter(function(model) {
        return Boolean(model && model.src);
      });
  }

  function sceneLights(props) {
    return rawSceneLights(props)
      .map(function(light, index) {
        return normalizeSceneLight(light, index, null);
      })
      .filter(Boolean);
  }

  function normalizeSceneLabelWhiteSpace(value) {
    const mode = typeof value === "string" ? value.trim().toLowerCase() : "";
    switch (mode) {
      case "pre-wrap":
        return "pre-wrap";
      case "pre":
        return "pre";
      default:
        return "normal";
    }
  }

  function normalizeSceneLabelAlign(value) {
    const align = typeof value === "string" ? value.trim().toLowerCase() : "";
    switch (align) {
      case "left":
      case "start":
        return "left";
      case "right":
      case "end":
        return "right";
      default:
        return "center";
    }
  }

  function normalizeSceneLabelCollision(value) {
    const mode = typeof value === "string" ? value.trim().toLowerCase() : "";
    switch (mode) {
      case "allow":
      case "none":
      case "overlap":
        return "allow";
      default:
        return "avoid";
    }
  }

  function sceneLabels(props) {
    const raw = rawSceneLabels(props);
    return raw
      .map(function(label, index) {
        return normalizeSceneLabel(label, index, null);
      })
      .filter(function(label) {
        return label.text.trim() !== "";
      });
  }

  function sceneSprites(props) {
    return rawSceneSprites(props)
      .map(function(sprite, index) {
        return normalizeSceneSprite(sprite, index, null);
      })
      .filter(function(sprite) {
        return sprite.src !== "";
      });
  }

  function normalizeScenePointStyle(value, fallback) {
    const current = typeof fallback === "string" && fallback ? fallback : "square";
    const raw = typeof value === "string" ? value.trim().toLowerCase() : "";
    switch (raw) {
      case "focus":
      case "focused":
      case "focus-star":
        return "focus";
      case "glow":
      case "gas":
      case "cloud":
      case "nebula":
        return "glow";
      case "square":
      case "pixel":
      case "hard":
      case "block":
      case "blocky":
        return "square";
      default:
        return current;
    }
  }

  function scenePointStyleCode(value) {
    const style = normalizeScenePointStyle(value, "square");
    if (style === "focus") return 1;
    if (style === "glow") return 2;
    return 0;
  }

  function normalizeScenePointsEntry(entry, index, fallback) {
    const current = sceneIsPlainObject(fallback) ? fallback : {};
    const item = sceneIsPlainObject(entry) ? entry : {};
    const lifecycle = sceneNormalizeLifecycle(item, current);
    const positions = Array.isArray(item.positions) ? item.positions.slice() : (Array.isArray(current.positions) ? current.positions : []);
    const sizes = Array.isArray(item.sizes) ? item.sizes.slice() : (Array.isArray(current.sizes) ? current.sizes : []);
    const colors = Array.isArray(item.colors) ? item.colors.slice() : (Array.isArray(current.colors) ? current.colors : []);
    const normalized = {
      id: item.id || current.id || ("scene-points-" + index),
      count: Math.max(0, Math.floor(sceneNumber(item.count, sceneNumber(current.count, positions.length >= 3 ? Math.floor(positions.length / 3) : 0)))),
      positions,
      sizes,
      colors,
      color: typeof item.color === "string" && item.color ? item.color : (typeof current.color === "string" ? current.color : "#ffffff"),
      style: normalizeScenePointStyle(item.style, current.style),
      size: Math.max(0, sceneNumber(item.size, sceneNumber(current.size, 1))),
      opacity: clamp01(sceneNumber(item.opacity, sceneNumber(current.opacity, 1))),
      blendMode: normalizeSceneMaterialBlendMode(item.blendMode || current.blendMode, "flat", sceneNumber(item.opacity, sceneNumber(current.opacity, 1))),
      depthWrite: Object.prototype.hasOwnProperty.call(item, "depthWrite") ? sceneBool(item.depthWrite, true) : current.depthWrite,
      attenuation: sceneBool(Object.prototype.hasOwnProperty.call(item, "attenuation") ? item.attenuation : current.attenuation, false),
      x: sceneNumber(item.x, sceneNumber(current.x, 0)),
      y: sceneNumber(item.y, sceneNumber(current.y, 0)),
      z: sceneNumber(item.z, sceneNumber(current.z, 0)),
      rotationX: sceneNumber(item.rotationX, sceneNumber(current.rotationX, 0)),
      rotationY: sceneNumber(item.rotationY, sceneNumber(current.rotationY, 0)),
      rotationZ: sceneNumber(item.rotationZ, sceneNumber(current.rotationZ, 0)),
      spinX: sceneNumber(item.spinX, sceneNumber(current.spinX, 0)),
      spinY: sceneNumber(item.spinY, sceneNumber(current.spinY, 0)),
      spinZ: sceneNumber(item.spinZ, sceneNumber(current.spinZ, 0)),
      _transition: lifecycle.transition,
      _inState: lifecycle.inState,
      _outState: lifecycle.outState,
      _live: lifecycle.live,
    };
    if (positions === current.positions && current._cachedPos) {
      normalized._cachedPos = current._cachedPos;
    }
    if (sizes === current.sizes && current._cachedSizes) {
      normalized._cachedSizes = current._cachedSizes;
    }
    if (colors === current.colors && current._cachedColors) {
      normalized._cachedColors = current._cachedColors;
    }
    if (Array.isArray(item.previewPositions)) {
      normalized.previewPositions = item.previewPositions.slice();
    } else if (Array.isArray(current.previewPositions)) {
      normalized.previewPositions = current.previewPositions;
    }
    if (Array.isArray(item.previewSizes)) {
      normalized.previewSizes = item.previewSizes.slice();
    } else if (Array.isArray(current.previewSizes)) {
      normalized.previewSizes = current.previewSizes;
    }
    return normalized;
  }

  function scenePoints(props) {
    return rawScenePoints(props).map(function(entry, index) {
      return normalizeScenePointsEntry(entry, index, null);
    });
  }

  function normalizeSceneInstancedMeshEntry(entry, index, fallback) {
    const current = sceneIsPlainObject(fallback) ? fallback : {};
    const item = sceneIsPlainObject(entry) ? entry : {};
    const lifecycle = sceneNormalizeLifecycle(item, current);
    return {
      id: item.id || current.id || ("scene-instanced-" + index),
      count: Math.max(0, Math.floor(sceneNumber(item.count, sceneNumber(current.count, 0)))),
      kind: normalizeSceneKind(item.kind || current.kind),
      width: Math.max(0.0001, sceneNumber(item.width, sceneNumber(current.width, sceneNumber(current.size, 1.2)))),
      height: Math.max(0.0001, sceneNumber(item.height, sceneNumber(current.height, sceneNumber(current.size, 1.2)))),
      depth: Math.max(0.0001, sceneNumber(item.depth, sceneNumber(current.depth, sceneNumber(current.size, 1.2)))),
      radius: Math.max(0.0001, sceneNumber(item.radius, sceneNumber(current.radius, sceneNumber(current.size, 0.6)))),
      segments: sceneSegmentResolution(Object.prototype.hasOwnProperty.call(item, "segments") ? item.segments : current.segments),
      materialKind: normalizeSceneMaterialKind(item.materialKind || current.materialKind),
      color: typeof item.color === "string" && item.color ? item.color : (typeof current.color === "string" ? current.color : "#8de1ff"),
      roughness: sceneNumber(item.roughness, sceneNumber(current.roughness, 0)),
      metalness: sceneNumber(item.metalness, sceneNumber(current.metalness, 0)),
      transforms: Array.isArray(item.transforms) ? item.transforms.slice() : (Array.isArray(current.transforms) ? current.transforms : []),
      castShadow: sceneBool(Object.prototype.hasOwnProperty.call(item, "castShadow") ? item.castShadow : current.castShadow, false),
      receiveShadow: sceneBool(Object.prototype.hasOwnProperty.call(item, "receiveShadow") ? item.receiveShadow : current.receiveShadow, false),
      _transition: lifecycle.transition,
      _inState: lifecycle.inState,
      _outState: lifecycle.outState,
      _live: lifecycle.live,
    };
  }

  function sceneInstancedMeshes(props) {
    return rawSceneInstancedMeshes(props).map(function(entry, index) {
      return normalizeSceneInstancedMeshEntry(entry, index, null);
    });
  }

  function normalizeSceneComputeEmitter(raw, fallback) {
    const current = sceneIsPlainObject(fallback) ? fallback : {};
    const item = sceneIsPlainObject(raw) ? raw : {};
    return {
      kind: typeof item.kind === "string" && item.kind ? item.kind : (typeof current.kind === "string" ? current.kind : "point"),
      x: sceneNumber(item.x, sceneNumber(current.x, 0)),
      y: sceneNumber(item.y, sceneNumber(current.y, 0)),
      z: sceneNumber(item.z, sceneNumber(current.z, 0)),
      rotationX: sceneNumber(item.rotationX, sceneNumber(current.rotationX, 0)),
      rotationY: sceneNumber(item.rotationY, sceneNumber(current.rotationY, 0)),
      rotationZ: sceneNumber(item.rotationZ, sceneNumber(current.rotationZ, 0)),
      spinX: sceneNumber(item.spinX, sceneNumber(current.spinX, 0)),
      spinY: sceneNumber(item.spinY, sceneNumber(current.spinY, 0)),
      spinZ: sceneNumber(item.spinZ, sceneNumber(current.spinZ, 0)),
      radius: Math.max(0, sceneNumber(item.radius, sceneNumber(current.radius, 0))),
      rate: Math.max(0, sceneNumber(item.rate, sceneNumber(current.rate, 0))),
      lifetime: Math.max(0.01, sceneNumber(item.lifetime, sceneNumber(current.lifetime, 1))),
      arms: Math.max(0, Math.floor(sceneNumber(item.arms, sceneNumber(current.arms, 0)))),
      wind: sceneNumber(item.wind, sceneNumber(current.wind, 0)),
      scatter: Math.max(0, sceneNumber(item.scatter, sceneNumber(current.scatter, 0))),
    };
  }

  function normalizeSceneComputeForce(raw, index, fallback) {
    const current = sceneIsPlainObject(fallback) ? fallback : {};
    const item = sceneIsPlainObject(raw) ? raw : {};
    return {
      kind: typeof item.kind === "string" && item.kind ? item.kind : (typeof current.kind === "string" ? current.kind : ""),
      strength: sceneNumber(item.strength, sceneNumber(current.strength, 0)),
      x: sceneNumber(item.x, sceneNumber(current.x, 0)),
      y: sceneNumber(item.y, sceneNumber(current.y, 0)),
      z: sceneNumber(item.z, sceneNumber(current.z, 0)),
      frequency: sceneNumber(item.frequency, sceneNumber(current.frequency, 0)),
      id: current.id || ("scene-force-" + index),
    };
  }

  function normalizeSceneComputeMaterial(raw, fallback) {
    const current = sceneIsPlainObject(fallback) ? fallback : {};
    const item = sceneIsPlainObject(raw) ? raw : {};
    return {
      color: typeof item.color === "string" && item.color ? item.color : (typeof current.color === "string" ? current.color : "#ffffff"),
      colorEnd: typeof item.colorEnd === "string" && item.colorEnd ? item.colorEnd : (typeof current.colorEnd === "string" ? current.colorEnd : ""),
      style: normalizeScenePointStyle(item.style, current.style),
      size: Math.max(0, sceneNumber(item.size, sceneNumber(current.size, 1))),
      sizeEnd: Math.max(0, sceneNumber(item.sizeEnd, sceneNumber(current.sizeEnd, sceneNumber(current.size, 1)))),
      opacity: clamp01(sceneNumber(item.opacity, sceneNumber(current.opacity, 1))),
      opacityEnd: clamp01(sceneNumber(item.opacityEnd, sceneNumber(current.opacityEnd, sceneNumber(current.opacity, 1)))),
      blendMode: normalizeSceneMaterialBlendMode(item.blendMode || current.blendMode, "flat", sceneNumber(item.opacity, sceneNumber(current.opacity, 1))),
      attenuation: sceneBool(Object.prototype.hasOwnProperty.call(item, "attenuation") ? item.attenuation : current.attenuation, false),
    };
  }

  function normalizeSceneComputeParticlesEntry(entry, index, fallback) {
    const current = sceneIsPlainObject(fallback) ? fallback : {};
    const item = sceneIsPlainObject(entry) ? entry : {};
    const lifecycle = sceneNormalizeLifecycle(item, current);
    const emitterSource = Object.prototype.hasOwnProperty.call(item, "emitter") ? item.emitter : current.emitter;
    const materialSource = Object.prototype.hasOwnProperty.call(item, "material") ? item.material : current.material;
    const forcesSource = Array.isArray(item.forces) ? item.forces : (Array.isArray(current.forces) ? current.forces : []);
    return {
      id: item.id || current.id || ("scene-particles-" + index),
      count: Math.max(0, Math.floor(sceneNumber(item.count, sceneNumber(current.count, 0)))),
      emitter: normalizeSceneComputeEmitter(emitterSource, current.emitter),
      forces: forcesSource.map(function(force, forceIndex) {
        return normalizeSceneComputeForce(force, forceIndex, Array.isArray(current.forces) ? current.forces[forceIndex] : null);
      }),
      material: normalizeSceneComputeMaterial(materialSource, current.material),
      bounds: Math.max(0, sceneNumber(item.bounds, sceneNumber(current.bounds, 0))),
      _transition: lifecycle.transition,
      _inState: lifecycle.inState,
      _outState: lifecycle.outState,
      _live: lifecycle.live,
    };
  }

  function sceneComputeParticles(props) {
    return rawSceneComputeParticles(props).map(function(entry, index) {
      return normalizeSceneComputeParticlesEntry(entry, index, null);
    });
  }

  function sceneCamera(props) {
    const raw = props && props.camera && typeof props.camera === "object" ? props.camera : {};
    return normalizeSceneCamera(raw, {
      x: 0,
      y: 0,
      z: 6,
      fov: 75,
      near: 0.05,
      far: 128,
    });
  }

  function normalizeSceneCamera(raw, fallback) {
    const base = fallback || {};
    return {
      x: sceneNumber(raw.x, sceneNumber(base.x, 0)),
      y: sceneNumber(raw.y, sceneNumber(base.y, 0)),
      z: sceneNumber(raw.z, sceneNumber(base.z, 6)),
      rotationX: sceneNumber(raw.rotationX, sceneNumber(base.rotationX, 0)),
      rotationY: sceneNumber(raw.rotationY, sceneNumber(base.rotationY, 0)),
      rotationZ: sceneNumber(raw.rotationZ, sceneNumber(base.rotationZ, 0)),
      fov: sceneNumber(raw.fov, sceneNumber(base.fov, 75)),
      near: sceneNumber(raw.near, sceneNumber(base.near, 0.05)),
      far: sceneNumber(raw.far, sceneNumber(base.far, 128)),
    };
  }

  function normalizeSceneEnvironment(raw, fallback) {
    const base = sceneIsPlainObject(fallback) ? fallback : {};
    const source = sceneIsPlainObject(raw) ? raw : {};
    const lifecycle = sceneNormalizeLifecycle(source, base);
    const environment = {
      ambientColor: typeof source.ambientColor === "string" && source.ambientColor ? source.ambientColor : (typeof base.ambientColor === "string" ? base.ambientColor : ""),
      ambientIntensity: Math.max(0, Math.min(4, sceneNumber(source.ambientIntensity, sceneNumber(base.ambientIntensity, 0)))),
      skyColor: typeof source.skyColor === "string" && source.skyColor ? source.skyColor : (typeof base.skyColor === "string" ? base.skyColor : ""),
      skyIntensity: Math.max(0, Math.min(4, sceneNumber(source.skyIntensity, sceneNumber(base.skyIntensity, 0)))),
      groundColor: typeof source.groundColor === "string" && source.groundColor ? source.groundColor : (typeof base.groundColor === "string" ? base.groundColor : ""),
      groundIntensity: Math.max(0, Math.min(4, sceneNumber(source.groundIntensity, sceneNumber(base.groundIntensity, 0)))),
      exposure: Math.max(0.05, Math.min(4, sceneNumber(Object.prototype.hasOwnProperty.call(source, "exposure") ? source.exposure : undefined, sceneNumber(base.exposure, 1) || 1))),
      toneMapping: typeof source.toneMapping === "string" && source.toneMapping ? source.toneMapping : (typeof base.toneMapping === "string" ? base.toneMapping : ""),
      fogColor: typeof source.fogColor === "string" && source.fogColor ? source.fogColor : (typeof base.fogColor === "string" ? base.fogColor : ""),
      fogDensity: Math.max(0, sceneNumber(source.fogDensity, sceneNumber(base.fogDensity, 0))),
      _transition: lifecycle.transition,
      _inState: lifecycle.inState,
      _outState: lifecycle.outState,
      _live: lifecycle.live,
      specified: false,
    };
    environment.specified = Boolean(raw || base.specified) && (
      environment.ambientColor ||
      environment.ambientIntensity !== 0 ||
      environment.skyColor ||
      environment.skyIntensity !== 0 ||
      environment.groundColor ||
      environment.groundIntensity !== 0 ||
      environment.fogColor ||
      environment.fogDensity !== 0 ||
      environment.toneMapping ||
      Object.prototype.hasOwnProperty.call(source, "exposure")
    );
    if (typeof hashEnvironmentContent === "function") {
      environment._envHash = hashEnvironmentContent(environment);
    }
    return environment;
  }

  function sceneResolveLightingEnvironment(environment, hasLights) {
    const base = environment && typeof environment === "object" && Object.prototype.hasOwnProperty.call(environment, "specified")
      ? {
        ambientColor: typeof environment.ambientColor === "string" ? environment.ambientColor : "",
        ambientIntensity: Math.max(0, Math.min(4, sceneNumber(environment.ambientIntensity, 0))),
        skyColor: typeof environment.skyColor === "string" ? environment.skyColor : "",
        skyIntensity: Math.max(0, Math.min(4, sceneNumber(environment.skyIntensity, 0))),
        groundColor: typeof environment.groundColor === "string" ? environment.groundColor : "",
        groundIntensity: Math.max(0, Math.min(4, sceneNumber(environment.groundIntensity, 0))),
        exposure: Math.max(0.05, Math.min(4, sceneNumber(environment.exposure, 1) || 1)),
        toneMapping: typeof environment.toneMapping === "string" ? environment.toneMapping : "",
        fogColor: typeof environment.fogColor === "string" ? environment.fogColor : "",
        fogDensity: Math.max(0, sceneNumber(environment.fogDensity, 0)),
        specified: Boolean(environment.specified),
      }
      : normalizeSceneEnvironment(environment, null);
    if (typeof base._envHash !== "number" && typeof hashEnvironmentContent === "function") {
      base._envHash = hashEnvironmentContent(base);
    }
    if (base.specified || !hasLights) {
      return base;
    }
    return normalizeSceneEnvironment({
      ambientColor: "#f5fbff",
      ambientIntensity: 0.18,
      skyColor: "#d5ebff",
      skyIntensity: 0.12,
      groundColor: "#102030",
      groundIntensity: 0.04,
      exposure: base.exposure,
    }, base);
  }

  function createSceneState(props) {
    if (typeof sceneDecompressProps === "function") {
      sceneDecompressProps(props);
    }
    const state = {
      background: typeof props.background === "string" && props.background ? props.background : "#08151f",
      camera: sceneCamera(props),
      objects: new Map(),
      labels: new Map(),
      sprites: new Map(),
      lights: new Map(),
      points: scenePoints(props),
      instancedMeshes: sceneInstancedMeshes(props),
      computeParticles: sceneComputeParticles(props),
      _transitions: [],
      _scrollCamera: (sceneNumber(props.scrollCameraStart, 0) !== 0 || sceneNumber(props.scrollCameraEnd, 0) !== 0)
        ? { start: sceneNumber(props.scrollCameraStart, 0), end: sceneNumber(props.scrollCameraEnd, 0) }
        : null,
      environment: normalizeSceneEnvironment(rawSceneEnvironment(props), null),
    };
    for (const object of sceneObjects(props)) {
      state.objects.set(object.id, object);
    }
    for (const label of sceneLabels(props)) {
      state.labels.set(label.id, label);
    }
    for (const sprite of sceneSprites(props)) {
      state.sprites.set(sprite.id, sprite);
    }
    for (const light of sceneLights(props)) {
      state.lights.set(light.id, light);
    }
    return state;
  }

  function sceneStateObjects(state) {
    return Array.from(state.objects.values());
  }

  function sceneStateLabels(state) {
    return Array.from(state.labels.values());
  }

  function sceneStateSprites(state) {
    return Array.from(state.sprites.values());
  }

  function sceneStateLights(state) {
    return Array.from(state.lights.values());
  }

  function sceneStateTransitions(state) {
    if (!state || !Array.isArray(state._transitions)) {
      return [];
    }
    return state._transitions;
  }

  function sceneHasActiveTransitions(state) {
    return sceneStateTransitions(state).length > 0;
  }

  function sceneNowMilliseconds() {
    if (typeof window !== "undefined" && window.performance && typeof window.performance.now === "function") {
      return window.performance.now();
    }
    return Date.now();
  }

  function sceneTransitionTimingForPhase(entry, phase) {
    const transition = sceneIsPlainObject(entry && entry._transition) ? entry._transition : null;
    const fallback = transition && phase === "update" ? transition.in : null;
    const timing = transition && sceneIsPlainObject(transition[phase]) ? transition[phase] : null;
    const duration = Math.max(0, Math.round(sceneNumber(timing && timing.duration, sceneNumber(fallback && fallback.duration, 0))));
    const easing = normalizeSceneEasing((timing && timing.easing) || (fallback && fallback.easing));
    return { duration, easing };
  }

  function sceneTransitionEase(easing, t) {
    const clamped = sceneClamp(sceneNumber(t, 0), 0, 1);
    switch (normalizeSceneEasing(easing)) {
      case "ease-in":
        return clamped * clamped;
      case "ease-out":
        return clamped * (2 - clamped);
      case "ease-in-out":
        return clamped < 0.5 ? 2 * clamped * clamped : -1 + (4 - 2 * clamped) * clamped;
      default:
        return clamped;
    }
  }

  function sceneTransitionColorLike(key, from, to) {
    if (typeof from !== "string" || typeof to !== "string") {
      return false;
    }
    if (typeof key === "string" && key.toLowerCase().indexOf("color") >= 0) {
      return true;
    }
    return /^#|^rgba?\(/i.test(from.trim()) && /^#|^rgba?\(/i.test(to.trim());
  }

  function sceneRGBAToHSL(rgba) {
    const r = clamp01(sceneNumber(rgba && rgba[0], 0));
    const g = clamp01(sceneNumber(rgba && rgba[1], 0));
    const b = clamp01(sceneNumber(rgba && rgba[2], 0));
    const a = clamp01(sceneNumber(rgba && rgba[3], 1));
    const max = Math.max(r, g, b);
    const min = Math.min(r, g, b);
    const delta = max - min;
    let h = 0;
    let s = 0;
    const l = (max + min) / 2;
    if (delta > 0.000001) {
      s = l > 0.5 ? delta / (2 - max - min) : delta / (max + min);
      switch (max) {
        case r:
          h = ((g - b) / delta) + (g < b ? 6 : 0);
          break;
        case g:
          h = ((b - r) / delta) + 2;
          break;
        default:
          h = ((r - g) / delta) + 4;
          break;
      }
      h /= 6;
    }
    return [h, s, l, a];
  }

  function sceneHueToRGB(p, q, t) {
    let value = t;
    if (value < 0) value += 1;
    if (value > 1) value -= 1;
    if (value < 1 / 6) return p + (q - p) * 6 * value;
    if (value < 1 / 2) return q;
    if (value < 2 / 3) return p + (q - p) * (2 / 3 - value) * 6;
    return p;
  }

  function sceneHSLToRGBA(hsla) {
    const h = sceneNumber(hsla && hsla[0], 0);
    const s = clamp01(sceneNumber(hsla && hsla[1], 0));
    const l = clamp01(sceneNumber(hsla && hsla[2], 0));
    const a = clamp01(sceneNumber(hsla && hsla[3], 1));
    if (s <= 0.000001) {
      return [l, l, l, a];
    }
    const q = l < 0.5 ? l * (1 + s) : l + s - (l * s);
    const p = 2 * l - q;
    return [
      sceneHueToRGB(p, q, h + (1 / 3)),
      sceneHueToRGB(p, q, h),
      sceneHueToRGB(p, q, h - (1 / 3)),
      a,
    ];
  }

  function sceneLerpColorString(from, to, t) {
    const left = sceneColorRGBA(from, [0, 0, 0, 1]);
    const right = sceneColorRGBA(to, left);
    const leftHSL = sceneRGBAToHSL(left);
    const rightHSL = sceneRGBAToHSL(right);
    const achromatic = leftHSL[1] <= 0.0001 && rightHSL[1] <= 0.0001;
    let rgba;
    if (achromatic) {
      rgba = [
        left[0] + (right[0] - left[0]) * t,
        left[1] + (right[1] - left[1]) * t,
        left[2] + (right[2] - left[2]) * t,
        left[3] + (right[3] - left[3]) * t,
      ];
    } else {
      let hueDelta = rightHSL[0] - leftHSL[0];
      if (hueDelta > 0.5) hueDelta -= 1;
      if (hueDelta < -0.5) hueDelta += 1;
      rgba = sceneHSLToRGBA([
        (leftHSL[0] + hueDelta * t + 1) % 1,
        leftHSL[1] + (rightHSL[1] - leftHSL[1]) * t,
        leftHSL[2] + (rightHSL[2] - leftHSL[2]) * t,
        leftHSL[3] + (rightHSL[3] - leftHSL[3]) * t,
      ]);
    }
    return sceneRGBAString(rgba);
  }

  function sceneTransitionValuesEqual(left, right) {
    if (left === right) {
      return true;
    }
    if (Array.isArray(left) || Array.isArray(right)) {
      if (!Array.isArray(left) || !Array.isArray(right) || left.length !== right.length) {
        return false;
      }
      for (let i = 0; i < left.length; i += 1) {
        if (!sceneTransitionValuesEqual(left[i], right[i])) {
          return false;
        }
      }
      return true;
    }
    if (sceneIsPlainObject(left) && sceneIsPlainObject(right)) {
      const keys = new Set(Object.keys(left).concat(Object.keys(right)));
      for (const key of keys) {
        if (sceneTransitionMetadataKey(key)) {
          continue;
        }
        if (!sceneTransitionValuesEqual(left[key], right[key])) {
          return false;
        }
      }
      return true;
    }
    return false;
  }

  function sceneTransitionBuildDelta(from, to, keyName) {
    if (sceneTransitionValuesEqual(from, to)) {
      return null;
    }
    if (sceneIsPlainObject(from) && sceneIsPlainObject(to)) {
      const delta = {};
      const keys = new Set(Object.keys(from).concat(Object.keys(to)));
      for (const key of keys) {
        if (sceneTransitionMetadataKey(key)) {
          continue;
        }
        const child = sceneTransitionBuildDelta(from[key], to[key], key);
        if (child !== null) {
          delta[key] = child;
        }
      }
      return Object.keys(delta).length > 0 ? delta : null;
    }
    return {
      __from: sceneCloneData(from),
      __to: sceneCloneData(to),
      __key: typeof keyName === "string" ? keyName : "",
    };
  }

  function sceneTransitionLeafValue(from, to, t, keyName) {
    if (typeof from === "number" && typeof to === "number" && Number.isFinite(from) && Number.isFinite(to)) {
      const value = from + (to - from) * t;
      return Number.isInteger(from) && Number.isInteger(to) ? Math.round(value) : value;
    }
    if (sceneTransitionColorLike(keyName, from, to)) {
      return sceneLerpColorString(from, to, t);
    }
    return sceneCloneData(to);
  }

  function sceneTransitionPatchAt(delta, t) {
    if (!delta || typeof delta !== "object") {
      return null;
    }
    if (Object.prototype.hasOwnProperty.call(delta, "__from")) {
      return sceneTransitionLeafValue(delta.__from, delta.__to, t, delta.__key);
    }
    const patch = {};
    const keys = Object.keys(delta);
    for (let i = 0; i < keys.length; i += 1) {
      const key = keys[i];
      patch[key] = sceneTransitionPatchAt(delta[key], t);
    }
    return patch;
  }

  function sceneApplyTransitionPatch(target, patch) {
    if (!sceneIsPlainObject(target) || patch == null) {
      return;
    }
    const keys = Object.keys(patch);
    for (let i = 0; i < keys.length; i += 1) {
      const key = keys[i];
      const value = patch[key];
      if (sceneIsPlainObject(value) && sceneIsPlainObject(target[key])) {
        sceneApplyTransitionPatch(target[key], value);
      } else {
        target[key] = sceneCloneData(value);
        if (key === "colors" && Object.prototype.hasOwnProperty.call(target, "_cachedColors")) {
          target._cachedColors = null;
        } else if (key === "positions" && Object.prototype.hasOwnProperty.call(target, "_cachedPos")) {
          target._cachedPos = null;
        } else if (key === "sizes" && Object.prototype.hasOwnProperty.call(target, "_cachedSizes")) {
          target._cachedSizes = null;
        }
      }
    }
    if (typeof target._lightHash === "number" && typeof hashLightContent === "function") {
      target._lightHash = hashLightContent(target);
    }
    if (typeof target._envHash === "number" && typeof hashEnvironmentContent === "function") {
      target._envHash = hashEnvironmentContent(target);
    }
  }

  function sceneTransitionKey(kind, entry) {
    return String(kind || "scene") + ":" + String(entry && entry.id ? entry.id : "__singleton");
  }

  function sceneCancelEntryTransition(state, kind, entry) {
    if (!state || !Array.isArray(state._transitions)) {
      return;
    }
    const key = sceneTransitionKey(kind, entry);
    state._transitions = state._transitions.filter(function(item) {
      return item.key !== key;
    });
  }

  function sceneNormalizeEntryByKind(kind, raw, fallback) {
    switch (kind) {
      case "object":
        return normalizeSceneObject(raw, fallback && fallback.id ? fallback.id : 0, fallback);
      case "label":
        return normalizeSceneLabel(raw, fallback && fallback.id ? fallback.id : 0, fallback);
      case "sprite":
        return normalizeSceneSprite(raw, fallback && fallback.id ? fallback.id : 0, fallback);
      case "light":
        return normalizeSceneLight(raw, fallback && fallback.id ? fallback.id : 0, fallback);
      case "points":
        return normalizeScenePointsEntry(raw, fallback && fallback.id ? fallback.id : 0, fallback);
      case "instanced":
        return normalizeSceneInstancedMeshEntry(raw, fallback && fallback.id ? fallback.id : 0, fallback);
      case "compute":
        return normalizeSceneComputeParticlesEntry(raw, fallback && fallback.id ? fallback.id : 0, fallback);
      case "environment":
        return normalizeSceneEnvironment(raw, fallback);
      default:
        return sceneCloneData(fallback || raw);
    }
  }

  function sceneDefaultTransitionPatch(kind) {
    switch (kind) {
      case "object":
      case "points":
      case "sprite":
        return { opacity: 0 };
      case "light":
        return { intensity: 0 };
      case "compute":
        return { count: 0, material: { opacity: 0 } };
      case "environment":
        return { ambientIntensity: 0, skyIntensity: 0, groundIntensity: 0, fogDensity: 0 };
      default:
        return null;
    }
  }

  function sceneTransitionStatePatch(kind, entry, phase) {
    const statePatch = phase === "out" ? entry && entry._outState : entry && entry._inState;
    if (sceneIsPlainObject(statePatch) && Object.keys(statePatch).length > 0) {
      return sceneCloneData(statePatch);
    }
    return null;
  }

  function sceneStartEntryTransition(state, kind, entry, reducedMotion, nowMs) {
    if (!entry || !sceneIsPlainObject(entry)) {
      return false;
    }
    const timing = sceneTransitionTimingForPhase(entry, "in");
    let startPatch = sceneTransitionStatePatch(kind, entry, "in");
    if (!startPatch && timing.duration > 0) {
      startPatch = sceneCloneData(sceneDefaultTransitionPatch(kind));
    }
    if ((!startPatch || !Object.keys(startPatch).length) && timing.duration <= 0) {
      return false;
    }
    const target = sceneCloneData(entry);
    const startState = sceneNormalizeEntryByKind(kind, startPatch || {}, target);
    sceneApplyTransitionPatch(entry, startState);
    if (reducedMotion || timing.duration <= 0) {
      sceneApplyTransitionPatch(entry, target);
      return false;
    }
    const delta = sceneTransitionBuildDelta(startState, target, "");
    if (!delta) {
      sceneApplyTransitionPatch(entry, target);
      return false;
    }
    sceneCancelEntryTransition(state, kind, entry);
    sceneStateTransitions(state).push({
      key: sceneTransitionKey(kind, entry),
      entry,
      target,
      delta,
      startTime: nowMs,
      duration: Math.max(1, timing.duration),
      easing: timing.easing,
    });
    return true;
  }

  function scenePrimeInitialTransitions(state, reducedMotion, nowMs) {
    let started = false;
    if (state && sceneIsPlainObject(state.environment)) {
      started = sceneStartEntryTransition(state, "environment", state.environment, reducedMotion, nowMs) || started;
    }
    const collections = [
      ["object", sceneStateObjects(state)],
      ["label", sceneStateLabels(state)],
      ["sprite", sceneStateSprites(state)],
      ["light", sceneStateLights(state)],
      ["points", Array.isArray(state && state.points) ? state.points : []],
      ["instanced", Array.isArray(state && state.instancedMeshes) ? state.instancedMeshes : []],
      ["compute", Array.isArray(state && state.computeParticles) ? state.computeParticles : []],
    ];
    for (let ci = 0; ci < collections.length; ci += 1) {
      const kind = collections[ci][0];
      const entries = collections[ci][1];
      for (let i = 0; i < entries.length; i += 1) {
        started = sceneStartEntryTransition(state, kind, entries[i], reducedMotion, nowMs) || started;
      }
    }
    return started;
  }

  function sceneAdvanceTransitions(state, nowMs) {
    const active = sceneStateTransitions(state);
    if (!active.length) {
      return false;
    }
    const next = [];
    for (let i = 0; i < active.length; i += 1) {
      const transition = active[i];
      const elapsed = Math.max(0, nowMs - sceneNumber(transition.startTime, 0));
      const rawT = sceneClamp(elapsed / Math.max(1, sceneNumber(transition.duration, 1)), 0, 1);
      const eased = sceneTransitionEase(transition.easing, rawT);
      const patch = sceneTransitionPatchAt(transition.delta, eased);
      if (patch) {
        sceneApplyTransitionPatch(transition.entry, patch);
      }
      if (rawT >= 1) {
        sceneApplyTransitionPatch(transition.entry, transition.target);
      } else {
        next.push(transition);
      }
    }
    state._transitions = next;
    return true;
  }

  function sceneEntryListensToEvent(entry, eventName) {
    if (!entry || !Array.isArray(entry._live) || !eventName) {
      return false;
    }
    return entry._live.indexOf(eventName) >= 0;
  }

  function sceneApplyLiveTransition(state, kind, entry, payload, reducedMotion, nowMs) {
    if (!entry || !sceneEntryListensToEvent(entry, payload && payload.__eventName)) {
      return false;
    }
    const target = sceneNormalizeEntryByKind(kind, payload, entry);
    if (sceneTransitionValuesEqual(entry, target)) {
      return false;
    }
    const timing = sceneTransitionTimingForPhase(entry, "update");
    sceneCancelEntryTransition(state, kind, entry);
    if (reducedMotion || timing.duration <= 0) {
      sceneApplyTransitionPatch(entry, target);
      return true;
    }
    const current = sceneCloneData(entry);
    const delta = sceneTransitionBuildDelta(current, target, "");
    if (!delta) {
      return false;
    }
    sceneStateTransitions(state).push({
      key: sceneTransitionKey(kind, entry),
      entry,
      target,
      delta,
      startTime: nowMs,
      duration: Math.max(1, timing.duration),
      easing: timing.easing,
    });
    return true;
  }

  function sceneApplyLiveEvent(state, eventName, payload, reducedMotion, nowMs) {
    const event = typeof eventName === "string" ? eventName.trim() : "";
    if (!event) {
      return false;
    }
    const rawPayload = sceneIsPlainObject(payload) ? sceneCloneData(payload) : {};
    rawPayload.__eventName = event;
    let changed = sceneApplyLiveTransition(state, "environment", state && state.environment, rawPayload, reducedMotion, nowMs);
    const collections = [
      ["object", sceneStateObjects(state)],
      ["label", sceneStateLabels(state)],
      ["sprite", sceneStateSprites(state)],
      ["light", sceneStateLights(state)],
      ["points", Array.isArray(state && state.points) ? state.points : []],
      ["instanced", Array.isArray(state && state.instancedMeshes) ? state.instancedMeshes : []],
      ["compute", Array.isArray(state && state.computeParticles) ? state.computeParticles : []],
    ];
    for (let ci = 0; ci < collections.length; ci += 1) {
      const kind = collections[ci][0];
      const entries = collections[ci][1];
      for (let i = 0; i < entries.length; i += 1) {
        changed = sceneApplyLiveTransition(state, kind, entries[i], rawPayload, reducedMotion, nowMs) || changed;
      }
    }
    delete rawPayload.__eventName;
    return changed;
  }

  function sceneObjectAnimated(object) {
    if (!object || typeof object !== "object") {
      return false;
    }
    if (sceneNumber(object.spinX, 0) !== 0 || sceneNumber(object.spinY, 0) !== 0 || sceneNumber(object.spinZ, 0) !== 0) {
      return true;
    }
    if (sceneNumber(object.driftSpeed, 0) === 0) {
      return false;
    }
    return sceneNumber(object.shiftX, 0) !== 0 || sceneNumber(object.shiftY, 0) !== 0 || sceneNumber(object.shiftZ, 0) !== 0;
  }

  function sceneLabelAnimated(label) {
    if (!label || typeof label !== "object") {
      return false;
    }
    if (sceneNumber(label.driftSpeed, 0) === 0) {
      return false;
    }
    return sceneNumber(label.shiftX, 0) !== 0 || sceneNumber(label.shiftY, 0) !== 0 || sceneNumber(label.shiftZ, 0) !== 0;
  }

  function sceneSpriteAnimated(sprite) {
    if (!sprite || typeof sprite !== "object") {
      return false;
    }
    if (sceneNumber(sprite.driftSpeed, 0) === 0) {
      return false;
    }
    return sceneNumber(sprite.shiftX, 0) !== 0 || sceneNumber(sprite.shiftY, 0) !== 0 || sceneNumber(sprite.shiftZ, 0) !== 0;
  }

  const SCENE_CMD_CREATE_OBJECT = 0;
  const SCENE_CMD_REMOVE_OBJECT = 1;
  const SCENE_CMD_SET_TRANSFORM = 2;
  const SCENE_CMD_SET_MATERIAL = 3;
  const SCENE_CMD_SET_LIGHT = 4;
  const SCENE_CMD_SET_CAMERA = 5;
  const SCENE_CMD_SET_PARTICLES = 6;

  function applySceneCommands(state, commands) {
    if (!state || !Array.isArray(commands) || commands.length === 0) return;
    for (const command of commands) {
      applySceneCommand(state, command);
    }
  }

  function applySceneCommand(state, command) {
    if (!command || typeof command !== "object") return;
    switch (command.kind) {
      case SCENE_CMD_CREATE_OBJECT:
        applySceneCreateCommand(state, command.objectId, command.data);
        return;
      case SCENE_CMD_REMOVE_OBJECT:
        state.objects.delete(sceneObjectKey(command.objectId));
        state.labels.delete(sceneObjectKey(command.objectId));
        state.sprites.delete(sceneObjectKey(command.objectId));
        state.lights.delete(sceneObjectKey(command.objectId));
        return;
      case SCENE_CMD_SET_TRANSFORM:
      case SCENE_CMD_SET_MATERIAL:
        applySceneObjectPatch(state, command.objectId, command.data);
        return;
      case SCENE_CMD_SET_CAMERA:
        state.camera = normalizeSceneCamera(command.data || {}, state.camera);
        return;
      case SCENE_CMD_SET_LIGHT:
        applySceneLightPatch(state, command.objectId, command.data);
        return;
      case SCENE_CMD_SET_PARTICLES:
      default:
        return;
    }
  }

  function applySceneCreateCommand(state, objectID, payload) {
    if (!payload || typeof payload !== "object") return;
    if (payload.kind === "camera") {
      state.camera = normalizeSceneCamera(payload.props || {}, state.camera);
      return;
    }
    if (payload.kind === "light") {
      const light = sceneLightFromPayload(objectID, payload, state.lights.get(sceneObjectKey(objectID)));
      if (light) {
        state.lights.set(sceneObjectKey(objectID), light);
      }
      return;
    }
    if (payload.kind === "particles") {
      return;
    }
    if (payload.kind === "label") {
      const label = sceneLabelFromPayload(objectID, payload, state.labels.get(sceneObjectKey(objectID)));
      if (label) {
        state.labels.set(sceneObjectKey(objectID), label);
      }
      return;
    }
    if (payload.kind === "sprite") {
      const sprite = sceneSpriteFromPayload(objectID, payload, state.sprites.get(sceneObjectKey(objectID)));
      if (sprite) {
        state.sprites.set(sceneObjectKey(objectID), sprite);
      }
      return;
    }
    const key = sceneObjectKey(objectID);
    const next = sceneObjectFromPayload(objectID, payload, state.objects.get(key));
    if (next) {
      state.objects.set(key, next);
    }
  }

  function applySceneObjectPatch(state, objectID, patch) {
    const key = sceneObjectKey(objectID);
    const current = state.objects.get(key);
    if (current) {
      const next = sceneObjectFromPayload(objectID, {
        geometry: current.kind,
        props: Object.assign({}, current, patch || {}),
      }, current);
      if (next) {
        state.objects.set(key, next);
      }
      return;
    }
    const currentLabel = state.labels.get(key);
    if (currentLabel) {
      const nextLabel = sceneLabelFromPayload(objectID, {
        props: Object.assign({}, currentLabel, patch || {}),
      }, currentLabel);
      if (nextLabel) {
        state.labels.set(key, nextLabel);
      }
      return;
    }
    const currentSprite = state.sprites.get(key);
    if (!currentSprite) return;
    const nextSprite = sceneSpriteFromPayload(objectID, {
      props: Object.assign({}, currentSprite, patch || {}),
    }, currentSprite);
    if (nextSprite) {
      state.sprites.set(key, nextSprite);
    }
  }

  function sceneObjectKey(objectID) {
    return String(objectID);
  }

  function sceneObjectFromPayload(objectID, payload, fallback) {
    const current = fallback && typeof fallback === "object" ? fallback : {};
    const props = payload && payload.props && typeof payload.props === "object" ? payload.props : {};
    const geometry = payload && typeof payload.geometry === "string" && payload.geometry ? payload.geometry : current.kind;
    const merged = Object.assign({}, current, props);
    merged.id = current.id || merged.id || ("scene-object-" + objectID);
    merged.kind = normalizeSceneKind(merged.kind || geometry);
    return normalizeSceneObject(merged, objectID, current);
  }

  function sceneLabelFromPayload(objectID, payload, fallback) {
    const current = fallback && typeof fallback === "object" ? fallback : {};
    const props = payload && payload.props && typeof payload.props === "object" ? payload.props : {};
    const merged = Object.assign({}, current, props);
    merged.id = current.id || merged.id || ("scene-label-" + objectID);
    const label = normalizeSceneLabel(merged, objectID, current);
    if (!label.text.trim()) {
      return null;
    }
    return label;
  }

  function sceneSpriteFromPayload(objectID, payload, fallback) {
    const current = fallback && typeof fallback === "object" ? fallback : {};
    const props = payload && payload.props && typeof payload.props === "object" ? payload.props : {};
    const merged = Object.assign({}, current, props);
    merged.id = current.id || merged.id || ("scene-sprite-" + objectID);
    const sprite = normalizeSceneSprite(merged, objectID, current);
    if (!sprite.src) {
      return null;
    }
    return sprite;
  }

  function applySceneLightPatch(state, objectID, patch) {
    const key = sceneObjectKey(objectID);
    const current = state.lights.get(key);
    if (!current) {
      return;
    }
    const next = normalizeSceneLight(Object.assign({}, current, patch || {}), objectID, current);
    if (next) {
      state.lights.set(key, next);
    }
  }

  function sceneLightFromPayload(objectID, payload, fallback) {
    const current = fallback && typeof fallback === "object" ? fallback : {};
    const props = payload && payload.props && typeof payload.props === "object" ? payload.props : {};
    const merged = Object.assign({}, current, props);
    merged.id = current.id || merged.id || ("scene-light-" + objectID);
    return normalizeSceneLight(merged, objectID, current);
  }

  function clearChildren(node) {
    while (node && node.firstChild) {
      node.removeChild(node.firstChild);
    }
  }

  function sceneRenderCamera(camera, out) {
    const target = out || { x: 0, y: 0, z: 0, rotationX: 0, rotationY: 0, rotationZ: 0, fov: 0, near: 0, far: 0 };
    target.x = sceneNumber(camera && camera.x, 0);
    target.y = sceneNumber(camera && camera.y, 0);
    target.z = sceneNumber(camera && camera.z, 6);
    target.rotationX = sceneNumber(camera && camera.rotationX, 0);
    target.rotationY = sceneNumber(camera && camera.rotationY, 0);
    target.rotationZ = sceneNumber(camera && camera.rotationZ, 0);
    target.fov = sceneNumber(camera && camera.fov, 75);
    target.near = sceneNumber(camera && camera.near, 0.05);
    target.far = sceneNumber(camera && camera.far, 128);
    return target;
  }

  function sceneCameraEquivalent(left, right) {
    const a = sceneRenderCamera(left);
    const b = sceneRenderCamera(right);
    return Math.abs(a.x - b.x) <= 0.0001 &&
      Math.abs(a.y - b.y) <= 0.0001 &&
      Math.abs(a.z - b.z) <= 0.0001 &&
      Math.abs(a.rotationX - b.rotationX) <= 0.0001 &&
      Math.abs(a.rotationY - b.rotationY) <= 0.0001 &&
      Math.abs(a.rotationZ - b.rotationZ) <= 0.0001 &&
      Math.abs(a.fov - b.fov) <= 0.0001 &&
      Math.abs(a.near - b.near) <= 0.0001 &&
      Math.abs(a.far - b.far) <= 0.0001;
  }

  const _sceneBoundsDepthCameraScratch = {
    x: 0, y: 0, z: 0,
    rotationX: 0, rotationY: 0, rotationZ: 0,
    fov: 0, near: 0, far: 0,
  };

  function sceneBoundsDepthMetrics(bounds, camera) {
    if (!bounds) {
      const depth = sceneWorldPointDepth(0, camera);
      return { near: depth, far: depth, center: depth };
    }
    const cam = sceneRenderCamera(camera, _sceneBoundsDepthCameraScratch);

    const sinX = Math.sin(-cam.rotationX);
    const cosX = Math.cos(-cam.rotationX);
    const sinY = Math.sin(-cam.rotationY);
    const cosY = Math.cos(-cam.rotationY);
    const sinZ = Math.sin(-cam.rotationZ);
    const cosZ = Math.cos(-cam.rotationZ);

    const minX = sceneNumber(bounds.minX, 0);
    const minY = sceneNumber(bounds.minY, 0);
    const minZ = sceneNumber(bounds.minZ, 0);
    const maxX = sceneNumber(bounds.maxX, 0);
    const maxY = sceneNumber(bounds.maxY, 0);
    const maxZ = sceneNumber(bounds.maxZ, 0);

    let near = Infinity;
    let far = -Infinity;

    for (let i = 0; i < 8; i += 1) {
      const worldX = (i & 4) ? maxX : minX;
      const worldY = (i & 2) ? maxY : minY;
      const worldZ = (i & 1) ? maxZ : minZ;

      let lx = worldX - cam.x;
      let ly = worldY - cam.y;
      let lz = worldZ + cam.z;

      let nX = lx * cosZ - ly * sinZ;
      let nY = lx * sinZ + ly * cosZ;
      lx = nX;
      ly = nY;

      nX = lx * cosY + lz * sinY;
      let nZ = -lx * sinY + lz * cosY;
      lx = nX;
      lz = nZ;

      nZ = ly * sinX + lz * cosX;
      lz = nZ;

      if (lz < near) near = lz;
      if (lz > far) far = lz;
    }

    return {
      near: near,
      far: far,
      center: (near + far) / 2,
    };
  }

  function sceneBoundsViewCulled(bounds, camera) {
    if (!bounds) {
      return false;
    }
    const depth = sceneBoundsDepthMetrics(bounds, camera);
    const near = sceneNumber(camera && camera.near, 0.05);
    const far = sceneNumber(camera && camera.far, 128);
    return depth.far <= near || depth.near >= far;
  }

  function createSceneRenderBundle(width, height, background, camera, objects, labels, sprites, lights, environment, timeSeconds, points, instancedMeshes, computeParticles) {
    const resolvedEnvironment = sceneResolveLightingEnvironment(environment, Array.isArray(lights) && lights.length > 0);
    const bundle = {
      background: background,
      camera: sceneRenderCamera(camera),
      lights: Array.isArray(lights) ? lights.slice() : [],
      environment: resolvedEnvironment,
      materials: [],
      objects: [],
      surfaces: [],
      labels: [],
      sprites: [],
      lines: [],
      points: Array.isArray(points) ? points : [],
      instancedMeshes: Array.isArray(instancedMeshes) ? instancedMeshes : [],
      computeParticles: Array.isArray(computeParticles) ? computeParticles : [],
      positions: [],
      colors: [],
      worldPositions: [],
      worldColors: [],
      worldLineWidths: [],
      worldLinePasses: [],
      meshObjects: [],
      worldMeshPositions: [],
      worldMeshColors: [],
      worldMeshNormals: [],
      worldMeshUVs: [],
      worldMeshTangents: [],
      vertexCount: 0,
      worldVertexCount: 0,
      worldMeshVertexCount: 0,
      objectCount: 0,
    };
    const materialLookup = new Map();
    appendSceneGridToBundle(bundle, width, height);
    for (const object of objects) {
      appendSceneObjectToBundle(bundle, materialLookup, camera, width, height, object, bundle.lights, resolvedEnvironment, timeSeconds);
    }
    for (const label of labels || []) {
      appendSceneLabelToBundle(bundle, camera, width, height, label, timeSeconds);
    }
    for (const sprite of sprites || []) {
      appendSceneSpriteToBundle(bundle, camera, width, height, sprite, timeSeconds);
    }
    bundle.positions = new Float32Array(bundle.positions);
    bundle.colors = new Float32Array(bundle.colors);
    bundle.vertexCount = bundle.positions.length / 2;
    bundle.worldPositions = new Float32Array(bundle.worldPositions);
    bundle.worldColors = new Float32Array(bundle.worldColors);
    bundle.worldVertexCount = bundle.worldPositions.length / 3;
    bundle.worldLineWidths = new Float32Array(bundle.worldLineWidths);
    bundle.worldLinePasses = new Uint8Array(bundle.worldLinePasses);
    bundle.worldMeshPositions = new Float32Array(bundle.worldMeshPositions);
    bundle.worldMeshColors = new Float32Array(bundle.worldMeshColors);
    bundle.worldMeshNormals = new Float32Array(bundle.worldMeshNormals);
    bundle.worldMeshUVs = new Float32Array(bundle.worldMeshUVs);
    bundle.worldMeshTangents = new Float32Array(bundle.worldMeshTangents);
    bundle.worldMeshVertexCount = bundle.worldMeshPositions.length / 3;
    bundle.objectCount = bundle.objects.length;
    return bundle;
  }

  function translateScenePointInto(out, px, py, pz, object, timeSeconds) {
    const scaleX = sceneNumber(object && object.scaleX, 1);
    const scaleY = sceneNumber(object && object.scaleY, 1);
    const scaleZ = sceneNumber(object && object.scaleZ, 1);
    let x = sceneNumber(px, 0) * scaleX;
    let y = sceneNumber(py, 0) * scaleY;
    let z = sceneNumber(pz, 0) * scaleZ;

    const rotX = object.rotationX + object.spinX * timeSeconds;
    const rotY = object.rotationY + object.spinY * timeSeconds;
    const rotZ = object.rotationZ + object.spinZ * timeSeconds;

    const sinX = Math.sin(rotX);
    const cosX = Math.cos(rotX);
    let nextY = y * cosX - z * sinX;
    let nextZ = y * sinX + z * cosX;
    y = nextY;
    z = nextZ;

    const sinY = Math.sin(rotY);
    const cosY = Math.cos(rotY);
    let nextX = x * cosY + z * sinY;
    nextZ = -x * sinY + z * cosY;
    x = nextX;
    z = nextZ;

    const sinZ = Math.sin(rotZ);
    const cosZ = Math.cos(rotZ);
    nextX = x * cosZ - y * sinZ;
    nextY = x * sinZ + y * cosZ;
    x = nextX;
    y = nextY;

    if (object && (object.shiftX || object.shiftY || object.shiftZ)) {
      const driftPhase = sceneNumber(object.driftPhase, 0);
      const angle = driftPhase + timeSeconds * sceneNumber(object.driftSpeed, 0);
      x += Math.cos(angle) * sceneNumber(object.shiftX, 0);
      y += Math.sin(angle * 0.82 + driftPhase * 0.35) * sceneNumber(object.shiftY, 0);
      z += Math.sin(angle) * sceneNumber(object.shiftZ, 0);
    }

    out.x = x + object.x;
    out.y = y + object.y;
    out.z = z + object.z;
  }

  const _lineSegmentFromScratch = { x: 0, y: 0, z: 0 };
  const _lineSegmentToScratch = { x: 0, y: 0, z: 0 };
  const _meshTriangleP0Scratch = { x: 0, y: 0, z: 0 };
  const _meshTriangleP1Scratch = { x: 0, y: 0, z: 0 };
  const _meshTriangleP2Scratch = { x: 0, y: 0, z: 0 };
  const _meshTrianglePoints = [_meshTriangleP0Scratch, _meshTriangleP1Scratch, _meshTriangleP2Scratch];

  function appendSceneGridToBundle(bundle, width, height) {
    for (let x = 0; x <= width; x += 48) {
      appendSceneLine(bundle, width, height, { x: x, y: 0 }, { x: x, y: height }, "rgba(141, 225, 255, 0.14)", 1);
    }
    for (let y = 0; y <= height; y += 48) {
      appendSceneLine(bundle, width, height, { x: 0, y: y }, { x: width, y: y }, "rgba(141, 225, 255, 0.14)", 1);
    }
  }

  function appendSceneObjectToBundle(bundle, materialLookup, camera, width, height, object, lights, environment, timeSeconds) {
    if (sceneObjectHasTriangleMesh(object)) {
      appendSceneMeshObjectToBundle(bundle, materialLookup, camera, width, height, object, lights, environment, timeSeconds);
      return;
    }
    const sourceSegments = sceneObjectSegments(object);
    const vertexOffset = bundle.worldPositions.length / 3;
    const material = sceneObjectMaterialProfile(object);
    const materialIndex = sceneBundleMaterialIndex(bundle, materialLookup, material);
    const includeLineGeometry = sceneWorldObjectUsesLinePass(object, material);
    let bounds = null;
    let vertexCount = 0;
    if (includeLineGeometry) {
      const rawLineWidth = sceneNumber(object && object.lineWidth, 0);
      const objectLineWidth = rawLineWidth > 0 ? rawLineWidth : 1.8;
      const objectPassString = sceneWorldObjectRenderPass(object, material);
      const objectPassIndex = objectPassString === "alpha" ? 1 : (objectPassString === "additive" ? 2 : 0);
      const fromWorld = _lineSegmentFromScratch;
      const toWorld = _lineSegmentToScratch;
      for (let index = 0; index < sourceSegments.length; index += 1) {
        const sourceSegment = sourceSegments[index];
        translateScenePointInto(fromWorld, sourceSegment[0] && sourceSegment[0].x, sourceSegment[0] && sourceSegment[0].y, sourceSegment[0] && sourceSegment[0].z, object, timeSeconds);
        translateScenePointInto(toWorld, sourceSegment[1] && sourceSegment[1].x, sourceSegment[1] && sourceSegment[1].y, sourceSegment[1] && sourceSegment[1].z, object, timeSeconds);
        const fromLighting = sceneLitColorRGBA(material, fromWorld, sceneObjectWorldNormal(object, sourceSegment[0], timeSeconds), lights, environment);
        const toLighting = sceneLitColorRGBA(material, toWorld, sceneObjectWorldNormal(object, sourceSegment[1], timeSeconds), lights, environment);
        bundle.worldPositions.push(fromWorld.x, fromWorld.y, fromWorld.z, toWorld.x, toWorld.y, toWorld.z);
        bundle.worldColors.push(
          fromLighting[0], fromLighting[1], fromLighting[2], fromLighting[3],
          toLighting[0], toLighting[1], toLighting[2], toLighting[3],
        );
        bundle.worldLineWidths.push(rawLineWidth);
        bundle.worldLinePasses.push(objectPassIndex);
        bounds = sceneExpandWorldBounds(bounds, fromWorld);
        bounds = sceneExpandWorldBounds(bounds, toWorld);
        vertexCount += 2;
        const from = sceneProjectPoint(fromWorld, camera, width, height);
        const to = sceneProjectPoint(toWorld, camera, width, height);
        if (!from || !to) continue;
        const stroke = sceneMixRGBA(fromLighting, toLighting);
        stroke[3] = clamp01(stroke[3] * sceneMaterialOpacity(material));
        appendSceneLine(bundle, width, height, from, to, sceneRGBAString(stroke), objectLineWidth);
      }
    } else if (sceneObjectHasTexturedSurface(object, material)) {
      const corners = scenePlaneSurfaceCorners(object, timeSeconds);
      for (const corner of corners) {
        bounds = sceneExpandWorldBounds(bounds, corner);
      }
    }
    if (vertexCount > 0 || bounds) {
      const depth = sceneBoundsDepthMetrics(bounds, camera);
      bundle.objects.push({
        id: object.id,
        kind: object.kind,
        pickable: typeof object.pickable === "boolean" ? object.pickable : undefined,
        materialIndex: materialIndex,
        renderPass: sceneWorldObjectRenderPass(object, material),
        vertexOffset: vertexOffset,
        vertexCount: vertexCount,
        static: Boolean(object.static),
        castShadow: Boolean(object.castShadow),
        receiveShadow: Boolean(object.receiveShadow),
        depthWrite: object.depthWrite,
        bounds: bounds || {
          minX: 0,
          minY: 0,
          minZ: 0,
          maxX: 0,
          maxY: 0,
          maxZ: 0,
        },
        depthNear: depth.near,
        depthFar: depth.far,
        depthCenter: depth.center,
        viewCulled: Boolean(object.viewCulled) || sceneBoundsViewCulled(bounds, camera),
      });
      appendSceneSurfaceToBundle(bundle, camera, object, materialIndex, material, bounds, depth, timeSeconds);
    }
  }

  function sceneObjectHasTriangleMesh(object) {
    return Boolean(
      object &&
      object.vertices &&
      object.vertices.positions &&
      typeof object.vertices.count === "number" &&
      object.vertices.count >= 3
    );
  }

  function sceneMeshVertexPoint(vertices, index) {
    const offset = index * 3;
    return {
      x: sceneNumber(vertices && vertices.positions && vertices.positions[offset], 0),
      y: sceneNumber(vertices && vertices.positions && vertices.positions[offset + 1], 0),
      z: sceneNumber(vertices && vertices.positions && vertices.positions[offset + 2], 0),
    };
  }

  function sceneMeshVertexNormal(vertices, index) {
    const offset = index * 3;
    if (!vertices || !vertices.normals || vertices.normals.length < offset + 3) {
      return { x: 0, y: 1, z: 0 };
    }
    return {
      x: sceneNumber(vertices.normals[offset], 0),
      y: sceneNumber(vertices.normals[offset + 1], 1),
      z: sceneNumber(vertices.normals[offset + 2], 0),
    };
  }

  function sceneMeshVertexUV(vertices, index) {
    const offset = index * 2;
    if (!vertices || !vertices.uvs || vertices.uvs.length < offset + 2) {
      return { x: 0, y: 0 };
    }
    return {
      x: sceneNumber(vertices.uvs[offset], 0),
      y: sceneNumber(vertices.uvs[offset + 1], 0),
    };
  }

  function sceneMeshVertexTangent(vertices, index) {
    const offset = index * 4;
    if (!vertices || !vertices.tangents || vertices.tangents.length < offset + 4) {
      return { x: 1, y: 0, z: 0, w: 1 };
    }
    return {
      x: sceneNumber(vertices.tangents[offset], 1),
      y: sceneNumber(vertices.tangents[offset + 1], 0),
      z: sceneNumber(vertices.tangents[offset + 2], 0),
      w: sceneNumber(vertices.tangents[offset + 3], 1),
    };
  }

  function sceneMeshWorldNormal(object, vertices, index, timeSeconds) {
    const normal = sceneMeshVertexNormal(vertices, index);
    return sceneNormalizeDirection(sceneRotatePoint(
      normal,
      object.rotationX + object.spinX * timeSeconds,
      object.rotationY + object.spinY * timeSeconds,
      object.rotationZ + object.spinZ * timeSeconds,
    ));
  }

  function sceneMeshWorldTangent(object, vertices, index, timeSeconds) {
    const tangent = sceneMeshVertexTangent(vertices, index);
    const rotated = sceneNormalizeDirection(sceneRotatePoint({
      x: tangent.x,
      y: tangent.y,
      z: tangent.z,
    }, object.rotationX + object.spinX * timeSeconds, object.rotationY + object.spinY * timeSeconds, object.rotationZ + object.spinZ * timeSeconds));
    return {
      x: rotated.x,
      y: rotated.y,
      z: rotated.z,
      w: tangent.w,
    };
  }

  function sceneNormalizeDirection(point) {
    const length = Math.sqrt(
      sceneNumber(point && point.x, 0) * sceneNumber(point && point.x, 0) +
      sceneNumber(point && point.y, 0) * sceneNumber(point && point.y, 0) +
      sceneNumber(point && point.z, 0) * sceneNumber(point && point.z, 0)
    );
    if (length <= 0.000001) {
      return { x: 0, y: 1, z: 0 };
    }
    return {
      x: sceneNumber(point && point.x, 0) / length,
      y: sceneNumber(point && point.y, 0) / length,
      z: sceneNumber(point && point.z, 0) / length,
    };
  }

  function appendSceneMeshWireSegment(bundle, camera, width, height, fromWorld, toWorld, fromLighting, toLighting) {
    bundle.worldPositions.push(fromWorld.x, fromWorld.y, fromWorld.z, toWorld.x, toWorld.y, toWorld.z);
    bundle.worldColors.push(
      fromLighting[0], fromLighting[1], fromLighting[2], fromLighting[3],
      toLighting[0], toLighting[1], toLighting[2], toLighting[3],
    );
    const from = sceneProjectPoint(fromWorld, camera, width, height);
    const to = sceneProjectPoint(toWorld, camera, width, height);
    if (!from || !to) {
      return 2;
    }
    const stroke = sceneMixRGBA(fromLighting, toLighting);
    appendSceneLine(bundle, width, height, from, to, sceneRGBAString(stroke), 1.6);
    return 2;
  }

  function appendSceneMeshObjectToBundle(bundle, materialLookup, camera, width, height, object, lights, environment, timeSeconds) {
    const vertices = object && object.vertices;
    if (!vertices || !vertices.positions || !vertices.count) {
      return;
    }
    const material = sceneObjectMaterialProfile(object);
    const materialIndex = sceneBundleMaterialIndex(bundle, materialLookup, material);
    const wireVertexOffset = bundle.worldPositions.length / 3;
    const meshVertexOffset = bundle.worldMeshPositions.length / 3;
    let wireVertexCount = 0;
    let meshVertexCount = 0;
    let bounds = null;

    const points = _meshTrianglePoints;
    const positions = vertices.positions;
    for (let tri = 0; tri + 2 < vertices.count; tri += 3) {
      const triOffset = tri * 3;
      const tri0 = triOffset;
      const tri1 = triOffset + 3;
      const tri2 = triOffset + 6;
      translateScenePointInto(points[0], positions[tri0], positions[tri0 + 1], positions[tri0 + 2], object, timeSeconds);
      translateScenePointInto(points[1], positions[tri1], positions[tri1 + 1], positions[tri1 + 2], object, timeSeconds);
      translateScenePointInto(points[2], positions[tri2], positions[tri2 + 1], positions[tri2 + 2], object, timeSeconds);
      const normals = [
        sceneMeshWorldNormal(object, vertices, tri, timeSeconds),
        sceneMeshWorldNormal(object, vertices, tri + 1, timeSeconds),
        sceneMeshWorldNormal(object, vertices, tri + 2, timeSeconds),
      ];
      const lighting = [
        sceneLitColorRGBA(material, points[0], normals[0], lights, environment),
        sceneLitColorRGBA(material, points[1], normals[1], lights, environment),
        sceneLitColorRGBA(material, points[2], normals[2], lights, environment),
      ];
      const uvs = [
        sceneMeshVertexUV(vertices, tri),
        sceneMeshVertexUV(vertices, tri + 1),
        sceneMeshVertexUV(vertices, tri + 2),
      ];
      const tangents = [
        sceneMeshWorldTangent(object, vertices, tri, timeSeconds),
        sceneMeshWorldTangent(object, vertices, tri + 1, timeSeconds),
        sceneMeshWorldTangent(object, vertices, tri + 2, timeSeconds),
      ];

      for (let index = 0; index < 3; index += 1) {
        const point = points[index];
        const normal = normals[index];
        const uv = uvs[index];
        const tangent = tangents[index];
        const color = lighting[index];
        bundle.worldMeshPositions.push(point.x, point.y, point.z);
        bundle.worldMeshColors.push(color[0], color[1], color[2], color[3]);
        bundle.worldMeshNormals.push(normal.x, normal.y, normal.z);
        bundle.worldMeshUVs.push(uv.x, uv.y);
        bundle.worldMeshTangents.push(tangent.x, tangent.y, tangent.z, tangent.w);
        bounds = sceneExpandWorldBounds(bounds, point);
        meshVertexCount += 1;
      }

      wireVertexCount += appendSceneMeshWireSegment(bundle, camera, width, height, points[0], points[1], lighting[0], lighting[1]);
      wireVertexCount += appendSceneMeshWireSegment(bundle, camera, width, height, points[1], points[2], lighting[1], lighting[2]);
      wireVertexCount += appendSceneMeshWireSegment(bundle, camera, width, height, points[2], points[0], lighting[2], lighting[0]);
    }

    if (!bounds || meshVertexCount <= 0) {
      return;
    }
    const depth = sceneBoundsDepthMetrics(bounds, camera);
    const shared = {
      id: object.id,
      kind: object.kind,
      pickable: typeof object.pickable === "boolean" ? object.pickable : undefined,
      materialIndex: materialIndex,
      renderPass: sceneWorldObjectRenderPass(object, material),
      static: Boolean(object.static),
      castShadow: Boolean(object.castShadow),
      receiveShadow: Boolean(object.receiveShadow),
      depthWrite: object.depthWrite,
      bounds: bounds,
      depthNear: depth.near,
      depthFar: depth.far,
      depthCenter: depth.center,
      viewCulled: Boolean(object.viewCulled) || sceneBoundsViewCulled(bounds, camera),
      doubleSided: Boolean(object.doubleSided),
      skin: object.skin,
      vertices: vertices,
    };
    bundle.objects.push(Object.assign({}, shared, {
      vertexOffset: wireVertexOffset,
      vertexCount: wireVertexCount,
    }));
    bundle.meshObjects.push(Object.assign({}, shared, {
      vertexOffset: meshVertexOffset,
      vertexCount: meshVertexCount,
    }));
  }

  function sceneObjectHasTexturedSurface(object, material) {
    return Boolean(
      object &&
      object.kind === "plane" &&
      material &&
      typeof material.texture === "string" &&
      material.texture.trim() !== "",
    );
  }

  function appendSceneSurfaceToBundle(bundle, camera, object, materialIndex, material, bounds, depthMetrics, timeSeconds) {
    if (!sceneObjectHasTexturedSurface(object, material)) {
      return;
    }
    bundle.surfaces.push({
      id: object.id,
      kind: object.kind,
      materialIndex: materialIndex,
      renderPass: sceneWorldObjectRenderPass(object, material),
      static: Boolean(object.static),
      positions: scenePlaneSurfacePositions(scenePlaneSurfaceCorners(object, timeSeconds)),
      uv: scenePlaneSurfaceUVs(),
      vertexCount: 6,
      bounds: bounds,
      depthNear: depthMetrics.near,
      depthFar: depthMetrics.far,
      depthCenter: depthMetrics.center,
      viewCulled: Boolean(object.viewCulled) || sceneBoundsViewCulled(bounds, camera),
    });
  }

  function sceneLabelPoint(label, timeSeconds) {
    const offset = sceneLabelOffset(label, timeSeconds);
    return {
      x: label.x + offset.x,
      y: label.y + offset.y,
      z: label.z + offset.z,
    };
  }

  function sceneLabelOffset(label, timeSeconds) {
    if (!label || (!label.shiftX && !label.shiftY && !label.shiftZ)) {
      return { x: 0, y: 0, z: 0 };
    }
    const angle = sceneNumber(label.driftPhase, 0) + timeSeconds * sceneNumber(label.driftSpeed, 0);
    return {
      x: Math.cos(angle) * sceneNumber(label.shiftX, 0),
      y: Math.sin(angle * 0.82 + sceneNumber(label.driftPhase, 0) * 0.35) * sceneNumber(label.shiftY, 0),
      z: Math.sin(angle) * sceneNumber(label.shiftZ, 0),
    };
  }

  function sceneSpritePoint(sprite, timeSeconds) {
    const offset = sceneSpriteOffset(sprite, timeSeconds);
    return {
      x: sceneNumber(sprite && sprite.x, 0) + offset.x,
      y: sceneNumber(sprite && sprite.y, 0) + offset.y,
      z: sceneNumber(sprite && sprite.z, 0) + offset.z,
    };
  }

  function sceneSpriteOffset(sprite, timeSeconds) {
    if (!sprite || (!sprite.shiftX && !sprite.shiftY && !sprite.shiftZ)) {
      return { x: 0, y: 0, z: 0 };
    }
    const angle = sceneNumber(sprite.driftPhase, 0) + timeSeconds * sceneNumber(sprite.driftSpeed, 0);
    return {
      x: Math.cos(angle) * sceneNumber(sprite.shiftX, 0),
      y: Math.sin(angle * 0.82 + sceneNumber(sprite.driftPhase, 0) * 0.35) * sceneNumber(sprite.shiftY, 0),
      z: Math.sin(angle) * sceneNumber(sprite.shiftZ, 0),
    };
  }

  function sceneProjectedSpriteSize(camera, width, height, sprite, depth) {
    if (depth <= 0) {
      return { width: 0, height: 0 };
    }
    const normalizedCamera = sceneRenderCamera(camera);
    const focal = (Math.min(width, height) / 2) / Math.tan((normalizedCamera.fov * Math.PI) / 360);
    const scale = Math.max(0.05, sceneNumber(sprite && sprite.scale, 1));
    const worldWidth = Math.max(0.05, sceneNumber(sprite && sprite.width, 1.25));
    const worldHeight = Math.max(0.05, sceneNumber(sprite && sprite.height, worldWidth));
    return {
      width: Math.max(1, (worldWidth * scale * focal) / depth),
      height: Math.max(1, (worldHeight * scale * focal) / depth),
    };
  }

  function appendSceneLabelToBundle(bundle, camera, width, height, label, timeSeconds) {
    const point = sceneLabelPoint(label, timeSeconds);
    const projected = sceneProjectPoint(point, camera, width, height);
    if (!projected) {
      return;
    }

    const marginX = Math.max(24, sceneNumber(label.maxWidth, 180));
    const marginY = Math.max(24, sceneNumber(label.lineHeight, 18) * 2);
    if (projected.x < -marginX || projected.x > width + marginX || projected.y < -marginY || projected.y > height + marginY) {
      return;
    }

    bundle.labels.push({
      id: label.id,
      text: label.text,
      className: label.className,
      position: { x: projected.x, y: projected.y },
      depth: projected.depth,
      priority: sceneNumber(label.priority, 0),
      maxWidth: sceneNumber(label.maxWidth, 180),
      maxLines: Math.max(0, Math.floor(sceneNumber(label.maxLines, 0))),
      overflow: normalizeTextLayoutOverflow(label.overflow),
      font: label.font,
      lineHeight: sceneNumber(label.lineHeight, 18),
      color: label.color,
      background: label.background,
      borderColor: label.borderColor,
      offsetX: sceneNumber(label.offsetX, 0),
      offsetY: sceneNumber(label.offsetY, -14),
      anchorX: sceneNumber(label.anchorX, 0.5),
      anchorY: sceneNumber(label.anchorY, 1),
      collision: normalizeSceneLabelCollision(label.collision),
      occlude: Boolean(label.occlude),
      whiteSpace: normalizeSceneLabelWhiteSpace(label.whiteSpace),
      textAlign: normalizeSceneLabelAlign(label.textAlign),
    });
  }

  function appendSceneSpriteToBundle(bundle, camera, width, height, sprite, timeSeconds) {
    const point = sceneSpritePoint(sprite, timeSeconds);
    const projected = sceneProjectPoint(point, camera, width, height);
    if (!projected) {
      return;
    }
    const size = sceneProjectedSpriteSize(camera, width, height, sprite, projected.depth);
    if (size.width <= 0 || size.height <= 0) {
      return;
    }
    const marginX = Math.max(24, size.width);
    const marginY = Math.max(24, size.height);
    if (projected.x < -marginX || projected.x > width + marginX || projected.y < -marginY || projected.y > height + marginY) {
      return;
    }
    bundle.sprites.push({
      id: sprite.id,
      src: sprite.src,
      className: sprite.className,
      position: { x: projected.x, y: projected.y },
      depth: projected.depth,
      priority: sceneNumber(sprite.priority, 0),
      width: size.width,
      height: size.height,
      opacity: clamp01(sceneNumber(sprite.opacity, 1)),
      offsetX: sceneNumber(sprite.offsetX, 0),
      offsetY: sceneNumber(sprite.offsetY, 0),
      anchorX: sceneNumber(sprite.anchorX, 0.5),
      anchorY: sceneNumber(sprite.anchorY, 0.5),
      occlude: Boolean(sprite.occlude),
      fit: normalizeSceneSpriteFit(sprite.fit),
    });
  }

  function sceneExpandWorldBounds(bounds, point) {
    const next = bounds || {
      minX: point.x,
      minY: point.y,
      minZ: point.z,
      maxX: point.x,
      maxY: point.y,
      maxZ: point.z,
    };
    next.minX = Math.min(next.minX, point.x);
    next.minY = Math.min(next.minY, point.y);
    next.minZ = Math.min(next.minZ, point.z);
    next.maxX = Math.max(next.maxX, point.x);
    next.maxY = Math.max(next.maxY, point.y);
    next.maxZ = Math.max(next.maxZ, point.z);
    return next;
  }

  function appendSceneLine(bundle, width, height, from, to, color, lineWidth) {
    if (!from || !to) return;
    const rgba = sceneColorRGBA(color, [0.55, 0.88, 1, 1]);
    const fromClip = sceneClipPoint(from, width, height);
    const toClip = sceneClipPoint(to, width, height);
    bundle.lines.push({
      from: from,
      to: to,
      color: color,
      lineWidth: lineWidth,
    });
    bundle.positions.push(fromClip.x, fromClip.y, toClip.x, toClip.y);
    bundle.colors.push(rgba[0], rgba[1], rgba[2], rgba[3], rgba[0], rgba[1], rgba[2], rgba[3]);
  }

  function createSceneWebGLRenderer(canvas, options) {
    if (!canvas || typeof canvas.getContext !== "function") {
      return null;
    }
    const contextOptions = {
      alpha: false,
      antialias: !(options && options.antialias === false),
      powerPreference: options && options.powerPreference ? options.powerPreference : "high-performance",
      preserveDrawingBuffer: false,
    };
    const gl =
      canvas.getContext("webgl", contextOptions) ||
      canvas.getContext("experimental-webgl", contextOptions) ||
      canvas.getContext("webgl2", contextOptions);
    if (!gl) {
      return null;
    }

    const program = createSceneWebGLProgram(gl);
    const surfaceProgram = createSceneWebGLSurfaceProgram(gl);
    if (!program) {
      return null;
    }

    const resources = createSceneWebGLResources(gl, program, surfaceProgram);
    return {
      kind: "webgl",
      render(bundle) {
        const geometry = sceneWebGLBundleGeometry(bundle);
        prepareSceneWebGLFrame(gl, canvas, bundle, geometry.usePerspective, resources);
        if (!bundle) {
          return;
        }
        const worldRendered = geometry.usePerspective && renderSceneWebGLWorldBundle(gl, bundle, canvas, resources);
        if (worldRendered) {
          applySceneWebGLBlend(gl, "opaque", resources.stateCache);
          applySceneWebGLDepth(gl, "opaque", resources.stateCache);
          return;
        }
        if (geometry.vertexCount === 0 || !geometry.positions || !geometry.colors) {
          return;
        }
        gl.useProgram(program);
        applySceneWebGLUniforms(gl, bundle, canvas, geometry.usePerspective, resources);
        renderSceneWebGLFallbackBundle(gl, geometry, resources);
      },
      dispose() {
        disposeSceneWebGLRenderer(gl, program, resources);
      },
    };
  }

  function createSceneWebGLResources(gl, program, surfaceProgram) {
    const thickLineProgram = createSceneThickLineProgram(gl);
    return {
      program,
      surfaceProgram,
      fallbackBuffers: createSceneWebGLBufferSet(gl),
      passBuffers: {
        staticOpaque: createSceneWebGLBufferSet(gl),
        alpha: createSceneWebGLBufferSet(gl),
        additive: createSceneWebGLBufferSet(gl),
        dynamicOpaque: createSceneWebGLBufferSet(gl),
      },
      drawScratch: createSceneWorldDrawScratch(),
      thickLineProgram,
      thickLineBuffers: createSceneThickLineBufferSet(gl),
      thickLineScratch: createSceneThickLineScratch(),
      positionLocation: gl.getAttribLocation(program, "a_position"),
      colorLocation: gl.getAttribLocation(program, "a_color"),
      materialLocation: gl.getAttribLocation(program, "a_material"),
      cameraLocation: gl.getUniformLocation(program, "u_camera"),
      cameraRotationLocation: gl.getUniformLocation(program, "u_camera_rotation"),
      aspectLocation: gl.getUniformLocation(program, "u_aspect"),
      perspectiveLocation: gl.getUniformLocation(program, "u_use_perspective"),
      surfaceBuffers: createSceneWebGLSurfaceBufferSet(gl),
      surfacePositionLocation: surfaceProgram ? gl.getAttribLocation(surfaceProgram, "a_position") : -1,
      surfaceUVLocation: surfaceProgram ? gl.getAttribLocation(surfaceProgram, "a_uv") : -1,
      surfaceCameraLocation: surfaceProgram ? gl.getUniformLocation(surfaceProgram, "u_camera") : null,
      surfaceCameraRotationLocation: surfaceProgram ? gl.getUniformLocation(surfaceProgram, "u_camera_rotation") : null,
      surfaceAspectLocation: surfaceProgram ? gl.getUniformLocation(surfaceProgram, "u_aspect") : null,
      surfaceTintLocation: surfaceProgram ? gl.getUniformLocation(surfaceProgram, "u_tint") : null,
      surfaceEmissiveLocation: surfaceProgram ? gl.getUniformLocation(surfaceProgram, "u_emissive") : null,
      surfaceTextureLocation: surfaceProgram ? gl.getUniformLocation(surfaceProgram, "u_texture") : null,
      floatType: typeof gl.FLOAT === "number" ? gl.FLOAT : 0x1406,
      arrayBuffer: typeof gl.ARRAY_BUFFER === "number" ? gl.ARRAY_BUFFER : 0x8892,
      staticDraw: typeof gl.STATIC_DRAW === "number" ? gl.STATIC_DRAW : 0x88E4,
      dynamicDraw: typeof gl.DYNAMIC_DRAW === "number" ? gl.DYNAMIC_DRAW : 0x88E8,
      trianglesMode: typeof gl.TRIANGLES === "number" ? gl.TRIANGLES : 0x0004,
      colorBufferBit: typeof gl.COLOR_BUFFER_BIT === "number" ? gl.COLOR_BUFFER_BIT : 0x4000,
      depthBufferBit: typeof gl.DEPTH_BUFFER_BIT === "number" ? gl.DEPTH_BUFFER_BIT : 0x0100,
      linesMode: typeof gl.LINES === "number" ? gl.LINES : 0x0001,
      texture2D: typeof gl.TEXTURE_2D === "number" ? gl.TEXTURE_2D : 0x0DE1,
      texture0: typeof gl.TEXTURE0 === "number" ? gl.TEXTURE0 : 0x84C0,
      rgbaFormat: typeof gl.RGBA === "number" ? gl.RGBA : 0x1908,
      unsignedByte: typeof gl.UNSIGNED_BYTE === "number" ? gl.UNSIGNED_BYTE : 0x1401,
      linearFilter: typeof gl.LINEAR === "number" ? gl.LINEAR : 0x2601,
      clampToEdge: typeof gl.CLAMP_TO_EDGE === "number" ? gl.CLAMP_TO_EDGE : 0x812F,
      textureMinFilter: typeof gl.TEXTURE_MIN_FILTER === "number" ? gl.TEXTURE_MIN_FILTER : 0x2801,
      textureMagFilter: typeof gl.TEXTURE_MAG_FILTER === "number" ? gl.TEXTURE_MAG_FILTER : 0x2800,
      textureWrapS: typeof gl.TEXTURE_WRAP_S === "number" ? gl.TEXTURE_WRAP_S : 0x2802,
      textureWrapT: typeof gl.TEXTURE_WRAP_T === "number" ? gl.TEXTURE_WRAP_T : 0x2803,
      passCache: {
        staticOpaque: {
          key: "",
          vertexCount: 0,
        },
      },
      textureCache: new Map(),
      stateCache: {
        blendMode: "",
        depthMode: "",
      },
    };
  }

  function sceneWebGLBundleGeometry(bundle) {
    const hasWorldLines = Boolean(bundle && bundle.worldVertexCount > 0 && bundle.worldPositions && bundle.worldColors);
    const hasSurfaces = Boolean(bundle && Array.isArray(bundle.surfaces) && bundle.surfaces.length > 0);
    const usePerspective = hasWorldLines || hasSurfaces;
    return {
      usePerspective,
      positions: usePerspective ? bundle.worldPositions : bundle && bundle.positions,
      colors: usePerspective ? bundle.worldColors : bundle && bundle.colors,
      vertexCount: usePerspective ? bundle && bundle.worldVertexCount : bundle && bundle.vertexCount,
    };
  }

  function prepareSceneWebGLFrame(gl, canvas, bundle, usePerspective, resources) {
    const background = sceneColorRGBA(bundle && bundle.background, [0.03, 0.08, 0.12, 1]);
    gl.viewport(0, 0, canvas.width, canvas.height);
    gl.clearColor(background[0], background[1], background[2], background[3]);
    if (usePerspective && typeof gl.clearDepth === "function") {
      gl.clearDepth(1);
    }
    gl.clear(usePerspective ? resources.colorBufferBit | resources.depthBufferBit : resources.colorBufferBit);
  }

  function applySceneWebGLUniforms(gl, bundle, canvas, usePerspective, resources) {
    const aspect = Math.max(0.0001, canvas.width / Math.max(1, canvas.height));
    const camera = sceneRenderCamera(bundle && bundle.camera);
    if (typeof gl.uniform4f === "function" && resources.cameraLocation) {
      gl.uniform4f(
        resources.cameraLocation,
        camera.x,
        camera.y,
        camera.z,
        camera.fov,
      );
    }
    if (typeof gl.uniform3f === "function" && resources.cameraRotationLocation) {
      gl.uniform3f(
        resources.cameraRotationLocation,
        camera.rotationX,
        camera.rotationY,
        camera.rotationZ,
      );
    }
    if (typeof gl.uniform1f === "function" && resources.aspectLocation) {
      gl.uniform1f(resources.aspectLocation, aspect);
    }
    if (typeof gl.uniform1f === "function" && resources.perspectiveLocation) {
      gl.uniform1f(resources.perspectiveLocation, usePerspective ? 1 : 0);
    }
  }

  function renderSceneWebGLWorldBundle(gl, bundle, canvas, resources) {
    let drew = renderSceneWebGLSurfaces(gl, bundle, canvas, resources, "opaque");
    drew = renderSceneWebGLMeshWorldBundle(gl, bundle, canvas, resources) || drew;

    if (sceneBundleNeedsThickLines(bundle) && resources.thickLineProgram) {
      const thickDrew = drawSceneThickLines(gl, bundle, canvas, resources);
      if (thickDrew) {
        gl.useProgram(resources.program);
        applySceneWebGLUniforms(gl, bundle, canvas, true, resources);
        drew = renderSceneWebGLSurfaces(gl, bundle, canvas, resources, "alpha") || drew || true;
        drew = renderSceneWebGLSurfaces(gl, bundle, canvas, resources, "additive") || drew;
        return true;
      }
    }

    gl.useProgram(resources.program);
    applySceneWebGLUniforms(gl, bundle, canvas, true, resources);
    if (sceneBundleCanUseBundledPasses(bundle)) {
      const bundledPasses = createSceneWorldWebGLPassesFromBundle(bundle, resources.passBuffers, {
        staticDraw: resources.staticDraw,
        dynamicDraw: resources.dynamicDraw,
      });
      if (bundledPasses.length > 0) {
        drawSceneWebGLPasses(gl, resources.arrayBuffer, resources.floatType, resources.linesMode, resources.positionLocation, resources.colorLocation, resources.materialLocation, bundledPasses, resources.passCache, resources.stateCache);
        drew = true;
        drew = renderSceneWebGLSurfaces(gl, bundle, canvas, resources, "alpha") || drew;
        drew = renderSceneWebGLSurfaces(gl, bundle, canvas, resources, "additive") || drew;
        return true;
      }
    }
    const drawPlan = buildSceneWorldDrawPlan(bundle, resources.drawScratch);
    if (!drawPlan) {
      drew = renderSceneWebGLSurfaces(gl, bundle, canvas, resources, "alpha") || drew;
      drew = renderSceneWebGLSurfaces(gl, bundle, canvas, resources, "additive") || drew;
      return drew;
    }
    const worldPasses = createSceneWorldWebGLPasses(drawPlan, resources.passBuffers, {
      staticDraw: resources.staticDraw,
      dynamicDraw: resources.dynamicDraw,
    });
    drawSceneWebGLPasses(gl, resources.arrayBuffer, resources.floatType, resources.linesMode, resources.positionLocation, resources.colorLocation, resources.materialLocation, worldPasses, resources.passCache, resources.stateCache);
    drew = true;
    drew = renderSceneWebGLSurfaces(gl, bundle, canvas, resources, "alpha") || drew;
    drew = renderSceneWebGLSurfaces(gl, bundle, canvas, resources, "additive") || drew;
    return true;
  }

  function renderSceneWebGLMeshWorldBundle(gl, bundle, canvas, resources) {
    const meshObjects = Array.isArray(bundle && bundle.meshObjects) ? bundle.meshObjects : [];
    if (!meshObjects.length || !bundle || !bundle.worldMeshPositions || !bundle.worldMeshColors) {
      return false;
    }
    const opaque = [];
    const alpha = [];
    const additive = [];
    for (let index = 0; index < meshObjects.length; index += 1) {
      const object = meshObjects[index];
      if (!sceneWorldObjectRenderable(object, bundle.camera)) {
        continue;
      }
      const material = Array.isArray(bundle.materials) ? bundle.materials[object.materialIndex] || null : null;
      const renderPass = sceneWorldObjectRenderPass(object, material);
      const entry = {
        object,
        material,
        order: index,
        depth: sceneNumber(object && object.depthCenter, 0),
      };
      if (renderPass === "alpha") {
        alpha.push(entry);
        continue;
      }
      if (renderPass === "additive") {
        additive.push(entry);
        continue;
      }
      opaque.push(entry);
    }
    if (!opaque.length && !alpha.length && !additive.length) {
      return false;
    }
    gl.useProgram(resources.program);
    applySceneWebGLUniforms(gl, bundle, canvas, true, resources);
    let drew = false;
    drew = renderSceneWebGLMeshWorldPass(gl, bundle, resources, opaque, "opaque", "opaque") || drew;
    drew = renderSceneWebGLMeshWorldPass(gl, bundle, resources, alpha, "alpha", "translucent") || drew;
    drew = renderSceneWebGLMeshWorldPass(gl, bundle, resources, additive, "additive", "translucent") || drew;
    return drew;
  }

  function renderSceneWebGLMeshWorldPass(gl, bundle, resources, entries, blendMode, depthMode) {
    if (!Array.isArray(entries) || !entries.length) {
      return false;
    }
    if (blendMode !== "opaque") {
      entries.sort(compareSceneWorldPassEntries);
    }
    applySceneWebGLDepth(gl, depthMode, resources.stateCache);
    applySceneWebGLBlend(gl, blendMode, resources.stateCache);
    let drew = false;
    for (const entry of entries) {
      drew = renderSceneWebGLMeshObject(gl, bundle, resources, entry.object, entry.material) || drew;
    }
    return drew;
  }

  function renderSceneWebGLMeshObject(gl, bundle, resources, object, material) {
    const vertexOffset = Math.max(0, Math.floor(sceneNumber(object && object.vertexOffset, 0)));
    const vertexCount = Math.max(0, Math.floor(sceneNumber(object && object.vertexCount, 0)));
    if (!vertexCount) {
      return false;
    }
    const positions = sceneSliceFloatArray(bundle.worldMeshPositions, vertexOffset * 3, vertexCount * 3);
    const colors = sceneSliceFloatArray(bundle.worldMeshColors, vertexOffset * 4, vertexCount * 4);
    const materials = sceneMeshMaterialArray(vertexCount, material);
    uploadSceneWebGLBuffers(
      gl,
      resources.arrayBuffer,
      object && object.static ? resources.staticDraw : resources.dynamicDraw,
      resources.fallbackBuffers.position,
      resources.fallbackBuffers.color,
      resources.fallbackBuffers.material,
      positions,
      colors,
      materials,
    );
    drawSceneWebGLPrimitives(
      gl,
      resources.arrayBuffer,
      resources.floatType,
      resources.trianglesMode,
      resources.positionLocation,
      resources.colorLocation,
      resources.materialLocation,
      resources.fallbackBuffers.position,
      resources.fallbackBuffers.color,
      resources.fallbackBuffers.material,
      vertexCount,
      3,
    );
    return true;
  }

  function sceneSliceFloatArray(values, start, count) {
    const safeStart = Math.max(0, Math.floor(sceneNumber(start, 0)));
    const safeCount = Math.max(0, Math.floor(sceneNumber(count, 0)));
    const typed = new Float32Array(safeCount);
    for (let index = 0; index < safeCount; index += 1) {
      typed[index] = sceneNumber(values && values[safeStart + index], 0);
    }
    return typed;
  }

  function sceneMeshMaterialArray(vertexCount, material) {
    const data = sceneMaterialShaderData(material);
    const typed = new Float32Array(Math.max(0, vertexCount) * 3);
    for (let index = 0; index < vertexCount; index += 1) {
      const offset = index * 3;
      typed[offset] = data[0];
      typed[offset + 1] = data[1];
      typed[offset + 2] = data[2];
    }
    return typed;
  }

  function sceneBundleCanUseBundledPasses(bundle) {
    if (!bundle || !Array.isArray(bundle.passes) || bundle.passes.length === 0) {
      return false;
    }
    if (!bundle.sourceCamera) {
      return true;
    }
    return sceneCameraEquivalent(bundle.sourceCamera, bundle.camera);
  }

  function renderSceneWebGLSurfaces(gl, bundle, canvas, resources, renderPass) {
    const surfaces = sceneBundleSurfaceEntries(bundle, renderPass);
    if (!surfaces.length || !resources.surfaceProgram) {
      return false;
    }
    gl.useProgram(resources.surfaceProgram);
    applySceneWebGLSurfaceUniforms(gl, bundle, canvas, resources);
    applySceneWebGLBlend(gl, renderPass === "additive" ? "additive" : (renderPass === "alpha" ? "alpha" : "opaque"), resources.stateCache);
    applySceneWebGLDepth(gl, renderPass === "opaque" ? "opaque" : "translucent", resources.stateCache);
    for (const entry of surfaces) {
      const material = bundle.materials[entry.materialIndex] || null;
      const textureRecord = sceneWebGLTextureRecord(gl, resources, material && material.texture);
      if (!textureRecord || !textureRecord.texture) {
        continue;
      }
      uploadSceneWebGLSurfaceBuffers(gl, resources, entry);
      bindSceneWebGLSurfaceTexture(gl, resources, textureRecord);
      applySceneWebGLSurfaceMaterial(gl, resources, material);
      drawSceneWebGLSurface(gl, resources, entry.vertexCount);
    }
    return true;
  }

  function sceneBundleSurfaceEntries(bundle, renderPass) {
    const surfaces = Array.isArray(bundle && bundle.surfaces) ? bundle.surfaces.slice() : [];
    const filtered = surfaces.filter(function(surface) {
      return surface &&
        !surface.viewCulled &&
        Math.max(0, Math.floor(sceneNumber(surface.vertexCount, 0))) > 0 &&
        String(surface.renderPass || "opaque") === renderPass;
    });
    if (renderPass !== "opaque") {
      filtered.sort(function(left, right) {
        if (sceneNumber(left.depthCenter, 0) !== sceneNumber(right.depthCenter, 0)) {
          return sceneNumber(right.depthCenter, 0) - sceneNumber(left.depthCenter, 0);
        }
        return String(left.id || "").localeCompare(String(right.id || ""));
      });
    }
    return filtered;
  }

  function applySceneWebGLSurfaceUniforms(gl, bundle, canvas, resources) {
    const aspect = Math.max(0.0001, canvas.width / Math.max(1, canvas.height));
    const camera = sceneRenderCamera(bundle && bundle.camera);
    if (typeof gl.uniform4f === "function" && resources.surfaceCameraLocation) {
      gl.uniform4f(resources.surfaceCameraLocation, camera.x, camera.y, camera.z, camera.fov);
    }
    if (typeof gl.uniform3f === "function" && resources.surfaceCameraRotationLocation) {
      gl.uniform3f(resources.surfaceCameraRotationLocation, camera.rotationX, camera.rotationY, camera.rotationZ);
    }
    if (typeof gl.uniform1f === "function" && resources.surfaceAspectLocation) {
      gl.uniform1f(resources.surfaceAspectLocation, aspect);
    }
  }

  function uploadSceneWebGLSurfaceBuffers(gl, resources, surface) {
    gl.bindBuffer(resources.arrayBuffer, resources.surfaceBuffers.position);
    gl.bufferData(resources.arrayBuffer, sceneTypedFloatArray(surface && surface.positions), resources.dynamicDraw);
    gl.bindBuffer(resources.arrayBuffer, resources.surfaceBuffers.uv);
    gl.bufferData(resources.arrayBuffer, sceneTypedFloatArray(surface && surface.uv), resources.dynamicDraw);
  }

  function bindSceneWebGLSurfaceTexture(gl, resources, record) {
    if (typeof gl.activeTexture === "function") {
      gl.activeTexture(resources.texture0);
    }
    if (typeof gl.bindTexture === "function") {
      gl.bindTexture(resources.texture2D, record.texture);
    }
    if (typeof gl.uniform1i === "function" && resources.surfaceTextureLocation) {
      gl.uniform1i(resources.surfaceTextureLocation, 0);
    }
  }

  function applySceneWebGLSurfaceMaterial(gl, resources, material) {
    const tint = sceneColorRGBA(material && material.color, [1, 1, 1, 1]);
    tint[3] = clamp01(tint[3] * sceneMaterialOpacity(material));
    if (typeof gl.uniform4f === "function" && resources.surfaceTintLocation) {
      gl.uniform4f(resources.surfaceTintLocation, tint[0], tint[1], tint[2], tint[3]);
    }
    if (typeof gl.uniform1f === "function" && resources.surfaceEmissiveLocation) {
      gl.uniform1f(resources.surfaceEmissiveLocation, sceneMaterialEmissive(material));
    }
  }

  function drawSceneWebGLSurface(gl, resources, vertexCount) {
    if (!vertexCount) {
      return;
    }
    gl.bindBuffer(resources.arrayBuffer, resources.surfaceBuffers.position);
    gl.enableVertexAttribArray(resources.surfacePositionLocation);
    gl.vertexAttribPointer(resources.surfacePositionLocation, 3, resources.floatType, false, 0, 0);
    gl.bindBuffer(resources.arrayBuffer, resources.surfaceBuffers.uv);
    gl.enableVertexAttribArray(resources.surfaceUVLocation);
    gl.vertexAttribPointer(resources.surfaceUVLocation, 2, resources.floatType, false, 0, 0);
    gl.drawArrays(resources.trianglesMode, 0, vertexCount);
  }

  function sceneWebGLTextureRecord(gl, resources, src) {
    const key = typeof src === "string" ? src.trim() : "";
    if (!key || !resources || !resources.textureCache) {
      return null;
    }
    if (resources.textureCache.has(key)) {
      return resources.textureCache.get(key);
    }
    const texture = typeof gl.createTexture === "function" ? gl.createTexture() : null;
    const record = { texture, src: key, loaded: false };
    resources.textureCache.set(key, record);
    if (!texture) {
      return record;
    }
    initializeSceneWebGLTexture(gl, resources, texture);
    const image = createSceneWebGLImage();
    if (!image) {
      return record;
    }
    image.onload = function() {
      uploadSceneWebGLTextureImage(gl, resources, texture, image);
      record.loaded = true;
    };
    image.onerror = function() {
      record.failed = true;
    };
    image.src = key;
    return record;
  }

  function createSceneWebGLImage() {
    if (typeof Image === "function") {
      return new Image();
    }
    return null;
  }

  function initializeSceneWebGLTexture(gl, resources, texture) {
    if (typeof gl.bindTexture !== "function" || typeof gl.texImage2D !== "function") {
      return;
    }
    gl.bindTexture(resources.texture2D, texture);
    if (typeof gl.texParameteri === "function") {
      gl.texParameteri(resources.texture2D, resources.textureMinFilter, resources.linearFilter);
      gl.texParameteri(resources.texture2D, resources.textureMagFilter, resources.linearFilter);
      gl.texParameteri(resources.texture2D, resources.textureWrapS, resources.clampToEdge);
      gl.texParameteri(resources.texture2D, resources.textureWrapT, resources.clampToEdge);
    }
    gl.texImage2D(resources.texture2D, 0, resources.rgbaFormat, 1, 1, 0, resources.rgbaFormat, resources.unsignedByte, new Uint8Array([255, 255, 255, 255]));
  }

  function uploadSceneWebGLTextureImage(gl, resources, texture, image) {
    if (typeof gl.bindTexture !== "function" || typeof gl.texImage2D !== "function") {
      return;
    }
    gl.bindTexture(resources.texture2D, texture);
    if (typeof gl.texParameteri === "function") {
      gl.texParameteri(resources.texture2D, resources.textureMinFilter, resources.linearFilter);
      gl.texParameteri(resources.texture2D, resources.textureMagFilter, resources.linearFilter);
      gl.texParameteri(resources.texture2D, resources.textureWrapS, resources.clampToEdge);
      gl.texParameteri(resources.texture2D, resources.textureWrapT, resources.clampToEdge);
    }
    gl.texImage2D(resources.texture2D, 0, resources.rgbaFormat, resources.rgbaFormat, resources.unsignedByte, image);
  }

  function renderSceneWebGLFallbackBundle(gl, geometry, resources) {
    applySceneWebGLDepth(gl, "disabled", resources.stateCache);
    applySceneWebGLBlend(gl, "opaque", resources.stateCache);
    uploadSceneWebGLBuffers(
      gl,
      resources.arrayBuffer,
      resources.dynamicDraw,
      resources.fallbackBuffers.position,
      resources.fallbackBuffers.color,
      resources.fallbackBuffers.material,
      geometry.positions,
      geometry.colors,
      sceneFallbackMaterialData(geometry.vertexCount),
    );
    drawSceneWebGLLines(
      gl,
      resources.arrayBuffer,
      resources.floatType,
      resources.linesMode,
      resources.positionLocation,
      resources.colorLocation,
      resources.materialLocation,
      resources.fallbackBuffers.position,
      resources.fallbackBuffers.color,
      resources.fallbackBuffers.material,
      geometry.vertexCount,
      geometry.usePerspective ? 3 : 2,
    );
  }

  function disposeSceneWebGLRenderer(gl, program, resources) {
    if (typeof gl.deleteBuffer === "function") {
      deleteSceneWebGLBufferSet(gl, resources.fallbackBuffers);
      deleteSceneWebGLBufferSet(gl, resources.passBuffers.staticOpaque);
      deleteSceneWebGLBufferSet(gl, resources.passBuffers.alpha);
      deleteSceneWebGLBufferSet(gl, resources.passBuffers.additive);
      deleteSceneWebGLBufferSet(gl, resources.passBuffers.dynamicOpaque);
      deleteSceneWebGLSurfaceBufferSet(gl, resources.surfaceBuffers);
    }
    if (resources && resources.textureCache && typeof gl.deleteTexture === "function") {
      for (const record of resources.textureCache.values()) {
        if (record && record.texture) {
          gl.deleteTexture(record.texture);
        }
      }
    }
    if (typeof gl.deleteProgram === "function") {
      gl.deleteProgram(program);
      if (resources && resources.surfaceProgram) {
        gl.deleteProgram(resources.surfaceProgram);
      }
    }
  }

  function createSceneWebGLBufferSet(gl) {
    return {
      position: gl.createBuffer(),
      color: gl.createBuffer(),
      material: gl.createBuffer(),
    };
  }

  function createSceneWebGLSurfaceBufferSet(gl) {
    return {
      position: gl.createBuffer(),
      uv: gl.createBuffer(),
    };
  }

  function deleteSceneWebGLBufferSet(gl, buffers) {
    if (!buffers) {
      return;
    }
    gl.deleteBuffer(buffers.position);
    gl.deleteBuffer(buffers.color);
    gl.deleteBuffer(buffers.material);
  }

  function deleteSceneWebGLSurfaceBufferSet(gl, buffers) {
    if (!buffers) {
      return;
    }
    gl.deleteBuffer(buffers.position);
    gl.deleteBuffer(buffers.uv);
  }

  function createSceneWorldWebGLPasses(drawPlan, buffers, usages) {
    const passes = [];
    passes.push({
      name: "staticOpaque",
      blend: "opaque",
      depth: "opaque",
      usage: usages.staticDraw,
      cacheSlot: "staticOpaque",
      cacheKey: drawPlan.staticOpaqueKey,
      buffers: buffers.staticOpaque,
      positions: drawPlan.staticOpaquePositions,
      colors: drawPlan.staticOpaqueColors,
      materials: drawPlan.staticOpaqueMaterials,
      vertexCount: drawPlan.staticOpaqueVertexCount,
    });
    passes.push({
      name: "dynamicOpaque",
      blend: "opaque",
      depth: "opaque",
      usage: usages.dynamicDraw,
      buffers: buffers.dynamicOpaque,
      positions: drawPlan.dynamicOpaquePositions,
      colors: drawPlan.dynamicOpaqueColors,
      materials: drawPlan.dynamicOpaqueMaterials,
      vertexCount: drawPlan.dynamicOpaqueVertexCount,
    });
    if (drawPlan.hasAlphaPass) {
      passes.push({
        name: "alpha",
        blend: "alpha",
        depth: "translucent",
        usage: usages.dynamicDraw,
        buffers: buffers.alpha,
        positions: drawPlan.alphaPositions,
        colors: drawPlan.alphaColors,
        materials: drawPlan.alphaMaterials,
        vertexCount: drawPlan.alphaVertexCount,
      });
    }
    if (drawPlan.hasAdditivePass) {
      passes.push({
        name: "additive",
        blend: "additive",
        depth: "translucent",
        usage: usages.dynamicDraw,
        buffers: buffers.additive,
        positions: drawPlan.additivePositions,
        colors: drawPlan.additiveColors,
        materials: drawPlan.additiveMaterials,
        vertexCount: drawPlan.additiveVertexCount,
      });
    }
    return passes;
  }

  function createSceneWorldWebGLPassesFromBundle(bundle, buffers, usages) {
    const sourcePasses = Array.isArray(bundle && bundle.passes) ? bundle.passes : [];
    const passes = [];
    for (const source of sourcePasses) {
      const pass = sceneWorldWebGLPassFromSource(source, buffers, usages);
      if (pass) {
        passes.push(pass);
      }
    }
    return passes;
  }

  function sceneWorldWebGLPassFromSource(source, buffers, usages) {
    const name = sceneWorldWebGLPassName(source);
    if (!name) {
      return null;
    }
    const targetBuffers = buffers[name];
    if (!targetBuffers) {
      return null;
    }
    const isStatic = Boolean(source && source.static);
    const positions = sceneTypedFloatArray(source && source.positions);
    const colors = sceneTypedFloatArray(source && source.colors);
    const materials = sceneTypedFloatArray(source && source.materials);
    return {
      name,
      blend: sceneWorldWebGLPassMode(source && source.blend, "opaque"),
      depth: sceneWorldWebGLPassMode(source && source.depth, "opaque"),
      usage: isStatic ? usages.staticDraw : usages.dynamicDraw,
      cacheSlot: sceneWorldWebGLPassCacheSlot(name, isStatic),
      cacheKey: String(source && source.cacheKey || ""),
      buffers: targetBuffers,
      positions,
      colors,
      materials,
      vertexCount: sceneWorldWebGLPassVertexCount(source, positions, colors, materials),
    };
  }

  function sceneWorldWebGLPassName(source) {
    return String(source && source.name || "");
  }

  function sceneWorldWebGLPassMode(value, fallback) {
    const mode = String(value || fallback);
    return mode || fallback;
  }

  function sceneWorldWebGLPassCacheSlot(name, isStatic) {
    if (!isStatic) {
      return "";
    }
    return name;
  }

  function sceneWorldWebGLPassVertexCount(source, positions, colors, materials) {
    const requested = Math.max(0, Math.floor(sceneNumber(source && source.vertexCount, NaN)));
    const positionCount = Math.floor((positions && positions.length || 0) / 3);
    const colorCount = Math.floor((colors && colors.length || 0) / 4);
    const materialCount = Math.floor((materials && materials.length || 0) / 3);
    const maxCount = Math.max(0, Math.min(positionCount, colorCount, materialCount));
    if (Number.isFinite(requested)) {
      return Math.min(requested, maxCount);
    }
    return maxCount;
  }

  function drawSceneWebGLPasses(gl, arrayBuffer, floatType, linesMode, positionLocation, colorLocation, materialLocation, passes, cache, stateCache) {
    for (const pass of passes) {
      const vertexCount = uploadSceneWebGLPass(gl, arrayBuffer, pass, cache);
      if (!vertexCount) {
        continue;
      }
      applySceneWebGLDepth(gl, pass.depth, stateCache);
      applySceneWebGLBlend(gl, pass.blend, stateCache);
      drawSceneWebGLLines(gl, arrayBuffer, floatType, linesMode, positionLocation, colorLocation, materialLocation, pass.buffers.position, pass.buffers.color, pass.buffers.material, vertexCount, 3);
    }
  }

  function uploadSceneWebGLPass(gl, arrayBuffer, pass, cache) {
    if (!pass || !pass.buffers) {
      return 0;
    }
    if (pass.cacheSlot) {
      const record = cache[pass.cacheSlot] || (cache[pass.cacheSlot] = { key: "", vertexCount: 0 });
      if (record.key !== pass.cacheKey) {
        uploadSceneWebGLBuffers(gl, arrayBuffer, pass.usage, pass.buffers.position, pass.buffers.color, pass.buffers.material, pass.positions, pass.colors, pass.materials);
        record.key = pass.cacheKey;
        record.vertexCount = pass.vertexCount;
      }
      return record.vertexCount;
    }
    if (!pass.vertexCount) {
      return 0;
    }
    uploadSceneWebGLBuffers(gl, arrayBuffer, pass.usage, pass.buffers.position, pass.buffers.color, pass.buffers.material, pass.positions, pass.colors, pass.materials);
    return pass.vertexCount;
  }

  function uploadSceneWebGLBuffers(gl, arrayBuffer, usage, positionBuffer, colorBuffer, materialBuffer, positions, colors, materials) {
    gl.bindBuffer(arrayBuffer, positionBuffer);
    gl.bufferData(arrayBuffer, positions, usage);
    gl.bindBuffer(arrayBuffer, colorBuffer);
    gl.bufferData(arrayBuffer, colors, usage);
    gl.bindBuffer(arrayBuffer, materialBuffer);
    gl.bufferData(arrayBuffer, materials, usage);
  }

  function drawSceneWebGLLines(gl, arrayBuffer, floatType, linesMode, positionLocation, colorLocation, materialLocation, positionBuffer, colorBuffer, materialBuffer, vertexCount, positionSize) {
    drawSceneWebGLPrimitives(gl, arrayBuffer, floatType, linesMode, positionLocation, colorLocation, materialLocation, positionBuffer, colorBuffer, materialBuffer, vertexCount, positionSize);
  }

  function drawSceneWebGLPrimitives(gl, arrayBuffer, floatType, drawMode, positionLocation, colorLocation, materialLocation, positionBuffer, colorBuffer, materialBuffer, vertexCount, positionSize) {
    if (!vertexCount) {
      return;
    }
    gl.bindBuffer(arrayBuffer, positionBuffer);
    gl.enableVertexAttribArray(positionLocation);
    gl.vertexAttribPointer(positionLocation, positionSize, floatType, false, 0, 0);

    gl.bindBuffer(arrayBuffer, colorBuffer);
    gl.enableVertexAttribArray(colorLocation);
    gl.vertexAttribPointer(colorLocation, 4, floatType, false, 0, 0);

    gl.bindBuffer(arrayBuffer, materialBuffer);
    gl.enableVertexAttribArray(materialLocation);
    gl.vertexAttribPointer(materialLocation, 3, floatType, false, 0, 0);

    gl.drawArrays(drawMode, 0, vertexCount);
  }

  function sceneTypedFloatArray(values) {
    if (values instanceof Float32Array) {
      return values;
    }
    const list = Array.isArray(values) ? values : [];
    const typed = new Float32Array(list.length);
    for (let i = 0; i < list.length; i += 1) {
      typed[i] = sceneNumber(list[i], 0);
    }
    return typed;
  }

  function applySceneWebGLBlend(gl, mode, stateCache) {
    if (sceneWebGLStateUnchanged(stateCache, "blendMode", mode)) {
      return;
    }
    const blendConst = typeof gl.BLEND === "number" ? gl.BLEND : 0x0BE2;
    const one = typeof gl.ONE === "number" ? gl.ONE : 1;
    const srcAlpha = typeof gl.SRC_ALPHA === "number" ? gl.SRC_ALPHA : 0x0302;
    const oneMinusSrcAlpha = typeof gl.ONE_MINUS_SRC_ALPHA === "number" ? gl.ONE_MINUS_SRC_ALPHA : 0x0303;
    const config = sceneWebGLBlendConfig(mode, srcAlpha, oneMinusSrcAlpha, one);
    rememberSceneWebGLState(stateCache, "blendMode", mode);
    setSceneWebGLCapability(gl, blendConst, config.enabled);
    if (config.enabled && typeof gl.blendFunc === "function") {
      gl.blendFunc(config.src, config.dst);
    }
  }

  function applySceneWebGLDepth(gl, mode, stateCache) {
    if (sceneWebGLStateUnchanged(stateCache, "depthMode", mode)) {
      return;
    }
    const depthTest = typeof gl.DEPTH_TEST === "number" ? gl.DEPTH_TEST : 0x0B71;
    const lequal = typeof gl.LEQUAL === "number" ? gl.LEQUAL : 0x0203;
    const config = sceneWebGLDepthConfig(mode);
    rememberSceneWebGLState(stateCache, "depthMode", mode);
    setSceneWebGLCapability(gl, depthTest, config.enabled);
    if (!config.enabled) {
      return;
    }
    if (typeof gl.depthFunc === "function") {
      gl.depthFunc(lequal);
    }
    if (typeof gl.depthMask === "function") {
      gl.depthMask(config.mask);
    }
  }

  function sceneWebGLStateUnchanged(stateCache, key, mode) {
    return Boolean(stateCache && stateCache[key] === mode);
  }

  function rememberSceneWebGLState(stateCache, key, mode) {
    if (!stateCache) {
      return;
    }
    stateCache[key] = mode;
  }

  function setSceneWebGLCapability(gl, capability, enabled) {
    if (enabled) {
      if (typeof gl.enable === "function") {
        gl.enable(capability);
      }
      return;
    }
    if (typeof gl.disable === "function") {
      gl.disable(capability);
    }
  }

  function sceneWebGLBlendConfig(mode, srcAlpha, oneMinusSrcAlpha, one) {
    switch (mode) {
    case "alpha":
      return { enabled: true, src: srcAlpha, dst: oneMinusSrcAlpha };
    case "additive":
      return { enabled: true, src: srcAlpha, dst: one };
    default:
      return { enabled: false };
    }
  }

  function sceneWebGLDepthConfig(mode) {
    switch (mode) {
    case "opaque":
      return { enabled: true, mask: true };
    case "translucent":
      return { enabled: true, mask: false };
    default:
      return { enabled: false, mask: false };
    }
  }

  function createSceneWebGLProgram(gl) {
    const vertexSource = [
      "attribute vec3 a_position;",
      "attribute vec4 a_color;",
      "attribute vec3 a_material;",
      "uniform vec4 u_camera;",
      "uniform vec3 u_camera_rotation;",
      "uniform float u_aspect;",
      "uniform float u_use_perspective;",
      "varying vec4 v_color;",
      "varying vec3 v_material;",
      "vec3 inverseRotatePoint(vec3 point, vec3 rotation) {",
      "  float sinZ = sin(-rotation.z);",
      "  float cosZ = cos(-rotation.z);",
      "  float nextX = point.x * cosZ - point.y * sinZ;",
      "  float nextY = point.x * sinZ + point.y * cosZ;",
      "  point = vec3(nextX, nextY, point.z);",
      "  float sinY = sin(-rotation.y);",
      "  float cosY = cos(-rotation.y);",
      "  nextX = point.x * cosY + point.z * sinY;",
      "  float nextZ = -point.x * sinY + point.z * cosY;",
      "  point = vec3(nextX, point.y, nextZ);",
      "  float sinX = sin(-rotation.x);",
      "  float cosX = cos(-rotation.x);",
      "  nextY = point.y * cosX - point.z * sinX;",
      "  nextZ = point.y * sinX + point.z * cosX;",
      "  return vec3(point.x, nextY, nextZ);",
      "}",
      "void main() {",
      "  vec4 clip = vec4(a_position.xy, 0.0, 1.0);",
      "  if (u_use_perspective > 0.5) {",
      "    vec3 local = inverseRotatePoint(vec3(a_position.x - u_camera.x, a_position.y - u_camera.y, a_position.z + u_camera.z), u_camera_rotation);",
      "    float depth = local.z;",
      "    if (depth <= 0.001) {",
      "      clip = vec4(2.0, 2.0, 0.0, 1.0);",
      "    } else {",
      "      float focal = 1.0 / tan(radians(u_camera.w) * 0.5);",
      "      vec2 projected = vec2(local.x * focal / depth, local.y * focal / depth);",
      "      projected.x /= max(u_aspect, 0.0001);",
      "      float clipDepth = clamp(depth / 128.0, 0.0, 1.0) * 2.0 - 1.0;",
      "      clip = vec4(projected, clipDepth, 1.0);",
      "    }",
      "  }",
      "  gl_Position = clip;",
      "  v_color = a_color;",
      "  v_material = a_material;",
      "}",
    ].join("\n");
    const fragmentSource = [
      "precision mediump float;",
      "varying vec4 v_color;",
      "varying vec3 v_material;",
      "void main() {",
      "  vec4 color = v_color;",
      "  float kind = floor(v_material.x + 0.5);",
      "  float emissive = max(v_material.y, 0.0);",
      "  float tone = clamp(v_material.z, 0.0, 1.0);",
      "  if (kind > 3.5) {",
      "    color.rgb *= mix(0.78, 1.0, tone);",
      "  } else if (kind > 2.5) {",
      "    color.rgb *= 1.0 + emissive * 0.75;",
      "  } else if (kind > 1.5) {",
      "    color.rgb = mix(color.rgb, vec3(0.92, 0.98, 1.0), 0.28 + tone * 0.16);",
      "    color.a *= 0.84;",
      "  } else if (kind > 0.5) {",
      "    color.rgb = mix(color.rgb, vec3(0.84, 0.94, 1.0), 0.18 + tone * 0.12);",
      "    color.a *= 0.9;",
      "  } else {",
      "    color.rgb *= mix(0.9, 1.0, tone);",
      "  }",
      "  gl_FragColor = vec4(clamp(color.rgb, 0.0, 1.0), clamp(color.a, 0.0, 1.0));",
      "}",
    ].join("\n");

    const vertexShader = createSceneShader(gl, gl.VERTEX_SHADER, vertexSource);
    const fragmentShader = createSceneShader(gl, gl.FRAGMENT_SHADER, fragmentSource);
    if (!vertexShader || !fragmentShader) {
      return null;
    }

    const program = gl.createProgram();
    gl.attachShader(program, vertexShader);
    gl.attachShader(program, fragmentShader);
    gl.linkProgram(program);
    if (!gl.getProgramParameter(program, gl.LINK_STATUS)) {
      console.warn("[gosx] Scene3D WebGL link failed");
      return null;
    }
    return program;
  }

  function createSceneWebGLSurfaceProgram(gl) {
    const vertexSource = [
      "attribute vec3 a_position;",
      "attribute vec2 a_uv;",
      "uniform vec4 u_camera;",
      "uniform vec3 u_camera_rotation;",
      "uniform float u_aspect;",
      "varying vec2 v_uv;",
      "vec3 inverseRotatePoint(vec3 point, vec3 rotation) {",
      "  float sinZ = sin(-rotation.z);",
      "  float cosZ = cos(-rotation.z);",
      "  float nextX = point.x * cosZ - point.y * sinZ;",
      "  float nextY = point.x * sinZ + point.y * cosZ;",
      "  point = vec3(nextX, nextY, point.z);",
      "  float sinY = sin(-rotation.y);",
      "  float cosY = cos(-rotation.y);",
      "  nextX = point.x * cosY + point.z * sinY;",
      "  float nextZ = -point.x * sinY + point.z * cosY;",
      "  point = vec3(nextX, point.y, nextZ);",
      "  float sinX = sin(-rotation.x);",
      "  float cosX = cos(-rotation.x);",
      "  nextY = point.y * cosX - point.z * sinX;",
      "  nextZ = point.y * sinX + point.z * cosX;",
      "  return vec3(point.x, nextY, nextZ);",
      "}",
      "void main() {",
      "  vec3 local = inverseRotatePoint(vec3(a_position.x - u_camera.x, a_position.y - u_camera.y, a_position.z + u_camera.z), u_camera_rotation);",
      "  float depth = local.z;",
      "  if (depth <= 0.001) {",
      "    gl_Position = vec4(2.0, 2.0, 0.0, 1.0);",
      "  } else {",
      "    float focal = 1.0 / tan(radians(u_camera.w) * 0.5);",
      "    vec2 projected = vec2(local.x * focal / depth, local.y * focal / depth);",
      "    projected.x /= max(u_aspect, 0.0001);",
      "    float clipDepth = clamp(depth / 128.0, 0.0, 1.0) * 2.0 - 1.0;",
      "    gl_Position = vec4(projected, clipDepth, 1.0);",
      "  }",
      "  v_uv = a_uv;",
      "}",
    ].join("\n");
    const fragmentSource = [
      "precision mediump float;",
      "varying vec2 v_uv;",
      "uniform sampler2D u_texture;",
      "uniform vec4 u_tint;",
      "uniform float u_emissive;",
      "void main() {",
      "  vec4 sampleColor = texture2D(u_texture, v_uv);",
      "  vec3 rgb = sampleColor.rgb * u_tint.rgb;",
      "  rgb *= 1.0 + max(u_emissive, 0.0) * 0.5;",
      "  gl_FragColor = vec4(clamp(rgb, 0.0, 1.0), clamp(sampleColor.a * u_tint.a, 0.0, 1.0));",
      "}",
    ].join("\n");

    const vertexShader = createSceneShader(gl, gl.VERTEX_SHADER, vertexSource);
    const fragmentShader = createSceneShader(gl, gl.FRAGMENT_SHADER, fragmentSource);
    if (!vertexShader || !fragmentShader) {
      return null;
    }

    const program = gl.createProgram();
    gl.attachShader(program, vertexShader);
    gl.attachShader(program, fragmentShader);
    gl.linkProgram(program);
    if (!gl.getProgramParameter(program, gl.LINK_STATUS)) {
      console.warn("[gosx] Scene3D WebGL surface link failed");
      return null;
    }
    return program;
  }

  function createSceneShader(gl, type, source) {
    const shader = gl.createShader(type);
    gl.shaderSource(shader, source);
    gl.compileShader(shader);
    if (!gl.getShaderParameter(shader, gl.COMPILE_STATUS)) {
      console.warn("[gosx] Scene3D WebGL shader compile failed");
      return null;
    }
    return shader;
  }

  function createSceneThickLineProgram(gl) {
    const vertexSource = [
      "attribute vec3 a_positionA;",
      "attribute vec3 a_positionB;",
      "attribute vec4 a_colorA;",
      "attribute vec4 a_colorB;",
      "attribute float a_side;",
      "attribute float a_endpoint;",
      "attribute float a_width;",
      "uniform vec4 u_camera;",
      "uniform vec3 u_camera_rotation;",
      "uniform float u_aspect;",
      "uniform vec2 u_viewport;",
      "varying vec4 v_color;",
      "vec3 inverseRotatePoint(vec3 point, vec3 rotation) {",
      "  float sinZ = sin(-rotation.z);",
      "  float cosZ = cos(-rotation.z);",
      "  float nextX = point.x * cosZ - point.y * sinZ;",
      "  float nextY = point.x * sinZ + point.y * cosZ;",
      "  point = vec3(nextX, nextY, point.z);",
      "  float sinY = sin(-rotation.y);",
      "  float cosY = cos(-rotation.y);",
      "  nextX = point.x * cosY + point.z * sinY;",
      "  float nextZ = -point.x * sinY + point.z * cosY;",
      "  point = vec3(nextX, point.y, nextZ);",
      "  float sinX = sin(-rotation.x);",
      "  float cosX = cos(-rotation.x);",
      "  nextY = point.y * cosX - point.z * sinX;",
      "  nextZ = point.y * sinX + point.z * cosX;",
      "  return vec3(point.x, nextY, nextZ);",
      "}",
      "vec4 projectEndpoint(vec3 world) {",
      "  vec3 local = inverseRotatePoint(vec3(world.x - u_camera.x, world.y - u_camera.y, world.z + u_camera.z), u_camera_rotation);",
      "  float depth = local.z;",
      "  if (depth <= 0.001) {",
      "    return vec4(2.0, 2.0, 0.0, 1.0);",
      "  }",
      "  float focal = 1.0 / tan(radians(u_camera.w) * 0.5);",
      "  vec2 projected = vec2(local.x * focal / depth, local.y * focal / depth);",
      "  projected.x /= max(u_aspect, 0.0001);",
      "  float clipDepth = clamp(depth / 128.0, 0.0, 1.0) * 2.0 - 1.0;",
      "  return vec4(projected, clipDepth, 1.0);",
      "}",
      "void main() {",
      "  vec4 clipA = projectEndpoint(a_positionA);",
      "  vec4 clipB = projectEndpoint(a_positionB);",
      "  vec4 base = mix(clipA, clipB, a_endpoint);",
      "  vec2 ndcA = clipA.xy / max(clipA.w, 0.0001);",
      "  vec2 ndcB = clipB.xy / max(clipB.w, 0.0001);",
      "  vec2 screenA = ndcA * (u_viewport * 0.5);",
      "  vec2 screenB = ndcB * (u_viewport * 0.5);",
      "  vec2 dir = screenB - screenA;",
      "  float len = length(dir);",
      "  if (len < 0.0001) {",
      "    dir = vec2(1.0, 0.0);",
      "  } else {",
      "    dir = dir / len;",
      "  }",
      "  vec2 normal = vec2(-dir.y, dir.x);",
      "  vec2 pixelOffset = normal * (a_side * a_width * 0.5);",
      "  vec2 ndcOffset = pixelOffset / max(u_viewport * 0.5, vec2(0.0001));",
      "  vec2 clipOffset = ndcOffset * base.w;",
      "  gl_Position = base + vec4(clipOffset, 0.0, 0.0);",
      "  v_color = mix(a_colorA, a_colorB, a_endpoint);",
      "}",
    ].join("\n");

    const fragmentSource = [
      "precision mediump float;",
      "varying vec4 v_color;",
      "void main() {",
      "  gl_FragColor = vec4(clamp(v_color.rgb, 0.0, 1.0), clamp(v_color.a, 0.0, 1.0));",
      "}",
    ].join("\n");

    const vertexShader = createSceneShader(gl, gl.VERTEX_SHADER, vertexSource);
    const fragmentShader = createSceneShader(gl, gl.FRAGMENT_SHADER, fragmentSource);
    if (!vertexShader || !fragmentShader) {
      return null;
    }
    const program = gl.createProgram();
    gl.attachShader(program, vertexShader);
    gl.attachShader(program, fragmentShader);
    gl.linkProgram(program);
    if (!gl.getProgramParameter(program, gl.LINK_STATUS)) {
      console.warn("[gosx] Scene3D thick-line link failed");
      return null;
    }
    return {
      program,
      positionALocation: gl.getAttribLocation(program, "a_positionA"),
      positionBLocation: gl.getAttribLocation(program, "a_positionB"),
      colorALocation: gl.getAttribLocation(program, "a_colorA"),
      colorBLocation: gl.getAttribLocation(program, "a_colorB"),
      sideLocation: gl.getAttribLocation(program, "a_side"),
      endpointLocation: gl.getAttribLocation(program, "a_endpoint"),
      widthLocation: gl.getAttribLocation(program, "a_width"),
      cameraLocation: gl.getUniformLocation(program, "u_camera"),
      cameraRotationLocation: gl.getUniformLocation(program, "u_camera_rotation"),
      aspectLocation: gl.getUniformLocation(program, "u_aspect"),
      viewportLocation: gl.getUniformLocation(program, "u_viewport"),
    };
  }

  function createSceneThickLineScratch() {
    return {
      segmentCapacity: 0,
      positionsA: new Float32Array(0),
      positionsB: new Float32Array(0),
      colorsA: new Float32Array(0),
      colorsB: new Float32Array(0),
      sides: new Float32Array(0),
      endpoints: new Float32Array(0),
      widths: new Float32Array(0),
      opaqueIndices: new Uint16Array(0),
      alphaIndices: new Uint16Array(0),
      additiveIndices: new Uint16Array(0),
      opaqueIndexCount: 0,
      alphaIndexCount: 0,
      additiveIndexCount: 0,
    };
  }

  function ensureSceneThickLineScratchCapacity(scratch, segmentCount) {
    if (scratch.segmentCapacity >= segmentCount) {
      return;
    }
    const nextCapacity = Math.max(64, Math.max(scratch.segmentCapacity * 2, segmentCount));
    const totalVerts = nextCapacity * 4;
    scratch.positionsA = new Float32Array(totalVerts * 3);
    scratch.positionsB = new Float32Array(totalVerts * 3);
    scratch.colorsA = new Float32Array(totalVerts * 4);
    scratch.colorsB = new Float32Array(totalVerts * 4);
    scratch.sides = new Float32Array(totalVerts);
    scratch.endpoints = new Float32Array(totalVerts);
    scratch.widths = new Float32Array(totalVerts);
    scratch.opaqueIndices = new Uint16Array(nextCapacity * 6);
    scratch.alphaIndices = new Uint16Array(nextCapacity * 6);
    scratch.additiveIndices = new Uint16Array(nextCapacity * 6);
    scratch.segmentCapacity = nextCapacity;
  }

  const _thickLineQuadEndpoints = [0, 0, 1, 1];
  const _thickLineQuadSides = [-1, 1, 1, -1];

  function expandSceneThickLineIntoScratch(scratch, worldPositions, worldColors, worldLineWidths, worldLinePasses, segmentCount) {
    const safeCount = Math.min(segmentCount, 16384);
    ensureSceneThickLineScratchCapacity(scratch, safeCount);

    const positionsA = scratch.positionsA;
    const positionsB = scratch.positionsB;
    const colorsA = scratch.colorsA;
    const colorsB = scratch.colorsB;
    const sides = scratch.sides;
    const endpoints = scratch.endpoints;
    const widths = scratch.widths;
    const opaqueIndices = scratch.opaqueIndices;
    const alphaIndices = scratch.alphaIndices;
    const additiveIndices = scratch.additiveIndices;

    let opaqueIdx = 0;
    let alphaIdx = 0;
    let additiveIdx = 0;

    for (let seg = 0; seg < safeCount; seg += 1) {
      const posOffset = seg * 6;
      const colorOffset = seg * 8;
      const ax = worldPositions[posOffset];
      const ay = worldPositions[posOffset + 1];
      const az = worldPositions[posOffset + 2];
      const bx = worldPositions[posOffset + 3];
      const by = worldPositions[posOffset + 4];
      const bz = worldPositions[posOffset + 5];
      const caR = worldColors[colorOffset];
      const caG = worldColors[colorOffset + 1];
      const caB = worldColors[colorOffset + 2];
      const caA = worldColors[colorOffset + 3];
      const cbR = worldColors[colorOffset + 4];
      const cbG = worldColors[colorOffset + 5];
      const cbB = worldColors[colorOffset + 6];
      const cbA = worldColors[colorOffset + 7];
      const width = (worldLineWidths && worldLineWidths[seg] > 0) ? worldLineWidths[seg] : 1;

      for (let corner = 0; corner < 4; corner += 1) {
        const vi = seg * 4 + corner;
        const p3 = vi * 3;
        const p4 = vi * 4;
        positionsA[p3] = ax;
        positionsA[p3 + 1] = ay;
        positionsA[p3 + 2] = az;
        positionsB[p3] = bx;
        positionsB[p3 + 1] = by;
        positionsB[p3 + 2] = bz;
        colorsA[p4] = caR;
        colorsA[p4 + 1] = caG;
        colorsA[p4 + 2] = caB;
        colorsA[p4 + 3] = caA;
        colorsB[p4] = cbR;
        colorsB[p4 + 1] = cbG;
        colorsB[p4 + 2] = cbB;
        colorsB[p4 + 3] = cbA;
        sides[vi] = _thickLineQuadSides[corner];
        endpoints[vi] = _thickLineQuadEndpoints[corner];
        widths[vi] = width;
      }

      const base = seg * 4;
      const pass = (worldLinePasses && seg < worldLinePasses.length) ? worldLinePasses[seg] : 0;
      if (pass === 2) {
        additiveIndices[additiveIdx] = base;
        additiveIndices[additiveIdx + 1] = base + 1;
        additiveIndices[additiveIdx + 2] = base + 2;
        additiveIndices[additiveIdx + 3] = base;
        additiveIndices[additiveIdx + 4] = base + 2;
        additiveIndices[additiveIdx + 5] = base + 3;
        additiveIdx += 6;
      } else if (pass === 1) {
        alphaIndices[alphaIdx] = base;
        alphaIndices[alphaIdx + 1] = base + 1;
        alphaIndices[alphaIdx + 2] = base + 2;
        alphaIndices[alphaIdx + 3] = base;
        alphaIndices[alphaIdx + 4] = base + 2;
        alphaIndices[alphaIdx + 5] = base + 3;
        alphaIdx += 6;
      } else {
        opaqueIndices[opaqueIdx] = base;
        opaqueIndices[opaqueIdx + 1] = base + 1;
        opaqueIndices[opaqueIdx + 2] = base + 2;
        opaqueIndices[opaqueIdx + 3] = base;
        opaqueIndices[opaqueIdx + 4] = base + 2;
        opaqueIndices[opaqueIdx + 5] = base + 3;
        opaqueIdx += 6;
      }
    }

    scratch.opaqueIndexCount = opaqueIdx;
    scratch.alphaIndexCount = alphaIdx;
    scratch.additiveIndexCount = additiveIdx;
    return safeCount;
  }

  function createSceneThickLineBufferSet(gl) {
    return {
      positionA: gl.createBuffer(),
      positionB: gl.createBuffer(),
      colorA: gl.createBuffer(),
      colorB: gl.createBuffer(),
      side: gl.createBuffer(),
      endpoint: gl.createBuffer(),
      width: gl.createBuffer(),
      opaqueIndex: gl.createBuffer(),
      alphaIndex: gl.createBuffer(),
      additiveIndex: gl.createBuffer(),
    };
  }

  function uploadSceneThickLineBuffers(gl, resources, scratch, segmentCount) {
    const buffers = resources.thickLineBuffers;
    const arrayBuffer = resources.arrayBuffer;
    const elementArrayBuffer = typeof gl.ELEMENT_ARRAY_BUFFER === "number" ? gl.ELEMENT_ARRAY_BUFFER : 0x8893;
    const usedVerts = segmentCount * 4;

    gl.bindBuffer(arrayBuffer, buffers.positionA);
    gl.bufferData(arrayBuffer, scratch.positionsA.subarray(0, usedVerts * 3), resources.dynamicDraw);
    gl.bindBuffer(arrayBuffer, buffers.positionB);
    gl.bufferData(arrayBuffer, scratch.positionsB.subarray(0, usedVerts * 3), resources.dynamicDraw);
    gl.bindBuffer(arrayBuffer, buffers.colorA);
    gl.bufferData(arrayBuffer, scratch.colorsA.subarray(0, usedVerts * 4), resources.dynamicDraw);
    gl.bindBuffer(arrayBuffer, buffers.colorB);
    gl.bufferData(arrayBuffer, scratch.colorsB.subarray(0, usedVerts * 4), resources.dynamicDraw);
    gl.bindBuffer(arrayBuffer, buffers.side);
    gl.bufferData(arrayBuffer, scratch.sides.subarray(0, usedVerts), resources.dynamicDraw);
    gl.bindBuffer(arrayBuffer, buffers.endpoint);
    gl.bufferData(arrayBuffer, scratch.endpoints.subarray(0, usedVerts), resources.dynamicDraw);
    gl.bindBuffer(arrayBuffer, buffers.width);
    gl.bufferData(arrayBuffer, scratch.widths.subarray(0, usedVerts), resources.dynamicDraw);

    if (scratch.opaqueIndexCount > 0) {
      gl.bindBuffer(elementArrayBuffer, buffers.opaqueIndex);
      gl.bufferData(elementArrayBuffer, scratch.opaqueIndices.subarray(0, scratch.opaqueIndexCount), resources.dynamicDraw);
    }
    if (scratch.alphaIndexCount > 0) {
      gl.bindBuffer(elementArrayBuffer, buffers.alphaIndex);
      gl.bufferData(elementArrayBuffer, scratch.alphaIndices.subarray(0, scratch.alphaIndexCount), resources.dynamicDraw);
    }
    if (scratch.additiveIndexCount > 0) {
      gl.bindBuffer(elementArrayBuffer, buffers.additiveIndex);
      gl.bufferData(elementArrayBuffer, scratch.additiveIndices.subarray(0, scratch.additiveIndexCount), resources.dynamicDraw);
    }
  }

  function drawSceneThickLines(gl, bundle, canvas, resources) {
    const thickProgram = resources.thickLineProgram;
    if (!thickProgram || !thickProgram.program) {
      return false;
    }
    const widths = bundle && bundle.worldLineWidths;
    const passes = bundle && bundle.worldLinePasses;
    const vertexCount = Math.floor(sceneNumber(bundle && bundle.worldVertexCount, 0));
    const segmentCount = Math.floor(vertexCount / 2);
    if (segmentCount <= 0 || !bundle.worldPositions || !bundle.worldColors) {
      return false;
    }
    if (segmentCount > 16384) {
      return false;
    }

    const scratch = resources.thickLineScratch;
    const usedSegments = expandSceneThickLineIntoScratch(scratch, bundle.worldPositions, bundle.worldColors, widths, passes, segmentCount);
    uploadSceneThickLineBuffers(gl, resources, scratch, usedSegments);

    gl.useProgram(thickProgram.program);

    const camera = sceneRenderCamera(bundle && bundle.camera);
    if (thickProgram.cameraLocation && typeof gl.uniform4f === "function") {
      gl.uniform4f(thickProgram.cameraLocation, camera.x, camera.y, camera.z, camera.fov);
    }
    if (thickProgram.cameraRotationLocation && typeof gl.uniform3f === "function") {
      gl.uniform3f(thickProgram.cameraRotationLocation, camera.rotationX, camera.rotationY, camera.rotationZ);
    }
    if (thickProgram.aspectLocation && typeof gl.uniform1f === "function") {
      const aspect = Math.max(0.0001, canvas.width / Math.max(1, canvas.height));
      gl.uniform1f(thickProgram.aspectLocation, aspect);
    }
    if (thickProgram.viewportLocation && typeof gl.uniform2f === "function") {
      gl.uniform2f(thickProgram.viewportLocation, canvas.width, canvas.height);
    }

    const arrayBuffer = resources.arrayBuffer;
    const floatType = resources.floatType;
    const buffers = resources.thickLineBuffers;

    gl.bindBuffer(arrayBuffer, buffers.positionA);
    gl.enableVertexAttribArray(thickProgram.positionALocation);
    gl.vertexAttribPointer(thickProgram.positionALocation, 3, floatType, false, 0, 0);

    gl.bindBuffer(arrayBuffer, buffers.positionB);
    gl.enableVertexAttribArray(thickProgram.positionBLocation);
    gl.vertexAttribPointer(thickProgram.positionBLocation, 3, floatType, false, 0, 0);

    gl.bindBuffer(arrayBuffer, buffers.colorA);
    gl.enableVertexAttribArray(thickProgram.colorALocation);
    gl.vertexAttribPointer(thickProgram.colorALocation, 4, floatType, false, 0, 0);

    gl.bindBuffer(arrayBuffer, buffers.colorB);
    gl.enableVertexAttribArray(thickProgram.colorBLocation);
    gl.vertexAttribPointer(thickProgram.colorBLocation, 4, floatType, false, 0, 0);

    gl.bindBuffer(arrayBuffer, buffers.side);
    gl.enableVertexAttribArray(thickProgram.sideLocation);
    gl.vertexAttribPointer(thickProgram.sideLocation, 1, floatType, false, 0, 0);

    gl.bindBuffer(arrayBuffer, buffers.endpoint);
    gl.enableVertexAttribArray(thickProgram.endpointLocation);
    gl.vertexAttribPointer(thickProgram.endpointLocation, 1, floatType, false, 0, 0);

    gl.bindBuffer(arrayBuffer, buffers.width);
    gl.enableVertexAttribArray(thickProgram.widthLocation);
    gl.vertexAttribPointer(thickProgram.widthLocation, 1, floatType, false, 0, 0);

    const elementArrayBuffer = typeof gl.ELEMENT_ARRAY_BUFFER === "number" ? gl.ELEMENT_ARRAY_BUFFER : 0x8893;
    const unsignedShort = typeof gl.UNSIGNED_SHORT === "number" ? gl.UNSIGNED_SHORT : 0x1403;

    if (scratch.opaqueIndexCount > 0) {
      applySceneWebGLDepth(gl, "opaque", resources.stateCache);
      applySceneWebGLBlend(gl, "opaque", resources.stateCache);
      gl.bindBuffer(elementArrayBuffer, buffers.opaqueIndex);
      gl.drawElements(resources.trianglesMode, scratch.opaqueIndexCount, unsignedShort, 0);
    }
    if (scratch.alphaIndexCount > 0) {
      applySceneWebGLDepth(gl, "translucent", resources.stateCache);
      applySceneWebGLBlend(gl, "alpha", resources.stateCache);
      gl.bindBuffer(elementArrayBuffer, buffers.alphaIndex);
      gl.drawElements(resources.trianglesMode, scratch.alphaIndexCount, unsignedShort, 0);
    }
    if (scratch.additiveIndexCount > 0) {
      applySceneWebGLDepth(gl, "translucent", resources.stateCache);
      applySceneWebGLBlend(gl, "additive", resources.stateCache);
      gl.bindBuffer(elementArrayBuffer, buffers.additiveIndex);
      gl.drawElements(resources.trianglesMode, scratch.additiveIndexCount, unsignedShort, 0);
    }
    return true;
  }

  function sceneBundleNeedsThickLines(bundle) {
    const widths = bundle && bundle.worldLineWidths;
    if (!widths || !widths.length) {
      return false;
    }
    for (let i = 0; i < widths.length; i += 1) {
      if (widths[i] > 1) {
        return true;
      }
    }
    return false;
  }

  window.__gosx_scene3d_api = {
    appendSceneObjectToBundle,
    appendSceneSurfaceToBundle,
    applySceneCommands,
    cancelEngineFrame,
    clearChildren,
    createSceneRenderBundle,
    createSceneState,
    createSceneWebGLRenderer,
    engineFrame,
    normalizeSceneEnvironment,
    normalizeSceneLabel,
    normalizeSceneLabelAlign,
    normalizeSceneLabelCollision,
    normalizeSceneLabelWhiteSpace,
    normalizeSceneLight,
    normalizeSceneObject,
    normalizeSceneSprite,
    normalizeSceneSpriteFit,
    publishPointerSignals,
    queueInputSignal,
    sceneAdvanceTransitions,
    sceneApplyLiveEvent,
    sceneBool,
    sceneBoundsDepthMetrics,
    sceneBoundsViewCulled,
    sceneBundleNeedsThickLines,
    sceneCameraEquivalent,
    sceneHasActiveTransitions,
    sceneLabelAnimated,
    sceneMeshMaterialArray,
    sceneModels,
    sceneNormalizeDirection,
    sceneNowMilliseconds,
    sceneNumber,
    sceneObjectAnimated,
    scenePointStyleCode,
    scenePrimeInitialTransitions,
    sceneProps,
    sceneRenderCamera,
    sceneResolveLightingEnvironment,
    sceneSpriteAnimated,
    sceneStateLabels,
    sceneStateLights,
    sceneStateObjects,
    sceneStateSprites,
    sceneTypedFloatArray,
    translateScenePointInto,

    scenePBRDepthSort: typeof scenePBRDepthSort === "function" ? scenePBRDepthSort : undefined,
    scenePBRObjectRenderPass: typeof scenePBRObjectRenderPass === "function" ? scenePBRObjectRenderPass : undefined,
    scenePBRProjectionMatrix: typeof scenePBRProjectionMatrix === "function" ? scenePBRProjectionMatrix : undefined,
    scenePBRViewMatrix: typeof scenePBRViewMatrix === "function" ? scenePBRViewMatrix : undefined,
    sceneShadowLightSpaceMatrix: typeof sceneShadowLightSpaceMatrix === "function" ? sceneShadowLightSpaceMatrix : undefined,
    sceneShadowComputeBounds: typeof sceneShadowComputeBounds === "function" ? sceneShadowComputeBounds : undefined,

    resolvePostFXFactor: typeof resolvePostFXFactor === "function" ? resolvePostFXFactor : undefined,
    resolveShadowSize: typeof resolveShadowSize === "function" ? resolveShadowSize : undefined,

    sceneColorRGBA: typeof sceneColorRGBA === "function" ? sceneColorRGBA : undefined,
  };

  function sceneClamp(value, min, max) {
    return Math.max(min, Math.min(max, value));
  }

  function sceneAddPoint(left, right) {
    return {
      x: sceneNumber(left && left.x, 0) + sceneNumber(right && right.x, 0),
      y: sceneNumber(left && left.y, 0) + sceneNumber(right && right.y, 0),
      z: sceneNumber(left && left.z, 0) + sceneNumber(right && right.z, 0),
    };
  }

  function sceneScalePoint(point, scale) {
    return {
      x: sceneNumber(point && point.x, 0) * scale,
      y: sceneNumber(point && point.y, 0) * scale,
      z: sceneNumber(point && point.z, 0) * scale,
    };
  }

  function sceneMultiplyPoint(left, right) {
    return {
      x: sceneNumber(left && left.x, 0) * sceneNumber(right && right.x, 0),
      y: sceneNumber(left && left.y, 0) * sceneNumber(right && right.y, 0),
      z: sceneNumber(left && left.z, 0) * sceneNumber(right && right.z, 0),
    };
  }

  function scenePointLength(point) {
    var px = sceneNumber(point && point.x, 0);
    var py = sceneNumber(point && point.y, 0);
    var pz = sceneNumber(point && point.z, 0);
    return Math.sqrt(px * px + py * py + pz * pz);
  }

  function sceneNormalizePoint(point) {
    const length = scenePointLength(point);
    if (length <= 0.000001) {
      return { x: 0, y: 0, z: 0 };
    }
    return sceneScalePoint(point, 1 / length);
  }

  function sceneDotPoint(left, right) {
    return (
      sceneNumber(left && left.x, 0) * sceneNumber(right && right.x, 0) +
      sceneNumber(left && left.y, 0) * sceneNumber(right && right.y, 0) +
      sceneNumber(left && left.z, 0) * sceneNumber(right && right.z, 0)
    );
  }

  function sceneRotatePoint(point, rotationX, rotationY, rotationZ) {
    let x = point.x;
    let y = point.y;
    let z = point.z;

    const sinX = Math.sin(rotationX);
    const cosX = Math.cos(rotationX);
    let nextY = y * cosX - z * sinX;
    let nextZ = y * sinX + z * cosX;
    y = nextY;
    z = nextZ;

    const sinY = Math.sin(rotationY);
    const cosY = Math.cos(rotationY);
    let nextX = x * cosY + z * sinY;
    nextZ = -x * sinY + z * cosY;
    x = nextX;
    z = nextZ;

    const sinZ = Math.sin(rotationZ);
    const cosZ = Math.cos(rotationZ);
    nextX = x * cosZ - y * sinZ;
    nextY = x * sinZ + y * cosZ;

    return { x: nextX, y: nextY, z: z };
  }

  function sceneInverseRotatePoint(point, rotationX, rotationY, rotationZ) {
    let x = sceneNumber(point && point.x, 0);
    let y = sceneNumber(point && point.y, 0);
    let z = sceneNumber(point && point.z, 0);

    const sinZ = Math.sin(-rotationZ);
    const cosZ = Math.cos(-rotationZ);
    let nextX = x * cosZ - y * sinZ;
    let nextY = x * sinZ + y * cosZ;
    x = nextX;
    y = nextY;

    const sinY = Math.sin(-rotationY);
    const cosY = Math.cos(-rotationY);
    nextX = x * cosY + z * sinY;
    let nextZ = -x * sinY + z * cosY;
    x = nextX;
    z = nextZ;

    const sinX = Math.sin(-rotationX);
    const cosX = Math.cos(-rotationX);
    nextY = y * cosX - z * sinX;
    nextZ = y * sinX + z * cosX;
    return { x, y: nextY, z: nextZ };
  }

  const _sceneProjectCameraScratch = {
    x: 0, y: 0, z: 0,
    rotationX: 0, rotationY: 0, rotationZ: 0,
    fov: 0, near: 0, far: 0,
  };

  function sceneProjectPoint(point, camera, width, height) {
    const cam = sceneRenderCamera(camera, _sceneProjectCameraScratch);

    let lx = sceneNumber(point && point.x, 0) - cam.x;
    let ly = sceneNumber(point && point.y, 0) - cam.y;
    let lz = sceneNumber(point && point.z, 0) + cam.z;

    const sinZ = Math.sin(-cam.rotationZ);
    const cosZ = Math.cos(-cam.rotationZ);
    let nextX = lx * cosZ - ly * sinZ;
    let nextY = lx * sinZ + ly * cosZ;
    lx = nextX;
    ly = nextY;

    const sinY = Math.sin(-cam.rotationY);
    const cosY = Math.cos(-cam.rotationY);
    nextX = lx * cosY + lz * sinY;
    let nextZ = -lx * sinY + lz * cosY;
    lx = nextX;
    lz = nextZ;

    const sinX = Math.sin(-cam.rotationX);
    const cosX = Math.cos(-cam.rotationX);
    nextY = ly * cosX - lz * sinX;
    nextZ = ly * sinX + lz * cosX;
    ly = nextY;
    lz = nextZ;

    if (lz <= cam.near || lz >= cam.far) return null;

    const focal = (Math.min(width, height) / 2) / Math.tan((cam.fov * Math.PI) / 360);
    return {
      x: width / 2 + (lx * focal) / lz,
      y: height / 2 - (ly * focal) / lz,
      depth: lz,
    };
  }

  function sceneCameraLocalPoint(point, camera) {
    const normalizedCamera = sceneRenderCamera(camera);
    return sceneInverseRotatePoint({
      x: sceneNumber(point && point.x, 0) - normalizedCamera.x,
      y: sceneNumber(point && point.y, 0) - normalizedCamera.y,
      z: sceneNumber(point && point.z, 0) + normalizedCamera.z,
    }, normalizedCamera.rotationX, normalizedCamera.rotationY, normalizedCamera.rotationZ);
  }

  function sceneClipPoint(point, width, height) {
    return {
      x: (point.x / width) * 2 - 1,
      y: 1 - (point.y / height) * 2,
    };
  }

  function sceneSafeNormal(normal) {
    const normalized = sceneNormalizePoint(normal);
    return scenePointLength(normalized) > 0.0001 ? normalized : { x: 0, y: 1, z: 0 };
  }

  function sceneColorPoint(value, fallback) {
    const rgba = sceneColorRGBA(value, [sceneNumber(fallback && fallback.x, 1), sceneNumber(fallback && fallback.y, 1), sceneNumber(fallback && fallback.z, 1), 1]);
    return { x: rgba[0], y: rgba[1], z: rgba[2] };
  }

  function sceneRGBAString(rgba) {
    const color = Array.isArray(rgba) ? rgba : [0.55, 0.88, 1, 1];
    return "rgba(" +
      Math.round(clamp01(sceneNumber(color[0], 0.55)) * 255) + ", " +
      Math.round(clamp01(sceneNumber(color[1], 0.88)) * 255) + ", " +
      Math.round(clamp01(sceneNumber(color[2], 1)) * 255) + ", " +
      clamp01(sceneNumber(color[3], 1)).toFixed(3) + ")";
  }

  function sceneColorRGBA(value, fallback) {
    const base = Array.isArray(fallback) && fallback.length === 4 ? fallback.slice() : [0.55, 0.88, 1, 1];
    if (typeof value !== "string") {
      return base;
    }

    const trimmed = value.trim();
    const shortHex = trimmed.match(/^#([0-9a-f]{3})$/i);
    if (shortHex) {
      return [
        parseInt(shortHex[1][0] + shortHex[1][0], 16) / 255,
        parseInt(shortHex[1][1] + shortHex[1][1], 16) / 255,
        parseInt(shortHex[1][2] + shortHex[1][2], 16) / 255,
        1,
      ];
    }

    const fullHex = trimmed.match(/^#([0-9a-f]{6})$/i);
    if (fullHex) {
      return [
        parseInt(fullHex[1].slice(0, 2), 16) / 255,
        parseInt(fullHex[1].slice(2, 4), 16) / 255,
        parseInt(fullHex[1].slice(4, 6), 16) / 255,
        1,
      ];
    }

    const rgba = trimmed.match(/^rgba?\(([^)]+)\)$/i);
    if (rgba) {
      const parts = rgba[1].split(",").map(function(part) {
        return Number(part.trim());
      });
      if (parts.length >= 3 && parts.every(function(part, index) {
        return Number.isFinite(part) && (index < 3 || index === 3);
      })) {
        return [
          Math.max(0, Math.min(255, parts[0])) / 255,
          Math.max(0, Math.min(255, parts[1])) / 255,
          Math.max(0, Math.min(255, parts[2])) / 255,
          parts.length > 3 ? Math.max(0, Math.min(1, parts[3])) : 1,
        ];
      }
    }

    return base;
  }

  function sceneMixRGBA(left, right) {
    return [
      (sceneNumber(left && left[0], 0.55) + sceneNumber(right && right[0], 0.55)) / 2,
      (sceneNumber(left && left[1], 0.88) + sceneNumber(right && right[1], 0.88)) / 2,
      (sceneNumber(left && left[2], 1) + sceneNumber(right && right[2], 1)) / 2,
      (sceneNumber(left && left[3], 1) + sceneNumber(right && right[3], 1)) / 2,
    ];
  }

  function sceneDot3(ax, ay, az, bx, by, bz) {
    return ax * bx + ay * by + az * bz;
  }

  function sceneCross3Into(out, offset, ax, ay, az, bx, by, bz) {
    out[offset] = ay * bz - az * by;
    out[offset + 1] = az * bx - ax * bz;
    out[offset + 2] = ax * by - ay * bx;
  }

  function sceneNormalize3Into(out, offset, x, y, z) {
    var len = Math.sqrt(x * x + y * y + z * z);
    if (len < 1e-10) { out[offset] = 0; out[offset + 1] = 0; out[offset + 2] = 0; return 0; }
    var inv = 1 / len;
    out[offset] = x * inv; out[offset + 1] = y * inv; out[offset + 2] = z * inv;
    return len;
  }

  function sceneRayIntersectsAABB(rayOrigin, rayDir, boundsMin, boundsMax) {
    var tMin = -Infinity;
    var tMax = Infinity;

    for (var ai = 0; ai < 3; ai++) {
      var axis = ai === 0 ? "x" : ai === 1 ? "y" : "z";
      var origin = rayOrigin[axis];
      var dir = rayDir[axis];
      var min = boundsMin[axis];
      var max = boundsMax[axis];

      if (Math.abs(dir) < 1e-10) {
        if (origin < min || origin > max) return -1;
      } else {
        var t1 = (min - origin) / dir;
        var t2 = (max - origin) / dir;
        if (t1 > t2) { var tmp = t1; t1 = t2; t2 = tmp; }
        tMin = Math.max(tMin, t1);
        tMax = Math.min(tMax, t2);
        if (tMin > tMax) return -1;
      }
    }

    if (tMax < 0) return -1;
    return tMin >= 0 ? tMin : tMax;
  }

  function sceneRayIntersectsTriangle(rayOrigin, rayDir, v0, v1, v2) {
    var EPSILON = 1e-7;

    var rox = rayOrigin.x, roy = rayOrigin.y, roz = rayOrigin.z;
    var rdx = rayDir.x, rdy = rayDir.y, rdz = rayDir.z;
    var v0x = v0.x, v0y = v0.y, v0z = v0.z;

    var edge1x = v1.x - v0x, edge1y = v1.y - v0y, edge1z = v1.z - v0z;
    var edge2x = v2.x - v0x, edge2y = v2.y - v0y, edge2z = v2.z - v0z;

    var hx = rdy * edge2z - rdz * edge2y;
    var hy = rdz * edge2x - rdx * edge2z;
    var hz = rdx * edge2y - rdy * edge2x;

    var a = sceneDot3(edge1x, edge1y, edge1z, hx, hy, hz);
    if (a > -EPSILON && a < EPSILON) return null; // parallel

    var f = 1.0 / a;
    var sx = rox - v0x;
    var sy = roy - v0y;
    var sz = roz - v0z;

    var u = f * sceneDot3(sx, sy, sz, hx, hy, hz);
    if (u < 0.0 || u > 1.0) return null;

    var qx = sy * edge1z - sz * edge1y;
    var qy = sz * edge1x - sx * edge1z;
    var qz = sx * edge1y - sy * edge1x;

    var v = f * sceneDot3(rdx, rdy, rdz, qx, qy, qz);
    if (v < 0.0 || u + v > 1.0) return null;

    var t = f * sceneDot3(edge2x, edge2y, edge2z, qx, qy, qz);
    if (t < EPSILON) return null; // behind ray

    return { distance: t, u: u, v: v };
  }

  function clamp01(value) {
    return Math.max(0, Math.min(1, value));
  }

  var SCENE_IDENTITY_MAT4 = new Float32Array([
    1, 0, 0, 0,
    0, 1, 0, 0,
    0, 0, 1, 0,
    0, 0, 0, 1,
  ]);

  var _sceneMat4ScratchA = new Float32Array(16);
  var _sceneMat4ScratchB = new Float32Array(16);

  function sceneMat4Multiply(a, b) {
    var out = new Float32Array(16);
    for (var col = 0; col < 4; col++) {
      for (var row = 0; row < 4; row++) {
        out[col * 4 + row] =
          a[row]      * b[col * 4] +
          a[4 + row]  * b[col * 4 + 1] +
          a[8 + row]  * b[col * 4 + 2] +
          a[12 + row] * b[col * 4 + 3];
      }
    }
    return out;
  }

  function sceneMat4MultiplyInto(out, a, b) {
    for (var col = 0; col < 4; col++) {
      for (var row = 0; row < 4; row++) {
        out[col * 4 + row] =
          a[row]      * b[col * 4] +
          a[4 + row]  * b[col * 4 + 1] +
          a[8 + row]  * b[col * 4 + 2] +
          a[12 + row] * b[col * 4 + 3];
      }
    }
    return out;
  }

  function sceneTRSToMat4(t, r, s) {
    var out = new Float32Array(16);
    var x = r[0], y = r[1], z = r[2], w = r[3];
    var x2 = x + x, y2 = y + y, z2 = z + z;
    var xx = x * x2, xy = x * y2, xz = x * z2;
    var yy = y * y2, yz = y * z2, zz = z * z2;
    var wx = w * x2, wy = w * y2, wz = w * z2;

    out[0]  = (1 - (yy + zz)) * s[0]; out[1]  = (xy + wz) * s[0];       out[2]  = (xz - wy) * s[0];       out[3]  = 0;
    out[4]  = (xy - wz) * s[1];       out[5]  = (1 - (xx + zz)) * s[1]; out[6]  = (yz + wx) * s[1];       out[7]  = 0;
    out[8]  = (xz + wy) * s[2];       out[9]  = (yz - wx) * s[2];       out[10] = (1 - (xx + yy)) * s[2]; out[11] = 0;
    out[12] = t[0];                    out[13] = t[1];                    out[14] = t[2];                    out[15] = 1;

    return out;
  }

  function sceneTRSToMat4Into(out, t, r, s) {
    var x = r[0], y = r[1], z = r[2], w = r[3];
    var x2 = x + x, y2 = y + y, z2 = z + z;
    var xx = x * x2, xy = x * y2, xz = x * z2;
    var yy = y * y2, yz = y * z2, zz = z * z2;
    var wx = w * x2, wy = w * y2, wz = w * z2;

    out[0]  = (1 - (yy + zz)) * s[0]; out[1]  = (xy + wz) * s[0];       out[2]  = (xz - wy) * s[0];       out[3]  = 0;
    out[4]  = (xy - wz) * s[1];       out[5]  = (1 - (xx + zz)) * s[1]; out[6]  = (yz + wx) * s[1];       out[7]  = 0;
    out[8]  = (xz + wy) * s[2];       out[9]  = (yz - wx) * s[2];       out[10] = (1 - (xx + yy)) * s[2]; out[11] = 0;
    out[12] = t[0];                    out[13] = t[1];                    out[14] = t[2];                    out[15] = 1;

    return out;
  }

  var _animScratch3 = [0, 0, 0];
  var _animScratch4 = [0, 0, 0, 0];

  function sceneScreenToRay(pointerX, pointerY, width, height, camera) {
    var cam = sceneRenderCamera(camera);

    var origin = { x: cam.x, y: cam.y, z: -cam.z };

    var halfFov = (cam.fov * Math.PI) / 360;
    var tanHalf = Math.tan(halfFov);
    var minDim = Math.min(width, height) / 2;
    var focal = minDim / tanHalf;

    var dirCam = {
      x: (pointerX - width / 2) / focal,
      y: (height / 2 - pointerY) / focal,
      z: 1.0,
    };

    var dirWorld = sceneRotatePoint(dirCam, cam.rotationX, cam.rotationY, cam.rotationZ);

    var len = Math.sqrt(dirWorld.x * dirWorld.x + dirWorld.y * dirWorld.y + dirWorld.z * dirWorld.z);
    if (len < 1e-12) {
      return { origin: origin, dir: { x: 0, y: 0, z: 1 } };
    }

    return {
      origin: origin,
      dir: { x: dirWorld.x / len, y: dirWorld.y / len, z: dirWorld.z / len },
    };
  }

  function sceneDecompressArray(chunks) {
    if (!Array.isArray(chunks) || chunks.length === 0) return null;
    var result = [];
    for (var ci = 0; ci < chunks.length; ci++) {
      var chunk = chunks[ci];
      var packed = chunk.packed;
      var minVal = chunk.norm;      // "norm" field stores the min value
      var maxVal = chunk.maxVal;
      var count = chunk.count;
      var bitWidth = chunk.bitWidth;

      if (!packed || count < 2) continue;

      var bytes;
      if (typeof packed === "string") {
        bytes = sceneBase64Decode(packed);
      } else if (packed instanceof Uint8Array) {
        bytes = packed;
      } else if (Array.isArray(packed)) {
        bytes = new Uint8Array(packed);
      } else {
        continue;
      }

      var levels = (1 << bitWidth) - 1;
      var step = levels > 0 ? (maxVal - minVal) / levels : 0;

      var indices = sceneUnpackIndices(bytes, count, bitWidth);
      for (var i = 0; i < count; i++) {
        result.push(minVal + indices[i] * step);
      }
    }
    return result.length > 0 ? result : null;
  }

  function sceneUnpackIndices(src, count, bitWidth) {
    var indices = new Int32Array(count);
    switch (bitWidth) {
      case 1:
        for (var i = 0; i < count; i++) {
          indices[i] = (src[i >> 3] >> (i & 7)) & 1;
        }
        break;
      case 2:
        for (var i = 0; i < count; i++) {
          indices[i] = (src[i >> 2] >> ((i & 3) * 2)) & 3;
        }
        break;
      case 4:
        for (var i = 0; i < count; i++) {
          indices[i] = (src[i >> 1] >> ((i & 1) * 4)) & 15;
        }
        break;
      case 8:
        for (var i = 0; i < count; i++) {
          indices[i] = src[i];
        }
        break;
      default:
        var bitPos = 0;
        var mask = (1 << bitWidth) - 1;
        for (var i = 0; i < count; i++) {
          var val = 0;
          for (var b = 0; b < bitWidth; b++) {
            if (src[bitPos >> 3] & (1 << (bitPos & 7))) {
              val |= 1 << b;
            }
            bitPos++;
          }
          indices[i] = val & mask;
        }
    }
    return indices;
  }

  function sceneBase64Decode(str) {
    if (typeof atob === "function") {
      var raw = atob(str);
      var bytes = new Uint8Array(raw.length);
      for (var i = 0; i < raw.length; i++) {
        bytes[i] = raw.charCodeAt(i);
      }
      return bytes;
    }
    if (typeof Buffer !== "undefined") {
      return new Uint8Array(Buffer.from(str, "base64"));
    }
    return new Uint8Array(0);
  }

  function sceneReinterleave(data, stride) {
    var n = data.length / stride;
    var out = new Array(data.length);
    for (var c = 0; c < stride; c++) {
      for (var i = 0; i < n; i++) {
        out[i * stride + c] = data[c * n + i];
      }
    }
    return out;
  }

  function sceneDecompressPointsEntry(entry) {
    if (entry.compressedPositions && !entry.positions) {
      entry.positions = sceneDecompressArray(entry.compressedPositions);
      if (entry.positions && entry.positionStride > 1) {
        entry.positions = sceneReinterleave(entry.positions, entry.positionStride);
      }
      if (entry.positions) {
        delete entry.compressedPositions;
      }
    }
    if (entry.compressedSizes && !entry.sizes) {
      entry.sizes = sceneDecompressArray(entry.compressedSizes);
      if (entry.sizes) {
        delete entry.compressedSizes;
      }
    }
  }

  function sceneDecompressInstancedMeshEntry(entry) {
    if (entry.compressedTransforms && !entry.transforms) {
      entry.transforms = sceneDecompressArray(entry.compressedTransforms);
      if (entry.transforms) {
        delete entry.compressedTransforms;
      }
    }
  }

  function sceneDecompressAnimationChannel(channel) {
    if (channel.compressedTimes && !channel.times) {
      channel.times = sceneDecompressArray(channel.compressedTimes);
      if (channel.times) {
        delete channel.compressedTimes;
      }
    }
    if (channel.compressedValues && !channel.values) {
      channel.values = sceneDecompressArray(channel.compressedValues);
      if (channel.values) {
        delete channel.compressedValues;
      }
    }
  }

  function sceneDecompressProps(props) {
    var scene = sceneProps(props);
    var comp = props && props.compression;
    var progressive = comp && comp.progressive;
    var lod = comp && comp.lod;
    var lodThreshold = comp && comp.lodThreshold || 20;

    var points = scene && Array.isArray(scene.points) ? scene.points :
                 (props && Array.isArray(props.points) ? props.points : []);
    for (var i = 0; i < points.length; i++) {
      var entry = points[i];
      if (progressive || lod) {
        if (entry.previewPositions && !entry.positions) {
          entry.positions = sceneDecompressArray(entry.previewPositions);
          if (entry.positions && entry.positionStride > 1) {
            entry.positions = sceneReinterleave(entry.positions, entry.positionStride);
          }
          entry._pendingPositions = entry.compressedPositions;
          entry._positionStride = entry.positionStride || 0;
          entry._previewActive = true;
          delete entry.previewPositions;
        }
        if (entry.previewSizes && !entry.sizes) {
          entry.sizes = sceneDecompressArray(entry.previewSizes);
          entry._pendingSizes = entry.compressedSizes;
          delete entry.previewSizes;
        }
        if (lod) {
          if (entry._pendingPositions) {
            entry._fullPositions = sceneDecompressArray(entry._pendingPositions);
            if (entry._fullPositions && entry._positionStride > 1) {
              entry._fullPositions = sceneReinterleave(entry._fullPositions, entry._positionStride);
            }
            entry._previewPositions = entry.positions;
            entry._lodThreshold = lodThreshold;
          }
        }
      } else {
        sceneDecompressPointsEntry(entry);
      }
    }

    var meshes = scene && Array.isArray(scene.instancedMeshes) ? scene.instancedMeshes :
                 (props && Array.isArray(props.instancedMeshes) ? props.instancedMeshes : []);
    for (var i = 0; i < meshes.length; i++) {
      var entry = meshes[i];
      if (progressive || lod) {
        if (entry.previewTransforms && !entry.transforms) {
          entry.transforms = sceneDecompressArray(entry.previewTransforms);
          entry._pendingTransforms = entry.compressedTransforms;
          entry._previewActive = true;
          delete entry.previewTransforms;
        }
        if (lod && entry._pendingTransforms) {
          entry._fullTransforms = sceneDecompressArray(entry._pendingTransforms);
          entry._previewTransforms = entry.transforms;
          entry._lodThreshold = lodThreshold;
        }
      } else {
        sceneDecompressInstancedMeshEntry(entry);
      }
    }

    var animations = scene && Array.isArray(scene.animations) ? scene.animations :
                     (props && Array.isArray(props.animations) ? props.animations : []);
    for (var i = 0; i < animations.length; i++) {
      var clip = animations[i];
      var channels = clip && Array.isArray(clip.channels) ? clip.channels : [];
      for (var j = 0; j < channels.length; j++) {
        var channel = channels[j];
        if (progressive || lod) {
          if (channel.previewTimes && !channel.times) {
            channel.times = sceneDecompressArray(channel.previewTimes);
            channel._pendingTimes = channel.compressedTimes;
            channel._previewActive = true;
            delete channel.previewTimes;
          }
          if (channel.previewValues && !channel.values) {
            channel.values = sceneDecompressArray(channel.previewValues);
            channel._pendingValues = channel.compressedValues;
            delete channel.previewValues;
          }
          if (!progressive) {
            sceneDecompressAnimationChannel(channel);
          }
        } else {
          sceneDecompressAnimationChannel(channel);
        }
      }
    }
  }

  function sceneUpgradeProgressive(props) {
    var scene = sceneProps(props);
    var points = scene && Array.isArray(scene.points) ? scene.points :
                 (props && Array.isArray(props.points) ? props.points : []);
    for (var i = 0; i < points.length; i++) {
      var entry = points[i];
      if (entry._pendingPositions) {
        entry.positions = sceneDecompressArray(entry._pendingPositions);
        if (entry.positions && entry._positionStride > 1) {
          entry.positions = sceneReinterleave(entry.positions, entry._positionStride);
        }
        delete entry._pendingPositions;
        delete entry._positionStride;
        delete entry._previewActive;
        delete entry._cachedPos;
      }
      if (entry._pendingSizes) {
        entry.sizes = sceneDecompressArray(entry._pendingSizes);
        delete entry._pendingSizes;
        delete entry._cachedSizes;
      }
    }
    var meshes = scene && Array.isArray(scene.instancedMeshes) ? scene.instancedMeshes :
                 (props && Array.isArray(props.instancedMeshes) ? props.instancedMeshes : []);
    for (var i = 0; i < meshes.length; i++) {
      var entry = meshes[i];
      if (entry._pendingTransforms) {
        entry.transforms = sceneDecompressArray(entry._pendingTransforms);
        delete entry._pendingTransforms;
        delete entry._previewActive;
      }
    }
    var animations = scene && Array.isArray(scene.animations) ? scene.animations :
                     (props && Array.isArray(props.animations) ? props.animations : []);
    for (var i = 0; i < animations.length; i++) {
      var clip = animations[i];
      var channels = clip && Array.isArray(clip.channels) ? clip.channels : [];
      for (var j = 0; j < channels.length; j++) {
        var channel = channels[j];
        if (channel._pendingTimes) {
          channel.times = sceneDecompressArray(channel._pendingTimes);
          delete channel._pendingTimes;
          delete channel._previewActive;
        }
        if (channel._pendingValues) {
          channel.values = sceneDecompressArray(channel._pendingValues);
          delete channel._pendingValues;
        }
      }
    }
  }

  function sceneApplyLOD(entry, cameraX, cameraY, cameraZ) {
    if (!entry._fullPositions || !entry._previewPositions) return;
    var dx = (entry.x || 0) - cameraX;
    var dy = (entry.y || 0) - cameraY;
    var dz = (entry.z || 0) - cameraZ;
    var dist = Math.sqrt(dx * dx + dy * dy + dz * dz);
    var threshold = entry._lodThreshold || 20;
    var wantFull = dist < threshold;
    var hasFull = entry.positions === entry._fullPositions;
    if (wantFull && !hasFull) {
      entry.positions = entry._fullPositions;
      delete entry._cachedPos; // force buffer rebuild
    } else if (!wantFull && hasFull) {
      entry.positions = entry._previewPositions;
      delete entry._cachedPos;
    }
  }

  function sceneSegmentResolution(value) {
    const segments = Math.round(sceneNumber(value, 12));
    return Math.max(6, Math.min(24, segments));
  }

  function boxVertices(width, height, depth) {
    const halfWidth = width / 2;
    const halfHeight = height / 2;
    const halfDepth = depth / 2;
    return [
      { x: -halfWidth, y: -halfHeight, z: -halfDepth },
      { x: halfWidth, y: -halfHeight, z: -halfDepth },
      { x: halfWidth, y: halfHeight, z: -halfDepth },
      { x: -halfWidth, y: halfHeight, z: -halfDepth },
      { x: -halfWidth, y: -halfHeight, z: halfDepth },
      { x: halfWidth, y: -halfHeight, z: halfDepth },
      { x: halfWidth, y: halfHeight, z: halfDepth },
      { x: -halfWidth, y: halfHeight, z: halfDepth },
    ];
  }

  const boxEdgePairs = [
    [0, 1], [1, 2], [2, 3], [3, 0],
    [4, 5], [5, 6], [6, 7], [7, 4],
    [0, 4], [1, 5], [2, 6], [3, 7],
  ];

  function indexSegments(points, edgePairs) {
    return edgePairs.map(function(edge) {
      return [points[edge[0]], points[edge[1]]];
    });
  }

  function boxSegments(object) {
    return indexSegments(boxVertices(object.width, object.height, object.depth), boxEdgePairs);
  }

  function planeSegments(object) {
    const vertices = boxVertices(object.width, 0, object.depth);
    return indexSegments(vertices.slice(0, 4), [
      [0, 1], [1, 2], [2, 3], [3, 0],
    ]);
  }

  function pyramidSegments(object) {
    const halfWidth = object.width / 2;
    const halfDepth = object.depth / 2;
    const halfHeight = object.height / 2;
    const vertices = [
      { x: -halfWidth, y: -halfHeight, z: -halfDepth },
      { x: halfWidth, y: -halfHeight, z: -halfDepth },
      { x: halfWidth, y: -halfHeight, z: halfDepth },
      { x: -halfWidth, y: -halfHeight, z: halfDepth },
      { x: 0, y: halfHeight, z: 0 },
    ];
    return indexSegments(vertices, [
      [0, 1], [1, 2], [2, 3], [3, 0],
      [0, 4], [1, 4], [2, 4], [3, 4],
    ]);
  }

  function circleSegments(radius, axis, segments) {
    const points = [];
    for (let i = 0; i < segments; i += 1) {
      const angle = (Math.PI * 2 * i) / segments;
      points.push(circlePoint(radius, axis, angle));
    }
    const out = [];
    for (let i = 0; i < points.length; i += 1) {
      out.push([points[i], points[(i + 1) % points.length]]);
    }
    return out;
  }

  function circlePoint(radius, axis, angle) {
    const sin = Math.sin(angle) * radius;
    const cos = Math.cos(angle) * radius;
    switch (axis) {
      case "xy":
        return { x: cos, y: sin, z: 0 };
      case "yz":
        return { x: 0, y: cos, z: sin };
      default:
        return { x: cos, y: 0, z: sin };
    }
  }

  function sphereSegments(object) {
    return []
      .concat(circleSegments(object.radius, "xy", object.segments))
      .concat(circleSegments(object.radius, "xz", object.segments))
      .concat(circleSegments(object.radius, "yz", object.segments));
  }

  function lineSegments(object) {
    const points = Array.isArray(object && object.points) ? object.points : [];
    const segments = Array.isArray(object && object.lineSegments) ? object.lineSegments : [];
    const out = [];
    for (const pair of segments) {
      if (!Array.isArray(pair) || pair.length < 2) {
        continue;
      }
      const from = points[pair[0]];
      const to = points[pair[1]];
      if (!from || !to) {
        continue;
      }
      out.push([from, to]);
    }
    return out;
  }

  function sceneObjectSegments(object) {
    switch (object.kind) {
      case "box":
      case "cube":
        return boxSegments(object);
      case "lines":
        return lineSegments(object);
      case "plane":
        return planeSegments(object);
      case "pyramid":
        return pyramidSegments(object);
      case "sphere":
        return sphereSegments(object);
      default:
        return boxSegments(object);
    }
  }

  function scenePlaneLocalCorners(object) {
    return boxVertices(
      sceneNumber(object && object.width, 1),
      0,
      sceneNumber(object && object.depth, sceneNumber(object && object.height, 1)),
    ).slice(0, 4);
  }

  const _scenePlaneSurfaceCornersScratch = [
    { x: 0, y: 0, z: 0 },
    { x: 0, y: 0, z: 0 },
    { x: 0, y: 0, z: 0 },
    { x: 0, y: 0, z: 0 },
  ];

  function scenePlaneSurfaceCorners(object, timeSeconds) {
    const local = scenePlaneLocalCorners(object);
    const out = _scenePlaneSurfaceCornersScratch;
    for (let i = 0; i < 4; i += 1) {
      const p = local[i];
      translateScenePointInto(out[i], p && p.x, p && p.y, p && p.z, object, timeSeconds);
    }
    return out;
  }

  function scenePlaneSurfacePositions(corners) {
    if (!Array.isArray(corners) || corners.length < 4) {
      return [];
    }
    return [
      corners[0].x, corners[0].y, corners[0].z,
      corners[1].x, corners[1].y, corners[1].z,
      corners[2].x, corners[2].y, corners[2].z,
      corners[0].x, corners[0].y, corners[0].z,
      corners[2].x, corners[2].y, corners[2].z,
      corners[3].x, corners[3].y, corners[3].z,
    ];
  }

  function scenePlaneSurfaceUVs() {
    return [
      0, 1,
      1, 1,
      1, 0,
      0, 1,
      1, 0,
      0, 0,
    ];
  }

  function normalizeSceneMaterialKind(value) {
    const kind = typeof value === "string" ? value.trim().toLowerCase() : "";
    switch (kind) {
      case "ghost":
      case "glass":
      case "glow":
      case "matte":
        return kind;
      default:
        return "flat";
    }
  }

  function sceneDefaultMaterialOpacity(kind) {
    switch (normalizeSceneMaterialKind(kind)) {
      case "ghost":
        return 0.42;
      case "glass":
        return 0.28;
      case "glow":
        return 0.92;
      default:
        return 1;
    }
  }

  function sceneDefaultMaterialEmissive(kind) {
    switch (normalizeSceneMaterialKind(kind)) {
      case "ghost":
        return 0.12;
      case "glass":
        return 0.08;
      case "glow":
        return 0.42;
      default:
        return 0;
    }
  }

  function sceneDefaultMaterialBlendMode(kind) {
    switch (normalizeSceneMaterialKind(kind)) {
      case "ghost":
      case "glass":
        return "alpha";
      case "glow":
        return "additive";
      default:
        return "opaque";
    }
  }

  function normalizeSceneMaterialBlendMode(value, kind, opacity) {
    const mode = typeof value === "string" ? value.trim().toLowerCase() : "";
    switch (mode) {
      case "opaque":
      case "solid":
        return "opaque";
      case "alpha":
      case "transparent":
      case "translucent":
        return "alpha";
      case "add":
      case "additive":
      case "glow":
      case "emissive":
        return "additive";
      default: {
        const fallback = sceneDefaultMaterialBlendMode(kind);
        if (fallback !== "opaque") {
          return fallback;
        }
        return opacity < 0.999 ? "alpha" : "opaque";
      }
    }
  }

  function normalizeSceneMaterialRenderPass(value, blendMode, opacity) {
    const pass = typeof value === "string" ? value.trim().toLowerCase() : "";
    switch (pass) {
      case "opaque":
      case "alpha":
      case "additive":
        return pass;
      case "add":
        return "additive";
      case "transparent":
      case "translucent":
        return "alpha";
      default:
        if (blendMode === "additive") {
          return "additive";
        }
        return blendMode === "alpha" || opacity < 0.999 ? "alpha" : "opaque";
    }
  }

  function sceneObjectMaterialSource(item) {
    return item && item.material && typeof item.material === "object" ? item.material : null;
  }

  function sceneObjectMaterialKindValue(item) {
    if (!item || typeof item !== "object") {
      return "";
    }
    if (typeof item.material === "string" && item.material.trim()) {
      return item.material.trim();
    }
    if (typeof item.materialKind === "string" && item.materialKind.trim()) {
      return item.materialKind.trim();
    }
    const material = sceneObjectMaterialSource(item);
    if (material && typeof material.kind === "string" && material.kind.trim()) {
      return material.kind.trim();
    }
    return "";
  }

  function sceneObjectMaterialValue(item, name) {
    if (!item || typeof item !== "object") {
      return undefined;
    }
    const material = sceneObjectMaterialSource(item);
    if (material && Object.prototype.hasOwnProperty.call(material, name)) {
      return material[name];
    }
    return Object.prototype.hasOwnProperty.call(item, name) ? item[name] : undefined;
  }

  function sceneObjectMaterialHasValue(item, name) {
    if (!item || typeof item !== "object") {
      return false;
    }
    const material = sceneObjectMaterialSource(item);
    if (material && Object.prototype.hasOwnProperty.call(material, name)) {
      return true;
    }
    return Object.prototype.hasOwnProperty.call(item, name);
  }

  function sceneObjectBlendModeValue(item) {
    const direct = sceneObjectMaterialValue(item, "blendMode");
    if (direct !== undefined) {
      return direct;
    }
    const material = sceneObjectMaterialSource(item);
    if (material && Object.prototype.hasOwnProperty.call(material, "blend")) {
      return material.blend;
    }
    return item && Object.prototype.hasOwnProperty.call(item, "blend") ? item.blend : undefined;
  }

  function sceneObjectBlendModeHasValue(item) {
    if (!item || typeof item !== "object") {
      return false;
    }
    if (sceneObjectMaterialHasValue(item, "blendMode")) {
      return true;
    }
    const material = sceneObjectMaterialSource(item);
    if (material && Object.prototype.hasOwnProperty.call(material, "blend")) {
      return true;
    }
    return Object.prototype.hasOwnProperty.call(item, "blend");
  }

  function sceneObjectMaterialProfile(object) {
    const kind = normalizeSceneMaterialKind(object && object.materialKind);
    const opacity = clamp01(sceneNumber(object && object.opacity, sceneDefaultMaterialOpacity(kind)));
    const profile = {
      kind,
      color: object && typeof object.color === "string" && object.color ? object.color : "#8de1ff",
      texture: object && typeof object.texture === "string" ? object.texture.trim() : "",
      opacity,
      wireframe: sceneBool(object && object.wireframe, true),
      blendMode: normalizeSceneMaterialBlendMode(object && object.blendMode, kind, opacity),
      emissive: clamp01(sceneNumber(object && object.emissive, sceneDefaultMaterialEmissive(kind))),
    };
    profile.renderPass = normalizeSceneMaterialRenderPass(object && object.renderPass, profile.blendMode, profile.opacity);
    profile.key = sceneMaterialProfileKey(profile);
    profile.shaderData = sceneMaterialShaderData(profile);
    return profile;
  }

  function sceneMaterialProfileKey(profile) {
    return [
      normalizeSceneMaterialKind(profile && profile.kind),
      String(profile && profile.color || ""),
      String(profile && profile.texture || ""),
      clamp01(sceneNumber(profile && profile.opacity, 1)).toFixed(3),
      String(sceneBool(profile && profile.wireframe, true)),
      String(profile && profile.blendMode || "opaque"),
      String(profile && profile.renderPass || "opaque"),
      clamp01(sceneNumber(profile && profile.emissive, 0)).toFixed(3),
    ].join("|");
  }

  function sceneBundleMaterialIndex(bundle, materialLookup, profile) {
    if (!bundle || !Array.isArray(bundle.materials)) {
      return 0;
    }
    const key = profile && profile.key ? profile.key : sceneMaterialProfileKey(profile);
    if (materialLookup && materialLookup.has(key)) {
      return materialLookup.get(key);
    }
    const index = bundle.materials.length;
    bundle.materials.push(profile);
    if (materialLookup) {
      materialLookup.set(key, index);
    }
    return index;
  }

  function sceneMaterialStrokeColor(material) {
    const rgba = sceneColorRGBA(material && material.color, [0.55, 0.88, 1, 1]);
    rgba[3] = clamp01(rgba[3] * sceneMaterialOpacity(material));
    return "rgba(" +
      Math.round(rgba[0] * 255) + ", " +
      Math.round(rgba[1] * 255) + ", " +
      Math.round(rgba[2] * 255) + ", " +
      rgba[3].toFixed(3) + ")";
  }

  function sceneMaterialOpacity(material) {
    if (!material || typeof material !== "object") {
      return 1;
    }
    return clamp01(sceneNumber(material.opacity, 1));
  }

  function sceneMaterialEmissive(material) {
    if (!material || typeof material !== "object") {
      return 0;
    }
    return clamp01(sceneNumber(material.emissive, 0));
  }

  function sceneMaterialUsesAlpha(material) {
    return sceneMaterialRenderPass(material) !== "opaque";
  }

  function sceneMaterialRenderPass(material) {
    if (!material || typeof material !== "object") {
      return "opaque";
    }
    const renderPass = String(material.renderPass || "").toLowerCase();
    if (renderPass === "opaque" || renderPass === "alpha" || renderPass === "additive") {
      return renderPass;
    }
    const blendMode = String(material.blendMode || "").toLowerCase();
    if (blendMode === "additive") {
      return "additive";
    }
    if (blendMode === "alpha" || sceneMaterialOpacity(material) < 0.999) {
      return "alpha";
    }
    return "opaque";
  }

  function sceneMaterialShaderData(material) {
    if (material && Array.isArray(material.shaderData) && material.shaderData.length >= 3) {
      return material.shaderData;
    }
    if (!material || typeof material !== "object") {
      return [0, 0, 1];
    }
    const kind = String(material.kind || "").toLowerCase();
    switch (kind) {
    case "ghost":
      return [1, sceneMaterialEmissive(material), 0.3];
    case "glass":
      return [2, sceneMaterialEmissive(material), 0.7];
    case "glow":
      return [3, sceneMaterialEmissive(material), 1];
    case "matte":
      return [4, sceneMaterialEmissive(material), 0.2];
    default:
      return [0, sceneMaterialEmissive(material), 1];
    }
  }

  function sceneFallbackMaterialData(vertexCount) {
    const values = new Float32Array(vertexCount * 3);
    for (let i = 0; i < vertexCount; i += 1) {
      values[i * 3 + 2] = 1;
    }
    return values;
  }

  function sceneLightingActive(lights, environment) {
    return Boolean(
      (Array.isArray(lights) && lights.length > 0) ||
      sceneNumber(environment && environment.ambientIntensity, 0) > 0 ||
      sceneNumber(environment && environment.skyIntensity, 0) > 0 ||
      sceneNumber(environment && environment.groundIntensity, 0) > 0
    );
  }

  function sceneEnvironmentLightContribution(baseColor, normal, environment) {
    let lighting = { x: 0, y: 0, z: 0 };
    if (sceneNumber(environment && environment.ambientIntensity, 0) > 0) {
      lighting = sceneAddPoint(lighting, sceneMultiplyPoint(
        baseColor,
        sceneScalePoint(sceneColorPoint(environment && environment.ambientColor, { x: 1, y: 1, z: 1 }), sceneNumber(environment && environment.ambientIntensity, 0)),
      ));
    }
    if (sceneNumber(environment && environment.skyIntensity, 0) > 0 || sceneNumber(environment && environment.groundIntensity, 0) > 0) {
      const hemi = clamp01((normal.y * 0.5) + 0.5);
      const sky = sceneScalePoint(sceneColorPoint(environment && environment.skyColor, { x: 0.88, y: 0.94, z: 1 }), sceneNumber(environment && environment.skyIntensity, 0) * hemi);
      const ground = sceneScalePoint(sceneColorPoint(environment && environment.groundColor, { x: 0.12, y: 0.16, z: 0.22 }), sceneNumber(environment && environment.groundIntensity, 0) * (1 - hemi));
      lighting = sceneAddPoint(lighting, sceneMultiplyPoint(baseColor, sceneAddPoint(sky, ground)));
    }
    return lighting;
  }

  function sceneAmbientLightContribution(baseColor, light) {
    return sceneMultiplyPoint(
      baseColor,
      sceneScalePoint(sceneColorPoint(light && light.color, { x: 1, y: 1, z: 1 }), sceneNumber(light && light.intensity, 0)),
    );
  }

  function sceneDirectionalLightContribution(baseColor, normal, light) {
    const direction = sceneNormalizePoint({
      x: -sceneNumber(light && light.directionX, 0),
      y: -sceneNumber(light && light.directionY, -1),
      z: -sceneNumber(light && light.directionZ, 0),
    });
    const diffuse = clamp01(sceneDotPoint(normal, direction));
    if (diffuse <= 0) {
      return { x: 0, y: 0, z: 0 };
    }
    return sceneMultiplyPoint(
      baseColor,
      sceneScalePoint(sceneColorPoint(light && light.color, { x: 1, y: 1, z: 1 }), sceneNumber(light && light.intensity, 0) * diffuse),
    );
  }

  function scenePointLightContribution(baseColor, worldPoint, normal, light) {
    const offset = {
      x: sceneNumber(light && light.x, 0) - sceneNumber(worldPoint && worldPoint.x, 0),
      y: sceneNumber(light && light.y, 0) - sceneNumber(worldPoint && worldPoint.y, 0),
      z: sceneNumber(light && light.z, 0) - sceneNumber(worldPoint && worldPoint.z, 0),
    };
    const distance = Math.max(0.0001, scenePointLength(offset));
    const diffuse = clamp01(sceneDotPoint(normal, sceneScalePoint(offset, 1 / distance)));
    if (diffuse <= 0) {
      return { x: 0, y: 0, z: 0 };
    }
    const attenuation = scenePointLightAttenuation(light, distance);
    if (attenuation <= 0) {
      return { x: 0, y: 0, z: 0 };
    }
    return sceneMultiplyPoint(
      baseColor,
      sceneScalePoint(sceneColorPoint(light && light.color, { x: 1, y: 1, z: 1 }), sceneNumber(light && light.intensity, 0) * diffuse * attenuation),
    );
  }

  function sceneLightContribution(baseColor, worldPoint, normal, light) {
    switch (light && light.kind) {
      case "ambient":
        return sceneAmbientLightContribution(baseColor, light);
      case "directional":
        return sceneDirectionalLightContribution(baseColor, normal, light);
      case "point":
        return scenePointLightContribution(baseColor, worldPoint, normal, light);
      default:
        return { x: 0, y: 0, z: 0 };
    }
  }

  function sceneLitColorRGBA(material, worldPoint, normal, lights, environment) {
    const base = sceneColorRGBA(material && material.color, [0.55, 0.88, 1, 1]);
    if (!sceneLightingActive(lights, environment)) {
      return base;
    }
    const safeNormal = sceneSafeNormal(normal);
    const baseColor = { x: base[0], y: base[1], z: base[2] };
    const emissive = clamp01(sceneMaterialEmissive(material));
    let lighting = sceneEnvironmentLightContribution(baseColor, safeNormal, environment);
    for (const light of Array.isArray(lights) ? lights : []) {
      lighting = sceneAddPoint(lighting, sceneLightContribution(baseColor, worldPoint, safeNormal, light));
    }
    const exposure = sceneNumber(environment && environment.exposure, 1);
    let lit = sceneAddPoint(
      sceneScalePoint(baseColor, emissive),
      sceneScalePoint(lighting, exposure),
    );
    lit = sceneAddPoint(lit, sceneScalePoint(baseColor, 0.06));
    return [clamp01(lit.x), clamp01(lit.y), clamp01(lit.z), base[3]];
  }

  function sceneObjectWorldNormal(object, point, timeSeconds) {
    const localPoint = {
      x: sceneNumber(point && point.x, 0) * sceneNumber(object && object.scaleX, 1),
      y: sceneNumber(point && point.y, 0) * sceneNumber(object && object.scaleY, 1),
      z: sceneNumber(point && point.z, 0) * sceneNumber(object && object.scaleZ, 1),
    };
    return sceneNormalizePoint(sceneRotatePoint(
      sceneObjectLocalNormal(object, localPoint),
      sceneNumber(object && object.rotationX, 0) + sceneNumber(object && object.spinX, 0) * timeSeconds,
      sceneNumber(object && object.rotationY, 0) + sceneNumber(object && object.spinY, 0) * timeSeconds,
      sceneNumber(object && object.rotationZ, 0) + sceneNumber(object && object.spinZ, 0) * timeSeconds,
    ));
  }

  function sceneObjectLocalNormal(object, point) {
    const safePoint = point && typeof point === "object" ? point : { x: 0, y: 0, z: 0 };
    switch (object && object.kind) {
      case "lines":
        return sceneBoxNormal(object, safePoint);
      case "plane":
        return { x: 0, y: 1, z: 0 };
      case "sphere":
        return sceneNormalizePoint(safePoint);
      case "pyramid":
        return scenePyramidNormal(object, safePoint);
      default:
        return sceneBoxNormal(object, safePoint);
    }
  }

  function scenePyramidNormal(object, point) {
    const width = Math.max((sceneNumber(object && object.width, object && object.size) * Math.abs(sceneNumber(object && object.scaleX, 1))) / 2, 0.0001);
    const height = Math.max((sceneNumber(object && object.height, object && object.size) * Math.abs(sceneNumber(object && object.scaleY, 1))) / 2, 0.0001);
    const depth = Math.max((sceneNumber(object && object.depth, object && object.size) * Math.abs(sceneNumber(object && object.scaleZ, 1))) / 2, 0.0001);
    return sceneNormalizePoint({
      x: sceneNumber(point && point.x, 0) / width,
      y: (sceneNumber(point && point.y, 0) / height) + 0.35,
      z: sceneNumber(point && point.z, 0) / depth,
    });
  }

  function sceneBoxNormal(object, point) {
    const width = Math.max((sceneNumber(object && object.width, object && object.size) * Math.abs(sceneNumber(object && object.scaleX, 1))) / 2, 0.0001);
    const height = Math.max((sceneNumber(object && object.height, object && object.size) * Math.abs(sceneNumber(object && object.scaleY, 1))) / 2, 0.0001);
    const depth = Math.max((sceneNumber(object && object.depth, object && object.size) * Math.abs(sceneNumber(object && object.scaleZ, 1))) / 2, 0.0001);
    const x = sceneNumber(point && point.x, 0);
    const y = sceneNumber(point && point.y, 0);
    const z = sceneNumber(point && point.z, 0);
    const ax = Math.abs(x / width);
    const ay = Math.abs(y / height);
    const az = Math.abs(z / depth);
    if (ax >= ay && ax >= az) {
      return { x: Math.sign(x) || 1, y: 0, z: 0 };
    }
    if (ay >= az) {
      return { x: 0, y: Math.sign(y) || 1, z: 0 };
    }
    return { x: 0, y: 0, z: Math.sign(z) || 1 };
  }

  function scenePointLightAttenuation(light, distance) {
    const range = Math.max(0, sceneNumber(light && light.range, 0));
    const decay = Math.max(0.1, sceneNumber(light && light.decay, 1.35));
    if (range > 0) {
      return Math.pow(clamp01(1 - (distance / range)), decay);
    }
    return 1 / (1 + Math.pow(distance * 0.35, Math.max(decay, 1)));
  }

  function buildSceneWorldDrawPlan(bundle, scratch) {
    const objects = Array.isArray(bundle.objects) ? bundle.objects : [];
    const materials = Array.isArray(bundle.materials) ? bundle.materials : [];
    if (!objects.length || !materials.length) {
      return null;
    }
    const drawScratch = resetSceneWorldDrawScratch(scratch || createSceneWorldDrawScratch());
    for (let index = 0; index < objects.length; index += 1) {
      collectSceneWorldDrawObject(drawScratch, bundle, materials, objects[index], index);
    }
    return finalizeSceneWorldDrawPlan(bundle, drawScratch);
  }

  function collectSceneWorldDrawObject(drawScratch, bundle, materials, object, order) {
    if (!sceneWorldObjectRenderable(object, bundle && bundle.camera)) {
      return;
    }
    const material = materials[object.materialIndex] || null;
    if (!sceneWorldObjectUsesLinePass(object, material)) {
      return;
    }
    const renderPass = sceneWorldObjectRenderPass(object, material);
    if (renderPass === "additive" || renderPass === "alpha") {
      collectSceneWorldTranslucentObject(drawScratch, bundle, object, material, renderPass, order);
      return;
    }
    collectSceneWorldOpaqueObject(drawScratch, bundle, object, material);
  }

  function sceneWorldObjectRenderable(object, camera) {
    return Boolean(
      object &&
      Number.isFinite(object.vertexOffset) &&
      Number.isFinite(object.vertexCount) &&
      object.vertexCount > 0 &&
      !sceneWorldObjectCulled(object, camera)
    );
  }

  function sceneWorldObjectUsesLinePass(object, material) {
    return !(object && object.kind === "plane" && material && typeof material.texture === "string" && material.texture.trim() !== "" && !sceneBool(material.wireframe, true));
  }

  function collectSceneWorldOpaqueObject(drawScratch, bundle, object, material) {
    const target = object.static ? {
      positions: drawScratch.staticOpaquePositions,
      colors: drawScratch.staticOpaqueColors,
      materials: drawScratch.staticOpaqueMaterials,
    } : {
      positions: drawScratch.dynamicOpaquePositions,
      colors: drawScratch.dynamicOpaqueColors,
      materials: drawScratch.dynamicOpaqueMaterials,
    };
    if (object.static) {
      drawScratch.staticOpaqueObjects.push(object);
      drawScratch.staticOpaqueMaterialProfiles.push(material);
    }
    appendSceneWorldObjectSlice(target.positions, target.colors, target.materials, bundle.worldPositions, bundle.worldColors, object, material);
  }

  function collectSceneWorldTranslucentObject(drawScratch, bundle, object, material, renderPass, order) {
    const targetEntries = renderPass === "additive" ? drawScratch.additiveEntries : drawScratch.alphaEntries;
    targetEntries.push(createSceneWorldPassEntry(object, material, bundle.worldPositions, bundle.camera, order));
  }

  function finalizeSceneWorldDrawPlan(bundle, drawScratch) {
    const typedStaticOpaque = createSceneWorldOpaqueBuffers(drawScratch, "typedStaticOpaque", drawScratch.staticOpaquePositions, drawScratch.staticOpaqueColors, drawScratch.staticOpaqueMaterials);
    const typedDynamicOpaque = createSceneWorldOpaqueBuffers(drawScratch, "typedDynamicOpaque", drawScratch.dynamicOpaquePositions, drawScratch.dynamicOpaqueColors, drawScratch.dynamicOpaqueMaterials);
    const typedAlphaPlan = createSceneWorldPassPlan(bundle.worldPositions, bundle.worldColors, drawScratch.alphaEntries, drawScratch.alphaPlan);
    const typedAdditivePlan = createSceneWorldPassPlan(bundle.worldPositions, bundle.worldColors, drawScratch.additiveEntries, drawScratch.additivePlan);
    const plan = drawScratch.plan;
    plan.staticOpaqueKey = sceneStaticDrawKey(bundle, drawScratch.staticOpaqueObjects, drawScratch.staticOpaqueMaterialProfiles, bundle.camera);
    plan.staticOpaquePositions = typedStaticOpaque.positions;
    plan.staticOpaqueColors = typedStaticOpaque.colors;
    plan.staticOpaqueMaterials = typedStaticOpaque.materials;
    plan.staticOpaqueVertexCount = typedStaticOpaque.vertexCount;
    plan.dynamicOpaquePositions = typedDynamicOpaque.positions;
    plan.dynamicOpaqueColors = typedDynamicOpaque.colors;
    plan.dynamicOpaqueMaterials = typedDynamicOpaque.materials;
    plan.dynamicOpaqueVertexCount = typedDynamicOpaque.vertexCount;
    plan.alphaPositions = typedAlphaPlan.positions;
    plan.alphaColors = typedAlphaPlan.colors;
    plan.alphaMaterials = typedAlphaPlan.materials;
    plan.alphaVertexCount = typedAlphaPlan.vertexCount;
    plan.additivePositions = typedAdditivePlan.positions;
    plan.additiveColors = typedAdditivePlan.colors;
    plan.additiveMaterials = typedAdditivePlan.materials;
    plan.additiveVertexCount = typedAdditivePlan.vertexCount;
    plan.hasAlphaPass = typedAlphaPlan.vertexCount > 0;
    plan.hasAdditivePass = typedAdditivePlan.vertexCount > 0;
    return plan;
  }

  function createSceneWorldOpaqueBuffers(drawScratch, keyPrefix, positions, colors, materials) {
    const typedPositions = sceneWriteFloatArray(drawScratch, keyPrefix + "Positions", positions);
    const typedColors = sceneWriteFloatArray(drawScratch, keyPrefix + "Colors", colors);
    const typedMaterials = sceneWriteFloatArray(drawScratch, keyPrefix + "Materials", materials);
    return {
      positions: typedPositions,
      colors: typedColors,
      materials: typedMaterials,
      vertexCount: typedPositions.length / 3,
    };
  }

  function createSceneWorldPassEntry(object, material, sourcePositions, camera, order) {
    return {
      object,
      material,
      order,
      depth: sceneWorldObjectDepth(sourcePositions, object, camera),
    };
  }

  function createSceneWorldPassPlan(sourcePositions, sourceColors, entries, scratch) {
    const passScratch = resetSceneWorldPassScratch(scratch || createSceneWorldPassScratch());
    if (!entries.length) {
      passScratch.typedPositions = sceneWriteFloatArray(passScratch, "typedPositions", passScratch.positions);
      passScratch.typedColors = sceneWriteFloatArray(passScratch, "typedColors", passScratch.colors);
      passScratch.typedMaterials = sceneWriteFloatArray(passScratch, "typedMaterials", passScratch.materials);
      passScratch.vertexCount = 0;
      return passScratch;
    }
    const positions = passScratch.positions;
    const colors = passScratch.colors;
    const materials = passScratch.materials;
    entries.sort(compareSceneWorldPassEntries);
    for (const entry of entries) {
      appendSceneWorldObjectSlice(positions, colors, materials, sourcePositions, sourceColors, entry.object, entry.material);
    }
    passScratch.typedPositions = sceneWriteFloatArray(passScratch, "typedPositions", positions);
    passScratch.typedColors = sceneWriteFloatArray(passScratch, "typedColors", colors);
    passScratch.typedMaterials = sceneWriteFloatArray(passScratch, "typedMaterials", materials);
    passScratch.vertexCount = passScratch.typedPositions.length / 3;
    return passScratch;
  }

  function compareSceneWorldPassEntries(a, b) {
    if (a.depth !== b.depth) {
      return b.depth - a.depth;
    }
    return a.order - b.order;
  }

  const _sceneWorldObjectDepthCameraScratch = {
    x: 0, y: 0, z: 0,
    rotationX: 0, rotationY: 0, rotationZ: 0,
    fov: 0, near: 0, far: 0,
  };

  function sceneWorldObjectDepth(sourcePositions, object, camera) {
    if (object && object.bounds) {
      return sceneBoundsDepthMetrics(object.bounds, camera).center;
    }
    if (object && Number.isFinite(object.depthCenter)) {
      return sceneNumber(object.depthCenter, sceneWorldPointDepth(0, camera));
    }
    const vertexOffset = Math.max(0, Math.floor(sceneNumber(object && object.vertexOffset, 0)));
    const vertexCount = Math.max(0, Math.floor(sceneNumber(object && object.vertexCount, 0)));
    if (!vertexCount) {
      return sceneWorldPointDepth(0, camera);
    }

    const cam = sceneRenderCamera(camera, _sceneWorldObjectDepthCameraScratch);
    const sinX = Math.sin(-cam.rotationX);
    const cosX = Math.cos(-cam.rotationX);
    const sinY = Math.sin(-cam.rotationY);
    const cosY = Math.cos(-cam.rotationY);
    const sinZ = Math.sin(-cam.rotationZ);
    const cosZ = Math.cos(-cam.rotationZ);

    const start = vertexOffset * 3;
    const end = start + vertexCount * 3;
    let depthSum = 0;
    let count = 0;
    for (let i = start; i < end; i += 3) {
      let lx = sceneNumber(sourcePositions[i], 0) - cam.x;
      let ly = sceneNumber(sourcePositions[i + 1], 0) - cam.y;
      let lz = sceneNumber(sourcePositions[i + 2], 0) + cam.z;

      let nX = lx * cosZ - ly * sinZ;
      let nY = lx * sinZ + ly * cosZ;
      lx = nX;
      ly = nY;

      nX = lx * cosY + lz * sinY;
      let nZ = -lx * sinY + lz * cosY;
      lx = nX;
      lz = nZ;

      nZ = ly * sinX + lz * cosX;
      depthSum += nZ;
      count += 1;
    }
    return depthSum / Math.max(1, count);
  }

  function sceneWorldObjectCulled(object, camera) {
    if (object && object.bounds) {
      return sceneBoundsViewCulled(object.bounds, camera);
    }
    return Boolean(object && object.viewCulled);
  }

  function appendSceneWorldObjectSlice(targetPositions, targetColors, targetMaterials, sourcePositions, sourceColors, object, material) {
    const vertexOffset = Math.max(0, Math.floor(sceneNumber(object.vertexOffset, 0)));
    const vertexCount = Math.max(0, Math.floor(sceneNumber(object.vertexCount, 0)));
    const opacity = sceneMaterialOpacity(material);
    const materialData = sceneMaterialShaderData(material);
    const startPosition = vertexOffset * 3;
    const endPosition = startPosition + vertexCount * 3;
    const startColor = vertexOffset * 4;
    const endColor = startColor + vertexCount * 4;
    for (let i = startPosition; i < endPosition; i += 1) {
      targetPositions.push(sceneNumber(sourcePositions[i], 0));
    }
    for (let i = startColor; i < endColor; i += 4) {
      targetColors.push(
        sceneNumber(sourceColors[i], 0),
        sceneNumber(sourceColors[i + 1], 0),
        sceneNumber(sourceColors[i + 2], 0),
        sceneNumber(sourceColors[i + 3], 1) * opacity,
      );
      targetMaterials.push(materialData[0], materialData[1], materialData[2]);
    }
  }

  function createSceneWorldDrawScratch() {
    return {
      staticOpaquePositions: [],
      staticOpaqueColors: [],
      staticOpaqueMaterials: [],
      dynamicOpaquePositions: [],
      dynamicOpaqueColors: [],
      dynamicOpaqueMaterials: [],
      staticOpaqueObjects: [],
      staticOpaqueMaterialProfiles: [],
      alphaEntries: [],
      additiveEntries: [],
      typedStaticOpaquePositions: new Float32Array(0),
      typedStaticOpaqueColors: new Float32Array(0),
      typedStaticOpaqueMaterials: new Float32Array(0),
      typedDynamicOpaquePositions: new Float32Array(0),
      typedDynamicOpaqueColors: new Float32Array(0),
      typedDynamicOpaqueMaterials: new Float32Array(0),
      alphaPlan: createSceneWorldPassScratch(),
      additivePlan: createSceneWorldPassScratch(),
      plan: {},
    };
  }

  function resetSceneWorldDrawScratch(scratch) {
    scratch.staticOpaquePositions.length = 0;
    scratch.staticOpaqueColors.length = 0;
    scratch.staticOpaqueMaterials.length = 0;
    scratch.dynamicOpaquePositions.length = 0;
    scratch.dynamicOpaqueColors.length = 0;
    scratch.dynamicOpaqueMaterials.length = 0;
    scratch.staticOpaqueObjects.length = 0;
    scratch.staticOpaqueMaterialProfiles.length = 0;
    scratch.alphaEntries.length = 0;
    scratch.additiveEntries.length = 0;
    resetSceneWorldPassScratch(scratch.alphaPlan);
    resetSceneWorldPassScratch(scratch.additivePlan);
    return scratch;
  }

  function createSceneWorldPassScratch() {
    return {
      positions: [],
      colors: [],
      materials: [],
      typedPositions: new Float32Array(0),
      typedColors: new Float32Array(0),
      typedMaterials: new Float32Array(0),
      vertexCount: 0,
    };
  }

  function resetSceneWorldPassScratch(scratch) {
    scratch.positions.length = 0;
    scratch.colors.length = 0;
    scratch.materials.length = 0;
    scratch.vertexCount = 0;
    return scratch;
  }

  function sceneWriteFloatArray(target, key, values) {
    let buffer = target[key];
    if (!buffer || buffer.length !== values.length) {
      buffer = new Float32Array(values.length);
      target[key] = buffer;
    }
    for (let i = 0; i < values.length; i += 1) {
      buffer[i] = sceneNumber(values[i], 0);
    }
    return buffer;
  }

  function sceneWorldPointDepth(pointOrZ, camera) {
    if (pointOrZ && typeof pointOrZ === "object") {
      return sceneCameraLocalPoint(pointOrZ, camera).z;
    }
    return sceneCameraLocalPoint({ x: 0, y: 0, z: sceneNumber(pointOrZ, 0) }, camera).z;
  }

  function sceneWorldObjectRenderPass(object, material) {
    const renderPass = String(object && object.renderPass || "").toLowerCase();
    if (renderPass === "opaque" || renderPass === "alpha" || renderPass === "additive") {
      return renderPass;
    }
    return sceneMaterialRenderPass(material);
  }

  function sceneStaticDrawKey(bundle, objects, materials, camera) {
    let hash = 2166136261 >>> 0;
    hash = sceneHashCamera(hash, camera);
    for (const object of objects) {
      const material = object && object.materialIndex >= 0 && object.materialIndex < materials.length ? materials[object.materialIndex] : null;
      if (!sceneWorldObjectUsesLinePass(object, material)) {
        continue;
      }
      hash = sceneHashStaticObject(hash, object);
      const start = Math.max(0, Math.floor(sceneNumber(object && object.vertexOffset, 0))) * 4;
      const end = start + Math.max(0, Math.floor(sceneNumber(object && object.vertexCount, 0))) * 4;
      const colors = bundle && bundle.worldColors;
      for (let index = start; index < end; index += 1) {
        hash = sceneHashNumber(hash, sceneNumber(colors && colors[index], 0));
      }
    }
    for (const material of materials) {
      hash = sceneHashMaterialProfile(hash, material);
    }
    return String(hash) + ":" + objects.length + ":" + materials.length;
  }

  function sceneHashCamera(hash, camera) {
    hash = sceneHashNumber(hash, sceneNumber(camera && camera.x, 0));
    hash = sceneHashNumber(hash, sceneNumber(camera && camera.y, 0));
    hash = sceneHashNumber(hash, sceneNumber(camera && camera.z, 6));
    hash = sceneHashNumber(hash, sceneNumber(camera && camera.rotationX, 0));
    hash = sceneHashNumber(hash, sceneNumber(camera && camera.rotationY, 0));
    hash = sceneHashNumber(hash, sceneNumber(camera && camera.rotationZ, 0));
    hash = sceneHashNumber(hash, sceneNumber(camera && camera.fov, 75));
    hash = sceneHashNumber(hash, sceneNumber(camera && camera.near, 0.05));
    return sceneHashNumber(hash, sceneNumber(camera && camera.far, 128));
  }

  const sceneStaticObjectStringFields = ["id", "kind"];
  const sceneStaticObjectNumberFields = [
    ["materialIndex", 0],
    ["vertexOffset", 0],
    ["vertexCount", 0],
    ["depthNear", 0],
    ["depthFar", 0],
    ["depthCenter", 0],
  ];
  const sceneStaticObjectFlagFields = ["static", "viewCulled"];
  const sceneBoundsNumberFields = [
    ["minX", 0],
    ["minY", 0],
    ["minZ", 0],
    ["maxX", 0],
    ["maxY", 0],
    ["maxZ", 0],
  ];
  const sceneMaterialStringFields = ["kind", "color", "texture", "blendMode", "renderPass"];
  const sceneMaterialNumberFields = [
    ["opacity", 1],
    ["emissive", 0],
  ];
  const sceneMaterialFlagFields = ["wireframe"];

  function sceneHashStaticObject(hash, object) {
    hash = sceneHashFieldStrings(hash, object, sceneStaticObjectStringFields);
    hash = sceneHashFieldNumbers(hash, object, sceneStaticObjectNumberFields);
    hash = sceneHashFieldFlags(hash, object, sceneStaticObjectFlagFields);
    return sceneHashBounds(hash, object && object.bounds);
  }

  function sceneHashBounds(hash, bounds) {
    return sceneHashFieldNumbers(hash, bounds, sceneBoundsNumberFields);
  }

  function sceneHashMaterialProfile(hash, material) {
    const key = material && material.key;
    if (key) {
      return sceneHashString(hash, key);
    }
    hash = sceneHashFieldStrings(hash, material, sceneMaterialStringFields);
    hash = sceneHashFieldNumbers(hash, material, sceneMaterialNumberFields);
    return sceneHashFieldFlags(hash, material, sceneMaterialFlagFields);
  }

  function sceneHashFieldStrings(hash, source, fields) {
    for (const field of fields) {
      hash = sceneHashString(hash, source && source[field] || "");
    }
    return hash;
  }

  function sceneHashFieldNumbers(hash, source, fields) {
    for (const field of fields) {
      hash = sceneHashNumber(hash, sceneNumber(source && source[field[0]], field[1]));
    }
    return hash;
  }

  function sceneHashFieldFlags(hash, source, fields) {
    for (const field of fields) {
      hash = sceneHashNumber(hash, source && source[field] ? 1 : 0);
    }
    return hash;
  }

  function sceneHashNumber(hash, value) {
    const scaled = Math.round(sceneNumber(value, 0) * 1000);
    hash ^= scaled;
    return Math.imul(hash, 16777619) >>> 0;
  }

  function sceneHashString(hash, value) {
    const text = String(value || "");
    for (let i = 0; i < text.length; i += 1) {
      hash ^= text.charCodeAt(i);
      hash = Math.imul(hash, 16777619) >>> 0;
    }
    return hash;
  }

function resolvePostFXFactor(maxPixels, canvasPixels) {
  var cap = (typeof maxPixels === "number" && maxPixels > 0)
    ? maxPixels
    : 2073600; // PostFXMaxPixels1080p default
  if (canvasPixels <= cap) return 1;
  return Math.sqrt(cap / canvasPixels);
}

function resolveShadowSize(requestedSize, shadowMaxPixels) {
  var size = Math.max(1, requestedSize | 0);
  var cap = (typeof shadowMaxPixels === "number" && shadowMaxPixels > 0)
    ? shadowMaxPixels
    : 1048576; // ShadowMaxPixels1024 default
  var requestedPixels = size * size;
  if (requestedPixels <= cap) return size;
  var factor = Math.sqrt(cap / requestedPixels);
  return Math.max(1, Math.floor(size * factor));
}

  var SCENE_POST_TONE_MAPPING = "toneMapping";
  var SCENE_POST_BLOOM = "bloom";
  var SCENE_POST_VIGNETTE = "vignette";
  var SCENE_POST_COLOR_GRADE = "colorGrade";

  const SCENE_PBR_VERTEX_SOURCE = [
    "#version 300 es",
    "precision highp float;",
    "precision highp int;",
    "",
    "in vec3 a_position;",
    "in vec3 a_normal;",
    "in vec2 a_uv;",
    "in vec4 a_tangent;",
    "",
    "uniform mat4 u_viewMatrix;",
    "uniform mat4 u_projectionMatrix;",
    "",
    "out vec3 v_worldPosition;",
    "out vec3 v_normal;",
    "out vec2 v_uv;",
    "out vec3 v_tangent;",
    "out vec3 v_bitangent;",
    "",
    "void main() {",
    "    v_worldPosition = a_position;",
    "    v_normal = normalize(a_normal);",
    "    v_uv = a_uv;",
    "",
    "    vec3 T = normalize(a_tangent.xyz);",
    "    vec3 N = v_normal;",
    "    vec3 B = cross(N, T) * a_tangent.w;",
    "    v_tangent = T;",
    "    v_bitangent = B;",
    "",
    "    gl_Position = u_projectionMatrix * u_viewMatrix * vec4(a_position, 1.0);",
    "}",
  ].join("\n");

  const SCENE_PBR_FRAGMENT_SOURCE = [
    "#version 300 es",
    "precision highp float;",
    "precision highp int;",
    "",
    "in vec3 v_worldPosition;",
    "in vec3 v_normal;",
    "in vec2 v_uv;",
    "in vec3 v_tangent;",
    "in vec3 v_bitangent;",
    "",
    "// Camera",
    "uniform vec3 u_cameraPosition;",
    "",
    "// Material",
    "uniform vec3 u_albedo;",
    "uniform float u_roughness;",
    "uniform float u_metalness;",
    "uniform float u_emissive;",
    "uniform float u_opacity;",
    "uniform bool u_unlit;",
    "",
    "// Texture maps",
    "uniform sampler2D u_albedoMap;",
    "uniform sampler2D u_normalMap;",
    "uniform sampler2D u_roughnessMap;",
    "uniform sampler2D u_metalnessMap;",
    "uniform sampler2D u_emissiveMap;",
    "uniform bool u_hasAlbedoMap;",
    "uniform bool u_hasNormalMap;",
    "uniform bool u_hasRoughnessMap;",
    "uniform bool u_hasMetalnessMap;",
    "uniform bool u_hasEmissiveMap;",
    "",
    "// Lights (max 8)",
    "uniform int u_lightCount;",
    "uniform int u_lightTypes[8];",
    "uniform vec3 u_lightPositions[8];",
    "uniform vec3 u_lightDirections[8];",
    "uniform vec3 u_lightColors[8];",
    "uniform float u_lightIntensities[8];",
    "uniform float u_lightRanges[8];",
    "uniform float u_lightDecays[8];",
    "uniform float u_lightAngles[8];",
    "uniform float u_lightPenumbras[8];",
    "uniform vec3 u_lightGroundColors[8];",
    "",
    "// Environment",
    "uniform vec3 u_ambientColor;",
    "uniform float u_ambientIntensity;",
    "uniform vec3 u_skyColor;",
    "uniform float u_skyIntensity;",
    "uniform vec3 u_groundColor;",
    "uniform float u_groundIntensity;",
    "",
    "// Shadow maps (max 2 directional lights)",
    "uniform sampler2D u_shadowMap0;",
    "uniform mat4 u_lightSpaceMatrix0;",
    "uniform bool u_hasShadow0;",
    "uniform float u_shadowBias0;",
    "",
    "uniform sampler2D u_shadowMap1;",
    "uniform mat4 u_lightSpaceMatrix1;",
    "uniform bool u_hasShadow1;",
    "uniform float u_shadowBias1;",
    "",
    "// Per-object shadow receive control",
    "uniform bool u_receiveShadow;",
    "",
    "// Shadow-casting light indices — maps shadow slot to light array index.",
    "uniform int u_shadowLightIndex0;",
    "uniform int u_shadowLightIndex1;",
    "",
    "// Exposure and tone mapping control.",
    "uniform float u_exposure;",
    "uniform int u_toneMapMode;",
    "",
    "// Fog",
    "uniform int u_hasFog;",
    "uniform float u_fogDensity;",
    "uniform vec3 u_fogColor;",
    "",
    "out vec4 fragColor;",
    "",
    "const float PI = 3.14159265359;",
    "",
    "// 4-tap Poisson disk PCF shadow sampling.",
    "float shadowFactor(sampler2D shadowMap, mat4 lightSpaceMatrix, float bias) {",
    "    vec4 lightSpacePos = lightSpaceMatrix * vec4(v_worldPosition, 1.0);",
    "    vec3 projCoords = lightSpacePos.xyz / lightSpacePos.w;",
    "    projCoords = projCoords * 0.5 + 0.5;",
    "",
    "    if (projCoords.z > 1.0) return 1.0;",
    "",
    "    float shadow = 0.0;",
    "    float texelSize = 1.0 / float(textureSize(shadowMap, 0).x);",
    "    vec2 poissonDisk[4] = vec2[](",
    "        vec2(-0.94201624, -0.39906216),",
    "        vec2(0.94558609, -0.76890725),",
    "        vec2(-0.094184101, -0.92938870),",
    "        vec2(0.34495938, 0.29387760)",
    "    );",
    "",
    "    for (int i = 0; i < 4; i++) {",
    "        float depth = texture(shadowMap, projCoords.xy + poissonDisk[i] * texelSize).r;",
    "        shadow += (projCoords.z - bias > depth) ? 0.0 : 1.0;",
    "    }",
    "    return shadow / 4.0;",
    "}",
    "",
    "// GGX/Trowbridge-Reitz normal distribution function.",
    "float distributionGGX(vec3 N, vec3 H, float roughness) {",
    "    float a = roughness * roughness;",
    "    float a2 = a * a;",
    "    float NdotH = max(dot(N, H), 0.0);",
    "    float NdotH2 = NdotH * NdotH;",
    "    float denom = NdotH2 * (a2 - 1.0) + 1.0;",
    "    denom = PI * denom * denom;",
    "    return a2 / max(denom, 0.0000001);",
    "}",
    "",
    "// Smith geometry function (GGX variant) — single direction.",
    "float geometrySchlickGGX(float NdotV, float roughness) {",
    "    float r = roughness + 1.0;",
    "    float k = (r * r) / 8.0;",
    "    return NdotV / (NdotV * (1.0 - k) + k);",
    "}",
    "",
    "// Smith geometry function — combined for view and light directions.",
    "float geometrySmith(vec3 N, vec3 V, vec3 L, float roughness) {",
    "    float NdotV = max(dot(N, V), 0.0);",
    "    float NdotL = max(dot(N, L), 0.0);",
    "    return geometrySchlickGGX(NdotV, roughness) * geometrySchlickGGX(NdotL, roughness);",
    "}",
    "",
    "// Schlick fresnel approximation.",
    "vec3 fresnelSchlick(float cosTheta, vec3 F0) {",
    "    return F0 + (1.0 - F0) * pow(clamp(1.0 - cosTheta, 0.0, 1.0), 5.0);",
    "}",
    "",
    "// Point light distance attenuation.",
    "float pointLightAttenuation(float distance, float range, float decay) {",
    "    if (range > 0.0) {",
    "        float ratio = clamp(1.0 - pow(distance / range, 4.0), 0.0, 1.0);",
    "        return ratio * ratio / max(distance * distance, 0.0001);",
    "    }",
    "    return 1.0 / max(pow(distance, decay), 0.0001);",
    "}",
    "",
    "void main() {",
    "    // Resolve material properties, sampling textures when available.",
    "    vec3 albedo = u_albedo;",
    "    if (u_hasAlbedoMap) {",
    "        vec4 texAlbedo = texture(u_albedoMap, v_uv);",
    "        albedo *= texAlbedo.rgb;",
    "    }",
    "",
    "    float roughness = u_roughness;",
    "    if (u_hasRoughnessMap) {",
    "        roughness *= texture(u_roughnessMap, v_uv).g;",
    "    }",
    "    roughness = clamp(roughness, 0.04, 1.0);",
    "",
    "    float metalness = u_metalness;",
    "    if (u_hasMetalnessMap) {",
    "        metalness *= texture(u_metalnessMap, v_uv).b;",
    "    }",
    "    metalness = clamp(metalness, 0.0, 1.0);",
    "",
    "    float emissiveStrength = u_emissive;",
    "    vec3 emissiveColor = albedo;",
    "    if (u_hasEmissiveMap) {",
    "        emissiveColor = texture(u_emissiveMap, v_uv).rgb;",
    "    }",
    "",
    "    // Unlit path: output albedo directly.",
    "    if (u_unlit) {",
    "        vec3 color = albedo + emissiveColor * emissiveStrength;",
    "        fragColor = vec4(color, u_opacity);",
    "        return;",
    "    }",
    "",
    "    // Resolve per-pixel normal via TBN matrix.",
    "    vec3 N = normalize(v_normal);",
    "    if (u_hasNormalMap) {",
    "        vec3 T = normalize(v_tangent);",
    "        vec3 B = normalize(v_bitangent);",
    "        mat3 TBN = mat3(T, B, N);",
    "        vec3 mapNormal = texture(u_normalMap, v_uv).rgb * 2.0 - 1.0;",
    "        N = normalize(TBN * mapNormal);",
    "    }",
    "",
    "    vec3 V = normalize(u_cameraPosition - v_worldPosition);",
    "",
    "    // Fresnel reflectance at normal incidence — dielectric vs metallic blend.",
    "    vec3 F0 = mix(vec3(0.04), albedo, metalness);",
    "",
    "    // Accumulate direct lighting.",
    "    vec3 Lo = vec3(0.0);",
    "",
    "    for (int i = 0; i < 8; i++) {",
    "        if (i >= u_lightCount) break;",
    "",
    "        int lightType = u_lightTypes[i];",
    "        vec3 lightColor = u_lightColors[i];",
    "        float intensity = u_lightIntensities[i];",
    "",
    "        // Ambient light (type 0): add flat contribution, no BRDF.",
    "        if (lightType == 0) {",
    "            Lo += albedo * lightColor * intensity;",
    "            continue;",
    "        }",
    "",
    "        // Hemisphere light (type 4): sky/ground blend based on normal Y.",
    "        if (lightType == 4) {",
    "            float hBlend = N.y * 0.5 + 0.5;",
    "            vec3 hemiColor = mix(u_lightGroundColors[i], lightColor, hBlend);",
    "            Lo += albedo * hemiColor * intensity;",
    "            continue;",
    "        }",
    "",
    "        vec3 L;",
    "        float attenuation = 1.0;",
    "",
    "        if (lightType == 1) {",
    "            // Directional light.",
    "            L = normalize(-u_lightDirections[i]);",
    "        } else if (lightType == 3) {",
    "            // Spot light.",
    "            vec3 toLight = u_lightPositions[i] - v_worldPosition;",
    "            float dist = length(toLight);",
    "            L = toLight / max(dist, 0.0001);",
    "",
    "            // Cone attenuation.",
    "            float cosAngle = dot(L, -normalize(u_lightDirections[i]));",
    "            float outerCos = cos(u_lightAngles[i]);",
    "            float innerCos = cos(u_lightAngles[i] * (1.0 - u_lightPenumbras[i]));",
    "            float spotAtten = clamp((cosAngle - outerCos) / max(innerCos - outerCos, 0.001), 0.0, 1.0);",
    "",
    "            // Distance attenuation (same as point light).",
    "            attenuation = pointLightAttenuation(dist, u_lightRanges[i], u_lightDecays[i]) * spotAtten;",
    "        } else {",
    "            // Point light (type 2).",
    "            vec3 toLight = u_lightPositions[i] - v_worldPosition;",
    "            float dist = length(toLight);",
    "            L = toLight / max(dist, 0.0001);",
    "            attenuation = pointLightAttenuation(dist, u_lightRanges[i], u_lightDecays[i]);",
    "        }",
    "",
    "        vec3 H = normalize(V + L);",
    "        float NdotL = max(dot(N, L), 0.0);",
    "",
    "        // Cook-Torrance specular BRDF.",
    "        float D = distributionGGX(N, H, roughness);",
    "        float G = geometrySmith(N, V, L, roughness);",
    "        vec3 F = fresnelSchlick(max(dot(H, V), 0.0), F0);",
    "",
    "        vec3 numerator = D * G * F;",
    "        float denominator = 4.0 * max(dot(N, V), 0.0) * NdotL + 0.0001;",
    "        vec3 specular = numerator / denominator;",
    "",
    "        // Energy conservation: diffuse complement of specular.",
    "        vec3 kD = (vec3(1.0) - F) * (1.0 - metalness);",
    "",
    "        // Shadow attenuation for directional lights.",
    "        float shadow = 1.0;",
    "        if (u_receiveShadow && lightType == 1) {",
    "            if (u_hasShadow0 && i == u_shadowLightIndex0) {",
    "                shadow = shadowFactor(u_shadowMap0, u_lightSpaceMatrix0, u_shadowBias0);",
    "            } else if (u_hasShadow1 && i == u_shadowLightIndex1) {",
    "                shadow = shadowFactor(u_shadowMap1, u_lightSpaceMatrix1, u_shadowBias1);",
    "            }",
    "        }",
    "",
    "        vec3 radiance = lightColor * intensity * attenuation;",
    "        Lo += (kD * albedo / PI + specular) * radiance * NdotL * shadow;",
    "    }",
    "",
    "    // Environment hemisphere lighting.",
    "    float hemi = N.y * 0.5 + 0.5;",
    "    vec3 envDiffuse = u_ambientColor * u_ambientIntensity",
    "                    + u_skyColor * u_skyIntensity * hemi",
    "                    + u_groundColor * u_groundIntensity * (1.0 - hemi);",
    "    vec3 ambient = envDiffuse * albedo;",
    "",
    "    // Emissive contribution.",
    "    vec3 emission = emissiveColor * emissiveStrength;",
    "",
    "    vec3 color = ambient + Lo + emission;",
    "",
    "    // Exponential fog.",
    "    if (u_hasFog != 0) {",
    "        float fogDist = length(v_worldPosition - u_cameraPosition);",
    "        float fogFactor = exp(-u_fogDensity * u_fogDensity * fogDist * fogDist);",
    "        fogFactor = clamp(fogFactor, 0.0, 1.0);",
    "        color = mix(u_fogColor, color, fogFactor);",
    "    }",
    "",
    "    // Apply exposure.",
    "    color *= u_exposure;",
    "",
    "    // Tone mapping.",
    "    if (u_toneMapMode == 1) {",
    "        // ACES filmic.",
    "        color = (color * (2.51 * color + 0.03)) / (color * (2.43 * color + 0.59) + 0.14);",
    "    } else if (u_toneMapMode == 2) {",
    "        // Reinhard.",
    "        color = color / (color + vec3(1.0));",
    "    }",
    "    // else: linear (no tone mapping).",
    "",
    "    // Gamma correction.",
    "    color = pow(color, vec3(1.0 / 2.2));",
    "",
    "    fragColor = vec4(color, u_opacity);",
    "}",
  ].join("\n");

  const SCENE_SHADOW_VERTEX_SOURCE = [
    "#version 300 es",
    "precision highp float;",
    "in vec3 a_position;",
    "uniform mat4 u_lightViewProjection;",
    "void main() {",
    "    gl_Position = u_lightViewProjection * vec4(a_position, 1.0);",
    "}",
  ].join("\n");

  const SCENE_SHADOW_FRAGMENT_SOURCE = [
    "#version 300 es",
    "precision highp float;",
    "void main() {}",
  ].join("\n");

  const SCENE_POINTS_VERTEX_SOURCE = [
    "#version 300 es",
    "precision highp float;",
    "precision highp int;",
    "",
    "in vec3 a_position;",
    "in float a_size;",
    "in vec4 a_color;",
    "",
    "uniform mat4 u_viewMatrix;",
    "uniform mat4 u_projectionMatrix;",
    "uniform mat4 u_modelMatrix;",
    "uniform float u_defaultSize;",
    "uniform vec4 u_defaultColor;",
    "uniform bool u_hasPerVertexColor;",
    "uniform bool u_hasPerVertexSize;",
    "uniform bool u_sizeAttenuation;",
    "uniform int u_pointStyle;",
    "uniform float u_viewportHeight;",
    "",
    "// Fog",
    "uniform int u_hasFog;",
    "uniform float u_fogDensity;",
    "",
    "out vec4 v_color;",
    "out float v_fogFactor;",
    "out float v_pointSize;",
    "",
    "void main() {",
    "    vec4 worldPos = u_modelMatrix * vec4(a_position, 1.0);",
    "    vec4 viewPos = u_viewMatrix * worldPos;",
    "    gl_Position = u_projectionMatrix * viewPos;",
    "",
    "    float size = u_hasPerVertexSize ? a_size : u_defaultSize;",
    "    if (u_sizeAttenuation) {",
    "        gl_PointSize = max(size * (u_viewportHeight * 0.5) / max(-viewPos.z, 0.001), 1.0);",
    "    } else {",
    "        gl_PointSize = max(size, 1.0);",
    "    }",
    "    v_pointSize = gl_PointSize;",
    "",
    "    v_color = u_hasPerVertexColor ? a_color : u_defaultColor;",
    "",
    "    // Exponential fog",
    "    if (u_hasFog != 0) {",
    "        float dist = length(viewPos.xyz);",
    "        v_fogFactor = exp(-u_fogDensity * u_fogDensity * dist * dist);",
    "        v_fogFactor = clamp(v_fogFactor, 0.0, 1.0);",
    "    } else {",
    "        v_fogFactor = 1.0;",
    "    }",
    "}",
  ].join("\n");

  const SCENE_POINTS_FRAGMENT_SOURCE = [
    "#version 300 es",
    "precision highp float;",
    "precision highp int;",
    "",
    "in vec4 v_color;",
    "in float v_fogFactor;",
    "in float v_pointSize;",
    "",
    "uniform float u_opacity;",
    "uniform vec3 u_fogColor;",
    "uniform int u_hasFog;",
    "uniform int u_pointStyle;",
    "",
    "out vec4 fragColor;",
    "",
    "void main() {",
    "    float alpha = 1.0;",
    "    vec3 color = v_color.rgb;",
    "    if (u_pointStyle == 1) {",
    "        vec2 centered = gl_PointCoord - vec2(0.5);",
    "        float radial = length(centered);",
    "        float square = max(abs(centered.x), abs(centered.y));",
    "        float focus = clamp((v_pointSize - 1.0) / 10.0, 0.0, 1.0);",
    "        float coreRadius = mix(0.49, 0.18, focus);",
    "        float core = 1.0 - smoothstep(coreRadius, coreRadius + 0.05, square);",
    "        float halo = (1.0 - smoothstep(0.12, 0.72, radial)) * focus;",
    "        float streakX = 1.0 - smoothstep(0.02, 0.16, abs(centered.x));",
    "        float streakY = 1.0 - smoothstep(0.02, 0.16, abs(centered.y));",
    "        float streak = max(streakX, streakY) * focus;",
    "        alpha = clamp(core + halo * 0.5 + streak * 0.2, 0.0, 1.0);",
    "        color = mix(color, vec3(1.0), clamp(focus * 0.22 + core * focus * 0.28, 0.0, 0.4));",
    "    } else if (u_pointStyle == 2) {",
    "        // glow: pure radial gaussian falloff — soft gas cloud, no core, no streaks",
    "        vec2 centered = gl_PointCoord - vec2(0.5);",
    "        float radial = length(centered) * 2.0;",
    "        if (radial > 1.0) discard;",
    "        float g = exp(-radial * radial * 3.5);",
    "        alpha = g;",
    "    }",
    "",
    "    // Apply fog",
    "    if (u_hasFog != 0) {",
    "        color = mix(u_fogColor, color, v_fogFactor);",
    "    }",
    "",
    "    fragColor = vec4(color, alpha * v_color.a * u_opacity);",
    "}",
  ].join("\n");

  const SCENE_PBR_INSTANCED_VERTEX_SOURCE = [
    "#version 300 es",
    "precision highp float;",
    "",
    "in vec3 a_position;",
    "in vec3 a_normal;",
    "in vec2 a_uv;",
    "in vec4 a_tangent;",
    "in mat4 a_instanceMatrix;",
    "",
    "uniform mat4 u_viewMatrix;",
    "uniform mat4 u_projectionMatrix;",
    "",
    "out vec3 v_worldPosition;",
    "out vec3 v_normal;",
    "out vec2 v_uv;",
    "out vec3 v_tangent;",
    "out vec3 v_bitangent;",
    "",
    "void main() {",
    "    vec4 worldPos = a_instanceMatrix * vec4(a_position, 1.0);",
    "    v_worldPosition = worldPos.xyz;",
    "    mat3 normalMatrix = mat3(a_instanceMatrix);",
    "    v_normal = normalize(normalMatrix * a_normal);",
    "    v_uv = a_uv;",
    "    vec3 T = normalize(normalMatrix * a_tangent.xyz);",
    "    vec3 N = v_normal;",
    "    v_bitangent = cross(N, T) * a_tangent.w;",
    "    v_tangent = T;",
    "    gl_Position = u_projectionMatrix * u_viewMatrix * worldPos;",
    "}",
  ].join("\n");

  const SCENE_PBR_SKINNED_VERTEX_SOURCE = [
    "#version 300 es",
    "precision highp float;",
    "",
    "in vec3 a_position;",
    "in vec3 a_normal;",
    "in vec2 a_uv;",
    "in vec4 a_tangent;",
    "in vec4 a_joints;",
    "in vec4 a_weights;",
    "",
    "uniform mat4 u_viewMatrix;",
    "uniform mat4 u_projectionMatrix;",
    "uniform mat4 u_jointMatrices[64];",
    "uniform bool u_hasSkin;",
    "",
    "out vec3 v_worldPosition;",
    "out vec3 v_normal;",
    "out vec2 v_uv;",
    "out vec3 v_tangent;",
    "out vec3 v_bitangent;",
    "",
    "void main() {",
    "    vec4 pos = vec4(a_position, 1.0);",
    "    vec3 norm = a_normal;",
    "    vec3 tang = a_tangent.xyz;",
    "",
    "    if (u_hasSkin) {",
    "        mat4 skinMatrix =",
    "            a_weights.x * u_jointMatrices[int(a_joints.x)] +",
    "            a_weights.y * u_jointMatrices[int(a_joints.y)] +",
    "            a_weights.z * u_jointMatrices[int(a_joints.z)] +",
    "            a_weights.w * u_jointMatrices[int(a_joints.w)];",
    "",
    "        pos = skinMatrix * pos;",
    "        norm = mat3(skinMatrix) * norm;",
    "        tang = mat3(skinMatrix) * tang;",
    "    }",
    "",
    "    v_worldPosition = pos.xyz;",
    "    v_normal = normalize(norm);",
    "    v_uv = a_uv;",
    "",
    "    vec3 T = normalize(tang);",
    "    vec3 N = v_normal;",
    "    vec3 B = cross(N, T) * a_tangent.w;",
    "    v_tangent = T;",
    "    v_bitangent = B;",
    "",
    "    gl_Position = u_projectionMatrix * u_viewMatrix * pos;",
    "}",
  ].join("\n");

  function scenePBRViewMatrix(camera, out) {
    const cam = sceneRenderCamera(camera);
    const tx = -cam.x;
    const ty = -cam.y;
    const tz = -cam.z;

    const sx = Math.sin(-cam.rotationX);
    const cx = Math.cos(-cam.rotationX);
    const sy = Math.sin(-cam.rotationY);
    const cy = Math.cos(-cam.rotationY);
    const sz = Math.sin(-cam.rotationZ);
    const cz = Math.cos(-cam.rotationZ);

    const r00 = cy * cz;
    const r01 = cy * sz;
    const r02 = -sy;

    const r10 = sx * sy * cz - cx * sz;
    const r11 = sx * sy * sz + cx * cz;
    const r12 = sx * cy;

    const r20 = cx * sy * cz + sx * sz;
    const r21 = cx * sy * sz - sx * cz;
    const r22 = cx * cy;

    const d0 = r00 * tx + r01 * ty + r02 * tz;
    const d1 = r10 * tx + r11 * ty + r12 * tz;
    const d2 = r20 * tx + r21 * ty + r22 * tz;

    var m = out || new Float32Array(16);
    m[0] = r00; m[1] = r10; m[2] = r20; m[3] = 0;
    m[4] = r01; m[5] = r11; m[6] = r21; m[7] = 0;
    m[8] = r02; m[9] = r12; m[10] = r22; m[11] = 0;
    m[12] = d0; m[13] = d1; m[14] = d2; m[15] = 1;
    return m;
  }

  function scenePBRProjectionMatrix(fov, aspect, near, far, out) {
    const fovRad = (fov * Math.PI) / 180;
    const f = 1 / Math.tan(fovRad * 0.5);
    const rangeInv = 1 / (near - far);

    var m = out || new Float32Array(16);
    m[0] = f / aspect; m[1] = 0; m[2] = 0; m[3] = 0;
    m[4] = 0; m[5] = f; m[6] = 0; m[7] = 0;
    m[8] = 0; m[9] = 0; m[10] = (near + far) * rangeInv; m[11] = -1;
    m[12] = 0; m[13] = 0; m[14] = 2 * near * far * rangeInv; m[15] = 0;
    return m;
  }

  function createSceneShadowResources(gl, size) {
    const framebuffer = gl.createFramebuffer();
    const depthTexture = gl.createTexture();

    gl.bindTexture(gl.TEXTURE_2D, depthTexture);
    gl.texImage2D(
      gl.TEXTURE_2D, 0, gl.DEPTH_COMPONENT24,
      size, size, 0,
      gl.DEPTH_COMPONENT, gl.UNSIGNED_INT, null
    );
    gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_MIN_FILTER, gl.NEAREST);
    gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_MAG_FILTER, gl.NEAREST);
    gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_WRAP_S, gl.CLAMP_TO_EDGE);
    gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_WRAP_T, gl.CLAMP_TO_EDGE);

    gl.bindFramebuffer(gl.FRAMEBUFFER, framebuffer);
    gl.framebufferTexture2D(
      gl.FRAMEBUFFER, gl.DEPTH_ATTACHMENT, gl.TEXTURE_2D, depthTexture, 0
    );
    gl.bindFramebuffer(gl.FRAMEBUFFER, null);

    return { framebuffer: framebuffer, depthTexture: depthTexture, size: size };
  }

  function sceneShadowLightSpaceMatrix(light, sceneBounds) {
    var dx = sceneNumber(light.directionX, 0);
    var dy = sceneNumber(light.directionY, -1);
    var dz = sceneNumber(light.directionZ, 0);
    var len = Math.sqrt(dx * dx + dy * dy + dz * dz);
    if (len < 0.0001) {
      dx = 0; dy = -1; dz = 0; len = 1;
    }
    dx /= len; dy /= len; dz /= len;

    var cx = (sceneBounds.minX + sceneBounds.maxX) * 0.5;
    var cy = (sceneBounds.minY + sceneBounds.maxY) * 0.5;
    var cz = (sceneBounds.minZ + sceneBounds.maxZ) * 0.5;
    var ex = (sceneBounds.maxX - sceneBounds.minX) * 0.5;
    var ey = (sceneBounds.maxY - sceneBounds.minY) * 0.5;
    var ez = (sceneBounds.maxZ - sceneBounds.minZ) * 0.5;
    var radius = Math.sqrt(ex * ex + ey * ey + ez * ez);
    if (radius < 0.01) radius = 10;

    var eyeX = cx - dx * radius * 2;
    var eyeY = cy - dy * radius * 2;
    var eyeZ = cz - dz * radius * 2;

    var fx = dx, fy = dy, fz = dz;

    var upX = 0, upY = 1, upZ = 0;
    if (Math.abs(fy) > 0.99) {
      upX = 0; upY = 0; upZ = 1;
    }

    var rx = fy * upZ - fz * upY;
    var ry = fz * upX - fx * upZ;
    var rz = fx * upY - fy * upX;
    var rLen = Math.sqrt(rx * rx + ry * ry + rz * rz);
    if (rLen < 0.0001) rLen = 1;
    rx /= rLen; ry /= rLen; rz /= rLen;

    upX = ry * fz - rz * fy;
    upY = rz * fx - rx * fz;
    upZ = rx * fy - ry * fx;

    var tx = -(rx * eyeX + ry * eyeY + rz * eyeZ);
    var ty = -(upX * eyeX + upY * eyeY + upZ * eyeZ);
    var tz = -(fx * eyeX + fy * eyeY + fz * eyeZ);

    var view = new Float32Array([
      rx,  upX, fx,  0,
      ry,  upY, fy,  0,
      rz,  upZ, fz,  0,
      tx,  ty,  tz,  1,
    ]);

    var near = 0.01;
    var far = radius * 4;
    var l = -radius, rr = radius, b = -radius, t = radius;
    var proj = new Float32Array([
      2 / (rr - l),     0,              0,                    0,
      0,                2 / (t - b),    0,                    0,
      0,                0,              -2 / (far - near),    0,
      -(rr + l) / (rr - l), -(t + b) / (t - b), -(far + near) / (far - near), 1,
    ]);

    return sceneMat4Multiply(proj, view);
  }

  function sceneShadowComputeBounds(bundle) {
    var minX = Infinity, minY = Infinity, minZ = Infinity;
    var maxX = -Infinity, maxY = -Infinity, maxZ = -Infinity;
    var positions = bundle.worldMeshPositions;
    var objects = Array.isArray(bundle.meshObjects) ? bundle.meshObjects : [];

    for (var i = 0; i < objects.length; i++) {
      var obj = objects[i];
      if (!obj || obj.viewCulled) continue;
      var offset = obj.vertexOffset;
      var count = obj.vertexCount;
      if (!Number.isFinite(offset) || !Number.isFinite(count) || count <= 0) continue;

      for (var v = 0; v < count; v++) {
        var idx = (offset + v) * 3;
        var px = positions[idx];
        var py = positions[idx + 1];
        var pz = positions[idx + 2];
        if (px < minX) minX = px;
        if (py < minY) minY = py;
        if (pz < minZ) minZ = pz;
        if (px > maxX) maxX = px;
        if (py > maxY) maxY = py;
        if (pz > maxZ) maxZ = pz;
      }
    }

    if (!isFinite(minX)) {
      return { minX: -10, minY: -10, minZ: -10, maxX: 10, maxY: 10, maxZ: 10 };
    }
    return { minX: minX, minY: minY, minZ: minZ, maxX: maxX, maxY: maxY, maxZ: maxZ };
  }

  function renderSceneShadowPass(gl, shadowProgram, shadowResources, lightMatrix, bundle, shadowState) {
    gl.bindFramebuffer(gl.FRAMEBUFFER, shadowResources.framebuffer);
    gl.viewport(0, 0, shadowResources.size, shadowResources.size);
    gl.clearDepth(1);
    gl.clear(gl.DEPTH_BUFFER_BIT);

    gl.useProgram(shadowProgram.program);
    gl.uniformMatrix4fv(shadowProgram.uniforms.lightViewProjection, false, lightMatrix);

    gl.enable(gl.DEPTH_TEST);
    gl.depthMask(true);
    gl.depthFunc(gl.LEQUAL);
    gl.disable(gl.BLEND);

    gl.enable(gl.CULL_FACE);
    gl.cullFace(gl.FRONT);

    var objects = Array.isArray(bundle.meshObjects) ? bundle.meshObjects : [];

    for (var i = 0; i < objects.length; i++) {
      var obj = objects[i];
      if (!obj || obj.viewCulled) continue;
      if (!obj.castShadow) continue;
      if (!Number.isFinite(obj.vertexOffset) || !Number.isFinite(obj.vertexCount) || obj.vertexCount <= 0) continue;

      var offset = obj.vertexOffset;
      var count = obj.vertexCount;

      var length = count * 3;
      var start = offset * 3;
      if (!shadowState.scratch || shadowState.scratch.length < length) {
        shadowState.scratch = new Float32Array(length);
      }
      var positions = shadowState.scratch;
      for (var vi = 0; vi < length; vi++) {
        positions[vi] = bundle.worldMeshPositions[start + vi] || 0;
      }

      gl.bindBuffer(gl.ARRAY_BUFFER, shadowState.buffer);
      gl.bufferData(gl.ARRAY_BUFFER, positions.subarray(0, length), gl.DYNAMIC_DRAW);
      gl.enableVertexAttribArray(shadowProgram.attributes.position);
      gl.vertexAttribPointer(shadowProgram.attributes.position, 3, gl.FLOAT, false, 0, 0);

      gl.drawArrays(gl.TRIANGLES, 0, count);
    }

    gl.cullFace(gl.BACK);
    gl.disable(gl.CULL_FACE);

    gl.bindFramebuffer(gl.FRAMEBUFFER, null);
  }

  function createSceneShadowProgram(gl) {
    var vertexShader = scenePBRCompileShader(gl, gl.VERTEX_SHADER, SCENE_SHADOW_VERTEX_SOURCE);
    if (!vertexShader) return null;
    var fragmentShader = scenePBRCompileShader(gl, gl.FRAGMENT_SHADER, SCENE_SHADOW_FRAGMENT_SOURCE);
    if (!fragmentShader) {
      gl.deleteShader(vertexShader);
      return null;
    }

    var program = scenePBRLinkProgram(gl, vertexShader, fragmentShader, "Shadow shader");
    if (!program) return null;

    return {
      program: program,
      vertexShader: vertexShader,
      fragmentShader: fragmentShader,
      attributes: {
        position: gl.getAttribLocation(program, "a_position"),
      },
      uniforms: {
        lightViewProjection: gl.getUniformLocation(program, "u_lightViewProjection"),
      },
    };
  }

  const SCENE_POST_VERTEX_SOURCE = [
    "#version 300 es",
    "in vec2 a_position;",
    "in vec2 a_uv;",
    "out vec2 v_uv;",
    "void main() {",
    "    v_uv = a_uv;",
    "    gl_Position = vec4(a_position, 0.0, 1.0);",
    "}",
  ].join("\n");

  const SCENE_POST_TONEMAPPING_SOURCE = [
    "#version 300 es",
    "precision highp float;",
    "in vec2 v_uv;",
    "uniform sampler2D u_texture;",
    "uniform float u_exposure;",
    "out vec4 fragColor;",
    "",
    "vec3 aces(vec3 x) {",
    "    float a = 2.51;",
    "    float b = 0.03;",
    "    float c = 2.43;",
    "    float d = 0.59;",
    "    float e = 0.14;",
    "    return clamp((x * (a * x + b)) / (x * (c * x + d) + e), 0.0, 1.0);",
    "}",
    "",
    "void main() {",
    "    vec3 color = texture(u_texture, v_uv).rgb;",
    "    color *= u_exposure;",
    "    color = aces(color);",
    "    fragColor = vec4(color, 1.0);",
    "}",
  ].join("\n");

  const SCENE_POST_BLOOM_BRIGHT_SOURCE = [
    "#version 300 es",
    "precision highp float;",
    "in vec2 v_uv;",
    "uniform sampler2D u_texture;",
    "uniform float u_threshold;",
    "out vec4 fragColor;",
    "",
    "void main() {",
    "    vec3 color = texture(u_texture, v_uv).rgb;",
    "    float brightness = dot(color, vec3(0.2126, 0.7152, 0.0722));",
    "    fragColor = vec4(brightness > u_threshold ? color : vec3(0.0), 1.0);",
    "}",
  ].join("\n");

  const SCENE_POST_BLUR_SOURCE = [
    "#version 300 es",
    "precision highp float;",
    "in vec2 v_uv;",
    "uniform sampler2D u_texture;",
    "uniform vec2 u_direction;",
    "uniform float u_radius;",
    "out vec4 fragColor;",
    "",
    "void main() {",
    "    vec2 texelSize = 1.0 / vec2(textureSize(u_texture, 0));",
    "    vec3 result = texture(u_texture, v_uv).rgb * 0.227027;",
    "",
    "    float offsets[4] = float[](1.0, 2.0, 3.0, 4.0);",
    "    float weights[4] = float[](0.1945946, 0.1216216, 0.054054, 0.016216);",
    "",
    "    for (int i = 0; i < 4; i++) {",
    "        vec2 offset = u_direction * texelSize * offsets[i] * u_radius;",
    "        result += texture(u_texture, v_uv + offset).rgb * weights[i];",
    "        result += texture(u_texture, v_uv - offset).rgb * weights[i];",
    "    }",
    "    fragColor = vec4(result, 1.0);",
    "}",
  ].join("\n");

  const SCENE_POST_BLOOM_COMPOSITE_SOURCE = [
    "#version 300 es",
    "precision highp float;",
    "in vec2 v_uv;",
    "uniform sampler2D u_texture;",
    "uniform sampler2D u_bloomTexture;",
    "uniform float u_intensity;",
    "out vec4 fragColor;",
    "",
    "void main() {",
    "    vec3 scene = texture(u_texture, v_uv).rgb;",
    "    vec3 bloom = texture(u_bloomTexture, v_uv).rgb;",
    "    fragColor = vec4(scene + bloom * u_intensity, 1.0);",
    "}",
  ].join("\n");

  const SCENE_POST_VIGNETTE_SOURCE = [
    "#version 300 es",
    "precision highp float;",
    "in vec2 v_uv;",
    "uniform sampler2D u_texture;",
    "uniform float u_intensity;",
    "out vec4 fragColor;",
    "",
    "void main() {",
    "    vec3 color = texture(u_texture, v_uv).rgb;",
    "    vec2 center = v_uv - 0.5;",
    "    float dist = length(center);",
    "    float vignette = 1.0 - smoothstep(0.3, 0.7, dist * u_intensity);",
    "    fragColor = vec4(color * vignette, 1.0);",
    "}",
  ].join("\n");

  const SCENE_POST_COLORGRADE_SOURCE = [
    "#version 300 es",
    "precision highp float;",
    "in vec2 v_uv;",
    "uniform sampler2D u_texture;",
    "uniform float u_exposure;",
    "uniform float u_contrast;",
    "uniform float u_saturation;",
    "out vec4 fragColor;",
    "",
    "void main() {",
    "    vec3 color = texture(u_texture, v_uv).rgb;",
    "    color *= u_exposure;",
    "    color = mix(vec3(0.5), color, u_contrast);",
    "    float gray = dot(color, vec3(0.2126, 0.7152, 0.0722));",
    "    color = mix(vec3(gray), color, u_saturation);",
    "    fragColor = vec4(clamp(color, 0.0, 1.0), 1.0);",
    "}",
  ].join("\n");

  function createScenePostFBO(gl, width, height) {
    var hdrSupported = Boolean(gl.getExtension("EXT_color_buffer_float"));
    var internalFormat = hdrSupported ? gl.RGBA16F : gl.RGBA8;
    var dataType = hdrSupported ? gl.FLOAT : gl.UNSIGNED_BYTE;

    var fbo = gl.createFramebuffer();
    var colorTex = gl.createTexture();
    gl.bindTexture(gl.TEXTURE_2D, colorTex);
    gl.texImage2D(gl.TEXTURE_2D, 0, internalFormat, width, height, 0, gl.RGBA, dataType, null);
    gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_MIN_FILTER, gl.LINEAR);
    gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_MAG_FILTER, gl.LINEAR);
    gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_WRAP_S, gl.CLAMP_TO_EDGE);
    gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_WRAP_T, gl.CLAMP_TO_EDGE);

    var depthRB = gl.createRenderbuffer();
    gl.bindRenderbuffer(gl.RENDERBUFFER, depthRB);
    gl.renderbufferStorage(gl.RENDERBUFFER, gl.DEPTH_COMPONENT24, width, height);

    gl.bindFramebuffer(gl.FRAMEBUFFER, fbo);
    gl.framebufferTexture2D(gl.FRAMEBUFFER, gl.COLOR_ATTACHMENT0, gl.TEXTURE_2D, colorTex, 0);
    gl.framebufferRenderbuffer(gl.FRAMEBUFFER, gl.DEPTH_ATTACHMENT, gl.RENDERBUFFER, depthRB);
    gl.bindFramebuffer(gl.FRAMEBUFFER, null);

    return { fbo: fbo, colorTex: colorTex, depthRB: depthRB, width: width, height: height };
  }

  function createScenePostPingPong(gl, width, height) {
    return {
      a: createScenePostFBO(gl, width, height),
      b: createScenePostFBO(gl, width, height),
    };
  }

  function createSceneFullscreenQuad(gl) {
    var vao = gl.createVertexArray();
    var vbo = gl.createBuffer();
    gl.bindVertexArray(vao);
    gl.bindBuffer(gl.ARRAY_BUFFER, vbo);
    gl.bufferData(gl.ARRAY_BUFFER, new Float32Array([
      -1, -1,  0, 0,
       1, -1,  1, 0,
      -1,  1,  0, 1,
       1,  1,  1, 1,
    ]), gl.STATIC_DRAW);
    gl.enableVertexAttribArray(0);
    gl.vertexAttribPointer(0, 2, gl.FLOAT, false, 16, 0);
    gl.enableVertexAttribArray(1);
    gl.vertexAttribPointer(1, 2, gl.FLOAT, false, 16, 8);
    gl.bindVertexArray(null);
    return { vao: vao, vbo: vbo };
  }

  function drawSceneFullscreenQuad(gl, quadVAO) {
    gl.bindVertexArray(quadVAO);
    gl.drawArrays(gl.TRIANGLE_STRIP, 0, 4);
    gl.bindVertexArray(null);
  }

  function createScenePostProgram(gl, fragmentSource) {
    var vs = scenePBRCompileShader(gl, gl.VERTEX_SHADER, SCENE_POST_VERTEX_SOURCE);
    if (!vs) return null;
    var fs = scenePBRCompileShader(gl, gl.FRAGMENT_SHADER, fragmentSource);
    if (!fs) {
      gl.deleteShader(vs);
      return null;
    }

    var prog = scenePBRLinkProgram(gl, vs, fs, "Post-process shader");
    if (!prog) return null;

    return { program: prog, vertexShader: vs, fragmentShader: fs };
  }

  function disposeScenePostFBO(gl, fboObj) {
    if (!fboObj) return;
    if (fboObj.colorTex) gl.deleteTexture(fboObj.colorTex);
    if (fboObj.depthRB) gl.deleteRenderbuffer(fboObj.depthRB);
    if (fboObj.fbo) gl.deleteFramebuffer(fboObj.fbo);
  }

  function createScenePostProcessor(gl) {
    var quad = createSceneFullscreenQuad(gl);
    var sceneFBO = null;
    var auxFBO = null;
    var pingPong = null;
    var currentWidth = 0;
    var currentHeight = 0;

    var programs = {};

    function getProgram(name, fragmentSource) {
      if (programs[name]) return programs[name];
      var prog = createScenePostProgram(gl, fragmentSource);
      if (prog) programs[name] = prog;
      return prog;
    }

    function beginPostPass(prog, inputTex, targetFBO, w, h) {
      gl.bindFramebuffer(gl.FRAMEBUFFER, targetFBO);
      gl.viewport(0, 0, w, h);
      gl.useProgram(prog.program);
      gl.activeTexture(gl.TEXTURE0);
      gl.bindTexture(gl.TEXTURE_2D, inputTex);
      gl.uniform1i(gl.getUniformLocation(prog.program, "u_texture"), 0);
    }

    function postPass(prog, inputTex, targetFBO, w, h) {
      beginPostPass(prog, inputTex, targetFBO ? targetFBO.fbo : null, w, h);
      drawSceneFullscreenQuad(gl, quad.vao);
      return targetFBO ? targetFBO.colorTex : null;
    }

    function applyToneMapping(inputTex, effect, targetFBO, w, h) {
      var prog = getProgram("toneMapping", SCENE_POST_TONEMAPPING_SOURCE);
      if (!prog) return inputTex;
      beginPostPass(prog, inputTex, targetFBO ? targetFBO.fbo : null, w, h);
      gl.uniform1f(gl.getUniformLocation(prog.program, "u_exposure"), sceneNumber(effect.exposure, 1.0));
      drawSceneFullscreenQuad(gl, quad.vao);
      return targetFBO ? targetFBO.colorTex : null;
    }

    function applyBloom(inputTex, effect, targetFBO, passW, passH, scaledW, scaledH) {
      var brightProg = getProgram("bloomBright", SCENE_POST_BLOOM_BRIGHT_SOURCE);
      var blurProg = getProgram("bloomBlur", SCENE_POST_BLUR_SOURCE);
      var compositeProg = getProgram("bloomComposite", SCENE_POST_BLOOM_COMPOSITE_SOURCE);
      if (!brightProg || !blurProg || !compositeProg) return inputTex;

      var bloomScale = (effect.scale > 0 && effect.scale <= 1) ? effect.scale : 0.5;
      var halfW = Math.max(1, Math.floor(scaledW * bloomScale));
      var halfH = Math.max(1, Math.floor(scaledH * bloomScale));

      if (!pingPong || pingPong.a.width !== halfW || pingPong.a.height !== halfH) {
        if (pingPong) {
          disposeScenePostFBO(gl, pingPong.a);
          disposeScenePostFBO(gl, pingPong.b);
        }
        pingPong = createScenePostPingPong(gl, halfW, halfH);
      }

      var threshold = sceneNumber(effect.threshold, 0.8);
      var radius = sceneNumber(effect.radius, 5.0);
      var intensity = sceneNumber(effect.intensity, 0.5);

      beginPostPass(brightProg, inputTex, pingPong.a.fbo, halfW, halfH);
      gl.uniform1f(gl.getUniformLocation(brightProg.program, "u_threshold"), threshold);
      drawSceneFullscreenQuad(gl, quad.vao);

      beginPostPass(blurProg, pingPong.a.colorTex, pingPong.b.fbo, halfW, halfH);
      gl.uniform2f(gl.getUniformLocation(blurProg.program, "u_direction"), 1.0, 0.0);
      gl.uniform1f(gl.getUniformLocation(blurProg.program, "u_radius"), radius);
      drawSceneFullscreenQuad(gl, quad.vao);

      beginPostPass(blurProg, pingPong.b.colorTex, pingPong.a.fbo, halfW, halfH);
      gl.uniform2f(gl.getUniformLocation(blurProg.program, "u_direction"), 0.0, 1.0);
      gl.uniform1f(gl.getUniformLocation(blurProg.program, "u_radius"), radius);
      drawSceneFullscreenQuad(gl, quad.vao);

      beginPostPass(compositeProg, inputTex, targetFBO ? targetFBO.fbo : null, passW, passH);
      gl.activeTexture(gl.TEXTURE1);
      gl.bindTexture(gl.TEXTURE_2D, pingPong.a.colorTex);
      gl.uniform1i(gl.getUniformLocation(compositeProg.program, "u_bloomTexture"), 1);
      gl.uniform1f(gl.getUniformLocation(compositeProg.program, "u_intensity"), intensity);
      drawSceneFullscreenQuad(gl, quad.vao);

      return targetFBO ? targetFBO.colorTex : null;
    }

    function applyVignette(inputTex, effect, targetFBO, w, h) {
      var prog = getProgram("vignette", SCENE_POST_VIGNETTE_SOURCE);
      if (!prog) return inputTex;
      beginPostPass(prog, inputTex, targetFBO ? targetFBO.fbo : null, w, h);
      gl.uniform1f(gl.getUniformLocation(prog.program, "u_intensity"), sceneNumber(effect.intensity, 1.0));
      drawSceneFullscreenQuad(gl, quad.vao);
      return targetFBO ? targetFBO.colorTex : null;
    }

    function applyColorGrade(inputTex, effect, targetFBO, w, h) {
      var prog = getProgram("colorGrade", SCENE_POST_COLORGRADE_SOURCE);
      if (!prog) return inputTex;
      beginPostPass(prog, inputTex, targetFBO ? targetFBO.fbo : null, w, h);
      gl.uniform1f(gl.getUniformLocation(prog.program, "u_exposure"), sceneNumber(effect.exposure, 1.0));
      gl.uniform1f(gl.getUniformLocation(prog.program, "u_contrast"), sceneNumber(effect.contrast, 1.0));
      gl.uniform1f(gl.getUniformLocation(prog.program, "u_saturation"), sceneNumber(effect.saturation, 1.0));
      drawSceneFullscreenQuad(gl, quad.vao);
      return targetFBO ? targetFBO.colorTex : null;
    }

    var blitProg = null;
    var SCENE_POST_BLIT_SOURCE = [
      "#version 300 es",
      "precision highp float;",
      "in vec2 v_uv;",
      "uniform sampler2D u_texture;",
      "out vec4 fragColor;",
      "void main() {",
      "    fragColor = texture(u_texture, v_uv);",
      "}",
    ].join("\n");

    function blitToScreen(inputTex, w, h) {
      if (!blitProg) {
        blitProg = createScenePostProgram(gl, SCENE_POST_BLIT_SOURCE);
      }
      if (!blitProg) return;
      postPass(blitProg, inputTex, null, w, h);
    }

    return {
      begin: function(canvasW, canvasH, maxPixels) {
        var factor = resolvePostFXFactor(maxPixels, canvasW * canvasH);
        var sw = Math.max(1, Math.floor(canvasW * factor));
        var sh = Math.max(1, Math.floor(canvasH * factor));

        if (sw !== currentWidth || sh !== currentHeight) {
          if (sceneFBO) disposeScenePostFBO(gl, sceneFBO);
          sceneFBO = createScenePostFBO(gl, sw, sh);
          if (auxFBO) disposeScenePostFBO(gl, auxFBO);
          auxFBO = null;
          currentWidth = sw;
          currentHeight = sh;
        }
        gl.bindFramebuffer(gl.FRAMEBUFFER, sceneFBO.fbo);
        return { width: sw, height: sh, factor: factor };
      },

      apply: function(effects, scaledW, scaledH, canvasW, canvasH) {
        gl.bindFramebuffer(gl.FRAMEBUFFER, null);
        gl.disable(gl.DEPTH_TEST);

        var currentTexture = sceneFBO.colorTex;

        if (effects.length > 1 && !auxFBO) {
          auxFBO = createScenePostFBO(gl, scaledW, scaledH);
        }

        for (var i = 0; i < effects.length; i++) {
          var effect = effects[i];
          var isLast = (i === effects.length - 1);

          var targetFBO = null;
          if (!isLast) {
            targetFBO = (currentTexture === sceneFBO.colorTex) ? auxFBO : sceneFBO;
          }

          var passW = isLast ? canvasW : scaledW;
          var passH = isLast ? canvasH : scaledH;

          switch (effect.kind) {
            case SCENE_POST_TONE_MAPPING:
              currentTexture = applyToneMapping(currentTexture, effect, targetFBO, passW, passH);
              break;
            case SCENE_POST_BLOOM:
              currentTexture = applyBloom(currentTexture, effect, targetFBO, passW, passH, scaledW, scaledH);
              break;
            case SCENE_POST_VIGNETTE:
              currentTexture = applyVignette(currentTexture, effect, targetFBO, passW, passH);
              break;
            case SCENE_POST_COLOR_GRADE:
              currentTexture = applyColorGrade(currentTexture, effect, targetFBO, passW, passH);
              break;
            default:
              break;
          }

          if (isLast && currentTexture === null) break;
        }

        if (currentTexture !== null && effects.length > 0) {
          blitToScreen(currentTexture, canvasW, canvasH);
        } else if (effects.length === 0) {
          blitToScreen(sceneFBO.colorTex, canvasW, canvasH);
        }

        gl.enable(gl.DEPTH_TEST);
      },

      dispose: function() {
        if (sceneFBO) {
          disposeScenePostFBO(gl, sceneFBO);
          sceneFBO = null;
        }
        if (auxFBO) {
          disposeScenePostFBO(gl, auxFBO);
          auxFBO = null;
        }
        if (pingPong) {
          disposeScenePostFBO(gl, pingPong.a);
          disposeScenePostFBO(gl, pingPong.b);
          pingPong = null;
        }
        for (var key in programs) {
          if (programs[key]) {
            gl.deleteShader(programs[key].vertexShader);
            gl.deleteShader(programs[key].fragmentShader);
            gl.deleteProgram(programs[key].program);
          }
        }
        programs = {};
        if (blitProg) {
          gl.deleteShader(blitProg.vertexShader);
          gl.deleteShader(blitProg.fragmentShader);
          gl.deleteProgram(blitProg.program);
          blitProg = null;
        }
        if (quad) {
          gl.deleteBuffer(quad.vbo);
          gl.deleteVertexArray(quad.vao);
        }
        currentWidth = 0;
        currentHeight = 0;
      },
    };
  }

  function scenePBRLoadTexture(gl, url, cache) {
    if (!cache) return null;
    const textureMap = cache;
    const key = typeof url === "string" ? url.trim() : "";
    if (!key) {
      return null;
    }
    if (textureMap.has(key)) {
      return textureMap.get(key);
    }

    const texture = gl.createTexture();
    const record = { texture: texture, src: key, loaded: false, failed: false };
    textureMap.set(key, record);

    gl.bindTexture(gl.TEXTURE_2D, texture);
    gl.texImage2D(gl.TEXTURE_2D, 0, gl.RGBA, 1, 1, 0, gl.RGBA, gl.UNSIGNED_BYTE, new Uint8Array([255, 255, 255, 255]));
    gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_MIN_FILTER, gl.LINEAR);
    gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_MAG_FILTER, gl.LINEAR);
    gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_WRAP_S, gl.CLAMP_TO_EDGE);
    gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_WRAP_T, gl.CLAMP_TO_EDGE);

    if (typeof Image === "function") {
      const image = new Image();
      image.onload = function() {
        gl.bindTexture(gl.TEXTURE_2D, texture);
        gl.texImage2D(gl.TEXTURE_2D, 0, gl.RGBA, gl.RGBA, gl.UNSIGNED_BYTE, image);
        gl.generateMipmap(gl.TEXTURE_2D);
        gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_MIN_FILTER, gl.LINEAR_MIPMAP_LINEAR);
        record.loaded = true;
      };
      image.onerror = function() {
        record.failed = true;
      };
      image.src = key;
    }

    return record;
  }

  function scenePBRBindTexture(gl, unit, texture) {
    gl.activeTexture(gl.TEXTURE0 + unit);
    gl.bindTexture(gl.TEXTURE_2D, texture);
  }

  function scenePBRCacheBaseUniforms(gl, program) {
    var uniforms = {
      viewMatrix: gl.getUniformLocation(program, "u_viewMatrix"),
      projectionMatrix: gl.getUniformLocation(program, "u_projectionMatrix"),
      cameraPosition: gl.getUniformLocation(program, "u_cameraPosition"),

      albedo: gl.getUniformLocation(program, "u_albedo"),
      roughness: gl.getUniformLocation(program, "u_roughness"),
      metalness: gl.getUniformLocation(program, "u_metalness"),
      emissive: gl.getUniformLocation(program, "u_emissive"),
      opacity: gl.getUniformLocation(program, "u_opacity"),
      unlit: gl.getUniformLocation(program, "u_unlit"),

      albedoMap: gl.getUniformLocation(program, "u_albedoMap"),
      normalMap: gl.getUniformLocation(program, "u_normalMap"),
      roughnessMap: gl.getUniformLocation(program, "u_roughnessMap"),
      metalnessMap: gl.getUniformLocation(program, "u_metalnessMap"),
      emissiveMap: gl.getUniformLocation(program, "u_emissiveMap"),
      hasAlbedoMap: gl.getUniformLocation(program, "u_hasAlbedoMap"),
      hasNormalMap: gl.getUniformLocation(program, "u_hasNormalMap"),
      hasRoughnessMap: gl.getUniformLocation(program, "u_hasRoughnessMap"),
      hasMetalnessMap: gl.getUniformLocation(program, "u_hasMetalnessMap"),
      hasEmissiveMap: gl.getUniformLocation(program, "u_hasEmissiveMap"),

      lightCount: gl.getUniformLocation(program, "u_lightCount"),
      lightTypes: [],
      lightPositions: [],
      lightDirections: [],
      lightColors: [],
      lightIntensities: [],
      lightRanges: [],
      lightDecays: [],
      lightAngles: [],
      lightPenumbras: [],
      lightGroundColors: [],

      ambientColor: gl.getUniformLocation(program, "u_ambientColor"),
      ambientIntensity: gl.getUniformLocation(program, "u_ambientIntensity"),
      skyColor: gl.getUniformLocation(program, "u_skyColor"),
      skyIntensity: gl.getUniformLocation(program, "u_skyIntensity"),
      groundColor: gl.getUniformLocation(program, "u_groundColor"),
      groundIntensity: gl.getUniformLocation(program, "u_groundIntensity"),

      shadowMap0: gl.getUniformLocation(program, "u_shadowMap0"),
      lightSpaceMatrix0: gl.getUniformLocation(program, "u_lightSpaceMatrix0"),
      hasShadow0: gl.getUniformLocation(program, "u_hasShadow0"),
      shadowBias0: gl.getUniformLocation(program, "u_shadowBias0"),
      shadowLightIndex0: gl.getUniformLocation(program, "u_shadowLightIndex0"),

      shadowMap1: gl.getUniformLocation(program, "u_shadowMap1"),
      lightSpaceMatrix1: gl.getUniformLocation(program, "u_lightSpaceMatrix1"),
      hasShadow1: gl.getUniformLocation(program, "u_hasShadow1"),
      shadowBias1: gl.getUniformLocation(program, "u_shadowBias1"),
      shadowLightIndex1: gl.getUniformLocation(program, "u_shadowLightIndex1"),

      receiveShadow: gl.getUniformLocation(program, "u_receiveShadow"),

      exposure: gl.getUniformLocation(program, "u_exposure"),
      toneMapMode: gl.getUniformLocation(program, "u_toneMapMode"),

      hasFog: gl.getUniformLocation(program, "u_hasFog"),
      fogDensity: gl.getUniformLocation(program, "u_fogDensity"),
      fogColor: gl.getUniformLocation(program, "u_fogColor"),
    };

    for (var i = 0; i < 8; i++) {
      uniforms.lightTypes.push(gl.getUniformLocation(program, "u_lightTypes[" + i + "]"));
      uniforms.lightPositions.push(gl.getUniformLocation(program, "u_lightPositions[" + i + "]"));
      uniforms.lightDirections.push(gl.getUniformLocation(program, "u_lightDirections[" + i + "]"));
      uniforms.lightColors.push(gl.getUniformLocation(program, "u_lightColors[" + i + "]"));
      uniforms.lightIntensities.push(gl.getUniformLocation(program, "u_lightIntensities[" + i + "]"));
      uniforms.lightRanges.push(gl.getUniformLocation(program, "u_lightRanges[" + i + "]"));
      uniforms.lightDecays.push(gl.getUniformLocation(program, "u_lightDecays[" + i + "]"));
      uniforms.lightAngles.push(gl.getUniformLocation(program, "u_lightAngles[" + i + "]"));
      uniforms.lightPenumbras.push(gl.getUniformLocation(program, "u_lightPenumbras[" + i + "]"));
      uniforms.lightGroundColors.push(gl.getUniformLocation(program, "u_lightGroundColors[" + i + "]"));
    }

    return uniforms;
  }

  function createScenePBRProgram(gl) {
    const vertexShader = scenePBRCompileShader(gl, gl.VERTEX_SHADER, SCENE_PBR_VERTEX_SOURCE);
    if (!vertexShader) {
      return null;
    }
    const fragmentShader = scenePBRCompileShader(gl, gl.FRAGMENT_SHADER, SCENE_PBR_FRAGMENT_SOURCE);
    if (!fragmentShader) {
      gl.deleteShader(vertexShader);
      return null;
    }

    const program = scenePBRLinkProgram(gl, vertexShader, fragmentShader, "PBR shader");
    if (!program) return null;

    const attributes = {
      position: gl.getAttribLocation(program, "a_position"),
      normal: gl.getAttribLocation(program, "a_normal"),
      uv: gl.getAttribLocation(program, "a_uv"),
      tangent: gl.getAttribLocation(program, "a_tangent"),
    };

    const uniforms = scenePBRCacheBaseUniforms(gl, program);

    return {
      program: program,
      vertexShader: vertexShader,
      fragmentShader: fragmentShader,
      attributes: attributes,
      uniforms: uniforms,
    };
  }

  function createScenePBRSkinnedProgram(gl) {
    var vertexShader = scenePBRCompileShader(gl, gl.VERTEX_SHADER, SCENE_PBR_SKINNED_VERTEX_SOURCE);
    if (!vertexShader) return null;
    var fragmentShader = scenePBRCompileShader(gl, gl.FRAGMENT_SHADER, SCENE_PBR_FRAGMENT_SOURCE);
    if (!fragmentShader) {
      gl.deleteShader(vertexShader);
      return null;
    }

    var program = scenePBRLinkProgram(gl, vertexShader, fragmentShader, "Skinned PBR shader");
    if (!program) return null;

    var attributes = {
      position: gl.getAttribLocation(program, "a_position"),
      normal: gl.getAttribLocation(program, "a_normal"),
      uv: gl.getAttribLocation(program, "a_uv"),
      tangent: gl.getAttribLocation(program, "a_tangent"),
      joints: gl.getAttribLocation(program, "a_joints"),
      weights: gl.getAttribLocation(program, "a_weights"),
    };

    var uniforms = scenePBRCacheBaseUniforms(gl, program);
    uniforms.hasSkin = gl.getUniformLocation(program, "u_hasSkin");
    uniforms.jointMatrices = [];

    for (var j = 0; j < 64; j++) {
      uniforms.jointMatrices.push(gl.getUniformLocation(program, "u_jointMatrices[" + j + "]"));
    }

    return {
      program: program,
      vertexShader: vertexShader,
      fragmentShader: fragmentShader,
      attributes: attributes,
      uniforms: uniforms,
    };
  }

  function createScenePointsProgram(gl) {
    var vertexShader = scenePBRCompileShader(gl, gl.VERTEX_SHADER, SCENE_POINTS_VERTEX_SOURCE);
    if (!vertexShader) return null;
    var fragmentShader = scenePBRCompileShader(gl, gl.FRAGMENT_SHADER, SCENE_POINTS_FRAGMENT_SOURCE);
    if (!fragmentShader) {
      gl.deleteShader(vertexShader);
      return null;
    }

    var program = scenePBRLinkProgram(gl, vertexShader, fragmentShader, "Points shader");
    if (!program) return null;

    var attributes = {
      position: gl.getAttribLocation(program, "a_position"),
      size: gl.getAttribLocation(program, "a_size"),
      color: gl.getAttribLocation(program, "a_color"),
    };

    var uniforms = {
      viewMatrix: gl.getUniformLocation(program, "u_viewMatrix"),
      projectionMatrix: gl.getUniformLocation(program, "u_projectionMatrix"),
      modelMatrix: gl.getUniformLocation(program, "u_modelMatrix"),
      defaultSize: gl.getUniformLocation(program, "u_defaultSize"),
      defaultColor: gl.getUniformLocation(program, "u_defaultColor"),
      hasPerVertexColor: gl.getUniformLocation(program, "u_hasPerVertexColor"),
      hasPerVertexSize: gl.getUniformLocation(program, "u_hasPerVertexSize"),
      sizeAttenuation: gl.getUniformLocation(program, "u_sizeAttenuation"),
      pointStyle: gl.getUniformLocation(program, "u_pointStyle"),
      viewportHeight: gl.getUniformLocation(program, "u_viewportHeight"),
      opacity: gl.getUniformLocation(program, "u_opacity"),
      hasFog: gl.getUniformLocation(program, "u_hasFog"),
      fogDensity: gl.getUniformLocation(program, "u_fogDensity"),
      fogColor: gl.getUniformLocation(program, "u_fogColor"),
    };

    return {
      program: program,
      vertexShader: vertexShader,
      fragmentShader: fragmentShader,
      attributes: attributes,
      uniforms: uniforms,
    };
  }

  function createScenePBRInstancedProgram(gl) {
    var vertexShader = scenePBRCompileShader(gl, gl.VERTEX_SHADER, SCENE_PBR_INSTANCED_VERTEX_SOURCE);
    if (!vertexShader) return null;
    var fragmentShader = scenePBRCompileShader(gl, gl.FRAGMENT_SHADER, SCENE_PBR_FRAGMENT_SOURCE);
    if (!fragmentShader) {
      gl.deleteShader(vertexShader);
      return null;
    }

    var program = scenePBRLinkProgram(gl, vertexShader, fragmentShader, "Instanced PBR shader");
    if (!program) return null;

    var attributes = {
      position: gl.getAttribLocation(program, "a_position"),
      normal: gl.getAttribLocation(program, "a_normal"),
      uv: gl.getAttribLocation(program, "a_uv"),
      tangent: gl.getAttribLocation(program, "a_tangent"),
      instanceMatrix: gl.getAttribLocation(program, "a_instanceMatrix"),
    };

    var uniforms = scenePBRCacheBaseUniforms(gl, program);

    return {
      program: program,
      vertexShader: vertexShader,
      fragmentShader: fragmentShader,
      attributes: attributes,
      uniforms: uniforms,
    };
  }

  function generateInstancedGeometry(kind, dims) {
    var w = sceneNumber(dims && dims.width, 1);
    var h = sceneNumber(dims && dims.height, 1);
    var d = sceneNumber(dims && dims.depth, 1);

    if (kind === "sphere") {
      return generateInstancedSphereGeometry(
        sceneNumber(dims && dims.radius, 0.5),
        sceneNumber(dims && dims.segments, 16)
      );
    }
    if (kind === "plane") {
      return generateInstancedPlaneGeometry(w, d);
    }

    return generateInstancedBoxGeometry(w, h, d);
  }

  function generateInstancedBoxGeometry(w, h, d) {
    var hw = w * 0.5, hh = h * 0.5, hd = d * 0.5;

    var faces = [
      { n: [0, 0, 1], t: [1, 0, 0, 1], v: [[-hw,-hh,hd],[hw,-hh,hd],[hw,hh,hd],[-hw,hh,hd]] },
      { n: [0, 0,-1], t: [-1, 0, 0, 1], v: [[hw,-hh,-hd],[-hw,-hh,-hd],[-hw,hh,-hd],[hw,hh,-hd]] },
      { n: [1, 0, 0], t: [0, 0,-1, 1], v: [[hw,-hh,hd],[hw,-hh,-hd],[hw,hh,-hd],[hw,hh,hd]] },
      { n: [-1, 0, 0], t: [0, 0, 1, 1], v: [[-hw,-hh,-hd],[-hw,-hh,hd],[-hw,hh,hd],[-hw,hh,-hd]] },
      { n: [0, 1, 0], t: [1, 0, 0, 1], v: [[-hw,hh,hd],[hw,hh,hd],[hw,hh,-hd],[-hw,hh,-hd]] },
      { n: [0,-1, 0], t: [1, 0, 0, 1], v: [[-hw,-hh,-hd],[hw,-hh,-hd],[hw,-hh,hd],[-hw,-hh,hd]] },
    ];

    var quadUVs = [[0,0],[1,0],[1,1],[0,1]];
    var triIndices = [0,1,2, 0,2,3];

    var vertexCount = 36;
    var positions = new Float32Array(vertexCount * 3);
    var normals = new Float32Array(vertexCount * 3);
    var uvs = new Float32Array(vertexCount * 2);
    var tangents = new Float32Array(vertexCount * 4);

    var vi = 0;
    for (var fi = 0; fi < 6; fi++) {
      var face = faces[fi];
      for (var ti = 0; ti < 6; ti++) {
        var ci = triIndices[ti];
        var p = face.v[ci];
        positions[vi * 3]     = p[0];
        positions[vi * 3 + 1] = p[1];
        positions[vi * 3 + 2] = p[2];
        normals[vi * 3]     = face.n[0];
        normals[vi * 3 + 1] = face.n[1];
        normals[vi * 3 + 2] = face.n[2];
        uvs[vi * 2]     = quadUVs[ci][0];
        uvs[vi * 2 + 1] = quadUVs[ci][1];
        tangents[vi * 4]     = face.t[0];
        tangents[vi * 4 + 1] = face.t[1];
        tangents[vi * 4 + 2] = face.t[2];
        tangents[vi * 4 + 3] = face.t[3];
        vi++;
      }
    }

    return { positions: positions, normals: normals, uvs: uvs, tangents: tangents, vertexCount: vertexCount };
  }

  function generateInstancedPlaneGeometry(w, d) {
    var hw = w * 0.5, hd = d * 0.5;
    var vertexCount = 6;
    var positions = new Float32Array(vertexCount * 3);
    var normals = new Float32Array(vertexCount * 3);
    var uvs = new Float32Array(vertexCount * 2);
    var tangents = new Float32Array(vertexCount * 4);

    var corners = [[-hw, 0, hd], [hw, 0, hd], [hw, 0, -hd], [-hw, 0, -hd]];
    var cornerUVs = [[0, 0], [1, 0], [1, 1], [0, 1]];
    var triIndices = [0, 1, 2, 0, 2, 3];

    for (var i = 0; i < 6; i++) {
      var ci = triIndices[i];
      var p = corners[ci];
      positions[i * 3] = p[0]; positions[i * 3 + 1] = p[1]; positions[i * 3 + 2] = p[2];
      normals[i * 3] = 0; normals[i * 3 + 1] = 1; normals[i * 3 + 2] = 0;
      uvs[i * 2] = cornerUVs[ci][0]; uvs[i * 2 + 1] = cornerUVs[ci][1];
      tangents[i * 4] = 1; tangents[i * 4 + 1] = 0; tangents[i * 4 + 2] = 0; tangents[i * 4 + 3] = 1;
    }

    return { positions: positions, normals: normals, uvs: uvs, tangents: tangents, vertexCount: vertexCount };
  }

  function generateInstancedSphereGeometry(radius, segments) {
    var rings = Math.max(4, segments);
    var slices = Math.max(4, segments * 2);

    var vertexCount = rings * slices * 6;
    var positions = new Float32Array(vertexCount * 3);
    var normals = new Float32Array(vertexCount * 3);
    var uvs = new Float32Array(vertexCount * 2);
    var tangents = new Float32Array(vertexCount * 4);
    var vi = 0;

    function spherePoint(ring, slice) {
      var phi = (ring / rings) * Math.PI;
      var theta = (slice / slices) * Math.PI * 2;
      var sp = Math.sin(phi);
      var nx = sp * Math.cos(theta);
      var ny = Math.cos(phi);
      var nz = sp * Math.sin(theta);
      return {
        px: nx * radius, py: ny * radius, pz: nz * radius,
        nx: nx, ny: ny, nz: nz,
        u: slice / slices, v: ring / rings,
        tx: -Math.sin(theta), ty: 0, tz: Math.cos(theta),
      };
    }

    function pushVert(pt) {
      positions[vi * 3] = pt.px; positions[vi * 3 + 1] = pt.py; positions[vi * 3 + 2] = pt.pz;
      normals[vi * 3] = pt.nx; normals[vi * 3 + 1] = pt.ny; normals[vi * 3 + 2] = pt.nz;
      uvs[vi * 2] = pt.u; uvs[vi * 2 + 1] = pt.v;
      tangents[vi * 4] = pt.tx; tangents[vi * 4 + 1] = pt.ty; tangents[vi * 4 + 2] = pt.tz; tangents[vi * 4 + 3] = 1;
      vi++;
    }

    for (var r = 0; r < rings; r++) {
      for (var s = 0; s < slices; s++) {
        var a = spherePoint(r, s);
        var b = spherePoint(r, s + 1);
        var c = spherePoint(r + 1, s + 1);
        var dd = spherePoint(r + 1, s);
        pushVert(a); pushVert(b); pushVert(c);
        pushVert(a); pushVert(c); pushVert(dd);
      }
    }

    return { positions: positions, normals: normals, uvs: uvs, tangents: tangents, vertexCount: vi };
  }

  function scenePBRCompileShader(gl, type, source) {
    const shader = gl.createShader(type);
    gl.shaderSource(shader, source);
    gl.compileShader(shader);
    if (!gl.getShaderParameter(shader, gl.COMPILE_STATUS)) {
      const label = type === gl.VERTEX_SHADER ? "vertex" : "fragment";
      console.warn("[gosx] PBR " + label + " shader compile failed:", gl.getShaderInfoLog(shader));
      gl.deleteShader(shader);
      return null;
    }
    return shader;
  }

  function scenePBRLinkProgram(gl, vertexShader, fragmentShader, label) {
    var program = gl.createProgram();
    gl.attachShader(program, vertexShader);
    gl.attachShader(program, fragmentShader);
    gl.linkProgram(program);
    if (!gl.getProgramParameter(program, gl.LINK_STATUS)) {
      console.warn("[gosx] " + label + " program link failed:", gl.getProgramInfoLog(program));
      gl.deleteProgram(program);
      gl.deleteShader(vertexShader);
      gl.deleteShader(fragmentShader);
      return null;
    }
    return program;
  }

  var _scenePBRLightsHashBuf = new ArrayBuffer(4);
  var _scenePBRLightsHashFloat = new Float32Array(_scenePBRLightsHashBuf);
  var _scenePBRLightsHashInt = new Uint32Array(_scenePBRLightsHashBuf);

  function scenePBRLightsHashNumber(h, n) {
    _scenePBRLightsHashFloat[0] = (typeof n === "number" && n === n) ? n : 0;
    return Math.imul((h ^ _scenePBRLightsHashInt[0]) >>> 0, 16777619) >>> 0;
  }

  function scenePBRLightsHashString(h, s) {
    var str = (typeof s === "string") ? s : "";
    var len = str.length;
    for (var i = 0; i < len; i++) {
      h = Math.imul((h ^ str.charCodeAt(i)) >>> 0, 16777619) >>> 0;
    }
    return Math.imul((h ^ (len + 1)) >>> 0, 16777619) >>> 0;
  }

  function hashLightContent(l) {
    if (!l) return 0;
    var h = 2166136261;
    h = scenePBRLightsHashString(h, l.kind);
    h = scenePBRLightsHashNumber(h, sceneNumber(l.x, 0));
    h = scenePBRLightsHashNumber(h, sceneNumber(l.y, 0));
    h = scenePBRLightsHashNumber(h, sceneNumber(l.z, 0));
    h = scenePBRLightsHashNumber(h, sceneNumber(l.directionX, 0));
    h = scenePBRLightsHashNumber(h, sceneNumber(l.directionY, -1));
    h = scenePBRLightsHashNumber(h, sceneNumber(l.directionZ, 0));
    h = scenePBRLightsHashString(h, l.color);
    h = scenePBRLightsHashNumber(h, sceneNumber(l.intensity, 1));
    h = scenePBRLightsHashNumber(h, sceneNumber(l.range, 0));
    h = scenePBRLightsHashNumber(h, sceneNumber(l.decay, 2));
    h = scenePBRLightsHashNumber(h, sceneNumber(l.angle, 0));
    h = scenePBRLightsHashNumber(h, sceneNumber(l.penumbra, 0));
    h = scenePBRLightsHashString(h, l.groundColor);
    return h;
  }

  function hashEnvironmentContent(env) {
    if (!env) return 0;
    var h = 2166136261;
    h = scenePBRLightsHashString(h, env.ambientColor);
    h = scenePBRLightsHashNumber(h, sceneNumber(env.ambientIntensity, 0));
    h = scenePBRLightsHashString(h, env.skyColor);
    h = scenePBRLightsHashNumber(h, sceneNumber(env.skyIntensity, 0));
    h = scenePBRLightsHashString(h, env.groundColor);
    h = scenePBRLightsHashNumber(h, sceneNumber(env.groundIntensity, 0));
    h = scenePBRLightsHashNumber(h, sceneNumber(env.fogDensity, 0));
    h = scenePBRLightsHashString(h, env.fogColor);
    return h;
  }

  function scenePBRLightsHash(lights, environment) {
    var h = 2166136261;
    var lightArray = Array.isArray(lights) ? lights : [];
    var count = Math.min(lightArray.length, 8);
    h = Math.imul((h ^ count) >>> 0, 16777619) >>> 0;
    for (var i = 0; i < count; i++) {
      var l = lightArray[i];
      var sub = (l && typeof l._lightHash === "number") ? l._lightHash : hashLightContent(l);
      h = Math.imul((h ^ (sub >>> 0)) >>> 0, 16777619) >>> 0;
    }
    var envSub = (environment && typeof environment._envHash === "number")
      ? environment._envHash
      : hashEnvironmentContent(environment);
    h = Math.imul((h ^ (envSub >>> 0)) >>> 0, 16777619) >>> 0;
    return h;
  }

  function scenePBRUploadLights(gl, uniforms, lights, environment, precomputedHash) {
    const contentHash = (typeof precomputedHash === "number")
      ? precomputedHash
      : scenePBRLightsHash(lights, environment);
    if (uniforms._lastLightsHash === contentHash) {
      return;
    }
    uniforms._lastLightsHash = contentHash;

    const lightArray = Array.isArray(lights) ? lights : [];
    const count = Math.min(lightArray.length, 8);

    var colorCache = {};

    gl.uniform1i(uniforms.lightCount, count);

    for (var i = 0; i < count; i++) {
      const light = lightArray[i];
      const kind = typeof light.kind === "string" ? light.kind.toLowerCase() : "";

      var lightType = 2; // default: point
      if (kind === "ambient") {
        lightType = 0;
      } else if (kind === "directional") {
        lightType = 1;
      } else if (kind === "spot") {
        lightType = 3;
      } else if (kind === "hemisphere") {
        lightType = 4;
      }

      gl.uniform1i(uniforms.lightTypes[i], lightType);
      gl.uniform3f(
        uniforms.lightPositions[i],
        sceneNumber(light.x, 0),
        sceneNumber(light.y, 0),
        sceneNumber(light.z, 0)
      );
      gl.uniform3f(
        uniforms.lightDirections[i],
        sceneNumber(light.directionX, 0),
        sceneNumber(light.directionY, -1),
        sceneNumber(light.directionZ, 0)
      );

      var colorKey = light.color;
      var lightColorRGBA = typeof colorKey === "string" && colorCache[colorKey];
      if (!lightColorRGBA) {
        lightColorRGBA = sceneColorRGBA(light.color, [1, 1, 1, 1]);
        if (typeof colorKey === "string") colorCache[colorKey] = lightColorRGBA;
      }
      gl.uniform3f(uniforms.lightColors[i], lightColorRGBA[0], lightColorRGBA[1], lightColorRGBA[2]);
      gl.uniform1f(uniforms.lightIntensities[i], sceneNumber(light.intensity, 1));
      gl.uniform1f(uniforms.lightRanges[i], sceneNumber(light.range, 0));
      gl.uniform1f(uniforms.lightDecays[i], sceneNumber(light.decay, 2));
      gl.uniform1f(uniforms.lightAngles[i], sceneNumber(light.angle, 0));
      gl.uniform1f(uniforms.lightPenumbras[i], sceneNumber(light.penumbra, 0));

      var gcKey = light.groundColor;
      var gcRGBA = typeof gcKey === "string" && colorCache[gcKey];
      if (!gcRGBA) {
        gcRGBA = sceneColorRGBA(light.groundColor, [0, 0, 0, 1]);
        if (typeof gcKey === "string") colorCache[gcKey] = gcRGBA;
      }
      gl.uniform3f(uniforms.lightGroundColors[i], gcRGBA[0], gcRGBA[1], gcRGBA[2]);
    }

    for (var j = count; j < 8; j++) {
      gl.uniform1i(uniforms.lightTypes[j], 0);
      gl.uniform3f(uniforms.lightPositions[j], 0, 0, 0);
      gl.uniform3f(uniforms.lightDirections[j], 0, -1, 0);
      gl.uniform3f(uniforms.lightColors[j], 0, 0, 0);
      gl.uniform1f(uniforms.lightIntensities[j], 0);
      gl.uniform1f(uniforms.lightRanges[j], 0);
      gl.uniform1f(uniforms.lightDecays[j], 2);
      gl.uniform1f(uniforms.lightAngles[j], 0);
      gl.uniform1f(uniforms.lightPenumbras[j], 0);
      gl.uniform3f(uniforms.lightGroundColors[j], 0, 0, 0);
    }

    const env = environment || {};
    var ambientKey = env.ambientColor;
    var ambientColorRGBA = typeof ambientKey === "string" && colorCache[ambientKey];
    if (!ambientColorRGBA) {
      ambientColorRGBA = sceneColorRGBA(env.ambientColor, [1, 1, 1, 1]);
      if (typeof ambientKey === "string") colorCache[ambientKey] = ambientColorRGBA;
    }
    gl.uniform3f(uniforms.ambientColor, ambientColorRGBA[0], ambientColorRGBA[1], ambientColorRGBA[2]);
    gl.uniform1f(uniforms.ambientIntensity, sceneNumber(env.ambientIntensity, 0));

    var skyKey = env.skyColor;
    var skyColorRGBA = typeof skyKey === "string" && colorCache[skyKey];
    if (!skyColorRGBA) {
      skyColorRGBA = sceneColorRGBA(env.skyColor, [0.88, 0.94, 1, 1]);
      if (typeof skyKey === "string") colorCache[skyKey] = skyColorRGBA;
    }
    gl.uniform3f(uniforms.skyColor, skyColorRGBA[0], skyColorRGBA[1], skyColorRGBA[2]);
    gl.uniform1f(uniforms.skyIntensity, sceneNumber(env.skyIntensity, 0));

    var groundKey = env.groundColor;
    var groundColorRGBA = typeof groundKey === "string" && colorCache[groundKey];
    if (!groundColorRGBA) {
      groundColorRGBA = sceneColorRGBA(env.groundColor, [0.12, 0.16, 0.22, 1]);
      if (typeof groundKey === "string") colorCache[groundKey] = groundColorRGBA;
    }
    gl.uniform3f(uniforms.groundColor, groundColorRGBA[0], groundColorRGBA[1], groundColorRGBA[2]);
    gl.uniform1f(uniforms.groundIntensity, sceneNumber(env.groundIntensity, 0));

    var fogDensity = sceneNumber(env.fogDensity, 0);
    gl.uniform1i(uniforms.hasFog, fogDensity > 0 ? 1 : 0);
    gl.uniform1f(uniforms.fogDensity, fogDensity);
    var fogKey = env.fogColor;
    var fogColorRGBA = typeof fogKey === "string" && colorCache[fogKey];
    if (!fogColorRGBA) {
      fogColorRGBA = sceneColorRGBA(env.fogColor, [0.5, 0.5, 0.5, 1]);
      if (typeof fogKey === "string") colorCache[fogKey] = fogColorRGBA;
    }
    gl.uniform3f(uniforms.fogColor, fogColorRGBA[0], fogColorRGBA[1], fogColorRGBA[2]);
  }

  function sceneToneMapMode(str) {
    if (typeof str === "string") {
      var s = str.toLowerCase();
      if (s === "linear") return 0;
      if (s === "reinhard") return 2;
    }
    return 1; // default: ACES
  }

  function scenePBRUploadExposure(gl, uniforms, environment, usePostProcessing) {
    var env = environment || {};
    var exposure = sceneNumber(env.exposure, 0);
    if (exposure <= 0) exposure = 1.0;
    var toneMapMode = usePostProcessing ? 0 : sceneToneMapMode(env.toneMapping);
    if (uniforms._lastExposure === exposure && uniforms._lastToneMapMode === toneMapMode) {
      return;
    }
    uniforms._lastExposure = exposure;
    uniforms._lastToneMapMode = toneMapMode;
    gl.uniform1f(uniforms.exposure, exposure);
    gl.uniform1i(uniforms.toneMapMode, toneMapMode);
  }

  function scenePBRUploadShadowUniforms(gl, uniforms, shadowSlots, shadowLightMatrices, shadowLightIndices, lights) {
    var lightArray = Array.isArray(lights) ? lights : [];

    if (shadowSlots[0] && shadowLightMatrices[0]) {
      scenePBRBindTexture(gl, 5, shadowSlots[0].depthTexture);
      gl.uniform1i(uniforms.shadowMap0, 5);
      gl.uniformMatrix4fv(uniforms.lightSpaceMatrix0, false, shadowLightMatrices[0]);
      gl.uniform1i(uniforms.hasShadow0, 1);
      var bias0 = sceneNumber(lightArray[shadowLightIndices[0]] && lightArray[shadowLightIndices[0]].shadowBias, 0.005);
      gl.uniform1f(uniforms.shadowBias0, bias0);
      gl.uniform1i(uniforms.shadowLightIndex0, shadowLightIndices[0]);
    } else {
      gl.uniform1i(uniforms.hasShadow0, 0);
      gl.uniform1i(uniforms.shadowLightIndex0, -1);
    }

    if (shadowSlots[1] && shadowLightMatrices[1]) {
      scenePBRBindTexture(gl, 6, shadowSlots[1].depthTexture);
      gl.uniform1i(uniforms.shadowMap1, 6);
      gl.uniformMatrix4fv(uniforms.lightSpaceMatrix1, false, shadowLightMatrices[1]);
      gl.uniform1i(uniforms.hasShadow1, 1);
      var bias1 = sceneNumber(lightArray[shadowLightIndices[1]] && lightArray[shadowLightIndices[1]].shadowBias, 0.005);
      gl.uniform1f(uniforms.shadowBias1, bias1);
      gl.uniform1i(uniforms.shadowLightIndex1, shadowLightIndices[1]);
    } else {
      gl.uniform1i(uniforms.hasShadow1, 0);
      gl.uniform1i(uniforms.shadowLightIndex1, -1);
    }
  }

  function createScenePBRRenderer(gl, canvas) {
    const pbrProgram = createScenePBRProgram(gl);
    if (!pbrProgram) {
      return null;
    }

    const program = pbrProgram.program;
    const attribs = pbrProgram.attributes;
    const uniforms = pbrProgram.uniforms;

    var skinnedProgram = null;

    const shadowProgram = createSceneShadowProgram(gl);

    var shadowSlots = [null, null];

    var postProcessor = null;

    var shadowLightMatrices = [null, null];
    var shadowLightIndices = [-1, -1];

    const positionBuffer = gl.createBuffer();
    const normalBuffer = gl.createBuffer();
    const uvBuffer = gl.createBuffer();
    const tangentBuffer = gl.createBuffer();
    const jointsBuffer = gl.createBuffer();
    const weightsBuffer = gl.createBuffer();

    var pointsProgram = null;

    const pointsPositionBuffer = gl.createBuffer();
    const pointsSizeBuffer = gl.createBuffer();
    const pointsColorBuffer = gl.createBuffer();
    var computeParticleSystems = new Map();
    var lastComputeParticleTimeSeconds = null;

    var instancedProgram = null;

    const instanceTransformBuffer = gl.createBuffer();
    const instanceVertexBuffer = gl.createBuffer();
    const instanceNormalBuffer = gl.createBuffer();
    const instanceUVBuffer = gl.createBuffer();
    const instanceTangentBuffer = gl.createBuffer();

    var instancedGeometryCache = {};

    const textureCache = new Map();

    var shadowState = { buffer: gl.createBuffer(), scratch: null };

    var scratchViewMatrix = new Float32Array(16);
    var scratchProjMatrix = new Float32Array(16);

    var _frameCam = {
      x: 0, y: 0, z: 0,
      rotationX: 0, rotationY: 0, rotationZ: 0,
      fov: 0, near: 0, far: 0,
    };
    var _frameLightsHash = 0;

    var scratchPositions = null;
    var scratchNormals = null;
    var scratchUVs = null;
    var scratchTangents = null;

    function ensureScratch(name, length) {
      if (name === "positions") {
        if (!scratchPositions || scratchPositions.length < length) {
          scratchPositions = new Float32Array(length);
        }
        return scratchPositions;
      }
      if (name === "normals") {
        if (!scratchNormals || scratchNormals.length < length) {
          scratchNormals = new Float32Array(length);
        }
        return scratchNormals;
      }
      if (name === "uvs") {
        if (!scratchUVs || scratchUVs.length < length) {
          scratchUVs = new Float32Array(length);
        }
        return scratchUVs;
      }
      if (name === "tangents") {
        if (!scratchTangents || scratchTangents.length < length) {
          scratchTangents = new Float32Array(length);
        }
        return scratchTangents;
      }
      return new Float32Array(length);
    }

    function sliceToFloat32(source, offset, count, stride, scratchName) {
      const length = count * stride;
      const start = offset * stride;
      const buf = ensureScratch(scratchName, length);
      for (var i = 0; i < length; i++) {
        buf[i] = source && source[start + i] !== undefined ? +source[start + i] : 0;
      }
      return buf.subarray(0, length);
    }

    function uploadMaterial(gl, uniforms, material, textureCache) {
      const mat = material || {};
      const albedoRGBA = sceneColorRGBA(mat.color, [0.8, 0.8, 0.8, 1]);
      gl.uniform3f(uniforms.albedo, albedoRGBA[0], albedoRGBA[1], albedoRGBA[2]);
      gl.uniform1f(uniforms.roughness, sceneNumber(mat.roughness, 0.5));
      gl.uniform1f(uniforms.metalness, sceneNumber(mat.metalness, 0));
      gl.uniform1f(uniforms.emissive, sceneNumber(mat.emissive, 0));
      gl.uniform1f(uniforms.opacity, clamp01(sceneNumber(mat.opacity, 1)));
      gl.uniform1i(uniforms.unlit, mat.unlit ? 1 : 0);

      var textureMaps = [
        { prop: "texture",      has: "hasAlbedoMap",    sampler: "albedoMap",    unit: 0 },
        { prop: "normalMap",    has: "hasNormalMap",     sampler: "normalMap",    unit: 1 },
        { prop: "roughnessMap", has: "hasRoughnessMap",  sampler: "roughnessMap", unit: 2 },
        { prop: "metalnessMap", has: "hasMetalnessMap",  sampler: "metalnessMap", unit: 3 },
        { prop: "emissiveMap",  has: "hasEmissiveMap",   sampler: "emissiveMap",  unit: 4 },
      ];
      for (var ti = 0; ti < textureMaps.length; ti++) {
        var tm = textureMaps[ti];
        var record = mat[tm.prop] ? scenePBRLoadTexture(gl, mat[tm.prop], textureCache) : null;
        var loaded = Boolean(record && record.texture && record.loaded);
        gl.uniform1i(uniforms[tm.has], loaded ? 1 : 0);
        if (loaded) {
          scenePBRBindTexture(gl, tm.unit, record.texture);
          gl.uniform1i(uniforms[tm.sampler], tm.unit);
        }
      }
    }

    function applyBlendMode(gl, renderPass) {
      if (renderPass === "alpha") {
        gl.enable(gl.BLEND);
        gl.blendFunc(gl.SRC_ALPHA, gl.ONE_MINUS_SRC_ALPHA);
      } else if (renderPass === "additive") {
        gl.enable(gl.BLEND);
        gl.blendFunc(gl.SRC_ALPHA, gl.ONE);
      } else {
        gl.disable(gl.BLEND);
      }
    }

    function applyDepthMode(gl, renderPass) {
      gl.enable(gl.DEPTH_TEST);
      if (renderPass === "opaque") {
        gl.depthMask(true);
        gl.depthFunc(gl.LEQUAL);
      } else {
        gl.depthMask(false);
        gl.depthFunc(gl.LEQUAL);
      }
    }

    const _drawListOpaque = [];
    const _drawListAlpha = [];
    const _drawListAdditive = [];
    const _drawListResult = { opaque: _drawListOpaque, alpha: _drawListAlpha, additive: _drawListAdditive };

    function buildPBRDrawList(bundle) {
      const objects = Array.isArray(bundle && bundle.meshObjects) ? bundle.meshObjects : [];
      const materials = Array.isArray(bundle.materials) ? bundle.materials : [];
      _drawListOpaque.length = 0;
      _drawListAlpha.length = 0;
      _drawListAdditive.length = 0;

      for (var i = 0; i < objects.length; i++) {
        const obj = objects[i];
        if (!obj || obj.viewCulled) {
          continue;
        }
        if (!Number.isFinite(obj.vertexOffset) || !Number.isFinite(obj.vertexCount) || obj.vertexCount <= 0) {
          continue;
        }
        const mat = materials[obj.materialIndex] || null;
        const pass = scenePBRObjectRenderPass(obj, mat);
        if (pass === "alpha") {
          _drawListAlpha.push(obj);
        } else if (pass === "additive") {
          _drawListAdditive.push(obj);
        } else {
          _drawListOpaque.push(obj);
        }
      }

      _drawListAlpha.sort(scenePBRDepthSort);
      _drawListAdditive.sort(scenePBRDepthSort);

      return _drawListResult;
    }

    function render(bundle, viewport) {
      if (!bundle) {
        return;
      }

      var perfEnabled = typeof window !== "undefined" && window.__gosx_scene3d_perf === true;
      if (perfEnabled) {
        performance.mark("scene3d-render-start");
      }

      const hasPBRData = Boolean(
        bundle.worldMeshPositions &&
        bundle.worldMeshNormals &&
        Array.isArray(bundle.meshObjects) &&
        bundle.meshObjects.length > 0
      );
      const hasPointsData = (Array.isArray(bundle.points) && bundle.points.length > 0) ||
        (Array.isArray(bundle.computeParticles) && bundle.computeParticles.length > 0);
      const hasInstancedData = Array.isArray(bundle.instancedMeshes) && bundle.instancedMeshes.length > 0;
      if (!hasPBRData && !hasPointsData && !hasInstancedData) {
        return;
      }

      shadowLightMatrices[0] = null; shadowLightMatrices[1] = null;
      shadowLightIndices[0] = -1; shadowLightIndices[1] = -1;
      var activeShadowCount = 0;

      if (shadowProgram) {
        var lightArray = Array.isArray(bundle.lights) ? bundle.lights : [];
        var sceneBounds = null;
        var shadowMaxPixels = (typeof bundle.shadowMaxPixels === "number") ? bundle.shadowMaxPixels : 0;

        for (var li = 0; li < lightArray.length && activeShadowCount < 2; li++) {
          var light = lightArray[li];
          if (!light || !light.castShadow) continue;
          var kind = typeof light.kind === "string" ? light.kind.toLowerCase() : "";
          if (kind !== "directional") continue;

          if (!sceneBounds) {
            sceneBounds = sceneShadowComputeBounds(bundle);
          }

          var slot = activeShadowCount;
          var shadowSize = sceneNumber(light.shadowSize, 1024);
          shadowSize = Math.max(256, Math.min(4096, shadowSize));
          shadowSize = resolveShadowSize(shadowSize, shadowMaxPixels);

          if (!shadowSlots[slot] || shadowSlots[slot].size !== shadowSize) {
            if (shadowSlots[slot]) {
              gl.deleteFramebuffer(shadowSlots[slot].framebuffer);
              gl.deleteTexture(shadowSlots[slot].depthTexture);
            }
            shadowSlots[slot] = createSceneShadowResources(gl, shadowSize);
          }

          var lightMatrix = sceneShadowLightSpaceMatrix(light, sceneBounds);
          shadowLightMatrices[slot] = lightMatrix;
          shadowLightIndices[slot] = li;

          renderSceneShadowPass(gl, shadowProgram, shadowSlots[slot], lightMatrix, bundle, shadowState);
          activeShadowCount++;
        }
      }

      var postEffects = Array.isArray(bundle.postEffects) ? bundle.postEffects : [];
      var postFXMaxPixels = (typeof bundle.postFXMaxPixels === "number") ? bundle.postFXMaxPixels : 0;
      var usePostProcessing = postEffects.length > 0;

      var renderW = canvas.width;
      var renderH = canvas.height;

      if (usePostProcessing) {
        if (!postProcessor) {
          postProcessor = createScenePostProcessor(gl);
        }
        var scaled = postProcessor.begin(canvas.width, canvas.height, postFXMaxPixels);
        renderW = scaled.width;
        renderH = scaled.height;
      }

      gl.viewport(0, 0, renderW, renderH);

      var bgStr = typeof bundle.background === "string" ? bundle.background.trim().toLowerCase() : "";
      const bg = bgStr === "transparent" ? [0, 0, 0, 0] : sceneColorRGBA(bundle.background, [0.03, 0.08, 0.12, 1]);
      gl.clearColor(bg[0], bg[1], bg[2], bg[3]);
      gl.clearDepth(1);
      gl.clear(gl.COLOR_BUFFER_BIT | gl.DEPTH_BUFFER_BIT);

      sceneRenderCamera(bundle.camera, _frameCam);
      const cam = _frameCam;
      const aspect = Math.max(0.0001, canvas.width / Math.max(1, canvas.height));
      const viewMatrix = scenePBRViewMatrix(cam, scratchViewMatrix);
      const projMatrix = scenePBRProjectionMatrix(cam.fov, aspect, cam.near, cam.far, scratchProjMatrix);

      _frameLightsHash = scenePBRLightsHash(bundle.lights, bundle.environment);

      if (hasPBRData) {
      gl.useProgram(program);
      gl.uniformMatrix4fv(uniforms.viewMatrix, false, viewMatrix);
      gl.uniformMatrix4fv(uniforms.projectionMatrix, false, projMatrix);
      gl.uniform3f(uniforms.cameraPosition, cam.x, cam.y, -cam.z);

      scenePBRUploadExposure(gl, uniforms, bundle.environment, usePostProcessing);

      scenePBRUploadLights(gl, uniforms, bundle.lights, bundle.environment, _frameLightsHash);

      scenePBRUploadShadowUniforms(gl, uniforms, shadowSlots, shadowLightMatrices, shadowLightIndices, bundle.lights);

      const drawList = buildPBRDrawList(bundle);
      const materials = Array.isArray(bundle.materials) ? bundle.materials : [];

      applyBlendMode(gl, "opaque");
      applyDepthMode(gl, "opaque");
      drawPBRObjectList(gl, drawList.opaque, bundle, materials);

      if (drawList.alpha.length > 0) {
        applyBlendMode(gl, "alpha");
        applyDepthMode(gl, "alpha");
        drawPBRObjectList(gl, drawList.alpha, bundle, materials);
      }

      if (drawList.additive.length > 0) {
        applyBlendMode(gl, "additive");
        applyDepthMode(gl, "additive");
        drawPBRObjectList(gl, drawList.additive, bundle, materials);
      }

      } // end if (hasPBRData)

      drawInstancedMeshes(gl, bundle, viewMatrix, projMatrix);

      var frameTimeSeconds = performance.now() / 1000;
      drawPointsEntries(gl, Array.isArray(bundle.points) ? bundle.points : [], bundle.environment, viewMatrix, projMatrix, frameTimeSeconds, renderH);
      drawPointsEntries(gl, buildComputePointsEntries(bundle.computeParticles, frameTimeSeconds), bundle.environment, viewMatrix, projMatrix, frameTimeSeconds, renderH);

      gl.depthMask(true);
      gl.disable(gl.BLEND);

      if (usePostProcessing && postProcessor) {
        postProcessor.apply(postEffects, renderW, renderH, canvas.width, canvas.height);
        gl.useProgram(program);
      }

      if (perfEnabled) {
        performance.mark("scene3d-render-end");
        performance.measure("scene3d-render", "scene3d-render-start", "scene3d-render-end");
        performance.clearMarks("scene3d-render-start");
        performance.clearMarks("scene3d-render-end");
      }
    }

    function objectIsSkinned(obj) {
      return Boolean(
        obj && obj.skin &&
        obj.vertices && obj.vertices.joints && obj.vertices.weights
      );
    }

    function ensureSkinnedProgram() {
      if (skinnedProgram) return skinnedProgram;
      skinnedProgram = createScenePBRSkinnedProgram(gl);
      if (!skinnedProgram) {
        console.warn("[gosx] Skinned PBR shader compilation failed; skinned objects will use static path.");
      }
      return skinnedProgram;
    }

    function drawPBRObjectList(gl, objectList, bundle, materials) {
      var lastMaterialIndex = -1;
      var currentProgram = program;       // the static PBR gl program
      var currentAttribs = attribs;
      var currentUniforms = uniforms;

      for (var i = 0; i < objectList.length; i++) {
        const obj = objectList[i];
        const matIndex = sceneNumber(obj.materialIndex, 0);
        const mat = materials[matIndex] || null;
        var isSkinned = objectIsSkinned(obj);

        if (isSkinned) {
          var sp = ensureSkinnedProgram();
          if (sp && currentProgram !== sp.program) {
            gl.useProgram(sp.program);
            currentProgram = sp.program;
            currentAttribs = sp.attributes;
            currentUniforms = sp.uniforms;

            gl.uniformMatrix4fv(currentUniforms.viewMatrix, false, scratchViewMatrix);
            gl.uniformMatrix4fv(currentUniforms.projectionMatrix, false, scratchProjMatrix);
            gl.uniform3f(currentUniforms.cameraPosition, _frameCam.x, _frameCam.y, -_frameCam.z);

            var postEffects = Array.isArray(bundle.postEffects) ? bundle.postEffects : [];
            scenePBRUploadExposure(gl, currentUniforms, bundle.environment, postEffects.length > 0);

            scenePBRUploadLights(gl, currentUniforms, bundle.lights, bundle.environment, _frameLightsHash);

            scenePBRUploadShadowUniforms(gl, currentUniforms, shadowSlots, shadowLightMatrices, shadowLightIndices, bundle.lights);

            lastMaterialIndex = -1;
          }
          if (!sp) isSkinned = false;
        } else if (currentProgram !== program) {
          gl.useProgram(program);
          currentProgram = program;
          currentAttribs = attribs;
          currentUniforms = uniforms;
          lastMaterialIndex = -1;
        }

        if (matIndex !== lastMaterialIndex) {
          uploadMaterial(gl, currentUniforms, mat, textureCache);
          lastMaterialIndex = matIndex;
        }

        gl.uniform1i(currentUniforms.receiveShadow, obj.receiveShadow ? 1 : 0);

        var objDepthWriteOverride = obj.depthWrite !== undefined && obj.depthWrite !== null;
        if (objDepthWriteOverride) {
          gl.depthMask(obj.depthWrite !== false);
        }

        if (isSkinned) {
          gl.uniform1i(currentUniforms.hasSkin, 1);

          var jointMatrices = obj.skin.jointMatrices;
          if (jointMatrices) {
            var jointCount = Math.min(Math.floor(jointMatrices.length / 16), 64);
            for (var ji = 0; ji < jointCount; ji++) {
              gl.uniformMatrix4fv(
                currentUniforms.jointMatrices[ji], false,
                jointMatrices.subarray(ji * 16, ji * 16 + 16)
              );
            }
          }
        } else if (currentUniforms.hasSkin) {
          gl.uniform1i(currentUniforms.hasSkin, 0);
        }

        const offset = obj.vertexOffset;
        const count = obj.vertexCount;

        const positions = sliceToFloat32(bundle.worldMeshPositions, offset, count, 3, "positions");
        gl.bindBuffer(gl.ARRAY_BUFFER, positionBuffer);
        gl.bufferData(gl.ARRAY_BUFFER, positions, gl.DYNAMIC_DRAW);
        gl.enableVertexAttribArray(currentAttribs.position);
        gl.vertexAttribPointer(currentAttribs.position, 3, gl.FLOAT, false, 0, 0);

        const normals = sliceToFloat32(bundle.worldMeshNormals, offset, count, 3, "normals");
        gl.bindBuffer(gl.ARRAY_BUFFER, normalBuffer);
        gl.bufferData(gl.ARRAY_BUFFER, normals, gl.DYNAMIC_DRAW);
        gl.enableVertexAttribArray(currentAttribs.normal);
        gl.vertexAttribPointer(currentAttribs.normal, 3, gl.FLOAT, false, 0, 0);

        if (bundle.worldMeshUVs) {
          const uvs = sliceToFloat32(bundle.worldMeshUVs, offset, count, 2, "uvs");
          gl.bindBuffer(gl.ARRAY_BUFFER, uvBuffer);
          gl.bufferData(gl.ARRAY_BUFFER, uvs, gl.DYNAMIC_DRAW);
          gl.enableVertexAttribArray(currentAttribs.uv);
          gl.vertexAttribPointer(currentAttribs.uv, 2, gl.FLOAT, false, 0, 0);
        } else if (currentAttribs.uv >= 0) {
          gl.disableVertexAttribArray(currentAttribs.uv);
          gl.vertexAttrib2f(currentAttribs.uv, 0, 0);
        }

        if (bundle.worldMeshTangents) {
          const tangents = sliceToFloat32(bundle.worldMeshTangents, offset, count, 4, "tangents");
          gl.bindBuffer(gl.ARRAY_BUFFER, tangentBuffer);
          gl.bufferData(gl.ARRAY_BUFFER, tangents, gl.DYNAMIC_DRAW);
          gl.enableVertexAttribArray(currentAttribs.tangent);
          gl.vertexAttribPointer(currentAttribs.tangent, 4, gl.FLOAT, false, 0, 0);
        } else if (currentAttribs.tangent >= 0) {
          gl.disableVertexAttribArray(currentAttribs.tangent);
          gl.vertexAttrib4f(currentAttribs.tangent, 1, 0, 0, 1);
        }

        if (isSkinned && currentAttribs.joints >= 0 && currentAttribs.weights >= 0) {
          var joints = obj.vertices.joints;
          var weights = obj.vertices.weights;

          gl.bindBuffer(gl.ARRAY_BUFFER, jointsBuffer);
          gl.bufferData(gl.ARRAY_BUFFER, joints instanceof Float32Array ? joints : new Float32Array(joints), gl.DYNAMIC_DRAW);
          gl.enableVertexAttribArray(currentAttribs.joints);
          gl.vertexAttribPointer(currentAttribs.joints, 4, gl.FLOAT, false, 0, 0);

          gl.bindBuffer(gl.ARRAY_BUFFER, weightsBuffer);
          gl.bufferData(gl.ARRAY_BUFFER, weights instanceof Float32Array ? weights : new Float32Array(weights), gl.DYNAMIC_DRAW);
          gl.enableVertexAttribArray(currentAttribs.weights);
          gl.vertexAttribPointer(currentAttribs.weights, 4, gl.FLOAT, false, 0, 0);
        } else if (currentAttribs.joints >= 0) {
          gl.disableVertexAttribArray(currentAttribs.joints);
          gl.vertexAttrib4f(currentAttribs.joints, 0, 0, 0, 0);
          gl.disableVertexAttribArray(currentAttribs.weights);
          gl.vertexAttrib4f(currentAttribs.weights, 0, 0, 0, 0);
        }

        gl.drawArrays(gl.TRIANGLES, 0, count);

        if (objDepthWriteOverride) {
          var mat2 = materials[sceneNumber(obj.materialIndex, 0)] || null;
          var pass2 = scenePBRObjectRenderPass(obj, mat2);
          gl.depthMask(pass2 === "opaque");
        }
      }

      if (currentProgram !== program) {
        gl.useProgram(program);
      }
    }

    function ensurePointsProgram() {
      if (pointsProgram) return pointsProgram;
      pointsProgram = createScenePointsProgram(gl);
      if (!pointsProgram) {
        console.warn("[gosx] Points shader compilation failed; points will not render.");
      }
      return pointsProgram;
    }

    function disposeComputeParticleSystemRecord(record) {
      if (record && record.system && typeof record.system.dispose === "function") {
        record.system.dispose();
      }
    }

    function syncComputeParticleSystems(entries) {
      var activeIds = new Set();
      var records = [];
      var sourceEntries = Array.isArray(entries) ? entries : [];
      for (var i = 0; i < sourceEntries.length; i++) {
        var entry = sourceEntries[i];
        if (!entry || typeof entry !== "object") continue;
        var id = typeof entry.id === "string" && entry.id ? entry.id : ("scene-particles-" + i);
        var signature = sceneComputeSystemSignature(entry);
        activeIds.add(id);
        var record = computeParticleSystems.get(id);
        if (!record || record.signature !== signature) {
          disposeComputeParticleSystemRecord(record);
          record = {
            signature: signature,
            system: createSceneParticleSystem(null, entry),
            colorBuffer: null,
          };
          computeParticleSystems.set(id, record);
        } else if (record.system) {
          record.system.entry = entry;
        }
        if (record && record.system) {
          records.push(record);
        }
      }
      for (const [id, record] of computeParticleSystems.entries()) {
        if (!activeIds.has(id)) {
          disposeComputeParticleSystemRecord(record);
          computeParticleSystems.delete(id);
        }
      }
      return records;
    }

    function buildComputePointsEntries(entries, timeSeconds) {
      var currentTime = Number.isFinite(timeSeconds) ? timeSeconds : 0;
      var deltaTime = lastComputeParticleTimeSeconds == null
        ? 0
        : Math.max(0, Math.min(0.1, currentTime - lastComputeParticleTimeSeconds));
      lastComputeParticleTimeSeconds = currentTime;
      var records = syncComputeParticleSystems(entries);
      var pointsEntries = [];
      for (var i = 0; i < records.length; i++) {
        var record = records[i];
        var system = record.system;
        if (!system) continue;
        system.update(deltaTime, currentTime);
        if (!record.colorBuffer || record.colorBuffer.length !== system.count * 4) {
          record.colorBuffer = new Float32Array(system.count * 4);
        }
        for (var pi = 0; pi < system.count; pi++) {
          var rgbBase = pi * 3;
          var rgbaBase = pi * 4;
          record.colorBuffer[rgbaBase] = system.colors[rgbBase];
          record.colorBuffer[rgbaBase + 1] = system.colors[rgbBase + 1];
          record.colorBuffer[rgbaBase + 2] = system.colors[rgbBase + 2];
          record.colorBuffer[rgbaBase + 3] = system.opacities[pi];
        }
        var material = system.entry && system.entry.material && typeof system.entry.material === "object"
          ? system.entry.material
          : {};
        var emitter = system.entry && system.entry.emitter && typeof system.entry.emitter === "object"
          ? system.entry.emitter
          : {};
        pointsEntries.push({
          id: system.entry && system.entry.id ? system.entry.id : ("scene-compute-points-" + i),
          count: system.count,
          color: typeof material.color === "string" ? material.color : "#ffffff",
          style: material.style,
          size: sceneNumber(material.size, 1),
          opacity: 1,
          blendMode: material.blendMode,
          attenuation: !!material.attenuation,
          x: sceneNumber(emitter.x, 0),
          y: sceneNumber(emitter.y, 0),
          z: sceneNumber(emitter.z, 0),
          rotationX: sceneNumber(emitter.rotationX, 0),
          rotationY: sceneNumber(emitter.rotationY, 0),
          rotationZ: sceneNumber(emitter.rotationZ, 0),
          spinX: sceneNumber(emitter.spinX, 0),
          spinY: sceneNumber(emitter.spinY, 0),
          spinZ: sceneNumber(emitter.spinZ, 0),
          _cachedPos: system.positions,
          _cachedSizes: system.sizes,
          _cachedColors: record.colorBuffer,
        });
      }
      return pointsEntries;
    }

    function drawPointsEntries(gl, pointsArray, environment, viewMatrix, projMatrix, timeSeconds, renderH) {
      if (pointsArray.length === 0) return;

      var pp = ensurePointsProgram();
      if (!pp) return;

      gl.useProgram(pp.program);

      gl.uniformMatrix4fv(pp.uniforms.viewMatrix, false, viewMatrix);
      gl.uniformMatrix4fv(pp.uniforms.projectionMatrix, false, projMatrix);
      gl.uniform1f(pp.uniforms.viewportHeight, renderH);

      var env = environment || {};
      var fogDensity = sceneNumber(env.fogDensity, 0);
      gl.uniform1i(pp.uniforms.hasFog, fogDensity > 0 ? 1 : 0);
      gl.uniform1f(pp.uniforms.fogDensity, fogDensity);
      var fogColorRGBA = sceneColorRGBA(env.fogColor, [0.5, 0.5, 0.5, 1]);
      gl.uniform3f(pp.uniforms.fogColor, fogColorRGBA[0], fogColorRGBA[1], fogColorRGBA[2]);

      gl.enable(gl.DEPTH_TEST);
      var _pointsModelMat = new Float32Array(16);
      var _pointsTilt = new Float32Array(16);
      var _pointsSpin = new Float32Array(16);

      for (var i = 0; i < pointsArray.length; i++) {
        var entry = pointsArray[i];

        var px = sceneNumber(entry.x, 0);
        var py = sceneNumber(entry.y, 0);
        var pz = sceneNumber(entry.z, 0);
        var hasSpin = sceneNumber(entry.spinX, 0) !== 0 || sceneNumber(entry.spinY, 0) !== 0 || sceneNumber(entry.spinZ, 0) !== 0;

        if (hasSpin) {

          var spx = sceneNumber(entry.spinX, 0) * timeSeconds;
          var spy = sceneNumber(entry.spinY, 0) * timeSeconds;
          var spz = sceneNumber(entry.spinZ, 0) * timeSeconds;
          var csx = Math.cos(spx), ssx = Math.sin(spx);
          var csy = Math.cos(spy), ssy = Math.sin(spy);
          var csz = Math.cos(spz), ssz = Math.sin(spz);
          _pointsSpin[0] = csy*csz; _pointsSpin[4] = ssx*ssy*csz-csx*ssz; _pointsSpin[8]  = csx*ssy*csz+ssx*ssz; _pointsSpin[12] = 0;
          _pointsSpin[1] = csy*ssz; _pointsSpin[5] = ssx*ssy*ssz+csx*csz; _pointsSpin[9]  = csx*ssy*ssz-ssx*csz; _pointsSpin[13] = 0;
          _pointsSpin[2] = -ssy;    _pointsSpin[6] = ssx*csy;             _pointsSpin[10] = csx*csy;             _pointsSpin[14] = 0;
          _pointsSpin[3] = 0;       _pointsSpin[7] = 0;                   _pointsSpin[11] = 0;                   _pointsSpin[15] = 1;

          var rx = sceneNumber(entry.rotationX, 0);
          var ry = sceneNumber(entry.rotationY, 0);
          var rz = sceneNumber(entry.rotationZ, 0);
          var cxr = Math.cos(rx), sxr = Math.sin(rx);
          var cyr = Math.cos(ry), syr = Math.sin(ry);
          var czr = Math.cos(rz), szr = Math.sin(rz);
          _pointsTilt[0] = cyr*czr; _pointsTilt[4] = sxr*syr*czr-cxr*szr; _pointsTilt[8]  = cxr*syr*czr+sxr*szr; _pointsTilt[12] = px;
          _pointsTilt[1] = cyr*szr; _pointsTilt[5] = sxr*syr*szr+cxr*czr; _pointsTilt[9]  = cxr*syr*szr-sxr*czr; _pointsTilt[13] = py;
          _pointsTilt[2] = -syr;    _pointsTilt[6] = sxr*cyr;             _pointsTilt[10] = cxr*cyr;             _pointsTilt[14] = pz;
          _pointsTilt[3] = 0;       _pointsTilt[7] = 0;                   _pointsTilt[11] = 0;                   _pointsTilt[15] = 1;

          sceneMat4MultiplyInto(_pointsModelMat, _pointsTilt, _pointsSpin);
        } else {
          var rx = sceneNumber(entry.rotationX, 0);
          var ry = sceneNumber(entry.rotationY, 0);
          var rz = sceneNumber(entry.rotationZ, 0);
          var cxr = Math.cos(rx), sxr = Math.sin(rx);
          var cyr = Math.cos(ry), syr = Math.sin(ry);
          var czr = Math.cos(rz), szr = Math.sin(rz);
          _pointsModelMat[0] = cyr*czr; _pointsModelMat[4] = sxr*syr*czr-cxr*szr; _pointsModelMat[8]  = cxr*syr*czr+sxr*szr; _pointsModelMat[12] = px;
          _pointsModelMat[1] = cyr*szr; _pointsModelMat[5] = sxr*syr*szr+cxr*czr; _pointsModelMat[9]  = cxr*syr*szr-sxr*czr; _pointsModelMat[13] = py;
          _pointsModelMat[2] = -syr;    _pointsModelMat[6] = sxr*cyr;             _pointsModelMat[10] = cxr*cyr;             _pointsModelMat[14] = pz;
          _pointsModelMat[3] = 0;       _pointsModelMat[7] = 0;                   _pointsModelMat[11] = 0;                   _pointsModelMat[15] = 1;
        }
        gl.uniformMatrix4fv(pp.uniforms.modelMatrix, false, _pointsModelMat);
        var count = sceneNumber(entry.count, 0);
        if (count <= 0) continue;

        var blendMode = typeof entry.blendMode === "string" ? entry.blendMode.toLowerCase() : "";
        if (blendMode === "additive") {
          gl.enable(gl.BLEND);
          gl.blendFunc(gl.SRC_ALPHA, gl.ONE);
        } else if (blendMode === "alpha") {
          gl.enable(gl.BLEND);
          gl.blendFunc(gl.SRC_ALPHA, gl.ONE_MINUS_SRC_ALPHA);
        } else {
          gl.disable(gl.BLEND);
        }

        var depthWrite = entry.depthWrite !== false;
        gl.depthMask(depthWrite);
        gl.depthFunc(gl.LEQUAL);

        gl.uniform1f(pp.uniforms.opacity, clamp01(sceneNumber(entry.opacity, 1)));

        var defaultColorRGBA = sceneColorRGBA(entry.color, [1, 1, 1, 1]);
        gl.uniform4f(pp.uniforms.defaultColor, defaultColorRGBA[0], defaultColorRGBA[1], defaultColorRGBA[2], 1);
        gl.uniform1f(pp.uniforms.defaultSize, sceneNumber(entry.size, 1));
        gl.uniform1i(pp.uniforms.sizeAttenuation, entry.attenuation ? 1 : 0);
        gl.uniform1i(pp.uniforms.pointStyle, scenePointStyleCode(entry.style));

        if (!entry._cachedPos && Array.isArray(entry.positions) && entry.positions.length >= count * 3) {
          entry._cachedPos = new Float32Array(entry.positions);
        }
        if (!entry._cachedSizes && Array.isArray(entry.sizes) && entry.sizes.length >= count) {
          entry._cachedSizes = new Float32Array(entry.sizes);
        }
        if (!entry._cachedColors && Array.isArray(entry.colors) && entry.colors.length >= count) {
          var rawColors = entry.colors;
          if (typeof rawColors[0] === "string") {
            entry._cachedColors = new Float32Array(count * 4);
            for (var ci = 0; ci < count; ci++) {
              var crgba = sceneColorRGBA(rawColors[ci], [1, 1, 1, 1]);
              entry._cachedColors[ci * 4] = crgba[0];
              entry._cachedColors[ci * 4 + 1] = crgba[1];
              entry._cachedColors[ci * 4 + 2] = crgba[2];
              entry._cachedColors[ci * 4 + 3] = crgba[3];
            }
          } else if (rawColors.length >= count * 4) {
            entry._cachedColors = new Float32Array(rawColors);
          } else if (rawColors.length >= count * 3) {
            entry._cachedColors = new Float32Array(count * 4);
            for (var ci2 = 0; ci2 < count; ci2++) {
              entry._cachedColors[ci2 * 4] = rawColors[ci2 * 3];
              entry._cachedColors[ci2 * 4 + 1] = rawColors[ci2 * 3 + 1];
              entry._cachedColors[ci2 * 4 + 2] = rawColors[ci2 * 3 + 2];
              entry._cachedColors[ci2 * 4 + 3] = 1;
            }
          }
        }
        if (!entry._cachedPos) continue;
        gl.bindBuffer(gl.ARRAY_BUFFER, pointsPositionBuffer);
        gl.bufferData(gl.ARRAY_BUFFER, entry._cachedPos, gl.STREAM_DRAW);
        gl.enableVertexAttribArray(pp.attributes.position);
        gl.vertexAttribPointer(pp.attributes.position, 3, gl.FLOAT, false, 0, 0);

        var hasSizes = !!entry._cachedSizes;
        gl.uniform1i(pp.uniforms.hasPerVertexSize, hasSizes ? 1 : 0);
        if (hasSizes && pp.attributes.size >= 0) {
          gl.bindBuffer(gl.ARRAY_BUFFER, pointsSizeBuffer);
          gl.bufferData(gl.ARRAY_BUFFER, entry._cachedSizes, gl.STREAM_DRAW);
          gl.enableVertexAttribArray(pp.attributes.size);
          gl.vertexAttribPointer(pp.attributes.size, 1, gl.FLOAT, false, 0, 0);
        } else if (pp.attributes.size >= 0) {
          gl.disableVertexAttribArray(pp.attributes.size);
          gl.vertexAttrib1f(pp.attributes.size, sceneNumber(entry.size, 1));
        }

        var hasColors = !!entry._cachedColors;
        gl.uniform1i(pp.uniforms.hasPerVertexColor, hasColors ? 1 : 0);
        if (hasColors && pp.attributes.color >= 0) {
          gl.bindBuffer(gl.ARRAY_BUFFER, pointsColorBuffer);
          gl.bufferData(gl.ARRAY_BUFFER, entry._cachedColors, gl.STREAM_DRAW);
          gl.enableVertexAttribArray(pp.attributes.color);
          gl.vertexAttribPointer(pp.attributes.color, 4, gl.FLOAT, false, 0, 0);
        } else if (pp.attributes.color >= 0) {
          gl.disableVertexAttribArray(pp.attributes.color);
          gl.vertexAttrib4f(pp.attributes.color, defaultColorRGBA[0], defaultColorRGBA[1], defaultColorRGBA[2], 1);
        }

        gl.drawArrays(gl.POINTS, 0, count);
      }

      gl.depthMask(true);
      gl.disable(gl.BLEND);

      gl.useProgram(program);
    }

    function ensureInstancedProgram() {
      if (instancedProgram) return instancedProgram;
      instancedProgram = createScenePBRInstancedProgram(gl);
      if (!instancedProgram) {
        console.warn("[gosx] Instanced PBR shader compilation failed; instanced meshes will not render.");
      }
      return instancedProgram;
    }

    function getInstancedGeometry(mesh) {
      var kind = typeof mesh.kind === "string" ? mesh.kind.toLowerCase() : "box";
      var w = sceneNumber(mesh.width, 1);
      var h = sceneNumber(mesh.height, 1);
      var d = sceneNumber(mesh.depth, 1);
      var r = sceneNumber(mesh.radius, 0.5);
      var s = sceneNumber(mesh.segments, 16);
      var key = kind + ":" + w + ":" + h + ":" + d + ":" + r + ":" + s;
      if (instancedGeometryCache[key]) return instancedGeometryCache[key];
      var geom = generateInstancedGeometry(kind, { width: w, height: h, depth: d, radius: r, segments: s });
      instancedGeometryCache[key] = geom;
      return geom;
    }

    function drawInstancedMeshes(gl, bundle, viewMatrix, projMatrix) {
      var meshes = Array.isArray(bundle.instancedMeshes) ? bundle.instancedMeshes : [];
      if (meshes.length === 0) return;

      var ip = ensureInstancedProgram();
      if (!ip) return;

      gl.useProgram(ip.program);

      gl.enable(gl.DEPTH_TEST);
      gl.depthMask(true);
      gl.depthFunc(gl.LEQUAL);
      gl.disable(gl.BLEND);

      gl.uniformMatrix4fv(ip.uniforms.viewMatrix, false, viewMatrix);
      gl.uniformMatrix4fv(ip.uniforms.projectionMatrix, false, projMatrix);
      gl.uniform3f(ip.uniforms.cameraPosition, _frameCam.x, _frameCam.y, -_frameCam.z);

      var postEffects = Array.isArray(bundle.postEffects) ? bundle.postEffects : [];
      scenePBRUploadExposure(gl, ip.uniforms, bundle.environment, postEffects.length > 0);

      scenePBRUploadLights(gl, ip.uniforms, bundle.lights, bundle.environment, _frameLightsHash);
      scenePBRUploadShadowUniforms(gl, ip.uniforms, shadowSlots, shadowLightMatrices, shadowLightIndices, bundle.lights);

      var materials = Array.isArray(bundle.materials) ? bundle.materials : [];

      for (var i = 0; i < meshes.length; i++) {
        var mesh = meshes[i];
        if (!mesh.transforms || mesh.instanceCount <= 0) continue;

        var instanceCount = sceneNumber(mesh.instanceCount, 0);
        if (instanceCount <= 0) continue;

        var geom = getInstancedGeometry(mesh);
        if (!geom || geom.vertexCount <= 0) continue;

        var mat = materials[sceneNumber(mesh.materialIndex, 0)] || null;
        if (!mat && mesh.color) {
          mat = {
            color: mesh.color,
            roughness: sceneNumber(mesh.roughness, 0.5),
            metalness: sceneNumber(mesh.metalness, 0),
          };
        }
        uploadMaterial(gl, ip.uniforms, mat, textureCache);

        gl.uniform1i(ip.uniforms.receiveShadow, mesh.receiveShadow ? 1 : 0);

        gl.bindBuffer(gl.ARRAY_BUFFER, instanceVertexBuffer);
        gl.bufferData(gl.ARRAY_BUFFER, geom.positions, gl.STATIC_DRAW);
        gl.enableVertexAttribArray(ip.attributes.position);
        gl.vertexAttribPointer(ip.attributes.position, 3, gl.FLOAT, false, 0, 0);

        gl.bindBuffer(gl.ARRAY_BUFFER, instanceNormalBuffer);
        gl.bufferData(gl.ARRAY_BUFFER, geom.normals, gl.STATIC_DRAW);
        gl.enableVertexAttribArray(ip.attributes.normal);
        gl.vertexAttribPointer(ip.attributes.normal, 3, gl.FLOAT, false, 0, 0);

        gl.bindBuffer(gl.ARRAY_BUFFER, instanceUVBuffer);
        gl.bufferData(gl.ARRAY_BUFFER, geom.uvs, gl.STATIC_DRAW);
        gl.enableVertexAttribArray(ip.attributes.uv);
        gl.vertexAttribPointer(ip.attributes.uv, 2, gl.FLOAT, false, 0, 0);

        gl.bindBuffer(gl.ARRAY_BUFFER, instanceTangentBuffer);
        gl.bufferData(gl.ARRAY_BUFFER, geom.tangents, gl.STATIC_DRAW);
        gl.enableVertexAttribArray(ip.attributes.tangent);
        gl.vertexAttribPointer(ip.attributes.tangent, 4, gl.FLOAT, false, 0, 0);

        if (!mesh._cachedTransforms) {
          if (mesh.transforms instanceof Float32Array) {
            mesh._cachedTransforms = mesh.transforms;
          } else if (Array.isArray(mesh.transforms)) {
            mesh._cachedTransforms = new Float32Array(mesh.transforms);
          }
        }
        var transformData = mesh._cachedTransforms;
        if (!transformData) continue;

        gl.bindBuffer(gl.ARRAY_BUFFER, instanceTransformBuffer);
        gl.bufferData(gl.ARRAY_BUFFER, transformData, gl.STATIC_DRAW);

        var baseLoc = ip.attributes.instanceMatrix;
        for (var col = 0; col < 4; col++) {
          var loc = baseLoc + col;
          gl.enableVertexAttribArray(loc);
          gl.vertexAttribPointer(loc, 4, gl.FLOAT, false, 64, col * 16);
          gl.vertexAttribDivisor(loc, 1);
        }

        gl.drawArraysInstanced(gl.TRIANGLES, 0, geom.vertexCount, instanceCount);

        for (var col = 0; col < 4; col++) {
          var loc2 = baseLoc + col;
          gl.vertexAttribDivisor(loc2, 0);
          gl.disableVertexAttribArray(loc2);
        }
      }

      gl.useProgram(program);
    }

    function dispose() {
      gl.deleteBuffer(positionBuffer);
      gl.deleteBuffer(normalBuffer);
      gl.deleteBuffer(uvBuffer);
      gl.deleteBuffer(tangentBuffer);
      gl.deleteBuffer(jointsBuffer);
      gl.deleteBuffer(weightsBuffer);
      gl.deleteBuffer(pointsPositionBuffer);
      gl.deleteBuffer(pointsSizeBuffer);
      gl.deleteBuffer(pointsColorBuffer);
      for (const record of computeParticleSystems.values()) {
        disposeComputeParticleSystemRecord(record);
      }
      computeParticleSystems.clear();
      lastComputeParticleTimeSeconds = null;
      gl.deleteBuffer(instanceTransformBuffer);
      gl.deleteBuffer(instanceVertexBuffer);
      gl.deleteBuffer(instanceNormalBuffer);
      gl.deleteBuffer(instanceUVBuffer);
      gl.deleteBuffer(instanceTangentBuffer);
      if (shadowState.buffer) gl.deleteBuffer(shadowState.buffer);

      for (const record of textureCache.values()) {
        if (record && record.texture) {
          gl.deleteTexture(record.texture);
        }
      }
      textureCache.clear();

      for (var si = 0; si < shadowSlots.length; si++) {
        if (shadowSlots[si]) {
          gl.deleteFramebuffer(shadowSlots[si].framebuffer);
          gl.deleteTexture(shadowSlots[si].depthTexture);
          shadowSlots[si] = null;
        }
      }

      if (postProcessor) {
        postProcessor.dispose();
        postProcessor = null;
      }

      if (shadowProgram) {
        gl.deleteShader(shadowProgram.vertexShader);
        gl.deleteShader(shadowProgram.fragmentShader);
        gl.deleteProgram(shadowProgram.program);
      }

      if (skinnedProgram) {
        gl.deleteShader(skinnedProgram.vertexShader);
        gl.deleteShader(skinnedProgram.fragmentShader);
        gl.deleteProgram(skinnedProgram.program);
        skinnedProgram = null;
      }

      if (pointsProgram) {
        gl.deleteShader(pointsProgram.vertexShader);
        gl.deleteShader(pointsProgram.fragmentShader);
        gl.deleteProgram(pointsProgram.program);
        pointsProgram = null;
      }

      if (instancedProgram) {
        gl.deleteShader(instancedProgram.vertexShader);
        gl.deleteShader(instancedProgram.fragmentShader);
        gl.deleteProgram(instancedProgram.program);
        instancedProgram = null;
      }
      instancedGeometryCache = {};

      gl.deleteShader(pbrProgram.vertexShader);
      gl.deleteShader(pbrProgram.fragmentShader);
      gl.deleteProgram(program);
    }

    return {
      render: render,
      dispose: dispose,
      type: "webgl-pbr",
    };
  }

  function scenePBRObjectRenderPass(obj, material) {
    if (obj && typeof obj.renderPass === "string" && obj.renderPass) {
      const pass = obj.renderPass.toLowerCase();
      if (pass === "alpha" || pass === "additive" || pass === "opaque") {
        return pass;
      }
    }
    if (material && typeof material.renderPass === "string" && material.renderPass) {
      const pass = material.renderPass.toLowerCase();
      if (pass === "alpha" || pass === "additive" || pass === "opaque") {
        return pass;
      }
    }
    if (material && sceneNumber(material.opacity, 1) < 1) {
      return "alpha";
    }
    return "opaque";
  }

  function scenePBRDepthSort(a, b) {
    const da = sceneNumber(a && a.depthCenter, 0);
    const db = sceneNumber(b && b.depthCenter, 0);
    if (da !== db) {
      return db - da;
    }
    return String(a && a.id || "").localeCompare(String(b && b.id || ""));
  }

  function createScenePBRRendererOrFallback(gl, canvas, options) {
    if (!gl || !canvas) {
      return null;
    }
    if (typeof WebGL2RenderingContext === "undefined" || !(gl instanceof WebGL2RenderingContext)) {
      return null;
    }
    var renderer = null;
    try {
      renderer = createScenePBRRenderer(gl, canvas);
    } catch (e) {
      console.warn("[gosx] PBR renderer creation failed:", e);
      return null;
    }
    return renderer;
  }

  var _webgpuAdapterProbe = null; // null = unprobed, false = unavailable, GPUAdapter = ready
  var _webgpuDeviceProbe = null;  // null = unprobed, false = unavailable, GPUDevice = ready
  var _webgpuAdapterReady = false;

  if (typeof navigator !== "undefined" && navigator.gpu && typeof navigator.gpu.requestAdapter === "function") {
    navigator.gpu.requestAdapter().then(function(adapter) {
      if (!adapter) {
        console.warn("[gosx] WebGPU probe: requestAdapter returned null");
        _webgpuAdapterProbe = false;
        _webgpuDeviceProbe = false;
        return null;
      }
      _webgpuAdapterProbe = adapter;
      return adapter.requestDevice();
    }).then(function(device) {
      if (!device) {
        console.warn("[gosx] WebGPU probe: requestDevice returned null");
        _webgpuDeviceProbe = false;
        return;
      }
      _webgpuDeviceProbe = device;
      _webgpuAdapterReady = true;
      device.lost.then(function(info) {
        console.warn("[gosx] WebGPU probe device lost:", info && info.message);
        _webgpuAdapterReady = false;
        _webgpuDeviceProbe = false;
      }).catch(function() {});
    }).catch(function(err) {
      console.warn("[gosx] WebGPU probe failed:", err && (err.message || err));
      _webgpuAdapterProbe = false;
      _webgpuDeviceProbe = false;
    });
    window.__gosx_scene3d_webgpu_probe = function() {
      return {
        adapter: _webgpuAdapterProbe,
        device: _webgpuDeviceProbe,
        ready: _webgpuAdapterReady,
      };
    };
  } else {
    _webgpuAdapterProbe = false;
    _webgpuDeviceProbe = false;
  }

  function sceneWebGPUAvailable() {
    return _webgpuAdapterReady
      && _webgpuAdapterProbe !== false
      && _webgpuAdapterProbe !== null
      && _webgpuDeviceProbe !== false
      && _webgpuDeviceProbe !== null
      && !!(window.__gosx_scene3d_webgpu_api
        && typeof window.__gosx_scene3d_webgpu_api.createRenderer === "function");
  }

  function createSceneWebGPURendererOrFallback(canvas) {
    if (!sceneWebGPUAvailable()) return null;
    if (!canvas || typeof canvas.getContext !== "function") return null;
    try {
      var renderer = window.__gosx_scene3d_webgpu_api.createRenderer(canvas);
      if (!renderer) {
        console.warn("[gosx] WebGPU factory returned null after probe success; canvas may be tainted");
      }
      return renderer;
    } catch (e) {
      console.warn("[gosx] WebGPU renderer creation failed:", e);
      return null;
    }
  }

  var SCENE_DRAG_MIN_EXTENT_X = 0.6;
  var SCENE_DRAG_MIN_EXTENT_Y = 0.35;
  var SCENE_PICK_MIN_EXTENT_X = 0.12;
  var SCENE_PICK_MIN_EXTENT_Y = 0.08;
  var SCENE_ORBIT_PITCH_MIN = -1.35;
  var SCENE_ORBIT_PITCH_MAX = 1.35;
  var SCENE_ORBIT_YAW_MIN = -1.1;
  var SCENE_ORBIT_YAW_MAX = 1.1;
  var SCENE_POINTER_PAD_MIN = 12;
  var SCENE_POINTER_PAD_RANGE = 22;
  var SCENE_POINTER_PAD_SCALE = 0.08;

  function sceneLocalPointerPoint(event, canvas, width, height) {
    const rect = canvas.getBoundingClientRect();
    const safeWidth = Math.max(rect.width || 0, 1);
    const safeHeight = Math.max(rect.height || 0, 1);
    return {
      x: sceneClamp(((sceneNumber(event && event.clientX, rect.left) - rect.left) / safeWidth) * width, 0, width),
      y: sceneClamp(((sceneNumber(event && event.clientY, rect.top) - rect.top) / safeHeight) * height, 0, height),
    };
  }

  function sceneLocalPointerSample(event, canvas, width, height, state, phase) {
    const previousX = state.lastX == null ? width / 2 : state.lastX;
    const previousY = state.lastY == null ? height / 2 : state.lastY;
    const hasPointerPosition = Number.isFinite(sceneNumber(event && event.clientX, NaN)) && Number.isFinite(sceneNumber(event && event.clientY, NaN));
    const point = hasPointerPosition ? sceneLocalPointerPoint(event, canvas, width, height) : { x: previousX, y: previousY };
    const sample = {
      x: point.x,
      y: point.y,
      deltaX: point.x - previousX,
      deltaY: point.y - previousY,
      buttons: phase === "end" ? 0 : 1,
      button: phase === "start" || phase === "end" ? 0 : null,
      active: phase !== "end",
    };
    state.lastX = point.x;
    state.lastY = point.y;
    return sample;
  }

  function resetScenePointerSample(width, height, state) {
    state.lastX = width / 2;
    state.lastY = height / 2;
    publishPointerSignals({
      x: state.lastX,
      y: state.lastY,
      deltaX: 0,
      deltaY: 0,
      buttons: 0,
      button: 0,
      active: false,
    });
  }

  function sceneDragSignalNamespace(props) {
    const value = props && props.dragSignalNamespace;
    return typeof value === "string" ? value.trim() : "";
  }

  function scenePickSignalNamespace(props) {
    const value = props && props.pickSignalNamespace;
    return typeof value === "string" ? value.trim() : "";
  }

  function sceneEventSignalNamespace(props) {
    const value = props && props.eventSignalNamespace;
    return typeof value === "string" ? value.trim() : "";
  }

  function sceneSignalSegment(value, fallback) {
    const source = typeof value === "string" ? value.trim().toLowerCase() : "";
    if (!source) {
      return fallback;
    }
    const normalized = source
      .replace(/[^a-z0-9]+/g, "-")
      .replace(/^-+|-+$/g, "");
    return normalized || fallback;
  }

  function sceneTargetIndex(target) {
    return target && target.index != null ? Math.max(-1, Math.floor(sceneNumber(target.index, -1))) : -1;
  }

  function sceneTargetID(target) {
    return target && target.object && typeof target.object.id === "string" ? target.object.id : "";
  }

  function sceneTargetKind(target) {
    return target && target.object && typeof target.object.kind === "string" ? target.object.kind : "";
  }

  function sceneObjectSignalSlug(index, id, kind) {
    const targetID = typeof id === "string" ? id.trim() : "";
    if (targetID) {
      return sceneSignalSegment(targetID, "object");
    }
    const targetKind = typeof kind === "string" ? kind.trim() : "";
    if (targetKind && index >= 0) {
      return sceneSignalSegment(targetKind + "-" + index, "object-" + index);
    }
    if (index >= 0) {
      return "object-" + index;
    }
    return "";
  }

  function publishSceneDragSignals(namespace, state, active) {
    if (!namespace) {
      return;
    }
    queueInputSignal(namespace + ".x", sceneNumber(state.orbitX, 0));
    queueInputSignal(namespace + ".y", sceneNumber(state.orbitY, 0));
    queueInputSignal(namespace + ".targetIndex", Math.max(-1, Math.floor(sceneNumber(state.targetIndex, -1))));
    queueInputSignal(namespace + ".active", Boolean(active));
  }

  function publishScenePickSignals(namespace, state) {
    if (!namespace) {
      return;
    }
    const snapshot = scenePickSignalSnapshot(state);
    const nextKey = scenePickSignalSnapshotKey(snapshot);
    if (nextKey === state.publishedKey) {
      return;
    }
    state.publishedKey = nextKey;
    queueInputSignal(namespace + ".hovered", snapshot.hovered);
    queueInputSignal(namespace + ".hoverIndex", snapshot.hoverIndex);
    queueInputSignal(namespace + ".hoverID", snapshot.hoverID);
    queueInputSignal(namespace + ".down", snapshot.down);
    queueInputSignal(namespace + ".downIndex", snapshot.downIndex);
    queueInputSignal(namespace + ".downID", snapshot.downID);
    queueInputSignal(namespace + ".selected", snapshot.selected);
    queueInputSignal(namespace + ".selectedIndex", snapshot.selectedIndex);
    queueInputSignal(namespace + ".selectedID", snapshot.selectedID);
    queueInputSignal(namespace + ".clickCount", snapshot.clickCount);
    queueInputSignal(namespace + ".pointerX", snapshot.pointerX);
    queueInputSignal(namespace + ".pointerY", snapshot.pointerY);
  }

  function publishSceneEventSignals(namespace, state) {
    if (!namespace) {
      return;
    }
    const snapshot = sceneInteractionSnapshot(state);
    const nextKey = sceneInteractionSnapshotKey(snapshot);
    if (nextKey === state.publishedEventKey) {
      return;
    }
    state.publishedEventKey = nextKey;
    queueInputSignal(namespace + ".revision", snapshot.revision);
    queueInputSignal(namespace + ".type", snapshot.type);
    queueInputSignal(namespace + ".targetIndex", snapshot.targetIndex);
    queueInputSignal(namespace + ".targetID", snapshot.targetID);
    queueInputSignal(namespace + ".targetKind", snapshot.targetKind);
    queueInputSignal(namespace + ".hovered", snapshot.hovered);
    queueInputSignal(namespace + ".hoverIndex", snapshot.hoverIndex);
    queueInputSignal(namespace + ".hoverID", snapshot.hoverID);
    queueInputSignal(namespace + ".hoverKind", snapshot.hoverKind);
    queueInputSignal(namespace + ".down", snapshot.down);
    queueInputSignal(namespace + ".downIndex", snapshot.downIndex);
    queueInputSignal(namespace + ".downID", snapshot.downID);
    queueInputSignal(namespace + ".downKind", snapshot.downKind);
    queueInputSignal(namespace + ".selected", snapshot.selected);
    queueInputSignal(namespace + ".selectedIndex", snapshot.selectedIndex);
    queueInputSignal(namespace + ".selectedID", snapshot.selectedID);
    queueInputSignal(namespace + ".selectedKind", snapshot.selectedKind);
    queueInputSignal(namespace + ".clickCount", snapshot.clickCount);
    queueInputSignal(namespace + ".pointerX", snapshot.pointerX);
    queueInputSignal(namespace + ".pointerY", snapshot.pointerY);
    publishSceneObjectEventSignals(namespace, state, snapshot);
  }

  function scenePickSignalSnapshot(state) {
    return {
      hovered: Boolean(state.hoverIndex >= 0),
      hoverIndex: Math.max(-1, Math.floor(sceneNumber(state.hoverIndex, -1))),
      hoverID: state.hoverID || "",
      down: Boolean(state.downIndex >= 0),
      downIndex: Math.max(-1, Math.floor(sceneNumber(state.downIndex, -1))),
      downID: state.downID || "",
      selected: Boolean(state.selectedIndex >= 0),
      selectedIndex: Math.max(-1, Math.floor(sceneNumber(state.selectedIndex, -1))),
      selectedID: state.selectedID || "",
      clickCount: Math.max(0, Math.floor(sceneNumber(state.clickCount, 0))),
      pointerX: sceneNumber(state.pointerX, 0),
      pointerY: sceneNumber(state.pointerY, 0),
    };
  }

  function sceneInteractionSnapshot(state) {
    var pick = scenePickSignalSnapshot(state);
    pick.revision = Math.max(0, Math.floor(sceneNumber(state.eventRevision, 0)));
    pick.type = state.eventType || "";
    pick.targetIndex = Math.max(-1, Math.floor(sceneNumber(state.eventTargetIndex, -1)));
    pick.targetID = state.eventTargetID || "";
    pick.targetKind = state.eventTargetKind || "";
    pick.hoverKind = state.hoverKind || "";
    pick.downKind = state.downKind || "";
    pick.selectedKind = state.selectedKind || "";
    return pick;
  }

  function scenePickSignalSnapshotKey(snapshot) {
    return [
      snapshot.hovered ? 1 : 0,
      snapshot.hoverIndex,
      snapshot.hoverID,
      snapshot.down ? 1 : 0,
      snapshot.downIndex,
      snapshot.downID,
      snapshot.selected ? 1 : 0,
      snapshot.selectedIndex,
      snapshot.selectedID,
      snapshot.clickCount,
      snapshot.pointerX,
      snapshot.pointerY,
    ].join("|");
  }

  function sceneInteractionSnapshotKey(snapshot) {
    return [
      snapshot.revision,
      snapshot.type,
      snapshot.targetIndex,
      snapshot.targetID,
      snapshot.targetKind,
      snapshot.hovered ? 1 : 0,
      snapshot.hoverIndex,
      snapshot.hoverID,
      snapshot.hoverKind,
      snapshot.down ? 1 : 0,
      snapshot.downIndex,
      snapshot.downID,
      snapshot.downKind,
      snapshot.selected ? 1 : 0,
      snapshot.selectedIndex,
      snapshot.selectedID,
      snapshot.selectedKind,
      snapshot.clickCount,
      snapshot.pointerX,
      snapshot.pointerY,
    ].join("|");
  }

  function publishSceneObjectEventSignals(namespace, state, snapshot) {
    publishSceneObjectBoolSignal(namespace, "hovered", state.publishedHoverSlug, sceneObjectSignalSlug(snapshot.hoverIndex, snapshot.hoverID, snapshot.hoverKind));
    state.publishedHoverSlug = sceneObjectSignalSlug(snapshot.hoverIndex, snapshot.hoverID, snapshot.hoverKind);
    publishSceneObjectBoolSignal(namespace, "down", state.publishedDownSlug, sceneObjectSignalSlug(snapshot.downIndex, snapshot.downID, snapshot.downKind));
    state.publishedDownSlug = sceneObjectSignalSlug(snapshot.downIndex, snapshot.downID, snapshot.downKind);
    publishSceneObjectBoolSignal(namespace, "selected", state.publishedSelectedSlug, sceneObjectSignalSlug(snapshot.selectedIndex, snapshot.selectedID, snapshot.selectedKind));
    state.publishedSelectedSlug = sceneObjectSignalSlug(snapshot.selectedIndex, snapshot.selectedID, snapshot.selectedKind);

    const nextCounts = state.objectClickCounts || Object.create(null);
    const previousCounts = state.publishedObjectClickCounts || Object.create(null);
    const slugs = new Set(Object.keys(previousCounts).concat(Object.keys(nextCounts)));
    slugs.forEach(function(slug) {
      const nextCount = Math.max(0, Math.floor(sceneNumber(nextCounts[slug], 0)));
      const previousCount = Math.max(0, Math.floor(sceneNumber(previousCounts[slug], 0)));
      if (nextCount === previousCount) {
        return;
      }
      queueInputSignal(namespace + ".object." + slug + ".clickCount", nextCount);
    });
    state.publishedObjectClickCounts = Object.assign(Object.create(null), nextCounts);
  }

  function publishSceneObjectBoolSignal(namespace, key, previousSlug, nextSlug) {
    if (previousSlug && previousSlug !== nextSlug) {
      queueInputSignal(namespace + ".object." + previousSlug + "." + key, false);
    }
    if (nextSlug && nextSlug !== previousSlug) {
      queueInputSignal(namespace + ".object." + nextSlug + "." + key, true);
    }
  }

  function sceneBoundsSize(bounds) {
    if (!bounds || typeof bounds !== "object") return [0, 0, 0];
    return [
      Math.abs(sceneNumber(bounds.maxX, 0) - sceneNumber(bounds.minX, 0)),
      Math.abs(sceneNumber(bounds.maxY, 0) - sceneNumber(bounds.minY, 0)),
      Math.abs(sceneNumber(bounds.maxZ, 0) - sceneNumber(bounds.minZ, 0)),
    ].sort(function(a, b) { return b - a; });
  }

  function sceneObjectAllowsPointerDrag(object) {
    if (!object || object.kind === "plane" || object.viewCulled) {
      return false;
    }
    const extents = sceneBoundsSize(object.bounds);
    return extents[0] > SCENE_DRAG_MIN_EXTENT_X && extents[1] > SCENE_DRAG_MIN_EXTENT_Y;
  }

  function sceneObjectAllowsPointerPick(object) {
    if (!object || object.viewCulled) {
      return false;
    }
    if (typeof object.pickable === "boolean") {
      return object.pickable;
    }
    if (object.kind === "plane") {
      return false;
    }
    const extents = sceneBoundsSize(object.bounds);
    return extents[0] > SCENE_PICK_MIN_EXTENT_X && extents[1] > SCENE_PICK_MIN_EXTENT_Y;
  }

  function sceneWorldPointAt(source, vertexIndex) {
    if (!source || typeof source.length !== "number") {
      return null;
    }
    const offset = Math.max(0, vertexIndex * 3);
    if (offset + 2 >= source.length) {
      return null;
    }
    return {
      x: sceneNumber(source[offset], 0),
      y: sceneNumber(source[offset + 1], 0),
      z: sceneNumber(source[offset + 2], 0),
    };
  }

  function sceneProjectedObjectSegments(bundle, object, width, height) {
    if (!bundle || !bundle.camera || !object) {
      return [];
    }
    const vertexOffset = Math.max(0, Math.floor(sceneNumber(object.vertexOffset, 0)));
    const vertexCount = Math.max(0, Math.floor(sceneNumber(object.vertexCount, 0)));
    if (vertexCount < 2) {
      return [];
    }
    const source = bundle.worldPositions;
    if (!source || typeof source.length !== "number") {
      return [];
    }
    const segments = [];
    for (let i = 0; i + 1 < vertexCount; i += 2) {
      const fromWorld = sceneWorldPointAt(source, vertexOffset + i);
      const toWorld = sceneWorldPointAt(source, vertexOffset + i + 1);
      if (!fromWorld || !toWorld) {
        continue;
      }
      const from = sceneProjectPoint(fromWorld, bundle.camera, width, height);
      const to = sceneProjectPoint(toWorld, bundle.camera, width, height);
      if (!from || !to) {
        continue;
      }
      segments.push([from, to]);
    }
    return segments;
  }

  function sceneProjectedSegmentsBounds(segments) {
    if (!Array.isArray(segments) || !segments.length) {
      return null;
    }
    let minX = segments[0][0].x;
    let maxX = segments[0][0].x;
    let minY = segments[0][0].y;
    let maxY = segments[0][0].y;
    for (const segment of segments) {
      for (const point of segment) {
        minX = Math.min(minX, point.x);
        maxX = Math.max(maxX, point.x);
        minY = Math.min(minY, point.y);
        maxY = Math.max(maxY, point.y);
      }
    }
    return { minX, maxX, minY, maxY };
  }

  function scenePointerPadding(bounds) {
    if (!bounds) {
      return SCENE_POINTER_PAD_MIN;
    }
    const span = Math.max(bounds.maxX - bounds.minX, bounds.maxY - bounds.minY);
    return sceneClamp(span * SCENE_POINTER_PAD_SCALE, SCENE_POINTER_PAD_MIN, SCENE_POINTER_PAD_RANGE);
  }

  function sceneDistanceToSegment(point, from, to) {
    const deltaX = to.x - from.x;
    const deltaY = to.y - from.y;
    const lengthSquared = deltaX * deltaX + deltaY * deltaY;
    if (lengthSquared <= 0.0001) {
      return Math.hypot(point.x - from.x, point.y - from.y);
    }
    const t = sceneClamp(((point.x - from.x) * deltaX + (point.y - from.y) * deltaY) / lengthSquared, 0, 1);
    const closestX = from.x + deltaX * t;
    const closestY = from.y + deltaY * t;
    return Math.hypot(point.x - closestX, point.y - closestY);
  }

  function sceneProjectedObjectHull(segments) {
    const points = [];
    const seen = new Set();
    for (const segment of segments) {
      for (const point of segment) {
        const key = point.x.toFixed(3) + ":" + point.y.toFixed(3);
        if (seen.has(key)) {
          continue;
        }
        seen.add(key);
        points.push({ x: point.x, y: point.y });
      }
    }
    if (points.length < 3) {
      return points;
    }
    points.sort(function(a, b) {
      return a.x === b.x ? a.y - b.y : a.x - b.x;
    });
    const lower = [];
    for (const point of points) {
      while (lower.length >= 2 && sceneTurnDirection(lower[lower.length - 2], lower[lower.length - 1], point) <= 0) {
        lower.pop();
      }
      lower.push(point);
    }
    const upper = [];
    for (let i = points.length - 1; i >= 0; i -= 1) {
      const point = points[i];
      while (upper.length >= 2 && sceneTurnDirection(upper[upper.length - 2], upper[upper.length - 1], point) <= 0) {
        upper.pop();
      }
      upper.push(point);
    }
    lower.pop();
    upper.pop();
    return lower.concat(upper);
  }

  function sceneTurnDirection(a, b, c) {
    return (b.x - a.x) * (c.y - a.y) - (b.y - a.y) * (c.x - a.x);
  }

  function scenePointInPolygon(point, polygon) {
    if (!Array.isArray(polygon) || polygon.length < 3) {
      return false;
    }
    let inside = false;
    for (let i = 0, j = polygon.length - 1; i < polygon.length; j = i, i += 1) {
      const xi = polygon[i].x;
      const yi = polygon[i].y;
      const xj = polygon[j].x;
      const yj = polygon[j].y;
      const intersects = ((yi > point.y) !== (yj > point.y)) &&
        (point.x < ((xj - xi) * (point.y - yi)) / ((yj - yi) || 0.000001) + xi);
      if (intersects) {
        inside = !inside;
      }
    }
    return inside;
  }

  function sceneObjectDepthCenter(object, camera) {
    const bounds = object && object.bounds;
    if (!bounds) {
      return sceneWorldPointDepth(0, camera);
    }
    return sceneBoundsDepthMetrics(bounds, camera).center;
  }

  function sceneObjectPointerCapture(bundle, object, point, width, height) {
    const segments = sceneProjectedObjectSegments(bundle, object, width, height);
    if (!segments.length) {
      return null;
    }
    const bounds = sceneProjectedSegmentsBounds(segments);
    if (!bounds) {
      return null;
    }
    const padding = scenePointerPadding(bounds);
    if (
      point.x < bounds.minX - padding ||
      point.x > bounds.maxX + padding ||
      point.y < bounds.minY - padding ||
      point.y > bounds.maxY + padding
    ) {
      return null;
    }
    let minDistance = Number.POSITIVE_INFINITY;
    for (const segment of segments) {
      minDistance = Math.min(minDistance, sceneDistanceToSegment(point, segment[0], segment[1]));
    }
    const inside = scenePointInPolygon(point, sceneProjectedObjectHull(segments));
    if (!inside && minDistance > padding) {
      return null;
    }
    return {
      inside,
      distance: inside ? 0 : minDistance,
      depth: sceneObjectDepthCenter(object, bundle.camera),
      area: Math.max(1, (bounds.maxX - bounds.minX) * (bounds.maxY - bounds.minY)),
    };
  }

  function scenePointerCaptureIsBetter(candidate, current) {
    if (!current) {
      return true;
    }
    if (candidate.inside !== current.inside) {
      return candidate.inside;
    }
    if (Math.abs(candidate.distance - current.distance) > 0.5) {
      return candidate.distance < current.distance;
    }
    if (Math.abs(candidate.depth - current.depth) > 0.01) {
      return candidate.depth < current.depth;
    }
    return candidate.area < current.area;
  }

  function sceneRaycastPick(pointerX, pointerY, width, height, camera, bundle) {
    if (!bundle) {
      return null;
    }

    var ray = sceneScreenToRay(pointerX, pointerY, width, height, camera);
    var closest = sceneRaycastPickGroup(ray, bundle.meshObjects, bundle.worldMeshPositions, 0);
    if (closest) {
      return closest;
    }
    return sceneRaycastPickGroup(ray, bundle.objects, bundle.worldPositions, 0);
  }

  function sceneRaycastPickGroup(ray, objects, positions, indexOffset) {
    if (!Array.isArray(objects) || !objects.length || !positions || typeof positions.length !== "number") {
      return null;
    }
    var closest = null;
    var safeIndexOffset = Math.max(0, Math.floor(sceneNumber(indexOffset, 0)));

    for (var i = 0; i < objects.length; i++) {
      var obj = objects[i];

      if (!sceneObjectAllowsPointerPick(obj)) continue;
      if (obj.viewCulled) continue;

      var bounds = obj.bounds;
      if (!bounds) continue;

      var boundsMin = { x: sceneNumber(bounds.minX, 0), y: sceneNumber(bounds.minY, 0), z: sceneNumber(bounds.minZ, 0) };
      var boundsMax = { x: sceneNumber(bounds.maxX, 0), y: sceneNumber(bounds.maxY, 0), z: sceneNumber(bounds.maxZ, 0) };

      var aabbDist = sceneRayIntersectsAABB(ray.origin, ray.dir, boundsMin, boundsMax);
      if (aabbDist < 0) continue;

      var vertexOffset = Math.max(0, Math.floor(sceneNumber(obj.vertexOffset, 0)));
      var vertexCount = Math.max(0, Math.floor(sceneNumber(obj.vertexCount, 0)));

      for (var tri = 0; tri + 2 < vertexCount; tri += 3) {
        var v0 = sceneWorldPointAt(positions, vertexOffset + tri);
        var v1 = sceneWorldPointAt(positions, vertexOffset + tri + 1);
        var v2 = sceneWorldPointAt(positions, vertexOffset + tri + 2);
        if (!v0 || !v1 || !v2) continue;

        var hit = sceneRayIntersectsTriangle(ray.origin, ray.dir, v0, v1, v2);
        if (hit && (!closest || hit.distance < closest.distance)) {
          closest = {
            index: safeIndexOffset + i,
            object: obj,
            distance: hit.distance,
            inside: true,
            depth: hit.distance,
            area: Math.max(1, (boundsMax.x - boundsMin.x) * (boundsMax.y - boundsMin.y)),
            point: {
              x: ray.origin.x + ray.dir.x * hit.distance,
              y: ray.origin.y + ray.dir.y * hit.distance,
              z: ray.origin.z + ray.dir.z * hit.distance,
            },
          };
        }
      }
    }

    return closest;
  }

  function sceneBundlePointerTarget(bundle, point, width, height, allowObject) {
    if (!bundle || !bundle.camera || !Array.isArray(bundle.objects) || !bundle.objects.length) {
      return null;
    }
    let best = null;
    for (let index = 0; index < bundle.objects.length; index += 1) {
      const object = bundle.objects[index];
      if (typeof allowObject === "function" && !allowObject(object)) {
        continue;
      }
      const capture = sceneObjectPointerCapture(bundle, object, point, width, height);
      if (!capture) {
        continue;
      }
      const candidate = {
        index,
        object,
        inside: capture.inside,
        distance: capture.distance,
        depth: capture.depth,
        area: capture.area,
      };
      if (scenePointerCaptureIsBetter(candidate, best)) {
        best = candidate;
      }
    }
    return best;
  }

  function sceneBundlePointerDragTarget(bundle, point, width, height) {
    return sceneBundlePointerTarget(bundle, point, width, height, sceneObjectAllowsPointerDrag);
  }

  function sceneBundlePointerPickTarget(bundle, point, width, height) {
    if (bundle && bundle.camera && bundle.worldPositions) {
      var rayHit = sceneRaycastPick(point.x, point.y, width, height, bundle.camera, bundle);
      if (rayHit) {
        return rayHit;
      }
    }
    return sceneBundlePointerTarget(bundle, point, width, height, sceneObjectAllowsPointerPick);
  }

  function sceneViewportValue(viewport, key, fallback) {
    return sceneNumber(viewport && viewport[key], fallback);
  }

  function sceneDragViewportMetrics(readViewport, initialWidth, initialHeight) {
    const viewport = typeof readViewport === "function" ? readViewport() : null;
    return {
      width: Math.max(1, sceneViewportValue(viewport, "cssWidth", initialWidth)),
      height: Math.max(1, sceneViewportValue(viewport, "cssHeight", initialHeight)),
    };
  }

  function createSceneDragState(initialWidth, initialHeight) {
    return {
      active: false,
      orbitX: 0,
      orbitY: 0,
      pointerId: null,
      targetIndex: -1,
      lastX: initialWidth / 2,
      lastY: initialHeight / 2,
    };
  }

  function createScenePickState(initialWidth, initialHeight) {
    return {
      pointerId: null,
      hoverIndex: -1,
      hoverID: "",
      hoverKind: "",
      downIndex: -1,
      downID: "",
      downKind: "",
      selectedIndex: -1,
      selectedID: "",
      selectedKind: "",
      clickCount: 0,
      pointerX: initialWidth / 2,
      pointerY: initialHeight / 2,
      eventRevision: 0,
      eventType: "",
      eventTargetIndex: -1,
      eventTargetID: "",
      eventTargetKind: "",
      publishedKey: "",
      publishedEventKey: "",
      publishedHoverSlug: "",
      publishedDownSlug: "",
      publishedSelectedSlug: "",
      objectClickCounts: Object.create(null),
      publishedObjectClickCounts: Object.create(null),
    };
  }

  function sceneDragMatchesActivePointer(state, event) {
    if (!state.active || state.pointerId == null) {
      return state.active;
    }
    if (!event || event.type === "lostpointercapture") {
      return true;
    }
    if (event.pointerId == null) {
      return true;
    }
    return event.pointerId === state.pointerId;
  }

  function scenePointerCanStartDrag(state, event) {
    if (state.active) {
      return false;
    }
    if (!event) {
      return false;
    }
    if (event.pointerType === "mouse") {
      return event.button === 0;
    }
    return event.button == null || event.button === 0;
  }

  function sceneDragTargetAtEvent(event, canvas, readViewport, readSceneBundle, initialWidth, initialHeight) {
    const metrics = sceneDragViewportMetrics(readViewport, initialWidth, initialHeight);
    const pointer = sceneLocalPointerPoint(event, canvas, metrics.width, metrics.height);
    return sceneBundlePointerDragTarget(readSceneBundle && readSceneBundle(), pointer, metrics.width, metrics.height);
  }

  function scenePickMetricsAtEvent(event, canvas, readViewport, initialWidth, initialHeight) {
    const metrics = sceneDragViewportMetrics(readViewport, initialWidth, initialHeight);
    return {
      metrics,
      pointer: sceneLocalPointerPoint(event, canvas, metrics.width, metrics.height),
    };
  }

  function scenePickTargetAtEvent(event, canvas, readViewport, readSceneBundle, initialWidth, initialHeight) {
    const sample = scenePickMetricsAtEvent(event, canvas, readViewport, initialWidth, initialHeight);
    return {
      metrics: sample.metrics,
      pointer: sample.pointer,
      target: sceneBundlePointerPickTarget(readSceneBundle && readSceneBundle(), sample.pointer, sample.metrics.width, sample.metrics.height),
    };
  }

  function updateSceneDragOrbit(state, sample, width, height) {
    state.orbitX = sceneClamp(state.orbitX + sample.deltaX / Math.max(width / 2, 1), SCENE_ORBIT_PITCH_MIN, SCENE_ORBIT_PITCH_MAX);
    state.orbitY = sceneClamp(state.orbitY - sample.deltaY / Math.max(height / 2, 1), SCENE_ORBIT_YAW_MIN, SCENE_ORBIT_YAW_MAX);
  }

  function publishSceneDragInteraction(canvas, event, phase, state, dragNamespace, readViewport, initialWidth, initialHeight) {
    const metrics = sceneDragViewportMetrics(readViewport, initialWidth, initialHeight);
    const sample = sceneLocalPointerSample(event, canvas, metrics.width, metrics.height, state, phase);
    if (!dragNamespace) {
      publishPointerSignals(sample);
      return;
    }
    if (phase === "move") {
      updateSceneDragOrbit(state, sample, metrics.width, metrics.height);
    }
    publishSceneDragSignals(dragNamespace, state, phase !== "end");
  }

  function resetSceneDragInteraction(state, dragNamespace, readViewport, initialWidth, initialHeight) {
    state.pointerId = null;
    state.targetIndex = -1;
    if (dragNamespace) {
      return;
    }
    const metrics = sceneDragViewportMetrics(readViewport, initialWidth, initialHeight);
    resetScenePointerSample(metrics.width, metrics.height, state);
  }

  function scenePrimaryPointerEvent(event) {
    if (!event) {
      return false;
    }
    if (event.pointerType === "mouse") {
      return event.button === 0 || event.button == null;
    }
    return event.button == null || event.button === 0;
  }

  function scenePickMatchesPointer(state, event) {
    if (state.pointerId == null) {
      return true;
    }
    if (!event || event.type === "lostpointercapture") {
      return true;
    }
    if (event.pointerId == null) {
      return true;
    }
    return event.pointerId === state.pointerId;
  }

  function sceneApplyPickTarget(state, sample) {
    const target = sample && sample.target ? sample.target : null;
    const pointer = sample && sample.pointer ? sample.pointer : { x: 0, y: 0 };
    state.pointerX = sceneNumber(pointer.x, 0);
    state.pointerY = sceneNumber(pointer.y, 0);
    state.hoverIndex = target ? target.index : -1;
    state.hoverID = sceneTargetID(target);
    state.hoverKind = sceneTargetKind(target);
    return target;
  }

  function sceneClearPickDown(state) {
    state.pointerId = null;
    state.downIndex = -1;
    state.downID = "";
    state.downKind = "";
  }

  function sceneSelectPickTarget(state, target) {
    state.selectedIndex = target ? target.index : -1;
    state.selectedID = sceneTargetID(target);
    state.selectedKind = sceneTargetKind(target);
  }

  function scenePickTargetsMatch(target, index, id) {
    if (!target) {
      return false;
    }
    const targetID = target.object && typeof target.object.id === "string" ? target.object.id : "";
    return target.index === index && targetID === id;
  }

  function scenePointerID(event) {
    return event && event.pointerId != null ? event.pointerId : null;
  }

  function sceneCapturePointer(canvas, pointerID) {
    if (pointerID == null || !canvas || typeof canvas.setPointerCapture !== "function") {
      return;
    }
    try {
      canvas.setPointerCapture(pointerID);
    } catch (_) {}
  }

  function sceneReleasePointer(canvas, pointerID) {
    if (pointerID == null || !canvas || typeof canvas.releasePointerCapture !== "function") {
      return;
    }
    try {
      canvas.releasePointerCapture(pointerID);
    } catch (_) {}
  }

  function sceneSnapshotTarget(index, id, kind) {
    const targetIndex = Math.max(-1, Math.floor(sceneNumber(index, -1)));
    const targetID = typeof id === "string" ? id : "";
    const targetKind = typeof kind === "string" ? kind : "";
    if (targetIndex < 0 && !targetID && !targetKind) {
      return null;
    }
    return {
      index: targetIndex,
      object: {
        id: targetID,
        kind: targetKind,
      },
    };
  }

  function scenePickStateSnapshot(state) {
    return {
      hover: sceneSnapshotTarget(state.hoverIndex, state.hoverID, state.hoverKind),
      down: sceneSnapshotTarget(state.downIndex, state.downID, state.downKind),
      selected: sceneSnapshotTarget(state.selectedIndex, state.selectedID, state.selectedKind),
      clickCount: Math.max(0, Math.floor(sceneNumber(state.clickCount, 0))),
      pointerX: sceneNumber(state.pointerX, 0),
      pointerY: sceneNumber(state.pointerY, 0),
    };
  }

  function sceneTargetsEqual(left, right) {
    if (!left || !right) {
      return left === right;
    }
    return sceneTargetIndex(left) === sceneTargetIndex(right) &&
      sceneTargetID(left) === sceneTargetID(right) &&
      sceneTargetKind(left) === sceneTargetKind(right);
  }

  function sceneDeriveInteractionEvent(action, before, after) {
    switch (action) {
      case "move":
        if (!sceneTargetsEqual(before.hover, after.hover)) {
          return after.hover ? { type: "hover", target: after.hover } : before.hover ? { type: "leave", target: before.hover } : null;
        }
        return null;
      case "down":
        if (after.down) {
          return { type: "down", target: after.down };
        }
        if (before.selected && !after.selected) {
          return { type: "deselect", target: before.selected };
        }
        return null;
      case "up":
        if (after.selected && (!sceneTargetsEqual(before.selected, after.selected) || after.clickCount !== before.clickCount)) {
          return { type: "select", target: after.selected };
        }
        if (before.selected && !after.selected) {
          return { type: "deselect", target: before.selected };
        }
        return null;
      case "cancel":
        return before.down ? { type: "cancel", target: before.down } : null;
      case "leave":
        return before.hover && !after.hover ? { type: "leave", target: before.hover } : null;
      default:
        return null;
    }
  }

  function sceneRecordInteractionEvent(state, interaction) {
    if (!state || !interaction || !interaction.type) {
      return null;
    }
    state.eventRevision = Math.max(0, Math.floor(sceneNumber(state.eventRevision, 0))) + 1;
    state.eventType = interaction.type;
    state.eventTargetIndex = sceneTargetIndex(interaction.target);
    state.eventTargetID = sceneTargetID(interaction.target);
    state.eventTargetKind = sceneTargetKind(interaction.target);
    const detail = sceneInteractionSnapshot(state);
    return {
      type: detail.type,
      revision: detail.revision,
      targetIndex: detail.targetIndex,
      targetID: detail.targetID,
      targetKind: detail.targetKind,
      hovered: detail.hovered,
      hoverIndex: detail.hoverIndex,
      hoverID: detail.hoverID,
      hoverKind: detail.hoverKind,
      down: detail.down,
      downIndex: detail.downIndex,
      downID: detail.downID,
      downKind: detail.downKind,
      selected: detail.selected,
      selectedIndex: detail.selectedIndex,
      selectedID: detail.selectedID,
      selectedKind: detail.selectedKind,
      clickCount: detail.clickCount,
      pointerX: detail.pointerX,
      pointerY: detail.pointerY,
    };
  }

  function publishSceneInteractionState(pickNamespace, eventNamespace, state) {
    publishScenePickSignals(pickNamespace, state);
    publishSceneEventSignals(eventNamespace, state);
  }

  function sceneHandlePickMove(state, event, canvas, readViewport, readSceneBundle, initialWidth, initialHeight) {
    if (!scenePickMatchesPointer(state, event)) {
      return false;
    }
    sceneApplyPickTarget(state, scenePickTargetAtEvent(event, canvas, readViewport, readSceneBundle, initialWidth, initialHeight));
    return true;
  }

  function sceneHandlePickDown(state, event, canvas, readViewport, readSceneBundle, initialWidth, initialHeight) {
    if (!scenePrimaryPointerEvent(event)) {
      return false;
    }
    const sample = scenePickTargetAtEvent(event, canvas, readViewport, readSceneBundle, initialWidth, initialHeight);
    const target = sceneApplyPickTarget(state, sample);
    if (!target) {
      sceneClearPickDown(state);
      sceneSelectPickTarget(state, null);
      return false;
    }
    state.pointerId = scenePointerID(event);
    state.downIndex = target.index;
    state.downID = sceneTargetID(target);
    state.downKind = sceneTargetKind(target);
    return true;
  }

  function sceneHandlePickUp(state, event, canvas, readViewport, readSceneBundle, initialWidth, initialHeight) {
    if (!scenePickMatchesPointer(state, event)) {
      return { handled: false, pointerId: null };
    }
    const downIndex = state.downIndex;
    const downID = state.downID;
    const pointerID = state.pointerId;
    const sample = scenePickTargetAtEvent(event, canvas, readViewport, readSceneBundle, initialWidth, initialHeight);
    const target = sceneApplyPickTarget(state, sample);
    if (downIndex >= 0) {
      if (scenePickTargetsMatch(target, downIndex, downID)) {
        state.clickCount += 1;
        sceneSelectPickTarget(state, target);
        const selectedSlug = sceneObjectSignalSlug(state.selectedIndex, state.selectedID, state.selectedKind);
        if (selectedSlug) {
          const previousCount = state.objectClickCounts && state.objectClickCounts[selectedSlug];
          state.objectClickCounts[selectedSlug] = Math.max(0, Math.floor(sceneNumber(previousCount, 0))) + 1;
        }
      } else if (!target) {
        sceneSelectPickTarget(state, null);
      }
    }
    sceneClearPickDown(state);
    return { handled: pointerID != null || downIndex >= 0, pointerId: pointerID };
  }

  function sceneHandlePickCancel(state, event) {
    if (!scenePickMatchesPointer(state, event)) {
      return { handled: false, pointerId: null };
    }
    const pointerID = state.pointerId;
    const handled = pointerID != null || state.downIndex >= 0;
    sceneClearPickDown(state);
    return { handled, pointerId: pointerID };
  }

  function sceneHandlePickLeave(state) {
    if (state.pointerId != null) {
      return false;
    }
    state.hoverIndex = -1;
    state.hoverID = "";
    state.hoverKind = "";
    return true;
  }

  function setupSceneDragInteractions(canvas, props, readViewport, readSceneBundle) {
    if (!canvas || !sceneBool(props.dragToRotate, false)) {
      return { dispose() {} };
    }

    const dragNamespace = sceneDragSignalNamespace(props);
    const initialMetrics = sceneDragViewportMetrics(readViewport, sceneNumber(props.width, 720), sceneNumber(props.height, 420));
    const initialWidth = initialMetrics.width;
    const initialHeight = initialMetrics.height;
    const state = createSceneDragState(initialWidth, initialHeight);
    let documentListenersAttached = false;

    canvas.style.cursor = "grab";
    canvas.style.touchAction = "none";

    function attachDocumentListeners() {
      if (documentListenersAttached) {
        return;
      }
      documentListenersAttached = true;
      document.addEventListener("pointermove", onPointerMove);
      document.addEventListener("pointerup", finishDrag);
      document.addEventListener("pointercancel", finishDrag);
    }

    function detachDocumentListeners() {
      if (!documentListenersAttached) {
        return;
      }
      documentListenersAttached = false;
      document.removeEventListener("pointermove", onPointerMove);
      document.removeEventListener("pointerup", finishDrag);
      document.removeEventListener("pointercancel", finishDrag);
    }

    function onPointerDown(event) {
      if (!scenePointerCanStartDrag(state, event)) {
        return;
      }
      const target = sceneDragTargetAtEvent(event, canvas, readViewport, readSceneBundle, initialWidth, initialHeight);
      if (!target) {
        return;
      }
      state.active = true;
      state.pointerId = event.pointerId;
      state.targetIndex = target.index;
      canvas.style.cursor = "grabbing";
      attachDocumentListeners();
      if (typeof canvas.setPointerCapture === "function") {
        canvas.setPointerCapture(event.pointerId);
      }
      event.preventDefault();
      event.stopPropagation();
      publishSceneDragInteraction(canvas, event, "start", state, dragNamespace, readViewport, initialWidth, initialHeight);
    }

    function onPointerMove(event) {
      if (!sceneDragMatchesActivePointer(state, event)) {
        return;
      }
      event.preventDefault();
      event.stopPropagation();
      publishSceneDragInteraction(canvas, event, "move", state, dragNamespace, readViewport, initialWidth, initialHeight);
    }

    function finishDrag(event) {
      if (!sceneDragMatchesActivePointer(state, event)) {
        return;
      }
      const wasActive = state.active;
      state.active = false;
      canvas.style.cursor = "grab";
      detachDocumentListeners();
      if (!wasActive) {
        return;
      }
      event.preventDefault();
      event.stopPropagation();
      if (state.pointerId != null && typeof canvas.releasePointerCapture === "function") {
        try {
          canvas.releasePointerCapture(state.pointerId);
        } catch (_) {}
      }
      state.pointerId = null;
      state.targetIndex = -1;
      publishSceneDragInteraction(canvas, event, "end", state, dragNamespace, readViewport, initialWidth, initialHeight);
      resetSceneDragInteraction(state, dragNamespace, readViewport, initialWidth, initialHeight);
    }

    canvas.addEventListener("pointerdown", onPointerDown);
    canvas.addEventListener("pointermove", onPointerMove);
    canvas.addEventListener("pointerup", finishDrag);
    canvas.addEventListener("pointercancel", finishDrag);
    canvas.addEventListener("lostpointercapture", finishDrag);

    return {
      dispose() {
        canvas.removeEventListener("pointerdown", onPointerDown);
        canvas.removeEventListener("pointermove", onPointerMove);
        canvas.removeEventListener("pointerup", finishDrag);
        canvas.removeEventListener("pointercancel", finishDrag);
        canvas.removeEventListener("lostpointercapture", finishDrag);
        detachDocumentListeners();
        canvas.style.cursor = "";
        canvas.style.touchAction = "";
        if (state.active && dragNamespace) {
          state.active = false;
          state.pointerId = null;
          state.targetIndex = -1;
          publishSceneDragSignals(dragNamespace, state, false);
        } else {
          state.active = false;
        }
        resetSceneDragInteraction(state, dragNamespace, readViewport, initialWidth, initialHeight);
      },
    };
  }

  function setupScenePickInteractions(canvas, props, readViewport, readSceneBundle, emitInteraction) {
    const pickNamespace = scenePickSignalNamespace(props);
    const eventNamespace = sceneEventSignalNamespace(props);
    if (!canvas || (!pickNamespace && !eventNamespace)) {
      return { dispose() {} };
    }

    const initialMetrics = sceneDragViewportMetrics(readViewport, sceneNumber(props.width, 720), sceneNumber(props.height, 420));
    const initialWidth = initialMetrics.width;
    const initialHeight = initialMetrics.height;
    const state = createScenePickState(initialWidth, initialHeight);
    let documentListenersAttached = false;

    function publish() {
      publishSceneInteractionState(pickNamespace, eventNamespace, state);
    }

    function emit(action, before) {
      const interaction = sceneDeriveInteractionEvent(action, before, scenePickStateSnapshot(state));
      const detail = sceneRecordInteractionEvent(state, interaction);
      if (detail && typeof emitInteraction === "function") {
        emitInteraction(detail);
      }
    }

    function attachDocumentListeners() {
      if (documentListenersAttached) {
        return;
      }
      documentListenersAttached = true;
      document.addEventListener("pointermove", onPointerMove);
      document.addEventListener("pointerup", onPointerUp);
      document.addEventListener("pointercancel", onPointerCancel);
    }

    function detachDocumentListeners() {
      if (!documentListenersAttached) {
        return;
      }
      documentListenersAttached = false;
      document.removeEventListener("pointermove", onPointerMove);
      document.removeEventListener("pointerup", onPointerUp);
      document.removeEventListener("pointercancel", onPointerCancel);
    }

    function onPointerMove(event) {
      const before = scenePickStateSnapshot(state);
      if (!sceneHandlePickMove(state, event, canvas, readViewport, readSceneBundle, initialWidth, initialHeight)) {
        return;
      }
      emit("move", before);
      publish();
    }

    function onPointerDown(event) {
      const before = scenePickStateSnapshot(state);
      const handled = sceneHandlePickDown(state, event, canvas, readViewport, readSceneBundle, initialWidth, initialHeight);
      emit("down", before);
      if (handled) {
        attachDocumentListeners();
        sceneCapturePointer(canvas, state.pointerId);
        if (typeof event.preventDefault === "function") {
          event.preventDefault();
        }
        if (typeof event.stopPropagation === "function") {
          event.stopPropagation();
        }
      }
      publish();
    }

    function onPointerUp(event) {
      const before = scenePickStateSnapshot(state);
      const result = sceneHandlePickUp(state, event, canvas, readViewport, readSceneBundle, initialWidth, initialHeight);
      if (!result.handled) {
        return;
      }
      emit("up", before);
      detachDocumentListeners();
      sceneReleasePointer(canvas, result.pointerId);
      if (typeof event.preventDefault === "function") {
        event.preventDefault();
      }
      if (typeof event.stopPropagation === "function") {
        event.stopPropagation();
      }
      publish();
    }

    function onPointerCancel(event) {
      const before = scenePickStateSnapshot(state);
      const result = sceneHandlePickCancel(state, event);
      if (!result.handled) {
        return;
      }
      emit("cancel", before);
      detachDocumentListeners();
      sceneReleasePointer(canvas, result.pointerId);
      publish();
    }

    function onPointerLeave() {
      const before = scenePickStateSnapshot(state);
      if (!sceneHandlePickLeave(state)) {
        return;
      }
      emit("leave", before);
      publish();
    }

    canvas.addEventListener("pointermove", onPointerMove);
    canvas.addEventListener("pointerdown", onPointerDown);
    canvas.addEventListener("pointerup", onPointerUp);
    canvas.addEventListener("pointercancel", onPointerCancel);
    canvas.addEventListener("pointerleave", onPointerLeave);
    canvas.addEventListener("lostpointercapture", onPointerCancel);
    publish();

    return {
      dispose() {
        canvas.removeEventListener("pointermove", onPointerMove);
        canvas.removeEventListener("pointerdown", onPointerDown);
        canvas.removeEventListener("pointerup", onPointerUp);
        canvas.removeEventListener("pointercancel", onPointerCancel);
        canvas.removeEventListener("pointerleave", onPointerLeave);
        canvas.removeEventListener("lostpointercapture", onPointerCancel);
        detachDocumentListeners();
        sceneReleasePointer(canvas, state.pointerId);
        state.pointerId = null;
        state.hoverIndex = -1;
        state.hoverID = "";
        state.hoverKind = "";
        state.downIndex = -1;
        state.downID = "";
        state.downKind = "";
        state.selectedIndex = -1;
        state.selectedID = "";
        state.selectedKind = "";
        state.clickCount = 0;
        state.eventType = "";
        state.eventTargetIndex = -1;
        state.eventTargetID = "";
        state.eventTargetKind = "";
        state.objectClickCounts = Object.create(null);
        publish();
      },
    };
  }

  function strokeLine(ctx2d, from, to) {
    ctx2d.beginPath();
    ctx2d.moveTo(from.x, from.y);
    ctx2d.lineTo(to.x, to.y);
    ctx2d.stroke();
  }

  function sceneBundleUsesWorldProjection(bundle) {
    return Boolean(
      bundle &&
      bundle.camera &&
      bundle.sourceCamera &&
      !sceneCameraEquivalent(bundle.sourceCamera, bundle.camera) &&
      bundle.worldVertexCount > 0 &&
      bundle.worldPositions &&
      bundle.worldColors
    );
  }

  function renderSceneCanvasWorldBundle(ctx2d, bundle, width, height) {
    const positions = bundle && bundle.worldPositions;
    const colors = bundle && bundle.worldColors;
    const widths = bundle && bundle.worldLineWidths;
    const vertexCount = Math.max(0, Math.floor(sceneNumber(bundle && bundle.worldVertexCount, 0)));
    for (let index = 0; index + 1 < vertexCount; index += 2) {
      const fromWorld = sceneWorldPointAt(positions, index);
      const toWorld = sceneWorldPointAt(positions, index + 1);
      if (!fromWorld || !toWorld) {
        continue;
      }
      const from = sceneProjectPoint(fromWorld, bundle.camera, width, height);
      const to = sceneProjectPoint(toWorld, bundle.camera, width, height);
      if (!from || !to) {
        continue;
      }
      const colorOffset = index * 4;
      ctx2d.strokeStyle = sceneRGBAString([
        sceneNumber(colors && colors[colorOffset], 0.55),
        sceneNumber(colors && colors[colorOffset + 1], 0.88),
        sceneNumber(colors && colors[colorOffset + 2], 1),
        sceneNumber(colors && colors[colorOffset + 3], 1),
      ]);
      const segmentIndex = index / 2;
      const segmentWidth = widths && segmentIndex < widths.length ? widths[segmentIndex] : 0;
      ctx2d.lineWidth = segmentWidth > 0 ? segmentWidth : 1.8;
      strokeLine(ctx2d, from, to);
    }
  }

  function createSceneCanvasRenderer(ctx2d, canvas) {
    return {
      kind: "canvas",
      render(bundle, viewport) {
        const devicePixelRatio = Math.max(1, sceneViewportValue(viewport, "devicePixelRatio", 1));
        const lines = Array.isArray(bundle && bundle.lines) ? bundle.lines : [];
        ctx2d.clearRect(0, 0, canvas.width, canvas.height);
        ctx2d.fillStyle = bundle && bundle.background ? bundle.background : "#08151f";
        ctx2d.fillRect(0, 0, canvas.width, canvas.height);
        if (typeof ctx2d.save === "function") {
          ctx2d.save();
        }
        if (devicePixelRatio !== 1 && typeof ctx2d.scale === "function") {
          ctx2d.scale(devicePixelRatio, devicePixelRatio);
        }
        if (sceneBundleUsesWorldProjection(bundle)) {
          renderSceneCanvasWorldBundle(ctx2d, bundle, sceneViewportValue(viewport, "cssWidth", canvas.width), sceneViewportValue(viewport, "cssHeight", canvas.height));
        } else {
          for (const line of lines) {
            ctx2d.strokeStyle = line.color;
            ctx2d.lineWidth = line.lineWidth;
            strokeLine(ctx2d, line.from, line.to);
          }
        }
        if (typeof ctx2d.restore === "function") {
          ctx2d.restore();
        }
      },
      dispose() {},
    };
  }

  function createSceneRenderer(canvas, props, capability) {
    const webglPreference = sceneCapabilityWebGLPreference(props, capability);
    if (webglPreference === "prefer" || webglPreference === "force") {
      if (webglPreference !== "force" && typeof sceneWebGPUAvailable === "function" && sceneWebGPUAvailable()) {
        var gpuRenderer = createSceneWebGPURendererOrFallback(canvas);
        if (gpuRenderer) {
          return {
            renderer: gpuRenderer,
            fallbackReason: "",
          };
        }
      }
      if (typeof createScenePBRRendererOrFallback === "function") {
        const gl = typeof canvas.getContext === "function" ? canvas.getContext("webgl2", {
          alpha: true,
          premultipliedAlpha: false,
          antialias: capability.tier === "full" && !capability.lowPower && !capability.reducedData,
          powerPreference: capability.lowPower || capability.tier === "constrained" ? "low-power" : "high-performance",
        }) : null;
        if (gl) {
          const pbrRenderer = createScenePBRRendererOrFallback(gl, canvas, {});
          if (pbrRenderer) {
            return {
              renderer: pbrRenderer,
              fallbackReason: "",
            };
          }
        }
      }
      const webglRenderer = createSceneWebGLRenderer(canvas, {
        antialias: capability.tier === "full" && !capability.lowPower && !capability.reducedData,
        powerPreference: capability.lowPower || capability.tier === "constrained" ? "low-power" : "high-performance",
      });
      if (webglRenderer) {
        return {
          renderer: webglRenderer,
          fallbackReason: "",
        };
      }
    }
    const ctx2d = typeof canvas.getContext === "function" ? canvas.getContext("2d") : null;
    if (!ctx2d) {
      return null;
    }
    return {
      renderer: createSceneCanvasRenderer(ctx2d, canvas),
      fallbackReason: sceneRendererFallbackReason(props, capability, "canvas"),
    };
  }

  const sceneModelAssetCache = new Map();

  function resolveSceneModelAssetURL(baseSrc, value) {
    const raw = typeof value === "string" ? value.trim() : "";
    if (!raw) {
      return "";
    }
    try {
      const baseURL = new URL(baseSrc || "", window.location.href).toString();
      return new URL(raw, baseURL).toString();
    } catch (_error) {
      return raw;
    }
  }

  function resolveSceneModelObjectURLs(baseSrc, rawObject) {
    if (!rawObject || typeof rawObject !== "object") {
      return rawObject;
    }
    const resolved = Object.assign({}, rawObject);
    if (typeof resolved.texture === "string" && resolved.texture.trim()) {
      resolved.texture = resolveSceneModelAssetURL(baseSrc, resolved.texture);
    }
    if (resolved.material && typeof resolved.material === "object") {
      const material = Object.assign({}, resolved.material);
      if (typeof material.texture === "string" && material.texture.trim()) {
        material.texture = resolveSceneModelAssetURL(baseSrc, material.texture);
      }
      resolved.material = material;
    }
    return resolved;
  }

  function sceneModelTransformPoint(point, model) {
    const local = point && typeof point === "object" ? point : { x: 0, y: 0, z: 0 };
    const scaleX = sceneNumber(model && model.scaleX, 1);
    const scaleY = sceneNumber(model && model.scaleY, 1);
    const scaleZ = sceneNumber(model && model.scaleZ, 1);
    const rotated = sceneRotatePoint(
      {
        x: local.x * scaleX,
        y: local.y * scaleY,
        z: local.z * scaleZ,
      },
      sceneNumber(model && model.rotationX, 0),
      sceneNumber(model && model.rotationY, 0),
      sceneNumber(model && model.rotationZ, 0),
    );
    return {
      x: rotated.x + sceneNumber(model && model.x, 0),
      y: rotated.y + sceneNumber(model && model.y, 0),
      z: rotated.z + sceneNumber(model && model.z, 0),
    };
  }

  function sceneModelTransformVector(point, model) {
    const local = point && typeof point === "object" ? point : { x: 0, y: 0, z: 0 };
    return sceneRotatePoint(
      {
        x: local.x * sceneNumber(model && model.scaleX, 1),
        y: local.y * sceneNumber(model && model.scaleY, 1),
        z: local.z * sceneNumber(model && model.scaleZ, 1),
      },
      sceneNumber(model && model.rotationX, 0),
      sceneNumber(model && model.rotationY, 0),
      sceneNumber(model && model.rotationZ, 0),
    );
  }

  function sceneModelMaxScale(model) {
    return Math.max(
      Math.abs(sceneNumber(model && model.scaleX, 1)),
      Math.abs(sceneNumber(model && model.scaleY, 1)),
      Math.abs(sceneNumber(model && model.scaleZ, 1)),
    );
  }

  function sceneModelRotateDirection(point, model) {
    return sceneRotatePoint(
      point && typeof point === "object" ? point : { x: 0, y: 0, z: 0 },
      sceneNumber(model && model.rotationX, 0),
      sceneNumber(model && model.rotationY, 0),
      sceneNumber(model && model.rotationZ, 0),
    );
  }

  function sceneModelMaterialOverrideSource(model) {
    return model && model.materialOverride && typeof model.materialOverride === "object"
      ? model.materialOverride
      : null;
  }

  function sceneAssignMaterialOverride(next, material, sourceKey, targetKey, override) {
    if (!override || !Object.prototype.hasOwnProperty.call(override, sourceKey)) {
      return;
    }
    const key = targetKey || sourceKey;
    next[key] = override[sourceKey];
    if (material) {
      material[key] = override[sourceKey];
    }
  }

  function sceneApplyMaterialOverride(raw, model) {
    const override = sceneModelMaterialOverrideSource(model);
    if (!override) {
      return raw && typeof raw === "object" ? Object.assign({}, raw) : {};
    }
    const next = raw && typeof raw === "object" ? Object.assign({}, raw) : {};
    const material = next.material && typeof next.material === "object"
      ? Object.assign({}, next.material)
      : null;
    if (typeof override.materialKind === "string" && override.materialKind) {
      next.materialKind = override.materialKind;
      if (typeof next.material === "string") {
        next.material = override.materialKind;
      }
      if (material) {
        material.kind = override.materialKind;
      }
    }
    sceneAssignMaterialOverride(next, material, "color", "color", override);
    sceneAssignMaterialOverride(next, material, "texture", "texture", override);
    sceneAssignMaterialOverride(next, material, "opacity", "opacity", override);
    sceneAssignMaterialOverride(next, material, "emissive", "emissive", override);
    sceneAssignMaterialOverride(next, material, "blendMode", "blendMode", override);
    sceneAssignMaterialOverride(next, material, "renderPass", "renderPass", override);
    sceneAssignMaterialOverride(next, material, "wireframe", "wireframe", override);
    if (material) {
      next.material = material;
    }
    return next;
  }

  function sceneModelPrimitiveObject(object, model, prefix) {
    const instanced = Object.assign({}, object, {
      id: prefix + "/" + (object.id || "object"),
      x: 0,
      y: 0,
      z: 0,
      rotationX: sceneNumber(object.rotationX, 0) + sceneNumber(model && model.rotationX, 0),
      rotationY: sceneNumber(object.rotationY, 0) + sceneNumber(model && model.rotationY, 0),
      rotationZ: sceneNumber(object.rotationZ, 0) + sceneNumber(model && model.rotationZ, 0),
    });
    const positioned = sceneModelTransformPoint({ x: object.x, y: object.y, z: object.z }, model);
    instanced.x = positioned.x;
    instanced.y = positioned.y;
    instanced.z = positioned.z;
    const scaleX = Math.abs(sceneNumber(model && model.scaleX, 1));
    const scaleY = Math.abs(sceneNumber(model && model.scaleY, 1));
    const scaleZ = Math.abs(sceneNumber(model && model.scaleZ, 1));
    switch (object.kind) {
      case "cube":
        if (Math.abs(scaleX - scaleY) > 0.0001 || Math.abs(scaleX - scaleZ) > 0.0001) {
          instanced.kind = "box";
          instanced.width = sceneNumber(object.size, 1.2) * scaleX;
          instanced.height = sceneNumber(object.size, 1.2) * scaleY;
          instanced.depth = sceneNumber(object.size, 1.2) * scaleZ;
        } else {
          instanced.size = sceneNumber(object.size, 1.2) * scaleX;
        }
        break;
      case "sphere":
        instanced.radius = sceneNumber(object.radius, sceneNumber(object.size, 1.2) / 2) * Math.max(scaleX, scaleY, scaleZ);
        break;
      default:
        instanced.width = sceneNumber(object.width, sceneNumber(object.size, 1.2)) * scaleX;
        instanced.height = sceneNumber(object.height, sceneNumber(object.size, 1.2)) * scaleY;
        instanced.depth = sceneNumber(object.depth, sceneNumber(object.size, 1.2)) * scaleZ;
        break;
    }
    if (model && model.static !== null) {
      instanced.static = Boolean(model.static);
    }
    if (model && typeof model.pickable === "boolean") {
      instanced.pickable = model.pickable;
    }
    return normalizeSceneObject(instanced, prefix);
  }

  function sceneModelLineObject(object, model, prefix) {
    const scaleX = sceneNumber(model && model.scaleX, 1);
    const scaleY = sceneNumber(model && model.scaleY, 1);
    const scaleZ = sceneNumber(model && model.scaleZ, 1);
    const scaled = sceneScaleModelLinePoints(object.points, scaleX, scaleY, scaleZ);
    const positioned = sceneModelTransformPoint({ x: object.x, y: object.y, z: object.z }, model);
    const instanced = Object.assign({}, object, {
      id: prefix + "/" + (object.id || "object"),
      points: scaled,
      lineSegments: sceneCloneModelLineSegments(object.lineSegments),
      x: positioned.x,
      y: positioned.y,
      z: positioned.z,
      rotationX: sceneNumber(object.rotationX, 0) + sceneNumber(model && model.rotationX, 0),
      rotationY: sceneNumber(object.rotationY, 0) + sceneNumber(model && model.rotationY, 0),
      rotationZ: sceneNumber(object.rotationZ, 0) + sceneNumber(model && model.rotationZ, 0),
    });
    if (model && model.static !== null) {
      instanced.static = Boolean(model.static);
    }
    if (model && typeof model.pickable === "boolean") {
      instanced.pickable = model.pickable;
    }
    return normalizeSceneObject(instanced, prefix);
  }

  function sceneScaleModelLinePoints(points, scaleX, scaleY, scaleZ) {
    return Array.isArray(points) ? points.map(function(point) {
      return {
        x: sceneNumber(point && point.x, 0) * scaleX,
        y: sceneNumber(point && point.y, 0) * scaleY,
        z: sceneNumber(point && point.z, 0) * scaleZ,
      };
    }) : [];
  }

  function sceneCloneModelLineSegments(segments) {
    return Array.isArray(segments) ? segments.map(function(pair) {
      return Array.isArray(pair) ? pair.slice(0, 2) : pair;
    }) : [];
  }

  function sceneInstantiateModelObject(rawObject, model, prefix, index) {
    const source = sceneApplyMaterialOverride(rawObject, model);
    const normalized = normalizeSceneObject(source, index);
    if (normalized.vertices && normalized.vertices.positions && normalized.vertices.count > 0) {
      return sceneModelMeshObject(normalized, model, prefix);
    }
    if (normalized.kind === "lines") {
      return sceneModelLineObject(normalized, model, prefix);
    }
    return sceneModelPrimitiveObject(normalized, model, prefix);
  }

  function sceneModelTransformMeshFloats(values, tupleSize, mapper) {
    const source = values instanceof Float32Array ? values : sceneTypedFloatArray(values);
    const typed = new Float32Array(source.length);
    const safeTupleSize = Math.max(1, Math.floor(sceneNumber(tupleSize, 1)));
    for (let index = 0; index + safeTupleSize - 1 < source.length; index += safeTupleSize) {
      const mapped = mapper(
        sceneNumber(source[index], 0),
        sceneNumber(source[index + 1], 0),
        sceneNumber(source[index + 2], 0),
        safeTupleSize > 3 ? sceneNumber(source[index + 3], 1) : undefined
      );
      typed[index] = sceneNumber(mapped && mapped.x, 0);
      typed[index + 1] = sceneNumber(mapped && mapped.y, 0);
      typed[index + 2] = sceneNumber(mapped && mapped.z, 0);
      if (safeTupleSize > 3) {
        typed[index + 3] = sceneNumber(mapped && mapped.w, 1);
      }
    }
    return typed;
  }

  function sceneModelMeshObject(object, model, prefix) {
    const vertices = object && object.vertices && typeof object.vertices === "object" ? object.vertices : null;
    if (!vertices || !vertices.positions || !vertices.count) {
      return null;
    }
    const instanced = Object.assign({}, object, {
      id: prefix + "/" + (object.id || "object"),
      x: 0,
      y: 0,
      z: 0,
      rotationX: 0,
      rotationY: 0,
      rotationZ: 0,
      spinX: 0,
      spinY: 0,
      spinZ: 0,
      shiftX: 0,
      shiftY: 0,
      shiftZ: 0,
      driftSpeed: 0,
      driftPhase: 0,
    });
    instanced.vertices = {
      count: Math.max(0, Math.floor(sceneNumber(vertices.count, 0))),
      positions: sceneModelTransformMeshFloats(vertices.positions, 3, function(x, y, z) {
        return sceneModelTransformPoint({ x: x, y: y, z: z }, model);
      }),
      normals: sceneModelTransformMeshFloats(vertices.normals, 3, function(x, y, z) {
        return sceneNormalizeDirection(sceneModelTransformVector({ x: x, y: y, z: z }, model));
      }),
      uvs: vertices.uvs instanceof Float32Array ? new Float32Array(vertices.uvs) : sceneTypedFloatArray(vertices.uvs),
      tangents: sceneModelTransformMeshFloats(vertices.tangents, 4, function(x, y, z, w) {
        const rotated = sceneNormalizeDirection(sceneModelTransformVector({ x: x, y: y, z: z }, model));
        return { x: rotated.x, y: rotated.y, z: rotated.z, w: sceneNumber(w, 1) };
      }),
      joints: vertices.joints instanceof Float32Array ? new Float32Array(vertices.joints) : sceneTypedFloatArray(vertices.joints),
      weights: vertices.weights instanceof Float32Array ? new Float32Array(vertices.weights) : sceneTypedFloatArray(vertices.weights),
    };
    if (model && model.static !== null) {
      instanced.static = Boolean(model.static);
    }
    if (model && typeof model.pickable === "boolean") {
      instanced.pickable = model.pickable;
    }
    return normalizeSceneObject(instanced, prefix);
  }

  function sceneInstantiateModelLabel(rawLabel, model, prefix, index) {
    const normalized = normalizeSceneLabel(rawLabel, index);
    const position = sceneModelTransformPoint({ x: normalized.x, y: normalized.y, z: normalized.z }, model);
    return Object.assign({}, normalized, {
      id: prefix + "/" + normalized.id,
      x: position.x,
      y: position.y,
      z: position.z,
    });
  }

  function sceneInstantiateModelLight(rawLight, model, prefix, index) {
    const normalized = normalizeSceneLight(rawLight, index, null);
    if (!normalized) {
      return null;
    }
    const next = Object.assign({}, normalized, {
      id: prefix + "/" + normalized.id,
    });
    if (next.kind === "directional") {
      const rotated = sceneModelRotateDirection({
        x: next.directionX,
        y: next.directionY,
        z: next.directionZ,
      }, model);
      next.directionX = rotated.x;
      next.directionY = rotated.y;
      next.directionZ = rotated.z;
      if (typeof hashLightContent === "function") {
        next._lightHash = hashLightContent(next);
      }
      return next;
    }
    const position = sceneModelTransformPoint({ x: next.x, y: next.y, z: next.z }, model);
    next.x = position.x;
    next.y = position.y;
    next.z = position.z;
    if (next.kind === "point") {
      next.range = sceneNumber(next.range, 0) * Math.max(
        Math.abs(sceneNumber(model && model.scaleX, 1)),
        Math.abs(sceneNumber(model && model.scaleY, 1)),
        Math.abs(sceneNumber(model && model.scaleZ, 1)),
      );
    }
    if (typeof hashLightContent === "function") {
      next._lightHash = hashLightContent(next);
    }
    return next;
  }

  function sceneInstantiateModelSprite(rawSprite, model, prefix, index) {
    const normalized = normalizeSceneSprite(rawSprite, index);
    if (!normalized || !normalized.src) {
      return null;
    }
    const position = sceneModelTransformPoint({ x: normalized.x, y: normalized.y, z: normalized.z }, model);
    const shift = sceneModelTransformVector({ x: normalized.shiftX, y: normalized.shiftY, z: normalized.shiftZ }, model);
    const modelScale = sceneModelMaxScale(model);
    return Object.assign({}, normalized, {
      id: prefix + "/" + normalized.id,
      x: position.x,
      y: position.y,
      z: position.z,
      shiftX: shift.x,
      shiftY: shift.y,
      shiftZ: shift.z,
      width: normalized.width * modelScale,
      height: normalized.height * modelScale,
    });
  }

  function parseSceneModelAsset(raw, src) {
    let payload = raw;
    if (payload && typeof payload === "object" && payload.scene && typeof payload.scene === "object") {
      payload = payload.scene;
    }
    if (Array.isArray(payload)) {
      payload = { objects: payload };
    }
    const record = payload && typeof payload === "object" ? payload : {};
    const sprites = Array.isArray(record.sprites) ? record.sprites.map(function(sprite) {
      if (!sprite || typeof sprite !== "object") {
        return sprite;
      }
      const resolved = Object.assign({}, sprite);
      resolved.src = resolveSceneModelAssetURL(src, sprite.src);
      return resolved;
    }) : [];
    return {
      src,
      objects: Array.isArray(record.objects) ? record.objects.map(function(object) {
        return resolveSceneModelObjectURLs(src, object);
      }) : [],
      labels: Array.isArray(record.labels) ? record.labels : [],
      sprites,
      lights: Array.isArray(record.lights) ? record.lights : [],
    };
  }

  function sceneModelAssetFormat(src) {
    const raw = typeof src === "string" ? src.trim() : "";
    if (!raw) {
      return "";
    }
    let pathname = raw;
    try {
      pathname = new URL(raw, window.location.href).pathname;
    } catch (_error) {
      pathname = raw.split(/[?#]/, 1)[0];
    }
    const normalized = pathname.toLowerCase();
    if (normalized.endsWith(".glb")) {
      return "glb";
    }
    if (normalized.endsWith(".gltf")) {
      return "gltf";
    }
    return "json";
  }

  function resolveSceneSubFeatureURL(datasetKey, fallback) {
    try {
      var tag = document.querySelector('script[data-gosx-script="feature-scene3d"]');
      if (tag && tag.dataset && tag.dataset[datasetKey]) {
        return tag.dataset[datasetKey];
      }
    } catch (_e) {}
    return fallback;
  }

  var sceneGLTFFeaturePromise = null;

  function ensureGLTFFeatureLoaded() {
    if (window.__gosx_scene3d_gltf_api) {
      return Promise.resolve(window.__gosx_scene3d_gltf_api);
    }
    if (sceneGLTFFeaturePromise) {
      return sceneGLTFFeaturePromise;
    }
    sceneGLTFFeaturePromise = new Promise(function(resolve, reject) {
      var s = document.createElement("script");
      s.async = false;
      s.dataset.gosxScript = "feature-scene3d-gltf";
      s.src = resolveSceneSubFeatureURL("gosxScene3dGltfUrl", "/gosx/bootstrap-feature-scene3d-gltf.js");
      s.onload = function() {
        if (window.__gosx_scene3d_gltf_api) {
          resolve(window.__gosx_scene3d_gltf_api);
        } else {
          reject(new Error("scene3d-gltf chunk loaded but did not publish API"));
        }
      };
      s.onerror = function() {
        sceneGLTFFeaturePromise = null; // allow retry on next attempt
        reject(new Error("failed to load scene3d-gltf chunk"));
      };
      document.head.appendChild(s);
    });
    return sceneGLTFFeaturePromise;
  }

  var sceneAnimationFeaturePromise = null;

  function ensureAnimationFeatureLoaded() {
    if (window.__gosx_scene3d_animation_api) {
      return Promise.resolve(window.__gosx_scene3d_animation_api);
    }
    if (sceneAnimationFeaturePromise) {
      return sceneAnimationFeaturePromise;
    }
    sceneAnimationFeaturePromise = new Promise(function(resolve, reject) {
      var s = document.createElement("script");
      s.async = false;
      s.dataset.gosxScript = "feature-scene3d-animation";
      s.src = resolveSceneSubFeatureURL("gosxScene3dAnimationUrl", "/gosx/bootstrap-feature-scene3d-animation.js");
      s.onload = function() {
        if (window.__gosx_scene3d_animation_api) {
          resolve(window.__gosx_scene3d_animation_api);
        } else {
          reject(new Error("scene3d-animation chunk loaded but did not publish API"));
        }
      };
      s.onerror = function() {
        sceneAnimationFeaturePromise = null;
        reject(new Error("failed to load scene3d-animation chunk"));
      };
      document.head.appendChild(s);
    });
    return sceneAnimationFeaturePromise;
  }

  window.__gosx_ensure_scene3d_animation_loaded = ensureAnimationFeatureLoaded;

  async function loadSceneModelAsset(src) {
    const key = String(src || "").trim();
    if (!key) {
      return parseSceneModelAsset({}, key);
    }
    if (!sceneModelAssetCache.has(key)) {
      sceneModelAssetCache.set(key, (async function() {
        try {
          const format = sceneModelAssetFormat(key);
          if (format === "glb" || format === "gltf") {
            var gltfApi = await ensureGLTFFeatureLoaded();
            return parseSceneModelAsset(gltfApi.gltfSceneToModelAsset(await gltfApi.sceneLoadGLTFModel(key), key), key);
          }
          const response = await fetch(key, { credentials: "same-origin" });
          if (!response || !response.ok) {
            throw new Error("HTTP " + String(response && response.status || 0));
          }
          return parseSceneModelAsset(await response.json(), key);
        } catch (error) {
          console.warn("[gosx] failed to load Scene3D model asset:", key, error && error.message ? error.message : error);
          return parseSceneModelAsset({}, key);
        }
      })());
    }
    return sceneModelAssetCache.get(key);
  }

  async function hydrateSceneStateModels(state, props) {
    const models = sceneModels(props);
    if (!models.length) {
      return { models: 0, objects: 0, labels: 0, sprites: 0, lights: 0 };
    }
    let objectCount = 0;
    let labelCount = 0;
    let spriteCount = 0;
    let lightCount = 0;
    await Promise.all(models.map(async function(model, modelIndex) {
      const asset = await loadSceneModelAsset(model.src);
      const prefix = model.id || ("scene-model-" + modelIndex);
      for (let i = 0; i < asset.objects.length; i += 1) {
        const object = sceneInstantiateModelObject(asset.objects[i], model, prefix, i);
        if (!object) {
          continue;
        }
        state.objects.set(object.id, object);
        objectCount += 1;
      }
      for (let i = 0; i < asset.labels.length; i += 1) {
        const label = sceneInstantiateModelLabel(asset.labels[i], model, prefix, i);
        if (!label || !label.text.trim()) {
          continue;
        }
        state.labels.set(label.id, label);
        labelCount += 1;
      }
      for (let i = 0; i < asset.sprites.length; i += 1) {
        const sprite = sceneInstantiateModelSprite(asset.sprites[i], model, prefix, i);
        if (!sprite) {
          continue;
        }
        state.sprites.set(sprite.id, sprite);
        spriteCount += 1;
      }
      for (let i = 0; i < asset.lights.length; i += 1) {
        const light = sceneInstantiateModelLight(asset.lights[i], model, prefix, i);
        if (!light) {
          continue;
        }
        state.lights.set(light.id, light);
        lightCount += 1;
      }
    }));
    return { models: models.length, objects: objectCount, labels: labelCount, sprites: spriteCount, lights: lightCount };
  }

  function normalizeSceneCapabilityTier(value) {
    switch (String(value || "").trim().toLowerCase()) {
      case "constrained":
      case "balanced":
      case "full":
        return String(value).trim().toLowerCase();
      default:
        return "";
    }
  }

  function sceneMediaQueryMatches(query) {
    if (!query || typeof window.matchMedia !== "function") {
      return false;
    }
    try {
      return Boolean(window.matchMedia(query).matches);
    } catch (_error) {
      return false;
    }
  }

  function sceneEnvironmentState() {
    if (window.__gosx
      && window.__gosx.environment
      && typeof window.__gosx.environment.get === "function") {
      return window.__gosx.environment.get();
    }
    return null;
  }

  function sceneCapabilityProfile(props) {
    const requestedTier = normalizeSceneCapabilityTier(props && props.capabilityTier);
    const environment = sceneEnvironmentState();
    const navigatorRef = window && window.navigator ? window.navigator : {};
    const coarsePointer = environment ? Boolean(environment.coarsePointer) : (sceneMediaQueryMatches("(pointer: coarse)") || sceneMediaQueryMatches("(any-pointer: coarse)"));
    const hover = environment ? Boolean(environment.hover) : (sceneMediaQueryMatches("(hover: hover)") || sceneMediaQueryMatches("(any-hover: hover)"));
    const reducedData = environment ? Boolean(environment.reducedData) : sceneMediaQueryMatches("(prefers-reduced-data: reduce)");
    const lowPower = environment ? Boolean(environment.lowPower) : false;
    const visualViewportActive = environment ? Boolean(environment.visualViewportActive) : Boolean(window.visualViewport);
    const deviceMemory = sceneNumber(environment && environment.deviceMemory, sceneNumber(navigatorRef && navigatorRef.deviceMemory, 0));
    const hardwareConcurrency = Math.max(0, Math.floor(sceneNumber(environment && environment.hardwareConcurrency, sceneNumber(navigatorRef && navigatorRef.hardwareConcurrency, 0))));
    const constrainedHardware = lowPower || reducedData || (deviceMemory > 0 && deviceMemory <= 4) || (hardwareConcurrency > 0 && hardwareConcurrency <= 4);

    let tier = requestedTier;
    if (!tier) {
      if ((coarsePointer && constrainedHardware) || reducedData || lowPower) {
        tier = "constrained";
      } else if (coarsePointer) {
        tier = "balanced";
      } else {
        tier = "full";
      }
    }

    return {
      tier,
      coarsePointer,
      hover,
      reducedData,
      lowPower,
      visualViewportActive,
      deviceMemory,
      hardwareConcurrency,
    };
  }

  function sceneCapabilityWebGLPreference(props, capability) {
    if (!sceneBool(props && props.preferWebGL, true)) {
      return "disabled";
    }
    if (sceneBool(props && props.forceWebGL, false)) {
      return "force";
    }
    if (sceneBool(props && props.preferCanvas, false)) {
      return "avoid";
    }
    if (!capability) {
      return "prefer";
    }
    if (capability.reducedData || capability.lowPower) {
      return "avoid";
    }
    if (capability.tier === "constrained" && capability.coarsePointer) {
      return "avoid";
    }
    return "prefer";
  }

  function sceneRendererFallbackReason(props, capability, rendererKind) {
    if (rendererKind === "webgl") {
      return "";
    }
    switch (sceneCapabilityWebGLPreference(props, capability)) {
      case "disabled":
        return "webgl-disabled";
      case "avoid":
        return "environment-constrained";
      default:
        return sceneBool(props && props.preferWebGL, true) ? "webgl-unavailable" : "";
    }
  }

  function sceneCapabilityChanged(prev, next) {
    if (!prev || !next) {
      return true;
    }
    return prev.tier !== next.tier
      || prev.coarsePointer !== next.coarsePointer
      || prev.hover !== next.hover
      || prev.reducedData !== next.reducedData
      || prev.lowPower !== next.lowPower
      || prev.visualViewportActive !== next.visualViewportActive
      || prev.deviceMemory !== next.deviceMemory
      || prev.hardwareConcurrency !== next.hardwareConcurrency;
  }

  function defaultSceneMaxDevicePixelRatio(capability) {
    if (capability && (capability.reducedData || capability.lowPower)) {
      switch (capability.tier) {
        case "constrained":
          return 1.25;
        case "balanced":
          return 1.5;
        default:
          return 1.75;
      }
    }
    switch (capability && capability.tier) {
      case "constrained":
        return 1.5;
      case "balanced":
        return 1.75;
      default:
        return 2;
    }
  }

  function applySceneCapabilityState(mount, props, capability) {
    if (!mount || !capability) {
      return;
    }
    setAttrValue(mount, "data-gosx-scene3d-capability-tier", capability.tier);
    setAttrValue(mount, "data-gosx-scene3d-coarse-pointer", capability.coarsePointer ? "true" : "false");
    setAttrValue(mount, "data-gosx-scene3d-hover", capability.hover ? "true" : "false");
    setAttrValue(mount, "data-gosx-scene3d-reduced-data", capability.reducedData ? "true" : "false");
    setAttrValue(mount, "data-gosx-scene3d-low-power", capability.lowPower ? "true" : "false");
    setAttrValue(mount, "data-gosx-scene3d-visual-viewport", capability.visualViewportActive ? "true" : "false");
    setAttrValue(mount, "data-gosx-scene3d-webgl-preference", sceneCapabilityWebGLPreference(props, capability));
    setAttrValue(mount, "data-gosx-scene3d-device-memory", capability.deviceMemory > 0 ? capability.deviceMemory : "");
    setAttrValue(mount, "data-gosx-scene3d-hardware-concurrency", capability.hardwareConcurrency > 0 ? capability.hardwareConcurrency : "");
  }

  function applySceneRendererState(mount, renderer, fallbackReason) {
    if (!mount) {
      return;
    }
    setAttrValue(mount, "data-gosx-scene3d-renderer", renderer && renderer.kind ? renderer.kind : "");
    setAttrValue(mount, "data-gosx-scene3d-renderer-fallback", fallbackReason || "");
  }

  function observeSceneCapability(mount, props, capability, onChange) {
    if (!mount || !capability || typeof onChange !== "function") {
      return function() {};
    }
    applySceneCapabilityState(mount, props, capability);
    if (!(window.__gosx.environment && typeof window.__gosx.environment.observe === "function")) {
      return function() {};
    }
    return window.__gosx.environment.observe(function() {
      const next = sceneCapabilityProfile(props);
      if (!sceneCapabilityChanged(capability, next)) {
        return;
      }
      capability.tier = next.tier;
      capability.coarsePointer = next.coarsePointer;
      capability.hover = next.hover;
      capability.reducedData = next.reducedData;
      capability.lowPower = next.lowPower;
      capability.visualViewportActive = next.visualViewportActive;
      capability.deviceMemory = next.deviceMemory;
      capability.hardwareConcurrency = next.hardwareConcurrency;
      applySceneCapabilityState(mount, props, capability);
      onChange("capability");
    }, { immediate: false });
  }

  function sceneViewportBase(props) {
    const width = Math.max(240, sceneNumber(props && props.width, 720));
    const height = Math.max(180, sceneNumber(props && props.height, 420));
    const explicitMaxDevicePixelRatio = sceneNumber(props && (props.maxDevicePixelRatio || props.maxPixelRatio), 0);
    return {
      baseWidth: width,
      baseHeight: height,
      aspectRatio: width / Math.max(1, height),
      responsive: sceneBool(props && props.responsive, true),
      explicitMaxDevicePixelRatio,
    };
  }

  function sceneViewportDevicePixelRatio(props, maxDevicePixelRatio) {
    const environment = sceneEnvironmentState();
    const preferred = sceneNumber(
      props && (props.devicePixelRatio || props.pixelRatio),
      sceneNumber(window && window.devicePixelRatio, sceneNumber(environment && environment.devicePixelRatio, 1)),
    );
    return Math.max(1, Math.min(Math.max(1, maxDevicePixelRatio || 1), preferred));
  }

  function sceneViewportFromMount(mount, props, base, canvas, capability) {
    let cssWidth = base.baseWidth;
    let cssHeight = base.baseHeight;
    const useMeasuredHeight = sceneBool(props && (props.fillHeight || props.responsiveHeight), false);
    if (base.responsive) {
      const mountRect = mount && typeof mount.getBoundingClientRect === "function"
        ? mount.getBoundingClientRect()
        : null;
      const canvasRect = canvas && typeof canvas.getBoundingClientRect === "function"
        ? canvas.getBoundingClientRect()
        : null;
      const measuredCanvasWidth = sceneNumber(canvasRect && canvasRect.width, 0);
      const measuredMountWidth = sceneNumber(mountRect && mountRect.width, 0);
      if (measuredCanvasWidth > 0 && (measuredMountWidth <= 0 || measuredCanvasWidth <= measuredMountWidth * 1.5)) {
        cssWidth = measuredCanvasWidth;
      } else if (measuredMountWidth > 0) {
        cssWidth = measuredMountWidth;
      }
      const measuredHeight = measuredCanvasWidth > 0 && (measuredMountWidth <= 0 || measuredCanvasWidth <= measuredMountWidth * 1.5)
        ? sceneNumber(canvasRect && canvasRect.height, 0)
        : sceneNumber(mountRect && mountRect.height, 0);
      if (useMeasuredHeight && measuredHeight > 0) {
        cssHeight = measuredHeight;
      } else if (cssWidth > 0) {
        cssHeight = cssWidth / Math.max(0.0001, base.aspectRatio);
      }
    }
    cssWidth = Math.max(1, Math.round(cssWidth));
    cssHeight = Math.max(1, Math.round(cssHeight));
    const capabilityMaxDevicePixelRatio = defaultSceneMaxDevicePixelRatio(capability);
    const maxDevicePixelRatio = Math.max(
      1,
      base.explicitMaxDevicePixelRatio > 0
        ? Math.min(base.explicitMaxDevicePixelRatio, capabilityMaxDevicePixelRatio)
        : capabilityMaxDevicePixelRatio,
    );
    const devicePixelRatio = sceneViewportDevicePixelRatio(props, maxDevicePixelRatio);
    return {
      cssWidth,
      cssHeight,
      devicePixelRatio,
      pixelWidth: Math.max(1, Math.round(cssWidth * devicePixelRatio)),
      pixelHeight: Math.max(1, Math.round(cssHeight * devicePixelRatio)),
    };
  }

  function sceneViewportChanged(prev, next) {
    if (!prev || !next) {
      return true;
    }
    return prev.cssWidth !== next.cssWidth
      || prev.cssHeight !== next.cssHeight
      || prev.pixelWidth !== next.pixelWidth
      || prev.pixelHeight !== next.pixelHeight
      || Math.abs(sceneNumber(prev.devicePixelRatio, 1) - sceneNumber(next.devicePixelRatio, 1)) > 0.001;
  }

  function applySceneViewport(mount, canvas, labelLayer, viewport, base) {
    if (!mount || !canvas || !viewport) {
      return viewport;
    }
    setAttrValue(mount, "data-gosx-scene3d-css-width", viewport.cssWidth);
    setAttrValue(mount, "data-gosx-scene3d-css-height", viewport.cssHeight);
    setAttrValue(mount, "data-gosx-scene3d-pixel-ratio", viewport.devicePixelRatio);
    setStyleValue(mount.style, "--gosx-scene-css-width", viewport.cssWidth + "px");
    setStyleValue(mount.style, "--gosx-scene-css-height", viewport.cssHeight + "px");
    setStyleValue(mount.style, "--gosx-scene-pixel-ratio", String(viewport.devicePixelRatio));
    canvas.width = viewport.pixelWidth;
    canvas.height = viewport.pixelHeight;
    canvas.setAttribute("width", String(viewport.pixelWidth));
    canvas.setAttribute("height", String(viewport.pixelHeight));
    if (labelLayer) {
      const mountRect = typeof mount.getBoundingClientRect === "function" ? mount.getBoundingClientRect() : null;
      const canvasRect = typeof canvas.getBoundingClientRect === "function" ? canvas.getBoundingClientRect() : null;
      const left = mountRect && canvasRect ? Math.max(0, sceneNumber(canvasRect.left, 0) - sceneNumber(mountRect.left, 0)) : 0;
      const top = mountRect && canvasRect ? Math.max(0, sceneNumber(canvasRect.top, 0) - sceneNumber(mountRect.top, 0)) : 0;
      labelLayer.style.left = left + "px";
      labelLayer.style.top = top + "px";
      labelLayer.style.right = "auto";
      labelLayer.style.bottom = "auto";
      labelLayer.style.width = viewport.cssWidth + "px";
      labelLayer.style.height = viewport.cssHeight + "px";
    }
    if (base && !base.responsive) {
      canvas.style.width = viewport.cssWidth + "px";
      canvas.style.height = viewport.cssHeight + "px";
    } else {
      canvas.style.width = "100%";
      canvas.style.height = "auto";
    }
    return viewport;
  }

  function observeSceneViewport(mount, refresh) {
    if (!mount || typeof refresh !== "function") {
      return function() {};
    }
    let resizeObserver = null;
    let windowResizeListener = null;
    let stopEnvironment = null;

    var resizeRefreshPending = false;
    function scheduleResizeRefresh() {
      if (resizeRefreshPending) {
        return;
      }
      resizeRefreshPending = true;
      if (typeof Promise === "function") {
        Promise.resolve().then(function() {
          resizeRefreshPending = false;
          refresh("resize");
        });
      } else {
        resizeRefreshPending = false;
        refresh("resize");
      }
    }

    if (typeof ResizeObserver === "function") {
      resizeObserver = new ResizeObserver(scheduleResizeRefresh);
      if (typeof resizeObserver.observe === "function") {
        resizeObserver.observe(mount);
      }
    } else if (typeof window.addEventListener === "function") {
      windowResizeListener = scheduleResizeRefresh;
      window.addEventListener("resize", windowResizeListener);
    }

    if (window.__gosx.environment && typeof window.__gosx.environment.observe === "function") {
      stopEnvironment = window.__gosx.environment.observe(function() {
        refresh("environment");
      }, { immediate: false });
    }

    return function() {
      if (resizeObserver && typeof resizeObserver.disconnect === "function") {
        resizeObserver.disconnect();
      }
      if (windowResizeListener && typeof window.removeEventListener === "function") {
        window.removeEventListener("resize", windowResizeListener);
      }
      if (typeof stopEnvironment === "function") {
        stopEnvironment();
      }
    };
  }

  function initialSceneLifecycleState() {
    const environment = sceneEnvironmentState();
    return {
      pageVisible: environment ? Boolean(environment.pageVisible) : String(document && document.visibilityState || "visible").toLowerCase() !== "hidden",
      inViewport: true,
    };
  }

  function initialSceneMotionState(props) {
    const respectReducedMotion = sceneBool(props && props.respectReducedMotion, true);
    const environment = sceneEnvironmentState();
    return {
      respectReducedMotion,
      reducedMotion: respectReducedMotion && environment
        ? Boolean(environment.reducedMotion)
        : sceneMediaQueryMatches("(prefers-reduced-motion: reduce)"),
    };
  }

  function applySceneMotionState(mount, motion) {
    if (!mount || !motion) {
      return;
    }
    setAttrValue(mount, "data-gosx-scene3d-reduced-motion", motion.reducedMotion ? "true" : "false");
  }

  function observeSceneMotion(mount, motion, onChange) {
    if (!mount || !motion || typeof onChange !== "function") {
      return function() {};
    }

    applySceneMotionState(mount, motion);
    if (!motion.respectReducedMotion || !(window.__gosx.environment && typeof window.__gosx.environment.observe === "function")) {
      return function() {};
    }

    return window.__gosx.environment.observe(function(environment) {
      const next = Boolean(environment && environment.reducedMotion);
      if (motion.reducedMotion === next) {
        return;
      }
      motion.reducedMotion = next;
      applySceneMotionState(mount, motion);
      onChange("motion");
    }, { immediate: false });
  }

  function applySceneLifecycleState(mount, lifecycle) {
    if (!mount || !lifecycle) {
      return;
    }
    setAttrValue(mount, "data-gosx-scene3d-page-visible", lifecycle.pageVisible ? "true" : "false");
    setAttrValue(mount, "data-gosx-scene3d-in-viewport", lifecycle.inViewport ? "true" : "false");
    setAttrValue(mount, "data-gosx-scene3d-active", lifecycle.pageVisible && lifecycle.inViewport ? "true" : "false");
  }

  function sceneLifecyclePinnedToViewport(mount) {
    if (!mount || typeof window.getComputedStyle !== "function") {
      return false;
    }
    try {
      const position = String(window.getComputedStyle(mount).position || "").toLowerCase();
      return position === "fixed";
    } catch (_error) {
      return false;
    }
  }

  function observeSceneLifecycle(mount, lifecycle, onChange) {
    if (!mount || !lifecycle || typeof onChange !== "function") {
      return function() {};
    }

    let stopIntersection = null;
    let stopEnvironment = null;

    if (sceneLifecyclePinnedToViewport(mount)) {
      lifecycle.inViewport = true;
    } else if (typeof IntersectionObserver === "function") {
      const observer = new IntersectionObserver(function(entries) {
        for (const entry of entries || []) {
          if (!entry || entry.target !== mount) {
            continue;
          }
          const next = entry.isIntersecting !== false && sceneNumber(entry.intersectionRatio, 1) > 0;
          if (lifecycle.inViewport === next) {
            continue;
          }
          lifecycle.inViewport = next;
          applySceneLifecycleState(mount, lifecycle);
          onChange("intersection");
        }
      }, { threshold: [0, 0.01, 0.25] });
      if (typeof observer.observe === "function") {
        observer.observe(mount);
      }
      stopIntersection = function() {
        if (typeof observer.disconnect === "function") {
          observer.disconnect();
        }
      };
    }

    if (window.__gosx.environment && typeof window.__gosx.environment.observe === "function") {
      stopEnvironment = window.__gosx.environment.observe(function(environment) {
        const next = Boolean(environment && environment.pageVisible);
        if (lifecycle.pageVisible === next) {
          return;
        }
        lifecycle.pageVisible = next;
        applySceneLifecycleState(mount, lifecycle);
        onChange("visibility");
      }, { immediate: false });
    }

    applySceneLifecycleState(mount, lifecycle);
    return function() {
      if (stopIntersection) {
        stopIntersection();
      }
      if (typeof stopEnvironment === "function") {
        stopEnvironment();
      }
    };
  }

  function sceneLabelLayoutKey(label) {
    return [
      gosxTextLayoutRevision(),
      label.text,
      label.font,
      sceneNumber(label.maxWidth, 180),
      Math.max(0, Math.floor(sceneNumber(label.maxLines, 0))),
      normalizeTextLayoutOverflow(label.overflow),
      normalizeSceneLabelWhiteSpace(label.whiteSpace),
      sceneNumber(label.lineHeight, 18),
      normalizeSceneLabelAlign(label.textAlign),
    ].join("\n");
  }

  function sceneMeasureTextWidth(font, text) {
    if (typeof window.__gosx_measure_text_batch !== "function") {
      return String(text || "").length * 8;
    }
    try {
      const raw = window.__gosx_measure_text_batch(font, JSON.stringify([String(text || "")]));
      const widths = typeof raw === "string" ? JSON.parse(raw) : raw;
      return Array.isArray(widths) && widths.length > 0 ? sceneNumber(widths[0], String(text || "").length * 8) : String(text || "").length * 8;
    } catch (_error) {
      return String(text || "").length * 8;
    }
  }

  function fallbackSceneLabelLayout(label) {
    return layoutBrowserText(
      String(label.text || ""),
      label.font,
      sceneNumber(label.maxWidth, 180),
      normalizeSceneLabelWhiteSpace(label.whiteSpace),
      sceneNumber(label.lineHeight, 18),
      {
        maxLines: Math.max(0, Math.floor(sceneNumber(label.maxLines, 0))),
        overflow: normalizeTextLayoutOverflow(label.overflow),
      },
    );
  }

  function layoutSceneLabel(label, layoutCache) {
    const revision = gosxTextLayoutRevision();
    if (layoutCache.__gosxRevision !== revision) {
      layoutCache.clear();
      layoutCache.__gosxRevision = revision;
    }
    const cacheKey = sceneLabelLayoutKey(label);
    if (layoutCache.has(cacheKey)) {
      return {
        key: cacheKey,
        value: layoutCache.get(cacheKey),
      };
    }

    let layout = null;
    if (typeof window.__gosx_text_layout === "function") {
      try {
        layout = window.__gosx_text_layout(
          label.text,
          label.font,
          sceneNumber(label.maxWidth, 180),
          normalizeSceneLabelWhiteSpace(label.whiteSpace),
          sceneNumber(label.lineHeight, 18),
          {
            maxLines: Math.max(0, Math.floor(sceneNumber(label.maxLines, 0))),
            overflow: normalizeTextLayoutOverflow(label.overflow),
          },
        );
      } catch (error) {
        console.error("[gosx] scene label layout failed:", error);
      }
    }

    if (!layout || !Array.isArray(layout.lines)) {
      layout = fallbackSceneLabelLayout(label);
    }
    if (layoutCache.size >= sceneLabelLayoutCacheLimit) {
      const oldest = layoutCache.keys().next();
      if (!oldest.done) {
        layoutCache.delete(oldest.value);
      }
    }
    layoutCache.set(cacheKey, layout);
    return {
      key: cacheKey,
      value: layout,
    };
  }

  const sceneLabelPaddingX = 10;
  const sceneLabelPaddingY = 8;

  function sceneLabelBoxMetrics(label, layout) {
    const contentWidth = Math.max(
      1,
      Math.min(
        sceneNumber(label.maxWidth, 180),
        Math.max(1, Math.ceil(sceneNumber(layout && layout.maxLineWidth, 0) || sceneMeasureTextWidth(label.font, label.text)))
      )
    );
    const contentHeight = Math.max(
      sceneNumber(label.lineHeight, 18),
      Math.ceil(sceneNumber(layout && layout.height, sceneNumber(label.lineHeight, 18)))
    );
    return {
      contentWidth,
      contentHeight,
      totalWidth: contentWidth + (sceneLabelPaddingX * 2),
      totalHeight: contentHeight + (sceneLabelPaddingY * 2),
      maxTotalWidth: Math.max(contentWidth + (sceneLabelPaddingX * 2), sceneNumber(label.maxWidth, 180) + (sceneLabelPaddingX * 2)),
    };
  }

  function sceneLabelBounds(label, metrics) {
    const anchorX = sceneNumber(label.anchorX, 0.5);
    const anchorY = sceneNumber(label.anchorY, 1);
    const anchorPointX = sceneNumber(label.position && label.position.x, 0) + sceneNumber(label.offsetX, 0);
    const anchorPointY = sceneNumber(label.position && label.position.y, 0) + sceneNumber(label.offsetY, 0);
    const left = anchorPointX - (anchorX * metrics.totalWidth);
    const top = anchorPointY - (anchorY * metrics.totalHeight);
    return {
      left,
      top,
      right: left + metrics.totalWidth,
      bottom: top + metrics.totalHeight,
      anchor: { x: anchorPointX, y: anchorPointY },
      center: { x: left + (metrics.totalWidth / 2), y: top + (metrics.totalHeight / 2) },
    };
  }

  function sceneRectArea(box) {
    if (!box) {
      return 0;
    }
    return Math.max(0, box.right - box.left) * Math.max(0, box.bottom - box.top);
  }

  function sceneRectOverlapArea(a, b) {
    if (!a || !b) {
      return 0;
    }
    const overlapX = Math.max(0, Math.min(a.right, b.maxX == null ? b.right : b.maxX) - Math.max(a.left, b.minX == null ? b.left : b.minX));
    const overlapY = Math.max(0, Math.min(a.bottom, b.maxY == null ? b.bottom : b.maxY) - Math.max(a.top, b.minY == null ? b.top : b.minY));
    return overlapX * overlapY;
  }

  function sceneRectsIntersect(a, b) {
    return sceneRectOverlapArea(a, b) > 0;
  }

  function sceneBoundsContainPoint(bounds, point) {
    if (!bounds || !point) {
      return false;
    }
    return point.x >= bounds.minX && point.x <= bounds.maxX && point.y >= bounds.minY && point.y <= bounds.maxY;
  }

  function buildSceneLabelOccluders(bundle, width, height) {
    if (!bundle || !bundle.camera || !Array.isArray(bundle.objects) || !bundle.objects.length) {
      return [];
    }
    const occluders = [];
    for (const object of bundle.objects) {
      if (!object || object.viewCulled) {
        continue;
      }
      const segments = sceneProjectedObjectSegments(bundle, object, width, height);
      if (!segments.length) {
        continue;
      }
      const bounds = sceneProjectedSegmentsBounds(segments);
      if (!bounds) {
        continue;
      }
      occluders.push({
        depth: sceneNumber(object.depthCenter, sceneObjectDepthCenter(object, bundle.camera)),
        bounds,
        hull: sceneProjectedObjectHull(segments),
      });
    }
    occluders.sort(function(a, b) {
      return a.depth - b.depth;
    });
    return occluders;
  }

  function sceneLabelOccluded(entry, occluders) {
    return sceneOverlayOccluded(entry, occluders, entry && entry.label && entry.label.occlude);
  }

  function sceneOverlayOccluded(entry, occluders, occlude) {
    if (!entry || !occlude || !Array.isArray(occluders) || !occluders.length) {
      return false;
    }
    const overlayDepth = sceneNumber(entry && entry.depth, 0);
    for (const occluder of occluders) {
      if (occluder.depth > overlayDepth + 0.05) {
        continue;
      }
      if (!sceneRectsIntersect(entry.box, occluder.bounds)) {
        continue;
      }
      if (scenePointInPolygon(entry.box.anchor, occluder.hull) || sceneBoundsContainPoint(occluder.bounds, entry.box.anchor)) {
        return true;
      }
      if (scenePointInPolygon(entry.box.center, occluder.hull)) {
        return true;
      }
      const overlapRatio = sceneRectOverlapArea(entry.box, occluder.bounds) / Math.max(1, sceneRectArea(entry.box));
      if (overlapRatio >= 0.28) {
        return true;
      }
    }
    return false;
  }

  function sceneLabelPriorityCompare(a, b) {
    const priorityDiff = sceneNumber(b && b.label && b.label.priority, 0) - sceneNumber(a && a.label && a.label.priority, 0);
    if (Math.abs(priorityDiff) > 0.001) {
      return priorityDiff;
    }
    const depthDiff = sceneNumber(a && a.label && a.label.depth, 0) - sceneNumber(b && b.label && b.label.depth, 0);
    if (Math.abs(depthDiff) > 0.001) {
      return depthDiff;
    }
    return sceneNumber(a && a.order, 0) - sceneNumber(b && b.order, 0);
  }

  function prepareSceneLabelEntries(bundle, layoutCache, width, height) {
    const labels = bundle && Array.isArray(bundle.labels) ? bundle.labels : [];
    const occluders = buildSceneLabelOccluders(bundle, width, height);
    const entries = [];
    for (let index = 0; index < labels.length; index += 1) {
      const label = labels[index];
      if (!label || typeof label.text !== "string" || label.text.trim() === "") {
        continue;
      }
      const layout = layoutSceneLabel(label, layoutCache);
      const metrics = sceneLabelBoxMetrics(label, layout.value);
      const box = sceneLabelBounds(label, metrics);
      entries.push({
        id: label.id || ("scene-label-" + index),
        order: index,
        label,
        depth: sceneNumber(label.depth, 0),
        layoutKey: layout.key,
        layout: layout.value,
        metrics,
        box,
        occluded: false,
        hidden: false,
      });
    }

    const sorted = entries.slice().sort(sceneLabelPriorityCompare);
    const occupied = [];
    for (const entry of sorted) {
      entry.occluded = sceneLabelOccluded(entry, occluders);
      if (entry.occluded) {
        entry.hidden = true;
        continue;
      }
      if (normalizeSceneLabelCollision(entry.label.collision) !== "allow") {
        for (const prior of occupied) {
          if (sceneRectsIntersect(entry.box, prior)) {
            entry.hidden = true;
            break;
          }
        }
      }
      if (!entry.hidden) {
        occupied.push(entry.box);
      }
    }

    return entries;
  }

  function sceneSpriteBounds(sprite) {
    const anchorX = sceneNumber(sprite.anchorX, 0.5);
    const anchorY = sceneNumber(sprite.anchorY, 0.5);
    const spriteWidth = Math.max(1, sceneNumber(sprite.width, 1));
    const spriteHeight = Math.max(1, sceneNumber(sprite.height, 1));
    const anchorPointX = sceneNumber(sprite.position && sprite.position.x, 0) + sceneNumber(sprite.offsetX, 0);
    const anchorPointY = sceneNumber(sprite.position && sprite.position.y, 0) + sceneNumber(sprite.offsetY, 0);
    const left = anchorPointX - (anchorX * spriteWidth);
    const top = anchorPointY - (anchorY * spriteHeight);
    return {
      left,
      top,
      right: left + spriteWidth,
      bottom: top + spriteHeight,
      anchor: { x: anchorPointX, y: anchorPointY },
      center: { x: left + (spriteWidth / 2), y: top + (spriteHeight / 2) },
    };
  }

  function sceneSpritePriorityCompare(a, b) {
    const priorityDiff = sceneNumber(b && b.sprite && b.sprite.priority, 0) - sceneNumber(a && a.sprite && a.sprite.priority, 0);
    if (Math.abs(priorityDiff) > 0.001) {
      return priorityDiff;
    }
    const depthDiff = sceneNumber(a && a.sprite && a.sprite.depth, 0) - sceneNumber(b && b.sprite && b.sprite.depth, 0);
    if (Math.abs(depthDiff) > 0.001) {
      return depthDiff;
    }
    return sceneNumber(a && a.order, 0) - sceneNumber(b && b.order, 0);
  }

  function prepareSceneSpriteEntries(bundle, width, height) {
    const sprites = bundle && Array.isArray(bundle.sprites) ? bundle.sprites : [];
    const occluders = buildSceneLabelOccluders(bundle, width, height);
    const entries = [];
    for (let index = 0; index < sprites.length; index += 1) {
      const sprite = sprites[index];
      if (!sprite || typeof sprite.src !== "string" || sprite.src.trim() === "") {
        continue;
      }
      const box = sceneSpriteBounds(sprite);
      entries.push({
        id: sprite.id || ("scene-sprite-" + index),
        order: index,
        sprite,
        depth: sceneNumber(sprite.depth, 0),
        box,
        occluded: false,
        hidden: false,
      });
    }
    const sorted = entries.slice().sort(sceneSpritePriorityCompare);
    for (const entry of sorted) {
      entry.occluded = sceneOverlayOccluded(entry, occluders, entry.sprite && entry.sprite.occlude);
      if (entry.occluded) {
        entry.hidden = true;
      }
    }
    return entries;
  }

  function renderSceneLabelElement(element, label, layoutKey, layout, metrics, box, hidden, occluded) {
    const align = normalizeSceneLabelAlign(label.textAlign);
    const whiteSpace = normalizeSceneLabelWhiteSpace(label.whiteSpace);
    const zIndex = Math.max(1, 1000 + Math.round(sceneNumber(label.priority, 0) * 10) - Math.round(sceneNumber(label.depth, 0) * 10));

    element.setAttribute("data-gosx-scene-label", label.id || "");
    setAttrValue(element, "class", label.className ? ("gosx-scene-label " + label.className) : "gosx-scene-label");
    setAttrValue(element, "data-gosx-scene-label-collision", normalizeSceneLabelCollision(label.collision));
    setAttrValue(element, "data-gosx-scene-label-occlude", label.occlude ? "true" : "false");
    setAttrValue(element, "data-gosx-scene-label-occluded", occluded ? "true" : "false");
    setAttrValue(element, "data-gosx-scene-label-visibility", hidden ? "hidden" : "visible");
    setAttrValue(element, "data-gosx-scene-label-priority", sceneNumber(label.priority, 0));
    setAttrValue(element, "data-gosx-scene-label-depth", sceneNumber(label.depth, 0));
    setAttrValue(element, "data-gosx-scene-label-truncated", layout && layout.truncated ? "true" : "false");

    applyTextLayoutPresentation(element, {
      font: label.font,
      whiteSpace: whiteSpace,
      lineHeight: sceneNumber(label.lineHeight, 18),
      maxLines: Math.max(0, Math.floor(sceneNumber(label.maxLines, 0))),
      overflow: normalizeTextLayoutOverflow(label.overflow),
      maxWidth: sceneNumber(label.maxWidth, 180),
    }, layout, {
      role: "label",
      surface: "scene3d",
      state: "ready",
      align: align,
      revision: gosxTextLayoutRevision(),
    });

    setStyleValue(element.style, "--gosx-scene-label-left", box.anchor.x + "px");
    setStyleValue(element.style, "--gosx-scene-label-top", box.anchor.y + "px");
    setStyleValue(element.style, "--gosx-scene-label-anchor-x", String(sceneNumber(label.anchorX, 0.5)));
    setStyleValue(element.style, "--gosx-scene-label-anchor-y", String(sceneNumber(label.anchorY, 1)));
    setStyleValue(element.style, "--gosx-scene-label-width", metrics.totalWidth + "px");
    setStyleValue(element.style, "--gosx-scene-label-max-width", metrics.maxTotalWidth + "px");
    setStyleValue(element.style, "--gosx-scene-label-height", metrics.totalHeight + "px");
    setStyleValue(element.style, "--gosx-scene-label-line-height", sceneNumber(label.lineHeight, 18) + "px");
    setStyleValue(element.style, "--gosx-scene-label-align", align);
    setStyleValue(element.style, "--gosx-scene-label-white-space", whiteSpace);
    setStyleValue(element.style, "--gosx-scene-label-font", label.font || '600 13px "IBM Plex Sans", "Segoe UI", sans-serif');
    setStyleValue(element.style, "--gosx-scene-label-color", label.color || "#ecf7ff");
    setStyleValue(element.style, "--gosx-scene-label-background", label.background || "rgba(8, 21, 31, 0.82)");
    setStyleValue(element.style, "--gosx-scene-label-border-color", label.borderColor || "rgba(141, 225, 255, 0.24)");
    setStyleValue(element.style, "--gosx-scene-label-z-index", String(zIndex));
    setStyleValue(element.style, "--gosx-scene-label-depth", String(sceneNumber(label.depth, 0)));
    element.__gosxTextLayout = layout;

    if (element.__gosxLayoutKey === layoutKey) {
      return;
    }

    clearChildren(element);
    const lines = Array.isArray(layout.lines) && layout.lines.length > 0 ? layout.lines : [{ text: label.text }];
    for (const line of lines) {
      const lineElement = document.createElement("div");
      lineElement.setAttribute("data-gosx-scene-label-line", "");
      lineElement.textContent = line && typeof line.text === "string" && line.text !== "" ? line.text : "\u00a0";
      if (whiteSpace !== "normal") {
        lineElement.style.whiteSpace = whiteSpace;
      }
      element.appendChild(lineElement);
    }
    element.__gosxLayoutKey = layoutKey;
  }

  function renderSceneLabels(layer, bundle, layoutCache, elements, width, height) {
    if (!layer) {
      return;
    }

    const labels = prepareSceneLabelEntries(bundle, layoutCache, width, height);
    const active = new Set();

    for (const entry of labels) {
      const id = entry.id;
      active.add(id);
      let element = elements.get(id);
      if (!element) {
        element = document.createElement("div");
        layer.appendChild(element);
        elements.set(id, element);
      }
      renderSceneLabelElement(element, entry.label, entry.layoutKey, entry.layout, entry.metrics, entry.box, entry.hidden, entry.occluded);
    }

    for (const [id, element] of elements.entries()) {
      if (active.has(id)) {
        continue;
      }
      if (element.parentNode === layer) {
        layer.removeChild(element);
      }
      elements.delete(id);
    }
  }

  function renderSceneSpriteElement(element, sprite, box, hidden, occluded) {
    const zIndex = Math.max(1, 1000 + Math.round(sceneNumber(sprite.priority, 0) * 10) - Math.round(sceneNumber(sprite.depth, 0) * 10));
    element.setAttribute("data-gosx-scene-sprite", sprite.id || "");
    setAttrValue(element, "class", sprite.className ? ("gosx-scene-sprite " + sprite.className) : "gosx-scene-sprite");
    setAttrValue(element, "data-gosx-scene-sprite-fit", normalizeSceneSpriteFit(sprite.fit));
    setAttrValue(element, "data-gosx-scene-sprite-occlude", sprite.occlude ? "true" : "false");
    setAttrValue(element, "data-gosx-scene-sprite-occluded", occluded ? "true" : "false");
    setAttrValue(element, "data-gosx-scene-sprite-visibility", hidden ? "hidden" : "visible");
    setAttrValue(element, "data-gosx-scene-sprite-priority", sceneNumber(sprite.priority, 0));
    setAttrValue(element, "data-gosx-scene-sprite-depth", sceneNumber(sprite.depth, 0));
    setStyleValue(element.style, "--gosx-scene-sprite-left", box.anchor.x + "px");
    setStyleValue(element.style, "--gosx-scene-sprite-top", box.anchor.y + "px");
    setStyleValue(element.style, "--gosx-scene-sprite-anchor-x", String(sceneNumber(sprite.anchorX, 0.5)));
    setStyleValue(element.style, "--gosx-scene-sprite-anchor-y", String(sceneNumber(sprite.anchorY, 0.5)));
    setStyleValue(element.style, "--gosx-scene-sprite-width", Math.max(1, sceneNumber(sprite.width, 1)) + "px");
    setStyleValue(element.style, "--gosx-scene-sprite-height", Math.max(1, sceneNumber(sprite.height, 1)) + "px");
    setStyleValue(element.style, "--gosx-scene-sprite-opacity", String(clamp01(sceneNumber(sprite.opacity, 1))));
    setStyleValue(element.style, "--gosx-scene-sprite-fit", normalizeSceneSpriteFit(sprite.fit));
    setStyleValue(element.style, "--gosx-scene-sprite-z-index", String(zIndex));
    setStyleValue(element.style, "--gosx-scene-sprite-depth", String(sceneNumber(sprite.depth, 0)));

    let image = element.firstChild;
    if (!image || image.tagName !== "IMG") {
      clearChildren(element);
      image = document.createElement("img");
      image.setAttribute("draggable", "false");
      image.setAttribute("alt", "");
      image.setAttribute("aria-hidden", "true");
      element.appendChild(image);
    }
    setAttrValue(image, "src", sprite.src || "");
    setStyleValue(image.style, "objectFit", normalizeSceneSpriteFit(sprite.fit) === "fill" ? "fill" : normalizeSceneSpriteFit(sprite.fit));
  }

  function renderSceneSprites(layer, bundle, elements, width, height) {
    if (!layer) {
      return;
    }

    const sprites = prepareSceneSpriteEntries(bundle, width, height);
    const active = new Set();
    for (const entry of sprites) {
      const id = entry.id;
      active.add(id);
      let element = elements.get(id);
      if (!element) {
        element = document.createElement("div");
        layer.appendChild(element);
        elements.set(id, element);
      }
      renderSceneSpriteElement(element, entry.sprite, entry.box, entry.hidden, entry.occluded);
    }
    for (const [id, element] of elements.entries()) {
      if (active.has(id)) {
        continue;
      }
      if (element.parentNode === layer) {
        layer.removeChild(element);
      }
      elements.delete(id);
    }
  }

  function normalizeSceneControlsMode(value) {
    switch (String(value || "").trim().toLowerCase()) {
      case "orbit":
        return "orbit";
      default:
        return "";
    }
  }

  function sceneControlsTarget(props) {
    const raw = props && props.controlTarget && typeof props.controlTarget === "object" ? props.controlTarget : null;
    return {
      x: sceneNumber(raw && raw.x, 0),
      y: sceneNumber(raw && raw.y, 0),
      z: sceneNumber(raw && raw.z, 0),
    };
  }

  function sceneControlsRotateSpeed(props) {
    return Math.max(0.1, sceneNumber(props && props.controlRotateSpeed, 1));
  }

  function sceneControlsZoomSpeed(props) {
    return Math.max(0.05, sceneNumber(props && props.controlZoomSpeed, 1));
  }

  function sceneWorldCameraPosition(camera) {
    const normalized = sceneRenderCamera(camera);
    return {
      x: normalized.x,
      y: normalized.y,
      z: -normalized.z,
    };
  }

  function sceneOrbitStateFromCamera(camera, target) {
    const normalized = sceneRenderCamera(camera);
    const worldPosition = sceneWorldCameraPosition(normalized);
    const orbitTarget = target || { x: 0, y: 0, z: 0 };
    const offsetX = worldPosition.x - sceneNumber(orbitTarget.x, 0);
    const offsetY = worldPosition.y - sceneNumber(orbitTarget.y, 0);
    const offsetZ = worldPosition.z - sceneNumber(orbitTarget.z, 0);
    const radius = Math.max(0.6, Math.hypot(offsetX, offsetY, offsetZ));
    return {
      target: {
        x: sceneNumber(orbitTarget.x, 0),
        y: sceneNumber(orbitTarget.y, 0),
        z: sceneNumber(orbitTarget.z, 0),
      },
      radius,
      yaw: Math.atan2(offsetX, -offsetZ),
      pitch: Math.asin(sceneClamp(offsetY / radius, -0.98, 0.98)),
      fov: normalized.fov,
      near: normalized.near,
      far: normalized.far,
    };
  }

  function sceneOrbitCamera(state, fallbackCamera) {
    const base = sceneRenderCamera(fallbackCamera);
    const orbit = state || sceneOrbitStateFromCamera(base, { x: 0, y: 0, z: 0 });
    const radius = Math.max(0.6, sceneNumber(orbit.radius, 6));
    const pitch = sceneClamp(sceneNumber(orbit.pitch, 0), -1.4, 1.4);
    const yaw = sceneNumber(orbit.yaw, 0);
    const target = orbit.target || { x: 0, y: 0, z: 0 };
    const cosPitch = Math.cos(pitch);
    const worldPosition = {
      x: sceneNumber(target.x, 0) + Math.sin(yaw) * cosPitch * radius,
      y: sceneNumber(target.y, 0) + Math.sin(pitch) * radius,
      z: sceneNumber(target.z, 0) - Math.cos(yaw) * cosPitch * radius,
    };
    const forward = {
      x: sceneNumber(target.x, 0) - worldPosition.x,
      y: sceneNumber(target.y, 0) - worldPosition.y,
      z: sceneNumber(target.z, 0) - worldPosition.z,
    };
    const horizontal = Math.max(0.0001, Math.hypot(forward.x, forward.z));
    return {
      x: worldPosition.x,
      y: worldPosition.y,
      z: -worldPosition.z,
      rotationX: -Math.atan2(forward.y, horizontal),
      rotationY: Math.atan2(forward.x, forward.z),
      rotationZ: 0,
      fov: sceneNumber(orbit.fov, base.fov),
      near: sceneNumber(orbit.near, base.near),
      far: sceneNumber(orbit.far, base.far),
    };
  }

  function createSceneControls(props) {
    const mode = normalizeSceneControlsMode(props && props.controls);
    if (!mode) {
      return null;
    }
    return {
      mode,
      active: false,
      touched: false,
      pointerId: null,
      lastX: 0,
      lastY: 0,
      rotateSpeed: sceneControlsRotateSpeed(props),
      zoomSpeed: sceneControlsZoomSpeed(props),
      orbit: null,
      target: sceneControlsTarget(props),
    };
  }

  function syncSceneControlsFromCamera(controls, camera) {
    if (!controls || controls.mode !== "orbit" || controls.active || controls.touched) {
      return;
    }
    controls.orbit = sceneOrbitStateFromCamera(camera, controls.target);
  }

  function sceneScrollViewportHeight() {
    const scrollingElement = document.scrollingElement || document.documentElement || document.body;
    const visualViewport = window.visualViewport;
    return Math.max(1, sceneNumber(
      visualViewport && visualViewport.height,
      sceneNumber(window.innerHeight, sceneNumber(scrollingElement && scrollingElement.clientHeight, 0)),
    ));
  }

  function sceneScrollTop() {
    const scrollingElement = document.scrollingElement || document.documentElement || document.body;
    const visualViewport = window.visualViewport;
    const visualViewportTop = sceneNumber(visualViewport && visualViewport.pageTop, NaN);
    if (Number.isFinite(visualViewportTop)) {
      return Math.max(0, visualViewportTop);
    }
    return Math.max(0, sceneNumber(
      window.scrollY,
      sceneNumber(window.pageYOffset, sceneNumber(scrollingElement && scrollingElement.scrollTop, 0)),
    ));
  }

  function sceneScrollMax() {
    const scrollingElement = document.scrollingElement || document.documentElement || document.body;
    const scrollHeight = Math.max(
      sceneNumber(scrollingElement && scrollingElement.scrollHeight, 0),
      sceneNumber(document.documentElement && document.documentElement.scrollHeight, 0),
      sceneNumber(document.body && document.body.scrollHeight, 0),
    );
    return Math.max(1, scrollHeight - sceneScrollViewportHeight());
  }

  function sceneAdvanceScrollCamera(scrollCamera) {
    if (!scrollCamera || scrollCamera.start === scrollCamera.end) {
      return;
    }
    scrollCamera._progress = Math.pow(Math.min(1, Math.max(0, sceneScrollTop() / sceneScrollMax())), 0.5);
    var target = scrollCamera._progress || 0;
    var current = sceneNumber(scrollCamera._smoothProgress, target);
    if (Math.abs(target - current) < 0.0005) {
      current = target;
    } else {
      current += (target - current) * 0.08;
    }
    scrollCamera._smoothProgress = current;
  }

  function sceneCurrentControlCamera(controls, sourceCamera, scrollCamera) {
    var cam;
    if (controls && controls.mode === "orbit") {
      syncSceneControlsFromCamera(controls, sourceCamera);
      cam = controls.orbit ? sceneOrbitCamera(controls.orbit, sourceCamera) : sceneRenderCamera(sourceCamera);
    } else {
      cam = sceneRenderCamera(sourceCamera);
    }
    if (scrollCamera && scrollCamera.start !== scrollCamera.end) {
      var progress = sceneNumber(scrollCamera._smoothProgress, sceneNumber(scrollCamera._progress, 0));
      cam.z = scrollCamera.start + progress * (scrollCamera.end - scrollCamera.start);
    }
    return cam;
  }

  function sceneBundleWithCameraOverride(bundle, camera) {
    if (!bundle) {
      return bundle;
    }
    const targetCamera = sceneRenderCamera(camera);
    const sourceCamera = sceneRenderCamera(bundle.camera);
    if (sceneCameraEquivalent(targetCamera, sourceCamera)) {
      return bundle;
    }
    return Object.assign({}, bundle, {
      camera: targetCamera,
      sourceCamera: sourceCamera,
    });
  }

  function sceneControlsMetrics(readViewport, props) {
    const viewport = typeof readViewport === "function" ? readViewport() : null;
    return {
      width: Math.max(1, sceneViewportValue(viewport, "cssWidth", sceneNumber(props && props.width, 720))),
      height: Math.max(1, sceneViewportValue(viewport, "cssHeight", sceneNumber(props && props.height, 420))),
    };
  }

  function syncSceneControlsFromSource(controls, readSourceCamera) {
    const sourceCamera = typeof readSourceCamera === "function" ? readSourceCamera() : null;
    syncSceneControlsFromCamera(controls, sourceCamera);
  }

  function sceneOrbitStartDrag(controls, canvas, props, readViewport, readSourceCamera, attachDocumentListeners, event) {
    if (controls.active || !scenePointerCanStartDrag(controls, event)) {
      return;
    }
    syncSceneControlsFromSource(controls, readSourceCamera);
    controls.active = true;
    controls.touched = true;
    controls.pointerId = event.pointerId;
    const metrics = sceneControlsMetrics(readViewport, props);
    const point = sceneLocalPointerPoint(event, canvas, metrics.width, metrics.height);
    controls.lastX = point.x;
    controls.lastY = point.y;
    canvas.style.cursor = "grabbing";
    attachDocumentListeners();
    if (typeof canvas.setPointerCapture === "function" && event.pointerId != null) {
      canvas.setPointerCapture(event.pointerId);
    }
    if (typeof event.preventDefault === "function") {
      event.preventDefault();
    }
    if (typeof event.stopPropagation === "function") {
      event.stopPropagation();
    }
  }

  function sceneOrbitMoveDrag(controls, canvas, props, readViewport, readSourceCamera, scheduleRender, event) {
    if (!sceneDragMatchesActivePointer(controls, event)) {
      return;
    }
    const metrics = sceneControlsMetrics(readViewport, props);
    const sample = sceneLocalPointerSample(event, canvas, metrics.width, metrics.height, controls, "move");
    if (!controls.orbit) {
      syncSceneControlsFromSource(controls, readSourceCamera);
    }
    controls.orbit.yaw += (sample.deltaX / Math.max(metrics.width, 1)) * Math.PI * controls.rotateSpeed;
    controls.orbit.pitch = sceneClamp(
      controls.orbit.pitch + (sample.deltaY / Math.max(metrics.height, 1)) * Math.PI * controls.rotateSpeed,
      -1.4,
      1.4,
    );
    if (typeof event.preventDefault === "function") {
      event.preventDefault();
    }
    if (typeof event.stopPropagation === "function") {
      event.stopPropagation();
    }
    scheduleRender("controls");
  }

  function sceneOrbitFinishDrag(controls, canvas, detachDocumentListeners, event) {
    if (!sceneDragMatchesActivePointer(controls, event)) {
      return;
    }
    const pointerId = controls.pointerId;
    controls.active = false;
    controls.pointerId = null;
    canvas.style.cursor = "grab";
    detachDocumentListeners();
    if (pointerId != null && typeof canvas.releasePointerCapture === "function") {
      try {
        canvas.releasePointerCapture(pointerId);
      } catch (_error) {}
    }
    if (event && typeof event.preventDefault === "function") {
      event.preventDefault();
    }
    if (event && typeof event.stopPropagation === "function") {
      event.stopPropagation();
    }
  }

  function sceneOrbitApplyWheel(controls, readSourceCamera, scheduleRender, event) {
    syncSceneControlsFromSource(controls, readSourceCamera);
    controls.touched = true;
    controls.orbit.radius = sceneClamp(
      controls.orbit.radius * Math.exp(sceneNumber(event && event.deltaY, 0) * 0.001 * controls.zoomSpeed),
      0.6,
      256,
    );
    if (event && typeof event.preventDefault === "function") {
      event.preventDefault();
    }
    if (event && typeof event.stopPropagation === "function") {
      event.stopPropagation();
    }
    scheduleRender("controls");
  }

  function setupSceneBuiltInControls(canvas, props, readViewport, readSourceCamera, scheduleRender) {
    const controls = createSceneControls(props);
    if (!canvas || !controls || controls.mode !== "orbit") {
      return {
        controller: controls,
        dispose() {},
      };
    }

    let documentListenersAttached = false;
    canvas.style.cursor = "grab";
    canvas.style.touchAction = "none";

    function attachDocumentListeners() {
      if (documentListenersAttached) {
        return;
      }
      documentListenersAttached = true;
      document.addEventListener("pointermove", onPointerMove);
      document.addEventListener("pointerup", finishPointerDrag);
      document.addEventListener("pointercancel", finishPointerDrag);
    }

    function detachDocumentListeners() {
      if (!documentListenersAttached) {
        return;
      }
      documentListenersAttached = false;
      document.removeEventListener("pointermove", onPointerMove);
      document.removeEventListener("pointerup", finishPointerDrag);
      document.removeEventListener("pointercancel", finishPointerDrag);
    }

    function onPointerDown(event) {
      sceneOrbitStartDrag(controls, canvas, props, readViewport, readSourceCamera, attachDocumentListeners, event);
    }

    function onPointerMove(event) {
      sceneOrbitMoveDrag(controls, canvas, props, readViewport, readSourceCamera, scheduleRender, event);
    }

    function finishPointerDrag(event) {
      sceneOrbitFinishDrag(controls, canvas, detachDocumentListeners, event);
    }

    function onWheel(event) {
      sceneOrbitApplyWheel(controls, readSourceCamera, scheduleRender, event);
    }

    canvas.addEventListener("pointerdown", onPointerDown);
    canvas.addEventListener("pointermove", onPointerMove);
    canvas.addEventListener("pointerup", finishPointerDrag);
    canvas.addEventListener("pointercancel", finishPointerDrag);
    canvas.addEventListener("lostpointercapture", finishPointerDrag);
    canvas.addEventListener("wheel", onWheel);

    return {
      controller: controls,
      dispose() {
        detachDocumentListeners();
        canvas.removeEventListener("pointerdown", onPointerDown);
        canvas.removeEventListener("pointermove", onPointerMove);
        canvas.removeEventListener("pointerup", finishPointerDrag);
        canvas.removeEventListener("pointercancel", finishPointerDrag);
        canvas.removeEventListener("lostpointercapture", finishPointerDrag);
        canvas.removeEventListener("wheel", onWheel);
      },
    };
  }

  window.__gosx_register_engine_factory("GoSXScene3D", async function(ctx) {
    if (!ctx.mount || typeof document.createElement !== "function") {
      console.warn("[gosx] Scene3D requires a mount element");
      return {};
    }

    const props = ctx.props || {};
    const capability = sceneCapabilityProfile(props);
    const viewportBase = sceneViewportBase(props);
    const sceneState = createSceneState(props);
    const sceneModelHydration = hydrateSceneStateModels(sceneState, props);
    const runtimeScene = ctx.runtimeMode === "shared" && Boolean(ctx.programRef);
    const lifecycle = initialSceneLifecycleState();
    const motion = initialSceneMotionState(props);

    function sceneShouldAnimate() {
      if (motion.reducedMotion) {
        return false;
      }
      if (sceneHasActiveTransitions(sceneState)) {
        return true;
      }
      if (runtimeScene || sceneBool(props.autoRotate, true)) {
        return true;
      }
      if (Array.isArray(sceneState.computeParticles) && sceneState.computeParticles.length > 0) {
        return true;
      }
      if (Array.isArray(sceneState.points) && sceneState.points.some(function(p) {
        return sceneNumber(p.spinX, 0) !== 0 || sceneNumber(p.spinY, 0) !== 0 || sceneNumber(p.spinZ, 0) !== 0;
      })) {
        return true;
      }
      return sceneStateObjects(sceneState).some(sceneObjectAnimated) ||
        sceneStateLabels(sceneState).some(sceneLabelAnimated) ||
        sceneStateSprites(sceneState).some(sceneSpriteAnimated);
    }

    clearChildren(ctx.mount);
    ctx.mount.setAttribute("data-gosx-scene3d-mounted", "true");
    ctx.mount.setAttribute("aria-label", props.ariaLabel || props.label || "Interactive GoSX 3D scene");
    setAttrValue(ctx.mount, "data-gosx-scene3d-controls", normalizeSceneControlsMode(props.controls));
    setAttrValue(ctx.mount, "data-gosx-scene3d-pick-signals", scenePickSignalNamespace(props));
    setAttrValue(ctx.mount, "data-gosx-scene3d-event-signals", sceneEventSignalNamespace(props));
    applySceneCapabilityState(ctx.mount, props, capability);
    if (!ctx.mount.style.position) {
      ctx.mount.style.position = "relative";
    }
    const canvas = document.createElement("canvas");
    canvas.setAttribute("data-gosx-scene3d-canvas", "true");
    canvas.setAttribute("role", "img");
    canvas.setAttribute("aria-label", props.label || "Interactive GoSX 3D scene");
    canvas.style.maxWidth = "100%";
    canvas.style.borderRadius = "inherit";
    canvas.width = viewportBase.baseWidth;
    canvas.height = viewportBase.baseHeight;
    canvas.setAttribute("width", String(viewportBase.baseWidth));
    canvas.setAttribute("height", String(viewportBase.baseHeight));
    ctx.mount.appendChild(canvas);

    const labelLayer = document.createElement("div");
    labelLayer.setAttribute("data-gosx-scene3d-label-layer", "true");
    labelLayer.setAttribute("aria-hidden", "true");
    ctx.mount.appendChild(labelLayer);

    let viewport = applySceneViewport(ctx.mount, canvas, labelLayer, sceneViewportFromMount(ctx.mount, props, viewportBase, canvas, capability), viewportBase);

    const initialRenderer = createSceneRenderer(canvas, props, capability);
    if (!initialRenderer || !initialRenderer.renderer) {
      console.warn("[gosx] Scene3D could not acquire a renderer");
      return {
        dispose() {
          if (canvas.parentNode === ctx.mount) {
            ctx.mount.removeChild(canvas);
          }
          if (labelLayer.parentNode === ctx.mount) {
            ctx.mount.removeChild(labelLayer);
          }
        },
      };
    }
    let renderer = initialRenderer.renderer;
    applySceneRendererState(ctx.mount, renderer, initialRenderer.fallbackReason || "");
    let latestBundle = null;
    const labelLayoutCache = new Map();
    const labelElements = new Map();
    const spriteElements = new Map();
    let labelRefreshHandle = null;
    const releaseTextLayoutListener = onTextLayoutInvalidated(function() {
      if (disposed || !latestBundle || !sceneCanRender()) {
        return;
      }
      if (labelRefreshHandle != null) {
        return;
      }
      labelRefreshHandle = engineFrame(function() {
        labelRefreshHandle = null;
        if (disposed || !latestBundle) {
          return;
        }
        renderSceneLabels(labelLayer, latestBundle, labelLayoutCache, labelElements, viewport.cssWidth, viewport.cssHeight);
        renderSceneSprites(labelLayer, latestBundle, spriteElements, viewport.cssWidth, viewport.cssHeight);
      });
    });

    let frameHandle = null;
    let scheduledRenderHandle = null;
    let disposed = false;

    function swapRenderer(nextRenderer, fallbackReason) {
      if (!nextRenderer) {
        return false;
      }
      const previous = renderer;
      renderer = nextRenderer;
      applySceneRendererState(ctx.mount, renderer, fallbackReason);
      if (previous && previous !== renderer && typeof previous.dispose === "function") {
        previous.dispose();
      }
      return true;
    }

    function fallbackSceneRenderer(reason) {
      const ctx2d = typeof canvas.getContext === "function" ? canvas.getContext("2d") : null;
      if (!ctx2d) {
        return false;
      }
      return swapRenderer(createSceneCanvasRenderer(ctx2d, canvas), reason || "webgl-unavailable");
    }

    function restoreSceneWebGLRenderer(reason) {
      const webglPreference = sceneCapabilityWebGLPreference(props, capability);
      if (!(webglPreference === "prefer" || webglPreference === "force")) {
        return false;
      }
      const webglRenderer = createSceneWebGLRenderer(canvas, {
        antialias: capability.tier === "full" && !capability.lowPower && !capability.reducedData,
        powerPreference: capability.lowPower || capability.tier === "constrained" ? "low-power" : "high-performance",
      });
      if (!webglRenderer) {
        return false;
      }
      return swapRenderer(webglRenderer, reason || "");
    }

    function onWebGLContextLost(event) {
      if (!renderer || renderer.kind !== "webgl") {
        return;
      }
      if (event && typeof event.preventDefault === "function") {
        event.preventDefault();
      }
      fallbackSceneRenderer("webgl-context-lost");
      scheduleRender("webgl-context-lost");
    }

    function onWebGLContextRestored() {
      if (restoreSceneWebGLRenderer("")) {
        scheduleRender("webgl-context-restored");
      }
    }

    canvas.addEventListener("webglcontextlost", onWebGLContextLost);
    canvas.addEventListener("webglcontextrestored", onWebGLContextRestored);

    function sceneCanRender() {
      return lifecycle.pageVisible && lifecycle.inViewport;
    }

    function sceneWantsAnimation() {
      return sceneShouldAnimate() && sceneCanRender();
    }

    function cancelFrame() {
      if (frameHandle != null) {
        cancelEngineFrame(frameHandle);
        frameHandle = null;
      }
    }

    function cancelScheduledRender() {
      if (scheduledRenderHandle != null) {
        cancelEngineFrame(scheduledRenderHandle);
        scheduledRenderHandle = null;
      }
    }

    function scheduleRender(reason) {
      if (disposed) {
        return;
      }
      if (scheduledRenderHandle != null) {
        return;
      }
      scheduledRenderHandle = engineFrame(function(now) {
        scheduledRenderHandle = null;
        if (disposed) {
          return;
        }
        const nextViewport = sceneViewportFromMount(ctx.mount, props, viewportBase, canvas, capability);
        if (sceneViewportChanged(viewport, nextViewport)) {
          viewport = applySceneViewport(ctx.mount, canvas, labelLayer, nextViewport, viewportBase);
        }
        if (!sceneCanRender()) {
          cancelFrame();
          return;
        }
        renderFrame(typeof now === "number" ? now : 0, reason || "refresh");
      });
    }

    function readSceneSourceCamera() {
      if (latestBundle && latestBundle.sourceCamera) {
        return latestBundle.sourceCamera;
      }
      if (latestBundle && latestBundle.camera) {
        return latestBundle.camera;
      }
      return sceneState.camera;
    }

    const sceneControlHandle = setupSceneBuiltInControls(canvas, props, function() {
      return viewport;
    }, readSceneSourceCamera, scheduleRender);
    const dragHandle = sceneControlHandle.controller
      ? { dispose() {} }
      : setupSceneDragInteractions(canvas, props, function() {
        return viewport;
      }, function() {
        return latestBundle;
      });
    const pickHandle = setupScenePickInteractions(canvas, props, function() {
      return viewport;
    }, function() {
      return latestBundle;
    }, function(detail) {
      ctx.emit("scene-interaction", detail);
    });
    const sceneHubListener = function(event) {
      if (disposed) {
        return;
      }
      const detail = event && event.detail && typeof event.detail === "object" ? event.detail : null;
      if (!detail || typeof detail.event !== "string") {
        return;
      }
      if (sceneApplyLiveEvent(sceneState, detail.event, detail.data, motion.reducedMotion, sceneNowMilliseconds())) {
        scheduleRender("hub-event");
      }
    };
    document.addEventListener("gosx:hub:event", sceneHubListener);

    const releaseViewportObserver = observeSceneViewport(ctx.mount, scheduleRender);
    const releaseCapabilityObserver = observeSceneCapability(ctx.mount, props, capability, function(reason) {
      const nextViewport = sceneViewportFromMount(ctx.mount, props, viewportBase, canvas, capability);
      if (sceneViewportChanged(viewport, nextViewport)) {
        viewport = applySceneViewport(ctx.mount, canvas, labelLayer, nextViewport, viewportBase);
      }
      const desiredFallback = sceneRendererFallbackReason(props, capability, renderer && renderer.kind);
      const webglPreference = sceneCapabilityWebGLPreference(props, capability);
      if (renderer && renderer.kind === "webgl" && !(webglPreference === "prefer" || webglPreference === "force")) {
        fallbackSceneRenderer(desiredFallback || "environment-constrained");
      } else if (renderer && renderer.kind !== "webgl" && (webglPreference === "prefer" || webglPreference === "force")) {
        if (!restoreSceneWebGLRenderer("")) {
          applySceneRendererState(ctx.mount, renderer, desiredFallback);
        }
      } else {
        applySceneRendererState(ctx.mount, renderer, desiredFallback);
      }
      scheduleRender(reason || "capability");
    });
    const releaseLifecycleObserver = observeSceneLifecycle(ctx.mount, lifecycle, function(reason) {
      if (!sceneCanRender()) {
        cancelFrame();
        cancelScheduledRender();
        if (labelRefreshHandle != null) {
          cancelEngineFrame(labelRefreshHandle);
          labelRefreshHandle = null;
        }
        return;
      }
      scheduleRender(reason || "lifecycle");
    });
    const releaseMotionObserver = observeSceneMotion(ctx.mount, motion, function(reason) {
      cancelFrame();
      cancelScheduledRender();
      scheduleRender(reason || "motion");
    });

    if (runtimeScene) {
      if (ctx.runtime && ctx.runtime.available()) {
        applySceneCommands(sceneState, await ctx.runtime.hydrateFromProgramRef());
      } else {
        console.warn("[gosx] Scene3D runtime requested but shared engine runtime is unavailable");
      }
    }

    function renderFrame(now) {
      if (disposed) return;
      viewport = applySceneViewport(ctx.mount, canvas, labelLayer, sceneViewportFromMount(ctx.mount, props, viewportBase, canvas, capability), viewportBase);
      if (!sceneCanRender()) {
        cancelFrame();
        return;
      }
      sceneAdvanceScrollCamera(sceneState._scrollCamera);
      const timeSeconds = now / 1000;
      if (runtimeScene && ctx.runtime && typeof ctx.runtime.renderFrame === "function") {
        const runtimeBundle = ctx.runtime.renderFrame(timeSeconds, viewport.cssWidth, viewport.cssHeight);
        if (runtimeBundle) {
          const effectiveBundle = sceneBundleWithCameraOverride(
            runtimeBundle,
            sceneCurrentControlCamera(sceneControlHandle.controller, runtimeBundle.camera || sceneState.camera, sceneState._scrollCamera),
          );
          latestBundle = effectiveBundle;
          renderer.render(effectiveBundle, viewport);
          renderSceneLabels(labelLayer, effectiveBundle, labelLayoutCache, labelElements, viewport.cssWidth, viewport.cssHeight);
          renderSceneSprites(labelLayer, effectiveBundle, spriteElements, viewport.cssWidth, viewport.cssHeight);
          if (sceneWantsAnimation()) {
            frameHandle = engineFrame(renderFrame);
          }
          return;
        }
      }
      if (runtimeScene && ctx.runtime) {
        applySceneCommands(sceneState, ctx.runtime.tick());
      }
      sceneAdvanceTransitions(sceneState, now);
      if (typeof sceneApplyLOD === "function" && props.compression && props.compression.lod) {
        var cam = sceneCurrentControlCamera(sceneControlHandle.controller, sceneState.camera, sceneState._scrollCamera);
        var camX = cam.x || 0, camY = cam.y || 0, camZ = cam.z || 0;
        for (var li = 0; li < sceneState.points.length; li++) {
          sceneApplyLOD(sceneState.points[li], camX, camY, camZ);
        }
      }
      latestBundle = createSceneRenderBundle(
        viewport.cssWidth,
        viewport.cssHeight,
        sceneState.background,
        sceneCurrentControlCamera(sceneControlHandle.controller, sceneState.camera, sceneState._scrollCamera),
        sceneStateObjects(sceneState),
        sceneStateLabels(sceneState),
        sceneStateSprites(sceneState),
        sceneStateLights(sceneState),
        sceneState.environment,
        timeSeconds,
        sceneState.points,
        sceneState.instancedMeshes,
        sceneState.computeParticles,
      );
      renderer.render(latestBundle, viewport);
      renderSceneLabels(labelLayer, latestBundle, labelLayoutCache, labelElements, viewport.cssWidth, viewport.cssHeight);
      renderSceneSprites(labelLayer, latestBundle, spriteElements, viewport.cssWidth, viewport.cssHeight);
      if (sceneWantsAnimation()) {
        frameHandle = engineFrame(renderFrame);
      }
    }

    await sceneModelHydration;
    scenePrimeInitialTransitions(sceneState, motion.reducedMotion, 0);

    function scheduleInitialRender() {
      if (disposed) return;
      if (typeof scheduler !== "undefined" && scheduler && typeof scheduler.postTask === "function") {
        scheduler.postTask(function() { if (!disposed) renderFrame(0); }, { priority: "user-visible" });
        return;
      }
      if (typeof requestAnimationFrame === "function") {
        requestAnimationFrame(function() { if (!disposed) renderFrame(0); });
        return;
      }
      setTimeout(function() { if (!disposed) renderFrame(0); }, 0);
    }
    scheduleInitialRender();

    if (typeof sceneUpgradeProgressive === "function" && props.compression && props.compression.progressive) {
      var upgradeTimer = typeof requestIdleCallback === "function" ? requestIdleCallback : setTimeout;
      upgradeTimer(function() {
        sceneUpgradeProgressive(props);
        if (sceneWantsAnimation()) {
        } else {
          renderFrame(0);
        }
      });
    }

    ctx.emit("mounted", {
      width: viewport.cssWidth,
      height: viewport.cssHeight,
      objects: sceneStateObjects(sceneState).length,
      labels: sceneStateLabels(sceneState).length,
      sprites: sceneStateSprites(sceneState).length,
      lights: sceneStateLights(sceneState).length,
      models: sceneModels(props).length,
    });

    var scrollHandler = null;
    var visualViewportScrollHandler = null;
    if (sceneState._scrollCamera) {
      sceneState._scrollCamera._progress = 0;
      sceneState._scrollCamera._smoothProgress = 0;
      scrollHandler = function() {
        if (!sceneWantsAnimation()) {
          scheduleRender("scroll");
        }
      };
      window.addEventListener("scroll", scrollHandler, { passive: true });
      var isTouchDevice =
        (typeof navigator !== "undefined" && navigator.maxTouchPoints > 0) ||
        ("ontouchstart" in window);
      if (
        isTouchDevice &&
        window.visualViewport &&
        typeof window.visualViewport.addEventListener === "function"
      ) {
        visualViewportScrollHandler = function() {
          if (!sceneWantsAnimation()) {
            scheduleRender("visual-viewport");
          }
        };
        window.visualViewport.addEventListener("scroll", visualViewportScrollHandler, { passive: true });
        window.visualViewport.addEventListener("resize", visualViewportScrollHandler, { passive: true });
      }
      sceneAdvanceScrollCamera(sceneState._scrollCamera);
    }

    return {
      applyCommands(commands) {
        applySceneCommands(sceneState, commands);
        scheduleRender("commands");
      },
      dispose() {
        disposed = true;
        if (scrollHandler) {
          window.removeEventListener("scroll", scrollHandler);
        }
        if (visualViewportScrollHandler && window.visualViewport && typeof window.visualViewport.removeEventListener === "function") {
          window.visualViewport.removeEventListener("scroll", visualViewportScrollHandler);
          window.visualViewport.removeEventListener("resize", visualViewportScrollHandler);
        }
        canvas.removeEventListener("webglcontextlost", onWebGLContextLost);
        canvas.removeEventListener("webglcontextrestored", onWebGLContextRestored);
        document.removeEventListener("gosx:hub:event", sceneHubListener);
        releaseViewportObserver();
        releaseCapabilityObserver();
        releaseLifecycleObserver();
        releaseMotionObserver();
        releaseTextLayoutListener();
        dragHandle.dispose();
        pickHandle.dispose();
        sceneControlHandle.dispose();
        renderer.dispose();
        cancelFrame();
        cancelScheduledRender();
        if (labelRefreshHandle != null) {
          cancelEngineFrame(labelRefreshHandle);
        }
        if (canvas.parentNode === ctx.mount) {
          ctx.mount.removeChild(canvas);
        }
        if (labelLayer.parentNode === ctx.mount) {
          ctx.mount.removeChild(labelLayer);
        }
      },
    };
  });

  window.__gosx_scene3d_available = true;
  if (typeof window.__gosx_scene3d_loaded === "function") {
    window.__gosx_scene3d_loaded();
  }
})();
//# sourceMappingURL=bootstrap-feature-scene3d.js.map
