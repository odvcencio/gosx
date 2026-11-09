// WebGPU temporal anti-aliasing with motion vectors.
//
// The resolve runs on the linear HDR scene color before the post chain, then
// copies its result back into the scene target, so tone mapping, bloom and the
// rest of the chain see the anti-aliased image. While no history exists (the
// first frame, or after a reset) the "taa" effect stays in the chain and the
// post processor draws its FXAA fallback.
//
// Motion: static geometry reprojects through the scene depth and the previous
// unjittered view-projection. Moving retained meshes and skinned meshes render
// a velocity pass (rgba16float: rg = uv offset to the previous frame, b = clip
// w, a = 1) into their own single-sample target; the resolve uses a mover's
// offset only where that mover is the visible surface. Blended compute
// particles write a = 2 into the same target (a reactive mask), and pixels
// near an ocean or pool surface count as water; both keep less history,
// because their surfaces move without depth.
//
// History resets on resize, sample-count or post-chain change, a projection
// change, a camera cut (more than 2 units or 60 degrees in one frame), a gap
// over 250 ms, and renderer replacement (a new renderer owns new history).

const WGSL_TAA_VELOCITY = `
struct MoverUniforms { current: mat4x4f, previous: mat4x4f };
@group(0) @binding(0) var<uniform> mover: MoverUniforms;
struct VelocityOut { @builtin(position) clip: vec4f, @location(0) current: vec4f, @location(1) previous: vec4f };
@vertex fn vertexMain(@location(0) position: vec3f, @location(1) previousPosition: vec3f) -> VelocityOut {
    var out: VelocityOut;
    out.clip = mover.current * vec4f(position, 1.0);
    out.current = out.clip;
    out.previous = mover.previous * vec4f(previousPosition, 1.0);
    return out;
}
fn uvOf(clip: vec4f) -> vec2f { let n = clip.xy / clip.w; return vec2f(n.x * 0.5 + 0.5, 0.5 - n.y * 0.5); }
@fragment fn fragmentMain(in: VelocityOut) -> @location(0) vec4f {
    return vec4f(uvOf(in.previous) - uvOf(in.current), in.current.w, 1.0);
}`;

function wgpuTAAResolveWGSL(multisampled: boolean): string {
  return `
struct TAAParams {
    inverseViewProjection: mat4x4f,
    viewProjection: mat4x4f,
    previousViewProjection: mat4x4f,
    settings: vec4f,
    jitter: vec4f,
    ocean: vec4f,
    pools: array<vec4f, 4>,
};
@group(0) @binding(0) var currentTex: texture_2d<f32>;
@group(0) @binding(1) var historyTex: texture_2d<f32>;
@group(0) @binding(2) var depthTex: ${multisampled ? "texture_depth_multisampled_2d" : "texture_depth_2d"};
@group(0) @binding(3) var velocityTex: texture_2d<f32>;
@group(0) @binding(4) var linearSamp: sampler;
@group(0) @binding(5) var<uniform> taa: TAAParams;
struct FullscreenOut { @builtin(position) position: vec4f, @location(0) uv: vec2f };
@vertex fn vertexMain(@builtin(vertex_index) i: u32) -> FullscreenOut {
    let p = array<vec2f, 3>(vec2f(-1.0, -1.0), vec2f(3.0, -1.0), vec2f(-1.0, 3.0));
    var out: FullscreenOut;
    out.position = vec4f(p[i], 0.0, 1.0);
    out.uv = vec2f(p[i].x * 0.5 + 0.5, 0.5 - p[i].y * 0.5);
    return out;
}
fn toYCoCg(c: vec3f) -> vec3f { return vec3f(dot(c, vec3f(0.25, 0.5, 0.25)), c.r * 0.5 - c.b * 0.5, -c.r * 0.25 + c.g * 0.5 - c.b * 0.25); }
fn fromYCoCg(c: vec3f) -> vec3f { return vec3f(c.x + c.y - c.z, c.x + c.z, c.x - c.y - c.z); }
fn waterAt(world: vec3f) -> bool {
    if (taa.ocean.z > 0.5 && abs(world.y - taa.ocean.x) <= taa.ocean.y) { return true; }
    for (var k = 0u; k < 4u; k = k + 1u) {
        let pool = taa.pools[k];
        if (pool.w > 0.5 && abs(world.x) <= pool.x && abs(world.z) <= pool.y && abs(world.y) <= pool.z) { return true; }
    }
    return false;
}
@fragment fn fragmentMain(in: FullscreenOut) -> @location(0) vec4f {
    let size = vec2i(textureDimensions(currentTex));
    let px = clamp(vec2i(in.position.xy), vec2i(0), size - vec2i(1));
    let current = textureLoad(currentTex, px, 0);
    if (taa.settings.z < 0.5) { return current; }
    let depth = textureLoad(depthTex, px, 0);
    let uv = (vec2f(px) + vec2f(0.5)) / vec2f(size);
    // Remove this frame's jitter before reprojecting through the depth.
    let ndc = vec4f((uv.x - taa.jitter.x) * 2.0 - 1.0, 1.0 - (uv.y - taa.jitter.y) * 2.0, depth, 1.0);
    var world = taa.inverseViewProjection * ndc;
    world = world / world.w;
    let previous = taa.previousViewProjection * vec4f(world.xyz, 1.0);
    if (previous.w <= 0.0) { return current; }
    var historyUV = vec2f(previous.x / previous.w * 0.5 + 0.5, 0.5 - previous.y / previous.w * 0.5);
    var reactive = 0.0;
    if (taa.settings.w > 0.5) {
        let v = textureLoad(velocityTex, px, 0);
        let surfaceW = (taa.viewProjection * vec4f(world.xyz, 1.0)).w;
        if (v.a > 0.5 && v.a < 1.5 && abs(v.b - surfaceW) <= 0.02 * abs(surfaceW) + 0.02) { historyUV = uv + v.xy; }
        if (v.a > 1.5) { reactive = 1.0; }
    }
    if (depth < 0.99999 && waterAt(world.xyz)) { reactive = max(reactive, 0.75); }
    historyUV = historyUV + taa.jitter.zw;
    if (any(historyUV < vec2f(0.0)) || any(historyUV > vec2f(1.0))) { return current; }
    var low = vec4f(1e20);
    var high = vec4f(-1e20);
    var mean = vec4f(0.0);
    var square = vec4f(0.0);
    for (var y = -1; y <= 1; y = y + 1) {
        for (var x = -1; x <= 1; x = x + 1) {
            let s = textureLoad(currentTex, clamp(px + vec2i(x, y), vec2i(0), size - vec2i(1)), 0);
            let c = vec4f(toYCoCg(s.rgb), s.a);
            low = min(low, c); high = max(high, c); mean = mean + c / 9.0; square = square + c * c / 9.0;
        }
    }
    let sigma = sqrt(max(vec4f(0.0), square - mean * mean));
    low = max(low, mean - sigma * taa.settings.y);
    high = min(high, mean + sigma * taa.settings.y);
    let historySample = textureSampleLevel(historyTex, linearSamp, historyUV, 0.0);
    let bounded = clamp(vec4f(toYCoCg(historySample.rgb), historySample.a), low, high);
    let motion = length((historyUV - uv) * vec2f(size));
    let change = abs(bounded.x - toYCoCg(current.rgb).x);
    let weight = taa.settings.x * exp(-motion * 0.05) / (1.0 + change * 8.0) * (1.0 - reactive * 0.85);
    return vec4f(mix(current.rgb, fromYCoCg(bounded.rgb), weight), mix(current.a, bounded.a, weight));
}`;
}

function wgpuTAABlitWGSL(): string {
  return `
@group(0) @binding(0) var source: texture_2d<f32>;
@vertex fn vertexMain(@builtin(vertex_index) i: u32) -> @builtin(position) vec4f {
    let p = array<vec2f, 3>(vec2f(-1.0, -1.0), vec2f(3.0, -1.0), vec2f(-1.0, 3.0));
    return vec4f(p[i], 0.0, 1.0);
}
@fragment fn fragmentMain(@builtin(position) position: vec4f) -> @location(0) vec4f {
    return textureLoad(source, vec2i(position.xy), 0);
}`;
}

const SCENE_WEBGPU_TAA_IDENTITY = new Float32Array([1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1]);

function sceneWebGPUTAAHalton(index: number, base: number): number {
  var value = 0, fraction = 1;
  while (index > 0) { fraction /= base; value += fraction * (index % base); index = Math.floor(index / base); }
  return value;
}

// sceneWebGPUTAAJitter offsets a WebGPU projection by a sub-pixel Halton
// sample and returns the uv-space offset that sample moves the image.
function sceneWebGPUTAAJitter(projection: Float32Array, offset: number, width: number, height: number, index: number) {
  var x = (sceneWebGPUTAAHalton(index % 8 + 1, 2) - 0.5) * 2 / Math.max(1, width);
  var y = (sceneWebGPUTAAHalton(index % 8 + 1, 3) - 0.5) * 2 / Math.max(1, height);
  for (var column = 0; column < 4; column++) {
    projection[offset + column * 4] += x * projection[offset + column * 4 + 3];
    projection[offset + column * 4 + 1] += y * projection[offset + column * 4 + 3];
  }
  // Clip x moves uv x by x / 2; clip y moves uv y by -y / 2.
  return [x * 0.5, -y * 0.5];
}

function sceneWebGPUInvertMat4(m: Float32Array, out: Float32Array): boolean {
  var a = m, inv = out;
  inv[0] = a[5] * a[10] * a[15] - a[5] * a[11] * a[14] - a[9] * a[6] * a[15] + a[9] * a[7] * a[14] + a[13] * a[6] * a[11] - a[13] * a[7] * a[10];
  inv[4] = -a[4] * a[10] * a[15] + a[4] * a[11] * a[14] + a[8] * a[6] * a[15] - a[8] * a[7] * a[14] - a[12] * a[6] * a[11] + a[12] * a[7] * a[10];
  inv[8] = a[4] * a[9] * a[15] - a[4] * a[11] * a[13] - a[8] * a[5] * a[15] + a[8] * a[7] * a[13] + a[12] * a[5] * a[11] - a[12] * a[7] * a[9];
  inv[12] = -a[4] * a[9] * a[14] + a[4] * a[10] * a[13] + a[8] * a[5] * a[14] - a[8] * a[6] * a[13] - a[12] * a[5] * a[10] + a[12] * a[6] * a[9];
  inv[1] = -a[1] * a[10] * a[15] + a[1] * a[11] * a[14] + a[9] * a[2] * a[15] - a[9] * a[3] * a[14] - a[13] * a[2] * a[11] + a[13] * a[3] * a[10];
  inv[5] = a[0] * a[10] * a[15] - a[0] * a[11] * a[14] - a[8] * a[2] * a[15] + a[8] * a[3] * a[14] + a[12] * a[2] * a[11] - a[12] * a[3] * a[10];
  inv[9] = -a[0] * a[9] * a[15] + a[0] * a[11] * a[13] + a[8] * a[1] * a[15] - a[8] * a[3] * a[13] - a[12] * a[1] * a[11] + a[12] * a[3] * a[9];
  inv[13] = a[0] * a[9] * a[14] - a[0] * a[10] * a[13] - a[8] * a[1] * a[14] + a[8] * a[2] * a[13] + a[12] * a[1] * a[10] - a[12] * a[2] * a[9];
  inv[2] = a[1] * a[6] * a[15] - a[1] * a[7] * a[14] - a[5] * a[2] * a[15] + a[5] * a[3] * a[14] + a[13] * a[2] * a[7] - a[13] * a[3] * a[6];
  inv[6] = -a[0] * a[6] * a[15] + a[0] * a[7] * a[14] + a[4] * a[2] * a[15] - a[4] * a[3] * a[14] - a[12] * a[2] * a[7] + a[12] * a[3] * a[6];
  inv[10] = a[0] * a[5] * a[15] - a[0] * a[7] * a[13] - a[4] * a[1] * a[15] + a[4] * a[3] * a[13] + a[12] * a[1] * a[7] - a[12] * a[3] * a[5];
  inv[14] = -a[0] * a[5] * a[14] + a[0] * a[6] * a[13] + a[4] * a[1] * a[14] - a[4] * a[2] * a[13] - a[12] * a[1] * a[6] + a[12] * a[2] * a[5];
  inv[3] = -a[1] * a[6] * a[11] + a[1] * a[7] * a[10] + a[5] * a[2] * a[11] - a[5] * a[3] * a[10] - a[9] * a[2] * a[7] + a[9] * a[3] * a[6];
  inv[7] = a[0] * a[6] * a[11] - a[0] * a[7] * a[10] - a[4] * a[2] * a[11] + a[4] * a[3] * a[10] + a[8] * a[2] * a[7] - a[8] * a[3] * a[6];
  inv[11] = -a[0] * a[5] * a[11] + a[0] * a[7] * a[9] + a[4] * a[1] * a[11] - a[4] * a[3] * a[9] - a[8] * a[1] * a[7] + a[8] * a[3] * a[5];
  inv[15] = a[0] * a[5] * a[10] - a[0] * a[6] * a[9] - a[4] * a[1] * a[10] + a[4] * a[2] * a[9] + a[8] * a[1] * a[6] - a[8] * a[2] * a[5];
  var det = a[0] * inv[0] + a[1] * inv[4] + a[2] * inv[8] + a[3] * inv[12];
  if (!det || !isFinite(det)) return false;
  for (var i = 0; i < 16; i++) inv[i] /= det;
  return true;
}

// sceneWebGPUTAAWaterRegions packs the ocean band and up to four pools.
function sceneWebGPUTAAWaterRegions(bundle: any, out: Float32Array, offset: number) {
  var ocean = bundle && bundle.environment && bundle.environment.ocean;
  out[offset] = ocean ? sceneNumber(ocean.level, 0) : 0;
  out[offset + 1] = ocean ? Math.max(0.05, sceneNumber(ocean.waveHeight, 0.8) * 0.75) : 0;
  out[offset + 2] = ocean ? 1 : 0;
  out[offset + 3] = 0;
  var pools = Array.isArray(bundle && bundle.waterSystems) ? bundle.waterSystems : [];
  for (var k = 0; k < 4; k++) {
    var pool = pools[k], base = offset + 4 + k * 4;
    out[base] = pool ? Math.max(0.001, sceneNumber(pool.poolWidth, 1)) : 0;
    out[base + 1] = pool ? Math.max(0.001, sceneNumber(pool.poolLength, 1)) : 0;
    out[base + 2] = pool ? 0.2 * Math.max(0.001, sceneNumber(pool.poolHeight, 1)) + 0.02 : 0;
    out[base + 3] = pool ? 1 : 0;
  }
}

function taaDestroyTargets(st: any) {
  for (var i = 0; i < st.history.length; i++) st.history[i].texture.destroy();
  if (st.velocity) st.velocity.destroy();
  if (st.velocityDepth) st.velocityDepth.destroy();
  st.history = []; st.velocity = st.velocityDepth = null; st.valid = false;
}
function taaEnsureTargets(st: any, r: any, w: number, h: number) {
  if (st.history.length && w === st.width && h === st.height) return;
  taaDestroyTargets(st);
  for (var i = 0; i < 2; i++) {
    var texture = r.device.createTexture({ label: "gosx-taa-history", size: [w, h, 1], format: "rgba16float",
      usage: GPUTextureUsage.RENDER_ATTACHMENT | GPUTextureUsage.TEXTURE_BINDING });
    st.history.push({ texture: texture, view: texture.createView() });
  }
  st.velocity = r.device.createTexture({ label: "gosx-taa-velocity", size: [w, h, 1], format: "rgba16float",
    usage: GPUTextureUsage.RENDER_ATTACHMENT | GPUTextureUsage.TEXTURE_BINDING });
  st.velocityDepth = r.device.createTexture({ label: "gosx-taa-velocity-depth", size: [w, h, 1], format: "depth24plus",
    usage: GPUTextureUsage.RENDER_ATTACHMENT });
  st.width = w; st.height = h;
}
function taaPipeline(st: any, r: any, key: string, build: () => any) {
  return st.pipelines[key] || (st.pipelines[key] = build());
}
function taaResolvePipeline(st: any, r: any, ms: boolean) {
  return taaPipeline(st, r, "resolve" + (ms ? "ms" : ""), function() {
    var module = r.device.createShaderModule({ label: "taa-resolve", code: wgpuTAAResolveWGSL(ms) });
    var layout = r.device.createBindGroupLayout({ label: "gosx-taa-resolve", entries: [
      { binding: 0, visibility: GPUShaderStage.FRAGMENT, texture: { sampleType: "unfilterable-float" } },
      { binding: 1, visibility: GPUShaderStage.FRAGMENT, texture: { sampleType: "float" } },
      { binding: 2, visibility: GPUShaderStage.FRAGMENT, texture: { sampleType: "depth", multisampled: ms } },
      { binding: 3, visibility: GPUShaderStage.FRAGMENT, texture: { sampleType: "unfilterable-float" } },
      { binding: 4, visibility: GPUShaderStage.FRAGMENT, sampler: { type: "filtering" } },
      { binding: 5, visibility: GPUShaderStage.FRAGMENT, buffer: { type: "uniform" } },
    ] });
    return { layout: layout, pipeline: wgpuCreateValidatedPipeline(r.device, "render", { label: "gosx-taa-resolve", layout: r.device.createPipelineLayout({ bindGroupLayouts: [layout] }),
      vertex: { module: module, entryPoint: "vertexMain" }, fragment: { module: module, entryPoint: "fragmentMain", targets: [{ format: "rgba16float" }] },
      primitive: { topology: "triangle-list" } }) };
  });
}
function taaBlitPipeline(st: any, r: any, format: string) {
  return taaPipeline(st, r, "blit|" + format, function() {
    var module = r.device.createShaderModule({ label: "taa-blit", code: wgpuTAABlitWGSL() });
    var layout = r.device.createBindGroupLayout({ label: "gosx-taa-blit", entries: [
      { binding: 0, visibility: GPUShaderStage.FRAGMENT, texture: { sampleType: "unfilterable-float" } },
    ] });
    return { layout: layout, pipeline: wgpuCreateValidatedPipeline(r.device, "render", { label: "gosx-taa-blit", layout: r.device.createPipelineLayout({ bindGroupLayouts: [layout] }),
      vertex: { module: module, entryPoint: "vertexMain" }, fragment: { module: module, entryPoint: "fragmentMain", targets: [{ format: format }] },
      primitive: { topology: "triangle-list" } }) };
  });
}
function taaVelocityPipeline(st: any, r: any) {
  return taaPipeline(st, r, "velocity", function() {
    var module = r.device.createShaderModule({ label: "taa-velocity", code: WGSL_TAA_VELOCITY });
    var layout = r.device.createBindGroupLayout({ label: "gosx-taa-mover", entries: [
      { binding: 0, visibility: GPUShaderStage.VERTEX, buffer: { type: "uniform", hasDynamicOffset: true, minBindingSize: 128 } },
    ] });
    var position = function(location: number) { return { arrayStride: 12, attributes: [{ shaderLocation: location, offset: 0, format: "float32x3" }] }; };
    return { layout: layout, pipeline: wgpuCreateValidatedPipeline(r.device, "render", { label: "gosx-taa-velocity", layout: r.device.createPipelineLayout({ bindGroupLayouts: [layout] }),
      vertex: { module: module, entryPoint: "vertexMain", buffers: [position(0), position(1)] },
      fragment: { module: module, entryPoint: "fragmentMain", targets: [{ format: "rgba16float" }] },
      primitive: { topology: "triangle-list" }, depthStencil: { format: "depth24plus", depthWriteEnabled: true, depthCompare: "less" } }) };
  });
}
function taaMaskPipeline(st: any, r: any, ms: boolean) {
  return taaPipeline(st, r, "mask" + (ms ? "ms" : ""), function() {
    var code = [WGSL_FRAME_STRUCTS, "@group(0) @binding(0) var<uniform> frame: FrameUniforms;", "struct PointsUniforms {", WGSL_POINTS_UNIFORM_FIELDS, "};",
      "@group(3) @binding(0) var sceneDepth: " + (ms ? "texture_depth_multisampled_2d" : "texture_depth_2d") + ";", WGSL_POINTS_SHADE,
      "@fragment fn fragmentMain(in: PointsInput, @builtin(position) fragCoord: vec4f) -> @location(0) vec4f {",
      "    if (pointsShade(in).a < 0.05 || fragCoord.z > textureLoad(sceneDepth, vec2i(fragCoord.xy), 0)) { discard; }",
      "    return vec4f(0.0, 0.0, 0.0, 2.0);",
      "}"].join("\n");
    var depthLayout = r.device.createBindGroupLayout({ label: "gosx-taa-mask-depth", entries: [
      { binding: 0, visibility: GPUShaderStage.FRAGMENT, texture: { sampleType: "depth", multisampled: ms } },
    ] });
    var module = r.device.createShaderModule({ label: "taa-particle-mask", code: code });
    return { depthLayout: depthLayout, pipeline: wgpuCreateValidatedPipeline(r.device, "render", { label: "gosx-taa-particle-mask",
      layout: r.device.createPipelineLayout({ bindGroupLayouts: [r.frameLayout, r.materialLayout, r.pointsLayout, depthLayout] }),
      vertex: { module: r.pointsVertex, entryPoint: "vertexMain", buffers: [] },
      fragment: { module: module, entryPoint: "fragmentMain", targets: [{ format: "rgba16float",
        blend: { color: { operation: "add", srcFactor: "zero", dstFactor: "one" }, alpha: { operation: "max", srcFactor: "one", dstFactor: "one" } } }] },
      primitive: { topology: "triangle-list" }, depthStencil: { format: "depth24plus", depthWriteEnabled: false, depthCompare: "always" } }) };
  });
}
function taaEnsureMoverBuffer(st: any, r: any, count: number) {
  if (st.moverBuffer && st.moverCapacity >= count) return;
  if (st.moverBuffer) st.moverBuffer.destroy();
  st.moverCapacity = Math.max(16, count * 2);
  st.moverBuffer = r.device.createBuffer({ label: "gosx-taa-movers", size: st.moverCapacity * 256, usage: GPUBufferUsage.UNIFORM | GPUBufferUsage.COPY_DST });
}
// drawMovers renders every retained or skinned mesh that moved since the
// previous frame and returns how many it drew.
function taaDrawMovers(st: any, r: any, pass: any, objects: any[]) {
  var list: any[] = [], scratch = new Float32Array(16), seen = new Set();
  for (var i = 0; i < objects.length; i++) {
    var obj = objects[i];
    if (!obj || obj.viewCulled || !obj.id) continue;
    // Skinned output is already in world space; retained meshes carry a model matrix.
    var skin = r.skinBuffer(obj);
    var model = skin ? SCENE_WEBGPU_TAA_IDENTITY : obj.modelMatrix && obj.modelMatrix.length >= 16 ? obj.modelMatrix : null;
    if (!model || (!obj.retainedGeometry && !skin)) continue;
    seen.add(obj.id);
    var record = st.movers.get(obj.id);
    var moved = !record || skin || record.model.some(function(v: number, k: number) { return v !== model[k]; });
    if (record && moved) list.push({ obj: obj, skin: skin, model: model, previous: record.model.slice() });
    if (!record) st.movers.set(obj.id, record = { model: new Float32Array(16), previousSkin: null });
    record.model.set(model);
  }
  st.movers.forEach(function(_value: any, id: any) { if (!seen.has(id)) st.movers.delete(id); });
  if (!list.length) return 0;
  taaEnsureMoverBuffer(st, r, list.length);
  var vp = taaVelocityPipeline(st, r), data = new Float32Array(64);
  var group = r.device.createBindGroup({ layout: vp.layout, entries: [{ binding: 0, resource: { buffer: st.moverBuffer, size: 128 } }] });
  pass.setPipeline(wgpuRequirePipeline(vp.pipeline));
  var drawn = 0;
  for (var m = 0; m < list.length; m++) {
    var item = list[m];
    sceneMat4MultiplyInto(scratch, st.viewProjection, item.model); data.set(scratch, 0);
    sceneMat4MultiplyInto(scratch, st.previousViewProjection, item.previous); data.set(scratch, 16);
    r.device.queue.writeBuffer(st.moverBuffer, m * 256, data, 0, 32);
    pass.setBindGroup(0, group, [m * 256]);
    if (item.skin) {
      pass.setVertexBuffer(0, item.skin);
      pass.setVertexBuffer(1, r.previousSkin(item.obj) || item.skin);
    } else if (!r.bindPositions(pass, 0, item.obj) || !r.bindPositions(pass, 1, item.obj)) continue;
    var indexCount = r.bindIndices(pass, item.obj);
    if (indexCount > 0) pass.drawIndexed(indexCount); else pass.draw(item.obj.vertexCount);
    drawn += 1;
  }
  return drawn;
}
function taaDrawParticleMask(st: any, r: any, pass: any, records: any[], depthView: any, ms: boolean, frameGroup: any) {
  var mp = null, drawn = 0;
  for (var i = 0; i < (records || []).length; i++) {
    var system = records[i] && records[i].system;
    var cached = system && (system._gosxWGPUCPPointsBG || system._gosxWGPUCPSoftPointsBG);
    var blend = system && system.entry && system.entry.material && String(system.entry.material.blendMode || "").toLowerCase();
    if (!cached || !cached.bg || system.count <= 0 || (blend !== "alpha" && blend !== "additive")) continue;
    if (!mp) {
      mp = taaMaskPipeline(st, r, ms);
      pass.setPipeline(wgpuRequirePipeline(mp.pipeline));
      pass.setBindGroup(0, frameGroup);
      pass.setBindGroup(1, r.materialGroup(), [0]);
      pass.setBindGroup(3, r.device.createBindGroup({ layout: mp.depthLayout, entries: [{ binding: 0, resource: depthView }] }));
    }
    pass.setBindGroup(2, cached.bg);
    pass.draw(6, system.count);
    drawn += 1;
  }
  return drawn;
}

// taaBeginFrame decides whether this frame accumulates history and, if so,
// jitters the projection already written to the frame uniforms.
function taaBeginFrame(st: any, effects: any[], frame: any): boolean {
  var effect = Array.isArray(effects) ? effects.find(function(e: any) { return e && e.kind === "taa"; }) : null;
  st.active = !!effect && frame.postProcessing && frame.perspective;
  if (!st.active) { if (st.history.length) taaDestroyTargets(st); return false; }
  var r = st.resources();
  var nextStamp = JSON.stringify([effects, frame.width, frame.height, frame.samples]);
  var view = frame.view, eye = [-(view[0] * view[12] + view[1] * view[13] + view[2] * view[14]), -(view[4] * view[12] + view[5] * view[13] + view[6] * view[14]), -(view[8] * view[12] + view[9] * view[13] + view[10] * view[14])];
  var forward = [-view[2], -view[6], -view[10]];
  var cut = Math.hypot(eye[0] - st.previousEye[0], eye[1] - st.previousEye[1], eye[2] - st.previousEye[2]) > 2 ||
    forward[0] * st.previousForward[0] + forward[1] * st.previousForward[1] + forward[2] * st.previousForward[2] < 0.5;
  var projectionChanged = [0, 5, 10, 11, 14].some(function(k) { return Math.abs(frame.projection[frame.projectionOffset + k] - st.previousProjection[k]) > 1e-5; });
  if (st.stamp !== nextStamp || frame.now - st.lastAt > 250 || cut || projectionChanged || st.samples !== frame.samples) { if (st.valid) st.resets += 1; st.valid = false; }
  st.stamp = nextStamp; st.lastAt = frame.now; st.samples = frame.samples;
  st.previousEye = eye; st.previousForward = forward;
  st.previousProjection.set(frame.projection.subarray(frame.projectionOffset, frame.projectionOffset + 16));
  taaEnsureTargets(st, r, frame.width, frame.height);
  st.previousViewProjection.set(st.viewProjection);
  st.viewProjection.set(frame.viewProjection);
  st.previousJitter = st.jitter;
  st.jitter = st.valid ? sceneWebGPUTAAJitter(frame.projection, frame.projectionOffset, frame.width, frame.height, st.frameIndex) : [0, 0];
  return st.valid;
}

// taaResolve runs the velocity pass and the temporal resolve, writes the
// result back into the scene target, and returns the effects the post chain
// still needs (without "taa" once the frame resolved).
function taaResolve(st: any, encoder: any, effects: any[], frame: any) {
  if (!st.active || !st.history.length) return effects;
  var r = st.resources(), ms = frame.samples > 1;
  // Create every pipeline this frame may need at once, so they validate
  // together and cost at most one pending frame.
  taaResolvePipeline(st, r, ms); taaBlitPipeline(st, r, frame.colorFormat); taaVelocityPipeline(st, r);
  if (Array.isArray(frame.particles) && frame.particles.length) taaMaskPipeline(st, r, ms);
  var effect = effects.find(function(e: any) { return e && e.kind === "taa"; }) || {};
  var vpass = encoder.beginRenderPass({ label: "gosx-taa-velocity",
    colorAttachments: [{ view: st.velocity.createView(), loadOp: "clear", storeOp: "store", clearValue: { r: 0, g: 0, b: 0, a: 0 } }],
    depthStencilAttachment: { view: st.velocityDepth.createView(), depthLoadOp: "clear", depthClearValue: 1, depthStoreOp: "discard" } });
  st.moverCount = st.valid ? taaDrawMovers(st, r, vpass, frame.objects || []) : 0;
  var masked = st.valid ? taaDrawParticleMask(st, r, vpass, frame.particles, frame.depthView, ms, frame.frameGroup) : 0;
  vpass.end();
  if (!sceneWebGPUInvertMat4(st.viewProjection, st.inverse)) st.valid = false;
  st.params.set(st.inverse, 0); st.params.set(st.viewProjection, 16); st.params.set(st.previousViewProjection, 32);
  st.params[48] = Math.max(0, Math.min(0.95, sceneNumber(effect.historyWeight, 0.9)));
  st.params[49] = Math.max(0.5, Math.min(3, sceneNumber(effect.clampGamma, 1)));
  st.params[50] = st.valid ? 1 : 0;
  st.params[51] = st.moverCount + masked > 0 ? 1 : 0;
  // History holds the previous frame's jittered image: look it up at the
  // reprojected unjittered point plus that frame's st.jitter.
  st.params[52] = st.jitter[0]; st.params[53] = st.jitter[1]; st.params[54] = st.previousJitter[0]; st.params[55] = st.previousJitter[1];
  sceneWebGPUTAAWaterRegions(frame.bundle, st.params, 56);
  if (!st.paramBuffer) st.paramBuffer = r.device.createBuffer({ label: "gosx-taa-params", size: st.params.byteLength, usage: GPUBufferUsage.UNIFORM | GPUBufferUsage.COPY_DST });
  r.device.queue.writeBuffer(st.paramBuffer, 0, st.params);
  var target = st.history[st.historyIndex % 2], previous = st.history[(st.historyIndex + 1) % 2];
  var rp = taaResolvePipeline(st, r, ms);
  var group = r.device.createBindGroup({ layout: rp.layout, entries: [
    { binding: 0, resource: frame.colorView }, { binding: 1, resource: previous.view }, { binding: 2, resource: frame.depthView },
    { binding: 3, resource: st.velocity.createView() }, { binding: 4, resource: r.linearSampler }, { binding: 5, resource: { buffer: st.paramBuffer } },
  ] });
  var pass = encoder.beginRenderPass({ label: "gosx-taa-resolve", colorAttachments: [{ view: target.view, loadOp: "clear", storeOp: "store", clearValue: { r: 0, g: 0, b: 0, a: 0 } }] });
  pass.setPipeline(wgpuRequirePipeline(rp.pipeline)); pass.setBindGroup(0, group); pass.draw(3); pass.end();
  var bp = taaBlitPipeline(st, r, frame.colorFormat);
  var blit = encoder.beginRenderPass({ label: "gosx-taa-copy", colorAttachments: [{ view: frame.colorView, loadOp: "load", storeOp: "store" }] });
  blit.setPipeline(wgpuRequirePipeline(bp.pipeline));
  blit.setBindGroup(0, r.device.createBindGroup({ layout: bp.layout, entries: [{ binding: 0, resource: target.view }] }));
  blit.draw(3); blit.end();
  var resolved = st.valid;
  st.valid = true; st.historyIndex += 1; st.frameIndex += 1;
  return resolved ? effects.filter(function(e: any) { return !e || e.kind !== "taa"; }) : effects;
}

// taaCapturePreviousSkins copies each skinned mesh's positions before this
// frame's skinning dispatch overwrites them.
function taaCapturePreviousSkins(st: any, encoder: any, objects: any[]) {
  if (!st.active || !st.valid) return;
  var r = st.resources();
  for (var i = 0; i < (objects || []).length; i++) {
    var obj = objects[i], skin = obj && r.skinBuffer(obj);
    if (!skin) continue;
    if (!obj._gosxTAAPreviousSkin || obj._gosxTAAPreviousSkin.size !== skin.size) {
      obj._gosxTAAPreviousSkin = r.device.createBuffer({ label: "gosx-taa-previous-skin", size: skin.size, usage: GPUBufferUsage.VERTEX | GPUBufferUsage.COPY_DST });
    }
    encoder.copyBufferToBuffer(skin, 0, obj._gosxTAAPreviousSkin, 0, skin.size);
  }
}

// wgpuCreateTemporalAA owns one renderer's history, jitter and velocity
// state. resources() returns the device, layouts and closure helpers the
// passes need; nothing is allocated until a frame asks for "taa".
function wgpuCreateTemporalAA(resources: () => any) {
  var st: any = {
    resources: resources, history: [], historyIndex: 0, width: 0, height: 0, samples: 0, valid: false,
    stamp: "", lastAt: -Infinity, frameIndex: 0, active: false, resets: 0, jitter: [0, 0], previousJitter: [0, 0],
    viewProjection: new Float32Array(16), previousViewProjection: new Float32Array(16), inverse: new Float32Array(16),
    previousEye: [0, 0, 0], previousForward: [0, 0, -1], previousProjection: new Float32Array(16),
    params: new Float32Array(16 * 3 + 4 * 3 + 16), paramBuffer: null, moverBuffer: null, moverCapacity: 0,
    velocity: null, velocityDepth: null, movers: new Map(), moverCount: 0, pipelines: {},
  };
  return {
    // prepare creates the TAA pipelines with the frame's other pipelines, so
    // a scene that asks for TAA costs no extra pending frame.
    prepare: function(effects: any[], postProcessing: boolean, samples: number) {
      if (!postProcessing || !Array.isArray(effects) || !effects.some(function(e: any) { return e && e.kind === "taa"; })) return;
      var r = st.resources();
      taaResolvePipeline(st, r, samples > 1); taaBlitPipeline(st, r, "rgba16float"); taaVelocityPipeline(st, r);
    },
    beginFrame: function(effects: any[], frame: any): boolean { return taaBeginFrame(st, effects, frame); },
    resolve: function(encoder: any, effects: any[], frame: any) { return taaResolve(st, encoder, effects, frame); },
    capturePreviousSkins: function(encoder: any, objects: any[]) { taaCapturePreviousSkins(st, encoder, objects); },
    stats: function() { return { taaActive: st.active ? 1 : 0, taaHistoryValid: st.valid ? 1 : 0, taaMovers: st.moverCount, taaResets: st.resets }; },
    dispose: function() { taaDestroyTargets(st); if (st.paramBuffer) st.paramBuffer.destroy(); if (st.moverBuffer) st.moverBuffer.destroy(); st.paramBuffer = st.moverBuffer = null; st.movers.clear(); },
  };
}
