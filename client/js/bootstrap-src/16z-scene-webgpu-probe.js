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
  // The probe here is full-lifecycle: we call both requestAdapter AND
  // adapter.requestDevice, and only mark WebGPU "ready" when BOTH succeed.
  // Checking just the adapter was not enough — on systems with a partial
  // WebGPU implementation (SwiftShader in headless, constrained mobile
  // GPUs, some ANGLE backends), requestAdapter returns an adapter but
  // requestDevice later fails with an internal error. If we had already
  // tainted the canvas with canvas.getContext("webgpu") by that point
  // (as the 16a factory used to do eagerly), the canvas becomes unusable
  // for WebGL and the scene gets stuck with a broken WebGPU context —
  // exactly the "shader/context-loss" symptom reported against v0.17.15.
  //
  // The probed device is cached and handed to createSceneWebGPURenderer
  // via this probe function, so the renderer reuses the exact adapter +
  // device that succeeded at probe time instead of re-issuing device
  // creation (which could race or fail again).

  var _webgpuAdapterProbe = null; // null = unprobed, false = unavailable, GPUAdapter = ready
  var _webgpuDeviceProbe = null;  // null = unprobed, false = unavailable, GPUDevice = ready
  var _webgpuAdapterReady = false;

  if (typeof navigator !== "undefined" && navigator.gpu && typeof navigator.gpu.requestAdapter === "function") {
    navigator.gpu.requestAdapter({ powerPreference: "high-performance" }).then(function(adapter) {
      if (!adapter) {
        _webgpuAdapterProbe = false;
        _webgpuDeviceProbe = false;
        return null;
      }
      _webgpuAdapterProbe = adapter;
      // Verify device creation actually succeeds — this is where
      // partial implementations (SwiftShader WebGPU, constrained
      // mobile GPUs, broken ANGLE backends) fail. We don't mark
      // WebGPU "ready" until the device itself is in hand.
      return adapter.requestDevice();
    }).then(function(device) {
      if (!device) {
        _webgpuDeviceProbe = false;
        return;
      }
      _webgpuDeviceProbe = device;
      _webgpuAdapterReady = true;
      // Invalidate the probe if the device is ever lost post-probe —
      // consumers re-check sceneWebGPUAvailable() on each mount.
      device.lost.then(function(info) {
        console.warn("[gosx] WebGPU probe device lost:", info && info.message);
        _webgpuAdapterReady = false;
        _webgpuDeviceProbe = false;
      }).catch(function() {});
    }).catch(function(err) {
      console.warn("[gosx] WebGPU probe failed:", err && err.message);
      _webgpuAdapterProbe = false;
      _webgpuDeviceProbe = false;
    });
    // Share the probe (including the pre-obtained device) with the
    // sub-feature chunk so it doesn't re-probe and can skip its own
    // async device creation entirely.
    window.__gosx_scene3d_webgpu_probe = function() {
      return {
        adapter: _webgpuAdapterProbe,
        device: _webgpuDeviceProbe,
        ready: _webgpuAdapterReady,
      };
    };
  } else {
    _webgpuAdapterProbe = false;
    _webgpuDeviceProbe = false;
  }

  // sceneWebGPUAvailable returns true only when BOTH the adapter+device
  // probe succeeded AND the sub-feature chunk has loaded its factory.
  // Any of (probe pending, probe failed, chunk not loaded) → false,
  // and the mount code falls through to the WebGL renderer with a
  // CLEAN canvas (we never called getContext("webgpu") on it).
  function sceneWebGPUAvailable() {
    return _webgpuAdapterReady
      && _webgpuAdapterProbe !== false
      && _webgpuAdapterProbe !== null
      && _webgpuDeviceProbe !== false
      && _webgpuDeviceProbe !== null
      && !!(window.__gosx_scene3d_webgpu_api
        && typeof window.__gosx_scene3d_webgpu_api.createRenderer === "function");
  }

  // createSceneWebGPURendererOrFallback calls the real factory from the
  // sub-feature chunk ONLY when the probe confirmed both adapter + device
  // work. Returns null otherwise so the caller can fall through to
  // WebGL without having tainted the canvas.
  function createSceneWebGPURendererOrFallback(canvas) {
    if (!sceneWebGPUAvailable()) return null;
    if (!canvas || typeof canvas.getContext !== "function") return null;
    try {
      var renderer = window.__gosx_scene3d_webgpu_api.createRenderer(canvas);
      // Defensive: the sub-feature factory may still return null if it
      // hits an internal error after getContext but before handing back
      // a renderer object. In that case the canvas is tainted — there's
      // nothing the mount code can do to fall back — so we log loudly.
      if (!renderer) {
        console.warn("[gosx] WebGPU factory returned null after probe success; canvas may be tainted");
      }
      return renderer;
    } catch (e) {
      console.warn("[gosx] WebGPU renderer creation failed:", e);
      return null;
    }
  }
