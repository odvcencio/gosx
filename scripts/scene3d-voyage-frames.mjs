#!/usr/bin/env node
// Real-GPU frames of the Voyage demo (/demos/voyage): one still per sea-state preset, the two views, a click-through
// of the controls, and short frame sequences at 1x and 1,000x game speed.
//
//   SCENE3D_TOOLS_DIR=<dir containing node_modules/playwright-core> \
//   SCENE3D_REAL_GPU_PROFILE=<empty browser profile dir> SCENE3D_BROWSER=<chrome.exe> \
//     node scripts/scene3d-voyage-frames.mjs --base http://localhost:8091 --out <dir> \
//       [--size 1440x900] [--wait 15000] [--only presets|controls|sequence] [--steps 12] [--interval 500]
//
// Serve the docs site from a worktree with `cd examples/gosx-docs && PORT=8091 GOWORK=off go run ../../cmd/gosx dev .`
// after `make build-bootstrap`. Output names:
//   preset-<state>.png                    quarter view at 1x, after --wait ms
//   controls-<n>-<what>.png               after clicking Storm, then 1,000x, then Calm (with --wait ms to settle)
//   seq-<speed>-<nn>.png                  --steps stills --interval ms apart in the waterline view, moderate sea
// The ocean pass state is read from data-gosx-scene3d-ocean ("...+spectrum" when the spectral ocean draws).
import fs from 'node:fs';
import path from 'node:path';
import { createRequire } from 'node:module';
import { webglInit, glTiming, describeTiming } from './scene3d-gl-timer.mjs';

const toolsDir = process.env.SCENE3D_TOOLS_DIR;
if (!toolsDir) throw new Error('set SCENE3D_TOOLS_DIR to a directory that contains node_modules/playwright-core');
const toolsRequire = createRequire(import.meta.url);
let chromium;
for (const name of ['playwright', 'playwright-core']) {
  try { ({ chromium } = toolsRequire(path.join(toolsDir, 'node_modules', name))); break; } catch (error) { if (name === 'playwright-core') throw error; }
}
const argv = process.argv.slice(2);
const arg = (name, fallback) => { const i = argv.indexOf(name); return i >= 0 ? argv[i + 1] : fallback; };
const backend = arg('--backend', 'webgpu'); // 'webgl' hides WebGPU so Scene3D draws with WebGL2, and times the frames with EXT_disjoint_timer_query_webgl2
if (backend !== 'webgpu' && backend !== 'webgl') throw new Error('--backend must be webgpu or webgl');
const base = arg('--base', 'http://localhost:8091').replace(/\/$/, '');
const outDir = arg('--out', '');
if (!outDir) throw new Error('pass --out <dir>');
fs.mkdirSync(outDir, { recursive: true });
const [W, H] = arg('--size', '1440x900').split('x').map(Number);
const wait = Number(arg('--wait', '15000'));
const only = arg('--only', '');
const steps = Number(arg('--steps', '12'));
const interval = Number(arg('--interval', '500'));
const profile = process.env.SCENE3D_REAL_GPU_PROFILE;
if (!profile) throw new Error('set SCENE3D_REAL_GPU_PROFILE (and SCENE3D_BROWSER) to run on a real GPU');

const browser = await chromium.launchPersistentContext(profile, {
  executablePath: process.env.SCENE3D_BROWSER, headless: false, viewport: { width: W, height: H }, deviceScaleFactor: 1,
  args: ['--enable-gpu', '--use-angle=d3d11', '--enable-unsafe-webgpu', '--ignore-gpu-blocklist', '--enable-dawn-features=allow_unsafe_apis'],
});
const page = browser.pages()[0] || await browser.newPage();
if (backend === 'webgl') await page.addInitScript(webglInit);
page.on('pageerror', error => console.error('[pageerror]', error.message));
page.on('console', message => { if (['warning', 'error'].includes(message.type()) && !/preloaded|CORS|Failed to load resource/.test(message.text())) console.error('[console]', message.text().slice(0, 300)); });

async function open(view, period) {
  await page.goto(`${base}/demos/voyage?view=${view}&period=${period}`, { waitUntil: 'load' });
  await page.waitForFunction(() => document.querySelector('[data-gosx-scene3d-ocean]'), null, { timeout: 90000 }).catch(() => console.error('[warn] no ocean pass state for ' + period));
}
const state = () => page.evaluate(() => {
  const mount = document.querySelector('[data-gosx-scene3d-ocean]') || document.body;
  const root = document.querySelector('.vy');
  return { ocean: mount.getAttribute('data-gosx-scene3d-ocean'), period: root && root.getAttribute('data-period'), view: root && root.getAttribute('data-view'),
    failed: mount.getAttribute('data-gosx-scene3d-webgpu-pipeline-failed'), gpuMs: mount.getAttribute('data-gosx-scene3d-webgpu-gpu-ms') };
});
const shot = async name => { await page.screenshot({ path: path.join(outDir, name + '.png') }); const s = await state(); console.log(`${name}: ocean ${s.ocean}, period ${s.period}, view ${s.view}, ${backend === 'webgl' ? describeTiming(await glTiming(page)) : 'gpu ' + s.gpuMs + ' ms'}${s.failed ? ', pipeline failures ' + s.failed : ''}`); return s; };
let failed = false;
const check = s => { if (s.failed || !/\+spectrum$/.test(String(s.ocean))) failed = true; };

if (!only || only === 'presets') {
  for (const sea of ['calm', 'moderate', 'rough', 'storm']) {
    await open('quarter', `${sea}-1x`);
    await page.waitForTimeout(wait);
    check(await shot(`preset-${sea}`));
  }
}
if (!only || only === 'controls') {
  await open('quarter', 'moderate-1x');
  await page.waitForTimeout(4000);
  check(await shot('controls-0-start'));
  await page.getByRole('button', { name: 'Storm' }).click();
  await page.waitForTimeout(2000);
  check(await shot('controls-1-storm-2s'));
  await page.waitForTimeout(wait + 10000);
  check(await shot('controls-2-storm-settled'));
  await page.getByRole('button', { name: '1,000×' }).click();
  await page.waitForTimeout(wait);
  const s = await shot('controls-3-storm-1000x');
  check(s);
  if (s.period !== 'storm-1000x') { console.error('FAIL: clicking Storm then 1,000x gave period ' + s.period); failed = true; }
  await page.getByRole('button', { name: 'Calm' }).click();
  await page.waitForTimeout(wait + 12000);
  check(await shot('controls-4-calm-after-storm'));
}
if (!only || only === 'sequence') {
  for (const speed of [1, 1000]) {
    await open('waterline', `moderate-${speed}x`);
    await page.waitForTimeout(wait);
    for (let i = 0; i < steps; i++) {
      const t0 = Date.now();
      await page.screenshot({ path: path.join(outDir, `seq-${speed}x-${String(i).padStart(2, '0')}.png`) });
      await page.waitForTimeout(Math.max(0, interval - (Date.now() - t0)));
    }
    const s = await state();
    console.log(`seq-${speed}x: ${steps} stills about ${interval} ms apart, ocean ${s.ocean}, period ${s.period}`);
    check(s);
  }
}
await browser.close();
process.exit(failed ? 1 : 0);
