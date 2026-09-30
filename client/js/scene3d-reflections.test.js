"use strict";
const test = require("node:test"), assert = require("node:assert/strict");
const {createBoardWebGPUHarness,createWebGLRendererForPost,makePointsBundle} = require("./runtime-test-harness.js");
function scene() { const b = makePointsBundle(null); b.points = []; b.environment.sky = {mode:"physical"}; b.environment.ocean = {reflections:{mode:"ssr+planar",resolution:0.5,strength:0.8}}; return b; }
function glSupport(gl) {
 gl.isEnabled = () => false; gl.createSampler = () => ({}); gl.deleteSampler = gl.samplerParameteri = gl.bindSampler = () => {};
 gl.uniform4fv ||= () => {}; gl.blendFuncSeparate ||= () => {};
 const get = gl.getParameter.bind(gl); let bound = null;
 gl.FRAMEBUFFER_BINDING = 0x8ca6;
 const bind = gl.bindFramebuffer.bind(gl); gl.bindFramebuffer = (t,f) => { if (t===gl.FRAMEBUFFER) bound=f; bind(t,f); };
 gl.getParameter = p => p===gl.FRAMEBUFFER_BINDING ? bound : get(p);
}
test("reflection contracts default only on opt-in and quality sheds planar before SSR",async () => {
 const h=await createBoardWebGPUHarness({fresh:true}), a=h.env.context.__gosx_scene3d_api;
 assert.equal(a.sceneOceanReflections({mode:""}),null);
 const c=a.sceneOceanReflections({mode:" SSR+Planar "}); assert.deepEqual(JSON.parse(JSON.stringify(c)),{mode:"ssr+planar",resolution:0.5,strength:1});
 const full=a.sceneAtmosphereQuality({qualityEnabled:true,qualityProfile:{tier:"full"}});
 const balanced=a.sceneAtmosphereQuality({qualityEnabled:true,qualityProfile:{tier:"balanced"}});
 const off=a.sceneAtmosphereQuality({atmosphereTier:"survival"});
 assert.deepEqual([full.planar,full.reflectionSteps,balanced.planar,balanced.reflectionSteps,off.reflections],[true,20,false,10,false]);
 const state=a.createSceneState({scene:{environment:{ocean:{reflections:{mode:"ssr"}}}}}); assert.equal(state.environment.ocean.reflections.mode,"ssr");
 h.renderer.dispose();
});
test("WebGPU captures before ocean, allocates reduced targets, caches variants across walking, releases on survival",async () => {
 const h=await createBoardWebGPUHarness({fresh:true});
 const create=h.fake.device.createRenderPipeline.bind(h.fake.device);
 h.fake.device.createRenderPipeline=d => Object.assign(create(d),{getBindGroupLayout:()=>h.fake.device.createBindGroupLayout({entries:[]})});
 const b=scene(); h.canvas.width=h.canvas.height=64;
 h.renderer.render(b,{width:64,height:64});
 const p=h.fake.state.renderPasses; assert.ok(p.some(x=>x.descriptor?.label==="gosx-reflection-capture")); assert.ok(p.some(x=>x.descriptor?.label==="gosx-planar-reflection"));
 const ocean=h.fake.state.shaderModules.find(x=>x.label==="gosx-ocean"); assert.match(ocean.code,/oceanGeometryReflection/); assert.match(ocean.code,/j < 5/);
 const count=h.fake.state.shaderModules.length; b.camera.x+=1; h.renderer.render(b,{width:64,height:64}); assert.equal(h.fake.state.shaderModules.length,count);
 const owned=h.fake.state.textures.filter(t=>t.desc.label==="gosx-reflection"); assert.ok(owned.length>0); assert.ok(owned.every(t=>t.desc.size[0]===32));
 const before=p.length; h.renderer.render(b,{width:64,height:64},{qualityEnabled:true,qualityRevision:1,qualityProfile:{tier:"survival"}});
 assert.equal(p.slice(before).filter(x=>x.descriptor?.label==="gosx-reflection-capture").length,0); assert.ok(owned.every(t=>t.destroyed));
 h.renderer.dispose(); assert.ok(h.fake.state.buffers.filter(x=>/gosx-reflection/.test(x.desc?.label||x.label||"")).every(x=>x.destroyed));
});
test("WebGL allocates reflections only when enabled and disposes snapshots",() => {
 const h=createWebGLRendererForPost({fresh:true}),gl=h.canvas.getContext("webgl2");glSupport(gl);
 const b=scene(), shaders=[];const src=gl.shaderSource.bind(gl);gl.shaderSource=(s,c)=>{shaders.push(c);src(s,c);};
 h.renderer.render(b,{width:320,height:180}); assert.ok(shaders.some(s=>s.includes("oceanGeometryReflection")));assert.ok(gl.ops.some(x=>x[0]==="blitFramebuffer"));
 const n=shaders.length; b.camera.x+=1;h.renderer.render(b,{width:320,height:180});assert.equal(shaders.length,n);
 h.renderer.render(b,{width:320,height:180},{qualityEnabled:true,qualityProfile:{tier:"survival"}});assert.ok(gl.ops.some(x=>x[0]==="deleteFramebuffer"));h.renderer.dispose();assert.deepEqual(h.warnLog,[]);
});
module.exports={glSupport,scene};
