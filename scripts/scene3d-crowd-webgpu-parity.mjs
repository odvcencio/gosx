#!/usr/bin/env node
// Crowd batches on WebGPU against the same frames on WebGL, in a real headless
// Chromium. Both backends run on SwiftShader (software Vulkan and software
// GL), so rasterization rules match and what differs is shading math.
//
//   SCENE3D_TOOLS_DIR=<dir containing node_modules/playwright> \
//     node scripts/scene3d-crowd-webgpu-parity.mjs [pose|motion] [flags...]
//
// flags (any order):
//   shadows    add a receiving ground and a shadow-casting light
//   parent     ride a moving parent node ("ship"), pose changing every frame
//   frustum    opt in to GPU culling (bundle.gpuDriven {})
//   occlusion  GPU culling with Hi-Z (bundle.gpuDriven {occlusion: true}); adds
//              an opaque deck (a wide wall) that hides the back rows of the crowd
//   window     with occlusion, leave a window in the deck wall, so the agents
//              behind it must stay visible
//   msaa       4x multisampling on WebGPU (the Hi-Z seed reads a multisampled depth)
//   indexed    agents use an indexed mesh (shared vertices and a Uint32 index
//              stream), as glTF assets do
//   mixed      alternate the agents' colors, so half of them cannot join the
//              batch and draw as batches of one
//   count=N    agents (default 48)
//   dump=DIR   write each backend's frames as PPM files into DIR
//
// The two backends run in two browser processes, because a WebGPU device and a
// WebGL context in one SwiftShader process lose each other. Headless Chromium
// also cannot present a WebGPU canvas on SwiftShader (the GPU process loses its
// Instance: "A valid external Instance reference no longer exists"), so the
// script gives the real WebGPU renderer a canvas whose WebGPU context hands out
// an offscreen texture and reads that texture back. Pipelines, shaders, buffers,
// compute and passes are the production ones.
//
// SCENE3D_BROWSER names a Chromium or headless-shell binary when Playwright's
// own download is not runnable. Headless WebGPU needs only --enable-unsafe-webgpu
// with the headless shell, and the page must be a secure context (the script
// serves http://localhost from a route).
//
// What it checks, per frame state:
//   1. Both backends render ("submitted") and draw lit pixels.
//   2. The WebGPU frame equals the WebGL frame. The script first measures the
//      baseline difference of the two backends on plain static quads in the
//      agents' colors. Pixels away from a color step ("interior") must match to
//      a few levels; pixels on a color step ("edge") may differ by rasterization
//      (silhouettes of many small triangles); the lit/unlit silhouette may
//      differ by a few percent.
//   3. With culling on, the WebGPU frame equals the frame WebGPU draws with
//      culling off (at most 3 pixels differ; see below), and the indirect-args
//      readback shows fewer agents drawn than submitted when the deck hides some.
import fs from 'node:fs';
import path from 'node:path';
import { createRequire } from 'node:module';
import { fileURLToPath } from 'node:url';

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..', 'client', 'js');
const toolsDir = process.env.SCENE3D_TOOLS_DIR;
if (!toolsDir) throw new Error('set SCENE3D_TOOLS_DIR to a directory that contains node_modules/playwright');
const { chromium } = createRequire(import.meta.url)(path.join(toolsDir, 'node_modules/playwright'));
const argv = process.argv.slice(2);
const mode = argv.includes('motion') ? 'motion' : 'pose';
const flags = {
  shadows: argv.includes('shadows'), parent: argv.includes('parent'),
  frustum: argv.includes('frustum'), occlusion: argv.includes('occlusion'),
  indexed: argv.includes('indexed'), mixed: argv.includes('mixed'), window: argv.includes('window'), msaa: argv.includes('msaa'),
};
const countArg = argv.find(a => a.startsWith('count='));
const count = countArg ? Number(countArg.slice(6)) : 48;
const dumpArg = argv.find(a => a.startsWith('dump='));
const dumpDir = dumpArg ? dumpArg.slice(5) : '';

async function renderBackend(backendKind, cullingOverride) {
  const browser = await chromium.launch({
    executablePath: process.env.SCENE3D_BROWSER || undefined,
    headless: true,
    // WebGPU needs only --enable-unsafe-webgpu; adding the ANGLE flags that WebGL
    // needs makes Dawn lose its instance ("A valid external Instance reference no
    // longer exists") as soon as the first frame presents.
    args: backendKind === 'gpu' ? ['--enable-unsafe-webgpu']
      : ['--use-angle=swiftshader', '--enable-unsafe-swiftshader', '--ignore-gpu-blocklist', '--use-gl=angle'],
  });
  const page = await browser.newPage({ viewport: { width: 700, height: 300 } });
  page.on('pageerror', error => console.error('[pageerror]', error.message));
  page.on('console', message => { if (message.type() === 'warning' || message.type() === 'error') console.error('[console]', message.text().slice(0, 400)); });
  await page.route('http://localhost/**', route => route.fulfill({ contentType: 'text/html',
    body: '<!doctype html><html><body style="margin:0"><canvas id="c" width="320" height="180"></canvas></body></html>' }));
  await page.goto('http://localhost/');
  await page.addScriptTag({ path: path.join(root, 'bootstrap.js') });
  await page.addScriptTag({ path: path.join(root, 'bootstrap-feature-scene3d-animation.js') });
  await page.addScriptTag({ path: path.join(root, 'bootstrap-feature-scene3d-webgpu.js') });
  await page.waitForFunction(() => window.__gosx_scene3d_api && window.__gosx_scene3d_animation_api, null, { timeout: 10000 });
  const result = await page.evaluate(async ([backendKind, mode, flags, count, culling]) => {
    const api = window.__gosx_scene3d_api;
    const anim = window.__gosx_scene3d_animation_api;
    const canvas = document.getElementById('c');
    let target;
    let offscreen = null;
    if (backendKind === 'gpu') {
      if (!(await window.__gosx_scene3d_webgpu_probe_ready())) return { error: 'WebGPU probe failed: ' + JSON.stringify(window.__gosx_scene3d_webgpu_probe().error) };
      const device = window.__gosx_scene3d_webgpu_probe().device;
      // Presenting a WebGPU canvas loses the GPU process in headless Chromium on
      // SwiftShader ("A valid external Instance reference no longer exists"), but
      // offscreen rendering, compute and readback all work. Give the renderer a
      // canvas whose WebGPU context hands out an offscreen texture, and read that
      // texture back. Everything else the renderer does is unchanged.
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
      const backend = api.sceneBackendRegistry.select({ webgpu: true, preferWebGPU: true, webgl: false });
      target = backend.create(canvas, { background: '#101820', msaaSamples: flags.msaa ? 4 : 0 }, { tier: 'full' });
      if (!target || target.kind !== 'webgpu') return { error: 'no WebGPU renderer' };
    } else {
      const backend = api.sceneBackendRegistry.select({ webgl: true, webgl2: true, webgpu: false });
      target = backend.create(canvas, { background: '#101820' }, { tier: 'full' });
    }

    // A tapered prism skinned to two joints, so the palette rows visibly bend it.
    const pos = [], nor = [], uv = [], tan = [], joints = [], weights = [];
    const tri = (a, b, c, n) => {
      for (const p of [a, b, c]) {
        pos.push(...p); nor.push(...n); uv.push(p[0], p[1]); tan.push(1, 0, 0, 1);
        joints.push(p[1] > 0.5 ? 1 : 0, 0, 0, 0); weights.push(1, 0, 0, 0);
      }
    };
    const s = 0.18;
    let vertices;
    if (flags.indexed) {
      // Five shared vertices, four side triangles.
      const corners = [[-s, 0, s], [s, 0, s], [s, 0, -s], [-s, 0, -s], [0, 0.6, 0]];
      for (const [i, p] of corners.entries()) {
        pos.push(...p); nor.push(0, 0.3 + (i === 4 ? 0.7 : 0), 0.6); uv.push(p[0], p[1]); tan.push(1, 0, 0, 1);
        joints.push(p[1] > 0.5 ? 1 : 0, 0, 0, 0); weights.push(1, 0, 0, 0);
      }
      vertices = { count: 5, immutable: true, revision: 0, positions: new Float32Array(pos), normals: new Float32Array(nor),
        uvs: new Float32Array(uv), tangents: new Float32Array(tan), joints: new Float32Array(joints), weights: new Float32Array(weights),
        indices: new Uint32Array([0, 1, 4, 1, 2, 4, 2, 3, 4, 3, 0, 4]) };
    } else {
      tri([-s, 0, s], [s, 0, s], [0, 0.6, 0], [0, 0.3, 1]);
      tri([s, 0, s], [s, 0, -s], [0, 0.6, 0], [1, 0.3, 0]);
      tri([s, 0, -s], [-s, 0, -s], [0, 0.6, 0], [0, 0.3, -1]);
      tri([-s, 0, -s], [-s, 0, s], [0, 0.6, 0], [-1, 0.3, 0]);
      vertices = {
        count: pos.length / 3, immutable: true, revision: 0, positions: new Float32Array(pos), normals: new Float32Array(nor),
        uvs: new Float32Array(uv), tangents: new Float32Array(tan), joints: new Float32Array(joints), weights: new Float32Array(weights),
      };
    }
    // Two-joint palette: row r lifts joint 0 by r * 0.03 and shifts joint 1 sideways by r * 0.01.
    const width = 8, height = 32, data = new Float32Array(width * height * 4);
    for (let row = 0; row < height; row++) {
      const o = row * width * 4;
      for (let joint = 0; joint < 2; joint++) {
        const j = o + joint * 16;
        data[j] = data[j + 5] = data[j + 10] = data[j + 15] = 1;
        data[j + 13] = row * 0.03;
        if (joint === 1) data[j + 12] = row * 0.01;
      }
    }
    const atlas = { id: 'parity-atlas', width, height, data, clips: new Map([['Run', { start: 1, segments: 30, duration: 1 }]]), missingClips: new Set() };
    // Bounds of every pose in the palette: a lift of 0.03 per row (0.84 at row 28), apex at 0.6.
    const bounds = { minX: -.3, minY: 0, minZ: -.3, maxX: .5, maxY: 1.5, maxZ: .3 };

    const agents = [];
    const columns = Math.min(24, Math.max(4, Math.ceil(Math.sqrt(count * 2))));
    for (let i = 0; i < count; i++) {
      const x = (i % columns - (columns - 1) / 2) * 0.7, z = -Math.floor(i / columns) * 0.9 - 3;
      const agent = api.normalizeSceneObject({ id: `p/a${i}/0`, kind: 'mesh', vertices, materialKind: 'standard',
        color: flags.mixed && i % 2 ? '#5fb0d9' : '#d98c5f', roughness: .6 }, i);
      Object.assign(agent, { x: 0, y: 0, z: 0, rotationX: 0, rotationY: 0, rotationZ: 0, spinX: 0, spinY: 0, spinZ: 0,
        shiftX: 0, shiftY: 0, shiftZ: 0, static: false, skin: null, pickable: false, castShadow: flags.shadows, receiveShadow: flags.shadows,
        depthWrite: true, parentMatrix: new Float32Array([1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, x, 0, z, 1]) });
      agent.vertices.immutable = true;
      agent.vertices.revision = 0;
      agent._crowdSkin = { atlas, bounds, rows: new Float32Array([(i * 3) % 28, (i * 3) % 28 + 1, .5]), poseRows: null,
        batchID: 'p', rev: 0, group: null, parent: flags.parent ? 'ship' : '' };
      agents.push(agent);
    }
    const writeMotion = (agent, i, dx) => {
      const x = agent.parentMatrix[12], z = agent.parentMatrix[14];
      anim.crowdMotionWriteRecord(agent._crowdMotion.record, { prevX: x, prevY: 0, prevZ: z, prevRotationX: 0, prevRotationY: i * .1, prevRotationZ: 0,
        prevScaleX: 1, prevScaleY: 1, prevScaleZ: 1, tPrev: 0, nextX: x + dx, nextY: 0, nextZ: z, nextRotationX: 0,
        nextRotationY: i * .1 + .1, nextRotationZ: 0, nextScaleX: 1, nextScaleY: 1, nextScaleZ: 1, tNext: 1,
        animation: 'Run', clipStartTime: 0, animationLoop: true, playbackRate: 1 }, anim.crowdMotionClipIndex(atlas, 'Run'));
      const b = agent._crowdMotion.bounds;
      b.minX = b.minY = b.minZ = Infinity; b.maxX = b.maxY = b.maxZ = -Infinity;
      anim.crowdMotionSweptBoundsInto(b, agent._crowdMotion.record, agent._crowdMotion.localRadius);
      agent._crowdMotion.dirty = true;
    };
    if (mode === 'motion') {
      agents.forEach((agent, i) => {
        agent._crowdMotion = { atlas, clipTable: anim.crowdMotionClipTable(atlas), record: new Float32Array(anim.crowdMotionRecordFloats), dirty: true,
          localRadius: anim.crowdMotionLocalRadius(bounds), bounds: { minX: 0, minY: 0, minZ: 0, maxX: 0, maxY: 0, maxZ: 0 } };
        writeMotion(agent, i, .2);
      });
    }
    if (typeof target.prepareCrowdAtlas === 'function') target.prepareCrowdAtlas(atlas);
    if (typeof target.prepareCrowdMotionShaders === 'function') target.prepareCrowdMotionShaders();

    const mesh = (id, material, color, positions, normals, castShadow, receiveShadow, index) => {
      const o = api.normalizeSceneObject({ id, kind: 'mesh', materialKind: material, color, roughness: .9,
        vertices: { count: positions.length / 3, immutable: true, revision: 0, positions: new Float32Array(positions), normals: new Float32Array(normals),
          uvs: new Float32Array(positions.length / 3 * 2), tangents: new Float32Array(positions.length / 3 * 4).map((v, k) => (k % 4 === 0 || k % 4 === 3 ? 1 : 0)) } }, index);
      Object.assign(o, { castShadow, receiveShadow, pickable: false, wireframe: false, doubleSided: true });
      o.vertices.immutable = true; o.vertices.revision = 0;
      return o;
    };
    const floor = (id, y, x0, x1, z0, z1, castShadow, receiveShadow, color, material = 'standard') => mesh(id, material, color,
      [x0, y, z1, x1, y, z1, x1, y, z0, x0, y, z1, x1, y, z0, x0, y, z0], [0, 1, 0, 0, 1, 0, 0, 1, 0, 0, 1, 0, 0, 1, 0, 0, 1, 0], castShadow, receiveShadow, 900 + id.length);
    const wallPart = (id, x0, x1, z, color, index) => mesh(id, 'standard', color,
      [x0, -1, z, x1, -1, z, x1, 9, z, x0, -1, z, x1, 9, z, x0, 9, z], [0, 0, 1, 0, 0, 1, 0, 0, 1, 0, 0, 1, 0, 0, 1, 0, 0, 1], false, false, index);
    const rows = Math.ceil(count / columns);
    const statics = [];
    if (flags.shadows) statics.push(floor('ground', -0.02, -12, 12, -3 - rows * 0.9 - 2, 3, false, true, '#808080'));
    // The deck stands in front of the back half of the crowd.
    if (flags.occlusion) {
      const z = -3 - rows * 0.9 / 2 + 0.45;
      if (flags.window) statics.push(wallPart('deck-l', -9, -0.6, z, '#3a4a5a', 950), wallPart('deck-r', 0.6, 9, z, '#3a4a5a', 951));
      else statics.push(wallPart('deck', -9, 9, z, '#3a4a5a', 950));
    }
    // Control: a static mesh with the agents' material, to measure the baseline difference of the backends.
    const controls = [floor('control-a', 0.0, -1.6, -0.2, -2.2, -1.2, false, false, '#d98c5f'),
      floor('control-b', 0.0, 0.2, 1.6, -2.2, -1.2, false, false, '#5fb0d9')];
    const ship = api.normalizeSceneObject({ id: 'ship/hull', kind: 'mesh', vertices, materialKind: 'standard', color: '#884422', pickable: false }, 998);
    Object.assign(ship, { x: 0, y: 0, z: 0, rotationX: 0, rotationY: 0, rotationZ: 0, spinX: 0, spinY: 0, spinZ: 0, shiftX: 0, shiftY: 0, shiftZ: 0,
      static: false, skin: null, visible: false, parentMatrix: new Float32Array([1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1]) });
    const setShip = k => {
      const yaw = .1 * k, c = Math.cos(yaw), sn = Math.sin(yaw);
      ship.parentMatrix.set([c, 0, -sn, 0, 0, 1, 0, 0, sn, 0, c, 0, k * .3, .05 * k, k * .2, 1]);
    };

    const lights = [{ id: 'sun', kind: 'directional', directionX: .3, directionY: -1, directionZ: -.4, intensity: 1.4,
      color: '#ffffff', castShadow: flags.shadows, shadowSize: 1024 }];
    const viewport = { cssWidth: 320, cssHeight: 180, width: 320, height: 180 };
    const markDirty = () => { for (const agent of agents) if (agent._crowdMotion) agent._crowdMotion.dirty = true; };
    const build = (camera, controlOnly) => {
      markDirty();
      const objects = controlOnly ? controls : [...statics, ...(flags.parent ? [ship] : []), ...agents];
      const bundle = api.createSceneRenderBundle(320, 180, '#101820', camera, objects, [], [], [], [], {}, 0.4, [], [], [], [], [], 0, false,
        backendKind === 'gl'
          ? { retainedGeometry: true, worldBakedAttributeViews: true, rigidImportedBatches: true, crowdBatches: true }
          : { retainedGeometry: true, worldBakedAttributeViews: false, rigidImportedBatches: false, crowdBatches: true });
      bundle.lights = lights;
      if (backendKind === 'gpu' && culling) bundle.gpuDriven = { occlusion: flags.occlusion, shadowCulling: true };
      return bundle;
    };
    const wait = ms => new Promise(resolve => setTimeout(resolve, ms));
    let frameNo = 0;
    async function draw(bundle) {
      let status = '';
      for (let tries = 0; tries < 200; tries++) {
        frameNo += 1;
        status = target.render(bundle, viewport, { sceneTime: 0.5, nowMS: 500 + frameNo });
        if (status !== 'pending') break;
        await wait(25);
      }
      let pixels;
      if (backendKind === 'gl') {
        const ctx = canvas.getContext('webgl2');
        const raw = new Uint8Array(320 * 180 * 4);
        ctx.readPixels(0, 0, 320, 180, ctx.RGBA, ctx.UNSIGNED_BYTE, raw);
        pixels = new Uint8Array(raw.length);
        for (let y = 0; y < 180; y++) pixels.set(raw.subarray((179 - y) * 1280, (180 - y) * 1280), y * 1280);
      } else {
        const { device, state } = offscreen;
        const rowBytes = 320 * 4, paddedRow = Math.ceil(rowBytes / 256) * 256;
        const buffer = device.createBuffer({ size: paddedRow * 180, usage: GPUBufferUsage.COPY_DST | GPUBufferUsage.MAP_READ });
        const encoder = device.createCommandEncoder();
        encoder.copyTextureToBuffer({ texture: state.texture }, { buffer, bytesPerRow: paddedRow }, [320, 180, 1]);
        device.queue.submit([encoder.finish()]);
        await buffer.mapAsync(GPUMapMode.READ);
        const mapped = new Uint8Array(buffer.getMappedRange());
        pixels = new Uint8Array(320 * 180 * 4);
        const swap = state.format.startsWith('bgra');
        for (let y = 0; y < 180; y++) {
          for (let x = 0; x < 320; x++) {
            const from = y * paddedRow + x * 4, to = (y * 320 + x) * 4;
            pixels[to] = mapped[from + (swap ? 2 : 0)]; pixels[to + 1] = mapped[from + 1]; pixels[to + 2] = mapped[from + (swap ? 0 : 2)]; pixels[to + 3] = mapped[from + 3];
          }
        }
        buffer.unmap(); buffer.destroy();
      }
      let binary = '';
      for (let i = 0; i < pixels.length; i += 0x8000) binary += String.fromCharCode.apply(null, pixels.subarray(i, i + 0x8000));
      return { status, pixels: btoa(binary) };
    }
    const stats = () => {
      const s = document.body.__gosxScene3DWebGPUStats || {};
      return { batches: s.crowdBatches, agents: s.crowdAgents, culling: s.crowdCulling, occlusion: s.crowdOcclusion, draws: s.crowdDraws,
        early: s.crowdEarlyVisible, late: s.crowdLateVisible, shadowCasters: s.crowdShadowCasters, dispatches: s.crowdDispatches,
        uploads: s.crowdUploads, uploadedBytes: s.crowdUploadedBytes, batchesCulled: s.crowdBatchesCulled };
    };
    const cameraA = { x: 0, y: 1.2, z: 3, fov: 62, near: .05, far: 100 };
    const cameraB = { x: .8, y: 1.6, z: 3.5, rotationY: .12, fov: 62, near: .05, far: 100 };
    const frames = [];
    const capture = async (label, camera, controlOnly) => {
      const first = await draw(build(camera, controlOnly));
      // Later frames let the culling readback settle and give Hi-Z its history.
      let last = first, settled = {};
      const repeats = backendKind === 'gpu' && !controlOnly ? 4 : 0;
      for (let k = 0; k < repeats; k++) { await wait(30); last = await draw(build(camera, controlOnly)); settled = stats(); }
      frames.push({ label, status: last.status, pixels: last.pixels, stats: settled });
    };
    await capture('baseline', cameraA, true);
    if (flags.parent) setShip(0);
    await capture('first frame', cameraA, false);
    if (flags.parent) setShip(1);
    await capture(flags.parent ? 'parent moved' : 'camera moved', cameraB, false);
    if (mode === 'pose') {
      for (let i = 0; i < agents.length; i += 3) { agents[i]._crowdSkin.rows.set([(i * 5) % 27, (i * 5) % 27 + 1, .25]); agents[i]._crowdSkin.rev += 1; }
      if (flags.parent) setShip(2);
      await capture('pose rows changed', cameraB, false);
    } else {
      for (let i = 0; i < agents.length; i += 4) writeMotion(agents[i], i, .5);
      if (flags.parent) setShip(2);
      await capture('motion frame', cameraB, false);
    }
    agents.splice(5, 3);
    await capture('three agents removed', cameraB, false);
    return { frames };
  }, [backendKind, mode, flags, count, cullingOverride]);
  await browser.close();
  if (result.error) throw new Error(backendKind + ': ' + result.error);
  return result.frames.map(f => ({ ...f, pixels: Buffer.from(f.pixels, 'base64') }));
}

const bg = [0x10, 0x18, 0x20];
const isLit = (p, i) => Math.abs(p[i] - bg[0]) + Math.abs(p[i + 1] - bg[1]) + Math.abs(p[i + 2] - bg[2]) > 12;
// A pixel is an edge when a neighbor in either image differs from it by more than 16 levels.
function edgeMask(a, b) {
  const edge = new Uint8Array(320 * 180);
  const step = (img, i, j) => Math.max(Math.abs(img[i] - img[j]), Math.abs(img[i + 1] - img[j + 1]), Math.abs(img[i + 2] - img[j + 2])) > 16;
  for (let y = 0; y < 180; y++) {
    for (let x = 0; x < 320; x++) {
      const i = (y * 320 + x) * 4;
      let near = false;
      for (let dy = -1; dy <= 1 && !near; dy++) {
        for (let dx = -1; dx <= 1; dx++) {
          const nx = x + dx, ny = y + dy;
          if ((dx || dy) && nx >= 0 && nx < 320 && ny >= 0 && ny < 180) {
            const j = (ny * 320 + nx) * 4;
            if (step(a, i, j) || step(b, i, j)) { near = true; break; }
          }
        }
      }
      edge[y * 320 + x] = near ? 1 : 0;
    }
  }
  return edge;
}
function diff(a, b) {
  const edge = edgeMask(a, b);
  const r = { different: 0, maxDelta: 0, lit: 0, maskMismatch: 0, interior: 0, interiorDifferent: 0, interiorMax: 0, edge: 0, edgeDifferent: 0, edgeSum: 0 };
  for (let p = 0; p < 320 * 180; p++) {
    const i = p * 4;
    let delta = 0;
    for (let k = 0; k < 3; k++) delta = Math.max(delta, Math.abs(a[i + k] - b[i + k]));
    if (delta > 0) r.different++;
    r.maxDelta = Math.max(r.maxDelta, delta);
    const la = isLit(a, i), lb = isLit(b, i);
    if (la !== lb) r.maskMismatch++;
    if (!la && !lb) continue;
    r.lit++;
    if (edge[p]) { r.edge++; r.edgeSum += delta; if (delta > 0) r.edgeDifferent++; }
    else { r.interior++; if (delta > 0) r.interiorDifferent++; r.interiorMax = Math.max(r.interiorMax, delta); }
  }
  r.edgeMean = r.edge ? r.edgeSum / r.edge : 0;
  return r;
}
function writePPM(file, pixels) {
  const out = Buffer.alloc(320 * 180 * 3);
  for (let i = 0, j = 0; i < pixels.length; i += 4, j += 3) { out[j] = pixels[i]; out[j + 1] = pixels[i + 1]; out[j + 2] = pixels[i + 2]; }
  fs.writeFileSync(file, Buffer.concat([Buffer.from('P6\n320 180\n255\n'), out]));
}

const culling = flags.frustum || flags.occlusion;
const glFrames = await renderBackend('gl', false);
const gpuFrames = await renderBackend('gpu', culling);
// With culling on, the same frames again with culling off: culling may not change a pixel.
const uncutFrames = culling ? await renderBackend('gpu', false) : null;
if (dumpDir) {
  fs.mkdirSync(dumpDir, { recursive: true });
  glFrames.forEach((f, i) => writePPM(path.join(dumpDir, `gl-${i}.ppm`), f.pixels));
  gpuFrames.forEach((f, i) => writePPM(path.join(dumpDir, `gpu-${i}.ppm`), f.pixels));
  if (uncutFrames) uncutFrames.forEach((f, i) => writePPM(path.join(dumpDir, `gpu-uncut-${i}.ppm`), f.pixels));
}

const label = `${mode}${Object.entries(flags).filter(([, on]) => on).map(([name]) => '/' + name).join('')}`;
let failed = false;
const baseline = diff(glFrames[0].pixels, gpuFrames[0].pixels);
const baselineOK = glFrames[0].status === 'submitted' && gpuFrames[0].status === 'submitted' && baseline.lit > 0 && baseline.interiorMax <= 12;
if (!baselineOK) failed = true;
console.log(`${baselineOK ? 'ok  ' : 'FAIL'} ${label} baseline static quads, WebGPU vs WebGL: ${baseline.different} pixels differ (max ${baseline.maxDelta}); interior ${baseline.interiorDifferent}/${baseline.interior} differ (max ${baseline.interiorMax}), edge ${baseline.edgeDifferent}/${baseline.edge} differ`);
// Tolerance, stated: interior pixels match to 12 levels and differ in at most 10%;
// edge pixels may differ, with a mean gap under 12 levels; the silhouette
// differs in at most 4% of lit pixels. Measured on this scene: interior max 10,
// edge mean 9, silhouette 3%.
for (let i = 1; i < glFrames.length; i++) {
  const gl = glFrames[i], gpu = gpuFrames[i];
  const d = diff(gl.pixels, gpu.pixels);
  let ok = gl.status === 'submitted' && gpu.status === 'submitted' && d.lit > 0;
  if (d.interiorMax > 12 || d.interiorDifferent > 0.1 * d.interior || d.edgeMean > 12 || d.maskMismatch > 0.04 * d.lit) ok = false;
  let cull = '';
  if (culling) {
    const u = diff(uncutFrames[i].pixels, gpu.pixels);
    const s = gpu.stats;
    const drawn = (s.early || 0) + (s.late || 0);
    cull = ` | culled vs uncut: ${u.different} pixels differ; agents ${s.agents}, drawn ${drawn} (early ${s.early}, late ${s.late})`;
    // An agent about 1 m behind a wall can leak a single pixel through it when
    // drawn (WebGPU and WebGL on SwiftShader both do; the leak vanishes when the
    // agent is 4 m behind). Culling removes the agent, so allow a few pixels.
    if (u.different > 3 || s.culling !== true) ok = false;
    // A window in the deck wall keeps coarse Hi-Z blocks from proving anything hidden near it.
    if (flags.occlusion && !flags.window && i === 1 && !(drawn < s.agents)) ok = false;
  }
  if (!ok) failed = true;
  console.log(`${ok ? 'ok  ' : 'FAIL'} ${label} ${gl.label}: interior ${d.interiorDifferent}/${d.interior} differ (max ${d.interiorMax}), edge ${d.edgeDifferent}/${d.edge} differ (mean ${d.edgeMean.toFixed(1)}), silhouette ${d.maskMismatch}/${d.lit} differ${cull}`);
}
process.exit(failed ? 1 : 0);
