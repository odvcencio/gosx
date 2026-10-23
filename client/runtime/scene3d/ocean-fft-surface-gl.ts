// Spectral surface for the WebGL2 ocean pass (lazy chunk bootstrap-feature-scene3d-ocean-fft.js).
//
// The WebGL2 ocean pass shaders (the WebGL2 ocean pass source) sum six Gerstner waves. This file turns those two
// sources into the spectral variant, the same way ocean-fft-surface.ts does for WGSL: displacement,
// slopes and the Jacobian come from the cascade textures that ocean-fft-gl.ts fills each frame, and foam
// comes from the combined Jacobian plus the persistent foam texture. Sky, glint, reflections, bathymetry,
// run-up and fog stay in the base shaders. The variant is made by replacing anchored spans of the base
// source; a span that is missing throws, so a base-shader edit makes the caller fall back to Gerstner
// waves (and fails the drift test), never a silent half-patch.
(function () {
  // Texture units of the spectral samplers. Units 0 to 3 belong to the ocean pass (bathymetry, reflection inputs).
  const UNIT = { disp: 4, slope: 8, foam: 12, wake: 13 };
  // Surface-side wake tuning (not simulation), as in the WGSL variant: the window edge fade in cells, the wake's shoaling wavenumber (rad/m) and foam gain.
  const WAKE = { edgeCells: 8, k: 0.2, slope: 3, slickLo: 0.04, slickHi: 0.4, slickFine: 0.85, slickMid: 0.4, glow: 0.3,
    // Foam opacity: thin foam 0.12 to 0.42 (lace strands lift it), the dense core 0.45 to 0.75; the lace line widths (noise units) at three scales.
    thinBase: 0.12, thinLace: 0.30, bodyBase: 0.4, bodyLace: 0.3, fadeNear: 120, fadeFar: 700 };

  function swap(src: string, from: string, to: string): string {
    if (src.indexOf(from) < 0) throw new Error("ocean-fft: anchor not found: " + from.slice(0, 48));
    return src.replace(from, () => to);
  }
  function between(src: string, from: string, to: string, replacement: string): string {
    const a = src.indexOf(from);
    if (a < 0) throw new Error("ocean-fft: anchor not found: " + from.slice(0, 48));
    const b = src.indexOf(to, a);
    if (b < 0) throw new Error("ocean-fft: anchor not found: " + to.slice(0, 48));
    return src.slice(0, a) + replacement + src.slice(b);
  }

  // u_sp[i] = (tile size m, band centre rad/m, texels, enabled) for cascade i; u_sp[4] = (foam uv scale, persistent foam on,
  // J threshold, gain); u_sp[5] = (crest amplitude m, foam water offset x, foam water offset z, persistent radius m); u_sp[6 + i].xy = the
  // water offset wrapped by cascade i's tile (the graph's waterMod<c> parameters).
  // u_sp[4].y is the foam lambda over the authored lambda, as in the WGSL variant.
  function declarations(): string {
    let s = "uniform vec4 u_sp[12];\n";
    for (let i = 0; i < 4; i++) s += "uniform sampler2D u_spDisp" + i + ";\n";
    for (let i = 0; i < 4; i++) s += "uniform sampler2D u_spSlope" + i + ";\n";
    s += "uniform sampler2D u_spFoam;\n";
    s += "uniform sampler2D u_spWake;\n";
    // u_sp[10] = (wake window origin relative to the camera x, z in metres, toroidal origin x, z in texels); u_sp[11] = (cell metres, on, window texels).
    // spWake returns (u, v, mask): the wake texture coordinate of a rest position and how much of the wake shows there (zero outside the window).
    s += "vec3 spWake(vec2 rest, vec2 cam) {\n";
    s += "  vec2 r = (rest - cam - u_sp[10].xy) / max(u_sp[11].x, 1e-3);\n";
    s += "  float edge = min(min(r.x, u_sp[11].z - r.x), min(r.y, u_sp[11].z - r.y));\n";
    s += "  return vec3((r + u_sp[10].zw) / u_sp[11].z, smoothstep(0., " + WAKE.edgeCells.toFixed(1) + ", edge) * u_sp[11].y);\n";
    s += "}\n";
    s += "float spLod(vec4 c, float spacing) { return max(log2(max(spacing, 1e-3) * c.z / c.x), 0.); }\n";
    return s;
  }

  function vertexBlock(): string {
    let s = "  vec3 p = vec3(xz.x, level, xz.y), dx = vec3(1., 0., 0.), dz = vec3(0., 0., 1.);\n";
    s += "  float amp = u_sp[5].x;\n";
    s += "  float spacing = max(u_ocean[8].z * u_ocean[8].w * exp(u_ocean[8].w * float(ring)), r * 6.2831853 / float(segs));\n";
    for (let i = 0; i < 4; i++) {
      s += "  if (u_sp[" + i + "].w > 0.5) {\n";
      s += "    vec2 suv = (xz - u_sp[" + (6 + i) + "].xy) / u_sp[" + i + "].x; float slod = spLod(u_sp[" + i + "], spacing); float sk = smoothstep(0., 1.2, depth0 * u_sp[" + i + "].y);\n";
      s += "    vec4 sa = textureLod(u_spDisp" + i + ", suv, slod), sb = textureLod(u_spSlope" + i + ", suv, slod);\n";
      s += "    p += sa.xyz * sk; dx += vec3(sb.z, sb.x, sa.w) * sk; dz += vec3(sa.w, sb.y, sb.w) * sk;\n";
      s += "  }\n";
    }
    s += "  vec3 swv = spWake(xz, u_ocean[7].xz);\n";
    // Debug view (u_sp[11].w = ramp range in metres, 0 = off): a flat sea.
    s += "  if (u_sp[11].w > 0.) { p = vec3(xz.x, level, xz.y); dx = vec3(1., 0., 0.); dz = vec3(0., 0., 1.); }\n";
    s += "  if (swv.z > 0. && u_sp[11].w <= 0.) {\n";
    s += "    vec4 swk = textureLod(u_spWake, swv.xy, max(log2(max(spacing, 1e-3) / max(u_sp[11].x, 1e-3)), 0.));\n";
    s += "    float sws = swv.z * smoothstep(0., 1.2, depth0 * " + WAKE.k.toFixed(2) + ");\n";
    s += "    p.y += swk.x * sws; dx.y += swk.y * sws; dz.y += swk.z * sws;\n";
    s += "  }\n";
    return s;
  }

  function fragmentBlock(): string {
    let s = "  float sGx = 0., sGz = 0., sXX = 0., sZZ = 0., sXZ = 0.;\n";
    s += "  vec2 wxz = v_rest - u_sp[5].yz;\n";
    s += "  vec2 sFw = fwidth(wxz);\n";
    // The wake first: the slick (calmer water where it churned, T = swf.w) damps the small cascades' slopes below.
    s += "  float sOp = 0., sAlb = 1.;\n";
    s += "  vec3 swv = spWake(v_rest, u_ocean[7].xz);\n";
    s += "  vec4 swf = texture(u_spWake, swv.xy);\n";
    s += "  float sSlick = swv.z * smoothstep(" + WAKE.slickLo.toFixed(2) + ", " + WAKE.slickHi.toFixed(2) + ", swf.w);\n";
    for (let i = 0; i < 4; i++) {
      const damp = i >= 2 ? WAKE.slickFine : i === 1 ? WAKE.slickMid : 0;
      s += "  if (u_sp[" + i + "].w > 0.5) {\n";
      s += "    vec2 suv = (v_rest - u_sp[" + (6 + i) + "].xy) / u_sp[" + i + "].x; float sk = smoothstep(0., 1.2, v_d0 * u_sp[" + i + "].y)" + (damp ? " * (1. - " + damp.toFixed(2) + " * sSlick)" : "") + ";\n";
      s += "    vec4 sa = texture(u_spDisp" + i + ", suv), sb = texture(u_spSlope" + i + ", suv);\n";
      s += "    sGx += sb.x * sk; sGz += sb.y * sk; sXX += sb.z * sk; sZZ += sb.w * sk; sXZ += sa.w * sk;\n";
      s += "  }\n";
    }
    s += "  float swm = swv.z * smoothstep(0., 1.2, v_d0 * " + WAKE.k.toFixed(2) + ");\n";
    s += "  sGx += swf.y * swm * " + WAKE.slope.toFixed(2) + "; sGz += swf.z * swm * " + WAKE.slope.toFixed(2) + ";\n";
    s += "  float sJ = (1. + sXX) * (1. + sZZ) - sXZ * sXZ;\n";
    s += "  vec4 sFoamTex = textureLod(u_spFoam, wxz * u_sp[4].x, 0.);\n";
    s += "  float sPersist = sFoamTex.r;\n";
    s += "  vec3 N = normalize(vec3(sGz * sXZ - (1. + sZZ) * sGx, max(sJ, 0.1), sXZ * sGx - sGz * (1. + sXX)));\n";
    return s;
  }

  const FOAM_LINE = "    float sNear = 1. - smoothstep(0.7 * u_sp[5].w, u_sp[5].w, length(v_w.xz - u_ocean[7].xz));\n" +
    "    float sJf = (1. + u_sp[4].y * sXX) * (1. + u_sp[4].y * sZZ) - u_sp[4].y * u_sp[4].y * sXZ * sXZ;\n" +
    "    float sStateless = clamp((u_sp[4].z - sJf) * u_sp[4].w, 0., 1.) * lace * bu;\n" +
    "    float sKept = smoothstep(0.04, 0.6, sPersist) * (0.55 + 0.45 * lace);\n" +
    "    foam = mix(sStateless, sKept, sNear);\n" +
    // The wake's foam, composited after the crest foam (see the colour step): its density is the foam texture's wake lane near the camera, the
    // wake's own turbulence beyond that window. Density maps to opacity on a curve (thin foam 0.12 to 0.42, the core up to 0.75), and the lace
    // strands lift it, so the foam is translucent and its edges fade over a wide band with the strands breaking up.
    "    float sDen = mix(smoothstep(0.1, 0.9, swf.w) * swv.z, sFoamTex.b, sNear);\n" +
    "    float sStr = mix(0.4, spLace(wxz, t, sFw), 1. - smoothstep(" + WAKE.fadeNear.toFixed(1) + ", " + WAKE.fadeFar.toFixed(1) + ", dist));\n" +
    "    sStr *= mix(0.55, 1., smoothstep(0.1, 0.6, sDen));\n" +
    "    float sThin = smoothstep(0.02, 0.5, sDen), sBody = smoothstep(0.5, 1., sDen);\n" +
    "    sOp = sThin * mix(" + WAKE.thinBase.toFixed(2) + " + " + WAKE.thinLace.toFixed(2) + " * sStr, " + WAKE.bodyBase.toFixed(2) + " + " + WAKE.bodyLace.toFixed(2) + " * sStr, sBody);\n" +
    "    sOp *= (1. - smoothstep(1500., 6000., dist)) * min(u_ocean[3].w, 1.);\n" +
    "    sAlb = mix(0.8, 1., sStr);\n";

  // Wake foam lace (fragment shader only: it needs the base shader's value noise `on`). spLine is one scale of cellular lines (the edges of a jittered cell grid, width w in cells) that fades to its mean coverage when the pattern is finer than a pixel; spLace joins three scales
  // (0.3 m, 0.9 m, 2.7 m) over a slowly warped, slowly cross-faded domain, so the lace advects with the water and dissolves and re-forms.
  function laceFunctions(): string {
    let s = "";
    s += "float spLine(vec2 p, float sc, float w, vec2 fw) {\n";
    s += "  vec2 q = p * sc; q += 0.55 * (vec2(on(q * 0.8 + 3.1), on(q * 0.8 + 9.7)) - 0.5);\n";
    s += "  vec2 i = floor(q), f = fract(q); float d1 = 8., d2 = 8.;\n";
    s += "  for (int y = -1; y <= 1; y++) for (int x = -1; x <= 1; x++) {\n";
    s += "    vec2 g = vec2(float(x), float(y)), c = i + g; vec2 r = g + vec2(oh(c), oh(c + vec2(37.7, 11.3))) - f;\n";
    s += "    float d = dot(r, r); if (d < d1) { d2 = d1; d1 = d; } else if (d < d2) { d2 = d; }\n";
    s += "  }\n";
    s += "  float line = 1. - smoothstep(0., w, sqrt(d2) - sqrt(d1));\n";
    s += "  return mix(1.2 * w, line, 1. - smoothstep(0.35, 0.9, length(fw) * sc));\n";
    s += "}\n";
    s += "float spStrand(vec2 q, vec2 o, vec2 fw) {\n";
    s += "  float a = spLine(q + o, 3.3, 0.14, fw), b = spLine(q + o * 1.7, 1.1, 0.12, fw), c = spLine(q + o * 0.6, 0.4, 0.09, fw);\n";
    s += "  return 1. - (1. - 0.85 * a) * (1. - 0.6 * b) * (1. - 0.4 * c);\n";
    s += "}\n";
    s += "float spLace(vec2 p, float t, vec2 fw) {\n";
    s += "  vec2 q = p + 0.7 * (vec2(on(p * 0.55 + vec2(t * 0.13, 3.1)), on(p * 0.55 + vec2(8.7, -t * 0.11))) - 0.5);\n";
    s += "  float k = floor(t / 4.);\n";
    s += "  return mix(spStrand(q, vec2(17.3, 5.1) * k, fw), spStrand(q, vec2(17.3, 5.1) * (k + 1.), fw), smoothstep(0., 1., fract(t / 4.)));\n";
    s += "}\n";
    return s;
  }

  // glsl returns the spectral variant of the base ocean vertex and fragment shaders.
  function glsl(vertex: string, fragment: string): any {
    let v = vertex;
    v = swap(v, "out float v_d0;", "out float v_d0; out vec2 v_rest;");
    v = swap(v, "void main() {", declarations() + "void main() {");
    v = between(v, "  vec3 p = vec3(xz.x, level, xz.y), dx", "  float ph = fract(t * u_ocean[0].w / 9.", vertexBlock());
    v = swap(v, "v_d0 = depth0;", "v_d0 = depth0; v_rest = xz;");
    let f = fragment;
    f = swap(f, "in float v_d0;", "in float v_d0; in vec2 v_rest;");
    f = swap(f, "void main() {", declarations() + laceFunctions() + "void main() {");
    f = swap(f, "  vec3 N = od(normalize(v_n), v_w.xz, t, u_ocean[9].xy);\n", fragmentBlock());
    f = swap(f, "  if (dist < 250.) {", "  if (dist < 6000.) {");
    f = swap(f, "    foam = (1. - smoothstep(0.12, 0.35, v_j)) * lace * bu;\n", FOAM_LINE);
    f = swap(f, "(1. - smoothstep(80., 250., dist))", "(1. - smoothstep(1500., 6000., dist))");
    // Debug view: the wake height as a colour ramp (blue below rest, red above, grey outside the wake window), unlit, replacing the colour.
    f = swap(f, "  fragColor = vec4(color, alpha);", "  if (u_sp[11].w > 0.) { float hh = clamp(swf.x / u_sp[11].w, -1., 1.); color = swv.z > 0. ? (hh < 0. ? mix(vec3(1.), vec3(.1, .3, 1.), -hh) : mix(vec3(1.), vec3(1., .15, .1), hh)) : vec3(.25); alpha = 1.; }\n  fragColor = vec4(color, alpha);");
    // Aerated water: a turquoise glow where the wake is freshly churned (T from the wake texture), under the foam.
    f = swap(f, "  color = mix(color, fl, foam); alpha = mix(alpha, 1., foam);", "  float sAer = swv.z * smoothstep(0.45, 1.0, swf.w);\n  vec3 sTurq = vec3(0.05, 0.42, 0.44) * (ambient * 0.9 + sunCol * 0.05);\n  color = mix(color, sTurq, sAer * " + WAKE.glow.toFixed(2) + " * (1. - foam)); alpha = mix(alpha, 1., sAer * 0.5);\n  color = mix(color, fl, foam); alpha = mix(alpha, 1., foam);\n" +
      // Wake foam: lit white where the sun reaches it, blue-grey where it faces away or is shadowed; the sun glint still shows through thin foam.
      "  float sSun = smoothstep(0., 0.55, dot(N, L));\n  vec3 sWfl = fl * mix(vec3(0.66, 0.76, 0.9), vec3(1.), sSun) * sAlb + u_ocean[4].xyz * sunCol * 0.2 * max(dot(N, L), 0.) * sAlb;\n  color = mix(color, sWfl, sOp) + spec * sOp * 0.25; alpha = mix(alpha, 1., sOp);");
    // Foam is lit like the base shader lights it, with the sky term lifted so it reads as white foam under a bright sky.
    f = swap(f, "(ambient * 0.55 + sunCol * 0.04 * max(dot(N, L), 0.))", "(ambient * 1.4 + sunCol * 0.2 * max(dot(N, L), 0.) + vec3(0.05))");
    return { vertex: v, fragment: f };
  }

  window.__gosx_scene3d_ocean_fft_surface_gl = { glsl: glsl, UNIT: UNIT };
})();
