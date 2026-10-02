// The reveal observes delivered scene intervals and resource transfers. It
// never reports the configured FPS cap or an asset-size estimate as a measure.
(function () {
  "use strict";
  function mount() {
    const root = document.querySelector(".bgb");
    if (!root || root.dataset.revealMounted) return;
    root.dataset.revealMounted = "true";
    const bytes = root.querySelector('[data-gosx-scene3d-status="bytes"]');
    const fps = root.querySelector('[data-gosx-scene3d-status="fps"]');
    const scene = root.querySelector("[data-gosx-scene3d]");
    const intervals = [], frames = [];
    let readyAt = 0, previousSubmit = 0, previousSequence = 0, previousSample = 0;
    function reveal() { root.dataset.revealed = "true"; }
    function tick() {
      if (!root.isConnected) { clearInterval(timer); return; }
      if (document.hidden) { intervals.length = 0; frames.length = 0; previousSubmit = 0; previousSequence = 0; previousSample = 0; return; }
      let transfer = 0;
      for (const kind of ["navigation", "resource"]) {
        for (const entry of performance.getEntriesByType(kind)) {
          if (!entry.name.startsWith("blob:")) transfer += entry.transferSize || 0;
        }
      }
      bytes.textContent = Math.round(transfer / 1024).toLocaleString();
      const registry = window.__gosx_scene3d_debug_registry;
      let timing = null;
      if (registry) registry.forEach(record => {
        if (record.mount === scene) timing = record.snapshot("summary").frameTiming;
      });
      const now = performance.now();
      if (timing && timing.submitAtMS > 0) {
        if (!readyAt) readyAt = now;
        if (timing.submitAtMS !== previousSubmit && timing.frameIntervalMS > 0) {
          intervals.push(timing.frameIntervalMS);
          if (intervals.length > 24) intervals.shift();
        }
        previousSubmit = timing.submitAtMS;
        const mean = intervals.reduce((sum, v) => sum + v, 0) / intervals.length;
        let rate = mean > 0 ? 1000 / mean : 0;
        const stats = scene.__gosxScene3DWebGPUStats;
        const sequence = stats && stats.frameSeq;
        if (sequence > 0) {
          if (previousSample && sequence >= previousSequence) {
            frames.push([sequence - previousSequence, now - previousSample]);
            if (frames.length > 16) frames.shift();
          }
          previousSequence = sequence; previousSample = now;
          if (frames.length) rate = 1000 * frames.reduce((sum, f) => sum + f[0], 0) / frames.reduce((sum, f) => sum + f[1], 0);
        }
        fps.textContent = now - timing.submitAtMS > 1000 ? "0" : rate > 0 ? String(Math.round(rate)) : "…";
        if (now - readyAt >= 6000) reveal();
      }
    }
    const timer = setInterval(tick, 239);
    root.addEventListener("pointerdown", reveal, { once: true });
    root.addEventListener("keydown", reveal, { once: true });
    window.addEventListener("wheel", reveal, { once: true, passive: true });
    window.addEventListener("scroll", reveal, { once: true, passive: true });
    window.addEventListener("pagehide", () => clearInterval(timer), { once: true });
    tick();
  }
  if (document.readyState === "loading") document.addEventListener("DOMContentLoaded", mount, { once: true });
  else mount();
})();
