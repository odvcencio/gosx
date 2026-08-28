'use strict';

// Prerequisites: Node with a global WebSocket, Go, and Chrome (via
// GOSX_CHROME_BIN). Invocation:
//   node client/js/testdata/scene3d-spot-shadow-browser.cjs <repoRoot> <existingArtifactDir>
// GOSX_PROBE_RUNTIME_ROOT swaps only runtime assets for the pre-feature
// negative control. Both native APIs (WebGL2 and WebGPU) are required; the
// run may use software rasterization.
// Browser spot-shadow test: 24 static captures (12 fixture scenes x GL/WG)
// plus 2 live pages over CDP, driven by spot-shadow-browser-driver.cjs with
// masks from the pure spot-shadow-oracle.cjs. Scene data comes ONLY from the
// typed fixture emitted by `go run ./client/js/testdata/spot-shadow-typed-fixture`
// (real scene.Props -> SceneIR lowering); nothing here hand-authors IR.

const fs = require('fs');
const path = require('path');
const { spawnSync } = require('child_process');
const { startDriver } = require('./spot-shadow-browser-driver.cjs');
const { ORACLE, buildMasks } = require('./spot-shadow-oracle.cjs');

const W = 320;
const H = 180;
const READY_TIMEOUT_MS = 20000;
const CASE_WAIT_MS = 5000;
const WATCHDOG_MS = 240000;
const LIGHT_ID = 'typed-spot-key';
const SCENES = ['off', 'on', 'ambient-only', 'no-caster', 'no-receiver', 'discarded',
  'equal', 'moved', 'moved-off', 'aimed', 'aimed-off', 'invalid-prefix'];
const CHAIN = ['off', 'on', 'moved', 'aimed', 'off'];
const ALL = new Array(W * H);
for (let i = 0; i < ALL.length; i++) ALL[i] = i;

const num = (v) => (typeof v === 'number' && Number.isFinite(v)) ? v : 0;
function pos3(o) {
  if (Array.isArray(o)) return [num(o[0]), num(o[1]), num(o[2])];
  if (o && typeof o === 'object' && ('x' in o || 'y' in o || 'z' in o)) {
    return [num(o.x), num(o.y), num(o.z)];
  }
  return null;
}
function vecEq(a, b, eps) {
  return Math.abs(a[0] - b[0]) <= eps && Math.abs(a[1] - b[1]) <= eps && Math.abs(a[2] - b[2]) <= eps;
}
const findObj = (ir, id) => (ir.objects || []).find((o) => o && o.id === id) || null;
const findLight = (ir, id) => (ir.lights || []).find((l) => l && l.id === id) || null;

// Convert an actual fixture Go light into the oracle's light shape. No
// production shadow math is used anywhere: masks come only from buildMasks.
function oracleLight(L) {
  const p = pos3(L.position) || pos3(L) || [0, 0, 0];
  const d = pos3(L.direction) || [num(L.directionX), num(L.directionY), num(L.directionZ)];
  return { position: p, direction: d, angle: num(L.angle), castShadow: L.castShadow === true };
}

function loadFixture(repo) {
  const r = spawnSync('go', ['run', './client/js/testdata/spot-shadow-typed-fixture'],
    { cwd: repo, timeout: 60000, maxBuffer: 4 * 1024 * 1024, encoding: 'utf8' });
  if (r.status !== 0) throw new Error('fixture build failed: ' + (r.stderr || r.stdout || 'exit ' + r.status));
  const fx = JSON.parse(r.stdout);
  if (!fx || fx.schema !== 'gosx.spot-shadow.fixture.v1' || !fx.scenes || !Array.isArray(fx.transitions)) {
    throw new Error('unexpected fixture schema');
  }
  return fx;
}

// Verify the fixture geometry agrees with the oracle before any pixels.
function verifyFixture(fx) {
  const errs = [];
  for (const sn of SCENES) if (!fx.scenes[sn]) errs.push('missing scene ' + sn);
  const recv = findObj(fx.scenes.on, 'typed-spot-receiver');
  const cast = findObj(fx.scenes.on, 'typed-spot-caster');
  if (!recv || !cast) errs.push('fixture missing receiver/caster objects');
  else {
    if (!vecEq(pos3(recv) || [0, 0, 0], [0, 0, -0.5], 1e-6) ||
        num(recv.width) !== 3 || num(recv.height) !== 2.2 || num(recv.depth) !== 0.1) {
      errs.push('receiver geometry does not align with oracle');
    }
    if (!vecEq(pos3(cast) || [0, 0, 0], ORACLE.casterCenter, 1e-6) ||
        num(cast.width) !== 0.55 || num(cast.height) !== 0.55 || num(cast.depth) !== 0.15) {
      errs.push('caster geometry does not align with oracle');
    }
  }
  const want = { on: ORACLE.base, moved: ORACLE.moved, aimed: ORACLE.aimed };
  for (const [sn, o] of Object.entries(want)) {
    const L = findLight(fx.scenes[sn], LIGHT_ID);
    if (!L) { errs.push('missing primary light in ' + sn); continue; }
    const ol = oracleLight(L);
    if (num(L.intensity) !== 6 || num(L.range) !== 6.5 || Math.abs(num(L.angle) - 0.75) > 1e-9) {
      errs.push(sn + ': primary intensity/range/angle mismatch');
    }
    if (!vecEq(ol.position, o.position, 1e-6) || !vecEq(ol.direction, o.direction, 1e-6) ||
        ol.castShadow !== true) {
      errs.push(sn + ': primary position/direction/castShadow mismatch');
    }
  }
  if (fx.transitions.length !== CHAIN.length - 1) errs.push('expected 4 transitions');
  fx.transitions.forEach((t, i) => {
    if (!t || t.from !== CHAIN[i] || t.to !== CHAIN[i + 1] || !Array.isArray(t.commands)) {
      errs.push('transition ' + i + ' does not match off->on->moved->aimed->off');
    }
  });
  if (errs.length) throw new Error('fixture verification failed: ' + errs.join('; '));
}

function manifestFor(sceneIR, webgpu, mountId, engineId) {
  const p = {
    width: W, height: H, maxDevicePixelRatio: 1, autoRotate: false,
    animation: false, responsive: false, background: '#000000',
    forceWebGL: !webgpu, requireWebGL: !webgpu, preferWebGPU: Boolean(webgpu),
    camera: { x: 0, y: 0, z: 4, fov: 50 },
    // Explicit black environment colors and zero intensities: an empty
    // descriptor would otherwise receive a default fill.
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

// Strict readiness: the renderer attribute must match exactly. GL pages
// require exactly "webgl" or "webgl2"; WG pages require exactly "webgpu".
// Any fallback attribute is rejected. No fallback renderer is accepted.
function readyExpr(mount, be) {
  const okRenderer = be === 'wg'
    ? '(r==="webgpu")'
    : '(r==="webgl"||r==="webgl2")';
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
  // Every decoded pixel must be fully opaque: PNG screenshots are opaque, so
  // this makes the exact/RGB comparisons below establish full RGBA equality.
  for (let o = 3; o < rgba.length; o += 4) {
    if (rgba[o] !== 255) throw new Error('readback for ' + mount + ' has non-opaque alpha ' + rgba[o] + ' at pixel ' + ((o - 3) / 4));
  }
  let nonEmpty = 0;
  for (let i = 0; i < rgba.length; i += 997) if (rgba[i] || rgba[i + 1] || rgba[i + 2]) nonEmpty++;
  if (!nonEmpty) throw new Error('empty readback for ' + mount); // never substituted with zeros
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
    // Positive means the lit capture is darker than the off capture.
    const d = lumaAt(off, i) - lumaAt(lit, i);
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

// Pixel assertions for the shadow cases. Failure here is recorded, never
// fatal mid-run, and never gated on engine attributes: the pass signal is
// actual interior darkening plus reference-pixel equality, so a silent old
// runtime cannot satisfy the checks.
function checkStatic(be, sn, shots, masks, diag) {
  const errs = [];
  const key = (s) => shots[be + '|' + s];
  const rec = (name, s) => { diag[be + '|' + name] = s; return s; };
  if (sn === 'on' || sn === 'moved' || sn === 'aimed') {
    const offKey = sn === 'on' ? 'off' : sn + '-off';
    const m = masks[sn];
    if (m.interior.length < 20) errs.push(be + '/' + sn + ': interior mask too small: ' + m.interior.length);
    if (m.exterior.length < 50) errs.push(be + '/' + sn + ': exterior mask too small: ' + m.exterior.length);
    const dark = rec(sn + '-interior-darken', lumaStats(key(sn), key(offKey), m.interior));
    if (!(dark.min > 12)) errs.push(be + '/' + sn + ': interior min luma darkening ' + dark.min + ' <= 12 vs ' + offKey);
    const amb = rec(sn + '-interior-vs-ambient', deltaStats(key(sn), key('ambient-only'), m.interior));
    if (!(amb.max <= 2)) errs.push(be + '/' + sn + ': interior maxRGB delta vs ambient-only ' + amb.max + ' > 2');
    const ext = rec(sn + '-exterior-vs-off', deltaStats(key(sn), key(offKey), m.exterior));
    if (!(ext.max <= 2)) errs.push(be + '/' + sn + ': exterior maxRGB delta vs ' + offKey + ' ' + ext.max + ' > 2');
    const lit = rec(sn + '-exterior-lit', { count: m.exterior.length, litFraction: litFraction(key(sn), m.exterior) });
    if (!(lit.litFraction > 0.5)) errs.push(be + '/' + sn + ': exterior lit coverage ' + lit.litFraction + ' too low');
    if (sn !== 'on') {
      const changed = rec(sn + '-receiver-changed', { count: changedPixels(key(sn), key('on'), masks.on.receiver) });
      if (!(changed.count > 20)) errs.push(be + '/' + sn + ': receiver changed pixels vs on = ' + changed.count);
    }
  } else if (sn === 'no-caster' || sn === 'no-receiver' || sn === 'discarded') {
    const d = rec(sn + '-vs-off', deltaStats(key(sn), key('off'), masks.on.receiver));
    if (!(d.max <= 2)) errs.push(be + '/' + sn + ': expected match with off on receiver ROI, max=' + d.max);
  } else if (sn === 'equal' || sn === 'invalid-prefix') {
    const d = rec(sn + '-vs-on', deltaStats(key(sn), key('on'), masks.on.receiver));
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

function lightStateExpr(mount, id) {
  return '(function(){var m=document.getElementById(' + JSON.stringify(mount) + ');' +
    'var st=m&&m.__gosxScene3DState;' +
    'if(!st||!(st.lights&&st.lights.get))return null;' +
    'var L=st.lights.get(' + JSON.stringify(id) + ');' +
    'if(!L)return null;return {castShadow:L.castShadow===true,' +
    'x:L.x,y:L.y,z:L.z,directionX:L.directionX,directionY:L.directionY,directionZ:L.directionZ,' +
    'angle:L.angle,range:L.range,intensity:L.intensity,' +
    'hash:(typeof L._lightHash==="number")?L._lightHash:null};})()';
}

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
  for (const f of ['x', 'y', 'z', 'directionX', 'directionY', 'directionZ', 'angle', 'range', 'intensity']) {
    const g = num(got[f]); const w = num(want[f]);
    if (Math.abs(g - w) > 1e-6) errs.push(be + '/' + stage + ': light ' + f + '=' + g + ' want ' + w);
  }
  if (got.castShadow !== (want.castShadow === true)) {
    errs.push(be + '/' + stage + ': light castShadow=' + got.castShadow);
  }
}

async function main() {
  const repo = process.argv[2];
  const art = process.argv[3];
  if (!repo || !fs.existsSync(repo) || !art || !fs.existsSync(art)) {
    throw new Error('usage: node scene3d-spot-shadow-browser.cjs <repoRoot> <existingArtifactDir>');
  }
  // Preflight: failures here happen before driver creation and may exit with
  // a preflight message (nothing to report yet).
  const fx = loadFixture(repo);
  verifyFixture(fx);
  const masks = {
    on: buildMasks(oracleLight(findLight(fx.scenes.on, LIGHT_ID))),
    moved: buildMasks(oracleLight(findLight(fx.scenes.moved, LIGHT_ID))),
    aimed: buildMasks(oracleLight(findLight(fx.scenes.aimed, LIGHT_ID))),
  };
  const pages = {};
  const staticMeta = [];
  for (const be of ['gl', 'wg']) {
    for (const sn of SCENES) {
      const mount = 'spot-' + be + '-' + sn;
      pages['/' + mount] = htmlFor(mount, manifestFor(fx.scenes[sn], be === 'wg', mount, 'eng-' + mount));
      staticMeta.push({ be, sn, mount });
    }
  }
  const liveMeta = [];
  for (const be of ['gl', 'wg']) {
    const mount = 'spot-live-' + be;
    pages['/' + mount] = htmlFor(mount, manifestFor(fx.scenes.off, be === 'wg', mount, 'eng-' + mount));
    liveMeta.push({ be, mount });
  }
  const runtimeRoot = process.env.GOSX_PROBE_RUNTIME_ROOT || path.join(repo, 'client', 'js');
  const driver = await startDriver({ repoRoot: repo, runtimeRoot, pages, preload: PRELOAD });
  module.exports.__activeDriver = driver; // watchdog cleanup handle

  // From here on, the run must ALWAYS produce a report: fatal errors are
  // caught, counts reflect what actually executed, and the report is written
  // in an always-run finally after driver.close().
  const assertionErrors = [];
  const diag = {};
  const capabilities = {};
  const shots = {};
  const liveEvidence = [];
  let executedStatic = 0;
  let executedLiveCases = 0;
  let executedLiveStages = 0;
  let fatal = null;
  try {
    await driver.send('Emulation.setDeviceMetricsOverride', { width: W, height: H, deviceScaleFactor: 1, mobile: false });
    // ---- 24 static captures + checks (all run even when assertions fail) ----
    for (const meta of staticMeta) {
      const label = meta.be + '/' + meta.sn;
      await driver.load('/' + meta.mount);
      const ready = await waitReady(driver, meta.mount, meta.be);
      await settleFrames(driver, 8);
      const counters = await driver.eval(COUNTERS_EXPR);
      const caps = await driver.eval('window.__probeCapsPromise', true);
      // Both backends must have real WebGL2 AND WebGPU available in the
      // browser; this is a hard requirement, recorded per case.
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
      shots[meta.be + '|' + meta.sn] = shot.rgba;
      fs.writeFileSync(path.join(art, meta.mount + '.png'), shot.png);
      executedStatic += 1; // actual completed capture, not planned
      let disposalOk = false;
      if (await driver.eval(disposeExpr('eng-' + meta.mount, meta.mount)) !== true) {
        assertionErrors.push(label + ': engine disposal did not clear state');
      } else {
        disposalOk = true;
      }
      capabilities[label].disposalClearedState = disposalOk;
    }
    // Run all static pixel checks only after every capture/disposal so the
    // negative controls collect both backends even if assertions fail.
    for (const meta of staticMeta) {
      for (const e of checkStatic(meta.be, meta.sn, shots, masks, diag)) assertionErrors.push(e);
    }
    // ---- 2 live pages, 4 transitions each via public mount commands ----
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
      const baseDelta = deltaStats(base.rgba, shots[be + '|off'], ALL);
      diag['live-' + be + '-baseline-vs-static-off'] = baseDelta;
      if (baseDelta.max !== 0) assertionErrors.push(be + ' live: off baseline differs from static off, max=' + baseDelta.max);
      let revision = 0;
      let prevHash = null;
      let stage = 0;
      for (const tr of fx.transitions) {
        stage += 1;
        revision += 1;
        let ack = false;
        if (await driver.eval(dispatchExpr(live.mount, tr.commands, revision, tr.commands.length), true) !== true) {
          throw new Error(be + ' live stage ' + tr.name + ': commands not applied (revision ' + revision + ')');
        }
        ack = true;
        await settleFrames(driver, 8);
        const L = await driver.eval(lightStateExpr(live.mount, LIGHT_ID));
        if (!L) throw new Error(be + ' live stage ' + tr.name + ': primary spot missing from state.lights');
        checkLightState(be, tr.name, L, findLight(fx.scenes[tr.to], LIGHT_ID), assertionErrors);
        const geometryMutation = tr.name === 'on-to-moved' || tr.name === 'moved-to-aimed';
        if (typeof L.hash !== 'number' || !Number.isFinite(L.hash)) {
          assertionErrors.push(be + '/' + tr.name + ': light hash is not finite: ' + L.hash);
        } else if (prevHash !== null && geometryMutation && L.hash === prevHash) {
          assertionErrors.push(be + '/' + tr.name + ': light hash unchanged on position/direction mutation');
        }
        prevHash = L.hash;
        const countersBefore = counters;
        const next = await driver.eval(COUNTERS_EXPR);
        // GL: draw count must advance. WG: BOTH pass and submit must advance.
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
        const cmp = deltaStats(shot.rgba, shots[be + '|' + tr.to], ALL);
        diag['live-' + be + '-' + tr.name] = cmp;
        if (tr.to === 'off') {
          if (cmp.max !== 0) assertionErrors.push(be + '/' + tr.name + ': off restoration not exact, max=' + cmp.max);
        } else if (cmp.max > 2) {
          assertionErrors.push(be + '/' + tr.name + ': capture differs from static ' + tr.to + ', max=' + cmp.max);
        }
        executedLiveStages += 1; // actual completed stage
        // Evidence for this stage: only public/observable results, no engine
        // internal state.
        liveEvidence.push({
          backend: be, stage, transition: tr.name, to: tr.to,
          ackRevision: revision, commandCount: tr.commands.length, commandsApplied: ack,
          readbackLightHash: (typeof L.hash === 'number') ? L.hash : null,
          countersBefore, countersAfter: next,
          drawsAdvanced, passesAdvanced, submitsAdvanced,
          identity: id, identityStable: identityOk,
          rgbDelta: cmp,
        });
      }
      const disposalOk = await driver.eval(disposeExpr('eng-' + live.mount, live.mount)) === true;
      if (!disposalOk) assertionErrors.push(be + ' live: final disposal did not clear state');
      executedLiveCases += 1; // actual completed live case
      liveEvidence.push({ backend: be, stage: 'final', transition: 'dispose', disposalClearedState: disposalOk });
    }
  } catch (e) {
    fatal = (e && e.message) ? e.message : String(e);
  } finally {
    try { await driver.close(); } catch (e) { if (!fatal) fatal = 'driver close failed: ' + ((e && e.message) || String(e)); }
    module.exports.__activeDriver = null;
    // Merge driver errors into the error set; any driver error fails the run.
    // Warnings and 404s remain hard failures as well.
    const driverErrors = Array.isArray(driver.errors) ? driver.errors.slice() : [];
    const errors = assertionErrors.concat(driverErrors);
    const report = {
      executedStatic, executedLiveCases, executedLiveStages,
      plannedStatic: staticMeta.length, plannedLiveCases: liveMeta.length,
      plannedLiveStages: liveMeta.length * fx.transitions.length,
      failedAssertions: assertionErrors.length,
      driverErrors, capabilities, diagnostics: diag, liveEvidence, errors,
      fatal, warnings: driver.warnings, notFound: driver.notFound,
    };
    fs.writeFileSync(path.join(art, 'report.json'), JSON.stringify(report, null, 2));
    if (errors.length || driver.warnings.length || driver.notFound.length || fatal ||
        executedStatic !== 24 || executedLiveCases !== 2 || executedLiveStages !== 8) {
      console.error('spot-shadow browser test FAILED:', JSON.stringify({
        fatal, errors: errors.length, warnings: driver.warnings.length,
        notFound: driver.notFound.length,
        executedStatic, executedLiveCases, executedLiveStages,
      }));
      process.exitCode = 1;
    } else {
      console.log('spot-shadow browser test: 24 static + 2 live (8 stages) cases complete');
    }
  }
}

if (require.main === module) {
  const watchdog = setTimeout(async () => {
    console.error('spot-shadow browser test: 240s watchdog expired');
    try { if (module.exports.__activeDriver) await module.exports.__activeDriver.close(); } catch (e) { /* best effort */ }
    process.exit(1);
  }, WATCHDOG_MS);
  main().then(() => { clearTimeout(watchdog); if (process.exitCode) process.exit(process.exitCode); })
    .catch((e) => { clearTimeout(watchdog); console.error('preflight fatal:', e && e.message); process.exit(1); });
}

module.exports = { __activeDriver: null };
