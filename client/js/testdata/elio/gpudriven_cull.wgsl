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
