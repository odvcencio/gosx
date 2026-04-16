(function() {
  "use strict";

  const registerFeature = window.__gosx_register_bootstrap_feature;
  if (typeof registerFeature !== "function") {
    console.error("[gosx] runtime bootstrap feature registry missing");
    return;
  }

  registerFeature("engines", function(api) {
    const engineFactories = api.engineFactories;
    const fetchProgram = api.fetchProgram;
    const inferProgramFormat = api.inferProgramFormat;
    const engineFrame = api.engineFrame;
    const cancelEngineFrame = api.cancelEngineFrame;
    const capabilityList = api.capabilityList;
    const activateInputProviders = api.activateInputProviders;
    const releaseInputProviders = api.releaseInputProviders;
    const clearChildren = api.clearChildren;
    const sceneNumber = api.sceneNumber;
    const sceneBool = api.sceneBool;
    const gosxReadSharedSignal = api.gosxReadSharedSignal;
    const gosxNotifySharedSignal = api.gosxNotifySharedSignal;
    const gosxSubscribeSharedSignal = api.gosxSubscribeSharedSignal;

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

  function createEngineContext(entry, mount, runtime) {
    return {
      id: entry.id,
      kind: entry.kind,
      component: entry.component,
      mount: mount,
      props: entry.props || {},
      capabilities: entry.capabilities || [],
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
    const runtime = createEngineRuntime(entry, mount);
    const ctx = createEngineContext(entry, mount, runtime);
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

    return {
      runtimeReady(manifest) {
        return mountAllEngines(manifest);
      },
      disposePage() {
        for (const engineID of Array.from(window.__gosx.engines.keys())) {
          window.__gosx_dispose_engine(engineID);
        }
      },
      disposeEngine: window.__gosx_dispose_engine,
      engineFrame: window.__gosx_engine_frame,
    };
  });
})();
//# sourceMappingURL=bootstrap-feature-engines.js.map
