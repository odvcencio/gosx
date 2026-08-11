// 30b — engine mounting: the engine factory registry, the built-in engine
// factories, and the mount and remount paths.
//
// Chunks: bootstrap.js, bootstrap-feature-engines.js.
// Depends on 30a for delegated listeners and on the scene and video modules
// the factories call. 30e disposes what this file mounts.
  // --------------------------------------------------------------------------
  // Engine mounting
  // --------------------------------------------------------------------------

  function resolveEngineFactory(entry) {
    if (entry && entry.kind === "video") {
      return createBuiltInVideoEngine;
    }
    const exportName = engineExportName(entry);
    if (!exportName) return null;
    return engineFactories[exportName] || null;
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

  const goWASMEngineModules = new Map();
  const goWASMEngineRegistrationTokens = new Map();
  const goWASMEngineRegistrationTokenEnv = "GOSX_GO_WASM_REGISTRATION_TOKEN";
  let goWASMEnginePageGeneration = 0;

  function engineUsesGoWASMRuntime(entry) {
    return Boolean(entry && entry.runtime === "go-wasm");
  }

  function goWASMEngineError(code, message, entry, cause) {
    const error = new Error(message);
    error.name = "GoSXGoWASMEngineError";
    error.code = code;
    if (entry) {
      error.component = String(entry.component || "");
      error.engineID = String(entry.id || "");
      error.programRef = String(entry.programRef || "");
    }
    if (cause) {
      error.cause = cause;
    }
    return error;
  }

  function goWASMEngineRegistrationToken() {
    if (
      typeof crypto === "undefined" ||
      !crypto ||
      typeof crypto.getRandomValues !== "function" ||
      typeof Uint32Array !== "function"
    ) {
      throw goWASMEngineError(
        "go-wasm-secure-random-missing",
        "secure random values are required to boot a Go-WASM engine",
      );
    }
    const words = new Uint32Array(4);
    crypto.getRandomValues(words);
    return Array.from(words, function(word) {
      return Number(word).toString(16).padStart(8, "0");
    }).join("");
  }

  window.__gosx_register_go_wasm_engine_factory = function(token, name, factory) {
    const registrationToken = String(token || "").trim();
    const component = String(name || "").trim();
    const active = goWASMEngineRegistrationTokens.get(registrationToken);
    if (!active || active.state !== "registering" || !component || typeof factory !== "function") {
      console.error("[gosx] rejected Go-WASM engine factory registration:", component || "<empty>");
      return false;
    }
    if (active.factories.has(component)) {
      console.error("[gosx] duplicate Go-WASM engine factory registration:", component);
      return false;
    }
    active.factories.set(component, factory);
    if (!active.registrationSettleScheduled) {
      active.registrationSettleScheduled = true;
      queueMicrotask(function() {
        active.registrationSettleScheduled = false;
        if (active.state === "registering" && active.factories.size > 0 && typeof active.resolveReady === "function") {
          active.resolveReady();
        }
      });
    }
    return true;
  };

  async function instantiateGoWASMEngine(response, importObject) {
    if (typeof WebAssembly.instantiateStreaming === "function") {
      try {
        return await WebAssembly.instantiateStreaming(response.clone(), importObject);
      } catch (_streamError) {
        // Incorrect or stripped application/wasm content types are common in
        // static hosting. Fall back to bytes without issuing a second fetch.
      }
    }
    const bytes = await response.arrayBuffer();
    return WebAssembly.instantiate(bytes, importObject);
  }

  function invalidateGoWASMEngineModule(record, cause, state) {
    if (!record || record.state === "failed" || record.state === "exited") return;
    record.state = state || "failed";
    record.error = cause || goWASMEngineError(
      "go-wasm-module-invalidated",
      "Go-WASM engine module was invalidated",
      { programRef: record.programRef },
    );
    goWASMEngineRegistrationTokens.delete(record.token);
    if (goWASMEngineModules.get(record.programRef) === record) {
      goWASMEngineModules.delete(record.programRef);
    }
    if (record.controller && typeof record.controller.abort === "function") {
      try {
        record.controller.abort();
      } catch (_abortError) {
        // AbortController.abort is idempotent; tolerate incomplete shims.
      }
    }
    if (typeof record.rejectReady === "function") {
      record.rejectReady(record.error);
    }
    if (typeof record.rejectCancellation === "function") {
      record.rejectCancellation(record.error);
    }
    record.factories.clear();

    if (record.mountedIDs.size > 0 && typeof window.__gosx_dispose_engine === "function") {
      for (const engineID of Array.from(record.mountedIDs)) {
        const mounted = window.__gosx && window.__gosx.engines
          ? window.__gosx.engines.get(engineID)
          : null;
        if (mounted && mounted.moduleRecord === record) {
          window.__gosx_dispose_engine(engineID);
        }
      }
    }
  }

  function cancelGoWASMEngineModuleIfUnused(record) {
    if (!record || record.state !== "booting" && record.state !== "registering") return;
    if (record.waiters.size > 0) return;
    invalidateGoWASMEngineModule(record, goWASMEngineError(
      "go-wasm-boot-cancelled",
      "Go-WASM engine module boot was cancelled because no mounts still need it",
      { programRef: record.programRef },
    ));
  }

  function attachGoWASMEngineWaiter(record, pending) {
    if (!record || !pending || pending.closed) return;
    if (pending.moduleWaiter === record) return;
    releaseGoWASMEngineWaiter(pending);
    pending.moduleWaiter = record;
    pending.moduleRecord = record;
    record.waiters.add(pending);
  }

  function releaseGoWASMEngineWaiter(pending) {
    if (!pending || !pending.moduleWaiter) return;
    const record = pending.moduleWaiter;
    pending.moduleWaiter = null;
    record.waiters.delete(pending);
    cancelGoWASMEngineModuleIfUnused(record);
  }

  async function bootGoWASMEngineModule(record) {
    const programRef = record.programRef;
    const StandardGo = window.__gosx_standard_go_wasm_ctor;
    if (typeof StandardGo !== "function") {
      throw goWASMEngineError(
        "go-wasm-runtime-missing",
        "the isolated standard-Go wasm_exec asset must be loaded before a Go-WASM engine",
        { programRef: programRef },
      );
    }
    let response;
    try {
      response = await Promise.race([
        fetch(programRef, record.controller ? { signal: record.controller.signal } : {}),
        record.cancellation,
      ]);
    } catch (cause) {
      throw goWASMEngineError(
        "go-wasm-fetch-failed",
        "failed to fetch Go-WASM engine module " + programRef,
        { programRef: programRef },
        cause,
      );
    }
    if (!response.ok) {
      throw goWASMEngineError(
        "go-wasm-fetch-status",
        "Go-WASM engine fetch failed with status " + response.status,
        { programRef: programRef },
      );
    }

    const go = new StandardGo();
    if (!go.env || typeof go.env !== "object") go.env = {};
    go.env[goWASMEngineRegistrationTokenEnv] = record.token;
    let result;
    try {
      result = await Promise.race([
        instantiateGoWASMEngine(response, go.importObject),
        record.cancellation,
      ]);
    } catch (cause) {
      throw goWASMEngineError(
        "go-wasm-instantiate-failed",
        "failed to instantiate Go-WASM engine module " + programRef,
        { programRef: programRef },
        cause,
      );
    }

    const ready = new Promise(function(resolve, reject) {
      record.resolveReady = resolve;
      record.rejectReady = reject;
    });
    const configuredFactoryTimeout = Number(window.__gosx_go_wasm_factory_timeout_ms);
    record.timeout = setTimeout(function() {
      record.rejectReady(goWASMEngineError(
        "go-wasm-factory-timeout",
        "timed out waiting for Go-WASM engine factory registration",
        { programRef: programRef },
      ));
    }, Number.isFinite(configuredFactoryTimeout) && configuredFactoryTimeout > 0
      ? Math.min(60000, Math.max(1, Math.floor(configuredFactoryTimeout)))
      : 10000);

    record.state = "registering";
    goWASMEngineRegistrationTokens.set(record.token, record);
    let runResult;
    try {
      runResult = go.run(result.instance);
      const exited = runResult && typeof runResult.then === "function"
        ? Promise.resolve(runResult).then(function() {
            return goWASMEngineError(
              "go-wasm-module-exited",
              "Go-WASM engine module exited",
              { programRef: programRef },
            );
          }, function(cause) {
            return goWASMEngineError(
              "go-wasm-run-failed",
              "Go-WASM engine module failed",
              { programRef: programRef },
              cause,
            );
          })
        : new Promise(function() {});
      record.exitSignal = exited;
      const startup = await Promise.race([
        ready.then(function() { return null; }),
        exited,
        record.cancellation,
      ]);
      if (startup) throw startup;

      // A standard-Go main that returns immediately leaves syscall/js factory
      // wrappers behind, but those wrappers can no longer resume the runtime.
      // Give the run promise one macrotask to report that terminal state before
      // publishing the module as ready.
      const immediateExit = await Promise.race([
        new Promise(function(resolve) { setTimeout(function() { resolve(null); }, 0); }),
        exited,
        record.cancellation,
      ]);
      if (immediateExit) throw immediateExit;
      record.state = "ready";
      exited.then(function(exitError) {
        if (record.state === "ready") {
          invalidateGoWASMEngineModule(record, exitError, "exited");
        }
      });
    } catch (cause) {
      if (cause && cause.name === "GoSXGoWASMEngineError") throw cause;
      throw goWASMEngineError(
        "go-wasm-run-failed",
        "Go-WASM engine module failed while starting",
        { programRef: programRef },
        cause,
      );
    } finally {
      clearTimeout(record.timeout);
      goWASMEngineRegistrationTokens.delete(record.token);
    }
    return record;
  }

  function loadGoWASMEngineModule(programRef, pending) {
    let record = goWASMEngineModules.get(programRef);
    if (!record) {
      let rejectCancellation;
      record = {
        programRef,
        token: goWASMEngineRegistrationToken(),
        state: "booting",
        error: null,
        factories: new Map(),
        waiters: new Set(),
        mountedIDs: new Set(),
        controller: typeof AbortController === "function" ? new AbortController() : null,
        cancellation: new Promise(function(_resolve, reject) { rejectCancellation = reject; }),
        rejectCancellation: null,
        resolveReady: null,
        rejectReady: null,
        timeout: 0,
        exitSignal: null,
        registrationSettleScheduled: false,
        boot: null,
      };
      record.rejectCancellation = rejectCancellation;
      goWASMEngineModules.set(programRef, record);
      record.boot = bootGoWASMEngineModule(record).catch(function(cause) {
        invalidateGoWASMEngineModule(record, cause, record.state === "exited" ? "exited" : "failed");
        throw cause;
      });
    }
    attachGoWASMEngineWaiter(record, pending);
    return record;
  }

  async function resolveGoWASMEngineFactory(entry, pending) {
    const programRef = String(entry && entry.programRef || "").trim();
    if (!programRef) {
      throw goWASMEngineError(
        "go-wasm-program-ref-missing",
        "Go-WASM engine requires a WASM programRef",
        entry,
      );
    }
    const record = loadGoWASMEngineModule(programRef, pending);
    try {
      await record.boot;
    } finally {
      releaseGoWASMEngineWaiter(pending);
    }
    if (record.state !== "ready") {
      throw record.error || goWASMEngineError(
        "go-wasm-module-unavailable",
        "Go-WASM engine module is unavailable",
        entry,
      );
    }
    const component = String(entry.component || "").trim();
    const factory = record.factories.get(component);
    if (typeof factory !== "function") {
      throw goWASMEngineError(
        "go-wasm-factory-missing",
        "Go-WASM engine module did not register component " + component,
        entry,
      );
    }
    return { factory, moduleRecord: record };
  }

  function validateGoWASMEngineEntry(entry) {
    if (!engineUsesGoWASMRuntime(entry)) return null;
    if (!entry || typeof entry !== "object") {
      return goWASMEngineError("go-wasm-manifest-entry-invalid", "Go-WASM engine manifest entry must be an object", entry);
    }
    const engineID = String(entry.id || "").trim();
    const component = String(entry.component || "").trim();
    const programRef = String(entry.programRef || "").trim();
    const kind = String(entry.kind || "").trim();
    if (!engineID) {
      return goWASMEngineError("go-wasm-engine-id-missing", "Go-WASM engine requires an id", entry);
    }
    if (!component) {
      return goWASMEngineError("go-wasm-component-missing", "Go-WASM engine requires a component", entry);
    }
    if (!programRef) {
      return goWASMEngineError("go-wasm-program-ref-missing", "Go-WASM engine requires a WASM programRef", entry);
    }
    if (kind !== "worker" && kind !== "surface" && kind !== "video") {
      return goWASMEngineError("go-wasm-kind-invalid", "Go-WASM engine kind is invalid: " + kind, entry);
    }
    if (engineKindNeedsMount(kind) && !String(entry.mountId || entry.id || "").trim()) {
      return goWASMEngineError("go-wasm-mount-id-missing", "Go-WASM engine kind " + kind + " requires a mount id", entry);
    }
    return null;
  }

  function snapshotGoWASMEngineFallback(mount) {
    if (!mount || !mount.childNodes) return null;
    return Array.from(mount.childNodes, function(child) {
      return child && typeof child.cloneNode === "function" ? child.cloneNode(true) : null;
    }).filter(Boolean);
  }

  function restoreGoWASMEngineFallback(mount, snapshot) {
    if (!mount || !Array.isArray(snapshot)) return;
    clearChildren(mount);
    for (const child of snapshot) {
      mount.appendChild(typeof child.cloneNode === "function" ? child.cloneNode(true) : child);
    }
  }

  function pendingEngineOwned(pending) {
    return Boolean(
      pending &&
      !pending.closed &&
      pending.generation === goWASMEnginePageGeneration &&
      pendingEngineRuntimes.get(pending.id) === pending
    );
  }

  function disposePendingEngine(pending, restoreFallback) {
    if (!pending || pending.closed) return;
    pending.closed = true;
    if (pendingEngineRuntimes.get(pending.id) === pending) {
      pendingEngineRuntimes.delete(pending.id);
    }
    releaseGoWASMEngineWaiter(pending);
    if (!pending.runtimeDisposed && pending.runtime && typeof pending.runtime.dispose === "function") {
      pending.runtimeDisposed = true;
      try {
        pending.runtime.dispose();
      } catch (disposeError) {
        console.error("[gosx] runtime dispose error for engine " + pending.id + ":", disposeError);
      }
    }
    if (restoreFallback && pending.fallbackSnapshot) {
      restoreGoWASMEngineFallback(pending.mount, pending.fallbackSnapshot);
    }
  }

  function transferPendingEngine(pending) {
    if (!pending || pending.closed) return false;
    if (!pendingEngineOwned(pending)) return false;
    pending.closed = true;
    pendingEngineRuntimes.delete(pending.id);
    releaseGoWASMEngineWaiter(pending);
    return true;
  }

  function reportGoWASMEngineModuleFailure(entry, mount, error) {
    console.error(`[gosx] failed to load engine module ${entry && entry.id || "<unknown>"}:`, error);
    if (typeof window !== "undefined" && typeof window.__gosx_emit === "function") {
      window.__gosx_emit("error", "engine", "failed to load engine module", {
        component: String(entry && entry.component || ""),
        engineID: String(entry && entry.id || ""),
        programRef: String(entry && entry.programRef || ""),
        code: error && error.code ? String(error.code) : "engine-module-load-failed",
        error: error && error.message ? String(error.message) : String(error),
      });
    }
    if (window.__gosx && typeof window.__gosx.reportIssue === "function") {
      window.__gosx.reportIssue({
        scope: "engine",
        type: "module",
        component: entry && entry.component,
        source: entry && entry.id,
        ref: entry && (entry.programRef || entry.component),
        element: mount,
        code: error && error.code ? String(error.code) : "engine-module-load-failed",
        message: error && error.message ? String(error.message) : `failed to load engine module ${entry && entry.id || "<unknown>"}`,
        error,
        fallback: "server",
      });
    }
  }

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
    const result = String(video.canPlayType("application/vnd.apple.mpegurl") || "").trim().toLowerCase();
    // Only "probably" indicates reliable native HLS (Safari / iOS). Chrome and
    // Chromium-Edge return the speculative "maybe" but cannot actually decode
    // an .m3u8 playlist natively — they must use hls.js (MSE). Treating "maybe"
    // as native support black-screens Chrome: it sets video.src=playlist,
    // decodes a few frames, then stalls. Anything but "probably" → use hls.js.
    return result === "probably";
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
    await loadScriptTag(path, "hls");
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

  function videoSeekableRange(video) {
    if (!video || !video.seekable || typeof video.seekable.length !== "number" || video.seekable.length === 0) {
      return [0, 0];
    }
    const lastIndex = video.seekable.length - 1;
    const start = typeof video.seekable.start === "function" ? Math.max(0, sceneNumber(video.seekable.start(lastIndex), 0)) : 0;
    const end = typeof video.seekable.end === "function" ? Math.max(0, sceneNumber(video.seekable.end(lastIndex), 0)) : 0;
    return [start, Math.max(start, end)];
  }

  function videoNormalizeHLSAudioTrack(item, index, activeIndex) {
    const source = item && typeof item === "object" ? item : {};
    const id = source.id != null ? String(source.id) : String(index);
    const language = String(source.lang || source.language || "").trim();
    const label = String(source.name || source.label || language || ("Audio " + (index + 1))).trim();
    return {
      id: id,
      index: index,
      label: label,
      language: language,
      active: index === activeIndex,
    };
  }

  function videoNormalizeNativeAudioTrack(track, index) {
    const id = track && track.id ? String(track.id) : String(index);
    const language = String((track && track.language) || "").trim();
    const label = String((track && track.label) || language || ("Audio " + (index + 1))).trim();
    return {
      id: id,
      index: index,
      label: label,
      language: language,
      active: Boolean(track && track.enabled),
    };
  }

  function videoNormalizeConfiguredAudioTrack(item, index, activeID) {
    const source = item && typeof item === "object" ? item : {};
    const id = String(videoPropValue(source, ["id", "trackID", "trackId"], String(index)) || String(index)).trim();
    const language = String(videoPropValue(source, ["language", "lang"], "") || "").trim();
    const label = String(videoPropValue(source, ["label", "title", "name"], language || ("Audio " + (index + 1))) || "").trim();
    return {
      id: id || String(index),
      index: index,
      label: label,
      language: language,
      active: String(activeID || "") !== "" && String(activeID) === (id || String(index)),
    };
  }

  function videoConfiguredAudioTracks(props, activeID) {
    const tracks = videoPropValue(props, ["audioTracks", "audio_tracks"], []);
    if (!Array.isArray(tracks)) {
      return [];
    }
    return tracks.map(function(track, index) {
      return videoNormalizeConfiguredAudioTrack(track, index, activeID);
    });
  }

  function videoNormalizeQualityLevel(item, index, activeIndex) {
    const source = item && typeof item === "object" ? item : {};
    const height = Math.max(0, Math.round(sceneNumber(source.height, 0)));
    const width = Math.max(0, Math.round(sceneNumber(source.width, 0)));
    const bitrate = Math.max(0, Math.round(sceneNumber(source.bitrate, 0)));
    const name = String(source.name || (height > 0 ? (height + "p") : ("Level " + (index + 1)))).trim();
    return {
      index: index,
      height: height,
      width: width,
      bitrate: bitrate,
      name: name,
      active: index === activeIndex,
    };
  }

  // Preference persistence (Slice 4, opt-in via VideoProps.PersistPrefs /
  // PersistKey): all localStorage access below is guarded with try/catch so
  // private-browsing / storage-disabled contexts degrade to a no-op instead
  // of throwing. This field list is module-scope (not a local const inside
  // createBuiltInVideoEngine) because restorePersistedVideoPrefs() runs
  // eagerly at the very top of that closure, before a same-scope `const`
  // declared further down would have run its initializer (TDZ).
  const PERSISTED_VIDEO_PREF_FIELDS = ["volume", "mute", "rate", "subtitleTrack", "subtitleOffsetMs", "subtitleScale", "subtitleStyle", "audioTrack", "qualityLevel"];

  function videoPersistEnabled(props) {
    return sceneBool(videoPropValue(props, ["persistPrefs"], false), false) ||
      String(videoPropValue(props, ["persistKey"], "") || "").trim() !== "";
  }

  function videoPersistStorageKey(props, ctx) {
    const explicit = String(videoPropValue(props, ["persistKey"], "") || "").trim();
    const key = explicit || String((ctx && ctx.id) || "default");
    return "gosx:video:" + key + ":prefs";
  }

  function videoPersistStorage() {
    try {
      if (typeof window === "undefined" || !window.localStorage) {
        return null;
      }
      const probeKey = "__gosx_video_prefs_probe__";
      window.localStorage.setItem(probeKey, "1");
      window.localStorage.removeItem(probeKey);
      return window.localStorage;
    } catch (_error) {
      return null;
    }
  }

  function loadPersistedVideoPrefs(storage, key) {
    if (!storage) {
      return null;
    }
    try {
      const raw = storage.getItem(key);
      if (!raw) {
        return null;
      }
      const parsed = JSON.parse(raw);
      return parsed && typeof parsed === "object" ? parsed : null;
    } catch (_error) {
      return null;
    }
  }

  function savePersistedVideoPrefs(storage, key, prefs) {
    if (!storage) {
      return;
    }
    try {
      storage.setItem(key, JSON.stringify(prefs));
    } catch (_error) {
    }
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
    const normalized = {
      id: id || ("track-" + index),
      language: language,
      srclang: srcLang || language,
      title: title,
      kind: kind,
      src: src,
      default: sceneBool(videoPropValue(source, ["default"], false), false),
      forced: sceneBool(videoPropValue(source, ["forced"], false), false),
    };
    const authKey = String(videoPropValue(source, ["authKey", "auth_key"], "") || "").trim();
    if (authKey) {
      normalized.authKey = authKey;
    }
    if (sceneBool(videoPropValue(source, ["bitmap"], false), false)) {
      normalized.bitmap = true;
    }
    return normalized;
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

  function videoSubtitleOptions(props) {
    const options = videoPropValue(props, ["subtitles", "subtitleOptions", "subtitle_options"], {}) || {};
    return {
      offsetMS: sceneNumber(videoPropValue(options, ["offsetMs", "offsetMS", "offset_ms"], 0), 0),
      scale: String(videoPropValue(options, ["scale"], "") || "").trim().toLowerCase(),
      style: String(videoPropValue(options, ["style"], "") || "").trim().toLowerCase(),
      gapBridgeMS: Math.max(0, sceneNumber(videoPropValue(options, ["gapBridgeMs", "gap_bridge_ms"], 0), 0)),
      cueTailMS: Math.max(0, sceneNumber(videoPropValue(options, ["cueTailMs", "cue_tail_ms"], 0), 0)),
      paintLeadMS: sceneNumber(videoPropValue(options, ["paintLeadMs", "paint_lead_ms"], 0), 0),
      bitmapPrefetchLimit: Math.max(0, Math.floor(sceneNumber(videoPropValue(options, ["bitmapPrefetchLimit", "bitmap_prefetch_limit"], 0), 0))),
      retryLimit: Math.max(1, Math.floor(sceneNumber(videoPropValue(options, ["retryLimit", "retry_limit"], 60), 60))),
      retryRefreshAfter: Math.max(0, Math.floor(sceneNumber(videoPropValue(options, ["retryRefreshAfter", "retry_refresh_after"], 0), 0))),
      refreshEndpoint: String(videoPropValue(options, ["refreshEndpoint", "refresh_endpoint"], "") || "").trim(),
      refreshCallback: String(videoPropValue(options, ["refreshCallback", "refresh_callback"], "") || "").trim(),
    };
  }

  function videoAudioSourceOptions(props) {
    const options = videoPropValue(props, ["audioSource", "audio_source"], {}) || {};
    return {
      queryParam: String(videoPropValue(options, ["queryParam", "query_param"], "") || "").trim(),
    };
  }

  function videoFullscreenOptions(props) {
    const options = videoPropValue(props, ["fullscreen", "fullscreenOptions", "fullscreen_options"], {}) || {};
    return {
      target: String(videoPropValue(options, ["target"], "") || "").trim().toLowerCase(),
    };
  }

  function videoTelemetryOptions(props) {
    const options = videoPropValue(props, ["telemetry", "videoTelemetry", "video_telemetry"], {}) || {};
    return {
      endpoint: String(videoPropValue(options, ["endpoint"], "") || "").trim(),
      qualityIntervalMS: Math.max(0, sceneNumber(videoPropValue(options, ["qualityIntervalMs", "quality_interval_ms"], 0), 0)),
      stallRecoveryDelayMS: Math.max(0, sceneNumber(videoPropValue(options, ["stallRecoveryDelayMs", "stall_recovery_delay_ms"], 0), 0)),
      maxStallRecoveryCount: Math.max(0, Math.floor(sceneNumber(videoPropValue(options, ["maxStallRecoveryCount", "max_stall_recovery_count"], 0), 0))),
    };
  }

  function videoStableTrackIdentity(track, props) {
    const id = String(track && track.id || "").trim();
    if (id) {
      return id;
    }
    const authKey = String(track && track.authKey || "").trim();
    if (authKey) {
      return authKey;
    }
    const url = videoTrackURL(track, props);
    if (!url) {
      return "";
    }
    try {
      const parsed = new URL(url, window.location && window.location.href ? window.location.href : "http://localhost/");
      parsed.search = "";
      parsed.hash = "";
      return parsed.pathname;
    } catch (_error) {
      return String(url).split("#")[0].split("?")[0];
    }
  }

  function videoRefreshTrackIdentity(track, props) {
    const authKey = String(track && track.authKey || "").trim();
    if (authKey) {
      return authKey;
    }
    return videoStableTrackIdentity(track, props);
  }

  function videoSubtitleRefreshPayload(track, props, engineID) {
    const payload = {
      track: videoRefreshTrackIdentity(track, props),
      src: videoTrackURL(track, props),
    };
    if (engineID !== undefined) {
      payload.engineID = String(engineID || "");
    }
    return payload;
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
        if (videoNeedsHLS(source && source.src) && !videoSupportsNativeHLS(video)) {
          continue;
        }
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

  function videoParseBitmapCue(text) {
    const firstLine = String(text == null ? "" : text).split(/\n/)[0].trim();
    if (!/\.png(?:[#?]|$)/i.test(firstLine)) {
      return null;
    }
    const hashIndex = firstLine.indexOf("#");
    const image = { src: hashIndex >= 0 ? firstLine.slice(0, hashIndex) : firstLine };
    if (hashIndex < 0) {
      return image;
    }
    for (const part of firstLine.slice(hashIndex + 1).split("&")) {
      const bits = part.split("=");
      if (bits.length !== 2) {
        continue;
      }
      if (bits[0] === "xywh") {
        const xywh = bits[1].split(",").map(Number);
        if (xywh.length === 4 && xywh.every(Number.isFinite)) {
          image.x = xywh[0];
          image.y = xywh[1];
          image.w = xywh[2];
          image.h = xywh[3];
        }
      } else if (bits[0] === "canvas") {
        const canvas = bits[1].split(",").map(Number);
        if (canvas.length === 2 && canvas.every(Number.isFinite)) {
          image.canvasW = canvas[0];
          image.canvasH = canvas[1];
        }
      }
    }
    return image;
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
      const cueText = textLines.join("\n");
      cues.push({
        startMS,
        endMS,
        text: videoSanitizeCueHTML(cueText),
        image: videoParseBitmapCue(cueText),
      });
    }

    cues.sort(function(a, b) {
      if (a.startMS !== b.startMS) {
        return a.startMS - b.startMS;
      }
      return a.endMS - b.endMS;
    });
    let maxEndMS = 0;
    for (let i = 0; i < cues.length; i += 1) {
      const cue = cues[i];
      cue.nextStartMS = i + 1 < cues.length ? cues[i + 1].startMS : -1;
      maxEndMS = Math.max(maxEndMS, cue.endMS);
      cue.prefixMaxEndMS = maxEndMS;
    }
    return cues;
  }

  function videoCueVisibleAt(cue, currentMS, options) {
    if (!cue) {
      return false;
    }
    if (currentMS < cue.endMS) {
      return true;
    }
    const nextStart = sceneNumber(cue.nextStartMS, -1);
    if (nextStart > cue.endMS) {
      const gap = nextStart - cue.endMS;
      return gap <= sceneNumber(options && options.gapBridgeMS, 0) && currentMS < nextStart;
    }
    const tailMS = sceneNumber(options && options.cueTailMS, 0);
    return tailMS > 0 && currentMS < cue.endMS + tailMS;
  }

  function videoCueSearchEndMS(cue, options) {
    if (!cue) {
      return 0;
    }
    let endMS = cue.endMS + sceneNumber(options && options.cueTailMS, 0);
    const nextStart = sceneNumber(cue.nextStartMS, -1);
    if (nextStart > cue.endMS && nextStart - cue.endMS <= sceneNumber(options && options.gapBridgeMS, 0)) {
      endMS = Math.max(endMS, nextStart);
    }
    return endMS;
  }

  function videoActiveCues(cues, currentTimeSeconds, options) {
    const timing = options && typeof options === "object" ? options : {};
    const currentMS = Math.max(0, Math.round(sceneNumber(currentTimeSeconds, 0) * 1000 + sceneNumber(timing.offsetMS, 0) + sceneNumber(timing.paintLeadMS, 0)));
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
      if (sceneNumber(cue.prefixMaxEndMS, cue.endMS) + sceneNumber(timing.cueTailMS, 0) <= currentMS && videoCueSearchEndMS(cue, timing) <= currentMS) {
        break;
      }
      if (videoCueVisibleAt(cue, currentMS, timing)) {
        const activeCue = { text: cue.text };
        if (cue.image) {
          activeCue.image = cue.image;
        }
        active.push(activeCue);
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
    let subtitleOptions = videoSubtitleOptions(props);
    const audioSourceOptions = videoAudioSourceOptions(props);
    const fullscreenOptions = videoFullscreenOptions(props);
    const telemetryOptions = videoTelemetryOptions(props);
    const video = fallbackChildren.find(function(child) {
      return child && child.nodeType === 1 && child.tagName === "VIDEO";
    }) || document.createElement("video");
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
    // Restore persisted preferences (Slice 4) before any signal default is
    // read below, so requestedRate/volume/mute/subtitleTrack initial reads
    // above and the immediate subscribeVideoSignal() callbacks further down
    // observe the restored value as if the island had set it itself.
    restorePersistedVideoPrefs();
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
    let sourceSignalInitialized = false;
    let hlsLiveFlag = false;
    let lastAudioTracksSignature = "";
    let lastQualityLevelsSignature = "";
    let telemetryQualityTimer = 0;
    let telemetryStallTimer = 0;
    let telemetryStallRecoveries = 0;
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
      writeVideoOutputSignal("subtitleOptions", {
        offsetMs: subtitleOptions.offsetMS,
        scale: subtitleOptions.scale,
        style: subtitleOptions.style,
        gapBridgeMs: subtitleOptions.gapBridgeMS,
        cueTailMs: subtitleOptions.cueTailMS,
        paintLeadMs: subtitleOptions.paintLeadMS,
      });
    }

    function refreshSubtitleOptionsFromSignals() {
      const base = videoSubtitleOptions(props);
      subtitleOptions = Object.assign({}, base, {
        offsetMS: sceneNumber(readVideoSignal("subtitleOffsetMs", base.offsetMS), base.offsetMS),
        scale: String(readVideoSignal("subtitleScale", base.scale) || base.scale || "").trim().toLowerCase(),
        style: String(readVideoSignal("subtitleStyle", base.style) || base.style || "").trim().toLowerCase(),
      });
      if (subtitleOverlay) {
        if (subtitleOptions.scale) {
          subtitleOverlay.setAttribute("data-gosx-video-subtitle-scale", subtitleOptions.scale);
        } else {
          subtitleOverlay.removeAttribute("data-gosx-video-subtitle-scale");
        }
        if (subtitleOptions.style) {
          subtitleOverlay.setAttribute("data-gosx-video-subtitle-style", subtitleOptions.style);
        } else {
          subtitleOverlay.removeAttribute("data-gosx-video-subtitle-style");
        }
      }
      updateSubtitleOutputs();
      updateCueOutputs();
      persistPrefsIfEnabled();
    }

    function updateCueOutputs() {
      const next = videoActiveCues(subtitleState.cues, sceneNumber(video.currentTime, 0), subtitleOptions);
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
      if (subtitleOptions.scale) {
        subtitleOverlay.setAttribute("data-gosx-video-subtitle-scale", subtitleOptions.scale);
      }
      if (subtitleOptions.style) {
        subtitleOverlay.setAttribute("data-gosx-video-subtitle-style", subtitleOptions.style);
      }
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
      if (subtitleOptions.scale) {
        overlay.setAttribute("data-gosx-video-subtitle-scale", subtitleOptions.scale);
      }
      if (subtitleOptions.style) {
        overlay.setAttribute("data-gosx-video-subtitle-style", subtitleOptions.style);
      }
      for (const cue of active) {
        if (cue && cue.image && cue.image.src) {
          const image = cue.image;
          const img = document.createElement("img");
          img.setAttribute("class", "subtitle-image");
          img.setAttribute("src", image.src);
          if (image.canvasW > 0 && image.canvasH > 0) {
            img.style.left = (image.x / image.canvasW * 100) + "%";
            img.style.top = (image.y / image.canvasH * 100) + "%";
            img.style.width = (image.w / image.canvasW * 100) + "%";
          }
          overlay.appendChild(img);
          continue;
        }
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
      // Do NOT block when followState is null (no server heartbeat yet). The
      // 'play' handler pauses on a blocked autoplay, which consumes the
      // browser's one-shot autoplay grant; the later programmatic safePlay()
      // resume is then autoplay-blocked, leaving the video parked black. Until
      // the first heartbeat arrives, let local autoplay proceed and rely on
      // applyFollowState to sync once the server state is known. Only honor an
      // actual server "paused" (followState present AND playing=false).
      return Boolean(followState) && !sceneBool(followState.playing, false);
    }

    function clampVideoPercent(value) {
      return Math.max(0, Math.min(100, Math.round(sceneNumber(value, 0))));
    }

    // Slice 5 input lock: explicit props.lockInput, OR auto-on whenever the
    // engine is locked to a "follow" sync session — local transport commands
    // are already ignored server-side in that mode (see the "command"/"seek"/
    // "rate" subscribeVideoSignal handlers below), so the native <video>
    // controls and click/keyboard shortcuts should not be able to fight it.
    function videoInputLockActive() {
      return sceneBool(videoPropValue(props, ["lockInput"], false), false) || syncLockedToServer();
    }

    function videoInputLockBlocksKey(event) {
      // Do NOT trim: " " (Space) is itself the exact `key` value browsers
      // report for the spacebar, and String.trim() would strip it to "".
      const key = event && (event.key != null ? event.key : event.code);
      return key === " " || key === "Space" || key === "Spacebar" || key === "Enter";
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

    // Preference persistence (Slice 4). restorePersistedVideoPrefs() runs once
    // very early in setup (before any other `let`/`const` in this closure has
    // executed its initializer — hence PERSISTED_VIDEO_PREF_FIELDS below lives
    // at module scope, not as a local const here, to avoid a TDZ reference)
    // and only ever seeds a signal that has no value yet
    // (readVideoSignal(field, null) === null) — an island-provided initial
    // value, or a value already carried over from a prior mount on the same
    // page, always wins over the persisted one. persistPrefsIfEnabled() is
    // called from each relevant input-signal handler after it applies a
    // change, so storage always reflects the last-applied value.
    function restorePersistedVideoPrefs() {
      if (!videoPersistEnabled(props)) {
        return;
      }
      const storage = videoPersistStorage();
      if (!storage) {
        return;
      }
      const prefs = loadPersistedVideoPrefs(storage, videoPersistStorageKey(props, ctx));
      if (!prefs) {
        return;
      }
      for (const field of PERSISTED_VIDEO_PREF_FIELDS) {
        if (!Object.prototype.hasOwnProperty.call(prefs, field) || prefs[field] == null) {
          continue;
        }
        if (readVideoSignal(field, null) != null) {
          continue;
        }
        writeVideoSignal(field, prefs[field]);
      }
    }

    function persistPrefsIfEnabled() {
      if (!videoPersistEnabled(props)) {
        return;
      }
      const storage = videoPersistStorage();
      if (!storage) {
        return;
      }
      savePersistedVideoPrefs(storage, videoPersistStorageKey(props, ctx), {
        volume: sceneNumber(readVideoSignal("volume", video.volume), video.volume),
        mute: Boolean(video.muted),
        rate: requestedRate,
        subtitleTrack: String(readVideoSignal("subtitleTrack", "") || ""),
        subtitleOffsetMs: sceneNumber(readVideoSignal("subtitleOffsetMs", subtitleOptions.offsetMS), subtitleOptions.offsetMS),
        subtitleScale: String(readVideoSignal("subtitleScale", subtitleOptions.scale) || ""),
        subtitleStyle: String(readVideoSignal("subtitleStyle", subtitleOptions.style) || ""),
        audioTrack: String(readVideoSignal("audioTrack", "") || ""),
        qualityLevel: sceneNumber(readVideoSignal("qualityLevel", -1), -1),
      });
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

    function pipSupported() {
      return Boolean(document && document.pictureInPictureEnabled && video && typeof video.requestPictureInPicture === "function");
    }

    function enterPiP() {
      if (videoIsPoppedOut()) {
        return;
      }
      if (!pipSupported()) {
        setError("picture-in-picture unsupported");
        return;
      }
      try {
        const result = video.requestPictureInPicture();
        if (result && typeof result.catch === "function") {
          result.catch(function(error) {
            setError(error && error.message ? error.message : "picture-in-picture failed");
            updateVideoOutputs();
          });
        }
      } catch (error) {
        setError(error && error.message ? error.message : "picture-in-picture failed");
      }
    }

    function exitPiP() {
      if (!videoIsPoppedOut()) {
        return;
      }
      if (!document || typeof document.exitPictureInPicture !== "function") {
        setError("picture-in-picture unsupported");
        return;
      }
      try {
        const result = document.exitPictureInPicture();
        if (result && typeof result.catch === "function") {
          result.catch(function(error) {
            setError(error && error.message ? error.message : "exit picture-in-picture failed");
            updateVideoOutputs();
          });
        }
      } catch (error) {
        setError(error && error.message ? error.message : "exit picture-in-picture failed");
      }
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

    function videoIsLive() {
      if (hls && hlsLiveFlag) {
        return true;
      }
      return Boolean(video) && video.duration === Infinity;
    }

    function fullscreenTargetElement() {
      const target = fullscreenOptions.target;
      if (target === "video") {
        return video;
      }
      if (target === "parent" || target === "shell") {
        return mount && mount.parentNode && mount.parentNode.nodeType === 1 ? mount.parentNode : mount;
      }
      return mount || video;
    }

    function activeFullscreenElement() {
      return (document && (document.fullscreenElement || document.webkitFullscreenElement || document.webkitCurrentFullScreenElement)) || null;
    }

    function videoIsFullscreen() {
      const active = activeFullscreenElement();
      return Boolean(active && (active === mount || active === video || active === fullscreenTargetElement()));
    }

    function requestVideoFullscreen() {
      const target = fullscreenTargetElement();
      if (!target) {
        return;
      }
      const request = target.requestFullscreen || target.webkitRequestFullscreen || target.webkitEnterFullscreen;
      if (typeof request === "function") {
        try {
          request.call(target);
        } catch (error) {
          setError(error && error.message ? error.message : "fullscreen failed");
        }
      }
    }

    function exitVideoFullscreen() {
      const exit = document && (document.exitFullscreen || document.webkitExitFullscreen || document.webkitCancelFullScreen);
      if (typeof exit === "function") {
        try {
          exit.call(document);
        } catch (error) {
          setError(error && error.message ? error.message : "exit fullscreen failed");
        }
      }
    }

    function updateVideoOutputs() {
      const duration = videoOutputDuration();
      const playing = !sceneBool(video.paused, true) && !sceneBool(video.ended, false);
      const seekableRange = videoSeekableRange(video);
      const live = videoIsLive();
      writeVideoOutputSignal("position", Math.max(0, sceneNumber(video.currentTime, 0)));
      writeVideoOutputSignal("duration", duration);
      writeVideoOutputSignal("playing", playing);
      writeVideoOutputSignal("buffered", videoBufferedAhead(video));
      writeVideoOutputSignal("stalled", stalled);
      writeVideoOutputSignal("fullscreen", videoIsFullscreen());
      writeVideoOutputSignal("ready", sceneNumber(video.readyState, 0) >= 2);
      writeVideoOutputSignal("muted", Boolean(video.muted));
      writeVideoOutputSignal("actualRate", sceneNumber(video.playbackRate, requestedRate));
      writeVideoOutputSignal("syncConnected", Boolean(syncSocket && syncSocket.readyState === 1));
      writeVideoOutputSignal("viewerCount", Math.max(0, Math.floor(sceneNumber(followState && followState.viewerCount, 0))));
      writeVideoOutputSignal("seekable", seekableRange);
      writeVideoOutputSignal("isLive", live);
      writeVideoOutputSignal("liveEdgeLag", live ? Math.max(0, seekableRange[1] - Math.max(0, sceneNumber(video.currentTime, 0))) : null);
      writeVideoOutputSignal("pip", videoIsPoppedOut());
      writeVideoOutputSignal("inputLocked", videoInputLockActive());
      updateCueOutputs();
      if (!lastError) {
        writeVideoOutputSignal("error", "");
      }
    }

    function addListener(target, type, listener, options) {
      if (!target || typeof target.addEventListener !== "function") {
        return;
      }
      target.addEventListener(type, listener, options);
      eventListeners.push({ target, type, listener, options });
    }

    function emitVideoTelemetry(event, extra) {
      const endpoint = telemetryOptions.endpoint;
      if (!endpoint) {
        return;
      }
      try {
        const payload = Object.assign({
          version: 1,
          event: String(event || ""),
          engineID: String((ctx && ctx.id) || ""),
          src: currentSource,
          position: Math.max(0, sceneNumber(video.currentTime, 0)),
          duration: videoOutputDuration(),
          buffered: videoBufferedAhead(video),
          readyState: Math.max(0, Math.floor(sceneNumber(video.readyState, 0))),
          networkState: Math.max(0, Math.floor(sceneNumber(video.networkState, 0))),
          stalled: Boolean(stalled),
          qualityLevel: sceneNumber(readVideoSignal("qualityLevel", -1), -1),
        }, extra || {});
        const body = JSON.stringify(payload);
        if (typeof navigator !== "undefined" && navigator && typeof navigator.sendBeacon === "function" && typeof Blob === "function") {
          if (navigator.sendBeacon(endpoint, new Blob([body], { type: "application/json" }))) {
            return;
          }
        }
        fetch(endpoint, {
          method: "POST",
          credentials: "same-origin",
          cache: "no-store",
          keepalive: true,
          headers: { "Content-Type": "application/json" },
          body,
        }).catch(function() {});
      } catch (_error) {
      }
    }

    function startVideoQualityTelemetry() {
      const interval = telemetryOptions.qualityIntervalMS;
      if (!telemetryOptions.endpoint || interval <= 0 || telemetryQualityTimer) {
        return;
      }
      telemetryQualityTimer = setInterval(function() {
        if (disposed || sceneBool(video.paused, true)) {
          return;
        }
        const quality = typeof video.getVideoPlaybackQuality === "function" ? video.getVideoPlaybackQuality() : null;
        emitVideoTelemetry("quality", {
          droppedVideoFrames: quality ? Math.max(0, sceneNumber(quality.droppedVideoFrames, 0)) : null,
          totalVideoFrames: quality ? Math.max(0, sceneNumber(quality.totalVideoFrames, 0)) : null,
        });
      }, Math.max(1000, interval));
    }

    function clearTelemetryStallTimer() {
      if (telemetryStallTimer) {
        clearTimeout(telemetryStallTimer);
        telemetryStallTimer = 0;
      }
    }

    function scheduleTelemetryStallRecovery(reason) {
      emitVideoTelemetry("stall", { reason: String(reason || "unknown") });
      clearTelemetryStallTimer();
      const delay = telemetryOptions.stallRecoveryDelayMS;
      const maxCount = telemetryOptions.maxStallRecoveryCount;
      if (!delay || !maxCount || telemetryStallRecoveries >= maxCount) {
        return;
      }
      telemetryStallTimer = setTimeout(function() {
        telemetryStallTimer = 0;
        if (disposed || !stalled || sceneBool(video.paused, true)) {
          return;
        }
        telemetryStallRecoveries += 1;
        emitVideoTelemetry("stall-recovery", { action: "reload", attempt: telemetryStallRecoveries });
        try {
          video.load();
          safePlay();
        } catch (_error) {
        }
      }, Math.max(250, delay));
    }

    function teardownHLS() {
      if (hls && typeof hls.destroy === "function") {
        hls.destroy();
      }
      hls = null;
    }

    // Slice 1: audio tracks. hls.js owns selection when attached (in-manifest
    // alternate audio); otherwise fall back to the native video.audioTracks
    // list where the browser exposes one (e.g. multi-audio MKV in Firefox).
    function updateAudioTrackOutputs() {
      let tracks = [];
      if (hls && Array.isArray(hls.audioTracks) && hls.audioTracks.length > 0) {
        const activeIndex = sceneNumber(hls.audioTrack, -1);
        tracks = hls.audioTracks.map(function(item, index) {
          return videoNormalizeHLSAudioTrack(item, index, activeIndex);
        });
      } else if (video.audioTracks && video.audioTracks.length > 0) {
        for (let i = 0; i < video.audioTracks.length; i += 1) {
          tracks.push(videoNormalizeNativeAudioTrack(video.audioTracks[i], i));
        }
      } else {
        tracks = videoConfiguredAudioTracks(props, readVideoSignal("audioTrack", videoPropValue(props, ["audioTrack"], "")));
      }
      const signature = JSON.stringify(tracks);
      if (signature !== lastAudioTracksSignature) {
        lastAudioTracksSignature = signature;
        writeVideoOutputSignal("audioTracks", tracks);
      }
    }

    function applyAudioTrackSelection(value) {
      const selected = String(value == null ? "" : value).trim();
      if (hls && Array.isArray(hls.audioTracks)) {
        if (selected !== "" && selected !== "-1") {
          let nextIndex = -1;
          const asIndex = Number(selected);
          if (Number.isInteger(asIndex) && hls.audioTracks[asIndex]) {
            nextIndex = asIndex;
          } else {
            nextIndex = hls.audioTracks.findIndex(function(item, index) {
              return (item && item.id != null ? String(item.id) : String(index)) === selected;
            });
          }
          if (nextIndex >= 0) {
            hls.audioTrack = nextIndex;
          }
        }
        updateAudioTrackOutputs();
        persistPrefsIfEnabled();
        return;
      }
      if (video.audioTracks && video.audioTracks.length > 0) {
        const wantsDefault = selected === "" || selected === "-1";
        let matched = false;
        for (let i = 0; i < video.audioTracks.length; i += 1) {
          const track = video.audioTracks[i];
          const id = track && track.id ? String(track.id) : String(i);
          const isMatch = !wantsDefault && (id === selected || String(i) === selected);
          if (!wantsDefault) {
            track.enabled = isMatch;
          }
          if (isMatch) {
            matched = true;
          }
        }
        if (wantsDefault || !matched) {
          let anyEnabled = false;
          for (let i = 0; i < video.audioTracks.length; i += 1) {
            if (video.audioTracks[i].enabled) {
              anyEnabled = true;
              break;
            }
          }
          if (!anyEnabled) {
            video.audioTracks[0].enabled = true;
          }
        }
        updateAudioTrackOutputs();
        persistPrefsIfEnabled();
      }
      if (audioSourceOptions.queryParam && currentSource) {
        rewriteSourceForAudioTrack(selected);
        updateAudioTrackOutputs();
        persistPrefsIfEnabled();
      }
    }

    function sourceWithAudioTrack(source, selected) {
      const key = String(audioSourceOptions.queryParam || "").trim();
      if (!key) {
        return String(source || "");
      }
      try {
        const base = window.location && window.location.href ? window.location.href : "http://localhost/";
        const url = new URL(String(source || ""), base);
        if (selected === "" || selected === "-1") {
          url.searchParams.delete(key);
        } else {
          url.searchParams.set(key, selected);
        }
        if (!/^[a-z][a-z0-9+.-]*:/i.test(String(source || ""))) {
          return url.pathname + url.search + url.hash;
        }
        return url.toString();
      } catch (_error) {
        return String(source || "");
      }
    }

    async function rewriteSourceForAudioTrack(selected) {
      const nextSource = sourceWithAudioTrack(currentSource, selected);
      if (!nextSource || nextSource === currentSource) {
        return;
      }
      const wasPaused = sceneBool(video.paused, true);
      const position = Math.max(0, sceneNumber(video.currentTime, 0));
      await applySource(nextSource);
      try {
        if (position > 0) {
          video.currentTime = position;
        }
      } catch (_error) {
      }
      if (!wasPaused) {
        safePlay();
      }
    }

    // Slice 3: HLS quality levels (ABR rungs).
    function updateQualityLevelOutputs() {
      if (!hls || !Array.isArray(hls.levels)) {
        return;
      }
      const activeIndex = sceneNumber(hls.currentLevel, -1);
      const levels = hls.levels.map(function(item, index) {
        return videoNormalizeQualityLevel(item, index, activeIndex);
      });
      const signature = JSON.stringify(levels);
      if (signature !== lastQualityLevelsSignature) {
        lastQualityLevelsSignature = signature;
        writeVideoOutputSignal("qualityLevels", levels);
      }
      writeVideoOutputSignal("qualityLevel", activeIndex);
    }

    function recoverHLSFatalError(HlsCtor, data) {
      if (!hls || !data || !data.fatal) {
        return false;
      }
      const errorTypes = HlsCtor && HlsCtor.ErrorTypes || {};
      if (data.type === errorTypes.NETWORK_ERROR && typeof hls.startLoad === "function") {
        setError(data.details || "reopening video transport");
        hls.startLoad();
        updateVideoOutputs();
        return true;
      }
      if (data.type === errorTypes.MEDIA_ERROR && typeof hls.recoverMediaError === "function") {
        setError(data.details || "recovering video transport");
        hls.recoverMediaError();
        updateVideoOutputs();
        return true;
      }
      return false;
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

    function retryMutedPlay(error) {
      if (video.muted) {
        setError(error && error.message ? error.message : "playback failed");
        updateVideoOutputs();
        return Promise.resolve();
      }
      video.muted = true;
      if (typeof video.setAttribute === "function") {
        video.setAttribute("muted", "true");
      }
      updateVideoOutputs();
      try {
        const mutedResult = video.play();
        if (mutedResult && typeof mutedResult.then === "function") {
          return mutedResult.then(function() {
            clearError();
            updateVideoOutputs();
          }).catch(function(mutedError) {
            setError(mutedError && mutedError.message ? mutedError.message : "playback failed");
            updateVideoOutputs();
          });
        }
        clearError();
        updateVideoOutputs();
      } catch (mutedError) {
        setError(mutedError && mutedError.message ? mutedError.message : "playback failed");
        updateVideoOutputs();
      }
      return Promise.resolve();
    }

    function safePlay() {
      try {
        const result = video.play();
        if (result && typeof result.then === "function") {
          return result.then(function() {
            clearError();
            updateVideoOutputs();
          }).catch(function(error) {
            return retryMutedPlay(error);
          });
        }
        clearError();
      } catch (error) {
        return retryMutedPlay(error);
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

    function prefetchBitmapSubtitleImages(cues) {
      const limit = Math.max(0, subtitleOptions.bitmapPrefetchLimit);
      if (!limit || typeof Image !== "function" || !Array.isArray(cues)) {
        return;
      }
      const seen = new Set();
      let count = 0;
      for (const cue of cues) {
        const src = String(cue && cue.image && cue.image.src || "").trim();
        if (!src || seen.has(src)) {
          continue;
        }
        seen.add(src);
        try {
          const image = new Image();
          image.decoding = "async";
          image.src = src;
        } catch (_error) {
        }
        count += 1;
        if (count >= limit) {
          break;
        }
      }
    }

    async function refreshSubtitleCredentials(track) {
      const endpoint = subtitleOptions.refreshEndpoint;
      const callbackName = subtitleOptions.refreshCallback;
      if (callbackName && typeof window[callbackName] === "function") {
        await window[callbackName](videoSubtitleRefreshPayload(track, props, String((ctx && ctx.id) || "")));
        return true;
      }
      if (!endpoint) {
        return false;
      }
      const body = JSON.stringify(videoSubtitleRefreshPayload(track, props));
      const response = await fetch(endpoint, {
        method: "POST",
        credentials: "same-origin",
        cache: "no-store",
        headers: { "Content-Type": "application/json" },
        body,
      });
      return Boolean(response && response.ok);
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
      let refreshes = 0;
      for (let attempt = 0; attempt < subtitleOptions.retryLimit; attempt += 1) {
        if (!isCurrentLoad()) {
          return;
        }
        let response = null;
        try {
          response = await fetch(subtitleURL, { credentials: "same-origin", cache: "no-store" });
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
        if ((response.status === 401 || response.status === 403) && refreshes < Math.max(1, subtitleOptions.retryRefreshAfter || 1)) {
          refreshes += 1;
          subtitleState.status = "warming";
          updateSubtitleOutputs();
          try {
            if (await refreshSubtitleCredentials(localTrack)) {
              await waitMS(subtitleRetryDelayMS(response, 500));
              continue;
            }
          } catch (_error) {
          }
        }
        if (response.status === 202 || response.status === 425 || response.status === 429 || response.status === 503) {
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
        prefetchBitmapSubtitleImages(subtitleState.cues);
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
      hlsLiveFlag = false;
      lastQualityLevelsSignature = "";
      writeVideoOutputSignal("qualityLevels", []);
      writeVideoOutputSignal("qualityLevel", -1);
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
        // The server no longer emits a native src= for HLS playlists (see
        // server/video.go videoBaselineSrc), so the <video> has no source for
        // the browser to load natively — hls.js owns it. Do NOT set
        // video.src = "" here: in Firefox an empty src resolves to the page URL
        // and triggers MediaLoadInvalidURI.
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
              if (tracks.length === 0 && subtitleState.tracks.length > 0) {
                return;
              }
              subtitleState.tracks = tracks.map(videoNormalizeTrackInfo);
              updateSubtitleOutputs();
            });
          }
          if (HlsCtor.Events.ERROR) {
            hls.on(HlsCtor.Events.ERROR, function(_event, data) {
              if (data && data.fatal) {
                if (recoverHLSFatalError(HlsCtor, data)) {
                  return;
                }
                setError(data && data.details ? data.details : "video transport failed");
                updateVideoOutputs();
              }
            });
          }
          if (HlsCtor.Events.AUDIO_TRACKS_UPDATED) {
            hls.on(HlsCtor.Events.AUDIO_TRACKS_UPDATED, function() {
              updateAudioTrackOutputs();
              const requested = readVideoSignal("audioTrack", videoPropValue(props, ["audioTrack"], ""));
              if (String(requested || "").trim() !== "") {
                applyAudioTrackSelection(requested);
              }
            });
          }
          if (HlsCtor.Events.AUDIO_TRACK_SWITCHED) {
            hls.on(HlsCtor.Events.AUDIO_TRACK_SWITCHED, function() {
              updateAudioTrackOutputs();
            });
          }
          if (HlsCtor.Events.LEVELS_UPDATED) {
            hls.on(HlsCtor.Events.LEVELS_UPDATED, function() {
              updateQualityLevelOutputs();
              const requested = sceneNumber(readVideoSignal("qualityLevel", videoPropValue(props, ["qualityLevel"], -1)), -1);
              if (requested !== -1) {
                // See the "qualityLevel" subscribeVideoSignal handler below for
                // why nextLevel (not currentLevel) is used here.
                hls.nextLevel = requested;
              }
            });
          }
          if (HlsCtor.Events.LEVEL_SWITCHED) {
            hls.on(HlsCtor.Events.LEVEL_SWITCHED, function() {
              updateQualityLevelOutputs();
            });
          }
          if (HlsCtor.Events.LEVEL_LOADED) {
            hls.on(HlsCtor.Events.LEVEL_LOADED, function(_event, data) {
              hlsLiveFlag = Boolean(data && data.details && data.details.live);
              updateVideoOutputs();
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
    if (videoInputLockActive() && typeof video.removeAttribute === "function") {
      // lockInput (Slice 5) suppresses native controls so they cannot fight
      // the swallowed click/dblclick/keyboard transport interaction below.
      video.removeAttribute("controls");
    }
    videoEnsureAuthoredChildren(video, props);
    syncNativeSubtitleTrackMode();
    subtitleState.status = subtitleState.tracks.length > 0 ? "ready" : "idle";
    readInitialVideoCacheState();
    updateSubtitleOutputs();
    writeVideoOutputSignal("activeCues", []);
    writeVideoOutputSignal("syncConnected", false);
    writeVideoOutputSignal("error", "");
    writeVideoOutputSignal("audioTracks", []);
    writeVideoOutputSignal("qualityLevels", []);
    writeVideoOutputSignal("qualityLevel", -1);

    addListener(video, "timeupdate", function() {
      updateVideoOutputs();
      sendLeadSnapshot(false);
    });
    addListener(video, "durationchange", updateVideoOutputs);
    addListener(video, "loadedmetadata", updateVideoOutputs);
    addListener(video, "progress", updateVideoOutputs);
    addListener(video, "loadedmetadata", function() {
      updateAudioTrackOutputs();
      const requested = readVideoSignal("audioTrack", videoPropValue(props, ["audioTrack"], ""));
      if (String(requested || "").trim() !== "") {
        applyAudioTrackSelection(requested);
      }
    });
    if (video.audioTracks && typeof video.audioTracks.addEventListener === "function") {
      addListener(video.audioTracks, "addtrack", updateAudioTrackOutputs);
      addListener(video.audioTracks, "removetrack", updateAudioTrackOutputs);
      addListener(video.audioTracks, "change", updateAudioTrackOutputs);
    }
    if (videoInputLockActive()) {
      // Swallow click/dblclick/space/Enter transport interaction on the
      // <video> element itself, in the capture phase so it preempts the
      // browser's own default click-to-toggle-play / keyboard shortcuts.
      addListener(video, "click", function(event) {
        if (videoInputLockActive() && event) {
          event.preventDefault();
          event.stopPropagation();
        }
      }, true);
      addListener(video, "dblclick", function(event) {
        if (videoInputLockActive() && event) {
          event.preventDefault();
          event.stopPropagation();
        }
      }, true);
      addListener(video, "keydown", function(event) {
        if (videoInputLockActive() && videoInputLockBlocksKey(event)) {
          event.preventDefault();
          event.stopPropagation();
        }
      }, true);
    }
    addListener(video, "canplay", function() {
      stalled = false;
      clearTelemetryStallTimer();
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
      clearTelemetryStallTimer();
      clearError();
      markInteractionActive(1800);
      syncBrainPlaybackStart();
      jsBrainPlaybackStart();
      updateVideoOutputs();
      sendLeadSnapshot(true);
    });
    addListener(video, "pause", function() {
      stalled = false;
      clearTelemetryStallTimer();
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
      scheduleTelemetryStallRecovery("waiting");
      updateVideoOutputs();
    });
    addListener(video, "stalled", function() {
      stalled = true;
      markInteractionActive(0);
      renderSyncOverlay();
      scheduleTelemetryStallRecovery("stalled");
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
    addListener(document, "webkitfullscreenchange", function() {
      refreshVideoViewportOutput();
      updateVideoOutputs();
    });
    addListener(video, "enterpictureinpicture", syncNativeSubtitleTrackMode);
    addListener(video, "leavepictureinpicture", syncNativeSubtitleTrackMode);
    addListener(video, "enterpictureinpicture", updateVideoOutputs);
    addListener(video, "leavepictureinpicture", updateVideoOutputs);

    if (typeof ResizeObserver === "function") {
      resizeObserver = new ResizeObserver(function() {
        refreshVideoViewportOutput();
        updateVideoOutputs();
      });
      resizeObserver.observe(mount);
    }

    unsubscribers.push(subscribeVideoSignal("src", function(value) {
      if (!sourceSignalInitialized) {
        sourceSignalInitialized = true;
        if (value == null) {
          return;
        }
      }
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
      } else if (command === "enter-fullscreen") {
        requestVideoFullscreen();
      } else if (command === "toggle-fullscreen") {
        if (videoIsFullscreen()) {
          exitVideoFullscreen();
        } else {
          requestVideoFullscreen();
        }
      } else if (command === "exit-fullscreen") {
        exitVideoFullscreen();
      } else if (command === "enter-pip") {
        enterPiP();
      } else if (command === "exit-pip") {
        exitPiP();
      } else if (command === "toggle-pip") {
        if (videoIsPoppedOut()) {
          exitPiP();
        } else {
          enterPiP();
        }
      }
      updateVideoOutputs();
    }));
    unsubscribers.push(subscribeVideoSignal("volume", function(value) {
      const volume = Math.max(0, Math.min(1, sceneNumber(value, sceneNumber(video.volume, 1))));
      video.volume = volume;
      updateVideoOutputs();
      persistPrefsIfEnabled();
    }));
    unsubscribers.push(subscribeVideoSignal("mute", function(value) {
      video.muted = sceneBool(value, Boolean(video.muted));
      updateVideoOutputs();
      persistPrefsIfEnabled();
    }));
    unsubscribers.push(subscribeVideoSignal("rate", function(value) {
      requestedRate = Math.max(0.1, sceneNumber(value, requestedRate));
      const mode = String(videoPropValue(props, ["syncMode", "sync_mode"], "follow")).trim().toLowerCase();
      if (!(mode === "follow" && String(videoPropValue(props, ["sync"], "")).trim() !== "")) {
        applyRequestedRate();
      }
      persistPrefsIfEnabled();
    }));
    unsubscribers.push(subscribeVideoSignal("subtitleTrack", function(value) {
      startSubtitleLoad(value);
      persistPrefsIfEnabled();
    }));
    unsubscribers.push(subscribeVideoSignal("subtitleOffsetMs", refreshSubtitleOptionsFromSignals));
    unsubscribers.push(subscribeVideoSignal("subtitleScale", refreshSubtitleOptionsFromSignals));
    unsubscribers.push(subscribeVideoSignal("subtitleStyle", refreshSubtitleOptionsFromSignals));
    unsubscribers.push(subscribeVideoSignal("audioTrack", function(value) {
      applyAudioTrackSelection(value);
    }));
    unsubscribers.push(subscribeVideoSignal("qualityLevel", function(value) {
      if (!hls) {
        return;
      }
      const requested = sceneNumber(value, -1);
      // Use nextLevel (not currentLevel): hls.js's currentLevel setter calls
      // immediateLevelSwitch(), which flushes the already-buffered-ahead
      // media and forces an immediate re-buffer/stall. nextLevel calls
      // nextLevelSwitch() instead, which only takes effect at the next
      // fragment boundary — no flush, no visible stall, at the cost of the
      // switch not being instantaneous. That trade-off is the right default
      // for a user-driven "change quality" action.
      hls.nextLevel = requested;
      updateQualityLevelOutputs();
      persistPrefsIfEnabled();
    }));

    const initialVolume = Math.max(0, Math.min(1, sceneNumber(readVideoSignal("volume", videoPropValue(props, ["volume"], 1)), 1)));
    video.volume = initialVolume;
    video.muted = sceneBool(readVideoSignal("mute", videoPropValue(props, ["muted"], false)), false);
    video.playbackRate = requestedRate;
    startVideoQualityTelemetry();
    refreshVideoViewportOutput();
    updateVideoOutputs();

    const initialSource = readVideoSignal("src", videoPropValue(props, ["src", "Src"], ""));
    await applySource(initialSource);
    videoClearChildren(mount);
    mount.appendChild(video);
    mount.appendChild(ensureSubtitleOverlay());
    mount.appendChild(ensureSyncOverlay());
    refreshVideoViewportOutput();
    updateVideoOutputs();

    return {
      video,
      dispose() {
        disposed = true;
        clearInteractionTimer();
        clearTelemetryStallTimer();
        if (telemetryQualityTimer) {
          clearInterval(telemetryQualityTimer);
          telemetryQualityTimer = 0;
        }
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
            entry.target.removeEventListener(entry.type, entry.listener, entry.options);
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

  async function mountEngine(entry, preflightError) {
    const existing = window.__gosx.engines.get(entry.id);
    if (existing) {
      window.__gosx_dispose_engine(entry.id);
    }
    const existingPending = pendingEngineRuntimes.get(entry.id);
    if (existingPending) {
      disposePendingEngine(existingPending, true);
    }

    const mount = resolveEngineMount(entry);
    if (engineKindNeedsMount(entry.kind) && !mount) return;
    if (preflightError) {
      reportGoWASMEngineModuleFailure(entry, mount, preflightError);
      return;
    }

    // Claim this engine id and page generation before the first asynchronous
    // capability probe. A dispose/rebootstrap can then revoke the claim while
    // the probe is pending instead of allowing the older mount to publish into
    // the new page generation.
    const pending = {
      id: entry.id,
      generation: goWASMEnginePageGeneration,
      runtime: null,
      mount,
      fallbackSnapshot: engineUsesGoWASMRuntime(entry) ? snapshotGoWASMEngineFallback(mount) : null,
      moduleWaiter: null,
      moduleRecord: null,
      runtimeDisposed: false,
      closed: false,
    };
    pendingEngineRuntimes.set(entry.id, pending);
    await prepareRuntimeCapabilityProbe(entry);
    if (!pendingEngineOwned(pending)) {
      disposePendingEngine(pending, true);
      return;
    }
    const capabilityStatus = engineCapabilityStatus(entry);
    applyRuntimeCapabilityState(mount, "engine", capabilityStatus);
    if (!capabilityStatus.ok) {
      disposePendingEngine(pending, true);
      showEngineCapabilityUnsupported(mount, entry, capabilityStatus);
      reportMissingEngineCapabilities(entry, mount, capabilityStatus);
      return;
    }
    const runtime = createEngineRuntime(entry, mount);
    pending.runtime = runtime;
    const ctx = createEngineContext(entry, mount, runtime, capabilityStatus);
    if (entry.props && entry.props.audio && window.__gosx && window.__gosx.audio && typeof window.__gosx.audio.registerManifest === "function") {
      window.__gosx.audio.registerManifest(entry.props.audio);
    }
    let factory;
    let moduleRecord = null;
    try {
      const resolved = await (engineUsesGoWASMRuntime(entry)
        ? resolveGoWASMEngineFactory(entry, pending)
        : { factory: resolveEngineFactory(entry), moduleRecord: null });
      factory = resolved.factory;
      moduleRecord = resolved.moduleRecord;
    } catch (e) {
      const owned = pendingEngineOwned(pending);
      disposePendingEngine(pending, true);
      if (!owned) return;
      reportGoWASMEngineModuleFailure(entry, mount, e);
      return;
    }
    if (!pendingEngineOwned(pending)) {
      disposePendingEngine(pending, true);
      return;
    }
    if (typeof factory !== "function") {
      disposePendingEngine(pending, true);
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
      if (!pendingEngineOwned(pending)) {
        if (mounted.handle && typeof mounted.handle.dispose === "function") {
          try {
            mounted.handle.dispose();
          } catch (disposeError) {
            console.error("[gosx] dispose error for stale engine " + entry.id + ":", disposeError);
          }
        }
        disposePendingEngine(pending, true);
        return;
      }
      if (moduleRecord && moduleRecord.state !== "ready") {
        if (mounted.handle && typeof mounted.handle.dispose === "function") {
          mounted.handle.dispose();
        }
        throw moduleRecord.error || goWASMEngineError(
          "go-wasm-module-unavailable",
          "Go-WASM engine module became unavailable before mount publication",
          entry,
        );
      }
      if (!transferPendingEngine(pending)) {
        if (mounted.handle && typeof mounted.handle.dispose === "function") mounted.handle.dispose();
        disposePendingEngine(pending, true);
        return;
      }
      rememberMountedEngine(entry, mount, mounted.context, mounted.handle, moduleRecord, pending.fallbackSnapshot);
    } catch (e) {
      const owned = pendingEngineOwned(pending);
      disposePendingEngine(pending, true);
      if (!owned) return;
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

  function rememberMountedEngine(entry, mount, context, handle, moduleRecord, fallbackSnapshot) {
    if (window.__gosx && typeof window.__gosx.clearIssueState === "function") {
      window.__gosx.clearIssueState(mount);
    }
    activateInputProviders(entry);
    const record = {
      component: entry.component,
      kind: entry.kind,
      capabilities: capabilityList(entry),
      requiredCapabilities: requiredCapabilityList(entry),
      capabilityStatus: context.capabilityStatus || engineCapabilityStatus(entry),
      runtime: context.runtime,
      mount: mount,
      handle: handle,
      moduleRecord,
      fallbackSnapshot,
      disposed: false,
    };
    window.__gosx.engines.set(entry.id, record);
    if (moduleRecord) moduleRecord.mountedIDs.add(entry.id);
  }

  // The manifest of the most recent mountAllEngines call, kept for the
  // late-factory hook below. Bundles do not share the runtime's
  // pendingManifest closure variable, so this module records its own copy.
  let lastMountManifest = null;

  // A feature chunk can register its engine factory AFTER bootstrap already
  // tried to mount (a cold cache plus a slow network loses the race against
  // DOMContentLoaded). The post-init registrar in the bootstrap head calls
  // this hook after storing a late factory: mount every manifest engine of
  // that component that has no live record and no pending claim.
  // mountEngine owns takeover semantics (it revokes an existing record and
  // pending claim on entry), so a duplicate trigger converges to one
  // mounted engine. A stale manifest after navigation is harmless: the
  // entries' mounts are gone and mountEngine returns without them.
  window.__gosx_mount_late_engine_factory = function(name) {
    const manifest = lastMountManifest;
    if (!manifest || !Array.isArray(manifest.engines)) return;
    for (const entry of manifest.engines) {
      if (!entry || typeof entry !== "object") continue;
      if (engineExportName(entry) !== name) continue;
      const engineID = String(entry.id || "");
      if (!engineID) continue;
      if (window.__gosx.engines.has(engineID)) continue;
      if (pendingEngineRuntimes.has(engineID)) continue;
      console.info("[gosx] mounting engine after late factory registration:", engineID);
      mountEngine(entry).catch(function(err) {
        console.error("[gosx] late engine mount failed for " + engineID + ":", err);
      });
    }
  };

  async function mountAllEngines(manifest, reuseEngineIDs, isNavigationBootstrap) {
    lastMountManifest = manifest;
    if (!manifest.engines || manifest.engines.length === 0) return;
    // isNavigationBootstrap is true only when this bootstrap is running as
    // part of a soft navigation (see bootstrapPage/window.__gosx_reusable_engines).
    // It must come from the caller, computed from bootstrapPage's ORIGINAL
    // argument — reuseEngineIDs itself is always a real Set by the time it
    // reaches here (pendingEngineReuseIDs coerces a missing/non-Set argument
    // to an empty Set so downstream .has() calls never need a type check),
    // so a `reuseEngineIDs instanceof Set` check at this point is always
    // true, even on a genuine first page load. isNavigationBootstrap
    // therefore also gates the "engine-remounted" telemetry below: a
    // freshly mounted engine on the FIRST page load was never "re"-mounted.
    const reuseIDs = isNavigationBootstrap && reuseEngineIDs instanceof Set ? reuseEngineIDs : new Set();

    const invalidEntries = new Map();
    const seenIDs = new Set();
    for (const entry of manifest.engines) {
      if (!entry || typeof entry !== "object") continue;
      const engineID = String(entry.id || "").trim();
      if (seenIDs.has(engineID) && engineID) {
        invalidEntries.set(entry, goWASMEngineError(
          "go-wasm-engine-id-duplicate",
          "engine id is duplicated: " + engineID,
          entry,
        ));
        continue;
      }
      if (engineID) seenIDs.add(engineID);
      const validationError = validateGoWASMEngineEntry(entry);
      if (validationError) invalidEntries.set(entry, validationError);
    }

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

    const promises = manifest.engines.filter(function(entry) {
      return entry && typeof entry === "object" &&
        (entry.kind !== "video" || videoEngines.indexOf(entry) === 0);
    }).map(function(entry) {
      const engineID = String((entry && entry.id) || "");
      if (engineID && reuseIDs.has(engineID)) {
        // Carried across the navigation — window.__gosx_dispose_page already
        // skipped disposing it (and reported engine-reused-across-navigation)
        // and replaceBody moved its live mount element into the new body
        // unchanged, so there is nothing to (re)mount here.
        return Promise.resolve();
      }
      const promise = mountEngine(entry, invalidEntries.get(entry) || null).catch(function(e) {
        console.error(`[gosx] unexpected error mounting engine ${entry.id}:`, e);
      });
      if (isNavigationBootstrap && engineID) {
        promise.then(function() {
          if (typeof window !== "undefined" && typeof window.__gosx_emit === "function") {
            window.__gosx_emit("info", "engine", "engine-remounted", {
              engineID,
              component: String((entry && entry.component) || ""),
            });
          }
        });
      }
      return promise;
    });

    await Promise.all(promises);
  }
