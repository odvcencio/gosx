'use strict';
const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const vm = require('node:vm');
const ts = require('../runtime/node_modules/typescript');
const source = ts.transpileModule(fs.readFileSync(require.resolve('../runtime/scene3d/mount-zoom.ts'), 'utf8'), { compilerOptions: { target: ts.ScriptTarget.ES2022 } }).outputText;
class Target {
  constructor() { this.listeners = []; this.style = {}; this.attrs = {}; }
  addEventListener(name, fn, options) { this.listeners.push({ name, fn, options }); }
  removeEventListener(name, fn, options) { this.listeners = this.listeners.filter(x => x.name !== name || x.fn !== fn || x.options !== options); }
  getAttribute(name) { return this.attrs[name] ?? null; }
  setAttribute(name, value) { this.attrs[name] = value; }
  removeAttribute(name) { delete this.attrs[name]; }
  closest() { return this.host || null; }
  setPointerCapture() {}
  emit(type, props = {}) {
    const e = { type, ...props, preventDefault() { this.defaultPrevented = true; }, stopImmediatePropagation() { this.stopped = true; } };
    for (const x of [...this.listeners].sort((a,b) => Number(!!b.options?.capture)-Number(!!a.options?.capture))) {
      if (x.name === type) x.fn(e);
      if (e.stopped) break;
    }
    return e;
  }
}
function harness(mode = 'first-person', reduced = false, enabled = true) {
  const window = new Target(), canvas = new Target(), document = { activeElement: canvas }, media = new Target();
  window.__gosx_runtime_api = {};
  media.matches = reduced; window.matchMedia = () => media;
  vm.runInNewContext(source, { window, document, Map, Math, Number, Array });
  const api = window.__gosx_runtime_api.scene3DZoom, frames = new Map(), reasons = [];
  let id = 0, now = 0, pose = { x: 1, y: 2, z: 3, fov: 75 }, cancelled = 0, disposed = 0;
  const controller = { mode, touched: false, zoomScale: 1, minDistance: 2, maxDistance: 100, orbit: { radius: 20 }, syncCamera() {},
    currentCamera: () => pose, applyCamera: c => { pose = c; } };
  const base = { controller, cancelTouch: () => cancelled++, reset: () => { pose = { ...pose, fov: 75 }; }, dispose: () => disposed++ };
  const helpers = { read: () => pose, schedule: r => reasons.push(r), now: () => now,
    requestFrame: fn => { frames.set(++id, fn); return id; }, cancelFrame: id => frames.delete(id) };
  if (mode === 'stern' || mode === 'wheel') { base.state = { mode: 'sailing', cameraMode: mode, length: 10 }; pose.fov = 65; }
  api.setup(canvas, { controlZoom: enabled }, base, helpers);
  const flush = () => { for (let i = 0; frames.size && i < 200; i++) { now += 16; const pending = [...frames.values()]; frames.clear(); pending.forEach(fn => fn(now)); } };
  return { api, window, canvas, document, media, base, controller, frames, reasons, helpers, flush, cancelled: () => cancelled, disposed: () => disposed };
}
const near = (a,b) => assert.ok(Math.abs(a-b)<1e-8, `${a} != ${b}`);
test('zoom targets clamp and easing is invariant to frame subdivision', () => {
  const { api } = harness(), a = api.create(75,30,90), b = api.create(75,30,90);
  api.change(a,-100); api.change(b,-100); assert.equal(a.target,30);
  api.advance(a,.1,false); for(let i=0;i<10;i++)api.advance(b,.01,false); near(a.value,b.value);
  assert.ok(a.value>30 && a.value<75); api.advance(a,0,true); assert.equal(a.value,30);
  api.change(a,100); api.advance(a,0,true); assert.equal(a.value,90);
  api.change(a,NaN); assert.equal(a.target,90);
});
test('wheel units, ctrl trackpad pinch and pinch distance preserve natural direction', () => {
  const { api } = harness(); near(api.wheel({deltaY:100,deltaMode:0,ctrlKey:false},800),.1);
  near(api.wheel({deltaY:10,deltaMode:0,ctrlKey:true},800),.1);
  near(api.wheel({deltaY:1,deltaMode:1},800),.016); near(api.wheel({deltaY:1,deltaMode:2},800),.8);
  near(api.distance({clientX:1,clientY:1},{clientX:4,clientY:5}),5);
  near(api.pinch(100,200),-Math.log(2)); assert.equal(api.pinch(0,100),0);
});
test('canvas wheel suppresses scrolling and ctrl page zoom; outside wheel is untouched', () => {
  const h = harness();
  for(const ctrlKey of [false,true]) {
    const event = h.canvas.emit('wheel',{deltaY:-100,deltaMode:0,ctrlKey});
    assert.equal(event.defaultPrevented,true); h.flush();
  }
  assert.equal(h.controller.currentCamera().fov,30);
  assert.equal(h.window.emit('wheel',{deltaY:100}).defaultPrevented,undefined);
  const off = harness('first-person',false,false); assert.equal(off.canvas.listeners.length,0);
});
test('FOV eases, clamps and resets on applying a view or reset; zoom scales navigation', () => {
  const h=harness(); h.canvas.emit('wheel',{deltaY:-1000}); assert.equal(h.controller.currentCamera().fov,75);
  h.flush(); assert.equal(h.controller.currentCamera().fov,30); assert.ok(h.controller.zoomScale<.4);
  h.controller.applyCamera({fov:80}); assert.equal(h.controller.currentCamera().fov,80);
  h.canvas.emit('wheel',{deltaY:1000}); h.flush(); assert.equal(h.controller.currentCamera().fov,90);
  h.base.reset(); assert.equal(h.controller.currentCamera().fov,75); near(h.controller.zoomScale,1);
});
test('orbit and stern dolly clamp their distances while helm changes FOV and resets on V', () => {
  const orbit=harness('orbit',true); orbit.canvas.emit('wheel',{deltaY:-5000}); assert.equal(orbit.controller.orbit.radius,2);
  orbit.canvas.emit('wheel',{deltaY:5000}); orbit.canvas.emit('wheel',{deltaY:5000}); assert.equal(orbit.controller.orbit.radius,100);
  const ship=harness('stern',true); ship.canvas.emit('wheel',{deltaY:-5000}); assert.equal(ship.base.state.followDistance,8.1);
  ship.base.state.cameraMode='wheel'; assert.equal(ship.controller.currentCamera().fov,65);
  ship.canvas.emit('wheel',{deltaY:-5000}); assert.equal(ship.controller.currentCamera().fov,30);
  ship.base.state.cameraMode='stern'; ship.controller.currentCamera(); assert.equal(ship.base.state.followDistance,18);
  ship.base.state.cameraMode='wheel'; assert.equal(ship.controller.currentCamera().fov,65);
});
test('focused + = - _ keys zoom, reduced motion updates immediately and page keys remain available', () => {
  const h=harness('first-person',true); h.document.activeElement=null;
  assert.equal(h.canvas.emit('keydown',{key:'+'}).defaultPrevented,undefined); h.document.activeElement=h.canvas;
  for(const key of ['+','='])assert.equal(h.canvas.emit('keydown',{key}).defaultPrevented,true);
  assert.ok(h.controller.currentCamera().fov<75);
  for(const key of ['-','_'])h.canvas.emit('keydown',{key}); near(h.controller.currentCamera().fov,75);
  assert.equal(h.frames.size,0);
});
test('pinch cancels single finger controls and suppresses look until both fingers lift', () => {
  const h=harness('first-person',true), touch={pointerType:'touch',clientY:10};
  const first=h.canvas.emit('pointerdown',{...touch,pointerId:1,clientX:10}); assert.equal(first.defaultPrevented,undefined);
  h.canvas.emit('pointerdown',{...touch,pointerId:2,clientX:110}); assert.equal(h.cancelled(),1);
  h.canvas.emit('pointermove',{...touch,pointerId:2,clientX:210}); near(h.controller.currentCamera().fov,37.5);
  h.canvas.emit('pointerup',{...touch,pointerId:2,clientX:210});
  assert.equal(h.canvas.emit('pointermove',{...touch,pointerId:1,clientX:30}).stopped,true);
  h.canvas.emit('pointercancel',{...touch,pointerId:1});
  assert.equal(h.canvas.emit('pointerdown',{...touch,pointerId:3,clientX:20}).stopped,undefined);
});
test('active render loop keeps its budget; reduced-motion changes settle and dispose removes listeners and frames', () => {
  const h=harness(); h.canvas.style.touchAction='none';
  h.canvas.host={getAttribute:name=>name.endsWith('wants-animation')?'true':'active'};
  h.canvas.emit('wheel',{deltaY:-1000}); h.flush(); assert.equal(h.reasons.length,0);
  h.canvas.emit('wheel',{deltaY:5000}); h.media.matches=true; h.media.emit('change'); assert.equal(h.controller.currentCamera().fov,90);
  h.base.dispose(); assert.equal(h.frames.size,0); assert.equal(h.canvas.listeners.length,0);
  assert.equal(h.window.listeners.length,0); assert.equal(h.media.listeners.length,0); assert.equal(h.disposed(),1);
  assert.equal(h.canvas.getAttribute('aria-keyshortcuts'),null); assert.equal(h.controller.zoomScale,1);
  assert.equal(h.canvas.emit('wheel',{deltaY:100}).defaultPrevented,undefined);
});

test('portrait framing provides the default FOV and preserves optical zoom through viewport adaptation', () => {
  const h=harness('first-person',true); h.helpers.fov=()=>70;
  assert.equal(h.controller.currentCamera().fov,70);
  h.canvas.emit('wheel',{deltaY:-6000}); const zoomed=h.controller.currentCamera(); assert.equal(zoomed.fov,30);
  const core=fs.readFileSync(require.resolve('./bootstrap-src/10-runtime-scene-core.ts'),'utf8');
  const start=core.indexOf('  function sceneViewportCamera('),end=core.indexOf('  function normalizeSceneTextureDescriptor(',start);
  const env={sceneNumber:(v,f)=>v??f}; vm.runInNewContext(core.slice(start,end),env);
  assert.equal(env.sceneViewportCamera(zoomed,{portraitFOV:70},{cssWidth:390,cssHeight:844}).fov,30);
  let adapter;
  env.window={__gosx_runtime_api:{scene3DZoom:{setup:(_canvas,_props,_base,helpers)=>{adapter=helpers;}}}};
  env.sceneMotionRequestFrame=()=>{}; env.sceneMotionCancelFrame=()=>{}; env.sceneNowMilliseconds=()=>0;
  vm.runInNewContext(fs.readFileSync(require.resolve('../runtime/scene3d/mount-controls.ts'),'utf8'),env);
  env.setupSceneBaseControls=()=>({controller:{currentCamera:()=>({fov:42}),syncCamera(){}}}); env.sceneVesselEnabled=()=>false;
  env.setupSceneBuiltInControls({}, {controlZoom:true},()=>({cssWidth:390,cssHeight:844}),()=>({fov:42}),()=>{}, {camera:{fov:42,portraitFOV:70}});
  assert.equal(adapter.fov({fov:42}),70);
  assert.equal(env.sceneViewportCamera({fov:42},{portraitFOV:70},{cssWidth:390,cssHeight:844}).fov,70);
  near(h.controller.zoomScale,h.api.scale(30,70));
  h.helpers.fov=adapter.fov; h.base.reset(); assert.equal(h.controller.currentCamera().fov,70);
});
