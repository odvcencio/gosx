  // Scene material — normalization, profiling, and shader data for material kinds.

  var sceneMaterialProfileRegistry = Object.create(null);
  var sceneMaterialProfileRegistryVersion = 0;

  function sceneMaterialProfileKindKey(value) {
    const key = typeof value === "string" ? value.trim().toLowerCase() : "";
    return key && /^[a-z][a-z0-9_-]*$/.test(key) ? key : "";
  }

  function sceneMaterialProfileBlendMode(value) {
    const mode = typeof value === "string" ? value.trim().toLowerCase() : "";
    switch (mode) {
      case "opaque":
      case "solid":
        return "opaque";
      case "alpha":
      case "transparent":
      case "translucent":
        return "alpha";
      case "add":
      case "additive":
      case "glow":
      case "emissive":
        return "additive";
      default:
        return "";
    }
  }

  function sceneMaterialProfileRenderPass(value) {
    const pass = typeof value === "string" ? value.trim().toLowerCase() : "";
    switch (pass) {
      case "opaque":
      case "alpha":
      case "additive":
        return pass;
      case "add":
        return "additive";
      case "transparent":
      case "translucent":
        return "alpha";
      default:
        return "";
    }
  }

  function sceneNormalizeMaterialShaderData(values, fallback) {
    if (values && typeof values.length === "number" && values.length >= 3) {
      return [
        sceneNumber(values[0], fallback ? fallback[0] : 0),
        sceneNumber(values[1], fallback ? fallback[1] : 0),
        sceneNumber(values[2], fallback ? fallback[2] : 1),
      ];
    }
    return fallback ? fallback.slice(0, 3) : null;
  }

  function sceneRegisteredMaterialProfile(kind) {
    const key = sceneMaterialProfileKindKey(kind);
    return key && sceneMaterialProfileRegistry[key] || null;
  }

  function sceneMaterialProfileSnapshot(profile) {
    if (!profile) {
      return null;
    }
    return {
      kind: profile.kind,
      version: profile.version,
      opacity: profile.opacity,
      emissive: profile.emissive,
      blendMode: profile.blendMode,
      renderPass: profile.renderPass,
      shaderData: profile.shaderData ? profile.shaderData.slice(0, 3) : undefined,
      key: profile.key,
      dynamicShaderData: typeof profile.shaderDataFactory === "function",
    };
  }

  function registerSceneMaterialProfile(kind, profile) {
    const key = sceneMaterialProfileKindKey(kind);
    if (!key) {
      return null;
    }
    const src = profile && typeof profile === "object" ? profile : {};
    const record = {
      kind: key,
      version: sceneMaterialProfileRegistryVersion + 1,
      opacity: Object.prototype.hasOwnProperty.call(src, "opacity") ? clamp01(sceneNumber(src.opacity, 1)) : undefined,
      emissive: Object.prototype.hasOwnProperty.call(src, "emissive") ? clamp01(sceneNumber(src.emissive, 0)) : undefined,
      blendMode: sceneMaterialProfileBlendMode(src.blendMode),
      renderPass: sceneMaterialProfileRenderPass(src.renderPass),
      shaderData: typeof src.shaderData === "function" ? null : sceneNormalizeMaterialShaderData(src.shaderData, null),
      shaderDataFactory: typeof src.shaderData === "function" ? src.shaderData : null,
      key: typeof src.key === "string" ? src.key : "",
    };
    sceneMaterialProfileRegistryVersion = record.version;
    sceneMaterialProfileRegistry[key] = record;
    return sceneMaterialProfileSnapshot(record);
  }

  function unregisterSceneMaterialProfile(kind) {
    const key = sceneMaterialProfileKindKey(kind);
    if (!key || !sceneMaterialProfileRegistry[key]) {
      return false;
    }
    delete sceneMaterialProfileRegistry[key];
    sceneMaterialProfileRegistryVersion += 1;
    return true;
  }

  function listSceneMaterialProfiles() {
    return Object.keys(sceneMaterialProfileRegistry).sort().map(function(kind) {
      return sceneMaterialProfileSnapshot(sceneMaterialProfileRegistry[kind]);
    });
  }

  function normalizeSceneMaterialKind(value) {
    const kind = typeof value === "string" ? value.trim().toLowerCase() : "";
    switch (kind) {
      case "flat":
      case "ghost":
      case "glass":
      case "glow":
      case "matte":
      case "standard":
      case "custom":
      case "line-basic":
      case "line-dashed":
        return kind;
      default:
        if (sceneRegisteredMaterialProfile(kind)) {
          return kind;
        }
        return "flat";
    }
  }

  function sceneDefaultMaterialOpacity(kind) {
    const profile = sceneRegisteredMaterialProfile(kind);
    if (profile && profile.opacity !== undefined) {
      return profile.opacity;
    }
    switch (normalizeSceneMaterialKind(kind)) {
      case "ghost":
        return 0.42;
      case "glass":
        return 0.28;
      case "glow":
        return 0.92;
      default:
        return 1;
    }
  }

  function sceneDefaultMaterialEmissive(kind) {
    const profile = sceneRegisteredMaterialProfile(kind);
    if (profile && profile.emissive !== undefined) {
      return profile.emissive;
    }
    switch (normalizeSceneMaterialKind(kind)) {
      case "ghost":
        return 0.12;
      case "glass":
        return 0.08;
      case "glow":
        return 0.42;
      default:
        return 0;
    }
  }

  function sceneDefaultMaterialBlendMode(kind) {
    const profile = sceneRegisteredMaterialProfile(kind);
    if (profile && profile.blendMode) {
      return profile.blendMode;
    }
    switch (normalizeSceneMaterialKind(kind)) {
      case "ghost":
      case "glass":
        return "alpha";
      case "glow":
        return "additive";
      default:
        return "opaque";
    }
  }

  function normalizeSceneMaterialBlendMode(value, kind, opacity) {
    const mode = typeof value === "string" ? value.trim().toLowerCase() : "";
    switch (mode) {
      case "opaque":
      case "solid":
        return "opaque";
      case "alpha":
      case "transparent":
      case "translucent":
        return "alpha";
      case "add":
      case "additive":
      case "glow":
      case "emissive":
        return "additive";
      default: {
        const fallback = sceneDefaultMaterialBlendMode(kind);
        if (fallback !== "opaque") {
          return fallback;
        }
        return opacity < 0.999 ? "alpha" : "opaque";
      }
    }
  }

  function normalizeSceneMaterialRenderPass(value, blendMode, opacity, kind) {
    const pass = typeof value === "string" ? value.trim().toLowerCase() : "";
    switch (pass) {
      case "opaque":
      case "alpha":
      case "additive":
        return pass;
      case "add":
        return "additive";
      case "transparent":
      case "translucent":
        return "alpha";
      default:
        if (blendMode === "additive") {
          return "additive";
        }
        const profile = sceneRegisteredMaterialProfile(kind);
        if (profile && profile.renderPass) {
          return profile.renderPass;
        }
        return blendMode === "alpha" || opacity < 0.999 ? "alpha" : "opaque";
    }
  }

  function sceneObjectMaterialSource(item) {
    return item && item.material && typeof item.material === "object" ? item.material : null;
  }

  function sceneObjectMaterialKindValue(item) {
    if (!item || typeof item !== "object") {
      return "";
    }
    if (typeof item.material === "string" && item.material.trim()) {
      return item.material.trim();
    }
    if (typeof item.materialKind === "string" && item.materialKind.trim()) {
      return item.materialKind.trim();
    }
    const material = sceneObjectMaterialSource(item);
    if (material && typeof material.kind === "string" && material.kind.trim()) {
      return material.kind.trim();
    }
    return "";
  }

  function sceneObjectMaterialValue(item, name) {
    if (!item || typeof item !== "object") {
      return undefined;
    }
    const material = sceneObjectMaterialSource(item);
    if (material && Object.prototype.hasOwnProperty.call(material, name)) {
      return material[name];
    }
    return Object.prototype.hasOwnProperty.call(item, name) ? item[name] : undefined;
  }

  function sceneObjectMaterialHasValue(item, name) {
    if (!item || typeof item !== "object") {
      return false;
    }
    const material = sceneObjectMaterialSource(item);
    if (material && Object.prototype.hasOwnProperty.call(material, name)) {
      return true;
    }
    return Object.prototype.hasOwnProperty.call(item, name);
  }

  function sceneObjectBlendModeValue(item) {
    const direct = sceneObjectMaterialValue(item, "blendMode");
    if (direct !== undefined) {
      return direct;
    }
    const material = sceneObjectMaterialSource(item);
    if (material && Object.prototype.hasOwnProperty.call(material, "blend")) {
      return material.blend;
    }
    return item && Object.prototype.hasOwnProperty.call(item, "blend") ? item.blend : undefined;
  }

  function sceneObjectBlendModeHasValue(item) {
    if (!item || typeof item !== "object") {
      return false;
    }
    if (sceneObjectMaterialHasValue(item, "blendMode")) {
      return true;
    }
    const material = sceneObjectMaterialSource(item);
    if (material && Object.prototype.hasOwnProperty.call(material, "blend")) {
      return true;
    }
    return Object.prototype.hasOwnProperty.call(item, "blend");
  }

  function sceneObjectMaterialProfile(object) {
    const kind = normalizeSceneMaterialKind(object && object.materialKind);
    const opacity = clamp01(sceneNumber(object && object.opacity, sceneDefaultMaterialOpacity(kind)));
    const profile = {
      kind,
      color: object && typeof object.color === "string" && object.color ? object.color : "#8de1ff",
      texture: object && typeof object.texture === "string" ? object.texture.trim() : "",
      opacity,
      wireframe: sceneBool(object && object.wireframe, true),
      blendMode: normalizeSceneMaterialBlendMode(object && object.blendMode, kind, opacity),
      emissive: sceneCSSVarReference(object && object.emissive) ? String(object.emissive).trim() : clamp01(sceneNumber(object && object.emissive, sceneDefaultMaterialEmissive(kind))),
      roughness: sceneNumberOrCSSVar(object && object.roughness, 0.5),
      metalness: sceneNumberOrCSSVar(object && object.metalness, 0),
      clearcoat: sceneNumberOrCSSVar(object && object.clearcoat, 0),
      sheen: sceneNumberOrCSSVar(object && object.sheen, 0),
      transmission: sceneNumberOrCSSVar(object && object.transmission, 0),
      iridescence: sceneNumberOrCSSVar(object && object.iridescence, 0),
      anisotropy: sceneNumberOrCSSVar(object && object.anisotropy, 0),
      lineDash: sceneBool(object && object.lineDash, false),
      dashSize: sceneNumber(object && object.dashSize, 0),
      gapSize: sceneNumber(object && object.gapSize, 0),
      customVertex: typeof (object && object.customVertex) === "string" ? object.customVertex : "",
      customFragment: typeof (object && object.customFragment) === "string" ? object.customFragment : "",
      customVertexWGSL: typeof (object && object.customVertexWGSL) === "string" ? object.customVertexWGSL : "",
      customFragmentWGSL: typeof (object && object.customFragmentWGSL) === "string" ? object.customFragmentWGSL : "",
      customUniforms: object && object.customUniforms && typeof object.customUniforms === "object" ? Object.assign({}, object.customUniforms) : null,
      normalMap: object && typeof object.normalMap === "string" ? object.normalMap.trim() : "",
      roughnessMap: object && typeof object.roughnessMap === "string" ? object.roughnessMap.trim() : "",
      metalnessMap: object && typeof object.metalnessMap === "string" ? object.metalnessMap.trim() : "",
      emissiveMap: object && typeof object.emissiveMap === "string" ? object.emissiveMap.trim() : "",
    };
    profile.renderPass = normalizeSceneMaterialRenderPass(object && object.renderPass, profile.blendMode, profile.opacity, kind);
    profile.key = sceneMaterialProfileKey(profile);
    profile.shaderData = sceneMaterialShaderData(profile);
    return profile;
  }

  function sceneMaterialProfileKey(profile) {
    const registryProfile = sceneRegisteredMaterialProfile(profile && profile.kind);
    const parts = [
      normalizeSceneMaterialKind(profile && profile.kind),
      String(profile && profile.color || ""),
      String(profile && profile.texture || ""),
      clamp01(sceneNumber(profile && profile.opacity, 1)).toFixed(3),
      String(sceneBool(profile && profile.wireframe, true)),
      String(profile && profile.blendMode || "opaque"),
      String(profile && profile.renderPass || "opaque"),
      sceneCSSVarReference(profile && profile.emissive) ? String(profile.emissive).trim() : clamp01(sceneNumber(profile && profile.emissive, 0)).toFixed(3),
      sceneCSSVarReference(profile && profile.roughness) ? String(profile.roughness).trim() : sceneNumber(profile && profile.roughness, 0.5).toFixed(3),
      sceneCSSVarReference(profile && profile.metalness) ? String(profile.metalness).trim() : sceneNumber(profile && profile.metalness, 0).toFixed(3),
      sceneCSSVarReference(profile && profile.clearcoat) ? String(profile.clearcoat).trim() : sceneNumber(profile && profile.clearcoat, 0).toFixed(3),
      sceneCSSVarReference(profile && profile.sheen) ? String(profile.sheen).trim() : sceneNumber(profile && profile.sheen, 0).toFixed(3),
      sceneCSSVarReference(profile && profile.transmission) ? String(profile.transmission).trim() : sceneNumber(profile && profile.transmission, 0).toFixed(3),
      sceneCSSVarReference(profile && profile.iridescence) ? String(profile.iridescence).trim() : sceneNumber(profile && profile.iridescence, 0).toFixed(3),
      sceneCSSVarReference(profile && profile.anisotropy) ? String(profile.anisotropy).trim() : sceneNumber(profile && profile.anisotropy, 0).toFixed(3),
      String(sceneBool(profile && profile.lineDash, false)),
      sceneNumber(profile && profile.dashSize, 0).toFixed(3),
      sceneNumber(profile && profile.gapSize, 0).toFixed(3),
      String(profile && profile.customVertex || ""),
      String(profile && profile.customFragment || ""),
      String(profile && profile.customVertexWGSL || ""),
      String(profile && profile.customFragmentWGSL || ""),
      JSON.stringify(profile && profile.customUniforms || null),
      String(profile && profile.normalMap || ""),
      String(profile && profile.roughnessMap || ""),
      String(profile && profile.metalnessMap || ""),
      String(profile && profile.emissiveMap || ""),
    ];
    if (registryProfile) {
      parts.push("profile:" + registryProfile.version + ":" + String(registryProfile.key || ""));
    }
    return parts.join("|");
  }

  function sceneBundleMaterialIndex(bundle, materialLookup, profile) {
    if (!bundle || !Array.isArray(bundle.materials)) {
      return 0;
    }
    const key = profile && profile.key ? profile.key : sceneMaterialProfileKey(profile);
    if (materialLookup && materialLookup.has(key)) {
      return materialLookup.get(key);
    }
    const index = bundle.materials.length;
    bundle.materials.push(profile);
    if (materialLookup) {
      materialLookup.set(key, index);
    }
    return index;
  }

  function sceneMaterialStrokeColor(material) {
    const rgba = sceneColorRGBA(material && material.color, [0.55, 0.88, 1, 1]);
    rgba[3] = clamp01(rgba[3] * sceneMaterialOpacity(material));
    return "rgba(" +
      Math.round(rgba[0] * 255) + ", " +
      Math.round(rgba[1] * 255) + ", " +
      Math.round(rgba[2] * 255) + ", " +
      rgba[3].toFixed(3) + ")";
  }

  function sceneMaterialOpacity(material) {
    if (!material || typeof material !== "object") {
      return 1;
    }
    return clamp01(sceneNumber(material.opacity, 1));
  }

  function sceneMaterialEmissive(material) {
    if (!material || typeof material !== "object") {
      return 0;
    }
    return clamp01(sceneNumber(material.emissive, 0));
  }

  function sceneMaterialUsesAlpha(material) {
    return sceneMaterialRenderPass(material) !== "opaque";
  }

  function sceneMaterialRenderPass(material) {
    if (!material || typeof material !== "object") {
      return "opaque";
    }
    const renderPass = String(material.renderPass || "").toLowerCase();
    if (renderPass === "opaque" || renderPass === "alpha" || renderPass === "additive") {
      return renderPass;
    }
    const blendMode = String(material.blendMode || "").toLowerCase();
    if (blendMode === "additive") {
      return "additive";
    }
    if (blendMode === "alpha" || sceneMaterialOpacity(material) < 0.999) {
      return "alpha";
    }
    return "opaque";
  }

  function sceneMaterialShaderData(material) {
    if (material && Array.isArray(material.shaderData) && material.shaderData.length >= 3) {
      // Fast path: the material already carries a computed shaderData
      // array (typically stamped by sceneMaterialProfile on first call).
      // Previously this branch copied the array element-by-element via
      // sceneNumber — an extra 3-field allocation + 3 NaN-checks that
      // served no purpose since the values were already computed by
      // this very function. Returning the existing reference directly
      // saves an allocation per call; callers are strictly read-only
      // on the result (verified: sceneMeshMaterialArray,
      // appendSceneWorldObjectSlice, and the profile self-assignment
      // in sceneMaterialProfile all read data[0..2] and never mutate).
      return material.shaderData;
    }
    if (!material || typeof material !== "object") {
      return [0, 0, 1];
    }
    const kind = String(material.kind || "").toLowerCase();
    const profile = sceneRegisteredMaterialProfile(kind);
    if (profile) {
      if (profile.shaderDataFactory) {
        return sceneNormalizeMaterialShaderData(profile.shaderDataFactory(material), [0, sceneMaterialEmissive(material), 1]);
      }
      if (profile.shaderData) {
        return sceneNormalizeMaterialShaderData(profile.shaderData, [0, sceneMaterialEmissive(material), 1]);
      }
    }
    switch (kind) {
    case "ghost":
      return [1, sceneMaterialEmissive(material), 0.3];
    case "glass":
      return [2, sceneMaterialEmissive(material), 0.7];
    case "glow":
      return [3, sceneMaterialEmissive(material), 1];
    case "matte":
      return [4, sceneMaterialEmissive(material), 0.2];
    default:
      return [0, sceneMaterialEmissive(material), 1];
    }
  }

  function sceneFallbackMaterialData(vertexCount) {
    const values = new Float32Array(vertexCount * 3);
    for (let i = 0; i < vertexCount; i += 1) {
      values[i * 3 + 2] = 1;
    }
    return values;
  }

  // clamp01 is defined in 11-scene-math.js (shared across all modules).
