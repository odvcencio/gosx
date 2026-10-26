// Browser proof for the running example; this file is never served to the app.
// SCENE3D_TOOLS_DIR=/path/containing/node_modules SCENE3D_BROWSER=/path/to/chrome \
//   node examples/turbofan/verify.mjs http://127.0.0.1:8080
import assert from 'node:assert/strict';
import { execFileSync } from 'node:child_process';
import { createHash } from 'node:crypto';
import { mkdir, readFile, writeFile } from 'node:fs/promises';
import { createRequire } from 'node:module';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const root = path.dirname(fileURLToPath(import.meta.url));
const require = createRequire(import.meta.url);
const { chromium } = process.env.SCENE3D_TOOLS_DIR
  ? require(path.join(process.env.SCENE3D_TOOLS_DIR, 'node_modules/playwright'))
  : require('playwright');
const base = process.argv[2] || 'http://127.0.0.1:8080';
const output = process.env.TURBOFAN_EVIDENCE_DIR || path.join(root, 'evidence');
await mkdir(output, { recursive: true });
const browser = await chromium.launch({
  executablePath: process.env.SCENE3D_BROWSER || undefined,
  headless: true,
  args: ['--no-sandbox', '--use-angle=swiftshader', '--enable-unsafe-swiftshader', '--enable-unsafe-webgpu', '--ignore-gpu-blocklist'],
});
const report = { browser: browser.version(), environment: 'Linux headless Chromium / software rendering', sourceSHA256: {}, checks: [], errors: [] };
report.frameworkRevision = execFileSync('git', ['rev-parse', 'HEAD'], { cwd: root, encoding: 'utf8' }).trim();
for (const file of ['main.go', 'main_test.go', 'gosx.config.json', 'verify.mjs', 'app/program.go', 'app/page.server.go', 'app/page.gsx', 'public/turbofan.css', 'cycle/model.go', 'cycle/model_test.go', 'cycle/view.go', 'browser/main.go', '../../scene/geom/lathe.go']) {
  report.sourceSHA256[file] = createHash('sha256').update(await readFile(path.join(root, file))).digest('hex');
}
const manifestBytes = await readFile(path.join(root, 'dist/build.json'));
report.buildManifestSHA256 = createHash('sha256').update(manifestBytes).digest('hex');
const engineFile = JSON.parse(manifestBytes).goWASM.cycle.file;
const enginePath = path.join(root, 'dist/assets/go-wasm', engineFile);
const engineURL = `${base}/gosx/assets/go-wasm/${engineFile}`;
const engineWASM = await readFile(enginePath);
report.cycleEngine = {
  bytes: engineWASM.length, sha256: createHash('sha256').update(engineWASM).digest('hex'),
  brotliBytes: (await readFile(enginePath + '.br')).length,
  gzipBytes: (await readFile(enginePath + '.gz')).length,
};
const mount = '[data-gosx-scene3d="true"]';
const context = await browser.newContext({ viewport: { width: 1440, height: 1080 }, deviceScaleFactor: 1 });
context.on('page', page => {
  page.on('pageerror', error => report.errors.push(error.message));
  page.on('response', response => {
    if (response.status() >= 400 && !response.url().endsWith('/favicon.ico')) report.errors.push(`${response.status()} ${response.url()}`);
    if (response.url() === engineURL && !report.cycleEngine.response) {
      report.cycleEngine.response = { status: response.status(), encoding: response.headers()['content-encoding'], contentType: response.headers()['content-type'] };
    }
  });
});
const page = await context.newPage();
async function ready(target = page) {
  await target.waitForFunction(selector => document.querySelector(selector)?.getAttribute('data-gosx-scene3d-mounted') === 'true', mount);
  await target.evaluate(() => document.fonts.ready);
  await target.waitForFunction(() => document.querySelector('.segmented button')?.disabled === false);
  await target.waitForFunction(() => document.querySelector('#cycle-workbench')?.getAttribute('data-cycle-ready') === 'true');
}
async function visit(query = '', target = page) {
  await target.goto(`${base}/${query}`, { waitUntil: 'networkidle' });
  await ready(target);
}
async function sceneSummary(target = page) {
  return target.locator(mount).evaluate(element => {
    const state = element.__gosxScene3DState;
    return {
      backend: element.getAttribute('data-gosx-scene3d-renderer'),
      clock: Number(element.getAttribute('data-gosx-scene3d-animation-clock')),
      reducedMotion: element.getAttribute('data-gosx-scene3d-reduced-motion'),
      camera: element.__gosxScene3DHandle.getCamera(),
      objects: state.objects.size,
      visibleObjects: [...state.objects.values()].filter(o => o.visible !== false).length,
      animation: element.__gosxScene3DHandle.getAnimationClock(),
      groups: state.groups.map(g => ({ id: g.id, spinX: g.spinX })),
      fanVertices: state.objects.get('fan')?.vertices?.count,
      triangles: [...state.objects.values()].reduce((sum, o) => sum + (o.vertices?.indices?.length || o.vertices?.positions?.length / 3 || 0) / 3, 0),
      renderTruth: JSON.parse(element.getAttribute('data-gosx-scene3d-render-backend-truth') || '{}'),
    };
  });
}
async function fit(target, label) {
  const bounds = await target.evaluate(() => ({
    width: innerWidth, scrollWidth: document.documentElement.scrollWidth,
    regions: [...document.querySelectorAll('.viewer-heading,.scene-area,.viewer-controls,.viewer-footnote,.workbench,.workbench-grid')].map(e => {
      const b = e.getBoundingClientRect(); return { name: e.className, x: b.x, width: b.width, height: b.height };
    }),
  }));
  assert(bounds.scrollWidth <= bounds.width, `${label}: horizontal overflow`);
  for (const region of bounds.regions) assert(region.x >= 0 && region.x + region.width <= bounds.width + 1 && region.height > 0, `${label}: clipped ${region.name}`);
  report.checks.push({ name: `${label} fit`, observed: bounds });
}
async function click(name) {
  await page.getByRole('button', { name, exact: true }).click();
  await ready();
}
try {
  await visit('?backend=webgl');
  assert.equal(report.cycleEngine.response.encoding, 'br', 'the numerical engine must ship compressed');
  assert.match(report.cycleEngine.response.contentType, /application\/wasm/);
  const gzipEngine = await page.request.get(engineURL, { headers: { 'Accept-Encoding': 'gzip' } });
  assert.equal(gzipEngine.headers()['content-encoding'], 'gzip');
  assert.equal(createHash('sha256').update(await gzipEngine.body()).digest('hex'), report.cycleEngine.sha256, 'compressed module must decode to the exact built engine');
  report.checks.push({ name: 'compressed Go/WASM delivery', observed: report.cycleEngine });
  const initial = await sceneSummary();
  assert.equal(initial.backend, 'webgl');
  assert.equal(initial.objects, 88);
  assert.equal(initial.visibleObjects, 78);
  assert(initial.fanVertices > 1000, 'custom fan vertices must reach the renderer');
  assert.equal(initial.groups.length, 2);
  await fit(page, 'desktop');
  const before = await page.locator('canvas').screenshot({ path: path.join(output, 'motion-before.png') });
  await page.waitForTimeout(700);
  const after = await page.locator('canvas').screenshot({ path: path.join(output, 'motion-after.png') });
  assert(!before.equals(after), 'running rotor must change visible pixels');
  assert((await sceneSummary()).clock > initial.clock, 'scene clock must advance');
  report.checks.push({ name: 'visible rotor animation', observed: initial });
  await page.screenshot({ path: path.join(root, 'preview.png'), fullPage: true });

  const canvas = await page.locator('canvas').elementHandle();
  const document = await page.evaluateHandle(() => window.document);
  const requests = [];
  const collect = r => { if (['document', 'fetch', 'xhr'].includes(r.resourceType())) requests.push(r.url()); };
  page.on('request', collect);
  const cycleState = () => page.locator('#cycle-workbench').evaluate(e => JSON.parse(e.getAttribute('data-cycle-state')));
  const baselineCycle = await cycleState();
  assert.equal(baselineCycle.valid, true);
  // Native Go results cross-check the actual standard-Go WASM execution.
  // The separate analytical benchmark establishes equation correctness.
  assert(Math.abs(baselineCycle.result.netThrustN - 99481.050149483) < 1e-6);
  assert(Math.abs(baselineCycle.result.fuelFlowKGS - .746732036) < 1e-9);
  const temperaturePath = await page.locator('[data-cycle-plot]').getAttribute('d');
  const t4 = page.locator('[name=turbineInletK]');
  await t4.focus();
  await t4.press('End');
  const hotCycle = await cycleState();
  assert.equal(hotCycle.result.inputs.turbineInletK, 1800, 'keyboard input must run the solver');
  assert(hotCycle.result.netThrustN > baselineCycle.result.netThrustN);
  assert(hotCycle.result.fuelFlowKGS > baselineCycle.result.fuelFlowKGS);
  assert.notEqual(await page.locator('[data-cycle-plot]').getAttribute('d'), temperaturePath);
  await click('Cruise');
  const cruiseCycle = await cycleState();
  assert.equal(cruiseCycle.result.inputs.altitudeM, 11000);
  assert.equal(cruiseCycle.result.inputs.mach, .78);
  assert.equal(cruiseCycle.result.inputs.turbineInletK, 1800, 'flight preset must preserve component design');
  assert(cruiseCycle.result.maxRelativeResidual < 1e-12);
  const [download] = await Promise.all([
    page.waitForEvent('download'), page.getByRole('button', { name: /Export point/ }).click(),
  ]);
  const csv = await readFile(await download.path(), 'utf8');
  assert(csv.includes('fuel LHV,43000000,J/kg'));
  assert(csv.includes('altitude,11000,m'));
  assert(csv.includes('core pressure thrust'));
  assert(csv.includes('9 · Core nozzle (static)'));
  const csvNumber = name => Number(csv.split('\n').find(line => line.startsWith(name + ',')).split(',')[1]);
  assert(Math.abs(csvNumber('net thrust') - cruiseCycle.result.netThrustN) < 1e-8, 'export must contain the currently selected point');
  assert(Math.abs(csvNumber('fuel flow') - cruiseCycle.result.fuelFlowKGS) < 1e-10);
  await download.saveAs(path.join(output, 'cycle-cruise.csv'));
  await click('Reset all');
  const inputTimings = [];
  async function setRange(name, value) {
    const elapsed = await page.locator(`[name=${name}]`).evaluate((e, value) => {
      const start = performance.now();
      e.value = String(value);
      e.dispatchEvent(new Event('input', { bubbles: true }));
      return performance.now() - start;
    }, value);
    inputTimings.push(elapsed);
  }
  await setRange('fanPressureRatio', 1.8);
  await setRange('bypassRatio', 12);
  await setRange('turbineInletK', 1000);
  assert.equal((await cycleState()).valid, false);
  assert.equal(await page.locator('[data-cycle-output=net-thrust]').textContent(), '—');
  assert.equal(await page.locator('[data-cycle-plot]').getAttribute('d'), '');
  assert(await page.getByRole('button', { name: /Export point/ }).isDisabled());
  assert.match(await page.locator('[data-cycle-output=status]').textContent(), /Infeasible point/);
  await page.locator('#cycle-workbench').screenshot({ path: path.join(output, 'cycle-infeasible.png') });
  await click('Reset all');
  assert.equal((await cycleState()).valid, true, 'reset must recover from an infeasible point');
  assert.deepEqual((await cycleState()).result, baselineCycle.result);
  assert(await canvas.evaluate(element => element === document.querySelector('canvas')), 'cycle inputs must preserve the mounted scene');
  assert(await document.evaluate(element => element === window.document), 'cycle inputs must preserve the document');
  assert.deepEqual(requests, [], 'cycle controls and CSV export must not navigate or fetch application data');
  await page.locator('#cycle-workbench').screenshot({ path: path.join(root, 'cycle-preview.png') });
  report.checks.push({ name: 'local thermodynamic cycle and export', observed: {
    baseline: baselineCycle.result, hot: hotCycle.result, cruise: cruiseCycle.result,
    inputHandlerMs: inputTimings, csv: 'cycle-cruise.csv; assumptions and station results present',
    invalidPoint: 'cleared results, empty plot and disabled export; reset restores the exact default',
    requests: 0, documentAndCanvas: 'preserved',
  } });
  await click('Pause rotation');
  const paused = await sceneSummary();
  assert.equal(paused.animation.paused, true);
  assert(paused.clock > 0, 'pause must keep the running pose, not reset to zero');
  const stopped1 = await page.locator('canvas').screenshot();
  await page.waitForTimeout(700);
  const stopped2 = await page.locator('canvas').screenshot();
  assert(stopped1.equals(stopped2), 'static study must keep identical pixels');
  assert.equal((await sceneSummary()).clock, paused.clock);
  report.checks.push({ name: 'pause at current pose', observed: { clock: paused.clock, pixelResult: 'identical canvas pixels across 700 ms' } });
  await click('Exterior');
  assert.equal((await sceneSummary()).objects, 88);
  assert.equal((await sceneSummary()).visibleObjects, 76);
  assert.equal((await sceneSummary()).clock, paused.clock);
  assert.deepEqual((await sceneSummary()).camera, paused.camera);
  await page.screenshot({ path: path.join(output, 'exterior.png'), fullPage: true });
  await click('Cutaway');
  await click('Side');
  assert(Math.abs((await sceneSummary()).camera.x + .1) < 1e-6);
  await page.screenshot({ path: path.join(output, 'side.png'), fullPage: true });
  await click('Intake');
  assert(Math.abs((await sceneSummary()).camera.x + 11.5) < 1e-6);
  await page.screenshot({ path: path.join(output, 'intake.png'), fullPage: true });
  await click('Perspective');
  const cameraBefore = (await sceneSummary()).camera;
  const box = await page.locator('canvas').boundingBox();
  await page.mouse.move(box.x + box.width * .55, box.y + box.height * .5);
  await page.mouse.down();
  await page.mouse.move(box.x + box.width * .62, box.y + box.height * .55, { steps: 12 });
  await page.mouse.up();
  await page.waitForTimeout(300);
  const cameraAfterOrbit = (await sceneSummary()).camera;
  assert.notEqual(cameraAfterOrbit.x, cameraBefore.x, 'drag must orbit camera');
  await page.mouse.wheel(0, -200);
  await page.waitForTimeout(300);
  assert.notEqual((await sceneSummary()).camera.z, cameraAfterOrbit.z, 'wheel must zoom camera');
  await click('Exterior');
  await click('Cutaway');
  await page.goBack();
  await page.waitForFunction(() => document.querySelector('.viewer').getAttribute('data-period') === 'exterior');
  await page.goForward();
  await page.waitForFunction(() => document.querySelector('.viewer').getAttribute('data-period') === 'cutaway');
  assert.equal((await sceneSummary()).clock, paused.clock);
  await click('Resume rotation');
  await page.waitForFunction(selector => document.querySelector(selector).__gosxScene3DHandle.getAnimationClock().paused === false, mount);
  assert(await canvas.evaluate(element => element === document.querySelector('canvas')), 'controls must keep the same canvas');
  assert(await document.evaluate(element => element === window.document), 'controls must keep the same document');
  page.off('request', collect);
  assert.deepEqual(requests, [], 'controls must not navigate or fetch application data');
  report.checks.push({ name: 'immediate controls preserve scene', observed: 'same document and canvas; zero document/fetch/XHR requests; casing preserves camera and clock; camera presets, orbit, zoom, Back/Forward and resume pass' });

  await visit('?shell=bad&view=bad&motion=bad&unknown=discard');
  assert.equal(await page.getByRole('button', { name: 'Cutaway', exact: true }).getAttribute('aria-pressed'), 'true');
  report.checks.push({ name: 'invalid query values', observed: 'default cutaway/perspective/animation restored' });

  const mobile = await browser.newContext({ viewport: { width: 390, height: 844 }, deviceScaleFactor: 1, isMobile: true, hasTouch: true, reducedMotion: 'reduce' });
  const phone = await mobile.newPage();
  phone.on('pageerror', error => report.errors.push(error.message));
  await visit('?backend=webgl', phone);
  await fit(phone, 'mobile');
  assert.equal((await sceneSummary(phone)).reducedMotion, 'true');
  const reduced1 = await phone.locator('canvas').screenshot();
  await phone.waitForTimeout(700);
  const reduced2 = await phone.locator('canvas').screenshot();
  assert(reduced1.equals(reduced2), 'reduced motion must keep identical pixels');
  await phone.screenshot({ path: path.join(output, 'mobile.png'), fullPage: true });
  await phone.screenshot({ path: path.join(root, 'mobile-preview.png'), fullPage: true });
  await phone.getByRole('button', { name: 'Cruise', exact: true }).tap();
  const phoneCycle = await phone.locator('#cycle-workbench').evaluate(e => JSON.parse(e.getAttribute('data-cycle-state')));
  assert.equal(phoneCycle.result.inputs.mach, .78, 'touch preset must solve the mobile cycle');
  assert.equal(phoneCycle.valid, true);
  const scrollableTable = phone.getByRole('region', { name: 'Scrollable station conditions' });
  await scrollableTable.evaluate(e => { e.scrollLeft = e.scrollWidth; });
  assert(await scrollableTable.evaluate(e => e.scrollLeft > 0), 'all station columns must remain reachable on mobile');
  report.checks.push({ name: 'mobile reduced motion', observed: '390 × 844 touch viewport; identical canvas pixels across 700 ms' });
  report.checks.push({ name: 'mobile cycle controls', observed: 'touch cruise preset updates the local solver; station flow column reachable by scrolling' });
  await mobile.close();

  const noScript = await browser.newContext({ javaScriptEnabled: false });
  const staticPage = await noScript.newPage();
  await staticPage.goto(base, { waitUntil: 'load' });
  assert.equal(await staticPage.locator('[data-cycle-output=net-thrust]').textContent(), '99.5');
  assert(await staticPage.locator('[name=altitudeM]').isDisabled());
  assert.equal(await staticPage.locator('[data-cycle-station]').count(), 8);
  report.checks.push({ name: 'server calculated fallback', observed: 'initial thrust and eight stations render without JavaScript; interactive controls remain disabled' });
  await noScript.close();

  await visit('?backend=webgpu');
  // A created device is not a submitted image. Wait for multiple WebGPU
  // frames, or for the renderer's explicit device-loss fallback to settle.
  await page.waitForFunction(selector => {
    const element = document.querySelector(selector);
    return element.getAttribute('data-gosx-scene3d-renderer') === 'webgl' ||
      Number(element.getAttribute('data-gosx-scene3d-webgpu-frame-seq')) >= 3;
  }, mount);
  const gpuBefore = await page.locator('canvas').screenshot();
  await page.waitForTimeout(700);
  const gpuAfter = await page.locator('canvas').screenshot();
  assert(!gpuBefore.equals(gpuAfter), 'preferred backend or fallback must draw visible moving geometry');
  const gpu = await sceneSummary();
  await page.screenshot({ path: path.join(output, 'webgpu.png'), fullPage: true });
  report.checks.push({ name: 'WebGPU preference', observed: gpu });
  assert(['webgl', 'webgpu'].includes(gpu.backend), 'must render or explicitly fall back');
  assert.equal(report.errors.length, 0, report.errors.join('\n'));
  report.result = 'pass';
} catch (error) {
  report.result = 'fail';
  report.failure = error.stack;
  throw error;
} finally {
  await writeFile(path.join(output, 'verification.json'), JSON.stringify(report, null, 2) + '\n');
  console.log(JSON.stringify({ result: report.result, checks: report.checks.map(c => c.name), failure: report.failure, errors: report.errors }, null, 2));
  await browser.close();
}
