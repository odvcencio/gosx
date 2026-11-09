// WebGL2 soft particles. A blended builtin compute-particle sprite fades over
// material.softDistance world units as it nears the opaque depth behind it.
//
// WebGL2 forbids sampling a texture that is attached to the bound framebuffer,
// so the first soft sprite of a frame blits the scene depth into a depth-only
// copy once. This needs an offscreen scene target: every PBR offscreen target
// comes from createScenePostFBO and stores DEPTH_COMPONENT24, which matches the
// copy. A scene drawn straight to the canvas keeps hard sprites (the default
// framebuffer depth format is not guaranteed to match).
//
// The fade shader wraps the builtin points fragment shader rather than editing
// its expressions, because the bundle builder compacts WebGL shader text.

// sceneWebGLSoftParticleInverseDistance returns 1 / softDistance for a compute
// points entry that may soften, else 0.
function sceneWebGLSoftParticleInverseDistance(entry: any): number {
  if (!entry || !entry._computeParticlesSynthetic) return 0;
  var distance = Number(entry.softDistance);
  if (!(distance > 0) || !isFinite(distance)) return 0;
  var blend = typeof entry.blendMode === "string" ? entry.blendMode.toLowerCase() : "";
  if (blend !== "alpha" && blend !== "additive") return 0;
  if (entry.customVertex && entry.customFragment) return 0;
  return 1 / distance;
}

function sceneWebGLSoftParticleFragment(base: string): string {
  var fade = [
    "uniform highp sampler2D u_softDepth;",
    "uniform float u_softInverse;",
    "uniform mat4 u_projectionMatrix;",
    "float softParticleViewDistance(float z) {",
    "    mat4 m = u_projectionMatrix;",
    "    return -(m[3][2] - z * m[3][3]) / (z * m[2][3] - m[2][2]);",
    "}",
    "float softParticleFade() {",
    "    if (u_softInverse <= 0.0) return 1.0;",
    "    float sceneDepth = texelFetch(u_softDepth, ivec2(gl_FragCoord.xy), 0).r * 2.0 - 1.0;",
    "    float spriteDepth = gl_FragCoord.z * 2.0 - 1.0;",
    "    float gap = softParticleViewDistance(sceneDepth) - softParticleViewDistance(spriteDepth);",
    "    return clamp(gap * u_softInverse, 0.0, 1.0);",
    "}",
  ].join("\n");
  return base.replace("out vec4 fragColor;", "out vec4 fragColor;\n" + fade)
    .replace("void main() {", "void pointsMain() {")
    + "\nvoid main() { pointsMain(); fragColor.a *= softParticleFade(); }\n";
}

// createSceneWebGLSoftParticles owns the soft program and the depth copy for
// one PBR renderer.
function createSceneWebGLSoftParticles(gl: any) {
  var program: any = null, failed = false;
  var copy: any = null, width = 0, height = 0;
  var framebuffer: any = null, frameW = 0, frameH = 0, captured = false, used = false;
  var unit = 0;
  function releaseCopy() {
    if (copy) { gl.deleteFramebuffer(copy.fbo); gl.deleteTexture(copy.depthTex); }
    copy = null; width = 0; height = 0;
  }
  function ensureCopy(w: number, h: number) {
    if (copy && width === w && height === h) return copy;
    releaseCopy();
    var depthTex = gl.createTexture();
    gl.bindTexture(gl.TEXTURE_2D, depthTex);
    gl.texImage2D(gl.TEXTURE_2D, 0, gl.DEPTH_COMPONENT24, w, h, 0, gl.DEPTH_COMPONENT, gl.UNSIGNED_INT, null);
    gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_MIN_FILTER, gl.NEAREST);
    gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_MAG_FILTER, gl.NEAREST);
    gl.bindTexture(gl.TEXTURE_2D, null);
    var fbo = gl.createFramebuffer();
    gl.bindFramebuffer(gl.FRAMEBUFFER, fbo);
    gl.framebufferTexture2D(gl.FRAMEBUFFER, gl.DEPTH_ATTACHMENT, gl.TEXTURE_2D, depthTex, 0);
    var complete = typeof gl.checkFramebufferStatus !== "function" || gl.checkFramebufferStatus(gl.FRAMEBUFFER) === gl.FRAMEBUFFER_COMPLETE;
    gl.bindFramebuffer(gl.FRAMEBUFFER, framebuffer);
    copy = { fbo: fbo, depthTex: depthTex };
    width = w; height = h;
    if (!complete) { releaseCopy(); failed = true; }
    return copy;
  }
  function capture() {
    if (captured) return copy;
    var target = ensureCopy(frameW, frameH);
    if (!target) return null;
    gl.bindFramebuffer(gl.READ_FRAMEBUFFER, framebuffer);
    gl.bindFramebuffer(gl.DRAW_FRAMEBUFFER, target.fbo);
    gl.blitFramebuffer(0, 0, frameW, frameH, 0, 0, frameW, frameH, gl.DEPTH_BUFFER_BIT, gl.NEAREST);
    gl.bindFramebuffer(gl.FRAMEBUFFER, framebuffer);
    captured = true;
    return target;
  }
  function ensureProgram() {
    if (program || failed) return program;
    program = scenePBRCreateShaderProgram(gl, SCENE_POINTS_VERTEX_SOURCE, sceneWebGLSoftParticleFragment(SCENE_POINTS_FRAGMENT_SOURCE), "Soft points shader", function(linked: any, vertexShader: any, fragmentShader: any) {
      var info: any = scenePointsProgramInfo(gl, linked, vertexShader, fragmentShader);
      info.uniforms.softDepth = gl.getUniformLocation(linked, "u_softDepth");
      info.uniforms.softInverse = gl.getUniformLocation(linked, "u_softInverse");
      return info;
    });
    if (!program) failed = true;
    // The highest fragment unit is free of material and IBL bindings.
    unit = Math.max(0, Math.min(31, (typeof gl.getParameter === "function" ? Number(gl.getParameter(gl.MAX_TEXTURE_IMAGE_UNITS)) || 16 : 16) - 1));
    return program;
  }
  return {
    // beginFrame records the target the compute sprites draw into this frame.
    beginFrame: function(target: any, w: number, h: number) {
      framebuffer = target && target.framebuffer || null; frameW = w; frameH = h; captured = false; used = false;
    },
    // program returns the soft program for an eligible entry, or null so the
    // caller draws the hard builtin sprite.
    program: function(entry: any) {
      if (!framebuffer || failed || !gl.blitFramebuffer) return null;
      if (!(sceneWebGLSoftParticleInverseDistance(entry) > 0)) return null;
      var info = ensureProgram();
      if (!info || scenePBRProgramFailed(info.program)) { failed = !!info; return null; }
      // A program still linking draws the hard sprite this frame.
      if (!scenePBRProgramReady(gl, info.program)) return null;
      return capture() ? info : null;
    },
    // bind uploads the depth copy and fade distance for one soft draw.
    bind: function(info: any, entry: any) {
      gl.activeTexture(gl.TEXTURE0 + unit);
      gl.bindTexture(gl.TEXTURE_2D, copy.depthTex);
      gl.uniform1i(info.uniforms.softDepth, unit);
      gl.uniform1f(info.uniforms.softInverse, sceneWebGLSoftParticleInverseDistance(entry));
      gl.activeTexture(gl.TEXTURE0);
      used = true;
    },
    used: function() { return used; },
    available: function() { return !!framebuffer && !failed; },
    dispose: function() {
      releaseCopy();
      if (program) { gl.deleteProgram(program.program); gl.deleteShader(program.vertexShader); gl.deleteShader(program.fragmentShader); }
      program = null; failed = false;
    },
  };
}
