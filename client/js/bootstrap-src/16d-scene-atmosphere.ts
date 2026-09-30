// Opt-in atmosphere contracts and camera-independent quality policy.
function sceneAtmosphereNumber(raw, key, fallback, lo, hi) {
  return Math.max(lo, Math.min(hi, sceneNumber(raw && raw[key], fallback)));
}
function sceneOceanReflections(raw) {
  if (!sceneIsPlainObject(raw)) return null;
  const mode = String(raw.mode || "").trim().toLowerCase();
  if (!["ssr", "planar", "ssr+planar"].includes(mode)) return null;
  return { mode, resolution: sceneAtmosphereNumber(raw, "resolution", 0.5, 0.125, 1),
    strength: sceneAtmosphereNumber(raw, "strength", 1, 0, 1) };
}
// Authored settings stay immutable. Tier profiles and explicit ladder rungs
// shed planar first, then ray samples, then all optional atmosphere work.
function sceneAtmosphereQuality(meta) {
  const profile = meta && meta.qualityProfile || {};
  const tier = meta && meta.atmosphereTier || (meta && meta.qualityEnabled ? String(profile.tier || meta.qualityTier || "full") : "full");
  const off = ["survival", "minimal", "low", "off"].includes(tier);
  const cheap = off || ["reduced", "balanced", "medium"].includes(tier);
  const rung = meta && meta.atmosphere || profile.atmosphere || {};
  return { reflections: !off && rung.reflections !== false,
    planar: !cheap && rung.planar !== false,
    reflectionSteps: off ? 0 : cheap ? 10 : 20,
    clouds: !off && rung.clouds !== false, cloudOctaves: cheap ? 3 : 5,
    godRays: !off && rung.godRays !== false, raySamples: cheap ? 12 : 32,
    haze: !off && rung.haze !== false, grain: !off && rung.grain !== false };
}
function sceneReflectionMatrices(view, proj, level, webgpu) {
  const mirror = new Float32Array([1,0,0,0, 0,-1,0,0, 0,0,1,0, 0,2*level,0,1]);
  const reflected = sceneMat4Multiply(view, mirror);
  const vp = sceneMat4Multiply(proj, view);
  return { view: reflected, projection: sceneReflectionProjection(reflected, proj, level, webgpu), vp, inverse: sceneAtmosphereInverse(vp),
    planarVP: sceneMat4Multiply(proj, reflected) };
}

// A pivoted inverse handles perspective and orthographic cameras alike.
function sceneAtmosphereInverse(m) {
  const rows = Array.from({length: 4}, (_, r) => Array.from({length: 8}, (_, c) => c < 4 ? m[c*4+r] : +(c-4 === r)));
  for (let c = 0; c < 4; c++) {
    let pivot = c;
    for (let r = c+1; r < 4; r++) if (Math.abs(rows[r][c]) > Math.abs(rows[pivot][c])) pivot = r;
    [rows[c], rows[pivot]] = [rows[pivot], rows[c]];
    const d = rows[c][c];
    if (Math.abs(d) < 1e-12) return new Float32Array(16);
    for (let j = 0; j < 8; j++) rows[c][j] /= d;
    for (let r = 0; r < 4; r++) {
      if (r === c) continue;
      const k = rows[r][c];
      for (let j = 0; j < 8; j++) rows[r][j] -= k * rows[c][j];
    }
  }
  return new Float32Array(Array.from({length: 16}, (_, i) => rows[i%4][4+(i/4|0)]));
}
// Clip the reflected camera's near plane to the mean sea level. Based on
// Lengyel's oblique near-plane projection; WGSL uses a [0,w] depth range.
function sceneReflectionProjection(view, proj, level, webgpu) {
  const p = new Float32Array(proj), invView = sceneAtmosphereInverse(view);
  if (webgpu) for (let c = 0; c < 4; c++) p[c*4+2] = 2*p[c*4+2]-p[c*4+3];
  const plane = Array.from({length: 4}, (_, c) => invView[c*4+1]-level*invView[c*4+3]);
  const inv = sceneAtmosphereInverse(p), corner = [Math.sign(plane[0]),Math.sign(plane[1]),1,1];
  const q = Array.from({length: 4}, (_, r) => corner.reduce((v,k,c) => v+inv[c*4+r]*k,0));
  const dot = plane.reduce((v,k,i) => v+k*q[i],0);
  if (Math.abs(dot) > 1e-6) for (let c = 0; c < 4; c++) p[c*4+2] = plane[c]*2/dot-p[c*4+3];
  if (webgpu) for (let c = 0; c < 4; c++) p[c*4+2] = 0.5*(p[c*4+2]+p[c*4+3]);
  return p;
}
function sceneReflectDispose(resources) {
  if (resources.reflection) resources.reflection.dispose();
  resources.reflection = null;
}
function sceneReflectOpaqueList(list, materials) {
  // User shader uniforms have their own frame ownership. Visible custom
  // materials still reflect through SSR; the fallback draws built-in PBR.
  return (list || []).filter(obj => {
    const mat = materials[sceneNumber(obj.materialIndex,0)];
    return !(mat && (mat.shaderBackend === "selena" || mat.customVertex || mat.customFragment));
  });
}

function sceneAtmosphereTier(state) {
  if (!state || state.mode !== "ladder") return null;
  return state.rungIndex === 0 ? "full" : state.rungIndex >= state.ladder.length-1 ? "survival" : "balanced";
}
