// SMAA on WebGL2: the three passes smaa.ts describes, in GLSL ES 3.00, the same
// arithmetic as smaa-wgpu.ts. Framebuffer rows run bottom to top here, so the row
// above a pixel is y + 1 (UP).
(() => {
  const w = window as any;
  const api = w.__gosx_scene3d_smaa_api;
  if (!api || api.webgl) return;

  const VERTEX = `#version 300 es
void main() {
  vec2 c = vec2(float((gl_VertexID << 1) & 2), float(gl_VertexID & 2));
  gl_Position = vec4(c * 2.0 - 1.0, 0.0, 1.0);
}`;
  const HEAD = `#version 300 es
precision highp float;
precision highp int;
const ivec2 UP = ivec2(0, 1);
uniform vec3 u_params;
out vec4 fragColor;
`;

  const EDGES = HEAD + `uniform sampler2D u_src;
float luma(ivec2 p) {
  vec3 c = texelFetch(u_src, clamp(p, ivec2(0), textureSize(u_src, 0) - 1), 0).rgb;
  return dot(clamp(c, 0.0, 1.0), vec3(0.2126, 0.7152, 0.0722));
}
void main() {
  ivec2 p = ivec2(gl_FragCoord.xy);
  float c = luma(p), l = luma(p + ivec2(-1, 0)), t = luma(p + UP);
  vec2 d = abs(vec2(c - l, c - t));
  vec2 e = step(vec2(u_params.x), d);
  if (e.x + e.y == 0.0) { fragColor = vec4(0.0); return; }
  float r = abs(c - luma(p + ivec2(1, 0))), b = abs(c - luma(p - UP));
  float m = max(max(max(d.x, d.y), max(r, b)), max(abs(l - luma(p + ivec2(-2, 0))), abs(t - luma(p + 2 * UP))));
  e *= step(vec2(m), 2.0 * d);
  fragColor = vec4(e, 0.0, 1.0);
}`;

  const WEIGHTS = HEAD + `uniform sampler2D u_edges;
ivec2 dims;
vec2 E(ivec2 p) {
  if (any(lessThan(p, ivec2(0))) || any(greaterThanEqual(p, dims))) return vec2(0.0);
  return texelFetch(u_edges, p, 0).rg;
}
float ch(vec2 v, int k) { return k == 1 ? v.y : v.x; }
float endHeight(float cp, float cq, float kp, float kq) {
  if (cp * cq > 0.0) return 0.0;
  return 0.5 * (cq * (kq > 0.0 ? u_params.z : 1.0) - cp * (kp > 0.0 ? u_params.z : 1.0));
}
vec2 walk(ivec2 p, int s, int k) {
  ivec2 a = k == 1 ? ivec2(1, 0) : -UP;
  ivec2 q = k == 1 ? UP : ivec2(-1, 0);
  int n = int(u_params.y);
  int first = s < 0 ? 0 : 1;
  int d = 0;
  for (int i = 0; i <= 32; i++) {
    ivec2 b = p + a * (s * d + first);
    float cp = ch(E(b), 1 - k), cq = ch(E(b + q), 1 - k);
    if (cp + cq > 0.0) return vec2(float(d), endHeight(cp, cq, ch(E(b - q), 1 - k), ch(E(b + 2 * q), 1 - k)));
    if (d >= n || ch(E(p + a * (s * (d + 1))), k) == 0.0) break;
    d += 1;
  }
  return vec2(float(d), 0.0);
}
vec2 span(float x0, float x1, float h0, float h1) {
  float a = max(x0, 0.0), b = min(x1, 1.0);
  if (b <= a) return vec2(0.0);
  float v = (b - a) * (h0 + (h1 - h0) * (0.5 * (a + b) - x0) / (x1 - x0));
  return vec2(max(-v, 0.0), max(v, 0.0));
}
vec2 area(float d1, float d2, float h1, float h2) {
  float m = 0.5 * (d2 + 1.0 - d1);
  return span(-d1, m, h1, 0.0) + span(m, d2 + 1.0, 0.0, h2);
}
void main() {
  dims = textureSize(u_edges, 0);
  ivec2 p = ivec2(gl_FragCoord.xy);
  vec2 e = E(p);
  vec4 o = vec4(0.0);
  if (e.y > 0.0) { vec2 l = walk(p, -1, 1), r = walk(p, 1, 1); o.xy = area(l.x, r.x, l.y, r.y); }
  if (e.x > 0.0) { vec2 u = walk(p, -1, 0), v = walk(p, 1, 0); o.zw = area(u.x, v.x, u.y, v.y); }
  fragColor = o;
}`;

  // The last pass of a chain may draw a canvas larger than the chain; then each
  // fragment mixes the blended values of its four nearest chain pixels.
  const BLEND = HEAD + `uniform sampler2D u_src;
uniform sampler2D u_wts;
uniform vec2 u_out;
ivec2 dims;
vec4 lin(ivec2 p) {
  vec4 c = texelFetch(u_src, clamp(p, ivec2(0), dims - 1), 0);
  return vec4(pow(max(c.rgb, 0.0), vec3(2.2)), c.a);
}
vec4 W(ivec2 p) {
  if (any(lessThan(p, ivec2(0))) || any(greaterThanEqual(p, dims))) return vec4(0.0);
  return texelFetch(u_wts, p, 0);
}
vec4 resolve(ivec2 p) {
  p = clamp(p, ivec2(0), dims - 1);
  vec4 own = W(p);
  float up = own.x, left = own.z, down = W(p - UP).y, right = W(p + ivec2(1, 0)).w;
  if (up + left + down + right == 0.0) return lin(p);
  ivec2 a = UP;
  float wa = up, wb = down;
  if (max(left, right) > max(up, down)) { a = ivec2(-1, 0); wa = left; wb = right; }
  return lin(p) * (1.0 - wa - wb) + lin(p + a) * wa + lin(p - a) * wb;
}
void main() {
  dims = textureSize(u_src, 0);
  vec4 m;
  if (u_out == vec2(dims)) {
    m = resolve(ivec2(gl_FragCoord.xy));
  } else {
    vec2 t = gl_FragCoord.xy / u_out * vec2(dims) - 0.5;
    ivec2 i = ivec2(floor(t));
    vec2 f = fract(t);
    m = mix(mix(resolve(i), resolve(i + ivec2(1, 0)), f.x), mix(resolve(i + ivec2(0, 1)), resolve(i + ivec2(1, 1)), f.x), f.y);
  }
  fragColor = vec4(pow(max(m.rgb, 0.0), vec3(1.0 / 2.2)), m.a);
}`;

  function create(gl: WebGL2RenderingContext): any {
    const programs: Record<string, any> = {};
    const vao = gl.createVertexArray();
    let width = 0, height = 0, edges: any = null, weights: any = null, failed = false;

    function program(name: string, source: string): any {
      if (programs[name] !== undefined) return programs[name];
      const shader = (type: number, src: string): WebGLShader | null => {
        const s = gl.createShader(type);
        if (!s) return null;
        gl.shaderSource(s, src); gl.compileShader(s);
        if (!gl.getShaderParameter(s, gl.COMPILE_STATUS)) { console.error("[gosx] smaa " + name + ": " + gl.getShaderInfoLog(s)); gl.deleteShader(s); return null; }
        return s;
      };
      const vs = shader(gl.VERTEX_SHADER, VERTEX), fs = shader(gl.FRAGMENT_SHADER, source);
      let info: any = null;
      if (vs && fs) {
        const p = gl.createProgram();
        gl.attachShader(p, vs); gl.attachShader(p, fs); gl.linkProgram(p);
        if (gl.getProgramParameter(p, gl.LINK_STATUS)) {
          info = { program: p, loc: (u: string) => gl.getUniformLocation(p, u) };
        } else console.error("[gosx] smaa " + name + ": " + gl.getProgramInfoLog(p));
      }
      if (vs) gl.deleteShader(vs);
      if (fs) gl.deleteShader(fs);
      if (!info) failed = true;
      return (programs[name] = info);
    }
    function target(internal: number, format: number, w2: number, h2: number): any {
      const tex = gl.createTexture();
      gl.bindTexture(gl.TEXTURE_2D, tex);
      gl.texImage2D(gl.TEXTURE_2D, 0, internal, w2, h2, 0, format, gl.UNSIGNED_BYTE, null);
      for (const [k, v] of [[gl.TEXTURE_MIN_FILTER, gl.NEAREST], [gl.TEXTURE_MAG_FILTER, gl.NEAREST], [gl.TEXTURE_WRAP_S, gl.CLAMP_TO_EDGE], [gl.TEXTURE_WRAP_T, gl.CLAMP_TO_EDGE]]) gl.texParameteri(gl.TEXTURE_2D, k, v);
      const fbo = gl.createFramebuffer();
      gl.bindFramebuffer(gl.FRAMEBUFFER, fbo);
      gl.framebufferTexture2D(gl.FRAMEBUFFER, gl.COLOR_ATTACHMENT0, gl.TEXTURE_2D, tex, 0);
      return { fbo, colorTex: tex };
    }
    function release(): void {
      for (const t of [edges, weights]) if (t) { gl.deleteFramebuffer(t.fbo); gl.deleteTexture(t.colorTex); }
      edges = weights = null;
      width = height = 0;
    }
    function resize(w2: number, h2: number): void {
      if (w2 === width && h2 === height) return;
      release();
      edges = target(gl.RG8, gl.RG, w2, h2);
      weights = target(gl.RGBA8, gl.RGBA, w2, h2);
      width = w2; height = h2;
    }
    function draw(p: any, fbo: any, w2: number, h2: number, textures: [string, any][], s: any): void {
      gl.bindFramebuffer(gl.FRAMEBUFFER, fbo);
      gl.drawBuffers([fbo ? gl.COLOR_ATTACHMENT0 : gl.BACK]);
      gl.viewport(0, 0, w2, h2);
      gl.disable(gl.BLEND); gl.disable(gl.DEPTH_TEST); gl.disable(gl.SCISSOR_TEST);
      if (fbo) { gl.clearColor(0, 0, 0, 0); gl.clear(gl.COLOR_BUFFER_BIT); }
      gl.useProgram(p.program);
      textures.forEach(([name, tex], unit) => {
        gl.activeTexture(gl.TEXTURE0 + unit);
        gl.bindTexture(gl.TEXTURE_2D, tex);
        gl.uniform1i(p.loc(name), unit);
      });
      gl.uniform3f(p.loc("u_params"), s.threshold, s.steps, s.corner);
      gl.bindVertexArray(vao);
      gl.drawArrays(gl.TRIANGLES, 0, 3);
      gl.bindVertexArray(null);
      for (let unit = textures.length - 1; unit >= 0; unit--) { gl.activeTexture(gl.TEXTURE0 + unit); gl.bindTexture(gl.TEXTURE_2D, null); }
    }
    return {
      // Draws inputTex (srcW x srcH) into targetFBO (null: the canvas) at outW x outH.
      apply(effect: any, inputTex: any, targetFBO: any, srcW: number, srcH: number, outW: number, outH: number): boolean {
        if (!inputTex || failed || srcW < 1 || srcH < 1) return false;
        const pe = program("edges", EDGES), pw = program("weights", WEIGHTS), pb = program("blend", BLEND);
        if (!pe || !pw || !pb) return false;
        resize(srcW, srcH);
        const s = api.settings(effect);
        draw(pe, edges.fbo, srcW, srcH, [["u_src", inputTex]], s);
        draw(pw, weights.fbo, srcW, srcH, [["u_edges", edges.colorTex]], s);
        gl.useProgram(pb.program);
        gl.uniform2f(pb.loc("u_out"), outW, outH);
        draw(pb, targetFBO ? targetFBO.fbo : null, outW, outH, [["u_src", inputTex], ["u_wts", weights.colorTex]], s);
        return true;
      },
      dispose(): void {
        release();
        for (const name of Object.keys(programs)) if (programs[name]) gl.deleteProgram(programs[name].program);
        gl.deleteVertexArray(vao);
      },
    };
  }

  api.webgl = create;
  api.glsl = { edges: EDGES, weights: WEIGHTS, blend: BLEND, vertex: VERTEX };
})();
