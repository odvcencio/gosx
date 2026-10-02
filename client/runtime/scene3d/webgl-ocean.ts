// The WebGL2 open-ocean pass for Environment.Ocean.
// A camera-centred polar grid uses gl_VertexID and Gerstner swell with sky,
// sun glint, crest scatter, whitecaps and bathymetry-driven shore foam.
// It draws after opaque geometry with premultiplied alpha and depth.
// Uniform layout: sceneOceanUniformData (16c).

const SCENE_OCEAN_GLSL_COMMON = [
  "uniform vec4 u_ocean[35];",
  "uniform sampler2D u_bathymetry;",
  "float oceanFloor(vec2 xz) {",
  "  if (u_ocean[6].z < 0.5) return -1e4;",
  "  vec2 uv = clamp((xz - u_ocean[5].xy) / (u_ocean[5].zw - u_ocean[5].xy), 0., 1.);",
  "  float r = textureLod(u_bathymetry, uv, 0.).r;",
  "  if (u_ocean[6].z > 1.5) { float s = 2. * r - 1.; return sign(s) * s * s * u_ocean[6].y; }", // signed-sqrt: fine steps near y = 0
  "  return mix(u_ocean[6].x, u_ocean[6].y, r);",
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
  "//GOSX_REFLECTION",
  "//GOSX_CLOUD_SOURCE",
  "vec3 oceanSky(vec3 d) {",
  "  if (u_ocean[26].w == 4.)  { vec3 base = gosxPhysicalSky(d, u_ocean[28], u_ocean[29], vec4(u_ocean[30].xyz, 2.), u_ocean[31].x) * u_ocean[24].w; //GOSX_CLOUD_REFLECT\n return base; }",
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
  "    g += D * (0.09 * pow(0.83, float(i)) * cos(ph) * fade);",
  "    lambda *= 0.74;",
  "  }",
  "  return normalize(n + vec3(-g.x, 0., -g.y) * (1. - smoothstep(40., 500., length(xz - u_ocean[7].xz))));",
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
  "  //GOSX_REFLECTION_LOOKUP",
  "  vec3 H = normalize(L + V);",
  "  float NdL = max(dot(N, L), 0.), NdH = max(dot(N, H), 0.);",
  "  float al = rough * rough, al2 = al * al, dd = NdH * NdH * (al2 - 1.) + 1.;",
  "  float k = al * 0.5, G = (NdL / (NdL * (1. - k) + k)) * (NdV / (NdV * (1. - k) + k));",
  "  float Fh = 0.02 + 0.98 * pow(1. - max(dot(H, V), 0.), 5.);",
  "  vec3 spec = min(sunCol * (al2 / (3.14159265 * dd * dd)) * G * Fh / max(4. * NdV, 1e-3), vec3(64.));",
  "  //GOSX_SUN_PATH",
  "  float crest = v_crest * v_crest;",
  "  vec3 scatter = u_ocean[3].xyz * (sunCol * 0.18 * pow(clamp(dot(V, -L) * 0.5 + 0.5, 0., 1.), 4.) + ambient * 0.12) * crest;",
  "  float depth = max(v_world.y - oceanFloor(v_world.xz), 0.);",
  "  float clarity = u_ocean[1].w;",
  "  float T = exp(-3. * depth * 0.5 * (1. + 1. / max(V.y, 0.08)) / clarity);",
  "  vec3 water = mix(u_ocean[2].xyz * ambient, u_ocean[1].xyz * ambient + scatter, clamp(1. - exp(-depth / (clarity * 0.6)), 0., 1.));",
  "  float foam = 0.;",
  "  if (dist < 250.) {",
  // Lace: thin web lines from ridged noise, advected with the surface.
  "    vec2 fp = v_world.xz + vec2(t * 0.06, t * 0.035);",
  "    float lace = smoothstep(0.72, 0.94, 1. - abs(2. * oceanFbm(fp * 0.9) - 1.)) * smoothstep(0.3, 0.7, oceanFbm(fp * 0.23 + 3.1));",
  "    foam = (1. - smoothstep(0.25, 0.6, v_jacobian)) * lace;",
  "    if (u_ocean[6].z > 0.5) {",
  "      float ph = fract(t / 9. + 0.15 * sin(v_world.x * 0.07 + 1.3) + 0.08 * sin(v_world.x * 0.19));",
  "      float breakDepth = mix(1.6, 0.25, smoothstep(0., 0.28, ph));", // the breaker line runs shoreward with the surge
  "      float breaker = exp(-pow((v_depth0 - breakDepth) / 0.3, 2.)) * (1. - smoothstep(0.28, 0.6, ph));",
  "      float edge = (1. - smoothstep(0.0, 0.1, depth)) * smoothstep(0.002, 0.03, depth);",
  "      float wash = (1. - smoothstep(0.2, 1.8, v_depth0)) * 0.35;",
  "      foam = max(foam, (max(breaker, edge) + wash) * lace * 1.6);",
  "    }",
  "    foam = clamp(foam * u_ocean[3].w, 0., 1.) * (1. - smoothstep(80., 250., dist));",
  "  }",
  "  vec3 foamLit = u_ocean[4].xyz * (ambient * 0.55 + sunCol * 0.04 * max(dot(N, L), 0.));",
  "  vec3 color = water * (1. - T) * (1. - F) + refl * F + spec;",
  "  float alpha = 1. - T * (1. - F);",
  "  color = mix(color, foamLit, foam); alpha = mix(alpha, 1., foam);",
  "  float shoreFade = u_ocean[6].z > 0.5 ? smoothstep(0., 0.04, depth) : 1.;",
  "  color *= shoreFade; alpha *= shoreFade;",
  // Aerial perspective: the far sea fades into the sky at the horizon.
  "  vec3 hz = normalize(vec3(-V.x, 0.001, -V.z));",
  "  float fog = max(1. - exp(-dist / (u_ocean[0].y * 0.25)), smoothstep(0.7, 0.98, dist / u_ocean[0].y));",
  "  color = mix(color, oceanSky(hz), fog); alpha = mix(alpha, 1., fog);",
  "  if (u_ocean[7].w == 0.) {",
  "    vec3 c = color / max(alpha, 1e-4);",
  "    color = mix(1.055 * pow(c, vec3(1. / 2.4)) - 0.055, c * 12.92, lessThanEqual(c, vec3(0.0031308))) * alpha;",
  "  }",
  "  //GOSX_REFLECTION_APPLY",
  "  fragColor = vec4(color, alpha);",
  "}",
].join("\n");

// createSceneOceanWebGLRenderer compiles the pass lazily on the first ocean
// frame. draw() returns the state the mount reports as
// data-gosx-scene3d-ocean: "surface", "shore", "bathymetry-pending",
// "bathymetry-failed" or "unavailable".
function createSceneOceanWebGLRenderer(gl, textureCache, placeholder, reflections, clouds) {
  const vertex = scenePBRCompileShader(gl, gl.VERTEX_SHADER, SCENE_OCEAN_VERTEX_GLSL);
  const fragment = scenePBRCompileShader(gl, gl.FRAGMENT_SHADER,
    SCENE_OCEAN_FRAGMENT_GLSL.replace("//GOSX_SKY_PHYSICAL", sceneSkyPhysicalSource("glsl"))
      .replace("//GOSX_REFLECTION", reflections ? sceneOceanReflectGLSL() : "")
      .replace("//GOSX_CLOUD_SOURCE", clouds ? sceneOceanCloudGLSL() : "")
      .replace("//GOSX_CLOUD_REFLECT", clouds ? "vec4 cloud = gosxClouds(d,u_cloud[13].xyz,u_cloud[11],u_cloud[12],u_cloud[14].xyz,u_cloud[15].xyz,u_cloud[9].xyz); base = base*(1.-cloud.a)+cloud.rgb;" : "")
      .replace("//GOSX_REFLECTION_LOOKUP", reflections ? "refl = oceanGeometryReflection(v_world, normalize(R), N, rough, refl);" : "")
      .replace("//GOSX_SUN_PATH", reflections ? "spec = mix(spec, oceanSunPath(N,H,L,V,rough,sunCol), 0.35);" : ""));
  if (!vertex || !fragment) return null;
  const program = scenePBRLinkProgram(gl, vertex, fragment, "Scene ocean");
  if (!program) return null;
  let uniforms;
  scenePBRWhenProgramReady(gl, program, function() {
    uniforms = scenePBRUniformLocations(gl, program, "viewProj ocean[0] bathymetry");
  });
  const vao = gl.createVertexArray();
  const cloudData = clouds ? new Float32Array(64) : null;
  const data = new Float32Array(140 /* sceneOceanUniformData: 35 vec4 */), viewProj = new Float32Array(16);
  return {
    draw: function(opts) {
      if (!uniforms) return "unavailable";
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
      gl.uniformMatrix4fv(uniforms.viewProj, false, viewProj);
      gl.uniform4fv(uniforms["ocean[0]"], data);
      if (reflections) sceneReflectWebGLBind(gl, program, opts.reflection);
      if (clouds) sceneOceanCloudWebGL(gl, program, opts, cloudData);
      scenePBRBindTexture(gl, 0, bathymetry, gl.TEXTURE_2D);
      gl.uniform1i(uniforms.bathymetry, 0);
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
    const featureKey = (sceneOceanReflections(oceanReflections(env)) ? 1 : 0) + (env.sky && env.sky.mode === "physical" && env.sky.clouds ? 2 : 0);
    if (resources.renderer && resources.featureKey !== featureKey) { resources.renderer.dispose(); resources.renderer = null; resources.failed = false; }
    if (!resources.renderer && !resources.failed) {
      resources.featureKey = featureKey;
      resources.renderer = createSceneOceanWebGLRenderer(gl, opts.textureCache, opts.placeholder, Boolean(featureKey & 1), Boolean(featureKey & 2));
      resources.failed = !resources.renderer;
    }
    state = resources.renderer ? resources.renderer.draw(opts) : "unavailable";
  }
  if (opts.mount && opts.mount.setAttribute) opts.mount.setAttribute("data-gosx-scene3d-ocean", state);
}

function oceanReflections(env) { return env && env.ocean && env.ocean.reflections; }
