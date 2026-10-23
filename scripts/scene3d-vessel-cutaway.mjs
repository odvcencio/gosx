#!/usr/bin/env node
// Pixel checks for the vessel cutaway in a real headless Chromium with software
// WebGL2 (SwiftShader).
//
//   SCENE3D_TOOLS_DIR=<dir containing node_modules/playwright> \
//     node scripts/scene3d-vessel-cutaway.mjs [webgl|webgpu] [shadows] [--out <dir>]
//
// The backend is webgl by default. --out writes one PNG per state to <dir>, named
// <backend>-<n>-<state>.png.
//
// SCENE3D_BROWSER names a Chromium or headless-shell binary when Playwright's
// own download is not runnable.
//
// Real GPU (for example Windows Chrome): set SCENE3D_REAL_GPU_PROFILE to a
// browser profile directory and SCENE3D_BROWSER to chrome.exe. The script then
// launches a visible persistent context with ANGLE on D3D11 instead of headless
// SwiftShader, prints the WebGPU adapter info, and accepts playwright-core in
// SCENE3D_TOOLS_DIR. SCENE3D_JS_DIR names a directory that holds copies of the
// bundles when the repository is not readable from the browser host.
// The WebGPU backend also loads bootstrap-feature-scene3d-webgpu.js.
//
// The page loads client/js/bootstrap.js, the animation chunk and the vessel
// chunk, and builds a four-deck ship out of slabs, wall segments and a crowd of
// skinned agents parented to it. Each state is rendered by one WebGL renderer:
//
//   full         no cutaway
//   cut          cutaway to deck 1, equal to a scene that simply omits what is
//                above deck 1 (the same pixels, exactly)
//   cut, tilted  the same on a rolled, pitched and moved ship
//   fade         decks above are ghosted: more pixels than "cut", fewer than "full"
//   section      the starboard half is sliced away (the cut is open, not capped),
//                equal to a scene built without it within a few edge pixels
//   off          cutaway disabled again, equal to "full"
//
// Exit status 1 if any comparison fails or a render is not "submitted".
import fs from 'node:fs';
import path from 'node:path';
import { createRequire } from 'node:module';
import { fileURLToPath } from 'node:url';

const root = process.env.SCENE3D_JS_DIR || path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..', 'client', 'js');
const toolsDir = process.env.SCENE3D_TOOLS_DIR;
if (!toolsDir) throw new Error('set SCENE3D_TOOLS_DIR to a directory that contains node_modules/playwright');
const toolsRequire = createRequire(import.meta.url);
let chromium;
for (const name of ['playwright', 'playwright-core']) {
  try { ({ chromium } = toolsRequire(path.join(toolsDir, 'node_modules', name))); break; } catch (error) { if (name === 'playwright-core') throw error; }
}
const argv = process.argv.slice(2);
const shadows = argv.includes('shadows');
const backendName = argv.includes('webgpu') ? 'webgpu' : 'webgl';
const outIndex = argv.indexOf('--out');
const outDir = outIndex >= 0 ? argv[outIndex + 1] : '';
if (outDir) fs.mkdirSync(outDir, { recursive: true });
const realProfile = process.env.SCENE3D_REAL_GPU_PROFILE;

let browser, page;
if (realProfile) {
  browser = await chromium.launchPersistentContext(realProfile, {
    executablePath: process.env.SCENE3D_BROWSER, headless: false, viewport: { width: 400, height: 300 },
    args: ['--enable-gpu', '--use-angle=d3d11', '--enable-unsafe-webgpu', '--ignore-gpu-blocklist'],
  });
  page = browser.pages()[0] || await browser.newPage();
} else {
  browser = await chromium.launch({
    executablePath: process.env.SCENE3D_BROWSER || undefined,
    headless: true,
    args: backendName === 'webgpu' ? ['--enable-unsafe-webgpu']
      : ['--use-angle=swiftshader', '--enable-unsafe-swiftshader', '--ignore-gpu-blocklist', '--use-gl=angle'],
  });
  page = await browser.newPage({ viewport: { width: 400, height: 300 } });
}
page.on('pageerror', error => console.error('[pageerror]', error.message));
page.on('console', message => { if (message.type() === 'warning' || message.type() === 'error') console.error('[console]', message.text().slice(0, 400)); });
// WebGPU needs a secure context: serve the page from http://localhost.
await page.route('http://localhost/**', route => route.fulfill({ contentType: 'text/html',
  body: '<!doctype html><html><body style="margin:0"><canvas id="c" width="320" height="240"></canvas></body></html>' }));
await page.goto('http://localhost/');
for (const file of ['bootstrap.js', 'bootstrap-feature-scene3d-animation.js', 'bootstrap-feature-scene3d-vessel.js', ...(backendName === 'webgpu' ? ['bootstrap-feature-scene3d-webgpu.js'] : [])]) {
  await page.addScriptTag({ path: path.join(root, file) });
}
await page.waitForFunction(() => window.__gosx_scene3d_api && window.__gosx_scene3d_animation_api && window.__gosx_scene3d_vessel_cutaway_api, null, { timeout: 10000 });

const { results, images, adapter } = await page.evaluate(async ([withShadows, backendName]) => {
  const api = window.__gosx_scene3d_api;
  const physics = window.__gosx_scene3d_vessel_physics;
  const canvas = document.getElementById('c');
  let adapter = null, renderer;
  if (backendName === 'webgpu') {
    const a = await navigator.gpu.requestAdapter();
    const info = a && (a.info || (a.requestAdapterInfo && await a.requestAdapterInfo()));
    adapter = info ? { vendor: info.vendor, architecture: info.architecture, device: info.device, description: info.description } : null;
    if (!(await window.__gosx_scene3d_webgpu_probe_ready())) throw new Error('WebGPU probe failed: ' + JSON.stringify(window.__gosx_scene3d_webgpu_probe().error));
    const backend = api.sceneBackendRegistry.select({ webgpu: true, preferWebGPU: true, webgl: false, webgl2: false, canvas: false, canvas2d: false });
    renderer = backend.create(canvas, { background: '#101820' }, { tier: 'full' });
    if (!renderer || renderer.kind !== 'webgpu') throw new Error('no WebGPU renderer (got ' + (renderer && renderer.kind) + ')');
  } else {
    const backend = api.sceneBackendRegistry.select({ webgl: true, webgl2: true, webgpu: false, canvas: false, canvas2d: false });
    renderer = backend.create(canvas, { background: '#101820' }, { tier: 'full' });
  }
  // Frames are copied through a 2D canvas in the same task as the render, so both backends read upright pixels.
  const copy = document.createElement('canvas');
  copy.width = 320; copy.height = 240;
  const copyContext = copy.getContext('2d', { willReadFrequently: true });
  const images = [];
  const W = 320, H = 240;

  // ---- geometry ----
  const box = (x0, y0, z0, x1, y1, z1, openEnd = false) => {
    const pos = [], nor = [], uv = [], tan = [];
    const face = (n, a, b, c, d) => {
      for (const p of [a, b, c, a, c, d]) { pos.push(...p); nor.push(...n); uv.push(p[0], p[2]); tan.push(1, 0, 0, 1); }
    };
    face([0, 1, 0], [x0, y1, z0], [x0, y1, z1], [x1, y1, z1], [x1, y1, z0]);
    face([0, -1, 0], [x0, y0, z0], [x1, y0, z0], [x1, y0, z1], [x0, y0, z1]);
    if (!openEnd) face([1, 0, 0], [x1, y0, z0], [x1, y1, z0], [x1, y1, z1], [x1, y0, z1]);
    face([-1, 0, 0], [x0, y0, z0], [x0, y0, z1], [x0, y1, z1], [x0, y1, z0]);
    face([0, 0, 1], [x0, y0, z1], [x1, y0, z1], [x1, y1, z1], [x0, y1, z1]);
    face([0, 0, -1], [x0, y0, z0], [x0, y1, z0], [x1, y1, z0], [x1, y0, z0]);
    return { count: pos.length / 3, immutable: true, revision: 0, positions: new Float32Array(pos), normals: new Float32Array(nor), uvs: new Float32Array(uv), tangents: new Float32Array(tan) };
  };
  const part = (id, geometry, color) => {
    const o = api.normalizeSceneObject({ id, kind: 'mesh', vertices: geometry, materialKind: 'standard', color, roughness: .7 }, 1);
    Object.assign(o, { x: 0, y: 0, z: 0, rotationX: 0, rotationY: 0, rotationZ: 0, spinX: 0, spinY: 0, spinZ: 0, shiftX: 0, shiftY: 0, shiftZ: 0,
      static: false, skin: null, pickable: false, castShadow: withShadows, receiveShadow: withShadows, depthWrite: true, parentMatrix: new Float32Array(16) });
    o.vertices.immutable = true; o.vertices.revision = 0;
    return o;
  };
  const Y = k => 1.5 + 3.3 * k;
  const DECKS = 4;
  const deckConfig = Array.from({ length: DECKS }, (_, k) => ({ y: Y(k), polygons: [[-4, -7, 4, -7, 4, 7, -4, 7]] }));
  const colors = ['#c2b280', '#a0b4c8', '#b88a9a', '#8ab890'];
  const parts = [];
  for (let k = 0; k < DECKS; k++) {
    const slab = part(`ship/slab${k}`, box(-4, Y(k) - .2, -7, 4, Y(k), 7), colors[k]);
    const wallR = part(`ship/wallR${k}`, box(3.9, Y(k), -7, 4, Y(k) + 2.9, 7), '#e8e8e8');
    const wallL = part(`ship/wallL${k}`, box(-4, Y(k), -7, -3.9, Y(k) + 2.9, 7), '#d8d8d8');
    parts.push({ deck: k, side: 0, obj: slab }, { deck: k, side: 1, obj: wallR }, { deck: k, side: -1, obj: wallL });
  }
  const node = part('ship/node', box(0, 0, 0, .01, .01, .01), '#000000');
  node.visible = false;

  // A tapered prism skinned to one joint, as in the crowd parity script.
  const pos = [], nor = [], uv = [], tan = [], joints = [], weights = [];
  const tri = (a, b, c, n) => { for (const p of [a, b, c]) { pos.push(...p); nor.push(...n); uv.push(p[0], p[1]); tan.push(1, 0, 0, 1); joints.push(0, 0, 0, 0); weights.push(1, 0, 0, 0); } };
  const s = .22;
  tri([-s, 0, s], [s, 0, s], [0, 1.6, 0], [0, .3, 1]); tri([s, 0, s], [s, 0, -s], [0, 1.6, 0], [1, .3, 0]);
  tri([s, 0, -s], [-s, 0, -s], [0, 1.6, 0], [0, .3, -1]); tri([-s, 0, -s], [-s, 0, s], [0, 1.6, 0], [-1, .3, 0]);
  const agentGeometry = { count: pos.length / 3, immutable: true, revision: 0, positions: new Float32Array(pos), normals: new Float32Array(nor),
    uvs: new Float32Array(uv), tangents: new Float32Array(tan), joints: new Float32Array(joints), weights: new Float32Array(weights) };
  const width = 4, height = 32, data = new Float32Array(width * height * 4);
  for (let row = 0; row < height; row++) { const o = row * width * 4; data[o] = data[o + 5] = data[o + 10] = data[o + 15] = 1; data[o + 13] = row * .03; }
  const atlas = { id: 'cutaway-atlas', width, height, data, clips: new Map([['Run', { start: 1, segments: 30, duration: 1 }]]), missingClips: new Set() };
  const bounds = { minX: -.3, minY: 0, minZ: -.3, maxX: .3, maxY: 1.8, maxZ: .3 };
  const agents = [];
  for (let k = 0; k < DECKS; k++) {
    for (const [i, [x, z]] of [[-2.4, -3], [-1.2, 2], [1.4, -2], [2.6, 3.5]].entries()) {
      const a = api.normalizeSceneObject({ id: `crowd/d${k}a${i}/0`, kind: 'mesh', vertices: agentGeometry, materialKind: 'standard', color: '#d98c5f', roughness: .6 }, 2);
      Object.assign(a, { x: 0, y: 0, z: 0, rotationX: 0, rotationY: 0, rotationZ: 0, spinX: 0, spinY: 0, spinZ: 0, shiftX: 0, shiftY: 0, shiftZ: 0,
        static: false, skin: null, pickable: false, castShadow: withShadows, receiveShadow: withShadows, depthWrite: true,
        parentMatrix: new Float32Array([1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, x, Y(k), z, 1]) });
      a.vertices.immutable = true; a.vertices.revision = 0;
      a._crowdSkin = { atlas, bounds, rows: new Float32Array([(k * 4 + i) % 20, (k * 4 + i) % 20 + 1, .5]), poseRows: null, batchID: 'crowd', rev: 0, group: null, parent: 'ship' };
      agents.push({ deck: k, side: x > 0 ? 1 : -1, obj: a });
    }
  }
  const ground = withShadows ? part('ground', { count: 6, immutable: true, revision: 0,
    positions: new Float32Array([-14, -.02, 14, 14, -.02, 14, 14, -.02, -14, -14, -.02, 14, 14, -.02, -14, -14, -.02, -14]),
    normals: new Float32Array([0, 1, 0, 0, 1, 0, 0, 1, 0, 0, 1, 0, 0, 1, 0, 0, 1, 0]), uvs: new Float32Array(12),
    tangents: new Float32Array([1, 0, 0, 1, 1, 0, 0, 1, 1, 0, 0, 1, 1, 0, 0, 1, 1, 0, 0, 1, 1, 0, 0, 1]) }, '#808080') : null;
  if (ground) { ground.castShadow = false; ground.receiveShadow = true; ground.parentMatrix = null; ground.shiftY = 0; }

  // ---- cutaway controller, driven the way the vessel mount drives it ----
  const mountStub = { id: 'm', attrs: {}, getAttribute(k) { return this.attrs[k] ?? null; }, setAttribute(k, v) { this.attrs[k] = String(v); } };
  const decks = window.__gosx_scene3d_vessel_decks.create(deckConfig, physics);
  const cw = window.__gosx_scene3d_vessel_cutaway_api.create(decks, mountStub, null, undefined);
  const matrix = new Float32Array(16);
  const shipState = { x: 0, y: 0, z: 0, heading: 0, pitch: 0, roll: 0 };
  const place = pose => {
    Object.assign(shipState, { x: 0, y: 0, z: 0, heading: 0, pitch: 0, roll: 0 }, pose);
    window.__gosx_scene3d_vessel_model.matrix(shipState, physics, matrix);
    decks.update(shipState); cw.update(decks);
    for (const o of [node, ...parts.map(p => p.obj)]) { o.parentMatrix.set(matrix); cw.touch(o); }
  };
  const lights = [{ id: 'sun', kind: 'directional', directionX: .35, directionY: -1, directionZ: -.5, intensity: 1.5, color: '#ffffff', castShadow: withShadows, shadowSize: 1024 }];
  const viewport = { cssWidth: W, cssHeight: H, width: W, height: H };
  const camera = { x: 16, y: 11, z: 17, rotationX: 0, rotationY: 0, fov: 46, near: .1, far: 120 };
  { const t = { x: 0, y: 5.5, z: 0 }; camera.rotationY = Math.atan2(-(t.x - camera.x), -(t.z - camera.z)); camera.rotationX = Math.atan2(t.y - camera.y, Math.hypot(t.x - camera.x, t.z - camera.z)); }
  const build = list => {
    for (const a of agents) if (a.obj._crowdMotion) a.obj._crowdMotion.dirty = true;
    const bundle = api.createSceneRenderBundle(W, H, '#101820', camera, list, [], [], [], [], {}, 0.4, [], [], [], [], [], 0, false,
      { retainedGeometry: true, worldBakedAttributeViews: true, rigidImportedBatches: true, crowdBatches: true });
    bundle.lights = lights;
    return bundle;
  };
  async function draw(list) {
    let status = '';
    // WebGPU compiles pipelines lazily; settle for a few submitted frames before the one that is kept.
    let settled = 0;
    for (let tries = 0; tries < 200; tries++) {
      status = renderer.render(build(list), viewport, { sceneTime: .5 });
      if (status === 'submitted' && ++settled >= (backendName === 'webgpu' ? 4 : 1)) break;
      if (status !== 'pending' && status !== 'submitted') break;
      await new Promise(resolve => setTimeout(resolve, 40));
    }
    copyContext.clearRect(0, 0, W, H);
    copyContext.drawImage(canvas, 0, 0);
    const pixels = new Uint8Array(copyContext.getImageData(0, 0, W, H).data);
    return { status, pixels };
  }
  const lit = pixels => { let n = 0; for (let i = 0; i < pixels.length; i += 4) if (Math.abs(pixels[i] - 0x10) + Math.abs(pixels[i + 1] - 0x18) + Math.abs(pixels[i + 2] - 0x20) > 12) n++; return n; };
  const diff = (a, b) => { let n = 0, max = 0; for (let i = 0; i < a.length; i += 4) { let d = 0; for (let k = 0; k < 3; k++) d = Math.max(d, Math.abs(a[i + k] - b[i + k])); if (d > 0) n++; max = Math.max(max, d); } return { different: n, maxDelta: max }; };
  const all = () => [...(ground ? [ground] : []), node, ...parts.map(p => p.obj), ...agents.map(a => a.obj)];
  const without = predicate => [...(ground ? [ground] : []), node, ...parts.filter(p => !predicate(p)).map(p => p.obj), ...agents.filter(a => !predicate(a)).map(a => a.obj)];
  const out = [];
  const snap = (name, frame) => images.push({ name, url: copy.toDataURL('image/png') });
  const record = (label, a, b, budget, extra = {}) => out.push({ label, status: [a.status, b.status], ...diff(a.pixels, b.pixels), lit: lit(a.pixels), budget, ...extra });

  // Reference scenes cast no shadows from model parts, as under a cutaway.
  const quiet = list => { for (const p of parts) p.obj.castShadow = false; return list; };
  const loud = () => { for (const p of parts) p.obj.castShadow = withShadows; };

  place({});
  const full = await draw(all());
  snap('full', full);
  out.push({ label: 'full ship draws', status: [full.status], different: 0, maxDelta: 0, lit: lit(full.pixels), budget: 0, minLit: 3000 });

  cw.set({ enabled: true, deck: 1 }); place({});
  const cut = await draw(all());
  snap('cut', cut);
  quiet(null); const ref = await draw(without(p => p.deck >= 2)); loud();
  record('cut to deck 1 equals omitting the decks above', cut, ref, 0, { dropsPixels: lit(full.pixels) - lit(cut.pixels) });

  const tilt = { x: 1.5, y: .6, z: -2, heading: .5, pitch: .06, roll: .2 };
  place(tilt);
  const cutTilted = await draw(all());
  snap('cut-tilted', cutTilted);
  quiet(null); const refTilted = await draw(without(p => p.deck >= 2)); loud();
  record('cut follows a rolled, pitched ship', cutTilted, refTilted, 0);

  cw.set({ enabled: true, deck: 1, above: 'fade', fadeAlpha: .3 }); place({});
  const fade = await draw(all());
  snap('fade', fade);
  out.push({ label: 'fade ghosts parts above and still culls agents', status: [fade.status], different: 0, maxDelta: 0, lit: lit(fade.pixels), budget: 0,
    between: [lit(cut.pixels), lit(full.pixels)] });

  cw.set({ enabled: true, deck: 3, section: { axis: 'x', position: 0, side: 1 } }); place({});
  const section = await draw(all());
  snap('section', section);
  // The reference builds the kept half directly: half-width slabs with an open cut end (a clip plane
  // does not cap the section), no right wall, no agents on the right.
  const half = [...(ground ? [ground] : [])];
  const halfParts = [];
  for (let k = 0; k < DECKS; k++) {
    halfParts.push(part(`ship/slab${k}`, box(-4, Y(k) - .2, -7, 0, Y(k), 7, true), colors[k]), part(`ship/wallL${k}`, box(-4, Y(k), -7, -3.9, Y(k) + 2.9, 7), '#d8d8d8'));
  }
  for (const o of halfParts) { o.parentMatrix.set(matrix); o.castShadow = false; }
  half.push(node, ...halfParts, ...agents.filter(a => a.side < 0).map(a => a.obj));
  const sectionRef = await draw(half);
  // A clipped edge is hard; the reference's triangle edge is antialiased. Only edge pixels may differ.
  quiet(null); const sectionBudget = Math.ceil(lit(sectionRef.pixels) * .04);
  record('section removes the starboard half', section, sectionRef, sectionBudget); loud();

  cw.set({ enabled: false }); place({});
  const off = await draw(all());
  snap('off', off);
  record('disabling restores the whole ship', off, full, 0);
  return { results: out, images, adapter };
}, [shadows, backendName]);
await browser.close();
if (adapter) console.log('adapter', JSON.stringify(adapter));
images.forEach((image, index) => {
  if (outDir) fs.writeFileSync(path.join(outDir, `${backendName}-${index + 1}-${image.name}.png`), Buffer.from(image.url.split(',')[1], 'base64'));
});

let failed = false;
for (const r of results) {
  let ok = r.status.every(s => s === 'submitted') && r.different <= r.budget && r.lit > 0;
  if (r.minLit && r.lit < r.minLit) ok = false;
  if (r.between && !(r.lit > r.between[0] && r.lit < r.between[1])) ok = false;
  if (r.dropsPixels !== undefined && r.dropsPixels <= 0) ok = false;
  if (!ok) failed = true;
  console.log(`${ok ? 'ok  ' : 'FAIL'} ${backendName}${shadows ? ' shadows' : ''} ${r.label}: ${r.different} pixels differ (max delta ${r.maxDelta}, budget ${r.budget}), ${r.lit} lit pixels` +
    (r.between ? `, between ${r.between.join(' and ')}` : '') + (r.dropsPixels !== undefined ? `, cut removes ${r.dropsPixels} pixels` : ''));
}
process.exit(failed ? 1 : 0);
