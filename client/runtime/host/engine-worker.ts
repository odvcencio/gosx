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
//   Native      The page half can also talk to an engine that runs natively in a
//               GoSX desktop host (desktop/enginehost) instead of in a Worker. A
//               small channel object (createNativeWorker) speaks the same message
//               protocol over window.gosxDesktop, so the rest of the page half,
//               and every island that reads the engine's signals or calls it,
//               cannot tell the two apart. The page tries it first when the
//               desktop bridge is present, and starts the Worker when the host
//               has no native engine for the component.
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
      return { disposePage: api.disposePage };
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
      if (entry && entry.forceWorkerFallback === true) return { code: "worker-forced-fallback", message: "the page forces the main-thread fallback" };
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

    // ---- page signals -------------------------------------------------------
    //
    // The host writes shared signals (status, call results) the same way a
    // main-thread engine does, but it has no engine context before the mount,
    // so it goes through the page-level setter.

    function writeSignal(name, value) {
      var json = JSON.stringify(value == null ? null : value);
      try {
        if (typeof window.__gosx_set_shared_signal === "function") {
          var result = window.__gosx_set_shared_signal(name, json);
          if (typeof result === "string" && result !== "" && typeof window.__gosx_notify_shared_signal === "function") window.__gosx_notify_shared_signal(name, json);
        } else if (typeof window.__gosx_notify_shared_signal === "function") {
          window.__gosx_notify_shared_signal(name, json);
        }
      } catch (_) { /* a signal write must not break the engine path */ }
    }

    // $gosx.engine.<id> carries {State, Thread, Reason}. Keys are Go field names.
    function setStatus(entry, state, thread, reason) {
      writeSignal("$gosx.engine." + entry.id, { State: state, Thread: thread || "", Reason: reason || "" });
    }

    function dispatchDocumentEvent(name, detail) {
      try { document.dispatchEvent(new CustomEvent("gosx:engine:" + name, { detail: detail })); } catch (_) {}
    }

    // ---- native engine channel -----------------------------------------------
    //
    // A desktop host that registered a native engine (desktop/enginehost) answers
    // on the typed desktop bridge. The capability check is the bridge itself:
    // window.gosxDesktop with call and on. The channel below stands in for a
    // Worker: postMessage sends page-to-engine messages with gosxDesktop.call,
    // and the host's gosx.engine.msg events arrive as worker messages. The
    // message protocol is the Worker's, so nothing after boot knows the engine
    // is native.

    var NATIVE_ATTACH = "gosx.engine.attach";
    var NATIVE_POST = "gosx.engine.post";
    var NATIVE_EVENT = "gosx.engine.msg";
    // Messages that mean something to a native engine. Input, app messages and
    // netplay belong to a worker's page APIs and are not forwarded.
    var NATIVE_FORWARD = { mount: 1, dispose: 1, visibility: 1, signal: 1, ack: 1, fault: 1 };

    function nativeBridge() {
      if (window.__gosx_engine_native_disable === true) return null;
      var api = window.gosxDesktop;
      if (!api || typeof api.call !== "function" || typeof api.on !== "function") return null;
      return api;
    }

    function createNativeWorker(api, entry) {
      var channel = { onmessage: null, onerror: null, onmessageerror: null };
      var off = null, attached = false, ended = false;
      function deliver(msg) {
        if (ended || typeof channel.onmessage !== "function") return;
        channel.onmessage({ data: msg });
      }
      function send(msg, onFail) {
        var pending;
        try { pending = api.call(NATIVE_POST, { id: entry.id, msg: msg }); } catch (error) { if (onFail) onFail(error); return; }
        if (pending && typeof pending.then === "function") pending.then(null, function(error) { if (onFail) onFail(error); });
      }
      function attach(init) {
        try {
          off = api.on(NATIVE_EVENT, function(payload) {
            if (payload && payload.id === entry.id && payload.msg) deliver(payload.msg);
          });
        } catch (error) {
          deliver({ t: "fatal", code: "native-attach-failed", message: String(error && error.message || error) });
          return;
        }
        var request = { id: entry.id, component: entry.component, props: init.props || {}, hidden: init.hidden === true };
        var pending;
        try { pending = api.call(NATIVE_ATTACH, request); } catch (error) { pending = Promise.reject(error); }
        Promise.resolve(pending).then(function(answer) {
          attached = true;
          deliver({ t: "booted", v: answer && answer.v });
        }, function(error) {
          var missing = error && (error.code === "engine.not_registered" || error.code === "bridge.not_found");
          deliver({ t: "fatal", code: missing ? "native-not-registered" : "native-attach-failed", message: String(error && error.message || error) });
        });
      }
      channel.postMessage = function(msg) {
        if (ended || !msg || typeof msg.t !== "string") return;
        if (msg.t === "init") { attach(msg); return; }
        if (!attached || !NATIVE_FORWARD[msg.t]) return;
        if (msg.t === "mount") send(msg, function(error) { deliver({ t: "fatal", code: "native-mount-failed", message: String(error && error.message || error) }); });
        else if (msg.t === "dispose") { disposeSent = true; send(msg, function() { deliver({ t: "disposed" }); }); }
        else send(msg);
      };
      var disposeSent = false;
      channel.terminate = function() {
        if (ended) return;
        // A page that drops the engine without Dispose still lets the host shut it down.
        if (attached && !disposeSent) send({ t: "dispose" });
        ended = true;
        if (off) { try { off(); } catch (_) {} off = null; }
      };
      return channel;
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
      var record = null;
      // A native engine in the desktop host needs no Worker, no wasm and no
      // browser toolchain, so it is tried before the checks for those. A page
      // that forces the fallback still skips it.
      var bridgeApi = entry && entry.forceWorkerFallback === true ? null : nativeBridge();
      if (bridgeApi) {
        try {
          record = await boot(entry, pending, bridgeApi);
        } catch (error) {
          // A host without a native engine for this component is the ordinary case
          // for an app that ships both; the worker path below takes over quietly.
          if (!error || error.code !== "native-not-registered") {
            report(entry, "warn", (error && error.code) || "native-boot-failed", "native engine unavailable; trying the worker: " + (error && error.message || error), error);
          }
        }
      }
      var reason = record ? null : unsupportedReason(entry);
      if (!reason && !record) {
        try {
          record = await boot(entry, pending, null);
        } catch (error) {
          reason = { code: (error && error.code) || "worker-boot-failed", message: (error && error.message) || String(error), error: error };
        }
      }
      if (reason && entry.workerMode === "required") return refuse(entry, reason);
      if (reason) return degrade(entry, reason, fallback);
      return { factory: function(ctx) { return mountRecord(record, ctx); }, moduleRecord: null };
    }

    // A worker-required engine never runs on the main thread: report why and
    // hand the runtime an engine that does nothing.
    function refuse(entry, reason) {
      report(entry, "error", reason.code, "engine worker unavailable and " + entry.id + " requires one: " + reason.message, reason.error);
      dispatchDocumentEvent("worker-unsupported", { engineID: entry.id, component: entry.component, code: reason.code, message: reason.message });
      setStatus(entry, "unsupported", "", reason.code);
      return {
        moduleRecord: null,
        factory: function(ctx) {
          ctx.threadMode = "";
          ctx.threadFallbackReason = reason.code;
          return { dispose: function() { setStatus(entry, "disposed", "", reason.code); } };
        },
      };
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
          // "running" only once the engine has mounted; a failed mount is "crashed".
          var finish = function(h) {
            setStatus(entry, "running", "main", reason.code);
            var original = h && h.dispose;
            return Object.assign({}, h, { dispose: function() { setStatus(entry, "disposed", "main", reason.code); if (typeof original === "function") original.call(h); } });
          };
          var failed = function(error) { setStatus(entry, "crashed", "main", reason.code); throw error; };
          var handle;
          try { handle = inner(ctx); } catch (error) { failed(error); }
          return handle && typeof handle.then === "function" ? handle.then(finish, failed) : finish(handle);
        },
      };
    }

    function boot(entry, pending, nativeApi) {
      return new Promise(function(resolveBoot, rejectBoot) {
        var worker;
        try {
          worker = nativeApi ? createNativeWorker(nativeApi, entry) : new Worker(workerScriptURL(), { name: "gosx-engine-" + entry.id });
        } catch (error) {
          rejectBoot(workerError("worker-create-failed", "could not create the engine worker: " + (error && error.message), error));
          return;
        }
        var rec = {
          entry: entry, worker: worker, thread: nativeApi ? "native" : "worker", state: "booting", ctx: null, disposed: false,
          subs: new Map(), calls: new Map(), vessel: new Map(), crashedStatus: false, listeners: new Map(), appHandlers: new Map(), nets: new Map(), netModes: {}, mountWaiter: null,
          stats: { messagesIn: 0, framesIn: 0, frameBytes: 0, signalBatches: 0, signalBytes: 0, appMessages: 0, inputSent: 0, vesselRejected: 0 },
          terminate: function() { terminate(rec); },
        };
        setStatus(entry, "booting", rec.thread, "");
        var timer = setTimeout(function() {
          if (rec.state !== "booting") return;
          rec.bootFailed = true;
          terminate(rec);
          rejectBoot(workerError("worker-boot-timeout", "timed out waiting for the engine worker to start"));
        }, bootTimeoutMS());
        rec.bootDone = function(error) {
          clearTimeout(timer);
          if (error) { rec.bootFailed = true; terminate(rec); rejectBoot(error); } else { rec.state = "ready"; resolveBoot(rec); }
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
            programRef: absolute(entry.programRef), wasmExec: nativeApi ? "" : wasmExecURL(), runtimeMode: entry.runtime || "go-wasm",
            props: entry.props || {}, capabilities: entry.capabilities || [], requiredCapabilities: entry.requiredCapabilities || [],
            hidden: documentHidden(), clock: { epochMs: nowStamp() }, rtc: typeof window.RTCPeerConnection === "function",
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
      // Stop the call watchdogs. After a crash the subscriptions stay, so a call
      // written later still gets "worker-crashed"; otherwise release them.
      closeCalls(rec, rec.crashedStatus);
      // A boot failure continues as a fallback or "unsupported", which write
      // their own status. Every other path ends with a final status here.
      if (!rec.bootFailed && !rec.finalStatus) { rec.finalStatus = true; setStatus(rec.entry, "disposed", rec.thread, ""); }
      for (var off of rec.subs.values()) { try { off(); } catch (_) {} }
      rec.subs.clear();
      for (var off2 of rec.listeners.values()) { try { off2(); } catch (_) {} }
      rec.listeners.clear();
      var netApi = window.__gosx && window.__gosx.netplay;
      if (netApi) for (var netID of rec.nets.values()) { try { netApi.close(netID); } catch (_) {} }
      rec.nets.clear();
      try { rec.worker.onmessage = rec.worker.onerror = rec.worker.onmessageerror = null; rec.worker.terminate(); } catch (_) {}
      if (records.get(rec.entry.id) === rec) records.delete(rec.entry.id);
      if (rec.mountWaiter) { var w = rec.mountWaiter; rec.mountWaiter = null; w.bad(workerError("worker-terminated", "engine worker was terminated")); }
      if (rec.onTerminate) { try { rec.onTerminate(); } catch (_) {} }
    }

    function onWorkerCrash(rec, error) {
      if (rec.terminated) return;
      rec.crashedStatus = true;
      rec.finalStatus = true;
      setStatus(rec.entry, "crashed", rec.thread, error.code);
      failCalls(rec, "worker-crashed");
      report(rec.entry, "error", error.code, "engine worker crashed: " + error.message, error);
      dispatchDocumentEvent("worker-error", { engineID: rec.entry.id, component: rec.entry.component, code: error.code, message: error.message });
      terminate(rec);
    }

    // The page ended the engine: stop the watchdogs, say so, free the worker.
    function endRecord(rec) {
      if (rec.terminated) return;
      closeCalls(rec);
      rec.finalStatus = true;
      setStatus(rec.entry, "disposed", rec.thread, "");
      terminate(rec);
    }

    // ---- calls --------------------------------------------------------------
    //
    // A worker engine that serves calls (wasm.ServeCalls) declares the channel.
    // The page then watches "$<channel>" and, if no reply reaches
    // "$<channel>.result" within the timeout, writes the error result itself, so
    // an island is never left waiting on a worker that hung or died. Keys are
    // Go field names: ID, Name, Error, Value, Recent.

    function parseObject(encoded) {
      try { var v = JSON.parse(encoded); return v && typeof v === "object" ? v : null; } catch (_) { return null; }
    }

    function declareCalls(rec, msg) {
      if (!current(rec) || typeof msg.channel !== "string") return;
      var timeout = msg.timeout > 0 ? msg.timeout : 10000;
      var recent = msg.recent > 0 ? msg.recent : 8;
      var zero = parseObject(msg.zero) || {};
      var known = rec.calls.get(msg.channel);
      if (known) { known.timeout = timeout; known.recent = recent; known.zero = zero; return; }
      var st = { channel: msg.channel, resultSignal: msg.channel + ".result", timeout: timeout, recent: recent, zero: zero, pending: new Map(), maxCall: 0, last: null, offs: [] };
      rec.calls.set(msg.channel, st);
      // Shared signals outlive an engine, so a remounted engine finds the last
      // call and its result. The result's current value (delivered once, at
      // once) marks every call up to its ID as answered.
      var seeding = true;
      st.offs.push(rec.ctx.subscribeSignalJSON(st.resultSignal, function(encoded) {
        var result = parseObject(encoded);
        if (!result) return;
        st.last = result;
        var id = Number(result.ID) || 0;
        if (seeding && id > st.maxCall) st.maxCall = id;
        for (var entry of Array.from(st.pending)) {
          if (entry[0] <= id) { clearTimeout(entry[1].timer); st.pending.delete(entry[0]); }
        }
      }, { immediate: true }));
      seeding = false;
      // The first delivery is the call signal's current value: a call written
      // before the worker declared the channel starts its clock now.
      st.offs.push(rec.ctx.subscribeSignalJSON(st.channel, function(encoded) {
        var call = parseObject(encoded);
        var id = call ? Number(call.ID) || 0 : 0;
        if (id <= st.maxCall) return;
        st.maxCall = id;
        var name = typeof call.Name === "string" ? call.Name : "";
        if (rec.crashedStatus) { failCall(st, id, name, "worker-crashed"); return; }
        var timer = setTimeout(function() {
          if (rec.calls.get(st.channel) !== st) return;
          st.pending.delete(id);
          failCall(st, id, name, "timeout");
        }, st.timeout);
        st.pending.set(id, { name: name, timer: timer });
      }));
    }

    function failCall(st, id, name, code) {
      var result = Object.assign({}, st.zero, { ID: id, Name: name, Error: code });
      var recent = st.last && Array.isArray(st.last.Recent) ? st.last.Recent.slice() : [];
      recent.push({ ID: id, Error: code });
      result.Recent = recent.slice(-st.recent);
      st.last = result;
      writeSignal(st.resultSignal, result);
    }

    function failCalls(rec, code) {
      for (var st of rec.calls.values()) {
        for (var entry of Array.from(st.pending)) {
          clearTimeout(entry[1].timer);
          st.pending.delete(entry[0]);
          failCall(st, entry[0], entry[1].name, code);
        }
      }
    }

    function closeCalls(rec, keepSubscriptions) {
      for (var st of rec.calls.values()) {
        for (var entry of st.pending.values()) clearTimeout(entry.timer);
        st.pending.clear();
        if (!keepSubscriptions) for (var off of st.offs) { try { off(); } catch (_) {} }
      }
      if (!keepSubscriptions) rec.calls.clear();
    }

    // ---- mount ------------------------------------------------------------

    function mountRecord(rec, ctx) {
      if (rec.terminated) throw workerError("worker-terminated", "engine worker was terminated before mount");
      rec.ctx = ctx;
      ctx.threadMode = rec.thread;
      return new Promise(function(ok, bad) {
        rec.mountWaiter = { ok: ok, bad: bad };
        rec.worker.postMessage({ t: "mount" });
      }).then(function() {
        setStatus(rec.entry, "running", rec.thread, "");
        startInput(rec);
        return { dispose: function() { if (rec.terminated) closeCalls(rec); else disposeRecord(rec); } };
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
      rec.shutdownTimer = setTimeout(function() { endRecord(rec); }, SHUTDOWN_GRACE_MS);
    }

    // ---- input forwarding ---------------------------------------------------
    //
    // An engine that declares the keyboard, pointer or gamepad capability gets
    // the page's input as timestamped messages. The page samples (events and
    // gamepad polling need the main thread, and navigator.getGamepads does not
    // exist in a worker); the worker decides what to do with it. Timestamps
    // are epoch milliseconds, so the worker can place an event on its own
    // clock (__gosx.worker.toLocalTime) and measure the delay.

    var PAD_POLL_MS = 8;
    var PAD_EPSILON = 0.001;

    function wants(rec, capability) {
      var lists = [rec.entry.capabilities, rec.entry.requiredCapabilities];
      for (var i = 0; i < lists.length; i++) {
        if (Array.isArray(lists[i]) && lists[i].indexOf(capability) >= 0) return true;
      }
      return false;
    }

    function eventStamp(event) {
      return (performance.timeOrigin || 0) + (event && typeof event.timeStamp === "number" && event.timeStamp > 0 ? event.timeStamp : performance.now());
    }

    function editable(target) {
      if (!target || !target.tagName) return false;
      var tag = String(target.tagName).toUpperCase();
      return tag === "INPUT" || tag === "TEXTAREA" || tag === "SELECT" || target.isContentEditable === true;
    }

    function sendInput(rec, ev) {
      if (rec.terminated || rec.disposed) return;
      rec.stats.inputSent++;
      try { rec.worker.postMessage({ t: "input", ev: ev }); } catch (_) {}
    }

    function startInput(rec) {
      var stops = [];
      function listen(target, type, fn) {
        target.addEventListener(type, fn);
        stops.push(function() { target.removeEventListener(type, fn); });
      }
      if (wants(rec, "keyboard")) {
        var onKey = function(e) {
          if (e.repeat || editable(e.target)) return;
          sendInput(rec, { k: "key", code: String(e.code || ""), key: String(e.key || ""), down: e.type === "keydown", ts: eventStamp(e) });
        };
        listen(document, "keydown", onKey);
        listen(document, "keyup", onKey);
      }
      if (wants(rec, "pointer")) {
        var onPointer = function(e) {
          sendInput(rec, { k: "pointer", type: e.type.slice(7), x: e.clientX, y: e.clientY, mask: e.buttons | 0, pid: e.pointerId | 0, ts: eventStamp(e) });
        };
        listen(document, "pointermove", onPointer);
        listen(document, "pointerdown", onPointer);
        listen(document, "pointerup", onPointer);
      }
      var releaseAll = function() { sendInput(rec, { k: "blur", ts: nowStamp() }); };
      if (wants(rec, "keyboard") || wants(rec, "pointer") || wants(rec, "gamepad")) {
        listen(window, "blur", releaseAll);
      }
      if (wants(rec, "gamepad") && window.navigator && typeof window.navigator.getGamepads === "function") {
        stops.push(startGamepadPoll(rec, releaseAll, listen));
      }
      rec.stopInput = function() { for (var i = 0; i < stops.length; i++) { try { stops[i](); } catch (_) {} } stops = []; };
      var previous = rec.onTerminate;
      rec.onTerminate = function() { rec.stopInput(); if (previous) previous(); };
    }

    // Gamepads exist only while the page is visible and focused; poll then,
    // and send a pad only when something changed.
    function startGamepadPoll(rec, releaseAll, listen) {
      var last = new Map();
      var timer = 0;
      function snapshot(pad) {
        return { buttons: Array.prototype.map.call(pad.buttons || [], function(b) { return b && typeof b === "object" ? b.value : Number(b) || 0; }), axes: Array.prototype.slice.call(pad.axes || []) };
      }
      function changed(a, b) {
        if (!b || a.buttons.length !== b.buttons.length || a.axes.length !== b.axes.length) return true;
        for (var i = 0; i < a.buttons.length; i++) if (Math.abs(a.buttons[i] - b.buttons[i]) > PAD_EPSILON) return true;
        for (var j = 0; j < a.axes.length; j++) if (Math.abs(a.axes[j] - b.axes[j]) > PAD_EPSILON) return true;
        return false;
      }
      function poll() {
        var pads;
        try { pads = window.navigator.getGamepads() || []; } catch (_) { return; }
        var seen = new Set();
        for (var i = 0; i < pads.length; i++) {
          var pad = pads[i];
          if (!pad || pad.connected === false) continue;
          seen.add(pad.index);
          var snap = snapshot(pad);
          if (!changed(snap, last.get(pad.index))) continue;
          last.set(pad.index, snap);
          var stamp = typeof pad.timestamp === "number" && pad.timestamp > 0 ? (performance.timeOrigin || 0) + pad.timestamp : nowStamp();
          sendInput(rec, { k: "pad", index: pad.index, id: String(pad.id || ""), mapping: String(pad.mapping || ""), connected: true, buttons: snap.buttons, axes: snap.axes, ts: stamp });
        }
        for (var gone of Array.from(last.keys())) {
          if (seen.has(gone)) continue;
          last.delete(gone);
          sendInput(rec, { k: "pad", index: gone, connected: false, ts: nowStamp() });
        }
      }
      function start() { if (!timer) timer = setInterval(poll, PAD_POLL_MS); }
      function stop() { if (timer) { clearInterval(timer); timer = 0; } }
      function onVisibility() {
        if (documentHidden()) { stop(); last.clear(); releaseAll(); } else start();
      }
      listen(document, "visibilitychange", onVisibility);
      if (!documentHidden()) start();
      return stop;
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
        else if (rec.mountWaiter) {
          // The worker died while mounting: say so, and fail the calls it declared.
          rec.finalStatus = true;
          setStatus(rec.entry, "crashed", rec.thread, error.code);
          failCalls(rec, "worker-crashed");
          var w = rec.mountWaiter; rec.mountWaiter = null; w.bad(error);
        }
        else onWorkerCrash(rec, error);
      },
      mounted: function(rec) {
        if (rec.mountWaiter) { var w = rec.mountWaiter; rec.mountWaiter = null; w.ok(); }
      },
      disposed: function(rec) {
        clearTimeout(rec.shutdownTimer);
        endRecord(rec);
      },
      log: function(rec, msg) {
        var fn = console[msg.level === "error" ? "error" : msg.level === "warn" ? "warn" : "log"] || console.log;
        fn.call(console, "[gosx worker " + rec.entry.id + "]", msg.text);
      },

      // Shared signals. The worker sends a JSON batch as a transferred
      // ArrayBuffer; the page applies it through the same context method a
      // main-thread engine uses.
      signals: function(rec, msg) {
        // A worker sends the batch as a transferred buffer; a native host sends text.
        var isText = typeof msg.text === "string";
        if (!current(rec) || !(isText || msg.buf instanceof ArrayBuffer)) return;
        rec.stats.signalBatches++;
        rec.stats.signalBytes += isText ? msg.text.length : msg.buf.byteLength;
        var applied = true;
        try {
          var text = isText ? msg.text : new TextDecoder("utf-8").decode(msg.buf);
          rec.ctx.setSignalsJSON(text, msg.changedOnly === true);
        } catch (error) {
          applied = false;
          try { rec.worker.postMessage({ t: "fault", where: "signals", message: String(error && error.message || error) }); } catch (_) {}
        }
        // Acknowledge a paced view batch (wasm.View) so the worker may send the next.
        if (typeof msg.seq === "number") { try { rec.worker.postMessage({ t: "ack", seq: msg.seq, ok: applied }); } catch (_) {} }
      },
      calls: function(rec, msg) { declareCalls(rec, msg); },
      "vessel.cutaway": function(rec, msg) {
        if (!current(rec)) return;
        var fn = window.__gosx_scene3d_vessel_cutaway;
        var ok = false;
        try { ok = typeof fn === "function" && fn(String(msg.target), String(msg.nodeID), String(msg.json)) === true; } catch (error) { frameFault(rec, { kind: "vessel-cutaway", target: msg.target }, error); }
        vesselResult(rec, "cutaway", msg.target, msg.nodeID, ok);
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
          if (msg.kind === "vessel-pose") {
            // The entry point the main-thread Go call uses. A mount that is not
            // showing an external-pose vessel yet answers false; poses arrive
            // every tick, so that is counted, not raised.
            var pose = window.__gosx_scene3d_vessel_pose;
            vesselResult(rec, "pose", msg.target, "", typeof pose === "function" && pose(String(msg.target), bytes) === true);
            return;
          }
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

      // Netplay: the page owns the RTCPeerConnection (it does not exist in a
      // worker). Where the browser can transfer an RTCDataChannel to a worker,
      // the worker owns the datagrams (net.channel); otherwise the page relays
      // them (net.send, net.data). Signaling always goes through the worker's
      // Go code, over the hub, exactly as on the main thread.
      "net.open": function(rec, msg) {
        var api = window.__gosx && window.__gosx.netplay;
        var post = function(m, transfer) { if (!rec.terminated) try { rec.worker.postMessage(m, transfer || []); } catch (_) {} };
        if (!current(rec) || !api || typeof api.open !== "function") { post({ t: "net.state", nid: msg.nid, state: "failed" }); return; }
        var adopted = false;
        var cfg = {
          session: msg.session, iceServers: msg.iceServers,
          onSignal: function(event, json) { post({ t: "net.signal", nid: msg.nid, event: event, json: json }); },
          onState: function(state) { post({ t: "net.state", nid: msg.nid, state: state }); },
          onDatagram: function(bytes) { post({ t: "net.data", nid: msg.nid, buf: bytes.buffer }, [bytes.buffer]); },
        };
        // Setting window.__gosx_engine_worker_net_transfer to false keeps the channel on
        // the page and relays datagrams; use it to compare routes or to work
        // around a browser that mishandles a transferred channel.
        if (msg.canAdopt === true && window.__gosx_engine_worker_net_transfer !== false) {
          cfg.adopt = function(dc) {
            try { rec.worker.postMessage({ t: "net.channel", nid: msg.nid, dc: dc }, [dc]); adopted = true; return true; } catch (_) { return false; }
          };
        }
        var id = api.open(cfg);
        if (!(id > 0)) { post({ t: "net.state", nid: msg.nid, state: "failed" }); return; }
        rec.nets.set(msg.nid, id);
        rec.netModes[msg.nid] = adopted ? "transfer" : "relay";
        post({ t: "net.mode", nid: msg.nid, mode: rec.netModes[msg.nid] });
      },
      "net.signal.in": function(rec, msg) {
        var api = window.__gosx && window.__gosx.netplay, id = rec.nets.get(msg.nid);
        if (api && id) api.signal(id, msg.event, msg.json);
      },
      "net.send": function(rec, msg) {
        var api = window.__gosx && window.__gosx.netplay, id = rec.nets.get(msg.nid);
        if (api && id && msg.buf instanceof ArrayBuffer) api.send(id, new Uint8Array(msg.buf), msg.buf.byteLength);
      },
      "net.worker-state": function(rec, msg) {
        var api = window.__gosx && window.__gosx.netplay, id = rec.nets.get(msg.nid);
        if (api && id && typeof api.adopted === "function") api.adopted(id, msg.state);
      },
      "net.close": function(rec, msg) {
        var api = window.__gosx && window.__gosx.netplay, id = rec.nets.get(msg.nid);
        rec.nets.delete(msg.nid);
        if (api && id) api.close(id);
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

    // The worker's vessel calls return at once, so the page tells the worker
    // whenever the answer for a target changes (and on the first call). Go then
    // sees the same error as on the main thread, one round trip late.
    function vesselResult(rec, kind, target, nodeID, ok) {
      if (!ok) rec.stats.vesselRejected++;
      var key = kind + "|" + target + "|" + nodeID;
      if (rec.vessel.get(key) === ok) return;
      rec.vessel.set(key, ok);
      try { rec.worker.postMessage({ t: "vessel.result", kind: kind, target: String(target), nodeID: String(nodeID || ""), ok: ok }); } catch (_) {}
    }

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
        // "worker" or "native": where the engine runs.
        thread: rec.thread,
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
        // How each netplay connection moves datagrams: "transfer" (the worker owns
        // the RTCDataChannel) or "relay" (the page forwards them), by connection id.
        netModes: function() { return Object.assign({}, rec.netModes); },
        terminate: function() { terminate(rec); },
        worker: rec.worker,
      };
    }

    // The runtime calls this when the page changes. Engines that the next page
    // reuses keep their worker; the others shut down through Go's Dispose.
    function disposePage(reuseIDs) {
      for (var rec of Array.from(records.values())) {
        if (reuseIDs instanceof Set && reuseIDs.has(rec.entry.id)) continue;
        if (rec.ctx) disposeRecord(rec); else endRecord(rec);
      }
    }

    return {
      version: 1,
      protocol: PROTOCOL,
      supported: function() { return unsupportedReason(null) === null || nativeBridge() !== null; },
      nativeSupported: function() { return nativeBridge() !== null; },
      resolve: resolve,
      get: get,
      ids: function() { return Array.from(records.keys()); },
      disposePage: disposePage,
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
    var inputHandlers = new Set();
    var mountListeners = new Map();   // lid -> callback(json)
    var nextLID = 1;
    var hidden = false;
    var nextViewSeq = 1;
    var viewAcks = new Map();         // seq -> {token, fn}
    var vesselOK = new Map();         // "kind|target|nodeID" -> the page's last answer

    function post(msg, transfer) { scope.postMessage(msg, transfer || []); }

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
      // Both realms measure epoch time as timeOrigin + now(), so the two clocks
      // differ only by their time origins.
      toLocalTime: function(epochMs) { return epochMs - (scope.performance.timeOrigin || 0); },
      post: function(name, json) { post({ t: "app", name: String(name), json: json === undefined ? undefined : String(json) }); },
      postBytes: function(name, bytes) { var buf = ownedBuffer(bytes); post({ t: "app", name: String(name), buf: buf }, [buf]); },
      onMessage: function(fn) { appHandlers.add(fn); return function() { appHandlers.delete(fn); }; },
      onVisibility: function(fn) { visibilityHandlers.add(fn); return function() { visibilityHandlers.delete(fn); }; },
      // Page input as JSON strings: {k:"key"|"pointer"|"pad"|"blur", ..., at, recv}.
      // "at" is when the event happened and "recv" when the worker got it, both
      // in this worker's performance.now() milliseconds.
      onInput: function(fn) { inputHandlers.add(fn); return function() { inputHandlers.delete(fn); }; },
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
    // Vessel pose and cutaway: scene.DispatchVesselPose and DispatchVesselCutaway
    // look these globals up. In a worker they forward to the page's controller.
    // They return what the page last said about that target (false until it
    // has answered once), so Go gets the main thread's error while the vessel is
    // not ready: "no external-pose vessel yet".
    scope.__gosx_scene3d_vessel_pose = function(target, bytes) {
      postFrame("vessel-pose", target, bytes);
      return vesselOK.get("pose|" + target + "|") === true;
    };
    scope.__gosx_scene3d_vessel_cutaway = function(target, nodeID, json) {
      post({ t: "vessel.cutaway", target: String(target), nodeID: String(nodeID), json: String(json) });
      return vesselOK.get("cutaway|" + target + "|" + nodeID) === true;
    };
    scene3d.applyInstanceStream = function(target, bytes) {
      postFrame("instance", target, bytes);
      return { applied: true, queued: true };
    };

    var netplayShim = createNetplayShim(scope, post, function() { return init; });
    var gosx = scope.__gosx = scope.__gosx || {};
    gosx.netplay = netplayShim.api;
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
        // A paced view batch (wasm.View): like setSignalsJSON, but the page
        // acknowledges it, and then onApplied(token) runs.
        sendView: function(encoded, changedOnly, token, onApplied) {
          var bytes = encoder.encode(String(encoded));
          var seq = nextViewSeq++;
          viewAcks.set(seq, { token: token, fn: onApplied });
          post({ t: "signals", changedOnly: changedOnly === true, buf: bytes.buffer, seq: seq }, [bytes.buffer]);
          return undefined;
        },
        // Tell the page this engine serves calls on a channel (wasm.ServeCalls),
        // so its watchdog can fail a call the worker never answers.
        declareCalls: function(channel, timeoutMS, recent, zeroResult) {
          post({ t: "calls", channel: String(channel), timeout: Number(timeoutMS) || 0, recent: Number(recent) || 0, zero: String(zeroResult || "{}") });
        },
        subscribeSignal: function(name, handler, options) {
          return ctx.subscribeSignalJSON(name, function(encoded, signalName) { handler(JSON.parse(encoded), signalName); }, options);
        },
        subscribeSignalJSON: function(name, handler, options) {
          var sid = nextSID++;
          // {immediate: true} also delivers the current value once (Go passes
          // the option explicitly; the default stays "changes only").
          signalHandlers.set(sid, { name: String(name), fn: handler, immediate: !!(options && options.immediate === true) });
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
      viewAcks.clear(); // late acknowledgements must not reach released Go callbacks
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
      "vessel.result": function(msg) { vesselOK.set(String(msg.kind) + "|" + String(msg.target) + "|" + String(msg.nodeID || ""), msg.ok === true); },
      ack: function(msg) {
        var waiter = viewAcks.get(msg.seq);
        if (!waiter) return;
        viewAcks.delete(msg.seq);
        try { waiter.fn(waiter.token, msg.ok !== false); } catch (_) { /* the Go callback was released */ }
      },
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
        if (!entry || (msg.initial && !entry.immediate)) return;
        try { entry.fn(msg.json, name); } catch (error) { logToPage("error", error); }
      },
      app: function(msg) {
        var bytes = msg.buf instanceof ArrayBuffer ? new Uint8Array(msg.buf) : undefined;
        for (var fn of Array.from(appHandlers)) { try { fn(String(msg.name), msg.json, bytes); } catch (error) { logToPage("error", error); } }
      },
      input: function(msg) {
        var ev = msg.ev;
        if (!ev || typeof ev !== "object") return;
        ev.at = workerAPI.toLocalTime(ev.ts);
        ev.recv = scope.performance.now();
        if (inputHandlers.size === 0) return;
        var json = JSON.stringify(ev);
        for (var fn of Array.from(inputHandlers)) { try { fn(json); } catch (error) { logToPage("error", error); } }
      },
      "net.signal": function(msg) { netplayShim.onSignal(msg); },
      "net.state": function(msg) { netplayShim.onState(msg); },
      "net.mode": function(msg) { netplayShim.onMode(msg); },
      "net.channel": function(msg) { netplayShim.onChannel(msg); },
      "net.data": function(msg) { netplayShim.onData(msg); },
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

  // The worker's stand-in for the page's netplay binding (netplay.ts), with
  // the same polled API Go's netplay/client already uses. RTCPeerConnection does
  // not exist in a worker, so the page owns it and this shim asks the page
  // to open and signal it. Datagrams then take one of two routes:
  //   transfer  the page hands the RTCDataChannel to this worker at creation;
  //             datagrams go straight between the worker and the network and
  //             never touch the page's main thread.
  //   relay     the browser refused the transfer (or has no RTCDataChannel in
  //             workers); the page forwards datagrams over the port.
  function createNetplayShim(scope, post, getInit) {
    var MAX_PACKET = 1200, QUEUE_SLOTS = 128, MAX_BUFFERED = 16 * 1024, MAX_CONNECTIONS = 8;
    var conns = new Map();
    var nextNID = 1;

    function supported() {
      var init = getInit();
      return !!(init && init.rtc === true);
    }

    function notify(c, next) {
      if (c.state === next || c.state === "closed" || c.state === "failed") return;
      c.state = next;
      try { if (c.cfg.onState) c.cfg.onState(next); } catch (_) {}
    }

    function pushIncoming(c, bytes) {
      if (!bytes || bytes.length === 0 || bytes.length > MAX_PACKET || c.count === QUEUE_SLOTS) { c.receiveDropped++; return; }
      var slot = (c.head + c.count) % QUEUE_SLOTS;
      c.queue.set(bytes, slot * MAX_PACKET);
      c.lens[slot] = bytes.length;
      c.count++;
      c.received++;
    }

    function open(cfg) {
      if (!cfg || typeof cfg.onSignal !== "function") return 0;
      var fail = function() { try { if (cfg.onState) cfg.onState("failed"); } catch (_) {} return 0; };
      if (!supported() || conns.size >= MAX_CONNECTIONS) return fail();
      var nid = nextNID++;
      var c = {
        nid: nid, cfg: cfg, state: "connecting", mode: "pending", dc: null,
        queue: new Uint8Array(QUEUE_SLOTS * MAX_PACKET), lens: new Int32Array(QUEUE_SLOTS), head: 0, count: 0,
        received: 0, receiveDropped: 0, sent: 0, sendDropped: 0,
      };
      conns.set(nid, c);
      post({
        t: "net.open", nid: nid, session: String(cfg.session || ""), iceServers: Array.isArray(cfg.iceServers) ? cfg.iceServers : [],
        canAdopt: typeof scope.RTCDataChannel === "function",
      });
      return nid;
    }

    function release(c) {
      if (c.dc) {
        try { c.dc.onopen = c.dc.onmessage = c.dc.onclose = c.dc.onerror = null; c.dc.close(); } catch (_) {}
        c.dc = null;
      }
    }

    return {
      onSignal: function(msg) {
        var c = conns.get(msg.nid);
        if (c && c.state !== "closed" && c.state !== "failed") { try { c.cfg.onSignal(msg.event, msg.json); } catch (_) {} }
      },
      onState: function(msg) {
        var c = conns.get(msg.nid);
        if (!c) return;
        // The page reports a failed connection as "failed" until it has seen
        // the channel open. When this worker saw it open, that is a closure.
        if (msg.state === "failed" && c.state === "open") { notify(c, "closed"); release(c); return; }
        if (msg.state === "failed") { notify(c, "failed"); release(c); return; }
        if (msg.state === "closed") { notify(c, "closed"); release(c); return; }
        if (msg.state === "open" && c.mode === "relay") notify(c, "open");
      },
      onMode: function(msg) {
        var c = conns.get(msg.nid);
        if (c && c.mode === "pending") c.mode = msg.mode === "transfer" ? "transfer" : "relay";
      },
      onChannel: function(msg) {
        var c = conns.get(msg.nid);
        var dc = msg.dc;
        if (!c || !dc || c.state === "closed" || c.state === "failed") { try { if (dc) dc.close(); } catch (_) {} return; }
        c.mode = "transfer";
        c.dc = dc;
        dc.binaryType = "arraybuffer";
        dc.onmessage = function(ev) {
          var d = ev.data;
          pushIncoming(c, d instanceof ArrayBuffer ? new Uint8Array(d) : ArrayBuffer.isView(d) ? new Uint8Array(d.buffer, d.byteOffset, d.byteLength) : null);
        };
        dc.onopen = function() { notify(c, "open"); post({ t: "net.worker-state", nid: c.nid, state: "open" }); };
        dc.onclose = function() {
          var wasConnecting = c.state === "connecting";
          if (wasConnecting) notify(c, "failed"); else notify(c, "closed");
          post({ t: "net.worker-state", nid: c.nid, state: "closed" });
        };
        if (dc.readyState === "open") dc.onopen();
      },
      onData: function(msg) {
        var c = conns.get(msg.nid);
        if (!c || !(msg.buf instanceof ArrayBuffer)) return;
        if (c.state === "connecting") notify(c, "open");   // relay: data implies an open channel
        pushIncoming(c, new Uint8Array(msg.buf));
      },
      api: {
        version: 1,
        mode: function(id) { var c = conns.get(id); return c ? c.mode : "closed"; },
        supported: supported,
        open: open,
        signal: function(id, event, json) {
          var c = conns.get(id);
          if (!c || c.state === "closed" || c.state === "failed") return;
          post({ t: "net.signal.in", nid: id, event: String(event), json: String(json) });
        },
        send: function(id, bytes, n) {
          var c = conns.get(id);
          if (!c || c.state !== "open" || !bytes) return false;
          if (!(n > 0) || n > MAX_PACKET || n > bytes.length) { c.sendDropped++; return false; }
          if (c.mode === "transfer" && c.dc) {
            if (c.dc.bufferedAmount > MAX_BUFFERED) { c.sendDropped++; return false; }
            try { c.dc.send(bytes.subarray(0, n)); c.sent++; return true; } catch (_) { c.sendDropped++; return false; }
          }
          var copy = bytes.slice(0, n);
          post({ t: "net.send", nid: id, buf: copy.buffer }, [copy.buffer]);
          c.sent++;
          return true;
        },
        recv: function(id, dst) {
          var c = conns.get(id);
          if (!c || c.count === 0 || !dst) return -1;
          var n = Math.min(c.lens[c.head], dst.length);
          dst.set(c.queue.subarray(c.head * MAX_PACKET, c.head * MAX_PACKET + n));
          c.head = (c.head + 1) % QUEUE_SLOTS;
          c.count--;
          return n;
        },
        state: function(id) { var c = conns.get(id); return c ? c.state : "closed"; },
        stats: function(id) {
          var c = conns.get(id);
          return c ? { received: c.received, receiveDropped: c.receiveDropped, sent: c.sent, sendDropped: c.sendDropped, queued: c.count, mode: c.mode } : null;
        },
        close: function(id) {
          var c = conns.get(id);
          if (!c) return;
          if (c.state !== "failed") c.state = "closed";
          release(c);
          conns.delete(id);
          post({ t: "net.close", nid: id });
        },
      },
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
