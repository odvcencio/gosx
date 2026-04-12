  // Runtime infrastructure utilities extracted from 10-runtime-scene-core.js
  // for the selective bootstrap. Scene-specific code (normalizers, state
  // builders, bundle builders) stays in the scene3d feature chunk.

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


  // DOM utility used by both runtime and scene3d feature chunks.
  function clearChildren(node) {
    while (node && node.firstChild) {
      node.removeChild(node.firstChild);
    }
  }







  // sceneRenderCamera normalizes a raw camera (partial, may be missing
