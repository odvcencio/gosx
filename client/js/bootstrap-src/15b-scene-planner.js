  // Scene planner — backend-agnostic preparation of render bundles into pass
  // buckets, cache keys, and shared upload-cache helpers.

  function sceneCSSDebugLog() {
    if (typeof window === "undefined" || window.__gosx_scene3d_css_debug !== true) {
      return;
    }
    if (typeof console !== "undefined" && typeof console.debug === "function") {
      console.debug.apply(console, arguments);
    }
  }

  function prepareScene(ir, camera, viewport, lastPrepared, cssContext) {
    const initialSource = ir && typeof ir === "object" ? ir : {};
    const css = sceneCSSResolverContext(cssContext);
    css.nowMs = typeof cssContext === "object" && cssContext && cssContext.nowMs ? cssContext.nowMs : Date.now();
    const cssInputSignature = sceneCSSInputSignature(initialSource);
    const cssCache = lastPrepared && lastPrepared.cssCache;
    // Pass previous cache so var transitions can interpolate from old values
    css.prevCache = cssCache || null;
    const hasActiveVarTransitions = cssCache && Array.isArray(cssCache.varTransitions) && cssCache.varTransitions.length > 0;
    const cssResolved = cssCache
      && cssCache.inputSignature === cssInputSignature
      && cssCache.revision === css.revision
      && cssCache.transitionFrame === css.transitionFrame
      && !hasActiveVarTransitions
        ? sceneCSSApplyCachedResolution(initialSource, cssCache)
        : sceneResolveCSSBundleWithContext(initialSource, css, cssInputSignature);
    const source = cssResolved.ir;
    const resolvedCamera = camera || source.camera || {};
    const signature = scenePreparedSignature(source, resolvedCamera, viewport);
    if (lastPrepared && lastPrepared.signature === signature) {
      lastPrepared.ir = source;
      lastPrepared.camera = resolvedCamera;
      lastPrepared.viewport = viewport;
      lastPrepared.resolvedEnv = source.environment || {};
      lastPrepared.cssDynamic = Boolean(cssResolved.dynamic);
      lastPrepared.cssCache = cssResolved.cache;
      return lastPrepared;
    }

    const worldDrawScratch = lastPrepared && lastPrepared.worldDrawScratch
      ? lastPrepared.worldDrawScratch
      : createSceneWorldDrawScratch();
    const worldDrawPlan = buildSceneWorldDrawPlan(source, worldDrawScratch);
    const pbrPasses = prepareScenePBRPasses(source);
    const prepared = {
      ir: source,
      camera: resolvedCamera,
      viewport,
      signature,
      passes: scenePreparedPassList(worldDrawPlan, pbrPasses),
      pbrPasses,
      worldDrawPlan,
      worldDrawScratch,
      caches: lastPrepared && lastPrepared.caches ? lastPrepared.caches : new Map(),
      shadowPassHash: scenePlannerShadowHash(source),
      materialsHash: scenePlannerMaterialsHash(source),
      resolvedEnv: source.environment || {},
      cssDynamic: Boolean(cssResolved.dynamic),
      cssCache: cssResolved.cache,
      rebuilds: lastPrepared ? lastPrepared.rebuilds + 1 : 1,
    };
    return prepared;
  }

  function sceneResolveCSSBundle(source, cssContext) {
    const css = sceneCSSResolverContext(cssContext);
    return sceneResolveCSSBundleWithContext(source, css, sceneCSSInputSignature(source));
  }

  function sceneResolveCSSBundleWithContext(source, css, inputSignature) {
    sceneCSSDebugLog("[gosx:css-transition] FRESH RESOLVE revision=" + css.revision + " prevCache=" + Boolean(css.prevCache));
    const prevCache = css.prevCache || null;
    const prevResolved = prevCache && prevCache.resolvedVars ? prevCache.resolvedVars : null;
    const prevTransitions = prevCache && Array.isArray(prevCache.varTransitions) ? prevCache.varTransitions : [];
    const state = {
      source,
      out: source,
      dynamic: false,
      patches: [],
      resolvedVars: {},
      varTransitions: [],
      prevResolved,
      prevTransitions,
      nowMs: css.nowMs || Date.now(),
      _cssMount: css.mount || null,
    };
    sceneCSSResolveExplicitVars(state, css);
    sceneCSSApplyComputedDefaults(state, css);
    // Advance any in-flight CSS var transitions
    sceneCSSAdvanceVarTransitions(state);
    const cache = {
      inputSignature,
      revision: css.revision,
      transitionFrame: css.transitionFrame,
      dynamic: state.dynamic || state.varTransitions.length > 0,
      patches: state.patches,
      resolvedVars: state.resolvedVars,
      varTransitions: state.varTransitions,
    };
    return {
      ir: state.out,
      dynamic: state.dynamic || state.varTransitions.length > 0,
      cache,
    };
  }

  // sceneCSSVarTransitionKey builds a stable cache key for a resolved var.
  function sceneCSSVarTransitionKey(kind, collectionKey, index, key) {
    if (kind === "topObject") {
      return collectionKey + ":" + key;
    }
    return collectionKey + ":" + index + ":" + key;
  }

  // sceneCSSRecordTransitionTiming extracts the update transition config from
  // a material, environment, or points record. Falls back to any material
  // with a transition config as a scene-wide default.
  function sceneCSSRecordTransitionTiming(state, kind, collectionKey, index) {
    var record = null;
    if (kind === "topObject") {
      var bundle = state.out || state.source || {};
      record = bundle[collectionKey];
    } else {
      var collection = sceneCSSCurrentCollection(state, collectionKey);
      record = Array.isArray(collection) ? collection[index] : null;
    }
    if (record && sceneIsPlainObject(record.transition)) {
      // Record has its own transition config.
    } else {
      // Fall back to scene-wide default from any material with a transition.
      var fallback = sceneCSSDefaultTransitionTiming(state);
      if (fallback) {
        record = { transition: { update: fallback } };
      } else {
        return null;
      }
    }
    var update = record.transition.update || record.transition;
    var duration = sceneCSSParseDuration(update.duration);
    if (duration <= 0) {
      return null;
    }
    return {
      duration: duration,
      easing: typeof update.easing === "string" ? update.easing : "ease-in-out",
    };
  }

  // sceneCSSDefaultTransitionTiming returns the transition timing stashed
  // on the mount element by the scene mount init. The mount extracts this
  // from the original props (materials/environment) before they get merged
  // into the render bundle.
  function sceneCSSDefaultTransitionTiming(state) {
    if (state._defaultTransitionTiming !== undefined) {
      return state._defaultTransitionTiming;
    }
    // Read from the mount element where scene init stashed it
    var mount = state._cssMount;
    var timing = mount && mount.__gosxScene3DCSSVarTransition;
    if (timing && typeof timing.duration === "number" && timing.duration > 0) {
      state._defaultTransitionTiming = timing;
      return timing;
    }
    state._defaultTransitionTiming = null;
    return null;
  }

  // sceneCSSFindMaterialForRecord looks up the material referenced by a
  // points, object, or instanced mesh record via materialIndex or material name.
  function sceneCSSFindMaterialForRecord(state, record) {
    var materials = sceneCSSCurrentCollection(state, "materials");
    if (!Array.isArray(materials) || materials.length === 0) {
      return null;
    }
    // Direct index reference
    var idx = typeof record.materialIndex === "number" ? record.materialIndex : -1;
    if (idx >= 0 && idx < materials.length) {
      return materials[idx];
    }
    // Name-based reference
    var name = typeof record.material === "string" ? record.material : "";
    if (name) {
      for (var i = 0; i < materials.length; i++) {
        if (materials[i] && materials[i].name === name) {
          return materials[i];
        }
      }
    }
    return null;
  }

  // sceneCSSParseDuration parses a duration value — accepts milliseconds
  // (number) or CSS-style strings ("4s", "400ms", "2.5s").
  function sceneCSSParseDuration(value) {
    if (typeof value === "number") {
      return value;
    }
    if (typeof value !== "string") {
      return 0;
    }
    var text = value.trim().toLowerCase();
    if (text.endsWith("ms")) {
      return parseFloat(text) || 0;
    }
    if (text.endsWith("s")) {
      return (parseFloat(text) || 0) * 1000;
    }
    return parseFloat(text) || 0;
  }

  // sceneCSSMaybeTransitionValue checks if a var value should transition.
  // Returns true if a transition was created (caller should skip the slam).
  function sceneCSSMaybeTransitionValue(state, kind, collectionKey, index, key, newValue) {
    var cacheKey = sceneCSSVarTransitionKey(kind, collectionKey, index, key);
    // Record the resolved value for next pass
    state.resolvedVars[cacheKey] = newValue;
    // Check if there's a previous value to transition from
    if (!state.prevResolved || !Object.prototype.hasOwnProperty.call(state.prevResolved, cacheKey)) {
      sceneCSSDebugLog("[gosx:css-transition] no prev for", cacheKey, "value=", newValue);
      return false;
    }
    var oldValue = state.prevResolved[cacheKey];
    if (sceneCSSSameValue(oldValue, newValue)) {
      return false;
    }
    // Check if this record has a transition config
    var timing = sceneCSSRecordTransitionTiming(state, kind, collectionKey, index);
    if (!timing) {
      sceneCSSDebugLog("[gosx:css-transition] no timing for", cacheKey, "old=", oldValue, "new=", newValue);
      return false;
    }
    sceneCSSDebugLog("[gosx:css-transition] CREATING transition", cacheKey, oldValue, "->", newValue, "duration=", timing.duration);
    // Cancel any existing transition for this key
    for (var i = state.varTransitions.length - 1; i >= 0; i--) {
      if (state.varTransitions[i].cacheKey === cacheKey) {
        state.varTransitions.splice(i, 1);
      }
    }
    // Also cancel carried-over transitions for this key
    for (var j = state.prevTransitions.length - 1; j >= 0; j--) {
      if (state.prevTransitions[j].cacheKey === cacheKey) {
        // Use current interpolated value as the new "from"
        var active = state.prevTransitions[j];
        var elapsed = Math.max(0, state.nowMs - active.startTime);
        var t = Math.min(1, elapsed / Math.max(1, active.duration));
        oldValue = sceneTransitionLeafValue(active.from, active.to, sceneTransitionEase(active.easing, t), key);
        state.prevTransitions.splice(j, 1);
      }
    }
    // Create the transition
    state.varTransitions.push({
      cacheKey: cacheKey,
      kind: kind,
      collectionKey: collectionKey,
      index: index,
      key: key,
      from: sceneCloneData(oldValue),
      to: sceneCloneData(newValue),
      startTime: state.nowMs,
      duration: timing.duration,
      easing: timing.easing,
    });
    // Apply the old value for now — the transition will animate toward new
    return true;
  }

  // sceneCSSAdvanceVarTransitions processes all active var transitions,
  // applying interpolated values to the current bundle.
  function sceneCSSAdvanceVarTransitions(state) {
    // Carry over active transitions from the previous pass
    var carried = state.prevTransitions;
    var all = carried.concat(state.varTransitions);
    var active = [];
    for (var i = 0; i < all.length; i++) {
      var transition = all[i];
      var elapsed = Math.max(0, state.nowMs - transition.startTime);
      var rawT = Math.min(1, elapsed / Math.max(1, transition.duration));
      var eased = sceneTransitionEase(transition.easing, rawT);
      var value = sceneTransitionLeafValue(transition.from, transition.to, eased, transition.key);
      // Apply the interpolated value
      if (transition.kind === "topObject") {
        sceneCSSSetTopObjectKey(state, transition.collectionKey, transition.key, value);
      } else if (transition.kind === "nested") {
        sceneCSSSetNestedKey(state, transition.collectionKey, transition.index, transition.childKey || "", transition.key, value);
      } else {
        sceneCSSSetRecordKey(state, transition.collectionKey, transition.index, transition.key, value);
      }
      if (rawT < 1) {
        active.push(transition);
      }
      // Update resolved cache with the target so next diff is correct
      state.resolvedVars[transition.cacheKey] = transition.to;
    }
    state.varTransitions = active;
    if (active.length > 0) {
      state.dynamic = true;
    }
  }

  function sceneCSSResolverContext(input) {
    const context = input && typeof input === "object" ? input : {};
    const mount = context.mount && typeof context.mount === "object" ? context.mount : null;
    const sentinels = context.sentinels || (mount && mount.__gosxScene3DSentinels) || null;
    const win = typeof window !== "undefined" ? window : null;
    const revision = sceneCSSContextRevision(context, mount);
    const animationUntil = sceneCSSContextAnimationUntil(context, mount);
    const now = typeof Date !== "undefined" && typeof Date.now === "function" ? Date.now() : 0;
    return {
      mount,
      sentinels,
      styles: typeof Map === "function" ? new Map() : null,
      hasComputedStyle: Boolean(win && typeof win.getComputedStyle === "function"),
      revision,
      transitionFrame: animationUntil > now ? Math.floor(now / 16) : 0,
    };
  }

  function sceneCSSContextRevision(context, mount) {
    const raw = context.revision != null
      ? context.revision
      : context.cssRevision != null
        ? context.cssRevision
        : mount && mount.__gosxScene3DCSSRevision;
    const revision = Number(raw);
    return Number.isFinite(revision) ? revision : 0;
  }

  function sceneCSSContextAnimationUntil(context, mount) {
    const raw = context.animationUntil != null
      ? context.animationUntil
      : context.cssAnimationUntil != null
        ? context.cssAnimationUntil
        : mount && mount.__gosxScene3DCSSAnimationUntil;
    const until = Number(raw);
    return Number.isFinite(until) ? until : 0;
  }

  function sceneCSSInputSignature(bundle) {
    let hash = 2166136261 >>> 0;
    hash = scenePlannerHashString(hash, "css");
    hash = scenePlannerHashAny(hash, bundle && bundle.environment, 0);
    hash = sceneCSSHashCollection(hash, bundle && bundle.materials, [
      "id", "name", "kind", "color", "opacity", "emissive", "roughness", "metalness",
      "normalMap", "roughnessMap", "metalnessMap", "emissiveMap", "blendMode",
      "renderPass", "depthWrite", "style", "size", "attenuation",
    ]);
    hash = sceneCSSHashCollection(hash, bundle && bundle.lights, [
      "id", "kind", "color", "groundColor", "intensity", "x", "y", "z",
      "directionX", "directionY", "directionZ", "angle", "penumbra",
      "range", "decay", "shadowBias", "shadowSize",
    ]);
    hash = sceneCSSHashCollection(hash, bundle && bundle.objects, [
      "id", "kind", "material", "materialIndex", "color", "opacity", "emissive",
      "roughness", "metalness", "lineWidth", "x", "y", "z", "rotationX",
      "rotationY", "rotationZ", "spinX", "spinY", "spinZ",
    ]);
    hash = sceneCSSHashCollection(hash, bundle && bundle.meshObjects, [
      "id", "kind", "material", "materialIndex", "depthCenter", "vertexOffset",
      "vertexCount", "color", "opacity", "roughness", "metalness",
    ]);
    hash = sceneCSSHashCollection(hash, bundle && bundle.points, [
      "id", "material", "materialIndex", "count", "color", "size", "opacity",
      "blendMode", "depthWrite", "x", "y", "z", "rotationX", "rotationY",
      "rotationZ", "spinX", "spinY", "spinZ",
    ]);
    hash = sceneCSSHashCollection(hash, bundle && bundle.instancedMeshes, [
      "id", "kind", "material", "materialIndex", "count", "color", "roughness",
      "metalness", "width", "height", "depth", "radius",
    ]);
    hash = sceneCSSHashCollection(hash, bundle && bundle.labels, [
      "id", "color", "background", "borderColor", "offsetX", "offsetY", "opacity",
    ]);
    hash = sceneCSSHashCollection(hash, bundle && bundle.sprites, [
      "id", "width", "height", "scale", "opacity", "offsetX", "offsetY",
    ]);
    hash = sceneCSSHashComputeParticles(hash, bundle && bundle.computeParticles);
    hash = sceneCSSHashCollection(hash, bundle && bundle.postEffects, [
      "kind", "threshold", "intensity", "radius", "scale", "saturation", "contrast", "exposure",
    ]);
    hash = sceneCSSHashCollection(hash, bundle && bundle.postFX, [
      "kind", "threshold", "intensity", "radius", "scale", "saturation", "contrast", "exposure",
    ]);
    return String(hash);
  }

  function sceneCSSHashCollection(hash, collection, keys) {
    if (!Array.isArray(collection)) {
      return scenePlannerHashNumber(hash, 0);
    }
    hash = scenePlannerHashNumber(hash, collection.length);
    for (let index = 0; index < collection.length; index += 1) {
      hash = sceneCSSHashRecordKeys(hash, collection[index], keys);
    }
    return hash;
  }

  function sceneCSSHashRecordKeys(hash, record, keys) {
    if (!record || typeof record !== "object") {
      return scenePlannerHashString(hash, "null");
    }
    for (let index = 0; index < keys.length; index += 1) {
      const key = keys[index];
      hash = scenePlannerHashString(hash, key);
      hash = scenePlannerHashAny(hash, record[key], 0);
    }
    return hash;
  }

  function sceneCSSHashComputeParticles(hash, collection) {
    if (!Array.isArray(collection)) {
      return scenePlannerHashNumber(hash, 0);
    }
    hash = scenePlannerHashNumber(hash, collection.length);
    for (let index = 0; index < collection.length; index += 1) {
      const record = collection[index] || {};
      hash = sceneCSSHashRecordKeys(hash, record, ["id", "kind", "count"]);
      hash = sceneCSSHashRecordKeys(hash, record.emitter, [
        "x", "y", "z", "rotationX", "rotationY", "rotationZ", "spinX", "spinY",
        "spinZ", "radius", "rate", "lifetime", "arms", "wind", "scatter",
      ]);
      hash = sceneCSSHashRecordKeys(hash, record.material, [
        "color", "colorEnd", "size", "sizeEnd", "opacity", "opacityEnd",
      ]);
    }
    return hash;
  }

  function sceneCSSApplyCachedResolution(source, cache) {
    const patches = cache && Array.isArray(cache.patches) ? cache.patches : [];
    if (!patches.length) {
      return {
        ir: source,
        dynamic: Boolean(cache && cache.dynamic),
        cache,
      };
    }
    const state = {
      source,
      out: source,
      dynamic: Boolean(cache.dynamic),
    };
    const recordGroups = typeof Map === "function" ? new Map() : null;
    for (let index = 0; index < patches.length; index += 1) {
      const patch = patches[index];
      if (patch && patch.kind === "record" && recordGroups) {
        const key = String(patch.collectionKey) + ":" + String(patch.index);
        if (!recordGroups.has(key)) {
          recordGroups.set(key, []);
        }
        recordGroups.get(key).push(patch);
      } else {
        sceneCSSApplyPatch(state, patch);
      }
    }
    if (recordGroups) {
      recordGroups.forEach(function(group) {
        sceneCSSApplyRecordPatchGroup(state, group, cache);
      });
    }
    return {
      ir: state.out,
      dynamic: Boolean(cache.dynamic),
      cache,
    };
  }

  function sceneCSSApplyRecordPatchGroup(state, patches, cache) {
    if (!Array.isArray(patches) || patches.length === 0) {
      return;
    }
    const first = patches[0];
    const collectionKey = first.collectionKey;
    const index = first.index;
    const sourceList = Array.isArray(state.source[collectionKey]) ? state.source[collectionKey] : [];
    const sourceRecord = sourceList[index];
    if (!sceneIsPlainObject(sourceRecord)) {
      for (let patchIndex = 0; patchIndex < patches.length; patchIndex += 1) {
        sceneCSSApplyPatch(state, patches[patchIndex]);
      }
      return;
    }
    const signature = sceneCSSRecordPatchSignature(patches);
    const existingCache = sourceRecord._sceneCSSPatchCache;
    if (
      existingCache &&
      existingCache.inputSignature === cache.inputSignature &&
      existingCache.revision === cache.revision &&
      existingCache.transitionFrame === cache.transitionFrame &&
      existingCache.signature === signature &&
      existingCache.value
    ) {
      const list = sceneCSSMutableArray(state, collectionKey);
      list[index] = existingCache.value;
      return;
    }
    const list = sceneCSSMutableArray(state, collectionKey);
    const next = Object.assign({}, sourceRecord);
    for (let patchIndex = 0; patchIndex < patches.length; patchIndex += 1) {
      const patch = patches[patchIndex];
      next[patch.key] = sceneCSSClonePatchValue(patch.value);
    }
    list[index] = next;
    sourceRecord._sceneCSSPatchCache = {
      inputSignature: cache.inputSignature,
      revision: cache.revision,
      transitionFrame: cache.transitionFrame,
      signature,
      value: next,
    };
  }

  function sceneCSSRecordPatchSignature(patches) {
    let signature = "";
    for (let index = 0; index < patches.length; index += 1) {
      const patch = patches[index];
      signature += String(patch.key) + "=" + String(patch.value) + ";";
    }
    return signature;
  }

  function sceneCSSApplyPatch(state, patch) {
    if (!patch || typeof patch !== "object") {
      return;
    }
    const value = sceneCSSClonePatchValue(patch.value);
    switch (patch.kind) {
      case "topObject":
        sceneCSSSetTopObjectKey(state, patch.objectKey, patch.key, value);
        break;
      case "topValue":
        sceneCSSSetTopValue(state, patch.key, value);
        break;
      case "record":
        sceneCSSSetRecordKey(state, patch.collectionKey, patch.index, patch.key, value);
        break;
      case "nested":
        sceneCSSSetNestedKey(state, patch.collectionKey, patch.index, patch.childKey, patch.key, value);
        break;
      default:
        break;
    }
  }

  function sceneCSSClonePatchValue(value) {
    if (Array.isArray(value)) {
      return value.map(sceneCSSClonePatchValue);
    }
    if (sceneIsPlainObject(value)) {
      return Object.assign({}, value);
    }
    return value;
  }

  function sceneCSSResolveExplicitVars(state, css) {
    sceneCSSResolveTopObjectKeys(state, css, "environment", [
      "ambientColor", "ambientIntensity", "skyColor", "skyIntensity",
      "groundColor", "groundIntensity", "exposure", "fogColor", "fogDensity",
    ], css.mount);
    sceneCSSResolveCollectionKeys(state, css, "materials", [
      "color", "opacity", "emissive", "roughness", "metalness",
      "normalMap", "roughnessMap", "metalnessMap", "emissiveMap",
    ], null);
    sceneCSSResolveCollectionKeys(state, css, "lights", [
      "color", "groundColor", "intensity", "x", "y", "z",
      "directionX", "directionY", "directionZ", "angle", "penumbra",
      "range", "decay", "shadowBias", "shadowSize",
    ], sceneCSSRecordElement);
    sceneCSSResolveCollectionKeys(state, css, "objects", [
      "color", "opacity", "emissive", "roughness", "metalness", "lineWidth",
      "x", "y", "z", "rotationX", "rotationY", "rotationZ",
      "spinX", "spinY", "spinZ",
    ], sceneCSSRecordElement);
    sceneCSSResolveCollectionKeys(state, css, "meshObjects", [
      "depthCenter", "vertexOffset", "vertexCount",
    ], sceneCSSRecordElement);
    sceneCSSResolveCollectionKeys(state, css, "points", [
      "color", "size", "opacity", "x", "y", "z",
      "rotationX", "rotationY", "rotationZ", "spinX", "spinY", "spinZ",
    ], sceneCSSRecordElement);
    sceneCSSResolveCollectionKeys(state, css, "instancedMeshes", [
      "color", "roughness", "metalness", "width", "height", "depth", "radius",
    ], sceneCSSRecordElement);
    sceneCSSResolveCollectionKeys(state, css, "labels", [
      "color", "background", "borderColor", "offsetX", "offsetY", "opacity",
    ], sceneCSSRecordElement);
    sceneCSSResolveCollectionKeys(state, css, "sprites", [
      "width", "height", "scale", "opacity", "offsetX", "offsetY",
    ], sceneCSSRecordElement);
    sceneCSSResolveCollectionKeys(state, css, "postEffects", [
      "threshold", "intensity", "radius", "scale", "saturation", "contrast", "exposure",
    ], null);
    sceneCSSResolveCollectionKeys(state, css, "postFX", [
      "threshold", "intensity", "radius", "scale", "saturation", "contrast", "exposure",
    ], null);
    sceneCSSResolveComputeParticleVars(state, css);
  }

  function sceneCSSResolveTopObjectKeys(state, css, objectKey, keys, element) {
    const bundle = state.out || state.source || {};
    const target = bundle && bundle[objectKey];
    if (!sceneIsPlainObject(target)) {
      return;
    }
    for (let index = 0; index < keys.length; index += 1) {
      const key = keys[index];
      const resolved = sceneCSSResolveVarValue(target[key], element || css.mount, css);
      if (!resolved.matched) {
        continue;
      }
      state.dynamic = true;
      if (resolved.hasValue) {
        if (sceneCSSMaybeTransitionValue(state, "topObject", objectKey, 0, key, resolved.value)) {
          continue;
        }
        sceneCSSSetTopObjectKey(state, objectKey, key, resolved.value);
      }
    }
  }

  function sceneCSSResolveCollectionKeys(state, css, collectionKey, keys, elementFn) {
    const collection = sceneCSSCurrentCollection(state, collectionKey);
    if (!Array.isArray(collection)) {
      return;
    }
    for (let index = 0; index < collection.length; index += 1) {
      const record = collection[index];
      if (!sceneIsPlainObject(record)) {
        continue;
      }
      const element = typeof elementFn === "function" ? elementFn(css, record) : css.mount;
      for (let keyIndex = 0; keyIndex < keys.length; keyIndex += 1) {
        const key = keys[keyIndex];
        const resolved = sceneCSSResolveVarValue(record[key], element, css);
        if (!resolved.matched) {
          continue;
        }
        state.dynamic = true;
        if (resolved.hasValue) {
          if (sceneCSSMaybeTransitionValue(state, "record", collectionKey, index, key, resolved.value)) {
            continue;
          }
          sceneCSSSetRecordKey(state, collectionKey, index, key, resolved.value);
        }
      }
    }
  }

  function sceneCSSResolveComputeParticleVars(state, css) {
    const collection = sceneCSSCurrentCollection(state, "computeParticles");
    if (!Array.isArray(collection)) {
      return;
    }
    const nested = [
      ["emitter", ["x", "y", "z", "rotationX", "rotationY", "rotationZ", "spinX", "spinY", "spinZ", "radius", "rate", "lifetime", "arms", "wind", "scatter"]],
      ["material", ["color", "colorEnd", "size", "sizeEnd", "opacity", "opacityEnd"]],
    ];
    for (let index = 0; index < collection.length; index += 1) {
      const record = collection[index];
      if (!sceneIsPlainObject(record)) {
        continue;
      }
      const element = sceneCSSRecordElement(css, record);
      for (let nestedIndex = 0; nestedIndex < nested.length; nestedIndex += 1) {
        const childKey = nested[nestedIndex][0];
        const keys = nested[nestedIndex][1];
        const child = sceneIsPlainObject(record[childKey]) ? record[childKey] : null;
        if (!child) {
          continue;
        }
        for (let keyIndex = 0; keyIndex < keys.length; keyIndex += 1) {
          const key = keys[keyIndex];
          const resolved = sceneCSSResolveVarValue(child[key], element, css);
          if (!resolved.matched) {
            continue;
          }
          state.dynamic = true;
          if (resolved.hasValue) {
            sceneCSSSetNestedKey(state, "computeParticles", index, childKey, key, resolved.value);
          }
        }
      }
    }
  }

  function sceneCSSApplyComputedDefaults(state, css) {
    if (!css.hasComputedStyle || !css.mount) {
      return;
    }
    sceneCSSApplyEnvironmentDefaults(state, css);
    sceneCSSApplySceneFilter(state, css);
    sceneCSSApplyLightDefaults(state, css);
    sceneCSSApplyNodeDefaults(state, css);
  }

  function sceneCSSApplyEnvironmentDefaults(state, css) {
    const mappings = [
      ["ambientColor", ["--scene-ambient-color"]],
      ["ambientIntensity", ["--scene-ambient-intensity"]],
      ["fogColor", ["--scene-fog-color"]],
      ["fogDensity", ["--scene-fog-density"]],
      ["skyColor", ["--scene-sky-color"]],
      ["skyIntensity", ["--scene-sky-intensity"]],
      ["groundColor", ["--scene-ground-color"]],
      ["groundIntensity", ["--scene-ground-intensity"]],
      ["exposure", ["--scene-exposure"]],
      ["toneMapping", ["--scene-tone-mapping"]],
    ];
    for (let index = 0; index < mappings.length; index += 1) {
      const key = mappings[index][0];
      const value = sceneCSSReadFirstProperty(css, css.mount, mappings[index][1]);
      if (value == null) {
        continue;
      }
      sceneCSSSetTopObjectKey(state, "environment", key, sceneCSSCoerceValue(value));
      state.dynamic = true;
    }
  }

  function sceneCSSApplySceneFilter(state, css) {
    const value = sceneCSSReadFirstProperty(css, css.mount, ["--scene-filter", "scene-filter"]);
    if (value == null) {
      return;
    }
    const effects = sceneParseSceneFilter(value);
    if (!effects || effects.length === 0) {
      return;
    }
    sceneCSSSetTopValue(state, "postEffects", effects);
    state.dynamic = true;
  }

  function sceneCSSApplyLightDefaults(state, css) {
    const lights = sceneCSSCurrentCollection(state, "lights");
    if (!Array.isArray(lights)) {
      return;
    }
    for (let index = 0; index < lights.length; index += 1) {
      const light = lights[index];
      if (!sceneIsPlainObject(light)) {
        continue;
      }
      const element = sceneCSSRecordElement(css, light);
      const directional = String(light.kind || "").toLowerCase() === "directional";
      const colorNames = directional
        ? ["--sun-color", "--scene-sun-color", "--light-color", "--scene-light-color"]
        : ["--light-color", "--scene-light-color"];
      const intensityNames = directional
        ? ["--sun-intensity", "--scene-sun-intensity", "--light-intensity", "--scene-light-intensity"]
        : ["--light-intensity", "--scene-light-intensity"];
      const color = sceneCSSReadFirstProperty(css, element, colorNames);
      if (color != null) {
        sceneCSSSetRecordKey(state, "lights", index, "color", color);
        state.dynamic = true;
      }
      const intensity = sceneCSSReadFirstProperty(css, element, intensityNames);
      if (intensity != null) {
        sceneCSSSetRecordKey(state, "lights", index, "intensity", sceneCSSCoerceValue(intensity));
        state.dynamic = true;
      }
    }
  }

  function sceneCSSApplyNodeDefaults(state, css) {
    sceneCSSApplyMeshCollectionDefaults(state, css, "meshObjects");
    sceneCSSApplyMeshCollectionDefaults(state, css, "objects");
    sceneCSSApplyPointDefaults(state, css);
    sceneCSSApplyInstancedDefaults(state, css);
    sceneCSSApplyComputeDefaults(state, css);
  }

  function sceneCSSApplyMeshCollectionDefaults(state, css, collectionKey) {
    const collection = sceneCSSCurrentCollection(state, collectionKey);
    if (!Array.isArray(collection)) {
      return;
    }
    for (let index = 0; index < collection.length; index += 1) {
      const record = collection[index];
      if (!sceneIsPlainObject(record)) {
        continue;
      }
      const element = sceneCSSRecordElement(css, record);
      const color = sceneCSSReadFirstProperty(css, element, ["--mesh-color"]);
      const roughness = sceneCSSReadFirstProperty(css, element, ["--mesh-roughness"]);
      const metalness = sceneCSSReadFirstProperty(css, element, ["--mesh-metalness"]);
      const opacity = sceneCSSReadFirstProperty(css, element, ["--mesh-opacity", "--material-opacity"]);
      if (color != null || roughness != null || metalness != null || opacity != null) {
        const material = sceneCSSMutableMaterialForRecord(state, record);
        if (material) {
          const materialIndex = sceneCSSMaterialIndexForRecord(record);
          if (color != null) sceneCSSSetRecordKey(state, "materials", materialIndex, "color", color);
          if (roughness != null) sceneCSSSetRecordKey(state, "materials", materialIndex, "roughness", sceneCSSCoerceValue(roughness));
          if (metalness != null) sceneCSSSetRecordKey(state, "materials", materialIndex, "metalness", sceneCSSCoerceValue(metalness));
          if (opacity != null) sceneCSSSetRecordKey(state, "materials", materialIndex, "opacity", sceneCSSCoerceValue(opacity));
        } else {
          if (color != null) sceneCSSSetRecordKey(state, collectionKey, index, "color", color);
          if (roughness != null) sceneCSSSetRecordKey(state, collectionKey, index, "roughness", sceneCSSCoerceValue(roughness));
          if (metalness != null) sceneCSSSetRecordKey(state, collectionKey, index, "metalness", sceneCSSCoerceValue(metalness));
          if (opacity != null) sceneCSSSetRecordKey(state, collectionKey, index, "opacity", sceneCSSCoerceValue(opacity));
        }
        state.dynamic = true;
      }
    }
  }

  function sceneCSSApplyPointDefaults(state, css) {
    const collection = sceneCSSCurrentCollection(state, "points");
    if (!Array.isArray(collection)) {
      return;
    }
    for (let index = 0; index < collection.length; index += 1) {
      const record = collection[index];
      if (!sceneIsPlainObject(record)) {
        continue;
      }
      const element = sceneCSSRecordElement(css, record);
      const color = sceneCSSReadFirstProperty(css, element, ["--point-color", "--mesh-color"]);
      const size = sceneCSSReadFirstProperty(css, element, ["--point-size"]);
      const opacity = sceneCSSReadFirstProperty(css, element, ["--point-opacity"]);
      if (color != null) {
        sceneCSSSetRecordKey(state, "points", index, "color", color);
        state.dynamic = true;
      }
      if (size != null) {
        sceneCSSSetRecordKey(state, "points", index, "size", sceneCSSCoerceValue(size));
        state.dynamic = true;
      }
      if (opacity != null) {
        sceneCSSSetRecordKey(state, "points", index, "opacity", sceneCSSCoerceValue(opacity));
        state.dynamic = true;
      }
    }
  }

  function sceneCSSApplyInstancedDefaults(state, css) {
    const collection = sceneCSSCurrentCollection(state, "instancedMeshes");
    if (!Array.isArray(collection)) {
      return;
    }
    for (let index = 0; index < collection.length; index += 1) {
      const record = collection[index];
      if (!sceneIsPlainObject(record)) {
        continue;
      }
      const element = sceneCSSRecordElement(css, record);
      const color = sceneCSSReadFirstProperty(css, element, ["--mesh-color"]);
      const roughness = sceneCSSReadFirstProperty(css, element, ["--mesh-roughness"]);
      const metalness = sceneCSSReadFirstProperty(css, element, ["--mesh-metalness"]);
      if (color != null) {
        sceneCSSSetRecordKey(state, "instancedMeshes", index, "color", color);
        state.dynamic = true;
      }
      if (roughness != null) {
        sceneCSSSetRecordKey(state, "instancedMeshes", index, "roughness", sceneCSSCoerceValue(roughness));
        state.dynamic = true;
      }
      if (metalness != null) {
        sceneCSSSetRecordKey(state, "instancedMeshes", index, "metalness", sceneCSSCoerceValue(metalness));
        state.dynamic = true;
      }
    }
  }

  function sceneCSSApplyComputeDefaults(state, css) {
    const collection = sceneCSSCurrentCollection(state, "computeParticles");
    if (!Array.isArray(collection)) {
      return;
    }
    for (let index = 0; index < collection.length; index += 1) {
      const record = collection[index];
      if (!sceneIsPlainObject(record)) {
        continue;
      }
      const element = sceneCSSRecordElement(css, record);
      const wind = sceneCSSReadFirstProperty(css, element, ["--scene-particle-wind"]);
      if (wind != null) {
        sceneCSSSetNestedKey(state, "computeParticles", index, "emitter", "wind", sceneCSSCoerceValue(wind));
        state.dynamic = true;
      }
      const color = sceneCSSReadFirstProperty(css, element, ["--point-color", "--mesh-color"]);
      const size = sceneCSSReadFirstProperty(css, element, ["--point-size"]);
      const opacity = sceneCSSReadFirstProperty(css, element, ["--point-opacity"]);
      if (color != null || size != null || opacity != null) {
        if (color != null) sceneCSSSetNestedKey(state, "computeParticles", index, "material", "color", color);
        if (size != null) sceneCSSSetNestedKey(state, "computeParticles", index, "material", "size", sceneCSSCoerceValue(size));
        if (opacity != null) sceneCSSSetNestedKey(state, "computeParticles", index, "material", "opacity", sceneCSSCoerceValue(opacity));
        state.dynamic = true;
      }
    }
  }

  function sceneCSSCurrentCollection(state, key) {
    const bundle = state.out || state.source || {};
    return bundle[key];
  }

  function sceneCSSRecordElement(css, record) {
    if (!record || !record.id) {
      return css.mount;
    }
    const id = String(record.id);
    const sentinels = css.sentinels;
    if (!sentinels) {
      return css.mount;
    }
    if (typeof sentinels.get === "function") {
      return sentinels.get(id) || css.mount;
    }
    return sentinels[id] || css.mount;
  }

  function sceneCSSResolveVarValue(value, element, css) {
    if (typeof value !== "string") {
      return { matched: false, hasValue: false, value };
    }
    const parsed = sceneCSSParseVarExpression(value);
    if (!parsed) {
      return { matched: false, hasValue: false, value };
    }
    const computed = sceneCSSReadProperty(css, element, parsed.name);
    const text = computed != null && computed !== "" ? computed : parsed.fallback;
    if (text == null || String(text).trim() === "") {
      return { matched: true, hasValue: false, value };
    }
    return {
      matched: true,
      hasValue: true,
      value: sceneCSSCoerceValue(text),
    };
  }

  function sceneCSSParseVarExpression(value) {
    const text = String(value || "").trim();
    const match = /^var\(\s*(--[-_a-zA-Z0-9]+)\s*(?:,\s*([\s\S]*?))?\s*\)$/.exec(text);
    if (!match) {
      return null;
    }
    return {
      name: match[1],
      fallback: match[2] == null ? null : match[2].trim(),
    };
  }

  function sceneCSSReadFirstProperty(css, element, names) {
    for (let index = 0; index < names.length; index += 1) {
      const value = sceneCSSReadProperty(css, element, names[index]);
      if (value != null && value !== "") {
        return value;
      }
    }
    return null;
  }

  function sceneCSSReadProperty(css, element, name) {
    if (!css || !css.hasComputedStyle || !name) {
      return null;
    }
    const primary = sceneCSSReadPropertyOnElement(css, element || css.mount, name);
    if (primary != null && primary !== "") {
      return primary;
    }
    if (element && element !== css.mount) {
      return sceneCSSReadPropertyOnElement(css, css.mount, name);
    }
    return primary;
  }

  function sceneCSSReadPropertyOnElement(css, element, name) {
    const style = sceneCSSComputedStyle(css, element);
    if (!style) {
      return null;
    }
    let value = "";
    if (typeof style.getPropertyValue === "function") {
      value = style.getPropertyValue(name);
    } else if (Object.prototype.hasOwnProperty.call(style, name)) {
      value = style[name];
    }
    const text = String(value == null ? "" : value).trim();
    return text === "" ? null : text;
  }

  function sceneCSSComputedStyle(css, element) {
    if (!element || !css.hasComputedStyle) {
      return null;
    }
    if (css.styles && css.styles.has(element)) {
      return css.styles.get(element);
    }
    let style = null;
    try {
      style = window.getComputedStyle(element);
    } catch (_error) {
      style = null;
    }
    if (css.styles) {
      css.styles.set(element, style);
    }
    return style;
  }

  function sceneCSSCoerceValue(value) {
    const text = String(value == null ? "" : value).trim();
    if (text === "") {
      return "";
    }
    if (/^[-+]?(?:\d+\.?\d*|\.\d+)(?:e[-+]?\d+)?$/i.test(text)) {
      const number = Number(text);
      if (Number.isFinite(number)) {
        return number;
      }
    }
    return text;
  }

  function sceneCSSSameValue(left, right) {
    return left === right || String(left) === String(right);
  }

  function sceneCSSMutableBundle(state) {
    if (state.out === state.source) {
      state.out = Object.assign({}, state.source);
    }
    return state.out;
  }

  function sceneCSSMutableTopObject(state, key) {
    const bundle = sceneCSSMutableBundle(state);
    const current = sceneIsPlainObject(bundle[key]) ? bundle[key] : {};
    if (current === state.source[key]) {
      bundle[key] = Object.assign({}, current);
    } else if (!sceneIsPlainObject(bundle[key])) {
      bundle[key] = {};
    }
    return bundle[key];
  }

  function sceneCSSSetTopObjectKey(state, objectKey, key, value) {
    const current = state.out && state.out[objectKey];
    const oldValue = current && current[key];
    if (sceneCSSSameValue(oldValue, value)) {
      return false;
    }
    const object = sceneCSSMutableTopObject(state, objectKey);
    object[key] = value;
    sceneCSSRecordPatch(state, { kind: "topObject", objectKey, key, value });
    return true;
  }

  function sceneCSSSetTopValue(state, key, value) {
    const bundle = state.out || state.source || {};
    if (bundle[key] === value) {
      return false;
    }
    const mutable = sceneCSSMutableBundle(state);
    mutable[key] = value;
    sceneCSSRecordPatch(state, { kind: "topValue", key, value });
    return true;
  }

  function sceneCSSMutableArray(state, key) {
    const bundle = sceneCSSMutableBundle(state);
    const current = Array.isArray(bundle[key]) ? bundle[key] : [];
    if (current === state.source[key]) {
      bundle[key] = current.slice();
    } else if (!Array.isArray(bundle[key])) {
      bundle[key] = [];
    }
    return bundle[key];
  }

  function sceneCSSMutableRecord(state, key, index) {
    const list = sceneCSSMutableArray(state, key);
    const current = sceneIsPlainObject(list[index]) ? list[index] : {};
    const sourceList = Array.isArray(state.source[key]) ? state.source[key] : [];
    if (current === sourceList[index]) {
      list[index] = Object.assign({}, current);
    } else if (!sceneIsPlainObject(list[index])) {
      list[index] = {};
    }
    return list[index];
  }

  function sceneCSSSetRecordKey(state, collectionKey, index, key, value) {
    const list = sceneCSSCurrentCollection(state, collectionKey);
    const record = Array.isArray(list) ? list[index] : null;
    if (record && sceneCSSSameValue(record[key], value)) {
      return false;
    }
    const mutable = sceneCSSMutableRecord(state, collectionKey, index);
    mutable[key] = value;
    sceneCSSRecordPatch(state, { kind: "record", collectionKey, index, key, value });
    return true;
  }

  function sceneCSSSetNestedKey(state, collectionKey, index, childKey, key, value) {
    const list = sceneCSSCurrentCollection(state, collectionKey);
    const record = Array.isArray(list) ? list[index] : null;
    const child = record && sceneIsPlainObject(record[childKey]) ? record[childKey] : null;
    if (child && sceneCSSSameValue(child[key], value)) {
      return false;
    }
    const mutable = sceneCSSMutableNestedObject(state, collectionKey, index, childKey);
    mutable[key] = value;
    sceneCSSRecordPatch(state, { kind: "nested", collectionKey, index, childKey, key, value });
    return true;
  }

  function sceneCSSMutableNestedObject(state, collectionKey, index, childKey) {
    const record = sceneCSSMutableRecord(state, collectionKey, index);
    const current = sceneIsPlainObject(record[childKey]) ? record[childKey] : {};
    const sourceList = Array.isArray(state.source[collectionKey]) ? state.source[collectionKey] : [];
    const sourceRecord = sourceList[index];
    if (sourceRecord && current === sourceRecord[childKey]) {
      record[childKey] = Object.assign({}, current);
    } else if (!sceneIsPlainObject(record[childKey])) {
      record[childKey] = {};
    }
    return record[childKey];
  }

  function sceneCSSMutableMaterialForRecord(state, record) {
    const materialIndex = sceneCSSMaterialIndexForRecord(record);
    const materials = sceneCSSCurrentCollection(state, "materials");
    if (!Array.isArray(materials) || materialIndex < 0 || materialIndex >= materials.length) {
      return null;
    }
    return sceneCSSMutableRecord(state, "materials", materialIndex);
  }

  function sceneCSSMaterialIndexForRecord(record) {
    return Math.floor(sceneNumber(record && record.materialIndex, -1));
  }

  function sceneCSSRecordPatch(state, patch) {
    if (!state || !Array.isArray(state.patches) || !patch) {
      return;
    }
    state.patches.push(Object.assign({}, patch, {
      value: sceneCSSClonePatchValue(patch.value),
    }));
  }

  function sceneParseSceneFilter(value) {
    const text = String(value || "").trim();
    if (!text || text === "none") {
      return [];
    }
    const effects = [];
    const fnPattern = /([a-zA-Z][-_a-zA-Z0-9]*)\s*\(([^)]*)\)/g;
    let match;
    while ((match = fnPattern.exec(text))) {
      const kind = sceneNormalizeSceneFilterKind(match[1]);
      if (!kind) {
        continue;
      }
      const effect = { kind };
      const body = String(match[2] || "").replace(/[,:=]/g, " ");
      const parts = body.split(/\s+/).filter(Boolean);
      for (let index = 0; index + 1 < parts.length; index += 2) {
        const key = sceneNormalizeSceneFilterKey(parts[index]);
        const valueText = parts[index + 1];
        if (!key) {
          continue;
        }
        const number = Number(valueText);
        if (Number.isFinite(number)) {
          effect[key] = number;
        }
      }
      effects.push(effect);
    }
    return effects;
  }

  function sceneNormalizeSceneFilterKind(kind) {
    const text = String(kind || "").trim().toLowerCase();
    if (text === "bloom" || text === "vignette") {
      return text;
    }
    if (text === "color-grade" || text === "color-grading" || text === "colorgrade") {
      return "color-grade";
    }
    return "";
  }

  function sceneNormalizeSceneFilterKey(key) {
    const text = String(key || "").trim().toLowerCase();
    switch (text) {
      case "threshold":
      case "intensity":
      case "radius":
      case "scale":
      case "saturation":
      case "contrast":
      case "exposure":
        return text;
      default:
        return "";
    }
  }

  function prepareScenePBRPasses(bundle) {
    const objects = Array.isArray(bundle && bundle.meshObjects) ? bundle.meshObjects : [];
    const materials = Array.isArray(bundle && bundle.materials) ? bundle.materials : [];
    const opaque = [];
    const alpha = [];
    const additive = [];

    for (let index = 0; index < objects.length; index += 1) {
      const object = objects[index];
      if (!scenePlannerRenderableObject(object)) {
        continue;
      }
      const material = materials[object.materialIndex] || null;
      const pass = typeof scenePBRObjectRenderPass === "function"
        ? scenePBRObjectRenderPass(object, material)
        : scenePlannerObjectRenderPass(object, material);
      if (pass === "alpha") {
        alpha.push(object);
      } else if (pass === "additive") {
        additive.push(object);
      } else {
        opaque.push(object);
      }
    }

    if (typeof scenePBRDepthSort === "function") {
      alpha.sort(scenePBRDepthSort);
      additive.sort(scenePBRDepthSort);
    } else {
      alpha.sort(scenePlannerDepthSort);
      additive.sort(scenePlannerDepthSort);
    }

    return { opaque, alpha, additive };
  }

  function scenePreparedPassList(worldDrawPlan, pbrPasses) {
    const passes = [];
    if (worldDrawPlan) {
      if (worldDrawPlan.staticOpaqueVertexCount > 0) {
        passes.push({ name: "staticOpaque", kind: "lines", blend: "opaque", depth: "opaque", vertexCount: worldDrawPlan.staticOpaqueVertexCount });
      }
      if (worldDrawPlan.dynamicOpaqueVertexCount > 0) {
        passes.push({ name: "dynamicOpaque", kind: "lines", blend: "opaque", depth: "opaque", vertexCount: worldDrawPlan.dynamicOpaqueVertexCount });
      }
      if (worldDrawPlan.alphaVertexCount > 0) {
        passes.push({ name: "alpha", kind: "lines", blend: "alpha", depth: "translucent", vertexCount: worldDrawPlan.alphaVertexCount });
      }
      if (worldDrawPlan.additiveVertexCount > 0) {
        passes.push({ name: "additive", kind: "lines", blend: "additive", depth: "translucent", vertexCount: worldDrawPlan.additiveVertexCount });
      }
    }
    if (pbrPasses) {
      if (pbrPasses.opaque.length > 0) {
        passes.push({ name: "pbrOpaque", kind: "mesh", blend: "opaque", depth: "opaque", count: pbrPasses.opaque.length });
      }
      if (pbrPasses.alpha.length > 0) {
        passes.push({ name: "pbrAlpha", kind: "mesh", blend: "alpha", depth: "translucent", count: pbrPasses.alpha.length });
      }
      if (pbrPasses.additive.length > 0) {
        passes.push({ name: "pbrAdditive", kind: "mesh", blend: "additive", depth: "translucent", count: pbrPasses.additive.length });
      }
    }
    return passes;
  }

  function sceneCachedBuffer(cacheOwner, typedArray, allocFn, uploadFn, options) {
    if (!typedArray) {
      return null;
    }
    const opts = options || {};
    if (opts.slot) {
      return sceneCachedSlotBuffer(cacheOwner, opts.slot, typedArray, allocFn, uploadFn, opts);
    }
    if (!cacheOwner || typeof cacheOwner.get !== "function" || typeof cacheOwner.set !== "function") {
      return sceneCachedSlotBuffer(cacheOwner || {}, "_sceneCachedBuffer", typedArray, allocFn, uploadFn, opts);
    }
    let handle = cacheOwner.get(typedArray);
    if (handle) {
      return handle;
    }
    handle = allocFn(typedArray);
    uploadFn(handle, typedArray, null);
    cacheOwner.set(typedArray, handle);
    return handle;
  }

  function scenePreparedCommandSequence(prepared) {
    const scene = prepared && prepared.ir ? prepared.ir : {};
    const commands = [];
    const pbrPasses = prepared && prepared.pbrPasses;
    if (pbrPasses) {
      sceneAppendPreparedMeshCommands(commands, "opaque", pbrPasses.opaque);
      sceneAppendPreparedMeshCommands(commands, "alpha", pbrPasses.alpha);
      sceneAppendPreparedMeshCommands(commands, "additive", pbrPasses.additive);
    }
    if (prepared && prepared.worldDrawPlan) {
      sceneAppendPreparedLineCommand(commands, "staticOpaque", prepared.worldDrawPlan.staticOpaqueVertexCount);
      sceneAppendPreparedLineCommand(commands, "dynamicOpaque", prepared.worldDrawPlan.dynamicOpaqueVertexCount);
      sceneAppendPreparedLineCommand(commands, "alpha", prepared.worldDrawPlan.alphaVertexCount);
      sceneAppendPreparedLineCommand(commands, "additive", prepared.worldDrawPlan.additiveVertexCount);
    }
    const points = Array.isArray(scene.points) ? scene.points : [];
    for (let index = 0; index < points.length; index += 1) {
      const entry = points[index];
      commands.push({
        op: "drawPoints",
        id: entry && entry.id || "",
        count: Math.max(0, Math.floor(sceneNumber(entry && entry.count, 0))),
      });
    }
    const instanced = Array.isArray(scene.instancedMeshes) ? scene.instancedMeshes : [];
    for (let index = 0; index < instanced.length; index += 1) {
      const entry = instanced[index];
      commands.push({
        op: "drawInstancedMesh",
        id: entry && entry.id || "",
        kind: entry && entry.kind || "",
        count: Math.max(0, Math.floor(sceneNumber(entry && entry.count, 0))),
      });
    }
    return commands;
  }

  function sceneAppendPreparedMeshCommands(commands, pass, objects) {
    const list = Array.isArray(objects) ? objects : [];
    for (let index = 0; index < list.length; index += 1) {
      const object = list[index];
      commands.push({
        op: "drawMesh",
        pass,
        id: object && object.id || "",
        kind: object && object.kind || "",
        vertexOffset: Math.max(0, Math.floor(sceneNumber(object && object.vertexOffset, 0))),
        vertexCount: Math.max(0, Math.floor(sceneNumber(object && object.vertexCount, 0))),
      });
    }
  }

  function sceneAppendPreparedLineCommand(commands, pass, vertexCount) {
    const count = Math.max(0, Math.floor(sceneNumber(vertexCount, 0)));
    if (count > 0) {
      commands.push({ op: "drawLines", pass, vertexCount: count });
    }
  }

  function sceneCachedSlotBuffer(owner, slot, typedArray, allocFn, uploadFn, options) {
    if (!owner) {
      return null;
    }
    const bytesKey = slot + "Bytes";
    const sourceKey = slot + "Source";
    let handle = owner[slot];
    if (!handle) {
      handle = allocFn(typedArray);
      owner[slot] = handle;
      owner[bytesKey] = 0;
      owner[sourceKey] = null;
    }
    const sourceChanged = owner[sourceKey] !== typedArray;
    const bytesChanged = owner[bytesKey] !== typedArray.byteLength;
    if (options && options.dynamic || sourceChanged || bytesChanged) {
      const replacement = uploadFn(handle, typedArray, {
        sourceChanged,
        bytesChanged,
        previousBytes: owner[bytesKey] || 0,
      });
      if (replacement && replacement !== handle) {
        handle = replacement;
        owner[slot] = handle;
      }
      owner[bytesKey] = typedArray.byteLength;
      owner[sourceKey] = typedArray;
    }
    return handle;
  }

  function scenePreparedSignature(bundle, camera, viewport) {
    let hash = 2166136261 >>> 0;
    hash = scenePlannerHashString(hash, "v");
    hash = scenePlannerHashNumber(hash, sceneNumber(bundle && (bundle.bundleVersion != null ? bundle.bundleVersion : bundle.version), 0));
    hash = scenePlannerHashCamera(hash, camera);
    hash = scenePlannerHashViewport(hash, viewport);
    hash = scenePlannerHashCollection(hash, bundle && bundle.meshObjects, scenePlannerHashMeshObject);
    hash = scenePlannerHashCollection(hash, bundle && bundle.objects, scenePlannerHashLineObject);
    hash = scenePlannerHashCollection(hash, bundle && bundle.materials, scenePlannerHashMaterial);
    hash = scenePlannerHashCollection(hash, bundle && bundle.lights, scenePlannerHashLight);
    hash = scenePlannerHashCollection(hash, bundle && bundle.points, scenePlannerHashPointsEntry);
    hash = scenePlannerHashCollection(hash, bundle && bundle.instancedMeshes, scenePlannerHashInstancedEntry);
    hash = scenePlannerHashCollection(hash, bundle && bundle.computeParticles, scenePlannerHashComputeEntry);
    hash = scenePlannerHashNumber(hash, arrayLength(bundle && bundle.worldPositions));
    hash = scenePlannerHashNumber(hash, arrayLength(bundle && bundle.worldColors));
    hash = scenePlannerHashArrayShape(hash, bundle && bundle.worldLineWidths);
    hash = scenePlannerHashArrayShape(hash, bundle && bundle.worldLinePasses);
    hash = scenePlannerHashNumber(hash, arrayLength(bundle && bundle.worldMeshPositions));
    hash = scenePlannerHashNumber(hash, arrayLength(bundle && bundle.worldMeshNormals));
    return String(hash);
  }

  function scenePlannerMaterialsHash(bundle) {
    let hash = 2166136261 >>> 0;
    return String(scenePlannerHashCollection(hash, bundle && bundle.materials, scenePlannerHashMaterial));
  }

  function scenePlannerShadowHash(bundle) {
    let hash = 2166136261 >>> 0;
    hash = scenePlannerHashCollection(hash, bundle && bundle.lights, scenePlannerHashLight);
    hash = scenePlannerHashCollection(hash, bundle && bundle.meshObjects, function(next, object) {
      if (!object || !object.castShadow) {
        return next;
      }
      return scenePlannerHashMeshObject(next, object);
    });
    return String(hash);
  }

  function scenePlannerRenderableObject(object) {
    return Boolean(
      object &&
      !object.viewCulled &&
      Number.isFinite(object.vertexOffset) &&
      Number.isFinite(object.vertexCount) &&
      object.vertexCount > 0
    );
  }

  function scenePlannerObjectRenderPass(object, material) {
    const objectPass = object && typeof object.renderPass === "string" ? object.renderPass.toLowerCase() : "";
    if (objectPass === "opaque" || objectPass === "alpha" || objectPass === "additive") {
      return objectPass;
    }
    const materialPass = material && typeof material.renderPass === "string" ? material.renderPass.toLowerCase() : "";
    if (materialPass === "opaque" || materialPass === "alpha" || materialPass === "additive") {
      return materialPass;
    }
    if (material && sceneNumber(material.opacity, 1) < 1) {
      return "alpha";
    }
    return "opaque";
  }

  function scenePlannerDepthSort(a, b) {
    const da = sceneNumber(a && a.depthCenter, 0);
    const db = sceneNumber(b && b.depthCenter, 0);
    if (da !== db) {
      return db - da;
    }
    return String(a && a.id || "").localeCompare(String(b && b.id || ""));
  }

  function scenePlannerHashCamera(hash, camera) {
    hash = scenePlannerHashNumber(hash, sceneNumber(camera && camera.x, 0));
    hash = scenePlannerHashNumber(hash, sceneNumber(camera && camera.y, 0));
    hash = scenePlannerHashNumber(hash, sceneNumber(camera && camera.z, 0));
    hash = scenePlannerHashNumber(hash, sceneNumber(camera && camera.rotationX, 0));
    hash = scenePlannerHashNumber(hash, sceneNumber(camera && camera.rotationY, 0));
    hash = scenePlannerHashNumber(hash, sceneNumber(camera && camera.rotationZ, 0));
    hash = scenePlannerHashNumber(hash, sceneNumber(camera && camera.fov, 0));
    hash = scenePlannerHashNumber(hash, sceneNumber(camera && camera.near, 0));
    return scenePlannerHashNumber(hash, sceneNumber(camera && camera.far, 0));
  }

  function scenePlannerHashViewport(hash, viewport) {
    hash = scenePlannerHashNumber(hash, sceneNumber(viewport && viewport.cssWidth, 0));
    hash = scenePlannerHashNumber(hash, sceneNumber(viewport && viewport.cssHeight, 0));
    hash = scenePlannerHashNumber(hash, sceneNumber(viewport && viewport.pixelWidth, 0));
    hash = scenePlannerHashNumber(hash, sceneNumber(viewport && viewport.pixelHeight, 0));
    return scenePlannerHashNumber(hash, sceneNumber(viewport && viewport.pixelRatio, 0));
  }

  function scenePlannerHashCollection(hash, collection, itemFn) {
    if (!Array.isArray(collection)) {
      return scenePlannerHashNumber(hash, 0);
    }
    hash = scenePlannerHashNumber(hash, collection.length);
    for (let index = 0; index < collection.length; index += 1) {
      hash = itemFn(hash, collection[index]);
    }
    return hash;
  }

  function scenePlannerHashMeshObject(hash, object) {
    hash = scenePlannerHashString(hash, object && object.id || "");
    hash = scenePlannerHashString(hash, object && object.kind || "");
    hash = scenePlannerHashNumber(hash, sceneNumber(object && object.materialIndex, 0));
    hash = scenePlannerHashNumber(hash, sceneNumber(object && object.vertexOffset, 0));
    hash = scenePlannerHashNumber(hash, sceneNumber(object && object.vertexCount, 0));
    hash = scenePlannerHashNumber(hash, sceneNumber(object && object.depthCenter, 0));
    hash = scenePlannerHashNumber(hash, object && object.viewCulled ? 1 : 0);
    hash = scenePlannerHashNumber(hash, object && object.castShadow ? 1 : 0);
    return scenePlannerHashNumber(hash, object && object.receiveShadow ? 1 : 0);
  }

  function scenePlannerHashLineObject(hash, object) {
    hash = scenePlannerHashString(hash, object && object.id || "");
    hash = scenePlannerHashString(hash, object && object.kind || "");
    hash = scenePlannerHashNumber(hash, sceneNumber(object && object.materialIndex, 0));
    hash = scenePlannerHashNumber(hash, sceneNumber(object && object.vertexOffset, 0));
    hash = scenePlannerHashNumber(hash, sceneNumber(object && object.vertexCount, 0));
    hash = scenePlannerHashNumber(hash, sceneNumber(object && object.depthCenter, 0));
    hash = scenePlannerHashNumber(hash, object && object.static ? 1 : 0);
    return scenePlannerHashNumber(hash, object && object.viewCulled ? 1 : 0);
  }

  function scenePlannerHashMaterial(hash, material) {
    // Historical shortcut: if a material has a stable `key`, hash only the key
    // to avoid churn on unrelated fields. But that also excluded `color`/
    // `opacity`/`emissive`/`roughness`/`metalness`/`texture` from the hash —
    // which is wrong when CSS var resolution rewrites those fields per-bucket
    // (m31labs palette swap: every 30 min, new hex colors resolved from
    // --galaxy-* vars). Downstream the prepared-scene signature matched and
    // `lastPrepared.passes` was reused with the previous bucket's baked
    // colors, leaving the canvas visually frozen despite the IR updating.
    // Hash the identity prefix AND the resolved appearance so signature
    // invalidation catches palette rewrites on keyed materials too.
    if (material && material.key) {
      hash = scenePlannerHashString(hash, material.key);
    }
    hash = scenePlannerHashString(hash, material && material.kind || "");
    hash = scenePlannerHashString(hash, material && material.color || "");
    hash = scenePlannerHashString(hash, material && material.texture || "");
    hash = scenePlannerHashString(hash, material && material.blendMode || "");
    hash = scenePlannerHashString(hash, material && material.renderPass || "");
    hash = scenePlannerHashNumber(hash, sceneNumber(material && material.opacity, 1));
    hash = scenePlannerHashNumber(hash, sceneNumber(material && material.emissive, 0));
    hash = scenePlannerHashNumber(hash, sceneNumber(material && material.roughness, 0));
    hash = scenePlannerHashNumber(hash, sceneNumber(material && material.metalness, 0));
    return scenePlannerHashNumber(hash, material && material.wireframe ? 1 : 0);
  }

  function scenePlannerHashLight(hash, light) {
    hash = scenePlannerHashString(hash, light && light.id || "");
    hash = scenePlannerHashString(hash, light && light.kind || "");
    hash = scenePlannerHashString(hash, light && light.color || "");
    hash = scenePlannerHashNumber(hash, sceneNumber(light && light.intensity, 0));
    hash = scenePlannerHashNumber(hash, sceneNumber(light && light.x, 0));
    hash = scenePlannerHashNumber(hash, sceneNumber(light && light.y, 0));
    hash = scenePlannerHashNumber(hash, sceneNumber(light && light.z, 0));
    hash = scenePlannerHashNumber(hash, sceneNumber(light && light.directionX, 0));
    hash = scenePlannerHashNumber(hash, sceneNumber(light && light.directionY, 0));
    hash = scenePlannerHashNumber(hash, sceneNumber(light && light.directionZ, 0));
    hash = scenePlannerHashNumber(hash, light && light.castShadow ? 1 : 0);
    return scenePlannerHashNumber(hash, sceneNumber(light && light.shadowSize, 0));
  }

  function scenePlannerHashPointsEntry(hash, entry) {
    hash = scenePlannerHashString(hash, entry && entry.id || "");
    hash = scenePlannerHashString(hash, entry && entry.material || "");
    hash = scenePlannerHashNumber(hash, sceneNumber(entry && entry.count, 0));
    hash = scenePlannerHashNumber(hash, sceneNumber(entry && entry.materialIndex, 0));
    hash = scenePlannerHashString(hash, entry && entry.blendMode || "");
    hash = scenePlannerHashNumber(hash, entry && entry.depthWrite === false ? 0 : 1);
    return scenePlannerHashNumber(hash, entry && entry.viewCulled ? 1 : 0);
  }

  function scenePlannerHashInstancedEntry(hash, entry) {
    hash = scenePlannerHashString(hash, entry && entry.id || "");
    hash = scenePlannerHashString(hash, entry && entry.kind || "");
    hash = scenePlannerHashString(hash, entry && entry.material || "");
    hash = scenePlannerHashNumber(hash, sceneNumber(entry && entry.count, 0));
    hash = scenePlannerHashNumber(hash, sceneNumber(entry && entry.materialIndex, 0));
    hash = scenePlannerHashString(hash, entry && entry.blendMode || "");
    return scenePlannerHashNumber(hash, entry && entry.depthWrite === false ? 0 : 1);
  }

  function scenePlannerHashComputeEntry(hash, entry) {
    hash = scenePlannerHashString(hash, entry && entry.id || "");
    hash = scenePlannerHashNumber(hash, sceneNumber(entry && entry.count, 0));
    hash = scenePlannerHashString(hash, entry && entry.kind || "");
    return scenePlannerHashString(hash, entry && entry.material && entry.material.blendMode || "");
  }

  function scenePlannerHashArrayShape(hash, value) {
    hash = scenePlannerHashNumber(hash, arrayLength(value));
    if (value && typeof value === "object") {
      hash = scenePlannerHashString(hash, Array.isArray(value) ? "array" : "typed");
    }
    return hash;
  }

  function scenePlannerHashAny(hash, value, depth) {
    if (value == null) {
      return scenePlannerHashString(hash, "null");
    }
    const valueType = typeof value;
    if (valueType === "number") {
      return scenePlannerHashNumber(hash, value);
    }
    if (valueType === "string" || valueType === "boolean") {
      return scenePlannerHashString(hash, String(value));
    }
    if (valueType !== "object" || depth > 5) {
      return scenePlannerHashString(hash, valueType);
    }
    if (typeof ArrayBuffer !== "undefined" && ArrayBuffer.isView && ArrayBuffer.isView(value)) {
      return scenePlannerHashArraySample(hash, value);
    }
    if (Array.isArray(value)) {
      hash = scenePlannerHashString(hash, "array");
      hash = scenePlannerHashNumber(hash, value.length);
      const length = value.length;
      if (length <= 64) {
        for (let index = 0; index < length; index += 1) {
          hash = scenePlannerHashAny(hash, value[index], depth + 1);
        }
      } else {
        for (let index = 0; index < 16; index += 1) {
          hash = scenePlannerHashAny(hash, value[index], depth + 1);
        }
        hash = scenePlannerHashAny(hash, value[Math.floor(length / 2)], depth + 1);
        for (let index = Math.max(16, length - 16); index < length; index += 1) {
          hash = scenePlannerHashAny(hash, value[index], depth + 1);
        }
      }
      return hash;
    }
    const keys = Object.keys(value).filter(function(key) {
      return key && key.charAt(0) !== "_" && typeof value[key] !== "function";
    }).sort();
    hash = scenePlannerHashString(hash, "object");
    hash = scenePlannerHashNumber(hash, keys.length);
    for (let index = 0; index < keys.length; index += 1) {
      const key = keys[index];
      hash = scenePlannerHashString(hash, key);
      hash = scenePlannerHashAny(hash, value[key], depth + 1);
    }
    return hash;
  }

  function scenePlannerHashArraySample(hash, value) {
    const length = arrayLength(value);
    hash = scenePlannerHashString(hash, "typed");
    hash = scenePlannerHashNumber(hash, length);
    if (length === 0) {
      return hash;
    }
    const step = Math.max(1, Math.floor(length / 32));
    for (let index = 0; index < length; index += step) {
      hash = scenePlannerHashNumber(hash, sceneNumber(value[index], 0));
    }
    if ((length - 1) % step !== 0) {
      hash = scenePlannerHashNumber(hash, sceneNumber(value[length - 1], 0));
    }
    return hash;
  }

  function scenePlannerHashNumber(hash, value) {
    const scaled = Math.round(sceneNumber(value, 0) * 1000);
    hash ^= scaled;
    return Math.imul(hash, 16777619) >>> 0;
  }

  function scenePlannerHashString(hash, value) {
    const text = String(value || "");
    for (let i = 0; i < text.length; i += 1) {
      hash ^= text.charCodeAt(i);
      hash = Math.imul(hash, 16777619) >>> 0;
    }
    return hash;
  }

  function arrayLength(value) {
    return value && typeof value.length === "number" ? value.length : 0;
  }
