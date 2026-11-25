// SMAA on WebGPU: the three passes smaa.ts describes, in WGSL. Texture rows run
// top to bottom, so the row above a pixel is y - 1.
(() => {
  const w = window as any;
  const api = w.__gosx_scene3d_smaa_api;
  if (!api || api.webgpu) return;

  const VERTEX = `@vertex fn vertexMain(@builtin(vertex_index) i: u32) -> @builtin(position) vec4f {
  let c = vec2f(f32((i << 1u) & 2u), f32(i & 2u));
  return vec4f(c.x * 2.0 - 1.0, 1.0 - c.y * 2.0, 0.0, 1.0);
}
`;
  const PARAMS = `struct SMAAParams { threshold: f32, steps: f32, corner: f32, pad: f32 };
@group(0) @binding(1) var<uniform> prm: SMAAParams;
`;

  // Pass 1. An edge sits on the left (R) or top (G) side of a pixel whose luma
  // differs from that neighbour's by the threshold, unless a neighbouring step
  // is more than twice as large (local contrast adaptation).
  const EDGES = PARAMS + `@group(0) @binding(0) var src: texture_2d<f32>;
fn luma(p: vec2i) -> f32 {
  let c = textureLoad(src, clamp(p, vec2i(0), vec2i(textureDimensions(src)) - 1), 0).rgb;
  return dot(clamp(c, vec3f(0.0), vec3f(1.0)), vec3f(0.2126, 0.7152, 0.0722));
}
@fragment fn fragmentMain(@builtin(position) pos: vec4f) -> @location(0) vec4f {
  let p = vec2i(pos.xy);
  let c = luma(p);
  let l = luma(p + vec2i(-1, 0));
  let t = luma(p + vec2i(0, -1));
  let d = abs(vec2f(c - l, c - t));
  var e = step(vec2f(prm.threshold), d);
  if (e.x + e.y == 0.0) { return vec4f(0.0); }
  let r = abs(c - luma(p + vec2i(1, 0)));
  let b = abs(c - luma(p + vec2i(0, 1)));
  let m = max(max(max(d.x, d.y), max(r, b)), max(abs(l - luma(p + vec2i(-2, 0))), abs(t - luma(p + vec2i(0, -2)))));
  e *= step(vec2f(m), 2.0 * d);
  return vec4f(e, 0.0, 1.0);
}
`;

  // Pass 2. k = 1 is the top edge (a horizontal line: walk along x, the crossing
  // edges are left edges, Q is the row above); k = 0 is the left edge (walk along
  // y, crossings are top edges, Q is the column to the left).
  const WEIGHTS = PARAMS + `@group(0) @binding(0) var edges: texture_2d<f32>;
var<private> dims: vec2i;
fn E(p: vec2i) -> vec2f {
  if (any(p < vec2i(0)) || any(p >= dims)) { return vec2f(0.0); }
  return textureLoad(edges, p, 0).rg;
}
fn ch(v: vec2f, k: i32) -> f32 { return select(v.x, v.y, k == 1); }
// The height the line reaches at an end: -0.5 when the crossing turns into this
// pixel's row, 0.5 when it turns into Q's, 0 for neither or both. A crossing
// that runs on past one pixel is a corner and keeps prm.corner of the rounding.
fn endHeight(cp: f32, cq: f32, kp: f32, kq: f32) -> f32 {
  if (cp * cq > 0.0) { return 0.0; }
  return 0.5 * (cq * select(1.0, prm.corner, kq > 0.0) - cp * select(1.0, prm.corner, kp > 0.0));
}
// Walks from p along its edge in direction s (-1 or 1) and returns the distance
// in pixels to the end of the edge and the line's height there. The walk stops
// at a crossing edge, where the edge ends, or after prm.steps pixels.
fn walk(p: vec2i, s: i32, k: i32) -> vec2f {
  let a = select(vec2i(0, 1), vec2i(1, 0), k == 1);
  let q = select(vec2i(-1, 0), vec2i(0, -1), k == 1);
  let n = i32(prm.steps);
  let first = select(1, 0, s < 0);
  var d = 0;
  loop {
    let b = p + a * (s * d + first);
    let cp = ch(E(b), 1 - k);
    let cq = ch(E(b + q), 1 - k);
    if (cp + cq > 0.0) {
      return vec2f(f32(d), endHeight(cp, cq, ch(E(b - q), 1 - k), ch(E(b + 2 * q), 1 - k)));
    }
    if (d >= n || ch(E(p + a * (s * (d + 1))), k) == 0.0) { break; }
    d += 1;
  }
  return vec2f(f32(d), 0.0);
}
// The integral of the line from (x0, h0) to (x1, h1) over this pixel's span
// [0, 1], split into the part below the edge (x) and above it (y).
fn span(x0: f32, x1: f32, h0: f32, h1: f32) -> vec2f {
  let a = max(x0, 0.0);
  let b = min(x1, 1.0);
  if (b <= a) { return vec2f(0.0); }
  let v = (b - a) * (h0 + (h1 - h0) * (0.5 * (a + b) - x0) / (x1 - x0));
  return vec2f(max(-v, 0.0), max(v, 0.0));
}
// The segment runs from d1 pixels before this pixel to d2 pixels after it; the
// line falls from h1 at the first end to 0 at the middle and rises to h2.
fn area(d1: f32, d2: f32, h1: f32, h2: f32) -> vec2f {
  let m = 0.5 * (d2 + 1.0 - d1);
  return span(-d1, m, h1, 0.0) + span(m, d2 + 1.0, 0.0, h2);
}
@fragment fn fragmentMain(@builtin(position) pos: vec4f) -> @location(0) vec4f {
  dims = vec2i(textureDimensions(edges));
  let p = vec2i(pos.xy);
  let e = E(p);
  var o = vec4f(0.0);
  if (e.y > 0.0) {
    let l = walk(p, -1, 1);
    let r = walk(p, 1, 1);
    o = vec4f(area(l.x, r.x, l.y, r.y), 0.0, 0.0);
  }
  if (e.x > 0.0) {
    let u = walk(p, -1, 0);
    let v = walk(p, 1, 0);
    o = vec4f(o.xy, area(u.x, v.x, u.y, v.y));
  }
  return o;
}
`;

  // Pass 3. This pixel takes w.x from above and w.z from the left; the pixel
  // below gives its .y and the pixel to the right its .w.
  const BLEND = `@group(0) @binding(0) var src: texture_2d<f32>;
@group(0) @binding(1) var wts: texture_2d<f32>;
var<private> dims: vec2i;
fn lin(p: vec2i) -> vec4f {
  let c = textureLoad(src, clamp(p, vec2i(0), dims - 1), 0);
  return vec4f(pow(max(c.rgb, vec3f(0.0)), vec3f(2.2)), c.a);
}
fn W(p: vec2i) -> vec4f {
  if (any(p >= dims)) { return vec4f(0.0); }
  return textureLoad(wts, p, 0);
}
@fragment fn fragmentMain(@builtin(position) pos: vec4f) -> @location(0) vec4f {
  dims = vec2i(textureDimensions(src));
  let p = vec2i(pos.xy);
  let own = W(p);
  let up = own.x;
  let left = own.z;
  let down = W(p + vec2i(0, 1)).y;
  let right = W(p + vec2i(1, 0)).w;
  if (up + left + down + right == 0.0) { return textureLoad(src, p, 0); }
  var a = vec2i(0, -1);
  var wa = up;
  var wb = down;
  if (max(left, right) > max(up, down)) { a = vec2i(-1, 0); wa = left; wb = right; }
  let m = lin(p) * (1.0 - wa - wb) + lin(p + a) * wa + lin(p - a) * wb;
  return vec4f(pow(m.rgb, vec3f(1.0 / 2.2)), m.a);
}
`;

  function create(device: any): any {
    const FRAG = (globalThis as any).GPUShaderStage ? (globalThis as any).GPUShaderStage.FRAGMENT : 2;
    const usage = (globalThis as any).GPUTextureUsage || { RENDER_ATTACHMENT: 16, TEXTURE_BINDING: 4 };
    const bufferUsage = (globalThis as any).GPUBufferUsage || { UNIFORM: 64, COPY_DST: 8 };
    const tex = { texture: { sampleType: "unfilterable-float" } };
    const withParams = device.createBindGroupLayout({ label: "gosx-smaa-params", entries: [{ binding: 0, visibility: FRAG, ...tex }, { binding: 1, visibility: FRAG, buffer: { type: "uniform" } }] });
    const twoTextures = device.createBindGroupLayout({ label: "gosx-smaa-blend", entries: [{ binding: 0, visibility: FRAG, ...tex }, { binding: 1, visibility: FRAG, ...tex }] });
    const params = device.createBuffer({ label: "gosx-smaa-params", size: 16, usage: bufferUsage.UNIFORM | bufferUsage.COPY_DST });
    const pipelines = new Map<string, any>(), groups = new Map<any, any>();
    let width = 0, height = 0, edges: any = null, weights: any = null, edgesView: any = null, weightsView: any = null, written = "";

    function pipeline(name: string, code: string, layout: any, format: string): any {
      const key = name + ":" + format;
      let p = pipelines.get(key);
      if (!p) {
        const module = device.createShaderModule({ label: "gosx-smaa-" + name, code: VERTEX + code });
        p = device.createRenderPipeline({
          label: "gosx-smaa-" + name, layout: device.createPipelineLayout({ bindGroupLayouts: [layout] }),
          vertex: { module, entryPoint: "vertexMain" }, fragment: { module, entryPoint: "fragmentMain", targets: [{ format }] },
          primitive: { topology: "triangle-list" },
        });
        pipelines.set(key, p);
      }
      return p;
    }
    // Bind groups are kept per (layout, first view, second resource); the chain
    // alternates between two targets, so a frame reuses its groups.
    function group(layout: any, a: any, b: any): any {
      let byA = groups.get(layout);
      if (!byA) groups.set(layout, byA = new Map());
      let byB = byA.get(a);
      if (!byB) byA.set(a, byB = new Map());
      let g = byB.get(b);
      if (!g) {
        g = device.createBindGroup({ layout, entries: [{ binding: 0, resource: a }, { binding: 1, resource: b === params ? { buffer: params } : b }] });
        byB.set(b, g);
      }
      return g;
    }
    function resize(w2: number, h2: number): void {
      if (w2 === width && h2 === height) return;
      release();
      width = w2; height = h2;
      edges = device.createTexture({ label: "gosx-smaa-edges", size: [w2, h2], format: "rg8unorm", usage: usage.RENDER_ATTACHMENT | usage.TEXTURE_BINDING });
      weights = device.createTexture({ label: "gosx-smaa-weights", size: [w2, h2], format: "rgba8unorm", usage: usage.RENDER_ATTACHMENT | usage.TEXTURE_BINDING });
      edgesView = edges.createView(); weightsView = weights.createView();
    }
    function release(): void {
      if (edges) edges.destroy();
      if (weights) weights.destroy();
      edges = weights = edgesView = weightsView = null;
      groups.clear();
      width = height = 0;
    }
    function draw(encoder: any, p: any, g: any, target: any): void {
      const pass = encoder.beginRenderPass({ label: p.label, colorAttachments: [{ view: target, loadOp: "clear", clearValue: { r: 0, g: 0, b: 0, a: 0 }, storeOp: "store" }] });
      pass.setPipeline(p);
      pass.setBindGroup(0, g);
      pass.draw(3);
      pass.end();
    }
    return {
      apply(effect: any, encoder: any, input: any, output: any, format: string, w2: number, h2: number): boolean {
        if (!input || !output || w2 < 1 || h2 < 1) return false;
        resize(w2, h2);
        const s = api.settings(effect), key = s.threshold + ":" + s.steps + ":" + s.corner;
        if (key !== written) { device.queue.writeBuffer(params, 0, new Float32Array([s.threshold, s.steps, s.corner, 0])); written = key; }
        draw(encoder, pipeline("edges", EDGES, withParams, "rg8unorm"), group(withParams, input, params), edgesView);
        draw(encoder, pipeline("weights", WEIGHTS, withParams, "rgba8unorm"), group(withParams, edgesView, params), weightsView);
        draw(encoder, pipeline("blend", BLEND, twoTextures, format), group(twoTextures, input, weightsView), output);
        return true;
      },
      dispose(): void { release(); params.destroy(); pipelines.clear(); },
    };
  }

  api.webgpu = create;
  api.wgsl = { edges: EDGES, weights: WEIGHTS, blend: BLEND, vertex: VERTEX };
})();
