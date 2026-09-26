# Appendix A — Kernel sources, emitted WGSL, hand WGSL, derivations

Every block here was validated before the spec was written. The kernels
compiled on real WebGPU (Tint via Chromium 141 + SwiftShader), and the CPU
interpreter and the GPU produced identical survivor sets. Copy blocks
byte-for-byte. After copying, check the SHA-256 listed under each block. A
mismatch almost always means lost trailing whitespace or a missing final
newline. Every file ends with exactly one `\n`.

## A1. `elio/stdlib/gpudriven/cull.elio`

SHA-256 `01fdea902048ae858190988531209a87d468d475c94e38a7f101375cae54a5f8`

Language notes for editors: `.elio` has no `&&`, `||`, `continue`, helper
functions, bitwise operators, or exponent literals (`1e-8` must be written
`0.00000001`). That is why conditions are nested `if`s.

```elio
// cull.elio — Scene3D GPU-driven instance cull: frustum, optional distance
// and contribution culls, two-phase Hi-Z occlusion, and shadow-light views.
// One invocation per instance. A survivor appends its global instance index
// to its mesh's region for this view's slot in gdVisible, and bumps the
// instanceCount lane of the matching indirect draw args in gdArgs.

struct GDInstance {
  model: mat4;
  color: vec4;
  meshIndex: u32;
  pickId: u32;
  _pad0: u32;
  _pad1: u32;
}

struct GDMesh {
  sphere: vec4;
  firstInstance: u32;
  instanceCount: u32;
  argsBase: u32;
  listBase: u32;
  castShadow: u32;
  vertexCount: u32;
  _pad0: u32;
  _pad1: u32;
}

struct GDView {
  viewProj: mat4;
  planes: [6]vec4;
  viewport: vec4;
  eye: vec4;
  params: vec4;
  control: vec4u;
  hzbLevels: [16]vec4u;
}

@group(0) @binding(0) uniform gdView: GDView;
@group(0) @binding(1) storage read gdInstances: []GDInstance;
@group(0) @binding(2) storage read gdMeshes: []GDMesh;
@group(0) @binding(3) storage read_write gdArgs: []atomic_u32;
@group(0) @binding(4) storage read_write gdVisible: []u32;
@group(0) @binding(5) storage read_write gdVisibility: []u32;
@group(0) @binding(6) storage read gdHzb: []f32;

@workgroup(64) kernel cull(gid: global_invocation_id) {
  let i = gid.x;
  if i >= gdView.control.x { return; }
  let phase = gdView.control.z;
  let slot = gdView.control.y;
  let rec = gdInstances[i];
  let gm = gdMeshes[rec.meshIndex];
  let m = rec.model;
  let ls = gm.sphere;
  let center = m[0].xyz * ls.x + m[1].xyz * ls.y + m[2].xyz * ls.z + m[3].xyz;
  // Radius scale: the exact spectral norm (largest column length) when the
  // three basis columns are mutually orthogonal, which covers every
  // translate/rotate/scale transform; otherwise the Frobenius norm, which
  // never under-estimates the largest singular value under shear.
  let c0 = m[0].xyz;
  let c1 = m[1].xyz;
  let c2 = m[2].xyz;
  let l0 = dot(c0, c0);
  let l1 = dot(c1, c1);
  let l2 = dot(c2, c2);
  let d01 = dot(c0, c1);
  let d02 = dot(c0, c2);
  let d12 = dot(c1, c2);
  var scale2 = l0 + l1 + l2;
  if d01 * d01 <= 0.00000001 * l0 * l1 {
    if d02 * d02 <= 0.00000001 * l0 * l2 {
      if d12 * d12 <= 0.00000001 * l1 * l2 {
        scale2 = max(l0, max(l1, l2));
      }
    }
  }
  let scale = sqrt(scale2);
  var radius = ls.w;
  if scale > 0.0 { radius = ls.w * scale; }

  var keep = true;
  if phase == 2u {
    if gm.castShadow == 0u { keep = false; }
  }
  for (var p: i32 = 0; p < 6; p = p + 1) {
    let plane = gdView.planes[p];
    if dot(plane.xyz, center) + plane.w < -radius { keep = false; break; }
  }

  if phase < 2u {
    if keep {
      if gdView.eye.w > 0.0 {
        if distance(center, gdView.eye.xyz) - radius > gdView.eye.w { keep = false; }
      }
    }
    var testRect = false;
    if keep {
      if gdView.params.x > 0.0 { testRect = true; }
      if phase == 1u {
        if gdView.control.w > 0u { testRect = true; }
      }
    }
    if testRect {
      let vp = gdView.viewProj;
      var behind = false;
      var firstCorner = true;
      var minX = 0.0;
      var minY = 0.0;
      var maxX = 0.0;
      var maxY = 0.0;
      var minZ = 0.0;
      for (var c: u32 = 0u; c < 8u; c = c + 1u) {
        var sx = -1.0;
        if c % 2u == 1u { sx = 1.0; }
        var sy = -1.0;
        if (c / 2u) % 2u == 1u { sy = 1.0; }
        var sz = -1.0;
        if (c / 4u) % 2u == 1u { sz = 1.0; }
        let clip = vp[0] * (center.x + sx * radius) + vp[1] * (center.y + sy * radius) + vp[2] * (center.z + sz * radius) + vp[3];
        if clip.w <= 0.0001 {
          behind = true;
        } else {
          let nx = clip.x / clip.w;
          let ny = clip.y / clip.w;
          let nz = clip.z / clip.w;
          if firstCorner {
            minX = nx;
            maxX = nx;
            minY = ny;
            maxY = ny;
            minZ = nz;
            firstCorner = false;
          } else {
            minX = min(minX, nx);
            maxX = max(maxX, nx);
            minY = min(minY, ny);
            maxY = max(maxY, ny);
            minZ = min(minZ, nz);
          }
        }
      }
      if !behind {
        let w = gdView.viewport.x;
        let h = gdView.viewport.y;
        let x0 = clamp((minX * 0.5 + 0.5) * w, 0.0, w);
        let x1 = clamp((maxX * 0.5 + 0.5) * w, 0.0, w);
        let y0 = clamp((0.5 - maxY * 0.5) * h, 0.0, h);
        let y1 = clamp((0.5 - minY * 0.5) * h, 0.0, h);
        let extent = max(x1 - x0, y1 - y0);
        if gdView.params.x > 0.0 {
          if extent < gdView.params.x { keep = false; }
        }
        if phase == 1u {
          if gdView.control.w > 0u {
            if keep {
              var level = u32(max(ceil(log2(max(extent, 1.0))) - 1.0, 0.0));
              if level >= gdView.control.w { level = gdView.control.w - 1u; }
              let lv = gdView.hzbLevels[level];
              let texel = exp2(f32(level) + 1.0);
              let tx0 = min(u32(x0 / texel), lv.y - 1u);
              let tx1 = min(u32(x1 / texel), lv.y - 1u);
              let ty0 = min(u32(y0 / texel), lv.z - 1u);
              let ty1 = min(u32(y1 / texel), lv.z - 1u);
              let z00 = gdHzb[lv.x + ty0 * lv.y + tx0];
              let z10 = gdHzb[lv.x + ty0 * lv.y + tx1];
              let z01 = gdHzb[lv.x + ty1 * lv.y + tx0];
              let z11 = gdHzb[lv.x + ty1 * lv.y + tx1];
              let farthest = max(max(z00, z10), max(z01, z11));
              if minZ > farthest { keep = false; }
            }
          }
        }
      }
    }
  }

  if phase == 1u {
    let prev = gdVisibility[i];
    if keep {
      gdVisibility[i] = 1u;
    } else {
      gdVisibility[i] = 0u;
    }
    if prev == 1u { keep = false; }
  }
  if phase == 0u {
    if gdView.control.w > 0u {
      if gdVisibility[i] == 0u { keep = false; }
    }
  }
  if keep {
    let n = atomicAdd(&gdArgs[(gm.argsBase + slot) * 4u + 1u], 1u);
    gdVisible[gm.listBase + slot * gm.instanceCount + n] = i;
  }
}
```

### What the kernel does, step by step (read this before touching it)

1. One invocation per instance `i` (`gid.x`). It returns when
   `i >= gdView.control.x` (the live instance count).
2. `phase = control.z`: 0 = camera early (or the single camera view when
   occlusion is off), 1 = camera late, 2 = shadow light. `slot = control.y`
   selects the survivor list and args entry: 0 early, 1 late, 2 light 0,
   3 light 1.
3. World bounding sphere: the centre is `model × localCentre`. The radius is
   `localRadius × scale`. `scale` is the largest column length when the three
   basis columns are mutually orthogonal (|cos|² ≤ 1e-8), and the Frobenius
   norm otherwise. A zero scale keeps the local radius.
4. A light view (phase 2) drops meshes whose `castShadow == 0`.
5. Frustum: drop when `dot(n, c) + d < -r` for any of the six planes (inside
   is `>= 0`).
6. Camera phases only:
   - Distance cull: drop when `eye.w > 0` and `distance(c, eye) - r > eye.w`.
   - When `params.x > 0` (min pixel size), or when phase 1 has
     `control.w > 0` (Hi-Z levels), project the 8 corners of the sphere's
     world AABB. If any corner has `clip.w <= 0.0001`, the sphere crosses the
     eye plane; keep the instance and skip both tests. Otherwise build the
     pixel rect (y flipped) and the minimum NDC depth `minZ`.
   - Contribution: drop when the larger rect side is below `params.x` pixels.
   - Occlusion (phase 1): `level = max(ceil(log2(max(extent,1))) - 1, 0)`,
     clamped to the last level. One level-L texel covers `2^(L+1)` pixels, so
     the rect touches at most 2×2 texels. Drop when `minZ > max(4 texels)`.
7. Phase 1 rewrites `gdVisibility[i]` (1 when kept, 0 otherwise). It keeps an
   instance for the late list only when it was **not** visible before
   (`prev == 0`); visible-before instances were already drawn early.
8. Phase 0 with occlusion on (`control.w > 0`) keeps only instances with
   `gdVisibility[i] == 1`.
9. A survivor does `n = atomicAdd(args[(argsBase + slot)*4 + 1], 1)` and then
   `visible[listBase + slot*instanceCount + n] = i`.

## A2. `elio/stdlib/gpudriven/hiz_downsample.elio`

SHA-256 `57626c7a65fc2f7e1cc456a173879105158ebfdf8616658a83ec3f7511094649`

```elio
// hiz_downsample.elio — one Hi-Z pyramid level: dst[x,y] = max of the 2x2
// source texels under it, clamped at odd edges so the pyramid stays
// conservative and every read is in bounds. All levels share one buffer.

struct HiZLevel {
  srcOffset: u32;
  srcWidth: u32;
  srcHeight: u32;
  dstOffset: u32;
  dstWidth: u32;
  dstHeight: u32;
  _pad0: u32;
  _pad1: u32;
}

@group(0) @binding(0) uniform gdLevel: HiZLevel;
@group(0) @binding(1) storage read_write gdHzb: []f32;

@workgroup(64) kernel downsample(gid: global_invocation_id) {
  let i = gid.x;
  if i >= gdLevel.dstWidth * gdLevel.dstHeight { return; }
  let dx = i % gdLevel.dstWidth;
  let dy = i / gdLevel.dstWidth;
  let sx0 = dx * 2u;
  let sy0 = dy * 2u;
  let sx1 = min(sx0 + 1u, gdLevel.srcWidth - 1u);
  let sy1 = min(sy0 + 1u, gdLevel.srcHeight - 1u);
  let a = gdHzb[gdLevel.srcOffset + sy0 * gdLevel.srcWidth + sx0];
  let b = gdHzb[gdLevel.srcOffset + sy0 * gdLevel.srcWidth + sx1];
  let c = gdHzb[gdLevel.srcOffset + sy1 * gdLevel.srcWidth + sx0];
  let d = gdHzb[gdLevel.srcOffset + sy1 * gdLevel.srcWidth + sx1];
  gdHzb[gdLevel.dstOffset + i] = max(max(a, b), max(c, d));
}
```

## A3. Emitted `cull.wgsl` (Elio golden; also GoSX `client/js/testdata/elio/gpudriven_cull.wgsl`)

SHA-256 `86de419e4baf2067b5c3e26067e34abf1e4657a27d8565fdab379c2aa57ad485`

Generated, not hand-written: `UPDATE_GOLDEN=1 go test ./stdlib -run TestGPUDrivenWGSLGoldens`
in the elio repo writes it. It is shown here so reviewers and the E2 check
have a reference.

```wgsl
struct GDInstance {
  model : mat4x4<f32>,
  color : vec4<f32>,
  meshIndex : u32,
  pickId : u32,
  _pad0 : u32,
  _pad1 : u32,
};

struct GDMesh {
  sphere : vec4<f32>,
  firstInstance : u32,
  instanceCount : u32,
  argsBase : u32,
  listBase : u32,
  castShadow : u32,
  vertexCount : u32,
  _pad0 : u32,
  _pad1 : u32,
};

struct GDView {
  viewProj : mat4x4<f32>,
  planes : array<vec4<f32>, 6>,
  viewport : vec4<f32>,
  eye : vec4<f32>,
  params : vec4<f32>,
  control : vec4<u32>,
  hzbLevels : array<vec4<u32>, 16>,
};

@group(0) @binding(0) var<uniform> gdView : GDView;
@group(0) @binding(1) var<storage, read> gdInstances : array<GDInstance>;
@group(0) @binding(2) var<storage, read> gdMeshes : array<GDMesh>;
@group(0) @binding(3) var<storage, read_write> gdArgs : array<atomic<u32>>;
@group(0) @binding(4) var<storage, read_write> gdVisible : array<u32>;
@group(0) @binding(5) var<storage, read_write> gdVisibility : array<u32>;
@group(0) @binding(6) var<storage, read> gdHzb : array<f32>;

@compute @workgroup_size(64)
fn cull(@builtin(global_invocation_id) gid : vec3<u32>) {
  let i = gid.x;
  if ((i >= gdView.control.x)) {
    return;
  }
  let phase = gdView.control.z;
  let slot = gdView.control.y;
  let rec = gdInstances[i];
  let gm = gdMeshes[rec.meshIndex];
  let m = rec.model;
  let ls = gm.sphere;
  let center = ((((m[0].xyz * ls.x) + (m[1].xyz * ls.y)) + (m[2].xyz * ls.z)) + m[3].xyz);
  let c0 = m[0].xyz;
  let c1 = m[1].xyz;
  let c2 = m[2].xyz;
  let l0 = dot(c0, c0);
  let l1 = dot(c1, c1);
  let l2 = dot(c2, c2);
  let d01 = dot(c0, c1);
  let d02 = dot(c0, c2);
  let d12 = dot(c1, c2);
  var scale2 = ((l0 + l1) + l2);
  if (((d01 * d01) <= ((0.00000001 * l0) * l1))) {
    if (((d02 * d02) <= ((0.00000001 * l0) * l2))) {
      if (((d12 * d12) <= ((0.00000001 * l1) * l2))) {
        scale2 = max(l0, max(l1, l2));
      }
    }
  }
  let scale = sqrt(scale2);
  var radius = ls.w;
  if ((scale > 0.0)) {
    radius = (ls.w * scale);
  }
  var keep = true;
  if ((phase == 2u)) {
    if ((gm.castShadow == 0u)) {
      keep = false;
    }
  }
  for (var p : i32 = 0; (p < 6); p = (p + 1)) {
    let plane = gdView.planes[p];
    if (((dot(plane.xyz, center) + plane.w) < -radius)) {
      keep = false;
      break;
    }
  }
  if ((phase < 2u)) {
    if (keep) {
      if ((gdView.eye.w > 0.0)) {
        if (((distance(center, gdView.eye.xyz) - radius) > gdView.eye.w)) {
          keep = false;
        }
      }
    }
    var testRect = false;
    if (keep) {
      if ((gdView.params.x > 0.0)) {
        testRect = true;
      }
      if ((phase == 1u)) {
        if ((gdView.control.w > 0u)) {
          testRect = true;
        }
      }
    }
    if (testRect) {
      let vp = gdView.viewProj;
      var behind = false;
      var firstCorner = true;
      var minX = 0.0;
      var minY = 0.0;
      var maxX = 0.0;
      var maxY = 0.0;
      var minZ = 0.0;
      for (var c : u32 = 0u; (c < 8u); c = (c + 1u)) {
        var sx = -1.0;
        if (((c % 2u) == 1u)) {
          sx = 1.0;
        }
        var sy = -1.0;
        if ((((c / 2u) % 2u) == 1u)) {
          sy = 1.0;
        }
        var sz = -1.0;
        if ((((c / 4u) % 2u) == 1u)) {
          sz = 1.0;
        }
        let clip = ((((vp[0] * (center.x + (sx * radius))) + (vp[1] * (center.y + (sy * radius)))) + (vp[2] * (center.z + (sz * radius)))) + vp[3]);
        if ((clip.w <= 0.0001)) {
          behind = true;
        } else {
          let nx = (clip.x / clip.w);
          let ny = (clip.y / clip.w);
          let nz = (clip.z / clip.w);
          if (firstCorner) {
            minX = nx;
            maxX = nx;
            minY = ny;
            maxY = ny;
            minZ = nz;
            firstCorner = false;
          } else {
            minX = min(minX, nx);
            maxX = max(maxX, nx);
            minY = min(minY, ny);
            maxY = max(maxY, ny);
            minZ = min(minZ, nz);
          }
        }
      }
      if (!behind) {
        let w = gdView.viewport.x;
        let h = gdView.viewport.y;
        let x0 = clamp((((minX * 0.5) + 0.5) * w), 0.0, w);
        let x1 = clamp((((maxX * 0.5) + 0.5) * w), 0.0, w);
        let y0 = clamp(((0.5 - (maxY * 0.5)) * h), 0.0, h);
        let y1 = clamp(((0.5 - (minY * 0.5)) * h), 0.0, h);
        let extent = max((x1 - x0), (y1 - y0));
        if ((gdView.params.x > 0.0)) {
          if ((extent < gdView.params.x)) {
            keep = false;
          }
        }
        if ((phase == 1u)) {
          if ((gdView.control.w > 0u)) {
            if (keep) {
              var level = u32(max((ceil(log2(max(extent, 1.0))) - 1.0), 0.0));
              if ((level >= gdView.control.w)) {
                level = (gdView.control.w - 1u);
              }
              let lv = gdView.hzbLevels[level];
              let texel = exp2((f32(level) + 1.0));
              let tx0 = min(u32((x0 / texel)), (lv.y - 1u));
              let tx1 = min(u32((x1 / texel)), (lv.y - 1u));
              let ty0 = min(u32((y0 / texel)), (lv.z - 1u));
              let ty1 = min(u32((y1 / texel)), (lv.z - 1u));
              let z00 = gdHzb[((lv.x + (ty0 * lv.y)) + tx0)];
              let z10 = gdHzb[((lv.x + (ty0 * lv.y)) + tx1)];
              let z01 = gdHzb[((lv.x + (ty1 * lv.y)) + tx0)];
              let z11 = gdHzb[((lv.x + (ty1 * lv.y)) + tx1)];
              let farthest = max(max(z00, z10), max(z01, z11));
              if ((minZ > farthest)) {
                keep = false;
              }
            }
          }
        }
      }
    }
  }
  if ((phase == 1u)) {
    let prev = gdVisibility[i];
    if (keep) {
      gdVisibility[i] = 1u;
    } else {
      gdVisibility[i] = 0u;
    }
    if ((prev == 1u)) {
      keep = false;
    }
  }
  if ((phase == 0u)) {
    if ((gdView.control.w > 0u)) {
      if ((gdVisibility[i] == 0u)) {
        keep = false;
      }
    }
  }
  if (keep) {
    let n = atomicAdd(&gdArgs[(((gm.argsBase + slot) * 4u) + 1u)], 1u);
    gdVisible[((gm.listBase + (slot * gm.instanceCount)) + n)] = i;
  }
}
```

## A4. Emitted `hiz_downsample.wgsl` (Elio golden; also GoSX `client/js/testdata/elio/gpudriven_hiz_downsample.wgsl`)

SHA-256 `d35c9e084df9c15897058429ffdfea3a85fd74a6bf3c48243accd99391cb8428`

```wgsl
struct HiZLevel {
  srcOffset : u32,
  srcWidth : u32,
  srcHeight : u32,
  dstOffset : u32,
  dstWidth : u32,
  dstHeight : u32,
  _pad0 : u32,
  _pad1 : u32,
};

@group(0) @binding(0) var<uniform> gdLevel : HiZLevel;
@group(0) @binding(1) var<storage, read_write> gdHzb : array<f32>;

@compute @workgroup_size(64)
fn downsample(@builtin(global_invocation_id) gid : vec3<u32>) {
  let i = gid.x;
  if ((i >= (gdLevel.dstWidth * gdLevel.dstHeight))) {
    return;
  }
  let dx = (i % gdLevel.dstWidth);
  let dy = (i / gdLevel.dstWidth);
  let sx0 = (dx * 2u);
  let sy0 = (dy * 2u);
  let sx1 = min((sx0 + 1u), (gdLevel.srcWidth - 1u));
  let sy1 = min((sy0 + 1u), (gdLevel.srcHeight - 1u));
  let a = gdHzb[((gdLevel.srcOffset + (sy0 * gdLevel.srcWidth)) + sx0)];
  let b = gdHzb[((gdLevel.srcOffset + (sy0 * gdLevel.srcWidth)) + sx1)];
  let c = gdHzb[((gdLevel.srcOffset + (sy1 * gdLevel.srcWidth)) + sx0)];
  let d = gdHzb[((gdLevel.srcOffset + (sy1 * gdLevel.srcWidth)) + sx1)];
  gdHzb[(gdLevel.dstOffset + i)] = max(max(a, b), max(c, d));
}
```

## A5. Hi-Z seed shaders (GoSX-owned, hand-written)

Elio's IR has no texture bindings, so the step that reads the depth texture
cannot be an Elio kernel. These two shaders reduce the depth target 2×2 into
Hi-Z level 0. Each output texel is the max over the covered pixels; the
multisampled variant also takes the max over all samples. Edges clamp the
same way as A2. Both compiled on real WebGPU and matched a per-pixel CPU
reference exactly.

Single-sample (`SCENE_GPU_DRIVEN_HIZ_SEED_WGSL`), SHA-256 of the file text
`6a99f76e87d54b746748e51cdb1b7414c2428e827a705205304a32f24bdc988f`:

```wgsl
struct GDHiZSeed {
  dstWidth: u32,
  dstHeight: u32,
  srcWidth: u32,
  srcHeight: u32,
};

@group(0) @binding(0) var<uniform> gdSeed: GDHiZSeed;
@group(0) @binding(1) var gdDepth: texture_depth_2d;
@group(0) @binding(2) var<storage, read_write> gdHzb: array<f32>;

@compute @workgroup_size(8, 8)
fn seed(@builtin(global_invocation_id) gid: vec3<u32>) {
  if (gid.x >= gdSeed.dstWidth || gid.y >= gdSeed.dstHeight) { return; }
  let sx0 = gid.x * 2u;
  let sy0 = gid.y * 2u;
  let sx1 = min(sx0 + 1u, gdSeed.srcWidth - 1u);
  let sy1 = min(sy0 + 1u, gdSeed.srcHeight - 1u);
  let a = textureLoad(gdDepth, vec2<u32>(sx0, sy0), 0);
  let b = textureLoad(gdDepth, vec2<u32>(sx1, sy0), 0);
  let c = textureLoad(gdDepth, vec2<u32>(sx0, sy1), 0);
  let d = textureLoad(gdDepth, vec2<u32>(sx1, sy1), 0);
  gdHzb[gid.y * gdSeed.dstWidth + gid.x] = max(max(a, b), max(c, d));
}
```

Multisampled (`SCENE_GPU_DRIVEN_HIZ_SEED_MS_WGSL`), SHA-256
`9a01a1841a0c6bd396a85507042f5a39beccf3ed3404c48d2a541fea48a0a0be`:

```wgsl
struct GDHiZSeed {
  dstWidth: u32,
  dstHeight: u32,
  srcWidth: u32,
  srcHeight: u32,
};

@group(0) @binding(0) var<uniform> gdSeed: GDHiZSeed;
@group(0) @binding(1) var gdDepth: texture_depth_multisampled_2d;
@group(0) @binding(2) var<storage, read_write> gdHzb: array<f32>;

fn gdPixelMax(p: vec2<u32>) -> f32 {
  var d = 0.0;
  let n = textureNumSamples(gdDepth);
  for (var s = 0u; s < n; s = s + 1u) {
    d = max(d, textureLoad(gdDepth, p, s));
  }
  return d;
}

@compute @workgroup_size(8, 8)
fn seed(@builtin(global_invocation_id) gid: vec3<u32>) {
  if (gid.x >= gdSeed.dstWidth || gid.y >= gdSeed.dstHeight) { return; }
  let sx0 = gid.x * 2u;
  let sy0 = gid.y * 2u;
  let sx1 = min(sx0 + 1u, gdSeed.srcWidth - 1u);
  let sy1 = min(sy0 + 1u, gdSeed.srcHeight - 1u);
  let a = gdPixelMax(vec2<u32>(sx0, sy0));
  let b = gdPixelMax(vec2<u32>(sx1, sy0));
  let c = gdPixelMax(vec2<u32>(sx0, sy1));
  let d = gdPixelMax(vec2<u32>(sx1, sy1));
  gdHzb[gid.y * gdSeed.dstWidth + gid.x] = max(max(a, b), max(c, d));
}
```

Bind group layouts:

| Binding | Single-sample | Multisampled |
|---|---|---|
| 0 | `{ buffer: { type: "uniform" } }` (16 bytes: `dstWidth, dstHeight, srcWidth, srcHeight` as u32) | same |
| 1 | `{ texture: { sampleType: "depth", viewDimension: "2d", multisampled: false } }` | `{ texture: { sampleType: "depth", viewDimension: "2d", multisampled: true } }` |
| 2 | `{ buffer: { type: "storage" } }` (the Hi-Z buffer; level 0 starts at element 0) | same |

All entries use `visibility: GPUShaderStage.COMPUTE`. Dispatch:
`dispatchWorkgroups(ceil(level0Width / 8), ceil(level0Height / 8))`.

## A6. The GoSX instance struct used by the vertex-pulling shaders

Exactly this text (4-space indent, the renderer's vertex-shader style):

```wgsl
struct GDInstance {
    model: mat4x4f,
    color: vec4f,
    meshIndex: u32,
    pickId: u32,
    _pad0: u32,
    _pad1: u32,
};
```

It is layout-identical to the Elio struct in A3 (96 bytes).

## A7. Vertex-shader derivations (exact JS for `indirect-instancing.ts`)

These derive the GPU-driven vertex shaders from the renderer's own instanced
shaders. Lighting, varyings and normal handling therefore stay identical. Every
anchor was checked against `WGSL_PBR_INSTANCED_VERTEX` and
`WGSL_SHADOW_INSTANCED_VERTEX` at the baseline commit. The derived shaders
compiled and linked with the real `WGSL_PBR_FRAGMENT` / `WGSL_SHADOW_FRAGMENT`
on WebGPU.

```js
  var SCENE_GPU_DRIVEN_INSTANCE_STRUCT_WGSL = [
    "struct GDInstance {",
    "    model: mat4x4f,",
    "    color: vec4f,",
    "    meshIndex: u32,",
    "    pickId: u32,",
    "    _pad0: u32,",
    "    _pad1: u32,",
    "};",
  ].join("\n");

  // sceneGPUDrivenReplace swaps one exact anchor and throws when it is
  // missing, so a renderer edit that moves an anchor fails loudly in tests
  // instead of shipping a shader that still reads the old instance inputs.
  function sceneGPUDrivenReplace(source, anchor, replacement, label) {
    if (typeof source !== "string" || source.indexOf(anchor) < 0) {
      throw new Error("gpu-driven: vertex shader anchor missing: " + label);
    }
    return source.replace(anchor, replacement);
  }

  function sceneGPUDrivenPBRVertexWGSL(base) {
    var s = sceneGPUDrivenReplace(base, [
      "    @location(4) instanceMatrix0: vec4f,",
      "    @location(5) instanceMatrix1: vec4f,",
      "    @location(6) instanceMatrix2: vec4f,",
      "    @location(7) instanceMatrix3: vec4f,",
      "    @location(8) instanceColor: vec4f,",
    ].join("\n"), "    @location(4) gdSlot: u32,", "pbr inputs");
    s = sceneGPUDrivenReplace(s, "@group(0) @binding(0) var<uniform> frame: FrameUniforms;",
      "@group(0) @binding(0) var<uniform> frame: FrameUniforms;\n" + SCENE_GPU_DRIVEN_INSTANCE_STRUCT_WGSL +
      "\n@group(2) @binding(0) var<storage, read> gdInstances: array<GDInstance>;", "pbr binding");
    s = sceneGPUDrivenReplace(s, "    let model = mat4x4f(in.instanceMatrix0, in.instanceMatrix1, in.instanceMatrix2, in.instanceMatrix3);",
      "    let gdInstance = gdInstances[in.gdSlot];\n    let model = gdInstance.model;", "pbr model");
    s = sceneGPUDrivenReplace(s, "    out.instanceColor = in.instanceColor;",
      "    out.instanceColor = gdInstance.color;", "pbr color");
    return s;
  }

  function sceneGPUDrivenShadowVertexWGSL(base) {
    var s = sceneGPUDrivenReplace(base, [
      "    @location(4) instanceMatrix0: vec4f,",
      "    @location(5) instanceMatrix1: vec4f,",
      "    @location(6) instanceMatrix2: vec4f,",
      "    @location(7) instanceMatrix3: vec4f,",
    ].join("\n"), "    @location(4) gdSlot: u32,", "shadow inputs");
    s = sceneGPUDrivenReplace(s, "@group(0) @binding(0) var<uniform> shadowFrame: ShadowFrameUniforms;",
      "@group(0) @binding(0) var<uniform> shadowFrame: ShadowFrameUniforms;\n" + SCENE_GPU_DRIVEN_INSTANCE_STRUCT_WGSL +
      "\n@group(1) @binding(0) var<storage, read> gdInstances: array<GDInstance>;", "shadow binding");
    s = sceneGPUDrivenReplace(s, "    let model = mat4x4f(in.instanceMatrix0, in.instanceMatrix1, in.instanceMatrix2, in.instanceMatrix3);",
      "    let model = gdInstances[in.gdSlot].model;", "shadow model");
    return s;
  }
```

Properties of the derived text (the G06 derivation test asserts all of them):

- PBR: contains `@location(4) gdSlot: u32,`,
  `@group(2) @binding(0) var<storage, read> gdInstances: array<GDInstance>;`,
  `let gdInstance = gdInstances[in.gdSlot];` and
  `out.instanceColor = gdInstance.color;`. Contains no `instanceMatrix` and no
  `@location(8)`.
- Shadow: contains `@location(4) gdSlot: u32,`,
  `@group(1) @binding(0) var<storage, read> gdInstances: array<GDInstance>;`
  and `let model = gdInstances[in.gdSlot].model;`. Contains no `instanceMatrix`.

## A8. Embedding a WGSL file as a JS string array

Do not hand-transcribe WGSL into the runtime. From the gosx root, generate the
literal. It prints a block you paste verbatim:

```sh
node -e '
const fs = require("fs");
const [file, name] = process.argv.slice(1);
const lines = fs.readFileSync(file, "utf8").replace(/\n$/, "").split("\n");
process.stdout.write("  var " + name + " = [\n" +
  lines.map((l) => "    " + JSON.stringify(l) + ",").join("\n") +
  "\n  ].join(\"\\n\");\n");
' client/js/testdata/elio/gpudriven_cull.wgsl SCENE_GPU_DRIVEN_CULL_WGSL
```

The embedded string equals the file **without its final newline**. G05's test
compares `embedded === fileText.replace(/\n$/, "")`.
