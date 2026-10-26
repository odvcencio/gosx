'use strict';
// A vessel scene with orbit controls (Bon Voyage's scene) wraps the orbit controller in the vessel's own. Camera
// commands, the orbit target and the orbit limits must reach the orbit controller inside it, so a game can move
// between camera presets that look at different points and keep mouse orbit.
const test=require('node:test'),assert=require('node:assert/strict'),fs=require('node:fs'),path=require('node:path');
const {FakeElement,createContext,installManualRAF,runScript,flushAsyncWork,bootstrapSource}=require('./runtime-test-harness.js');
const sources={};for(const part of ['walk','ocean-query','vessel'])sources[part]=fs.readFileSync(path.join(__dirname,'bootstrap-feature-scene3d-'+part+'.js'),'utf8');
async function mountScene() {
 const mount=new FakeElement('div',null);mount.id='orbit-vessel';mount.setAttribute('data-gosx-engine','GoSXScene3D');
 const camera={x:16,y:9,z:24,rotationX:0,rotationY:0,fov:55,near:.1,far:600};
 const props={controls:'orbit',controlTarget:{x:0,y:3,z:0},controlMinDistance:1,controlMaxDistance:400,width:640,height:360,maxFPS:60,camera,
  vessel:{nodeId:'ship',externalPose:false,wake:false},scene:{camera,objects:[{id:'ship/hull',kind:'box',geometry:'box',size:3}]}};
 const routes=Object.fromEntries(Object.entries(sources).map(([name,text])=>['/'+name+'.js',{text}]));
 const env=createContext({elements:[mount],enableWebGL:true,disableCanvas2D:true,performanceNow:()=>0,fetchRoutes:routes,
  manifest:{engines:[{id:'orbit-vessel-scene',component:'GoSXScene3D',kind:'surface',mountId:mount.id,props}]}});
 env.context.atob=atob;const script=env.document.createElement('script');script.setAttribute('data-gosx-script','feature-scene3d');
 for(const name of ['walk','ocean-query','vessel'])script.setAttribute('data-gosx-scene3d-'+name+'-url','/'+name+'.js');env.document.head.appendChild(script);
 const raf=installManualRAF(env.context);runScript(bootstrapSource,env.context,'bootstrap.js');env.document.dispatchEvent({type:'DOMContentLoaded'});
 for(let i=0;i<12;i++) {await flushAsyncWork();raf.flush(i*17);}
 const mounted=env.context.__gosx.engines.get('orbit-vessel-scene');assert.ok(mounted&&mounted.handle);
 assert.equal(mount.getAttribute('data-gosx-scene3d-vessel')!==null||env.context.__gosx_scene3d_vessel_api!==undefined,true,'the vessel controller did not load');
 return {env,mount,raf,canvas:mount.querySelector('canvas'),handle:mounted.handle};
}
const dist=(c,t)=>Math.hypot(c.x-t.x,c.y-t.y,c.z-t.z);
function aimError(c,t) {
 const f={x:-Math.sin(c.rotationY)*Math.cos(c.rotationX),y:Math.sin(c.rotationX),z:-Math.cos(c.rotationY)*Math.cos(c.rotationX)},d=dist(c,t);
 return Math.hypot(f.x-(t.x-c.x)/d,f.y-(t.y-c.y)/d,f.z-(t.z-c.z)/d);
}
async function drag(h,dx,dy=0) {
 const base={button:0,pointerId:3,preventDefault(){},stopPropagation(){}};
 h.canvas.dispatchEvent({...base,type:'pointerdown',clientX:320,clientY:180});
 h.canvas.dispatchEvent({...base,type:'pointermove',buttons:1,clientX:320+dx,clientY:180+dy});
 h.canvas.dispatchEvent({...base,type:'pointerup',clientX:320+dx,clientY:180+dy});
 await flushAsyncWork();h.raf.flush(500);
}
const far={x:30,y:10,z:-40};
const farCamera={kind:'perspective',x:62,y:16,z:-4,rotationX:0,rotationY:0,fov:55,near:.1,far:600,orbit:{target:far}};

test('a vessel scene with orbit controls aims a camera at its orbit target and orbits around it',async()=>{
 const h=await mountScene();
 await drag(h,120);                       // the player has orbited: the controls own the camera now
 h.handle.setCamera(farCamera);
 await flushAsyncWork();h.raf.flush(600);
 const before=h.handle.getCamera();
 assert.ok(Math.abs(before.x-62)<1e-6&&Math.abs(before.z+4)<1e-6,'camera did not move: '+JSON.stringify(before));
 assert.ok(aimError(before,far)<1e-6,'not aimed at the new target');
 await drag(h,150,20);
 const after=h.handle.getCamera();
 assert.ok(Math.abs(after.x-before.x)>.1||Math.abs(after.z-before.z)>.1,'drag did not orbit');
 assert.ok(Math.abs(dist(after,far)-dist(before,far))<1e-6,'orbit left the new target');
 assert.ok(aimError(after,far)<1e-6);
});

test('a dispatched camera command moves a vessel scene after the player has orbited',async()=>{
 const h=await mountScene();
 await drag(h,120);
 await h.handle.applyCommands([{kind:5,data:farCamera}]);
 await flushAsyncWork();h.raf.flush(600);
 const c=h.handle.getCamera();
 assert.ok(Math.abs(c.x-62)<1e-6&&aimError(c,far)<1e-6,JSON.stringify(c));
});

test('setOrbit and the orbit limits reach the controller inside a vessel scene',async()=>{
 const h=await mountScene();
 h.handle.setOrbit({target:far,minDistance:30,maxDistance:60,pitchLimit:.4});
 await flushAsyncWork();h.raf.flush(600);
 assert.ok(aimError(h.handle.getCamera(),far)<1e-6);
 for(const deltaY of [-100000,100000]) {
  h.canvas.dispatchEvent({type:'wheel',deltaY,preventDefault(){},stopPropagation(){}});
  await flushAsyncWork();h.raf.flush(700);
  const d=dist(h.handle.getCamera(),far);
  assert.ok(d>=29.999&&d<=60.001,'distance '+d+' left [30, 60]');
 }
});
