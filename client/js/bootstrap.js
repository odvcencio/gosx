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
      const response = await fetch(runtimeRef.path);
      if (!response.ok) {
        throw new Error("runtime fetch failed with status " + response.status);
      }

      let result;
      if (typeof WebAssembly.instantiateStreaming === "function") {
        try {
          result = await WebAssembly.instantiateStreaming(response.clone(), go.importObject);
        } catch (streamErr) {
          const bytes = await response.arrayBuffer();
          result = await WebAssembly.instantiate(bytes, go.importObject);
        }
      } else {
        const bytes = await response.arrayBuffer();
        result = await WebAssembly.instantiate(bytes, go.importObject);
      }

      // go.run is intentionally not awaited — it resolves when the Go main()
      // exits, but the runtime stays alive via syscall/js callbacks.
      go.run(result.instance);
    } catch (e) {
      console.error("[gosx] failed to load WASM runtime:", e);
    }
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

  function sceneObjects(props) {
    const scene = props && props.scene && typeof props.scene === "object" ? props.scene : null;
    const raw = Array.isArray(scene && scene.objects) ? scene.objects : (Array.isArray(props && props.objects) ? props.objects : null);
    const objects = raw && raw.length > 0 ? raw : [
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

    return objects.map(function(object, index) {
      const item = object && typeof object === "object" ? object : {};
      return {
        id: item.id || ("scene-object-" + index),
        kind: item.kind || "cube",
        size: sceneNumber(item.size, 1.2),
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
    });
  }

  function sceneCamera(props) {
    const raw = props && props.camera && typeof props.camera === "object" ? props.camera : {};
    return {
      x: sceneNumber(raw.x, 0),
      y: sceneNumber(raw.y, 0),
      z: sceneNumber(raw.z, 6),
      fov: sceneNumber(raw.fov, 75),
    };
  }

  function clearChildren(node) {
    while (node && node.firstChild) {
      node.removeChild(node.firstChild);
    }
  }

  function cubeVertices(size) {
    const half = size / 2;
    return [
      { x: -half, y: -half, z: -half },
      { x: half, y: -half, z: -half },
      { x: half, y: half, z: -half },
      { x: -half, y: half, z: -half },
      { x: -half, y: -half, z: half },
      { x: half, y: -half, z: half },
      { x: half, y: half, z: half },
      { x: -half, y: half, z: half },
    ];
  }

  const cubeEdgePairs = [
    [0, 1], [1, 2], [2, 3], [3, 0],
    [4, 5], [5, 6], [6, 7], [7, 4],
    [0, 4], [1, 5], [2, 6], [3, 7],
  ];

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

  function drawScene3D(ctx2d, width, height, background, camera, objects, timeSeconds) {
    ctx2d.clearRect(0, 0, width, height);
    ctx2d.fillStyle = background;
    ctx2d.fillRect(0, 0, width, height);

    ctx2d.strokeStyle = "rgba(141, 225, 255, 0.14)";
    ctx2d.lineWidth = 1;
    for (let x = 0; x <= width; x += 48) {
      strokeLine(ctx2d, { x, y: 0 }, { x, y: height });
    }
    for (let y = 0; y <= height; y += 48) {
      strokeLine(ctx2d, { x: 0, y }, { x: width, y });
    }

    for (const object of objects) {
      if (object.kind !== "cube") continue;

      const vertices = cubeVertices(object.size).map(function(vertex) {
        const rotated = rotatePoint(
          vertex,
          object.rotationX + object.spinX * timeSeconds,
          object.rotationY + object.spinY * timeSeconds,
          object.rotationZ + object.spinZ * timeSeconds,
        );
        return {
          x: rotated.x + object.x,
          y: rotated.y + object.y,
          z: rotated.z + object.z,
        };
      });

      const projected = vertices.map(function(vertex) {
        return projectPoint(vertex, camera, width, height);
      });

      ctx2d.strokeStyle = object.color;
      ctx2d.lineWidth = 1.8;
      for (const edge of cubeEdgePairs) {
        const from = projected[edge[0]];
        const to = projected[edge[1]];
        if (!from || !to) continue;
        strokeLine(ctx2d, from, to);
      }
    }
  }

  window.__gosx_register_engine_factory("GoSXScene3D", function(ctx) {
    if (!ctx.mount || typeof document.createElement !== "function") {
      console.warn("[gosx] Scene3D requires a mount element");
      return {};
    }

    const props = ctx.props || {};
    const width = Math.max(240, sceneNumber(props.width, 720));
    const height = Math.max(180, sceneNumber(props.height, 420));
    const background = typeof props.background === "string" && props.background ? props.background : "#08151f";
    const camera = sceneCamera(props);
    const objects = sceneObjects(props);
    const shouldAnimate = sceneBool(props.autoRotate, true) || objects.some(function(object) {
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

    const ctx2d = typeof canvas.getContext === "function" ? canvas.getContext("2d") : null;
    if (!ctx2d) {
      console.warn("[gosx] Scene3D could not acquire a 2D canvas context");
      return {
        dispose() {
          if (canvas.parentNode === ctx.mount) {
            ctx.mount.removeChild(canvas);
          }
        },
      };
    }

    let frameHandle = null;
    let disposed = false;

    function renderFrame(now) {
      if (disposed) return;
      drawScene3D(ctx2d, width, height, background, camera, objects, now / 1000);
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
      dispose() {
        disposed = true;
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
    const specificAttr = "data-gosx-on-" + eventType;
    const genericAttr = "data-gosx-handler"; // legacy: treated as click-only

    let el = target;
    while (el && el !== root.parentNode) {
      if (el.hasAttribute && el.hasAttribute(specificAttr)) {
        return el.getAttribute(specificAttr);
      }
      // data-gosx-handler is shorthand for click only
      if (eventType === "click" && el.hasAttribute && el.hasAttribute(genericAttr)) {
        return el.getAttribute(genericAttr);
      }
      el = el.parentNode;
    }
    return null;
  }

  function setupEventDelegation(islandRoot, islandID) {
    const entries = [];

    for (const eventType of DELEGATED_EVENTS) {
      const listener = function(e) {
        // Skip if already handled by an inner island
        if (e.__gosx_handled) return;

        const handlerName = findHandlerForEvent(e.target, islandRoot, eventType);
        if (!handlerName) return;

        // Mark handled
        e.__gosx_handled = true;

        const actionFn = window.__gosx_action;
        if (typeof actionFn !== "function") return;

        const eventData = extractEventData(e);
        try {
          const result = actionFn(islandID, handlerName, JSON.stringify(eventData));
          if (typeof result === "string" && result !== "") {
            console.error(`[gosx] action error (${islandID}/${handlerName}):`, result);
          }
        } catch (err) {
          console.error(`[gosx] action error (${islandID}/${handlerName}):`, err);
        }
      };

      const useCapture = (eventType === "focus" || eventType === "blur");
      islandRoot.addEventListener(eventType, listener, useCapture);
      entries.push({ type: eventType, listener, capture: useCapture });
    }

    return entries;
  }

  // --------------------------------------------------------------------------
  // Engine mounting
  // --------------------------------------------------------------------------

  function resolveEngineFactory(entry) {
    const exportName = entry.jsExport || entry.component;
    if (!exportName) return null;
    return engineFactories[exportName] || null;
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

  function createEngineContext(entry, mount) {
    return {
      id: entry.id,
      kind: entry.kind,
      component: entry.component,
      mount: mount,
      props: entry.props || {},
      capabilities: entry.capabilities || [],
      programRef: entry.programRef || "",
      jsRef: entry.jsRef || "",
      jsExport: entry.jsExport || "",
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

    let mount = null;
    if (entry.kind === "surface") {
      const mountID = entry.mountId || entry.id;
      mount = document.getElementById(mountID);
      if (!mount) {
        console.warn(`[gosx] engine mount #${mountID} not found for ${entry.id}`);
        return;
      }
    }

    let factory = resolveEngineFactory(entry);
    if (!factory && entry.jsRef) {
      await loadEngineScript(entry.jsRef);
      factory = resolveEngineFactory(entry);
    }

    if (typeof factory !== "function") {
      console.warn(`[gosx] no engine factory registered for ${entry.component}`);
      return;
    }

    try {
      let result = factory(createEngineContext(entry, mount));
      if (result && typeof result.then === "function") {
        result = await result;
      }
      window.__gosx.engines.set(entry.id, {
        component: entry.component,
        kind: entry.kind,
        mount: mount,
        handle: normalizeEngineHandle(result),
      });
    } catch (e) {
      console.error(`[gosx] failed to mount engine ${entry.id}:`, e);
    }
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
    if (path.startsWith("ws://") || path.startsWith("wss://")) {
      return path;
    }
    const proto = window.location && window.location.protocol === "https:" ? "wss://" : "ws://";
    const host = window.location && window.location.host ? window.location.host : "";
    if (path.startsWith("/")) {
      return proto + host + path;
    }
    return proto + host + "/" + path;
  }

  function applyHubBindings(entry, message) {
    if (!entry.bindings || entry.bindings.length === 0) return;
    const setSharedSignal = window.__gosx_set_shared_signal;
    if (typeof setSharedSignal !== "function") return;

    for (const binding of entry.bindings) {
      if (!binding || binding.event !== message.event || !binding.signal) continue;
      try {
        const result = setSharedSignal(binding.signal, JSON.stringify(message.data));
        if (typeof result === "string" && result !== "") {
          console.error(`[gosx] hub binding error (${entry.id}/${binding.signal}):`, result);
        }
      } catch (e) {
        console.error(`[gosx] hub binding error (${entry.id}/${binding.signal}):`, e);
      }
    }
  }

  function connectHub(entry) {
    if (!entry || !entry.id || !entry.path || typeof WebSocket !== "function") return;

    window.__gosx_disconnect_hub(entry.id);

    const socket = new WebSocket(hubURL(entry.path));
    const record = {
      entry: entry,
      socket: socket,
      reconnectTimer: null,
    };
    window.__gosx.hubs.set(entry.id, record);

    socket.onmessage = function(evt) {
      let message;
      try {
        message = JSON.parse(evt.data);
      } catch (e) {
        console.error(`[gosx] failed to decode hub message for ${entry.id}:`, e);
        return;
      }

      applyHubBindings(entry, message);
      if (typeof document.dispatchEvent === "function" && typeof CustomEvent === "function") {
        document.dispatchEvent(new CustomEvent("gosx:hub:event", {
          detail: {
            hubID: entry.id,
            hubName: entry.name,
            event: message.event,
            data: message.data,
          },
        }));
      }
    };

    socket.onclose = function() {
      const current = window.__gosx.hubs.get(entry.id);
      if (!current || current.socket !== socket) return;
      current.reconnectTimer = setTimeout(function() {
        connectHub(entry);
      }, 1000);
    };

    socket.onerror = function(e) {
      console.error(`[gosx] hub connection error for ${entry.id}:`, e);
    };
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
    const root = document.getElementById(entry.id);
    if (!root) {
      console.warn(`[gosx] island root #${entry.id} not found in DOM`);
      return;
    }

    // Skip purely static islands (no client interactivity).
    if (entry.static) return;

    // Determine program format (default to JSON in development).
    const programFormat = inferProgramFormat(entry);

    // Fetch the island's program data.
    if (!entry.programRef) {
      console.error(`[gosx] skipping island ${entry.id} — missing programRef`);
      return;
    }

    const programData = await fetchProgram(entry.programRef, programFormat);
    if (programData === null) {
      console.error(`[gosx] skipping island ${entry.id} — program fetch failed`);
      return;
    }

    // Call the WASM-exported hydrate function.
    const hydrateFn = window.__gosx_hydrate;
    if (typeof hydrateFn !== "function") {
      console.error("[gosx] __gosx_hydrate not available — cannot hydrate island", entry.id);
      return;
    }

    try {
      const result = hydrateFn(
        entry.id,                           // islandID
        entry.component,                    // componentName
        JSON.stringify(entry.props || {}),  // propsJSON
        programData,                        // program data (ArrayBuffer or string)
        programFormat                       // program format identifier
      );
      if (typeof result === "string" && result !== "") {
        console.error(`[gosx] failed to hydrate island ${entry.id}: ${result}`);
        return;
      }
    } catch (e) {
      console.error(`[gosx] failed to hydrate island ${entry.id}:`, e);
      return;
    }

    // Set up delegated event listeners on the island root.
    const listeners = setupEventDelegation(root, entry.id);

    // Track the island.
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

    // Load the shared WASM runtime. The runtime will call
    // __gosx_runtime_ready() when it is initialized, which triggers
    // island hydration.
    if ((manifest.islands && manifest.islands.length > 0 || manifest.hubs && manifest.hubs.length > 0) && manifest.runtime && manifest.runtime.path) {
      if (runtimeReady()) {
        window.__gosx_runtime_ready();
      } else {
        await loadRuntime(manifest.runtime);
      }
    } else {
      if ((manifest.islands && manifest.islands.length > 0) || (manifest.hubs && manifest.hubs.length > 0)) {
        console.error("[gosx] islands and hub bindings require manifest.runtime.path");
      }
      window.__gosx_runtime_ready();
    }
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
