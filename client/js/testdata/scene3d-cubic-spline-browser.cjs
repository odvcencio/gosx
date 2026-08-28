'use strict';
/* Native browser regression probe for imported glTF CUBICSPLINE animation.
 *
 * Boots real Chrome over CDP (Node builtins only), serves the built
 * bootstrap.js plus a strict basename allowlist of feature assets on
 * localhost, and mounts the Scene3D engine twice — once forced onto native
 * WebGL2 and once onto WebGPU. Native WebGL2 AND WebGPU availability are
 * both required up front; there are no fallbacks and no skips.
 *
 * Per backend, the imported 'curve' clip of the cubic-spline fixture GLB is
 * driven end to end:
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
 *     observed through strictly forwarding wrappers; changed geometry pixels
 *     are decoded and compared as RGBA, not by file size or hash.
 *
 * Any page console error or warning fails the probe. Usage:
 *
 *   node scene3d-cubic-spline-browser.cjs <repoRoot> <existingArtifactDir>
 *
 * Writes report.json and per-case PNGs into the artifact directory. */

const fs = require('fs');
const os = require('os');
const path = require('path');
const http = require('http');
const { spawn } = require('child_process');

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

const CASES = [
  { name: 'gl', webgpu: false, mount: 'scene-cubic-gl', engine: 'gosx-engine-cubic-gl' },
  { name: 'wg', webgpu: true, mount: 'scene-cubic-wg', engine: 'gosx-engine-cubic-wg' },
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

function htmlFor(mount, engine, webgpu) {
  return '<!doctype html><html><head><meta charset="utf-8">' +
    '<link rel="icon" href="data:,"></head><body>' +
    '<div id="' + mount + '" width="' + W + '" height="' + H + '"></div>' +
    '<script type="application/json" id="gosx-manifest">' +
    manifestFor(mount, engine, webgpu) + '</script>' +
    '<script src="/bootstrap.js"></script></body></html>';
}

// Strict basename allowlist: only these basenames are ever served, from a
// bounded set of repo locations; everything else 404s (and is recorded).
const FEATURE_CHUNKS = [
  'bootstrap-feature-scene3d-webgl.js',
  'bootstrap-feature-scene3d-webgpu.js',
  'bootstrap-feature-scene3d-gltf.js',
  'bootstrap-feature-scene3d-animation.js',
];
const ALLOWED_BASENAMES = new Set(['bootstrap.js', 'cubic-spline.glb', ...FEATURE_CHUNKS]);
const CHUNK_CANDIDATE_DIRS = [
  ['client', 'js'],
];
function readRepoBasename(basename) {
  if (!ALLOWED_BASENAMES.has(basename)) return null;
  if (basename === 'cubic-spline.glb') return GLB;
  if (basename === 'bootstrap.js') {
    return fs.readFileSync(path.join(REPO, 'client', 'js', 'bootstrap.js'));
  }
  for (const dir of CHUNK_CANDIDATE_DIRS) {
    const p = path.join(REPO, ...dir, basename);
    if (fs.existsSync(p)) return fs.readFileSync(p);
  }
  return null;
}

const notFound = [];
const server = http.createServer((req, res) => {
  const url = (req.url || '/').split('?')[0];
  const base = url.slice(url.lastIndexOf('/') + 1);
  const send = (body, type) => {
    res.writeHead(200, { 'content-type': type, 'content-length': body.length });
    res.end(body);
  };
  if (url === '/') {
    res.writeHead(200, { 'content-type': 'text/html' });
    res.end('<!doctype html><html><head><meta charset="utf-8">' +
      '<link rel="icon" href="data:,"></head><body>probe-origin</body></html>');
    return;
  }
  if (url === '/case/gl') { send(Buffer.from(htmlFor('scene-cubic-gl', 'gosx-engine-cubic-gl', false)), 'text/html'); return; }
  if (url === '/case/wg') { send(Buffer.from(htmlFor('scene-cubic-wg', 'gosx-engine-cubic-wg', true)), 'text/html'); return; }
  if (ALLOWED_BASENAMES.has(base)) {
    const body = readRepoBasename(base);
    if (body) {
      const type = base.endsWith('.glb') ? 'model/gltf-binary' : 'text/javascript';
      send(body, type);
      return;
    }
  }
  notFound.push(url);
  res.writeHead(404);
  res.end();
});

// ---- CDP plumbing (bounded, strict) ----
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
window.__cubicGLDraws = 0; window.__cubicGLContext = '';
window.__cubicWGPasses = 0; window.__cubicWGSubmits = 0;
(function () {
  function wrapGL(proto) {
    if (!proto) return;
    ['drawArrays', 'drawElements'].forEach(function (name) {
      var orig = proto[name];
      if (!orig) return;
      proto[name] = function () {
        window.__cubicGLDraws += 1;
        window.__cubicGLContext = (this instanceof WebGL2RenderingContext) ? 'webgl2' : 'webgl';
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
      window.__cubicWGPasses += 1;
      return origPass.apply(this, arguments);
    };
  }
  if (typeof GPUQueue !== 'undefined' && GPUQueue.prototype && GPUQueue.prototype.submit) {
    var origSubmit = GPUQueue.prototype.submit;
    GPUQueue.prototype.submit = function () {
      window.__cubicWGSubmits += 1;
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

function compareSampledPose(pose, t, label) {
  assertClose(pose.t0.translation, fixture.evalTranslation(t), label + ' sampled translation', 2e-3);
  assertClose(pose.t0.rotation, fixture.evalRotation(t), label + ' sampled rotation', 2e-3);
  assertClose(pose.t0.scale, fixture.evalScale(t), label + ' sampled scale', 2e-3);
  assertClose(pose.t0.weights, fixture.evalWeights(t), label + ' sampled weights', 2e-3);
}

async function runCase(send, c) {
  const ev = { name: c.name, webgpu: c.webgpu, mount: c.mount, engine: c.engine };

  const loadP = waitForEvent('Page.loadEventFired', MOUNT_WAIT_MS);
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

  const baseline = await capture(send, c.mount);
  writeArtifact(c.name + '-baseline.png', baseline);

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

  const playing = await capture(send, c.mount);
  writeArtifact(c.name + '-playing.png', playing);
  const playDiff = await evalSend(send, diffExpr(baseline, playing), { awaitPromise: true });
  ev.playDiff = playDiff;
  if (!playDiff || !playDiff.dimsMatch) {
    fail('[' + c.name + '] playing screenshot not comparable: ' + JSON.stringify(playDiff));
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
  if ((await evalSend(send, hubExpr(STOP_DETAIL))) !== true) {
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

  const restoredShot = await capture(send, c.mount);
  writeArtifact(c.name + '-restored.png', restoredShot);
  const restoreDiff = await evalSend(send, diffExpr(baseline, restoredShot), { awaitPromise: true });
  ev.restoreDiff = restoreDiff;
  if (!restoreDiff || !restoreDiff.dimsMatch) {
    fail('[' + c.name + '] restored screenshot not comparable: ' + JSON.stringify(restoreDiff));
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

  const disposed = await evalSend(send, disposeExpr(c.engine, c.mount));
  ev.disposed = disposed;
  if (disposed !== true) fail('[' + c.name + '] disposal did not clear engine state');

  return ev;
}

// ---- Owned resources, report, watchdog, and central cleanup ----
const CASE_EVIDENCE = [];
let BASE = '';
let finished = false;
let exitCode = 0;
let reportWriteFailed = false;

function writeReport(extra) {
  const report = Object.assign({
    errors, warnings, notFound, nativeCaps: global.__caps || null, cases: CASE_EVIDENCE,
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
      const exited = new Promise((res) => chrome.once('exit', res));
      try { chrome.kill('SIGKILL'); } catch (e) {}
      await Promise.race([exited, sleep(5000)]);
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
  await new Promise((res, rej) => {
    server.once('error', rej);
    server.listen(0, '127.0.0.1', () => res());
  });
  BASE = 'http://127.0.0.1:' + server.address().port;

  profile = fs.mkdtempSync(path.join(os.tmpdir(), 'gosx-cubic-probe-'));
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
  const caps = await evalSend(send, 'window.__cubicCapsPromise', { awaitPromise: true });
  global.__caps = caps;
  if (!caps || caps.webgl2 !== true || caps.webgpu !== true) {
    throw new Error('native WebGL2 and WebGPU are both required; got ' + JSON.stringify(caps));
  }

  for (let i = 0; i < CASES.length; i += 1) {
    CASE_EVIDENCE.push(await runCase(send, CASES[i]));
  }
})().catch((e) => {
  fail(String(e && e.stack || e));
}).then(async () => {
  if (!finished) {
    finished = true;
    clearTimeout(watchdog);
  }
  await cleanup();
  writeReport({});
  exitCode = (errors.length || warnings.length || reportWriteFailed) ? 1 : 0;
  setTimeout(() => process.exit(exitCode), 50);
});
