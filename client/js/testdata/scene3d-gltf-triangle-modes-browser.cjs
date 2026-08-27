'use strict';
/* Browser-verification probe: real Chrome + WebGL loading of ordinary glTF
 * TRIANGLE_STRIP (mode 5) and TRIANGLE_FAN (mode 6) mesh primitives through the
 * built client/js/bootstrap.js runtime. Node builtin-only, no npm deps.
 * Generates a tiny valid GLB in-script (named mesh "patch", model id "topo"),
 * serves it over real HTTP, asserts expanded corner positions and +Z normals,
 * real WebGL draw calls, unique object ids, and dispose cleanup.
 * Run: node scene3d-gltf-triangle-modes-browser.cjs <repoRoot> */

const fs = require('fs'), os = require('os'), path = require('path');
const http = require('http');
const { spawn } = require('child_process');

const REPO = process.argv[2];
if (!REPO) {
  console.error('usage: node scene3d-gltf-triangle-modes-browser.cjs <repoRoot>');
  process.exit(2);
}
const errors = [], warnings = [];
const fail = (m) => { errors.push(m); };

// ---- GLB fixture: one named mesh "patch", two primitives (strip mode 5, fan mode 6) ----
function buildPatchGLB() {
  // Authored CCW winding viewed from +Z.
  const stripPos = new Float32Array([
    0, 0, 0,  2, 0, 0,  0, 1, 0,  2, 1, 0, // sequential vertices 0..3
  ]);
  const stripIdx = new Uint16Array([0, 1, 2, 3]);
  const fanPos = new Float32Array([
    5, 0, 0,  6, 0, 0,  5, 1, 0,  4, 0, 0, // fan center + rim, CCW from +Z
  ]);
  const fanIdx = new Uint16Array([0, 1, 2, 3]);

  const binParts = [];
  const views = [];
  let off = 0;
  function addView(typedArr, target) {
    const bytes = Buffer.from(typedArr.buffer, typedArr.byteOffset, typedArr.byteLength);
    binParts.push(bytes);
    const view = { buffer: 0, byteOffset: off, byteLength: bytes.length };
    if (target) view.target = target;
    views.push(view);
    off += bytes.length;
    // pad to 4-byte boundary
    const pad = (4 - (off % 4)) % 4;
    if (pad) { binParts.push(Buffer.alloc(pad)); off += pad; }
    return views.length - 1;
  }
  const stripPosView = addView(stripPos, 34962); // ARRAY_BUFFER
  const stripIdxView = addView(stripIdx, 34963); // ELEMENT_ARRAY_BUFFER
  const fanPosView = addView(fanPos, 34962);
  const fanIdxView = addView(fanIdx, 34963);

  const accessors = [
    { bufferView: stripPosView, componentType: 5126, count: 4, type: 'VEC3',
      min: [0, 0, 0], max: [2, 1, 0] },
    { bufferView: stripIdxView, componentType: 5123, count: 4, type: 'SCALAR',
      min: [0], max: [3] },
    { bufferView: fanPosView, componentType: 5126, count: 4, type: 'VEC3',
      min: [4, 0, 0], max: [6, 1, 0] },
    { bufferView: fanIdxView, componentType: 5123, count: 4, type: 'SCALAR',
      min: [0], max: [3] },
  ];

  const bin = Buffer.concat(binParts);
  const json = {
    asset: { version: '2.0', generator: 'scene3d-gltf-triangle-modes-browser probe' },
    scene: 0,
    scenes: [{ nodes: [0] }],
    nodes: [{ mesh: 0, name: 'patch' }], // identity node (no TRS)
    meshes: [{
      name: 'patch',
      primitives: [
        { attributes: { POSITION: 0 }, indices: 1, mode: 5, material: 0 }, // TRIANGLE_STRIP
        { attributes: { POSITION: 2 }, indices: 3, mode: 6, material: 0 }, // TRIANGLE_FAN
      ],
    }],
    materials: [{ pbrMetallicRoughness: { baseColorFactor: [0.8, 0.8, 0.8, 1] } }],
    accessors,
    bufferViews: views,
    buffers: [{ byteLength: bin.length }],
  };

  let jsonBuf = Buffer.from(JSON.stringify(json), 'utf8');
  const jsonPad = (4 - (jsonBuf.length % 4)) % 4;
  if (jsonPad) jsonBuf = Buffer.concat([jsonBuf, Buffer.alloc(jsonPad, 0x20)]);
  const binPad = (4 - (bin.length % 4)) % 4;
  const binPadded = binPad ? Buffer.concat([bin, Buffer.alloc(binPad)]) : bin;
  const header = Buffer.alloc(12);
  header.writeUInt32LE(0x46546C67, 0); // 'glTF'
  header.writeUInt32LE(2, 4);
  header.writeUInt32LE(12 + 8 + jsonBuf.length + 8 + binPadded.length, 8);
  const jsonHeader = Buffer.alloc(8);
  jsonHeader.writeUInt32LE(jsonBuf.length, 0);
  jsonHeader.writeUInt32LE(0x4E4F534A, 4); // 'JSON'
  const binHeader = Buffer.alloc(8);
  binHeader.writeUInt32LE(binPadded.length, 0);
  binHeader.writeUInt32LE(0x004E4942, 4); // 'BIN'
  return Buffer.concat([header, jsonHeader, jsonBuf, binHeader, binPadded]);
}
const glb = buildPatchGLB();

// Expanded corner expectations from the canonical decompositions:
// strip [0,1,2,3,2,1]; fan [0,1,2,0,2,3].
const STRIP_EXPECTED = [
  0, 0, 0, 2, 0, 0, 0, 1, 0, 2, 1, 0, 0, 1, 0, 2, 0, 0,
];
const FAN_EXPECTED = [
  5, 0, 0, 6, 0, 0, 5, 1, 0, 5, 0, 0, 5, 1, 0, 4, 0, 0,
];

const manifest = JSON.stringify({ engines: [{ id: 'gosx-engine-patch-browser', component: 'GoSXScene3D',
  kind: 'surface', mountId: 'scene-patch-browser',
  props: { width: 320, height: 180, autoRotate: false,
    forceWebGL: true, requireWebGL: true,
    models: [{ id: 'topo', src: '/models/topo.glb', static: true }] } }] });
const html = '<!doctype html><html><head><meta charset="utf-8"></head><body>' +
  '<div id="scene-patch-browser" width="320" height="180"></div>' +
  '<script type="application/json" id="gosx-manifest">' + manifest + '</script>' +
  '<script src="/bootstrap.js"></script></body></html>';

const server = http.createServer((req, res) => {
  if (req.url === '/models/topo.glb') {
    res.writeHead(200, { 'content-type': 'model/gltf-binary', 'content-length': glb.length });
    res.end(glb);
  } else if (req.url === '/bootstrap.js' || req.url === '/client/js/bootstrap.js') {
    const js = fs.readFileSync(path.join(REPO, 'client', 'js', 'bootstrap.js'));
    res.writeHead(200, { 'content-type': 'text/javascript', 'content-length': js.length });
    res.end(js);
  } else if (req.url === '/') {
    res.writeHead(200, { 'content-type': 'text/html' });
    res.end(html);
  } else { res.writeHead(404); res.end(); }
});

// ---- CDP plumbing (same lifecycle/flags as the repo-owned TRS probe) ----
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
      const text = m.params.args.map((x) => x.value || x.description || '').join(' ');
      if (m.params.type === 'error') errors.push(text);
      else if (m.params.type === 'warning') warnings.push(text);
    }
    if (m.method === 'Runtime.exceptionThrown') {
      errors.push((m.params.exceptionDetails.exception &&
        m.params.exceptionDetails.exception.description) || m.params.exceptionDetails.text);
    }
  }
}
const PRELOAD = `
window.__gosxDRAWS = 0; window.__gosxTRIANGLE6_DRAWS = 0; window.__gosxGL = null;
function wrap(proto) {
  if (!proto) return;
  ['drawArrays', 'drawElements'].forEach(function (n) {
    const o = proto[n]; if (!o) return;
    proto[n] = function () {
      window.__gosxDRAWS += 1;
      const mode = arguments[0];
      const count = (n === 'drawArrays') ? arguments[2] : arguments[1];
      if (mode === 4 && count === 6) window.__gosxTRIANGLE6_DRAWS += 1;
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

let exitCode = 0;
(async () => {
  await new Promise((res) => server.listen(0, '127.0.0.1', res));
  const port = server.address().port;
  profile = fs.mkdtempSync(path.join(os.tmpdir(), 'gosx-patch-probe-'));
  chrome = spawn('/usr/bin/google-chrome', [
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

  // Ordinary static mount: no animation, no _modelSkins assertion. Just wait for
  // both triangle-mode objects to appear in the scene state.
  const POLL = '(()=>{const m=document.getElementById("scene-patch-browser");' +
    'const s=m&&m.__gosxScene3DState;' +
    'return !!(s&&s.objects&&s.objects.get("topo/patch-prim-0")&&s.objects.get("topo/patch-prim-1"));})()';
  let mounted = false;
  for (let i = 0; i < 100; i += 1) {
    const r = await send('Runtime.evaluate', { expression: POLL, returnByValue: true });
    if (r.result && r.result.value === true) { mounted = true; break; }
    await new Promise((res) => setTimeout(res, 100));
  }
  if (!mounted) fail('engine did not load topo/patch-prim-0 and topo/patch-prim-1');

  // Let a few real frames render.
  await new Promise((res) => setTimeout(res, 1500));

  const READ = '(()=>{const m=document.getElementById("scene-patch-browser");const s=m&&m.__gosxScene3DState;' +
    'if(!s)return null;' +
    'const read=(r)=>({pos:(r&&r.vertices&&r.vertices.positions)?Array.from(r.vertices.positions):null,' +
    'nrm:(r&&r.vertices&&r.vertices.normals)?Array.from(r.vertices.normals):null});' +
    'return {drawCalls:window.__gosxDRAWS,triangle6Draws:window.__gosxTRIANGLE6_DRAWS,' +
    'gl:window.__gosxGL,strip:read(s.objects.get("topo/patch-prim-0")),' +
    'fan:read(s.objects.get("topo/patch-prim-1")),' +
    'objCount:(s.objects&&s.objects.size)||0};})()';
  const read = await send('Runtime.evaluate', { expression: READ, returnByValue: true });
  const s = read.result && read.result.value;
  if (!s) { fail('could not read __gosxScene3DState'); }
  else {
    const attr = await send('Runtime.evaluate', { expression:
      '(()=>{const m=document.getElementById("scene-patch-browser");' +
      'return {mounted:m.getAttribute("data-gosx-scene3d-mounted"),' +
      'renderer:m.getAttribute("data-gosx-scene3d-renderer"),' +
      'fallback:m.getAttribute("data-gosx-scene3d-fallback")};})()', returnByValue: true });
    const attrs = attr.result && attr.result.value;
    if (attrs) { s.mountedAttr = attrs.mounted; s.rendererAttr = attrs.renderer; s.fallbackAttr = attrs.fallback; }
    if (attrs && attrs.renderer === 'webgl' && attrs.mounted === 'true') {
      if (s.gl !== 'webgl' && s.gl !== 'webgl2') fail('observed context not WebGL (got ' + s.gl + ')');
    } else fail('data-gosx-scene3d-renderer/mounted attributes missing or not webgl' +
      (attrs ? ' (mounted=' + attrs.mounted + ', renderer=' + attrs.renderer +
        ', fallback=' + attrs.fallback + ')' : ' (attribute read failed)'));

    if (!(s.drawCalls > 0)) fail('no real WebGL draw calls observed');
    if (!(s.triangle6Draws > 0)) fail('no TRIANGLES(mode 4) draws with vertex/index count 6 counted; triangle6Draws=' + s.triangle6Draws + ', total draws=' + s.drawCalls);

    if (!s.strip || !s.strip.pos) fail('topo/patch-prim-0 (TRIANGLE_STRIP) missing');
    else {
      if (s.strip.pos.length !== 18) fail('strip corners length ' + s.strip.pos.length + ' want 18 (6 corners)');
      assertClose(s.strip.pos, STRIP_EXPECTED, 'strip-prim-0');
      if (s.strip.nrm) {
        for (let i = 0; i < 6; i += 1) {
          const nx = s.strip.nrm[i * 3], ny = s.strip.nrm[i * 3 + 1], nz = s.strip.nrm[i * 3 + 2];
          if (![nx, ny, nz].every(Number.isFinite) ||
              Math.abs(nx) >= 1e-3 || Math.abs(ny) >= 1e-3 || Math.abs(nz - 1) >= 1e-3) {
            fail('strip-prim-0 corner ' + i + ' normal=[' + nx + ',' + ny + ',' + nz + '] want [0,0,1]');
          }
        }
      } else fail('strip-prim-0 normals not exposed after extraction');
    }
    if (!s.fan || !s.fan.pos) fail('topo/patch-prim-1 (TRIANGLE_FAN) missing');
    else {
      if (s.fan.pos.length !== 18) fail('fan corners length ' + s.fan.pos.length + ' want 18 (6 corners)');
      assertClose(s.fan.pos, FAN_EXPECTED, 'fan-prim-1');
      if (s.fan.nrm) {
        for (let i = 0; i < 6; i += 1) {
          const nx = s.fan.nrm[i * 3], ny = s.fan.nrm[i * 3 + 1], nz = s.fan.nrm[i * 3 + 2];
          if (![nx, ny, nz].every(Number.isFinite) ||
              Math.abs(nx) >= 1e-3 || Math.abs(ny) >= 1e-3 || Math.abs(nz - 1) >= 1e-3) {
            fail('fan-prim-1 corner ' + i + ' normal=[' + nx + ',' + ny + ',' + nz + '] want [0,0,1]');
          }
        }
      } else fail('fan-prim-1 normals not exposed after extraction');
    }
    if (s.objCount < 2) fail('expected at least 2 unique scene objects, got ' + s.objCount);
  }

  const disp = await send('Runtime.evaluate', {
    expression: 'typeof __gosx_dispose_engine==="function" && ' +
      '(__gosx_dispose_engine("gosx-engine-patch-browser"), true) && ' +
      '!document.getElementById("scene-patch-browser").__gosxScene3DState',
    returnByValue: true });
  if (!(disp.result && disp.result.value === true)) fail('__gosx_dispose_engine did not remove state');

  const evidence = { mounted, drawCalls: s && s.drawCalls, triangle6Draws: s && s.triangle6Draws,
    renderer: s && s.gl,
    mountedAttr: s && s.mountedAttr, rendererAttr: s && s.rendererAttr,
    fallbackAttr: s && s.fallbackAttr, objects: s && s.objCount,
    stripCorners: s && s.strip && s.strip.pos ? s.strip.pos.length / 3 : null,
    fanCorners: s && s.fan && s.fan.pos ? s.fan.pos.length / 3 : null,
    disposeRemovedState: disp.result && disp.result.value === true,
    errors, warnings, note: 'real browser + WebGL verification of TRIANGLE_STRIP/TRIANGLE_FAN ' +
      'primitive loading; actual GL_TRIANGLES draw count reported where visible; ' +
      'GPU hardware acceleration type not certified' };
  console.log(JSON.stringify(evidence, null, 2));
  if (errors.length) exitCode = 1;
})().catch((e) => {
  errors.push(String(e && e.stack || e)); exitCode = 1;
  console.log(JSON.stringify({ errors, warnings, fatal: String(e && e.stack || e) }, null, 2));
})
  .then(async () => {
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
    await new Promise((res) => {
      let done = false; const fin = () => { if (!done) { done = true; res(); } };
      const t = setTimeout(fin, 3000);
      try { server.close(() => { clearTimeout(t); fin(); }); } catch (e) { clearTimeout(t); fin(); }
    });
    setTimeout(() => process.exit(exitCode), 50);
  });
