'use strict';
/* Temporary browser-verification probe: real Chrome + WebGL playback of ordinary
 * glTF node TRS animation from client/js/bootstrap.js. Node builtin-only. */
const fs = require('fs'), os = require('os'), path = require('path');
const http = require('http'), vm = require('vm');
const { spawn } = require('child_process');

const REPO = process.argv[2];
if (!REPO) { console.error('usage: node trs-browser-verify.js <repoRoot>'); process.exit(2); }
const errors = [], warnings = [];
const fail = (m) => { errors.push(m); };

// ---- Bounded VM extraction of the GLB fixture from the existing test file ----
const testPath = path.join(REPO, 'client', 'js', 'scene3d-node-animation.test.js');
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

const manifest = JSON.stringify({ engines: [{ id: 'gosx-engine-trs-browser', component: 'GoSXScene3D',
  kind: 'surface', mountId: 'scene-trs-browser',
  props: { width: 320, height: 180, autoRotate: false,
    forceWebGL: true, requireWebGL: true,
    models: [{ id: 'trs', src: '/models/trs.glb', animation: 'move', loop: true, static: true }] } }] });
const html = '<!doctype html><html><head><meta charset="utf-8"></head><body>' +
  '<div id="scene-trs-browser" width="320" height="180"></div>' +
  '<script type="application/json" id="gosx-manifest">' + manifest + '</script>' +
  '<script src="/bootstrap.js"></script></body></html>';

const server = http.createServer((req, res) => {
  if (req.url === '/models/trs.glb') {
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

// ---- CDP plumbing ----
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
window.__gosxDRAWS = 0; window.__gosxGL = null;
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
  ['bufferData', 'bufferSubData'].forEach(function (n) {
    const o = proto[n]; if (!o) return;
    proto[n] = function () { return o.apply(this, arguments); };
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
  profile = fs.mkdtempSync(path.join(os.tmpdir(), 'gosx-trs-probe-'));
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

  const POLL = '(()=>{const m=document.getElementById("scene-trs-browser");' +
    'const s=m&&m.__gosxScene3DState;' +
    'return !!(s&&s._modelSkins&&s._modelSkins.length===1&&s.objects&&s.objects.size);})()';
  let mounted = false;
  for (let i = 0; i < 100; i += 1) {
    const r = await send('Runtime.evaluate', { expression: POLL, returnByValue: true });
    if (r.result && r.result.value === true) { mounted = true; break; }
    await new Promise((res) => setTimeout(res, 100));
  }
  if (!mounted) fail('engine did not mount with exactly one _modelSkins record');

  // Real frames: flat-tail clip means any sample after ~0.2s is the final pose.
  await new Promise((res) => setTimeout(res, 1500));

  const READ = '(()=>{const m=document.getElementById("scene-trs-browser");const s=m&&m.__gosxScene3DState;' +
    'if(!s)return null;' +
    'const meshPos=(r)=>(r&&r.vertices&&r.vertices.positions)?Array.from(r.vertices.positions):null;' +
    'const pe=s.points.find((p)=>p.id==="trs/tri-points-1");' +
    'const lo=s.objects.get("trs/filament-lines-0");' +
    'return {drawCalls:window.__gosxDRAWS,gl:window.__gosxGL,skins:s._modelSkins.length,' +
    'tri:meshPos(s.objects.get("trs/tri-prim-0")),ghost:meshPos(s.objects.get("trs/ghost-prim-0")),' +
    'pt:(pe&&pe.positions)?Array.from(pe.positions):null,' +
    'line:(lo&&lo.points)?lo.points.map((v)=>({x:v.x,y:v.y,z:v.z})):null};})()';
  const read = await send('Runtime.evaluate', { expression: READ, returnByValue: true });
  const s = read.result && read.result.value;
  if (!s) { fail('could not read __gosxScene3DState'); }
  else {
    const attr = await send('Runtime.evaluate', { expression:
      '(()=>{const m=document.getElementById("scene-trs-browser");' +
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
    const attempts = await send('Runtime.evaluate', { expression:
      'JSON.stringify(window.__gosx_scene3d_backend_attempts||[])', returnByValue: true });
    s.backendAttempts = attempts.result && attempts.result.value || 'unavailable';
    if (!(s.drawCalls > 0)) fail('no real WebGL draw calls observed');
    assertClose(s.tri, [5, 0, 0, 7, 0, 0, 5, 2, 0], 'tri-prim-0');
    assertClose(s.pt, [9, 0, 0], 'tri-points-1');
    if (s.line) {
      assertClose([s.line[0].x, s.line[0].y, s.line[0].z], [8, 0, 0], 'filament-lines-0[0]');
      assertClose([s.line[1].x, s.line[1].y, s.line[1].z], [9, 0, 0], 'filament-lines-0[1]');
    } else fail('filament-lines-0 missing');
    assertClose(s.ghost, [5, 0, 0, 6, 0, 0, 5, 1, 0], 'ghost-prim-0');
    if (s.skins !== 1) fail('_modelSkins length ' + s.skins);
  }

  const disp = await send('Runtime.evaluate', {
    expression: 'typeof __gosx_dispose_engine==="function" && ' +
      '(__gosx_dispose_engine("gosx-engine-trs-browser"), true) && ' +
      '!document.getElementById("scene-trs-browser").__gosxScene3DState',
    returnByValue: true });
  if (!(disp.result && disp.result.value === true)) fail('__gosx_dispose_engine did not remove state');

  const evidence = { mounted, drawCalls: s && s.drawCalls, renderer: s && s.gl,
    mountedAttr: s && s.mountedAttr, rendererAttr: s && s.rendererAttr,
    fallbackAttr: s && s.fallbackAttr,
    backendAttempts: s && s.backendAttempts,
    skins: s && s.skins, objects: s && s.tri ? 'read' : 'unread',
    disposeRemovedState: disp.result && disp.result.value === true,
    errors, warnings, note: 'real browser + WebGL verification; GPU acceleration type ' +
      'not certified (renderer backend not queried); no actual WASM test' };
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
