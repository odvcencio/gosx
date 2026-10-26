// Browser proof for the running example; this file is never served to the app.
// SCENE3D_TOOLS_DIR=/path/containing/node_modules SCENE3D_BROWSER=/path/to/chrome \
//   node examples/turbofan/verify.mjs http://127.0.0.1:8080
import assert from 'node:assert/strict';
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
for (const file of ['main.go', 'app/program.go', 'app/page.server.go', 'app/page.gsx', 'public/turbofan.css', '../../scene/geom/lathe.go']) {
  report.sourceSHA256[file] = createHash('sha256').update(await readFile(path.join(root, file))).digest('hex');
}
report.buildManifestSHA256 = createHash('sha256').update(await readFile(path.join(root, 'dist/build.json'))).digest('hex');
const mount = '[data-gosx-scene3d="true"]';
const context = await browser.newContext({ viewport: { width: 1440, height: 1080 }, deviceScaleFactor: 1 });
context.on('page', page => {
  page.on('pageerror', error => report.errors.push(error.message));
  page.on('response', response => {
    if (response.status() >= 400 && !response.url().endsWith('/favicon.ico')) report.errors.push(`${response.status()} ${response.url()}`);
  });
});
const page = await context.newPage();
async function ready(target = page) {
  await target.waitForFunction(selector => document.querySelector(selector)?.getAttribute('data-gosx-scene3d-mounted') === 'true', mount);
  await target.evaluate(() => document.fonts.ready);
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
    regions: [...document.querySelectorAll('.viewer-heading,.scene-area,.viewer-controls,.viewer-footnote')].map(e => {
      const b = e.getBoundingClientRect(); return { name: e.className, x: b.x, width: b.width, height: b.height };
    }),
  }));
  assert(bounds.scrollWidth <= bounds.width, `${label}: horizontal overflow`);
  for (const region of bounds.regions) assert(region.x >= 0 && region.x + region.width <= bounds.width + 1 && region.height > 0, `${label}: clipped ${region.name}`);
  report.checks.push({ name: `${label} fit`, observed: bounds });
}
async function click(name) {
  await page.getByRole('link', { name, exact: true }).click();
  await ready();
}
try {
  await visit('?backend=webgl');
  const initial = await sceneSummary();
  assert.equal(initial.backend, 'webgl');
  assert.equal(initial.objects, 78);
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

  await click('Pause rotation');
  assert.equal((await sceneSummary()).groups.length, 0);
  const stopped1 = await page.locator('canvas').screenshot();
  await page.waitForTimeout(700);
  const stopped2 = await page.locator('canvas').screenshot();
  assert(stopped1.equals(stopped2), 'static study must keep identical pixels');
  report.checks.push({ name: 'paused static study', observed: 'identical canvas pixels across 700 ms; no retained spinning groups' });
  await click('Exterior');
  assert.equal((await sceneSummary()).objects, 66);
  assert.equal(new URL(page.url()).searchParams.get('motion'), 'paused');
  await page.screenshot({ path: path.join(output, 'exterior.png'), fullPage: true });
  await click('Cutaway');
  await click('Side');
  assert.equal((await sceneSummary()).camera.x, -.1);
  await page.screenshot({ path: path.join(output, 'side.png'), fullPage: true });
  await click('Intake');
  assert.equal((await sceneSummary()).camera.x, -11.5);
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
  report.checks.push({ name: 'casing, camera presets, orbit and zoom', observed: 'all links retain motion choice; real pointer drag and wheel change camera' });
  await click('Resume rotation');
  assert.equal((await sceneSummary()).groups.length, 2);

  await visit('?shell=bad&view=bad&motion=bad&unknown=discard');
  assert.equal(await page.getByRole('link', { name: 'Cutaway', exact: true }).getAttribute('aria-current'), 'true');
  assert(!(await page.getByRole('link', { name: 'Side', exact: true }).getAttribute('href')).includes('unknown'));
  report.checks.push({ name: 'invalid query values', observed: 'default cutaway/perspective/animation restored and unknown parameters dropped' });

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
  report.checks.push({ name: 'mobile reduced motion', observed: '390 × 844 touch viewport; identical canvas pixels across 700 ms' });
  await mobile.close();

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
