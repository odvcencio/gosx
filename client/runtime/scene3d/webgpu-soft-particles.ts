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

// wgpuSoftParticleFragment derives the soft fragment shader from the builtin
// points fragment, so sprite styles and fog stay identical to hard sprites.
function wgpuSoftParticleFragment(base: string, frameStructs: string, multisampled: boolean): string {
  var depthType = multisampled ? "texture_depth_multisampled_2d" : "texture_depth_2d";
  var source = base
    .replace("    fogColor: vec4f,\n};", "    fogColor: vec4f,\n    soft: vec4f,\n};")
    .replace("struct PointsInput {\n", "struct PointsInput {\n    @builtin(position) fragCoord: vec4f,\n")
    .replace("    return vec4f(color, alpha);\n}", "    return vec4f(color, alpha * softParticleFade(in.fragCoord));\n}");
  return [
    frameStructs,
    "@group(0) @binding(0) var<uniform> frame: FrameUniforms;",
    "@group(3) @binding(0) var softDepth: " + depthType + ";",
    source,
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
  ].join("\n");
}

// wgpuCreateSoftParticles owns the soft pipelines and depth bind groups for one
// renderer. resources() returns the renderer's live device and layouts; it is
// read lazily, so a scene without soft sprites allocates nothing.
function wgpuCreateSoftParticles(resources: () => any) {
  var layouts: any = {}, modules: any = {}, pipelines: any = {};
  var group: any = null, groupView: any = null, groupSamples = 0;
  var scratch = new Float32Array(SCENE_WEBGPU_SOFT_POINTS_FLOATS);
  function layoutFor(samples: number) {
    var key = samples > 1 ? "ms" : "single";
    if (layouts[key]) return layouts[key];
    var r = resources();
    var depthLayout = r.device.createBindGroupLayout({ label: "gosx-soft-particle-depth", entries: [
      { binding: 0, visibility: GPUShaderStage.FRAGMENT, texture: { sampleType: "depth", multisampled: samples > 1 } },
    ] });
    layouts[key] = {
      depth: depthLayout,
      pipeline: r.device.createPipelineLayout({ bindGroupLayouts: [r.frame, r.material, r.points, depthLayout] }),
    };
    return layouts[key];
  }
  function moduleFor(samples: number) {
    var key = samples > 1 ? "ms" : "single";
    if (!modules[key]) {
      var r = resources();
      modules[key] = r.device.createShaderModule({ label: "points-soft-frag", code: wgpuSoftParticleFragment(r.fragmentSource, r.frameStructs, samples > 1) });
    }
    return modules[key];
  }
  return {
    // plan reports whether this frame needs the soft continuation pass.
    plan: function(records: any[], depthView: any): boolean {
      if (!depthView || !Array.isArray(records)) return false;
      for (var i = 0; i < records.length; i++) {
        var system = records[i] && records[i].system;
        if (system && system.count > 0 && wgpuSoftParticleInverseDistance(system.entry) > 0) return true;
      }
      return false;
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
    pipeline: function(blendMode: string, targetFormat: string, samples: number) {
      var key = blendMode + "|" + targetFormat + "|" + samples;
      if (!pipelines[key]) {
        var r = resources();
        pipelines[key] = wgpuCreatePointsPipeline(r.device, layoutFor(samples).pipeline, r.vertex, moduleFor(samples), blendMode, false, targetFormat, samples);
      }
      return pipelines[key];
    },
    depthGroup: function(depthView: any, samples: number) {
      if (group && groupView === depthView && groupSamples === samples) return group;
      group = resources().device.createBindGroup({ label: "gosx-soft-particle-depth", layout: layoutFor(samples).depth, entries: [
        { binding: 0, resource: depthView },
      ] });
      groupView = depthView; groupSamples = samples;
      return group;
    },
    // uniforms copies the 32-float points uniforms and appends the fade terms.
    uniforms: function(pointsUniforms: Float32Array, inverseDistance: number) {
      scratch.set(pointsUniforms.subarray(0, 32), 0);
      scratch[32] = inverseDistance; scratch[33] = 0; scratch[34] = 0; scratch[35] = 0;
      return scratch;
    },
    reset: function() { layouts = {}; modules = {}; pipelines = {}; group = null; groupView = null; groupSamples = 0; },
  };
}

// wgpuSoftParticleMergeStats adds the soft pass draw counters to the frame.
function wgpuSoftParticleMergeStats(frameStats: any, softStats: any) {
  // The soft pass walks the same records, so its skip counters repeat the
  // main pass and are left out.
  for (var key in softStats) {
    if (typeof softStats[key] === "number" && key.indexOf("Skipped") < 0) frameStats[key] = (frameStats[key] || 0) + softStats[key];
  }
  frameStats.computeParticleSoftDrawCalls = softStats.computeParticleDrawCalls || 0;
}
