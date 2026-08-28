'use strict';

// Native browser regression for offscreen skinned/static shadow fixtures.
// First milestone: STATIC captures only (no live stages). 7 scenes x 2
// backends = 14 exact captures. No forged state, no injected geometry, no
// fakeGPU, no skip paths: every assertion accumulates and the run is nonzero
// on any error, warning, notFound, fatal, or short capture count.

const fs = require('fs');
const path = require('path');
const { execFileSync } = require('child_process');
const {
  htmlFor, waitReady, settleFrames, captureRGBA, deltaStats, changedPixels,
  disposeExpr, PRELOAD, COUNTERS_EXPR, ALL,
} = require('./scene3d-point-shadow-browser.cjs');
const { startDriver } = require('./spot-shadow-browser-driver.cjs');
const { buildOffscreenShadowFixture } = require('./offscreen-shadow-fixture.cjs');

const W = 320;
const H = 180;
const WATCHDOG_MS = 360 * 1000;
const HYDRATION_TIMEOUT_MS = 20 * 1000;
const SCENES = ['empty', 'reference-rest', 'reference-pose', 'skin-rest', 'skin-pose', 'skin-no-cast', 'skin-light-off'];
const SCHEMA = 'gosx.offscreen-shadow.fixture.v1';
const PLANNED_STATIC = SCENES.length * 2;

const num = (v) => (Number.isFinite(Number(v)) ? Number(v) : 0);

function buildTypedFixture(repo) {
  const out = execFileSync('go', ['run', './client/js/testdata/offscreen-shadow-typed-fixture'],
    { cwd: repo, timeout: 60000, maxBuffer: 8 * 1024 * 1024, encoding: 'utf8' });
  return JSON.parse(out);
}

// Mirrors the point-shadow manifest builder, but keeps IR.models verbatim and
// animation:true (required so the mixer ticks and joint matrices update).
function manifestFor(IR, webgpu, mountId, engineId, camera) {
  const cam = camera || {};
  const props = {
    width: W, height: H, maxDevicePixelRatio: 1, autoRotate: false,
    animation: true, responsive: false, background: '#000000',
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
    lights: IR.lights, objects: IR.objects, models: IR.models,
  };
  return JSON.stringify({ engines: [{ id: engineId, component: 'GoSXScene3D', kind: 'surface', mountId, props }] });
}

// READONLY snapshot: native attrs + model/object reads. Never writes
// engine state. Missing state is reported, never bypassed.
function snapshotExpr(mountId) {
  return '(function(){' +
    'var el=document.getElementById(' + JSON.stringify(mountId) + ');' +
    'if(!el)return{found:false};' +
    'var out={found:true,attrs:{}};' +
    'out.attrs.culled=el.getAttribute("data-gosx-scene3d-webgpu-mesh-view-culled");' +
    'out.attrs.skinDispatches=el.getAttribute("data-gosx-scene3d-webgpu-elio-skinning-dispatches");' +
    'var st=el.__gosxScene3DState;' +
    'if(!st){out.found=false;return out;}' +
    'var skins=st._modelSkins||[];var rec=null;' +
    'for(var j=0;j<skins.length;j++){if(skins[j]&&skins[j].id==="skin-caster"){rec=skins[j];break;}}' +
    'if(rec){var s0=rec.skins&&rec.skins[0];' +
    'out.skin={id:rec.id,animation:rec.animation,' +
    'rootTransform:rec.rootTransform?Array.prototype.slice.call(rec.rootTransform):null,' +
    'jointMatrices:(s0&&s0.jointMatrices)?Array.prototype.slice.call(s0.jointMatrices):null,' +
    'objectIDs:(rec.objectIDs||[]).slice()};}' +
    'if(st.objects&&out.skin&&out.skin.objectIDs.length){' +
    'var o=st.objects.get(out.skin.objectIDs[0]);' +
    'if(o){out.object={castShadow:!!o.castShadow,' +
    'joints:(o.vertices&&o.vertices.joints)?o.vertices.joints.length:0,' +
    'weights:(o.vertices&&o.vertices.weights)?o.vertices.weights.length:0};}}' +
    'return out;})()';
}

function matErr(m, want) { // max |m[i]-want[i]| over 16 elements; Infinity if unusable
  if (!Array.isArray(m) || m.length !== 16) return Infinity;
  let e = 0;
  for (let i = 0; i < 16; i++) {
    const v = m[i];
    if (typeof v !== 'number' || !Number.isFinite(v)) return Infinity;
    e = Math.max(e, Math.abs(v - want[i]));
  }
  return e;
}

const IDENT = [1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1];
function posedWant() {
  const w = IDENT.slice();
  w[13] = Math.fround(0.3); // translation only along Y
  return w;
}

function assertSkinSnapshot(label, snap, posed, errs) {
  if (!snap || !snap.found) { errs.push(label + ': engine state not reachable'); return; }
  if (!snap.skin) { errs.push(label + ': no _modelSkins record id="skin-caster"'); return; }
  const sk = snap.skin;
  if (!sk.jointMatrices || sk.jointMatrices.length !== 16) errs.push(label + ': jointMatrices length != 16');
  if (sk.animation !== (posed ? 'pose' : '')) errs.push(label + ': animation clip ' + JSON.stringify(sk.animation) + ' want ' + JSON.stringify(posed ? 'pose' : ''));
  if (!sk.rootTransform || matErr(sk.rootTransform, IDENT) > 1e-7) errs.push(label + ': rootTransform not identity (1e-7)');
  const want = posed ? posedWant() : IDENT;
  const eps = posed ? 1e-5 : 1e-5;
  if (sk.jointMatrices && sk.jointMatrices.length === 16) {
    const e = matErr(sk.jointMatrices, want);
    if (e > eps) errs.push(label + ': joint matrix mismatch (max ' + e.toExponential(3) + ', eps ' + eps + ')');
  }
  if (!snap.object) { errs.push(label + ': object record missing for objectIDs[0]'); return; }
  if (!(snap.object.joints > 0)) errs.push(label + ': empty joints buffer');
  if (!(snap.object.weights > 0)) errs.push(label + ': empty weights buffer');
  const wantCast = !/no-cast/.test(label);
  if (snap.object.castShadow !== wantCast) errs.push(label + ': castShadow=' + snap.object.castShadow + ' want ' + wantCast);
}

function maxRGB(a, b) {
  return deltaStats(a, b, ALL).max;
}

async function pollHydration(driver, mount) {
  const expr = '(function(){var el=document.getElementById(' + JSON.stringify(mount) + ');' +
    'return el?el.getAttribute("data-gosx-scene3d-model-hydration-status"):null;})()';
  const deadline = Date.now() + HYDRATION_TIMEOUT_MS;
  for (;;) {
    const v = await driver.eval(expr);
    if (v === 'committed') return true;
    if (Date.now() > deadline) return false;
    await new Promise((r) => setTimeout(r, 200));
  }
}

async function main() {
  const repo = process.argv[2];
  const art = process.argv[3];
  if (!repo || !art) throw new Error('usage: node scene3d-offscreen-shadow-browser.cjs <repoRoot> <artifactDir>');
  fs.mkdirSync(art, { recursive: true });
  const fx = buildTypedFixture(repo);
  if (!fx || fx.schema !== SCHEMA) throw new Error('unexpected fixture schema: ' + (fx && fx.schema));
  if (!fx.scenes || Object.keys(fx.scenes).length !== 9) throw new Error('fixture must contain 9 scenes');
  const modelB64 = buildOffscreenShadowFixture();
  const assets = {
    '/models/skin-rest.glb': { body: Buffer.from(modelB64.skinRest, 'base64'), contentType: 'model/gltf-binary' },
    '/models/skin-pose.glb': { body: Buffer.from(modelB64.skinPose, 'base64'), contentType: 'model/gltf-binary' },
    '/models/static-rest.glb': { body: Buffer.from(modelB64.staticRest, 'base64'), contentType: 'model/gltf-binary' },
    '/models/static-pose.glb': { body: Buffer.from(modelB64.staticPose, 'base64'), contentType: 'model/gltf-binary' },
  };
  const pages = {};
  const cases = [];
  for (const be of ['gl', 'wg']) {
    for (const sn of SCENES) {
      const mount = 'off-' + be + '-' + sn;
      const IR = fx.scenes[sn];
      if (!IR) throw new Error('missing scene IR: ' + sn);
      pages['/' + mount] = htmlFor(mount, manifestFor(IR, be === 'wg', mount, 'eng-' + mount, fx.camera));
      cases.push({ be, sn, mount });
    }
  }
  const runtimeRoot = process.env.GOSX_PROBE_RUNTIME_ROOT || path.join(repo, 'client', 'js');
  let driver = null;
  let fatal = null;
  try {
    driver = await startDriver({ repoRoot: repo, runtimeRoot, pages, assets, preload: PRELOAD });
    module.exports.__activeDriver = driver;
  } catch (e) {
    fatal = 'driver startup failed: ' + ((e && e.message) || String(e));
  }

  const assertionErrors = [];
  const capabilities = {};
  const diagnostics = { pixels: {}, modelRecords: {}, attributes: {} };
  const shots = {};
  let executedStatic = 0;
  try {
    if (driver) {
      await driver.send('Emulation.setDeviceMetricsOverride', { width: W, height: H, deviceScaleFactor: 1, mobile: false });
      for (const c of cases) {
        const label = c.be + '/' + c.sn;
        await driver.load('/' + c.mount);
        await waitReady(driver, c.mount, c.be); // exact requested backend, no fallback
        const caps = await driver.eval('window.__probeCapsPromise', true);
        if (!(caps && caps.webgl2 === true && caps.webgpu === true)) {
          assertionErrors.push(label + ': capabilities missing ' + JSON.stringify(caps));
        }
        if (c.sn !== 'empty') {
          if (!(await pollHydration(driver, c.mount))) assertionErrors.push(label + ': model hydration never reached committed');
        }
        await settleFrames(driver, 8);
        const counters = await driver.eval(COUNTERS_EXPR);
        if (c.be === 'gl') {
          if (!(counters && counters.draws > 0)) assertionErrors.push(label + ': no native GL draws');
          else if (counters.context !== 'webgl2') assertionErrors.push(label + ': GL context not webgl2 (' + counters.context + ')');
        } else if (!(counters && counters.wgPasses > 0 && counters.wgSubmits > 0)) {
          assertionErrors.push(label + ': no native WG render pass/submit');
        }
        capabilities[label] = { counters, caps };
        const snap = await driver.eval(snapshotExpr(c.mount));
        diagnostics.attributes[label] = snap && snap.attrs;
        if (/^skin-/.test(c.sn)) {
          assertSkinSnapshot(label, snap, c.sn !== 'skin-rest', assertionErrors);
          diagnostics.modelRecords[label] = {
            attrs: snap && snap.attrs,
            animation: snap && snap.skin ? snap.skin.animation : null,
            rootTransform: snap && snap.skin ? snap.skin.rootTransform : null,
            jointMatrices: snap && snap.skin ? snap.skin.jointMatrices : null,
            object: snap && snap.object,
          };
        }
        if (c.be === 'wg' && c.sn !== 'empty') {
          if (!snap || !snap.attrs) { assertionErrors.push(label + ': snapshot attrs missing'); }
          else {
          const culled = parseInt(snap.attrs.culled, 10);
          // All skin-* scenes are emitted with viewCulled:false even when physically
          // offscreen/behind the camera, so they must not be culled and must dispatch.
          // This validates physically offscreen skin shadows, but not the
          // culled-deformation branch.
          const isSkin = (c.sn === 'skin-rest' || c.sn === 'skin-pose' || c.sn === 'skin-no-cast' || c.sn === 'skin-light-off');
          const wantCulled = isSkin ? culled === 0 : culled >= 1;
          if (!wantCulled) assertionErrors.push(label + ': webgpu mesh-view-culled attr ' + snap.attrs.culled + ' unexpected');
          const disp = parseInt(snap.attrs.skinDispatches, 10);
          const wantDisp = isSkin ? disp > 0 : disp === 0;
          if (!wantDisp) assertionErrors.push(label + ': elio-skinning-dispatches ' + snap.attrs.skinDispatches + ' unexpected');
          }
        }
        const shot = await captureRGBA(driver, c.mount);
        shots[c.be + '|' + c.sn] = shot.rgba;
        fs.writeFileSync(path.join(art, c.mount + '.png'), shot.png);
        executedStatic += 1;
        const disposed = await driver.eval(disposeExpr('eng-' + c.mount, c.mount)) === true;
        capabilities[label].disposalClearedState = disposed;
        if (!disposed) assertionErrors.push(label + ': engine disposal did not clear state');
      }
      // Pixel comparisons per backend; accumulate everything, never stop early.
      for (const be of ['gl', 'wg']) {
        const k = (sn) => be + '|' + sn;
        const have = (sn) => Boolean(shots[k(sn)]);
        const cmp = (name, a, b, kind) => {
          if (!have(a) || !have(b)) { assertionErrors.push(be + ': pixel check ' + name + ' missing capture'); return; }
          const v = kind === 'changed' ? changedPixels(shots[k(a)], shots[k(b)], ALL) : maxRGB(shots[k(a)], shots[k(b)]);
          diagnostics.pixels[be + ':' + name] = v;
          const ok = kind === 'changed' ? v > 50 : v <= 2;
          if (!ok) assertionErrors.push(be + ': pixel check ' + name + ' = ' + v + (kind === 'changed' ? ' (want >50 changed)' : ' (want maxRGB <=2)'));
        };
        cmp('refrest-vs-empty', 'reference-rest', 'empty', 'changed');
        cmp('refpose-vs-refrest', 'reference-pose', 'reference-rest', 'changed');
        cmp('skinrest-vs-refrest', 'skin-rest', 'reference-rest', 'maxrgb');
        cmp('skinpose-vs-refpose', 'skin-pose', 'reference-pose', 'maxrgb');
        cmp('nocast-vs-empty', 'skin-no-cast', 'empty', 'maxrgb');
        cmp('lightoff-vs-empty', 'skin-light-off', 'empty', 'maxrgb');
      }
    }
  } catch (e) {
    fatal = (e && e.message) ? e.message : String(e);
  } finally {
    if (driver) {
      try { await driver.close(); } catch (e) { if (!fatal) fatal = 'driver close failed: ' + ((e && e.message) || String(e)); }
      module.exports.__activeDriver = null;
    }
    const driverErrors = driver && Array.isArray(driver.errors) ? driver.errors.slice() : [];
    const warnings = driver && Array.isArray(driver.warnings) ? driver.warnings.slice() : [];
    const notFound = driver && Array.isArray(driver.notFound) ? driver.notFound.slice() : [];
    const errors = assertionErrors.concat(driverErrors);
    const report = {
      runtimeRoot, executedStatic, plannedStatic: PLANNED_STATIC,
      failedAssertions: assertionErrors.length, errors, driverErrors, warnings, notFound, fatal,
      capabilities, diagnostics,
    };
    fs.writeFileSync(path.join(art, 'report.json'), JSON.stringify(report, null, 2));
    const countsOk = executedStatic === PLANNED_STATIC;
    if (errors.length || warnings.length || notFound.length || fatal || !countsOk) {
      console.error('offscreen-shadow browser test FAILED:', JSON.stringify({
        fatal, errors: errors.length, warnings: warnings.length, notFound: notFound.length, executedStatic,
      }));
      process.exitCode = 1;
    } else {
      console.log('offscreen-shadow browser test: ' + executedStatic + ' static captures complete');
    }
  }
}

module.exports = { main, __activeDriver: null };

if (require.main === module) {
  const watchdog = setTimeout(async () => {
    console.error('offscreen-shadow browser test: 360s watchdog expired');
    try { if (module.exports.__activeDriver) await module.exports.__activeDriver.close(); } catch (e) { /* best effort */ }
    process.exit(1);
  }, WATCHDOG_MS);
  main().then(() => { clearTimeout(watchdog); if (process.exitCode) process.exit(process.exitCode); })
    .catch((e) => { clearTimeout(watchdog); console.error('preflight fatal:', e && e.message); process.exit(1); });
}
