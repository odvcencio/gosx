// GoSX Performance Instrumentation — injected by gosx perf via CDP
// Page.addScriptToEvaluateOnNewDocument. Runs BEFORE the page's own
// scripts on every navigation.
//
// Because this script runs first, the GoSX runtime globals don't exist
// yet. We use Object.defineProperty traps to intercept assignments and
// wrap them with performance marks at assignment time.

(function() {
  "use strict";

  var perf = window.__gosx_perf = {
    ready: false,
    firstFrame: false,
    dispatchLog: [],
    hydrationLog: [],
    hubMessageCount: 0,
    hubMessageBytes: 0,
    hubSendCount: 0,
    frameCount: 0,
    longTasks: [],        // {name, duration, startTime}
    largestContentfulPaint: 0,
    cumulativeLayoutShift: 0,
    firstInputDelay: 0,
    signalWrites: 0,
    signalReads: 0
  };

  // --- 1. Bridge gosx:ready CustomEvent → performance mark ---
  document.addEventListener("gosx:ready", function() {
    performance.mark("gosx:ready");
  }, { once: true });

  // --- 2. Enable Scene3D frame instrumentation ---
  window.__gosx_scene3d_perf = true;

  // --- 3. Observe scene frames + detect first frame ---
  try {
    var sceneObserver = new PerformanceObserver(function(list) {
      var entries = list.getEntries();
      for (var i = 0; i < entries.length; i++) {
        if (entries[i].name === "scene3d-render") {
          perf.frameCount++;
          if (!perf.firstFrame) {
            perf.firstFrame = true;
            performance.mark("gosx:scene:first-frame");
          }
        }
      }
    });
    sceneObserver.observe({ type: "measure", buffered: true });
  } catch (_e) {}

  // --- 3a. Long task observer (tasks > 50ms that block main thread) ---
  // This is the single most valuable signal for scroll jank diagnosis:
  // any main-thread task > 50ms will cause dropped frames during scroll.
  try {
    var longTaskObserver = new PerformanceObserver(function(list) {
      var entries = list.getEntries();
      for (var i = 0; i < entries.length; i++) {
        var e = entries[i];
        perf.longTasks.push({
          name: e.name || "unknown",
          duration: e.duration,
          startTime: e.startTime
        });
      }
    });
    longTaskObserver.observe({ type: "longtask", buffered: true });
  } catch (_e) {}

  // --- 3b. Core Web Vitals: Largest Contentful Paint ---
  try {
    var lcpObserver = new PerformanceObserver(function(list) {
      var entries = list.getEntries();
      // LCP can update multiple times; use the latest entry.
      var last = entries[entries.length - 1];
      if (last && last.startTime > perf.largestContentfulPaint) {
        perf.largestContentfulPaint = last.startTime;
      }
    });
    lcpObserver.observe({ type: "largest-contentful-paint", buffered: true });
  } catch (_e) {}

  // --- 3c. Core Web Vitals: Cumulative Layout Shift ---
  try {
    var clsObserver = new PerformanceObserver(function(list) {
      var entries = list.getEntries();
      for (var i = 0; i < entries.length; i++) {
        var e = entries[i];
        // Ignore shifts triggered by user input
        if (!e.hadRecentInput) {
          perf.cumulativeLayoutShift += e.value;
        }
      }
    });
    clsObserver.observe({ type: "layout-shift", buffered: true });
  } catch (_e) {}

  // --- 3d. Core Web Vitals: First Input Delay ---
  try {
    var fidObserver = new PerformanceObserver(function(list) {
      var entries = list.getEntries();
      if (entries.length > 0 && perf.firstInputDelay === 0) {
        var first = entries[0];
        perf.firstInputDelay = first.processingStart - first.startTime;
      }
    });
    fidObserver.observe({ type: "first-input", buffered: true });
  } catch (_e) {}

  // --- 4. Trap __gosx_runtime_ready assignment ---
  // The bootstrap JS assigns this as a function. The WASM bridge later
  // calls it. We intercept via a property trap so we can wrap it at
  // assignment time, before it's invoked.
  var _capturedReadyHandler = null;
  Object.defineProperty(window, "__gosx_runtime_ready", {
    set: function(fn) {
      _capturedReadyHandler = fn;
    },
    get: function() {
      // Return a wrapper that calls the original + patches WASM exports.
      return function() {
        if (typeof _capturedReadyHandler === "function") {
          _capturedReadyHandler.apply(this, arguments);
        }
        // At this point WASM exports (__gosx_hydrate, __gosx_action, etc.)
        // have been registered via js.Global().Set(). Wrap them now.
        _wrapWasmExports();
        perf.ready = true;
        performance.mark("gosx:perf:ready");
      };
    },
    configurable: true
  });

  // --- 5. Trap __gosx_hydrate assignment ---
  // The WASM bridge sets this via js.Global().Set() before calling
  // __gosx_runtime_ready. But just in case the order varies, we also
  // trap the assignment so wrapping happens regardless of timing.
  var _origHydrate = null;
  var _hydrateWrapped = false;
  Object.defineProperty(window, "__gosx_hydrate", {
    set: function(fn) { _origHydrate = fn; _hydrateWrapped = false; },
    get: function() {
      if (!_origHydrate) return undefined;
      if (_hydrateWrapped) return _origHydrate;
      // Return instrumented version
      return function(islandID) {
        var startMark = "gosx:island:hydrate:start:" + islandID;
        var endMark = "gosx:island:hydrate:end:" + islandID;
        var measureName = "gosx:island:hydrate:" + islandID;
        performance.mark(startMark);
        var result = _origHydrate.apply(this, arguments);
        performance.mark(endMark);
        var m = performance.measure(measureName, startMark, endMark);
        perf.hydrationLog.push({ id: String(islandID), ms: m.duration });
        return result;
      };
    },
    configurable: true
  });

  // --- 6. Trap __gosx_action assignment ---
  var _origAction = null;
  Object.defineProperty(window, "__gosx_action", {
    set: function(fn) { _origAction = fn; },
    get: function() {
      if (!_origAction) return undefined;
      return function(islandID, handlerName, eventData) {
        var startMark = "gosx:dispatch:start:" + islandID + ":" + handlerName;
        var endMark = "gosx:dispatch:end:" + islandID + ":" + handlerName;
        var measureName = "gosx:dispatch:" + islandID + ":" + handlerName;
        performance.mark(startMark);
        var result = _origAction.apply(this, arguments);
        performance.mark(endMark);
        var m = performance.measure(measureName, startMark, endMark);
        var patchCount = 0;
        try { patchCount = JSON.parse(result).length; } catch (_e) {}
        perf.dispatchLog.push({
          island: String(islandID),
          handler: String(handlerName),
          ms: m.duration,
          patches: patchCount
        });
        return result;
      };
    },
    configurable: true
  });

  // --- 7. Trap __gosx_hydrate_engine assignment ---
  var _origHydrateEngine = null;
  Object.defineProperty(window, "__gosx_hydrate_engine", {
    set: function(fn) { _origHydrateEngine = fn; },
    get: function() {
      if (!_origHydrateEngine) return undefined;
      return function(engineName) {
        var startMark = "gosx:engine:mount:start:" + engineName;
        var endMark = "gosx:engine:mount:end:" + engineName;
        var measureName = "gosx:engine:mount:" + engineName;
        performance.mark(startMark);
        var result = _origHydrateEngine.apply(this, arguments);
        performance.mark(endMark);
        performance.measure(measureName, startMark, endMark);
        return result;
      };
    },
    configurable: true
  });

  // Fallback: if __gosx_runtime_ready was never trapped (older bootstrap),
  // try to wrap exports directly if they exist.
  function _wrapWasmExports() {
    // No-op — wrapping happens via the property traps above. This function
    // exists as a hook point for the __gosx_runtime_ready getter.
  }

  // --- 8. Hub message instrumentation ---
  var origOnMessageDesc = Object.getOwnPropertyDescriptor(
    WebSocket.prototype, "onmessage"
  );
  if (origOnMessageDesc && origOnMessageDesc.set) {
    Object.defineProperty(WebSocket.prototype, "onmessage", {
      set: function(handler) {
        if (typeof handler !== "function") {
          origOnMessageDesc.set.call(this, handler);
          return;
        }
        var ws = this;
        var wrapped = function(evt) {
          performance.mark("gosx:hub:message:start");
          handler.call(ws, evt);
          performance.mark("gosx:hub:message:end");
          performance.measure("gosx:hub:message",
            "gosx:hub:message:start", "gosx:hub:message:end");
          perf.hubMessageCount++;
          if (evt && evt.data) {
            perf.hubMessageBytes += (typeof evt.data === "string" ? evt.data.length : (evt.data.byteLength || 0));
          }
        };
        origOnMessageDesc.set.call(this, wrapped);
      },
      get: function() {
        return origOnMessageDesc.get.call(this);
      },
      configurable: true
    });
  }

  // --- 9. Hub send instrumentation ---
  var origSend = WebSocket.prototype.send;
  WebSocket.prototype.send = function(data) {
    performance.mark("gosx:hub:send:start");
    var result = origSend.call(this, data);
    performance.mark("gosx:hub:send:end");
    performance.measure("gosx:hub:send",
      "gosx:hub:send:start", "gosx:hub:send:end");
    perf.hubSendCount++;
    return result;
  };

  // --- 10. Signal throughput counters ---
  // Wrap the shared signal setter/getter to count per-dispatch signal
  // operations. This helps diagnose whether a slow dispatch is signal-
  // bound (too many writes) or reconcile-bound (too many DOM diffs).
  var _origSet = null;
  Object.defineProperty(window, "__gosx_set_shared_signal", {
    set: function(fn) { _origSet = fn; },
    get: function() {
      if (!_origSet) return undefined;
      return function(name, valueJSON) {
        perf.signalWrites++;
        return _origSet.apply(this, arguments);
      };
    },
    configurable: true
  });

  var _origGet = null;
  Object.defineProperty(window, "__gosx_get_shared_signal", {
    set: function(fn) { _origGet = fn; },
    get: function() {
      if (!_origGet) return undefined;
      return function(name) {
        perf.signalReads++;
        return _origGet.apply(this, arguments);
      };
    },
    configurable: true
  });

  // --- 11. GPU context introspection (WebGPU + WebGL2 + WebGL1) ---
  // Reports which GPU tier the engine is actually using and what the
  // browser supports, so the report can flag when a better tier is
  // available but unused.
  window.__gosx_perf_webgl_info = function() {
    // Browser-level capability detection — what the browser can provide,
    // independent of what any canvas selected.
    var caps = {
      webgpuAvailable: typeof navigator !== "undefined" && !!navigator.gpu,
      webgl2Available: false,
      webgl1Available: false
    };
    try {
      var probe = document.createElement("canvas");
      caps.webgl2Available = !!probe.getContext("webgl2");
    } catch (_e) {}
    try {
      var probe2 = document.createElement("canvas");
      caps.webgl1Available = !!(probe2.getContext("webgl") || probe2.getContext("experimental-webgl"));
    } catch (_e) {}

    // Find the engine's canvas. Try known mount attributes first.
    var canvases = document.querySelectorAll("canvas");
    for (var i = 0; i < canvases.length; i++) {
      var c = canvases[i];

      // Engine-provided tier hint (preferred — doesn't alter context type).
      var hint = c.__gosx_scene_tier || c.getAttribute("data-gosx-scene-tier") || null;
      if (hint === "webgpu") {
        return {
          tier: "webgpu",
          version: "WebGPU",
          vendor: "",
          renderer: "(WebGPU canvas — details not available via DOM)",
          maxTextureSize: 0,
          caps: caps
        };
      }

      // No hint — try reading existing WebGL context. getContext with the
      // same type returns the existing context. Using a DIFFERENT type on
      // a canvas that already has a context returns null without disturbing
      // the existing context.
      var ctx = null;
      var contextType = "";
      try {
        ctx = c.getContext("webgl2");
        if (ctx) contextType = "webgl2";
      } catch (_e) {}
      if (!ctx) {
        try {
          ctx = c.getContext("webgl");
          if (ctx) contextType = "webgl1";
        } catch (_e) {}
      }
      if (!ctx) {
        // Canvas has a non-WebGL context (WebGPU or 2d). If hint wasn't
        // set, we can't know for sure — guess WebGPU if available.
        continue;
      }

      // Guard against null/invalid context state. getContextAttributes()
      // can return null on lost contexts or unusual canvas states.
      var attrs = {};
      try {
        var raw = ctx.getContextAttributes();
        if (raw) attrs = raw;
      } catch (_e) {}

      var dbg = null;
      try { dbg = ctx.getExtension("WEBGL_debug_renderer_info"); } catch (_e) {}

      var info = {
        tier: contextType,
        version: "",
        shadingLanguageVersion: "",
        vendor: "",
        renderer: "",
        maxTextureSize: 0,
        maxCubeMapSize: 0,
        maxRenderbufferSize: 0,
        maxVertexAttribs: 0,
        maxCombinedTextureImageUnits: 0,
        antialiasing: !!attrs.antialias,
        preserveDrawingBuffer: !!attrs.preserveDrawingBuffer,
        extensions: [],
        caps: caps
      };

      try { info.version = ctx.getParameter(ctx.VERSION) || ""; } catch (_e) {}
      try { info.shadingLanguageVersion = ctx.getParameter(ctx.SHADING_LANGUAGE_VERSION) || ""; } catch (_e) {}
      try {
        // Use || "" fallback — getParameter can return null on lost contexts
        // or when the debug extension is partially exposed, and we want empty
        // strings (not JSON null) so the Go report and software-GPU detection
        // downstream always have a string to pattern-match on.
        info.vendor = (dbg ? ctx.getParameter(dbg.UNMASKED_VENDOR_WEBGL) : ctx.getParameter(ctx.VENDOR)) || "";
      } catch (_e) {}
      try {
        info.renderer = (dbg ? ctx.getParameter(dbg.UNMASKED_RENDERER_WEBGL) : ctx.getParameter(ctx.RENDERER)) || "";
      } catch (_e) {}
      try { info.maxTextureSize = ctx.getParameter(ctx.MAX_TEXTURE_SIZE) || 0; } catch (_e) {}
      try { info.maxCubeMapSize = ctx.getParameter(ctx.MAX_CUBE_MAP_TEXTURE_SIZE) || 0; } catch (_e) {}
      try { info.maxRenderbufferSize = ctx.getParameter(ctx.MAX_RENDERBUFFER_SIZE) || 0; } catch (_e) {}
      try { info.maxVertexAttribs = ctx.getParameter(ctx.MAX_VERTEX_ATTRIBS) || 0; } catch (_e) {}
      try { info.maxCombinedTextureImageUnits = ctx.getParameter(ctx.MAX_COMBINED_TEXTURE_IMAGE_UNITS) || 0; } catch (_e) {}
      try { info.extensions = ctx.getSupportedExtensions() || []; } catch (_e) {}

      // If the existing canvas's context is lost/stale (all queries returned
      // empty), probe a FRESH canvas to get real GPU info. This happens when
      // Scene3D has failed mid-initialization — the canvas still advertises
      // webgl2 but getParameter returns null for everything. A fresh canvas
      // gets a clean context straight from the browser/driver so we can
      // still report renderer/vendor for software-GPU detection and general
      // diagnostics.
      if (!info.renderer && !info.version && !info.maxTextureSize) {
        try {
          var fresh = document.createElement("canvas");
          fresh.width = 1;
          fresh.height = 1;
          var freshCtx = fresh.getContext("webgl2") || fresh.getContext("webgl");
          if (freshCtx) {
            var freshDbg = null;
            try { freshDbg = freshCtx.getExtension("WEBGL_debug_renderer_info"); } catch (_e) {}
            try { info.version = freshCtx.getParameter(freshCtx.VERSION) || info.version; } catch (_e) {}
            try { info.shadingLanguageVersion = freshCtx.getParameter(freshCtx.SHADING_LANGUAGE_VERSION) || info.shadingLanguageVersion; } catch (_e) {}
            try {
              info.vendor = (freshDbg ? freshCtx.getParameter(freshDbg.UNMASKED_VENDOR_WEBGL) : freshCtx.getParameter(freshCtx.VENDOR)) || info.vendor;
            } catch (_e) {}
            try {
              info.renderer = (freshDbg ? freshCtx.getParameter(freshDbg.UNMASKED_RENDERER_WEBGL) : freshCtx.getParameter(freshCtx.RENDERER)) || info.renderer;
            } catch (_e) {}
            try { info.maxTextureSize = freshCtx.getParameter(freshCtx.MAX_TEXTURE_SIZE) || info.maxTextureSize; } catch (_e) {}
            try { info.maxCubeMapSize = freshCtx.getParameter(freshCtx.MAX_CUBE_MAP_TEXTURE_SIZE) || info.maxCubeMapSize; } catch (_e) {}
            try { info.maxRenderbufferSize = freshCtx.getParameter(freshCtx.MAX_RENDERBUFFER_SIZE) || info.maxRenderbufferSize; } catch (_e) {}
            try { info.maxVertexAttribs = freshCtx.getParameter(freshCtx.MAX_VERTEX_ATTRIBS) || info.maxVertexAttribs; } catch (_e) {}
            try { info.maxCombinedTextureImageUnits = freshCtx.getParameter(freshCtx.MAX_COMBINED_TEXTURE_IMAGE_UNITS) || info.maxCombinedTextureImageUnits; } catch (_e) {}
            try {
              var freshExts = freshCtx.getSupportedExtensions() || [];
              if (freshExts.length > 0) info.extensions = freshExts;
            } catch (_e) {}
            // Mark that the existing canvas had a stale context — useful diagnostic.
            info.staleExistingContext = true;
          }
        } catch (_e) {}
      }

      return info;
    }

    // No canvas matched WebGL — check if any canvas exists and WebGPU is
    // available (likely using WebGPU since we couldn't attach a WebGL ctx).
    if (canvases.length > 0 && caps.webgpuAvailable) {
      return {
        tier: "webgpu",
        version: "WebGPU",
        vendor: "",
        renderer: "(detected by elimination — no WebGL context on canvas)",
        maxTextureSize: 0,
        caps: caps
      };
    }

    return { tier: "none", caps: caps, version: "", vendor: "", renderer: "", maxTextureSize: 0 };
  };

})();
