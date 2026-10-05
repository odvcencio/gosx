"use strict";
const test = require("node:test"), assert = require("node:assert/strict");
const {FakeWebGLContext,createContext,installManualRAF,runScript,flushAsyncWork,bootstrapRuntimeSource,freshFeatureBundleSource,installManualTimers} = require("./runtime-test-harness.js");

function water(paused=false) {
 const entry = {id:"tide",resolution:16,surfaceResolution:4,seedDrops:0,activeObject:"None",objectKind:"none",renderPool:false,paused};
 for (const name of ["simulation","normal","pool","surface"]) {
  entry[name+"VertexGLES"] = "void main() {}";
  entry[name+"FragmentGLES"] = `void main() { /* ${name}-program */ }`;
 }
 return entry;
}
function harness(options={}) {
 let complete = !options.parallel;
 const contexts=[];
 const env=createContext({disableCanvas2D:true,prefersReducedMotion:options.reduced,createWebGL2Context() {
  const gl=new FakeWebGLContext(); contexts.push(gl);
  gl.createSampler=()=>({});gl.samplerParameteri=()=>{};gl.bindSampler=()=>{};gl.deleteSampler=()=>{};
  gl.HALF_FLOAT=0x140b; gl.FRAMEBUFFER_COMPLETE=0x8cd5; gl.checkFramebufferStatus=()=>gl.FRAMEBUFFER_COMPLETE;
  const extension=gl.getExtension.bind(gl), parameter=gl.getProgramParameter.bind(gl);
  gl.getExtension=name=>name==="KHR_parallel_shader_compile" && options.parallel ? {COMPLETION_STATUS_KHR:91}
   : ["EXT_color_buffer_float","OES_texture_float_linear"].includes(name) ? {} : extension(name);
  gl.getProgramParameter=(program,p)=>p===91 ? complete : p===gl.LINK_STATUS && options.reject && options.reject(gl.programShaderSources(program)) ? false : parameter(program,p);
  return gl;
 }});
 env.context.WebGL2RenderingContext=FakeWebGLContext; env.context.Event=Event;
 const create=env.document.createElement.bind(env.document);
 env.document.createElement=tag=>{
  const element=create(tag);
  if(tag==="canvas") {
   const dispatch=element.dispatchEvent.bind(element), get=element.getContext.bind(element);
   element.dispatchEvent=e=>{const result=dispatch(e);if(e.bubbles && element.parentNode) element.parentNode.dispatchEvent(e);return result;};
   element.getContext=(...args)=>{const gl=get(...args);if(gl instanceof FakeWebGLContext) gl.canvas=element;return gl;};
  }
  return element;
 };
 const timers=installManualTimers(env.context),raf=installManualRAF(env.context);
 runScript(bootstrapRuntimeSource,env.context,"bootstrap-runtime.js");
 let factory; const register=env.context.__gosx_register_engine_factory;
 env.context.__gosx_register_engine_factory=(name,value)=>{if(name==="GoSXScene3D") factory=value;register(name,value);};
 for(const name of ["scene3d","scene3d-webgl"]) runScript(freshFeatureBundleSource(name),env.context,name+".js");
 env.context.__gosx_scene3d_webgl_api.prepareScenePBRInitialRenderer=null;
 const mount=env.document.createElement("div"); env.document.body.appendChild(mount);
 let frame=0;
 async function advance(count=8) {for(let i=0;i<count;i++){timers.runDelay(0);timers.runDelay(16);await flushAsyncWork();raf.flush(++frame*16);}}
 async function start(scene) {const pending=factory({mount,emit(){},props:{capabilityTier:options.tier,preferWebGL:true,width:320,height:180,forceWebGL:true,scene:{objects:[],...scene}}});await advance();return pending;}
 return {env,contexts,mount,raf,factory,start,advance,setComplete(){complete=true;}};
}

test("adding an ocean through a mounted water command draws its grid",async t=>{
 const h=harness(), controller=await h.start({waterSystems:[water()]});t.after(()=>controller.dispose());
 assert.notEqual(h.mount.getAttribute("data-gosx-scene3d-ocean"),"surface");
 await controller.applyCommands([{kind:13,data:{environment:{ocean:{}},shape:"sceneIR"}}]);await h.advance();
 assert.equal(h.mount.getAttribute("data-gosx-scene3d-ocean"),"surface");
 assert.ok(h.contexts.some(gl=>gl.ops.some(op=>op[0]==="drawArrays" && op[3]>=96*128*6)),"live ocean grid reaches drawing");
});

test("paused water leaves an active ocean scheduled",async t=>{
 const h=harness(), c=await h.start({waterSystems:[water(true)],environment:{ocean:{}}});t.after(()=>c.dispose());
 assert.equal(h.mount.getAttribute("data-gosx-scene3d-render-loop-wants-animation"),"true");
 assert.notEqual(h.mount.getAttribute("data-gosx-scene3d-render-loop-reason"),"water-paused");
});

test("cloud-only mounts animate, pause, resume, and respect reduced motion",async t=>{
 for(const reduced of [false,true]) {
  const h=harness({reduced}),c=await h.start({environment:{sky:{mode:"physical",clouds:{coverage:0.45,speed:8}}}});t.after(()=>c.dispose());
  assert.equal(h.mount.getAttribute("data-gosx-scene3d-render-loop-wants-animation"),String(!reduced));
  c.setAnimationClock({timeSeconds:1,paused:true});await h.advance();
  assert.equal(h.mount.getAttribute("data-gosx-scene3d-render-loop-wants-animation"),"false");
  c.setAnimationClock({timeSeconds:1,paused:false});await h.advance();
  assert.equal(h.mount.getAttribute("data-gosx-scene3d-render-loop-wants-animation"),String(!reduced));

 }
});

for(const failure of ["surface","world"]) test(`${failure} shader rejection in mounted water stops with an explicit unsupported state`,async t=>{
 const h=harness({parallel:true,reject:source=>failure==="surface" ? source.includes("surface-program") : source.includes("u_normalUVScale") && !source.includes("a_instanceMatrix")});
 const c=await h.start({waterSystems:[water()],environment:failure==="world" ? {ocean:{}} : {}});t.after(()=>c.dispose());
 h.setComplete();await h.advance(16);
 assert.equal(h.mount.getAttribute("data-gosx-scene3d-backend"),"unsupported");
 assert.equal(h.mount.getAttribute("data-gosx-scene3d-ready"),"false");
 assert.match(h.mount.getAttribute("data-gosx-scene3d-renderer-fallback"),/shader-failed/);
 assert.equal(h.mount.getAttribute("data-gosx-scene3d-render-loop-wants-animation"),"false");
 const count=h.contexts.length;await h.advance(20);assert.equal(h.contexts.length,count,"failure does not repeatedly recreate the renderer");
});

test("superseded shader preparation does not retain a readiness listener",async t=>{
 const h=harness();let release,seen;
 const preparing=new Promise(resolve=>{seen=resolve;});
 h.env.context.__gosx_scene3d_webgl_api.prepareScenePBRInitialRenderer=()=>{seen();return new Promise(resolve=>{release=resolve;});};
 const abandoned=h.factory({mount:h.mount,props:{forceWebGL:true}});
 await preparing;
 assert.equal(h.mount.listenerCount("gosx:scene3d:program-ready"),0);
 h.env.context.__gosx_scene3d_webgl_api.prepareScenePBRInitialRenderer=null;
 const replacement=await h.start({environment:{ocean:{}}});t.after(()=>replacement.dispose());
 release(null);await abandoned;
 assert.equal(h.mount.listenerCount("gosx:scene3d:program-ready"),1);
 assert.doesNotThrow(()=>h.mount.dispatchEvent(new Event("gosx:scene3d:program-ready")));
 replacement.dispose();assert.equal(h.mount.listenerCount("gosx:scene3d:program-ready"),0);
});

test("still or invisible clouds leave a scene static", async t => {
 for (const clouds of [{speed:0},{coverage:0},{opacity:0}]) {
  const h=harness(),c=await h.start({environment:{sky:{mode:"physical",clouds}}});t.after(()=>c.dispose());
  assert.equal(h.mount.getAttribute("data-gosx-scene3d-render-loop-wants-animation"),"false");
 }
});

for (const tier of ["full", "balanced", "constrained"]) test(`cloud-only mount respects ${tier} atmosphere quality`, async t => {
 const h=harness({tier}),c=await h.start({environment:{sky:{mode:"physical",clouds:{coverage:0.45,speed:8}}}});t.after(()=>c.dispose());
 await h.advance();
 const draws=()=>h.contexts.flatMap(gl=>gl.ops).filter(op=>op[0]==="drawArrays").length;
 const before=draws();await h.advance(20);assert.equal(h.mount.getAttribute("data-gosx-scene3d-ready"),"true");assert.deepEqual(h.env.consoleLogs.error,[]);
 assert.equal(h.mount.getAttribute("data-gosx-scene3d-render-loop-wants-animation"),String(tier!=="constrained"));
 if(tier==="constrained") {assert.equal(draws(),before,"disabled clouds must not continuously redraw the sky");assert.equal(h.mount.getAttribute("data-gosx-scene3d-render-loop"),"stopped");}
 else assert.ok(draws()>before,"enabled clouds continue drawing");
});
