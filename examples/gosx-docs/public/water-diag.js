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
      var knobs = ["dpr", "res", "caustics", "reflection", "refraction", "causticsRes", "shadowRes", "maxPixels"]
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
        row("compute", attr("webgpu-water-compute-dispatches") + " dispatch", false) +
        (knobs.length
          ? '<div style="margin-top:7px;padding-top:6px;border-top:1px solid rgba(255,255,255,.12);opacity:.7">' +
            knobs.join(" · ") + "</div>"
          : '<div style="margin-top:7px;padding-top:6px;border-top:1px solid rgba(255,255,255,.12);opacity:.5">stock settings</div>');
    }

    function tick(now) {
      if (last > 0) {
        times.push(now - last);
        if (times.length > 180) times.shift();
      }
      last = now;
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
        computeDispatches: attr("webgpu-water-compute-dispatches"),
      };
      console.log(JSON.stringify(out, null, 1));
      return out;
    };
  });
})();
