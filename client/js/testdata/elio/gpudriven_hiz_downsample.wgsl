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
