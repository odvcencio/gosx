// WebGL2 projected decals. The same technique as decal-wgpu.ts: copy the input
// color into the target, then draw each decal box (back faces) with blending,
// reading the scene depth texture to place and shade the decal.
(() => {
  const fx = (window as any).__gosx_scene3d_depthfx_api;
  if (!fx) return;

  const CUBE = [1, 3, 7, 1, 7, 5, 0, 6, 2, 0, 4, 6, 2, 7, 3, 2, 6, 7, 0, 1, 5, 0, 5, 4, 4, 5, 7, 4, 7, 6, 0, 2, 3, 0, 3, 1];

  const VERT = `#version 300 es
precision highp float;
precision highp int;
layout(location=0) in vec4 a_m0; layout(location=1) in vec4 a_m1; layout(location=2) in vec4 a_m2; layout(location=3) in vec4 a_m3;
layout(location=4) in vec4 a_i0; layout(location=5) in vec4 a_i1; layout(location=6) in vec4 a_i2; layout(location=7) in vec4 a_i3;
layout(location=8) in vec4 a_rect; layout(location=9) in vec4 a_tint;
uniform mat4 u_viewProj;
flat out vec4 v_i0; flat out vec4 v_i1; flat out vec4 v_i2; flat out vec4 v_i3;
flat out vec4 v_rect; flat out vec4 v_tint;
flat out vec3 v_ax; flat out vec3 v_ay; flat out vec3 v_az;
const int CUBE[36] = int[36](${CUBE.join(",")});
void main() {
  int c = CUBE[gl_VertexID];
  vec3 corner = vec3((c & 1) != 0 ? 0.5 : -0.5, (c & 2) != 0 ? 0.5 : -0.5, (c & 4) != 0 ? 0.5 : -0.5);
  mat4 model = mat4(a_m0, a_m1, a_m2, a_m3);
  gl_Position = u_viewProj * (model * vec4(corner, 1.0));
  v_i0 = a_i0; v_i1 = a_i1; v_i2 = a_i2; v_i3 = a_i3; v_rect = a_rect; v_tint = a_tint;
  v_ax = normalize(a_m0.xyz); v_ay = normalize(a_m1.xyz); v_az = normalize(a_m2.xyz);
}`;

  const FRAG = `#version 300 es
precision highp float;
precision highp int;
precision highp sampler2D;
flat in vec4 v_i0; flat in vec4 v_i1; flat in vec4 v_i2; flat in vec4 v_i3;
flat in vec4 v_rect; flat in vec4 v_tint;
flat in vec3 v_ax; flat in vec3 v_ay; flat in vec3 v_az;
uniform mat4 u_invViewProj;
uniform vec3 u_eye; uniform vec3 u_sunDir; uniform vec3 u_sunColor; uniform vec3 u_ambient; uniform vec2 u_size;
uniform sampler2D u_depth; uniform sampler2D u_albedo; uniform sampler2D u_normal; uniform sampler2D u_rough; uniform sampler2D u_emissive;
uniform vec4 u_fade; uniform vec4 u_pbr; uniform vec4 u_emissiveColor; uniform vec4 u_color;
out vec4 fragColor;
float depthAt(ivec2 t) {
  ivec2 dims = textureSize(u_depth, 0);
  return texelFetch(u_depth, clamp(t, ivec2(0), dims - ivec2(1)), 0).r;
}
vec3 worldAt(ivec2 t) {
  vec2 uv = (vec2(t) + 0.5) / vec2(textureSize(u_depth, 0));
  vec4 w = u_invViewProj * vec4(uv * 2.0 - 1.0, depthAt(t) * 2.0 - 1.0, 1.0);
  return w.xyz / w.w;
}
void main() {
  vec2 dims = vec2(textureSize(u_depth, 0));
  ivec2 tc = ivec2(gl_FragCoord.xy / u_size * dims);
  float d = depthAt(tc);
  if (d >= 0.999999) discard;
  mat4 inv = mat4(v_i0, v_i1, v_i2, v_i3);
  vec3 p = worldAt(tc);
  vec3 lp = (inv * vec4(p, 1.0)).xyz;
  if (any(greaterThan(abs(lp), vec3(0.5)))) discard;
  vec3 pl = worldAt(tc + ivec2(-1, 0)); vec3 pr = worldAt(tc + ivec2(1, 0));
  vec3 pu = worldAt(tc + ivec2(0, -1)); vec3 pd = worldAt(tc + ivec2(0, 1));
  bool useR = length(pr - p) < length(p - pl);
  bool useD = length(pd - p) < length(p - pu);
  vec3 dx = useR ? pr - p : p - pl;
  vec3 dy = useD ? pd - p : p - pu;
  vec3 n = normalize(cross(dx, dy));
  vec3 toEye = u_eye - p;
  if (dot(n, toEye) < 0.0) n = -n;
  float angle = smoothstep(u_fade.y, u_fade.x, dot(n, v_az));
  float e = u_fade.z;
  float edge = (1.0 - smoothstep(0.5 - e, 0.5, abs(lp.x))) * (1.0 - smoothstep(0.5 - e, 0.5, abs(lp.y))) * (1.0 - smoothstep(0.5 - e, 0.5, abs(lp.z)));
  vec2 inset = 0.5 / vec2(textureSize(u_albedo, 0));
  vec2 uv = clamp(vec2(lp.x + 0.5, 0.5 - lp.y) * v_rect.zw + v_rect.xy, v_rect.xy + inset, v_rect.xy + v_rect.zw - inset);
  vec2 lx = (inv * vec4(dx + p, 1.0)).xy - lp.xy;
  vec2 ly = (inv * vec4(dy + p, 1.0)).xy - lp.xy;
  vec2 ddx = vec2(lx.x, -lx.y) * v_rect.zw;
  vec2 ddy = vec2(ly.x, -ly.y) * v_rect.zw;
  vec4 albedo = textureGrad(u_albedo, uv, ddx, ddy);
  float alpha = albedo.a * v_tint.a * edge * angle;
  if (alpha <= 0.002) discard;
  vec3 base = albedo.rgb * v_tint.rgb * u_color.rgb;
  if (u_color.a > 0.5) { fragColor = vec4(mix(vec3(1.0), base, alpha), 0.0); return; }
  vec3 nm = n;
  if (u_pbr.z > 0.5) {
    vec3 t = textureGrad(u_normal, uv, ddx, ddy).xyz * 2.0 - 1.0;
    nm = normalize(n + (v_ax * t.x + v_ay * t.y) * u_fade.w);
  }
  float rough = u_pbr.x;
  if (u_pbr.w > 0.5) rough *= textureGrad(u_rough, uv, ddx, ddy).g;
  rough = clamp(rough, 0.04, 1.0);
  vec3 toSun = -u_sunDir;
  float ndl = max(dot(nm, toSun), 0.0);
  vec3 light = u_ambient + u_sunColor * ndl;
  vec3 h = normalize(toSun + normalize(toEye));
  float shininess = 2.0 / (rough * rough) - 2.0;
  vec3 spec = ndl > 0.0 ? u_sunColor * pow(max(dot(nm, h), 0.0), shininess) * (1.0 - rough) * 0.25 : vec3(0.0);
  vec3 emit = u_emissiveColor.w > 0.5 ? textureGrad(u_emissive, uv, ddx, ddy).rgb * u_emissiveColor.rgb * u_pbr.y : u_emissiveColor.rgb * u_pbr.y;
  fragColor = vec4((base * light + spec + emit) * alpha, alpha);
}`;

  const BLIT_VERT = `#version 300 es
out vec2 v_uv;
void main() { vec2 p = vec2(float((gl_VertexID << 1) & 2), float(gl_VertexID & 2)); v_uv = p; gl_Position = vec4(p * 2.0 - 1.0, 0.0, 1.0); }`;
  const BLIT_FRAG = `#version 300 es
precision highp float;
in vec2 v_uv; uniform sampler2D u_texture; out vec4 fragColor;
void main() { fragColor = texture(u_texture, v_uv); }`;

  function compile(gl: any, vertex: string, fragment: string): any {
    const make = (type: number, source: string): any => {
      const shader = gl.createShader(type);
      gl.shaderSource(shader, source); gl.compileShader(shader);
      if (!gl.getShaderParameter(shader, gl.COMPILE_STATUS)) throw new Error("decal shader: " + gl.getShaderInfoLog(shader));
      return shader;
    };
    const program = gl.createProgram();
    const vs = make(gl.VERTEX_SHADER, vertex), fs = make(gl.FRAGMENT_SHADER, fragment);
    gl.attachShader(program, vs); gl.attachShader(program, fs); gl.linkProgram(program);
    gl.deleteShader(vs); gl.deleteShader(fs);
    if (!gl.getProgramParameter(program, gl.LINK_STATUS)) throw new Error("decal program: " + gl.getProgramInfoLog(program));
    return program;
  }

  function createWebGLDecals(gl: any) {
    const set = fx.createDecalSet();
    const STRIDE = fx.DECAL_INSTANCE_FLOATS * 4;
    const invVP = new Float32Array(16);
    const eye = new Float32Array(3);
    const ambient = new Float32Array(3), sunDir = new Float32Array(3), sunColor = new Float32Array(3);
    let program: any = null, blitProgram: any = null, vao: any = null, instanceBuffer: any = null, instanceCapacity = 0, emptyVAO: any = null;
    const uniforms: any = {}, blitUniforms: any = {};
    let failed = false, disposed = false;
    let materialTextures: any[] = [];
    let materialsFor: any = null;
    const textures = new Map<string, any>();
    const placeholders: any = {};

    function placeholder(kind: string, rgba: number[]): any {
      if (placeholders[kind]) return placeholders[kind];
      const texture = gl.createTexture();
      gl.bindTexture(gl.TEXTURE_2D, texture);
      gl.texImage2D(gl.TEXTURE_2D, 0, gl.RGBA8, 1, 1, 0, gl.RGBA, gl.UNSIGNED_BYTE, new Uint8Array(rgba));
      gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_MIN_FILTER, gl.NEAREST); gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_MAG_FILTER, gl.NEAREST);
      return (placeholders[kind] = texture);
    }

    function textureFor(url: string, kind: string): any {
      const fallback = placeholder(kind, kind === "normal" ? [128, 128, 255, 255] : kind === "emissive" ? [0, 0, 0, 255] : [255, 255, 255, 255]);
      if (!url) return { texture: fallback };
      const key = kind + "|" + url;
      let entry = textures.get(key);
      if (entry) return entry;
      entry = { texture: fallback };
      textures.set(key, entry);
      fx.loadBitmap(url).then((bitmap: any) => {
        if (disposed) { bitmap.close && bitmap.close(); return; }
        const texture = gl.createTexture();
        gl.activeTexture(gl.TEXTURE0);
        gl.bindTexture(gl.TEXTURE_2D, texture);
        gl.pixelStorei(gl.UNPACK_FLIP_Y_WEBGL, false); gl.pixelStorei(gl.UNPACK_PREMULTIPLY_ALPHA_WEBGL, false);
        const srgb = kind === "albedo" || kind === "emissive";
        gl.texImage2D(gl.TEXTURE_2D, 0, srgb ? gl.SRGB8_ALPHA8 : gl.RGBA8, bitmap.width, bitmap.height, 0, gl.RGBA, gl.UNSIGNED_BYTE, bitmap);
        gl.generateMipmap(gl.TEXTURE_2D);
        gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_MIN_FILTER, gl.LINEAR_MIPMAP_LINEAR); gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_MAG_FILTER, gl.LINEAR);
        gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_WRAP_S, gl.CLAMP_TO_EDGE); gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_WRAP_T, gl.CLAMP_TO_EDGE);
        bitmap.close && bitmap.close();
        entry.texture = texture; entry.loaded = true;
      }).catch((error: any) => { console.warn("[gosx] decal texture failed:", url, error && error.message || error); });
      return entry;
    }

    function setup(): void {
      program = compile(gl, VERT, FRAG);
      for (const name of ["u_viewProj", "u_invViewProj", "u_eye", "u_sunDir", "u_sunColor", "u_ambient", "u_size", "u_depth", "u_albedo", "u_normal", "u_rough", "u_emissive", "u_fade", "u_pbr", "u_emissiveColor", "u_color"]) uniforms[name] = gl.getUniformLocation(program, name);
      blitProgram = compile(gl, BLIT_VERT, BLIT_FRAG);
      blitUniforms.texture = gl.getUniformLocation(blitProgram, "u_texture");
      emptyVAO = gl.createVertexArray();
      instanceBuffer = gl.createBuffer();
      vao = gl.createVertexArray();
    }

    function bindInstances(first: number): void {
      gl.bindVertexArray(vao);
      gl.bindBuffer(gl.ARRAY_BUFFER, instanceBuffer);
      // model (0..3), inverse (4..7), uv rect (8), tint (9): vec4 each, per instance.
      for (let i = 0; i < 10; i++) {
        gl.enableVertexAttribArray(i);
        gl.vertexAttribPointer(i, 4, gl.FLOAT, false, STRIDE, first * STRIDE + i * 16);
        gl.vertexAttribDivisor(i, 1);
      }
    }

    // apply draws the decals over inputTex into targetFBO (or the default
    // framebuffer when null). It returns false, leaving the target untouched,
    // when there is nothing to draw.
    function apply(effect: any, inputTex: any, targetFBO: any, width: number, height: number, depthTex: any, frame: any): boolean {
      if (failed || disposed || !depthTex || !frame || !frame.viewProj) return false;
      try {
        if (!program) setup();
        const rebuilt = set.update(effect);
        if (rebuilt || materialsFor !== set.materials) {
          materialsFor = set.materials;
          materialTextures = set.materials.map((m: any) => [textureFor(m.albedo, "albedo"), textureFor(m.normal, "normal"), textureFor(m.roughness, "rough"), textureFor(m.emissive, "emissive")]);
        }
        const cam = frame.camera || {};
        eye[0] = +cam.x || 0; eye[1] = +cam.y || 0; eye[2] = +cam.z || 0;
        const count = set.prepare(frame.viewProj, false, eye);
        if (count === 0 || !fx.invert(invVP, frame.viewProj)) return false;
        fx.lightSummary(frame, ambient, sunDir, sunColor);

        gl.bindFramebuffer(gl.FRAMEBUFFER, targetFBO ? targetFBO.fbo : null);
        gl.viewport(0, 0, width, height);
        gl.disable(gl.DEPTH_TEST); gl.disable(gl.BLEND); gl.disable(gl.CULL_FACE);
        gl.useProgram(blitProgram);
        gl.bindVertexArray(emptyVAO);
        gl.activeTexture(gl.TEXTURE0); gl.bindTexture(gl.TEXTURE_2D, inputTex);
        gl.uniform1i(blitUniforms.texture, 0);
        gl.drawArrays(gl.TRIANGLES, 0, 3);

        gl.bindBuffer(gl.ARRAY_BUFFER, instanceBuffer);
        const bytes = count * STRIDE;
        if (bytes > instanceCapacity) { instanceCapacity = Math.max(bytes, instanceCapacity * 2); gl.bufferData(gl.ARRAY_BUFFER, instanceCapacity, gl.DYNAMIC_DRAW); }
        gl.bufferSubData(gl.ARRAY_BUFFER, 0, set.instances, 0, count * fx.DECAL_INSTANCE_FLOATS);

        gl.useProgram(program);
        gl.uniformMatrix4fv(uniforms.u_viewProj, false, frame.viewProj);
        gl.uniformMatrix4fv(uniforms.u_invViewProj, false, invVP);
        gl.uniform3fv(uniforms.u_eye, eye); gl.uniform3fv(uniforms.u_sunDir, sunDir);
        gl.uniform3fv(uniforms.u_sunColor, sunColor); gl.uniform3fv(uniforms.u_ambient, ambient);
        gl.uniform2f(uniforms.u_size, width, height);
        gl.activeTexture(gl.TEXTURE1); gl.bindTexture(gl.TEXTURE_2D, depthTex); gl.uniform1i(uniforms.u_depth, 1);
        gl.uniform1i(uniforms.u_albedo, 2); gl.uniform1i(uniforms.u_normal, 3); gl.uniform1i(uniforms.u_rough, 4); gl.uniform1i(uniforms.u_emissive, 5);
        gl.enable(gl.BLEND); gl.enable(gl.CULL_FACE); gl.cullFace(gl.FRONT);
        const mats = set.materials, runMaterial = set.runMaterial, runFirst = set.runFirst, runLength = set.runLength;
        let lastMultiply = -1;
        for (let r = 0; r < set.runs; r++) {
          const mi = runMaterial[r], m = mats[mi], u = m.uniforms;
          if (lastMultiply !== (m.multiply ? 1 : 0)) {
            lastMultiply = m.multiply ? 1 : 0;
            if (m.multiply) gl.blendFuncSeparate(gl.DST_COLOR, gl.ZERO, gl.ZERO, gl.ONE);
            else gl.blendFuncSeparate(gl.ONE, gl.ONE_MINUS_SRC_ALPHA, gl.ONE, gl.ONE_MINUS_SRC_ALPHA);
          }
          const tex = materialTextures[mi];
          for (let t = 0; t < 4; t++) { gl.activeTexture(gl.TEXTURE2 + t); gl.bindTexture(gl.TEXTURE_2D, tex[t].texture); }
          gl.uniform4f(uniforms.u_fade, u[0], u[1], u[2], u[3]);
          gl.uniform4f(uniforms.u_pbr, u[4], u[5], u[6], u[7]);
          gl.uniform4f(uniforms.u_emissiveColor, u[8], u[9], u[10], u[11]);
          gl.uniform4f(uniforms.u_color, u[12], u[13], u[14], u[15]);
          bindInstances(runFirst[r]);
          gl.drawArraysInstanced(gl.TRIANGLES, 0, 36, runLength[r]);
        }
        for (let i = 0; i < 10; i++) gl.disableVertexAttribArray(i);
        gl.bindVertexArray(null);
        gl.disable(gl.BLEND); gl.disable(gl.CULL_FACE);
        for (let t = 5; t >= 1; t--) { gl.activeTexture(gl.TEXTURE0 + t); gl.bindTexture(gl.TEXTURE_2D, null); }
        gl.activeTexture(gl.TEXTURE0);
        fx.stats.decals.frames++; fx.stats.decals.visible = count;
        return true;
      } catch (error: any) {
        failed = true;
        console.error("[gosx] decal pass disabled:", error && error.message || error);
        return false;
      }
    }

    return {
      apply,
      stats: () => ({ visible: set.visible, runs: set.runs }),
      dispose() {
        disposed = true;
        if (program) gl.deleteProgram(program);
        if (blitProgram) gl.deleteProgram(blitProgram);
        if (instanceBuffer) gl.deleteBuffer(instanceBuffer);
        if (vao) gl.deleteVertexArray(vao);
        if (emptyVAO) gl.deleteVertexArray(emptyVAO);
        textures.forEach((t: any) => t.loaded && gl.deleteTexture(t.texture));
        Object.keys(placeholders).forEach((k: string) => gl.deleteTexture(placeholders[k]));
      },
    };
  }

  fx.webgl.decals = createWebGLDecals;
})();
