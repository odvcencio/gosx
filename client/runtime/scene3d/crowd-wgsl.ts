// WebGPU crowd pass (lazy chunk bootstrap-feature-scene3d-crowd.js).
//
// The twin of crowd-glsl.ts. The renderer calls
// window.__gosx_scene3d_crowd_api.webgpu inside its main pass. One shader
// module serves the three levels of detail through the CROWD_TIER override
// constant; each visible run of blocks is one drawIndexed with its first
// instance, so no buffer is rebound between runs. The fragment stage is the
// renderer's PBR fragment (opts.fragment) with a crowdGlow input added.
//
// Bind groups: 0 the renderer's frame group, 1 the renderer's material layout
// (white textures, the crowd material, a zero cutaway slot), 2 the crowd
// uniform block, the bone palette and the section texture. Every crowd
// resource is a vertex buffer or a texture, so the pass needs no vertex-stage
// storage buffers.

function sceneCrowdVertexWGSL(frameStructs: string): string {
  return frameStructs + `
struct CrowdBlock { v: array<vec4f, 32> };
@group(0) @binding(0) var<uniform> frame: FrameUniforms;
@group(2) @binding(0) var<uniform> crowd: CrowdBlock;
@group(2) @binding(1) var crowdPalette: texture_2d<f32>;
@group(2) @binding(2) var crowdSections: texture_2d<f32>;
override CROWD_TIER: u32 = 0u;
struct CrowdIn {
  @location(0) position: vec3f, @location(1) normal: vec3f, @location(2) joints: vec4<u32>, @location(3) weights: vec4f,
  @location(4) part: vec4<u32>, @location(5) seat: vec4f, @location(6) wave: f32, @location(7) ids: vec2<u32>,
  @location(8) look: vec4<u32>, @location(9) extra: vec4<u32>,
};
struct VertexOutput {
  @builtin(position) clipPos: vec4f,
  @location(0) worldPos: vec3f,
  @location(1) normal: vec3f,
  @location(2) uv: vec2f,
  @location(3) tangent: vec3f,
  @location(4) bitangent: vec3f,
  @location(5) @interpolate(flat) instanceColor: vec4f,
  @location(6) @interpolate(flat) crowdGlow: vec4f,
};
fn crowdSection(s: i32, t: i32) -> vec4f { return textureLoad(crowdSections, vec2i(t, s), 0); }
fn crowdJoint(j: i32, row: i32) -> mat4x4f {
  let x = j * 4;
  return mat4x4f(textureLoad(crowdPalette, vec2i(x, row), 0), textureLoad(crowdPalette, vec2i(x + 1, row), 0),
    textureLoad(crowdPalette, vec2i(x + 2, row), 0), textureLoad(crowdPalette, vec2i(x + 3, row), 0));
}
fn crowdPose(a: i32, ph: f32, j: i32) -> mat4x4f {
  let clip = crowd.v[14 + a];
  var f = fract(ph) * clip.y;
  if (a == 8) { f = clamp(ph, 0.0, 1.0) * clip.y; }
  if (CROWD_TIER > 0u) { return crowdJoint(j, i32(clip.x + floor(f + 0.5))); }
  let f0 = min(floor(f), clip.y - 1.0);
  let m0 = crowdJoint(j, i32(clip.x + f0));
  let m1 = crowdJoint(j, i32(clip.x + f0) + 1);
  return m0 + (m1 - m0) * (f - f0);
}
fn crowdJoin(t1: vec4f, t2: vec4f, eager: f32, now: f32) -> f32 {
  let tau = (eager - 64.0) / 192.0;
  let was = t1.x > tau;
  let is = t1.y > tau;
  if (was == is) { return select(0.0, 1.0, is); }
  let crossing = t2.z + (tau - t1.x) / (t1.y - t1.x) * max(t2.w, 0.001);
  let j = clamp((now - crossing) / 0.4, 0.0, 1.0);
  return select(1.0 - j, j, is);
}
fn crowdPhase(a: i32, synced: bool, t1: vec4f, t2: vec4f, now: f32, seed: f32, variant: f32) -> f32 {
  let cycle = max(crowd.v[14 + a].z, 0.05);
  if (synced) { return (now - t2.x) * t1.z / cycle + t2.y * (seed - 0.5); }
  return now * t1.z * (0.92 + 0.16 * variant) / cycle + seed;
}
@vertex fn vertexMain(in: CrowdIn) -> VertexOutput {
  var out: VertexOutput;
  let s = i32(in.ids.x);
  let t0 = crowdSection(s, 0);
  let t1 = crowdSection(s, 1);
  let t2 = crowdSection(s, 2);
  let flags = i32(t1.w + 0.5);
  let now = crowd.v[4].x;
  let eager = f32(in.extra.x);
  let variant = f32(in.look.w) / 255.0;
  let seed = f32(in.ids.y) / 65536.0;
  let rest = select(0, 1, (flags & 8) != 0);
  let fanA = i32(t0.x + 0.5);
  let fanB = i32(t0.y + 0.5);
  var join = crowdJoin(t1, t2, eager, now);
  join = join * join * (3.0 - 2.0 * join);
  var k = clamp((now - t0.z - eager / 255.0 * 0.6) / max(t0.w, 0.001), 0.0, 1.0);
  k = k * k * (3.0 - 2.0 * k);
  let synced = (flags & 1) != 0;
  let phA = crowdPhase(fanA, synced, t1, t2, now, seed, variant);
  let phB = crowdPhase(fanB, synced, t1, t2, now, seed, variant);
  let phR = crowdPhase(rest, false, t1, t2, now, seed, variant);
  var waveU = -1.0;
  if ((flags & 2) == 0 && in.extra.x < 246u) {
    for (var w = 0; w < 4; w++) {
      let wa = crowd.v[23 + w * 2];
      let wb = crowd.v[24 + w * 2];
      if (wa.w <= 0.0 || wa.z == 0.0 || now < wa.x || waveU >= 0.0) { continue; }
      let x = abs(wa.z) * (now - wa.x) - fract(sign(wa.z) * (in.wave - wa.y));
      let u = (x - clamp(floor(x + 0.5), 0.0, wb.x - 1.0)) / wa.w + 0.5;
      if (u > 0.0 && u < 1.0) { waveU = u; }
    }
  }
  var waveW = 0.0;
  if (waveU >= 0.0) { waveW = smoothstep(0.0, 0.2, waveU) * (1.0 - smoothstep(0.8, 1.0, waveU)); }
  var skin = mat4x4f(vec4f(0.0), vec4f(0.0), vec4f(0.0), vec4f(0.0));
  for (var i = 0; i < 4; i++) {
    let wt = in.weights[i];
    if (wt <= 0.0) { continue; }
    let j = i32(in.joints[i]);
    var m: mat4x4f;
    if (join <= 0.0) { m = crowdPose(rest, phR, j); }
    else {
      m = crowdPose(fanB, phB, j);
      if (k < 1.0 && fanA != fanB) { let ma = crowdPose(fanA, phA, j); m = ma + (m - ma) * k; }
      if (join < 1.0) { let mr = crowdPose(rest, phR, j); m = mr + (m - mr) * join; }
    }
    if (waveW > 0.0) { m = m + (crowdPose(8, waveU, j) - m) * waveW; }
    skin = skin + m * wt;
  }
  let local = (skin * vec4f(in.position, 1.0)).xyz;
  var n = mat3x3f(skin[0].xyz, skin[1].xyz, skin[2].xyz) * in.normal;
  let scale = 0.94 + 0.12 * f32(in.look.w & 15u) / 15.0;
  let model = mat4x4f(crowd.v[0], crowd.v[1], crowd.v[2], crowd.v[3]);
  var yaw = in.seat.w;
  if (CROWD_TIER == 2u) {
    let seatWorld = (model * vec4f(in.seat.xyz, 1.0)).xyz;
    let toCamera = frame.cameraPos - seatWorld;
    yaw = atan2(toCamera.x, toCamera.z) - crowd.v[4].y;
    n = normalize(n + vec3f(0.0, 0.6, 0.0));
  }
  let cy = cos(yaw);
  let sy = sin(yaw);
  let p = vec3f(cy * local.x + sy * local.z, local.y, -sy * local.x + cy * local.z) * scale + in.seat.xyz;
  let nr = vec3f(cy * n.x + sy * n.z, n.y, -sy * n.x + cy * n.z);
  let kind = in.part.y;
  var hidden = kind > 0u && (kind != in.look.z || (flags & 4) == 0);
  if (kind == 5u) { hidden = (in.look.w & 0x90u) != 0x90u; }
  let world = model * vec4f(p, 1.0);
  out.worldPos = world.xyz;
  out.normal = normalize(mat3x3f(model[0].xyz, model[1].xyz, model[2].xyz) * nr);
  out.uv = vec2f(0.0);
  var t = cross(vec3f(1.0, 0.0, 0.0), out.normal);
  if (abs(out.normal.y) < 0.9) { t = cross(vec3f(0.0, 1.0, 0.0), out.normal); }
  out.tangent = normalize(t);
  out.bitangent = cross(out.normal, out.tangent);
  let slot = in.part.x;
  var color = vec3f(0.25, 0.2, 0.15);
  if (slot == 0u) { color = crowdSection(s, 3 + i32(in.look.x)).rgb; }
  else if (slot == 1u) { color = crowd.v[6 + i32((in.look.w >> 4u) & 7u) % max(i32(crowd.v[5].w), 1)].rgb; }
  else if (slot == 2u) { color = crowd.v[5].rgb; }
  else if (slot == 3u || slot == 4u) { color = crowdSection(s, 7 + i32(in.look.y)).rgb; }
  out.instanceColor = vec4f(color, 1.0);
  let flicker = 0.8 + 0.2 * sin(now * 7.0 + seed * 40.0);
  out.crowdGlow = select(vec4f(0.0), vec4f(color * crowd.v[4].w * flicker, 1.0), slot == 4u);
  out.clipPos = frame.projMatrix * frame.viewMatrix * world;
  if (hidden) { out.clipPos = vec4f(2.0, 2.0, 2.0, 1.0); }
  return out;
}`;
}

// sceneCrowdFragmentWGSL adds the glow input to the renderer's PBR fragment
// and adds it after tone mapping, doubled for the linear post chain. The
// bundler strips indentation and renames shader-local variables, so the patch
// touches only module-level names: the VertexOutput struct, the entry points'
// gosxPBRColor(in) calls, frame.toneMap and a function appended to the module.
function sceneCrowdFragmentWGSL(pbr: string): string {
  const field = /(@location\(5\)\s*@interpolate\(flat\)\s*instanceColor\s*:\s*vec4f\s*,)(\s*\};)/;
  if (!field.test(pbr) || pbr.indexOf("gosxPBRColor(in)") < 0) throw new Error("crowd: PBR fragment anchor missing");
  return pbr.replace(field, "$1\n    @location(6) @interpolate(flat) crowdGlow: vec4f,$2").split("gosxPBRColor(in)").join("crowdGlowed(in, gosxPBRColor(in))") +
    "\nfn crowdGlowed(in: VertexOutput, c: vec4f) -> vec4f { return vec4f(c.rgb + in.crowdGlow.rgb * select(0.8, 2.0, frame.toneMap == 4u), c.a); }\n";
}

const sceneCrowdGPUResources = new WeakMap<object, any>();

function sceneCrowdGPUStatic(device: any, height: number): any {
  let res = sceneCrowdGPUResources.get(device);
  if (res && res.fanHeight === height) return res;
  if (res) sceneCrowdGPUDispose(device);
  res = { crowds: new Map(), pipelines: new Map(), fanHeight: height };
  res.bake = sceneCrowdBake(height);
  res.palette = device.createTexture({ label: "gosx-crowd-palette", size: [res.bake.width, res.bake.height, 1], format: "rgba32float", usage: GPUTextureUsage.TEXTURE_BINDING | GPUTextureUsage.COPY_DST });
  device.queue.writeTexture({ texture: res.palette }, res.bake.data, { bytesPerRow: res.bake.width * 16, rowsPerImage: res.bake.height }, [res.bake.width, res.bake.height, 1]);
  res.paletteView = res.palette.createView();
  res.meshes = sceneCrowdMeshes(height).map(function(mesh: any) {
    const vbo = device.createBuffer({ label: "gosx-crowd-mesh", size: Math.ceil(mesh.vertices.byteLength / 4) * 4, usage: GPUBufferUsage.VERTEX | GPUBufferUsage.COPY_DST });
    device.queue.writeBuffer(vbo, 0, mesh.vertices);
    const indices = mesh.indices.length % 2 ? Uint16Array.from(Array.from(mesh.indices).concat([0])) : mesh.indices;
    const ibo = device.createBuffer({ label: "gosx-crowd-index", size: indices.byteLength, usage: GPUBufferUsage.INDEX | GPUBufferUsage.COPY_DST });
    device.queue.writeBuffer(ibo, 0, indices);
    return { vbo, ibo, indexCount: mesh.indexCount };
  });
  res.white = device.createTexture({ label: "gosx-crowd-white", size: [1, 1, 1], format: "rgba8unorm", usage: GPUTextureUsage.TEXTURE_BINDING | GPUTextureUsage.COPY_DST });
  device.queue.writeTexture({ texture: res.white }, new Uint8Array([255, 255, 255, 255]), { bytesPerRow: 4 }, [1, 1, 1]);
  res.whiteView = res.white.createView();
  res.sampler = device.createSampler({ magFilter: "linear", minFilter: "linear" });
  res.slot = device.createBuffer({ label: "gosx-crowd-object", size: 256, usage: GPUBufferUsage.UNIFORM | GPUBufferUsage.COPY_DST });
  res.layout = device.createBindGroupLayout({ label: "gosx-crowd", entries: [
    { binding: 0, visibility: GPUShaderStage.VERTEX, buffer: { type: "uniform" } },
    { binding: 1, visibility: GPUShaderStage.VERTEX, texture: { sampleType: "unfilterable-float" } },
    { binding: 2, visibility: GPUShaderStage.VERTEX, texture: { sampleType: "unfilterable-float" } },
  ] });
  sceneCrowdGPUResources.set(device, res);
  return res;
}

function sceneCrowdGPUDisposeCrowd(gpu: any): void {
  for (const key of ["instances", "uniform", "sections", "material"]) { if (gpu[key]) gpu[key].destroy(); gpu[key] = null; }
}

function sceneCrowdGPUDispose(device: any): void {
  const res = sceneCrowdGPUResources.get(device);
  if (!res) return;
  sceneCrowdGPUResources.delete(device);
  for (const gpu of res.crowds.values()) sceneCrowdGPUDisposeCrowd(gpu);
  for (const mesh of res.meshes || []) { mesh.vbo.destroy(); mesh.ibo.destroy(); }
  for (const key of ["palette", "white", "slot"]) if (res[key]) res[key].destroy();
}

function sceneCrowdGPUCrowd(res: any, device: any, ir: any, store: any, o: any): any {
  let gpu = res.crowds.get(ir.id);
  if (!gpu || gpu.ir !== ir) {
    if (gpu) sceneCrowdGPUDisposeCrowd(gpu);
    const packed = sceneCrowdPack(ir);
    if (packed.error) throw new Error(packed.error);
    gpu = { ir, packed, model: sceneCrowdModel(ir), version: -1, uniforms: new Float32Array(SCENE_CROWD_UNIFORM_VEC4S * 4) };
    gpu.instances = device.createBuffer({ label: "gosx-crowd-fans", size: Math.max(32, packed.records.byteLength), usage: GPUBufferUsage.VERTEX | GPUBufferUsage.COPY_DST });
    device.queue.writeBuffer(gpu.instances, 0, packed.records);
    gpu.uniform = device.createBuffer({ label: "gosx-crowd-uniform", size: gpu.uniforms.byteLength, usage: GPUBufferUsage.UNIFORM | GPUBufferUsage.COPY_DST });
    gpu.sections = device.createTexture({ label: "gosx-crowd-sections", size: [SCENE_CROWD_SECTION_TEXELS, ir.sections.length, 1], format: "rgba32float", usage: GPUTextureUsage.TEXTURE_BINDING | GPUTextureUsage.COPY_DST });
    gpu.group = device.createBindGroup({ label: "gosx-crowd", layout: res.layout, entries: [
      { binding: 0, resource: { buffer: gpu.uniform } }, { binding: 1, resource: res.paletteView }, { binding: 2, resource: gpu.sections.createView() },
    ] });
    store.fans = packed.sectionFans;
    res.crowds.set(ir.id, gpu);
  }
  if (gpu.version !== store.version) {
    device.queue.writeTexture({ texture: gpu.sections }, store.sections, { bytesPerRow: SCENE_CROWD_SECTION_TEXELS * 16, rowsPerImage: ir.sections.length }, [SCENE_CROWD_SECTION_TEXELS, ir.sections.length, 1]);
    gpu.version = store.version;
  }
  const material = o.material({ color: "#ffffff", roughness: Number(ir.material && ir.material.roughness) || 0.85, metalness: 0 }, ir.receiveShadow !== false);
  const data = material && material.data;
  if (!gpu.material || gpu.materialSize !== data.byteLength) {
    if (gpu.material) gpu.material.destroy();
    gpu.material = device.createBuffer({ label: "gosx-crowd-material", size: Math.max(256, data.byteLength), usage: GPUBufferUsage.UNIFORM | GPUBufferUsage.COPY_DST });
    gpu.materialSize = data.byteLength; gpu.materialGroup = null;
  }
  device.queue.writeBuffer(gpu.material, 0, data);
  if (!gpu.materialGroup || gpu.materialLayout !== o.materialLayout) {
    const entries: any[] = [{ binding: 0, resource: { buffer: gpu.material } }];
    for (let i = 0; i < 9; i++) entries.push({ binding: 2 * i + 1, resource: res.whiteView }, { binding: 2 * i + 2, resource: res.sampler });
    entries.push({ binding: 19, resource: { buffer: res.slot, size: 112 } });
    gpu.materialGroup = device.createBindGroup({ label: "gosx-crowd-material", layout: o.materialLayout, entries });
    gpu.materialLayout = o.materialLayout;
  }
  return gpu;
}

function sceneCrowdGPUPipeline(res: any, o: any, tier: number): any {
  const key = tier + "|" + o.format + "|" + o.samples + "|" + (o.specular ? 1 : 0);
  let pipeline = res.pipelines.get(key);
  if (pipeline) return pipeline;
  const device = o.device;
  if (!res.vertexModule || res.vertexSource !== o.frameStructs) {
    res.vertexModule = device.createShaderModule({ label: "gosx-crowd-vert", code: sceneCrowdVertexWGSL(o.frameStructs) });
    res.vertexSource = o.frameStructs;
  }
  if (!res.fragmentModule || res.fragmentSource !== o.fragment) {
    res.fragmentModule = device.createShaderModule({ label: "gosx-crowd-frag", code: sceneCrowdFragmentWGSL(o.fragment) });
    res.fragmentSource = o.fragment;
  }
  const targets = o.specular ? [{ format: o.format }, { format: "rgba16float" }] : [{ format: o.format }];
  const descriptor = {
    label: "gosx-crowd-" + tier,
    layout: device.createPipelineLayout({ bindGroupLayouts: [o.frameLayout, o.materialLayout, res.layout] }),
    vertex: { module: res.vertexModule, entryPoint: "vertexMain", constants: { CROWD_TIER: tier }, buffers: [
      { arrayStride: SCENE_CROWD_VERTEX_BYTES, stepMode: "vertex", attributes: [
        { shaderLocation: 0, offset: 0, format: "float32x3" }, { shaderLocation: 1, offset: 12, format: "float32x3" },
        { shaderLocation: 2, offset: 24, format: "uint8x4" }, { shaderLocation: 3, offset: 28, format: "unorm8x4" },
        { shaderLocation: 4, offset: 32, format: "uint8x4" }] },
      { arrayStride: SCENE_CROWD_RECORD_BYTES, stepMode: "instance", attributes: [
        { shaderLocation: 5, offset: 0, format: "float32x4" }, { shaderLocation: 6, offset: 16, format: "float32" },
        { shaderLocation: 7, offset: 20, format: "uint16x2" }, { shaderLocation: 8, offset: 24, format: "uint8x4" },
        { shaderLocation: 9, offset: 28, format: "uint8x4" }] },
    ] },
    fragment: { module: res.fragmentModule, entryPoint: o.specular ? "fragmentMainSpecular" : "fragmentMain", targets },
    primitive: { topology: "triangle-list", cullMode: "back", frontFace: "ccw" },
    multisample: { count: Math.max(1, o.samples | 0) },
    depthStencil: { format: "depth24plus", depthWriteEnabled: true, depthCompare: "less-equal" },
  };
  pipeline = typeof o.create === "function" ? o.create(device, "render", descriptor) : device.createRenderPipeline(descriptor);
  res.pipelines.set(key, pipeline);
  return pipeline;
}

// sceneCrowdGPUDraw is the renderer's per-frame entry; it returns the state it
// publishes as data-gosx-scene3d-crowd: none, pending, ready or failed.
function sceneCrowdGPUDraw(pass: any, o: any): string {
  const crowds = o.environment && Array.isArray(o.environment.crowds) ? o.environment.crowds : null;
  const stats = { state: "none", fans: 0, full: 0, low: 0, impostor: 0, draws: 0, cpuMs: 0, error: "" };
  const started = typeof performance !== "undefined" ? performance.now() : 0;
  try {
    if (!crowds || !crowds.length) { if (o.device && sceneCrowdGPUResources.has(o.device)) sceneCrowdGPUDispose(o.device); return stats.state; }
    const now = Number(o.time) || 0;
    sceneCrowdNoteClock(o.mount, now);
    const live = new Set();
    stats.state = "ready";
    for (const ir of crowds) {
      if (!ir || !ir.id) continue;
      live.add(ir.id);
      const store = sceneCrowdStore(o.mount, ir);
      store.lastNow = now;
      const res = sceneCrowdGPUStatic(o.device, Number(ir.fan && ir.fan.height) || 1.75);
      const gpu = sceneCrowdGPUCrowd(res, o.device, ir, store, o);
      const pick = sceneCrowdSelect(gpu, gpu.packed, ir, o);
      stats.fans += gpu.packed.count; stats.full += pick.fans[0]; stats.low += pick.fans[1]; stats.impostor += pick.fans[2];
      o.device.queue.writeBuffer(gpu.uniform, 0, sceneCrowdUniforms(ir, store, res.bake, gpu.model, now, gpu.uniforms));
      for (let tier = 0; tier < 3; tier++) {
        const ranges = pick.ranges[tier];
        if (!ranges.length) continue;
        const pipeline = sceneCrowdGPUPipeline(res, o, tier);
        const validation = o.validation && o.validation.get(pipeline);
        if (validation && !validation.ready) { stats.state = "pending"; continue; }
        const mesh = res.meshes[tier];
        pass.setPipeline(validation && validation.pipeline || pipeline);
        pass.setBindGroup(0, o.frameGroup);
        pass.setBindGroup(1, gpu.materialGroup, [0]);
        pass.setBindGroup(2, gpu.group);
        pass.setVertexBuffer(0, mesh.vbo);
        pass.setVertexBuffer(1, gpu.instances);
        pass.setIndexBuffer(mesh.ibo, "uint16");
        for (let r = 0; r < ranges.length; r += 2) { pass.drawIndexed(mesh.indexCount, ranges[r + 1], 0, 0, ranges[r]); stats.draws++; }
      }
    }
    const res = sceneCrowdGPUResources.get(o.device);
    if (res) for (const id of Array.from(res.crowds.keys())) if (!live.has(id)) { sceneCrowdGPUDisposeCrowd(res.crowds.get(id)); res.crowds.delete(id); }
  } catch (error) {
    stats.state = "failed";
    stats.error = String(error && (error as any).message || error);
  } finally {
    stats.cpuMs = typeof performance !== "undefined" ? performance.now() - started : 0;
    sceneCrowdPublish(o.mount, stats);
  }
  return stats.state;
}
