'use strict';

// Native browser regression for offscreen skinned/static shadow fixtures.
// 7 scenes x 2 backends = 14 exact static captures, plus two live
// computed-morph cases (one per backend: a baseline capture and a guard/idle
// pose dispatch each, for 2 baselines and 8 stages). No forged state, no
// injected geometry, no fakeGPU, no skip paths: every assertion accumulates
// and the run is nonzero on any error, warning, notFound, fatal, or short
// capture count.

const fs = require('fs');
const path = require('path');
const { execFileSync } = require('child_process');
const {
  htmlFor, waitReady, settleFrames, captureRGBA, deltaStats, changedPixels,
  disposeExpr, PRELOAD, COUNTERS_EXPR, ALL,
} = require('./scene3d-point-shadow-browser.cjs');
const { startDriver } = require('./spot-shadow-browser-driver.cjs');
const { buildOffscreenShadowFixture } = require('./offscreen-shadow-fixture.cjs');
const {
  MORPH_PRELOAD, readMorphState, rememberMorphState, dispatchMorphPose,
} = require('./offscreen-morph-probe.cjs');

const W = 320;
const H = 180;
const WATCHDOG_MS = 360 * 1000;
const HYDRATION_TIMEOUT_MS = 20 * 1000;
const SCENES = ['empty', 'reference-rest', 'reference-pose', 'skin-rest', 'skin-pose', 'skin-no-cast', 'skin-light-off'];
const SCHEMA = 'gosx.offscreen-shadow.fixture.v1';
const PLANNED_STATIC = SCENES.length * 2;
const PLANNED_LIVE = 2;          // one morph-live page per backend
const PLANNED_LIVE_STAGES = 8;   // guard + idle + partial-1 + partial-2 per backend
const MORPH_SETTLE_TIMEOUT_MS = 5 * 1000;

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

// Assert the invariants shared by every live morph readback (baseline, guard,
// idle): stable remembered references, identity root matrix, a casting static
// main object, and a hidden, non-casting static target.
function assertLiveMorphState(label, st, errs) {
  if (!st || !st.found) { errs.push(label + ': morph state not reachable'); return; }
  if (!(st.sameMount && st.sameCanvas && st.sameState && st.sameRecord)) {
    errs.push(label + ': remembered references not stable (mount=' + st.sameMount +
      ' canvas=' + st.sameCanvas + ' state=' + st.sameState + ' record=' + st.sameRecord + ')');
  }
  if (!st.rootTransform || matErr(st.rootTransform, IDENT) > 1e-7) {
    errs.push(label + ': rootTransform not identity (1e-7)');
  }
  if (st.staticModel !== true) errs.push(label + ': main record not static');
  if (st.objectCastShadow !== true) errs.push(label + ': main object not casting');
  if (st.targetStatic !== true) errs.push(label + ': target record not static');
  if (st.targetVisible !== false) errs.push(label + ': target not hidden');
  if (st.targetCastShadow !== false) errs.push(label + ': target casting');
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
  const liveCases = [];
  for (const be of ['gl', 'wg']) {
    for (const sn of SCENES) {
      const mount = 'off-' + be + '-' + sn;
      const IR = fx.scenes[sn];
      if (!IR) throw new Error('missing scene IR: ' + sn);
      pages['/' + mount] = htmlFor(mount, manifestFor(IR, be === 'wg', mount, 'eng-' + mount, fx.camera));
      cases.push({ be, sn, mount });
    }
  }
  // Live computed-morph pages: one per backend from the shared morph-live IR.
  for (const be of ['gl', 'wg']) {
    const mount = 'off-' + be + '-morph-live';
    const IR = fx.scenes['morph-live'];
    if (!IR) throw new Error('missing scene IR: morph-live');
    pages['/' + mount] = htmlFor(mount, manifestFor(IR, be === 'wg', mount, 'eng-' + mount, fx.camera));
    liveCases.push({ be, mount });
  }
  const runtimeRoot = process.env.GOSX_PROBE_RUNTIME_ROOT || path.join(repo, 'client', 'js');
  let driver = null;
  let fatal = null;
  try {
    driver = await startDriver({ repoRoot: repo, runtimeRoot, pages, assets, preload: PRELOAD + '\n' + MORPH_PRELOAD });
    module.exports.__activeDriver = driver;
  } catch (e) {
    fatal = 'driver startup failed: ' + ((e && e.message) || String(e));
  }

  const assertionErrors = [];
  const capabilities = {};
  const diagnostics = { pixels: {}, modelRecords: {}, attributes: {} };
  const liveEvidence = {};
  const shots = {};
  const liveShots = {};
  let executedStatic = 0;
  let executedLive = 0;
  let executedLiveBaselines = 0;
  let executedLiveStages = 0;
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

      // ---- Live computed-morph cases (real GPU/CPU dispatch only) ----
      const morphRead = (mount) =>
        driver.eval('(' + readMorphState.toString() + ')(' + JSON.stringify(mount) + ')', true);
      for (const lc of liveCases) {
        const label = lc.be + '/morph-live';
        const ev = {};
        await driver.load('/' + lc.mount);
        await waitReady(driver, lc.mount, lc.be); // exact requested backend, no fallback
        const caps = await driver.eval('window.__probeCapsPromise', true);
        if (!(caps && caps.webgl2 === true && caps.webgpu === true)) {
          assertionErrors.push(label + ': capabilities missing ' + JSON.stringify(caps));
        }
        if (!(await pollHydration(driver, lc.mount))) assertionErrors.push(label + ': model hydration never reached committed');
        await settleFrames(driver, 8);
        const remembered = await driver.eval('(' + rememberMorphState.toString() + ')(' + JSON.stringify(lc.mount) + ')', true);
        if (remembered !== true) assertionErrors.push(label + ': rememberMorphState failed');

        // Baseline: real references saved, static main / hidden non-casting
        // target / identity root verified, pixels match reference-rest.
        const base = await morphRead(lc.mount);
        assertLiveMorphState(label + '/baseline', base, assertionErrors);
        ev.caps = caps;
        ev.baselineReadback = base;
        const baseShot = await captureRGBA(driver, lc.mount);
        fs.writeFileSync(path.join(art, lc.mount + '-baseline.png'), baseShot.png);
        executedLiveBaselines += 1;
        if (shots[lc.be + '|reference-rest']) {
          const v = maxRGB(baseShot.rgba, shots[lc.be + '|reference-rest']);
          ev.baseline = { maxRGB: v, want: 2 };
          if (!(v <= 2)) assertionErrors.push(label + ': baseline vs reference-rest maxRGB ' + v + ' want <=2');
        } else {
          assertionErrors.push(label + ': missing reference-rest capture for baseline comparison');
        }

        const runStage = async (stage, pose, targetID, alpha, prev, refKey) => {
          await driver.eval('(' + dispatchMorphPose.toString() + ')(' + JSON.stringify(pose) + ', ' + alpha + ')', true);
          const deadline = Date.now() + MORPH_SETTLE_TIMEOUT_MS;
          let st = null;
          for (;;) {
            st = await morphRead(lc.mount);
            if (st && st.found && st.pose === pose && st.targetID === targetID &&
                st.morphObjects > 0 && st.morphVertices > 0 &&
                st.objectMorphCount > 0 && st.objectMorphAlpha === alpha) break;
            if (Date.now() > deadline) {
              assertionErrors.push(label + '/' + stage + ': morph pose did not settle within ' + MORPH_SETTLE_TIMEOUT_MS +
                'ms (pose=' + (st && st.pose) + ' targetID=' + (st && st.targetID) +
                ' morphObjects=' + (st && st.morphObjects) + ' morphVertices=' + (st && st.morphVertices) +
                ' objectMorphCount=' + (st && st.objectMorphCount) + ' alpha=' + (st && st.objectMorphAlpha) +
                ' want ' + alpha + ')');
              break;
            }
            await new Promise((r) => setTimeout(r, 200));
          }
          await settleFrames(driver, 8);
          st = await morphRead(lc.mount);
          assertLiveMorphState(label + '/' + stage, st, assertionErrors);
          if (st && st.found) {
            if (st.pose !== pose) assertionErrors.push(label + '/' + stage + ': pose ' + JSON.stringify(st.pose) + ' want ' + JSON.stringify(pose));
            if (st.targetID !== targetID) assertionErrors.push(label + '/' + stage + ': targetID ' + JSON.stringify(st.targetID) + ' want ' + JSON.stringify(targetID));
            if (!(st.morphObjects > 0)) assertionErrors.push(label + '/' + stage + ': morphObjects not positive');
            if (!(st.morphVertices > 0)) assertionErrors.push(label + '/' + stage + ': morphVertices not positive');
            if (!(st.objectMorphCount > 0)) assertionErrors.push(label + '/' + stage + ': objectMorphCount not positive');
            if (st.objectMorphAlpha !== alpha) assertionErrors.push(label + '/' + stage + ': objectMorphAlpha ' + st.objectMorphAlpha + ' want ' + alpha);
          }
          const delta = {
            glDraws: (st && st.found ? st.glDraws : 0) - (prev && prev.glDraws ? prev.glDraws : 0),
            wgPasses: (st && st.found ? st.wgPasses : 0) - (prev && prev.wgPasses ? prev.wgPasses : 0),
            wgSubmits: (st && st.found ? st.wgSubmits : 0) - (prev && prev.wgSubmits ? prev.wgSubmits : 0),
            nativeMorphDispatches: (st && st.found ? st.nativeMorphDispatches : 0) - (prev && prev.nativeMorphDispatches ? prev.nativeMorphDispatches : 0),
          };
          if (lc.be === 'gl') {
            if (!(st && st.found && st.glDraws > prev.glDraws)) {
              assertionErrors.push(label + '/' + stage + ': GL draws did not advance (' + (st && st.found ? st.glDraws : 'n/a') + ' vs ' + prev.glDraws + ')');
            }
          } else {
            if (!(st && st.found && st.wgPasses > prev.wgPasses && st.wgSubmits > prev.wgSubmits &&
                  st.nativeMorphDispatches > prev.nativeMorphDispatches)) {
              assertionErrors.push(label + '/' + stage + ': WG passes/submits/computed-morph dispatches did not advance');
            }
            const culled = st ? parseInt(st.culled, 10) : NaN;
            if (!(culled >= 1)) assertionErrors.push(label + '/' + stage + ': published culled count ' + (st ? st.culled : 'n/a') + ' want >=1');
            const disp = st ? parseInt(st.dispatches, 10) : NaN;
            if (!(disp > 0)) assertionErrors.push(label + '/' + stage + ': published computed-morph dispatches ' + (st ? st.dispatches : 'n/a') + ' want >0');
          }
          const shot = await captureRGBA(driver, lc.mount);
          fs.writeFileSync(path.join(art, lc.mount + '-' + stage + '.png'), shot.png);
          liveShots[lc.be + '|' + stage] = shot.rgba;
          executedLiveStages += 1;
          ev[stage] = { pose, targetID, delta, pixel: null };
          ev[stage].readback = st;
          if (refKey !== null) {
            if (shots[refKey]) {
              const v = maxRGB(shot.rgba, shots[refKey]);
              ev[stage].pixel = { maxRGB: v, want: 2 };
              if (!(v <= 2)) assertionErrors.push(label + '/' + stage + ': maxRGB vs ' + refKey.split('|')[1] + ' = ' + v + ' want <=2');
            } else {
              assertionErrors.push(label + '/' + stage + ': missing ' + refKey + ' capture for comparison');
            }
          }
          return st;
        };
        const guardState = await runStage('guard', 'guard', 'morph-caster-guard', 1, base, lc.be + '|reference-pose');
        const idleState = await runStage('idle', 'idle', 'morph-caster', 1, guardState, lc.be + '|reference-rest');
        const partial1State = await runStage('partial-1', 'guard', 'morph-caster-guard', 0.5, idleState, null);
        const partial2State = await runStage('partial-2', 'guard', 'morph-caster-guard', 0.5, partial1State, null);
        const idleY = idleState && idleState.found ? idleState.objectFirstPositionY : NaN;
        const partial1Y = partial1State && partial1State.found ? partial1State.objectFirstPositionY : NaN;
        const partial2Y = partial2State && partial2State.found ? partial2State.objectFirstPositionY : NaN;
        if (!Number.isFinite(idleY)) assertionErrors.push(label + '/idle: objectFirstPositionY not finite (' + idleY + ')');
        if (!Number.isFinite(partial1Y)) assertionErrors.push(label + '/partial-1: objectFirstPositionY not finite (' + partial1Y + ')');
        if (!Number.isFinite(partial2Y)) assertionErrors.push(label + '/partial-2: objectFirstPositionY not finite (' + partial2Y + ')');
        if (Number.isFinite(idleY) && Number.isFinite(partial1Y) && Number.isFinite(partial2Y)) {
          const d1 = Math.abs(partial1Y - idleY);
          const d2 = Math.abs(partial2Y - idleY);
          ev.partialProgress = { idle: idleY, partial1: partial1Y, partial2: partial2Y, d1, d2 };
          if (!(d2 > d1 + 1e-7)) {
            assertionErrors.push(label + ': partial alpha progress |partial2-idle|=' + d2 + ' not > |partial1-idle|=' + d1 + ' + 1e-7');
          }
        } else {
          ev.partialProgress = { idle: idleY, partial1: partial1Y, partial2: partial2Y };
        }

        liveEvidence[lc.be] = ev;
        const disposed = await driver.eval(disposeExpr('eng-' + lc.mount, lc.mount)) === true;
        if (!disposed) assertionErrors.push(label + ': engine disposal did not clear state');
        executedLive += 1;
      }

      // ---- Per-backend repeated-step partial-stage pixel check ----
      // Direct GL/WG maxRGB comparison is invalid: the backends have an
      // inherent raster offset (maxRGB 6 at partial-1, 11 at partial-2).
      // Instead assert, independently per backend, that the repeated
      // partial-1 -> partial-2 step actually animates pixels.
      liveEvidence.partialPixels = {};
      for (const be of ['gl', 'wg']) {
        const partial1Shot = liveShots[be + '|partial-1'];
        const partial2Shot = liveShots[be + '|partial-2'];
        if (!partial1Shot || !partial2Shot) {
          assertionErrors.push('live-partial/' + be + ': missing live capture (partial-1=' + (partial1Shot ? 'present' : 'missing') + ' partial-2=' + (partial2Shot ? 'present' : 'missing') + ')');
          continue;
        }
        const changed = changedPixels(partial1Shot, partial2Shot, ALL);
        liveEvidence.partialPixels[be] = changed;
        if (!(changed > 50)) assertionErrors.push('live-partial/' + be + ': partial-1 -> partial-2 changed pixels ' + changed + ' want >50');
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
      executedLive, plannedLive: PLANNED_LIVE,
      executedLiveBaselines, plannedLiveBaselines: PLANNED_LIVE,
      executedLiveStages, plannedLiveStages: PLANNED_LIVE_STAGES,
      failedAssertions: assertionErrors.length, errors, driverErrors, warnings, notFound, fatal,
      capabilities, diagnostics, liveEvidence,
    };
    fs.writeFileSync(path.join(art, 'report.json'), JSON.stringify(report, null, 2));
    const countsOk = executedStatic === PLANNED_STATIC &&
      executedLive === PLANNED_LIVE &&
      executedLiveBaselines === PLANNED_LIVE &&
      executedLiveStages === PLANNED_LIVE_STAGES;
    if (errors.length || warnings.length || notFound.length || fatal || !countsOk) {
      console.error('offscreen-shadow browser test FAILED:', JSON.stringify({
        fatal, errors: errors.length, warnings: warnings.length, notFound: notFound.length, executedStatic,
      }));
      process.exitCode = 1;
    } else {
      console.log('offscreen-shadow browser test: ' + executedStatic + ' static captures, ' +
        executedLive + ' live cases (' + executedLiveBaselines + ' baselines, ' + executedLiveStages + ' stages) complete');
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
