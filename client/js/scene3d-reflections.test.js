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

// The sea's mirror and sky bodies. A sky body (a mesh more than 800 m away and
// more than 8 degrees above the sea, such as a moon built as geometry) is not
// mirrored: its image lands in the near water as a hard-edged disc, and the
// sky already supplies the glint. Near and low geometry still reflects.
test("the sea's mirror keeps near and low geometry and leaves out sky bodies: a mesh far away and high in the sky is not drawn into the planar reflection", async () => {
 const h = await createBoardWebGPUHarness({ fresh: true }), a = h.env.context.__gosx_scene3d_api;
 const box = (x, y, z, r) => ({ minX: x - r, maxX: x + r, minY: y - r, maxY: y + r, minZ: z - r, maxZ: z + r });
 const ship = { id: "ship", bounds: box(0, 20, -120, 40) }, moon = { id: "moon", bounds: box(80, 380, -1330, 60) };
 const mountain = { id: "mountain", bounds: box(900, 250, -2500, 300) }, noBounds = { id: "plain" }, kite = { id: "kite", bounds: box(0, 90, -400, 5) };
 const camera = { x: 0, y: 26, z: 95 };
 const ids = (list) => Array.from(list, (o) => o.id);
 const all = [ship, moon, mountain, noBounds, kite];
 assert.deepEqual(ids(a.sceneReflectOpaqueList(all, [], camera, 0)), ["ship", "mountain", "plain", "kite"],
  "the moon (1.4 km away, 16 degrees up) leaves; the ship, a mountain at 5 degrees, a mesh without bounds and a near kite stay");
 assert.deepEqual(ids(a.sceneReflectOpaqueList(all, [])), ids(all), "without a camera nothing is judged");
 h.renderer.dispose();
});

test("sky bodies are judged against the sea level, from valid bounds, and beside the custom-material rule", async () => {
 const h = await createBoardWebGPUHarness({ fresh: true }), a = h.env.context.__gosx_scene3d_api;
 const at = (id, x, y, z, r = 10) => ({ id, bounds: { minX: x - r, maxX: x + r, minY: y - r, maxY: y + r, minZ: z - r, maxZ: z + r } });
 const ids = (list) => Array.from(list, (o) => o.id), camera = { x: 0, y: 26, z: 0 };
 const left = (list, materials = [], level = 0) => ids(a.sceneReflectOpaqueList(list, materials, camera, level));
 // 1,000 m out, the 8 degree line is 140.5 m up (tan 8 degrees = 0.1405): 141 m is a sky body, 140 m is not.
 assert.deepEqual(left([at("above-line", 0, 141, -1000), at("below-line", 0, 140, -1000)]), ["below-line"]);
 // Distance counts too: 500 m out and 300 m up (31 degrees, 570 m from the camera) is near geometry, not sky.
 assert.deepEqual(left([at("close", 0, 300, -500)]), ["close"], "a body under 800 m away stays, however high");
 // The sea level is the horizon: raise the sea to 400 m and the moon's centre (380 m) is below it.
 assert.deepEqual(left([at("moon", 80, 380, -1330, 60)], [], 0), []);
 assert.deepEqual(left([at("moon", 80, 380, -1330, 60)], [], 400), ["moon"], "the sea level is the horizon, not y = 0");
 assert.deepEqual(left([at("moon", 80, 380, -1330, 60)], [], NaN), [], "a level that is not a number reads as 0");
 assert.deepEqual(left([at("moon", 80, 380, -1330, 60)], [], "deep"), [], "so does text");
 // Bounds that are not finite, or incomplete, are not judged.
 const bad = [{ id: "nan", bounds: { minX: NaN, maxX: 0, minY: 900, maxY: 1000, minZ: -2000, maxZ: -1900 } },
  { id: "partial", bounds: { minX: 0, maxX: 0, minY: 900, maxY: 1000 } }, { id: "inf", bounds: { minX: -Infinity, maxX: Infinity, minY: 900, maxY: 1000, minZ: -2000, maxZ: -1900 } },
  { id: "unbounded-up", bounds: { minX: -10, maxX: 10, minY: 900, maxY: Infinity, minZ: -2010, maxZ: -1990 } },
  { id: "text", bounds: { minX: "-10", maxX: "10", minY: "900", maxY: "1000", minZ: "-2010", maxZ: "-1990" } }];
 assert.deepEqual(left(bad), ["nan", "partial", "inf", "unbounded-up", "text"], "geometry without valid bounds stays reflected");
 assert.deepEqual(left([at("same-but-valid", 0, 1000, -2000)]), [], "the same place with valid bounds is a sky body");
 // The rule that was already there still applies to the same list.
 const sky = at("sky", 0, 900, -2000), shipFar = at("ship", 0, 10, -900), custom = Object.assign(at("custom", 0, 5, -50), { materialIndex: 1 });
 const materials = [{ baseColor: "#ffffff" }, { customFragment: "void main() {}" }];
 assert.deepEqual(left([sky, shipFar, custom], materials), ["ship"], "custom shaders stay out of the fallback and sky bodies join them");
 h.renderer.dispose();
});

// Both renderers mirror the same meshes. Spheres with different segment counts
// have different vertex counts, so a recorded draw names the mesh that made it.
const seaCamera = { x: 0, y: 26, z: 95, rotationX: -0.2, rotationY: 0, fov: 62, near: 0.5, far: 20000 };
const seaMeshes = [
 { id: "moon", segments: 24, radius: 60, x: 80, y: 380, z: -1330 }, // 1.4 km away, 16 degrees up: a sky body
 { id: "buoy", segments: 12, radius: 4, x: 6, y: 5, z: -60 }, // near
 { id: "island", segments: 16, radius: 300, x: 900, y: 250, z: -2500 }, // far but 5 degrees up: stays
];
function seaBundle(api, meshes, camera, level = 0) {
 const objects = api.sceneStateObjectsWithMaterials(api.createSceneState({ scene: { objects: meshes.map((m) => ({ id: m.id, kind: "sphere", radius: m.radius, segments: m.segments, x: m.x, y: m.y, z: m.z, material: { color: "#d8dce4", wireframe: false }, wireframe: false })) } }));
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
test("WebGPU leaves a distant high mesh out of the planar mirror and still draws it in the main view", async () => {
 assertMirrors(await webgpuSeaDraws(seaMeshes, seaCamera, 0), ["buoy", "island"], ["moon"]);
});
test("WebGL leaves a distant high mesh out of the planar mirror and still draws it in the main view", () => {
 assertMirrors(webglSeaDraws(seaMeshes, seaCamera, 0), ["buoy", "island"], ["moon"]);
});
test("both renderers judge sky bodies from the real camera, not the mirrored one", async () => {
 // A viewer 400 m up. The tower is 743 m from the real camera, so it is near geometry and stays; it is 890 m from
 // the camera's mirror image below the sea, which would wrongly drop it. The sun disc is 1.6 km away and high.
 const high = { x: 0, y: 400, z: 0, rotationX: -0.05, rotationY: 0, fov: 62, near: 0.5, far: 20000 };
 const meshes = [{ id: "tower", segments: 12, radius: 20, x: 0, y: 150, z: -700 }, { id: "sun", segments: 24, radius: 50, x: 0, y: 900, z: -1500 }];
 assertMirrors(await webgpuSeaDraws(meshes, high, 0), ["tower"], ["sun"]);
 assertMirrors(webglSeaDraws(meshes, high, 0), ["tower"], ["sun"]);
});
test("both renderers judge sky bodies from the sea level, not from y = 0", async () => {
 // The same scene with the sea and everything over it lifted 300 m. Judged from y = 0 the island (550 m up, 2.7 km out)
 // would count as a sky body; over a sea at 300 m it is 250 m up, 5 degrees, and stays, exactly as in the unlifted scene.
 const lift = 300, camera = Object.assign({}, seaCamera, { y: seaCamera.y + lift }), meshes = seaMeshes.map((m) => Object.assign({}, m, { y: m.y + lift }));
 assertMirrors(await webgpuSeaDraws(meshes, camera, lift), ["buoy", "island"], ["moon"]);
 assertMirrors(webglSeaDraws(meshes, camera, lift), ["buoy", "island"], ["moon"]);
});
