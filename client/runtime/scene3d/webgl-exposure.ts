// WebGL2 auto exposure (Environment.AutoExposure), the mip-reduction twin of
// the WebGPU compute path. Same color contract: the adapted value is a linear
// multiplier on the HDR scene target, applied before the post chain.
//
// 1. Log2 luminance of the scene target renders into a 64 x 64 R16F texture.
// 2. generateMipmap averages it down to the 1 x 1 level.
// 3. A 1 x 1 pass adapts toward key / average (ping-pong between two 1 x 1
//    textures), exponential at speedUp or speedDown per second.
// 4. A fullscreen pass multiplies the scene target by the adapted value with
//    blending (dst * src). Nothing is read back to the CPU.
// Needs EXT_color_buffer_float for the R16F targets and an offscreen scene
// target; without either the scene keeps its authored exposure.

const SCENE_AUTO_EXPOSURE_LUMINANCE_SOURCE = `#version 300 es
precision highp float;
in vec2 v_uv;
uniform sampler2D u_scene;
out vec4 fragColor;
void main() {
    vec3 c = texture(u_scene, v_uv).rgb;
    fragColor = vec4(log2(max(dot(c, vec3(0.2126, 0.7152, 0.0722)), 0.0001)), 0.0, 0.0, 1.0);
}`;

const SCENE_AUTO_EXPOSURE_ADAPT_SOURCE = `#version 300 es
precision highp float;
in vec2 v_uv;
uniform sampler2D u_luminance;
uniform sampler2D u_previous;
uniform vec4 u_settings;
uniform vec4 u_rates;
uniform float u_level;
out vec4 fragColor;
void main() {
    float average = exp2(textureLod(u_luminance, vec2(0.5), u_level).r);
    float target = clamp(u_settings.z / max(average, 0.0001) * exp2(u_settings.w), u_settings.x, u_settings.y);
    float previous = texture(u_previous, vec2(0.5)).r;
    float current = (u_rates.w > 0.5 || !(previous > 0.0)) ? target : previous;
    float rate = target > current ? u_rates.x : u_rates.y;
    fragColor = vec4(current + (target - current) * (1.0 - exp(-max(u_rates.z, 0.0) * rate)), average, 0.0, 1.0);
}`;

const SCENE_AUTO_EXPOSURE_APPLY_SOURCE = `#version 300 es
precision highp float;
in vec2 v_uv;
uniform sampler2D u_exposure;
out vec4 fragColor;
void main() {
    fragColor = vec4(vec3(texture(u_exposure, vec2(0.5)).r), 1.0);
}`;

function sceneWebGLAutoExposureSettings(raw: any) {
  if (!raw || typeof raw !== "object") return null;
  var positive = function(value: any, fallback: number) { var n = Number(value); return isFinite(n) && n > 0 ? n : fallback; };
  var low = positive(raw.minExposure, 0.1), high = positive(raw.maxExposure, 8), comp = Number(raw.compensation);
  return { minExposure: Math.min(low, high), maxExposure: Math.max(low, high), key: positive(raw.key, 0.18),
    speedUp: positive(raw.speedUp, 3), speedDown: positive(raw.speedDown, 1), compensation: isFinite(comp) ? Math.max(-8, Math.min(8, comp)) : 0 };
}

function createSceneWebGLAutoExposure(gl: any) {
  var programs: any = null, failed = false, quad: any = null;
  var luminance: any = null, adapted: any[] = [], index = 0, lastAt = -1, previousEye: number[] | null = null, active = false;
  function target(w: number, h: number, levels: number) {
    var texture = gl.createTexture();
    gl.bindTexture(gl.TEXTURE_2D, texture);
    gl.texStorage2D(gl.TEXTURE_2D, levels, gl.R16F, w, h);
    gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_MIN_FILTER, levels > 1 ? gl.LINEAR_MIPMAP_LINEAR : gl.NEAREST);
    gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_MAG_FILTER, gl.NEAREST);
    var fbo = gl.createFramebuffer();
    gl.bindFramebuffer(gl.FRAMEBUFFER, fbo);
    gl.framebufferTexture2D(gl.FRAMEBUFFER, gl.COLOR_ATTACHMENT0, gl.TEXTURE_2D, texture, 0);
    return { texture: texture, fbo: fbo };
  }
  function ensure() {
    if (programs || failed) return !!programs;
    if (!gl.getExtension("EXT_color_buffer_float") || typeof gl.texStorage2D !== "function") { failed = true; return false; }
    programs = { luminance: createScenePostProgram(gl, SCENE_AUTO_EXPOSURE_LUMINANCE_SOURCE), adapt: createScenePostProgram(gl, SCENE_AUTO_EXPOSURE_ADAPT_SOURCE),
      apply: createScenePostProgram(gl, SCENE_AUTO_EXPOSURE_APPLY_SOURCE) };
    if (!programs.luminance || !programs.adapt || !programs.apply) { programs = null; failed = true; return false; }
    quad = createSceneFullscreenQuad(gl);
    luminance = target(64, 64, 7);
    adapted = [target(1, 1, 1), target(1, 1, 1)];
    return true;
  }
  function ready() {
    return scenePBRProgramReady(gl, programs.luminance.program) && scenePBRProgramReady(gl, programs.adapt.program) && scenePBRProgramReady(gl, programs.apply.program);
  }
  function bindTexture(unit: number, texture: any, program: any, name: string) {
    gl.activeTexture(gl.TEXTURE0 + unit);
    gl.bindTexture(gl.TEXTURE_2D, texture);
    gl.uniform1i(gl.getUniformLocation(program, name), unit);
  }
  return {
    // apply measures, adapts and multiplies the scene target. It returns false
    // and changes nothing unless the scene asked for auto exposure.
    apply: function(renderTarget: any, w: number, h: number, environment: any, camera: any): boolean {
      var settings = sceneWebGLAutoExposureSettings(environment && environment.autoExposure);
      if (!settings || !renderTarget || !renderTarget.framebuffer || !renderTarget.colorTexture || !ensure() || !ready()) { active = false; return false; }
      var now = typeof performance !== "undefined" ? performance.now() : Date.now();
      var eye = [sceneNumber(camera && camera.x, 0), sceneNumber(camera && camera.y, 0), sceneNumber(camera && camera.z, 0)];
      var reset = !active || !previousEye || Math.hypot(eye[0] - previousEye[0], eye[1] - previousEye[1], eye[2] - previousEye[2]) > 2;
      var dt = lastAt < 0 || now - lastAt > 250 ? 0 : (now - lastAt) / 1000;
      previousEye = eye; lastAt = now; active = true;
      gl.disable(gl.BLEND); gl.disable(gl.DEPTH_TEST);
      gl.bindFramebuffer(gl.FRAMEBUFFER, luminance.fbo); gl.viewport(0, 0, 64, 64);
      gl.useProgram(programs.luminance.program);
      bindTexture(0, renderTarget.colorTexture, programs.luminance.program, "u_scene");
      drawSceneFullscreenQuad(gl, quad.vao);
      gl.bindTexture(gl.TEXTURE_2D, luminance.texture); gl.generateMipmap(gl.TEXTURE_2D);
      var next = adapted[index % 2], previous = adapted[(index + 1) % 2];
      gl.bindFramebuffer(gl.FRAMEBUFFER, next.fbo); gl.viewport(0, 0, 1, 1);
      gl.useProgram(programs.adapt.program);
      bindTexture(0, luminance.texture, programs.adapt.program, "u_luminance");
      bindTexture(1, previous.texture, programs.adapt.program, "u_previous");
      gl.uniform4f(gl.getUniformLocation(programs.adapt.program, "u_settings"), settings.minExposure, settings.maxExposure, settings.key, settings.compensation);
      gl.uniform4f(gl.getUniformLocation(programs.adapt.program, "u_rates"), settings.speedUp, settings.speedDown, dt, reset ? 1 : 0);
      gl.uniform1f(gl.getUniformLocation(programs.adapt.program, "u_level"), 6);
      drawSceneFullscreenQuad(gl, quad.vao);
      // Multiply only color attachment 0; the specular MRT target keeps its value.
      gl.bindFramebuffer(gl.FRAMEBUFFER, renderTarget.framebuffer); gl.viewport(0, 0, w, h);
      var mrt = typeof gl.getParameter === "function" && gl.getParameter(gl.DRAW_BUFFER1) === gl.COLOR_ATTACHMENT1;
      if (mrt) gl.drawBuffers([gl.COLOR_ATTACHMENT0, gl.NONE]);
      gl.useProgram(programs.apply.program);
      bindTexture(0, next.texture, programs.apply.program, "u_exposure");
      gl.enable(gl.BLEND); gl.blendFuncSeparate(gl.ZERO, gl.SRC_COLOR, gl.ZERO, gl.ONE);
      drawSceneFullscreenQuad(gl, quad.vao);
      gl.disable(gl.BLEND);
      if (mrt) gl.drawBuffers([gl.COLOR_ATTACHMENT0, gl.COLOR_ATTACHMENT1]);
      gl.activeTexture(gl.TEXTURE0);
      index += 1;
      return true;
    },
    dispose: function() {
      for (var t of [luminance].concat(adapted)) { if (t) { gl.deleteTexture(t.texture); gl.deleteFramebuffer(t.fbo); } }
      if (programs) for (var key in programs) gl.deleteProgram(programs[key].program);
      if (quad) { gl.deleteVertexArray(quad.vao); if (quad.vbo) gl.deleteBuffer(quad.vbo); }
      programs = null; luminance = null; adapted = []; quad = null; failed = false; active = false;
    },
  };
}
