'use strict';
const test=require('node:test'),assert=require('node:assert/strict'),fs=require('node:fs'),path=require('node:path'),vm=require('node:vm');
const context={window:{}};
for(const name of ['ocean-waves','ocean-query','vessel-navigation','vessel-physics','vessel-input','vessel-model','walk-surfaces'])vm.runInNewContext(fs.readFileSync(path.join(__dirname,'../runtime/scene3d',name+'.ts'),'utf8'),context);
const api=context.window.__gosx_scene3d_vessel_physics,touch=context.window.__gosx_scene3d_vessel_input;
const flat=(x,z,t)=>({y:0}),deep=()=>-10,radians=Math.PI/180;
function ship(config={},collision={}) {return api.create({nodeId:'ship',...config},{windDirection:0},collision);}
function sail(s,seconds,input={rudder:0,sail:0},floor=deep) {for(let i=0;i<seconds*60;i++)api.advance(s,1/60,i/60,input,flat,floor);}
const near=(a,b,e=1e-5)=>assert.ok(Math.abs(a-b)<e,`${a} != ${b}`);
test('course hold turns out of irons and converges without teleporting the vessel',()=>{
 const s=ship({navigation:true,heading:8*radians,windDirection:8});s.mode='sailing';s.trim=1;
 assert.equal(context.window.__gosx_scene3d_vessel_navigation.holdCourse(s,60),true);const initial=s.heading;sail(s,30);
 near(s.heading,-60*radians,.015);assert.ok(Math.abs(s.heading-initial)>.7);assert.ok(s.speed>4&&s.distance>100);
 assert.ok(s.x>50&&s.z<-40);
});
test('course hold takes the short route across the yaw wrap and yields immediately to manual steering',()=>{
 const s=ship({navigation:true,heading:179*radians});s.mode='sailing';s.trim=1;
 assert.equal(context.window.__gosx_scene3d_vessel_navigation.holdCourse(s,179),true);sail(s,15);near(s.heading,-179*radians,.02);
 sail(s,.1,{rudder:1});assert.ok(Number.isNaN(s.course));
 assert.equal(context.window.__gosx_scene3d_vessel_navigation.holdCourse(s,60),true);api.helm(s,{});assert.ok(Number.isNaN(s.course));
});
test('navigation is opt-in and rejects non-finite or out-of-range courses',()=>{
 const disabled=ship();disabled.mode='sailing';assert.equal(context.window.__gosx_scene3d_vessel_navigation.holdCourse(disabled,60),false);
 const s=ship({navigation:true});assert.equal(context.window.__gosx_scene3d_vessel_navigation.holdCourse(s,60),false);s.mode='sailing';
 for(const bearing of [NaN,Infinity,-1,361,'60',null])assert.equal(context.window.__gosx_scene3d_vessel_navigation.holdCourse(s,bearing),false);
 assert.equal(context.window.__gosx_scene3d_vessel_navigation.holdCourse(s,0),true);near(s.course,0);
});
test('course-held voyages retain real collision constraints and record only accepted travel',()=>{
 const s=ship({navigation:true,heading:Math.PI,length:4,beam:2,position:{x:0,z:-12}});s.mode='sailing';s.trim=1;
 context.window.__gosx_scene3d_vessel_navigation.holdCourse(s,180);sail(s,20,{},(x,z)=>z>-1?-.3:-10);
 assert.ok(s.z<-1.5);assert.ok(s.distance<12);assert.ok(Number.isFinite(s.speed));
});
test('boarding from a distance requires opt-in and an explicit button action; E retains proximity',()=>{
 const far={x:80,y:4,z:80},plain=ship();
 assert.equal(api.helm(plain,far,true),'far');assert.equal(plain.mode,'moored');
 const quick=ship({quickBoard:true});
 assert.equal(api.helm(quick,far),'far');assert.equal(quick.mode,'moored');
 assert.equal(api.helm(quick,far,true),'take');assert.equal(quick.mode,'sailing');
 assert.equal(api.helm(quick,far,true),'leave');assert.equal(quick.mode,'deck');
});
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
test('the recorded W hold and short D hold clear irons and continue sailing after key release',()=>{
 const s=ship({heading:8*radians,windDirection:8});s.mode='sailing';
 sail(s,1.5,{sail:1});sail(s,2.5,{rudder:1});sail(s,8);
 assert.ok(api.speedCurve(s.heading-s.wind)>0,'the bow must leave the no-go zone');
 assert.ok(s.speed>1,`${s.speed} m/s after the recorded input sequence`);
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
test('open water crosses walk bounds on every side, preserves real hazards and honors explicit vessel bounds',()=>{
 const bounds={minX:-115,minZ:-180,maxX:115,maxZ:46};
 const collision={bounds,colliders:[{kind:'cylinder',x:4,z:0,radius:2,height:8,y:-4}]};
 const bounded=ship({},collision),open=ship({openWater:true},collision);
 for(const [x,z] of [[-140,-210],[140,-210],[0,-3000],[3000,1000]]) {
  assert.equal(api.allowed(bounded,x,z,0,deep),false);
  assert.equal(api.allowed(open,x,z,0,deep),true);
 }
 assert.equal(api.allowed(open,4,0,0,deep),false,'real rock collision is retained');
 assert.equal(api.allowed(open,0,0,0,()=>-.1),false,'a shallow beach still grounds the hull');
 const explicit=ship({openWater:true,bounds},collision);
 assert.equal(api.allowed(explicit,0,-3000,0,deep),false,'explicit vessel bounds take precedence');
 const voyage=ship({openWater:true,position:{x:20,z:-140},heading:-Math.PI/3},collision);
 voyage.mode='sailing';voyage.trim=1;sail(voyage,600);
 assert.ok(voyage.x>2000 && voyage.z<-1000,'ten-minute voyage crosses the former scene edges: '+JSON.stringify({x:voyage.x,z:voyage.z,speed:voyage.speed}));
 assert.ok(voyage.speed>4 && !voyage.grounded);
});
test('helm state machine enforces proximity and leaves on deck; V toggles wheel and stern',()=>{
 const s=ship();assert.equal(api.helm(s,{x:100,y:0,z:0}),'far');assert.equal(s.mode,'moored');
 const h=api.localPoint(s,s.helm.x,s.helm.y,s.helm.z);assert.equal(api.helm(s,h),'take');assert.equal(s.mode,'sailing');
 const stern=api.camera(s,{fov:60},1/60);assert.ok(stern.z>s.helm.z);assert.equal(api.toggleCamera(s),'wheel');
 const wheel=api.camera(s,{fov:60},1/60);near(wheel.z,h.z);assert.equal(wheel.fov,65);assert.equal(wheel.near,.05);
 assert.equal(api.helm(s,h),'leave');assert.equal(s.mode,'deck');near(s.trim,0);assert.equal(api.helm(s,h),'take');
});

test('wheel free look and chase orbit are bounded camera offsets, independent of sailing',()=>{
 const s=ship({navigation:true});s.mode='sailing';s.trim=1;s.cameraMode='wheel';
 context.window.__gosx_scene3d_vessel_navigation.holdCourse(s,60);
 const neutral=api.camera(s,{},.1),course=s.course;api.look(s,1,.3);const looked=api.camera(s,{},.1);
 near(looked.x,neutral.x);near(looked.z,neutral.z);near(looked.rotationY-neutral.rotationY,1);near(looked.rotationX-neutral.rotationX,.3);
 near(s.heading,0);near(s.trim,1);near(s.course,course);
 const yaw=s.lookYaw,pitch=s.lookPitch;api.look(s,Infinity,0);api.look(s,0,NaN);near(s.lookYaw,yaw);near(s.lookPitch,pitch);
 api.look(s,Math.PI*20,20);assert.ok(Math.abs(s.lookYaw)<=Math.PI);assert.equal(s.lookPitch,1.25);
 s.cameraMode='stern';s.lookYaw=s.lookPitch=0;s.cameraReady=false;const aft=api.camera(s,{},.1);
 api.look(s,Math.PI/2,0);s.cameraReady=false;const side=api.camera(s,{},.1);assert.ok(Math.hypot(aft.x-side.x,aft.z-side.z)>30);
 near(s.heading,0);near(s.course,course);
});
test('touch steering and sail hold have independent pointer IDs and cancel only their own input',()=>{
 const input=touch.create();assert.equal(touch.down(input,'rudder',11,20),true);assert.equal(touch.down(input,'sail',22,0),true);
 assert.equal(touch.down(input,'rudder',33,40),false);input.sail=1;touch.move(input,11,75);near(touch.value(input).rudder,1);near(touch.value(input).sail,1);
 touch.up(input,22);near(touch.value(input).rudder,1);near(touch.value(input).sail,0);touch.up(input,99);near(touch.value(input).rudder,1);
 touch.up(input,11);near(touch.value(input).rudder,0);input.keys.add('KeyA');input.keys.add('KeyW');near(touch.value(input).rudder,-1);touch.clear(input);near(touch.value(input).sail,0);
});
test('small thumb movements stay centered and retain course hold until an intentional steer',()=>{
 const s=ship({navigation:true}),input=touch.create();s.mode='sailing';
 context.window.__gosx_scene3d_vessel_navigation.holdCourse(s,60);const course=s.course;
 touch.down(input,'rudder',11,100);
 for(const dx of [-3,-1,0,2,3]) {touch.move(input,11,100+dx);near(touch.value(input).rudder,0);context.window.__gosx_scene3d_vessel_navigation.rudder(s,touch.value(input));near(s.course,course);}
 touch.move(input,11,127.5);near(touch.value(input).rudder,.5);context.window.__gosx_scene3d_vessel_navigation.rudder(s,touch.value(input));assert.ok(Number.isNaN(s.course));
 touch.up(input,11);near(touch.value(input).rudder,0);
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
test('render poses advance between physics ticks without changing the voyage or allocating another pose',()=>{
 const s=ship({heading:Math.PI/2});s.mode='sailing';s.trim=1;s.vx=-8.5;s.vz=.2/1.4;
 const pose=api.presentation(s),positions=[],authoritative=[];
 for(let i=1;i<=120;i++) {
  api.advance(s,1/120,i/120,{},flat,deep);
  assert.equal(api.presentation(s),pose);positions.push(pose.x);authoritative.push(s.x);
 }
 assert.ok(authoritative.slice(1).some((x,i)=>x===authoritative[i]),'fixed physics remains 60 Hz');
 for(let i=2;i<positions.length;i++)near(positions[i]-positions[i-1],-8.5/120,1e-6);
 const control=ship({heading:Math.PI/2});control.mode='sailing';control.trim=1;control.vx=-8.5;control.vz=.2/1.4;
 for(let i=1;i<=60;i++)api.advance(control,1/60,i/60,{},flat,deep);
 for(const key of ['x','z','heading','distance','speed'])near(s[key],control[key],1e-8);
 s.cameraMode='wheel';s.reduced=true;const h=api.localPoint(pose,s.helm.x,s.helm.y,s.helm.z),camera=api.camera(s,{},1/120,1,pose);
 near(camera.x,h.x);near(camera.y,h.y);near(camera.z,h.z);
 const model=context.window.__gosx_scene3d_vessel_model.create({objects:new Map()},{nodeId:'ship'},api);
 model.update(pose,camera,1,1);near(model.pose[12],pose.x);near(model.pose[14],pose.z);
 const before={x:s.x,z:s.z,heading:s.heading};api.look(s,.2,.1);near(pose.lookYaw,.2);assert.deepEqual({x:s.x,z:s.z,heading:s.heading},before);
});
test('render interpolation takes the short heading wrap and reset snaps instead of crossing the world',()=>{
 const s=ship({heading:Math.PI-.001});s.mode='sailing';s.yawVelocity=.2;
 const before=s.heading;api.advance(s,1/40,1/40,{rudder:-1},flat,deep);
 assert.ok(s.heading<0,'the physics heading crossed the wrap');const pose=api.presentation(s);
 assert.ok(Math.abs(pose.heading)>3,'presentation cannot spin through zero');
 near(pose.heading,before+Math.atan2(Math.sin(s.heading-before),Math.cos(s.heading-before))*.5);
 s.x=2500;s.z=-3700;s.accumulator=0;const fresh=api.presentation(s,true);near(fresh.x,s.x);near(fresh.z,s.z);near(fresh.heading,s.heading);
 api.advance(s,0,1,{},flat,deep);assert.equal(api.presentation(s),fresh);
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
 for(let i=0;i<240;i+=4) {
  if(p[i*3]===p[(i+1)*3]&&p[i*3+2]===p[(i+1)*3+2])continue;
  const center=[0,0,0];for(let j=0;j<4;j++)for(let k=0;k<3;k++)center[k]+=p[(i+j)*3+k]/4;
  near(center[1],wave(center[0],center[2],7.8).y+.065,1e-5,'each small foam patch follows its water sample');
 }
 for(let i=240*3;i<p.length;i+=3)near(p[i+1],wave(p[i],p[i+2],7.8).y+.055,1e-5);
 const object=scene.objects.values().next().value;assert.equal(object.depthWrite,false);assert.equal(object.renderPass,'alpha');assert.equal(object.texture,'/foam.png');
 assert.equal(object.wireframe,false,'foam uses textured triangles');assert.equal(wake.vertices.indices.BYTES_PER_ELEMENT,4,'the indexed mesh pipeline requires uint32 indices');
 const indices=wake.vertices.indices;
 for(let i=indices.length-48;i<indices.length;i+=3) {
  const [a,b,c]=Array.from(indices.slice(i,i+3),n=>n*3);
  assert.ok((p[b+2]-p[a+2])*(p[c]-p[a])-(p[b]-p[a])*(p[c+2]-p[a+2])>0,'bow foam faces upward on both sides');
 }
 wake.update(s,8,wave,.4);assert.ok(wake.trail.length<20);wake.reset();assert.equal(wake.trail.length,0);assert.equal(object.visible,false);
 wake.update(s,0,wave,1);assert.equal(wake.trail.length,1);wake.dispose();assert.equal(scene.objects.size,0);
});

test('Gerstner buoyancy is deterministic across render batching and responds to swell on both axes',()=>{
 const query=context.window.__gosx_scene3d_ocean_query,q=query.create({waveHeight:1.8,waveLength:24,choppiness:.7,windDirection:18},'high',deep);
 const sample=(x,z,t)=>query.sample(q,x,z,t),a=ship({heading:1.2}),b=ship({heading:1.2});a.mode=b.mode='sailing';a.trim=b.trim=.8;
 let heave=0,pitch=0,roll=0;
 for(let i=1;i<=480;i++) {api.advance(a,1/60,i/60,{rudder:.08},sample,deep);heave=Math.max(heave,Math.abs(a.y));pitch=Math.max(pitch,Math.abs(a.pitch));roll=Math.max(roll,Math.abs(a.roll));}
 for(let i=1;i<=80;i++)api.advance(b,.1,i/10,{rudder:.08},sample,deep);
 for(const key of ['x','y','z','heading','pitch','roll','vy','vp','vr','bowImpact'])near(a[key],b[key],1e-6);
 assert.ok(heave>.08);assert.ok(pitch>.005);assert.ok(roll>.04);
});

test('wind pressure heels a stationary hull on either tack, eases with trim and strength, and luffs when pinching',()=>{
 for(const sign of [-1,1]) {
  const s=ship({heading:sign*Math.PI/2});s.mode='sailing';s.trim=1;
  for(let i=0;i<360;i++)api.buoyancy(s,1/60,i/60,flat,deep);
  assert.ok(sign*s.roll>.1);near(s.speed,0);assert.equal(s.luff,0);
  const full=Math.abs(s.pressure);s.strength=4;api.windLoad(s);near(Math.abs(s.pressure),full*.25);
  s.trim=.5;api.windLoad(s);near(Math.abs(s.pressure),full*.125);
  s.heading=sign*35*radians;api.windLoad(s);assert.equal(s.luff,1);
  s.trim=0;api.windLoad(s);near(s.pressure,0);
 }
});

test('bow spray triggers on relative water entry, never on first sampling, and impact decays',()=>{
 const s=ship();s.mode='sailing';s.speed=5;
 api.buoyancy(s,1/60,0,()=>({y:.4}),deep);near(s.bowImpact,0);
 api.buoyancy(s,1/60,1/60,()=>({y:.7}),deep);assert.ok(s.bowImpact>1);
 for(let i=0;i<240;i++)api.buoyancy(s,1/60,i/60,flat,deep);
 assert.ok(s.bowImpact<.001);
});

test('wake ages in world space through turns; deterministic spray is bounded and disappears on landing',()=>{
 const create=()=>{const scene={objects:new Map()},wake=context.window.__gosx_scene3d_vessel_wake.create(scene,{addObject:(s,id,p)=>s.objects.set(id,p)},{nodeId:'ship'});return {scene,wake};};
 const a=create(),b=create(),s=ship();s.mode='sailing';s.speed=6;s.bowImpact=2;
 for(let i=0;i<40;i++) {s.z=-i*.4;s.heading=i*.025;for(const effect of [a,b])effect.wake.update(s,i*.1,flat,.4,{rotationY:.5});}
 assert.ok(a.wake.trail.length<20);assert.ok(a.wake.spray.particles.length<=10);assert.ok(a.wake.spray.particles.length>0);
 assert.deepEqual(Array.from(a.wake.spray.vertices.positions),Array.from(b.wake.spray.vertices.positions));
 const point={...a.wake.trail[4]};s.heading=2;s.speed=0;s.bowImpact=0;
 a.wake.update(s,4,flat,1);near(a.wake.trail[4].x,point.x);near(a.wake.trail[4].z,point.z);
 const spray=a.scene.objects.get('gosx-vessel-spray:ship');assert.equal(spray.depthWrite,false);assert.equal(spray.castShadow,false);
 a.wake.update(s,11,flat,1);assert.equal(a.wake.trail.length,0);assert.equal(a.wake.spray.particles.length,0);assert.equal(spray.visible,false);
 a.wake.reset();a.wake.dispose();b.wake.dispose();assert.equal(a.scene.objects.size,0);
});

test('inactive wake skips ocean queries; reduced detail samples only distinct foam rows',()=>{
 const scene={objects:new Map()},s=ship(),wake=context.window.__gosx_scene3d_vessel_wake.create(scene,{addObject:(s,id,p)=>s.objects.set(id,p)},{nodeId:'ship'});
 let queries=0;const wave=(x,z,t)=>{queries++;return {y:.2*Math.sin(x+t),normal:{x:0,y:1,z:0}};};
 wake.update(s,0,wave,1);assert.equal(queries,0);
 s.mode='sailing';s.speed=5;
 for(let i=0;i<30;i++){s.z=-i;wake.update(s,i*.25,wave,.4);}
 queries=0;wake.update(s,7.4,wave,.4);assert.ok(queries<=50,`${queries} ocean queries at reduced detail`);
 const p=wake.vertices.positions,n=wake.vertices.normals,u=wake.vertices.uvs;
 for(let i=10;i<20;i++) {
  for(let j=0;j<12;j++)assert.deepEqual(Array.from(p.slice((i*12+j)*3,(i*12+j+1)*3)),Array.from(p.slice(9*36,9*36+3)),'inactive patches collapse to zero area');
  assert.deepEqual(Array.from(n.slice(i*36,(i+1)*36)),Array.from(n.slice(9*36,10*36)));
  assert.deepEqual(Array.from(u.slice(i*24,(i+1)*24)),Array.from(u.slice(9*24,10*24)));
 }
 s.speed=0;queries=0;wake.update(s,14,wave,1);assert.equal(queries,0);assert.equal(scene.objects.get('gosx-vessel-wake:ship').visible,false);
 wake.dispose();
});

test('foam remains attached between stamps and fans along the traveled path when the hull crabs',()=>{
 const scene={objects:new Map()},s=ship(),wake=context.window.__gosx_scene3d_vessel_wake.create(scene,{addObject:(s,id,p)=>s.objects.set(id,p)},{nodeId:'ship'});
 s.mode='sailing';s.speed=6;s.heading=1.2;
 let queries=0;const water=()=>{queries++;return {y:0,normal:{x:0,y:1,z:0}};};
 for(let i=0;i<22;i++){s.z=-i*2.4;wake.update(s,i*.4,water,1);}
 s.z-=.3;queries=0;wake.update(s,8.45,water,1);
 const p=wake.vertices.positions,stern=api.localPoint(s,0,0,s.length*.51);
 const center=(start)=>[0,1,2,3].reduce((out,j)=>{out.x+=p[(start+j)*3]/4;out.z+=p[(start+j)*3+2]/4;return out;},{x:0,z:0});
 const fresh=center(4);assert.ok(Math.hypot(fresh.x-stern.x,fresh.z-stern.z)<.21,'fresh churn stays attached to the stern');
 assert.ok(queries<=80);assert.ok(wake.vertices.count<=260);assert.equal(scene.objects.size,2,'foam and bounded spray retain two effects');
 // The stored track runs along Z while the hull points diagonally. Its cross
 // sections must follow that track, rather than rotating with the hull.
 const left=center(5*12),right=center(5*12+8);near(left.z,right.z,1e-5);
 assert.ok(right.x-left.x>center(8).x-center(0).x+3,'the older wake spreads away from the stern');
 const stamp={...wake.trail[4]};s.speed=0;s.heading=-2;queries=0;wake.update(s,8.5,water,1);
 assert.equal(wake.trail[4].x,stamp.x);assert.equal(wake.trail[4].z,stamp.z);assert.ok(queries<=60,'stopped bow foam needs no water queries');
 wake.dispose();
});

test('wake detail changes retain the full track while drawing fewer feathered age cells',()=>{
 const scene={objects:new Map()},s=ship(),wake=context.window.__gosx_scene3d_vessel_wake.create(scene,{addObject:(s,id,p)=>s.objects.set(id,p)},{nodeId:'ship'});
 s.mode='sailing';s.speed=5;let queries=0;
 const water=()=>{queries++;return {y:0,normal:{x:0,y:1,z:0}};};
 // A reduced-detail frame with just one stamp must also remain valid.
 wake.update(s,0,water,.4);
 for(let i=1;i<=25;i++){s.z=-i*1.55;wake.update(s,i*.31,water,1);}
 const snapshot=()=>Array.from(wake.trail,p=>({...p})),retained=snapshot();
 queries=0;wake.update(s,7.75,water,.4);
 assert.deepEqual(snapshot(),retained,'reducing work cannot truncate the visible wake history');
 assert.ok(7.75-wake.trail.at(-1).time>5,'the retained track spans the older dissolving tail');
 assert.ok(queries<=50,'reduced detail keeps its ocean-query budget');
 const uv=wake.vertices.uvs;
 for(let row=0;row<10;row++)for(let patch=0;patch<3;patch++){
  const a=(row*12+patch*4)*2;
  assert.ok((uv[a]===0&&uv[a+2]===1)||(uv[a]===1&&uv[a+2]===0),'each patch samples both transparent side edges');
  near(uv[a+5]-uv[a+1],1/8,1e-6,'one complete feathered age cell per patch');
 }
 queries=0;wake.update(s,7.75,water,1);assert.ok(queries<=80);assert.deepEqual(snapshot(),retained);
 wake.dispose();
});
test('fresh wake textures keep their orientation and shape as the stern moves between stamps',()=>{
 const scene={objects:new Map()},s=ship(),wake=context.window.__gosx_scene3d_vessel_wake.create(scene,{addObject:(s,id,p)=>s.objects.set(id,p)},{nodeId:'ship'});
 s.mode='sailing';s.speed=8.5;
 const water=()=>({y:0,normal:{x:0,y:1,z:0}});
 wake.update(s,1,water,1);const first=Array.from(wake.vertices.positions.slice(0,36)),uv=Array.from(wake.vertices.uvs.slice(0,24));
 s.z-=8.5/120;wake.update(s,1+1/120,water,1);
 assert.deepEqual(Array.from(wake.vertices.uvs.slice(0,24)),uv,'movement cannot flip the newest foam texture');
 const next=wake.vertices.positions;
 for(let i=0;i<36;i+=3) {
  assert.ok(Math.abs(next[i]-first[i])<.01,'patch shape varies by less than a centimeter per frame');
  near(next[i+1],.065);assert.ok(Math.abs(next[i+2]-first[i+2]+8.5/120)<.01);
 }
 wake.dispose();
});

test('deck bob is gentle and reduced motion removes added bob and wheel roll',()=>{
 const s=ship();s.cameraMode='wheel';s.roll=.15;s.vy=.5;
 const h=api.localPoint(s,s.helm.x,s.helm.y,s.helm.z),normal=api.camera(s,{near:.1},1/60,1);
 assert.ok(Math.abs(normal.y-h.y)<.04);assert.ok(Math.abs(normal.y-h.y)>.01);assert.ok(normal.rotationZ>0);
 s.reduced=true;const reduced=api.camera(s,{near:.1},1/60,1);near(reduced.y,h.y);near(reduced.rotationZ,0);near(api.deckBob(s,1),0);
});

test('pinching flutters the lower sail, keeps yard anchors fixed and deforms rig without accumulating drift',()=>{
 const make=()=>({positions:new Float32Array([-1,4,0,1,4,0,-1,1,0,1,1,0,0,1,0]),normals:new Float32Array(15),uvs:new Float32Array([0,0,1,0,0,1,1,1,.5,1]),indices:new Uint16Array([0,1,4,0,4,2,1,3,4]),revision:0});
 const canvas={vertices:make()},rope={vertices:make()},scene={objects:new Map([['ship/canvas-test',canvas],['ship/rope-rigging',rope]])};
 const s=ship({heading:35*radians});s.mode='sailing';s.trim=1;api.windLoad(s);
 const model=context.window.__gosx_scene3d_vessel_model.create(scene,{nodeId:'ship'},api),camera={x:0,y:5,z:10};
 model.update(s,camera,1,1);const first=Array.from(canvas.vertices.positions),rig=Array.from(rope.vertices.positions);
 model.update(s,camera,1.13,1);assert.notEqual(canvas.vertices.positions[14],first[14]);near(canvas.vertices.positions[0],first[0]);near(canvas.vertices.positions[1],first[1]);
 for(let i=0;i<canvas.vertices.normals.length;i+=3)near(Math.hypot(...canvas.vertices.normals.slice(i,i+3)),1,1e-5);
 model.update(s,camera,1,1);assert.deepEqual(Array.from(rope.vertices.positions),rig);near(rope.vertices.positions[7],1);near(rope.vertices.positions[6],-1);
});

test('landings require a slow nearby ship and a finite authored safe eye point',()=>{
 const nav=context.window.__gosx_scene3d_vessel_navigation,site={id:'island',position:{x:100,y:4.3,z:-200},radius:28,maxSpeed:1.5};
 const s=ship({navigation:true,landings:[site]});s.mode='sailing';s.x=100;s.z=-180;s.speed=1;
 assert.equal(nav.landing(s),site);s.speed=2;assert.equal(nav.landing(s),null);s.speed=1;s.x=200;assert.equal(nav.landing(s),null);
 s.x=100;s.mode='moored';assert.equal(nav.landing(s),null);s.mode='deck';assert.equal(nav.landing(s),site);
 s.config.landings=[{...site,position:{x:NaN,y:4,z:0}}];assert.equal(nav.landing(s),null);
});
