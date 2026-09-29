// Opt-in HDR mip bloom. Each upsample writes a distinct scratch texture.
function createSceneWebGPUMipBloom(host) {
  var device = host.device;
  var levels = [];
  var chains = new Map();
  var textureHeader = "@group(0) @binding(0) var inputTex: texture_2d<f32>;\n@group(0) @binding(1) var inputSamp: sampler;\n";
  var paramsHeader = "struct MipParams { value: f32, _pad0: f32, _pad1: f32, _pad2: f32 };\n";
  var prefilterSource = paramsHeader + textureHeader + [
    "@group(0) @binding(2) var<uniform> params: MipParams;",
    "@fragment fn fragmentMain(@location(0) uv: vec2f) -> @location(0) vec4f {",
    "  let color = clamp(textureSample(inputTex, inputSamp, uv).rgb, vec3f(0.0), vec3f(64.0));",
    "  let br = max(color.r, max(color.g, color.b));",
    "  let knee = params.value * 0.5;",
    "  let soft = clamp(br - params.value + knee, 0.0, 2.0 * knee);",
    "  let curved = soft * soft / (4.0 * knee + 1e-4);",
    "  let contribution = max(curved, br - params.value) / max(br, 1e-4);",
    "  return vec4f(color * contribution, 1.0);",
    "}",
  ].join("\n");
  // Same normalized 13-tap footprint as the WebGL path, in source texels.
  var downsampleSource = textureHeader + [
    "fn tap(uv: vec2f, offset: vec2f) -> vec3f {",
    "  return textureSample(inputTex, inputSamp, uv + offset / vec2f(textureDimensions(inputTex))).rgb;",
    "}",
    "@fragment fn fragmentMain(@location(0) uv: vec2f) -> @location(0) vec4f {",
    "  var color = tap(uv, vec2f(0.0)) * 0.125;",
    "  color += (tap(uv, vec2f(-2.0, -2.0)) + tap(uv, vec2f(2.0, -2.0)) + tap(uv, vec2f(-2.0, 2.0)) + tap(uv, vec2f(2.0, 2.0))) * 0.03125;",
    "  color += (tap(uv, vec2f(-2.0, 0.0)) + tap(uv, vec2f(2.0, 0.0)) + tap(uv, vec2f(0.0, -2.0)) + tap(uv, vec2f(0.0, 2.0))) * 0.0625;",
    "  color += (tap(uv, vec2f(-1.0, -1.0)) + tap(uv, vec2f(1.0, -1.0)) + tap(uv, vec2f(-1.0, 1.0)) + tap(uv, vec2f(1.0, 1.0))) * 0.125;",
    "  return vec4f(color, 1.0);",
    "}",
  ].join("\n");
  var upsampleSource = paramsHeader + textureHeader + [
    "@group(0) @binding(2) var bloomTex: texture_2d<f32>;",
    "@group(0) @binding(3) var bloomSamp: sampler;",
    "@group(0) @binding(4) var<uniform> params: MipParams;",
    "fn tap(uv: vec2f, offset: vec2f) -> vec3f {",
    "  let step = params.value / vec2f(textureDimensions(bloomTex));",
    "  return textureSample(bloomTex, bloomSamp, uv + offset * step).rgb;",
    "}",
    "@fragment fn fragmentMain(@location(0) uv: vec2f) -> @location(0) vec4f {",
    "  var color = tap(uv, vec2f(0.0)) * 4.0;",
    "  color += (tap(uv, vec2f(-1.0, 0.0)) + tap(uv, vec2f(1.0, 0.0)) + tap(uv, vec2f(0.0, -1.0)) + tap(uv, vec2f(0.0, 1.0))) * 2.0;",
    "  color += tap(uv, vec2f(-1.0, -1.0)) + tap(uv, vec2f(1.0, -1.0)) + tap(uv, vec2f(-1.0, 1.0)) + tap(uv, vec2f(1.0, 1.0));",
    "  return vec4f(textureSample(inputTex, inputSamp, uv).rgb + color / 16.0, 1.0);",
    "}",
  ].join("\n");

  function number(value, fallback) {
    return typeof value === "number" && isFinite(value) ? value : fallback;
  }

  function disposeLevels() {
    for (var i = 0; i < levels.length; i++) {
      levels[i].base.texture.destroy();
      if (levels[i].scratch) levels[i].scratch.texture.destroy();
    }
    levels = [];
  }

  function dispose() {
    chains.forEach(function(chain) { levels = chain; disposeLevels(); });
    chains.clear();
  }

  function target(w, h, label) {
    var texture = device.createTexture({ label: label, size: [w, h, 1], format: host.format,
      usage: GPUTextureUsage.RENDER_ATTACHMENT | GPUTextureUsage.TEXTURE_BINDING });
    return { texture: texture, view: texture.createView(), width: w, height: h };
  }

  function ensureLevels(w, h, index) {
    levels = chains.get(index) || [];
    if (levels.length && levels[0].base.width === w && levels[0].base.height === h) return;
    disposeLevels();
    chains.set(index, levels);
    for (var i = 0; i < 6; i++) {
      var nextW = Math.floor(w / 2), nextH = Math.floor(h / 2);
      var last = i === 5 || Math.min(nextW, nextH) < 8;
      levels.push({ base: target(w, h, "gosx-bloom-mip-base-" + i), scratch: last ? null : target(w, h, "gosx-bloom-mip-scratch-" + i) });
      if (last) break;
      w = nextW; h = nextH;
    }
  }

  function pass(args) {
    var dual = !!args.bloom;
    var layout = dual ? host.compositeLayout() : host.paramsLayout();
    var pipeline = host.getPipeline(args.name, args.source, layout);
    // Distinct pass buffers remain stable until their encoded frame executes.
    var buffer = host.getParamBuffer(args.bufferName || args.name, 16);
    device.queue.writeBuffer(buffer, 0, new Float32Array([args.value || 0, 0, 0, 0]));
    var entries = [{ binding: 0, resource: args.input }, { binding: 1, resource: host.sampler }];
    if (dual) entries.push({ binding: 2, resource: args.bloom }, { binding: 3, resource: host.sampler });
    entries.push({ binding: dual ? 4 : 2, resource: { buffer: buffer } });
    var group = device.createBindGroup({ layout: layout, entries: entries });
    host.fullscreenPass(args.encoder, pipeline, group, args.output);
  }

  return {
    dispose: dispose,
    apply: function(args) {
      var effect = args.effect;
      var scale = effect.scale > 0 && effect.scale <= 1 ? effect.scale : 0.5;
      ensureLevels(Math.max(1, Math.floor(args.width * scale)), Math.max(1, Math.floor(args.height * scale)), args.index);
      var radius = Math.min(2, Math.max(0.25, (effect.radius > 0 ? number(effect.radius, 5) : 5) / 5));
      pass({ encoder: args.encoder, name: "bloomMipPrefilter", bufferName: "bloomMipPrefilter-" + args.index, source: prefilterSource, input: args.input,
        output: levels[0].base.view, value: Math.max(0, number(effect.threshold, 0.8)) });
      for (var i = 1; i < levels.length; i++) {
        pass({ encoder: args.encoder, name: "bloomMipDownsample", source: downsampleSource, input: levels[i - 1].base.view,
          output: levels[i].base.view });
      }
      var smaller = levels[levels.length - 1].base;
      for (var j = levels.length - 2; j >= 0; j--) {
        var level = levels[j];
        pass({ encoder: args.encoder, name: "bloomMipUpsample", bufferName: "bloomMipUpsample-" + args.index, source: upsampleSource, input: level.base.view,
          bloom: smaller.view, output: level.scratch.view, value: radius });
        smaller = level.scratch;
      }
      pass({ encoder: args.encoder, name: "bloomComposite", bufferName: "bloomMipComposite-" + args.index, source: host.compositeSource,
        input: args.input, bloom: smaller.view, output: args.output, value: number(effect.intensity, 0.5) });
      return args.output;
    },
  };
}
