  // Pending manifest reference, set during init, consumed when runtime is ready.
  let pendingManifest = null;

  function runtimeReady() {
    return (
      typeof window.__gosx_hydrate === "function" ||
      typeof window.__gosx_action === "function" ||
      typeof window.__gosx_set_shared_signal === "function"
    );
  }

  // --------------------------------------------------------------------------
  // Manifest loading
  // --------------------------------------------------------------------------

  // Parse the inline JSON manifest from #gosx-manifest script tag.
  // Returns the parsed object, or null if missing/malformed.
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

  // --------------------------------------------------------------------------
  // Shared WASM runtime loading
  // --------------------------------------------------------------------------

  // Load the single shared Go WASM binary referenced by the manifest runtime
  // entry. Uses Go's wasm_exec.js `Go` class. The WASM is expected to call
  // window.__gosx_runtime_ready() once it has finished initializing its
  // exported functions (__gosx_hydrate, __gosx_action, etc.).
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
      // go.run is intentionally not awaited — it resolves when the Go main()
      // exits, but the runtime stays alive via syscall/js callbacks.
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

  // --------------------------------------------------------------------------
  // Island program fetching
  // --------------------------------------------------------------------------

  // Fetch the compiled program data for a single island. Returns an
  // ArrayBuffer (for "wasm" format) or a string (for "json" or other text
  // formats). Returns null on failure.
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
      // Default: return as text (covers json, msgpack-base64, etc.)
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

    // Resolve the engine mount element from the entry.
    var mountID = entry && (entry.mountId || entry.id);
    mount = mountID ? document.getElementById(mountID) : null;
    if (!mount) {
      mount = document.body;
    }

    // Create transparent contenteditable for IME/keyboard activation.
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

    // beforeinput — text insertion, deletion, newline
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

    // Composition (IME)
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

    // Keyboard commands (arrows, shortcuts)
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

    // File drop
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

    // Mobile keyboard height. Passive: this handler only queues a signal
    // write — it never calls preventDefault. On iOS Safari a non-passive
    // visualViewport listener blocks the scroll thread during keyboard
    // show/hide animations, which manifests as jank and stale canvas
    // frames on the keyboard transition.
    if (window.visualViewport) {
      viewportListener = function() {
        var kh = window.innerHeight - window.visualViewport.height;
        queueInputSignal("$input.keyboard_height", Math.max(0, kh));
      };
      window.visualViewport.addEventListener("resize", viewportListener, { passive: true });
    }

    // Cursor position tracking for IME placement
    unsubCursorRect = gosxSubscribeSharedSignal("$editor.cursor_rect", function(rect) {
      if (!inputEl) return;
      var r = typeof rect === "string" ? JSON.parse(rect) : rect;
      if (r) {
        inputEl.style.left = (r.x || 0) + "px";
        inputEl.style.top = (r.y || 0) + "px";
        inputEl.style.height = (r.height || 20) + "px";
      }
    });

    // Clipboard content sync for copy/cut
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
      // lineWidth: 0 means "use renderer default" (1.8px on the canvas world
      // fallback). Non-zero values come from scene.LinesGeometry.Width on the
      // Go side and flow into per-segment width buffers at bundle build time.
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
    // Cache per-light content hash for scenePBRLightsHash dirty-tracking.
    // Paid here (once per mutation, rare) instead of per-frame inside
    // the hash function — ~13µs per call down to ~100ns in practice.
    normalized._lightHash = hashLightContent(normalized);
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
    // Cache env content hash for scenePBRLightsHash dirty-tracking.
    // Same rationale as _lightHash above — avoids re-walking fields on
    // every frame. sceneResolveLightingEnvironment rebuilds a new env
    // object per frame and must also stamp _envHash.
    environment._envHash = hashEnvironmentContent(environment);
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
    // Stamp _envHash for the manually-copied branch (normalizeSceneEnvironment
    // handles the other branch). sceneResolveLightingEnvironment runs per
    // bundle build, so the cost amortizes across the whole frame, not per
    // scenePBRUploadLights call.
    if (typeof base._envHash !== "number") {
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
    // Decompress any TurboQuant-compressed vertex data before the render loop.
    // Progressive mode: decompress preview now, schedule full-res upgrade after first frame.
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
      }
    }
    // If the patched target is a light or environment with a cached
    // content sub-hash, re-stamp it to reflect the mutated fields.
    // Without this, scenePBRLightsHash would read a stale _lightHash
    // (or _envHash) and miss content changes coming from transitions,
    // causing stale uniform state on screen.
    //
    // Duck-typed on the presence of the stamp rather than an explicit
    // kind check so this helper stays generic across light / env /
    // object transitions — only entries that opted into the stamped
    // fast path get re-stamped.
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







  // sceneRenderCamera normalizes a raw camera (partial, may be missing
  // fields) into a full PBR camera struct. Hot callers that want to avoid
  // the per-call allocation can pass an `out` scratch they own — the
  // function writes into it and returns it. Callers that don't care (or
  // need a fresh result for lifetime reasons) omit the second argument
  // and get a newly allocated object.
  //
  // The PBR render path (createScenePBRRenderer.render) uses the out-param
  // form with a renderer-scoped _frameCam scratch so each frame reuses
  // the same object in place — confirmed safe because no code path
  // between render() entry and the drawPBRObjectList/drawInstancedMeshes
  // reads calls sceneRenderCamera with a DIFFERENT camera object that
  // would clobber the scratch.
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



  // Dedicated camera scratch for sceneBoundsDepthMetrics. Separate from
  // _sceneProjectCameraScratch in 11-scene-math.js so back-to-back calls
  // from the bundle builder (projection + bounds) don't clobber each
  // other even though both are single-threaded.
  const _sceneBoundsDepthCameraScratch = {
    x: 0, y: 0, z: 0,
    rotationX: 0, rotationY: 0, rotationZ: 0,
    fov: 0, near: 0, far: 0,
  };

  // sceneBoundsDepthMetrics inlines the 8-corner depth computation that
  // used to go: sceneBoundsCorners (8 fresh point objects) →
  // sceneWorldPointDepth × 8 → sceneCameraLocalPoint → sceneInverseRotatePoint
  // (each allocating their own results). That chain was 16 allocations
  // per call. Now: 0 allocations for the 8-corner inner loop, plus one
  // fresh result object the caller retains.
  //
  // The inverse rotation math matches sceneInverseRotatePoint exactly so
  // the depth values are identical to the old path — bit-for-bit where
  // floating-point reordering isn't involved, within 1 ulp otherwise.
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

    // Iterate the 8 bounding-box corners by bit-coding (i & 1, i & 2, i & 4).
    for (let i = 0; i < 8; i += 1) {
      const worldX = (i & 4) ? maxX : minX;
      const worldY = (i & 2) ? maxY : minY;
      const worldZ = (i & 1) ? maxZ : minZ;

      // Translate into camera-local space.
      let lx = worldX - cam.x;
      let ly = worldY - cam.y;
      let lz = worldZ + cam.z;

      // Inverse rotate: apply -rotZ, then -rotY, then -rotX in that order.
      let nX = lx * cosZ - ly * sinZ;
      let nY = lx * sinZ + ly * cosZ;
      lx = nX;
      ly = nY;

      nX = lx * cosY + lz * sinY;
      let nZ = -lx * sinY + lz * cosY;
      lx = nX;
      lz = nZ;

      // Only lz matters for depth metrics — ly/lx discarded.
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
      // One entry per world-projected line segment, in lockstep with
      // worldPositions (which holds two vertices = 6 floats per segment).
      // The canvas 2D world renderer reads bundle.worldLineWidths[segmentIndex]
      // to honor LinesGeometry.Width. Absent values fall back to the runtime
      // default (1.8px) at the read site.
      worldLineWidths: [],
      // Parallel to worldLineWidths: per-segment render-pass index mapping
      // to the draw plan's pass buckets (0=opaque, 1=alpha, 2=additive).
      // The WebGL thick-line path honors per-pass blend/depth state by
      // emitting a separate drawElements call per non-empty pass. Legacy
      // gl.LINES path ignores this field because its per-pass separation
      // already happens in the draw plan.
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

  // translateScenePointInto is the alloc-free core of the scene-space
  // transform (scale → rotate → translate + drift offset). It writes the
  // result into a caller-provided `out` object, reads raw coordinates to
  // avoid an intermediate point object at the call site, and inlines the
  // rotation math so there's no sceneRotatePoint result allocation.
  //
  // Every caller in the tree uses a hoisted scratch `out` object (module-
  // level or above-loop), so translating a point at 60 fps costs zero
  // allocations after the initial scratch is wired up.
  function translateScenePointInto(out, px, py, pz, object, timeSeconds) {
    const scaleX = sceneNumber(object && object.scaleX, 1);
    const scaleY = sceneNumber(object && object.scaleY, 1);
    const scaleZ = sceneNumber(object && object.scaleZ, 1);
    let x = sceneNumber(px, 0) * scaleX;
    let y = sceneNumber(py, 0) * scaleY;
    let z = sceneNumber(pz, 0) * scaleZ;

    // Inlined XYZ Euler rotation (was sceneRotatePoint). Applies rotateX
    // then rotateY then rotateZ to match the original helper semantics.
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

    // Inlined sceneMotionOffset. Short-circuits when no drift components
    // are set so static objects skip the sin/cos math entirely — the old
    // helper early-returned a fresh {0,0,0} per call, still allocating.
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

  // Module-level scratches used by the hot line-geometry and triangle-mesh
  // loops inside appendSceneObjectToBundle / appendSceneMeshObjectToBundle.
  // They live above the loops so each frame reuses the same objects in
  // place — no per-iteration allocations. Callers MUST NOT retain these
  // references across another translateScenePointInto call on the same
  // scratch; each iteration is expected to consume its scratch inline
  // before the next iteration's translate.
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
      // Two widths in play here:
      //   - rawLineWidth: 0 when the user didn't set LinesGeometry.Width at
      //     all, positive when they did. Stored as-is in bundle.worldLineWidths
      //     so the WebGL thick-line dispatch only activates on explicit
      //     non-default widths (sceneBundleNeedsThickLines checks > 1) and
      //     legacy wireframe objects keep using gl.LINES.
      //   - objectLineWidth: resolved render width used by appendSceneLine's
      //     per-line record for the Canvas 2D non-world path, which still
      //     expects a positive width value. Falls back to the legacy 1.8px
      //     default when rawLineWidth is zero.
      const rawLineWidth = sceneNumber(object && object.lineWidth, 0);
      const objectLineWidth = rawLineWidth > 0 ? rawLineWidth : 1.8;
      // Map the object's render pass to the 0/1/2 bucket the thick-line
      // draw path uses to group segments by pass. Computed once per object
      // so downstream per-segment pushes are a single integer assignment.
      const objectPassString = sceneWorldObjectRenderPass(object, material);
      const objectPassIndex = objectPassString === "alpha" ? 1 : (objectPassString === "additive" ? 2 : 0);
      // Hoist segment world-space scratches above the loop. Both `fromWorld`
      // and `toWorld` are stable within a single iteration because all
      // downstream consumers (sceneLitColorRGBA, sceneExpandWorldBounds,
      // sceneProjectPoint) read fields inline rather than retaining refs,
      // and appendSceneLine stores the sceneProjectPoint *output* (fresh
      // per call) not the world scratch. Pre-restructure this path built
      // an intermediate worldSegments array with fresh pair objects per
      // segment — 4 allocs per segment × N segments × 60 fps = a lot of
      // GC churn on any line-heavy scene.
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
        // Keep worldLineWidths in lockstep with each segment pushed into
        // worldPositions. Store rawLineWidth (zero when unset) so the
        // canvas 2D world renderer and the WebGL thick-line dispatch can
        // distinguish "default width" (0 → fall back at read time) from
        // "user explicitly asked for width N" (N > 0 → honor on both paths).
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
      // Translate the three triangle vertices directly from the raw
      // positions Float32Array into hoisted scratch points, skipping the
      // intermediate sceneMeshVertexPoint object allocation (was 3 extra
      // allocs per triangle). points[] itself is the shared
      // _meshTrianglePoints module scratch — all downstream consumers
      // (lighting computation, mesh buffer push loop, three wire segment
      // calls) read fields inline before the next iteration clobbers
      // them, so the scratch is stable within each triangle.
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
    // Thick-line program is compiled lazily (and silently tolerates failure)
    // so a driver that refuses the program just falls back to the legacy
    // gl.LINES path. Buffers exist up-front so the draw path doesn't allocate
    // per frame.
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
    // Resolve the camera object once per invocation — sceneRenderCamera
    // allocates, and this function used to call it twice (once per uniform
    // group) which doubled GC pressure for no reason.
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

    // Dispatch to the thick-line program when any world line has an explicit
    // width > 1 (scene.LinesGeometry.Width on the Go side). This preserves
    // legacy behavior for existing scenes (hairline via gl.LINES) and only
    // activates the new draw path for scenes that opt in. The thick-line
    // path currently draws all world lines as one call with alpha blending
    // and does not respect the draw plan's per-pass (opaque/alpha/additive)
    // blend separation — follow-up work can thread per-pass state through
    // if a production scene needs mixed-blend thick lines.
    if (sceneBundleNeedsThickLines(bundle) && resources.thickLineProgram) {
      const thickDrew = drawSceneThickLines(gl, bundle, canvas, resources);
      if (thickDrew) {
        gl.useProgram(resources.program);
        applySceneWebGLUniforms(gl, bundle, canvas, true, resources);
        drew = renderSceneWebGLSurfaces(gl, bundle, canvas, resources, "alpha") || drew || true;
        drew = renderSceneWebGLSurfaces(gl, bundle, canvas, resources, "additive") || drew;
        return true;
      }
      // Thick-line draw failed (e.g. segment count overflow). Fall through
      // to the legacy gl.LINES path so the scene still renders.
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

  // ---- Thick line WebGL program (three.js Line2-style vertex expansion) ----
  //
  // gl.LINES respects only hairline widths on almost every driver. To honor
  // scene.LinesGeometry.Width on WebGL we expand each segment (A, B) into a
  // screen-space quad (4 vertices, 2 triangles) in the vertex shader and
  // offset each vertex perpendicular to the line's screen-space direction
  // by (side * width * 0.5) pixels.
  //
  // The projection math mirrors the legacy createSceneWebGLProgram path so
  // thick lines align exactly with whatever else the legacy world renderer
  // draws. u_viewport is the render target size in pixels (canvas.width /
  // canvas.height) and is used to convert a pixel-space offset back into
  // clip-space via multiplication by clip.w.
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
      // Compute the screen-space direction using post-divide NDC positions
      // scaled by half the viewport. Short-segment guard prevents a NaN when
      // both endpoints collapse onto the same pixel.
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
      // Offset = (side * width/2) pixels → NDC via division by half-viewport
      // → clip space via multiplication by base.w.
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

  // Pooled scratch for thick-line vertex expansion. Lives on the renderer
  // resources (resources.thickLineScratch) so it's reused across frames —
  // the previous implementation allocated 8 fresh typed arrays per frame
  // for any scene with thick lines, which on a 60 fps particle-effect
  // scene burned 480 typed-array allocations per second on the GC heap.
  //
  // Growth strategy: geometric (2× current capacity) up to the current
  // segment count. Never shrinks — sustained peak usage stays mapped.
  //
  // Quad layout per segment (4 vertices, 2 triangles):
  //
  //     endpoint=0,side=-1   endpoint=1,side=-1
  //              *──────────*
  //              │          │
  //              │          │
  //              *──────────*
  //     endpoint=0,side=+1   endpoint=1,side=+1
  //
  // All 4 vertices carry the full (positionA, positionB, colorA, colorB)
  // pair so the vertex shader can compute the screen-space direction
  // without touching the index buffer.
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
      // Three pooled index buffers — one per render pass. Writing them
      // separately lets the draw path issue up to three drawElements
      // calls with different blend/depth states so additive thick lines
      // composite correctly against opaque and alpha passes.
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
    // Worst case: all segments belong to one pass. Sized for that.
    scratch.opaqueIndices = new Uint16Array(nextCapacity * 6);
    scratch.alphaIndices = new Uint16Array(nextCapacity * 6);
    scratch.additiveIndices = new Uint16Array(nextCapacity * 6);
    scratch.segmentCapacity = nextCapacity;
  }

  // Quad corner layout: shared constants hoisted out of the hot loop.
  // Vertex indices inside a quad: 0 = A-, 1 = A+, 2 = B+, 3 = B-.
  // Triangles: (0,1,2) and (0,2,3).
  const _thickLineQuadEndpoints = [0, 0, 1, 1];
  const _thickLineQuadSides = [-1, 1, 1, -1];

  // expandSceneThickLineIntoScratch walks world line data, writes the
  // expanded per-quad attribute values into pooled scratch, and assigns
  // each segment's 6 triangle indices to the scratch index buffer
  // matching its render pass (0=opaque, 1=alpha, 2=additive). One linear
  // pass through worldPositions — no sorting, no allocations.
  //
  // Returns the total segment count actually processed (may be less than
  // the input when the 16384-segment Uint16 cap is hit; the overflow
  // guard in the draw path routes those scenes back to gl.LINES).
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
      // One GL index buffer per render pass so the draw path can swap
      // between them without re-uploading index data.
      opaqueIndex: gl.createBuffer(),
      alphaIndex: gl.createBuffer(),
      additiveIndex: gl.createBuffer(),
    };
  }

  // Uploads the expanded vertex attributes once (shared across all passes)
  // and uploads each non-empty pass's index buffer. Index uploads are
  // clipped to the used length via subarray so a 16k-capacity scratch
  // running 128 opaque segments only pushes 768 u16s to the GPU, not 96k.
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
    // Overflow guard: Uint16 indices cap at 65535 → 16384 segments. Falling
    // back to the legacy gl.LINES path lets enormous scenes (particle field
    // fallback bundles) still render without a buffer overflow crash — they
    // just won't honor per-segment width past that cutoff.
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

    // Opaque pass first (depth writes on), then alpha, then additive.
    // Matches the draw plan's standard pass ordering so thick lines
    // composite correctly against triangle meshes drawn by the PBR path.
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

  // sceneBundleNeedsThickLines returns true when any world line segment has
  // an explicit width > 1 (LinesGeometry.Width was set on the Go side). False
  // preserves the legacy gl.LINES draw path so pre-v0.15.1 scenes render
  // unchanged on drivers that happily take hairline widths.
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
