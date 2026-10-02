import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import vm from 'node:vm';
import path from 'node:path';
import {fileURLToPath} from 'node:url';
import harness from './runtime-test-harness.js';

const root=path.resolve(path.dirname(fileURLToPath(import.meta.url)),'../..');
const context=vm.createContext({window:{},Float32Array,Uint8Array,Uint16Array,Uint32Array,Int8Array,Int16Array,ArrayBuffer,DataView,TextDecoder});
for(const source of ['client/js/bootstrap-src/11-scene-math.ts','client/runtime/scene3d/gltf.ts',
 'client/runtime/scene3d/vessel-physics.ts','client/runtime/scene3d/vessel-model.ts']) {
 vm.runInContext(fs.readFileSync(path.join(root,source),'utf8'),context);
}
const physics=context.window.__gosx_scene3d_vessel_physics,modelAPI=context.window.__gosx_scene3d_vessel_model;
function load(lod) {
 const data=fs.readFileSync(path.join(root,'examples/gosx-docs/public/models/blackglass/clipper-'+lod+'.glb'));
 const parsed=context.sceneParseGLB(data.buffer.slice(data.byteOffset,data.byteOffset+data.byteLength));
 return context.gltfExtractScene(parsed.json,parsed.binaryBuffer).objects;
}
function area(p) {
 let result=0;
 for(let i=0;i<p.length;i+=9) {
  const a=[p[i+3]-p[i],p[i+4]-p[i+1],p[i+5]-p[i+2]],b=[p[i+6]-p[i],p[i+7]-p[i+1],p[i+8]-p[i+2]];
  const triangle=Math.hypot(a[1]*b[2]-a[2]*b[1],a[2]*b[0]-a[0]*b[2],a[0]*b[1]-a[1]*b[0])/2;
  assert.ok(Number.isFinite(triangle)&&triangle>1e-7,'each cloth triangle has finite, nonzero area');
  result+=triangle;
 }
 return result;
}
function bounds(p) {
 const lo=[Infinity,Infinity,Infinity],hi=[-Infinity,-Infinity,-Infinity];
 for(let i=0;i<p.length;i++) {lo[i%3]=Math.min(lo[i%3],p[i]);hi[i%3]=Math.max(hi[i%3],p[i]);}
 return {lo,hi};
}
test('all shipped clipper LODs have visible bundled and set canvas with nonzero triangle area',()=>{
 const objects=new Map(),lods=['high','mid','low'];
 for(const lod of lods) for(const object of load(lod)) {object.id='ship-'+lod+'/'+object.id;objects.set(object.id,object);}
 const config={nodeId:'ship-high',lods:[{nodeId:'ship-mid',distance:65},{nodeId:'ship-low',distance:120}]};
 const state=physics.create(config,{},{}),model=modelAPI.create({objects},config,physics);
 for(const [index,lod] of lods.entries()) {
  let furledArea=0;
  for(const trim of [0,.1,.6,1]) {
   state.mode=trim?'sailing':'moored';state.trim=trim;
   model.update(state,{x:index*70,y:0,z:0},1,1);
   const sails=[...objects.values()].filter(o=>o.id.startsWith('ship-'+lod+'/canvas-'));
   assert.equal(sails.length,lod==='low'?6:9);
   let total=0;
   for(const sail of sails) {
    assert.equal(sail.visible,true);assert.equal(sail._modelHidden,false);
    assert.equal(sail.doubleSided,true);assert.equal(sail.material.opacity,1);
    total+=area(sail.vertices.positions);
    if(!trim) {const b=bounds(sail.vertices.positions);assert.ok(b.hi[1]-b.lo[1]>.2,'bundle is thick enough to see below the yard');}
   }
   if(!trim)furledArea=total;
   else assert.ok(total>furledArea,'canvas fills as trim rises');
  }
 }
});
test('wheel eye clears every shipped hull, deckhouse and mast box and keeps the wheel in view',()=>{
 const state=physics.create({}, {}, {});state.cameraMode='wheel';
 const camera=physics.camera(state,{near:.1},1/60),eye=[camera.x,camera.y,camera.z];
 assert.ok(Math.abs(camera.y-state.deck-1.7)<1e-6);assert.equal(camera.near,.05);
 for(const lod of ['high','mid','low']) {
  const boxes=load(lod).filter(o=>/^(tarred-hull|timber-deck|deckhouse|mast-\d)-prim/.test(o.id));
  assert.equal(boxes.length,6,'hull, deck, deckhouse and three mast boxes');
  for(const object of boxes) {
   const {lo,hi}=bounds(object.vertices.positions);
   assert.ok(!eye.every((v,i)=>v>=lo[i]&&v<=hi[i]),`${lod}/${object.id} contains the eye`);
  }
 }
 // Wheel rim at z=6.5: the near plane clears it and the whole rim fits vertically.
 for(const y of [3.02,3.88]) {
  const dy=y-camera.y,distance=Math.hypot(camera.x,camera.z-6.5);
  assert.ok(distance>camera.near);
  assert.ok(Math.abs(Math.atan2(dy,distance)-camera.rotationX)<camera.fov*Math.PI/360);
 }
});
test('stern camera contains the whole shipped ship at 65 degrees on desktop and portrait phones',()=>{
 const objects=load('high'),state=physics.create({}, {}, {}),camera=physics.camera(state,{},1/60);
 assert.equal(camera.fov,65);
 for(const aspect of [1440/900,390/844,360/800]) {
  const halfY=Math.tan(camera.fov*Math.PI/360),halfX=halfY*aspect;
  for(const object of objects) {
   const p=object.vertices.positions;
   for(let i=0;i<p.length;i+=3) {
    const dx=p[i]-camera.x,dy=p[i+1]-camera.y,dz=camera.z-p[i+2];
    const depth=dz*Math.cos(camera.rotationX)+dy*Math.sin(camera.rotationX);
    const up=dy*Math.cos(camera.rotationX)-dz*Math.sin(camera.rotationX);
    assert.ok(depth>0&&Math.abs(up/depth)<halfY*.9&&Math.abs(dx/depth)<halfX*.9,`${object.id} fits at aspect ${aspect}`);
   }
  }
 }
});
test('WebGPU uploads live canvas alongside a larger retained hull without replacing it with zeros',async()=>{
 const h=await harness.createBoardWebGPUHarness({fresh:true});
 const baked=load('low').find(o=>o.id.startsWith('canvas-')).vertices;
 const retained=load('high').find(o=>o.id.startsWith('tarred-hull')).vertices;
 const camera={x:0,y:12,z:40,fov:65,near:.1,far:200};
 const bundle={width:640,height:480,camera,materials:[{color:'#d6c8a4',roughness:.88,opacity:1}],
  worldMeshPositions:baked.positions,worldMeshNormals:baked.normals,worldMeshUVs:baked.uvs,worldMeshTangents:baked.tangents,
  meshObjects:[{id:'canvas',vertexOffset:0,vertexCount:baked.count,materialIndex:0,doubleSided:true},
   {id:'hull',vertexOffset:0,vertexCount:retained.count,materialIndex:0,directVertices:true,retainedGeometry:true,
    vertices:{...retained,immutable:true,revision:0},modelMatrix:new Float32Array([1,0,0,0,0,1,0,0,0,0,1,0,0,0,0,1])}],
  lights:[],environment:{},postEffects:[]};
 assert.ok(retained.count>baked.count);
 h.renderer.render(bundle);
 const uploads=h.fake.state.writeBufferCalls.map(w=>w.data);
 assert.ok(uploads.some(p=>p.length===baked.positions.length&&p.every((v,i)=>v===baked.positions[i])),
  'the real PBR attribute upload must preserve animated cloth positions');
 const pipelines=h.fake.state.renderPipelines.length,shaders=h.fake.state.shaderModules.length;
 const sail={vertices:baked},state=physics.create({}, {}, {});state.mode='sailing';
 const model=modelAPI.create({objects:new Map([['ship/canvas-test',sail]])},{nodeId:'ship'},physics);
 for(const trim of [.1,.5,1]) {
  state.trim=trim;model.update(state,camera,trim,1);h.renderer.render(bundle);
  assert.equal(h.fake.state.renderPipelines.length,pipelines,'trim and billow must not recompile pipelines');
  assert.equal(h.fake.state.shaderModules.length,shaders,'trim and billow must reuse shader modules');
 }
 h.renderer.dispose();
});
