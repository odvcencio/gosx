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

  function normalizeSceneMaterialBlendMode(value, kind, opacity, maskOpaque) {
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
        if (maskOpaque) {
          return "opaque";
        }
        return opacity < 0.999 ? "alpha" : "opaque";
      }
    }
  }

  function normalizeSceneMaterialRenderPass(value, blendMode, opacity, kind, maskOpaque) {
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
        // An explicit alpha blend selects the alpha pass unless an
        // explicit pass or a registered profile supersedes it, even when
        // mask-opaque routing would otherwise default to opaque.
        if (blendMode === "alpha") {
          return "alpha";
        }
        if (maskOpaque) {
          return "opaque";
        }
        return blendMode === "alpha" || opacity < 0.999 ? "alpha" : "opaque";
    }
  }

  // Scene/glTF-authored index of refraction (the KHR_materials_ior numeric
  // contract): finite ior >= 1 is valid with no upper truncation; an
  // explicit numeric 0 is the glTF compatibility mode that pins the
  // dielectric Fresnel to 1 (it is neither a default nor a clamp); missing,
  // null, invalid, non-finite, negative and 0<ior<1 all default safely.
  // CSS var strings ride the existing explicit-var machinery and come back
  // trimmed; null, false and empty strings are never coerced to zero.
  function sceneNormalizeMaterialIor(value, fallback) {
    if (sceneCSSVarReference(value)) {
      return String(value).trim();
    }
    var numeric = value;
    if (typeof numeric === "string") {
      numeric = numeric.trim() !== "" ? Number(numeric) : NaN;
    } else if (typeof numeric !== "number") {
      numeric = NaN;
    }
    if (numeric === 0) {
      return 0;
    }
    if (Number.isFinite(numeric) && numeric >= 1) {
      return numeric;
    }
    if (sceneCSSVarReference(fallback)) {
      return String(fallback).trim();
    }
    // The inherited fallback must satisfy the same numeric contract as the
    // direct value: sceneNumber(fallback, 1.5) coerces null, false and ""
    // to 0 — silently enabling the glTF zero mode — and passes negative or
    // 0<ior<1 numbers straight through. Revalidating under the same rule
    // terminates because the hard 1.5 default is always valid.
    return sceneNormalizeMaterialIor(fallback, 1.5);
  }

  // Specular intensity (KHR-style numeric contract): finite intensity within
  // [0, 1] is valid with an explicit 0 preserved; missing, null, booleans,
  // empty strings, non-finite and out-of-range values fall back to a valid
  // inherited value or the hard default of 1. CSS var strings ride the
  // existing explicit-var machinery and come back trimmed.
  function sceneNormalizeMaterialSpecularIntensity(value, fallback) {
    if (sceneCSSVarReference(value)) {
      return String(value).trim();
    }
    var numeric = value;
    if (typeof numeric === "string") {
      numeric = numeric.trim() !== "" ? Number(numeric) : NaN;
    } else if (typeof numeric !== "number") {
      numeric = NaN;
    }
    if (Number.isFinite(numeric) && numeric >= 0 && numeric <= 1) {
      return numeric;
    }
    if (sceneCSSVarReference(fallback)) {
      return String(fallback).trim();
    }
    // The inherited fallback must satisfy the same numeric contract as the
    // direct value; the hard default of 1 is always valid, so revalidation
    // terminates.
    return sceneNormalizeMaterialSpecularIntensity(fallback, 1);
  }

  function sceneIsSpecularColorArray(value) {
    if (Array.isArray(value)) {
      return true;
    }
    if (typeof ArrayBuffer !== "undefined" && ArrayBuffer.isView) {
      return ArrayBuffer.isView(value) && !(typeof DataView !== "undefined" && value instanceof DataView);
    }
    return false;
  }

  function sceneSpecularColorComponent(component) {
    if (typeof component === "string") {
      component = component.trim() !== "" ? Number(component) : NaN;
    } else if (typeof component !== "number") {
      component = NaN;
    }
    return Number.isFinite(component) && component >= 0 ? component : NaN;
  }

  // A specular tint triple is valid only as a whole: exactly three
  // components, each a finite non-negative number (numeric component
  // strings parse under the scalar convention). Only arrays and typed
  // arrays qualify — arbitrary array-like objects, strings (which would
  // index characters), holes, nulls, booleans, negative and non-finite
  // components invalidate the entire triple.
  function sceneParseSpecularColorTriple(value) {
    if (!sceneIsSpecularColorArray(value) || value.length !== 3) {
      return null;
    }
    const triple = [
      sceneSpecularColorComponent(value[0]),
      sceneSpecularColorComponent(value[1]),
      sceneSpecularColorComponent(value[2]),
    ];
    return triple.every(Number.isFinite) ? triple : null;
  }

  // Numeric CSS-style triples ("0.5 0.5 0.5" / "0.5,0.5,0.5") are accepted
  // as explicit LINEAR-space colors: exactly three numeric tokens separated
  // by whitespace or commas. Anything else (bare numbers like "123", wrong
  // token counts, junk tokens, negatives) is rejected wholesale. No gamma
  // conversion happens here and CSS var text never reaches this parser —
  // var references are resolved by the caller first.
  function sceneParseSpecularColorText(text) {
    if (typeof text !== "string") {
      return null;
    }
    const parts = text.trim().split(/[\s,]+/).filter(Boolean);
    if (parts.length !== 3) {
      return null;
    }
    const triple = parts.map(sceneSpecularColorComponent);
    return triple.every(Number.isFinite) ? triple : null;
  }

  // Specular tint (KHR-style color factor): LINEAR RGB, never sRGB. A valid
  // triple is consumed whole; any invalid shape or component (including a
  // single bad channel) makes the whole color invalid, so the inherited
  // fallback color or the hard [1, 1, 1] default applies — there is no
  // per-channel repair. The returned value is always a fresh snapshot so
  // mutating author inputs cannot mutate normalized or cached materials.
  function sceneNormalizeMaterialSpecularColor(value, fallback) {
    if (sceneCSSVarReference(value)) {
      return String(value).trim();
    }
    const parsed = sceneParseSpecularColorTriple(value)
      || sceneParseSpecularColorText(value);
    if (parsed) {
      return parsed;
    }
    if (sceneCSSVarReference(fallback)) {
      return String(fallback).trim();
    }
    const fallbackParsed = sceneParseSpecularColorTriple(fallback)
      || sceneParseSpecularColorText(fallback);
    if (fallbackParsed) {
      return fallbackParsed;
    }
    return [1, 1, 1];
  }

  // Scene/glTF-authored alpha cutoff:
  // finite numbers >= 0 are valid with no upper clamp — 0 and values above
  // 1 included; nonempty numeric strings are accepted. An explicit null
  // disables the cutoff outright. undefined inherits a validated fallback;
  // booleans, empty strings, unparseable text, non-finite and negative
  // values fall back safely, terminating at null. CSS var strings ride the
  // existing explicit-var machinery and come back trimmed; null, false and
  // empty strings are never coerced to 0.
  function sceneNormalizeMaterialAlphaCutoff(value, fallback) {
    if (value === null) {
      return null;
    }
    if (sceneCSSVarReference(value)) {
      return String(value).trim();
    }
    var numeric = value;
    if (typeof numeric === "string") {
      numeric = numeric.trim() !== "" ? Number(numeric) : NaN;
    } else if (typeof numeric !== "number") {
      numeric = NaN;
    }
    if (Number.isFinite(numeric) && numeric >= 0) {
      return numeric;
    }
    if (fallback === null) {
      return null;
    }
    if (sceneCSSVarReference(fallback)) {
      return String(fallback).trim();
    }
    // The inherited fallback must satisfy the same numeric contract as the
    // direct value; the hard null default always terminates the recursion.
    return sceneNormalizeMaterialAlphaCutoff(fallback, null);
  }

  // Alpha-mask routing provenance helpers. A numeric-enabled cutoff (finite
  // number >= 0, including 0; null and invalid values are disabled) on a
  // built-in shader forces surviving mask fragments opaque, so derived
  // (non-explicit) blend/renderPass choices default to opaque. Explicit
  // choices and authored shaders (custom/Selena) keep their legacy behavior.
  function sceneMaterialMaskActive(alphaCutoff) {
    const cutoff = sceneNormalizeMaterialAlphaCutoff(alphaCutoff, null);
    return typeof cutoff === "number" && Number.isFinite(cutoff);
  }

  function sceneMaterialHasDirectAuthoredShaderValues(values) {
    if (!values || typeof values !== "object") {
      return false;
    }
    if (String(values.shaderBackend || "").trim().toLowerCase() === "selena") {
      return true;
    }
    return Boolean(
      (typeof values.customVertex === "string" && values.customVertex.trim()) ||
      (typeof values.customFragment === "string" && values.customFragment.trim()) ||
      (typeof values.customVertexWGSL === "string" && values.customVertexWGSL.trim()) ||
      (typeof values.customFragmentWGSL === "string" && values.customFragmentWGSL.trim()) ||
      (typeof values.shaderSource === "string" && values.shaderSource.trim())
    );
  }

  function sceneMaterialHasAuthoredShaderValues(values) {
    if (sceneMaterialHasDirectAuthoredShaderValues(values)) {
      return true;
    }
    // Raw scene objects may carry material fields on a nested
    // item.material source; shader detection must see the same effective
    // values the normalizer inheritance uses.
    return !!(values && typeof values === "object" &&
      sceneMaterialHasDirectAuthoredShaderValues(values.material));
  }

  // Effective per-field shader values the normalizers actually inherit.
  // Authorship must be judged per field: a string item value wins (nested
  // material first for objects/instanced meshes, direct-only for named
  // records), and a non-string value inherits the current string instead
  // of clearing — matching actual field normalization. Clearing one shader
  // field must not clear a retained sibling field.
  const SCENE_SHADER_AUTHORED_KEYS = [
    "shaderBackend",
    "customVertex",
    "customFragment",
    "customVertexWGSL",
    "customFragmentWGSL",
    "shaderSource",
  ];

  function sceneEffectiveShaderValues(item, current, directOnly) {
    const values = {};
    for (let i = 0; i < SCENE_SHADER_AUTHORED_KEYS.length; i += 1) {
      const key = SCENE_SHADER_AUTHORED_KEYS[i];
      const itemValue = item && typeof item === "object"
        ? (directOnly ? item[key] : sceneObjectMaterialValue(item, key))
        : undefined;
      const currentValue = current && typeof current === "object" ? current[key] : undefined;
      values[key] = typeof itemValue === "string"
        ? itemValue
        : (typeof currentValue === "string" ? currentValue : "");
    }
    return values;
  }

  // Resolves the object that supplied a routed blend/pass value so the
  // derived marker is read from that same object:
  // - "blendAlias" mirrors sceneObjectBlendModeValue: material blendMode,
  //   item blendMode, then the blend alias (material blend, item blend),
  //   including its handling of undefined values.
  // - "material" mirrors sceneObjectMaterialValue: material own property,
  //   then item own property, no blend alias.
  // - "direct" reads only the item's own property, never nested material.
  // With no route value, the fallback (current) object supplies the marker.
  function sceneRoutedValueSource(item, name, mode) {
    if (!item || typeof item !== "object") {
      return null;
    }
    if (mode === "direct") {
      return Object.prototype.hasOwnProperty.call(item, name) ? item : null;
    }
    const material = sceneObjectMaterialSource(item);
    if (mode === "blendAlias") {
      const materialHasBlendMode =
        material && Object.prototype.hasOwnProperty.call(material, "blendMode");
      if (materialHasBlendMode && material.blendMode !== undefined) {
        return material;
      }
      if (!materialHasBlendMode &&
          Object.prototype.hasOwnProperty.call(item, "blendMode") &&
          item.blendMode !== undefined) {
        return item;
      }
      if (material && Object.prototype.hasOwnProperty.call(material, "blend")) {
        return material;
      }
      return Object.prototype.hasOwnProperty.call(item, "blend") ? item : null;
    }
    if (material && Object.prototype.hasOwnProperty.call(material, name)) {
      return material;
    }
    return Object.prototype.hasOwnProperty.call(item, name) ? item : null;
  }

  function sceneRoutedValueExplicit(item, current, hasItemValue, name, derivedKey, mode) {
    if (!hasItemValue) {
      return current[derivedKey] !== true;
    }
    const source = sceneRoutedValueSource(item, name, mode);
    return !(source && source[derivedKey] === true);
  }

  function sceneRoutedBlendExplicit(item, current, hasItemValue, mode) {
    return sceneRoutedValueExplicit(item, current, hasItemValue,
      "blendMode", "_blendModeDerived", mode || "blendAlias");
  }

  function sceneRoutedPassExplicit(item, current, hasItemValue, mode) {
    return sceneRoutedValueExplicit(item, current, hasItemValue,
      "renderPass", "_renderPassDerived", mode || "material");
  }

  function sceneMaterialMaskOpaqueRouting(material) {
    if (!material || typeof material !== "object" ||
        sceneMaterialHasAuthoredShaderValues(material)) {
      return false;
    }
    return sceneMaterialMaskActive(material.alphaCutoff);
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
    // Routing must judge the same effective shader fields this profile
    // actually copies to the renderer (the flattened top-level values),
    // not stale nested source material retained on instanced entries.
    const maskOpaque = sceneMaterialMaskActive(object && object.alphaCutoff) &&
      !sceneMaterialHasDirectAuthoredShaderValues(object);
    // A positive derived marker means computed. Raw (unmarked) values are
    // authored only when they carry a recognized explicit route value; a
    // raw profile with no route value at all conveys the computed default,
    // and an explicit (marker === false) marker always stays authored.
    // Recognition reuses the shared profile alias maps so raw authored
    // aliases (transparent/translucent/add/solid/glow/emissive) stay
    // authored exactly like canonical opaque/alpha/additive values.
    const blendMarker = object && object._blendModeDerived;
    const passMarker = object && object._renderPassDerived;
    const blendDerived = blendMarker === true ||
      (blendMarker !== false &&
        sceneMaterialProfileBlendMode(object && object.blendMode) === "");
    const passDerived = passMarker === true ||
      (passMarker !== false &&
        sceneMaterialProfileRenderPass(object && object.renderPass) === "");
    const profile = {
      kind,
      color: object && typeof object.color === "string" && object.color ? object.color : "#8de1ff",
      texture: object && typeof object.texture === "string" ? object.texture.trim() : "",
      opacity,
      unlit: sceneBool(object && object.unlit, false),
      wireframe: sceneBool(object && object.wireframe, true),
      blendMode: normalizeSceneMaterialBlendMode(
        blendDerived ? "" : (object && object.blendMode),
        kind,
        opacity,
        maskOpaque,
      ),
      emissive: sceneCSSVarReference(object && object.emissive) ? String(object.emissive).trim() : clamp01(sceneNumber(object && object.emissive, sceneDefaultMaterialEmissive(kind))),
      roughness: sceneNumberOrCSSVar(object && object.roughness, 0.5),
      metalness: sceneNumberOrCSSVar(object && object.metalness, 0),
      ior: sceneNormalizeMaterialIor(object && object.ior, 1.5),
      alphaCutoff: sceneNormalizeMaterialAlphaCutoff(object && object.alphaCutoff, null),
      specularIntensity: sceneNormalizeMaterialSpecularIntensity(object && object.specularIntensity, 1),
      specularColor: sceneNormalizeMaterialSpecularColor(object && object.specularColor, null),
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
      shaderBackend: typeof (object && object.shaderBackend) === "string" ? object.shaderBackend.trim().toLowerCase() : "",
      shaderLayout: object && object.shaderLayout && typeof object.shaderLayout === "object" ? sceneCloneData(object.shaderLayout) : null,
      shaderSource: typeof (object && object.shaderSource) === "string" ? object.shaderSource.trim() : "",
      shaderSourceFiles: object && object.shaderSourceFiles && typeof object.shaderSourceFiles === "object" ? sceneCloneData(object.shaderSourceFiles) : null,
      normalMap: object && typeof object.normalMap === "string" ? object.normalMap.trim() : "",
      roughnessMap: object && typeof object.roughnessMap === "string" ? object.roughnessMap.trim() : "",
      metalnessMap: object && typeof object.metalnessMap === "string" ? object.metalnessMap.trim() : "",
      occlusionMap: object && typeof object.occlusionMap === "string" ? object.occlusionMap.trim() : "",
      emissiveMap: object && typeof object.emissiveMap === "string" ? object.emissiveMap.trim() : "",
      textureDescriptors: typeof normalizeSceneMaterialTextureDescriptors === "function"
        ? normalizeSceneMaterialTextureDescriptors(object && object.textureDescriptors, null)
        : (object && object.textureDescriptors ? sceneCloneData(object.textureDescriptors) : null),
    };
    profile.renderPass = normalizeSceneMaterialRenderPass(
      passDerived ? "" : (object && object.renderPass),
      profile.blendMode,
      profile.opacity,
      kind,
      maskOpaque,
    );
    // Carry routing provenance on the profile so downstream bundle
    // records, dedup keys, and selectors can distinguish derived
    // (computed) routes from authored ones after CSS resolution.
    profile._blendModeDerived = blendDerived;
    profile._renderPassDerived = passDerived;
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
      String(sceneBool(profile && profile.unlit, false)),
      clamp01(sceneNumber(profile && profile.opacity, 1)).toFixed(3),
      String(sceneBool(profile && profile.wireframe, true)),
      String(profile && profile.blendMode || "opaque"),
      String(profile && profile.renderPass || "opaque"),
      // Derived-vs-authored provenance changes routing behavior, so
      // otherwise-identical profiles must never share a cached material.
      String(profile && profile._blendModeDerived === true),
      String(profile && profile._renderPassDerived === true),
      sceneCSSVarReference(profile && profile.emissive) ? String(profile.emissive).trim() : clamp01(sceneNumber(profile && profile.emissive, 0)).toFixed(3),
      sceneCSSVarReference(profile && profile.roughness) ? String(profile.roughness).trim() : sceneNumber(profile && profile.roughness, 0.5).toFixed(3),
      sceneCSSVarReference(profile && profile.metalness) ? String(profile.metalness).trim() : sceneNumber(profile && profile.metalness, 0).toFixed(3),
      // Authored ior keys at full precision — no toFixed quantization — so
      // distinct valid iors never share a cached material. Raw invalid values
      // (null/false/"") go through sceneNormalizeMaterialIor onto the 1.5
      // shader default instead of colliding with an explicit 0 (F0 = 1).
      sceneCSSVarReference(profile && profile.ior) ? String(profile.ior).trim() : sceneNormalizeMaterialIor(profile && profile.ior),
      // Authored alpha cutoff keys at full precision — no toFixed
      // quantization — so distinct valid cutoffs never share a cached
      // material. A disabled cutoff (null) stringifies distinctly from an
      // explicit 0 threshold; CSS vars keep their trimmed form.
      sceneCSSVarReference(profile && profile.alphaCutoff) ? String(profile.alphaCutoff).trim() : String(sceneNormalizeMaterialAlphaCutoff(profile && profile.alphaCutoff)),
      // Specular factors key at full precision — intensity is never
      // quantized and the tint is serialized exactly — so distinct valid
      // values (per RGB component and per intensity, however close) never
      // share a cached material. Raw invalid values normalize onto the
      // defaults instead of colliding with explicit zero/black.
      sceneCSSVarReference(profile && profile.specularIntensity) ? String(profile.specularIntensity).trim() : sceneNormalizeMaterialSpecularIntensity(profile && profile.specularIntensity),
      sceneCSSVarReference(profile && profile.specularColor) ? String(profile.specularColor).trim() : JSON.stringify(sceneNormalizeMaterialSpecularColor(profile && profile.specularColor, null)),
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
      String(profile && profile.shaderBackend || ""),
      JSON.stringify(profile && profile.shaderLayout || null),
      String(profile && profile.shaderSource || ""),
      JSON.stringify(profile && profile.shaderSourceFiles || null),
      String(profile && profile.normalMap || ""),
      String(profile && profile.roughnessMap || ""),
      String(profile && profile.metalnessMap || ""),
      String(profile && profile.occlusionMap || ""),
      String(profile && profile.emissiveMap || ""),
      JSON.stringify(profile && profile.textureDescriptors || null),
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

  function sceneMaterialRenderPass(material) {
    if (!material || typeof material !== "object") {
      return "opaque";
    }
    // Derived (computed) material routes must be re-evaluated from the
    // effective fields whenever consulted — notably after real CSS
    // resolution replaced an authored var() alphaCutoff string with a
    // numeric value — instead of replaying the pre-resolution cached
    // strings. Authored (unmarked raw or explicitly non-derived) values
    // keep precedence through the normal path below.
    if (material._renderPassDerived === true || material._blendModeDerived === true) {
      const opacity = sceneMaterialOpacity(material);
      const maskOpaque = sceneMaterialMaskOpaqueRouting(material);
      const kind = String(material.kind || "");
      const blendMode = material._blendModeDerived === true
        ? normalizeSceneMaterialBlendMode("", kind, opacity, maskOpaque)
        : String(material.blendMode || "").toLowerCase();
      return normalizeSceneMaterialRenderPass(
        material._renderPassDerived === true ? "" : String(material.renderPass || ""),
        blendMode,
        opacity,
        kind,
        maskOpaque,
      );
    }
    const renderPass = String(material.renderPass || "").toLowerCase();
    if (renderPass === "opaque" || renderPass === "alpha" || renderPass === "additive") {
      return renderPass;
    }
    const blendMode = String(material.blendMode || "").toLowerCase();
    if (blendMode === "additive") {
      return "additive";
    }
    if (blendMode === "alpha") {
      return "alpha";
    }
    if (sceneMaterialMaskOpaqueRouting(material)) {
      return "opaque";
    }
    if (sceneMaterialOpacity(material) < 0.999) {
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
