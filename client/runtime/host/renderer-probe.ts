// @ts-check
// GoSX renderer probe: publishes which Scene3D renderer this browser would
// use (webgpu, webgl2, canvas2d or none) on <html data-gosx-renderer> and
// the $gosx.renderer signal, for pages that show a capability table without
// mounting a scene. Loads only for a page that calls RequireFeature("renderer-probe").
// It borrows the probe and the publisher from the scene3d-defer chunk.
(function() {
  "use strict";
  if (typeof window.__gosx_register_bootstrap_feature !== "function") return;
  window.__gosx_register_bootstrap_feature("renderer-probe", function(api) {
    async function probe() {
      const current = document.documentElement.getAttribute("data-gosx-renderer");
      if (current && current !== "pending") return; // a mounted scene already answered
      const shared = await api.ensureBootstrapFeature("scene3d-defer");
      const publish = shared.publishRendererChoice;
      try {
        if (navigator.gpu && typeof navigator.gpu.requestAdapter === "function") {
          const adapter = await navigator.gpu.requestAdapter();
          if (adapter && !adapter.isFallbackAdapter) { publish("webgpu", "adapter", "probe"); return; }
        }
      } catch (_e) { /* fall through to WebGL2 */ }
      const supported = api.browserCapabilitySupported;
      if (shared.detectHardwareGPU()) { publish("webgl2", "hardware-gpu", "probe"); return; }
      if (supported("webgl2")) { publish("webgl2", "software-renderer", "probe"); return; }
      if (supported("canvas2d")) { publish("canvas2d", "no-webgl2", "probe"); return; }
      publish("none", "no-renderer", "probe");
    }
    return {
      runtimeReady() {
        if (typeof window.requestIdleCallback === "function") window.requestIdleCallback(function() { probe(); }, { timeout: 1500 });
        else setTimeout(probe, 1);
      },
    };
  });
})();
