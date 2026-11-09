// GoSX drive spike probe (throwaway, never shipped). It replaces the realm's
// timers, animation frames, idle callbacks, performance.now, performance.timeOrigin,
// Date and Event.prototype.timeStamp with versions that read one virtual clock,
// so the driver moves time with __drive.step(ms) or __drive.frames(n).
//
// Pages: Page.addScriptToEvaluateOnNewDocument (runs before page scripts, in
// every frame, after every reload). Workers: Runtime.evaluate while the worker
// waits for the debugger. The driver replaces the trailing config object.
//
// Same-origin iframes share the top window's clock. The top window keeps the
// clock across reloads in sessionStorage. Sockets: inbound WebSocket messages
// queue and are delivered only at a frame boundary of a step.
(function (G, CFG) {
  "use strict";
  if (G.__drive) return;
  var KEY = "__gosx_drive_clock";
  var isWindow = typeof G.Window === "function" && G instanceof G.Window;
  var isTop = false;
  try { isTop = isWindow && G === G.top; } catch (e) { isTop = false; }
  var perf = G.performance;
  var RealDate = G.Date;
  var R = {
    setTimeout: G.setTimeout.bind(G),
    clearTimeout: G.clearTimeout.bind(G),
    raf: typeof G.requestAnimationFrame === "function" ? G.requestAnimationFrame.bind(G) : null,
    perfNow: perf.now.bind(perf),
    timeOrigin: perf.timeOrigin,
    dateNow: RealDate.now.bind(RealDate),
    MessageChannel: G.MessageChannel,
  };

  // ---- the clock core: shared by every same-origin frame of a tab
  var core = null, shared = false;
  if (isWindow && !isTop) {
    try { if (G.top.__drive && G.top.__drive.__core) { core = G.top.__drive.__core; shared = true; } } catch (e) { /* cross-origin frame: own clock */ }
  }
  var interval = CFG.interval > 0 ? CFG.interval : 1000 / 60;
  var frameAt = function (k) { return Math.round(k * interval * 1000) / 1000; };
  if (!core) {
    var restored = null;
    if (isTop && CFG.persist !== false) {
      try { var s = G.sessionStorage.getItem(KEY); if (s) restored = JSON.parse(s); } catch (e) { restored = null; }
    }
    var v0 = restored ? restored.V : (CFG.v0 || 0);
    core = {
      V: v0,
      wall: restored ? restored.wall : (CFG.wall != null ? CFG.wall : R.dateNow()),
      frameIndex: restored ? restored.frameIndex : Math.floor(v0 / interval + 1e-9),
      seq: 0, nextId: 0,
      timers: [], timerById: new Map(),
      frames: [], idles: [], inbox: [],
      gpu: false, stepping: false, navigating: false, nesting: 0,
      loads: restored ? (restored.loads || 1) + 1 : 1,
      restoredFrom: restored ? restored.V : null,
      cssSeek: !!CFG.cssSeek, cssSeen: new WeakMap(), cssV: v0,
      stats: { steps: 0, timersRun: 0, boundaries: 0, frames: 0, frameCallbacks: 0, idles: 0, delivered: 0, queued: 0,
        realFrames: 0, presentTimeouts: 0, gpuSubmits: 0, workerPosted: 0, workerReceived: 0,
        postedToWorkers: 0, receivedFromWorkers: 0, cssSeeks: 0 },
    };
  }
  var realm = { G: G, O: core.V, doc: isWindow ? G.document : null };
  var docId = Math.random().toString(36).slice(2);
  var alive = function (doc) { return !doc || !!doc.defaultView; };
  var vPerfNow = function () { return core.V - realm.O; };
  var vDateNow = function () { return Math.floor(core.wall + core.V); };

  // ---- real tasks: each callback runs in its own task, so microtasks drain between callbacks
  var mc = new R.MessageChannel(), jobs = [];
  mc.port1.onmessage = function () {
    var j = jobs.shift();
    try { j.fn(); } finally { j.done(); }
  };
  var inTask = function (fn) { return new Promise(function (done) { jobs.push({ fn: fn, done: done }); mc.port2.postMessage(0); }); };

  // ---- timers
  function insertTimer(t) {
    var a = core.timers, lo = 0, hi = a.length;
    while (lo < hi) { var m = (lo + hi) >> 1, x = a[m]; if (x.due < t.due || (x.due === t.due && x.seq < t.seq)) lo = m + 1; else hi = m; }
    a.splice(lo, 0, t);
  }
  function addTimer(fn, delay, args, repeat) {
    if (typeof fn !== "function") { var src = String(fn); fn = function () { (0, G.eval)(src); }; }
    delay = +delay; if (!(delay > 0)) delay = 0;
    var level = core.nesting + 1;
    if (level > 5 && delay < 4) delay = 4; // HTML timer nesting clamp
    var id = ++core.nextId;
    var t = { id: id, due: core.V + delay, seq: ++core.seq, fn: fn, args: args, every: repeat ? delay : -1, level: level, doc: realm.doc, realm: realm };
    core.timerById.set(id, t);
    insertTimer(t);
    return id;
  }
  function clearTimer(id) {
    var t = core.timerById.get(+id);
    if (!t) return;
    core.timerById.delete(+id);
    var i = core.timers.indexOf(t);
    if (i >= 0) core.timers.splice(i, 1);
  }
  G.setTimeout = function setTimeout(fn, delay) { return addTimer(fn, delay, Array.prototype.slice.call(arguments, 2), false); };
  G.setInterval = function setInterval(fn, delay) { return addTimer(fn, delay, Array.prototype.slice.call(arguments, 2), true); };
  G.clearTimeout = function clearTimeout(id) { clearTimer(id); };
  G.clearInterval = function clearInterval(id) { clearTimer(id); };

  // ---- animation frames and idle callbacks
  if (R.raf) {
    G.requestAnimationFrame = function requestAnimationFrame(cb) {
      if (typeof cb !== "function") throw new TypeError("requestAnimationFrame: callback is not a function");
      var id = ++core.nextId;
      core.frames.push({ id: id, cb: cb, doc: realm.doc, realm: realm });
      return id;
    };
    G.cancelAnimationFrame = function cancelAnimationFrame(id) {
      for (var i = 0; i < core.frames.length; i++) if (core.frames[i].id === +id) { core.frames.splice(i, 1); return; }
    };
  }
  if (typeof G.requestIdleCallback === "function") {
    G.requestIdleCallback = function requestIdleCallback(cb, opts) {
      var id = ++core.nextId;
      core.idles.push({ id: id, cb: cb, doc: realm.doc, realm: realm, deadline: opts && opts.timeout > 0 ? core.V + opts.timeout : Infinity });
      return id;
    };
    G.cancelIdleCallback = function cancelIdleCallback(id) {
      for (var i = 0; i < core.idles.length; i++) if (core.idles[i].id === +id) { core.idles.splice(i, 1); return; }
    };
  }

  // ---- performance, Date, Event.timeStamp
  try { Object.defineProperty(perf, "now", { value: function now() { return vPerfNow(); }, configurable: true, writable: true }); } catch (e) {}
  try { Object.defineProperty(perf, "timeOrigin", { get: function () { return core.wall + realm.O; }, configurable: true }); } catch (e) {}
  function VDate() {
    if (!new.target) return new RealDate(vDateNow()).toString();
    var a = Array.prototype.slice.call(arguments);
    return Reflect.construct(RealDate, a.length ? a : [vDateNow()], new.target);
  }
  VDate.prototype = RealDate.prototype;
  VDate.now = function now() { return vDateNow(); };
  VDate.parse = RealDate.parse;
  VDate.UTC = RealDate.UTC;
  try { Object.defineProperty(RealDate.prototype, "constructor", { value: VDate, configurable: true, writable: true }); } catch (e) {}
  G.Date = VDate;
  if (G.Event) {
    var stamps = new WeakMap();
    Object.defineProperty(G.Event.prototype, "timeStamp", { configurable: true, enumerable: true, get: function () {
      var v = stamps.get(this);
      if (v === undefined) { v = vPerfNow(); stamps.set(this, v); }
      return v;
    } });
  }

  // ---- GPU canvas detection (a GPU page waits for real frames) and WebGPU submits
  [G.HTMLCanvasElement, G.OffscreenCanvas].forEach(function (C) {
    if (!C || !C.prototype.getContext) return;
    var gc = C.prototype.getContext;
    C.prototype.getContext = function (type) {
      var ctx = gc.apply(this, arguments);
      if (ctx && /webgl|webgpu/.test(String(type))) core.gpu = true;
      return ctx;
    };
  });
  if (G.GPUQueue && G.GPUQueue.prototype.submit) {
    var submit = G.GPUQueue.prototype.submit;
    G.GPUQueue.prototype.submit = function () { core.stats.gpuSubmits++; return submit.apply(this, arguments); };
  }

  // ---- worker message accounting (the driver uses it to prove both sides drained)
  if (isWindow && G.Worker) {
    var RW = G.Worker, pm = RW.prototype.postMessage;
    var DW = function Worker(url, opts) {
      var w = Reflect.construct(RW, [url, opts], new.target || DW);
      w.addEventListener("message", function () { core.stats.receivedFromWorkers++; });
      return w;
    };
    DW.prototype = RW.prototype;
    G.Worker = DW;
    RW.prototype.postMessage = function () { core.stats.postedToWorkers++; return pm.apply(this, arguments); };
  }
  if (!isWindow && typeof G.postMessage === "function" && typeof G.importScripts === "function") {
    var wpm = G.postMessage.bind(G);
    G.postMessage = function postMessage() { core.stats.workerPosted++; return wpm.apply(null, arguments); };
    G.addEventListener("message", function () { core.stats.workerReceived++; });
  }

  // ---- WebSocket: inbound messages queue until a step delivers them
  if (G.WebSocket && CFG.sockets !== false) {
    var RealWS = G.WebSocket;
    var DriveWS = function WebSocket(url, protocols) {
      var ws = protocols === undefined ? Reflect.construct(RealWS, [url], new.target || DriveWS) : Reflect.construct(RealWS, [url, protocols], new.target || DriveWS);
      ws.addEventListener("message", function (ev) {
        if (ws.__driveDelivering) return;
        ev.stopImmediatePropagation();
        core.stats.queued++;
        core.inbox.push({ ws: ws, data: ev.data, origin: ev.origin, lastEventId: ev.lastEventId, ME: G.MessageEvent, doc: realm.doc });
      });
      return ws;
    };
    DriveWS.prototype = RealWS.prototype;
    ["CONNECTING", "OPEN", "CLOSING", "CLOSED"].forEach(function (k) { DriveWS[k] = RealWS[k]; });
    G.WebSocket = DriveWS;
  }

  // ---- reload: halt stepping and keep the clock in sessionStorage
  var save = function () {
    try { G.sessionStorage.setItem(KEY, JSON.stringify({ V: core.V, wall: core.wall, frameIndex: core.frameIndex, loads: core.loads })); } catch (e) {}
  };
  if (isTop) {
    G.addEventListener("beforeunload", function () { core.navigating = true; if (CFG.persist !== false) save(); }, true);
    G.addEventListener("pagehide", function () { core.navigating = true; if (CFG.persist !== false) save(); }, true);
  }

  // ---- CSS animations: with the document timeline frozen (Animation.setPlaybackRate(0)),
  // advance each animation's currentTime by the virtual time since it was last seeked.
  // cssMode "pause" (the alternative): no timeline freeze; pause each animation when
  // first seen, set currentTime from the virtual clock, and call finish() at its end.
  function seekCSS() {
    if (!core.cssSeek || !isWindow || !G.document.getAnimations) return;
    var list = G.document.getAnimations();
    for (var i = 0; i < list.length; i++) {
      var a = list[i], last = core.cssSeen.get(a);
      if (CFG.cssMode === "pause") {
        if (last === undefined) { last = { base: core.V, t0: a.currentTime || 0, done: false }; core.cssSeen.set(a, last); a.pause(); }
        if (last.done) continue;
        var end = a.effect ? a.effect.getComputedTiming().endTime : Infinity;
        var want = last.t0 + (core.V - last.base);
        if (want >= end) { last.done = true; a.finish(); } else a.currentTime = want;
        core.stats.cssSeeks++;
        continue;
      }
      if (last === undefined) { core.cssSeen.set(a, core.V); continue; }
      var d = core.V - last;
      if (d > 0 && a.playState !== "paused" && a.currentTime !== null) { a.currentTime = a.currentTime + d; core.stats.cssSeeks++; }
      core.cssSeen.set(a, core.V);
    }
  }

  // ---- stepping
  async function runTimer() {
    var t = core.timers.shift();
    if (t.due > core.V) core.V = t.due;
    if (t.every >= 0) {
      t.level++;
      var d = t.every; if (t.level > 5 && d < 4) d = 4;
      t.due = core.V + d; t.seq = ++core.seq;
      insertTimer(t);
    } else core.timerById.delete(t.id);
    if (!alive(t.doc)) { clearTimer(t.id); return; }
    core.stats.timersRun++;
    await inTask(function () {
      core.nesting = t.level;
      try { t.fn.apply(t.realm.G, t.args); } finally { core.nesting = 0; }
    });
    seekCSS(); // registers animations a timer started at the instant it started them
  }
  async function deliver() {
    while (core.inbox.length && !core.navigating) {
      var m = core.inbox.shift();
      if (!alive(m.doc)) continue;
      core.stats.delivered++;
      await inTask(function () {
        m.ws.__driveDelivering = true;
        try { m.ws.dispatchEvent(new m.ME("message", { data: m.data, origin: m.origin, lastEventId: m.lastEventId })); } finally { m.ws.__driveDelivering = false; }
      });
    }
  }
  async function runFrame(at) {
    seekCSS();
    var batch = core.frames;
    core.frames = [];
    if (batch.length) {
      core.stats.frames++;
      core.stats.frameCallbacks += batch.length;
      if (core.gpu && R.raf && !CFG.noRealFrames) {
        // GPU page: run the frame's callbacks inside one real animation frame, then
        // wait for a task after it, which runs once that frame's rendering update is done.
        await new Promise(function (resolve) {
          var done = false, ran = new Array(batch.length);
          var runOne = function (i) { if (ran[i]) return; ran[i] = true; var f = batch[i]; if (!alive(f.doc)) return; core.V = at; f.cb.call(f.realm.G, at - f.realm.O); };
          var finish = function () { if (done) return; done = true; R.clearTimeout(guard); resolve(); };
          var guard = R.setTimeout(function () { core.stats.presentTimeouts++; for (var i = 0; i < batch.length; i++) runOne(i); finish(); }, 250);
          batch.forEach(function (_, i) { R.raf(function () { runOne(i); }); });
          R.raf(function () { core.stats.realFrames++; inTask(function () {}).then(finish); });
        });
      } else {
        for (var i = 0; i < batch.length; i++) {
          var f = batch[i];
          if (!alive(f.doc)) continue;
          await inTask(function () { f.cb.call(f.realm.G, at - f.realm.O); });
        }
      }
    }
    if (core.idles.length) {
      var ib = core.idles; core.idles = [];
      for (var j = 0; j < ib.length; j++) {
        var it = ib[j];
        if (!alive(it.doc)) continue;
        core.stats.idles++;
        await inTask(function () {
          var end = R.perfNow() + 10;
          it.cb.call(it.realm.G, { didTimeout: false, timeRemaining: function () { return Math.max(0, end - R.perfNow()); } });
        });
      }
    }
  }
  async function step(ms) {
    if (core.stepping) throw new Error("drive: a step is already running");
    core.stepping = true;
    core.stats.steps++;
    var from = core.V, target = core.V + Math.max(0, +ms || 0), t0 = R.perfNow();
    var b0 = Object.assign({}, core.stats), sameInstant = 0, lastV = -1;
    seekCSS(); // animations started between steps started at this instant
    try {
      for (;;) {
        if (core.navigating) break;
        var nt = core.timers.length ? core.timers[0].due : Infinity;
        var nf = frameAt(core.frameIndex + 1);
        if (nt < nf && nt <= target + 1e-9) {
          if (core.V === lastV) { if (++sameInstant > 10000) throw new Error("drive: livelock, 10000 timers at one instant"); } else { sameInstant = 0; lastV = core.V; }
          await runTimer();
          continue;
        }
        if (nf <= target + 1e-9) {
          core.V = nf; core.frameIndex++; core.stats.boundaries++;
          await deliver();
          while (!core.navigating && core.timers.length && core.timers[0].due <= core.V) await runTimer();
          if (!core.navigating) await runFrame(nf);
          continue;
        }
        if (target > core.V) core.V = target;
        seekCSS();
        break;
      }
    } finally { core.stepping = false; }
    var s = core.stats;
    return { from: from, to: core.V, advanced: core.V - from, navigated: core.navigating, realMS: R.perfNow() - t0,
      boundaries: s.boundaries - b0.boundaries, frames: s.frames - b0.frames, frameCallbacks: s.frameCallbacks - b0.frameCallbacks,
      timersRun: s.timersRun - b0.timersRun, delivered: s.delivered - b0.delivered, realFrames: s.realFrames - b0.realFrames,
      presentTimeouts: s.presentTimeouts - b0.presentTimeouts, gpuSubmits: s.gpuSubmits - b0.gpuSubmits };
  }

  var api = {
    __core: core,
    version: 1,
    shared: shared,
    docId: docId,
    now: function () { return core.V; },
    perfNow: vPerfNow,
    dateNow: vDateNow,
    origin: function () { return realm.O; },
    step: function (ms) { return step(ms); },
    frames: function (n) { return step(frameAt(core.frameIndex + Math.max(0, n | 0)) - core.V); },
    pending: function () {
      return { timers: core.timers.length, nextTimerIn: core.timers.length ? core.timers[0].due - core.V : null,
        frames: core.frames.length, idles: core.idles.length, inbox: core.inbox.length, stepping: core.stepping };
    },
    stats: function () {
      return Object.assign({}, core.stats, { V: core.V, gpu: core.gpu, loads: core.loads, restoredFrom: core.restoredFrom,
        frameIndex: core.frameIndex, shared: shared, docId: docId, wall: core.wall, origin: realm.O });
    },
    realNow: R.perfNow,
    realFrame: function () { return new Promise(function (r) { if (R.raf) R.raf(function () { inTask(function () {}).then(r); }); else R.setTimeout(r, 16); }); },
    realSleep: function (ms) { return new Promise(function (r) { R.setTimeout(r, ms); }); },
    realDateNow: R.dateNow,
    realTimeOrigin: R.timeOrigin,
    setCSSSeek: function (on) { core.cssSeek = !!on; },
    resetSession: function () { try { G.sessionStorage.removeItem(KEY); } catch (e) {} },
  };
  Object.defineProperty(G, "__drive", { value: api, configurable: true });
})(globalThis, /*DRIVE_CONFIG*/{});
