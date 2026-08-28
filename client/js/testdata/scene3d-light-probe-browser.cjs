'use strict';
/* Native browser regression probe for Scene3D light probes (second-order
 * spherical-harmonics irradiance in linear RGB).
 *
 * Boots real Chrome over CDP (Node builtins only), serves the built
 * bootstrap.js plus a strict basename allowlist of runtime feature chunks on
 * a loopback origin, and drives the Scene3D engine on BOTH native WebGL2 and
 * native WebGPU. Both backends are required up front: no fallbacks, no
 * skips.
 *
 * The probe tests the freshly implemented public light-probe contract
 * against an INDEPENDENT analytic reference:
 *   - One face-on square quad with authored constant normalized normals
 *     N=(2,3,6)/7 and the same positions/UVs/tangents as the IOR fixture.
 *   - Expected SH basis constants are computed explicitly at that fixed
 *     normal (and at the rotated world normal (-3,2,6)/7 for the rotated
 *     case). Production SH helpers and production shaders are NEVER
 *     imported or called.
 *   - Reference scenes use ambient RGB primary lights whose intensities are
 *     max(sum of expected coefficient contributions, 0)/PI, with the same
 *     backend/material/camera/geometry and explicit black environment.
 *   - Decoded actual PNG RGBA pixels in a central geometry-only ROI are
 *     compared with a max per-channel tolerance of 2. Positive cases must
 *     also show ample visible coverage; dark controls must match their
 *     (black) reference and stay dark.
 *
 * Contract points covered: 9 linear RGB radiance coefficients {x,y,z},
 * irradiance cosine-convolution (standard l<=2), intensity applied exactly
 * once, no Color tint for valid9, clamping only AFTER summing probes
 * (two signed probes: negative plus positive yields a known small positive),
 * albedo*(1-metalness)/PI diffuse, valid9 zero never falls back to ambient,
 * coefficientless probes use the legacy ambient fallback, rotated geometry
 * normals, and a live 'gosx:hub:event' sequence on a SINGLE unchanged
 * mounted scene. (AO native coverage is not claimed.)
 *
 * Two cases are produced by a real Go program
 * (client/js/testdata/light-probe-typed-fixture) through the actual
 * scene.Props -> SceneIR -> encoding/json wire. Because Vector3 components
 * are json:omitempty, this proves that OMITTED components mean zero and
 * that Color carries no tint for valid9.
 *
 * Live case: one unchanged mounted scene whose light has id 'probe' and
 * live:['probe-control']. Public document CustomEvents with FLAT payloads
 * ({coefficients:[...]}) are dispatched; generic sceneApplyLiveEvent applies
 * the raw payload to each listening light. Each stage checks the normalized
 * state coefficients, cached _lightHash changes, real draw/pass/submit
 * advancement, real pixels against independent reference scenes, and that
 * the original mount/canvas/state identity survives the entire sequence
 * before disposal.
 *
 * Draw/pass/submit instrumentation is strictly forwarding (counter-only);
 * the probe never writes engine state or render helpers, never invokes
 * applySceneCommands/normalize helpers directly, and never rewrites
 * uniforms. Readiness is bounded on mounted=true + state + canvas. Any page
 * console error or warning fails the probe. Missing readbacks are failures,
 * never zeros.
 *
 * Runtime .js assets are read from GOSX_PROBE_RUNTIME_ROOT when set
 * (strictly for reading runtime assets, so this same probe and the same Go
 * fixture can run against older built assets as a negative control);
 * default is <repoRoot>/client/js. Usage:
 *
 *   node scene3d-light-probe-browser.cjs <repoRoot> <existingArtifactDir>
 *
 * Writes report.json and per-case PNGs into the artifact directory. */

const fs = require('fs');
const os = require('os');
const path = require('path');
const http = require('http');
const { spawn, spawnSync } = require('child_process');

const REPO = process.argv[2];
const ART = process.argv[3];
if (!REPO || !ART) {
  console.error('usage: node scene3d-light-probe-browser.cjs <repoRoot> <existingArtifactDir>');
  process.exit(2);
}
try {
  if (!fs.statSync(ART).isDirectory()) throw new Error('not a directory');
} catch (e) {
  console.error('artifact dir not usable: ' + ART + ' (' + e.message + ')');
  process.exit(2);
}

// Strictly for reading runtime .js assets; default is the repository build.
const RUNTIME_ROOT = process.env.GOSX_PROBE_RUNTIME_ROOT ||
  path.join(REPO, 'client', 'js');

const errors = [];
const warnings = [];
const fail = (m) => { errors.push(m); };

const W = 256;
const H = 144;
const OVERALL_MS = 240000;
const STEP_MS = 20000;
const MOUNT_WAIT_MS = 20000;
const STAGE_WAIT_MS = 5000;
const GO_TIMEOUT_MS = 60000;

const BACKENDS = [
  { name: 'gl', webgpu: false },
  { name: 'wg', webgpu: true },
];

// ---- Independent analytic reference (no production SH helper, no
// production shader imports). Basis order 0..8 evaluated explicitly at a
// fixed normalized world normal. ----
function basisFor(nx, ny, nz) {
  return [
    0.886227,
    1.023328 * ny,
    1.023328 * nz,
    1.023328 * nx,
    0.858086 * nx * ny,
    0.858086 * ny * nz,
    0.743125 * nz * nz - 0.247708,
    0.858086 * nx * nz,
    0.429043 * (nx * nx - ny * ny),
  ];
}
const N_FACE = [2 / 7, 3 / 7, 6 / 7];
const N_ROT = [-3 / 7, 2 / 7, 6 / 7]; // rotationZ = PI/2 of the authored normal
const B_FACE = basisFor(N_FACE[0], N_FACE[1], N_FACE[2]);
const B_ROT = basisFor(N_ROT[0], N_ROT[1], N_ROT[2]);
const INV_PI = 1 / Math.PI;

// Face-on square quad: positions/UVs/tangents identical to the supplied IOR
// fixture; authored constant normalized normals N=(2,3,6)/7.
const QUAD = (function () {
  const positions = [
    -0.6, -0.6, 0, 0.6, -0.6, 0, 0.6, 0.6, 0,
    -0.6, -0.6, 0, 0.6, 0.6, 0, -0.6, 0.6, 0,
  ];
  const normals = [];
  const uvs = [0, 0, 1, 0, 1, 1, 0, 0, 1, 1, 0, 1];
  const tangents = [];
  for (let i = 0; i < 6; i += 1) {
    normals.push(2 / 7, 3 / 7, 6 / 7);
    tangents.push(1, 0, 0, 1);
  }
  return { positions, normals, uvs, tangents, count: 6 };
})();

function zeros9() {
  const a = [];
  for (let i = 0; i < 9; i += 1) a.push({});
  return a;
}
function withIndex(coeffs, i, comp) {
  const a = coeffs.slice();
  a[i] = comp;
  return a;
}
function probeLight(coeffs, intensity, extra) {
  const l = Object.assign({ id: 'probe', kind: 'light-probe' }, extra || {});
  if (intensity !== undefined) l.intensity = intensity;
  if (coeffs !== undefined) l.coefficients = coeffs;
  return l;
}
// Expected per-channel irradiance E = intensity * sum_i c[i][ch] * B[i].
function expectedIrradiance(coeffs, intensity, basis) {
  const E = [0, 0, 0];
  for (let i = 0; i < 9; i += 1) {
    const c = coeffs[i] || {};
    const b = basis[i];
    E[0] += (typeof c.x === 'number' && Number.isFinite(c.x) ? c.x : 0) * b;
    E[1] += (typeof c.y === 'number' && Number.isFinite(c.y) ? c.y : 0) * b;
    E[2] += (typeof c.z === 'number' && Number.isFinite(c.z) ? c.z : 0) * b;
  }
  return [E[0] * intensity, E[1] * intensity, E[2] * intensity];
}
// Reference ambient RGB primary lights: intensity = max(E_ch, 0)/PI. The
// per-channel max is applied AFTER summing all probes (sum-then-clamp).
function refLights(E) {
  return [
    { id: 'ref-r', kind: 'ambient', color: '#ff0000', intensity: Math.max(E[0], 0) * INV_PI },
    { id: 'ref-g', kind: 'ambient', color: '#00ff00', intensity: Math.max(E[1], 0) * INV_PI },
    { id: 'ref-b', kind: 'ambient', color: '#0000ff', intensity: Math.max(E[2], 0) * INV_PI },
  ];
}
function combinedE(caseDef, basis) {
  const E = [0, 0, 0];
  for (const p of caseDef.probes) {
    const e = expectedIrradiance(p.coeffs, p.intensity, basis);
    E[0] += e[0]; E[1] += e[1]; E[2] += e[2];
  }
  return E;
}

// ---- Go-produced typed fixtures (real Go-to-browser wire path). ----
let GO_FIXTURE = null;
function runGoFixture() {
  const r = spawnSync('go', ['run', './client/js/testdata/light-probe-typed-fixture'], {
    cwd: REPO, timeout: GO_TIMEOUT_MS, encoding: 'utf8',
  });
  if (r.error) throw new Error('go fixture spawn failed: ' + r.error);
  if (r.status !== 0) {
    throw new Error('go fixture exited ' + r.status + ': ' +
      String(r.stderr || '').slice(0, 800));
  }
  let fx;
  try { fx = JSON.parse(r.stdout); } catch (e) {
    throw new Error('go fixture stdout is not JSON: ' + e.message);
  }
  for (const key of ['sparse', 'zero']) {
    const lights = fx[key];
    if (!Array.isArray(lights) || lights.length !== 1) {
      throw new Error('go fixture ' + key + ': expected exactly one light');
    }
    const L = lights[0];
    if (!L || L.id !== 'probe' || L.kind !== 'light-probe') {
      throw new Error('go fixture ' + key + ': unexpected light ' +
        JSON.stringify(L && { id: L.id, kind: L.kind }));
    }
    if (!Array.isArray(L.coefficients) || L.coefficients.length !== 9) {
      throw new Error('go fixture ' + key + ': coefficients must be exactly 9 entries');
    }
    for (let i = 0; i < 9; i += 1) {
      const c = L.coefficients[i];
      if (!c || typeof c !== 'object' || Array.isArray(c)) {
        throw new Error('go fixture ' + key + ': coefficient ' + i + ' malformed');
      }
    }
  }
  const s0 = fx.sparse[0].coefficients[0];
  if (s0.x !== 1 || s0.y !== 0.5) {
    throw new Error('go fixture sparse: c0 must carry x=1, y=0.5');
  }
  if (Object.prototype.hasOwnProperty.call(s0, 'z')) {
    throw new Error('go fixture sparse: z must be OMITTED (omitted components mean zero)');
  }
  for (const key of ['sparse', 'zero']) {
    const arr = fx[key][0].coefficients;
    for (let i = key === 'sparse' ? 1 : 0; i < 9; i += 1) {
      const c = arr[i];
      if ((c.x !== undefined && c.x !== 0) || (c.y !== undefined && c.y !== 0) ||
        (c.z !== undefined && c.z !== 0)) {
        throw new Error('go fixture ' + key + ': coefficient ' + i + ' must be zero-valued');
      }
    }
  }
  return fx;
}

// ---- Case and reference-scene definitions (built after the Go fixture
// run, since two cases consume its light arrays VERBATIM). ----
const CBASE = { x: 0.30, y: 0.26, z: 0.22 }; // small base keeps negative basis visible
const SGN = [0, 0.5, 0.5, -0.35, -0.35, -0.3, 0.45, -0.3, 0.45, -0.25];
const CASE_DEFS = [];
const LIVE_STAGES = [];
const LEGACY = probeLight(undefined, 0.6, { color: '#ff7f00' });

function buildCaseDefs() {
  // All 9 coefficients independently; signed values included. A small base
  // c0 keeps negative basis contributions visible instead of clamping the
  // whole channel sum to black.
  for (let i = 0; i < 9; i += 1) {
    const coeffs = zeros9();
    if (i === 0) coeffs[0] = { x: 0.80, y: 0.76, z: 0.72 };
    else { coeffs[0] = CBASE; coeffs[i] = { x: SGN[i] }; }
    CASE_DEFS.push({
      name: 'coeff' + i,
      probes: [{ coeffs, intensity: 1 }],
      lights: [probeLight(coeffs, 1)],
      kind: 'positive', refKey: 'coeff' + i, basis: B_FACE,
    });
  }
  // Distinct RGB channels; intensity 0.5 applied exactly once; Color
  // '#ff0000' must be ignored because valid9 coefficients carry RGB.
  const RGB = withIndex(zeros9(), 0, { x: 0.9, y: 0.45, z: 0.18 });
  CASE_DEFS.push({
    name: 'rgb-tint',
    probes: [{ coeffs: RGB, intensity: 0.5 }],
    lights: [probeLight(RGB, 0.5, { color: '#ff0000' })],
    kind: 'positive', refKey: 'rgb-tint', basis: B_FACE,
  });
  // Two signed probes summed BEFORE the clamp: (-0.6 + 0.75) * B0 is a
  // known small positive. Per-probe clamping would give the larger
  // positive-only result and fail this comparison.
  const NEG = withIndex(zeros9(), 0, { x: -0.6 });
  const POS = withIndex(zeros9(), 0, { x: 0.75 });
  CASE_DEFS.push({
    name: 'two-signed',
    probes: [{ coeffs: NEG, intensity: 1 }, { coeffs: POS, intensity: 1 }],
    lights: [probeLight(NEG, 1, { id: 'probe-a' }), probeLight(POS, 1, { id: 'probe-b' })],
    kind: 'positive', refKey: 'two-signed', basis: B_FACE,
  });
  // Valid9 all-zero never falls back: dark despite whiteColor/intensity.
  CASE_DEFS.push({
    name: 'zeros-dark',
    probes: [{ coeffs: zeros9(), intensity: 0.5 }],
    lights: [probeLight(zeros9(), 0.5, { color: '#ffffff' })],
    kind: 'dark', refKey: 'black', basis: B_FACE,
  });
  // Fully metallic: diffuse albedo (1-metalness) = 0 => zero diffuse.
  const MET = withIndex(zeros9(), 0, { x: 0.9, y: 0.85, z: 0.8 });
  CASE_DEFS.push({
    name: 'metallic-zero',
    probes: [{ coeffs: MET, intensity: 1 }],
    lights: [probeLight(MET, 1)],
    kind: 'dark', refKey: 'black', basis: B_FACE, object: { metalness: 1 },
  });
  // Unlit material is invariant to probe lighting. materialKind:'unlit' is
  // not a trusted API flag; the explicit unlit:true flag on the standard
  // material and object is what forward shading must honor.
  CASE_DEFS.push({
    name: 'unlit',
    probes: [{ coeffs: MET, intensity: 1 }],
    lights: [probeLight(MET, 1)],
    kind: 'positive', refKey: 'unlit', basis: B_FACE,
    object: { unlit: true, color: '#4080c0' },
  });
  // Coefficientless probe keeps the legacy flat Color/Intensity ambient.
  CASE_DEFS.push({
    name: 'legacy-ambient',
    probes: [],
    lights: [LEGACY],
    kind: 'positive', refKey: 'legacy-ambient', basis: B_FACE,
  });
  // Rotated geometry: rotationZ=PI/2 gives world normal (-3,2,6)/7 with its
  // own independent explicit basis expectations.
  const RC = withIndex(zeros9(), 0, { x: 0.9, y: 0.85, z: 0.8 });
  RC[3] = { x: 0.5 };
  CASE_DEFS.push({
    name: 'rotated',
    probes: [{ coeffs: RC, intensity: 1 }],
    lights: [probeLight(RC, 1)],
    kind: 'positive', refKey: 'rotated', basis: B_ROT,
    object: { rotationZ: Math.PI / 2 },
  });
  // Real Go-produced cases, consumed VERBATIM (zero members truly omitted).
  // Sparse oracle: RGB irradiance [.4431135, .22155675, 0] — derived here
  // from the actual parsed payload (intensity*sum(c*B)), not hand-expanded.
  CASE_DEFS.push({
    name: 'go-sparse',
    probes: [{ coeffs: GO_FIXTURE.sparse[0].coefficients,
      intensity: GO_FIXTURE.sparse[0].intensity }],
    lights: GO_FIXTURE.sparse,
    kind: 'positive', refKey: 'go-sparse', basis: B_FACE,
  });
  CASE_DEFS.push({
    name: 'go-zero',
    probes: [],
    lights: GO_FIXTURE.zero,
    kind: 'dark', refKey: 'black', basis: B_FACE,
  });

  // Live sequence on a SINGLE unchanged mounted scene. Flat payloads only;
  // generic sceneApplyLiveEvent applies them raw to the listening light.
  const C0POS = withIndex(zeros9(), 0, { x: 0.6, y: 0.55, z: 0.5 });
  const C2DIR = withIndex(zeros9(), 2, { x: 0.9, y: 0.85, z: 0.8 });
  LIVE_STAGES.push(
    { name: 'l0-zero', data: null, coeffs: zeros9(), refKey: 'black', dark: true },
    { name: 'l1-c0', data: { coefficients: C0POS }, coeffs: C0POS, refKey: 'live-c0' },
    { name: 'l2-c2', data: { coefficients: C2DIR }, coeffs: C2DIR, refKey: 'live-c2' },
    { name: 'l3-zero', data: { coefficients: zeros9() }, coeffs: zeros9(),
      refKey: 'black', dark: true },
    { name: 'l4-legacy', data: { coefficients: [], color: '#ffffff', intensity: 0.5 },
      coeffs: [], refKey: 'live-legacy' },
    { name: 'l5-zero', data: { coefficients: zeros9() }, coeffs: zeros9(),
      refKey: 'black', dark: true },
  );
}

function refLightsFor(key) {
  // 'black' is one shared reference explicitly reused by every dark control
  // (zeros-dark, metallic-zero, go-zero, live l0/l3/l5).
  if (key === 'black') return refLights([0, 0, 0]);
  // Legacy fallback references are REAL flat ambient lights with the same
  // Color/Intensity as the legacy probe — never an identical LightProbe
  // scene, which would self-validate a wrong fallback.
  if (key === 'legacy-ambient') {
    return [{ id: 'ref-legacy', kind: 'ambient', color: '#ff7f00',
      intensity: 0.6 }];
  }
  if (key === 'live-legacy') {
    return [{ id: 'ref-legacy', kind: 'ambient', color: '#ffffff',
      intensity: 0.5 }];
  }
  // The unlit reference must have NO probe lighting (black ambient): the
  // explicit unlit flag alone must reproduce the color on both sides.
  if (key === 'unlit') {
    return refLights([0, 0, 0]);
  }
  if (key === 'live-c0') {
    return refLights(expectedIrradiance(LIVE_STAGES[1].data.coefficients, 0.5, B_FACE));
  }
  if (key === 'live-c2') {
    return refLights(expectedIrradiance(LIVE_STAGES[2].data.coefficients, 0.5, B_FACE));
  }
  const cd = CASE_DEFS.find((c) => c.refKey === key);
  return refLights(combinedE(cd, cd.basis || B_FACE));
}
function refObjectFor(key) {
  const cd = CASE_DEFS.find((c) => c.refKey === key);
  return cd ? cd.object : undefined;
}

// ---- Manifests / pages ----
function objectFor(over) {
  const o = {
    // Object id 'surface': the light id 'probe' must stay unique within the
    // scene, and the live probe light is looked up by id 'probe'.
    id: 'surface', kind: 'box',
    materialKind: (over && over.materialKind) || 'standard',
    wireframe: false, color: (over && over.color) || '#b0c0d0',
    roughness: 0.35, metalness: (over && over.metalness) || 0,
    vertices: QUAD,
  };
  if (over && over.unlit) o.unlit = true;
  if (over && over.rotationZ) o.rotationZ = over.rotationZ;
  return o;
}
function manifestFor(lights, object, webgpu, mountId, engineId) {
  const p = {
    width: W, height: H, autoRotate: false, animation: false,
    responsive: false, maxDevicePixelRatio: 1, background: '#101418',
    forceWebGL: !webgpu, requireWebGL: !webgpu, preferWebGPU: Boolean(webgpu),
    camera: { x: 0, y: 0, z: 4, fov: 50 },
    // Explicit black environment colors and zero intensities are MANDATORY:
    // an all-zero colorless descriptor would otherwise get a default fill.
    environment: {
      ambientColor: '#000000', skyColor: '#000000', groundColor: '#000000',
      ambientIntensity: 0, skyIntensity: 0, groundIntensity: 0,
    },
    lights,
    objects: [objectFor(object)],
  };
  return JSON.stringify({ engines: [{
    id: engineId, component: 'GoSXScene3D', kind: 'surface', mountId, props: p,
  }] });
}
function htmlFor(mount, manifestJSON) {
  return '<!doctype html><html><head><meta charset="utf-8">' +
    '<link rel="icon" href="data:,"></head><body>' +
    '<div id="' + mount + '" width="' + W + '" height="' + H + '"></div>' +
    '<script type="application/json" id="gosx-manifest">' + manifestJSON +
    '</script><script src="/bootstrap.js"></script></body></html>';
}

// ---- Loopback server with a strict basename allowlist. ----
const ALLOWED_BASENAMES = new Set([
  'bootstrap.js',
  'bootstrap-feature-scene3d-webgl.js',
  'bootstrap-feature-scene3d-webgpu.js',
]);
function readRuntimeBasename(basename) {
  if (!ALLOWED_BASENAMES.has(basename)) return null;
  const p = path.join(RUNTIME_ROOT, basename);
  if (!fs.existsSync(p)) return null;
  return fs.readFileSync(p);
}

const notFound = [];
const ROUTES = new Map();
function buildRoutes() {
  for (const b of BACKENDS) {
    for (const cd of CASE_DEFS) {
      ROUTES.set('/case/' + b.name + '/' + cd.name, htmlFor('scene-probe-main',
        manifestFor(cd.lights, cd.object, b.webgpu, 'scene-probe-main',
          'gosx-engine-probe-main')));
    }
    const refKeys = Array.from(new Set(CASE_DEFS.map((c) => c.refKey)
      .concat(LIVE_STAGES.map((s) => s.refKey))));
    for (const key of refKeys) {
      ROUTES.set('/ref/' + b.name + '/' + key, htmlFor('scene-probe-ref',
        manifestFor(refLightsFor(key), refObjectFor(key), b.webgpu,
          'scene-probe-ref', 'gosx-engine-probe-ref')));
    }
    ROUTES.set('/live/' + b.name, htmlFor('scene-probe-live',
      manifestFor([probeLight(zeros9(), 0.5,
        { color: '#ffffff', live: ['probe-control'] })], undefined, b.webgpu,
        'scene-probe-live', 'gosx-engine-probe-live')));
  }
}

const server = http.createServer((req, res) => {
  const url = (req.url || '/').split('?')[0];
  const route = ROUTES.get(url);
  if (route) {
    res.writeHead(200, { 'content-type': 'text/html', 'content-length': route.length });
    res.end(route);
    return;
  }
  if (url === '/') {
    res.writeHead(200, { 'content-type': 'text/html' });
    res.end('<!doctype html><html><head><meta charset="utf-8">' +
      '<link rel="icon" href="data:,"></head><body>probe-origin</body></html>');
    return;
  }
  const base = url.slice(url.lastIndexOf('/') + 1);
  if (ALLOWED_BASENAMES.has(base)) {
    const body = readRuntimeBasename(base);
    if (body) {
      res.writeHead(200, { 'content-type': 'text/javascript', 'content-length': body.length });
      res.end(body);
      return;
    }
  }
  notFound.push(url);
  res.writeHead(404);
  res.end();
});

// ---- CDP plumbing (bounded, strict). ----
let ws = null;
let chrome = null;
let profile = null;
let msgId = 0;
const pending = new Map();
const listeners = [];

function cdpSend(method, params, sessionId, timeoutMs) {
  if (!ws) return Promise.reject(new Error('CDP connection closed'));
  const id = ++msgId;
  return new Promise((resolve, reject) => {
    const t = setTimeout(() => { pending.delete(id); reject(new Error('CDP timeout: ' + method)); },
      timeoutMs || STEP_MS);
    pending.set(id, { resolve, reject, t });
    try {
      ws.send(JSON.stringify(Object.assign({ id, method }, params ? { params } : {},
        sessionId ? { sessionId } : {})));
    } catch (e) { clearTimeout(t); pending.delete(id); reject(e); }
  });
}

function waitForEvent(name, timeoutMs) {
  return new Promise((resolve, reject) => {
    const entry = { name, resolve, timer: setTimeout(() => {
      const i = listeners.indexOf(entry);
      if (i >= 0) listeners.splice(i, 1);
      reject(new Error('event timeout: ' + name));
    }, timeoutMs || STEP_MS) };
    listeners.push(entry);
  });
}

function dispatch(raw) {
  let m;
  try { m = JSON.parse(raw); } catch (e) { return; }
  if (m.id && pending.has(m.id)) {
    const p = pending.get(m.id);
    pending.delete(m.id);
    clearTimeout(p.t);
    if (m.error) p.reject(new Error(m.error.message));
    else if (m.result && m.result.exceptionDetails) {
      const d = m.result.exceptionDetails;
      p.reject(new Error('Runtime.evaluate exception: ' +
        ((d.exception && d.exception.description) || d.text)));
    } else p.resolve(m.result);
  } else if (m.method) {
    for (let i = listeners.length - 1; i >= 0; i -= 1) {
      if (listeners[i].name === m.method) {
        const e = listeners[i];
        clearTimeout(e.timer);
        listeners.splice(i, 1);
        e.resolve(m.params || {});
      }
    }
    if (m.method === 'Runtime.consoleAPICalled' && m.params && m.params.args) {
      const text = m.params.args.map((x) => x.value !== undefined ? String(x.value) : (x.description || '')).join(' ');
      if (m.params.type === 'error') errors.push('console.error: ' + text);
      else if (m.params.type === 'warning') warnings.push('console.warning: ' + text);
    }
    if (m.method === 'Runtime.exceptionThrown' && m.params && m.params.exceptionDetails) {
      errors.push('page exception: ' + ((m.params.exceptionDetails.exception &&
        m.params.exceptionDetails.exception.description) || m.params.exceptionDetails.text));
    }
  }
}

async function evalSend(send, expression, extra) {
  const r = await send('Runtime.evaluate', Object.assign({ expression, returnByValue: true }, extra || {}));
  return r && r.result && r.result.value;
}

const sleep = (ms) => new Promise((res) => setTimeout(res, ms));

// Strictly forwarding wrappers only: every wrapped native forwards
// arguments/this/result unchanged; observation is counter-only.
const PRELOAD = `
window.__probeGLDraws = 0; window.__probeGLContext = '';
window.__probeWGPasses = 0; window.__probeWGSubmits = 0;
(function () {
  function wrapGL(proto) {
    if (!proto) return;
    ['drawArrays', 'drawElements'].forEach(function (name) {
      var orig = proto[name];
      if (!orig) return;
      proto[name] = function () {
        window.__probeGLDraws += 1;
        window.__probeGLContext = (this instanceof WebGL2RenderingContext) ? 'webgl2' : 'webgl';
        return orig.apply(this, arguments);
      };
    });
  }
  wrapGL(typeof WebGLRenderingContext !== 'undefined' ? WebGLRenderingContext.prototype : null);
  wrapGL(typeof WebGL2RenderingContext !== 'undefined' ? WebGL2RenderingContext.prototype : null);
  if (typeof GPUCommandEncoder !== 'undefined' && GPUCommandEncoder.prototype &&
      GPUCommandEncoder.prototype.beginRenderPass) {
    var origPass = GPUCommandEncoder.prototype.beginRenderPass;
    GPUCommandEncoder.prototype.beginRenderPass = function () {
      window.__probeWGPasses += 1;
      return origPass.apply(this, arguments);
    };
  }
  if (typeof GPUQueue !== 'undefined' && GPUQueue.prototype && GPUQueue.prototype.submit) {
    var origSubmit = GPUQueue.prototype.submit;
    GPUQueue.prototype.submit = function () {
      window.__probeWGSubmits += 1;
      return origSubmit.apply(this, arguments);
    };
  }
})();
`;

const CAPS_START_EXPR = '(function(){' +
  'var c=document.createElement("canvas");var gl2=false;' +
  'try{gl2=!!c.getContext("webgl2");}catch(e){gl2=false;}' +
  'var p=(navigator.gpu&&navigator.gpu.requestAdapter)?' +
  'navigator.gpu.requestAdapter().then(function(a){return {webgl2:gl2,webgpu:!!a};},' +
  'function(){return {webgl2:gl2,webgpu:false};}):' +
  'Promise.resolve({webgl2:gl2,webgpu:false,reason:"navigator.gpu unavailable"});' +
  'window.__probeCapsPromise=p;return true;})()';

function refsSetExpr(mount) {
  return '(function(){var m=document.getElementById(' + JSON.stringify(mount) + ');' +
    'if(!m)return false;' +
    'if(m.getAttribute("data-gosx-scene3d-mounted")!=="true")return false;' +
    'var cv=m.querySelector("canvas");var st=m.__gosxScene3DState;' +
    'if(!cv||!st)return false;' +
    'window.__probeRefs={mount:m,canvas:cv,state:st};return true;})()';
}

function refsCheckExpr(mount) {
  return '(function(){var r=window.__probeRefs;if(!r)return null;' +
    'var m=document.getElementById(' + JSON.stringify(mount) + ');' +
    'var cv=m&&m.querySelector("canvas");var st=m&&m.__gosxScene3DState;' +
    'return {sameMount:m===r.mount,sameCanvas:cv===r.canvas,sameState:st===r.state};})()';
}

function attrsExpr(mount) {
  return '(function(){var m=document.getElementById(' + JSON.stringify(mount) + ');' +
    'if(!m)return null;return {' +
    'mounted:m.getAttribute("data-gosx-scene3d-mounted"),' +
    'renderer:m.getAttribute("data-gosx-scene3d-renderer"),' +
    'fallback:m.getAttribute("data-gosx-scene3d-renderer-fallback")};})()';
}

function surfaceStateExpr(mount, id) {
  return '(function(){var m=document.getElementById(' + JSON.stringify(mount) + ');' +
    'var st=m&&m.__gosxScene3DState;if(!st)return null;' +
    'var o=st.objects&&st.objects.get?st.objects.get(' + JSON.stringify(id) + '):null;' +
    'if(!o)return null;return {unlit:o.unlit===true,' +
    'materialKind:o.materialKind||null};})()';
}

function nativeCountersExpr() {
  return '({draws:window.__probeGLDraws,wgPasses:window.__probeWGPasses,' +
    'wgSubmits:window.__probeWGSubmits})';
}

// sceneState.lights is a real Map (normalize/applySceneLightPatch use
// state.lights.get): read it as a Map, fail when the id is missing, and do
// NOT fall back to array scanning.
function lightStateExpr(mount, id) {
  return '(function(){var m=document.getElementById(' + JSON.stringify(mount) + ');' +
    'var st=m&&m.__gosxScene3DState;' +
    'if(!st||!(st.lights&&st.lights.get))return null;' +
    'var L=st.lights.get(' + JSON.stringify(id) + ');' +
    'if(!L)return null;return {' +
    'kind:L.kind,coefficients:Array.isArray(L.coefficients)?L.coefficients:null,' +
    'color:L.color,intensity:L.intensity,' +
    'hash:(typeof L._lightHash==="number")?L._lightHash:null};})()';
}

function liveProbeExpr(mount) {
  return '(function(){var m=document.getElementById(' + JSON.stringify(mount) + ');' +
    'var st=m&&m.__gosxScene3DState;' +
    'if(!st||!(st.lights&&st.lights.get))return null;' +
    'var L=st.lights.get("probe");' +
    'if(!L)return null;return {light:{' +
    'coefficients:Array.isArray(L.coefficients)?L.coefficients:null,' +
    'hash:(typeof L._lightHash==="number")?L._lightHash:null},' +
    'draws:window.__probeGLDraws,wgPasses:window.__probeWGPasses,' +
    'wgSubmits:window.__probeWGSubmits};})()';
}

function hubExpr(detail) {
  return '(function(){try{document.dispatchEvent(new CustomEvent("gosx:hub:event",' +
    '{detail:' + JSON.stringify(detail) + '}));return true;}catch(e){return false;}})()';
}

function settleFramesExpr(n) {
  return '(function(){return new Promise(function(res){var left=' + n + ';' +
    'function step(){if(left--<=0){res(true);return;}requestAnimationFrame(step);}' +
    'requestAnimationFrame(step);});})()';
}

async function settleFrames(send, n) {
  const v = await evalSend(send, settleFramesExpr(n), { awaitPromise: true });
  if (v !== true) throw new Error('settling ' + n + ' real frames failed');
}

function disposeExpr(engine, mount) {
  return '(function(){try{if(typeof __gosx_dispose_engine!=="function")return false;' +
    '__gosx_dispose_engine(' + JSON.stringify(engine) + ');' +
    'var m=document.getElementById(' + JSON.stringify(mount) + ');' +
    'return !!(m&&!m.__gosxScene3DState);}catch(e){return false;}})()';
}

// Central geometry-only ROI: a genuinely interior central 24x24 window.
// The face-on quad only projects ~46x46 px at this camera, so the previous
// 40% x 40% ROI (102x58) extended past the quad into background; 24x24
// centered on the canvas stays strictly inside the shaded geometry. The
// max RGBA tolerance stays strict at 2; only the ROI geometry changed.
const ROI = {
  x: Math.round(W / 2) - 12, y: Math.round(H / 2) - 12, w: 24, h: 24,
};
function roiCompareExpr(a, b) {
  return 'new Promise(function(res){var A=new Image(),B=new Image(),n=0;' +
    'function load(){if(++n<2)return;try{' +
    'if(A.width!==B.width||A.height!==B.height){res({dimsMatch:false});return;}' +
    'var c=document.createElement("canvas");c.width=A.width;c.height=A.height;' +
    'var x=c.getContext("2d",{willReadFrequently:true});' +
    'x.drawImage(A,0,0);var d1=x.getImageData(0,0,c.width,c.height).data;' +
    'x.clearRect(0,0,c.width,c.height);x.drawImage(B,0,0);' +
    'var d2=x.getImageData(0,0,c.width,c.height).data;' +
    'var R=' + JSON.stringify(ROI) + ';' +
    'var md=0,over=0,lit=0,tot=R.w*R.h,i,px,dbl;' +
    'for(var yy=R.y;yy<R.y+R.h;yy++){for(var xx=R.x;xx<R.x+R.w;xx++){' +
    'i=(yy*c.width+xx)*4;' +
    'var m=Math.max(Math.abs(d1[i]-d2[i]),Math.abs(d1[i+1]-d2[i+1]),' +
    'Math.abs(d1[i+2]-d2[i+2]),Math.abs(d1[i+3]-d2[i+3]));' +
    'if(m>md)md=m;if(m>2)over+=1;' +
    'dbl=d1[i]>10||d1[i+1]>10||d1[i+2]>10;' + // differs from black quad
    'px=Math.abs(d1[i]-16)>10||Math.abs(d1[i+1]-20)>10||Math.abs(d1[i+2]-24)>10;' + // differs from background
    'if(dbl&&px)lit+=1;}}' +
    'res({dimsMatch:true,maxDelta:md,overTol:over,litActual:lit,tot:tot});' +
    '}catch(e){res(null);}}' +
    'A.onload=B.onload=load;A.onerror=B.onerror=function(){res(null);};' +
    'A.src="data:image/png;base64,' + a + '";B.src="data:image/png;base64,' + b + '";})';
}

async function capture(send, mount) {
  const rect = await evalSend(send,
    '(function(){var m=document.getElementById(' + JSON.stringify(mount) + ');' +
    'var cv=m&&m.querySelector("canvas");if(!cv)return null;' +
    'var b=cv.getBoundingClientRect();' +
    'return {x:b.x,y:b.y,width:b.width,height:b.height,dpr:window.devicePixelRatio||1};})()');
  if (!rect) throw new Error('canvas rect unavailable for ' + mount);
  const r = await send('Page.captureScreenshot', { format: 'png', fromSurface: true,
    clip: { x: rect.x, y: rect.y, width: rect.width, height: rect.height, scale: rect.dpr } });
  if (!r || !r.data) throw new Error('screenshot failed for ' + mount);
  return r.data;
}

function writeArtifact(name, base64) {
  try {
    fs.writeFileSync(path.join(ART, name), Buffer.from(base64, 'base64'));
  } catch (e) {
    fail('artifact write failed for ' + name + ': ' + e.message);
  }
}

function checkStateCoeffs(st, expected, label) {
  if (!st || !Array.isArray(st.coefficients)) {
    return { ok: false, why: label + ': normalized coefficients missing' };
  }
  const coeffs = st.coefficients;
  // The actual array length must match expected EXACTLY, even when expected
  // is empty: nine zero entries are NOT a match for no coefficients.
  if (coeffs.length !== expected.length) {
    return { ok: false, why: label + ': coefficients length ' + coeffs.length +
      ' want ' + expected.length };
  }
  for (let i = 0; i < expected.length; i += 1) {
    const c = coeffs[i];
    // Each actual entry must be a non-null, non-array object. A missing,
    // null or array entry is a malformed state, never a silent zero.
    if (!c || typeof c !== 'object' || Array.isArray(c)) {
      return { ok: false, why: label + '[' + i + '] is not a coefficient object: ' +
        String(c) };
    }
    const e = expected[i] || {};
    for (const ch of ['x', 'y', 'z']) {
      // Omitted actual/expected components mean 0. A PRESENT but
      // non-numeric or non-finite actual component must FAIL, not convert.
      const has = Object.prototype.hasOwnProperty.call(c, ch);
      if (has && (typeof c[ch] !== 'number' || !Number.isFinite(c[ch]))) {
        return { ok: false, why: label + '[' + i + '].' + ch +
          ' present but not finite: ' + String(c[ch]) };
      }
      const v = has ? c[ch] : 0;
      const w = typeof e[ch] === 'number' && Number.isFinite(e[ch]) ? e[ch] : 0;
      if (Math.abs(v - w) > 1e-6) {
        return { ok: false, why: label + '[' + i + '].' + ch + '=' + v + ' want ' + w };
      }
    }
  }
  return { ok: true };
}

// No report success with missing cases: definitions and complete evidences
// are asserted exactly (18 static cases, 6 live stages, 18+1 per backend).
function validateCaseCompleteness() {
  if (CASE_DEFS.length !== 18) {
    fail('expected exactly 18 static case definitions, found ' + CASE_DEFS.length);
  }
  if (LIVE_STAGES.length !== 6) {
    fail('expected exactly 6 live stage definitions, found ' + LIVE_STAGES.length);
  }
  const want = BACKENDS.length * (CASE_DEFS.length + 1); // 18 static + 1 live per backend
  if (CASE_EVIDENCE.length !== want) {
    fail('expected ' + want + ' case evidences (18 static + 1 live per ' +
      'backend), found ' + CASE_EVIDENCE.length);
  }
  for (const b of BACKENDS) {
    for (const cd of CASE_DEFS) {
      const ev = CASE_EVIDENCE.find((x) => x.backend === b.name && x.name === cd.name);
      if (!ev || !ev.compare || !ev.compare.dimsMatch ||
        typeof ev.litFraction !== 'number') {
        fail('missing complete case evidence: ' + b.name + '/' + cd.name);
      }
    }
    const live = CASE_EVIDENCE.find((x) => x.backend === b.name && x.name === 'live');
    if (!live || !Array.isArray(live.stages) || live.stages.length !== 6) {
      fail('live case for ' + b.name + ' must record exactly 6 stages');
    } else {
      for (let s = 0; s < live.stages.length; s += 1) {
        const st = live.stages[s];
        if (!st || !st.compare || !st.compare.dimsMatch ||
          typeof st.litFraction !== 'number') {
          fail('live stage ' + s + ' for ' + b.name +
            ' must record a pixel comparison and litFraction');
        }
      }
    }
  }
}

function checkAttrs(attrs, backend, label) {
  const want = backend.webgpu ? 'webgpu' : 'webgl';
  if (!attrs || attrs.mounted !== 'true' || attrs.renderer !== want) {
    fail(label + ' wrong backend attributes: ' + JSON.stringify(attrs) +
      ' (want mounted=true, renderer=' + want + ')');
  }
  if (attrs && attrs.fallback) {
    fail(label + ' fallback attribute set: ' + attrs.fallback);
  }
}

async function navigateAndWait(send, urlPath) {
  const loadP = waitForEvent('Page.loadEventFired', MOUNT_WAIT_MS);
  await send('Page.navigate', { url: BASE + urlPath });
  await loadP;
}

async function waitReady(send, mount) {
  const deadline = Date.now() + MOUNT_WAIT_MS;
  while (Date.now() < deadline) {
    if ((await evalSend(send, refsSetExpr(mount))) === true) return true;
    await sleep(50);
  }
  return false;
}

async function waitStageState(send, mount, expected, label) {
  const deadline = Date.now() + STAGE_WAIT_MS;
  let last = null;
  for (;;) {
    const cur = await evalSend(send, liveProbeExpr(mount));
    last = cur;
    if (cur && cur.light) {
      const r = checkStateCoeffs(cur.light, expected, label);
      if (r.ok) return cur;
    }
    if (Date.now() > deadline) break;
    await sleep(100);
  }
  fail(label + ': normalized state never matched (last coefficients=' +
    JSON.stringify(last && last.light && last.light.coefficients) + ')');
  return null;
}

async function runStaticCase(send, backend, cd) {
  const label = '[' + backend.name + '/' + cd.name + ']';
  const ev = { backend: backend.name, name: cd.name, kind: cd.kind };

  await navigateAndWait(send, '/case/' + backend.name + '/' + cd.name);
  if (!(await waitReady(send, 'scene-probe-main'))) {
    fail(label + ' engine mount readiness timeout');
    return ev;
  }
  ev.attrs = await evalSend(send, attrsExpr('scene-probe-main'));
  checkAttrs(ev.attrs, backend, label);
  await settleFrames(send, 5);
  // Native activity, not just mount existence: real draw/pass/submit
  // counters must have advanced on the actual backend.
  ev.native = await evalSend(send, nativeCountersExpr());
  if (!ev.native || (backend.webgpu
    ? !(ev.native.wgPasses > 0 && ev.native.wgSubmits > 0)
    : !(ev.native.draws > 0))) {
    fail(label + ' no native draw activity observed (counters=' +
      JSON.stringify(ev.native) + ')');
  }

  // Normalized public state must carry exactly the authored coefficients.
  ev.state = [];
  for (const L of cd.lights) {
    const expected = Array.isArray(L.coefficients) ? L.coefficients : [];
    const st = await evalSend(send, lightStateExpr('scene-probe-main', L.id));
    if (!st) { fail(label + ' normalized light ' + L.id + ' unreadable'); continue; }
    const r = checkStateCoeffs(st, expected, label + ' state/' + L.id);
    if (!r.ok) fail(r.why);
    ev.state.push({ id: L.id, kind: st.kind, hash: st.hash });
  }
  if (cd.object && cd.object.unlit) {
    const su = await evalSend(send, surfaceStateExpr('scene-probe-main', 'surface'));
    if (!su || su.unlit !== true) {
      fail(label + ' normalized surface must carry unlit===true: ' +
        JSON.stringify(su));
    }
    ev.surface = su;
  }

  const actual = await capture(send, 'scene-probe-main');
  writeArtifact(backend.name + '-' + cd.name + '.png', actual);
  ev.identity = await evalSend(send, refsCheckExpr('scene-probe-main'));
  if (!ev.identity || !ev.identity.sameMount || !ev.identity.sameCanvas ||
    !ev.identity.sameState) {
    fail(label + ' mount/canvas/state identity changed before capture');
  }

  await navigateAndWait(send, '/ref/' + backend.name + '/' + cd.refKey);
  if (!(await waitReady(send, 'scene-probe-ref'))) {
    fail(label + ' reference mount readiness timeout (' + cd.refKey + ')');
    return ev;
  }
  checkAttrs(await evalSend(send, attrsExpr('scene-probe-ref')), backend,
    label + ' ref');
  await settleFrames(send, 5);
  const refNative = await evalSend(send, nativeCountersExpr());
  if (!refNative || (backend.webgpu
    ? !(refNative.wgPasses > 0 && refNative.wgSubmits > 0)
    : !(refNative.draws > 0))) {
    fail(label + ' reference scene showed no native draw activity');
  }
  const ref = await capture(send, 'scene-probe-ref');
  writeArtifact(backend.name + '-' + cd.name + '-ref.png', ref);

  const cmp = await evalSend(send, roiCompareExpr(actual, ref), { awaitPromise: true });
  ev.compare = cmp;
  if (!cmp || !cmp.dimsMatch) {
    fail(label + ' ROI comparison unavailable: ' + JSON.stringify(cmp));
    return ev;
  }
  if (cmp.maxDelta > 2 || cmp.overTol > 0) {
    fail(label + ' ROI mismatch vs independent reference: maxDelta=' + cmp.maxDelta +
      ' overTol=' + cmp.overTol);
  }
  const litFrac = cmp.litActual / cmp.tot;
  ev.litFraction = litFrac;
  if (cd.kind === 'positive') {
    if (!(litFrac > 0.05)) {
      fail(label + ' insufficient visible coverage: litFraction=' + litFrac.toFixed(4));
    }
  } else if (!(litFrac < 0.02)) {
    fail(label + ' expected dark control, litFraction=' + litFrac.toFixed(4));
  }
  return ev;
}

async function runLiveCase(send, backend) {
  const mount = 'scene-probe-live';
  const engine = 'gosx-engine-probe-live';
  const label = '[live/' + backend.name + ']';
  const ev = { backend: backend.name, name: 'live', stages: [] };

  await navigateAndWait(send, '/live/' + backend.name);
  if (!(await waitReady(send, mount))) {
    fail(label + ' engine mount readiness timeout');
    return ev;
  }
  checkAttrs(await evalSend(send, attrsExpr(mount)), backend, label);
  await settleFrames(send, 8);
  if ((await evalSend(send, refsSetExpr(mount))) !== true) {
    fail(label + ' identity refs not captured');
    return ev;
  }

  const shots = [];
  let prevHash = null;
  let prevCounters = null;
  for (let s = 0; s < LIVE_STAGES.length; s += 1) {
    const stage = LIVE_STAGES[s];
    const sl = label + '/' + stage.name;
    if (stage.data) {
      // Public event only — flat payload applied raw by sceneApplyLiveEvent.
      const okDispatch = await evalSend(send, hubExpr({
        event: 'probe-control', data: stage.data }));
      if (okDispatch !== true) fail(sl + ' hub event dispatch failed');
      await settleFrames(send, 4);
    }
    const cur = await waitStageState(send, mount, stage.coeffs, sl);
    if (!cur) return ev;
    if (typeof cur.light.hash !== 'number') {
      fail(sl + ' cached _lightHash missing on normalized light');
    } else if (prevHash !== null && cur.light.hash === prevHash) {
      fail(sl + ' cached hash did not change: ' + cur.light.hash);
    }
    if (prevCounters) {
      if (backend.webgpu) {
        const passes = cur.wgPasses - prevCounters.wgPasses;
        const submits = cur.wgSubmits - prevCounters.wgSubmits;
        if (!(passes > 0 && submits > 0)) {
          fail(sl + ' WebGPU pass/submit counters did not advance: ' +
            passes + '/' + submits);
        }
      } else if (!(cur.draws - prevCounters.draws > 0)) {
        fail(sl + ' WebGL draw counter did not advance: ' +
          (cur.draws - prevCounters.draws));
      }
    }
    prevHash = cur.light.hash;
    prevCounters = cur;
    await settleFrames(send, 3);
    shots.push(await capture(send, mount));
    ev.stages.push({ name: stage.name, hash: cur.light.hash, draws: cur.draws,
      wgPasses: cur.wgPasses, wgSubmits: cur.wgSubmits });
  }

  // Identity must survive the ENTIRE live sequence before disposal.
  const id = await evalSend(send, refsCheckExpr(mount));
  ev.identity = id;
  if (!id || !id.sameMount || !id.sameCanvas || !id.sameState) {
    fail(label + ' mount/canvas/state identity changed across live sequence: ' +
      JSON.stringify(id));
  }
  const disposed = await evalSend(send, disposeExpr(engine, mount));
  ev.disposed = disposed;
  if (disposed !== true) fail(label + ' disposal did not clear engine state');

  // Compare each live snapshot against an independent reference scene.
  for (let s = 0; s < LIVE_STAGES.length; s += 1) {
    const stage = LIVE_STAGES[s];
    await navigateAndWait(send, '/ref/' + backend.name + '/' + stage.refKey);
    if (!(await waitReady(send, 'scene-probe-ref'))) {
      fail(label + '/' + stage.name + ' reference mount timeout');
      continue;
    }
    checkAttrs(await evalSend(send, attrsExpr('scene-probe-ref')), backend,
      label + '/' + stage.name + ' ref');
    await settleFrames(send, 5);
    const ref = await capture(send, 'scene-probe-ref');
    writeArtifact(backend.name + '-live-' + stage.name + '-ref.png', ref);
    const cmp = await evalSend(send, roiCompareExpr(shots[s], ref), { awaitPromise: true });
    if (!cmp || !cmp.dimsMatch) {
      fail(label + '/' + stage.name + ' pixel comparison unavailable: ' + JSON.stringify(cmp));
      continue;
    }
    if (cmp.maxDelta > 2 || cmp.overTol > 0) {
      fail(label + '/' + stage.name + ' pixels mismatch reference: maxDelta=' +
        cmp.maxDelta + ' overTol=' + cmp.overTol);
    }
    const litFrac = cmp.litActual / cmp.tot;
    if (stage.dark && litFrac >= 0.02) {
      fail(label + '/' + stage.name + ' expected dark, litFraction=' + litFrac.toFixed(4));
    }
    if (!stage.dark && !(litFrac > 0.02)) {
      fail(label + '/' + stage.name + ' expected visible, litFraction=' + litFrac.toFixed(4));
    }
    // Preserve the live pixel comparison and lit fraction in the report.
    ev.stages[s].compare = cmp;
    ev.stages[s].litFraction = litFrac;
    writeArtifact(backend.name + '-live-' + stage.name + '.png', shots[s]);
  }
  return ev;
}

// ---- Owned resources, report, watchdog, and central cleanup. ----
const CASE_EVIDENCE = [];
let BASE = '';
let finished = false;
let exitCode = 0;
let reportWriteFailed = false;

function writeReport(extra) {
  const report = Object.assign({
    errors, warnings, notFound, nativeCaps: global.__caps || null,
    goFixture: global.__goFixtureSummary || null, cases: CASE_EVIDENCE,
  }, extra || {});
  try {
    fs.writeFileSync(path.join(ART, 'report.json'), JSON.stringify(report, null, 2));
  } catch (e) {
    console.error('failed to write report.json: ' + e.message);
    reportWriteFailed = true;
  }
}

function cleanup() {
  return (async () => {
    for (const p of pending.values()) clearTimeout(p.t);
    pending.clear();
    for (const l of listeners) clearTimeout(l.timer);
    listeners.length = 0;
    try { if (ws) ws.close(); } catch (e) {}
    if (chrome) {
      let exited = false;
      const onExit = () => { exited = true; };
      chrome.once('exit', onExit);
      // Graceful SIGTERM first; SIGKILL only after a bounded grace period.
      try { chrome.kill('SIGTERM'); } catch (e) {}
      await Promise.race([
        new Promise((res) => chrome.once('exit', res)), sleep(3000)]);
      if (!exited) {
        try { chrome.kill('SIGKILL'); } catch (e) {}
      }
      await Promise.race([
        new Promise((res) => chrome.once('exit', res)), sleep(5000)]);
      chrome.removeListener('exit', onExit);
    }
    if (profile) {
      try { fs.rmSync(profile, { recursive: true, force: true }); }
      catch (e) { warnings.push('profile cleanup skipped: ' + e.message); }
    }
    await new Promise((res) => {
      let done = false;
      const fin = () => { if (!done) { done = true; res(); } };
      const t = setTimeout(fin, 3000);
      try { server.close(() => { clearTimeout(t); fin(); }); }
      catch (e) { clearTimeout(t); fin(); }
    });
  })();
}

process.on('exit', () => { try { if (chrome) chrome.kill('SIGKILL'); } catch (e) {} });

const watchdog = setTimeout(() => {
  if (finished) return;
  finished = true;
  fail('overall watchdog: probe exceeded ' + OVERALL_MS + 'ms');
  writeReport({ fatal: 'overall watchdog' });
  cleanup().then(() => process.exit(1));
  setTimeout(() => process.exit(1), 5000).unref();
}, OVERALL_MS);

(async () => {
  // Real Go-to-browser path first: the whole suite is fatal without it.
  GO_FIXTURE = runGoFixture();
  global.__goFixtureSummary = {
    ran: true,
    sparseFirst: GO_FIXTURE.sparse[0].coefficients[0],
    zeroOmittedZ: !Object.prototype.hasOwnProperty.call(
      GO_FIXTURE.zero[0].coefficients[0], 'z'),
  };
  buildCaseDefs();
  buildRoutes();

  await new Promise((res, rej) => {
    server.once('error', rej);
    server.listen(0, '127.0.0.1', () => res());
  });
  BASE = 'http://127.0.0.1:' + server.address().port;

  profile = fs.mkdtempSync(path.join(os.tmpdir(), 'gosx-light-probe-'));
  const chromeBin = process.env.GOSX_CHROME_BIN || '/usr/bin/google-chrome';
  if (!fs.existsSync(chromeBin)) throw new Error('chrome binary not found: ' + chromeBin);
  chrome = spawn(chromeBin, [
    '--headless=new', '--no-sandbox', '--use-gl=angle', '--use-angle=gl-egl',
    '--ignore-gpu-blocklist', '--enable-unsafe-swiftshader', '--enable-unsafe-webgpu',
    '--disable-dev-shm-usage', '--user-data-dir=' + profile,
    '--remote-debugging-port=0', 'about:blank',
  ], { stdio: ['ignore', 'ignore', 'pipe'] });

  const wsUrl = await new Promise((resolve, reject) => {
    let buf = '';
    const t = setTimeout(() => reject(new Error('no DevTools ws URL')), 20000);
    const onExit = () => { clearTimeout(t); reject(new Error('chrome exited early: ' + buf)); };
    const onErr = (e) => { clearTimeout(t); reject(new Error('chrome spawn error: ' + e.message)); };
    chrome.stderr.on('data', (d) => {
      buf += d.toString();
      const m = buf.match(/ws:\/\/127\.0\.0\.1:\d+\/devtools\/browser\/[^\s]+/);
      if (m) {
        clearTimeout(t);
        chrome.removeListener('exit', onExit);
        chrome.removeListener('error', onErr);
        resolve(m[0]);
      }
    });
    chrome.once('exit', onExit);
    chrome.once('error', onErr);
  });

  ws = new WebSocket(wsUrl);
  await new Promise((res, rej) => {
    const t = setTimeout(() => rej(new Error('ws connect timeout')), 20000);
    ws.onopen = () => { clearTimeout(t); res(); };
    ws.onerror = () => { clearTimeout(t); rej(new Error('ws error')); };
  });
  ws.onmessage = (evData) => dispatch(evData.data);

  const { targetId } = await cdpSend('Target.createTarget', { url: 'about:blank' });
  const { sessionId } = await cdpSend('Target.attachToTarget', { targetId, flatten: true });
  const send = (method, params, to) => cdpSend(method, params, sessionId, to || STEP_MS);
  await send('Page.enable');
  await send('Runtime.enable');
  await send('Page.addScriptToEvaluateOnNewDocument', { source: PRELOAD });

  // Native capability gate on a real served loopback origin: BOTH WebGL2 and
  // WebGPU are required. No fallback, no skips.
  const capsLoad = waitForEvent('Page.loadEventFired', STEP_MS);
  await send('Page.navigate', { url: BASE + '/' });
  await capsLoad;
  await evalSend(send, CAPS_START_EXPR);
  const caps = await evalSend(send, 'window.__probeCapsPromise', { awaitPromise: true });
  global.__caps = caps;
  if (!caps || caps.webgl2 !== true || caps.webgpu !== true) {
    throw new Error('native WebGL2 and WebGPU are both required; got ' + JSON.stringify(caps));
  }

  for (const b of BACKENDS) {
    for (const cd of CASE_DEFS) {
      CASE_EVIDENCE.push(await runStaticCase(send, b, cd));
    }
    CASE_EVIDENCE.push(await runLiveCase(send, b));
  }
})().catch((e) => {
  fail(String(e && e.stack || e));
}).then(async () => {
  validateCaseCompleteness();
  if (!finished) {
    finished = true;
    clearTimeout(watchdog);
  }
  await cleanup();
  writeReport({});
  exitCode = (errors.length || warnings.length || reportWriteFailed) ? 1 : 0;
  setTimeout(() => process.exit(exitCode), 50);
});
