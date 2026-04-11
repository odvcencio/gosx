  // Scene material — normalization, profiling, and shader data for material kinds.

  function normalizeSceneMaterialKind(value) {
    const kind = typeof value === "string" ? value.trim().toLowerCase() : "";
    switch (kind) {
      case "ghost":
      case "glass":
      case "glow":
      case "matte":
        return kind;
      default:
        return "flat";
    }
  }

  function sceneDefaultMaterialOpacity(kind) {
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

  function normalizeSceneMaterialRenderPass(value, blendMode, opacity) {
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
      emissive: clamp01(sceneNumber(object && object.emissive, sceneDefaultMaterialEmissive(kind))),
    };
    profile.renderPass = normalizeSceneMaterialRenderPass(object && object.renderPass, profile.blendMode, profile.opacity);
    profile.key = sceneMaterialProfileKey(profile);
    profile.shaderData = sceneMaterialShaderData(profile);
    return profile;
  }

  function sceneMaterialProfileKey(profile) {
    return [
      normalizeSceneMaterialKind(profile && profile.kind),
      String(profile && profile.color || ""),
      String(profile && profile.texture || ""),
      clamp01(sceneNumber(profile && profile.opacity, 1)).toFixed(3),
      String(sceneBool(profile && profile.wireframe, true)),
      String(profile && profile.blendMode || "opaque"),
      String(profile && profile.renderPass || "opaque"),
      clamp01(sceneNumber(profile && profile.emissive, 0)).toFixed(3),
    ].join("|");
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
