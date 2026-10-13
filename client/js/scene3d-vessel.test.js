'use strict';
const test=require('node:test'),assert=require('node:assert/strict'),fs=require('node:fs'),path=require('node:path');
const {FakeElement,createContext,installManualRAF,runScript,flushAsyncWork,bootstrapSource}=require('./runtime-test-harness.js');
const sources={};for(const part of ['walk','ocean-query','vessel'])sources[part]=fs.readFileSync(path.join(__dirname,'bootstrap-feature-scene3d-'+part+'.js'),'utf8');
async function mountScene(vessel=true,maxFPS=60,realShip=false,coarse=false,spectator=false,reduced=false,options={}) {
 const mount=new FakeElement('div',null);mount.id='mounted-vessel';mount.setAttribute('data-gosx-engine','GoSXScene3D');
 const camera={x:0,y:4.2,z:6.5,rotationX:0,rotationY:0,fov:60,near:.1,far:200};
 const props={controls:'first-person',walk:{headBob:0},camera,maxFPS,scene:{camera,objects:[{id:'ship/hull',kind:'box',geometry:'box',size:3}]}};
 if(vessel) {props.vessel={nodeId:'ship',heading:Math.PI/2,windStrength:8,helm:{x:0,y:4.2,z:6.5},wake:false};props.camera={...camera,x:6.5,z:0};props.scene.camera=props.camera;props.scene.environment={ocean:{waveHeight:.9,waveLength:17,choppiness:.7}};}
 if(vessel&&options.quickBoard)props.vessel.quickBoard=true;
 if(vessel&&options.navigation)props.vessel.navigation=true;
 if(vessel&&options.openWater) {
  props.vessel.openWater=true;props.vessel.position={x:2000,z:-3000};
  props.walk.bounds={minX:-10,minZ:-10,maxX:10,maxZ:10};
  props.walk.ground={minX:0,minZ:0,sizeX:1,sizeZ:1,cols:1,rows:1,minHeight:30,maxHeight:30,heights:'AAA='};
 }
 if(options.far)props.scene.camera=props.camera={...props.camera,x:80,z:80};
 const routes=Object.fromEntries(Object.entries(sources).map(([name,text])=>['/'+name+'.js',{text}]));
 if(realShip) {
  if(spectator)props.camera=props.scene.camera={...props.camera,x:180,z:0,rotationY:Math.PI/2};
  props.scene.objects=[];props.scene.models=[];props.vessel.lods=[];
  for(const [i,lod] of ['high','mid','low'].entries()) {
   const id=i?'ship-'+lod:'ship',src='/clipper-'+lod+'.glb';
   routes[src]={bytes:Array.from(fs.readFileSync(path.join(__dirname,'../../examples/gosx-docs/public/models/blackglass',src.slice(1))))};
   props.scene.models.push({id,src,visible:i===0});if(i)props.vessel.lods.push({nodeId:id,distance:i*65});
  }
 }
 const env=createContext({elements:[mount],enableWebGL:true,disableCanvas2D:true,performanceNow:()=>0,
  matchMedia:{'(pointer: coarse)':coarse},prefersReducedMotion:reduced,
  fetchRoutes:routes,
  manifest:{engines:[{id:'ship-scene',component:'GoSXScene3D',kind:'surface',mountId:mount.id,props}]}});
 env.context.atob=atob;const script=env.document.createElement('script');script.setAttribute('data-gosx-script','feature-scene3d');
 for(const name of ['walk','ocean-query','vessel'])script.setAttribute('data-gosx-scene3d-'+name+'-url','/'+name+'.js');env.document.head.appendChild(script);
 const raf=installManualRAF(env.context);runScript(bootstrapSource,env.context,'bootstrap.js');env.document.dispatchEvent({type:'DOMContentLoaded'});
 for(let i=0;i<12;i++) {await flushAsyncWork();raf.flush(i*17);}
 const mounted=env.context.__gosx.engines.get('ship-scene');assert.ok(mounted&&mounted.handle);
 const canvas=mount.querySelector('canvas'),gl=canvas.getContext('webgl');
 function event(target,type,extra={}) {const e={type,preventDefault(){this.defaultPrevented=true;},...extra};target.dispatchEvent(e);return e;}
 return {env,mount,canvas,gl,raf,event,handle:mounted.handle};
}
test('scoped navigation commands validate input, publish snapshots, yield to steering, and dispose',async()=>{
 const h=await mountScene(true,60,false,false,false,false,{quickBoard:true,far:true,navigation:true});
 const snapshots=[];h.mount.addEventListener('gosx:scene3d:vessel-state',event=>snapshots.push(event.detail));
 const command=detail=>h.event(h.mount,'gosx:scene3d:vessel-command',{detail:{nodeId:'ship',...detail}});
 command({nodeId:'other',action:'helm'});assert.equal(h.mount.getAttribute('data-gosx-scene3d-vessel'),'moored');
 command({action:'course',bearing:60});assert.equal(snapshots.length,0,'a moored vessel cannot hold course');
 command({action:'helm'});assert.equal(snapshots.at(-1).mode,'sailing');
 for(const bearing of [NaN,Infinity,-1,361,'60'])command({action:'course',bearing});
 assert.equal(h.mount.getAttribute('data-gosx-scene3d-course-hold'),'false');
 command({action:'trim',trim:1});command({action:'course',bearing:60});
 assert.equal(h.mount.getAttribute('data-gosx-scene3d-course-hold'),'true');assert.ok(Math.abs(snapshots.at(-1).course-60)<1e-8);
 const old=snapshots.at(-1),oldPosition={...old.position};for(let t=250;t<3000;t+=17)h.raf.flush(t);
 assert.deepEqual({...old.position},oldPosition,'published position is a snapshot');assert.ok(snapshots.at(-1).distance>0);
 assert.ok(snapshots.length<25,'telemetry is bounded to five updates per second plus explicit commands');
 h.canvas.focus();h.event(h.env.document,'keydown',{code:'KeyA'});for(let t=3000;t<3400;t+=17)h.raf.flush(t);
 assert.equal(h.mount.getAttribute('data-gosx-scene3d-course-hold'),'false');
 h.event(h.env.document,'keyup',{code:'KeyA'});command({action:'course',bearing:0});assert.equal(snapshots.at(-1).course,0);
 command({action:'trim',trim:0});assert.equal(snapshots.at(-1).trim,0);command({action:'helm'});assert.equal(snapshots.at(-1).course,null);
 h.handle.resetCamera();h.raf.flush(3650);assert.equal(h.mount.getAttribute('data-gosx-scene3d-course-hold'),'false');
 h.handle.dispose();const count=snapshots.length;command({action:'helm'});assert.equal(snapshots.length,count);
});
test('navigation telemetry remains live with a frozen reduced-motion animation clock',async()=>{
 const h=await mountScene(true,60,false,false,false,true,{navigation:true});
 const snapshots=[];h.mount.addEventListener('gosx:scene3d:vessel-state',e=>snapshots.push(e.detail));
 h.event(h.mount,'gosx:scene3d:vessel-command',{detail:{nodeId:'ship',action:'helm'}});
 h.event(h.mount,'gosx:scene3d:vessel-command',{detail:{nodeId:'ship',action:'trim',trim:1}});
 for(let t=250;t<2500;t+=17)h.raf.flush(t);
 assert.equal(h.mount.getAttribute('data-gosx-scene3d-animation-clock'),'0.000');
 assert.ok(snapshots.length>8);assert.ok(snapshots.at(-1).distance>0);h.handle.dispose();
});
test('ordinary vessels ignore navigation events and publish no telemetry',async()=>{
 const h=await mountScene();let calls=0;h.mount.addEventListener('gosx:scene3d:vessel-state',()=>calls++);
 h.event(h.mount,'gosx:scene3d:vessel-command',{detail:{nodeId:'ship',action:'helm'}});h.raf.flush(250);
 assert.equal(h.mount.getAttribute('data-gosx-scene3d-vessel'),'moored');assert.equal(calls,0);h.handle.dispose();
});
test('Pilot ship is visible from shore, boards in one click, and returns to the authored camera on reset',async()=>{
 const h=await mountScene(true,60,false,false,false,false,{quickBoard:true,far:true});
 const panel=h.mount.querySelector('.gosx-scene3d-vessel-controls'),pilot=panel.querySelector('button');
 assert.equal(panel.hidden,false);assert.equal(pilot.textContent,'Pilot ship');
 h.canvas.focus();h.event(h.env.document,'keydown',{code:'KeyE'});h.raf.flush(230);
 assert.equal(h.mount.getAttribute('data-gosx-scene3d-vessel'),'moored','E cannot board remotely');
 const programs=h.gl.programs.length;h.event(pilot,'click');h.raf.flush(250);
 assert.equal(h.mount.getAttribute('data-gosx-scene3d-vessel'),'sailing');
 const aboard=h.handle.getCamera();assert.ok(Math.hypot(aboard.x-80,aboard.z-80)>50);assert.ok(aboard.y>2);
 assert.equal(h.env.document.activeElement,h.canvas);assert.match(pilot.textContent,/Leave helm/);
 assert.equal(h.gl.programs.length,programs,'boarding reuses the renderer');
 h.event(pilot,'click');h.raf.flush(300);assert.equal(h.mount.getAttribute('data-gosx-scene3d-vessel'),'deck');
 h.handle.resetCamera();h.raf.flush(350);const reset=h.handle.getCamera();
 assert.ok(Math.abs(reset.x-80)<.01&&Math.abs(reset.z-80)<.01);
 h.handle.dispose();assert.equal(h.mount.querySelector('.gosx-scene3d-vessel-controls'),null);
});
test('mounted open-water vessel uses deep water beyond the finite terrain and leaves on its offshore deck',async()=>{
 const h=await mountScene(true,60,false,false,false,false,{quickBoard:true,far:true,openWater:true});
 const api=h.env.context.__gosx_scene3d_vessel_physics,advance=api.advance;let state,floor;
 api.advance=(...args)=>{state=args[0];floor=args[5];return advance(...args);};
 const pilot=h.mount.querySelector('.gosx-scene3d-vessel-controls').querySelector('button');h.event(pilot,'click');h.raf.flush(250);
 assert.equal(state.mode,'sailing');
 assert.equal(state.grounded,false);assert.equal(floor(2000,-3000),-1e4);assert.equal(floor(.5,.5),30);
 h.event(pilot,'click');h.raf.flush(300);const aboard=h.handle.getCamera();
 assert.equal(state.mode,'deck');
 h.canvas.focus();h.event(h.env.document,'keydown',{code:'KeyW'});h.raf.flush(350);h.raf.flush(400);
 const walked=h.handle.getCamera();assert.ok(walked.x>1900&&walked.z<-2900,'deck walking must not clamp the camera back to land bounds: '+JSON.stringify({aboard,walked}));
 assert.ok(walked.y<6,'the edge of the land heightfield must not lift the deck walker');
 assert.ok(Math.hypot(walked.x-aboard.x,walked.z-aboard.z)<2);h.handle.resetCamera();h.raf.flush(450);
 assert.equal(h.mount.getAttribute('data-gosx-scene3d-vessel'),'moored');h.handle.dispose();
});
test('a scene without a vessel fetches neither vessel nor ocean query even when URLs are advertised',async()=>{
 const h=await mountScene(false);assert.equal(h.env.context.__gosx_scene3d_vessel_api,undefined);assert.equal(h.env.context.__gosx_scene3d_ocean_query,undefined);
 assert.ok(!h.env.fetchCalls.some(c=>/vessel\.js|ocean-query\.js/.test(String(c.url||c))));h.handle.dispose();
});
test('initially hidden real LODs become drawable when a distant walker views the ship',async()=>{
 const h=await mountScene(true,60,true,false,true);
 assert.ok(h.gl.ops.some(op=>(op[0]==='drawElements'&&op[2]===360)||(op[0]==='drawArrays'&&op[3]===360)),'low hull renders at distance; imported hidden state must be cleared');
 h.handle.dispose();
});
test('mounted touch rudder and sail buttons retain independent pointers and clear on blur',async()=>{
 const h=await mountScene(true,60,false,true);h.canvas.focus();h.event(h.env.document,'keydown',{code:'KeyE'});h.raf.flush(250);
 const api=h.env.context.__gosx_scene3d_vessel_physics,advance=api.advance;let input;
 api.advance=(...args)=>{input=args[3];return advance(...args);};
 const panel=h.mount.querySelector('.gosx-scene3d-vessel-controls'),raise=Array.from(panel.querySelectorAll('button')).find(n=>n.textContent==='Raise sail');
 const hint=Array.from(h.mount.querySelectorAll('span')).find(n=>n.textContent==='Drag left to steer');assert.equal(hint.hidden,false);assert.match(panel.style.width,/50%/);
 h.event(h.canvas,'pointerdown',{pointerType:'touch',pointerId:11,clientX:20,clientY:200});
 h.event(h.canvas,'pointermove',{pointerType:'touch',pointerId:11,clientX:75,clientY:200});h.event(raise,'pointerdown',{pointerId:22});h.raf.flush(300);
 assert.equal(input.rudder,1);assert.equal(input.sail,1);h.event(raise,'pointerup',{pointerId:22});h.raf.flush(350);
 assert.equal(input.rudder,1);assert.equal(input.sail,0);h.event(raise,'pointerdown',{pointerId:22});
 h.event(h.canvas,'lostpointercapture',{pointerType:'touch',pointerId:11});h.raf.flush(400);assert.equal(input.rudder,0);assert.equal(input.sail,1);
 h.event(h.env.context,'blur');h.raf.flush(450);assert.equal(input.rudder,0);assert.equal(input.sail,0);h.handle.dispose();assert.equal(hint.parentElement,null);
});
test('real clipper GLBs hydrate, sail, leave and reset through the existing renderer',async()=>{
 const h=await mountScene(true,60,true);
 const upload=h.gl.bufferData.bind(h.gl);h.gl.bufferData=(target,data,usage)=>{assert.ok(!Array.isArray(data),'WebGL bufferData requires typed geometry, including moving rigging');return upload(target,data,usage);};
 assert.equal(h.mount.getAttribute('data-gosx-scene3d-vessel'),'moored');
 for(const lod of ['high','mid','low'])assert.ok(h.env.fetchCalls.some(c=>c.url==='/clipper-'+lod+'.glb'));
 h.canvas.focus();h.event(h.env.document,'keydown',{code:'KeyE'});h.event(h.env.document,'keydown',{code:'KeyW'});
 for(let t=250;t<1000;t+=17)h.raf.flush(t);assert.equal(h.mount.getAttribute('data-gosx-scene3d-vessel'),'sailing');
 const programs=h.gl.programs.length;for(let t=1000;t<1800;t+=17)h.raf.flush(t);assert.equal(h.gl.programs.length,programs);
 h.event(h.env.document,'keydown',{code:'KeyE'});h.raf.flush(1850);const deck=h.handle.getCamera();assert.ok(deck.y>2);
 assert.equal(h.handle.resetCamera(),true);h.raf.flush(1900);assert.equal(h.mount.getAttribute('data-gosx-scene3d-vessel'),'moored');
 const reset=h.handle.getCamera();assert.ok(Math.abs(reset.x-6.5)<.01&&Math.abs(reset.z)<.01);h.handle.dispose();
});
test('mounted walker takes and leaves helm, V toggles camera, sailing updates nodes without recompiling shaders',async()=>{
 const h=await mountScene(),count=h.gl.programs.length;assert.ok(count>0);assert.equal(h.mount.getAttribute('data-gosx-scene3d-vessel'),'moored');
 h.canvas.focus();h.event(h.env.document,'keydown',{code:'KeyE'});h.raf.flush(250);
 assert.equal(h.mount.getAttribute('data-gosx-scene3d-vessel'),'sailing');const stern=h.handle.getCamera();
 h.event(h.env.document,'keydown',{code:'KeyV'});h.raf.flush(300);const wheel=h.handle.getCamera();assert.ok(Math.hypot(stern.x-wheel.x,stern.z-wheel.z)>5);
 h.event(h.env.document,'keydown',{code:'KeyW'});h.event(h.env.document,'keydown',{code:'KeyA'});
 for(let t=350;t<2000;t+=17)h.raf.flush(t);assert.equal(h.gl.programs.length,count);
 h.event(h.env.document,'keydown',{code:'KeyE'});h.raf.flush(2050);assert.equal(h.mount.getAttribute('data-gosx-scene3d-vessel'),'deck');
 const deck=h.handle.getCamera();assert.ok(Number.isFinite(deck.y));assert.ok(deck.y>2);h.handle.dispose();
 assert.equal(h.mount.querySelector('.gosx-scene3d-vessel-controls'),null);
});
test('mounted vessel does not create another RAF loop and respects MaxFPS frame pacing',async()=>{
 const h=await mountScene(true,20);h.canvas.focus();h.event(h.env.document,'keydown',{code:'KeyE'});
 const before=h.gl.ops.filter(op=>op[0]==='drawArrays'||op[0]==='drawElements').length;for(let t=300;t<600;t+=8)h.raf.flush(t);
 // One mesh and one ocean draw per paced frame, plus renderer setup passes.
 assert.ok(h.gl.ops.filter(op=>op[0]==='drawArrays'||op[0]==='drawElements').length-before<60);assert.ok(h.raf.count()<=1);h.handle.dispose();
});

test('reduced motion keeps held sailing inputs responsive while removing wheel bob and roll',async()=>{
 const h=await mountScene(true,60,false,false,false,true),api=h.env.context.__gosx_scene3d_vessel_physics,advance=api.advance;let state;
 api.advance=(...args)=>{state=args[0];return advance(...args);};
 h.canvas.focus();h.event(h.env.document,'keydown',{code:'KeyE'});h.event(h.env.document,'keydown',{code:'KeyW'});
 for(let t=250;t<2500;t+=17)h.raf.flush(t);
 assert.ok(state.trim>.9);assert.ok(state.speed>2);assert.equal(state.reduced,true);
 assert.equal(h.mount.getAttribute('data-gosx-scene3d-animation-clock'),'0.000','declarative animation stays frozen');
 h.event(h.env.document,'keyup',{code:'KeyW'});h.event(h.env.document,'keydown',{code:'KeyV'});h.raf.flush(2550);
 const wheel=h.handle.getCamera(),helm=api.localPoint(state,state.helm.x,state.helm.y,state.helm.z);
 assert.ok(Math.abs(wheel.y-helm.y)<1e-5);assert.equal(wheel.rotationZ,0);assert.ok(h.raf.count()<=1);
 h.event(h.env.document,'keydown',{code:'KeyE'});h.raf.flush(2600);h.raf.flush(2700);
 assert.equal(h.mount.getAttribute('data-gosx-scene3d-vessel'),'deck');assert.equal(h.mount.getAttribute('data-gosx-scene3d-render-loop-wants-animation'),'false');
 h.handle.dispose();
});
