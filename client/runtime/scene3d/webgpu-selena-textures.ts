  type SceneSelenaTextureMaterial = { customUniforms?: Record<string, unknown> };
  type SceneSelenaTextureDescriptor = { name?: string; dimension?: string; wgsl?: { textureBinding?: number; samplerBinding?: number } };
  type SceneSelenaIBLTextures = { active: boolean; radiance?: { view: unknown }; irradiance?: { view: unknown }; brdfLUT?: { view: unknown } };
  type SceneSelenaTextureBindingContext = {
    device: Parameters<typeof wgpuLoadTexture>[0];
    textureCache: Parameters<typeof wgpuLoadTexture>[2];
    iblResources: SceneSelenaIBLTextures;
    placeholderCubeView: unknown; placeholderView: unknown;
    envMapSampler: unknown; linearSampler: unknown;
    liveView: (material: SceneSelenaTextureMaterial, texture: SceneSelenaTextureDescriptor) => unknown;
    url: (material: SceneSelenaTextureMaterial, texture: SceneSelenaTextureDescriptor, index: number) => string;
  };

  function sceneWebGPUSelenaEnvironmentSlot(material: SceneSelenaTextureMaterial, texture: SceneSelenaTextureDescriptor): "radiance" | "irradiance" | "brdf-lut" | "" {
    const values = material && material.customUniforms;
    let ref = values && texture && values[texture.name];
    if (ref && typeof ref === "object") {
      const resource = ref as { resource?: unknown; ref?: unknown; sceneResource?: unknown };
      ref = resource.resource || resource.ref || resource.sceneResource;
    }
    const slot = typeof ref === "string" && ref.trim().startsWith("gosx:environment:") ? ref.trim().slice(17) : "";
    if (slot === "brdf-lut" && texture.dimension !== "cube") return slot;
    if ((slot === "radiance" || slot === "irradiance") && texture.dimension === "cube") return slot;
    return "";
  }

  // The renderer creates this context once after GPU initialization. Texture
  // records and IBL readiness remain live; view identity drives bind-group reuse.
  function sceneWebGPUAppendSelenaTextures(context: SceneSelenaTextureBindingContext, material: SceneSelenaTextureMaterial, textures: SceneSelenaTextureDescriptor[], entries: { binding: number; resource: unknown }[], cacheViews: unknown[]) {
    const { device, textureCache, iblResources, placeholderCubeView, placeholderView, envMapSampler, linearSampler } = context;
    for (var i = 0; i < textures.length; i++) {
      var tex = textures[i] || {};
      var isCube = tex.dimension === "cube";
      var environmentSlot = sceneWebGPUSelenaEnvironmentSlot(material, tex);
      var environmentRecord = environmentSlot && iblResources.active ? iblResources[environmentSlot === "brdf-lut" ? "brdfLUT" : environmentSlot] : null;
      var liveView = environmentSlot ? (environmentRecord && environmentRecord.view) : context.liveView(material, tex);
      var url = liveView || environmentSlot ? "" : context.url(material, tex, i);
      // dimension:"cube" (the water surface/surface-below "sky" environment
      // map) loads through wgpuLoadCubeTexture/placeholderCubeView instead
      // of the plain-2d wgpuLoadTexture/placeholderView path every other
      // Selena texture uses; this mirrors the hand-written
      // createWaterRenderBindGroup's cubeMap handling.
      var record = url ? (isCube ? wgpuLoadCubeTexture(device, url, textureCache) : wgpuLoadTexture(device, url, textureCache, null, undefined, undefined)) : null;
      var view = liveView || (record && record.view ? record.view : (isCube ? placeholderCubeView : placeholderView));
      var wgsl = tex.wgsl || {};
      entries.push({ binding: sceneNumber(wgsl.textureBinding, 1 + i * 2), resource: view });
      entries.push({ binding: sceneNumber(wgsl.samplerBinding, 2 + i * 2), resource: environmentSlot ? envMapSampler : linearSampler });
      cacheViews.push(view);
    }
  }

  function sceneWebGPUSelenaEnvironmentInfo(ibl: { active: boolean; diagnostics: { radianceMipLevels: number } }, env: { envIntensity?: number; envRotation?: number }) {
    return ibl.active
      ? [1, Math.max(0, sceneNumber(env.envIntensity, 1)), sceneNumber(env.envRotation, 0), Math.max(0, ibl.diagnostics.radianceMipLevels - 1)]
      : [0, 0, 0, 0];
  }
