// webgl-ocean.ts — the WebGL2 open-ocean pass for Environment.Ocean.
//
// One draw of a camera-centred polar grid (no vertex buffers; gl_VertexID
// builds the grid) displaced by Gerstner swell, shaded with the scene sky
// (the physical model when the sky is physical), a GGX sun glint, crest
// scatter, whitecaps from horizontal compression, and, with a bathymetry map,
// depth-aware shallow water, shore foam and a run-up surge. The pass draws
// after opaque geometry with premultiplied alpha, so shallow water shows the
// terrain beneath it. Uniform layout: sceneOceanUniformData (16c).

const SCENE_OCEAN_GLSL_COMMON = [
  "uniform vec4 u_ocean[35];",
  "uniform sampler2D u_bathymetry;",
  "float oceanFloor(vec2 xz) {",
  "  if (u_ocean[6].z < 0.5) return -1e4;",
  "  vec2 uv = clamp((xz - u_ocean[5].xy) / (u_ocean[5].zw - u_ocean[5].xy), 0., 1.);",
  "  return mix(u_ocean[6].x, u_ocean[6].y, textureLod(u_bathymetry, uv, 0.).r);",
  "}",
].join("\n");

const SCENE_OCEAN_VERTEX_GLSL = [
  "#version 300 es",
  "precision highp float;",
  "precision highp sampler2D;",
  "uniform mat4 u_viewProj;",
  SCENE_OCEAN_GLSL_COMMON,
  "out vec3 v_world; out vec3 v_normal; out float v_jacobian; out float v_crest; out float v_depth0;",
  "void main() {",
  "  int segs = int(u_ocean[8].y);",
  "  int quad = gl_VertexID / 6, corner = gl_VertexID - quad * 6;",
  "  int ring = quad / segs, seg = quad - ring * segs;",
  "  ring += (corner == 1 || corner == 3 || corner == 4) ? 1 : 0;",
  "  seg += (corner == 2 || corner == 4 || corner == 5) ? 1 : 0;",
  "  float r = u_ocean[8].z * (exp(u_ocean[8].w * float(ring)) - 1.);",
  "  float a = float(seg) / float(segs) * 6.28318530718;",
  "  vec2 xz = floor(u_ocean[7].xz * 0.5) * 2. + r * vec2(cos(a), sin(a));",
  "  float level = u_ocean[0].x, t = u_ocean[0].z;",
  "  float depth0 = level - oceanFloor(xz);",
  "  vec3 p = vec3(xz.x, level, xz.y), dx = vec3(1., 0., 0.), dz = vec3(0., 0., 1.);",
  "  float amp = 0.;",
  "  int waves = int(u_ocean[6].w);",
  "  for (int i = 0; i < 6; i++) {",
  "    if (i >= waves) break;",
  "    vec4 w0 = u_ocean[9 + i * 2], w1 = u_ocean[10 + i * 2];",
  "    float shoal = smoothstep(0., 1.2, depth0 * w0.z);",
  "    float A = w1.x * shoal, QA = w1.y * shoal;",
  "    float th = w0.z * dot(w0.xy, xz) - w0.w * t + w1.z;",
  "    float s = sin(th), c = cos(th), kA = w0.z * A, kQA = w0.z * QA;",
  "    p += vec3(QA * w0.x * c, A * s, QA * w0.y * c);",
  "    dx += vec3(-kQA * w0.x * w0.x * s, kA * w0.x * c, -kQA * w0.x * w0.y * s);",
  "    dz += vec3(-kQA * w0.x * w0.y * s, kA * w0.y * c, -kQA * w0.y * w0.y * s);",
  "    amp += A;",
  "  }",
  // Run-up: near the shore the water rises and falls with a 9 s surge that
  // arrives quickly and drains slowly, offset along the beach.
  "  float ph = fract(t * u_ocean[0].w / 9. + 0.15 * sin(xz.x * 0.07 + 1.3) + 0.08 * sin(xz.x * 0.19));",
  "  float surge = smoothstep(0., 0.28, ph) * (1. - smoothstep(0.28, 1., ph));",
  "  p.y += u_ocean[4].w * 0.45 * surge * (1. - smoothstep(0.3, 4., depth0));",
  "  v_world = p; v_normal = normalize(cross(dz, dx)); v_depth0 = depth0;",
  "  v_jacobian = dx.x * dz.z - dx.z * dz.x;",
  "  v_crest = amp > 0. ? clamp((p.y - level) / (amp * 1.2) * 0.5 + 0.5, 0., 1.) : 0.5;",
  "  vec4 clip = u_viewProj * vec4(p, 1.);",
  "  clip.z = min(clip.z, clip.w * 0.99999);", // keep the horizon beyond the far plane
  "  gl_Position = clip;",
  "}",
].join("\n");

const SCENE_OCEAN_FRAGMENT_GLSL = [
  "#version 300 es",
  "precision highp float;",
  "precision highp sampler2D;",
  SCENE_OCEAN_GLSL_COMMON,
  "in vec3 v_world; in vec3 v_normal; in float v_jacobian; in float v_crest; in float v_depth0;",
  "out vec4 fragColor;",
  "//GOSX_SKY_PHYSICAL",
  "vec3 oceanSky(vec3 d) {",
  "  if (u_ocean[26].w == 4.) return gosxPhysicalSky(d, u_ocean[28], u_ocean[29], vec4(u_ocean[30].xyz, 2.), u_ocean[31].x) * u_ocean[24].w;",
  "  return mix(u_ocean[25].xyz, d.y >= 0. ? u_ocean[24].xyz : u_ocean[26].xyz, abs(d.y)) * u_ocean[24].w;",
  "}",
  "float oceanHash(vec2 p) { p = fract(p * vec2(0.1031, 0.1030)); p += dot(p, p.yx + 33.33); return fract((p.x + p.y) * p.x); }",
  "float oceanNoise(vec2 p) {",
  "  vec2 i = floor(p), f = fract(p), u = f * f * (3. - 2. * f);",
  "  return mix(mix(oceanHash(i), oceanHash(i + vec2(1., 0.)), u.x), mix(oceanHash(i + vec2(0., 1.)), oceanHash(i + vec2(1., 1.)), u.x), u.y);",
  "}",
  "float oceanFbm(vec2 p) { float v = 0., a = 0.5; for (int i = 0; i < 4; i++) { v += a * oceanNoise(p); p = p * 2.03 + 17.1; a *= 0.5; } return v; }",
  // Short capillary-scale waves perturb the normal per pixel. Each wave fades
  // out when its phase changes by more than about a radian per pixel.
  "vec3 oceanDetail(vec3 n, vec2 xz, float t, vec2 wind) {",
  "  vec2 g = vec2(0.); float lambda = 1.7;",
  "  for (int i = 0; i < 8; i++) {",
  "    float ang = float(i) * 2.39996;",
  "    vec2 D = normalize(wind * 2.2 + vec2(cos(ang), sin(ang)));",
  "    float k = 6.2831853 / lambda;",
  "    float ph = k * dot(D, xz) - sqrt(9.81 * k) * t + float(i) * 1.7;",
  "    float fade = 1. - smoothstep(0.6, 2., fwidth(ph));",
  "    g += D * (0.11 * pow(0.83, float(i)) * cos(ph) * fade);",
  "    lambda *= 0.74;",
  "  }",
  "  return normalize(n + vec3(-g.x, 0., -g.y));",
  "}",
  "void main() {",
  "  vec3 toCam = u_ocean[7].xyz - v_world; float dist = length(toCam); vec3 V = toCam / dist;",
  "  float t = u_ocean[0].z * u_ocean[0].w;",
  "  vec3 N = oceanDetail(normalize(v_normal), v_world.xz, t, u_ocean[9].xy);",
  "  float NdV = max(dot(N, V), 1e-3);",
  "  vec3 L = normalize(u_ocean[34].xyz), sunCol = u_ocean[33].xyz, ambient = u_ocean[32].xyz;",
  "  float F = 0.02 + 0.98 * pow(1. - NdV, 5.);",
  "  vec3 R = reflect(-V, N); R.y = abs(R.y);",
  "  vec3 refl = oceanSky(normalize(R));",
  // GGX glint with a roughness that widens for unresolved slopes (distance and
  // normal variation across the pixel), so the sun path stays a streak.
  "  float rough = clamp(u_ocean[2].w + 0.6 * length(fwidth(N)) + dist * 0.0003, u_ocean[2].w, 0.5);",
  "  vec3 H = normalize(L + V);",
  "  float NdL = max(dot(N, L), 0.), NdH = max(dot(N, H), 0.);",
  "  float al = rough * rough, al2 = al * al, dd = NdH * NdH * (al2 - 1.) + 1.;",
  "  float k = al * 0.5, G = (NdL / (NdL * (1. - k) + k)) * (NdV / (NdV * (1. - k) + k));",
  "  float Fh = 0.02 + 0.98 * pow(1. - max(dot(H, V), 0.), 5.);",
  "  vec3 spec = min(sunCol * (al2 / (3.14159265 * dd * dd)) * G * Fh / max(4. * NdV, 1e-3), vec3(64.));",
  "  float crest = v_crest * v_crest;",
  "  vec3 scatter = u_ocean[3].xyz * (sunCol * 0.18 * pow(clamp(dot(V, -L) * 0.5 + 0.5, 0., 1.), 4.) + ambient * 0.12) * crest;",
  "  float depth = v_world.y - oceanFloor(v_world.xz);",
  "  if (depth <= 0.) discard;",
  "  float clarity = u_ocean[1].w;",
  "  float T = exp(-3. * depth * 0.5 * (1. + 1. / max(V.y, 0.08)) / clarity);",
  "  vec3 water = mix(u_ocean[2].xyz * ambient, u_ocean[1].xyz * ambient + scatter, clamp(1. - exp(-depth / (clarity * 0.6)), 0., 1.));",
  "  float foam = 0.;",
  "  if (dist < 250.) {",
  "    float n1 = oceanFbm(v_world.xz * 0.35 + vec2(t * 0.05, t * 0.03)), n2 = oceanFbm(v_world.xz * 1.3 - vec2(t * 0.11, 0.));",
  "    foam = smoothstep(0.72, 0.28, v_jacobian) * smoothstep(0.35, 0.75, n1);",
  "    if (u_ocean[6].z > 0.5) {",
  "      float edge = smoothstep(0.18, 0., depth) * smoothstep(0.25, 0.6, n2 + 0.2);",
  "      float band = smoothstep(2.2, 0.3, v_depth0) * smoothstep(0.45, 0.8, n1 * 0.6 + n2 * 0.5 + 0.25 * sin(v_depth0 * 5. - t * 1.4));",
  "      foam = max(foam, max(edge, band));",
  "    }",
  "    foam = clamp(foam * u_ocean[3].w * 1.6, 0., 1.) * (1. - smoothstep(60., 250., dist));",
  "  }",
  "  vec3 foamLit = u_ocean[4].xyz * (ambient * 0.9 + sunCol * 0.06 * max(dot(N, L), 0.));",
  "  vec3 color = water * (1. - T) * (1. - F) + refl * F + spec;",
  "  float alpha = 1. - T * (1. - F);",
  "  color = mix(color, foamLit, foam); alpha = mix(alpha, 1., foam);",
  // Aerial perspective: the far sea fades into the sky at the horizon.
  "  vec3 hz = normalize(vec3(-V.x, 0.001, -V.z));",
  "  float fog = max(1. - exp(-dist / (u_ocean[0].y * 0.25)), smoothstep(0.7, 0.98, dist / u_ocean[0].y));",
  "  color = mix(color, oceanSky(hz), fog); alpha = mix(alpha, 1., fog);",
  "  if (u_ocean[7].w == 0.) {",
  "    vec3 c = color / max(alpha, 1e-4);",
  "    color = mix(1.055 * pow(c, vec3(1. / 2.4)) - 0.055, c * 12.92, lessThanEqual(c, vec3(0.0031308))) * alpha;",
  "  }",
  "  fragColor = vec4(color, alpha);",
  "}",
].join("\n");

// createSceneOceanWebGLRenderer compiles the pass lazily on the first ocean
// frame. draw() returns the state the mount reports as
// data-gosx-scene3d-ocean: "surface", "shore", "bathymetry-pending",
// "bathymetry-failed" or "unavailable".
function createSceneOceanWebGLRenderer(gl, textureCache, placeholder) {
  const vertex = scenePBRCompileShader(gl, gl.VERTEX_SHADER, SCENE_OCEAN_VERTEX_GLSL);
  const fragment = scenePBRCompileShader(gl, gl.FRAGMENT_SHADER,
    SCENE_OCEAN_FRAGMENT_GLSL.replace("//GOSX_SKY_PHYSICAL", sceneSkyPhysicalSource("glsl")));
  if (!vertex || !fragment) return null;
  const program = scenePBRLinkProgram(gl, vertex, fragment, "Scene ocean");
  if (!program) return null;
  const viewProjLoc = gl.getUniformLocation(program, "u_viewProj");
  const oceanLoc = gl.getUniformLocation(program, "u_ocean[0]");
  const bathymetryLoc = gl.getUniformLocation(program, "u_bathymetry");
  const vao = gl.createVertexArray();
  const data = new Float32Array(140 /* sceneOceanUniformData: 35 vec4 */), viewProj = new Float32Array(16);
  return {
    draw: function(opts) {
      const env = opts.environment, ocean = env.ocean;
      sceneOceanUniformData(ocean, env, opts.camera, opts.timeSeconds, opts.linear, opts.quality, data);
      sceneMat4MultiplyInto(viewProj, opts.proj, opts.view);
      let bathymetry = placeholder, state = "surface";
      if (ocean.bathymetry && ocean.bathymetry.src) {
        const record = scenePBRLoadTexture(gl, ocean.bathymetry.src, textureCache, null, "ocean-bathymetry", "linear");
        if (record && record.loaded && !record.failed) { bathymetry = record.texture; state = "shore"; }
        else { data[26] = 0; state = record && record.failed ? "bathymetry-failed" : "bathymetry-pending"; }
      }
      const cull = gl.isEnabled(gl.CULL_FACE);
      gl.useProgram(program);
      gl.uniformMatrix4fv(viewProjLoc, false, viewProj);
      gl.uniform4fv(oceanLoc, data);
      scenePBRBindTexture(gl, 0, bathymetry, gl.TEXTURE_2D);
      gl.uniform1i(bathymetryLoc, 0);
      gl.enable(gl.BLEND);
      gl.blendFuncSeparate(gl.ONE, gl.ONE_MINUS_SRC_ALPHA, gl.ONE, gl.ONE_MINUS_SRC_ALPHA);
      gl.enable(gl.DEPTH_TEST); gl.depthFunc(gl.LEQUAL); gl.depthMask(true);
      gl.disable(gl.CULL_FACE);
      gl.bindVertexArray(vao);
      gl.drawArrays(gl.TRIANGLES, 0, data[32] * data[33] * 6);
      gl.bindVertexArray(null);
      gl.disable(gl.BLEND);
      if (cull) gl.enable(gl.CULL_FACE);
      return state;
    },
    dispose: function() {
      gl.deleteVertexArray(vao);
      gl.deleteProgram(program); gl.deleteShader(vertex); gl.deleteShader(fragment);
    },
  };
}

// sceneOceanWebGLDraw is the one call the WebGL renderer makes per frame.
function sceneOceanWebGLDraw(resources, gl, opts) {
  const env = opts.environment;
  let state = "none";
  if (env && env.ocean) {
    if (!resources.renderer && !resources.failed) {
      resources.renderer = createSceneOceanWebGLRenderer(gl, opts.textureCache, opts.placeholder);
      resources.failed = !resources.renderer;
    }
    state = resources.renderer ? resources.renderer.draw(opts) : "unavailable";
  }
  if (opts.mount && opts.mount.setAttribute) opts.mount.setAttribute("data-gosx-scene3d-ocean", state);
}
