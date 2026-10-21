#!/usr/bin/env node
// Pixel parity between the per-agent crowd path and the crowd batch path, in a
// real headless Chromium with software WebGL2 (SwiftShader).
//
//   SCENE3D_TOOLS_DIR=<dir containing node_modules/playwright> \
//     node scripts/scene3d-crowd-batch-parity.mjs [pose|motion] [uniform|mixed] [shadows]
//
// SCENE3D_BROWSER names a Chromium or headless-shell binary when Playwright's
// own download is not runnable. The page loads client/js/bootstrap.js and
// bootstrap-feature-scene3d-animation.js, builds 48 skinned agents and renders
// the same agents twice, once with crowdBatches off (one record per agent) and
// once with it on (one record per batch), through two real WebGL renderers.
// Both frames are read back and compared across five states: first frame,
// camera move, pose or motion change, matrix replacement and membership
// change. The script exits 1 if any pixel differs, if a render is not
// "submitted", or if a frame is empty.
//
// "mixed" gives alternate agents another color, so half of them must stay on
// their own records. "shadows" adds a receiving ground and a shadow-casting
// light. The two renderers share one set of agents, so the script marks
// motion records dirty before each build, as a motion frame would.
import path from 'node:path';
import { createRequire } from 'node:module';
import { fileURLToPath } from 'node:url';

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..', 'client', 'js');
const toolsDir = process.env.SCENE3D_TOOLS_DIR;
if (!toolsDir) throw new Error('set SCENE3D_TOOLS_DIR to a directory that contains node_modules/playwright');
const { chromium } = createRequire(import.meta.url)(path.join(toolsDir, 'node_modules/playwright'));
const [mode = 'pose', appearance = 'uniform', shadowFlag = ''] = process.argv.slice(2);
if (!['pose', 'motion'].includes(mode) || !['uniform', 'mixed'].includes(appearance)) {
  throw new Error('usage: scene3d-crowd-batch-parity.mjs [pose|motion] [uniform|mixed] [shadows]');
}

const browser = await chromium.launch({
  executablePath: process.env.SCENE3D_BROWSER || undefined,
  headless: true,
  args: ['--use-angle=swiftshader', '--enable-unsafe-swiftshader', '--ignore-gpu-blocklist', '--use-gl=angle'],
});
const page = await browser.newPage({ viewport: { width: 400, height: 300 } });
page.on('pageerror', error => console.error('[pageerror]', error.message));
await page.setContent('<!doctype html><html><body style="margin:0"><canvas id="legacy" width="320" height="180"></canvas><canvas id="batch" width="320" height="180"></canvas></body></html>');
await page.addScriptTag({ path: path.join(root, 'bootstrap.js') });
await page.addScriptTag({ path: path.join(root, 'bootstrap-feature-scene3d-animation.js') });
await page.waitForFunction(() => window.__gosx_scene3d_api && window.__gosx_scene3d_animation_api, null, { timeout: 10000 });

const results = await page.evaluate(async ([mode, uniform, shadows]) => {
  const api = window.__gosx_scene3d_api;
  const anim = window.__gosx_scene3d_animation_api;
  const backend = api.sceneBackendRegistry.select({ webgl: true, webgl2: true, webgpu: false, canvas: false, canvas2d: false });
  const make = id => {
    const canvas = document.getElementById(id);
    return { canvas, renderer: backend.create(canvas, { background: '#101820' }, { tier: 'full' }) };
  };
  const legacy = make('legacy'), batch = make('batch');

  // A tapered prism skinned to one joint.
  const pos = [], nor = [], uv = [], tan = [], joints = [], weights = [];
  const tri = (a, b, c, n) => {
    for (const p of [a, b, c]) {
      pos.push(...p); nor.push(...n); uv.push(p[0], p[1]); tan.push(1, 0, 0, 1); joints.push(0, 0, 0, 0); weights.push(1, 0, 0, 0);
    }
  };
  const s = 0.18;
  tri([-s, 0, s], [s, 0, s], [0, 0.6, 0], [0, 0.3, 1]);
  tri([s, 0, s], [s, 0, -s], [0, 0.6, 0], [1, 0.3, 0]);
  tri([s, 0, -s], [-s, 0, -s], [0, 0.6, 0], [0, 0.3, -1]);
  tri([-s, 0, -s], [-s, 0, s], [0, 0.6, 0], [-1, 0.3, 0]);
  const vertices = {
    count: pos.length / 3, immutable: true, revision: 0, positions: new Float32Array(pos), normals: new Float32Array(nor),
    uvs: new Float32Array(uv), tangents: new Float32Array(tan), joints: new Float32Array(joints), weights: new Float32Array(weights),
  };
  // One-joint palette: row r lifts the agent by r * 0.03.
  const width = 4, height = 32, data = new Float32Array(width * height * 4);
  for (let row = 0; row < height; row++) {
    const o = row * width * 4;
    data[o] = data[o + 5] = data[o + 10] = data[o + 15] = 1;
    data[o + 13] = row * 0.03;
  }
  const atlas = { id: 'parity-atlas', width, height, data, clips: new Map([['Run', { start: 1, segments: 30, duration: 1 }]]), missingClips: new Set() };
  const bounds = { minX: -.3, minY: 0, minZ: -.3, maxX: .3, maxY: 1.2, maxZ: .3 };

  const count = 48;
  const agents = [];
  for (let i = 0; i < count; i++) {
    const x = (i % 8 - 3.5) * 0.7, z = -Math.floor(i / 8) * 0.9 - 3;
    const agent = api.normalizeSceneObject({ id: `p/a${i}/0`, kind: 'mesh', vertices, materialKind: 'standard',
      color: (uniform || i % 2) ? '#d98c5f' : '#5fb0d9', roughness: .6 }, i);
    Object.assign(agent, { x: 0, y: 0, z: 0, rotationX: 0, rotationY: 0, rotationZ: 0, spinX: 0, spinY: 0, spinZ: 0,
      shiftX: 0, shiftY: 0, shiftZ: 0, static: false, skin: null, pickable: false, castShadow: shadows, receiveShadow: shadows,
      depthWrite: true, parentMatrix: new Float32Array([1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, x, 0, z, 1]) });
    agent.vertices.immutable = true;
    agent.vertices.revision = 0;
    agent._crowdSkin = { atlas, bounds, rows: new Float32Array([(i * 3) % 28, (i * 3) % 28 + 1, .5]), poseRows: null,
      batchID: 'p', rev: 0, group: null };
    agents.push(agent);
  }
  if (mode === 'motion') {
    const clipTable = anim.crowdMotionClipTable(atlas);
    agents.forEach((agent, i) => {
      const x = agent.parentMatrix[12], z = agent.parentMatrix[14];
      const motion = { atlas, clipTable, record: new Float32Array(anim.crowdMotionRecordFloats), dirty: true,
        localRadius: anim.crowdMotionLocalRadius(bounds),
        bounds: { minX: Infinity, minY: Infinity, minZ: Infinity, maxX: -Infinity, maxY: -Infinity, maxZ: -Infinity } };
      anim.crowdMotionWriteRecord(motion.record, { prevX: x, prevY: 0, prevZ: z, prevRotationX: 0, prevRotationY: i * .1, prevRotationZ: 0,
        prevScaleX: 1, prevScaleY: 1, prevScaleZ: 1, tPrev: 0, nextX: x + .2, nextY: 0, nextZ: z, nextRotationX: 0,
        nextRotationY: i * .1 + .1, nextRotationZ: 0, nextScaleX: 1, nextScaleY: 1, nextScaleZ: 1, tNext: 1,
        animation: 'Run', clipStartTime: 0, animationLoop: true, playbackRate: 1 }, anim.crowdMotionClipIndex(atlas, 'Run'));
      anim.crowdMotionSweptBoundsInto(motion.bounds, motion.record, motion.localRadius);
      agent._crowdMotion = motion;
    });
    legacy.renderer.prepareCrowdMotionShaders();
    batch.renderer.prepareCrowdMotionShaders();
  }
  let objects = agents.slice();
  if (shadows) {
    const ground = api.normalizeSceneObject({ id: 'ground', kind: 'mesh', materialKind: 'standard', color: '#808080', roughness: .9,
      vertices: { count: 6, immutable: true, revision: 0,
        positions: new Float32Array([-6, 0, 2, 6, 0, 2, 6, 0, -12, -6, 0, 2, 6, 0, -12, -6, 0, -12]),
        normals: new Float32Array([0, 1, 0, 0, 1, 0, 0, 1, 0, 0, 1, 0, 0, 1, 0, 0, 1, 0]),
        uvs: new Float32Array([0, 0, 1, 0, 1, 1, 0, 0, 1, 1, 0, 1]),
        tangents: new Float32Array([1, 0, 0, 1, 1, 0, 0, 1, 1, 0, 0, 1, 1, 0, 0, 1, 1, 0, 0, 1, 1, 0, 0, 1]) } }, 999);
    Object.assign(ground, { castShadow: false, receiveShadow: true, pickable: false, wireframe: false, y: -0.02 });
    ground.vertices.immutable = true;
    ground.vertices.revision = 0;
    objects = [ground, ...agents];
  }
  const lights = [{ id: 'sun', kind: 'directional', directionX: .3, directionY: -1, directionZ: -.4, intensity: 1.4,
    color: '#ffffff', castShadow: shadows, shadowSize: 1024 }];
  const viewport = { cssWidth: 320, cssHeight: 180, width: 320, height: 180 };
  const markDirty = () => { for (const agent of agents) if (agent._crowdMotion) agent._crowdMotion.dirty = true; };
  const build = (camera, crowdBatches) => {
    markDirty();
    const bundle = api.createSceneRenderBundle(320, 180, '#101820', camera, objects, [], [], [], [], {}, 0.4, [], [], [], [], [], 0, false,
      { retainedGeometry: true, worldBakedAttributeViews: true, rigidImportedBatches: true, crowdBatches });
    bundle.lights = lights;
    return bundle;
  };
  async function draw(target, bundle) {
    let status = '';
    for (let tries = 0; tries < 80; tries++) {
      status = target.renderer.render(bundle, viewport, { sceneTime: 0.5 });
      if (status !== 'pending') break;
      await new Promise(resolve => setTimeout(resolve, 25));
    }
    const gl = target.canvas.getContext('webgl2');
    const pixels = new Uint8Array(320 * 180 * 4);
    gl.readPixels(0, 0, 320, 180, gl.RGBA, gl.UNSIGNED_BYTE, pixels);
    return { status, pixels };
  }
  const compare = async (label, camera) => {
    const a = await draw(legacy, build(camera, false));
    const b = await draw(batch, build(camera, true));
    let different = 0, maxDelta = 0, lit = 0;
    for (let i = 0; i < a.pixels.length; i += 4) {
      let delta = 0;
      for (let k = 0; k < 3; k++) delta = Math.max(delta, Math.abs(a.pixels[i + k] - b.pixels[i + k]));
      if (delta > 0) different++;
      maxDelta = Math.max(maxDelta, delta);
      if (Math.abs(a.pixels[i] - 0x10) + Math.abs(a.pixels[i + 1] - 0x18) + Math.abs(a.pixels[i + 2] - 0x20) > 12) lit++;
    }
    return { label, legacy: a.status, batch: b.status, different, maxDelta, lit,
      records: build(camera, true).meshObjects.filter(record => record._crowdBatch).length };
  };
  const cameraA = { x: 0, y: 1.2, z: 3, fov: 62, near: .05, far: 100 };
  const cameraB = { x: .8, y: 1.6, z: 3.5, rotationY: .12, fov: 62, near: .05, far: 100 };
  const out = [await compare('first frame', cameraA), await compare('camera moved', cameraB)];
  if (mode === 'pose') {
    for (let i = 0; i < count; i += 3) {
      agents[i]._crowdSkin.rows.set([(i * 5) % 27, (i * 5) % 27 + 1, .25]);
      agents[i]._crowdSkin.rev += 1;
    }
    out.push(await compare('pose rows changed', cameraB));
    agents[7].parentMatrix = new Float32Array([1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, 0, .5, -2, 1]);
    out.push(await compare('matrix replaced', cameraB));
  } else {
    for (let i = 0; i < count; i += 4) {
      const motion = agents[i]._crowdMotion;
      motion.record[0] += .3;
      motion.record[10] += .3;
    }
    out.push(await compare('motion frame', cameraB));
  }
  objects.splice(objects.indexOf(agents[5]), 3);
  out.push(await compare('three agents removed', cameraB));
  return out;
}, [mode, appearance === 'uniform', shadowFlag === 'shadows']);
await browser.close();

let failed = false;
for (const r of results) {
  const ok = r.different === 0 && r.legacy === 'submitted' && r.batch === 'submitted' && r.lit > 0;
  if (!ok) failed = true;
  console.log(`${ok ? 'ok  ' : 'FAIL'} ${mode}/${appearance}${shadowFlag ? '/shadows' : ''} ${r.label}: ${r.different} pixels differ (max delta ${r.maxDelta}), ${r.lit} lit pixels, ${r.records} batch record(s)`);
}
process.exit(failed ? 1 : 0);
