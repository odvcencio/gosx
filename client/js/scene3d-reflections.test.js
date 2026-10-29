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

// Which meshes the sea's planar mirror draws. The mirror draws every opaque mesh a second time, upside down under the
// water. A mesh is drawn unless its author opted out with planarReflection: false (scene.Mesh.PlanarReflection). Where
// the mesh stands never decides this: a bridge deck 200 m up and 950 m out belongs to the bridge, and a moon built as
// a mesh leaves the mirror because its author said so. Review 5678771838 on PR 769 reported the geometry test that
// this replaces; its bridge is the first case below.
const ids = (list) => Array.from(list, (o) => o.id);
const solid = { color: "#d8dce4", wireframe: false };
const mirrorCamera = { x: 0, y: 2, z: 0, rotationX: -0.05, rotationY: 0, fov: 62, near: 0.5, far: 20000 };
// A pier from the sea to 200 m, and the deck above it, 200 to 230 m up and 950 m out (the review's probe.cjs numbers).
const bridge = [
 { id: "pier", kind: "cylinder", radiusTop: 10, radiusBottom: 10, height: 200, segments: 12, x: 0, y: 100, z: -950 },
 { id: "bridge-deck", kind: "box", width: 400, height: 30, depth: 100, x: 0, y: 215, z: -950 },
];
// The rest of the scene. Each mesh has its own vertex count, so a recorded draw names the mesh that made it.
const harbour = [
 { id: "buoy", segments: 12, radius: 4, x: -6, y: 4, z: -50 }, // near and ordinary
 { id: "tower", kind: "cone", radiusBottom: 30, radiusTop: 0, height: 300, segments: 10, x: -300, y: 550, z: -1200 }, // ordinary, clear of the sea and 1.3 km out
 { id: "moon", segments: 24, radius: 60, x: 80, y: 380, z: -1330, planarReflection: false }, // a moon built as a mesh: opts out
 { id: "lamp", segments: 16, radius: 3, x: 6, y: 5, z: -60, planarReflection: false }, // near, and opts out all the same
];
const seaScene = bridge.concat(harbour), mirrored = ["pier", "bridge-deck", "buoy", "tower"], opted = ["moon", "lamp"];
function seaState(api, objects) {
 // The objects cross JSON on their way from the server.
 const wire = JSON.parse(JSON.stringify(objects));
 return api.createSceneState({ scene: { objects: wire.map((m) => Object.assign({ kind: "sphere", material: solid, wireframe: false }, m)) } });
}
function seaBundle(api, state, camera, level = 0, capabilities = {}) {
 const objects = api.sceneStateObjectsWithMaterials(state);
 const bundle = api.createSceneRenderBundle(320, 180, "#101820", camera, objects, [], [], [], [], {}, 0.4, [], [], [], [], [], 0, false, capabilities);
 bundle.environment = { sky: { mode: "physical", sunDirection: { x: -0.35, y: 0.7, z: 0.62 }, turbidity: 3 }, ocean: { level, extent: 8000, reflections: { mode: "ssr+planar", resolution: 0.5, strength: 0.8 } } };
 bundle.lights = [];
 const verts = Object.fromEntries(bundle.meshObjects.map((o) => [o.id, o.vertexCount]));
 assert.equal(Object.keys(verts).length, state.objects.size, "every mesh reaches the bundle");
 assert.equal(new Set(Object.values(verts)).size, state.objects.size, "each mesh has its own vertex count");
 return { bundle, verts };
}
const sortedNumbers = (list) => Array.from(list).sort((x, y) => x - y);
// One renderer, any number of frames. Each frame builds a fresh bundle from the state, as the mount loop does, and reports
// the vertex counts drawn into the planar mirror and those drawn everywhere else.
async function webgpuSea() {
 const h = await createBoardWebGPUHarness({ fresh: true });
 const create = h.fake.device.createRenderPipeline.bind(h.fake.device);
 h.fake.device.createRenderPipeline = (d) => Object.assign(create(d), { getBindGroupLayout: () => h.fake.device.createBindGroupLayout({ entries: [] }) });
 h.canvas.width = 320; h.canvas.height = 180;
 const api = h.env.context.__gosx_scene3d_api;
 return {
  api,
  frame(state, camera = mirrorCamera, level = 0) {
   const { bundle, verts } = seaBundle(api, state, camera, level);
   const before = h.fake.state.renderPasses.length;
   h.renderer.render(bundle, { width: 320, height: 180 });
   const passes = h.fake.state.renderPasses.slice(before), planar = passes.filter((p) => p.descriptor?.label === "gosx-planar-reflection");
   assert.equal(planar.length, 1, "the planar mirror is drawn once per frame");
   const drawn = (list) => list.flatMap((p) => p.draws.map((d) => d.vertexCount));
   return { verts, planar: drawn(planar), elsewhere: drawn(passes.filter((p) => !planar.includes(p))) };
  },
  dispose() { h.renderer.dispose(); },
 };
}
function webglSea() {
 const h = createWebGLRendererForPost({ fresh: true }), gl = h.canvas.getContext("webgl2"); glSupport(gl);
 const api = h.env.context.__gosx_scene3d_api;
 return {
  api,
  frame(state, camera = mirrorCamera, level = 0) {
   const { bundle, verts } = seaBundle(api, state, camera, level);
   const first = gl.ops.length;
   h.renderer.render(bundle, { width: 320, height: 180 });
   // The mirror renders into a half-resolution target (160 x 90 here); the main view and the ocean use 320 x 180.
   const planar = [], elsewhere = []; let into = elsewhere;
   for (const op of gl.ops.slice(first)) {
    if (op[0] === "viewport") into = op[3] === 160 ? planar : elsewhere;
    else if (op[0] === "drawArrays") into.push(op[3]);
    else if (op[0] === "drawElements") into.push(op[2]);
   }
   return { verts, planar, elsewhere };
  },
  dispose() { h.renderer.dispose(); assert.deepEqual(h.warnLog, []); },
 };
}
function assertMirrors(got, kept, left) {
 const want = kept.map((id) => got.verts[id]);
 assert.deepEqual(sortedNumbers(got.planar), sortedNumbers(want), `the mirror draws ${kept.join(", ")} and nothing else`);
 for (const id of left) assert.ok(got.elsewhere.includes(got.verts[id]), `the main view still draws ${id}`);
}

test("the mirror list keeps every ordinary mesh wherever it stands and drops only a mesh that opts out", async () => {
 const h = await createBoardWebGPUHarness({ fresh: true }), a = h.env.context.__gosx_scene3d_api;
 // The review's probe: camera (0, 2, 0), sea level 0. The deck's lowest point is 200 m up and 950 m out, which a test on
 // position took for sky; the list is [pier] there and must be [pier, bridge-deck].
 const pier = { id: "pier", bounds: { minX: -10, maxX: 10, minY: 0, maxY: 200, minZ: -960, maxZ: -940 } };
 const deck = { id: "bridge-deck", bounds: { minX: -200, maxX: 200, minY: 200, maxY: 230, minZ: -1000, maxZ: -900 } };
 assert.deepEqual(ids(a.sceneReflectOpaqueList([pier, deck], [], { x: 0, y: 2, z: 0 }, 0)), ["pier", "bridge-deck"], "called as the probe calls it");
 assert.deepEqual(ids(a.sceneReflectOpaqueList([pier, deck], [])), ["pier", "bridge-deck"], "called with the list and the materials only");
 // The same high, far box stays unless its own record says planarReflection: false.
 const box = (x, y, z, r) => ({ minX: x - r, maxX: x + r, minY: y - r, maxY: y + r, minZ: z - r, maxZ: z + r });
 const skyLike = box(80, 380, -1330, 60);
 const records = [pier, deck, { id: "ordinary-moon-sized", bounds: skyLike }, { id: "moon", planarReflection: false, bounds: skyLike },
  { id: "lamp", planarReflection: false, bounds: box(6, 5, -60, 3) }, { id: "no-bounds" }, { id: "explicit-true", planarReflection: true, bounds: skyLike }];
 assert.deepEqual(ids(a.sceneReflectOpaqueList(records, [])), ["pier", "bridge-deck", "ordinary-moon-sized", "no-bounds", "explicit-true"],
  "only the two records that say false leave, in either place; the order of the rest is kept");
 // Only the boolean false opts out. Nothing else a record can hold (absent, null, true) does.
 for (const value of [undefined, null, true]) assert.deepEqual(ids(a.sceneReflectOpaqueList([{ id: "x", planarReflection: value }], [])), ["x"], `planarReflection: ${value} reflects`);
 // The rule that was already there still applies in the same list: a custom shader stays out of the built-in fallback.
 const custom = { id: "custom", materialIndex: 1, bounds: skyLike }, flaggedCustom = { id: "flagged-custom", materialIndex: 1, planarReflection: false };
 const materials = [{ baseColor: "#ffffff" }, { customFragment: "void main() {}" }];
 assert.deepEqual(ids(a.sceneReflectOpaqueList([pier, custom, flaggedCustom, { id: "moon", planarReflection: false }], materials)), ["pier"]);
 h.renderer.dispose();
});

test("the bridge built from scene state reaches the mirror list whole, pier and deck", async () => {
 const h = await createBoardWebGPUHarness({ fresh: true }), a = h.env.context.__gosx_scene3d_api;
 const { bundle } = seaBundle(a, seaState(a, bridge), mirrorCamera, 0);
 const near = (got, want) => Object.keys(want).every((k) => Math.abs(got[k] - want[k]) < 1e-3);
 const record = (id) => bundle.meshObjects.find((o) => o.id === id);
 assert.ok(near(record("bridge-deck").bounds, { minX: -200, maxX: 200, minY: 200, maxY: 230, minZ: -1000, maxZ: -900 }), "the deck has the review's bounds");
 assert.ok(near(record("pier").bounds, { minX: -10, maxX: 10, minY: 0, maxY: 200, minZ: -960, maxZ: -940 }), "so does the pier");
 assert.deepEqual(ids(a.sceneReflectOpaqueList(bundle.meshObjects, bundle.materials, mirrorCamera, 0)), ["pier", "bridge-deck"]);
 h.renderer.dispose();
});

// Participation is part of the object, not of a frame: it starts as true, it is set by the author, a live patch changes it and
// a reset restores it, and a mount that builds the object again gets it back. The patch below is what scene.DiffScene with
// PatchObjectState sends (scene/planar_reflection_test.go pins the same text), and the create payload is the whole record.
const statePatch = (participates) => JSON.parse('{"x":80,"y":380,"z":-1330,"rotationX":0,"rotationY":0,"rotationZ":0,"scaleX":1,"scaleY":1,"scaleZ":1,"parentMatrix":null,' +
 '"interactive":false,"label":"","interactiveOrder":0,"visible":true,"selected":false,"planarReflection":' + participates + '}');
test("an object's participation starts true, is set by the author, and survives a patch, a reset, a create and a remount", async () => {
 const h = await createBoardWebGPUHarness({ fresh: true }), a = h.env.context.__gosx_scene3d_api;
 const state = seaState(a, [{ id: "plain" }, { id: "moon", planarReflection: false, radius: 60, x: 80, y: 380, z: -1330 }, { id: "said-true", planarReflection: true }]);
 const flag = (id, s = state) => s.objects.get(id).planarReflection;
 assert.deepEqual([flag("plain"), flag("moon"), flag("said-true")], [true, false, true], "absent is true; the author's value is kept");
 // A transform-only patch has no such key and leaves the value alone, both ways.
 a.applySceneCommands(state, [{ kind: 2, objectId: "moon", data: { x: 90 } }, { kind: 2, objectId: "plain", data: { x: 3 } }]);
 assert.deepEqual([flag("plain"), flag("moon")], [true, false]);
 const radius = state.objects.get("moon").radius, vertices = state.objects.get("moon").vertices;
 // The state patch that DiffScene sends (all the state keys, geometry untouched) turns participation off, then the reset turns it on.
 a.applySceneCommands(state, [{ kind: 2, objectId: "plain", data: statePatch(false) }]);
 assert.equal(flag("plain"), false, "a live patch opts a mesh out");
 const plain = state.objects.get("plain");
 assert.deepEqual([plain.visible, plain.selected, plain.x, plain.y], [true, false, 80, 380], "and carries the rest of the state with it");
 a.applySceneCommands(state, [{ kind: 2, objectId: "plain", data: statePatch(true) }]);
 assert.equal(flag("plain"), true, "an explicit true restores participation");
 a.applySceneCommands(state, [{ kind: 2, objectId: "moon", data: statePatch(true) }]);
 assert.equal(flag("moon"), true);
 assert.equal(state.objects.get("moon").radius, radius, "geometry is untouched by the patch");
 assert.equal(state.objects.get("moon").vertices, vertices, "and so are the vertex buffers, which the patch must not rebuild");
 // The replace path: remove then create, with the whole record as the create payload, as DiffScene sends it.
 a.applySceneCommands(state, [{ kind: 1, objectId: "moon" }, { kind: 0, objectId: "moon", data: { geometry: "sphere", props: JSON.parse(JSON.stringify({ id: "moon", kind: "sphere", radius: 60, planarReflection: false })) } }]);
 assert.equal(flag("moon"), false, "a created object keeps the key of its record");
 a.applySceneCommands(state, [{ kind: 1, objectId: "moon" }, { kind: 0, objectId: "moon", data: { geometry: "sphere", props: { id: "moon", kind: "sphere", radius: 60 } } }]);
 assert.equal(flag("moon"), true, "a record without the key creates an object that participates");
 // A mount that builds the same scene again from the same JSON gets the same values.
 const again = seaState(a, [{ id: "plain" }, { id: "moon", planarReflection: false }]);
 assert.deepEqual([flag("plain", again), flag("moon", again)], [true, false]);
 h.renderer.dispose();
});

test("every mesh record the bundle emits carries the object's participation", async () => {
 const h = await createBoardWebGPUHarness({ fresh: true }), a = h.env.context.__gosx_scene3d_api;
 // The retained-geometry capability selects the other record builder for the same objects.
 for (const capabilities of [{}, { retainedGeometry: true }]) {
  const { bundle } = seaBundle(a, seaState(a, seaScene), mirrorCamera, 0, capabilities);
  const flags = Object.fromEntries(bundle.meshObjects.map((o) => [o.id, o.planarReflection]));
  const kind = capabilities.retainedGeometry ? "retained" : "baked";
  assert.equal(bundle.meshObjects.every((o) => Boolean(o.retainedGeometry) === Boolean(capabilities.retainedGeometry)), true, `the ${kind} builder made every record`);
  assert.deepEqual(flags, { pier: true, "bridge-deck": true, buoy: true, tower: true, moon: false, lamp: false }, `${kind} records carry true or false for every mesh`);
  assert.deepEqual(ids(a.sceneReflectOpaqueList(bundle.meshObjects, bundle.materials)), mirrored, `${kind} records: the list is the ordinary meshes`);
 }
 h.renderer.dispose();
});

test("WebGPU mirrors the bridge, the tower and the buoy and leaves out the moon and the lamp that opt out, which the main view still draws", async () => {
 const sea = await webgpuSea(), state = seaState(sea.api, seaScene);
 assertMirrors(sea.frame(state), mirrored, opted);
 sea.dispose();
});
test("WebGL mirrors the bridge, the tower and the buoy and leaves out the moon and the lamp that opt out, which the main view still draws", () => {
 const sea = webglSea(), state = seaState(sea.api, seaScene);
 assertMirrors(sea.frame(state), mirrored, opted);
 sea.dispose();
});
test("both renderers mirror the same meshes whatever the camera height or the sea level, because position decides nothing", async () => {
 // The viewer on a mast 400 m up, and the sea raised to 300 m: the mirror is the same list in every case. (From the mast the
 // buoy and the lamp are outside the view, so only the mirror is compared.)
 for (const [camera, level] of [[mirrorCamera, 0], [Object.assign({}, mirrorCamera, { y: 402 }), 0], [Object.assign({}, mirrorCamera, { y: 302 }), 300]]) {
  const gpu = await webgpuSea(), gl = webglSea();
  assertMirrors(gpu.frame(seaState(gpu.api, seaScene), camera, level), mirrored, []);
  assertMirrors(gl.frame(seaState(gl.api, seaScene), camera, level), mirrored, []);
  gpu.dispose(); gl.dispose();
 }
});

// A live patch reaches the next frame on a renderer that stays mounted: the scene does not change shape, so nothing but the
// patch can tell the planner and the draw lists that the mirror changed.
async function assertLiveParticipation(sea) {
 const state = seaState(sea.api, seaScene);
 assertMirrors(sea.frame(state), mirrored, opted);
 sea.api.applySceneCommands(state, [{ kind: 2, objectId: "moon", data: statePatch(true) }, { kind: 2, objectId: "buoy", data: Object.assign(statePatch(false), { x: -6, y: 4, z: -50 }) }]);
 assertMirrors(sea.frame(state), ["pier", "bridge-deck", "tower", "moon"], ["lamp", "buoy"]);
 sea.api.applySceneCommands(state, [{ kind: 2, objectId: "moon", data: statePatch(false) }, { kind: 2, objectId: "buoy", data: Object.assign(statePatch(true), { x: -6, y: 4, z: -50 }) }]);
 assertMirrors(sea.frame(state), mirrored, opted);
 sea.dispose();
}
test("WebGPU follows a live participation change on the next frame, both ways", async () => { await assertLiveParticipation(await webgpuSea()); });
test("WebGL follows a live participation change on the next frame, both ways", () => assertLiveParticipation(webglSea()));
