// GoSX Client Bootstrap v0.2.0
// Loads shared WASM runtime, fetches per-island programs, hydrates islands
// via event delegation. This is the only JavaScript in a GoSX app.
//
// Expects:
//   - wasm_exec.js loaded before this script (standard Go WASM support)
//   - <script id="gosx-manifest" type="application/json"> with island manifest
//   - WASM exports: __gosx_hydrate, __gosx_action, __gosx_dispose
//   - WASM calls window.__gosx_runtime_ready() when Go runtime is initialized

(function() {
  "use strict";

  const GOSX_VERSION = "0.2.0";

  // --- GoSX runtime namespace ---
  const engineFactories = window.__gosx_engine_factories || Object.create(null);
  const loadedEngineScripts = new Map();
  window.__gosx_engine_factories = engineFactories;
  window.__gosx_register_engine_factory = function(name, factory) {
    if (!name || typeof factory !== "function") {
      console.error("[gosx] invalid engine factory registration");
      return;
    }
    engineFactories[name] = factory;
  };

  window.__gosx = {
    version: GOSX_VERSION,
    islands: new Map(),   // islandID -> { component, listeners, root }
    engines: new Map(),   // engineID -> { component, kind, mount, handle }
    hubs: new Map(),      // hubID -> { entry, socket, reconnectTimer }
    input: {
      pending: null,
      frameHandle: 0,
      providers: Object.create(null),
    },
    ready: false,
  };

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

  async function loadEngineScript(jsRef) {
    if (!jsRef) return;
    if (loadedEngineScripts.has(jsRef)) {
      return loadedEngineScripts.get(jsRef);
    }

    const promise = (async function() {
      try {
        const resp = await fetch(jsRef);
        if (!resp.ok) {
          throw new Error("engine script fetch failed with status " + resp.status);
        }

        const source = await resp.text();
        (0, eval)(String(source) + "\n//# sourceURL=" + jsRef);
      } catch (e) {
        console.error(`[gosx] failed to load engine script ${jsRef}:`, e);
      }
    })();

    loadedEngineScripts.set(jsRef, promise);
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
    if (typeof setInputBatch !== "function") return;

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
      activateInputProvider(capability);
    }
  }

  function activateInputProvider(capability) {
    const state = gosxInputState();
    const current = state.providers[capability];
    if (current) {
      current.refCount += 1;
      return;
    }

    const provider = createInputProvider(capability);
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

  function createInputProvider(capability) {
    switch (capability) {
      case "keyboard":
        return createKeyboardInputProvider();
      case "pointer":
        return createPointerInputProvider();
      case "gamepad":
        return createGamepadInputProvider();
      default:
        return null;
    }
  }

  function createKeyboardInputProvider() {
    const pressed = new Set();

    function normalizeKeyName(event) {
      const raw = event && (event.key || event.code);
      if (!raw) return "";
      return String(raw).trim().toLowerCase();
    }

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

    document.addEventListener("keydown", onKey);
    document.addEventListener("keyup", onKey);
    window.addEventListener("blur", onBlur);

    return {
      dispose() {
        document.removeEventListener("keydown", onKey);
        document.removeEventListener("keyup", onKey);
        window.removeEventListener("blur", onBlur);
      },
    };
  }

  function createPointerInputProvider() {
    let lastX = null;
    let lastY = null;

    function pointerNumber(value, fallback) {
      const num = Number(value);
      return Number.isFinite(num) ? num : fallback;
    }

    function publishPointer(event) {
      const x = pointerNumber(event && event.clientX, lastX == null ? 0 : lastX);
      const y = pointerNumber(event && event.clientY, lastY == null ? 0 : lastY);
      const deltaX = pointerNumber(event && event.movementX, lastX == null ? 0 : x - lastX);
      const deltaY = pointerNumber(event && event.movementY, lastY == null ? 0 : y - lastY);
      lastX = x;
      lastY = y;

      queueInputSignal("$input.pointer.x", x);
      queueInputSignal("$input.pointer.y", y);
      queueInputSignal("$input.pointer.deltaX", deltaX);
      queueInputSignal("$input.pointer.deltaY", deltaY);
      if (event && typeof event.buttons !== "undefined") {
        queueInputSignal("$input.pointer.buttons", pointerNumber(event.buttons, 0));
      }
      if (event && typeof event.button === "number") {
        queueInputSignal("$input.pointer.button" + event.button, event.type !== "pointerup");
      }
    }

    function onBlur() {
      queueInputSignal("$input.pointer.deltaX", 0);
      queueInputSignal("$input.pointer.deltaY", 0);
      queueInputSignal("$input.pointer.buttons", 0);
    }

    document.addEventListener("pointermove", publishPointer);
    document.addEventListener("pointerdown", publishPointer);
    document.addEventListener("pointerup", publishPointer);
    window.addEventListener("blur", onBlur);

    return {
      dispose() {
        document.removeEventListener("pointermove", publishPointer);
        document.removeEventListener("pointerdown", publishPointer);
        document.removeEventListener("pointerup", publishPointer);
        window.removeEventListener("blur", onBlur);
      },
    };
  }

  function createGamepadInputProvider() {
    let active = true;
    let frameHandle = 0;

    function readButton(pad, index) {
      return Boolean(pad && pad.buttons && pad.buttons[index] && pad.buttons[index].pressed);
    }

    function pollGamepad() {
      if (!active) return;
      const navigatorRef = window.navigator;
      if (navigatorRef && typeof navigatorRef.getGamepads === "function") {
        const pads = navigatorRef.getGamepads() || [];
        const pad = pads[0];
        if (pad) {
          const axes = Array.isArray(pad.axes) ? pad.axes : [];
          queueInputSignal("$input.gamepad0.connected", true);
          queueInputSignal("$input.gamepad0.leftX", sceneNumber(axes[0], 0));
          queueInputSignal("$input.gamepad0.leftY", sceneNumber(axes[1], 0));
          queueInputSignal("$input.gamepad0.rightX", sceneNumber(axes[2], 0));
          queueInputSignal("$input.gamepad0.rightY", sceneNumber(axes[3], 0));
          queueInputSignal("$input.gamepad0.buttonA", readButton(pad, 0));
          queueInputSignal("$input.gamepad0.buttonB", readButton(pad, 1));
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

  function sceneProps(props) {
    return props && props.scene && typeof props.scene === "object" ? props.scene : null;
  }

  function sceneObjectList(value) {
    return Array.isArray(value) && value.length > 0 ? value : null;
  }

  function normalizeSceneObject(object, index) {
    const item = object && typeof object === "object" ? object : {};
    const size = sceneNumber(item.size, 1.2);
    return {
      id: item.id || ("scene-object-" + index),
      kind: normalizeSceneKind(item.kind),
      size,
      width: sceneNumber(item.width, size),
      height: sceneNumber(item.height, size),
      depth: sceneNumber(item.depth, size),
      radius: sceneNumber(item.radius, size / 2),
      segments: sceneSegmentResolution(item.segments),
      x: sceneNumber(item.x, 0),
      y: sceneNumber(item.y, 0),
      z: sceneNumber(item.z, 0),
      color: typeof item.color === "string" && item.color ? item.color : "#8de1ff",
      rotationX: sceneNumber(item.rotationX, 0),
      rotationY: sceneNumber(item.rotationY, 0),
      rotationZ: sceneNumber(item.rotationZ, 0),
      spinX: sceneNumber(item.spinX, 0),
      spinY: sceneNumber(item.spinY, 0),
      spinZ: sceneNumber(item.spinZ, 0),
    };
  }

  function normalizeSceneKind(value) {
    const kind = typeof value === "string" ? value.trim().toLowerCase() : "";
    switch (kind) {
      case "box":
      case "plane":
      case "pyramid":
      case "sphere":
        return kind;
      default:
        return "cube";
    }
  }

  function sceneSegmentResolution(value) {
    const segments = Math.round(sceneNumber(value, 12));
    return Math.max(6, Math.min(24, segments));
  }

  function sceneObjects(props) {
    return rawSceneObjects(props).map(function(object, index) {
      return normalizeSceneObject(object, index);
    });
  }

  function sceneCamera(props) {
    const raw = props && props.camera && typeof props.camera === "object" ? props.camera : {};
    return normalizeSceneCamera(raw, {
      x: 0,
      y: 0,
      z: 6,
      fov: 75,
    });
  }

  function normalizeSceneCamera(raw, fallback) {
    const base = fallback || {};
    return {
      x: sceneNumber(raw.x, sceneNumber(base.x, 0)),
      y: sceneNumber(raw.y, sceneNumber(base.y, 0)),
      z: sceneNumber(raw.z, sceneNumber(base.z, 6)),
      fov: sceneNumber(raw.fov, sceneNumber(base.fov, 75)),
    };
  }

  function createSceneState(props) {
    const state = {
      background: typeof props.background === "string" && props.background ? props.background : "#08151f",
      camera: sceneCamera(props),
      objects: new Map(),
    };
    for (const object of sceneObjects(props)) {
      state.objects.set(object.id, object);
    }
    return state;
  }

  function sceneStateObjects(state) {
    return Array.from(state.objects.values());
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
        return;
      case SCENE_CMD_SET_TRANSFORM:
      case SCENE_CMD_SET_MATERIAL:
        applySceneObjectPatch(state, command.objectId, command.data);
        return;
      case SCENE_CMD_SET_CAMERA:
        state.camera = normalizeSceneCamera(command.data || {}, state.camera);
        return;
      case SCENE_CMD_SET_LIGHT:
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
    if (payload.kind === "light" || payload.kind === "particles") {
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
    if (!current) return;
    const next = sceneObjectFromPayload(objectID, {
      geometry: current.kind,
      props: Object.assign({}, current, patch || {}),
    }, current);
    if (next) {
      state.objects.set(key, next);
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
    return normalizeSceneObject(merged, objectID);
  }

  function clearChildren(node) {
    while (node && node.firstChild) {
      node.removeChild(node.firstChild);
    }
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

  function sceneObjectSegments(object) {
    switch (object.kind) {
      case "box":
      case "cube":
        return boxSegments(object);
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

  function rotatePoint(point, rotationX, rotationY, rotationZ) {
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

  function projectPoint(point, camera, width, height) {
    const depth = point.z + camera.z;
    if (depth <= 0.15) return null;
    const focal = (Math.min(width, height) / 2) / Math.tan((camera.fov * Math.PI) / 360);
    return {
      x: width / 2 + ((point.x - camera.x) * focal) / depth,
      y: height / 2 - ((point.y - camera.y) * focal) / depth,
      depth,
    };
  }

  function strokeLine(ctx2d, from, to) {
    ctx2d.beginPath();
    ctx2d.moveTo(from.x, from.y);
    ctx2d.lineTo(to.x, to.y);
    ctx2d.stroke();
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

  function sceneClipPoint(point, width, height) {
    return {
      x: (point.x / width) * 2 - 1,
      y: 1 - (point.y / height) * 2,
    };
  }

  function createSceneRenderBundle(width, height, background, camera, objects, timeSeconds) {
    const bundle = {
      background: background,
      lines: [],
      positions: [],
      colors: [],
      vertexCount: 0,
      objectCount: objects.length,
    };
    appendSceneGridToBundle(bundle, width, height);
    for (const object of objects) {
      appendSceneObjectToBundle(bundle, camera, width, height, object, timeSeconds);
    }
    bundle.positions = new Float32Array(bundle.positions);
    bundle.colors = new Float32Array(bundle.colors);
    bundle.vertexCount = bundle.positions.length / 2;
    return bundle;
  }

  function projectSceneObject(object, camera, width, height, timeSeconds) {
    return sceneObjectSegments(object).map(function(segment) {
      return [
        projectPoint(translateScenePoint(segment[0], object, timeSeconds), camera, width, height),
        projectPoint(translateScenePoint(segment[1], object, timeSeconds), camera, width, height),
      ];
    });
  }

  function translateScenePoint(point, object, timeSeconds) {
    const rotated = rotatePoint(
      point,
      object.rotationX + object.spinX * timeSeconds,
      object.rotationY + object.spinY * timeSeconds,
      object.rotationZ + object.spinZ * timeSeconds,
    );
    return {
      x: rotated.x + object.x,
      y: rotated.y + object.y,
      z: rotated.z + object.z,
    };
  }

  function appendSceneGridToBundle(bundle, width, height) {
    for (let x = 0; x <= width; x += 48) {
      appendSceneLine(bundle, width, height, { x: x, y: 0 }, { x: x, y: height }, "rgba(141, 225, 255, 0.14)", 1);
    }
    for (let y = 0; y <= height; y += 48) {
      appendSceneLine(bundle, width, height, { x: 0, y: y }, { x: width, y: y }, "rgba(141, 225, 255, 0.14)", 1);
    }
  }

  function appendSceneObjectToBundle(bundle, camera, width, height, object, timeSeconds) {
    const projected = projectSceneObject(object, camera, width, height, timeSeconds);
    for (const segment of projected) {
      const from = segment[0];
      const to = segment[1];
      if (!from || !to) continue;
      appendSceneLine(bundle, width, height, from, to, object.color, 1.8);
    }
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

  function createSceneCanvasRenderer(ctx2d, canvas) {
    return {
      kind: "canvas",
      render(bundle) {
        ctx2d.clearRect(0, 0, canvas.width, canvas.height);
        ctx2d.fillStyle = bundle.background;
        ctx2d.fillRect(0, 0, canvas.width, canvas.height);
        for (const line of bundle.lines) {
          ctx2d.strokeStyle = line.color;
          ctx2d.lineWidth = line.lineWidth;
          strokeLine(ctx2d, line.from, line.to);
        }
      },
      dispose() {},
    };
  }

  function createSceneWebGLRenderer(canvas) {
    if (!canvas || typeof canvas.getContext !== "function") {
      return null;
    }
    const gl = canvas.getContext("webgl", { antialias: true, alpha: false }) || canvas.getContext("experimental-webgl", { antialias: true, alpha: false });
    if (!gl) {
      return null;
    }

    const program = createSceneWebGLProgram(gl);
    if (!program) {
      return null;
    }

    const positionBuffer = gl.createBuffer();
    const colorBuffer = gl.createBuffer();
    const materialBuffer = gl.createBuffer();
    const staticOpaquePositionBuffer = gl.createBuffer();
    const staticOpaqueColorBuffer = gl.createBuffer();
    const staticOpaqueMaterialBuffer = gl.createBuffer();
    const staticAlphaPositionBuffer = gl.createBuffer();
    const staticAlphaColorBuffer = gl.createBuffer();
    const staticAlphaMaterialBuffer = gl.createBuffer();
    const staticAdditivePositionBuffer = gl.createBuffer();
    const staticAdditiveColorBuffer = gl.createBuffer();
    const staticAdditiveMaterialBuffer = gl.createBuffer();
    const dynamicOpaquePositionBuffer = gl.createBuffer();
    const dynamicOpaqueColorBuffer = gl.createBuffer();
    const dynamicOpaqueMaterialBuffer = gl.createBuffer();
    const dynamicAlphaPositionBuffer = gl.createBuffer();
    const dynamicAlphaColorBuffer = gl.createBuffer();
    const dynamicAlphaMaterialBuffer = gl.createBuffer();
    const dynamicAdditivePositionBuffer = gl.createBuffer();
    const dynamicAdditiveColorBuffer = gl.createBuffer();
    const dynamicAdditiveMaterialBuffer = gl.createBuffer();
    const positionLocation = gl.getAttribLocation(program, "a_position");
    const colorLocation = gl.getAttribLocation(program, "a_color");
    const materialLocation = gl.getAttribLocation(program, "a_material");
    const cameraLocation = gl.getUniformLocation(program, "u_camera");
    const aspectLocation = gl.getUniformLocation(program, "u_aspect");
    const perspectiveLocation = gl.getUniformLocation(program, "u_use_perspective");
    const floatType = typeof gl.FLOAT === "number" ? gl.FLOAT : 0x1406;
    const arrayBuffer = typeof gl.ARRAY_BUFFER === "number" ? gl.ARRAY_BUFFER : 0x8892;
    const dynamicDraw = typeof gl.DYNAMIC_DRAW === "number" ? gl.DYNAMIC_DRAW : 0x88E8;
    const colorBufferBit = typeof gl.COLOR_BUFFER_BIT === "number" ? gl.COLOR_BUFFER_BIT : 0x4000;
    const linesMode = typeof gl.LINES === "number" ? gl.LINES : 0x0001;
    let staticOpaqueKey = "";
    let staticOpaqueVertexCount = 0;
    let staticAlphaKey = "";
    let staticAlphaVertexCount = 0;
    let staticAdditiveKey = "";
    let staticAdditiveVertexCount = 0;

    return {
      kind: "webgl",
      render(bundle) {
        const background = sceneColorRGBA(bundle.background, [0.03, 0.08, 0.12, 1]);
        gl.viewport(0, 0, canvas.width, canvas.height);
        gl.clearColor(background[0], background[1], background[2], background[3]);
        gl.clear(colorBufferBit);
        const usePerspective = Boolean(bundle && bundle.worldVertexCount > 0 && bundle.worldPositions && bundle.worldColors);
        const positions = usePerspective ? bundle.worldPositions : bundle.positions;
        const colors = usePerspective ? bundle.worldColors : bundle.colors;
        const vertexCount = usePerspective ? bundle.worldVertexCount : bundle.vertexCount;
        if (!bundle || vertexCount === 0 || !positions || !colors) {
          return;
        }
        gl.useProgram(program);
        if (typeof gl.uniform4f === "function" && cameraLocation) {
          const camera = bundle.camera || {};
          gl.uniform4f(
            cameraLocation,
            sceneNumber(camera.x, 0),
            sceneNumber(camera.y, 0),
            sceneNumber(camera.z, 6),
            sceneNumber(camera.fov, 75),
          );
        }
        if (typeof gl.uniform1f === "function" && aspectLocation) {
          gl.uniform1f(aspectLocation, Math.max(0.0001, canvas.width / Math.max(1, canvas.height)));
        }
        if (typeof gl.uniform1f === "function" && perspectiveLocation) {
          gl.uniform1f(perspectiveLocation, usePerspective ? 1 : 0);
        }

        if (usePerspective) {
          const drawPlan = buildSceneWorldDrawPlan(bundle);
          if (drawPlan) {
            if (drawPlan.staticOpaqueKey !== staticOpaqueKey) {
              uploadSceneWebGLBuffers(gl, arrayBuffer, dynamicDraw, staticOpaquePositionBuffer, staticOpaqueColorBuffer, staticOpaqueMaterialBuffer, drawPlan.staticOpaquePositions, drawPlan.staticOpaqueColors, drawPlan.staticOpaqueMaterials);
              staticOpaqueKey = drawPlan.staticOpaqueKey;
              staticOpaqueVertexCount = drawPlan.staticOpaqueVertexCount;
            }
            if (drawPlan.staticAlphaKey !== staticAlphaKey) {
              uploadSceneWebGLBuffers(gl, arrayBuffer, dynamicDraw, staticAlphaPositionBuffer, staticAlphaColorBuffer, staticAlphaMaterialBuffer, drawPlan.staticAlphaPositions, drawPlan.staticAlphaColors, drawPlan.staticAlphaMaterials);
              staticAlphaKey = drawPlan.staticAlphaKey;
              staticAlphaVertexCount = drawPlan.staticAlphaVertexCount;
            }
            if (drawPlan.staticAdditiveKey !== staticAdditiveKey) {
              uploadSceneWebGLBuffers(gl, arrayBuffer, dynamicDraw, staticAdditivePositionBuffer, staticAdditiveColorBuffer, staticAdditiveMaterialBuffer, drawPlan.staticAdditivePositions, drawPlan.staticAdditiveColors, drawPlan.staticAdditiveMaterials);
              staticAdditiveKey = drawPlan.staticAdditiveKey;
              staticAdditiveVertexCount = drawPlan.staticAdditiveVertexCount;
            }

            applySceneWebGLBlend(gl, "opaque");
            drawSceneWebGLLines(gl, arrayBuffer, floatType, linesMode, positionLocation, colorLocation, materialLocation, staticOpaquePositionBuffer, staticOpaqueColorBuffer, staticOpaqueMaterialBuffer, staticOpaqueVertexCount, 3);
            uploadSceneWebGLBuffers(gl, arrayBuffer, dynamicDraw, dynamicOpaquePositionBuffer, dynamicOpaqueColorBuffer, dynamicOpaqueMaterialBuffer, drawPlan.dynamicOpaquePositions, drawPlan.dynamicOpaqueColors, drawPlan.dynamicOpaqueMaterials);
            drawSceneWebGLLines(gl, arrayBuffer, floatType, linesMode, positionLocation, colorLocation, materialLocation, dynamicOpaquePositionBuffer, dynamicOpaqueColorBuffer, dynamicOpaqueMaterialBuffer, drawPlan.dynamicOpaqueVertexCount, 3);

            if (drawPlan.hasAlphaPass) {
              applySceneWebGLBlend(gl, "alpha");
              drawSceneWebGLLines(gl, arrayBuffer, floatType, linesMode, positionLocation, colorLocation, materialLocation, staticAlphaPositionBuffer, staticAlphaColorBuffer, staticAlphaMaterialBuffer, staticAlphaVertexCount, 3);
              uploadSceneWebGLBuffers(gl, arrayBuffer, dynamicDraw, dynamicAlphaPositionBuffer, dynamicAlphaColorBuffer, dynamicAlphaMaterialBuffer, drawPlan.dynamicAlphaPositions, drawPlan.dynamicAlphaColors, drawPlan.dynamicAlphaMaterials);
              drawSceneWebGLLines(gl, arrayBuffer, floatType, linesMode, positionLocation, colorLocation, materialLocation, dynamicAlphaPositionBuffer, dynamicAlphaColorBuffer, dynamicAlphaMaterialBuffer, drawPlan.dynamicAlphaVertexCount, 3);
            }
            if (drawPlan.hasAdditivePass) {
              applySceneWebGLBlend(gl, "additive");
              drawSceneWebGLLines(gl, arrayBuffer, floatType, linesMode, positionLocation, colorLocation, materialLocation, staticAdditivePositionBuffer, staticAdditiveColorBuffer, staticAdditiveMaterialBuffer, staticAdditiveVertexCount, 3);
              uploadSceneWebGLBuffers(gl, arrayBuffer, dynamicDraw, dynamicAdditivePositionBuffer, dynamicAdditiveColorBuffer, dynamicAdditiveMaterialBuffer, drawPlan.dynamicAdditivePositions, drawPlan.dynamicAdditiveColors, drawPlan.dynamicAdditiveMaterials);
              drawSceneWebGLLines(gl, arrayBuffer, floatType, linesMode, positionLocation, colorLocation, materialLocation, dynamicAdditivePositionBuffer, dynamicAdditiveColorBuffer, dynamicAdditiveMaterialBuffer, drawPlan.dynamicAdditiveVertexCount, 3);
            }
            applySceneWebGLBlend(gl, "opaque");
            return;
          }
        }

        applySceneWebGLBlend(gl, "opaque");
        uploadSceneWebGLBuffers(gl, arrayBuffer, dynamicDraw, positionBuffer, colorBuffer, materialBuffer, positions, colors, sceneFallbackMaterialData(vertexCount));
        drawSceneWebGLLines(gl, arrayBuffer, floatType, linesMode, positionLocation, colorLocation, materialLocation, positionBuffer, colorBuffer, materialBuffer, vertexCount, usePerspective ? 3 : 2);
      },
      dispose() {
        if (typeof gl.deleteBuffer === "function") {
          gl.deleteBuffer(positionBuffer);
          gl.deleteBuffer(colorBuffer);
          gl.deleteBuffer(materialBuffer);
          gl.deleteBuffer(staticOpaquePositionBuffer);
          gl.deleteBuffer(staticOpaqueColorBuffer);
          gl.deleteBuffer(staticOpaqueMaterialBuffer);
          gl.deleteBuffer(staticAlphaPositionBuffer);
          gl.deleteBuffer(staticAlphaColorBuffer);
          gl.deleteBuffer(staticAlphaMaterialBuffer);
          gl.deleteBuffer(staticAdditivePositionBuffer);
          gl.deleteBuffer(staticAdditiveColorBuffer);
          gl.deleteBuffer(staticAdditiveMaterialBuffer);
          gl.deleteBuffer(dynamicOpaquePositionBuffer);
          gl.deleteBuffer(dynamicOpaqueColorBuffer);
          gl.deleteBuffer(dynamicOpaqueMaterialBuffer);
          gl.deleteBuffer(dynamicAlphaPositionBuffer);
          gl.deleteBuffer(dynamicAlphaColorBuffer);
          gl.deleteBuffer(dynamicAlphaMaterialBuffer);
          gl.deleteBuffer(dynamicAdditivePositionBuffer);
          gl.deleteBuffer(dynamicAdditiveColorBuffer);
          gl.deleteBuffer(dynamicAdditiveMaterialBuffer);
        }
        if (typeof gl.deleteProgram === "function") {
          gl.deleteProgram(program);
        }
      },
    };
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

    gl.drawArrays(linesMode, 0, vertexCount);
  }

  function applySceneWebGLBlend(gl, mode) {
    const blendConst = typeof gl.BLEND === "number" ? gl.BLEND : 0x0BE2;
    const one = typeof gl.ONE === "number" ? gl.ONE : 1;
    const srcAlpha = typeof gl.SRC_ALPHA === "number" ? gl.SRC_ALPHA : 0x0302;
    const oneMinusSrcAlpha = typeof gl.ONE_MINUS_SRC_ALPHA === "number" ? gl.ONE_MINUS_SRC_ALPHA : 0x0303;
    switch (mode) {
    case "alpha":
      if (typeof gl.enable === "function") {
        gl.enable(blendConst);
      }
      if (typeof gl.blendFunc === "function") {
        gl.blendFunc(srcAlpha, oneMinusSrcAlpha);
      }
      return;
    case "additive":
      if (typeof gl.enable === "function") {
        gl.enable(blendConst);
      }
      if (typeof gl.blendFunc === "function") {
        gl.blendFunc(srcAlpha, one);
      }
      return;
    default:
      if (typeof gl.disable === "function") {
        gl.disable(blendConst);
      }
    }
  }

  function buildSceneWorldDrawPlan(bundle) {
    const objects = Array.isArray(bundle.objects) ? bundle.objects : [];
    const materials = Array.isArray(bundle.materials) ? bundle.materials : [];
    if (!objects.length || !materials.length) {
      return null;
    }

    const staticOpaquePositions = [];
    const staticOpaqueColors = [];
    const staticOpaqueMaterials = [];
    const staticAlphaPositions = [];
    const staticAlphaColors = [];
    const staticAlphaMaterials = [];
    const staticAdditivePositions = [];
    const staticAdditiveColors = [];
    const staticAdditiveMaterials = [];
    const dynamicOpaquePositions = [];
    const dynamicOpaqueColors = [];
    const dynamicOpaqueMaterials = [];
    const dynamicAlphaPositions = [];
    const dynamicAlphaColors = [];
    const dynamicAlphaMaterials = [];
    const dynamicAdditivePositions = [];
    const dynamicAdditiveColors = [];
    const dynamicAdditiveMaterials = [];
    const staticOpaqueObjects = [];
    const staticOpaqueMaterialProfiles = [];
    const staticAlphaObjects = [];
    const staticAlphaMaterialProfiles = [];
    const staticAdditiveObjects = [];
    const staticAdditiveMaterialProfiles = [];

    for (const object of objects) {
      if (!object || !Number.isFinite(object.vertexOffset) || !Number.isFinite(object.vertexCount) || object.vertexCount <= 0) {
        continue;
      }
      const material = materials[object.materialIndex] || null;
      const renderPass = sceneMaterialRenderPass(material);
      if (object.static && renderPass === "additive") {
        staticAdditiveObjects.push(object);
        staticAdditiveMaterialProfiles.push(material);
        appendSceneWorldObjectSlice(staticAdditivePositions, staticAdditiveColors, staticAdditiveMaterials, bundle.worldPositions, bundle.worldColors, object, material);
      } else if (object.static && renderPass === "alpha") {
        staticAlphaObjects.push(object);
        staticAlphaMaterialProfiles.push(material);
        appendSceneWorldObjectSlice(staticAlphaPositions, staticAlphaColors, staticAlphaMaterials, bundle.worldPositions, bundle.worldColors, object, material);
      } else if (object.static) {
        staticOpaqueObjects.push(object);
        staticOpaqueMaterialProfiles.push(material);
        appendSceneWorldObjectSlice(staticOpaquePositions, staticOpaqueColors, staticOpaqueMaterials, bundle.worldPositions, bundle.worldColors, object, material);
      } else if (renderPass === "additive") {
        appendSceneWorldObjectSlice(dynamicAdditivePositions, dynamicAdditiveColors, dynamicAdditiveMaterials, bundle.worldPositions, bundle.worldColors, object, material);
      } else if (renderPass === "alpha") {
        appendSceneWorldObjectSlice(dynamicAlphaPositions, dynamicAlphaColors, dynamicAlphaMaterials, bundle.worldPositions, bundle.worldColors, object, material);
      } else {
        appendSceneWorldObjectSlice(dynamicOpaquePositions, dynamicOpaqueColors, dynamicOpaqueMaterials, bundle.worldPositions, bundle.worldColors, object, material);
      }
    }

    const typedStaticOpaquePositions = sceneFloatArray(staticOpaquePositions);
    const typedStaticOpaqueColors = sceneFloatArray(staticOpaqueColors);
    const typedStaticOpaqueMaterialData = sceneFloatArray(staticOpaqueMaterials);
    const typedStaticAlphaPositions = sceneFloatArray(staticAlphaPositions);
    const typedStaticAlphaColors = sceneFloatArray(staticAlphaColors);
    const typedStaticAlphaMaterialData = sceneFloatArray(staticAlphaMaterials);
    const typedStaticAdditivePositions = sceneFloatArray(staticAdditivePositions);
    const typedStaticAdditiveColors = sceneFloatArray(staticAdditiveColors);
    const typedStaticAdditiveMaterialData = sceneFloatArray(staticAdditiveMaterials);
    const typedDynamicOpaquePositions = sceneFloatArray(dynamicOpaquePositions);
    const typedDynamicOpaqueColors = sceneFloatArray(dynamicOpaqueColors);
    const typedDynamicOpaqueMaterialData = sceneFloatArray(dynamicOpaqueMaterials);
    const typedDynamicAlphaPositions = sceneFloatArray(dynamicAlphaPositions);
    const typedDynamicAlphaColors = sceneFloatArray(dynamicAlphaColors);
    const typedDynamicAlphaMaterialData = sceneFloatArray(dynamicAlphaMaterials);
    const typedDynamicAdditivePositions = sceneFloatArray(dynamicAdditivePositions);
    const typedDynamicAdditiveColors = sceneFloatArray(dynamicAdditiveColors);
    const typedDynamicAdditiveMaterialData = sceneFloatArray(dynamicAdditiveMaterials);

    return {
      staticOpaqueKey: sceneStaticDrawKey(staticOpaqueObjects, staticOpaqueMaterialProfiles, typedStaticOpaquePositions, typedStaticOpaqueColors, typedStaticOpaqueMaterialData),
      staticOpaquePositions: typedStaticOpaquePositions,
      staticOpaqueColors: typedStaticOpaqueColors,
      staticOpaqueMaterials: typedStaticOpaqueMaterialData,
      staticOpaqueVertexCount: typedStaticOpaquePositions.length / 3,
      staticAlphaKey: sceneStaticDrawKey(staticAlphaObjects, staticAlphaMaterialProfiles, typedStaticAlphaPositions, typedStaticAlphaColors, typedStaticAlphaMaterialData),
      staticAlphaPositions: typedStaticAlphaPositions,
      staticAlphaColors: typedStaticAlphaColors,
      staticAlphaMaterials: typedStaticAlphaMaterialData,
      staticAlphaVertexCount: typedStaticAlphaPositions.length / 3,
      staticAdditiveKey: sceneStaticDrawKey(staticAdditiveObjects, staticAdditiveMaterialProfiles, typedStaticAdditivePositions, typedStaticAdditiveColors, typedStaticAdditiveMaterialData),
      staticAdditivePositions: typedStaticAdditivePositions,
      staticAdditiveColors: typedStaticAdditiveColors,
      staticAdditiveMaterials: typedStaticAdditiveMaterialData,
      staticAdditiveVertexCount: typedStaticAdditivePositions.length / 3,
      dynamicOpaquePositions: typedDynamicOpaquePositions,
      dynamicOpaqueColors: typedDynamicOpaqueColors,
      dynamicOpaqueMaterials: typedDynamicOpaqueMaterialData,
      dynamicOpaqueVertexCount: typedDynamicOpaquePositions.length / 3,
      dynamicAlphaPositions: typedDynamicAlphaPositions,
      dynamicAlphaColors: typedDynamicAlphaColors,
      dynamicAlphaMaterials: typedDynamicAlphaMaterialData,
      dynamicAlphaVertexCount: typedDynamicAlphaPositions.length / 3,
      dynamicAdditivePositions: typedDynamicAdditivePositions,
      dynamicAdditiveColors: typedDynamicAdditiveColors,
      dynamicAdditiveMaterials: typedDynamicAdditiveMaterialData,
      dynamicAdditiveVertexCount: typedDynamicAdditivePositions.length / 3,
      hasAlphaPass: typedStaticAlphaPositions.length > 0 || typedDynamicAlphaPositions.length > 0,
      hasAdditivePass: typedStaticAdditivePositions.length > 0 || typedDynamicAdditivePositions.length > 0,
    };
  }

  function appendSceneWorldObjectSlice(targetPositions, targetColors, targetMaterials, sourcePositions, sourceColors, object, material) {
    const vertexOffset = Math.max(0, Math.floor(sceneNumber(object.vertexOffset, 0)));
    const vertexCount = Math.max(0, Math.floor(sceneNumber(object.vertexCount, 0)));
    const startPosition = vertexOffset * 3;
    const endPosition = startPosition + vertexCount * 3;
    const startColor = vertexOffset * 4;
    const endColor = startColor + vertexCount * 4;
    const opacity = sceneMaterialOpacity(material);
    const materialData = sceneMaterialShaderData(material);

    for (let i = startPosition; i < endPosition; i += 1) {
      targetPositions.push(sourcePositions[i] || 0);
    }
    for (let i = startColor; i < endColor; i += 4) {
      targetColors.push(sourceColors[i] || 0);
      targetColors.push(sourceColors[i + 1] || 0);
      targetColors.push(sourceColors[i + 2] || 0);
      targetColors.push((sourceColors[i + 3] == null ? 1 : sourceColors[i + 3]) * opacity);
      targetMaterials.push(materialData[0], materialData[1], materialData[2]);
    }
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
      return [4, sceneMaterialEmissive(material), 0.78];
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

  function clamp01(value) {
    return Math.max(0, Math.min(1, value));
  }

  function sceneStaticDrawKey(objects, materials, positions, colors, materialData) {
    let hash = 2166136261 >>> 0;
    for (const object of objects) {
      hash = sceneHashString(hash, object.id || "");
      hash = sceneHashString(hash, object.kind || "");
      hash = sceneHashNumber(hash, sceneNumber(object.materialIndex, 0));
      hash = sceneHashNumber(hash, sceneNumber(object.vertexOffset, 0));
      hash = sceneHashNumber(hash, sceneNumber(object.vertexCount, 0));
      hash = sceneHashNumber(hash, object.static ? 1 : 0);
    }
    for (const material of materials) {
      hash = sceneHashString(hash, material && material.kind || "");
      hash = sceneHashString(hash, material && material.color || "");
      hash = sceneHashNumber(hash, sceneNumber(material && material.opacity, 1));
      hash = sceneHashNumber(hash, material && material.wireframe ? 1 : 0);
      hash = sceneHashString(hash, material && material.blendMode || "");
      hash = sceneHashNumber(hash, sceneNumber(material && material.emissive, 0));
    }
    for (let i = 0; i < positions.length; i += 1) {
      hash = sceneHashNumber(hash, positions[i]);
    }
    for (let i = 0; i < colors.length; i += 1) {
      hash = sceneHashNumber(hash, colors[i]);
    }
    for (let i = 0; i < materialData.length; i += 1) {
      hash = sceneHashNumber(hash, materialData[i]);
    }
    return String(hash) + ":" + positions.length + ":" + colors.length + ":" + materialData.length;
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

  function createSceneWebGLProgram(gl) {
    const vertexSource = [
      "attribute vec3 a_position;",
      "attribute vec4 a_color;",
      "attribute vec3 a_material;",
      "uniform vec4 u_camera;",
      "uniform float u_aspect;",
      "uniform float u_use_perspective;",
      "varying vec4 v_color;",
      "varying vec3 v_material;",
      "void main() {",
      "  vec4 clip = vec4(a_position.xy, 0.0, 1.0);",
      "  if (u_use_perspective > 0.5) {",
      "    float depth = a_position.z + u_camera.z;",
      "    if (depth <= 0.001) {",
      "      clip = vec4(2.0, 2.0, 0.0, 1.0);",
      "    } else {",
      "      float focal = 1.0 / tan(radians(u_camera.w) * 0.5);",
      "      vec2 projected = vec2((a_position.x - u_camera.x) * focal / depth, (a_position.y - u_camera.y) * focal / depth);",
      "      projected.x /= max(u_aspect, 0.0001);",
      "      clip = vec4(projected, 0.0, 1.0);",
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

  function createSceneRenderer(canvas, props) {
    if (sceneBool(props.preferWebGL, true)) {
      const webglRenderer = createSceneWebGLRenderer(canvas);
      if (webglRenderer) {
        return webglRenderer;
      }
    }
    const ctx2d = typeof canvas.getContext === "function" ? canvas.getContext("2d") : null;
    if (!ctx2d) {
      return null;
    }
    return createSceneCanvasRenderer(ctx2d, canvas);
  }

  window.__gosx_register_engine_factory("GoSXScene3D", async function(ctx) {
    if (!ctx.mount || typeof document.createElement !== "function") {
      console.warn("[gosx] Scene3D requires a mount element");
      return {};
    }

    const props = ctx.props || {};
    const width = Math.max(240, sceneNumber(props.width, 720));
    const height = Math.max(180, sceneNumber(props.height, 420));
    const sceneState = createSceneState(props);
    const runtimeScene = ctx.runtimeMode === "shared" && Boolean(ctx.programRef);
    const objects = sceneStateObjects(sceneState);
    const shouldAnimate = runtimeScene || sceneBool(props.autoRotate, true) || objects.some(function(object) {
      return object.spinX !== 0 || object.spinY !== 0 || object.spinZ !== 0;
    });

    clearChildren(ctx.mount);
    ctx.mount.setAttribute("data-gosx-scene3d-mounted", "true");
    ctx.mount.setAttribute("aria-label", props.ariaLabel || props.label || "Interactive GoSX 3D scene");

    const canvas = document.createElement("canvas");
    canvas.setAttribute("width", String(width));
    canvas.setAttribute("height", String(height));
    canvas.setAttribute("role", "img");
    canvas.setAttribute("aria-label", props.label || "Interactive GoSX 3D scene");
    canvas.setAttribute("style", "display:block;width:100%;height:auto;border-radius:22px;");
    canvas.width = width;
    canvas.height = height;
    ctx.mount.appendChild(canvas);

    const renderer = createSceneRenderer(canvas, props);
    if (!renderer) {
      console.warn("[gosx] Scene3D could not acquire a renderer");
      return {
        dispose() {
          if (canvas.parentNode === ctx.mount) {
            ctx.mount.removeChild(canvas);
          }
        },
      };
    }
    ctx.mount.setAttribute("data-gosx-scene3d-renderer", renderer.kind);

    let frameHandle = null;
    let disposed = false;

    if (runtimeScene) {
      if (ctx.runtime && ctx.runtime.available()) {
        applySceneCommands(sceneState, await ctx.runtime.hydrateFromProgramRef());
      } else {
        console.warn("[gosx] Scene3D runtime requested but shared engine runtime is unavailable");
      }
    }

    function renderFrame(now) {
      if (disposed) return;
      const timeSeconds = now / 1000;
      if (runtimeScene && ctx.runtime && typeof ctx.runtime.renderFrame === "function") {
        const runtimeBundle = ctx.runtime.renderFrame(timeSeconds, width, height);
        if (runtimeBundle) {
          renderer.render(runtimeBundle);
          if (shouldAnimate) {
            frameHandle = engineFrame(renderFrame);
          }
          return;
        }
      }
      if (runtimeScene && ctx.runtime) {
        applySceneCommands(sceneState, ctx.runtime.tick());
      }
      renderer.render(createSceneRenderBundle(width, height, sceneState.background, sceneState.camera, sceneStateObjects(sceneState), timeSeconds));
      if (shouldAnimate) {
        frameHandle = engineFrame(renderFrame);
      }
    }

    renderFrame(0);

    ctx.emit("mounted", {
      width,
      height,
      objects: objects.length,
    });

    return {
      applyCommands(commands) {
        applySceneCommands(sceneState, commands);
      },
      dispose() {
        disposed = true;
        renderer.dispose();
        if (frameHandle != null) {
          cancelEngineFrame(frameHandle);
        }
        if (canvas.parentNode === ctx.mount) {
          ctx.mount.removeChild(canvas);
        }
      },
    };
  });

  // --------------------------------------------------------------------------
  // Event delegation
  // --------------------------------------------------------------------------

  // Event types that are delegated on each island root element.
  const DELEGATED_EVENTS = [
    "click", "input", "change", "submit",
    "keydown", "keyup", "focus", "blur",
  ];

  // Extract a small payload from a DOM event for forwarding to WASM.
  function extractEventData(e) {
    const data = { type: e.type };

    switch (e.type) {
      case "input":
      case "change":
        if (e.target && e.target.value !== undefined) {
          data.value = e.target.value;
        }
        break;
      case "keydown":
      case "keyup":
        data.key = e.key;
        break;
      case "submit":
        // Prevent default form submission — the WASM handler decides what to do.
        e.preventDefault();
        break;
      // click, focus, blur: no extra data needed beyond type
    }

    return data;
  }

  // Attach ONE delegated listener per event type on `islandRoot`. Each
  // listener walks the ancestor chain from event.target to the root looking
  // for a `data-gosx-handler` attribute. If found, it calls the WASM-side
  // __gosx_action(islandID, handlerName, eventDataJSON).
  //
  // Returns an array of { type, listener } objects so callers can remove them.
  // Handler attribute pattern: data-gosx-on-{eventType}="handlerName"
  // Examples: data-gosx-on-click="increment", data-gosx-on-input="updateName"
  // Falls back to data-gosx-handler for click-only (legacy/shorthand).
  function findHandlerForEvent(target, root, eventType) {
    const specificAttr = handlerAttrName(eventType);

    let el = target;
    while (el && el !== root.parentNode) {
      const handlerName = elementHandlerName(el, eventType, specificAttr);
      if (handlerName) {
        return handlerName;
      }
      el = el.parentNode;
    }
    return null;
  }

  function handlerAttrName(eventType) {
    return "data-gosx-on-" + eventType;
  }

  function elementHandlerName(el, eventType, specificAttr) {
    if (hasAttributeName(el, specificAttr)) {
      return el.getAttribute(specificAttr);
    }
    if (eventType === "click" && hasAttributeName(el, "data-gosx-handler")) {
      return el.getAttribute("data-gosx-handler");
    }
    return null;
  }

  function hasAttributeName(el, attr) {
    return Boolean(el && el.hasAttribute && el.hasAttribute(attr));
  }

  function setupEventDelegation(islandRoot, islandID) {
    const entries = [];

    for (const eventType of DELEGATED_EVENTS) {
      const listener = createDelegatedListener(islandRoot, islandID, eventType);
      const useCapture = delegatedEventCapture(eventType);
      islandRoot.addEventListener(eventType, listener, useCapture);
      entries.push({ type: eventType, listener, capture: useCapture });
    }

    return entries;
  }

  function delegatedEventCapture(eventType) {
    return eventType === "focus" || eventType === "blur";
  }

  function createDelegatedListener(islandRoot, islandID, eventType) {
    return function(e) {
      if (e.__gosx_handled) return;

      const handlerName = findHandlerForEvent(e.target, islandRoot, eventType);
      if (!handlerName) return;

      e.__gosx_handled = true;
      dispatchIslandAction(islandID, handlerName, extractEventData(e));
    };
  }

  function dispatchIslandAction(islandID, handlerName, eventData) {
    const actionFn = window.__gosx_action;
    if (typeof actionFn !== "function") return;

    try {
      const result = actionFn(islandID, handlerName, JSON.stringify(eventData));
      if (typeof result === "string" && result !== "") {
        console.error(`[gosx] action error (${islandID}/${handlerName}):`, result);
      }
    } catch (err) {
      console.error(`[gosx] action error (${islandID}/${handlerName}):`, err);
    }
  }

  // --------------------------------------------------------------------------
  // Engine mounting
  // --------------------------------------------------------------------------

  function resolveEngineFactory(entry) {
    const exportName = engineExportName(entry);
    if (!exportName) return null;
    return engineFactories[exportName] || null;
  }

  function engineExportName(entry) {
    return entry.jsExport || entry.component;
  }

  function normalizeEngineHandle(result) {
    if (typeof result === "function") {
      return { dispose: result };
    }
    if (result && typeof result === "object") {
      return result;
    }
    return {};
  }

  function engineUsesSharedRuntime(entry) {
    return entry && entry.runtime === "shared";
  }

  function createEngineRuntime(entry) {
    let programPromise = null;

    function engineProgramFormat() {
      return inferProgramFormat(entry);
    }

    async function loadProgram() {
      if (!entry.programRef) {
        return null;
      }
      if (!programPromise) {
        programPromise = fetchProgram(entry.programRef, engineProgramFormat()).then(function(data) {
          return data == null ? null : { data, format: engineProgramFormat() };
        });
      }
      return programPromise;
    }

    function hydrateProgramData(program) {
      const hydrate = window.__gosx_hydrate_engine;
      if (typeof hydrate !== "function" || !program) {
        return [];
      }
      return decodeEngineCommands(hydrate(
        entry.id,
        entry.component,
        JSON.stringify(entry.props || {}),
        program.data,
        program.format || "json",
      ));
    }

    return {
      mode: entry.runtime || "",
      available() {
        return engineUsesSharedRuntime(entry)
          && typeof window.__gosx_hydrate_engine === "function"
          && typeof window.__gosx_tick_engine === "function"
          && typeof window.__gosx_render_engine === "function"
          && typeof window.__gosx_engine_dispose === "function";
      },
      async hydrateFromProgramRef() {
        if (!engineUsesSharedRuntime(entry)) {
          return [];
        }
        const program = await loadProgram();
        return hydrateProgramData(program);
      },
      tick() {
        if (!this.available()) {
          return [];
        }
        return decodeEngineCommands(window.__gosx_tick_engine(entry.id));
      },
      renderFrame(timeSeconds, width, height) {
        if (!engineUsesSharedRuntime(entry) || typeof window.__gosx_render_engine !== "function") {
          return null;
        }
        return decodeEngineRenderBundle(window.__gosx_render_engine(entry.id, timeSeconds, width, height));
      },
      dispose() {
        if (!engineUsesSharedRuntime(entry) || typeof window.__gosx_engine_dispose !== "function") {
          return;
        }
        window.__gosx_engine_dispose(entry.id);
      },
    };
  }

  function decodeEngineCommands(result) {
    if (result == null) {
      return [];
    }
    if (typeof result !== "string") {
      return [];
    }
    if (result === "" || result === "[]") {
      return [];
    }
    if (result.startsWith("error:") || result.startsWith("marshal:")) {
      console.error("[gosx] engine runtime error:", result);
      return [];
    }
    try {
      const commands = JSON.parse(result);
      return Array.isArray(commands) ? commands : [];
    } catch (e) {
      console.error("[gosx] failed to decode engine commands:", e);
      return [];
    }
  }

  function decodeEngineRenderBundle(result) {
    if (result == null || typeof result !== "string" || result === "") {
      return null;
    }
    if (result.startsWith("error:") || result.startsWith("marshal:")) {
      console.error("[gosx] engine runtime error:", result);
      return null;
    }
    try {
      const bundle = JSON.parse(result);
      return normalizeEngineRenderBundle(bundle);
    } catch (e) {
      console.error("[gosx] failed to decode engine render bundle:", e);
      return null;
    }
  }

  function normalizeEngineRenderBundle(bundle) {
    if (!bundle || typeof bundle !== "object") {
      return null;
    }
    bundle.positions = sceneFloatArray(bundle.positions);
    bundle.colors = sceneFloatArray(bundle.colors);
    bundle.worldPositions = sceneFloatArray(bundle.worldPositions);
    bundle.worldColors = sceneFloatArray(bundle.worldColors);
    return bundle;
  }

  function sceneFloatArray(values) {
    if (values instanceof Float32Array) {
      return values;
    }
    if (Array.isArray(values)) {
      return new Float32Array(values);
    }
    return new Float32Array(0);
  }

  function createEngineContext(entry, mount) {
    return {
      id: entry.id,
      kind: entry.kind,
      component: entry.component,
      mount: mount,
      props: entry.props || {},
      capabilities: entry.capabilities || [],
      programRef: entry.programRef || "",
      runtimeMode: entry.runtime || "",
      jsRef: entry.jsRef || "",
      jsExport: entry.jsExport || "",
      runtime: createEngineRuntime(entry),
      emit: function(name, detail) {
        if (typeof document.dispatchEvent === "function" && typeof CustomEvent === "function") {
          document.dispatchEvent(new CustomEvent("gosx:engine:" + name, {
            detail: {
              engineID: entry.id,
              component: entry.component,
              detail: detail,
            },
          }));
        }
      },
    };
  }

  async function mountEngine(entry) {
    const existing = window.__gosx.engines.get(entry.id);
    if (existing) {
      window.__gosx_dispose_engine(entry.id);
    }

    const mount = resolveEngineMount(entry);
    if (entry.kind === "surface" && !mount) return;

    const factory = await resolveMountedEngineFactory(entry);
    if (typeof factory !== "function") {
      console.warn(`[gosx] no engine factory registered for ${entry.component}`);
      return;
    }

    try {
      const mounted = await runEngineFactory(factory, entry, mount);
      rememberMountedEngine(entry, mount, mounted.context, mounted.handle);
    } catch (e) {
      console.error(`[gosx] failed to mount engine ${entry.id}:`, e);
    }
  }

  function resolveEngineMount(entry) {
    if (entry.kind !== "surface") {
      return null;
    }
    const mountID = entry.mountId || entry.id;
    const mount = document.getElementById(mountID);
    if (!mount) {
      console.warn(`[gosx] engine mount #${mountID} not found for ${entry.id}`);
      return null;
    }
    return mount;
  }

  async function resolveMountedEngineFactory(entry) {
    let factory = resolveEngineFactory(entry);
    if (!factory && entry.jsRef) {
      await loadEngineScript(entry.jsRef);
      factory = resolveEngineFactory(entry);
    }
    return factory;
  }

  async function runEngineFactory(factory, entry, mount) {
    const ctx = createEngineContext(entry, mount);
    let result = factory(ctx);
    if (result && typeof result.then === "function") {
      result = await result;
    }
    return {
      context: ctx,
      handle: normalizeEngineHandle(result),
    };
  }

  function rememberMountedEngine(entry, mount, context, handle) {
    activateInputProviders(entry);
    window.__gosx.engines.set(entry.id, {
      component: entry.component,
      kind: entry.kind,
      capabilities: capabilityList(entry),
      runtime: context.runtime,
      mount: mount,
      handle: handle,
    });
  }

  async function mountAllEngines(manifest) {
    if (!manifest.engines || manifest.engines.length === 0) return;

    const promises = manifest.engines.map(function(entry) {
      return mountEngine(entry).catch(function(e) {
        console.error(`[gosx] unexpected error mounting engine ${entry.id}:`, e);
      });
    });

    await Promise.all(promises);
  }

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

  function applyHubBindings(entry, message) {
    if (!entry.bindings || entry.bindings.length === 0) return;
    const setSharedSignal = window.__gosx_set_shared_signal;
    if (typeof setSharedSignal !== "function") return;

    for (const binding of entry.bindings) {
      applyHubBinding(entry, binding, message, setSharedSignal);
    }
  }

  function applyHubBinding(entry, binding, message, setSharedSignal) {
    if (!binding || binding.event !== message.event || !binding.signal) return;
    try {
      const result = setSharedSignal(binding.signal, JSON.stringify(message.data));
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
    socket.onmessage = function(evt) {
      const message = decodeHubMessage(entry, evt.data);
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
    try {
      return JSON.parse(raw);
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

  // --------------------------------------------------------------------------
  // Island disposal
  // --------------------------------------------------------------------------

  // Remove all delegated event listeners for an island and clear it from the
  // tracking map. Optionally calls the WASM-side __gosx_dispose if available.
  window.__gosx_dispose_island = function(islandID) {
    const record = window.__gosx.islands.get(islandID);
    if (!record) return;

    // Remove delegated listeners from the island root.
    if (record.root && record.listeners) {
      for (const entry of record.listeners) {
        record.root.removeEventListener(entry.type, entry.listener, entry.capture);
      }
    }

    // Notify WASM side if dispose function is available.
    if (typeof window.__gosx_dispose === "function") {
      try {
        window.__gosx_dispose(islandID);
      } catch (e) {
        console.error(`[gosx] dispose error for ${islandID}:`, e);
      }
    }

    window.__gosx.islands.delete(islandID);
  };

  window.__gosx_dispose_engine = function(engineID) {
    const record = window.__gosx.engines.get(engineID);
    if (!record) return;

    releaseInputProviders(record);

    if (record.runtime && typeof record.runtime.dispose === "function") {
      try {
        record.runtime.dispose();
      } catch (e) {
        console.error(`[gosx] runtime dispose error for engine ${engineID}:`, e);
      }
    }

    if (record.handle && typeof record.handle.dispose === "function") {
      try {
        record.handle.dispose();
      } catch (e) {
        console.error(`[gosx] dispose error for engine ${engineID}:`, e);
      }
    }

    window.__gosx.engines.delete(engineID);
  };

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

  async function disposePage() {
    for (const islandID of Array.from(window.__gosx.islands.keys())) {
      window.__gosx_dispose_island(islandID);
    }
    for (const engineID of Array.from(window.__gosx.engines.keys())) {
      window.__gosx_dispose_engine(engineID);
    }
    for (const hubID of Array.from(window.__gosx.hubs.keys())) {
      window.__gosx_disconnect_hub(hubID);
    }
    pendingManifest = null;
    window.__gosx.ready = false;
  }

  // --------------------------------------------------------------------------
  // Hydration
  // --------------------------------------------------------------------------

  // Hydrate a single island: fetch its program data, call __gosx_hydrate,
  // and set up event delegation on the island root element.
  async function hydrateIsland(entry) {
    const root = islandRoot(entry);
    if (!root) return;
    if (entry.static) return;

    const program = await loadIslandProgram(entry);
    if (!program) return;
    if (!runIslandHydration(entry, program)) return;
    const listeners = setupEventDelegation(root, entry.id);
    rememberHydratedIsland(entry, root, listeners);
  }

  function islandRoot(entry) {
    const root = document.getElementById(entry.id);
    if (!root) {
      console.warn(`[gosx] island root #${entry.id} not found in DOM`);
      return null;
    }
    return root;
  }

  async function loadIslandProgram(entry) {
    const programFormat = inferProgramFormat(entry);
    if (!entry.programRef) {
      console.error(`[gosx] skipping island ${entry.id} — missing programRef`);
      return null;
    }

    const programData = await fetchProgram(entry.programRef, programFormat);
    if (programData === null) {
      console.error(`[gosx] skipping island ${entry.id} — program fetch failed`);
      return null;
    }
    return { data: programData, format: programFormat };
  }

  function runIslandHydration(entry, program) {
    const hydrateFn = window.__gosx_hydrate;
    if (typeof hydrateFn !== "function") {
      console.error("[gosx] __gosx_hydrate not available — cannot hydrate island", entry.id);
      return false;
    }

    try {
      const result = hydrateFn(
        entry.id,
        entry.component,
        JSON.stringify(entry.props || {}),
        program.data,
        program.format
      );
      if (typeof result === "string" && result !== "") {
        console.error(`[gosx] failed to hydrate island ${entry.id}: ${result}`);
        return false;
      }
      return true;
    } catch (e) {
      console.error(`[gosx] failed to hydrate island ${entry.id}:`, e);
      return false;
    }
  }

  function rememberHydratedIsland(entry, root, listeners) {
    window.__gosx.islands.set(entry.id, {
      component: entry.component,
      root: root,
      listeners: listeners,
    });
  }

  // Hydrate all islands from the manifest. Called once the WASM runtime
  // signals readiness via __gosx_runtime_ready.
  async function hydrateAllIslands(manifest) {
    if (!manifest.islands || manifest.islands.length === 0) return;

    // Hydrate islands concurrently — each is independent.
    const promises = manifest.islands.map(function(entry) {
      return hydrateIsland(entry).catch(function(e) {
        console.error(`[gosx] unexpected error hydrating ${entry.id}:`, e);
      });
    });

    await Promise.all(promises);
  }

  // --------------------------------------------------------------------------
  // Runtime ready callback
  // --------------------------------------------------------------------------

  // Called by the Go WASM binary once the runtime has finished initializing
  // and all exported functions (__gosx_hydrate, __gosx_action, etc.) are
  // registered. This is the signal that it is safe to hydrate islands.
  window.__gosx_runtime_ready = function() {
    if (!pendingManifest) {
      window.__gosx.ready = true;
      return;
    }

    Promise.all([
      hydrateAllIslands(pendingManifest),
      mountAllEngines(pendingManifest),
      connectAllHubs(pendingManifest),
    ]).then(function() {
      window.__gosx.ready = true;
      document.dispatchEvent(new CustomEvent("gosx:ready"));
    }).catch(function(e) {
      console.error("[gosx] bootstrap failed:", e);
      window.__gosx.ready = true;
    });
  };

  // --------------------------------------------------------------------------
  // Main initialization
  // --------------------------------------------------------------------------

  async function bootstrapPage() {
    const manifest = loadManifest();
    if (!manifest) {
      // No manifest — pure server-rendered page, no islands to hydrate.
      pendingManifest = null;
      window.__gosx.ready = true;
      return;
    }

    // Stash manifest for use when WASM signals readiness.
    pendingManifest = manifest;
    window.__gosx.ready = false;

    if (manifestNeedsRuntime(manifest)) {
      if (runtimeReady()) {
        window.__gosx_runtime_ready();
      } else {
        await loadRuntime(manifest.runtime);
      }
    } else {
      if (manifestNeedsRuntimeBridge(manifest)) {
        console.error("[gosx] islands and hub bindings require manifest.runtime.path");
      }
      window.__gosx_runtime_ready();
    }
  }

  function manifestNeedsRuntimeBridge(manifest) {
    return manifestHasEntries(manifest, "islands")
      || manifestHasEntries(manifest, "hubs")
      || manifestNeedsEngineInputBridge(manifest)
      || manifestNeedsSharedEngineRuntime(manifest);
  }

  function manifestNeedsRuntime(manifest) {
    return Boolean(manifestNeedsRuntimeBridge(manifest) && manifest.runtime && manifest.runtime.path);
  }

  function manifestNeedsEngineInputBridge(manifest) {
    if (!manifestHasEntries(manifest, "engines")) {
      return false;
    }
    return manifest.engines.some(function(entry) {
      const capabilities = capabilityList(entry);
      return capabilities.includes("keyboard") || capabilities.includes("pointer") || capabilities.includes("gamepad");
    });
  }

  function manifestNeedsSharedEngineRuntime(manifest) {
    if (!manifestHasEntries(manifest, "engines")) {
      return false;
    }
    return manifest.engines.some(function(entry) {
      return engineUsesSharedRuntime(entry);
    });
  }

  function manifestHasEntries(manifest, key) {
    return Boolean(manifest && manifest[key] && manifest[key].length > 0);
  }

  window.__gosx_bootstrap_page = bootstrapPage;
  window.__gosx_dispose_page = disposePage;

  // Start when DOM is ready.
  if (document.readyState === "loading") {
    document.addEventListener("DOMContentLoaded", bootstrapPage);
  } else {
    bootstrapPage();
  }
})();
