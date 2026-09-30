// Validate the actual assembled shaders without starting a browser.
import { createRequire } from "node:module";
import { mkdirSync, writeFileSync } from "node:fs";
import { execFileSync } from "node:child_process";
import os from "node:os";
const require = createRequire(import.meta.url);
const {createBoardWebGPUHarness,createWebGLRendererForPost,makePointsBundle} = require("../client/js/runtime-test-harness.js");
const directory = "/tmp/gosx-atmosphere-shaders"; mkdirSync(directory,{recursive:true});
const bundle = makePointsBundle([{kind:"toneMapping",mode:"agx",exposure:0.7},{kind:"godRays",intensity:0.15},{kind:"grain",intensity:0.015}]); bundle.points=[];
bundle.environment.sky={mode:"physical",clouds:{coverage:0.45}};
bundle.environment.ocean={reflections:{mode:"ssr+planar"}};
bundle.environment.haze={density:0.002,heightFalloff:0.04,sunScatter:0.25};
const h=createWebGLRendererForPost({fresh:true}),gl=h.canvas.getContext("webgl2"),shaders=[];
gl.isEnabled=()=>false;gl.createSampler=()=>({});gl.deleteSampler=gl.samplerParameteri=gl.bindSampler=()=>{};
gl.uniform4fv||=()=>{};gl.blendFuncSeparate||=()=>{};
const get=gl.getParameter.bind(gl);let bound=null;gl.FRAMEBUFFER_BINDING=0x8ca6;
const bind=gl.bindFramebuffer.bind(gl);gl.bindFramebuffer=(t,f)=>{if(t===gl.FRAMEBUFFER)bound=f;bind(t,f);};gl.getParameter=p=>p===gl.FRAMEBUFFER_BINDING?bound:get(p);
const src=gl.shaderSource.bind(gl);gl.shaderSource=(s,c)=>{shaders.push([s.type,c]);src(s,c);};
h.renderer.render(bundle,{width:64,height:64});
for(let i=0;i<shaders.length;i++){
 const [type,code]=shaders[i],stage=type===gl.VERTEX_SHADER?"vert":"frag",path=`${directory}/atmosphere-${i}.${stage}`;
 writeFileSync(path,code);execFileSync("/usr/bin/glslangValidator",["-S",stage,path],{stdio:"pipe"});
 console.log(`/usr/bin/glslangValidator -S ${stage} ${path}`);
}
h.renderer.dispose();
const g=await createBoardWebGPUHarness({fresh:true}),create=g.fake.device.createRenderPipeline.bind(g.fake.device);
g.fake.device.createRenderPipeline=d=>Object.assign(create(d),{getBindGroupLayout:()=>g.fake.device.createBindGroupLayout({entries:[]})});
g.canvas.width=g.canvas.height=64;g.renderer.render(bundle,{width:64,height:64});
const names=["gosx-ocean","gosx-reflection-capture","gosx-clouds","gosx-atmosphere","gosx-atmosphere-post"];
for(const [i,module] of g.fake.state.shaderModules.entries()){
 if(!names.includes(module.label))continue;
 const path=`${directory}/${module.label}-${i}.wgsl`;writeFileSync(path,module.code);
 execFileSync(`${os.homedir()}/.cargo/bin/naga`,[path],{stdio:"pipe"});console.log(`~/.cargo/bin/naga ${path}`);
}
g.renderer.dispose();
