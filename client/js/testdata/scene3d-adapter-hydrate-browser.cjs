'use strict';
/* Real-browser acceptance for the Scene3D generic adapter/hydrate seam.
 *
 * The proof builds and boots the genuine standard-Go client/wasm runtime in
 * Chrome, then mounts one shared-runtime Scene3D generation on native WebGL2
 * and one on WebGPU. For each backend a forwarding wrapper delays only the
 * first real hydrate result. A second same-ID mount hydrates a different real
 * VM program and wins; releasing the first result must not mutate or publish.
 * The winning version-1 scene3d.commands envelope must render a nonblank frame
 * with intact rest pixels before a hub-wire transform command is delivered
 * through the public targeted command bridge. Native WebGL/WebGPU calls are
 * observed only through forwarding wrappers.
 *
 * The bounded hub test invoked below also proves an ordinary diff round-trips
 * while a mixed remount-required diff returns no commands and mutates nothing.
 * Native WebGL2 is required. The WebGPU-requested case must either prove a
 * native WebGPU frame or a typed fail-closed WebGL fallback for native WebGPU
 * unavailability/device loss; no skip/generic fallback is accepted. Every
 * request is constrained to an exact loopback allowlist; warnings,
 * errors, HTTP failures, unaccounted aborts, and cleanup failures fail.
 *
 * Usage:
 *   node scene3d-adapter-hydrate-browser.cjs <repoRoot> <artifactDir>
 */

const fs = require('fs');
const os = require('os');
const path = require('path');
const http = require('http');
const crypto = require('crypto');
const { spawn, spawnSync, execFileSync } = require('child_process');

const REPO = path.resolve(process.argv[2] || '');
const ART = path.resolve(process.argv[3] || '');
if (!process.argv[2] || !process.argv[3]) {
  console.error('usage: node scene3d-adapter-hydrate-browser.cjs <repoRoot> <artifactDir>');
  process.exit(2);
}
try {
  if (!fs.statSync(ART).isDirectory()) throw new Error('not a directory');
} catch (e) {
  console.error('artifact dir not usable: ' + ART + ' (' + e.message + ')');
  process.exit(2);
}

const errors = [];
const warnings = [];
const warningOccurrences = [];
let currentCaseName = '';
let currentCasePhase = '';
const fail = (message) => { errors.push(String(message)); };
const sleep = (ms) => new Promise((resolve) => setTimeout(resolve, ms));

const W = 320;
const H = 180;
const FG_THRESHOLD = 12;
const FG_COVERAGE = 0.01;
const REST_COVERAGE = 0.5;
const STEP_MS = 20000;
const MOUNT_MS = 40000;
const OVERALL_MS = 420000;
const BUILD_MS = 240000;
const DIAGNOSTIC_CAPTURE_MS = 2000;
const CHROME_BIN = process.env.GOSX_CHROME_BIN || '/usr/bin/google-chrome';
const EXPECTED_VERSION_ENV = 'GOSX_EXPECTED_CHROME_VERSION';
const FOUR_PART_VERSION = /^\d+\.\d+\.\d+\.\d+$/;
const CHROME_CLI_PRODUCT = /^(Google Chrome|Google Chrome for Testing) (\d+\.\d+\.\d+\.\d+)$/;
const HOSTED_POLICY = 'requested-webgpu-or-typed-webgl-fallback';
const OUTCOME_NATIVE_WEBGPU = 'native-webgpu';
const OUTCOME_FALLBACK_UNAVAILABLE = 'fallback-unavailable';
const OUTCOME_FALLBACK_DEVICE_LOST = 'fallback-device-lost';
const FALLBACK_UNAVAILABLE = 'webgpu-unavailable';
const FALLBACK_DEVICE_LOST = 'webgpu-device-lost';
const OUTCOME_NATIVE_WEBGL2 = 'native-webgl2';
const FALLBACK_WARNING_PATTERNS = [
  /^console\.warning: \[gosx\] WebGPU probe(:| failed:| requestDevice failed; retrying with a fresh adapter:| device lost:)/,
  /^console\.warning: \[gosx\] WebGPU renderer creation failed:/,
  /^console\.warning: \[gosx\] WebGPU factory returned null after probe success; canvas may be tainted$/,
];
const CAPTURE_DRIVER_WARNING = /^browser log warning: GL Driver Message \(OpenGL, Performance, GL_CLOSE_PATH_NV, High\): GPU stall due to ReadPixels(?: \(this message will no longer repeat\))?$/;

const CASES = [
  { name: 'gl', webgpu: false, engine: 'gosx-engine-adapter-gl', mount: 'scene-adapter-gl' },
  { name: 'wg', webgpu: true, engine: 'gosx-engine-adapter-wg', mount: 'scene-adapter-wg' },
];

function selectedBrowser() {
  if (!fs.existsSync(CHROME_BIN)) throw new Error('chrome binary not found: ' + CHROME_BIN);
  const probe = spawnSync(CHROME_BIN, ['--version'], {
    encoding: 'utf8', timeout: 10000, windowsHide: true,
  });
  if (probe.error || probe.status !== 0) {
    throw new Error('selected Chrome --version failed: ' +
      String(probe.error && probe.error.message || probe.stderr || probe.status));
  }
  const invocation = String(probe.stdout || probe.stderr || '').trim();
  const match = CHROME_CLI_PRODUCT.exec(invocation);
  if (!match) {
    throw new Error('selected browser CLI identity must be exactly Google Chrome <four-part> ' +
      'or Google Chrome for Testing <four-part>; got ' + JSON.stringify(invocation));
  }
  const expectedIsSet = Object.prototype.hasOwnProperty.call(process.env, EXPECTED_VERSION_ENV);
  const actionVersion = expectedIsSet ? process.env[EXPECTED_VERSION_ENV] : null;
  if (expectedIsSet && !FOUR_PART_VERSION.test(actionVersion)) {
    throw new Error(EXPECTED_VERSION_ENV + ' must be an exact four-part version; got ' +
      JSON.stringify(actionVersion));
  }
  const expectedVersion = expectedIsSet ? actionVersion : match[2];
  if (match[2] !== expectedVersion) {
    throw new Error('selected browser CLI version ' + match[2] +
      ' does not equal expected action version ' + expectedVersion);
  }
  return {
    configuredPath: CHROME_BIN,
    realPath: fs.realpathSync(CHROME_BIN),
    invocation,
    cliProduct: match[1],
    version: match[2],
    expectedVersion,
    expectedVersionSource: expectedIsSet ? EXPECTED_VERSION_ENV : 'anchored-cli',
    cdp: null,
    selfCheck: null,
  };
}

function verifyCDPBrowserIdentity(value, browser, label) {
  if (!value || value.product !== 'Chrome/' + browser.expectedVersion) {
    throw new Error(label + '.product: got ' + JSON.stringify(value && value.product) +
      ', want ' + JSON.stringify('Chrome/' + browser.expectedVersion));
  }
  for (const field of ['protocolVersion', 'revision']) {
    if (typeof value[field] !== 'string' || value[field].trim() === '') {
      throw new Error(label + '.' + field + ' must be non-empty');
    }
  }
}

function browserIdentitySelfCheck(browser) {
  const wrongProduct = { product: 'NotChrome/' + browser.expectedVersion,
    protocolVersion: '1.3', revision: '@adversarial-mutation' };
  let rejection = '';
  try { verifyCDPBrowserIdentity(wrongProduct, browser, 'wrongSameVersionCDP'); }
  catch (error) { rejection = String(error && error.message || error); }
  if (!rejection.includes('wrongSameVersionCDP.product')) {
    throw new Error('same-version wrong CDP product was not rejected exactly: ' + rejection);
  }
  return { mutation: 'wrong-product-same-version', product: wrongProduct.product,
    rejected: true, rejection };
}

function expr(op, value, type) {
  return { op, operands: null, value: String(value), type };
}

function program(name, engineNodes, exprs) {
  return JSON.stringify({
    name,
    props: [],
    nodes: [],
    root: 0,
    exprs,
    signals: [],
    computeds: [],
    handlers: [],
    static_mask: [],
    engineNodes,
  });
}

// Generation A owns object 0. Generation B owns camera 0, mesh 1, and light
// 2. If A applies after B, object key "0" appears in B's object map.
const PROGRAM_A = program('AdapterStaleA', [
  {
    kind: 'mesh', geometry: 'box', material: 'flat', static: false,
    props: { x: 0, y: 1, z: 2, width: 3, height: 4, depth: 5, color: 6 },
  },
], [
  expr(2, -2, 2), expr(2, 0, 2), expr(2, 0, 2),
  expr(2, 1, 2), expr(2, 1, 2), expr(2, 1, 2), expr(0, '#ff3355', 0),
]);

const PROGRAM_B = program('AdapterWinnerB', [
  { kind: 'camera', props: { z: 0, fov: 1 } },
  {
    kind: 'mesh', geometry: 'box', material: 'flat', static: false,
    props: { x: 2, y: 3, z: 4, width: 5, height: 6, depth: 7, color: 8 },
  },
  {
    kind: 'light', static: false,
    props: { kind: 9, intensity: 10, directionX: 11, directionY: 12, directionZ: 13 },
  },
], [
  expr(2, 6, 2), expr(2, 55, 2),
  expr(2, 0, 2), expr(2, 0, 2), expr(2, 0, 2),
  expr(2, 2.4, 2), expr(2, 2.4, 2), expr(2, 2.4, 2), expr(0, '#8de1ff', 0),
  expr(0, 'directional', 0), expr(2, 1.2, 2),
  expr(2, -0.4, 2), expr(2, -0.7, 2), expr(2, -1, 2),
]);

const ASSETS = new Map([
  ['/bootstrap.js', path.join(REPO, 'client', 'js', 'bootstrap.js')],
  ['/gosx/bootstrap-feature-scene3d-webgl.js', path.join(REPO, 'client', 'js', 'bootstrap-feature-scene3d-webgl.js')],
  ['/gosx/bootstrap-feature-scene3d-webgpu.js', path.join(REPO, 'client', 'js', 'bootstrap-feature-scene3d-webgpu.js')],
  ['/gosx/bootstrap-feature-scene3d-command.js', path.join(REPO, 'client', 'js', 'bootstrap-feature-scene3d-command.js')],
  ['/gosx/bootstrap-feature-scene3d-hydrate.js', path.join(REPO, 'client', 'js', 'bootstrap-feature-scene3d-hydrate.js')],
]);
for (const asset of ASSETS.values()) {
  if (!fs.existsSync(asset)) {
    console.error('missing generated browser asset: ' + asset);
    process.exit(2);
  }
}

const scratch = fs.mkdtempSync(path.join(os.tmpdir(), 'gosx-adapter-hydrate-'));
const wasmPath = path.join(scratch, 'client.wasm');
let wasmExecPath = '';
let runtimeArtifact = null;
let hubGate = null;

try {
  const goRoot = execFileSync('go', ['env', 'GOROOT'], {
    cwd: REPO, encoding: 'utf8', timeout: 15000,
  }).trim();
  for (const candidate of [
    path.join(goRoot, 'lib', 'wasm', 'wasm_exec.js'),
    path.join(goRoot, 'misc', 'wasm', 'wasm_exec.js'),
  ]) {
    if (fs.existsSync(candidate)) { wasmExecPath = candidate; break; }
  }
  if (!wasmExecPath) throw new Error('version-matched wasm_exec.js not found under GOROOT');

  const hubCommand = ['test', './hub/scene3d', '-run',
    '^(TestDiffScenePreservesOrdinaryAndEmptyBehavior|TestDiffSceneRejectsMixedDiffAtomically)$',
    '-count=1'];
  const hubOutput = execFileSync('go', hubCommand, {
    cwd: REPO, encoding: 'utf8', timeout: 120000,
  });
  hubGate = {
    command: 'go ' + hubCommand.join(' '),
    ordinaryRoundTrip: true,
    remountAtomicReject: true,
    output: hubOutput.trim(),
  };

  execFileSync('go', ['build', '-o', wasmPath, './client/wasm'], {
    cwd: REPO,
    env: Object.assign({}, process.env, { GOOS: 'js', GOARCH: 'wasm', CGO_ENABLED: '0' }),
    timeout: BUILD_MS,
  });
  const bytes = fs.readFileSync(wasmPath);
  if (bytes.length < 64) throw new Error('built WASM runtime is empty or suspiciously small');
  runtimeArtifact = {
    source: 'GOOS=js GOARCH=wasm CGO_ENABLED=0 go build -o <scratch>/client.wasm ./client/wasm',
    bytes: bytes.length,
    sha256: crypto.createHash('sha256').update(bytes).digest('hex'),
    wasmExec: wasmExecPath,
  };
} catch (e) {
  try { fs.rmSync(scratch, { recursive: true, force: true }); } catch (_cleanupError) {}
  console.error('adapter proof setup failed: ' + (e && e.message ? e.message : e));
  process.exit(2);
}

function manifestFor(c) {
  return JSON.stringify({
    runtime: { path: '/runtime.wasm' },
    engines: [{
      id: c.engine,
      component: 'GoSXScene3D',
      kind: 'surface',
      mountId: c.mount,
      runtime: 'shared',
      programRef: '/program/' + c.name + '.json',
      props: {
        width: W,
        height: H,
        responsive: false,
        maxDevicePixelRatio: 1,
        background: '#08151f',
        forceWebGL: !c.webgpu,
        requireWebGL: !c.webgpu,
        preferWebGPU: Boolean(c.webgpu),
      },
    }],
  });
}

const BOOT = `
window.__adapterBootErrors = [];
window.__adapterRuntimeReady = false;
window.__adapterRuntimeExited = false;
window.__adapterRuntimeReadyCalls = 0;
window.__adapterHydrates = [];
window.__adapterDisposeCalls = [];
window.__adapterFirstPending = false;
window.__adapterSSR = document.querySelector('[data-adapter-ssr]');
window.__gosx_runtime_ready = function () { window.__adapterRuntimeReadyCalls += 1; };
(async function () {
  try {
    if (typeof Go !== 'function') throw new Error('wasm_exec.js did not publish Go');
    var go = new Go();
    var response = await fetch('/runtime.wasm');
    if (!response.ok) throw new Error('runtime.wasm HTTP ' + response.status);
    var bytes = await response.arrayBuffer();
    var built = await WebAssembly.instantiate(bytes, go.importObject);
    var runPromise = go.run(built.instance);
    runPromise.then(function () { window.__adapterRuntimeExited = true; }, function (error) {
      window.__adapterBootErrors.push('go.run rejected: ' + String(error && (error.stack || error.message) || error));
    });
    var deadline = Date.now() + 20000;
    while (typeof window.__gosx_hydrate !== 'function' ||
           typeof window.__gosx_engine_dispose !== 'function' ||
           typeof window.__gosx_render_engine !== 'function') {
      if (Date.now() > deadline) throw new Error('real Go runtime exports were not ready in time');
      await new Promise(function (resolve) { setTimeout(resolve, 20); });
    }
    var abi = window.__gosx && window.__gosx.runtime && window.__gosx.runtime.abi;
    window.__adapterHandshake = abi && typeof abi.handshake === 'function' ? abi.handshake() : null;
    var realHydrate = window.__gosx_hydrate;
    var realDispose = window.__gosx_engine_dispose;
    window.__adapterRealHydrate = realHydrate;
    window.__adapterRealDispose = realDispose;
    window.__gosx_hydrate = function () {
      var args = Array.prototype.slice.call(arguments);
      var output = realHydrate.apply(this, args);
      var generation = window.__adapterHydrates.length + 1;
      var envelope = null;
      try { envelope = JSON.parse(JSON.stringify(output)); } catch (_copyError) {}
      var parsedProgram = null;
      try { parsedProgram = JSON.parse(String(args[4] || '')); } catch (_programError) {}
      window.__adapterHydrates.push({
        generation: generation,
        argCount: args.length,
        surfaceKind: args[0],
        targetId: args[1],
        component: args[2],
        propsJSON: args[3],
        programName: parsedProgram && parsedProgram.name || '',
        programBytes: String(args[4] || '').length,
        format: args[5],
        envelope: envelope
      });
      if (generation === 1) {
        window.__adapterFirstPending = true;
        return new Promise(function (resolve) {
          window.__adapterReleaseFirst = function () {
            window.__adapterFirstPending = false;
            resolve(output);
          };
        });
      }
      return output;
    };
    window.__gosx_engine_dispose = function () {
      window.__adapterDisposeCalls.push(Array.prototype.slice.call(arguments));
      return realDispose.apply(this, arguments);
    };
    window.__adapterRuntimeReady = true;
    var script = document.createElement('script');
    script.src = '/bootstrap.js';
    script.onerror = function () { window.__adapterBootErrors.push('bootstrap.js failed to load'); };
    document.body.appendChild(script);
  } catch (error) {
    window.__adapterBootErrors.push(String(error && (error.stack || error.message) || error));
  }
})();
`;

function htmlFor(c) {
  return '<!doctype html><html><head><meta charset="utf-8">' +
    '<link rel="icon" href="data:,"><style>html,body{margin:0;background:#08151f;overflow:hidden}</style>' +
    '</head><body><div id="' + c.mount + '" width="' + W + '" height="' + H + '">' +
    '<span data-adapter-ssr="true">server fallback</span></div>' +
    '<script type="application/json" id="gosx-manifest">' + manifestFor(c) + '</script>' +
    '<script src="/wasm_exec.js"></script><script>' + BOOT + '</script></body></html>';
}

const programRequests = new Map();
const unexpectedRequests = [];
const notFound = [];
const networkFailures = [];
const intentionalNoContent = [];
const networkRequests = new Map();
let BASE = '';

function requestAllowed(method, pathname, search) {
  if (search !== '') return false;
  if (method === 'POST') return pathname === '/_gosx/client-events';
  if (method !== 'GET') return false;
  if (pathname === '/' || pathname === '/runtime.wasm' || pathname === '/wasm_exec.js') return true;
  if (pathname === '/case/gl' || pathname === '/case/wg') return true;
  if (pathname === '/program/gl.json' || pathname === '/program/wg.json') return true;
  return ASSETS.has(pathname);
}

function sendResponse(res, status, body, type) {
  res.writeHead(status, { 'content-type': type, 'content-length': body.length });
  res.end(body);
}

const server = http.createServer((req, res) => {
  const method = String(req.method || 'GET').toUpperCase();
  const parsed = new URL(req.url || '/', 'http://127.0.0.1');
  if (!requestAllowed(method, parsed.pathname, parsed.search)) {
    const detail = method + ' ' + parsed.pathname + parsed.search;
    unexpectedRequests.push(detail);
    notFound.push(detail);
    fail('unexpected browser request: ' + detail);
    req.resume();
    sendResponse(res, 404, Buffer.from('unexpected request'), 'text/plain');
    return;
  }
  if (method === 'POST') {
    req.once('error', (error) => fail('client-events request failed: ' + error.message));
    req.once('end', () => { res.writeHead(204); res.end(); });
    req.resume();
    return;
  }
  if (parsed.pathname === '/') {
    sendResponse(res, 200, Buffer.from('<!doctype html><link rel="icon" href="data:,"><title>adapter proof origin</title>'), 'text/html');
    return;
  }
  const foundCase = CASES.find((c) => parsed.pathname === '/case/' + c.name);
  if (foundCase) {
    sendResponse(res, 200, Buffer.from(htmlFor(foundCase)), 'text/html');
    return;
  }
  if (parsed.pathname === '/runtime.wasm') {
    sendResponse(res, 200, fs.readFileSync(wasmPath), 'application/wasm');
    return;
  }
  if (parsed.pathname === '/wasm_exec.js') {
    sendResponse(res, 200, fs.readFileSync(wasmExecPath), 'text/javascript');
    return;
  }
  if (parsed.pathname.startsWith('/program/')) {
    const count = (programRequests.get(parsed.pathname) || 0) + 1;
    programRequests.set(parsed.pathname, count);
    sendResponse(res, 200, Buffer.from(count === 1 ? PROGRAM_A : PROGRAM_B), 'application/json');
    return;
  }
  try {
    sendResponse(res, 200, fs.readFileSync(ASSETS.get(parsed.pathname)), 'text/javascript');
  } catch (e) {
    fail('allowlisted browser asset unavailable: ' + parsed.pathname + ': ' + e.message);
    sendResponse(res, 500, Buffer.from('asset unavailable'), 'text/plain');
  }
});

let ws = null;
let chrome = null;
let profile = null;
let msgID = 0;
const pending = new Map();
const listeners = [];

function cdpSend(method, params, sessionId, timeoutMs) {
  if (!ws) return Promise.reject(new Error('CDP connection closed'));
  const id = ++msgID;
  return new Promise((resolve, reject) => {
    const timer = setTimeout(() => {
      pending.delete(id);
      reject(new Error('CDP timeout: ' + method));
    }, timeoutMs || STEP_MS);
    pending.set(id, { resolve, reject, timer });
    ws.send(JSON.stringify(Object.assign({ id, method }, params ? { params } : {},
      sessionId ? { sessionId } : {})));
  });
}

function waitForEvent(name, timeoutMs) {
  return new Promise((resolve, reject) => {
    const entry = { name, resolve, timer: setTimeout(() => {
      const index = listeners.indexOf(entry);
      if (index >= 0) listeners.splice(index, 1);
      reject(new Error('event timeout: ' + name));
    }, timeoutMs || STEP_MS) };
    listeners.push(entry);
  });
}

function networkFail(message) {
  networkFailures.push(message);
  fail('network failure: ' + message);
}

function recordWarning(message, source) {
  const entry = {
    message: String(message),
    source: source || 'unknown',
    caseName: currentCaseName || '',
    phase: currentCasePhase || '',
    atMS: Date.now(),
  };
  warnings.push(entry.message);
  warningOccurrences.push(entry);
}

function inspectNetwork(rawURL, method) {
  let parsed;
  try { parsed = new URL(rawURL); } catch (_error) {
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
    networkFail(String(method || 'GET') + ' outside exact allowlist: ' + rawURL);
  }
}

function dispatch(raw) {
  let message;
  try { message = JSON.parse(raw); } catch (_error) { return; }
  if (message.id && pending.has(message.id)) {
    const record = pending.get(message.id);
    pending.delete(message.id);
    clearTimeout(record.timer);
    if (message.error) record.reject(new Error(message.error.message));
    else if (message.result && message.result.exceptionDetails) {
      const details = message.result.exceptionDetails;
      record.reject(new Error('Runtime.evaluate exception: ' +
        ((details.exception && details.exception.description) || details.text)));
    } else record.resolve(message.result);
    return;
  }
  if (!message.method) return;
  for (let index = listeners.length - 1; index >= 0; index -= 1) {
    if (listeners[index].name === message.method) {
      const entry = listeners[index];
      clearTimeout(entry.timer);
      listeners.splice(index, 1);
      entry.resolve(message.params || {});
    }
  }
  const params = message.params || {};
  if (message.method === 'Runtime.consoleAPICalled' && params.args) {
    const text = params.args.map((arg) => arg.value !== undefined ? String(arg.value) : (arg.description || '')).join(' ');
    if (params.type === 'error') errors.push('console.error: ' + text);
    else if (params.type === 'warning') recordWarning('console.warning: ' + text, 'Runtime.consoleAPICalled');
  } else if (message.method === 'Runtime.exceptionThrown' && params.exceptionDetails) {
    errors.push('page exception: ' + ((params.exceptionDetails.exception &&
      params.exceptionDetails.exception.description) || params.exceptionDetails.text));
  } else if (message.method === 'Network.requestWillBeSent' && params.request) {
    networkRequests.set(params.requestId, { method: params.request.method, url: params.request.url });
    inspectNetwork(params.request.url, params.request.method);
  } else if (message.method === 'Network.responseReceived' && params.response) {
    const request = networkRequests.get(params.requestId);
    if (request) request.responseStatus = Number(params.response.status);
    inspectNetwork(params.response.url, request ? request.method : 'GET');
    if (Number(params.response.status) >= 400) networkFail('HTTP ' + params.response.status + ' for ' + params.response.url);
  } else if (message.method === 'Network.loadingFailed') {
    const request = networkRequests.get(params.requestId);
    let allowedAbort = false;
    if (request && request.method === 'POST' && request.responseStatus === 204 &&
        params.errorText === 'net::ERR_ABORTED' && params.canceled === true) {
      try {
        const parsed = new URL(request.url);
        allowedAbort = parsed.origin === BASE && parsed.pathname === '/_gosx/client-events' && parsed.search === '';
      } catch (_error) {}
    }
    if (allowedAbort) intentionalNoContent.push({ method: 'POST', path: '/_gosx/client-events', status: 204 });
    else networkFail('loadingFailed ' + (request ? request.method + ' ' + request.url : params.requestId) +
      ': ' + (params.errorText || 'unknown') + (params.canceled ? ' (canceled)' : ''));
    networkRequests.delete(params.requestId);
  } else if (message.method === 'Network.loadingFinished') {
    networkRequests.delete(params.requestId);
  } else if (message.method === 'Log.entryAdded' && params.entry) {
    if (params.entry.level === 'error') errors.push('browser log error: ' + params.entry.text);
    else if (params.entry.level === 'warning') recordWarning('browser log warning: ' + params.entry.text, 'Log.entryAdded');
  }
}

async function evalSend(send, expression, extra) {
  const response = await send('Runtime.evaluate', Object.assign({ expression, returnByValue: true }, extra || {}));
  return response && response.result && response.result.value;
}

async function poll(send, expression, label, timeoutMs) {
  const deadline = Date.now() + (timeoutMs || MOUNT_MS);
  let value = null;
  while (Date.now() < deadline) {
    value = await evalSend(send, expression);
    if (value && value.terminal === true) {
      const classification = value.classification || value.terminalClassification || 'terminal-predicate';
      const error = new Error('terminal waiting for ' + label + ': ' + classification +
        ' (last=' + JSON.stringify(value) + ')');
      error.lastPredicate = value;
      error.phase = label;
      error.classification = classification;
      error.terminal = true;
      throw error;
    }
    if (value && (value.ready === undefined || value.ready === true)) return value;
    await sleep(50);
  }
  const error = new Error('timeout waiting for ' + label + ' (last=' + JSON.stringify(value) + ')');
  error.lastPredicate = value;
  error.phase = label;
  throw error;
}

const PRELOAD = `
window.__adapterGLDraws = 0;
window.__adapterGLContext = '';
window.__adapterWGPasses = 0;
window.__adapterWGColorPasses = 0;
window.__adapterWGDraws = 0;
window.__adapterWGSubmits = 0;
window.__adapterWGSubmitFailures = 0;
window.__adapterWGCompletedSubmits = 0;
window.__adapterWGFailedSubmits = 0;
window.__adapterWGCompletedColorPasses = 0;
window.__adapterWGSubmittedColorPasses = 0;
window.__adapterWGQueueFenceCalls = 0;
window.__adapterWGCompletionFencePending = false;
window.__adapterWGCompletionFenceError = '';
window.__adapterWGCompletionFencePromise = null;
window.__adapterWGEncoders = 0;
window.__adapterWGConfigureSuccesses = 0;
window.__adapterWGConfigureFailures = 0;
window.__adapterWGCurrentTextureSuccesses = 0;
window.__adapterWGCurrentTextureFailures = 0;
window.__adapterWGConfigureError = '';
window.__adapterWGCurrentTextureError = '';
window.__adapterWGSubmitError = '';
window.__adapterWGLatestQueue = null;
window.__adapterWGLatestEncoderDevice = null;
window.__adapterWGLatestConfiguredDevice = null;
(function () {
  function wrapGL(proto) {
    if (!proto) return;
    ['drawArrays', 'drawElements'].forEach(function (name) {
      var original = proto[name];
      if (!original) return;
      proto[name] = function () {
        window.__adapterGLDraws += 1;
        window.__adapterGLContext = this instanceof WebGL2RenderingContext ? 'webgl2' : 'webgl';
        return original.apply(this, arguments);
      };
    });
  }
  wrapGL(typeof WebGLRenderingContext !== 'undefined' ? WebGLRenderingContext.prototype : null);
  wrapGL(typeof WebGL2RenderingContext !== 'undefined' ? WebGL2RenderingContext.prototype : null);
  if (typeof GPUCommandEncoder !== 'undefined' && GPUCommandEncoder.prototype.beginRenderPass) {
    var originalPass = GPUCommandEncoder.prototype.beginRenderPass;
    GPUCommandEncoder.prototype.beginRenderPass = function (descriptor) {
      window.__adapterWGPasses += 1;
      // The renderer creates one depth-only pass while initializing its dummy
      // shadow resource. It proves neither a scene color pass nor a frame a
      // compositor can present, so keep it distinct from color submissions.
      var colors = descriptor && descriptor.colorAttachments;
      if (colors && typeof colors.length === 'number') {
        for (var index = 0; index < colors.length; index += 1) {
          if (colors[index] && colors[index].view) {
            window.__adapterWGColorPasses += 1;
            break;
          }
        }
      }
      return originalPass.apply(this, arguments);
    };
  }
  if (typeof GPURenderPassEncoder !== 'undefined' && GPURenderPassEncoder.prototype) {
    ['draw', 'drawIndexed'].forEach(function (name) {
      var originalDraw = GPURenderPassEncoder.prototype[name];
      if (!originalDraw) return;
      GPURenderPassEncoder.prototype[name] = function () {
        window.__adapterWGDraws += 1;
        return originalDraw.apply(this, arguments);
      };
    });
  }
  if (typeof GPUQueue !== 'undefined' && GPUQueue.prototype.submit) {
    var originalSubmit = GPUQueue.prototype.submit;
    GPUQueue.prototype.submit = function () {
      window.__adapterWGLatestQueue = this;
      var result;
      try {
        result = originalSubmit.apply(this, arguments);
      } catch (error) {
        window.__adapterWGSubmitFailures += 1;
        window.__adapterWGSubmitError = String(error && (error.message || error) || 'submit failed').slice(0, 240);
        throw error;
      }
      window.__adapterWGSubmits += 1;
      window.__adapterWGSubmittedColorPasses = Math.max(
        window.__adapterWGSubmittedColorPasses,
        window.__adapterWGColorPasses
      );
      return result;
    };
    window.__adapterWGAwaitSubmittedWork = function () {
      var queue = window.__adapterWGLatestQueue;
      if (!queue || typeof queue.onSubmittedWorkDone !== 'function') {
        return Promise.resolve({ ready: false, reason: 'queue-unavailable' });
      }
      if (window.__adapterWGCompletionFencePromise) return window.__adapterWGCompletionFencePromise;
      var targetSubmits = window.__adapterWGSubmits;
      var targetColorPasses = window.__adapterWGSubmittedColorPasses;
      window.__adapterWGQueueFenceCalls += 1;
      window.__adapterWGCompletionFencePending = true;
      window.__adapterWGCompletionFenceError = '';
      var fence;
      try {
        fence = Promise.resolve(queue.onSubmittedWorkDone()).then(function () {
          window.__adapterWGCompletedSubmits = Math.max(window.__adapterWGCompletedSubmits, targetSubmits);
          window.__adapterWGCompletedColorPasses = Math.max(window.__adapterWGCompletedColorPasses, targetColorPasses);
          return { ready: true, targetSubmits: targetSubmits, targetColorPasses: targetColorPasses };
        }, function (error) {
          window.__adapterWGFailedSubmits += 1;
          window.__adapterWGCompletionFenceError = String(error && (error.message || error) || 'queue completion rejected').slice(0, 240);
          throw error;
        });
      } catch (error) {
        window.__adapterWGFailedSubmits += 1;
        window.__adapterWGCompletionFenceError = String(error && (error.message || error) || 'queue completion rejected').slice(0, 240);
        return Promise.reject(error);
      }
      window.__adapterWGCompletionFencePromise = fence.finally(function () {
        window.__adapterWGCompletionFencePending = false;
        window.__adapterWGCompletionFencePromise = null;
      });
      return window.__adapterWGCompletionFencePromise;
    };
  }
  if (typeof GPUDevice !== 'undefined' && GPUDevice.prototype.createCommandEncoder) {
    var originalCreateCommandEncoder = GPUDevice.prototype.createCommandEncoder;
    GPUDevice.prototype.createCommandEncoder = function () {
      window.__adapterWGLatestEncoderDevice = this;
      window.__adapterWGEncoders += 1;
      return originalCreateCommandEncoder.apply(this, arguments);
    };
  }
  if (typeof GPUCanvasContext !== 'undefined' && GPUCanvasContext.prototype) {
    if (GPUCanvasContext.prototype.configure) {
      var originalConfigure = GPUCanvasContext.prototype.configure;
      GPUCanvasContext.prototype.configure = function (descriptor) {
        try {
          var result = originalConfigure.apply(this, arguments);
          window.__adapterWGLatestConfiguredDevice = descriptor && descriptor.device || null;
          window.__adapterWGConfigureSuccesses += 1;
          return result;
        } catch (error) {
          window.__adapterWGConfigureFailures += 1;
          window.__adapterWGConfigureError = String(error && (error.message || error) || 'configure failed').slice(0, 240);
          throw error;
        }
      };
    }
    if (GPUCanvasContext.prototype.getCurrentTexture) {
      var originalGetCurrentTexture = GPUCanvasContext.prototype.getCurrentTexture;
      GPUCanvasContext.prototype.getCurrentTexture = function () {
        try {
          var result = originalGetCurrentTexture.apply(this, arguments);
          window.__adapterWGCurrentTextureSuccesses += 1;
          return result;
        } catch (error) {
          window.__adapterWGCurrentTextureFailures += 1;
          window.__adapterWGCurrentTextureError = String(error && (error.message || error) || 'getCurrentTexture failed').slice(0, 240);
          throw error;
        }
      };
    }
  }
})();
`;

const CAPS_EXPR = `(async function () {
  var canvas = document.createElement('canvas');
  var webgl2 = false;
  var webglIdentity = null;
  try { webgl2 = !!canvas.getContext('webgl2'); } catch (_error) {}
  try {
    var gl = canvas.getContext('webgl2');
    var debug = gl && gl.getExtension && gl.getExtension('WEBGL_debug_renderer_info');
    if (gl && debug) {
      webglIdentity = {
        vendor: gl.getParameter(debug.UNMASKED_VENDOR_WEBGL),
        renderer: gl.getParameter(debug.UNMASKED_RENDERER_WEBGL)
      };
    }
  } catch (_identityError) {}
  var adapter = navigator.gpu && navigator.gpu.requestAdapter ? await navigator.gpu.requestAdapter() : null;
  var webgpuAdapterInfo = null;
  try {
    if (adapter && typeof adapter.requestAdapterInfo === 'function') {
      webgpuAdapterInfo = await adapter.requestAdapterInfo();
    } else if (adapter && adapter.info) {
      webgpuAdapterInfo = adapter.info;
    }
  } catch (infoError) {
    webgpuAdapterInfo = { error: String(infoError && (infoError.message || infoError) || infoError) };
  }
  return { webgl2: webgl2, webgpu: !!adapter, webglIdentity: webglIdentity, webgpuAdapterInfo: webgpuAdapterInfo };
})()`;

function preflightExpr(c) {
  return `(function () {
    var mount = document.getElementById(${JSON.stringify(c.mount)});
    var result = {
      bootErrors: (window.__adapterBootErrors || []).slice(),
      runtimeReady: window.__adapterRuntimeReady === true,
      runtimeExited: window.__adapterRuntimeExited === true,
      handshake: window.__adapterHandshake || null,
      hydrates: (window.__adapterHydrates || []).slice(),
      firstPending: window.__adapterFirstPending === true,
      ssrIdentity: !!(mount && mount.firstChild === window.__adapterSSR),
      childCount: mount ? mount.childNodes.length : -1,
      canvasCount: mount ? mount.querySelectorAll('canvas').length : -1,
      hasState: !!(mount && mount.__gosxScene3DState),
      hasHandle: !!(mount && mount.__gosxScene3DHandle),
      registered: !!(window.__gosx && window.__gosx.engines && window.__gosx.engines.has(${JSON.stringify(c.engine)}))
    };
    return result.firstPending || result.bootErrors.length ? result : null;
  })()`;
}

function winnerExpr(c, remember) {
  return `(function () {
    var mount = document.getElementById(${JSON.stringify(c.mount)});
    var state = mount && mount.__gosxScene3DState;
    var handle = mount && mount.__gosxScene3DHandle;
    var record = window.__gosx && window.__gosx.engines && window.__gosx.engines.get(${JSON.stringify(c.engine)});
    if (!mount || !state || !handle || !record) return null;
    ${remember ? 'window.__adapterWinnerRefs={mount:mount,state:state,handle:handle,canvas:mount.querySelector("canvas"),record:record};' : ''}
    var keys = [];
    if (state.objects && state.objects.forEach) state.objects.forEach(function (_value, key) { keys.push(String(key)); });
    keys.sort();
    var mesh = state.objects && state.objects.get('1');
    return {
      hydrates: (window.__adapterHydrates || []).slice(),
      disposes: (window.__adapterDisposeCalls || []).map(function (args) { return args.slice(); }),
      keys: keys,
      mesh: mesh ? { id: String(mesh.id), x: mesh.x, color: mesh.color, kind: mesh.kind } : null,
      lights: state.lights ? state.lights.size : -1,
      handleReady: handle.__gosxScene3DCommandReady === true,
      registrySameHandle: record.handle === handle,
      mounted: mount.getAttribute('data-gosx-scene3d-mounted'),
      renderer: mount.getAttribute('data-gosx-scene3d-renderer'),
      fallback: mount.getAttribute('data-gosx-scene3d-renderer-fallback'),
      commandReady: mount.getAttribute('data-gosx-scene3d-command-ready'),
      commandRevision: mount.getAttribute('data-gosx-scene3d-command-revision'),
      commandAppliedRevision: mount.getAttribute('data-gosx-scene3d-command-applied-revision'),
      glDraws: window.__adapterGLDraws,
      glContext: window.__adapterGLContext,
      wgPasses: window.__adapterWGPasses,
      wgColorPasses: window.__adapterWGColorPasses,
      wgDraws: window.__adapterWGDraws,
      wgSubmits: window.__adapterWGSubmits,
      wgCompletedSubmits: window.__adapterWGCompletedSubmits,
      wgFailedSubmits: window.__adapterWGFailedSubmits,
      wgCompletedColorPasses: window.__adapterWGCompletedColorPasses,
      webgpuMeshObjects: mount.getAttribute('data-gosx-scene3d-webgpu-mesh-objects'),
      webgpuMeshDrawCalls: mount.getAttribute('data-gosx-scene3d-webgpu-mesh-draw-calls'),
      webgpuMeshViewCulled: mount.getAttribute('data-gosx-scene3d-webgpu-mesh-view-culled'),
      webgpuMeshUndrawable: mount.getAttribute('data-gosx-scene3d-webgpu-mesh-undrawable')
    };
  })()`;
}

function webGPUPresentExpr(c, afterColorPasses, afterCompletedColorPasses, expectedObjectX, requireCompleted) {
  return `(function () {
    var mount = document.getElementById(${JSON.stringify(c.mount)});
    var state = mount && mount.__gosxScene3DState;
    var object = state && state.objects && state.objects.get('1');
    var probe = null;
    try { probe = typeof window.__gosx_scene3d_webgpu_probe === 'function' ? window.__gosx_scene3d_webgpu_probe() : null; } catch (_error) { probe = null; }
    var diagnostics = null;
    try { diagnostics = typeof window.__gosx_scene3d_webgpu_diagnostics === 'function' ? window.__gosx_scene3d_webgpu_diagnostics() : null; } catch (_error) { diagnostics = null; }
    // A scene color pass is distinct from the depth-only dummy-shadow
    // initialization pass. Completion of that color submission is deliberately
    // required before capturing the compositor surface; the pixel assertions
    // below remain the proof that the scene itself is visible.
    var expectedX = ${JSON.stringify(expectedObjectX === undefined ? null : expectedObjectX)};
    var needsCompletion = ${JSON.stringify(requireCompleted !== false)};
    var predicates = {
      mountFound: !!mount,
      rendererWebGPU: !!mount && mount.getAttribute('data-gosx-scene3d-renderer') === 'webgpu',
      mounted: !!mount && mount.getAttribute('data-gosx-scene3d-mounted') === 'true',
      colorPasses: window.__adapterWGColorPasses,
      submittedColorPasses: window.__adapterWGSubmittedColorPasses,
      colorPassBaseline: ${JSON.stringify(afterColorPasses || 0)},
      completedColorPasses: window.__adapterWGCompletedColorPasses,
      completedColorPassBaseline: ${JSON.stringify(afterCompletedColorPasses || 0)},
      completedSubmits: window.__adapterWGCompletedSubmits,
      failedSubmits: window.__adapterWGFailedSubmits,
      queueFenceCalls: window.__adapterWGQueueFenceCalls,
      queueFencePending: window.__adapterWGCompletionFencePending,
      queueFenceError: window.__adapterWGCompletionFenceError,
      fallback: mount && mount.getAttribute('data-gosx-scene3d-renderer-fallback') || '',
      probeLost: !!(probe && probe.lost),
      diagnosticsDeviceLost: !!(diagnostics && diagnostics.deviceLost),
      expectedObjectX: expectedX,
      objectX: object && object.x
    };
    predicates.freshColorPass = predicates.colorPasses > predicates.colorPassBaseline;
    predicates.freshCompletedColorPass = predicates.completedColorPasses > predicates.colorPassBaseline &&
      predicates.completedColorPasses > predicates.completedColorPassBaseline;
    predicates.commandState = expectedX === null || predicates.objectX === expectedX;
    predicates.deviceLost = predicates.fallback === 'webgpu-device-lost' || predicates.probeLost || predicates.diagnosticsDeviceLost;
    var ready = predicates.mountFound && predicates.rendererWebGPU && predicates.mounted &&
      predicates.freshColorPass && predicates.failedSubmits === 0 && predicates.commandState && !predicates.deviceLost &&
      (!needsCompletion || (predicates.freshCompletedColorPass && predicates.completedSubmits > 0));
    var classification = predicates.deviceLost && predicates.colorPasses === 0 ? 'device-lost-before-color-pass' :
      (predicates.deviceLost ? 'device-lost-after-color-pass' : 'predicate-not-ready');
    var terminal = !!(predicates.mountFound && predicates.deviceLost);
    return {
      ready: ready,
      terminal: terminal,
      classification: classification,
      predicates: predicates,
      passes: window.__adapterWGPasses,
      colorPasses: window.__adapterWGColorPasses,
      submittedColorPasses: window.__adapterWGSubmittedColorPasses,
      draws: window.__adapterWGDraws,
      submits: window.__adapterWGSubmits,
      completedSubmits: window.__adapterWGCompletedSubmits,
      queueFenceCalls: window.__adapterWGQueueFenceCalls,
      queueFencePending: window.__adapterWGCompletionFencePending,
      queueFenceError: window.__adapterWGCompletionFenceError,
      objectX: object && object.x,
      commandRevision: mount && mount.getAttribute('data-gosx-scene3d-command-revision'),
      commandAppliedRevision: mount && mount.getAttribute('data-gosx-scene3d-command-applied-revision'),
      meshObjects: mount && mount.getAttribute('data-gosx-scene3d-webgpu-mesh-objects'),
      meshDrawCalls: mount && mount.getAttribute('data-gosx-scene3d-webgpu-mesh-draw-calls'),
      meshViewCulled: mount && mount.getAttribute('data-gosx-scene3d-webgpu-mesh-view-culled'),
      meshUndrawable: mount && mount.getAttribute('data-gosx-scene3d-webgpu-mesh-undrawable')
    };
  })()`;
}

function webGPUQueueFenceExpr(c, label) {
  const proofLabel = label || c.name || 'scene';
  return `(async function () {
    if (typeof window.__adapterWGAwaitSubmittedWork !== 'function') {
      throw new Error('WebGPU queue fence helper unavailable for ' + ${JSON.stringify(proofLabel)});
    }
    return await window.__adapterWGAwaitSubmittedWork();
  })()`;
}

async function waitForWebGPUPresentation(send, c, label, afterColorPasses, afterCompletedColorPasses, expectedObjectX) {
  await poll(
    send, webGPUPresentExpr(c, afterColorPasses || 0, afterCompletedColorPasses || 0, expectedObjectX, false),
    label + ' submitted scene color frame'
  );
  const queueFence = await evalSend(send, webGPUQueueFenceExpr(c, label), { awaitPromise: true });
  const evidence = await poll(
    send, webGPUPresentExpr(c, afterColorPasses || 0, afterCompletedColorPasses || 0, expectedObjectX, true),
    label + ' completed scene color frame'
  );
  evidence.queueFence = queueFence;
  // GPU completion precedes presentation; allow the browser compositor to
  // consume that finished canvas texture before Page.captureScreenshot.
  await evalSend(send, settleFramesExpr(2), { awaitPromise: true });
  return evidence;
}

function webGLFallbackPresentExpr(c, fallbackKind, afterGLDraws, expectedObjectX, requireRevision) {
  return `(function () {
    var mount = document.getElementById(${JSON.stringify(c.mount)});
    var state = mount && mount.__gosxScene3DState;
    var object = state && state.objects && state.objects.get('1');
    var revision = mount && mount.getAttribute('data-gosx-scene3d-command-revision');
    var appliedRevision = mount && mount.getAttribute('data-gosx-scene3d-command-applied-revision');
    var expectedX = ${JSON.stringify(expectedObjectX === undefined ? null : expectedObjectX)};
    var needsRevision = ${JSON.stringify(requireRevision === true)};
    var predicates = {
      mountFound: !!mount,
      rendererWebGL: !!mount && mount.getAttribute('data-gosx-scene3d-renderer') === 'webgl',
      rendererCanvas2D: !!mount && mount.getAttribute('data-gosx-scene3d-renderer') === 'canvas2d',
      fallback: mount && mount.getAttribute('data-gosx-scene3d-renderer-fallback') || '',
      expectedFallback: ${JSON.stringify(fallbackKind)},
      mounted: !!mount && mount.getAttribute('data-gosx-scene3d-mounted') === 'true',
      commandReady: !!mount && mount.getAttribute('data-gosx-scene3d-command-ready') === 'true',
      commandRevision: revision,
      commandAppliedRevision: appliedRevision,
      glDraws: window.__adapterGLDraws,
      glDrawBaseline: ${JSON.stringify(afterGLDraws || 0)},
      glContext: window.__adapterGLContext,
      expectedObjectX: expectedX,
      objectX: object && object.x
    };
    predicates.freshGLDraw = predicates.glDraws > predicates.glDrawBaseline;
    predicates.commandState = expectedX === null || predicates.objectX === expectedX;
    predicates.revisionState = !needsRevision || (!!revision && revision === appliedRevision);
    var ready = predicates.mountFound && predicates.rendererWebGL && !predicates.rendererCanvas2D &&
      predicates.fallback === predicates.expectedFallback && predicates.mounted && predicates.commandReady &&
      predicates.freshGLDraw && predicates.glContext === 'webgl2' && predicates.commandState && predicates.revisionState;
    return {
      ready: ready,
      predicates: predicates,
      renderer: mount && mount.getAttribute('data-gosx-scene3d-renderer'),
      fallback: predicates.fallback,
      mounted: mount && mount.getAttribute('data-gosx-scene3d-mounted'),
      glDraws: window.__adapterGLDraws,
      glContext: window.__adapterGLContext,
      objectX: object && object.x,
      commandRevision: revision,
      commandAppliedRevision: appliedRevision
    };
  })()`;
}

async function waitForWebGLFallbackPresentation(send, c, label, fallbackKind, afterGLDraws, expectedObjectX, requireRevision) {
  const evidence = await poll(
    send, webGLFallbackPresentExpr(c, fallbackKind, afterGLDraws || 0, expectedObjectX, requireRevision),
    label + ' typed WebGL fallback frame'
  );
  // Let the compositor consume the proven fresh WebGL draw before screenshot capture.
  await evalSend(send, settleFramesExpr(2), { awaitPromise: true });
  return evidence;
}

function boundedValue(value, depth, seen) {
  const level = depth || 0;
  if (value === null || value === undefined || typeof value === 'boolean' || typeof value === 'number') return value;
  if (typeof value === 'string') return value.length > 480 ? value.slice(0, 480) + '…' : value;
  if (typeof value === 'function') return '[function]';
  if (level >= 4) return '[truncated]';
  if (typeof value === 'object') {
    const visited = seen || [];
    if (visited.includes(value)) return '[cycle]';
    let keys;
    try { keys = Object.getOwnPropertyNames(value).sort().slice(0, 48); }
    catch (_error) { return '[uninspectable]'; }
    const out = Array.isArray(value) ? [] : {};
    const nextSeen = visited.concat([value]);
    for (const key of keys) {
      if (key === 'length') continue;
      let descriptor;
      try { descriptor = Object.getOwnPropertyDescriptor(value, key); }
      catch (_error) { out[key] = '[unreadable]'; continue; }
      if (!descriptor) continue;
      out[key] = Object.prototype.hasOwnProperty.call(descriptor, 'value')
        ? boundedValue(descriptor.value, level + 1, nextSeen)
        : '[accessor]';
    }
    return out;
  }
  try { return String(value).slice(0, 480); }
  catch (_error) { return '[unstringifiable]'; }
}

function boundedMessages(entries) {
  const out = [];
  const start = Math.max(0, entries.length - 24);
  for (let index = start; index < entries.length; index += 1) out.push(boundedValue(entries[index]));
  return out;
}

function safeOwnDataValue(value, key) {
  if (value === null || value === undefined) return undefined;
  try {
    const descriptor = Object.getOwnPropertyDescriptor(value, key);
    return descriptor && Object.prototype.hasOwnProperty.call(descriptor, 'value') ? descriptor.value : undefined;
  } catch (_error) {
    return undefined;
  }
}

function safeErrorSnapshot(error) {
  return {
    kind: error === null ? 'null' : typeof error,
    name: boundedValue(safeOwnDataValue(error, 'name')),
    message: boundedValue(safeOwnDataValue(error, 'message')),
    stack: boundedValue(safeOwnDataValue(error, 'stack')),
  };
}

function recordDiagnosticFailure(evidence, stage, error) {
  try {
    let failures = safeOwnDataValue(evidence, 'diagnosticFailures');
    if (!Array.isArray(failures)) {
      failures = [];
      evidence.diagnosticFailures = failures;
    }
    if (failures.length >= 8) failures.shift();
    failures.push({ stage, atMS: Date.now(), error: safeErrorSnapshot(error) });
  } catch (_error) {}
}

async function captureDiagnosticBestEffort(evidence, stage, callback) {
  await new Promise((resolve) => {
    let done = false;
    const finish = () => {
      if (done) return;
      done = true;
      clearTimeout(timer);
      resolve();
    };
    const timer = setTimeout(() => {
      recordDiagnosticFailure(evidence, stage + '-timeout', new Error('diagnostic capture exceeded ' + DIAGNOSTIC_CAPTURE_MS + 'ms'));
      finish();
    }, DIAGNOSTIC_CAPTURE_MS);
    Promise.resolve().then(callback).then(finish, (error) => {
      recordDiagnosticFailure(evidence, stage, error);
      finish();
    });
  });
}

function webGPUFailureReceiptExpr(c, phase) {
  return `(function () {
    function bounded(value, depth) {
      var level = depth || 0;
      if (value === null || value === undefined || typeof value === 'boolean' || typeof value === 'number') return value;
      if (typeof value === 'string') return value.length > 480 ? value.slice(0, 480) + '…' : value;
      if (level >= 3) return '[truncated]';
      if (Array.isArray(value)) return value.slice(0, 24).map(function (entry) { return bounded(entry, level + 1); });
      if (typeof value === 'object') {
        var out = {}, keys = [];
        try { keys = Object.keys(value).sort().slice(0, 48); } catch (_error) { return '[uninspectable]'; }
        for (var i = 0; i < keys.length; i += 1) {
          try { out[keys[i]] = bounded(value[keys[i]], level + 1); } catch (_error) { out[keys[i]] = '[unreadable]'; }
        }
        return out;
      }
      return String(value).slice(0, 480);
    }
    var mount = document.getElementById(${JSON.stringify(c.mount)});
    var state = mount && mount.__gosxScene3DState;
    var handle = mount && mount.__gosxScene3DHandle;
    var object = state && state.objects && state.objects.get('1');
    var probe = null;
    try { probe = typeof window.__gosx_scene3d_webgpu_probe === 'function' ? window.__gosx_scene3d_webgpu_probe() : null; } catch (error) { probe = { probeCallError: String(error && (error.message || error) || error) }; }
    var diagnostics = null;
    try { diagnostics = typeof window.__gosx_scene3d_webgpu_diagnostics === 'function' ? window.__gosx_scene3d_webgpu_diagnostics() : null; } catch (error) { diagnostics = { diagnosticsCallError: String(error && (error.message || error) || error) }; }
    var probeDevice = probe && probe.device || null;
    var probeQueue = probeDevice && probeDevice.queue || null;
    var queue = window.__adapterWGLatestQueue || null;
    var encoderDevice = window.__adapterWGLatestEncoderDevice || null;
    var configuredDevice = window.__adapterWGLatestConfiguredDevice || null;
    var wrappers = {
      encoders: window.__adapterWGEncoders || 0,
      passes: window.__adapterWGPasses || 0,
      colorPasses: window.__adapterWGColorPasses || 0,
      submittedColorPasses: window.__adapterWGSubmittedColorPasses || 0,
      draws: window.__adapterWGDraws || 0,
      submits: window.__adapterWGSubmits || 0,
      submitFailures: window.__adapterWGSubmitFailures || 0,
      submitError: window.__adapterWGSubmitError || '',
      completedSubmits: window.__adapterWGCompletedSubmits || 0,
      failedSubmits: window.__adapterWGFailedSubmits || 0,
      completedColorPasses: window.__adapterWGCompletedColorPasses || 0,
      queueFenceCalls: window.__adapterWGQueueFenceCalls || 0,
      queueFencePending: window.__adapterWGCompletionFencePending === true,
      queueFenceError: window.__adapterWGCompletionFenceError || '',
      configureSuccesses: window.__adapterWGConfigureSuccesses || 0,
      configureFailures: window.__adapterWGConfigureFailures || 0,
      configureError: window.__adapterWGConfigureError || '',
      currentTextureSuccesses: window.__adapterWGCurrentTextureSuccesses || 0,
      currentTextureFailures: window.__adapterWGCurrentTextureFailures || 0,
      currentTextureError: window.__adapterWGCurrentTextureError || ''
    };
    var identity = {
      wrappedQueueObserved: !!queue,
      probeDeviceObserved: !!probeDevice,
      probeQueueObserved: !!probeQueue,
      wrappedQueueMatchesProbe: queue && probeQueue ? queue === probeQueue : null,
      encoderDeviceMatchesProbe: encoderDevice && probeDevice ? encoderDevice === probeDevice : null,
      configuredDeviceMatchesProbe: configuredDevice && probeDevice ? configuredDevice === probeDevice : null,
      backendIsWebGPU: !!mount && mount.getAttribute('data-gosx-scene3d-renderer') === 'webgpu'
    };
    var classification = 'predicate-not-ready';
    var fallback = mount && mount.getAttribute('data-gosx-scene3d-renderer-fallback') || '';
    var independentDeviceLoss = !!((probe && probe.lost) ||
      (diagnostics && (diagnostics.deviceLost || diagnostics.deviceLostInfo)));
    var deviceLost = fallback === 'webgpu-device-lost' || independentDeviceLoss;
    if (identity.wrappedQueueMatchesProbe === false || identity.encoderDeviceMatchesProbe === false || identity.configuredDeviceMatchesProbe === false) classification = 'instrumented-queue-or-device-mismatch';
    else if (deviceLost && wrappers.colorPasses === 0) classification = 'device-lost-before-color-pass';
    else if (wrappers.colorPasses === 0) classification = 'no-scene-color-pass';
    else if (wrappers.submitFailures > 0) classification = 'queue-submit-rejected';
    else if (wrappers.failedSubmits > 0) classification = 'queue-completion-rejected';
    else if (deviceLost) classification = 'device-lost-after-color-pass';
    else if (wrappers.completedColorPasses === 0) classification = 'color-submitted-awaiting-queue';
    return bounded({
      capturedAtMS: Date.now(),
      phase: ${JSON.stringify(phase)},
      classification: classification,
      independentDeviceLoss: independentDeviceLoss,
      mount: mount ? {
        mounted: mount.getAttribute('data-gosx-scene3d-mounted'),
        renderer: mount.getAttribute('data-gosx-scene3d-renderer'),
        fallback: mount.getAttribute('data-gosx-scene3d-renderer-fallback'),
        commandReady: mount.getAttribute('data-gosx-scene3d-command-ready'),
        revision: mount.getAttribute('data-gosx-scene3d-command-revision'),
        appliedRevision: mount.getAttribute('data-gosx-scene3d-command-applied-revision'),
        objectX: object && object.x,
        hasState: !!state,
        hasHandle: !!handle,
        stats: mount.__gosxScene3DWebGPUStats || null
      } : null,
      wrappers: wrappers,
      identity: identity,
      factory: {
        error: window.__gosx_scene3d_webgpu_factory_error || '',
        context: window.__gosx_scene3d_webgpu_factory_context || null
      },
      probe: probe ? {
        ready: !!probe.ready,
        error: probe.error || '',
        lost: probe.lost || null,
        retryCount: probe.retryCount || 0,
        lostProbeCount: probe.lostProbeCount || 0,
        warnings: probe.warnings || []
      } : null,
      diagnostics: diagnostics
    });
  })()`;
}

async function captureWebGPUFailureReceipt(send, c, phase) {
  return boundedValue(await evalSend(send, webGPUFailureReceiptExpr(c, phase)));
}

async function rejectedWGOutcomeWithReceipt(send, c, label, before) {
  const rejected = {
    acceptedOutcome: '',
    fallbackKind: before && before.fallback || '',
    fallbackEvidence: null,
  };
  try {
    const receipt = await captureWebGPUFailureReceipt(send, c, currentCasePhase || label);
    if (receiptHasIndependentDeviceLoss(receipt) ||
        /^device-lost-/.test(String(receipt && receipt.classification || ''))) {
      rejected.webgpuFailureReceipt = receipt;
    }
  } catch (receiptError) {
    rejected.webgpuFailureReceiptError = safeErrorSnapshot(receiptError);
  }
  return rejected;
}

async function retainCaseEvidence(sink, evidence, work, onFailure) {
  try {
    return await work();
  } catch (error) {
    await captureDiagnosticBestEffort(evidence, 'case-failure-receipt', () => onFailure(error));
    throw error;
  } finally {
    try {
      evidence.finishedAt = new Date().toISOString();
      evidence.finishedAtMS = Date.now();
      sink.push(evidence);
    } catch (diagnosticError) {
      recordDiagnosticFailure(evidence, 'case-evidence-append', diagnosticError);
    }
  }
}

function identityExpr(c) {
  return `(function () {
    var refs = window.__adapterWinnerRefs;
    var mount = document.getElementById(${JSON.stringify(c.mount)});
    var record = window.__gosx && window.__gosx.engines && window.__gosx.engines.get(${JSON.stringify(c.engine)});
    var state = mount && mount.__gosxScene3DState;
    var keys = [];
    if (state && state.objects && state.objects.forEach) state.objects.forEach(function (_value, key) { keys.push(String(key)); });
    keys.sort();
    return refs && {
      sameMount: refs.mount === mount,
      sameState: refs.state === state,
      sameHandle: refs.handle === (mount && mount.__gosxScene3DHandle),
      sameCanvas: refs.canvas === (mount && mount.querySelector('canvas')),
      sameRecord: refs.record === record,
      keys: keys,
      disposes: (window.__adapterDisposeCalls || []).map(function (args) { return args.slice(); })
    };
  })()`;
}

function hubCommandExpr(c) {
  return `(async function () {
    var mount = document.getElementById(${JSON.stringify(c.mount)});
    var state = mount && mount.__gosxScene3DState;
    var handle = mount && mount.__gosxScene3DHandle;
    var before = state && state.objects && state.objects.get('1');
    var beforeX = before && before.x;
    var result = await window.__gosx.scene3d.dispatchCommands(
      ${JSON.stringify(c.engine)},
      [{kind:2,objectId:'1',data:{x:1.25}}],
      {engineID:${JSON.stringify(c.engine)},timeoutMS:5000}
    );
    var deadline = Date.now() + 5000;
    var after = null;
    var revision = null;
    var appliedRevision = null;
    var commandApplied = false;
    // dispatchCommands normally applies synchronously, but the proof has to
    // tolerate an asynchronous engine. Once the command state is observed,
    // sample the color-pass baseline in this same task so an old-state rAF
    // cannot create the pass that satisfies the post-command freshness gate.
    while (Date.now() < deadline) {
      after = state && state.objects && state.objects.get('1');
      revision = mount.getAttribute('data-gosx-scene3d-command-revision');
      appliedRevision = mount.getAttribute('data-gosx-scene3d-command-applied-revision');
      if (after && after.x === 1.25 && state === mount.__gosxScene3DState &&
          handle === mount.__gosxScene3DHandle && revision && revision === appliedRevision) {
        commandApplied = true;
        break;
      }
      await new Promise(function(resolve) { setTimeout(resolve, 25); });
    }
    var colorPassBaseline = window.__adapterWGColorPasses;
    var completedColorPassBaseline = window.__adapterWGCompletedColorPasses;
    return {
      result: result,
      beforeX: beforeX,
      afterX: after && after.x,
      sameState: state === mount.__gosxScene3DState,
      sameHandle: handle === mount.__gosxScene3DHandle,
      revision: revision,
      appliedRevision: appliedRevision,
      commandApplied: commandApplied,
      glDrawBaseline: window.__adapterGLDraws,
      webgpuColorPassBaseline: colorPassBaseline,
      webgpuCompletedColorPassBaseline: completedColorPassBaseline
    };
  })()`;
}

function settleFramesExpr(count) {
  return `(function () { return new Promise(function (resolve) {
    var left = ${count};
    function frame() { if (left-- <= 0) { resolve(true); return; } requestAnimationFrame(frame); }
    requestAnimationFrame(frame);
  }); })()`;
}

async function capture(send, c, name) {
  const rect = await evalSend(send, `(function () {
    var mount=document.getElementById(${JSON.stringify(c.mount)});
    var canvas=mount&&mount.querySelector('canvas');
    if(!canvas)return null;
    var box=canvas.getBoundingClientRect();
    return {x:box.x,y:box.y,width:box.width,height:box.height,dpr:window.devicePixelRatio||1};
  })()`);
  if (!rect) throw new Error('canvas rect unavailable for ' + c.name);
  const shot = await send('Page.captureScreenshot', {
    format: 'png', fromSurface: true,
    clip: { x: rect.x, y: rect.y, width: rect.width, height: rect.height, scale: rect.dpr },
  });
  if (!shot || !shot.data) throw new Error('screenshot failed for ' + c.name);
  fs.writeFileSync(path.join(ART, c.name + '-' + name + '.png'), Buffer.from(shot.data, 'base64'));
  const metrics = await evalSend(send, `new Promise(function (resolve) {
    var image=new Image();
    image.onload=function () { try {
      var canvas=document.createElement('canvas');canvas.width=image.width;canvas.height=image.height;
      var context=canvas.getContext('2d');context.drawImage(image,0,0);
      var data=context.getImageData(0,0,canvas.width,canvas.height).data;
      var corners=[[0,0],[canvas.width-4,0],[0,canvas.height-4],[canvas.width-4,canvas.height-4]];
      var red=0,green=0,blue=0,samples=0;
      for(var corner=0;corner<corners.length;corner+=1){for(var y=0;y<4;y+=1){for(var x=0;x<4;x+=1){
        var offset=((corners[corner][1]+y)*canvas.width+corners[corner][0]+x)*4;
        red+=data[offset];green+=data[offset+1];blue+=data[offset+2];samples+=1;
      }}}
      var background=[Math.round(red/samples),Math.round(green/samples),Math.round(blue/samples)];
      var foreground=0,rest=0,maxDelta=0,total=data.length/4;
      for(var index=0;index<data.length;index+=4){
        var delta=Math.max(Math.abs(data[index]-background[0]),Math.abs(data[index+1]-background[1]),Math.abs(data[index+2]-background[2]));
        if(delta>=${FG_THRESHOLD})foreground+=1;else rest+=1;
        if(delta>maxDelta)maxDelta=delta;
      }
      resolve({width:canvas.width,height:canvas.height,background:background,foreground:foreground,
        foregroundFraction:foreground/total,rest:rest,restFraction:rest/total,maxDelta:maxDelta});
    } catch(error){resolve({error:String(error&&error.message||error)});} };
    image.onerror=function(){resolve({error:'image decode failed'});};
    image.src='data:image/png;base64,${shot.data}';
  })`, { awaitPromise: true });
  return { rect, metrics };
}

async function waitNetworkIdle(label) {
  const deadline = Date.now() + 10000;
  let idleSince = 0;
  while (Date.now() < deadline) {
    if (networkRequests.size === 0) {
      if (!idleSince) idleSince = Date.now();
      if (Date.now() - idleSince >= 100) return;
    } else idleSince = 0;
    await sleep(25);
  }
  fail('[' + label + '] network did not become idle: ' + JSON.stringify(Array.from(networkRequests.values())));
}

function assertVisibleFrame(c, label, metrics) {
  if (!metrics || metrics.error || metrics.width !== W || metrics.height !== H ||
      !(metrics.foregroundFraction >= FG_COVERAGE) || !(metrics.maxDelta >= FG_THRESHOLD) ||
      !(metrics.restFraction >= REST_COVERAGE)) {
    fail('[' + c.name + '] ' + label + ' nonblank/rest-pixel integrity failed: ' + JSON.stringify(metrics));
  }
}

function visibleFrameOK(metrics) {
  return !!(metrics && !metrics.error && metrics.width === W && metrics.height === H &&
    metrics.foregroundFraction >= FG_COVERAGE && metrics.maxDelta >= FG_THRESHOLD &&
    metrics.restFraction >= REST_COVERAGE);
}

function receiptHasIndependentDeviceLoss(receipt) {
  if (!receipt) return false;
  if (receipt.independentDeviceLoss === true) return true;
  if (receipt.probe && receipt.probe.lost) return true;
  if (receipt.diagnostics && (receipt.diagnostics.deviceLost === true || receipt.diagnostics.deviceLostInfo)) return true;
  return false;
}

function classifyWGOutcomeSnapshot(snapshot) {
  const nativeCapsSnapshot = snapshot && snapshot.nativeCaps || {};
  const first = snapshot && snapshot.firstState || {};
  const post = snapshot && snapshot.postState || {};
  const receipt = snapshot && snapshot.fallbackReceipt || null;
  const outcome = snapshot && snapshot.acceptedOutcome || '';
  const fallbackKind = snapshot && snapshot.fallbackKind || '';
  const firstVisible = snapshot && snapshot.firstFrameVisible === true;
  const postVisible = snapshot && snapshot.postFrameVisible === true;
  const commandRevision = post.commandRevision || '';
  const commandAppliedRevision = post.commandAppliedRevision || '';
  const commandRevisionOK = !!commandRevision && commandRevision === commandAppliedRevision;
  const mountedAndCommandReady = first.mounted === 'true' && post.mounted === 'true' &&
    first.handleReady === true && post.handleReady === true &&
    first.commandReady === 'true' && post.commandReady === 'true';
  const result = { accepted: false, reason: '', hardwareNativeCertified: false };
  if (outcome === OUTCOME_NATIVE_WEBGPU) {
    if (nativeCapsSnapshot.webgpu !== true) result.reason = 'native-webgpu-cap-missing';
    else if (first.renderer !== 'webgpu' || post.renderer !== 'webgpu') result.reason = 'native-renderer-mismatch';
    else if (fallbackKind) result.reason = 'native-fallback-present';
    else if (!mountedAndCommandReady) result.reason = 'native-mounted-handle-missing';
    else if (!(first.webgpuColorPasses > 0) || !(first.webgpuCompletedColorPasses > 0) ||
        !(post.webgpuColorPasses > first.webgpuColorPasses) || !(post.webgpuCompletedColorPasses > first.webgpuCompletedColorPasses) ||
        !(post.webgpuCompletedSubmits > 0) || post.webgpuFailedSubmits !== 0) result.reason = 'native-webgpu-evidence-missing';
    else if (post.objectX !== 1.25 || !commandRevisionOK) result.reason = 'native-command-state-stale';
    else if (!firstVisible || !postVisible) result.reason = 'native-pixels-missing';
    else {
      result.accepted = true;
      result.reason = 'accepted';
      result.hardwareNativeCertified = false;
    }
    return result;
  }
  if (outcome === OUTCOME_FALLBACK_UNAVAILABLE || outcome === OUTCOME_FALLBACK_DEVICE_LOST) {
    const wantFallback = outcome === OUTCOME_FALLBACK_UNAVAILABLE ? FALLBACK_UNAVAILABLE : FALLBACK_DEVICE_LOST;
    if (first.renderer !== 'webgl' || post.renderer !== 'webgl') result.reason = 'fallback-renderer-mismatch';
    else if (fallbackKind !== wantFallback || first.fallback !== wantFallback || post.fallback !== wantFallback) result.reason = 'fallback-label-mismatch';
    else if (first.renderer === 'canvas2d' || post.renderer === 'canvas2d') result.reason = 'canvas2d-fallback-rejected';
    else if (!mountedAndCommandReady) result.reason = 'fallback-mounted-handle-missing';
    else if (outcome === OUTCOME_FALLBACK_UNAVAILABLE && nativeCapsSnapshot.webgpu === true) result.reason = 'unavailable-while-native-webgpu-available';
    else if (outcome === OUTCOME_FALLBACK_DEVICE_LOST &&
        !(receipt && /^device-lost-/.test(String(receipt.classification || '')))) result.reason = 'device-loss-receipt-missing';
    else if (outcome === OUTCOME_FALLBACK_DEVICE_LOST && !receiptHasIndependentDeviceLoss(receipt)) result.reason = 'device-loss-evidence-missing';
    else if (!(first.glDraws > 0) || !(post.glDraws > first.glDraws) ||
        first.glContext !== 'webgl2' || post.glContext !== 'webgl2') result.reason = 'fallback-webgl2-evidence-missing';
    else if (post.objectX !== 1.25 || !commandRevisionOK) result.reason = 'fallback-command-state-stale';
    else if (!firstVisible || !postVisible) result.reason = 'fallback-pixels-missing';
    else {
      result.accepted = true;
      result.reason = 'accepted';
      result.hardwareNativeCertified = false;
    }
    return result;
  }
  result.reason = outcome ? 'unknown-outcome' : 'missing-outcome';
  return result;
}

function assertWGOutcomeAccepted(c, evidence) {
  const verdict = classifyWGOutcomeSnapshot({
    nativeCaps,
    acceptedOutcome: evidence.acceptedOutcome,
    fallbackKind: evidence.fallbackKind || '',
    fallbackReceipt: evidence.fallbackReceipt,
    firstFrameVisible: visibleFrameOK(evidence.firstFrame && evidence.firstFrame.metrics),
    postFrameVisible: visibleFrameOK(evidence.afterHub && evidence.afterHub.metrics),
    firstState: evidence.firstRenderState || {},
    postState: evidence.postCommandRenderState || {},
  });
  evidence.outcomeVerdict = verdict;
  evidence.hardwareNativeCertified = verdict.hardwareNativeCertified;
  if (!verdict.accepted) {
    fail('[' + c.name + '] WebGPU-requested outcome rejected: ' + JSON.stringify({
      reason: verdict.reason,
      acceptedOutcome: evidence.acceptedOutcome,
      fallbackKind: evidence.fallbackKind,
      firstState: evidence.firstRenderState,
      postState: evidence.postCommandRenderState,
      fallbackReceipt: evidence.fallbackReceipt,
    }));
  }
}

function renderStateForOutcome(drawState) {
  return {
    renderer: drawState && drawState.renderer || '',
    fallback: drawState && drawState.fallback || '',
    mounted: drawState && drawState.mounted || '',
    handleReady: drawState && drawState.handleReady === true,
    registrySameHandle: drawState && drawState.registrySameHandle === true,
    commandReady: drawState && drawState.commandReady || '',
    commandRevision: drawState && drawState.commandRevision || '',
    commandAppliedRevision: drawState && drawState.commandAppliedRevision || '',
    objectX: drawState && drawState.mesh && drawState.mesh.x,
    glDraws: drawState && drawState.glDraws || 0,
    glContext: drawState && drawState.glContext || '',
    webgpuPasses: drawState && drawState.wgPasses || 0,
    webgpuColorPasses: drawState && drawState.wgColorPasses || 0,
    webgpuDraws: drawState && drawState.wgDraws || 0,
    webgpuSubmits: drawState && drawState.wgSubmits || 0,
    webgpuCompletedSubmits: drawState && drawState.wgCompletedSubmits || 0,
    webgpuFailedSubmits: drawState && drawState.wgFailedSubmits || 0,
    webgpuCompletedColorPasses: drawState && drawState.wgCompletedColorPasses || 0,
    webgpuMeshObjects: drawState && drawState.webgpuMeshObjects || '',
    webgpuMeshDrawCalls: drawState && drawState.webgpuMeshDrawCalls || '',
    webgpuMeshViewCulled: drawState && drawState.webgpuMeshViewCulled || '',
    webgpuMeshUndrawable: drawState && drawState.webgpuMeshUndrawable || '',
  };
}

function fallbackInstabilityDiagnostic(c, evidence, stage, drawState, captureMetrics) {
  if (!c.webgpu || (evidence.acceptedOutcome !== OUTCOME_FALLBACK_UNAVAILABLE &&
      evidence.acceptedOutcome !== OUTCOME_FALLBACK_DEVICE_LOST)) return null;
  const state = renderStateForOutcome(drawState);
  const expectedFallback = evidence.fallbackKind || '';
  if (state.renderer === 'webgl' && state.fallback === expectedFallback &&
      state.mounted === 'true' && state.handleReady && state.commandReady === 'true') return null;
  return {
    classification: 'fallback-instability',
    stage,
    expectedFallback,
    acceptedOutcome: evidence.acceptedOutcome,
    currentRenderState: state,
    captureMetrics: captureMetrics || null,
    fallbackReceipt: evidence.fallbackReceipt || null,
    warningTimeline: warningOccurrences.slice(),
  };
}

async function assertStableWGTypedFallback(send, c, evidence, stage, captureMetrics) {
  const drawState = await evalSend(send, winnerExpr(c, false));
  const diagnostic = fallbackInstabilityDiagnostic(c, evidence, stage, drawState, captureMetrics);
  if (!diagnostic) return;
  evidence.fallbackInstability = diagnostic;
  const error = new Error('[' + c.name + '] fallback-instability: ' + JSON.stringify(diagnostic));
  error.fallbackInstability = diagnostic;
  throw error;
}

async function waitForWGPresentationOrFallback(send, c, label, baselines, expectedObjectX, requireRevision) {
  const baseline = baselines || {};
  const before = await evalSend(send, winnerExpr(c, false));
  if (!before) throw new Error('[' + c.name + '] missing render state before ' + label);
  if (before.renderer === 'webgl') {
    if (before.fallback !== FALLBACK_UNAVAILABLE && before.fallback !== FALLBACK_DEVICE_LOST) {
      const rejected = await rejectedWGOutcomeWithReceipt(send, c, label, before);
      fail('[' + c.name + '] requested-WebGPU case reached WebGL with unacceptable fallback label before ' +
        label + ': ' + JSON.stringify({ state: before, webgpuFailureReceipt: rejected.webgpuFailureReceipt || null }));
      return rejected;
    }
    const fallbackOutcome = before.fallback === FALLBACK_UNAVAILABLE ?
      OUTCOME_FALLBACK_UNAVAILABLE : OUTCOME_FALLBACK_DEVICE_LOST;
    const receipt = await captureWebGPUFailureReceipt(send, c, currentCasePhase || label);
    const fallbackEvidence = await waitForWebGLFallbackPresentation(
      send, c, label, before.fallback, baseline.glDraws || 0, expectedObjectX, requireRevision
    );
    return {
      acceptedOutcome: fallbackOutcome,
      fallbackKind: before.fallback,
      fallbackReceipt: receipt,
      fallbackEvidence: fallbackEvidence,
    };
  }
  if (before.renderer !== 'webgpu' || before.fallback) {
    const rejected = await rejectedWGOutcomeWithReceipt(send, c, label, before);
    fail('[' + c.name + '] requested-WebGPU case reached unacceptable renderer before ' +
      label + ': ' + JSON.stringify({ state: before, webgpuFailureReceipt: rejected.webgpuFailureReceipt || null }));
    return rejected;
  }
  try {
    const webgpuEvidence = await waitForWebGPUPresentation(
      send, c, label, baseline.webgpuColorPasses || 0,
      baseline.webgpuCompletedColorPasses || 0, expectedObjectX
    );
    return {
      acceptedOutcome: OUTCOME_NATIVE_WEBGPU,
      fallbackKind: '',
      webgpuEvidence: webgpuEvidence,
    };
  } catch (error) {
    if (safeOwnDataValue(error, 'terminal') !== true ||
        !/^device-lost-/.test(String(safeOwnDataValue(error, 'classification') || ''))) {
      throw error;
    }
    const receipt = await captureWebGPUFailureReceipt(send, c, currentCasePhase || label);
    const fallbackEvidence = await waitForWebGLFallbackPresentation(
      send, c, label, FALLBACK_DEVICE_LOST, baseline.glDraws || 0, expectedObjectX, requireRevision
    );
    return {
      acceptedOutcome: OUTCOME_FALLBACK_DEVICE_LOST,
      fallbackKind: FALLBACK_DEVICE_LOST,
      fallbackReceipt: receipt,
      fallbackEvidence: fallbackEvidence,
      terminalWebGPU: {
        classification: safeOwnDataValue(error, 'classification') || '',
        lastPredicate: boundedValue(safeOwnDataValue(error, 'lastPredicate')),
      },
    };
  }
}

function assertEnvelope(call, c, generation, programName, commandIDs) {
  if (!call) { fail('[' + c.name + '] missing hydrate generation ' + generation); return; }
  const envelope = call.envelope;
  if (call.generation !== generation || call.argCount !== 6 || call.surfaceKind !== 'scene3d' ||
      call.targetId !== c.engine || call.component !== 'GoSXScene3D' || call.format !== 'json' ||
      call.programName !== programName) {
    fail('[' + c.name + '] hydrate call identity mismatch: ' + JSON.stringify(call));
  }
  if (!envelope || envelope.version !== 1 || envelope.surfaceKind !== 'scene3d' ||
      envelope.outputKind !== 'scene3d.commands' || envelope.targetId !== c.engine ||
      envelope.mode !== 'initial' || !Array.isArray(envelope.commands)) {
    fail('[' + c.name + '] invalid real WASM envelope: ' + JSON.stringify(envelope));
    return;
  }
  const gotIDs = envelope.commands.map((command) => command.objectId);
  const gotKinds = envelope.commands.map((command) => command.kind);
  if (JSON.stringify(gotIDs) !== JSON.stringify(commandIDs) || gotKinds.some((kind) => kind !== 0)) {
    fail('[' + c.name + '] command count/order mismatch: IDs=' + JSON.stringify(gotIDs) +
      ' kinds=' + JSON.stringify(gotKinds));
  }
}

async function runCase(send, c) {
  const evidence = {
    name: c.name,
    backend: c.webgpu ? 'webgpu' : 'webgl2',
    requestedBackend: c.webgpu ? 'webgpu' : 'webgl2',
    startedAt: new Date().toISOString(),
    startedAtMS: Date.now(),
    phases: [],
  };
  const phase = (name) => {
    currentCaseName = c.name;
    currentCasePhase = name;
    evidence.phase = name;
    evidence.phases.push({ name, at: new Date().toISOString(), atMS: Date.now() });
  };
  return retainCaseEvidence(caseEvidence, evidence, async () => {
  phase('navigate');
  const loaded = waitForEvent('Page.loadEventFired', MOUNT_MS);
  await send('Page.navigate', { url: BASE + '/case/' + c.name });
  await loaded;

  phase('preflight-readiness');
  evidence.preflight = await poll(send, preflightExpr(c), c.name + ' first blocked hydrate');
  const pre = evidence.preflight;
  if (pre.bootErrors.length || !pre.runtimeReady || pre.runtimeExited || !pre.firstPending) {
    fail('[' + c.name + '] runtime/preflight state invalid: ' + JSON.stringify(pre));
  }
  if (!pre.handshake || pre.handshake.abiVersion !== 3 || pre.handshake.variant !== 'full') {
    fail('[' + c.name + '] ABI 3 full handshake missing: ' + JSON.stringify(pre.handshake));
  }
  if (!pre.ssrIdentity || pre.childCount !== 1 || pre.canvasCount !== 0 || pre.hasState || pre.hasHandle || pre.registered) {
    fail('[' + c.name + '] blocked generation mutated SSR/mount/registry: ' + JSON.stringify(pre));
  }
  assertEnvelope(pre.hydrates[0], c, 1, 'AdapterStaleA', [0]);

  phase('winning-generation-readiness');
  await evalSend(send, 'window.__gosx_runtime_ready(); true');
  evidence.winner = await poll(send, winnerExpr(c, true), c.name + ' winning generation');
  assertEnvelope(evidence.winner.hydrates[1], c, 2, 'AdapterWinnerB', [0, 1, 2]);
  const winner = evidence.winner;
  if (!winner.keys.includes('1') || winner.keys.includes('0') || !winner.mesh || winner.mesh.id !== 'scene-object-1' ||
      winner.mesh.color !== '#8de1ff' || winner.lights !== 1) {
    fail('[' + c.name + '] winning commands not present exactly: ' + JSON.stringify(winner));
  }
  const winnerBackendReady = c.webgpu ?
    ((winner.renderer === 'webgpu' && !winner.fallback) ||
      (winner.renderer === 'webgl' && (winner.fallback === FALLBACK_UNAVAILABLE || winner.fallback === FALLBACK_DEVICE_LOST))) :
    (winner.renderer === 'webgl' && !winner.fallback);
  if (!winner.handleReady || !winner.registrySameHandle || !winnerBackendReady || winner.commandReady !== 'true') {
    fail('[' + c.name + '] winning handle/backend not ready: ' + JSON.stringify(winner));
  }
  if (winner.disposes.length !== 1 || winner.disposes[0][0] !== c.engine) {
    fail('[' + c.name + '] replaced adapter disposal mismatch: ' + JSON.stringify(winner.disposes));
  }

  phase('stale-generation-release');
  await evalSend(send, 'window.__adapterReleaseFirst(); true');
  await evalSend(send, settleFramesExpr(12), { awaitPromise: true });
  evidence.afterStaleRelease = await evalSend(send, identityExpr(c));
  const identity = evidence.afterStaleRelease;
  if (!identity || !identity.sameMount || !identity.sameState || !identity.sameHandle ||
      !identity.sameCanvas || !identity.sameRecord || !identity.keys.includes('1') || identity.keys.includes('0') ||
      identity.disposes.length !== 1) {
    fail('[' + c.name + '] stale output applied or republished: ' + JSON.stringify(identity));
  }

  if (c.webgpu) {
    phase('requested-webgpu-first-presentation-readiness');
    const firstOutcome = await waitForWGPresentationOrFallback(send, c, c.name + ' first frame',
      { glDraws: winner.glDraws || 0 }, undefined, false);
    evidence.acceptedOutcome = firstOutcome.acceptedOutcome;
    evidence.fallbackKind = firstOutcome.fallbackKind || '';
    if (firstOutcome.fallbackReceipt) evidence.fallbackReceipt = firstOutcome.fallbackReceipt;
    if (firstOutcome.webgpuFailureReceipt) evidence.webgpuFailureReceipt = firstOutcome.webgpuFailureReceipt;
    if (firstOutcome.webgpuEvidence) evidence.webgpuPresentation = firstOutcome.webgpuEvidence;
    if (firstOutcome.fallbackEvidence) evidence.webgpuFallbackPresentation = firstOutcome.fallbackEvidence;
    if (firstOutcome.terminalWebGPU) evidence.terminalWebGPU = firstOutcome.terminalWebGPU;
  }

  phase('first-capture');
  await assertStableWGTypedFallback(send, c, evidence, 'before-first-capture');
  evidence.firstFrame = await capture(send, c, 'first-frame');
  await assertStableWGTypedFallback(send, c, evidence, 'after-first-capture', evidence.firstFrame.metrics);
  const metrics = evidence.firstFrame.metrics;
  assertVisibleFrame(c, 'first frame', metrics);
  const drawState = await evalSend(send, winnerExpr(c, false));
  evidence.firstRenderState = renderStateForOutcome(drawState);
  evidence.observedRenderer = evidence.firstRenderState.renderer;
  evidence.fallbackKind = evidence.fallbackKind || evidence.firstRenderState.fallback || '';
  evidence.nativeRenderer = {
    mounted: drawState.mounted,
    renderer: drawState.renderer,
    fallback: drawState.fallback,
    handleReady: drawState.handleReady,
    commandReady: drawState.commandReady,
    commandRevision: drawState.commandRevision,
    commandAppliedRevision: drawState.commandAppliedRevision,
    objectX: drawState.mesh && drawState.mesh.x,
    glDraws: drawState.glDraws,
    glContext: drawState.glContext,
    webgpuPasses: drawState.wgPasses,
    webgpuColorPasses: drawState.wgColorPasses,
    webgpuDraws: drawState.wgDraws,
    webgpuSubmits: drawState.wgSubmits,
    webgpuCompletedSubmits: drawState.wgCompletedSubmits,
    webgpuFailedSubmits: drawState.wgFailedSubmits,
    webgpuCompletedColorPasses: drawState.wgCompletedColorPasses,
    webgpuMeshObjects: drawState.webgpuMeshObjects,
    webgpuMeshDrawCalls: drawState.webgpuMeshDrawCalls,
    webgpuMeshViewCulled: drawState.webgpuMeshViewCulled,
    webgpuMeshUndrawable: drawState.webgpuMeshUndrawable,
  };
  if (drawState.mounted !== 'true') {
    fail('[' + c.name + '] first frame never published mounted readiness: ' + JSON.stringify(evidence.nativeRenderer));
  }
  if (!c.webgpu && (!(drawState.glDraws > 0) || drawState.glContext !== 'webgl2')) {
    fail('[gl] native WebGL2 draw evidence missing: ' + JSON.stringify(evidence.nativeRenderer));
  }
  if (c.webgpu && evidence.acceptedOutcome === OUTCOME_NATIVE_WEBGPU &&
      (!(drawState.wgColorPasses > 0) || !(drawState.wgCompletedColorPasses > 0) ||
      !(drawState.wgCompletedSubmits > 0) || drawState.wgFailedSubmits !== 0)) {
    fail('[wg] native WebGPU render evidence missing: ' + JSON.stringify(evidence.nativeRenderer));
  }
  if (!c.webgpu) evidence.acceptedOutcome = OUTCOME_NATIVE_WEBGL2;

  phase('hub-command');
  evidence.hubCommand = await evalSend(send, hubCommandExpr(c), { awaitPromise: true });
  if (!evidence.hubCommand || evidence.hubCommand.beforeX !== 0 || evidence.hubCommand.afterX !== 1.25 ||
      !evidence.hubCommand.sameState || !evidence.hubCommand.sameHandle ||
      !evidence.hubCommand.commandApplied || !evidence.hubCommand.revision ||
      evidence.hubCommand.revision !== evidence.hubCommand.appliedRevision) {
    fail('[' + c.name + '] explicit hub-wire command did not reach winning handle: ' +
      JSON.stringify(evidence.hubCommand));
  }
  await evalSend(send, settleFramesExpr(4), { awaitPromise: true });
  if (c.webgpu) {
    phase('requested-webgpu-post-command-presentation-readiness');
    const postOutcome = await waitForWGPresentationOrFallback(send, c, c.name + ' after hub command', {
      glDraws: evidence.hubCommand.glDrawBaseline,
      webgpuColorPasses: evidence.hubCommand.webgpuColorPassBaseline,
      webgpuCompletedColorPasses: evidence.hubCommand.webgpuCompletedColorPassBaseline,
    }, 1.25, true);
    if (postOutcome.acceptedOutcome !== evidence.acceptedOutcome ||
        (postOutcome.fallbackKind || '') !== (evidence.fallbackKind || '')) {
      fail('[wg] requested-WebGPU outcome changed between first and post-command proof: ' +
        JSON.stringify({ firstOutcome: evidence.acceptedOutcome, firstFallback: evidence.fallbackKind,
          postOutcome: postOutcome.acceptedOutcome, postFallback: postOutcome.fallbackKind }));
    }
    if (postOutcome.fallbackReceipt && !evidence.fallbackReceipt) evidence.fallbackReceipt = postOutcome.fallbackReceipt;
    if (postOutcome.webgpuFailureReceipt && !evidence.webgpuFailureReceipt) evidence.webgpuFailureReceipt = postOutcome.webgpuFailureReceipt;
    if (postOutcome.webgpuEvidence) evidence.webgpuAfterHubPresentation = postOutcome.webgpuEvidence;
    if (postOutcome.fallbackEvidence) evidence.webgpuFallbackAfterHubPresentation = postOutcome.fallbackEvidence;
    if (postOutcome.terminalWebGPU) evidence.terminalWebGPU = postOutcome.terminalWebGPU;
    const postPresentation = postOutcome.webgpuEvidence || postOutcome.fallbackEvidence;
    if (!postPresentation || postPresentation.objectX !== 1.25 ||
        !postPresentation.commandRevision ||
        postPresentation.commandRevision !== postPresentation.commandAppliedRevision) {
      fail('[wg] post-command frame did not retain applied command state: ' +
        JSON.stringify(postPresentation));
    }
  }
  phase('post-command-capture');
  await assertStableWGTypedFallback(send, c, evidence, 'before-post-command-capture');
  evidence.afterHub = await capture(send, c, 'after-hub-command');
  await assertStableWGTypedFallback(send, c, evidence, 'after-post-command-capture', evidence.afterHub.metrics);
  assertVisibleFrame(c, 'after hub command', evidence.afterHub.metrics);
  const postCommandDrawState = await evalSend(send, winnerExpr(c, false));
  evidence.postCommandRenderState = renderStateForOutcome(postCommandDrawState);
  evidence.observedRenderer = evidence.postCommandRenderState.renderer;
  evidence.fallbackKind = evidence.fallbackKind || evidence.postCommandRenderState.fallback || '';
  if (c.webgpu) assertWGOutcomeAccepted(c, evidence);

  phase('disposal');
  evidence.disposed = await evalSend(send, `(function () {
    window.__gosx_dispose_engine(${JSON.stringify(c.engine)});
    var mount=document.getElementById(${JSON.stringify(c.mount)});
    return {
      noState:!mount.__gosxScene3DState,
      noHandle:!mount.__gosxScene3DHandle,
      noRecord:!window.__gosx.engines.has(${JSON.stringify(c.engine)}),
      disposes:(window.__adapterDisposeCalls||[]).map(function(args){return args.slice();})
    };
  })()`);
  if (!evidence.disposed.noState || !evidence.disposed.noHandle || !evidence.disposed.noRecord ||
      evidence.disposed.disposes.length !== 2) {
    fail('[' + c.name + '] final disposal mismatch: ' + JSON.stringify(evidence.disposed));
  }

  phase('telemetry-drain');
  await evalSend(send, `(async function () {
    if(typeof window.__gosx_telemetry_flush!=='function'||typeof window.__gosx_telemetry_snapshot!=='function')return null;
    window.__gosx_telemetry_flush({drain:true});
    var deadline=Date.now()+10000,last=null;
    while(Date.now()<deadline){last=window.__gosx_telemetry_snapshot();
      if(last&&last.queueDepth===0&&last.pendingRequests===0)return last;
      await new Promise(function(resolve){setTimeout(resolve,25);});}
    return last;
  })()`, { awaitPromise: true });
  phase('network-idle');
  await waitNetworkIdle(c.name);
  phase('complete');
  return evidence;
  }, async (error) => {
    evidence.failure = {
      phase: evidence.phase || 'before-case-phase',
      at: new Date().toISOString(),
      atMS: Date.now(),
      error: safeErrorSnapshot(error),
      lastPredicate: boundedValue(safeOwnDataValue(error, 'lastPredicate')),
      errorsAtFailure: boundedMessages(errors),
      warningsAtFailure: boundedMessages(warnings),
    };
    if (c.webgpu) {
      try {
        evidence.webgpuFailureReceipt = await captureWebGPUFailureReceipt(send, c, evidence.failure.phase);
      } catch (receiptError) {
        evidence.failure.webgpuReceiptError = safeErrorSnapshot(receiptError);
      }
    }
  });
}

const caseEvidence = [];
let nativeCaps = null;
let browserReceipt = null;
let finished = false;
let reportWriteFailed = false;

function classifyWarningEntry(entry, typedFallback) {
  const occurrence = typeof entry === 'string' ? {
    message: entry,
    source: 'legacy-string',
    caseName: '',
    phase: '',
  } : (entry || {});
  const message = String(occurrence.message || '');
  const captureDriver = classifyCaptureDriverWarning(occurrence, message);
  if (captureDriver) return captureDriver;
  if (!typedFallback) {
    return { allowed: false, reason: 'no-accepted-typed-fallback' };
  }
  const receiptPhase = typedFallback.fallbackReceipt && typedFallback.fallbackReceipt.phase || '';
  if (typedFallback.name !== 'wg' || occurrence.caseName !== typedFallback.name) {
    return { allowed: false, reason: 'warning-case-mismatch' };
  }
  if (!receiptPhase || occurrence.phase !== receiptPhase) {
    return { allowed: false, reason: 'warning-phase-mismatch' };
  }
  for (const pattern of FALLBACK_WARNING_PATTERNS) {
    if (pattern.test(message)) {
      return {
        allowed: true,
        reason: 'accepted-typed-fallback-environment-warning',
        phase: typedFallback.fallbackReceipt && typedFallback.fallbackReceipt.phase || '',
        caseName: typedFallback.name,
        outcome: typedFallback.acceptedOutcome,
        fallbackKind: typedFallback.fallbackKind,
      };
    }
  }
  return { allowed: false, reason: 'not-in-exact-fallback-warning-allowlist' };
}

function classifyCaptureDriverWarning(occurrence, message) {
  if (!CAPTURE_DRIVER_WARNING.test(message)) return null;
  if (occurrence.source !== 'Log.entryAdded') return { allowed: false, reason: 'capture-driver-warning-source-mismatch' };
  if (occurrence.caseName !== 'gl') return { allowed: false, reason: 'capture-driver-warning-case-mismatch' };
  if (occurrence.phase !== 'stale-generation-release') return { allowed: false, reason: 'capture-driver-warning-phase-mismatch' };
  return { allowed: true, reason: 'accepted-gl-capture-readpixels-warning', phase: occurrence.phase, caseName: occurrence.caseName };
}

function classifyWarningsForReport() {
  const typedFallback = caseEvidence.find((entry) =>
    entry && (entry.acceptedOutcome === OUTCOME_FALLBACK_UNAVAILABLE ||
      entry.acceptedOutcome === OUTCOME_FALLBACK_DEVICE_LOST) &&
    entry.outcomeVerdict && entry.outcomeVerdict.accepted === true &&
    entry.fallbackReceipt);
  const occurrences = warningOccurrences.length ? warningOccurrences :
    warnings.map((message) => ({ message, source: 'legacy-string', caseName: '', phase: '', atMS: 0 }));
  const entries = occurrences.map((entry) => ({
    message: entry.message,
    occurrence: entry,
    classification: classifyWarningEntry(entry, typedFallback),
  }));
  return {
    total: warnings.length,
    allowed: entries.filter((entry) => entry.classification.allowed),
    unexpected: entries.filter((entry) => !entry.classification.allowed),
  };
}

function writeReport(extra) {
  const warningClassification = classifyWarningsForReport();
  const report = Object.assign({
    contract: 'gosx.scene3d.adapter-hydrate/v1',
    hostedPolicy: HOSTED_POLICY,
    abi: 3,
    runtimeArtifact,
    hubGate,
    selectedBrowser: browserReceipt,
    nativeCaps,
    cases: caseEvidence,
    errors,
    warnings,
    warningOccurrences,
    warningClassification,
    notFound,
    unexpectedRequests,
    networkFailures,
    intentionalNoContent,
    programRequests: Object.fromEntries(programRequests),
  }, extra || {});
  try {
    fs.writeFileSync(path.join(ART, 'report.json'), JSON.stringify(report, null, 2));
  } catch (e) {
    reportWriteFailed = true;
    console.error('failed to write report.json: ' + e.message);
  }
}

async function cleanup() {
  for (const record of pending.values()) clearTimeout(record.timer);
  pending.clear();
  for (const entry of listeners) clearTimeout(entry.timer);
  listeners.length = 0;
  networkRequests.clear();
  try { if (ws) ws.close(); } catch (_error) {}
  if (chrome) {
    const exited = new Promise((resolve) => chrome.once('exit', resolve));
    try { chrome.kill('SIGKILL'); } catch (_error) {}
    await Promise.race([exited, sleep(5000)]);
  }
  if (profile) {
    try { fs.rmSync(profile, { recursive: true, force: true }); }
    catch (e) { fail('profile cleanup failed: ' + e.message); }
  }
  try { fs.rmSync(scratch, { recursive: true, force: true }); }
  catch (e) { fail('WASM scratch cleanup failed: ' + e.message); }
  await new Promise((resolve) => {
    let done = false;
    const finish = () => { if (!done) { done = true; resolve(); } };
    const timer = setTimeout(finish, 3000);
    try { server.close(() => { clearTimeout(timer); finish(); }); }
    catch (_error) { clearTimeout(timer); finish(); }
  });
}

process.on('exit', () => { try { if (chrome) chrome.kill('SIGKILL'); } catch (_error) {} });

const watchdog = setTimeout(() => {
  if (finished) return;
  finished = true;
  fail('overall watchdog exceeded ' + OVERALL_MS + 'ms');
  writeReport({ fatal: 'overall watchdog' });
  cleanup().then(() => process.exit(1));
  setTimeout(() => process.exit(1), 5000).unref();
}, OVERALL_MS);

(async () => {
  await new Promise((resolve, reject) => {
    server.once('error', reject);
    server.listen(0, '127.0.0.1', resolve);
  });
  BASE = 'http://127.0.0.1:' + server.address().port;

  profile = fs.mkdtempSync(path.join(os.tmpdir(), 'gosx-adapter-chrome-'));
  browserReceipt = selectedBrowser();
  browserReceipt.selfCheck = browserIdentitySelfCheck(browserReceipt);
  chrome = spawn(browserReceipt.realPath, [
    '--headless=new', '--no-sandbox', '--use-gl=angle', '--use-angle=gl-egl',
    '--ignore-gpu-blocklist', '--enable-unsafe-swiftshader', '--enable-unsafe-webgpu',
    '--disable-dev-shm-usage', '--user-data-dir=' + profile,
    '--remote-debugging-port=0', 'about:blank',
  ], { stdio: ['ignore', 'ignore', 'pipe'] });

  const wsURL = await new Promise((resolve, reject) => {
    let stderr = '';
    const timer = setTimeout(() => reject(new Error('no DevTools WebSocket URL')), 20000);
    const onExit = () => { clearTimeout(timer); reject(new Error('Chrome exited early: ' + stderr)); };
    chrome.once('exit', onExit);
    chrome.stderr.on('data', (chunk) => {
      stderr += chunk.toString();
      const match = stderr.match(/ws:\/\/127\.0\.0\.1:\d+\/devtools\/browser\/[^\s]+/);
      if (match) {
        clearTimeout(timer);
        chrome.removeListener('exit', onExit);
        resolve(match[0]);
      }
    });
  });

  ws = new WebSocket(wsURL);
  await new Promise((resolve, reject) => {
    const timer = setTimeout(() => reject(new Error('CDP WebSocket connect timeout')), 20000);
    ws.onopen = () => { clearTimeout(timer); resolve(); };
    ws.onerror = () => { clearTimeout(timer); reject(new Error('CDP WebSocket error')); };
  });
  ws.onmessage = (event) => dispatch(event.data);
  browserReceipt.cdp = await cdpSend('Browser.getVersion');
  verifyCDPBrowserIdentity(browserReceipt.cdp, browserReceipt, 'selectedBrowser.cdp');

  const target = await cdpSend('Target.createTarget', { url: 'about:blank' });
  const attached = await cdpSend('Target.attachToTarget', { targetId: target.targetId, flatten: true });
  const send = (method, params, timeout) => cdpSend(method, params, attached.sessionId, timeout || STEP_MS);
  await send('Page.enable');
  await send('Runtime.enable');
  await send('Network.enable');
  await send('Log.enable');
  await send('Page.addScriptToEvaluateOnNewDocument', { source: PRELOAD });

  const capsLoaded = waitForEvent('Page.loadEventFired', STEP_MS);
  await send('Page.navigate', { url: BASE + '/' });
  await capsLoaded;
  nativeCaps = await evalSend(send, CAPS_EXPR, { awaitPromise: true });
  if (!nativeCaps || nativeCaps.webgl2 !== true) {
    throw new Error('native WebGL2 is required; WebGPU capability is recorded but not required; got ' +
      JSON.stringify(nativeCaps));
  }

  for (const c of CASES) await runCase(send, c);
  for (const c of CASES) {
    if (programRequests.get('/program/' + c.name + '.json') !== 2) {
      fail('[' + c.name + '] expected exactly two program fetches, got ' +
        (programRequests.get('/program/' + c.name + '.json') || 0));
    }
  }
})().catch((e) => {
  fail(String(e && (e.stack || e.message) || e));
}).then(async () => {
  if (!finished) {
    finished = true;
    clearTimeout(watchdog);
  }
  await cleanup();
  writeReport({});
  const warningClassification = classifyWarningsForReport();
  const exitCode = errors.length || warningClassification.unexpected.length || notFound.length || unexpectedRequests.length ||
    networkFailures.length || reportWriteFailed ? 1 : 0;
  setTimeout(() => process.exit(exitCode), 50);
});
