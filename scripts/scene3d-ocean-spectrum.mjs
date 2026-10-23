#!/usr/bin/env node
// Real-GPU frames of the spectral (FFT) ocean beside the Gerstner ocean, on WebGPU (default) or WebGL2 (--backend webgl).
//
//   SCENE3D_TOOLS_DIR=<dir containing node_modules/playwright-core> \
//   SCENE3D_REAL_GPU_PROFILE=<empty browser profile dir> SCENE3D_BROWSER=<chrome.exe> \
//   SCENE3D_JS_DIR=<dir holding copies of the bundles> \
//     node scripts/scene3d-ocean-spectrum.mjs --out <dir> [--size 1280x720] [--frames 240] [--only <substring>] [--backend webgpu|webgl]
//
// Windows Chrome with a GPU (Linux browsers cannot run on the development host):
//   C:\Temp\showcase-tools\node.exe scripts\scene3d-ocean-spectrum.mjs --out C:\Temp\gosx-cypress\out
// SCENE3D_JS_DIR must hold bootstrap.js, bootstrap-feature-scene3d-webgpu.js (or -webgl.js with --backend webgl) and
// bootstrap-feature-scene3d-ocean-fft.js (client/js after `make build-bootstrap`). On WebGL2 the GPU time of the whole
// render comes from EXT_disjoint_timer_query_webgl2 (compare spectrum cases with Gerstner cases for the ocean graph's cost).
//
// The page is served from http://localhost (WebGPU needs a secure context). One WebGPU
// renderer draws every case in turn, so spectrum, tier and ocean-kind changes go through
// the same paths a running scene uses. Each case renders --frames frames on a presentation
// clock that advances 1/30 s per frame, so the persistent foam has built up before the
// frame is kept. Cases: wind 9 and 15 m/s spectra, and Gerstner oceans of similar height,
// each from camera heights of 30, 120 and 300 m. Prints per-case whole-frame GPU time (from the
// renderer's timestamp-query attribute) when the device offers timestamp-query. Exit status 1 if a frame is not "submitted" or the spectral ocean
// did not draw.
import fs from 'node:fs';
import path from 'node:path';
import { createRequire } from 'node:module';
import { fileURLToPath } from 'node:url';

const root = process.env.SCENE3D_JS_DIR || path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..', 'client', 'js');
const toolsDir = process.env.SCENE3D_TOOLS_DIR;
if (!toolsDir) throw new Error('set SCENE3D_TOOLS_DIR to a directory that contains node_modules/playwright-core');
const toolsRequire = createRequire(import.meta.url);
let chromium;
for (const name of ['playwright', 'playwright-core']) {
  try { ({ chromium } = toolsRequire(path.join(toolsDir, 'node_modules', name))); break; } catch (error) { if (name === 'playwright-core') throw error; }
}
const argv = process.argv.slice(2);
const arg = (name, fallback) => { const i = argv.indexOf(name); return i >= 0 ? argv[i + 1] : fallback; };
const outDir = arg('--out', '');
if (outDir) fs.mkdirSync(outDir, { recursive: true });
const [W, H] = arg('--size', '1280x720').split('x').map(Number);
const frames = Number(arg('--frames', '240'));
const only = arg('--only', '');
const backendName = arg('--backend', 'webgpu');
const tier = arg('--tier', 'high');
if (backendName !== 'webgpu' && backendName !== 'webgl') throw new Error('--backend must be webgpu or webgl');
const realProfile = process.env.SCENE3D_REAL_GPU_PROFILE;
if (!realProfile) throw new Error('set SCENE3D_REAL_GPU_PROFILE (and SCENE3D_BROWSER) to run on a real GPU');

const browser = await chromium.launchPersistentContext(realProfile, {
  executablePath: process.env.SCENE3D_BROWSER, headless: false, viewport: { width: W + 20, height: H + 120 },
  args: ['--enable-gpu', '--use-angle=d3d11', '--enable-unsafe-webgpu', '--ignore-gpu-blocklist', '--enable-dawn-features=allow_unsafe_apis'],
});
const page = browser.pages()[0] || await browser.newPage();
page.on('pageerror', error => console.error('[pageerror]', error.message));
page.on('console', message => { if (message.type() === 'warning' || message.type() === 'error') console.error('[console]', message.text().slice(0, 600)); });
await page.route('http://localhost/**', route => route.fulfill({ contentType: 'text/html',
  body: `<!doctype html><html><body style="margin:0"><canvas id="c" width="${W}" height="${H}"></canvas></body></html>` }));
await page.goto('http://localhost/');
for (const file of ['bootstrap.js', backendName === 'webgl' ? 'bootstrap-feature-scene3d-webgl.js' : 'bootstrap-feature-scene3d-webgpu.js', 'bootstrap-feature-scene3d-ocean-fft.js']) {
  await page.addScriptTag({ path: path.join(root, file) });
}
await page.waitForFunction(() => window.__gosx_scene3d_api && window.__gosx_scene3d_ocean_fft_api, null, { timeout: 15000 });
const webgl = backendName === 'webgl';

const cases = [];
for (const height of [30, 120, 300]) {
  for (const wind of [9, 15]) {
    cases.push({ name: `spectrum-wind${wind}-h${height}`, height, spectrum: { windSpeed: wind, fetch: 300, peakEnhancement: 3.3, spread: 8, swell: wind === 9 ? 0.25 : 0, swellDirection: -35, seed: 7 } });
  }
}
// Glassy and onset winds, for the foam check.
for (const wind of [3, 10]) cases.push({ name: `spectrum-wind${wind}-h30`, height: 30, spectrum: { windSpeed: wind, fetch: 300, peakEnhancement: 3.3, spread: 8, swell: 0, seed: 7 } });
for (const height of [30, 120, 300]) {
  for (const [wind, waveHeight, waveLength] of [[9, 1.0, 38], [15, 2.2, 70]]) {
    cases.push({ name: `gerstner-wind${wind}-h${height}`, height, gerstner: { waveHeight, waveLength } });
  }
}
const selected = cases.filter(c => !only || c.name.includes(only));

const { results, images, adapter } = await page.evaluate(async ([cases, W, H, frames, webgl, tier, foam]) => {
  const api = window.__gosx_scene3d_api;
  const canvas = document.getElementById('c');
  let adapter = null, renderer, gl = null, timer = null;
  if (!webgl) {
    const a = await navigator.gpu.requestAdapter();
    const info = a && (a.info || (a.requestAdapterInfo && await a.requestAdapterInfo()));
    adapter = info ? { vendor: info.vendor, architecture: info.architecture, device: info.device, description: info.description } : null;
    if (!(await window.__gosx_scene3d_webgpu_probe_ready())) throw new Error('WebGPU probe failed: ' + JSON.stringify(window.__gosx_scene3d_webgpu_probe().error));
    const backend = api.sceneBackendRegistry.select({ webgpu: true, preferWebGPU: true, webgl: false, webgl2: false, canvas: false, canvas2d: false });
    renderer = backend.create(canvas, { background: '#101820' }, { tier: 'full' });
    if (!renderer || renderer.kind !== 'webgpu') throw new Error('no WebGPU renderer (got ' + (renderer && renderer.kind) + ')');
  } else {
    // The renderer takes the canvas's WebGL2 context; the page reaches the same context to time the frame.
    const backend = api.sceneBackendRegistry.select({ webgpu: false, webgl: true, webgl2: true, canvas: false, canvas2d: false });
    renderer = backend.create(canvas, { background: '#101820' }, { tier: 'full' });
    if (!renderer || !/webgl/.test(renderer.kind)) throw new Error('no WebGL2 renderer (got ' + (renderer && renderer.kind) + ')');
    gl = canvas.getContext('webgl2');
    const dbg = gl.getExtension('WEBGL_debug_renderer_info');
    adapter = { vendor: gl.getParameter(gl.VENDOR), renderer: dbg ? gl.getParameter(dbg.UNMASKED_RENDERER_WEBGL) : gl.getParameter(gl.RENDERER),
      colorBufferFloat: Boolean(gl.getExtension('EXT_color_buffer_float')) };
    timer = gl.getExtension('EXT_disjoint_timer_query_webgl2');
  }
  // WebGL2 timing: queue TIME_ELAPSED queries around the chosen span and read finished ones on later frames.
  const queries = [], samples = [];
  let timing = false;
  const timed = (fn) => {
    if (!timer || timing) return fn(); // GL allows one TIME_ELAPSED query at a time
    const q = gl.createQuery();
    gl.beginQuery(timer.TIME_ELAPSED_EXT, q);
    timing = true;
    try { return fn(); } finally { gl.endQuery(timer.TIME_ELAPSED_EXT); timing = false; queries.push(q); }
  };
  const drain = () => {
    if (!timer) return;
    while (queries.length && gl.getQueryParameter(queries[0], gl.QUERY_RESULT_AVAILABLE)) {
      const q = queries.shift(), bad = gl.getParameter(timer.GPU_DISJOINT_EXT);
      const ns = gl.getQueryParameter(q, gl.QUERY_RESULT); gl.deleteQuery(q);
      if (!bad) samples.push(ns / 1e6);
    }
  };
  const copy = document.createElement('canvas');
  copy.width = W; copy.height = H;
  const copyContext = copy.getContext('2d', { willReadFrequently: true });
  const viewport = { cssWidth: W, cssHeight: H, width: W, height: H };
  const frame = () => new Promise(resolve => requestAnimationFrame(resolve));
  const out = [], images = [];
  let time = 100;
  for (const c of cases) {
    const camera = { x: 0, y: c.height, z: 0, rotationX: -0.045 - 0.25 * Math.min(1, c.height / 300), rotationY: 0.6, fov: 62, near: 0.5, far: 20000 };
    const ocean = Object.assign({ level: 0, extent: 8000, choppiness: 0.7, windDirection: 20, foam }, c.gerstner || {});
    if (c.spectrum) {
      ocean.spectrum = c.spectrum;
      // What Go's normalization (scene/ocean_graph.go) would put in environment.ocean.graphParams for this scene; the graph's own defaults
      // are for a different sea. Foam follows the wind: none below 5 m/s, full whitecaps from 9 m/s.
      const t = Math.min(1, Math.max(0, (c.spectrum.windSpeed - 5) / 4)), base = 0.64 * t * t * (3 - 2 * t);
      ocean.graphParams = { windSpeed: c.spectrum.windSpeed, fetch: c.spectrum.fetch, gamma: c.spectrum.peakEnhancement, spread: c.spectrum.spread, swell: c.spectrum.swell,
        windDirDeg: ocean.windDirection, swellDirDeg: c.spectrum.swellDirection, seed: c.spectrum.seed, loopPeriod: 1000, lambda: ocean.choppiness, timeScale: 1, driftX: 0, driftZ: 0,
        foamTau: 6, foamGain: 0.9, foamThreshold: base === 0 ? 0 : 1 - ocean.choppiness * (1 - base) };
      ocean.graphTween = 0;
    }
    let status = '', first = null;
    const gpu = [];
    const t0 = performance.now();
    for (let i = 0; i < frames; i++) {
      const bundle = api.createSceneRenderBundle(W, H, '#101820', camera, [], [], [], [], [], {}, 0.4, [], [], [], [], [], 0, false, {});
      bundle.environment = { sky: { mode: 'physical', sunDirection: { x: -0.35, y: 0.7, z: 0.62 }, turbidity: 3 }, ocean };
      bundle.lights = [];
      bundle.postEffects = [{ kind: 'toneMapping', mode: 'aces', exposure: 0.5 }];
      time += 1 / 30;
      // Drift the camera a little so the foam window moves in whole texels, as under a ship.
      camera.x += 0.15; camera.z += 0.1;
      status = webgl ? timed(() => renderer.render(bundle, viewport, { sceneTime: time, oceanTier: tier })) : renderer.render(bundle, viewport, { sceneTime: time, oceanTier: tier });
      drain();
      if (first === null && status === 'submitted' && String(canvas.parentNode.getAttribute('data-gosx-scene3d-ocean')).endsWith(c.spectrum ? '+spectrum' : 'surface')) first = i;
      if (webgl && i === Math.floor(frames / 2)) samples.length = 0;
      if (!webgl && i >= frames / 2) { const ms = parseFloat(canvas.parentNode.getAttribute('data-gosx-scene3d-webgpu-gpu-ms')); if (Number.isFinite(ms)) gpu.push(ms); }
      // The canvas is copied in the same task as the last render, while its texture is still current.
      if (i === frames - 1) { copyContext.clearRect(0, 0, W, H); copyContext.drawImage(canvas, 0, 0); break; }
      await frame();
    }
    const pixels = copyContext.getImageData(0, 0, W, H).data;
    let sum = 0, bright = 0;
    for (let i = 0; i < pixels.length; i += 4) { const l = pixels[i] + pixels[i + 1] + pixels[i + 2]; sum += l; if (l > 600) bright++; }
    const mount = canvas.parentNode;
    if (webgl) { for (let k = 0; k < 6 && queries.length; k++) { await frame(); drain(); } gpu.push(...samples); queries.length = 0; }
    out.push({ name: c.name, status, firstSubmitted: first, ocean: mount.getAttribute('data-gosx-scene3d-ocean'),
      failed: mount.getAttribute('data-gosx-scene3d-webgpu-pipeline-failed'), fallback: mount.getAttribute('data-gosx-scene3d-ocean-fft-fallback'), meanLuma: sum / (pixels.length / 4) / 3, brightPixels: bright,
      wallMsPerFrame: (performance.now() - t0) / frames, gpuSamples: gpu.length,
      gpuMsP50: gpu.length ? gpu.slice().sort((a, b) => a - b)[Math.floor(gpu.length * 0.5)] : null,
      gpuMsMin: gpu.length ? Math.min(...gpu) : null, gpuMsMax: gpu.length ? Math.max(...gpu) : null, gpuMsP95: gpu.length ? gpu.slice().sort((a, b) => a - b)[Math.min(gpu.length - 1, Math.floor(gpu.length * 0.95))] : null });
    images.push({ name: c.name, url: copy.toDataURL('image/png') });
  }
  return { results: out, images, adapter };
}, [selected, W, H, frames, webgl, tier, Number(arg('--foam', '0.9'))]);
await browser.close();
if (adapter) console.log('adapter', JSON.stringify(adapter));
if (outDir) for (const image of images) fs.writeFileSync(path.join(outDir, image.name + '.png'), Buffer.from(image.url.split(',')[1], 'base64'));
let failed = false;
for (const r of results) {
  const spectral = r.name.startsWith('spectrum');
  const ok = r.status === 'submitted' && !r.failed && (spectral ? /\+spectrum$/.test(r.ocean) : r.ocean === 'surface');
  if (!ok) failed = true;
  console.log(`${ok ? 'ok  ' : 'FAIL'} ${r.name}: status ${r.status}, ocean ${r.ocean}, first spectral frame ${r.firstSubmitted}, mean luma ${r.meanLuma.toFixed(1)}, ${r.brightPixels} bright pixels, ${r.wallMsPerFrame.toFixed(2)} ms/frame wall, GPU frame ${r.gpuSamples ? 'p50 ' + r.gpuMsP50.toFixed(3) + ' / p95 ' + r.gpuMsP95.toFixed(3) + ' / min ' + r.gpuMsMin.toFixed(3) + ' / max ' + r.gpuMsMax.toFixed(3) + ' ms (' + r.gpuSamples + ' samples)' : 'n/a'}${r.failed ? ', pipeline failures ' + r.failed : ''}${r.fallback ? ', fft fallback ' + r.fallback : ''}`);
}
process.exit(failed ? 1 : 0);
