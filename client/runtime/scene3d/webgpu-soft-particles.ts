// WebGPU soft particles. A blended builtin compute-particle sprite fades over
// material.softDistance world units as it nears the opaque depth behind it.
//
// The main pass draws every hard sprite first. When any system asks for soft
// edges, the main pass ends and a continuation pass reopens the same color
// attachments with loadOp "load" and the same depth texture as a read-only
// depth attachment. WebGPU permits a read-only depth attachment to be bound as
// a texture in the same pass, so soft sprites keep the depth test and read the
// scene depth without a copy.
//
// Both depths are linearized with the projection the frame actually uploaded
// (frame.projMatrix, already remapped to WebGPU's [0,1] clip depth):
//   viewZ = (m[3][2] - z * m[3][3]) / (z * m[2][3] - m[2][2])
// That expression holds for perspective and orthographic projections alike.

// Soft points uniforms extend PointsUniforms (128 bytes) with one vec4.
// soft.x is 1 / softDistance; zero disables the fade.
const SCENE_WEBGPU_SOFT_POINTS_FLOATS = 36;

// wgpuSoftParticleInverseDistance returns 1 / softDistance for a compute
// particle entry that may soften, else 0. Authored render shaders own their
// fragment output, and depth-writing or opaque sprites have no blended edge.
function wgpuSoftParticleInverseDistance(entry: any): number {
  if (!entry || typeof entry !== "object") return 0;
  var material = entry.material && typeof entry.material === "object" ? entry.material : null;
  var distance = material ? Number(material.softDistance) : 0;
  if (!(distance > 0) || !isFinite(distance)) return 0;
  var blend = typeof material.blendMode === "string" ? material.blendMode.toLowerCase() : "";
  if (blend !== "alpha" && blend !== "additive") return 0;
  if (entry.depthWrite === true) return 0;
  if (entry.renderVertexWGSL && entry.renderFragmentWGSL) return 0;
  return 1 / distance;
}

// wgpuSoftParticleFragment builds the soft entry point over the shared
// pointsShade function, so sprite styles and fog match hard sprites exactly.
// PointsUniforms gains one trailing vec4 for the fade terms.
function wgpuSoftParticleFragment(multisampled: boolean): string {
  var depthType = multisampled ? "texture_depth_multisampled_2d" : "texture_depth_2d";
  return [
    WGSL_FRAME_STRUCTS,
    "@group(0) @binding(0) var<uniform> frame: FrameUniforms;",
    "struct PointsUniforms {",
    WGSL_POINTS_UNIFORM_FIELDS,
    "    soft: vec4f,",
    "};",
    "@group(3) @binding(0) var softDepth: " + depthType + ";",
    WGSL_POINTS_SHADE,
    "fn softParticleViewDistance(z: f32) -> f32 {",
    "    let m = frame.projMatrix;",
    "    return -(m[3][2] - z * m[3][3]) / (z * m[2][3] - m[2][2]);",
    "}",
    "fn softParticleFade(fragCoord: vec4f) -> f32 {",
    "    if (points.soft.x <= 0.0) { return 1.0; }",
    "    let sceneDepth = textureLoad(softDepth, vec2i(fragCoord.xy), 0);",
    "    let gap = softParticleViewDistance(sceneDepth) - softParticleViewDistance(fragCoord.z);",
    "    return clamp(gap * points.soft.x, 0.0, 1.0);",
    "}",
    "@fragment fn fragmentMain(in: PointsInput, @builtin(position) fragCoord: vec4f) -> @location(0) vec4f {",
    "    let shaded = pointsShade(in);",
    "    return vec4f(shaded.rgb, shaded.a * softParticleFade(fragCoord));",
    "}",
  ].join("\n");
}

// wgpuCreateSoftParticles owns the soft pipelines and depth bind groups for one
// renderer. resources() returns the renderer's live device, layouts, scratch
// uniforms and cache helpers; it is read lazily, so a scene without soft
// sprites allocates nothing.
function wgpuCreateSoftParticles(resources: () => any) {
  var layouts: any = {}, modules: any = {}, pipelines: any = {};
  var group: any = null, groupView: any = null, groupSamples = 0;
  var scratch = new Float32Array(SCENE_WEBGPU_SOFT_POINTS_FLOATS);
  var model = new Float32Array(16), tilt = new Float32Array(16), spin = new Float32Array(16);
  function layoutFor(r: any, samples: number) {
    var key = samples > 1 ? "ms" : "single";
    if (layouts[key]) return layouts[key];
    var depthLayout = r.device.createBindGroupLayout({ label: "gosx-soft-particle-depth", entries: [
      { binding: 0, visibility: GPUShaderStage.FRAGMENT, texture: { sampleType: "depth", multisampled: samples > 1 } },
    ] });
    layouts[key] = {
      depth: depthLayout,
      pipeline: r.device.createPipelineLayout({ bindGroupLayouts: [r.frame, r.material, r.points, depthLayout] }),
    };
    return layouts[key];
  }
  function pipelineFor(r: any, blendMode: string) {
    var key = blendMode + "|" + r.format + "|" + r.samples;
    if (pipelines[key]) return pipelines[key];
    var moduleKey = r.samples > 1 ? "ms" : "single";
    if (!modules[moduleKey]) modules[moduleKey] = r.device.createShaderModule({ label: "points-soft-frag", code: wgpuSoftParticleFragment(r.samples > 1) });
    pipelines[key] = wgpuCreatePointsPipeline(r.device, layoutFor(r, r.samples).pipeline, r.vertex, modules[moduleKey], blendMode, false, r.format, r.samples);
    return pipelines[key];
  }
  function depthGroup(r: any, depthView: any) {
    if (group && groupView === depthView && groupSamples === r.samples) return group;
    group = r.device.createBindGroup({ label: "gosx-soft-particle-depth", layout: layoutFor(r, r.samples).depth, entries: [
      { binding: 0, resource: depthView },
    ] });
    groupView = depthView; groupSamples = r.samples;
    return group;
  }
  function softSystem(record: any) {
    var system = record && record.system;
    if (!system || !system.renderBuffer || system.count <= 0) return null;
    if (typeof system.isReady === "function" && !system.isReady()) return null;
    return wgpuSoftParticleInverseDistance(system.entry) > 0 ? system : null;
  }
  return {
    // plan reports whether this frame needs the soft continuation pass.
    plan: function(records: any[], depthView: any): boolean {
      return !!depthView && Array.isArray(records) && records.some(function(record: any) { return !!softSystem(record); });
    },
    // hard returns the records the main pass draws: everything not soft.
    hard: function(records: any[]) {
      return records.filter(function(record: any) { return !softSystem(record); });
    },
    // beginPass ends the main pass and reopens its targets with read-only depth.
    beginPass: function(encoder: any, pass: any, descriptor: any) {
      pass.end();
      var colors = (descriptor.colorAttachments || []).map(function(attachment: any) {
        return attachment ? Object.assign({}, attachment, { loadOp: "load" }) : attachment;
      });
      return encoder.beginRenderPass({
        label: "gosx-soft-particles",
        colorAttachments: colors,
        depthStencilAttachment: { view: descriptor.depthStencilAttachment.view, depthReadOnly: true },
      });
    },
    // draw renders every soft system into the continuation pass.
    draw: function(pass: any, records: any[], environment: any, timeSeconds: number, depthView: any) {
      var r = resources();
      var stats = { computeParticleDrawEntries: 0, computeParticleDrawInstances: 0, computeParticleDrawCalls: 0 };
      var env = environment || {};
      var fogDensity = sceneNumber(env.fogDensity, 0);
      var fogColorRGBA = sceneColorRGBA(env.fogColor, [0.5, 0.5, 0.5, 1]);
      for (var i = 0; i < records.length; i++) {
        var system = softSystem(records[i]);
        if (!system) continue;
        var entry = system.entry, material = entry.material;
        wgpuComputeParticleModelMatrix(entry.emitter && typeof entry.emitter === "object" ? entry.emitter : {}, timeSeconds, model, tilt, spin);
        var uniforms = wgpuComputeParticlePointsUniforms(r.scratchF, r.scratchU, entry, material, tilt, model, fogDensity, fogColorRGBA);
        scratch.set(uniforms.subarray(0, 32), 0);
        scratch[32] = wgpuSoftParticleInverseDistance(entry);
        var points = r.bindGroup(system, "_gosxWGPUCPSoftPointsBG", r.points, [
          { binding: 0, resource: { buffer: r.buffer(system, scratch) } },
          { binding: 1, resource: { buffer: system.renderBuffer } },
        ]);
        pass.setPipeline(wgpuRequirePipeline(pipelineFor(r, material.blendMode.toLowerCase())));
        pass.setBindGroup(1, r.materialGroup(), [0]);
        pass.setBindGroup(2, points);
        pass.setBindGroup(3, depthGroup(r, depthView));
        pass.draw(6, system.count);
        stats.computeParticleDrawEntries += 1;
        stats.computeParticleDrawInstances += system.count;
        stats.computeParticleDrawCalls += 1;
      }
      return stats;
    },
  };
}

// wgpuSoftParticleMergeStats adds the soft pass draw counters to the frame.
function wgpuSoftParticleMergeStats(frameStats: any, softStats: any) {
  for (var key in softStats) frameStats[key] = (frameStats[key] || 0) + softStats[key];
  frameStats.computeParticleSoftDrawCalls = softStats.computeParticleDrawCalls;
}
