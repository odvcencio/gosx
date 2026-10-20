"use strict";
const test=require("node:test"),assert=require("node:assert/strict");
const {createBoardWebGPUHarness,createWebGLRendererForPost,makePointsBundle}=require("./runtime-test-harness.js");
function bundle(){const b=makePointsBundle(null);b.points=[];b.environment.sky={mode:"physical",sunDirection:{x:0,y:0.1,z:-1}};b.environment.haze={density:0.002,heightFalloff:0.04,sunScatter:0.25};b.environment.fogDensity=0.02;b.postEffects=[{kind:"godRays"},{kind:"bloom",mode:"mip",threshold:1.2,intensity:0.12},{kind:"toneMapping",mode:"agx",exposure:0.7},{kind:"grain"}];return b;}
function support(gl){gl.deleteRenderbuffer ||=()=>{};gl.isEnabled=()=>false;gl.createSampler=()=>({});gl.deleteSampler=gl.samplerParameteri=gl.bindSampler=()=>{};gl.blendFuncSeparate ||=()=>{};gl.uniform4fv ||=()=>{};}
test("haze/post contracts keep browser defaults, replace flat fog, fuse shafts and preserve grain after AgX",async()=>{
 const h=await createBoardWebGPUHarness({fresh:true}),a=h.env.context.__gosx_scene3d_api,b=bundle();
 const normalized=a.sceneAtmosphereEffects(b.postEffects,b.environment,{});assert.deepEqual([normalized[0].rays.samples,normalized[0].rays.intensity,normalized[0].rays.decay,normalized[0].rays.density,normalized[3].grain],[32,0.18,0.96,0.9,0.015]);
 const frame=a.sceneAtmosphereBundle(b,{});assert.equal(frame.environment.fogDensity,0);assert.equal(b.environment.fogDensity,0.02);
 assert.deepEqual(Array.from(frame.postEffects,e=>e.kind),["atmosphere","bloom","atmosphere","atmosphere"]);assert.ok(frame.postEffects[0].haze&&frame.postEffects[0].rays);
 const off=a.sceneAtmosphereBundle(b,{atmosphereTier:"survival"});assert.equal(off.environment.fogDensity,0.02);assert.equal(off.postEffects.length,2);assert.ok(off.postEffects[1].agx);
 const zero=a.sceneAtmosphereEffects([{kind:"godRays",intensity:0},{kind:"grain",intensity:0}],{sky:{mode:"physical"},haze:{density:0}},{});assert.equal(zero.length,0);
 const clamped=a.sceneAtmosphereEffects([{kind:"godRays",intensity:3,decay:2,density:3,samples:128},{kind:"grain",intensity:1}],{sky:{mode:"physical"},haze:{}},{});assert.deepEqual([clamped[0].rays.intensity,clamped[0].rays.decay,clamped[0].rays.density,clamped[0].rays.samples,clamped[1].grain],[2,1,2,64,0.1]);assert.deepEqual([clamped[0].haze.density,clamped[0].haze.heightFalloff,clamped[0].haze.sunScatter],[0.0015,0.04,0.25]);
 const old=makePointsBundle(null);assert.equal(a.sceneAtmosphereBundle(old,{}),old);
 const state=a.createSceneState({scene:{environment:{haze:{density:0.003}}}});assert.equal(state.environment.haze.density,0.003);h.renderer.dispose();
});
test("WebGPU atmosphere uses reduced shafts and depth bindings, stable shader keys, and quality disposal",async()=>{
 const h=await createBoardWebGPUHarness({fresh:true}),b=bundle();h.canvas.width=h.canvas.height=64;h.renderer.render(b,{width:64,height:64});
 const modules=h.fake.state.shaderModules.filter(m=>m.label?.startsWith("post-atmosphere:"));assert.equal(modules.length,4);assert.ok(modules.some(m=>/gosxAgX/.test(m.code)));assert.ok(modules.some(m=>/normalization/.test(m.code)));
 const shafts=h.fake.state.textures.filter(t=>t.desc.label==="gosx-sun-shafts");assert.equal(shafts.length,1);assert.equal(shafts[0].desc.size[0],32);
 const uniforms=h.fake.state.writeBufferCalls.filter(w=>w.data.length===60);assert.equal(uniforms[0].data[47],32);assert.equal(uniforms[0].data[20],Math.fround(0.002));
 const n=h.fake.state.shaderModules.length;b.camera.x+=1;h.renderer.render(b,{width:64,height:64});assert.equal(h.fake.state.shaderModules.length,n);
 h.renderer.render(b,{width:64,height:64},{qualityEnabled:true,qualityProfile:{tier:"balanced"}});assert.equal(h.fake.state.writeBufferCalls.filter(w=>w.data.length===60&&w.data[47]>0).at(-1).data[47],12);assert.ok(shafts[0].destroyed);assert.equal(h.fake.state.textures.filter(t=>t.desc.label==="gosx-sun-shafts").at(-1).desc.size[0],16);
 h.renderer.render(b,{width:64,height:64},{atmosphereTier:"survival"});assert.ok(h.fake.state.textures.filter(t=>t.desc.label==="gosx-sun-shafts").every(t=>t.destroyed));h.renderer.dispose();
});
test("WebGL grade and shafts upload uniforms, preserve shader counts while walking and dispose targets",()=>{
 const h=createWebGLRendererForPost({fresh:true}),gl=h.canvas.getContext("webgl2"),b=bundle(),sources=[];support(gl);
 gl.uniform4fv=(loc,d)=>gl.ops.push(["atmosphereUniform",loc.name,Array.from(d)]);const shader=gl.shaderSource.bind(gl);gl.shaderSource=(s,c)=>{sources.push(c);shader(s,c);};
 h.renderer.render(b,{width:64,height:64});assert.ok(sources.some(x=>/vec3 gosxAgX/.test(x)));assert.ok(sources.some(x=>/normalization/.test(x)));const uploads=gl.ops.filter(o=>o[0]==="atmosphereUniform"&&o[1]==="u_atmo[0]");assert.equal(uploads[0][2][47],32);assert.equal(uploads.length,4);
 const n=sources.length;b.camera.x++;h.renderer.render(b,{width:64,height:64});assert.equal(sources.length,n);h.renderer.render(b,{width:64,height:64},{atmosphereTier:"survival"});h.renderer.dispose();assert.ok(h.warnLog.every(x=>x.includes("RGBA8 LDR")));
});
test("sky lighting stays finite through all three sun periods and shuts shafts below the horizon",async()=>{
 const h=await createBoardWebGPUHarness({fresh:true}),a=h.env.context.__gosx_scene3d_api;
 const identity=new Float32Array([1,0,0,0,0,1,0,0,0,0,1,0,0,0,0,1]);
 for(const el of [6,50,-2]){const b=bundle(),r=el*Math.PI/180;b.environment.sky.sunDirection={x:0,y:Math.sin(r),z:-Math.cos(r)};const effect=a.sceneAtmosphereEffects(b.postEffects,b.environment,{})[0],out=new Float32Array(60);a.sceneAtmospherePostUniforms(effect,{environment:b.environment,camera:{x:0,y:1.7,z:0},viewProj:identity,meta:{}},false,out);assert.ok(Array.from(out).every(Number.isFinite));if(el<0)assert.equal(out[42],0);}
 h.renderer.dispose();
});

test("WebGPU depth sampling uses the actual MSAA target; legacy scenes allocate no atmosphere buffers",async()=>{
 const h=await createBoardWebGPUHarness({fresh:true});h.canvas.width=h.canvas.height=64;const old=makePointsBundle(null);old.points=[];old.environment.sky={mode:"physical"};h.renderer.render(old,{width:64,height:64});assert.equal(h.fake.state.shaderModules.filter(m=>m.label?.startsWith("post-atmosphere:")).length,0);assert.equal(h.fake.state.buffers.filter(b=>b.size===240).length,0);
 const b=bundle();b.msaaSamples=4;h.renderer.render(b,{width:64,height:64});const source=h.fake.state.shaderModules.filter(m=>m.label?.startsWith("post-atmosphere:")&&m.label.endsWith(":4"));assert.equal(source.length,2);assert.ok(source.every(m=>m.code.includes("texture_depth_multisampled_2d")&&m.code.includes("i<4")));h.renderer.dispose();assert.ok(h.fake.state.buffers.filter(b=>b.size===240).every(b=>b.destroyed));
});
