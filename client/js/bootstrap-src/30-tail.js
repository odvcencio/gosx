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
      case "click":
        if (e.target && e.target.value !== undefined) {
          const value = String(e.target.value == null ? "" : e.target.value);
          if (value !== "") {
            data.value = value;
          }
        }
        break;
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

    async function loadProgram() {
      if (!entry.programRef) {
        return null;
      }
      if (!programPromise) {
        const format = inferProgramFormat(entry);
        programPromise = fetchProgram(entry.programRef, format).then(function(data) {
          return data == null ? null : { data, format };
        });
      }
      return programPromise;
    }

    return {
      mode: entry.runtime || "",
      available() {
        return sharedEngineRuntimeAvailable(entry);
      },
      async hydrateFromProgramRef() {
        const program = await loadProgram();
        return hydrateSharedEngineProgram(entry, program);
      },
      tick() {
        return tickSharedEngineRuntime(entry);
      },
      renderFrame(timeSeconds, width, height) {
        return renderSharedEngineFrame(entry, timeSeconds, width, height);
      },
      dispose() {
        disposeSharedEngineRuntime(entry);
      },
    };
  }

  function sharedEngineRuntimeBridge() {
    return {
      hydrate: window.__gosx_hydrate_engine,
      tick: window.__gosx_tick_engine,
      render: window.__gosx_render_engine,
      dispose: window.__gosx_engine_dispose,
    };
  }

  function sharedEngineRuntimeAvailable(entry) {
    const bridge = sharedEngineRuntimeBridge();
    return engineUsesSharedRuntime(entry)
      && typeof bridge.hydrate === "function"
      && typeof bridge.tick === "function"
      && typeof bridge.render === "function"
      && typeof bridge.dispose === "function";
  }

  function hydrateSharedEngineProgram(entry, program) {
    const bridge = sharedEngineRuntimeBridge();
    if (!engineUsesSharedRuntime(entry) || typeof bridge.hydrate !== "function" || !program) {
      return [];
    }
    return decodeEngineCommands(bridge.hydrate(
      entry.id,
      entry.component,
      JSON.stringify(entry.props || {}),
      program.data,
      program.format || "json",
    ));
  }

  function tickSharedEngineRuntime(entry) {
    const bridge = sharedEngineRuntimeBridge();
    if (!engineUsesSharedRuntime(entry) || typeof bridge.tick !== "function") {
      return [];
    }
    return decodeEngineCommands(bridge.tick(entry.id));
  }

  function renderSharedEngineFrame(entry, timeSeconds, width, height) {
    const bridge = sharedEngineRuntimeBridge();
    if (!engineUsesSharedRuntime(entry) || typeof bridge.render !== "function") {
      return null;
    }
    return decodeEngineRenderBundle(bridge.render(entry.id, timeSeconds, width, height));
  }

  function disposeSharedEngineRuntime(entry) {
    const bridge = sharedEngineRuntimeBridge();
    if (!engineUsesSharedRuntime(entry) || typeof bridge.dispose !== "function") {
      return;
    }
    bridge.dispose(entry.id);
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
    bundle.camera = sceneRenderCamera(bundle.camera);
    bundle.labels = Array.isArray(bundle.labels) ? bundle.labels.map(function(label, index) {
      const item = label && typeof label === "object" ? label : {};
      return {
        id: item.id || ("scene-label-" + index),
        text: typeof item.text === "string" ? item.text : "",
        className: sceneLabelClassName(item),
        position: {
          x: sceneNumber(item.position && item.position.x, 0),
          y: sceneNumber(item.position && item.position.y, 0),
        },
      depth: sceneNumber(item.depth, 0),
      priority: sceneNumber(item.priority, 0),
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
    }).filter(function(label) {
      return label.text.trim() !== "";
    }) : [];
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
    disposeManagedTextLayouts();
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
    if (typeof window.__gosx_text_layout === "function" && window.__gosx_text_layout !== gosxTextLayout) {
      adoptTextLayoutImpl(window.__gosx_text_layout);
      window.__gosx_text_layout = gosxTextLayout;
    }
    if (typeof window.__gosx_text_layout_metrics === "function" && window.__gosx_text_layout_metrics !== gosxTextLayoutMetrics) {
      adoptTextLayoutMetricsImpl(window.__gosx_text_layout_metrics);
      window.__gosx_text_layout_metrics = gosxTextLayoutMetrics;
    }
    if (typeof window.__gosx_text_layout_ranges === "function" && window.__gosx_text_layout_ranges !== gosxTextLayoutRanges) {
      adoptTextLayoutRangesImpl(window.__gosx_text_layout_ranges);
      window.__gosx_text_layout_ranges = gosxTextLayoutRanges;
    }
    refreshManagedTextLayouts();
    refreshGosxDocumentState("runtime-ready");
    refreshGosxEnvironmentState("runtime-ready");
    if (!pendingManifest) {
      window.__gosx.ready = true;
      refreshGosxDocumentState("ready");
      return;
    }

    Promise.all([
      hydrateAllIslands(pendingManifest),
      mountAllEngines(pendingManifest),
      connectAllHubs(pendingManifest),
    ]).then(function() {
      window.__gosx.ready = true;
      refreshGosxDocumentState("ready");
      document.dispatchEvent(new CustomEvent("gosx:ready"));
    }).catch(function(e) {
      console.error("[gosx] bootstrap failed:", e);
      window.__gosx.ready = true;
      refreshGosxDocumentState("ready");
    });
  };

  // --------------------------------------------------------------------------
  // Main initialization
  // --------------------------------------------------------------------------

  async function bootstrapPage() {
    refreshGosxEnvironmentState("bootstrap-page");
    refreshGosxDocumentState("bootstrap-page");
    mountManagedTextLayouts(document.body || document.documentElement);

    const manifest = loadManifest();
    if (!manifest) {
      // No manifest — pure server-rendered page, no islands to hydrate.
      pendingManifest = null;
      window.__gosx.ready = true;
      refreshGosxDocumentState("ready");
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
