'use strict';
const test=require('node:test'),assert=require('node:assert/strict'),fs=require('node:fs'),path=require('node:path'),vm=require('node:vm');
const context={window:{}};
for(const name of ['vessel-physics','vessel-input','vessel-model','walk-surfaces'])vm.runInNewContext(fs.readFileSync(path.join(__dirname,'../runtime/scene3d',name+'.ts'),'utf8'),context);
const api=context.window.__gosx_scene3d_vessel_physics,touch=context.window.__gosx_scene3d_vessel_input;
const flat=(x,z,t)=>({y:0}),deep=()=>-10,radians=Math.PI/180;
function ship(config={},collision={}) {return api.create({nodeId:'ship',...config},{windDirection:0},collision);}
function sail(s,seconds,input={rudder:0,sail:0},floor=deep) {for(let i=0;i<seconds*60;i++)api.advance(s,1/60,i/60,input,flat,floor);}
const near=(a,b,e=1e-5)=>assert.ok(Math.abs(a-b)<e,`${a} != ${b}`);
test('no-go zone stops drive through 40 degrees and reaches peak on a broad reach',()=>{
 for(const deg of [-40,-20,0,20,40])near(api.speedCurve(deg*radians),0);
 assert.ok(api.speedCurve(90*radians)>.8);near(api.speedCurve(135*radians),1);assert.ok(api.speedCurve(Math.PI)<1);
 const s=ship();s.mode='sailing';s.trim=1;sail(s,8);near(s.speed,0);
});
test('both tacks gather speed and crossing the no-go zone preserves momentum before slowing',()=>{
 for(const heading of [-65,65]) {const s=ship({heading:heading*radians});s.mode='sailing';s.trim=1;sail(s,8);assert.ok(s.speed>4);assert.equal(s.tack,Math.sign(heading));
  const speed=s.speed;s.heading=0;sail(s,.2);assert.ok(s.speed>speed*.6);sail(s,8);assert.ok(s.speed<.1);}
});
test('rudder turns both ways, even when caught in irons; sail raising and lowering is bounded',()=>{
 for(const rudder of [-1,1]) {const s=ship();s.mode='sailing';s.trim=0;sail(s,2,{rudder,sail:1});assert.equal(Math.sign(s.heading),-rudder);assert.ok(Math.abs(s.heading)>.1);assert.ok(s.trim>.7);sail(s,5,{rudder:0,sail:-1});near(s.trim,0);}
});
test('backed canvas pays off from the mooring by 30 degrees in five seconds and gathers speed in ten',()=>{
 for(const rudder of [-1,1]) {
  const s=ship({heading:8*radians,windDirection:8});s.mode='sailing';s.trim=1;
  sail(s,5,{rudder});assert.ok(Math.abs(s.heading-8*radians)>=30*radians);
  sail(s,5,{rudder});assert.ok(s.speed>1,`${s.speed} m/s after ten seconds`);
 }
});
test('buoyancy samples bow, stern and beam; damping settles heave, pitch and roll',()=>{
 const s=ship();s.y=5;s.pitch=.3;s.roll=.3;let count=0;
 const wave=(x,z)=>{count++;return {y:.03*x-.02*z};};
 for(let i=0;i<600;i++)api.advance(s,1/60,i/60,{},wave,deep);
 assert.ok(count>=2400);near(s.y,0,.01);assert.ok(s.pitch>0);assert.ok(s.roll>0);assert.ok(Math.abs(s.vy)<.001);
 s.vx=8;s.mode='deck';s.trim=0;sail(s,8);assert.ok(s.speed<.15);
});
test('shallow seabed gently grounds the vessel and blocks entry without tunnelling',()=>{
 const s=ship({position:{x:0,z:-12},heading:0,length:4,beam:2,draft:1.4});s.mode='sailing';s.trim=1;s.vz=15;
 sail(s,3,{},(x,z)=>z>-1?-.3:-10);assert.ok(s.z<-1.5);assert.ok(s.vz<1);
 const grounded=ship();grounded.mode='sailing';grounded.heading=Math.PI/2;grounded.trim=1;sail(grounded,3,{},()=>-.2);
 assert.equal(grounded.grounded,true);assert.ok(grounded.y>1.15);assert.ok(grounded.speed<.1);
});
test('a grounded hull can turn and retreat toward deeper water without climbing the bank',()=>{
 const s=ship({heading:Math.PI/2,length:4,beam:2});s.mode='sailing';s.trim=1;
 const bank=(x,z)=>-.2+.8*x;
 api.buoyancy(s,1/60,0,flat,bank);assert.equal(s.grounded,true);
 assert.equal(api.allowed(s,-.1,0,s.heading,bank),true);assert.equal(api.allowed(s,.1,0,s.heading,bank),false);
 sail(s,3,{rudder:.3},bank);assert.ok(s.x<-.05);assert.ok(s.heading<Math.PI/2);assert.ok(s.speed<.2);
});
test('bounds and cylinder, sphere, rotated box footprints reject translation and hull rotation',()=>{
 const colliders=[{kind:'cylinder',x:4,z:0,radius:1,height:8,y:-4},{kind:'sphere',x:-4,z:0,radius:1},
 {kind:'box',x:0,z:5,sizeX:4,sizeZ:.5,sizeY:4,rotationY:Math.PI/2}];
 const s=ship({length:4,beam:2},{bounds:{minX:-9,minZ:-9,maxX:9,maxZ:9},colliders});
 assert.equal(api.allowed(s,4,0,0,deep),false);assert.equal(api.allowed(s,-4,0,0,deep),false);assert.equal(api.allowed(s,0,5,0,deep),false);
 assert.equal(api.allowed(s,8,8,0,deep),false);assert.equal(api.allowed(s,0,0,0,deep),true);
 s.mode='sailing';s.vx=30;sail(s,1);assert.ok(s.x<2.5);assert.ok(s.vx<4);
});
test('helm state machine enforces proximity and leaves on deck; V toggles wheel and stern',()=>{
 const s=ship();assert.equal(api.helm(s,{x:100,y:0,z:0}),'far');assert.equal(s.mode,'moored');
 const h=api.localPoint(s,s.helm.x,s.helm.y,s.helm.z);assert.equal(api.helm(s,h),'take');assert.equal(s.mode,'sailing');
 const stern=api.camera(s,{fov:60},1/60);assert.ok(stern.z>s.helm.z);assert.equal(api.toggleCamera(s),'wheel');
 const wheel=api.camera(s,{fov:60},1/60);near(wheel.z,h.z);assert.equal(wheel.fov,65);assert.equal(wheel.near,.05);
 assert.equal(api.helm(s,h),'leave');assert.equal(s.mode,'deck');near(s.trim,0);assert.equal(api.helm(s,h),'take');
});
test('touch steering and sail hold have independent pointer IDs and cancel only their own input',()=>{
 const input=touch.create();assert.equal(touch.down(input,'rudder',11,20),true);assert.equal(touch.down(input,'sail',22,0),true);
 assert.equal(touch.down(input,'rudder',33,40),false);input.sail=1;touch.move(input,11,75);near(touch.value(input).rudder,1);near(touch.value(input).sail,1);
 touch.up(input,22);near(touch.value(input).rudder,1);near(touch.value(input).sail,0);touch.up(input,99);near(touch.value(input).rudder,1);
 touch.up(input,11);near(touch.value(input).rudder,0);input.keys.add('KeyA');input.keys.add('KeyW');near(touch.value(input).rudder,-1);touch.clear(input);near(touch.value(input).sail,0);
});
test('stern camera frames the full clipper rig and hull at the sailing field of view',()=>{
 const s=ship(),camera=api.camera(s,{fov:42},1/60),halfFOV=camera.fov/2*radians;
 for(const [height,z] of [[18.5,-6.3],[21,0],[17,6],[2.5,11],[-1.5,0]]) {
  const p=api.localPoint(s,0,height,z),pitch=Math.atan2(p.y-camera.y,Math.hypot(p.x-camera.x,p.z-camera.z));
  assert.ok(Math.abs(pitch-camera.rotationX)<halfFOV,'mastheads and keel fit in the stern view');
 }
});
test('fixed steps are invariant to render batching and clamp background-tab elapsed time',()=>{
 const a=ship({heading:Math.PI/2}),b=ship({heading:Math.PI/2});a.mode=b.mode='sailing';a.trim=b.trim=1;
 for(let i=0;i<120;i++)api.advance(a,1/60,0,{rudder:.3},flat,deep);
 for(let i=0;i<20;i++)api.advance(b,.1,0,{rudder:.3},flat,deep);
 near(a.x,b.x);near(a.z,b.z);near(a.heading,b.heading);
 const c=ship();c.mode='sailing';api.advance(c,20,0,{sail:1},flat,deep);assert.ok(c.trim<.6);
});
test('walk deck and ramp surfaces preserve terrain and respect yaw and slopes',()=>{
 const api=context.window.__gosx_scene3d_walk_surfaces,s=[{x:0,y:2,z:0,sizeX:2,sizeZ:10,slopeZ:-.1}];
 near(api.height(s,0,-4,-6),2.4);near(api.height(s,3,0,-6),-6);s[0].rotationY=Math.PI/2;near(api.height(s,4,0,-6),1.6);near(api.height(s,4,0,7),7);
});

test('model LODs, cloth trim and wind flag update reusable scene records',()=>{
 const modelAPI=context.window.__gosx_scene3d_vessel_model;
 const vertices=()=>({positions:new Float32Array([-1,4,0,1,4,0,-1,1,0,1,1,0]),normals:new Float32Array(12),uvs:new Float32Array([0,0,1,0,0,1,1,1]),revision:0});
 const canvas={vertices:vertices()},flag={vertices:{...vertices(),positions:new Float32Array([0,21,0,1,21,0,0,20,0,1,20,0])}},low={_modelHidden:true};
 const scene={objects:new Map([['ship/canvas-test',canvas],['ship/wind-flag',flag],['ship-low/hull',low]])};
 const config={nodeId:'ship',lods:[{nodeId:'ship-low',distance:100}]},s=ship({position:{x:12,z:-20},windDirection:0});s.trim=.5;
 const model=modelAPI.create(scene,config,api);model.update(s,{x:12,y:5,z:-20},0,1);
 assert.equal(canvas.visible,true);assert.equal(low.visible,false);assert.ok(canvas.vertices.positions[7]>1);assert.ok(flag.vertices.positions[5]>.9);
 const point=api.localPoint(s,2,3,4),local=modelAPI.inversePoint(model.pose,point);near(local.x,2);near(local.y,3);near(local.z,4);
 model.update(s,{x:62,y:0,z:-20},1,.45);assert.equal(low.visible,true);assert.equal(low._modelHidden,false);assert.equal(canvas.visible,false);
});

test('wake is bounded, follows wave height, uses alpha without depth writes and disposes',()=>{
 vm.runInNewContext(fs.readFileSync(path.join(__dirname,'../runtime/scene3d/vessel-wake.ts'),'utf8'),context);
 const scene={objects:new Map()},helpers={addObject:(s,id,p)=>s.objects.set(id,p)},s=ship();s.mode='sailing';s.speed=5;
 const wake=context.window.__gosx_scene3d_vessel_wake.create(scene,helpers,{nodeId:'ship',wakeTexture:'/foam.png'});
 const wave=(x,z,t)=>({y:.2*Math.sin(x+t)+.1*Math.cos(z)});
 for(let i=0;i<40;i++) {s.z=-i*.8;wake.update(s,i*.2,wave,1);}
 assert.ok(wake.trail.length<=24);const p=wake.vertices.positions;
 for(let i=0;i<p.length;i+=3)near(p[i+1],wave(p[i],p[i+2],7.8).y+.055,1e-5);
 const object=scene.objects.values().next().value;assert.equal(object.depthWrite,false);assert.equal(object.renderPass,'alpha');assert.equal(object.texture,'/foam.png');
 const indices=wake.vertices.indices;
 for(let i=indices.length-12;i<indices.length;i+=3) {
  const [a,b,c]=Array.from(indices.slice(i,i+3),n=>n*3);
  assert.ok((p[b+2]-p[a+2])*(p[c]-p[a])-(p[b]-p[a])*(p[c+2]-p[a+2])>0,'bow foam faces upward on both sides');
 }
 wake.update(s,8,wave,.4);assert.ok(wake.trail.length<=14);wake.reset();assert.equal(wake.trail.length,0);assert.equal(object.visible,false);
 wake.update(s,0,wave,1);assert.equal(wake.trail.length,1);wake.dispose();assert.equal(scene.objects.size,0);
});
