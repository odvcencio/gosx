'use strict';
/* Temporary browser-verification probe: real Chrome + real WebGL playback of
 * ordinary glTF node TRS animation driven by the REAL standard-Go WASM motion
 * mixer built from ./client/wasm (GOOS=js GOARCH=wasm CGO_ENABLED=0; not
 * TinyGo; GPU/hardware renderer certification is not in scope).
 * Node builtin-only; no npm dependencies. Supplements (does not replace) the
 * existing JavaScript-mixer browser probe and the stubbed-exports WASM unit
 * route by executing the actual compiled Go module in the browser.
 *
 * Usage: node scene3d-node-animation-wasm-browser.cjs <repoRoot>
 *
 * What this proves:
 *  - the genuine standard-Go runtime builds, instantiates and runs in Chrome;
 *    main registers the real __gosx_motion_mixer_* exports and then blocks in
 *    select{} forever (the go.run promise is tracked but never awaited, since
 *    a healthy runtime never resolves it);
 *  - bootstrap mounts only AFTER the real exports exist, with
 *    __gosx_motion_wasm set, and the model record is owned by the WASM mixer
 *    (never the JS mixer);
 *  - ordinary mesh (TRIANGLES, mode 4), POINTS and LINES geometry all land at
 *    the known animated final coordinates, including the ghost node authored
 *    at zero scale recovering to [1,1,1];
 *  - real native WebGL/WebGL2 draw calls are observed;
 *  - dispose destroys exactly one mixer handle per created handle, stale
 *    handles no longer report playing via the real is_playing export, and the
 *    scene state is removed.
 *
 * The mixer exports are only wrapped in counting forwarders that preserve
 * arguments, results and this — never stubbed or replaced. The GLB fixture is
 * extracted from client/js/scene3d-node-animation.test.js via the existing
 * bounded-VM template; expected coordinates are the same hand-calculated
 * values the working TRS browser probe already uses. No production files are
 * modified; the wasm artifact is generated into a private mkdtemp scratch dir
 * and removed on exit (no checked-in binaries).
 */
const fs = require('fs');
const os = require('os');
const path = require('path');
const http = require('http');
const vm = require('vm');
const crypto = require('crypto');
const { spawn, execFileSync } = require('child_process');

const REPO = path.resolve(process.argv[2] || '');
if (!process.argv[2]) {
  console.error('usage: node scene3d-node-animation-wasm-browser.cjs <repoRoot>');
  process.exit(2);
}

const errors = [], warnings = [];
const fail = (m) => { errors.push(m); };

const GO_BUILD_TIMEOUT_MS = 240000;    // bounded go build
const PAGE_POLL_ITERATIONS = 200;      // 200 x 100ms = 20s per page-side poll
const PAGE_POLL_INTERVAL_MS = 100;
const SETTLE_MS = 1500;                // real RAF frames (flat-tail clip)
const OVERALL_MS = 420000;             // whole-process watchdog

// ---- Bounded VM extraction of the GLB fixture from the existing test file ----
const testPath = path.join(REPO, 'client', 'js', 'scene3d-node-animation.test.js');
const bootstrapPath = path.join(REPO, 'client', 'js', 'bootstrap.js');
if (!fs.existsSync(testPath)) { console.error('missing fixture source: ' + testPath); process.exit(2); }
if (!fs.existsSync(bootstrapPath)) { console.error('missing production loader: ' + bootstrapPath); process.exit(2); }
const testSrc = fs.readFileSync(testPath, 'utf8');
const a = testSrc.indexOf('const S =');
const b = testSrc.indexOf('function mountTRSEngine');
if (a < 0 || b < 0 || b <= a) { console.error('fixture markers not found'); process.exit(2); }
const sandbox = { Buffer, console,
  assert: { ok(cond, msg) { if (!cond) throw new Error(String(msg)); } } };
vm.createContext(sandbox);
vm.runInContext(testSrc.slice(a, b), sandbox, { filename: 'trs-fixture-snippet.js', timeout: 10000 });
if (typeof sandbox.buildTRSGLBBytes !== 'function') { console.error('buildTRSGLBBytes missing'); process.exit(2); }
const glb = Buffer.from(sandbox.buildTRSGLBBytes({ ghost: true }));

// ---- Owned scratch dir for the generated runtime artifact (no checked-in wasm) ----
const scratch = fs.mkdtempSync(path.join(os.tmpdir(), 'gosx-trs-wasm-build-'));
const wasmPath = path.join(scratch, 'client-wasm.wasm');

// ---- Version-matched wasm_exec.js from GOROOT (lib/wasm first, misc/wasm fallback) ----
let wasmExecJs = null;
try {
  const goRoot = execFileSync('go', ['env', 'GOROOT'],
    { cwd: REPO, encoding: 'utf8', timeout: 15000 }).trim();
  const candidates = [path.join(goRoot, 'lib', 'wasm', 'wasm_exec.js'),
    path.join(goRoot, 'misc', 'wasm', 'wasm_exec.js')];
  for (let i = 0; i < candidates.length; i += 1) {
    if (fs.existsSync(candidates[i])) { wasmExecJs = candidates[i]; break; }
  }
} catch (e) { fail('go env GOROOT failed: ' + (e && e.message ? e.message : e)); }
if (!wasmExecJs) fail('version-matched wasm_exec.js not found under GOROOT (lib/wasm, then misc/wasm)');

const ENGINE_ID = 'gosx-engine-trs-wasm-browser';
const MOUNT_ID = 'scene-trs-wasm-browser';

const manifest = JSON.stringify({ engines: [{ id: ENGINE_ID, component: 'GoSXScene3D',
  kind: 'surface', mountId: MOUNT_ID,
  props: { width: 320, height: 180, autoRotate: false,
    forceWebGL: true, requireWebGL: true,
    models: [{ id: 'trs', src: '/models/trs.glb', animation: 'move', loop: true, static: true }] } }] });

// In-page boot: instantiate the GENUINE module, call go.run (never awaited —
// main blocks in select{} forever), wait for the REAL __gosx_motion_mixer_*
// exports, install counting forwarders, set the flag, THEN append bootstrap.
const BOOT = `
window.__gosx_boot_errors = [];
window.__gosx_wasm_ready = false;
window.__gosx_wasm_exited = false;
window.__gosx_run_started = false;
window.__gosx_mixer_counters = {};
window.__gosx_mixer_wrapped = false;
(async function bootRealGoWasm() {
  const NAMES = ['__gosx_motion_mixer_create', '__gosx_motion_mixer_add_clip',
    '__gosx_motion_mixer_play', '__gosx_motion_mixer_update', '__gosx_motion_mixer_stop',
    '__gosx_motion_mixer_is_playing', '__gosx_motion_mixer_destroy'];
  try {
    if (typeof Go !== 'function') throw new Error('wasm_exec.js did not define global Go');
    const go = new Go();
    const resp = await fetch('/runtime.wasm');
    if (!resp.ok) throw new Error('runtime.wasm load failed: HTTP ' + resp.status);
    const bytes = await resp.arrayBuffer();
    if (bytes.byteLength < 64) throw new Error('runtime.wasm suspiciously small (' + bytes.byteLength + ' bytes)');
    let instance;
    try {
      const compiled = await WebAssembly.instantiate(bytes, go.importObject);
      instance = compiled.instance;
    } catch (e) {
      throw new Error('WebAssembly.instantiate failed: ' + (e && e.message ? e.message : e));
    }
    // main() registers the real mixer exports then blocks in select{} forever:
    // the run promise is tracked but NEVER awaited to completion.
    const runPromise = go.run(instance);
    window.__gosx_run_started = true;
    runPromise.then(function () { window.__gosx_wasm_exited = true; },
      function (err) { window.__gosx_boot_errors.push('go.run rejected: ' +
        (err && (err.stack || err.message) || String(err))); });
    const deadline = Date.now() + 20000;
    for (;;) {
      let all = true;
      for (let i = 0; i < NAMES.length; i += 1) {
        if (typeof window[NAMES[i]] !== 'function') { all = false; break; }
      }
      if (all) break;
      if (window.__gosx_boot_errors.length) throw new Error(window.__gosx_boot_errors[0]);
      if (Date.now() > deadline) throw new Error('real __gosx_motion_mixer_* exports were not registered by the Go runtime in time');
      await new Promise(function (r) { setTimeout(r, 20); });
    }
    // Counting wrappers ONLY: each faithfully forwards to the captured real
    // Go callback, preserving arguments, results and this. No stubbing.
    NAMES.forEach(function (name) {
      const orig = window[name];
      window['__gosx_orig_' + name] = orig;
      window.__gosx_mixer_counters[name] = 0;
      window[name] = function () {
        window.__gosx_mixer_counters[name] += 1;
        return orig.apply(this, arguments);
      };
    });
    window.__gosx_mixer_wrapped = true;
    window.__gosx_motion_wasm = true;
    window.__gosx_wasm_ready = true;
    // Exports exist before the production loader mounts: append bootstrap now.
    const s = document.createElement('script');
    s.src = '/bootstrap.js';
    s.onerror = function () { window.__gosx_boot_errors.push('bootstrap.js failed to load'); };
    document.body.appendChild(s);
  } catch (e) {
    window.__gosx_boot_errors.push(String(e && e.stack || e));
  }
})();
`;

const html = '<!doctype html><html><head><meta charset="utf-8"></head><body>' +
  '<div id="' + MOUNT_ID + '" width="320" height="180"></div>' +
  '<script type="application/json" id="gosx-manifest">' + manifest + '</script>' +
  '<script src="/wasm_exec.js"></script>' +
  '<script>' + BOOT + '</script>' +
  '</body></html>';

const server = http.createServer((req, res) => {
  if (req.url === '/models/trs.glb') {
    res.writeHead(200, { 'content-type': 'model/gltf-binary', 'content-length': glb.length });
    res.end(glb);
  } else if (req.url === '/bootstrap.js' || req.url === '/client/js/bootstrap.js') {
    const js = fs.readFileSync(bootstrapPath);
    res.writeHead(200, { 'content-type': 'text/javascript; charset=utf-8', 'content-length': js.length });
    res.end(js);
  } else if (req.url === '/wasm_exec.js') {
    const js = fs.readFileSync(wasmExecJs);
    res.writeHead(200, { 'content-type': 'text/javascript; charset=utf-8', 'content-length': js.length });
    res.end(js);
  } else if (req.url === '/runtime.wasm') {
    const wasmBytes = fs.readFileSync(wasmPath);
    res.writeHead(200, { 'content-type': 'application/wasm', 'content-length': wasmBytes.length });
    res.end(wasmBytes);
  } else if (req.url === '/') {
    res.writeHead(200, { 'content-type': 'text/html; charset=utf-8' });
    res.end(html);
  } else { res.writeHead(404); res.end(); }
});

// ---- CDP plumbing (reused shape from the working TRS browser probe) ----
let ws = null, msgId = 0, chrome = null, profile = null;
const pending = new Map(), listeners = [];
function cdpSend(method, params, sessionId, timeoutMs) {
  const id = ++msgId;
  return new Promise((resolve, reject) => {
    const t = setTimeout(() => { pending.delete(id); reject(new Error('CDP timeout: ' + method)); },
      timeoutMs || 15000);
    pending.set(id, { resolve, reject, t });
    ws.send(JSON.stringify(Object.assign({ id, method }, params ? { params } : {},
      sessionId ? { sessionId } : {})));
  });
}
function waitForEvent(name, timeoutMs) {
  return new Promise((resolve, reject) => {
    const entry = { name, resolve, timer: setTimeout(() => {
      const i = listeners.indexOf(entry); if (i >= 0) listeners.splice(i, 1);
      reject(new Error('event timeout: ' + name)); }, timeoutMs || 15000) };
    listeners.push(entry);
  });
}
function dispatch(raw) {
  const m = JSON.parse(raw);
  if (m.id && pending.has(m.id)) {
    const p = pending.get(m.id); pending.delete(m.id); clearTimeout(p.t);
    if (m.error) p.reject(new Error(m.error.message));
    else if (m.result && m.result.exceptionDetails) {
      const d = m.result.exceptionDetails;
      p.reject(new Error('Runtime.evaluate exception: ' + ((d.exception && d.exception.description) || d.text)));
    } else p.resolve(m.result);
  } else if (m.method) {
    for (let i = listeners.length - 1; i >= 0; i -= 1) {
      if (listeners[i].name === m.method) {
        const e = listeners[i]; clearTimeout(e.timer); listeners.splice(i, 1); e.resolve(m.params || {});
      }
    }
    if (m.method === 'Runtime.consoleAPICalled') {
      const args = (m.params && m.params.args) || [];
      const text = args.map((x) => x.value || x.description || '').join(' ');
      if (m.params.type === 'error') errors.push(text);
      else if (m.params.type === 'warning') warnings.push(text);
    }
    if (m.method === 'Runtime.exceptionThrown') {
      errors.push((m.params.exceptionDetails.exception &&
        m.params.exceptionDetails.exception.description) || m.params.exceptionDetails.text);
    }
  }
}

// Native draw-call counter + a __gosx_runtime_ready recorder (main's
// notifyRuntimeReady invokes it once the runtime has finished registering).
const PRELOAD = `
window.__gosxDRAWS = 0; window.__gosxGL = null;
window.__gosx_runtime_ready_calls = 0;
window.__gosx_runtime_ready = function () { window.__gosx_runtime_ready_calls += 1; };
function wrap(proto) {
  if (!proto) return;
  ['drawArrays', 'drawElements'].forEach(function (n) {
    const o = proto[n]; if (!o) return;
    proto[n] = function () {
      window.__gosxDRAWS += 1;
      window.__gosxGL = (this instanceof WebGL2RenderingContext) ? 'webgl2' : 'webgl';
      return o.apply(this, arguments);
    };
  });
}
wrap(WebGLRenderingContext.prototype); wrap(WebGL2RenderingContext.prototype);
`;

function assertClose(actual, expected, label, tol) {
  const t = tol == null ? 1e-3 : tol;
  if (!actual || actual.length !== expected.length) { fail(label + ': missing/wrong length'); return; }
  for (let i = 0; i < expected.length; i += 1) {
    const v = Number(actual[i]);
    if (!Number.isFinite(v) || !Number.isFinite(expected[i]) || Math.abs(v - expected[i]) >= t) {
      fail(label + '[' + i + ']=' + actual[i] + ' want ' + expected[i]);
    }
  }
}

// Known final poses (same hand-calculated values as the existing probe).
const EXPECTED = {
  tri: [5, 0, 0, 7, 0, 0, 5, 2, 0],     // rig +5x * tri-node scale 2
  points: [9, 0, 0],                    // (2,0,0) * scale2 + rig +5x
  lines: [8, 0, 0, 9, 0, 0],            // authored (3,0,0)-(4,0,0) + rig +5x
  ghost: [5, 0, 0, 6, 0, 0, 5, 1, 0],   // zero-scale authored, animated to [1,1,1]
};

const evidence = {
  label: 'standard Go WASM motion-mixer browser probe (GOOS=js GOARCH=wasm CGO_ENABLED=0; ' +
    'not TinyGo; hardware/GPU renderer certification not in scope)',
  repo: REPO,
  runtimeArtifact: null,
  wasmExecJs: wasmExecJs,
  realWasmReady: false,
  wasmReadyCalls: null,
  mounted: false,
  wasm: null,
  geometry: null,
  renderer: null,
  disposal: null,
  disposeRemovedState: false,
  exitCode: null,
  errors: [], warnings: [],
};

let exitCode = 0;
let cleaned = false;
async function cleanup() {
  if (cleaned) return; cleaned = true;
  try { if (ws) ws.close(); } catch (e) {}
  if (chrome) {
    const exited = new Promise((res) => chrome.once('exit', res));
    try { chrome.kill('SIGKILL'); } catch (e) {}
    await Promise.race([exited, new Promise((res) => setTimeout(res, 5000))]);
  }
  if (profile) {
    try { fs.rmSync(profile, { recursive: true, force: true }); }
    catch (e) { warnings.push('profile cleanup skipped: ' + e.message); }
  }
  if (scratch) {
    try { fs.rmSync(scratch, { recursive: true, force: true }); }
    catch (e) { warnings.push('scratch cleanup skipped: ' + e.message); }
  }
  await new Promise((res) => {
    let done = false; const fin = () => { if (!done) { done = true; res(); } };
    const t = setTimeout(fin, 3000);
    try { server.close(() => { clearTimeout(t); fin(); }); } catch (e) { clearTimeout(t); fin(); }
  });
}

let reported = false;
function finish() {
  if (reported) return; reported = true;
  evidence.errors = errors.slice();
  evidence.warnings = warnings.slice();
  evidence.exitCode = exitCode || (errors.length ? 1 : 0);
  console.log(JSON.stringify(evidence, null, 2));
  setTimeout(() => process.exit(evidence.exitCode), 50);
}

const overallTimer = setTimeout(() => {
  fail('overall probe timeout after ' + OVERALL_MS + 'ms');
  exitCode = 1;
  cleanup().then(finish);
}, OVERALL_MS);

(async () => {
  // ---- Build the genuine standard-Go runtime (bounded, owned scratch) ----
  try {
    const buildEnv = Object.assign({}, process.env,
      { GOOS: 'js', GOARCH: 'wasm', CGO_ENABLED: '0' });
    execFileSync('go', ['build', '-o', wasmPath, './client/wasm'],
      { cwd: REPO, env: buildEnv, timeout: GO_BUILD_TIMEOUT_MS });
    const wasmBytes = fs.readFileSync(wasmPath);
    if (!wasmBytes || wasmBytes.length < 64) throw new Error('built runtime artifact is empty or too small');
    evidence.runtimeArtifact = {
      source: 'generated: go build -o <scratch>/client-wasm.wasm ./client/wasm (standard Go; no checked-in binaries)',
      bytes: wasmBytes.length,
      sha256: crypto.createHash('sha256').update(wasmBytes).digest('hex'),
    };
  } catch (e) {
    fail('standard-Go runtime build failed: ' + (e && e.message ? e.message : e));
    return;
  }
  if (!wasmExecJs) return;

  await new Promise((res) => server.listen(0, '127.0.0.1', res));
  const port = server.address().port;
  profile = fs.mkdtempSync(path.join(os.tmpdir(), 'gosx-trs-wasm-probe-'));
  const chromeBin = process.env.GOSX_PROBE_CHROME || '/usr/bin/google-chrome';
  chrome = spawn(chromeBin, [
    '--headless=new', '--no-sandbox', '--use-gl=angle', '--use-angle=gl-egl',
    '--ignore-gpu-blocklist', '--enable-unsafe-swiftshader',
    '--disable-dev-shm-usage', '--user-data-dir=' + profile, '--remote-debugging-port=0', 'about:blank',
  ], { stdio: ['ignore', 'ignore', 'pipe'] });
  const wsUrl = await new Promise((resolve, reject) => {
    let buf = '';
    const t = setTimeout(() => reject(new Error('no DevTools ws URL')), 20000);
    chrome.stderr.on('data', (d) => {
      buf += d.toString();
      const m = buf.match(/ws:\/\/127\.0\.0\.1:\d+\/devtools\/browser\/[^\s]+/);
      if (m) { clearTimeout(t); resolve(m[0]); }
    });
    chrome.on('exit', () => { clearTimeout(t); reject(new Error('chrome exited early: ' + buf)); });
  });
  ws = new WebSocket(wsUrl);
  await new Promise((res, rej) => {
    const t = setTimeout(() => rej(new Error('ws connect timeout')), 20000);
    ws.onopen = () => { clearTimeout(t); res(); };
    ws.onerror = () => { clearTimeout(t); rej(new Error('ws error')); };
  });
  ws.onmessage = (ev) => dispatch(ev.data);

  const { targetId } = await cdpSend('Target.createTarget', { url: 'about:blank' });
  const { sessionId } = await cdpSend('Target.attachToTarget', { targetId, flatten: true });
  const send = (method, params, to) => cdpSend(method, params, sessionId, to || 20000);
  await send('Page.enable'); await send('Runtime.enable');
  await send('Page.addScriptToEvaluateOnNewDocument', { source: PRELOAD });
  const loaded = waitForEvent('Page.loadEventFired', 20000);
  await send('Page.navigate', { url: 'http://127.0.0.1:' + port + '/' });
  await loaded;

  // ---- Wait for the REAL Go runtime: exports registered, wrappers on, flag set ----
  const WASM_POLL = '(()=>({ready:!!window.__gosx_wasm_ready,exited:!!window.__gosx_wasm_exited,' +
    'bootErrors:(window.__gosx_boot_errors||[]).length,' +
    'firstBootError:(window.__gosx_boot_errors||[])[0]||null,' +
    'readyCalls:window.__gosx_runtime_ready_calls||0}))()';
  let wasmReady = false;
  for (let i = 0; i < PAGE_POLL_ITERATIONS; i += 1) {
    const r = await send('Runtime.evaluate', { expression: WASM_POLL, returnByValue: true });
    const v = r.result && r.result.value;
    if (v) {
      if (v.bootErrors > 0) { fail('Go runtime boot error: ' + v.firstBootError); break; }
      if (v.exited) { fail('Go runtime main exited early (select{} must block forever)'); break; }
      if (v.ready) { wasmReady = true; evidence.wasmReadyCalls = v.readyCalls; break; }
    }
    await new Promise((res) => setTimeout(res, PAGE_POLL_INTERVAL_MS));
  }
  evidence.realWasmReady = wasmReady;
  if (!wasmReady) {
    fail('real standard-Go WASM runtime did not become ready (silent skip / JS fallback is not permitted)');
    return;
  }

  // ---- Mount poll (bootstrap is appended only after the exports existed) ----
  const POLL = '(()=>{const m=document.getElementById("' + MOUNT_ID + '");' +
    'const s=m&&m.__gosxScene3DState;' +
    'return !!(s&&s._modelSkins&&s._modelSkins.length===1&&s.objects&&s.objects.size);})()';
  let mounted = false;
  for (let i = 0; i < PAGE_POLL_ITERATIONS; i += 1) {
    const r = await send('Runtime.evaluate', { expression: POLL, returnByValue: true });
    if (r.result && r.result.value === true) { mounted = true; break; }
    await new Promise((res) => setTimeout(res, PAGE_POLL_INTERVAL_MS));
  }
  evidence.mounted = mounted;
  if (!mounted) fail('engine did not mount with exactly one _modelSkins record');

  // Real frames: flat-tail clip means any sample after ~0.2s is the final pose.
  await new Promise((res) => setTimeout(res, SETTLE_MS));

  const READ = '(()=>{const m=document.getElementById("' + MOUNT_ID + '");const s=m&&m.__gosxScene3DState;' +
    'if(!s)return null;' +
    'const meshPos=(r)=>(r&&r.vertices&&r.vertices.positions)?Array.from(r.vertices.positions):null;' +
    'const getObj=(k)=>(s.objects&&s.objects.get)?s.objects.get(k):null;' +
    'const pe=(s.points||[]).find((p)=>p.id==="trs/tri-points-1");' +
    'const lo=getObj("trs/filament-lines-0");' +
    'const rec=(s._modelSkins&&s._modelSkins[0])||null;' +
    'return {drawCalls:window.__gosxDRAWS,gl:window.__gosxGL,objectsCount:s.objects?s.objects.size:0,' +
    'wasmFlag:!!window.__gosx_motion_wasm,wasmReady:!!window.__gosx_wasm_ready,' +
    'wasmWrapped:!!window.__gosx_mixer_wrapped,runStarted:!!window.__gosx_run_started,' +
    'exited:!!window.__gosx_wasm_exited,runtimeReadyCalls:window.__gosx_runtime_ready_calls||0,' +
    'bootErrors:(window.__gosx_boot_errors||[]).slice(),' +
    'counters:(window.__gosx_mixer_counters||{}),' +
    'skins:s._modelSkins?s._modelSkins.length:0,' +
    'wasmMixer:rec?(rec.wasmMixer||null):null,wasmMixerActive:rec?!!rec.wasmMixerActive:false,' +
    'jsMixerPresent:rec?!(rec.mixer==null):null,' +
    'animation:rec?(rec.animation||""):"",' +
    'tri:meshPos(getObj("trs/tri-prim-0")),' +
    'ghost:meshPos(getObj("trs/ghost-prim-0")),' +
    'pt:(pe&&pe.positions)?Array.from(pe.positions):null,' +
    'line:(lo&&lo.points)?lo.points.map((v)=>({x:v.x,y:v.y,z:v.z})):null};})()';
  const read = await send('Runtime.evaluate', { expression: READ, returnByValue: true });
  const s = read.result && read.result.value;
  if (!s) { fail('could not read __gosxScene3DState'); }
  else {
    evidence.wasm = {
      flag: !!s.wasmFlag, ready: !!s.wasmReady, wrapped: !!s.wasmWrapped,
      runStarted: !!s.runStarted, exited: !!s.exited,
      runtimeReadyCalls: s.runtimeReadyCalls || 0,
      bootErrors: s.bootErrors || [],
      counters: s.counters || {},
      handle: s.wasmMixer,
      wasmMixerActive: !!s.wasmMixerActive,
      jsMixerUsedForRecord: s.jsMixerPresent === true,
      animation: s.animation || '',
    };

    const attr = await send('Runtime.evaluate', { expression:
      '(()=>{const m=document.getElementById("' + MOUNT_ID + '");' +
      'return {mounted:m.getAttribute("data-gosx-scene3d-mounted"),' +
      'renderer:m.getAttribute("data-gosx-scene3d-renderer"),' +
      'fallback:m.getAttribute("data-gosx-scene3d-fallback")};})()', returnByValue: true });
    const attrs = attr.result && attr.result.value;
    if (attrs && attrs.renderer === 'webgl' && attrs.mounted === 'true') {
      if (s.gl !== 'webgl' && s.gl !== 'webgl2') fail('observed context not WebGL (got ' + s.gl + ')');
    } else fail('data-gosx-scene3d-renderer/mounted attributes missing or not webgl' +
      (attrs ? ' (mounted=' + attrs.mounted + ', renderer=' + attrs.renderer +
        ', fallback=' + attrs.fallback + ')' : ' (attribute read failed)'));
    const attempts = await send('Runtime.evaluate', { expression:
      'JSON.stringify(window.__gosx_scene3d_backend_attempts||[])', returnByValue: true });
    evidence.renderer = {
      drawCalls: s.drawCalls,
      observedContext: s.gl,
      mountedAttr: attrs ? attrs.mounted : null,
      rendererAttr: attrs ? attrs.renderer : null,
      fallbackAttr: attrs ? attrs.fallback : null,
      backendAttempts: attempts.result && attempts.result.value || 'unavailable',
      objectsCount: s.objectsCount,
    };
    if (!(s.drawCalls > 0)) fail('no real native WebGL/WebGL2 draw calls observed');

    // ---- WASM-selected asserts (flag, handle, real create/update calls, no JS mixer) ----
    if (!s.wasmFlag) fail('__gosx_motion_wasm flag not set');
    if (!(s.wasmMixer >= 1)) fail('record.wasmMixer handle not positive (got ' + s.wasmMixer + ')');
    if (!s.wasmMixerActive) fail('record.wasmMixerActive not true');
    const counters = s.counters || {};
    if (!((counters.__gosx_motion_mixer_create | 0) >= 1)) fail('real __gosx_motion_mixer_create was never called');
    if (!((counters.__gosx_motion_mixer_update | 0) >= 1)) fail('real __gosx_motion_mixer_update was never called');
    if (!((counters.__gosx_motion_mixer_add_clip | 0) >= 1)) fail('real __gosx_motion_mixer_add_clip was never called');
    if (s.jsMixerPresent === true) fail('record used the JS mixer; the WASM mixer must own this record');
    if (s.exited) fail('Go runtime exited before dispose');
    if ((s.bootErrors || []).length) fail('page boot errors: ' + s.bootErrors.join(' | '));

    // ---- Geometry: all three kinds + ghost zero-scale recovery ----
    const geoErrsBefore = errors.length;
    assertClose(s.tri, EXPECTED.tri, 'tri-prim-0');
    assertClose(s.pt, EXPECTED.points, 'tri-points-1');
    if (s.line) {
      assertClose([s.line[0].x, s.line[0].y, s.line[0].z], [EXPECTED.lines[0], EXPECTED.lines[1], EXPECTED.lines[2]],
        'filament-lines-0[0]');
      assertClose([s.line[1].x, s.line[1].y, s.line[1].z], [EXPECTED.lines[3], EXPECTED.lines[4], EXPECTED.lines[5]],
        'filament-lines-0[1]');
    } else fail('filament-lines-0 missing');
    assertClose(s.ghost, EXPECTED.ghost, 'ghost-prim-0');
    if (s.skins !== 1) fail('_modelSkins length ' + s.skins);
    evidence.geometry = {
      expected: EXPECTED,
      actual: { tri: s.tri, points: s.pt, lines: s.line, ghost: s.ghost },
      matchesAnimatedExpected: errors.length === geoErrsBefore,
    };
  }

  // ---- Dispose: state removal + destroy accounting + stale handle check ----
  const destroyBefore = s ? ((s.counters && s.counters.__gosx_motion_mixer_destroy) | 0) : null;
  const preBootErrors = s ? (s.bootErrors || []).length : 0;
  const handle = s ? s.wasmMixer : null;
  const disp = await send('Runtime.evaluate', {
    expression: 'typeof __gosx_dispose_engine==="function" && ' +
      '(__gosx_dispose_engine("' + ENGINE_ID + '"), true) && ' +
      '!document.getElementById("' + MOUNT_ID + '").__gosxScene3DState',
    returnByValue: true });
  const disposeRemovedState = disp.result && disp.result.value === true;
  if (!disposeRemovedState) fail('__gosx_dispose_engine did not remove state');
  evidence.disposeRemovedState = disposeRemovedState;

  const POST = '(()=>({destroy:(window.__gosx_mixer_counters||{}).__gosx_motion_mixer_destroy|0,' +
    'create:(window.__gosx_mixer_counters||{}).__gosx_motion_mixer_create|0,' +
    'exited:!!window.__gosx_wasm_exited,' +
    'bootErrors:(window.__gosx_boot_errors||[]).length}))()';
  const post = await send('Runtime.evaluate', { expression: POST, returnByValue: true });
  const pv = post.result && post.result.value || {};
  const destroyAfter = pv.destroy | 0;
  const created = pv.create | 0;
  const destroyDelta = destroyAfter - (destroyBefore | 0);
  const disposal = {
    destroyBefore: destroyBefore,
    destroyAfter: destroyAfter,
    destroyDelta: destroyDelta,
    createdHandles: created,
    oneDestroyPerCreatedHandle: created >= 1 && destroyDelta === created,
    runtimeExitedAfterDispose: !!pv.exited,
    bootErrorsAfterDispose: pv.bootErrors | 0,
    staleHandle: null,
  };
  if (!(created >= 1)) fail('no WASM mixer handles were created');
  else if (destroyDelta !== created) {
    fail('destroy calls (' + destroyDelta + ') != created handles (' + created + ') at dispose');
  }
  if (pv.exited) fail('Go runtime exited during dispose (select{} must keep it alive)');
  if ((pv.bootErrors | 0) > preBootErrors) fail('page errors appeared during dispose');
  // Stale handle must no longer be playing (known API exercised via the real export).
  if (handle >= 1) {
    const STALE = '(()=>{try{return {playing:window.__gosx_orig___gosx_motion_mixer_is_playing(' +
      handle + ',"move")===true,error:null};}catch(e){return {playing:null,error:String(e&&e.message||e)};}})()';
    const stale = await send('Runtime.evaluate', { expression: STALE, returnByValue: true });
    const sv = stale.result && stale.result.value || {};
    disposal.staleHandle = sv;
    if (sv.playing === true) fail('stale mixer handle ' + handle + ' still reports playing after dispose');
    else if (sv.error) fail('is_playing on stale handle ' + handle + ' errored: ' + sv.error);
  }
  evidence.disposal = disposal;

  if (errors.length) exitCode = 1;
})().catch((e) => {
  errors.push(String(e && e.stack || e)); exitCode = 1;
})
  .then(async () => {
    clearTimeout(overallTimer);
    await cleanup();
  })
  .then(finish);
