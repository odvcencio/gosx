#!/usr/bin/env node
// Loads one Bon Voyage scene-preview page (cmd/bon-voyage-scenepreview in the game repo) in a real browser, reports the mount's quality and
// capability attributes and the clouds and reflection state, and saves a still. Used to see which quality tier a page runs in and what that tier draws.
//
//   SCENE3D_TOOLS_DIR=<dir with node_modules/playwright-core> SCENE3D_REAL_GPU_PROFILE=<empty profile dir> SCENE3D_BROWSER=<chrome> \
//     node scripts/scene3d-bv-preview.mjs --url 'http://127.0.0.1:5191/?preset=quarter@minute=720@band=1' (@ stands for &, which cmd.exe would eat) --out <dir> [--backend webgpu|webgl] [--wait 15000] [--name quarter]
import fs from 'node:fs';
import path from 'node:path';
import { createRequire } from 'node:module';
import { webglInit } from './scene3d-gl-timer.mjs';

const toolsDir = process.env.SCENE3D_TOOLS_DIR;
if (!toolsDir) throw new Error('set SCENE3D_TOOLS_DIR');
const req = createRequire(import.meta.url);
const { chromium } = req(path.join(toolsDir, 'node_modules', 'playwright-core'));
const argv = process.argv.slice(2);
const arg = (n, d) => { const i = argv.indexOf(n); return i >= 0 ? argv[i + 1] : d; };
const url = arg('--url', '').replace(/@/g, '&'), outDir = arg('--out', ''), backend = arg('--backend', 'webgpu'), wait = Number(arg('--wait', '15000')), name = arg('--name', 'preview');
if (!url || !outDir) throw new Error('pass --url and --out');
fs.mkdirSync(outDir, { recursive: true });
const context = await chromium.launchPersistentContext(process.env.SCENE3D_REAL_GPU_PROFILE, { executablePath: process.env.SCENE3D_BROWSER, headless: true,
  viewport: { width: 1440, height: 900 }, args: ['--enable-unsafe-webgpu', '--ignore-gpu-blocklist', '--enable-features=Vulkan'] });
const page = context.pages()[0] || await context.newPage();
if (backend === 'webgl') await page.addInitScript(webglInit);
page.on('console', (m) => { if (/warn|error/i.test(m.type())) console.log('console', m.type(), m.text().slice(0, 200)); });
const throttle = Number(arg('--cpu-throttle', '1'));
if (throttle > 1) await (await context.newCDPSession(page)).send('Emulation.setCPUThrottlingRate', { rate: throttle }); // slows the page so the quality governor sees missed frames
await page.goto(url, { waitUntil: 'load' });
// Print the governor's tier every 2 seconds while waiting, to see when and why it moves.
for (let t = 0; t < wait; t += 2000) {
  await page.waitForTimeout(2000);
  console.log((t + 2000) + ' ms', await page.evaluate(() => { const m = document.querySelector('[data-gosx-scene3d]'); const g = (n) => m && m.getAttribute('data-gosx-scene3d-' + n); return ['quality-tier', 'quality-reason', 'quality-frame-ms', 'capability-tier'].map((n) => n + '=' + g(n)).join(' '); }));
}
const info = await page.evaluate(() => {
  const mount = document.querySelector('[data-gosx-scene3d]') || document.querySelector('[data-gosx-engine]');
  const attrs = {};
  if (mount) for (const a of mount.attributes) if (/quality|capability|low-power|software|renderer|backend|ocean|webgpu-pipeline|reflect|cloud|atmosphere|dpr|ladder|rung/i.test(a.name)) attrs[a.name.replace('data-gosx-scene3d-', '')] = a.value;
  return attrs;
});
console.log(JSON.stringify(info, null, 1));
await page.screenshot({ path: path.join(outDir, name + '.png') });
await context.close();
