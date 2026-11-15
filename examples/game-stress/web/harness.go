package web

// harnessJS runs before the GoSX bootstrap. It measures the page's main
// thread frame by frame:
//
//   - raf: the summed duration of every requestAnimationFrame callback of the
//     frame (the Scene3D renderer's frame, which runs a main-thread engine's
//     frame hook, plus any other loop);
//   - the renderer's own performance.measure entries, enabled by
//     window.__gosx_scene3d_perf: scene3d-bundle (scene planning),
//     scene3d-model-animations and scene3d-render (render submission, CPU);
//   - msg: time spent in Web Worker message handlers (the worker engine's
//     frames and posts);
//   - gap: the time between this frame's start and the previous one's.
//
// The engine's own per-frame records (simulation, AI, animation, GC) arrive
// through window.__stressGoSlots on the main thread and as "stress.frames"
// messages from a worker. __stress.collect() returns everything as JSON.
const harnessJS = `
(function(){
"use strict";
window.__gosx_scene3d_perf = true;
var REC = 20, RING = 1024;
var S = window.__stress = { frames: [], go: [], gpu: [], latency: [], errors: [], probes: [], hashes: [], audio: {}, t0: performance.now() };
var buf = new ArrayBuffer(RING * REC * 8), slots = [];
for (var i = 0; i < RING; i++) slots.push(new Uint8Array(buf, i * REC * 8, REC * 8));
window.__stressGoSlots = slots;
var goView = new Float64Array(buf), goSeen = 0;
function drainMainGo() {
  for (var i = 0; i < RING; i++) {
    var f = goView[i * REC];
    if (f > goSeen) S.go.push(Array.prototype.slice.call(goView, i * REC, (i + 1) * REC));
  }
  S.go.sort(function(a, b) { return a[0] - b[0]; });
  var dedup = [];
  for (var k = 0; k < S.go.length; k++) if (!dedup.length || dedup[dedup.length - 1][0] !== S.go[k][0]) dedup.push(S.go[k]);
  S.go = dedup;
  if (dedup.length) goSeen = dedup[dedup.length - 1][0];
}
setInterval(drainMainGo, 500);
document.addEventListener("gosx:engine:message", function(e) {
  var d = e.detail || {};
  if (d.name !== "stress.frames" || !(d.data instanceof ArrayBuffer)) return;
  var v = new Float64Array(d.data);
  for (var i = 0; i + REC <= v.length; i += REC) S.go.push(Array.prototype.slice.call(v, i, i + REC));
});
var names = { "scene3d-bundle": "bundle", "scene3d-model-animations": "anim", "scene3d-render": "render" };
var raf = window.requestAnimationFrame.bind(window);
var cur = null, inRAF = false, lastStart = 0, pendingLatency = [];
function flush() {
  if (!cur) return;
  var entries = performance.getEntriesByType("measure");
  for (var i = 0; i < entries.length; i++) {
    var key = names[entries[i].name];
    if (key) cur[key] = (cur[key] || 0) + entries[i].duration;
  }
  performance.clearMeasures();
  S.frames.push(cur);
  if (S.frames.length > 20000) S.frames.splice(0, 5000);
  cur = null;
}
window.requestAnimationFrame = function(cb) {
  return raf(function(ts) {
    var start = performance.now();
    if (!cur || cur.ts !== ts) {
      flush();
      cur = { ts: ts, at: start, raf: 0, msg: msgAcc, gap: lastStart ? start - lastStart : 0, n: 0 };
      msgAcc = 0;
      lastStart = start;
    }
    inRAF = true;
    try { cb(ts); } finally {
      inRAF = false;
      var end = performance.now();
      cur.raf += end - start;
      cur.n++;
      for (var i = 0; i < pendingLatency.length; i++) {
        var p = pendingLatency[i];
        if (p.drawInThisFrame || p.seenFrame !== ts) { S.latency.push({ ms: end - p.down, sync: !!p.drawInThisFrame }); p.done = true; }
      }
      pendingLatency = pendingLatency.filter(function(p) { return !p.done; });
    }
  });
};
// Worker message handlers: time each one; the next frame record takes the sum.
var msgAcc = 0;
var desc = Object.getOwnPropertyDescriptor(Worker.prototype, "onmessage");
if (desc && desc.set) {
  Object.defineProperty(Worker.prototype, "onmessage", { configurable: true, enumerable: true,
    get: function() { return desc.get.call(this); },
    set: function(fn) {
      if (typeof fn !== "function") return desc.set.call(this, fn);
      desc.set.call(this, function(ev) { var t = performance.now(); try { return fn.call(this, ev); } finally { msgAcc += performance.now() - t; } });
    } });
}
// Input-to-display latency: the time from a KeyM press to the end of the
// frame that first draws the marked figure (agent 0 lifted).
var keyDown = 0, keyMarked = false;
window.addEventListener("keydown", function(e) { if (e.code === "KeyM" && !e.repeat) { keyDown = performance.now(); keyMarked = false; } }, true);
window.addEventListener("keyup", function(e) { if (e.code === "KeyM") keyDown = 0; }, true);
function watchPoses(handle) {
  if (!handle || handle.__stressWatched || typeof handle.applyPoseFrame !== "function") return false;
  var apply = handle.applyPoseFrame;
  handle.applyPoseFrame = function(batches) {
    var result = apply.apply(this, arguments);
    if (keyDown && !keyMarked && Array.isArray(batches)) {
      for (var i = 0; i < batches.length; i++) {
        var b = batches[i];
        if (b && b.id === "figures" && b.instances && b.instances[0] && b.instances[0].y > 0.5) {
          keyMarked = true;
          pendingLatency.push({ down: keyDown, drawInThisFrame: inRAF, seenFrame: cur ? cur.ts : -1 });
        }
      }
    }
    return result;
  };
  handle.__stressWatched = true;
  return true;
}
var watchTimer = setInterval(function() {
  var m = document.getElementById("arena");
  if (m && watchPoses(m.__gosxScene3DHandle)) clearInterval(watchTimer);
}, 100);
["pose-frame-error", "motion-frame-error", "clip-frame-error"].forEach(function(n) {
  window.addEventListener("gosx:scene3d:" + n, function(e) { S.errors.push({ name: n, detail: JSON.stringify(e.detail || null).slice(0, 400) }); }, true);
});
// GPU time: the renderer's frame timing, sampled every 2 s outside a frame.
setInterval(function() {
  var reg = window.__gosx_scene3d_debug_registry, got = null;
  if (reg && typeof reg.forEach === "function") reg.forEach(function(rec) {
    if (!got && rec && rec.mount && rec.mount.id === "arena" && typeof rec.snapshot === "function") {
      try { got = rec.snapshot("summary"); } catch (_) {}
    }
  });
  if (got && got.frameTiming) S.gpu.push({ at: performance.now(), renderer: got.renderer, timing: got.frameTiming, counts: got.counts });
}, 2000);
document.addEventListener("gosx:engine:report", function(e) { var d = e.detail && e.detail.detail; if (d) S.probes.push(d); });
["thread-fallback", "worker-error", "worker-frame-error", "worker-unsupported"].forEach(function(n) {
  document.addEventListener("gosx:engine:" + n, function(e) { S.errors.push({ name: n, detail: JSON.stringify(e.detail || null).slice(0, 400) }); });
});
window.addEventListener("error", function(e) { S.errors.push({ name: "error", detail: String(e.message || e) }); });
S.mount = function() {
  var m = document.getElementById("arena");
  return m ? { backend: m.getAttribute("data-gosx-scene3d-backend") || "", mounted: m.hasAttribute("data-gosx-scene3d-mounted"),
    ready: m.getAttribute("data-gosx-scene3d-ready") === "true", fallback: m.getAttribute("data-gosx-scene3d-renderer-fallback") || "" } : null;
};
S.running = function() {
  drainMainGo();
  var m = S.mount();
  return { ready: !!(m && m.ready), frames: S.go.length, errors: S.errors };
};
S.collect = function(fromMS) {
  flush();
  drainMainGo();
  var from = fromMS || 0;
  var telemetry = null;
  try { var m = document.getElementById("arena"); telemetry = m && m.__gosxScene3DHandle ? m.__gosxScene3DHandle.getTelemetry() : null; } catch (_) {}
  return JSON.stringify({
    run: window.__stressRun, ua: navigator.userAgent, mount: S.mount(), hardwareConcurrency: navigator.hardwareConcurrency,
    frames: S.frames.filter(function(f) { return f.at >= from; }),
    go: S.go, gpu: S.gpu, latency: S.latency, errors: S.errors, probes: S.probes, audio: S.audio,
    poseFrames: telemetry && telemetry.poseFrames, motionFrames: telemetry && telemetry.motionFrames,
    timedPoses: (function() { try { return document.getElementById("arena").__gosxScene3DHandle.__gosxTimedPoseStats || null; } catch (_) { return null; } })(),
    bundle: telemetry && telemetry.bundle, frameHook: telemetry && telemetry.frameHook,
    webgpuStats: (function() { try { var s = document.getElementById("arena").__gosxScene3DWebGPUStats; return s ? { crowdAgents: s.crowdAgents, crowdDraws: s.crowdDraws, crowdBatches: s.crowdBatches, crowdCulling: s.crowdCulling } : null; } catch (_) { return null; } })(),
    now: performance.now(),
  });
};
})();
`

// audioJS starts the music on __stress.startAudio(), which a test calls with a
// user gesture. "cicada" runs the Cicada kernel and score from the fixture the
// server was given; "worklet" runs a silent kernel through the same
// AudioWorklet processor. Both play into the GoSX mixer's music bus
// (window.__gosx.audioMixer, the mixer game/audio wraps). The kernel renders
// on the AudioWorklet thread; the page only posts commands and reads state.
const audioJS = `
(function(){
"use strict";
var S = window.__stress, mode = window.__stressRun.audio;
var b64 = function(s) { return Uint8Array.from(atob(s), function(c) { return c.charCodeAt(0); }); };
S.startAudio = async function() {
  var a = S.audio = { mode: mode, state: "off", messages: 0, frames: 0, steps: [] };
  if (mode === "off") return JSON.stringify(a);
  try {
    var mixer = window.__gosx && window.__gosx.audioMixer;
    if (!mixer) throw new Error("no GoSX audio mixer on the page");
    var fx = null, wasmURL = "/audio/silent.wasm";
    var backend = { prefix: "test_", maxFrames: 128, channels: 2, commandBytes: 16, messageBytes: 4, messageCapacity: 8, supportedCapabilities: 0 };
    var info = { tempoMilli: 0, requiredCapabilities: 0 }, image;
    if (mode === "cicada") {
      fx = await (await fetch("/audio/cicada/fixture.json")).json();
      wasmURL = "/audio/cicada/kernel.wasm";
      backend = fx.backend;
      info = { tempoMilli: fx.tempoMilli, requiredCapabilities: fx.requiredCapabilities };
      image = b64(fx.image);
    }
    var ctx = mixer.context(fx ? { sampleRate: fx.rate } : {});
    await ctx.resume();
    await ctx.audioWorklet.addModule("/audio/processor.js");
    var module = await WebAssembly.compile(await (await fetch(wasmURL)).arrayBuffer());
    var opts = { m: module, r: "stress", l: 0, backend: backend, info: info };
    if (image) opts.i = image.buffer;
    var node = new AudioWorkletNode(ctx, "gosx-wasm-audio", { numberOfInputs: 0, numberOfOutputs: 1, channelCount: 2,
      channelCountMode: "explicit", outputChannelCount: [2], processorOptions: opts });
    await new Promise(function(resolve, reject) {
      var timer = setTimeout(function() { reject(new Error("audio kernel never became ready")); }, 10000);
      node.port.onmessage = function(ev) {
        var d = ev.data || {};
        if (d.t === "r") { clearTimeout(timer); resolve(); }
        if (d.t === "e" || d.t === "x" || d.t === "f") a.steps.push("processor " + JSON.stringify(d.e || d.a || d.r || d));
        if (d.t === "m") { a.messages++; a.frames = d.f; node.port.postMessage({ t: "b", bytes: d.bytes }, [d.bytes]); }
      };
    });
    node.connect(mixer.input("music"));
    mixer.setVolume("music", 0.6);
    if (fx) {
      var send = function(name) { if (fx.batches[name]) node.port.postMessage({ t: "c", bytes: b64(fx.batches[name]) }); };
      send("start");
      // Change the music's state every few seconds, as a game would.
      var flip = false;
      setInterval(function() { flip = !flip; send(flip ? "combat" : "intensity"); }, 4000);
    }
    a.state = ctx.state;
    a.sampleRate = ctx.sampleRate;
    S.audioContext = ctx;
  } catch (error) {
    a.state = "error";
    a.error = String(error && error.message || error);
  }
  return JSON.stringify(a);
};
S.audioState = function() {
  var a = S.audio || {};
  if (S.audioContext) { a.state = S.audioContext.state; a.currentTime = S.audioContext.currentTime; }
  return a;
};
})();
`
