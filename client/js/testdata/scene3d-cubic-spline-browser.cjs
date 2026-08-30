'use strict';
/* Native browser regression probe for imported glTF CUBICSPLINE animation
 * and exact affine Group.Scale rendering/picking.
 *
 * Boots real Chrome over CDP (Node builtins only), serves the built
 * bootstrap.js plus a strict method-and-path allowlist of feature assets on
 * localhost, and mounts the Scene3D engine twice — once forced onto native
 * WebGL2 and once onto WebGPU. Native WebGL2 AND WebGPU availability are
 * both required up front; there are no fallbacks and no skips.
 *
 * Per backend an affine-only document first proves a retained indexed mesh
 * under non-uniform parent scale, rotated leaf shear, and reflection. Forwarding
 * API wrappers join the exact local vertex upload and model-matrix uniform to
 * the draw which consumes them, assert CW front-face selection and visible
 * pixels, then a real pointer event picks that same sole scene object. A
 * separate document then drives the imported 'curve' clip of the cubic-spline
 * fixture GLB end to end:
 *   - JS mixer (never WASM), all five channels loaded, first-clamp pose is
 *     the authored property values; the baseline screenshot is frozen there.
 *   - Playback starts ONLY via the public document 'gosx:hub:event' with
 *     event 'cubic-control'; time is observed exclusively through
 *     record.animatedTransforms.get(clockNode).translation[0]. Time is never
 *     synthesized and the mixer is never updated directly.
 *   - A public speed:0 event freezes the clock across real frames; sampled
 *     TRS/weights and emitted triangle vertices are compared against the
 *     analytic oracle in cubic-spline-fixture.cjs at the observed clock, and
 *     the pose must be far from linear-endpoint interpolation.
 *   - Public stop with animation:'' and zero fades restores authored
 *     TRS/morph state and baseline pixels exactly on the SAME
 *     mount/canvas/state/record; disposal then clears the engine state.
 *   - Real draw calls (GL) and render passes + queue submissions (WG) are
 *     observed through forwarding wrappers. WebGL pixels come from a native
 *     canvas CDP screenshot. Hosted WebGPU proof mode bypasses native canvas
 *     configuration/presentation and returns a proof-private COPY_SRC render
 *     target to the unmodified production renderer; that exact target is
 *     copied into a mapped buffer after the forwarded product submission.
 *     Changed geometry pixels are compared as RGBA bytes, not by file size or
 *     hash. Actual WebGPU canvas presentation is outside this hosted proof and
 *     remains a release-pinned hardware certification obligation.
 *
 * Any page console error or warning fails the probe. Usage:
 *
 *   node scene3d-cubic-spline-browser.cjs <repoRoot> <existingArtifactDir>
 *
 * Writes report.json and WebGL PNGs into the artifact directory. */

const fs = require('fs');
const os = require('os');
const path = require('path');
const http = require('http');
const { spawn, spawnSync } = require('child_process');

const REPO = process.argv[2];
const ART = process.argv[3];
if (!REPO || !ART) {
  console.error('usage: node scene3d-cubic-spline-browser.cjs <repoRoot> <existingArtifactDir>');
  process.exit(2);
}
try {
  if (!fs.statSync(ART).isDirectory()) throw new Error('not a directory');
} catch (e) {
  console.error('artifact dir not usable: ' + ART + ' (' + e.message + ')');
  process.exit(2);
}

const fixture = require(path.join(__dirname, 'cubic-spline-fixture.cjs'));
const GLB = fixture.buildCubicSplineGLB();
const MUTATION = String(process.env.GOSX_SCENE3D_CUBIC_MUTATION || '').trim();
if (MUTATION && MUTATION !== 'webgpu-no-draw' && MUTATION !== 'webgpu-no-submit') {
  console.error('unsupported GOSX_SCENE3D_CUBIC_MUTATION: ' + MUTATION);
  process.exit(2);
}
const PROOF_TARGET_ENV = 'GOSX_SCENE3D_CUBIC_WEBGPU_TARGET';
const PROOF_TARGET = String(process.env[PROOF_TARGET_ENV] || 'private-texture').trim();
if (PROOF_TARGET !== 'private-texture') {
  console.error(PROOF_TARGET_ENV + ' must be exactly private-texture');
  process.exit(2);
}
const RESTORE_ATOMIC_GAP_MS = Number(
  process.env.GOSX_SCENE3D_CUBIC_RESTORE_ATOMIC_GAP_MS || 0);
if (!Number.isInteger(RESTORE_ATOMIC_GAP_MS) || RESTORE_ATOMIC_GAP_MS < 0 ||
    RESTORE_ATOMIC_GAP_MS > 1000) {
  console.error('GOSX_SCENE3D_CUBIC_RESTORE_ATOMIC_GAP_MS must be an integer from 0 to 1000');
  process.exit(2);
}
const sourceProbe = spawnSync('git', ['rev-parse', 'HEAD'], {
  cwd: REPO, encoding: 'utf8', timeout: 10000,
});
if (sourceProbe.error || sourceProbe.status !== 0 ||
    !/^[0-9a-f]{40}$/.test(String(sourceProbe.stdout || '').trim())) {
  console.error('unable to pin browser proof to source commit');
  process.exit(2);
}
const SOURCE_COMMIT = sourceProbe.stdout.trim();
const errors = [];
const warnings = [];
const fail = (m) => { errors.push(m); };

const W = 320;
const H = 180;
const OVERALL_MS = 240000;
const STEP_MS = 20000;
const MOUNT_WAIT_MS = 30000;
const CLOCK_TIMEOUT_MS = 20000;
const CLOCK_MIN = 0.9;
const CLOCK_MAX = 2.5;

// Select Chromium's test-only SwiftShader paths explicitly for both ANGLE and
// Dawn. WebGL2 still owns a native canvas pixel oracle; WebGPU renders only to
// a device-created private texture and never initializes a swap surface.
const CHROME_GPU_FLAGS = Object.freeze([
  '--ignore-gpu-blocklist',
  '--enable-unsafe-swiftshader',
  '--enable-unsafe-webgpu',
  '--use-gl=angle',
  '--use-angle=swiftshader',
  '--use-webgpu-adapter=swiftshader',
  '--use-gpu-in-tests',
]);

function validateChromeGPUFlags(flags) {
  if (!Array.isArray(flags) || new Set(flags).size !== flags.length) {
    throw new Error('Chrome GPU flags must be unique');
  }
  for (const required of ['--enable-unsafe-swiftshader', '--enable-unsafe-webgpu',
    '--use-gl=angle', '--use-angle=swiftshader',
    '--use-webgpu-adapter=swiftshader', '--use-gpu-in-tests']) {
    if (!flags.includes(required)) throw new Error('required Chrome GPU flag missing: ' + required);
  }
  for (const prefix of ['--use-gl=', '--use-angle=', '--use-webgpu-adapter=']) {
    if (flags.filter((flag) => flag.startsWith(prefix)).length !== 1) {
      throw new Error('Chrome GPU selection must contain exactly one ' + prefix + ' flag');
    }
  }
  return true;
}

validateChromeGPUFlags(CHROME_GPU_FLAGS);

const CHROME_WINDOW_FLAGS = Object.freeze(['--headless=new']);
const CHROME_SWAP_DIAGNOSTICS = Object.freeze([
  'webgpuswapchaintexture',
  'sharedimagebackingfactory',
  'unable to create shared image',
  'non-existent mailbox',
]);
const CHROME_PRE_TEARDOWN_LIFECYCLE_DIAGNOSTICS = Object.freeze([
  'external instance',
  'device was destroyed',
  'device has been lost',
  'gpu device lost',
]);

const CASES = [
  { name: 'gl', webgpu: false, mount: 'scene-cubic-gl', engine: 'gosx-engine-cubic-gl' },
  { name: 'wg', webgpu: true, mount: 'scene-cubic-wg', engine: 'gosx-engine-cubic-wg' },
];

const AFFINE_CASES = [
  { name: 'affine-gl', webgpu: false, mount: 'scene-affine-gl', engine: 'gosx-engine-affine-gl' },
  { name: 'affine-wg', webgpu: true, mount: 'scene-affine-wg', engine: 'gosx-engine-affine-wg' },
];

const AFFINE_PARENT = [
  -2, 0, 0, 0,
  0, 1, 0, 0,
  0, 0, 1, 0,
  0.5, 0.5, 1, 1,
];
const AFFINE_LOCAL_POSITIONS = [-0.4, -0.4, 0, 0.4, -0.4, 0, 0, 0.4, 0];
const AFFINE_MODEL_MATRIX = [
  -Math.SQRT2, Math.SQRT1_2, 0, 0,
  Math.SQRT2, Math.SQRT1_2, 0, 0,
  0, 0, 1, 0,
  0.5, 0.5, 1, 1,
];
const AFFINE_WORLD_POSITIONS = [
  0.5, 0.5 - 0.4 * Math.SQRT2, 1,
  0.5 - 0.8 * Math.SQRT2, 0.5, 1,
  0.5 + 0.4 * Math.SQRT2, 0.5 + 0.2 * Math.SQRT2, 1,
];

const START_DETAIL = { event: 'cubic-control',
  data: { cubic: { animationSpeed: 1, animationFadeInMS: 0, animationFadeOutMS: 0 } } };
const FREEZE_DETAIL = { event: 'cubic-control', data: { cubic: { animationSpeed: 0 } } };
const STOP_DETAIL = { event: 'cubic-control',
  data: { cubic: { animation: '', animationFadeInMS: 0, animationFadeOutMS: 0 } } };

function manifestFor(mount, engine, webgpu) {
  return JSON.stringify({ engines: [{
    id: engine, component: 'GoSXScene3D', kind: 'surface', mountId: mount,
    props: {
      width: W, height: H, autoRotate: false, animation: false,
      responsive: false, maxDevicePixelRatio: 1, background: '#101418',
      camera: { x: 0.5, y: 0.5, z: 3, fov: 50 },
      lights: [{ id: 'key', kind: 'directional', intensity: 1.2,
        directionX: 0, directionY: 0, directionZ: -1 }],
      forceWebGL: !webgpu, requireWebGL: !webgpu, preferWebGPU: Boolean(webgpu),
      models: [{
        id: 'cubic', src: '/models/cubic-spline.glb',
        animation: 'curve', animationSpeed: 0, loop: true,
        animationFadeInMS: 0, animationFadeOutMS: 0,
        live: ['cubic-control'],
      }],
    },
  }] });
}

function affineManifestFor(mount, engine, webgpu) {
  return JSON.stringify({ engines: [{
    id: engine, component: 'GoSXScene3D', kind: 'surface', mountId: mount,
    props: {
      width: W, height: H, autoRotate: false, animation: false,
      responsive: false, maxDevicePixelRatio: 1, background: '#101418',
      camera: { x: 0.5, y: 0.5, z: 3, fov: 50 },
      lights: [{ id: 'key', kind: 'directional', intensity: 1.2,
        directionX: 0, directionY: 0, directionZ: -1 }],
      forceWebGL: !webgpu, requireWebGL: !webgpu, preferWebGPU: Boolean(webgpu),
      pickSignalNamespace: 'affine.pick',
      objects: [{
        id: 'affine-group-child', kind: 'box', pickable: true, color: '#f6a44c', wireframe: false,
        rotationZ: Math.PI / 4, parentMatrix: AFFINE_PARENT,
        vertices: {
          positions: AFFINE_LOCAL_POSITIONS,
          normals: [Math.SQRT1_2, Math.SQRT1_2, 0, Math.SQRT1_2, Math.SQRT1_2, 0,
            Math.SQRT1_2, Math.SQRT1_2, 0],
          uvs: [0, 0, 1, 0, 0.5, 1],
          tangents: [1, 0, 0, 1, 1, 0, 0, 1, 1, 0, 0, 1],
          indices: [0, 1, 2], count: 3, immutable: true, revision: 0,
        },
      }],
    },
  }] });
}

function htmlFor(mount, engine, webgpu, affine) {
  return '<!doctype html><html><head><meta charset="utf-8">' +
    '<link rel="icon" href="data:,"></head><body>' +
    '<div id="' + mount + '" width="' + W + '" height="' + H + '"></div>' +
    '<script type="application/json" id="gosx-manifest">' +
    (affine ? affineManifestFor(mount, engine, webgpu) : manifestFor(mount, engine, webgpu)) + '</script>' +
    '<script src="/bootstrap.js"></script></body></html>';
}

// Exact HTTP contract for this proof. A request is allowed only when method,
// path, and the absence of a query string match one of these entries. The
// runtime event transport is intentionally acknowledged with 204; every
// other request is a fatal proof failure rather than a permissive 404.
const STATIC_ROUTES = new Map([
  ['/bootstrap.js', path.join(REPO, 'client', 'js', 'bootstrap.js')],
  ['/gosx/bootstrap-feature-scene3d-webgl.js', path.join(REPO, 'client', 'js', 'bootstrap-feature-scene3d-webgl.js')],
  ['/gosx/bootstrap-feature-scene3d-webgpu.js', path.join(REPO, 'client', 'js', 'bootstrap-feature-scene3d-webgpu.js')],
  ['/gosx/bootstrap-feature-scene3d-gltf.js', path.join(REPO, 'client', 'js', 'bootstrap-feature-scene3d-gltf.js')],
  ['/gosx/bootstrap-feature-scene3d-animation.js', path.join(REPO, 'client', 'js', 'bootstrap-feature-scene3d-animation.js')],
]);

function requestAllowed(method, pathname, search) {
  if (search !== '') return false;
  if (method === 'POST') return pathname === '/_gosx/client-events';
  return method === 'GET' && (pathname === '/' || pathname === '/case/gl' ||
    pathname === '/case/wg' || pathname === '/case/affine-gl' ||
    pathname === '/case/affine-wg' || pathname === '/models/cubic-spline.glb' ||
    STATIC_ROUTES.has(pathname));
}

const unexpectedRequests = [];
const notFound = [];
const networkFailures = [];
const intentionalNoContent = [];
const clientEventResponses = [];
const networkRequests = new Map();
const server = http.createServer((req, res) => {
  const method = String(req.method || 'GET').toUpperCase();
  const parsed = new URL(req.url || '/', 'http://127.0.0.1');
  const send = (status, body, type) => {
    res.writeHead(status, { 'content-type': type, 'content-length': body.length });
    res.end(body);
  };
  if (!requestAllowed(method, parsed.pathname, parsed.search)) {
    const detail = method + ' ' + parsed.pathname + parsed.search;
    unexpectedRequests.push(detail);
    notFound.push(detail);
    fail('unexpected browser request: ' + detail);
    req.resume();
    send(404, Buffer.from('unexpected request'), 'text/plain');
    return;
  }
  if (method === 'POST') {
    req.once('error', (e) => {
      const detail = method + ' ' + parsed.pathname + ': ' + e.message;
      unexpectedRequests.push(detail);
      fail('client-events request failed: ' + detail);
      if (!res.headersSent) send(500, Buffer.from('client-events request failed'), 'text/plain');
    });
    req.once('end', () => {
      if (res.headersSent) return;
      clientEventResponses.push({ method, path: parsed.pathname, status: 204 });
      res.writeHead(204);
      res.end();
    });
    req.resume();
    return;
  }
  if (parsed.pathname === '/') {
    res.writeHead(200, { 'content-type': 'text/html' });
    res.end('<!doctype html><html><head><meta charset="utf-8">' +
      '<link rel="icon" href="data:,"></head><body>probe-origin</body></html>');
    return;
  }
  if (parsed.pathname === '/case/gl') {
    send(200, Buffer.from(htmlFor('scene-cubic-gl', 'gosx-engine-cubic-gl', false)), 'text/html');
    return;
  }
  if (parsed.pathname === '/case/wg') {
    send(200, Buffer.from(htmlFor('scene-cubic-wg', 'gosx-engine-cubic-wg', true)), 'text/html');
    return;
  }
  if (parsed.pathname === '/case/affine-gl') {
    send(200, Buffer.from(htmlFor('scene-affine-gl', 'gosx-engine-affine-gl', false, true)), 'text/html');
    return;
  }
  if (parsed.pathname === '/case/affine-wg') {
    send(200, Buffer.from(htmlFor('scene-affine-wg', 'gosx-engine-affine-wg', true, true)), 'text/html');
    return;
  }
  if (parsed.pathname === '/models/cubic-spline.glb') {
    send(200, GLB, 'model/gltf-binary');
    return;
  }
  const assetPath = STATIC_ROUTES.get(parsed.pathname);
  try {
    send(200, fs.readFileSync(assetPath), 'text/javascript');
  } catch (e) {
    const detail = method + ' ' + parsed.pathname + ': ' + e.message;
    unexpectedRequests.push(detail);
    fail('allowlisted browser asset unavailable: ' + detail);
    send(500, Buffer.from('allowlisted asset unavailable'), 'text/plain');
  }
});

// ---- CDP plumbing (bounded, strict) ----
let ws = null;
let chrome = null;
let chromeClosed = false;
let profile = null;
let chromeStderrFD = null;
let chromeStderrBytes = 0;
let chromeStderrWriteError = '';
let webGPUIntentionalTeardownStderrByte = null;
let chromeDiagnostics = null;
let msgId = 0;
let activeSessionId = null;
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

function waitForEvent(name, timeoutMs, sessionId) {
  return new Promise((resolve, reject) => {
    const entry = { name, sessionId: sessionId || '', resolve, timer: setTimeout(() => {
      const i = listeners.indexOf(entry);
      if (i >= 0) listeners.splice(i, 1);
      reject(new Error('event timeout: ' + name));
    }, timeoutMs || STEP_MS) };
    listeners.push(entry);
  });
}

function networkFail(message) {
  networkFailures.push(message);
  fail('network failure: ' + message);
}

function inspectNetworkRequest(rawURL, method) {
  let parsed;
  try { parsed = new URL(rawURL); } catch (e) {
    networkFail(String(method || 'GET') + ' invalid URL ' + rawURL);
    return;
  }
  if (parsed.protocol === 'about:' || parsed.protocol === 'data:') return;
  if (parsed.protocol !== 'http:' && parsed.protocol !== 'https:') {
    networkFail(String(method || 'GET') + ' unsupported URL ' + rawURL);
    return;
  }
  if (!BASE || parsed.origin !== BASE) {
    networkFail(String(method || 'GET') + ' escaped loopback origin: ' + rawURL);
    return;
  }
  if (!requestAllowed(String(method || 'GET').toUpperCase(), parsed.pathname, parsed.search)) {
    networkFail(String(method || 'GET') + ' is outside the exact allowlist: ' + rawURL);
  }
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
      if (listeners[i].name === m.method &&
          (!listeners[i].sessionId || listeners[i].sessionId === m.sessionId)) {
        const e = listeners[i];
        clearTimeout(e.timer);
        listeners.splice(i, 1);
        e.resolve(m.params || {});
      }
    }
    // Capability checks and completed case targets intentionally have no
    // diagnostic ownership. Chromium can emit device/context-loss warnings
    // while those throwaway targets are closing; only the live proof target's
    // events are evidence about the case under test.
    if (!activeSessionId || m.sessionId !== activeSessionId) return;
    if (m.method === 'Runtime.consoleAPICalled' && m.params && m.params.args) {
      const text = m.params.args.map((x) => x.value !== undefined ? String(x.value) : (x.description || '')).join(' ');
      if (m.params.type === 'error') errors.push('console.error: ' + text);
      else if (m.params.type === 'warning') warnings.push('console.warning: ' + text);
    }
    if (m.method === 'Runtime.exceptionThrown' && m.params && m.params.exceptionDetails) {
      errors.push('page exception: ' + ((m.params.exceptionDetails.exception &&
        m.params.exceptionDetails.exception.description) || m.params.exceptionDetails.text));
    }
    if (m.method === 'Network.requestWillBeSent' && m.params && m.params.request) {
      networkRequests.set(m.params.requestId, {
        method: m.params.request.method, url: m.params.request.url,
      });
      inspectNetworkRequest(m.params.request.url, m.params.request.method);
    }
    if (m.method === 'Network.responseReceived' && m.params && m.params.response) {
      const response = m.params.response;
      const request = networkRequests.get(m.params.requestId);
      if (request) request.responseStatus = Number(response.status);
      inspectNetworkRequest(response.url, request ? request.method : 'GET');
      if (Number(response.status) >= 400) {
        networkFail('HTTP ' + response.status + ' for ' + response.url);
      }
    }
    if (m.method === 'Network.loadingFailed' && m.params) {
      const request = networkRequests.get(m.params.requestId);
      // Chromium reports a successfully received 204 Fetch as a canceled
      // loadingFailed(net::ERR_ABORTED), even though responseReceived carried
      // 204 and fetch resolved ok. Recognize only that exact, allowlisted
      // no-content terminal signal; every other loadingFailed remains fatal.
      let intentional204 = false;
      if (request && request.method === 'POST' && request.responseStatus === 204 &&
          m.params.errorText === 'net::ERR_ABORTED' && m.params.canceled === true) {
        try {
          const parsed = new URL(request.url);
          intentional204 = parsed.origin === BASE && parsed.pathname === '/_gosx/client-events' &&
            parsed.search === '';
        } catch (e) {}
      }
      if (intentional204) {
        intentionalNoContent.push({
          method: request.method, path: '/_gosx/client-events', status: 204,
          cdpTerminal: 'loadingFailed:net::ERR_ABORTED',
        });
      } else {
        networkFail('loadingFailed ' + (m.params.requestId || '?') + ' ' +
          (request ? request.method + ' ' + request.url : '(request unknown)') + ': ' +
          (m.params.errorText || 'unknown error') +
          (request && Number.isFinite(request.responseStatus) ?
            ' after HTTP ' + request.responseStatus : '') +
          (m.params.canceled ? ' (canceled)' : ''));
      }
      networkRequests.delete(m.params.requestId);
    }
    if (m.method === 'Network.loadingFinished' && m.params) networkRequests.delete(m.params.requestId);
    if (m.method === 'Log.entryAdded' && m.params && m.params.entry) {
      const entry = m.params.entry;
      if (entry.level === 'error') errors.push('browser log error: ' + entry.text);
      else if (entry.level === 'warning') warnings.push('browser log warning: ' + entry.text);
    }
  }
}

async function evalSend(send, expression, extra) {
  const r = await send('Runtime.evaluate', Object.assign({ expression, returnByValue: true }, extra || {}));
  return r && r.result && r.result.value;
}

const sleep = (ms) => new Promise((res) => setTimeout(res, ms));

// Production calls are forwarding-only in the normal proof. The two explicit
// mutation modes are local negative controls: they suppress WebGPU draw or
// submit while leaving the old pass/submission counters green, so acceptance
// must come from the mapped renderer-target pixels rather than those counters.
const PRELOAD = `
window.__cubicGLDraws = 0; window.__cubicGLContext = '';
window.__cubicWGPasses = 0; window.__cubicWGSubmits = 0;
window.__cubicProofMutation = ${JSON.stringify(MUTATION)};
window.__cubicProofTarget = ${JSON.stringify(PROOF_TARGET)};
window.__affineGLUploads = []; window.__affineGLDrawRecords = [];
window.__affineWGUploads = []; window.__affineWGDrawRecords = [];
window.__affineWGExecutedBundles = [];
window.__affineInputEvents = [];
(function () {
  var mutation = window.__cubicProofMutation;
  var proofTarget = window.__cubicProofTarget;
  var nextGLBufferID = 1;
  var glBufferIDs = new WeakMap();
  var glStates = new WeakMap();
  function glBufferID(buffer) {
    if (!buffer) return 0;
    var id = glBufferIDs.get(buffer);
    if (!id) { id = nextGLBufferID++; glBufferIDs.set(buffer, id); }
    return id;
  }
  function glState(ctx) {
    var state = glStates.get(ctx);
    if (!state) {
      state = { arrayBuffer: 0, vao: null, defaults: Object.create(null),
        vaos: new WeakMap(), matrices: new Map(), frontFace: ctx.CCW };
      glStates.set(ctx, state);
    }
    return state;
  }
  function glAttributes(state) {
    if (!state.vao) return state.defaults;
    var attrs = state.vaos.get(state.vao);
    if (!attrs) { attrs = Object.create(null); state.vaos.set(state.vao, attrs); }
    return attrs;
  }
  function floats(value) {
    return value instanceof Float32Array ? Array.prototype.slice.call(value) : null;
  }
  function wrap(proto, name, handler) {
    if (!proto) return;
    var orig = proto[name];
    if (!orig || orig.__gosxAffineProbeWrapped) return;
    var wrapped = function () { return handler.call(this, orig, arguments); };
    wrapped.__gosxAffineProbeWrapped = true;
    proto[name] = wrapped;
  }
  function wrapGL(proto) {
    if (!proto) return;
    ['drawArrays', 'drawElements'].forEach(function (name) {
      wrap(proto, name, function (orig, args) {
        window.__cubicGLDraws += 1;
        window.__cubicGLContext = (this instanceof WebGL2RenderingContext) ? 'webgl2' : 'webgl';
        var state = glState(this);
        window.__affineGLDrawRecords.push({ kind: name,
          buffers: Object.keys(glAttributes(state)).map(function (key) { return glAttributes(state)[key]; }),
          matrices: Array.from(state.matrices.values()), frontFace: state.frontFace });
        return orig.apply(this, args);
      });
    });
    wrap(proto, 'createBuffer', function (orig, args) {
      var buffer = orig.apply(this, args); glBufferID(buffer); return buffer;
    });
    wrap(proto, 'bindBuffer', function (orig, args) {
      if (args[0] === this.ARRAY_BUFFER) glState(this).arrayBuffer = glBufferID(args[1]);
      return orig.apply(this, args);
    });
    wrap(proto, 'bufferData', function (orig, args) {
      var values = floats(args[1]);
      if (values) window.__affineGLUploads.push({ buffer: glState(this).arrayBuffer, values: values });
      return orig.apply(this, args);
    });
    wrap(proto, 'bindVertexArray', function (orig, args) {
      glState(this).vao = args[0] || null; return orig.apply(this, args);
    });
    wrap(proto, 'vertexAttribPointer', function (orig, args) {
      glAttributes(glState(this))[String(args[0])] = glState(this).arrayBuffer;
      return orig.apply(this, args);
    });
    wrap(proto, 'uniformMatrix4fv', function (orig, args) {
      var values = floats(args[2]);
      if (values) glState(this).matrices.set(args[0], values.slice(0, 16));
      return orig.apply(this, args);
    });
    wrap(proto, 'frontFace', function (orig, args) {
      glState(this).frontFace = args[0]; return orig.apply(this, args);
    });
  }
  wrapGL(typeof WebGLRenderingContext !== 'undefined' ? WebGLRenderingContext.prototype : null);
  wrapGL(typeof WebGL2RenderingContext !== 'undefined' ? WebGL2RenderingContext.prototype : null);
  if (mutation === 'webgpu-no-draw' && typeof GPURenderPassEncoder !== 'undefined' &&
      GPURenderPassEncoder.prototype) {
    ['draw', 'drawIndexed', 'drawIndirect', 'drawIndexedIndirect'].forEach(function (name) {
      if (!GPURenderPassEncoder.prototype[name]) return;
      GPURenderPassEncoder.prototype[name] = function () {};
    });
  }

  var readback = window.__cubicWGReadback = {
    pending: null,
    snapshots: Object.create(null),
    failures: [],
    sequence: 0,
    interceptedConfigureCalls: 0,
    interceptedGetCurrentTextureCalls: 0,
    nativeConfigureCalls: 0,
    nativeGetCurrentTextureCalls: 0,
    submitCalls: 0,
    proofCopySubmitCalls: 0,
    reusedConfigureCalls: 0,
    contextCount: 0,
    targetGenerationCount: 0,
    results: Object.create(null),
    arm: function (canvas, label) {
      var self = this;
      label = String(label || '');
      if (!canvas) return false;
      if (self.pending) return false;
      self.results[label] = new Promise(function (resolve) {
        var pending = { canvas: canvas, label: String(label || ''), resolve: resolve, timer: null };
        pending.timer = setTimeout(function () {
          if (self.pending !== pending) return;
          self.pending = null;
          var error = 'private-target readback timed out (configure=' +
            self.interceptedConfigureCalls + ', texture=' +
            self.interceptedGetCurrentTextureCalls + ', submit=' + self.submitCalls + ')';
          self.failures.push(error);
          resolve({ error: error });
        }, 10000);
        self.pending = pending;
      });
      return true;
    },
    result: function (label) {
      return this.results[String(label || '')] || Promise.resolve({ error: 'readback was not armed' });
    },
    capture: function (canvas, label) {
      return this.arm(canvas, label)
        ? this.result(label)
        : Promise.resolve({ error: canvas ? 'readback already pending' : 'canvas unavailable' });
    },
    compare: function (a, b) {
      var left = this.snapshots[String(a || '')];
      var right = this.snapshots[String(b || '')];
      if (!left || !right) return null;
      if (left.width !== right.width || left.height !== right.height ||
          left.pixels.length !== right.pixels.length) return { dimsMatch: false };
      var d1 = left.pixels, d2 = right.pixels;
      var exactBytes = 0, exactPixels = 0, meanChanged = 0, maxDelta = 0;
      for (var i = 0; i < d1.length; i += 4) {
        var delta = Math.max(Math.abs(d1[i] - d2[i]), Math.abs(d1[i + 1] - d2[i + 1]),
          Math.abs(d1[i + 2] - d2[i + 2]), Math.abs(d1[i + 3] - d2[i + 3]));
        if (delta > 0) {
          if (d1[i] !== d2[i]) exactBytes++;
          if (d1[i + 1] !== d2[i + 1]) exactBytes++;
          if (d1[i + 2] !== d2[i + 2]) exactBytes++;
          if (d1[i + 3] !== d2[i + 3]) exactBytes++;
          exactPixels++;
          if (delta > 2) meanChanged++;
        }
        if (delta > maxDelta) maxDelta = delta;
      }
      return { dimsMatch: true, exactBytes: exactBytes, exactPixels: exactPixels,
        meanChanged: meanChanged, maxDelta: maxDelta };
    },
    receipt: function () {
      return {
        proofTarget: proofTarget,
        renderTargetKind: 'proof-private-gpu-texture',
        canvasPresented: false,
        interceptedConfigureCalls: this.interceptedConfigureCalls,
        interceptedGetCurrentTextureCalls: this.interceptedGetCurrentTextureCalls,
        nativeConfigureCalls: this.nativeConfigureCalls,
        nativeGetCurrentTextureCalls: this.nativeGetCurrentTextureCalls,
        productSubmitCalls: this.submitCalls,
        proofCopySubmitCalls: this.proofCopySubmitCalls,
        reusedConfigureCalls: this.reusedConfigureCalls,
        contextCount: this.contextCount,
        targetGenerationCount: this.targetGenerationCount,
        failures: this.failures.slice(),
        configuredTargets: configuredTargets.map(targetReceipt),
        capturedLabels: Object.keys(this.snapshots).sort(),
      };
    },
  };

  function readbackFailure(pending, reason) {
    if (pending.timer) clearTimeout(pending.timer);
    var message = String(reason && (reason.message || reason) || 'unknown readback failure');
    readback.failures.push(message);
    pending.resolve({ error: message });
  }

  function readbackSummary(snapshot) {
    var pixels = snapshot.pixels;
    var r = pixels[0] || 0, g = pixels[1] || 0, b = pixels[2] || 0, a = pixels[3] || 0;
    var foregroundPixels = 0;
    for (var i = 0; i < pixels.length; i += 4) {
      if (Math.max(Math.abs(pixels[i] - r), Math.abs(pixels[i + 1] - g),
          Math.abs(pixels[i + 2] - b), Math.abs(pixels[i + 3] - a)) > 2) {
        foregroundPixels++;
      }
    }
    return { label: snapshot.label, sequence: snapshot.sequence,
      width: snapshot.width, height: snapshot.height, format: snapshot.format,
      byteLength: pixels.length, pixelSHA256: snapshot.pixelSHA256,
      cornerRGBA: [r, g, b, a],
      foregroundPixels: foregroundPixels,
      renderTargetKind: snapshot.renderTargetKind,
      canvasPresented: snapshot.canvasPresented,
      contextID: snapshot.contextID,
      deviceID: snapshot.deviceID,
      queueID: snapshot.queueID,
      targetID: snapshot.targetID,
      targetGeneration: snapshot.targetGeneration,
      configureSequence: snapshot.configureSequence,
      getCurrentTextureSequence: snapshot.getCurrentTextureSequence,
      targetViewSequence: snapshot.targetViewSequence,
      renderPassSequence: snapshot.renderPassSequence,
      commandBufferSequence: snapshot.commandBufferSequence,
      productSubmitSequence: snapshot.productSubmitSequence,
      proofCopySequence: snapshot.proofCopySequence,
      targetUsage: snapshot.targetUsage,
      alphaMode: snapshot.alphaMode,
      colorSpace: snapshot.colorSpace,
      toneMapping: snapshot.toneMapping == null
        ? null : JSON.parse(JSON.stringify(snapshot.toneMapping)),
      returnedToProduct: snapshot.returnedToProduct,
      productRenderPassLinked: snapshot.productRenderPassLinked,
      productCommandBufferLinked: snapshot.productCommandBufferLinked,
      productQueueMatched: snapshot.productQueueMatched,
      proofCommandKinds: snapshot.proofCommandKinds.slice(),
      interceptedConfigureCalls: readback.interceptedConfigureCalls,
      interceptedGetCurrentTextureCalls: readback.interceptedGetCurrentTextureCalls,
      nativeConfigureCalls: readback.nativeConfigureCalls,
      nativeGetCurrentTextureCalls: readback.nativeGetCurrentTextureCalls,
      productSubmissionForwarded: snapshot.productSubmissionForwarded };
  }

  var identityMaps = {
    context: new WeakMap(), device: new WeakMap(), queue: new WeakMap(), target: new WeakMap(),
  };
  var identityCounts = { context: 0, device: 0, queue: 0, target: 0 };
  function identity(kind, value) {
    var map = identityMaps[kind];
    var token = map.get(value);
    if (!token) {
      token = kind + '-' + (++identityCounts[kind]);
      map.set(value, token);
    }
    return token;
  }
  function targetReceipt(meta) {
    return {
      renderTargetKind: 'proof-private-gpu-texture',
      canvasPresented: false,
      contextID: meta.contextID,
      deviceID: meta.deviceID,
      queueID: meta.queueID,
      targetID: meta.targetID,
      targetGeneration: meta.targetGeneration,
      configureSequence: meta.configureSequence,
      width: meta.width,
      height: meta.height,
      format: meta.format,
      productionUsage: meta.productionUsage,
      targetUsage: meta.targetUsage,
      viewFormats: meta.viewFormats.slice(),
      alphaMode: meta.alphaMode,
      colorSpace: meta.colorSpace,
      toneMapping: meta.toneMapping == null
        ? null : JSON.parse(JSON.stringify(meta.toneMapping)),
      configureCalls: meta.configureCalls,
      reusedConfigureCalls: meta.reusedConfigureCalls,
      returnedToProduct: meta.returnedToProduct,
      getCurrentTextureCalls: meta.getCurrentTextureCalls,
      targetViewCalls: meta.targetViewCalls,
      linkedRenderPasses: meta.linkedRenderPasses,
      linkedCommandBuffers: meta.linkedCommandBuffers,
      linkedProductSubmits: meta.linkedProductSubmits,
    };
  }
  function failClosed(message) {
    readback.failures.push(String(message));
    throw new Error(String(message));
  }

  var contexts = new WeakMap();
  var targetMetaByTexture = new WeakMap();
  var targetMetaByView = new WeakMap();
  var encoderLinks = new WeakMap();
  var commandBufferLinks = new WeakMap();
  var configuredTargets = [];
  var configureSequence = 0;
  var getCurrentTextureSequence = 0;
  var targetViewSequence = 0;
  var renderPassSequence = 0;
  var commandBufferSequence = 0;
  var productSubmitSequence = 0;
  var proofCopySequence = 0;

  function optionalString(value, label) {
    if (value == null) return null;
    if (typeof value !== 'string' || !value) {
      failClosed('private target ' + label + ' is invalid');
    }
    return value;
  }
  function copyToneMapping(value) {
    if (value == null) return null;
    if (typeof value !== 'object' || Array.isArray(value)) {
      failClosed('private target toneMapping is invalid');
    }
    var copied;
    try { copied = JSON.parse(JSON.stringify(value)); }
    catch (_error) { failClosed('private target toneMapping is not serializable'); }
    if (!copied || typeof copied !== 'object' || Array.isArray(copied)) {
      failClosed('private target toneMapping did not serialize as an object');
    }
    return copied;
  }
  function sameArray(left, right) {
    if (left.length !== right.length) return false;
    for (var i = 0; i < left.length; i++) if (left[i] !== right[i]) return false;
    return true;
  }
  function sameJSON(left, right) {
    return JSON.stringify(left) === JSON.stringify(right);
  }

  if (typeof GPUCanvasContext !== 'undefined' && GPUCanvasContext.prototype &&
      GPUCanvasContext.prototype.configure && GPUCanvasContext.prototype.getCurrentTexture &&
      typeof GPUTextureUsage !== 'undefined') {
    GPUCanvasContext.prototype.configure = function (config) {
      readback.interceptedConfigureCalls++;
      var next = config || {};
      var device = next.device;
      var canvas = this.canvas;
      if (proofTarget !== 'private-texture') failClosed('unsupported WebGPU proof target');
      if (!device || typeof device.createTexture !== 'function' || !device.queue) {
        failClosed('private target configure requires the production GPUDevice and queue');
      }
      if (!canvas) failClosed('private target configure requires a canvas-owned context');
      var width = Number(canvas.width);
      var height = Number(canvas.height);
      if (!Number.isInteger(width) || width <= 0 || !Number.isInteger(height) || height <= 0) {
        failClosed('private target dimensions are invalid: ' + width + 'x' + height);
      }
      var format = optionalString(next.format, 'format');
      var productionUsage = next.usage == null
        ? GPUTextureUsage.RENDER_ATTACHMENT : Number(next.usage);
      if (!Number.isInteger(productionUsage) || productionUsage < 0 ||
          productionUsage > 0xffffffff) {
        failClosed('private target production usage is invalid');
      }
      productionUsage = productionUsage >>> 0;
      var targetUsage = (productionUsage | GPUTextureUsage.RENDER_ATTACHMENT |
        GPUTextureUsage.COPY_SRC) >>> 0;
      if (next.viewFormats != null && !Array.isArray(next.viewFormats)) {
        failClosed('private target viewFormats must be an array');
      }
      var viewFormats = Array.isArray(next.viewFormats) ? next.viewFormats.slice() : [];
      if (!viewFormats.every(function (value) {
        return typeof value === 'string' && value.length > 0;
      })) failClosed('private target viewFormats are invalid');
      var alphaMode = optionalString(next.alphaMode, 'alphaMode');
      var colorSpace = optionalString(next.colorSpace, 'colorSpace');
      var toneMapping = copyToneMapping(next.toneMapping);
      var previous = contexts.get(this);
      if (previous && previous.canvas === canvas && previous.device === device &&
          previous.width === width && previous.height === height &&
          previous.format === format && previous.productionUsage === productionUsage &&
          sameArray(previous.viewFormats, viewFormats) &&
          previous.alphaMode === alphaMode && previous.colorSpace === colorSpace &&
          sameJSON(previous.toneMapping, toneMapping)) {
        readback.reusedConfigureCalls++;
        previous.configureCalls++;
        previous.reusedConfigureCalls++;
        return undefined;
      }
      var texture = device.createTexture({
        label: 'gosx-cubic-proof-private-target',
        size: { width: width, height: height, depthOrArrayLayers: 1 },
        mipLevelCount: 1,
        sampleCount: 1,
        dimension: '2d',
        format: format,
        usage: targetUsage,
        viewFormats: viewFormats,
      });
      if (previous && previous.texture && typeof previous.texture.destroy === 'function') {
        try { previous.texture.destroy(); } catch (_error) {}
      } else if (!previous) {
        readback.contextCount++;
      }
      var meta = {
        canvas: canvas,
        context: this,
        device: device,
        queue: device.queue,
        texture: texture,
        contextID: identity('context', this),
        deviceID: identity('device', device),
        queueID: identity('queue', device.queue),
        targetID: identity('target', texture),
        targetGeneration: ++readback.targetGenerationCount,
        configureSequence: ++configureSequence,
        width: width,
        height: height,
        format: format,
        productionUsage: productionUsage,
        targetUsage: targetUsage,
        viewFormats: viewFormats,
        alphaMode: alphaMode,
        colorSpace: colorSpace,
        toneMapping: toneMapping,
        configureCalls: 1,
        reusedConfigureCalls: 0,
        returnedToProduct: false,
        getCurrentTextureCalls: 0,
        targetViewCalls: 0,
        linkedRenderPasses: 0,
        linkedCommandBuffers: 0,
        linkedProductSubmits: 0,
        lastGetCurrentTextureSequence: 0,
      };
      contexts.set(this, meta);
      targetMetaByTexture.set(texture, meta);
      configuredTargets.push(meta);
      return undefined;
    };
    GPUCanvasContext.prototype.getCurrentTexture = function () {
      readback.interceptedGetCurrentTextureCalls++;
      var meta = contexts.get(this);
      if (!meta || !meta.texture) failClosed('getCurrentTexture preceded private configure');
      meta.returnedToProduct = true;
      meta.getCurrentTextureCalls++;
      meta.lastGetCurrentTextureSequence = ++getCurrentTextureSequence;
      return meta.texture;
    };
  }

  if (typeof GPUTexture !== 'undefined' && GPUTexture.prototype &&
      GPUTexture.prototype.createView) {
    var origCreateView = GPUTexture.prototype.createView;
    GPUTexture.prototype.createView = function () {
      var view = origCreateView.apply(this, arguments);
      var meta = targetMetaByTexture.get(this);
      if (meta) {
        meta.targetViewCalls++;
        meta.lastTargetViewSequence = ++targetViewSequence;
        targetMetaByView.set(view, meta);
      }
      return view;
    };
  }

  if (typeof GPUCommandEncoder !== 'undefined' && GPUCommandEncoder.prototype &&
      GPUCommandEncoder.prototype.beginRenderPass) {
    var origPass = GPUCommandEncoder.prototype.beginRenderPass;
    GPUCommandEncoder.prototype.beginRenderPass = function (descriptor) {
      window.__cubicWGPasses += 1;
      var passSequence = ++renderPassSequence;
      var attachments = descriptor && descriptor.colorAttachments;
      var linkedMeta = null;
      if (attachments && typeof attachments.length === 'number') {
        for (var i = 0; i < attachments.length; i++) {
          var attachment = attachments[i];
          if (!attachment) continue;
          var meta = targetMetaByView.get(attachment.view) ||
            targetMetaByView.get(attachment.resolveTarget);
          if (!meta) continue;
          if (linkedMeta && linkedMeta !== meta) {
            failClosed('one render pass linked multiple private targets');
          }
          linkedMeta = meta;
        }
      }
      if (linkedMeta) {
        if (!linkedMeta.returnedToProduct || linkedMeta.lastGetCurrentTextureSequence <= 0) {
          failClosed('private target render pass was not returned through getCurrentTexture');
        }
        var existing = encoderLinks.get(this);
        if (existing && existing.meta !== linkedMeta) {
          failClosed('one command encoder linked multiple private targets');
        }
        linkedMeta.linkedRenderPasses++;
        encoderLinks.set(this, {
          meta: linkedMeta,
          getCurrentTextureSequence: linkedMeta.lastGetCurrentTextureSequence,
          targetViewSequence: linkedMeta.lastTargetViewSequence,
          renderPassSequence: passSequence,
        });
      }
      return origPass.apply(this, arguments);
    };
    if (GPUCommandEncoder.prototype.finish) {
      var origFinish = GPUCommandEncoder.prototype.finish;
      GPUCommandEncoder.prototype.finish = function () {
        var commandBuffer = origFinish.apply(this, arguments);
        var link = encoderLinks.get(this);
        if (link) {
          link.meta.linkedCommandBuffers++;
          commandBufferLinks.set(commandBuffer, {
            meta: link.meta,
            getCurrentTextureSequence: link.getCurrentTextureSequence,
            targetViewSequence: link.targetViewSequence,
            renderPassSequence: link.renderPassSequence,
            commandBufferSequence: ++commandBufferSequence,
          });
        }
        return commandBuffer;
      };
    }
  }

  if (typeof GPUQueue !== 'undefined' && GPUQueue.prototype && GPUQueue.prototype.submit &&
      typeof GPUBufferUsage !== 'undefined' && typeof GPUMapMode !== 'undefined') {
    var origSubmit = GPUQueue.prototype.submit;
    GPUQueue.prototype.submit = function (commandBuffers) {
      readback.submitCalls++;
      window.__cubicWGSubmits += 1;
      var link = null;
      if (commandBuffers && typeof commandBuffers.length === 'number') {
        for (var i = 0; i < commandBuffers.length; i++) {
          var candidate = commandBufferLinks.get(commandBuffers[i]);
          if (!candidate) continue;
          if (link && link.meta !== candidate.meta) {
            failClosed('one queue submit linked multiple private targets');
          }
          link = candidate;
        }
      }
      var surface = link && link.meta;
      var productQueueMatched = !surface || this === surface.queue;
      if (surface && !productQueueMatched) {
        failClosed('private target command buffer reached a different product queue');
      }
      var suppress = mutation === 'webgpu-no-submit' && !!surface;
      var submitSequence = 0;
      if (surface) {
        submitSequence = ++productSubmitSequence;
        surface.linkedProductSubmits++;
      }
      var result;
      if (!suppress) result = origSubmit.apply(this, arguments);

      var pending = readback.pending;
      if (!pending || !surface || pending.canvas !== surface.canvas) return result;
      readback.pending = null;
      if (pending.timer) clearTimeout(pending.timer);
      var buffer = null;
      try {
        var device = surface.device;
        var width = surface.width;
        var height = surface.height;
        var bytesPerRow = Math.ceil(width * 4 / 256) * 256;
        buffer = device.createBuffer({ size: bytesPerRow * height,
          usage: GPUBufferUsage.COPY_DST | GPUBufferUsage.MAP_READ });
        var encoder = device.createCommandEncoder({ label: 'gosx-cubic-proof-readback' });
        encoder.copyTextureToBuffer({ texture: surface.texture },
          { buffer: buffer, bytesPerRow: bytesPerRow, rowsPerImage: height },
          { width: width, height: height, depthOrArrayLayers: 1 });
        // Bypass this wrapper: the proof copy is not a product submission and
        // must not make the product submission counter pass.
        origSubmit.call(this, [encoder.finish()]);
        readback.proofCopySubmitCalls++;
        var copySequence = ++proofCopySequence;
        var sequence = ++readback.sequence;
        buffer.mapAsync(GPUMapMode.READ).then(function () {
          try {
            var padded = new Uint8Array(buffer.getMappedRange());
            var dense = new Uint8Array(width * height * 4);
            for (var y = 0; y < height; y++) {
              dense.set(padded.subarray(y * bytesPerRow, y * bytesPerRow + width * 4), y * width * 4);
            }
            if (String(surface.format || '').toLowerCase().indexOf('bgra') === 0) {
              for (var i = 0; i < dense.length; i += 4) {
                var red = dense[i];
                dense[i] = dense[i + 2];
                dense[i + 2] = red;
              }
            }
            buffer.unmap();
            buffer.destroy();
            if (!globalThis.crypto || !globalThis.crypto.subtle) {
              throw new Error('Web Crypto SHA-256 unavailable for renderer-target receipt');
            }
            globalThis.crypto.subtle.digest('SHA-256', dense).then(function (digest) {
              try {
                var bytes = new Uint8Array(digest);
                var pixelSHA256 = '';
                for (var j = 0; j < bytes.length; j++) {
                  pixelSHA256 += bytes[j].toString(16).padStart(2, '0');
                }
                var snapshot = { label: pending.label, sequence: sequence,
                  width: width, height: height,
                  format: String(surface.format || ''), pixels: dense,
                  pixelSHA256: pixelSHA256,
                  renderTargetKind: 'proof-private-gpu-texture',
                  canvasPresented: false,
                  contextID: surface.contextID,
                  deviceID: surface.deviceID,
                  queueID: surface.queueID,
                  targetID: surface.targetID,
                  targetGeneration: surface.targetGeneration,
                  configureSequence: surface.configureSequence,
                  getCurrentTextureSequence: link.getCurrentTextureSequence,
                  targetViewSequence: link.targetViewSequence,
                  renderPassSequence: link.renderPassSequence,
                  commandBufferSequence: link.commandBufferSequence,
                  productSubmitSequence: submitSequence,
                  proofCopySequence: copySequence,
                  targetUsage: surface.targetUsage,
                  alphaMode: surface.alphaMode,
                  colorSpace: surface.colorSpace,
                  toneMapping: surface.toneMapping == null
                    ? null : JSON.parse(JSON.stringify(surface.toneMapping)),
                  returnedToProduct: surface.returnedToProduct,
                  productRenderPassLinked: link.renderPassSequence > 0,
                  productCommandBufferLinked: link.commandBufferSequence > 0,
                  productQueueMatched: productQueueMatched,
                  proofCommandKinds: ['copyTextureToBuffer'],
                  productSubmissionForwarded: !suppress };
                readback.snapshots[pending.label] = snapshot;
                pending.resolve(readbackSummary(snapshot));
              } catch (err) {
                readbackFailure(pending, err);
              }
            }, function (err) { readbackFailure(pending, err); });
          } catch (err) {
            try { buffer.unmap(); } catch (_err) {}
            try { buffer.destroy(); } catch (_err) {}
            readbackFailure(pending, err);
          }
        }, function (err) {
          try { buffer.destroy(); } catch (_err) {}
          readbackFailure(pending, err);
        });
      } catch (err) {
        try { if (buffer) buffer.destroy(); } catch (_err) {}
        readbackFailure(pending, err);
      }
      return result;
    };
  }

  var nextWGBufferID = 1;
  var wgBufferIDs = new WeakMap();
  var wgBindGroups = new WeakMap();
  var wgPipelines = new WeakMap();
  var wgPassStates = new WeakMap();
  function wgBufferID(buffer) {
    if (!buffer) return 0;
    var id = wgBufferIDs.get(buffer);
    if (!id) { id = nextWGBufferID++; wgBufferIDs.set(buffer, id); }
    return id;
  }
  function wgPassState(pass) {
    var state = wgPassStates.get(pass);
    if (!state) {
      state = { vertexBuffers: Object.create(null), groups: Object.create(null),
        frontFace: '', cullMode: '', records: [] };
      wgPassStates.set(pass, state);
    }
    return state;
  }
  if (typeof GPUDevice !== 'undefined' && GPUDevice.prototype) {
    wrap(GPUDevice.prototype, 'createBuffer', function (orig, args) {
      var buffer = orig.apply(this, args); wgBufferID(buffer); return buffer;
    });
    wrap(GPUDevice.prototype, 'createBindGroup', function (orig, args) {
      var group = orig.apply(this, args), bindings = Object.create(null);
      var entries = args[0] && Array.isArray(args[0].entries) ? args[0].entries : [];
      entries.forEach(function (entry) {
        var resource = entry && entry.resource;
        if (resource && resource.buffer) bindings[String(entry.binding)] = wgBufferID(resource.buffer);
      });
      wgBindGroups.set(group, bindings); return group;
    });
    wrap(GPUDevice.prototype, 'createRenderPipeline', function (orig, args) {
      var pipeline = orig.apply(this, args);
      var primitive = args[0] && args[0].primitive || {};
      wgPipelines.set(pipeline, {
        frontFace: String(primitive.frontFace || 'ccw'),
        cullMode: String(primitive.cullMode || 'none'),
      });
      return pipeline;
    });
  }
  if (typeof GPUCommandEncoder !== 'undefined' && GPUCommandEncoder.prototype &&
      GPUCommandEncoder.prototype.beginRenderPass) {
    wrap(GPUCommandEncoder.prototype, 'beginRenderPass', function (orig, args) {
      var pass = orig.apply(this, args); wgPassState(pass); return pass;
    });
  }
  if (typeof GPUQueue !== 'undefined' && GPUQueue.prototype) {
    wrap(GPUQueue.prototype, 'writeBuffer', function (orig, args) {
      var values = floats(args[2]);
      if (values) window.__affineWGUploads.push({ buffer: wgBufferID(args[0]), values: values });
      return orig.apply(this, args);
    });
    wrap(GPUQueue.prototype, 'submit', function (orig, args) {
      return orig.apply(this, args);
    });
  }
  function wrapWGDrawEncoder(proto, retained) {
    if (!proto) return;
    wrap(proto, 'setVertexBuffer', function (orig, args) {
      wgPassState(this).vertexBuffers[String(args[0])] = wgBufferID(args[1]);
      return orig.apply(this, args);
    });
    wrap(proto, 'setBindGroup', function (orig, args) {
      wgPassState(this).groups[String(args[0])] = wgBindGroups.get(args[1]) || null;
      return orig.apply(this, args);
    });
    wrap(proto, 'setPipeline', function (orig, args) {
      var primitive = wgPipelines.get(args[0]) || {};
      wgPassState(this).frontFace = primitive.frontFace || '';
      wgPassState(this).cullMode = primitive.cullMode || '';
      return orig.apply(this, args);
    });
    ['draw', 'drawIndexed'].forEach(function (name) {
      wrap(proto, name, function (orig, args) {
        var state = wgPassState(this), material = state.groups['1'];
        var record = { kind: name,
          positionBuffer: state.vertexBuffers['0'] || 0,
          materialBuffer: material && material['0'] || 0,
          frontFace: state.frontFace, cullMode: state.cullMode,
          bundle: retained ? -1 : 0 };
        window.__affineWGDrawRecords.push(record);
        if (retained) state.records.push(record);
        return orig.apply(this, args);
      });
    });
  }
  wrapWGDrawEncoder(typeof GPURenderPassEncoder !== 'undefined' ? GPURenderPassEncoder.prototype : null, false);

  var nextWGBundleID = 1;
  var wgBundleIDs = new WeakMap();
  if (typeof GPURenderBundleEncoder !== 'undefined' && GPURenderBundleEncoder.prototype) {
    wrapWGDrawEncoder(GPURenderBundleEncoder.prototype, true);
    wrap(GPURenderBundleEncoder.prototype, 'finish', function (orig, args) {
      var state = wgPassState(this), bundle = orig.apply(this, args);
      var id = nextWGBundleID++;
      wgBundleIDs.set(bundle, id);
      state.records.forEach(function (record) { record.bundle = id; });
      return bundle;
    });
  }
  if (typeof GPURenderPassEncoder !== 'undefined' && GPURenderPassEncoder.prototype) {
    wrap(GPURenderPassEncoder.prototype, 'executeBundles', function (orig, args) {
      var bundles = args[0] || [];
      for (var i = 0; i < bundles.length; i++) {
        var id = wgBundleIDs.get(bundles[i]);
        if (id) window.__affineWGExecutedBundles.push(id);
      }
      return orig.apply(this, args);
    });
  }
  document.addEventListener('gosx:scene3d:input', function (event) {
    window.__affineInputEvents.push(event && event.detail || null);
  });
})();
`;

const CAPS_START_EXPR = '(function(){' +
  'var c=document.createElement("canvas");var gl2=false,gl=null,released=false;' +
  'try{gl=c.getContext("webgl2");gl2=!!gl;' +
  'var lose=gl&&gl.getExtension("WEBGL_lose_context");' +
  'if(lose&&typeof lose.loseContext==="function"){lose.loseContext();released=true;}}' +
  'catch(e){gl2=false;released=false;}' +
  'var snapshot=function(a){var out={};var i=a&&a.info;' +
  'if(!i||typeof i!=="object")return out;' +
  '["vendor","architecture","device","description","subgroupMinSize","subgroupMaxSize"].' +
  'forEach(function(k){var v=i[k];if(typeof v==="string"&&v)out[k]=v;' +
  'else if(typeof v==="number"&&Number.isFinite(v))out[k]=v;});return out;};' +
  'var p=(navigator.gpu&&navigator.gpu.requestAdapter)?' +
  'navigator.gpu.requestAdapter().then(function(a){return {' +
  'webgl2:gl2,webglContextReleased:released,webgpu:!!a,adapterInfo:snapshot(a)};},' +
  'function(){return {webgl2:gl2,webglContextReleased:released,' +
  'webgpu:false,adapterInfo:{}};}):' +
  'Promise.resolve({webgl2:gl2,webglContextReleased:released,webgpu:false,adapterInfo:{},' +
  'reason:"navigator.gpu unavailable"});' +
  'window.__cubicCapsPromise=p;return true;})()';

const CLOCK_EXPR = '(function(){var r=window.__cubicRefs;if(!r||!r.record)return null;' +
  'var rec=r.record;if(!rec.animatedTransforms)return null;' +
  'var e=rec.animatedTransforms.get(' + fixture.CLOCK_NODE_INDEX + ');' +
  'return (e&&e.translation&&typeof e.translation[0]==="number")?e.translation[0]:null;})()';

function refsSetExpr(mount) {
  return '(function(){var m=document.getElementById(' + JSON.stringify(mount) + ');' +
    'if(!m)return false;' +
    'if(m.getAttribute("data-gosx-scene3d-mounted")!=="true")return false;' +
    'var cv=m.querySelector("canvas");var st=m.__gosxScene3DState;' +
    'if(!cv||!st)return false;var rec=null;' +
    'if(Array.isArray(st._modelSkins)){' +
    'for(var i=0;i<st._modelSkins.length;i+=1){' +
    'if(st._modelSkins[i]&&st._modelSkins[i].id==="cubic"){rec=st._modelSkins[i];break;}}}' +
    'if(!rec)return false;' +
    'window.__cubicRefs={mount:m,canvas:cv,state:st,record:rec};return true;})()';
}

function refsCheckExpr(mount) {
  return '(function(){var r=window.__cubicRefs;if(!r)return null;' +
    'var m=document.getElementById(' + JSON.stringify(mount) + ');' +
    'var cv=m&&m.querySelector("canvas");var st=m&&m.__gosxScene3DState;' +
    'var rec=null;var sk=st&&st._modelSkins;' +
    'if(Array.isArray(sk)){for(var i=0;i<sk.length;i+=1){' +
    'if(sk[i]&&sk[i].id==="cubic"){rec=sk[i];break;}}}' +
    'return {sameMount:m===r.mount,sameCanvas:cv===r.canvas,' +
    'sameState:st===r.state,sameRecord:rec===r.record};})()';
}

function attrsExpr(mount) {
  return '(function(){var m=document.getElementById(' + JSON.stringify(mount) + ');' +
    'if(!m)return null;return {' +
    'mounted:m.getAttribute("data-gosx-scene3d-mounted"),' +
    'renderer:m.getAttribute("data-gosx-scene3d-renderer"),' +
    'fallback:m.getAttribute("data-gosx-scene3d-renderer-fallback")};})()';
}

function webGPUDiagnosticsExpr() {
  return '(function(){try{' +
    'var fn=window.__gosx_scene3d_webgpu_diagnostics;' +
    'if(typeof fn!=="function")return null;' +
    'return JSON.parse(JSON.stringify(fn()));' +
    '}catch(e){return {error:String(e&&e.message||e)}}})()';
}

function poseExpr() {
  return '(function(){var r=window.__cubicRefs;if(!r||!r.record||!r.state)return null;' +
    'var rec=r.record,st=r.state;' +
    'if(!rec.animatedTransforms)return null;' +
    'var e0=rec.animatedTransforms.get(' + fixture.CURVE_NODE_INDEX + ');' +
    'var e1=rec.animatedTransforms.get(' + fixture.CLOCK_NODE_INDEX + ');' +
    'var cp=function(v){return v?Array.prototype.slice.call(v):null;};' +
    'var animCount=0;rec.animatedTransforms.forEach(function(){animCount+=1;});' +
    'var mesh=null,meshID=null;' +
    'if(st.objects&&typeof st.objects.forEach==="function"){' +
    'st.objects.forEach(function(o,id){' +
    'var sid=String(id);' +
    'if(!mesh&&o&&o.vertices&&o.vertices.positions&&sid==="cubic/tri-prim-0"){mesh=o;meshID=id;}});}' +
    'return {mixer:!!rec.mixer,wasm:!!rec.wasmMixerActive,' +
    'playing:((typeof rec.animation==="string"&&rec.animation!=="")&&' +
    '!!(rec.mixer&&rec.mixer.isPlaying&&rec.mixer.isPlaying("curve"))),' +
    'animation:(typeof rec.animation==="string")?rec.animation:null,' +
    'animatedCount:animCount,' +
    't0:e0?{translation:cp(e0.translation),rotation:cp(e0.rotation),' +
    'scale:cp(e0.scale),weights:cp(e0.weights)}:null,' +
    't1:e1?{translation:cp(e1.translation)}:null,' +
    'clock:(e1&&e1.translation)?e1.translation[0]:null,' +
    'meshID:meshID,' +
    'verts:(mesh&&mesh.vertices&&mesh.vertices.positions)?cp(mesh.vertices.positions):null,' +
    'draws:window.__cubicGLDraws,gl:window.__cubicGLContext,' +
    'wgPasses:window.__cubicWGPasses,wgSubmits:window.__cubicWGSubmits};})()';
}

function affineRefsSetExpr(mount) {
  return '(function(){var m=document.getElementById(' + JSON.stringify(mount) + ');' +
    'if(!m||m.getAttribute("data-gosx-scene3d-mounted")!=="true")return false;' +
    'var cv=m.querySelector("canvas"),st=m.__gosxScene3DState;' +
    'var object=st&&st.objects&&st.objects.get("affine-group-child");' +
    'if(!cv||!st||!object)return false;' +
    'window.__affineRefs={mount:m,canvas:cv,state:st,object:object};return true;})()';
}

function affinePickExpr() {
  return '(function(){var r=window.__affineRefs;if(!r||!r.canvas)return false;' +
    'window.__affineInputEvents.length=0;var b=r.canvas.getBoundingClientRect();' +
    'var base={bubbles:true,cancelable:true,pointerId:71,pointerType:"mouse",isPrimary:true,' +
      'button:0,clientX:b.left+b.width/2,clientY:b.top+b.height/2};' +
    'r.canvas.dispatchEvent(new PointerEvent("pointerdown",Object.assign({buttons:1},base)));' +
    'document.dispatchEvent(new PointerEvent("pointerup",Object.assign({buttons:0},base)));' +
    'return true;})()';
}

function affineScalePolicyExpr() {
  return '(function(){' +
    'function hit(matrix,origin,dir){var mesh={id:"scaled",kind:"sphere",radius:1,count:1,pickable:true,transforms:new Float32Array(matrix)};' +
      'var h=window.__gosx_scene3d_api.sceneRaycastPickInstancedMeshes({origin:origin,dir:dir},[mesh],0);' +
      'return h&&{distance:h.distance,local:h.localPosition};}' +
    'function uniform(s){return hit([s,0,0,0,0,s,0,0,0,0,s,0,0,0,0,1],{x:0,y:0,z:2*s},{x:0,y:0,z:-1});}' +
    'function shear(s){var n=Math.SQRT1_2,len=s*Math.sqrt(5),x=-s*n,y=3*s*n;' +
      'return {expected:len,hit:hit([-2*s,0,0,0,s,3*s,0,0,0,0,4*s,0,0,0,0,1],{x:2*x,y:2*y,z:0},{x:-x/len,y:-y/len,z:0})};}' +
    'var near=[9e307,-9e307,0,0,9e307,9e307,0,0,0,0,9e307,0,0,0,0,1],inv=new Float64Array(12);' +
    'return {uniformCutoff:uniform(1e6),uniformLarge:uniform(1e9),uniformSmall:uniform(1e-9),shearedLarge:shear(1e9),shearedSmall:shear(1e-9),' +
      'nearMax:{determinant:window.__gosx_scene3d_api.sceneAffineDeterminant(near,0,inv),inverse:Array.from(inv)},' +
      'overflow:window.__gosx_scene3d_api.sceneAffineDeterminant([1e-308,0,0,0,0,1e-308,0,0,0,0,2e-320,0,0,0,0,1],0,new Float64Array(12)),' +
      'singular:window.__gosx_scene3d_api.sceneAffineDeterminant([1,0,0,0,1,0,0,0,0,0,1,0,0,0,0,1],0,new Float64Array(12))};})()';
}

function affineEvidenceExpr() {
  return '(function(){var r=window.__affineRefs;if(!r)return null;' +
    'var local=' + JSON.stringify(AFFINE_LOCAL_POSITIONS) + ',model=' + JSON.stringify(AFFINE_MODEL_MATRIX) + ';' +
    'function close(a,b){if(!a||a.length<b.length)return false;' +
      'for(var i=0;i<b.length;i++)if(!Number.isFinite(a[i])||Math.abs(a[i]-b[i])>0.00002)return false;' +
      'return true;}' +
    'var glUpload=null,glDraw=null,glUploads=window.__affineGLUploads||[];' +
    'for(var i=0;i<glUploads.length;i++)if(close(glUploads[i].values,local)){' +
      'glUpload=glUploads[i];break;}' +
    'var glRecords=window.__affineGLDrawRecords||[];' +
    'for(var j=0;glUpload&&j<glRecords.length;j++){' +
      'var record=glRecords[j],hasMatrix=false;' +
      'for(var k=0;k<record.matrices.length;k++)if(close(record.matrices[k],model)){hasMatrix=true;break;}' +
      'if(record.buffers.indexOf(glUpload.buffer)>=0&&hasMatrix){glDraw=record;break;}}' +
    'var wgPosition=null,wgMaterial=null,wgDraw=null,wgUploads=window.__affineWGUploads||[];' +
    'for(var wi=0;wi<wgUploads.length;wi++){' +
      'var upload=wgUploads[wi];if(!wgPosition&&close(upload.values,local))wgPosition=upload;' +
      'if(!wgMaterial&&upload.values&&upload.values.length>=36&&close(upload.values.slice(20,36),model))wgMaterial=upload;}' +
    'var wgRecords=window.__affineWGDrawRecords||[];' +
    'var executed=window.__affineWGExecutedBundles||[];' +
    'for(var wj=0;wgPosition&&wgMaterial&&wj<wgRecords.length;wj++){' +
      'if(wgRecords[wj].positionBuffer===wgPosition.buffer&&' +
        'wgRecords[wj].materialBuffer===wgMaterial.buffer&&' +
        '(wgRecords[wj].bundle===0||executed.indexOf(wgRecords[wj].bundle)>=0)){' +
          'wgDraw=wgRecords[wj];break;}}' +
    'var picked=null,events=window.__affineInputEvents||[];' +
    'for(var ei=0;ei<events.length;ei++){' +
      'var input=events[ei]&&events[ei].input;' +
      'if(input&&input.targetID==="affine-group-child")picked=input;}' +
    'var snapshot=window.__gosx_scene3d_debug&&window.__gosx_scene3d_debug.inspect(r.mount.id);' +
    'var stats=snapshot&&snapshot.webgpuStats||{},diagnostics=snapshot&&snapshot.rendererDiagnostics||{};' +
    'return {parent:Array.prototype.slice.call(r.object.parentMatrix||[]),' +
      'glUpload:glUpload,glDraw:glDraw,wgPosition:wgPosition,wgMaterial:wgMaterial,wgDraw:wgDraw,' +
      'pick:picked,draws:window.__cubicGLDraws,gl:window.__cubicGLContext,' +
      'wgPasses:window.__cubicWGPasses,wgSubmits:window.__cubicWGSubmits,' +
      'executedBundles:executed.slice(),' +
      'path:{worldMeshVertexCount:snapshot&&snapshot.counts&&snapshot.counts.worldMeshVertexCount,' +
        'retainedMeshObjects:stats.retainedMeshObjects,' +
        'bundleState:stats.bundleState,' +
        'cacheEntries:diagnostics.retainedGeometry&&diagnostics.retainedGeometry.cacheEntries},' +
      'events:events};})()';
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

function telemetryQuiesceExpr() {
  return '(async function(){' +
    'if(typeof window.__gosx_telemetry_flush!=="function"||' +
      'typeof window.__gosx_telemetry_snapshot!=="function")return null;' +
    'window.__gosx_telemetry_flush({drain:true});' +
    'var deadline=Date.now()+10000,last=null;' +
    'for(;;){last=window.__gosx_telemetry_snapshot();' +
      'if(last&&last.queueDepth===0&&last.pendingRequests===0)return last;' +
      'if(Date.now()>deadline)return last;' +
      'await new Promise(function(resolve){setTimeout(resolve,25);});}})()';
}

function diffExpr(a, b) {
  // Decode both PNGs via canvas and compare full RGBA; no size/hash shortcuts.
  return 'new Promise(function(res){var A=new Image(),B=new Image(),n=0;' +
    'function done(){try{if(++n<2)return;' +
    'if(A.width!==B.width||A.height!==B.height){res({dimsMatch:false});return;}' +
    'var c=document.createElement("canvas");c.width=A.width;c.height=A.height;' +
    'var x=c.getContext("2d",{willReadFrequently:true});x.drawImage(A,0,0);' +
    'var d1=x.getImageData(0,0,c.width,c.height).data;' +
    'x.clearRect(0,0,c.width,c.height);x.drawImage(B,0,0);' +
    'var d2=x.getImageData(0,0,c.width,c.height).data;' +
    'var eb=0,ep=0,mp=0,md=0;' +
    'for(var i=0;i<d1.length;i+=4){' +
    'var mx=Math.max(Math.abs(d1[i]-d2[i]),Math.abs(d1[i+1]-d2[i+1]),' +
    'Math.abs(d1[i+2]-d2[i+2]),Math.abs(d1[i+3]-d2[i+3]));' +
    'if(mx>0){if(d1[i]!==d2[i])eb++;if(d1[i+1]!==d2[i+1])eb++;' +
    'if(d1[i+2]!==d2[i+2])eb++;if(d1[i+3]!==d2[i+3])eb++;ep++;if(mx>2)mp++;}' +
    'if(mx>md)md=mx;}' +
    'res({dimsMatch:true,exactBytes:eb,exactPixels:ep,meanChanged:mp,maxDelta:md});' +
    '}catch(e){res(null);}}' +
    'A.onload=B.onload=done;A.onerror=B.onerror=function(){res(null);};' +
    'A.src="data:image/png;base64,' + a + '";B.src="data:image/png;base64,' + b + '";})';
}

function imageStatsExpr(base64) {
  return 'new Promise(function(res){var image=new Image();image.onload=function(){try{' +
    'var c=document.createElement("canvas");c.width=image.width;c.height=image.height;' +
    'var x=c.getContext("2d",{willReadFrequently:true});x.drawImage(image,0,0);' +
    'var data=x.getImageData(0,0,c.width,c.height).data,changed=0,maxDelta=0;' +
    'for(var i=0;i<data.length;i+=4){var d=Math.max(Math.abs(data[i]-16),' +
      'Math.abs(data[i+1]-20),Math.abs(data[i+2]-24),Math.abs(data[i+3]-255));' +
      'if(d>3)changed++;if(d>maxDelta)maxDelta=d;}' +
    'res({width:c.width,height:c.height,nonBackgroundPixels:changed,maxDelta:maxDelta});' +
    '}catch(e){res(null);}};image.onerror=function(){res(null);};' +
    'image.src="data:image/png;base64,' + base64 + '";})';
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

function webGPUCaptureExpr(mount, label) {
  return '(function(){var m=document.getElementById(' + JSON.stringify(mount) + ');' +
    'var cv=m&&m.querySelector("canvas");var r=window.__cubicWGReadback;' +
    'if(!cv||!r||typeof r.capture!=="function")' +
      'return Promise.resolve({error:"WebGPU readback unavailable"});' +
    'return r.capture(cv,' + JSON.stringify(label) + ');})()';
}

async function captureWebGPU(send, mount, label) {
  return evalSend(send, webGPUCaptureExpr(mount, label), { awaitPromise: true });
}

function webGPUArmAndHubExpr(mount, label, detail) {
  return '(function(){var m=document.getElementById(' + JSON.stringify(mount) + ');' +
    'var cv=m&&m.querySelector("canvas");var r=window.__cubicWGReadback;' +
    'var armed=!!(cv&&r&&typeof r.arm==="function"&&r.arm(cv,' +
      JSON.stringify(label) + '));' +
    // The optional busy gap is an adversarial control. Because it remains in
    // this synchronous page task, even a long gap cannot let RAF consume the
    // arm before the public STOP event is dispatched.
    'var until=performance.now()+' + RESTORE_ATOMIC_GAP_MS + ';' +
    'while(performance.now()<until){}' +
    'var dispatched=false;try{document.dispatchEvent(new CustomEvent("gosx:hub:event",' +
      '{detail:' + JSON.stringify(detail) + '}));dispatched=true;}catch(e){}' +
    'return {armed:armed,dispatched:dispatched};})()';
}

function webGPUResultExpr(label) {
  return '(function(){var r=window.__cubicWGReadback;' +
    'return r&&typeof r.result==="function"?r.result(' + JSON.stringify(label) + '):' +
      'Promise.resolve({error:"WebGPU readback unavailable"});})()';
}

function webGPUCompareExpr(a, b) {
  return '(function(){var r=window.__cubicWGReadback;' +
    'return r&&typeof r.compare==="function"?r.compare(' +
      JSON.stringify(a) + ',' + JSON.stringify(b) + '):null;})()';
}

function webGPUProofExpr() {
  return '(function(){var r=window.__cubicWGReadback;' +
    'return r&&typeof r.receipt==="function"?r.receipt():null;})()';
}

function assertWebGPUReadback(summary, label) {
  if (!summary || summary.error) {
    fail(label + ' mapped renderer-target readback failed: ' +
      JSON.stringify(summary && summary.error || summary));
    return;
  }
  if (summary.width !== W || summary.height !== H || summary.byteLength !== W * H * 4) {
    fail(label + ' mapped renderer-target dimensions are wrong: ' + JSON.stringify(summary));
  }
  if (typeof summary.pixelSHA256 !== 'string' || !/^[a-f0-9]{64}$/.test(summary.pixelSHA256)) {
    fail(label + ' mapped renderer-target SHA-256 is invalid: ' +
      JSON.stringify(summary.pixelSHA256));
  }
  if (summary.renderTargetKind !== 'proof-private-gpu-texture' ||
      summary.canvasPresented !== false) {
    fail(label + ' renderer-target kind/presentation receipt is invalid: ' +
      JSON.stringify(summary));
  }
  for (const field of ['contextID', 'deviceID', 'queueID', 'targetID']) {
    if (typeof summary[field] !== 'string' || !summary[field]) {
      fail(label + ' renderer-target ' + field + ' is missing');
    }
  }
  for (const field of ['targetGeneration', 'configureSequence',
    'getCurrentTextureSequence', 'targetViewSequence', 'renderPassSequence',
    'commandBufferSequence', 'productSubmitSequence', 'proofCopySequence']) {
    if (!Number.isInteger(summary[field]) || summary[field] <= 0) {
      fail(label + ' renderer-target ' + field + ' is invalid: ' + summary[field]);
    }
  }
  if ((summary.targetUsage & 0x01) !== 0x01 || (summary.targetUsage & 0x10) !== 0x10) {
    fail(label + ' renderer-target usage lacks COPY_SRC|RENDER_ATTACHMENT: ' +
      summary.targetUsage);
  }
  if (summary.returnedToProduct !== true || summary.productRenderPassLinked !== true ||
      summary.productCommandBufferLinked !== true || summary.productQueueMatched !== true) {
    fail(label + ' renderer-target product linkage is invalid: ' + JSON.stringify(summary));
  }
  if (JSON.stringify(summary.proofCommandKinds) !== JSON.stringify(['copyTextureToBuffer'])) {
    fail(label + ' proof commands were not copy-only: ' +
      JSON.stringify(summary.proofCommandKinds));
  }
  if (summary.nativeConfigureCalls !== 0 || summary.nativeGetCurrentTextureCalls !== 0) {
    fail(label + ' native WebGPU canvas calls were observed: ' + JSON.stringify(summary));
  }
  if (summary.productSubmissionForwarded !== true) {
    fail(label + ' product queue submission was not forwarded');
  }
  if (!(summary.foregroundPixels > 20)) {
    fail(label + ' mapped renderer target contains ' + summary.foregroundPixels +
      ' non-background pixels, expected > 20');
  }
}

function writeArtifact(name, base64) {
  try {
    fs.writeFileSync(path.join(ART, name), Buffer.from(base64, 'base64'));
  } catch (e) {
    fail('artifact write failed for ' + name + ': ' + e.message);
  }
}

function assertClose(actual, expected, label, tol) {
  const t = tol == null ? 1e-3 : tol;
  if (!Array.isArray(actual) || actual.length !== expected.length) {
    fail(label + ': missing or wrong length (got ' + JSON.stringify(actual) + ')');
    return;
  }
  for (let i = 0; i < expected.length; i += 1) {
    const v = Number(actual[i]);
    if (!Number.isFinite(v) || Math.abs(v - expected[i]) >= t) {
      fail(label + '[' + i + ']=' + actual[i] + ' want ' + expected[i] + ' (tol ' + t + ')');
    }
  }
}

function assertRelative(actual, expected, label, tolerance) {
  const value = Number(actual);
  if (!Number.isFinite(value) || Math.abs(value - expected) > Math.abs(expected) * tolerance) {
    fail(label + '=' + actual + ' want ' + expected + ' (relative tolerance ' + tolerance + ')');
  }
}

function transformAffinePositions(local, matrix) {
  const out = [];
  for (let i = 0; i + 2 < local.length; i += 3) {
    const x = local[i];
    const y = local[i + 1];
    const z = local[i + 2];
    out.push(
      matrix[0] * x + matrix[4] * y + matrix[8] * z + matrix[12],
      matrix[1] * x + matrix[5] * y + matrix[9] * z + matrix[13],
      matrix[2] * x + matrix[6] * y + matrix[10] * z + matrix[14],
    );
  }
  return out;
}

async function pollClock(send, label) {
  const deadline = Date.now() + CLOCK_TIMEOUT_MS;
  let last = null;
  for (;;) {
    last = await evalSend(send, CLOCK_EXPR);
    if (typeof last === 'number' && Number.isFinite(last) && last >= CLOCK_MIN) return last;
    if (Date.now() > deadline) {
      throw new Error('[' + label + '] clock never reached ' + CLOCK_MIN +
        's within ' + CLOCK_TIMEOUT_MS + 'ms (last=' + last + ')');
    }
    await sleep(100);
  }
}

async function waitForNetworkIdle(label) {
  const deadline = Date.now() + 10000;
  let idleSince = 0;
  for (;;) {
    if (networkRequests.size === 0) {
      if (idleSince === 0) idleSince = Date.now();
      if (Date.now() - idleSince >= 100) return;
    } else {
      idleSince = 0;
    }
    if (Date.now() > deadline) {
      fail('[' + label + '] network did not become idle: ' +
        JSON.stringify(Array.from(networkRequests.values())));
      return;
    }
    await sleep(25);
  }
}

function compareSampledPose(pose, t, label) {
  assertClose(pose.t0.translation, fixture.evalTranslation(t), label + ' sampled translation', 2e-3);
  assertClose(pose.t0.rotation, fixture.evalRotation(t), label + ' sampled rotation', 2e-3);
  assertClose(pose.t0.scale, fixture.evalScale(t), label + ' sampled scale', 2e-3);
  assertClose(pose.t0.weights, fixture.evalWeights(t), label + ' sampled weights', 2e-3);
}

async function runAffineCase(send, c, sessionId) {
  const ev = { name: c.name, kind: 'affine-renderer-owned', webgpu: c.webgpu,
    mount: c.mount, engine: c.engine };
  const loadP = waitForEvent('Page.loadEventFired', MOUNT_WAIT_MS, sessionId);
  await send('Page.navigate', { url: BASE + '/case/' + c.name });
  await loadP;

  const deadline = Date.now() + MOUNT_WAIT_MS;
  let ready = false;
  while (Date.now() < deadline) {
    if ((await evalSend(send, affineRefsSetExpr(c.mount))) === true) { ready = true; break; }
    await sleep(100);
  }
  if (!ready) {
    fail('[' + c.name + '] affine-only engine did not mount');
    return ev;
  }

  ev.attrs = await evalSend(send, attrsExpr(c.mount));
  const wantRenderer = c.webgpu ? 'webgpu' : 'webgl';
  if (!ev.attrs || ev.attrs.mounted !== 'true' || ev.attrs.renderer !== wantRenderer) {
    fail('[' + c.name + '] wrong backend attributes: ' + JSON.stringify(ev.attrs) +
      ' (want mounted=true, renderer=' + wantRenderer + ')');
  }
  if (ev.attrs && ev.attrs.fallback) {
    fail('[' + c.name + '] fallback attribute set: ' + ev.attrs.fallback);
  }

  await settleFrames(send, 10);
  if ((await evalSend(send, affinePickExpr())) !== true) {
    fail('[' + c.name + '] real canvas pointer pick dispatch failed');
  }
  await settleFrames(send, 2);

  const evidence = await evalSend(send, affineEvidenceExpr());
  ev.affine = evidence;
  ev.scalePolicy = await evalSend(send, affineScalePolicyExpr());
  if (!evidence) {
    fail('[' + c.name + '] affine GPU/pick evidence unavailable');
  } else {
    assertClose(evidence.parent, AFFINE_PARENT, '[' + c.name + '] strict parent matrix', 1e-6);
    if (!evidence.path || evidence.path.worldMeshVertexCount !== 0 || !(evidence.path.cacheEntries > 0) ||
        (c.webgpu && evidence.path.retainedMeshObjects !== 1)) {
      fail('[' + c.name + '] affine object fell off retained renderer path: ' +
        JSON.stringify(evidence.path));
    }
    if (!c.webgpu) {
      if (!evidence.glUpload || !evidence.glDraw) {
        fail('[' + c.name + '] no WebGL draw joined retained local vertices to affine model matrix: ' +
          JSON.stringify(evidence));
      } else {
        assertClose(evidence.glUpload.values, AFFINE_LOCAL_POSITIONS,
          '[' + c.name + '] submitted retained local vertices', 1e-6);
        let submittedModel = null;
        for (const candidate of evidence.glDraw.matrices || []) {
          if (Array.isArray(candidate) && candidate.length === 16 &&
              candidate.every((value, i) => Math.abs(value - AFFINE_MODEL_MATRIX[i]) < 2e-5)) {
            submittedModel = candidate;
            break;
          }
        }
        assertClose(submittedModel, AFFINE_MODEL_MATRIX,
          '[' + c.name + '] submitted affine model matrix', 2e-5);
        assertClose(transformAffinePositions(evidence.glUpload.values, submittedModel || AFFINE_MODEL_MATRIX),
          AFFINE_WORLD_POSITIONS, '[' + c.name + '] independently reconstructed rendered vertices', 2e-5);
        if (evidence.glDraw.kind !== 'drawElements') {
          fail('[' + c.name + '] retained indexed affine object did not use drawElements: ' +
            JSON.stringify(evidence.glDraw.kind));
        }
        if (evidence.glDraw.frontFace !== 2304) {
          fail('[' + c.name + '] reflected WebGL draw did not select CW front face: ' +
            JSON.stringify(evidence.glDraw.frontFace));
        }
      }
      if (!(evidence.draws > 0) || evidence.gl !== 'webgl2') {
        fail('[' + c.name + '] native WebGL2 draw evidence missing: ' + JSON.stringify(evidence));
      }
    } else {
      if (!evidence.wgPosition || !evidence.wgMaterial || !evidence.wgDraw) {
        fail('[' + c.name + '] no WebGPU draw joined retained local vertices to affine material: ' +
          JSON.stringify(evidence));
      } else {
        const submittedModel = evidence.wgMaterial.values.slice(20, 36);
        assertClose(evidence.wgPosition.values, AFFINE_LOCAL_POSITIONS,
          '[' + c.name + '] submitted retained local vertices', 1e-6);
        assertClose(submittedModel, AFFINE_MODEL_MATRIX,
          '[' + c.name + '] submitted affine model matrix', 2e-5);
        assertClose(transformAffinePositions(evidence.wgPosition.values, submittedModel),
          AFFINE_WORLD_POSITIONS, '[' + c.name + '] independently reconstructed rendered vertices', 2e-5);
        if (evidence.wgDraw.kind !== 'drawIndexed') {
          fail('[' + c.name + '] retained indexed affine object did not use drawIndexed: ' +
            JSON.stringify(evidence.wgDraw.kind));
        }
        if (evidence.wgDraw.frontFace !== 'cw') {
          fail('[' + c.name + '] reflected WebGPU draw did not select CW front face: ' +
            JSON.stringify(evidence.wgDraw.frontFace));
        }
        if (!(evidence.wgDraw.bundle > 0) ||
            !Array.isArray(evidence.executedBundles) ||
            evidence.executedBundles.indexOf(evidence.wgDraw.bundle) < 0) {
          fail('[' + c.name + '] retained affine draw bundle was not executed by the render pass: ' +
            JSON.stringify({ draw: evidence.wgDraw, executed: evidence.executedBundles }));
        }
      }
      if (!(evidence.wgPasses > 0) || !(evidence.wgSubmits > 0)) {
        fail('[' + c.name + '] native WebGPU pass/submit evidence missing: ' + JSON.stringify(evidence));
      }
    }
    const pick = evidence.pick;
    if (!pick || pick.targetID !== 'affine-group-child') {
      fail('[' + c.name + '] actual canvas pick missed affine child: ' + JSON.stringify(pick));
    } else {
      assertClose([pick.worldX, pick.worldY, pick.worldZ], [0.5, 0.5, 1],
        '[' + c.name + '] actual affine pick point', 2e-4);
      if (Math.abs(Number(pick.depth) - 2) >= 2e-4) {
        fail('[' + c.name + '] actual affine pick distance=' + pick.depth + ' want 2');
      }
    }
  }
  const scalePolicy = ev.scalePolicy;
  if (!scalePolicy || !scalePolicy.uniformCutoff || !scalePolicy.uniformLarge || !scalePolicy.uniformSmall ||
      !scalePolicy.shearedLarge || !scalePolicy.shearedLarge.hit ||
      !scalePolicy.shearedSmall || !scalePolicy.shearedSmall.hit) {
    fail('[' + c.name + '] affine scale-policy proof missing: ' + JSON.stringify(scalePolicy));
  } else {
    assertRelative(scalePolicy.uniformCutoff.distance, 1e6,
      '[' + c.name + '] exact former-cutoff instanced pick distance', 1e-12);
    assertRelative(scalePolicy.uniformLarge.distance, 1e9,
      '[' + c.name + '] exact 1e9 instanced pick distance', 1e-12);
    assertRelative(scalePolicy.uniformSmall.distance, 1e-9,
      '[' + c.name + '] small instanced pick distance', 1e-6);
    assertRelative(scalePolicy.shearedLarge.hit.distance, scalePolicy.shearedLarge.expected,
      '[' + c.name + '] large sheared/reflected pick distance', 1e-6);
    assertRelative(scalePolicy.shearedSmall.hit.distance, scalePolicy.shearedSmall.expected,
      '[' + c.name + '] small sheared/reflected pick distance', 1e-6);
    const nearInverse = scalePolicy.nearMax && scalePolicy.nearMax.inverse;
    if (!scalePolicy.nearMax || scalePolicy.nearMax.determinant !== 2 ||
        !Array.isArray(nearInverse) || !nearInverse.every(Number.isFinite) ||
        !nearInverse.some((value) => value !== 0)) {
      fail('[' + c.name + '] near-max affine inverse invalid: ' + JSON.stringify(scalePolicy.nearMax));
    }
    if (scalePolicy.overflow !== 0 || scalePolicy.singular !== 0) {
      fail('[' + c.name + '] invalid affine inverse did not fail closed: ' +
        JSON.stringify({ overflow: scalePolicy.overflow, singular: scalePolicy.singular }));
    }
  }

  const shot = await capture(send, c.mount);
  writeArtifact(c.name + '-visible.png', shot);
  ev.pixelStats = await evalSend(send, imageStatsExpr(shot), { awaitPromise: true });
  if (!ev.pixelStats || !(ev.pixelStats.nonBackgroundPixels > 20)) {
    fail('[' + c.name + '] reflected/sheared face was not visibly rasterized: ' +
      JSON.stringify(ev.pixelStats));
  }

  ev.disposed = await evalSend(send, disposeExpr(c.engine, c.mount));
  if (ev.disposed !== true) fail('[' + c.name + '] disposal did not clear engine state');
  ev.telemetry = await evalSend(send, telemetryQuiesceExpr(), { awaitPromise: true });
  if (ev.telemetry && (ev.telemetry.queueDepth !== 0 || ev.telemetry.pendingRequests !== 0)) {
    fail('[' + c.name + '] telemetry did not quiesce before navigation: ' +
      JSON.stringify(ev.telemetry));
  }
  await waitForNetworkIdle(c.name);
  return ev;
}

async function runCase(send, c, sessionId) {
  const ev = { name: c.name, webgpu: c.webgpu, mount: c.mount, engine: c.engine };

  const loadP = waitForEvent('Page.loadEventFired', MOUNT_WAIT_MS, sessionId);
  await send('Page.navigate', { url: BASE + '/case/' + c.name });
  await loadP;

  const deadline = Date.now() + MOUNT_WAIT_MS;
  let ready = false;
  while (Date.now() < deadline) {
    if ((await evalSend(send, refsSetExpr(c.mount))) === true) { ready = true; break; }
    await sleep(100);
  }
  if (!ready) {
    fail('[' + c.name + '] engine did not mount a cubic model record');
    return ev;
  }

  ev.attrs = await evalSend(send, attrsExpr(c.mount));
  const wantRenderer = c.webgpu ? 'webgpu' : 'webgl';
  if (!ev.attrs || ev.attrs.mounted !== 'true' || ev.attrs.renderer !== wantRenderer) {
    fail('[' + c.name + '] wrong backend attributes: ' + JSON.stringify(ev.attrs) +
      ' (want mounted=true, renderer=' + wantRenderer + ')');
  }
  if (ev.attrs && ev.attrs.fallback) {
    fail('[' + c.name + '] fallback attribute set: ' + ev.attrs.fallback);
  }

  await settleFrames(send, 10);

  // Load-time checks: JS mixer, all five channels, first-clamp authored pose.
  const load = await evalSend(send, poseExpr());
  ev.load = load;
  if (!load) {
    fail('[' + c.name + '] load pose read failed');
    return ev;
  }
  if (!load.mixer || load.wasm) {
    fail('[' + c.name + '] model record must use the JS mixer, not WASM');
  }
  if (load.meshID !== 'cubic/tri-prim-0') {
    fail('[' + c.name + '] observed object must be exactly cubic/tri-prim-0, got ' +
      JSON.stringify(load.meshID));
  }
  if (!load.t1 || !load.t1.translation) {
    fail('[' + c.name + '] clock LINEAR channel missing at first clamp');
  } else {
    assertClose(load.t1.translation, [0, 0, 0], '[' + c.name + '] first-clamp clock translation', 1e-6);
  }
  if (!load.t0 || !load.t0.translation || !load.t0.rotation || !load.t0.scale || !load.t0.weights) {
    fail('[' + c.name + '] CUBICSPLINE channels missing at first clamp: ' + JSON.stringify(load.t0));
  } else {
    assertClose(load.t0.translation, [0, 0, 0], '[' + c.name + '] first-clamp translation', 1e-6);
    assertClose(load.t0.rotation, [0, 0, 0, 1], '[' + c.name + '] first-clamp rotation', 1e-6);
    assertClose(load.t0.scale, [1, 1, 1], '[' + c.name + '] first-clamp scale', 1e-6);
    assertClose(load.t0.weights, [0, 0], '[' + c.name + '] first-clamp weights', 1e-6);
  }
  assertClose(load.verts, fixture.BASE_POSITIONS, '[' + c.name + '] authored vertices at load', 1e-6);
  ev.meshID = load.meshID;
  if (c.webgpu) {
    ev.webgpuDiagnostics = await evalSend(send, webGPUDiagnosticsExpr());
  }

  let baseline = null;
  if (c.webgpu) {
    ev.baselineReadback = await captureWebGPU(send, c.mount, c.name + '-baseline');
    assertWebGPUReadback(ev.baselineReadback, '[' + c.name + '] baseline');
  } else {
    baseline = await capture(send, c.mount);
    writeArtifact(c.name + '-baseline.png', baseline);
  }

  // Playback starts ONLY through the public hub event.
  if ((await evalSend(send, hubExpr(START_DETAIL))) !== true) {
    fail('[' + c.name + '] hub start event dispatch failed');
  }

  const observed = await pollClock(send, c.name);
  ev.observedClock = observed;
  if (observed < CLOCK_MIN || observed > CLOCK_MAX) {
    fail('[' + c.name + '] observed clock outside [' + CLOCK_MIN + ',' + CLOCK_MAX + ']: ' + observed);
  }

  // Freeze via the public speed:0 event; the clock must stop for real frames.
  if ((await evalSend(send, hubExpr(FREEZE_DETAIL))) !== true) {
    fail('[' + c.name + '] hub freeze event dispatch failed');
  }
  await settleFrames(send, 6);
  const frozenA = await evalSend(send, poseExpr());
  await settleFrames(send, 8);
  const frozenB = await evalSend(send, poseExpr());
  if (!frozenA || !frozenB) {
    fail('[' + c.name + '] frozen pose read failed');
    return ev;
  }
  ev.frozenClock = frozenA.clock;
  ev.frozen = frozenA;
  if (frozenA.clock !== frozenB.clock) {
    fail('[' + c.name + '] speed 0 did not freeze the clock: ' +
      frozenA.clock + ' -> ' + frozenB.clock);
  }
  if (!(frozenA.clock >= CLOCK_MIN && frozenA.clock <= CLOCK_MAX)) {
    fail('[' + c.name + '] frozen clock outside [' + CLOCK_MIN + ',' + CLOCK_MAX + ']: ' + frozenA.clock);
  }

  // Sampled properties vs the analytic oracle at the observed clock, and a
  // hard check that the pose is NOT linear-endpoint interpolation.
  if (!frozenA.t0 || !frozenA.t0.translation || !frozenA.t0.rotation ||
    !frozenA.t0.scale || !frozenA.t0.weights) {
    fail('[' + c.name + '] sampled channels missing while frozen: ' + JSON.stringify(frozenA.t0));
  } else {
    compareSampledPose(frozenA, frozenA.clock, '[' + c.name + ']');
    const t = frozenA.clock;
    const linT = fixture.linearEndpointTranslation(t)[0];
    if (Math.abs(frozenA.t0.translation[0] - linT) < 0.05) {
      fail('[' + c.name + '] sampled translation matches linear-endpoint interpolation; ' +
        'CUBICSPLINE not exercised');
    }
    const linW = fixture.linearEndpointWeights(t)[0];
    if (Math.abs(frozenA.t0.weights[0] - linW) < 0.05) {
      fail('[' + c.name + '] sampled weights match linear-endpoint interpolation; ' +
        'CUBICSPLINE not exercised');
    }
  }
  if (!frozenA.verts) {
    fail('[' + c.name + '] emitted vertices unreadable while frozen');
  } else {
    assertClose(frozenA.verts, fixture.expectedWorldPositions(frozenA.clock),
      '[' + c.name + '] emitted vertices vs analytic oracle', 2e-3);
  }

  if (!c.webgpu) {
    if (!(frozenA.draws > load.draws)) {
      fail('[' + c.name + '] WebGL draw counter did not advance past baseline: ' +
        load.draws + ' -> ' + frozenA.draws);
    }
    if (frozenA.gl !== 'webgl2') {
      fail('[' + c.name + '] observed GL context is not native WebGL2: ' + frozenA.gl);
    }
  } else {
    if (!(frozenA.wgPasses > load.wgPasses)) {
      fail('[' + c.name + '] WebGPU render-pass counter did not advance past baseline: ' +
        load.wgPasses + ' -> ' + frozenA.wgPasses);
    }
    if (!(frozenA.wgSubmits > load.wgSubmits)) {
      fail('[' + c.name + '] WebGPU submission counter did not advance past baseline: ' +
        load.wgSubmits + ' -> ' + frozenA.wgSubmits);
    }
  }

  let playDiff = null;
  if (c.webgpu) {
    ev.playingReadback = await captureWebGPU(send, c.mount, c.name + '-playing');
    assertWebGPUReadback(ev.playingReadback, '[' + c.name + '] playing');
    playDiff = await evalSend(send,
      webGPUCompareExpr(c.name + '-baseline', c.name + '-playing'));
  } else {
    const playing = await capture(send, c.mount);
    writeArtifact(c.name + '-playing.png', playing);
    playDiff = await evalSend(send, diffExpr(baseline, playing), { awaitPromise: true });
  }
  ev.playDiff = playDiff;
  if (!playDiff || !playDiff.dimsMatch) {
    fail('[' + c.name + '] playing frame not comparable: ' + JSON.stringify(playDiff));
  } else if (!(playDiff.exactPixels > 20)) {
    fail('[' + c.name + '] playing frame changed ' + playDiff.exactPixels +
      ' pixels, expected > 20');
  }

  const idPlay = await evalSend(send, refsCheckExpr(c.mount));
  ev.identityAfterPlayback = idPlay;
  if (!idPlay || !idPlay.sameMount || !idPlay.sameCanvas || !idPlay.sameState || !idPlay.sameRecord) {
    fail('[' + c.name + '] mount/canvas/state/record identity changed during playback: ' +
      JSON.stringify(idPlay));
  }

  // Public stop with zero fades restores the authored pose on the SAME scene.
  // Arm the WebGPU copy and dispatch stop synchronously in one page task.
  // Once the restored frame is submitted the idle scene may stop rendering,
  // while separate CDP evaluations would leave a RAF-sized interception gap.
  let restoreReadbackArmed = true;
  let stopDispatched = false;
  if (c.webgpu) {
    const armAndStop = await evalSend(send,
      webGPUArmAndHubExpr(c.mount, c.name + '-restored', STOP_DETAIL));
    restoreReadbackArmed = !!(armAndStop && armAndStop.armed);
    stopDispatched = !!(armAndStop && armAndStop.dispatched);
    if (restoreReadbackArmed !== true) {
      fail('[' + c.name + '] restored mapped renderer-target readback could not be armed');
    }
  } else {
    stopDispatched = (await evalSend(send, hubExpr(STOP_DETAIL))) === true;
  }
  if (!stopDispatched) {
    fail('[' + c.name + '] hub stop event dispatch failed');
  }
  await settleFrames(send, 12);
  const restored = await evalSend(send, poseExpr());
  ev.restored = restored;
  if (!restored) {
    fail('[' + c.name + '] restored pose read failed');
    return ev;
  }
  if (restored.playing !== false || restored.animation) {
    fail('[' + c.name + '] animation still playing after stop: ' +
      JSON.stringify(restored.animation));
  }
  if (restored.animatedCount !== 0) {
    fail('[' + c.name + '] animatedTransforms not empty after stop: ' + restored.animatedCount);
  }
  if (restored.t0 !== null || restored.t1 !== null) {
    fail('[' + c.name + '] sampled entries must be absent after stop (authored pose is ' +
      'restored by node traversal, not sampled TRS): t0=' + JSON.stringify(restored.t0) +
      ' t1=' + JSON.stringify(restored.t1));
  }
  assertClose(restored.verts, fixture.BASE_POSITIONS, '[' + c.name + '] restored authored vertices', 1e-6);

  let restoreDiff = null;
  if (c.webgpu) {
    ev.restoredReadback = restoreReadbackArmed === true
      ? await evalSend(send, webGPUResultExpr(c.name + '-restored'), { awaitPromise: true })
      : { error: 'readback was not armed' };
    assertWebGPUReadback(ev.restoredReadback, '[' + c.name + '] restored');
    restoreDiff = await evalSend(send,
      webGPUCompareExpr(c.name + '-baseline', c.name + '-restored'));
  } else {
    const restoredShot = await capture(send, c.mount);
    writeArtifact(c.name + '-restored.png', restoredShot);
    restoreDiff = await evalSend(send, diffExpr(baseline, restoredShot), { awaitPromise: true });
  }
  ev.restoreDiff = restoreDiff;
  if (!restoreDiff || !restoreDiff.dimsMatch) {
    fail('[' + c.name + '] restored frame not comparable: ' + JSON.stringify(restoreDiff));
  } else if (restoreDiff.exactPixels !== 0) {
    fail('[' + c.name + '] restore did not reproduce baseline pixels exactly: ' +
      JSON.stringify(restoreDiff));
  }

  const idRestore = await evalSend(send, refsCheckExpr(c.mount));
  ev.identityAfterRestore = idRestore;
  if (!idRestore || !idRestore.sameMount || !idRestore.sameCanvas ||
    !idRestore.sameState || !idRestore.sameRecord) {
    fail('[' + c.name + '] mount/canvas/state/record identity changed after restore: ' +
      JSON.stringify(idRestore));
  }

  if (c.webgpu) {
    ev.webgpuProof = await evalSend(send, webGPUProofExpr());
    if (!ev.webgpuProof || ev.webgpuProof.proofTarget !== 'private-texture' ||
        ev.webgpuProof.renderTargetKind !== 'proof-private-gpu-texture' ||
        ev.webgpuProof.canvasPresented !== false ||
        ev.webgpuProof.nativeConfigureCalls !== 0 ||
        ev.webgpuProof.nativeGetCurrentTextureCalls !== 0 ||
        !Array.isArray(ev.webgpuProof.failures) || ev.webgpuProof.failures.length !== 0) {
      fail('[' + c.name + '] private renderer-target proof receipt is invalid: ' +
        JSON.stringify(ev.webgpuProof));
    }
    // Give Chrome's stderr pipe a bounded chance to flush all renderer work,
    // then mark the exact boundary before intentional product/target teardown.
    await sleep(100);
    webGPUIntentionalTeardownStderrByte = chromeStderrBytes;
  }

  const disposed = await evalSend(send, disposeExpr(c.engine, c.mount));
  ev.disposed = disposed;
  if (disposed !== true) fail('[' + c.name + '] disposal did not clear engine state');

  // Drain runtime telemetry before the next navigation. That leaves pagehide
  // with an empty queue, so every observed request can complete instead of a
  // keepalive beacon being canceled by teardown.
  ev.telemetry = await evalSend(send, telemetryQuiesceExpr(), { awaitPromise: true });
  if (ev.telemetry && (ev.telemetry.queueDepth !== 0 || ev.telemetry.pendingRequests !== 0)) {
    fail('[' + c.name + '] telemetry did not quiesce before navigation: ' +
      JSON.stringify(ev.telemetry));
  }
  await waitForNetworkIdle(c.name);

  return ev;
}

// ---- Owned resources, report, watchdog, and central cleanup ----
const CASE_EVIDENCE = [];
let BASE = '';
let SELECTED_BROWSER = null;
let BROWSER_GPU_INFO = null;
let CAPABILITY_ADAPTER_INFO = null;
let CAPABILITY_WEBGL_CONTEXT_RELEASED = null;
let CAPABILITY_STDERR_RANGE = null;
const CASE_STDERR_RANGES = [];
let finished = false;
let exitCode = 0;
let reportWriteFailed = false;

function countDiagnostic(content, needle) {
  let count = 0;
  let offset = 0;
  for (;;) {
    const found = content.indexOf(needle, offset);
    if (found < 0) return count;
    count += 1;
    offset = found + Math.max(1, needle.length);
  }
}

function scanChromeDiagnostics() {
  const stderrPath = path.join(ART, 'chrome-stderr.log');
  try {
    const raw = fs.readFileSync(stderrPath);
    const all = raw.toString('utf8').toLowerCase();
    const boundary = Number.isInteger(webGPUIntentionalTeardownStderrByte)
      ? Math.max(0, Math.min(raw.length, webGPUIntentionalTeardownStderrByte))
      : raw.length;
    const beforeTeardown = raw.subarray(0, boundary).toString('utf8').toLowerCase();
    const swapFindings = CHROME_SWAP_DIAGNOSTICS.map((needle) => ({
      needle, count: countDiagnostic(all, needle),
    })).filter((entry) => entry.count > 0);
    const preTeardownLifecycleFindings =
      CHROME_PRE_TEARDOWN_LIFECYCLE_DIAGNOSTICS.map((needle) => ({
        needle, count: countDiagnostic(beforeTeardown, needle),
      })).filter((entry) => entry.count > 0);
    return {
      scannedBytes: raw.length,
      webgpuIntentionalTeardownStderrByte: boundary,
      swapFindings,
      preTeardownLifecycleFindings,
      scanError: '',
    };
  } catch (error) {
    return {
      scannedBytes: null,
      webgpuIntentionalTeardownStderrByte,
      swapFindings: [],
      preTeardownLifecycleFindings: [],
      scanError: String(error && error.message || error),
    };
  }
}

function writeReport(extra) {
  const report = Object.assign({
    errors, warnings, notFound, unexpectedRequests, networkFailures, intentionalNoContent,
    clientEventResponses,
    sourceCommit: SOURCE_COMMIT,
    selectedBrowser: SELECTED_BROWSER,
    webgpuProofTarget: PROOF_TARGET,
    renderTargetKind: 'proof-private-gpu-texture',
    canvasPresented: false,
    chromeLaunch: {
      browserMode: 'headless',
      display: null,
      waylandDisplay: null,
      windowFlags: CHROME_WINDOW_FLAGS.slice(),
      gpuFlags: CHROME_GPU_FLAGS.slice(),
      stderrFile: 'chrome-stderr.log',
      stderrBytes: chromeStderrBytes,
      stderrWriteError: chromeStderrWriteError,
    },
    chromeDiagnostics: chromeDiagnostics || scanChromeDiagnostics(),
    browserGPU: BROWSER_GPU_INFO,
    capabilityAdapterInfo: CAPABILITY_ADAPTER_INFO,
    capabilityWebGLContextReleased: CAPABILITY_WEBGL_CONTEXT_RELEASED,
    capabilityStderrRange: CAPABILITY_STDERR_RANGE,
    caseStderrRanges: CASE_STDERR_RANGES,
    nativeCaps: global.__caps || null, mutation: MUTATION || null,
    restoreAtomicGapMS: RESTORE_ATOMIC_GAP_MS, cases: CASE_EVIDENCE,
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
    networkRequests.clear();
    for (const l of listeners) clearTimeout(l.timer);
    listeners.length = 0;
    try { if (ws) ws.close(); } catch (e) {}
    if (chrome) {
      if (!chromeClosed) {
        const closed = new Promise((res) => chrome.once('close', res));
        try { chrome.kill('SIGKILL'); } catch (e) {}
        await Promise.race([closed, sleep(5000)]);
      }
    }
    if (chromeStderrFD !== null) {
      try { fs.closeSync(chromeStderrFD); }
      catch (e) {
        if (!chromeStderrWriteError) chromeStderrWriteError = e.message;
      }
      chromeStderrFD = null;
    }
    if (profile) {
      let cleanupError = null;
      for (let attempt = 0; attempt < 5; attempt += 1) {
        try {
          fs.rmSync(profile, { recursive: true, force: true });
          cleanupError = null;
          break;
        } catch (e) {
          cleanupError = e;
          await sleep(50 * (attempt + 1));
        }
      }
      if (cleanupError) warnings.push('profile cleanup skipped: ' + cleanupError.message);
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
  await new Promise((res, rej) => {
    server.once('error', rej);
    server.listen(0, '127.0.0.1', () => res());
  });
  BASE = 'http://127.0.0.1:' + server.address().port;

  profile = fs.mkdtempSync(path.join(os.tmpdir(), 'gosx-cubic-probe-'));
  const chromeBin = process.env.GOSX_CHROME_BIN || '/usr/bin/google-chrome';
  if (!fs.existsSync(chromeBin)) throw new Error('chrome binary not found: ' + chromeBin);
  chromeStderrFD = fs.openSync(path.join(ART, 'chrome-stderr.log'), 'w');
  chrome = spawn(chromeBin, [
    ...CHROME_WINDOW_FLAGS, '--no-sandbox', ...CHROME_GPU_FLAGS,
    '--disable-dev-shm-usage', '--user-data-dir=' + profile,
    '--remote-debugging-port=0', 'about:blank',
  ], { stdio: ['ignore', 'ignore', 'pipe'] });
  chrome.once('close', () => { chromeClosed = true; });

  const wsUrl = await new Promise((resolve, reject) => {
    let buf = '';
    let settled = false;
    const t = setTimeout(() => reject(new Error('no DevTools ws URL')), 20000);
    const onExit = () => {
      if (settled) return;
      settled = true;
      clearTimeout(t);
      reject(new Error('chrome exited early: ' + buf));
    };
    const onErr = (e) => {
      if (settled) return;
      settled = true;
      clearTimeout(t);
      reject(new Error('chrome spawn error: ' + e.message));
    };
    chrome.stderr.on('data', (d) => {
      const chunk = Buffer.isBuffer(d) ? d : Buffer.from(d);
      if (chromeStderrFD !== null) {
        try {
          fs.writeSync(chromeStderrFD, chunk);
          chromeStderrBytes += chunk.length;
        } catch (e) {
          if (!chromeStderrWriteError) {
            chromeStderrWriteError = e.message;
            fail('Chrome stderr artifact write failed: ' + e.message);
          }
        }
      }
      if (settled) return;
      buf += chunk.toString();
      const m = buf.match(/ws:\/\/127\.0\.0\.1:\d+\/devtools\/browser\/[^\s]+/);
      if (m) {
        settled = true;
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

  // Release-pin the proof to the browser that actually owns the CDP session.
  // The four-mode matrix cross-checks this against the one selected binary.
  SELECTED_BROWSER = await cdpSend('Browser.getVersion', null, null, STEP_MS);
  if (!SELECTED_BROWSER || typeof SELECTED_BROWSER.product !== 'string') {
    throw new Error('Browser.getVersion did not return a product/version');
  }
  console.log('selected browser: ' + SELECTED_BROWSER.product +
    ' (protocol ' + SELECTED_BROWSER.protocolVersion + ', revision ' +
    SELECTED_BROWSER.revision + ')');
  const systemInfo = await cdpSend('SystemInfo.getInfo', null, null, STEP_MS);
  BROWSER_GPU_INFO = systemInfo && systemInfo.gpu || null;

  // Native capability gate on a real served loopback origin: BOTH WebGL2 and
  // WebGPU are required. No fallback, no skips. This target is deliberately
  // separate from the proof targets: closing its throwaway WebGL context and
  // adapter must not contaminate either renderer case's diagnostics.
  const capabilityStderrStart = chromeStderrBytes;
  const { targetId: capsTargetId } = await cdpSend('Target.createTarget', { url: 'about:blank' });
  const { sessionId: capsSessionId } = await cdpSend('Target.attachToTarget', {
    targetId: capsTargetId, flatten: true,
  });
  const capsSend = (method, params, to) =>
    cdpSend(method, params, capsSessionId, to || STEP_MS);
  await capsSend('Page.enable');
  await capsSend('Runtime.enable');
  const capsLoad = waitForEvent('Page.loadEventFired', STEP_MS, capsSessionId);
  await capsSend('Page.navigate', { url: BASE + '/' });
  await capsLoad;
  await evalSend(capsSend, CAPS_START_EXPR);
  const capsReceipt = await evalSend(capsSend, 'window.__cubicCapsPromise', { awaitPromise: true });
  const caps = capsReceipt && {
    webgl2: capsReceipt.webgl2 === true,
    webgpu: capsReceipt.webgpu === true,
  };
  global.__caps = caps;
  CAPABILITY_ADAPTER_INFO = capsReceipt && capsReceipt.adapterInfo || {};
  CAPABILITY_WEBGL_CONTEXT_RELEASED =
    !!(capsReceipt && capsReceipt.webglContextReleased === true);
  if (!caps || caps.webgl2 !== true || caps.webgpu !== true) {
    throw new Error('native WebGL2 and WebGPU are both required; got ' + JSON.stringify(caps));
  }
  await cdpSend('Target.closeTarget', { targetId: capsTargetId }, null, STEP_MS);
  await sleep(100);
  CAPABILITY_STDERR_RANGE = {
    startByte: capabilityStderrStart,
    afterTargetCloseByte: chromeStderrBytes,
  };

  for (let i = 0; i < AFFINE_CASES.length; i += 1) {
    const c = AFFINE_CASES[i];
    const caseStderrStart = chromeStderrBytes;
    const { targetId } = await cdpSend('Target.createTarget', { url: 'about:blank' });
    const { sessionId } = await cdpSend('Target.attachToTarget', { targetId, flatten: true });
    const send = (method, params, to) => cdpSend(method, params, sessionId, to || STEP_MS);
    try {
      await send('Page.enable');
      await send('Runtime.enable');
      await send('Network.enable');
      await send('Log.enable');
      await send('Page.addScriptToEvaluateOnNewDocument', { source: PRELOAD });
      activeSessionId = sessionId;
      CASE_EVIDENCE.push(await runAffineCase(send, c, sessionId));
    } finally {
      await sleep(100);
      CASE_STDERR_RANGES.push({
        name: c.name,
        startByte: caseStderrStart,
        beforeTargetCloseByte: chromeStderrBytes,
      });
      activeSessionId = null;
      networkRequests.clear();
      try { await cdpSend('Target.closeTarget', { targetId }, null, STEP_MS); } catch (_err) {}
    }
  }
  for (let i = 0; i < CASES.length; i += 1) {
    const c = CASES[i];
    const caseStderrStart = chromeStderrBytes;
    const { targetId } = await cdpSend('Target.createTarget', { url: 'about:blank' });
    const { sessionId } = await cdpSend('Target.attachToTarget', { targetId, flatten: true });
    const send = (method, params, to) => cdpSend(method, params, sessionId, to || STEP_MS);
    try {
      await send('Page.enable');
      await send('Runtime.enable');
      await send('Network.enable');
      await send('Log.enable');
      await send('Page.addScriptToEvaluateOnNewDocument', { source: PRELOAD });
      activeSessionId = sessionId;
      CASE_EVIDENCE.push(await runCase(send, c, sessionId));
    } finally {
      await sleep(100);
      CASE_STDERR_RANGES.push({
        name: c.name,
        startByte: caseStderrStart,
        beforeTargetCloseByte: chromeStderrBytes,
      });
      // Stop accepting diagnostics before intentional target destruction.
      // In particular, Chromium 154 destroys SwiftShader devices here and
      // reports that expected lifecycle event as a console warning.
      activeSessionId = null;
      networkRequests.clear();
      try { await cdpSend('Target.closeTarget', { targetId }, null, STEP_MS); } catch (_err) {}
    }
  }
  if (clientEventResponses.length !== CASES.length + AFFINE_CASES.length) {
    fail('expected one intentional client-events 204 per renderer case, got ' +
      clientEventResponses.length);
  }
})().catch((e) => {
  fail(String(e && e.stack || e));
}).then(async () => {
  if (!finished) {
    finished = true;
    clearTimeout(watchdog);
  }
  await cleanup();
  chromeDiagnostics = scanChromeDiagnostics();
  if (chromeDiagnostics.scanError) {
    fail('Chrome stderr diagnostic scan failed: ' + chromeDiagnostics.scanError);
  }
  if (chromeDiagnostics.swapFindings.length > 0) {
    fail('Chrome stderr contains forbidden swap/SharedImage diagnostics: ' +
      JSON.stringify(chromeDiagnostics.swapFindings));
  }
  if (chromeDiagnostics.preTeardownLifecycleFindings.length > 0) {
    fail('Chrome stderr contains pre-teardown WebGPU lifecycle diagnostics: ' +
      JSON.stringify(chromeDiagnostics.preTeardownLifecycleFindings));
  }
  writeReport({});
  exitCode = (errors.length || warnings.length || notFound.length || unexpectedRequests.length ||
    networkFailures.length || reportWriteFailed) ? 1 : 0;
  setTimeout(() => process.exit(exitCode), 50);
});
