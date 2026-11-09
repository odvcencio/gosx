// WebGPU projected decals. One pass copies the input color to the output, then
// draws each decal's box (back faces only) with blending. The fragment shader
// rebuilds the surface position from the scene depth, rejects pixels outside
// the box, rebuilds a normal from depth differences, and paints the decal.
(() => {
  const fx = (window as any).__gosx_scene3d_depthfx_api;
  if (!fx) return;

  const CUBE_INDICES = "1u,3u,7u,1u,7u,5u,0u,6u,2u,0u,4u,6u,2u,7u,3u,2u,6u,7u,0u,1u,5u,0u,5u,4u,4u,5u,7u,4u,7u,6u,0u,2u,3u,0u,3u,1u";

  const BLIT_WGSL = `
@group(0) @binding(0) var src: texture_2d<f32>;
@group(0) @binding(1) var samp: sampler;
struct V { @builtin(position) p: vec4f, @location(0) uv: vec2f };
@vertex fn vs(@builtin(vertex_index) i: u32) -> V {
  let p = vec2f(f32((i << 1u) & 2u), f32(i & 2u));
  var o: V; o.p = vec4f(p * 2.0 - 1.0, 0.0, 1.0); o.uv = vec2f(p.x, 1.0 - p.y); return o;
}
@fragment fn fs(v: V) -> @location(0) vec4f { return textureSample(src, samp, v.uv); }`;

  const DECAL_WGSL = `
struct Frame { viewProj: mat4x4f, invViewProj: mat4x4f, eye: vec4f, sunDir: vec4f, sunColor: vec4f, ambient: vec4f, size: vec4f };
struct Inst { model: mat4x4f, inv: mat4x4f, rect: vec4f, tint: vec4f };
struct Mat { fade: vec4f, pbr: vec4f, emissive: vec4f, color: vec4f };
@group(0) @binding(0) var<uniform> frame: Frame;
@group(0) @binding(1) var depthTex: texture_depth_2d;
@group(0) @binding(2) var<storage, read> insts: array<Inst>;
@group(1) @binding(0) var<uniform> mat: Mat;
@group(1) @binding(1) var albedoTex: texture_2d<f32>;
@group(1) @binding(2) var normalTex: texture_2d<f32>;
@group(1) @binding(3) var roughTex: texture_2d<f32>;
@group(1) @binding(4) var emissiveTex: texture_2d<f32>;
@group(1) @binding(5) var samp: sampler;
var<private> CUBE = array<u32, 36>(${CUBE_INDICES});
struct VOut { @builtin(position) pos: vec4f, @location(0) @interpolate(flat) id: u32 };
@vertex fn vs(@builtin(vertex_index) vi: u32, @builtin(instance_index) ii: u32) -> VOut {
  let c = CUBE[vi];
  let corner = vec3f(select(-0.5, 0.5, (c & 1u) != 0u), select(-0.5, 0.5, (c & 2u) != 0u), select(-0.5, 0.5, (c & 4u) != 0u));
  var o: VOut;
  o.pos = frame.viewProj * (insts[ii].model * vec4f(corner, 1.0));
  o.id = ii;
  return o;
}
fn depthAt(t: vec2i) -> f32 {
  let dims = vec2i(textureDimensions(depthTex));
  return textureLoad(depthTex, clamp(t, vec2i(0), dims - vec2i(1)), 0);
}
fn worldAt(t: vec2i) -> vec3f {
  let dims = vec2f(textureDimensions(depthTex));
  let uv = (vec2f(t) + 0.5) / dims;
  let w = frame.invViewProj * vec4f(uv.x * 2.0 - 1.0, 1.0 - uv.y * 2.0, depthAt(t), 1.0);
  return w.xyz / w.w;
}
struct FOut { @location(0) color: vec4f };
@fragment fn fs(in: VOut) -> FOut {
  let dims = vec2f(textureDimensions(depthTex));
  let tc = vec2i(in.pos.xy / frame.size.xy * dims);
  let d = depthAt(tc);
  if (d >= 0.999999) { discard; }
  let inst = insts[in.id];
  let p = worldAt(tc);
  let lp = (inst.inv * vec4f(p, 1.0)).xyz;
  if (any(abs(lp) > vec3f(0.5))) { discard; }
  let pl = worldAt(tc + vec2i(-1, 0)); let pr = worldAt(tc + vec2i(1, 0));
  let pu = worldAt(tc + vec2i(0, -1)); let pd = worldAt(tc + vec2i(0, 1));
  let useR = length(pr - p) < length(p - pl);
  let useD = length(pd - p) < length(p - pu);
  let dx = select(p - pl, pr - p, useR);
  let dy = select(p - pu, pd - p, useD);
  var n = normalize(cross(dx, dy));
  let toEye = frame.eye.xyz - p;
  if (dot(n, toEye) < 0.0) { n = -n; }
  let axisX = normalize(inst.model[0].xyz); let axisY = normalize(inst.model[1].xyz); let axisZ = normalize(inst.model[2].xyz);
  let angle = smoothstep(mat.fade.y, mat.fade.x, dot(n, axisZ));
  let e = mat.fade.z;
  let edge = (1.0 - smoothstep(0.5 - e, 0.5, abs(lp.x))) * (1.0 - smoothstep(0.5 - e, 0.5, abs(lp.y))) * (1.0 - smoothstep(0.5 - e, 0.5, abs(lp.z)));
  let inset = 0.5 / vec2f(textureDimensions(albedoTex, 0));
  let uv = clamp(vec2f(lp.x + 0.5, 0.5 - lp.y) * inst.rect.zw + inst.rect.xy, inst.rect.xy + inset, inst.rect.xy + inst.rect.zw - inset);
  let lx = (inst.inv * vec4f(select(p - pl, pr - p, useR) + p, 1.0)).xy - lp.xy;
  let ly = (inst.inv * vec4f(select(p - pu, pd - p, useD) + p, 1.0)).xy - lp.xy;
  let ddx = vec2f(lx.x, -lx.y) * inst.rect.zw;
  let ddy = vec2f(ly.x, -ly.y) * inst.rect.zw;
  let albedo = textureSampleGrad(albedoTex, samp, uv, ddx, ddy);
  let alpha = albedo.a * inst.tint.a * edge * angle;
  if (alpha <= 0.002) { discard; }
  let base = albedo.rgb * inst.tint.rgb * mat.color.rgb;
  var o: FOut;
  if (mat.color.w > 0.5) {
    o.color = vec4f(mix(vec3f(1.0), base, alpha), 0.0);
    return o;
  }
  var nm = n;
  if (mat.pbr.z > 0.5) {
    let t = textureSampleGrad(normalTex, samp, uv, ddx, ddy).xyz * 2.0 - 1.0;
    nm = normalize(n + (axisX * t.x + axisY * t.y) * mat.fade.w);
  }
  var rough = mat.pbr.x;
  if (mat.pbr.w > 0.5) { rough = rough * textureSampleGrad(roughTex, samp, uv, ddx, ddy).g; }
  rough = clamp(rough, 0.04, 1.0);
  let toSun = -frame.sunDir.xyz;
  let ndl = max(dot(nm, toSun), 0.0);
  var light = frame.ambient.rgb + frame.sunColor.rgb * ndl;
  let h = normalize(toSun + normalize(toEye));
  let shininess = 2.0 / (rough * rough) - 2.0;
  let spec = frame.sunColor.rgb * pow(max(dot(nm, h), 0.0), shininess) * (1.0 - rough) * 0.25 * select(0.0, 1.0, ndl > 0.0);
  var emit = vec3f(0.0);
  if (mat.emissive.w > 0.5) { emit = textureSampleGrad(emissiveTex, samp, uv, ddx, ddy).rgb * mat.emissive.rgb * mat.pbr.y; }
  else { emit = mat.emissive.rgb * mat.pbr.y; }
  o.color = vec4f((base * light + spec + emit) * alpha, alpha);
  return o;
}`;

  function createWebGPUDecals(device: any) {
    const set = fx.createDecalSet();
    const sampler = device.createSampler({ magFilter: "linear", minFilter: "linear", mipmapFilter: "linear", addressModeU: "clamp-to-edge", addressModeV: "clamp-to-edge", maxAnisotropy: 4 });
    const blitSampler = device.createSampler({ magFilter: "linear", minFilter: "linear" });
    const frameData = new Float32Array(56);
    const invVP = new Float32Array(16);
    const eye = new Float32Array(3);
    const ambient = new Float32Array(3), sunDir = new Float32Array(3), sunColor = new Float32Array(3);
    const frameBuffer = device.createBuffer({ label: "gosx-decal-frame", size: 224, usage: GPUBufferUsage.UNIFORM | GPUBufferUsage.COPY_DST });
    let instBuffer: any = null, instCapacity = 0;
    const pipelines = new Map<string, any>();
    let blitLayout: any = null, frameLayout: any = null, matLayout: any = null, decalLayout: any = null;
    let frameGroup: any = null, frameGroupDepth: any = null, frameGroupBuffer: any = null;
    const blitGroups = new WeakMap<object, any>();
    let failed = false, disposed = false;
    let materialStates: any[] = [];
    let materialsFor: any = null;

    // ---- textures ------------------------------------------------------------
    const placeholders: any = {};
    function placeholder(kind: string, rgba: number[], format: string): any {
      if (placeholders[kind]) return placeholders[kind];
      const texture = device.createTexture({ size: [1, 1], format, usage: GPUTextureUsage.TEXTURE_BINDING | GPUTextureUsage.COPY_DST });
      device.queue.writeTexture({ texture }, new Uint8Array(rgba), { bytesPerRow: 4 }, [1, 1]);
      return (placeholders[kind] = { texture, view: texture.createView() });
    }
    const textures = new Map<string, any>();
    let mipPipelines = new Map<string, any>();

    function blitModule(): any { return blitModuleCache || (blitModuleCache = device.createShaderModule({ label: "gosx-decal-blit", code: BLIT_WGSL })); }
    let blitModuleCache: any = null;
    function blitBGL(): any {
      return blitLayout || (blitLayout = device.createBindGroupLayout({ entries: [
        { binding: 0, visibility: GPUShaderStage.FRAGMENT, texture: { sampleType: "float" } },
        { binding: 1, visibility: GPUShaderStage.FRAGMENT, sampler: { type: "filtering" } }] }));
    }
    function blitPipeline(format: string, cache: Map<string, any>): any {
      let pipeline = cache.get(format);
      if (!pipeline) {
        const module = blitModule();
        pipeline = device.createRenderPipeline({ label: "gosx-decal-blit:" + format, layout: device.createPipelineLayout({ bindGroupLayouts: [blitBGL()] }),
          vertex: { module, entryPoint: "vs" }, fragment: { module, entryPoint: "fs", targets: [{ format }] }, primitive: { topology: "triangle-list" } });
        cache.set(format, pipeline);
      }
      return pipeline;
    }

    function generateMips(texture: any, format: string, levels: number): void {
      const encoder = device.createCommandEncoder({ label: "gosx-decal-mips" });
      const pipeline = blitPipeline(format, mipPipelines);
      for (let level = 1; level < levels; level++) {
        const group = device.createBindGroup({ layout: blitBGL(), entries: [
          { binding: 0, resource: texture.createView({ baseMipLevel: level - 1, mipLevelCount: 1 }) }, { binding: 1, resource: blitSampler }] });
        const pass = encoder.beginRenderPass({ colorAttachments: [{ view: texture.createView({ baseMipLevel: level, mipLevelCount: 1 }), loadOp: "clear", clearValue: { r: 0, g: 0, b: 0, a: 0 }, storeOp: "store" }] });
        pass.setPipeline(pipeline); pass.setBindGroup(0, group); pass.draw(3); pass.end();
      }
      device.queue.submit([encoder.finish()]);
    }

    // kind: albedo/emissive load as sRGB color; normal/rough as linear data.
    function textureFor(url: string, kind: string): any {
      const srgb = kind === "albedo" || kind === "emissive";
      const fallback = kind === "normal" ? placeholder("normal", [128, 128, 255, 255], "rgba8unorm")
        : kind === "rough" ? placeholder("rough", [255, 255, 255, 255], "rgba8unorm")
        : kind === "emissive" ? placeholder("emissive", [0, 0, 0, 255], "rgba8unorm-srgb")
        : placeholder("albedo", [255, 255, 255, 255], "rgba8unorm-srgb");
      if (!url) return { view: fallback.view, rev: 0 };
      const key = kind + "|" + url;
      let entry = textures.get(key);
      if (entry) return entry;
      entry = { view: fallback.view, rev: 0, texture: null };
      textures.set(key, entry);
      fx.loadBitmap(url).then((bitmap: any) => {
        if (disposed) { bitmap.close && bitmap.close(); return; }
        const format = srgb ? "rgba8unorm-srgb" : "rgba8unorm";
        const levels = Math.floor(Math.log2(Math.max(bitmap.width, bitmap.height))) + 1;
        const texture = device.createTexture({ size: [bitmap.width, bitmap.height], format, mipLevelCount: levels,
          usage: GPUTextureUsage.TEXTURE_BINDING | GPUTextureUsage.COPY_DST | GPUTextureUsage.RENDER_ATTACHMENT });
        device.queue.copyExternalImageToTexture({ source: bitmap }, { texture }, [bitmap.width, bitmap.height]);
        if (levels > 1) generateMips(texture, format, levels);
        bitmap.close && bitmap.close();
        entry.texture = texture; entry.view = texture.createView(); entry.rev++;
      }).catch((error: any) => { console.warn("[gosx] decal texture failed:", url, error && error.message || error); });
      return entry;
    }

    // ---- pipelines & per-material resources -----------------------------------
    function layouts(): void {
      if (frameLayout) return;
      frameLayout = device.createBindGroupLayout({ entries: [
        { binding: 0, visibility: GPUShaderStage.VERTEX | GPUShaderStage.FRAGMENT, buffer: { type: "uniform" } },
        { binding: 1, visibility: GPUShaderStage.FRAGMENT, texture: { sampleType: "depth" } },
        { binding: 2, visibility: GPUShaderStage.VERTEX | GPUShaderStage.FRAGMENT, buffer: { type: "read-only-storage" } }] });
      const tex = (binding: number) => ({ binding, visibility: GPUShaderStage.FRAGMENT, texture: { sampleType: "float" } });
      matLayout = device.createBindGroupLayout({ entries: [
        { binding: 0, visibility: GPUShaderStage.FRAGMENT, buffer: { type: "uniform" } },
        tex(1), tex(2), tex(3), tex(4), { binding: 5, visibility: GPUShaderStage.FRAGMENT, sampler: { type: "filtering" } }] });
      decalLayout = device.createPipelineLayout({ bindGroupLayouts: [frameLayout, matLayout] });
    }

    function decalPipeline(format: string, multiply: boolean): any {
      const key = format + (multiply ? "|mul" : "|lit");
      let pipeline = pipelines.get(key);
      if (pipeline) return pipeline;
      layouts();
      let module = pipelines.get("module");
      if (!module) { module = device.createShaderModule({ label: "gosx-decals", code: DECAL_WGSL }); pipelines.set("module", module); }
      const blend = multiply
        ? { color: { srcFactor: "dst", dstFactor: "zero", operation: "add" }, alpha: { srcFactor: "zero", dstFactor: "one", operation: "add" } }
        : { color: { srcFactor: "one", dstFactor: "one-minus-src-alpha", operation: "add" }, alpha: { srcFactor: "one", dstFactor: "one-minus-src-alpha", operation: "add" } };
      pipeline = device.createRenderPipeline({ label: "gosx-decals:" + key, layout: decalLayout,
        vertex: { module, entryPoint: "vs" }, fragment: { module, entryPoint: "fs", targets: [{ format, blend }] },
        primitive: { topology: "triangle-list", cullMode: "front", frontFace: "ccw" } });
      pipelines.set(key, pipeline);
      return pipeline;
    }

    function buildMaterials(): void {
      materialStates.forEach((m: any) => m.uniformBuffer && m.uniformBuffer.destroy());
      const list = set.materials;
      materialStates = list.map((m: any) => {
        const buffer = device.createBuffer({ label: "gosx-decal-material", size: 64, usage: GPUBufferUsage.UNIFORM | GPUBufferUsage.COPY_DST });
        device.queue.writeBuffer(buffer, 0, m.uniforms);
        return { uniformBuffer: buffer, group: null as any, revSum: -1, tex: [textureFor(m.albedo, "albedo"), textureFor(m.normal, "normal"), textureFor(m.roughness, "rough"), textureFor(m.emissive, "emissive")] };
      });
    }

    function materialGroup(state: any): any {
      const rev = state.tex[0].rev + state.tex[1].rev * 1000 + state.tex[2].rev * 1000000 + state.tex[3].rev * 1000000000;
      if (state.group && state.revSum === rev) return state.group;
      layouts();
      state.group = device.createBindGroup({ layout: matLayout, entries: [
        { binding: 0, resource: { buffer: state.uniformBuffer } },
        { binding: 1, resource: state.tex[0].view }, { binding: 2, resource: state.tex[1].view },
        { binding: 3, resource: state.tex[2].view }, { binding: 4, resource: state.tex[3].view }, { binding: 5, resource: sampler }] });
      state.revSum = rev;
      return state.group;
    }

    function ensureInstanceBuffer(floats: number): void {
      const bytes = Math.max(160, floats * 4);
      if (instBuffer && instCapacity >= bytes) return;
      if (instBuffer) instBuffer.destroy();
      instCapacity = Math.max(bytes, instCapacity * 2, 160 * 64);
      instBuffer = device.createBuffer({ label: "gosx-decal-instances", size: instCapacity, usage: GPUBufferUsage.STORAGE | GPUBufferUsage.COPY_DST });
      frameGroup = null;
    }

    // apply draws the decals over input into output. It returns false, leaving
    // output untouched, when there is nothing to draw.
    function apply(effect: any, encoder: any, input: any, output: any, format: string, depthView: any, width: number, height: number, frame: any): boolean {
      if (failed || disposed || !frame || !frame.viewProj) return false;
      try {
        const rebuilt = set.update(effect);
        if (rebuilt || materialsFor !== set.materials) { materialsFor = set.materials; buildMaterials(); }
        const cam = frame.camera || {};
        eye[0] = +cam.x || 0; eye[1] = +cam.y || 0; eye[2] = +cam.z || 0;
        const count = set.prepare(frame.viewProj, true, eye);
        if (count === 0 || !fx.invert(invVP, frame.viewProj)) return false;
        ensureInstanceBuffer(count * fx.DECAL_INSTANCE_FLOATS);
        device.queue.writeBuffer(instBuffer, 0, set.instances, 0, count * fx.DECAL_INSTANCE_FLOATS);
        const sun = fx.lightSummary(frame, ambient, sunDir, sunColor);
        frameData.set(frame.viewProj, 0); frameData.set(invVP, 16);
        frameData[32] = eye[0]; frameData[33] = eye[1]; frameData[34] = eye[2];
        frameData[36] = sunDir[0]; frameData[37] = sunDir[1]; frameData[38] = sunDir[2]; frameData[39] = sun ? 1 : 0;
        frameData[40] = sunColor[0]; frameData[41] = sunColor[1]; frameData[42] = sunColor[2];
        frameData[44] = ambient[0]; frameData[45] = ambient[1]; frameData[46] = ambient[2];
        frameData[48] = width; frameData[49] = height;
        device.queue.writeBuffer(frameBuffer, 0, frameData);
        layouts();
        if (!frameGroup || frameGroupDepth !== depthView || frameGroupBuffer !== instBuffer) {
          frameGroup = device.createBindGroup({ layout: frameLayout, entries: [
            { binding: 0, resource: { buffer: frameBuffer } }, { binding: 1, resource: depthView }, { binding: 2, resource: { buffer: instBuffer } }] });
          frameGroupDepth = depthView; frameGroupBuffer = instBuffer;
        }
        let blitGroup = blitGroups.get(input);
        if (!blitGroup) { blitGroup = device.createBindGroup({ layout: blitBGL(), entries: [{ binding: 0, resource: input }, { binding: 1, resource: blitSampler }] }); blitGroups.set(input, blitGroup); }
        const pass = encoder.beginRenderPass({ label: "gosx-decals", colorAttachments: [{ view: output, loadOp: "clear", clearValue: { r: 0, g: 0, b: 0, a: 0 }, storeOp: "store" }] });
        pass.setPipeline(blitPipeline(format, blitPipelines)); pass.setBindGroup(0, blitGroup); pass.draw(3);
        pass.setBindGroup(0, frameGroup);
        const mats = set.materials, runMaterial = set.runMaterial, runFirst = set.runFirst, runLength = set.runLength;
        let bound = "";
        for (let r = 0; r < set.runs; r++) {
          const mi = runMaterial[r];
          const multiply = mats[mi].multiply;
          const key = multiply ? "m" : "l";
          if (bound !== key) { pass.setPipeline(decalPipeline(format, multiply)); bound = key; }
          pass.setBindGroup(1, materialGroup(materialStates[mi]));
          pass.draw(36, runLength[r], 0, runFirst[r]);
        }
        pass.end();
        return true;
      } catch (error: any) {
        failed = true;
        console.error("[gosx] decal pass disabled:", error && error.message || error);
        return false;
      }
    }
    const blitPipelines = new Map<string, any>();

    return {
      apply,
      stats: () => ({ visible: set.visible, runs: set.runs }),
      dispose() {
        disposed = true;
        if (instBuffer) instBuffer.destroy();
        frameBuffer.destroy();
        materialStates.forEach((m: any) => m.uniformBuffer && m.uniformBuffer.destroy());
        textures.forEach((t: any) => t.texture && t.texture.destroy());
        Object.keys(placeholders).forEach((k: string) => placeholders[k].texture.destroy());
      },
    };
  }

  fx.webgpu.decals = createWebGPUDecals;
})();
