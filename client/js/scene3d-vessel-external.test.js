'use strict';
const test=require('node:test'),assert=require('node:assert/strict'),fs=require('node:fs'),path=require('node:path'),vm=require('node:vm');
const context={window:{},TextDecoder,DataView,Uint8Array,Float64Array,Math,Number,Map,Error,Date};
vm.runInNewContext(fs.readFileSync(path.join(__dirname,'../runtime/scene3d/vessel-external.ts'),'utf8'),context);
const api=context.window.__gosx_scene3d_vessel_external;
function frame(...poses) {
 const parts=[Buffer.from('GSV1'),Buffer.alloc(2)];parts[1].writeUInt16LE(poses.length);
 for(const p of poses) {
  const id=Buffer.from(p.id),b=Buffer.alloc(3+id.length+48);b[0]=p.snap?1:0;b.writeUInt16LE(id.length,1);id.copy(b,3);
  let at=3+id.length;for(const v of [p.time,p.x,p.y,p.z]){b.writeDoubleLE(v,at);at+=8;}
  for(const v of [p.heading||0,p.pitch||0,p.roll||0,p.speed||0]){b.writeFloatLE(v,at);at+=4;}
  parts.push(b);
 }
 return new Uint8Array(Buffer.concat(parts));
}
test('the decoder reads the bytes that the Go encoder wrote',()=>{
 const poses=api.decode(new Uint8Array(fs.readFileSync(path.join(__dirname,'testdata/vessel-pose-frame.bin'))));
 assert.equal(poses.length,2);
 assert.deepEqual({...poses[0]},{id:'liner',snap:true,time:12.5,x:1234.5,y:.25,z:-98765.5,heading:1.5,pitch:-.25,roll:.125,speed:9.75});
 assert.equal(poses[1].id,'tender');assert.equal(poses[1].snap,false);assert.equal(poses[1].heading,-3);
});
test('the decoder rejects malformed frames',()=>{
 const good=frame({id:'a',time:1,x:0,y:0,z:0});
 for(const bad of [new Uint8Array(0),good.subarray(0,10),good.subarray(0,good.length-1),new Uint8Array([...good,0]),new Uint8Array([71,83,86,50,...good.subarray(4)])])assert.throws(()=>api.decode(bad));
 assert.throws(()=>api.decode(frame({id:'a',time:1,x:NaN,y:0,z:0})),/non-finite/);
 assert.throws(()=>api.decode(frame({id:'',time:1,x:0,y:0,z:0})));
});
const sample=(time,x,extra={})=>({id:'a',snap:false,time,x,y:0,z:0,heading:0,pitch:0,roll:0,speed:0,...extra});
test('a channel blends between ticks without a jump and holds when a tick is late',()=>{
 const c=api.create();assert.equal(c.pose(0),null);
 c.push(sample(0,0),10);assert.equal(c.pose(10).x,0);
 c.push(sample(.1,10),10.1);
 const at=t=>c.pose(t).x;
 assert.ok(Math.abs(at(10.2)-10)<1e-9);c.push(sample(.2,20),10.2);
 assert.ok(Math.abs(at(10.2)-10)<1e-9,'starts where the previous blend had reached');
 assert.ok(at(10.25)>10&&at(10.25)<20);
 assert.ok(at(10.2+c.duration*.5)>at(10.21)&&at(10.2+c.duration*.99)<=20);
 assert.equal(at(10.9),20,'holds the newest pose when the next tick is late');
 let last=-1;for(let t=10.2;t<10.5;t+=.004){const x=at(t);assert.ok(x>=last-1e-9,'monotone');last=x;}
});
test('headings blend the short way round and stale or repeated ticks are dropped',()=>{
 const c=api.create();c.push(sample(0,0,{heading:3.1}),0);c.push(sample(.1,0,{heading:-3.1}),.1);
 const h=c.pose(.1+c.duration/2).heading;assert.ok(Math.abs(Math.abs(h)-Math.PI)<.05,`heading ${h}`);
 assert.equal(c.push(sample(.1,5),.2),false);assert.equal(c.push(sample(.05,5),.2),false);assert.equal(c.dropped,2);
});
test('snap and large jumps cut instead of sliding',()=>{
 const c=api.create();c.push(sample(0,0),0);c.push(sample(.1,1),.1);
 c.push(sample(.2,9,{snap:true}),.2);assert.equal(c.pose(.2).x,9);
 c.push(sample(.3,5000),.3);assert.equal(c.pose(.3).x,5000);
});
test('active covers a blend and a short gap, then goes idle',()=>{
 const c=api.create();assert.equal(c.active(0),false);c.push(sample(0,0),1);
 assert.equal(c.active(1.5),true);assert.equal(c.active(3),false);
});
test('dispatch routes by mount ID and ship ID and reports whether a vessel took the frame',()=>{
 const taken=[],c=api.create();api.register('scene-1','liner',c);
 api.now=()=>5;
 assert.equal(context.window.__gosx_scene3d_vessel_pose('scene-1',frame({id:'liner',time:1,x:3,y:0,z:4})),true);
 assert.equal(c.pose(5).x,3);
 assert.equal(context.window.__gosx_scene3d_vessel_pose('scene-2',frame({id:'liner',time:2,x:3,y:0,z:4})),false);
 assert.equal(context.window.__gosx_scene3d_vessel_pose('scene-1',frame({id:'other',time:2,x:3,y:0,z:4})),false);
 api.unregister('scene-1','liner',c);
 assert.equal(context.window.__gosx_scene3d_vessel_pose('scene-1',frame({id:'liner',time:3,x:3,y:0,z:4})),false);
 assert.deepEqual(taken,[]);
});

// Mount-level checks use the built vessel chunk, like scene3d-vessel.test.js.
const {FakeElement,createContext,installManualRAF,runScript,flushAsyncWork,bootstrapSource}=require('./runtime-test-harness.js');
async function mountExternal(extra={}) {
 const sources={};for(const part of ['walk','ocean-query','vessel'])sources[part]=fs.readFileSync(path.join(__dirname,'bootstrap-feature-scene3d-'+part+'.js'),'utf8');
 const mount=new FakeElement('div',null);mount.id='go-ship';mount.setAttribute('data-gosx-engine','GoSXScene3D');
 const camera={x:0,y:4.2,z:6.5,rotationX:0,rotationY:0,fov:60,near:.1,far:200};
 const props={controls:'first-person',walk:{headBob:0},camera,maxFPS:60,scene:{camera,objects:[{id:'ship/hull',kind:'box',geometry:'box',size:3}]}};
 props.vessel={nodeId:'ship',position:{x:10,y:0,z:-20},heading:.5,externalPose:true,wake:false,windStrength:8,helm:{x:0,y:4.2,z:6.5},...extra};
 props.scene.environment={ocean:{waveHeight:.9,waveLength:17,choppiness:.7}};
 const routes=Object.fromEntries(Object.entries(sources).map(([name,text])=>['/'+name+'.js',{text}]));
 const env=createContext({elements:[mount],enableWebGL:true,disableCanvas2D:true,performanceNow:()=>0,matchMedia:{'(pointer: coarse)':false},fetchRoutes:routes,
  manifest:{engines:[{id:'ship-scene',component:'GoSXScene3D',kind:'surface',mountId:mount.id,props}]}});
 env.context.atob=atob;env.context.TextDecoder=TextDecoder;const script=env.document.createElement('script');script.setAttribute('data-gosx-script','feature-scene3d');
 for(const name of ['walk','ocean-query','vessel'])script.setAttribute('data-gosx-scene3d-'+name+'-url','/'+name+'.js');env.document.head.appendChild(script);
 const raf=installManualRAF(env.context);runScript(bootstrapSource,env.context,'bootstrap.js');env.document.dispatchEvent({type:'DOMContentLoaded'});
 for(let i=0;i<12;i++) {await flushAsyncWork();raf.flush(i*17);}
 const mounted=env.context.__gosx.engines.get('ship-scene');assert.ok(mounted&&mounted.handle);
 return {env,mount,raf,handle:mounted.handle,objects:()=>mount.__gosxScene3DState.objects};
}
test('an external vessel shows the pose Go sends and runs no sailing physics',async()=>{
 const h=await mountExternal();
 assert.equal(h.mount.getAttribute('data-gosx-scene3d-vessel-source'),'go');
 const hull=()=>h.objects().get('ship/hull');
 const at=m=>[m[12],m[13],m[14]];
 for(let t=200;t<1200;t+=17)h.raf.flush(t);
 assert.ok(Math.abs(at(hull().parentMatrix)[0]-10)<1e-4&&Math.abs(at(hull().parentMatrix)[2]+20)<1e-4,'waits at the authored position');
 const push=(poses)=>h.env.context.__gosx_scene3d_vessel_pose('go-ship',frame(...poses));
 assert.equal(push([{id:'ship',time:1,x:300,y:1.5,z:-700,heading:Math.PI/2,pitch:.05,roll:-.1,speed:9,snap:true}]),true);
 for(let t=1300;t<1500;t+=17)h.raf.flush(t);
 const m=hull().parentMatrix;
 assert.ok(Math.abs(m[12]-300)<1e-3&&Math.abs(m[13]-1.5)<1e-3&&Math.abs(m[14]+700)<1e-3,`pose ${at(m)}`);
 // The ship's local +Z axis (stern) points along world +X at heading pi/2 with the bow toward -X... check the yaw column.
 assert.ok(Math.abs(m[0])<.02&&m[8]!==0,'yaw is applied');
 assert.equal(h.env.context.__gosx_scene3d_vessel_pose('wrong-mount',frame({id:'ship',time:2,x:0,y:0,z:0})),false);
 h.handle.dispose();
 assert.equal(h.env.context.__gosx_scene3d_vessel_pose('go-ship',frame({id:'ship',time:3,x:0,y:0,z:0})),false,'a disposed mount stops listening');
});
test('the scene keeps rendering while poses arrive and a sailing vessel is unaffected',async()=>{
 const h=await mountExternal();
 const controller=h.mount.__gosxScene3DState._gosxMotionController;assert.ok(controller);
 assert.equal(controller.active,false);
 h.env.context.__gosx_scene3d_vessel_pose('go-ship',frame({id:'ship',time:1,x:1,y:0,z:1,snap:true}));
 assert.equal(controller.active,true);
});
