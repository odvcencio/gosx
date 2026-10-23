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
  const SLOT = { uniform: 0, sampler: 1, disp: 2, slope: 6, foam: 10, wake: 11 };
  // Surface-side wake tuning (not simulation): the window edge fades over this many cells, the wake's shoaling wavenumber (rad/m, like a
  // cascade's band centre), and how strongly wake turbulence reads as foam.
  const WAKE = { edgeCells: 8, k: 0.2, slope: 3, slickLo: 0.04, slickHi: 0.4, slickFine: 0.85, slickMid: 0.4, glow: 0.3, ragged: 0.7, thin: 0.72, density: 0.55 };

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
    let s = "const WAKE_EDGE_CELLS: f32 = " + WAKE.edgeCells.toFixed(1) + ";\n";
    s += "struct OceanSpec { c: array<vec4f, 4>, f: vec4f, m: vec4f, w: array<vec4f, 4>, k: array<vec4f, 2> };\n";
    s += "@group(" + group + ") @binding(" + SLOT.uniform + ") var<uniform> sp: OceanSpec;\n";
    s += "@group(" + group + ") @binding(" + SLOT.sampler + ") var spSampler: sampler;\n";
    for (let i = 0; i < 4; i++) s += "@group(" + group + ") @binding(" + (SLOT.disp + i) + ") var spDisp" + i + ": texture_2d<f32>;\n";
    for (let i = 0; i < 4; i++) s += "@group(" + group + ") @binding(" + (SLOT.slope + i) + ") var spSlope" + i + ": texture_2d<f32>;\n";
    s += "@group(" + group + ") @binding(" + SLOT.foam + ") var spFoam: texture_2d<f32>;\n";
    s += "@group(" + group + ") @binding(" + SLOT.wake + ") var spWakeTex: texture_2d<f32>;\n";
    // c[i] = (tile size m, band centre rad/m, texels, enabled); f = (foam uv scale, foam lambda / authored lambda, J threshold, gain);
    // m = (crest amplitude m, foam water offset x, foam water offset z, persistent radius m); w[i].xy = the water offset wrapped by cascade
    // i's tile (the graph's waterMod<c> parameters), so a cascade is sampled at (position - w[i].xy) / tile.
    // k[0] = (wake window origin relative to the camera x, z in metres, toroidal origin x, z in texels); k[1] = (cell metres, on, window texels).
    // spWake returns (u, v, mask): the wake texture coordinate of a rest position and how much of the wake shows there (zero outside the window).
    s += "fn spWake(rest: vec2f, cam: vec2f) -> vec3f {\n";
    s += "  let r = (rest - cam - sp.k[0].xy) / max(sp.k[1].x, 1e-3);\n";
    s += "  let edge = min(min(r.x, sp.k[1].z - r.x), min(r.y, sp.k[1].z - r.y));\n";
    s += "  return vec3f((r + sp.k[0].zw) / sp.k[1].z, smoothstep(0.0, WAKE_EDGE_CELLS, edge) * sp.k[1].y);\n";
    s += "}\n";
    s += "fn spLod(c: vec4f, spacing: f32) -> f32 { return max(log2(max(spacing, 1e-3) * c.z / c.x), 0.0); }\n";
    return s;
  }

  function vertexBlock(): string {
    let s = "  var p = vec3f(xz.x, level, xz.y); var dx = vec3f(1.0, 0.0, 0.0); var dz = vec3f(0.0, 0.0, 1.0);\n";
    s += "  var amp = sp.m.x;\n";
    s += "  let spacing = max(ocean.p[8].z * ocean.p[8].w * exp(ocean.p[8].w * f32(ring)), r * 6.2831853 / f32(segs));\n";
    for (let i = 0; i < 4; i++) {
      s += "  if (sp.c[" + i + "].w > 0.5) {\n";
      s += "    let uv = (xz - sp.w[" + i + "].xy) / sp.c[" + i + "].x; let lod = spLod(sp.c[" + i + "], spacing); let k = smoothstep(0.0, 1.2, depth0 * sp.c[" + i + "].y);\n";
      s += "    let a = textureSampleLevel(spDisp" + i + ", spSampler, uv, lod); let b = textureSampleLevel(spSlope" + i + ", spSampler, uv, lod);\n";
      s += "    p += a.xyz * k; dx += vec3f(b.z, b.x, a.w) * k; dz += vec3f(a.w, b.y, b.w) * k;\n";
      s += "  }\n";
    }
    s += "  let swv = spWake(xz, ocean.p[7].xz);\n";
    // Debug view (k[1].w = ramp range in metres, 0 = off): a flat sea.
    s += "  if (sp.k[1].w > 0.0) { p = vec3f(xz.x, level, xz.y); dx = vec3f(1.0, 0.0, 0.0); dz = vec3f(0.0, 0.0, 1.0); }\n";
    s += "  if (swv.z > 0.0 && sp.k[1].w <= 0.0) {\n";
    s += "    let swk = textureSampleLevel(spWakeTex, spSampler, swv.xy, max(log2(max(spacing, 1e-3) / max(sp.k[1].x, 1e-3)), 0.0));\n";
    s += "    let sws = swv.z * smoothstep(0.0, 1.2, depth0 * " + WAKE.k.toFixed(2) + ");\n";
    s += "    p.y += swk.x * sws; dx.y += swk.y * sws; dz.y += swk.z * sws;\n";
    s += "  }\n";
    return s;
  }

  function fragmentBlock(): string {
    let s = "  var sGx = 0.0; var sGz = 0.0; var sXX = 0.0; var sZZ = 0.0; var sXZ = 0.0;\n";
    s += "  let wxz = in.rest - sp.m.yz;\n";
    // The wake first: the slick (calmer water where it churned, T = swf.w) damps the small cascades' slopes below.
    s += "  var sShade = 1.0;\n";
    s += "  let swv = spWake(in.rest, ocean.p[7].xz);\n";
    s += "  let swf = textureSample(spWakeTex, spSampler, swv.xy);\n";
    s += "  let sSlick = swv.z * smoothstep(" + WAKE.slickLo.toFixed(2) + ", " + WAKE.slickHi.toFixed(2) + ", swf.w);\n";
    for (let i = 0; i < 4; i++) {
      const damp = i >= 2 ? WAKE.slickFine : i === 1 ? WAKE.slickMid : 0;
      s += "  if (sp.c[" + i + "].w > 0.5) {\n";
      s += "    let uv = (in.rest - sp.w[" + i + "].xy) / sp.c[" + i + "].x; let k = smoothstep(0.0, 1.2, in.depth0 * sp.c[" + i + "].y)" + (damp ? " * (1.0 - " + damp.toFixed(2) + " * sSlick)" : "") + ";\n";
      s += "    let a = textureSample(spDisp" + i + ", spSampler, uv); let b = textureSample(spSlope" + i + ", spSampler, uv);\n";
      s += "    sGx += b.x * k; sGz += b.y * k; sXX += b.z * k; sZZ += b.w * k; sXZ += a.w * k;\n";
      s += "  }\n";
    }
    s += "  let swm = swv.z * smoothstep(0.0, 1.2, in.depth0 * " + WAKE.k.toFixed(2) + ");\n";
    s += "  sGx += swf.y * swm * " + WAKE.slope.toFixed(2) + "; sGz += swf.z * swm * " + WAKE.slope.toFixed(2) + ";\n";
    s += "  let sJ = (1.0 + sXX) * (1.0 + sZZ) - sXZ * sXZ;\n";
    s += "  let sFoamTex = textureSampleLevel(spFoam, spSampler, wxz * sp.f.x, 0.0);\n";
    s += "  let sPersist = sFoamTex.r;\n";
    s += "  let N = normalize(vec3f(sGz * sXZ - (1.0 + sZZ) * sGx, max(sJ, 0.1), sXZ * sGx - sGz * (1.0 + sXX)));\n";
    return s;
  }

  const FOAM_LINE = "    let sNear = 1.0 - smoothstep(0.7 * sp.m.w, sp.m.w, length(in.world.xz - ocean.p[7].xz));\n" +
    "    let sJf = (1.0 + sp.f.y * sXX) * (1.0 + sp.f.y * sZZ) - sp.f.y * sp.f.y * sXZ * sXZ;\n" +
    "    let sStateless = clamp((sp.f.z - sJf) * sp.f.w, 0.0, 1.0) * lace * bu;\n" +
    "    let sKept = smoothstep(0.04, 0.6, sPersist) * (0.55 + 0.45 * lace);\n" +
    "    foam = mix(sStateless, sKept, sNear);\n" +
    // The wake's foam: the foam texture's wake lane (the kernel's streaky coverage) near the camera, the wake's own turbulence beyond that window.
    "    let sWake = mix(smoothstep(0.35, 0.9, swf.w) * swv.z, sFoamTex.b, sNear);\n" +
    // Dissolve the kernel's coverage per pixel with fine noise so the edges are ragged below the foam texel; thin foam is greyer.
    "    let sDis = smoothstep(0.3, 0.6, sWake + (ob(wxz * 2.3 + vec2f(7.0)) - 0.5) * " + WAKE.ragged.toFixed(2) + ");\n" +
    "    sShade = mix(" + WAKE.thin.toFixed(2) + ", 1.0, smoothstep(0.35, 1.0, sWake)) * (0.86 + 0.14 * ob(wxz * 3.1 + vec2f(13.0)));\n" +
    "    foam = max(foam, sDis * (" + WAKE.density.toFixed(2) + " + " + (1 - 0.55).toFixed(2) + " * smoothstep(0.3, 0.9, sWake)) * (0.92 + 0.08 * lace));\n";

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
    // Debug view: the wake height as a colour ramp (blue below rest, red above, grey outside the wake window), unlit, replacing the colour.
    s = swap(s, "  return vec4f(color, alpha);", "  if (sp.k[1].w > 0.0) { let hh = clamp(swf.x / sp.k[1].w, -1.0, 1.0); color = select(vec3f(0.25), select(mix(vec3f(1.0), vec3f(1.0, 0.15, 0.1), hh), mix(vec3f(1.0), vec3f(0.1, 0.3, 1.0), -hh), hh < 0.0), swv.z > 0.0); alpha = 1.0; }\n  return vec4f(color, alpha);");
    // Aerated water: a turquoise glow where the wake is freshly churned (T from the wake texture), under the foam.
    s = swap(s, "  color = mix(color, fl, foam); alpha = mix(alpha, 1.0, foam);", "  let sAer = swv.z * smoothstep(0.45, 1.0, swf.w);\n  let sTurq = vec3f(0.05, 0.42, 0.44) * (ambient * 0.9 + sunCol * 0.05);\n  color = mix(color, sTurq, sAer * " + WAKE.glow.toFixed(2) + " * (1.0 - foam)); alpha = mix(alpha, 1.0, sAer * 0.5);\n  color = mix(color, fl * sShade, foam); alpha = mix(alpha, 1.0, foam);");
    // Foam is lit like the base shader lights it, with the sky term lifted so it reads as white foam under a bright sky.
    s = swap(s, "(ambient * 0.55 + sunCol * 0.04 * max(dot(N, L), 0.0))", "(ambient * 1.4 + sunCol * 0.2 * max(dot(N, L), 0.0) + vec3f(0.05))");
    return s + "\n" + declarations(group);
  }

  window.__gosx_scene3d_ocean_fft_surface = { wgsl: wgsl, SLOT: SLOT };
})();
