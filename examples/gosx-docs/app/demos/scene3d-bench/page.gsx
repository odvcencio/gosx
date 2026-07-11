package docs

func Page() Node {
	return <section class="scene3d-bench" aria-label="Scene3D render benchmark">
		<div class="scene3d-bench__scene">
			<Scene3D {...data.scene} />
		</div>
		<div class="scene3d-bench__overlay" id="bench3d-overlay" data-workload={data.workload}>
			<header class="scene3d-bench__title">Scene3D bench</header>
			<div class="scene3d-bench__row">
				<span>active workload</span>
				<b id="bench3d-workload">{data.workload}</b>
			</div>
			<div class="scene3d-bench__row">
				<span>CPU submit · current</span>
				<b id="bench3d-current">—</b>
			</div>
			<div class="scene3d-bench__row">
				<span>CPU submit · mean</span>
				<b id="bench3d-mean">—</b>
			</div>
			<div class="scene3d-bench__row">
				<span>CPU submit · p50</span>
				<b id="bench3d-p50">—</b>
			</div>
			<div class="scene3d-bench__row">
				<span>CPU submit · p95</span>
				<b id="bench3d-p95">—</b>
			</div>
			<div class="scene3d-bench__row">
				<span>CPU submit · max</span>
				<b id="bench3d-max">—</b>
			</div>
			<div class="scene3d-bench__row">
				<span>CPU samples</span>
				<b id="bench3d-samples">0</b>
			</div>
			<div class="scene3d-bench__row">
				<span>rAF cadence</span>
				<b id="bench3d-fps">—</b>
			</div>
			<div class="scene3d-bench__row">
				<span>rAF p95</span>
				<b id="bench3d-raf-p95">—</b>
			</div>
			<svg
				class="scene3d-bench__histogram"
				id="bench3d-histogram"
				viewBox="0 0 240 40"
				width="100%"
				height="40"
				aria-hidden="true"
			></svg>
			<div class="scene3d-bench__actions">
				<button type="button" id="bench3d-reset">reset</button>
				<button type="button" id="bench3d-copy">copy JSON</button>
				<button type="button" id="bench3d-download">download</button>
				<span id="bench3d-action-status" class="scene3d-bench__action-status" role="status"></span>
			</div>
			<nav class="scene3d-bench__workloads" aria-label="Switch workload">
				<a href="?workload=static">static</a>
				<a href="?workload=pbr-heavy">pbr-heavy</a>
				<a href="?workload=thick-lines">thick-lines</a>
				<a href="?workload=particles">particles</a>
				<a href="?workload=mixed">mixed</a>
			</nav>
			<div class="scene3d-bench__gpu" id="bench3d-gpu-strip">
				<div>
					<span>GPU</span>
					<b id="bench3d-gpu">detecting…</b>
				</div>
				<div>
					<span>API</span>
					<b id="bench3d-api">—</b>
				</div>
			</div>
			<p class="scene3d-bench__note">
				CPU submit measures the synchronous Scene3D render call. It does not claim GPU completion time. rAF cadence measures browser presentation opportunities and includes contention from the page, browser, and machine.
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
//  5. Reads the backend selected by the mounted GoSX Scene3D runtime and uses
//     a WebGL debug probe only for the optional GPU renderer label.
//  6. Paints a 60-bar frame-time histogram in the SVG on each updateOverlay.
//  7. Exports an honest, versioned JSON snapshot for comparisons.
//
// Must run BEFORE bootstrap.js evaluates the render loop so the flag is
// visible on the first frame — GoSX inlines user scripts at page-template
// render time, ahead of the bootstrap asset.
func BenchOverlayScript() Node {
	return <script>
		{`
	(function() {
	  if (typeof window === "undefined") return;
	  window.__gosx_scene3d_perf = true;

	  var BUF_LEN = 240;
	  var buf = new Float64Array(BUF_LEN);
	  var bufIdx = 0;
	  var bufCount = 0;
	  var lastSampleMs = 0;
	  var rafBuf = new Float64Array(BUF_LEN);
	  var rafIdx = 0;
	  var rafCount = 0;
	  var lastRAF = 0;
	  var detectedGPU = "unavailable";
	  var selectedBackend = "starting";

	  function push(duration) {
	    buf[bufIdx] = duration;
	    bufIdx = (bufIdx + 1) % BUF_LEN;
	    if (bufCount < BUF_LEN) bufCount++;
	  }
	  function computeStats() {
	    return statsFrom(buf, bufCount);
	  }
	  function computeRAFStats() {
	    return statsFrom(rafBuf, rafCount);
	  }
	  function statsFrom(source, count) {
	    if (count === 0) return null;
	    var sorted = new Float64Array(count);
	    for (var i = 0; i < count; i++) sorted[i] = source[i];
	    Array.prototype.sort.call(sorted, function(a, b) { return a - b; });
	    var sum = 0;
	    for (var j = 0; j < count; j++) sum += sorted[j];
	    return {
	      min: sorted[0],
	      p50: sorted[Math.min(count - 1, Math.floor(count * 0.5))],
	      p95: sorted[Math.min(count - 1, Math.floor(count * 0.95))],
	      max: sorted[count - 1],
	      mean: sum / count,
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
	    els.rafP95   = document.getElementById("bench3d-raf-p95");
	    els.hist     = document.getElementById("bench3d-histogram");
	    els.status   = document.getElementById("bench3d-action-status");
	    var reset = document.getElementById("bench3d-reset");
	    if (reset) {
	      reset.addEventListener("click", function() {
	        bufIdx = 0; bufCount = 0; rafIdx = 0; rafCount = 0; lastRAF = 0;
	        if (els.current) els.current.textContent = "—";
	        if (els.mean)    els.mean.textContent    = "—";
	        if (els.p50)     els.p50.textContent     = "—";
	        if (els.p95)     els.p95.textContent     = "—";
	        if (els.max)     els.max.textContent     = "—";
	        if (els.samples) els.samples.textContent = "0";
	        if (els.fps)     els.fps.textContent     = "—";
	        if (els.rafP95)  els.rafP95.textContent  = "—";
	        if (els.hist)    els.hist.textContent    = "";
	      });
	    }
	    var copy = document.getElementById("bench3d-copy");
	    if (copy) copy.addEventListener("click", copySnapshot);
	    var download = document.getElementById("bench3d-download");
	    if (download) download.addEventListener("click", downloadSnapshot);
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
	    var rafStats = computeRAFStats();
	    if (stats) {
	      if (els.mean)    els.mean.textContent    = fmt(stats.mean);
	      if (els.p50)     els.p50.textContent     = fmt(stats.p50);
	      if (els.p95)     els.p95.textContent     = fmt(stats.p95);
	      if (els.max)     els.max.textContent     = fmt(stats.max);
	      if (els.samples) els.samples.textContent = String(bufCount);
	      paintHistogram(els.hist);
	    }
	    if (rafStats && rafStats.mean > 0) {
	      if (els.fps) els.fps.textContent = (1000 / rafStats.mean).toFixed(1) + " fps";
	      if (els.rafP95) els.rafP95.textContent = fmt(rafStats.p95);
	    }
	  }

	  function sampleRAF(now) {
	    if (lastRAF) {
	      rafBuf[rafIdx] = now - lastRAF;
	      rafIdx = (rafIdx + 1) % BUF_LEN;
	      if (rafCount < BUF_LEN) rafCount++;
	    }
	    lastRAF = now;
	    requestAnimationFrame(sampleRAF);
	  }

	  function snapshot() {
	    var overlay = document.getElementById("bench3d-overlay");
	    return {
	      schema: "gosx.scene3d-bench.v1",
	      capturedAt: new Date().toISOString(),
	      workload: overlay ? overlay.getAttribute("data-workload") : "unknown",
	      backend: selectedBackend,
	      gpu: detectedGPU,
	      cpuSubmitMs: computeStats(),
	      cpuSamples: bufCount,
	      rafFrameMs: computeRAFStats(),
	      rafSamples: rafCount,
	      userAgent: navigator.userAgent,
	    };
	  }

	  function setActionStatus(message) {
	    if (!els.status) return;
	    els.status.textContent = message;
	    setTimeout(function() { if (els.status) els.status.textContent = ""; }, 1800);
	  }

	  function copySnapshot() {
	    var text = JSON.stringify(snapshot(), null, 2);
	    if (!navigator.clipboard || !navigator.clipboard.writeText) {
	      setActionStatus("Clipboard unavailable");
	      return;
	    }
	    navigator.clipboard.writeText(text).then(function() {
	      setActionStatus("JSON copied");
	    }, function() { setActionStatus("Copy failed"); });
	  }

	  function downloadSnapshot() {
	    var blob = new Blob([JSON.stringify(snapshot(), null, 2) + "\n"], { type: "application/json" });
	    var url = URL.createObjectURL(blob);
	    var link = document.createElement("a");
	    link.href = url;
	    link.download = "gosx-scene3d-bench.json";
	    link.click();
	    setTimeout(function() { URL.revokeObjectURL(url); }, 0);
	    setActionStatus("JSON downloaded");
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

	  // detectGPU uses the renderer selected by the mounted Scene3D runtime.
	  // A separate WebGL probe is used only for an optional renderer label.
	  function detectGPU() {
	    var gpuEl = document.getElementById("bench3d-gpu");
	    var apiEl = document.getElementById("bench3d-api");
	    if (!gpuEl && !apiEl) return;

	    var apiStr = "starting";
	    var gpuStr = "unavailable";

	    // WebGL2 fallback — also the primary source of renderer info.
	    var canvas = document.createElement("canvas");
	    var gl2 = null;
	    try { gl2 = canvas.getContext("webgl2"); } catch(e) {}
	    if (gl2) {
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
	        var dbg1 = gl1.getExtension("WEBGL_debug_renderer_info");
	        if (dbg1) {
	          gpuStr = gl1.getParameter(dbg1.UNMASKED_RENDERER_WEBGL) || "unknown";
	        } else {
	          gpuStr = gl1.getParameter(gl1.RENDERER) || "unknown";
	        }
	      }
	    }

	    detectedGPU = gpuStr;
	    if (gpuEl) gpuEl.textContent = gpuStr;
	    function syncBackend() {
	      var mount = document.querySelector(".scene3d-bench [data-gosx-scene3d-renderer]");
	      if (!mount) return false;
	      selectedBackend = mount.getAttribute("data-gosx-scene3d-renderer") || "starting";
	      if (apiEl) apiEl.textContent = selectedBackend;
	      if (selectedBackend === "webgpu" && gpuEl) {
	        detectedGPU = "adapter details unavailable";
	        gpuEl.textContent = detectedGPU;
	      }
	      return selectedBackend !== "starting";
	    }
	    if (!syncBackend()) {
	      var attempts = 0;
	      var timer = setInterval(function() {
	        attempts++;
	        if (syncBackend() || attempts >= 80) clearInterval(timer);
	      }, 100);
	    }
	  }

	  function markActiveWorkload() {
	    var overlay = document.getElementById("bench3d-overlay");
	    var active = overlay ? overlay.getAttribute("data-workload") : "mixed";
	    var links = document.querySelectorAll(".scene3d-bench__workloads a");
	    for (var i = 0; i < links.length; i++) {
	      var value = new URL(links[i].href, location.href).searchParams.get("workload") || "mixed";
	      if (value === active) links[i].setAttribute("aria-current", "page");
	      else links[i].removeAttribute("aria-current");
	    }
	  }

	  function boot() {
	    mountOverlay();
	    installObserver();
	    detectGPU();
	    markActiveWorkload();
	    requestAnimationFrame(sampleRAF);
	    setInterval(updateOverlay, 250);
	  }

	  if (document.readyState === "loading") {
	    document.addEventListener("DOMContentLoaded", boot);
	  } else {
	    boot();
	  }
	})();
	`}
	</script>
}
