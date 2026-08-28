'use strict';

// Prerequisites: Node with a global WebSocket, Go, and Chrome (via
// GOSX_CHROME_BIN). Invocation:
//   node client/js/testdata/scene3d-point-shadow-browser.cjs <repoRoot> <existingArtifactDir>
// GOSX_PROBE_RUNTIME_ROOT swaps ONLY runtime assets (bootstrap/scripts). It
// never changes assertions: every runtime runs the identical strict pixel
// checks, and a stale runtime that fails them exits nonzero naturally.
// Both native APIs (WebGL2 and WebGPU) are required per page, with the exact
// renderer attribute and no fallback; software rasterization is fine.
// Browser point-shadow test: 54 static captures (27 fixture scenes x GL/WG)
// plus 2 live pages (3 fixture transitions each = 6 stages) over CDP, driven
// by spot-shadow-browser-driver.cjs (reused unchanged) with masks from the
// pure point-shadow-oracle.cjs. Scene data comes ONLY from the typed fixture
// emitted by `go run ./client/js/testdata/point-shadow-typed-fixture` (real
// scene.Props -> SceneIR lowering); nothing here hand-authors IR, injects
// state, or uses production shadow math.

const fs = require('fs');
const path = require('path');
const { spawnSync } = require('child_process');
const { startDriver } = require('./spot-shadow-browser-driver.cjs');
const { ORACLE, FACE_NAMES, buildMasks, changedFootprint, verifyFixture, findLight } =
  require('./point-shadow-oracle.cjs');

const W = 320;
const H = 180;
const READY_TIMEOUT_MS = 20000;
const CASE_WAIT_MS = 5000;
const WATCHDOG_MS = 360000;
const BASE_SCENES = ['off', 'on', 'ambient-only'];
const NZ_SCENES = ['off', 'on', 'ambient-only', 'no-caster', 'no-receiver', 'discarded',
  'equal', 'moved', 'moved-off', 'slot1', 'slot0-paired', 'mixed-slot1'];
const ALL = new Array(W * H);
for (let i = 0; i < ALL.length; i++) ALL[i] = i;

const num = (v) => (typeof v === 'number' && Number.isFinite(v)) ? v : 0;
function pos3(o) {
  if (Array.isArray(o)) return [num(o[0]), num(o[1]), num(o[2])];
  if (o && typeof o === 'object' && ('x' in o || 'y' in o || 'z' in o)) return [num(o.x), num(o.y), num(o.z)];
  return null;
}
function vecEq(a, b, eps) {
  return Math.abs(a[0] - b[0]) <= eps && Math.abs(a[1] - b[1]) <= eps && Math.abs(a[2] - b[2]) <= eps;
}

function loadFixture(repo) {
  const r = spawnSync('go', ['run', './client/js/testdata/point-shadow-typed-fixture'],
    { cwd: repo, timeout: 60000, maxBuffer: 8 * 1024 * 1024, encoding: 'utf8' });
  if (r.status !== 0) throw new Error('fixture build failed: ' + (r.stderr || r.stdout || 'exit ' + r.status));
  return JSON.parse(r.stdout);
}

function manifestFor(faceFixture, sceneIR, webgpu, mountId, engineId) {
  const cam = faceFixture.camera || {};
  const p = {
    width: W, height: H, maxDevicePixelRatio: 1, autoRotate: false,
    animation: false, responsive: false, background: '#000000',
    forceWebGL: !webgpu, requireWebGL: !webgpu, preferWebGPU: Boolean(webgpu),
    camera: {
      x: num(cam.x), y: num(cam.y), z: num(cam.z),
      rotationX: num(cam.rotationX), rotationY: num(cam.rotationY), rotationZ: num(cam.rotationZ),
      fov: num(cam.fov) || 50,
    },
    environment: {
      ambientColor: '#000000', skyColor: '#000000', groundColor: '#000000',
      ambientIntensity: 0, skyIntensity: 0, groundIntensity: 0,
    },
    lights: sceneIR.lights, objects: sceneIR.objects, // actual fixture IR verbatim
  };
  return JSON.stringify({ engines: [{ id: engineId, component: 'GoSXScene3D', kind: 'surface', mountId, props: p }] });
}

function htmlFor(mount, manifest) {
  return '<!doctype html><html><head><meta charset="utf-8">' +
    '<link rel="icon" href="data:,">' +
    '<style>html,body{margin:0;padding:0;}canvas{display:block;}</style></head><body>' +
    '<div id="' + mount + '" width="' + W + '" height="' + H + '"></div>' +
    '<script type="application/json" id="gosx-manifest">' + manifest +
    '</script><script src="/bootstrap.js"></script></body></html>';
}

// Strictly forwarding counter wrappers: unchanged this/args, original return.
const PRELOAD = [
  'window.__probeGLDraws=0;window.__probeGLContext=null;',
  'window.__probeWGPasses=0;window.__probeWGSubmits=0;',
  '(function(){function wrapGL(p){if(!p)return;',
  '["drawArrays","drawElements"].forEach(function(n){var o=p[n];if(!o)return;',
  'p[n]=function(){window.__probeGLDraws+=1;',
  'window.__probeGLContext=(this instanceof WebGL2RenderingContext)?"webgl2":"webgl";',
  'return o.apply(this,arguments);};});}',
  'wrapGL(typeof WebGLRenderingContext!=="undefined"?WebGLRenderingContext.prototype:null);',
  'wrapGL(typeof WebGL2RenderingContext!=="undefined"?WebGL2RenderingContext.prototype:null);',
  'if(typeof GPUCommandEncoder!=="undefined"&&GPUCommandEncoder.prototype&&GPUCommandEncoder.prototype.beginRenderPass){',
  'var op=GPUCommandEncoder.prototype.beginRenderPass;',
  'GPUCommandEncoder.prototype.beginRenderPass=function(){window.__probeWGPasses+=1;return op.apply(this,arguments);};}',
  'if(typeof GPUQueue!=="undefined"&&GPUQueue.prototype&&GPUQueue.prototype.submit){',
  'var os=GPUQueue.prototype.submit;',
  'GPUQueue.prototype.submit=function(){window.__probeWGSubmits+=1;return os.apply(this,arguments);};}',
  'var c=document.createElement("canvas");var gl2=false;try{gl2=!!c.getContext("webgl2");}catch(e){gl2=false;}',
  'window.__probeCapsPromise=(navigator.gpu&&navigator.gpu.requestAdapter)?',
  'navigator.gpu.requestAdapter().then(function(a){return {webgl2:gl2,webgpu:!!a};},',
  'function(){return {webgl2:gl2,webgpu:false};}):Promise.resolve({webgl2:gl2,webgpu:false});',
  '})();',
].join('\n');

const COUNTERS_EXPR = '({draws:window.__probeGLDraws,context:window.__probeGLContext,' +
  'wgPasses:window.__probeWGPasses,wgSubmits:window.__probeWGSubmits})';

// Exact backend attribute or failure; any fallback attribute is rejected.
function readyExpr(mount, be) {
  const okRenderer = be === 'wg' ? '(r==="webgpu")' : '(r==="webgl"||r==="webgl2")';
  return '(function(){var m=document.getElementById(' + JSON.stringify(mount) + ');' +
    'if(!m)return {ok:false,why:"no mount"};' +
    'if(m.getAttribute("data-gosx-scene3d-mounted")!=="true")return {ok:false,why:"not mounted"};' +
    'var cv=m.querySelector("canvas");if(!cv)return {ok:false,why:"no canvas"};' +
    'if(!m.__gosxScene3DState)return {ok:false,why:"no state"};' +
    'var r=m.getAttribute("data-gosx-scene3d-renderer")||"";' +
    'if(!' + okRenderer + ')return {ok:false,why:"renderer "+r};' +
    'var fb=m.getAttribute("data-gosx-scene3d-renderer-fallback");' +
    'if(fb)return {ok:false,why:"fallback "+fb};' +
    'return {ok:true,renderer:r};})()';
}

async function waitReady(driver, mount, be) {
  const deadline = Date.now() + READY_TIMEOUT_MS;
  for (;;) {
    const st = await driver.eval(readyExpr(mount, be));
    if (st && st.ok) return st;
    if (Date.now() > deadline) throw new Error('readiness timeout for ' + mount + ': ' + JSON.stringify(st));
    await new Promise((r) => setTimeout(r, 200));
  }
}

function settleFramesExpr(n) {
  return '(function(){return new Promise(function(res){var left=' + n + ';' +
    'function step(){if(left--<=0){res(true);return;}requestAnimationFrame(step);}' +
    'requestAnimationFrame(step);});})()';
}
async function settleFrames(driver, n) {
  if (await driver.eval(settleFramesExpr(n), true) !== true) throw new Error('settling frames failed');
}

// Real screenshot -> Image -> 2D canvas -> RGBA bytes back to Node.
const DECODE_HEAD = '(function(){var img=new Image();return new Promise(function(res,rej){' +
  'img.onload=function(){try{if(img.width!==' + W + '||img.height!==' + H + '){' +
  'rej(new Error("decoded "+img.width+"x"+img.height));return;}' +
  'var c=document.createElement("canvas");c.width=' + W + ';c.height=' + H + ';' +
  'var g=c.getContext("2d");if(!g){rej(new Error("no 2d context"));return;}' +
  'g.drawImage(img,0,0);var d=g.getImageData(0,0,' + W + ',' + H + ').data;' +
  'var bin="";for(var i=0;i<d.length;i++)bin+=String.fromCharCode(d[i]);res(btoa(bin));' +
  '}catch(e){rej(e);}};img.onerror=function(){rej(new Error("screenshot decode error"));};';

async function captureRGBA(driver, mount) {
  const png = await driver.capture(mount);
  const b64 = await driver.eval(
    DECODE_HEAD + 'img.src="data:image/png;base64,' + png.toString('base64') + '";})})()', true);
  if (typeof b64 !== 'string' || !b64.length) throw new Error('screenshot readback failed for ' + mount);
  const rgba = Buffer.from(b64, 'base64');
  if (rgba.length !== W * H * 4) throw new Error('readback size ' + rgba.length + ' != ' + W * H * 4);
  for (let o = 3; o < rgba.length; o += 4) {
    if (rgba[o] !== 255) throw new Error('readback for ' + mount + ' has non-opaque alpha ' + rgba[o]);
  }
  // A blank capture is a HARD failure, never a substitute result (this is
  // what keeps any runtime swap from degrading into a skip).
  let nonEmpty = 0;
  for (let i = 0; i < rgba.length; i += 4 * 249) {
    if (rgba[i] || rgba[i + 1] || rgba[i + 2]) nonEmpty++;
  }
  if (!nonEmpty) throw new Error('blank readback for ' + mount);
  return { png, rgba };
}

const lumaAt = (rgba, i) => {
  const o = i * 4;
  return 0.2126 * rgba[o] + 0.7152 * rgba[o + 1] + 0.0722 * rgba[o + 2];
};
function deltaStats(a, b, idxs) {
  let min = Infinity; let max = -Infinity; let sum = 0;
  for (const i of idxs) {
    const o = i * 4;
    const d = Math.max(Math.abs(a[o] - b[o]), Math.abs(a[o + 1] - b[o + 1]), Math.abs(a[o + 2] - b[o + 2]));
    if (d < min) min = d;
    if (d > max) max = d;
    sum += d;
  }
  return { count: idxs.length, min, max, mean: sum / Math.max(1, idxs.length) };
}
function lumaStats(lit, off, idxs) {
  let min = Infinity; let max = -Infinity; let sum = 0;
  for (const i of idxs) {
    const d = lumaAt(off, i) - lumaAt(lit, i); // positive = lit is darker
    if (d < min) min = d;
    if (d > max) max = d;
    sum += d;
  }
  return { count: idxs.length, min, max, mean: sum / Math.max(1, idxs.length) };
}
function litFraction(rgba, idxs) {
  let n = 0;
  for (const i of idxs) if (lumaAt(rgba, i) > 20) n++;
  return n / Math.max(1, idxs.length);
}
function changedPixels(a, b, idxs) {
  let n = 0;
  for (const i of idxs) {
    const o = i * 4;
    if (Math.abs(a[o] - b[o]) > 2 || Math.abs(a[o + 1] - b[o + 1]) > 2 || Math.abs(a[o + 2] - b[o + 2]) > 2) n++;
  }
  return n;
}

// Pixel assertions. Failure is recorded, never fatal mid-run, never converted
// into a skip, and never gated on engine attributes or runtime overrides.
function checkStatic(be, face, sn, shots, masksOn, masksMoved, diag) {
  const errs = [];
  const key = (s) => shots[be + '|' + face + '|' + s];
  const rec = (n, s) => { diag[be + '|' + face + '|' + n] = s; return s; };
  if (sn === 'on') {
    if (masksOn.interior.length < 20) errs.push(be + '/' + face + ': interior mask too small: ' + masksOn.interior.length);
    if (masksOn.exterior.length < 50) errs.push(be + '/' + face + ': exterior mask too small: ' + masksOn.exterior.length);
    const dark = rec('interior-darken', lumaStats(key('on'), key('off'), masksOn.interior));
    if (!(dark.min > 12)) errs.push(be + '/' + face + ': interior min darkening ' + dark.min + ' <= 12 vs off');
    const amb = rec('interior-vs-ambient', deltaStats(key('on'), key('ambient-only'), masksOn.interior));
    if (!(amb.max <= 2)) errs.push(be + '/' + face + ': interior maxRGB vs ambient-only ' + amb.max + ' > 2');
    const ext = rec('exterior-vs-off', deltaStats(key('on'), key('off'), masksOn.exterior));
    if (!(ext.max <= 2)) errs.push(be + '/' + face + ': exterior maxRGB vs off ' + ext.max + ' > 2');
    const lit = rec('exterior-lit', { count: masksOn.exterior.length, litFraction: litFraction(key('on'), masksOn.exterior) });
    if (!(lit.litFraction > 0.5)) errs.push(be + '/' + face + ': exterior lit coverage ' + lit.litFraction + ' too low');
  } else if (face === 'nz' && sn === 'moved') {
    const dark = rec('moved-interior-darken', lumaStats(key('moved'), key('moved-off'), masksMoved.interior));
    if (!(dark.min > 12)) errs.push(be + '/moved: interior min darkening ' + dark.min + ' <= 12 vs moved-off');
    const amb = rec('moved-interior-vs-ambient', deltaStats(key('moved'), key('ambient-only'), masksMoved.interior));
    if (!(amb.max <= 2)) errs.push(be + '/moved: interior maxRGB vs ambient-only ' + amb.max + ' > 2');
    const ext = rec('moved-exterior-vs-off', deltaStats(key('moved'), key('moved-off'), masksMoved.exterior));
    if (!(ext.max <= 2)) errs.push(be + '/moved: exterior maxRGB vs moved-off ' + ext.max + ' > 2');
    const changed = rec('moved-receiver-changed', { count: changedPixels(key('moved'), key('on'), masksOn.receiver) });
    if (!(changed.count > 20)) errs.push(be + '/moved: receiver changed pixels vs on = ' + changed.count);
  } else if (face === 'nz' && (sn === 'no-caster' || sn === 'no-receiver' || sn === 'discarded')) {
    const d = rec(sn + '-vs-off', deltaStats(key(sn), key('off'), masksOn.receiverMargin));
    if (!(d.max <= 2)) errs.push(be + '/' + sn + ': expected match with off on robust receiver ROI, max=' + d.max);
  } else if (face === 'nz' && (sn === 'equal' || sn === 'slot1' || sn === 'slot0-paired' || sn === 'mixed-slot1')) {
    const d = rec(sn + '-vs-on', deltaStats(key(sn), key('on'), masksOn.receiverMargin));
    if (!(d.max <= 2)) errs.push(be + '/' + sn + ': expected match with on on receiver ROI, max=' + d.max);
  }
  return errs;
}

const disposeExpr = (engine, mount) =>
  '(function(){try{if(typeof __gosx_dispose_engine!=="function")return false;' +
  '__gosx_dispose_engine(' + JSON.stringify(engine) + ');' +
  'var m=document.getElementById(' + JSON.stringify(mount) + ');' +
  'return !!(m&&!m.__gosxScene3DState);}catch(e){return false;}})()';

const refsSetExpr = (mount) =>
  '(function(){var m=document.getElementById(' + JSON.stringify(mount) + ');' +
  'if(!m||m.getAttribute("data-gosx-scene3d-mounted")!=="true")return false;' +
  'var cv=m.querySelector("canvas");var st=m.__gosxScene3DState;' +
  'if(!cv||!st)return false;window.__liveRefs={mount:m,canvas:cv,state:st};return true;})()';
const refsCheckExpr = () =>
  '(function(){var r=window.__liveRefs;if(!r)return null;' +
  'var m=document.getElementById(r.mount.id);' +
  'var cv=m&&m.querySelector("canvas");var st=m&&m.__gosxScene3DState;' +
  'return {sameMount:m===r.mount,sameCanvas:cv===r.canvas,sameState:st===r.state};})()';

// Read the live point light's OWN state: shadow flag + position (+ range).
function lightStateExpr(mount, id) {
  return '(function(){var m=document.getElementById(' + JSON.stringify(mount) + ');' +
    'var st=m&&m.__gosxScene3DState;' +
    'if(!st||!(st.lights&&st.lights.get))return null;' +
    'var L=st.lights.get(' + JSON.stringify(id) + ');' +
    'if(!L)return null;return {castShadow:L.castShadow===true,' +
    'x:L.x,y:L.y,z:L.z,range:L.range,intensity:L.intensity};})()';
}

// Bounded ack: revision + commandCount on gosx:scene3d:commands-applied,
// with a hard 5s timer — the same pattern as the spot live proof.
function dispatchExpr(mount, cmds, revision, commandCount) {
  return '(function(){return new Promise(function(res){' +
    'var cmds=JSON.parse(' + JSON.stringify(JSON.stringify(cmds)) + ');' +
    'var REV=' + Number(revision) + ',COUNT=' + Number(commandCount) + ',WAIT=' + CASE_WAIT_MS + ';' +
    'var m=document.getElementById(' + JSON.stringify(mount) + ');' +
    'if(!m||typeof m.dispatchEvent!=="function")return res(false);' +
    'var timer=null,h=null,done=false;' +
    'var finish=function(v){if(done)return;done=true;' +
    'if(timer!==null)clearTimeout(timer);' +
    'if(m.removeEventListener)m.removeEventListener("gosx:scene3d:commands-applied",h);res(v);};' +
    'h=function(ev){var d=(ev&&ev.detail)||{};' +
    'if(typeof d.revision==="number"&&d.revision===REV&&' +
    'typeof d.commandCount==="number"&&d.commandCount===COUNT)finish(true);};' +
    'm.addEventListener("gosx:scene3d:commands-applied",h);' +
    'timer=setTimeout(function(){finish(false);},WAIT);' +
    'try{m.dispatchEvent(new CustomEvent("gosx:scene3d:commands",' +
    '{detail:{revision:REV,commands:cmds}}));}catch(e){finish(false);}});})()';
}

function checkLightState(be, stage, got, want, errs) {
  for (const f of ['x', 'y', 'z', 'range', 'intensity']) {
    if (Math.abs(num(got[f]) - num(want[f])) > 1e-6) {
      errs.push(be + '/' + stage + ': light ' + f + '=' + got[f] + ' want ' + want[f]);
    }
  }
  if (got.castShadow !== (want.castShadow === true)) errs.push(be + '/' + stage + ': light castShadow=' + got.castShadow);
}

async function main() {
  const repo = process.argv[2];
  const art = process.argv[3];
  if (!repo || !fs.existsSync(repo) || !art || !fs.existsSync(art)) {
    throw new Error('usage: node scene3d-point-shadow-browser.cjs <repoRoot> <existingArtifactDir>');
  }
  // Preflight: build the ACTUAL fixture through the real Go lowering and
  // validate all geometry/controls against the pure oracle BEFORE pixels.
  const fx = loadFixture(repo);
  verifyFixture(fx);
  const masksOn = buildMasks({ position: ORACLE.basePosition.slice(), castShadow: true });
  const masksMoved = buildMasks({ position: ORACLE.movedPosition.slice(), castShadow: true });
  if (changedFootprint(masksOn, masksMoved).length === 0) {
    throw new Error('oracle masks for base and moved lights are identical');
  }
  if (masksMoved.interior.length < 20 || masksMoved.exterior.length < 50 ||
      masksMoved.receiver.length < masksMoved.receiverMargin.length) {
    throw new Error('moved masks are vacuous: interior=' + masksMoved.interior.length +
      ' exterior=' + masksMoved.exterior.length + ' receiver=' + masksMoved.receiver.length);
  }
  const diag = { masks: {
    interior: masksOn.interior.length, exterior: masksOn.exterior.length,
    receiver: masksOn.receiver.length, receiverMargin: masksOn.receiverMargin.length,
    movedInterior: masksMoved.interior.length, movedExterior: masksMoved.exterior.length,
  } };
  const pages = {};
  const staticMeta = [];
  for (const be of ['gl', 'wg']) {
    for (const face of FACE_NAMES) {
      const F = fx.faces[face];
      for (const sn of (face === 'nz' ? NZ_SCENES : BASE_SCENES)) {
        const mount = 'pt-' + be + '-' + face + '-' + sn;
        pages['/' + mount] = htmlFor(mount, manifestFor(F, F.scenes[sn], be === 'wg', mount, 'eng-' + mount));
        staticMeta.push({ be, face, sn, mount });
      }
    }
  }
  if (staticMeta.length !== 54) throw new Error('planned static pages != 54: ' + staticMeta.length);
  const liveMeta = [];
  for (const be of ['gl', 'wg']) {
    const mount = 'pt-live-' + be;
    pages['/' + mount] = htmlFor(mount, manifestFor(fx.faces.nz, fx.faces.nz.scenes.off, be === 'wg', mount, 'eng-' + mount));
    liveMeta.push({ be, mount });
  }
  const runtimeRoot = process.env.GOSX_PROBE_RUNTIME_ROOT || path.join(repo, 'client', 'js');
  const driver = await startDriver({ repoRoot: repo, runtimeRoot, pages, preload: PRELOAD });
  module.exports.__activeDriver = driver; // watchdog cleanup handle

  // From here on the run ALWAYS produces a report, written in a finally.
  const assertionErrors = [];
  const capabilities = {};
  const shots = {};
  const liveEvidence = [];
  let executedStatic = 0;
  let executedLiveCases = 0;
  let executedLiveStages = 0;
  let fatal = null;
  try {
    await driver.send('Emulation.setDeviceMetricsOverride', { width: W, height: H, deviceScaleFactor: 1, mobile: false });
    // ---- 54 static captures + disposals (all run even if assertions fail) ----
    for (const meta of staticMeta) {
      const label = meta.be + '/' + meta.face + '/' + meta.sn;
      await driver.load('/' + meta.mount);
      const ready = await waitReady(driver, meta.mount, meta.be);
      await settleFrames(driver, 8);
      const counters = await driver.eval(COUNTERS_EXPR);
      const caps = await driver.eval('window.__probeCapsPromise', true);
      // Hard requirement: real WebGL2 AND WebGPU available, recorded per case.
      if (!(caps && caps.webgl2 === true && caps.webgpu === true)) {
        assertionErrors.push(label + ': capabilities missing, webgl2=' + JSON.stringify(caps && caps.webgl2) +
          ' webgpu=' + JSON.stringify(caps && caps.webgpu));
      }
      if (meta.be === 'gl') {
        if (!(counters && counters.draws > 0)) assertionErrors.push(label + ': no native GL draws');
        else if (counters.context !== 'webgl2') {
          assertionErrors.push(label + ': GL draws did not run on WebGL2 (context=' + counters.context + ')');
        }
      } else if (!(counters && counters.wgPasses > 0 && counters.wgSubmits > 0)) {
        assertionErrors.push(label + ': no native WG render pass/submit');
      }
      capabilities[label] = { renderer: ready.renderer, counters, caps };
      const shot = await captureRGBA(driver, meta.mount);
      shots[meta.be + '|' + meta.face + '|' + meta.sn] = shot.rgba;
      fs.writeFileSync(path.join(art, meta.mount + '.png'), shot.png);
      executedStatic += 1; // actual completed capture, never planned-only
      const disposalOk = await driver.eval(disposeExpr('eng-' + meta.mount, meta.mount)) === true;
      if (!disposalOk) assertionErrors.push(label + ': engine disposal did not clear state');
      capabilities[label].disposalClearedState = disposalOk;
    }
    // Static pixel checks after all captures so both backends are always collected.
    for (const meta of staticMeta) {
      for (const e of checkStatic(meta.be, meta.face, meta.sn, shots, masksOn, masksMoved, diag)) {
        assertionErrors.push(e);
      }
    }
    // ---- 2 live pages, 3 fixture transitions each (6 stages), nz face ----
    for (const live of liveMeta) {
      const be = live.be;
      await driver.load('/' + live.mount);
      await waitReady(driver, live.mount, be);
      await settleFrames(driver, 8);
      if (await driver.eval(refsSetExpr(live.mount)) !== true) {
        throw new Error(be + ' live: could not retain mount/canvas/state references');
      }
      let counters = await driver.eval(COUNTERS_EXPR);
      const base = await captureRGBA(driver, live.mount);
      fs.writeFileSync(path.join(art, live.mount + '-off-baseline.png'), base.png);
      const baseDelta = deltaStats(base.rgba, shots[be + '|nz|off'], ALL);
      diag['live-' + be + '-baseline-vs-static-off'] = baseDelta;
      if (baseDelta.max !== 0) {
        assertionErrors.push(be + ' live: off baseline not exact full RGBA vs static off, max=' + baseDelta.max);
      }
      let revision = 0;
      let stage = 0;
      for (const tr of fx.faces.nz.transitions) {
        stage += 1;
        revision += 1;
        const acked = await driver.eval(dispatchExpr(live.mount, tr.commands, revision, tr.commands.length), true) === true;
        if (!acked) throw new Error(be + ' live stage ' + tr.name + ': commands not applied (revision ' + revision + ')');
        await settleFrames(driver, 8);
        const wantLight = findLight(fx.faces.nz.scenes[tr.to], ORACLE.keyID);
        const L = await driver.eval(lightStateExpr(live.mount, ORACLE.keyID));
        if (!L) throw new Error(be + ' live stage ' + tr.name + ': key point light missing from state.lights');
        checkLightState(be, tr.name, L, wantLight, assertionErrors);
        const next = await driver.eval(COUNTERS_EXPR);
        const countersBefore = counters;
        let drawsAdvanced = null; let passesAdvanced = null; let submitsAdvanced = null;
        if (be === 'gl') {
          drawsAdvanced = next.draws > countersBefore.draws;
          if (!drawsAdvanced) assertionErrors.push(be + '/' + tr.name + ': native GL draws did not advance');
        } else {
          passesAdvanced = next.wgPasses > countersBefore.wgPasses;
          submitsAdvanced = next.wgSubmits > countersBefore.wgSubmits;
          if (!passesAdvanced || !submitsAdvanced) {
            assertionErrors.push(be + '/' + tr.name + ': native WG counters did not both advance (passes=' +
              passesAdvanced + ', submits=' + submitsAdvanced + ')');
          }
        }
        counters = next;
        const id = await driver.eval(refsCheckExpr());
        const identityOk = Boolean(id && id.sameMount && id.sameCanvas && id.sameState);
        if (!identityOk) assertionErrors.push(be + '/' + tr.name + ': engine identity changed');
        const shot = await captureRGBA(driver, live.mount);
        fs.writeFileSync(path.join(art, live.mount + '-stage' + stage + '-' + tr.name + '.png'), shot.png);
        const cmp = deltaStats(shot.rgba, shots[be + '|nz|' + tr.to], ALL);
        diag['live-' + be + '-' + tr.name] = cmp;
        if (tr.to === 'off') {
          if (cmp.max !== 0) assertionErrors.push(be + '/' + tr.name + ': off restoration not exact full RGBA, max=' + cmp.max);
        } else if (cmp.max > 2) {
          assertionErrors.push(be + '/' + tr.name + ': capture differs from static ' + tr.to + ', max=' + cmp.max);
        }
        executedLiveStages += 1;
        liveEvidence.push({
          backend: be, stage, transition: tr.name, to: tr.to,
          ackRevision: revision, commandCount: tr.commands.length, commandsApplied: acked,
          readbackLight: L, countersBefore, countersAfter: next,
          drawsAdvanced, passesAdvanced, submitsAdvanced,
          identity: id, identityStable: identityOk, rgbDelta: cmp,
        });
      }
      const disposalOk = await driver.eval(disposeExpr('eng-' + live.mount, live.mount)) === true;
      if (!disposalOk) assertionErrors.push(be + ' live: final disposal did not clear state');
      executedLiveCases += 1;
      liveEvidence.push({ backend: be, stage: 'final', transition: 'dispose', disposalClearedState: disposalOk });
    }
  } catch (e) {
    fatal = (e && e.message) ? e.message : String(e);
  } finally {
    try { await driver.close(); } catch (e) { if (!fatal) fatal = 'driver close failed: ' + ((e && e.message) || String(e)); }
    module.exports.__activeDriver = null;
    const driverErrors = Array.isArray(driver.errors) ? driver.errors.slice() : [];
    const errors = assertionErrors.concat(driverErrors);
    const report = {
      mode: 'proof',
      runtimeRoot,
      executedStatic, executedLiveCases, executedLiveStages,
      plannedStatic: staticMeta.length, plannedLiveCases: liveMeta.length,
      plannedLiveStages: liveMeta.length * fx.faces.nz.transitions.length,
      failedAssertions: assertionErrors.length,
      driverErrors,
      maskCounts: diag.masks, capabilities, diagnostics: diag, liveEvidence,
      errors, fatal, warnings: driver.warnings, notFound: driver.notFound,
    };
    fs.writeFileSync(path.join(art, 'report.json'), JSON.stringify(report, null, 2));
    const countsOk = executedStatic === 54 && executedLiveCases === 2 && executedLiveStages === 6;
    if (errors.length || driver.warnings.length || driver.notFound.length || fatal || !countsOk) {
      console.error('point-shadow browser test FAILED (' + report.mode + '):', JSON.stringify({
        fatal, errors: errors.length, warnings: driver.warnings.length,
        notFound: driver.notFound.length, executedStatic, executedLiveCases, executedLiveStages,
      }));
      process.exitCode = 1;
    } else {
      console.log('point-shadow browser test (' + report.mode + '): 54 static + 2 live (6 stages) cases complete');
    }
  }
}

if (require.main === module) {
  const watchdog = setTimeout(async () => {
    console.error('point-shadow browser test: 360s watchdog expired');
    try { if (module.exports.__activeDriver) await module.exports.__activeDriver.close(); } catch (e) { /* best effort */ }
    process.exit(1);
  }, WATCHDOG_MS);
  main().then(() => { clearTimeout(watchdog); if (process.exitCode) process.exit(process.exitCode); })
    .catch((e) => { clearTimeout(watchdog); console.error('preflight fatal:', e && e.message); process.exit(1); });
}

module.exports = { __activeDriver: null };
