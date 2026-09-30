"use strict";
const test=require("node:test"),assert=require("node:assert/strict");
const {createBoardWebGPUHarness,createWebGLRendererForPost,makePointsBundle}=require("./runtime-test-harness.js");
function bundle(){const b=makePointsBundle(null);b.points=[];b.environment.sky={mode:"physical",clouds:{coverage:0.45,altitude:1500,scale:3000,speed:8,direction:90,opacity:0.85}};b.environment.ocean={};return b;}
test("cloud defaults, normalized clamps and stable drift/lighting at golden hour, noon and blue hour",async()=>{
 const h=await createBoardWebGPUHarness({fresh:true}),a=h.env.context.__gosx_scene3d_api;
 assert.equal(a.sceneSkyClouds(null),null);const c=a.sceneSkyClouds({});assert.equal(c.coverage,0.45);assert.equal(c.altitude,1500);assert.equal(a.sceneSkyClouds({coverage:0,speed:0,opacity:0}).opacity,0);
 const env=a.normalizeSceneEnvironment({sky:{mode:"physical",clouds:{coverage:2,direction:-90,scale:2}}});assert.deepEqual([env.sky.clouds.coverage,env.sky.clouds.direction,env.sky.clouds.scale],[1,270,100]);
 const identity=new Float32Array([1,0,0,0,0,1,0,0,0,0,1,0,0,0,0,1]);
 for(const elevation of [6,50,-2]){const r=elevation*Math.PI/180,e=bundle().environment;e.sky.sunDirection={x:0,y:Math.sin(r),z:-Math.cos(r)};const data=new Float32Array(64);a.sceneCloudUniformData({environment:e,view:identity,camera:{x:0,y:1.7,z:0,fov:60},aspect:1,timeSeconds:10,linear:true},data);assert.ok(Array.from(data).every(Number.isFinite));assert.ok(data[56]>0);assert.deepEqual(Array.from(data.slice(48,52)).map(x=>Math.round(x)),[8,0,10,5]);}
 h.renderer.dispose();
});
test("WebGPU clouds draw before ocean, reflect on ocean, cache while walking, reduce octaves and release on survival",async()=>{
 const h=await createBoardWebGPUHarness({fresh:true}),b=bundle();h.canvas.width=h.canvas.height=64;h.renderer.render(b,{width:64,height:64});
 const draws=h.fake.state.renderPasses.flatMap(p=>p.draws),cloud=draws.findIndex(d=>d.pipeline?.desc.label==="gosx-clouds"),ocean=draws.findIndex(d=>d.pipeline?.desc.label==="gosx-ocean");assert.ok(cloud>=0&&cloud<ocean);
 const shader=h.fake.state.shaderModules.find(m=>m.label==="gosx-ocean");assert.match(shader.code,/fn gosxClouds/);
 const n=h.fake.state.shaderModules.length;b.camera.x++;h.renderer.render(b,{width:64,height:64});assert.equal(h.fake.state.shaderModules.length,n);
 h.renderer.render(b,{width:64,height:64},{qualityEnabled:true,qualityProfile:{tier:"balanced"}});
 const uploads=h.fake.state.writeBufferCalls.filter(x=>x.data.length===64);assert.equal(uploads.at(-1).data[51],3);
 const buffers=h.fake.state.buffers.filter(x=>x.size===256);h.renderer.render(b,{width:64,height:64},{atmosphereTier:"survival"});assert.ok(buffers.some(b=>b.destroyed));h.renderer.dispose();
});
test("WebGL cloud/ocean uniforms and removal preserve legacy shader variants",()=>{
 const h=createWebGLRendererForPost({fresh:true}),gl=h.canvas.getContext("webgl2"),b=bundle(),sources=[];
 gl.isEnabled=()=>false;gl.createSampler=()=>({});gl.deleteSampler=gl.samplerParameteri=gl.bindSampler=()=>{};gl.blendFuncSeparate||=()=>{};
 gl.uniform4fv=(l,d)=>gl.ops.push(["cloudUniform",l.name,Array.from(d)]);const src=gl.shaderSource.bind(gl);gl.shaderSource=(s,c)=>{sources.push(c);src(s,c);};
 h.renderer.render(b,{width:64,height:64});assert.ok(sources.filter(x=>x.includes("vec4 gosxClouds")).length>=2);const data=gl.ops.filter(x=>x[0]==="cloudUniform"&&x[1]==="u_cloud[0]").at(-1);assert.equal(data[2][51],5);
 b.environment.sky.clouds=null;h.renderer.render(b,{width:64,height:64});assert.ok(gl.ops.some(x=>x[0]==="deleteProgram"));h.renderer.dispose();assert.deepEqual(h.warnLog,[]);
});
