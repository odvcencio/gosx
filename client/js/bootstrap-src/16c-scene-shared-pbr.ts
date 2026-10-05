  // Backend-agnostic Scene3D PBR helpers. This file stays in the base
  // scene3d chunk. 16-scene-webgl.js became a lazily fetched chunk
  // (bootstrap-feature-scene3d-webgl.js) because a WebGPU-capable browser
  // never runs it. These helpers are the part of the old WebGL file that the
  // base chunk and the WebGPU chunk still need, so they must stay eager.
  //
  // Three groups of callers depend on them:
  //   1. 15b-scene-planner.js sorts and classifies draw passes with
  //      scenePBRObjectRenderPass and scenePBRDepthSort.
  //   2. 10-runtime-scene-core.js and 20-scene-mount.js keep light and
  //      environment dirty hashes with hashLightContent and
  //      hashEnvironmentContent.
  //   3. 10-runtime-scene-core.js publishes the camera matrices, the shadow
  //      bounds and the instanced geometry generators on
  //      window.__gosx_scene3d_api. The WebGPU chunk reads them there through
  //      26e-feature-scene3d-webgpu-prefix.js.
  //
  // Every function here is pure math or plain object work. None of them
  // touches a WebGLRenderingContext. Keep it that way: a `gl.` call in this
  // file means the WebGL split leaked back into the eager path.
  //
  // The 37 declarations below are a closed dependency set. Adding a caller
  // here that reaches back into 16-scene-webgl.js breaks a WebGPU-only page
  // with a ReferenceError, which no test in this repo can catch without a
  // real GPU. Re-derive the closure before you move anything in or out.

  // Post-effect kind constants.
  var SCENE_POST_TONE_MAPPING = "toneMapping";

  var SCENE_POST_BLOOM = "bloom";

  var SCENE_POST_VIGNETTE = "vignette";

  var SCENE_POST_COLOR_GRADE = "colorGrade";

  var SCENE_POST_SSAO = "ssao";

  var SCENE_POST_DOF = "dof";

  var SCENE_POST_CUSTOM_POST = "customPost";

  var SCENE_POST_FXAA = "fxaa";

  // Shared sky layout: camera basis, linear gradient stops, and sampling controls.
  function sceneSkyUniformData(out, environment, view, camera, aspect, linear) {
    var sky = environment.sky;
    var spread = camera.kind === "orthographic" || camera.mode === "ortho2d"
      ? 0 : Math.tan(sceneNumber(camera.fov, 60) * Math.PI / 360);
    out.set([view[0], view[4], view[8], spread * aspect,
      view[1], view[5], view[9], spread,
      -view[2], -view[6], -view[10], sceneNumber(environment.envRotation, 0)]);
    var horizon = sceneColorRGBA(sky.horizonColor, [0.5, 0.6, 0.7, 1]);
    var colors = [sceneColorRGBA(sky.topColor, horizon), horizon, sceneColorRGBA(sky.bottomColor, horizon)];
    for (var stop = 0; stop < 3; stop++) {
      for (var channel = 0; channel < 3; channel++) {
        var c = colors[stop][channel];
        out[12 + stop * 4 + channel] = c <= 0.04045 ? c / 12.92 : Math.pow((c + 0.055) / 1.055, 2.4);
      }
    }
    out[15] = Math.max(0, sceneNumber(sky.intensity, 1) || 1);
    out[19] = 0; // Backend supplies the available mip count.
    out[23] = sky.mode === "environment" ? 3 : 0; // Horizon fallback until a texture is ready.
    out[24] = linear ? 1 : 0;
    if (sky.mode === "physical" && out.length >= 44) {
      out[23] = 4;
      sceneSkyPhysicalParams(sky, out, 28);
    }
    return out;
  }

  // GLSL and WGSL of the physical sky. Both renderers' sky passes and any
  // pass that reflects the sky include these; inputs come from
  // sceneSkyPhysicalParams.
  var SCENE_SKY_PHYSICAL_GLSL = `vec3 gosxPhysicalSky(vec3 dir, vec4 betaR, vec4 betaM, vec4 sun, float g) {
  float zenith = acos(clamp(dir.y, 0., 1.));
  float inv = 1. / (cos(zenith) + 0.15 * pow(93.885 - zenith * 57.29577951, -1.253));
  vec3 fex = exp(-(betaR.xyz * (8400. * inv) + betaM.xyz * (1250. * inv)));
  float ct = dot(dir, sun.xyz);
  float rPhase = 0.0596831 * (1. + ct * ct);
  float g2 = g * g;
  float mPhase = 0.0795775 * (1. - g2) / pow(max(1. - 2. * g * ct + g2, 1e-6), 1.5);
  vec3 scatter = (betaR.xyz * rPhase + betaM.xyz * mPhase) / max(betaR.xyz + betaM.xyz, vec3(1e-30));
  vec3 lin = pow(max(betaR.w * scatter * (1. - fex), vec3(0.)), vec3(1.5));
  lin *= mix(vec3(1.), pow(max(betaR.w * scatter * fex, vec3(0.)), vec3(.5)), clamp(pow(max(1. - sun.y, 0.), 5.), 0., 1.));
  float disk = sun.w <= 1. ? smoothstep(sun.w, sun.w + 0.00002, ct) : 0.;
  vec3 l0 = 0.1 * fex + betaR.w * 19000. * fex * disk;
  vec3 c = (lin + l0) * 0.04 + vec3(0., 0.0003, 0.00075);
  return pow(max(c, vec3(0.)), vec3(1. / (1.2 + 1.2 * betaM.w)));
}`;

  var SCENE_SKY_PHYSICAL_WGSL = `fn gosxPhysicalSky(dir: vec3f, betaR: vec4f, betaM: vec4f, sun: vec4f, g: f32) -> vec3f {
  let zenith = acos(clamp(dir.y, 0.0, 1.0));
  let inv = 1.0 / (cos(zenith) + 0.15 * pow(93.885 - zenith * 57.29577951, -1.253));
  let fex = exp(-(betaR.xyz * (8400.0 * inv) + betaM.xyz * (1250.0 * inv)));
  let ct = dot(dir, sun.xyz);
  let rPhase = 0.0596831 * (1.0 + ct * ct);
  let g2 = g * g;
  let mPhase = 0.0795775 * (1.0 - g2) / pow(max(1.0 - 2.0 * g * ct + g2, 1e-6), 1.5);
  let scatter = (betaR.xyz * rPhase + betaM.xyz * mPhase) / max(betaR.xyz + betaM.xyz, vec3f(1e-30));
  var lin = pow(max(betaR.w * scatter * (1.0 - fex), vec3f(0.0)), vec3f(1.5));
  lin = lin * mix(vec3f(1.0), pow(max(betaR.w * scatter * fex, vec3f(0.0)), vec3f(0.5)), clamp(pow(max(1.0 - sun.y, 0.0), 5.0), 0.0, 1.0));
  var disk = 0.0;
  if (sun.w <= 1.0) { disk = smoothstep(sun.w, sun.w + 0.00002, ct); }
  let l0 = 0.1 * fex + betaR.w * 19000.0 * fex * disk;
  let c = (lin + l0) * 0.04 + vec3f(0.0, 0.0003, 0.00075);
  return pow(max(c, vec3f(0.0)), vec3f(1.0 / (1.2 + 1.2 * betaM.w)));
}`;

  // --- Ocean ---
  //
  // sceneOceanUniformData packs one Environment.Ocean (already normalized by
  // normalizeSceneOcean, so every field holds its default) and the scene sky
  // into SCENE_OCEAN_VEC4S vec4s shared by the WebGL2 and WebGPU passes:
  //   0  level, extent, time (s), speed        1  deep.rgb, clarity
  //   2  shallow.rgb, roughness                3  scatter.rgb, foam
  //   4  foamColor.rgb, surf                   5  bathymetry minX, minZ, maxX, maxZ
  //   6  bathymetry minH, maxH, has (1 linear, 2 signed-sqrt: minH 0, maxH = range), waves
  //   7  camera.xyz, output linear (1) or sRGB (0)
  //   8  rings, segments, inner radius, ring growth
  //   9..20  six Gerstner waves: (dir.x, dir.z, k, omega), (amplitude, Q*a, phase, 0)
  //   21..31 the sky block from sceneSkyUniformData (mode in [26].w)
  //   32 ambient sky light rgb            33 sun radiance rgb at the surface
  //   34 direction toward the sun xyz
  // Colors are converted to linear light. Wave amplitudes follow a significant
  // height Hs: sum(a^2) = Hs^2 / 8. quality "low" (default on low-end
  // hardware) halves the grid and uses four waves.
  var SCENE_OCEAN_VEC4S = 35;
  var SCENE_OCEAN_WAVE_RATIO = [1, 0.73, 0.53, 0.39, 0.28, 0.21];
  var SCENE_OCEAN_WAVE_ANGLE = [0, 0.38, -0.46, 0.83, -0.95, 1.4];
  var SCENE_OCEAN_WAVE_WEIGHT = [1, 0.62, 0.42, 0.28, 0.19, 0.13];
  var sceneOceanSkyScratch = new Float32Array(44), sceneOceanViewScratch = new Float32Array(16);

  function sceneOceanLinear(hex, out, offset) {
    var c = sceneColorRGBA(hex, [0, 0, 0, 1]);
    for (var i = 0; i < 3; i++) out[offset + i] = c[i] <= 0.04045 ? c[i] / 12.92 : Math.pow((c[i] + 0.055) / 1.055, 2.4);
  }

  // sceneSkyPhysicalRadianceInto evaluates gosxPhysicalSky on the CPU (no sun
  // disk) from a sceneSkyPhysicalParams block; the ocean uses it for the
  // per-frame ambient light so phones do not evaluate it per pixel.
  function sceneSkyPhysicalRadianceInto(p, dx, dy, dz, out, offset) {
    var len = Math.sqrt(dx * dx + dy * dy + dz * dz) || 1; dx /= len; dy /= len; dz /= len;
    var zenith = Math.acos(Math.max(0, Math.min(1, dy)));
    var inv = 1 / (Math.cos(zenith) + 0.15 * Math.pow(93.885 - zenith * 57.29577951, -1.253));
    var ct = dx * p[8] + dy * p[9] + dz * p[10], g = p[12], g2 = g * g;
    var rPhase = 0.0596831 * (1 + ct * ct), mPhase = 0.0795775 * (1 - g2) / Math.pow(Math.max(1 - 2 * g * ct + g2, 1e-6), 1.5);
    var horizon = Math.max(0, Math.min(1, Math.pow(1 - p[9], 5))), exponent = 1 / (1.2 + 1.2 * p[7]);
    var bias = [0, 0.0003, 0.00075];
    for (var i = 0; i < 3; i++) {
      var fex = Math.exp(-(p[i] * 8400 * inv + p[4 + i] * 1250 * inv));
      var scatter = (p[i] * rPhase + p[4 + i] * mPhase) / Math.max(p[i] + p[4 + i], 1e-30);
      var lin = Math.pow(p[3] * scatter * (1 - fex), 1.5) * (1 + (Math.pow(p[3] * scatter * fex, 0.5) - 1) * horizon);
      out[offset + i] = Math.pow(Math.max((lin + 0.1 * fex) * 0.04 + bias[i], 0), exponent);
    }
    return out;
  }

  function sceneOceanUniformData(ocean, environment, camera, timeSeconds, linear, quality, out) {
    out = out || new Float32Array(SCENE_OCEAN_VEC4S * 4);
    out.fill(0);
    if (quality !== "low" && quality !== "high") {
      var nav = typeof navigator !== "undefined" ? navigator : {};
      // @ts-ignore TS2339 -- deviceMemory is a Chromium-only Navigator field.
      quality = gosxLowEndHardware(nav.deviceMemory, nav.hardwareConcurrency) ? "low" : "high";
    }
    var o = ocean || {};
    out[0] = sceneNumber(o.level, 0); out[1] = sceneNumber(o.extent, 4000);
    out[2] = sceneNumber(timeSeconds, 0); out[3] = sceneNumber(o.speed, 1);
    sceneOceanLinear(o.deepColor || "#03141f", out, 4); out[7] = sceneNumber(o.clarity, 4);
    sceneOceanLinear(o.shallowColor || "#1f6f78", out, 8); out[11] = sceneNumber(o.roughness, 0.06);
    sceneOceanLinear(o.scatterColor || "#2fa58f", out, 12); out[15] = sceneNumber(o.foam, 0.6);
    sceneOceanLinear(o.foamColor || "#e9eef0", out, 16); out[19] = sceneNumber(o.surf, 0.5);
    var b = o.bathymetry;
    if (b && b.src) {
      out[20] = b.minX; out[21] = b.minZ; out[22] = b.maxX; out[23] = b.maxZ;
      if (b.encoding === "signed-sqrt") {
        out[24] = 0; out[25] = Math.max(Math.abs(b.minHeight), Math.abs(b.maxHeight)); out[26] = 2;
      } else {
        out[24] = b.minHeight; out[25] = b.maxHeight; out[26] = 1;
      }
    }
    var low = quality === "low";
    var waves = low ? 4 : 6;
    out[27] = waves;
    out[28] = sceneNumber(camera && camera.x, 0); out[29] = sceneNumber(camera && camera.y, 0);
    out[30] = sceneNumber(camera && camera.z, 0); out[31] = linear ? 1 : 0;
    var rings = low ? 96 : 192, segments = low ? 128 : 256, inner = 0.35;
    out[32] = rings; out[33] = segments; out[34] = inner; out[35] = Math.log(out[1] / inner + 1) / rings;
    window.__gosx_scene3d_ocean_waves.write(o, quality, out);
    var sky = environment && environment.sky;
    var skyEnv = sky ? environment : { sky: { mode: "gradient", topColor: "#6d8fb0", horizonColor: "#c8d6e0", bottomColor: "#c8d6e0" } };
    sceneSkyUniformData(sceneOceanSkyScratch, skyEnv, sceneOceanViewScratch, { fov: 60 }, 1, true);
    if (sky && sky.mode !== "physical") sceneOceanSkyScratch[23] = 0; // Reflect the gradient stops for image skies.
    out.set(sceneOceanSkyScratch, 84);
    var intensity = sceneOceanSkyScratch[15];
    if (sky && sky.mode === "physical") {
      var sp = sceneOceanSkyScratch.subarray(28, 44);
      sceneSkyPhysicalRadianceInto(sp, 0, 1, 0, out, 128);
      sceneSkyPhysicalRadianceInto(sp, sp[8], 0.08, sp[10], sceneOceanViewScratch, 0);
      sceneSkyPhysicalRadianceInto(sp, -sp[8], 0.08, -sp[10], sceneOceanViewScratch, 4);
      for (var a = 0; a < 3; a++) out[128 + a] = intensity * (0.6 * out[128 + a] + 0.25 * sceneOceanViewScratch[a] + 0.15 * sceneOceanViewScratch[4 + a]);
      // Sun radiance: sea-level transmittance along the sun ray times the sun's
      // illuminance, scaled to the sky's display-referred range.
      var zen = Math.acos(Math.max(0, Math.min(1, sp[9])));
      var inv = 1 / (Math.cos(zen) + 0.15 * Math.pow(93.885 - zen * 57.29577951, -1.253));
      for (var c = 0; c < 3; c++) out[132 + c] = intensity * Math.exp(-(sp[c] * 8400 * inv + sp[4 + c] * 1250 * inv)) * sp[3] * 0.012;
      out[136] = sp[8]; out[137] = sp[9]; out[138] = sp[10];
    } else {
      for (var b2 = 0; b2 < 3; b2++) out[128 + b2] = 0.6 * sceneOceanSkyScratch[12 + b2] * intensity + 0.4 * sceneOceanSkyScratch[16 + b2] * intensity;
      out[136] = 0.3; out[137] = 0.6; out[138] = -0.74; // No sun disk: a soft key light, no glint.
    }
    return out;
  }

  // sceneSkyPhysicalShaderSource returns the GLSL ("glsl") or WGSL ("wgsl")
  // gosxPhysicalSky function. A function, not a var, so the scene API object
  // can export it before this file's assignments run.
  function sceneSkyPhysicalShaderSource(kind) {
    return kind === "wgsl" ? SCENE_SKY_PHYSICAL_WGSL : SCENE_SKY_PHYSICAL_GLSL;
  }

  // sceneSkyPhysicalSource is what renderers call when they build a sky
  // pipeline. If the shared helper is absent (a renderer evaluated on its own)
  // it returns a stub that draws black, so gradient and environment skies
  // still compile.
  function sceneSkyPhysicalSource(kind) {
    if (typeof sceneSkyPhysicalShaderSource === "function") return sceneSkyPhysicalShaderSource(kind);
    return kind === "wgsl"
      ? "fn gosxPhysicalSky(dir: vec3f, betaR: vec4f, betaM: vec4f, sun: vec4f, g: f32) -> vec3f { return vec3f(0.0); }"
      : "vec3 gosxPhysicalSky(vec3 dir, vec4 betaR, vec4 betaM, vec4 sun, float g) { return vec3(0.); }";
  }

  // sceneSkyPhysicalParams writes the view-independent terms of the physical
  // sky (Preetham 1999; Hoffman and Preetham 2002) as four vec4s at offset:
  // [betaR.xyz, sunE], [betaM.xyz, sunFade], [sun.xyz, diskCos], [g, 0, 0, 0].
  // scene/sky_physical.go (newPhysicalSkyParams) computes the same values;
  // scene3d-sky-physical.test.js pins both to one golden block.
  function sceneSkyPhysicalParams(sky, out, offset) {
    var sun = sky.sunDirection || {}, sx = sceneNumber(sun.x, 0), sy = sceneNumber(sun.y, 0), sz = sceneNumber(sun.z, 0);
    var len = Math.sqrt(sx * sx + sy * sy + sz * sz);
    if (!(len > 1e-9) || !isFinite(len)) { // Default: 6 degrees above the horizon over -Z.
      sx = 0; sy = Math.sin(6 * Math.PI / 180); sz = -Math.cos(6 * Math.PI / 180); len = 1;
    }
    sx /= len; sy /= len; sz /= len;
    var pick = function(v, d) { v = sceneNumber(v, 0); return v === 0 ? d : v; };
    var turbidity = pick(sky.turbidity, 10), rayleigh = pick(sky.rayleigh, 2);
    var mie = pick(sky.mieCoefficient, 0.005), g = pick(sky.mieDirectionalG, 0.8), disk = pick(sky.sunDiskRadius, 0.53);
    var zenith = Math.acos(Math.max(-1, Math.min(1, sy)));
    var sunE = 1000 * Math.max(0, 1 - Math.exp(-((Math.PI / 1.95 - zenith) / 1.5)));
    var sunFade = 1 - Math.max(0, Math.min(1, 1 - Math.exp(sy)));
    var rayleighCoefficient = Math.max(0, rayleigh - (1 - sunFade)), c = 0.2 * turbidity * 10e-18;
    var totalRayleigh = [5.804542996261093e-6, 1.3562911419845635e-5, 3.0265902468824876e-5];
    var mieConst = [1.8399918514433978e14, 2.7798023919660528e14, 4.0790479543861094e14];
    for (var i = 0; i < 3; i++) {
      out[offset + i] = totalRayleigh[i] * rayleighCoefficient;
      out[offset + 4 + i] = 0.434 * c * mieConst[i] * mie;
    }
    out[offset + 3] = sunE; out[offset + 7] = sunFade;
    out[offset + 8] = sx; out[offset + 9] = sy; out[offset + 10] = sz;
    out[offset + 11] = disk < 0 ? 2 : Math.cos(disk * Math.PI / 180);
    out[offset + 12] = g; out[offset + 13] = 0; out[offset + 14] = 0; out[offset + 15] = 0;
    return out;
  }

  // --- Camera matrices ---

  // Build a 4x4 view matrix from camera position and Euler rotation.
  //
  // The GoSX camera convention: the camera has position (x, y, z) and Euler
  // angles (rotationX, rotationY, rotationZ). The shared Scene3D contract
  // shifts world points by (-camX, -camY, -camZ) then applies inverse
  // rotation. Positive forward depth is -viewZ.
  //
  // To produce a standard 4x4 view matrix we construct:
  //   V = inverseRotation * translation(-camX, -camY, -camZ)
  //
  // The inverse rotation is computed by applying -rotZ, -rotY, -rotX
  // (reverse order, negative angles) — matching sceneInverseRotatePoint.
  // Build a 4x4 view matrix into `out` (or a new Float32Array if omitted).
  function scenePBRViewMatrix(camera, out) {
    const cam = sceneRenderCamera(camera);
    const tx = -cam.x;
    const ty = -cam.y;
    const tz = -cam.z;

    // Inverse Euler: apply -rotZ, then -rotY, then -rotX.
    const sx = Math.sin(-cam.rotationX);
    const cx = Math.cos(-cam.rotationX);
    const sy = Math.sin(-cam.rotationY);
    const cy = Math.cos(-cam.rotationY);
    const sz = Math.sin(-cam.rotationZ);
    const cz = Math.cos(-cam.rotationZ);

    // Rotation matrix = Rx(-rx) * Ry(-ry) * Rz(-rz), matching
    // sceneInverseRotatePoint's scalar sequence exactly.
    // Column-major order for WebGL.
    const r00 = cy * cz;
    const r01 = -cy * sz;
    const r02 = sy;

    const r10 = cx * sz + sx * sy * cz;
    const r11 = cx * cz - sx * sy * sz;
    const r12 = -sx * cy;

    const r20 = sx * sz - cx * sy * cz;
    const r21 = sx * cz + cx * sy * sz;
    const r22 = cx * cy;

    // Translation part: R * t
    const d0 = r00 * tx + r01 * ty + r02 * tz;
    const d1 = r10 * tx + r11 * ty + r12 * tz;
    const d2 = r20 * tx + r21 * ty + r22 * tz;

    // Column-major 4x4 matrix as Float32Array.
    var m = out || new Float32Array(16);
    m[0] = r00; m[1] = r10; m[2] = r20; m[3] = 0;
    m[4] = r01; m[5] = r11; m[6] = r21; m[7] = 0;
    m[8] = r02; m[9] = r12; m[10] = r22; m[11] = 0;
    m[12] = d0; m[13] = d1; m[14] = d2; m[15] = 1;
    return m;
  }

  // Build a perspective projection matrix into `out` (or a new Float32Array).
  // fov is in degrees, matching sceneRenderCamera output.
  function scenePBRProjectionMatrix(fov, aspect, near, far, out) {
    const fovRad = (fov * Math.PI) / 180;
    const f = 1 / Math.tan(fovRad * 0.5);
    const rangeInv = 1 / (near - far);

    // Column-major.
    var m = out || new Float32Array(16);
    m[0] = f / aspect; m[1] = 0; m[2] = 0; m[3] = 0;
    m[4] = 0; m[5] = f; m[6] = 0; m[7] = 0;
    m[8] = 0; m[9] = 0; m[10] = (near + far) * rangeInv; m[11] = -1;
    m[12] = 0; m[13] = 0; m[14] = 2 * near * far * rangeInv; m[15] = 0;
    return m;
  }

  function scenePBROrthographicProjectionMatrix(left, right, top, bottom, near, far, out) {
    var m = out || new Float32Array(16);
    const width = Math.max(0.000001, right - left);
    const height = Math.max(0.000001, top - bottom);
    const depth = Math.max(0.000001, far - near);
    m[0] = 2 / width; m[1] = 0; m[2] = 0; m[3] = 0;
    m[4] = 0; m[5] = 2 / height; m[6] = 0; m[7] = 0;
    m[8] = 0; m[9] = 0; m[10] = -2 / depth; m[11] = 0;
    m[12] = -(right + left) / width; m[13] = -(top + bottom) / height; m[14] = -(far + near) / depth; m[15] = 1;
    return m;
  }

  function scenePBRProjectionMatrixForCamera(camera, aspect, out) {
    const cam = sceneRenderCamera(camera);
    if (cam.kind === "orthographic") {
      const bounds = sceneOrthographicBounds(cam, Math.max(1, aspect * 1000), 1000);
      return scenePBROrthographicProjectionMatrix(bounds.left, bounds.right, bounds.top, bounds.bottom, cam.near, cam.far, out);
    }
    return scenePBRProjectionMatrix(cam.fov, aspect, cam.near, cam.far, out);
  }

  // --- Shadow Map Infrastructure ---

  // Build one perspective shadow matrix for a normalized spot light. The
  // public light normalizer supplies the established zero-value defaults;
  // this lower-level helper is deliberately fail-closed for values that
  // cannot be represented by one planar map. In particular, a half-angle at
  // or above 90 degrees needs multiple faces and therefore consumes no slot.
  function sceneSpotShadowLightSpaceMatrix(light, sceneBounds) {
    var px = sceneNumber(light && light.x, NaN);
    var py = sceneNumber(light && light.y, NaN);
    var pz = sceneNumber(light && light.z, NaN);
    var dx = sceneNumber(light && light.directionX, NaN);
    var dy = sceneNumber(light && light.directionY, NaN);
    var dz = sceneNumber(light && light.directionZ, NaN);
    var angle = sceneNumber(light && light.angle, NaN);
    var range = sceneNumber(light && light.range, 0);
    if (!isFinite(px) || !isFinite(py) || !isFinite(pz) ||
        !isFinite(dx) || !isFinite(dy) || !isFinite(dz) ||
        !isFinite(angle) || !(angle > 0) || angle >= Math.PI / 2) {
      return null;
    }

    // Normalize after scaling by the largest component. This stays stable
    // for very large and very small finite vectors where a direct square sum
    // would overflow or underflow.
    var scale = Math.max(Math.abs(dx), Math.abs(dy), Math.abs(dz));
    if (!(scale > 0)) return null;
    var sx = dx / scale;
    var sy = dy / scale;
    var sz = dz / scale;
    var subLength = Math.sqrt(sx * sx + sy * sy + sz * sz);
    dx = sx / subLength;
    dy = sy / subLength;
    dz = sz / subLength;

    // Cover the furthest scene-bounds corner in front of the light. A finite
    // positive authored range may tighten that extent. The near strategy is
    // intentionally small but finite; geometry closer than it is outside the
    // documented shadow volume.
    var far = 0;
    var boundCoordinateScale = 0;
    if (sceneBounds &&
        isFinite(sceneBounds.minX) && isFinite(sceneBounds.maxX) &&
        isFinite(sceneBounds.minY) && isFinite(sceneBounds.maxY) &&
        isFinite(sceneBounds.minZ) && isFinite(sceneBounds.maxZ)) {
      boundCoordinateScale = Math.max(
        Math.abs(sceneBounds.minX), Math.abs(sceneBounds.maxX),
        Math.abs(sceneBounds.minY), Math.abs(sceneBounds.maxY),
        Math.abs(sceneBounds.minZ), Math.abs(sceneBounds.maxZ));
      for (var corner = 0; corner < 8; corner++) {
        var bx = (corner & 1) ? sceneBounds.maxX : sceneBounds.minX;
        var by = (corner & 2) ? sceneBounds.maxY : sceneBounds.minY;
        var bz = (corner & 4) ? sceneBounds.maxZ : sceneBounds.minZ;
        var depth = (bx - px) * dx + (by - py) * dy + (bz - pz) * dz;
        if (isFinite(depth) && depth > far) far = depth;
      }
    }
    if (!(far > 0)) far = 10;
    if (range > 0 && isFinite(range)) far = Math.min(far, range);
    if (!isFinite(far) || !(far > 0)) return null;
    var near = Math.min(0.1, Math.max(0.01, far * 0.001));
    if (near >= far) near = far * 0.5;

    // Leave a conservative finite-precision guard beyond the logical far
    // depth before producing Float32 view/projection matrices. Exact fits can
    // round a boundary corner just past clip Z in the shader, especially when
    // world-space translation is large or far/near is wide. The first term
    // covers view-space multiply/add error; the second covers perspective
    // coefficient quantization. An authored range remains the logical light
    // limit (the BRDF still enforces it): only this projection guard extends
    // beyond it. Refuse a numerically ill-conditioned projection rather than
    // allowing padding to grow beyond one eighth of the selected depth.
    var float32Epsilon = 1.1920928955078125e-7;
    var coordinateScale = Math.abs(px) + Math.abs(py) + Math.abs(pz) + boundCoordinateScale + far;
    var viewPadding = coordinateScale * 64 * float32Epsilon;
    var projectionPadding = far * Math.max(1, far / near) * 64 * float32Epsilon;
    var farPadding = Math.max(far * 64 * float32Epsilon, viewPadding, projectionPadding);
    if (!isFinite(farPadding) || farPadding > far * 0.125) return null;
    far += farPadding;
    if (!isFinite(far)) return null;

    var fx = dx, fy = dy, fz = dz;
    var upX = 0, upY = 1, upZ = 0;
    if (Math.abs(fy) > 0.99) {
      upX = 0; upY = 0; upZ = 1;
    }
    var rx = fy * upZ - fz * upY;
    var ry = fz * upX - fx * upZ;
    var rz = fx * upY - fy * upX;
    var rightLength = Math.sqrt(rx * rx + ry * ry + rz * rz);
    if (!(rightLength > 0.0001)) return null;
    rx /= rightLength; ry /= rightLength; rz /= rightLength;
    upX = ry * fz - rz * fy;
    upY = rz * fx - rx * fz;
    upZ = rx * fy - ry * fx;

    var view = new Float32Array([
      rx, upX, -fx, 0,
      ry, upY, -fy, 0,
      rz, upZ, -fz, 0,
      -(rx * px + ry * py + rz * pz),
      -(upX * px + upY * py + upZ * pz),
      fx * px + fy * py + fz * pz,
      1,
    ]);
    var tanHalf = Math.tan(angle);
    var rangeInv = 1 / (near - far);
    var projection = new Float32Array([
      1 / tanHalf, 0, 0, 0,
      0, 1 / tanHalf, 0, 0,
      0, 0, (near + far) * rangeInv, -1,
      0, 0, 2 * near * far * rangeInv, 0,
    ]);
    var matrix = sceneMat4Multiply(projection, view);
    for (var i = 0; i < 16; i++) {
      if (!isFinite(matrix[i])) return null;
    }
    return matrix;
  }

  // Compute an orthographic light-space matrix for a directional light.
  // sceneBounds is { minX, minY, minZ, maxX, maxY, maxZ }.
  function sceneShadowLightSpaceMatrix(light, sceneBounds) {
    if (light && typeof light.kind === "string" && light.kind.toLowerCase() === "spot") {
      return sceneSpotShadowLightSpaceMatrix(light, sceneBounds);
    }
    // Light direction (normalized).
    var dx = sceneNumber(light.directionX, 0);
    var dy = sceneNumber(light.directionY, -1);
    var dz = sceneNumber(light.directionZ, 0);
    var len = Math.sqrt(dx * dx + dy * dy + dz * dz);
    if (len < 0.0001) {
      dx = 0; dy = -1; dz = 0; len = 1;
    }
    dx /= len; dy /= len; dz /= len;

    // Scene center and radius from AABB.
    var cx = (sceneBounds.minX + sceneBounds.maxX) * 0.5;
    var cy = (sceneBounds.minY + sceneBounds.maxY) * 0.5;
    var cz = (sceneBounds.minZ + sceneBounds.maxZ) * 0.5;
    var ex = (sceneBounds.maxX - sceneBounds.minX) * 0.5;
    var ey = (sceneBounds.maxY - sceneBounds.minY) * 0.5;
    var ez = (sceneBounds.maxZ - sceneBounds.minZ) * 0.5;
    var radius = Math.sqrt(ex * ex + ey * ey + ez * ez);
    if (radius < 0.01) radius = 10;

    // Position the light camera behind the scene center along the light direction.
    var eyeX = cx - dx * radius * 2;
    var eyeY = cy - dy * radius * 2;
    var eyeZ = cz - dz * radius * 2;

    // Build a lookAt view matrix (light looking at scene center).
    // Forward = normalize(center - eye) = (dx, dy, dz).
    var fx = dx, fy = dy, fz = dz;

    // Choose an up vector not parallel to forward.
    var upX = 0, upY = 1, upZ = 0;
    if (Math.abs(fy) > 0.99) {
      upX = 0; upY = 0; upZ = 1;
    }

    // Right = normalize(forward x up).
    var rx = fy * upZ - fz * upY;
    var ry = fz * upX - fx * upZ;
    var rz = fx * upY - fy * upX;
    var rLen = Math.sqrt(rx * rx + ry * ry + rz * rz);
    if (rLen < 0.0001) rLen = 1;
    rx /= rLen; ry /= rLen; rz /= rLen;

    // Recompute up = right x forward.
    upX = ry * fz - rz * fy;
    upY = rz * fx - rx * fz;
    upZ = rx * fy - ry * fx;

    // View matrix (column-major).
    var tx = -(rx * eyeX + ry * eyeY + rz * eyeZ);
    var ty = -(upX * eyeX + upY * eyeY + upZ * eyeZ);
    var tz = -(fx * eyeX + fy * eyeY + fz * eyeZ);

    // Note: forward is positive — we look along +forward, so no negation.
    var view = new Float32Array([
      rx,  upX, fx,  0,
      ry,  upY, fy,  0,
      rz,  upZ, fz,  0,
      tx,  ty,  tz,  1,
    ]);

    // Orthographic projection matrix (column-major).
    // Maps [-radius, radius] in all axes to [-1, 1] clip space.
    var near = 0.01;
    var far = radius * 4;
    var l = -radius, rr = radius, b = -radius, t = radius;
    var proj = new Float32Array([
      2 / (rr - l),     0,              0,                    0,
      0,                2 / (t - b),    0,                    0,
      0,                0,              -2 / (far - near),    0,
      -(rr + l) / (rr - l), -(t + b) / (t - b), -(far + near) / (far - near), 1,
    ]);

    // Multiply proj * view (column-major).
    return sceneMat4Multiply(proj, view);
  }

  // Compute the AABB of all objects in the bundle.
  function sceneShadowComputeBounds(bundle) {
    var minX = Infinity, minY = Infinity, minZ = Infinity;
    var maxX = -Infinity, maxY = -Infinity, maxZ = -Infinity;
    var positions = bundle.worldMeshPositions;
    var objects = Array.isArray(bundle.meshObjects) ? bundle.meshObjects : [];

    for (var i = 0; i < objects.length; i++) {
      var obj = objects[i];
      if (!obj || obj.viewCulled) continue;
      if (obj.directVertices) {
        // Retained casters draw from model-space vertex buffers and never land
        // in the baked soup, so fold their transformed world bounds into the
        // light-frustum fit instead of skipping them outright.
        if (!obj.retainedGeometry || !obj.castShadow) continue;
        var casterBounds = obj.bounds;
        if (
          casterBounds &&
          isFinite(casterBounds.minX) && isFinite(casterBounds.minY) && isFinite(casterBounds.minZ) &&
          isFinite(casterBounds.maxX) && isFinite(casterBounds.maxY) && isFinite(casterBounds.maxZ)
        ) {
          if (casterBounds.minX < minX) minX = casterBounds.minX;
          if (casterBounds.minY < minY) minY = casterBounds.minY;
          if (casterBounds.minZ < minZ) minZ = casterBounds.minZ;
          if (casterBounds.maxX > maxX) maxX = casterBounds.maxX;
          if (casterBounds.maxY > maxY) maxY = casterBounds.maxY;
          if (casterBounds.maxZ > maxZ) maxZ = casterBounds.maxZ;
        }
        continue;
      }
      var offset = obj.vertexOffset;
      var count = obj.vertexCount;
      if (!Number.isFinite(offset) || !Number.isFinite(count) || count <= 0) continue;

      for (var v = 0; v < count; v++) {
        var idx = (offset + v) * 3;
        var px = positions[idx];
        var py = positions[idx + 1];
        var pz = positions[idx + 2];
        if (px < minX) minX = px;
        if (py < minY) minY = py;
        if (pz < minZ) minZ = pz;
        if (px > maxX) maxX = px;
        if (py > maxY) maxY = py;
        if (pz > maxZ) maxZ = pz;
      }
    }

    if (!isFinite(minX)) {
      return { minX: -10, minY: -10, minZ: -10, maxX: 10, maxY: 10, maxZ: 10 };
    }
    return { minX: minX, minY: minY, minZ: minZ, maxX: maxX, maxY: maxY, maxZ: maxZ };
  }

  // Generate PBR vertex data (positions, normals, UVs, tangents) for a geometry
  // kind. Returns { positions, normals, uvs, tangents, vertexCount } where each
  // array is a Float32Array ready for GPU upload.
  function generateInstancedGeometry(kind, dims) {
    kind = normalizeInstancedGeometryKind(kind);
    var w = sceneNumber(dims && dims.width, 1);
    var h = sceneNumber(dims && dims.height, 1);
    var d = sceneNumber(dims && dims.depth, 1);
    var size = sceneNumber(dims && dims.size, 0);
    if (kind === "cube" && size > 0) {
      w = size;
      h = size;
      d = size;
    }

    if (kind === "sphere") {
      return generateInstancedSphereGeometry(
        sceneNumber(dims && dims.radius, 0.5),
        sceneNumber(dims && dims.segments, 32)
      );
    }
    if (kind === "plane") {
      return generateInstancedPlaneGeometry(w, d);
    }
    if (kind === "pyramid") {
      return generateInstancedPyramidGeometry(w, h, d);
    }
    if (kind === "cylinder") {
      return generateInstancedCylinderGeometry(
        sceneNumber(dims && dims.radiusTop, sceneNumber(dims && dims.radius, 0.5)),
        sceneNumber(dims && dims.radiusBottom, sceneNumber(dims && dims.radius, 0.5)),
        h,
        sceneNumber(dims && dims.segments, 32)
      );
    }
    if (kind === "cone") {
      return generateInstancedCylinderGeometry(
        0,
        sceneNumber(dims && dims.radiusBottom, sceneNumber(dims && dims.radius, 0.5)),
        h,
        sceneNumber(dims && dims.segments, 32)
      );
    }
    if (kind === "torus") {
      return generateInstancedTorusGeometry(
        sceneNumber(dims && dims.radius, 0.7),
        sceneNumber(dims && dims.tube, 0.3),
        sceneNumber(dims && dims.radialSegments, 32),
        sceneNumber(dims && dims.tubularSegments, 16)
      );
    }

    // Default: box geometry.
    return generateInstancedBoxGeometry(w, h, d);
  }

  function normalizeInstancedGeometryKind(kind) {
    if (typeof normalizeSceneKind === "function") {
      return normalizeSceneKind(kind);
    }
    var text = typeof kind === "string" ? kind.trim().toLowerCase() : "";
    switch (text) {
      case "cubegeometry":
        return "cube";
      case "boxgeometry":
        return "box";
      case "planegeometry":
      case "quad":
      case "quadgeometry":
        return "plane";
      case "pyramidgeometry":
        return "pyramid";
      case "spheregeometry":
      case "uvsphere":
      case "uvspheregeometry":
        return "sphere";
      case "cylindergeometry":
        return "cylinder";
      case "conegeometry":
        return "cone";
      case "torusgeometry":
        return "torus";
      case "torusknotgeometry":
      case "torus-knot":
        return "torusknot";
      default:
        return text || "box";
    }
  }

  // Generate a unit box with the given dimensions. 36 vertices (12 triangles).
  // Each face has outward normals, [0,1] UVs, and MikkTSpace-compatible tangents.
  function generateInstancedBoxGeometry(w, h, d) {
    var hw = w * 0.5, hh = h * 0.5, hd = d * 0.5;

    // 6 faces × 2 triangles × 3 vertices = 36 vertices.
    // Each face: normal, tangent(vec4), 4 corners → 2 triangles.
    var faces = [
      // +Z face (front)
      { n: [0, 0, 1], t: [1, 0, 0, 1], v: [[-hw,-hh,hd],[hw,-hh,hd],[hw,hh,hd],[-hw,hh,hd]] },
      // -Z face (back)
      { n: [0, 0,-1], t: [-1, 0, 0, 1], v: [[hw,-hh,-hd],[-hw,-hh,-hd],[-hw,hh,-hd],[hw,hh,-hd]] },
      // +X face (right)
      { n: [1, 0, 0], t: [0, 0,-1, 1], v: [[hw,-hh,hd],[hw,-hh,-hd],[hw,hh,-hd],[hw,hh,hd]] },
      // -X face (left)
      { n: [-1, 0, 0], t: [0, 0, 1, 1], v: [[-hw,-hh,-hd],[-hw,-hh,hd],[-hw,hh,hd],[-hw,hh,-hd]] },
      // +Y face (top)
      { n: [0, 1, 0], t: [1, 0, 0, 1], v: [[-hw,hh,hd],[hw,hh,hd],[hw,hh,-hd],[-hw,hh,-hd]] },
      // -Y face (bottom)
      { n: [0,-1, 0], t: [1, 0, 0, 1], v: [[-hw,-hh,-hd],[hw,-hh,-hd],[hw,-hh,hd],[-hw,-hh,hd]] },
    ];

    var quadUVs = [[0,0],[1,0],[1,1],[0,1]];
    var triIndices = [0,1,2, 0,2,3];

    var vertexCount = 36;
    var positions = new Float32Array(vertexCount * 3);
    var normals = new Float32Array(vertexCount * 3);
    var uvs = new Float32Array(vertexCount * 2);
    var tangents = new Float32Array(vertexCount * 4);

    var vi = 0;
    for (var fi = 0; fi < 6; fi++) {
      var face = faces[fi];
      for (var ti = 0; ti < 6; ti++) {
        var ci = triIndices[ti];
        var p = face.v[ci];
        positions[vi * 3]     = p[0];
        positions[vi * 3 + 1] = p[1];
        positions[vi * 3 + 2] = p[2];
        normals[vi * 3]     = face.n[0];
        normals[vi * 3 + 1] = face.n[1];
        normals[vi * 3 + 2] = face.n[2];
        uvs[vi * 2]     = quadUVs[ci][0];
        uvs[vi * 2 + 1] = quadUVs[ci][1];
        tangents[vi * 4]     = face.t[0];
        tangents[vi * 4 + 1] = face.t[1];
        tangents[vi * 4 + 2] = face.t[2];
        tangents[vi * 4 + 3] = face.t[3];
        vi++;
      }
    }

    return { positions: positions, normals: normals, uvs: uvs, tangents: tangents, vertexCount: vertexCount };
  }

  // Generate a plane (quad) with the given width and depth, lying in the XZ plane.
  // 6 vertices (2 triangles), face normal pointing up (+Y).
  function generateInstancedPlaneGeometry(w, d) {
    var hw = w * 0.5, hd = d * 0.5;
    var vertexCount = 6;
    var positions = new Float32Array(vertexCount * 3);
    var normals = new Float32Array(vertexCount * 3);
    var uvs = new Float32Array(vertexCount * 2);
    var tangents = new Float32Array(vertexCount * 4);

    var corners = [[-hw, 0, hd], [hw, 0, hd], [hw, 0, -hd], [-hw, 0, -hd]];
    var cornerUVs = [[0, 0], [1, 0], [1, 1], [0, 1]];
    var triIndices = [0, 1, 2, 0, 2, 3];

    for (var i = 0; i < 6; i++) {
      var ci = triIndices[i];
      var p = corners[ci];
      positions[i * 3] = p[0]; positions[i * 3 + 1] = p[1]; positions[i * 3 + 2] = p[2];
      normals[i * 3] = 0; normals[i * 3 + 1] = 1; normals[i * 3 + 2] = 0;
      uvs[i * 2] = cornerUVs[ci][0]; uvs[i * 2 + 1] = cornerUVs[ci][1];
      tangents[i * 4] = 1; tangents[i * 4 + 1] = 0; tangents[i * 4 + 2] = 0; tangents[i * 4 + 3] = 1;
    }

    return { positions: positions, normals: normals, uvs: uvs, tangents: tangents, vertexCount: vertexCount };
  }

  // Generate a UV sphere with the given radius and segment count.
  function generateInstancedSphereGeometry(radius, segments) {
    var slices = instancedSegmentCount(segments, 32, 3, 256);
    var rings = Math.max(2, Math.floor(slices / 2));

    // Count: each ring-slice quad = 2 triangles = 6 vertices,
    // except the top and bottom caps which are single triangles.
    var vertexCount = rings * slices * 6;
    var positions = new Float32Array(vertexCount * 3);
    var normals = new Float32Array(vertexCount * 3);
    var uvs = new Float32Array(vertexCount * 2);
    var tangents = new Float32Array(vertexCount * 4);
    var vi = 0;

    function spherePoint(ring, slice) {
      var phi = (ring / rings) * Math.PI;
      var theta = (slice / slices) * Math.PI * 2;
      var sp = Math.sin(phi);
      var nx = sp * Math.cos(theta);
      var ny = Math.cos(phi);
      var nz = sp * Math.sin(theta);
      return {
        px: nx * radius, py: ny * radius, pz: nz * radius,
        nx: nx, ny: ny, nz: nz,
        u: slice / slices, v: ring / rings,
        tx: -Math.sin(theta), ty: 0, tz: Math.cos(theta),
      };
    }

    function pushVert(pt) {
      positions[vi * 3] = pt.px; positions[vi * 3 + 1] = pt.py; positions[vi * 3 + 2] = pt.pz;
      normals[vi * 3] = pt.nx; normals[vi * 3 + 1] = pt.ny; normals[vi * 3 + 2] = pt.nz;
      uvs[vi * 2] = pt.u; uvs[vi * 2 + 1] = pt.v;
      tangents[vi * 4] = pt.tx; tangents[vi * 4 + 1] = pt.ty; tangents[vi * 4 + 2] = pt.tz; tangents[vi * 4 + 3] = 1;
      vi++;
    }

    for (var r = 0; r < rings; r++) {
      for (var s = 0; s < slices; s++) {
        var a = spherePoint(r, s);
        var b = spherePoint(r, s + 1);
        var c = spherePoint(r + 1, s + 1);
        var dd = spherePoint(r + 1, s);
        pushVert(a); pushVert(b); pushVert(c);
        pushVert(a); pushVert(c); pushVert(dd);
      }
    }

    return { positions: positions, normals: normals, uvs: uvs, tangents: tangents, vertexCount: vi };
  }

  function instancedSegmentCount(value, fallback, minValue, maxValue) {
    var count = Math.round(sceneNumber(value, fallback));
    return Math.max(minValue, Math.min(maxValue, count));
  }

  function instancedPositiveNumber(value, fallback) {
    var number = sceneNumber(value, fallback);
    return number > 0 ? number : fallback;
  }

  function instancedNormalize3(x, y, z) {
    var length = Math.sqrt(x * x + y * y + z * z);
    if (!Number.isFinite(length) || length <= 0.000001) {
      return [0, 1, 0];
    }
    return [x / length, y / length, z / length];
  }

  function instancedTriangleNormal(a, b, c) {
    var abx = b[0] - a[0];
    var aby = b[1] - a[1];
    var abz = b[2] - a[2];
    var acx = c[0] - a[0];
    var acy = c[1] - a[1];
    var acz = c[2] - a[2];
    return instancedNormalize3(
      aby * acz - abz * acy,
      abz * acx - abx * acz,
      abx * acy - aby * acx
    );
  }

  function createInstancedGeometryWriter(vertexCount) {
    var positions = new Float32Array(vertexCount * 3);
    var normals = new Float32Array(vertexCount * 3);
    var uvs = new Float32Array(vertexCount * 2);
    var tangents = new Float32Array(vertexCount * 4);
    var vi = 0;
    function push(position, normal, uv, tangent) {
      positions[vi * 3] = position[0];
      positions[vi * 3 + 1] = position[1];
      positions[vi * 3 + 2] = position[2];
      normals[vi * 3] = normal[0];
      normals[vi * 3 + 1] = normal[1];
      normals[vi * 3 + 2] = normal[2];
      uvs[vi * 2] = uv[0];
      uvs[vi * 2 + 1] = uv[1];
      tangents[vi * 4] = tangent[0];
      tangents[vi * 4 + 1] = tangent[1];
      tangents[vi * 4 + 2] = tangent[2];
      tangents[vi * 4 + 3] = tangent[3];
      vi++;
    }
    function build() {
      return {
        positions: vi * 3 === positions.length ? positions : positions.subarray(0, vi * 3),
        normals: vi * 3 === normals.length ? normals : normals.subarray(0, vi * 3),
        uvs: vi * 2 === uvs.length ? uvs : uvs.subarray(0, vi * 2),
        tangents: vi * 4 === tangents.length ? tangents : tangents.subarray(0, vi * 4),
        vertexCount: vi,
      };
    }
    return { push: push, build: build };
  }

  function pushInstancedFlatTri(writer, p0, p1, p2, uv0, uv1, uv2) {
    var normal = instancedTriangleNormal(p0, p1, p2);
    var tangent3 = instancedNormalize3(p1[0] - p0[0], p1[1] - p0[1], p1[2] - p0[2]);
    var tangent = [tangent3[0], tangent3[1], tangent3[2], 1];
    writer.push(p0, normal, uv0, tangent);
    writer.push(p1, normal, uv1, tangent);
    writer.push(p2, normal, uv2, tangent);
  }

  function generateInstancedPyramidGeometry(w, h, d) {
    var hw = instancedPositiveNumber(w, 1) * 0.5;
    var hh = instancedPositiveNumber(h, 1) * 0.5;
    var hd = instancedPositiveNumber(d, 1) * 0.5;
    var base = [[-hw, -hh, -hd], [hw, -hh, -hd], [hw, -hh, hd], [-hw, -hh, hd]];
    var apex = [0, hh, 0];
    var writer = createInstancedGeometryWriter(18);

    pushInstancedFlatTri(writer, base[0], base[1], base[2], [0, 0], [1, 0], [1, 1]);
    pushInstancedFlatTri(writer, base[0], base[2], base[3], [0, 0], [1, 1], [0, 1]);
    for (var i = 0; i < 4; i++) {
      pushInstancedFlatTri(writer, base[i], apex, base[(i + 1) % 4], [0, 1], [0.5, 0], [1, 1]);
    }
    return writer.build();
  }

  function generateInstancedCylinderGeometry(radiusTop, radiusBottom, height, segments) {
    var rt = Math.max(0, sceneNumber(radiusTop, 0.5));
    var rb = Math.max(0, sceneNumber(radiusBottom, 0.5));
    var h = instancedPositiveNumber(height, 1);
    if (rt === 0 && rb === 0) {
      rb = 0.5;
    }
    var count = instancedSegmentCount(segments, 32, 3, 256);
    var vertsPerSegment = (rt > 0 && rb > 0 ? 6 : 3) + (rb > 0 ? 3 : 0) + (rt > 0 ? 3 : 0);
    var writer = createInstancedGeometryWriter(count * vertsPerSegment);
    var halfH = h * 0.5;
    var slopeY = (rb - rt) / h;

    for (var i = 0; i < count; i++) {
      var u0 = i / count;
      var u1 = (i + 1) / count;
      var th0 = (Math.PI * 2 * i) / count;
      var th1 = (Math.PI * 2 * (i + 1)) / count;
      var c0 = Math.cos(th0);
      var s0 = Math.sin(th0);
      var c1 = Math.cos(th1);
      var s1 = Math.sin(th1);
      var n0 = instancedNormalize3(c0, slopeY, s0);
      var n1 = instancedNormalize3(c1, slopeY, s1);
      var t0 = [-s0, 0, c0, 1];
      var t1 = [-s1, 0, c1, 1];
      var b0 = [rb * c0, -halfH, rb * s0];
      var b1 = [rb * c1, -halfH, rb * s1];
      var top0 = [rt * c0, halfH, rt * s0];
      var top1 = [rt * c1, halfH, rt * s1];

      if (rb > 0 && rt > 0) {
        writer.push(b0, n0, [u0, 1], t0);
        writer.push(top1, n1, [u1, 0], t1);
        writer.push(b1, n1, [u1, 1], t1);
        writer.push(b0, n0, [u0, 1], t0);
        writer.push(top0, n0, [u0, 0], t0);
        writer.push(top1, n1, [u1, 0], t1);
      } else if (rt === 0) {
        writer.push(b0, n0, [u0, 1], t0);
        writer.push(top1, n1, [u1, 0], t1);
        writer.push(b1, n1, [u1, 1], t1);
      } else {
        writer.push(b0, n0, [u0, 1], t0);
        writer.push(top0, n0, [u0, 0], t0);
        writer.push(top1, n1, [u1, 0], t1);
      }

      if (rb > 0) {
        writer.push([0, -halfH, 0], [0, -1, 0], [0.5, 0.5], [1, 0, 0, 1]);
        writer.push(b0, [0, -1, 0], [0.5 + c0 * 0.5, 0.5 + s0 * 0.5], [1, 0, 0, 1]);
        writer.push(b1, [0, -1, 0], [0.5 + c1 * 0.5, 0.5 + s1 * 0.5], [1, 0, 0, 1]);
      }
      if (rt > 0) {
        writer.push([0, halfH, 0], [0, 1, 0], [0.5, 0.5], [1, 0, 0, 1]);
        writer.push(top1, [0, 1, 0], [0.5 + c1 * 0.5, 0.5 + s1 * 0.5], [1, 0, 0, 1]);
        writer.push(top0, [0, 1, 0], [0.5 + c0 * 0.5, 0.5 + s0 * 0.5], [1, 0, 0, 1]);
      }
    }
    return writer.build();
  }

  function generateInstancedTorusGeometry(radius, tube, radialSegments, tubularSegments) {
    var major = instancedPositiveNumber(radius, 0.7);
    var minor = instancedPositiveNumber(tube, 0.3);
    var radial = instancedSegmentCount(radialSegments, 32, 3, 256);
    var tubular = instancedSegmentCount(tubularSegments, 16, 3, 128);
    var writer = createInstancedGeometryWriter(radial * tubular * 6);

    function vertexAt(i, j) {
      var u = (Math.PI * 2 * i) / radial;
      var v = (Math.PI * 2 * j) / tubular;
      var cu = Math.cos(u);
      var su = Math.sin(u);
      var cv = Math.cos(v);
      var sv = Math.sin(v);
      var r = major + minor * cv;
      return {
        position: [r * cu, minor * sv, r * su],
        normal: instancedNormalize3(cv * cu, sv, cv * su),
        uv: [i / radial, j / tubular],
        tangent: [-su, 0, cu, 1],
      };
    }

    function pushTorusVertex(v) {
      writer.push(v.position, v.normal, v.uv, v.tangent);
    }

    for (var i = 0; i < radial; i++) {
      for (var j = 0; j < tubular; j++) {
        var a = vertexAt(i, j);
        var b = vertexAt(i, j + 1);
        var c = vertexAt(i + 1, j);
        var dd = vertexAt(i + 1, j + 1);
        pushTorusVertex(a);
        pushTorusVertex(b);
        pushTorusVertex(c);
        pushTorusVertex(c);
        pushTorusVertex(b);
        pushTorusVertex(dd);
      }
    }
    return writer.build();
  }

  // Shared scratch for number → u32 bit reinterpretation used by the light
  // hash. Allocated once at module level; safe because the hash function
  // is called synchronously per upload and never recursively.
  var _scenePBRLightsHashBuf = new ArrayBuffer(4);

  var _scenePBRLightsHashFloat = new Float32Array(_scenePBRLightsHashBuf);

  var _scenePBRLightsHashInt = new Uint32Array(_scenePBRLightsHashBuf);

  function scenePBRLightsHashNumber(h, n) {
    _scenePBRLightsHashFloat[0] = (typeof n === "number" && n === n) ? n : 0;
    return Math.imul((h ^ _scenePBRLightsHashInt[0]) >>> 0, 16777619) >>> 0;
  }

  function scenePBRLightsHashString(h, s) {
    var str = (typeof s === "string") ? s : "";
    var len = str.length;
    for (var i = 0; i < len; i++) {
      h = Math.imul((h ^ str.charCodeAt(i)) >>> 0, 16777619) >>> 0;
    }
    // Length-delimit to distinguish "ab" + "c" from "a" + "bc".
    return Math.imul((h ^ (len + 1)) >>> 0, 16777619) >>> 0;
  }

  // hashLightContent computes the per-light sub-hash the frame-level
  // scenePBRLightsHash combines. Called from normalizeSceneLight (in
  // 10-runtime-scene-core.js) whenever a light is created or patched,
  // so the expensive string/number walk runs at mutation time — rare —
  // instead of per-frame. The result is stamped onto the light object
  // as `_lightHash` and read by scenePBRLightsHash without rehashing.
  //
  // Kept in 16-scene-webgl.js alongside scenePBRLightsHash so the two
  // must agree on what fields contribute to the hash; moving either
  // without the other is a correctness bug.
  function hashLightContent(l) {
    if (!l) return 0;
    var h = 2166136261;
    h = scenePBRLightsHashString(h, l.kind);
    h = scenePBRLightsHashNumber(h, sceneNumber(l.x, 0));
    h = scenePBRLightsHashNumber(h, sceneNumber(l.y, 0));
    h = scenePBRLightsHashNumber(h, sceneNumber(l.z, 0));
    h = scenePBRLightsHashNumber(h, sceneNumber(l.directionX, 0));
    h = scenePBRLightsHashNumber(h, sceneNumber(l.directionY, -1));
    h = scenePBRLightsHashNumber(h, sceneNumber(l.directionZ, 0));
    h = scenePBRLightsHashString(h, l.color);
    h = scenePBRLightsHashNumber(h, sceneNumber(l.intensity, 1));
    h = scenePBRLightsHashNumber(h, sceneNumber(l.range, 0));
    h = scenePBRLightsHashNumber(h, sceneNumber(l.decay, 2));
    h = scenePBRLightsHashNumber(h, sceneNumber(l.angle, 0));
    h = scenePBRLightsHashNumber(h, sceneNumber(l.penumbra, 0));
    h = scenePBRLightsHashNumber(h, sceneNumber(l.width, 0));
    h = scenePBRLightsHashNumber(h, sceneNumber(l.height, 0));
    h = scenePBRLightsHashString(h, l.groundColor);
    h = scenePBRLightsHashNumber(h, sceneNumber(l.shadowBias, 0));
    h = scenePBRLightsHashNumber(h, sceneNumber(l.shadowSize, 0));
    h = scenePBRLightsHashNumber(h, sceneNumber(l.shadowCascades, 0));
    h = scenePBRLightsHashNumber(h, sceneNumber(l.shadowSoftness, 0));
    return h;
  }

  // hashEnvironmentContent is the env-side counterpart to hashLightContent.
  // Called from normalizeSceneEnvironment and sceneResolveLightingEnvironment
  // whenever the environment is normalized so the cached sub-hash travels
  // with the environment object downstream.
  function hashEnvironmentContent(env) {
    if (!env) return 0;
    var h = 2166136261;
    h = scenePBRLightsHashString(h, env.ambientColor);
    h = scenePBRLightsHashNumber(h, sceneNumber(env.ambientIntensity, 0));
    h = scenePBRLightsHashString(h, env.skyColor);
    h = scenePBRLightsHashNumber(h, sceneNumber(env.skyIntensity, 0));
    h = scenePBRLightsHashString(h, env.groundColor);
    h = scenePBRLightsHashNumber(h, sceneNumber(env.groundIntensity, 0));
    h = scenePBRLightsHashString(h, env.envMap);
    h = scenePBRLightsHashNumber(h, sceneNumber(env.envIntensity, 1));
    h = scenePBRLightsHashNumber(h, sceneNumber(env.envRotation, 0));
    h = scenePBRLightsHashNumber(h, sceneNumber(env.fogDensity, 0));
    h = scenePBRLightsHashString(h, env.fogColor);
    return h;
  }

  // Determine the render pass for an object given its material.
  function scenePBRObjectRenderPass(obj, material) {
    if (sceneTransmissionMaterial(material)) return "alpha";
    // Derived object passes are cached defaults; after CSS substitution the
    // effective material must be allowed to choose the route again.
    if (obj && obj._renderPassDerived !== true &&
        typeof obj.renderPass === "string" && obj.renderPass) {
      const pass = obj.renderPass.toLowerCase();
      if (pass === "alpha" || pass === "additive" || pass === "opaque") {
        return pass;
      }
    }
    if (material && (material._renderPassDerived === true ||
        material._blendModeDerived === true)) {
      return sceneMaterialRenderPass(material);
    }
    if (material && typeof material.renderPass === "string" && material.renderPass) {
      const pass = material.renderPass.toLowerCase();
      if (pass === "alpha" || pass === "additive" || pass === "opaque") {
        return pass;
      }
    }
    // Authored blend choices remain authoritative. Built-in masked materials
    // otherwise use the opaque/depth-writing route and discard in-shader.
    const materialBlend = material && typeof material.blendMode === "string"
      ? material.blendMode.toLowerCase() : "";
    if (materialBlend === "additive") return "additive";
    if (materialBlend === "alpha") return "alpha";
    if (sceneMaterialMaskOpaqueRouting(material)) return "opaque";
    // If material opacity < 1, default to alpha pass.
    if (material && sceneNumber(material.opacity, 1) < 1) {
      return "alpha";
    }
    return "opaque";
  }

  // Depth-based sort comparator for translucent objects (back-to-front).
  function scenePBRDepthSort(a, b) {
    const da = sceneNumber(a && a.depthCenter, 0);
    const db = sceneNumber(b && b.depthCenter, 0);
    if (da !== db) {
      return db - da;
    }
    return String(a && a.id || "").localeCompare(String(b && b.id || ""));
  }

  // Built-in PBR transmission is a separate draw phase; authored shader hooks
  // keep control of their own shading and passes.
  /** @param {*} mat */
  function sceneTransmissionMaterial(mat) {
    return Boolean(mat && !mat.unlit && mat.kind !== "flat" && mat.kind !== "custom" && mat.kind !== "selena" &&
      !mat.customFragment && !mat.customFragmentWGSL && sceneNumber(mat.transmission, 0) > 0 && sceneNumber(mat.metalness, 0) < 1);
  }

  /** @param {*} bundle */
  function sceneTransmissionPresent(bundle) {
    return Array.isArray(bundle.materials) && bundle.materials.some(sceneTransmissionMaterial);
  }

  /** @param {*} object @param {*} mat @param {*} defaultWrite */
  function sceneTransmissionDepthWrite(object, mat, defaultWrite) {
    if (object && typeof object.depthWrite === "boolean") return object.depthWrite;
    // Opaque glass must depth-test its own facets after the background capture.
    return defaultWrite || sceneTransmissionMaterial(mat) && sceneNumber(mat.opacity, 1) >= 1;
  }

  /** @param {*} frameMeta @param {*} mount */
  function sceneTransmissionSettings(frameMeta, mount) {
    var tier = frameMeta && frameMeta.qualityProfile && frameMeta.qualityProfile.tier || frameMeta && frameMeta.qualityTier || "full";
    if (mount && mount.getAttribute && mount.getAttribute("data-gosx-scene3d-quality-ladder") === "true") {
      var rung = Number(mount.getAttribute("data-gosx-scene3d-quality-rung"));
      tier = rung === 0 ? "constrained" : rung === 1 ? "balanced" : "full";
    }
    var low = tier === "constrained" || tier === "low" || tier === "minimal" || tier === "survival";
    return { tier: tier, screen: !low, levels: tier === "balanced" || tier === "medium" ? 5 : 9 };
  }

  /** @param {*} effects @param {*} environment */
  function sceneTransmissionEffects(effects, environment) {
    if (effects.length) return effects;
    var env = environment || {};
    return [{ kind: "toneMapping", mode: env.toneMapping || "aces", exposure: sceneNumber(env.exposure, 1) }];
  }

  // The implicit output transform must leave an authored flat background
  // unchanged. Invert its curve for the clear value; keep alpha untouched.
  /** @param {*} background @param {*} effect */
  function sceneRenderBackground(background, effect) {
    var bg = typeof background === "string" && background.trim().toLowerCase() === "transparent" ? [0, 0, 0, 0] : sceneColorRGBA(background, [0.03, 0.08, 0.12, 1]);
    if (!effect) return bg;
    var mode = typeof effect.mode === "string" && effect.mode.trim().toLowerCase(), filmic = mode === "filmic";
    var c = filmic ? [6.2, 0.5, 6.2, 1.7, 0.06] : mode === "reinhard" ? [0, 1, 0, 1, 1] : mode === "none" || mode === "linear" ? [0, 1, 0, 0, 1] : [2.51, 0.03, 2.43, 0.59, 0.14];
    return bg.map(function(value, index) {
      if (index === 3) return value;
      var y = filmic ? value : Math.pow(value, 2.2), a = c[0] - c[2] * y, b = c[1] - c[3] * y;
      var x = 2 * c[4] * y / Math.max(1e-6, Math.sqrt(b * b + 4 * a * c[4] * y) + b);
      return sceneClamp((x + (filmic ? 0.004 : 0)) / Math.max(1e-6, sceneNumber(effect.exposure, 1)), 0, 65504);
    });
  }

  /** @param {*} mount @param {*} state */
  function sceneTransmissionPublish(mount, state) {
    if (mount && mount.getAttribute("data-gosx-scene3d-transmission") !== state) mount.setAttribute("data-gosx-scene3d-transmission", state);
  }
