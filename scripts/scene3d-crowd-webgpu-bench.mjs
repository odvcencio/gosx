#!/usr/bin/env node
// Crowd batches on a moving ship, WebGPU against WebGL, with and without GPU
// culling. A stand-in for the Bon Voyage! cruise ship: six decks inside a hull
// whose lower four decks are closed by a front wall, a crowd of N agents spread
// over the decks, the whole ship heaving and rolling every frame.
//
//   SCENE3D_TOOLS_DIR=<dir containing node_modules/playwright> \
//     node scripts/scene3d-crowd-webgpu-bench.mjs [agents=5000,7800] [frames=120] [present] [webgl]
//
//   agents=A,B   crowd sizes (default 5000,7800)
//   frames=N     measured frames per run (default 120, after 30 warm-up frames)
//   present      draw to a real WebGPU canvas instead of an offscreen texture.
//                Use this on a machine with a real GPU and a headed browser:
//                SCENE3D_BROWSER=/path/to/chrome node ... present
//   webgl        also run the same scene on the WebGL renderer
//   dump=DIR     write the last frame of each run as a PPM file into DIR
//
// Each run reports, per frame, the main-thread time in three parts: the planner
// (createSceneRenderBundle, which walks every agent), the renderer's render()
// call (JS encode and submit; the GPU works asynchronously), and their sum.
// It also reports the agents submitted, the agents that survive the frustum
// test, the agents the GPU draws once Hi-Z occlusion has removed those hidden
// by decks and walls (indirect-args readback), and the time until the GPU
// finishes a batch of frames. On a software rasterizer (the default here, with
// no GPU) the completion time is CPU rasterization, not GPU time; when the
// device has timestamp-query the script also reports the renderer's GPU
// timestamps.
//
// Per frame the crowd changes the way a networked game would: every sixth
// frame a partial motion frame rewrites 5% of the agents' motion records.
import fs from 'node:fs';
import path from 'node:path';
import { createRequire } from 'node:module';
import { fileURLToPath } from 'node:url';

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..', 'client', 'js');
const toolsDir = process.env.SCENE3D_TOOLS_DIR;
if (!toolsDir) throw new Error('set SCENE3D_TOOLS_DIR to a directory that contains node_modules/playwright');
const { chromium } = createRequire(import.meta.url)(path.join(toolsDir, 'node_modules/playwright'));
const argv = process.argv.slice(2);
const option = name => { const hit = argv.find(a => a.startsWith(name + '=')); return hit ? hit.slice(name.length + 1) : ''; };
const sizes = (option('agents') || '5000,7800').split(',').map(Number);
const frames = Number(option('frames') || 120);
const present = argv.includes('present');
const withWebGL = argv.includes('webgl');
const dumpDir = option('dump');

async function runBackend(backendKind, runs) {
  const browser = await chromium.launch({
    executablePath: process.env.SCENE3D_BROWSER || undefined,
    headless: !present,
    // WebGPU needs only --enable-unsafe-webgpu; the ANGLE flags that WebGL
    // needs make Dawn lose its instance on SwiftShader.
    args: backendKind === 'gpu' ? ['--enable-unsafe-webgpu']
      : ['--use-angle=swiftshader', '--enable-unsafe-swiftshader', '--ignore-gpu-blocklist', '--use-gl=angle'],
  });
  const page = await browser.newPage({ viewport: { width: 700, height: 400 } });
  page.on('pageerror', error => console.error('[pageerror]', error.message));
  page.on('console', message => { if (message.type() === 'warning' || message.type() === 'error') console.error('[console]', message.text().slice(0, 300)); });
  await page.route('http://localhost/**', route => route.fulfill({ contentType: 'text/html',
    body: '<!doctype html><html><body style="margin:0"><canvas id="c" width="640" height="360"></canvas></body></html>' }));
  await page.goto('http://localhost/');
  await page.addScriptTag({ path: path.join(root, 'bootstrap.js') });
  await page.addScriptTag({ path: path.join(root, 'bootstrap-feature-scene3d-animation.js') });
  await page.addScriptTag({ path: path.join(root, 'bootstrap-feature-scene3d-webgpu.js') });
  await page.waitForFunction(() => window.__gosx_scene3d_api && window.__gosx_scene3d_animation_api, null, { timeout: 10000 });
  const out = [];
  for (const run of runs) {
    // A fresh canvas per run: a canvas that has a WebGPU context cannot get another.
    await page.evaluate(() => { const old = document.getElementById('c'); const next = old.cloneNode(false); old.replaceWith(next); });
    out.push(await page.evaluate(runScene, [backendKind, run, frames, present]));
    if (dumpDir && out[out.length - 1].last) {
      const r = out[out.length - 1];
      fs.mkdirSync(dumpDir, { recursive: true });
      const rgb = Buffer.alloc(640 * 360 * 3);
      const px = Buffer.from(r.last, 'base64');
      for (let i = 0, j = 0; i < px.length; i += 4, j += 3) { rgb[j] = px[i]; rgb[j + 1] = px[i + 1]; rgb[j + 2] = px[i + 2]; }
      fs.writeFileSync(path.join(dumpDir, `${backendKind}-${r.label.replace(/[^a-z0-9]+/gi, '-')}.ppm`), Buffer.concat([Buffer.from('P6\n640 360\n255\n'), rgb]));
    }
  }
  await browser.close();
  return out;
}

// Runs in the page.
async function runScene([backendKind, run, frames, present]) {
  const api = window.__gosx_scene3d_api;
  const anim = window.__gosx_scene3d_animation_api;
  const canvas = document.getElementById('c');
  const W = 640, H = 360;
  let target, offscreen = null;
  if (backendKind === 'gpu') {
    if (!(await window.__gosx_scene3d_webgpu_probe_ready())) return { label: run.label, error: 'WebGPU probe failed: ' + JSON.stringify(window.__gosx_scene3d_webgpu_probe().error) };
    const device = window.__gosx_scene3d_webgpu_probe().device;
    if (!present) {
      // Headless Chromium on SwiftShader cannot present a WebGPU canvas (the GPU
      // process loses its Instance). Hand the renderer an offscreen texture.
      const state = { format: navigator.gpu.getPreferredCanvasFormat(), texture: null };
      const fakeContext = {
        canvas,
        configure(config) {
          state.format = config.format || state.format;
          if (state.texture) state.texture.destroy();
          state.texture = device.createTexture({ size: [canvas.width, canvas.height, 1], format: state.format,
            usage: GPUTextureUsage.RENDER_ATTACHMENT | GPUTextureUsage.COPY_SRC | GPUTextureUsage.TEXTURE_BINDING });
        },
        getCurrentTexture() { return state.texture; },
        unconfigure() {},
      };
      const realGetContext = canvas.getContext.bind(canvas);
      canvas.getContext = kind => (kind === 'webgpu' ? fakeContext : realGetContext(kind));
      offscreen = { device, state };
    }
    const backend = api.sceneBackendRegistry.select({ webgpu: true, preferWebGPU: true, webgl: false });
    target = backend.create(canvas, { background: '#101820' }, { tier: 'full' });
    if (!target || target.kind !== 'webgpu') return { label: run.label, error: 'no WebGPU renderer' };
  } else {
    const backend = api.sceneBackendRegistry.select({ webgl: true, webgl2: true, webgpu: false });
    target = backend.create(canvas, { background: '#101820' }, { tier: 'full' });
  }

  // Agent: a tapered prism skinned to one joint, 12 vertices.
  const pos = [], nor = [], uv = [], tan = [], joints = [], weights = [];
  const tri = (a, b, c, n) => { for (const p of [a, b, c]) { pos.push(...p); nor.push(...n); uv.push(p[0], p[1]); tan.push(1, 0, 0, 1); joints.push(0, 0, 0, 0); weights.push(1, 0, 0, 0); } };
  const s = 0.2;
  tri([-s, 0, s], [s, 0, s], [0, 1.7, 0], [0, 0.3, 1]);
  tri([s, 0, s], [s, 0, -s], [0, 1.7, 0], [1, 0.3, 0]);
  tri([s, 0, -s], [-s, 0, -s], [0, 1.7, 0], [0, 0.3, -1]);
  tri([-s, 0, -s], [-s, 0, s], [0, 1.7, 0], [-1, 0.3, 0]);
  const vertices = { count: pos.length / 3, immutable: true, revision: 0, positions: new Float32Array(pos), normals: new Float32Array(nor),
    uvs: new Float32Array(uv), tangents: new Float32Array(tan), joints: new Float32Array(joints), weights: new Float32Array(weights) };
  const width = 4, height = 32, data = new Float32Array(width * height * 4);
  for (let row = 0; row < height; row++) {
    const o = row * width * 4;
    data[o] = data[o + 5] = data[o + 10] = data[o + 15] = 1;
    data[o + 13] = row * 0.01;
  }
  const atlas = { id: 'bench-atlas', width, height, data, clips: new Map([['Walk', { start: 1, segments: 30, duration: 1 }]]), missingClips: new Set() };
  const bounds = { minX: -.25, minY: 0, minZ: -.25, maxX: .25, maxY: 1.7, maxZ: .25 };
  const clipTable = anim.crowdMotionClipTable(atlas);
  const walkRow = anim.crowdMotionClipIndex(atlas, 'Walk');

  // Ship: six decks, 40 x 12 m, 3.2 m apart. The lower four decks sit behind a
  // front wall; the two upper decks are open to the camera in front.
  const mesh = (id, positions, normals, color, parentMatrix, index) => {
    const o = api.normalizeSceneObject({ id, kind: 'mesh', materialKind: 'standard', color, roughness: .8,
      vertices: { count: positions.length / 3, immutable: true, revision: 0, positions: new Float32Array(positions), normals: new Float32Array(normals),
        uvs: new Float32Array(positions.length / 3 * 2), tangents: new Float32Array(positions.length / 3 * 4).map((_v, k) => (k % 4 === 0 || k % 4 === 3 ? 1 : 0)) } }, index);
    Object.assign(o, { x: 0, y: 0, z: 0, rotationX: 0, rotationY: 0, rotationZ: 0, spinX: 0, spinY: 0, spinZ: 0, shiftX: 0, shiftY: 0, shiftZ: 0,
      static: false, castShadow: false, receiveShadow: false, pickable: false, wireframe: false, doubleSided: true, parentMatrix });
    o.vertices.immutable = true; o.vertices.revision = 0;
    return o;
  };
  const quad = (a, b, c, d) => [...a, ...b, ...c, ...a, ...c, ...d];
  const shipPose = new Float32Array(16);
  const decks = 6, deckGap = 3.2, halfX = 20, halfZ = 6;
  const parts = [];
  for (let d = 0; d < decks; d++) {
    const y = d * deckGap;
    parts.push(mesh(`ship/deck${d}`, quad([-halfX, y, halfZ], [halfX, y, halfZ], [halfX, y, -halfZ], [-halfX, y, -halfZ]), new Array(18).fill(0).map((_v, k) => (k % 3 === 1 ? 1 : 0)), '#7a6a58', shipPose, 10 + d));
  }
  const top = decks * deckGap;
  const closed = 4 * deckGap;
  const wall = (id, a, b, c, d, color, n, index) => mesh(id, quad(a, b, c, d), new Array(6).fill(n).flat(), color, shipPose, index);
  parts.push(wall('ship/front', [-halfX, 0, halfZ], [halfX, 0, halfZ], [halfX, closed, halfZ], [-halfX, closed, halfZ], '#3a4a5a', [0, 0, 1], 30));
  parts.push(wall('ship/back', [halfX, 0, -halfZ], [-halfX, 0, -halfZ], [-halfX, top, -halfZ], [halfX, top, -halfZ], '#3a4a5a', [0, 0, 1], 31));
  parts.push(wall('ship/left', [-halfX, 0, -halfZ], [-halfX, 0, halfZ], [-halfX, top, halfZ], [-halfX, top, -halfZ], '#3a4a5a', [1, 0, 0], 32));
  parts.push(wall('ship/right', [halfX, 0, halfZ], [halfX, 0, -halfZ], [halfX, top, -halfZ], [halfX, top, halfZ], '#3a4a5a', [-1, 0, 0], 33));
  const hull = parts[0];

  const agents = [];
  const perDeck = Math.ceil(run.agents / decks);
  const cols = Math.max(2, Math.ceil(Math.sqrt(perDeck * (2 * halfX) / (2 * halfZ))));
  const rowsPerDeck = Math.ceil(perDeck / cols);
  for (let i = 0; i < run.agents; i++) {
    const d = Math.min(decks - 1, Math.floor(i / perDeck)), k = i - d * perDeck;
    const x = (k % cols + .5) / cols * (2 * halfX - 2) - (halfX - 1), z = (Math.floor(k / cols) + .5) / rowsPerDeck * (2 * halfZ - 2) - (halfZ - 1);
    const agent = api.normalizeSceneObject({ id: `crowd/a${i}/0`, kind: 'mesh', vertices, materialKind: 'standard', color: '#d98c5f', roughness: .6 }, i);
    Object.assign(agent, { x: 0, y: 0, z: 0, rotationX: 0, rotationY: 0, rotationZ: 0, spinX: 0, spinY: 0, spinZ: 0, shiftX: 0, shiftY: 0, shiftZ: 0,
      static: false, skin: null, pickable: false, castShadow: false, receiveShadow: false, depthWrite: true,
      parentMatrix: new Float32Array([1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, x, d * deckGap + .02, z, 1]) });
    agent.vertices.immutable = true; agent.vertices.revision = 0;
    agent._crowdSkin = { atlas, bounds, rows: new Float32Array([0, 1, 0]), poseRows: null, batchID: 'crowd', rev: 0, group: null, parent: 'ship' };
    agent._crowdMotion = { atlas, clipTable, record: new Float32Array(anim.crowdMotionRecordFloats), dirty: true,
      localRadius: anim.crowdMotionLocalRadius(bounds), bounds: { minX: 0, minY: 0, minZ: 0, maxX: 0, maxY: 0, maxZ: 0 } };
    agents.push(agent);
  }
  let motionStep = 0;
  const writeMotion = (agent, i) => {
    const x = agent.parentMatrix[12], y = agent.parentMatrix[13], z = agent.parentMatrix[14];
    const dx = Math.sin(motionStep * .7 + i) * .4, dz = Math.cos(motionStep * .5 + i) * .3;
    anim.crowdMotionWriteRecord(agent._crowdMotion.record, { prevX: x, prevY: y, prevZ: z, prevRotationX: 0, prevRotationY: i * .01, prevRotationZ: 0,
      prevScaleX: 1, prevScaleY: 1, prevScaleZ: 1, tPrev: 0, nextX: x + dx, nextY: y, nextZ: z + dz, nextRotationX: 0,
      nextRotationY: i * .01 + .2, nextRotationZ: 0, nextScaleX: 1, nextScaleY: 1, nextScaleZ: 1, tNext: 1e6,
      animation: 'Walk', clipStartTime: 0, animationLoop: true, playbackRate: 1 }, walkRow);
    const b = agent._crowdMotion.bounds;
    b.minX = b.minY = b.minZ = Infinity; b.maxX = b.maxY = b.maxZ = -Infinity;
    anim.crowdMotionSweptBoundsInto(b, agent._crowdMotion.record, agent._crowdMotion.localRadius);
    agent._crowdMotion.dirty = true;
  };
  agents.forEach(writeMotion);
  if (typeof target.prepareCrowdAtlas === 'function') target.prepareCrowdAtlas(atlas);
  if (typeof target.prepareCrowdMotionShaders === 'function') target.prepareCrowdMotionShaders();

  const setShip = t => {
    const roll = Math.sin(t * .02) * .05, heave = Math.sin(t * .031) * .6, yaw = Math.sin(t * .011) * .08;
    const cr = Math.cos(roll), sr = Math.sin(roll), cy = Math.cos(yaw), sy = Math.sin(yaw);
    // yaw (Y) then roll (Z): R = Ry * Rz, column-major
    shipPose.set([cy * cr, sr, -sy * cr, 0, -cy * sr, cr, sy * sr, 0, sy, 0, cy, 0, 0, heave, 0, 1]);
  };
  const lights = [{ id: 'sun', kind: 'directional', directionX: .3, directionY: -1, directionZ: -.4, intensity: 1.2, color: '#ffffff', castShadow: false }];
  const viewport = { cssWidth: W, cssHeight: H, width: W, height: H };
  // Close enough that the ends of the ship leave the frustum.
  const camera = { x: 4, y: 13, z: 27, rotationX: -0.1, rotationY: 0, fov: 50, near: .1, far: 200 };
  const objects = [...parts, ...agents];
  const planner = { retainedGeometry: true, worldBakedAttributeViews: backendKind === 'gl', rigidImportedBatches: backendKind === 'gl', crowdBatches: true };
  const build = () => {
    const bundle = api.createSceneRenderBundle(W, H, '#101820', camera, objects, [], [], [], [], {}, 0.4, [], [], [], [], [], 0, false, planner);
    bundle.lights = lights;
    if (backendKind === 'gpu' && run.culling) bundle.gpuDriven = { occlusion: run.culling === 'occlusion', shadowCulling: true };
    return bundle;
  };
  const wait = ms => new Promise(resolve => setTimeout(resolve, ms));
  let frameNo = 0;
  const partialCount = Math.max(1, Math.floor(agents.length * .05));
  function step() {
    frameNo += 1;
    setShip(frameNo);
    if (frameNo % 6 === 0) {
      motionStep += 1;
      // A partial frame: one contiguous run of 5% of the agents, as a ship section updating.
      const start = (frameNo / 6 * partialCount) % (agents.length - partialCount);
      for (let i = start; i < start + partialCount; i++) writeMotion(agents[i], i);
    }
  }
  async function frame(timed) {
    step();
    const t0 = performance.now();
    const bundle = build();
    const t1 = performance.now();
    let status = target.render(bundle, viewport, { sceneTime: frameNo / 60, nowMS: 1000 + frameNo * 16, active: true });
    const t2 = performance.now();
    return { status, planner: t1 - t0, render: t2 - t1 };
  }
  // Warm up: pipelines compile asynchronously, so render until they are ready.
  let status = 'pending';
  for (let tries = 0; tries < 400 && status !== 'submitted'; tries++) { status = (await frame(false)).status; if (status !== 'submitted') await wait(25); }
  if (status !== 'submitted') return { label: run.label, error: 'frames stayed ' + status };
  for (let i = 0; i < 30; i++) { await frame(false); await wait(0); }
  if (backendKind === 'gpu' && run.culling) await wait(200);
  const samples = [];
  for (let i = 0; i < frames; i++) {
    samples.push(await frame(true));
    // Yield so the GPU process and the readback map callbacks run, as a real page's rAF loop does.
    if (i % 4 === 3) await wait(0);
  }
  const stat = values => {
    const sorted = values.slice().sort((a, b) => a - b);
    return { mean: values.reduce((a, b) => a + b, 0) / values.length, p50: sorted[Math.floor(sorted.length * .5)], p99: sorted[Math.min(sorted.length - 1, Math.floor(sorted.length * .99))] };
  };
  const totals = samples.map(x => x.planner + x.render);
  // Time until the device finishes a block of frames.
  let completionMS = null;
  if (backendKind === 'gpu') {
    const device = window.__gosx_scene3d_webgpu_probe().device;
    await device.queue.onSubmittedWorkDone();
    const t0 = performance.now();
    for (let i = 0; i < 20; i++) await frame(false);
    await device.queue.onSubmittedWorkDone();
    completionMS = (performance.now() - t0) / 20;
  } else {
    const ctx = canvas.getContext('webgl2');
    const t0 = performance.now();
    for (let i = 0; i < 20; i++) await frame(false);
    ctx.finish();
    completionMS = (performance.now() - t0) / 20;
  }
  // Counters.
  let counts = null, gpuTiming = null;
  if (backendKind === 'gpu') {
    for (let i = 0; i < 6; i++) { await frame(false); await wait(40); }
    const st = document.body.__gosxScene3DWebGPUStats || {};
    counts = { agents: st.crowdAgents, batches: st.crowdBatches, culling: st.crowdCulling, occlusion: st.crowdOcclusion, draws: st.crowdDraws,
      early: st.crowdEarlyVisible, late: st.crowdLateVisible, dispatches: st.crowdDispatches, uploads: st.crowdUploads, uploadedBytes: st.crowdUploadedBytes };
    const timing = typeof target.getFrameTiming === 'function' ? target.getFrameTiming() : null;
    gpuTiming = timing && timing.status === 'measured' ? timing.gpuMS : (timing ? timing.status : null);
  }
  // A frozen frame: the same ship pose and motion records in every run, so
  // the runs' pixels can be compared (culling may not change a pixel).
  motionStep = 0;
  agents.forEach(writeMotion);
  frameNo = 1000;
  setShip(frameNo);
  for (let i = 0; i < 8; i++) {
    const bundle = build();
    for (let tries = 0; tries < 100; tries++) {
      const r = target.render(bundle, viewport, { sceneTime: 1000 / 60, nowMS: 5000 + i, active: true });
      if (r !== 'pending') break;
      await wait(25);
    }
    await wait(30);
  }
  let last = '';
  if (backendKind === 'gpu' && offscreen) {
    const { device, state } = offscreen;
    const rowBytes = W * 4, paddedRow = Math.ceil(rowBytes / 256) * 256;
    const buffer = device.createBuffer({ size: paddedRow * H, usage: GPUBufferUsage.COPY_DST | GPUBufferUsage.MAP_READ });
    const encoder = device.createCommandEncoder();
    encoder.copyTextureToBuffer({ texture: state.texture }, { buffer, bytesPerRow: paddedRow }, [W, H, 1]);
    device.queue.submit([encoder.finish()]);
    await buffer.mapAsync(GPUMapMode.READ);
    const mapped = new Uint8Array(buffer.getMappedRange());
    const pixels = new Uint8Array(W * H * 4);
    const swap = state.format.startsWith('bgra');
    for (let y = 0; y < H; y++) for (let x = 0; x < W; x++) {
      const from = y * paddedRow + x * 4, to = (y * W + x) * 4;
      pixels[to] = mapped[from + (swap ? 2 : 0)]; pixels[to + 1] = mapped[from + 1]; pixels[to + 2] = mapped[from + (swap ? 0 : 2)]; pixels[to + 3] = 255;
    }
    buffer.unmap();
    let binary = '';
    for (let i = 0; i < pixels.length; i += 0x8000) binary += String.fromCharCode.apply(null, pixels.subarray(i, i + 0x8000));
    last = btoa(binary);
  } else if (backendKind === 'gl') {
    const ctx = canvas.getContext('webgl2');
    const raw = new Uint8Array(W * H * 4);
    ctx.readPixels(0, 0, W, H, ctx.RGBA, ctx.UNSIGNED_BYTE, raw);
    const pixels = new Uint8Array(raw.length);
    for (let y = 0; y < H; y++) pixels.set(raw.subarray((H - 1 - y) * W * 4, (H - y) * W * 4), y * W * 4);
    let binary = '';
    for (let i = 0; i < pixels.length; i += 0x8000) binary += String.fromCharCode.apply(null, pixels.subarray(i, i + 0x8000));
    last = btoa(binary);
  }
  return { label: run.label, agents: run.agents, planner: stat(samples.map(x => x.planner)), render: stat(samples.map(x => x.render)), total: stat(totals),
    completionMS, counts, gpuTiming, last };
}

const runs = [];
for (const agents of sizes) {
  runs.push({ agents, label: `${agents} unculled`, culling: '' });
  runs.push({ agents, label: `${agents} frustum`, culling: 'frustum' });
  runs.push({ agents, label: `${agents} occlusion`, culling: 'occlusion' });
}
const results = { gpu: await runBackend('gpu', runs) };
if (withWebGL) results.gl = await runBackend('gl', sizes.map(agents => ({ agents, label: `${agents} webgl`, culling: '' })));

// Culling may not change a pixel: compare each culled run's frozen frame with the unculled one.
const frozen = new Map();
for (const r of results.gpu) if (r.last) frozen.set(r.label, Buffer.from(r.last, 'base64'));
for (const r of results.gpu) {
  const base = frozen.get(`${r.agents} unculled`), mine = frozen.get(r.label);
  if (!base || !mine || r.label.endsWith('unculled')) continue;
  let differ = 0;
  for (let i = 0; i < base.length; i += 4) if (base[i] !== mine[i] || base[i + 1] !== mine[i + 1] || base[i + 2] !== mine[i + 2]) differ++;
  r.pixelsDifferFromUnculled = differ;
}
const f = v => (v == null ? '-' : v.toFixed(2));
console.log(`frames per run: ${frames}; viewport 640x360; partial motion frame (5% of agents) every 6th frame; ship moves every frame`);
console.log('backend | run | planner ms | render() ms | main thread mean / p50 / p99 ms | completion ms/frame | submitted | frustum-visible | drawn (early+late) | pixels differing from unculled (frozen frame)');
for (const [backend, list] of Object.entries(results)) {
  for (const r of list) {
    if (r.error) { console.log(`${backend} | ${r.label} | ERROR ${r.error}`); continue; }
    const c = r.counts;
    const drawn = c ? (c.early || 0) + (c.late || 0) : '-';
    console.log([backend, r.label, f(r.planner.mean), f(r.render.mean), `${f(r.total.mean)} / ${f(r.total.p50)} / ${f(r.total.p99)}`, f(r.completionMS),
      c ? c.agents : r.agents, c && c.culling && !c.occlusion ? c.early : '-', drawn, r.pixelsDifferFromUnculled == null ? '-' : r.pixelsDifferFromUnculled, r.gpuTiming != null ? `renderer timing ${r.gpuTiming}` : ''].join(' | '));
  }
}
console.log(JSON.stringify(results, (key, value) => (key === 'last' ? undefined : value)));
