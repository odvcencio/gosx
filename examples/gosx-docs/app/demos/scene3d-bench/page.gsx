package docs

func Page() Node {
	return <section class="scene3d-bench" aria-label="Scene3D render benchmark">
		<div class="scene3d-bench__scene">
			<Scene3D {...data.scene} />
		</div>
		<div class="scene3d-bench__overlay" id="bench3d-overlay" aria-live="polite">
			<header class="scene3d-bench__title">Scene3D bench</header>
			<div class="scene3d-bench__row"><span>workload</span><b>{data.workload}</b></div>
			<div class="scene3d-bench__row"><span>frame</span><b id="bench3d-current">—</b></div>
			<div class="scene3d-bench__row"><span>frame ms avg</span><b id="bench3d-mean">—</b></div>
			<div class="scene3d-bench__row"><span>p50</span><b id="bench3d-p50">—</b></div>
			<div class="scene3d-bench__row"><span>p95</span><b id="bench3d-p95">—</b></div>
			<div class="scene3d-bench__row"><span>max</span><b id="bench3d-max">—</b></div>
			<div class="scene3d-bench__row"><span>samples</span><b id="bench3d-samples">0</b></div>
			<div class="scene3d-bench__row"><span>render/s</span><b id="bench3d-fps">—</b></div>
			<svg class="scene3d-bench__histogram" id="bench3d-histogram" viewBox="0 0 240 40" width="100%" height="40" aria-hidden="true"></svg>
			<div class="scene3d-bench__actions">
				<button type="button" id="bench3d-reset">reset</button>
			</div>
			<nav class="scene3d-bench__workloads" aria-label="Switch workload">
				<a href="?workload=static">static</a>
				<a href="?workload=pbr-heavy">pbr-heavy</a>
				<a href="?workload=thick-lines">thick-lines</a>
				<a href="?workload=particles">particles</a>
				<a href="?workload=mixed">mixed</a>
			</nav>
			<div class="scene3d-bench__gpu" id="bench3d-gpu-strip">
				<div><span>GPU</span><b id="bench3d-gpu">detecting…</b></div>
				<div><span>API</span><b id="bench3d-api">—</b></div>
			</div>
			<p class="scene3d-bench__note">
				Measures wall-clock duration of Scene3D.render() via performance.mark
				inside the PBR renderer. GPU work is included; JS hot-path cost after
				the v0.15.x sweep is ~2µs and invisible at 60fps.
			</p>
		</div>
		{BenchOverlayScript()}
	</section>
}

// BenchOverlayScript injects the inline script that enables the perf gate
// and wires up the live stats overlay. Kept as a separate function so the
// page template stays readable and the script contents can be edited
// without disturbing the Go syntax tree.
//
// The script:
//  1. Sets window.__gosx_scene3d_perf = true so the PBR renderer starts
//     emitting 'scene3d-render' performance.measure entries.
//  2. Installs a PerformanceObserver that pushes each measure duration
//     into a 240-entry ring buffer (4 seconds at 60 fps) and updates
//     the current-frame readout immediately.
//  3. Polls computeStats() every 250ms and paints p50/p95/max/mean/sample
//     count onto the overlay.
//  4. Wires the reset button to clear the ring buffer.
//  5. On boot, detects GPU vendor/renderer via WebGL debug extension and
//     populates the GPU/API strip.
//  6. Paints a 60-bar frame-time histogram in the SVG on each updateOverlay.
//
// Must run BEFORE bootstrap.js evaluates the render loop so the flag is
// visible on the first frame — GoSX inlines user scripts at page-template
// render time, ahead of the bootstrap asset.
func BenchOverlayScript() Node {
	return <script>{`
(function() {
  if (typeof window === "undefined") return;
  window.__gosx_scene3d_perf = true;

  var BUF_LEN = 240;
  var buf = new Float64Array(BUF_LEN);
  var bufIdx = 0;
  var bufCount = 0;
  var lastSampleMs = 0;

  function push(duration) {
    buf[bufIdx] = duration;
    bufIdx = (bufIdx + 1) % BUF_LEN;
    if (bufCount < BUF_LEN) bufCount++;
  }
  function computeStats() {
    if (bufCount === 0) return null;
    var sorted = new Float64Array(bufCount);
    for (var i = 0; i < bufCount; i++) sorted[i] = buf[i];
    Array.prototype.sort.call(sorted, function(a, b) { return a - b; });
    var sum = 0;
    for (var j = 0; j < bufCount; j++) sum += sorted[j];
    return {
      min: sorted[0],
      p50: sorted[Math.floor(bufCount * 0.5)],
      p95: sorted[Math.floor(bufCount * 0.95)],
      max: sorted[bufCount - 1],
      mean: sum / bufCount,
    };
  }

  var els = {};
  function mountOverlay() {
    els.current  = document.getElementById("bench3d-current");
    els.mean     = document.getElementById("bench3d-mean");
    els.p50      = document.getElementById("bench3d-p50");
    els.p95      = document.getElementById("bench3d-p95");
    els.max      = document.getElementById("bench3d-max");
    els.samples  = document.getElementById("bench3d-samples");
    els.fps      = document.getElementById("bench3d-fps");
    els.hist     = document.getElementById("bench3d-histogram");
    var reset = document.getElementById("bench3d-reset");
    if (reset) {
      reset.addEventListener("click", function() {
        bufIdx = 0; bufCount = 0;
        if (els.current) els.current.textContent = "—";
        if (els.mean)    els.mean.textContent    = "—";
        if (els.p50)     els.p50.textContent     = "—";
        if (els.p95)     els.p95.textContent     = "—";
        if (els.max)     els.max.textContent     = "—";
        if (els.samples) els.samples.textContent = "0";
        if (els.fps)     els.fps.textContent     = "—";
        if (els.hist)    els.hist.textContent    = "";
      });
    }
  }

  function fmt(ms) {
    if (typeof ms !== "number" || !isFinite(ms)) return "—";
    if (ms < 1) return (ms * 1000).toFixed(0) + "µs";
    return ms.toFixed(2) + "ms";
  }

  // barClass returns the CSS class suffix for a frame time value.
  function barClass(ms) {
    if (ms <= 8)  return "healthy";
    if (ms <= 16) return "nominal";
    if (ms <= 33) return "stretched";
    return "dropping";
  }

  // paintHistogram renders up to 60 bars into the SVG element.
  function paintHistogram(svg) {
    if (!svg || bufCount === 0) return;
    var BARS = 60;
    var viewW = 240;
    var viewH = 40;
    var gap   = 1;
    var barW  = (viewW - (BARS - 1) * gap) / BARS;

    // Collect the most-recent min(bufCount, BARS) samples in chronological order.
    var count = Math.min(bufCount, BARS);
    var samples = [];
    // bufIdx points at the NEXT write slot, so walk backwards count steps.
    for (var i = count - 1; i >= 0; i--) {
      var idx = ((bufIdx - 1 - i) % BUF_LEN + BUF_LEN) % BUF_LEN;
      samples.push(buf[idx]);
    }

    // Find max for scaling (floor at 1ms to avoid divide-by-zero).
    var maxVal = 1;
    for (var k = 0; k < samples.length; k++) {
      if (samples[k] > maxVal) maxVal = samples[k];
    }

    // Build SVG rects as a string to do a single innerHTML assignment.
    var svgNS = "http://www.w3.org/2000/svg";
    // Remove old children.
    while (svg.firstChild) svg.removeChild(svg.firstChild);

    for (var b = 0; b < samples.length; b++) {
      var val = samples[b];
      var h   = Math.max(2, Math.round((val / maxVal) * viewH));
      var x   = b * (barW + gap);
      var y   = viewH - h;
      var rect = document.createElementNS(svgNS, "rect");
      rect.setAttribute("x",      x.toFixed(1));
      rect.setAttribute("y",      y.toFixed(1));
      rect.setAttribute("width",  barW.toFixed(1));
      rect.setAttribute("height", h);
      rect.setAttribute("class",  "scene3d-bench__bar--" + barClass(val));
      svg.appendChild(rect);
    }
  }

  function updateOverlay() {
    var stats = computeStats();
    if (!stats) return;
    if (els.mean)    els.mean.textContent    = fmt(stats.mean);
    if (els.p50)     els.p50.textContent     = fmt(stats.p50);
    if (els.p95)     els.p95.textContent     = fmt(stats.p95);
    if (els.max)     els.max.textContent     = fmt(stats.max);
    if (els.samples) els.samples.textContent = String(bufCount);
    // render/s — reciprocal of mean frame time, capped at display refresh.
    if (els.fps && stats.mean > 0) {
      els.fps.textContent = (1000 / stats.mean).toFixed(0);
    }
    paintHistogram(els.hist);
  }

  function onEntries(entries) {
    for (var i = 0; i < entries.length; i++) {
      var entry = entries[i];
      if (entry.name !== "scene3d-render" || entry.entryType !== "measure") continue;
      push(entry.duration);
      if (els.current) els.current.textContent = fmt(entry.duration);
      lastSampleMs = performance.now();
    }
    // Clear consumed measures so the performance buffer doesn't grow.
    if (typeof performance.clearMeasures === "function") {
      performance.clearMeasures("scene3d-render");
    }
  }

  function installObserver() {
    if (typeof PerformanceObserver !== "function") {
      console.warn("[bench3d] PerformanceObserver unavailable; overlay disabled");
      return;
    }
    try {
      var observer = new PerformanceObserver(function(list) { onEntries(list.getEntries()); });
      observer.observe({ entryTypes: ["measure"] });
    } catch (err) {
      console.warn("[bench3d] PerformanceObserver.observe failed:", err);
    }
  }

  // detectGPU probes the WebGL context for renderer/vendor strings and fills
  // in the GPU and API info strip in the overlay panel.
  function detectGPU() {
    var gpuEl = document.getElementById("bench3d-gpu");
    var apiEl = document.getElementById("bench3d-api");
    if (!gpuEl && !apiEl) return;

    // Determine API string: prefer WebGPU, fall back to WebGL2/WebGL1.
    var apiStr = "unavailable";
    var gpuStr = "unavailable";

    // WebGPU check (no context creation needed).
    if (typeof navigator !== "undefined" && typeof navigator.gpu !== "undefined") {
      apiStr = "webgpu";
      // GPU adapter name not synchronously available; leave generic label.
      gpuStr = "WebGPU adapter";
    }

    // WebGL2 fallback — also the primary source of renderer info.
    var canvas = document.createElement("canvas");
    var gl2 = null;
    try { gl2 = canvas.getContext("webgl2"); } catch(e) {}
    if (gl2) {
      if (apiStr === "unavailable") apiStr = "webgl2";
      var dbg = gl2.getExtension("WEBGL_debug_renderer_info");
      if (dbg) {
        gpuStr = gl2.getParameter(dbg.UNMASKED_RENDERER_WEBGL) || "unknown";
      } else {
        gpuStr = gl2.getParameter(gl2.RENDERER) || "unknown";
      }
    } else {
      // WebGL1 fallback.
      var gl1 = null;
      try { gl1 = canvas.getContext("webgl") || canvas.getContext("experimental-webgl"); } catch(e) {}
      if (gl1) {
        if (apiStr === "unavailable") apiStr = "webgl";
        var dbg1 = gl1.getExtension("WEBGL_debug_renderer_info");
        if (dbg1) {
          gpuStr = gl1.getParameter(dbg1.UNMASKED_RENDERER_WEBGL) || "unknown";
        } else {
          gpuStr = gl1.getParameter(gl1.RENDERER) || "unknown";
        }
      }
    }

    if (gpuEl) gpuEl.textContent = gpuStr;
    if (apiEl) apiEl.textContent = apiStr;
  }

  function boot() {
    mountOverlay();
    installObserver();
    detectGPU();
    setInterval(updateOverlay, 250);
  }

  if (document.readyState === "loading") {
    document.addEventListener("DOMContentLoaded", boot);
  } else {
    boot();
  }
})();
`}</script>
}
