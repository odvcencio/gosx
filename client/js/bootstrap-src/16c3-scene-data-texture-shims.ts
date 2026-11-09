  // Selena data textures (client/runtime/scene3d/data-texture.ts) ship in the
  // gated scene3d-data-texture chunk. A material that names a "gosx:data:" key
  // requests the chunk on first use and binds the placeholder until it lands.
  // WebGPU calls View and Sampler; WebGL calls Bind.
  var sceneDataTextureRequested = false;
  function sceneDataTextureAPI(material, texture) {
    var api = window.__gosx_scene3d_data_texture_api, values = material && material.customUniforms, ref = !api && !sceneDataTextureRequested && values && texture && values[texture.name];
    if (ref && typeof ref === "object") ref = ref.resource || ref.ref || ref.sceneResource;
    if (typeof ref === "string" && ref.trim().indexOf("gosx:data:") === 0) {
      sceneDataTextureRequested = true;
      window.__gosx_scene3d_api.loadPresentation("data-texture", "gosxScene3dDataTextureUrl", "").catch(console.error);
    }
    return api;
  }
  function sceneDataTextureView(context, material, texture) {
    var api = sceneDataTextureAPI(material, texture);
    return api && api.webgpuView(context.device, material, texture);
  }
  // Load-only and 32-bit float textures are read without filtering.
  function sceneDataTextureSampler(context, texture) {
    var format = texture && texture.format;
    return texture && (texture.sampleMode === "load" || format === "r32float" || format === "rgba32float")
      ? context.nearestSampler || (context.nearestSampler = context.device.createSampler({ magFilter: "nearest", minFilter: "nearest", mipmapFilter: "nearest" }))
      : null;
  }
  function sceneDataTextureBind(gl, info, unit, loc, material, texture, placeholder) {
    var api = sceneDataTextureAPI(material, texture);
    return api && api.webglBind(gl, info, unit, loc, material, texture, placeholder);
  }
