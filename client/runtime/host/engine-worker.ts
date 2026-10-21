// @ts-check
// GoSX engine worker host. One file, two halves:
//
//   Page half   Loads as the "engine-worker" runtime feature
//               (bootstrap-feature-engine-worker.js) on a page that has an
//               engine with thread "worker". It starts a dedicated Worker from
//               this same file, relays the engine's calls to the page runtime
//               (signals, Scene3D, navigation, events) and applies binary
//               frames the worker sends.
//
//   Worker half Runs when this file is the Worker script. It loads the
//               standard-Go wasm_exec.js and the engine's Go module, builds the
//               engine context the Go code expects, and gives Go a port to the
//               page (self.__gosx.worker) plus worker-safe stand-ins for the
//               page APIs Go calls (self.__gosx.scene3d, self.__gosx.netplay).
//
// Applications write no JavaScript: Go opts in with engine.Config{Runtime:
// engine.RuntimeGoWASM, Thread: engine.ThreadWorker}. See docs/engine-worker.md
// for the message protocol.
(function() {
  "use strict";

  var PROTOCOL = 1;
  var DEFAULT_FRAME_HZ = 60;
  var SHUTDOWN_GRACE_MS = 250;

  if (typeof window === "undefined") {
    if (typeof self !== "undefined" && typeof self.postMessage === "function") workerMain(self);
    return;
  }
  pageMain();

  // ==========================================================================
  // Page half
  // ==========================================================================

  function pageMain() {
    var scriptURL = "";
    try { scriptURL = (document.currentScript && document.currentScript.src) || ""; } catch (_) { scriptURL = ""; }

    var registerFeature = window.__gosx_register_bootstrap_feature;
    if (typeof registerFeature !== "function") {
      // The feature can execute before the runtime bundle; the runtime adopts
      // window.__gosx_bootstrap_features when it boots.
      var shared = window.__gosx_bootstrap_features;
      if (!shared) {
        shared = Object.create(null);
        Object.defineProperty(window, "__gosx_bootstrap_features", { value: shared, configurable: true, writable: true });
      }
      registerFeature = function(name, factory) { shared[name] = factory; };
    }

    registerFeature("engine-worker", function() {
      var host = window.__gosx;
      if (!host) return {};
      var api = createPageHost(scriptURL);
      host.engineWorker = api;
      return { disposePage: api.disposeAll };
    });
  }

  function createPageHost(scriptURL) {
    /** @type {Map<string, any>} */
    var records = new Map();
    var visibilityBound = false;

    function documentHidden() {
      try { return document.hidden === true; } catch (_) { return false; }
    }

    function nowStamp() {
      // Epoch-based high resolution time, comparable across page and worker.
      return (performance.timeOrigin || 0) + performance.now();
    }

    function absolute(path) {
      try { return new URL(String(path), document.baseURI || location.href).href; } catch (_) { return String(path); }
    }

    function runtimeAssets() {
      try {
        var state = window.__gosx && window.__gosx.document && window.__gosx.document.get && window.__gosx.document.get();
        return (state && state.assets && state.assets.runtime) || {};
      } catch (_) { return {}; }
    }

    function workerScriptURL() {
      if (typeof window.__gosx_engine_worker_url === "string" && window.__gosx_engine_worker_url) return absolute(window.__gosx_engine_worker_url);
      var assets = runtimeAssets();
      return absolute(scriptURL || assets.bootstrapFeatureEngineWorkerPath || "/gosx/bootstrap-feature-engine-worker.js");
    }

    function wasmExecURL() {
      if (typeof window.__gosx_engine_worker_wasm_exec_url === "string" && window.__gosx_engine_worker_wasm_exec_url) return absolute(window.__gosx_engine_worker_wasm_exec_url);
      var assets = runtimeAssets();
      return absolute(assets.standardGoWasmExecPath || "/gosx/standard-go-wasm_exec.js");
    }

    function bootTimeoutMS() {
      var configured = Number(window.__gosx_go_wasm_factory_timeout_ms);
      if (isFinite(configured) && configured > 0) return Math.min(60000, Math.max(1, Math.floor(configured)));
      return 10000;
    }

    function unsupportedReason(entry) {
      if (window.__gosx_engine_worker_disable === true) return { code: "worker-disabled", message: "engine workers are disabled for this page" };
      if (typeof window.Worker !== "function") return { code: "worker-unsupported", message: "this browser has no Web Worker" };
      if (entry && entry.toolchain === "tinygo") return { code: "worker-toolchain-unsupported", message: "TinyGo engines cannot run in a worker yet" };
      return null;
    }

    function workerError(code, message, cause) {
      var error = new Error(message);
      error.name = "GoSXEngineWorkerError";
      error.code = code;
      if (cause) error.cause = cause;
      return error;
    }

    function report(entry, severity, code, message, error) {
      try {
        if (typeof window.__gosx_emit === "function") {
          window.__gosx_emit(severity === "error" ? "error" : "warn", "engine", message, {
            component: String(entry.component || ""), engineID: String(entry.id || ""), code: code,
            error: error && error.message ? String(error.message) : "",
          });
        }
        if (window.__gosx && typeof window.__gosx.reportIssue === "function") {
          window.__gosx.reportIssue({
            scope: "engine", type: "worker", severity: severity, code: code,
            component: entry.component, source: entry.id, ref: entry.programRef || entry.component,
            message: message, error: error, fallback: severity === "error" ? "server" : "main-thread",
          });
        }
      } catch (_) { /* reporting must not break the mount path */ }
    }

    function dispatchDocumentEvent(name, detail) {
      try { document.dispatchEvent(new CustomEvent("gosx:engine:" + name, { detail: detail })); } catch (_) {}
    }

    // ---- boot -------------------------------------------------------------

    /**
     * Resolve the factory for a worker-thread engine. When the worker cannot
     * start, run the engine on the main thread through `fallback` and report
     * why.
     * @param {any} entry
     * @param {any} pending
     * @param {() => Promise<{factory: any, moduleRecord: any}>} fallback
     */
    async function resolve(entry, pending, fallback) {
      var reason = unsupportedReason(entry);
      var record = null;
      if (!reason) {
        try {
          record = await boot(entry, pending);
        } catch (error) {
          reason = { code: (error && error.code) || "worker-boot-failed", message: (error && error.message) || String(error), error: error };
        }
      }
      if (reason) return degrade(entry, reason, fallback);
      return { factory: function(ctx) { return mountRecord(record, ctx); }, moduleRecord: null };
    }

    async function degrade(entry, reason, fallback) {
      report(entry, "warn", reason.code, "engine worker unavailable; running " + entry.id + " on the main thread: " + reason.message, reason.error);
      dispatchDocumentEvent("thread-fallback", { engineID: entry.id, component: entry.component, code: reason.code, message: reason.message });
      var resolved = await fallback();
      var inner = resolved.factory;
      return {
        moduleRecord: resolved.moduleRecord,
        factory: typeof inner !== "function" ? inner : function(ctx) {
          ctx.threadMode = "main";
          ctx.threadFallbackReason = reason.code;
          return inner(ctx);
        },
      };
    }

    function boot(entry, pending) {
      return new Promise(function(resolveBoot, rejectBoot) {
        var worker;
        try {
          worker = new Worker(workerScriptURL(), { name: "gosx-engine-" + entry.id });
        } catch (error) {
          rejectBoot(workerError("worker-create-failed", "could not create the engine worker: " + (error && error.message), error));
          return;
        }
        var rec = {
          entry: entry, worker: worker, state: "booting", ctx: null, disposed: false,
          subs: new Map(), listeners: new Map(), appHandlers: new Map(), mountWaiter: null,
          stats: { messagesIn: 0, framesIn: 0, frameBytes: 0, signalBatches: 0, signalBytes: 0, appMessages: 0, inputSent: 0 },
          terminate: function() { terminate(rec); },
        };
        var timer = setTimeout(function() {
          if (rec.state !== "booting") return;
          terminate(rec);
          rejectBoot(workerError("worker-boot-timeout", "timed out waiting for the engine worker to start"));
        }, bootTimeoutMS());
        rec.bootDone = function(error) {
          clearTimeout(timer);
          if (error) { terminate(rec); rejectBoot(error); } else { rec.state = "ready"; resolveBoot(rec); }
        };
        worker.onmessage = function(event) { onWorkerMessage(rec, event.data); };
        worker.onerror = function(event) {
          if (event && typeof event.preventDefault === "function") event.preventDefault();
          var message = (event && event.message) || "engine worker script failed";
          if (rec.state === "booting") rec.bootDone(workerError("worker-script-error", message));
          else onWorkerCrash(rec, workerError("worker-crashed", message));
        };
        worker.onmessageerror = function() {
          if (rec.state !== "booting") report(entry, "error", "worker-message-error", "engine worker sent a message the page could not decode");
        };
        // A disposed pending engine must not leak the worker.
        if (pending) pending.workerRecord = rec;
        records.set(entry.id, rec);
        bindVisibility();
        try {
          worker.postMessage({
            t: "init", v: PROTOCOL, id: entry.id, component: entry.component, kind: entry.kind,
            programRef: absolute(entry.programRef), wasmExec: wasmExecURL(), runtimeMode: entry.runtime || "go-wasm",
            props: entry.props || {}, capabilities: entry.capabilities || [], requiredCapabilities: entry.requiredCapabilities || [],
            hidden: documentHidden(), clock: { epochMs: nowStamp() },
            frameHz: Number(entry.props && entry.props.workerFrameHz) > 0 ? Number(entry.props.workerFrameHz) : DEFAULT_FRAME_HZ,
          });
        } catch (error) {
          rec.bootDone(workerError("worker-init-failed", "could not start the engine worker: " + (error && error.message), error));
        }
      });
    }

    function terminate(rec) {
      if (rec.terminated) return;
      rec.terminated = true;
      rec.disposed = true;
      for (var off of rec.subs.values()) { try { off(); } catch (_) {} }
      rec.subs.clear();
      for (var off2 of rec.listeners.values()) { try { off2(); } catch (_) {} }
      rec.listeners.clear();
      try { rec.worker.onmessage = rec.worker.onerror = rec.worker.onmessageerror = null; rec.worker.terminate(); } catch (_) {}
      if (records.get(rec.entry.id) === rec) records.delete(rec.entry.id);
      if (rec.mountWaiter) { var w = rec.mountWaiter; rec.mountWaiter = null; w.bad(workerError("worker-terminated", "engine worker was terminated")); }
      if (rec.onTerminate) { try { rec.onTerminate(); } catch (_) {} }
    }

    function onWorkerCrash(rec, error) {
      if (rec.terminated) return;
      report(rec.entry, "error", error.code, "engine worker crashed: " + error.message, error);
      dispatchDocumentEvent("worker-error", { engineID: rec.entry.id, component: rec.entry.component, code: error.code, message: error.message });
      terminate(rec);
    }

    // ---- mount ------------------------------------------------------------

    function mountRecord(rec, ctx) {
      if (rec.terminated) throw workerError("worker-terminated", "engine worker was terminated before mount");
      rec.ctx = ctx;
      ctx.threadMode = "worker";
      return new Promise(function(ok, bad) {
        rec.mountWaiter = { ok: ok, bad: bad };
        rec.worker.postMessage({ t: "mount" });
      }).then(function() {
        return { dispose: function() { disposeRecord(rec); } };
      }, function(error) {
        terminate(rec);
        throw error;
      });
    }

    function disposeRecord(rec) {
      if (rec.terminated || rec.disposing) return;
      rec.disposing = true;
      rec.disposed = true;
      try { rec.worker.postMessage({ t: "dispose" }); } catch (_) {}
      rec.shutdownTimer = setTimeout(function() { terminate(rec); }, SHUTDOWN_GRACE_MS);
    }

    // ---- worker -> page ---------------------------------------------------

    function reply(rec, rid, ok, value, error) {
      if (rec.terminated) return;
      try { rec.worker.postMessage({ t: "reply", rid: rid, ok: ok, value: value, error: error }); } catch (_) {}
    }

    function current(rec) {
      if (rec.terminated || rec.disposed) return false;
      var ctx = rec.ctx;
      if (!ctx) return false;
      return typeof ctx.isCurrent !== "function" || ctx.isCurrent();
    }

    function resolveMount(target) {
      if (typeof target === "string") return document.getElementById(target);
      return target || null;
    }

    function onWorkerMessage(rec, msg) {
      if (rec.terminated || !msg || typeof msg.t !== "string") return;
      rec.stats.messagesIn++;
      var handler = pageHandlers[msg.t];
      if (typeof handler === "function") handler(rec, msg);
    }

    var pageHandlers = {
      booted: function(rec, msg) {
        if (rec.state !== "booting") return;
        if (msg.v !== PROTOCOL) { rec.bootDone(workerError("worker-protocol", "engine worker speaks protocol " + msg.v + ", the page speaks " + PROTOCOL)); return; }
        rec.bootDone(null);
      },
      fatal: function(rec, msg) {
        var error = workerError(msg.code || "worker-failed", msg.message || "engine worker failed");
        if (rec.state === "booting") rec.bootDone(error);
        else if (rec.mountWaiter) { var w = rec.mountWaiter; rec.mountWaiter = null; w.bad(error); }
        else onWorkerCrash(rec, error);
      },
      mounted: function(rec) {
        if (rec.mountWaiter) { var w = rec.mountWaiter; rec.mountWaiter = null; w.ok(); }
      },
      disposed: function(rec) {
        clearTimeout(rec.shutdownTimer);
        terminate(rec);
      },
      log: function(rec, msg) {
        var fn = console[msg.level === "error" ? "error" : msg.level === "warn" ? "warn" : "log"] || console.log;
        fn.call(console, "[gosx worker " + rec.entry.id + "]", msg.text);
      },

      // Shared signals. The worker sends a JSON batch as a transferred
      // ArrayBuffer; the page applies it through the same context method a
      // main-thread engine uses.
      signals: function(rec, msg) {
        if (!current(rec) || !(msg.buf instanceof ArrayBuffer)) return;
        rec.stats.signalBatches++;
        rec.stats.signalBytes += msg.buf.byteLength;
        try {
          var text = new TextDecoder("utf-8").decode(msg.buf);
          rec.ctx.setSignalsJSON(text, msg.changedOnly === true);
        } catch (error) {
          try { rec.worker.postMessage({ t: "fault", where: "signals", message: String(error && error.message || error) }); } catch (_) {}
        }
      },
      "signal.set": function(rec, msg) {
        if (!current(rec)) return;
        try { rec.ctx.setSignal(msg.name, JSON.parse(msg.json)); } catch (error) {
          try { rec.worker.postMessage({ t: "fault", where: "signal.set", message: String(error && error.message || error) }); } catch (_) {}
        }
      },
      subscribe: function(rec, msg) {
        if (!current(rec) || rec.subs.has(msg.sid)) return;
        var primed = false;
        var off = rec.ctx.subscribeSignalJSON(msg.name, function(encoded, name) {
          if (!primed) return;
          try { rec.worker.postMessage({ t: "signal", sid: msg.sid, name: name || msg.name, json: encoded }); } catch (_) {}
        }, { immediate: false });
        rec.subs.set(msg.sid, off);
        primed = true;
        // Prime the worker's mirror so a synchronous read after subscribe works.
        var value = rec.ctx.getSignal(msg.name);
        if (value !== undefined) {
          try { rec.worker.postMessage({ t: "signal", sid: msg.sid, name: msg.name, json: JSON.stringify(value), initial: true }); } catch (_) {}
        }
      },
      unsubscribe: function(rec, msg) {
        var off = rec.subs.get(msg.sid);
        if (off) { rec.subs.delete(msg.sid); try { off(); } catch (_) {} }
      },

      // Calls that return a result use a request id.
      scene3d: function(rec, msg) {
        if (!current(rec)) { reply(rec, msg.rid, false, null, "engine context is disposed"); return; }
        Promise.resolve().then(function() {
          var args = Array.isArray(msg.args) ? msg.args : [];
          return rec.ctx.scene3D.apply(rec.ctx, [msg.method, resolveMount(msg.target)].concat(args));
        }).then(function(value) { reply(rec, msg.rid, true, value === undefined ? null : value, null); },
          function(error) { reply(rec, msg.rid, false, null, String(error && error.message || error)); });
      },
      navigate: function(rec, msg) {
        if (!current(rec)) { reply(rec, msg.rid, false, null, "engine context is disposed"); return; }
        Promise.resolve().then(function() { return rec.ctx.navigate(msg.target, msg.options); })
          .then(function(value) { reply(rec, msg.rid, true, value === undefined ? null : value, null); },
            function(error) { reply(rec, msg.rid, false, null, String(error && error.message || error)); });
      },
      emit: function(rec, msg) {
        if (current(rec) && typeof rec.ctx.emit === "function") rec.ctx.emit(msg.name, msg.detail);
      },

      // Binary frames: the same page entry points a main-thread engine calls.
      frame: function(rec, msg) {
        if (!current(rec) || !(msg.buf instanceof ArrayBuffer)) return;
        rec.stats.framesIn++;
        rec.stats.frameBytes += msg.buf.byteLength;
        var scene = window.__gosx && window.__gosx.scene3d;
        var bytes = new Uint8Array(msg.buf);
        var options = msg.options && typeof msg.options === "object" ? msg.options : undefined;
        if (options && options.fallbackPoseFrame instanceof ArrayBuffer) options.fallbackPoseFrame = new Uint8Array(options.fallbackPoseFrame);
        var result;
        try {
          if (msg.kind === "instance") {
            var mount = resolveMount(msg.target);
            var handle = mount && mount.__gosxScene3DHandle;
            if (!handle || typeof handle.applyInstanceStream !== "function") throw new Error("Scene3D instance stream target is not ready");
            result = handle.applyInstanceStream(bytes);
          } else {
            var method = FRAME_METHODS[msg.kind];
            if (!method || !scene || typeof scene[method] !== "function") throw new Error("Scene3D " + msg.kind + " frame dispatch is unavailable");
            result = scene[method](msg.target, bytes, options);
          }
        } catch (error) {
          frameFault(rec, msg, error);
          return;
        }
        if (result && typeof result.catch === "function") result.catch(function(error) { frameFault(rec, msg, error); });
      },

      // Scene3D mount access for Go code that has no document: events on the
      // mount element, and a short list of renderer calls. Event names stay in
      // the gosx:scene3d: namespace.
      "mount.op": function(rec, msg) {
        if (!current(rec)) return;
        var mount = resolveMount(msg.target);
        if (!mount) { mountFault(rec, msg, "mount #" + msg.target + " not found"); return; }
        try {
          var detail = typeof msg.json === "string" ? JSON.parse(msg.json) : undefined;
          if (msg.op === "dispatchEvent" && SCENE_EVENT.test(msg.name)) {
            mount.dispatchEvent(new CustomEvent(msg.name, { detail: detail }));
          } else if (msg.op === "call" && MOUNT_CALLS.indexOf(msg.name) >= 0 && mount.__gosxScene3DHandle) {
            mount.__gosxScene3DHandle[msg.name](detail);
          } else {
            mountFault(rec, msg, "mount operation " + msg.op + " " + msg.name + " is not allowed");
          }
        } catch (error) { mountFault(rec, msg, String(error && error.message || error)); }
      },
      "mount.listen": function(rec, msg) {
        if (!current(rec) || !SCENE_EVENT.test(msg.name)) return;
        var mount = resolveMount(msg.target);
        if (!mount || typeof mount.addEventListener !== "function") { mountFault(rec, msg, "mount #" + msg.target + " not found"); return; }
        var listener = function(event) {
          if (event.target !== mount || rec.terminated) return;
          var json;
          try { json = JSON.stringify(event.detail); } catch (_) { return; }
          try { rec.worker.postMessage({ t: "mount.event", lid: msg.lid, json: json }); } catch (_) {}
        };
        mount.addEventListener(msg.name, listener);
        rec.listeners.set(msg.lid, function() { mount.removeEventListener(msg.name, listener); });
      },
      "mount.unlisten": function(rec, msg) {
        var off = rec.listeners.get(msg.lid);
        if (off) { rec.listeners.delete(msg.lid); try { off(); } catch (_) {} }
      },

      // Messages the application defines between its page code and its engine.
      app: function(rec, msg) {
        rec.stats.appMessages++;
        var data = msg.buf instanceof ArrayBuffer ? msg.buf : undefined;
        var json;
        if (typeof msg.json === "string") { try { json = JSON.parse(msg.json); } catch (_) { json = undefined; } }
        var handlers = rec.appHandlers.get(msg.name);
        if (handlers) for (var fn of Array.from(handlers)) { try { fn(json, data); } catch (error) { console.error("[gosx] engine worker handler error:", error); } }
        dispatchDocumentEvent("message", { engineID: rec.entry.id, component: rec.entry.component, name: msg.name, json: json, data: data });
      },
    };

    var FRAME_METHODS = { pose: "dispatchPoseFrame", motion: "dispatchMotionFrame", clip: "dispatchClipFrame" };
    var SCENE_EVENT = /^gosx:scene3d:[a-z0-9-]+$/;
    var MOUNT_CALLS = ["updateSceneProps"];

    function mountFault(rec, msg, text) {
      try { rec.worker.postMessage({ t: "fault", where: "mount." + msg.op, message: text }); } catch (_) {}
    }

    function frameFault(rec, msg, error) {
      console.error("[gosx] engine worker " + msg.kind + " frame rejected:", error);
      dispatchDocumentEvent("worker-frame-error", { engineID: rec.entry.id, kind: msg.kind, target: msg.target, message: String(error && error.message || error) });
    }

    // ---- page -> worker ---------------------------------------------------

    function bindVisibility() {
      if (visibilityBound || typeof document.addEventListener !== "function") return;
      visibilityBound = true;
      document.addEventListener("visibilitychange", function() {
        var msg = { t: "visibility", hidden: documentHidden(), at: nowStamp() };
        for (var rec of records.values()) { try { rec.worker.postMessage(msg); } catch (_) {} }
      });
    }

    function get(id) {
      var rec = records.get(id);
      if (!rec) return null;
      return {
        id: id,
        stats: function() { return Object.assign({}, rec.stats); },
        // Send an application message to the engine. `data` is JSON-encoded;
        // `bytes` (ArrayBuffer or typed array) is transferred.
        post: function(name, data, bytes) {
          if (rec.terminated) return false;
          var msg = { t: "app", name: String(name), json: data === undefined ? undefined : JSON.stringify(data) };
          var transfer = [];
          if (bytes) {
            var buf = bytes instanceof ArrayBuffer ? bytes : (bytes.byteOffset === 0 && bytes.byteLength === bytes.buffer.byteLength ? bytes.buffer : bytes.slice().buffer);
            msg.buf = buf; transfer.push(buf);
          }
          rec.worker.postMessage(msg, transfer);
          return true;
        },
        onMessage: function(name, fn) {
          var set = rec.appHandlers.get(name) || new Set();
          set.add(fn); rec.appHandlers.set(name, set);
          return function() { set.delete(fn); };
        },
        terminate: function() { terminate(rec); },
        worker: rec.worker,
      };
    }

    function disposeAll() { for (var rec of Array.from(records.values())) terminate(rec); }

    return {
      version: 1,
      protocol: PROTOCOL,
      supported: function() { return unsupportedReason(null) === null; },
      resolve: resolve,
      get: get,
      ids: function() { return Array.from(records.keys()); },
      disposeAll: disposeAll,
    };
  }

  // ==========================================================================
  // Worker half
  // ==========================================================================

  function workerMain(scope) {
    var encoder = new TextEncoder();
    var init = null;
    var factory = null;
    var handle = null;
    var goExit = null;
    var nextRID = 1;
    var pendingReplies = new Map();
    var mirror = new Map();           // shared signal values the page pushed
    var signalHandlers = new Map();   // sid -> {name, fn}
    var nextSID = 1;
    var appHandlers = new Set();
    var visibilityHandlers = new Set();
    var mountListeners = new Map();   // lid -> callback(json)
    var nextLID = 1;
    var hidden = false;
    var epochSkew = 0;                // page epoch ms minus worker epoch ms at init

    function post(msg, transfer) { scope.postMessage(msg, transfer || []); }
    function workerEpoch() { return (scope.performance.timeOrigin || 0) + scope.performance.now(); }

    function logToPage(level, text) { try { post({ t: "log", level: level, text: String(text) }); } catch (_) {} }

    function fatal(code, error) {
      post({ t: "fatal", code: code, message: String(error && error.message || error), stack: String(error && error.stack || "") });
    }

    // ---- requests to the page ---------------------------------------------

    function request(msg) {
      return new Promise(function(ok, bad) {
        var rid = nextRID++;
        pendingReplies.set(rid, { ok: ok, bad: bad });
        msg.rid = rid;
        try { post(msg); } catch (error) { pendingReplies.delete(rid); bad(error); }
      });
    }

    // Take ownership of a typed array's buffer for transfer, copying when the
    // array is a view of a larger buffer.
    function ownedBuffer(bytes) {
      if (bytes instanceof ArrayBuffer) return bytes;
      if (bytes.byteOffset === 0 && bytes.byteLength === bytes.buffer.byteLength && bytes.buffer instanceof ArrayBuffer) return bytes.buffer;
      return bytes.slice().buffer;
    }

    function postFrame(kind, target, bytes, options) {
      var buf = ownedBuffer(bytes);
      var transfer = [buf];
      var sent;
      if (options && typeof options === "object") {
        sent = Object.assign({}, options);
        var fb = sent.fallbackPoseFrame;
        if (fb && typeof fb === "object" && !(fb instanceof ArrayBuffer) && fb.buffer) { sent.fallbackPoseFrame = ownedBuffer(fb); transfer.push(sent.fallbackPoseFrame); }
      }
      post({ t: "frame", kind: kind, target: String(target), buf: buf, options: sent }, transfer);
    }

    // ---- page APIs Go calls, made worker-safe -----------------------------

    var frameClock = createFrameClock(scope);

    var workerAPI = {
      version: 1,
      get hidden() { return hidden; },
      requestFrame: frameClock.request,
      cancelFrame: frameClock.cancel,
      setFrameRate: frameClock.setRate,
      frameRate: frameClock.rate,
      frameStats: frameClock.stats,
      now: function() { return scope.performance.now(); },
      // Page-clock time for an epoch stamp the page attached to a message.
      toLocalTime: function(epochMs) { return epochMs - epochSkew - (scope.performance.timeOrigin || 0); },
      post: function(name, json) { post({ t: "app", name: String(name), json: json === undefined ? undefined : String(json) }); },
      postBytes: function(name, bytes) { var buf = ownedBuffer(bytes); post({ t: "app", name: String(name), buf: buf }, [buf]); },
      onMessage: function(fn) { appHandlers.add(fn); return function() { appHandlers.delete(fn); }; },
      onVisibility: function(fn) { visibilityHandlers.add(fn); return function() { visibilityHandlers.delete(fn); }; },
      request: request,
      postFrame: postFrame,
      // Scene3D mount access without a document (see the page handlers).
      mountOp: function(target, op, name, json) {
        post({ t: "mount.op", target: String(target), op: String(op), name: String(name), json: json === undefined ? undefined : String(json) });
      },
      mountListen: function(target, name, callback) {
        var lid = nextLID++;
        mountListeners.set(lid, callback);
        post({ t: "mount.listen", lid: lid, target: String(target), name: String(name) });
        return function() {
          if (!mountListeners.delete(lid)) return;
          try { post({ t: "mount.unlisten", lid: lid }); } catch (_) {}
        };
      },
    };

    var scene3d = {};
    ["dispatchPoseFrame:pose", "dispatchMotionFrame:motion", "dispatchClipFrame:clip"].forEach(function(pair) {
      var parts = pair.split(":");
      scene3d[parts[0]] = function(target, bytes, options) {
        postFrame(parts[1], target, bytes, options);
        return Promise.resolve({ queued: true });
      };
    });
    scene3d.applyInstanceStream = function(target, bytes) {
      postFrame("instance", target, bytes);
      return { applied: true, queued: true };
    };

    var gosx = scope.__gosx = scope.__gosx || {};
    gosx.worker = workerAPI;
    gosx.scene3d = Object.assign(gosx.scene3d || {}, scene3d);
    scope.__gosx_worker_extensions = scope.__gosx_worker_extensions || [];

    // ---- engine context ---------------------------------------------------

    function stripAbortOption(args) {
      var last = args[args.length - 1];
      if (last && typeof last === "object" && Object.keys(last).length === 1 && "signal" in last) return args.slice(0, -1);
      return args;
    }

    function buildContext() {
      var ctx = {
        id: init.id, kind: init.kind, component: init.component, mount: null, props: init.props || {},
        capabilities: init.capabilities || [], requiredCapabilities: init.requiredCapabilities || [],
        programRef: init.programRef, runtimeMode: init.runtimeMode, threadMode: "worker",
        isCurrent: function() { return !ctx._disposed; },
        getSignal: function(name) { return mirror.get(name); },
        setSignal: function(name, value) { post({ t: "signal.set", name: String(name), json: JSON.stringify(value) }); mirror.set(name, value); },
        setSignals: function(values) { return ctx.setSignalsJSON(JSON.stringify(values), false); },
        setSignalsJSON: function(encoded, changedOnly) {
          var bytes = encoder.encode(String(encoded));
          post({ t: "signals", changedOnly: changedOnly === true, buf: bytes.buffer }, [bytes.buffer]);
          return undefined;
        },
        subscribeSignal: function(name, handler, options) {
          return ctx.subscribeSignalJSON(name, function(encoded, signalName) { handler(JSON.parse(encoded), signalName); }, options);
        },
        subscribeSignalJSON: function(name, handler) {
          var sid = nextSID++;
          signalHandlers.set(sid, { name: String(name), fn: handler });
          post({ t: "subscribe", sid: sid, name: String(name) });
          return function() {
            if (!signalHandlers.delete(sid)) return;
            try { post({ t: "unsubscribe", sid: sid }); } catch (_) {}
          };
        },
        navigate: function(target, options) { return request({ t: "navigate", target: String(target), options: options || null }); },
        scene3D: function(method, target) {
          var args = stripAbortOption(Array.prototype.slice.call(arguments, 2));
          if (method === "getCamera") return null; // reads are main-thread only; use SetCamera
          if (typeof target !== "string") throw new TypeError("a worker Scene3D needs the mount id as a string; use ScenebyID");
          return request({ t: "scene3d", method: String(method), target: target, args: args });
        },
        emit: function(name, detail) { post({ t: "emit", name: String(name), detail: detail === undefined ? null : detail }); },
        _disposed: false,
      };
      return ctx;
    }

    // ---- boot ---------------------------------------------------------------

    async function boot(message) {
      init = message;
      hidden = message.hidden === true;
      epochSkew = (message.clock && message.clock.epochMs ? message.clock.epochMs : workerEpoch()) - workerEpoch();
      frameClock.setRate(message.frameHz || DEFAULT_FRAME_HZ);
      var token = randomToken();
      var registered = new Promise(function(resolveReg) {
        scope.__gosx_register_go_wasm_engine_factory = function(given, name, fn) {
          if (given !== token || String(name) !== init.component || typeof fn !== "function") return false;
          factory = fn;
          resolveReg();
          return true;
        };
      });
      scope.importScripts(message.wasmExec);
      var StandardGo = scope.__gosx_standard_go_wasm_ctor;
      if (typeof StandardGo !== "function") throw codedError("worker-wasm-exec-missing", "wasm_exec.js did not publish the standard Go runtime");
      var go = new StandardGo();
      go.env = go.env && typeof go.env === "object" ? go.env : {};
      go.env.GOSX_GO_WASM_REGISTRATION_TOKEN = token;
      var response = await scope.fetch(message.programRef);
      if (!response.ok) throw codedError("worker-fetch-status", "engine module fetch failed with status " + response.status);
      var instance = await instantiate(response, go.importObject);
      var run = go.run(instance);
      goExit = run && typeof run.then === "function"
        ? run.then(function() { return codedError("worker-module-exited", "the engine module exited"); }, function(e) { return codedError("worker-run-failed", e && e.message || String(e)); })
        : new Promise(function() {});
      var early = await Promise.race([registered.then(function() { return null; }), goExit]);
      if (early) throw early;
      goExit.then(function(error) { if (!ctxDisposing) fatal(error.code, error); });
      post({ t: "booted", v: PROTOCOL });
    }

    var ctxDisposing = false;

    function codedError(code, message) { var e = new Error(message); e.code = code; return e; }

    async function instantiate(response, importObject) {
      if (typeof scope.WebAssembly.instantiateStreaming === "function") {
        try { return (await scope.WebAssembly.instantiateStreaming(response.clone(), importObject)).instance; } catch (_) { /* wrong MIME type: use bytes */ }
      }
      return (await scope.WebAssembly.instantiate(await response.arrayBuffer(), importObject)).instance;
    }

    function randomToken() {
      var words = new Uint32Array(4);
      scope.crypto.getRandomValues(words);
      return Array.from(words, function(w) { return w.toString(16).padStart(8, "0"); }).join("");
    }

    var ctxObject = null;

    async function mount() {
      ctxObject = buildContext();
      var extensions = scope.__gosx_worker_extensions;
      for (var ext of extensions) { try { ext(ctxObject, workerAPI); } catch (error) { logToPage("error", error); } }
      var result = factory(ctxObject);
      if (result && typeof result.then === "function") result = await result;
      handle = result || {};
      post({ t: "mounted" });
    }

    function disposeEngine() {
      ctxDisposing = true;
      if (ctxObject) ctxObject._disposed = true;
      try { if (handle && typeof handle.dispose === "function") handle.dispose(); } catch (error) { logToPage("error", error); }
      frameClock.stop();
      post({ t: "disposed" });
    }

    // ---- page -> worker -----------------------------------------------------

    var handlers = {
      init: function(msg) {
        boot(msg).catch(function(error) { fatal(error && error.code || "worker-boot-failed", error); });
      },
      mount: function() {
        mount().catch(function(error) { fatal(error && error.code || "worker-mount-failed", error); });
      },
      dispose: disposeEngine,
      reply: function(msg) {
        var waiter = pendingReplies.get(msg.rid);
        if (!waiter) return;
        pendingReplies.delete(msg.rid);
        if (msg.ok) waiter.ok(msg.value); else waiter.bad(new Error(msg.error || "request failed"));
      },
      visibility: function(msg) {
        hidden = msg.hidden === true;
        for (var fn of Array.from(visibilityHandlers)) { try { fn(hidden); } catch (error) { logToPage("error", error); } }
      },
      signal: function(msg) {
        var entry = signalHandlers.get(msg.sid);
        var name = String(msg.name);
        try { mirror.set(name, JSON.parse(msg.json)); } catch (_) { return; }
        if (!entry || msg.initial) return;
        try { entry.fn(msg.json, name); } catch (error) { logToPage("error", error); }
      },
      app: function(msg) {
        var bytes = msg.buf instanceof ArrayBuffer ? new Uint8Array(msg.buf) : undefined;
        for (var fn of Array.from(appHandlers)) { try { fn(String(msg.name), msg.json, bytes); } catch (error) { logToPage("error", error); } }
      },
      "mount.event": function(msg) {
        var callback = mountListeners.get(msg.lid);
        if (callback) { try { callback(msg.json); } catch (error) { logToPage("error", error); } }
      },
      fault: function(msg) { logToPage("error", "page rejected " + msg.where + ": " + msg.message); },
    };

    scope.onmessage = function(event) {
      var msg = event.data;
      if (!msg || typeof msg.t !== "string") return;
      var handler = handlers[msg.t];
      if (typeof handler === "function") handler(msg);
      else if (scope.__gosx_worker_handlers && typeof scope.__gosx_worker_handlers[msg.t] === "function") scope.__gosx_worker_handlers[msg.t](msg);
    };
  }

  // A fixed-rate frame clock built on timers. requestAnimationFrame may be
  // missing in a worker, and where it exists it follows the page's display,
  // so the simulation does not use it. Callbacks fire on a fixed grid of
  // 1000/hz ms slots; a late timer skips ahead to the next slot, never
  // bursting to catch up.
  function createFrameClock(scope) {
    var hz = DEFAULT_FRAME_HZ;
    var callbacks = new Map();
    var nextHandle = 1;
    var timer = 0;
    var base = 0;
    var firedCount = 0;
    var lateMax = 0;
    var gapMax = 0;
    var gapSum = 0;
    var lastFire = 0;
    var skipped = 0;
    var stopped = false;

    function period() { return 1000 / hz; }

    function arm() {
      if (timer || stopped || callbacks.size === 0) return;
      var now = scope.performance.now();
      if (!base) base = now;
      // A timer can fire a fraction of a millisecond before its slot. Count
      // from the later of "now" and the slot just served, or the early fire
      // would land in the same slot again and double the frame rate.
      var reference = Math.max(now, armedFor);
      var slot = Math.floor((reference - base) / period() + 1e-9) + 1;
      var due = base + slot * period();
      timer = scope.setTimeout(fire, Math.max(0, due - now));
      armedFor = due;
    }
    var armedFor = 0;

    function fire() {
      timer = 0;
      var now = scope.performance.now();
      var late = now - armedFor;
      if (late > lateMax) lateMax = late;
      if (late > period()) skipped += Math.floor(late / period());
      if (lastFire) { var gap = now - lastFire; gapSum += gap; if (gap > gapMax) gapMax = gap; }
      lastFire = now;
      firedCount++;
      var batch = Array.from(callbacks.values());
      callbacks.clear();
      for (var cb of batch) {
        try { cb(now); } catch (error) { try { scope.console.error("[gosx] frame callback error:", error); } catch (_) {} }
      }
      arm();
    }

    return {
      request: function(cb) {
        var handle = nextHandle++;
        callbacks.set(handle, cb);
        stopped = false;
        arm();
        return handle;
      },
      cancel: function(handle) { callbacks.delete(handle); },
      setRate: function(next) {
        next = Number(next);
        if (!(next >= 1 && next <= 1000)) return false;
        hz = next;
        base = 0;
        armedFor = 0;
        if (timer) { scope.clearTimeout(timer); timer = 0; arm(); }
        return true;
      },
      rate: function() { return hz; },
      stats: function() {
        return { hz: hz, frames: firedCount, lateMaxMs: lateMax, gapMaxMs: gapMax, gapAvgMs: firedCount > 1 ? gapSum / (firedCount - 1) : 0, skippedSlots: skipped };
      },
      stop: function() { stopped = true; callbacks.clear(); if (timer) { scope.clearTimeout(timer); timer = 0; } },
    };
  }
})();
