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
    const builtin = resolveBuiltinEngineFactory(entry);
    if (builtin) {
      return builtin;
    }
    const exportName = engineExportName(entry);
    if (!exportName) return null;
    return engineFactories[exportName] || null;
  }

  function resolveBuiltinEngineFactory(entry) {
    if (!entry || !engineKindUsesBuiltinFactory(entry.kind)) {
      return null;
    }
    if (entry.kind === "video") {
      return createBuiltInVideoEngine;
    }
    return null;
  }

  function engineExportName(entry) {
    return entry.component;
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

  const pendingEngineRuntimes = new Map();

  function pixelSurfaceCapabilityEnabled(entry) {
    return Boolean(entry && Array.isArray(entry.capabilities) && entry.capabilities.includes("pixel-surface"));
  }

  function pixelSurfaceDimension(value, fallback) {
    const num = Math.floor(Number(value));
    return Number.isFinite(num) && num > 0 ? num : fallback;
  }

  function normalizePixelSurfaceScaling(value) {
    const mode = String(value || "pixel-perfect").trim().toLowerCase();
    switch (mode) {
      case "fill":
      case "stretch":
        return mode;
      default:
        return "pixel-perfect";
    }
  }

  function normalizePixelSurfaceClearColor(value) {
    const color = Array.isArray(value) ? value : [0, 0, 0, 255];
    const out = [0, 0, 0, 255];
    for (let i = 0; i < out.length; i += 1) {
      const num = Math.max(0, Math.min(255, Math.floor(Number(color[i])) || 0));
      out[i] = num;
    }
    return out;
  }

  function pixelSurfaceBackgroundColor(clearColor) {
    return "rgba(" + clearColor[0] + ", " + clearColor[1] + ", " + clearColor[2] + ", " + (clearColor[3] / 255) + ")";
  }

  function resolvePixelSurfaceConfig(entry, mount) {
    if (!pixelSurfaceCapabilityEnabled(entry)) {
      return null;
    }
    const source = entry && entry.pixelSurface && typeof entry.pixelSurface === "object" ? entry.pixelSurface : {};
    const widthAttr = mount && typeof mount.getAttribute === "function" ? mount.getAttribute("data-gosx-pixel-width") : "";
    const heightAttr = mount && typeof mount.getAttribute === "function" ? mount.getAttribute("data-gosx-pixel-height") : "";
    const scalingAttr = mount && typeof mount.getAttribute === "function" ? mount.getAttribute("data-gosx-pixel-scaling") : "";
    const width = pixelSurfaceDimension(source.width, pixelSurfaceDimension(widthAttr, 0));
    const height = pixelSurfaceDimension(source.height, pixelSurfaceDimension(heightAttr, 0));
    if (width <= 0 || height <= 0) {
      return null;
    }
    return {
      width,
      height,
      scaling: normalizePixelSurfaceScaling(source.scaling || scalingAttr),
      clearColor: normalizePixelSurfaceClearColor(source.clearColor),
      vsync: source.vsync !== false,
    };
  }

  function pixelSurfaceLayout(config, mount) {
    const rect = mount && typeof mount.getBoundingClientRect === "function" ? mount.getBoundingClientRect() : null;
    const surfaceWidth = Math.max(1, pixelSurfaceDimension(rect && rect.width, pixelSurfaceDimension(mount && mount.width, config.width)));
    const surfaceHeight = Math.max(1, pixelSurfaceDimension(rect && rect.height, pixelSurfaceDimension(mount && mount.height, config.height)));
    let drawWidth = surfaceWidth;
    let drawHeight = surfaceHeight;
    let scaleX = surfaceWidth / config.width;
    let scaleY = surfaceHeight / config.height;

    switch (config.scaling) {
      case "stretch":
        break;
      case "fill": {
        const scale = Math.min(scaleX, scaleY);
        drawWidth = config.width * scale;
        drawHeight = config.height * scale;
        scaleX = scale;
        scaleY = scale;
        break;
      }
      default: {
        const scale = Math.max(1, Math.floor(Math.min(scaleX, scaleY)));
        drawWidth = config.width * scale;
        drawHeight = config.height * scale;
        scaleX = scale;
        scaleY = scale;
        break;
      }
    }

    return {
      surfaceWidth,
      surfaceHeight,
      drawWidth,
      drawHeight,
      left: Math.max(0, (surfaceWidth - drawWidth) / 2),
      top: Math.max(0, (surfaceHeight - drawHeight) / 2),
      scaleX,
      scaleY,
    };
  }

  function pixelSurfaceWindowToPixel(windowX, windowY, mount, layout, config) {
    const rect = mount && typeof mount.getBoundingClientRect === "function"
      ? mount.getBoundingClientRect()
      : { left: 0, top: 0 };
    const localX = Number(windowX) - Number(rect.left || 0) - layout.left;
    const localY = Number(windowY) - Number(rect.top || 0) - layout.top;
    const pixelX = Math.floor(localX / Math.max(0.0001, layout.scaleX));
    const pixelY = Math.floor(localY / Math.max(0.0001, layout.scaleY));
    return {
      x: pixelX,
      y: pixelY,
      inside: pixelX >= 0 && pixelX < config.width && pixelY >= 0 && pixelY < config.height,
    };
  }

  function createPixelSurfaceRuntime(entry, mount) {
    const config = resolvePixelSurfaceConfig(entry, mount);
    if (!config || !mount || entry.kind !== "surface") {
      return null;
    }

    const pixels = new Uint8ClampedArray(config.width * config.height * 4);
    const fallbackChildren = mount && mount.childNodes ? Array.from(mount.childNodes) : [];
    const initialPosition = mount && mount.style ? String(mount.style.position || "") : "";
    const initialOverflow = mount && mount.style ? String(mount.style.overflow || "") : "";
    const initialBackgroundColor = mount && mount.style ? String(mount.style.backgroundColor || "") : "";
    let canvas = null;
    let ctx2d = null;
    let imageData = null;
    let layout = null;
    let resizeObserver = null;
    let presentHandle = 0;
    let disposed = false;

    function restoreMountFallback() {
      if (!mount) {
        return;
      }
      if (canvas && canvas.parentNode === mount) {
        mount.removeChild(canvas);
      }
      mount.removeAttribute("data-gosx-pixel-surface-mounted");
      mount.style.position = initialPosition;
      mount.style.overflow = initialOverflow;
      mount.style.backgroundColor = initialBackgroundColor;
      if (mount.childNodes && mount.childNodes.length === 0) {
        for (const child of fallbackChildren) {
          if (child && child.parentNode !== mount) {
            mount.appendChild(child);
          }
        }
      }
    }

    function ensureCanvas() {
      if (disposed) {
        return null;
      }
      if (canvas && ctx2d) {
        return canvas;
      }

      const nextCanvas = document.createElement("canvas");
      nextCanvas.setAttribute("data-gosx-pixel-surface", "true");
      nextCanvas.setAttribute("width", String(config.width));
      nextCanvas.setAttribute("height", String(config.height));
      nextCanvas.width = config.width;
      nextCanvas.height = config.height;
      nextCanvas.style.position = "absolute";
      nextCanvas.style.maxWidth = "none";
      nextCanvas.style.maxHeight = "none";
      nextCanvas.style.imageRendering = config.scaling === "pixel-perfect" ? "pixelated" : "auto";

      const nextCtx2d = typeof nextCanvas.getContext === "function" ? nextCanvas.getContext("2d") : null;
      if (!nextCtx2d) {
        return null;
      }
      if ("imageSmoothingEnabled" in nextCtx2d) {
        nextCtx2d.imageSmoothingEnabled = config.scaling !== "pixel-perfect";
      }

      canvas = nextCanvas;
      ctx2d = nextCtx2d;
      clearChildren(mount);
      if (!mount.style.position) {
        mount.style.position = "relative";
      }
      mount.style.overflow = "hidden";
      mount.style.backgroundColor = pixelSurfaceBackgroundColor(config.clearColor);
      mount.setAttribute("data-gosx-pixel-surface-mounted", "true");
      mount.appendChild(canvas);
      applyLayout();
      if (!resizeObserver && typeof ResizeObserver === "function") {
        resizeObserver = new ResizeObserver(function() {
          applyLayout();
        });
        resizeObserver.observe(mount);
      }
      return canvas;
    }

    function applyLayout() {
      if (!canvas) {
        return null;
      }
      layout = pixelSurfaceLayout(config, mount);
      canvas.style.left = layout.left + "px";
      canvas.style.top = layout.top + "px";
      canvas.style.width = layout.drawWidth + "px";
      canvas.style.height = layout.drawHeight + "px";
      return layout;
    }

    function copyPixelsIntoImageData() {
      if (!ctx2d) {
        return null;
      }
      if ((!imageData || imageData.width !== config.width || imageData.height !== config.height) && typeof ctx2d.createImageData === "function") {
        imageData = ctx2d.createImageData(config.width, config.height);
      }
      if (imageData && imageData.data && typeof imageData.data.set === "function") {
        imageData.data.set(pixels);
        return imageData;
      }
      return {
        width: config.width,
        height: config.height,
        data: pixels,
      };
    }

    function drawNow() {
      presentHandle = 0;
      if (!ensureCanvas() || !ctx2d || typeof ctx2d.putImageData !== "function") {
        return;
      }
      const data = copyPixelsIntoImageData();
      if (!data) {
        return;
      }
      ctx2d.putImageData(data, 0, 0);
    }

    function present() {
      if (!ensureCanvas()) {
        return api;
      }
      if (!config.vsync) {
        drawNow();
        return api;
      }
      if (!presentHandle) {
        presentHandle = engineFrame(function() {
          drawNow();
        });
      }
      return api;
    }

    const api = {
      id: entry.id,
      width: config.width,
      height: config.height,
      stride: config.width * 4,
      scaling: config.scaling,
      clearColor: config.clearColor.slice(),
      vsync: config.vsync,
      pixels,
      get mount() {
        return mount;
      },
      get canvas() {
        ensureCanvas();
        return canvas;
      },
      get context() {
        ensureCanvas();
        return ctx2d;
      },
      clear() {
        for (let i = 0; i < pixels.length; i += 4) {
          pixels[i] = config.clearColor[0];
          pixels[i + 1] = config.clearColor[1];
          pixels[i + 2] = config.clearColor[2];
          pixels[i + 3] = config.clearColor[3];
        }
        return api;
      },
      layout() {
        ensureCanvas();
        return applyLayout();
      },
      present,
      toPixel(windowX, windowY) {
        ensureCanvas();
        const currentLayout = layout || applyLayout();
        if (!currentLayout) {
          return { x: 0, y: 0, inside: false };
        }
        return pixelSurfaceWindowToPixel(windowX, windowY, mount, currentLayout, config);
      },
      dispose() {
        disposed = true;
        if (presentHandle) {
          cancelEngineFrame(presentHandle);
          presentHandle = 0;
        }
        if (resizeObserver && typeof resizeObserver.disconnect === "function") {
          resizeObserver.disconnect();
        }
        resizeObserver = null;
        restoreMountFallback();
        canvas = null;
        ctx2d = null;
        imageData = null;
        layout = null;
      },
    };
    api.clear();
    return api;
  }

  function createEngineRuntime(entry, mount) {
    let programPromise = null;
    let pixelSurface = undefined;

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

    function frame() {
      if (pixelSurface === undefined) {
        pixelSurface = createPixelSurfaceRuntime(entry, mount);
      }
      return pixelSurface || null;
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
      frame,
      pixelSurface: frame,
      dispose() {
        const currentFrame = frame();
        if (currentFrame && typeof currentFrame.dispose === "function") {
          currentFrame.dispose();
        }
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
    bundle.html = Array.isArray(bundle.html) ? bundle.html.map(function(entry, index) {
      const item = entry && typeof entry === "object" ? entry : {};
      return {
        id: item.id || ("scene-html-" + index),
        html: typeof item.html === "string" ? item.html : (typeof item.markup === "string" ? item.markup : ""),
        className: sceneLabelClassName(item),
        position: {
          x: sceneNumber(item.position && item.position.x, 0),
          y: sceneNumber(item.position && item.position.y, 0),
        },
        depth: sceneNumber(item.depth, 0),
        priority: sceneNumber(item.priority, 0),
        width: Math.max(1, sceneNumber(item.width, 180)),
        height: Math.max(1, sceneNumber(item.height, 72)),
        opacity: clamp01(sceneNumber(item.opacity, 1)),
        offsetX: sceneNumber(item.offsetX, 0),
        offsetY: sceneNumber(item.offsetY, 0),
        anchorX: Math.max(0, Math.min(1, sceneNumber(item.anchorX, 0.5))),
        anchorY: Math.max(0, Math.min(1, sceneNumber(item.anchorY, 0.5))),
        occlude: sceneBool(item.occlude, false),
        pointerEvents: normalizeSceneHTMLPointerEvents(item.pointerEvents, "none"),
      };
    }).filter(function(entry) {
      return entry.html.trim() !== "";
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

  function engineKindNeedsMount(kind) {
    return kind === "surface" || kind === "video";
  }

  function engineKindUsesBuiltinFactory(kind) {
    return kind === "video";
  }

  function videoPropValue(props, names, fallback) {
    const source = props && typeof props === "object" ? props : {};
    const list = Array.isArray(names) ? names : [names];
    for (const name of list) {
      if (!name) {
        continue;
      }
      if (Object.prototype.hasOwnProperty.call(source, name) && source[name] != null) {
        return source[name];
      }
    }
    return fallback;
  }

  function videoSignalName(name) {
    return "$video." + name;
  }

  function videoRuntimeAssets() {
    if (window.__gosx && window.__gosx.document && typeof window.__gosx.document.get === "function") {
      const documentState = window.__gosx.document.get();
      if (documentState && documentState.assets && documentState.assets.runtime) {
        return documentState.assets.runtime;
      }
    }
    return {};
  }

  function readVideoSignal(name, fallback) {
    const value = gosxReadSharedSignal(videoSignalName(name), fallback);
    return value == null ? fallback : value;
  }

  function writeVideoSignal(name, value) {
    const signalName = videoSignalName(name);
    const payload = JSON.stringify(value == null ? null : value);
    const setSharedSignal = window.__gosx_set_shared_signal;
    if (typeof setSharedSignal === "function") {
      try {
        const result = setSharedSignal(signalName, payload);
        if (typeof result === "string" && result !== "") {
          console.error("[gosx] shared signal update error (" + signalName + "):", result);
          gosxNotifySharedSignal(signalName, payload);
        }
        return;
      } catch (error) {
        console.error("[gosx] shared signal update error (" + signalName + "):", error);
      }
    }
    gosxNotifySharedSignal(signalName, payload);
  }

  function subscribeVideoSignal(name, listener) {
    return gosxSubscribeSharedSignal(videoSignalName(name), function(value) {
      listener(value);
    }, { immediate: true });
  }

  function videoClearChildren(node) {
    if (!node) {
      return;
    }
    while (node.firstChild) {
      node.removeChild(node.firstChild);
    }
  }

  function videoRestoreChildren(node, children) {
    if (!node) {
      return;
    }
    videoClearChildren(node);
    for (const child of children || []) {
      if (child) {
        node.appendChild(child);
      }
    }
  }

  function videoNeedsHLS(source) {
    return /\.m3u8(?:$|[?#])/i.test(String(source || "").trim());
  }

  function videoSupportsNativeHLS(video) {
    if (!video || typeof video.canPlayType !== "function") {
      return false;
    }
    const result = String(video.canPlayType("application/vnd.apple.mpegurl") || "");
    return result !== "" && result !== "no";
  }

  async function ensureVideoHLSLibrary() {
    if (typeof window.Hls === "function") {
      return window.Hls;
    }
    const runtimeAssets = videoRuntimeAssets();
    const path = String(runtimeAssets.hlsPath || "/gosx/hls.min.js").trim();
    if (!path) {
      return null;
    }
    await loadScriptTag(path);
    return typeof window.Hls === "function" ? window.Hls : null;
  }

  function videoBufferedAhead(video) {
    if (!video || !video.buffered || typeof video.buffered.length !== "number" || typeof video.buffered.end !== "function") {
      return 0;
    }
    const current = Math.max(0, sceneNumber(video.currentTime, 0));
    for (let i = 0; i < video.buffered.length; i += 1) {
      const end = sceneNumber(video.buffered.end(i), current);
      const start = typeof video.buffered.start === "function" ? sceneNumber(video.buffered.start(i), 0) : 0;
      if (current >= start && current <= end + 0.1) {
        return Math.max(0, end - current);
      }
    }
    return 0;
  }

  function videoViewportSize(mount) {
    const rect = mount && typeof mount.getBoundingClientRect === "function"
      ? mount.getBoundingClientRect()
      : { width: 0, height: 0 };
    return [
      Math.max(0, Math.round(sceneNumber(rect && rect.width, sceneNumber(mount && mount.width, 0)))),
      Math.max(0, Math.round(sceneNumber(rect && rect.height, sceneNumber(mount && mount.height, 0)))),
    ];
  }

  function videoNormalizeSourceInfo(item, index) {
    const source = item && typeof item === "object" ? item : {};
    const src = String(videoPropValue(source, ["src", "source", "url"], "") || "").trim();
    if (!src) {
      return null;
    }
    const type = String(videoPropValue(source, ["type"], "") || "").trim();
    const media = String(videoPropValue(source, ["media"], "") || "").trim();
    const id = String(videoPropValue(source, ["id", "name"], src || ("source-" + index)) || "").trim();
    return {
      id: id || ("source-" + index),
      src: src,
      type: type,
      media: media,
    };
  }

  function videoSourcesFromProps(props) {
    const sources = videoPropValue(props, ["sources"], []);
    if (!Array.isArray(sources)) {
      return [];
    }
    const out = [];
    for (let index = 0; index < sources.length; index += 1) {
      const source = videoNormalizeSourceInfo(sources[index], index);
      if (source) {
        out.push(source);
      }
    }
    return out;
  }

  function videoNormalizeTrackInfo(item, index) {
    const source = item && typeof item === "object" ? item : {};
    const src = String(videoPropValue(source, ["src"], "") || "").trim();
    const srcLang = String(videoPropValue(source, ["srclang", "srcLang"], "") || "").trim();
    const language = String(videoPropValue(source, ["language", "lang", "srclang", "srcLang"], srcLang) || "").trim();
    const title = String(videoPropValue(source, ["title", "label", "name"], language || ("Track " + (index + 1))) || "").trim();
    const id = String(videoPropValue(source, ["id", "trackID", "trackId"], language || title || ("track-" + index)) || "").trim();
    const kind = String(videoPropValue(source, ["kind"], "subtitles") || "subtitles").trim().toLowerCase() || "subtitles";
    return {
      id: id || ("track-" + index),
      language: language,
      srclang: srcLang || language,
      title: title,
      kind: kind,
      src: src,
      default: sceneBool(videoPropValue(source, ["default"], false), false),
      forced: sceneBool(videoPropValue(source, ["forced"], false), false),
    };
  }

  function videoTracksFromProps(props) {
    const tracks = videoPropValue(props, ["subtitleTracks", "subtitle_tracks"], []);
    if (!Array.isArray(tracks)) {
      return [];
    }
    return tracks.map(videoNormalizeTrackInfo);
  }

  function videoTrackURL(track, props) {
    const explicit = String(videoPropValue(track, ["src"], "") || "").trim();
    if (explicit) {
      return explicit;
    }
    const subtitleBase = String(videoPropValue(props, ["subtitleBase", "subtitle_base"], "") || "").trim();
    const id = String(track && track.id || "").trim();
    if (!subtitleBase || !id) {
      return "";
    }
    return subtitleBase.replace(/\/$/, "") + "/" + encodeURIComponent(id) + ".vtt";
  }

  function videoFirstFallbackNode(children, tagName) {
    for (const child of children || []) {
      if (child && child.nodeType === 1 && child.tagName === tagName) {
        return child;
      }
    }
    return null;
  }

  function videoCanUseSourceNatively(video, source) {
    if (!source || typeof source !== "object") {
      return false;
    }
    const src = String(source.src || "").trim();
    if (!src) {
      return false;
    }
    if (videoNeedsHLS(src)) {
      return videoSupportsNativeHLS(video);
    }
    const type = String(source.type || "").trim();
    if (!type || !video || typeof video.canPlayType !== "function") {
      return true;
    }
    const result = String(video.canPlayType(type) || "").trim().toLowerCase();
    return result !== "" && result !== "no";
  }

  function videoEnsureAuthoredChildren(video, props) {
    if (!video || !props || typeof props !== "object") {
      return;
    }
    let hasSourceChildren = false;
    let hasTrackChildren = false;
    for (const child of Array.from(video.childNodes || [])) {
      if (!child || child.nodeType !== 1) {
        continue;
      }
      if (child.tagName === "SOURCE") {
        hasSourceChildren = true;
      } else if (child.tagName === "TRACK") {
        hasTrackChildren = true;
      }
    }
    if (!hasSourceChildren) {
      for (const source of videoSourcesFromProps(props)) {
        const sourceNode = document.createElement("source");
        sourceNode.setAttribute("src", source.src);
        if (source.type) {
          sourceNode.setAttribute("type", source.type);
        }
        if (source.media) {
          sourceNode.setAttribute("media", source.media);
        }
        video.appendChild(sourceNode);
      }
    }
    if (!hasTrackChildren) {
      for (const track of videoTracksFromProps(props)) {
        const trackURL = videoTrackURL(track, props);
        if (!trackURL) {
          continue;
        }
        const trackNode = document.createElement("track");
        trackNode.setAttribute("src", trackURL);
        trackNode.setAttribute("kind", track.kind || "subtitles");
        if (track.srclang) {
          trackNode.setAttribute("srclang", track.srclang);
        }
        if (track.title) {
          trackNode.setAttribute("label", track.title);
        }
        if (track.default) {
          trackNode.setAttribute("default", "true");
        }
        video.appendChild(trackNode);
      }
    }
  }

  function videoSanitizeCueHTML(text) {
    const escaped = String(text == null ? "" : text)
      .replace(/&/g, "&amp;")
      .replace(/</g, "&lt;")
      .replace(/>/g, "&gt;");
    return escaped
      .replace(/&lt;(\/?)(b|i|u|s)&gt;/gi, "<$1$2>");
  }

  function videoParseTimestamp(value) {
    const text = String(value || "").trim();
    if (!text) {
      return -1;
    }
    const parts = text.split(":");
    if (parts.length < 2 || parts.length > 3) {
      return -1;
    }
    let hours = 0;
    let minutes = 0;
    let seconds = 0;
    if (parts.length === 3) {
      hours = Number(parts[0]);
      minutes = Number(parts[1]);
      seconds = Number(parts[2].replace(",", "."));
    } else {
      minutes = Number(parts[0]);
      seconds = Number(parts[1].replace(",", "."));
    }
    if (!Number.isFinite(hours) || !Number.isFinite(minutes) || !Number.isFinite(seconds)) {
      return -1;
    }
    return Math.round(((hours * 3600) + (minutes * 60) + seconds) * 1000);
  }

  function parseVideoVTT(text) {
    const raw = String(text == null ? "" : text).replace(/\r/g, "");
    const lines = raw.split("\n");
    const cues = [];
    let index = 0;

    while (index < lines.length) {
      let line = String(lines[index] || "").trim();
      if (!line) {
        index += 1;
        continue;
      }
      if (/^WEBVTT/i.test(line)) {
        index += 1;
        continue;
      }
      if (/^NOTE\b/i.test(line)) {
        index += 1;
        while (index < lines.length && String(lines[index] || "").trim() !== "") {
          index += 1;
        }
        continue;
      }
      if (!line.includes("-->")) {
        index += 1;
        line = String(lines[index] || "").trim();
      }
      if (!line.includes("-->")) {
        index += 1;
        continue;
      }
      const timing = line.split("-->");
      const startMS = videoParseTimestamp(timing[0]);
      const endBits = String(timing[1] || "").trim().split(/\s+/);
      const endMS = videoParseTimestamp(endBits[0]);
      index += 1;
      const textLines = [];
      while (index < lines.length && String(lines[index] || "").trim() !== "") {
        textLines.push(String(lines[index] || ""));
        index += 1;
      }
      if (startMS < 0 || endMS <= startMS) {
        continue;
      }
      cues.push({
        startMS,
        endMS,
        text: videoSanitizeCueHTML(textLines.join("\n")),
      });
    }

    cues.sort(function(a, b) {
      if (a.startMS !== b.startMS) {
        return a.startMS - b.startMS;
      }
      return a.endMS - b.endMS;
    });
    return cues;
  }

  function videoActiveCues(cues, currentTimeSeconds) {
    const currentMS = Math.max(0, Math.round(sceneNumber(currentTimeSeconds, 0) * 1000));
    const active = [];
    for (const cue of cues || []) {
      if (cue.startMS <= currentMS && currentMS < cue.endMS) {
        active.push({ text: cue.text });
      }
      if (cue.startMS > currentMS) {
        break;
      }
    }
    return active;
  }

  function videoApplyElementProps(video, props) {
    if (!video || !props || typeof props !== "object") {
      return;
    }
    const stringAttrs = [
      ["poster", "poster"],
      ["preload", "preload"],
      ["crossorigin", "crossOrigin"],
      ["crossorigin", "crossorigin"],
    ];
    for (const entry of stringAttrs) {
      const value = videoPropValue(props, [entry[1], entry[0]], "");
      if (value == null || value === "") {
        continue;
      }
      video.setAttribute(entry[0], String(value));
    }
    const boolAttrs = [
      ["autoplay", ["autoplay", "autoPlay"]],
      ["controls", ["controls"]],
      ["loop", ["loop"]],
      ["muted", ["muted"]],
      ["playsinline", ["playsinline", "playsInline"]],
    ];
    for (const entry of boolAttrs) {
      const enabled = sceneBool(videoPropValue(props, entry[1], false), false);
      if (enabled) {
        video.setAttribute(entry[0], "true");
      } else if (typeof video.removeAttribute === "function") {
        video.removeAttribute(entry[0]);
      }
    }
    if (sceneBool(videoPropValue(props, ["muted"], false), false)) {
      video.muted = true;
    }
    const width = Math.max(0, Math.round(sceneNumber(videoPropValue(props, ["width"], 0), 0)));
    const height = Math.max(0, Math.round(sceneNumber(videoPropValue(props, ["height"], 0), 0)));
    if (width > 0) {
      video.setAttribute("width", String(width));
      video.width = width;
    }
    if (height > 0) {
      video.setAttribute("height", String(height));
      video.height = height;
    }
  }

  function videoSyncURL(path) {
    const source = String(path || "").trim();
    if (!source) {
      return "";
    }
    return isAbsoluteHubURL(source) ? source : hubURL(source);
  }

  async function createBuiltInVideoEngine(ctx) {
    const mount = ctx && ctx.mount;
    if (!mount) {
      return {};
    }

    const props = ctx && ctx.props && typeof ctx.props === "object" ? ctx.props : {};
    const fallbackChildren = mount && mount.childNodes ? Array.from(mount.childNodes) : [];
    const authoredSources = videoSourcesFromProps(props);
    const video = videoFirstFallbackNode(fallbackChildren, "VIDEO") || document.createElement("video");
    const unsubscribers = [];
    const eventListeners = [];
    const subtitleState = {
      tracks: videoTracksFromProps(props),
      loadedID: "",
      activeID: "",
      cues: [],
      lastSignature: "",
      status: "idle",
    };
    let disposed = false;
    let hls = null;
    let syncSocket = null;
    let reconnectTimer = 0;
    let followTimer = 0;
    let lastLeadSendAt = 0;
    let followState = null;
    let requestedRate = Math.max(0.1, sceneNumber(readVideoSignal("rate", videoPropValue(props, ["rate"], 1)), 1));
    let lastError = "";
    let stalled = false;
    let resizeObserver = null;
    let currentSource = "";

    function setError(message) {
      lastError = String(message || "").trim();
      writeVideoSignal("error", lastError);
    }

    function clearError() {
      if (!lastError) {
        return;
      }
      lastError = "";
      writeVideoSignal("error", "");
    }

    function updateSubtitleOutputs() {
      writeVideoSignal("subtitleTracks", subtitleState.tracks.slice());
      writeVideoSignal("subtitleStatus", subtitleState.status);
    }

    function updateCueOutputs() {
      const next = videoActiveCues(subtitleState.cues, sceneNumber(video.currentTime, 0));
      const signature = JSON.stringify(next);
      if (signature === subtitleState.lastSignature) {
        return;
      }
      subtitleState.lastSignature = signature;
      writeVideoSignal("activeCues", next);
    }

    function updateVideoOutputs() {
      const duration = Math.max(0, sceneNumber(video.duration, 0));
      const viewport = videoViewportSize(mount);
      const playing = !sceneBool(video.paused, true) && !sceneBool(video.ended, false);
      writeVideoSignal("position", Math.max(0, sceneNumber(video.currentTime, 0)));
      writeVideoSignal("duration", duration);
      writeVideoSignal("playing", playing);
      writeVideoSignal("buffered", videoBufferedAhead(video));
      writeVideoSignal("stalled", stalled);
      writeVideoSignal("fullscreen", Boolean(document && document.fullscreenElement && (document.fullscreenElement === mount || document.fullscreenElement === video)));
      writeVideoSignal("viewport", viewport);
      writeVideoSignal("ready", sceneNumber(video.readyState, 0) >= 2);
      writeVideoSignal("muted", Boolean(video.muted));
      writeVideoSignal("actualRate", sceneNumber(video.playbackRate, requestedRate));
      writeVideoSignal("syncConnected", Boolean(syncSocket && syncSocket.readyState === 1));
      updateSubtitleOutputs();
      updateCueOutputs();
      if (!lastError) {
        writeVideoSignal("error", "");
      }
    }

    function addListener(target, type, listener) {
      if (!target || typeof target.addEventListener !== "function") {
        return;
      }
      target.addEventListener(type, listener);
      eventListeners.push({ target, type, listener });
    }

    function teardownHLS() {
      if (hls && typeof hls.destroy === "function") {
        hls.destroy();
      }
      hls = null;
    }

    function projectedFollowPosition(state) {
      if (!state) {
        return 0;
      }
      let position = Math.max(0, sceneNumber(state.position, 0));
      const playing = sceneBool(state.playing, false);
      if (playing) {
        const sentAtMS = sceneNumber(state.sentAtMS, 0);
        const rate = Math.max(0.1, sceneNumber(state.rate, 1));
        if (sentAtMS > 0) {
          position += Math.max(0, (Date.now() - sentAtMS) / 1000) * rate;
        }
      }
      return position;
    }

    function applyRequestedRate() {
      const mode = String(videoPropValue(props, ["syncMode", "sync_mode"], "follow") || "follow").trim().toLowerCase();
      if (mode === "follow" && followState) {
        return;
      }
      video.playbackRate = requestedRate;
      updateVideoOutputs();
    }

    function safePlay() {
      try {
        const result = video.play();
        if (result && typeof result.then === "function") {
          return result.catch(function(error) {
            setError(error && error.message ? error.message : "playback failed");
            updateVideoOutputs();
          });
        }
        clearError();
      } catch (error) {
        setError(error && error.message ? error.message : "playback failed");
      }
      updateVideoOutputs();
      return Promise.resolve();
    }

    function applyFollowState() {
      if (!followState || disposed) {
        return;
      }
      const strategy = String(videoPropValue(props, ["syncStrategy", "sync_strategy"], "nudge") || "nudge").trim().toLowerCase();
      const playing = sceneBool(followState.playing, false);
      const target = projectedFollowPosition(followState);
      const drift = Math.max(-9999, Math.min(9999, sceneNumber(video.currentTime, 0) - target, 0));
      if (playing) {
        safePlay();
      } else {
        video.pause();
      }
      if (strategy === "snap") {
        if (Math.abs(drift) > 1) {
          video.currentTime = Math.max(0, target);
        }
        video.playbackRate = requestedRate;
      } else if (Math.abs(drift) > 5) {
        video.currentTime = Math.max(0, target);
        video.playbackRate = requestedRate;
      } else if (drift > 0.5) {
        video.playbackRate = 0.92;
      } else if (drift < -0.5) {
        video.playbackRate = 1.08;
      } else {
        video.playbackRate = requestedRate;
      }
      updateVideoOutputs();
    }

    function clearFollowTimer() {
      if (followTimer) {
        clearInterval(followTimer);
        followTimer = 0;
      }
    }

    function ensureFollowTimer() {
      if (followTimer || String(videoPropValue(props, ["syncMode", "sync_mode"], "follow")).trim().toLowerCase() !== "follow") {
        return;
      }
      followTimer = setInterval(applyFollowState, 500);
    }

    function sendLeadSnapshot(force) {
      if (!syncSocket || syncSocket.readyState !== 1) {
        return;
      }
      const mode = String(videoPropValue(props, ["syncMode", "sync_mode"], "follow")).trim().toLowerCase();
      if (mode !== "lead") {
        return;
      }
      const now = Date.now();
      if (!force && now-lastLeadSendAt < 250) {
        return;
      }
      lastLeadSendAt = now;
      try {
        syncSocket.send(JSON.stringify({
          type: "sync",
          mediaID: currentSource,
          position: Math.max(0, sceneNumber(video.currentTime, 0)),
          playing: !sceneBool(video.paused, true) && !sceneBool(video.ended, false),
          rate: sceneNumber(video.playbackRate, requestedRate),
          sentAtMS: now,
        }));
      } catch (_error) {
      }
    }

    function clearReconnectTimer() {
      if (reconnectTimer) {
        clearTimeout(reconnectTimer);
        reconnectTimer = 0;
      }
    }

    function closeSyncSocket() {
      clearReconnectTimer();
      clearFollowTimer();
      if (syncSocket && typeof syncSocket.close === "function") {
        syncSocket.close();
      }
      syncSocket = null;
      writeVideoSignal("syncConnected", false);
    }

    function connectSync(attempt) {
      const rawURL = videoSyncURL(videoPropValue(props, ["sync"], ""));
      if (!rawURL || typeof WebSocket !== "function" || disposed) {
        writeVideoSignal("syncConnected", false);
        return;
      }
      const retryAttempt = Math.max(0, attempt || 0);
      const socket = new WebSocket(rawURL);
      syncSocket = socket;
      socket.onopen = function() {
        writeVideoSignal("syncConnected", true);
        updateVideoOutputs();
        if (String(videoPropValue(props, ["syncMode", "sync_mode"], "follow")).trim().toLowerCase() === "lead") {
          sendLeadSnapshot(true);
        } else {
          ensureFollowTimer();
        }
      };
      socket.onclose = function() {
        writeVideoSignal("syncConnected", false);
        updateVideoOutputs();
        if (disposed) {
          return;
        }
        const delay = Math.min(30000, Math.max(1000, 1000 * Math.pow(2, retryAttempt)));
        reconnectTimer = setTimeout(function() {
          connectSync(retryAttempt + 1);
        }, delay);
      };
      socket.onerror = function() {
        writeVideoSignal("syncConnected", false);
      };
      socket.onmessage = function(event) {
        let message = null;
        try {
          message = JSON.parse(String(event && event.data || ""));
        } catch (_error) {
          return;
        }
        if (!message || message.type !== "sync") {
          return;
        }
        if (message.mediaID && currentSource && String(message.mediaID) !== String(currentSource)) {
          return;
        }
        followState = message;
        if (String(videoPropValue(props, ["syncMode", "sync_mode"], "follow")).trim().toLowerCase() === "follow") {
          ensureFollowTimer();
          applyFollowState();
        }
      };
    }

    async function loadSubtitleTrack(trackID) {
      const selected = String(trackID || "").trim();
      subtitleState.activeID = selected;
      subtitleState.cues = [];
      subtitleState.lastSignature = "";
      if (!selected) {
        subtitleState.loadedID = "";
        subtitleState.status = subtitleState.tracks.length > 0 ? "ready" : "idle";
        updateSubtitleOutputs();
        updateCueOutputs();
        return;
      }
      const localTrack = subtitleState.tracks.find(function(track) {
        return track.id === selected;
      });
      if (!localTrack) {
        subtitleState.status = "error";
        updateSubtitleOutputs();
        return;
      }
      if (hls && Array.isArray(hls.subtitleTracks) && Object.prototype.hasOwnProperty.call(hls, "subtitleTrack")) {
        const nextIndex = hls.subtitleTracks.findIndex(function(track) {
          return videoNormalizeTrackInfo(track, 0).id === selected;
        });
        if (nextIndex >= 0) {
          hls.subtitleTrack = nextIndex;
        }
      }
      const subtitleURL = videoTrackURL(localTrack, props);
      if (!subtitleURL) {
        subtitleState.status = "ready";
        updateSubtitleOutputs();
        updateCueOutputs();
        return;
      }
      subtitleState.status = "loading";
      updateSubtitleOutputs();
      for (let attempt = 0; attempt < 60; attempt += 1) {
        const response = await fetch(subtitleURL);
        if (response.status === 202) {
          subtitleState.status = "warming";
          updateSubtitleOutputs();
          await new Promise(function(resolve) {
            setTimeout(resolve, 1500);
          });
          continue;
        }
        if (!response.ok) {
          subtitleState.status = "error";
          setError("subtitle fetch failed");
          updateSubtitleOutputs();
          return;
        }
        const text = await response.text();
        subtitleState.cues = parseVideoVTT(text);
        subtitleState.loadedID = selected;
        subtitleState.status = "ready";
        clearError();
        updateSubtitleOutputs();
        updateCueOutputs();
        return;
      }
      subtitleState.status = "error";
      setError("subtitle warmup timed out");
      updateSubtitleOutputs();
    }

    async function applySource(source) {
      const requestedSource = String(source || "").trim();
      let nextSource = requestedSource;
      let useAuthoredSources = false;
      if (!nextSource && authoredSources.length > 0) {
        let nativeCandidate = null;
        for (const candidate of authoredSources) {
          if (videoCanUseSourceNatively(video, candidate)) {
            nativeCandidate = candidate;
            break;
          }
        }
        if (nativeCandidate) {
          nextSource = nativeCandidate.src;
          useAuthoredSources = true;
        } else {
          const hlsCandidate = authoredSources.find(function(candidate) {
            return videoNeedsHLS(candidate && candidate.src);
          });
          if (hlsCandidate) {
            nextSource = hlsCandidate.src;
          } else {
            nextSource = authoredSources[0].src;
            useAuthoredSources = true;
          }
        }
      }
      currentSource = nextSource;
      clearError();
      followState = null;
      clearFollowTimer();
      teardownHLS();
      subtitleState.cues = [];
      subtitleState.lastSignature = "";
      updateCueOutputs();
      if (!nextSource && !useAuthoredSources) {
        if (typeof video.removeAttribute === "function") {
          video.removeAttribute("src");
        }
        try {
          video.src = "";
        } catch (_error) {
        }
        if (typeof video.load === "function") {
          video.load();
        }
        updateVideoOutputs();
        return;
      }
      if (useAuthoredSources) {
        if (typeof video.removeAttribute === "function") {
          video.removeAttribute("src");
        }
        try {
          video.src = "";
        } catch (_error) {
        }
        if (typeof video.load === "function") {
          video.load();
        }
      } else if (videoNeedsHLS(nextSource) && !videoSupportsNativeHLS(video)) {
        const HlsCtor = await ensureVideoHLSLibrary();
        if (!HlsCtor) {
          setError("HLS.js unavailable");
          updateVideoOutputs();
          return;
        }
        const supported = typeof HlsCtor.isSupported === "function" ? HlsCtor.isSupported() : true;
        if (!supported) {
          setError("HLS playback unsupported");
          updateVideoOutputs();
          return;
        }
        hls = new HlsCtor(videoPropValue(props, ["hls", "hlsConfig"], {}));
        if (hls && typeof hls.attachMedia === "function") {
          hls.attachMedia(video);
        }
        if (hls && typeof hls.loadSource === "function") {
          hls.loadSource(nextSource);
        }
        if (hls && typeof hls.on === "function" && HlsCtor.Events) {
          if (HlsCtor.Events.MANIFEST_PARSED) {
            hls.on(HlsCtor.Events.MANIFEST_PARSED, function() {
              if (Array.isArray(hls.subtitleTracks) && hls.subtitleTracks.length > 0) {
                subtitleState.tracks = hls.subtitleTracks.map(videoNormalizeTrackInfo);
                updateSubtitleOutputs();
              }
              clearError();
              updateVideoOutputs();
            });
          }
          if (HlsCtor.Events.SUBTITLE_TRACKS_UPDATED) {
            hls.on(HlsCtor.Events.SUBTITLE_TRACKS_UPDATED, function(_event, data) {
              const tracks = data && Array.isArray(data.subtitleTracks) ? data.subtitleTracks : (Array.isArray(hls.subtitleTracks) ? hls.subtitleTracks : []);
              subtitleState.tracks = tracks.map(videoNormalizeTrackInfo);
              updateSubtitleOutputs();
            });
          }
          if (HlsCtor.Events.ERROR) {
            hls.on(HlsCtor.Events.ERROR, function(_event, data) {
              if (data && data.fatal) {
                setError(data && data.details ? data.details : "video transport failed");
                updateVideoOutputs();
              }
            });
          }
        }
      } else {
        video.src = nextSource;
        if (typeof video.load === "function") {
          video.load();
        }
      }
      updateVideoOutputs();
      const activeSubtitleTrack = readVideoSignal("subtitleTrack", videoPropValue(props, ["subtitleTrack", "subtitle_track"], ""));
      await loadSubtitleTrack(activeSubtitleTrack);
      if (String(videoPropValue(props, ["sync"], "")).trim() !== "") {
        closeSyncSocket();
        connectSync(0);
      }
    }

    video.setAttribute("data-gosx-video", "true");
    videoApplyElementProps(video, props);
    videoEnsureAuthoredChildren(video, props);
    videoClearChildren(mount);
    mount.appendChild(video);
    subtitleState.status = subtitleState.tracks.length > 0 ? "ready" : "idle";
    writeVideoSignal("subtitleTracks", subtitleState.tracks.slice());
    writeVideoSignal("subtitleStatus", subtitleState.status);
    writeVideoSignal("activeCues", []);
    writeVideoSignal("syncConnected", false);
    writeVideoSignal("error", "");

    addListener(video, "timeupdate", function() {
      updateVideoOutputs();
      sendLeadSnapshot(false);
    });
    addListener(video, "durationchange", updateVideoOutputs);
    addListener(video, "loadedmetadata", updateVideoOutputs);
    addListener(video, "canplay", function() {
      stalled = false;
      clearError();
      updateVideoOutputs();
    });
    addListener(video, "play", function() {
      stalled = false;
      clearError();
      updateVideoOutputs();
      sendLeadSnapshot(true);
    });
    addListener(video, "pause", function() {
      stalled = false;
      updateVideoOutputs();
      sendLeadSnapshot(true);
    });
    addListener(video, "seeked", function() {
      updateVideoOutputs();
      sendLeadSnapshot(true);
    });
    addListener(video, "waiting", function() {
      stalled = true;
      updateVideoOutputs();
    });
    addListener(video, "stalled", function() {
      stalled = true;
      updateVideoOutputs();
    });
    addListener(video, "volumechange", function() {
      updateVideoOutputs();
    });
    addListener(video, "ratechange", function() {
      updateVideoOutputs();
      sendLeadSnapshot(true);
    });
    addListener(video, "error", function() {
      const mediaError = video && video.error && video.error.message ? video.error.message : "video playback failed";
      setError(mediaError);
      updateVideoOutputs();
    });
    addListener(document, "fullscreenchange", updateVideoOutputs);

    if (typeof ResizeObserver === "function") {
      resizeObserver = new ResizeObserver(function() {
        updateVideoOutputs();
      });
      resizeObserver.observe(mount);
    }

    unsubscribers.push(subscribeVideoSignal("src", function(value) {
      applySource(value);
    }));
    unsubscribers.push(subscribeVideoSignal("seek", function(value) {
      const mode = String(videoPropValue(props, ["syncMode", "sync_mode"], "follow")).trim().toLowerCase();
      if (mode === "follow" && String(videoPropValue(props, ["sync"], "")).trim() !== "") {
        return;
      }
      const nextTime = sceneNumber(value, -1);
      if (nextTime >= 0) {
        video.currentTime = nextTime;
        updateVideoOutputs();
      }
    }));
    unsubscribers.push(subscribeVideoSignal("command", function(value) {
      const mode = String(videoPropValue(props, ["syncMode", "sync_mode"], "follow")).trim().toLowerCase();
      if (mode === "follow" && String(videoPropValue(props, ["sync"], "")).trim() !== "") {
        return;
      }
      const command = String(value || "").trim().toLowerCase();
      if (!command) {
        return;
      }
      if (command === "play") {
        safePlay();
      } else if (command === "pause") {
        video.pause();
      } else if (command === "toggle") {
        if (sceneBool(video.paused, true)) {
          safePlay();
        } else {
          video.pause();
        }
      } else if ((command === "enter-fullscreen" || command === "toggle-fullscreen") && mount && typeof mount.requestFullscreen === "function") {
        mount.requestFullscreen();
      } else if (command === "exit-fullscreen" && document && typeof document.exitFullscreen === "function") {
        document.exitFullscreen();
      }
      updateVideoOutputs();
    }));
    unsubscribers.push(subscribeVideoSignal("volume", function(value) {
      const volume = Math.max(0, Math.min(1, sceneNumber(value, sceneNumber(video.volume, 1))));
      video.volume = volume;
      updateVideoOutputs();
    }));
    unsubscribers.push(subscribeVideoSignal("mute", function(value) {
      video.muted = sceneBool(value, Boolean(video.muted));
      updateVideoOutputs();
    }));
    unsubscribers.push(subscribeVideoSignal("rate", function(value) {
      requestedRate = Math.max(0.1, sceneNumber(value, requestedRate));
      const mode = String(videoPropValue(props, ["syncMode", "sync_mode"], "follow")).trim().toLowerCase();
      if (!(mode === "follow" && String(videoPropValue(props, ["sync"], "")).trim() !== "")) {
        applyRequestedRate();
      }
    }));
    unsubscribers.push(subscribeVideoSignal("subtitleTrack", function(value) {
      loadSubtitleTrack(value);
    }));

    const initialVolume = Math.max(0, Math.min(1, sceneNumber(readVideoSignal("volume", videoPropValue(props, ["volume"], 1)), 1)));
    video.volume = initialVolume;
    video.muted = sceneBool(readVideoSignal("mute", videoPropValue(props, ["muted"], false)), false);
    video.playbackRate = requestedRate;
    updateVideoOutputs();

    const initialSource = readVideoSignal("src", videoPropValue(props, ["src", "Src"], ""));
    await applySource(initialSource);

    return {
      video,
      dispose() {
        disposed = true;
        closeSyncSocket();
        teardownHLS();
        if (resizeObserver && typeof resizeObserver.disconnect === "function") {
          resizeObserver.disconnect();
        }
        for (const entry of eventListeners) {
          if (entry.target && typeof entry.target.removeEventListener === "function") {
            entry.target.removeEventListener(entry.type, entry.listener);
          }
        }
        for (const unsub of unsubscribers) {
          if (typeof unsub === "function") {
            unsub();
          }
        }
        videoRestoreChildren(mount, fallbackChildren);
      },
    };
  }

  function engineCapabilityUnsupportedMessage(entry, status) {
    const custom = entry && entry.props && entry.props.unsupportedMessage;
    if (typeof custom === "string" && custom.trim() !== "") {
      return custom.trim();
    }
    const missing = status && status.missing && status.missing.length > 0
      ? status.missing.join(", ")
      : "required browser APIs";
    return `This experience requires ${missing} support. Use a current browser with hardware acceleration enabled.`;
  }

  function showEngineCapabilityUnsupported(mount, entry, status) {
    if (!mount || !document || typeof document.createElement !== "function") {
      return;
    }
    clearChildren(mount);
    const wrapper = document.createElement("div");
    wrapper.setAttribute("class", "gosx-engine-unsupported");
    wrapper.setAttribute("data-gosx-engine-unsupported", "true");
    wrapper.setAttribute("data-gosx-engine-unsupported-reason", "missing-capability");
    wrapper.setAttribute("role", "alert");
    wrapper.textContent = engineCapabilityUnsupportedMessage(entry, status);
    mount.appendChild(wrapper);
  }

  function reportMissingEngineCapabilities(entry, mount, status) {
    const missing = status.missing.join(", ");
    console.error(`[gosx] missing required engine capabilities for ${entry.id}: ${missing}`);
    if (typeof window !== "undefined" && typeof window.__gosx_emit === "function") {
      window.__gosx_emit("error", "engine", "missing required engine capabilities", {
        component: String(entry.component || ""),
        engineID: String(entry.id || ""),
        missingCapabilities: status.missing.slice(),
        requiredCapabilities: status.required.slice(),
      });
    }
    if (window.__gosx && typeof window.__gosx.reportIssue === "function") {
      window.__gosx.reportIssue({
        scope: "engine",
        type: "capability",
        component: entry.component,
        source: entry.id,
        ref: status.missing.join(" "),
        element: mount,
        message: `missing required engine capabilities: ${missing}`,
        fallback: "unsupported",
      });
    }
  }

  function createEngineContext(entry, mount, runtime, capabilityStatus) {
    return {
      id: entry.id,
      kind: entry.kind,
      component: entry.component,
      mount: mount,
      props: entry.props || {},
      capabilities: entry.capabilities || [],
      requiredCapabilities: requiredCapabilityList(entry),
      capabilityStatus: capabilityStatus || engineCapabilityStatus(entry),
      programRef: entry.programRef || "",
      runtimeMode: entry.runtime || "",
      runtime: runtime,
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
    if (engineKindNeedsMount(entry.kind) && !mount) return;
    const capabilityStatus = engineCapabilityStatus(entry);
    applyRuntimeCapabilityState(mount, "engine", capabilityStatus);
    if (!capabilityStatus.ok) {
      showEngineCapabilityUnsupported(mount, entry, capabilityStatus);
      reportMissingEngineCapabilities(entry, mount, capabilityStatus);
      return;
    }
    const runtime = createEngineRuntime(entry, mount);
    const ctx = createEngineContext(entry, mount, runtime, capabilityStatus);
    if (entry.props && entry.props.audio && window.__gosx && window.__gosx.audio && typeof window.__gosx.audio.registerManifest === "function") {
      window.__gosx.audio.registerManifest(entry.props.audio);
    }
    pendingEngineRuntimes.set(entry.id, runtime);

    const factory = await resolveMountedEngineFactory(entry);
    if (typeof factory !== "function") {
      pendingEngineRuntimes.delete(entry.id);
      console.warn(`[gosx] no engine factory registered for ${entry.component}`);
      if (typeof window !== "undefined" && typeof window.__gosx_emit === "function") {
        window.__gosx_emit("warn", "engine", "no engine factory registered", {
          component: String(entry.component || ""),
          engineID: String(entry.id || ""),
        });
      }
      if (window.__gosx && typeof window.__gosx.reportIssue === "function") {
        window.__gosx.reportIssue({
          scope: "engine",
          type: "factory",
          component: entry.component,
          source: entry.id,
          ref: entry.component,
          element: mount,
          message: `no engine factory registered for ${entry.component}`,
          fallback: "server",
        });
      }
      return;
    }

    try {
      const mounted = await runEngineFactory(factory, ctx);
      pendingEngineRuntimes.delete(entry.id);
      rememberMountedEngine(entry, mount, mounted.context, mounted.handle);
    } catch (e) {
      pendingEngineRuntimes.delete(entry.id);
      if (runtime && typeof runtime.dispose === "function") {
        runtime.dispose();
      }
      console.error(`[gosx] failed to mount engine ${entry.id}:`, e);
      if (typeof window !== "undefined" && typeof window.__gosx_emit === "function") {
        window.__gosx_emit("error", "engine", "failed to mount engine", {
          component: String(entry.component || ""),
          engineID: String(entry.id || ""),
          error: e && e.message ? String(e.message) : String(e),
          stack: e && e.stack ? String(e.stack) : "",
        });
      }
      if (window.__gosx && typeof window.__gosx.reportIssue === "function") {
        window.__gosx.reportIssue({
          scope: "engine",
          type: "mount",
          component: entry.component,
          source: entry.id,
          ref: entry.programRef || entry.component,
          element: mount,
          message: `failed to mount engine ${entry.id}`,
          error: e,
          fallback: "server",
        });
      }
    }
  }

  function resolveEngineMount(entry) {
    if (!engineKindNeedsMount(entry.kind)) {
      return null;
    }
    const mountID = entry.mountId || entry.id;
    const mount = document.getElementById(mountID);
    if (!mount) {
      console.warn(`[gosx] engine mount #${mountID} not found for ${entry.id}`);
      if (window.__gosx && typeof window.__gosx.reportIssue === "function") {
        window.__gosx.reportIssue({
          scope: "engine",
          type: "mount",
          component: entry.component,
          source: entry.id,
          ref: mountID,
          message: `engine mount #${mountID} not found`,
          fallback: "server",
        });
      }
      return null;
    }
    return mount;
  }

  async function resolveMountedEngineFactory(entry) {
    return resolveEngineFactory(entry);
  }

  async function runEngineFactory(factory, ctx) {
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
    if (window.__gosx && typeof window.__gosx.clearIssueState === "function") {
      window.__gosx.clearIssueState(mount);
    }
    activateInputProviders(entry);
    window.__gosx.engines.set(entry.id, {
      component: entry.component,
      kind: entry.kind,
      capabilities: capabilityList(entry),
      requiredCapabilities: requiredCapabilityList(entry),
      capabilityStatus: context.capabilityStatus || engineCapabilityStatus(entry),
      runtime: context.runtime,
      mount: mount,
      handle: handle,
    });
  }

  async function mountAllEngines(manifest) {
    if (!manifest.engines || manifest.engines.length === 0) return;

    const videoEngines = manifest.engines.filter(function(entry) {
      return entry && entry.kind === "video";
    });
    if (videoEngines.length > 1) {
      console.error("[gosx] only one video engine is supported per page");
      if (window.__gosx && typeof window.__gosx.reportIssue === "function") {
        for (const entry of videoEngines.slice(1)) {
          window.__gosx.reportIssue({
            scope: "engine",
            type: "mount",
            component: entry.component,
            source: entry.id,
            ref: entry.id,
            message: "only one video engine is supported per page",
            fallback: "server",
          });
        }
      }
    }

    const promises = manifest.engines.filter(function(entry, index) {
      return entry.kind !== "video" || videoEngines.indexOf(entry) === 0;
    }).map(function(entry) {
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
    try {
      socket.binaryType = "arraybuffer";
    } catch (_e) {
      // Some test doubles and embedded runtimes expose binaryType as read-only.
    }
    socket.onmessage = function(evt) {
      const decoded = decodeHubMessage(entry, evt.data);
      if (decoded && typeof decoded.then === "function") {
        decoded.then(function(message) {
          if (!message) return;
          applyHubBindings(entry, message);
          emitHubEvent(entry, message);
        });
        return;
      }
      const message = decoded;
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

  window.__gosx_dispose_compute_island = function(islandID) {
    const record = window.__gosx.computeIslands && window.__gosx.computeIslands.get(islandID);
    if (!record) return;

    releaseInputProviders(record);

    if (typeof window.__gosx_dispose === "function") {
      try {
        window.__gosx_dispose(islandID);
      } catch (e) {
        console.error(`[gosx] dispose error for compute island ${islandID}:`, e);
      }
    }

    window.__gosx.computeIslands.delete(islandID);
  };

  window.__gosx_dispose_engine = function(engineID) {
    const pending = pendingEngineRuntimes.get(engineID);
    if (pending && typeof pending.dispose === "function") {
      try {
        pending.dispose();
      } catch (e) {
        console.error(`[gosx] pending runtime dispose error for engine ${engineID}:`, e);
      }
    }
    pendingEngineRuntimes.delete(engineID);

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

  window.__gosx_engine_frame = function(engineID) {
    const pending = pendingEngineRuntimes.get(engineID);
    if (pending && typeof pending.frame === "function") {
      return pending.frame();
    }
    const record = window.__gosx.engines.get(engineID);
    if (!record || !record.runtime || typeof record.runtime.frame !== "function") {
      return null;
    }
    return record.runtime.frame();
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
    if (window.__gosx.computeIslands) {
      for (const islandID of Array.from(window.__gosx.computeIslands.keys())) {
        window.__gosx_dispose_compute_island(islandID);
      }
    }
    for (const engineID of Array.from(window.__gosx.engines.keys())) {
      window.__gosx_dispose_engine(engineID);
    }
    for (const hubID of Array.from(window.__gosx.hubs.keys())) {
      window.__gosx_disconnect_hub(hubID);
    }
    disposeManagedMotion();
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

    const program = await loadIslandProgram(entry, root);
    if (!program) return;
    if (!runIslandHydration(entry, root, program)) return;
    const listeners = setupEventDelegation(root, entry.id);
    rememberHydratedIsland(entry, root, listeners);
  }

  async function hydrateComputeIsland(entry) {
    if (!entry || entry.static) return;
    const capabilityStatus = runtimeCapabilityStatus(entry);
    if (!capabilityStatus.ok) {
      reportMissingComputeIslandCapabilities(entry, capabilityStatus);
      return;
    }

    const program = await loadIslandProgram(entry, null);
    if (!program) return;
    if (!runComputeIslandHydration(entry, program)) return;
    activateInputProviders(entry);
    rememberHydratedComputeIsland(entry);
  }

  function reportMissingComputeIslandCapabilities(entry, status) {
    const missing = status.missing.join(" ");
    console.error(`[gosx] missing required compute island capabilities for ${entry.id}: ${missing}`);
    if (typeof window !== "undefined" && typeof window.__gosx_emit === "function") {
      window.__gosx_emit("error", "compute-island", "missing required compute island capabilities", {
        islandID: String(entry.id || ""),
        component: String(entry.component || ""),
        missing,
      });
    }
    if (window.__gosx && typeof window.__gosx.reportIssue === "function") {
      window.__gosx.reportIssue({
        scope: "compute-island",
        type: "capability",
        component: entry.component,
        source: entry.id,
        message: `missing required compute island capabilities: ${missing}`,
        fallback: "none",
      });
    }
  }

  function islandRoot(entry) {
    const root = document.getElementById(entry.id);
    if (!root) {
      console.warn(`[gosx] island root #${entry.id} not found in DOM`);
      return null;
    }
    return root;
  }

  async function loadIslandProgram(entry, root) {
    const programFormat = inferProgramFormat(entry);
    if (!entry.programRef) {
      console.error(`[gosx] skipping island ${entry.id} — missing programRef`);
      if (typeof window !== "undefined" && typeof window.__gosx_emit === "function") {
        window.__gosx_emit("error", "island", "missing programRef", {
          islandID: String(entry.id || ""),
          component: String(entry.component || ""),
        });
      }
      if (window.__gosx && typeof window.__gosx.reportIssue === "function") {
        window.__gosx.reportIssue({
          scope: "island",
          type: "program",
          component: entry.component,
          source: entry.id,
          ref: entry.programRef,
          element: root,
          message: `missing programRef for island ${entry.id}`,
          fallback: "server",
        });
      }
      return null;
    }

    const programData = await fetchProgram(entry.programRef, programFormat);
    if (programData === null) {
      console.error(`[gosx] skipping island ${entry.id} — program fetch failed`);
      if (window.__gosx && typeof window.__gosx.reportIssue === "function") {
        window.__gosx.reportIssue({
          scope: "island",
          type: "program",
          component: entry.component,
          source: entry.id,
          ref: entry.programRef,
          element: root,
          message: `failed to fetch island program for ${entry.id}`,
          fallback: "server",
        });
      }
      return null;
    }
    return { data: programData, format: programFormat };
  }

  function runIslandHydration(entry, root, program) {
    const hydrateFn = window.__gosx_hydrate;
    if (typeof hydrateFn !== "function") {
      console.error("[gosx] __gosx_hydrate not available — cannot hydrate island", entry.id);
      if (window.__gosx && typeof window.__gosx.reportIssue === "function") {
        window.__gosx.reportIssue({
          scope: "island",
          type: "hydrate",
          component: entry.component,
          source: entry.id,
          ref: entry.programRef,
          element: root,
          message: `__gosx_hydrate not available for island ${entry.id}`,
          fallback: "server",
        });
      }
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
        if (typeof window !== "undefined" && typeof window.__gosx_emit === "function") {
          window.__gosx_emit("error", "island", "failed to hydrate island", {
            islandID: String(entry.id || ""),
            component: String(entry.component || ""),
            programRef: String(entry.programRef || ""),
            reason: String(result),
          });
        }
        if (window.__gosx && typeof window.__gosx.reportIssue === "function") {
          window.__gosx.reportIssue({
            scope: "island",
            type: "hydrate",
            component: entry.component,
            source: entry.id,
            ref: entry.programRef,
            element: root,
            message: result,
            fallback: "server",
          });
        }
        return false;
      }
      return true;
    } catch (e) {
      console.error(`[gosx] failed to hydrate island ${entry.id}:`, e);
      if (window.__gosx && typeof window.__gosx.reportIssue === "function") {
        window.__gosx.reportIssue({
          scope: "island",
          type: "hydrate",
          component: entry.component,
          source: entry.id,
          ref: entry.programRef,
          element: root,
          message: `failed to hydrate island ${entry.id}`,
          error: e,
          fallback: "server",
        });
      }
      return false;
    }
  }

  function runComputeIslandHydration(entry, program) {
    const hydrateFn = typeof window.__gosx_hydrate_compute === "function"
      ? window.__gosx_hydrate_compute
      : window.__gosx_hydrate;
    if (typeof hydrateFn !== "function") {
      console.error("[gosx] __gosx_hydrate_compute not available — cannot hydrate compute island", entry.id);
      if (window.__gosx && typeof window.__gosx.reportIssue === "function") {
        window.__gosx.reportIssue({
          scope: "compute-island",
          type: "hydrate",
          component: entry.component,
          source: entry.id,
          ref: entry.programRef,
          message: `__gosx_hydrate_compute not available for compute island ${entry.id}`,
          fallback: "none",
        });
      }
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
        console.error(`[gosx] failed to hydrate compute island ${entry.id}: ${result}`);
        if (typeof window !== "undefined" && typeof window.__gosx_emit === "function") {
          window.__gosx_emit("error", "compute-island", "failed to hydrate compute island", {
            islandID: String(entry.id || ""),
            component: String(entry.component || ""),
            programRef: String(entry.programRef || ""),
            reason: String(result),
          });
        }
        if (window.__gosx && typeof window.__gosx.reportIssue === "function") {
          window.__gosx.reportIssue({
            scope: "compute-island",
            type: "hydrate",
            component: entry.component,
            source: entry.id,
            ref: entry.programRef,
            message: result,
            fallback: "none",
          });
        }
        return false;
      }
      return true;
    } catch (e) {
      console.error(`[gosx] failed to hydrate compute island ${entry.id}:`, e);
      if (window.__gosx && typeof window.__gosx.reportIssue === "function") {
        window.__gosx.reportIssue({
          scope: "compute-island",
          type: "hydrate",
          component: entry.component,
          source: entry.id,
          ref: entry.programRef,
          message: `failed to hydrate compute island ${entry.id}`,
          error: e,
          fallback: "none",
        });
      }
      return false;
    }
  }

  function rememberHydratedIsland(entry, root, listeners) {
    if (window.__gosx && typeof window.__gosx.clearIssueState === "function") {
      window.__gosx.clearIssueState(root);
    }
    window.__gosx.islands.set(entry.id, {
      component: entry.component,
      root: root,
      listeners: listeners,
    });
  }

  function rememberHydratedComputeIsland(entry) {
    if (!window.__gosx.computeIslands) {
      window.__gosx.computeIslands = new Map();
    }
    window.__gosx.computeIslands.set(entry.id, {
      component: entry.component,
      capabilities: capabilityList(entry),
    });
  }

  // Hydrate all islands from the manifest. Called once the WASM runtime
  // signals readiness via __gosx_runtime_ready.
  async function hydrateAllIslands(manifest) {
    const islands = Array.isArray(manifest && manifest.islands) ? manifest.islands : [];
    const computeIslands = Array.isArray(manifest && manifest.computeIslands) ? manifest.computeIslands : [];
    if (islands.length === 0 && computeIslands.length === 0) return;

    // Hydrate islands concurrently — each is independent.
    const promises = islands.map(function(entry) {
      return hydrateIsland(entry).catch(function(e) {
        console.error(`[gosx] unexpected error hydrating ${entry.id}:`, e);
      });
    });
    for (const entry of computeIslands) {
      promises.push(hydrateComputeIsland(entry).catch(function(e) {
        console.error(`[gosx] unexpected error hydrating compute island ${entry.id}:`, e);
      }));
    }

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

    mountAllEngines(pendingManifest).then(function() {
      return Promise.all([
        hydrateAllIslands(pendingManifest),
        connectAllHubs(pendingManifest),
      ]);
    }).then(function() {
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
    mountManagedMotion(document.body || document.documentElement);
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
        console.error("[gosx] islands, compute islands, and hub bindings require manifest.runtime.path");
      }
      window.__gosx_runtime_ready();
    }
  }

  function manifestNeedsRuntimeBridge(manifest) {
    return manifestHasEntries(manifest, "islands")
      || manifestHasEntries(manifest, "computeIslands")
      || manifestHasEntries(manifest, "hubs")
      || manifestNeedsVideoBridge(manifest)
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

  function manifestNeedsVideoBridge(manifest) {
    if (!manifestHasEntries(manifest, "engines")) {
      return false;
    }
    return manifest.engines.some(function(entry) {
      return entry && entry.kind === "video";
    });
  }

  function manifestHasEntries(manifest, key) {
    return Boolean(manifest && manifest[key] && manifest[key].length > 0);
  }

  window.__gosx_bootstrap_page = bootstrapPage;
  window.__gosx_dispose_page = disposePage;

  // Bench-mode exports. Activated only when window.__gosx_bench_exports
  // is set to true BEFORE the bundle runs. Zero runtime cost in production
  // — single boolean check per page load, never touches any function
  // reference unless the flag is on. The bench harness at
  // client/js/runtime.bench.js uses these to microbenchmark hot path
  // functions in isolation without standing up the full DOM mount surface.
  if (window.__gosx_bench_exports === true) {
    window.__gosx_bench = {
      // 10-runtime-scene-core.js
      sceneRenderCamera: sceneRenderCamera,
      translateScenePointInto: translateScenePointInto,
      createSceneThickLineScratch: createSceneThickLineScratch,
      expandSceneThickLineIntoScratch: expandSceneThickLineIntoScratch,
      sceneBundleNeedsThickLines: sceneBundleNeedsThickLines,
      // 16-scene-webgl.js — file-scope light/exposure helpers.
      // (The per-frame functions like buildPBRDrawList, drawPBRObjectList,
      // and render live inside createScenePBRRenderer closures and are not
      // exposed here. Measure those via a real Scene3D mount in a follow-up
      // if/when we need end-to-end per-frame numbers.)
      scenePBRLightsHash: scenePBRLightsHash,
      hashLightContent: hashLightContent,
      hashEnvironmentContent: hashEnvironmentContent,
      scenePBRUploadLights: scenePBRUploadLights,
      scenePBRUploadExposure: scenePBRUploadExposure,
    };
  }

  // Start when DOM is ready.
  if (document.readyState === "loading") {
    document.addEventListener("DOMContentLoaded", bootstrapPage);
  } else {
    bootstrapPage();
  }
})();
