'use strict';
const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const vm = require('node:vm');
const ts = require('../runtime/node_modules/typescript');
const source = ts.transpileModule(['mount-pan.ts', 'mount-zoom.ts'].map(name => fs.readFileSync(require.resolve('../runtime/scene3d/'+name), 'utf8')).join('\n'), { compilerOptions: { target: ts.ScriptTarget.ES2022 } }).outputText;
class Target {
  constructor() { this.listeners = []; this.style = {}; this.attrs = {}; }
  addEventListener(name, fn, options) { this.listeners.push({ name, fn, options }); }
  removeEventListener(name, fn, options) { this.listeners = this.listeners.filter(x => x.name !== name || x.fn !== fn || x.options !== options); }
  getAttribute(name) { return this.attrs[name] ?? null; }
  setAttribute(name, value) { this.attrs[name] = value; }
  removeAttribute(name) { delete this.attrs[name]; }
  closest() { return this.host || null; }
  setPointerCapture() {}
  dispatchEvent(e) { this.emit(e.type, e); return true; }
  emit(type, props = {}) {
    const e = { type, target: this, ...props, preventDefault() { this.defaultPrevented = true; }, stopImmediatePropagation() { this.stopped = true; } };
    for (const x of [...this.listeners].sort((a,b) => Number(!!b.options?.capture)-Number(!!a.options?.capture))) {
      if (x.name === type) x.fn(e);
      if (e.stopped) break;
    }
    return e;
  }
}
function harness(mode = 'first-person', reduced = false, enabled = true, options = {}) {
  const window = new Target(), canvas = new Target(), handle = new Target(), document = { activeElement: canvas, querySelector: () => handle }, media = new Target();
  window.__gosx_runtime_api = {};
  media.matches = reduced; window.matchMedia = () => media;
  vm.runInNewContext(source, { window, document, Map, Math, Number, Array, CustomEvent: class { constructor(type, options) { this.type=type; Object.assign(this,options); } } });
  const api = window.__gosx_runtime_api.scene3DZoom, frames = new Map(), reasons = [], subscribers=new Map(), signals=new Map();
  let id = 0, now = 0, pose = { x: 1, y: 2, z: 3, fov: 75 }, cancelled = 0, disposed = 0;
  const controller = { mode, target: {x:0,y:0,z:0}, touched: false, zoomScale: 1, minDistance: 2, maxDistance: 100, orbit: { radius: 20 }, syncCamera() {},
    currentCamera: () => pose, applyCamera: c => { pose = c; } };
  const base = { controller, cancelTouch: () => cancelled++, reset: () => { pose = { ...pose, fov: 75 }; }, dispose: () => disposed++ };
  const helpers = { read: () => pose, pan: d => { pose = {...pose, x:pose.x+d.x, y:pose.y+d.y, z:pose.z+d.z}; }, schedule: r => reasons.push(r), now: () => now,
    subscribe: (name,fn) => {subscribers.set(name,fn);return ()=>subscribers.delete(name);},
    publish: (name,value) => {signals.set(name,value);subscribers.get(name)?.(value);},
    requestFrame: fn => { frames.set(++id, fn); return id; }, cancelFrame: id => frames.delete(id) };
  if (mode === 'stern' || mode === 'wheel') { base.state = { mode: 'sailing', cameraMode: mode, length: 10 }; pose.fov = 65; }
  api.setup(canvas, { controlZoom: enabled, ...options }, base, helpers);
  const flush = () => { for (let i = 0; frames.size && i < 200; i++) { now += 16; const pending = [...frames.values()]; frames.clear(); pending.forEach(fn => fn(now)); } };
  return { api, window, canvas, handle, document, media, base, controller, frames, reasons, helpers, signals, subscribers, setSignal:(name,value)=>helpers.publish(name,value), flush, cancelled: () => cancelled, disposed: () => disposed };
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

test('the lazy zoom loader finds the controller in the runtime API', () => {
 const source = ts.transpileModule(fs.readFileSync(require.resolve('../runtime/scene3d/script-loader.ts'), 'utf8'), { compilerOptions: { target: ts.ScriptTarget.ES2022 } }).outputText;
 const api = { setup() {} }, env = { window: { __gosx_runtime_api: { scene3DZoom: api } } };
 vm.runInNewContext(source, env);
 assert.equal(env.sceneGatedFeatureAPI('zoom'), api);
});

test('optical lens magnifies without moving or detaching a sailing camera and restores the prior zoom', () => {
  for (const mode of ['first-person','orbit','stern','wheel']) {
    const h=harness(mode,true), before=h.controller.currentCamera(), navigation=h.controller.zoomScale;
    h.canvas.emit('gosx:scene3d:optical-zoom',{detail:{factor:3}});
    const after=h.controller.currentCamera(); near(Math.tan(after.fov*Math.PI/360),Math.tan(before.fov*Math.PI/360)/3);
    for(const key of ['x','y','z'])assert.equal(after[key],before[key]);
    assert.equal(h.controller.zoomScale,navigation);assert.equal(h.frames.size,0,'lens uses the existing render scheduler');
    if(h.base.state)assert.equal(h.base.state.mode,'sailing');
    h.canvas.emit('gosx:scene3d:optical-zoom',{detail:{factor:1}});assert.equal(h.controller.currentCamera().fov,before.fov);
    h.canvas.emit('gosx:scene3d:optical-zoom',{detail:{factor:3}});h.controller.applyCamera({fov:70});assert.equal(h.controller.currentCamera().fov,70);
    h.base.dispose();assert.equal(h.canvas.listeners.length,0);
  }
});

test('optical lens honors portrait FOV, rejects malformed and foreign events, and resets with the scene', () => {
  const h=harness('stern',true);h.helpers.fov=c=>c._gosxZoomFOV?c.fov:70;
  h.canvas.emit('gosx:scene3d:optical-zoom',{detail:{factor:3}});
  const expected=360/Math.PI*Math.atan(Math.tan(70*Math.PI/360)/3);near(h.controller.currentCamera().fov,expected);
  for(const factor of [NaN,Infinity,0,9,'3',null]) {h.canvas.emit('gosx:scene3d:optical-zoom',{detail:{factor}});near(h.controller.currentCamera().fov,expected);}
  h.canvas.emit('gosx:scene3d:optical-zoom',{target:{},detail:{factor:1}});near(h.controller.currentCamera().fov,expected);
  let resets=0;h.canvas.addEventListener('gosx:scene3d:optical-zoom-state',e=>{if(e.detail.factor===1)resets++;});
  h.base.reset();assert.equal(h.controller.currentCamera().fov,75);assert.equal(resets,1);
});

test('Go-authored optics uses shared signals for magnification, readings, escape and disposal',()=>{
 const input='$survey.optics',output='$survey.reading';
 const h=harness('wheel',false,true,{optics:{inputSignal:input,outputSignal:output,magnification:3,landmarks:[{id:'north',name:'North isle',position:{x:1,z:-100},landing:true}]}});
 h.controller.currentCamera();assert.equal(h.signals.get(output).Ready,true);
 h.setSignal(input,true);const c=h.controller.currentCamera(),reading=h.signals.get(output);
 near(Math.tan(c.fov*Math.PI/360),Math.tan(65*Math.PI/360)/3);
 assert.equal(reading.Enabled,true);assert.equal(reading.LandmarkID,'north');assert.equal(reading.CanNavigate,true);
 assert.equal(h.base.state.mode,'sailing');assert.equal(h.frames.size,0);
 h.canvas.emit('keydown',{key:'Escape',stopPropagation(){}});assert.equal(h.signals.get(input),false);assert.equal(h.controller.currentCamera().fov,65);
 h.setSignal(input,true);h.base.reset();assert.equal(h.signals.get(input),false);
 h.base.dispose();assert.equal(h.subscribers.size,0);assert.equal(h.canvas.listeners.length,0);
});

test('an optics-only fixed viewer keeps ordinary wheel and touch gestures untouched',()=>{
 const h=harness('fixed',false,false,{optics:{inputSignal:'$lens',magnification:3}});
 h.setSignal('$lens',true);assert.ok(h.controller.currentCamera().fov<30);
 assert.equal(h.canvas.emit('wheel',{deltaY:100}).defaultPrevented,undefined);
 assert.equal(h.canvas.emit('pointerdown',{pointerType:'touch',pointerId:1}).defaultPrevented,undefined);
 h.setSignal('$lens',false);assert.equal(h.controller.currentCamera().fov,75);h.base.dispose();
});


test('fixed-camera zoom limits are explicit and two-finger pan preserves angle', () => {
  const h=harness('fixed',true,true,{controlPan:true,controlZoomMinFOV:15,controlZoomMaxFOV:75});
  h.canvas.emit('wheel',{deltaY:-6000}); assert.equal(h.controller.currentCamera().fov,15);
  const before=h.controller.currentCamera();
  const touch=(type,id,x,y)=>h.canvas.emit(type,{pointerType:'touch',pointerId:id,clientX:x,clientY:y});
  assert.equal(touch('pointerdown',1,100,100).stopped,undefined,'one finger stays available to object interaction');
  assert.equal(touch('pointerdown',2,200,100).stopped,true);
  touch('pointermove',1,120,140); touch('pointermove',2,220,140);
  const after=h.controller.currentCamera(); assert.notEqual(after.x,before.x); assert.notEqual(after.y,before.y);
  assert.equal(after.rotationX,before.rotationX); assert.equal(after.rotationY,before.rotationY);
  touch('pointerup',1,120,140);touch('pointerup',2,220,140);
  assert.equal(touch('pointerdown',3,100,100).stopped,undefined,'gesture releases single-finger interactions');
});

test('pan handle supports capture, keyboard, cancellation and disposal', () => {
  const h=harness('fixed',true,true,{controlPan:true,controlPanHandle:'#pan'});
  const before=h.controller.currentCamera().x;
  h.handle.emit('pointerdown',{pointerId:9,button:0,isPrimary:true,clientX:10,clientY:20});
  h.handle.emit('pointermove',{pointerId:9,clientX:40,clientY:20});
  assert.ok(h.controller.currentCamera().x<before);
  h.handle.emit('pointercancel',{pointerId:9});
  const canceled=h.controller.currentCamera().x;
  h.handle.emit('pointermove',{pointerId:9,clientX:400,clientY:20}); assert.equal(h.controller.currentCamera().x,canceled);
  const key=h.handle.emit('keydown',{key:'ArrowLeft',stopPropagation(){}}); assert.equal(key.defaultPrevented,true);
  assert.ok(h.controller.currentCamera().x<canceled);
  h.base.dispose(); assert.equal(h.handle.listeners.length,0);
  assert.equal(h.window.listeners.length,0); assert.equal(h.frames.size,0);
});

test('screen-space pan follows pitched and orthographic cameras without changing pose', () => {
  const {api}=harness();
  const camera={kind:'perspective',x:0,y:10,z:0,fov:60,rotationX:-Math.PI/2};
  const d=api.panDelta(camera,{x:0,y:0,z:0},0,100,1000);
  near(d.x,0);near(d.y,0);near(d.z,-2*Math.tan(Math.PI/6));
  const ortho={kind:'orthographic',x:0,y:0,z:10,top:5,bottom:-5,zoom:2};
  const o=api.panDelta(ortho,{x:0,y:0,z:0},100,0,1000);near(o.x,-.5);near(o.y,0);near(o.z,0);
});


test('opt-in navigation creates a fixed controller without enabling orbit drag', () => {
 const source=fs.readFileSync(require.resolve('../runtime/scene3d/mount-controls.ts'),'utf8');
 let adapter, sourceCamera={kind:'perspective',x:0,y:5,z:4,fov:40,rotationX:-.8};
 const env={window:{__gosx_runtime_api:{scene3DZoom:{setup:(_canvas,_props,base,helpers)=>{adapter={base,helpers};return base;}}}},
  sceneNumber:(v,f)=>v??f,sceneRenderCamera:c=>({...c}),sceneControlsTarget:()=>({x:0,y:0,z:0}),sceneMotionRequestFrame(){},sceneMotionCancelFrame(){},sceneNowMilliseconds:()=>0,
  sceneViewportCamera:c=>c};
 vm.runInNewContext(source,env);env.setupSceneBaseControls=()=>({controller:null,dispose(){}});env.sceneVesselEnabled=()=>false;
 const base=env.setupSceneBuiltInControls({}, {controlZoom:true,controlPan:true},()=>({cssWidth:640,cssHeight:360}),()=>sourceCamera,()=>{}, {camera:sourceCamera});
 assert.equal(base.controller.mode,'fixed');assert.equal(adapter.helpers.read().rotationX,-.8);
 adapter.helpers.pan({x:2,y:0,z:1});assert.equal(adapter.helpers.read().x,2);assert.equal(adapter.helpers.read().rotationX,-.8);
 base.controller.applyCamera({...sourceCamera,x:0});assert.equal(adapter.helpers.read().x,0,'switching views resets the pan');
});
