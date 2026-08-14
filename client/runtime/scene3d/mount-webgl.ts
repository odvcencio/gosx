// mount-webgl.ts — WebGL renderer factory resolution and the lazy WebGL chunk loader.
// @ts-check
//
// A WebGPU-capable browser never runs the loader. It fetches
// bootstrap-feature-scene3d-webgl.js when the selection order in 20a puts
// WebGL first, and again when the WebGPU fallback ladder steps down to WebGL
// after a device loss.
//
// Depends on 20a for the selection verdict.

/**
 * @typedef {object} GoSXSceneWebGLMountHost
 * @property {(canvas: HTMLCanvasElement, props: object, capability: object) => object|null} create
 * @property {() => Promise<object|null>} load
 */
  // --------------------------------------------------------------------------
  // WebGL renderer factory resolution
  // --------------------------------------------------------------------------
  //
  // 16-scene-webgl.js moved into the lazily fetched
  // bootstrap-feature-scene3d-webgl.js chunk, so its factories are no longer
  // lexically in scope here. Two resolutions exist, and both must keep
  // working:
  //
  //   1. bootstrap.js (the monolith) keeps 16-scene-webgl.js inline, so the
  //      lexical binding is present and wins.
  //   2. The split scene3d chunk reads the factory the WebGL chunk published
  //      on window.__gosx_scene3d_webgl_api. It is null until the chunk lands.
  //
  // Use sceneWebGLRendererFactory as the readiness test. Never test
  // sceneLegacyWebGLRendererFactory: that returns the legacy vertex-colour
  // renderer, and treating it as "WebGL is ready" would silently downgrade a
  // PBR scene.
  function sceneWebGLChunkAPI() {
    return typeof window !== "undefined" && window.__gosx_scene3d_webgl_api
      ? window.__gosx_scene3d_webgl_api
      : null;
  }

  // sceneLegacyWebGLRendererFactory resolves the legacy vertex-colour
  // renderer. It lived in 10-runtime-scene-core.js, so this file used to call
  // it lexically. It now ships in 16e-scene-webgl-legacy.ts inside the WebGL
  // chunk, which lands after this file, so the lookup must happen at call
  // time. 16e assigns the function onto window.__gosx_scene3d_api when it
  // runs; bootstrap.js keeps the lexical binding and wins first.
  function sceneLegacyWebGLRendererFactory() {
    if (typeof createSceneWebGLRenderer === "function") {
      return createSceneWebGLRenderer;
    }
    const api = typeof window !== "undefined" ? window.__gosx_scene3d_api : null;
    return api && typeof api.createSceneWebGLRenderer === "function"
      ? api.createSceneWebGLRenderer
      : null;
  }

  function sceneWebGLRendererFactory() {
    if (typeof createScenePBRRendererOrFallback === "function") {
      return createScenePBRRendererOrFallback;
    }
    const api = sceneWebGLChunkAPI();
    return api && typeof api.createScenePBRRendererOrFallback === "function"
      ? api.createScenePBRRendererOrFallback
      : null;
  }

  function sceneWaterWebGLRendererFactory() {
    if (typeof createSceneWaterRendererWebGL === "function") {
      return createSceneWaterRendererWebGL;
    }
    const api = sceneWebGLChunkAPI();
    return api && typeof api.createSceneWaterRendererWebGL === "function"
      ? api.createSceneWaterRendererWebGL
      : null;
  }

  function createSceneWebGLResult(canvas, props, capability, fallbackReason) {
    // Water scenes that land on WebGL (e.g. after a WebGPU device loss /
    // watchdog fallback, or any inline webgl selection) must render via the
    // WebGL2 water runtime, not the generic PBR path which cannot draw the
    // simulation.
    if (sceneFirstWaterEntry(props)) {
      var waterResult = createSceneWaterWebGLResult(canvas, props, capability, fallbackReason);
      if (waterResult) {
        return waterResult;
      }
      // A generic PBR/WebGL renderer cannot draw the water simulation. Return
      // an explicit unsupported result so callers never mistake a valid GL
      // context and a blank generic scene for a working water backend.
      return {
        renderer: null,
        fallbackReason: fallbackReason || "",
        unsupportedReason: "water-webgl2-unavailable",
      };
    }
    const pbrFactory = sceneWebGLRendererFactory();
    if (pbrFactory) {
      const useCanvasAlpha = sceneCanvasAlpha(props);
      const gl = typeof canvas.getContext === "function" ? canvas.getContext("webgl2", {
        alpha: useCanvasAlpha,
        premultipliedAlpha: useCanvasAlpha,
        antialias: sceneWebGLAntialias(props, capability),
        powerPreference: capability.lowPower || capability.tier === "constrained" ? "low-power" : "high-performance",
      }) : null;
      if (gl) {
        const pbrRenderer = pbrFactory(gl, canvas, {});
        if (pbrRenderer) { return { renderer: pbrRenderer, fallbackReason: fallbackReason, degraded: [] }; }
      }
    }
    const legacyFactory = sceneLegacyWebGLRendererFactory();
    if (!legacyFactory) {
      return null;
    }
    const webglRenderer = legacyFactory(canvas, {
      antialias: sceneWebGLAntialias(props, capability),
      powerPreference: capability.lowPower || capability.tier === "constrained" ? "low-power" : "high-performance",
    });
    return webglRenderer ? { renderer: webglRenderer, fallbackReason: fallbackReason, degraded: [] } : null;
  }

  function sceneFirstWaterEntry(props) {
    var scene = props && props.scene && typeof props.scene === "object" ? props.scene : null;
    var systems = scene && Array.isArray(scene.waterSystems) ? scene.waterSystems : null;
    if (!systems || !systems.length) return null;
    return systems[0] || null;
  }

  function sceneWebGLAntialias(props, capability) {
    var caps = capability || {};
    var requestedSamples = Math.max(0, Math.floor(sceneNumber(props && props.msaaSamples, 0)));
    if (requestedSamples > 1) return true;
    if (requestedSamples === 1) return false;
    var tierDefault = caps.tier === "full" && !caps.lowPower && !caps.reducedData;
    return sceneBool(props && props.antialias, tierDefault);
  }

  // createSceneWaterWebGLResult builds the WebGL2 water runtime for a water
  // scene. It is the single construction point shared by (a) the real A3
  // capability-gate fallback (WebGPU unavailable / lost) and (b) the
  // device-loss recovery path.
  function createSceneWaterWebGLResult(canvas, props, capability, fallbackReason) {
    var waterFactory = sceneWaterWebGLRendererFactory();
    if (!waterFactory) return null;
    var entry = sceneFirstWaterEntry(props);
    if (!entry) return null;
    var gl = typeof canvas.getContext === "function" ? canvas.getContext("webgl2", {
      alpha: false, premultipliedAlpha: false, antialias: sceneWebGLAntialias(props, capability), depth: true,
      powerPreference: capability && (capability.lowPower || capability.tier === "constrained") ? "low-power" : "high-performance",
    }) : null;
    if (!gl) return null;
    var renderer = null;
    try {
      renderer = waterFactory(gl, canvas, entry);
    } catch (e) {
      try { console.warn("[gosx] WebGL water renderer failed:", e); } catch (_e) {}
      return null;
    }
    if (!renderer) return null;
    try {
      if (typeof window !== "undefined") {
        window.__gosx_scene3d_webgl_water = true;
      }
    } catch (_e) {}
    return { renderer: renderer, fallbackReason: fallbackReason || "", degraded: [] };
  }

  // sceneWaterWebGLAutoResult is the real A3 capability-gate selection: for a
  // water scene whose active backend resolves to WebGL (because WebGPU is
  // unavailable, failed the probe, or its device was lost), render the water on
  // the WebGL2 water runtime instead of the generic PBR path (which cannot draw
  // the simulation). Returns null when WebGPU will render the scene — WebGPU
  // stays primary — or when this is not a water scene.
  function sceneWaterWebGLAutoResult(canvas, props, capability) {
    if (!sceneFirstWaterEntry(props)) return null;
    var webgpuAvail = typeof sceneWebGPUAvailable === "function" && sceneWebGPUAvailable();
    var backendCaps = sceneBackendCapsOf(props);
    var verdict = chooseSceneBackend(backendCaps, {
      requireWebGL: sceneRequiresWebGL(props),
      forceWebGL: sceneForcesWebGL(props),
      preferCanvas: sceneBool(props && props.preferCanvas, false),
      preferWebGPU: sceneCapabilityWebGPUPreference(props, capability) === "prefer",
    }, { webgpu: webgpuAvail, webgl: true });
    if (!verdict) return null;
    // WebGPU stays primary: defer to the normal path when WebGPU is the
    // resolved + available backend.
    if (verdict.backend === "webgpu" && webgpuAvail) return null;
    // Only intercept when WebGL2 is the active backend for this water scene.
    if (verdict.backend !== "webgl") return null;
    return createSceneWaterWebGLResult(canvas, props, capability, verdict.fallbackReason || "webgpu-unavailable") || {
      renderer: null,
      fallbackReason: verdict.fallbackReason || "webgpu-unavailable",
      unsupportedReason: "water-webgl2-unavailable",
    };
  }

  function createSceneRenderer(canvas, props, capability) {
    // A3: real capability-gate fallback. For a water scene whose backend
    // resolves to WebGL (WebGPU unavailable / probe-failed / device lost), use
    // the WebGL2 water runtime up front so the registry/inline paths don't grab
    // the generic PBR renderer. WebGPU-capable sessions skip this (returns null)
    // and keep WebGPU primary.
    const autoWater = sceneWaterWebGLAutoResult(canvas, props, capability);
    if (autoWater) {
      return autoWater;
    }
    const registryResult = createSceneRendererFromRegistry(canvas, props, capability);
    if (registryResult) {
      return registryResult;
    }

    const webglPreference = sceneCapabilityWebGLPreference(props, capability);
    const webgpuPreference = sceneCapabilityWebGPUPreference(props, capability);
    const webgpuAvail = typeof sceneWebGPUAvailable === "function" && sceneWebGPUAvailable();
    const prefs = {
      requireWebGL: sceneRequiresWebGL(props),
      forceWebGL: sceneForcesWebGL(props),
      preferCanvas: sceneBool(props && props.preferCanvas, false),
      preferWebGPU: webgpuPreference === "prefer",
    };
    const backendCaps = sceneBackendCapsOf(props);
    const verdict = chooseSceneBackend(backendCaps, prefs, { webgpu: webgpuAvail, webgl: true });
    if (verdict) {
      if (verdict.backend === "webgpu" && webgpuAvail && typeof createSceneWebGPURendererOrFallback === "function") {
        const gpuRenderer = createSceneWebGPURendererOrFallback(canvas, sceneWebGPUOptions(props, capability));
        if (gpuRenderer) {
          return { renderer: gpuRenderer, fallbackReason: verdict.fallbackReason || "", degraded: verdict.degraded || [] };
        }
      }
      if (verdict.backend === "webgl" || (verdict.backend === "webgpu" && !webgpuAvail)) {
        const fallback = verdict.backend === "webgpu" ? "webgpu-unavailable" : (verdict.fallbackReason || "");
        return createSceneWebGLResult(canvas, props, capability, fallback);
      }
      if (verdict.backend === "canvas2d") {
        if (sceneRequiresWebGL(props)) { return null; }
        const ctx2d = typeof canvas.getContext === "function" ? canvas.getContext("2d") : null;
        if (!ctx2d) { return null; }
        return { renderer: createSceneCanvasRenderer(ctx2d, canvas), fallbackReason: sceneRendererFallbackReason(props, capability, "canvas"), degraded: [] };
      }
      return null;
    }
    const webgpuFeatureGap = sceneNeedsWebGLForWebGPUCoverage(props);
    if (webgpuPreference === "prefer" && !webgpuFeatureGap && webgpuAvail && typeof createSceneWebGPURendererOrFallback === "function") {
      var gpuRenderer = createSceneWebGPURendererOrFallback(canvas, sceneWebGPUOptions(props, capability));
      if (gpuRenderer) {
        return {
          renderer: gpuRenderer,
          fallbackReason: "",
        };
      }
    }
    if (webglPreference === "prefer" || webglPreference === "force") {
      const webglResult = createSceneWebGLResult(canvas, props, capability, webgpuFeatureGap ? "webgpu-feature-gap" : "");
      if (webglResult) { return webglResult; }
    }
    if (sceneRequiresWebGL(props)) {
      return null;
    }
    const ctx2d = typeof canvas.getContext === "function" ? canvas.getContext("2d") : null;
    if (!ctx2d) {
      return null;
    }
    return {
      renderer: createSceneCanvasRenderer(ctx2d, canvas),
      fallbackReason: sceneRendererFallbackReason(props, capability, "canvas"),
    };
  }

  function createSceneRendererFromRegistry(canvas, props, capability) {
    if (typeof sceneBackendRegistry === "undefined" || !sceneBackendRegistry || typeof sceneBackendRegistry.candidates !== "function") {
      return null;
    }
    const webglPreference = sceneCapabilityWebGLPreference(props, capability);
    const webgpuPreference = sceneCapabilityWebGPUPreference(props, capability);
    const requireWebGL = sceneRequiresWebGL(props);
    const webgpuFeatureGap = sceneNeedsWebGLForWebGPUCoverage(props);
    const backendCaps = sceneBackendCapsOf(props);
    const webgpuAvail = typeof sceneWebGPUAvailable === "function" && sceneWebGPUAvailable();
    const verdict = chooseSceneBackend(backendCaps, {
      requireWebGL, forceWebGL: sceneForcesWebGL(props),
      preferCanvas: sceneBool(props && props.preferCanvas, false), preferWebGPU: webgpuPreference === "prefer",
    }, { webgpu: webgpuAvail, webgl: true });
    const preferWebGPU = verdict ? verdict.backend === "webgpu" : (webgpuPreference === "prefer" && !webgpuFeatureGap);
    const verdictFallback = verdict ? verdict.fallbackReason : "";
    // Same fix as sceneWebGLBackendRequest above: trust the verdict over the
    // raw avoid/prefer/force preference when backendCaps gave chooseSceneBackend
    // enough to resolve one. See that function for the full incident note.
    const wantsWebGL = verdict
      ? verdict.backend === "webgl"
      : (webglPreference === "prefer" || webglPreference === "force");
    const allowWebGL = wantsWebGL && sceneBackendCapsAllowsKind(backendCaps, "webgl");
    const request = {
      props,
      capability,
      webgpu: preferWebGPU && sceneBackendCapsAllowsKind(backendCaps, "webgpu"),
      webgl: allowWebGL,
      webgl2: allowWebGL,
      canvas2d: !requireWebGL && sceneBackendCapsAllowsKind(backendCaps, "canvas2d"),
      preferWebGPU: preferWebGPU && sceneBackendCapsAllowsKind(backendCaps, "webgpu"),
      forceWebGL: webglPreference === "force" && sceneBackendCapsAllowsKind(backendCaps, "webgl"),
    };
    const candidates = sceneBackendRegistry.candidates(request);
    for (const entry of candidates) {
      if (!entry || typeof entry.create !== "function") {
        continue;
      }
      try {
        if (typeof window !== "undefined") {
          window.__gosx_scene3d_backend_attempts = window.__gosx_scene3d_backend_attempts || [];
          window.__gosx_scene3d_backend_attempts.push({
            kind: String(entry.kind || ""),
            webgpuAvailable: webgpuAvail,
            preferWebGPU: preferWebGPU,
            webglPreference: webglPreference,
            webgpuFactoryErrorBefore: String(window.__gosx_scene3d_webgpu_factory_error || ""),
          });
        }
      } catch (_err) {}
      const renderer = entry.create(canvas, props, capability);
      try {
        if (typeof window !== "undefined" && Array.isArray(window.__gosx_scene3d_backend_attempts)) {
          const last = window.__gosx_scene3d_backend_attempts[window.__gosx_scene3d_backend_attempts.length - 1];
          if (last) {
            last.result = renderer ? String(renderer.kind || renderer.type || "renderer") : "";
            last.webgpuFactoryErrorAfter = String(window.__gosx_scene3d_webgpu_factory_error || "");
          }
        }
      } catch (_err) {}
      if (renderer) {
        const isCanvas = entry.kind === "canvas2d" || renderer.kind === "canvas";
        let fallbackReason = isCanvas
          ? sceneRendererFallbackReason(props, capability, "canvas")
          : (verdictFallback || (webgpuFeatureGap && renderer.kind === "webgl" ? "webgpu-feature-gap" : ""));
        return {
          renderer,
          fallbackReason,
          degraded: verdict && renderer.kind === "webgpu" ? (verdict.degraded || []) : [],
        };
      }
    }
    return null;
  }

  const sceneModelAssetCache = new Map();
  const sceneModelAssetReady = new Set();
  let sceneModelTextureVariantScopeSequence = 0;

  function normalizeSceneModelTextureVariantContext(value, fallbackBackend) {
    const source = value && typeof value === "object" ? value : {};
    const backend = String(source.backend || fallbackBackend || "").trim().toLowerCase();
    const seen = {};
    const tokens = [];
    const input = Array.isArray(source.tokens) ? source.tokens : [];
    for (let index = 0; index < input.length; index += 1) {
      const token = String(input[index] || "").trim().toLowerCase();
      if (!token || seen[token]) continue;
      seen[token] = true;
      tokens.push(token);
    }
    tokens.sort();
    return {
      backend: backend === "canvas" ? "canvas2d" : backend,
      uploadReady: source.uploadReady === true,
      tokens,
    };
  }

  function sceneModelTextureVariantFingerprint(context) {
    const normalized = normalizeSceneModelTextureVariantContext(context, "");
    return [
      normalized.backend,
      normalized.uploadReady ? "ready" : "authored",
      normalized.tokens.join(","),
    ].join("|");
  }

  function sceneModelTextureVariantContextForRenderer(renderer) {
    const kind = renderer && renderer.kind ? renderer.kind : "";
    return normalizeSceneModelTextureVariantContext(
      renderer && renderer.textureVariantContext,
      kind === "canvas" ? "canvas2d" : kind
    );
  }

  function createSceneModelTextureVariantScope(context) {
    const scope = {
      key: "scene-model-variant-" + String(++sceneModelTextureVariantScopeSequence),
      context: null,
      fingerprint: "",
      settled: false,
      ready: null,
      _resolve: null,
    };
    scope.ready = new Promise(function(resolve) {
      scope._resolve = resolve;
    });
    if (arguments.length > 0) {
      settleSceneModelTextureVariantScope(scope, context);
    }
    return scope;
  }

  function settleSceneModelTextureVariantScope(scope, context) {
    if (!scope || scope.settled) {
      return scope && scope.context;
    }
    const normalized = normalizeSceneModelTextureVariantContext(context, "");
    scope.context = normalized;
    scope.fingerprint = sceneModelTextureVariantFingerprint(normalized);
    scope.settled = true;
    const resolve = scope._resolve;
    scope._resolve = null;
    if (typeof resolve === "function") {
      resolve(normalized);
    }
    return normalized;
  }

  function replaceSceneModelTextureVariantScope(state, renderer) {
    if (!state) {
      return { changed: false, scope: null };
    }
    const context = sceneModelTextureVariantContextForRenderer(renderer);
    const fingerprint = sceneModelTextureVariantFingerprint(context);
    const current = state._modelTextureVariantScope;
    if (current && current.settled && current.fingerprint === fingerprint) {
      return { changed: false, scope: current };
    }
    const scope = createSceneModelTextureVariantScope(context);
    state._modelTextureVariantScope = scope;
    return { changed: true, scope };
  }

  function publishSceneModelTextureVariantContext(mount, scope) {
    if (!mount) return;
    const context = scope && scope.context
      ? normalizeSceneModelTextureVariantContext(scope.context, "")
      : normalizeSceneModelTextureVariantContext(null, "");
    const snapshot = {
      scope: scope && scope.key ? String(scope.key) : "",
      fingerprint: scope && scope.fingerprint ? String(scope.fingerprint) : "",
      backend: context.backend,
      uploadReady: context.uploadReady,
      tokens: context.tokens.slice(),
    };
    mount.__gosxScene3DTextureVariantContext = snapshot;
    setAttrValue(mount, "data-gosx-scene3d-texture-variant-backend", snapshot.backend);
    setAttrValue(mount, "data-gosx-scene3d-texture-variant-upload-ready", snapshot.uploadReady ? "true" : "false");
    setAttrValue(mount, "data-gosx-scene3d-texture-variant-token-count", String(snapshot.tokens.length));
    setAttrValue(mount, "data-gosx-scene3d-texture-variant-scope", snapshot.scope);
  }

  function resolveSceneModelAssetURL(baseSrc, value) {
    const raw = typeof value === "string" ? value.trim() : "";
    if (!raw) {
      return "";
    }
    try {
      const baseURL = new URL(baseSrc || "", window.location.href).toString();
      return new URL(raw, baseURL).toString();
    } catch (_error) {
      return raw;
    }
  }

  function resolveSceneModelObjectURLs(baseSrc, rawObject) {
    if (!rawObject || typeof rawObject !== "object") {
      return rawObject;
    }
    const resolved = Object.assign({}, rawObject);
    if (typeof resolved.texture === "string" && resolved.texture.trim()) {
      resolved.texture = resolveSceneModelAssetURL(baseSrc, resolved.texture);
    }
    if (resolved.material && typeof resolved.material === "object") {
      const material = Object.assign({}, resolved.material);
      if (typeof material.texture === "string" && material.texture.trim()) {
        material.texture = resolveSceneModelAssetURL(baseSrc, material.texture);
      }
      resolved.material = material;
    }
    return resolved;
  }

  function sceneModelTransformPoint(point, model) {
    const local = point && typeof point === "object" ? point : { x: 0, y: 0, z: 0 };
    const scaleX = sceneNumber(model && model.scaleX, 1);
    const scaleY = sceneNumber(model && model.scaleY, 1);
    const scaleZ = sceneNumber(model && model.scaleZ, 1);
    const rotated = sceneRotatePoint(
      {
        x: local.x * scaleX,
        y: local.y * scaleY,
        z: local.z * scaleZ,
      },
      sceneNumber(model && model.rotationX, 0),
      sceneNumber(model && model.rotationY, 0),
      sceneNumber(model && model.rotationZ, 0),
    );
    return {
      x: rotated.x + sceneNumber(model && model.x, 0),
      y: rotated.y + sceneNumber(model && model.y, 0),
      z: rotated.z + sceneNumber(model && model.z, 0),
    };
  }

  function sceneModelTransformVector(point, model) {
    const local = point && typeof point === "object" ? point : { x: 0, y: 0, z: 0 };
    return sceneRotatePoint(
      {
        x: local.x * sceneNumber(model && model.scaleX, 1),
        y: local.y * sceneNumber(model && model.scaleY, 1),
        z: local.z * sceneNumber(model && model.scaleZ, 1),
      },
      sceneNumber(model && model.rotationX, 0),
      sceneNumber(model && model.rotationY, 0),
      sceneNumber(model && model.rotationZ, 0),
    );
  }

  function sceneModelMaxScale(model) {
    return Math.max(
      Math.abs(sceneNumber(model && model.scaleX, 1)),
      Math.abs(sceneNumber(model && model.scaleY, 1)),
      Math.abs(sceneNumber(model && model.scaleZ, 1)),
    );
  }

  function sceneModelZeroOpacityHidesObject(model, object) {
    const opacity = object && Object.prototype.hasOwnProperty.call(object, "opacity")
      ? sceneNumber(object.opacity, sceneNumber(model && model.opacity, 1))
      : sceneNumber(model && model.opacity, 1);
    if (opacity > 0.0001) {
      return false;
    }
    const material = sceneObjectMaterialProfile(object);
    return !sceneMaterialUsesAuthoredMeshShader(material);
  }

  function sceneModelEffectivelyHidden(model, object) {
    return Boolean(model && model.visible === false)
      || sceneModelMaxScale(model) <= 0.0015
      || sceneModelZeroOpacityHidesObject(model, object);
  }

  function sceneApplyModelObjectHiddenState(object, model) {
    if (!object) {
      return;
    }
    object._modelHidden = sceneModelEffectivelyHidden(model, object);
  }

  function sceneModelRotateDirection(point, model) {
    return sceneRotatePoint(
      point && typeof point === "object" ? point : { x: 0, y: 0, z: 0 },
      sceneNumber(model && model.rotationX, 0),
      sceneNumber(model && model.rotationY, 0),
      sceneNumber(model && model.rotationZ, 0),
    );
  }

  function sceneModelTransformMatrix(model) {
    const rx = sceneNumber(model && model.rotationX, 0);
    const ry = sceneNumber(model && model.rotationY, 0);
    const rz = sceneNumber(model && model.rotationZ, 0);
    const basisX = sceneRotatePoint({ x: sceneNumber(model && model.scaleX, 1), y: 0, z: 0 }, rx, ry, rz);
    const basisY = sceneRotatePoint({ x: 0, y: sceneNumber(model && model.scaleY, 1), z: 0 }, rx, ry, rz);
    const basisZ = sceneRotatePoint({ x: 0, y: 0, z: sceneNumber(model && model.scaleZ, 1) }, rx, ry, rz);
    return new Float32Array([
      basisX.x, basisX.y, basisX.z, 0,
      basisY.x, basisY.y, basisY.z, 0,
      basisZ.x, basisZ.y, basisZ.z, 0,
      sceneNumber(model && model.x, 0), sceneNumber(model && model.y, 0), sceneNumber(model && model.z, 0), 1,
    ]);
  }

  function sceneModelFitMode(value) {
    switch (String(value || "").trim().toLowerCase()) {
      case "bounds":
      case "bound":
      case "contain":
      case "fit":
      case "max-dimension":
        return "contain";
      default:
        return "";
    }
  }

  function sceneModelFitAlign(value) {
    switch (String(value || "").trim().toLowerCase()) {
      case "none":
        return "none";
      case "bottom":
      case "center-bottom":
        return "center-bottom";
      case "center-min-y":
      case "center-min":
      case "water-center-min-y":
        return "center-min-y";
      case "center":
      default:
        return "center";
    }
  }

  function sceneModelWithAssetFit(model, asset) {
    const mode = sceneModelFitMode(model && model.fit);
    const target = Math.max(0, sceneNumber(model && model.bounds, 0));
    const bounds = sceneModelNormalizeAssetBounds(asset && asset.bounds);
    if (!mode || target <= 0 || !bounds || !(bounds.maxDimension > 0)) {
      return model;
    }
    const fitScale = target / bounds.maxDimension;
    if (!Number.isFinite(fitScale) || fitScale <= 0) {
      return model;
    }
    const align = sceneModelFitAlign(model && model.fitAlign);
    const offset = { x: 0, y: 0, z: 0 };
    if (align !== "none") {
      offset.x = -bounds.centerX * fitScale;
      offset.z = -bounds.centerZ * fitScale;
      if (align === "center-bottom") {
        offset.y = -bounds.minY * fitScale;
      } else {
        offset.y = -bounds.centerY * fitScale;
        if (align === "center-min-y") {
          offset.y -= bounds.minY * fitScale;
        }
      }
    }
    const translated = sceneModelTransformVector(offset, model);
    const fitted = Object.assign({}, model, {
      x: sceneNumber(model && model.x, 0) + translated.x,
      y: sceneNumber(model && model.y, 0) + translated.y,
      z: sceneNumber(model && model.z, 0) + translated.z,
      scaleX: sceneNumber(model && model.scaleX, 1) * fitScale,
      scaleY: sceneNumber(model && model.scaleY, 1) * fitScale,
      scaleZ: sceneNumber(model && model.scaleZ, 1) * fitScale,
    });
    fitted._fitScale = fitScale;
    fitted._fitBounds = bounds;
    fitted._fitAlign = align;
    return fitted;
  }

  function sceneModelIdentityBindMatrices(jointCount) {
    const matrices = new Float32Array(Math.max(0, jointCount) * 16);
    for (let index = 0; index < jointCount; index += 1) {
      matrices[index * 16] = 1;
      matrices[index * 16 + 5] = 1;
      matrices[index * 16 + 10] = 1;
      matrices[index * 16 + 15] = 1;
    }
    return matrices;
  }

  function sceneCloneModelSkin(skin) {
    if (!skin || typeof skin !== "object") {
      return null;
    }
    const joints = Array.isArray(skin.joints) ? skin.joints.slice() : [];
    if (!joints.length || joints.length > 64) {
      return null;
    }
    let inverseBindMatrices = skin.inverseBindMatrices instanceof Float32Array
      ? new Float32Array(skin.inverseBindMatrices)
      : sceneTypedFloatArray(skin.inverseBindMatrices);
    if (inverseBindMatrices.length < joints.length * 16) {
      inverseBindMatrices = sceneModelIdentityBindMatrices(joints.length);
    } else if (inverseBindMatrices.length !== joints.length * 16) {
      inverseBindMatrices = inverseBindMatrices.slice(0, joints.length * 16);
    }
    return {
      index: typeof skin.index === "number" ? skin.index : null,
      name: typeof skin.name === "string" ? skin.name : "",
      joints,
      inverseBindMatrices,
      skeleton: skin.skeleton != null ? skin.skeleton : null,
    };
  }

  function sceneCloneModelSkins(skins) {
    return Array.isArray(skins) ? skins.map(sceneCloneModelSkin) : [];
  }

  function sceneCloneModelAnimations(animations) {
    if (!Array.isArray(animations)) {
      return [];
    }
    return animations.map(function(clip, index) {
      const source = clip && typeof clip === "object" ? clip : {};
      const channels = Array.isArray(source.channels) ? source.channels.map(function(channel) {
        const ch = channel && typeof channel === "object" ? channel : {};
        return {
          targetID: ch.targetID != null ? ch.targetID : ch.targetNode,
          targetNode: ch.targetNode != null ? ch.targetNode : ch.targetID,
          property: typeof ch.property === "string" ? ch.property : "translation",
          interpolation: typeof ch.interpolation === "string" && ch.interpolation ? ch.interpolation : "LINEAR",
          times: ch.times instanceof Float32Array ? new Float32Array(ch.times) : sceneTypedFloatArray(ch.times),
          values: ch.values instanceof Float32Array ? new Float32Array(ch.values) : sceneTypedFloatArray(ch.values),
        };
      }) : [];
      return {
        name: typeof source.name === "string" && source.name ? source.name : ("clip-" + index),
        duration: sceneNumber(source.duration, 0),
        channels,
      };
    });
  }

  function sceneModelMaterialOverrideSource(model) {
    if (!model || typeof model !== "object") {
      return null;
    }
    if (model.materialOverride && typeof model.materialOverride === "object") {
      return model.materialOverride;
    }
    const keys = ["material", "materialKind", "color", "texture", "opacity", "emissive", "blendMode", "renderPass", "wireframe", "roughness", "metalness", "clearcoat", "sheen", "transmission", "iridescence", "anisotropy", "customVertex", "customFragment", "customVertexWGSL", "customFragmentWGSL", "customUniforms", "shaderBackend", "shaderLayout", "shaderSource", "shaderSourceFiles"];
    for (let index = 0; index < keys.length; index += 1) {
      if (Object.prototype.hasOwnProperty.call(model, keys[index])) {
        return model;
      }
    }
    return null;
  }

  function sceneAssignMaterialOverride(next, material, sourceKey, targetKey, override) {
    if (!override || !Object.prototype.hasOwnProperty.call(override, sourceKey)) {
      return;
    }
    const key = targetKey || sourceKey;
    next[key] = override[sourceKey];
    if (material) {
      material[key] = override[sourceKey];
    }
  }

  function sceneApplyMaterialOverride(raw, model) {
    const override = sceneModelMaterialOverrideSource(model);
    if (!override) {
      return raw && typeof raw === "object" ? Object.assign({}, raw) : {};
    }
    const next = raw && typeof raw === "object" ? Object.assign({}, raw) : {};
    const material = next.material && typeof next.material === "object"
      ? Object.assign({}, next.material)
      : null;
    const namedMaterialOverride = typeof override.material === "string" && override.material.trim();
    if (typeof override.materialKind === "string" && override.materialKind) {
      next.materialKind = override.materialKind;
      if (typeof next.material === "string") {
        next.material = override.materialKind;
      }
      if (material) {
        material.kind = override.materialKind;
      }
    }
    if (namedMaterialOverride) {
      next.material = override.material.trim();
    }
    sceneAssignMaterialOverride(next, material, "color", "color", override);
    sceneAssignMaterialOverride(next, material, "texture", "texture", override);
    sceneAssignMaterialOverride(next, material, "opacity", "opacity", override);
    sceneAssignMaterialOverride(next, material, "emissive", "emissive", override);
    sceneAssignMaterialOverride(next, material, "blendMode", "blendMode", override);
    sceneAssignMaterialOverride(next, material, "renderPass", "renderPass", override);
    sceneAssignMaterialOverride(next, material, "wireframe", "wireframe", override);
    sceneAssignMaterialOverride(next, material, "roughness", "roughness", override);
    sceneAssignMaterialOverride(next, material, "metalness", "metalness", override);
    sceneAssignMaterialOverride(next, material, "clearcoat", "clearcoat", override);
    sceneAssignMaterialOverride(next, material, "sheen", "sheen", override);
    sceneAssignMaterialOverride(next, material, "transmission", "transmission", override);
    sceneAssignMaterialOverride(next, material, "iridescence", "iridescence", override);
    sceneAssignMaterialOverride(next, material, "anisotropy", "anisotropy", override);
    sceneAssignMaterialOverride(next, material, "customVertex", "customVertex", override);
    sceneAssignMaterialOverride(next, material, "customFragment", "customFragment", override);
    sceneAssignMaterialOverride(next, material, "customVertexWGSL", "customVertexWGSL", override);
    sceneAssignMaterialOverride(next, material, "customFragmentWGSL", "customFragmentWGSL", override);
    sceneAssignMaterialOverride(next, material, "customUniforms", "customUniforms", override);
    sceneAssignMaterialOverride(next, material, "shaderBackend", "shaderBackend", override);
    sceneAssignMaterialOverride(next, material, "shaderLayout", "shaderLayout", override);
    sceneAssignMaterialOverride(next, material, "shaderSource", "shaderSource", override);
    sceneAssignMaterialOverride(next, material, "shaderSourceFiles", "shaderSourceFiles", override);
    if (material && !namedMaterialOverride) {
      next.material = material;
    }
    return next;
  }

  function sceneApplyModelLOD(instanced, model) {
    if (!instanced || !model || !model.lodGroup) {
      return;
    }
    instanced.lodGroup = model.lodGroup;
    instanced.lodLevel = model.lodLevel;
    instanced.lodMinDistance = model.lodMinDistance;
    instanced.lodMaxDistance = model.lodMaxDistance;
  }

  function sceneApplyModelRenderFlags(instanced, model) {
    if (!instanced || !model) {
      return;
    }
    if (typeof model.castShadow === "boolean") {
      instanced.castShadow = model.castShadow;
    }
    if (typeof model.receiveShadow === "boolean") {
      instanced.receiveShadow = model.receiveShadow;
    }
  }

  function sceneApplyModelMaterialName(instanced, model) {
    if (!instanced || !model || typeof model.material !== "string" || !model.material.trim()) {
      return;
    }
    instanced.material = model.material.trim();
  }

  function sceneModelPrimitiveObject(object, model, prefix) {
    const instanced = Object.assign({}, object, {
      id: prefix + "/" + (object.id || "object"),
      x: 0,
      y: 0,
      z: 0,
      rotationX: sceneNumber(object.rotationX, 0) + sceneNumber(model && model.rotationX, 0),
      rotationY: sceneNumber(object.rotationY, 0) + sceneNumber(model && model.rotationY, 0),
      rotationZ: sceneNumber(object.rotationZ, 0) + sceneNumber(model && model.rotationZ, 0),
    });
    const positioned = sceneModelTransformPoint({ x: object.x, y: object.y, z: object.z }, model);
    instanced.x = positioned.x;
    instanced.y = positioned.y;
    instanced.z = positioned.z;
    const scaleX = Math.abs(sceneNumber(model && model.scaleX, 1));
    const scaleY = Math.abs(sceneNumber(model && model.scaleY, 1));
    const scaleZ = Math.abs(sceneNumber(model && model.scaleZ, 1));
    switch (object.kind) {
      case "cube":
        if (Math.abs(scaleX - scaleY) > 0.0001 || Math.abs(scaleX - scaleZ) > 0.0001) {
          instanced.kind = "box";
          instanced.width = sceneNumber(object.size, 1.2) * scaleX;
          instanced.height = sceneNumber(object.size, 1.2) * scaleY;
          instanced.depth = sceneNumber(object.size, 1.2) * scaleZ;
        } else {
          instanced.size = sceneNumber(object.size, 1.2) * scaleX;
        }
        break;
      case "sphere":
        instanced.radius = sceneNumber(object.radius, sceneNumber(object.size, 1.2) / 2) * Math.max(scaleX, scaleY, scaleZ);
        break;
      default:
        instanced.width = sceneNumber(object.width, sceneNumber(object.size, 1.2)) * scaleX;
        instanced.height = sceneNumber(object.height, sceneNumber(object.size, 1.2)) * scaleY;
        instanced.depth = sceneNumber(object.depth, sceneNumber(object.size, 1.2)) * scaleZ;
        break;
    }
    if (model && model.static !== null) {
      instanced.static = Boolean(model.static);
    }
    if (model && typeof model.pickable === "boolean") {
      instanced.pickable = model.pickable;
    }
    sceneApplyModelObjectHiddenState(instanced, model);
    sceneApplyModelMaterialName(instanced, model);
    sceneApplyModelRenderFlags(instanced, model);
    sceneApplyModelLOD(instanced, model);
    return normalizeSceneObject(instanced, prefix);
  }

  function sceneModelLineObject(object, model, prefix) {
    const scaleX = sceneNumber(model && model.scaleX, 1);
    const scaleY = sceneNumber(model && model.scaleY, 1);
    const scaleZ = sceneNumber(model && model.scaleZ, 1);
    const scaled = sceneScaleModelLinePoints(object.points, scaleX, scaleY, scaleZ);
    const positioned = sceneModelTransformPoint({ x: object.x, y: object.y, z: object.z }, model);
    const instanced = Object.assign({}, object, {
      id: prefix + "/" + (object.id || "object"),
      points: scaled,
      lineSegments: sceneCloneModelLineSegments(object.lineSegments),
      x: positioned.x,
      y: positioned.y,
      z: positioned.z,
      rotationX: sceneNumber(object.rotationX, 0) + sceneNumber(model && model.rotationX, 0),
      rotationY: sceneNumber(object.rotationY, 0) + sceneNumber(model && model.rotationY, 0),
      rotationZ: sceneNumber(object.rotationZ, 0) + sceneNumber(model && model.rotationZ, 0),
    });
    if (model && model.static !== null) {
      instanced.static = Boolean(model.static);
    }
    if (model && typeof model.pickable === "boolean") {
      instanced.pickable = model.pickable;
    }
    sceneApplyModelObjectHiddenState(instanced, model);
    sceneApplyModelMaterialName(instanced, model);
    sceneApplyModelRenderFlags(instanced, model);
    sceneApplyModelLOD(instanced, model);
    return normalizeSceneObject(instanced, prefix);
  }

  function sceneScaleModelLinePoints(points, scaleX, scaleY, scaleZ) {
    return Array.isArray(points) ? points.map(function(point) {
      return {
        x: sceneNumber(point && point.x, 0) * scaleX,
        y: sceneNumber(point && point.y, 0) * scaleY,
        z: sceneNumber(point && point.z, 0) * scaleZ,
      };
    }) : [];
  }

  function sceneCloneModelLineSegments(segments) {
    return Array.isArray(segments) ? segments.map(function(pair) {
      return Array.isArray(pair) ? pair.slice(0, 2) : pair;
    }) : [];
  }

  function sceneScaleModelPointPositions(positions, scaleX, scaleY, scaleZ) {
    const source = positions instanceof Float32Array ? positions : sceneTypedFloatArray(positions);
    if (!source.length) {
      return source;
    }
    if (Math.abs(scaleX - 1) < 0.000001 && Math.abs(scaleY - 1) < 0.000001 && Math.abs(scaleZ - 1) < 0.000001) {
      return source;
    }
    const scaled = new Float32Array(source.length);
    for (let i = 0; i + 2 < source.length; i += 3) {
      scaled[i] = source[i] * scaleX;
      scaled[i + 1] = source[i + 1] * scaleY;
      scaled[i + 2] = source[i + 2] * scaleZ;
    }
    return scaled;
  }

  function sceneApplyModelPointOverride(point, model) {
    const override = Object.assign({}, point);
    if (!model || typeof model !== "object") {
      return override;
    }
    if (typeof model.material === "string" && model.material.trim()) {
      override.material = model.material.trim();
    }
    if (typeof model.color === "string" && model.color) {
      override.color = model.color;
    }
    if (typeof model.style === "string" && model.style) {
      override.style = model.style;
    }
    if (model.size != null) {
      override.size = model.size;
    }
    if (model.opacity != null) {
      override.opacity = model.opacity;
    }
    if (typeof model.blendMode === "string" && model.blendMode) {
      override.blendMode = model.blendMode;
    }
    if (model.depthWrite != null) {
      override.depthWrite = model.depthWrite;
    }
    if (model.attenuation != null) {
      override.attenuation = model.attenuation;
    }
    return override;
  }

  function sceneInstantiateModelPointsEntry(rawPoint, model, prefix, index) {
    const source = sceneApplyModelPointOverride(rawPoint, model);
    const normalized = normalizeScenePointsEntry(source, index, null);
    const scaleX = sceneNumber(model && model.scaleX, 1);
    const scaleY = sceneNumber(model && model.scaleY, 1);
    const scaleZ = sceneNumber(model && model.scaleZ, 1);
    const positions = sceneScaleModelPointPositions(normalized._cachedPos || normalized.positions, scaleX, scaleY, scaleZ);
    const positioned = sceneModelTransformPoint({ x: normalized.x, y: normalized.y, z: normalized.z }, model);
    const instanced = Object.assign({}, normalized, {
      id: prefix + "/" + normalized.id,
      positions,
      x: positioned.x,
      y: positioned.y,
      z: positioned.z,
      rotationX: sceneNumber(normalized.rotationX, 0) + sceneNumber(model && model.rotationX, 0),
      rotationY: sceneNumber(normalized.rotationY, 0) + sceneNumber(model && model.rotationY, 0),
      rotationZ: sceneNumber(normalized.rotationZ, 0) + sceneNumber(model && model.rotationZ, 0),
    });
    if (positions instanceof Float32Array) {
      instanced._cachedPos = positions;
    }
    if (normalized._cachedSizes) {
      instanced._cachedSizes = normalized._cachedSizes;
    }
    if (normalized._cachedColors) {
      instanced._cachedColors = normalized._cachedColors;
    }
    return normalizeScenePointsEntry(instanced, instanced.id, normalized);
  }

  function sceneInstantiateModelObject(rawObject, model, prefix, index, skinInstances) {
    const source = sceneApplyMaterialOverride(rawObject, model);
    if (skinInstances && source && source.skinIndex != null && skinInstances[source.skinIndex]) {
      source.skin = skinInstances[source.skinIndex];
    }
    const normalized = normalizeSceneObject(source, index);
    if (normalized.vertices && normalized.vertices.positions && normalized.vertices.count > 0) {
      return sceneModelMeshObject(normalized, model, prefix);
    }
    if (normalized.kind === "lines") {
      return sceneModelLineObject(normalized, model, prefix);
    }
    return sceneModelPrimitiveObject(normalized, model, prefix);
  }

  function sceneModelTransformMeshFloats(values, tupleSize, mapper) {
    const source = values instanceof Float32Array ? values : sceneTypedFloatArray(values);
    const typed = new Float32Array(source.length);
    const safeTupleSize = Math.max(1, Math.floor(sceneNumber(tupleSize, 1)));
    for (let index = 0; index + safeTupleSize - 1 < source.length; index += safeTupleSize) {
      const mapped = mapper(
        sceneNumber(source[index], 0),
        sceneNumber(source[index + 1], 0),
        sceneNumber(source[index + 2], 0),
        safeTupleSize > 3 ? sceneNumber(source[index + 3], 1) : undefined
      );
      typed[index] = sceneNumber(mapped && mapped.x, 0);
      typed[index + 1] = sceneNumber(mapped && mapped.y, 0);
      typed[index + 2] = sceneNumber(mapped && mapped.z, 0);
      if (safeTupleSize > 3) {
        typed[index + 3] = sceneNumber(mapped && mapped.w, 1);
      }
    }
    return typed;
  }

  function sceneModelMeshObject(object, model, prefix) {
    const vertices = object && object.vertices && typeof object.vertices === "object" ? object.vertices : null;
    if (!vertices || !vertices.positions || !vertices.count) {
      return null;
    }
    const instanced = Object.assign({}, object, {
      id: prefix + "/" + (object.id || "object"),
      x: 0,
      y: 0,
      z: 0,
      rotationX: 0,
      rotationY: 0,
      rotationZ: 0,
      spinX: 0,
      spinY: 0,
      spinZ: 0,
      shiftX: 0,
      shiftY: 0,
      shiftZ: 0,
      driftSpeed: 0,
      driftPhase: 0,
    });
    const hasSkin = instanced.skin && typeof instanced.skin === "object";
    if (hasSkin) {
      instanced.vertices = {
        count: Math.max(0, Math.floor(sceneNumber(vertices.count, 0))),
        positions: vertices.positions instanceof Float32Array ? new Float32Array(vertices.positions) : sceneTypedFloatArray(vertices.positions),
        normals: vertices.normals instanceof Float32Array ? new Float32Array(vertices.normals) : sceneTypedFloatArray(vertices.normals),
        uvs: vertices.uvs instanceof Float32Array ? new Float32Array(vertices.uvs) : sceneTypedFloatArray(vertices.uvs),
        tangents: vertices.tangents instanceof Float32Array ? new Float32Array(vertices.tangents) : sceneTypedFloatArray(vertices.tangents),
        joints: vertices.joints instanceof Float32Array ? new Float32Array(vertices.joints) : sceneTypedFloatArray(vertices.joints),
        weights: vertices.weights instanceof Float32Array ? new Float32Array(vertices.weights) : sceneTypedFloatArray(vertices.weights),
      };
    } else {
      instanced.vertices = {
        count: Math.max(0, Math.floor(sceneNumber(vertices.count, 0))),
        positions: sceneModelTransformMeshFloats(vertices.positions, 3, function(x, y, z) {
          return sceneModelTransformPoint({ x: x, y: y, z: z }, model);
        }),
        normals: sceneModelTransformMeshFloats(vertices.normals, 3, function(x, y, z) {
          return sceneNormalizeDirection(sceneModelTransformVector({ x: x, y: y, z: z }, model));
        }),
        uvs: vertices.uvs instanceof Float32Array ? new Float32Array(vertices.uvs) : sceneTypedFloatArray(vertices.uvs),
        tangents: sceneModelTransformMeshFloats(vertices.tangents, 4, function(x, y, z, w) {
          const rotated = sceneNormalizeDirection(sceneModelTransformVector({ x: x, y: y, z: z }, model));
          return { x: rotated.x, y: rotated.y, z: rotated.z, w: sceneNumber(w, 1) };
        }),
        joints: vertices.joints instanceof Float32Array ? new Float32Array(vertices.joints) : sceneTypedFloatArray(vertices.joints),
        weights: vertices.weights instanceof Float32Array ? new Float32Array(vertices.weights) : sceneTypedFloatArray(vertices.weights),
      };
    }
    if (model && model.static !== null) {
      instanced.static = Boolean(model.static);
    }
    if (model && Array.isArray(model._live) && model._live.length > 0) {
      instanced.static = false;
    }
    if (hasSkin && model && model.animation) {
      instanced.static = false;
    }
    if (model && typeof model.pickable === "boolean") {
      instanced.pickable = model.pickable;
    }
    sceneApplyModelObjectHiddenState(instanced, model);
    sceneApplyModelMaterialName(instanced, model);
    sceneApplyModelRenderFlags(instanced, model);
    sceneApplyModelLOD(instanced, model);
    const normalized = normalizeSceneObject(instanced, prefix);
    sceneApplyModelMaterialName(normalized, model);
    if (!hasSkin && normalized && normalized.vertices) {
      normalized._modelLocalVertices = {
        positions: vertices.positions instanceof Float32Array ? new Float32Array(vertices.positions) : sceneTypedFloatArray(vertices.positions),
        normals: vertices.normals instanceof Float32Array ? new Float32Array(vertices.normals) : sceneTypedFloatArray(vertices.normals),
        uvs: vertices.uvs instanceof Float32Array ? new Float32Array(vertices.uvs) : sceneTypedFloatArray(vertices.uvs),
        tangents: vertices.tangents instanceof Float32Array ? new Float32Array(vertices.tangents) : sceneTypedFloatArray(vertices.tangents),
        count: Math.max(0, Math.floor(sceneNumber(vertices.count, 0))),
      };
    }
    return normalized;
  }

  function sceneSkinnedModelLocalBounds(vertices) {
    if (!vertices || !vertices.positions || !vertices.count) {
      return null;
    }
    const cached = vertices._skinnedLocalBounds;
    if (cached) {
      return cached;
    }
    const positions = vertices.positions instanceof Float32Array ? vertices.positions : sceneTypedFloatArray(vertices.positions);
    let minX = Infinity;
    let minY = Infinity;
    let minZ = Infinity;
    let maxX = -Infinity;
    let maxY = -Infinity;
    let maxZ = -Infinity;
    const limit = Math.min(positions.length, Math.max(0, Math.floor(sceneNumber(vertices.count, 0))) * 3);
    for (let index = 0; index + 2 < limit; index += 3) {
      const x = positions[index];
      const y = positions[index + 1];
      const z = positions[index + 2];
      if (x < minX) minX = x;
      if (y < minY) minY = y;
      if (z < minZ) minZ = z;
      if (x > maxX) maxX = x;
      if (y > maxY) maxY = y;
      if (z > maxZ) maxZ = z;
    }
    const bounds = Number.isFinite(minX)
      ? { minX, minY, minZ, maxX, maxY, maxZ }
      : { minX: -1, minY: -1, minZ: -1, maxX: 1, maxY: 1, maxZ: 1 };
    vertices._skinnedLocalBounds = bounds;
    return bounds;
  }

  function sceneInstantiateModelLabel(rawLabel, model, prefix, index) {
    const normalized = normalizeSceneLabel(rawLabel, index);
    const position = sceneModelTransformPoint({ x: normalized.x, y: normalized.y, z: normalized.z }, model);
    return Object.assign({}, normalized, {
      id: prefix + "/" + normalized.id,
      x: position.x,
      y: position.y,
      z: position.z,
    });
  }

  function sceneInstantiateModelLight(rawLight, model, prefix, index) {
    const normalized = normalizeSceneLight(rawLight, index, null);
    if (!normalized) {
      return null;
    }
    const next = Object.assign({}, normalized, {
      id: prefix + "/" + normalized.id,
    });
    if (next.kind === "directional") {
      const rotated = sceneModelRotateDirection({
        x: next.directionX,
        y: next.directionY,
        z: next.directionZ,
      }, model);
      next.directionX = rotated.x;
      next.directionY = rotated.y;
      next.directionZ = rotated.z;
      // Re-stamp the light sub-hash since directionX/Y/Z were just mutated
      // in place after normalizeSceneLight's original stamp. Without this
      // scenePBRLightsHash would read a stale _lightHash and miss content
      // changes for model-rotated directional lights.
      if (typeof hashLightContent === "function") {
        next._lightHash = hashLightContent(next);
      }
      return next;
    }
    const position = sceneModelTransformPoint({ x: next.x, y: next.y, z: next.z }, model);
    next.x = position.x;
    next.y = position.y;
    next.z = position.z;
    if (next.kind === "point") {
      next.range = sceneNumber(next.range, 0) * Math.max(
        Math.abs(sceneNumber(model && model.scaleX, 1)),
        Math.abs(sceneNumber(model && model.scaleY, 1)),
        Math.abs(sceneNumber(model && model.scaleZ, 1)),
      );
    }
    // Re-stamp after the above in-place x/y/z (and range for points)
    // writes — see the directional branch comment above.
    if (typeof hashLightContent === "function") {
      next._lightHash = hashLightContent(next);
    }
    return next;
  }

  function sceneInstantiateModelSprite(rawSprite, model, prefix, index) {
    const normalized = normalizeSceneSprite(rawSprite, index);
    if (!normalized || !normalized.src) {
      return null;
    }
    const position = sceneModelTransformPoint({ x: normalized.x, y: normalized.y, z: normalized.z }, model);
    const shift = sceneModelTransformVector({ x: normalized.shiftX, y: normalized.shiftY, z: normalized.shiftZ }, model);
    const modelScale = sceneModelMaxScale(model);
    return Object.assign({}, normalized, {
      id: prefix + "/" + normalized.id,
      x: position.x,
      y: position.y,
      z: position.z,
      shiftX: shift.x,
      shiftY: shift.y,
      shiftZ: shift.z,
      width: normalized.width * modelScale,
      height: normalized.height * modelScale,
    });
  }

  function sceneInstantiateModelHTML(rawHTML, model, prefix, index) {
    const normalized = normalizeSceneHTML(rawHTML, index);
    if (!normalized || !normalized.html.trim()) {
      return null;
    }
    const position = sceneModelTransformPoint({ x: normalized.x, y: normalized.y, z: normalized.z }, model);
    const shift = sceneModelTransformVector({ x: normalized.shiftX, y: normalized.shiftY, z: normalized.shiftZ }, model);
    const modelScale = sceneModelMaxScale(model);
    return Object.assign({}, normalized, {
      id: prefix + "/" + normalized.id,
      x: position.x,
      y: position.y,
      z: position.z,
      shiftX: shift.x,
      shiftY: shift.y,
      shiftZ: shift.z,
      width: normalized.width * modelScale,
      height: normalized.height * modelScale,
    });
  }

  function sceneModelBoundsNumber(value) {
    const number = typeof value === "number" ? value : Number(value);
    return Number.isFinite(number) ? number : null;
  }

  function sceneModelExpandBounds(bounds, x, y, z) {
    const px = sceneModelBoundsNumber(x);
    const py = sceneModelBoundsNumber(y);
    const pz = sceneModelBoundsNumber(z);
    if (px == null || py == null || pz == null) {
      return bounds;
    }
    const next = bounds || {
      minX: Infinity,
      minY: Infinity,
      minZ: Infinity,
      maxX: -Infinity,
      maxY: -Infinity,
      maxZ: -Infinity,
    };
    if (px < next.minX) next.minX = px;
    if (py < next.minY) next.minY = py;
    if (pz < next.minZ) next.minZ = pz;
    if (px > next.maxX) next.maxX = px;
    if (py > next.maxY) next.maxY = py;
    if (pz > next.maxZ) next.maxZ = pz;
    return next;
  }

  function sceneModelFinalizeBounds(bounds) {
    if (!bounds) {
      return null;
    }
    const minX = sceneModelBoundsNumber(bounds.minX);
    const minY = sceneModelBoundsNumber(bounds.minY);
    const minZ = sceneModelBoundsNumber(bounds.minZ);
    const maxX = sceneModelBoundsNumber(bounds.maxX);
    const maxY = sceneModelBoundsNumber(bounds.maxY);
    const maxZ = sceneModelBoundsNumber(bounds.maxZ);
    if (minX == null || minY == null || minZ == null || maxX == null || maxY == null || maxZ == null) {
      return null;
    }
    if (maxX < minX || maxY < minY || maxZ < minZ) {
      return null;
    }
    const width = maxX - minX;
    const height = maxY - minY;
    const depth = maxZ - minZ;
    return {
      minX,
      minY,
      minZ,
      maxX,
      maxY,
      maxZ,
      width,
      height,
      depth,
      centerX: (minX + maxX) * 0.5,
      centerY: (minY + maxY) * 0.5,
      centerZ: (minZ + maxZ) * 0.5,
      maxDimension: Math.max(width, height, depth),
    };
  }

  function sceneModelNormalizeAssetBounds(value) {
    return value && typeof value === "object" ? sceneModelFinalizeBounds(value) : null;
  }

  function sceneModelExpandPositionArray(bounds, positions, offset) {
    if (!positions || typeof positions.length !== "number") {
      return bounds;
    }
    const source = positions instanceof Float32Array ? positions : sceneTypedFloatArray(positions);
    const ox = sceneNumber(offset && offset.x, 0);
    const oy = sceneNumber(offset && offset.y, 0);
    const oz = sceneNumber(offset && offset.z, 0);
    for (let index = 0; index + 2 < source.length; index += 3) {
      bounds = sceneModelExpandBounds(bounds, source[index] + ox, source[index + 1] + oy, source[index + 2] + oz);
    }
    return bounds;
  }

  function sceneModelExpandPointList(bounds, points, offset) {
    if (!Array.isArray(points)) {
      return bounds;
    }
    const ox = sceneNumber(offset && offset.x, 0);
    const oy = sceneNumber(offset && offset.y, 0);
    const oz = sceneNumber(offset && offset.z, 0);
    for (let index = 0; index < points.length; index += 1) {
      const point = points[index] || {};
      bounds = sceneModelExpandBounds(bounds, sceneNumber(point.x, 0) + ox, sceneNumber(point.y, 0) + oy, sceneNumber(point.z, 0) + oz);
    }
    return bounds;
  }

  function sceneModelExpandPrimitiveBounds(bounds, object) {
    const kind = String(object && object.kind || "").trim().toLowerCase();
    const x = sceneNumber(object && object.x, 0);
    const y = sceneNumber(object && object.y, 0);
    const z = sceneNumber(object && object.z, 0);
    if (kind === "sphere") {
      const radius = Math.max(0, sceneNumber(object.radius, sceneNumber(object.size, 1.2) * 0.5));
      bounds = sceneModelExpandBounds(bounds, x - radius, y - radius, z - radius);
      return sceneModelExpandBounds(bounds, x + radius, y + radius, z + radius);
    }
    if (kind === "box" || kind === "cube") {
      const size = sceneNumber(object.size, 1.2);
      const halfX = Math.max(0, sceneNumber(object.width, size)) * 0.5;
      const halfY = Math.max(0, sceneNumber(object.height, size)) * 0.5;
      const halfZ = Math.max(0, sceneNumber(object.depth, size)) * 0.5;
      bounds = sceneModelExpandBounds(bounds, x - halfX, y - halfY, z - halfZ);
      return sceneModelExpandBounds(bounds, x + halfX, y + halfY, z + halfZ);
    }
    return bounds;
  }

  function sceneModelAssetBounds(record, objects, points) {
    const authored = sceneModelNormalizeAssetBounds(record && record.bounds);
    if (authored && authored.maxDimension > 0) {
      return authored;
    }
    let bounds = null;
    const objectEntries = Array.isArray(objects) ? objects : [];
    for (let index = 0; index < objectEntries.length; index += 1) {
      const object = objectEntries[index] || {};
      if (object.vertices && object.vertices.positions) {
        bounds = sceneModelExpandPositionArray(bounds, object.vertices.positions, object);
      } else if (Array.isArray(object.points)) {
        bounds = sceneModelExpandPointList(bounds, object.points, object);
      } else {
        bounds = sceneModelExpandPrimitiveBounds(bounds, object);
      }
    }
    const pointEntries = Array.isArray(points) ? points : [];
    for (let index = 0; index < pointEntries.length; index += 1) {
      const pointEntry = pointEntries[index] || {};
      bounds = sceneModelExpandPositionArray(bounds, pointEntry._cachedPos || pointEntry.positions, pointEntry);
    }
    return sceneModelFinalizeBounds(bounds);
  }

  function parseSceneModelAsset(raw, src) {
    let payload = raw;
    if (payload && typeof payload === "object" && payload.scene && typeof payload.scene === "object") {
      payload = payload.scene;
    }
    if (Array.isArray(payload)) {
      payload = { objects: payload };
    }
    const record = payload && typeof payload === "object" ? payload : {};
    const objects = Array.isArray(record.objects) ? record.objects.map(function(object) {
      return resolveSceneModelObjectURLs(src, object);
    }) : [];
    const points = Array.isArray(record.points) ? record.points : [];
    const sprites = Array.isArray(record.sprites) ? record.sprites.map(function(sprite) {
      if (!sprite || typeof sprite !== "object") {
        return sprite;
      }
      const resolved = Object.assign({}, sprite);
      resolved.src = resolveSceneModelAssetURL(src, sprite.src);
      return resolved;
    }) : [];
    return {
      src,
      bounds: sceneModelAssetBounds(record, objects, points),
      objects,
      points,
      labels: Array.isArray(record.labels) ? record.labels : [],
      sprites,
      html: Array.isArray(record.html) ? record.html : (Array.isArray(record.htmlOverlays) ? record.htmlOverlays : []),
      lights: Array.isArray(record.lights) ? record.lights : [],
      animations: Array.isArray(record.animations) ? record.animations : [],
      skins: Array.isArray(record.skins) ? record.skins : [],
      nodes: Array.isArray(record.nodes) ? record.nodes : [],
    };
  }

  function sceneModelAssetFormat(src) {
    const raw = typeof src === "string" ? src.trim() : "";
    if (!raw) {
      return "";
    }
    let pathname = raw;
    try {
      pathname = new URL(raw, window.location.href).pathname;
    } catch (_error) {
      pathname = raw.split(/[?#]/, 1)[0];
    }
    const normalized = pathname.toLowerCase();
    if (normalized.endsWith(".glb")) {
      return "glb";
    }
    if (normalized.endsWith(".gltf")) {
      return "gltf";
    }
    return "json";
  }

  // resolveSceneSubFeatureURL reads a hashed sub-feature URL that the
  // island renderer embedded as a data-* attribute on the main scene3d
  // script tag. Using the hashed URL (rather than the unhashed compat
  // URL) lets the browser cache the sub-feature forever, keyed on its
  // content hash. Falls back to the unhashed URL when the attribute
  // isn't present (dev mode, manual integration without the island
  // renderer, etc.).
  function resolveSceneSubFeatureURL(datasetKey, fallback) {
    try {
      var tag = document.querySelector('script[data-gosx-script="feature-scene3d"]');
      if (tag && tag.dataset && tag.dataset[datasetKey]) {
        return tag.dataset[datasetKey];
      }
    } catch (_e) {}
    return fallback;
  }

  // Cached promise for the WebGPU sub-feature chunk. Scene3D now treats
  // WebGPU as the default accelerated backend when the browser exposes it,
  // so the first mount awaits this before choosing its renderer. Failed or
  // unsupported probes still fall through to WebGL/canvas.
  var sceneWebGPUFeaturePromise = null;

  function sceneHasNavigatorWebGPU() {
    return typeof navigator !== "undefined"
      && navigator.gpu
      && typeof navigator.gpu.requestAdapter === "function";
  }

  function ensureWebGPUFeatureLoaded() {
    if (!sceneHasNavigatorWebGPU()) {
      return Promise.resolve(null);
    }
    if (window.__gosx_scene3d_webgpu_api) {
      return Promise.resolve(window.__gosx_scene3d_webgpu_api);
    }
    if (window.__gosx_scene3d_webgpu_feature_promise) {
      return window.__gosx_scene3d_webgpu_feature_promise;
    }
    if (sceneWebGPUFeaturePromise) {
      return sceneWebGPUFeaturePromise;
    }
    sceneWebGPUFeaturePromise = new Promise(function(resolve, reject) {
      var s = document.createElement("script");
      s.async = false;
      s.dataset.gosxScript = "feature-scene3d-webgpu";
      s.src = resolveSceneSubFeatureURL("gosxScene3dWebgpuUrl", "/gosx/bootstrap-feature-scene3d-webgpu.js");
      gosxApplyCurrentScriptNonce(s);
      s.onload = function() {
        if (window.__gosx_scene3d_webgpu_api) {
          resolve(window.__gosx_scene3d_webgpu_api);
        } else {
          sceneWebGPUFeaturePromise = null;
          window.__gosx_scene3d_webgpu_feature_promise = null;
          reject(new Error("scene3d-webgpu chunk loaded but did not publish API"));
        }
      };
      s.onerror = function() {
        sceneWebGPUFeaturePromise = null;
        window.__gosx_scene3d_webgpu_feature_promise = null;
        reject(new Error("failed to load scene3d-webgpu chunk"));
      };
      document.head.appendChild(s);
    });
    window.__gosx_scene3d_webgpu_feature_promise = sceneWebGPUFeaturePromise;
    return sceneWebGPUFeaturePromise;
  }

  function sceneNextFrame() {
    return new Promise(function(resolve) {
      if (typeof window !== "undefined" && typeof window.requestAnimationFrame === "function") {
        window.requestAnimationFrame(function() { resolve(); });
        return;
      }
      setTimeout(resolve, 0);
    });
  }

  async function settlePreferredWebGPUBackend(props, capability) {
    await ensurePreferredWebGPUBackend(props, capability);
    if (typeof sceneWebGPUAvailable === "function" && sceneWebGPUAvailable()) {
      await sceneNextFrame();
    }
  }

  async function ensurePreferredWebGPUBackend(props, capability) {
    if (sceneCapabilityWebGPUPreference(props, capability) !== "prefer") {
      return false;
    }
    if (sceneNeedsWebGLForWebGPUCoverage(props)) {
      return false;
    }
    try {
      var api = await ensureWebGPUFeatureLoaded();
      if (!api) {
        return false;
      }
      if (typeof window.__gosx_scene3d_webgpu_probe_ready === "function") {
        await window.__gosx_scene3d_webgpu_probe_ready();
      }
      return typeof sceneWebGPUAvailable === "function" && sceneWebGPUAvailable();
    } catch (error) {
      console.warn("[gosx] failed to prepare Scene3D WebGPU backend:", error && error.message ? error.message : error);
      return false;
    }
  }

  // --------------------------------------------------------------------------
  // WebGL sub-feature chunk
  // --------------------------------------------------------------------------
  //
  // Cached promise for the WebGL sub-feature chunk. A WebGPU-capable browser
  // never fetches it, which is the whole point of the split: it used to ride
  // in the base scene3d chunk and cost a Chromium page 160_835 minified bytes
  // it never executed. See 26j-feature-scene3d-webgl-prefix.js.
  var sceneWebGLFeaturePromise = null;

  function ensureWebGLFeatureLoaded() {
    // The monolith keeps 16-scene-webgl.js inline, so nothing to fetch.
    if (typeof createScenePBRRendererOrFallback === "function") {
      return Promise.resolve(sceneWebGLChunkAPI() || { inline: true });
    }
    if (window.__gosx_scene3d_webgl_api) {
      return Promise.resolve(window.__gosx_scene3d_webgl_api);
    }
    if (window.__gosx_scene3d_webgl_feature_promise) {
      return window.__gosx_scene3d_webgl_feature_promise;
    }
    if (sceneWebGLFeaturePromise) {
      return sceneWebGLFeaturePromise;
    }
    sceneWebGLFeaturePromise = new Promise(function(resolve, reject) {
      var s = document.createElement("script");
      s.async = false;
      s.dataset.gosxScript = "feature-scene3d-webgl";
      s.src = resolveSceneSubFeatureURL("gosxScene3dWebglUrl", "/gosx/bootstrap-feature-scene3d-webgl.js");
      gosxApplyCurrentScriptNonce(s);
      s.onload = function() {
        if (window.__gosx_scene3d_webgl_api) {
          resolve(window.__gosx_scene3d_webgl_api);
        } else {
          sceneWebGLFeaturePromise = null;
          window.__gosx_scene3d_webgl_feature_promise = null;
          reject(new Error("scene3d-webgl chunk loaded but did not publish API"));
        }
      };
      s.onerror = function() {
        sceneWebGLFeaturePromise = null;
        window.__gosx_scene3d_webgl_feature_promise = null;
        reject(new Error("failed to load scene3d-webgl chunk"));
      };
      document.head.appendChild(s);
    });
    window.__gosx_scene3d_webgl_feature_promise = sceneWebGLFeaturePromise;
    return sceneWebGLFeaturePromise;
  }

  // sceneWebGLBackendRequest builds the same registry request
  // createSceneRendererFromRegistry builds, with one difference: it drops a
  // WebGPU backend the probe could not deliver. That lets backendSelectionOrder
  // rank only the backends this page can really use.
  //
  // There is exactly ONE backend selection policy — backendSelectionOrder in
  // 15c-scene-backend-registry.ts. This function only feeds it. Do not add a
  // second ordering rule here.
  function sceneWebGLBackendRequest(props, capability) {
    const webglPreference = sceneCapabilityWebGLPreference(props, capability);
    const webgpuPreference = sceneCapabilityWebGPUPreference(props, capability);
    const requireWebGL = sceneRequiresWebGL(props);
    const backendCaps = sceneBackendCapsOf(props);
    const webgpuAvail = typeof sceneWebGPUAvailable === "function" && sceneWebGPUAvailable();
    const verdict = chooseSceneBackend(backendCaps, {
      requireWebGL,
      forceWebGL: sceneForcesWebGL(props),
      preferCanvas: sceneBool(props && props.preferCanvas, false),
      preferWebGPU: webgpuPreference === "prefer",
    }, { webgpu: webgpuAvail, webgl: true });
    const wantsWebGPU = verdict
      ? verdict.backend === "webgpu"
      : (webgpuPreference === "prefer" && !sceneNeedsWebGLForWebGPUCoverage(props));
    const preferWebGPU = wantsWebGPU && webgpuAvail && sceneBackendCapsAllowsKind(backendCaps, "webgpu");
    // Trust the verdict the same way wantsWebGPU does above. Without this, a
    // scene whose backendCaps exclude canvas2d (skinning, water) and whose
    // capability tier marks WebGL "avoid" (low-power/reduced-data) computed
    // allowWebGL=false even when verdict.backend was "webgl" — the ONLY real
    // backend chooseSceneBackend could pick once WebGPU was unavailable. That
    // starved settlePreferredWebGLBackend of a reason to fetch the WebGL
    // chunk, so createSceneRenderer ran with no factory loaded and reported
    // "could not acquire a renderer" even though WebGL was the correct and
    // only viable backend.
    const wantsWebGL = verdict
      ? verdict.backend === "webgl"
      : (webglPreference === "prefer" || webglPreference === "force");
    const allowWebGL = wantsWebGL && sceneBackendCapsAllowsKind(backendCaps, "webgl");
    return {
      props,
      capability,
      webgpu: preferWebGPU,
      webgl: allowWebGL,
      webgl2: allowWebGL,
      canvas2d: !requireWebGL && sceneBackendCapsAllowsKind(backendCaps, "canvas2d"),
      preferWebGPU: preferWebGPU,
      forceWebGL: webglPreference === "force" && sceneBackendCapsAllowsKind(backendCaps, "webgl"),
    };
  }

  // sceneMountWantsWebGLFirst reports whether WebGL is the backend this mount
  // will actually draw with. It reads the head of the shared selection order,
  // so forceWebGL, requireWebGL, preferWebGL and preferCanvas keep exactly the
  // meaning 15c gives them.
  function sceneMountWantsWebGLFirst(props, capability) {
    const order = backendSelectionOrder(sceneWebGLBackendRequest(props, capability));
    return order.length > 0 && order[0] === "webgl";
  }

  // settlePreferredWebGLBackend is the WebGL sibling of
  // settlePreferredWebGPUBackend. Await it before createSceneRenderer so a
  // page that draws with WebGL has the chunk, the registry entry and the water
  // runtime in place on the very first render.
  //
  // A WebGPU page does NOT wait here. It reaches WebGL only through a device
  // loss, and deferSceneWebGLFallback below fetches the chunk at that point.
  async function settlePreferredWebGLBackend(props, capability) {
    if (sceneWebGLRendererFactory()) {
      return true;
    }
    if (!sceneMountWantsWebGLFirst(props, capability)) {
      return false;
    }
    try {
      return !!(await ensureWebGLFeatureLoaded());
    } catch (error) {
      console.warn("[gosx] failed to prepare Scene3D WebGL backend:", error && error.message ? error.message : error);
      return false;
    }
  }

  // Warm start for the WebGL chunk. A browser with no navigator.gpu can draw a
  // Scene3D only with WebGL, so the fetch may begin while the base chunk still
  // evaluates instead of waiting for the first mount. First paint then costs
  // the same one round trip it cost before the split. The same holds when the
  // page asked for WebGL through window.__gosx_scene3d_force_webgl.
  //
  // This is a warm start, not a policy decision. settlePreferredWebGLBackend
  // still asks backendSelectionOrder which backend the mount uses, and
  // ensureWebGLFeatureLoaded caches its promise, so the fetch happens once.
  //
  // bootstrap.js keeps 16-scene-webgl.js inline, so the guard below sees the
  // lexical factory and fetches nothing.
  if (typeof createScenePBRRendererOrFallback !== "function"
    && typeof document !== "undefined"
    && document.head
    && (!sceneHasNavigatorWebGPU() || sceneForcesWebGL(null))) {
    try {
      ensureWebGLFeatureLoaded().catch(function() {});
    } catch (_error) {}
  }

  // Cached promise for the GLTF sub-feature chunk. First call starts the
  // fetch; subsequent calls await the same promise. See 26f-feature-
  // scene3d-gltf-prefix.js for the split rationale.
  var sceneGLTFFeaturePromise = null;

  function ensureGLTFFeatureLoaded() {
    if (window.__gosx_scene3d_gltf_api) {
      return Promise.resolve(window.__gosx_scene3d_gltf_api);
    }
    if (sceneGLTFFeaturePromise) {
      return sceneGLTFFeaturePromise;
    }
    sceneGLTFFeaturePromise = new Promise(function(resolve, reject) {
      var s = document.createElement("script");
      s.async = false;
      s.dataset.gosxScript = "feature-scene3d-gltf";
      s.src = resolveSceneSubFeatureURL("gosxScene3dGltfUrl", "/gosx/bootstrap-feature-scene3d-gltf.js");
      gosxApplyCurrentScriptNonce(s);
      s.onload = function() {
        if (window.__gosx_scene3d_gltf_api) {
          resolve(window.__gosx_scene3d_gltf_api);
        } else {
          reject(new Error("scene3d-gltf chunk loaded but did not publish API"));
        }
      };
      s.onerror = function() {
        sceneGLTFFeaturePromise = null; // allow retry on next attempt
        reject(new Error("failed to load scene3d-gltf chunk"));
      };
      document.head.appendChild(s);
    });
    return sceneGLTFFeaturePromise;
  }

  function scenePropsHasIBLProducts(props) {
    var scene = props && props.scene && typeof props.scene === "object" ? props.scene : props;
    var environment = scene && scene.environment && typeof scene.environment === "object"
      ? scene.environment
      : null;
    var ibl = environment && environment.ibl && typeof environment.ibl === "object"
      ? environment.ibl
      : null;
    return Boolean(
      ibl &&
      ibl.radiance && typeof ibl.radiance.uri === "string" && ibl.radiance.uri.trim() &&
      ibl.irradiance && typeof ibl.irradiance.uri === "string" && ibl.irradiance.uri.trim() &&
      ibl.brdfLUT && typeof ibl.brdfLUT.uri === "string" && ibl.brdfLUT.uri.trim()
    );
  }

  // IBL products use the small KTX2 reader that currently ships ahead of the
  // glTF parser in the glTF sub-feature. Settle it before renderer creation so
  // WebGPU cannot silently construct a frame layout that ignores an authored
  // Environment.IBL descriptor. The parser chunk is cached page-wide.
  async function settleSceneIBLFeature(props) {
    if (!scenePropsHasIBLProducts(props)) return true;
    try {
      await ensureGLTFFeatureLoaded();
      var ready = Boolean(window.__gosx_scene3d_ktx2);
      if (!ready) {
        gosxSceneEmit("warn", "ibl-loader-unavailable", {
          reason: "ktx2-api-not-published",
        });
      }
      return ready;
    } catch (error) {
      console.warn("[gosx] failed to prepare Scene3D IBL products:", error && error.message ? error.message : error);
      gosxSceneEmit("warn", "ibl-loader-unavailable", {
        reason: "ktx2-feature-load-failed",
        error: error && error.message ? String(error.message) : String(error),
      });
      return false;
    }
  }

  // Cached promise for the animation sub-feature chunk. Consumers that
  // want to drive keyframe or skeletal animations can await this helper
  // and then use window.__gosx_scene3d_animation_api.
  var sceneAnimationFeaturePromise = null;

  function ensureAnimationFeatureLoaded() {
    if (window.__gosx_scene3d_animation_api) {
      return Promise.resolve(window.__gosx_scene3d_animation_api);
    }
    if (sceneAnimationFeaturePromise) {
      return sceneAnimationFeaturePromise;
    }
    sceneAnimationFeaturePromise = new Promise(function(resolve, reject) {
      var s = document.createElement("script");
      s.async = false;
      s.dataset.gosxScript = "feature-scene3d-animation";
      s.src = resolveSceneSubFeatureURL("gosxScene3dAnimationUrl", "/gosx/bootstrap-feature-scene3d-animation.js");
      gosxApplyCurrentScriptNonce(s);
      s.onload = function() {
        if (window.__gosx_scene3d_animation_api) {
          resolve(window.__gosx_scene3d_animation_api);
        } else {
          reject(new Error("scene3d-animation chunk loaded but did not publish API"));
        }
      };
      s.onerror = function() {
        sceneAnimationFeaturePromise = null;
        reject(new Error("failed to load scene3d-animation chunk"));
      };
      document.head.appendChild(s);
    });
    return sceneAnimationFeaturePromise;
  }

  // Expose the animation lazy-loader for consumers that need to drive
  // keyframe or skeletal clips from outside the main scene mount.
  window.__gosx_ensure_scene3d_animation_loaded = ensureAnimationFeatureLoaded;

  // Cached promise for the compute sub-feature chunk. It carries the GPU
  // particle simulation, the CPU particle fallback, the particle force
  // registry and the GPU instanced-cull system. A scene with one cube and one
  // directional light runs none of them, and used to pay 8_772 gzip bytes for
  // all of them. See 26k-feature-scene3d-compute-prefix.js.
  var sceneComputeFeaturePromise = null;

  function ensureComputeFeatureLoaded() {
    if (window.__gosx_scene3d_compute_api) {
      return Promise.resolve(window.__gosx_scene3d_compute_api);
    }
    // bootstrap.js keeps 16b-scene-compute.js inline, so the API object
    // already carries the factory and no fetch is needed.
    if (window.__gosx_scene3d_api
      && typeof window.__gosx_scene3d_api.createSceneParticleSystem === "function") {
      return Promise.resolve(window.__gosx_scene3d_api);
    }
    if (sceneComputeFeaturePromise) {
      return sceneComputeFeaturePromise;
    }
    sceneComputeFeaturePromise = new Promise(function(resolve, reject) {
      var url = resolveSceneSubFeatureURL("gosxScene3dComputeUrl", "");
      if (!url) {
        // The server did not advertise the chunk, so this page's scene
        // declared no particles and no instanced meshes. Refuse rather than
        // guess a path: a 404 here would look like a broken deployment.
        sceneComputeFeaturePromise = null;
        reject(new Error("scene3d-compute chunk URL was not advertised"));
        return;
      }
      var s = document.createElement("script");
      s.async = false;
      s.dataset.gosxScript = "feature-scene3d-compute";
      s.src = url;
      gosxApplyCurrentScriptNonce(s);
      s.onload = function() {
        if (window.__gosx_scene3d_compute_api) {
          resolve(window.__gosx_scene3d_compute_api);
        } else {
          sceneComputeFeaturePromise = null;
          reject(new Error("scene3d-compute chunk loaded but did not publish API"));
        }
      };
      s.onerror = function() {
        sceneComputeFeaturePromise = null; // allow retry on the next attempt
        reject(new Error("failed to load scene3d-compute chunk"));
      };
      document.head.appendChild(s);
    });
    return sceneComputeFeaturePromise;
  }

  // Expose the compute lazy-loader so a runtime program that adds particles
  // after mount can await the chunk instead of dropping the first frames.
  window.__gosx_ensure_scene3d_compute_loaded = ensureComputeFeatureLoaded;

  // sceneNeedsComputeFeature reports whether a scene state reaches the compute
  // chunk. Two paths do:
  //
  //   1. A compute particle system. Both renderers call
  //      createSceneParticleSystem for each entry.
  //   2. An instanced mesh. The WebGPU renderer runs the GPU frustum cull for
  //      instanced meshes, and that lives in the same file.
  //
  // Be permissive on purpose. A chunk that is needed and not fetched is a
  // dropped particle system; a chunk fetched and unused costs one request the
  // page would otherwise not make.
  function sceneNeedsComputeFeature(state) {
    if (!state) return false;
    if (Array.isArray(state.computeParticles) && state.computeParticles.length > 0) {
      return true;
    }
    return Array.isArray(state.instancedMeshes) && state.instancedMeshes.length > 0;
  }

  // requestSceneComputeFeatureIfNeeded starts the fetch without awaiting it.
  // A runtime program or a scene command can add a particle system after the
  // mount settled, and the renderer bridges return null until the chunk lands.
  // The scene then draws its particles one or two frames later instead of
  // throwing.
  function requestSceneComputeFeatureIfNeeded(state) {
    if (!sceneNeedsComputeFeature(state)) return;
    try {
      ensureComputeFeatureLoaded().catch(function() {});
    } catch (_error) {}
  }

  // settleSceneComputeFeature awaits the compute chunk before the first
  // render when the scene needs it. Await it next to settlePreferredWebGLBackend
  // so the first frame draws the particles instead of skipping them.
  async function settleSceneComputeFeature(state) {
    if (!sceneNeedsComputeFeature(state)) {
      return false;
    }
    try {
      return !!(await ensureComputeFeatureLoaded());
    } catch (error) {
      console.warn("[gosx] failed to prepare Scene3D compute systems:",
        error && error.message ? error.message : error);
      return false;
    }
  }

  // Cached promise for the decompress sub-feature chunk. It carries the
  // quantized-array decoder, the progressive and level-of-detail ladders, and
  // the procedural point generators. See
  // 26l-feature-scene3d-decompress-prefix.js.
  var sceneDecompressFeaturePromise = null;

  // sceneDecompressAPIFunction resolves one decompress entry point. The
  // monolith keeps 11a and 11b inline, so the lookup finds the function on the
  // API object either way: 10-runtime-scene-core.js publishes the inline copy
  // there, and the chunk suffix publishes the fetched copy to the same place.
  function sceneDecompressAPIFunction(name) {
    var api = typeof window !== "undefined" ? window.__gosx_scene3d_api : null;
    return api && typeof api[name] === "function" ? api[name] : null;
  }

  function ensureDecompressFeatureLoaded() {
    if (sceneDecompressAPIFunction("sceneDecompressProps")) {
      return Promise.resolve(window.__gosx_scene3d_api);
    }
    if (sceneDecompressFeaturePromise) {
      return sceneDecompressFeaturePromise;
    }
    sceneDecompressFeaturePromise = new Promise(function(resolve, reject) {
      var url = resolveSceneSubFeatureURL("gosxScene3dDecompressUrl", "");
      if (!url) {
        // The server did not advertise the chunk, so this page's scene carries
        // no compressed array and no generator descriptor. Refuse rather than
        // guess a path: a 404 here would look like a broken deployment.
        sceneDecompressFeaturePromise = null;
        reject(new Error("scene3d-decompress chunk URL was not advertised"));
        return;
      }
      var s = document.createElement("script");
      s.async = false;
      s.dataset.gosxScript = "feature-scene3d-decompress";
      s.src = url;
      gosxApplyCurrentScriptNonce(s);
      s.onload = function() {
        if (sceneDecompressAPIFunction("sceneDecompressProps")) {
          resolve(window.__gosx_scene3d_api);
        } else {
          sceneDecompressFeaturePromise = null;
          reject(new Error("scene3d-decompress chunk loaded but did not publish API"));
        }
      };
      s.onerror = function() {
        sceneDecompressFeaturePromise = null; // allow retry on the next attempt
        reject(new Error("failed to load scene3d-decompress chunk"));
      };
      document.head.appendChild(s);
    });
    return sceneDecompressFeaturePromise;
  }

  window.__gosx_ensure_scene3d_decompress_loaded = ensureDecompressFeatureLoaded;

  // sceneEntryNeedsDecompress reports whether one points, instanced-mesh or
  // animation-channel record carries something only the decompress chunk can
  // read. The field names match the writers in 11a-scene-decompress.ts.
  function sceneEntryNeedsDecompress(entry) {
    if (!entry || typeof entry !== "object") return false;
    return Boolean(entry.generator
      || entry.compressedPositions
      || entry.compressedSizes
      || entry.compressedTransforms
      || entry.compressedTimes
      || entry.compressedValues
      || entry.previewPositions
      || entry.previewSizes
      || entry.previewTransforms
      || entry.previewTimes
      || entry.previewValues);
  }

  function sceneListNeedsDecompress(list) {
    if (!Array.isArray(list)) return false;
    for (var i = 0; i < list.length; i += 1) {
      if (sceneEntryNeedsDecompress(list[i])) return true;
    }
    return false;
  }

  // sceneNeedsDecompressFeature inspects the RAW props, not the scene state,
  // because createSceneState calls sceneDecompressProps before it builds the
  // state. A compression policy alone is enough: progressive and
  // level-of-detail mode both drive the chunk on every frame.
  function sceneNeedsDecompressFeature(props) {
    if (!props || typeof props !== "object") return false;
    if (props.compression) return true;
    var scene = props.scene && typeof props.scene === "object" ? props.scene : null;
    var points = (scene && scene.points) || props.points;
    if (sceneListNeedsDecompress(points)) return true;
    var meshes = (scene && scene.instancedMeshes) || props.instancedMeshes;
    if (sceneListNeedsDecompress(meshes)) return true;
    var animations = (scene && scene.animations) || props.animations;
    if (!Array.isArray(animations)) return false;
    for (var i = 0; i < animations.length; i += 1) {
      var clip = animations[i];
      if (clip && sceneListNeedsDecompress(clip.channels)) return true;
    }
    return false;
  }

  // settleSceneDecompressFeature awaits the decompress chunk BEFORE
  // createSceneState runs. createSceneState calls sceneDecompressProps as its
  // first statement, so a late chunk would leave every compressed array
  // undecoded and the scene would draw nothing.
  async function settleSceneDecompressFeature(props) {
    if (!sceneNeedsDecompressFeature(props)) {
      return false;
    }
    try {
      return !!(await ensureDecompressFeatureLoaded());
    } catch (error) {
      console.warn("[gosx] failed to prepare Scene3D compressed data:",
        error && error.message ? error.message : error);
      return false;
    }
  }

  function sceneModelHydrationIsCurrent(meta) {
    if (!meta || !meta.state) {
      return true;
    }
    return Number(meta.state._modelHydrationGeneration) === Number(meta.generation);
  }

  function invalidateSceneModelHydration(state) {
    if (!state) return 0;
    const generation = Math.max(0, Math.floor(sceneNumber(state._modelHydrationGeneration, 0))) + 1;
    state._modelHydrationGeneration = generation;
    return generation;
  }

  function publishSceneModelAssetStatus(mount, status, asset, cached, error, meta) {
    if (!mount) return;
    const context = meta && typeof meta === "object" ? meta : {};
    const variantScope = context.variantScope;
    const variantContext = variantScope && variantScope.context
      ? normalizeSceneModelTextureVariantContext(variantScope.context, "")
      : normalizeSceneModelTextureVariantContext(null, "");
    const detail = {
      status: String(status || ""),
      asset: String(asset || ""),
      cached: Boolean(cached),
      error: error ? String(error) : "",
      generation: Math.max(0, Math.floor(sceneNumber(context.generation, 0))),
      modelID: String(context.modelID || ""),
      modelIndex: Math.max(0, Math.floor(sceneNumber(context.modelIndex, 0))),
      stage: String(context.stage || "load"),
      stale: Boolean(context.stale),
      committed: Boolean(context.committed),
      variantScope: variantScope && variantScope.key ? String(variantScope.key) : "",
      variantBackend: variantContext.backend,
      variantUploadReady: variantContext.uploadReady,
      variantTokenCount: variantContext.tokens.length,
    };
    setAttrValue(mount, "data-gosx-scene3d-model-status", detail.status);
    setAttrValue(mount, "data-gosx-scene3d-model-asset", detail.asset);
    setAttrValue(mount, "data-gosx-scene3d-model-cache", detail.cached ? "true" : "false");
    setAttrValue(mount, "data-gosx-scene3d-model-error", detail.error);
    setAttrValue(mount, "data-gosx-scene3d-model-generation", String(detail.generation));
    setAttrValue(mount, "data-gosx-scene3d-model-id", detail.modelID);
    setAttrValue(mount, "data-gosx-scene3d-model-stage", detail.stage);
    setAttrValue(mount, "data-gosx-scene3d-model-variant-scope", detail.variantScope);
    setAttrValue(mount, "data-gosx-scene3d-model-variant-backend", detail.variantBackend);
    setAttrValue(mount, "data-gosx-scene3d-model-variant-upload-ready", detail.variantUploadReady ? "true" : "false");
    if (typeof CustomEvent === "function" && typeof mount.dispatchEvent === "function") {
      mount.dispatchEvent(new CustomEvent("gosx:scene3d:model-status", { detail, bubbles: true }));
    }
  }

  function publishSceneModelHydrationStatus(mount, status, detail) {
    if (!mount) return;
    const source = detail && typeof detail === "object" ? detail : {};
    const counts = source.counts && typeof source.counts === "object" ? source.counts : {};
    const eventDetail = {
      status: String(status || ""),
      generation: Math.max(0, Math.floor(sceneNumber(source.generation, 0))),
      currentGeneration: Math.max(0, Math.floor(sceneNumber(source.currentGeneration, source.generation))),
      stale: Boolean(source.stale),
      committed: Boolean(source.committed),
      stage: String(source.stage || ""),
      modelID: String(source.modelID || ""),
      modelIndex: Math.max(0, Math.floor(sceneNumber(source.modelIndex, 0))),
      asset: String(source.asset || ""),
      error: source.error ? String(source.error) : "",
      counts,
    };
    setAttrValue(mount, "data-gosx-scene3d-model-hydration-status", eventDetail.status);
    setAttrValue(mount, "data-gosx-scene3d-model-hydration-generation", String(eventDetail.generation));
    setAttrValue(mount, "data-gosx-scene3d-model-hydration-current-generation", String(eventDetail.currentGeneration));
    setAttrValue(mount, "data-gosx-scene3d-model-hydration-stale", eventDetail.stale ? "true" : "false");
    setAttrValue(mount, "data-gosx-scene3d-model-hydration-committed", eventDetail.committed ? "true" : "false");
    setAttrValue(mount, "data-gosx-scene3d-model-hydration-failure-stage", eventDetail.stage);
    setAttrValue(mount, "data-gosx-scene3d-model-hydration-error", eventDetail.error);
    try {
      setAttrValue(mount, "data-gosx-scene3d-model-hydration-counts", JSON.stringify(counts));
    } catch (_error) {
      setAttrValue(mount, "data-gosx-scene3d-model-hydration-counts", "{}");
    }
    if (typeof CustomEvent === "function" && typeof mount.dispatchEvent === "function") {
      mount.dispatchEvent(new CustomEvent("gosx:scene3d:model-hydration-status", { detail: eventDetail, bubbles: true }));
    }
  }

  function publishSceneProgressiveModelStatus(mount, status, detail) {
    if (!mount) return;
    const source = detail && typeof detail === "object" ? detail : {};
    const eventDetail = {
      status: String(status || ""),
      modelID: String(source.modelID || ""),
      modelIndex: Math.max(0, Math.floor(sceneNumber(source.modelIndex, 0))),
      previewSrc: String(source.previewSrc || ""),
      fullSrc: String(source.fullSrc || ""),
      error: source.error ? String(source.error) : "",
    };
    setAttrValue(mount, "data-gosx-scene3d-progressive-model-status", eventDetail.status);
    setAttrValue(mount, "data-gosx-scene3d-progressive-model-id", eventDetail.modelID);
    setAttrValue(mount, "data-gosx-scene3d-progressive-model-preview-src", eventDetail.previewSrc);
    setAttrValue(mount, "data-gosx-scene3d-progressive-model-full-src", eventDetail.fullSrc);
    setAttrValue(mount, "data-gosx-scene3d-progressive-model-error", eventDetail.error);
    if (typeof CustomEvent === "function" && typeof mount.dispatchEvent === "function") {
      mount.dispatchEvent(new CustomEvent("gosx:scene3d:progressive-model", { detail: eventDetail, bubbles: true }));
    }
  }

  function sceneProgressiveModelEntries(models) {
    const entries = [];
    const source = Array.isArray(models) ? models : [];
    for (let index = 0; index < source.length; index += 1) {
      const model = source[index];
      if (!model || model.progressive !== true || !model.previewSrc || !model.fullSrc) {
        continue;
      }
      entries.push({ model, index });
    }
    return entries;
  }

  function sceneFullModelFromProgressive(model) {
    const next = Object.assign({}, model);
    next.src = String(model.fullSrc || "").trim();
    next.progressive = false;
    return next;
  }

  function sceneModelAssetHasContent(asset) {
    if (!asset || typeof asset !== "object") return false;
    return (Array.isArray(asset.objects) && asset.objects.length > 0)
      || (Array.isArray(asset.points) && asset.points.length > 0)
      || (Array.isArray(asset.labels) && asset.labels.length > 0)
      || (Array.isArray(asset.sprites) && asset.sprites.length > 0)
      || (Array.isArray(asset.html) && asset.html.length > 0)
      || (Array.isArray(asset.lights) && asset.lights.length > 0)
      || (Array.isArray(asset.animations) && asset.animations.length > 0)
      || (Array.isArray(asset.skins) && asset.skins.length > 0)
      || (Array.isArray(asset.nodes) && asset.nodes.length > 0);
  }

  function sceneModelHydrationOutcomeDetail(outcome, expectedGeneration) {
    if (Array.isArray(outcome)) {
      const entries = outcome.filter(function(entry) { return entry && typeof entry === "object"; });
      const expected = Math.max(0, Math.floor(sceneNumber(expectedGeneration, 0)));
      const committed = entries.filter(function(entry) {
        return entry.committed === true && entry.stale !== true && (expected <= 0 || Number(entry.generation) === expected);
      });
      const source = committed.length ? committed : entries;
      return source.sort(function(a, b) {
        return Math.max(0, Math.floor(sceneNumber(b.generation, 0))) - Math.max(0, Math.floor(sceneNumber(a.generation, 0)));
      })[0] || null;
    }
    return outcome && typeof outcome === "object" ? outcome : null;
  }

  function sceneCreateProgressiveModelToken(state, models, previewGeneration) {
    if (!state) return null;
    const token = {
      id: Math.max(0, Math.floor(sceneNumber(state._modelProgressiveTokenID, 0))) + 1,
      models,
      previewGeneration: Math.max(0, Math.floor(sceneNumber(previewGeneration, state._modelHydrationGeneration))),
      fullGeneration: 0,
      cancelled: false,
      resolveRender: null,
      renderTimeout: null,
    };
    state._modelProgressiveTokenID = token.id;
    state._modelProgressiveToken = token;
    return token;
  }

  function sceneProgressiveModelTokenCurrent(state, token) {
    return Boolean(state && token && state._modelProgressiveToken === token && token.cancelled !== true);
  }

  function cancelSceneProgressiveModelLifecycle(state) {
    if (!state || !state._modelProgressiveToken) return;
    state._modelProgressiveToken.cancelled = true;
    if (typeof state._modelProgressiveToken.resolveRender === "function") {
      state._modelProgressiveToken.resolveRender(false);
    }
    if (state._modelProgressiveToken.renderTimeout != null) {
      clearTimeout(state._modelProgressiveToken.renderTimeout);
      state._modelProgressiveToken.renderTimeout = null;
    }
    state._modelProgressiveToken = null;
  }

  function notifySceneProgressiveModelRenderCommitted(state) {
    const token = state && state._modelProgressiveToken;
    if (!token || typeof token.resolveRender !== "function") return;
    const resolve = token.resolveRender;
    token.resolveRender = null;
    if (token.renderTimeout != null) {
      clearTimeout(token.renderTimeout);
      token.renderTimeout = null;
    }
    resolve(sceneProgressiveModelTokenCurrent(state, token));
  }

  function sceneProgressiveModelWaitForRender(state, token, mount, entry, canRender, timeoutMS) {
    return new Promise(function(resolve) {
      if (!sceneProgressiveModelTokenCurrent(state, token)) {
        resolve(false);
        return;
      }
      token.resolveRender = resolve;
      if (typeof canRender === "function" && !canRender()) {
        publishSceneProgressiveModelStatus(mount, "full-pending-render", {
          modelID: entry && entry.model ? entry.model.id : "",
          modelIndex: entry ? entry.index : 0,
          previewSrc: entry && entry.model ? entry.model.previewSrc : "",
          fullSrc: entry && entry.model ? entry.model.fullSrc : "",
          error: "render paused",
        });
        return;
      }
      token.renderTimeout = setTimeout(function() {
        if (token.resolveRender !== resolve) return;
        token.renderTimeout = null;
        token.resolveRender = null;
        if (state && state._modelProgressiveToken === token) {
          state._modelProgressiveToken = null;
        }
        token.cancelled = true;
        publishSceneProgressiveModelStatus(mount, "full-render-timeout", {
          modelID: entry && entry.model ? entry.model.id : "",
          modelIndex: entry ? entry.index : 0,
          previewSrc: entry && entry.model ? entry.model.previewSrc : "",
          fullSrc: entry && entry.model ? entry.model.fullSrc : "",
          error: "render wait timed out",
        });
      }, Math.max(1, Math.floor(sceneNumber(timeoutMS, 2000))));
    });
  }

  function scheduleSceneProgressiveModelLifecycle(state, mount, initialHydration, applyModels, options) {
    if (!state || typeof applyModels !== "function") return null;
    const opts = options && typeof options === "object" ? options : {};
    const canRender = typeof opts.canRender === "function" ? opts.canRender : null;
    const renderTimeoutMS = Math.max(1, Math.floor(sceneNumber(opts.renderTimeoutMS, 2000)));
    const restorePreview = typeof opts.restorePreview === "function" ? opts.restorePreview : null;
    const models = Array.isArray(opts.models) ? opts.models : (Array.isArray(state.models) ? state.models : []);
    const entries = sceneProgressiveModelEntries(models);
    if (!entries.length) return null;
    const previewGeneration = Math.max(0, Math.floor(sceneNumber(state._modelHydrationGeneration, 0)));
    const variantScope = state._modelTextureVariantScope;
    let token = null;
    const lifecycle = Promise.resolve(initialHydration).then(function(outcome) {
      const hydration = sceneModelHydrationOutcomeDetail(outcome, previewGeneration);
      if (!hydration || hydration.committed !== true || hydration.stale === true || Number(hydration.generation) !== previewGeneration || state.models !== models) {
        return null;
      }
      token = sceneCreateProgressiveModelToken(state, models, previewGeneration);
      if (!sceneProgressiveModelTokenCurrent(state, token)) {
        return null;
      }
      entries.forEach(function(entry) {
        publishSceneProgressiveModelStatus(mount, "preview-ready", {
          modelID: entry.model.id,
          modelIndex: entry.index,
          previewSrc: entry.model.previewSrc,
          fullSrc: entry.model.fullSrc,
        });
      });
      return Promise.all(entries.map(function(entry) {
        return loadSceneModelAsset(entry.model.fullSrc, mount, {
          state,
          generation: previewGeneration,
          modelID: entry.model.id || "",
          modelIndex: entry.index,
          stage: "progressive-preload",
          variantScope,
        }).then(function(asset) {
          if (!sceneModelAssetHasContent(asset)) {
            publishSceneProgressiveModelStatus(mount, "full-preload-failed", {
              modelID: entry.model.id,
              modelIndex: entry.index,
              previewSrc: entry.model.previewSrc,
              fullSrc: entry.model.fullSrc,
              error: "empty model asset",
            });
            return { ok: false, entry, error: "empty model asset" };
          }
          return { ok: true, entry, asset };
        }, function(error) {
          publishSceneProgressiveModelStatus(mount, "full-preload-failed", {
            modelID: entry.model.id,
            modelIndex: entry.index,
            previewSrc: entry.model.previewSrc,
            fullSrc: entry.model.fullSrc,
            error: error && error.message ? error.message : error,
          });
          return { ok: false, entry, error };
        });
      }));
    }).then(function(results) {
      if (!sceneProgressiveModelTokenCurrent(state, token) || !Array.isArray(results) || !results.some(function(result) { return result && result.ok; })) {
        return null;
      }
      const nextModels = models.map(function(model, index) {
        const matched = results.find(function(result) {
          return result && result.ok && result.entry && result.entry.index === index;
        });
        return matched ? sceneFullModelFromProgressive(model) : model;
      });
      const firstOK = results.find(function(result) { return result && result.ok; });
      publishSceneProgressiveModelStatus(mount, "swap-issued", {
        modelID: firstOK.entry.model.id,
        modelIndex: firstOK.entry.index,
        previewSrc: firstOK.entry.model.previewSrc,
        fullSrc: firstOK.entry.model.fullSrc,
      });
      if (!sceneProgressiveModelTokenCurrent(state, token) || state.models !== models) {
        return null;
      }
      const swapResult = applyModels(nextModels);
      token.fullGeneration = Math.max(0, Math.floor(sceneNumber(state._modelHydrationGeneration, 0)));
      return Promise.resolve(swapResult).then(function(outcome) {
        const hydration = sceneModelHydrationOutcomeDetail(outcome, token.fullGeneration);
        if (!sceneProgressiveModelTokenCurrent(state, token) || !hydration || hydration.committed !== true || hydration.stale === true || Number(hydration.generation) !== Number(token.fullGeneration)) {
          if (restorePreview) {
            restorePreview(models);
          }
          publishSceneProgressiveModelStatus(mount, "full-preload-failed", {
            modelID: firstOK.entry.model.id,
            modelIndex: firstOK.entry.index,
            previewSrc: firstOK.entry.model.previewSrc,
            fullSrc: firstOK.entry.model.fullSrc,
            error: hydration && hydration.failureStage ? hydration.failureStage : "full hydration failed",
          });
          return null;
        }
        return sceneProgressiveModelWaitForRender(state, token, mount, firstOK.entry, canRender, renderTimeoutMS).then(function(rendered) {
          if (!rendered || !sceneProgressiveModelTokenCurrent(state, token)) {
            return null;
          }
          const settled = firstOK;
          state._modelProgressiveToken = null;
          state._modelProgressiveLastSettledGeneration = token.fullGeneration;
          token.cancelled = true;
          token.resolveRender = null;
          return settled;
        });
      }).then(function(settled) {
        if (!settled) return null;
        publishSceneProgressiveModelStatus(mount, "full-settled", {
          modelID: settled.entry.model.id,
          modelIndex: settled.entry.index,
          previewSrc: settled.entry.model.previewSrc,
          fullSrc: settled.entry.model.fullSrc,
        });
        return null;
      });
    }).catch(function(error) {
      if (sceneProgressiveModelTokenCurrent(state, token)) {
        publishSceneProgressiveModelStatus(mount, "full-preload-failed", {
          error: error && error.message ? error.message : error,
        });
      }
    });
    return lifecycle;
  }

  async function loadSceneModelAsset(src, mount, hydrationMeta) {
    const key = String(src || "").trim();
    if (!key) {
      return parseSceneModelAsset({}, key);
    }
    const format = sceneModelAssetFormat(key);
    const variantScope = hydrationMeta && hydrationMeta.variantScope
      ? hydrationMeta.variantScope
      : (hydrationMeta && hydrationMeta.state ? hydrationMeta.state._modelTextureVariantScope : null);
    const cacheKey = (format === "glb" || format === "gltf") && variantScope && variantScope.key
      ? String(variantScope.key) + "\u0000" + key
      : key;
    const statusMeta = hydrationMeta
      ? Object.assign({}, hydrationMeta, { variantScope })
      : { variantScope };
    const cached = sceneModelAssetCache.has(cacheKey);
    if (sceneModelAssetReady.has(cacheKey)) {
      const readyResult = await sceneModelAssetCache.get(cacheKey);
      if (sceneModelHydrationIsCurrent(hydrationMeta)) {
        publishSceneModelAssetStatus(mount, "cached", key, true, "", statusMeta);
      }
      return readyResult.asset;
    }
    if (sceneModelHydrationIsCurrent(hydrationMeta)) {
      publishSceneModelAssetStatus(mount, "loading", key, cached, "", statusMeta);
    }
    if (!cached) {
      const loadPromise = (async function() {
        try {
          if (format === "glb" || format === "gltf") {
            // GLTF parsing lives in a sub-feature chunk that's fetched
            // on demand — the first .glb/.gltf request on a page pays
            // the download + parse cost, subsequent ones reuse the
            // cached module. Pages that never load models never fetch
            // the chunk at all.
            var gltfApi = await ensureGLTFFeatureLoaded();
            const variantContext = variantScope ? variantScope.ready : null;
            const asset = parseSceneModelAsset(gltfApi.gltfSceneToModelAsset(
              await gltfApi.sceneLoadGLTFModel(key, variantContext),
              key
            ), key);
            sceneModelAssetReady.add(cacheKey);
            return { asset, error: null };
          }
          const response = await fetch(key, { credentials: "same-origin" });
          if (!response || !response.ok) {
            throw new Error("HTTP " + String(response && response.status || 0));
          }
          const asset = parseSceneModelAsset(await response.json(), key);
          sceneModelAssetReady.add(cacheKey);
          return { asset, error: null };
        } catch (error) {
          console.warn("[gosx] failed to load Scene3D model asset:", key, error && error.message ? error.message : error);
          gosxSceneEmit("warn", "model-asset-load-failed", {
            asset: String(key || ""),
            error: error && error.message ? String(error.message) : String(error),
          });
          return { asset: parseSceneModelAsset({}, key), error };
        }
      })();
      sceneModelAssetCache.set(cacheKey, loadPromise);
      // Failed entries must not poison the page for its lifetime. Keep the
      // in-flight Promise long enough to deduplicate current waiters, then
      // evict it before a subsequent request so a transient failure can retry.
      loadPromise.then(function(result) {
        if (result && result.error && sceneModelAssetCache.get(cacheKey) === loadPromise) {
          sceneModelAssetCache.delete(cacheKey);
        }
      });
    }
    const result = await sceneModelAssetCache.get(cacheKey);
    if (sceneModelHydrationIsCurrent(hydrationMeta)) {
      if (result.error) {
        publishSceneModelAssetStatus(mount, "error", key, cached,
          result.error && result.error.message ? result.error.message : "model asset failed to load", statusMeta);
      } else {
        publishSceneModelAssetStatus(mount, cached ? "cached" : "loaded", key, cached, "", statusMeta);
      }
    }
    return result.asset;
  }

  // Public prewarm hook for progressive single-engine upgrades (e.g. a boot
  // script that swaps a preview mount from a small preview GLB to the full
  // model without tearing down/re-mounting the engine). Fetching + parsing a
  // model asset ahead of the actual swap lands the parsed result in the
  // module-level asset cache. JSON assets remain reusable by a later mount.
  // GLB/glTF preloads are intentionally neutral and do not populate a
  // renderer-scoped parsed cache: guessing a backend here would let one mount
  // choose texture URIs for another. Safe to call with no mounted engine;
  // resolves to the parsed asset ({objects, points, labels, sprites, html,
  // lights, ...}, all empty arrays on failure) so callers can verify the load
  // actually produced content before committing to a swap.
  window.__gosx_scene3d_preload_model = function(src) {
    return loadSceneModelAsset(String(src || "").trim(), null);
  };
  if (typeof window !== "undefined") {
    window.__gosx_runtime_payload_normalizers = window.__gosx_runtime_payload_normalizers || {};
    window.__gosx_runtime_payload_normalizers.GoSXScene3D = function(props, _entry, helpers) {
      if (props && props.scene) helpers.inflateSceneShaderLib(props.scene);
      return props || null;
    };
    window.__gosx = window.__gosx || {};
    window.__gosx.scene3d = window.__gosx.scene3d || {};
    window.__gosx.scene3d.preloadModel = function(src) {
      return window.__gosx_scene3d_preload_model(src);
    };
    window.__gosx.scene3d.setPerformanceTelemetry = function(enabled) {
      window.__gosx_scene3d_perf = enabled === true;
      return window.__gosx_scene3d_perf;
    };
    window.__gosx.scene3d.isPerformanceTelemetryEnabled = function() {
      return window.__gosx_scene3d_perf === true;
    };
  }

  function sceneModelHasSkins(skins) {
    return Array.isArray(skins) && skins.some(function(skin) {
      return Boolean(skin && Array.isArray(skin.joints) && skin.joints.length > 0 && skin.inverseBindMatrices);
    });
  }

  function sceneModelRootNodes(nodes) {
    if (!Array.isArray(nodes) || !nodes.length) {
      return [];
    }
    const childSet = new Set();
    for (let index = 0; index < nodes.length; index += 1) {
      const children = nodes[index] && nodes[index].children;
      if (!Array.isArray(children)) {
        continue;
      }
      for (let childIndex = 0; childIndex < children.length; childIndex += 1) {
        childSet.add(children[childIndex]);
      }
    }
    const roots = [];
    for (let index = 0; index < nodes.length; index += 1) {
      if (!childSet.has(index)) {
        roots.push(index);
      }
    }
    return roots;
  }

  // True when the WASM motion mixer is active for this record (P4-M3, opt-in).
  function sceneModelWasmMixerActive(record) {
    return Boolean(
      record && record.wasmMixer &&
      typeof window !== "undefined" && window.__gosx_motion_wasm &&
      typeof window.__gosx_motion_mixer_update === "function"
    );
  }

  // Drive one WASM-mixer frame: tick the mixer into a reused out buffer
  // (grow-and-retick when the write count exceeds capacity) and decode the
  // packed writes into animatedTransforms via the published animation API.
  function sceneAdvanceWasmModelMixer(record, deltaTime, reduced, animatedTransforms) {
    const api = record.animationApi;
    if (!api || typeof api.wasmDecodePose !== "function") {
      return;
    }
    if (!record._wasmMixerF64 || !record._wasmMixerU8) {
      record._wasmMixerF64 = new Float64Array(2048);
      record._wasmMixerU8 = new Uint8Array(record._wasmMixerF64.buffer);
    }
    const reducedFlag = reduced === true;
    let count = window.__gosx_motion_mixer_update(record.wasmMixer, deltaTime, reducedFlag, record._wasmMixerU8);
    if (count > record._wasmMixerF64.length) {
      record._wasmMixerF64 = new Float64Array(count);
      record._wasmMixerU8 = new Uint8Array(record._wasmMixerF64.buffer);
      // Pass dt=0: the clip clock already advanced on the first call above.
      // Re-emitting at the current time with dt=0 avoids a double clock step.
      count = window.__gosx_motion_mixer_update(record.wasmMixer, 0, reducedFlag, record._wasmMixerU8);
      if (count > record._wasmMixerF64.length) {
        count = record._wasmMixerF64.length;
      }
    }
    api.wasmDecodePose(record._wasmMixerF64, count, animatedTransforms);
  }

  function sceneApplyModelSkinPose(record, deltaTime, reduced) {
    if (!record || !record.animationApi || !record.nodes || !record.skins) {
      return;
    }
    const animatedTransforms = record.animatedTransforms;
    if (animatedTransforms && typeof animatedTransforms.clear === "function") {
      animatedTransforms.clear();
    }
    if (sceneModelWasmMixerActive(record)) {
      sceneAdvanceWasmModelMixer(record, deltaTime, reduced, animatedTransforms);
    } else if (record.mixer) {
      record.mixer.update(deltaTime, function(targetNode, property, value) {
        let entry = animatedTransforms.get(targetNode);
        if (!entry) {
          entry = {};
          animatedTransforms.set(targetNode, entry);
        }
        entry[property] = Array.isArray(value) ? value.slice() : Array.from(value || []);
      });
    }
    const nodeTransforms = record.animationApi.buildNodeTransforms(record.nodes, animatedTransforms, record.rootTransform, record.rootNodes);
    for (let index = 0; index < record.skins.length; index += 1) {
      const skin = record.skins[index];
      if (!skin) {
        continue;
      }
      skin.jointMatrices = record.animationApi.computeJointMatrices(skin, nodeTransforms);
    }
  }

  function sceneOwns(source, key) {
    return Boolean(source && Object.prototype.hasOwnProperty.call(source, key));
  }

  function sceneAnimationNumber(source, key, fallback, min) {
    if (!sceneOwns(source, key)) {
      return fallback;
    }
    const value = sceneNumber(source[key], fallback);
    return Number.isFinite(value) ? Math.max(min, value) : fallback;
  }

  function sceneAnimationMilliseconds(source, key, fallbackSeconds) {
    if (!sceneOwns(source, key)) {
      return fallbackSeconds;
    }
    const value = Number(source[key]);
    return Number.isFinite(value) ? Math.max(0, value) / 1000 : fallbackSeconds;
  }

  function sceneModelAnimationPlayOptions(model, patch, defaults) {
    const fallbackLoop = defaults && typeof defaults.loop === "boolean" ? defaults.loop : true;
    const modelLoop = sceneOwns(model, "loop") ? model.loop !== false : fallbackLoop;
    const loop = sceneOwns(patch, "loop") ? patch.loop !== false : modelLoop;
    const modelSpeed = sceneAnimationNumber(model, "animationSpeed", defaults && defaults.speed !== undefined ? defaults.speed : 1, 0);
    const modelWeight = sceneAnimationNumber(model, "animationWeight", defaults && defaults.weight !== undefined ? defaults.weight : 1, 0);
    return {
      loop,
      speed: sceneAnimationNumber(patch, "animationSpeed", modelSpeed, 0),
      weight: sceneAnimationNumber(patch, "animationWeight", modelWeight, 0),
      fadeIn: sceneAnimationMilliseconds(
        patch,
        "animationFadeInMS",
        sceneAnimationMilliseconds(model, "animationFadeInMS", defaults && defaults.fadeIn !== undefined ? defaults.fadeIn : 0),
      ),
    };
  }

  function sceneModelAnimationStopOptions(model, patch, defaults) {
    return {
      fadeOut: sceneAnimationMilliseconds(
        patch,
        "animationFadeOutMS",
        sceneAnimationMilliseconds(model, "animationFadeOutMS", defaults && defaults.fadeOut !== undefined ? defaults.fadeOut : 0),
      ),
    };
  }

  function sceneApplyModelAnimationControls(record, patch) {
    if (!record || !record.model || !sceneIsPlainObject(patch)) {
      return;
    }
    const keys = ["loop", "animationSpeed", "animationWeight", "animationFadeInMS", "animationFadeOutMS"];
    for (let index = 0; index < keys.length; index += 1) {
      const key = keys[index];
      if (sceneOwns(patch, key)) {
        record.model[key] = patch[key];
      }
    }
  }

  function sceneRegisterModelAnimationRecord(state, record) {
    if (!state || !record || (!record.mixer && !record.wasmMixerActive)) {
      return;
    }
    if (!Array.isArray(state._modelAnimations)) {
      state._modelAnimations = [];
    }
    if (state._modelAnimations.indexOf(record) < 0) {
      state._modelAnimations.push(record);
    }
  }

  function sceneRegisterStaticModelLiveRecord(state, instanceModel, objectIDs) {
    if (!state || !instanceModel || !Array.isArray(instanceModel._live) || instanceModel._live.length === 0 || !Array.isArray(objectIDs) || objectIDs.length === 0) {
      return;
    }
    const modelCopy = Object.assign({}, instanceModel || {});
    const record = {
      id: typeof modelCopy.id === "string" ? modelCopy.id : "",
      model: modelCopy,
      live: modelCopy._live.slice(),
      objectIDs: objectIDs.slice(),
      rootTransform: sceneModelTransformMatrix(modelCopy),
      animation: "",
      animationSeq: "",
      poseDirty: false,
      staticModel: true,
    };
    if (!Array.isArray(state._modelSkins)) {
      state._modelSkins = [];
    }
    state._modelSkins.push(record);
  }

  async function scenePrepareModelSkinPlayback(state, asset, instanceModel, skinInstances, objectIDs) {
    if (!sceneModelHasSkins(skinInstances) || !Array.isArray(asset.nodes) || !asset.nodes.length) {
      return;
    }

    let animationApi = null;
    try {
      animationApi = await ensureAnimationFeatureLoaded();
    } catch (error) {
      console.warn("[gosx] failed to load Scene3D animation support:", error && error.message ? error.message : error);
      return;
    }
    if (!animationApi || typeof animationApi.buildNodeTransforms !== "function" || typeof animationApi.computeJointMatrices !== "function") {
      return;
    }

    const record = {
      id: typeof instanceModel.id === "string" ? instanceModel.id : "",
      model: Object.assign({}, instanceModel || {}),
      live: Array.isArray(instanceModel && instanceModel._live) ? instanceModel._live.slice() : [],
      objectIDs: Array.isArray(objectIDs) ? objectIDs.slice() : [],
      nodes: asset.nodes,
      rootNodes: sceneModelRootNodes(asset.nodes),
      skins: skinInstances,
      animatedTransforms: new Map(),
      rootTransform: sceneModelTransformMatrix(instanceModel),
      animationApi,
      mixer: null,
      animation: "",
      animationSeq: "",
      poseDirty: false,
    };
    if (!Array.isArray(state._modelSkins)) {
      state._modelSkins = [];
    }
    state._modelSkins.push(record);

    const clips = sceneCloneModelAnimations(asset.animations);
    const wantWasmMixer = clips.length > 0
      && typeof window !== "undefined"
      && window.__gosx_motion_wasm
      && typeof window.__gosx_motion_mixer_create === "function"
      && typeof animationApi.wasmClipJSON === "function";
    if (wantWasmMixer) {
      // P4-M3: route glTF clip playback through the Go WASM motion mixer.
      const handle = window.__gosx_motion_mixer_create();
      if (handle >= 1) {
        record.wasmMixer = handle;
        record.wasmMixerActive = true;
        for (let index = 0; index < clips.length; index += 1) {
          const clip = clips[index];
          window.__gosx_motion_mixer_add_clip(handle, clip.name, animationApi.wasmClipJSON(clip));
        }
        sceneRegisterModelAnimationRecord(state, record);
        const requestedAnimation = typeof instanceModel.animation === "string" ? instanceModel.animation.trim() : "";
        if (requestedAnimation) {
          sceneModelRecordPlay(record, requestedAnimation, sceneModelAnimationPlayOptions(instanceModel, null, { loop: true, speed: 1, weight: 1, fadeIn: 0 }));
          if (sceneModelRecordIsPlaying({ animation: requestedAnimation, wasmMixerActive: true, wasmMixer: handle })) {
            record.animation = requestedAnimation;
            record.animationSeq = typeof instanceModel.animationSeq === "string" ? instanceModel.animationSeq : "";
          }
        }
      }
    } else if (typeof animationApi.createMixer === "function") {
      if (clips.length) {
        const mixer = animationApi.createMixer();
        // Own the mixer before clip registration: addClip is extensible and may
        // throw, so failed transactional staging must still be able to dispose
        // the resource through the already-registered model record.
        record.mixer = mixer;
        for (let index = 0; index < clips.length; index += 1) {
          const clip = clips[index];
          mixer.addClip(clip.name, clip);
        }
        sceneRegisterModelAnimationRecord(state, record);
        const requestedAnimation = typeof instanceModel.animation === "string" ? instanceModel.animation.trim() : "";
        if (requestedAnimation) {
          mixer.play(requestedAnimation, sceneModelAnimationPlayOptions(instanceModel, null, { loop: true, speed: 1, weight: 1, fadeIn: 0 }));
          if (mixer.isPlaying(requestedAnimation)) {
            record.animation = requestedAnimation;
            record.animationSeq = typeof instanceModel.animationSeq === "string" ? instanceModel.animationSeq : "";
          }
        }
      }
    }

    sceneApplyModelSkinPose(record, 0, false);
  }

  // Route a clip play through the active mixer. opts is the JS-mixer options
  // shape ({loop, speed, weight, fadeIn}); the WASM mixer takes the same values
  // as positional arguments.
  function sceneModelRecordPlay(record, name, opts) {
    const options = opts || {};
    if (record && record.wasmMixerActive) {
      if (typeof window !== "undefined" && typeof window.__gosx_motion_mixer_play === "function") {
        window.__gosx_motion_mixer_play(
          record.wasmMixer,
          name,
          options.fadeIn !== undefined ? options.fadeIn : 0,
          options.loop !== undefined ? options.loop !== false : true,
          options.speed !== undefined ? options.speed : 1,
          options.weight !== undefined ? options.weight : 1
        );
      }
      return;
    }
    if (record && record.mixer) {
      record.mixer.play(name, options);
    }
  }

  // Route a clip stop through the active mixer. opts is the JS-mixer options
  // shape ({fadeOut}); the WASM mixer takes fadeOut positionally.
  function sceneModelRecordStop(record, name, opts) {
    const options = opts || {};
    if (record && record.wasmMixerActive) {
      if (typeof window !== "undefined" && typeof window.__gosx_motion_mixer_stop === "function") {
        window.__gosx_motion_mixer_stop(record.wasmMixer, name, options.fadeOut !== undefined ? options.fadeOut : 0);
      }
      return;
    }
    if (record && record.mixer) {
      record.mixer.stop(name, options);
    }
  }

  // Whether a named clip is playing on the record's active mixer, routed to the
  // WASM mixer when active (P4-M3) and the JS mixer otherwise.
  function sceneModelRecordWasPlaying(record, name) {
    if (!record || !name) {
      return false;
    }
    if (record.wasmMixerActive) {
      return Boolean(
        typeof window !== "undefined" &&
        typeof window.__gosx_motion_mixer_is_playing === "function" &&
        window.__gosx_motion_mixer_is_playing(record.wasmMixer, name)
      );
    }
    return Boolean(record.mixer && record.mixer.isPlaying(name));
  }

  // Whether a record's currently-selected animation is playing.
  function sceneModelRecordIsPlaying(record) {
    return record ? sceneModelRecordWasPlaying(record, record.animation) : false;
  }

  function sceneHasActiveModelAnimations(state) {
    const records = state && Array.isArray(state._modelAnimations) ? state._modelAnimations : [];
    return records.some(function(record) {
      return sceneModelRecordIsPlaying(record);
    });
  }

  function sceneAdvanceModelAnimations(state, deltaTime, reduced) {
    const records = state && Array.isArray(state._modelAnimations) ? state._modelAnimations : [];
    for (let index = 0; index < records.length; index += 1) {
      const record = records[index];
      if (!record) {
        continue;
      }
      const playing = sceneModelRecordIsPlaying(record);
      if (!playing && !record.poseDirty) {
        continue;
      }
      record.poseDirty = false;
      sceneApplyModelSkinPose(record, deltaTime, reduced);
    }
  }

  function sceneModelRecordListensToEvent(record, eventName) {
    return Boolean(record && Array.isArray(record.live) && record.live.indexOf(eventName) >= 0);
  }

  function sceneModelLivePatchForRecord(record, payload) {
    if (!sceneIsPlainObject(payload)) {
      return null;
    }
    if (record && record.id && sceneIsPlainObject(payload[record.id])) {
      return payload[record.id];
    }
    return payload;
  }

  function sceneApplyModelLiveOpacity(state, record, patch) {
    if (!state || !record || !sceneIsPlainObject(patch) || !Object.prototype.hasOwnProperty.call(patch, "opacity")) {
      return false;
    }
    const opacity = Math.max(0, Math.min(1, sceneNumber(patch.opacity, sceneNumber(record.model && record.model.opacity, 1))));
    if (record.model) {
      record.model.opacity = opacity;
    }
    const objectIDs = Array.isArray(record.objectIDs) ? record.objectIDs : [];
    let changed = false;
    for (let index = 0; index < objectIDs.length; index += 1) {
      const object = state.objects && state.objects.get ? state.objects.get(objectIDs[index]) : null;
      if (!object || object.opacity === opacity) {
        continue;
      }
      object.opacity = opacity;
      if (opacity < 1 && (!object.blendMode || object.blendMode === "opaque")) {
        object.blendMode = "alpha";
      }
      changed = true;
    }
    return changed;
  }

  function sceneApplyStaticModelObjectTransform(state, record) {
    if (!state || !record || !record.staticModel || !Array.isArray(record.objectIDs)) {
      return false;
    }
    let changed = false;
    for (let index = 0; index < record.objectIDs.length; index += 1) {
      const object = state.objects && state.objects.get ? state.objects.get(record.objectIDs[index]) : null;
      const local = object && object._modelLocalVertices;
      if (!object || !object.vertices || !local || !local.positions) {
        continue;
      }
      object.vertices.positions = sceneModelTransformMeshFloats(local.positions, 3, function(x, y, z) {
        return sceneModelTransformPoint({ x: x, y: y, z: z }, record.model);
      });
      if (local.normals && local.normals.length) {
        object.vertices.normals = sceneModelTransformMeshFloats(local.normals, 3, function(x, y, z) {
          return sceneNormalizeDirection(sceneModelTransformVector({ x: x, y: y, z: z }, record.model));
        });
      }
      if (local.tangents && local.tangents.length) {
        object.vertices.tangents = sceneModelTransformMeshFloats(local.tangents, 4, function(x, y, z, w) {
          const rotated = sceneNormalizeDirection(sceneModelTransformVector({ x: x, y: y, z: z }, record.model));
          return { x: rotated.x, y: rotated.y, z: rotated.z, w: sceneNumber(w, 1) };
        });
      }
      object.vertices.uvs = local.uvs;
      object.vertices.count = local.count;
      object.static = false;
      sceneApplyModelObjectHiddenState(object, record.model);
      changed = true;
    }
    return changed;
  }

  function sceneComputedPoseName(value) {
    const pose = String(value == null ? "" : value).trim();
    switch (pose) {
      case "guard":
      case "strike":
      case "kick":
      case "hit":
      case "down":
      case "surge":
      case "start":
        return pose;
      case "idle":
      default:
        return "idle";
    }
  }

  function sceneComputedPoseBaseID(id) {
    let base = String(id || "").trim();
    const suffixes = ["-guard", "-strike", "-kick", "-hit", "-down", "-surge", "-start"];
    for (let index = 0; index < suffixes.length; index += 1) {
      const suffix = suffixes[index];
      if (base.length > suffix.length && base.slice(base.length - suffix.length) === suffix) {
        base = base.slice(0, base.length - suffix.length);
        break;
      }
    }
    return base;
  }

  function sceneComputedPoseTargetID(recordID, pose) {
    const base = sceneComputedPoseBaseID(recordID);
    const normalized = sceneComputedPoseName(pose);
    if (!base || normalized === "idle") {
      return base;
    }
    return base + "-" + normalized;
  }

  function sceneComputedPoseRecordByID(state, id) {
    const want = String(id || "").trim();
    if (!want) {
      return null;
    }
    const records = Array.isArray(state && state._modelSkins) ? state._modelSkins : [];
    for (let index = 0; index < records.length; index += 1) {
      const record = records[index];
      if (record && String(record.id || "") === want) {
        return record;
      }
    }
    return null;
  }

  function sceneComputedPoseObject(state, id) {
    return state && state.objects && typeof state.objects.get === "function"
      ? state.objects.get(id)
      : null;
  }

  function sceneComputedPoseLocalVertices(object) {
    const local = object && object._modelLocalVertices;
    if (!local || !local.positions || typeof local.positions.length !== "number") {
      return null;
    }
    const count = Math.max(0, Math.floor(sceneNumber(local.count, 0)));
    if (count <= 0 || local.positions.length < count * 3) {
      return null;
    }
    return local;
  }

  function sceneComputedPoseFloat32Array(value) {
    if (!value || typeof value.length !== "number") {
      return null;
    }
    return value instanceof Float32Array ? value : new Float32Array(value);
  }

  function sceneComputedPoseBlendArray(object, cacheKey, source, target, tupleSize, alpha, normalizeVec3) {
    const sourceArray = sceneComputedPoseFloat32Array(source);
    const targetArray = sceneComputedPoseFloat32Array(target || source);
    const width = Math.max(1, Math.floor(sceneNumber(tupleSize, 1)));
    if (!sourceArray || !targetArray || sourceArray.length < width || targetArray.length < width) {
      return null;
    }
    const limit = Math.min(sourceArray.length, targetArray.length);
    let current = object && object[cacheKey];
    if (!current || current.length !== sourceArray.length) {
      current = new Float32Array(sourceArray);
      if (object) {
        object[cacheKey] = current;
      }
    }
    const t = Math.max(0, Math.min(1, sceneNumber(alpha, 0.45)));
    for (let index = 0; index + width - 1 < limit; index += width) {
      for (let component = 0; component < width; component += 1) {
        current[index + component] += (targetArray[index + component] - current[index + component]) * t;
      }
      if (normalizeVec3 && width >= 3) {
        const x = current[index];
        const y = current[index + 1];
        const z = current[index + 2];
        const length = Math.hypot(x, y, z);
        if (length > 0.000001) {
          current[index] = x / length;
          current[index + 1] = y / length;
          current[index + 2] = z / length;
        }
      }
    }
    return current;
  }

  function sceneComputedPoseApplyObjectMorph(object, sourceLocal, targetLocal, model, alpha) {
    if (!object || !object.vertices || !sourceLocal || !targetLocal) {
      return 0;
    }
    const sourceCount = Math.max(0, Math.floor(sceneNumber(sourceLocal.count, 0)));
    const targetCount = Math.max(0, Math.floor(sceneNumber(targetLocal.count, 0)));
    const count = Math.min(sourceCount, targetCount);
    if (count <= 0 || sourceLocal.positions.length < count * 3 || targetLocal.positions.length < count * 3) {
      return 0;
    }
    const sourcePositions = sourceLocal.positions.length === count * 3
      ? sourceLocal.positions
      : sourceLocal.positions.subarray(0, count * 3);
    const targetPositions = targetLocal.positions.length === count * 3
      ? targetLocal.positions
      : targetLocal.positions.subarray(0, count * 3);
    const morphedPositions = sceneComputedPoseBlendArray(object, "_computedPoseLocalPositions", sourcePositions, targetPositions, 3, alpha, false);
    if (!morphedPositions) {
      return 0;
    }

    object.computedMorph = {
      sourcePositions,
      targetPositions,
      sourceNormals: sourceLocal.normals && sourceLocal.normals.length >= count * 3
        ? (sourceLocal.normals.subarray ? sourceLocal.normals.subarray(0, count * 3) : sourceLocal.normals)
        : null,
      targetNormals: targetLocal.normals && targetLocal.normals.length >= count * 3
        ? (targetLocal.normals.subarray ? targetLocal.normals.subarray(0, count * 3) : targetLocal.normals)
        : null,
      sourceTangents: sourceLocal.tangents && sourceLocal.tangents.length >= count * 4
        ? (sourceLocal.tangents.subarray ? sourceLocal.tangents.subarray(0, count * 4) : sourceLocal.tangents)
        : null,
      targetTangents: targetLocal.tangents && targetLocal.tangents.length >= count * 4
        ? (targetLocal.tangents.subarray ? targetLocal.tangents.subarray(0, count * 4) : targetLocal.tangents)
        : null,
      uvs: sourceLocal.uvs,
      count,
      alpha: Math.max(0, Math.min(1, sceneNumber(alpha, 0.45))),
      modelMatrix: sceneModelTransformMatrix(model),
    };

    object.vertices.positions = sceneModelTransformMeshFloats(morphedPositions, 3, function(x, y, z) {
      return sceneModelTransformPoint({ x: x, y: y, z: z }, model);
    });

    const sourceNormals = sourceLocal.normals && sourceLocal.normals.length >= count * 3
      ? sourceLocal.normals.subarray ? sourceLocal.normals.subarray(0, count * 3) : sourceLocal.normals
      : null;
    const targetNormals = targetLocal.normals && targetLocal.normals.length >= count * 3
      ? targetLocal.normals.subarray ? targetLocal.normals.subarray(0, count * 3) : targetLocal.normals
      : null;
    const morphedNormals = sourceNormals
      ? sceneComputedPoseBlendArray(object, "_computedPoseLocalNormals", sourceNormals, targetNormals || sourceNormals, 3, alpha, true)
      : null;
    if (morphedNormals) {
      object.vertices.normals = sceneModelTransformMeshFloats(morphedNormals, 3, function(x, y, z) {
        return sceneNormalizeDirection(sceneModelTransformVector({ x: x, y: y, z: z }, model));
      });
    }

    const sourceTangents = sourceLocal.tangents && sourceLocal.tangents.length >= count * 4
      ? sourceLocal.tangents.subarray ? sourceLocal.tangents.subarray(0, count * 4) : sourceLocal.tangents
      : null;
    const targetTangents = targetLocal.tangents && targetLocal.tangents.length >= count * 4
      ? targetLocal.tangents.subarray ? targetLocal.tangents.subarray(0, count * 4) : targetLocal.tangents
      : null;
    const morphedTangents = sourceTangents
      ? sceneComputedPoseBlendArray(object, "_computedPoseLocalTangents", sourceTangents, targetTangents || sourceTangents, 4, alpha, true)
      : null;
    if (morphedTangents) {
      object.vertices.tangents = sceneModelTransformMeshFloats(morphedTangents, 4, function(x, y, z, w) {
        const rotated = sceneNormalizeDirection(sceneModelTransformVector({ x: x, y: y, z: z }, model));
        return { x: rotated.x, y: rotated.y, z: rotated.z, w: sceneNumber(w, 1) };
      });
    }

    object.vertices.uvs = sourceLocal.uvs;
    object.vertices.count = count;
    object.static = false;
    sceneApplyModelObjectHiddenState(object, model);
    return count;
  }

  function sceneApplyModelComputedPose(state, record, patch) {
    if (!state || !record || !record.staticModel || !sceneOwns(patch, "computedPose")) {
      return false;
    }
    const pose = sceneComputedPoseName(patch.computedPose);
    const alpha = Math.max(0, Math.min(1, sceneNumber(patch.computedPoseAlpha, pose === "idle" ? 0.32 : 0.52)));
    const targetID = sceneComputedPoseTargetID(record.id, pose);
    const targetRecord = targetID === record.id ? record : sceneComputedPoseRecordByID(state, targetID);
    record.computedPose = pose;
    record.computedPoseAlpha = alpha;
    record.computedPoseTargetID = targetID;
    record.computedMorphObjects = 0;
    record.computedMorphVertices = 0;
    if (!targetRecord || !Array.isArray(record.objectIDs) || !Array.isArray(targetRecord.objectIDs)) {
      return false;
    }

    const count = Math.min(record.objectIDs.length, targetRecord.objectIDs.length);
    let changed = false;
    let morphObjects = 0;
    let morphVertices = 0;
    for (let index = 0; index < count; index += 1) {
      const object = sceneComputedPoseObject(state, record.objectIDs[index]);
      const targetObject = sceneComputedPoseObject(state, targetRecord.objectIDs[index]);
      const sourceLocal = sceneComputedPoseLocalVertices(object);
      const targetLocal = sceneComputedPoseLocalVertices(targetObject) || sourceLocal;
      const vertices = sceneComputedPoseApplyObjectMorph(object, sourceLocal, targetLocal, record.model, alpha);
      if (vertices <= 0) {
        continue;
      }
      changed = true;
      morphObjects += 1;
      morphVertices += vertices;
    }
    record.computedMorphObjects = morphObjects;
    record.computedMorphVertices = morphVertices;
    return changed;
  }

  function sceneApplyModelLivePatch(state, record, patch) {
    if (!record || !record.model || !sceneIsPlainObject(patch)) {
      return false;
    }
    const keys = ["x", "y", "z", "rotationX", "rotationY", "rotationZ", "scaleX", "scaleY", "scaleZ"];
    const hasComputedPose = sceneOwns(patch, "computedPose");
    let changed = sceneApplyModelLiveOpacity(state, record, patch);
    if (sceneOwns(patch, "visible")) {
      const nextVisible = sceneBool(patch.visible, true);
      if (record.model.visible !== nextVisible) {
        record.model.visible = nextVisible;
        changed = true;
      }
    }
    for (let index = 0; index < keys.length; index += 1) {
      const key = keys[index];
      if (!Object.prototype.hasOwnProperty.call(patch, key)) {
        continue;
      }
      const next = sceneNumber(patch[key], sceneNumber(record.model[key], key.indexOf("scale") === 0 ? 1 : 0));
      if (record.model[key] === next) {
        continue;
      }
      record.model[key] = next;
      changed = true;
    }
    if (!changed && !hasComputedPose) {
      return false;
    }
    record.rootTransform = sceneModelTransformMatrix(record.model);
    let computedPoseChanged = false;
    if (hasComputedPose) {
      computedPoseChanged = sceneApplyModelComputedPose(state, record, patch);
    }
    if (record.staticModel && !computedPoseChanged) {
      sceneApplyStaticModelObjectTransform(state, record);
    }
    changed = changed || computedPoseChanged;
    if (!changed) {
      return false;
    }
    record.poseDirty = true;
    return true;
  }

  function sceneApplyModelLiveAnimation(record, patch) {
    if (!record || (!record.mixer && !record.wasmMixerActive) || !sceneIsPlainObject(patch)) {
      return false;
    }
    const hasAnimation = sceneOwns(patch, "animation");
    const hasControls = sceneOwns(patch, "loop")
      || sceneOwns(patch, "animationSpeed")
      || sceneOwns(patch, "animationWeight")
      || sceneOwns(patch, "animationFadeInMS")
      || sceneOwns(patch, "animationFadeOutMS");
    if (!hasAnimation && !hasControls) {
      return false;
    }
    const animation = hasAnimation
      ? (typeof patch.animation === "string" ? patch.animation.trim() : "")
      : record.animation;
    const hasSeq = sceneOwns(patch, "animationSeq");
    const animationSeq = hasSeq ? String(patch.animationSeq == null ? "" : patch.animationSeq) : "";
    const replay = Boolean(hasSeq && animationSeq && record.animation === animation && record.animationSeq !== animationSeq);
    sceneApplyModelAnimationControls(record, patch);
    if (!animation) {
      if (record.animation && sceneModelRecordIsPlaying(record)) {
        const stopOptions = sceneModelAnimationStopOptions(record.model, patch, { fadeOut: 0.05 });
        sceneModelRecordStop(record, record.animation, stopOptions);
        if (stopOptions.fadeOut <= 0) {
          record.animation = "";
        }
      }
      record.animationSeq = animationSeq;
      record.poseDirty = true;
      return true;
    }
    if (record.animation === animation && sceneModelRecordIsPlaying(record) && !replay) {
      if (hasControls) {
        sceneModelRecordPlay(record, animation, sceneModelAnimationPlayOptions(record.model, patch, { loop: true, speed: 1, weight: 1, fadeIn: 0 }));
        record.poseDirty = true;
        return true;
      }
      return false;
    }
    if (record.animation && sceneModelRecordIsPlaying(record)) {
      sceneModelRecordStop(record, record.animation, sceneModelAnimationStopOptions(record.model, patch, { fadeOut: replay ? 0 : 0.05 }));
    }
    sceneModelRecordPlay(record, animation, sceneModelAnimationPlayOptions(record.model, patch, { loop: true, speed: 1, weight: 1, fadeIn: replay ? 0 : 0.04 }));
    if (!sceneModelRecordWasPlaying(record, animation)) {
      return false;
    }
    record.animation = animation;
    record.animationSeq = animationSeq;
    record.poseDirty = true;
    return true;
  }

  function sceneApplyModelLiveEvent(state, eventName, payload) {
    const event = typeof eventName === "string" ? eventName.trim() : "";
    if (!event) {
      return false;
    }
    const records = state && Array.isArray(state._modelSkins) ? state._modelSkins : [];
    let changed = false;
    for (let index = 0; index < records.length; index += 1) {
      const record = records[index];
      if (!sceneModelRecordListensToEvent(record, event)) {
        continue;
      }
      const patch = sceneModelLivePatchForRecord(record, payload);
      changed = sceneApplyModelLivePatch(state, record, patch) || changed;
      changed = sceneApplyModelLiveAnimation(record, patch) || changed;
    }
    return changed;
  }

  function sceneApplyCameraLiveEvent(state, payload) {
    if (!state || !sceneIsPlainObject(payload) || !sceneIsPlainObject(payload.camera)) {
      return false;
    }
    const nextCamera = normalizeSceneCamera(payload.camera, state.camera);
    if (sceneCameraEquivalent(state.camera, nextCamera)) {
      return false;
    }
    state.camera = nextCamera;
    return true;
  }

  function sceneHydrationModels(state, props) {
    const models = Array.isArray(state && state.models) ? state.models : sceneModels(props);
    const instancedGLBMeshes = Array.isArray(state && state.instancedGLBMeshes)
      ? state.instancedGLBMeshes
      : sceneInstancedGLBMeshes(props);
    return models.concat(sceneInstancedGLBModelsFromBatches(instancedGLBMeshes));
  }

  function sceneClearHydratedModelRecords(state) {
    if (!state || !state._hydratedModelRecords) {
      return;
    }
    const records = state._hydratedModelRecords;
    for (const id of (Array.isArray(records.objects) ? records.objects : [])) {
      state.objects.delete(sceneObjectKey(id));
    }
    for (const id of (Array.isArray(records.labels) ? records.labels : [])) {
      state.labels.delete(sceneObjectKey(id));
    }
    for (const id of (Array.isArray(records.sprites) ? records.sprites : [])) {
      state.sprites.delete(sceneObjectKey(id));
    }
    for (const id of (Array.isArray(records.html) ? records.html : [])) {
      state.html.delete(sceneObjectKey(id));
    }
    for (const id of (Array.isArray(records.lights) ? records.lights : [])) {
      state.lights.delete(sceneObjectKey(id));
    }
    if (Array.isArray(records.points) && records.points.length > 0 && Array.isArray(state.points)) {
      const pointIDs = new Set(records.points.map(function(id) { return sceneObjectKey(id); }));
      state.points = state.points.filter(function(point) {
        return !pointIDs.has(sceneObjectKey(point && point.id));
      });
    }
    state._hydratedModelRecords = null;
  }

  // Free motion mixers attached to model records before they are dropped
  // (re-hydration, failed staging, supersession or teardown). The name is kept
  // for compatibility with source-level consumers from the original WASM-only
  // cleanup, but JS mixers are resources too and must follow the same lifetime.
  function sceneDestroyModelWasmMixers(records) {
    if (!Array.isArray(records)) {
      return;
    }
    for (let index = 0; index < records.length; index += 1) {
      const record = records[index];
      if (!record) {
        continue;
      }
      if (record.wasmMixer && typeof window !== "undefined" && typeof window.__gosx_motion_mixer_destroy === "function") {
        window.__gosx_motion_mixer_destroy(record.wasmMixer);
        record.wasmMixer = 0;
        record.wasmMixerActive = false;
      }
      if (record.mixer && typeof record.mixer.dispose === "function") {
        record.mixer.dispose();
        record.mixer = null;
      }
    }
  }

  function sceneModelHydrationCounts(modelCount) {
    return {
      models: Math.max(0, Math.floor(sceneNumber(modelCount, 0))),
      objects: 0,
      points: 0,
      labels: 0,
      sprites: 0,
      html: 0,
      lights: 0,
    };
  }

  function sceneModelHydrationOutcome(counts, generation, outcome, committed, stale, failureStage) {
    return Object.assign({}, counts, {
      generation,
      outcome: String(outcome || ""),
      committed: Boolean(committed),
      stale: Boolean(stale),
      failureStage: String(failureStage || ""),
    });
  }

  async function sceneStageModelHydration(state, model, modelIndex, generation) {
    const staged = {
      model,
      modelIndex,
      objects: [],
      points: [],
      labels: [],
      sprites: [],
      html: [],
      lights: [],
      modelAnimations: [],
      modelSkins: [],
    };
    const stageState = {
      _modelAnimations: staged.modelAnimations,
      _modelSkins: staged.modelSkins,
    };
    let stage = "load";
    try {
      const asset = await loadSceneModelAsset(model.src, state && state._modelStatusMount, {
        state,
        generation,
        modelID: model.id || "",
        modelIndex,
        stage: "load",
        variantScope: state && state._modelTextureVariantScope,
      });
      // A newer command already owns the scene. Avoid needless instantiation
      // and mixer creation; the terminal generation check still fences the
      // whole batch in case supersession happens later.
      if (Number(state._modelHydrationGeneration) !== Number(generation)) {
        return { ok: true, staged };
      }
      stage = "fit";
      const instanceModel = sceneModelWithAssetFit(model, asset);
      const prefix = model.id || ("scene-model-" + modelIndex);
      stage = "skin-clone";
      const skinInstances = sceneCloneModelSkins(asset.skins);
      const objectIDs = [];
      stage = "object";
      for (let i = 0; i < asset.objects.length; i += 1) {
        const object = sceneInstantiateModelObject(asset.objects[i], instanceModel, prefix, i, skinInstances);
        if (!object) {
          continue;
        }
        staged.objects.push(object);
        objectIDs.push(object.id);
      }
      stage = "points";
      for (let i = 0; i < asset.points.length; i += 1) {
        const point = sceneInstantiateModelPointsEntry(asset.points[i], instanceModel, prefix, i);
        if (point && point.count > 0) {
          staged.points.push(point);
        }
      }
      stage = "label";
      for (let i = 0; i < asset.labels.length; i += 1) {
        const label = sceneInstantiateModelLabel(asset.labels[i], instanceModel, prefix, i);
        if (label && label.text.trim()) {
          staged.labels.push(label);
        }
      }
      stage = "sprite";
      for (let i = 0; i < asset.sprites.length; i += 1) {
        const sprite = sceneInstantiateModelSprite(asset.sprites[i], instanceModel, prefix, i);
        if (sprite) {
          staged.sprites.push(sprite);
        }
      }
      stage = "html";
      for (let i = 0; i < asset.html.length; i += 1) {
        const entry = sceneInstantiateModelHTML(asset.html[i], instanceModel, prefix, i);
        if (entry) {
          staged.html.push(entry);
        }
      }
      stage = "light";
      for (let i = 0; i < asset.lights.length; i += 1) {
        const light = sceneInstantiateModelLight(asset.lights[i], instanceModel, prefix, i);
        if (light) {
          staged.lights.push(light);
        }
      }
      stage = "skin";
      if (sceneModelHasSkins(skinInstances)) {
        await scenePrepareModelSkinPlayback(stageState, asset, instanceModel, skinInstances, objectIDs);
      } else {
        sceneRegisterStaticModelLiveRecord(stageState, instanceModel, objectIDs);
      }
      return { ok: true, staged };
    } catch (error) {
      return { ok: false, staged, stage, error };
    }
  }

  function sceneDestroyStagedModelHydrations(results) {
    for (let index = 0; index < results.length; index += 1) {
      const staged = results[index] && results[index].staged;
      if (staged) {
        sceneDestroyModelWasmMixers(staged.modelSkins);
      }
    }
  }

  async function hydrateSceneStateModels(state, props) {
    if (!state) {
      return sceneModelHydrationOutcome(sceneModelHydrationCounts(0), 0, "failed", false, false, "state");
    }
    const generation = Math.max(0, Math.floor(sceneNumber(state._modelHydrationGeneration, 0))) + 1;
    state._modelHydrationGeneration = generation;
    let models;
    try {
      // Commands can replace the declaration arrays while their assets are in
      // flight. Clone the fully-expanded list once so this generation has an
      // immutable, deterministic declaration order.
      models = sceneHydrationModels(state, props).map(sceneCloneData);
    } catch (error) {
      const counts = sceneModelHydrationCounts(0);
      publishSceneModelHydrationStatus(state._modelStatusMount, "failed", {
        generation,
        currentGeneration: state._modelHydrationGeneration,
        committed: false,
        stage: "declarations",
        error: error && error.message ? error.message : error,
        counts,
      });
      return sceneModelHydrationOutcome(counts, generation, "failed", false, false, "declarations");
    }

    const counts = sceneModelHydrationCounts(models.length);
    publishSceneModelHydrationStatus(state._modelStatusMount, "loading", {
      generation,
      currentGeneration: generation,
      committed: false,
      counts,
    });
    if (!models.length) {
      sceneDestroyModelWasmMixers(state._modelSkins);
      sceneClearHydratedModelRecords(state);
      state._modelAnimations = [];
      state._modelSkins = [];
      publishSceneModelHydrationStatus(state._modelStatusMount, "committed", {
        generation,
        currentGeneration: generation,
        committed: true,
        counts,
      });
      gosxSceneEmit("info", "model-hydration-committed", {
        generation,
        committed: true,
        stale: false,
        models: 0,
      });
      return sceneModelHydrationOutcome(counts, generation, "committed", true, false, "");
    }

    const results = await Promise.all(models.map(function(model, modelIndex) {
      return sceneStageModelHydration(state, model, modelIndex, generation);
    }));

    if (Number(state._modelHydrationGeneration) !== Number(generation)) {
      sceneDestroyStagedModelHydrations(results);
      gosxSceneEmit("info", "model-hydration-stale", {
        generation,
        currentGeneration: state._modelHydrationGeneration,
        committed: false,
        stale: true,
      });
      return sceneModelHydrationOutcome(counts, generation, "stale", false, true, "");
    }

    const failure = results.find(function(result) { return !result || result.ok !== true; });
    if (failure) {
      sceneDestroyStagedModelHydrations(results);
      const failedStage = failure && failure.stage ? failure.stage : "unknown";
      const failedError = failure && failure.error;
      const failedStaged = failure && failure.staged;
      publishSceneModelHydrationStatus(state._modelStatusMount, "failed", {
        generation,
        currentGeneration: generation,
        committed: false,
        stage: failedStage,
        modelID: failedStaged && failedStaged.model ? failedStaged.model.id : "",
        modelIndex: failedStaged ? failedStaged.modelIndex : 0,
        asset: failedStaged && failedStaged.model ? failedStaged.model.src : "",
        error: failedError && failedError.message ? failedError.message : failedError,
        counts,
      });
      console.warn("[gosx] Scene3D model hydration failed during " + failedStage + ":",
        failedError && failedError.message ? failedError.message : failedError);
      gosxSceneEmit("warn", "model-hydration-failed", {
        generation,
        committed: false,
        stale: false,
        stage: failedStage,
        modelID: failedStaged && failedStaged.model ? String(failedStaged.model.id || "") : "",
        asset: failedStaged && failedStaged.model ? String(failedStaged.model.src || "") : "",
        error: failedError && failedError.message ? String(failedError.message) : String(failedError || ""),
      });
      return sceneModelHydrationOutcome(counts, generation, "failed", false, false, failedStage);
    }

    // The entire generation is ready and still current. Replace the previous
    // model-derived records in one synchronous turn, preserving declaration
    // order regardless of network completion order.
    sceneDestroyModelWasmMixers(state._modelSkins);
    sceneClearHydratedModelRecords(state);
    state._modelAnimations = [];
    state._modelSkins = [];
    const hydrated = { objects: [], points: [], labels: [], sprites: [], html: [], lights: [] };
    for (let modelIndex = 0; modelIndex < results.length; modelIndex += 1) {
      const staged = results[modelIndex].staged;
      for (let index = 0; index < staged.objects.length; index += 1) {
        const object = staged.objects[index];
        state.objects.set(object.id, object);
        hydrated.objects.push(object.id);
      }
      for (let index = 0; index < staged.points.length; index += 1) {
        const point = staged.points[index];
        state.points.push(point);
        hydrated.points.push(point.id);
      }
      for (let index = 0; index < staged.labels.length; index += 1) {
        const label = staged.labels[index];
        state.labels.set(label.id, label);
        hydrated.labels.push(label.id);
      }
      for (let index = 0; index < staged.sprites.length; index += 1) {
        const sprite = staged.sprites[index];
        state.sprites.set(sprite.id, sprite);
        hydrated.sprites.push(sprite.id);
      }
      for (let index = 0; index < staged.html.length; index += 1) {
        const entry = staged.html[index];
        state.html.set(entry.id, entry);
        hydrated.html.push(entry.id);
      }
      for (let index = 0; index < staged.lights.length; index += 1) {
        const light = staged.lights[index];
        state.lights.set(light.id, light);
        hydrated.lights.push(light.id);
      }
      Array.prototype.push.apply(state._modelAnimations, staged.modelAnimations);
      Array.prototype.push.apply(state._modelSkins, staged.modelSkins);
    }
    state._hydratedModelRecords = hydrated;
    counts.objects = hydrated.objects.length;
    counts.points = hydrated.points.length;
    counts.labels = hydrated.labels.length;
    counts.sprites = hydrated.sprites.length;
    counts.html = hydrated.html.length;
    counts.lights = hydrated.lights.length;
    publishSceneModelHydrationStatus(state._modelStatusMount, "committed", {
      generation,
      currentGeneration: generation,
      committed: true,
      counts,
    });
    gosxSceneEmit("info", "model-hydration-committed", Object.assign({
      generation,
      committed: true,
      stale: false,
    }, counts));
    return sceneModelHydrationOutcome(counts, generation, "committed", true, false, "");
  }

  function normalizeSceneCapabilityTier(value) {
    switch (String(value || "").trim().toLowerCase()) {
      case "constrained":
      case "balanced":
      case "full":
        return String(value).trim().toLowerCase();
      default:
        return "";
    }
  }

  function sceneMediaQueryMatches(query) {
    if (!query || typeof window.matchMedia !== "function") {
      return false;
    }
    try {
      return Boolean(window.matchMedia(query).matches);
    } catch (_error) {
      return false;
    }
  }

  function sceneEnvironmentState() {
    if (window.__gosx
      && window.__gosx.environment
      && typeof window.__gosx.environment.get === "function") {
      return window.__gosx.environment.get();
    }
    return null;
  }

  // sceneExtractCSSVarTransitionTiming scans the original props for materials
  // or environment with a transition config and returns the first timing found.
  // This is stashed on the mount element so the planner can use it as a default
  // when CSS var values change.
  function sceneExtractCSSVarTransitionTiming(props) {
    var scene = props && props.scene;
    if (!scene || typeof scene !== "object") return null;
    var materials = Array.isArray(scene.materials) ? scene.materials : [];
    for (var i = 0; i < materials.length; i++) {
      var m = materials[i];
      if (m && m.transition && typeof m.transition === "object") {
        var update = m.transition.update || m.transition;
        var duration = typeof update.duration === "number" ? update.duration
          : typeof update.duration === "string" ? parseFloat(update.duration) * (update.duration.indexOf("ms") >= 0 ? 1 : 1000)
          : 0;
        if (duration > 0) {
          return { duration: duration, easing: update.easing || "ease-in-out" };
        }
      }
    }
    var env = scene.environment;
    if (env && env.transition && typeof env.transition === "object") {
      var envUpdate = env.transition.update || env.transition;
      var envDuration = typeof envUpdate.duration === "number" ? envUpdate.duration : 0;
      if (envDuration > 0) {
        return { duration: envDuration, easing: envUpdate.easing || "ease-in-out" };
      }
    }
    return null;
  }

  function sceneCapabilityProfile(props) {
    const requestedTier = normalizeSceneCapabilityTier(props && props.capabilityTier);
    const environment = sceneEnvironmentState();
    const navigatorRef = window && window.navigator ? window.navigator : {};
    const webglProbe = sceneBool(props && props.preferWebGL, true) ? sceneProbeWebGLRenderer() : null;
    const softwareWebGL = Boolean(webglProbe && webglProbe.available && webglProbe.software);
    const coarsePointer = environment ? Boolean(environment.coarsePointer) : (sceneMediaQueryMatches("(pointer: coarse)") || sceneMediaQueryMatches("(any-pointer: coarse)"));
    const hover = environment ? Boolean(environment.hover) : (sceneMediaQueryMatches("(hover: hover)") || sceneMediaQueryMatches("(any-hover: hover)"));
    const reducedData = environment ? Boolean(environment.reducedData) : sceneMediaQueryMatches("(prefers-reduced-data: reduce)");
    const lowPower = (environment ? Boolean(environment.lowPower) : false) || softwareWebGL;
    const visualViewportActive = environment ? Boolean(environment.visualViewportActive) : Boolean(window.visualViewport);
    const deviceMemory = sceneNumber(environment && environment.deviceMemory, sceneNumber(navigatorRef && navigatorRef.deviceMemory, 0));
    const hardwareConcurrency = Math.max(0, Math.floor(sceneNumber(environment && environment.hardwareConcurrency, sceneNumber(navigatorRef && navigatorRef.hardwareConcurrency, 0))));
    // Device-capability gate via the single source of truth gosxLowEndHardware
    // (05-document-env), preferring the value already computed in the environment
    // snapshot. This is what previously drifted (OR vs AND) and throttled capable
    // phones to the low-power GPU; deriving it from one helper prevents recurrence.
    const lowEndHardware = environment && typeof environment.lowEndHardware === "boolean"
      ? environment.lowEndHardware
      : gosxLowEndHardware(deviceMemory, hardwareConcurrency);
    const constrainedHardware = lowPower || reducedData || lowEndHardware;

    let tier = requestedTier;
    if (!tier) {
      if ((coarsePointer && constrainedHardware) || reducedData || lowPower) {
        tier = "constrained";
      } else if (coarsePointer) {
        tier = "balanced";
      } else {
        tier = "full";
      }
    }

    return {
      tier,
      coarsePointer,
      hover,
      reducedData,
      lowPower,
      softwareWebGL,
      visualViewportActive,
      deviceMemory,
      hardwareConcurrency,
    };
  }

  function sceneCapabilityWebGLPreference(props, capability) {
    if (sceneRequiresWebGL(props) || sceneForcesWebGL(props)) {
      return "force";
    }
    if (!sceneBool(props && props.preferWebGL, true)) {
      return "disabled";
    }
    if (sceneBool(props && props.preferCanvas, false)) {
      return "avoid";
    }
    if (!capability) {
      return "prefer";
    }
    if (capability.reducedData || capability.lowPower) {
      return "avoid";
    }
    if (capability.tier === "constrained" && capability.coarsePointer) {
      return "avoid";
    }
    return "prefer";
  }

  function sceneCapabilityWebGPUPreference(props, capability) {
    if (sceneRequiresWebGL(props) || sceneForcesWebGL(props) || sceneBool(props && props.preferCanvas, false)) {
      return "disabled";
    }
    if (scenePrefersWebGPU(props)) {
      return "prefer";
    }
    return sceneCapabilityWebGLPreference(props, capability) === "prefer" ? "prefer" : "avoid";
  }

  function sceneRendererFallbackReason(props, capability, rendererKind) {
    if (rendererKind === "webgl") {
      return "";
    }
    if (sceneCapabilityWebGPUPreference(props, capability) === "prefer") {
      return sceneNeedsWebGLForWebGPUCoverage(props) ? "webgpu-feature-gap" : "webgpu-unavailable";
    }
    switch (sceneCapabilityWebGLPreference(props, capability)) {
      case "disabled":
        return "webgl-disabled";
      case "avoid":
        return "environment-constrained";
      default:
        return sceneBool(props && props.preferWebGL, true) ? "webgl-unavailable" : "";
    }
  }

  function sceneCapabilityChanged(prev, next) {
    if (!prev || !next) {
      return true;
    }
    return prev.tier !== next.tier
      || prev.coarsePointer !== next.coarsePointer
      || prev.hover !== next.hover
      || prev.reducedData !== next.reducedData
      || prev.lowPower !== next.lowPower
      || prev.softwareWebGL !== next.softwareWebGL
      || prev.visualViewportActive !== next.visualViewportActive
      || prev.deviceMemory !== next.deviceMemory
      || prev.hardwareConcurrency !== next.hardwareConcurrency;
  }

  function defaultSceneMaxDevicePixelRatio(capability) {
    if (capability && (capability.reducedData || capability.lowPower)) {
      switch (capability.tier) {
        case "constrained":
          return 1.25;
        case "balanced":
          return 1.5;
        default:
          return 1.75;
      }
    }
    switch (capability && capability.tier) {
      case "constrained":
        return 1.5;
      case "balanced":
        return 1.75;
      default:
        return 2;
    }
  }

  function applySceneCapabilityState(mount, props, capability) {
    if (!mount || !capability) {
      return;
    }
    setAttrValue(mount, "data-gosx-scene3d-capability-tier", capability.tier);
    setAttrValue(mount, "data-gosx-scene3d-coarse-pointer", capability.coarsePointer ? "true" : "false");
    setAttrValue(mount, "data-gosx-scene3d-hover", capability.hover ? "true" : "false");
    setAttrValue(mount, "data-gosx-scene3d-reduced-data", capability.reducedData ? "true" : "false");
    setAttrValue(mount, "data-gosx-scene3d-low-power", capability.lowPower ? "true" : "false");
    setAttrValue(mount, "data-gosx-scene3d-software-webgl", capability.softwareWebGL ? "true" : "false");
    setAttrValue(mount, "data-gosx-scene3d-require-webgl", sceneRequiresWebGL(props) ? "true" : "false");
    setAttrValue(mount, "data-gosx-scene3d-visual-viewport", capability.visualViewportActive ? "true" : "false");
    setAttrValue(mount, "data-gosx-scene3d-webgl-preference", sceneCapabilityWebGLPreference(props, capability));
    setAttrValue(mount, "data-gosx-scene3d-webgpu-preference", sceneCapabilityWebGPUPreference(props, capability));
    setAttrValue(mount, "data-gosx-scene3d-device-memory", capability.deviceMemory > 0 ? capability.deviceMemory : "");
    setAttrValue(mount, "data-gosx-scene3d-hardware-concurrency", capability.hardwareConcurrency > 0 ? capability.hardwareConcurrency : "");
  }

  function sceneWebGPULimitKey(name) {
    return String(name || "").trim().toLowerCase().replace(/[^a-z0-9]/g, "");
  }

  function sceneWebGPULimitValue(limits, name) {
    if (!limits || typeof limits !== "object") {
      return "";
    }
    const wanted = sceneWebGPULimitKey(name);
    for (const key of Object.keys(limits)) {
      if (sceneWebGPULimitKey(key) !== wanted) {
        continue;
      }
      const value = Number(limits[key]);
      return Number.isFinite(value) ? value : "";
    }
    return "";
  }

  function sceneWebGPULimitList(limits) {
    if (!limits || typeof limits !== "object") {
      return "";
    }
    return Object.keys(limits).sort().map(function(key) {
      const value = Number(limits[key]);
      return Number.isFinite(value) ? key + "=" + value : "";
    }).filter(Boolean).join(",");
  }

  // sceneRenderTruthAPI resolves the shared render-truth helpers. Returns null
  // when 15a-scene-postfx-shared.ts is absent, and every caller null-checks.
  function sceneRenderTruthAPI() {
    if (typeof window !== "undefined" && window && window.__gosx_scene3d_render_truth_api) {
      return window.__gosx_scene3d_render_truth_api;
    }
    return null;
  }

  // sceneRenderBackendTruth builds the SINGLE machine-readable record of which
  // backend actually runs and why anything else was rejected.
  //
  // Before this, answering "why is this page on WebGL?" meant correlating
  // data-gosx-scene3d-backend, -renderer-fallback, -software-webgl,
  // -webgpu-preference, -dropped and the WebGPU probe's internal error string
  // across two files. Each of those is individually true and individually
  // insufficient; three of them read "healthy" during the incidents this work
  // came out of. One JSON blob with the adapter, the WGSL implementation
  // (Dawn/Tint versus wgpu/naga), the fallback reason and the transition
  // journal is what a probe or a deploy gate can actually assert against.
  function sceneRenderBackendTruth(mount, renderer, fallbackReason, degraded) {
    const api = sceneRenderTruthAPI();
    const kind = renderer && renderer.kind ? renderer.kind : "";
    const diag = renderer && typeof renderer.diagnostics === "function" ? renderer.diagnostics() : null;
    const adapterInfo = diag && diag.adapterInfo ? diag.adapterInfo : {};
    const truth = {
      backend: kind,
      // gpu is the assertion a deploy gate wants: did a shader run at all?
      // A canvas2d mount runs none, which is how a dead post pass shipped
      // behind a green "visual smoke passed" check for a week.
      gpu: kind === "webgpu" || kind === "webgl",
      fallbackReason: fallbackReason || "",
      dropped: Array.isArray(degraded) ? degraded.slice() : [],
      implementation: api ? api.implementation(adapterInfo) : "",
      browserEngine: api && typeof api.browserEngine === "function" ? api.browserEngine() : "",
      adapter: api && typeof api.adapterLabel === "function" ? api.adapterLabel(adapterInfo) : "",
      adapterInfo: {
        vendor: adapterInfo.vendor || "",
        architecture: adapterInfo.architecture || "",
        device: adapterInfo.device || "",
        description: adapterInfo.description || "",
      },
      deviceLost: !!(diag && diag.deviceLost),
      initError: (diag && diag.initError) || "",
      lastError: (diag && diag.lastError) || "",
      shaderDiagnostics: (diag && diag.shaderDiagnostics) || { messages: 0, errors: 0 },
      events: api && typeof api.events === "function" ? api.events() : [],
    };
    if (mount && typeof mount.setAttribute === "function") {
      let encoded = "";
      try {
        encoded = JSON.stringify(truth);
      } catch (_err) {
        encoded = "";
      }
      setAttrValue(mount, "data-gosx-scene3d-render-backend-truth", encoded);
      setAttrValue(mount, "data-gosx-scene3d-render-gpu", truth.gpu ? "true" : "false");
      setAttrValue(mount, "data-gosx-scene3d-render-implementation", truth.implementation);
      mount.__gosxScene3DRenderBackendTruth = truth;
    }
    return truth;
  }

  function applySceneRendererState(mount, renderer, fallbackReason, degraded) {
    if (!mount) {
      return;
    }
    const chosenBackend = renderer && renderer.kind ? renderer.kind : "";
    setAttrValue(mount, "data-gosx-scene3d-renderer", chosenBackend);
    setAttrValue(mount, "data-gosx-scene3d-renderer-fallback", fallbackReason || "");
    // data-gosx-scene3d-backend mirrors the renderer kind (canonical chosen backend name).
    setAttrValue(mount, "data-gosx-scene3d-backend", chosenBackend);
    // Journal every backend selection and swap with a timestamp. A final-state
    // attribute cannot describe "mounted on WebGPU, device died at t=8.2s,
    // continued on WebGL"; an ordered log can, and that sequence is the shape
    // of the intermittent, environment-specific defects that cost the most time.
    const truthAPI = sceneRenderTruthAPI();
    if (truthAPI && typeof truthAPI.record === "function") {
      truthAPI.record("backend", chosenBackend + (fallbackReason ? " fallback=" + fallbackReason : ""));
    }
    sceneRenderBackendTruth(mount, renderer, fallbackReason, degraded);
    // data-gosx-scene3d-dropped lists features skipped per the backendCaps degraded verdict.
    setAttrValue(mount, "data-gosx-scene3d-dropped",
      Array.isArray(degraded) && degraded.length > 0 ? degraded.join(",") : "");
    const webgpuDiagnostics = renderer && renderer.kind === "webgpu" && typeof renderer.diagnostics === "function"
      ? renderer.diagnostics()
      : null;
    const webgpuAdapterLimits = webgpuDiagnostics && webgpuDiagnostics.adapterLimits ? webgpuDiagnostics.adapterLimits : null;
    const webgpuDeviceLimits = webgpuDiagnostics && webgpuDiagnostics.deviceLimits ? webgpuDiagnostics.deviceLimits : null;
    const webgpuRequiredLimits = webgpuDiagnostics && webgpuDiagnostics.requiredLimits ? webgpuDiagnostics.requiredLimits : null;
    setAttrValue(mount, "data-gosx-scene3d-webgpu-features", webgpuDiagnostics && Array.isArray(webgpuDiagnostics.requestedFeatures) ? webgpuDiagnostics.requestedFeatures.join(",") : "");
    setAttrValue(mount, "data-gosx-scene3d-webgpu-required-features", webgpuDiagnostics && Array.isArray(webgpuDiagnostics.requiredFeatures) ? webgpuDiagnostics.requiredFeatures.join(",") : "");
    setAttrValue(mount, "data-gosx-scene3d-webgpu-device-features", webgpuDiagnostics && Array.isArray(webgpuDiagnostics.deviceFeatures) ? webgpuDiagnostics.deviceFeatures.join(",") : "");
    setAttrValue(mount, "data-gosx-scene3d-webgpu-required-limits", sceneWebGPULimitList(webgpuRequiredLimits));
    setAttrValue(mount, "data-gosx-scene3d-webgpu-sample-count", webgpuDiagnostics && webgpuDiagnostics.activeSampleCount > 0 ? webgpuDiagnostics.activeSampleCount : "");
    setAttrValue(mount, "data-gosx-scene3d-webgpu-target-format", webgpuDiagnostics && webgpuDiagnostics.targetFormat ? webgpuDiagnostics.targetFormat : "");
    setAttrValue(mount, "data-gosx-scene3d-webgpu-presentation-alpha-mode", webgpuDiagnostics && webgpuDiagnostics.presentationAlphaMode ? webgpuDiagnostics.presentationAlphaMode : "");
    setAttrValue(mount, "data-gosx-scene3d-webgpu-presentation-color-space", webgpuDiagnostics && webgpuDiagnostics.presentationColorSpace ? webgpuDiagnostics.presentationColorSpace : "");
    setAttrValue(mount, "data-gosx-scene3d-webgpu-presentation-tone-mapping", webgpuDiagnostics && webgpuDiagnostics.presentationToneMappingMode ? webgpuDiagnostics.presentationToneMappingMode : "");
    setAttrValue(mount, "data-gosx-scene3d-webgpu-power-preference", webgpuDiagnostics && webgpuDiagnostics.powerPreference ? webgpuDiagnostics.powerPreference : "");
    setAttrValue(mount, "data-gosx-scene3d-webgpu-adapter-limits", sceneWebGPULimitList(webgpuAdapterLimits));
    setAttrValue(mount, "data-gosx-scene3d-webgpu-device-limits", sceneWebGPULimitList(webgpuDeviceLimits));
    setAttrValue(mount, "data-gosx-scene3d-webgpu-adapter-max-texture-2d", sceneWebGPULimitValue(webgpuAdapterLimits, "maxTextureDimension2D"));
    setAttrValue(mount, "data-gosx-scene3d-webgpu-device-max-texture-2d", sceneWebGPULimitValue(webgpuDeviceLimits, "maxTextureDimension2D"));
    setAttrValue(mount, "data-gosx-scene3d-webgpu-adapter-max-buffer-size", sceneWebGPULimitValue(webgpuAdapterLimits, "maxBufferSize"));
    setAttrValue(mount, "data-gosx-scene3d-webgpu-device-max-buffer-size", sceneWebGPULimitValue(webgpuDeviceLimits, "maxBufferSize"));
    setAttrValue(mount, "data-gosx-scene3d-webgpu-adapter-max-compute-workgroup-size-x", sceneWebGPULimitValue(webgpuAdapterLimits, "maxComputeWorkgroupSizeX"));
    setAttrValue(mount, "data-gosx-scene3d-webgpu-device-max-compute-workgroup-size-x", sceneWebGPULimitValue(webgpuDeviceLimits, "maxComputeWorkgroupSizeX"));
    setAttrValue(mount, "data-gosx-scene3d-webgpu-adapter-max-compute-workgroups-per-dimension", sceneWebGPULimitValue(webgpuAdapterLimits, "maxComputeWorkgroupsPerDimension"));
    setAttrValue(mount, "data-gosx-scene3d-webgpu-device-max-compute-workgroups-per-dimension", sceneWebGPULimitValue(webgpuDeviceLimits, "maxComputeWorkgroupsPerDimension"));
    setAttrValue(mount, "data-gosx-scene3d-webgpu-adapter", webgpuDiagnostics && webgpuDiagnostics.adapterInfo ? [
      webgpuDiagnostics.adapterInfo.vendor || "",
      webgpuDiagnostics.adapterInfo.architecture || "",
      webgpuDiagnostics.adapterInfo.device || "",
    ].filter(Boolean).join(" ") : "");
    sceneSyncStatusBindings(mount);
  }

  function showSceneRequiredRendererMessage(mount, props, reason) {
    if (!mount || typeof document === "undefined" || !document || typeof document.createElement !== "function") {
      return;
    }
    const defaultMessage = sceneRequiresWebGL(props)
      ? "Accelerated WebGL is required. Update your browser or enable hardware acceleration."
      : "Scene rendering is unavailable in this browser.";
    const message = String(
      props && props.unsupportedMessage
        ? props.unsupportedMessage
        : defaultMessage
    );
    const wrapper = document.createElement("div");
    wrapper.setAttribute("class", "gosx-scene3d-unsupported");
    wrapper.setAttribute("data-gosx-scene3d-unsupported", "true");
    wrapper.setAttribute("data-gosx-scene3d-unsupported-reason", reason || "webgl-required");
    wrapper.setAttribute("role", "status");
    const text = document.createElement("p");
    text.textContent = message;
    wrapper.appendChild(text);
    mount.appendChild(wrapper);
  }

  function observeSceneCapability(mount, props, capability, onChange) {
    if (!mount || !capability || typeof onChange !== "function") {
      return function() {};
    }
    applySceneCapabilityState(mount, props, capability);
    if (!(window.__gosx.environment && typeof window.__gosx.environment.observe === "function")) {
      return function() {};
    }
    return window.__gosx.environment.observe(function() {
      const next = sceneCapabilityProfile(props);
      if (!sceneCapabilityChanged(capability, next)) {
        return;
      }
      capability.tier = next.tier;
      capability.coarsePointer = next.coarsePointer;
      capability.hover = next.hover;
      capability.reducedData = next.reducedData;
      capability.lowPower = next.lowPower;
      capability.softwareWebGL = next.softwareWebGL;
      capability.visualViewportActive = next.visualViewportActive;
      capability.deviceMemory = next.deviceMemory;
      capability.hardwareConcurrency = next.hardwareConcurrency;
      applySceneCapabilityState(mount, props, capability);
      onChange("capability");
    }, { immediate: false });
  }

  function sceneViewportBase(props) {
    const width = Math.max(240, sceneNumber(props && props.width, 720));
    const height = Math.max(180, sceneNumber(props && props.height, 420));
    const explicitMaxDevicePixelRatio = sceneNumber(props && (props.maxDevicePixelRatio || props.maxPixelRatio), 0);
    return {
      baseWidth: width,
      baseHeight: height,
      aspectRatio: width / Math.max(1, height),
      responsive: sceneBool(props && props.responsive, true),
      explicitMaxDevicePixelRatio,
    };
  }

  function scheduleSceneIdleTask(callback, delayMS) {
    if (typeof callback !== "function") {
      return;
    }
    const delay = Math.max(0, sceneNumber(delayMS, 0));
    const runIdle = function() {
      if (typeof requestIdleCallback === "function") {
        let fired = false;
        const invoke = function(deadline) {
          if (fired) {
            return;
          }
          fired = true;
          callback(deadline);
        };
        requestIdleCallback(invoke, { timeout: 1000 });
        setTimeout(invoke, 1200);
      } else {
        setTimeout(callback, 0);
      }
    };
    if (delay > 0) {
      setTimeout(runIdle, delay);
      return;
    }
    runIdle();
  }

  function sceneCompressionProgressiveDelay(props) {
    const comp = props && props.compression && typeof props.compression === "object" ? props.compression : null;
    if (!comp) {
      return 0;
    }
    return Math.max(0, sceneNumber(
      comp.progressiveDelayMS != null ? comp.progressiveDelayMS : comp.upgradeDelayMS,
      0,
    ));
  }

  function sceneDeferredPostFXDelay(props) {
    return Math.max(0, sceneNumber(
      props && (props.deferPostFXDelayMS != null ? props.deferPostFXDelayMS : props.postFXDelayMS),
      0,
    ));
  }

  function sceneStatusBindingLabel(value) {
    const normalized = String(value || "");
    const branded = normalized.replace(/^webgpu(?=-|$)/, "WebGPU").replace(/^webgl2?(?=-|$)/, "WebGL2");
    return branded.replace(/(^|-)([a-z])/g, function(_match, dash, letter) {
      return (dash ? " " : "") + letter.toUpperCase();
    });
  }

  // Declarative status bindings keep Scene3D diagnostics visible without
  // requiring demo-specific scripts or CSS parent selectors. A status scope
  // owns one scene mount and any number of renderer/fallback/quality outputs.
  function sceneSyncStatusBindings(mount) {
    if (!mount) return;
    let scope = mount;
    while (scope && (!scope.hasAttribute || !scope.hasAttribute("data-gosx-scene3d-status-scope"))) {
      scope = scope.parentNode;
    }
    if (!scope || typeof scope.querySelectorAll !== "function") return;
    const bindings = scope.querySelectorAll("[data-gosx-scene3d-status]");
    const backend = mount.getAttribute("data-gosx-scene3d-renderer") || "starting";
    const fallback = mount.getAttribute("data-gosx-scene3d-renderer-fallback") || "";
    const quality = mount.getAttribute("data-gosx-scene3d-quality-active") || "measuring";
    for (let i = 0; i < bindings.length; i++) {
      const output = bindings[i];
      const kind = output.getAttribute("data-gosx-scene3d-status") || "";
      let value = "";
      if (kind === "renderer") {
        value = backend === "starting" ? "starting…" : sceneStatusBindingLabel(backend);
        output.hidden = false;
        setAttrValue(output, "data-state", backend);
      } else if (kind === "fallback") {
        value = fallback ? "· fallback: " + sceneStatusBindingLabel(fallback) : "";
        output.hidden = !fallback;
        setAttrValue(output, "data-state", fallback ? "active" : "none");
      } else if (kind === "quality") {
        value = quality === "measuring" ? "measuring…" : sceneStatusBindingLabel(quality);
        output.hidden = false;
        setAttrValue(output, "data-state", quality);
      } else {
        continue;
      }
      if (output.textContent !== value) output.textContent = value;
    }
  }

  function createSceneAdaptiveQualityState(props, base, capability) {
    const adaptiveValue = props && (props.adaptiveQuality != null
      ? props.adaptiveQuality
      : (props.adaptivePerformance != null ? props.adaptivePerformance : props.dynamicQuality));
    const adaptiveConfig = adaptiveValue && typeof adaptiveValue === "object" ? adaptiveValue : {};
    const enabled = adaptiveValue && typeof adaptiveValue === "object"
      ? adaptiveValue.enabled !== false
      : sceneBool(adaptiveValue, false);
    const targetFrameMS = Math.max(8, Math.min(50, sceneNumber(
      props && (props.adaptiveTargetFrameMS != null ? props.adaptiveTargetFrameMS : props.targetFrameMS),
      capability && capability.tier === "constrained" ? 20 : 16.7,
    )));
    const minProp = props && (props.minDevicePixelRatio != null ? props.minDevicePixelRatio : props.minPixelRatio);
    const minDevicePixelRatio = Math.max(1, Math.min(2, sceneNumber(minProp, 1)));
    const warmupFrames = Math.max(0, Math.floor(sceneNumber(props && props.adaptiveWarmupFrames, 24)));
    const adaptivePostFX = sceneBool(props && props.adaptivePostFX, true);
    const authoredFrameIntervalMS = Math.max(0, sceneNumber(props && props.frameIntervalMS, 0));
    const authoredMaxFPS = Math.max(0, sceneNumber(props && props.maxFPS, 0));
    const cpuRAFBudgetMS = Math.max(targetFrameMS, authoredFrameIntervalMS > 0
      ? authoredFrameIntervalMS
      : (authoredMaxFPS > 0 ? 1000 / authoredMaxFPS : 0));
    const profileOverrides = (props && (props.qualityProfiles || props.adaptiveQualityProfiles)) || adaptiveConfig.profiles || {};
    const defaults = {
      full: { dprCap: 1.6, surfaceResolution: 160, causticsResolution: 512, objectShadowResolution: 512, objectTextureMaxSide: 512, objectTexturePixelBudget: 786432, expensivePassCadence: 1 },
      balanced: { dprCap: 1.25, surfaceResolution: 128, causticsResolution: 384, objectShadowResolution: 384, objectTextureMaxSide: 384, objectTexturePixelBudget: 442368, expensivePassCadence: 2 },
      survival: { dprCap: 1.0, surfaceResolution: 96, causticsResolution: 256, objectShadowResolution: 256, objectTextureMaxSide: 256, objectTexturePixelBudget: 196608, expensivePassCadence: 3 },
    };
    const profiles = {};
    ["full", "balanced", "survival"].forEach(function(tier) {
      const source = profileOverrides && profileOverrides[tier] && typeof profileOverrides[tier] === "object"
        ? profileOverrides[tier]
        : {};
      const fallback = defaults[tier];
      profiles[tier] = {
        tier,
        dprCap: Math.max(1, Math.min(3, sceneNumber(source.dprCap, fallback.dprCap))),
        surfaceResolution: Math.max(32, Math.floor(sceneNumber(source.surfaceResolution, fallback.surfaceResolution))),
        causticsResolution: Math.max(64, Math.floor(sceneNumber(source.causticsResolution, fallback.causticsResolution))),
        objectShadowResolution: Math.max(64, Math.floor(sceneNumber(source.objectShadowResolution, fallback.objectShadowResolution))),
        objectTextureMaxSide: Math.max(64, Math.floor(sceneNumber(source.objectTextureMaxSide, fallback.objectTextureMaxSide))),
        objectTexturePixelBudget: Math.max(65536, Math.floor(sceneNumber(source.objectTexturePixelBudget, fallback.objectTexturePixelBudget))),
        expensivePassCadence: Math.max(1, Math.floor(sceneNumber(source.expensivePassCadence, fallback.expensivePassCadence))),
      };
    });
    const requestedValue = (props && (props.requestedQualityTier || props.adaptiveQualityTier || props.qualityTier)) || adaptiveConfig.tier;
    const requestedTier = requestedValue === "balanced" || requestedValue === "survival" ? requestedValue : "full";
    // G2: QualityLadder, when authored, supersedes the dprCap-tier governor
    // built above entirely — see sceneUpdateQualityLadder/
    // applySceneQualityLadderState. `tierEnabled` (NOT `enabled`) gates every
    // dprCap-tier field below so a ladder-governed mount reads as "tier
    // disabled" throughout (in particular sceneViewportFromMount's DPR clamp
    // at `adaptiveQuality.enabled`, which must stay false so a ladder NEVER
    // touches DPR — the PRIME DIRECTIVE's resolution axis is forbidden for
    // ladder degradation). No ladder authored: tierEnabled === enabled,
    // zero behavior change (back-compat).
    const ladder = sceneQualityLadder(props);
    const hasLadder = ladder.rungs.length > 0;
    const tierEnabled = hasLadder ? false : enabled;
    // hasExplicitAdaptiveTierConfig: true only when the author configured
    // actual dprCap-tier substance beyond the plain `adaptiveQuality: true`
    // opt-in every scene carries as its framework default (adaptiveQuality
    // defaults to enabled with the built-in full/balanced/survival presets —
    // see `defaults` above). A bare boolean toggle strands nothing worth
    // warning about since there is no author-authored tier config to lose;
    // only a non-default profile override or an explicit requested tier
    // means AdaptiveQuality's tier/profile behavior is actually superseded.
    const hasExplicitAdaptiveTierConfig = Object.keys(profileOverrides).length > 0 || Boolean(requestedValue);
    if (hasLadder && enabled && hasExplicitAdaptiveTierConfig && typeof console !== "undefined" && console.warn) {
      // Go-side authors get the equivalent Props.QualityLadderWarnings()
      // warning at build time; this covers directly JS-authored scenes too.
      console.warn("[gosx] QualityLadder overrides adaptiveQuality.");
    }
    const state = {
      enabled: tierEnabled,
      targetFrameMS,
      cpuRAFBudgetMS,
      minDevicePixelRatio,
      warmupFrames,
      adaptivePostFX,
      profiles,
      requestedTier,
      activeTier: requestedTier,
      activeProfile: profiles[requestedTier],
      frameCount: 0,
      validSamples: 0,
      badFrames: 0,
      goodFrames: 0,
      severeFrames: 0,
      ewmaFrameMS: 0,
      p95FrameMS: 0,
      lastFrameMS: 0,
      measurement: "none",
      lastMeasurement: null,
      sampleWindow: [],
      sampleCursor: 0,
      sampleScratch: new Array(120),
      lastRAFNowMS: null,
      missingRendererSamples: 0,
      rendererTimingFallbackFrames: Math.max(2, Math.floor(sceneNumber(props && props.adaptiveRendererTimingFallbackFrames, 8))),
      resumePending: true,
      cooldownMS: Math.max(5000, sceneNumber(props && props.adaptiveCooldownMS, 5000)),
      cooldownUntilMS: 0,
      transitionReason: tierEnabled ? "initial" : "disabled",
      currentMaxDevicePixelRatio: profiles[requestedTier].dprCap,
      postFXSuppressed: false,
      tier: tierEnabled ? requestedTier : "fixed",
      qualityRevision: 0,
      lastPublishedAtMS: 0,
      lastPublishedRevision: -1,
      baseExplicitMaxDevicePixelRatio: sceneNumber(base && base.explicitMaxDevicePixelRatio, 0),
      mode: hasLadder ? "ladder" : "tier",
    };
    if (hasLadder) {
      state.ladder = ladder.rungs;
      state.rungIndex = ladder.startRung;
      state.rungRevision = 0;
      state.rungReason = "initial";
      // PROMOTE after N (default 120) consecutive frames with headroom below
      // promoteThreshold (default 0.7) × the frame budget. DEMOTE reuses the
      // dprCap-tier governor's sustained-miss condition verbatim (badFrames
      // >= 20 || severeFrames >= 3) — see sceneUpdateQualityLadder.
      state.rungPromoteFrames = Math.max(1, Math.floor(sceneNumber(props && props.qualityLadderPromoteFrames, 120)));
      state.rungPromoteThreshold = Math.max(0.05, Math.min(0.95, sceneNumber(props && props.qualityLadderPromoteThreshold, 0.7)));
      // rungPromoteRule: which promotion rule the last measurement used —
      // "gpu-headroom" (real GPU timing) or "raf-cadence" (cpu-raf fallback,
      // no timestamp-query support). Set fresh every sampled frame in
      // sceneUpdateQualityLadder; this is just the pre-first-sample default.
      state.rungPromoteRule = "gpu-headroom";
    }
    return state;
  }

  function sceneAdaptivePostFXSource(sceneState) {
    return Array.isArray(sceneState && sceneState._adaptiveSourcePostEffects)
      ? sceneState._adaptiveSourcePostEffects
      : [];
  }

  function applySceneAdaptiveQualityState(mount, state, nowMS, force) {
    if (!mount || !state) {
      return;
    }
    if (state.mode === "ladder") {
      applySceneQualityLadderState(mount, state, nowMS, force);
      return;
    }
    const profile = state.activeProfile || (state.profiles && state.profiles.full) || {};
    mount.__gosxScene3DQualityState = {
      enabled: Boolean(state.enabled),
      requestedTier: state.requestedTier,
      activeTier: state.activeTier,
      qualityRevision: state.qualityRevision,
      reason: state.transitionReason,
      cooldownUntilMS: state.cooldownUntilMS,
      validSamples: state.validSamples,
      ewmaFrameMS: state.ewmaFrameMS,
      p95FrameMS: state.p95FrameMS,
      measurement: state.measurement,
      missingRendererSamples: state.missingRendererSamples,
      lastMeasurement: state.lastMeasurement,
      profile,
      postFXSuppressed: Boolean(state.postFXSuppressed),
    };
    const now = Number.isFinite(Number(nowMS)) ? Number(nowMS) : (typeof performance !== "undefined" && performance.now ? performance.now() : Date.now());
    const changed = state.lastPublishedRevision !== state.qualityRevision;
    if (!force && !changed && now - state.lastPublishedAtMS < 250) return;
    state.lastPublishedAtMS = now;
    state.lastPublishedRevision = state.qualityRevision;
    setAttrValue(mount, "data-gosx-scene3d-adaptive-quality", state.enabled ? "true" : "false");
    setAttrValue(mount, "data-gosx-scene3d-quality-tier", state.tier || (state.enabled ? "full" : "fixed"));
    setAttrValue(mount, "data-gosx-scene3d-quality-requested", state.requestedTier || "full");
    setAttrValue(mount, "data-gosx-scene3d-quality-active", state.activeTier || "full");
    setAttrValue(mount, "data-gosx-scene3d-quality-revision", String(Math.max(0, state.qualityRevision || 0)));
    setAttrValue(mount, "data-gosx-scene3d-quality-reason", state.transitionReason || "");
    setAttrValue(mount, "data-gosx-scene3d-quality-measurement", state.measurement || "none");
    setAttrValue(mount, "data-gosx-scene3d-quality-renderer-timing-misses", String(Math.max(0, state.missingRendererSamples || 0)));
    setAttrValue(mount, "data-gosx-scene3d-quality-dpr-cap", state.currentMaxDevicePixelRatio > 0 ? state.currentMaxDevicePixelRatio.toFixed(3) : "");
    setAttrValue(mount, "data-gosx-scene3d-quality-frame-ms", state.lastFrameMS > 0 ? state.lastFrameMS.toFixed(1) : "");
    setAttrValue(mount, "data-gosx-scene3d-quality-ewma-ms", state.ewmaFrameMS > 0 ? state.ewmaFrameMS.toFixed(2) : "");
    setAttrValue(mount, "data-gosx-scene3d-quality-p95-ms", state.p95FrameMS > 0 ? state.p95FrameMS.toFixed(2) : "");
    setAttrValue(mount, "data-gosx-scene3d-quality-surface-resolution", String(profile.surfaceResolution || 0));
    setAttrValue(mount, "data-gosx-scene3d-quality-caustics-resolution", String(profile.causticsResolution || 0));
    setAttrValue(mount, "data-gosx-scene3d-quality-object-shadow-resolution", String(profile.objectShadowResolution || 0));
    setAttrValue(mount, "data-gosx-scene3d-quality-object-texture-max-side", String(profile.objectTextureMaxSide || 0));
    setAttrValue(mount, "data-gosx-scene3d-quality-object-texture-pixel-budget", String(profile.objectTexturePixelBudget || 0));
    setAttrValue(mount, "data-gosx-scene3d-quality-expensive-pass-cadence", String(profile.expensivePassCadence || 1));
    setAttrValue(mount, "data-gosx-scene3d-quality-postfx-suppressed", state.postFXSuppressed ? "true" : "false");
    sceneSyncStatusBindings(mount);
  }

  function scenePrimeAdaptiveQuality(state, viewport, mount, sceneState) {
    if (state && state.mode === "ladder") {
      // Prime the STARTING rung's postEffects admitted-set before the first
      // render — without this, the scene would render with the author's
      // full postEffects list (whatever createSceneState built from props)
      // until the governor's first promote/demote transition, ignoring
      // QualityStartRung entirely.
      sceneApplyQualityLadderRung(sceneState, state);
      applyScenePostFXState(mount, sceneState);
      applySceneAdaptiveQualityState(mount, state, 0, true);
      return;
    }
    if (!state || !state.enabled) {
      applySceneAdaptiveQualityState(mount, state, 0, true);
      return;
    }
    state.currentMaxDevicePixelRatio = Math.max(state.minDevicePixelRatio, sceneNumber(state.activeProfile && state.activeProfile.dprCap, 1));
    applySceneAdaptiveQualityState(mount, state, 0, true);
  }

  function sceneApplyAdaptivePostFX(sceneState, adaptiveQuality) {
    if (!sceneState || !adaptiveQuality || !adaptiveQuality.enabled) {
      return false;
    }
    const source = sceneAdaptivePostFXSource(sceneState);
    if (Array.isArray(sceneState._deferredPostEffects) && sceneState._deferredPostEffects.length > 0) {
      sceneState.postEffects = [];
      return false;
    }
    const suppress = adaptiveQuality.adaptivePostFX && adaptiveQuality.postFXSuppressed && source.length > 0;
    const next = suppress ? [] : source;
    const current = Array.isArray(sceneState.postEffects) ? sceneState.postEffects : [];
    if (current.length === next.length && current.every(function(effect, index) { return effect === next[index]; })) {
      return false;
    }
    sceneState.postEffects = next;
    return true;
  }

  function sceneAdaptiveP95(state) {
    const count = state.sampleWindow.length;
    if (count === 0) return 0;
    const scratch = state.sampleScratch;
    for (let i = 0; i < count; i++) scratch[i] = state.sampleWindow[i];
    for (let i = 1; i < count; i++) {
      const value = scratch[i];
      let j = i - 1;
      while (j >= 0 && scratch[j] > value) {
        scratch[j + 1] = scratch[j];
        j -= 1;
      }
      scratch[j + 1] = value;
    }
    return scratch[Math.max(0, Math.ceil(count * 0.95) - 1)];
  }

  function sceneAdaptiveRendererSample(renderer, nowMS) {
    if (!renderer || typeof renderer.pollPerformanceSample !== "function") return null;
    let sample = null;
    try { sample = renderer.pollPerformanceSample(); } catch (e) { return null; }
    if (sample && typeof sample.then === "function") return null;
    if (typeof sample === "number") sample = { durationMS: sample, source: "renderer" };
    if (!sample || typeof sample !== "object") return null;
    const durationMS = sceneNumber(sample.durationMS != null ? sample.durationMS : (sample.frameMS != null ? sample.frameMS : sample.gpuMS), 0);
    if (!(durationMS > 0) || !Number.isFinite(durationMS)) return null;
    return {
      durationMS,
      source: String(sample.source || sample.measurement || "renderer"),
      atMS: sceneNumber(sample.atMS, nowMS),
      rafIntervalMS: 0,
      cpuDurationMS: 0,
    };
  }

  function sceneAdaptiveRendererTimingStatus(renderer) {
    if (!renderer || typeof renderer.getPerformanceTimingStatus !== "function") return null;
    try {
      const status = renderer.getPerformanceTimingStatus();
      return status && typeof status === "object" ? status : null;
    } catch (e) {
      return null;
    }
  }

  function sceneAdaptiveSetTier(state, tier, reason, nowMS) {
    if (!state.profiles[tier] || state.activeTier === tier) return false;
    state.activeTier = tier;
    state.activeProfile = state.profiles[tier];
    state.tier = tier;
    state.currentMaxDevicePixelRatio = Math.max(state.minDevicePixelRatio, state.activeProfile.dprCap);
    state.qualityRevision += 1;
    state.transitionReason = reason;
    state.cooldownUntilMS = nowMS + state.cooldownMS;
    state.badFrames = 0;
    state.goodFrames = 0;
    state.severeFrames = 0;
    return true;
  }

  function sceneUpdateAdaptiveQuality(state, mount, sceneState, viewport, frameStart, frameNowMS, renderer) {
    if (state && state.mode === "ladder") {
      return sceneUpdateQualityLadder(state, mount, sceneState, viewport, frameStart, frameNowMS, renderer);
    }
    if (!state || !state.enabled) {
      return false;
    }
    const now = typeof performance !== "undefined" && performance.now ? performance.now() : Date.now();
    const rafNow = Number.isFinite(Number(frameNowMS)) ? Number(frameNowMS) : now;
    const cpuDurationMS = Math.max(0, now - sceneNumber(frameStart, now));
    const rafIntervalMS = state.lastRAFNowMS != null && rafNow >= state.lastRAFNowMS ? rafNow - state.lastRAFNowMS : 0;
    state.lastRAFNowMS = rafNow;
    state.frameCount += 1;
    const timingStatus = sceneAdaptiveRendererTimingStatus(renderer);
    const rendererTimingLocked = Boolean(timingStatus && (timingStatus.available === true || timingStatus.active === true));
    let sample = sceneAdaptiveRendererSample(renderer, now);
    if (sample) state.missingRendererSamples = 0;
    else if (rendererTimingLocked) state.missingRendererSamples += 1;
    else state.missingRendererSamples = 0;
    const rendererTimingStale = rendererTimingLocked && state.missingRendererSamples >= state.rendererTimingFallbackFrames;
    if (!sample && (!rendererTimingLocked || rendererTimingStale) && rafIntervalMS > 0 && cpuDurationMS >= 0) {
      sample = {
        durationMS: Math.max(rafIntervalMS, cpuDurationMS),
        source: rendererTimingStale ? "cpu-raf-stale-renderer-timing" : "cpu-raf",
        atMS: now,
        rafIntervalMS,
        cpuDurationMS,
      };
    }
    if (state.resumePending || state.frameCount <= state.warmupFrames || !sample) {
      state.resumePending = false;
      applySceneAdaptiveQualityState(mount, state, now, false);
      return false;
    }
    const frameMS = sample.durationMS;
    state.lastFrameMS = frameMS;
    state.measurement = sample.source;
    state.lastMeasurement = sample;
    state.validSamples += 1;
    state.ewmaFrameMS = state.ewmaFrameMS > 0
      ? state.ewmaFrameMS * 0.84 + frameMS * 0.16
      : frameMS;
    if (state.sampleWindow.length < 120) state.sampleWindow.push(frameMS);
    else {
      state.sampleWindow[state.sampleCursor] = frameMS;
      state.sampleCursor = (state.sampleCursor + 1) % 120;
    }
    if (state.validSamples === 1 || state.validSamples % 10 === 0) state.p95FrameMS = sceneAdaptiveP95(state);

    const target = Math.max(8, sceneNumber(sample.source.indexOf("cpu-raf") === 0 ? state.cpuRAFBudgetMS : state.targetFrameMS, 16.7));
    const missesBudget = state.ewmaFrameMS > target * 1.15 || state.p95FrameMS > target * 1.35;
    const severeMiss = frameMS > target * 2;
    if (missesBudget) {
      state.badFrames += 1;
      state.goodFrames = 0;
    } else if (frameMS < target * 0.72) {
      state.goodFrames += 1;
      state.badFrames = 0;
    } else {
      state.badFrames = 0;
      state.goodFrames = 0;
    }
    state.severeFrames = severeMiss ? state.severeFrames + 1 : 0;

    let changed = false;
    const order = ["full", "balanced", "survival"];
    const activeIndex = Math.max(0, order.indexOf(state.activeTier));
    const requestedIndex = Math.max(0, order.indexOf(state.requestedTier));
    if (now >= state.cooldownUntilMS && (state.badFrames >= 20 || state.severeFrames >= 3)) {
      if (activeIndex < order.length - 1) {
        changed = sceneAdaptiveSetTier(state, order[activeIndex + 1], state.severeFrames >= 3 ? "severe" : "sustained", now);
      } else if (state.adaptivePostFX && !state.postFXSuppressed && sceneAdaptivePostFXSource(sceneState).length > 0) {
        state.postFXSuppressed = true;
        state.qualityRevision += 1;
        state.transitionReason = "postfx";
        state.cooldownUntilMS = now + state.cooldownMS;
        changed = true;
      }
    } else if (now >= state.cooldownUntilMS && state.validSamples >= 300 && state.goodFrames >= 300) {
      if (state.postFXSuppressed) {
        state.postFXSuppressed = false;
        state.qualityRevision += 1;
        state.transitionReason = "postfx-restore";
        state.cooldownUntilMS = now + state.cooldownMS;
        changed = true;
      } else if (activeIndex > requestedIndex) {
        changed = sceneAdaptiveSetTier(state, order[activeIndex - 1], "recovered", now);
      }
    }
    if (changed) {
      sceneApplyAdaptivePostFX(sceneState, state);
      applyScenePostFXState(mount, sceneState);
      applySceneAdaptiveQualityState(mount, state, now, true);
      gosxSceneEmit("warn", "adaptive-quality-transition", {
        reason: state.transitionReason,
        requestedTier: state.requestedTier,
        activeTier: state.activeTier,
        frameMS,
        ewmaFrameMS: state.ewmaFrameMS,
        p95FrameMS: state.p95FrameMS,
        targetFrameMS: state.targetFrameMS,
        dprCap: state.currentMaxDevicePixelRatio,
        postFXSuppressed: state.postFXSuppressed,
      });
      return true;
    }
    applySceneAdaptiveQualityState(mount, state, now, false);
    return false;
  }
