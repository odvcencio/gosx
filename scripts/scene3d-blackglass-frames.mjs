#!/usr/bin/env node
// Real-GPU screenshots of the Blackglass Beach demo (/demos/beacon) in the Shore, Overlook and Ship
// views at golden hour, blue hour and midday, on WebGPU.
//
//   SCENE3D_TOOLS_DIR=<dir containing node_modules/playwright-core> \
//   SCENE3D_REAL_GPU_PROFILE=<empty browser profile dir> SCENE3D_BROWSER=<chrome.exe> \
//     node scripts/scene3d-blackglass-frames.mjs --base http://localhost:8091 --out <dir> \
//       [--wait 20000] [--size 1440x900] [--only <substring>] [--prefix after]
//
// Serve the docs site from a worktree with:
//   cd examples/gosx-docs && PORT=8091 GOSX_DEV=1 GOWORK=off go run .
// (dev mode serves the runtime bundles from client/js, so run `make build-bootstrap` first.) Pass --base
// https://gosx.m31labs.dev to capture the deployed site instead. The page URL carries the view and period:
// /demos/beacon?view=<shore|cliff|ship>&period=<golden-hour|blue-hour|noon>. Each frame waits --wait ms for
// models, textures and the persistent foam, reads the ocean pass state from the mount attribute
// data-gosx-scene3d-ocean ("surface+spectrum" when the spectral ocean draws), and saves
// <prefix>-<view>-<period>.png. Prints the pass state and, when the device has timestamp-query, the
// renderer's whole-frame GPU time.
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
const wait = Number(arg('--wait', '20000'));
const only = arg('--only', '');
const prefix = arg('--prefix', 'frame');
const profile = process.env.SCENE3D_REAL_GPU_PROFILE;
if (!profile) throw new Error('set SCENE3D_REAL_GPU_PROFILE (and SCENE3D_BROWSER) to run on a real GPU');

const browser = await chromium.launchPersistentContext(profile, {
  executablePath: process.env.SCENE3D_BROWSER, headless: false, viewport: { width: W, height: H }, deviceScaleFactor: 1,
  args: ['--enable-gpu', '--use-angle=d3d11', '--enable-unsafe-webgpu', '--ignore-gpu-blocklist', '--enable-dawn-features=allow_unsafe_apis'],
});
const page = browser.pages()[0] || await browser.newPage();
if (backend === 'webgl') await page.addInitScript(webglInit);
page.on('pageerror', error => console.error('[pageerror]', error.message));
page.on('console', message => { if (message.type() === 'warning' || message.type() === 'error') console.error('[console]', message.text().slice(0, 300)); });

let failed = false;
for (const view of ['shore', 'cliff', 'ship']) {
  for (const period of ['golden-hour', 'blue-hour', 'noon']) {
    const name = `${prefix}-${view}-${period}`;
    if (only && !name.includes(only)) continue;
    await page.goto(`${base}/demos/beacon?view=${view}&period=${period}`, { waitUntil: 'load' });
    // The page shows a poster until the scene mounts. Wait for the ocean pass to publish its state, then let
    // the foam and the presentation settle for --wait ms.
    await page.waitForFunction(() => document.querySelector('[data-gosx-scene3d-ocean]'), null, { timeout: 90000 }).catch(() => console.error('[warn] no ocean pass state published for ' + name));
    await page.waitForTimeout(wait);
    const state = await page.evaluate(() => {
      const mount = document.querySelector('[data-gosx-scene3d-ocean]') || document.querySelector('[data-gosx-engine="GoSXScene3D"]') || document.body;
      const attr = name => mount.getAttribute(name);
      return { ocean: attr('data-gosx-scene3d-ocean'), backend: attr('data-gosx-scene3d-renderer') || attr('data-gosx-scene3d-backend'),
        failed: attr('data-gosx-scene3d-webgpu-pipeline-failed'), gpuMs: attr('data-gosx-scene3d-webgpu-gpu-ms') };
    });
    await page.screenshot({ path: path.join(outDir, name + '.png') });
    console.log(`${name}: ocean ${state.ocean}, backend ${state.backend}, ${backend === 'webgl' ? describeTiming(await glTiming(page)) : 'gpu ' + state.gpuMs + ' ms'}${state.failed ? ', pipeline failures ' + state.failed : ''}`);
    if (state.failed) failed = true;
  }
}
await browser.close();
process.exit(failed ? 1 : 0);
