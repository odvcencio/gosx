// WebGPU Environment.Terrain pass (lazy chunk bootstrap-feature-scene3d-terrain.js).
//
// The renderer calls window.__gosx_scene3d_terrain_api.webgpu once per frame
// inside its main pass, before the opaque meshes. The pass draws every
// selected patch with one instanced, indexed draw. Its vertex stage displaces
// a 17x17 grid from the height texture and morphs odd vertices onto the next
// coarser grid (CDLOD). Its fragment stage is the renderer's own PBR fragment
// (opts.fragment) with the surface replaced by the terrain splat, so lighting,
// shadows, fog and IBL are the scene's.
//
// Bind groups: 0 is the renderer's frame group. 1 uses the renderer's
// material layout; its nine texture slots carry the terrain normal map
// (albedo slot), the splat map (normal slot) and the four layer albedos
// (roughness, metalness, emissive, specularColor slots), which keeps the
// pass inside the 16 sampled textures a baseline device allows per stage.
// 2 holds the terrain uniform block and the height texture.

const SCENE_TERRAIN_WGSL_BLOCK = "struct TerrainBlock { p: array<vec4f, 36> };\n@group(2) @binding(0) var<uniform> terrain: TerrainBlock;\n";

function sceneTerrainVertexWGSL(frameStructs: string): string {
  return frameStructs + "\n" + SCENE_TERRAIN_WGSL_BLOCK + `
@group(2) @binding(1) var terrainHeights: texture_2d<f32>;
@group(0) @binding(0) var<uniform> frame: FrameUniforms;
struct TerrainIn { @builtin(vertex_index) vid: u32, @location(0) tile: vec4f };
struct VertexOutput {
  @builtin(position) clipPos: vec4f,
  @location(0) worldPos: vec3f,
  @location(1) normal: vec3f,
  @location(2) uv: vec2f,
  @location(3) tangent: vec3f,
  @location(4) bitangent: vec3f,
  @location(5) @interpolate(flat) instanceColor: vec4f,
};
fn terrainTexel(c: vec2i) -> f32 {
  return textureLoad(terrainHeights, clamp(c, vec2i(0), vec2i(terrain.p[1].zw) - vec2i(1)), 0).r;
}
fn terrainGrid(xz: vec2f) -> vec2f {
  let last = terrain.p[1].zw - vec2f(1.0);
  return clamp((xz - terrain.p[0].xy) / terrain.p[0].zw * last, vec2f(0.0), last);
}
fn terrainSurface(xz: vec2f) -> f32 {
  let g = terrainGrid(xz);
  let cell = min(floor(g), terrain.p[1].zw - vec2f(2.0));
  let f = g - cell;
  let c = vec2i(cell);
  let h00 = terrainTexel(c);
  let h10 = terrainTexel(c + vec2i(1, 0));
  let h01 = terrainTexel(c + vec2i(0, 1));
  let h11 = terrainTexel(c + vec2i(1, 1));
  var h = h00 + f.y * (h01 - h00) + f.x * (h11 - h01);
  if (f.x >= f.y) { h = h00 + f.x * (h10 - h00) + f.y * (h11 - h10); }
  return terrain.p[1].x + h * terrain.p[1].y;
}
fn terrainTint(level: u32) -> vec4f {
  let k = f32(level % 6u);
  return vec4f(0.55 + 0.45 * cos(6.2831 * (k / 6.0 + vec3f(0.0, 0.33, 0.67))), 1.0);
}
@vertex fn vertexMain(in: TerrainIn) -> VertexOutput {
  let q = u32(terrain.p[2].y);
  let ij = vec2f(f32(in.vid % (q + 1u)), f32(in.vid / (q + 1u)));
  let level = u32(in.tile.w);
  let lo = terrain.p[0].xy;
  let hi = lo + terrain.p[0].zw;
  let s = terrain.p[2].x;
  var index = in.tile.xy + ij * in.tile.z;
  let p0 = min(lo + index * s, hi);
  let d = distance(frame.cameraPos, vec3f(p0.x, terrainSurface(p0), p0.y));
  let m = terrain.p[4u + min(level, 11u)];
  let k = clamp((d - m.x) * m.y, 0.0, 1.0);
  index = index - (ij - 2.0 * floor(ij * 0.5)) * in.tile.z * k;
  let xz = min(lo + index * s, hi);
  let texel = terrain.p[0].zw / (terrain.p[1].zw - vec2f(1.0));
  let dx = terrainSurface(xz + vec2f(texel.x, 0.0)) - terrainSurface(xz - vec2f(texel.x, 0.0));
  let dz = terrainSurface(xz + vec2f(0.0, texel.y)) - terrainSurface(xz - vec2f(0.0, texel.y));
  let N = normalize(vec3f(-dx / (2.0 * texel.x), 1.0, -dz / (2.0 * texel.y)));
  var out: VertexOutput;
  out.worldPos = vec3f(xz.x, terrainSurface(xz), xz.y);
  out.normal = N;
  out.uv = (terrainGrid(xz) + vec2f(0.5)) / terrain.p[1].zw;
  let T = normalize(vec3f(1.0, 0.0, 0.0) - N * N.x);
  out.tangent = T;
  out.bitangent = cross(N, T);
  out.instanceColor = select(vec4f(1.0), terrainTint(level), terrain.p[3].w > 0.5);
  out.clipPos = frame.projMatrix * frame.viewMatrix * vec4f(out.worldPos, 1.0);
  return out;
}
`;
}

// sceneTerrainFragmentWGSL patches the renderer's PBR fragment: the terrain
// functions go in front of the entry point, derivatives are taken at the top
// of gosxPBRColor (uniform control flow), and albedo, normal and roughness are
// replaced just before the view vector is formed. A missing anchor throws, and
// the pass reports "failed" instead of drawing an unlit surface.
function sceneTerrainFragmentWGSL(pbr: string): string {
  const anchors = ["@fragment fn fragmentMain(in: VertexOutput)", "fn gosxPBRColor(in: VertexOutput) -> vec4f {", "    let V = normalize(frame.cameraPos - in.worldPos);"];
  for (const anchor of anchors) if (pbr.indexOf(anchor) < 0) throw new Error("terrain: PBR fragment anchor missing: " + anchor.trim());
  return pbr.replace("@fragment fn fragmentMain(in: VertexOutput)", SCENE_TERRAIN_WGSL_BLOCK + `
struct TerrainSurface { albedo: vec3f, normal: vec3f, roughness: f32 };
fn terrainMask(lo: f32, hi: f32, blend: f32, v: f32) -> f32 {
  let b = blend * 0.5;
  return smoothstep(lo - b, lo + b, v) * (1.0 - smoothstep(hi - b, hi + b, v));
}
fn terrainTap(layer: u32, uv: vec2f, gx: vec2f, gy: vec2f) -> vec3f {
  if (layer == 0u) { return textureSampleGrad(roughnessTex, roughnessSamp, uv, gx, gy).rgb; }
  if (layer == 1u) { return textureSampleGrad(metalnessTex, metalnessSamp, uv, gx, gy).rgb; }
  if (layer == 2u) { return textureSampleGrad(emissiveTex, emissiveSamp, uv, gx, gy).rgb; }
  return textureSampleGrad(specularColorTex, specularColorSamp, uv, gx, gy).rgb;
}
fn terrainLayer(layer: u32, p: vec3f, dx: vec3f, dy: vec3f, w: vec3f, tri: f32) -> vec3f {
  let a = terrain.p[16u + layer * 4u];
  let b = terrain.p[17u + layer * 4u];
  if (b.y < 0.5) { return a.rgb; }
  let s = 1.0 / b.x;
  var c = terrainTap(layer, p.xz * s, dx.xz * s, dy.xz * s);
  if (tri > 0.001) {
    let cx = terrainTap(layer, p.zy * s, dx.zy * s, dy.zy * s);
    let cz = terrainTap(layer, p.xy * s, dx.xy * s, dy.xy * s);
    c = mix(c, cx * w.x + c * w.y + cz * w.z, tri);
  }
  return a.rgb * c;
}
fn terrainSurfaceAt(in: VertexOutput, dx: vec3f, dy: vec3f, ux: vec2f, uy: vec2f) -> TerrainSurface {
  let N = normalize(textureSampleGrad(albedoTex, albedoSamp, in.uv, ux, uy).xyz * 2.0 - 1.0);
  let slope = degrees(acos(clamp(N.y, -1.0, 1.0)));
  let p = in.worldPos;
  let edge = terrain.p[2].w;
  let tri = select(0.0, smoothstep(edge - 5.0, edge + 5.0, slope), edge < 89.5);
  var w = pow(abs(N), vec3f(4.0));
  w = w / max(w.x + w.y + w.z, 1e-5);
  let dim = max(terrain.p[3].yz, vec2f(1.0));
  let suv = (clamp((p.xz - terrain.p[0].xy) / terrain.p[0].zw, vec2f(0.0), vec2f(1.0)) * (dim - 1.0) + 0.5) / dim;
  let splat = select(vec4f(1.0), textureSampleLevel(normalTex, normalSamp, suv, 0.0), terrain.p[3].x > 0.5);
  var albedo = terrainLayer(0u, p, dx, dy, w, tri);
  var rough = terrain.p[16].w;
  let count = u32(terrain.p[2].z);
  for (var i = 1u; i < 4u; i++) {
    if (i >= count) { break; }
    let hb = terrain.p[18u + i * 4u];
    let sb = terrain.p[19u + i * 4u];
    let paint = select(splat.b, select(splat.g, splat.r, i == 1u), i < 3u);
    let m = terrainMask(hb.x, hb.y, hb.z, p.y) * terrainMask(sb.x, sb.y, sb.z, slope) * paint;
    if (m > 0.001) {
      albedo = mix(albedo, terrainLayer(i, p, dx, dy, w, tri), m);
      rough = mix(rough, terrain.p[16u + i * 4u].w, m);
    }
  }
  return TerrainSurface(albedo * in.instanceColor.rgb, N, clamp(rough, 0.04, 1.0));
}
@fragment fn fragmentMain(in: VertexOutput)`)
    .replace("fn gosxPBRColor(in: VertexOutput) -> vec4f {", `fn gosxPBRColor(in: VertexOutput) -> vec4f {
    let terrainDx = dpdx(in.worldPos); let terrainDy = dpdy(in.worldPos);
    let terrainUx = dpdx(in.uv); let terrainUy = dpdy(in.uv);`)
    .replace("    let V = normalize(frame.cameraPos - in.worldPos);", `    let terrainS = terrainSurfaceAt(in, terrainDx, terrainDy, terrainUx, terrainUy);
    albedo = terrainS.albedo; N = terrainS.normal; roughness = terrainS.roughness;
    let V = normalize(frame.cameraPos - in.worldPos);`);
}

function sceneTerrainGPUTexture(device: any, width: number, height: number, format: string, mips: any[], bytesPerTexel: number, label: string): any {
  const texture = device.createTexture({ label, size: [width, height, 1], format, mipLevelCount: mips.length,
    usage: GPUTextureUsage.TEXTURE_BINDING | GPUTextureUsage.COPY_DST });
  for (let level = 0; level < mips.length; level++) {
    const mip = mips[level];
    device.queue.writeTexture({ texture, mipLevel: level }, mip.data, { bytesPerRow: mip.width * bytesPerTexel, rowsPerImage: mip.height }, [mip.width, mip.height, 1]);
  }
  return texture;
}

function sceneTerrainGPUDisposeModel(res: any): void {
  for (const key of ["heightTex", "normalTex", "splatTex"]) { if (res[key]) res[key].destroy(); res[key] = null; }
  res.gpuModel = null; res.group1 = null; res.group2 = null;
}

function sceneTerrainGPUDispose(res: any): void {
  if (!res || res.kind !== "webgpu") return;
  sceneTerrainGPUDisposeModel(res);
  for (const key of ["index", "patchBuffer", "uniform", "material", "slot", "white"]) { if (res[key]) res[key].destroy(); res[key] = null; }
  res.pipelines = null; res.kind = ""; res.model = null; res.modelKey = "";
}

// sceneTerrainGPUStatic creates the per-device buffers, samplers and layout.
function sceneTerrainGPUStatic(res: any, device: any): void {
  if (res.kind === "webgpu" && res.device === device) return;
  sceneTerrainGPUDispose(res);
  res.kind = "webgpu"; res.device = device; res.dispose = function() { sceneTerrainGPUDispose(res); };
  const indices = sceneTerrainIndices();
  res.index = device.createBuffer({ label: "gosx-terrain-index", size: indices.byteLength, usage: GPUBufferUsage.INDEX | GPUBufferUsage.COPY_DST });
  device.queue.writeBuffer(res.index, 0, indices);
  res.indexCount = indices.length;
  res.patchBuffer = device.createBuffer({ label: "gosx-terrain-patches", size: SCENE_TERRAIN_MAX_PATCHES * 16, usage: GPUBufferUsage.VERTEX | GPUBufferUsage.COPY_DST });
  res.uniformData = new Float32Array(SCENE_TERRAIN_VEC4S * 4);
  res.uniform = device.createBuffer({ label: "gosx-terrain-uniform", size: res.uniformData.byteLength, usage: GPUBufferUsage.UNIFORM | GPUBufferUsage.COPY_DST });
  res.slot = device.createBuffer({ label: "gosx-terrain-object", size: 256, usage: GPUBufferUsage.UNIFORM | GPUBufferUsage.COPY_DST });
  res.white = device.createTexture({ label: "gosx-terrain-white", size: [1, 1, 1], format: "rgba8unorm", usage: GPUTextureUsage.TEXTURE_BINDING | GPUTextureUsage.COPY_DST });
  device.queue.writeTexture({ texture: res.white }, new Uint8Array([255, 255, 255, 255]), { bytesPerRow: 4 }, [1, 1, 1]);
  res.whiteView = res.white.createView();
  res.clamp = device.createSampler({ magFilter: "linear", minFilter: "linear", mipmapFilter: "linear", addressModeU: "clamp-to-edge", addressModeV: "clamp-to-edge" });
  res.repeat = device.createSampler({ magFilter: "linear", minFilter: "linear", mipmapFilter: "linear", addressModeU: "repeat", addressModeV: "repeat", maxAnisotropy: 8 });
  res.layout = device.createBindGroupLayout({ label: "gosx-terrain", entries: [
    { binding: 0, visibility: GPUShaderStage.VERTEX | GPUShaderStage.FRAGMENT, buffer: { type: "uniform" } },
    { binding: 1, visibility: GPUShaderStage.VERTEX, texture: { sampleType: "unfilterable-float" } },
  ] });
  res.pipelines = new Map();
}

function sceneTerrainGPUModel(res: any, device: any, model: any): void {
  if (res.gpuModel === model) return;
  sceneTerrainGPUDisposeModel(res);
  res.heightTex = sceneTerrainGPUTexture(device, model.columns, model.rows, "r32float", [{ width: model.columns, height: model.rows, data: model.heights }], 4, "gosx-terrain-heights");
  res.normalTex = sceneTerrainGPUTexture(device, model.columns, model.rows, "rgba8unorm", model.normals, 4, "gosx-terrain-normals");
  const splat = sceneTerrainSplat(model.ir) || { width: 1, height: 1, data: new Uint8Array([255, 255, 255, 255]) };
  res.splatTex = sceneTerrainGPUTexture(device, splat.width, splat.height, "rgba8unorm", [splat], 4, "gosx-terrain-splat");
  res.heightView = res.heightTex.createView(); res.normalView = res.normalTex.createView(); res.splatView = res.splatTex.createView();
  res.gpuModel = model;
}

// sceneTerrainGPULayers resolves the layer albedo views. It returns the
// per-layer flags (bit 0: albedo texture ready) for the uniform block.
function sceneTerrainGPULayers(res: any, o: any, model: any): number[] {
  const layers = Array.isArray(model.ir.layers) ? model.ir.layers : [];
  const flags = [0, 0, 0, 0];
  res.layerViews = [res.whiteView, res.whiteView, res.whiteView, res.whiteView];
  for (let i = 0; i < 4 && i < layers.length; i++) {
    const url = layers[i] && layers[i].albedo;
    const record = url && typeof o.load === "function" ? o.load(o.device, url, o.textureCache, null, "base-color", "srgb") : null;
    if (record && record.loaded && !record.failed && record.view) { res.layerViews[i] = record.view; flags[i] = 1; }
  }
  return flags;
}

function sceneTerrainGPUGroups(res: any, o: any): void {
  const views = [res.normalView, res.splatView, res.layerViews[0], res.layerViews[1], res.layerViews[2], res.whiteView, res.whiteView, res.layerViews[3], res.whiteView];
  const samplers = [res.clamp, res.clamp, res.repeat, res.repeat, res.repeat, res.clamp, res.clamp, res.repeat, res.clamp];
  const key = views.concat([o.materialLayout, res.material]);
  if (!res.group1 || res.group1Key.some(function(value: any, i: number) { return value !== key[i]; })) {
    const entries: any[] = [{ binding: 0, resource: { buffer: res.material } }];
    for (let i = 0; i < 9; i++) entries.push({ binding: 2 * i + 1, resource: views[i] }, { binding: 2 * i + 2, resource: samplers[i] });
    entries.push({ binding: 19, resource: { buffer: res.slot, size: 112 } });
    res.group1 = o.device.createBindGroup({ label: "gosx-terrain-material", layout: o.materialLayout, entries });
    res.group1Key = key;
  }
  if (!res.group2 || res.group2View !== res.heightView) {
    res.group2 = o.device.createBindGroup({ label: "gosx-terrain", layout: res.layout, entries: [
      { binding: 0, resource: { buffer: res.uniform } }, { binding: 1, resource: res.heightView },
    ] });
    res.group2View = res.heightView;
  }
}

function sceneTerrainGPUPipeline(res: any, o: any): any {
  const key = o.format + "|" + o.samples + "|" + (o.specular ? 1 : 0);
  let pipeline = res.pipelines.get(key);
  if (pipeline) return pipeline;
  const device = o.device;
  if (!res.vertexModule || res.vertexSource !== o.frameStructs) {
    res.vertexModule = device.createShaderModule({ label: "gosx-terrain-vert", code: sceneTerrainVertexWGSL(o.frameStructs) });
    res.vertexSource = o.frameStructs;
  }
  if (!res.fragmentModule || res.fragmentSource !== o.fragment) {
    res.fragmentModule = device.createShaderModule({ label: "gosx-terrain-frag", code: sceneTerrainFragmentWGSL(o.fragment) });
    res.fragmentSource = o.fragment;
  }
  const targets = o.specular ? [{ format: o.format }, { format: "rgba16float" }] : [{ format: o.format }];
  const descriptor = {
    label: "gosx-terrain",
    layout: device.createPipelineLayout({ bindGroupLayouts: [o.frameLayout, o.materialLayout, res.layout] }),
    vertex: { module: res.vertexModule, entryPoint: "vertexMain", buffers: [{ arrayStride: 16, stepMode: "instance", attributes: [{ shaderLocation: 0, offset: 0, format: "float32x4" }] }] },
    fragment: { module: res.fragmentModule, entryPoint: o.specular ? "fragmentMainSpecular" : "fragmentMain", targets },
    primitive: { topology: "triangle-list", cullMode: "none" },
    multisample: { count: Math.max(1, o.samples | 0) },
    depthStencil: { format: "depth24plus", depthWriteEnabled: true, depthCompare: "less-equal" },
  };
  pipeline = typeof o.create === "function" ? o.create(device, "render", descriptor) : device.createRenderPipeline(descriptor);
  res.pipelines.set(key, pipeline);
  return pipeline;
}

// sceneTerrainGPUDraw is the renderer's per-frame entry. It returns the state
// it publishes as data-gosx-scene3d-terrain: none, pending, failed or ready.
function sceneTerrainGPUDraw(res: any, pass: any, o: any): string {
  const ir = o.environment && o.environment.terrain;
  let state = "none";
  try {
    const wind = sceneWindUpdate(o.environment, o.time);
    if (o.frameBuffer) o.device.queue.writeBuffer(o.frameBuffer, 160, wind);
    if (!ir) { if (res.kind) sceneTerrainGPUDispose(res); return state; }
    sceneTerrainGPUStatic(res, o.device);
    const model = sceneTerrainModelFor(res, ir);
    state = res.modelError ? "failed" : "pending";
    if (!model) return state;
    sceneTerrainGPUModel(res, o.device, model);
    const flags = sceneTerrainGPULayers(res, o, model);
    const material = o.material({ color: "#ffffff", roughness: 1, metalness: 0 }, ir.receiveShadow !== false);
    const data = material && material.data;
    if (!res.material || res.materialSize !== data.byteLength) {
      if (res.material) res.material.destroy();
      res.material = o.device.createBuffer({ label: "gosx-terrain-material", size: Math.max(256, data.byteLength), usage: GPUBufferUsage.UNIFORM | GPUBufferUsage.COPY_DST });
      res.materialSize = data.byteLength;
    }
    o.device.queue.writeBuffer(res.material, 0, data);
    const pipeline = sceneTerrainGPUPipeline(res, o);
    const validation = o.validation && o.validation.get(pipeline);
    if (validation && !validation.ready) return "pending";
    const count = sceneTerrainFrame(res, model, o.camera, o.viewProj, o.height, 1);
    sceneTerrainUniforms(model, res.ranges, flags, sceneTerrainTint(), res.uniformData);
    o.device.queue.writeBuffer(res.uniform, 0, res.uniformData);
    if (count > 0) o.device.queue.writeBuffer(res.patchBuffer, 0, res.patches, 0, count * 4);
    sceneTerrainGPUGroups(res, o);
    if (count > 0) {
      pass.setPipeline(validation && validation.pipeline || pipeline);
      pass.setBindGroup(0, o.frameGroup);
      pass.setBindGroup(1, res.group1, [0]);
      pass.setBindGroup(2, res.group2);
      pass.setVertexBuffer(0, res.patchBuffer);
      pass.setIndexBuffer(res.index, "uint16");
      pass.drawIndexed(res.indexCount, count);
    }
    state = "ready";
  } catch (error) {
    res.error = String(error && (error as any).message || error);
    state = "failed";
  } finally {
    if (typeof o.attr === "function") {
      o.attr("data-gosx-scene3d-terrain", state);
      o.attr("data-gosx-scene3d-terrain-patches", String(res.count || 0));
      if (state === "failed") o.attr("data-gosx-scene3d-terrain-error", res.error || res.modelError || "");
    }
  }
  return state;
}
