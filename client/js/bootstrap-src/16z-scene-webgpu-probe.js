  // --------------------------------------------------------------------------
  // WebGPU probe + lazy-load stub
  // --------------------------------------------------------------------------
  //
  // The actual WebGPU renderer (createSceneWebGPURenderer + compute particles)
  // lives in bootstrap-feature-scene3d-webgpu.js, a separate async chunk
  // loaded only when navigator.gpu exists. Keeping this stub in the main
  // scene3d bundle lets 20-scene-mount.js call sceneWebGPUAvailable() /
  // createSceneWebGPURendererOrFallback() without awaiting anything — the
  // chunk either has loaded and registered its API on
  // window.__gosx_scene3d_webgpu_api, or it hasn't and we gracefully fall
  // through to the WebGL renderer.
  //
  // The adapter probe fires here (not in the sub-feature) so the result is
  // ready by the time the sub-feature loads. Pages without navigator.gpu
  // (Safari, Firefox non-gpu) never request the chunk — saving ~55KB gzip
  // on every scene3d page load.

  var _webgpuAdapterProbe = null;  // null = unprobed, false = unavailable, GPUAdapter = ready
  var _webgpuAdapterReady = false;

  if (typeof navigator !== "undefined" && navigator.gpu && typeof navigator.gpu.requestAdapter === "function") {
    navigator.gpu.requestAdapter({ powerPreference: "high-performance" }).then(function(adapter) {
      if (adapter) {
        _webgpuAdapterProbe = adapter;
        _webgpuAdapterReady = true;
      } else {
        _webgpuAdapterProbe = false;
      }
    }).catch(function() {
      _webgpuAdapterProbe = false;
    });
    // Share the probe with the sub-feature chunk so it doesn't re-probe.
    window.__gosx_scene3d_webgpu_probe = function() {
      return { adapter: _webgpuAdapterProbe, ready: _webgpuAdapterReady };
    };
  } else {
    _webgpuAdapterProbe = false;
  }

  // sceneWebGPUAvailable returns true only when BOTH the adapter probe
  // succeeded AND the sub-feature chunk has loaded its factory. Either
  // missing → fall back to WebGL.
  function sceneWebGPUAvailable() {
    return _webgpuAdapterReady
      && _webgpuAdapterProbe !== false
      && _webgpuAdapterProbe !== null
      && !!(window.__gosx_scene3d_webgpu_api
        && typeof window.__gosx_scene3d_webgpu_api.createRenderer === "function");
  }

  // createSceneWebGPURendererOrFallback calls the real factory from the
  // sub-feature chunk. Returns null if the chunk isn't loaded yet, so the
  // caller can fall through to WebGL without waiting.
  function createSceneWebGPURendererOrFallback(canvas) {
    if (!sceneWebGPUAvailable()) return null;
    if (!canvas || typeof canvas.getContext !== "function") return null;
    try {
      return window.__gosx_scene3d_webgpu_api.createRenderer(canvas);
    } catch (e) {
      console.warn("[gosx] WebGPU renderer creation failed:", e);
      return null;
    }
  }
