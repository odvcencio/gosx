// Opt-in HDR mip bloom. Scratch targets avoid framebuffer feedback on upsample.
function createSceneWebGLMipBloom(host) {
  var gl = host.gl;
  var levels = [];
  var header = "#version 300 es\nprecision highp float;\nin vec2 v_uv;\nuniform sampler2D u_texture;\nout vec4 fragColor;\n";
  var prefilterSource = header + [
    "uniform float u_threshold;",
    "void main() {",
    "  vec3 color = clamp(texture(u_texture, v_uv).rgb, vec3(0.0), vec3(64.0));",
    "  float br = max(color.r, max(color.g, color.b));",
    "  float knee = u_threshold * 0.5;",
    "  float soft = clamp(br - u_threshold + knee, 0.0, 2.0 * knee);",
    "  soft = soft * soft / (4.0 * knee + 1e-4);",
    "  float contribution = max(soft, br - u_threshold) / max(br, 1e-4);",
    "  fragColor = vec4(color * contribution, 1.0);",
    "}",
  ].join("\n");
  // 13 bilinear taps: center, outer corners/axes, inner diagonals.
  // Weights sum to one: 1/8 + 4/32 + 4/16 + 4/8.
  var downsampleSource = header + [
    "uniform vec2 u_texelSize;",
    "vec3 tap(vec2 offset) { return texture(u_texture, v_uv + offset * u_texelSize).rgb; }",
    "void main() {",
    "  vec3 color = tap(vec2(0.0)) * 0.125;",
    "  color += (tap(vec2(-2.0, -2.0)) + tap(vec2(2.0, -2.0)) + tap(vec2(-2.0, 2.0)) + tap(vec2(2.0, 2.0))) * 0.03125;",
    "  color += (tap(vec2(-2.0, 0.0)) + tap(vec2(2.0, 0.0)) + tap(vec2(0.0, -2.0)) + tap(vec2(0.0, 2.0))) * 0.0625;",
    "  color += (tap(vec2(-1.0, -1.0)) + tap(vec2(1.0, -1.0)) + tap(vec2(-1.0, 1.0)) + tap(vec2(1.0, 1.0))) * 0.125;",
    "  fragColor = vec4(color, 1.0);",
    "}",
  ].join("\n");
  var upsampleSource = header + [
    "uniform sampler2D u_bloomTexture;",
    "uniform vec2 u_texelSize;",
    "vec3 tap(vec2 offset) { return texture(u_bloomTexture, v_uv + offset * u_texelSize).rgb; }",
    "void main() {",
    "  vec3 color = tap(vec2(0.0)) * 4.0;",
    "  color += (tap(vec2(-1.0, 0.0)) + tap(vec2(1.0, 0.0)) + tap(vec2(0.0, -1.0)) + tap(vec2(0.0, 1.0))) * 2.0;",
    "  color += tap(vec2(-1.0, -1.0)) + tap(vec2(1.0, -1.0)) + tap(vec2(-1.0, 1.0)) + tap(vec2(1.0, 1.0));",
    "  fragColor = vec4(texture(u_texture, v_uv).rgb + color / 16.0, 1.0);",
    "}",
  ].join("\n");

  function number(value, fallback) {
    return typeof value === "number" && isFinite(value) ? value : fallback;
  }

  function dispose() {
    for (var i = 0; i < levels.length; i++) {
      disposeScenePostFBO(gl, levels[i].base);
      if (levels[i].scratch) disposeScenePostFBO(gl, levels[i].scratch);
    }
    levels = [];
  }

  function ensureLevels(w, h) {
    if (levels.length && levels[0].base.width === w && levels[0].base.height === h) return;
    dispose();
    for (var i = 0; i < 6; i++) {
      var nextW = Math.floor(w / 2), nextH = Math.floor(h / 2);
      var last = i === 5 || Math.min(nextW, nextH) < 8;
      levels.push({ base: createScenePostFBO(gl, w, h, false), scratch: last ? null : createScenePostFBO(gl, w, h, false) });
      if (last) break;
      w = nextW; h = nextH;
    }
  }

  function draw(prog, input, target) {
    host.beginPostPass(prog, input, target.fbo, target.width, target.height);
  }

  function bloomTexture(prog, texture) {
    gl.activeTexture(gl.TEXTURE1);
    gl.bindTexture(gl.TEXTURE_2D, texture);
    gl.uniform1i(gl.getUniformLocation(prog.program, "u_bloomTexture"), 1);
  }

  return {
    dispose: dispose,
    apply: function(args) {
      var effect = args.effect;
      var prefilter = host.getProgram("bloomMipPrefilter", prefilterSource);
      var downsample = host.getProgram("bloomMipDownsample", downsampleSource);
      var upsample = host.getProgram("bloomMipUpsample", upsampleSource);
      var composite = host.getProgram("bloomComposite", host.compositeSource);
      if (!prefilter || !downsample || !upsample || !composite) return args.input;
      var scale = effect.scale > 0 && effect.scale <= 1 ? effect.scale : 0.5;
      ensureLevels(Math.max(1, Math.floor(args.width * scale)), Math.max(1, Math.floor(args.height * scale)));
      var radius = Math.min(2, Math.max(0.25, (effect.radius > 0 ? number(effect.radius, 5) : 5) / 5));
      draw(prefilter, args.source || args.input, levels[0].base);
      gl.uniform1f(gl.getUniformLocation(prefilter.program, "u_threshold"), Math.max(0, number(effect.threshold, 0.8)));
      drawSceneFullscreenQuad(gl, host.quad.vao);
      for (var i = 1; i < levels.length; i++) {
        var source = levels[i - 1].base;
        draw(downsample, source.colorTex, levels[i].base);
        gl.uniform2f(gl.getUniformLocation(downsample.program, "u_texelSize"), 1 / source.width, 1 / source.height);
        drawSceneFullscreenQuad(gl, host.quad.vao);
      }
      var smaller = levels[levels.length - 1].base;
      for (var j = levels.length - 2; j >= 0; j--) {
        var level = levels[j];
        draw(upsample, level.base.colorTex, level.scratch);
        bloomTexture(upsample, smaller.colorTex);
        gl.uniform2f(gl.getUniformLocation(upsample.program, "u_texelSize"), radius / smaller.width, radius / smaller.height);
        drawSceneFullscreenQuad(gl, host.quad.vao);
        smaller = level.scratch;
      }
      host.beginPostPass(composite, args.input, args.target ? args.target.fbo : null, args.passWidth, args.passHeight);
      bloomTexture(composite, smaller.colorTex);
      gl.uniform1f(gl.getUniformLocation(composite.program, "u_intensity"), number(effect.intensity, 0.5));
      drawSceneFullscreenQuad(gl, host.quad.vao);
      return args.target ? args.target.colorTex : null;
    },
  };
}
