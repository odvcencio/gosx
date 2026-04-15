  // Scene planner — backend-agnostic preparation of render bundles into pass
  // buckets, cache keys, and shared upload-cache helpers.

  function prepareScene(ir, camera, viewport, lastPrepared, cssContext) {
    const initialSource = ir && typeof ir === "object" ? ir : {};
    const cssResolved = sceneResolveCSSBundle(initialSource, cssContext);
    const source = cssResolved.ir;
    const resolvedCamera = camera || source.camera || {};
    const signature = scenePreparedSignature(source, resolvedCamera, viewport);
    if (lastPrepared && lastPrepared.signature === signature) {
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
      rebuilds: lastPrepared ? lastPrepared.rebuilds + 1 : 1,
    };
    return prepared;
  }

  function sceneResolveCSSBundle(source, cssContext) {
    const css = sceneCSSResolverContext(cssContext);
    const state = {
      source,
      out: source,
      dynamic: false,
    };
    sceneCSSResolveExplicitVars(state, css);
    sceneCSSApplyComputedDefaults(state, css);
    return {
      ir: state.out,
      dynamic: state.dynamic,
    };
  }

  function sceneCSSResolverContext(input) {
    const context = input && typeof input === "object" ? input : {};
    const mount = context.mount && typeof context.mount === "object" ? context.mount : null;
    const sentinels = context.sentinels || (mount && mount.__gosxScene3DSentinels) || null;
    const win = typeof window !== "undefined" ? window : null;
    return {
      mount,
      sentinels,
      styles: typeof Map === "function" ? new Map() : null,
      hasComputedStyle: Boolean(win && typeof win.getComputedStyle === "function"),
    };
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
      if (resolved.hasValue && !sceneCSSSameValue(target[key], resolved.value)) {
        const object = sceneCSSMutableTopObject(state, objectKey);
        object[key] = resolved.value;
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
        if (resolved.hasValue && !sceneCSSSameValue(record[key], resolved.value)) {
          const mutable = sceneCSSMutableRecord(state, collectionKey, index);
          mutable[key] = resolved.value;
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
          if (resolved.hasValue && !sceneCSSSameValue(child[key], resolved.value)) {
            const mutable = sceneCSSMutableNestedObject(state, "computeParticles", index, childKey);
            mutable[key] = resolved.value;
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
          if (color != null) material.color = color;
          if (roughness != null) material.roughness = sceneCSSCoerceValue(roughness);
          if (metalness != null) material.metalness = sceneCSSCoerceValue(metalness);
          if (opacity != null) material.opacity = sceneCSSCoerceValue(opacity);
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
        const emitter = sceneCSSMutableNestedObject(state, "computeParticles", index, "emitter");
        emitter.wind = sceneCSSCoerceValue(wind);
        state.dynamic = true;
      }
      const color = sceneCSSReadFirstProperty(css, element, ["--point-color", "--mesh-color"]);
      const size = sceneCSSReadFirstProperty(css, element, ["--point-size"]);
      const opacity = sceneCSSReadFirstProperty(css, element, ["--point-opacity"]);
      if (color != null || size != null || opacity != null) {
        const material = sceneCSSMutableNestedObject(state, "computeParticles", index, "material");
        if (color != null) material.color = color;
        if (size != null) material.size = sceneCSSCoerceValue(size);
        if (opacity != null) material.opacity = sceneCSSCoerceValue(opacity);
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
    return true;
  }

  function sceneCSSSetTopValue(state, key, value) {
    const bundle = state.out || state.source || {};
    if (bundle[key] === value) {
      return false;
    }
    const mutable = sceneCSSMutableBundle(state);
    mutable[key] = value;
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
    const materialIndex = Math.floor(sceneNumber(record && record.materialIndex, -1));
    const materials = sceneCSSCurrentCollection(state, "materials");
    if (!Array.isArray(materials) || materialIndex < 0 || materialIndex >= materials.length) {
      return null;
    }
    return sceneCSSMutableRecord(state, "materials", materialIndex);
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
    hash = scenePlannerHashNumber(hash, sceneNumber(bundle && bundle.timeSeconds, 0));
    hash = scenePlannerHashCamera(hash, camera);
    hash = scenePlannerHashViewport(hash, viewport);
    hash = scenePlannerHashAny(hash, bundle && bundle.environment, 0);
    hash = scenePlannerHashCollection(hash, bundle && bundle.meshObjects, scenePlannerHashMeshObject);
    hash = scenePlannerHashCollection(hash, bundle && bundle.objects, scenePlannerHashLineObject);
    hash = scenePlannerHashCollection(hash, bundle && bundle.materials, scenePlannerHashMaterial);
    hash = scenePlannerHashCollection(hash, bundle && bundle.lights, scenePlannerHashLight);
    hash = scenePlannerHashAny(hash, bundle && bundle.points, 0);
    hash = scenePlannerHashAny(hash, bundle && bundle.instancedMeshes, 0);
    hash = scenePlannerHashAny(hash, bundle && bundle.computeParticles, 0);
    hash = scenePlannerHashAny(hash, bundle && bundle.labels, 0);
    hash = scenePlannerHashAny(hash, bundle && bundle.sprites, 0);
    hash = scenePlannerHashAny(hash, bundle && bundle.postFX, 0);
    hash = scenePlannerHashAny(hash, bundle && bundle.postEffects, 0);
    hash = scenePlannerHashNumber(hash, arrayLength(bundle && bundle.worldPositions));
    hash = scenePlannerHashNumber(hash, arrayLength(bundle && bundle.worldColors));
    hash = scenePlannerHashAny(hash, bundle && bundle.worldLineWidths, 0);
    hash = scenePlannerHashAny(hash, bundle && bundle.worldLinePasses, 0);
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
    if (material && material.key) {
      return scenePlannerHashString(hash, material.key);
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
