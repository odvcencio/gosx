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
// Ship wakes: a hull of 125 x 21 m crossing the water at 8, 15 and 22 knots (bow toward -Z), seen from above, from astern and from a three-quarter view at the stern rail, and the same
// passage with the wake off for the cost comparison. The page publishes the hull's pose each frame as a vessel would.
for (const wake of [true, false]) {
  for (const knots of [8, 15, 22]) {
    for (const view of ['above', 'astern', 'quarter', 'trail']) {
      if (!wake && (view === 'above' || view === 'trail')) continue;
      cases.push({ name: `wake-${wake ? 'on' : 'off'}-${knots}kn-${view}`, height: 0, view, knots, wakeOn: wake, spectrum: { windSpeed: Number(arg('--wake-wind', '7')), fetch: 100, peakEnhancement: 3.3, spread: 8, swell: 0, seed: 7 } });
    }
  }
}
const wakeRun = argv.includes('--wake');
const selected = cases.filter(c => (wakeRun ? c.name.startsWith('wake-') : !c.name.startsWith('wake-')) && (!only || c.name.includes(only)));

const { results, images, adapter } = await page.evaluate(async ([cases, W, H, frames, webgl, tier, foam, wakeDebug, profiling, throttle, throttleMatch, seqCount, seqStep, staticCamera]) => {
  const api = window.__gosx_scene3d_api;
  const diag = window.__gosx_elio_diagnostics = {};
  // --profile: the page times every graph pass with its own timer query, so the renderer must not see the timer extension (one query at a time).
  let realTimer = null;
  if (profiling) {
    const getContext = HTMLCanvasElement.prototype.getContext;
    HTMLCanvasElement.prototype.getContext = function (...a) {
      const c = getContext.apply(this, a);
      if (c && c.getExtension && !c.__hidden) { c.__hidden = true; const get = c.getExtension.bind(c); realTimer = get('EXT_disjoint_timer_query_webgl2'); c.getExtension = (n) => n === 'EXT_disjoint_timer_query_webgl2' ? null : get(n); }
      return c;
    };
  }
  if (throttle > 1) diag.throttle = [{ match: throttleMatch, every: throttle }];
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
    timer = profiling ? null : gl.getExtension('EXT_disjoint_timer_query_webgl2');
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
  // Profiling: one query per executed pass; a frame's passes are summed by group (pass name without repeat indices, first two segments).
  const frameQueries = [], profiled = [];
  if (profiling && realTimer) {
    const T = realTimer.TIME_ELAPSED_EXT;
    diag.profile = (name, fn) => {
      const q = gl.createQuery();
      gl.beginQuery(T, q);
      try { fn(); } finally { gl.endQuery(T); (frameQueries[frameQueries.length - 1] ||= []).push({ key: name.replace(/\[\d+\]/g, '').split('.').slice(0, /^foam\.copy/.test(name) ? 3 : 2).join('.'), q }); }
    };
  }
  const drainProfile = (all) => {
    while (frameQueries.length && (all || (frameQueries.length > 1 && frameQueries[0].every((e) => gl.getQueryParameter(e.q, gl.QUERY_RESULT_AVAILABLE))))) {
      const list = frameQueries.shift(), sums = {};
      for (const e of list) { sums[e.key] = (sums[e.key] || 0) + gl.getQueryParameter(e.q, gl.QUERY_RESULT) / 1e6; gl.deleteQuery(e.q); }
      profiled.push(sums);
    }
  };
  // Diagnostics: capture the graph runner so the wake texture can be read back (WebGL2 only) once a wake case has run.
  const runners = [];
  const createRunner = window.__gosx_elio_graph.create;
  window.__gosx_elio_graph.create = function () { const r = createRunner.apply(this, arguments); runners.push(r); return r; };
  const wakeImage = () => {
    const r = runners[runners.length - 1], e = r && r.export && r.export('wake');
    if (!gl || !e || !e.tex) return null;
    const w = e.width, h = e.height, px = new Float32Array(w * h * 4), fb = gl.createFramebuffer(), prev = gl.getParameter(gl.READ_FRAMEBUFFER_BINDING);
    gl.bindFramebuffer(gl.READ_FRAMEBUFFER, fb); gl.framebufferTexture2D(gl.READ_FRAMEBUFFER, gl.COLOR_ATTACHMENT0, gl.TEXTURE_2D, e.tex, 0);
    gl.readPixels(0, 0, w, h, gl.RGBA, gl.FLOAT, px);
    gl.bindFramebuffer(gl.READ_FRAMEBUFFER, prev); gl.deleteFramebuffer(fb);
    const stats = { h: [0, 0], gx: 0, gz: 0, turb: 0 }, c = document.createElement('canvas'); c.width = w; c.height = h;
    const ctx = c.getContext('2d'), img = ctx.createImageData(w, h);
    // The window is toroidal: the hull sits at storage cell (originMod + hullU / cell) mod width. Far field = more than 24 cells from it. The image
    // stretches height by the far-field maximum so the trail shows (the hull's own depression saturates).
    const wrapCell = (v) => ((Math.floor(v) % w) + w) % w;
    const hx = wrapCell(r.value('wakeOriginX') + r.value('wakeHullU') / r.value('wakeCell')), hz = wrapCell(r.value('wakeOriginZ') + r.value('wakeHullV') / r.value('wakeCell'));
    const near = (x, z) => Math.min(Math.abs(x - hx), w - Math.abs(x - hx)) <= 24 && Math.min(Math.abs(z - hz), h - Math.abs(z - hz)) <= 24;
    let hmax = 1e-9, far = 0, hull = 0;
    for (let z = 0; z < h; z++) for (let x = 0; x < w; x++) { const v = Math.abs(px[(z * w + x) * 4]); if (near(x, z)) hull = Math.max(hull, v); else { far = Math.max(far, v); hmax = Math.max(hmax, v); } }
    stats.far = far; stats.hull = hull; stats.hullCell = [hx, hz];
    for (let i = 0; i < w * h; i++) {
      const hh = px[i * 4], gx = px[i * 4 + 1], gz = px[i * 4 + 2], t = px[i * 4 + 3];
      stats.h[0] = Math.min(stats.h[0], hh); stats.h[1] = Math.max(stats.h[1], hh); stats.gx = Math.max(stats.gx, Math.abs(gx)); stats.gz = Math.max(stats.gz, Math.abs(gz)); stats.turb = Math.max(stats.turb, t);
      const v = Math.min(1, Math.max(0, 0.5 + 0.5 * hh / hmax));
      img.data[i * 4] = 255 * v; img.data[i * 4 + 1] = 255 * v; img.data[i * 4 + 2] = 255 * Math.min(1, t); img.data[i * 4 + 3] = 255;
    }
    ctx.putImageData(img, 0, 0);
    for (const n of ['hullX', 'hullZ', 'hullHeading', 'hullSpeed', 'wakeEnabled', 'wakeOn', 'wakeDt', 'wakeSrcLength', 'wakeSrcBeam', 'wakeHullU', 'wakeHullV', 'wakeGEff', 'wakeReset', 'wakeFoamGainEff']) (stats.params ||= {})[n] = r.value(n);
    return { stats, url: c.toDataURL('image/png') };
  };
  const copy = document.createElement('canvas');
  copy.width = W; copy.height = H;
  const copyContext = copy.getContext('2d', { willReadFrequently: true });
  const viewport = { cssWidth: W, cssHeight: H, width: W, height: H };
  const frame = () => new Promise(resolve => requestAnimationFrame(resolve));
  const out = [], images = [];
  let time = 100;
  for (const c of cases) {
    const hull = c.knots ? { x: 0, z: 0, speed: c.knots * 0.514444 } : null;
    const views = { above: [0, 170, 30, -1.35, 0], astern: [0, 26, 95, -0.2, 0], quarter: [9, 13, 58, -0.2, 2.62], trail: [0, 420, 190, -1.45, 0] }; // x, y, z, rotationX, rotationY
    const camera = hull ? { x: views[c.view][0], y: views[c.view][1], z: views[c.view][2], rotationX: views[c.view][3], rotationY: views[c.view][4], fov: 62, near: 0.5, far: 20000 } : { x: 0, y: c.height, z: 0, rotationX: -0.045 - 0.25 * Math.min(1, c.height / 300), rotationY: 0.6, fov: 62, near: 0.5, far: 20000 };
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
      if (hull) { Object.assign(ocean.graphParams, { wakeEnabled: c.wakeOn ? 1 : 0, hullLength: 125, hullBeam: 21 }); ocean.graphNodes = { hull: 'ship' }; ocean.wake = { nodeId: 'ship', enabled: c.wakeOn, length: 125, beam: 21, debug: wakeDebug }; }
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
      if (hull) {
        // The hull advances along -Z; the camera keeps its offset from it.
        hull.z -= hull.speed / 30;
        if (!staticCamera) camera.z -= hull.speed / 30;
        else if (i === frames - 2 - Math.max(seqCount - 1, 0) * seqStep) {
          // --static-camera: from the first frame of the sequence the camera stays put in the world, beside the track, 40 m behind the hull, so the
          // trail passes by a fixed view and any change in the lace is the foam's own, not the camera's.
          Object.assign(camera, { x: 16, y: 12, z: hull.z + 40, rotationX: -0.45, rotationY: 1.5708 });
        }
        (window.__gosx_scene3d_node_poses ||= {}).ship = { x: hull.x, z: hull.z, heading: Math.PI, speed: hull.speed };
      } else {
        // Drift the camera a little so the foam window moves in whole texels, as under a ship.
        camera.x += 0.15; camera.z += 0.1;
      }
      if (profiling) frameQueries.push([]);
      status = webgl ? timed(() => renderer.render(bundle, viewport, { sceneTime: time, oceanTier: tier })) : renderer.render(bundle, viewport, { sceneTime: time, oceanTier: tier });
      drain(); if (profiling) drainProfile(false);
      if (first === null && status === 'submitted' && String(canvas.parentNode.getAttribute('data-gosx-scene3d-ocean')).endsWith(c.spectrum ? '+spectrum' : 'surface')) first = i;
      if (webgl && i === Math.floor(frames / 2)) { samples.length = 0; profiled.length = 0; }
      if (!webgl && i >= frames / 2) { const ms = parseFloat(canvas.parentNode.getAttribute('data-gosx-scene3d-webgpu-gpu-ms')); if (Number.isFinite(ms)) gpu.push(ms); }
      // The canvas is copied in the same task as the last render, while its texture is still current.
      // --sequence N --sequence-step K: also keep the N frames that end the run, K frames apart (the last one is the final frame), for motion checks.
      if (seqCount > 1 && (frames - 1 - i) % seqStep === 0 && (frames - 1 - i) / seqStep < seqCount && i !== frames - 1) {
        copyContext.clearRect(0, 0, W, H); copyContext.drawImage(canvas, 0, 0);
        images.push({ name: c.name + '-seq' + (seqCount - 1 - (frames - 1 - i) / seqStep), url: copy.toDataURL('image/png') });
      }
      if (i === frames - 1) { copyContext.clearRect(0, 0, W, H); copyContext.drawImage(canvas, 0, 0); break; }
      await frame();
    }
    const pixels = copyContext.getImageData(0, 0, W, H).data;
    let sum = 0, bright = 0;
    for (let i = 0; i < pixels.length; i += 4) { const l = pixels[i] + pixels[i + 1] + pixels[i + 2]; sum += l; if (l > 600) bright++; }
    const mount = canvas.parentNode;
    if (profiling) { for (let k = 0; k < 6; k++) await frame(); drainProfile(true); }
    if (webgl) { for (let k = 0; k < 6 && queries.length; k++) { await frame(); drain(); } gpu.push(...samples); queries.length = 0; }
    const pass = (() => {
      if (!profiling || !profiled.length) return null;
      const keys = [...new Set(profiled.flatMap((f) => Object.keys(f)))], med = (v) => v.slice().sort((a, b) => a - b)[Math.floor(v.length / 2)];
      const table = {}; for (const k of keys) table[k] = med(profiled.map((f) => f[k] || 0));
      return { frames: profiled.length, table, total: med(profiled.map((f) => Object.values(f).reduce((a, b) => a + b, 0))) };
    })();
    out.push({ name: c.name, passes: pass, status, firstSubmitted: first, ocean: mount.getAttribute('data-gosx-scene3d-ocean'),
      failed: mount.getAttribute('data-gosx-scene3d-webgpu-pipeline-failed'), fallback: mount.getAttribute('data-gosx-scene3d-ocean-fft-fallback'), meanLuma: sum / (pixels.length / 4) / 3, brightPixels: bright,
      wallMsPerFrame: (performance.now() - t0) / frames, gpuSamples: gpu.length, graphMs: (() => { const t = !webgl && window.__gosx_scene3d_ocean_fft_api && window.__gosx_scene3d_ocean_fft_api.timing(); if (!t || !t.samples.length) return null; const v = t.samples.slice().sort((a, b) => a - b); return { p50: v[Math.floor(v.length * 0.5)], p95: v[Math.min(v.length - 1, Math.floor(v.length * 0.95))], samples: v.length }; })(),
      gpuMsP50: gpu.length ? gpu.slice().sort((a, b) => a - b)[Math.floor(gpu.length * 0.5)] : null,
      gpuMsMin: gpu.length ? Math.min(...gpu) : null, gpuMsMax: gpu.length ? Math.max(...gpu) : null, gpuMsP95: gpu.length ? gpu.slice().sort((a, b) => a - b)[Math.min(gpu.length - 1, Math.floor(gpu.length * 0.95))] : null });
    images.push({ name: c.name, url: copy.toDataURL('image/png') });
    if (c.knots && c.wakeOn) { const wi = wakeImage(); if (wi) { images.push({ name: c.name + '-texture', url: wi.url }); out[out.length - 1].wakeStats = wi.stats; } }
  }
  return { results: out, images, adapter };
}, [selected, W, H, frames, webgl, tier, Number(arg('--foam', '0.9')), argv.includes('--wake-debug'), argv.includes('--profile'), Number(arg('--throttle', '1')), arg('--throttle-match', '^wake\\.'), Number(arg('--sequence', '0')), Number(arg('--sequence-step', '15')), argv.includes('--static-camera')]);
await browser.close();
if (adapter) console.log('adapter', JSON.stringify(adapter));
if (outDir) for (const image of images) fs.writeFileSync(path.join(outDir, image.name + '.png'), Buffer.from(image.url.split(',')[1], 'base64'));
let failed = false;
for (const r of results) {
  if (r.passes) console.log(`passes ${r.name}: ${r.passes.frames} frames, graph total p50 ${r.passes.total.toFixed(3)} ms; ` + Object.entries(r.passes.table).map(([k, v]) => `${k} ${v.toFixed(3)}`).join(', '));
  const spectral = r.name.startsWith('spectrum') || r.name.startsWith('wake-');
  const ok = r.status === 'submitted' && !r.failed && (spectral ? /\+spectrum$/.test(r.ocean) : r.ocean === 'surface');
  if (!ok) failed = true;
  console.log(`${ok ? 'ok  ' : 'FAIL'} ${r.name}: status ${r.status}, ocean ${r.ocean}, first spectral frame ${r.firstSubmitted}, mean luma ${r.meanLuma.toFixed(1)}, ${r.brightPixels} bright pixels, ${r.wallMsPerFrame.toFixed(2)} ms/frame wall, GPU frame ${r.gpuSamples ? 'p50 ' + r.gpuMsP50.toFixed(3) + ' / p95 ' + r.gpuMsP95.toFixed(3) + ' / min ' + r.gpuMsMin.toFixed(3) + ' / max ' + r.gpuMsMax.toFixed(3) + ' ms (' + r.gpuSamples + ' samples)' : 'n/a'}${r.wakeStats ? ', wake texture h ' + r.wakeStats.h[0].toFixed(3) + '..' + r.wakeStats.h[1].toFixed(3) + ' m (hull neighbourhood ' + r.wakeStats.hull.toFixed(3) + ', far field ' + r.wakeStats.far.toFixed(4) + '), max slope ' + Math.max(r.wakeStats.gx, r.wakeStats.gz).toFixed(3) + ', max turbulence ' + r.wakeStats.turb.toFixed(3) + ' params ' + JSON.stringify(r.wakeStats.params) : ''}${r.graphMs ? ', graph compute p50 ' + r.graphMs.p50.toFixed(3) + ' / p95 ' + r.graphMs.p95.toFixed(3) + ' ms (' + r.graphMs.samples + ' samples)' : ''}${r.failed ? ', pipeline failures ' + r.failed : ''}${r.fallback ? ', fft fallback ' + r.fallback : ''}`);
}
process.exit(failed ? 1 : 0);
