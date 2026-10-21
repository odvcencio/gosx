'use strict';
const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');
const ts = require('../runtime/node_modules/typescript');
const { createWebGLRendererForPost, createBoardWebGPUHarness } = require('./runtime-test-harness.js');
const source = fs.readFileSync(path.join(__dirname, '../runtime/scene3d/model-preparation.ts'), 'utf8');
const code = ts.transpileModule(source, { compilerOptions: { target: ts.ScriptTarget.ES2022 } }).outputText;
const tick = () => new Promise(resolve => setImmediate(resolve));
const deferred = () => { let resolve, reject; const promise = new Promise((yes, no) => { resolve = yes; reject = no; }); return { promise, resolve, reject }; };
function boundary(native) {
  const listeners = new Map(), observers = [], calls = [], pending = new Map(), records = new Map();
  const canvas = native?.canvas || {
    addEventListener(name, callback) { listeners.set(name, callback); },
    removeEventListener(name, callback) { if (listeners.get(name) === callback) listeners.delete(name); },
  };
  if (!canvas.addEventListener) {
    canvas.addEventListener = (name, callback) => listeners.set(name, callback);
    canvas.removeEventListener = (name, callback) => { if (listeners.get(name) === callback) listeners.delete(name); };
    canvas.dispatchEvent = event => { listeners.get(event.type)?.(event); return true; };
  }
  const generation = { disposed: false };
  const renderer = native?.renderer || { texturePreparation: { generation, load(uri, desc, role, colorSpace) {
    calls.push({ uri, desc, role, colorSpace });
    if (!records.has(uri)) records.set(uri, { src: uri, loaded: false, failed: false, generation });
    return records.get(uri);
  } } };
  const scope = { key: 'actual-webgl-scope', ready: Promise.resolve({ backend: 'webgl' }) };
  const mount = { isConnected: true, ownerDocument: {}, __gosxScene3DHandle: {}, __gosxScene3DState: { _crowdRenderer: renderer, _modelTextureVariantScope: scope }, querySelector() { return canvas; } };
  const context = native?.env.context || vm.createContext({ window: {}, DOMException });
  context.DOMException = DOMException;
  context.MutationObserver = class { constructor(callback) { this.callback = callback; observers.push(this); } observe() {} disconnect() { this.closed = true; } };
  const win = context.window || context;
  win.__gosx_scene3d_preload_model = (src, meta) => { calls.push({ src, meta }); if (!pending.has(src)) pending.set(src, deferred()); return pending.get(src).promise; };
  vm.runInContext(code, context, { filename: 'model-preparation.ts' });
  const prepare = (requests = [{ id: 'hero', src: '/hero.glb' }], controller = new AbortController()) => win.__gosx_scene3d_model_preparation_api.prepare(mount, requests, controller.signal);
  const notify = () => listeners.get('gosx:scene3d:resource-ready')?.();
  const material = { texture: '/albedo.png', normalMap: '/normal.png', textureDescriptors: { baseColor: { uri: '/albedo.png', wrapS: 'repeat' } } };
  return { prepare, mount, renderer, canvas, calls, pending, records, scope, observers, listeners, generation, notify, material };
}

test('selected models stay pending until decoded geometry and every native map settle; requests snapshot and scopes match', async () => {
  const h = boundary(), selected = [{ id: 'hero', src: '/hero.glb' }, { id: 'enemy', src: '/enemy.glb' }];
  let finished = false;
  const wait = h.prepare(selected).then(result => { finished = true; return result; });
  selected[0].src = '/changed.glb';
  assert.deepEqual(h.calls.map(call => call.src), ['/hero.glb', '/enemy.glb']);
  assert.ok(h.calls.every(call => call.meta.variantScope === h.scope));
  h.pending.get('/hero.glb').resolve({ objects: [{ material: h.material }], animations: [{ name: 'idle' }] });
  h.pending.get('/enemy.glb').resolve({ objects: [{ material: h.material }] });
  await tick();
  assert.equal(finished, false);
  h.records.get('/albedo.png').loaded = true; h.notify(); await tick();
  assert.equal(finished, false);
  h.records.get('/normal.png').loaded = true; h.notify();
  assert.deepEqual({ ...await wait }, { models: 2, textures: 2 });
  assert.equal(h.listeners.size, 0); assert.ok(h.observers.every(observer => observer.closed));
  assert.equal(h.calls.find(call => call.uri === '/albedo.png').role, 'base-color');
});

test('delayed model failure and material upload failure reject with asset identity', async () => {
  for (const mode of ['model', 'map']) {
    const h = boundary(), wait = h.prepare();
    const rejected = assert.rejects(wait, mode === 'model' ? /hero.*hero.glb/ : /normal.png.*decode failed/);
    h.pending.get('/hero.glb').resolve({ objects: mode === 'model' ? [] : [{ material: h.material }] });
    await tick();
    if (mode === 'map') { Object.assign(h.records.get('/normal.png'), { failed: true, error: 'decode failed' }); h.notify(); }
    await rejected; assert.equal(h.listeners.size, 0); assert.ok(h.observers.every(observer => observer.closed));
  }
});

test('cancel before/during delayed decode prevents uploads and observes late rejection', async () => {
  for (const before of [true, false]) {
    const h = boundary(), controller = new AbortController();
    if (before) controller.abort();
    const wait = h.prepare(undefined, controller), rejected = assert.rejects(wait, /canceled/);
    if (!before) { controller.abort(); h.pending.get('/hero.glb').reject(new Error('late network failure')); }
    await rejected; await tick();
    assert.equal(h.calls.filter(call => call.uri).length, 0); assert.equal(h.listeners.size, 0);
    if (before) assert.equal(h.calls.length, 0);
  }
});

test('mount replacement, island removal, renderer disposal and pending-map cancellation all reject without lingering listeners', async () => {
  for (const kind of ['replacement', 'removal', 'renderer', 'cancel']) {
    const h = boundary(), controller = new AbortController(), wait = h.prepare(undefined, controller);
    const rejected = assert.rejects(wait, /disposed|replaced|canceled/);
    h.pending.get('/hero.glb').resolve({ objects: [{ material: h.material }] }); await tick();
    if (kind === 'replacement') h.mount.__gosxScene3DHandle = {};
    if (kind === 'removal') h.mount.isConnected = false;
    if (kind === 'renderer') h.generation.disposed = true;
    if (kind === 'cancel') controller.abort();
    h.notify();
    await rejected; assert.equal(h.listeners.size, 0); assert.ok(h.observers.every(observer => observer.closed));
  }
});

test('already disposed renderer fails before model fetch and invalid selections do not start work', async () => {
  const h = boundary(); h.generation.disposed = true;
  await assert.rejects(h.prepare(), /disposed/); assert.equal(h.calls.length, 0);
  for (const requests of [[], new Array(33).fill({ id: 'a', src: '/a.glb' }), [{ id: '', src: '/a' }], [{ id: 'a', src: '/a' }, { id: 'a', src: '/b' }]]) {
    assert.throws(() => h.prepare(requests), /preparation/);
  }
});

for (const backend of ['webgl', 'webgpu']) {
  test('preparation uses actual ' + backend + ' image upload/cache and native disposal notification', async () => {
    const native = backend === 'webgl' ? createWebGLRendererForPost({ fresh: true }) : await createBoardWebGPUHarness({ fresh: true });
    const h = boundary(native), images = [];
    native.env.context.Image = class { constructor() { this.width = this.naturalWidth = 2; this.height = this.naturalHeight = 2; images.push(this); } };
    const wait = h.prepare(); let settled = false; wait.then(() => { settled = true; });
    h.pending.get('/hero.glb').resolve({ objects: [{ material: h.material }] }); await tick();
    assert.equal(images.length, 2); assert.equal(settled, false);
    images[0].onload(); await tick(); assert.equal(settled, false);
    images[1].onload();
    assert.deepEqual({ ...await wait }, { models: 1, textures: 2 });
    const cached = await h.prepare(); assert.equal(cached.textures, 2); assert.equal(images.length, 2);
    const pending = h.prepare([{ id: 'delayed', src: '/delayed.glb' }]);
    const rejected = assert.rejects(pending, /disposed/);
    native.renderer.dispose(); await rejected;
    h.pending.get('/delayed.glb').resolve({ objects: [{ material: { texture: '/after-dispose.png' } }] });
    await tick(); assert.equal(images.length, 2);
  });
}

test('preparation is separately gated and has a fixed new-chunk size bound', () => {
  const chunks = JSON.parse(fs.readFileSync(path.join(__dirname, 'bootstrap-src/chunks.json'))).chunks;
  const chunk = chunks.find(chunk => chunk.name === 'bootstrap-feature-scene3d-model-preparation.js');
  assert.deepEqual(chunk.sources, ['../runtime/scene3d/model-preparation.ts']);
  for (const base of ['bootstrap.js', 'bootstrap-feature-scene3d.js', 'bootstrap-feature-scene3d-command.js']) {
    assert.ok(!chunks.find(chunk => chunk.name === base).sources.includes('../runtime/scene3d/model-preparation.ts'));
  }
  for (const [suffix, limit] of [['', 4096], ['.gz', 1536], ['.br', 1280]]) {
    assert.ok(fs.statSync(path.join(__dirname, chunk.name + suffix)).size <= limit, 'new optional chunk budget ' + suffix);
  }
  assert.doesNotMatch(source, /requestAnimationFrame|setInterval|getParameter|getError/);
});


for(const backend of ['webgl','webgpu']) {
 test('opt-in rigid preparation uses '+backend+' scope without submitting nodes or draws',async()=>{
  const native=backend==='webgl'?createWebGLRendererForPost({fresh:true}):await createBoardWebGPUHarness({fresh:true});
  const h=boundary(native),win=native.env.context.window||native.env.context,geometry=[];
  let draws=0;const render=h.renderer.render.bind(h.renderer);
  h.renderer.render=(...args)=>{draws++;return render(...args);};
  const prepareRigid=win.__gosx_scene3d_prepare_rigid_geometry;
  win.__gosx_scene3d_prepare_rigid_geometry=asset=>{geometry.push(asset);prepareRigid(asset);};
  const asset={objects:[{kind:'mesh',vertices:{count:3,positions:[0,0,0,1,0,0,0,1,0],normals:[0,0,1,0,0,1,0,0,1],uvs:[0,0,1,0,0,1]}}],skins:[],animations:[],points:[],labels:[],sprites:[],html:[],lights:[]},selection=[{id:'prop',src:'/prop.glb',rigidGeometry:true}];
  const wait=h.prepare(selection);
  selection[0].rigidGeometry=false;
  h.pending.get('/prop.glb').resolve(asset);
  assert.deepEqual({...await wait},{models:1,textures:0});
  assert.equal(geometry.length,1);assert.equal(geometry[0],asset);assert.equal(draws,0);
  assert.equal(h.mount.__gosxScene3DState.objects,undefined,'no scene member map is created');
  native.renderer.dispose();
 });
}

test('rigid preparation obeys cancellation and renderer replacement before delayed decode',async()=>{
 for(const mode of ['cancel','replace']) {
  const h=boundary(),controller=new AbortController();
  // The optional hook must not be reached after the existing ownership check.
  const wait=h.prepare([{id:'prop',src:'/prop.glb',rigidGeometry:true}],controller);
  const rejected=assert.rejects(wait,/canceled|replaced/);
  if(mode==='cancel')controller.abort();else h.mount.__gosxScene3DState._crowdRenderer={};
  h.pending.get('/prop.glb').resolve({objects:[{material:{}}]});
  await rejected;await tick();
  assert.equal(h.listeners.size,0);assert.equal(h.calls.filter(c=>c.uri).length,0);
 }
});

test('rigid preparation failure is explicit and opt-out leaves it untouched',async()=>{
 const h=boundary();
 const wait=h.prepare([{id:'prop',src:'/prop.glb',rigidGeometry:true}]);
 const rejected=assert.rejects(wait); // missing capability must not silently succeed
 h.pending.get('/prop.glb').resolve({objects:[{material:{}}]});
 await rejected;
 assert.equal(h.listeners.size,0);
 assert.throws(()=>h.prepare([{id:'bad',src:'/bad.glb',rigidGeometry:1}]),/rigid geometry/);
});
