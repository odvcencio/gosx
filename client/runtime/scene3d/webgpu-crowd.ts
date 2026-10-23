// Crowd batches on the Scene3D WebGPU renderer.
//
// A crowd batch record (_crowdBatch, built by sceneAppendCrowdBatchRecords)
// stands for every agent of a batch: palette-skinned agents in "pose" mode, or
// GPU-motion agents in "motion" mode. The WebGL renderer draws it from
// instance-rate attributes. This renderer pulls each agent's record from a
// storage buffer instead, so culling can hand the draw a list of survivor
// indices without touching the records:
//
//   vertex input   location 4 = survivor slot (u32, instance step),
//                  locations 5 and 6 = joints and weights (per vertex)
//   group 2 (color) or group 1 (shadow)
//     0 uniform  CrowdParams (parent matrix, clock, bounds sphere, clip table)
//     1 texture  joint-pose atlas (rgba32float, one texel per matrix column)
//     2 storage  pose mode: 16 floats per agent (transform);
//                motion mode: 24 floats per agent (the GSP3 motion record)
//     3 storage  pose mode: 3 floats per agent (atlas rows a, b, blend)
//
// The shaders mirror the WebGL renderer's SCENE_CROWD_SKIN_GLSL and
// SCENE_CROWD_SKIN_MOTION_GLSL line for line; scene3d-webgpu-crowd.test.js pins
// the constants.
//
// Culling (bundle.gpuDriven, the same opt-in as GPU-driven instancing): one
// compute dispatch per batch and view tests each agent's bounds sphere against
// the view frustum. With gpuDriven.occlusion the main pass runs in two phases,
// like indirect-instancing.ts: the early pass draws the agents that were
// visible last frame, a Hi-Z pyramid is built from that pass's depth (decks and
// walls drawn before it included), and the late pass draws the agents that the
// pyramid shows to be newly visible. The pyramid is built from the current
// frame, so a moving camera or ship cannot reproject stale depth.

const SCENE_WEBGPU_CROWD_RECORD_FLOATS = 24;
const SCENE_WEBGPU_CROWD_MAX_CLIPS = 32;
const SCENE_WEBGPU_CROWD_EXTRAPOLATION_SECONDS = 0.25;
const SCENE_WEBGPU_CROWD_PARAMS_BYTES = 656;
const SCENE_WEBGPU_CROWD_VIEW_BYTES = 448;
const SCENE_WEBGPU_CROWD_VIEW_STRIDE = 512;
const SCENE_WEBGPU_CROWD_ARGS_BYTES = 128;
const SCENE_WEBGPU_CROWD_MAX_LEVELS = 16;
const SCENE_WEBGPU_CROWD_PALETTE_BUDGET = 32 * 1024 * 1024;

// A frame without crowd batches publishes no crowd statistics.
const SCENE_WEBGPU_CROWD_IDLE_STATS = Object.freeze({});

const SCENE_WEBGPU_CROWD_STRUCTS = [
  "struct CrowdParams {",
  "    parent: mat4x4f,",
  "    timing: vec4f,",
  "    sphere: vec4f,",
  "    counts: vec4u,",
  "    clips: array<vec4f, " + SCENE_WEBGPU_CROWD_MAX_CLIPS + ">,",
  "    cut: array<vec4f, 2>,",
  "};",
].join("\n");

function sceneWebGPUCrowdDecls(group: number): string {
  return [
    "@group(" + group + ") @binding(0) var<uniform> crowdParams: CrowdParams;",
    "@group(" + group + ") @binding(1) var crowdAtlas: texture_2d<f32>;",
    "@group(" + group + ") @binding(2) var<storage, read> crowdAgents: array<f32>;",
    "@group(" + group + ") @binding(3) var<storage, read> crowdPoses: array<f32>;",
  ].join("\n");
}

// Agent models. Read only crowdParams and crowdAgents, so the cull kernel
// shares them with the vertex shaders.
const SCENE_WEBGPU_CROWD_MODEL_WGSL = [
  "const CROWD_TWO_PI: f32 = 6.283185307179586;",
  "const CROWD_PI: f32 = 3.141592653589793;",
  "fn crowdV3(i: u32) -> vec3f { return vec3f(crowdAgents[i], crowdAgents[i + 1u], crowdAgents[i + 2u]); }",
  "fn crowdPoseModel(slot: u32) -> mat4x4f {",
  "    let o = slot * 16u;",
  "    return mat4x4f(",
  "        vec4f(crowdAgents[o], crowdAgents[o + 1u], crowdAgents[o + 2u], crowdAgents[o + 3u]),",
  "        vec4f(crowdAgents[o + 4u], crowdAgents[o + 5u], crowdAgents[o + 6u], crowdAgents[o + 7u]),",
  "        vec4f(crowdAgents[o + 8u], crowdAgents[o + 9u], crowdAgents[o + 10u], crowdAgents[o + 11u]),",
  "        vec4f(crowdAgents[o + 12u], crowdAgents[o + 13u], crowdAgents[o + 14u], crowdAgents[o + 15u]));",
  "}",
  "fn crowdWrapLerp(a: f32, b: f32, t: f32) -> f32 {",
  "    var d = b - a;",
  "    d = d - CROWD_TWO_PI * floor((d + CROWD_PI) / CROWD_TWO_PI);",
  "    return a + d * t;",
  "}",
  "fn crowdMotionFactor(b: u32) -> f32 {",
  "    let tPrev = crowdAgents[b + 9u];",
  "    let tNext = crowdAgents[b + 19u];",
  "    let now = crowdParams.timing.x;",
  "    let span = max(tNext - tPrev, 1e-6);",
  "    if (now <= tPrev) { return 0.0; }",
  "    if (now < tNext) { return (now - tPrev) / span; }",
  "    return 1.0 + min(now - tNext, crowdParams.timing.y) / span;",
  "}",
  "fn crowdMotionModel(b: u32) -> mat4x4f {",
  "    let t = crowdMotionFactor(b);",
  "    let pos = mix(crowdV3(b), crowdV3(b + 10u), t);",
  "    let r0 = crowdV3(b + 3u);",
  "    let r1 = crowdV3(b + 13u);",
  "    let rot = vec3f(crowdWrapLerp(r0.x, r1.x, t), crowdWrapLerp(r0.y, r1.y, t), crowdWrapLerp(r0.z, r1.z, t));",
  "    let scale = mix(crowdV3(b + 6u), crowdV3(b + 16u), t);",
  "    let cx = cos(rot.x); let sx = sin(rot.x);",
  "    let cy = cos(rot.y); let sy = sin(rot.y);",
  "    let cz = cos(rot.z); let sz = sin(rot.z);",
  "    return mat4x4f(",
  "        vec4f(cy * cz * scale.x, cy * sz * scale.x, -sy * scale.x, 0.0),",
  "        vec4f((sx * sy * cz - cx * sz) * scale.y, (sx * sy * sz + cx * cz) * scale.y, sx * cy * scale.y, 0.0),",
  "        vec4f((cx * sy * cz + sx * sz) * scale.z, (cx * sy * sz - sx * cz) * scale.z, cx * cy * scale.z, 0.0),",
  "        vec4f(pos, 1.0));",
  "}",
].join("\n");

// Skinning. Vertex shaders only: reads the atlas and the pose rows.
const SCENE_WEBGPU_CROWD_SKIN_WGSL = [
  "fn crowdJoint(joint: i32, row: i32) -> mat4x4f {",
  "    let x = joint * 4;",
  "    return mat4x4f(textureLoad(crowdAtlas, vec2i(x, row), 0), textureLoad(crowdAtlas, vec2i(x + 1, row), 0),",
  "        textureLoad(crowdAtlas, vec2i(x + 2, row), 0), textureLoad(crowdAtlas, vec2i(x + 3, row), 0));",
  "}",
  "fn crowdSkin(joints: vec4f, weights: vec4f, pose: vec3f) -> mat4x4f {",
  "    var m = mat4x4f(vec4f(0.0), vec4f(0.0), vec4f(0.0), vec4f(0.0));",
  "    var total = 0.0;",
  "    for (var i = 0; i < 4; i++) {",
  "        let w = weights[i];",
  "        if (w > 0.0) {",
  "            let a = crowdJoint(i32(joints[i]), i32(pose.x));",
  "            let b = crowdJoint(i32(joints[i]), i32(pose.y));",
  "            m = m + (a * (1.0 - pose.z) + b * pose.z) * w;",
  "            total = total + w;",
  "        }",
  "    }",
  "    if (total > 0.0) { return m; }",
  "    return mat4x4f(vec4f(1.0, 0.0, 0.0, 0.0), vec4f(0.0, 1.0, 0.0, 0.0), vec4f(0.0, 0.0, 1.0, 0.0), vec4f(0.0, 0.0, 0.0, 1.0));",
  "}",
  "fn crowdMotionPoseRows(b: u32) -> vec3f {",
  "    let clipRow = clamp(i32(crowdAgents[b + 20u]), 0, " + (SCENE_WEBGPU_CROWD_MAX_CLIPS - 1) + ");",
  "    let clip = crowdParams.clips[clipRow];",
  "    let duration = clip.z;",
  "    var elapsed = (crowdParams.timing.x - crowdAgents[b + 21u]) * crowdAgents[b + 23u];",
  "    if (!(elapsed >= 0.0)) { elapsed = 0.0; }",
  "    var t = 0.0;",
  "    if (duration > 0.0) {",
  "        if (crowdAgents[b + 22u] > 0.5) { t = elapsed - duration * floor(elapsed / duration); } else { t = min(elapsed, duration); }",
  "    }",
  "    let segments = clip.y;",
  "    var frame = 0.0;",
  "    if (duration > 0.0) { frame = t / duration * segments; }",
  "    let a = min(segments, floor(frame));",
  "    return vec3f(clip.x + a, clip.x + min(segments, a + 1.0), frame - a);",
  "}",
].join("\n");

function sceneWebGPUCrowdModelFn(motion: boolean): string {
  return motion
    ? [
      "fn crowdModel(slot: u32, joints: vec4f, weights: vec4f) -> mat4x4f {",
      "    let b = slot * " + SCENE_WEBGPU_CROWD_RECORD_FLOATS + "u;",
      "    return crowdParams.parent * crowdMotionModel(b) * crowdSkin(joints, weights, crowdMotionPoseRows(b));",
      "}",
    ].join("\n")
    : [
      "fn crowdModel(slot: u32, joints: vec4f, weights: vec4f) -> mat4x4f {",
      "    let o = slot * 3u;",
      "    let pose = vec3f(crowdPoses[o], crowdPoses[o + 1u], crowdPoses[o + 2u]);",
      "    return crowdParams.parent * crowdPoseModel(slot) * crowdSkin(joints, weights, pose);",
      "}",
    ].join("\n");
}

function sceneWebGPUCrowdBlock(group: number, motion: boolean): string {
  return [SCENE_WEBGPU_CROWD_STRUCTS, sceneWebGPUCrowdDecls(group), SCENE_WEBGPU_CROWD_MODEL_WGSL,
    SCENE_WEBGPU_CROWD_SKIN_WGSL, sceneWebGPUCrowdModelFn(motion), SCENE_WEBGPU_CUTAWAY_CROWD_WGSL].join("\n");
}

// The color shader is the instanced PBR vertex shader with the instance matrix
// replaced by the crowd model and the instance color by white, as the WebGL
// crowd shaders replace a_instanceMatrix. It is written out in full rather than
// patched from the instanced shader's text: the bundle build strips that text's
// indentation and renames its locals.
function sceneWebGPUCrowdColorVertexWGSL(motion: boolean): string {
  return [
    WGSL_FRAME_STRUCTS,
    WGSL_AFFINE_NORMAL,
    "struct VertexInput {",
    "    @location(0) position: vec3f,",
    "    @location(1) normal: vec3f,",
    "    @location(2) uv: vec2f,",
    "    @location(3) tangent: vec4f,",
    "    @location(4) crowdSlot: u32,",
    "    @location(5) joints: vec4f,",
    "    @location(6) weights: vec4f,",
    "};",
    "struct VertexOutput {",
    "    @builtin(position) clipPos: vec4f,",
    "    @location(0) worldPos: vec3f,",
    "    @location(1) normal: vec3f,",
    "    @location(2) uv: vec2f,",
    "    @location(3) tangent: vec3f,",
    "    @location(4) bitangent: vec3f,",
    "    @location(5) @interpolate(flat) instanceColor: vec4f,",
    "};",
    "@group(0) @binding(0) var<uniform> frame: FrameUniforms;",
    sceneWebGPUCrowdBlock(2, motion),
    "@vertex fn vertexMain(in: VertexInput) -> VertexOutput {",
    "    var out: VertexOutput;",
    "    let agentModel = crowdModel(in.crowdSlot, in.joints, in.weights);",
    "    if (crowdCulled(agentModel)) { out.clipPos = vec4f(2.0, 2.0, 2.0, 1.0); return out; }",
    "    let world = agentModel * vec4f(in.position, 1.0);",
    "    out.worldPos = world.xyz;",
    "    let an = gosxAffineNormal(agentModel, in.normal);",
    "    out.normal = an.xyz;",
    "    out.uv = in.uv;",
    "    let rawT = (agentModel * vec4f(in.tangent.xyz, 0.0)).xyz;",
    "    let N = out.normal;",
    "    var tangentFrame = rawT - N * dot(N, rawT);",
    "    if (dot(tangentFrame, tangentFrame) < 1e-12) {",
    "        let axis = select(vec3f(0.0, 1.0, 0.0), vec3f(1.0, 0.0, 0.0), abs(N.x) < 0.8);",
    "        tangentFrame = axis - N * dot(N, axis);",
    "    }",
    "    let T = normalize(tangentFrame);",
    "    out.tangent = T;",
    "    out.bitangent = cross(N, T) * in.tangent.w * an.w;",
    "    out.instanceColor = vec4f(1.0);",
    "    out.clipPos = frame.projMatrix * frame.viewMatrix * world;",
    "    return out;",
    "}",
  ].join("\n");
}

function sceneWebGPUCrowdShadowVertexWGSL(motion: boolean): string {
  return [
    WGSL_AFFINE_NORMAL,
    "struct ShadowFrameUniforms {",
    "    lightViewProjection: mat4x4f,",
    "};",
    "struct VertexOutput {",
    "    @builtin(position) clipPos: vec4f,",
    "    @location(0) @interpolate(flat) orientation: f32,",
    "};",
    "struct VertexInput {",
    "    @location(0) position: vec3f,",
    "    @location(4) crowdSlot: u32,",
    "    @location(5) joints: vec4f,",
    "    @location(6) weights: vec4f,",
    "};",
    "@group(0) @binding(0) var<uniform> shadowFrame: ShadowFrameUniforms;",
    sceneWebGPUCrowdBlock(1, motion),
    "@vertex fn vertexMain(in: VertexInput) -> VertexOutput {",
    "    let model = crowdModel(in.crowdSlot, in.joints, in.weights);",
    "    var out: VertexOutput;",
    "    if (crowdCulled(model)) { out.clipPos = vec4f(2.0, 2.0, 2.0, 1.0); out.orientation = 1.0; return out; }",
    "    out.clipPos = shadowFrame.lightViewProjection * model * vec4f(in.position, 1.0);",
    "    out.orientation = gosxAffineNormal(model, vec3f(0.0, 0.0, 1.0)).w;",
    "    return out;",
    "}",
  ].join("\n");
}

const SCENE_WEBGPU_CROWD_CULL_WGSL = [
  SCENE_WEBGPU_CROWD_STRUCTS,
  "struct CrowdView {",
  "    viewProj: mat4x4f,",
  "    planes: array<vec4f, 6>,",
  "    viewport: vec4f,",
  "    control: vec4u,",
  "    levels: array<vec4u, " + SCENE_WEBGPU_CROWD_MAX_LEVELS + ">,",
  "};",
  "@group(0) @binding(0) var<uniform> crowdView: CrowdView;",
  "@group(0) @binding(1) var<uniform> crowdParams: CrowdParams;",
  "@group(0) @binding(2) var<storage, read> crowdAgents: array<f32>;",
  "@group(0) @binding(3) var<storage, read_write> crowdArgs: array<atomic<u32>>;",
  "@group(0) @binding(4) var<storage, read_write> crowdVisible: array<u32>;",
  "@group(0) @binding(5) var<storage, read_write> crowdVisibility: array<u32>;",
  "@group(0) @binding(6) var<storage, read> crowdHzb: array<f32>;",
  SCENE_WEBGPU_CROWD_MODEL_WGSL,
  // control: x = list slot, y = phase (0 early camera, 1 late camera, 2 shadow),
  // z = Hi-Z level count (0 without occlusion). counts.w = args word base.
  "@compute @workgroup_size(64)",
  "fn cull(@builtin(global_invocation_id) gid: vec3u) {",
  "    let i = gid.x;",
  "    let count = crowdParams.counts.x;",
  "    if (i >= count) { return; }",
  "    let slot = crowdView.control.x;",
  "    let phase = crowdView.control.y;",
  "    var model: mat4x4f;",
  "    if (crowdParams.counts.y == 1u) { model = crowdParams.parent * crowdMotionModel(i * " + SCENE_WEBGPU_CROWD_RECORD_FLOATS + "u); }",
  "    else { model = crowdParams.parent * crowdPoseModel(i); }",
  "    let ls = crowdParams.sphere;",
  "    let center = (model * vec4f(ls.xyz, 1.0)).xyz;",
  "    let c0 = model[0].xyz; let c1 = model[1].xyz; let c2 = model[2].xyz;",
  "    let l0 = dot(c0, c0); let l1 = dot(c1, c1); let l2 = dot(c2, c2);",
  "    var scale2 = l0 + l1 + l2;",
  "    if (dot(c0, c1) * dot(c0, c1) <= 0.00000001 * l0 * l1 && dot(c0, c2) * dot(c0, c2) <= 0.00000001 * l0 * l2 && dot(c1, c2) * dot(c1, c2) <= 0.00000001 * l1 * l2) {",
  "        scale2 = max(l0, max(l1, l2));",
  "    }",
  "    let radius = ls.w * sqrt(scale2) * 1.001;",
  "    var keep = true;",
  "    if (phase == 2u && crowdParams.counts.z == 0u) { keep = false; }",
  "    for (var p = 0; p < 6; p = p + 1) {",
  "        let plane = crowdView.planes[p];",
  "        if (dot(plane.xyz, center) + plane.w < -radius) { keep = false; break; }",
  "    }",
  "    if (phase == 1u && keep && crowdView.control.z > 0u) {",
  "        let vp = crowdView.viewProj;",
  "        var behind = false;",
  "        var firstCorner = true;",
  "        var minX = 0.0; var maxX = 0.0; var minY = 0.0; var maxY = 0.0; var minZ = 0.0;",
  "        for (var c = 0u; c < 8u; c = c + 1u) {",
  "            let sx = select(-1.0, 1.0, (c & 1u) != 0u);",
  "            let sy = select(-1.0, 1.0, (c & 2u) != 0u);",
  "            let sz = select(-1.0, 1.0, (c & 4u) != 0u);",
  "            let clip = vp * vec4f(center + vec3f(sx, sy, sz) * radius, 1.0);",
  "            if (clip.w <= 0.0001) { behind = true; }",
  "            else {",
  "                let nx = clip.x / clip.w; let ny = clip.y / clip.w; let nz = clip.z / clip.w;",
  "                if (firstCorner) { minX = nx; maxX = nx; minY = ny; maxY = ny; minZ = nz; firstCorner = false; }",
  "                else { minX = min(minX, nx); maxX = max(maxX, nx); minY = min(minY, ny); maxY = max(maxY, ny); minZ = min(minZ, nz); }",
  "            }",
  "        }",
  "        if (!behind) {",
  "            let w = crowdView.viewport.x;",
  "            let h = crowdView.viewport.y;",
  "            let x0 = clamp((minX * 0.5 + 0.5) * w, 0.0, w);",
  "            let x1 = clamp((maxX * 0.5 + 0.5) * w, 0.0, w);",
  "            let y0 = clamp((0.5 - maxY * 0.5) * h, 0.0, h);",
  "            let y1 = clamp((0.5 - minY * 0.5) * h, 0.0, h);",
  "            let extent = max(x1 - x0, y1 - y0);",
  "            var level = u32(max(ceil(log2(max(extent, 1.0))) - 1.0, 0.0));",
  "            if (level >= crowdView.control.z) { level = crowdView.control.z - 1u; }",
  "            let lv = crowdView.levels[level];",
  "            let texel = exp2(f32(level) + 1.0);",
  "            let tx0 = min(u32(x0 / texel), lv.y - 1u);",
  "            let tx1 = min(u32(x1 / texel), lv.y - 1u);",
  "            let ty0 = min(u32(y0 / texel), lv.z - 1u);",
  "            let ty1 = min(u32(y1 / texel), lv.z - 1u);",
  "            let z00 = crowdHzb[lv.x + ty0 * lv.y + tx0];",
  "            let z10 = crowdHzb[lv.x + ty0 * lv.y + tx1];",
  "            let z01 = crowdHzb[lv.x + ty1 * lv.y + tx0];",
  "            let z11 = crowdHzb[lv.x + ty1 * lv.y + tx1];",
  "            let farthest = max(max(z00, z10), max(z01, z11));",
  "            if (minZ - 0.00001 > farthest) { keep = false; }",
  "        }",
  "    }",
  "    if (phase == 1u) {",
  "        let prev = crowdVisibility[i];",
  "        if (keep) { crowdVisibility[i] = 1u; } else { crowdVisibility[i] = 0u; }",
  "        if (prev == 1u) { keep = false; }",
  "    }",
  "    if (phase == 0u && crowdView.control.z > 0u) {",
  "        if (crowdVisibility[i] == 0u) { keep = false; }",
  "    }",
  "    if (keep) {",
  "        let n = atomicAdd(&crowdArgs[crowdParams.counts.w + slot * 8u + 1u], 1u);",
  "        crowdVisible[slot * count + n] = i;",
  "    }",
  "}",
].join("\n");

const SCENE_WEBGPU_CROWD_HIZ_DOWNSAMPLE_WGSL = [
  "struct HiZLevel { srcOffset: u32, srcWidth: u32, srcHeight: u32, dstOffset: u32, dstWidth: u32, dstHeight: u32, pad0: u32, pad1: u32, };",
  "@group(0) @binding(0) var<uniform> hizLevel: HiZLevel;",
  "@group(0) @binding(1) var<storage, read_write> hizBuffer: array<f32>;",
  "@compute @workgroup_size(64)",
  "fn downsample(@builtin(global_invocation_id) gid: vec3u) {",
  "    let i = gid.x;",
  "    if (i >= hizLevel.dstWidth * hizLevel.dstHeight) { return; }",
  "    let dx = i % hizLevel.dstWidth;",
  "    let dy = i / hizLevel.dstWidth;",
  "    let sx0 = dx * 2u;",
  "    let sy0 = dy * 2u;",
  "    let sx1 = min(sx0 + 1u, hizLevel.srcWidth - 1u);",
  "    let sy1 = min(sy0 + 1u, hizLevel.srcHeight - 1u);",
  "    let a = hizBuffer[hizLevel.srcOffset + sy0 * hizLevel.srcWidth + sx0];",
  "    let b = hizBuffer[hizLevel.srcOffset + sy0 * hizLevel.srcWidth + sx1];",
  "    let c = hizBuffer[hizLevel.srcOffset + sy1 * hizLevel.srcWidth + sx0];",
  "    let d = hizBuffer[hizLevel.srcOffset + sy1 * hizLevel.srcWidth + sx1];",
  "    hizBuffer[hizLevel.dstOffset + i] = max(max(a, b), max(c, d));",
  "}",
].join("\n");

function sceneWebGPUCrowdSeedWGSL(multisampled: boolean): string {
  return [
    "struct HiZSeed { dstWidth: u32, dstHeight: u32, srcWidth: u32, srcHeight: u32, };",
    "@group(0) @binding(0) var<uniform> hizSeed: HiZSeed;",
    "@group(0) @binding(1) var hizDepth: " + (multisampled ? "texture_depth_multisampled_2d" : "texture_depth_2d") + ";",
    "@group(0) @binding(2) var<storage, read_write> hizBuffer: array<f32>;",
    multisampled
      ? "fn hizPixelMax(p: vec2u) -> f32 { var d = 0.0; let n = textureNumSamples(hizDepth); for (var s = 0u; s < n; s = s + 1u) { d = max(d, textureLoad(hizDepth, p, s)); } return d; }"
      : "fn hizPixelMax(p: vec2u) -> f32 { return textureLoad(hizDepth, p, 0); }",
    "@compute @workgroup_size(8, 8)",
    "fn seed(@builtin(global_invocation_id) gid: vec3u) {",
    "    if (gid.x >= hizSeed.dstWidth || gid.y >= hizSeed.dstHeight) { return; }",
    "    let sx0 = gid.x * 2u;",
    "    let sy0 = gid.y * 2u;",
    "    let sx1 = min(sx0 + 1u, hizSeed.srcWidth - 1u);",
    "    let sy1 = min(sy0 + 1u, hizSeed.srcHeight - 1u);",
    "    let a = hizPixelMax(vec2u(sx0, sy0));",
    "    let b = hizPixelMax(vec2u(sx1, sy0));",
    "    let c = hizPixelMax(vec2u(sx0, sy1));",
    "    let d = hizPixelMax(vec2u(sx1, sy1));",
    "    hizBuffer[gid.y * hizSeed.dstWidth + gid.x] = max(max(a, b), max(c, d));",
    "}",
  ].join("\n");
}

// Level 0 is the depth target reduced 2x2; each next level halves again.
function sceneWebGPUCrowdHiZLevels(width: number, height: number): any {
  const levels: any[] = [];
  let w = Math.max(1, Math.ceil(Math.max(1, width) / 2));
  let h = Math.max(1, Math.ceil(Math.max(1, height) / 2));
  let offset = 0;
  for (;;) {
    levels.push({ offset, width: w, height: h });
    offset += w * h;
    if ((w === 1 && h === 1) || levels.length === SCENE_WEBGPU_CROWD_MAX_LEVELS) break;
    w = Math.ceil(w / 2);
    h = Math.ceil(h / 2);
  }
  return { levels, total: offset };
}

// Six normalized planes (left, right, bottom, top, near, far) of a column-major
// view-projection with clip z in [0, 1]. A point is inside when dot(n, p) + d >= 0.
function sceneWebGPUCrowdPlanes(m: any, out: any, base: number): any {
  function put(p: number, x: number, y: number, z: number, w: number): void {
    const len = Math.sqrt(x * x + y * y + z * z);
    const inv = len > 0 ? 1 / len : 1;
    out[base + p * 4] = x * inv; out[base + p * 4 + 1] = y * inv; out[base + p * 4 + 2] = z * inv; out[base + p * 4 + 3] = w * inv;
  }
  put(0, m[3] + m[0], m[7] + m[4], m[11] + m[8], m[15] + m[12]);
  put(1, m[3] - m[0], m[7] - m[4], m[11] - m[8], m[15] - m[12]);
  put(2, m[3] + m[1], m[7] + m[5], m[11] + m[9], m[15] + m[13]);
  put(3, m[3] - m[1], m[7] - m[5], m[11] - m[9], m[15] - m[13]);
  put(4, m[2], m[6], m[10], m[14]);
  put(5, m[3] - m[2], m[7] - m[6], m[11] - m[10], m[15] - m[14]);
  return out;
}

// True when the box lies outside one of the six planes.
function sceneWebGPUCrowdBoundsOutside(b: any, planes: any): boolean {
  if (!b) return false;
  for (let i = 0; i < 6; i++) {
    const nx = planes[i * 4], ny = planes[i * 4 + 1], nz = planes[i * 4 + 2], d = planes[i * 4 + 3];
    if (nx * (nx >= 0 ? b.maxX : b.minX) + ny * (ny >= 0 ? b.maxY : b.minY) + nz * (nz >= 0 ? b.maxZ : b.minZ) + d < -0.0001) return true;
  }
  return false;
}

// Writes one CrowdView (448 bytes) at word index base of f32 and u32.
function sceneWebGPUCrowdPackView(f32: any, u32: any, base: number, view: any): void {
  for (let k = 0; k < 16; k++) f32[base + k] = view.viewProj[k];
  sceneWebGPUCrowdPlanes(view.viewProj, f32, base + 16);
  f32[base + 40] = view.width > 0 ? view.width : 1;
  f32[base + 41] = view.height > 0 ? view.height : 1;
  f32[base + 42] = 0;
  f32[base + 43] = 0;
  const levels = view.levels || null;
  u32[base + 44] = view.slot >>> 0;
  u32[base + 45] = view.phase >>> 0;
  u32[base + 46] = levels ? levels.length : 0;
  u32[base + 47] = 0;
  for (let l = 0; l < SCENE_WEBGPU_CROWD_MAX_LEVELS; l++) {
    const level = levels && l < levels.length ? levels[l] : null;
    u32[base + 48 + l * 4] = level ? level.offset : 0;
    u32[base + 49 + l * 4] = level ? level.width : 0;
    u32[base + 50 + l * 4] = level ? level.height : 0;
    u32[base + 51 + l * 4] = 0;
  }
}

// Which batches the CPU may write into dirty runs: at most 32 writeBuffer
// calls, then one covering range (the WebGL path caps its streams the same way).
function sceneWebGPUCrowdDirtyRuns(flags: any, count: number): number[] {
  const runs: number[] = [];
  for (let i = 0; i < count; i++) {
    if (!flags[i]) continue;
    const start = i;
    while (i + 1 < count && flags[i + 1]) i++;
    runs.push(start, i + 1);
  }
  if (runs.length > 64) {
    const end = runs[runs.length - 1];
    runs.length = 0;
    runs.push(0, end);
  }
  return runs;
}

// An agent that left its batch (a different appearance, or selected) stays a
// plain mesh record carrying _crowdSkin. The WebGL renderer draws it with the
// crowd shader on its own; here it becomes a batch of one, so it skins the same
// way. The record inherits everything but the crowd fields from the agent's.
function sceneWebGPUCrowdAgentRecord(object: any, serial: number): any {
  const skin = object._crowdSkin, motion = object._crowdMotion || null;
  const record = Object.create(object);
  const matrix = object.modelMatrix instanceof Float32Array ? object.modelMatrix : new Float32Array(object.modelMatrix || 16);
  record._crowdBatch = true;
  record._crowdAgent = true;
  record.instanceCount = 1;
  record.crowdMembers = [object];
  record.crowdMotion = motion !== null;
  record.crowdAtlas = skin.atlas;
  record.crowdClipTable = motion ? motion.clipTable : null;
  record.crowdTransforms = matrix;
  record.crowdPoses = skin.rows;
  record.crowdRevision = serial;
  record.crowdShape = 0;
  record.crowdDirtyRanges = null;
  record.crowdDirtyBase = -1;
  record.crowdParentMatrix = null;
  return record;
}

// hooks: bindAttribute(pass, slot, record, key, components) and
// bindIndex(pass, record) bind the retained geometry of a batch record;
// materialGroup(record, materials) returns its material bind group.
function createSceneWebGPUCrowd(device: any, hooks: any): any {
  let disposed = false;
  const atlases = new Map();
  let paletteBytes = 0;
  const batches = new Map();
  let frameBatches: any[] = [];
  let layouts: any = null;
  let pipelines: any = {};
  const shaderModules: any = {};
  let target: any = { format: "", sampleCount: 1, specular: false };
  let config: any = null;
  let argsBuffer: any = null;
  let argsCapacity = 0;
  let hzb: any = null;
  let hzbEpoch = 0;
  let hzbCapacityFloats = 0;
  let hzbLevelsBuffer: any = null;
  let hzbSeedBuffer: any = null;
  let hiz: any = { width: 0, height: 0, levels: [], total: 0 };
  let seedGroup: any = null;
  let seedGroupView: any = null;
  let levelGroups: any[] = [];
  let viewsBuffer: any = null;
  const viewStaging = new ArrayBuffer(4 * SCENE_WEBGPU_CROWD_VIEW_STRIDE);
  const viewF32 = new Float32Array(viewStaging);
  const viewU32 = new Uint32Array(viewStaging);
  const paramsStaging = new ArrayBuffer(SCENE_WEBGPU_CROWD_PARAMS_BYTES);
  const paramsF32 = new Float32Array(paramsStaging);
  const paramsU32 = new Uint32Array(paramsStaging);
  const argsTemplate = new Uint32Array(SCENE_WEBGPU_CROWD_ARGS_BYTES / 4);
  let culling = false;
  let occlusion = { on: false, last: false, prepared: false, split: false, resolveTargets: [] as any[], timestamps: null as any };
  let lightDispatched = [0, 0];
  let serial = 0;
  let failed = "";
  let readback: any = { buffer: null, busy: false, pending: false, bytes: 0, batches: 0, early: 0, late: 0, shadow: 0 };
  let counters = { dispatches: 0, uploads: 0, uploadedBytes: 0, draws: 0, shadowDraws: 0, cpuCulled: 0 };

  // Batches the current bundle no longer carries give their buffers back.
  function dropStale(): void {
    batches.forEach(function(state: any, id: string) {
      if (state.serial === serial) return;
      release(state.params); release(state.agents); release(state.poses); release(state.identity); release(state.visible); release(state.visibility);
      batches.delete(id);
    });
  }

  function capability(): void {
    const limits = device.limits || {};
    if (typeof limits.maxStorageBuffersInVertexStage === "number" && limits.maxStorageBuffersInVertexStage < 2) {
      throw new Error("crowd needs vertex-stage storage buffers");
    }
  }

  function ensureLayouts(): any {
    if (layouts) return layouts;
    const V = GPUShaderStage.VERTEX, C = GPUShaderStage.COMPUTE;
    function buf(binding: number, visibility: number, type: string, dynamic?: boolean): any {
      const buffer: any = { type };
      if (dynamic) buffer.hasDynamicOffset = true;
      return { binding, visibility, buffer };
    }
    function seedLayout(label: string, multisampled: boolean): any {
      return device.createBindGroupLayout({ label, entries: [
        buf(0, C, "uniform"),
        { binding: 1, visibility: C, texture: { sampleType: "depth", viewDimension: "2d", multisampled } },
        buf(2, C, "storage"),
      ] });
    }
    layouts = {
      frame: wgpuCreateFrameBindGroupLayout(device),
      material: wgpuCreateMaterialBindGroupLayout(device, true),
      shadow: wgpuCreateShadowBindGroupLayout(device),
      crowd: device.createBindGroupLayout({ label: "gosx-crowd", entries: [
        buf(0, V | C, "uniform"),
        { binding: 1, visibility: V, texture: { sampleType: "unfilterable-float", viewDimension: "2d" } },
        buf(2, V | C, "read-only-storage"),
        buf(3, V, "read-only-storage"),
      ] }),
      cull: device.createBindGroupLayout({ label: "gosx-crowd-cull", entries: [
        buf(0, C, "uniform", true), buf(1, C, "uniform"), buf(2, C, "read-only-storage"),
        buf(3, C, "storage"), buf(4, C, "storage"), buf(5, C, "storage"), buf(6, C, "read-only-storage"),
      ] }),
      downsample: device.createBindGroupLayout({ label: "gosx-crowd-hiz-downsample", entries: [buf(0, C, "uniform"), buf(1, C, "storage")] }),
      seed: seedLayout("gosx-crowd-hiz-seed", false),
      seedMS: seedLayout("gosx-crowd-hiz-seed-ms", true),
    };
    return layouts;
  }

  function module(name: string, code: string): any {
    return shaderModules[name] || (shaderModules[name] = device.createShaderModule({ label: "gosx-crowd-" + name, code }));
  }

  function slotLayouts(): any[] {
    return [
      { arrayStride: 4, stepMode: "instance", attributes: [{ format: "uint32", offset: 0, shaderLocation: 4 }] },
      { arrayStride: 16, stepMode: "vertex", attributes: [{ format: "float32x4", offset: 0, shaderLocation: 5 }] },
      { arrayStride: 16, stepMode: "vertex", attributes: [{ format: "float32x4", offset: 0, shaderLocation: 6 }] },
    ];
  }

  function targetKey(): string {
    return target.format + "|" + target.sampleCount + "|" + (target.specular ? 1 : 0);
  }

  function colorPipeline(motion: boolean): any {
    const key = "color|" + (motion ? "m" : "p") + "|" + targetKey();
    if (pipelines[key]) return pipelines[key];
    const l = ensureLayouts();
    return pipelines[key] = wgpuCreateValidatedPipeline(device, "render", {
      label: "gosx-crowd-color-" + (motion ? "motion" : "pose"),
      layout: device.createPipelineLayout({ bindGroupLayouts: [l.frame, l.material, l.crowd] }),
      vertex: {
        module: module("color-" + (motion ? "motion" : "pose"), sceneWebGPUCrowdColorVertexWGSL(motion)),
        entryPoint: "vertexMain", buffers: WGPU_PBR_VERTEX_LAYOUT.concat(slotLayouts()),
      },
      fragment: sceneWebGPUColorFragment(module("color-fragment", WGSL_PBR_FRAGMENT), sceneWebGPUColorTarget(target.format, target.specular), wgpuBlendState("opaque")),
      primitive: { topology: "triangle-list", cullMode: "none" },
      multisample: { count: Math.max(1, target.sampleCount) },
      depthStencil: { format: "depth24plus", depthWriteEnabled: true, depthCompare: "less-equal" },
    });
  }

  function shadowPipeline(motion: boolean): any {
    const key = "shadow|" + (motion ? "m" : "p");
    if (pipelines[key]) return pipelines[key];
    const l = ensureLayouts();
    const slots = slotLayouts();
    // Shadow vertex buffers: 0 position, 1 survivor slot, 2 joints, 3 weights.
    return pipelines[key] = wgpuCreateValidatedPipeline(device, "render", {
      label: "gosx-crowd-shadow-" + (motion ? "motion" : "pose"),
      layout: device.createPipelineLayout({ bindGroupLayouts: [l.shadow, l.crowd] }),
      vertex: {
        module: module("shadow-" + (motion ? "motion" : "pose"), sceneWebGPUCrowdShadowVertexWGSL(motion)),
        entryPoint: "vertexMain", buffers: [WGPU_SHADOW_VERTEX_LAYOUT[0]].concat(slots),
      },
      fragment: { module: module("shadow-fragment", WGSL_SHADOW_FRAGMENT), entryPoint: "fragmentMain", targets: [] },
      primitive: { topology: "triangle-list", cullMode: "none", frontFace: "ccw" },
      depthStencil: { format: "depth24plus", depthWriteEnabled: true, depthCompare: "less-equal" },
    });
  }

  function computePipeline(name: string, code: string, entry: string, layout: any): any {
    if (pipelines[name]) return pipelines[name];
    return pipelines[name] = wgpuCreateValidatedPipeline(device, "compute", {
      label: "gosx-crowd-" + name,
      layout: device.createPipelineLayout({ bindGroupLayouts: [layout] }),
      compute: { module: module(name, code), entryPoint: entry },
    });
  }

  // A pipeline handle is ready once validation settled; a rejected one stays
  // not ready, and the guard has already reported it.
  function ready(handle: any): boolean {
    const state = handle && wgpuPipelineValidation.get(handle);
    return !state || state.ready === true;
  }

  function buffer(label: string, size: number, usage: number): any {
    return device.createBuffer({ label: "gosx.crowd." + label, size: Math.max(16, Math.ceil(size / 4) * 4), usage });
  }

  function release(resource: any): void {
    if (resource && typeof resource.destroy === "function") resource.destroy();
  }

  // The joint-pose atlas, one rgba32float texel per matrix column. The mount
  // calls this at hydration (like the WebGL renderer) so a bad atlas fails
  // before any agent takes the crowd path.
  function prepareAtlas(atlas: any): any {
    let record = atlases.get(atlas);
    if (record) return record;
    capability();
    if (!atlas || !atlas.data || !(atlas.width > 0) || !(atlas.height > 0)) throw new Error("crowd palette capability unavailable: empty atlas");
    const limit = (device.limits && device.limits.maxTextureDimension2D) || 8192;
    if (atlas.width > limit || atlas.height > limit) throw new Error("crowd palette capability unavailable: texture dimensions");
    if (paletteBytes + atlas.data.byteLength > SCENE_WEBGPU_CROWD_PALETTE_BUDGET) throw new Error("crowd palette residency budget exceeded");
    const texture = device.createTexture({ label: "gosx.crowd.atlas", size: [atlas.width, atlas.height, 1], format: "rgba32float",
      usage: GPUTextureUsage.TEXTURE_BINDING | GPUTextureUsage.COPY_DST });
    if (!texture) throw new Error("crowd palette texture allocation failed");
    device.queue.writeTexture({ texture }, atlas.data, { bytesPerRow: atlas.width * 16, rowsPerImage: atlas.height }, [atlas.width, atlas.height, 1]);
    record = { texture, view: texture.createView(), bytes: atlas.data.byteLength };
    atlases.set(atlas, record);
    paletteBytes += record.bytes;
    return record;
  }

  function prepareMotionShaders(): boolean {
    capability();
    return true;
  }

  function ensureBatchBuffers(state: any, record: any): void {
    const motion = record.crowdMotion === true;
    const stride = motion ? SCENE_WEBGPU_CROWD_RECORD_FLOATS : 16;
    const count = record.instanceCount;
    let grew = false;
    if (!state.params) {
      state.params = buffer("params", SCENE_WEBGPU_CROWD_PARAMS_BYTES, GPUBufferUsage.UNIFORM | GPUBufferUsage.COPY_DST);
      grew = true;
    }
    if (!state.agents || state.capacity < count || state.motion !== motion) {
      release(state.agents); release(state.poses); release(state.identity); release(state.visible); release(state.visibility);
      state.capacity = Math.max(8, Math.ceil(count * 1.25));
      state.motion = motion;
      state.agents = buffer("agents", state.capacity * stride * 4, GPUBufferUsage.STORAGE | GPUBufferUsage.COPY_DST);
      state.poses = buffer("poses", motion ? 16 : state.capacity * 12, GPUBufferUsage.STORAGE | GPUBufferUsage.COPY_DST);
      const identity = new Uint32Array(state.capacity);
      for (let i = 0; i < identity.length; i++) identity[i] = i;
      state.identity = buffer("identity", identity.byteLength, GPUBufferUsage.VERTEX | GPUBufferUsage.COPY_DST);
      device.queue.writeBuffer(state.identity, 0, identity);
      state.visible = buffer("visible", state.capacity * 16, GPUBufferUsage.STORAGE | GPUBufferUsage.VERTEX);
      state.visibility = buffer("visibility", state.capacity * 4, GPUBufferUsage.STORAGE | GPUBufferUsage.COPY_DST);
      state.uploadedRevision = -1;
      state.visibilityReset = true;
      grew = true;
    }
    if (grew) { state.group = null; state.cullGroup = null; }
  }

  // Pose mode uploads the record's own streams after a revision change. Motion
  // mode packs the members' motion records and uploads only the dirty runs.
  function syncAgents(state: any, record: any): void {
    const count = record.instanceCount;
    if (state.uploadedRevision === record.crowdRevision && state.count === count) return;
    const ranges = record.crowdDirtyRanges && state.uploadedRevision === record.crowdDirtyBase ? record.crowdDirtyRanges : null;
    const reshaped = state.count !== count || state.shape !== record.crowdShape;
    if (state.motion) {
      const stride = SCENE_WEBGPU_CROWD_RECORD_FLOATS;
      const needed = count * stride;
      let resync = state.uploadedRevision < 0 || reshaped;
      if (!state.packed || state.packed.length < needed || state.packed.length > Math.max(stride * 8, needed * 4)) {
        let capacity = stride * 8;
        while (capacity < needed) capacity *= 2;
        state.packed = new Float32Array(capacity);
        state.flags = new Uint8Array(capacity / stride);
        resync = true;
      }
      const members = record.crowdMembers;
      if (resync) state.flags.fill(1, 0, count);
      const spans = !resync && ranges ? ranges.length : 2;
      for (let r = 0; r < spans; r += 2) {
        const from = !resync && ranges ? Math.max(0, ranges[r]) : 0;
        const to = !resync && ranges ? Math.min(count, ranges[r + 1]) : count;
        for (let i = from; i < to; i++) {
          const motion = members[i]._crowdMotion;
          if (resync || motion.dirty) {
            state.packed.set(motion.record, i * stride);
            state.flags[i] = 1;
            motion.dirty = false;
          }
        }
      }
      const runs = sceneWebGPUCrowdDirtyRuns(state.flags, count);
      for (let r = 0; r < runs.length; r += 2) {
        const bytes = (runs[r + 1] - runs[r]) * stride * 4;
        device.queue.writeBuffer(state.agents, runs[r] * stride * 4, state.packed, runs[r] * stride, (runs[r + 1] - runs[r]) * stride);
        counters.uploads += 1;
        counters.uploadedBytes += bytes;
      }
      state.flags.fill(0, 0, count);
    } else {
      device.queue.writeBuffer(state.agents, 0, record.crowdTransforms, 0, count * 16);
      device.queue.writeBuffer(state.poses, 0, record.crowdPoses, 0, count * 3);
      counters.uploads += 2;
      counters.uploadedBytes += count * 19 * 4;
    }
    state.uploadedRevision = record.crowdRevision;
    state.count = count;
    state.shape = record.crowdShape;
    if (reshaped) state.visibilityReset = true;
  }

  function writeParams(state: any, record: any, frame: any, atlas: any): void {
    const f = paramsF32, u = paramsU32;
    const parent = record.crowdParentMatrix;
    for (let k = 0; k < 16; k++) f[k] = parent ? parent[k] : (k % 5 === 0 ? 1 : 0);
    f[16] = frame.now; f[17] = SCENE_WEBGPU_CROWD_EXTRAPOLATION_SECONDS; f[18] = 0; f[19] = 0;
    const b = state.localBounds;
    f[20] = b.cx; f[21] = b.cy; f[22] = b.cz; f[23] = b.r;
    u[24] = record.instanceCount; u[25] = state.motion ? 1 : 0; u[26] = record.castShadow ? 1 : 0; u[27] = state.argsIndex * (SCENE_WEBGPU_CROWD_ARGS_BYTES / 4);
    const table = record.crowdClipTable;
    for (let k = 0; k < SCENE_WEBGPU_CROWD_MAX_CLIPS * 4; k++) f[28 + k] = table && table.data && k < table.data.length ? table.data[k] : 0;
    webGPUCutawayCrowdParams(f, 28 + SCENE_WEBGPU_CROWD_MAX_CLIPS * 4, record.crowdCut);
    device.queue.writeBuffer(state.params, 0, paramsStaging, 0, SCENE_WEBGPU_CROWD_PARAMS_BYTES);
  }

  // A sphere around the agents' local bounds. The atlas bounds already cover
  // every pose the palette holds, so no padding beyond a rounding margin.
  function localBounds(record: any): any {
    const first = record.crowdMembers && record.crowdMembers[0];
    const b = first && first._crowdSkin && first._crowdSkin.bounds;
    if (!b) return { cx: 0, cy: 0, cz: 0, r: 1e6 };
    const cx = (b.minX + b.maxX) / 2, cy = (b.minY + b.maxY) / 2, cz = (b.minZ + b.maxZ) / 2;
    const ex = (b.maxX - b.minX) / 2, ey = (b.maxY - b.minY) / 2, ez = (b.maxZ - b.minZ) / 2;
    return { cx, cy, cz, r: Math.sqrt(ex * ex + ey * ey + ez * ez) };
  }

  function ensureBindGroups(state: any, atlas: any): void {
    const l = ensureLayouts();
    if (!state.group || state.atlasView !== atlas.view) {
      state.group = device.createBindGroup({ label: "gosx-crowd", layout: l.crowd, entries: [
        { binding: 0, resource: { buffer: state.params, size: SCENE_WEBGPU_CROWD_PARAMS_BYTES } },
        { binding: 1, resource: atlas.view },
        { binding: 2, resource: { buffer: state.agents } },
        { binding: 3, resource: { buffer: state.poses } },
      ] });
      state.atlasView = atlas.view;
    }
  }

  function ensureArgs(count: number): boolean {
    if (count * SCENE_WEBGPU_CROWD_ARGS_BYTES <= argsCapacity) return false;
    release(argsBuffer);
    argsCapacity = Math.max(4, Math.ceil(count * 1.5)) * SCENE_WEBGPU_CROWD_ARGS_BYTES;
    argsBuffer = buffer("args", argsCapacity, GPUBufferUsage.STORAGE | GPUBufferUsage.INDIRECT | GPUBufferUsage.COPY_DST | GPUBufferUsage.COPY_SRC);
    release(readback.buffer);
    readback.buffer = null;
    return true;
  }

  function ensureCullGroup(state: any): void {
    if (state.cullGroup && state.cullHzb === hzbEpoch && state.cullArgs === argsBuffer) return;
    const l = ensureLayouts();
    state.cullGroup = device.createBindGroup({ label: "gosx-crowd-cull", layout: l.cull, entries: [
      { binding: 0, resource: { buffer: viewsBuffer, size: SCENE_WEBGPU_CROWD_VIEW_BYTES } },
      { binding: 1, resource: { buffer: state.params, size: SCENE_WEBGPU_CROWD_PARAMS_BYTES } },
      { binding: 2, resource: { buffer: state.agents } },
      { binding: 3, resource: { buffer: argsBuffer } },
      { binding: 4, resource: { buffer: state.visible } },
      { binding: 5, resource: { buffer: state.visibility } },
      { binding: 6, resource: { buffer: hzb } },
    ] });
    state.cullHzb = hzbEpoch;
    state.cullArgs = argsBuffer;
  }

  function dispatchCull(encoder: any, viewIndex: number): void {
    const pass = encoder.beginComputePass({ label: "gosx-crowd-cull" });
    pass.setPipeline(wgpuRequirePipeline(pipelines.cull));
    for (let i = 0; i < frameBatches.length; i++) {
      const state = frameBatches[i];
      // A batch outside the camera frustum still casts shadows into view.
      if (state.agent || (viewIndex < 2 ? state.hidden : !state.castShadow)) continue;
      ensureCullGroup(state);
      pass.setBindGroup(0, state.cullGroup, [viewIndex * SCENE_WEBGPU_CROWD_VIEW_STRIDE]);
      pass.dispatchWorkgroups(Math.ceil(state.count / 64));
    }
    pass.end();
    counters.dispatches += 1;
  }

  function writeView(viewIndex: number, view: any): void {
    sceneWebGPUCrowdPackView(viewF32, viewU32, viewIndex * (SCENE_WEBGPU_CROWD_VIEW_STRIDE / 4), view);
    device.queue.writeBuffer(viewsBuffer, viewIndex * SCENE_WEBGPU_CROWD_VIEW_STRIDE, viewStaging, viewIndex * SCENE_WEBGPU_CROWD_VIEW_STRIDE, SCENE_WEBGPU_CROWD_VIEW_BYTES);
  }

  function occlusionReady(): boolean {
    if (!config.occlusion || failed) return false;
    const l = ensureLayouts();
    computePipeline("downsample", SCENE_WEBGPU_CROWD_HIZ_DOWNSAMPLE_WGSL, "downsample", l.downsample);
    const ms = target.sampleCount > 1;
    computePipeline(ms ? "seed-ms" : "seed", sceneWebGPUCrowdSeedWGSL(ms), "seed", ms ? l.seedMS : l.seed);
    return ready(pipelines.downsample) && ready(pipelines[ms ? "seed-ms" : "seed"]);
  }

  function ensureHiZ(): void {
    if (hiz.width === target.width && hiz.height === target.height && hzb) return;
    const layout = sceneWebGPUCrowdHiZLevels(target.width, target.height);
    hiz = { width: target.width, height: target.height, levels: layout.levels, total: layout.total };
    if (!hzb || layout.total > hzbCapacityFloats) {
      release(hzb);
      hzbCapacityFloats = layout.total;
      hzb = buffer("hzb", Math.max(16, layout.total * 4), GPUBufferUsage.STORAGE);
      hzbEpoch += 1;
      seedGroup = null;
    }
    if (!hzbLevelsBuffer) hzbLevelsBuffer = buffer("hiz-levels", SCENE_WEBGPU_CROWD_MAX_LEVELS * 256, GPUBufferUsage.UNIFORM | GPUBufferUsage.COPY_DST);
    if (!hzbSeedBuffer) hzbSeedBuffer = buffer("hiz-seed", 16, GPUBufferUsage.UNIFORM | GPUBufferUsage.COPY_DST);
    const table = new Uint32Array(SCENE_WEBGPU_CROWD_MAX_LEVELS * 64);
    for (let k = 1; k < hiz.levels.length; k++) {
      const src = hiz.levels[k - 1], dst = hiz.levels[k];
      table.set([src.offset, src.width, src.height, dst.offset, dst.width, dst.height, 0, 0], k * 64);
    }
    device.queue.writeBuffer(hzbLevelsBuffer, 0, table);
    device.queue.writeBuffer(hzbSeedBuffer, 0, new Uint32Array([hiz.levels[0].width, hiz.levels[0].height, target.width, target.height]));
    levelGroups = [];
    seedGroup = null;
  }

  function buildHiZ(encoder: any, depthView: any): void {
    const l = ensureLayouts();
    const ms = target.sampleCount > 1;
    if (!seedGroup || seedGroupView !== depthView) {
      seedGroup = device.createBindGroup({ label: "gosx-crowd-hiz-seed", layout: ms ? l.seedMS : l.seed, entries: [
        { binding: 0, resource: { buffer: hzbSeedBuffer } }, { binding: 1, resource: depthView }, { binding: 2, resource: { buffer: hzb } },
      ] });
      seedGroupView = depthView;
    }
    const pass = encoder.beginComputePass({ label: "gosx-crowd-hiz" });
    pass.setPipeline(wgpuRequirePipeline(pipelines[ms ? "seed-ms" : "seed"]));
    pass.setBindGroup(0, seedGroup);
    pass.dispatchWorkgroups(Math.ceil(hiz.levels[0].width / 8), Math.ceil(hiz.levels[0].height / 8));
    pass.setPipeline(wgpuRequirePipeline(pipelines.downsample));
    for (let k = 1; k < hiz.levels.length; k++) {
      if (!levelGroups[k]) {
        levelGroups[k] = device.createBindGroup({ label: "gosx-crowd-hiz-level-" + k, layout: l.downsample, entries: [
          { binding: 0, resource: { buffer: hzbLevelsBuffer, offset: k * 256, size: 32 } }, { binding: 1, resource: { buffer: hzb } },
        ] });
      }
      pass.setBindGroup(0, levelGroups[k]);
      pass.dispatchWorkgroups(Math.ceil(hiz.levels[k].width * hiz.levels[k].height / 64));
    }
    pass.end();
    counters.dispatches += hiz.levels.length;
  }

  function resetVisibility(): void {
    for (let i = 0; i < frameBatches.length; i++) {
      const state = frameBatches[i];
      device.queue.writeBuffer(state.visibility, 0, new Uint32Array(state.count).fill(1));
    }
  }

  // The crowd batch records of a bundle, and every agent that left its batch as
  // a batch of one.
  function gatherRecords(bundle: any): any[] {
    const records: any[] = [];
    const objects = bundle && bundle.meshObjects;
    if (!Array.isArray(objects)) return records;
    for (let i = 0; i < objects.length; i++) {
      const o = objects[i];
      if (!o || o.viewCulled) continue;
      if (o._crowdBatch === true) {
        if (o.instanceCount > 0 && o.crowdAtlas) records.push(o);
      } else if (o._crowdSkin && o._crowdSkin.atlas && !o.skin && o.directVertices) records.push(sceneWebGPUCrowdAgentRecord(o, serial));
    }
    return records;
  }

  // Brings one batch's GPU state up to date with its record.
  function syncBatch(record: any, index: number, frame: any, planes: any): any {
    let state = batches.get(record.id);
    if (!state) {
      state = { id: record.id, params: null, agents: null, poses: null, identity: null, visible: null, visibility: null, capacity: 0,
        motion: false, count: 0, shape: -1, uploadedRevision: -1, packed: null, flags: null, group: null, cullGroup: null };
      batches.set(record.id, state);
    }
    const atlas = prepareAtlas(record.crowdAtlas);
    state.serial = serial;
    state.record = record;
    state.argsIndex = index;
    state.castShadow = record.castShadow === true;
    state.listed = false;
    state.shadowListed = false;
    state.localBounds = localBounds(record);
    ensureBatchBuffers(state, record);
    syncAgents(state, record);
    writeParams(state, record, frame, atlas);
    ensureBindGroups(state, atlas);
    state.agent = record._crowdAgent === true;
    state.hidden = !state.agent && sceneWebGPUCrowdBoundsOutside(record.bounds, planes);
    if (state.hidden) counters.cpuCulled += 1;
    return state;
  }

  // The indirect draw count of a record: indices when it has them, else vertices.
  function drawCount(record: any): number {
    const indices = record.vertices && record.vertices.indices;
    return indices instanceof Uint32Array && indices.length >= 3 ? indices.length : record.vertexCount;
  }

  // Resets the indirect args, switches Hi-Z on when it can run and culls the
  // camera view against the frustum (early phase). Until the kernels compile,
  // the batches draw unculled.
  function startCulling(encoder: any, frame: any, grewArgs: boolean): void {
    if (!viewsBuffer) viewsBuffer = buffer("views", 4 * SCENE_WEBGPU_CROWD_VIEW_STRIDE, GPUBufferUsage.UNIFORM | GPUBufferUsage.COPY_DST);
    // The cull bind group always needs binding 6, even with occlusion off.
    if (!hzb) { hzb = buffer("hzb", 16, GPUBufferUsage.STORAGE); hzbEpoch += 1; }
    computePipeline("cull", SCENE_WEBGPU_CROWD_CULL_WGSL, "cull", ensureLayouts().cull);
    if (!ready(pipelines.cull)) return;
    occlusion.on = occlusionReady();
    if (occlusion.on) ensureHiZ();
    // A new layout, or Hi-Z switching on, restarts last-frame visibility at
    // "all visible" so no agent is lost while the history is empty.
    if (frameBatches.some(function(state: any) { return state.visibilityReset; }) || (occlusion.on && !occlusion.last) || grewArgs) resetVisibility();
    occlusion.last = occlusion.on;
    for (let i = 0; i < frameBatches.length; i++) {
      const state = frameBatches[i];
      state.visibilityReset = false;
      state.listed = !state.agent;
      state.shadowListed = config.shadowLists && !state.agent;
      for (let s = 0; s < 4; s++) {
        argsTemplate.fill(0, s * 8, s * 8 + 8);
        argsTemplate[s * 8] = drawCount(state.record);
      }
      device.queue.writeBuffer(argsBuffer, state.argsIndex * SCENE_WEBGPU_CROWD_ARGS_BYTES, argsTemplate);
    }
    const view: any = { viewProj: frame.viewProjection, width: target.width, height: target.height, slot: 0, phase: 0, levels: occlusion.on ? hiz.levels : null };
    writeView(0, view);
    if (occlusion.on) { view.slot = 1; view.phase = 1; writeView(1, view); }
    culling = true;
    dispatchCull(encoder, 0);
  }

  // bind: which list of survivors a draw reads. Slot 0 is the early camera
  // list, 1 the late camera list, 2 and 3 the two shadow lights.
  function drawBatch(pass: any, state: any, slot: number, shadow: boolean): boolean {
    const record = state.record;
    const motion = state.motion;
    const indexed = record.vertices && record.vertices.indices instanceof Uint32Array && record.vertices.indices.length >= 3;
    const pipeline = wgpuRequirePipeline(shadow ? shadowPipeline(motion) : colorPipeline(motion));
    pass.setPipeline(pipeline);
    pass.setBindGroup(shadow ? 1 : 2, state.group);
    const attributes = shadow ? [["positions", 3, 0]] : [["positions", 3, 0], ["normals", 3, 1], ["uvs", 2, 2], ["tangents", 4, 3]];
    for (let a = 0; a < attributes.length; a++) {
      if (!hooks.bindAttribute(pass, attributes[a][2], record, attributes[a][0], attributes[a][1])) return false;
    }
    const base = shadow ? 1 : 4;
    if (!hooks.bindAttribute(pass, base + 1, record, "joints", 4) || !hooks.bindAttribute(pass, base + 2, record, "weights", 4)) return false;
    const viaList = slot < 2 ? state.listed : state.shadowListed;
    if (viaList) pass.setVertexBuffer(base, state.visible, slot * state.count * 4, state.count * 4);
    else pass.setVertexBuffer(base, state.identity, 0, state.count * 4);
    const indexCount = indexed ? hooks.bindIndex(pass, record) : 0;
    if (viaList) {
      const offset = state.argsIndex * SCENE_WEBGPU_CROWD_ARGS_BYTES + slot * 32;
      if (indexCount > 0) pass.drawIndexedIndirect(argsBuffer, offset);
      else pass.drawIndirect(argsBuffer, offset);
    } else if (indexCount > 0) pass.drawIndexed(indexCount, state.count);
    else pass.draw(record.vertexCount, state.count);
    return true;
  }

  function drawColor(pass: any, frameBindGroup: any, materials: any, slot: number): number {
    if (!frameBatches.length) return 0;
    pass.setBindGroup(0, frameBindGroup);
    let drawn = 0;
    for (let i = 0; i < frameBatches.length; i++) {
      const state = frameBatches[i];
      if (state.hidden) continue;
      pass.setBindGroup(1, hooks.materialGroup(state.record, materials), [0]);
      if (drawBatch(pass, state, slot, false)) { drawn += 1; counters.draws += 1; }
    }
    return drawn;
  }

  return {
    prepareCrowdAtlas: prepareAtlas,
    prepareCrowdMotionShaders: prepareMotionShaders,

    // present reports whether a bundle has crowd batches to draw.
    present: function(bundle: any): boolean {
      const objects = bundle && bundle.meshObjects;
      if (!Array.isArray(objects)) return false;
      for (let i = 0; i < objects.length; i++) if (objects[i] && (objects[i]._crowdBatch === true || objects[i]._crowdSkin)) return true;
      return false;
    },

    // beginFrame syncs every batch of the bundle to GPU buffers and, when the
    // scene opts in to GPU-driven culling, culls the camera view (early phase).
    // frame: { now, viewProjection, width, height, sampleCount, targetFormat,
    // specular }.
    beginFrame: function(bundle: any, encoder: any, frame: any): boolean {
      serial += 1;
      counters.dispatches = counters.uploads = counters.uploadedBytes = counters.draws = counters.shadowDraws = counters.cpuCulled = 0;
      occlusion.prepared = false; occlusion.split = false; occlusion.on = false;
      occlusion.resolveTargets = []; occlusion.timestamps = null;
      frameBatches = [];
      culling = false;
      lightDispatched = [0, 0];
      if (disposed) return false;
      const records = gatherRecords(bundle);
      if (!records.length) { dropStale(); return false; }
      capability();
      target = { format: String(frame.targetFormat || ""), sampleCount: Math.max(1, Math.floor(frame.sampleCount || 1)),
        specular: !!frame.specular, width: Math.max(1, Math.floor(frame.width || 1)), height: Math.max(1, Math.floor(frame.height || 1)) };
      config = bundle.gpuDriven && typeof bundle.gpuDriven === "object"
        ? { occlusion: bundle.gpuDriven.occlusion === true, shadowLists: bundle.gpuDriven.shadowCulling !== false } : null;
      const planes = sceneWebGPUCrowdPlanes(frame.viewProjection, new Float32Array(24), 0);
      const grewArgs = ensureArgs(records.length);
      for (let i = 0; i < records.length; i++) frameBatches.push(syncBatch(records[i], i, frame, planes));
      dropStale();
      if (!config || !encoder || typeof encoder.beginComputePass !== "function") return true;
      startCulling(encoder, frame, grewArgs);
      return true;
    },

    // lightView builds every batch's survivor list for one shadow light before its pass.
    lightView: function(encoder: any, lightMatrix: any, lightSlot: number): boolean {
      if (!config || !config.shadowLists || !culling || !lightMatrix) return false;
      const light = lightSlot === 1 ? 1 : 0;
      writeView(2 + light, { viewProj: lightMatrix, width: target.width, height: target.height, slot: 2 + light, phase: 2, levels: null });
      dispatchCull(encoder, 2 + light);
      lightDispatched[light] = serial;
      return true;
    },

    // drawColor draws the early (or only) camera list into the main pass.
    drawColor: function(pass: any, frameBindGroup: any, materials: any): number {
      return drawColor(pass, frameBindGroup, materials, 0);
    },

    // drawShadow draws every shadow-casting batch into one light's pass. The
    // caller has bound group 0 (the shadow uniform).
    drawShadow: function(pass: any, lightSlot: number): number {
      let drawn = 0;
      const light = lightSlot === 1 ? 1 : 0;
      for (let i = 0; i < frameBatches.length; i++) {
        const state = frameBatches[i];
        if (!state.castShadow) continue;
        state.shadowListed = Boolean(config && config.shadowLists) && culling && !state.agent && lightDispatched[light] === serial;
        if (drawBatch(pass, state, 2 + light, true)) { drawn += 1; counters.shadowDraws += 1; }
      }
      return drawn;
    },

    // True when this frame splits the main pass for two-phase occlusion.
    splitsMainPass: function(): boolean { return culling && occlusion.on; },

    // prepareMainPass moves the MSAA resolve target and the end timestamp off
    // the main pass descriptor. The late pass puts them back. Call it before
    // the GPU-driven instancing host's own prepareMainPass, so that host's
    // late pass (if any) hands the resolve to the last pass of the chain.
    prepareMainPass: function(descriptor: any): boolean {
      if (!culling || !occlusion.on || !descriptor) return false;
      occlusion.resolveTargets = (descriptor.colorAttachments || []).map(function(color: any) {
        const resolve = color && color.resolveTarget;
        if (color) delete color.resolveTarget;
        return resolve;
      });
      const stamps = descriptor.timestampWrites;
      if (stamps && stamps.endOfPassWriteIndex !== undefined) {
        occlusion.timestamps = { querySet: stamps.querySet, endOfPassWriteIndex: stamps.endOfPassWriteIndex };
        descriptor.timestampWrites = { querySet: stamps.querySet, beginningOfPassWriteIndex: stamps.beginningOfPassWriteIndex };
      }
      occlusion.prepared = true;
      return true;
    },

    // splitMainPass ends the early pass, builds the Hi-Z pyramid from its depth,
    // culls the camera view against it and opens the late pass, which loads
    // color and depth and draws the newly visible agents. Returns the pass the
    // renderer keeps drawing into.
    splitMainPass: function(encoder: any, pass: any, descriptor: any, frameBindGroup: any, materials: any): any {
      if (!occlusion.prepared || occlusion.split) return pass;
      occlusion.split = true;
      const depthView = descriptor.depthStencilAttachment.view;
      pass.end();
      buildHiZ(encoder, depthView);
      dispatchCull(encoder, 1);
      const colors = descriptor.colorAttachments.map(function(color: any, index: number) {
        return { ...color, loadOp: "load", storeOp: "store", resolveTarget: occlusion.resolveTargets[index] };
      });
      const late: any = { label: "gosx-crowd-late", colorAttachments: colors,
        depthStencilAttachment: { view: depthView, depthLoadOp: "load", depthStoreOp: "store" } };
      if (occlusion.timestamps) late.timestampWrites = occlusion.timestamps;
      const latePass = encoder.beginRenderPass(late);
      drawColor(latePass, frameBindGroup, materials, 1);
      return latePass;
    },

    // A later color capture (transmission) opens a third pass. Restore the MSAA
    // resolve targets prepareMainPass removed.
    resumeMainPass: function(descriptor: any): void {
      if (!occlusion.prepared) return;
      descriptor.colorAttachments.forEach(function(color: any, index: number) { color.resolveTarget = occlusion.resolveTargets[index]; });
    },

    // finishEncoding copies the indirect args to a readback buffer when no
    // earlier copy is still mapping (telemetry only).
    finishEncoding: function(encoder: any): boolean {
      if (!culling || readback.busy || typeof encoder.copyBufferToBuffer !== "function") return false;
      const bytes = frameBatches.length * SCENE_WEBGPU_CROWD_ARGS_BYTES;
      if (!readback.buffer) readback.buffer = device.createBuffer({ label: "gosx.crowd.readback", size: argsCapacity, usage: GPUBufferUsage.MAP_READ | GPUBufferUsage.COPY_DST });
      if (typeof readback.buffer.mapAsync !== "function") return false;
      encoder.copyBufferToBuffer(argsBuffer, 0, readback.buffer, 0, bytes);
      readback.pending = true; readback.bytes = bytes; readback.batches = frameBatches.length;
      return true;
    },

    // endFrame runs after submit: it maps the readback copy, if one was made.
    endFrame: function(): any {
      if (readback.pending && !readback.busy) {
        readback.pending = false;
        readback.busy = true;
        const copy = readback.buffer, bytes = readback.bytes, n = readback.batches;
        copy.mapAsync(typeof GPUMapMode !== "undefined" ? GPUMapMode.READ : 1).then(function() {
          const words = new Uint32Array(copy.getMappedRange(0, bytes).slice(0));
          copy.unmap();
          const sums = [0, 0, 0, 0];
          for (let b = 0; b < n; b++) for (let s = 0; s < 4; s++) sums[s] += words[b * 32 + s * 8 + 1];
          readback.early = sums[0]; readback.late = sums[1]; readback.shadow = sums[2] + sums[3];
          readback.busy = false;
        }).catch(function() { readback.busy = false; });
      }
      return this.stats();
    },

    stats: function(): any {
      if (!frameBatches.length) return SCENE_WEBGPU_CROWD_IDLE_STATS;
      let agents = 0;
      for (let i = 0; i < frameBatches.length; i++) agents += frameBatches[i].count;
      return {
        crowdBatches: frameBatches.length,
        crowdAgents: agents,
        crowdCulling: culling,
        crowdOcclusion: culling && occlusion.on,
        crowdBatchesCulled: counters.cpuCulled,
        crowdDispatches: counters.dispatches,
        crowdUploads: counters.uploads,
        crowdUploadedBytes: counters.uploadedBytes,
        crowdDraws: counters.draws,
        crowdShadowDraws: counters.shadowDraws,
        crowdEarlyVisible: readback.early,
        crowdLateVisible: readback.late,
        crowdShadowCasters: readback.shadow,
      };
    },

    dispose: function(): void {
      disposed = true;
      batches.forEach(function(state: any) {
        release(state.params); release(state.agents); release(state.poses); release(state.identity); release(state.visible); release(state.visibility);
      });
      batches.clear();
      atlases.forEach(function(record: any) { release(record.texture); });
      atlases.clear();
      [argsBuffer, hzb, hzbLevelsBuffer, hzbSeedBuffer, viewsBuffer].forEach(release);
      if (!readback.busy) release(readback.buffer);
      argsBuffer = hzb = hzbLevelsBuffer = hzbSeedBuffer = viewsBuffer = null;
      frameBatches = [];
      pipelines = {};
    },
  };
}
