  function sceneWebGPUCanvasAlphaMode(value) {
    var normalized = String(value || "").trim().toLowerCase();
    if (normalized === "opaque" || normalized === "premultiplied") {
      return normalized;
    }
    return "premultiplied";
  }

  function sceneWebGPUCanvasColorSpace(value) {
    var normalized = String(value || "").trim().toLowerCase();
    if (normalized === "display-p3" || normalized === "srgb") {
      return normalized;
    }
    return "srgb";
  }

  function sceneWebGPUCanvasToneMappingMode(value) {
    var normalized = String(value || "").trim().toLowerCase();
    if (normalized === "extended" || normalized === "standard") {
      return normalized;
    }
    return "";
  }

  function sceneWebGPUCanvasPowerPreference(value) {
    var normalized = String(value || "").trim().toLowerCase();
    if (normalized === "high-performance" || normalized === "low-power") {
      return normalized;
    }
    return "";
  }

  function destroyGPUTimingResources(timing) {
    if (!timing || timing === false) return;
    destroyRendererGPUResource(timing.querySet);
    var slots = Array.isArray(timing.slots) ? timing.slots : [];
    for (var i = 0; i < slots.length; i++) {
      var slot = slots[i];
      if (!slot) continue;
      try {
        if (slot.readback && typeof slot.readback.unmap === "function" && slot.mapping) slot.readback.unmap();
      } catch (_unmapError) {}
      destroyRendererGPUResource(slot.resolve);
      destroyRendererGPUResource(slot.readback);
      slot.pending = false;
      slot.mapping = false;
    }
  }

  function wgpuTrackedBufferSize(buffer) {
    if (!buffer) return 0;
    if (typeof buffer.size === "number") return buffer.size;
    if (typeof buffer._gosxByteLength === "number") return buffer._gosxByteLength;
    return 0;
  }

  function pointsDefaultColorChanged(entry, rgba) {
    var cached = entry._cachedParticleDefaultColor;
    return !cached ||
      cached[0] !== rgba[0] ||
      cached[1] !== rgba[1] ||
      cached[2] !== rgba[2] ||
      cached[3] !== rgba[3];
  }

  function ensurePointsParticleData(entry, count, hasSizes, hasColors, defaultColorRGBA) {
    var pos = entry._cachedPos;
    var sizes = hasSizes ? entry._cachedSizes : null;
    var colors = hasColors ? entry._cachedColors : null;
    if (
      entry._cachedParticleData &&
      entry._cachedParticleCount === count &&
      entry._cachedParticlePositions === pos &&
      entry._cachedParticleSizes === sizes &&
      entry._cachedParticleColors === colors &&
      !pointsDefaultColorChanged(entry, defaultColorRGBA)
    ) {
      return entry._cachedParticleData;
    }

    var particleData = new Float32Array(count * 8);
    for (var pi = 0; pi < count; pi++) {
      var base = pi * 8;
      particleData[base + 0] = pos[pi * 3];
      particleData[base + 1] = pos[pi * 3 + 1];
      particleData[base + 2] = pos[pi * 3 + 2];
      particleData[base + 3] = hasSizes ? sizes[pi] : sceneNumber(entry.size, 1);
      if (hasColors) {
        particleData[base + 4] = colors[pi * 4];
        particleData[base + 5] = colors[pi * 4 + 1];
        particleData[base + 6] = colors[pi * 4 + 2];
        particleData[base + 7] = colors[pi * 4 + 3];
      } else {
        particleData[base + 4] = defaultColorRGBA[0];
        particleData[base + 5] = defaultColorRGBA[1];
        particleData[base + 6] = defaultColorRGBA[2];
        particleData[base + 7] = 1.0;
      }
    }

    entry._cachedParticleData = particleData;
    entry._cachedParticleCount = count;
    entry._cachedParticlePositions = pos;
    entry._cachedParticleSizes = sizes;
    entry._cachedParticleColors = colors;
    entry._cachedParticleDefaultColor = defaultColorRGBA.slice ? defaultColorRGBA.slice(0, 4) : [
      defaultColorRGBA[0], defaultColorRGBA[1], defaultColorRGBA[2], defaultColorRGBA[3],
    ];
    return particleData;
  }

  function instancedMeshUsesCull(mesh: any) {
    return Boolean(typeof mesh.cullKernelWGSL === "string" && mesh.cullKernelWGSL.trim()) || webGPUBuiltinCullEligible(mesh);
  }
  function sceneSelenaWGSLSource(material) {
    var src = typeof material.customVertexWGSL === "string" && material.customVertexWGSL.trim()
      ? material.customVertexWGSL
      : material.customFragmentWGSL;
    return String(src || "").trim();
  }

  function sceneSelenaUniformBufferSlot(renderContext) {
    var suffix = renderContext && typeof renderContext.uniformSlotSuffix === "string"
      ? renderContext.uniformSlotSuffix.trim().replace(/[^A-Za-z0-9_-]+/g, "-")
      : "";
    return suffix ? "_gosxWGPUSelenaUniform_" + suffix : "_gosxWGPUSelenaUniform";
  }

  function sceneSelenaResourceRef(material, descriptor) {
    var name = descriptor && descriptor.name;
    var value = sceneSelenaMaterialValue(material, name);
    if (value && typeof value === "object") {
      if (typeof value.resource === "string") return value.resource.trim();
      if (typeof value.ref === "string") return value.ref.trim();
      if (typeof value.sceneResource === "string") return value.sceneResource.trim();
    }
    if (typeof value === "string") {
      var trimmed = value.trim();
      if (trimmed.indexOf("gosx:") === 0 || trimmed.indexOf("water:") === 0) return trimmed;
    }
    return "";
  }

  function sceneSelenaParseResourceRef(ref) {
    if (typeof ref !== "string") return null;
    var trimmed = ref.trim();
    if (!trimmed) return null;
    var parts = trimmed.split(":").filter(function(part) { return part !== ""; });
    if (parts[0] === "gosx") parts.shift();
    if (parts[0] !== "water" || parts.length < 3) return null;
    return { kind: "water", id: parts[1], slot: parts.slice(2).join(":") };
  }

  function wgpuSceneSelenaTextureURL(material, texture, index) {
    var name = texture && texture.name;
    var value = sceneSelenaMaterialValue(material, name);
    if (typeof value === "string" && value.trim() && !value.trim().startsWith("gosx:") && !sceneSelenaParseResourceRef(value)) {
      return value.trim();
    }
    if (material && name && typeof material[name] === "string" && material[name].trim()) {
      return material[name].trim();
    }
    if (index === 0 && material && typeof material.texture === "string" && material.texture.trim()) {
      return material.texture.trim();
    }
    return "";
  }

  function wgpuSceneSelenaTextureDescriptors(layout) {
    return layout && Array.isArray(layout.textures) ? layout.textures : [];
  }

  function sceneSelenaStorageBufferDescriptors(layout) {
    if (layout && Array.isArray(layout.storageBuffers)) return layout.storageBuffers;
    if (layout && Array.isArray(layout.buffers)) {
      return layout.buffers.filter(function(buffer) {
        var kind = String(buffer && (buffer.kind || buffer.type || "")).toLowerCase();
        return kind.indexOf("storage") >= 0;
      });
    }
    return [];
  }

  // sceneSelenaStateDescriptors returns a material's Selena `state`
  // statefields (bindings.Layout.States), e.g. the water pool pass's
  // `state height` feedback heightfield. This is DISTINCT from
  // sceneSelenaStorageBufferDescriptors (which reads a hand-authored
  // "storageBuffers"/"buffers" descriptor list): a Selena-compiled mesh+state
  // material emits its statefield as `layout.states[]` + a companion
  // `layout.grid` uniform (StateGrid{gridWidth,gridHeight}), not a generic
  // storage buffer entry. No current non-water Selena custom material
  // declares `state`, so this is purely additive.
  function sceneSelenaStateDescriptors(layout) {
    return layout && Array.isArray(layout.states) ? layout.states : [];
  }

  function wgpuSelenaAttributeSource(name) {
    switch (name) {
    case "position": return "positions";
    case "normal": return "normals";
    case "uv": return "uvs";
    case "tangent": return "tangents";
    default: return "";
    }
  }

  function sceneSelenaPipelineAttributes(layout) {
    var attrs = Array.isArray(layout && layout.attributes) ? layout.attributes : [];
    var out = [];
    var seenLocations = {};
    // Single static membership string of canonical reserved aliases that
    // must never become custom attributes.
    var RESERVED = "|position|positions|normal|normals|uv|uvs|uv1|tangent|tangents|index|indices|skin|skinIndex|joints|weights|";
    // Single pass over the declared layout so output preserves the original
    // Selena attribute order. Each descriptor is validated in place or
    // dropped entirely (fail closed).
    for (var i = 0; i < attrs.length; i++) {
      var attr = attrs[i] || {};
      var name = typeof attr.name === "string" ? attr.name.trim() : "";
      if (!name) continue;
      // Only exact float/vec2/vec3/vec4 types are supported.
      var type = String(attr.type || "");
      var comps = type === "float" ? 1 : type === "vec2" ? 2 :
        type === "vec3" ? 3 : type === "vec4" ? 4 : 0;
      if (!comps) continue;
      // Builtin sources come from the shared helper; everything else must
      // clear the reserved list and identifier validation.
      var source = wgpuSelenaAttributeSource(name);
      var isBuiltin = !!source;
      if (!isBuiltin) {
        if (RESERVED.indexOf("|" + name + "|") !== -1) continue;
        // Custom names must be valid WGSL-safe identifiers.
        if (!/^[A-Za-z_][A-Za-z0-9_]*$/.test(name)) continue;
        source = "custom";
        // Custom shader locations must be explicit nonnegative integers.
        var n = Number(attr.location);
        if (!Number.isFinite(n) || n < 0 || Math.floor(n) !== n) continue;
      }
      // Location: explicit value when provided, else prior builtin fallback
      // of the running slot index (compatible only for builtin attributes).
      var loc;
      if (attr.location != null && Number.isFinite(Number(attr.location))) {
        loc = Math.floor(Number(attr.location));
        if (loc < 0 || loc !== Number(attr.location)) continue;
      } else {
        if (!isBuiltin) continue; // custom already required a location above
        loc = out.length;
      }
      // Skip any descriptor whose location duplicates an earlier emission.
      if (seenLocations[loc]) continue;
      seenLocations[loc] = true;
      out.push({
        name: name,
        source: source,
        slot: out.length,
        components: comps,
        shaderLocation: loc,
        format: comps === 1 ? "float32" : comps === 2 ? "float32x2" :
          comps === 3 ? "float32x3" : "float32x4",
      });
    }
    return out;
  }

  // sceneSelenaGridUniformData packs a Selena mesh+state material's
  // StateGrid{gridWidth,gridHeight} uniform (8 bytes, 2x u32) from
  // renderContext.grid: either a single number (square grid, e.g. the water
  // heightfield's resolution x resolution) or a {width,height} pair. Returns
  // null when renderContext carries no grid value (caller treats that as a
  // bind-group build failure, mirroring the "missing storage buffer" null
  // returns elsewhere in this function).
  //
  // Falls back to material.customUniforms.grid when renderContext supplies
  // none, mirroring the SAME renderContext-then-customUniforms priority
  // sceneSelenaUniformValue already uses for ordinary uniform fields (see
  // sceneSelenaRenderContextUniformValue / sceneSelenaMaterialValue above).
  // This matters for a Selena mesh+state material drawn through a call site
  // that has no per-draw renderContext at all (e.g. drawPBRObjects' generic
  // main-scene mesh path, used by the water object-material/duck-material
  // Selena materials): without this fallback, the grid uniform would be
  // silently unbuildable there even though the grid size is a known,
  // effectively-static per-demo constant.
  function sceneSelenaGridUniformData(material, renderContext) {
    var grid = renderContext && renderContext.grid;
    if (grid === undefined || grid === null) {
      grid = material && material.customUniforms && material.customUniforms.grid;
    }
    if (grid === undefined || grid === null) return null;
    var width, height;
    if (typeof grid === "number" || typeof grid === "string") {
      width = sceneNumber(grid, 0);
      height = width;
    } else {
      width = sceneNumber(grid.width, 0);
      height = sceneNumber(grid.height, width);
    }
    width = Math.max(1, Math.floor(width));
    height = Math.max(1, Math.floor(height));
    return new Uint32Array([width, height]);
  }

  // sceneSelenaComputeGridUniformData packs a feedback material's
  // GridUniforms{gridWidth,gridLen} uniform (8 bytes, 2x u32) straight from
  // the WaterSystem's own resolution/cellCount -- UNLIKE the render path's
  // sceneSelenaGridUniformData (StateGrid{gridWidth,gridHeight}, sourced from
  // renderContext.grid), gridLen here is the total cell COUNT (dispatch
  // bounds check), not a second grid dimension. See design doc 3.4.2/3.5:
  // "resolution/cellCount are supplied to kernels ONLY via GridUniforms, not
  // as context fields".
  function sceneSelenaComputeGridUniformData(system) {
    var resolution = Math.max(1, Math.floor(sceneNumber(system && system.resolution, 1)));
    var cellCount = Math.max(1, Math.floor(sceneNumber(system && system.cellCount, resolution * resolution)));
    return new Uint32Array([resolution, cellCount]);
  }

  function wgpuToneMapMode(environment, usePostProcessing) {
    if (usePostProcessing) return 4;
    var mode = environment && typeof environment.toneMapping === "string"
      ? environment.toneMapping.trim().toLowerCase()
      : "";
    if (mode === "linear" || mode === "none") return 0;
    if (mode === "reinhard") return 2;
    if (mode === "filmic") return 3;
    return 1;
  }

  function webGPUIBLProduct(ibl, name, role, view, format) {
    var source = ibl && ibl[name] && typeof ibl[name] === "object" ? ibl[name] : null;
    if (!source || typeof source.uri !== "string" || !source.uri.trim()) return null;
    var descriptor = wgpuTextureDescriptor(source, source.uri, role, "linear");
    if (descriptor.role !== role || descriptor.colorSpace !== "linear" || descriptor.view !== view) return null;
    if (descriptor.format && descriptor.format !== format) return null;
    return descriptor;
  }

  function webGPUIBLRecordValid(record, descriptor, brdfModel) {
    if (!record || !record.loaded || record.failed) return false;
    if (!descriptor || descriptor.width <= 0 || descriptor.height <= 0 || descriptor.faces <= 0 || descriptor.mipLevels <= 0) return false;
    var metadata = record.keyValues || {};
    if (metadata.GoSXiblRole !== descriptor.role) return false;
    if (metadata.GoSXColorSpace !== "linear") return false;
    if (brdfModel && metadata.GoSXiblModel !== brdfModel) return false;
    var expectedVKFormat = descriptor.format === "rgba16f" ? 97 : (descriptor.format === "rg16f" ? 83 : 0);
    if (!expectedVKFormat || record.vkFormat !== expectedVKFormat) return false;
    if (record.width !== descriptor.width || record.height !== descriptor.height) return false;
    if (record.faces !== descriptor.faces || record.levels !== descriptor.mipLevels) return false;
    return true;
  }

  function webGPUIBLRoughnessMappingValid(ibl, mipLevels) {
    var mapping = ibl && Array.isArray(ibl.roughnessPerLevel) ? ibl.roughnessPerLevel : [];
    if (mipLevels <= 0 || mapping.length !== mipLevels) return false;
    for (var i = 0; i < mapping.length; i++) {
      var expected = mipLevels <= 1 ? 0 : i / (mipLevels - 1);
      if (!Number.isFinite(mapping[i]) || Math.abs(mapping[i] - expected) > 0.000001) return false;
    }
    return true;
  }

  // Normal-incidence dielectric Fresnel for an authored material IOR:
  // F0 = ((ior-1)/(ior+1))^2. Total function — missing, null, invalid,
  // non-finite, negative and 0<ior<1 inputs fall back to the default ior
  // 1.5 (F0 0.04); the glTF explicit-zero compatibility mode maps to a
  // Fresnel of exactly 1. The (ior-1)/(ior+1) form stays stable for huge
  // finite inputs and the result always fits float32 for upload.
  function sceneWebGPUDielectricF0(ior) {
    var value = typeof ior === "number"
      ? ior
      : (typeof ior === "string" && ior.trim() !== "" ? Number(ior) : NaN);
    if (!(Number.isFinite(value) && (value >= 1 || value === 0))) {
      value = 1.5;
    }
    if (value === 0) {
      return 1;
    }
    var t = (value - 1) / (value + 1);
    return t * t;
  }

  // Effective dielectric specular factors from the authored KHR-style
  // specularIntensity / specularColor factors. The intensity contract is a
  // finite number in [0, 1] — an explicit 0 is valid, an omitted value
  // means 1 — and the color contract is exactly three finite non-negative
  // LINEAR components, omitted meaning white. The effective F0 is
  // min(IOR F0 * color, 1) * intensity with the clamp applied BEFORE the
  // intensity so a finite HDR tint above 1 clamps to 1 rather than scaling
  // past it, and F90 is the intensity itself. Every returned component is
  // finite and non-negative, so the packed buffer can never see NaN or
  // Infinity, and the result is bounded to [0, 1]. A future specular
  // texture must multiply its colour into `color` BEFORE this clamp, never
  // into an already-clamped F0.
  function sceneWebGPUSpecularFactors(material) {
    var mat = material || {};
    var intensity = mat.specularIntensity;
    if (!(typeof intensity === "number" && Number.isFinite(intensity) && intensity >= 0 && intensity <= 1)) {
      intensity = 1;
    }
    var color = mat.specularColor;
    var valid = Boolean(color) && typeof color.length === "number" && color.length === 3;
    if (valid) {
      for (var i = 0; i < 3; i++) {
        var component = color[i];
        if (!(typeof component === "number" && Number.isFinite(component) && component >= 0)) {
          valid = false;
          break;
        }
      }
    }
    if (!valid) {
      color = [1, 1, 1];
    }
    var iorF0 = sceneWebGPUDielectricF0(mat.ior);
    return {
      f0: [
        Math.min(iorF0 * color[0], 1) * intensity,
        Math.min(iorF0 * color[1], 1) * intensity,
        Math.min(iorF0 * color[2], 1) * intensity,
      ],
      f90: intensity,
    };
  }

  // Log-space coefficients for the optional specular-colour texture:
  // log2(IOR F0) + log2(authored colour) per channel, so the shader can add
  // log2 of the sampled texel and exp2 back the unclamped HDR product
  // without ever forming a float32 overflow on the CPU. Exact-zero channels
  // (IOR F0 or colour component 0) use a finite sentinel far below any real
  // log2 value; the shader maps the sentinel to zero. Invalid or omitted
  // colour arrays fall back to white, matching sceneWebGPUSpecularFactors.
  function sceneWebGPUSpecularColorLogs(material) {
    var mat = material || {};
    var color = mat.specularColor;
    var valid = Boolean(color) && typeof color.length === "number" && color.length === 3;
    if (valid) {
      for (var i = 0; i < 3; i++) {
        var component = color[i];
        if (!(typeof component === "number" && Number.isFinite(component) && component >= 0)) {
          valid = false;
          break;
        }
      }
    }
    var iorF0 = sceneWebGPUDielectricF0(mat.ior);
    var out = [0, 0, 0];
    for (var j = 0; j < 3; j++) {
      var c = valid ? color[j] : 1;
      out[j] = (iorF0 > 0 && c > 0) ? Math.log2(iorF0) + Math.log2(c) : -1e30;
    }
    return out;
  }

  // Whether the material carries a valid authored emissive colour factor:
  // exactly three finite, non-negative components (the glTF emissiveFactor
  // triple). Mirrors the WebGL2 renderer's scenePBRHasEmissiveColor. The
  // gltf.ts loader always sets this, even to [0, 0, 0]; hand-authored
  // materials that only set the scalar `emissive` glow knob never do,
  // which keeps their pre-existing albedo-tinted glow in the shader.
  function sceneWebGPUHasEmissiveColor(material) {
    var color = material && material.emissiveColor;
    if (!(color && typeof color.length === "number" && color.length === 3)) {
      return false;
    }
    for (var i = 0; i < 3; i++) {
      var component = color[i];
      if (!(typeof component === "number" && Number.isFinite(component) && component >= 0)) {
        return false;
      }
    }
    return true;
  }

  function sceneWebGPUEmissiveColor(material) {
    var mat = material || {};
    return sceneWebGPUHasEmissiveColor(mat) ? mat.emissiveColor : [0, 0, 0];
  }

  // Rim highlight tint. Off by default (rimStrength 0 in materialUniformData).
  // Mirrors scenePBRRimColor on the WebGL2 renderer.
  function sceneWebGPURimColor(material) {
    var color = material && material.rimColor;
    var valid = Boolean(color) && typeof color.length === "number" && color.length === 3;
    if (valid) {
      for (var i = 0; i < 3; i++) {
        var component = color[i];
        if (!(typeof component === "number" && Number.isFinite(component) && component >= 0)) {
          valid = false;
          break;
        }
      }
    }
    return valid ? color : [1, 1, 1];
  }

  function webGPUObjectVertexCount(obj) {
    return Math.max(0, Math.floor(sceneNumber(obj && obj.vertexCount, obj && obj.vertices && obj.vertices.count || 0)));
  }

  function webGPUDirectAttribute(obj, key, count, tupleSize) {
    var vertices = obj && obj.vertices;
    var isCustom = typeof key === "string" && key.indexOf("custom:") === 0;
    var data;
    if (isCustom) {
      // Custom streams resolve fail-closed from vertices.attributes only.
      var attrs = vertices && typeof vertices.attributes === "object" ? vertices.attributes : null;
      var entry = attrs ? attrs[key.slice("custom:".length)] : null;
      if (
        !entry ||
        typeof entry !== "object" ||
        !(entry.data instanceof Float32Array) ||
        entry.itemSize !== tupleSize
      ) {
        return null;
      }
      data = entry.data;
    } else {
      data = vertices && vertices[key];
      if (!vertices || !data || typeof data.length !== "number") {
        return null;
      }
    }
    var required = Math.max(0, Math.floor(sceneNumber(count, 0))) * Math.max(1, tupleSize);
    if (required <= 0 || data.length < required) {
      return null;
    }
    if (!isCustom && !(data instanceof Float32Array)) {
      data = new Float32Array(data);
      vertices[key] = data;
    }
    if (data.length === required) {
      return data;
    }
    var views = vertices._wgpuAttributeViews;
    if (!views) {
      views = Object.create(null);
      vertices._wgpuAttributeViews = views;
    }
    var viewKey = key + ":" + required;
    var record = views[viewKey];
    if (!record || record.source !== data) {
      record = {
        source: data,
        view: data.subarray(0, required),
      };
      views[viewKey] = record;
    }
    return record.view;
  }

  function webGPUObjectIsSkinned(obj) {
    var count = webGPUObjectVertexCount(obj);
    var vertices = obj && obj.vertices;
    var skin = obj && obj.skin;
    var jointMatrices = skin && skin.jointMatrices;
    return Boolean(
      count > 0 &&
      vertices &&
      skin &&
      jointMatrices &&
      typeof jointMatrices.length === "number" &&
      jointMatrices.length >= 16 &&
      webGPUDirectAttribute(obj, "positions", count, 3) &&
      webGPUDirectAttribute(obj, "joints", count, 4) &&
      webGPUDirectAttribute(obj, "weights", count, 4)
    );
  }

  function webGPUMat4MultiplyInto(out, outOffset, a, b, bOffset) {
    for (var col = 0; col < 4; col++) {
      var bi = bOffset + col * 4;
      var b0 = sceneNumber(b[bi], col === 0 ? 1 : 0);
      var b1 = sceneNumber(b[bi + 1], col === 1 ? 1 : 0);
      var b2 = sceneNumber(b[bi + 2], col === 2 ? 1 : 0);
      var b3 = sceneNumber(b[bi + 3], col === 3 ? 1 : 0);
      out[outOffset + col * 4] = sceneNumber(a[0], 1) * b0 + sceneNumber(a[4], 0) * b1 + sceneNumber(a[8], 0) * b2 + sceneNumber(a[12], 0) * b3;
      out[outOffset + col * 4 + 1] = sceneNumber(a[1], 0) * b0 + sceneNumber(a[5], 1) * b1 + sceneNumber(a[9], 0) * b2 + sceneNumber(a[13], 0) * b3;
      out[outOffset + col * 4 + 2] = sceneNumber(a[2], 0) * b0 + sceneNumber(a[6], 0) * b1 + sceneNumber(a[10], 1) * b2 + sceneNumber(a[14], 0) * b3;
      out[outOffset + col * 4 + 3] = sceneNumber(a[3], 0) * b0 + sceneNumber(a[7], 0) * b1 + sceneNumber(a[11], 0) * b2 + sceneNumber(a[15], 1) * b3;
    }
  }

  function webGPUElioBoneData(obj, jointCount) {
    var skin = obj && obj.skin;
    var jointMatrices = skin && skin.jointMatrices;
    if (!skin || !jointMatrices || typeof jointMatrices.length !== "number" || jointCount <= 0) return null;
    var data = skin._gosxWGPUElioBoneData;
    if (!data || data.length !== jointCount * 16) {
      data = new Float32Array(jointCount * 16);
      skin._gosxWGPUElioBoneData = data;
    }
    var model = webGPUObjectModelMatrix(obj);
    for (var i = 0; i < jointCount; i++) {
      webGPUMat4MultiplyInto(data, i * 16, model, jointMatrices, i * 16);
    }
    return data;
  }

  // webGPUObjectResourceOwner returns the object that owns a draw record's
  // per-object GPU state. The scene rebuilds each mesh draw record every
  // frame; resourceOwner is the scene object behind it, which lasts. State
  // cached on the record itself is lost at the end of the frame, and the
  // GPU buffers it held stay allocated until the renderer is disposed.
  function webGPUObjectResourceOwner(obj: any) {
    return obj && obj.resourceOwner || obj;
  }

  function webGPUComputedMorphArray(morph, key, count, components) {
    var required = Math.max(0, Math.floor(sceneNumber(count, 0))) * Math.max(1, Math.floor(sceneNumber(components, 1)));
    var source = morph && morph[key];
    if (!source || required <= 0 || typeof source.length !== "number" || source.length < required) return null;
    var typed = source instanceof Float32Array ? source : toSceneFloat32Array(source);
    if (typed !== source && morph) morph[key] = typed;
    return typed.length === required ? typed : typed.subarray(0, required);
  }

  function webGPUComputedMorphDefaultArray(morph, key, count, components, defaults) {
    var required = Math.max(0, Math.floor(sceneNumber(count, 0))) * Math.max(1, Math.floor(sceneNumber(components, 1)));
    if (required <= 0) return null;
    var data = morph && morph[key];
    if (!data || data.length !== required) {
      data = new Float32Array(required);
      var width = Math.max(1, Math.floor(sceneNumber(components, 1)));
      for (var i = 0; i < count; i++) {
        for (var c = 0; c < width; c++) {
          data[i * width + c] = sceneNumber(defaults && defaults[c], 0);
        }
      }
      if (morph) morph[key] = data;
    }
    return data;
  }

  function webGPUComputedMorphUniformData(obj, morph, count) {
    var data = morph && morph._gosxWGPUComputedMorphUniformData;
    if (!data || data.length !== 20) {
      data = new Float32Array(20);
      if (morph) morph._gosxWGPUComputedMorphUniformData = data;
    }
    var matrix = morph && morph.modelMatrix || webGPUObjectModelMatrix(obj);
    for (var i = 0; i < 16; i++) {
      data[i] = sceneNumber(matrix && matrix[i], i % 5 === 0 ? 1 : 0);
    }
    data[16] = Math.max(0, Math.min(1, sceneNumber(morph && morph.alpha, 0.45)));
    data[17] = Math.max(0, Math.floor(sceneNumber(count, 0)));
    data[18] = 0;
    data[19] = 0;
    return data;
  }

  function webGPUComputedMorphPackedData(morph, slot, count, positions, normals, tangents) {
    if (!morph || !positions || !normals || !tangents) return null;
    var cache = morph[slot];
    if (
      cache &&
      cache.count === count &&
      cache.positions === positions &&
      cache.normals === normals &&
      cache.tangents === tangents
    ) {
      return cache.data;
    }
    var data = new Float32Array(Math.max(0, count) * 10);
    for (var i = 0; i < count; i++) {
      var p = i * 3;
      var t = i * 4;
      var out = i * 10;
      data[out] = sceneNumber(positions[p], 0);
      data[out + 1] = sceneNumber(positions[p + 1], 0);
      data[out + 2] = sceneNumber(positions[p + 2], 0);
      data[out + 3] = sceneNumber(normals[p], 0);
      data[out + 4] = sceneNumber(normals[p + 1], 0);
      data[out + 5] = sceneNumber(normals[p + 2], 1);
      data[out + 6] = sceneNumber(tangents[t], 1);
      data[out + 7] = sceneNumber(tangents[t + 1], 0);
      data[out + 8] = sceneNumber(tangents[t + 2], 0);
      data[out + 9] = sceneNumber(tangents[t + 3], 1);
    }
    morph[slot] = {
      count: count,
      positions: positions,
      normals: normals,
      tangents: tangents,
      data: data,
    };
    return data;
  }

  function webGPUObjectComputedMorphDrawRecord(obj) {
    var record = obj && obj._gosxWGPUComputedMorphRecord;
    return record && record.positionBuffer && record.normalBuffer && record.tangentBuffer ? record : null;
  }

  function webGPUBindComputedMorphBuffer(pass, slot, buffer, count, components) {
    if (!buffer) return false;
    var byteSize = Math.max(4, Math.max(0, Math.floor(sceneNumber(count, 0))) * Math.max(1, components) * 4);
    pass.setVertexBuffer(slot, buffer, 0, byteSize);
    return true;
  }

  function webGPUCountSkinnedMeshes(bundle) {
    var objects = Array.isArray(bundle && bundle.meshObjects) ? bundle.meshObjects : [];
    var count = 0;
    for (var i = 0; i < objects.length; i++) {
      if (webGPUObjectIsSkinned(objects[i])) count++;
    }
    return count;
  }

  // webGPUCountViewCulledMeshObjects counts bundle.meshObjects entries with
  // viewCulled === true -- the CPU frustum cull applied before buildDrawList
  // ever hands an object to drawPBRObjects. data-gosx-scene3d-webgpu-mesh-objects
  // publishes bundle.meshObjects.length UNCONDITIONALLY (a bundle/authoring
  // count, not a "what actually drew" count), so a viewCulled object still
  // counts there -- that ambiguity is exactly what let three Selena mesh
  // planes read "3" on data-gosx-scene3d-webgpu-mesh-objects for ~two weeks
  // while a camera-depth sign error CPU-frustum-culled them to zero pixels.
  // Pairing this SUBMITTED/CULLED split (mesh-draw-calls vs mesh-view-culled)
  // alongside the bundle count, mirroring the point/compute-particle
  // draw-call counters, closes that diagnostic gap.
  function webGPUCountViewCulledMeshObjects(bundle) {
    var objects = Array.isArray(bundle && bundle.meshObjects) ? bundle.meshObjects : [];
    var count = 0;
    for (var i = 0; i < objects.length; i++) {
      if (objects[i] && objects[i].viewCulled) count++;
    }
    return count;
  }

  // webGPUCountUndrawableMeshObjects counts entries buildDrawList rejects for
  // DEGENERATE GEOMETRY (non-finite vertexOffset/vertexCount, or zero
  // vertices) rather than for frustum culling. Without it the accounting is
  // open-ended: meshObjects - meshDrawCalls - meshViewCulled leaves an
  // unexplained remainder that could be a cull bug, a planner bug or a
  // geometry bug. With it the identity
  //     meshObjects == meshDrawCalls + meshViewCulled + meshUndrawable
  // closes, and any violation is itself a reportable defect.
  function webGPUCountUndrawableMeshObjects(bundle) {
    var objects = Array.isArray(bundle && bundle.meshObjects) ? bundle.meshObjects : [];
    var count = 0;
    for (var i = 0; i < objects.length; i++) {
      var obj = objects[i];
      if (!obj || obj.viewCulled) continue;
      if (!Number.isFinite(obj.vertexOffset) || !Number.isFinite(obj.vertexCount) || obj.vertexCount <= 0) count++;
    }
    return count;
  }

  function webGPUSceneMeshVertexCount(bundle) {
    var count = Math.max(0, Math.floor(sceneNumber(bundle && bundle.worldMeshVertexCount, 0))), positions = bundle && bundle.worldMeshPositions;
    if (positions && Number.isFinite(positions.length)) {
      count = Math.max(count, Math.floor(positions.length / 3));
    }
    var objects = Array.isArray(bundle && bundle.meshObjects) ? bundle.meshObjects : [];
    for (var obj of objects) {
      if (!sceneWebGPUUsesSharedMeshStreams(obj)) continue;
      count = Math.max(count, Math.floor(sceneNumber(obj.vertexOffset, 0) + sceneNumber(obj.vertexCount, 0)));
    }
    return count;
  }

  function webGPUSceneMeshAttributeData(bundle, key, components, defaults, vertexCount) {
    var required = Math.max(0, Math.floor(vertexCount || 0)) * Math.max(1, components);
    var source = bundle && bundle[key];
    if (source && typeof source.length === "number" && source.length >= required) {
      var typed = toSceneFloat32Array(source);
      if (typed !== source && bundle) bundle[key] = typed;
      return typed.length === required ? typed : typed.subarray(0, required);
    }

    var cacheKey = "_gosxWGPUDefault" + key;
    var cacheCountKey = cacheKey + "VertexCount";
    var data = bundle && bundle[cacheKey];
    if (!data || data.length !== required || bundle[cacheCountKey] !== vertexCount) {
      data = new Float32Array(required);
      var stride = Math.max(1, components);
      for (var i = 0; i < vertexCount; i++) {
        for (var c = 0; c < stride; c++) {
          data[i * stride + c] = sceneNumber(defaults && defaults[c], 0);
        }
      }
      if (bundle) {
        bundle[cacheKey] = data;
        bundle[cacheCountKey] = vertexCount;
      }
    }
    return data;
  }

  function webGPUBindSceneMeshVertexBuffer(pass, slot, record, vertexOffset, vertexCount) {
    if (!record || !record.buffer) return false;
    var components = Math.max(1, Math.floor(sceneNumber(record.components, 1)));
    var offset = Math.max(0, Math.floor(sceneNumber(vertexOffset, 0)));
    var count = Math.max(0, Math.floor(sceneNumber(vertexCount, 0)));
    var byteOffset = offset * components * 4;
    var byteSize = Math.max(4, count * components * 4);
    pass.setVertexBuffer(slot, record.buffer, byteOffset, byteSize);
    return true;
  }

  function buildDrawList(bundle) {
    var objects = Array.isArray(bundle && bundle.meshObjects) ? bundle.meshObjects : [];
    var materials = Array.isArray(bundle.materials) ? bundle.materials : [];
    var opaque = [];
    var alpha = [];
    var additive = [];

    for (var i = 0; i < objects.length; i++) {
      var obj = objects[i];
      if (!obj || obj.viewCulled) continue;
      if (!Number.isFinite(obj.vertexOffset) || !Number.isFinite(obj.vertexCount) || obj.vertexCount <= 0) continue;
      var mat = materials[obj.materialIndex] || null;
      var pass = scenePBRObjectRenderPass(obj, mat);
      if (pass === "alpha") alpha.push(obj);
      else if (pass === "additive") additive.push(obj);
      else opaque.push(obj);
    }

    alpha.sort(scenePBRDepthSort);
    additive.sort(scenePBRDepthSort);

    return { opaque: opaque, alpha: alpha, additive: additive };
  }

  function instancedMeshCount(mesh) {
    if (!mesh) return 0;
    return Math.max(0, Math.floor(sceneNumber(mesh.instanceCount, sceneNumber(mesh.count, 0))));
  }
  function instancedMeshMaterial(mesh, materials) {
    var mat = materials[sceneNumber(mesh && mesh.materialIndex, 0)] || null;
    if (mat) return mat;
    return {
      color: mesh && mesh.color || "#8de1ff",
      roughness: sceneNumber(mesh && mesh.roughness, 0.5),
      metalness: sceneNumber(mesh && mesh.metalness, 0),
      emissive: sceneNumber(mesh && mesh.emissive, 0),
      opacity: clamp01(sceneNumber(mesh && mesh.opacity, 1)),
      unlit: mesh && mesh.materialKind === "flat",
      renderPass: mesh && mesh.renderPass,
    };
  }
  // webGPUInstancedCullRadius returns a bounding-sphere radius that encloses
  // the mesh's UNSCALED local geometry, padded by 5%. The cull kernel scales
  // it per instance, so this value must never under-estimate the primitive.
  // The pad covers the torus-knot envelope, which instancedLocalBounds
  // approximates.
  function webGPUInstancedCullRadius(mesh) {
    var b = instancedLocalBounds(mesh);
    if (!b) return 2;
    var hx = Math.max(Math.abs(b.minX), Math.abs(b.maxX));
    var hy = Math.max(Math.abs(b.minY), Math.abs(b.maxY));
    var hz = Math.max(Math.abs(b.minZ), Math.abs(b.maxZ));
    var radius = Math.sqrt(hx * hx + hy * hy + hz * hz) * 1.05;
    return radius > 0 ? radius : 2;
  }

  // webGPUBuiltinCullEligible decides whether a mesh with no authored cull
  // kernel may use the renderer's own kernel. Three conditions, each of them a
  // correctness or a payoff limit:
  //
  //   1. The page has not opted out.
  //   2. The mesh carries no per-instance colours. The culled draw path binds
  //      the compacted 80-byte record, whose last vec4 is pick data, not
  //      colour, so a coloured mesh would lose its colours.
  //   3. The instance count is high enough that one compute dispatch and one
  //      indirect draw beat a plain draw-all.
  function webGPUBuiltinCullEligible(mesh) {
    if (!mesh) return false;
    if (typeof window !== "undefined" && window.__gosx_scene3d_webgpu_builtin_cull === false) return false;
    var colors = mesh.colors;
    if (colors && typeof colors.length === "number" && colors.length > 0) return false;
    return instancedMeshCount(mesh) >= SCENE_WEBGPU_BUILTIN_CULL_MIN_INSTANCES;
  }

  function instancedLocalBounds(mesh) {
    var kind = typeof normalizeInstancedGeometryKind === "function"
      ? normalizeInstancedGeometryKind(mesh && mesh.kind)
      : (typeof mesh.kind === "string" ? mesh.kind.toLowerCase() : "box");
    if (kind === "sphere") {
      var radius = Math.max(0.0001, sceneNumber(mesh.radius, 0.5));
      return { minX: -radius, minY: -radius, minZ: -radius, maxX: radius, maxY: radius, maxZ: radius };
    }
    if (kind === "cylinder" || kind === "cone") {
      var top = kind === "cone" ? 0 : Math.max(0, sceneNumber(mesh.radiusTop, sceneNumber(mesh.radius, 0.5)));
      var bottom = Math.max(0, sceneNumber(mesh.radiusBottom, sceneNumber(mesh.radius, 0.5)));
      var cylinderRadius = Math.max(top, bottom);
      var cylinderHeight = Math.max(0.0001, sceneNumber(mesh.height, 1));
      return { minX: -cylinderRadius, minY: -cylinderHeight * 0.5, minZ: -cylinderRadius, maxX: cylinderRadius, maxY: cylinderHeight * 0.5, maxZ: cylinderRadius };
    }
    if (kind === "torus") {
      var major = Math.max(0.0001, sceneNumber(mesh.radius, 0.7));
      var tube = Math.max(0.0001, sceneNumber(mesh.tube, 0.3));
      var torusRadius = major + tube;
      return { minX: -torusRadius, minY: -tube, minZ: -torusRadius, maxX: torusRadius, maxY: tube, maxZ: torusRadius };
    }
    if (kind === "torusknot") {
      // (p=2,q=3): major envelope ≈ radius*(2+1)*0.5 + tube = 1.5*radius + tube
      var knMajor = Math.max(0.0001, sceneNumber(mesh.radius, 0.17));
      var knTube = Math.max(0.0001, sceneNumber(mesh.tube, 0.045));
      var knEnvelope = knMajor * 1.5 + knTube;
      var knHeight = knMajor * 0.5 + knTube;
      return { minX: -knEnvelope, minY: -knHeight, minZ: -knEnvelope, maxX: knEnvelope, maxY: knHeight, maxZ: knEnvelope };
    }
    var w = Math.max(0.0001, sceneNumber(mesh.width, 1));
    var h = kind === "plane" ? 0 : Math.max(0.0001, sceneNumber(mesh.height, 1));
    var d = Math.max(0.0001, sceneNumber(mesh.depth, 1));
    return { minX: -w * 0.5, minY: -h * 0.5, minZ: -d * 0.5, maxX: w * 0.5, maxY: h * 0.5, maxZ: d * 0.5 };
  }

  function expandBoundsPoint(bounds, x, y, z) {
    if (!bounds) return { minX: x, minY: y, minZ: z, maxX: x, maxY: y, maxZ: z };
    if (x < bounds.minX) bounds.minX = x;
    if (y < bounds.minY) bounds.minY = y;
    if (z < bounds.minZ) bounds.minZ = z;
    if (x > bounds.maxX) bounds.maxX = x;
    if (y > bounds.maxY) bounds.maxY = y;
    if (z > bounds.maxZ) bounds.maxZ = z;
    return bounds;
  }

  function expandInstancedBounds(bounds, mesh, transformData, count) {
    var b = instancedLocalBounds(mesh);
    var xs = [b.minX, b.maxX];
    var ys = [b.minY, b.maxY];
    var zs = [b.minZ, b.maxZ];
    for (var ii = 0; ii < count; ii++) {
      var base = ii * 16;
      for (var xi = 0; xi < 2; xi++) {
        for (var yi = 0; yi < 2; yi++) {
          for (var zi = 0; zi < 2; zi++) {
            var x = xs[xi], y = ys[yi], z = zs[zi];
            bounds = expandBoundsPoint(bounds,
              transformData[base + 0] * x + transformData[base + 4] * y + transformData[base + 8] * z + transformData[base + 12],
              transformData[base + 1] * x + transformData[base + 5] * y + transformData[base + 9] * z + transformData[base + 13],
              transformData[base + 2] * x + transformData[base + 6] * y + transformData[base + 10] * z + transformData[base + 14]
            );
          }
        }
      }
    }
    return bounds;
  }

  function toSceneFloat32Array(values) {
    if (values instanceof Float32Array) return values;
    if (!values || typeof values.length !== "number") return new Float32Array(0);
    return new Float32Array(values);
  }

  function webGPUUnsupportedLineStyles(bundle) {
    var dashes = bundle && bundle.worldLineDashes;
    if (dashes && typeof dashes.length === "number") {
      for (var di = 0; di < dashes.length; di++) {
        if (dashes[di]) return true;
      }
    }
    var lines = Array.isArray(bundle && bundle.lines) ? bundle.lines : [];
    for (var li = 0; li < lines.length; li++) {
      var line = lines[li];
      if (!line) continue;
      if (line.lineDash) return true;
      var material = line.material && typeof line.material === "object" ? line.material : null;
      var materialKind = String(line.materialKind || line.kind || material && material.kind || "").toLowerCase();
      if (material && material.lineDash) return true;
      if (materialKind === "line-dashed" || materialKind === "dashed") return true;
    }
    return false;
  }

  function webGPUWorldLineSegmentCount(bundle) {
    return Math.max(0, Math.floor(sceneNumber(bundle && bundle.worldVertexCount, 0) / 2));
  }

  function webGPUHasThickWorldLines(bundle) {
    var widths = bundle && bundle.worldLineWidths;
    if (widths && typeof widths.length === "number") {
      for (var i = 0; i < widths.length; i++) {
        if (sceneNumber(widths[i], 0) > 1) return true;
      }
    }
    var lines = Array.isArray(bundle && bundle.lines) ? bundle.lines : [];
    for (var li = 0; li < lines.length; li++) {
      if (sceneNumber(lines[li] && lines[li].lineWidth, 0) > 1) return true;
    }
    return false;
  }

  function webGPUCanUseThickWorldLines(bundle) {
    if (!webGPUHasThickWorldLines(bundle)) return true;
    if (typeof createSceneThickLineScratch !== "function" || typeof expandSceneThickLineIntoScratch !== "function") return false;
    var segmentCount = webGPUWorldLineSegmentCount(bundle);
    return segmentCount > 0 && segmentCount <= 16384;
  }

  function hasWorldLineData(bundle) {
    return Boolean(
      bundle &&
      !webGPUUnsupportedLineStyles(bundle) &&
      webGPUCanUseThickWorldLines(bundle) &&
      bundle.worldPositions &&
      bundle.worldColors &&
      Number(bundle.worldVertexCount || 0) > 0
    );
  }

  function hasScreenLineData(bundle) {
    return Boolean(
      bundle &&
      !hasWorldLineData(bundle) &&
      !webGPUUnsupportedLineStyles(bundle) &&
      bundle.positions &&
      bundle.colors &&
      Number(bundle.vertexCount || 0) > 0
    );
  }

  function hasSurfaceData(bundle) {
    var surfaces = Array.isArray(bundle && bundle.surfaces) ? bundle.surfaces : [];
    for (var i = 0; i < surfaces.length; i++) {
      var surface = surfaces[i];
      if (surface && !surface.viewCulled && sceneNumber(surface.vertexCount, 0) > 0) return true;
    }
    return false;
  }

  function fallbackMaterialData(owner, vertexCount) {
    var count = Math.max(0, Math.floor(sceneNumber(vertexCount, 0)));
    if (
      owner &&
      owner._gosxWGPUFallbackMaterialData &&
      owner._gosxWGPUFallbackMaterialCount === count
    ) {
      return owner._gosxWGPUFallbackMaterialData;
    }
    var data = new Float32Array(count * 3);
    for (var i = 0; i < count; i++) {
      data[i * 3] = 0;
      data[i * 3 + 1] = 0;
      data[i * 3 + 2] = 1;
    }
    if (owner) {
      owner._gosxWGPUFallbackMaterialData = data;
      owner._gosxWGPUFallbackMaterialCount = count;
    }
    return data;
  }

  function screenLinePositionData(bundle) {
    var source = bundle && bundle.positions;
    var count = Math.max(0, Math.floor(sceneNumber(bundle && bundle.vertexCount, 0)));
    if (
      bundle &&
      bundle._gosxWGPUScreenLineSource === source &&
      bundle._gosxWGPUScreenLineCount === count &&
      bundle._gosxWGPUScreenLinePositions
    ) {
      return bundle._gosxWGPUScreenLinePositions;
    }
    var src = toSceneFloat32Array(source);
    var data = new Float32Array(count * 3);
    for (var i = 0; i < count; i++) {
      data[i * 3] = src[i * 2] || 0;
      data[i * 3 + 1] = src[i * 2 + 1] || 0;
      data[i * 3 + 2] = 0;
    }
    if (bundle) {
      bundle._gosxWGPUScreenLineSource = source;
      bundle._gosxWGPUScreenLineCount = count;
      bundle._gosxWGPUScreenLinePositions = data;
    }
    return data;
  }

  function primitiveVertexCount(positions, colors, materials, requested) {
    var positionCount = Math.floor((positions && positions.length || 0) / 3);
    var colorCount = Math.floor((colors && colors.length || 0) / 4);
    var materialCount = Math.floor((materials && materials.length || 0) / 3);
    var maxCount = Math.max(0, Math.min(positionCount, colorCount, materialCount));
    var count = Math.max(0, Math.floor(sceneNumber(requested, maxCount)));
    return Math.min(count, maxCount);
  }

  function linePassDepthWrite(blendMode) {
    return blendMode !== "alpha" && blendMode !== "additive";
  }

  function thickLinePassIndexData(record, passName) {
    var scratch = record && record.scratch;
    if (!scratch) return null;
    if (passName === "additive") {
      return { slot: "_gosxWGPUThickLineAdditiveIndex", data: scratch.additiveIndices.subarray(0, scratch.additiveIndexCount), count: scratch.additiveIndexCount };
    }
    if (passName === "alpha") {
      return { slot: "_gosxWGPUThickLineAlphaIndex", data: scratch.alphaIndices.subarray(0, scratch.alphaIndexCount), count: scratch.alphaIndexCount };
    }
    return { slot: "_gosxWGPUThickLineOpaqueIndex", data: scratch.opaqueIndices.subarray(0, scratch.opaqueIndexCount), count: scratch.opaqueIndexCount };
  }

  function surfaceEntries(bundle, renderPass) {
    var surfaces = Array.isArray(bundle && bundle.surfaces) ? bundle.surfaces.slice() : [];
    var filtered = [];
    for (var i = 0; i < surfaces.length; i++) {
      var surface = surfaces[i];
      if (!surface || surface.viewCulled) continue;
      if (Math.max(0, Math.floor(sceneNumber(surface.vertexCount, 0))) <= 0) continue;
      if (String(surface.renderPass || "opaque") !== renderPass) continue;
      filtered.push(surface);
    }
    if (renderPass !== "opaque") {
      filtered.sort(function(left, right) {
        var leftDepth = sceneNumber(left && left.depthCenter, 0);
        var rightDepth = sceneNumber(right && right.depthCenter, 0);
        if (leftDepth !== rightDepth) return rightDepth - leftDepth;
        return String(left && left.id || "").localeCompare(String(right && right.id || ""));
      });
    }
    return filtered;
  }

  function webGPUEmptyPointDrawStats() {
    return {
      pointDrawEntries: 0,
      pointDrawInstances: 0,
      pointDrawCalls: 0,
      pointAuthoredDrawEntries: 0,
      pointAuthoredDrawInstances: 0,
      pointAuthoredDrawCalls: 0,
      pointSkippedEmpty: 0,
      pointSkippedNoPositions: 0,
    };
  }

  function webGPUPlannedPointStats(bundle, computeParticleRecords) {
    var pointsArray = Array.isArray(bundle && bundle.points) ? bundle.points : [];
    var pointInstances = 0;
    for (var i = 0; i < pointsArray.length; i++) {
      pointInstances += Math.max(0, Math.floor(sceneNumber(pointsArray[i] && pointsArray[i].count, 0)));
    }
    var computeRecords = Array.isArray(computeParticleRecords) ? computeParticleRecords : [];
    var computeInstances = 0;
    for (var c = 0; c < computeRecords.length; c++) {
      var system = computeRecords[c] && computeRecords[c].system;
      computeInstances += Math.max(0, Math.floor(sceneNumber(system && system.count, 0)));
    }
    return {
      pointEntries: pointsArray.length,
      pointInstances: pointInstances,
      computeParticleEntries: computeRecords.length,
      computeParticleInstances: computeInstances,
    };
  }

  function webGPUPlannedInstanceCount(list) {
    var total = 0;
    var source = Array.isArray(list) ? list : [];
    for (var i = 0; i < source.length; i++) {
      total += Math.max(0, Math.floor(sceneNumber(source[i] && source[i].count, 0)));
    }
    return total;
  }

  function isWebGPUErrorScopeLifecycleMessage(message) {
    var text = String(message || "").toLowerCase();
    return text.indexOf("instance dropped") >= 0 && text.indexOf("poperrorscope") >= 0;
  }

  // adaptOrtho2DBoardBundle is the Go-wire ↔ 16a seam for the 2D board path.
  //
  // The Go board pipeline (render/bundle2d/bundle2d_gpu.go documents the Go
  // half) marshals engine.RenderBundle JSON in the NATIVE renderer
  // vocabulary: rect quads live in `objects` (vertexOffset/vertexCount/
  // materialIndex) slicing `worldPositions`/`worldNormals`/`worldUVs`. 16a
  // draws the same geometry from the JS scene-core vocabulary (`meshObjects`
  // + `worldMesh*`) — native drawObjectMeshes and 16a drawPBRObjects are the
  // two consumers of one buffer layout, so the bridge is pure ZERO-COPY
  // aliasing: no records are copied or transformed. Idempotent by
  // construction via the !bundle.meshObjects re-entry guard (hosts re-render
  // the same bundle object every frame, and 16a's attribute getter
  // canonicalizes worldMesh* fields to typed arrays in place — re-aliasing
  // would clobber that cache).
  //
  // The only per-record touch-ups are materializing the vertexOffset and
  // materialIndex zeros that Go's `omitempty` elides from the wire (the
  // first object marshals without either): 16a's draw gates require
  // Number.isFinite(vertexOffset), and buildDrawList's pass classification
  // reads materials[obj.materialIndex] directly — an elided zero would
  // mis-default the first rect's material to null.
  //
  // Board bundles can also carry `lines`/`labels`/`sprites` (engine
  // RenderLine {from,to,color,lineWidth} / RenderLabel / RenderSprite).
  // Audited against this renderer: they are inert here, so they are left
  // untouched rather than guarded —
  //   - the world/screen line draw paths key off worldColors+
  //     worldVertexCount / positions+colors+vertexCount, none of which the
  //     board bundle sets, so hasWorldLineData/hasScreenLineData stay false;
  //   - `lines` records are only read by webGPUUnsupportedLineStyles (Go
  //     lines have no lineDash/material → false) and
  //     webGPUHasThickWorldLines (lineWidth > 1 would flip supportsBundle
  //     false at backend selection; the typed CanvasBoardNode wire always
  //     emits lineWidth 1 today);
  //   - `labels`/`sprites` are not read by 16a at all.
  // TODO(M1 slice 2): line/label/sprite primitive parity gives these
  // payloads a real draw path (and revisits the ortho2d gate in
  // buildSceneWorldDrawPlan).
  //
  // `background` needs no bridging: the main-pass clear color already reads
  // bundle.background (same JSON name on Scene3D and board bundles).
  function adaptOrtho2DBoardBundle(bundle) {
    if (
      !bundle ||
      !bundle.camera ||
      bundle.camera.mode !== "ortho2d" ||
      !Array.isArray(bundle.objects) ||
      !bundle.objects.length ||
      bundle.meshObjects
    ) {
      return bundle;
    }
    for (var i = 0; i < bundle.objects.length; i++) {
      var obj = bundle.objects[i];
      if (obj && !Number.isFinite(obj.vertexOffset)) obj.vertexOffset = 0;
      if (obj && !Number.isFinite(obj.materialIndex)) obj.materialIndex = 0;
    }
    // Thumbnail level-of-detail: the board's per-page thumbnail sprites are a
    // MEDIUM-zoom tier — they read as a faithful preview only while zoomed out
    // (CANVAS_LOD_THUMB_ZOOM <= z < CANVAS_LOD_SURFACE_ZOOM); below that the
    // cards alone are the overview and at/above it the live surface owns the
    // page, so a full-card thumbnail just smears over the card fill + labels.
    // Mirror the 2D painter's gate (muddy canvas2d_painter.js) by dropping
    // sprite meshObjects outside the medium band. Geometry stays in the shared
    // buffers (just unreferenced); rects/lines/labels are unaffected.
    var BOARD_LOD_THUMB_ZOOM = 0.3;
    var BOARD_LOD_SURFACE_ZOOM = 0.8;
    var lodZoom = (typeof bundle.camera.z === "number" && bundle.camera.z > 0) ? bundle.camera.z : 1;
    var showThumbs = lodZoom >= BOARD_LOD_THUMB_ZOOM && lodZoom < BOARD_LOD_SURFACE_ZOOM;
    var lodMats = Array.isArray(bundle.materials) ? bundle.materials : [];
    var isThumbSprite = function (o) {
      var m = o && lodMats[o.materialIndex || 0];
      return !!(m && m.kind === "sprite");
    };
    // Only break the zero-copy alias when we actually drop sprites (outside the
    // medium band AND sprites are present); otherwise meshObjects aliases
    // objects by identity (rects/lines/labels and the no-sprite case unchanged).
    if (!showThumbs && bundle.objects.some(isThumbSprite)) {
      bundle.meshObjects = bundle.objects.filter(function (o) { return !isThumbSprite(o); });
    } else {
      bundle.meshObjects = bundle.objects;
    }
    bundle.worldMeshPositions = bundle.worldPositions;
    bundle.worldMeshNormals = bundle.worldNormals;
    bundle.worldMeshUVs = bundle.worldUVs;
    return bundle;
  }

  function parseBoardFontSizePx(font) {
    var m = String(font || "").match(/(\d+(?:\.\d+)?)px/);
    return m ? parseFloat(m[1]) : 12;
  }

  function boardCreateCanvas(w, h) {
    try {
      if (typeof OffscreenCanvas !== "undefined") return new OffscreenCanvas(w, h);
      if (typeof document !== "undefined" && document.createElement) {
        var c = document.createElement("canvas");
        c.width = w;
        c.height = h;
        return c;
      }
    } catch (e) {}
    return null;
  }

  // hasLabelData: a labels-ONLY board (no rects/lines/etc.) must still render,
  // so the render() early-return gate consults this predicate too.
  function hasLabelData(bundle) {
    return Boolean(bundle && Array.isArray(bundle.labels) && bundle.labels.length > 0);
  }

  // Compute-owned mesh resources keep the direct encoder. Selena uniforms
  // now have stable owners; the recorder uploads their current values and
  // checks resource identities before replaying.
  function webGPUObjectBlocksBundle(obj, materials) {
    if (!obj) return false;
    if (webGPUObjectIsSkinned(obj)) return true;
    if (webGPUObjectComputedMorphDrawRecord(obj)) return true;
    return obj._crowdBatch === true || Boolean(obj._crowdSkin);
  }

  // webGPURenderBundlesEnabled reports the page's opt-out. Bundling is on by
  // default; set window.__gosx_scene3d_webgpu_render_bundles to false to force
  // direct encoding, which is the first thing to try when an image looks stale.
  function webGPURenderBundlesEnabled() {
    if (typeof window === "undefined") return true;
    return window.__gosx_scene3d_webgpu_render_bundles !== false;
  }

  function destroyRendererGPUResource(resource) {
    if (!resource || typeof resource.destroy !== "function") return;
    try { resource.destroy(); } catch (_err) {}
  }

  // webGPUAdapterInfoSnapshot returns the GPUAdapterInfo the probe captured.
  // Vendor / architecture / device / description together are what let a
  // dump be attributed to a specific driver, and (with the browser engine)
  // to Dawn-plus-Tint versus wgpu-plus-naga. "backend=webgpu" alone cannot
  // distinguish two WGSL compilers with two different sets of bugs.
  function webGPUAdapterInfoSnapshot() {
    var base = typeof sceneWebGPUDiagnostics === "function" ? sceneWebGPUDiagnostics() : {};
    return (base && base.adapterInfo) ? base.adapterInfo : {};
  }
