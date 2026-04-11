package docs

func Page() Node {
	return <section class="bench3d" aria-label="Scene3D render benchmark">
		<div class="bench3d__scene">
			<Scene3D {...data.scene} />
		</div>
		<div class="bench3d__overlay" id="bench3d-overlay" aria-live="polite">
			<header class="bench3d__title">Scene3D bench</header>
			<div class="bench3d__row"><span>workload</span><b>{data.workload}</b></div>
			<div class="bench3d__row"><span>frame</span><b id="bench3d-current">—</b></div>
			<div class="bench3d__row"><span>p50</span><b id="bench3d-p50">—</b></div>
			<div class="bench3d__row"><span>p95</span><b id="bench3d-p95">—</b></div>
			<div class="bench3d__row"><span>max</span><b id="bench3d-max">—</b></div>
			<div class="bench3d__row"><span>samples</span><b id="bench3d-samples">0</b></div>
			<div class="bench3d__row"><span>render/s</span><b id="bench3d-fps">—</b></div>
			<div class="bench3d__actions">
				<button type="button" id="bench3d-reset">reset</button>
			</div>
			<nav class="bench3d__workloads" aria-label="Switch workload">
				<a href="?workload=static">static</a>
				<a href="?workload=pbr-heavy">pbr-heavy</a>
				<a href="?workload=thick-lines">thick-lines</a>
				<a href="?workload=particles">particles</a>
				<a href="?workload=mixed">mixed</a>
			</nav>
			<p class="bench3d__note">
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
//   1. Sets window.__gosx_scene3d_perf = true so the PBR renderer starts
//      emitting 'scene3d-render' performance.measure entries.
//   2. Installs a PerformanceObserver that pushes each measure duration
//      into a 240-entry ring buffer (4 seconds at 60 fps) and updates
//      the current-frame readout immediately.
//   3. Polls computeStats() every 250ms and paints p50/p95/max/sample
//      count onto the overlay.
//   4. Wires the reset button to clear the ring buffer.
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
    els.current = document.getElementById("bench3d-current");
    els.p50 = document.getElementById("bench3d-p50");
    els.p95 = document.getElementById("bench3d-p95");
    els.max = document.getElementById("bench3d-max");
    els.samples = document.getElementById("bench3d-samples");
    els.fps = document.getElementById("bench3d-fps");
    var reset = document.getElementById("bench3d-reset");
    if (reset) {
      reset.addEventListener("click", function() {
        bufIdx = 0; bufCount = 0;
        if (els.current) els.current.textContent = "—";
        if (els.p50) els.p50.textContent = "—";
        if (els.p95) els.p95.textContent = "—";
        if (els.max) els.max.textContent = "—";
        if (els.samples) els.samples.textContent = "0";
        if (els.fps) els.fps.textContent = "—";
      });
    }
  }

  function fmt(ms) {
    if (typeof ms !== "number" || !isFinite(ms)) return "—";
    if (ms < 1) return (ms * 1000).toFixed(0) + "µs";
    return ms.toFixed(2) + "ms";
  }

  function updateOverlay() {
    var stats = computeStats();
    if (!stats) return;
    if (els.p50) els.p50.textContent = fmt(stats.p50);
    if (els.p95) els.p95.textContent = fmt(stats.p95);
    if (els.max) els.max.textContent = fmt(stats.max);
    if (els.samples) els.samples.textContent = String(bufCount);
    // render/s — reciprocal of mean frame time, capped at display refresh.
    if (els.fps && stats.mean > 0) {
      els.fps.textContent = (1000 / stats.mean).toFixed(0);
    }
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

  function boot() {
    mountOverlay();
    installObserver();
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
