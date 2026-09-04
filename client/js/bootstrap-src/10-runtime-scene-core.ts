  function sceneCSSVarReference(value) {
    return typeof value === "string" && /^var\(\s*--[-_a-zA-Z0-9]+\s*(?:,|\))/.test(value.trim());
  }

  // Fixed-rate water clock shared by WebGL and WebGPU. Kept after the
  // runtime-utils extraction boundary so non-Scene3D routes do not download
  // water simulation policy.
  function sceneWaterResetClock(clock, nowMS, active, paused) {
    var state = clock && typeof clock === "object" ? clock : {};
    var now = Number(nowMS);
    state.lastNowMS = Number.isFinite(now) ? now : 0;
    state.accumulatorMS = 0;
    state.anchored = false;
    state.active = Boolean(active);
    state.paused = Boolean(paused);
    state.ticks = 0;
    state.substeps = 0;
    state.dropped = 0;
    state.deltaSeconds = 0;
    state.reset = true;
    if (!Number.isFinite(Number(state.tickSeq))) state.tickSeq = 0;
    if (!Number.isFinite(Number(state.solverSubstepSeq))) state.solverSubstepSeq = 0;
    if (!Number.isFinite(Number(state.droppedTicks))) state.droppedTicks = 0;
    return state;
  }

  function sceneWaterAdvanceClock(clock, nowMS, active, paused, options) {
    var state = clock && typeof clock === "object" ? clock : {};
    var opts = options && typeof options === "object" ? options : {};
    var simulationHz = Math.max(1, Math.min(240, sceneNumber(opts.simulationHz, 60)));
    var maxCatchUpTicks = Math.max(0, Math.min(8, Math.floor(sceneNumber(opts.maxCatchUpTicks, 2))));
    var solverSubsteps = Math.max(1, Math.min(8, Math.floor(sceneNumber(opts.solverSubsteps, 2))));
    var tickMS = 1000 / simulationHz;
    var now = Number(nowMS);

    state.simulationHz = simulationHz;
    state.maxCatchUpTicks = maxCatchUpTicks;
    state.solverSubstepsPerTick = solverSubsteps;
    state.tickMS = tickMS;
    state.tickSeconds = 1 / simulationHz;
    state.ticks = 0;
    state.substeps = 0;
    state.dropped = 0;
    state.deltaSeconds = 0;
    state.reset = false;
    if (!Number.isFinite(Number(state.tickSeq))) state.tickSeq = 0;
    if (!Number.isFinite(Number(state.solverSubstepSeq))) state.solverSubstepSeq = 0;
    if (!Number.isFinite(Number(state.droppedTicks))) state.droppedTicks = 0;

    var running = Boolean(active) && !Boolean(paused);
    if (!running || !Number.isFinite(now)) return sceneWaterResetClock(state, now, active, paused);
    var lastNow = Number(state.lastNowMS);
    if (!state.anchored || !Number.isFinite(lastNow) || state.active === false || state.paused === true) {
      sceneWaterResetClock(state, now, true, false);
      state.anchored = true;
      return state;
    }
    if (now < lastNow) {
      sceneWaterResetClock(state, now, true, false);
      state.anchored = true;
      return state;
    }

    var accumulatedMS = Math.max(0, sceneNumber(state.accumulatorMS, 0)) + Math.max(0, now - lastNow);
    state.lastNowMS = now;
    state.active = true;
    state.paused = false;
    var wholeTicks = Math.max(0, Math.floor((accumulatedMS + tickMS * 1e-9) / tickMS));
    var ticks = Math.min(wholeTicks, maxCatchUpTicks);
    var dropped = Math.max(0, wholeTicks - ticks);
    accumulatedMS -= wholeTicks * tickMS;
    if (!(accumulatedMS >= 0) || accumulatedMS >= tickMS) accumulatedMS = 0;

    state.accumulatorMS = accumulatedMS;
    state.ticks = ticks;
    state.substeps = ticks * solverSubsteps;
    state.dropped = dropped;
    state.deltaSeconds = ticks * state.tickSeconds;
    state.tickSeq += ticks;
    state.solverSubstepSeq += state.substeps;
    state.droppedTicks += dropped;
    return state;
  }

  function sceneNumberOrCSSVar(value, fallback) {
    return sceneCSSVarReference(value) ? value.trim() : sceneNumber(value, fallback);
  }

  function sceneClampNumberOrCSSVar(value, fallback, min, max) {
    if (sceneCSSVarReference(value)) {
      return value.trim();
    }
    return Math.max(min, Math.min(max, sceneNumber(value, fallback)));
  }

  function defaultSceneObjects() {
    return [
      {
        kind: "cube",
        size: 1.8,
        x: -1.1,
        y: 0.3,
        z: 0,
        color: "#8de1ff",
        spinX: 0.42,
        spinY: 0.74,
        spinZ: 0.16,
      },
      {
        kind: "cube",
        size: 1.1,
        x: 1.6,
        y: -0.7,
        z: 1.4,
        color: "#ffd48f",
        spinX: -0.24,
        spinY: 0.48,
        spinZ: 0.12,
      },
    ];
  }

  function rawSceneObjects(props) {
    const scene = sceneProps(props);
    if (scene && Array.isArray(scene.objects)) {
      return scene.objects;
    }
    if (props && Array.isArray(props.objects)) {
      return props.objects;
    }
    if (sceneHasAuthoredRenderableContent(props)) {
      return [];
    }
    return defaultSceneObjects();
  }

  function sceneHasAuthoredRenderableContent(props) {
    const scene = sceneProps(props);
    const sources = scene ? [scene, props] : [props];
    const keys = [
      "models",
      "points",
      "instancedMeshes",
      "instancedGLBMeshes",
      "computeParticles",
      "waterSystems",
      "lines",
      "surfaces",
      "labels",
      "sprites",
      "html",
      "htmlOverlays",
      "lights",
    ];
    for (const source of sources) {
      if (!source || typeof source !== "object") {
        continue;
      }
      for (const key of keys) {
        if (Array.isArray(source[key]) && source[key].length > 0) {
          return true;
        }
      }
    }
    return false;
  }

  function rawSceneLabels(props) {
    const scene = sceneProps(props);
    if (scene && Array.isArray(scene.labels)) {
      return scene.labels;
    }
    return props && Array.isArray(props.labels) ? props.labels : [];
  }

  function rawSceneSprites(props) {
    const scene = sceneProps(props);
    if (scene && Array.isArray(scene.sprites)) {
      return scene.sprites;
    }
    return props && Array.isArray(props.sprites) ? props.sprites : [];
  }

  function rawSceneHTML(props) {
    const scene = sceneProps(props);
    if (scene && Array.isArray(scene.html)) {
      return scene.html;
    }
    if (scene && Array.isArray(scene.htmlOverlays)) {
      return scene.htmlOverlays;
    }
    if (scene && Array.isArray(scene.htmls)) {
      return scene.htmls;
    }
    if (props && Array.isArray(props.html)) {
      return props.html;
    }
    return props && Array.isArray(props.htmlOverlays) ? props.htmlOverlays : [];
  }

  function rawSceneLights(props) {
    const scene = sceneProps(props);
    if (scene && Array.isArray(scene.lights)) {
      return scene.lights;
    }
    return props && Array.isArray(props.lights) ? props.lights : [];
  }

  function rawSceneEnvironment(props) {
    const scene = sceneProps(props);
    if (scene && scene.environment && typeof scene.environment === "object") {
      return scene.environment;
    }
    return props && props.environment && typeof props.environment === "object" ? props.environment : null;
  }

  function rawSceneModels(props) {
    const scene = sceneProps(props);
    if (scene && Array.isArray(scene.models)) {
      return scene.models;
    }
    return props && Array.isArray(props.models) ? props.models : [];
  }

  function rawSceneInstancedGLBMeshes(props) {
    const scene = sceneProps(props);
    if (scene && Array.isArray(scene.instancedGLBMeshes)) {
      return scene.instancedGLBMeshes;
    }
    return props && Array.isArray(props.instancedGLBMeshes) ? props.instancedGLBMeshes : [];
  }

  function rawScenePoints(props) {
    const scene = sceneProps(props);
    if (scene && Array.isArray(scene.points)) {
      return scene.points;
    }
    return props && Array.isArray(props.points) ? props.points : [];
  }

  function rawSceneInstancedMeshes(props) {
    const scene = sceneProps(props);
    if (scene && Array.isArray(scene.instancedMeshes)) {
      return scene.instancedMeshes;
    }
    return props && Array.isArray(props.instancedMeshes) ? props.instancedMeshes : [];
  }

  function rawSceneComputeParticles(props) {
    const scene = sceneProps(props);
    if (scene && Array.isArray(scene.computeParticles)) {
      return scene.computeParticles;
    }
    return props && Array.isArray(props.computeParticles) ? props.computeParticles : [];
  }

  function rawSceneWaterSystems(props) {
    const scene = sceneProps(props);
    if (scene && Array.isArray(scene.waterSystems)) {
      return scene.waterSystems;
    }
    return props && Array.isArray(props.waterSystems) ? props.waterSystems : [];
  }

  function rawSceneMaterials(props) {
    const scene = sceneProps(props);
    if (scene && Array.isArray(scene.materials)) {
      return scene.materials;
    }
    return props && Array.isArray(props.materials) ? props.materials : [];
  }

  function rawScenePostEffects(props) {
    const scene = sceneProps(props);
    if (scene && Array.isArray(scene.postEffects)) {
      return scene.postEffects;
    }
    if (scene && Array.isArray(scene.postFX)) {
      return scene.postFX;
    }
    if (props && Array.isArray(props.postEffects)) {
      return props.postEffects;
    }
    return props && Array.isArray(props.postFX) ? props.postFX : [];
  }

  function rawSceneAnimations(props) {
    const scene = sceneProps(props);
    if (scene && Array.isArray(scene.animations)) {
      return scene.animations;
    }
    return props && Array.isArray(props.animations) ? props.animations : [];
  }

  function sceneProps(props) {
    return props && props.scene && typeof props.scene === "object" ? props.scene : null;
  }

  function sceneCloneData(value) {
    if (Array.isArray(value)) {
      return value.map(sceneCloneData);
    }
    if (typeof ArrayBuffer !== "undefined" && ArrayBuffer.isView && ArrayBuffer.isView(value)) {
      return typeof value.slice === "function" ? value.slice() : value;
    }
    if (!value || typeof value !== "object") {
      return value;
    }
    const clone = {};
    const keys = Object.keys(value);
    for (let i = 0; i < keys.length; i += 1) {
      const key = keys[i];
      clone[key] = sceneCloneData(value[key]);
    }
    return clone;
  }

  function sceneIsPlainObject(value) {
    return Boolean(value) && typeof value === "object" && !Array.isArray(value) && !(typeof ArrayBuffer !== "undefined" && ArrayBuffer.isView && ArrayBuffer.isView(value));
  }

  function normalizeSceneMaterialCapabilityTier(value) {
    switch (String(value || "").trim().toLowerCase()) {
      case "constrained":
      case "balanced":
      case "full":
        return String(value).trim().toLowerCase();
      default:
        return "";
    }
  }

  function normalizeSceneMaterialVariantName(value) {
    const normalized = String(value || "").trim().toLowerCase().replace(/[^a-z0-9]/g, "");
    switch (normalized) {
      case "full":
      case "balanced":
      case "constrained":
        return normalized;
      case "lowpower":
        return "lowPower";
      case "reduceddata":
        return "reducedData";
      case "coarsepointer":
        return "coarsePointer";
      default:
        return normalized;
    }
  }

  function sceneMaterialCapabilityVariantKeys(capability) {
    const caps = sceneIsPlainObject(capability) ? capability : {};
    const keys = [];
    const tier = normalizeSceneMaterialCapabilityTier(caps.tier);
    if (tier) {
      keys.push(tier);
    }
    if (caps.lowPower) {
      keys.push("lowPower");
    }
    if (caps.reducedData) {
      keys.push("reducedData");
    }
    if (caps.coarsePointer) {
      keys.push("coarsePointer");
    }
    return keys;
  }

  function sceneMaterialVariantForKey(variants, key) {
    if (!sceneIsPlainObject(variants)) {
      return null;
    }
    if (sceneIsPlainObject(variants[key])) {
      return variants[key];
    }
    const normalized = normalizeSceneMaterialVariantName(key);
    const names = Object.keys(variants);
    for (let index = 0; index < names.length; index += 1) {
      const name = names[index];
      if (normalizeSceneMaterialVariantName(name) === normalized && sceneIsPlainObject(variants[name])) {
        return variants[name];
      }
    }
    return null;
  }

  function sceneMaterialRecordForCapability(raw, capability) {
    const source = sceneIsPlainObject(raw) ? raw : {};
    if (!sceneIsPlainObject(source.variants)) {
      return source;
    }
    const out = Object.assign({}, source);
    delete out.variants;
    const selected = [];
    const keys = sceneMaterialCapabilityVariantKeys(capability);
    for (let index = 0; index < keys.length; index += 1) {
      const key = keys[index];
      const variant = sceneMaterialVariantForKey(source.variants, key);
      if (!variant) {
        continue;
      }
      Object.assign(out, variant);
      selected.push(key);
    }
    if (selected.length) {
      out._variantKey = selected.join(",");
    }
    return out;
  }

  function sceneTransitionMetadataKey(key) {
    return key === "transition" || key === "inState" || key === "outState" || key === "live" || (typeof key === "string" && key.charAt(0) === "_");
  }

  function normalizeSceneEasing(value) {
    switch (String(value || "").trim().toLowerCase()) {
      case "linear":
        return "linear";
      case "ease-in":
        return "ease-in";
      case "ease-out":
        return "ease-out";
      case "ease-in-out":
        return "ease-in-out";
      default:
        return "";
    }
  }

  function normalizeSceneTransitionTiming(raw, fallback) {
    const base = sceneIsPlainObject(fallback) ? fallback : {};
    const source = sceneIsPlainObject(raw) ? raw : {};
    return {
      duration: Math.max(0, Math.round(sceneNumber(source.duration, sceneNumber(base.duration, 0)))),
      easing: normalizeSceneEasing(source.easing || base.easing),
    };
  }

  function normalizeSceneTransition(raw, fallback) {
    const base = sceneIsPlainObject(fallback) ? fallback : {};
    const source = sceneIsPlainObject(raw) ? raw : {};
    return {
      in: normalizeSceneTransitionTiming(source.in, base.in),
      out: normalizeSceneTransitionTiming(source.out, base.out),
      update: normalizeSceneTransitionTiming(source.update, base.update),
    };
  }

  function sceneNormalizeLive(value, fallback) {
    const source = Array.isArray(value) ? value : (Array.isArray(fallback) ? fallback : []);
    if (!source.length) {
      return [];
    }
    const seen = new Set();
    const out = [];
    for (let i = 0; i < source.length; i += 1) {
      const next = typeof source[i] === "string" ? source[i].trim() : "";
      if (!next || seen.has(next)) {
        continue;
      }
      seen.add(next);
      out.push(next);
    }
    return out;
  }

  function sceneNormalizeLifecycle(item, fallback) {
    const current = sceneIsPlainObject(fallback) ? fallback : {};
    const source = sceneIsPlainObject(item) ? item : {};
    const hasInState = Object.prototype.hasOwnProperty.call(source, "inState");
    const hasOutState = Object.prototype.hasOwnProperty.call(source, "outState");
    const hasLive = Object.prototype.hasOwnProperty.call(source, "live");
    return {
      transition: normalizeSceneTransition(source.transition, current._transition),
      inState: sceneCloneData(hasInState ? source.inState : current._inState),
      outState: sceneCloneData(hasOutState ? source.outState : current._outState),
      live: sceneNormalizeLive(hasLive ? source.live : undefined, current._live),
    };
  }


  function sceneLinePoint(value) {
    if (Array.isArray(value)) {
      return {
        x: sceneNumber(value[0], 0),
        y: sceneNumber(value[1], 0),
        z: sceneNumber(value[2], 0),
      };
    }
    const item = value && typeof value === "object" ? value : {};
    return {
      x: sceneNumber(item.x, 0),
      y: sceneNumber(item.y, 0),
      z: sceneNumber(item.z, 0),
    };
  }

  function sceneLinePoints(value) {
    const list = Array.isArray(value) ? value : [];
    return list.map(sceneLinePoint);
  }

  function sceneLineSegmentValue(value) {
    function sceneLineIndex(entry) {
      const index = Math.floor(sceneNumber(entry, -1));
      return Number.isFinite(index) ? index : -1;
    }
    if (Array.isArray(value)) {
      return [sceneLineIndex(value[0]), sceneLineIndex(value[1])];
    }
    const item = value && typeof value === "object" ? value : {};
    return [
      sceneLineIndex(item.from !== undefined ? item.from : item.a),
      sceneLineIndex(item.to !== undefined ? item.to : item.b),
    ];
  }

  function sceneLineSegments(value, pointCount) {
    const list = Array.isArray(value) ? value : [];
    const out = [];
    for (const item of list) {
      const pair = sceneLineSegmentValue(item);
      if (!Number.isFinite(pair[0]) || !Number.isFinite(pair[1])) {
        continue;
      }
      if (pair[0] < 0 || pair[1] < 0 || pair[0] === pair[1]) {
        continue;
      }
      if (pair[0] >= pointCount || pair[1] >= pointCount) {
        continue;
      }
      out.push(pair);
    }
    if (out.length === 0 && pointCount > 1) {
      for (let index = 0; index + 1 < pointCount; index += 1) {
        out.push([index, index + 1]);
      }
    }
    return out;
  }

  // sceneNormalizeMeshIndices validates an optional authored triangle index
  // stream over count unique vertices. Absent (null/undefined) input returns
  // null — the geometry simply stays non-indexed. Present-but-malformed input
  // (wrong length, non-triangle list, or any index outside [0, count)) returns
  // undefined so the caller can fail closed instead of drawing a partial mesh
  // or handing the GPU an out-of-range fetch. Valid input returns a fresh
  // Uint32Array copy so callers can never alias the author's slice. Indices are
  // normalized once here, never per frame. It lives in the base chunk next to
  // sceneTypedFloatArray because mesh normalization runs on every Scene3D page,
  // backend and all.
  function sceneNormalizeMeshIndices(value, count) {
    if (value === undefined || value === null) return null;
    const source = value;
    if (typeof source !== "object" || typeof source.length !== "number") {
      return undefined;
    }
    const total = Math.floor(source.length);
    if (total < 3 || total % 3 !== 0) {
      return undefined;
    }
    const out = source instanceof Uint32Array ? source.slice() : new Uint32Array(total);
    for (let i = 0; i < total; i += 1) {
      const entry = Number(source[i]);
      if (!Number.isInteger(entry) || entry < 0 || entry >= count) {
        return undefined;
      }
      out[i] = entry;
    }
    return out;
  }

  // sceneTypedFloatArray stays in the base chunk even though the legacy
  // vertex-colour renderer (16e-scene-webgl-legacy.js) also calls it.
  function sceneTypedFloatArray(values) {
    if (values instanceof Float32Array) {
      return values;
    }
    const list = Array.isArray(values) ? values : [];
    const typed = new Float32Array(list.length);
    for (let i = 0; i < list.length; i += 1) {
      typed[i] = sceneNumber(list[i], 0);
    }
    return typed;
  }

  function sceneNormalizeMeshFloatArray(value, tupleSize) {
    const typed = sceneTypedFloatArray(value);
    const safeTupleSize = Math.max(1, Math.floor(sceneNumber(tupleSize, 1)));
    const count = Math.floor(typed.length / safeTupleSize);
    if (!count) {
      return new Float32Array(0);
    }
    if (typed.length === count * safeTupleSize) {
      return typed;
    }
    return typed.slice(0, count * safeTupleSize);
  }

  function sceneNormalizeMeshVertexData(value) {
    const item = value && typeof value === "object" ? value : {};
    const positions = sceneNormalizeMeshFloatArray(item.positions, 3);
    if (!positions.length) {
      return null;
    }
    const inferredCount = Math.floor(positions.length / 3);
    const count = Math.max(0, Math.min(
      inferredCount,
      Math.floor(sceneNumber(item.count, inferredCount)),
    ));
    if (!count) {
      return null;
    }
    const normals = sceneNormalizeMeshFloatArray(item.normals, 3);
    const uvs = sceneNormalizeMeshFloatArray(item.uvs, 2);
    const tangents = sceneNormalizeMeshFloatArray(item.tangents, 4);
    const joints = sceneNormalizeMeshFloatArray(item.joints, 4);
    const weights = sceneNormalizeMeshFloatArray(item.weights, 4);
    // Normalize the optional index stream once. Malformed indexed geometry
    // fails closed: the object carries no vertices, so nothing partial is ever
    // published or drawn.
    const indices = sceneNormalizeMeshIndices(item.indices, count);
    if (indices === undefined) {
      return null;
    }
    return {
      positions: count * 3 === positions.length ? positions : positions.slice(0, count * 3),
      normals: normals.length >= count * 3 ? normals.slice(0, count * 3) : new Float32Array(0),
      uvs: uvs.length >= count * 2 ? uvs.slice(0, count * 2) : new Float32Array(0),
      tangents: tangents.length >= count * 4 ? tangents.slice(0, count * 4) : new Float32Array(0),
      joints: joints.length >= count * 4 ? joints.slice(0, count * 4) : new Float32Array(0),
      weights: weights.length >= count * 4 ? weights.slice(0, count * 4) : new Float32Array(0),
      indices: indices || null,
      count,
      // Retained geometry is an explicit snapshot contract, never inferred
      // from typed-array identity. For immutable=true, every attribute remains
      // immutable until revision changes; dynamic=true always forces baking.
      immutable: item.immutable === true,
      revision: Object.prototype.hasOwnProperty.call(item, "revision") &&
        Number.isFinite(Number(item.revision)) && Number(item.revision) >= 0
          ? Math.floor(Number(item.revision))
          : null,
      dynamic: item.dynamic === true,
    };
  }

  function sceneLineGeometryMetrics(points) {
    if (!Array.isArray(points) || points.length === 0) {
      return null;
    }
    let minX = points[0].x;
    let minY = points[0].y;
    let minZ = points[0].z;
    let maxX = points[0].x;
    let maxY = points[0].y;
    let maxZ = points[0].z;
    for (let i = 1; i < points.length; i += 1) {
      const point = points[i];
      minX = Math.min(minX, point.x);
      minY = Math.min(minY, point.y);
      minZ = Math.min(minZ, point.z);
      maxX = Math.max(maxX, point.x);
      maxY = Math.max(maxY, point.y);
      maxZ = Math.max(maxZ, point.z);
    }
    return {
      width: Math.max(0.0001, maxX - minX),
      height: Math.max(0.0001, maxY - minY),
      depth: Math.max(0.0001, maxZ - minZ),
      radius: Math.max(0.0001, Math.max(maxX - minX, maxY - minY, maxZ - minZ) / 2),
    };
  }

  function sceneNormalizeParentMatrix(value, fallback) {
    const source = value === undefined ? fallback : value;
    return source && source.length === 16 && sceneAffineDeterminant(source, 0) ? Array.from(source) : null;
  }


  function normalizeSceneObject(object, index, fallback) {
    const current = sceneIsPlainObject(fallback) ? fallback : {};
    const item = sceneIsPlainObject(object) ? object : {};
    const scaleSource = sceneIsPlainObject(item.scale) ? item.scale : (sceneIsPlainObject(current.scale) ? current.scale : null);
    const vertices = sceneNormalizeMeshVertexData(item.vertices);
    const kind = normalizeSceneKind(item.kind || current.kind);
    const size = sceneNumber(item.size, sceneNumber(current.size, 1.2));
    const points = kind === "lines"
      ? sceneLinePoints(Object.prototype.hasOwnProperty.call(item, "points") ? item.points : current.points)
      : [];
    const lineMetrics = kind === "lines" ? sceneLineGeometryMetrics(points) : null;
    const materialKind = normalizeSceneMaterialKind(sceneObjectMaterialKindValue(item) || current.materialKind);
    const materialColor = sceneObjectMaterialHasValue(item, "color") ? sceneObjectMaterialValue(item, "color") : current.color;
    const textureValue = sceneObjectMaterialHasValue(item, "texture") ? sceneObjectMaterialValue(item, "texture") : current.texture;
    const texture = typeof textureValue === "string" ? textureValue.trim() : "";
    const unlit = sceneBool(sceneObjectMaterialValue(item, "unlit"), sceneBool(current.unlit, false));
    const opacity = sceneClampNumberOrCSSVar(sceneObjectMaterialValue(item, "opacity"), sceneNumber(current.opacity, sceneDefaultMaterialOpacity(materialKind)), 0, 1);
    const numericOpacity = sceneNumber(opacity, sceneNumber(current.opacity, sceneDefaultMaterialOpacity(materialKind)));
    const maskCutoff = sceneNormalizeMaterialAlphaCutoff(
      sceneObjectMaterialValue(item, "alphaCutoff"), current.alphaCutoff);
    const maskOpaque = sceneMaterialMaskActive(maskCutoff) &&
      !sceneMaterialHasDirectAuthoredShaderValues(
        sceneEffectiveShaderValues(item, current, false));
    const rawBlendMode = sceneObjectBlendModeHasValue(item)
      ? sceneObjectBlendModeValue(item) : current.blendMode;
    const blendExplicit = sceneMaterialProfileBlendMode(rawBlendMode) !== "" &&
      sceneRoutedBlendExplicit(item, current, sceneObjectBlendModeHasValue(item));
    const blendMode = normalizeSceneMaterialBlendMode(
      blendExplicit ? rawBlendMode : "",
      materialKind,
      numericOpacity,
      maskOpaque,
    );
    const rawRenderPass = sceneObjectMaterialHasValue(item, "renderPass")
      ? sceneObjectMaterialValue(item, "renderPass") : current.renderPass;
    const passExplicit = sceneMaterialProfileRenderPass(rawRenderPass) !== "" &&
      sceneRoutedPassExplicit(item, current,
        sceneObjectMaterialHasValue(item, "renderPass"));
    const lifecycle = sceneNormalizeLifecycle(item, current);
    const segmentSource = Object.prototype.hasOwnProperty.call(item, "segments") ? item.segments : current.segments;
    const radialSegmentSource = Object.prototype.hasOwnProperty.call(item, "radialSegments") ? item.radialSegments : current.radialSegments;
    const tubularSegmentSource = Object.prototype.hasOwnProperty.call(item, "tubularSegments") ? item.tubularSegments : current.tubularSegments;
    const radius = sceneNumber(item.radius, sceneNumber(current.radius, lineMetrics ? lineMetrics.radius : (kind === "torus" ? 0.7 : (size / 2))));
    const radiusTop = Math.max(0, sceneNumber(item.radiusTop, sceneNumber(current.radiusTop, radius)));
    const radiusBottom = Math.max(0, sceneNumber(item.radiusBottom, sceneNumber(current.radiusBottom, radius)));
    const normalized = {
      id: item.id || current.id || ("scene-object-" + index),
      kind,
      material: typeof item.material === "string" && item.material.trim() ? item.material.trim() : (typeof current.material === "string" ? current.material : ""),
      size,
      width: sceneNumber(item.width, sceneNumber(current.width, lineMetrics ? lineMetrics.width : size)),
      height: sceneNumber(item.height, sceneNumber(current.height, lineMetrics ? lineMetrics.height : size)),
      depth: sceneNumber(item.depth, sceneNumber(current.depth, kind === "plane" ? sceneNumber(item.height, size) : (lineMetrics ? lineMetrics.depth : size))),
      radius,
      radiusTop,
      radiusBottom,
      tube: Math.max(0.0001, sceneNumber(item.tube, sceneNumber(current.tube, 0.3))),
      segments: scenePrimitiveSegmentResolution(segmentSource, kind === "sphere" || kind === "cylinder" || kind === "cone" ? 32 : 12, 3, 256),
      radialSegments: scenePrimitiveSegmentResolution(radialSegmentSource, 32, 3, 256),
      tubularSegments: scenePrimitiveSegmentResolution(tubularSegmentSource, 16, 3, 128),
      points,
      lineSegments: kind === "lines" ? sceneLineSegments(Array.isArray(item.lineSegments) ? item.lineSegments : (Array.isArray(current.lineSegments) ? current.lineSegments : item.segments), points.length) : [],
      vertices: vertices || current.vertices || null,
      x: sceneNumber(item.x, sceneNumber(current.x, 0)),
      y: sceneNumber(item.y, sceneNumber(current.y, 0)),
      z: sceneNumber(item.z, sceneNumber(current.z, 0)),
      materialKind,
      color: typeof materialColor === "string" && materialColor ? materialColor : (typeof current.color === "string" && current.color ? current.color : "#8de1ff"),
      texture,
      unlit,
      opacity,
      emissive: sceneClampNumberOrCSSVar(sceneObjectMaterialValue(item, "emissive"), sceneNumber(current.emissive, sceneDefaultMaterialEmissive(materialKind)), 0, 1),
      roughness: sceneNumberOrCSSVar(sceneObjectMaterialValue(item, "roughness"), sceneNumber(current.roughness, 0.5)),
      metalness: sceneNumberOrCSSVar(sceneObjectMaterialValue(item, "metalness"), sceneNumber(current.metalness, 0)),
      ior: sceneNormalizeMaterialIor(sceneObjectMaterialValue(item, "ior"), current.ior),
      specularIntensity: sceneNormalizeMaterialSpecularIntensity(sceneObjectMaterialValue(item, "specularIntensity"), current.specularIntensity),
      specularColor: sceneNormalizeMaterialSpecularColor(sceneObjectMaterialValue(item, "specularColor"), current.specularColor),
      clearcoat: sceneClampNumberOrCSSVar(sceneObjectMaterialValue(item, "clearcoat"), sceneNumber(current.clearcoat, 0), 0, 1),
      sheen: sceneClampNumberOrCSSVar(sceneObjectMaterialValue(item, "sheen"), sceneNumber(current.sheen, 0), 0, 1),
      transmission: sceneClampNumberOrCSSVar(sceneObjectMaterialValue(item, "transmission"), sceneNumber(current.transmission, 0), 0, 1),
      iridescence: sceneClampNumberOrCSSVar(sceneObjectMaterialValue(item, "iridescence"), sceneNumber(current.iridescence, 0), 0, 1),
      anisotropy: sceneClampNumberOrCSSVar(sceneObjectMaterialValue(item, "anisotropy"), sceneNumber(current.anisotropy, 0), -1, 1),
      alphaCutoff: sceneNormalizeMaterialAlphaCutoff(sceneObjectMaterialValue(item, "alphaCutoff"), current.alphaCutoff),
      normalMap: typeof sceneObjectMaterialValue(item, "normalMap") === "string" ? sceneObjectMaterialValue(item, "normalMap").trim() : (typeof current.normalMap === "string" ? current.normalMap : ""),
      roughnessMap: typeof sceneObjectMaterialValue(item, "roughnessMap") === "string" ? sceneObjectMaterialValue(item, "roughnessMap").trim() : (typeof current.roughnessMap === "string" ? current.roughnessMap : ""),
      metalnessMap: typeof sceneObjectMaterialValue(item, "metalnessMap") === "string" ? sceneObjectMaterialValue(item, "metalnessMap").trim() : (typeof current.metalnessMap === "string" ? current.metalnessMap : ""),
      occlusionMap: typeof sceneObjectMaterialValue(item, "occlusionMap") === "string" ? sceneObjectMaterialValue(item, "occlusionMap").trim() : (typeof current.occlusionMap === "string" ? current.occlusionMap : ""),
      emissiveMap: typeof sceneObjectMaterialValue(item, "emissiveMap") === "string" ? sceneObjectMaterialValue(item, "emissiveMap").trim() : (typeof current.emissiveMap === "string" ? current.emissiveMap : ""),
      textureDescriptors: normalizeSceneMaterialTextureDescriptors(
        sceneObjectMaterialValue(item, "textureDescriptors"),
        current.textureDescriptors,
      ),
      lineDash: sceneBool(sceneObjectMaterialHasValue(item, "lineDash") ? sceneObjectMaterialValue(item, "lineDash") : current.lineDash, false),
      dashSize: sceneNumber(sceneObjectMaterialValue(item, "dashSize"), sceneNumber(current.dashSize, 0)),
      gapSize: sceneNumber(sceneObjectMaterialValue(item, "gapSize"), sceneNumber(current.gapSize, 0)),
      customVertex: typeof sceneObjectMaterialValue(item, "customVertex") === "string" ? sceneObjectMaterialValue(item, "customVertex") : (typeof current.customVertex === "string" ? current.customVertex : ""),
      customFragment: typeof sceneObjectMaterialValue(item, "customFragment") === "string" ? sceneObjectMaterialValue(item, "customFragment") : (typeof current.customFragment === "string" ? current.customFragment : ""),
      customVertexWGSL: typeof sceneObjectMaterialValue(item, "customVertexWGSL") === "string" ? sceneObjectMaterialValue(item, "customVertexWGSL") : (typeof current.customVertexWGSL === "string" ? current.customVertexWGSL : ""),
      customFragmentWGSL: typeof sceneObjectMaterialValue(item, "customFragmentWGSL") === "string" ? sceneObjectMaterialValue(item, "customFragmentWGSL") : (typeof current.customFragmentWGSL === "string" ? current.customFragmentWGSL : ""),
      customUniforms: sceneIsPlainObject(sceneObjectMaterialValue(item, "customUniforms")) ? Object.assign({}, sceneObjectMaterialValue(item, "customUniforms")) : (sceneIsPlainObject(current.customUniforms) ? Object.assign({}, current.customUniforms) : null),
      shaderBackend: typeof sceneObjectMaterialValue(item, "shaderBackend") === "string" ? sceneObjectMaterialValue(item, "shaderBackend").trim().toLowerCase() : (typeof current.shaderBackend === "string" ? current.shaderBackend : ""),
      shaderLayout: sceneIsPlainObject(sceneObjectMaterialValue(item, "shaderLayout")) ? sceneCloneData(sceneObjectMaterialValue(item, "shaderLayout")) : (sceneIsPlainObject(current.shaderLayout) ? sceneCloneData(current.shaderLayout) : null),
      shaderSource: typeof sceneObjectMaterialValue(item, "shaderSource") === "string" ? sceneObjectMaterialValue(item, "shaderSource").trim() : (typeof current.shaderSource === "string" ? current.shaderSource : ""),
      shaderSourceFiles: sceneIsPlainObject(sceneObjectMaterialValue(item, "shaderSourceFiles")) ? sceneCloneData(sceneObjectMaterialValue(item, "shaderSourceFiles")) : (sceneIsPlainObject(current.shaderSourceFiles) ? sceneCloneData(current.shaderSourceFiles) : null),
      blendMode,
      _blendModeDerived: !blendExplicit,
      _renderPassDerived: !passExplicit,
      renderPass: normalizeSceneMaterialRenderPass(
        passExplicit ? rawRenderPass : "",
        blendMode,
        numericOpacity,
        materialKind,
        maskOpaque,
      ),
      wireframe: sceneBool(
        sceneObjectMaterialHasValue(item, "wireframe") ? sceneObjectMaterialValue(item, "wireframe") : current.wireframe,
        texture === "",
      ),
      pickable: Object.prototype.hasOwnProperty.call(item, "pickable") ? sceneBool(item.pickable, false) : current.pickable,
      visible: Object.prototype.hasOwnProperty.call(item, "visible")
        ? sceneBool(item.visible, true)
        : (Object.prototype.hasOwnProperty.call(current, "visible") ? sceneBool(current.visible, true) : true),
      rotationX: sceneNumber(item.rotationX, sceneNumber(current.rotationX, 0)),
      rotationY: sceneNumber(item.rotationY, sceneNumber(current.rotationY, 0)),
      rotationZ: sceneNumber(item.rotationZ, sceneNumber(current.rotationZ, 0)),
      scaleX: sceneNumber(item.scaleX, sceneNumber(scaleSource ? scaleSource.x : undefined, sceneNumber(current.scaleX, 1))),
      scaleY: sceneNumber(item.scaleY, sceneNumber(scaleSource ? scaleSource.y : undefined, sceneNumber(current.scaleY, 1))),
      scaleZ: sceneNumber(item.scaleZ, sceneNumber(scaleSource ? scaleSource.z : undefined, sceneNumber(current.scaleZ, 1))),
      parentMatrix: sceneNormalizeParentMatrix(item.parentMatrix, current.parentMatrix),
      spinX: sceneNumber(item.spinX, sceneNumber(current.spinX, 0)),
      spinY: sceneNumber(item.spinY, sceneNumber(current.spinY, 0)),
      spinZ: sceneNumber(item.spinZ, sceneNumber(current.spinZ, 0)),
      shiftX: sceneNumber(item.shiftX, sceneNumber(current.shiftX, 0)),
      shiftY: sceneNumber(item.shiftY, sceneNumber(current.shiftY, 0)),
      shiftZ: sceneNumber(item.shiftZ, sceneNumber(current.shiftZ, 0)),
      driftSpeed: sceneNumber(item.driftSpeed, sceneNumber(current.driftSpeed, 0)),
      driftPhase: sceneNumber(item.driftPhase, sceneNumber(current.driftPhase, 0)),
      // lineWidth: 0 means "use renderer default" (1.8px on the canvas world
      // fallback). Non-zero values come from scene.LinesGeometry.Width on the
      // Go side and flow into per-segment width buffers at bundle build time.
      lineWidth: sceneNumber(item.lineWidth, sceneNumber(current.lineWidth, 0)),
      selected: sceneBool(Object.prototype.hasOwnProperty.call(item, "selected") ? item.selected : current.selected, false),
      // gizmoRing marks a TransformControls rotate-mode ring helper; the mount
      // layer flips its `visible` off Props.GizmoInputSignal at runtime (see
      // applyMountedSceneGizmoMode in 20-scene-mount.js).
      gizmoRing: sceneBool(Object.prototype.hasOwnProperty.call(item, "gizmoRing") ? item.gizmoRing : current.gizmoRing, false),
      // gizmoHelper/gizmoFormMode: this object is one piece of a
      // TransformControls live helper group. The mount layer hides the whole
      // group when the selection signal is empty, repositions it onto the
      // selected object's world transform, and shows only the piece whose
      // gizmoFormMode matches the active gizmo-mode signal (see
      // syncMountedSceneGizmoHelpers in 20-scene-mount.js).
      gizmoHelper: sceneBool(Object.prototype.hasOwnProperty.call(item, "gizmoHelper") ? item.gizmoHelper : current.gizmoHelper, false),
      gizmoFormMode: typeof item.gizmoFormMode === "string" && item.gizmoFormMode
        ? item.gizmoFormMode
        : (typeof current.gizmoFormMode === "string" ? current.gizmoFormMode : ""),
      _modelHidden: Object.prototype.hasOwnProperty.call(item, "_modelHidden")
        ? sceneBool(item._modelHidden, false)
        : sceneBool(current._modelHidden, false),
      // qualityGroup: G2 QualityLadder layer tagging (see scene.Mesh.QualityGroup
      // / QualityRung.LayerGroups). Empty means unconditionally visible at
      // every rung — a ladder only gates objects that opted in. Read by the
      // mount layer's per-frame object filter (sceneFilterObjectsByQualityGroups
      // in 20-scene-mount.js), never here — this normalizer only carries the
      // tag through.
      qualityGroup: typeof item.qualityGroup === "string" && item.qualityGroup.trim()
        ? item.qualityGroup.trim()
        : (typeof current.qualityGroup === "string" ? current.qualityGroup : ""),
      outlineColor: typeof item.outlineColor === "string" && item.outlineColor ? item.outlineColor : (typeof current.outlineColor === "string" ? current.outlineColor : ""),
      outlineWidth: sceneNumber(item.outlineWidth, sceneNumber(current.outlineWidth, 0)),
      viewCulled: sceneBool(Object.prototype.hasOwnProperty.call(item, "viewCulled") ? item.viewCulled : current.viewCulled, false),
      castShadow: sceneBool(Object.prototype.hasOwnProperty.call(item, "castShadow") ? item.castShadow : current.castShadow, false),
      receiveShadow: sceneBool(Object.prototype.hasOwnProperty.call(item, "receiveShadow") ? item.receiveShadow : current.receiveShadow, false),
      doubleSided: sceneBool(Object.prototype.hasOwnProperty.call(item, "doubleSided") ? item.doubleSided : current.doubleSided, false),
      depthWrite: Object.prototype.hasOwnProperty.call(item, "depthWrite") ? sceneBool(item.depthWrite, true) : current.depthWrite,
      lodGroup: typeof item.lodGroup === "string" && item.lodGroup ? item.lodGroup : (typeof current.lodGroup === "string" ? current.lodGroup : ""),
      lodLevel: Math.max(0, Math.floor(sceneNumber(item.lodLevel, sceneNumber(current.lodLevel, 0)))),
      lodMinDistance: Math.max(0, sceneNumber(item.lodMinDistance, sceneNumber(current.lodMinDistance, 0))),
      lodMaxDistance: Math.max(0, sceneNumber(item.lodMaxDistance, sceneNumber(current.lodMaxDistance, 0))),
      skin: item.skin && typeof item.skin === "object" ? item.skin : (current.skin && typeof current.skin === "object" ? current.skin : null),
      _transition: lifecycle.transition,
      _inState: lifecycle.inState,
      _outState: lifecycle.outState,
      _live: lifecycle.live,
    };
    normalized.static = sceneBool(
      Object.prototype.hasOwnProperty.call(item, "static") ? item.static : current.static,
      !sceneObjectAnimated(normalized),
    );
    if (!normalized.vertices && normalized.wireframe === false && typeof scenePrimitiveTriangleMesh === "function") {
      normalized.vertices = scenePrimitiveTriangleMesh(normalized);
    }
    return normalized;
  }

  function normalizeSceneLightKind(value) {
    const kind = typeof value === "string" ? value.trim().toLowerCase() : "";
    switch (kind) {
      case "ambient":
        return "ambient";
      case "directional":
      case "sun":
        return "directional";
      case "point":
        return "point";
      case "spot":
      case "spotlight":
        return "spot";
      case "hemisphere":
      case "hemi":
      case "hemispherelight":
        return "hemisphere";
      case "rect":
      case "area":
      case "rectarea":
      case "rect-area":
      case "rectarealight":
      case "rect-area-light":
        return "rect-area";
      case "probe":
      case "lightprobe":
      case "light-probe":
        return "light-probe";
      default:
        return "";
    }
  }

  function sceneDefaultLightIntensity(kind) {
    switch (normalizeSceneLightKind(kind)) {
      case "ambient":
        return 0.28;
      case "directional":
        return 1;
      case "point":
        return 1.1;
      case "rect-area":
        return 1.4;
      case "light-probe":
        return 0.25;
      default:
        return 1;
    }
  }

  function normalizeSceneLight(light, index, fallback) {
    const current = sceneIsPlainObject(fallback) ? fallback : {};
    const item = sceneIsPlainObject(light) ? light : {};
    const kind = normalizeSceneLightKind(item.kind || item.lightKind || current.kind);
    if (!kind) {
      return null;
    }
    const lifecycle = sceneNormalizeLifecycle(item, current);
    const normalized = {
      id: (typeof item.id === "string" && item.id) || current.id || ("scene-light-" + index),
      kind,
      color: typeof item.color === "string" && item.color ? item.color : (typeof current.color === "string" && current.color ? current.color : "#f3fbff"),
      groundColor: typeof item.groundColor === "string" && item.groundColor ? item.groundColor : (typeof current.groundColor === "string" ? current.groundColor : ""),
      intensity: sceneClampNumberOrCSSVar(item.intensity, sceneNumber(current.intensity, sceneDefaultLightIntensity(kind)), 0, 6),
      x: sceneNumber(item.x, sceneNumber(current.x, 0)),
      y: sceneNumber(item.y, sceneNumber(current.y, 0)),
      z: sceneNumber(item.z, sceneNumber(current.z, 0)),
      directionX: sceneNumber(item.directionX, sceneNumber(current.directionX, 0)),
      directionY: sceneNumber(item.directionY, sceneNumber(current.directionY, 0)),
      directionZ: sceneNumber(item.directionZ, sceneNumber(current.directionZ, 0)),
      angle: Math.max(0, Math.min(Math.PI, sceneNumber(item.angle, sceneNumber(current.angle, 0)))),
      penumbra: sceneClamp(sceneNumber(item.penumbra, sceneNumber(current.penumbra, 0)), 0, 1),
      range: Math.max(0, Math.min(256, sceneNumber(item.range, sceneNumber(current.range, (kind === "point" || kind === "spot" || kind === "rect-area") ? 6.5 : 0)))),
      decay: Math.max(0.1, Math.min(8, sceneNumber(item.decay, sceneNumber(current.decay, (kind === "point" || kind === "spot" || kind === "rect-area") ? 1.35 : 1)))),
      width: Math.max(0, sceneNumber(item.width, sceneNumber(current.width, kind === "rect-area" ? 1 : 0))),
      height: Math.max(0, sceneNumber(item.height, sceneNumber(current.height, kind === "rect-area" ? 1 : 0))),
      coefficients: Array.isArray(item.coefficients) ? item.coefficients.slice() : (Array.isArray(current.coefficients) ? current.coefficients.slice() : []),
      castShadow: sceneBool(Object.prototype.hasOwnProperty.call(item, "castShadow") ? item.castShadow : current.castShadow, false),
      shadowBias: sceneNumber(item.shadowBias, sceneNumber(current.shadowBias, 0)),
      shadowSize: Math.max(0, Math.floor(sceneNumber(item.shadowSize, sceneNumber(current.shadowSize, 0)))),
      shadowCascades: kind === "directional"
        ? Math.max(0, Math.min(4, Math.floor(sceneNumber(item.shadowCascades, sceneNumber(current.shadowCascades, 0)))))
        : (kind === "spot"
          ? Math.max(0, Math.min(1, Math.floor(sceneNumber(item.shadowCascades, sceneNumber(current.shadowCascades, 0)))))
          : 0),
      shadowSoftness: Math.max(0, sceneNumber(item.shadowSoftness, sceneNumber(current.shadowSoftness, 0))),
      _transition: lifecycle.transition,
      _inState: lifecycle.inState,
      _outState: lifecycle.outState,
      _live: lifecycle.live,
    };
    if (normalized.kind === "directional" && normalized.directionX === 0 && normalized.directionY === 0 && normalized.directionZ === 0) {
      normalized.directionX = 0.35;
      normalized.directionY = -1;
      normalized.directionZ = -0.4;
    }
    if ((normalized.kind === "spot" || normalized.kind === "rect-area") && normalized.directionX === 0 && normalized.directionY === 0 && normalized.directionZ === 0) {
      normalized.directionY = -1;
    }
    // An unset SpotLight.Angle reaches the browser as 0, because Go's
    // setNumeric drops zero values from the IR. Both backends build the cone
    // from cos(angle), and cos(0) is 1, so a 0 angle admits no direction at all
    // and the light disappears. Give the author a 30 degree cone instead.
    //
    // The default belongs here, not in a renderer: normalizeSceneLight is the
    // one normalization point WebGL and WebGPU share, so both cones stay
    // identical. A per-renderer default would reintroduce the parity gap the
    // seven-light-type work just closed.
    if (normalized.kind === "spot" && !(normalized.angle > 0)) {
      normalized.angle = Math.PI / 6;
    }
    // Cache per-light content hash for scenePBRLightsHash dirty-tracking.
    // Paid here (once per mutation, rare) instead of per-frame inside
    // the hash function — ~13µs per call down to ~100ns in practice.
    if (typeof hashLightContent === "function") {
      normalized._lightHash = hashLightContent(normalized);
    }
    return normalized;
  }

  function normalizeSceneLabel(label, index, fallback) {
    const current = sceneIsPlainObject(fallback) ? fallback : {};
    const item = sceneIsPlainObject(label) ? label : {};
    const lifecycle = sceneNormalizeLifecycle(item, current);
    return {
      id: item.id || current.id || ("scene-label-" + index),
      text: typeof item.text === "string" ? item.text : (typeof current.text === "string" ? current.text : ""),
      className: sceneLabelClassName(item) || sceneLabelClassName(current),
      x: sceneNumber(item.x, sceneNumber(current.x, 0)),
      y: sceneNumber(item.y, sceneNumber(current.y, 0)),
      z: sceneNumber(item.z, sceneNumber(current.z, 0)),
      priority: sceneNumber(item.priority, sceneNumber(current.priority, 0)),
      shiftX: sceneNumber(item.shiftX, sceneNumber(current.shiftX, 0)),
      shiftY: sceneNumber(item.shiftY, sceneNumber(current.shiftY, 0)),
      shiftZ: sceneNumber(item.shiftZ, sceneNumber(current.shiftZ, 0)),
      driftSpeed: sceneNumber(item.driftSpeed, sceneNumber(current.driftSpeed, 0)),
      driftPhase: sceneNumber(item.driftPhase, sceneNumber(current.driftPhase, 0)),
      maxWidth: Math.max(48, sceneNumber(item.maxWidth, sceneNumber(current.maxWidth, 180))),
      maxLines: Math.max(0, Math.floor(sceneNumber(item.maxLines, sceneNumber(current.maxLines, 0)))),
      overflow: normalizeTextLayoutOverflow(item.overflow || current.overflow),
      font: typeof item.font === "string" && item.font ? item.font : (typeof current.font === "string" && current.font ? current.font : '600 13px "IBM Plex Sans", "Segoe UI", sans-serif'),
      lineHeight: Math.max(12, sceneNumber(item.lineHeight, sceneNumber(current.lineHeight, 18))),
      color: typeof item.color === "string" && item.color ? item.color : (typeof current.color === "string" && current.color ? current.color : "#ecf7ff"),
      background: typeof item.background === "string" && item.background ? item.background : (typeof current.background === "string" && current.background ? current.background : "rgba(8, 21, 31, 0.82)"),
      borderColor: typeof item.borderColor === "string" && item.borderColor ? item.borderColor : (typeof current.borderColor === "string" && current.borderColor ? current.borderColor : "rgba(141, 225, 255, 0.24)"),
      offsetX: sceneNumber(item.offsetX, sceneNumber(current.offsetX, 0)),
      offsetY: sceneNumber(item.offsetY, sceneNumber(current.offsetY, -14)),
      anchorX: Math.max(0, Math.min(1, sceneNumber(item.anchorX, sceneNumber(current.anchorX, 0.5)))),
      anchorY: Math.max(0, Math.min(1, sceneNumber(item.anchorY, sceneNumber(current.anchorY, 1)))),
      collision: normalizeSceneLabelCollision(item.collision || current.collision),
      occlude: sceneBool(Object.prototype.hasOwnProperty.call(item, "occlude") ? item.occlude : current.occlude, false),
      whiteSpace: normalizeSceneLabelWhiteSpace(item.whiteSpace || current.whiteSpace),
      textAlign: normalizeSceneLabelAlign(item.textAlign || current.textAlign),
      _transition: lifecycle.transition,
      _inState: lifecycle.inState,
      _outState: lifecycle.outState,
      _live: lifecycle.live,
    };
  }

  function normalizeSceneSpriteFit(value) {
    const mode = typeof value === "string" ? value.trim().toLowerCase() : "";
    switch (mode) {
      case "cover":
        return "cover";
      case "stretch":
      case "fill":
        return "fill";
      default:
        return "contain";
    }
  }

  function normalizeSceneSprite(sprite, index, fallback) {
    const current = sceneIsPlainObject(fallback) ? fallback : {};
    const item = sceneIsPlainObject(sprite) ? sprite : {};
    const width = Math.max(0.05, sceneNumber(item.width, sceneNumber(current.width, 1.25)));
    const height = Math.max(0.05, sceneNumber(item.height, sceneNumber(current.height, width)));
    const scale = Math.max(0.05, sceneNumber(item.scale, sceneNumber(current.scale, 1)));
    const lifecycle = sceneNormalizeLifecycle(item, current);
    return {
      id: item.id || current.id || ("scene-sprite-" + index),
      src: typeof item.src === "string" ? item.src.trim() : (typeof current.src === "string" ? current.src : ""),
      className: sceneLabelClassName(item) || sceneLabelClassName(current),
      x: sceneNumber(item.x, sceneNumber(current.x, 0)),
      y: sceneNumber(item.y, sceneNumber(current.y, 0)),
      z: sceneNumber(item.z, sceneNumber(current.z, 0)),
      priority: sceneNumber(item.priority, sceneNumber(current.priority, 0)),
      shiftX: sceneNumber(item.shiftX, sceneNumber(current.shiftX, 0)),
      shiftY: sceneNumber(item.shiftY, sceneNumber(current.shiftY, 0)),
      shiftZ: sceneNumber(item.shiftZ, sceneNumber(current.shiftZ, 0)),
      driftSpeed: sceneNumber(item.driftSpeed, sceneNumber(current.driftSpeed, 0)),
      driftPhase: sceneNumber(item.driftPhase, sceneNumber(current.driftPhase, 0)),
      width: width,
      height: height,
      scale: scale,
      opacity: clamp01(sceneNumber(item.opacity, sceneNumber(current.opacity, 1))),
      offsetX: sceneNumber(item.offsetX, sceneNumber(current.offsetX, 0)),
      offsetY: sceneNumber(item.offsetY, sceneNumber(current.offsetY, 0)),
      anchorX: sceneClamp(sceneNumber(item.anchorX, sceneNumber(current.anchorX, 0.5)), 0, 1),
      anchorY: sceneClamp(sceneNumber(item.anchorY, sceneNumber(current.anchorY, 0.5)), 0, 1),
      occlude: sceneBool(Object.prototype.hasOwnProperty.call(item, "occlude") ? item.occlude : current.occlude, false),
      fit: normalizeSceneSpriteFit(item.fit || current.fit),
      _transition: lifecycle.transition,
      _inState: lifecycle.inState,
      _outState: lifecycle.outState,
      _live: lifecycle.live,
    };
  }

  function sceneHTMLMarkup(item, current) {
    for (const key of ["html", "markup", "content"]) {
      if (typeof item[key] === "string") {
        return item[key];
      }
    }
    for (const key of ["html", "markup", "content"]) {
      if (typeof current[key] === "string") {
        return current[key];
      }
    }
    return "";
  }

  function normalizeSceneHTMLPointerEvents(value, fallback) {
    const raw = typeof value === "string" ? value.trim().toLowerCase() : "";
    switch (raw) {
      case "auto":
      case "true":
      case "interactive":
        return "auto";
      case "none":
      case "false":
        return "none";
      default:
        return fallback || "none";
    }
  }

  function normalizeSceneHTMLMode(value, fallback) {
    const raw = typeof value === "string" ? value.trim().toLowerCase() : "";
    switch (raw) {
      case "texture":
      case "htmltexture":
        return "texture";
      case "dom":
      case "world":
      case "htmldom":
        return "dom";
      default:
        return fallback || "dom";
    }
  }

  function normalizeSceneHTML(entry, index, fallback) {
    const current = sceneIsPlainObject(fallback) ? fallback : {};
    const item = sceneIsPlainObject(entry) ? entry : {};
    const width = Math.max(0.05, sceneNumber(item.width, sceneNumber(current.width, 1.8)));
    const height = Math.max(0.05, sceneNumber(item.height, sceneNumber(current.height, 0.72)));
    const scale = Math.max(0.05, sceneNumber(item.scale, sceneNumber(current.scale, 1)));
    const lifecycle = sceneNormalizeLifecycle(item, current);
    const id = item.id || current.id || ("scene-html-" + index);
    const mode = normalizeSceneHTMLMode(item.mode, normalizeSceneHTMLMode(current.mode, "dom"));
    const fallbackMode = sceneHTMLStringField(item, current, ["fallback", "fallbackMode"]);
    const fallbackReason = sceneHTMLStringField(item, current, ["fallbackReason", "degradeReason", "degradationReason"]);
    const textureWidth = sceneHTMLTextureDimension(item.textureWidth, current.textureWidth, mode === "texture" ? 512 : 0);
    const textureHeight = sceneHTMLTextureDimension(item.textureHeight, current.textureHeight, mode === "texture" ? 320 : 0);
    const maxTexturePixels = Math.max(0, Math.floor(sceneNumber(item.maxTexturePixels, sceneNumber(current.maxTexturePixels, mode === "texture" ? 1024 * 1024 : 0))));
    return {
      id,
      target: sceneHTMLStringField(item, current, ["target", "targetID"]),
      mode,
      html: sceneHTMLMarkup(item, current),
      className: sceneLabelClassName(item) || sceneLabelClassName(current),
      fallback: fallbackMode || (mode === "texture" ? "dom-overlay" : ""),
      fallbackReason: fallbackReason || (mode === "texture" ? "html-texture-manager-unavailable" : ""),
      textureKey: sceneHTMLStringField(item, current, ["textureKey", "texture", "src"]) || (mode === "texture" ? "gosx-html://" + id : ""),
      textureWidth,
      textureHeight,
      maxTexturePixels,
      textureReady: sceneBool(Object.prototype.hasOwnProperty.call(item, "textureReady") ? item.textureReady : current.textureReady, false),
      surfaceWidth: Math.max(0.05, sceneNumber(item.surfaceWidth, sceneNumber(current.surfaceWidth, width))),
      surfaceHeight: Math.max(0.05, sceneNumber(item.surfaceHeight, sceneNumber(current.surfaceHeight, height))),
      x: sceneNumber(item.x, sceneNumber(current.x, 0)),
      y: sceneNumber(item.y, sceneNumber(current.y, 0)),
      z: sceneNumber(item.z, sceneNumber(current.z, 0)),
      rotationX: sceneNumber(item.rotationX, sceneNumber(current.rotationX, 0)),
      rotationY: sceneNumber(item.rotationY, sceneNumber(current.rotationY, 0)),
      rotationZ: sceneNumber(item.rotationZ, sceneNumber(current.rotationZ, 0)),
      spinX: sceneNumber(item.spinX, sceneNumber(current.spinX, 0)),
      spinY: sceneNumber(item.spinY, sceneNumber(current.spinY, 0)),
      spinZ: sceneNumber(item.spinZ, sceneNumber(current.spinZ, 0)),
      priority: sceneNumber(item.priority, sceneNumber(current.priority, 0)),
      shiftX: sceneNumber(item.shiftX, sceneNumber(current.shiftX, 0)),
      shiftY: sceneNumber(item.shiftY, sceneNumber(current.shiftY, 0)),
      shiftZ: sceneNumber(item.shiftZ, sceneNumber(current.shiftZ, 0)),
      driftSpeed: sceneNumber(item.driftSpeed, sceneNumber(current.driftSpeed, 0)),
      driftPhase: sceneNumber(item.driftPhase, sceneNumber(current.driftPhase, 0)),
      width,
      height,
      scale,
      opacity: clamp01(sceneNumber(item.opacity, sceneNumber(current.opacity, 1))),
      offsetX: sceneNumber(item.offsetX, sceneNumber(current.offsetX, 0)),
      offsetY: sceneNumber(item.offsetY, sceneNumber(current.offsetY, 0)),
      anchorX: sceneClamp(sceneNumber(item.anchorX, sceneNumber(current.anchorX, 0.5)), 0, 1),
      anchorY: sceneClamp(sceneNumber(item.anchorY, sceneNumber(current.anchorY, 0.5)), 0, 1),
      occlude: sceneBool(Object.prototype.hasOwnProperty.call(item, "occlude") ? item.occlude : current.occlude, false),
      pointerEvents: normalizeSceneHTMLPointerEvents(item.pointerEvents, normalizeSceneHTMLPointerEvents(current.pointerEvents, "none")),
      _transition: lifecycle.transition,
      _inState: lifecycle.inState,
      _outState: lifecycle.outState,
      _live: lifecycle.live,
    };
  }

  function sceneHTMLTextureDimension(value, fallback, defaultValue) {
    const number = sceneNumber(value, sceneNumber(fallback, defaultValue));
    if (number <= 0) {
      return 0;
    }
    return Math.max(1, Math.floor(number));
  }

  // Texture decodes are asynchronous. Static scenes need a frame scheduled
  // after a decoded surface replaces its placeholder, and multiple mounts may
  // be listening at once.
  const sceneTextureLoadListeners = new Set();

  function onSceneTextureLoaded(listener) {
    if (typeof listener !== "function") {
      return function() {};
    }
    sceneTextureLoadListeners.add(listener);
    return function() {
      sceneTextureLoadListeners.delete(listener);
    };
  }

  function notifySceneTextureLoaded(src, loaded) {
    sceneTextureLoadListeners.forEach(function(listener) {
      try {
        listener(src, loaded !== false);
      } catch (_err) {
        // One mount's scheduler must not stop another's.
      }
    });
  }

  function sceneHTMLStringField(item, current, keys) {
    for (const key of keys) {
      if (typeof item[key] === "string" && item[key].trim()) {
        return item[key].trim();
      }
    }
    for (const key of keys) {
      if (typeof current[key] === "string" && current[key].trim()) {
        return current[key].trim();
      }
    }
    return "";
  }

  function sceneLabelClassName(item) {
    if (!item || typeof item !== "object") {
      return "";
    }
    if (typeof item.className === "string" && item.className.trim()) {
      return item.className.trim();
    }
    if (typeof item.class === "string" && item.class.trim()) {
      return item.class.trim();
    }
    return "";
  }

  function normalizeSceneKind(value) {
    const kind = typeof value === "string" ? value.trim().toLowerCase() : "";
    switch (kind) {
      case "boxgeometry":
        return "box";
      case "cubegeometry":
        return "cube";
      case "planegeometry":
      case "quad":
      case "quadgeometry":
        return "plane";
      case "pyramidgeometry":
        return "pyramid";
      case "spheregeometry":
      case "uvsphere":
      case "uvspheregeometry":
        return "sphere";
      case "cylindergeometry":
        return "cylinder";
      case "conegeometry":
        return "cone";
      case "torusgeometry":
        return "torus";
      case "torusknotgeometry":
      case "torus-knot":
        return "torusknot";
      case "box":
      case "cube":
      case "lines":
      case "plane":
      case "pyramid":
      case "sphere":
      case "cylinder":
      case "cone":
      case "torus":
      case "torusknot":
      case "gltf-mesh":
        return kind;
      default:
        return "cube";
    }
  }


  function sceneObjects(props) {
    return rawSceneObjects(props).map(function(object, index) {
      return normalizeSceneObject(object, index, null);
    });
  }

  function normalizeSceneModel(item, index) {
    const current = item && typeof item === "object" ? item : {};
    const previewSrc = typeof current.previewSrc === "string" && current.previewSrc.trim() ? current.previewSrc.trim() : "";
    const fullSrc = typeof current.fullSrc === "string" && current.fullSrc.trim() ? current.fullSrc.trim() : "";
    const progressive = Boolean(current.progressive && previewSrc && fullSrc);
    const scaleSource = current.scale && typeof current.scale === "object" ? current.scale : null;
    const hasStatic = Object.prototype.hasOwnProperty.call(current, "static");
    const hasPickable = Object.prototype.hasOwnProperty.call(current, "pickable");
    const hasVisible = Object.prototype.hasOwnProperty.call(current, "visible");
    const hasCastShadow = Object.prototype.hasOwnProperty.call(current, "castShadow");
    const hasReceiveShadow = Object.prototype.hasOwnProperty.call(current, "receiveShadow");
    const hasAnimationSpeed = Object.prototype.hasOwnProperty.call(current, "animationSpeed");
    const hasAnimationWeight = Object.prototype.hasOwnProperty.call(current, "animationWeight");
    const hasAnimationFadeInMS = Object.prototype.hasOwnProperty.call(current, "animationFadeInMS");
    const hasAnimationFadeOutMS = Object.prototype.hasOwnProperty.call(current, "animationFadeOutMS");
    const override = {};
    const lifecycle = sceneNormalizeLifecycle(current, null);
    const materialKind = sceneObjectMaterialKindValue(current);
    if (materialKind) {
      override.materialKind = normalizeSceneMaterialKind(materialKind);
    }
    const materialName = typeof current.material === "string" && current.material.trim() ? current.material.trim() : "";
    if (sceneObjectMaterialHasValue(current, "color")) {
      override.color = sceneObjectMaterialValue(current, "color");
    }
    if (sceneObjectMaterialHasValue(current, "texture")) {
      override.texture = sceneObjectMaterialValue(current, "texture");
    }
    if (sceneObjectMaterialHasValue(current, "opacity")) {
      override.opacity = sceneObjectMaterialValue(current, "opacity");
    }
    if (sceneObjectMaterialHasValue(current, "emissive")) {
      override.emissive = sceneObjectMaterialValue(current, "emissive");
    }
    if (sceneObjectMaterialHasValue(current, "roughness")) {
      override.roughness = sceneObjectMaterialValue(current, "roughness");
    }
    if (sceneObjectMaterialHasValue(current, "metalness")) {
      override.metalness = sceneObjectMaterialValue(current, "metalness");
    }
    if (sceneObjectMaterialHasValue(current, "unlit")) {
      const rawUnlit = sceneObjectMaterialValue(current, "unlit");
      const unlit = sceneBool(rawUnlit, undefined);
      if (unlit !== undefined) {
        override.unlit = unlit;
      }
    }
    // Authored ior is normalized under the KHR contract (CSS var text
    // trimmed, explicit zero preserved) while genuine absence stays absent.
    if (sceneObjectMaterialHasValue(current, "ior")) {
      override.ior = sceneNormalizeMaterialIor(sceneObjectMaterialValue(current, "ior"), 1.5);
    }
    // Authored specular factors are normalized (CSS var text trimmed,
    // explicit zero/black preserved) while genuine absence stays absent from
    // the override bag.
    if (sceneObjectMaterialHasValue(current, "specularIntensity")) {
      override.specularIntensity = sceneNormalizeMaterialSpecularIntensity(sceneObjectMaterialValue(current, "specularIntensity"), 1);
    }
    if (sceneObjectMaterialHasValue(current, "specularColor")) {
      override.specularColor = sceneNormalizeMaterialSpecularColor(sceneObjectMaterialValue(current, "specularColor"), null);
    }
    // Authored alpha cutoffs follow the shared KHR mask contract: a defined
    // value is normalized (explicit null disables masking) while genuine
    // absence — including an own undefined field — must never erase imported
    // asset masking.
    if (sceneObjectMaterialValue(current, "alphaCutoff") !== undefined) {
      override.alphaCutoff = sceneNormalizeMaterialAlphaCutoff(sceneObjectMaterialValue(current, "alphaCutoff"), null);
    }
    for (const key of ["clearcoat", "sheen", "transmission", "iridescence", "anisotropy"]) {
      if (sceneObjectMaterialHasValue(current, key)) {
        override[key] = sceneObjectMaterialValue(current, key);
      }
    }
    if (sceneObjectBlendModeHasValue(current)) {
      override.blendMode = sceneObjectBlendModeValue(current);
    }
    if (sceneObjectMaterialHasValue(current, "renderPass")) {
      override.renderPass = sceneObjectMaterialValue(current, "renderPass");
    }
    if (sceneObjectMaterialHasValue(current, "wireframe")) {
      override.wireframe = sceneObjectMaterialValue(current, "wireframe");
    }
    for (const key of ["customVertex", "customFragment", "customVertexWGSL", "customFragmentWGSL", "customUniforms", "shaderBackend", "shaderLayout", "shaderSource", "shaderSourceFiles"]) {
      if (sceneObjectMaterialHasValue(current, key)) {
        override[key] = sceneObjectMaterialValue(current, key);
      }
    }
    const model = {
      id: typeof current.id === "string" && current.id.trim() ? current.id.trim() : ("scene-model-" + index),
      src: progressive ? previewSrc : (typeof current.src === "string" && current.src.trim() ? current.src.trim() : ""),
      previewSrc,
      fullSrc,
      progressive,
      material: materialName,
      x: sceneNumber(current.x, 0),
      y: sceneNumber(current.y, 0),
      z: sceneNumber(current.z, 0),
      rotationX: sceneNumber(current.rotationX, 0),
      rotationY: sceneNumber(current.rotationY, 0),
      rotationZ: sceneNumber(current.rotationZ, 0),
      scaleX: sceneNumber(current.scaleX, sceneNumber(scaleSource ? scaleSource.x : undefined, sceneNumber(current.scale, 1))),
      scaleY: sceneNumber(current.scaleY, sceneNumber(scaleSource ? scaleSource.y : undefined, sceneNumber(current.scale, 1))),
      scaleZ: sceneNumber(current.scaleZ, sceneNumber(scaleSource ? scaleSource.z : undefined, sceneNumber(current.scale, 1))),
      parentMatrix: sceneNormalizeParentMatrix(current.parentMatrix),
      bounds: Math.max(0, sceneNumber(current.bounds, 0)),
      fit: typeof current.fit === "string" ? current.fit.trim() : "",
      fitAlign: typeof current.fitAlign === "string" ? current.fitAlign.trim() : "",
      animation: typeof current.animation === "string" && current.animation.trim() ? current.animation.trim() : "",
      animationSeq: typeof current.animationSeq === "string" ? current.animationSeq : "",
      loop: Object.prototype.hasOwnProperty.call(current, "loop") ? sceneBool(current.loop, true) : true,
      pickable: hasPickable ? sceneBool(current.pickable, false) : undefined,
      visible: hasVisible ? sceneBool(current.visible, true) : true,
      static: hasStatic ? sceneBool(current.static, false) : null,
      castShadow: hasCastShadow ? sceneBool(current.castShadow, false) : undefined,
      receiveShadow: hasReceiveShadow ? sceneBool(current.receiveShadow, false) : undefined,
      lodGroup: typeof current.lodGroup === "string" && current.lodGroup ? current.lodGroup : "",
      lodLevel: Math.max(0, Math.floor(sceneNumber(current.lodLevel, 0))),
      lodMinDistance: Math.max(0, sceneNumber(current.lodMinDistance, 0)),
      lodMaxDistance: Math.max(0, sceneNumber(current.lodMaxDistance, 0)),
      materialOverride: Object.keys(override).length > 0 ? override : null,
      _transition: lifecycle.transition,
      _inState: lifecycle.inState,
      _outState: lifecycle.outState,
      _live: lifecycle.live,
    };
    if (hasAnimationSpeed) {
      model.animationSpeed = Math.max(0, sceneNumber(current.animationSpeed, 1));
    }
    if (hasAnimationWeight) {
      model.animationWeight = Math.max(0, sceneNumber(current.animationWeight, 1));
    }
    if (hasAnimationFadeInMS) {
      model.animationFadeInMS = Math.max(0, sceneNumber(current.animationFadeInMS, 0));
    }
    if (hasAnimationFadeOutMS) {
      model.animationFadeOutMS = Math.max(0, sceneNumber(current.animationFadeOutMS, 0));
    }
    return model;
  }

  function normalizeSceneInstancedGLBInstance(item, index) {
    const current = item && typeof item === "object" ? item : {};
    const scaleSource = current.scale && typeof current.scale === "object" ? current.scale : null;
    const rawID = typeof current.id === "string" && current.id.trim() ? current.id.trim() : "";
    return {
      id: rawID || ("instance-" + index),
      x: sceneNumber(current.x, 0),
      y: sceneNumber(current.y, 0),
      z: sceneNumber(current.z, 0),
      rotationX: sceneNumber(current.rotationX, 0),
      rotationY: sceneNumber(current.rotationY, 0),
      rotationZ: sceneNumber(current.rotationZ, 0),
      scaleX: sceneNumber(current.scaleX, sceneNumber(scaleSource ? scaleSource.x : undefined, sceneNumber(current.scale, 1))),
      scaleY: sceneNumber(current.scaleY, sceneNumber(scaleSource ? scaleSource.y : undefined, sceneNumber(current.scale, 1))),
      scaleZ: sceneNumber(current.scaleZ, sceneNumber(scaleSource ? scaleSource.z : undefined, sceneNumber(current.scale, 1))),
      parentMatrix: sceneNormalizeParentMatrix(current.parentMatrix),
    };
  }

  function normalizeSceneInstancedGLBMeshEntry(item, index, fallback) {
    const current = fallback && typeof fallback === "object" ? fallback : {};
    const raw = item && typeof item === "object" ? item : {};
    const materialKind = sceneObjectMaterialKindValue(raw) || current.materialKind || "";
    const lifecycle = sceneNormalizeLifecycle(raw, current);
    const rawInstances = Array.isArray(raw.instances)
      ? raw.instances
      : (Array.isArray(current.instances) ? current.instances : []);
    const batch = {
      id: typeof raw.id === "string" && raw.id.trim() ? raw.id.trim() : (typeof current.id === "string" && current.id ? current.id : ("scene-instanced-glb-" + index)),
      src: typeof raw.src === "string" && raw.src.trim() ? raw.src.trim() : (typeof current.src === "string" ? current.src : ""),
      material: typeof raw.material === "string" && raw.material ? raw.material : (typeof current.material === "string" ? current.material : ""),
      materialKind: materialKind ? normalizeSceneMaterialKind(materialKind) : "",
      color: sceneObjectMaterialHasValue(raw, "color") ? sceneObjectMaterialValue(raw, "color") : current.color,
      texture: typeof sceneObjectMaterialValue(raw, "texture") === "string" ? sceneObjectMaterialValue(raw, "texture").trim() : (typeof current.texture === "string" ? current.texture : ""),
      opacity: sceneObjectMaterialHasValue(raw, "opacity") ? sceneClampNumberOrCSSVar(sceneObjectMaterialValue(raw, "opacity"), sceneNumber(current.opacity, 1), 0, 1) : current.opacity,
      emissive: sceneObjectMaterialHasValue(raw, "emissive") ? sceneClampNumberOrCSSVar(sceneObjectMaterialValue(raw, "emissive"), sceneNumber(current.emissive, 0), 0, 1) : current.emissive,
      blendMode: normalizeSceneMaterialBlendMode(
        sceneObjectMaterialHasValue(raw, "blendMode") ? sceneObjectMaterialValue(raw, "blendMode") : current.blendMode,
        materialKind || current.materialKind || "flat",
        sceneNumber(sceneObjectMaterialValue(raw, "opacity"), sceneNumber(current.opacity, 1)),
      ),
      roughness: sceneObjectMaterialHasValue(raw, "roughness") ? sceneNumberOrCSSVar(sceneObjectMaterialValue(raw, "roughness"), sceneNumber(current.roughness, 0.5)) : current.roughness,
      metalness: sceneObjectMaterialHasValue(raw, "metalness") ? sceneNumberOrCSSVar(sceneObjectMaterialValue(raw, "metalness"), sceneNumber(current.metalness, 0)) : current.metalness,
      ior: sceneObjectMaterialHasValue(raw, "ior")
        ? sceneNormalizeMaterialIor(sceneObjectMaterialValue(raw, "ior"), current.ior)
        : (Object.prototype.hasOwnProperty.call(current, "ior")
          ? sceneNormalizeMaterialIor(current.ior, 1.5)
          : undefined),
      specularIntensity: sceneObjectMaterialHasValue(raw, "specularIntensity")
        ? sceneNormalizeMaterialSpecularIntensity(sceneObjectMaterialValue(raw, "specularIntensity"), current.specularIntensity)
        : (Object.prototype.hasOwnProperty.call(current, "specularIntensity")
          ? sceneNormalizeMaterialSpecularIntensity(current.specularIntensity, 1)
          : undefined),
      specularColor: sceneObjectMaterialHasValue(raw, "specularColor")
        ? sceneNormalizeMaterialSpecularColor(sceneObjectMaterialValue(raw, "specularColor"), current.specularColor)
        : (Object.prototype.hasOwnProperty.call(current, "specularColor")
          ? sceneNormalizeMaterialSpecularColor(current.specularColor, null)
          : undefined),
      pickable: Object.prototype.hasOwnProperty.call(raw, "pickable") ? sceneBool(raw.pickable, false) : current.pickable,
      visible: Object.prototype.hasOwnProperty.call(raw, "visible")
        ? sceneBool(raw.visible, true)
        : (Object.prototype.hasOwnProperty.call(current, "visible") ? sceneBool(current.visible, true) : true),
      static: Object.prototype.hasOwnProperty.call(raw, "static") ? sceneBool(raw.static, false) : current.static,
      instances: rawInstances.map(function(instance, instanceIndex) {
        return normalizeSceneInstancedGLBInstance(instance, instanceIndex);
      }),
      _transition: lifecycle.transition,
      _inState: lifecycle.inState,
      _outState: lifecycle.outState,
      _live: lifecycle.live,
    };
    // A genuinely omitted ior (no authored raw value and no inherited field)
    // stays absent so the override plumbing cannot erase an authored glTF
    // ior with a defaulted value; explicit/inherited fields are normalized.
    if (batch.ior === undefined) {
      delete batch.ior;
    }
    // A genuinely omitted specular factor (no authored raw value and no
    // inherited field) stays absent for the same reason: the override
    // plumbing must not erase asset-authored specular factors with
    // defaulted values.
    if (batch.specularIntensity === undefined) {
      delete batch.specularIntensity;
    }
    if (batch.specularColor === undefined) {
      delete batch.specularColor;
    }
    // Unlit follows the raw-or-inherited contract: a defined raw or current
    // value is normalized through sceneBool, and the batch field is assigned
    // only when the boolean result is defined so plumbing never erases an
    // imported asset-authored true.
    const batchUnlit = sceneBool(
      sceneObjectMaterialValue(raw, "unlit"),
      sceneBool(current.unlit, undefined));
    if (batchUnlit !== undefined) {
      batch.unlit = batchUnlit;
    }
    // Alpha cutoff follows the shared mask contract: a defined raw (nested or
    // direct) or inherited current value is normalized with the inherited
    // fallback, explicit null disables, and genuine absence stays absent so
    // asset-authored masking is never erased by defaulted plumbing.
    const rawAlphaCutoff = sceneObjectMaterialValue(raw, "alphaCutoff");
    if (rawAlphaCutoff !== undefined) {
      batch.alphaCutoff = sceneNormalizeMaterialAlphaCutoff(
        rawAlphaCutoff,
        current.alphaCutoff !== undefined ? current.alphaCutoff : null);
    } else if (current.alphaCutoff !== undefined) {
      batch.alphaCutoff = sceneNormalizeMaterialAlphaCutoff(current.alphaCutoff, null);
    }
    return batch;
  }

  function sceneInstancedGLBMeshes(props) {
    return rawSceneInstancedGLBMeshes(props)
      .map(function(entry, index) {
        return normalizeSceneInstancedGLBMeshEntry(entry, index, null);
      })
      .filter(function(entry) {
        return Boolean(entry && entry.src && Array.isArray(entry.instances) && entry.instances.length > 0);
      });
  }

  function sceneInstancedGLBMeshToModels(batch, batchIndex) {
    if (!batch || !batch.src || !Array.isArray(batch.instances)) {
      return [];
    }
    const models = [];
    for (let index = 0; index < batch.instances.length; index += 1) {
      const instance = batch.instances[index];
      if (!instance) {
        continue;
      }
      const raw = {
        id: batch.id + "/" + (instance.id || ("instance-" + index)),
        src: batch.src,
        x: instance.x,
        y: instance.y,
        z: instance.z,
        rotationX: instance.rotationX,
        rotationY: instance.rotationY,
        rotationZ: instance.rotationZ,
        scaleX: instance.scaleX,
        scaleY: instance.scaleY,
        scaleZ: instance.scaleZ,
        parentMatrix: instance.parentMatrix,
      };
      for (const key of ["material", "materialKind", "color", "texture", "opacity", "emissive", "blendMode", "roughness", "metalness", "ior", "specularIntensity", "specularColor", "unlit", "pickable", "visible", "static"]) {
        if (batch[key] !== undefined && batch[key] !== null && batch[key] !== "") {
          raw[key] = batch[key];
        }
      }
      // Alpha cutoff is copied separately so an explicit null (masking
      // disabled) survives: the legacy loop above deliberately excludes null.
      if (batch.alphaCutoff !== undefined) {
        raw.alphaCutoff = batch.alphaCutoff;
      }
      models.push(normalizeSceneModel(raw, batchIndex + "-" + index));
    }
    return models;
  }

  function sceneInstancedGLBModelsFromBatches(batches) {
    const models = [];
    const entries = Array.isArray(batches) ? batches : [];
    for (let index = 0; index < entries.length; index += 1) {
      const batchModels = sceneInstancedGLBMeshToModels(entries[index], index);
      for (let modelIndex = 0; modelIndex < batchModels.length; modelIndex += 1) {
        models.push(batchModels[modelIndex]);
      }
    }
    return models;
  }

  function normalizeSceneAnimationClip(item, index) {
    const raw = item && typeof item === "object" ? item : {};
    const channels = Array.isArray(raw.channels) ? raw.channels.map(function(channel) {
      const ch = channel && typeof channel === "object" ? channel : {};
      return {
        targetID: ch.targetID != null ? ch.targetID : ch.targetNode,
        targetNode: ch.targetNode != null ? ch.targetNode : ch.targetID,
        property: typeof ch.property === "string" && ch.property ? ch.property : "translation",
        interpolation: typeof ch.interpolation === "string" && ch.interpolation ? ch.interpolation : "LINEAR",
        times: ch.times instanceof Float32Array ? new Float32Array(ch.times) : sceneTypedFloatArray(ch.times),
        values: ch.values instanceof Float32Array ? new Float32Array(ch.values) : sceneTypedFloatArray(ch.values),
      };
    }) : [];
    return {
      name: typeof raw.name === "string" && raw.name ? raw.name : ("scene-animation-" + index),
      duration: sceneNumber(raw.duration, 0),
      channels,
    };
  }

  function sceneAnimations(props) {
    return rawSceneAnimations(props).map(function(entry, index) {
      return normalizeSceneAnimationClip(entry, index);
    });
  }

  function sceneModels(props) {
    return rawSceneModels(props)
      .map(function(model, index) {
        return normalizeSceneModel(model, index);
      })
      .filter(function(model) {
        return Boolean(model && model.src);
      });
  }

  function sceneLights(props) {
    return rawSceneLights(props)
      .map(function(light, index) {
        return normalizeSceneLight(light, index, null);
      })
      .filter(Boolean);
  }

  function normalizeSceneLabelWhiteSpace(value) {
    const mode = typeof value === "string" ? value.trim().toLowerCase() : "";
    switch (mode) {
      case "pre-wrap":
        return "pre-wrap";
      case "pre":
        return "pre";
      default:
        return "normal";
    }
  }

  function normalizeSceneLabelAlign(value) {
    const align = typeof value === "string" ? value.trim().toLowerCase() : "";
    switch (align) {
      case "left":
      case "start":
        return "left";
      case "right":
      case "end":
        return "right";
      default:
        return "center";
    }
  }

  function normalizeSceneLabelCollision(value) {
    const mode = typeof value === "string" ? value.trim().toLowerCase() : "";
    switch (mode) {
      case "allow":
      case "none":
      case "overlap":
        return "allow";
      default:
        return "avoid";
    }
  }

  function sceneLabels(props) {
    const raw = rawSceneLabels(props);
    return raw
      .map(function(label, index) {
        return normalizeSceneLabel(label, index, null);
      })
      .filter(function(label) {
        return label.text.trim() !== "";
      });
  }

  function sceneSprites(props) {
    return rawSceneSprites(props)
      .map(function(sprite, index) {
        return normalizeSceneSprite(sprite, index, null);
      })
      .filter(function(sprite) {
        return sprite.src !== "";
      });
  }

  function sceneHTML(props) {
    return rawSceneHTML(props)
      .map(function(entry, index) {
        return normalizeSceneHTML(entry, index, null);
      })
      .filter(function(entry) {
        return entry.html.trim() !== "";
      });
  }

  function normalizeScenePointStyle(value, fallback) {
    const current = typeof fallback === "string" && fallback ? fallback : "square";
    const raw = typeof value === "string" ? value.trim().toLowerCase() : "";
    switch (raw) {
      case "focus":
      case "focused":
      case "focus-star":
        return "focus";
      case "glow":
      case "gas":
      case "cloud":
      case "nebula":
        return "glow";
      case "square":
      case "pixel":
      case "hard":
      case "block":
      case "blocky":
        return "square";
      default:
        return current;
    }
  }

  function scenePointStyleCode(value) {
    const style = normalizeScenePointStyle(value, "square");
    if (style === "focus") return 1;
    if (style === "glow") return 2;
    return 0;
  }

  function sceneIsNumericTypedArray(value) {
    return value &&
      typeof value === "object" &&
      typeof value.length === "number" &&
      typeof ArrayBuffer !== "undefined" &&
      typeof ArrayBuffer.isView === "function" &&
      ArrayBuffer.isView(value) &&
      Object.prototype.toString.call(value) !== "[object DataView]";
  }

  function scenePointDataBuffer(value, cloneArrays) {
    if (Array.isArray(value)) {
      return cloneArrays ? value.slice() : value;
    }
    if (sceneIsNumericTypedArray(value)) {
      return value;
    }
    return null;
  }

  function scenePointDataLength(value) {
    return value && typeof value.length === "number" ? value.length : 0;
  }

  function sceneFloat32PointData(value) {
    if (!sceneIsNumericTypedArray(value)) {
      return null;
    }
    return value instanceof Float32Array ? value : new Float32Array(value);
  }

  function sceneRGBAFloat32PointColors(value, count) {
    if (!sceneIsNumericTypedArray(value)) {
      return null;
    }
    if (value.length >= count * 4) {
      return value instanceof Float32Array ? value : new Float32Array(value);
    }
    if (value.length < count * 3) {
      return null;
    }
    const colors = new Float32Array(count * 4);
    for (let i = 0; i < count; i += 1) {
      colors[i * 4] = value[i * 3];
      colors[i * 4 + 1] = value[i * 3 + 1];
      colors[i * 4 + 2] = value[i * 3 + 2];
      colors[i * 4 + 3] = 1;
    }
    return colors;
  }

  function normalizeScenePointsEntry(entry, index, fallback) {
    const current = sceneIsPlainObject(fallback) ? fallback : {};
    const item = sceneIsPlainObject(entry) ? entry : {};
    const lifecycle = sceneNormalizeLifecycle(item, current);
    const itemPositions = scenePointDataBuffer(item.positions, true);
    const currentPositions = scenePointDataBuffer(current.positions, false);
    const itemSizes = scenePointDataBuffer(item.sizes, true);
    const currentSizes = scenePointDataBuffer(current.sizes, false);
    const itemColors = scenePointDataBuffer(item.colors, true);
    const currentColors = scenePointDataBuffer(current.colors, false);
    const positions = itemPositions !== null ? itemPositions : (currentPositions !== null ? currentPositions : []);
    const sizes = itemSizes !== null ? itemSizes : (currentSizes !== null ? currentSizes : []);
    const colors = itemColors !== null ? itemColors : (currentColors !== null ? currentColors : []);
    const normalized = {
      id: item.id || current.id || ("scene-points-" + index),
      count: Math.max(0, Math.floor(sceneNumber(item.count, sceneNumber(current.count, scenePointDataLength(positions) >= 3 ? Math.floor(scenePointDataLength(positions) / 3) : 0)))),
      positions,
      sizes,
      colors,
      material: typeof item.material === "string" && item.material ? item.material : (typeof current.material === "string" ? current.material : ""),
      color: typeof item.color === "string" && item.color ? item.color : (typeof current.color === "string" ? current.color : "#ffffff"),
      style: normalizeScenePointStyle(item.style, current.style),
      size: sceneClampNumberOrCSSVar(item.size, sceneNumber(current.size, 1), 0, Number.POSITIVE_INFINITY),
      minPixelSize: sceneClampNumberOrCSSVar(item.minPixelSize, sceneNumber(current.minPixelSize, 0), 0, Number.POSITIVE_INFINITY),
      opacity: sceneClampNumberOrCSSVar(item.opacity, sceneNumber(current.opacity, 1), 0, 1),
      blendMode: normalizeSceneMaterialBlendMode(item.blendMode || current.blendMode, "flat", sceneNumber(item.opacity, sceneNumber(current.opacity, 1))),
      depthWrite: Object.prototype.hasOwnProperty.call(item, "depthWrite") ? sceneBool(item.depthWrite, true) : current.depthWrite,
      attenuation: sceneBool(Object.prototype.hasOwnProperty.call(item, "attenuation") ? item.attenuation : current.attenuation, false),
      maxPixelSize: sceneClampNumberOrCSSVar(item.maxPixelSize, sceneNumber(current.maxPixelSize, 0), 0, Number.POSITIVE_INFINITY),
      x: sceneNumber(item.x, sceneNumber(current.x, 0)),
      y: sceneNumber(item.y, sceneNumber(current.y, 0)),
      z: sceneNumber(item.z, sceneNumber(current.z, 0)),
      rotationX: sceneNumber(item.rotationX, sceneNumber(current.rotationX, 0)),
      rotationY: sceneNumber(item.rotationY, sceneNumber(current.rotationY, 0)),
      rotationZ: sceneNumber(item.rotationZ, sceneNumber(current.rotationZ, 0)),
      parentMatrix: sceneNormalizeParentMatrix(item.parentMatrix, current.parentMatrix),
      spinX: sceneNumber(item.spinX, sceneNumber(current.spinX, 0)),
      spinY: sceneNumber(item.spinY, sceneNumber(current.spinY, 0)),
      spinZ: sceneNumber(item.spinZ, sceneNumber(current.spinZ, 0)),
      // qualityGroup: G2 QualityLadder layer tagging (see scene.Points.QualityGroup
      // / QualityRung.LayerGroups). Empty means unconditionally visible at
      // every rung — a ladder only gates points layers that opted in. Read by
      // the mount layer's per-frame points filter (sceneFilterPointsByQualityGroups
      // in 20-scene-mount.js), never here — this normalizer only carries the
      // tag through. Same trim/default-"" pattern as the mesh normalizer's
      // qualityGroup field above.
      qualityGroup: typeof item.qualityGroup === "string" && item.qualityGroup.trim()
        ? item.qualityGroup.trim()
        : (typeof current.qualityGroup === "string" ? current.qualityGroup : ""),
      // Inline authored material. The mesh, instanced-mesh and model
      // normalizers all carry these fields through; points did not, and
      // sceneStatePointsWithMaterials only re-attaches NAMED materials
      // (state.materials, keyed by a string point.material). A points layer
      // that authored its own shader inline therefore reached the renderer
      // stripped down to this whitelist, so both backends fell through to
      // the BUILTIN points program: no twinkle, no depth wrap, no per-star
      // impulse. Nothing reported a failure either — the shader was never
      // handed to the GPU to fail — and the render loop still ran, because
      // sceneHasTimeDrivenMaterials reads the RAW props scene. The result
      // was a field that redrew an identical frame forever.
      customVertex: typeof item.customVertex === "string" ? item.customVertex : (typeof current.customVertex === "string" ? current.customVertex : ""),
      customFragment: typeof item.customFragment === "string" ? item.customFragment : (typeof current.customFragment === "string" ? current.customFragment : ""),
      customVertexWGSL: typeof item.customVertexWGSL === "string" ? item.customVertexWGSL : (typeof current.customVertexWGSL === "string" ? current.customVertexWGSL : ""),
      customFragmentWGSL: typeof item.customFragmentWGSL === "string" ? item.customFragmentWGSL : (typeof current.customFragmentWGSL === "string" ? current.customFragmentWGSL : ""),
      shaderBackend: typeof item.shaderBackend === "string" ? item.shaderBackend : (typeof current.shaderBackend === "string" ? current.shaderBackend : ""),
      shaderSource: typeof item.shaderSource === "string" ? item.shaderSource : (typeof current.shaderSource === "string" ? current.shaderSource : ""),
      customUniforms: sceneIsPlainObject(item.customUniforms)
        ? Object.assign({}, item.customUniforms)
        : (sceneIsPlainObject(current.customUniforms) ? Object.assign({}, current.customUniforms) : null),
      shaderLayout: sceneIsPlainObject(item.shaderLayout)
        ? sceneCloneData(item.shaderLayout)
        : (sceneIsPlainObject(current.shaderLayout) ? sceneCloneData(current.shaderLayout) : null),
      shaderSourceFiles: sceneIsPlainObject(item.shaderSourceFiles)
        ? sceneCloneData(item.shaderSourceFiles)
        : (sceneIsPlainObject(current.shaderSourceFiles) ? sceneCloneData(current.shaderSourceFiles) : null),
      _transition: lifecycle.transition,
      _inState: lifecycle.inState,
      _outState: lifecycle.outState,
      _live: lifecycle.live,
    };
    if (positions === current.positions && current._cachedPos) {
      normalized._cachedPos = current._cachedPos;
    }
    if (sizes === current.sizes && current._cachedSizes) {
      normalized._cachedSizes = current._cachedSizes;
    }
    if (colors === current.colors && current._cachedColors) {
      normalized._cachedColors = current._cachedColors;
    }
    if (!normalized._cachedPos && sceneIsNumericTypedArray(positions) && scenePointDataLength(positions) >= normalized.count * 3) {
      normalized._cachedPos = sceneFloat32PointData(positions);
    }
    if (!normalized._cachedSizes && sceneIsNumericTypedArray(sizes) && scenePointDataLength(sizes) >= normalized.count) {
      normalized._cachedSizes = sceneFloat32PointData(sizes);
    }
    if (!normalized._cachedColors && sceneIsNumericTypedArray(colors)) {
      normalized._cachedColors = sceneRGBAFloat32PointColors(colors, normalized.count);
    }
    if (Array.isArray(item.previewPositions)) {
      normalized.previewPositions = item.previewPositions.slice();
    } else if (Array.isArray(current.previewPositions)) {
      normalized.previewPositions = current.previewPositions;
    }
    if (Array.isArray(item.previewSizes)) {
      normalized.previewSizes = item.previewSizes.slice();
    } else if (Array.isArray(current.previewSizes)) {
      normalized.previewSizes = current.previewSizes;
    }
    return normalized;
  }

  function scenePoints(props) {
    return rawScenePoints(props).map(function(entry, index) {
      return normalizeScenePointsEntry(entry, index, null);
    });
  }

  function normalizeSceneInstancedMeshEntry(entry, index, fallback) {
    const current = sceneIsPlainObject(fallback) ? fallback : {};
    const item = sceneIsPlainObject(entry) ? entry : {};
    const lifecycle = sceneNormalizeLifecycle(item, current);
    const transforms = Array.isArray(item.transforms) ? item.transforms.slice() : (Array.isArray(current.transforms) ? current.transforms : []);
    const colors = Object.prototype.hasOwnProperty.call(item, "colors")
      ? sceneCloneData(item.colors)
      : (Object.prototype.hasOwnProperty.call(current, "colors") ? current.colors : []);
    const attributes = Object.prototype.hasOwnProperty.call(item, "attributes")
      ? sceneCloneData(item.attributes)
      : (Object.prototype.hasOwnProperty.call(current, "attributes") ? current.attributes : undefined);
    const kind = normalizeSceneKind(item.kind || current.kind);
    const size = Math.max(0.0001, sceneNumber(item.size, sceneNumber(current.size, 1.2)));
    const radius = Math.max(0.0001, sceneNumber(item.radius, sceneNumber(current.radius, kind === "torus" ? 0.7 : (size * 0.5))));
    const segmentSource = Object.prototype.hasOwnProperty.call(item, "segments") ? item.segments : current.segments;
    const radialSegmentSource = Object.prototype.hasOwnProperty.call(item, "radialSegments") ? item.radialSegments : current.radialSegments;
    const tubularSegmentSource = Object.prototype.hasOwnProperty.call(item, "tubularSegments") ? item.tubularSegments : current.tubularSegments;
    const materialKind = normalizeSceneMaterialKind(sceneObjectMaterialKindValue(item) || current.materialKind);
    const opacity = sceneClampNumberOrCSSVar(sceneObjectMaterialValue(item, "opacity"), sceneNumber(current.opacity, sceneDefaultMaterialOpacity(materialKind)), 0, 1);
    const unlit = sceneBool(sceneObjectMaterialValue(item, "unlit"), sceneBool(current.unlit, false));
    const numericOpacity = sceneNumber(opacity, sceneNumber(current.opacity, sceneDefaultMaterialOpacity(materialKind)));
    const maskCutoff = sceneNormalizeMaterialAlphaCutoff(
      sceneObjectMaterialValue(item, "alphaCutoff"), current.alphaCutoff);
    const maskOpaque = sceneMaterialMaskActive(maskCutoff) &&
      !sceneMaterialHasDirectAuthoredShaderValues(
        sceneEffectiveShaderValues(item, current, false));
    const rawBlendMode = sceneObjectMaterialHasValue(item, "blendMode")
      ? sceneObjectMaterialValue(item, "blendMode") : current.blendMode;
    const blendExplicit = sceneMaterialProfileBlendMode(rawBlendMode) !== "" &&
      sceneRoutedBlendExplicit(item, current,
        sceneObjectMaterialHasValue(item, "blendMode"), "material");
    const blendMode = normalizeSceneMaterialBlendMode(
      blendExplicit ? rawBlendMode : "",
      materialKind,
      numericOpacity,
      maskOpaque,
    );
    const rawRenderPass = sceneObjectMaterialHasValue(item, "renderPass")
      ? sceneObjectMaterialValue(item, "renderPass") : current.renderPass;
    const passExplicit = sceneMaterialProfileRenderPass(rawRenderPass) !== "" &&
      sceneRoutedPassExplicit(item, current,
        sceneObjectMaterialHasValue(item, "renderPass"));
    const normalized = {
      id: item.id || current.id || ("scene-instanced-" + index),
      count: Math.max(0, Math.floor(sceneNumber(item.count, sceneNumber(current.count, 0)))),
      kind,
      material: Object.prototype.hasOwnProperty.call(item, "material")
        ? sceneCloneData(item.material)
        : (Object.prototype.hasOwnProperty.call(current, "material") ? current.material : undefined),
      size,
      width: Math.max(0.0001, sceneNumber(item.width, sceneNumber(current.width, size))),
      height: Math.max(0.0001, sceneNumber(item.height, sceneNumber(current.height, size))),
      depth: Math.max(0.0001, sceneNumber(item.depth, sceneNumber(current.depth, size))),
      radius,
      radiusTop: Math.max(0, sceneNumber(item.radiusTop, sceneNumber(current.radiusTop, radius))),
      radiusBottom: Math.max(0, sceneNumber(item.radiusBottom, sceneNumber(current.radiusBottom, radius))),
      tube: Math.max(0.0001, sceneNumber(item.tube, sceneNumber(current.tube, 0.3))),
      segments: scenePrimitiveSegmentResolution(segmentSource, 32, 3, 256),
      radialSegments: scenePrimitiveSegmentResolution(radialSegmentSource, 32, 3, 256),
      tubularSegments: scenePrimitiveSegmentResolution(tubularSegmentSource, 16, 3, 128),
      materialKind,
      color: typeof sceneObjectMaterialValue(item, "color") === "string" && sceneObjectMaterialValue(item, "color") ? sceneObjectMaterialValue(item, "color") : (typeof current.color === "string" ? current.color : "#8de1ff"),
      texture: typeof sceneObjectMaterialValue(item, "texture") === "string" ? sceneObjectMaterialValue(item, "texture").trim() : (typeof current.texture === "string" ? current.texture : ""),
      opacity,
      unlit,
      emissive: sceneClampNumberOrCSSVar(sceneObjectMaterialValue(item, "emissive"), sceneNumber(current.emissive, sceneDefaultMaterialEmissive(materialKind)), 0, 1),
      roughness: sceneNumberOrCSSVar(sceneObjectMaterialValue(item, "roughness"), sceneNumber(current.roughness, 0.5)),
      metalness: sceneNumberOrCSSVar(sceneObjectMaterialValue(item, "metalness"), sceneNumber(current.metalness, 0)),
      ior: sceneNormalizeMaterialIor(sceneObjectMaterialValue(item, "ior"), current.ior),
      specularIntensity: sceneNormalizeMaterialSpecularIntensity(sceneObjectMaterialValue(item, "specularIntensity"), current.specularIntensity),
      specularColor: sceneNormalizeMaterialSpecularColor(sceneObjectMaterialValue(item, "specularColor"), current.specularColor),
      clearcoat: sceneClampNumberOrCSSVar(sceneObjectMaterialValue(item, "clearcoat"), sceneNumber(current.clearcoat, 0), 0, 1),
      sheen: sceneClampNumberOrCSSVar(sceneObjectMaterialValue(item, "sheen"), sceneNumber(current.sheen, 0), 0, 1),
      transmission: sceneClampNumberOrCSSVar(sceneObjectMaterialValue(item, "transmission"), sceneNumber(current.transmission, 0), 0, 1),
      iridescence: sceneClampNumberOrCSSVar(sceneObjectMaterialValue(item, "iridescence"), sceneNumber(current.iridescence, 0), 0, 1),
      anisotropy: sceneClampNumberOrCSSVar(sceneObjectMaterialValue(item, "anisotropy"), sceneNumber(current.anisotropy, 0), -1, 1),
      alphaCutoff: sceneNormalizeMaterialAlphaCutoff(sceneObjectMaterialValue(item, "alphaCutoff"), current.alphaCutoff),
      normalMap: typeof sceneObjectMaterialValue(item, "normalMap") === "string" ? sceneObjectMaterialValue(item, "normalMap").trim() : (typeof current.normalMap === "string" ? current.normalMap : ""),
      roughnessMap: typeof sceneObjectMaterialValue(item, "roughnessMap") === "string" ? sceneObjectMaterialValue(item, "roughnessMap").trim() : (typeof current.roughnessMap === "string" ? current.roughnessMap : ""),
      metalnessMap: typeof sceneObjectMaterialValue(item, "metalnessMap") === "string" ? sceneObjectMaterialValue(item, "metalnessMap").trim() : (typeof current.metalnessMap === "string" ? current.metalnessMap : ""),
      occlusionMap: typeof sceneObjectMaterialValue(item, "occlusionMap") === "string" ? sceneObjectMaterialValue(item, "occlusionMap").trim() : (typeof current.occlusionMap === "string" ? current.occlusionMap : ""),
      emissiveMap: typeof sceneObjectMaterialValue(item, "emissiveMap") === "string" ? sceneObjectMaterialValue(item, "emissiveMap").trim() : (typeof current.emissiveMap === "string" ? current.emissiveMap : ""),
      textureDescriptors: normalizeSceneMaterialTextureDescriptors(
        sceneObjectMaterialValue(item, "textureDescriptors"),
        current.textureDescriptors,
      ),
      blendMode,
      _blendModeDerived: !blendExplicit,
      _renderPassDerived: !passExplicit,
      renderPass: normalizeSceneMaterialRenderPass(
        passExplicit ? rawRenderPass : "",
        blendMode,
        numericOpacity,
        materialKind,
        maskOpaque,
      ),
      wireframe: sceneBool(sceneObjectMaterialHasValue(item, "wireframe") ? sceneObjectMaterialValue(item, "wireframe") : current.wireframe, false),
      depthWrite: sceneObjectMaterialHasValue(item, "depthWrite") ? sceneBool(sceneObjectMaterialValue(item, "depthWrite"), true) : current.depthWrite,
      lineDash: sceneBool(sceneObjectMaterialHasValue(item, "lineDash") ? sceneObjectMaterialValue(item, "lineDash") : current.lineDash, false),
      dashSize: sceneNumber(sceneObjectMaterialValue(item, "dashSize"), sceneNumber(current.dashSize, 0)),
      gapSize: sceneNumber(sceneObjectMaterialValue(item, "gapSize"), sceneNumber(current.gapSize, 0)),
      customVertex: typeof sceneObjectMaterialValue(item, "customVertex") === "string" ? sceneObjectMaterialValue(item, "customVertex") : (typeof current.customVertex === "string" ? current.customVertex : ""),
      customFragment: typeof sceneObjectMaterialValue(item, "customFragment") === "string" ? sceneObjectMaterialValue(item, "customFragment") : (typeof current.customFragment === "string" ? current.customFragment : ""),
      customVertexWGSL: typeof sceneObjectMaterialValue(item, "customVertexWGSL") === "string" ? sceneObjectMaterialValue(item, "customVertexWGSL") : (typeof current.customVertexWGSL === "string" ? current.customVertexWGSL : ""),
      customFragmentWGSL: typeof sceneObjectMaterialValue(item, "customFragmentWGSL") === "string" ? sceneObjectMaterialValue(item, "customFragmentWGSL") : (typeof current.customFragmentWGSL === "string" ? current.customFragmentWGSL : ""),
      customUniforms: sceneIsPlainObject(sceneObjectMaterialValue(item, "customUniforms")) ? Object.assign({}, sceneObjectMaterialValue(item, "customUniforms")) : (sceneIsPlainObject(current.customUniforms) ? Object.assign({}, current.customUniforms) : null),
      shaderBackend: typeof sceneObjectMaterialValue(item, "shaderBackend") === "string" ? sceneObjectMaterialValue(item, "shaderBackend").trim().toLowerCase() : (typeof current.shaderBackend === "string" ? current.shaderBackend : ""),
      shaderLayout: sceneIsPlainObject(sceneObjectMaterialValue(item, "shaderLayout")) ? sceneCloneData(sceneObjectMaterialValue(item, "shaderLayout")) : (sceneIsPlainObject(current.shaderLayout) ? sceneCloneData(current.shaderLayout) : null),
      shaderSource: typeof sceneObjectMaterialValue(item, "shaderSource") === "string" ? sceneObjectMaterialValue(item, "shaderSource").trim() : (typeof current.shaderSource === "string" ? current.shaderSource : ""),
      shaderSourceFiles: sceneIsPlainObject(sceneObjectMaterialValue(item, "shaderSourceFiles")) ? sceneCloneData(sceneObjectMaterialValue(item, "shaderSourceFiles")) : (sceneIsPlainObject(current.shaderSourceFiles) ? sceneCloneData(current.shaderSourceFiles) : null),
      // Elio GPU cull kernel (WebGPU compute cull pass). Carried end-to-end
      // from scene/scene_ir.go InstancedMeshIR.Cull* fields through the
      // manifest; without these, updateInstancedCullSystems in
      // 16a-scene-webgpu.js never sees cullKernelWGSL and no GPU cull system
      // is created (cullSurvivors telemetry stays empty).
      cullKernelWGSL: typeof sceneObjectMaterialValue(item, "cullKernelWGSL") === "string" ? sceneObjectMaterialValue(item, "cullKernelWGSL").trim() : (typeof current.cullKernelWGSL === "string" ? current.cullKernelWGSL : ""),
      cullKernelEntry: typeof sceneObjectMaterialValue(item, "cullKernelEntry") === "string" ? sceneObjectMaterialValue(item, "cullKernelEntry").trim() : (typeof current.cullKernelEntry === "string" ? current.cullKernelEntry : ""),
      cullRadius: sceneNumber(sceneObjectMaterialValue(item, "cullRadius"), sceneNumber(current.cullRadius, 0)),
      cullBackend: typeof sceneObjectMaterialValue(item, "cullBackend") === "string" ? sceneObjectMaterialValue(item, "cullBackend").trim().toLowerCase() : (typeof current.cullBackend === "string" ? current.cullBackend : ""),
      transforms,
      colors,
      attributes,
      pickable: Object.prototype.hasOwnProperty.call(item, "pickable")
        ? sceneBool(item.pickable, true)
        : (Object.prototype.hasOwnProperty.call(current, "pickable") ? sceneBool(current.pickable, true) : undefined),
      castShadow: sceneBool(Object.prototype.hasOwnProperty.call(item, "castShadow") ? item.castShadow : current.castShadow, false),
      receiveShadow: sceneBool(Object.prototype.hasOwnProperty.call(item, "receiveShadow") ? item.receiveShadow : current.receiveShadow, false),
      _transition: lifecycle.transition,
      _inState: lifecycle.inState,
      _outState: lifecycle.outState,
      _live: lifecycle.live,
    };
    // Carry the cached Float32Array view of transforms forward when the
    // underlying array reference is unchanged (i.e. item.transforms was
    // not re-supplied this tick, so we fell through to current.transforms).
    // Mirrors the same pattern used for scene points at line 1469. Without
    // this, every tick would allocate a fresh Float32Array and VBO for
    // instanced meshes — defeating the static VBO cache in 16-scene-webgl.
    if (transforms === current.transforms && current._cachedTransforms) {
      normalized._cachedTransforms = current._cachedTransforms;
    }
    if (colors === current.colors && current._cachedInstanceColors) {
      normalized._cachedInstanceColors = current._cachedInstanceColors;
    }
    return normalized;
  }

  function sceneInstancedMeshes(props) {
    return rawSceneInstancedMeshes(props).map(function(entry, index) {
      return normalizeSceneInstancedMeshEntry(entry, index, null);
    });
  }

  function normalizeSceneComputeEmitter(raw, fallback) {
    const current = sceneIsPlainObject(fallback) ? fallback : {};
    const item = sceneIsPlainObject(raw) ? raw : {};
    return {
      kind: typeof item.kind === "string" && item.kind ? item.kind : (typeof current.kind === "string" ? current.kind : "point"),
      x: sceneNumber(item.x, sceneNumber(current.x, 0)),
      y: sceneNumber(item.y, sceneNumber(current.y, 0)),
      z: sceneNumber(item.z, sceneNumber(current.z, 0)),
      rotationX: sceneNumber(item.rotationX, sceneNumber(current.rotationX, 0)),
      rotationY: sceneNumber(item.rotationY, sceneNumber(current.rotationY, 0)),
      rotationZ: sceneNumber(item.rotationZ, sceneNumber(current.rotationZ, 0)),
      spinX: sceneNumber(item.spinX, sceneNumber(current.spinX, 0)),
      spinY: sceneNumber(item.spinY, sceneNumber(current.spinY, 0)),
      spinZ: sceneNumber(item.spinZ, sceneNumber(current.spinZ, 0)),
      radius: Math.max(0, sceneNumber(item.radius, sceneNumber(current.radius, 0))),
      rate: Math.max(0, sceneNumber(item.rate, sceneNumber(current.rate, 0))),
      lifetime: Math.max(0.01, sceneNumber(item.lifetime, sceneNumber(current.lifetime, 1))),
      arms: Math.max(0, Math.floor(sceneNumber(item.arms, sceneNumber(current.arms, 0)))),
      wind: sceneNumberOrCSSVar(item.wind, sceneNumber(current.wind, 0)),
      scatter: Math.max(0, sceneNumber(item.scatter, sceneNumber(current.scatter, 0))),
    };
  }

  function normalizeSceneComputeForce(raw, index, fallback) {
    const current = sceneIsPlainObject(fallback) ? fallback : {};
    const item = sceneIsPlainObject(raw) ? raw : {};
    return {
      kind: typeof item.kind === "string" && item.kind ? item.kind : (typeof current.kind === "string" ? current.kind : ""),
      strength: sceneNumber(item.strength, sceneNumber(current.strength, 0)),
      x: sceneNumber(item.x, sceneNumber(current.x, 0)),
      y: sceneNumber(item.y, sceneNumber(current.y, 0)),
      z: sceneNumber(item.z, sceneNumber(current.z, 0)),
      frequency: sceneNumber(item.frequency, sceneNumber(current.frequency, 0)),
      id: current.id || ("scene-force-" + index),
    };
  }

  function normalizeSceneComputeMaterial(raw, fallback) {
    const current = sceneIsPlainObject(fallback) ? fallback : {};
    const item = sceneIsPlainObject(raw) ? raw : {};
    return {
      color: typeof item.color === "string" && item.color ? item.color : (typeof current.color === "string" ? current.color : "#ffffff"),
      colorEnd: typeof item.colorEnd === "string" && item.colorEnd ? item.colorEnd : (typeof current.colorEnd === "string" ? current.colorEnd : ""),
      style: normalizeScenePointStyle(item.style, current.style),
      size: sceneClampNumberOrCSSVar(item.size, sceneNumber(current.size, 1), 0, Number.POSITIVE_INFINITY),
      sizeEnd: sceneClampNumberOrCSSVar(item.sizeEnd, sceneNumber(current.sizeEnd, sceneNumber(current.size, 1)), 0, Number.POSITIVE_INFINITY),
      opacity: sceneClampNumberOrCSSVar(item.opacity, sceneNumber(current.opacity, 1), 0, 1),
      opacityEnd: sceneClampNumberOrCSSVar(item.opacityEnd, sceneNumber(current.opacityEnd, sceneNumber(current.opacity, 1)), 0, 1),
      blendMode: normalizeSceneMaterialBlendMode(item.blendMode || current.blendMode, "flat", sceneNumber(item.opacity, sceneNumber(current.opacity, 1))),
      attenuation: sceneBool(Object.prototype.hasOwnProperty.call(item, "attenuation") ? item.attenuation : current.attenuation, false),
      minPixelSize: sceneClampNumberOrCSSVar(item.minPixelSize, sceneNumber(current.minPixelSize, 0), 0, Number.POSITIVE_INFINITY),
      maxPixelSize: sceneClampNumberOrCSSVar(item.maxPixelSize, sceneNumber(current.maxPixelSize, 0), 0, Number.POSITIVE_INFINITY),
    };
  }

  function normalizeSceneComputeParticlesEntry(entry, index, fallback) {
    const current = sceneIsPlainObject(fallback) ? fallback : {};
    const item = sceneIsPlainObject(entry) ? entry : {};
    const lifecycle = sceneNormalizeLifecycle(item, current);
    const emitterSource = Object.prototype.hasOwnProperty.call(item, "emitter") ? item.emitter : current.emitter;
    const materialSource = Object.prototype.hasOwnProperty.call(item, "material") ? item.material : current.material;
    const forcesSource = Array.isArray(item.forces) ? item.forces : (Array.isArray(current.forces) ? current.forces : []);
    const normalized = {
      id: item.id || current.id || ("scene-particles-" + index),
      count: Math.max(0, Math.floor(sceneNumber(item.count, sceneNumber(current.count, 0)))),
      emitter: normalizeSceneComputeEmitter(emitterSource, current.emitter),
      forces: forcesSource.map(function(force, forceIndex) {
        return normalizeSceneComputeForce(force, forceIndex, Array.isArray(current.forces) ? current.forces[forceIndex] : null);
      }),
      material: normalizeSceneComputeMaterial(materialSource, current.material),
      bounds: Math.max(0, sceneNumber(item.bounds, sceneNumber(current.bounds, 0))),
      _transition: lifecycle.transition,
      _inState: lifecycle.inState,
      _outState: lifecycle.outState,
      _live: lifecycle.live,
    };
    [
      "computeWGSL", "computeEntry", "computeBackend", "computeWGSLRef",
      "renderVertex", "renderFragment", "renderVertexWGSL", "renderFragmentWGSL",
      "renderVertexRef", "renderFragmentRef", "renderVertexWGSLRef", "renderFragmentWGSLRef",
      "renderShaderBackend",
    ].forEach(function(field) {
      normalized[field] = typeof item[field] === "string" ? item[field] : (typeof current[field] === "string" ? current[field] : "");
    });
    normalized.renderUniforms = sceneIsPlainObject(item.renderUniforms)
      ? sceneCloneData(item.renderUniforms)
      : (sceneIsPlainObject(current.renderUniforms) ? sceneCloneData(current.renderUniforms) : null);
    normalized.renderShaderLayout = sceneIsPlainObject(item.renderShaderLayout)
      ? sceneCloneData(item.renderShaderLayout)
      : (sceneIsPlainObject(current.renderShaderLayout) ? sceneCloneData(current.renderShaderLayout) : null);
    return normalized;
  }

  function sceneComputeParticles(props) {
    return rawSceneComputeParticles(props).map(function(entry, index) {
      return normalizeSceneComputeParticlesEntry(entry, index, null);
    });
  }

  const SCENE_WATER_SOURCE_ID_FIELDS = ["computeSource", "materialSource"];
  const SCENE_WATER_SOURCE_FILE_MAP_FIELDS = ["computeSourceFiles", "materialSourceFiles"];

  const SCENE_WATER_SHADER_STRING_FIELDS = [
    "seedWGSL", "dropWGSL", "displacementWGSL", "simulationWGSL", "normalWGSL", "causticsWGSL",
    "poolVertexWGSL", "poolFragmentWGSL", "surfaceVertexWGSL", "surfaceFragmentWGSL", "surfaceBelowFragmentWGSL",
    "objectShadowWGSL", "objectMeshShadowVertexWGSL", "objectMeshShadowFragmentWGSL",
    "seedWGSLRef", "dropWGSLRef", "displacementWGSLRef", "simulationWGSLRef", "normalWGSLRef", "causticsWGSLRef",
    "poolVertexWGSLRef", "poolFragmentWGSLRef", "surfaceVertexWGSLRef", "surfaceFragmentWGSLRef", "surfaceBelowFragmentWGSLRef",
    "objectShadowWGSLRef", "objectMeshShadowVertexWGSLRef", "objectMeshShadowFragmentWGSLRef",
  ];

  function sceneWaterSystemID(entry, index) {
    return entry && typeof entry.id === "string" && entry.id ? entry.id : ("scene-water-" + index);
  }

  function sceneMergeWaterShaderSources(sourceMap, entry, index) {
    if (!sourceMap || !entry || typeof entry !== "object") return;
    const id = sceneWaterSystemID(entry, index);
    const current = sourceMap.get(id) || { id: id };
    let changed = false;
    for (let i = 0; i < SCENE_WATER_SOURCE_ID_FIELDS.length; i += 1) {
      const name = SCENE_WATER_SOURCE_ID_FIELDS[i];
      if (typeof entry[name] === "string" && entry[name].trim()) {
        current[name] = entry[name];
        changed = true;
      }
    }
    for (let i = 0; i < SCENE_WATER_SOURCE_FILE_MAP_FIELDS.length; i += 1) {
      const name = SCENE_WATER_SOURCE_FILE_MAP_FIELDS[i];
      if (sceneIsPlainObject(entry[name])) {
        current[name] = sceneCloneData(entry[name]);
        changed = true;
      }
    }
    for (let i = 0; i < SCENE_WATER_SHADER_STRING_FIELDS.length; i += 1) {
      const name = SCENE_WATER_SHADER_STRING_FIELDS[i];
      if (typeof entry[name] === "string" && entry[name].trim()) {
        current[name] = entry[name];
        changed = true;
      }
    }
    if (changed) sourceMap.set(id, current);
  }

  function sceneWaterShaderSourceMap(entries) {
    const sourceMap = new Map();
    const source = Array.isArray(entries) ? entries : [];
    for (let i = 0; i < source.length; i += 1) {
      sceneMergeWaterShaderSources(sourceMap, source[i], i);
    }
    return sourceMap;
  }

  function scenePublishWaterShaderSourceMap(sourceMap) {
    if (typeof window === "undefined" || !sourceMap || typeof sourceMap.forEach !== "function") return;
    const published = window.__gosx_scene3d_water_shader_sources_by_id || {};
    sourceMap.forEach(function(record, id) {
      if (!record || typeof record !== "object") return;
      published[id] = Object.assign({}, published[id] || {}, record);
    });
    window.__gosx_scene3d_water_shader_sources_by_id = published;
  }

  function normalizeSceneWaterSystemEntry(entry, index, fallback) {
    const current = sceneIsPlainObject(fallback) ? fallback : {};
    const item = sceneIsPlainObject(entry) ? entry : {};
    const waterShaderString = function(name) {
      return typeof item[name] === "string" && item[name].trim()
        ? item[name]
        : (typeof current[name] === "string" ? current[name] : "");
    };
    return {
      id: item.id || current.id || ("scene-water-" + index),
      interactionProfile: typeof item.interactionProfile === "string" ? item.interactionProfile : (typeof current.interactionProfile === "string" ? current.interactionProfile : ""),
      interactionTarget: typeof item.interactionTarget === "string" ? item.interactionTarget : (typeof current.interactionTarget === "string" ? current.interactionTarget : ""),
      interactionObject: typeof item.interactionObject === "string" ? item.interactionObject : (typeof current.interactionObject === "string" ? current.interactionObject : ""),
      resolution: Math.max(1, Math.floor(sceneNumber(item.resolution, sceneNumber(current.resolution, 256)))),
      surfaceResolution: Math.max(2, Math.floor(sceneNumber(item.surfaceResolution, sceneNumber(current.surfaceResolution, sceneNumber(item.resolution, sceneNumber(current.resolution, 256)))))),
      poolShape: typeof item.poolShape === "string" && item.poolShape ? item.poolShape : (typeof current.poolShape === "string" ? current.poolShape : "Box"),
      poolWidth: Math.max(0.001, sceneNumber(item.poolWidth, sceneNumber(current.poolWidth, 1))),
      poolHeight: Math.max(0.001, sceneNumber(item.poolHeight, sceneNumber(current.poolHeight, 1))),
      poolLength: Math.max(0.001, sceneNumber(item.poolLength, sceneNumber(current.poolLength, 1))),
      cornerRadius: Math.max(0, sceneNumber(item.cornerRadius, sceneNumber(current.cornerRadius, 0))),
      waveSpeed: sceneNumber(item.waveSpeed, sceneNumber(current.waveSpeed, 1)),
      damping: sceneNumber(item.damping, sceneNumber(current.damping, 0.995)),
      normalScale: sceneNumber(item.normalScale, sceneNumber(current.normalScale, 1)),
      seedDrops: Math.max(0, Math.floor(sceneNumber(item.seedDrops, sceneNumber(current.seedDrops, 0)))),
      dropRadius: Math.max(0, sceneNumber(item.dropRadius, sceneNumber(current.dropRadius, 0.03))),
      dropStrength: sceneNumber(item.dropStrength, sceneNumber(current.dropStrength, 0.01)),
      dropEventID: Math.max(0, Math.floor(sceneNumber(item.dropEventID, sceneNumber(current.dropEventID, 0)))),
      dropX: Math.max(-1, Math.min(1, sceneNumber(item.dropX, sceneNumber(current.dropX, 0)))),
      dropZ: Math.max(-1, Math.min(1, sceneNumber(item.dropZ, sceneNumber(current.dropZ, 0)))),
      dropEventRadius: Math.max(0, sceneNumber(item.dropEventRadius, sceneNumber(current.dropEventRadius, sceneNumber(item.dropRadius, sceneNumber(current.dropRadius, 0.03))))),
      dropEventStrength: sceneNumber(item.dropEventStrength, sceneNumber(current.dropEventStrength, sceneNumber(item.dropStrength, sceneNumber(current.dropStrength, 0.01)))),
      tileTexture: typeof item.tileTexture === "string" ? item.tileTexture : (typeof current.tileTexture === "string" ? current.tileTexture : ""),
      cubeMap: typeof item.cubeMap === "string" ? item.cubeMap : (typeof current.cubeMap === "string" ? current.cubeMap : ""),
      shallowColor: typeof item.shallowColor === "string" ? item.shallowColor : (typeof current.shallowColor === "string" ? current.shallowColor : ""),
      deepColor: typeof item.deepColor === "string" ? item.deepColor : (typeof current.deepColor === "string" ? current.deepColor : ""),
      aboveWaterColorR: sceneNumber(item.aboveWaterColorR, sceneNumber(current.aboveWaterColorR, 0)),
      aboveWaterColorG: sceneNumber(item.aboveWaterColorG, sceneNumber(current.aboveWaterColorG, 0)),
      aboveWaterColorB: sceneNumber(item.aboveWaterColorB, sceneNumber(current.aboveWaterColorB, 0)),
      causticsResolution: Math.max(0, Math.floor(sceneNumber(item.causticsResolution, sceneNumber(current.causticsResolution, 0)))),
      objectTextureResolution: Math.max(0, Math.floor(sceneNumber(item.objectTextureResolution, sceneNumber(current.objectTextureResolution, 0)))),
      objectTextureResolutionMode: typeof item.objectTextureResolutionMode === "string" ? item.objectTextureResolutionMode : (typeof current.objectTextureResolutionMode === "string" ? current.objectTextureResolutionMode : ""),
      objectTexturePixelBudget: Math.max(0, Math.floor(sceneNumber(item.objectTexturePixelBudget, sceneNumber(current.objectTexturePixelBudget, 0)))),
      objectShadowResolution: Math.max(0, Math.floor(sceneNumber(item.objectShadowResolution, sceneNumber(current.objectShadowResolution, 0)))),
      caustics: sceneBool(Object.prototype.hasOwnProperty.call(item, "caustics") ? item.caustics : current.caustics, true),
      reflection: sceneBool(Object.prototype.hasOwnProperty.call(item, "reflection") ? item.reflection : current.reflection, true),
      refraction: sceneBool(Object.prototype.hasOwnProperty.call(item, "refraction") ? item.refraction : current.refraction, true),
      paused: sceneBool(Object.prototype.hasOwnProperty.call(item, "paused") ? item.paused : current.paused, false),
      followCamera: sceneBool(Object.prototype.hasOwnProperty.call(item, "followCamera") ? item.followCamera : current.followCamera, false),
      lightDirectionX: sceneNumber(item.lightDirectionX, sceneNumber(current.lightDirectionX, 2)),
      lightDirectionY: sceneNumber(item.lightDirectionY, sceneNumber(current.lightDirectionY, 3)),
      lightDirectionZ: sceneNumber(item.lightDirectionZ, sceneNumber(current.lightDirectionZ, -1)),
      activeObject: typeof item.activeObject === "string" ? item.activeObject : (typeof current.activeObject === "string" ? current.activeObject : ""),
      objectKind: typeof item.objectKind === "string" ? item.objectKind : (typeof current.objectKind === "string" ? current.objectKind : ""),
      objectX: sceneNumber(item.objectX, sceneNumber(current.objectX, 0)),
      objectY: sceneNumber(item.objectY, sceneNumber(current.objectY, 0)),
      objectZ: sceneNumber(item.objectZ, sceneNumber(current.objectZ, 0)),
      objectPreviousSet: sceneBool(Object.prototype.hasOwnProperty.call(item, "objectPreviousSet") ? item.objectPreviousSet : current.objectPreviousSet, false),
      objectPreviousX: sceneNumber(item.objectPreviousX, sceneNumber(current.objectPreviousX, 0)),
      objectPreviousY: sceneNumber(item.objectPreviousY, sceneNumber(current.objectPreviousY, 0)),
      objectPreviousZ: sceneNumber(item.objectPreviousZ, sceneNumber(current.objectPreviousZ, 0)),
      objectRadius: Math.max(0, sceneNumber(item.objectRadius, sceneNumber(current.objectRadius, 0))),
      objectHalfSizeX: Math.max(0, sceneNumber(item.objectHalfSizeX, sceneNumber(current.objectHalfSizeX, 0))),
      objectHalfSizeY: Math.max(0, sceneNumber(item.objectHalfSizeY, sceneNumber(current.objectHalfSizeY, 0))),
      objectHalfSizeZ: Math.max(0, sceneNumber(item.objectHalfSizeZ, sceneNumber(current.objectHalfSizeZ, 0))),
      objectDriftX: sceneNumber(item.objectDriftX, sceneNumber(current.objectDriftX, 0)),
      objectDriftY: sceneNumber(item.objectDriftY, sceneNumber(current.objectDriftY, 0)),
      objectDriftZ: sceneNumber(item.objectDriftZ, sceneNumber(current.objectDriftZ, 0)),
      objectBobAmplitude: Math.max(0, sceneNumber(item.objectBobAmplitude, sceneNumber(current.objectBobAmplitude, 0))),
      objectBobSpeed: Math.max(0, sceneNumber(item.objectBobSpeed, sceneNumber(current.objectBobSpeed, 0))),
      objectDisplacementScale: Math.max(0, sceneNumber(item.objectDisplacementScale, sceneNumber(current.objectDisplacementScale, 1))),
      objectDisplacementSpheres: normalizeSceneWaterDisplacementSpheres(item.objectDisplacementSpheres, current.objectDisplacementSpheres),
      // objectDisplacementEvents/dropEvents: bounded one-shot event queues
      // (id-tagged) consumed by the WebGL/WebGPU water renderers to replay
      // every queued splash/swept-volume-wake this frame instead of only the
      // latest scalar. Passed through here (not just carried on the raw
      // SET_PARTICLES command payload) so they survive
      // applySceneParticlesCommand -> normalizeSceneWaterSystemEntry, the
      // path every managed-control-forms drop/object-switch command
      // actually takes at runtime.
      objectDisplacementEvents: normalizeSceneWaterOneShotEvents(item.objectDisplacementEvents, current.objectDisplacementEvents),
      dropEvents: normalizeSceneWaterOneShotEvents(item.dropEvents, current.dropEvents),
      computeBackend: typeof item.computeBackend === "string" && item.computeBackend ? item.computeBackend : (typeof current.computeBackend === "string" ? current.computeBackend : "elio"),
      materialBackend: typeof item.materialBackend === "string" && item.materialBackend ? item.materialBackend : (typeof current.materialBackend === "string" ? current.materialBackend : "selena"),
      computeSource: typeof item.computeSource === "string" ? item.computeSource : (typeof current.computeSource === "string" ? current.computeSource : ""),
      materialSource: typeof item.materialSource === "string" ? item.materialSource : (typeof current.materialSource === "string" ? current.materialSource : ""),
      computeSourceFiles: sceneIsPlainObject(item.computeSourceFiles) ? sceneCloneData(item.computeSourceFiles) : (sceneIsPlainObject(current.computeSourceFiles) ? sceneCloneData(current.computeSourceFiles) : null),
      materialSourceFiles: sceneIsPlainObject(item.materialSourceFiles) ? sceneCloneData(item.materialSourceFiles) : (sceneIsPlainObject(current.materialSourceFiles) ? sceneCloneData(current.materialSourceFiles) : null),
      // shaderDescriptors carries the per-shader Selena host binding descriptor
      // (bindings.Layout JSON), keyed by logical shader name (e.g. "pool").
      // Passed through unfiltered like *SourceFiles above so the generic
      // descriptor-driven Selena render path (16a-scene-webgpu.js) can read
      // shaderDescriptors.pool for the pool pass.
      shaderDescriptors: sceneIsPlainObject(item.shaderDescriptors) ? sceneCloneData(item.shaderDescriptors) : (sceneIsPlainObject(current.shaderDescriptors) ? sceneCloneData(current.shaderDescriptors) : null),
      // seedSelenaWGSL..normalSelenaWGSL are the Selena-emitted single
      // @compute WGSL modules for the five feedback simulation kernels,
      // consumed by the generic descriptor-driven Selena feedback-compute
      // path in 16a-scene-webgpu.js. The hand-written seedWGSL/dropWGSL/
      // displacementWGSL/simulationWGSL/normalWGSL (and every other
      // hand-written *WGSL/*WGSLRef water slot) have been retired now that
      // Selena is the sole primary WGSL source ahead of the builtin
      // SCENE_WATER_*_SOURCE runtime fallback (see 16a-scene-webgpu.js).
      seedSelenaWGSL: waterShaderString("seedSelenaWGSL"),
      dropSelenaWGSL: waterShaderString("dropSelenaWGSL"),
      displacementSelenaWGSL: waterShaderString("displacementSelenaWGSL"),
      simulationSelenaWGSL: waterShaderString("simulationSelenaWGSL"),
      normalSelenaWGSL: waterShaderString("normalSelenaWGSL"),
      // poolSelenaWGSL is the Selena-emitted combined vertex+fragment WGSL
      // module for the pool pass.
      poolSelenaWGSL: waterShaderString("poolSelenaWGSL"),
      // surfaceSelenaWGSL..objectMeshShadowSelenaWGSL generalize
      // poolSelenaWGSL above to the remaining render passes, consumed by the
      // generic descriptor-driven Selena WebGPU render path in
      // 16a-scene-webgpu.js.
      surfaceSelenaWGSL: waterShaderString("surfaceSelenaWGSL"),
      surfaceBelowSelenaWGSL: waterShaderString("surfaceBelowSelenaWGSL"),
      causticsSelenaWGSL: waterShaderString("causticsSelenaWGSL"),
      objectShadowSelenaWGSL: waterShaderString("objectShadowSelenaWGSL"),
      compoundShadowSelenaWGSL: waterShaderString("compoundShadowSelenaWGSL"),
      objectMeshShadowSelenaWGSL: waterShaderString("objectMeshShadowSelenaWGSL"),
    };
  }

  function normalizeSceneWaterDisplacementSpheres(value, fallback) {
    const source = Array.isArray(value) ? value : (Array.isArray(fallback) ? fallback : []);
    const out = [];
    for (let i = 0; i < source.length && out.length < 64; i++) {
      const raw = source[i];
      let offsetX = 0;
      let offsetY = 0;
      let offsetZ = 0;
      let radius = 0;
      if (Array.isArray(raw)) {
        offsetX = sceneNumber(raw[0], 0);
        offsetY = sceneNumber(raw[1], 0);
        offsetZ = sceneNumber(raw[2], 0);
        radius = sceneNumber(raw[3], 0);
      } else if (sceneIsPlainObject(raw)) {
        const offset = sceneIsPlainObject(raw.offset) ? raw.offset : {};
        offsetX = sceneNumber(Object.prototype.hasOwnProperty.call(raw, "offsetX") ? raw.offsetX : offset.x, 0);
        offsetY = sceneNumber(Object.prototype.hasOwnProperty.call(raw, "offsetY") ? raw.offsetY : offset.y, 0);
        offsetZ = sceneNumber(Object.prototype.hasOwnProperty.call(raw, "offsetZ") ? raw.offsetZ : offset.z, 0);
        radius = sceneNumber(raw.radius, 0);
      }
      if (radius <= 0) continue;
      out.push({
        offsetX: offsetX,
        offsetY: offsetY,
        offsetZ: offsetZ,
        radius: Math.max(0, radius),
      });
    }
    return out;
  }

  // normalizeSceneWaterOneShotEvents: clones a bounded id-tagged one-shot
  // event queue array (objectDisplacementEvents / dropEvents) through the
  // command pipeline. Prefers the incoming command's array when present
  // (even if empty -- an explicit [] means "nothing new queued this frame");
  // only falls back to the prior normalized entry's array when the field is
  // entirely absent from the command payload, matching how every other
  // optional water field here treats item vs. current. Consumers dedupe by
  // monotonic id (see dispatchWaterObjectDisplacementEvents/
  // dispatchWaterDropEvents and their WebGL queueWaterEvents counterpart),
  // so replaying already-consumed ids from the fallback is harmless.
  function normalizeSceneWaterOneShotEvents(value, fallback) {
    const source = Array.isArray(value) ? value : (Array.isArray(fallback) ? fallback : []);
    const out = [];
    for (let i = 0; i < source.length && out.length < 32; i++) {
      const raw = source[i];
      if (!sceneIsPlainObject(raw)) continue;
      out.push(sceneCloneData(raw));
    }
    return out;
  }

  function sceneWaterSystems(props) {
    return rawSceneWaterSystems(props).map(function(entry, index) {
      return normalizeSceneWaterSystemEntry(entry, index, null);
    });
  }

  function normalizeSceneMaterialRecord(raw, index, fallback) {
    const current = sceneIsPlainObject(fallback) ? fallback : {};
    const item = sceneIsPlainObject(raw) ? raw : {};
    const kind = normalizeSceneMaterialKind(item.kind || item.materialKind || current.kind);
    const colorSpecified = typeof item.color === "string" && item.color !== "";
    const opacitySpecified = Object.prototype.hasOwnProperty.call(item, "opacity");
    const blendModeSpecified = Object.prototype.hasOwnProperty.call(item, "blendMode") && String(item.blendMode || "").trim() !== "";
    const depthWriteSpecified = Object.prototype.hasOwnProperty.call(item, "depthWrite");
    const opacity = sceneClampNumberOrCSSVar(item.opacity, sceneNumber(current.opacity, sceneDefaultMaterialOpacity(kind)), 0, 1);
    const numericOpacity = sceneNumber(opacity, sceneNumber(current.opacity, sceneDefaultMaterialOpacity(kind)));
    const maskCutoff = sceneObjectMaterialValue(item, "alphaCutoff") !== undefined
      ? sceneNormalizeMaterialAlphaCutoff(
        sceneObjectMaterialValue(item, "alphaCutoff"),
        current.alphaCutoff !== undefined
          ? sceneNormalizeMaterialAlphaCutoff(current.alphaCutoff, null)
          : null)
      : (current.alphaCutoff !== undefined
        ? sceneNormalizeMaterialAlphaCutoff(current.alphaCutoff, null)
        : undefined);
    const maskOpaque = sceneMaterialMaskActive(maskCutoff) &&
      !sceneMaterialHasDirectAuthoredShaderValues(
        sceneEffectiveShaderValues(item, current, true));
    const rawBlendMode = item.blendMode || current.blendMode;
    const blendExplicit = sceneMaterialProfileBlendMode(rawBlendMode) !== "" &&
      sceneRoutedBlendExplicit(item, current, blendModeSpecified, "direct");
    const blendMode = normalizeSceneMaterialBlendMode(
      blendExplicit ? rawBlendMode : "",
      kind,
      numericOpacity,
      maskOpaque,
    );
    const rawRenderPass = item.renderPass || current.renderPass;
    const passExplicit = sceneMaterialProfileRenderPass(rawRenderPass) !== "" &&
      sceneRoutedPassExplicit(item, current, !!item.renderPass, "direct");
    const out = {
      id: typeof item.id === "string" && item.id ? item.id : (typeof current.id === "string" ? current.id : ""),
      name: typeof item.name === "string" && item.name ? item.name : (typeof current.name === "string" ? current.name : ("scene-material-" + index)),
      kind,
      color: typeof item.color === "string" && item.color ? item.color : (typeof current.color === "string" ? current.color : "#8de1ff"),
      texture: typeof item.texture === "string" ? item.texture.trim() : (typeof current.texture === "string" ? current.texture : ""),
      opacity,
      emissive: sceneClampNumberOrCSSVar(item.emissive, sceneNumber(current.emissive, sceneDefaultMaterialEmissive(kind)), 0, 1),
      roughness: sceneNumberOrCSSVar(item.roughness, sceneNumber(current.roughness, 0.5)),
      metalness: sceneNumberOrCSSVar(item.metalness, sceneNumber(current.metalness, 0)),
      ior: sceneNormalizeMaterialIor(item.ior, current.ior),
      specularIntensity: sceneNormalizeMaterialSpecularIntensity(item.specularIntensity, current.specularIntensity),
      specularColor: sceneNormalizeMaterialSpecularColor(item.specularColor, current.specularColor),
      clearcoat: sceneClampNumberOrCSSVar(item.clearcoat, sceneNumber(current.clearcoat, 0), 0, 1),
      sheen: sceneClampNumberOrCSSVar(item.sheen, sceneNumber(current.sheen, 0), 0, 1),
      transmission: sceneClampNumberOrCSSVar(item.transmission, sceneNumber(current.transmission, 0), 0, 1),
      iridescence: sceneClampNumberOrCSSVar(item.iridescence, sceneNumber(current.iridescence, 0), 0, 1),
      anisotropy: sceneClampNumberOrCSSVar(item.anisotropy, sceneNumber(current.anisotropy, 0), -1, 1),
      normalMap: typeof item.normalMap === "string" ? item.normalMap.trim() : (typeof current.normalMap === "string" ? current.normalMap : ""),
      roughnessMap: typeof item.roughnessMap === "string" ? item.roughnessMap.trim() : (typeof current.roughnessMap === "string" ? current.roughnessMap : ""),
      metalnessMap: typeof item.metalnessMap === "string" ? item.metalnessMap.trim() : (typeof current.metalnessMap === "string" ? current.metalnessMap : ""),
      occlusionMap: typeof item.occlusionMap === "string" ? item.occlusionMap.trim() : (typeof current.occlusionMap === "string" ? current.occlusionMap : ""),
      emissiveMap: typeof item.emissiveMap === "string" ? item.emissiveMap.trim() : (typeof current.emissiveMap === "string" ? current.emissiveMap : ""),
      textureDescriptors: normalizeSceneMaterialTextureDescriptors(item.textureDescriptors, current.textureDescriptors),
      blendMode,
      _blendModeDerived: !blendExplicit,
      _renderPassDerived: !passExplicit,
      renderPass: normalizeSceneMaterialRenderPass(
        passExplicit ? rawRenderPass : "",
        blendMode,
        numericOpacity,
        kind,
        maskOpaque,
      ),
      wireframe: sceneBool(Object.prototype.hasOwnProperty.call(item, "wireframe") ? item.wireframe : current.wireframe, false),
      lineDash: sceneBool(Object.prototype.hasOwnProperty.call(item, "lineDash") ? item.lineDash : current.lineDash, false),
      dashSize: sceneNumber(item.dashSize, sceneNumber(current.dashSize, 0)),
      gapSize: sceneNumber(item.gapSize, sceneNumber(current.gapSize, 0)),
      customVertex: typeof item.customVertex === "string" ? item.customVertex : (typeof current.customVertex === "string" ? current.customVertex : ""),
      customFragment: typeof item.customFragment === "string" ? item.customFragment : (typeof current.customFragment === "string" ? current.customFragment : ""),
      customVertexWGSL: typeof item.customVertexWGSL === "string" ? item.customVertexWGSL : (typeof current.customVertexWGSL === "string" ? current.customVertexWGSL : ""),
      customFragmentWGSL: typeof item.customFragmentWGSL === "string" ? item.customFragmentWGSL : (typeof current.customFragmentWGSL === "string" ? current.customFragmentWGSL : ""),
      customUniforms: sceneIsPlainObject(item.customUniforms) ? Object.assign({}, item.customUniforms) : (sceneIsPlainObject(current.customUniforms) ? Object.assign({}, current.customUniforms) : null),
      shaderBackend: typeof item.shaderBackend === "string" ? item.shaderBackend.trim().toLowerCase() : (typeof current.shaderBackend === "string" ? current.shaderBackend : ""),
      shaderLayout: sceneIsPlainObject(item.shaderLayout) ? sceneCloneData(item.shaderLayout) : (sceneIsPlainObject(current.shaderLayout) ? sceneCloneData(current.shaderLayout) : null),
      shaderSource: typeof item.shaderSource === "string" ? item.shaderSource.trim() : (typeof current.shaderSource === "string" ? current.shaderSource : ""),
      shaderSourceFiles: sceneIsPlainObject(item.shaderSourceFiles) ? sceneCloneData(item.shaderSourceFiles) : (sceneIsPlainObject(current.shaderSourceFiles) ? sceneCloneData(current.shaderSourceFiles) : null),
      depthWrite: Object.prototype.hasOwnProperty.call(item, "depthWrite") ? sceneBool(item.depthWrite, true) : current.depthWrite,
      // Screen-space floor/cap a named material may declare for the point
      // layers bound to it. A GLB-derived layer carries no Points struct of
      // its own, so the material is its only route to a pixel floor — and
      // without one an attenuated layer scintillates as it moves.
      minPixelSize: sceneNumber(item.minPixelSize, sceneNumber(current.minPixelSize, 0)),
      maxPixelSize: sceneNumber(item.maxPixelSize, sceneNumber(current.maxPixelSize, 0)),
      variantKey: typeof item._variantKey === "string" ? item._variantKey : (typeof current.variantKey === "string" ? current.variantKey : ""),
      _colorSpecified: colorSpecified || current._colorSpecified === true,
      _opacitySpecified: opacitySpecified || current._opacitySpecified === true,
      _blendModeSpecified: blendModeSpecified || current._blendModeSpecified === true,
      _depthWriteSpecified: depthWriteSpecified || current._depthWriteSpecified === true,
    };
    // Normalize unlit from the raw record value: omission and own undefined
    // inherit the current flag; malformed values fall back to it too, so a
    // bad override can neither erase the inherited flag nor define a new one.
    // Only a valid boolean (or inherited valid flag) reaches the output.
    const normalizedUnlit = sceneBool(
      sceneObjectMaterialValue(item, "unlit"),
      sceneBool(current.unlit, undefined),
    );
    if (normalizedUnlit !== undefined) {
      out.unlit = normalizedUnlit;
    }
    const rawAlphaCutoff = sceneObjectMaterialValue(item, "alphaCutoff");
    if (rawAlphaCutoff !== undefined) {
      out.alphaCutoff = sceneNormalizeMaterialAlphaCutoff(rawAlphaCutoff, sceneNormalizeMaterialAlphaCutoff(current.alphaCutoff, null));
    } else if (current.alphaCutoff !== undefined) {
      out.alphaCutoff = sceneNormalizeMaterialAlphaCutoff(current.alphaCutoff, null);
    }
    out.key = sceneMaterialProfileKey(out);
    out.shaderData = sceneMaterialShaderData(out);
    return out;
  }

  function sceneMaterials(props, capability) {
    return sceneNormalizeMaterialList(rawSceneMaterials(props), capability);
  }

  function sceneNormalizeMaterialList(materials, capability) {
    const entries = Array.isArray(materials) ? materials : [];
    return entries.map(function(entry, index) {
      return normalizeSceneMaterialRecord(sceneMaterialRecordForCapability(entry, capability), index, null);
    });
  }

  // SCENE_POST_KIND_CANONICAL maps a case-folded post-effect kind onto the EXACT
  // spelling both render backends dispatch on. The WebGPU (16a-scene-webgpu.js
  // `switch (effect.kind)`) and WebGL (16-scene-webgl.js, same switch) chains
  // compare against the SCENE_POST_* constants with ===, and three of those
  // constants are camelCase: "toneMapping", "colorGrade" and "customPost".
  //
  // This normalizer used to force kind through .toLowerCase(), which silently
  // rewrote those three to "tonemapping"/"colorgrade"/"custompost". They then
  // fell through both switches to `default: break;` — the effect stayed in
  // state.postEffects, still counted toward the post-effects mount attribute,
  // and the post chain still ran its final blit, so every health signal looked
  // green while the pass itself never rendered a single pixel. Only the kinds
  // that happen to be all-lowercase (bloom/ssao/dof/fxaa/vignette) survived.
  //
  // Fold case for MATCHING, then hand back the canonical spelling.
  const SCENE_POST_KIND_CANONICAL = {
    tonemapping: "toneMapping",
    tonemap: "toneMapping",
    bloom: "bloom",
    vignette: "vignette",
    colorgrade: "colorGrade",
    "color-grade": "colorGrade",
    "color-grading": "colorGrade",
    ssao: "ssao",
    dof: "dof",
    fxaa: "fxaa",
    custompost: "customPost",
    "custom-post": "customPost",
  };

  function sceneCanonicalPostEffectKind(raw) {
    const text = typeof raw === "string" ? raw.trim() : "";
    if (!text) {
      return "";
    }
    // An unrecognized kind keeps the author's EXACT spelling. A kind this
    // runtime does not know may still be dispatched by a newer backend or a
    // downstream consumer, and case-folding it would break that pass the same
    // way folding "customPost" broke every Selena post effect.
    return SCENE_POST_KIND_CANONICAL[text.toLowerCase()] || text;
  }

  function normalizeScenePostEffect(raw, index, fallback) {
    const current = sceneIsPlainObject(fallback) ? fallback : {};
    const item = sceneIsPlainObject(raw) ? raw : {};
    const kind = typeof item.kind === "string" && item.kind ? sceneCanonicalPostEffectKind(item.kind) : sceneCanonicalPostEffectKind(current.kind);
    if (!kind) {
      return null;
    }
    // Custom (shader-authored) post passes carry fields this normalizer knows
    // nothing about: the shader source, its layout, stage, backend and uniforms.
    // createSceneState() stores post effects RAW, so those fields survive the
    // mount path — but rebuilding an effect from the built-in whitelist below
    // silently stripped them, so any custom pass re-applied through
    // applyCommands (a progressive post-FX upgrade, say) arrived at the renderer
    // with no shader and rendered nothing. Preserve everything the caller
    // supplied, then overlay the normalized built-in knobs.
    const normalized = {
      kind,
      threshold: sceneNumberOrCSSVar(item.threshold, sceneNumber(current.threshold, 0)),
      intensity: sceneNumberOrCSSVar(item.intensity, sceneNumber(current.intensity, 0)),
      radius: sceneNumberOrCSSVar(item.radius, sceneNumber(current.radius, 0)),
      scale: sceneNumberOrCSSVar(item.scale, sceneNumber(current.scale, 0)),
      bias: sceneNumberOrCSSVar(item.bias, sceneNumber(current.bias, 0)),
      saturation: sceneNumberOrCSSVar(item.saturation, sceneNumber(current.saturation, 0)),
      contrast: sceneNumberOrCSSVar(item.contrast, sceneNumber(current.contrast, 0)),
      exposure: sceneNumberOrCSSVar(item.exposure, sceneNumber(current.exposure, 0)),
      focusDistance: sceneNumberOrCSSVar(item.focusDistance, sceneNumber(current.focusDistance, 0)),
      aperture: sceneNumberOrCSSVar(item.aperture, sceneNumber(current.aperture, 0)),
      maxBlur: sceneNumberOrCSSVar(item.maxBlur, sceneNumber(current.maxBlur, 0)),
      mode: typeof item.mode === "string" ? item.mode : (typeof current.mode === "string" ? current.mode : ""),
      id: typeof item.id === "string" && item.id ? item.id : (typeof current.id === "string" ? current.id : ("scene-postfx-" + index)),
    };
    return Object.assign({}, current, item, normalized);
  }

  function scenePostEffects(props) {
    const out = [];
    const raw = rawScenePostEffects(props);
    for (let index = 0; index < raw.length; index += 1) {
      const effect = normalizeScenePostEffect(raw[index], index, null);
      if (effect) {
        out.push(effect);
      }
    }
    return out;
  }

  // --------------------------------------------------------------------------
  // G2 QualityLadder — bidirectional work-based ABR (see scene/quality_ladder.go
  // for the Go-side schema and the PRIME DIRECTIVE this shape enforces by
  // construction: no resolution/DPR/postFX-pixel-budget knob exists on a rung).
  // Mirrors the props.scene.* vs top-level props.* dual-path convention used by
  // rawScenePostEffects above, so both Go-authored (props.scene.qualityLadder,
  // lowered from Props.QualityLadder) and directly JS-authored scenes work.
  // --------------------------------------------------------------------------

  function rawSceneQualityLadder(props) {
    const scene = sceneProps(props);
    if (scene && Array.isArray(scene.qualityLadder)) {
      return scene.qualityLadder;
    }
    return props && Array.isArray(props.qualityLadder) ? props.qualityLadder : [];
  }

  function sceneQualityLadderStartRungRaw(props) {
    const scene = sceneProps(props);
    if (scene && scene.qualityStartRung != null) {
      return scene.qualityStartRung;
    }
    return props && props.qualityStartRung;
  }

  function normalizeSceneQualityRung(raw, index) {
    const item = sceneIsPlainObject(raw) ? raw : {};
    const postEffects = Array.isArray(item.postEffects)
      ? item.postEffects.filter(function(v) { return typeof v === "string" && v.trim() !== ""; }).map(function(v) { return v.trim(); })
      : [];
    const layerGroups = Array.isArray(item.layerGroups)
      ? item.layerGroups.filter(function(v) { return typeof v === "string" && v.trim() !== ""; }).map(function(v) { return v.trim(); })
      : [];
    return {
      name: typeof item.name === "string" && item.name.trim() ? item.name.trim() : ("rung-" + index),
      postEffects: postEffects,
      layerGroups: layerGroups,
      // Zero/unset means the full authored budget (1.0) — the same "zero
      // means unset, gets the sane default" idiom the Go IR uses (see
      // resolveQualityRung in scene/quality_ladder.go).
      computeBudgetScale: (function() {
        const scale = sceneNumber(item.computeBudgetScale, 0);
        if (scale === 0) return 1;
        return Math.max(0, Math.min(1, scale));
      })(),
      pointBudgetScale: (function() {
        const scale = sceneNumber(item.pointBudgetScale, 0);
        if (scale === 0) return 1;
        return Math.max(0, Math.min(1, scale));
      })(),
      expensivePassCadence: Math.max(1, Math.floor(sceneNumber(item.expensivePassCadence, 1))),
    };
  }

  // sceneQualityLadder normalizes Props.QualityLadder (scene.qualityLadder,
  // lowered from Go) or a directly-authored top-level qualityLadder prop
  // into { rungs, startRung }. An empty rungs array means "no ladder
  // authored" — callers MUST preserve legacy dprCap-tier adaptiveQuality
  // behavior unchanged (back-compat; see G2 spec).
  function sceneQualityLadder(props) {
    const raw = rawSceneQualityLadder(props);
    const rungs = raw.map(function(entry, index) { return normalizeSceneQualityRung(entry, index); });
    const startRung = rungs.length > 0
      ? Math.max(0, Math.min(rungs.length - 1, Math.floor(sceneNumber(sceneQualityLadderStartRungRaw(props), 0))))
      : 0;
    return { rungs: rungs, startRung: startRung };
  }

  // scenePointQualityGroups normalizes Props.PointQualityGroups
  // (scene.pointQualityGroups, lowered from Go) or a directly-authored
  // top-level pointQualityGroups prop into a plain layer-name -> group Map.
  // This is the scene-level fallback the points draw filter consults for
  // point entries that cannot carry Points.QualityGroup directly — most
  // notably GLB-baked point layers extracted at runtime, which expose their
  // layer name on the SAME `material` field the named-material binding path
  // (sceneApplyNamedMaterialToPoints / sceneNamedMaterialForRecord) already
  // matches by. Same props.scene.* vs top-level props.* dual-path convention
  // as sceneQualityLadder above.
  function rawScenePointQualityGroups(props) {
    const scene = sceneProps(props);
    if (scene && sceneIsPlainObject(scene.pointQualityGroups)) {
      return scene.pointQualityGroups;
    }
    return props && sceneIsPlainObject(props.pointQualityGroups) ? props.pointQualityGroups : null;
  }

  function scenePointQualityGroups(props) {
    const raw = rawScenePointQualityGroups(props);
    const out = new Map();
    if (!raw) {
      return out;
    }
    for (const key of Object.keys(raw)) {
      const name = typeof key === "string" ? key.trim() : "";
      const group = typeof raw[key] === "string" ? raw[key].trim() : "";
      if (!name || !group) {
        continue;
      }
      out.set(name, group);
    }
    return out;
  }

  function sceneCamera(props) {
    const raw = props && props.camera && typeof props.camera === "object" ? props.camera : {};
    return normalizeSceneCamera(raw, {
      kind: "perspective",
      x: 0,
      y: 0,
      z: 6,
      fov: 75,
      near: 0.05,
      far: 128,
    });
  }

  function normalizeSceneCameraKind(value, fallback) {
    const kind = typeof value === "string" ? value.trim().toLowerCase() : "";
    if (kind === "orthographic" || kind === "ortho") {
      return "orthographic";
    }
    if (kind === "perspective" || kind === "persp") {
      return "perspective";
    }
    return fallback === "orthographic" ? "orthographic" : "perspective";
  }

  function sceneCanvasAlpha(props) {
    if (props && typeof props.canvasAlpha === "boolean") {
      return props.canvasAlpha;
    }
    const background = props && typeof props.background === "string"
      ? props.background.trim().toLowerCase()
      : "";
    return background === "transparent" || background === "rgba(0,0,0,0)" || background === "rgba(0, 0, 0, 0)";
  }

  function normalizeSceneCamera(raw, fallback) {
    const base = fallback || {};
    const kind = normalizeSceneCameraKind(raw.kind, base.kind);
    return {
      kind,
      x: sceneNumber(raw.x, sceneNumber(base.x, 0)),
      y: sceneNumber(raw.y, sceneNumber(base.y, 0)),
      z: sceneNumber(raw.z, sceneNumber(base.z, 6)),
      rotationX: sceneNumber(raw.rotationX, sceneNumber(base.rotationX, 0)),
      rotationY: sceneNumber(raw.rotationY, sceneNumber(base.rotationY, 0)),
      rotationZ: sceneNumber(raw.rotationZ, sceneNumber(base.rotationZ, 0)),
      fov: sceneNumber(raw.fov, sceneNumber(base.fov, 75)),
      left: sceneNumber(raw.left, sceneNumber(base.left, 0)),
      right: sceneNumber(raw.right, sceneNumber(base.right, 0)),
      top: sceneNumber(raw.top, sceneNumber(base.top, 0)),
      bottom: sceneNumber(raw.bottom, sceneNumber(base.bottom, 0)),
      zoom: sceneNumber(raw.zoom, sceneNumber(base.zoom, 1)),
      near: sceneNumber(raw.near, sceneNumber(base.near, 0.05)),
      far: sceneNumber(raw.far, sceneNumber(base.far, 128)),
    };
  }

  function normalizeSceneTextureDescriptor(raw, fallback) {
    const source = sceneIsPlainObject(raw) ? raw : (sceneIsPlainObject(fallback) ? fallback : null);
    if (!source || typeof source.uri !== "string" || !source.uri.trim()) {
      return null;
    }
    return {
      uri: source.uri.trim(),
      role: typeof source.role === "string" ? source.role.trim().toLowerCase() : "",
      colorSpace: typeof source.colorSpace === "string" ? source.colorSpace.trim().toLowerCase() : "",
      channels: typeof source.channels === "string" ? source.channels.trim().toLowerCase() : "",
      view: typeof source.view === "string" ? source.view.trim().toLowerCase() : "2d",
      format: typeof source.format === "string" ? source.format.trim().toLowerCase() : "",
      mipLevels: Math.max(0, Math.floor(sceneNumber(source.mipLevels, 0))),
      width: Math.max(0, Math.floor(sceneNumber(source.width, 0))),
      height: Math.max(0, Math.floor(sceneNumber(source.height, 0))),
      faces: Math.max(0, Math.floor(sceneNumber(source.faces, 0))),
    };
  }

  function normalizeSceneMaterialTextureDescriptors(raw, fallback) {
    const source = sceneIsPlainObject(raw) ? raw : {};
    const base = sceneIsPlainObject(fallback) ? fallback : {};
    const out = {};
    for (const name of ["baseColor", "normal", "roughness", "metalness", "occlusion", "emissive", "specularIntensity", "specularColor"]) {
      const descriptor = normalizeSceneTextureDescriptor(source[name], base[name]);
      if (descriptor) out[name] = descriptor;
    }
    if (sceneIsPlainObject(source.data) || sceneIsPlainObject(base.data)) {
      const dataSource = sceneIsPlainObject(source.data) ? source.data : base.data;
      const data = {};
      for (const name of Object.keys(dataSource)) {
        const descriptor = normalizeSceneTextureDescriptor(dataSource[name], null);
        if (descriptor) data[name] = descriptor;
      }
      if (Object.keys(data).length) out.data = data;
    }
    return Object.keys(out).length ? out : null;
  }

  function normalizeSceneEnvironmentIBL(raw, fallback) {
    const source = sceneIsPlainObject(raw) ? raw : (sceneIsPlainObject(fallback) ? fallback : null);
    if (!source) return null;
    const out = {
      schemaVersion: Math.max(0, Math.floor(sceneNumber(source.schemaVersion, 0))),
      source: typeof source.source === "string" ? source.source.trim() : "",
      radiance: normalizeSceneTextureDescriptor(source.radiance, null),
      irradiance: normalizeSceneTextureDescriptor(source.irradiance, null),
      brdfLUT: normalizeSceneTextureDescriptor(source.brdfLUT, null),
      brdfModel: typeof source.brdfModel === "string" ? source.brdfModel.trim() : "",
      roughnessPerLevel: Array.isArray(source.roughnessPerLevel)
        ? source.roughnessPerLevel.map(function(value) { return sceneNumber(value, 0); })
        : [],
      sphericalHarmonics: Array.isArray(source.sphericalHarmonics)
        ? sceneCloneData(source.sphericalHarmonics)
        : [],
    };
    return out.radiance || out.irradiance || out.brdfLUT || out.source ? out : null;
  }

  function normalizeSceneEnvironment(raw, fallback) {
    const base = sceneIsPlainObject(fallback) ? fallback : {};
    const source = sceneIsPlainObject(raw) ? raw : {};
    const lifecycle = sceneNormalizeLifecycle(source, base);
    const environment = {
      ambientColor: typeof source.ambientColor === "string" && source.ambientColor ? source.ambientColor : (typeof base.ambientColor === "string" ? base.ambientColor : ""),
      ambientIntensity: sceneClampNumberOrCSSVar(source.ambientIntensity, sceneNumber(base.ambientIntensity, 0), 0, 4),
      skyColor: typeof source.skyColor === "string" && source.skyColor ? source.skyColor : (typeof base.skyColor === "string" ? base.skyColor : ""),
      skyIntensity: sceneClampNumberOrCSSVar(source.skyIntensity, sceneNumber(base.skyIntensity, 0), 0, 4),
      groundColor: typeof source.groundColor === "string" && source.groundColor ? source.groundColor : (typeof base.groundColor === "string" ? base.groundColor : ""),
      groundIntensity: sceneClampNumberOrCSSVar(source.groundIntensity, sceneNumber(base.groundIntensity, 0), 0, 4),
      envMap: typeof source.envMap === "string" && source.envMap ? source.envMap : (typeof base.envMap === "string" ? base.envMap : ""),
      ibl: normalizeSceneEnvironmentIBL(source.ibl, base.ibl),
      envIntensity: sceneClampNumberOrCSSVar(Object.prototype.hasOwnProperty.call(source, "envIntensity") ? source.envIntensity : undefined, sceneNumber(base.envIntensity, 1) || 1, 0, 8),
      envRotation: sceneClampNumberOrCSSVar(source.envRotation, sceneNumber(base.envRotation, 0), Number.NEGATIVE_INFINITY, Number.POSITIVE_INFINITY),
      exposure: sceneClampNumberOrCSSVar(Object.prototype.hasOwnProperty.call(source, "exposure") ? source.exposure : undefined, sceneNumber(base.exposure, 1) || 1, 0.05, 4),
      toneMapping: typeof source.toneMapping === "string" && source.toneMapping ? source.toneMapping : (typeof base.toneMapping === "string" ? base.toneMapping : ""),
      fogColor: typeof source.fogColor === "string" && source.fogColor ? source.fogColor : (typeof base.fogColor === "string" ? base.fogColor : ""),
      fogDensity: sceneClampNumberOrCSSVar(source.fogDensity, sceneNumber(base.fogDensity, 0), 0, Number.POSITIVE_INFINITY),
      _transition: lifecycle.transition,
      _inState: lifecycle.inState,
      _outState: lifecycle.outState,
      _live: lifecycle.live,
      specified: false,
    };
    environment.specified = Boolean(raw || base.specified) && (
      environment.ambientColor ||
      environment.ambientIntensity !== 0 ||
      environment.skyColor ||
      environment.skyIntensity !== 0 ||
      environment.groundColor ||
      environment.groundIntensity !== 0 ||
      environment.envMap ||
      environment.ibl ||
      environment.envIntensity !== 1 ||
      environment.envRotation !== 0 ||
      environment.fogColor ||
      environment.fogDensity !== 0 ||
      environment.toneMapping ||
      Object.prototype.hasOwnProperty.call(source, "exposure")
    );
    // Cache env content hash for scenePBRLightsHash dirty-tracking.
    // Same rationale as _lightHash above — avoids re-walking fields on
    // every frame. sceneResolveLightingEnvironment rebuilds a new env
    // object per frame and must also stamp _envHash.
    if (typeof hashEnvironmentContent === "function") {
      environment._envHash = hashEnvironmentContent(environment);
    }
    return environment;
  }

  function sceneResolveLightingEnvironment(environment, hasLights) {
    const base = environment && typeof environment === "object" && Object.prototype.hasOwnProperty.call(environment, "specified")
      ? {
        ambientColor: typeof environment.ambientColor === "string" ? environment.ambientColor : "",
        ambientIntensity: sceneClampNumberOrCSSVar(environment.ambientIntensity, 0, 0, 4),
        skyColor: typeof environment.skyColor === "string" ? environment.skyColor : "",
        skyIntensity: sceneClampNumberOrCSSVar(environment.skyIntensity, 0, 0, 4),
        groundColor: typeof environment.groundColor === "string" ? environment.groundColor : "",
        groundIntensity: sceneClampNumberOrCSSVar(environment.groundIntensity, 0, 0, 4),
        envMap: typeof environment.envMap === "string" ? environment.envMap : "",
        ibl: normalizeSceneEnvironmentIBL(environment.ibl, null),
        envIntensity: sceneClampNumberOrCSSVar(environment.envIntensity, 1, 0, 8),
        envRotation: sceneClampNumberOrCSSVar(environment.envRotation, 0, Number.NEGATIVE_INFINITY, Number.POSITIVE_INFINITY),
        exposure: sceneClampNumberOrCSSVar(environment.exposure, 1, 0.05, 4),
        toneMapping: typeof environment.toneMapping === "string" ? environment.toneMapping : "",
        fogColor: typeof environment.fogColor === "string" ? environment.fogColor : "",
        fogDensity: sceneClampNumberOrCSSVar(environment.fogDensity, 0, 0, Number.POSITIVE_INFINITY),
        specified: Boolean(environment.specified),
      }
      : normalizeSceneEnvironment(environment, null);
    // Stamp _envHash for the manually-copied branch (normalizeSceneEnvironment
    // handles the other branch). sceneResolveLightingEnvironment runs per
    // bundle build, so the cost amortizes across the whole frame, not per
    // scenePBRUploadLights call.
    if (typeof base._envHash !== "number" && typeof hashEnvironmentContent === "function") {
      base._envHash = hashEnvironmentContent(base);
    }
    if (base.specified || !hasLights) {
      return base;
    }
    return normalizeSceneEnvironment({
      ambientColor: "#f5fbff",
      ambientIntensity: 0.18,
      skyColor: "#d5ebff",
      skyIntensity: 0.12,
      groundColor: "#102030",
      groundIntensity: 0.04,
      exposure: base.exposure,
    }, base);
  }

  function createSceneState(props, capability) {
    // Decompress any TurboQuant-compressed vertex data before the render loop.
    // Progressive mode: decompress preview now, schedule the full-resolution
    // upgrade after the first frame.
    //
    // 11a-scene-decompress.ts now ships in the lazily fetched decompress
    // chunk, so resolve through the API object rather than lexically. The
    // mount awaits settleSceneDecompressFeature before it calls this function,
    // so the chunk has landed by now for any scene that needs it. A scene with
    // plain float arrays and no generator finds nothing here and needs
    // nothing. bootstrap.js keeps the file inline and wins on the first test.
    if (typeof sceneDecompressProps === "function") {
      sceneDecompressProps(props);
    } else if (typeof window !== "undefined"
      && window.__gosx_scene3d_api
      && typeof window.__gosx_scene3d_api.sceneDecompressProps === "function") {
      window.__gosx_scene3d_api.sceneDecompressProps(props);
    }
    const scene = sceneProps(props);
    const postEffects = scenePostEffects(props);
    const postFXMaxPixels = Math.max(0, Math.floor(sceneNumber(
      scene && scene.postFXMaxPixels,
      sceneNumber(props && props.postFXMaxPixels, 0),
    )));
    const deferPostFX = sceneBool(props && props.deferPostFX, sceneBool(props && props.progressivePostFX, false)) && postEffects.length > 0;
    const state = {
      background: typeof props.background === "string" && props.background ? props.background : "#08151f",
      camera: sceneCamera(props),
      objects: new Map(),
      labels: new Map(),
      sprites: new Map(),
      html: new Map(),
      lights: new Map(),
      models: sceneModels(props),
      points: scenePoints(props),
      pointQualityGroups: scenePointQualityGroups(props),
      instancedMeshes: sceneInstancedMeshes(props),
      instancedGLBMeshes: sceneInstancedGLBMeshes(props),
      computeParticles: sceneComputeParticles(props),
      waterSystems: sceneWaterSystems(props),
      animations: sceneAnimations(props),
      materials: sceneMaterials(props, capability),
      _materialSource: rawSceneMaterials(props).map(sceneCloneData),
      capability: capability || null,
      postEffects: deferPostFX ? [] : postEffects,
      postFXMaxPixels: postFXMaxPixels,
      _deferredPostEffects: deferPostFX ? postEffects : null,
      _adaptiveSourcePostEffects: postEffects,
      _transitions: [],
      _scrollCamera: (
        sceneNumber(props.scrollCameraStart, 0) !== 0 ||
        sceneNumber(props.scrollCameraEnd, 0) !== 0 ||
        sceneNumber(props.scrollCameraOffset && props.scrollCameraOffset.x, 0) !== 0 ||
        sceneNumber(props.scrollCameraOffset && props.scrollCameraOffset.y, 0) !== 0 ||
        sceneNumber(props.scrollCameraOffset && props.scrollCameraOffset.z, 0) !== 0
      )
        ? {
            start: sceneNumber(props.scrollCameraStart, 0),
            end: sceneNumber(props.scrollCameraEnd, 0),
            offset: {
              x: sceneNumber(props.scrollCameraOffset && props.scrollCameraOffset.x, 0),
              y: sceneNumber(props.scrollCameraOffset && props.scrollCameraOffset.y, 0),
              z: sceneNumber(props.scrollCameraOffset && props.scrollCameraOffset.z, 0),
            },
          }
        : null,
      environment: normalizeSceneEnvironment(rawSceneEnvironment(props), null),
    };
    state._waterShaderSourceByID = sceneWaterShaderSourceMap(state.waterSystems);
    scenePublishWaterShaderSourceMap(state._waterShaderSourceByID);
    for (const object of sceneObjects(props)) {
      state.objects.set(object.id, object);
    }
    for (const label of sceneLabels(props)) {
      state.labels.set(label.id, label);
    }
    for (const sprite of sceneSprites(props)) {
      state.sprites.set(sprite.id, sprite);
    }
    for (const entry of sceneHTML(props)) {
      state.html.set(entry.id, entry);
    }
    for (const light of sceneLights(props)) {
      state.lights.set(light.id, light);
    }
    return state;
  }

  function sceneStateObjects(state) {
    return Array.from(state.objects.values());
  }

  function sceneStateObjectsWithMaterials(state) {
    const objects = sceneStateObjects(state);
    const lookup = sceneMaterialLookup(state);
    if (!lookup.size) {
      return objects;
    }
    return objects.map(function(object) {
      const material = sceneNamedMaterialForRecord(lookup, object);
      if (!material) {
        return object;
      }
      return sceneApplyNamedMaterialToObject(object, material);
    });
  }

  function sceneStatePointsWithMaterials(state) {
    const points = Array.isArray(state && state.points) ? state.points : [];
    const lookup = sceneMaterialLookup(state);
    if (!lookup.size) {
      return points;
    }
    return points.map(function(point) {
      const material = sceneNamedMaterialForRecord(lookup, point);
      if (!material) {
        return point;
      }
      return sceneApplyNamedMaterialToPoints(point, material);
    });
  }

  function sceneStateInstancedMeshesWithMaterials(state) {
    const meshes = Array.isArray(state && state.instancedMeshes) ? state.instancedMeshes : [];
    const lookup = sceneMaterialLookup(state);
    if (!lookup.size) {
      return meshes;
    }
    return meshes.map(function(mesh) {
      const material = sceneNamedMaterialForRecord(lookup, mesh);
      if (!material) {
        return mesh;
      }
      return sceneApplyNamedMaterialToInstancedMesh(mesh, material);
    });
  }

  function sceneMaterialLookup(state) {
    const materials = Array.isArray(state && state.materials) ? state.materials : [];
    if (!materials.length) {
      return new Map();
    }
    const lookup = new Map();
    for (let index = 0; index < materials.length; index += 1) {
      const material = materials[index];
      if (!material) {
        continue;
      }
      if (material.name) {
        lookup.set(String(material.name), material);
      }
      if (material.id) {
        lookup.set(String(material.id), material);
      }
    }
    return lookup;
  }

  function sceneNamedMaterialForRecord(lookup, record) {
    const materialName = record && record.material ? String(record.material) : "";
    return materialName ? lookup.get(materialName) || null : null;
  }

  // C3: resolve the live customUniforms bag that sceneStateObjectsWithMaterials
  // reads each frame for the object keyed `meshId`. If the object references a
  // named material (state.materials), that material's customUniforms is the
  // source the per-frame Object.assign clones — so we return it (creating it if
  // absent). Otherwise the object carries customUniforms inline and we return
  // that. Returns null when no object exists. The returned object is mutated in
  // place by the caller, and the change is observed on the NEXT bundle build.
  function sceneResolveMaterialUniforms(state, meshId) {
    if (!state || !state.objects || typeof state.objects.get !== "function") {
      return null;
    }
    const record = state.objects.get(String(meshId));
    if (!record) {
      return null;
    }
    const lookup = sceneMaterialLookup(state);
    const material = lookup.size ? sceneNamedMaterialForRecord(lookup, record) : null;
    const target = material || record;
    if (!sceneIsPlainObject(target.customUniforms)) {
      target.customUniforms = {};
    }
    return target.customUniforms;
  }

  function sceneApplyNamedMaterialToObject(object, material) {
    return Object.assign({}, object, {
      materialKind: material.kind || object.materialKind,
      color: material.color || object.color,
      texture: material.texture || object.texture,
      opacity: material.opacity != null ? material.opacity : object.opacity,
      unlit: material.unlit !== undefined ? material.unlit : object.unlit,
      emissive: material.emissive != null ? material.emissive : object.emissive,
      roughness: material.roughness != null ? material.roughness : object.roughness,
      metalness: material.metalness != null ? material.metalness : object.metalness,
      ior: material.ior != null ? material.ior : object.ior,
      specularIntensity: material.specularIntensity != null ? material.specularIntensity : object.specularIntensity,
      specularColor: material.specularColor != null ? material.specularColor : object.specularColor,
      clearcoat: material.clearcoat != null ? material.clearcoat : object.clearcoat,
      sheen: material.sheen != null ? material.sheen : object.sheen,
      transmission: material.transmission != null ? material.transmission : object.transmission,
      iridescence: material.iridescence != null ? material.iridescence : object.iridescence,
      anisotropy: material.anisotropy != null ? material.anisotropy : object.anisotropy,
      alphaCutoff: material.alphaCutoff !== undefined ? material.alphaCutoff : object.alphaCutoff,
      normalMap: material.normalMap || object.normalMap,
      roughnessMap: material.roughnessMap || object.roughnessMap,
      metalnessMap: material.metalnessMap || object.metalnessMap,
      occlusionMap: material.occlusionMap || object.occlusionMap,
      emissiveMap: material.emissiveMap || object.emissiveMap,
      textureDescriptors: material.textureDescriptors || object.textureDescriptors,
      blendMode: material.blendMode || object.blendMode,
      renderPass: material.renderPass || object.renderPass,
      // Provenance follows the same source chosen for the routed value:
      // a raw (unmarked) named material route is authored, not computed.
      _blendModeDerived: material.blendMode
        ? material._blendModeDerived === true
        : object._blendModeDerived,
      _renderPassDerived: material.renderPass
        ? material._renderPassDerived === true
        : object._renderPassDerived,
      wireframe: material.wireframe != null ? material.wireframe : object.wireframe,
      depthWrite: material.depthWrite != null ? material.depthWrite : object.depthWrite,
      lineDash: material.lineDash != null ? material.lineDash : object.lineDash,
      dashSize: material.dashSize != null ? material.dashSize : object.dashSize,
      gapSize: material.gapSize != null ? material.gapSize : object.gapSize,
      customVertex: typeof material.customVertex === "string" ? material.customVertex : object.customVertex,
      customFragment: typeof material.customFragment === "string" ? material.customFragment : object.customFragment,
      customVertexWGSL: typeof material.customVertexWGSL === "string" ? material.customVertexWGSL : object.customVertexWGSL,
      customFragmentWGSL: typeof material.customFragmentWGSL === "string" ? material.customFragmentWGSL : object.customFragmentWGSL,
      customUniforms: sceneIsPlainObject(material.customUniforms) ? Object.assign({}, material.customUniforms) : object.customUniforms,
      shaderBackend: typeof material.shaderBackend === "string" ? material.shaderBackend : object.shaderBackend,
      shaderLayout: sceneIsPlainObject(material.shaderLayout) ? sceneCloneData(material.shaderLayout) : object.shaderLayout,
      shaderSource: typeof material.shaderSource === "string" ? material.shaderSource : object.shaderSource,
      shaderSourceFiles: sceneIsPlainObject(material.shaderSourceFiles) ? sceneCloneData(material.shaderSourceFiles) : object.shaderSourceFiles,
      variantKey: material.variantKey || object.variantKey,
    });
  }

  function sceneApplyNamedMaterialToInstancedMesh(mesh, material) {
    return sceneApplyNamedMaterialToObject(mesh, material);
  }

  function sceneApplyNamedMaterialToPoints(point, material) {
    const nextValues = {
      color: material._colorSpecified ? material.color : point.color,
      style: material.style || point.style,
      size: material.size != null ? material.size : point.size,
      opacity: material._opacitySpecified ? material.opacity : point.opacity,
      blendMode: material._blendModeSpecified ? material.blendMode : point.blendMode,
      depthWrite: material._depthWriteSpecified ? material.depthWrite : point.depthWrite,
      attenuation: material.attenuation != null ? material.attenuation : point.attenuation,
      // A named <Material> may declare a screen-space floor for the point
      // layers it is bound to. Without this a GLB-derived layer could not be
      // floored at all, because its render properties come from the material
      // rather than from a Points struct.
      minPixelSize: material.minPixelSize != null ? material.minPixelSize : point.minPixelSize,
      maxPixelSize: material.maxPixelSize != null ? material.maxPixelSize : point.maxPixelSize,
      // Authored-shader envelope: profile fields win over point defaults (empty string = not authored).
      customVertex: typeof material.customVertex === "string" && material.customVertex ? material.customVertex : (point.customVertex || ""),
      customFragment: typeof material.customFragment === "string" && material.customFragment ? material.customFragment : (point.customFragment || ""),
      customVertexWGSL: typeof material.customVertexWGSL === "string" && material.customVertexWGSL ? material.customVertexWGSL : (point.customVertexWGSL || ""),
      customFragmentWGSL: typeof material.customFragmentWGSL === "string" && material.customFragmentWGSL ? material.customFragmentWGSL : (point.customFragmentWGSL || ""),
      customUniforms: sceneIsPlainObject(material.customUniforms) ? Object.assign({}, material.customUniforms) : (point.customUniforms || null),
      shaderBackend: typeof material.shaderBackend === "string" && material.shaderBackend ? material.shaderBackend : (point.shaderBackend || ""),
      shaderLayout: sceneIsPlainObject(material.shaderLayout) ? sceneCloneData(material.shaderLayout) : (point.shaderLayout || null),
      shaderSource: typeof material.shaderSource === "string" && material.shaderSource ? material.shaderSource : (point.shaderSource || ""),
      shaderSourceFiles: sceneIsPlainObject(material.shaderSourceFiles) ? sceneCloneData(material.shaderSourceFiles) : (point.shaderSourceFiles || null),
    };
    const cache = point._namedMaterialCache;
    if (
      cache &&
      cache.material === material &&
      cache.positions === point.positions &&
      cache.sizes === point.sizes &&
      cache.colors === point.colors &&
      cache.color === nextValues.color &&
      cache.style === nextValues.style &&
      cache.size === nextValues.size &&
      cache.opacity === nextValues.opacity &&
      cache.blendMode === nextValues.blendMode &&
      cache.depthWrite === nextValues.depthWrite &&
      cache.attenuation === nextValues.attenuation &&
      cache.customVertexWGSL === nextValues.customVertexWGSL &&
      cache.customFragmentWGSL === nextValues.customFragmentWGSL &&
      cache.customVertex === nextValues.customVertex &&
      cache.customFragment === nextValues.customFragment &&
      cache.shaderSource === nextValues.shaderSource
    ) {
      return cache.value;
    }
    const previousValue = cache &&
      cache.positions === point.positions &&
      cache.sizes === point.sizes &&
      cache.colors === point.colors &&
      cache.value
        ? cache.value
        : null;
    const value = Object.assign({}, point, nextValues);
    if (previousValue) {
      if (previousValue._cachedPos) {
        value._cachedPos = previousValue._cachedPos;
      }
      if (previousValue._cachedSizes) {
        value._cachedSizes = previousValue._cachedSizes;
      }
      if (previousValue._cachedColors) {
        value._cachedColors = previousValue._cachedColors;
      }
    }
    point._namedMaterialCache = Object.assign({
      material,
      positions: point.positions,
      sizes: point.sizes,
      colors: point.colors,
      value,
    }, nextValues);
    return value;
  }

  function sceneStateLabels(state) {
    return Array.from(state.labels.values());
  }

  function sceneStateSprites(state) {
    return Array.from(state.sprites.values());
  }

  function sceneStateHTML(state) {
    return Array.from(state.html.values());
  }

  function sceneStateLights(state) {
    return Array.from(state.lights.values());
  }

  function sceneStateTransitions(state) {
    if (!state || !Array.isArray(state._transitions)) {
      return [];
    }
    return state._transitions;
  }

  function sceneHasActiveTransitions(state) {
    return sceneStateTransitions(state).length > 0;
  }

  function sceneNowMilliseconds() {
    if (typeof window !== "undefined" && window.performance && typeof window.performance.now === "function") {
      return window.performance.now();
    }
    return Date.now();
  }

  function sceneTransitionTimingForPhase(entry, phase) {
    const transition = sceneIsPlainObject(entry && entry._transition) ? entry._transition : null;
    const hasPhase = transition && Object.prototype.hasOwnProperty.call(transition, phase);
    const fallback = transition && phase === "update" && !hasPhase ? transition.in : null;
    const timing = transition && sceneIsPlainObject(transition[phase]) ? transition[phase] : null;
    const duration = Math.max(0, Math.round(sceneNumber(timing && timing.duration, sceneNumber(fallback && fallback.duration, 0))));
    const easing = normalizeSceneEasing((timing && timing.easing) || (fallback && fallback.easing));
    return { duration, easing };
  }

  function sceneTransitionEase(easing, t) {
    const clamped = sceneClamp(sceneNumber(t, 0), 0, 1);
    switch (normalizeSceneEasing(easing)) {
      case "ease-in":
        return clamped * clamped;
      case "ease-out":
        return clamped * (2 - clamped);
      case "ease-in-out":
        return clamped < 0.5 ? 2 * clamped * clamped : -1 + (4 - 2 * clamped) * clamped;
      default:
        return clamped;
    }
  }

  function sceneTransitionColorLike(key, from, to) {
    if (typeof from !== "string" || typeof to !== "string") {
      return false;
    }
    if (typeof key === "string" && key.toLowerCase().indexOf("color") >= 0) {
      return true;
    }
    return /^#|^rgba?\(/i.test(from.trim()) && /^#|^rgba?\(/i.test(to.trim());
  }

  function sceneRGBAToHSL(rgba) {
    const r = clamp01(sceneNumber(rgba && rgba[0], 0));
    const g = clamp01(sceneNumber(rgba && rgba[1], 0));
    const b = clamp01(sceneNumber(rgba && rgba[2], 0));
    const a = clamp01(sceneNumber(rgba && rgba[3], 1));
    const max = Math.max(r, g, b);
    const min = Math.min(r, g, b);
    const delta = max - min;
    let h = 0;
    let s = 0;
    const l = (max + min) / 2;
    if (delta > 0.000001) {
      s = l > 0.5 ? delta / (2 - max - min) : delta / (max + min);
      switch (max) {
        case r:
          h = ((g - b) / delta) + (g < b ? 6 : 0);
          break;
        case g:
          h = ((b - r) / delta) + 2;
          break;
        default:
          h = ((r - g) / delta) + 4;
          break;
      }
      h /= 6;
    }
    return [h, s, l, a];
  }

  function sceneHueToRGB(p, q, t) {
    let value = t;
    if (value < 0) value += 1;
    if (value > 1) value -= 1;
    if (value < 1 / 6) return p + (q - p) * 6 * value;
    if (value < 1 / 2) return q;
    if (value < 2 / 3) return p + (q - p) * (2 / 3 - value) * 6;
    return p;
  }

  function sceneHSLToRGBA(hsla) {
    const h = sceneNumber(hsla && hsla[0], 0);
    const s = clamp01(sceneNumber(hsla && hsla[1], 0));
    const l = clamp01(sceneNumber(hsla && hsla[2], 0));
    const a = clamp01(sceneNumber(hsla && hsla[3], 1));
    if (s <= 0.000001) {
      return [l, l, l, a];
    }
    const q = l < 0.5 ? l * (1 + s) : l + s - (l * s);
    const p = 2 * l - q;
    return [
      sceneHueToRGB(p, q, h + (1 / 3)),
      sceneHueToRGB(p, q, h),
      sceneHueToRGB(p, q, h - (1 / 3)),
      a,
    ];
  }

  function sceneLerpColorString(from, to, t) {
    const left = sceneColorRGBA(from, [0, 0, 0, 1]);
    const right = sceneColorRGBA(to, left);
    const leftHSL = sceneRGBAToHSL(left);
    const rightHSL = sceneRGBAToHSL(right);
    const achromatic = leftHSL[1] <= 0.0001 && rightHSL[1] <= 0.0001;
    let rgba;
    if (achromatic) {
      rgba = [
        left[0] + (right[0] - left[0]) * t,
        left[1] + (right[1] - left[1]) * t,
        left[2] + (right[2] - left[2]) * t,
        left[3] + (right[3] - left[3]) * t,
      ];
    } else {
      let hueDelta = rightHSL[0] - leftHSL[0];
      if (hueDelta > 0.5) hueDelta -= 1;
      if (hueDelta < -0.5) hueDelta += 1;
      rgba = sceneHSLToRGBA([
        (leftHSL[0] + hueDelta * t + 1) % 1,
        leftHSL[1] + (rightHSL[1] - leftHSL[1]) * t,
        leftHSL[2] + (rightHSL[2] - leftHSL[2]) * t,
        leftHSL[3] + (rightHSL[3] - leftHSL[3]) * t,
      ]);
    }
    return sceneRGBAString(rgba);
  }

  function sceneTransitionValuesEqual(left, right) {
    if (left === right) {
      return true;
    }
    if (Array.isArray(left) || Array.isArray(right)) {
      if (!Array.isArray(left) || !Array.isArray(right) || left.length !== right.length) {
        return false;
      }
      for (let i = 0; i < left.length; i += 1) {
        if (!sceneTransitionValuesEqual(left[i], right[i])) {
          return false;
        }
      }
      return true;
    }
    if (sceneIsPlainObject(left) && sceneIsPlainObject(right)) {
      const keys = new Set(Object.keys(left).concat(Object.keys(right)));
      for (const key of keys) {
        if (sceneTransitionMetadataKey(key)) {
          continue;
        }
        if (!sceneTransitionValuesEqual(left[key], right[key])) {
          return false;
        }
      }
      return true;
    }
    return false;
  }

  function sceneTransitionBuildDelta(from, to, keyName) {
    if (sceneTransitionValuesEqual(from, to)) {
      return null;
    }
    if (sceneIsPlainObject(from) && sceneIsPlainObject(to)) {
      const delta = {};
      const keys = new Set(Object.keys(from).concat(Object.keys(to)));
      for (const key of keys) {
        if (sceneTransitionMetadataKey(key)) {
          continue;
        }
        const child = sceneTransitionBuildDelta(from[key], to[key], key);
        if (child !== null) {
          delta[key] = child;
        }
      }
      return Object.keys(delta).length > 0 ? delta : null;
    }
    return {
      __from: sceneCloneData(from),
      __to: sceneCloneData(to),
      __key: typeof keyName === "string" ? keyName : "",
    };
  }

  function sceneTransitionLeafValue(from, to, t, keyName) {
    if (typeof from === "number" && typeof to === "number" && Number.isFinite(from) && Number.isFinite(to)) {
      const value = from + (to - from) * t;
      return Number.isInteger(from) && Number.isInteger(to) ? Math.round(value) : value;
    }
    if (sceneTransitionColorLike(keyName, from, to)) {
      return sceneLerpColorString(from, to, t);
    }
    return sceneCloneData(to);
  }

  function sceneTransitionPatchAt(delta, t) {
    if (!delta || typeof delta !== "object") {
      return null;
    }
    if (Object.prototype.hasOwnProperty.call(delta, "__from")) {
      return sceneTransitionLeafValue(delta.__from, delta.__to, t, delta.__key);
    }
    const patch = {};
    const keys = Object.keys(delta);
    for (let i = 0; i < keys.length; i += 1) {
      const key = keys[i];
      patch[key] = sceneTransitionPatchAt(delta[key], t);
    }
    return patch;
  }

  function sceneApplyTransitionPatch(target, patch) {
    if (!sceneIsPlainObject(target) || patch == null) {
      return;
    }
    const keys = Object.keys(patch);
    for (let i = 0; i < keys.length; i += 1) {
      const key = keys[i];
      const value = patch[key];
      if (sceneIsPlainObject(value) && sceneIsPlainObject(target[key])) {
        sceneApplyTransitionPatch(target[key], value);
      } else {
        target[key] = sceneCloneData(value);
        // Invalidate paired GPU buffer caches when the source array is
        // replaced. Points entries cache the uploaded GPU buffer in
        // _cachedColors / _cachedPos / _cachedSizes (set by
        // normalizeScenePointsEntry), and the renderer keeps using the
        // cached buffer until it sees the field cleared. Without this,
        // any live event that updates `colors`, `positions`, or `sizes`
        // mutates the source array but leaves the renderer uploading
        // the stale GPU buffer forever — every frame after the live
        // event reads the wrong data and the visual is unchanged.
        // Discovered while debugging the m31labs.dev galaxy hub: the
        // server broadcasts new colors at every time-of-day boundary,
        // the client receives them via the gosx:hub:event chain, the
        // patch correctly mutates entry.colors — but the galaxy never
        // actually re-paints because _cachedColors is never invalidated.
        if (key === "colors") {
          if (Object.prototype.hasOwnProperty.call(target, "_cachedColors")) {
            target._cachedColors = null;
          }
          if (Object.prototype.hasOwnProperty.call(target, "_cachedInstanceColors")) {
            target._cachedInstanceColors = null;
          }
        } else if (key === "positions" && Object.prototype.hasOwnProperty.call(target, "_cachedPos")) {
          target._cachedPos = null;
        } else if (key === "sizes" && Object.prototype.hasOwnProperty.call(target, "_cachedSizes")) {
          target._cachedSizes = null;
        } else if (key === "transforms" && Object.prototype.hasOwnProperty.call(target, "_cachedTransforms")) {
          // Same pattern for instanced mesh transforms — without this,
          // live transition patches mutating mesh.transforms would
          // leave the renderer binding the stale cached typed array
          // (and the WeakMap-keyed VBO bound to it) forever.
          target._cachedTransforms = null;
        }
      }
    }
    // If the patched target is a light or environment with a cached
    // content sub-hash, re-stamp it to reflect the mutated fields.
    // Without this, scenePBRLightsHash would read a stale _lightHash
    // (or _envHash) and miss content changes coming from transitions,
    // causing stale uniform state on screen.
    //
    // Duck-typed on the presence of the stamp rather than an explicit
    // kind check so this helper stays generic across light / env /
    // object transitions — only entries that opted into the stamped
    // fast path get re-stamped.
    if (typeof target._lightHash === "number" && typeof hashLightContent === "function") {
      target._lightHash = hashLightContent(target);
    }
    if (typeof target._envHash === "number" && typeof hashEnvironmentContent === "function") {
      target._envHash = hashEnvironmentContent(target);
    }
  }

  function sceneInstantLiveBufferKeys(kind) {
    switch (kind) {
      case "points":
        return ["count", "positions", "sizes", "colors"];
      case "instanced":
        return ["count", "transforms"];
      default:
        return [];
    }
  }

  function sceneCloneTransitionData(kind, value) {
    if (!sceneIsPlainObject(value)) {
      return sceneCloneData(value);
    }
    const bufferKeys = sceneInstantLiveBufferKeys(kind);
    const clone = {};
    const keys = Object.keys(value);
    for (let i = 0; i < keys.length; i += 1) {
      const key = keys[i];
      if (sceneTransitionMetadataKey(key) || bufferKeys.indexOf(key) >= 0) {
        continue;
      }
      clone[key] = sceneCloneData(value[key]);
    }
    return clone;
  }

  function sceneApplyInstantLiveBufferPatch(kind, entry, target, payload) {
    if (!sceneIsPlainObject(entry) || !sceneIsPlainObject(target) || !sceneIsPlainObject(payload)) {
      return false;
    }
    const keys = sceneInstantLiveBufferKeys(kind);
    if (!keys.length) {
      return false;
    }
    let changed = false;
    for (let i = 0; i < keys.length; i += 1) {
      const key = keys[i];
      if (!Object.prototype.hasOwnProperty.call(payload, key)) {
        continue;
      }
      if (!sceneTransitionValuesEqual(entry[key], target[key])) {
        const patch = {};
        patch[key] = target[key];
        sceneApplyTransitionPatch(entry, patch);
        changed = true;
      }
      // Keep the transition target aligned to the just-applied buffer so
      // sceneTransitionBuildDelta never captures large arrays into a tween.
      target[key] = entry[key];
    }
    return changed;
  }

  function sceneTransitionKey(kind, entry) {
    return String(kind || "scene") + ":" + String(entry && entry.id ? entry.id : "__singleton");
  }

  function sceneCancelEntryTransition(state, kind, entry) {
    if (!state || !Array.isArray(state._transitions)) {
      return;
    }
    const key = sceneTransitionKey(kind, entry);
    state._transitions = state._transitions.filter(function(item) {
      return item.key !== key;
    });
  }

  function sceneNormalizeEntryByKind(kind, raw, fallback) {
    switch (kind) {
      case "object":
        return normalizeSceneObject(raw, fallback && fallback.id ? fallback.id : 0, fallback);
      case "label":
        return normalizeSceneLabel(raw, fallback && fallback.id ? fallback.id : 0, fallback);
      case "sprite":
        return normalizeSceneSprite(raw, fallback && fallback.id ? fallback.id : 0, fallback);
      case "html":
        return normalizeSceneHTML(raw, fallback && fallback.id ? fallback.id : 0, fallback);
      case "light":
        return normalizeSceneLight(raw, fallback && fallback.id ? fallback.id : 0, fallback);
      case "points":
        return normalizeScenePointsEntry(raw, fallback && fallback.id ? fallback.id : 0, fallback);
      case "instanced":
        return normalizeSceneInstancedMeshEntry(raw, fallback && fallback.id ? fallback.id : 0, fallback);
      case "compute":
        return normalizeSceneComputeParticlesEntry(raw, fallback && fallback.id ? fallback.id : 0, fallback);
      case "environment":
        return normalizeSceneEnvironment(raw, fallback);
      default:
        return sceneCloneData(fallback || raw);
    }
  }

  function sceneDefaultTransitionPatch(kind) {
    switch (kind) {
      case "object":
      case "points":
      case "sprite":
      case "html":
        return { opacity: 0 };
      case "light":
        return { intensity: 0 };
      case "compute":
        return { count: 0, material: { opacity: 0 } };
      case "environment":
        return { ambientIntensity: 0, skyIntensity: 0, groundIntensity: 0, fogDensity: 0 };
      default:
        return null;
    }
  }

  function sceneTransitionStatePatch(kind, entry, phase) {
    const statePatch = phase === "out" ? entry && entry._outState : entry && entry._inState;
    if (sceneIsPlainObject(statePatch) && Object.keys(statePatch).length > 0) {
      return sceneCloneData(statePatch);
    }
    return null;
  }

  function sceneStartEntryTransition(state, kind, entry, reducedMotion, nowMs) {
    if (!entry || !sceneIsPlainObject(entry)) {
      return false;
    }
    const timing = sceneTransitionTimingForPhase(entry, "in");
    let startPatch = sceneTransitionStatePatch(kind, entry, "in");
    if (!startPatch && timing.duration > 0) {
      startPatch = sceneCloneData(sceneDefaultTransitionPatch(kind));
    }
    if ((!startPatch || !Object.keys(startPatch).length) && timing.duration <= 0) {
      return false;
    }
    const fullTarget = sceneCloneData(entry);
    const fullStartState = sceneNormalizeEntryByKind(kind, startPatch || {}, fullTarget);
    const target = sceneCloneTransitionData(kind, fullTarget);
    const startState = sceneCloneTransitionData(kind, fullStartState);
    sceneApplyTransitionPatch(entry, startState);
    if (reducedMotion || timing.duration <= 0) {
      sceneApplyTransitionPatch(entry, target);
      return false;
    }
    const delta = sceneTransitionBuildDelta(startState, target, "");
    if (!delta) {
      sceneApplyTransitionPatch(entry, target);
      return false;
    }
    sceneCancelEntryTransition(state, kind, entry);
    sceneStateTransitions(state).push({
      key: sceneTransitionKey(kind, entry),
      entry,
      target,
      delta,
      startTime: nowMs,
      duration: Math.max(1, timing.duration),
      easing: timing.easing,
    });
    return true;
  }

  function scenePrimeInitialTransitions(state, reducedMotion, nowMs) {
    let started = false;
    if (state && sceneIsPlainObject(state.environment)) {
      started = sceneStartEntryTransition(state, "environment", state.environment, reducedMotion, nowMs) || started;
    }
    const collections = [
      ["object", sceneStateObjects(state)],
      ["label", sceneStateLabels(state)],
      ["sprite", sceneStateSprites(state)],
      ["html", sceneStateHTML(state)],
      ["light", sceneStateLights(state)],
      ["points", Array.isArray(state && state.points) ? state.points : []],
      ["instanced", Array.isArray(state && state.instancedMeshes) ? state.instancedMeshes : []],
      ["compute", Array.isArray(state && state.computeParticles) ? state.computeParticles : []],
    ];
    for (let ci = 0; ci < collections.length; ci += 1) {
      const kind = collections[ci][0];
      const entries = collections[ci][1];
      for (let i = 0; i < entries.length; i += 1) {
        started = sceneStartEntryTransition(state, kind, entries[i], reducedMotion, nowMs) || started;
      }
    }
    return started;
  }

  function sceneAdvanceTransitions(state, nowMs) {
    const active = sceneStateTransitions(state);
    if (!active.length) {
      return false;
    }
    const next = [];
    for (let i = 0; i < active.length; i += 1) {
      const transition = active[i];
      const elapsed = Math.max(0, nowMs - sceneNumber(transition.startTime, 0));
      const rawT = sceneClamp(elapsed / Math.max(1, sceneNumber(transition.duration, 1)), 0, 1);
      const eased = sceneTransitionEase(transition.easing, rawT);
      const patch = sceneTransitionPatchAt(transition.delta, eased);
      if (patch) {
        sceneApplyTransitionPatch(transition.entry, patch);
      }
      if (rawT >= 1) {
        sceneApplyTransitionPatch(transition.entry, transition.target);
      } else {
        next.push(transition);
      }
    }
    state._transitions = next;
    return true;
  }

  function sceneEntryListensToEvent(entry, eventName) {
    if (!entry || !Array.isArray(entry._live) || !eventName) {
      return false;
    }
    return entry._live.indexOf(eventName) >= 0;
  }

  function sceneApplyLiveTransition(state, kind, entry, eventName, payload, reducedMotion, nowMs) {
    if (!entry || !sceneEntryListensToEvent(entry, eventName)) {
      return false;
    }
    const target = sceneNormalizeEntryByKind(kind, payload, entry);
    const instantChanged = sceneApplyInstantLiveBufferPatch(kind, entry, target, payload);
    if (sceneTransitionValuesEqual(entry, target)) {
      return instantChanged;
    }
    const timing = sceneTransitionTimingForPhase(entry, "update");
    sceneCancelEntryTransition(state, kind, entry);
    const current = sceneCloneTransitionData(kind, entry);
    const transitionTarget = sceneCloneTransitionData(kind, target);
    if (reducedMotion || timing.duration <= 0) {
      sceneApplyTransitionPatch(entry, transitionTarget);
      return true;
    }
    const delta = sceneTransitionBuildDelta(current, transitionTarget, "");
    if (!delta) {
      return instantChanged;
    }
    sceneStateTransitions(state).push({
      key: sceneTransitionKey(kind, entry),
      entry,
      target: transitionTarget,
      delta,
      startTime: nowMs,
      duration: Math.max(1, timing.duration),
      easing: timing.easing,
    });
    return true;
  }

  function sceneApplyLiveEvent(state, eventName, payload, reducedMotion, nowMs) {
    const event = typeof eventName === "string" ? eventName.trim() : "";
    if (!event) {
      return false;
    }
    const rawPayload = sceneIsPlainObject(payload) ? payload : {};
    let changed = sceneApplyLiveTransition(state, "environment", state && state.environment, event, rawPayload, reducedMotion, nowMs);
    const collections = [
      ["object", sceneStateObjects(state)],
      ["label", sceneStateLabels(state)],
      ["sprite", sceneStateSprites(state)],
      ["html", sceneStateHTML(state)],
      ["light", sceneStateLights(state)],
      ["points", Array.isArray(state && state.points) ? state.points : []],
      ["instanced", Array.isArray(state && state.instancedMeshes) ? state.instancedMeshes : []],
      ["compute", Array.isArray(state && state.computeParticles) ? state.computeParticles : []],
    ];
    for (let ci = 0; ci < collections.length; ci += 1) {
      const kind = collections[ci][0];
      const entries = collections[ci][1];
      for (let i = 0; i < entries.length; i += 1) {
        changed = sceneApplyLiveTransition(state, kind, entries[i], event, rawPayload, reducedMotion, nowMs) || changed;
      }
    }
    return changed;
  }

  function sceneObjectAnimated(object) {
    if (!object || typeof object !== "object") {
      return false;
    }
    // Hidden helper geometry must not keep the shared mount scheduler awake.
    // This is especially important for water scenes, where an invisible
    // spinning placeholder previously masked the missing water animation reason.
    if (Object.prototype.hasOwnProperty.call(object, "visible") && !sceneBool(object.visible, true)) {
      return false;
    }
    if (sceneNumber(object.spinX, 0) !== 0 || sceneNumber(object.spinY, 0) !== 0 || sceneNumber(object.spinZ, 0) !== 0) {
      return true;
    }
    if (sceneNumber(object.driftSpeed, 0) === 0) {
      return false;
    }
    return sceneNumber(object.shiftX, 0) !== 0 || sceneNumber(object.shiftY, 0) !== 0 || sceneNumber(object.shiftZ, 0) !== 0;
  }

  function sceneLabelAnimated(label) {
    if (!label || typeof label !== "object") {
      return false;
    }
    if (sceneNumber(label.driftSpeed, 0) === 0) {
      return false;
    }
    return sceneNumber(label.shiftX, 0) !== 0 || sceneNumber(label.shiftY, 0) !== 0 || sceneNumber(label.shiftZ, 0) !== 0;
  }

  function sceneSpriteAnimated(sprite) {
    if (!sprite || typeof sprite !== "object") {
      return false;
    }
    if (sceneNumber(sprite.driftSpeed, 0) === 0) {
      return false;
    }
    return sceneNumber(sprite.shiftX, 0) !== 0 || sceneNumber(sprite.shiftY, 0) !== 0 || sceneNumber(sprite.shiftZ, 0) !== 0;
  }

  function sceneHTMLAnimated(entry) {
    if (!entry || typeof entry !== "object") {
      return false;
    }
    if (
      sceneNumber(entry.spinX, 0) !== 0 ||
      sceneNumber(entry.spinY, 0) !== 0 ||
      sceneNumber(entry.spinZ, 0) !== 0
    ) {
      return true;
    }
    if (sceneNumber(entry.driftSpeed, 0) === 0) {
      return false;
    }
    return sceneNumber(entry.shiftX, 0) !== 0 || sceneNumber(entry.shiftY, 0) !== 0 || sceneNumber(entry.shiftZ, 0) !== 0;
  }

  const SCENE_CMD_CREATE_OBJECT = 0;
  const SCENE_CMD_REMOVE_OBJECT = 1;
  const SCENE_CMD_SET_TRANSFORM = 2;
  const SCENE_CMD_SET_MATERIAL = 3;
  const SCENE_CMD_SET_LIGHT = 4;
  const SCENE_CMD_SET_CAMERA = 5;
  const SCENE_CMD_SET_PARTICLES = 6;
  const SCENE_CMD_SET_POST_EFFECTS = 7;
  const SCENE_CMD_SET_INSTANCED_MESHES = 8;
  const SCENE_CMD_SET_MATERIALS = 9;
  const SCENE_CMD_SET_MODELS = 10;
  const SCENE_CMD_SET_INSTANCED_GLB_MESHES = 11;
  const SCENE_CMD_SET_ANIMATIONS = 12;
  const SCENE_CMD_SET_ENVIRONMENT = 13;
  // SCENE_CMD_SET_POST_UNIFORMS: non-destructive per-frame uniform patching
  // for named CustomPost passes (see applyScenePostUniformsCommand below).
  // NOTE: 11 is already SCENE_CMD_SET_INSTANCED_GLB_MESHES and the run
  // continues unbroken through 13 — this intentionally does NOT reuse 11
  // despite older planning notes assuming SET_MODELS(10) was still the last
  // command; every existing value here is load-bearing wire protocol and
  // must never be renumbered.
  const SCENE_CMD_SET_POST_UNIFORMS = 14;

  function applySceneCommands(state, commands) {
    if (!state || !Array.isArray(commands) || commands.length === 0) return;
    const pending = [];
    for (const command of commands) {
      const result = applySceneCommand(state, command);
      if (result && typeof result.then === "function") {
        pending.push(result);
      }
    }
    if (pending.length > 0) {
      return Promise.all(pending);
    }
    return null;
  }

  function applySceneCommand(state, command) {
    if (!command || typeof command !== "object") return;
    switch (command.kind) {
      case SCENE_CMD_CREATE_OBJECT:
        applySceneCreateCommand(state, command.objectId, command.data);
        return;
      case SCENE_CMD_REMOVE_OBJECT:
        state.objects.delete(sceneObjectKey(command.objectId));
        state.labels.delete(sceneObjectKey(command.objectId));
        state.sprites.delete(sceneObjectKey(command.objectId));
        state.html.delete(sceneObjectKey(command.objectId));
        state.lights.delete(sceneObjectKey(command.objectId));
        return;
      case SCENE_CMD_SET_TRANSFORM:
      case SCENE_CMD_SET_MATERIAL:
        applySceneObjectPatch(state, command.objectId, command.data);
        return;
      case SCENE_CMD_SET_CAMERA:
        state.camera = normalizeSceneCamera(command.data || {}, state.camera);
        return;
      case SCENE_CMD_SET_LIGHT:
        applySceneLightPatch(state, command.objectId, command.data);
        return;
      case SCENE_CMD_SET_PARTICLES:
        applySceneParticlesCommand(state, command.data);
        return;
      case SCENE_CMD_SET_POST_EFFECTS:
        applyScenePostEffectsCommand(state, command.data);
        return;
      case SCENE_CMD_SET_INSTANCED_MESHES:
        applySceneInstancedMeshesCommand(state, command.data);
        return;
      case SCENE_CMD_SET_MATERIALS:
        applySceneMaterialsCommand(state, command.data);
        return;
      case SCENE_CMD_SET_MODELS:
        return applySceneModelsCommand(state, command.data);
      case SCENE_CMD_SET_INSTANCED_GLB_MESHES:
        return applySceneInstancedGLBMeshesCommand(state, command.data);
      case SCENE_CMD_SET_ANIMATIONS:
        applySceneAnimationsCommand(state, command.data);
        return;
      case SCENE_CMD_SET_ENVIRONMENT:
        applySceneEnvironmentCommand(state, command.data);
        return;
      case SCENE_CMD_SET_POST_UNIFORMS:
        applyScenePostUniformsCommand(state, command.data);
        return;
      default:
        return;
    }
  }

  function applySceneMaterialsCommand(state, data) {
    if (!state) return;
    const payload = sceneIsPlainObject(data) ? data : {};
    const rawMaterials = Array.isArray(data)
      ? data
      : (Array.isArray(payload.materials) ? payload.materials : []);
    state._materialSource = rawMaterials.map(sceneCloneData);
    state.materials = sceneNormalizeMaterialList(state._materialSource, state.capability);
  }

  function applySceneEnvironmentCommand(state, data) {
    if (!state) return;
    const payload = sceneIsPlainObject(data) ? data : {};
    const rawEnvironment = sceneIsPlainObject(payload.environment) ? payload.environment : payload;
    state.environment = normalizeSceneEnvironment(rawEnvironment, state.environment);
  }

  function applySceneParticlesCommand(state, data) {
    if (!state) return;
    const payload = sceneIsPlainObject(data) ? data : {};
    const rawPoints = Array.isArray(data)
      ? data
      : (Array.isArray(payload.points) ? payload.points : []);
    const rawCompute = Array.isArray(payload.computeParticles) ? payload.computeParticles : [];
    const rawWater = Array.isArray(payload.waterSystems) ? payload.waterSystems : [];
    state.points = rawPoints.map(function(entry, index) {
      return normalizeScenePointsEntry(entry, index, null);
    });
    state.computeParticles = rawCompute.map(function(entry, index) {
      return normalizeSceneComputeParticlesEntry(entry, index, null);
    });
    const currentWaterByID = new Map();
    const currentWater = Array.isArray(state.waterSystems) ? state.waterSystems : [];
    const waterShaderSourceByID = state._waterShaderSourceByID instanceof Map ? state._waterShaderSourceByID : new Map();
    currentWater.forEach(function(entry, index) {
      if (!entry || typeof entry !== "object") return;
      const id = sceneWaterSystemID(entry, index);
      currentWaterByID.set(id, entry);
      sceneMergeWaterShaderSources(waterShaderSourceByID, entry, index);
    });
    state.waterSystems = rawWater.map(function(entry, index) {
      const id = sceneWaterSystemID(entry, index);
      const currentFallback = currentWaterByID.get(id) || currentWater[index] || null;
      const sourceFallback = waterShaderSourceByID.get(id) || null;
      const fallback = sourceFallback ? Object.assign({}, currentFallback || {}, sourceFallback) : currentFallback;
      const normalized = normalizeSceneWaterSystemEntry(entry, index, fallback);
      sceneMergeWaterShaderSources(waterShaderSourceByID, normalized, index);
      return normalized;
    });
    state._waterShaderSourceByID = waterShaderSourceByID;
    scenePublishWaterShaderSourceMap(state._waterShaderSourceByID);
  }

  function applySceneInstancedMeshesCommand(state, data) {
    if (!state) return;
    const payload = sceneIsPlainObject(data) ? data : {};
    const rawMeshes = Array.isArray(data)
      ? data
      : (Array.isArray(payload.instancedMeshes) ? payload.instancedMeshes : []);
    state.instancedMeshes = rawMeshes.map(function(entry, index) {
      return normalizeSceneInstancedMeshEntry(entry, index, null);
    });
  }

  function sceneRehydrateModelsAfterCommand(state) {
    if (!state || typeof hydrateSceneStateModels !== "function") {
      return null;
    }
    const promise = hydrateSceneStateModels(state, null);
    if (promise && typeof promise.then === "function") {
      state._modelHydrationPromise = promise;
      const clearCurrentPromise = function() {
        if (state._modelHydrationPromise === promise) {
          state._modelHydrationPromise = null;
        }
      };
      // Do not use an ignored finally() Promise here. When hydration rejects,
      // that derived Promise rejects too and can surface as unhandled even when
      // the command caller correctly awaits the original Promise.
      promise.then(clearCurrentPromise, clearCurrentPromise);
    }
    return promise;
  }

  function applySceneModelsCommand(state, data) {
    if (!state) return null;
    const payload = sceneIsPlainObject(data) ? data : {};
    const rawModels = Array.isArray(data)
      ? data
      : (Array.isArray(payload.models) ? payload.models : []);
    state.models = rawModels.map(function(entry, index) {
      return normalizeSceneModel(entry, index);
    }).filter(function(model) {
      return Boolean(model && model.src);
    });
    return sceneRehydrateModelsAfterCommand(state);
  }

  function applySceneInstancedGLBMeshesCommand(state, data) {
    if (!state) return null;
    const payload = sceneIsPlainObject(data) ? data : {};
    const rawMeshes = Array.isArray(data)
      ? data
      : (Array.isArray(payload.instancedGLBMeshes) ? payload.instancedGLBMeshes : []);
    state.instancedGLBMeshes = rawMeshes.map(function(entry, index) {
      return normalizeSceneInstancedGLBMeshEntry(entry, index, null);
    }).filter(function(entry) {
      return Boolean(entry && entry.src && Array.isArray(entry.instances) && entry.instances.length > 0);
    });
    return sceneRehydrateModelsAfterCommand(state);
  }

  function applySceneAnimationsCommand(state, data) {
    if (!state) return;
    const payload = sceneIsPlainObject(data) ? data : {};
    const rawAnimations = Array.isArray(data)
      ? data
      : (Array.isArray(payload.animations) ? payload.animations : []);
    state.animations = rawAnimations.map(function(entry, index) {
      return normalizeSceneAnimationClip(entry, index);
    });
  }

  function applyScenePostEffectsCommand(state, data) {
    if (!state) return;
    const payload = sceneIsPlainObject(data) ? data : {};
    const rawEffects = Array.isArray(data)
      ? data
      : (Array.isArray(payload.postEffects) ? payload.postEffects : []);
    const postEffects = [];
    for (let index = 0; index < rawEffects.length; index += 1) {
      const effect = normalizeScenePostEffect(rawEffects[index], index, null);
      if (effect) {
        postEffects.push(effect);
      }
    }
    state.postEffects = postEffects;
    state._adaptiveSourcePostEffects = postEffects.slice();
    state._deferredPostEffects = null;
    if (Object.prototype.hasOwnProperty.call(payload, "postFXMaxPixels")) {
      state.postFXMaxPixels = Math.max(0, Math.floor(sceneNumber(payload.postFXMaxPixels, 0)));
    }
  }

  function sceneNormalizePostDOMRegionBounds(raw) {
    if (!sceneIsPlainObject(raw)) return null;
    const mode = typeof raw.mode === "string" ? raw.mode.trim().toLowerCase() : "";
    if (mode !== "union") return null;
    const left = Math.max(0, Math.min(1, sceneNumber(raw.left, 0)));
    const top = Math.max(0, Math.min(1, sceneNumber(raw.top, 0)));
    const right = Math.max(0, Math.min(1, sceneNumber(raw.right, 0)));
    const bottom = Math.max(0, Math.min(1, sceneNumber(raw.bottom, 0)));
    return {
      mode: "union",
      active: raw.active === true,
      left: Math.min(left, right),
      top: Math.min(top, bottom),
      right: Math.max(left, right),
      bottom: Math.max(top, bottom),
      paddingPx: Math.max(0, Math.min(2048, sceneNumber(raw.paddingPx, 0))),
    };
  }

  function scenePostDOMRegionPixelBounds(effect, width, height) {
    const raw = effect && effect._domRegionBounds;
    if (!raw || raw.mode !== "union") return { mode: "off", bounds: null };
    if (raw.active !== true) return { mode: "union", bounds: null };
    const w = Math.max(1, Math.floor(sceneNumber(width, 1)));
    const h = Math.max(1, Math.floor(sceneNumber(height, 1)));
    const left = Math.max(0, Math.min(w, Math.floor(sceneNumber(raw.left, 0) * w)));
    const top = Math.max(0, Math.min(h, Math.floor(sceneNumber(raw.top, 0) * h)));
    const right = Math.max(0, Math.min(w, Math.ceil(sceneNumber(raw.right, 0) * w)));
    const bottom = Math.max(0, Math.min(h, Math.ceil(sceneNumber(raw.bottom, 0) * h)));
    const boundsWidth = right - left;
    const boundsHeight = bottom - top;
    if (boundsWidth <= 0 || boundsHeight <= 0) return { mode: "union", bounds: null };
    return { mode: "union", bounds: { x: left, y: top, width: boundsWidth, height: boundsHeight } };
  }

  // applyScenePostUniformsCommand (SCENE_CMD_SET_POST_UNIFORMS): patches
  // per-frame uniform overrides onto already-installed named CustomPost
  // passes WITHOUT rebuilding the post chain. This exists precisely because
  // SCENE_CMD_SET_POST_EFFECTS (applyScenePostEffectsCommand) replaces
  // state.postEffects wholesale — fine for built-in effects, but destructive
  // to a compiled custom pass's shader/pipeline identity (fragmentWGSL/
  // vertexWGSL/shaderLayout etc. all get re-normalized from scratch, and any
  // cached pipeline keyed by those fields has to recompile). A pure uniform
  // tweak (e.g. an app animating a shader param on a signal) should never
  // pay that cost or risk it.
  //
  // Payload shape: { effects: [{ name: "<CustomPost.Name>", uniforms: { key: value, ... } }] }
  //
  // For each entry, every state.postEffects item whose .name matches is
  // found and ONLY its .uniforms map is shallow-merged with the patch — the
  // pass object itself, its position in the array, and every other field
  // (fragmentWGSL/vertexWGSL/shaderBackend/shaderLayout/stage/...) are left
  // completely untouched. No pass is added, removed, or reordered.
  //
  // Uniform propagation needs no separate "mark dirty" step: both render
  // backends already re-read effect.uniforms and re-upload it fresh every
  // frame with no value-equality caching — ensureCustomPostUniformBuffer's
  // per-name GPUBuffer in 16a-scene-webgpu.js only caches the BUFFER
  // (reallocated on size growth) and unconditionally calls
  // device.queue.writeBuffer with the current uniforms on every call;
  // applyCustomPost's uniform upload loop in 16-scene-webgl.js reads
  // effect.uniforms directly every call. Mutating the SAME uniforms object
  // reference (or swapping it, since callers only ever read it) is
  // sufficient for a patch to reach the GPU next frame on both backends.
  //
  // Stats: bumps state.postUniformPatches once per matched pass, and
  // state.postUniformPatchMisses once per entry whose name matched nothing
  // — published as the post-uniform-patches / post-uniform-patch-misses
  // mount attributes by applyScenePostFXState in 20-scene-mount.js.
  function applyScenePostUniformsCommand(state, data) {
    if (!state) return;
    state.postUniformPatches = Math.max(0, Math.floor(sceneNumber(state.postUniformPatches, 0)));
    state.postUniformPatchMisses = Math.max(0, Math.floor(sceneNumber(state.postUniformPatchMisses, 0)));
    const payload = sceneIsPlainObject(data) ? data : {};
    const rawEntries = Array.isArray(data)
      ? data
      : (Array.isArray(payload.effects) ? payload.effects : []);
    const postEffects = Array.isArray(state.postEffects) ? state.postEffects : [];
    for (let index = 0; index < rawEntries.length; index += 1) {
      const entry = rawEntries[index];
      if (!sceneIsPlainObject(entry)) continue;
      const name = typeof entry.name === "string" ? entry.name.trim() : "";
      const patch = sceneIsPlainObject(entry.uniforms) ? entry.uniforms : null;
      const ownsBounds = Object.prototype.hasOwnProperty.call(entry, "domRegionBounds");
      const bounds = ownsBounds && entry.domRegionBounds !== null
        ? sceneNormalizePostDOMRegionBounds(entry.domRegionBounds)
        : null;
      if (!name || (!patch && !ownsBounds)) continue;
      let matched = 0;
      for (let i = 0; i < postEffects.length; i += 1) {
        const pass = postEffects[i];
        if (!pass || typeof pass.name !== "string" || pass.name !== name) continue;
        if (patch) pass.uniforms = Object.assign({}, pass.uniforms, patch);
        if (ownsBounds) {
          if (bounds) pass._domRegionBounds = bounds;
          else delete pass._domRegionBounds;
        }
        matched += 1;
      }
      if (matched > 0) {
        state.postUniformPatches += matched;
      } else {
        state.postUniformPatchMisses += 1;
      }
    }
  }

  function applySceneCreateCommand(state, objectID, payload) {
    if (!payload || typeof payload !== "object") return;
    if (payload.kind === "camera") {
      state.camera = normalizeSceneCamera(payload.props || {}, state.camera);
      return;
    }
    if (payload.kind === "light") {
      const light = sceneLightFromPayload(objectID, payload, state.lights.get(sceneObjectKey(objectID)));
      if (light) {
        state.lights.set(sceneObjectKey(objectID), light);
      }
      return;
    }
    if (payload.kind === "particles") {
      return;
    }
    if (payload.kind === "label") {
      const label = sceneLabelFromPayload(objectID, payload, state.labels.get(sceneObjectKey(objectID)));
      if (label) {
        state.labels.set(sceneObjectKey(objectID), label);
      }
      return;
    }
    if (payload.kind === "sprite") {
      const sprite = sceneSpriteFromPayload(objectID, payload, state.sprites.get(sceneObjectKey(objectID)));
      if (sprite) {
        state.sprites.set(sceneObjectKey(objectID), sprite);
      }
      return;
    }
    if (payload.kind === "html") {
      const entry = sceneHTMLFromPayload(objectID, payload, state.html.get(sceneObjectKey(objectID)));
      if (entry) {
        state.html.set(sceneObjectKey(objectID), entry);
      }
      return;
    }
    const key = sceneObjectKey(objectID);
    const next = sceneObjectFromPayload(objectID, payload, state.objects.get(key));
    if (next) {
      state.objects.set(key, next);
    }
  }

  // sceneGizmoTargetAnchor resolves the world-space anchor point (and, when
  // meaningful, rotation) that 20-scene-mount.js's syncMountedSceneGizmoHelpers
  // repositions the TransformControls gizmo helper group onto for a given
  // selected object. Most declaratively-authored objects carry their
  // transform in x/y/z/rotationX../rotationZ, which is directly the anchor.
  // Some callers (e.g. kiln's mesh objects — see editor_viewport.go's
  // sceneMeshNodes, which lowers BufferGeometry with world-baked vertex
  // positions) instead ship world-baked vertex data (object.vertices.positions)
  // with x/y/z left at 0 — reading x/y/z there would always anchor the gizmo
  // at the origin regardless of the object's real position. Detect that case
  // and fall back to the vertex bounding-box center instead.
  function sceneGizmoTargetAnchor(obj) {
    const vertices = obj && obj.vertices;
    const positions = vertices && vertices.positions;
    if (positions && positions.length >= 3) {
      let minX = Infinity, minY = Infinity, minZ = Infinity;
      let maxX = -Infinity, maxY = -Infinity, maxZ = -Infinity;
      for (let i = 0; i + 2 < positions.length; i += 3) {
        const x = positions[i], y = positions[i + 1], z = positions[i + 2];
        if (x < minX) minX = x;
        if (x > maxX) maxX = x;
        if (y < minY) minY = y;
        if (y > maxY) maxY = y;
        if (z < minZ) minZ = z;
        if (z > maxZ) maxZ = z;
      }
      // World-baked vertex data already has any rotation applied, so there's
      // no separate rotation left to extract — identity is the honest answer
      // here (matches the object's own rotationX/Y/Z, which callers baking
      // vertices this way leave at 0 for the same reason).
      return {
        x: (minX + maxX) / 2,
        y: (minY + maxY) / 2,
        z: (minZ + maxZ) / 2,
        rotationX: 0,
        rotationY: 0,
        rotationZ: 0,
      };
    }
    return {
      x: sceneNumber(obj && obj.x, 0),
      y: sceneNumber(obj && obj.y, 0),
      z: sceneNumber(obj && obj.z, 0),
      rotationX: sceneNumber(obj && obj.rotationX, 0),
      rotationY: sceneNumber(obj && obj.rotationY, 0),
      rotationZ: sceneNumber(obj && obj.rotationZ, 0),
    };
  }

  function applySceneObjectPatch(state, objectID, patch) {
    const key = sceneObjectKey(objectID);
    const current = state.objects.get(key);
    if (current) {
      const next = sceneObjectFromPayload(objectID, {
        geometry: current.kind,
        props: patch || {},
      }, current);
      if (next) {
        state.objects.set(key, next);
      }
      return;
    }
    const currentLabel = state.labels.get(key);
    if (currentLabel) {
      const nextLabel = sceneLabelFromPayload(objectID, {
        props: Object.assign({}, currentLabel, patch || {}),
      }, currentLabel);
      if (nextLabel) {
        state.labels.set(key, nextLabel);
      }
      return;
    }
    const currentSprite = state.sprites.get(key);
    if (currentSprite) {
      const nextSprite = sceneSpriteFromPayload(objectID, {
        props: Object.assign({}, currentSprite, patch || {}),
      }, currentSprite);
      if (nextSprite) {
        state.sprites.set(key, nextSprite);
      }
      return;
    }
    const currentHTML = state.html.get(key);
    if (currentHTML) {
      const nextHTML = sceneHTMLFromPayload(objectID, {
        props: Object.assign({}, currentHTML, patch || {}),
      }, currentHTML);
      if (nextHTML) {
        state.html.set(key, nextHTML);
      }
    }
  }

  function sceneObjectKey(objectID) {
    return String(objectID);
  }

  function sceneObjectFromPayload(objectID, payload, fallback) {
    const current = fallback && typeof fallback === "object" ? fallback : {};
    const props = payload && payload.props && typeof payload.props === "object" ? payload.props : {};
    const geometry = payload && typeof payload.geometry === "string" && payload.geometry ? payload.geometry : current.kind;
    const merged = Object.assign({}, current, props);
    merged.id = current.id || merged.id || ("scene-object-" + objectID);
    merged.kind = normalizeSceneKind(merged.kind || geometry);
    // Route provenance: a route value authored in the incoming props must
    // not inherit a stale derived marker merged in from the current object
    // (otherwise an explicit route equal to the derived text stays wrongly
    // marked as computed). Props that already carry their own derived
    // metadata keep it; unaffected fields retain the current markers so
    // defaults keep re-evaluating through the normal fallback.
    const passSource = sceneRoutedValueSource(props, "renderPass", "material");
    if (passSource && passSource._renderPassDerived !== true) {
      merged._renderPassDerived = false;
    }
    const blendSource = sceneRoutedValueSource(props, "blendMode", "blendAlias");
    if (blendSource && blendSource._blendModeDerived !== true) {
      merged._blendModeDerived = false;
    }
    return normalizeSceneObject(merged, objectID, current);
  }

  function sceneLabelFromPayload(objectID, payload, fallback) {
    const current = fallback && typeof fallback === "object" ? fallback : {};
    const props = payload && payload.props && typeof payload.props === "object" ? payload.props : {};
    const merged = Object.assign({}, current, props);
    merged.id = current.id || merged.id || ("scene-label-" + objectID);
    const label = normalizeSceneLabel(merged, objectID, current);
    if (!label.text.trim()) {
      return null;
    }
    return label;
  }

  function sceneSpriteFromPayload(objectID, payload, fallback) {
    const current = fallback && typeof fallback === "object" ? fallback : {};
    const props = payload && payload.props && typeof payload.props === "object" ? payload.props : {};
    const merged = Object.assign({}, current, props);
    merged.id = current.id || merged.id || ("scene-sprite-" + objectID);
    const sprite = normalizeSceneSprite(merged, objectID, current);
    if (!sprite.src) {
      return null;
    }
    return sprite;
  }

  function sceneHTMLFromPayload(objectID, payload, fallback) {
    const current = fallback && typeof fallback === "object" ? fallback : {};
    const props = payload && payload.props && typeof payload.props === "object" ? payload.props : {};
    const merged = Object.assign({}, current, props);
    merged.id = current.id || merged.id || ("scene-html-" + objectID);
    const entry = normalizeSceneHTML(merged, objectID, current);
    if (!entry.html.trim()) {
      return null;
    }
    return entry;
  }

  function applySceneLightPatch(state, objectID, patch) {
    const key = sceneObjectKey(objectID);
    const current = state.lights.get(key);
    if (!current) {
      return;
    }
    const next = normalizeSceneLight(Object.assign({}, current, patch || {}), objectID, current);
    if (next) {
      state.lights.set(key, next);
    }
  }

  function sceneLightFromPayload(objectID, payload, fallback) {
    const current = fallback && typeof fallback === "object" ? fallback : {};
    const props = payload && payload.props && typeof payload.props === "object" ? payload.props : {};
    const merged = Object.assign({}, current, props);
    merged.id = current.id || merged.id || ("scene-light-" + objectID);
    return normalizeSceneLight(merged, objectID, current);
  }

  // sceneRenderCamera normalizes a raw camera (partial, may be missing
  // fields) into a full PBR camera struct. Hot callers that want to avoid
  // the per-call allocation can pass an `out` scratch they own — the
  // function writes into it and returns it. Callers that don't care (or
  // need a fresh result for lifetime reasons) omit the second argument
  // and get a newly allocated object.
  //
  // The PBR render path (createScenePBRRenderer.render) uses the out-param
  // form with a renderer-scoped _frameCam scratch so each frame reuses
  // the same object in place — confirmed safe because no code path
  // between render() entry and the drawPBRObjectList/drawInstancedMeshes
  // reads calls sceneRenderCamera with a DIFFERENT camera object that
  // would clobber the scratch.
  function sceneRenderCamera(camera, out) {
    const target = out || {
      kind: "perspective",
      x: 0, y: 0, z: 0,
      rotationX: 0, rotationY: 0, rotationZ: 0,
      fov: 0,
      left: 0, right: 0, top: 0, bottom: 0, zoom: 1,
      near: 0, far: 0,
    };
    target.kind = normalizeSceneCameraKind(camera && camera.kind, "perspective");
    target.x = sceneNumber(camera && camera.x, 0);
    target.y = sceneNumber(camera && camera.y, 0);
    target.z = sceneNumber(camera && camera.z, 6);
    target.rotationX = sceneNumber(camera && camera.rotationX, 0);
    target.rotationY = sceneNumber(camera && camera.rotationY, 0);
    target.rotationZ = sceneNumber(camera && camera.rotationZ, 0);
    target.fov = sceneNumber(camera && camera.fov, 75);
    target.left = sceneNumber(camera && camera.left, 0);
    target.right = sceneNumber(camera && camera.right, 0);
    target.top = sceneNumber(camera && camera.top, 0);
    target.bottom = sceneNumber(camera && camera.bottom, 0);
    target.zoom = Math.max(0.0001, sceneNumber(camera && camera.zoom, 1));
    target.near = sceneNumber(camera && camera.near, 0.05);
    target.far = sceneNumber(camera && camera.far, 128);
    return target;
  }

  function sceneOrthographicBounds(camera, width, height) {
    const cam = sceneRenderCamera(camera);
    const aspect = Math.max(0.0001, sceneNumber(width, 1) / Math.max(1, sceneNumber(height, 1)));
    let left = sceneNumber(cam.left, 0);
    let right = sceneNumber(cam.right, 0);
    let top = sceneNumber(cam.top, 0);
    let bottom = sceneNumber(cam.bottom, 0);
    if (Math.abs(right - left) <= 0.000001 || Math.abs(top - bottom) <= 0.000001) {
      const halfHeight = 3;
      const halfWidth = halfHeight * aspect;
      left = -halfWidth;
      right = halfWidth;
      top = halfHeight;
      bottom = -halfHeight;
    }
    const zoom = Math.max(0.0001, sceneNumber(cam.zoom, 1));
    const centerX = (left + right) * 0.5;
    const centerY = (top + bottom) * 0.5;
    const halfWidth = Math.max(0.000001, Math.abs(right - left) * 0.5 / zoom);
    const halfHeight = Math.max(0.000001, Math.abs(top - bottom) * 0.5 / zoom);
    return {
      left: centerX - halfWidth,
      right: centerX + halfWidth,
      top: centerY + halfHeight,
      bottom: centerY - halfHeight,
    };
  }

  function sceneCameraEquivalent(left, right) {
    const a = sceneRenderCamera(left);
    const b = sceneRenderCamera(right);
    return a.kind === b.kind &&
      Math.abs(a.x - b.x) <= 0.0001 &&
      Math.abs(a.y - b.y) <= 0.0001 &&
      Math.abs(a.z - b.z) <= 0.0001 &&
      Math.abs(a.rotationX - b.rotationX) <= 0.0001 &&
      Math.abs(a.rotationY - b.rotationY) <= 0.0001 &&
      Math.abs(a.rotationZ - b.rotationZ) <= 0.0001 &&
      Math.abs(a.fov - b.fov) <= 0.0001 &&
      Math.abs(a.left - b.left) <= 0.0001 &&
      Math.abs(a.right - b.right) <= 0.0001 &&
      Math.abs(a.top - b.top) <= 0.0001 &&
      Math.abs(a.bottom - b.bottom) <= 0.0001 &&
      Math.abs(a.zoom - b.zoom) <= 0.0001 &&
      Math.abs(a.near - b.near) <= 0.0001 &&
      Math.abs(a.far - b.far) <= 0.0001;
  }



  // Dedicated camera scratch for sceneBoundsDepthMetrics. Separate from
  // _sceneProjectCameraScratch in 11-scene-math.js so back-to-back calls
  // from the bundle builder (projection + bounds) don't clobber each
  // other even though both are single-threaded.
  const _sceneBoundsDepthCameraScratch = {
    kind: "perspective",
    x: 0, y: 0, z: 0,
    rotationX: 0, rotationY: 0, rotationZ: 0,
    left: 0, right: 0, top: 0, bottom: 0, zoom: 1,
    fov: 0, near: 0, far: 0,
  };

  // sceneBoundsDepthMetrics inlines the 8-corner depth computation. It uses
  // the same camera eye convention as scenePBRViewMatrix: camera.z is a world
  // eye coordinate, and forward depth is -viewZ after inverse rotation.
  //
  // The inverse rotation math matches sceneInverseRotatePoint's rotation
  // order. The final sign matches the renderer's positive forward depth.
  function sceneBoundsDepthMetrics(bounds, camera, cacheOwner) {
    if (!bounds) {
      const depth = sceneWorldPointDepth(0, camera);
      return { near: depth, far: depth, center: depth };
    }
    const cam = sceneRenderCamera(camera, _sceneBoundsDepthCameraScratch);

    // Optional per-object cache. appendSceneObjectToBundle calls this
    // function twice per frame per object (once for depth, once via
    // sceneBoundsViewCulled), and across frames the inputs rarely
    // change on a static scene — so the second call and all subsequent
    // frames can reuse a stored result.
    //
    // Change detection uses the sum of bounds extents + camera position
    // and rotation. Any real edit moves at least one term; numerical
    // coincidences that sum to the same value without actually matching
    // are possible in theory but statistically irrelevant for real
    // world coordinates (and invisible to the viewer if they do hit).
    //
    // Worst case on a miss: same cost as before. Best case (static
    // scene): saves the 30 Math.sin/cos + 8 iterations of matrix math
    // per object per frame. For a 100-object scene that's ~0.3-0.5 ms
    // per frame reclaimed, plus a second-call hit on every frame.
    let cacheHash = 0;
    if (cacheOwner) {
      cacheHash = sceneNumber(bounds.minX, 0) + sceneNumber(bounds.minY, 0) + sceneNumber(bounds.minZ, 0)
        + sceneNumber(bounds.maxX, 0) + sceneNumber(bounds.maxY, 0) + sceneNumber(bounds.maxZ, 0)
        + cam.x + cam.y + cam.z
        + cam.rotationX + cam.rotationY + cam.rotationZ
        + (cam.kind === "orthographic" ? 17 : 0)
        + cam.left + cam.right + cam.top + cam.bottom + cam.zoom;
      if (cacheOwner._depthCacheHash === cacheHash && cacheOwner._depthCacheResult) {
        return cacheOwner._depthCacheResult;
      }
    }

    const sinX = Math.sin(-cam.rotationX);
    const cosX = Math.cos(-cam.rotationX);
    const sinY = Math.sin(-cam.rotationY);
    const cosY = Math.cos(-cam.rotationY);
    const sinZ = Math.sin(-cam.rotationZ);
    const cosZ = Math.cos(-cam.rotationZ);

    const minX = sceneNumber(bounds.minX, 0);
    const minY = sceneNumber(bounds.minY, 0);
    const minZ = sceneNumber(bounds.minZ, 0);
    const maxX = sceneNumber(bounds.maxX, 0);
    const maxY = sceneNumber(bounds.maxY, 0);
    const maxZ = sceneNumber(bounds.maxZ, 0);

    let near = Infinity;
    let far = -Infinity;

    // Iterate the 8 bounding-box corners by bit-coding (i & 1, i & 2, i & 4).
    for (let i = 0; i < 8; i += 1) {
      const worldX = (i & 4) ? maxX : minX;
      const worldY = (i & 2) ? maxY : minY;
      const worldZ = (i & 1) ? maxZ : minZ;

      // Translate into view space before inverse rotation. This matches
      // scenePBRViewMatrix's translation(-cam.x, -cam.y, -cam.z).
      let lx = worldX - cam.x;
      let ly = worldY - cam.y;
      let lz = worldZ - cam.z;

      // Inverse rotate: apply -rotZ, then -rotY, then -rotX in that order.
      let nX = lx * cosZ - ly * sinZ;
      let nY = lx * sinZ + ly * cosZ;
      lx = nX;
      ly = nY;

      nX = lx * cosY + lz * sinY;
      let nZ = -lx * sinY + lz * cosY;
      lx = nX;
      lz = nZ;

      // Only lz matters for depth metrics — ly/lx discarded.
      nZ = ly * sinX + lz * cosX;
      lz = nZ;

      const depth = -lz;
      if (depth < near) near = depth;
      if (depth > far) far = depth;
    }

    const result = {
      near: near,
      far: far,
      center: (near + far) / 2,
    };
    if (cacheOwner) {
      cacheOwner._depthCacheHash = cacheHash;
      cacheOwner._depthCacheResult = result;
    }
    return result;
  }

  function sceneBoundsViewCulled(bounds, camera, cacheOwner) {
    if (!bounds) {
      return false;
    }
    const depth = sceneBoundsDepthMetrics(bounds, camera, cacheOwner);
    const near = sceneNumber(camera && camera.near, 0.05);
    const far = sceneNumber(camera && camera.far, 128);
    return depth.far <= near || depth.near >= far;
  }

  function sceneLODDistance(object, camera) {
    const dx = sceneNumber(object && object.x, 0) - sceneNumber(camera && camera.x, 0);
    const dy = sceneNumber(object && object.y, 0) - sceneNumber(camera && camera.y, 0);
    const dz = sceneNumber(object && object.z, 0) - sceneNumber(camera && camera.z, 0);
    return Math.sqrt(dx * dx + dy * dy + dz * dz);
  }

  function sceneLODLevelActive(entries, distance) {
    let best = null;
    let bestMin = -1;
    for (const entry of entries) {
      const minDistance = Math.max(0, sceneNumber(entry && entry.lodMinDistance, 0));
      const maxDistance = Math.max(0, sceneNumber(entry && entry.lodMaxDistance, 0));
      if (distance + 0.0001 < minDistance) {
        continue;
      }
      if (maxDistance > 0 && distance >= maxDistance) {
        continue;
      }
      if (minDistance >= bestMin) {
        best = entry;
        bestMin = minDistance;
      }
    }
    return best;
  }

  function sceneSelectLODObjects(objects, camera) {
    const source = Array.isArray(objects) ? objects : [];
    if (!source.length) {
      return source;
    }
    const plain = [];
    const groups = new Map();
    for (const object of source) {
      const group = object && typeof object.lodGroup === "string" ? object.lodGroup.trim() : "";
      if (!group) {
        plain.push(object);
        continue;
      }
      let levels = groups.get(group);
      if (!levels) {
        levels = new Map();
        groups.set(group, levels);
      }
      const level = Math.max(0, Math.floor(sceneNumber(object && object.lodLevel, 0)));
      let entries = levels.get(level);
      if (!entries) {
        entries = [];
        levels.set(level, entries);
      }
      entries.push(object);
    }
    if (!groups.size) {
      return source;
    }
    const selected = plain.slice();
    for (const levels of groups.values()) {
      const candidates = [];
      for (const entries of levels.values()) {
        if (entries.length) {
          candidates.push(entries[0]);
        }
      }
      const distance = sceneLODDistance(candidates[0], camera);
      const active = sceneLODLevelActive(candidates, distance) || candidates[0];
      const activeLevel = Math.max(0, Math.floor(sceneNumber(active && active.lodLevel, 0)));
      const activeEntries = levels.get(activeLevel);
      if (activeEntries && activeEntries.length) {
        selected.push.apply(selected, activeEntries);
      }
    }
    return selected;
  }

  function createSceneRenderBundle(width, height, background, camera, objects, labels, sprites, html, lights, environment, timeSeconds, points, instancedMeshes, computeParticles, waterSystems, postEffects, postFXMaxPixels, showDebugGrid, rendererCapabilities) {
    const bundleBuildStartedAt = typeof performance !== "undefined" && typeof performance.now === "function"
      ? performance.now()
      : Date.now();
    const resolvedEnvironment = sceneResolveLightingEnvironment(environment, Array.isArray(lights) && lights.length > 0);
    const renderCamera = sceneRenderCamera(camera);
    const bundle = {
      bundleVersion: 1,
      background: background,
      timeSeconds: sceneNumber(timeSeconds, 0),
      camera: renderCamera,
      lights: Array.isArray(lights) ? lights.slice() : [],
      environment: resolvedEnvironment,
      postEffects: Array.isArray(postEffects) ? postEffects.slice() : [],
      materials: [],
      objects: [],
      surfaces: [],
      labels: [],
      sprites: [],
      html: [],
      lines: [],
      points: Array.isArray(points) ? points : [],
      instancedMeshes: [],
      computeParticles: Array.isArray(computeParticles) ? computeParticles : [],
      waterSystems: Array.isArray(waterSystems) ? waterSystems : [],
      positions: [],
      colors: [],
      worldPositions: [],
      worldColors: [],
      // One entry per world-projected line segment, in lockstep with
      // worldPositions (which holds two vertices = 6 floats per segment).
      // The canvas 2D world renderer reads bundle.worldLineWidths[segmentIndex]
      // to honor LinesGeometry.Width. Absent values fall back to the runtime
      // default (1.8px) at the read site.
      worldLineWidths: [],
      worldLineDashes: [],
      worldLineDashSizes: [],
      worldLineGapSizes: [],
      // Parallel to worldLineWidths: per-segment render-pass index mapping
      // to the draw plan's pass buckets (0=opaque, 1=alpha, 2=additive).
      // The WebGL thick-line path honors per-pass blend/depth state by
      // emitting a separate drawElements call per non-empty pass. Legacy
      // gl.LINES path ignores this field because its per-pass separation
      // already happens in the draw plan.
      worldLinePasses: [],
      meshObjects: [],
      worldMeshPositions: [],
      worldMeshColors: [],
      worldMeshNormals: [],
      worldMeshUVs: [],
      worldMeshTangents: [],
      vertexCount: 0,
      worldVertexCount: 0,
      worldMeshVertexCount: 0,
      retainedMeshObjectCount: 0,
      retainedMeshVertexCount: 0,
      worldBakedMeshObjectCount: 0,
      worldBakedMeshVertexCount: 0,
      objectCount: 0,
      // Fail closed: callers which do not identify a retained-capable
      // renderer receive a backend-neutral, fully baked bundle.
      retainedGeometryEnabled: Boolean(rendererCapabilities && rendererCapabilities.retainedGeometry === true),
      retainedGeometryTelemetry: {
        eligible: 0,
        retained: 0,
        fallback: 0,
      },
    };
    const normalizedPostFXMaxPixels = Math.max(0, Math.floor(sceneNumber(postFXMaxPixels, 0)));
    if (normalizedPostFXMaxPixels > 0) {
      bundle.postFXMaxPixels = normalizedPostFXMaxPixels;
    }
    const materialLookup = new Map();
    if (sceneBool(showDebugGrid, false)) {
      appendSceneGridToBundle(bundle, width, height);
    }
    for (const object of sceneSelectLODObjects(objects, renderCamera)) {
      appendSceneObjectToBundle(bundle, materialLookup, renderCamera, width, height, object, bundle.lights, resolvedEnvironment, timeSeconds);
    }
    for (const label of labels || []) {
      appendSceneLabelToBundle(bundle, camera, width, height, label, timeSeconds);
    }
    for (const sprite of sprites || []) {
      appendSceneSpriteToBundle(bundle, camera, width, height, sprite, timeSeconds);
    }
    for (const entry of html || []) {
      appendSceneHTMLToBundle(bundle, materialLookup, camera, width, height, entry, timeSeconds);
    }
    appendSceneInstancedMeshesToBundle(bundle, materialLookup, instancedMeshes);
    bundle.positions = new Float32Array(bundle.positions);
    bundle.colors = new Float32Array(bundle.colors);
    bundle.vertexCount = bundle.positions.length / 2;
    bundle.worldPositions = new Float32Array(bundle.worldPositions);
    bundle.worldColors = new Float32Array(bundle.worldColors);
    bundle.worldVertexCount = bundle.worldPositions.length / 3;
    bundle.worldLineWidths = new Float32Array(bundle.worldLineWidths);
    bundle.worldLinePasses = new Uint8Array(bundle.worldLinePasses);
    bundle.worldMeshPositions = new Float32Array(bundle.worldMeshPositions);
    bundle.worldMeshColors = new Float32Array(bundle.worldMeshColors);
    bundle.worldMeshNormals = new Float32Array(bundle.worldMeshNormals);
    bundle.worldMeshUVs = new Float32Array(bundle.worldMeshUVs);
    bundle.worldMeshTangents = new Float32Array(bundle.worldMeshTangents);
    bundle.worldMeshVertexCount = bundle.worldMeshPositions.length / 3;
    bundle.objectCount = bundle.objects.length;
    bundle.bundleBuildCPUms = Math.max(0, (
      (typeof performance !== "undefined" && typeof performance.now === "function" ? performance.now() : Date.now()) -
      bundleBuildStartedAt
    ));
    return bundle;
  }

  function appendSceneInstancedMeshesToBundle(bundle, materialLookup, instancedMeshes) {
    const meshes = Array.isArray(instancedMeshes) ? instancedMeshes : [];
    for (let index = 0; index < meshes.length; index += 1) {
      const mesh = meshes[index];
      if (!mesh || typeof mesh !== "object") {
        continue;
      }
      const material = sceneObjectMaterialProfile(mesh);
      const materialIndex = sceneBundleMaterialIndex(bundle, materialLookup, material);
      const entry = Object.assign({}, mesh, {
        materialIndex,
        materialKind: material.kind || mesh.materialKind,
        renderPass: material.renderPass || mesh.renderPass,
        _renderPassDerived: (mesh && mesh._renderPassDerived) === true,
      });
      bundle.instancedMeshes.push(entry);
    }
  }

  // translateScenePointInto is the alloc-free core of the scene-space
  // transform (scale → rotate → translate + drift offset). It writes the
  // result into a caller-provided `out` object, reads raw coordinates to
  // avoid an intermediate point object at the call site, and inlines the
  // rotation math so there's no sceneRotatePoint result allocation.
  //
  // Every caller in the tree uses a hoisted scratch `out` object (module-
  // level or above-loop), so translating a point at 60 fps costs zero
  // allocations after the initial scratch is wired up.
  function translateScenePointInto(out, px, py, pz, object, timeSeconds) {
    const scaleX = sceneNumber(object && object.scaleX, 1);
    const scaleY = sceneNumber(object && object.scaleY, 1);
    const scaleZ = sceneNumber(object && object.scaleZ, 1);
    let x = sceneNumber(px, 0) * scaleX;
    let y = sceneNumber(py, 0) * scaleY;
    let z = sceneNumber(pz, 0) * scaleZ;

    // Inlined XYZ Euler rotation (was sceneRotatePoint). Applies rotateX
    // then rotateY then rotateZ to match the original helper semantics.
    const rotX = object.rotationX + (object.spinX || 0) * timeSeconds;
    const rotY = object.rotationY + (object.spinY || 0) * timeSeconds;
    const rotZ = object.rotationZ + (object.spinZ || 0) * timeSeconds;

    const sinX = Math.sin(rotX);
    const cosX = Math.cos(rotX);
    let nextY = y * cosX - z * sinX;
    let nextZ = y * sinX + z * cosX;
    y = nextY;
    z = nextZ;

    const sinY = Math.sin(rotY);
    const cosY = Math.cos(rotY);
    let nextX = x * cosY + z * sinY;
    nextZ = -x * sinY + z * cosY;
    x = nextX;
    z = nextZ;

    const sinZ = Math.sin(rotZ);
    const cosZ = Math.cos(rotZ);
    nextX = x * cosZ - y * sinZ;
    nextY = x * sinZ + y * cosZ;
    x = nextX;
    y = nextY;

    // Inlined sceneMotionOffset. Short-circuits when no drift components
    // are set so static objects skip the sin/cos math entirely — the old
    // helper early-returned a fresh {0,0,0} per call, still allocating.
    if (object && (object.shiftX || object.shiftY || object.shiftZ)) {
      const driftPhase = sceneNumber(object.driftPhase, 0);
      const angle = driftPhase + timeSeconds * sceneNumber(object.driftSpeed, 0);
      x += Math.cos(angle) * sceneNumber(object.shiftX, 0);
      y += Math.sin(angle * 0.82 + driftPhase * 0.35) * sceneNumber(object.shiftY, 0);
      z += Math.sin(angle) * sceneNumber(object.shiftZ, 0);
    }

    x += object.x;
    y += object.y;
    z += object.z;
    const parentMatrix = object.parentMatrix;
    if (parentMatrix) {
      sceneMatrixTransformInto(out, parentMatrix, x, y, z, 4, true);
      return;
    }
    out.x = x;
    out.y = y;
    out.z = z;
  }

  // Module-level scratches used by the hot line-geometry and triangle-mesh
  // loops inside appendSceneObjectToBundle / appendSceneMeshObjectToBundle.
  // They live above the loops so each frame reuses the same objects in
  // place — no per-iteration allocations. Callers MUST NOT retain these
  // references across another translateScenePointInto call on the same
  // scratch; each iteration is expected to consume its scratch inline
  // before the next iteration's translate.
  const _lineSegmentFromScratch = { x: 0, y: 0, z: 0 };
  const _lineSegmentToScratch = { x: 0, y: 0, z: 0 };
	  const _meshTriangleP0Scratch = { x: 0, y: 0, z: 0 };
	  const _meshTriangleP1Scratch = { x: 0, y: 0, z: 0 };
	  const _meshTriangleP2Scratch = { x: 0, y: 0, z: 0 };
	  const _meshTrianglePoints = [_meshTriangleP0Scratch, _meshTriangleP1Scratch, _meshTriangleP2Scratch];
	  const _objectMatrixOriginScratch = { x: 0, y: 0, z: 0 };
	  const _objectMatrixXScratch = { x: 0, y: 0, z: 0 };
	  const _objectMatrixYScratch = { x: 0, y: 0, z: 0 };
	  const _objectMatrixZScratch = { x: 0, y: 0, z: 0 };
	  const _sceneObjectModelMatrixCache = new WeakMap();
	  const _sceneObjectMeshBakeLinearStateCache = new WeakMap();
	  const _sceneMeshLocalBoundsCache = new WeakMap();

	  function sceneObjectModelMatrix(object, timeSeconds) {
	    const origin = _objectMatrixOriginScratch;
	    const axisX = _objectMatrixXScratch;
	    const axisY = _objectMatrixYScratch;
	    const axisZ = _objectMatrixZScratch;
	    translateScenePointInto(origin, 0, 0, 0, object, timeSeconds);
	    translateScenePointInto(axisX, 1, 0, 0, object, timeSeconds);
	    translateScenePointInto(axisY, 0, 1, 0, object, timeSeconds);
	    translateScenePointInto(axisZ, 0, 0, 1, object, timeSeconds);

	    let out = object && typeof object === "object"
	      ? _sceneObjectModelMatrixCache.get(object)
	      : null;
	    if (!out) {
	      out = new Float32Array(16);
	      if (object && typeof object === "object") {
	        _sceneObjectModelMatrixCache.set(object, out);
	      }
	    }
	    out[0] = axisX.x - origin.x;
	    out[1] = axisX.y - origin.y;
	    out[2] = axisX.z - origin.z;
	    out[3] = 0;
	    out[4] = axisY.x - origin.x;
	    out[5] = axisY.y - origin.y;
	    out[6] = axisY.z - origin.z;
	    out[7] = 0;
	    out[8] = axisZ.x - origin.x;
	    out[9] = axisZ.y - origin.y;
	    out[10] = axisZ.z - origin.z;
	    out[11] = 0;
	    out[12] = origin.x;
	    out[13] = origin.y;
	    out[14] = origin.z;
	    out[15] = 1;
	    return out;
	  }

	  // Build the exact inverse-transpose normal transform and orientation for
	  // the world-baked mesh fallback. Entry 9 carries the determinant sign for
	  // triangle winding and tangent-frame handedness.
	  function sceneObjectMeshBakeLinearState(object, modelMatrix) {
	    let out = object && typeof object === "object"
	      ? _sceneObjectMeshBakeLinearStateCache.get(object)
	      : null;
	    if (!out) {
	      out = new Float64Array(10);
	      if (object && typeof object === "object") {
	        _sceneObjectMeshBakeLinearStateCache.set(object, out);
	      }
	    }

	    const determinant = sceneAffineDeterminant(modelMatrix, 0);
	    sceneAffineNormalMatrix(modelMatrix, out);
	    out[9] = determinant < 0 ? -1 : 1;
	    return out;
	  }

	  function sceneObjectTransformNormal(object, normal, timeSeconds) {
	    const transform = sceneObjectMeshBakeLinearState(object, sceneObjectModelMatrix(object, timeSeconds));
	    return sceneMatrixTransformInto({}, transform, normal.x, normal.y, normal.z, 3, false);
	  }

	  function sceneMeshGeometryRevision(object, vertices) {
	    if (object && Object.prototype.hasOwnProperty.call(object, "geometryRevision") && object.geometryRevision != null) {
	      const revision = Number(object.geometryRevision);
	      return Number.isFinite(revision) && revision >= 0 ? Math.floor(revision) : null;
	    }
	    if (vertices && Object.prototype.hasOwnProperty.call(vertices, "revision") && vertices.revision != null) {
	      const revision = Number(vertices.revision);
	      return Number.isFinite(revision) && revision >= 0 ? Math.floor(revision) : null;
	    }
	    return null;
	  }

	  function sceneMeshLocalBounds(vertices, revision) {
	    const positions = vertices && vertices.positions;
	    const count = Math.max(0, Math.floor(sceneNumber(vertices && vertices.count, 0)));
	    if (!positions || typeof positions.length !== "number" || count <= 0) {
	      return null;
	    }
	    const cached = _sceneMeshLocalBoundsCache.get(vertices);
	    if (
	      cached &&
	      cached.positions === positions &&
	      cached.count === count &&
	      cached.revision === revision
	    ) {
	      return cached.bounds;
	    }
	    let bounds = null;
	    const limit = Math.min(count * 3, positions.length);
	    for (let offset = 0; offset + 2 < limit; offset += 3) {
	      const x = sceneNumber(positions[offset], 0);
	      const y = sceneNumber(positions[offset + 1], 0);
	      const z = sceneNumber(positions[offset + 2], 0);
	      if (!bounds) {
	        bounds = { minX: x, minY: y, minZ: z, maxX: x, maxY: y, maxZ: z };
	      } else {
	        if (x < bounds.minX) bounds.minX = x;
	        if (y < bounds.minY) bounds.minY = y;
	        if (z < bounds.minZ) bounds.minZ = z;
	        if (x > bounds.maxX) bounds.maxX = x;
	        if (y > bounds.maxY) bounds.maxY = y;
	        if (z > bounds.maxZ) bounds.maxZ = z;
	      }
	    }
	    if (bounds) {
	      _sceneMeshLocalBoundsCache.set(vertices, {
	        positions,
	        count,
	        revision,
	        bounds,
	      });
	    }
	    return bounds;
	  }

	  function sceneTransformMeshBounds(localBounds, modelMatrix) {
	    if (!localBounds || !modelMatrix || modelMatrix.length < 16) {
	      return null;
	    }
	    let bounds = null;
	    const world = { x: 0, y: 0, z: 0 };
	    for (let corner = 0; corner < 8; corner += 1) {
	      const x = corner & 1 ? localBounds.maxX : localBounds.minX;
	      const y = corner & 2 ? localBounds.maxY : localBounds.minY;
	      const z = corner & 4 ? localBounds.maxZ : localBounds.minZ;
	      sceneMatrixTransformInto(world, modelMatrix, x, y, z, 4, true);
	      const wx = world.x, wy = world.y, wz = world.z;
	      if (!bounds) {
	        bounds = { minX: wx, minY: wy, minZ: wz, maxX: wx, maxY: wy, maxZ: wz };
	      } else {
	        if (wx < bounds.minX) bounds.minX = wx;
	        if (wy < bounds.minY) bounds.minY = wy;
	        if (wz < bounds.minZ) bounds.minZ = wz;
	        if (wx > bounds.maxX) bounds.maxX = wx;
	        if (wy > bounds.maxY) bounds.maxY = wy;
	        if (wz > bounds.maxZ) bounds.maxZ = wz;
	      }
	    }
	    return bounds;
	  }

  function appendSceneGridToBundle(bundle, width, height) {
    for (let x = 0; x <= width; x += 48) {
      appendSceneLine(bundle, width, height, { x: x, y: 0 }, { x: x, y: height }, "rgba(141, 225, 255, 0.14)", 1);
    }
    for (let y = 0; y <= height; y += 48) {
      appendSceneLine(bundle, width, height, { x: 0, y: y }, { x: width, y: y }, "rgba(141, 225, 255, 0.14)", 1);
    }
  }

  function appendSceneObjectToBundle(bundle, materialLookup, camera, width, height, object, lights, environment, timeSeconds) {
    if (sceneObjectHasTriangleMesh(object)) {
      appendSceneMeshObjectToBundle(bundle, materialLookup, camera, width, height, object, lights, environment, timeSeconds);
      return;
    }
    // Explicit visible:false always wins for the line/surface path too —
    // mirrors sceneMeshObjectEffectivelyInvisible's first check for the
    // triangle-mesh path above. Without this, line-kind objects (helpers,
    // TransformControls gizmo pieces, etc.) ignored `visible` entirely and
    // always rendered regardless of the flag (a latent gap the P7 live-
    // reactive gizmo helper feature depends on being fixed — see
    // syncMountedSceneGizmoHelpers in 20-scene-mount.js).
    if (object && object.visible === false) {
      return;
    }
    const sourceSegments = sceneObjectSegments(object);
    const vertexOffset = bundle.worldPositions.length / 3;
    const material = sceneObjectMaterialProfile(object);
    const materialIndex = sceneBundleMaterialIndex(bundle, materialLookup, material);
    const includeLineGeometry = sceneWorldObjectUsesLinePass(object, material);
    let bounds = null;
    let vertexCount = 0;
    if (includeLineGeometry) {
      // Two widths in play here:
      //   - rawLineWidth: 0 when the user didn't set LinesGeometry.Width at
      //     all, positive when they did. Stored as-is in bundle.worldLineWidths
      //     so the WebGL thick-line dispatch only activates on explicit
      //     non-default widths (sceneBundleNeedsThickLines checks > 1) and
      //     legacy wireframe objects keep using gl.LINES.
      //   - objectLineWidth: resolved render width used by appendSceneLine's
      //     per-line record for the Canvas 2D non-world path, which still
      //     expects a positive width value. Falls back to the legacy 1.8px
      //     default when rawLineWidth is zero.
      const rawLineWidth = sceneNumber(object && object.lineWidth, 0);
      const objectLineWidth = rawLineWidth > 0 ? rawLineWidth : 1.8;
      // Map the object's render pass to the 0/1/2 bucket the thick-line
      // draw path uses to group segments by pass. Computed once per object
      // so downstream per-segment pushes are a single integer assignment.
      const objectPassString = sceneWorldObjectRenderPass(object, material);
      const objectPassIndex = objectPassString === "alpha" ? 1 : (objectPassString === "additive" ? 2 : 0);
      // Hoist segment world-space scratches above the loop. Both `fromWorld`
      // and `toWorld` are stable within a single iteration because all
      // downstream consumers (sceneLitColorRGBA, sceneExpandWorldBounds,
      // sceneProjectPoint) read fields inline rather than retaining refs,
      // and appendSceneLine stores the sceneProjectPoint *output* (fresh
      // per call) not the world scratch. Pre-restructure this path built
      // an intermediate worldSegments array with fresh pair objects per
      // segment — 4 allocs per segment × N segments × 60 fps = a lot of
      // GC churn on any line-heavy scene.
      const fromWorld = _lineSegmentFromScratch;
      const toWorld = _lineSegmentToScratch;
      for (let index = 0; index < sourceSegments.length; index += 1) {
        const sourceSegment = sourceSegments[index];
        translateScenePointInto(fromWorld, sourceSegment[0] && sourceSegment[0].x, sourceSegment[0] && sourceSegment[0].y, sourceSegment[0] && sourceSegment[0].z, object, timeSeconds);
        translateScenePointInto(toWorld, sourceSegment[1] && sourceSegment[1].x, sourceSegment[1] && sourceSegment[1].y, sourceSegment[1] && sourceSegment[1].z, object, timeSeconds);
        const fromLighting = sceneLitColorRGBA(material, fromWorld, sceneObjectWorldNormal(object, sourceSegment[0], timeSeconds), lights, environment);
        const toLighting = sceneLitColorRGBA(material, toWorld, sceneObjectWorldNormal(object, sourceSegment[1], timeSeconds), lights, environment);
        bundle.worldPositions.push(fromWorld.x, fromWorld.y, fromWorld.z, toWorld.x, toWorld.y, toWorld.z);
        bundle.worldColors.push(
          fromLighting[0], fromLighting[1], fromLighting[2], fromLighting[3],
          toLighting[0], toLighting[1], toLighting[2], toLighting[3],
        );
        // Keep worldLineWidths in lockstep with each segment pushed into
        // worldPositions. Store rawLineWidth (zero when unset) so the
        // canvas 2D world renderer and the WebGL thick-line dispatch can
        // distinguish "default width" (0 → fall back at read time) from
        // "user explicitly asked for width N" (N > 0 → honor on both paths).
        bundle.worldLineWidths.push(rawLineWidth);
        bundle.worldLineDashes.push(Boolean(material && material.lineDash));
        bundle.worldLineDashSizes.push(sceneNumber(material && material.dashSize, 0));
        bundle.worldLineGapSizes.push(sceneNumber(material && material.gapSize, 0));
        bundle.worldLinePasses.push(objectPassIndex);
        bounds = sceneExpandWorldBounds(bounds, fromWorld);
        bounds = sceneExpandWorldBounds(bounds, toWorld);
        vertexCount += 2;
        const from = sceneProjectPoint(fromWorld, camera, width, height);
        const to = sceneProjectPoint(toWorld, camera, width, height);
        if (!from || !to) continue;
        const stroke = sceneMixRGBA(fromLighting, toLighting);
        stroke[3] = clamp01(stroke[3] * sceneMaterialOpacity(material));
        appendSceneLine(bundle, width, height, from, to, sceneRGBAString(stroke), objectLineWidth, {
          dashed: Boolean(material && material.lineDash),
          dashSize: sceneNumber(material && material.dashSize, 0),
          gapSize: sceneNumber(material && material.gapSize, 0),
        });
      }
    } else if (sceneObjectHasTexturedSurface(object, material)) {
      const corners = scenePlaneSurfaceCorners(object, timeSeconds);
      for (const corner of corners) {
        bounds = sceneExpandWorldBounds(bounds, corner);
      }
    }
    if (vertexCount > 0 || bounds) {
      const depth = sceneBoundsDepthMetrics(bounds, camera, object);
      bundle.objects.push({
        id: object.id,
        kind: object.kind,
        pickable: typeof object.pickable === "boolean" ? object.pickable : undefined,
        materialIndex: materialIndex,
        renderPass: sceneWorldObjectRenderPass(object, material),
        _renderPassDerived: (object && object._renderPassDerived) === true,
        vertexOffset: vertexOffset,
        vertexCount: vertexCount,
        static: Boolean(object.static),
        castShadow: Boolean(object.castShadow),
        receiveShadow: Boolean(object.receiveShadow),
        depthWrite: object.depthWrite,
        bounds: bounds || {
          minX: 0,
          minY: 0,
          minZ: 0,
          maxX: 0,
          maxY: 0,
          maxZ: 0,
        },
        depthNear: depth.near,
        depthFar: depth.far,
        depthCenter: depth.center,
        viewCulled: Boolean(object.viewCulled) || sceneBoundsViewCulled(bounds, camera, object),
      });
      appendSceneSurfaceToBundle(bundle, camera, object, materialIndex, material, bounds, depth, timeSeconds);
    }
  }

  function sceneObjectHasTriangleMesh(object) {
    return Boolean(
      object &&
      object.vertices &&
      object.vertices.positions &&
      typeof object.vertices.count === "number" &&
      object.vertices.count >= 3
    );
  }

  function sceneMeshVertexNormal(vertices, index) {
    const offset = index * 3;
    if (!vertices || !vertices.normals || vertices.normals.length < offset + 3) {
      return { x: 0, y: 1, z: 0 };
    }
    return {
      x: sceneNumber(vertices.normals[offset], 0),
      y: sceneNumber(vertices.normals[offset + 1], 1),
      z: sceneNumber(vertices.normals[offset + 2], 0),
    };
  }

  function sceneMeshVertexUV(vertices, index) {
    const offset = index * 2;
    if (!vertices || !vertices.uvs || vertices.uvs.length < offset + 2) {
      return { x: 0, y: 0 };
    }
    return {
      x: sceneNumber(vertices.uvs[offset], 0),
      y: sceneNumber(vertices.uvs[offset + 1], 0),
    };
  }

  function sceneMeshVertexTangent(vertices, index) {
    const offset = index * 4;
    if (!vertices || !vertices.tangents || vertices.tangents.length < offset + 4) {
      return { x: 1, y: 0, z: 0, w: 1 };
    }
    return {
      x: sceneNumber(vertices.tangents[offset], 1),
      y: sceneNumber(vertices.tangents[offset + 1], 0),
      z: sceneNumber(vertices.tangents[offset + 2], 0),
      w: sceneNumber(vertices.tangents[offset + 3], 1),
    };
  }

  function sceneMeshWorldNormal(vertices, index, normalTransform) {
    const normal = sceneMeshVertexNormal(vertices, index);
    return sceneNormalizeDirection(sceneMatrixTransformInto(
      normal, normalTransform, normal.x, normal.y, normal.z, 3, false,
    ));
  }

  function sceneMeshWorldTangent(vertices, index, modelMatrix, normal, orientation) {
    const tangent = sceneMeshVertexTangent(vertices, index);
    sceneMatrixTransformInto(tangent, modelMatrix, tangent.x, tangent.y, tangent.z, 4, false);
    let x = tangent.x, y = tangent.y, z = tangent.z;

    // A tangent is a surface direction, so it follows the ordinary linear
    // transform rather than the normal matrix. Remove any accumulated
    // non-orthogonality before the shader reconstructs B = cross(N, T) * w.
    const normalDot = x * normal.x + y * normal.y + z * normal.z;
    x -= normal.x * normalDot;
    y -= normal.y * normalDot;
    z -= normal.z * normalDot;
    let length = Math.sqrt(x * x + y * y + z * z);
    if (length <= 0.000001) {
      // Degenerate authored tangents or singular scales still need a finite
      // direction. Pick the least-aligned cardinal axis and cross it with N.
      if (Math.abs(normal.x) <= Math.abs(normal.y) && Math.abs(normal.x) <= Math.abs(normal.z)) {
        x = 0;
        y = -normal.z;
        z = normal.y;
      } else if (Math.abs(normal.y) <= Math.abs(normal.z)) {
        x = normal.z;
        y = 0;
        z = -normal.x;
      } else {
        x = -normal.y;
        y = normal.x;
        z = 0;
      }
      length = Math.max(0.000001, Math.sqrt(x * x + y * y + z * z));
    }
    return {
      x: x / length,
      y: y / length,
      z: z / length,
      w: tangent.w * orientation,
    };
  }

  function sceneNormalizeDirection(point) {
    const length = Math.sqrt(
      sceneNumber(point && point.x, 0) * sceneNumber(point && point.x, 0) +
      sceneNumber(point && point.y, 0) * sceneNumber(point && point.y, 0) +
      sceneNumber(point && point.z, 0) * sceneNumber(point && point.z, 0)
    );
    if (length <= 0.000001) {
      return { x: 0, y: 1, z: 0 };
    }
    return {
      x: sceneNumber(point && point.x, 0) / length,
      y: sceneNumber(point && point.y, 0) / length,
      z: sceneNumber(point && point.z, 0) / length,
    };
  }

  function appendSceneMeshWireSegment(bundle, camera, width, height, fromWorld, toWorld, fromLighting, toLighting, lineWidth, passIndex) {
    bundle.worldPositions.push(fromWorld.x, fromWorld.y, fromWorld.z, toWorld.x, toWorld.y, toWorld.z);
    bundle.worldColors.push(
      fromLighting[0], fromLighting[1], fromLighting[2], fromLighting[3],
      toLighting[0], toLighting[1], toLighting[2], toLighting[3],
    );
    bundle.worldLineWidths.push(lineWidth > 0 ? lineWidth : 0);
    bundle.worldLineDashes.push(false);
    bundle.worldLineDashSizes.push(0);
    bundle.worldLineGapSizes.push(0);
    bundle.worldLinePasses.push(passIndex || 0);
    const from = sceneProjectPoint(fromWorld, camera, width, height);
    const to = sceneProjectPoint(toWorld, camera, width, height);
    if (!from || !to) {
      return 2;
    }
    const stroke = sceneMixRGBA(fromLighting, toLighting);
    appendSceneLine(bundle, width, height, from, to, sceneRGBAString(stroke), lineWidth > 0 ? lineWidth : 1.6);
    return 2;
  }

  function sceneMeshObjectEffectivelyInvisible(object, material) {
    if (!object) {
      return false;
    }
    if (object.visible === false) {
      return true;
    }
    if (object.selected) {
      return false;
    }
    if (object._modelHidden) {
      return true;
    }
    if (material && sceneNumber(material.opacity, 1) <= 0.0001 &&
        !sceneMaterialUsesAuthoredMeshShader(material) &&
        !sceneMaterialHasEnabledNumericAlphaCutoff(material)) {
      return true;
    }
    const scaleX = Math.abs(sceneNumber(object.scaleX, sceneNumber(object.scale, 1)));
    const scaleY = Math.abs(sceneNumber(object.scaleY, sceneNumber(object.scale, 1)));
    const scaleZ = Math.abs(sceneNumber(object.scaleZ, sceneNumber(object.scale, 1)));
    return Math.max(scaleX, scaleY, scaleZ) <= 0.0015;
  }

  // sceneMaterialHasEnabledNumericAlphaCutoff reports whether the material
  // carries a normalized numeric alpha cutoff that is considered enabled:
  // finite numbers >= 0 qualify (0 included), while null, undefined, invalid
  // values and unresolved CSS var strings do not.
  function sceneMaterialHasEnabledNumericAlphaCutoff(material) {
    if (!material || typeof material !== "object") {
      return false;
    }
    const cutoff = sceneNormalizeMaterialAlphaCutoff(material.alphaCutoff, null);
    return typeof cutoff === "number" && Number.isFinite(cutoff);
  }

  function sceneMaterialUsesAuthoredMeshShader(material) {
    if (!material || typeof material !== "object") {
      return false;
    }
    if (String(material.shaderBackend || "").trim().toLowerCase() === "selena") {
      return true;
    }
    return Boolean(
      typeof material.customVertex === "string" && material.customVertex.trim() ||
      typeof material.customFragment === "string" && material.customFragment.trim() ||
      typeof material.customVertexWGSL === "string" && material.customVertexWGSL.trim() ||
      typeof material.customFragmentWGSL === "string" && material.customFragmentWGSL.trim() ||
      typeof material.shaderSource === "string" && material.shaderSource.trim()
    );
  }

  function sceneMaterialSuppressesGeneratedWireSegments(material) {
    return material && material.shaderBackend === "selena";
  }

  // A default selected mesh should keep looking like the material the author
  // chose. The historical fallback outlined selection by drawing every
  // triangle edge, which exposed cap fans and sphere tessellation as a dense
  // wire cage. Give ordinary PBR materials a restrained self-lit lift instead;
  // authors who explicitly set outlineColor or outlineWidth retain the legacy
  // edge overlay, and explicit wireframe materials remain wireframes.
  function sceneSelectedMaterialProfile(material, object, hasAuthoredOutline) {
    if (!material || !object || !object.selected || hasAuthoredOutline ||
        material.wireframe || sceneMaterialUsesAuthoredMeshShader(material)) {
      return material;
    }
    const currentEmissive = sceneCSSVarReference(material.emissive)
      ? 0
      : clamp01(sceneNumber(material.emissive, 0));
    const selected = Object.assign({}, material, {
      emissive: clamp01(currentEmissive + 0.08),
      shaderData: null,
    });
    selected.key = sceneMaterialProfileKey(selected);
    selected.shaderData = sceneMaterialShaderData(selected);
    return selected;
  }

  function sceneMeshCanRetainLocalGeometry(bundle, object, material, vertices, emitWireSegments) {
    const count = Math.max(0, Math.floor(sceneNumber(vertices && vertices.count, 0)));
    const scaleX = sceneNumber(object && object.scaleX, 1);
    const scaleY = sceneNumber(object && object.scaleY, 1);
    const scaleZ = sceneNumber(object && object.scaleZ, 1);
    function hasAttribute(name, tupleSize) {
      const data = vertices && vertices[name];
      return data instanceof Float32Array && data.length >= count * tupleSize;
    }
    return Boolean(
      count > 0 &&
      bundle && bundle.retainedGeometryEnabled === true &&
      vertices && vertices.immutable === true &&
      sceneMeshGeometryRevision(object, vertices) !== null &&
      vertices.dynamic !== true &&
      scaleX > 0.000001 && scaleY > 0.000001 && scaleZ > 0.000001 &&
      !emitWireSegments &&
      !(bundle && Array.isArray(bundle.waterSystems) && bundle.waterSystems.length) &&
      // Both GPU backends apply the full model matrix (and its affine normal
      // transform) to retained geometry. Positive non-uniform scale and
      // unindexed triangle lists therefore stay local for both the colour and
      // shadow passes instead of paying a world-space CPU bake per frame.
      !(object && object.skin) &&
      !(object && object.computedMorph) &&
      !(object && (object.dynamicGeometry || object.geometryDynamic || object.geometryDirty)) &&
      !(vertices && (vertices.dynamic || vertices.dirty || vertices.needsUpdate)) &&
      !sceneMaterialUsesAuthoredMeshShader(material) &&
      hasAttribute("positions", 3) &&
      hasAttribute("normals", 3) &&
      hasAttribute("uvs", 2) &&
      hasAttribute("tangents", 4)
    );
  }

  function appendSceneMeshObjectToBundle(bundle, materialLookup, camera, width, height, object, lights, environment, timeSeconds) {
    const vertices = object && object.vertices;
    if (!vertices || !vertices.positions || !vertices.count) {
      return;
    }
    const authoredOutlineColor = object && typeof object.outlineColor === "string"
      ? object.outlineColor.trim()
      : "";
    const authoredOutlineWidth = sceneNumber(object && object.outlineWidth, 0);
    const hasAuthoredOutline = Boolean(object && object.selected && (authoredOutlineColor || authoredOutlineWidth > 0));
    const sourceMaterial = sceneObjectMaterialProfile(object);
    // Custom shaders cannot safely accept the standard-material emissive lift.
    // Preserve their historical generated-edge selection fallback unless the
    // backend already owns its selection treatment (Selena does).
    const needsCustomShaderSelectionEdges = Boolean(
      object && object.selected && !hasAuthoredOutline &&
      sceneMaterialUsesAuthoredMeshShader(sourceMaterial) &&
      !sceneMaterialSuppressesGeneratedWireSegments(sourceMaterial)
    );
    const emitsSelectionEdges = hasAuthoredOutline || needsCustomShaderSelectionEdges;
    const material = sceneSelectedMaterialProfile(sourceMaterial, object, emitsSelectionEdges);
    if (sceneMeshObjectEffectivelyInvisible(object, material)) {
      return;
    }
    const materialIndex = sceneBundleMaterialIndex(bundle, materialLookup, material);
    const outlineColor = emitsSelectionEdges ? (authoredOutlineColor || "#facc15") : "";
    const outlineWidth = emitsSelectionEdges ? Math.max(2, authoredOutlineWidth || 3) : 0;
    const outlineLighting = outlineColor ? sceneColorRGBA(outlineColor, [1, 0.8, 0.15, 1]) : null;
    const objectPassString = sceneWorldObjectRenderPass(object, material);
    const objectPassIndex = objectPassString === "alpha" ? 1 : (objectPassString === "additive" ? 2 : 0);
    const emitWireSegments = !sceneMaterialSuppressesGeneratedWireSegments(material) && Boolean(material && material.wireframe || outlineWidth > 0);
    const geometryRevision = sceneMeshGeometryRevision(object, vertices);
    if (bundle && bundle.retainedGeometryTelemetry) {
      bundle.retainedGeometryTelemetry.eligible += 1;
    }
    // bundle.worldMeshColors is a CPU-baked per-vertex LIT color (ambient +
    // sky/ground + every scene light, via sceneLitColorRGBA) that historically
    // fed the legacy immediate-mode WebGL mesh fallback
    // (renderSceneWebGLMeshObject, reached only through
    // renderSceneWebGLWorldBundle when the bundle also carries world LINE
    // segments -- e.g. a debug grid or gizmo helper -- AND the object's
    // material has no texture). Neither the modern WebGL2 PBR path
    // (drawPBRObjectList/buildPBRDrawList in 16-scene-webgl.js) nor the
    // WebGPU renderer (16a-scene-webgpu.js) ever reads worldMeshColors --
    // both compute lighting in the GPU fragment shader from
    // worldMeshPositions/Normals/UVs/Tangents instead (grep confirms zero
    // references in either file). Computing the full per-vertex analytic
    // light loop for every triangle corner of every mesh object, every
    // frame, was therefore pure waste for the overwhelming majority of
    // scenes: a duck/torus-class glTF or procedural mesh with 10k+ vertices
    // paid tens of milliseconds/frame in sceneLitColorRGBA (regex-based hex
    // color parsing + a light-array loop, repeated per triangle corner) for
    // an array nothing on the hot render path ever reads -- see the
    // water-parity/p5-duck PR description for the profiled evidence (~30%+
    // of total CPU time, capping a 12k-vertex glTF object's frame rate at
    // ~1/4 of an equivalent low-poly object's).
    //
    // The one case where the computed lighting genuinely matters: wire
    // segments (wireframe fill or an explicit/custom-shader selection outline,
    // appendSceneMeshWireSegment below) ARE drawn through the shared
    // world-line path both backends actually render, so `lighting` is still
    // computed with full fidelity whenever emitWireSegments is true. In the
    // (far more common) non-wireframe case, worldMeshColors gets the
    // material's flat base color instead -- computed ONCE per object here,
    // not per vertex corner -- which keeps the rare legacy fallback's output
    // plausible without paying per-vertex lighting cost nothing displays.
    const flatMeshColor = emitWireSegments ? null : sceneColorRGBA(material && material.color, [0.55, 0.88, 1, 1]);
    if (object.skin && vertices.joints && vertices.weights) {
      const bounds = vertices._skinnedLocalBounds || object.bounds || { minX: -1, minY: -1, minZ: -1, maxX: 1, maxY: 2, maxZ: 1 };
      bundle.meshObjects.push({
        id: object.id,
        kind: object.kind,
        pickable: typeof object.pickable === "boolean" ? object.pickable : undefined,
        materialIndex: materialIndex,
        renderPass: sceneWorldObjectRenderPass(object, material),
        _renderPassDerived: (object && object._renderPassDerived) === true,
        texture: material && typeof material.texture === "string" ? material.texture : (typeof object.texture === "string" ? object.texture : ""),
        static: false,
        castShadow: false,
        receiveShadow: Boolean(object.receiveShadow),
        depthWrite: object.depthWrite,
        bounds: bounds,
        depthNear: 0,
        depthFar: 0,
        depthCenter: 0,
        viewCulled: false,
        doubleSided: Boolean(object.doubleSided),
        skin: object.skin,
        vertices: vertices,
        directVertices: true,
        modelMatrix: sceneObjectModelMatrix(object, timeSeconds),
        vertexOffset: 0,
        vertexCount: Math.max(0, Math.floor(sceneNumber(vertices.count, 0))),
      });
      return;
    }
    if (sceneMeshCanRetainLocalGeometry(bundle, object, material, vertices, emitWireSegments)) {
      const modelMatrix = sceneObjectModelMatrix(object, timeSeconds);
      const localBounds = sceneMeshLocalBounds(vertices, geometryRevision);
      const bounds = sceneTransformMeshBounds(localBounds, modelMatrix);
      if (bounds) {
        const vertexCount = Math.max(0, Math.floor(sceneNumber(vertices.count, 0)));
        const depth = sceneBoundsDepthMetrics(bounds, camera, object);
        bundle.meshObjects.push({
          id: object.id,
          kind: object.kind,
          pickable: typeof object.pickable === "boolean" ? object.pickable : undefined,
          materialIndex: materialIndex,
          renderPass: sceneWorldObjectRenderPass(object, material),
          _renderPassDerived: (object && object._renderPassDerived) === true,
          texture: material && typeof material.texture === "string" ? material.texture : (typeof object.texture === "string" ? object.texture : ""),
          static: Boolean(object.static),
          castShadow: Boolean(object.castShadow),
          receiveShadow: Boolean(object.receiveShadow),
          depthWrite: object.depthWrite,
          bounds,
          depthNear: depth.near,
          depthFar: depth.far,
          depthCenter: depth.center,
          viewCulled: Boolean(object.viewCulled) || sceneBoundsViewCulled(bounds, camera, object),
          doubleSided: Boolean(object.doubleSided),
          skin: null,
          vertices,
          directVertices: true,
          retainedGeometry: true,
          resourceOwner: object,
          geometryRevision,
          modelMatrix,
          vertexOffset: 0,
          vertexCount,
        });
        bundle.retainedMeshObjectCount += 1;
        bundle.retainedMeshVertexCount += vertexCount;
        bundle.retainedGeometryTelemetry.retained += 1;
        return;
      }
    }
    if (bundle && bundle.retainedGeometryTelemetry) {
      bundle.retainedGeometryTelemetry.fallback += 1;
    }
    const wireVertexOffset = bundle.worldPositions.length / 3;
    const meshVertexOffset = bundle.worldMeshPositions.length / 3;
    let wireVertexCount = 0;
    let meshVertexCount = 0;
    let bounds = null;

    const points = _meshTrianglePoints;
    const positions = vertices.positions;
    const modelMatrix = sceneObjectModelMatrix(object, timeSeconds);
    const bakeLinearState = sceneObjectMeshBakeLinearState(object, modelMatrix);
    const reverseWinding = bakeLinearState[9] < 0;
    // Indexed geometry keeps its authored triangle order: dereference the index
    // list while baking so the world soup, wire segments, and picking all see
    // exactly the triangles the author wrote. Unindexed geometry iterates the
    // flat stream unchanged.
    const authoredIndices = vertices.indices instanceof Uint32Array &&
      vertices.indices.length >= 3 &&
      vertices.indices.length % 3 === 0
      ? vertices.indices
      : null;
    const drawnTriangleCount = authoredIndices ? authoredIndices.length : vertices.count;
    for (let tri = 0; tri + 2 < drawnTriangleCount; tri += 3) {
      // Translate the three triangle vertices directly from the raw
      // positions Float32Array into hoisted scratch points, skipping the
      // intermediate sceneMeshVertexPoint object allocation (was 3 extra
      // allocs per triangle). points[] itself is the shared
      // _meshTrianglePoints module scratch — all downstream consumers
      // (lighting computation, mesh buffer push loop, three wire segment
      // calls) read fields inline before the next iteration clobbers
      // them, so the scratch is stable within each triangle.
      // A negative determinant reverses the rasterizer's front-face sense.
      // Swap vertices 1 and 2 while baking so every backend can keep its fixed
      // CCW front-face contract. UVs, normals, and tangents use the same source
      // order below, preserving picking interpolation and triangle identity.
      const base0 = authoredIndices ? authoredIndices[tri] : tri;
      const base1 = authoredIndices ? authoredIndices[tri + 1] : tri + 1;
      const base2 = authoredIndices ? authoredIndices[tri + 2] : tri + 2;
      const source0 = base0;
      const source1 = reverseWinding ? base2 : base1;
      const source2 = reverseWinding ? base1 : base2;
      const tri0 = source0 * 3;
      const tri1 = source1 * 3;
      const tri2 = source2 * 3;
      sceneMatrixTransformInto(points[0], modelMatrix, positions[tri0], positions[tri0 + 1], positions[tri0 + 2], 4, true);
      sceneMatrixTransformInto(points[1], modelMatrix, positions[tri1], positions[tri1 + 1], positions[tri1 + 2], 4, true);
      sceneMatrixTransformInto(points[2], modelMatrix, positions[tri2], positions[tri2 + 1], positions[tri2 + 2], 4, true);
      const normals = [
        sceneMeshWorldNormal(vertices, source0, bakeLinearState),
        sceneMeshWorldNormal(vertices, source1, bakeLinearState),
        sceneMeshWorldNormal(vertices, source2, bakeLinearState),
      ];
      // Full per-vertex analytic lighting is only computed when its result
      // is actually visible (wire segments) -- see flatMeshColor's comment
      // above. Otherwise reuse the one flat base color computed once for
      // the whole object; worldMeshColors' only consumer (the legacy
      // untextured-WebGL fallback) doesn't need per-vertex fidelity.
      const lighting = emitWireSegments
        ? [
          sceneLitColorRGBA(material, points[0], normals[0], lights, environment),
          sceneLitColorRGBA(material, points[1], normals[1], lights, environment),
          sceneLitColorRGBA(material, points[2], normals[2], lights, environment),
        ]
        : null;
      const uvs = [
        sceneMeshVertexUV(vertices, source0),
        sceneMeshVertexUV(vertices, source1),
        sceneMeshVertexUV(vertices, source2),
      ];
      const tangents = [
        sceneMeshWorldTangent(vertices, source0, modelMatrix, normals[0], bakeLinearState[9]),
        sceneMeshWorldTangent(vertices, source1, modelMatrix, normals[1], bakeLinearState[9]),
        sceneMeshWorldTangent(vertices, source2, modelMatrix, normals[2], bakeLinearState[9]),
      ];

      for (let index = 0; index < 3; index += 1) {
        const point = points[index];
        const normal = normals[index];
        const uv = uvs[index];
        const tangent = tangents[index];
        const color = lighting ? lighting[index] : flatMeshColor;
        bundle.worldMeshPositions.push(point.x, point.y, point.z);
        bundle.worldMeshColors.push(color[0], color[1], color[2], color[3]);
        bundle.worldMeshNormals.push(normal.x, normal.y, normal.z);
        bundle.worldMeshUVs.push(uv.x, uv.y);
        bundle.worldMeshTangents.push(tangent.x, tangent.y, tangent.z, tangent.w);
        bounds = sceneExpandWorldBounds(bounds, point);
        meshVertexCount += 1;
      }

      if (emitWireSegments) {
        const line0 = outlineLighting || lighting[0];
        const line1 = outlineLighting || lighting[1];
        const line2 = outlineLighting || lighting[2];
        wireVertexCount += appendSceneMeshWireSegment(bundle, camera, width, height, points[0], points[1], line0, line1, outlineWidth, objectPassIndex);
        wireVertexCount += appendSceneMeshWireSegment(bundle, camera, width, height, points[1], points[2], line1, line2, outlineWidth, objectPassIndex);
        wireVertexCount += appendSceneMeshWireSegment(bundle, camera, width, height, points[2], points[0], line2, line0, outlineWidth, objectPassIndex);
      }
    }

    if (!bounds || meshVertexCount <= 0) {
      return;
    }
    const depth = sceneBoundsDepthMetrics(bounds, camera, object);
    const shared = {
      id: object.id,
      kind: object.kind,
      pickable: typeof object.pickable === "boolean" ? object.pickable : undefined,
      materialIndex: materialIndex,
      renderPass: sceneWorldObjectRenderPass(object, material),
      _renderPassDerived: (object && object._renderPassDerived) === true,
      texture: material && typeof material.texture === "string" ? material.texture : (typeof object.texture === "string" ? object.texture : ""),
      static: Boolean(object.static),
      castShadow: Boolean(object.castShadow),
      receiveShadow: Boolean(object.receiveShadow),
      depthWrite: object.depthWrite,
      bounds: bounds,
      depthNear: depth.near,
      depthFar: depth.far,
      depthCenter: depth.center,
      viewCulled: Boolean(object.viewCulled) || sceneBoundsViewCulled(bounds, camera, object),
      doubleSided: Boolean(object.doubleSided),
      skin: object.skin,
      vertices: vertices,
      computedMorph: object.computedMorph || null,
    };
    if (wireVertexCount > 0) {
      bundle.objects.push(Object.assign({}, shared, {
        vertexOffset: wireVertexOffset,
        vertexCount: wireVertexCount,
      }));
    }
    bundle.meshObjects.push(Object.assign({}, shared, {
      vertexOffset: meshVertexOffset,
      vertexCount: meshVertexCount,
    }));
    bundle.worldBakedMeshObjectCount += 1;
    bundle.worldBakedMeshVertexCount += meshVertexCount;
  }

  function sceneObjectHasTexturedSurface(object, material) {
    return Boolean(
      object &&
      object.kind === "plane" &&
      material &&
      typeof material.texture === "string" &&
      material.texture.trim() !== "",
    );
  }


  function appendSceneSurfaceToBundle(bundle, camera, object, materialIndex, material, bounds, depthMetrics, timeSeconds) {
    if (!sceneObjectHasTexturedSurface(object, material)) {
      return;
    }
    bundle.surfaces.push({
      id: object.id,
      kind: object.kind,
      materialIndex: materialIndex,
      renderPass: sceneWorldObjectRenderPass(object, material),
      _renderPassDerived: (object && object._renderPassDerived) === true,
      static: Boolean(object.static),
      positions: scenePlaneSurfacePositions(scenePlaneSurfaceCorners(object, timeSeconds)),
      uv: scenePlaneSurfaceUVs(),
      vertexCount: 6,
      bounds: bounds,
      depthNear: depthMetrics.near,
      depthFar: depthMetrics.far,
      depthCenter: depthMetrics.center,
      viewCulled: Boolean(object.viewCulled) || sceneBoundsViewCulled(bounds, camera, object),
    });
  }

  function sceneLabelPoint(label, timeSeconds) {
    const offset = sceneLabelOffset(label, timeSeconds);
    return {
      x: label.x + offset.x,
      y: label.y + offset.y,
      z: label.z + offset.z,
    };
  }

  function sceneLabelOffset(label, timeSeconds) {
    if (!label || (!label.shiftX && !label.shiftY && !label.shiftZ)) {
      return { x: 0, y: 0, z: 0 };
    }
    const angle = sceneNumber(label.driftPhase, 0) + timeSeconds * sceneNumber(label.driftSpeed, 0);
    return {
      x: Math.cos(angle) * sceneNumber(label.shiftX, 0),
      y: Math.sin(angle * 0.82 + sceneNumber(label.driftPhase, 0) * 0.35) * sceneNumber(label.shiftY, 0),
      z: Math.sin(angle) * sceneNumber(label.shiftZ, 0),
    };
  }

  function sceneSpritePoint(sprite, timeSeconds) {
    const offset = sceneSpriteOffset(sprite, timeSeconds);
    return {
      x: sceneNumber(sprite && sprite.x, 0) + offset.x,
      y: sceneNumber(sprite && sprite.y, 0) + offset.y,
      z: sceneNumber(sprite && sprite.z, 0) + offset.z,
    };
  }

  function sceneSpriteOffset(sprite, timeSeconds) {
    if (!sprite || (!sprite.shiftX && !sprite.shiftY && !sprite.shiftZ)) {
      return { x: 0, y: 0, z: 0 };
    }
    const angle = sceneNumber(sprite.driftPhase, 0) + timeSeconds * sceneNumber(sprite.driftSpeed, 0);
    return {
      x: Math.cos(angle) * sceneNumber(sprite.shiftX, 0),
      y: Math.sin(angle * 0.82 + sceneNumber(sprite.driftPhase, 0) * 0.35) * sceneNumber(sprite.shiftY, 0),
      z: Math.sin(angle) * sceneNumber(sprite.shiftZ, 0),
    };
  }

  function sceneProjectedSpriteSize(camera, width, height, sprite, depth) {
    if (depth <= 0) {
      return { width: 0, height: 0 };
    }
    const normalizedCamera = sceneRenderCamera(camera);
    if (normalizedCamera.kind === "orthographic") {
      const bounds = sceneOrthographicBounds(normalizedCamera, width, height);
      const spanX = Math.max(0.000001, bounds.right - bounds.left);
      const spanY = Math.max(0.000001, bounds.top - bounds.bottom);
      const scale = Math.max(0.05, sceneNumber(sprite && sprite.scale, 1));
      const worldWidth = Math.max(0.05, sceneNumber(sprite && sprite.width, 1.25));
      const worldHeight = Math.max(0.05, sceneNumber(sprite && sprite.height, worldWidth));
      return {
        width: Math.max(1, worldWidth * scale * (width / spanX)),
        height: Math.max(1, worldHeight * scale * (height / spanY)),
      };
    }
    const focal = (Math.min(width, height) / 2) / Math.tan((normalizedCamera.fov * Math.PI) / 360);
    const scale = Math.max(0.05, sceneNumber(sprite && sprite.scale, 1));
    const worldWidth = Math.max(0.05, sceneNumber(sprite && sprite.width, 1.25));
    const worldHeight = Math.max(0.05, sceneNumber(sprite && sprite.height, worldWidth));
    return {
      width: Math.max(1, (worldWidth * scale * focal) / depth),
      height: Math.max(1, (worldHeight * scale * focal) / depth),
    };
  }

  function appendSceneLabelToBundle(bundle, camera, width, height, label, timeSeconds) {
    const point = sceneLabelPoint(label, timeSeconds);
    const projected = sceneProjectPoint(point, camera, width, height);
    if (!projected) {
      return;
    }

    const marginX = Math.max(24, sceneNumber(label.maxWidth, 180));
    const marginY = Math.max(24, sceneNumber(label.lineHeight, 18) * 2);
    if (projected.x < -marginX || projected.x > width + marginX || projected.y < -marginY || projected.y > height + marginY) {
      return;
    }

    bundle.labels.push({
      id: label.id,
      text: label.text,
      className: label.className,
      position: { x: projected.x, y: projected.y },
      depth: projected.depth,
      priority: sceneNumber(label.priority, 0),
      maxWidth: sceneNumber(label.maxWidth, 180),
      maxLines: Math.max(0, Math.floor(sceneNumber(label.maxLines, 0))),
      overflow: normalizeTextLayoutOverflow(label.overflow),
      font: label.font,
      lineHeight: sceneNumber(label.lineHeight, 18),
      color: label.color,
      background: label.background,
      borderColor: label.borderColor,
      offsetX: sceneNumber(label.offsetX, 0),
      offsetY: sceneNumber(label.offsetY, -14),
      anchorX: sceneNumber(label.anchorX, 0.5),
      anchorY: sceneNumber(label.anchorY, 1),
      collision: normalizeSceneLabelCollision(label.collision),
      occlude: Boolean(label.occlude),
      whiteSpace: normalizeSceneLabelWhiteSpace(label.whiteSpace),
      textAlign: normalizeSceneLabelAlign(label.textAlign),
    });
  }

  function appendSceneSpriteToBundle(bundle, camera, width, height, sprite, timeSeconds) {
    const point = sceneSpritePoint(sprite, timeSeconds);
    const projected = sceneProjectPoint(point, camera, width, height);
    if (!projected) {
      return;
    }
    const size = sceneProjectedSpriteSize(camera, width, height, sprite, projected.depth);
    if (size.width <= 0 || size.height <= 0) {
      return;
    }
    const marginX = Math.max(24, size.width);
    const marginY = Math.max(24, size.height);
    if (projected.x < -marginX || projected.x > width + marginX || projected.y < -marginY || projected.y > height + marginY) {
      return;
    }
    bundle.sprites.push({
      id: sprite.id,
      src: sprite.src,
      className: sprite.className,
      position: { x: projected.x, y: projected.y },
      // world and scale exist for the ray pick. sceneRaycastPickPoints in
      // 17-scene-input.js tests a sprite as a sphere at its world point, scaled
      // like spriteRadiusScale does in scene/raycast.go. `position` above is
      // already projected to screen pixels, so it cannot serve. `point` is the
      // object sceneSpritePoint just returned, so this stores it rather than
      // allocating a second one.
      world: point,
      scale: sceneNumber(sprite.scale, 1),
      depth: projected.depth,
      priority: sceneNumber(sprite.priority, 0),
      width: size.width,
      height: size.height,
      opacity: clamp01(sceneNumber(sprite.opacity, 1)),
      offsetX: sceneNumber(sprite.offsetX, 0),
      offsetY: sceneNumber(sprite.offsetY, 0),
      anchorX: sceneNumber(sprite.anchorX, 0.5),
      anchorY: sceneNumber(sprite.anchorY, 0.5),
      occlude: Boolean(sprite.occlude),
      fit: normalizeSceneSpriteFit(sprite.fit),
    });
  }

  function appendSceneHTMLToBundle(bundle, materialLookup, camera, width, height, entry, timeSeconds) {
    const point = sceneSpritePoint(entry, timeSeconds);
    const projected = sceneProjectPoint(point, camera, width, height);
    if (!projected) {
      return;
    }
    const size = sceneProjectedSpriteSize(camera, width, height, entry, projected.depth);
    if (size.width <= 0 || size.height <= 0) {
      return;
    }
    const marginX = Math.max(24, size.width);
    const marginY = Math.max(24, size.height);
    if (projected.x < -marginX || projected.x > width + marginX || projected.y < -marginY || projected.y > height + marginY) {
      return;
    }
    const mode = normalizeSceneHTMLMode(entry.mode, "dom");
    const texture = sceneHTMLTextureMetadata(entry);
    appendSceneHTMLTextureSurfaceToBundle(bundle, materialLookup, camera, entry, point, texture, timeSeconds);
    bundle.html.push({
      id: entry.id,
      target: entry.target,
      mode,
      html: entry.html,
      className: entry.className,
      fallback: entry.fallback,
      fallbackReason: texture.overBudget ? "html-texture-memory-cap" : entry.fallbackReason,
      textureKey: texture.key,
      textureWidth: texture.width,
      textureHeight: texture.height,
      textureBytes: texture.bytes,
      textureMaxBytes: texture.maxBytes,
      textureOverBudget: texture.overBudget,
      textureReady: texture.ready,
      surfaceWidth: sceneNumber(entry.surfaceWidth, sceneNumber(entry.width, 1.8)),
      surfaceHeight: sceneNumber(entry.surfaceHeight, sceneNumber(entry.height, 0.72)),
      rotationX: sceneNumber(entry.rotationX, 0),
      rotationY: sceneNumber(entry.rotationY, 0),
      rotationZ: sceneNumber(entry.rotationZ, 0),
      spinX: sceneNumber(entry.spinX, 0),
      spinY: sceneNumber(entry.spinY, 0),
      spinZ: sceneNumber(entry.spinZ, 0),
      position: { x: projected.x, y: projected.y },
      depth: projected.depth,
      priority: sceneNumber(entry.priority, 0),
      width: size.width,
      height: size.height,
      opacity: clamp01(sceneNumber(entry.opacity, 1)),
      offsetX: sceneNumber(entry.offsetX, 0),
      offsetY: sceneNumber(entry.offsetY, 0),
      anchorX: sceneNumber(entry.anchorX, 0.5),
      anchorY: sceneNumber(entry.anchorY, 0.5),
      occlude: Boolean(entry.occlude),
      pointerEvents: normalizeSceneHTMLPointerEvents(entry.pointerEvents, "none"),
    });
  }

  function sceneHTMLTextureMetadata(entry) {
    const mode = normalizeSceneHTMLMode(entry && entry.mode, "dom");
    if (mode !== "texture") {
      return { key: "", width: 0, height: 0, bytes: 0, maxBytes: 0, overBudget: false, ready: false };
    }
    const width = Math.max(1, Math.floor(sceneNumber(entry.textureWidth, 512)));
    const height = Math.max(1, Math.floor(sceneNumber(entry.textureHeight, 320)));
    const maxPixels = Math.max(0, Math.floor(sceneNumber(entry.maxTexturePixels, 1024 * 1024)));
    const bytes = width * height * 4;
    const maxBytes = maxPixels > 0 ? maxPixels * 4 : 0;
    return {
      key: typeof entry.textureKey === "string" && entry.textureKey.trim() ? entry.textureKey.trim() : "gosx-html://" + (entry.id || "scene-html"),
      width,
      height,
      bytes,
      maxBytes,
      overBudget: maxBytes > 0 && bytes > maxBytes,
      ready: sceneBool(entry.textureReady, false),
    };
  }

  function sceneHTMLTextureSurfaceObject(entry, point) {
    return {
      x: point.x,
      y: point.y,
      z: point.z,
      width: sceneNumber(entry.surfaceWidth, sceneNumber(entry.width, 1.8)),
      depth: sceneNumber(entry.surfaceHeight, sceneNumber(entry.height, 0.72)),
      height: 0,
      scaleX: 1,
      scaleY: 1,
      scaleZ: 1,
      rotationX: sceneNumber(entry.rotationX, 0),
      rotationY: sceneNumber(entry.rotationY, 0),
      rotationZ: sceneNumber(entry.rotationZ, 0),
      spinX: sceneNumber(entry.spinX, 0),
      spinY: sceneNumber(entry.spinY, 0),
      spinZ: sceneNumber(entry.spinZ, 0),
      shiftX: sceneNumber(entry.shiftX, 0),
      shiftY: sceneNumber(entry.shiftY, 0),
      shiftZ: sceneNumber(entry.shiftZ, 0),
      driftSpeed: sceneNumber(entry.driftSpeed, 0),
      driftPhase: sceneNumber(entry.driftPhase, 0),
    };
  }

  function appendSceneHTMLTextureSurfaceToBundle(bundle, materialLookup, camera, entry, point, texture, timeSeconds) {
    if (!texture || !texture.ready || texture.overBudget) {
      return;
    }
    const surfaceObject = sceneHTMLTextureSurfaceObject(entry, point);
    const corners = scenePlaneSurfaceCorners(surfaceObject, timeSeconds);
    const bounds = renderBoundsFromPoints(corners);
    if (!bounds) {
      return;
    }
    const material = {
      kind: "flat",
      color: "#ffffff",
      texture: texture.key,
      opacity: clamp01(sceneNumber(entry.opacity, 1)),
      wireframe: false,
      blendMode: "alpha",
      renderPass: "alpha",
      emissive: 1,
      unlit: true,
    };
    material.key = "html-texture|" + String(entry.id || "") + "|" + texture.key + "|" + texture.width + "x" + texture.height + "|" + material.opacity.toFixed(3);
    material.shaderData = sceneMaterialShaderData(material);
    const materialIndex = sceneBundleMaterialIndex(bundle, materialLookup, material);
    const depth = sceneBoundsDepthMetrics(bounds, camera, surfaceObject);
    bundle.surfaces.push({
      id: entry.id,
      kind: "html",
      sourceKind: "html",
      sourceID: entry.id,
      textureKey: texture.key,
      textureWidth: texture.width,
      textureHeight: texture.height,
      textureBytes: texture.bytes,
      textureMaxBytes: texture.maxBytes,
      textureReady: true,
      contentWidth: texture.width,
      contentHeight: texture.height,
      fallback: entry.fallback,
      fallbackReason: entry.fallbackReason,
      materialIndex,
      renderPass: "alpha",
      static: false,
      positions: scenePlaneSurfacePositions(corners),
      uv: scenePlaneSurfaceUVs(),
      vertexCount: 6,
      bounds,
      depthNear: depth.near,
      depthFar: depth.far,
      depthCenter: depth.center,
      viewCulled: sceneBoundsViewCulled(bounds, camera, surfaceObject),
    });
  }

  function renderBoundsFromPoints(points) {
    if (!Array.isArray(points) || points.length === 0) {
      return null;
    }
    let bounds = null;
    for (const point of points) {
      bounds = sceneExpandWorldBounds(bounds, point);
    }
    return bounds;
  }

  function sceneExpandWorldBounds(bounds, point) {
    const next = bounds || {
      minX: point.x,
      minY: point.y,
      minZ: point.z,
      maxX: point.x,
      maxY: point.y,
      maxZ: point.z,
    };
    next.minX = Math.min(next.minX, point.x);
    next.minY = Math.min(next.minY, point.y);
    next.minZ = Math.min(next.minZ, point.z);
    next.maxX = Math.max(next.maxX, point.x);
    next.maxY = Math.max(next.maxY, point.y);
    next.maxZ = Math.max(next.maxZ, point.z);
    return next;
  }

  function appendSceneLine(bundle, width, height, from, to, color, lineWidth, options) {
    if (!from || !to) return;
    const rgba = sceneColorRGBA(color, [0.55, 0.88, 1, 1]);
    const fromClip = sceneClipPoint(from, width, height);
    const toClip = sceneClipPoint(to, width, height);
    const dashOptions = options && typeof options === "object" ? options : null;
    bundle.lines.push({
      from: from,
      to: to,
      color: color,
      lineWidth: lineWidth,
      lineDash: Boolean(dashOptions && dashOptions.dashed),
      dashSize: sceneNumber(dashOptions && dashOptions.dashSize, 0),
      gapSize: sceneNumber(dashOptions && dashOptions.gapSize, 0),
    });
    bundle.positions.push(fromClip.x, fromClip.y, toClip.x, toClip.y);
    bundle.colors.push(rgba[0], rgba[1], rgba[2], rgba[3], rgba[0], rgba[1], rgba[2], rgba[3]);
  }






  if (typeof window !== "undefined" && window.__gosx_runtime_api) {
    window.__gosx_runtime_api.browserCapabilitySupported = browserCapabilitySupported;
    window.__gosx_runtime_api.runtimeCapabilityStatus = runtimeCapabilityStatus;
    window.__gosx_runtime_api.engineCapabilityStatus = engineCapabilityStatus;
    // Engine render-bundle normalizers (camera/label/html), consumed by
    // normalizeEngineRenderBundle in 30-tail.js's "engine mounting" section.
    // That function ships in bootstrap-feature-engines.js (see
    // 26b-feature-engines-prefix.js's registerFeature("engines", ...)), which
    // does NOT include this file — these live beyond the RUNTIME_UTILS
    // extraction bootstrap-runtime.js pulls from 10-runtime-scene-core.js (see
    // build-bootstrap.mjs). Bridged here so any page that already loaded a
    // chunk containing this file (monolithic bootstrap.js, or the Scene3D
    // feature chunk before "engines" reads runtimeApi) shares the canonical
    // implementation instead of the engines-prefix's local fallback copy.
    window.__gosx_runtime_api.normalizeSceneCameraKind = normalizeSceneCameraKind;
    window.__gosx_runtime_api.sceneRenderCamera = sceneRenderCamera;
    window.__gosx_runtime_api.sceneLabelClassName = sceneLabelClassName;
    window.__gosx_runtime_api.normalizeSceneLabelCollision = normalizeSceneLabelCollision;
    window.__gosx_runtime_api.normalizeSceneLabelWhiteSpace = normalizeSceneLabelWhiteSpace;
    window.__gosx_runtime_api.normalizeSceneLabelAlign = normalizeSceneLabelAlign;
    window.__gosx_runtime_api.normalizeSceneHTMLMode = normalizeSceneHTMLMode;
    window.__gosx_runtime_api.normalizeSceneHTMLPointerEvents = normalizeSceneHTMLPointerEvents;
  }

  // Scene3D shared API — exposed for the async bootstrap-feature-scene3d.js
  // chunk. Files 11-20 (scene-math through scene-mount) depend on these
  // functions via closure capture in the monolithic bootstrap.js. When
  // loaded as a separate feature chunk, they destructure from this
  // namespace instead.
  window.__gosx_scene3d_api = {
    appendSceneObjectToBundle,
    appendSceneSurfaceToBundle,
    applySceneCommands,
    cancelEngineFrame,
    clearChildren,
    createSceneRenderBundle,
    sceneSelectLODObjects,
    SCENE_IR_VERSION: 1,
    SCENE_IR_SCHEMA: "gosx.scene3d.ir.v1",
    SCENE_RENDER_BUNDLE_VERSION: 1,
    SCENE_POST_TONE_MAPPING: "toneMapping",
    SCENE_POST_BLOOM: "bloom",
    SCENE_POST_VIGNETTE: "vignette",
    SCENE_POST_COLOR_GRADE: "colorGrade",
    SCENE_POST_SSAO: "ssao",
    SCENE_POST_DOF: "dof",
    SCENE_POST_CUSTOM_POST: "customPost",
    SCENE_POST_FXAA: "fxaa",
    validateSceneIR: typeof validateSceneIR === "function" ? validateSceneIR : undefined,
    prepareScene: typeof prepareScene === "function" ? prepareScene : undefined,
    scenePreparedCommandSequence: typeof scenePreparedCommandSequence === "function" ? scenePreparedCommandSequence : undefined,
    sceneCachedBuffer: typeof sceneCachedBuffer === "function" ? sceneCachedBuffer : undefined,
    sceneWebGLCommandSequence: typeof sceneWebGLCommandSequence === "function" ? sceneWebGLCommandSequence : undefined,
    sceneWebGPUCommandSequence: typeof sceneWebGPUCommandSequence === "function" ? sceneWebGPUCommandSequence : undefined,
    sceneBackendRegistry: undefined,
    createSceneState,
    createSceneParticleSystem: typeof createSceneParticleSystem === "function" ? createSceneParticleSystem : undefined,
    sceneStateObjectsWithMaterials,
    sceneStatePointsWithMaterials,
    sceneStateInstancedMeshesWithMaterials,
    sceneResolveMaterialUniforms,
    // The legacy vertex-colour renderer moved to 16e-scene-webgl-legacy.js,
    // which ships only in bootstrap.js and in the WebGL chunk. Guard the read:
    // in the split base scene3d chunk the name does not exist, and an
    // unguarded shorthand would throw ReferenceError while this object builds.
    // 16e assigns the real function here when the WebGL chunk lands.
    createSceneWebGLRenderer: typeof createSceneWebGLRenderer === "function" ? createSceneWebGLRenderer : undefined,
    engineFrame,
    normalizeSceneEnvironment,
    normalizeSceneHTML,
    normalizeSceneInstancedGLBMeshEntry,
    normalizeSceneLabel,
    normalizeSceneLabelAlign,
    normalizeSceneLabelCollision,
    normalizeSceneLabelWhiteSpace,
    normalizeSceneLight,
    normalizeSceneObject,
    notifySceneTextureLoaded,
    sceneGizmoTargetAnchor,
    normalizeSceneSprite,
    normalizeSceneSpriteFit,
    listSceneMaterialProfiles: typeof listSceneMaterialProfiles === "function" ? listSceneMaterialProfiles : undefined,
    listSceneParticleForces: typeof listSceneParticleForces === "function" ? listSceneParticleForces : undefined,
    registerSceneMaterialProfile: typeof registerSceneMaterialProfile === "function" ? registerSceneMaterialProfile : undefined,
    registerSceneParticleForce: typeof registerSceneParticleForce === "function" ? registerSceneParticleForce : undefined,
    registerSceneParticleForceKind: typeof registerSceneParticleForceKind === "function" ? registerSceneParticleForceKind : undefined,
    sceneComputeSystemSignature: typeof sceneComputeSystemSignature === "function" ? sceneComputeSystemSignature : undefined,
    sceneMaterialShaderData: typeof sceneMaterialShaderData === "function" ? sceneMaterialShaderData : undefined,
    unregisterSceneMaterialProfile: typeof unregisterSceneMaterialProfile === "function" ? unregisterSceneMaterialProfile : undefined,
    unregisterSceneParticleForce: typeof unregisterSceneParticleForce === "function" ? unregisterSceneParticleForce : undefined,
    publishPointerSignals,
    queueInputSignal,
    sceneAdvanceTransitions,
    sceneApplyLiveEvent,
    sceneBool,
    sceneBoundsDepthMetrics,
    sceneBoundsViewCulled,
    // Frustum-plane extractor — hoisted to 11-scene-math.js (base scene3d
    // bundle) by the WebGL2 cull slice. It MUST be exported here so the
    // separate scene3d-webgpu chunk (16a instanced GPU cull) can reach it;
    // without the bridge the webgpu render path throws
    // "extractFrustumPlanesJS is not defined".
    extractFrustumPlanesJS: typeof extractFrustumPlanesJS === "function" ? extractFrustumPlanesJS : undefined,
    buildSceneWorldDrawPlan: typeof buildSceneWorldDrawPlan === "function" ? buildSceneWorldDrawPlan : undefined,
    createSceneWorldDrawScratch: typeof createSceneWorldDrawScratch === "function" ? createSceneWorldDrawScratch : undefined,
    sceneWorldPointDepth: typeof sceneWorldPointDepth === "function" ? sceneWorldPointDepth : undefined,
    createSceneThickLineScratch: typeof createSceneThickLineScratch === "function" ? createSceneThickLineScratch : undefined,
    expandSceneThickLineIntoScratch: typeof expandSceneThickLineIntoScratch === "function" ? expandSceneThickLineIntoScratch : undefined,
    sceneBundleNeedsThickLines: typeof sceneBundleNeedsThickLines === "function" ? sceneBundleNeedsThickLines : undefined,
    sceneCameraEquivalent,
    sceneOrthographicBounds,
    clamp01,
    sceneHasActiveTransitions,
    sceneHTMLAnimated,
    sceneLabelAnimated,
    sceneMeshMaterialArray: typeof sceneMeshMaterialArray === "function" ? sceneMeshMaterialArray : undefined,
    sceneAnimations,
    sceneInstancedGLBMeshes,
    sceneInstancedGLBModelsFromBatches,
    sceneModels,
    sceneNormalizeDirection,
    sceneNowMilliseconds,
    sceneNumber,
    scenePostDOMRegionPixelBounds,
    sceneWaterAdvanceClock,
    sceneWaterResetClock,
    sceneObjectAnimated,
    scenePointStyleCode,
    scenePrimeInitialTransitions,
    sceneProps,
    // 11-scene-base64.js. The lazily fetched decompress chunk reads it, and so
    // does any caller that needs the raw bytes of a base64 payload.
    sceneBase64Decode: typeof sceneBase64Decode === "function" ? sceneBase64Decode : undefined,
    // 11a-scene-decompress.ts and 11b-scene-points-generate.js ship in the
    // lazily fetched decompress chunk. Guard the reads: in the base chunk the
    // names do not exist, and the chunk suffix assigns the real functions here
    // when it lands. bootstrap.js keeps both files inline, so these carry the
    // real functions from the start.
    sceneDecompressProps: typeof sceneDecompressProps === "function" ? sceneDecompressProps : undefined,
    sceneUpgradeProgressive: typeof sceneUpgradeProgressive === "function" ? sceneUpgradeProgressive : undefined,
    sceneApplyLOD: typeof sceneApplyLOD === "function" ? sceneApplyLOD : undefined,
    sceneGeneratePointsEntry: typeof sceneGeneratePointsEntry === "function" ? sceneGeneratePointsEntry : undefined,
    sceneRenderCamera,
    sceneResolveLightingEnvironment,
    sceneSpriteAnimated,
    sceneStateLabels,
    sceneStateHTML,
    sceneStateLights,
    sceneStateObjects,
    sceneStateSprites,
    sceneTypedFloatArray,
    translateScenePointInto,

    // Typed-array guard used by 16a-scene-webgpu.js drawPointsEntries to test
    // whether entry.positions / sizes / colors are typed arrays. Exported here
    // so the webgpu sub-feature chunk can pull it from the scene3d API rather
    // than relying on a shared IIFE scope (which the sub-feature chunk doesn't
    // have when loaded as a separate <script defer>).
    sceneIsNumericTypedArray,

    // PBR + shadow helpers from 16-scene-webgl.js, re-exported for the
    // bootstrap-feature-scene3d-webgpu.js sub-feature chunk. These are
    // function declarations, hoisted to the enclosing IIFE scope, so
    // the literal captures them even though 16 lexically follows 10.
    scenePBRDepthSort: typeof scenePBRDepthSort === "function" ? scenePBRDepthSort : undefined,
    scenePBRObjectRenderPass: typeof scenePBRObjectRenderPass === "function" ? scenePBRObjectRenderPass : undefined,
    scenePBRProjectionMatrix: typeof scenePBRProjectionMatrix === "function" ? scenePBRProjectionMatrix : undefined,
    scenePBRProjectionMatrixForCamera: typeof scenePBRProjectionMatrixForCamera === "function" ? scenePBRProjectionMatrixForCamera : undefined,
    scenePBRViewMatrix: typeof scenePBRViewMatrix === "function" ? scenePBRViewMatrix : undefined,
    sceneShadowLightSpaceMatrix: typeof sceneShadowLightSpaceMatrix === "function" ? sceneShadowLightSpaceMatrix : undefined,
    sceneShadowComputeBounds: typeof sceneShadowComputeBounds === "function" ? sceneShadowComputeBounds : undefined,
    generateInstancedGeometry: typeof generateInstancedGeometry === "function" ? generateInstancedGeometry : undefined,
    normalizeInstancedGeometryKind: typeof normalizeInstancedGeometryKind === "function" ? normalizeInstancedGeometryKind : undefined,

    // Post-fx helpers from 15a-scene-postfx-shared.js.
    resolvePostFXFactor: typeof resolvePostFXFactor === "function" ? resolvePostFXFactor : undefined,
    resolveShadowSize: typeof resolveShadowSize === "function" ? resolveShadowSize : undefined,

    // Color helper from 11-scene-math.js (already visible, re-exported
    // explicitly so the webgpu chunk has a stable lookup path).
    sceneColorRGBA: typeof sceneColorRGBA === "function" ? sceneColorRGBA : undefined,

    // Ortho-2D board camera helpers from 11-scene-math.js — the JS half of
    // the native computeOrthoCamera2DMVP golden contract, consumed by the
    // webgpu chunk's uploadFrameUniforms ortho2d branch.
    sceneMat4Ortho2DView: typeof sceneMat4Ortho2DView === "function" ? sceneMat4Ortho2DView : undefined,
    sceneMat4Ortho2DProj: typeof sceneMat4Ortho2DProj === "function" ? sceneMat4Ortho2DProj : undefined,
    sceneMat4Ortho2DViewProj: typeof sceneMat4Ortho2DViewProj === "function" ? sceneMat4Ortho2DViewProj : undefined,
  };
