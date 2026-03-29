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

  function sceneClamp(value, min, max) {
    return Math.max(min, Math.min(max, value));
  }

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

  function publishSceneDragSignals(namespace, state, active) {
    if (!namespace) {
      return;
    }
    queueInputSignal(namespace + ".x", sceneNumber(state.orbitX, 0));
    queueInputSignal(namespace + ".y", sceneNumber(state.orbitY, 0));
    queueInputSignal(namespace + ".targetIndex", Math.max(-1, Math.floor(sceneNumber(state.targetIndex, -1))));
    queueInputSignal(namespace + ".active", Boolean(active));
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
    return extents[0] > 0.6 && extents[1] > 0.35;
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
      const from = projectPoint(fromWorld, bundle.camera, width, height);
      const to = projectPoint(toWorld, bundle.camera, width, height);
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
      return 12;
    }
    const span = Math.max(bounds.maxX - bounds.minX, bounds.maxY - bounds.minY);
    return sceneClamp(span * 0.08, 12, 22);
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
      return sceneNumber(camera && camera.z, 6);
    }
    const minZ = sceneNumber(bounds.minZ, 0);
    const maxZ = sceneNumber(bounds.maxZ, minZ);
    return ((minZ + maxZ) / 2) + sceneNumber(camera && camera.z, 6);
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

  function sceneBundlePointerDragTarget(bundle, point, width, height) {
    if (!bundle || !bundle.camera || !Array.isArray(bundle.objects) || !bundle.objects.length) {
      return null;
    }
    let best = null;
    for (let index = 0; index < bundle.objects.length; index += 1) {
      const object = bundle.objects[index];
      if (!sceneObjectAllowsPointerDrag(object)) {
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

  function sceneViewportValue(viewport, key, fallback) {
    return sceneNumber(viewport && viewport[key], fallback);
  }

  function setupSceneDragInteractions(canvas, props, readViewport, readSceneBundle) {
    if (!canvas || !sceneBool(props.dragToRotate, false)) {
      return { dispose() {} };
    }

    const dragNamespace = sceneDragSignalNamespace(props);
    const initialViewport = typeof readViewport === "function" ? readViewport() : null;
    const initialWidth = Math.max(1, sceneViewportValue(initialViewport, "cssWidth", sceneNumber(props.width, 720)));
    const initialHeight = Math.max(1, sceneViewportValue(initialViewport, "cssHeight", sceneNumber(props.height, 420)));
    const state = {
      active: false,
      orbitX: 0,
      orbitY: 0,
      pointerId: null,
      targetIndex: -1,
      lastX: initialWidth / 2,
      lastY: initialHeight / 2,
    };

    canvas.style.cursor = "grab";
    canvas.style.touchAction = "none";

    function publish(event, phase) {
      const viewport = typeof readViewport === "function" ? readViewport() : null;
      const width = Math.max(1, sceneViewportValue(viewport, "cssWidth", initialWidth));
      const height = Math.max(1, sceneViewportValue(viewport, "cssHeight", initialHeight));
      const sample = sceneLocalPointerSample(event, canvas, width, height, state, phase);
      if (!dragNamespace) {
        publishPointerSignals(sample);
        return;
      }
      if (phase === "move") {
        state.orbitX = sceneClamp(state.orbitX + sample.deltaX / Math.max(width / 2, 1), -1.35, 1.35);
        state.orbitY = sceneClamp(state.orbitY - sample.deltaY / Math.max(height / 2, 1), -1.1, 1.1);
      }
      publishSceneDragSignals(dragNamespace, state, phase !== "end");
    }

    function pointerMatchesActiveDrag(event) {
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

    function onPointerDown(event) {
      if (event.button !== 0) {
        return;
      }
      const viewport = typeof readViewport === "function" ? readViewport() : null;
      const width = Math.max(1, sceneViewportValue(viewport, "cssWidth", initialWidth));
      const height = Math.max(1, sceneViewportValue(viewport, "cssHeight", initialHeight));
      const pointer = sceneLocalPointerPoint(event, canvas, width, height);
      const target = sceneBundlePointerDragTarget(readSceneBundle && readSceneBundle(), pointer, width, height);
      if (!target) {
        return;
      }
      state.active = true;
      state.pointerId = event.pointerId;
      state.targetIndex = target.index;
      canvas.style.cursor = "grabbing";
      if (typeof canvas.setPointerCapture === "function") {
        canvas.setPointerCapture(event.pointerId);
      }
      event.preventDefault();
      event.stopPropagation();
      publish(event, "start");
    }

    function onPointerMove(event) {
      if (!pointerMatchesActiveDrag(event)) {
        return;
      }
      event.preventDefault();
      event.stopPropagation();
      publish(event, "move");
    }

    function finishDrag(event) {
      if (!pointerMatchesActiveDrag(event)) {
        return;
      }
      state.active = false;
      canvas.style.cursor = "grab";
      event.preventDefault();
      event.stopPropagation();
      if (state.pointerId != null && typeof canvas.releasePointerCapture === "function") {
        try {
          canvas.releasePointerCapture(state.pointerId);
        } catch (_) {}
      }
      state.pointerId = null;
      state.targetIndex = -1;
      if (dragNamespace) {
        publish(event, "end");
      } else {
        const viewport = typeof readViewport === "function" ? readViewport() : null;
        const width = Math.max(1, sceneViewportValue(viewport, "cssWidth", initialWidth));
        const height = Math.max(1, sceneViewportValue(viewport, "cssHeight", initialHeight));
        resetScenePointerSample(width, height, state);
      }
    }

    canvas.addEventListener("pointerdown", onPointerDown);
    canvas.addEventListener("pointermove", onPointerMove);
    canvas.addEventListener("pointerup", finishDrag);
    canvas.addEventListener("pointercancel", finishDrag);
    canvas.addEventListener("lostpointercapture", finishDrag);
    document.addEventListener("pointermove", onPointerMove);
    document.addEventListener("pointerup", finishDrag);
    document.addEventListener("pointercancel", finishDrag);

    return {
      dispose() {
        canvas.removeEventListener("pointerdown", onPointerDown);
        canvas.removeEventListener("pointermove", onPointerMove);
        canvas.removeEventListener("pointerup", finishDrag);
        canvas.removeEventListener("pointercancel", finishDrag);
        canvas.removeEventListener("lostpointercapture", finishDrag);
        document.removeEventListener("pointermove", onPointerMove);
        document.removeEventListener("pointerup", finishDrag);
        document.removeEventListener("pointercancel", finishDrag);
        canvas.style.cursor = "";
        canvas.style.touchAction = "";
        const viewport = typeof readViewport === "function" ? readViewport() : null;
        resetScenePointerSample(
          Math.max(1, sceneViewportValue(viewport, "cssWidth", initialWidth)),
          Math.max(1, sceneViewportValue(viewport, "cssHeight", initialHeight)),
          state,
        );
      },
    };
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
      shiftX: sceneNumber(item.shiftX, 0),
      shiftY: sceneNumber(item.shiftY, 0),
      shiftZ: sceneNumber(item.shiftZ, 0),
      driftSpeed: sceneNumber(item.driftSpeed, 0),
      driftPhase: sceneNumber(item.driftPhase, 0),
    };
  }

  function normalizeSceneLabel(label, index) {
    const item = label && typeof label === "object" ? label : {};
    return {
      id: item.id || ("scene-label-" + index),
      text: typeof item.text === "string" ? item.text : "",
      className: sceneLabelClassName(item),
      x: sceneNumber(item.x, 0),
      y: sceneNumber(item.y, 0),
      z: sceneNumber(item.z, 0),
      priority: sceneNumber(item.priority, 0),
      shiftX: sceneNumber(item.shiftX, 0),
      shiftY: sceneNumber(item.shiftY, 0),
      shiftZ: sceneNumber(item.shiftZ, 0),
      driftSpeed: sceneNumber(item.driftSpeed, 0),
      driftPhase: sceneNumber(item.driftPhase, 0),
      maxWidth: Math.max(48, sceneNumber(item.maxWidth, 180)),
      maxLines: Math.max(0, Math.floor(sceneNumber(item.maxLines, 0))),
      overflow: normalizeTextLayoutOverflow(item.overflow),
      font: typeof item.font === "string" && item.font ? item.font : '600 13px "IBM Plex Sans", "Segoe UI", sans-serif',
      lineHeight: Math.max(12, sceneNumber(item.lineHeight, 18)),
      color: typeof item.color === "string" && item.color ? item.color : "#ecf7ff",
      background: typeof item.background === "string" && item.background ? item.background : "rgba(8, 21, 31, 0.82)",
      borderColor: typeof item.borderColor === "string" && item.borderColor ? item.borderColor : "rgba(141, 225, 255, 0.24)",
      offsetX: sceneNumber(item.offsetX, 0),
      offsetY: sceneNumber(item.offsetY, -14),
      anchorX: Math.max(0, Math.min(1, sceneNumber(item.anchorX, 0.5))),
      anchorY: Math.max(0, Math.min(1, sceneNumber(item.anchorY, 1))),
      collision: normalizeSceneLabelCollision(item.collision),
      occlude: sceneBool(item.occlude, false),
      whiteSpace: normalizeSceneLabelWhiteSpace(item.whiteSpace),
      textAlign: normalizeSceneLabelAlign(item.textAlign),
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
        return normalizeSceneLabel(label, index);
      })
      .filter(function(label) {
        return label.text.trim() !== "";
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
      labels: new Map(),
    };
    for (const object of sceneObjects(props)) {
      state.objects.set(object.id, object);
    }
    for (const label of sceneLabels(props)) {
      state.labels.set(label.id, label);
    }
    return state;
  }

  function sceneStateObjects(state) {
    return Array.from(state.objects.values());
  }

  function sceneStateLabels(state) {
    return Array.from(state.labels.values());
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
    if (payload.kind === "label") {
      const label = sceneLabelFromPayload(objectID, payload, state.labels.get(sceneObjectKey(objectID)));
      if (label) {
        state.labels.set(sceneObjectKey(objectID), label);
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
    if (!currentLabel) return;
    const nextLabel = sceneLabelFromPayload(objectID, {
      props: Object.assign({}, currentLabel, patch || {}),
    }, currentLabel);
    if (nextLabel) {
      state.labels.set(key, nextLabel);
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

  function sceneLabelFromPayload(objectID, payload, fallback) {
    const current = fallback && typeof fallback === "object" ? fallback : {};
    const props = payload && payload.props && typeof payload.props === "object" ? payload.props : {};
    const merged = Object.assign({}, current, props);
    merged.id = current.id || merged.id || ("scene-label-" + objectID);
    const label = normalizeSceneLabel(merged, objectID);
    if (!label.text.trim()) {
      return null;
    }
    return label;
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
    const normalizedCamera = sceneRenderCamera(camera);
    const depth = sceneNumber(point && point.z, 0) + normalizedCamera.z;
    if (depth <= normalizedCamera.near || depth >= normalizedCamera.far) return null;
    const focal = (Math.min(width, height) / 2) / Math.tan((normalizedCamera.fov * Math.PI) / 360);
    return {
      x: width / 2 + ((sceneNumber(point && point.x, 0) - normalizedCamera.x) * focal) / depth,
      y: height / 2 - ((sceneNumber(point && point.y, 0) - normalizedCamera.y) * focal) / depth,
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

  function sceneRenderCamera(camera) {
    return {
      x: sceneNumber(camera && camera.x, 0),
      y: sceneNumber(camera && camera.y, 0),
      z: sceneNumber(camera && camera.z, 6),
      fov: sceneNumber(camera && camera.fov, 75),
      near: sceneNumber(camera && camera.near, 0.05),
      far: sceneNumber(camera && camera.far, 128),
    };
  }

  function createSceneRenderBundle(width, height, background, camera, objects, labels, timeSeconds) {
    const bundle = {
      background: background,
      camera: sceneRenderCamera(camera),
      objects: [],
      labels: [],
      lines: [],
      positions: [],
      colors: [],
      worldPositions: [],
      worldColors: [],
      vertexCount: 0,
      worldVertexCount: 0,
      objectCount: 0,
    };
    appendSceneGridToBundle(bundle, width, height);
    for (const object of objects) {
      appendSceneObjectToBundle(bundle, camera, width, height, object, timeSeconds);
    }
    for (const label of labels || []) {
      appendSceneLabelToBundle(bundle, camera, width, height, label, timeSeconds);
    }
    bundle.positions = new Float32Array(bundle.positions);
    bundle.colors = new Float32Array(bundle.colors);
    bundle.vertexCount = bundle.positions.length / 2;
    bundle.worldPositions = new Float32Array(bundle.worldPositions);
    bundle.worldColors = new Float32Array(bundle.worldColors);
    bundle.worldVertexCount = bundle.worldPositions.length / 3;
    bundle.objectCount = bundle.objects.length;
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
    const motion = sceneMotionOffset(object, timeSeconds);
    return {
      x: rotated.x + object.x + motion.x,
      y: rotated.y + object.y + motion.y,
      z: rotated.z + object.z + motion.z,
    };
  }

  function sceneMotionOffset(object, timeSeconds) {
    if (!object || (!object.shiftX && !object.shiftY && !object.shiftZ)) {
      return { x: 0, y: 0, z: 0 };
    }
    const angle = sceneNumber(object.driftPhase, 0) + timeSeconds * sceneNumber(object.driftSpeed, 0);
    return {
      x: Math.cos(angle) * sceneNumber(object.shiftX, 0),
      y: Math.sin(angle * 0.82 + sceneNumber(object.driftPhase, 0) * 0.35) * sceneNumber(object.shiftY, 0),
      z: Math.sin(angle) * sceneNumber(object.shiftZ, 0),
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
    const worldSegments = sceneWorldObjectSegments(object, timeSeconds);
    const vertexOffset = bundle.worldPositions.length / 3;
    const rgba = sceneColorRGBA(object.color, [0.55, 0.88, 1, 1]);
    let bounds = null;
    let vertexCount = 0;
    for (const segment of worldSegments) {
      const fromWorld = segment[0];
      const toWorld = segment[1];
      bundle.worldPositions.push(fromWorld.x, fromWorld.y, fromWorld.z, toWorld.x, toWorld.y, toWorld.z);
      bundle.worldColors.push(rgba[0], rgba[1], rgba[2], rgba[3], rgba[0], rgba[1], rgba[2], rgba[3]);
      bounds = sceneExpandWorldBounds(bounds, fromWorld);
      bounds = sceneExpandWorldBounds(bounds, toWorld);
      vertexCount += 2;
      const from = projectPoint(fromWorld, camera, width, height);
      const to = projectPoint(toWorld, camera, width, height);
      if (!from || !to) continue;
      appendSceneLine(bundle, width, height, from, to, object.color, 1.8);
    }
    if (vertexCount > 0) {
      bundle.objects.push({
        id: object.id,
        kind: object.kind,
        vertexOffset: vertexOffset,
        vertexCount: vertexCount,
        bounds: bounds || {
          minX: 0,
          minY: 0,
          minZ: 0,
          maxX: 0,
          maxY: 0,
          maxZ: 0,
        },
      });
    }
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

  function appendSceneLabelToBundle(bundle, camera, width, height, label, timeSeconds) {
    const point = sceneLabelPoint(label, timeSeconds);
    const projected = projectPoint(point, camera, width, height);
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

  function sceneWorldObjectSegments(object, timeSeconds) {
    return sceneObjectSegments(object).map(function(segment) {
      return [
        translateScenePoint(segment[0], object, timeSeconds),
        translateScenePoint(segment[1], object, timeSeconds),
      ];
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
        for (const line of lines) {
          ctx2d.strokeStyle = line.color;
          ctx2d.lineWidth = line.lineWidth;
          strokeLine(ctx2d, line.from, line.to);
        }
        if (typeof ctx2d.restore === "function") {
          ctx2d.restore();
        }
      },
      dispose() {},
    };
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
    const gl = canvas.getContext("webgl", contextOptions) || canvas.getContext("experimental-webgl", contextOptions);
    if (!gl) {
      return null;
    }

    const program = createSceneWebGLProgram(gl);
    if (!program) {
      return null;
    }

    const resources = createSceneWebGLResources(gl, program);
    return {
      kind: "webgl",
      render(bundle) {
        const geometry = sceneWebGLBundleGeometry(bundle);
        prepareSceneWebGLFrame(gl, canvas, bundle, geometry.usePerspective, resources);
        if (!bundle || geometry.vertexCount === 0 || !geometry.positions || !geometry.colors) {
          return;
        }
        gl.useProgram(program);
        applySceneWebGLUniforms(gl, bundle, canvas, geometry.usePerspective, resources);
        if (geometry.usePerspective && renderSceneWebGLWorldBundle(gl, bundle, resources)) {
          applySceneWebGLBlend(gl, "opaque", resources.stateCache);
          applySceneWebGLDepth(gl, "opaque", resources.stateCache);
          return;
        }
        renderSceneWebGLFallbackBundle(gl, geometry, resources);
      },
      dispose() {
        disposeSceneWebGLRenderer(gl, program, resources);
      },
    };
  }

  function createSceneWebGLResources(gl, program) {
    return {
      fallbackBuffers: createSceneWebGLBufferSet(gl),
      passBuffers: {
        staticOpaque: createSceneWebGLBufferSet(gl),
        alpha: createSceneWebGLBufferSet(gl),
        additive: createSceneWebGLBufferSet(gl),
        dynamicOpaque: createSceneWebGLBufferSet(gl),
      },
      drawScratch: createSceneWorldDrawScratch(),
      positionLocation: gl.getAttribLocation(program, "a_position"),
      colorLocation: gl.getAttribLocation(program, "a_color"),
      materialLocation: gl.getAttribLocation(program, "a_material"),
      cameraLocation: gl.getUniformLocation(program, "u_camera"),
      aspectLocation: gl.getUniformLocation(program, "u_aspect"),
      perspectiveLocation: gl.getUniformLocation(program, "u_use_perspective"),
      floatType: typeof gl.FLOAT === "number" ? gl.FLOAT : 0x1406,
      arrayBuffer: typeof gl.ARRAY_BUFFER === "number" ? gl.ARRAY_BUFFER : 0x8892,
      staticDraw: typeof gl.STATIC_DRAW === "number" ? gl.STATIC_DRAW : 0x88E4,
      dynamicDraw: typeof gl.DYNAMIC_DRAW === "number" ? gl.DYNAMIC_DRAW : 0x88E8,
      colorBufferBit: typeof gl.COLOR_BUFFER_BIT === "number" ? gl.COLOR_BUFFER_BIT : 0x4000,
      depthBufferBit: typeof gl.DEPTH_BUFFER_BIT === "number" ? gl.DEPTH_BUFFER_BIT : 0x0100,
      linesMode: typeof gl.LINES === "number" ? gl.LINES : 0x0001,
      passCache: {
        staticOpaque: {
          key: "",
          vertexCount: 0,
        },
      },
      stateCache: {
        blendMode: "",
        depthMode: "",
      },
    };
  }

  function sceneWebGLBundleGeometry(bundle) {
    const usePerspective = Boolean(bundle && bundle.worldVertexCount > 0 && bundle.worldPositions && bundle.worldColors);
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
    if (typeof gl.uniform4f === "function" && resources.cameraLocation) {
      const camera = bundle.camera || {};
      gl.uniform4f(
        resources.cameraLocation,
        sceneNumber(camera.x, 0),
        sceneNumber(camera.y, 0),
        sceneNumber(camera.z, 6),
        sceneNumber(camera.fov, 75),
      );
    }
    if (typeof gl.uniform1f === "function" && resources.aspectLocation) {
      gl.uniform1f(resources.aspectLocation, aspect);
    }
    if (typeof gl.uniform1f === "function" && resources.perspectiveLocation) {
      gl.uniform1f(resources.perspectiveLocation, usePerspective ? 1 : 0);
    }
  }

  function renderSceneWebGLWorldBundle(gl, bundle, resources) {
    const bundledPasses = createSceneWorldWebGLPassesFromBundle(bundle, resources.passBuffers, {
      staticDraw: resources.staticDraw,
      dynamicDraw: resources.dynamicDraw,
    });
    if (bundledPasses.length > 0) {
      drawSceneWebGLPasses(gl, resources.arrayBuffer, resources.floatType, resources.linesMode, resources.positionLocation, resources.colorLocation, resources.materialLocation, bundledPasses, resources.passCache, resources.stateCache);
      return true;
    }
    const drawPlan = buildSceneWorldDrawPlan(bundle, resources.drawScratch);
    if (!drawPlan) {
      return false;
    }
    const worldPasses = createSceneWorldWebGLPasses(drawPlan, resources.passBuffers, {
      staticDraw: resources.staticDraw,
      dynamicDraw: resources.dynamicDraw,
    });
    drawSceneWebGLPasses(gl, resources.arrayBuffer, resources.floatType, resources.linesMode, resources.positionLocation, resources.colorLocation, resources.materialLocation, worldPasses, resources.passCache, resources.stateCache);
    return true;
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
    }
    if (typeof gl.deleteProgram === "function") {
      gl.deleteProgram(program);
    }
  }

  function createSceneWebGLBufferSet(gl) {
    return {
      position: gl.createBuffer(),
      color: gl.createBuffer(),
      material: gl.createBuffer(),
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
      const name = String(source && source.name || "");
      const targetBuffers = buffers[name];
      if (!targetBuffers) {
        continue;
      }
      const isStatic = Boolean(source && source.static);
      const positions = sceneTypedFloatArray(source && source.positions);
      const colors = sceneTypedFloatArray(source && source.colors);
      const materials = sceneTypedFloatArray(source && source.materials);
      const vertexCount = Number.isFinite(source && source.vertexCount) ? source.vertexCount : positions.length / 3;
      passes.push({
        name,
        blend: String(source && source.blend || "opaque"),
        depth: String(source && source.depth || "opaque"),
        usage: isStatic ? usages.staticDraw : usages.dynamicDraw,
        cacheSlot: isStatic ? "staticOpaque" : "",
        cacheKey: String(source && source.cacheKey || ""),
        buffers: targetBuffers,
        positions,
        colors,
        materials,
        vertexCount,
      });
    }
    return passes;
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
    if (!sceneWorldObjectRenderable(object)) {
      return;
    }
    const material = materials[object.materialIndex] || null;
    const renderPass = sceneWorldObjectRenderPass(object, material);
    if (renderPass === "additive" || renderPass === "alpha") {
      collectSceneWorldTranslucentObject(drawScratch, bundle, object, material, renderPass, order);
      return;
    }
    collectSceneWorldOpaqueObject(drawScratch, bundle, object, material);
  }

  function sceneWorldObjectRenderable(object) {
    return Boolean(
      object &&
      Number.isFinite(object.vertexOffset) &&
      Number.isFinite(object.vertexCount) &&
      object.vertexCount > 0 &&
      !sceneWorldObjectCulled(object)
    );
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
    plan.staticOpaqueKey = sceneStaticDrawKey(drawScratch.staticOpaqueObjects, drawScratch.staticOpaqueMaterialProfiles, bundle.camera);
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

  function sceneWorldObjectDepth(sourcePositions, object, camera) {
    if (object && Number.isFinite(object.depthCenter)) {
      return sceneNumber(object.depthCenter, sceneWorldPointDepth(0, camera));
    }
    const vertexOffset = Math.max(0, Math.floor(sceneNumber(object && object.vertexOffset, 0)));
    const vertexCount = Math.max(0, Math.floor(sceneNumber(object && object.vertexCount, 0)));
    if (!vertexCount) {
      return sceneWorldPointDepth(0, camera);
    }
    const start = vertexOffset * 3 + 2;
    const end = start + vertexCount * 3;
    let depth = 0;
    let count = 0;
    for (let i = start; i < end; i += 3) {
      depth += sceneNumber(sourcePositions[i], 0);
      count += 1;
    }
    return depth / Math.max(1, count) + sceneWorldPointDepth(0, camera);
  }

  function sceneWorldObjectCulled(object) {
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

  function sceneWorldPointDepth(z, camera) {
    return sceneNumber(z, 0) + sceneNumber(camera && camera.z, 6);
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
      return [
        sceneNumber(material.shaderData[0], 0),
        sceneNumber(material.shaderData[1], 0),
        sceneNumber(material.shaderData[2], 1),
      ];
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
      return [4, sceneMaterialEmissive(material), 0.78];
    default:
      return [0, sceneMaterialEmissive(material), 1];
    }
  }

  function sceneWorldObjectRenderPass(object, material) {
    const renderPass = String(object && object.renderPass || "").toLowerCase();
    if (renderPass === "opaque" || renderPass === "alpha" || renderPass === "additive") {
      return renderPass;
    }
    return sceneMaterialRenderPass(material);
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

  function sceneStaticDrawKey(objects, materials, camera) {
    let hash = 2166136261 >>> 0;
    hash = sceneHashCamera(hash, camera);
    for (const object of objects) {
      hash = sceneHashStaticObject(hash, object);
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
  const sceneMaterialStringFields = ["kind", "color", "blendMode"];
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
