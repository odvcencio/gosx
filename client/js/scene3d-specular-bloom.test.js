"use strict";
const test = require("node:test"), assert = require("node:assert/strict");
const { createWebGLRendererForPost, createBoardWebGPUHarness, flushAsyncWork } = require("./runtime-test-harness.js");
function bundle(h, source = "specular", material = {}) {
 const api = h.env.context.__gosx_scene3d_api;
 const b = api.createSceneRenderBundle(640, 512, "#ffffff", {x:0,y:0,z:5,fov:60,near:.1,far:100},
  [{id:"solid",kind:"mesh",vertices:{count:3,positions:[-1,-1,0,1,-1,0,0,1,0],normals:[0,0,1,0,0,1,0,0,1],uvs:[0,0,1,0,.5,1]},materialKind:"standard",wireframe:false,color:"#ffffff",...material}],[],[],[],[],{},0,[],[],[],[],[],0,false);
 assert.equal(b.meshObjects.length,1,"real mesh fixture");
 b.postFXMaxPixels = 160*128;
 b.postEffects = [{kind:"bloom",mode:"mip",source,threshold:.7,intensity:.1},{kind:"toneMapping"}];
 return b;
}

test("WebGL selective source is a separate bounded attachment; disable and dispose release it",()=>{
 const h=createWebGLRendererForPost({fresh:true}), gl=h.canvas.getContext("webgl2");
 gl.getExtension=name=>name==="EXT_color_buffer_float"?{}:null;
 gl.deleteRenderbuffer=buffer=>gl.ops.push(["deleteRenderbuffer",buffer.id]);
 gl.COLOR_ATTACHMENT1=0x8ce1; gl.COLOR=0x1800;
 const attachments=[],textures=[],clears=[],drawBuffers=[]; let bound=null;
 const bt=gl.bindTexture.bind(gl);gl.bindTexture=(target,tex)=>{bound=tex;return bt(target,tex);};
 const image=gl.texImage2D.bind(gl);gl.texImage2D=(...a)=>{if(a.length===9)textures.push({tex:bound,width:a[3],height:a[4],format:a[2]});return image(...a);};
 const fb=gl.framebufferTexture2D.bind(gl);gl.framebufferTexture2D=(...a)=>{if(a[1]===gl.COLOR_ATTACHMENT1)attachments.push(a[3]);return fb(...a);};
 const db=gl.drawBuffers?.bind(gl);gl.drawBuffers=b=>{drawBuffers.push(b);if(db)db(b);};
 gl.clearBufferfv=(target,index,value)=>clears.push({target,index,value:Array.from(value)});
 h.canvas.width=640;h.canvas.height=512;
 h.renderer.render(bundle(h,"color"),{width:640,height:512});
 assert.equal(attachments.length,0,"default allocates no signal target");
 h.renderer.render(bundle(h),{width:640,height:512});
 const first=attachments.find(Boolean); assert.ok(first);
 assert.deepEqual(textures.find(t=>t.tex===first),{tex:first,width:160,height:128,format:gl.RGBA16F});
 assert.ok(clears.some(c=>c.index===1&&c.value.every(v=>v===0)),"bright white background never seeds the signal");
 assert.ok(drawBuffers.some(a=>a.length===2),"same-pass depth/coverage MRT is active");
 assert.equal(attachments.at(-1),null,"post pass detaches extraction texture to avoid feedback");
 h.renderer.render(bundle(h),{width:640,height:512});
 assert.equal(textures.filter(t=>t.tex===first).length,1,"steady frame reuses signal texture");
 const resized=bundle(h);resized.postFXMaxPixels=80*64;h.renderer.render(resized,{width:640,height:512});
 assert.ok(gl.ops.some(o=>o[0]==="deleteTexture"&&o[1]===first.id),"resize retires old signal");
 const smaller=attachments.filter(Boolean).at(-1);assert.equal(textures.find(t=>t.tex===smaller).width,80);
 h.renderer.render(bundle(h,"color"),{width:640,height:512});
 assert.ok(gl.ops.some(o=>o[0]==="deleteTexture"&&o[1]===first.id));
 h.renderer.render(bundle(h),{width:640,height:512});const last=attachments.filter(Boolean).at(-1);
 h.renderer.dispose();assert.ok(gl.ops.some(o=>o[0]==="deleteTexture"&&o[1]===last.id));
 assert.deepEqual(h.warnLog,[]);
});

test("WebGPU selective pipeline and MSAA target share bounded size and preserve color input",async()=>{
 const h=await createBoardWebGPUHarness({fresh:true});h.canvas.width=640;h.canvas.height=512;
 const render=async b=>{h.renderer.render(b,{width:640,height:512});await flushAsyncWork();h.renderer.render(b,{width:640,height:512});};
 await render(bundle(h,"color"));assert.ok(!h.fake.state.textures.some(t=>t.desc.label==="gosx-specular-bloom"));
 await render(bundle(h));
 const signal=h.fake.state.textures.find(t=>t.desc.label==="gosx-specular-bloom");assert.ok(signal);assert.deepEqual(Array.from(signal.desc.size),[160,128,1]);
 const main=h.fake.state.renderPasses.findLast(p=>p.descriptor.colorAttachments?.length===2);
 assert.ok(main,"actual draw has both color attachments");assert.equal(main.descriptor.colorAttachments[1].clearValue.r,0);
 const draw=main.draws.find(d=>d.pipeline?.desc.fragment?.entryPoint==="fragmentMainSpecular");assert.ok(draw,"standard material selects specular entrypoint");
 const prefilter=h.fake.state.renderPasses.findLast(p=>p.draws.some(d=>d.pipeline?.desc.fragment?.module.label==="post-bloomMipPrefilter"));
 const composite=h.fake.state.renderPasses.findLast(p=>p.draws.some(d=>d.pipeline?.desc.fragment?.module.label==="post-bloomComposite"));
 const source=d=>d.bindGroups[0].group.desc.entries[0].resource;
 assert.equal(source(prefilter).textureId,signal.id);assert.notEqual(source(composite).textureId,signal.id,"compose original color, not AOV");
 const resized=bundle(h);resized.postFXMaxPixels=80*64;await render(resized);
 assert.ok(signal.destroyed,"resize releases the old target");
 const smaller=h.fake.state.textures.findLast(t=>t.desc.label==="gosx-specular-bloom");assert.deepEqual(Array.from(smaller.desc.size),[80,64,1]);
 await render(bundle(h,"color"));assert.ok(smaller.destroyed,"turning selective source off frees the target");
 const msaa=bundle(h);msaa.msaaSamples=4;await render(msaa);
 const multisampled=h.fake.state.textures.findLast(t=>t.desc.label==="gosx-specular-bloom-msaa");assert.equal(multisampled.desc.sampleCount,4);assert.deepEqual(Array.from(multisampled.desc.size),[160,128,1]);
 const live=h.fake.state.textures.filter(t=>t.desc.label==="gosx-specular-bloom").at(-1);
 h.renderer.dispose();assert.ok(live.destroyed);assert.ok(multisampled.destroyed);assert.equal(h.renderer.getFailureReason(), "");
});

test("missing custom signal disables only selective bloom instead of inferring diffuse light",()=>{
 const h=createWebGLRendererForPost({fresh:true});const api=h.env.context.__gosx_scene3d_api;
 const effects=[{kind:"bloom",source:"specular"},{kind:"bloom"},{kind:"toneMapping"}];
 const filtered=api.sceneSpecularBloomEffects({materials:[{customFragment:"authored-discard-shader"}]},effects,"webgl",false,null);
 assert.deepEqual(Array.from(filtered,e=>e.kind+":"+(e.source||"color")),["bloom:color","toneMapping:color"]);
 const retained=api.sceneSpecularBloomEffects({materials:[{customFragment:"normal",specularFragmentGLSL:"compiler-zero-coverage"}]},effects,"webgl",false,null);
 assert.equal(retained,effects);h.renderer.dispose();
});
