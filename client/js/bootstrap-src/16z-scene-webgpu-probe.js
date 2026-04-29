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
  var _webgpuProbePromise = Promise.resolve(false);
  var _webgpuSupportedFeatures = [];
  var _webgpuRequestedFeatures = [];
  var _webgpuRequiredLimits = {};
  var _webgpuAdapterLimits = {};
  var _webgpuDeviceLimits = {};
  var _webgpuAdapterInfo = {};
  var _webgpuProbeError = "";
  var _webgpuDeviceLostInfo = null;
  var _webgpuProbeOptions = {};

  var WEBGPU_OPTIONAL_FEATURES = [
    "timestamp-query",
    "indirect-first-instance",
    "shader-f16",
    "texture-compression-bc",
    "texture-compression-bc-sliced-3d",
    "texture-compression-etc2",
    "texture-compression-astc",
    "texture-compression-astc-sliced-3d",
    "depth-clip-control",
    "depth32float-stencil8",
    "float32-filterable",
    "float32-blendable",
    "rg11b10ufloat-renderable",
    "bgra8unorm-storage",
    "clip-distances",
    "dual-source-blending",
    "subgroups",
    "subgroups-f16",
  ];

  var WEBGPU_LIMIT_NAMES = [
    "maxTextureDimension1D",
    "maxTextureDimension2D",
    "maxTextureDimension3D",
    "maxTextureArrayLayers",
    "maxBindGroups",
    "maxBindGroupsPlusVertexBuffers",
    "maxBindingsPerBindGroup",
    "maxDynamicUniformBuffersPerPipelineLayout",
    "maxDynamicStorageBuffersPerPipelineLayout",
    "maxSampledTexturesPerShaderStage",
    "maxSamplersPerShaderStage",
    "maxStorageBuffersPerShaderStage",
    "maxStorageTexturesPerShaderStage",
    "maxUniformBuffersPerShaderStage",
    "maxUniformBufferBindingSize",
    "maxStorageBufferBindingSize",
    "minUniformBufferOffsetAlignment",
    "minStorageBufferOffsetAlignment",
    "maxVertexBuffers",
    "maxBufferSize",
    "maxVertexAttributes",
    "maxVertexBufferArrayStride",
    "maxInterStageShaderComponents",
    "maxInterStageShaderVariables",
    "maxColorAttachments",
    "maxColorAttachmentBytesPerSample",
    "maxComputeWorkgroupStorageSize",
    "maxComputeInvocationsPerWorkgroup",
    "maxComputeWorkgroupSizeX",
    "maxComputeWorkgroupSizeY",
    "maxComputeWorkgroupSizeZ",
    "maxComputeWorkgroupsPerDimension",
  ];

  function sceneWebGPUFeatureList(features) {
    var out = [];
    if (!features) return out;
    if (typeof features.forEach === "function") {
      features.forEach(function(value) {
        if (typeof value === "string") out.push(value);
      });
    } else if (typeof features[Symbol.iterator] === "function") {
      for (var entry of features) {
        if (typeof entry === "string") out.push(entry);
      }
    } else if (Array.isArray(features)) {
      out = features.filter(function(value) { return typeof value === "string"; });
    }
    out.sort();
    return out.filter(function(value, index) { return index === 0 || out[index - 1] !== value; });
  }

  function sceneWebGPUFeatureSupported(adapter, feature) {
    var features = adapter && adapter.features;
    if (!features) return false;
    if (typeof features.has === "function") {
      return features.has(feature);
    }
    return sceneWebGPUFeatureList(features).indexOf(feature) >= 0;
  }

  function sceneWebGPURequestedFeatureList(adapter) {
    var out = [];
    for (var i = 0; i < WEBGPU_OPTIONAL_FEATURES.length; i++) {
      var feature = WEBGPU_OPTIONAL_FEATURES[i];
      if (!sceneWebGPUFeatureSupported(adapter, feature)) continue;
      if (feature === "texture-compression-bc-sliced-3d" && !sceneWebGPUFeatureSupported(adapter, "texture-compression-bc")) continue;
      if (feature === "texture-compression-astc-sliced-3d" && !sceneWebGPUFeatureSupported(adapter, "texture-compression-astc")) continue;
      if (feature === "subgroups-f16" && (!sceneWebGPUFeatureSupported(adapter, "subgroups") || !sceneWebGPUFeatureSupported(adapter, "shader-f16"))) continue;
      out.push(feature);
    }
    return out;
  }

  function sceneWebGPULimitsSnapshot(limits) {
    var out = {};
    if (!limits) return out;
    for (var i = 0; i < WEBGPU_LIMIT_NAMES.length; i++) {
      var name = WEBGPU_LIMIT_NAMES[i];
      var value = limits[name];
      if (Number.isFinite(Number(value))) {
        out[name] = Number(value);
      }
    }
    return out;
  }

  function sceneWebGPUAdapterInfoSnapshot(adapter) {
    var info = adapter && adapter.info;
    var out = {};
    if (!info || typeof info !== "object") return out;
    var keys = ["vendor", "architecture", "device", "description", "subgroupMinSize", "subgroupMaxSize"];
    for (var i = 0; i < keys.length; i++) {
      var key = keys[i];
      var value = info[key];
      if (typeof value === "string" && value) {
        out[key] = value;
      } else if (Number.isFinite(Number(value))) {
        out[key] = Number(value);
      }
    }
    return out;
  }

  function sceneWebGPUProbeSnapshot() {
    return {
      ready: !!_webgpuAdapterReady,
      adapterAvailable: _webgpuAdapterProbe !== false && _webgpuAdapterProbe !== null,
      deviceAvailable: _webgpuDeviceProbe !== false && _webgpuDeviceProbe !== null,
      supportedFeatures: _webgpuSupportedFeatures.slice(),
      requestedFeatures: _webgpuRequestedFeatures.slice(),
      deviceFeatures: sceneWebGPUFeatureList(_webgpuDeviceProbe && _webgpuDeviceProbe.features),
      requiredLimits: Object.assign({}, _webgpuRequiredLimits),
      adapterLimits: Object.assign({}, _webgpuAdapterLimits),
      deviceLimits: Object.assign({}, _webgpuDeviceLimits),
      adapterInfo: Object.assign({}, _webgpuAdapterInfo),
      error: _webgpuProbeError,
      lost: _webgpuDeviceLostInfo,
      probeOptions: Object.assign({}, _webgpuProbeOptions),
    };
  }

  function sceneWebGPUDiagnostics() {
    return sceneWebGPUProbeSnapshot();
  }

  // Shared probe helper. Callers in 16a-scene-webgpu.js use this to read
  // the current probe state without re-running the async adapter/device
  // request. Duplicated in 26e-feature-scene3d-webgpu-prefix.js for the
  // split webgpu sub-feature bundle (which does not include 16z), and
  // kept here for the legacy monolithic bootstrap.js bundle that inlines
  // 16a without the sub-feature prefix. When both definitions land in
  // the same IIFE (scene3d main bundle includes 16z but excludes 16a,
  // so no conflict), the function declaration is the only copy; when
  // bootstrap.js inlines 16a with 16z this function satisfies the
  // reference and the webgpu bundle's separate copy is loaded elsewhere.
  function _externalProbe() {
    if (typeof window !== "undefined" && typeof window.__gosx_scene3d_webgpu_probe === "function") {
      return window.__gosx_scene3d_webgpu_probe();
    }
    return { adapter: null, device: null, ready: false };
  }

  function _publishWebGPUProbeGlobals() {
    if (typeof window === "undefined") {
      return;
    }
    window.__gosx_scene3d_webgpu_probe = function() {
      return {
        adapter: _webgpuAdapterProbe,
        device: _webgpuDeviceProbe,
        ready: _webgpuAdapterReady,
        supportedFeatures: _webgpuSupportedFeatures.slice(),
        requestedFeatures: _webgpuRequestedFeatures.slice(),
        requiredLimits: Object.assign({}, _webgpuRequiredLimits),
        limits: Object.assign({}, _webgpuAdapterLimits),
        adapterInfo: Object.assign({}, _webgpuAdapterInfo),
        error: _webgpuProbeError,
        lost: _webgpuDeviceLostInfo,
        probeOptions: Object.assign({}, _webgpuProbeOptions),
      };
    };
    window.__gosx_scene3d_webgpu_diagnostics = sceneWebGPUDiagnostics;
    window.__gosx_scene3d_webgpu_probe_ready = function() {
      return _webgpuProbePromise.then(function() {
        return _webgpuAdapterReady;
      }).catch(function() {
        return false;
      });
    };
  }

  function sceneWebGPUPowerPreference(value) {
    var normalized = String(value || "").trim().toLowerCase();
    if (normalized === "high-performance" || normalized === "low-power") {
      return normalized;
    }
    return "";
  }

  function sceneWebGPUProbeOptionsFromManifest() {
    var manifest = sceneWebGPUManifest();
    var engines = manifest && Array.isArray(manifest.engines) ? manifest.engines : [];
    var powerPreference = "";
    for (var i = 0; i < engines.length; i++) {
      var entry = engines[i];
      if (!entry || entry.component !== "GoSXScene3D") {
        continue;
      }
      var props = entry.props && typeof entry.props === "object" ? entry.props : {};
      var requested = sceneWebGPUPowerPreference(
        props.webgpuPowerPreference ||
        props.webGPUPowerPreference ||
        props.webgpuAdapterPowerPreference ||
        props.webGPUAdapterPowerPreference
      );
      if (requested === "high-performance") {
        powerPreference = requested;
        break;
      }
      if (requested === "low-power") {
        powerPreference = requested;
      }
    }
    return powerPreference ? { powerPreference: powerPreference } : {};
  }

  function sceneWebGPUManifest() {
    if (typeof loadManifest === "function") {
      return loadManifest();
    }
    return null;
  }

  function sceneWebGPURequiredLimitsFromManifest() {
    var manifest = sceneWebGPUManifest();
    var limits = {};
    var groups = [
      manifest && Array.isArray(manifest.engines) ? manifest.engines : [],
      manifest && Array.isArray(manifest.computeIslands) ? manifest.computeIslands : [],
      manifest && Array.isArray(manifest.islands) ? manifest.islands : [],
    ];
    for (var groupIndex = 0; groupIndex < groups.length; groupIndex++) {
      var group = groups[groupIndex];
      for (var i = 0; i < group.length; i++) {
        var entry = group[i];
        var required = entry && Array.isArray(entry.requiredCapabilities) ? entry.requiredCapabilities : [];
        sceneWebGPUCollectRequiredLimits(required, limits);
      }
    }
    return limits;
  }

  function sceneWebGPUCollectRequiredLimits(required, out) {
    for (var i = 0; i < required.length; i++) {
      var raw = String(required[i] || "").trim();
      var lower = raw.toLowerCase();
      var body = "";
      if (lower.indexOf("webgpu:device-limit:") === 0) {
        body = raw.slice("webgpu:device-limit:".length);
      } else if (lower.indexOf("webgpu:limit:") === 0) {
        body = raw.slice("webgpu:limit:".length);
      } else if (lower.indexOf("webgpu-limit:") === 0) {
        body = raw.slice("webgpu-limit:".length);
      } else {
        continue;
      }
      var parsed = sceneWebGPUParseLimitRequirement(body);
      if (!parsed) {
        continue;
      }
      var name = sceneWebGPUCanonicalLimitName(parsed.name);
      if (!name) {
        continue;
      }
      var value = sceneWebGPURequiredLimitValue(parsed);
      if (!Number.isFinite(value)) {
        continue;
      }
      if (!Number.isFinite(Number(out[name])) || value > Number(out[name])) {
        out[name] = value;
      }
    }
  }

  function sceneWebGPUParseLimitRequirement(requirement) {
    var text = String(requirement || "").trim();
    var match = text.match(/^([a-z0-9_.:-]+)\s*(>=|<=|==|>|<|=|:)\s*([0-9]+(?:\.[0-9]+)?)$/i);
    if (!match) {
      return null;
    }
    var value = Number(match[3]);
    if (!Number.isFinite(value)) {
      return null;
    }
    return {
      name: match[1],
      operator: match[2] === ":" ? ">=" : match[2],
      value: value,
    };
  }

  function sceneWebGPURequiredLimitValue(parsed) {
    if (!parsed) {
      return NaN;
    }
    switch (parsed.operator) {
      case ">":
        return Math.floor(parsed.value) + 1;
      case "<":
      case "<=":
        return NaN;
      default:
        return Math.ceil(parsed.value);
    }
  }

  function sceneWebGPUCanonicalLimitName(name) {
    var wanted = sceneWebGPUNormalizedLimitName(name);
    if (!wanted) {
      return "";
    }
    for (var i = 0; i < WEBGPU_LIMIT_NAMES.length; i++) {
      var candidate = WEBGPU_LIMIT_NAMES[i];
      if (sceneWebGPUNormalizedLimitName(candidate) === wanted) {
        return candidate;
      }
    }
    return "";
  }

  function sceneWebGPUNormalizedLimitName(name) {
    return String(name || "").trim().toLowerCase().replace(/[^a-z0-9]/g, "");
  }

  if (typeof navigator !== "undefined" && navigator.gpu && typeof navigator.gpu.requestAdapter === "function") {
    // The default stays unbounded because some backends (SwiftShader in
    // headless Chrome, certain Linux Mesa/ANGLE builds) return null when
    // forced to "high-performance". Pages that need an adapter class can
    // opt in through Scene3D's webgpuPowerPreference prop; the manifest is
    // already in the document before this deferred feature script runs.
    _webgpuProbeOptions = sceneWebGPUProbeOptionsFromManifest();
    var adapterRequest = _webgpuProbeOptions && _webgpuProbeOptions.powerPreference ? _webgpuProbeOptions : undefined;
    _webgpuProbePromise = navigator.gpu.requestAdapter(adapterRequest).then(function(adapter) {
      if (!adapter) {
        _webgpuProbeError = "requestAdapter returned null";
        console.warn("[gosx] WebGPU probe: " + _webgpuProbeError);
        _webgpuAdapterProbe = false;
        _webgpuDeviceProbe = false;
        return false;
      }
      _webgpuAdapterProbe = adapter;
      _webgpuSupportedFeatures = sceneWebGPUFeatureList(adapter.features);
      _webgpuRequestedFeatures = sceneWebGPURequestedFeatureList(adapter);
      _webgpuRequiredLimits = sceneWebGPURequiredLimitsFromManifest();
      _webgpuAdapterLimits = sceneWebGPULimitsSnapshot(adapter.limits);
      _webgpuAdapterInfo = sceneWebGPUAdapterInfoSnapshot(adapter);
      // Verify device creation actually succeeds — this is where
      // partial implementations (SwiftShader WebGPU, constrained
      // mobile GPUs, broken ANGLE backends) fail. We don't mark
      // WebGPU "ready" until the device itself is in hand.
      var descriptor = {};
      if (_webgpuRequestedFeatures.length > 0) {
        descriptor.requiredFeatures = _webgpuRequestedFeatures;
      }
      if (Object.keys(_webgpuRequiredLimits).length > 0) {
        descriptor.requiredLimits = Object.assign({}, _webgpuRequiredLimits);
      }
      return adapter.requestDevice(descriptor);
    }).then(function(device) {
      if (device === false) {
        return;
      }
      if (!device) {
        _webgpuProbeError = "requestDevice returned null";
        console.warn("[gosx] WebGPU probe: " + _webgpuProbeError);
        _webgpuDeviceProbe = false;
        return;
      }
      _webgpuDeviceProbe = device;
      _webgpuDeviceLimits = sceneWebGPULimitsSnapshot(device.limits);
      _webgpuAdapterReady = true;
      // Invalidate the probe if the device is ever lost post-probe —
      // consumers re-check sceneWebGPUAvailable() on each mount.
      device.lost.then(function(info) {
        _webgpuDeviceLostInfo = {
          reason: info && info.reason || "",
          message: info && info.message || "",
        };
        console.warn("[gosx] WebGPU probe device lost:", info && info.message);
        _webgpuAdapterReady = false;
        _webgpuDeviceProbe = false;
      }).catch(function() {});
      return true;
    }).catch(function(err) {
      _webgpuProbeError = String(err && (err.message || err) || "unknown error");
      console.warn("[gosx] WebGPU probe failed:", _webgpuProbeError);
      _webgpuAdapterProbe = false;
      _webgpuDeviceProbe = false;
      return false;
    });
  } else {
    _webgpuAdapterProbe = false;
    _webgpuDeviceProbe = false;
  }
  // Share the probe (including the pre-obtained device) with the
  // sub-feature chunk so it doesn't re-probe and can skip its own
  // async device creation entirely. The ready promise lets the mount
  // path wait for WebGPU before falling through to WebGL.
  _publishWebGPUProbeGlobals();

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
  function createSceneWebGPURendererOrFallback(canvas, options) {
    if (!sceneWebGPUAvailable()) return null;
    if (!canvas || typeof canvas.getContext !== "function") return null;
    try {
      var renderer = window.__gosx_scene3d_webgpu_api.createRenderer(canvas, options || {});
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

  if (typeof sceneBackendRegistry !== "undefined" && sceneBackendRegistry) {
    sceneBackendRegistry.register("webgpu", {
      capabilities: ["webgpu", "shaders", "instancing", "compute", "shadows"],
      available: function() {
        return sceneWebGPUAvailable();
      },
      create: function(canvas, props, capability) {
        var options = typeof sceneWebGPUOptions === "function" ? sceneWebGPUOptions(props, capability) : {};
        return createSceneWebGPURendererOrFallback(canvas, options);
      },
    });
  }

  if (typeof window !== "undefined" && window.__gosx_scene3d_api) {
    window.__gosx_scene3d_api.sceneWebGPUDiagnostics = sceneWebGPUDiagnostics;
  }
