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
  var _webgpuRequiredFeatures = [];
  var _webgpuRequiredLimits = {};
  var _webgpuAdapterLimits = {};
  var _webgpuDeviceLimits = {};
  var _webgpuAdapterInfo = {};
  var _webgpuProbeError = "";
  var _webgpuDeviceLostInfo = null;
  var _webgpuProbeOptions = {};
  var _webgpuProbeRetryCount = 0;
  var _webgpuProbeWarnings = [];
  var _webgpuProbeInFlight = false;
  var _webgpuLastProbeStartedAt = 0;
  var WEBGPU_LOST_REPROBE_INTERVAL_MS = 1000;
  var WEBGPU_LOST_REPROBE_WINDOW_MS = 10000;
  var WEBGPU_LOST_REPROBE_MAX_PER_WINDOW = 3;
  var WEBGPU_LOST_REPROBE_BACKOFF_MS = 30000;
  var _webgpuLostProbeWindowStartedAt = 0;
  var _webgpuLostProbeCount = 0;
  var _webgpuLostProbeBackoffUntil = 0;

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
    var requestAllOptional = sceneWebGPUOptionalFeaturesRequestedFromManifest();
    var requestAdaptiveTiming = sceneWebGPUAdaptiveTimingRequestedFromManifest();
    if (!requestAllOptional && !requestAdaptiveTiming) {
      return [];
    }
    var out = [];
    for (var i = 0; i < WEBGPU_OPTIONAL_FEATURES.length; i++) {
      var feature = WEBGPU_OPTIONAL_FEATURES[i];
      if (!requestAllOptional && feature !== "timestamp-query") continue;
      if (!sceneWebGPUFeatureSupported(adapter, feature)) continue;
      if (feature === "texture-compression-bc-sliced-3d" && !sceneWebGPUFeatureSupported(adapter, "texture-compression-bc")) continue;
      if (feature === "texture-compression-astc-sliced-3d" && !sceneWebGPUFeatureSupported(adapter, "texture-compression-astc")) continue;
      if (feature === "subgroups-f16" && (!sceneWebGPUFeatureSupported(adapter, "subgroups") || !sceneWebGPUFeatureSupported(adapter, "shader-f16"))) continue;
      out.push(feature);
    }
    return out;
  }

  function sceneWebGPUAdaptiveTimingRequestedFromManifest() {
    var manifest = sceneWebGPUManifest();
    var engines = manifest && Array.isArray(manifest.engines) ? manifest.engines : [];
    for (var i = 0; i < engines.length; i++) {
      var entry = engines[i];
      if (!entry || entry.component !== "GoSXScene3D") continue;
      var props = entry.props && typeof entry.props === "object" ? entry.props : {};
      var adaptive = props.adaptiveQuality != null
        ? props.adaptiveQuality
        : (props.adaptivePerformance != null ? props.adaptivePerformance : props.dynamicQuality);
      if (adaptive && typeof adaptive === "object") {
        if (adaptive.enabled !== false) return true;
      } else if (sceneWebGPUTruthy(adaptive)) {
        return true;
      }
    }
    return false;
  }

  function sceneWebGPUOptionalFeaturesRequestedFromManifest() {
    var manifest = sceneWebGPUManifest();
    var engines = manifest && Array.isArray(manifest.engines) ? manifest.engines : [];
    for (var i = 0; i < engines.length; i++) {
      var entry = engines[i];
      if (!entry || entry.component !== "GoSXScene3D") {
        continue;
      }
      var props = entry.props && typeof entry.props === "object" ? entry.props : {};
      if (sceneWebGPUTruthy(props.webgpuOptionalFeatures) || sceneWebGPUTruthy(props.webGPUOptionalFeatures)) {
        return true;
      }
    }
    return false;
  }

  function sceneWebGPUTruthy(value) {
    if (value === true) return true;
    if (typeof value === "string") {
      var normalized = value.trim().toLowerCase();
      return normalized === "1" || normalized === "true" || normalized === "yes" || normalized === "on";
    }
    return false;
  }

  function sceneWebGPUMergeFeatureLists() {
    var out = [];
    var seen = {};
    for (var listIndex = 0; listIndex < arguments.length; listIndex++) {
      var list = arguments[listIndex];
      if (!Array.isArray(list)) {
        continue;
      }
      for (var i = 0; i < list.length; i++) {
        var feature = sceneWebGPUNormalizeFeatureName(list[i]);
        if (!feature || seen[feature]) {
          continue;
        }
        seen[feature] = true;
        out.push(feature);
      }
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
      requiredFeatures: _webgpuRequiredFeatures.slice(),
      deviceFeatures: sceneWebGPUFeatureList(_webgpuDeviceProbe && _webgpuDeviceProbe.features),
      requiredLimits: Object.assign({}, _webgpuRequiredLimits),
      adapterLimits: Object.assign({}, _webgpuAdapterLimits),
      deviceLimits: Object.assign({}, _webgpuDeviceLimits),
      adapterInfo: Object.assign({}, _webgpuAdapterInfo),
      error: _webgpuProbeError,
      lost: _webgpuDeviceLostInfo,
      probeOptions: Object.assign({}, _webgpuProbeOptions),
      retryCount: _webgpuProbeRetryCount,
      warnings: _webgpuProbeWarnings.slice(),
    };
  }

  function sceneWebGPUDiagnostics() {
    sceneWebGPUMaybeRetryUnavailableProbe();
    return sceneWebGPUProbeSnapshot();
  }

  function sceneWebGPUDispatchProbeReady(recoveredFromLoss) {
    if (!recoveredFromLoss || typeof window === "undefined" || typeof window.dispatchEvent !== "function") {
      return;
    }
    try {
      var detail = sceneWebGPUProbeSnapshot();
      var event = typeof CustomEvent === "function"
        ? new CustomEvent("gosx:scene3d:webgpu-probe-ready", { detail: detail })
        : { type: "gosx:scene3d:webgpu-probe-ready", detail: detail };
      window.dispatchEvent(event);
    } catch (_err) {}
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
        requiredFeatures: _webgpuRequiredFeatures.slice(),
        requiredLimits: Object.assign({}, _webgpuRequiredLimits),
        limits: Object.assign({}, _webgpuAdapterLimits),
        adapterInfo: Object.assign({}, _webgpuAdapterInfo),
        error: _webgpuProbeError,
        lost: _webgpuDeviceLostInfo,
        probeOptions: Object.assign({}, _webgpuProbeOptions),
        retryCount: _webgpuProbeRetryCount,
        warnings: _webgpuProbeWarnings.slice(),
        lostProbeCount: _webgpuLostProbeCount,
        lostProbeBackoffUntil: _webgpuLostProbeBackoffUntil,
      };
    };
    window.__gosx_scene3d_webgpu_diagnostics = sceneWebGPUDiagnostics;
    window.__gosx_scene3d_webgpu_probe_ready = function() {
      sceneWebGPUMaybeRetryUnavailableProbe();
      return _webgpuProbePromise.then(function() {
        return _webgpuAdapterReady;
      }).catch(function() {
        return false;
      });
    };
    window.__gosx_scene3d_webgpu_probe_invalidate = function(info) {
      return sceneWebGPUInvalidateProbe(info);
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

  function sceneWebGPURequiredFeaturesFromManifest(adapter) {
    var manifest = sceneWebGPUManifest();
    var out = [];
    var seen = {};
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
        for (var j = 0; j < required.length; j++) {
          var feature = sceneWebGPUFeatureFromCapability(required[j]);
          if (!feature || seen[feature] || !sceneWebGPUFeatureSupported(adapter, feature)) {
            continue;
          }
          seen[feature] = true;
          out.push(feature);
        }
      }
    }
    out.sort();
    return out;
  }

  function sceneWebGPUFeatureFromCapability(capability) {
    var raw = String(capability || "").trim();
    var lower = raw.toLowerCase();
    var feature = "";
    if (lower.indexOf("webgpu-feature:") === 0) {
      feature = raw.slice("webgpu-feature:".length);
    } else if (lower.indexOf("webgpu:") === 0) {
      feature = raw.slice("webgpu:".length);
    } else {
      return "";
    }
    feature = sceneWebGPUNormalizeFeatureName(feature);
    if (!feature || feature === "webgpu" || feature.indexOf("limit:") === 0 || feature.indexOf("device-limit:") === 0 || feature.indexOf("adapter-limit:") === 0) {
      return "";
    }
    return feature;
  }

  function sceneWebGPUNormalizeFeatureName(feature) {
    var normalized = String(feature || "").trim().toLowerCase();
    return /^[a-z0-9-]+$/.test(normalized) ? normalized : "";
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

  function sceneWebGPURememberAdapter(adapter) {
    _webgpuAdapterProbe = adapter;
    _webgpuSupportedFeatures = sceneWebGPUFeatureList(adapter.features);
    _webgpuRequiredFeatures = sceneWebGPURequiredFeaturesFromManifest(adapter);
    _webgpuRequestedFeatures = sceneWebGPUMergeFeatureLists(sceneWebGPURequestedFeatureList(adapter), _webgpuRequiredFeatures);
    _webgpuRequiredLimits = sceneWebGPURequiredLimitsFromManifest();
    _webgpuAdapterLimits = sceneWebGPULimitsSnapshot(adapter.limits);
    _webgpuAdapterInfo = sceneWebGPUAdapterInfoSnapshot(adapter);
  }

  function sceneWebGPUDeviceDescriptor() {
    var descriptor = null;
    if (_webgpuRequestedFeatures.length > 0) {
      descriptor = descriptor || {};
      descriptor.requiredFeatures = _webgpuRequestedFeatures;
    }
    if (Object.keys(_webgpuRequiredLimits).length > 0) {
      descriptor = descriptor || {};
      descriptor.requiredLimits = Object.assign({}, _webgpuRequiredLimits);
    }
    return descriptor;
  }

  function sceneWebGPURequestDevice(adapter, descriptor) {
    return descriptor ? adapter.requestDevice(descriptor) : adapter.requestDevice();
  }

  function sceneWebGPUDeviceLostSnapshot(info) {
    return {
      reason: info && info.reason || "",
      message: info && info.message || "",
    };
  }

  function sceneWebGPUWatchDeviceLoss(device) {
    if (!device || !device.lost || typeof device.lost.then !== "function") {
      return;
    }
    device.lost.then(function(info) {
      if (_webgpuDeviceProbe !== device) {
        return;
      }
      console.warn("[gosx] WebGPU probe device lost:", info && info.message);
      sceneWebGPUInvalidateProbe(info);
    }).catch(function() {});
  }

  function sceneWebGPUProbeNow() {
    if (typeof performance !== "undefined" && performance && typeof performance.now === "function") {
      return performance.now();
    }
    return Date.now();
  }

  function sceneWebGPUMaybeRetryUnavailableProbe() {
    if (!_webgpuDeviceLostInfo || _webgpuAdapterReady || _webgpuProbeInFlight) {
      return _webgpuProbePromise;
    }
    if (_webgpuAdapterProbe !== false && _webgpuDeviceProbe !== false) {
      return _webgpuProbePromise;
    }
    var now = sceneWebGPUProbeNow();
    if (_webgpuLostProbeBackoffUntil > now) {
      return _webgpuProbePromise;
    }
    if (now - _webgpuLastProbeStartedAt < WEBGPU_LOST_REPROBE_INTERVAL_MS) {
      return _webgpuProbePromise;
    }
    return sceneWebGPUStartProbe();
  }

  function sceneWebGPURecordProbeLoss() {
    var now = sceneWebGPUProbeNow();
    if (_webgpuLostProbeWindowStartedAt <= 0 || now - _webgpuLostProbeWindowStartedAt > WEBGPU_LOST_REPROBE_WINDOW_MS) {
      _webgpuLostProbeWindowStartedAt = now;
      _webgpuLostProbeCount = 0;
    }
    _webgpuLostProbeCount += 1;
    if (_webgpuLostProbeCount <= WEBGPU_LOST_REPROBE_MAX_PER_WINDOW) {
      return true;
    }
    _webgpuLostProbeBackoffUntil = now + WEBGPU_LOST_REPROBE_BACKOFF_MS;
    _webgpuProbeError = "device lost repeatedly; reprobe backed off";
    _webgpuProbeWarnings.push(_webgpuProbeError);
    console.warn("[gosx] WebGPU probe: " + _webgpuProbeError);
    return false;
  }

  function sceneWebGPUProbeDevice(adapter, adapterRequest, retried) {
    sceneWebGPURememberAdapter(adapter);
    var descriptor = sceneWebGPUDeviceDescriptor();
    return sceneWebGPURequestDevice(adapter, descriptor).catch(function(err) {
      if (descriptor || retried || !navigator.gpu || typeof navigator.gpu.requestAdapter !== "function") {
        throw err;
      }
      var message = String(err && (err.message || err) || "unknown error");
      _webgpuProbeRetryCount++;
      _webgpuProbeWarnings.push("requestDevice retry after: " + message);
      console.warn("[gosx] WebGPU probe requestDevice failed; retrying with a fresh adapter:", message);
      return navigator.gpu.requestAdapter(adapterRequest).then(function(retryAdapter) {
        if (!retryAdapter) {
          throw err;
        }
        return sceneWebGPUProbeDevice(retryAdapter, adapterRequest, true);
      });
    });
  }

  // sceneWebGPUProbeCanvasContext validates getContext("webgpu") + configure on
  // a 1x1 THROWAWAY canvas using the probed device. requestAdapter and
  // requestDevice can BOTH succeed on browser/driver combos that still fail to
  // create or configure a WebGPU canvas context ("Failed to create WebGPU
  // Context Provider" — seen on some Windows Edge/Firefox setups). The
  // adapter/device probe does not cover that step, so without this check the
  // real renderer calls getContext("webgpu") on the MOUNT canvas, taints it,
  // and fails — after which the WebGL2 fallback can no longer acquire a context
  // on that canvas (getContext("webgl2") returns null) and the scene reports
  // "could not acquire a renderer". Validating on a throwaway canvas keeps the
  // mount canvas clean so the fallback path works. Returns true only when the
  // browser can actually create AND configure a WebGPU canvas context.
  function sceneWebGPUProbeCanvasContext(device) {
    if (typeof document === "undefined" || typeof document.createElement !== "function") {
      return true; // non-DOM env: no mount canvas to taint; let the renderer decide
    }
    if (!device || typeof navigator === "undefined" || !navigator.gpu
        || typeof navigator.gpu.getPreferredCanvasFormat !== "function") {
      _webgpuProbeError = "navigator.gpu canvas API unavailable";
      return false;
    }
    var probeCanvas = document.createElement("canvas");
    probeCanvas.width = 1;
    probeCanvas.height = 1;
    var ctx = null;
    try {
      ctx = probeCanvas.getContext("webgpu");
    } catch (e) {
      _webgpuProbeError = "getContext(webgpu) threw: " + String(e && (e.message || e) || "unknown");
      console.warn("[gosx] WebGPU probe: " + _webgpuProbeError);
      return false;
    }
    if (!ctx) {
      _webgpuProbeError = "getContext(webgpu) returned null (context provider unavailable)";
      console.warn("[gosx] WebGPU probe: " + _webgpuProbeError);
      return false;
    }
    try {
      ctx.configure({
        device: device,
        format: navigator.gpu.getPreferredCanvasFormat(),
        alphaMode: "premultiplied",
      });
    } catch (e) {
      _webgpuProbeError = "canvas configure failed: " + String(e && (e.message || e) || "unknown");
      console.warn("[gosx] WebGPU probe: " + _webgpuProbeError);
      try { if (typeof ctx.unconfigure === "function") ctx.unconfigure(); } catch (_e) {}
      return false;
    }
    try { if (typeof ctx.unconfigure === "function") ctx.unconfigure(); } catch (_e) {}
    return true;
  }

  function sceneWebGPUStartProbe() {
    var now = sceneWebGPUProbeNow();
    if (_webgpuLostProbeBackoffUntil > now) {
      _webgpuAdapterReady = false;
      _webgpuAdapterProbe = false;
      _webgpuDeviceProbe = false;
      _webgpuProbePromise = Promise.resolve(false);
      return _webgpuProbePromise;
    }
    if (_webgpuProbeInFlight) {
      return _webgpuProbePromise;
    }
    _webgpuProbeInFlight = true;
    _webgpuLastProbeStartedAt = now;
    _webgpuAdapterReady = false;
    _webgpuAdapterProbe = null;
    _webgpuDeviceProbe = null;
    _webgpuDeviceLimits = {};
    _webgpuProbeError = "";
    if (!(typeof navigator !== "undefined" && navigator.gpu && typeof navigator.gpu.requestAdapter === "function")) {
      _webgpuAdapterProbe = false;
      _webgpuDeviceProbe = false;
      _webgpuProbeInFlight = false;
      _webgpuProbePromise = Promise.resolve(false);
      return _webgpuProbePromise;
    }
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
      // Verify device creation actually succeeds - this is where partial
      // implementations fail. Some headless Chrome/SwiftShader builds can
      // fail the first empty requestDevice() after page startup while a fresh
      // adapter succeeds immediately after, so empty descriptors get one
      // adapter reacquire retry. Required features/limits remain strict.
      return sceneWebGPUProbeDevice(adapter, adapterRequest, false);
    }).then(function(device) {
      if (device === false) {
        return false;
      }
      if (!device) {
        _webgpuProbeError = "requestDevice returned null";
        console.warn("[gosx] WebGPU probe: " + _webgpuProbeError);
        _webgpuDeviceProbe = false;
        return false;
      }
      // Full-lifecycle gate: confirm the browser can actually create AND
      // configure a WebGPU canvas context (on a throwaway canvas) before we
      // declare WebGPU available. This is the step that fails on some
      // Windows Edge/Firefox GPU+driver combos despite a working
      // adapter+device; catching it here keeps the mount canvas clean so the
      // WebGL2 fallback can acquire a context instead of dying with
      // "could not acquire a renderer".
      if (!sceneWebGPUProbeCanvasContext(device)) {
        if (!_webgpuProbeError) { _webgpuProbeError = "canvas webgpu context unavailable"; }
        _webgpuAdapterProbe = false;
        _webgpuDeviceProbe = false;
        return false;
      }
      var recoveredFromLoss = !!_webgpuDeviceLostInfo;
      _webgpuDeviceProbe = device;
      _webgpuDeviceLimits = sceneWebGPULimitsSnapshot(device.limits);
      _webgpuDeviceLostInfo = null;
      _webgpuAdapterReady = true;
      if (!recoveredFromLoss || sceneWebGPUProbeNow() - _webgpuLostProbeWindowStartedAt > WEBGPU_LOST_REPROBE_WINDOW_MS) {
        _webgpuLostProbeWindowStartedAt = 0;
        _webgpuLostProbeCount = 0;
        _webgpuLostProbeBackoffUntil = 0;
      }
      // Invalidate and restart the probe if the device is ever lost post-probe.
      // Consumers re-check sceneWebGPUAvailable() on each mount/recovery.
      sceneWebGPUWatchDeviceLoss(device);
      sceneWebGPUDispatchProbeReady(recoveredFromLoss);
      return true;
    }).catch(function(err) {
      _webgpuProbeError = String(err && (err.message || err) || "unknown error");
      console.warn("[gosx] WebGPU probe failed:", _webgpuProbeError);
      _webgpuAdapterProbe = false;
      _webgpuDeviceProbe = false;
      return false;
    });
    _webgpuProbePromise = _webgpuProbePromise.then(function(result) {
      _webgpuProbeInFlight = false;
      return result;
    }, function(err) {
      _webgpuProbeInFlight = false;
      throw err;
    });
    return _webgpuProbePromise;
  }

  function sceneWebGPUInvalidateProbe(info) {
    _webgpuDeviceLostInfo = sceneWebGPUDeviceLostSnapshot(info);
    _webgpuAdapterReady = false;
    if (!sceneWebGPURecordProbeLoss()) {
      _webgpuAdapterProbe = false;
      _webgpuDeviceProbe = false;
      _webgpuDeviceLimits = {};
      _webgpuProbeInFlight = false;
      _webgpuProbePromise = Promise.resolve(false);
      return _webgpuProbePromise;
    }
    if (_webgpuAdapterProbe === null && _webgpuDeviceProbe === null) {
      return _webgpuProbePromise;
    }
    _webgpuAdapterProbe = null;
    _webgpuDeviceProbe = null;
    _webgpuDeviceLimits = {};
    return sceneWebGPUStartProbe();
  }

  sceneWebGPUStartProbe();
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
    sceneWebGPUMaybeRetryUnavailableProbe();
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
