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
      const mode = normalizeSceneHTMLMode(item.mode, "dom");
      const fallback = typeof item.fallback === "string" && item.fallback.trim()
        ? item.fallback.trim()
        : (mode === "texture" ? "dom-overlay" : "");
      const fallbackReason = typeof item.fallbackReason === "string" && item.fallbackReason.trim()
        ? item.fallbackReason.trim()
        : (mode === "texture" ? "html-texture-manager-unavailable" : "");
      const textureWidth = Math.max(0, Math.floor(sceneNumber(item.textureWidth, 0)));
      const textureHeight = Math.max(0, Math.floor(sceneNumber(item.textureHeight, 0)));
      const textureBytes = Math.max(0, Math.floor(sceneNumber(item.textureBytes, textureWidth * textureHeight * 4)));
      const textureMaxBytes = Math.max(0, Math.floor(sceneNumber(item.textureMaxBytes, sceneNumber(item.maxTexturePixels, 0) * 4)));
      return {
        id: item.id || ("scene-html-" + index),
        target: typeof item.target === "string" && item.target.trim() ? item.target.trim() : (typeof item.targetID === "string" ? item.targetID.trim() : ""),
        mode,
        html: typeof item.html === "string" ? item.html : (typeof item.markup === "string" ? item.markup : ""),
        className: sceneLabelClassName(item),
        fallback,
        fallbackReason,
        textureKey: typeof item.textureKey === "string" ? item.textureKey.trim() : "",
        textureWidth,
        textureHeight,
        textureBytes,
        textureMaxBytes,
        textureOverBudget: sceneBool(item.textureOverBudget, textureMaxBytes > 0 && textureBytes > textureMaxBytes),
        textureReady: sceneBool(item.textureReady, false),
        surfaceWidth: Math.max(0, sceneNumber(item.surfaceWidth, 0)),
        surfaceHeight: Math.max(0, sceneNumber(item.surfaceHeight, 0)),
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
    bundle.surfaces = Array.isArray(bundle.surfaces) ? bundle.surfaces.map(function(surface, index) {
      const item = surface && typeof surface === "object" ? surface : {};
      return Object.assign({}, item, {
        id: item.id || ("scene-surface-" + index),
        sourceKind: typeof item.sourceKind === "string" ? item.sourceKind.trim() : "",
        sourceID: typeof item.sourceID === "string" ? item.sourceID.trim() : "",
        textureKey: typeof item.textureKey === "string" ? item.textureKey.trim() : "",
        textureWidth: Math.max(0, Math.floor(sceneNumber(item.textureWidth, 0))),
        textureHeight: Math.max(0, Math.floor(sceneNumber(item.textureHeight, 0))),
        textureBytes: Math.max(0, Math.floor(sceneNumber(item.textureBytes, 0))),
        textureMaxBytes: Math.max(0, Math.floor(sceneNumber(item.textureMaxBytes, 0))),
        textureReady: sceneBool(item.textureReady, false),
      });
    }) : [];
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
    const payload = JSON.stringify(value == null ? null : value);
    writeVideoSignalPayload(videoSignalName(name), payload);
  }

  function writeVideoSignalPayload(signalName, payload) {
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

  function videoBytesFromRaw(raw) {
    if (raw instanceof ArrayBuffer) {
      return new Uint8Array(raw);
    }
    if (ArrayBuffer.isView(raw)) {
      return new Uint8Array(raw.buffer, raw.byteOffset, raw.byteLength);
    }
    return null;
  }

  function videoReadU32BE(bytes, offset) {
    return ((bytes[offset] << 24) >>> 0)
      + (bytes[offset + 1] << 16)
      + (bytes[offset + 2] << 8)
      + bytes[offset + 3];
  }

  const videoFloat32Scratch = typeof ArrayBuffer === "function" && typeof Uint8Array === "function" && typeof DataView === "function"
    ? new Uint8Array(new ArrayBuffer(4))
    : null;
  const videoFloat32View = videoFloat32Scratch ? new DataView(videoFloat32Scratch.buffer) : null;

  function videoReadFloat32BE(bytes, offset) {
    if (!videoFloat32Scratch || !videoFloat32View) {
      return 0;
    }
    videoFloat32Scratch[0] = bytes[offset];
    videoFloat32Scratch[1] = bytes[offset + 1];
    videoFloat32Scratch[2] = bytes[offset + 2];
    videoFloat32Scratch[3] = bytes[offset + 3];
    return videoFloat32View.getFloat32(0, false);
  }

  function videoEncodePong(bytes) {
    if (!bytes || bytes.length < 9) {
      return null;
    }
    const payload = new Uint8Array(9);
    payload[0] = 0x04;
    for (let i = 1; i < 9; i += 1) {
      payload[i] = bytes[i];
    }
    return payload.buffer;
  }

  function videoDecodeBinarySyncMessage(raw) {
    const bytes = videoBytesFromRaw(raw);
    if (!bytes || bytes.length === 0) {
      return null;
    }
    switch (bytes[0]) {
    case 0x01:
      if (bytes.length < 16) {
        return null;
      }
      return {
        type: "sync",
        sentAtMS: videoReadU32BE(bytes, 1) * 4294967296 + videoReadU32BE(bytes, 5),
        position: videoReadFloat32BE(bytes, 9),
        playing: bytes[13] === 1,
        rate: 1,
        viewerCount: (bytes[14] << 8) + bytes[15],
      };
    case 0x04:
      if (bytes.length < 9) {
        return null;
      }
      return {
        type: "pong",
        echoedTimestamp: videoReadU32BE(bytes, 1) * 4294967296 + videoReadU32BE(bytes, 5),
      };
    case 0x05:
      return {
        type: "ping",
        payload: videoEncodePong(bytes),
      };
    default:
      return null;
    }
  }

  function videoDecodeSyncMessage(raw) {
    if (typeof raw === "string") {
      try {
        return JSON.parse(String(raw || ""));
      } catch (_error) {
        return null;
      }
    }
    const binary = videoDecodeBinarySyncMessage(raw);
    if (binary) {
      return binary;
    }
    if (raw && typeof raw.arrayBuffer === "function") {
      return raw.arrayBuffer().then(function(buffer) {
        return videoDecodeBinarySyncMessage(buffer);
      }, function() {
        return null;
      });
    }
    if (raw && typeof raw.text === "function") {
      return raw.text().then(function(text) {
        return videoDecodeSyncMessage(text);
      }, function() {
        return null;
      });
    }
    return null;
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

  function videoNowPerf() {
    return (typeof performance !== "undefined" && performance && typeof performance.now === "function")
      ? performance.now()
      : Date.now();
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
    const src = String(videoPropValue(source, ["src", "url", "uri"], "") || "").trim();
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
    for (const child of Array.from(video.childNodes || [])) {
      if (!child || child.nodeType !== 1) {
        continue;
      }
      if (child.tagName === "SOURCE") {
        hasSourceChildren = true;
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
    let maxEndMS = 0;
    for (const cue of cues) {
      maxEndMS = Math.max(maxEndMS, cue.endMS);
      cue.prefixMaxEndMS = maxEndMS;
    }
    return cues;
  }

  function videoActiveCues(cues, currentTimeSeconds) {
    const currentMS = Math.max(0, Math.round(sceneNumber(currentTimeSeconds, 0) * 1000));
    if (!Array.isArray(cues) || cues.length === 0) {
      return [];
    }
    let low = 0;
    let high = cues.length;
    while (low < high) {
      const mid = Math.floor((low + high) / 2);
      if (cues[mid].startMS <= currentMS) {
        low = mid + 1;
      } else {
        high = mid;
      }
    }
    const active = [];
    for (let index = low - 1; index >= 0; index -= 1) {
      const cue = cues[index];
      if (sceneNumber(cue.prefixMaxEndMS, cue.endMS) <= currentMS) {
        break;
      }
      if (currentMS < cue.endMS) {
        active.push({ text: cue.text });
      }
    }
    active.reverse();
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
    return videoIsAbsoluteSyncURL(source) ? source : videoHubURL(source);
  }

  function videoHubURL(path) {
    if (!path) return "";
    if (videoIsAbsoluteSyncURL(path)) {
      return path;
    }
    return videoHubOrigin() + videoNormalizeHubPath(path);
  }

  function videoIsAbsoluteSyncURL(path) {
    return path.startsWith("ws://") || path.startsWith("wss://");
  }

  function videoHubOrigin() {
    return videoHubScheme() + videoHubHost();
  }

  function videoHubScheme() {
    return window.location && window.location.protocol === "https:" ? "wss://" : "ws://";
  }

  function videoHubHost() {
    return window.location && window.location.host ? window.location.host : "";
  }

  function videoNormalizeHubPath(path) {
    return path.startsWith("/") ? path : "/" + path;
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
      lastTracksSignature: "",
      lastStatus: "",
      loadToken: 0,
      status: "idle",
    };
    let disposed = false;
    let hls = null;
    let syncSocket = null;
    let reconnectTimer = 0;
    let followTimer = 0;
    let lastLeadSendAt = 0;
    let followState = null;
    const syncBrainID = String((ctx && ctx.id) || "gosx-video-sync");
    let syncBrainActive = false;
    let syncBrainAvailable = typeof window !== "undefined" && typeof window.__gosx_video_sync_new === "function";
    let syncBrainWarned = false;
    // JS fallback drift engine — the brain-absent, parity-locked port of the
    // Go videosync engine (28-video-sync-fallback.js). Used on the default
    // "nudge" path when the WASM brain is unavailable. One instance per
    // follow session, created lazily.
    const jsBrainAvailable = typeof window !== "undefined" && typeof window.__gosx_video_sync_js_create === "function";
    let jsBrain = null;
    let pingTimer = 0;
    let lastPingSentAt = null;
    let requestedRate = Math.max(0.1, sceneNumber(readVideoSignal("rate", videoPropValue(props, ["rate"], 1)), 1));
    let lastError = "";
    let stalled = false;
    let resizeObserver = null;
    let currentSource = "";
    let interactionTimer = 0;
    let subtitleOverlay = null;
    let syncOverlay = null;
    let countdownTimer = 0;
    let syncPhase = "";
    let syncCountdown = 0;
    let cacheWaiting = false;
    let cacheProgress = 0;
    let cacheSegments = 0;
    let cacheStatus = "";
    let videoViewport = null;
    let nativeSubtitleTrack = null;
    const videoOutputPayloads = new Map();
    const videoOutputPrimitiveValues = new Map();

    function writeVideoOutputSignal(name, value) {
      if (value == null || typeof value !== "object") {
        if (videoOutputPayloads.has(name) && Object.is(videoOutputPrimitiveValues.get(name), value)) {
          return;
        }
        videoOutputPrimitiveValues.set(name, value);
      } else {
        videoOutputPrimitiveValues.delete(name);
      }
      const payload = JSON.stringify(value == null ? null : value);
      if (videoOutputPayloads.get(name) === payload) {
        return;
      }
      videoOutputPayloads.set(name, payload);
      writeVideoSignalPayload(videoSignalName(name), payload);
    }

    function setError(message) {
      lastError = String(message || "").trim();
      writeVideoOutputSignal("error", lastError);
      renderSyncOverlay();
    }

    function clearError() {
      if (!lastError) {
        return;
      }
      lastError = "";
      writeVideoOutputSignal("error", "");
      renderSyncOverlay();
    }

    function updateSubtitleOutputs() {
      const tracks = subtitleState.tracks.slice();
      const tracksSignature = JSON.stringify(tracks);
      if (tracksSignature !== subtitleState.lastTracksSignature) {
        subtitleState.lastTracksSignature = tracksSignature;
        writeVideoOutputSignal("subtitleTracks", tracks);
      }
      if (subtitleState.status !== subtitleState.lastStatus) {
        subtitleState.lastStatus = subtitleState.status;
        writeVideoOutputSignal("subtitleStatus", subtitleState.status);
      }
    }

    function updateCueOutputs() {
      const next = videoActiveCues(subtitleState.cues, sceneNumber(video.currentTime, 0));
      const signature = JSON.stringify(next);
      if (signature === subtitleState.lastSignature) {
        return;
      }
      subtitleState.lastSignature = signature;
      renderSubtitleOverlay(next);
      writeVideoOutputSignal("activeCues", next);
    }

    function ensureSubtitleOverlay() {
      if (subtitleOverlay) {
        return subtitleOverlay;
      }
      subtitleOverlay = document.createElement("div");
      subtitleOverlay.setAttribute("class", "gosx-video-subtitle-overlay subtitle-overlay");
      subtitleOverlay.setAttribute("data-gosx-video-subtitles", "true");
      subtitleOverlay.setAttribute("aria-hidden", "true");
      subtitleOverlay.setAttribute("hidden", "true");
      return subtitleOverlay;
    }

    function renderSubtitleOverlay(cues) {
      const overlay = ensureSubtitleOverlay();
      videoClearChildren(overlay);
      const active = Array.isArray(cues) ? cues : [];
      if (active.length === 0) {
        overlay.setAttribute("hidden", "true");
        return;
      }
      overlay.removeAttribute("hidden");
      for (const cue of active) {
        const node = document.createElement("div");
        node.setAttribute("class", "gosx-video-subtitle-cue subtitle-cue");
        const lines = String(cue && cue.text || "").split("\n");
        for (let i = 0; i < lines.length; i += 1) {
          if (i > 0) {
            node.appendChild(document.createElement("br"));
          }
          node.appendChild(document.createTextNode(lines[i]));
        }
        overlay.appendChild(node);
      }
    }

    function ensureSyncOverlay() {
      if (syncOverlay) {
        return syncOverlay;
      }
      syncOverlay = document.createElement("div");
      syncOverlay.setAttribute("class", "gosx-video-sync-overlay");
      syncOverlay.setAttribute("data-gosx-video-sync-overlay", "true");
      syncOverlay.setAttribute("aria-live", "polite");
      syncOverlay.setAttribute("hidden", "true");
      return syncOverlay;
    }

    function syncLockedToServer() {
      return String(videoPropValue(props, ["syncMode", "sync_mode"], "follow") || "follow").trim().toLowerCase() === "follow" &&
        String(videoPropValue(props, ["sync"], "") || "").trim() !== "";
    }

    function shouldBlockLocalPlayback() {
      if (!syncLockedToServer()) {
        return false;
      }
      if (cacheWaiting || syncPhase === "prepare" || syncPhase === "waiting") {
        return true;
      }
      return !followState || !sceneBool(followState.playing, false);
    }

    function clampVideoPercent(value) {
      return Math.max(0, Math.min(100, Math.round(sceneNumber(value, 0))));
    }

    function renderSyncOverlay() {
      const overlay = ensureSyncOverlay();
      let mode = "";
      let title = "";
      let detail = "";
      let count = "";
      const progress = clampVideoPercent(cacheProgress);
      if (lastError) {
        mode = "error";
        title = "Playback error";
        detail = lastError;
      } else if (cacheWaiting) {
        mode = "buffering";
        title = "Buffering for synced start";
        detail = cacheStatus || (progress > 0 ? "Buffering " + progress + "%" : "Buffering");
        if (cacheSegments > 0) {
          detail += " · " + cacheSegments + " segments";
        }
      } else if (syncPhase === "prepare") {
        mode = "countdown";
        title = "Starting in";
        count = syncCountdown > 0 ? String(syncCountdown) : "Sync";
        detail = "Locking to server sync";
      } else if (syncPhase === "waiting") {
        mode = "waiting";
        title = "Waiting for server sync";
        detail = "Playback will start automatically";
      } else if (stalled && !sceneBool(video.paused, true)) {
        mode = "buffering";
        title = "Buffering";
        detail = "Waiting for the stream";
      }

      videoClearChildren(overlay);
      if (!mode) {
        overlay.setAttribute("hidden", "true");
        if (mount && typeof mount.removeAttribute === "function") {
          mount.removeAttribute("data-gosx-video-overlay-state");
        }
        writeVideoOutputSignal("syncPhase", "");
        writeVideoOutputSignal("syncCountdown", 0);
        writeVideoOutputSignal("cacheWaiting", false);
        writeVideoOutputSignal("cacheProgress", progress);
        return;
      }

      overlay.removeAttribute("hidden");
      if (mount && typeof mount.setAttribute === "function") {
        mount.setAttribute("data-gosx-video-overlay-state", mode);
      }
      const panel = document.createElement("div");
      panel.setAttribute("class", "gosx-video-sync-overlay__panel");
      const titleNode = document.createElement("div");
      titleNode.setAttribute("class", "gosx-video-sync-overlay__title");
      titleNode.textContent = title;
      panel.appendChild(titleNode);
      if (count) {
        const countNode = document.createElement("div");
        countNode.setAttribute("class", "gosx-video-sync-overlay__count");
        countNode.textContent = count;
        panel.appendChild(countNode);
      }
      if (detail) {
        const detailNode = document.createElement("div");
        detailNode.setAttribute("class", "gosx-video-sync-overlay__detail");
        detailNode.textContent = detail;
        panel.appendChild(detailNode);
      }
      if (mode === "buffering" && progress > 0) {
        const meter = document.createElement("div");
        meter.setAttribute("class", "gosx-video-sync-overlay__meter");
        const bar = document.createElement("div");
        bar.setAttribute("class", "gosx-video-sync-overlay__bar");
        bar.style.width = progress + "%";
        meter.appendChild(bar);
        panel.appendChild(meter);
      }
      overlay.appendChild(panel);
      writeVideoOutputSignal("syncPhase", mode);
      writeVideoOutputSignal("syncCountdown", mode === "countdown" ? syncCountdown : 0);
      writeVideoOutputSignal("cacheWaiting", cacheWaiting);
      writeVideoOutputSignal("cacheProgress", progress);
      writeVideoOutputSignal("cacheSegments", cacheSegments);
    }

    function clearCountdownTimer() {
      if (countdownTimer) {
        clearInterval(countdownTimer);
        countdownTimer = 0;
      }
    }

    function setCacheWaiting(waiting, progress, segments, status) {
      cacheWaiting = Boolean(waiting);
      cacheProgress = clampVideoPercent(progress);
      cacheSegments = Math.max(0, Math.floor(sceneNumber(segments, 0)));
      cacheStatus = String(status || "").trim();
      if (cacheWaiting) {
        syncPhase = "waiting";
        clearCountdownTimer();
        if (!sceneBool(video.paused, true)) {
          video.pause();
        }
      } else if (syncPhase === "waiting") {
        syncPhase = "";
      }
      renderSyncOverlay();
      updateVideoOutputs();
    }

    function videoAttr(name, fallback) {
      if (!video || typeof video.getAttribute !== "function") {
        return fallback;
      }
      const value = video.getAttribute(name);
      return value == null ? fallback : value;
    }

    function videoBoolAttr(name, fallback) {
      if (!video || typeof video.hasAttribute !== "function" || !video.hasAttribute(name)) {
        return fallback;
      }
      const value = videoAttr(name, "");
      if (String(value).trim() === "") {
        return true;
      }
      return sceneBool(value, fallback);
    }

    function readInitialVideoCacheState() {
      const waiting = videoBoolAttr("data-gosx-video-cache-waiting", false);
      const progress = videoAttr("data-gosx-video-cache-progress", 0);
      const segments = videoAttr("data-gosx-video-cache-segments", 0);
      const status = videoAttr("data-gosx-video-cache-status", "");
      setCacheWaiting(waiting, progress, segments, status);
    }

    function followMessagePosition(message) {
      return Math.max(0, sceneNumber(message && (message.position != null ? message.position : message.position_seconds), 0));
    }

    function followMessageRate(message) {
      return Math.max(0.1, sceneNumber(message && (message.rate != null ? message.rate : message.playback_rate), 1));
    }

    function followMessageTimeMS(message) {
      if (!message || typeof message !== "object") {
        return Date.now();
      }
      const raw = message.sentAtMS != null ? message.sentAtMS :
        (message.sent_at_ms != null ? message.sent_at_ms :
        (message.serverTime != null ? message.serverTime :
        (message.server_time != null ? message.server_time : message.timestamp)));
      return sceneNumber(raw, Date.now());
    }

    function applyServerPosition(message) {
      const position = followMessagePosition(message);
      if (Number.isFinite(position)) {
        video.currentTime = position;
      }
      return position;
    }

    function startSyncPrepare(message) {
      const mediaID = message && (message.mediaID || message.media_id);
      if (mediaID && currentSource && String(mediaID) !== String(currentSource)) {
        return;
      }
      clearError();
      cacheWaiting = false;
      cacheProgress = 100;
      syncPhase = "prepare";
      applyServerPosition(message);
      if (!sceneBool(video.paused, true)) {
        video.pause();
      }
      const countdownMS = message && message.countdown_ms != null ? message.countdown_ms : (message && message.countdownMS);
      const startValue = message && message.start_at != null ? message.start_at : (message && message.startAt);
      const fallbackStart = Date.now() + sceneNumber(countdownMS, 3000);
      const startAt = sceneNumber(startValue, fallbackStart);
      const tick = function() {
        const remaining = Math.max(0, startAt - Date.now());
        syncCountdown = remaining > 0 ? Math.max(1, Math.ceil(remaining / 1000)) : 0;
        renderSyncOverlay();
        if (remaining <= 0) {
          clearCountdownTimer();
        }
      };
      clearCountdownTimer();
      tick();
      countdownTimer = setInterval(tick, 100);
      updateVideoOutputs();
    }

    function applySyncPlay(message) {
      const mediaID = message && (message.mediaID || message.media_id);
      if (mediaID && currentSource && String(mediaID) !== String(currentSource)) {
        return;
      }
      clearCountdownTimer();
      cacheWaiting = false;
      cacheProgress = 100;
      cacheStatus = "";
      syncPhase = "";
      followState = {
        type: "sync",
        mediaID: mediaID || currentSource,
        position: followMessagePosition(message),
        playing: true,
        rate: followMessageRate(message),
        sentAtMS: followMessageTimeMS(message),
        viewerCount: Math.max(0, Math.floor(sceneNumber(message && (message.viewerCount || message.viewer_count), 0))),
      };
      if (syncLockedToServer()) {
        ensureFollowTimer();
        applyFollowState();
      }
      renderSyncOverlay();
    }

    function applySyncPause(message) {
      const mediaID = message && (message.mediaID || message.media_id);
      if (mediaID && currentSource && String(mediaID) !== String(currentSource)) {
        return;
      }
      clearCountdownTimer();
      if (syncPhase !== "waiting") {
        syncPhase = "";
      }
      followState = {
        type: "sync",
        mediaID: mediaID || currentSource,
        position: followMessagePosition(message),
        playing: false,
        rate: followMessageRate(message),
        sentAtMS: followMessageTimeMS(message),
        viewerCount: Math.max(0, Math.floor(sceneNumber(message && (message.viewerCount || message.viewer_count), 0))),
      };
      applyServerPosition(message);
      if (!sceneBool(video.paused, true)) {
        video.pause();
      }
      renderSyncOverlay();
      updateVideoOutputs();
    }

    function applySyncSeek(message) {
      const mediaID = message && (message.mediaID || message.media_id);
      if (mediaID && currentSource && String(mediaID) !== String(currentSource)) {
        return;
      }
      applyServerPosition(message);
      if (followState) {
        followState.position = followMessagePosition(message);
        followState.sentAtMS = followMessageTimeMS(message);
      }
      updateVideoOutputs();
    }

    function applyChannelStatus(message) {
      const state = message && message.state && typeof message.state === "object" ? message.state : {};
      const waiting = sceneBool(state.cache_paused, false) || sceneBool(state.cachePaused, false) || sceneBool(state.cache_waiting, false);
      const progress = state.transcode_progress != null ? state.transcode_progress : (state.cache_progress != null ? state.cache_progress : state.cacheProgress);
      const segments = state.transcode_segments_finished != null ? state.transcode_segments_finished : (state.cache_segments != null ? state.cache_segments : state.cacheSegments);
      const status = waiting ? "Buffering " + clampVideoPercent(progress) + "%" : "";
      setCacheWaiting(waiting, progress, segments, status);
    }

    function videoIsPoppedOut() {
      return Boolean(document && document.pictureInPictureElement === video);
    }

    function setNativeSubtitleTrackMode(trackNode, mode) {
      if (!trackNode) {
        return;
      }
      const next = mode === "showing" || mode === "hidden" ? mode : "disabled";
      try {
        if (trackNode.track && typeof trackNode.track === "object") {
          trackNode.track.mode = next;
        }
      } catch (_error) {
      }
      try {
        trackNode.mode = next;
      } catch (_error) {
      }
    }

    function syncNativeSubtitleTrackMode() {
      for (const child of Array.from(video.childNodes || [])) {
        if (!child || child.nodeType !== 1 || child.tagName !== "TRACK" || child === nativeSubtitleTrack) {
          continue;
        }
        setNativeSubtitleTrackMode(child, "disabled");
      }
      if (!nativeSubtitleTrack) {
        return;
      }
      const active = subtitleState.activeID && subtitleState.loadedID === subtitleState.activeID;
      setNativeSubtitleTrackMode(nativeSubtitleTrack, active ? (videoIsPoppedOut() ? "showing" : "hidden") : "disabled");
    }

    function ensureNativeSubtitleMirror(track, subtitleURL) {
      if (!track || !subtitleURL) {
        return;
      }
      if (!nativeSubtitleTrack) {
        nativeSubtitleTrack = document.createElement("track");
        video.appendChild(nativeSubtitleTrack);
      }
      const trackNode = nativeSubtitleTrack;
      trackNode.setAttribute("src", subtitleURL);
      trackNode.setAttribute("kind", track.kind);
      if (track.srclang) {
        trackNode.setAttribute("srclang", track.srclang);
      }
      syncNativeSubtitleTrackMode();
    }

    function setInteractionState(state) {
      const next = String(state || "active").trim() === "idle" ? "idle" : "active";
      if (mount && typeof mount.setAttribute === "function") {
        mount.setAttribute("data-gosx-video-interaction", next);
      }
      if (video && typeof video.setAttribute === "function") {
        video.setAttribute("data-gosx-video-interaction", next);
      }
      writeVideoOutputSignal("interaction", next);
    }

    function clearInteractionTimer() {
      if (interactionTimer) {
        clearTimeout(interactionTimer);
        interactionTimer = 0;
      }
    }

    function scheduleInteractionIdle(delayMS) {
      clearInteractionTimer();
      if (disposed) {
        return;
      }
      interactionTimer = setTimeout(function() {
        interactionTimer = 0;
        if (!disposed && !sceneBool(video.paused, true) && !sceneBool(video.ended, false)) {
          setInteractionState("idle");
        }
      }, Math.max(250, sceneNumber(delayMS, 1800)));
    }

    function markInteractionActive(delayMS) {
      setInteractionState("active");
      if (!sceneBool(video.paused, true) && !sceneBool(video.ended, false)) {
        scheduleInteractionIdle(delayMS);
      } else {
        clearInteractionTimer();
      }
    }

    function refreshVideoViewportOutput() {
      const next = videoViewportSize(mount);
      if (!videoViewport || videoViewport[0] !== next[0] || videoViewport[1] !== next[1]) {
        videoViewport = next;
      }
      writeVideoOutputSignal("viewport", videoViewport);
    }

    function configuredVideoDuration() {
      const propDuration = sceneNumber(videoPropValue(props, ["duration", "durationSeconds", "duration_seconds"], 0), 0);
      if (propDuration > 0) {
        return propDuration;
      }
      if (!video || typeof video.getAttribute !== "function") {
        return 0;
      }
      for (const name of ["data-gosx-video-duration", "data-duration-seconds", "data-duration"]) {
        const attrDuration = sceneNumber(video.getAttribute(name), 0);
        if (attrDuration > 0) {
          return attrDuration;
        }
      }
      return 0;
    }

    function videoOutputDuration() {
      const mediaDuration = Math.max(0, sceneNumber(video.duration, 0));
      const configuredDuration = configuredVideoDuration();
      if (configuredDuration > 0 && (mediaDuration <= 0 || mediaDuration + 1 < configuredDuration)) {
        return configuredDuration;
      }
      return mediaDuration;
    }

    function updateVideoOutputs() {
      const duration = videoOutputDuration();
      const playing = !sceneBool(video.paused, true) && !sceneBool(video.ended, false);
      writeVideoOutputSignal("position", Math.max(0, sceneNumber(video.currentTime, 0)));
      writeVideoOutputSignal("duration", duration);
      writeVideoOutputSignal("playing", playing);
      writeVideoOutputSignal("buffered", videoBufferedAhead(video));
      writeVideoOutputSignal("stalled", stalled);
      writeVideoOutputSignal("fullscreen", Boolean(document && document.fullscreenElement && (document.fullscreenElement === mount || document.fullscreenElement === video)));
      writeVideoOutputSignal("ready", sceneNumber(video.readyState, 0) >= 2);
      writeVideoOutputSignal("muted", Boolean(video.muted));
      writeVideoOutputSignal("actualRate", sceneNumber(video.playbackRate, requestedRate));
      writeVideoOutputSignal("syncConnected", Boolean(syncSocket && syncSocket.readyState === 1));
      writeVideoOutputSignal("viewerCount", Math.max(0, Math.floor(sceneNumber(followState && followState.viewerCount, 0))));
      updateCueOutputs();
      if (!lastError) {
        writeVideoOutputSignal("error", "");
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
      const drift = Math.max(-9999, Math.min(9999, sceneNumber(video.currentTime, 0) - target));
      if (playing) {
        cacheWaiting = false;
        if (syncPhase === "prepare" || syncPhase === "waiting") {
          syncPhase = "";
        }
        clearCountdownTimer();
        if (sceneBool(video.paused, true) || sceneBool(video.ended, false)) {
          safePlay();
        }
      } else if (!sceneBool(video.paused, true)) {
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
      renderSyncOverlay();
      updateVideoOutputs();
    }

    function nowPerf() {
      return videoNowPerf();
    }

    function syncTuning() {
      const tuning = props && props.syncTuning && typeof props.syncTuning === "object" ? props.syncTuning : {};
      return tuning;
    }

    function syncStrategyName() {
      return String(videoPropValue(props, ["syncStrategy", "sync_strategy"], "nudge") || "nudge").trim().toLowerCase();
    }

    // The WASM drift-correction brain only drives the default "nudge" path.
    // "nudge-legacy" and "snap" keep the existing JS behavior. A brain throw
    // disables the brain for the rest of the session (hot-swap, no re-probe).
    function useSyncBrain() {
      if (!syncBrainAvailable) {
        return false;
      }
      const strategy = syncStrategyName();
      return strategy === "nudge" || strategy === "";
    }

    function disableSyncBrain(error) {
      const wasActive = syncBrainActive;
      syncBrainAvailable = false;
      syncBrainActive = false;
      if (!syncBrainWarned) {
        syncBrainWarned = true;
        if (typeof window !== "undefined" && typeof window.__gosx_emit === "function") {
          try {
            window.__gosx_emit("warn", "video-sync", "video sync brain failed; falling back to legacy follow path", {
              engineID: syncBrainID,
              message: error && error.message ? String(error.message) : String(error || ""),
            });
          } catch (_emitError) {
          }
        }
      }
      // Hot-swap to the legacy path: drop the brain tick/ping intervals and
      // re-arm the 500ms applyFollowState loop for the rest of the session.
      if (wasActive && !disposed) {
        clearFollowTimer();
        ensureFollowTimer();
      }
    }

    function ensureSyncBrain() {
      if (syncBrainActive || !useSyncBrain()) {
        return syncBrainActive;
      }
      try {
        let cfg = "";
        try {
          cfg = JSON.stringify(syncTuning() || {});
        } catch (_cfgError) {
          cfg = "";
        }
        window.__gosx_video_sync_new(syncBrainID, cfg);
        syncBrainActive = true;
      } catch (error) {
        disableSyncBrain(error);
        return false;
      }
      return syncBrainActive;
    }

    function ingestSyncBrain(message) {
      if (!syncBrainActive) {
        return;
      }
      try {
        window.__gosx_video_sync_ingest(
          syncBrainID,
          followMessageTimeMS(message),
          sceneNumber(message && message.position, 0),
          sceneBool(message && message.playing, false),
          nowPerf()
        );
      } catch (error) {
        disableSyncBrain(error);
      }
    }

    function syncBrainPlaybackStart() {
      if (!syncBrainActive) {
        return;
      }
      try {
        window.__gosx_video_sync_playback_start(syncBrainID, nowPerf());
      } catch (error) {
        disableSyncBrain(error);
      }
    }

    function syncBrainBufferedAhead() {
      if (!video || !video.buffered || typeof video.buffered.length !== "number" || typeof video.buffered.end !== "function") {
        return 1e9;
      }
      return videoBufferedAhead(video);
    }

    // Publishes preload/readiness signals via the same feature-detected
    // shared-signal path the factory already uses.
    function publishSyncBrainSignals(actualRate, ready, stalledFlag) {
      writeVideoSignalPayload(videoSignalName("actualRate"), JSON.stringify(sceneNumber(actualRate, requestedRate)));
      writeVideoSignalPayload(videoSignalName("ready"), JSON.stringify(Boolean(ready)));
      writeVideoSignalPayload(videoSignalName("stalled"), JSON.stringify(Boolean(stalledFlag)));
    }

    // Shared actuation for both the WASM brain and the JS fallback engine.
    // Both produce the same Decision shape; this applies it to the <video>
    // element and publishes the preload/readiness signals.
    //   kind: 0=none, 1=rate, 2=seek. resetRate forces nominal 1.0x.
    function actuateSyncDecision(kind, rate, seekTo, ready, stalledFlag, actualRate, resetRate) {
      if (kind === 2) {
        if (Number.isFinite(seekTo)) {
          video.currentTime = Math.max(0, seekTo);
        }
      } else if (kind === 1) {
        if (Number.isFinite(rate) && rate > 0) {
          video.playbackRate = rate;
        }
      }
      if (resetRate || kind === 0) {
        video.playbackRate = 1.0;
      }
      stalled = stalledFlag;
      publishSyncBrainSignals(actualRate, ready, stalledFlag);
      renderSyncOverlay();
      updateVideoOutputs();
    }

    // Decision array layout from the WASM brain:
    //   [kind, rate, seekTo, ready, stalled, actualRate, preloadPhase, resetRate]
    // kind: 0=none, 1=rate, 2=seek. ready/stalled/resetRate: 1/0.
    function applySyncBrainTick() {
      if (disposed || !syncBrainActive) {
        return;
      }
      let decision = null;
      try {
        decision = window.__gosx_video_sync_tick(
          syncBrainID,
          Math.max(0, sceneNumber(video.currentTime, 0)),
          nowPerf(),
          syncBrainBufferedAhead(),
          sceneBool(video.paused, true)
        );
      } catch (error) {
        // disableSyncBrain hot-swaps to the legacy 500ms applyFollowState path.
        disableSyncBrain(error);
        return;
      }
      if (!decision || typeof decision.length !== "number") {
        return;
      }
      const kind = sceneNumber(decision[0], 0);
      const rate = sceneNumber(decision[1], 1);
      const seekTo = sceneNumber(decision[2], 0);
      const ready = sceneNumber(decision[3], 0) === 1;
      const stalledFlag = sceneNumber(decision[4], 0) === 1;
      const actualRate = sceneNumber(decision[5], video.playbackRate);
      const resetRate = sceneNumber(decision[7], 0) === 1;
      actuateSyncDecision(kind, rate, seekTo, ready, stalledFlag, actualRate, resetRate);
    }

    // -------------------------------------------------------------------------
    // JS fallback drift engine (brain-absent "nudge" path).
    // Mirrors the WASM brain's ingest/tick/rtt/playback-start surface, but the
    // engine itself is the pure-JS port installed by 28-video-sync-fallback.js
    // (parity-locked to the Go videosync engine). Time is injected via nowPerf().
    // -------------------------------------------------------------------------
    function useJSBrain() {
      if (!jsBrainAvailable) {
        return false;
      }
      const strategy = syncStrategyName();
      return strategy === "nudge" || strategy === "";
    }

    function ensureJSBrain() {
      if (jsBrain) {
        return true;
      }
      if (!useJSBrain()) {
        return false;
      }
      try {
        jsBrain = window.__gosx_video_sync_js_create(syncTuning() || {});
      } catch (_error) {
        jsBrain = null;
        return false;
      }
      return !!jsBrain;
    }

    function ingestJSBrain(message) {
      if (!jsBrain) {
        return;
      }
      jsBrain.ingest(
        followMessageTimeMS(message),
        sceneNumber(message && message.position, 0),
        sceneBool(message && message.playing, false),
        nowPerf()
      );
    }

    function jsBrainPlaybackStart() {
      if (!jsBrain) {
        return;
      }
      jsBrain.onPlaybackStart(nowPerf());
    }

    function jsBrainRTT(rttMs) {
      if (!jsBrain) {
        return;
      }
      jsBrain.rtt(rttMs);
    }

    function applyJSBrainTick() {
      if (disposed || !jsBrain) {
        return;
      }
      const decision = jsBrain.tick(
        Math.max(0, sceneNumber(video.currentTime, 0)),
        nowPerf(),
        syncBrainBufferedAhead(),
        sceneBool(video.paused, true)
      );
      if (!decision || typeof decision !== "object") {
        return;
      }
      const kind = sceneNumber(decision.kind, 0);
      const rate = sceneNumber(decision.rate, 1);
      const seekTo = sceneNumber(decision.seekTo, 0);
      const ready = sceneBool(decision.ready, false);
      const stalledFlag = sceneBool(decision.stalled, false);
      const actualRate = sceneNumber(decision.actualRate, video.playbackRate);
      const resetRate = sceneBool(decision.resetRate, false);
      actuateSyncDecision(kind, rate, seekTo, ready, stalledFlag, actualRate, resetRate);
    }

    function syncBrainTickInterval() {
      return Math.max(50, sceneNumber(syncTuning().monitorIntervalMs, 1200));
    }

    function syncBrainPingInterval() {
      return Math.max(1000, sceneNumber(syncTuning().pingIntervalMs, 15000));
    }

    function sendSyncBrainPing() {
      if (disposed || !syncSocket || syncSocket.readyState !== 1) {
        return;
      }
      // Exactly one client ping outstanding at a time.
      if (lastPingSentAt != null) {
        return;
      }
      const now = nowPerf();
      const frame = new Uint8Array(9);
      frame[0] = 0x05;
      try {
        syncSocket.send(frame.buffer);
        lastPingSentAt = now;
      } catch (_error) {
      }
    }

    function clearPingTimer() {
      if (pingTimer) {
        clearInterval(pingTimer);
        pingTimer = 0;
      }
    }

    function ensurePingTimer() {
      if (pingTimer) {
        return;
      }
      pingTimer = setInterval(sendSyncBrainPing, syncBrainPingInterval());
      sendSyncBrainPing();
    }

    function clearFollowTimer() {
      if (followTimer) {
        clearInterval(followTimer);
        followTimer = 0;
      }
      clearPingTimer();
    }

    function ensureFollowTimer() {
      if (String(videoPropValue(props, ["syncMode", "sync_mode"], "follow")).trim().toLowerCase() !== "follow") {
        return;
      }
      if (ensureSyncBrain()) {
        if (!followTimer) {
          followTimer = setInterval(applySyncBrainTick, syncBrainTickInterval());
        }
        ensurePingTimer();
        return;
      }
      // Brain absent: on the default "nudge" path, use the parity-locked JS
      // fallback engine. "nudge-legacy"/"snap" fall through to applyFollowState.
      if (ensureJSBrain()) {
        if (!followTimer) {
          followTimer = setInterval(applyJSBrainTick, syncBrainTickInterval());
        }
        ensurePingTimer();
        return;
      }
      if (followTimer) {
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
      lastPingSentAt = null;
      if (syncSocket && typeof syncSocket.close === "function") {
        syncSocket.close();
      }
      syncSocket = null;
      writeVideoOutputSignal("syncConnected", false);
    }

    function dispatchSyncMessage(message) {
      if (!message || disposed) {
        return;
      }
      const type = String(message.type || "").trim();
      if (type === "ping") {
        if (message.payload && syncSocket && syncSocket.readyState === 1) {
          try {
            syncSocket.send(message.payload);
          } catch (_error) {
          }
        }
        return;
      }
      if (type === "pong") {
        // Client-originated RTT sample. Ignore unsolicited pongs (lastPingSentAt null).
        if (lastPingSentAt != null) {
          const rttMs = nowPerf() - lastPingSentAt;
          lastPingSentAt = null;
          if (syncBrainActive) {
            try {
              window.__gosx_video_sync_rtt(syncBrainID, rttMs);
            } catch (error) {
              disableSyncBrain(error);
            }
          } else if (jsBrain) {
            jsBrainRTT(rttMs);
          }
        }
        return;
      }
      if (type === "channel_status") {
        applyChannelStatus(message);
        return;
      }
      if (type === "sync_prepare") {
        startSyncPrepare(message);
        return;
      }
      if (type === "sync_play") {
        applySyncPlay(message);
        return;
      }
      if (type === "pause") {
        applySyncPause(message);
        return;
      }
      if (type === "seek") {
        applySyncSeek(message);
        return;
      }
      if (type !== "sync") {
        return;
      }
      const mediaID = message.mediaID || message.media_id;
      if (mediaID && currentSource && String(mediaID) !== String(currentSource)) {
        return;
      }
      followState = message;
      if (String(videoPropValue(props, ["syncMode", "sync_mode"], "follow")).trim().toLowerCase() === "follow") {
        ensureFollowTimer();
        if (syncBrainActive) {
          ingestSyncBrain(message);
          applySyncBrainTick();
        } else if (jsBrain) {
          ingestJSBrain(message);
          applyJSBrainTick();
        } else {
          applyFollowState();
        }
      }
    }

    function connectSync(attempt) {
      const rawURL = videoSyncURL(videoPropValue(props, ["sync"], ""));
      if (!rawURL || typeof WebSocket !== "function" || disposed) {
        writeVideoOutputSignal("syncConnected", false);
        return;
      }
      const retryAttempt = Math.max(0, attempt || 0);
      const socket = new WebSocket(rawURL);
      try {
        socket.binaryType = "arraybuffer";
      } catch (_error) {
      }
      syncSocket = socket;
      socket.onopen = function() {
        writeVideoOutputSignal("syncConnected", true);
        updateVideoOutputs();
        if (String(videoPropValue(props, ["syncMode", "sync_mode"], "follow")).trim().toLowerCase() === "lead") {
          sendLeadSnapshot(true);
        } else {
          ensureFollowTimer();
        }
      };
      socket.onclose = function() {
        writeVideoOutputSignal("syncConnected", false);
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
        writeVideoOutputSignal("syncConnected", false);
      };
      socket.onmessage = function(event) {
        const decoded = videoDecodeSyncMessage(event && event.data);
        if (!decoded) {
          return;
        }
        if (decoded && typeof decoded.then === "function") {
          decoded.then(dispatchSyncMessage);
          return;
        }
        dispatchSyncMessage(decoded);
      };
    }

    function subtitleRetryDelayMS(response, fallbackMS) {
      const fallback = Math.max(500, Math.min(10000, sceneNumber(fallbackMS, 1500)));
      const headers = response && response.headers;
      if (!headers || typeof headers.get !== "function") {
        return fallback;
      }
      const raw = String(headers.get("Retry-After") || "").trim();
      if (!raw) {
        return fallback;
      }
      const seconds = Number(raw);
      if (Number.isFinite(seconds) && seconds >= 0) {
        return Math.max(500, Math.min(10000, seconds * 1000));
      }
      const dateMS = Date.parse(raw);
      if (Number.isFinite(dateMS)) {
        return Math.max(500, Math.min(10000, dateMS - Date.now()));
      }
      return fallback;
    }

    function waitMS(delayMS) {
      return new Promise(function(resolve) {
        setTimeout(resolve, Math.max(0, sceneNumber(delayMS, 0)));
      });
    }

    async function loadSubtitleTrack(trackID) {
      const selected = String(trackID || "").trim();
      const loadToken = subtitleState.loadToken + 1;
      subtitleState.loadToken = loadToken;
      subtitleState.activeID = selected;
      subtitleState.cues = [];
      subtitleState.lastSignature = "";
      syncNativeSubtitleTrackMode();
      const isCurrentLoad = function() {
        return !disposed && subtitleState.loadToken === loadToken && subtitleState.activeID === selected;
      };
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
        if (!isCurrentLoad()) {
          return;
        }
        let response = null;
        try {
          response = await fetch(subtitleURL);
        } catch (error) {
          if (!isCurrentLoad()) {
            return;
          }
          if (attempt < 2) {
            subtitleState.status = "warming";
            updateSubtitleOutputs();
            await waitMS(750 * (attempt + 1));
            continue;
          }
          subtitleState.status = "error";
          setError(error && error.message ? error.message : "subtitle fetch failed");
          updateSubtitleOutputs();
          return;
        }
        if (!isCurrentLoad()) {
          return;
        }
        if (response.status === 202) {
          subtitleState.status = "warming";
          updateSubtitleOutputs();
          await waitMS(subtitleRetryDelayMS(response, 1500));
          continue;
        }
        if (!response.ok) {
          subtitleState.status = "error";
          setError("subtitle fetch failed");
          updateSubtitleOutputs();
          return;
        }
        const text = await response.text();
        if (!isCurrentLoad()) {
          return;
        }
        subtitleState.cues = parseVideoVTT(text);
        subtitleState.loadedID = selected;
        subtitleState.status = "ready";
        clearError();
        ensureNativeSubtitleMirror(localTrack, subtitleURL);
        updateSubtitleOutputs();
        updateCueOutputs();
        return;
      }
      subtitleState.status = "error";
      setError("subtitle warmup timed out");
      updateSubtitleOutputs();
    }

    function startSubtitleLoad(trackID) {
      loadSubtitleTrack(trackID).catch(function(error) {
        if (disposed) {
          return;
        }
        subtitleState.status = "error";
        setError(error && error.message ? error.message : "subtitle load failed");
        updateSubtitleOutputs();
      });
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
      startSubtitleLoad(activeSubtitleTrack);
      if (String(videoPropValue(props, ["sync"], "")).trim() !== "") {
        closeSyncSocket();
        connectSync(0);
      }
    }

    video.setAttribute("data-gosx-video", "true");
    setInteractionState("active");
    videoApplyElementProps(video, props);
    videoEnsureAuthoredChildren(video, props);
    syncNativeSubtitleTrackMode();
    videoClearChildren(mount);
    mount.appendChild(video);
    mount.appendChild(ensureSubtitleOverlay());
    mount.appendChild(ensureSyncOverlay());
    subtitleState.status = subtitleState.tracks.length > 0 ? "ready" : "idle";
    readInitialVideoCacheState();
    updateSubtitleOutputs();
    writeVideoOutputSignal("activeCues", []);
    writeVideoOutputSignal("syncConnected", false);
    writeVideoOutputSignal("error", "");

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
      if (shouldBlockLocalPlayback()) {
        if (!cacheWaiting && syncPhase !== "prepare") {
          syncPhase = "waiting";
        }
        video.pause();
        stalled = false;
        markInteractionActive(0);
        renderSyncOverlay();
        updateVideoOutputs();
        return;
      }
      stalled = false;
      clearError();
      markInteractionActive(1800);
      syncBrainPlaybackStart();
      jsBrainPlaybackStart();
      updateVideoOutputs();
      sendLeadSnapshot(true);
    });
    addListener(video, "pause", function() {
      stalled = false;
      markInteractionActive(0);
      updateVideoOutputs();
      sendLeadSnapshot(true);
    });
    addListener(video, "seeked", function() {
      updateVideoOutputs();
      sendLeadSnapshot(true);
    });
    addListener(video, "waiting", function() {
      stalled = true;
      markInteractionActive(0);
      renderSyncOverlay();
      updateVideoOutputs();
    });
    addListener(video, "stalled", function() {
      stalled = true;
      markInteractionActive(0);
      renderSyncOverlay();
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
      markInteractionActive(0);
      updateVideoOutputs();
    });
    addListener(mount, "pointerenter", function() {
      markInteractionActive(2400);
    });
    addListener(mount, "pointermove", function() {
      markInteractionActive(2400);
    });
    addListener(mount, "pointerleave", function() {
      scheduleInteractionIdle(450);
    });
    addListener(mount, "focusin", function() {
      markInteractionActive(0);
    });
    addListener(mount, "focusout", function() {
      scheduleInteractionIdle(900);
    });
    addListener(document, "fullscreenchange", function() {
      refreshVideoViewportOutput();
      updateVideoOutputs();
    });
    addListener(video, "enterpictureinpicture", syncNativeSubtitleTrackMode);
    addListener(video, "leavepictureinpicture", syncNativeSubtitleTrackMode);

    if (typeof ResizeObserver === "function") {
      resizeObserver = new ResizeObserver(function() {
        refreshVideoViewportOutput();
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
      startSubtitleLoad(value);
    }));

    const initialVolume = Math.max(0, Math.min(1, sceneNumber(readVideoSignal("volume", videoPropValue(props, ["volume"], 1)), 1)));
    video.volume = initialVolume;
    video.muted = sceneBool(readVideoSignal("mute", videoPropValue(props, ["muted"], false)), false);
    video.playbackRate = requestedRate;
    refreshVideoViewportOutput();
    updateVideoOutputs();

    const initialSource = readVideoSignal("src", videoPropValue(props, ["src", "Src"], ""));
    await applySource(initialSource);

    return {
      video,
      dispose() {
        disposed = true;
        clearInteractionTimer();
        clearCountdownTimer();
        closeSyncSocket();
        // Dispose the brain AFTER intervals are cleared and `disposed` is set,
        // so no correction tick can race the dispose.
        if (syncBrainActive) {
          syncBrainActive = false;
          try {
            window.__gosx_video_sync_dispose(syncBrainID);
          } catch (_error) {
          }
        }
        // Release the JS fallback engine (no native handle to free; just drop
        // the reference so no post-dispose tick can touch it).
        jsBrain = null;
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
    await prepareRuntimeCapabilityProbe(entry);
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
      spectator: Boolean(input.spectator),
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
    let lastPhaseCue = "";
    const lastFightAudioState = {
      initialized: false,
      p1Beast: false,
      p2Beast: false,
      p1Ready: false,
      p2Ready: false,
    };

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
      unlockArcadeAudio();

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
      unlockArcadeAudio();
      activePointers.set(event.pointerId, key);
      updateTouch(key, true);
      if (control.setPointerCapture && event.pointerId != null) {
        try {
          control.setPointerCapture(event.pointerId);
        } catch (_e) {
          // Pointer capture is best effort.
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

    function fightAudioFallbackCue(kind, event) {
      const cueKind = String(kind || "").trim().toLowerCase();
      if (!cueKind || cueKind === "none") return "none";
      if (cueKind === "block") return "block";
      if (cueKind === "just_guard") return "just_guard";
      if (cueKind === "guard_cancel") return "guard_cancel";
      if (cueKind === "armor") return "armor";
      if (cueKind === "throw_tech") return "throw_tech";
      if (cueKind === "throw") return "throw";
      if (cueKind === "hit") {
        if (event && event.punish) return "punish";
        if (event && event.counter) return "counter";
        if (event && event.launcher) return "launcher";
        const damage = Math.max(0, hubInputNumber(event && event.damage, 0));
        const move = Math.max(0, Math.floor(hubInputNumber(event && event.moveId, 0)));
        if (damage >= 95 || move === 1 || move === 3) return "hit_heavy";
        return "hit_light";
      }
      return cueKind;
    }

    function fightAudioPlayer(data, player) {
      if (Number(player) === 2) return data && data.p2;
      if (Number(player) === 1) return data && data.p1;
      return null;
    }

    function fightAudioPositionValue(player, field, fallback) {
      if (!player || typeof player !== "object") return fallback;
      if (field === "x" && player.hurtbox && Object.prototype.hasOwnProperty.call(player.hurtbox, "x")) {
        return hubInputNumber(player.hurtbox.x, fallback);
      }
      if (Object.prototype.hasOwnProperty.call(player, field)) {
        return hubInputNumber(player[field], fallback);
      }
      return fallback;
    }

    function fightAudioPan(data, event, cue) {
      if (cue && Object.prototype.hasOwnProperty.call(cue, "pan")) {
        return arcadeClamp(cue.pan, -0.95, 0.95, 0);
      }
      const attacker = fightAudioPlayer(data, event && event.attacker);
      const defender = fightAudioPlayer(data, event && event.defender);
      if (attacker && defender) {
        const x = (fightAudioPositionValue(attacker, "x", 0) + fightAudioPositionValue(defender, "x", 0)) * 0.5;
        return arcadeClamp(x / 3.4, -0.85, 0.85, 0);
      }
      if (attacker) return arcadeClamp(fightAudioPositionValue(attacker, "x", 0) / 3.4, -0.85, 0.85, 0);
      if (defender) return arcadeClamp(fightAudioPositionValue(defender, "x", 0) / 3.4, -0.85, 0.85, 0);
      return 0;
    }

    function fightAudioDepth(data, event, cue) {
      if (cue && Object.prototype.hasOwnProperty.call(cue, "depth")) {
        return arcadeClamp(cue.depth, -0.75, 0.75, 0);
      }
      const attacker = fightAudioPlayer(data, event && event.attacker);
      const defender = fightAudioPlayer(data, event && event.defender);
      if (attacker && defender) {
        return arcadeClamp((fightAudioPositionValue(attacker, "z", 0) + fightAudioPositionValue(defender, "z", 0)) * 0.5, -0.75, 0.75, 0);
      }
      if (attacker) return arcadeClamp(fightAudioPositionValue(attacker, "z", 0), -0.75, 0.75, 0);
      if (defender) return arcadeClamp(fightAudioPositionValue(defender, "z", 0), -0.75, 0.75, 0);
      return 0;
    }

    function fightAudioIntensity(kind, event, cue) {
      if (cue && Object.prototype.hasOwnProperty.call(cue, "intensity")) {
        return arcadeClamp(cue.intensity, 0.05, 1.25, 0.3);
      }
      const damage = Math.max(0, hubInputNumber(event && event.damage, 0));
      const blocked = Boolean(event && event.blocked) || kind === "block";
      const special = Boolean(event && (event.counter || event.punish || event.launcher || event.guardCancel || event.justGuard || event.armor))
        || kind === "throw" || kind === "throw_tech";
      let intensity = blocked ? 0.18 : Math.min(0.85, 0.24 + damage / 260);
      if (special) intensity = Math.min(1, intensity + 0.22);
      return intensity;
    }

    function inferFightPhaseCue(data) {
      if (!data || typeof data !== "object") return "";
      if (data.matchOver) return "match";
      const phase = String(data.phase || "").trim().toLowerCase();
      if (phase === "countdown") return "round";
      if (phase === "fight") return "fight";
      if (phase === "ko") return "ko";
      if (phase === "roundend") return "roundend";
      return "";
    }

    function playFightPhaseAudio(data) {
      const cue = data && data.audio && typeof data.audio === "object" ? data.audio : {};
      const phaseCue = String(cue.phaseCue || inferFightPhaseCue(data)).trim().toLowerCase();
      if (!phaseCue || phaseCue === "none") return;
      const key = [data && data.round, data && data.phase, data && data.matchOver, data && data.winner, phaseCue].join(":");
      if (key === lastPhaseCue) return;
      lastPhaseCue = key;
      playArcadeSFX(phaseCue, {
        intensity: phaseCue === "fight" ? 0.62 : 0.55,
        pan: 0,
        depth: 0,
      });
    }

    function playFightStateAudio(data) {
      const p1 = data && data.p1 || {};
      const p2 = data && data.p2 || {};
      const next = {
        p1Beast: Boolean(p1.beastActive),
        p2Beast: Boolean(p2.beastActive),
        p1Ready: hubInputNumber(p1.beast, 0) >= 100,
        p2Ready: hubInputNumber(p2.beast, 0) >= 100,
      };
      if (!lastFightAudioState.initialized) {
        Object.assign(lastFightAudioState, next, { initialized: true });
        return;
      }
      if (next.p1Beast && !lastFightAudioState.p1Beast) playArcadeSFX("surge", { intensity: 0.86, pan: -0.42 });
      if (next.p2Beast && !lastFightAudioState.p2Beast) playArcadeSFX("surge", { intensity: 0.86, pan: 0.42 });
      if (next.p1Ready && !lastFightAudioState.p1Ready) playArcadeSFX("surge_ready", { intensity: 0.55, pan: -0.36 });
      if (next.p2Ready && !lastFightAudioState.p2Ready) playArcadeSFX("surge_ready", { intensity: 0.55, pan: 0.36 });
      Object.assign(lastFightAudioState, next);
    }

    function publishCue(pads) {
      const connected = pads.length > 0;
      const cue = {
        connected: connected,
        active: true,
        pads: pads.length,
        padCount: pads.length,
        player: config.player,
        state: config.spectator ? "ready" : (connected ? "pad" : "touch"),
        title: config.spectator ? "CPU DUEL" : (connected ? "GAMEPAD LINKED" : "GRAB A GAMEPAD"),
        copy: config.spectator ? "Bots are driving both fighters." : (connected ? "Pad mapped: A/B/X/Y, shoulders guard." : "Keyboard and touch are live until a pad is connected."),
        mode: config.spectator ? "SPECTATE" : (config.local ? "LOCAL VS" : "ONLINE"),
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
      playFightPhaseAudio(data);
      playFightStateAudio(data);
      const cue = data.audio && typeof data.audio === "object" ? data.audio : {};
      const seq = Math.floor(hubInputNumber(cue.seq, hubInputNumber(event.seq, 0)));
      if (!seq || seq === lastFeedbackSeq) return;
      lastFeedbackSeq = seq;
      const kind = String(event.kind || "");
      if (!kind || kind === "none") return;

      let feedback = String(cue.cue || "").trim().toLowerCase();
      if (!feedback || feedback === "none") feedback = fightAudioFallbackCue(kind, event);
      if (!feedback || feedback === "none") return;
      const intensity = fightAudioIntensity(kind, event, cue);
      const special = Boolean(event.counter || event.punish || event.launcher || event.guardCancel || event.justGuard || event.armor)
        || kind === "throw" || kind === "throw_tech" || feedback === "counter" || feedback === "punish" || feedback === "launcher";
      playArcadeSFX(feedback, {
        intensity: intensity,
        pan: fightAudioPan(data, event, cue),
        depth: fightAudioDepth(data, event, cue),
      });
      vibrateGamepads(feedback, intensity, special ? 130 : 75);
    }

    function pump() {
      if (disposed) return;
      sendReady();
      const pads = gamepads();
      publishCue(pads);
      if (socketOpen() && !config.spectator) {
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
    if (kind === "block" || kind === "guard" || kind === "just_guard" || kind === "guard_cancel") weak = Math.min(0.8, strong * 0.85);
    if (kind === "armor" || kind === "throw" || kind === "throw_tech") weak = Math.min(0.7, strong * 0.55);
    if (kind === "hit_heavy" || kind === "counter" || kind === "punish" || kind === "launcher" || kind === "ko") weak = Math.min(0.9, strong * 0.62);
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
          // Optional haptics should never affect input delivery.
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

  const arcadeAudioState = {
    context: null,
    active: [],
    master: null,
    compressor: null,
    voiceLimit: 28,
  };

  function arcadeAudioContext() {
    const Ctor = window.AudioContext || window.webkitAudioContext;
    if (!Ctor) return null;
    if (!arcadeAudioState.context) {
      try {
        arcadeAudioState.context = new Ctor();
        arcadeConfigureOutput(arcadeAudioState.context);
      } catch (_e) {
        arcadeAudioState.context = null;
      }
    }
    return arcadeAudioState.context;
  }

  function arcadeConfigureOutput(audio) {
    if (!audio || arcadeAudioState.master) return;
    const destination = audio.destination;
    if (!destination || typeof audio.createGain !== "function") return;
    const master = audio.createGain();
    master.gain.value = 0.82;
    let tail = master;
    if (typeof audio.createDynamicsCompressor === "function") {
      const compressor = audio.createDynamicsCompressor();
      if (compressor.threshold) compressor.threshold.value = -18;
      if (compressor.knee) compressor.knee.value = 18;
      if (compressor.ratio) compressor.ratio.value = 4;
      if (compressor.attack) compressor.attack.value = 0.003;
      if (compressor.release) compressor.release.value = 0.12;
      master.connect(compressor);
      tail = compressor;
      arcadeAudioState.compressor = compressor;
    }
    tail.connect(destination);
    arcadeAudioState.master = master;
  }

  function arcadeOutput(audio) {
    arcadeConfigureOutput(audio);
    return arcadeAudioState.master || audio.destination;
  }

  function unlockArcadeAudio() {
    const audio = arcadeAudioContext();
    if (!audio || typeof audio.createOscillator !== "function" || typeof audio.createGain !== "function") return;
    if (typeof audio.resume === "function") audio.resume();
    return audio;
  }

  function arcadeClamp(value, min, max, fallback) {
    return Math.max(min, Math.min(max, hubInputNumber(value, fallback)));
  }

  function arcadeSoundOptions(options) {
    if (typeof options === "number") {
      return { delayMS: Math.max(0, hubInputNumber(options, 0)), intensity: 1, pan: 0, depth: 0 };
    }
    const raw = options && typeof options === "object" ? options : {};
    return {
      delayMS: Math.max(0, hubInputNumber(raw.delayMS, 0)),
      intensity: arcadeClamp(raw.intensity, 0.05, 1.35, 1),
      pan: arcadeClamp(raw.pan, -0.95, 0.95, 0),
      depth: arcadeClamp(raw.depth, -0.75, 0.75, 0),
      rate: arcadeClamp(raw.rate, 0.25, 2, 1),
    };
  }

  function playArcadeSFX(kind, options) {
    const audio = unlockArcadeAudio();
    if (!audio) return;
    const cue = String(kind || "move").trim().toLowerCase();
    const opts = arcadeSoundOptions(options);
    const heavy = Math.max(0.65, opts.intensity);
    if (cue === "confirm") {
      arcadeTone(audio, 220, 0.055, 0.08, "square", opts);
      arcadeTone(audio, 880, 0.09, 0.08, "square", Object.assign({}, opts, { delayMS: opts.delayMS + 18 }));
      return;
    }
    if (cue === "round") {
      arcadeTone(audio, 196, 0.12, 0.075, "square", opts);
      arcadeTone(audio, 294, 0.12, 0.055, "triangle", Object.assign({}, opts, { delayMS: opts.delayMS + 46 }));
      arcadeTone(audio, 392, 0.16, 0.05, "square", Object.assign({}, opts, { delayMS: opts.delayMS + 92 }));
      return;
    }
    if (cue === "fight") {
      arcadeTone(audio, 330, 0.06, 0.075, "square", opts);
      arcadeTone(audio, 660, 0.075, 0.075, "square", Object.assign({}, opts, { delayMS: opts.delayMS + 42 }));
      arcadeNoise(audio, 0.055, 0.04, "highpass", 1500, Object.assign({}, opts, { delayMS: opts.delayMS + 22 }));
      return;
    }
    if (cue === "ko" || cue === "match") {
      arcadeNoise(audio, 0.16, 0.095, "lowpass", 720, opts);
      arcadeSweep(audio, 190, 62, 0.32, 0.07, "sawtooth", opts);
      arcadeTone(audio, 82, 0.18, 0.08, "square", Object.assign({}, opts, { delayMS: opts.delayMS + 65 }));
      return;
    }
    if (cue === "hit_light" || cue === "hit") {
      arcadeNoise(audio, 0.052, 0.075 * opts.intensity, "bandpass", 1900, opts);
      arcadeTone(audio, 118, 0.035, 0.05 * opts.intensity, "square", opts);
      arcadeTone(audio, 720, 0.026, 0.038 * opts.intensity, "triangle", Object.assign({}, opts, { delayMS: opts.delayMS + 7 }));
      return;
    }
    if (cue === "hit_heavy") {
      arcadeNoise(audio, 0.082, 0.1 * heavy, "lowpass", 1100, opts);
      arcadeTone(audio, 74, 0.055, 0.075 * heavy, "square", opts);
      arcadeTone(audio, 540, 0.04, 0.054 * heavy, "triangle", Object.assign({}, opts, { delayMS: opts.delayMS + 10 }));
      arcadeTone(audio, 1260, 0.024, 0.034 * heavy, "square", Object.assign({}, opts, { delayMS: opts.delayMS + 22 }));
      return;
    }
    if (cue === "counter" || cue === "punish") {
      playArcadeSFX("hit_heavy", Object.assign({}, opts, { intensity: Math.min(1.25, opts.intensity + 0.12) }));
      arcadeTone(audio, cue === "punish" ? 990 : 1180, 0.075, 0.052, "square", Object.assign({}, opts, { delayMS: opts.delayMS + 42 }));
      arcadeTone(audio, cue === "punish" ? 1320 : 1480, 0.05, 0.04, "triangle", Object.assign({}, opts, { delayMS: opts.delayMS + 74 }));
      return;
    }
    if (cue === "launcher") {
      arcadeNoise(audio, 0.06, 0.07 * heavy, "highpass", 1100, opts);
      arcadeSweep(audio, 240, 980, 0.16, 0.06 * heavy, "sawtooth", opts);
      arcadeTone(audio, 1560, 0.04, 0.035, "square", Object.assign({}, opts, { delayMS: opts.delayMS + 80 }));
      return;
    }
    if (cue === "block") {
      arcadeNoise(audio, 0.045, 0.058 * opts.intensity, "bandpass", 820, opts);
      arcadeTone(audio, 150, 0.035, 0.055 * opts.intensity, "square", opts);
      arcadeTone(audio, 270, 0.04, 0.035 * opts.intensity, "triangle", Object.assign({}, opts, { delayMS: opts.delayMS + 10 }));
      return;
    }
    if (cue === "guard" || cue === "just_guard") {
      arcadeTone(audio, 420, 0.04, 0.05 * opts.intensity, "triangle", opts);
      arcadeTone(audio, 980, 0.035, 0.045 * opts.intensity, "square", Object.assign({}, opts, { delayMS: opts.delayMS + 14 }));
      arcadeTone(audio, 1540, 0.04, 0.03, "triangle", Object.assign({}, opts, { delayMS: opts.delayMS + 34 }));
      return;
    }
    if (cue === "guard_cancel") {
      playArcadeSFX("just_guard", opts);
      arcadeSweep(audio, 520, 1120, 0.12, 0.045, "square", Object.assign({}, opts, { delayMS: opts.delayMS + 44 }));
      return;
    }
    if (cue === "armor") {
      arcadeNoise(audio, 0.09, 0.07 * heavy, "lowpass", 420, opts);
      arcadeTone(audio, 72, 0.08, 0.075 * heavy, "square", opts);
      arcadeTone(audio, 144, 0.06, 0.05 * heavy, "sawtooth", Object.assign({}, opts, { delayMS: opts.delayMS + 18 }));
      return;
    }
    if (cue === "throw") {
      arcadeNoise(audio, 0.075, 0.06 * heavy, "bandpass", 620, opts);
      arcadeSweep(audio, 420, 120, 0.11, 0.052 * heavy, "sawtooth", opts);
      arcadeTone(audio, 110, 0.065, 0.08 * heavy, "square", Object.assign({}, opts, { delayMS: opts.delayMS + 24 }));
      return;
    }
    if (cue === "throw_tech") {
      arcadeTone(audio, 560, 0.035, 0.055, "square", opts);
      arcadeTone(audio, 1120, 0.05, 0.05, "triangle", Object.assign({}, opts, { delayMS: opts.delayMS + 20 }));
      arcadeNoise(audio, 0.035, 0.045, "highpass", 1800, Object.assign({}, opts, { delayMS: opts.delayMS + 10 }));
      return;
    }
    if (cue === "surge" || cue === "surge_ready") {
      arcadeSweep(audio, cue === "surge" ? 160 : 320, cue === "surge" ? 920 : 1280, cue === "surge" ? 0.34 : 0.12, cue === "surge" ? 0.07 : 0.045, "sawtooth", opts);
      arcadeTone(audio, cue === "surge" ? 80 : 640, cue === "surge" ? 0.24 : 0.06, cue === "surge" ? 0.055 : 0.035, "square", Object.assign({}, opts, { delayMS: opts.delayMS + 38 }));
      return;
    }
    arcadeTone(audio, 440, 0.035, 0.045, "square", opts);
    arcadeTone(audio, 660, 0.04, 0.035, "triangle", Object.assign({}, opts, { delayMS: opts.delayMS + 12 }));
  }

  function arcadeConnectToOutput(audio, node, opts, nodes) {
    let tail = node;
    if (typeof audio.createStereoPanner === "function" && Math.abs(opts.pan) > 0.001) {
      const panner = audio.createStereoPanner();
      panner.pan.value = opts.pan;
      tail.connect(panner);
      tail = panner;
      nodes.push(panner);
    }
    tail.connect(arcadeOutput(audio));
  }

  function arcadeSetParam(param, value, time) {
    if (param && typeof param.setValueAtTime === "function") {
      param.setValueAtTime(value, time || 0);
      return;
    }
    if (param && Object.prototype.hasOwnProperty.call(param, "value")) {
      param.value = value;
    }
  }

  function arcadeRampParam(param, value, time, exponential) {
    if (param && exponential && typeof param.exponentialRampToValueAtTime === "function") {
      param.exponentialRampToValueAtTime(Math.max(0.0001, value), time);
      return;
    }
    if (param && typeof param.linearRampToValueAtTime === "function") {
      param.linearRampToValueAtTime(value, time);
      return;
    }
    arcadeSetParam(param, value, time);
  }

  function arcadeEnvelope(gain, now, volume, duration) {
    if (!gain || !gain.gain) return;
    arcadeSetParam(gain.gain, 0.0001, now);
    arcadeRampParam(gain.gain, Math.max(0.0001, volume), now + 0.006, true);
    arcadeRampParam(gain.gain, 0.0001, now + duration + 0.04, true);
  }

  function arcadeTrackVoice(record) {
    arcadeAudioState.active.push(record);
    while (arcadeAudioState.active.length > arcadeAudioState.voiceLimit) {
      releaseArcadeAudio(arcadeAudioState.active[0], true);
    }
  }

  function arcadeTone(audio, freq, duration, volume, type, options) {
    const opts = arcadeSoundOptions(options);
    const now = audio.currentTime || 0;
    const osc = audio.createOscillator();
    const gain = audio.createGain();
    osc.type = type || "square";
    arcadeSetParam(osc.frequency, freq * opts.rate, now);
    arcadeEnvelope(gain, now, volume * opts.intensity, duration);
    osc.connect(gain);
    const nodes = [osc, gain];
    arcadeConnectToOutput(audio, gain, opts, nodes);
    const record = { source: osc, nodes: [osc, gain] };
    record.nodes = nodes;
    arcadeTrackVoice(record);
    osc.onended = function() {
      releaseArcadeAudio(record, false);
    };
    const startAt = now + opts.delayMS / 1000;
    osc.start(startAt);
    osc.stop(startAt + duration + 0.08);
  }

  function arcadeSweep(audio, startFreq, endFreq, duration, volume, type, options) {
    const opts = arcadeSoundOptions(options);
    const now = audio.currentTime || 0;
    const osc = audio.createOscillator();
    const gain = audio.createGain();
    osc.type = type || "sawtooth";
    const startAt = now + opts.delayMS / 1000;
    arcadeSetParam(osc.frequency, Math.max(20, startFreq * opts.rate), startAt);
    arcadeRampParam(osc.frequency, Math.max(20, endFreq * opts.rate), startAt + duration, false);
    arcadeEnvelope(gain, startAt, volume * opts.intensity, duration);
    osc.connect(gain);
    const nodes = [osc, gain];
    arcadeConnectToOutput(audio, gain, opts, nodes);
    const record = { source: osc, nodes: nodes };
    arcadeTrackVoice(record);
    osc.onended = function() {
      releaseArcadeAudio(record, false);
    };
    osc.start(startAt);
    osc.stop(startAt + duration + 0.08);
  }

  function arcadeNoise(audio, duration, volume, filterType, frequency, options) {
    if (typeof audio.createBuffer !== "function" || typeof audio.createBufferSource !== "function") {
      arcadeTone(audio, frequency || 440, duration, volume * 0.7, "square", options);
      return;
    }
    const opts = arcadeSoundOptions(options);
    const sampleRate = Math.max(8000, audio.sampleRate || 44100);
    const length = Math.max(1, Math.floor(sampleRate * duration));
    const buffer = audio.createBuffer(1, length, sampleRate);
    const data = buffer.getChannelData(0);
    for (let i = 0; i < length; i += 1) {
      const falloff = 1 - i / length;
      data[i] = (Math.random() * 2 - 1) * falloff;
    }
    const source = audio.createBufferSource();
    const gain = audio.createGain();
    source.buffer = buffer;
    let tail = source;
    const nodes = [source, gain];
    if (typeof audio.createBiquadFilter === "function") {
      const filter = audio.createBiquadFilter();
      filter.type = filterType || "bandpass";
      if (filter.frequency) filter.frequency.value = Math.max(40, frequency || 1200);
      if (filter.Q) filter.Q.value = filter.type === "lowpass" ? 0.75 : 4.5;
      source.connect(filter);
      tail = filter;
      nodes.push(filter);
    }
    const now = audio.currentTime || 0;
    const startAt = now + opts.delayMS / 1000;
    arcadeEnvelope(gain, startAt, volume * opts.intensity, duration);
    tail.connect(gain);
    arcadeConnectToOutput(audio, gain, opts, nodes);
    const record = { source: source, nodes: nodes };
    arcadeTrackVoice(record);
    source.onended = function() {
      releaseArcadeAudio(record, false);
    };
    source.start(startAt);
    source.stop(startAt + duration + 0.08);
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
      // Some test doubles and embedded runtimes expose binaryType as read-only.
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
  function entryRequiresAsyncWebGPUProbe(entry) {
    const required = requiredCapabilityList(entry);
    return required.some((capability) => capability.indexOf("webgpu:") === 0 || capability.indexOf("webgpu-feature:") === 0);
  }

  async function prepareRuntimeCapabilityProbe(entry) {
    if (!entryRequiresAsyncWebGPUProbe(entry)) {
      return;
    }
    if (typeof window !== "undefined" && typeof window.__gosx_scene3d_webgpu_probe_ready === "function") {
      await window.__gosx_scene3d_webgpu_probe_ready();
    }
  }

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
    await prepareRuntimeCapabilityProbe(entry);
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
    initializeClientIdentity(manifest.clientIdentity);

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
      || Boolean(manifest && manifest.clientIdentity)
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
