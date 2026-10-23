// Spectral surface for the WebGPU ocean pass (lazy chunk bootstrap-feature-scene3d-ocean-fft.js).
//
// The hand-written WebGPU ocean pass shader sums six Gerstner waves. This file turns that
// source into the spectral variant: displacement, slopes and the Jacobian come from the cascade
// textures that ocean-fft.ts fills each frame, and foam comes from the combined Jacobian
// plus the persistent foam texture. Sky, glint, reflections, bathymetry, run-up and fog stay in
// the base shader. The variant is made by replacing anchored spans of the base source; a span that
// is missing throws, so a base-shader edit fails the pipeline (which falls back to Gerstner waves)
// and the drift test, never a silent half-patch.
(function () {
  // Binding slots of the spectral group, shared with ocean-fft.ts.
  const SLOT = { uniform: 0, sampler: 1, disp: 2, slope: 6, foam: 10 };

  function between(src: string, from: string, to: string, replacement: string): string {
    const a = src.indexOf(from);
    if (a < 0) throw new Error("ocean-fft: anchor not found: " + from.slice(0, 48));
    const b = src.indexOf(to, a);
    if (b < 0) throw new Error("ocean-fft: anchor not found: " + to.slice(0, 48));
    return src.slice(0, a) + replacement + src.slice(b);
  }
  function swap(src: string, from: string, to: string): string {
    if (src.indexOf(from) < 0) throw new Error("ocean-fft: anchor not found: " + from.slice(0, 48));
    return src.replace(from, () => to);
  }

  function declarations(group: number): string {
    let s = "struct OceanSpec { c: array<vec4f, 4>, f: vec4f, m: vec4f };\n";
    s += "@group(" + group + ") @binding(" + SLOT.uniform + ") var<uniform> sp: OceanSpec;\n";
    s += "@group(" + group + ") @binding(" + SLOT.sampler + ") var spSampler: sampler;\n";
    for (let i = 0; i < 4; i++) s += "@group(" + group + ") @binding(" + (SLOT.disp + i) + ") var spDisp" + i + ": texture_2d<f32>;\n";
    for (let i = 0; i < 4; i++) s += "@group(" + group + ") @binding(" + (SLOT.slope + i) + ") var spSlope" + i + ": texture_2d<f32>;\n";
    s += "@group(" + group + ") @binding(" + SLOT.foam + ") var spFoam: texture_2d<f32>;\n";
    // c[i] = (tile size m, band centre rad/m, texels, enabled); f = (foam uv scale, foam lambda / authored lambda, J threshold, gain);
    // m = (crest amplitude m, water offset x, water offset z, persistent radius m).
    s += "fn spLod(c: vec4f, spacing: f32) -> f32 { return max(log2(max(spacing, 1e-3) * c.z / c.x), 0.0); }\n";
    return s;
  }

  function vertexBlock(): string {
    let s = "  var p = vec3f(xz.x, level, xz.y); var dx = vec3f(1.0, 0.0, 0.0); var dz = vec3f(0.0, 0.0, 1.0);\n";
    s += "  var amp = sp.m.x;\n";
    s += "  let spacing = max(ocean.p[8].z * ocean.p[8].w * exp(ocean.p[8].w * f32(ring)), r * 6.2831853 / f32(segs));\n";
    s += "  let wxz = xz - sp.m.yz;\n";
    for (let i = 0; i < 4; i++) {
      s += "  if (sp.c[" + i + "].w > 0.5) {\n";
      s += "    let uv = wxz / sp.c[" + i + "].x; let lod = spLod(sp.c[" + i + "], spacing); let k = smoothstep(0.0, 1.2, depth0 * sp.c[" + i + "].y);\n";
      s += "    let a = textureSampleLevel(spDisp" + i + ", spSampler, uv, lod); let b = textureSampleLevel(spSlope" + i + ", spSampler, uv, lod);\n";
      s += "    p += a.xyz * k; dx += vec3f(b.z, b.x, a.w) * k; dz += vec3f(a.w, b.y, b.w) * k;\n";
      s += "  }\n";
    }
    return s;
  }

  function fragmentBlock(): string {
    let s = "  var sGx = 0.0; var sGz = 0.0; var sXX = 0.0; var sZZ = 0.0; var sXZ = 0.0;\n";
    s += "  let wxz = in.rest - sp.m.yz;\n";
    for (let i = 0; i < 4; i++) {
      s += "  if (sp.c[" + i + "].w > 0.5) {\n";
      s += "    let uv = wxz / sp.c[" + i + "].x; let k = smoothstep(0.0, 1.2, in.depth0 * sp.c[" + i + "].y);\n";
      s += "    let a = textureSample(spDisp" + i + ", spSampler, uv); let b = textureSample(spSlope" + i + ", spSampler, uv);\n";
      s += "    sGx += b.x * k; sGz += b.y * k; sXX += b.z * k; sZZ += b.w * k; sXZ += a.w * k;\n";
      s += "  }\n";
    }
    s += "  let sJ = (1.0 + sXX) * (1.0 + sZZ) - sXZ * sXZ;\n";
    s += "  let sPersist = textureSampleLevel(spFoam, spSampler, wxz * sp.f.x, 0.0).r;\n";
    s += "  let N = normalize(vec3f(sGz * sXZ - (1.0 + sZZ) * sGx, max(sJ, 0.1), sXZ * sGx - sGz * (1.0 + sXX)));\n";
    return s;
  }

  const FOAM_LINE = "    let sNear = 1.0 - smoothstep(0.7 * sp.m.w, sp.m.w, length(in.world.xz - ocean.p[7].xz));\n" +
    "    let sJf = (1.0 + sp.f.y * sXX) * (1.0 + sp.f.y * sZZ) - sp.f.y * sp.f.y * sXZ * sXZ;\n" +
    "    let sStateless = clamp((sp.f.z - sJf) * sp.f.w, 0.0, 1.0) * lace * bu;\n" +
    "    let sKept = smoothstep(0.04, 0.6, sPersist) * (0.55 + 0.45 * lace);\n" +
    "    foam = mix(sStateless, sKept, sNear);\n";

  // wgsl returns the spectral variant of the base ocean shader. group is the bind group index of the spectral resources.
  function wgsl(base: string, group: number): string {
    let s = base;
    s = swap(s, "@location(4) depth0: f32 };", "@location(4) depth0: f32, @location(5) rest: vec2f };");
    s = between(s, "  var p = vec3f(xz.x, level, xz.y);", "  let ph = fract(t * ocean.p[0].w / 9.0", vertexBlock());
    s = swap(s, "out.depth0 = depth0;", "out.depth0 = depth0; out.rest = xz;");
    s = swap(s, "  let N = od(normalize(in.normal), in.world.xz, t, ocean.p[9].xy);\n", fragmentBlock());
    s = swap(s, "  if (dist < 250.0) {", "  if (dist < 6000.0) {");
    s = swap(s, "    foam = (1.0 - smoothstep(0.12, 0.35, in.jacobian)) * lace * bu;\n", FOAM_LINE);
    s = swap(s, "(1.0 - smoothstep(80.0, 250.0, dist))", "(1.0 - smoothstep(1500.0, 6000.0, dist))");
    // Foam is lit like the base shader lights it, with the sky term lifted so it reads as white foam under a bright sky.
    s = swap(s, "(ambient * 0.55 + sunCol * 0.04 * max(dot(N, L), 0.0))", "(ambient * 1.4 + sunCol * 0.2 * max(dot(N, L), 0.0) + vec3f(0.05))");
    return s + "\n" + declarations(group);
  }

  window.__gosx_scene3d_ocean_fft_surface = { wgsl: wgsl, SLOT: SLOT };
})();
