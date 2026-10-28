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
 const capture=p.find(x=>x.descriptor?.label==="gosx-reflection-capture");
 assert.equal(capture.draws[0].vertexCount,4); assert.equal(capture.draws[0].pipeline.desc.primitive.topology,"triangle-strip");
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

test("WebGL planar fallback draws built-in PBR outside the walking camera cull",()=>{
 const h=createWebGLRendererForPost({fresh:true}),gl=h.canvas.getContext("webgl2");glSupport(gl);const b=scene();b.camera.y=1.7;
 b.meshObjects=[{id:"reflection-quad",kind:"box",materialIndex:0,directVertices:true,vertexOffset:0,vertexCount:4,bounds:{minX:99,minY:0,minZ:0,maxX:101,maxY:2,maxZ:0},vertices:{count:4,positions:new Float32Array([99,0,0,101,0,0,101,2,0,99,2,0]),normals:new Float32Array([0,0,1,0,0,1,0,0,1,0,0,1]),uvs:new Float32Array([0,0,1,0,1,1,0,1]),indices:new Uint32Array([0,1,2,0,2,3])}}];b.materials=[{baseColor:"#ffffff",roughness:0.5}];
 h.renderer.render(b,{width:320,height:180});assert.ok(gl.ops.some(op=>op[0]==="drawElements"));assert.equal(b.camera.y,1.7);h.renderer.dispose();assert.deepEqual(h.warnLog,[]);
});

test("planar participation is explicit: elevated bridge components remain and only the opted-out moon leaves",async()=>{
 const h=await createBoardWebGPUHarness({fresh:true}),a=h.env.context.__gosx_scene3d_api;
 const pier={id:"pier",bounds:{minX:-10,maxX:10,minY:0,maxY:200,minZ:-960,maxZ:-940}};
 const deck={id:"bridge-deck",bounds:{minX:-200,maxX:200,minY:200,maxY:230,minZ:-1000,maxZ:-900}};
 const moon={id:"moon",planarReflection:false,bounds:{minX:20,maxX:140,minY:320,maxY:440,minZ:-1390,maxZ:-1270}};
 assert.deepEqual(Array.from(a.sceneReflectOpaqueList([pier,deck,moon],[]),o=>o.id),["pier","bridge-deck"]);
 assert.deepEqual(Array.from(a.sceneReflectOpaqueList([{...moon,planarReflection:true}],[]),o=>o.id),["moon"]);
 h.renderer.dispose();
});
test("live reflection changes retain geometry and restore participation without hiding the object",async()=>{
 const h=await createBoardWebGPUHarness({fresh:true}),a=h.env.context.__gosx_scene3d_api;
 const state=a.createSceneState({scene:{objects:[{id:"moon",kind:"sphere",radius:60,x:80,y:380,z:-1330}]}});
 const object=()=>state.objects.get("moon"), original=object();
 assert.equal(original.planarReflection,true);
 a.applySceneCommands(state,[{kind:2,objectId:"moon",data:{planarReflection:false}}]);
 assert.equal(object().planarReflection,false);assert.equal(object().radius,60);assert.equal(object().visible,true);
 a.applySceneCommands(state,[{kind:2,objectId:"moon",data:{planarReflection:true}}]);
 assert.equal(object().planarReflection,true);assert.equal(object().radius,60);assert.equal(object().visible,true);
 h.renderer.dispose();
});

function seaBundle(api, meshes, camera, level = 0) {
 const objects = api.sceneStateObjectsWithMaterials(api.createSceneState({ scene: { objects: meshes.map((m) => Object.assign({ kind: "sphere", material: { color: "#d8dce4", wireframe: false }, wireframe: false }, m)) } }));
 const bundle = api.createSceneRenderBundle(320, 180, "#101820", camera, objects, [], [], [], [], {}, 0.4, [], [], [], [], [], 0, false, {});
 bundle.environment = { sky: { mode: "physical", sunDirection: { x: -0.35, y: 0.7, z: 0.62 }, turbidity: 3 }, ocean: { level, extent: 8000, reflections: { mode: "ssr+planar", resolution: 0.5, strength: 0.8 } } };
 bundle.lights = [];
 const verts = Object.fromEntries(bundle.meshObjects.map((o) => [o.id, o.vertexCount]));
 assert.equal(Object.keys(verts).length, meshes.length, "every mesh reaches the bundle");
 assert.equal(new Set(Object.values(verts)).size, meshes.length, "each mesh has its own vertex count");
 return { bundle, verts };
}
const sortedNumbers = (list) => Array.from(list).sort((x, y) => x - y);
// The vertex counts drawn into the planar mirror, and those drawn everywhere else.
function webgpuSeaDraws(meshes, camera, level) {
 return createBoardWebGPUHarness({ fresh: true }).then((h) => {
  const create = h.fake.device.createRenderPipeline.bind(h.fake.device);
  h.fake.device.createRenderPipeline = (d) => Object.assign(create(d), { getBindGroupLayout: () => h.fake.device.createBindGroupLayout({ entries: [] }) });
  const { bundle, verts } = seaBundle(h.env.context.__gosx_scene3d_api, meshes, camera, level);
  h.canvas.width = 320; h.canvas.height = 180;
  h.renderer.render(bundle, { width: 320, height: 180 });
  const passes = h.fake.state.renderPasses, planar = passes.filter((p) => p.descriptor?.label === "gosx-planar-reflection");
  assert.equal(planar.length, 1, "the planar mirror is drawn once");
  const drawn = (list) => list.flatMap((p) => p.draws.map((d) => d.vertexCount));
  const out = { verts, planar: drawn(planar), elsewhere: drawn(passes.filter((p) => !planar.includes(p))) };
  h.renderer.dispose();
  return out;
 });
}
function webglSeaDraws(meshes, camera, level) {
 const h = createWebGLRendererForPost({ fresh: true }), gl = h.canvas.getContext("webgl2"); glSupport(gl);
 const { bundle, verts } = seaBundle(h.env.context.__gosx_scene3d_api, meshes, camera, level);
 h.renderer.render(bundle, { width: 320, height: 180 });
 // The mirror renders into a half-resolution target (160 x 90 here); the main view and the ocean use 320 x 180.
 const planar = [], elsewhere = []; let into = elsewhere;
 for (const op of gl.ops) {
  if (op[0] === "viewport") into = op[3] === 160 ? planar : elsewhere;
  else if (op[0] === "drawArrays") into.push(op[3]);
  else if (op[0] === "drawElements") into.push(op[2]);
 }
 h.renderer.dispose(); assert.deepEqual(h.warnLog, []);
 return { verts, planar, elsewhere };
}
function assertMirrors(got, kept, left) {
 const want = kept.map((id) => got.verts[id]);
 assert.deepEqual(sortedNumbers(got.planar), sortedNumbers(want), `the mirror draws ${kept.join(", ")} and nothing else`);
 for (const id of left) assert.ok(got.elsewhere.includes(got.verts[id]), `the main view still draws ${id}`);
}

const participationCamera={x:0,y:2,z:0,rotationX:-.05,rotationY:0,fov:62,near:.5,far:20000};
const participationMeshes=[{id:"pier",segments:12,radius:30,x:0,y:100,z:-950},{id:"bridge-deck",segments:18,radius:15,x:0,y:215,z:-950},{id:"moon",segments:24,radius:60,x:80,y:380,z:-1330,planarReflection:false}];
test("WebGPU planar reflection keeps the elevated deck while excluding only the opted-out mesh",async()=>{assertMirrors(await webgpuSeaDraws(participationMeshes,participationCamera,0),["pier","bridge-deck"],["moon"])});
test("WebGL planar reflection keeps the elevated deck while excluding only the opted-out mesh",()=>{assertMirrors(webglSeaDraws(participationMeshes,participationCamera,0),["pier","bridge-deck"],["moon"])});
