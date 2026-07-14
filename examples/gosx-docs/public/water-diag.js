// Water demo perf diagnostics overlay. Loaded only when ?diag=1.
//
// Reports the frame cost the BROWSER actually delivers, next to the cost the scene
// runtime thinks it is paying. Those two numbers disagreeing by 15x is what hid a
// GPU-bound water demo for a whole debugging session: the runtime measured the JS
// render call (command encoding, ~4ms) while the browser delivered 58ms frames,
// because on WebGPU the GPU executes asynchronously afterwards.
//
// Everything here is read-only observation plus the URL knobs. It changes no
// rendering behaviour of its own.
(function () {
  "use strict";

  function ready(fn) {
    if (document.readyState !== "loading") fn();
    else document.addEventListener("DOMContentLoaded", fn);
  }

  ready(function () {
    var mount = document.querySelector("[data-gosx-scene3d]");
    if (!mount) return;

    var box = document.createElement("div");
    box.setAttribute("data-water-diag", "true");
    box.style.cssText = [
      "position:fixed", "top:8px", "right:8px", "z-index:99999",
      "font:11px/1.45 ui-monospace,SFMono-Regular,Menlo,monospace",
      "background:rgba(8,10,14,0.92)", "color:#d8e2ef",
      "border:1px solid rgba(255,255,255,0.16)", "border-radius:6px",
      "padding:10px 12px", "min-width:270px", "pointer-events:auto",
      "box-shadow:0 6px 28px rgba(0,0,0,0.45)",
    ].join(";");
    document.body.appendChild(box);

    // Rolling frame-interval window: this is ground truth for delivered frame rate.
    var times = [];
    var last = 0;

    function pct(sorted, p) {
      if (!sorted.length) return 0;
      var i = Math.min(sorted.length - 1, Math.max(0, Math.round((p / 100) * (sorted.length - 1))));
      return sorted[i];
    }

    function attr(name) {
      var v = mount.getAttribute("data-gosx-scene3d-" + name);
      return v === null ? "" : v;
    }

    function num(v) {
      var n = parseFloat(v);
      return isFinite(n) ? n : 0;
    }

    function row(label, value, warn) {
      return '<div style="display:flex;justify-content:space-between;gap:14px">' +
        '<span style="opacity:.62">' + label + "</span>" +
        '<span style="color:' + (warn ? "#ff8f6b" : "#e8f0fa") + '">' + value + "</span></div>";
    }

    function paint() {
      var canvas = mount.querySelector("canvas");
      var backingMP = canvas ? (canvas.width * canvas.height) / 1e6 : 0;
      var sorted = times.slice().sort(function (a, b) { return a - b; });
      var med = pct(sorted, 50);
      var p95 = pct(sorted, 95);
      var fps = med > 0 ? 1000 / med : 0;

      // The scene runtime's own numbers, so the gap is visible side by side.
      var cpuMS = num(attr("quality-frame-cpu-ms"));
      var intervalMS = num(attr("quality-frame-interval-ms"));
      var reported = num(attr("quality-frame-ms"));

      var q = new URLSearchParams(location.search);
      var knobs = ["dpr", "res", "meshRes", "caustics", "reflection", "refraction", "causticsRes", "shadowRes", "maxPixels"]
        .filter(function (k) { return q.has(k); })
        .map(function (k) { return k + "=" + q.get(k); });

      box.innerHTML =
        '<div style="font-weight:600;margin-bottom:6px;letter-spacing:.04em">WATER · PERF DIAG</div>' +
        row("fps (delivered)", fps.toFixed(1), fps < 50) +
        row("frame median", med.toFixed(1) + " ms", med > 20) +
        row("frame p95", p95.toFixed(1) + " ms", p95 > 33) +
        '<div style="height:6px"></div>' +
        row("runtime says", reported.toFixed(1) + " ms", Math.abs(reported - med) > med * 0.4) +
        row("· cpu encode", cpuMS.toFixed(1) + " ms", false) +
        row("· interval", intervalMS.toFixed(1) + " ms", false) +
        '<div style="height:6px"></div>' +
        row("backing", backingMP.toFixed(3) + " MP", backingMP > 0.5) +
        row("dpr (device)", String(window.devicePixelRatio), false) +
        row("dpr (scene)", attr("pixel-ratio") || "?", false) +
        row("backend", attr("backend") || "?", false) +
        row("adapter", attr("webgpu-adapter") || "-", false) +
        row("quality tier", attr("quality-tier") || "?", attr("quality-tier") !== "full") +
        '<div style="height:6px"></div>' +
        row("water passes", attr("webgpu-water-selena-surface-passes") + " surface / " +
          attr("webgpu-water-caustic-passes") + " caustic", false) +
        row("water verts", attr("webgpu-water-draw-vertices") || "-", false) +
        // If the Selena surface pipeline fails validation the renderer silently falls back
        // to the built-in shader, which draws the FULL simulation-resolution mesh and reads
        // the heightfield from the storage buffer -- losing both optimisations at once and
        // still paying for the state-texture copy. Silent means unmeasurable, so say it.
        row("selena surface", (attr("webgpu-water-selena-surface-passes") || "0") + " passes",
          num(attr("webgpu-water-selena-surface-passes")) === 0) +
        row("surface FALLBACK", (attr("webgpu-water-authored-surface-fallbacks") || "0") +
          (attr("webgpu-water-authored-surface-fallback-reason")
            ? " · " + attr("webgpu-water-authored-surface-fallback-reason").slice(0, 40)
            : ""),
          num(attr("webgpu-water-authored-surface-fallbacks")) > 0) +
        row("mesh res (used)", attr("webgpu-water-surface-mesh-resolution") || "-",
          Boolean(q.get("meshRes")) && attr("webgpu-water-surface-mesh-resolution") !== q.get("meshRes")) +
        row("compute", attr("webgpu-water-compute-dispatches") + " dispatch", false) +
        '<div style="height:6px"></div>' +
        row("canvas", lastW + "x" + lastH, false) +
        row("resizes/sec", String(resizeWindow.length), resizeWindow.length > 0) +
        row("resizes total", String(resizeCount), resizeCount > 3) +
        row("LoAF worst", loaf.maxMS.toFixed(0) + " ms", loaf.maxMS > 30) +
        row("· script", loaf.scriptMS.toFixed(0) + " ms", false) +
        row("· render", loaf.renderMS.toFixed(0) + " ms", false) +
        '<div style="height:6px"></div>' +
        row("GPU work", (attr("webgpu-gpu-ms") || "-") + " ms", num(attr("webgpu-gpu-ms")) > 20) +
        (knobs.length
          ? '<div style="margin-top:7px;padding-top:6px;border-top:1px solid rgba(255,255,255,.12);opacity:.7">' +
            knobs.join(" · ") + "</div>"
          : '<div style="margin-top:7px;padding-top:6px;border-top:1px solid rgba(255,255,255,.12);opacity:.5">stock settings</div>');
    }

    // Canvas-size churn. Toggling every expensive knob in the water system moved the
    // frame rate by nothing at all, which means the cost is not a workload — it is a
    // stall. The prime suspect is a swapchain reconfiguration every frame: a canvas
    // whose backing store and CSS box disagree by a rounding step at fractional DPR
    // can oscillate forever, and reconfiguring a Metal drawable per frame costs tens
    // of milliseconds no matter how little you ask it to draw. If resizes/sec is
    // anything but 0, that is the bug.
    var lastW = 0, lastH = 0, resizeCount = 0, resizeWindow = [];
    function sampleCanvasSize(now) {
      var c = mount.querySelector("canvas");
      if (!c) return;
      if (c.width !== lastW || c.height !== lastH) {
        if (lastW !== 0) { resizeCount += 1; resizeWindow.push(now); }
        lastW = c.width; lastH = c.height;
      }
      while (resizeWindow.length && now - resizeWindow[0] > 1000) resizeWindow.shift();
    }

    // Long Animation Frames: splits a slow frame into script vs render vs the rest, so
    // a stall inside the browser's own pipeline is distinguishable from our JS.
    var loaf = { count: 0, maxMS: 0, scriptMS: 0, renderMS: 0 };
    try {
      new PerformanceObserver(function (list) {
        list.getEntries().forEach(function (e) {
          loaf.count += 1;
          loaf.maxMS = Math.max(loaf.maxMS, e.duration || 0);
          loaf.renderMS = Math.max(loaf.renderMS, e.renderStart ? (e.startTime + e.duration) - e.renderStart : 0);
          (e.scripts || []).forEach(function (sc) {
            loaf.scriptMS = Math.max(loaf.scriptMS, sc.duration || 0);
          });
        });
      }).observe({ type: "long-animation-frame", buffered: true });
    } catch (_) {}

    function tick(now) {
      if (last > 0) {
        times.push(now - last);
        if (times.length > 180) times.shift();
      }
      last = now;
      sampleCanvasSize(now);
      requestAnimationFrame(tick);
    }
    requestAnimationFrame(tick);
    setInterval(paint, 500);
    paint();

    // One-shot copyable summary for pasting back.
    window.waterDiagReport = function () {
      var canvas = mount.querySelector("canvas");
      var sorted = times.slice().sort(function (a, b) { return a - b; });
      var med = pct(sorted, 50);
      var out = {
        url: location.search || "(stock)",
        fps: med > 0 ? +(1000 / med).toFixed(1) : 0,
        frameMedianMS: +med.toFixed(1),
        frameP95MS: +pct(sorted, 95).toFixed(1),
        backingMP: canvas ? +((canvas.width * canvas.height) / 1e6).toFixed(3) : 0,
        devicePixelRatio: window.devicePixelRatio,
        scenePixelRatio: attr("pixel-ratio"),
        runtimeFrameMS: num(attr("quality-frame-ms")),
        cpuEncodeMS: num(attr("quality-frame-cpu-ms")),
        intervalMS: num(attr("quality-frame-interval-ms")),
        qualityTier: attr("quality-tier"),
        backend: attr("backend"),
        adapter: attr("webgpu-adapter"),
        surfacePasses: attr("webgpu-water-selena-surface-passes"),
        causticPasses: attr("webgpu-water-caustic-passes"),
        drawVertices: attr("webgpu-water-draw-vertices"),
        meshResRequested: new URLSearchParams(location.search).get("meshRes") || "(= sim)",
        // The EFFECTIVE value. These two disagreeing means the prop was dropped on the
        // way to the renderer -- a silent no-op knob, which already happened once.
        meshResUsed: attr("webgpu-water-surface-mesh-resolution"),
        selenaSurfacePasses: attr("webgpu-water-selena-surface-passes"),
        surfaceFallbacks: attr("webgpu-water-authored-surface-fallbacks"),
        surfaceFallbackReason: attr("webgpu-water-authored-surface-fallback-reason"),
        surfaceAboveVertices: attr("webgpu-water-surface-above-draw-vertices"),
        computeDispatches: attr("webgpu-water-compute-dispatches"),
        canvas: lastW + "x" + lastH,
        resizesPerSec: resizeWindow.length,
        resizesTotal: resizeCount,
        loafWorstMS: +loaf.maxMS.toFixed(0),
        loafScriptMS: +loaf.scriptMS.toFixed(0),
        loafRenderMS: +loaf.renderMS.toFixed(0),
        gpuWorkMS: num(attr("webgpu-gpu-ms")),
      };
      console.log(JSON.stringify(out, null, 1));
      return out;
    };
  });
})();
