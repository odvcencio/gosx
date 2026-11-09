// WebGPU cascaded shadow maps.
//
// Each shadow slot (two per frame, as in WebGL) owns one depth texture array.
// A directional light with shadowCascades 2..4 splits the camera view into
// that many cascades, one array layer each; a spot light, or a directional
// light with shadowCascades 0 or 1, keeps one layer with the full-scene fit.
//
// Stable cascades: each cascade is fitted to the bounding sphere of its view
// sub-frustum, so its size, and therefore its texel size, does not change when
// the camera rotates. The sphere center is snapped to the shadow-map texel
// grid in a light-space basis that depends only on the light direction, so a
// slow pan moves a fixed shadow edge in whole texels, never in fractions.
//
// Shadow slot contract (Selena bindings and other consumers rely on it):
//   slot.texture      GPUTexture, depth24plus, size x size x numCascades
//   slot.view         2d-array view of every layer (frame group bindings 4/6)
//   slot.layerViews   2d view per layer, used as the depth attachment
//   slot.size         texels per side
//   slot.numCascades  1..4
//   slot.cascades[c]  { lightMatrix, splitFar, biasScale }
//                     lightMatrix: column-major Float32Array(16), world to
//                     WebGPU clip with depth already 0..1 (y up in clip).
//                     splitFar: view-space distance where cascade c ends;
//                     the last cascade ends at the shadow distance.
//
// Fragment uniforms, frame group binding 8 (WGSL_SHADOW_UNIFORMS_STRUCT):
//   struct ShadowCascades {
//     matrices: array<mat4x4f, 4>,  // slot.cascades[c].lightMatrix
//     splits:   vec4f,              // splitFar per cascade, view-space units
//     params:   vec4f,              // x cascades, y bias, z softness, w blend band
//     scales:   vec4f,              // per-cascade bias scale (1 today)
//     texel:    vec4f,              // x 1/size, y size
//   };
//   struct ShadowUniforms { slot0: ShadowCascades, slot1: ShadowCascades,
//                           lightIndex: vec4i };  // x/y light index, -1 empty
// Byte offsets: slot0 at 0, slot1 at 320, lightIndex at 640; 656 bytes total.

const SCENE_WEBGPU_SHADOW_CASCADE_FLOATS = 80; // 320 bytes per slot
const SCENE_WEBGPU_SHADOW_UNIFORM_BYTES = 656;
const SCENE_WEBGPU_SHADOW_BLEND_BAND = 0.12;

const WGSL_SHADOW_UNIFORMS_STRUCT = [
  "struct ShadowCascades {",
  "    matrices: array<mat4x4f, 4>,",
  "    splits: vec4f,",
  "    params: vec4f,",
  "    scales: vec4f,",
  "    texel: vec4f,",
  "};",
  "",
  "struct ShadowUniforms {",
  "    slot0: ShadowCascades,",
  "    slot1: ShadowCascades,",
  "    lightIndex: vec4i,",
  "};",
].join("\n");

// WGSL_SHADOW_CASCADE_FNS samples one slot. It needs frame.viewMatrix for the
// fragment's view depth. softness 0 is a 4-tap 1-texel PCF; softness > 0 runs
// WebGL's 8-tap PCSS (blocker search, penumbra estimate, scaled PCF) with the
// same constants, so both backends soften the same way.
const WGSL_SHADOW_CASCADE_FNS = [
  "const kShadowDisk8 = array<vec2f, 8>(",
  "    vec2f(-0.94201624, -0.39906216), vec2f(0.94558609, -0.76890725),",
  "    vec2f(-0.09418410, -0.92938870), vec2f(0.34495938, 0.29387760),",
  "    vec2f(-0.91588581, 0.45771432), vec2f(-0.81544232, -0.87912464),",
  "    vec2f(0.38277543, 0.27676845), vec2f(0.97484398, 0.75648379),",
  ");",
  "",
  "fn shadowCascadeTaps(map: texture_depth_2d_array, samp: sampler_comparison, slot: ShadowCascades, c: u32, worldPos: vec3f) -> f32 {",
  "    let p = slot.matrices[c] * vec4f(worldPos, 1.0);",
  "    if (p.w <= 0.0) { return 1.0; }",
  "    let ndc = p.xyz / p.w;",
  "    let uv = vec2f(ndc.x * 0.5 + 0.5, 0.5 - ndc.y * 0.5);",
  "    if (uv.x < 0.0 || uv.x > 1.0 || uv.y < 0.0 || uv.y > 1.0 || ndc.z < 0.0 || ndc.z > 1.0) { return 1.0; }",
  "    let texel = slot.texel.x;",
  "    let receiver = clamp(ndc.z - slot.params.y * slot.scales[c], 0.0, 1.0);",
  "    let layer = i32(c);",
  "    let softness = slot.params.z;",
  "    if (softness <= 0.0001) {",
  "        var hard = 0.0;",
  "        for (var i = 0u; i < 4u; i = i + 1u) {",
  "            hard = hard + textureSampleCompareLevel(map, samp, clamp(uv + kShadowDisk8[i] * texel, vec2f(0.0), vec2f(1.0)), layer, receiver);",
  "        }",
  "        return hard / 4.0;",
  "    }",
  "    let size = vec2f(slot.texel.y);",
  "    let blockerRadius = max(1.0, softness * 32.0);",
  "    var blockerSum = 0.0;",
  "    var blockers = 0.0;",
  "    for (var i = 0u; i < 8u; i = i + 1u) {",
  "        let tap = clamp(uv + kShadowDisk8[i] * texel * blockerRadius, vec2f(0.0), vec2f(1.0));",
  "        let d = textureLoad(map, vec2i(min(tap * size, size - vec2f(1.0))), layer, 0);",
  "        if (receiver > d) { blockerSum = blockerSum + d; blockers = blockers + 1.0; }",
  "    }",
  "    if (blockers < 0.5) { return 1.0; }",
  "    let blocker = blockerSum / blockers;",
  "    let penumbra = (receiver - blocker) * softness / max(blocker, 0.0001);",
  "    let radius = max(1.0, clamp(penumbra * 128.0, 1.0, softness * 96.0));",
  "    var lit = 0.0;",
  "    for (var i = 0u; i < 8u; i = i + 1u) {",
  "        lit = lit + textureSampleCompareLevel(map, samp, clamp(uv + kShadowDisk8[i] * texel * radius, vec2f(0.0), vec2f(1.0)), layer, receiver);",
  "    }",
  "    return lit / 8.0;",
  "}",
  "",
  // Pick the cascade by view depth, blend across the seam into the next
  // cascade, and fade to lit at the end of the shadow distance.
  "fn shadowSlotFactor(map: texture_depth_2d_array, samp: sampler_comparison, slot: ShadowCascades, worldPos: vec3f) -> f32 {",
  "    let count = u32(slot.params.x);",
  "    if (count == 0u) { return 1.0; }",
  "    let viewDepth = -(frame.viewMatrix * vec4f(worldPos, 1.0)).z;",
  "    if (count > 1u && viewDepth >= slot.splits[count - 1u]) { return 1.0; }",
  "    var c = 0u;",
  "    for (var k = 0u; k + 1u < count; k = k + 1u) { if (viewDepth >= slot.splits[k]) { c = k + 1u; } }",
  "    var factor = shadowCascadeTaps(map, samp, slot, c, worldPos);",
  "    if (count == 1u) { return factor; }",
  "    let far = slot.splits[c];",
  "    let near = select(0.0, slot.splits[max(c, 1u) - 1u], c > 0u);",
  "    let band = max((far - near) * slot.params.w, 0.0001);",
  "    let t = clamp((viewDepth - (far - band)) / band, 0.0, 1.0);",
  "    if (t > 0.0) {",
  "        var next = 1.0;",
  "        if (c + 1u < count) { next = shadowCascadeTaps(map, samp, slot, c + 1u, worldPos); }",
  "        factor = mix(factor, next, t);",
  "    }",
  "    return factor;",
  "}",
].join("\n");

// sceneWebGPUShadowCascadeSplits returns the far distance of each cascade,
// using the practical split scheme WebGL uses (lambda blends logarithmic and
// uniform spacing).
function sceneWebGPUShadowCascadeSplits(near: number, far: number, count: number, lambda: number): number[] {
  var n = Math.max(0.0001, near), f = Math.max(n + 0.0001, far);
  var lam = Math.max(0, Math.min(1, lambda));
  var splits = [];
  for (var i = 1; i <= count; i++) {
    var p = i / count;
    splits.push(lam * n * Math.pow(f / n, p) + (1 - lam) * (n + (f - n) * p));
  }
  return splits;
}

// sceneWebGPUShadowSphere returns the minimal bounding sphere of the view
// sub-frustum between near and far: its distance along the view axis and its
// radius. Neither depends on the camera orientation.
function sceneWebGPUShadowSphere(near: number, far: number, tanX: number, tanY: number) {
  var k2 = tanX * tanX + tanY * tanY;
  var z = Math.min(far, (far + near) * (1 + k2) * 0.5);
  var radius = Math.sqrt(far * far * k2 + (far - z) * (far - z));
  // Quantize the radius so float noise cannot change the texel size.
  return { distance: z, radius: Math.ceil(radius * 64) / 64 };
}

function sceneWebGPUShadowLightBasis(lightDir: number[]) {
  var fx = lightDir[0], fy = lightDir[1], fz = lightDir[2];
  var len = Math.hypot(fx, fy, fz);
  if (len < 0.0001) { fx = 0; fy = -1; fz = 0; len = 1; }
  fx /= len; fy /= len; fz /= len;
  var ux = 0, uy = 1, uz = 0;
  if (Math.abs(fy) > 0.99) { uy = 0; uz = 1; }
  var rx = fy * uz - fz * uy, ry = fz * ux - fx * uz, rz = fx * uy - fy * ux;
  var rl = Math.hypot(rx, ry, rz) || 1;
  rx /= rl; ry /= rl; rz /= rl;
  return { f: [fx, fy, fz], r: [rx, ry, rz], u: [ry * fz - rz * fy, rz * fx - rx * fz, rx * fy - ry * fx] };
}

// sceneWebGPUShadowCascadeMatrix fits one cascade: an orthographic box of
// half-width radius around the snapped sphere center, reaching back along the
// light far enough to include every caster in the scene bounds. The result is
// GL clip (depth -1..1); the caller remaps depth for WebGPU.
function sceneWebGPUShadowCascadeMatrix(basis: any, center: number[], sphereRadius: number, size: number, sceneCenter: number[], sceneRadius: number) {
  var r = basis.r, u = basis.u, f = basis.f;
  // One texel of margin covers the half-texel snap shift; the snap grid and
  // the projected texel are then the same size.
  var radius = sphereRadius * size / Math.max(1, size - 2);
  var texel = (2 * radius) / Math.max(1, size);
  var cr = r[0] * center[0] + r[1] * center[1] + r[2] * center[2];
  var cu = u[0] * center[0] + u[1] * center[1] + u[2] * center[2];
  var dr = Math.round(cr / texel) * texel - cr, du = Math.round(cu / texel) * texel - cu;
  var cx = center[0] + r[0] * dr + u[0] * du, cy = center[1] + r[1] * dr + u[1] * du, cz = center[2] + r[2] * dr + u[2] * du;
  var gap = Math.hypot(cx - sceneCenter[0], cy - sceneCenter[1], cz - sceneCenter[2]);
  var back = Math.max(radius, gap + sceneRadius) + radius * 0.05;
  var depth = back + radius;
  // View: rows right, up, -forward; eye = center - forward * back.
  var ex = cx - f[0] * back, ey = cy - f[1] * back, ez = cz - f[2] * back;
  var view = new Float32Array([
    r[0], u[0], -f[0], 0,
    r[1], u[1], -f[1], 0,
    r[2], u[2], -f[2], 0,
    -(r[0] * ex + r[1] * ey + r[2] * ez), -(u[0] * ex + u[1] * ey + u[2] * ez), f[0] * ex + f[1] * ey + f[2] * ez, 1,
  ]);
  // Orthographic, x and y in [-radius, radius], view z in [-depth, 0].
  var proj = new Float32Array([
    1 / radius, 0, 0, 0,
    0, 1 / radius, 0, 0,
    0, 0, -2 / depth, 0,
    0, 0, -1, 1,
  ]);
  var out = new Float32Array(16);
  sceneMat4MultiplyInto(out, proj, view);
  return out;
}

// sceneWebGPUShadowCascadeCount is the cascade count a light gets: 2..4 for a
// directional light seen by a perspective camera, else 1.
function sceneWebGPUShadowCascadeCount(light: any, camera: any): number {
  var kind = typeof light.kind === "string" ? light.kind.toLowerCase() : "";
  var requested = Math.floor(sceneNumber(light.shadowCascades, 0));
  if (kind !== "directional" || requested < 2) return 1;
  if (!camera || !(sceneNumber(camera.fov, 0) > 0) || camera.kind === "orthographic" || camera.mode === "ortho2d") return 1;
  return Math.min(4, requested);
}

// wgpuEnsureShadowSlot returns a slot with the requested size and layers,
// reusing the previous slot when it already matches.
function wgpuEnsureShadowSlot(device: any, previous: any, size: number, layers: number, destroy: (texture: any) => void) {
  if (previous && previous.size === size && previous.numCascades === layers) return previous;
  if (previous) destroy(previous.texture);
  var texture = device.createTexture({
    label: "gosx-shadow-cascades",
    size: [size, size, layers],
    format: "depth24plus",
    usage: GPUTextureUsage.RENDER_ATTACHMENT | GPUTextureUsage.TEXTURE_BINDING,
  });
  var layerViews = [], cascades = [];
  for (var i = 0; i < layers; i++) {
    layerViews.push(texture.createView({ dimension: "2d", baseArrayLayer: i, arrayLayerCount: 1 }));
    cascades.push({ lightMatrix: null, splitFar: 0, biasScale: 1 });
  }
  return { texture: texture, view: texture.createView({ dimension: "2d-array" }), layerViews: layerViews, size: size, numCascades: layers, cascades: cascades };
}

// wgpuFitShadowSlot fills slot.cascades for one light. rawMatrix is the light's
// full-scene matrix (GL clip); a one-layer slot uses it unchanged. frame
// carries { view, camera, aspect, bounds }.
function wgpuFitShadowSlot(slot: any, light: any, rawMatrix: Float32Array, frame: any) {
  var cam = frame.camera || {};
  if (slot.numCascades <= 1) {
    slot.cascades[0].lightMatrix = sceneWebGPUShadowDepthMatrix(rawMatrix);
    slot.cascades[0].splitFar = sceneNumber(cam.far, 100);
    slot.cascades[0].biasScale = 1;
    return slot;
  }
  var near = Math.max(0.01, sceneNumber(cam.near, 0.1));
  var distance = sceneNumber(light.shadowDistance, 0);
  var far = Math.max(near + 0.01, distance > 0 ? Math.min(distance, sceneNumber(cam.far, distance)) : sceneNumber(cam.far, 100));
  var splits = sceneWebGPUShadowCascadeSplits(near, far, slot.numCascades, sceneNumber(light.shadowCascadeLambda, 0.5));
  var tanY = Math.tan((sceneNumber(cam.fov, 60) * Math.PI) / 360), tanX = tanY * Math.max(0.0001, frame.aspect || 1);
  var view = frame.view, b = frame.bounds || {};
  // Camera position and forward from the column-major view matrix.
  var px = -(view[0] * view[12] + view[1] * view[13] + view[2] * view[14]);
  var py = -(view[4] * view[12] + view[5] * view[13] + view[6] * view[14]);
  var pz = -(view[8] * view[12] + view[9] * view[13] + view[10] * view[14]);
  var fx = -view[2], fy = -view[6], fz = -view[10];
  var basis = sceneWebGPUShadowLightBasis([sceneNumber(light.directionX, 0), sceneNumber(light.directionY, -1), sceneNumber(light.directionZ, 0)]);
  var sceneCenter = [((b.minX || 0) + (b.maxX || 0)) / 2, ((b.minY || 0) + (b.maxY || 0)) / 2, ((b.minZ || 0) + (b.maxZ || 0)) / 2];
  var sceneRadius = Math.hypot(((b.maxX || 0) - (b.minX || 0)) / 2, ((b.maxY || 0) - (b.minY || 0)) / 2, ((b.maxZ || 0) - (b.minZ || 0)) / 2);
  var previous = near;
  for (var c = 0; c < slot.numCascades; c++) {
    var sphere = sceneWebGPUShadowSphere(previous, splits[c], tanX, tanY);
    var center = [px + fx * sphere.distance, py + fy * sphere.distance, pz + fz * sphere.distance];
    var matrix = sceneWebGPUShadowCascadeMatrix(basis, center, sphere.radius, slot.size, sceneCenter, sceneRadius);
    slot.cascades[c].lightMatrix = sceneWebGPUShadowDepthMatrix(matrix);
    slot.cascades[c].splitFar = splits[c];
    // Each cascade's depth range spans the scene, as the one-map fit does, so
    // the authored bias already covers its texels. The scale stays at 1.
    slot.cascades[c].biasScale = 1;
    previous = splits[c];
  }
  return slot;
}

// wgpuPackShadowUniforms writes ShadowUniforms for both slots and returns the
// float span [first, end) that differs from the last upload, or null when
// nothing changed, so a frame uploads only what moved. WebGPU zeroes new
// buffers, so the comparison starts from zero; an empty slot stays zero
// (count 0 means unshadowed). slots[s] may be null; lightIndices[s] is the
// light's index in lights, or -1.
function wgpuPackShadowUniforms(f: Float32Array, i32: Int32Array, slots: any[], lightIndices: number[], lights: any[]): number[] | null {
  var cache = f as any;
  if (!cache.__gosxShadowLast) {
    cache.__gosxShadowLast = new Uint32Array(f.length);
    cache.__gosxShadowBits = new Uint32Array(f.buffer, f.byteOffset, f.length);
    cache.__gosxShadowSpan = [0, 0];
  }
  var last = cache.__gosxShadowLast as Uint32Array, bits = cache.__gosxShadowBits as Uint32Array, span = cache.__gosxShadowSpan as number[];
  f.fill(0);
  for (var s = 0; s < 2; s++) {
    var slot = slots[s], light = lightIndices[s] >= 0 ? lights[lightIndices[s]] : null;
    var base = s * SCENE_WEBGPU_SHADOW_CASCADE_FLOATS;
    var count = slot && light ? slot.numCascades : 0;
    for (var c = 0; c < count; c++) {
      if (slot.cascades[c].lightMatrix) f.set(slot.cascades[c].lightMatrix, base + c * 16);
      f[base + 64 + c] = slot.cascades[c].splitFar;
      f[base + 72 + c] = slot.cascades[c].biasScale;
    }
    if (count > 0) {
      f[base + 68] = count;
      f[base + 69] = sceneNumber(light.shadowBias, 0.005);
      f[base + 70] = Math.max(0, sceneNumber(light.shadowSoftness, 0));
      f[base + 71] = SCENE_WEBGPU_SHADOW_BLEND_BAND;
      f[base + 76] = 1 / slot.size;
      f[base + 77] = slot.size;
    }
    i32[160 + s] = count > 0 ? lightIndices[s] : -1;
  }
  var first = -1, stop = 0;
  for (var k = 0; k < bits.length; k++) {
    if (bits[k] !== last[k]) { if (first < 0) first = k; stop = k + 1; }
  }
  if (first < 0) return null;
  last.set(bits.subarray(first, stop), first);
  span[0] = first;
  span[1] = stop;
  return span;
}
