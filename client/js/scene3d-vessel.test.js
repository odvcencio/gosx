'use strict';
const test=require('node:test'),assert=require('node:assert/strict'),fs=require('node:fs'),path=require('node:path');
const {FakeElement,createContext,installManualRAF,runScript,flushAsyncWork,bootstrapSource}=require('./runtime-test-harness.js');
const sources={};for(const part of ['walk','ocean-query','vessel'])sources[part]=fs.readFileSync(path.join(__dirname,'bootstrap-feature-scene3d-'+part+'.js'),'utf8');
async function mountScene(vessel=true,maxFPS=60,realShip=false,coarse=false,spectator=false,reduced=false) {
 const mount=new FakeElement('div',null);mount.id='mounted-vessel';mount.setAttribute('data-gosx-engine','GoSXScene3D');
 const camera={x:0,y:4.2,z:6.5,rotationX:0,rotationY:0,fov:60,near:.1,far:200};
 const props={controls:'first-person',walk:{headBob:0},camera,maxFPS,scene:{camera,objects:[{id:'ship/hull',kind:'box',geometry:'box',size:3}]}};
 if(vessel) {props.vessel={nodeId:'ship',heading:Math.PI/2,windStrength:8,helm:{x:0,y:4.2,z:6.5},wake:false};props.camera={...camera,x:6.5,z:0};props.scene.camera=props.camera;props.scene.environment={ocean:{waveHeight:.9,waveLength:17,choppiness:.7}};}
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
