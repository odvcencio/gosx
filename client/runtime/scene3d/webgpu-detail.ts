// Opt-in WebGPU detail resources. The base material layout and maps stay intact.
function sceneWebGPUDetailFragment(source = "", detail = false) {
  if (!detail) return source;
  return source.replace("@fragment fn fragmentMain(in: VertexOutput)", sceneDetailShaderSource("wgsl") + "\n@fragment fn fragmentMain(in: VertexOutput)")
    .replace("-> @location(0) vec4f {\n    var albedo = material.albedo;", "-> @location(0) vec4f {\n    let detailDx = dpdx(in.worldPos);\n    let detailDy = dpdy(in.worldPos);\n    var albedo = material.albedo;")
    .replace("    let V = normalize(frame.cameraPos - in.worldPos);", `    let detailResult = detailApply(in.worldPos, normalize(in.normal), detailDx, detailDy, frame.cameraPos, albedo, N, roughness);
    albedo = detailResult.albedo; N = detailResult.normal; roughness = detailResult.roughness;
    let V = normalize(frame.cameraPos - in.worldPos);`);
}

function sceneWebGPUCreateDetailResources(device = Object.create(null), frameLayout = Object.create(null), materialLayout = Object.create(null), source = "") {
  const layout = device.createBindGroupLayout({ label: "detail", entries: [
    { binding: 0, visibility: GPUShaderStage.FRAGMENT, buffer: { type: "uniform" } },
    { binding: 1, visibility: GPUShaderStage.FRAGMENT, texture: { sampleType: "float", viewDimension: "2d-array" } },
    { binding: 2, visibility: GPUShaderStage.FRAGMENT, sampler: { type: "filtering" } },
  ] });
  return {
    layout: layout, atlases: new Map(), materials: new Map(), bake: false,
    pipelineLayout: device.createPipelineLayout({ bindGroupLayouts: [frameLayout, materialLayout, layout] }),
    fragment: device.createShaderModule({ label: "pbr-detail-frag", code: sceneWebGPUDetailFragment(source, true) }),
    sampler: device.createSampler({ addressModeU: "repeat", addressModeV: "repeat", magFilter: "linear", minFilter: "linear", mipmapFilter: "linear" }),
  };
}

function sceneWebGPUDetailBakeResources(device = Object.create(null), resources = Object.create(null), placeholderView = Object.create(null)) {
  if (resources.bake) return resources.bake;
  const code = `
    @group(0) @binding(0) var src: texture_2d<f32>;
    @group(0) @binding(1) var rough: texture_2d<f32>;
    @group(0) @binding(2) var samp: sampler;
    @group(0) @binding(3) var<uniform> flags: vec4f;
    struct Out { @builtin(position) position: vec4f, @location(0) uv: vec2f, };
    @vertex fn vertexMain(@builtin(vertex_index) index: u32) -> Out {
      let p = vec2f(f32((index << 1u) & 2u), f32(index & 2u));
      return Out(vec4f(p * 2.0 - 1.0, 0.0, 1.0), vec2f(p.x, 1.0 - p.y));
    }
    @fragment fn fragmentMain(in: Out) -> @location(0) vec4f {
      let color = textureSample(src, samp, in.uv);
      let r = textureSample(rough, samp, in.uv).r;
      if (flags.w > 0.5) { return select(vec4f(0.5, 0.5, 1.0, 1.0), color, flags.y > 0.5); }
      return vec4f(select(vec3f(0.5), color.rgb, flags.x > 0.5), select(0.5, r, flags.z > 0.5));
    }`;
  const module = device.createShaderModule({ label: "detail-atlas-bake", code: code });
  const pipeline = device.createRenderPipeline({ label: "detail-atlas-bake", layout: "auto",
    vertex: { module: module, entryPoint: "vertexMain" }, fragment: { module: module, entryPoint: "fragmentMain", targets: [{ format: "rgba8unorm" }] },
    primitive: { topology: "triangle-list" },
  });
  resources.bake = { pipeline: pipeline, placeholder: placeholderView };
  return resources.bake;
}

function sceneWebGPUBakeDetailAtlas(device = Object.create(null), resources = Object.create(null), atlas = Object.create(null), inputs = Object.create(null), placeholderView = Object.create(null)) {
  const bake = sceneWebGPUDetailBakeResources(device, resources, placeholderView);
  const encoder = device.createCommandEncoder({ label: "detail-atlas-bake" });
  for (let layer = 0; layer < 4; layer++) {
    const offset = Math.floor(layer / 2) * 3, normalPass = layer % 2 === 1;
    const src = inputs.records[offset + (normalPass ? 1 : 0)], rough = inputs.records[offset + 2];
    device.queue.writeBuffer(atlas.flags[layer], 0, new Float32Array(inputs.masks.slice(offset, offset + 3).concat(normalPass ? 1 : 0)));
    const group = device.createBindGroup({ layout: bake.pipeline.getBindGroupLayout(0), entries: [
      { binding: 0, resource: src && src.loaded ? src.view : bake.placeholder },
      { binding: 1, resource: rough && rough.loaded ? rough.view : bake.placeholder },
      { binding: 2, resource: resources.sampler }, { binding: 3, resource: { buffer: atlas.flags[layer] } },
    ] });
    for (let mip = 0; mip < 10; mip++) {
      const pass = encoder.beginRenderPass({ colorAttachments: [{
        view: atlas.texture.createView({ dimension: "2d", baseArrayLayer: layer, arrayLayerCount: 1, baseMipLevel: mip, mipLevelCount: 1 }),
        loadOp: "clear", storeOp: "store", clearValue: { r: 0.5, g: 0.5, b: 0.5, a: 0.5 },
      }] });
      pass.setPipeline(bake.pipeline); pass.setBindGroup(0, group); pass.draw(3); pass.end();
    }
  }
  device.queue.submit([encoder.finish()]);
}

function sceneWebGPUDetailAtlasKey(detail = Object.create(null)) {
  return JSON.stringify([detail.ground, detail.steep].map(function(layer = Object.create(null)) {
    return layer ? [layer.albedo || "", layer.normal || "", layer.roughness || ""] : null;
  }));
}

function sceneWebGPUDetailMaterialKey(detail = Object.create(null)) {
  return JSON.stringify([sceneWebGPUDetailAtlasKey(detail), Array.from(sceneDetailUniformData(detail, null, true))]);
}

function sceneWebGPUBeginDetailFrame(resources = Object.create(null)) {
  if (!resources || !resources.materials) return;
  for (const entry of resources.materials.values()) entry.active = false;
}

function sceneWebGPUReuseDetailEntry(resources = Object.create(null), atlas = Object.create(null), key = "") {
  for (const [previousKey, entry] of resources.materials) {
    if (entry.active || entry.atlas !== atlas) continue;
    resources.materials.delete(previousKey);
    resources.materials.set(key, entry);
    return entry;
  }
  return null;
}

function sceneWebGPUPrepareDetail(device = Object.create(null), resources = Object.create(null), material = Object.create(null), textureCache = Object.create(null), placeholderView = Object.create(null)) {
  const detail = material.detail, key = sceneWebGPUDetailAtlasKey(detail);
  let atlas = resources.atlases.get(key);
  if (!atlas) {
    const texture = device.createTexture({ label: "detail-atlas", size: [512, 512, 4], format: "rgba8unorm", mipLevelCount: 10,
      usage: GPUTextureUsage.TEXTURE_BINDING | GPUTextureUsage.RENDER_ATTACHMENT });
    atlas = { texture: texture, view: texture.createView({ dimension: "2d-array" }), flags: [], signature: "", masks: [], sources: [] };
    for (let i = 0; i < 4; i++) atlas.flags.push(device.createBuffer({ size: 16, usage: GPUBufferUsage.UNIFORM | GPUBufferUsage.COPY_DST }));
    resources.atlases.set(key, atlas);
  }
  const inputs = sceneDetailTextureRecords(detail, function(url = "", role = "") {
    return wgpuLoadTexture(device, url, textureCache, null, role === "albedo" ? "base-color" : role, "linear");
  });
  const signature = inputs.masks.join("");
  const sources = inputs.records.map(function(record = Object.create(null)) { return record && record.loaded ? record.texture : null; });
  if (signature !== atlas.signature || sources.some(function(texture = Object.create(null), i = 0) { return !atlas.sources || atlas.sources[i] !== texture; })) {
    sceneWebGPUBakeDetailAtlas(device, resources, atlas, inputs, placeholderView);
    atlas.signature = signature; atlas.masks = inputs.masks;
    atlas.sources = sources;
  }
  const materialKey = sceneWebGPUDetailMaterialKey(detail);
  let entry = resources.materials.get(materialKey) || sceneWebGPUReuseDetailEntry(resources, atlas, materialKey);
  if (!entry) {
    const buffer = device.createBuffer({ label: "detail-uniform", size: 96, usage: GPUBufferUsage.UNIFORM | GPUBufferUsage.COPY_DST });
    entry = { buffer: buffer, atlas: atlas, group: device.createBindGroup({ layout: resources.layout, entries: [
      { binding: 0, resource: { buffer: buffer } }, { binding: 1, resource: atlas.view }, { binding: 2, resource: resources.sampler },
    ] }) };
    resources.materials.set(materialKey, entry);
  }
  entry.active = true;
  return entry;
}

function sceneWebGPUUploadDetail(device = Object.create(null), resources = Object.create(null), material = Object.create(null), enabled = true) {
  const entry = resources.materials.get(sceneWebGPUDetailMaterialKey(material.detail));
  device.queue.writeBuffer(entry.buffer, 0, sceneDetailUniformData(material.detail, entry.atlas.masks, enabled));
  return entry.group;
}

function sceneWebGPUPrepareDetailFrame(device = Object.create(null), resources: any = null, materials: any[] = [], textureCache = Object.create(null), options = Object.create(null)) {
  sceneWebGPUBeginDetailFrame(resources);
  for (const material of materials) {
    if (!material || !material.detail) continue;
    if (!resources) resources = sceneWebGPUCreateDetailResources(device, options.frameLayout, options.materialLayout, options.source);
    sceneWebGPUPrepareDetail(device, resources, material, textureCache, options.placeholderView);
    sceneWebGPUUploadDetail(device, resources, material, options.enabled);
  }
  sceneWebGPUPruneDetail(resources, materials);
  return resources;
}

// Retire detail resources after preparing the complete draw set for this frame.
function sceneWebGPUPruneDetail(resources = Object.create(null), materials: any[] = []) {
  if (!resources || !resources.atlases) return;
  const active = new Set(materials.filter(function(material = Object.create(null)) {
    return material && material.detail;
  }).map(function(material = Object.create(null)) { return sceneWebGPUDetailMaterialKey(material.detail); }));
  const atlases = new Set();
  for (const [key, entry] of resources.materials) {
    if (active.has(key)) atlases.add(entry.atlas);
    else { entry.buffer.destroy(); resources.materials.delete(key); }
  }
  for (const [key, atlas] of resources.atlases) {
    if (atlases.has(atlas)) continue;
    atlas.texture.destroy();
    for (const buffer of atlas.flags) buffer.destroy();
    resources.atlases.delete(key);
  }
}

function sceneWebGPUDisposeDetail(resources = Object.create(null)) {
  sceneWebGPUPruneDetail(resources, []);
}
