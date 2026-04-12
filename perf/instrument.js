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
    frameCount: 0
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
    return result;
  };

})();
