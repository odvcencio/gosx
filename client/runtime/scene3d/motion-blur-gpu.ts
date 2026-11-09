// WebGPU motion blur: velocity, tile max, neighbor max, gather. Camera motion
// comes from reprojecting the depth buffer through the previous frame's
// view-projection. Moving meshes add their own velocity when the renderer
// supplies frame.motion.velocityView (rgba16float: rg = uv offset to the
// previous frame, b = clip w, a = 1 where a mover is the visible surface).
(() => {
  const fx = (window as any).__gosx_scene3d_depthfx_api;
  if (!fx || !fx.motionBlurPlan) return;

  const FULLSCREEN = `
struct VOut { @builtin(position) pos: vec4f };
@vertex fn vs(@builtin(vertex_index) i: u32) -> VOut {
  let p = vec2f(f32((i << 1u) & 2u), f32(i & 2u));
  var o: VOut; o.pos = vec4f(p * 2.0 - 1.0, 0.0, 1.0); return o;
}`;

  const PARAMS = `
struct Params {
  invViewProj: mat4x4f, prevViewProj: mat4x4f, viewProj: mat4x4f,
  eye: vec4f, forward: vec4f,
  settings: vec4f,  // strength, maxBlur px, hasPrevious, hasMoverVelocity
  size: vec4f,      // width, height, 1/width, 1/height
};`;

  const VELOCITY_WGSL = FULLSCREEN + PARAMS + `
@group(0) @binding(0) var<uniform> P: Params;
@group(0) @binding(1) var depthTex: texture_depth_2d;
@group(0) @binding(2) var moverTex: texture_2d<f32>;
@fragment fn fs(in: VOut) -> @location(0) vec4f {
  let dims = vec2i(textureDimensions(depthTex));
  let px = clamp(vec2i(in.pos.xy), vec2i(0), dims - vec2i(1));
  let depth = textureLoad(depthTex, px, 0);
  let uv = (vec2f(px) + 0.5) * P.size.zw;
  var world = P.invViewProj * vec4f(uv.x * 2.0 - 1.0, 1.0 - uv.y * 2.0, depth, 1.0);
  world = world / world.w;
  var v = vec2f(0.0);
  if (P.settings.z > 0.5) {
    let prev = P.prevViewProj * vec4f(world.xyz, 1.0);
    if (prev.w > 0.0) { v = vec2f(prev.x / prev.w * 0.5 + 0.5, 0.5 - prev.y / prev.w * 0.5) - uv; }
  }
  if (P.settings.w > 0.5) {
    let m = textureLoad(moverTex, px, 0);
    let surfaceW = (P.viewProj * vec4f(world.xyz, 1.0)).w;
    if (m.a > 0.5 && m.a < 1.5 && abs(m.b - surfaceW) <= 0.02 * abs(surfaceW) + 0.02) { v = m.xy; }
  }
  var vp = v * P.size.xy * P.settings.x;
  let len = length(vp);
  if (len > P.settings.y) { vp = vp * (P.settings.y / len); }
  if (len < 0.5) { vp = vec2f(0.0); }
  return vec4f(vp, dot(world.xyz - P.eye.xyz, P.forward.xyz), 1.0);
}`;

  function tileWGSL(tile: number): string {
    return FULLSCREEN + `
@group(0) @binding(0) var velTex: texture_2d<f32>;
@fragment fn fs(in: VOut) -> @location(0) vec2f {
  let dims = vec2i(textureDimensions(velTex));
  let base = vec2i(in.pos.xy) * ${tile};
  var best = vec2f(0.0); var bestLen = 0.0;
  for (var y = 0; y < ${tile}; y++) {
    for (var x = 0; x < ${tile}; x++) {
      let p = base + vec2i(x, y);
      if (p.x >= dims.x || p.y >= dims.y) { continue; }
      let v = textureLoad(velTex, p, 0).xy;
      let l = dot(v, v);
      if (l > bestLen) { bestLen = l; best = v; }
    }
  }
  return best;
}`;
  }

  const NEIGHBOR_WGSL = FULLSCREEN + `
@group(0) @binding(0) var tileTex: texture_2d<f32>;
@fragment fn fs(in: VOut) -> @location(0) vec2f {
  let dims = vec2i(textureDimensions(tileTex));
  let c = vec2i(in.pos.xy);
  var best = vec2f(0.0); var bestLen = 0.0;
  for (var y = -1; y <= 1; y++) {
    for (var x = -1; x <= 1; x++) {
      let v = textureLoad(tileTex, clamp(c + vec2i(x, y), vec2i(0), dims - vec2i(1)), 0).xy;
      let l = dot(v, v);
      if (l > bestLen) { bestLen = l; best = v; }
    }
  }
  return best;
}`;

  function gatherWGSL(tile: number, samples: number): string {
    return FULLSCREEN + `
@group(0) @binding(0) var colorTex: texture_2d<f32>;
@group(0) @binding(1) var velTex: texture_2d<f32>;
@group(0) @binding(2) var neighborTex: texture_2d<f32>;
const SAMPLES = ${samples};
const SOFT_Z = 0.25;
fn softDepth(a: f32, b: f32, extent: f32) -> f32 { return clamp(1.0 - (a - b) / extent, 0.0, 1.0); }
fn cone(dist: f32, len: f32) -> f32 { return clamp(1.0 - dist / max(len, 0.0001), 0.0, 1.0); }
fn cylinder(dist: f32, len: f32) -> f32 { return 1.0 - smoothstep(0.95 * len, 1.05 * len, dist); }
@fragment fn fs(in: VOut) -> @location(0) vec4f {
  let dims = vec2i(textureDimensions(colorTex));
  let px = vec2i(in.pos.xy);
  let c0 = textureLoad(colorTex, px, 0);
  let tileDims = vec2i(textureDimensions(neighborTex));
  let vN = textureLoad(neighborTex, clamp(px / ${tile}, vec2i(0), tileDims - vec2i(1)), 0).xy;
  let lenN = length(vN);
  if (lenN < 0.5) { return c0; }
  let center = textureLoad(velTex, px, 0);
  let vX = center.xy; let zX = center.z;
  let lenX = length(vX);
  let pos = vec2f(px) + 0.5;
  let jitter = fract(52.9829189 * fract(dot(in.pos.xy, vec2f(0.06711056, 0.00583715)))) - 0.5;
  let extent = max(0.25, abs(zX) * 0.1);
  var sum = c0 * (1.0 / max(lenX, 1.0) * f32(SAMPLES) / 40.0);
  var weight = (1.0 / max(lenX, 1.0) * f32(SAMPLES) / 40.0);
  for (var i = 0; i < SAMPLES; i++) {
    let t = (f32(i) + 0.5 + jitter * 0.5) / f32(SAMPLES) * 2.0 - 1.0;
    // Alternate between the neighborhood's dominant velocity and this pixel's own.
    let dir = select(vN, select(vN, vX, lenX > 0.5), (i & 1) == 1);
    let q = clamp(vec2i(pos + dir * t), vec2i(0), dims - vec2i(1));
    let s = textureLoad(velTex, q, 0);
    let vY = s.xy; let zY = s.z;
    let dist = length(vec2f(q) + 0.5 - pos);
    let f = softDepth(zX, zY, extent);   // Y in front of X
    let b = softDepth(zY, zX, extent);   // Y behind X
    let lenY = length(vY);
    let alpha = f * cone(dist, lenY) + b * cone(dist, lenX) + cylinder(dist, lenY) * cylinder(dist, lenX) * 2.0;
    sum = sum + textureLoad(colorTex, q, 0) * alpha;
    weight = weight + alpha;
  }
  return vec4f(sum.rgb / max(weight, 0.0001), c0.a);
}`;
  }

  function createWebGPUMotionBlur(device: any) {
    const history = fx.createMotionHistory();
    const paramsData = new Float32Array(64);
    const paramsBuffer = device.createBuffer({ label: "gosx-motion-blur", size: 256, usage: GPUBufferUsage.UNIFORM | GPUBufferUsage.COPY_DST });
    const invVP = new Float32Array(16), eye = new Float32Array(3), forward = new Float32Array(3);
    const dummy = device.createTexture({ size: [1, 1], format: "rgba16float", usage: GPUTextureUsage.TEXTURE_BINDING });
    const dummyView = dummy.createView();
    const pipelines = new Map<string, any>();
    const layouts = new Map<string, any>();
    let targets: any = null;   // {w, h, tile, velocity, tileMax, neighbor, views...}
    let failed = false, disposed = false;

    function layout(key: string, entries: any[]): any {
      let l = layouts.get(key);
      if (!l) { l = device.createBindGroupLayout({ entries }); layouts.set(key, l); }
      return l;
    }
    const tex = (binding: number, sampleType = "unfilterable-float") => ({ binding, visibility: GPUShaderStage.FRAGMENT, texture: { sampleType } });
    function pipeline(key: string, code: string, bgl: any, format: string): any {
      let p = pipelines.get(key);
      if (!p) {
        const module = device.createShaderModule({ label: "gosx-motion-blur:" + key, code });
        p = device.createRenderPipeline({ label: "gosx-motion-blur:" + key, layout: device.createPipelineLayout({ bindGroupLayouts: [bgl] }),
          vertex: { module, entryPoint: "vs" }, fragment: { module, entryPoint: "fs", targets: [{ format }] }, primitive: { topology: "triangle-list" } });
        pipelines.set(key, p);
      }
      return p;
    }

    function ensureTargets(w: number, h: number, tile: number): any {
      if (targets && targets.w === w && targets.h === h && targets.tile === tile) return targets;
      destroyTargets();
      const tw = Math.ceil(w / tile), th = Math.ceil(h / tile);
      const make = (width: number, height: number, format: string) => {
        const texture = device.createTexture({ size: [width, height], format, usage: GPUTextureUsage.RENDER_ATTACHMENT | GPUTextureUsage.TEXTURE_BINDING });
        return { texture, view: texture.createView() };
      };
      targets = { w, h, tile, velocity: make(w, h, "rgba16float"), tileMax: make(tw, th, "rg16float"), neighbor: make(tw, th, "rg16float") };
      return targets;
    }
    function destroyTargets(): void {
      if (!targets) return;
      for (const k of ["velocity", "tileMax", "neighbor"]) targets[k].texture.destroy();
      targets = null;
    }

    function draw(encoder: any, label: string, target: any, pipe: any, group: any): void {
      const pass = encoder.beginRenderPass({ label, colorAttachments: [{ view: target, loadOp: "clear", clearValue: { r: 0, g: 0, b: 0, a: 0 }, storeOp: "store" }] });
      pass.setPipeline(pipe); pass.setBindGroup(0, group); pass.draw(3); pass.end();
    }

    function apply(effect: any, encoder: any, input: any, output: any, format: string, depthView: any, width: number, height: number, frame: any): boolean {
      if (failed || disposed || !frame || !frame.viewProj) return false;
      try {
        const plan = fx.motionBlurPlan(effect, frame, height);
        const now = typeof performance !== "undefined" ? performance.now() : Date.now();
        if (!fx.invert(invVP, frame.viewProj)) return false;
        fx.eyeAndForward(invVP, 0, 1, eye, forward);
        const hasPrev = history.step(now, eye);
        const prevVP = history.prev;
        const motion = frame.motion;
        const moverView = motion && motion.velocityView || null;
        const drawIt = !plan.skip && hasPrev;
        // Always remember this frame so the next one has a previous matrix.
        const snapshot = paramsData;
        if (!drawIt) { history.commit(frame.viewProj, now, eye); return false; }
        const t = ensureTargets(width, height, plan.tile);
        snapshot.set(invVP, 0); snapshot.set(prevVP, 16); snapshot.set(frame.viewProj, 32);
        snapshot.set([eye[0], eye[1], eye[2], 0], 48); snapshot.set([forward[0], forward[1], forward[2], 0], 52);
        snapshot.set([plan.strength, plan.maxBlur, 1, moverView ? 1 : 0], 56);
        snapshot.set([width, height, 1 / width, 1 / height], 60);
        device.queue.writeBuffer(paramsBuffer, 0, snapshot);
        history.commit(frame.viewProj, now, eye);

        const velBGL = layout("velocity", [{ binding: 0, visibility: GPUShaderStage.FRAGMENT, buffer: { type: "uniform" } }, { binding: 1, visibility: GPUShaderStage.FRAGMENT, texture: { sampleType: "depth" } }, tex(2)]);
        const unf = (n: number) => layout("tex" + n, Array.from({ length: n }, (_v, i) => tex(i)));
        const velGroup = device.createBindGroup({ layout: velBGL, entries: [{ binding: 0, resource: { buffer: paramsBuffer } }, { binding: 1, resource: depthView }, { binding: 2, resource: moverView || dummyView }] });
        draw(encoder, "gosx-motion-velocity", t.velocity.view, pipeline("velocity:" + format, VELOCITY_WGSL, velBGL, "rgba16float"), velGroup);
        const tileBGL = unf(1);
        draw(encoder, "gosx-motion-tilemax", t.tileMax.view, pipeline("tile:" + plan.tile, tileWGSL(plan.tile), tileBGL, "rg16float"),
          device.createBindGroup({ layout: tileBGL, entries: [{ binding: 0, resource: t.velocity.view }] }));
        draw(encoder, "gosx-motion-neighbor", t.neighbor.view, pipeline("neighbor", NEIGHBOR_WGSL, tileBGL, "rg16float"),
          device.createBindGroup({ layout: tileBGL, entries: [{ binding: 0, resource: t.tileMax.view }] }));
        const gatherBGL = unf(3);
        draw(encoder, "gosx-motion-gather", output, pipeline("gather:" + plan.tile + ":" + plan.samples + ":" + format, gatherWGSL(plan.tile, plan.samples), gatherBGL, format),
          device.createBindGroup({ layout: gatherBGL, entries: [{ binding: 0, resource: input }, { binding: 1, resource: t.velocity.view }, { binding: 2, resource: t.neighbor.view }] }));
        return true;
      } catch (error: any) {
        failed = true;
        console.error("[gosx] motion blur disabled:", error && error.message || error);
        return false;
      }
    }

    return {
      apply,
      dispose() { disposed = true; destroyTargets(); paramsBuffer.destroy(); dummy.destroy(); },
    };
  }

  fx.webgpu.motionBlur = createWebGPUMotionBlur;
})();
