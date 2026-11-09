// WebGL2 motion blur: the same four passes as motion-blur-wgpu.ts. The WebGL2
// renderer has no per-object velocity target, so only camera motion blurs
// here: each pixel reprojects through the previous frame's view-projection.
// Moving objects on a still camera are not blurred on this backend.
(() => {
  const fx = (window as any).__gosx_scene3d_depthfx_api;
  if (!fx || !fx.motionBlurPlan) return;

  const VERT = `#version 300 es
void main() { vec2 p = vec2(float((gl_VertexID << 1) & 2), float(gl_VertexID & 2)); gl_Position = vec4(p * 2.0 - 1.0, 0.0, 1.0); }`;
  const HEAD = `#version 300 es
precision highp float;
precision highp int;
precision highp sampler2D;
out vec4 fragColor;
`;

  const VELOCITY = HEAD + `
uniform sampler2D u_depth;
uniform mat4 u_invViewProj; uniform mat4 u_prevViewProj;
uniform vec3 u_eye; uniform vec3 u_forward;
uniform vec4 u_settings; uniform vec2 u_size;
void main() {
  ivec2 px = ivec2(gl_FragCoord.xy);
  float depth = texelFetch(u_depth, px, 0).r;
  vec2 uv = (gl_FragCoord.xy) / u_size;
  vec4 world = u_invViewProj * vec4(uv * 2.0 - 1.0, depth * 2.0 - 1.0, 1.0);
  world /= world.w;
  vec2 v = vec2(0.0);
  if (u_settings.z > 0.5) {
    vec4 prev = u_prevViewProj * vec4(world.xyz, 1.0);
    if (prev.w > 0.0) v = prev.xy / prev.w * 0.5 + 0.5 - uv;
  }
  vec2 vp = v * u_size * u_settings.x;
  float len = length(vp);
  if (len > u_settings.y) vp *= u_settings.y / len;
  if (len < 0.5) vp = vec2(0.0);
  fragColor = vec4(vp, -dot(world.xyz - u_eye, u_forward), 1.0); // view-space z: negative in front, as the gather expects
}`;

  const tileSource = (tile: number): string => HEAD + `
uniform sampler2D u_velocity;
void main() {
  ivec2 dims = textureSize(u_velocity, 0);
  ivec2 base = ivec2(gl_FragCoord.xy) * ${tile};
  vec2 best = vec2(0.0); float bestLen = 0.0;
  for (int y = 0; y < ${tile}; y++) for (int x = 0; x < ${tile}; x++) {
    ivec2 p = base + ivec2(x, y);
    if (p.x >= dims.x || p.y >= dims.y) continue;
    vec2 v = texelFetch(u_velocity, p, 0).xy;
    float l = dot(v, v);
    if (l > bestLen) { bestLen = l; best = v; }
  }
  fragColor = vec4(best, 0.0, 1.0);
}`;

  const NEIGHBOR = HEAD + `
uniform sampler2D u_tiles;
void main() {
  ivec2 dims = textureSize(u_tiles, 0);
  ivec2 c = ivec2(gl_FragCoord.xy);
  vec2 best = vec2(0.0); float bestLen = 0.0;
  for (int y = -1; y <= 1; y++) for (int x = -1; x <= 1; x++) {
    vec2 v = texelFetch(u_tiles, clamp(c + ivec2(x, y), ivec2(0), dims - ivec2(1)), 0).xy;
    float l = dot(v, v);
    if (l > bestLen) { bestLen = l; best = v; }
  }
  fragColor = vec4(best, 0.0, 1.0);
}`;

  const gatherSource = (tile: number, samples: number): string => HEAD + `
uniform sampler2D u_color; uniform sampler2D u_velocity; uniform sampler2D u_neighbor;
const int SAMPLES = ${samples};
float softDepth(float a, float b, float extent) { return clamp(1.0 - (a - b) / extent, 0.0, 1.0); }
float cone(float dist, float len) { return clamp(1.0 - dist / max(len, 0.0001), 0.0, 1.0); }
float cylinder(float dist, float len) { return 1.0 - smoothstep(0.95 * len, 1.05 * len, dist); }
void main() {
  ivec2 dims = textureSize(u_color, 0);
  ivec2 px = ivec2(gl_FragCoord.xy);
  vec4 c0 = texelFetch(u_color, px, 0);
  ivec2 tileDims = textureSize(u_neighbor, 0);
  vec2 vN = texelFetch(u_neighbor, clamp(px / ${tile}, ivec2(0), tileDims - ivec2(1)), 0).xy;
  float lenN = length(vN);
  if (lenN < 0.5) { fragColor = c0; return; }
  vec4 center = texelFetch(u_velocity, px, 0);
  vec2 vX = center.xy; float zX = center.z;
  float lenX = length(vX);
  vec2 pos = gl_FragCoord.xy;
  float jitter = fract(52.9829189 * fract(dot(gl_FragCoord.xy, vec2(0.06711056, 0.00583715)))) - 0.5;
  float extent = max(0.25, abs(zX) * 0.1);
  float w0 = 1.0 / max(lenX, 1.0) * float(SAMPLES) / 40.0;
  vec4 sum = c0 * w0; float weight = w0;
  for (int i = 0; i < SAMPLES; i++) {
    float t = (float(i) + 0.5 + jitter * 0.5) / float(SAMPLES) * 2.0 - 1.0;
    vec2 dir = ((i & 1) == 1 && lenX > 0.5) ? vX : vN;
    ivec2 q = clamp(ivec2(pos + dir * t), ivec2(0), dims - ivec2(1));
    vec4 s = texelFetch(u_velocity, q, 0);
    vec2 vY = s.xy; float zY = s.z;
    float dist = length(vec2(q) + 0.5 - pos);
    float f = softDepth(zX, zY, extent);
    float b = softDepth(zY, zX, extent);
    float lenY = length(vY);
    float alpha = f * cone(dist, lenY) + b * cone(dist, lenX) + cylinder(dist, lenY) * cylinder(dist, lenX) * 2.0;
    sum += texelFetch(u_color, q, 0) * alpha;
    weight += alpha;
  }
  fragColor = vec4(sum.rgb / max(weight, 0.0001), c0.a);
}`;

  function compile(gl: any, fragment: string): any {
    const make = (type: number, source: string): any => {
      const shader = gl.createShader(type);
      gl.shaderSource(shader, source); gl.compileShader(shader);
      if (!gl.getShaderParameter(shader, gl.COMPILE_STATUS)) throw new Error("motion blur shader: " + gl.getShaderInfoLog(shader));
      return shader;
    };
    const program = gl.createProgram();
    const vs = make(gl.VERTEX_SHADER, VERT), fs = make(gl.FRAGMENT_SHADER, fragment);
    gl.attachShader(program, vs); gl.attachShader(program, fs); gl.linkProgram(program);
    gl.deleteShader(vs); gl.deleteShader(fs);
    if (!gl.getProgramParameter(program, gl.LINK_STATUS)) throw new Error("motion blur program: " + gl.getProgramInfoLog(program));
    return program;
  }

  function createWebGLMotionBlur(gl: any) {
    const history = fx.createMotionHistory();
    const invVP = new Float32Array(16), eye = new Float32Array(3), forward = new Float32Array(3);
    const programs = new Map<string, any>();
    let vao: any = null;
    let targets: any = null;
    let failed = false, disposed = false;
    const supported = !!gl.getExtension("EXT_color_buffer_float");

    function program(key: string, source: string): any {
      let p = programs.get(key);
      if (!p) { p = compile(gl, source); programs.set(key, p); }
      return p;
    }
    function makeTarget(width: number, height: number, internal: number, format: number): any {
      const texture = gl.createTexture();
      gl.bindTexture(gl.TEXTURE_2D, texture);
      gl.texImage2D(gl.TEXTURE_2D, 0, internal, width, height, 0, format, gl.HALF_FLOAT, null);
      gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_MIN_FILTER, gl.NEAREST); gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_MAG_FILTER, gl.NEAREST);
      gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_WRAP_S, gl.CLAMP_TO_EDGE); gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_WRAP_T, gl.CLAMP_TO_EDGE);
      const fbo = gl.createFramebuffer();
      gl.bindFramebuffer(gl.FRAMEBUFFER, fbo);
      gl.framebufferTexture2D(gl.FRAMEBUFFER, gl.COLOR_ATTACHMENT0, gl.TEXTURE_2D, texture, 0);
      if (gl.checkFramebufferStatus(gl.FRAMEBUFFER) !== gl.FRAMEBUFFER_COMPLETE) throw new Error("motion blur target incomplete");
      return { texture, fbo, width, height };
    }
    function ensureTargets(w: number, h: number, tile: number): any {
      if (targets && targets.w === w && targets.h === h && targets.tile === tile) return targets;
      destroyTargets();
      const tw = Math.ceil(w / tile), th = Math.ceil(h / tile);
      targets = { w, h, tile, velocity: makeTarget(w, h, gl.RGBA16F, gl.RGBA), tileMax: makeTarget(tw, th, gl.RG16F, gl.RG), neighbor: makeTarget(tw, th, gl.RG16F, gl.RG) };
      return targets;
    }
    function destroyTargets(): void {
      if (!targets) return;
      for (const k of ["velocity", "tileMax", "neighbor"]) { gl.deleteTexture(targets[k].texture); gl.deleteFramebuffer(targets[k].fbo); }
      targets = null;
    }
    function bind(unit: number, texture: any, program: any, name: string): void {
      gl.activeTexture(gl.TEXTURE0 + unit); gl.bindTexture(gl.TEXTURE_2D, texture);
      gl.uniform1i(gl.getUniformLocation(program, name), unit);
    }
    function pass(target: any, width: number, height: number, program: any): void {
      gl.bindFramebuffer(gl.FRAMEBUFFER, target);
      gl.viewport(0, 0, width, height);
      gl.useProgram(program);
      gl.drawArrays(gl.TRIANGLES, 0, 3);
    }

    function apply(effect: any, inputTex: any, targetFBO: any, width: number, height: number, depthTex: any, frame: any): boolean {
      if (failed || disposed || !supported || !depthTex || !frame || !frame.viewProj) return false;
      try {
        const plan = fx.motionBlurPlan(effect, frame, height);
        const now = typeof performance !== "undefined" ? performance.now() : Date.now();
        if (!fx.invert(invVP, frame.viewProj)) return false;
        fx.eyeAndForward(invVP, -1, 1, eye, forward);
        const hasPrev = history.begin(now, eye, frame.viewProj); // every frame, so a skipped one still advances history
        const prevVP = history.prev;
        if (plan.skip || !hasPrev) return false;
        const t = ensureTargets(width, height, plan.tile);
        fx.stats.motionBlur.frames++;
        if (!vao) vao = gl.createVertexArray();
        gl.bindVertexArray(vao);
        gl.disable(gl.DEPTH_TEST); gl.disable(gl.BLEND); gl.disable(gl.CULL_FACE);

        const vel = program("velocity", VELOCITY);
        gl.useProgram(vel);
        bind(0, depthTex, vel, "u_depth");
        gl.uniformMatrix4fv(gl.getUniformLocation(vel, "u_invViewProj"), false, invVP);
        gl.uniformMatrix4fv(gl.getUniformLocation(vel, "u_prevViewProj"), false, prevVP);
        gl.uniform3fv(gl.getUniformLocation(vel, "u_eye"), eye); gl.uniform3fv(gl.getUniformLocation(vel, "u_forward"), forward);
        gl.uniform4f(gl.getUniformLocation(vel, "u_settings"), plan.strength, plan.maxBlur, 1, 0);
        gl.uniform2f(gl.getUniformLocation(vel, "u_size"), width, height);
        pass(t.velocity.fbo, width, height, vel);

        const tile = program("tile:" + plan.tile, tileSource(plan.tile));
        gl.useProgram(tile); bind(0, t.velocity.texture, tile, "u_velocity");
        pass(t.tileMax.fbo, t.tileMax.width, t.tileMax.height, tile);
        const neighbor = program("neighbor", NEIGHBOR);
        gl.useProgram(neighbor); bind(0, t.tileMax.texture, neighbor, "u_tiles");
        pass(t.neighbor.fbo, t.neighbor.width, t.neighbor.height, neighbor);
        const gather = program("gather:" + plan.tile + ":" + plan.samples, gatherSource(plan.tile, plan.samples));
        gl.useProgram(gather);
        bind(0, inputTex, gather, "u_color"); bind(1, t.velocity.texture, gather, "u_velocity"); bind(2, t.neighbor.texture, gather, "u_neighbor");
        pass(targetFBO ? targetFBO.fbo : null, width, height, gather);

        for (let unit = 2; unit >= 0; unit--) { gl.activeTexture(gl.TEXTURE0 + unit); gl.bindTexture(gl.TEXTURE_2D, null); }
        gl.activeTexture(gl.TEXTURE0);
        gl.bindVertexArray(null);
        return true;
      } catch (error: any) {
        failed = true;
        console.error("[gosx] motion blur disabled:", error && error.message || error);
        return false;
      }
    }

    return {
      apply,
      dispose() {
        disposed = true;
        destroyTargets();
        programs.forEach((p: any) => gl.deleteProgram(p));
        if (vao) gl.deleteVertexArray(vao);
      },
    };
  }

  fx.webgl.motionBlur = createWebGLMotionBlur;
})();
