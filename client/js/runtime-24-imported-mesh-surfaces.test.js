"use strict";

const test = require("node:test");
const assert = require("node:assert/strict");
const vm = require("node:vm");
const {
  bootstrapRuntimeSource, createContext, freshFeatureBundleSource,
  createWebGLRendererForPost, runScript, flushAsyncWork, buildMinimalGLBBytes,
} = require("./runtime-test-harness.js");

function importedMeshRuntime(options = {}) {
  const env = createContext(options);
  runScript(bootstrapRuntimeSource, env.context, "bootstrap-runtime.js");
  const source = freshFeatureBundleSource("scene3d").replace(
    "window.__gosx_scene3d_available = true;",
    "window.__meshTest = { countVertexCopies: function() { const original=sceneNormalizeMeshVertexData; let count=0; sceneNormalizeMeshVertexData=function(value) { if(value && value.positions && value.positions.length) count++; return original(value); }; return function(){return count;}; }, countVertexTransforms: function() { const original=sceneModelTransformMeshFloats; let count=0; sceneModelTransformMeshFloats=function(...args) { count++; return original(...args); }; return function(){return count;}; }, countStages: function() { const original=sceneStageModelHydration; let count=0; sceneStageModelHydration=async function(...args) { count++; return original(...args); }; return function(){return count;}; }, hydrate: hydrateSceneStateModels, normalize: normalizeSceneModel, instantiate: sceneInstantiateModelObject, transform: sceneApplyStaticModelObjectTransform, failLoad: function(src) { const original=loadSceneModelAsset; loadSceneModelAsset=async function(path,...args) { if(path===src) throw new Error('injected stage failure'); return original(path,...args); }; }, countMatrices: function() { const original = sceneObjectModelMatrix; let count = 0; sceneObjectModelMatrix = function(object, time) { count++; return original(object, time); }; return function() { return count; }; } }; window.__gosx_scene3d_available = true;",
  );
  runScript(source, env.context, "bootstrap-feature-scene3d.js");
  return env;
}

for (const fresh of [true, false]) {
  test(`WebGL ${fresh ? "source" : "generated"} instanced meshes obey blend and depth passes`, () => {
    const h = createWebGLRendererForPost({ fresh });
    const api = h.env.context.__gosx_scene3d_api;
    const bundle = api.createSceneRenderBundle(320, 180, "#000000",
      { x: 0, y: 0, z: 6, fov: 72, near: .05, far: 128 },
      [], [], [], [], [], {}, 0, [], [], [], [], [], 0, false);
    const identity = [1,0,0,0, 0,1,0,0, 0,0,1,0, 0,0,0,1];
    bundle.materials = ["opaque", "alpha", "additive"].map((pass, index) => ({
      key: pass, kind: "standard", color: "#ffffff", renderPass: pass, opacity: index ? .2 : 1,
    }));
    // Reverse declaration order: batching must still draw opaque first and
    // never write transparent geometry into the depth buffer.
    bundle.instancedMeshes = [2, 1, 0].map(index => ({ id: "batch"+index, kind: "box", materialIndex: index,
      count: index+2, transforms: Array.from({ length: index+2 }, () => identity).flat() }));
    const gl = h.canvas.getContext("webgl2");
    const start = gl.ops.length;
    h.renderer.render(bundle, { cssWidth: 320, cssHeight: 180, width: 320, height: 180 });
    let blend = false, depth = true, destination;
    const draws = [];
    for (const op of gl.ops.slice(start)) {
      if ((op[0] === "enable" || op[0] === "disable") && op[1] === gl.BLEND) blend = op[0] === "enable";
      if (op[0] === "depthMask") depth = op[1];
      if (op[0] === "blendFunc") destination = op[2];
      if (op[0] === "drawArraysInstanced") draws.push({ count: op[4], blend, depth, destination });
    }
    assert.deepEqual(draws.map(d => d.count), [2, 3, 4]);
    assert.equal(draws[0].blend, false);
    assert.equal(draws[0].depth, true);
    assert.equal(draws[1].blend, true);
    assert.equal(draws[1].depth, false);
    assert.equal(draws[1].destination, gl.ONE_MINUS_SRC_ALPHA);
    assert.equal(draws[2].blend, true);
    assert.equal(draws[2].depth, false);
    assert.equal(draws[2].destination, gl.ONE);
    h.renderer.dispose();
  });
}

test("ordinary rigid Model updates retain GPU geometry and still invalidate changed static models", async () => {
  const env = importedMeshRuntime({ fetchRoutes: { "/actor.glb": { bytes: buildMinimalGLBBytes() } } });
  runScript(freshFeatureBundleSource("scene3d-gltf"), env.context, "bootstrap-feature-scene3d-gltf.js");
  const api = env.context.__gosx_scene3d_api;
  const models = [{ id: "deck", src: "/actor.glb", static: true }, { id: "robot", src: "/actor.glb", x: 1 }];
  const state = api.createSceneState({ scene: { models } });
  await env.context.__meshTest.hydrate(state, null);
  const robot = Array.from(state.objects.values()).find(o => o.id.startsWith("robot/"));
  const generation = state._modelHydrationGeneration;
  await api.applySceneCommands(state, [{ kind: 10, data: { models: [models[0], { ...models[1], x: 7, rotationY: .5, scaleX: 2 }] } }]);
  assert.equal(state._modelHydrationGeneration, generation);
  assert.equal(state.objects.get(robot.id), robot);
  assert.equal(robot.parentMatrix[12], 7);
  assert.ok(Math.abs(robot.parentMatrix[0] - Math.cos(.5)*2) < 1e-6);
  await api.applySceneCommands(state, [{ kind: 10, data: { models: [{ ...models[0], x: 4 }, models[1]] } }]);
  assert.ok(state._modelHydrationGeneration > generation, "a changed static model must not be skipped by the pose shortcut");
});

test("rigid instances of the same GLB share immutable geometry and keep independent poses", async () => {
  const env = importedMeshRuntime({ fetchRoutes: { "/swarm.glb": { bytes: buildMinimalGLBBytes() } } });
  runScript(freshFeatureBundleSource("scene3d-gltf"), env.context, "bootstrap-feature-scene3d-gltf.js");
  const api = env.context.__gosx_scene3d_api;
  const models = [{ id: "a", src: "/swarm.glb", x: -2 }, { id: "b", src: "/swarm.glb", x: 3 }];
  const state = api.createSceneState({ scene: { models } });
  await env.context.__meshTest.hydrate(state, null);
  const objects = Array.from(state.objects.values());
  assert.equal(objects.length, 2);
  assert.equal(objects[0].vertices, objects[1].vertices, "identical imported primitives must share one geometry identity");
  assert.notEqual(objects[0].parentMatrix, objects[1].parentMatrix);
  const vertices = objects[0].vertices;
  const positions = Array.from(vertices.positions);
  await api.applySceneCommands(state, [{ kind: 10, data: { models: [models[0], { ...models[1], x: 8, scaleY: 2 }] } }]);
  assert.equal(objects[0].parentMatrix[12], -2);
  assert.equal(objects[1].parentMatrix[12], 8);
  assert.deepEqual(Array.from(vertices.positions), positions, "pose changes must never rebake the shared stream");
});

function rigidBatchFixture(fresh, count = 100) {
  const h = createWebGLRendererForPost({ fresh });
  const api = h.env.context.__gosx_scene3d_api;
  const vertices = vm.runInContext(`({count:3, immutable:true, revision:0,
    positions:new Float32Array([-.1,0,0, .1,0,0, 0,.2,0]),
    normals:new Float32Array([0,0,1, 0,0,1, 0,0,1]),
    uvs:new Float32Array([0,0,1,0,.5,1]),
    indices:new Uint32Array([0,1,2])})`, h.env.context);
  const F32 = vm.runInContext("Float32Array", h.env.context);
  const bundle = api.createSceneRenderBundle(320, 180, "#000000",
    { x: 0, y: 0, z: 6, fov: 72, near: .05, far: 128 },
    [], [], [], [], [], {}, 0, [], [], [], [], [], 0, false);
  bundle.materials = [{ key:"swarm", kind:"standard", color:"#669966", renderPass:"opaque", opacity:1 }];
  bundle.meshObjects = Array.from({length:count}, (_, i) => ({
    id:"bug-"+i, vertices, directVertices:true, retainedGeometry:true, geometryRevision:0,
    vertexOffset:0, vertexCount:3, materialIndex:0, depthNear:4, depthFar:6,
    castShadow:true, receiveShadow:true,
    modelMatrix:new F32([1,0,0,0, 0,1,0,0, 0,0,1,0, (i%10)*.15-1,Math.floor(i/10)*.1,0,1]),
  }));
  const gl = h.canvas.getContext("webgl2");
  const render = () => h.renderer.render(bundle, { cssWidth:320, cssHeight:180, width:320, height:180 });
  return { ...h, bundle, gl, render };
}

for (const fresh of [true, false]) {
  test(`WebGL ${fresh ? "source" : "generated"} batches shared indexed geometry and retires its streams`, () => {
    const h = rigidBatchFixture(fresh);
    h.render();
    let draws = h.gl.ops.filter(op => op[0] === "drawElementsInstanced");
    assert.equal(draws.length, 1);
    assert.equal(draws[0][5], 100, "one indexed GPU call must render the whole compatible group");
    const warm = h.gl.ops.length;
    for (let frame = 0; frame < 180; frame++) {
      h.bundle.meshObjects.forEach((obj, i) => obj.modelMatrix[12] = .01*Math.sin(frame+i));
      h.render();
    }
    assert.equal(h.gl.ops.slice(warm).filter(op => op[0] === "createBuffer").length, 0);
    assert.equal(h.gl.ops.slice(warm).filter(op => op[0] === "drawElementsInstanced").length, 180);
    const removed = h.gl.ops.length;
    h.bundle.meshObjects = [];
    h.bundle.points = [{ id:"keep",count:1,positions:new Float32Array([0,0,0]),color:"#fff" }];
    h.render();
    assert.ok(h.gl.ops.slice(removed).filter(op => op[0] === "deleteBuffer").length >= 4,
      "geometry and transform buffers must retire after the group disappears");
    h.renderer.dispose();
  });
}

test("pooled swarm geometry survives an empty wave and expires within its residency window", () => {
  const h=rigidBatchFixture(true,10), actors=h.bundle.meshObjects;
  actors[0].vertices._rigidPool=true;
  h.render();
  h.bundle.meshObjects=[];
  h.bundle.points=[{id:"keep",count:1,positions:new Float32Array([0,0,0]),color:"#fff"}];
  for(let i=0;i<20;i++) h.render();
  const start=h.gl.ops.length;
  h.bundle.meshObjects=actors; h.render();
  const uploads=h.gl.ops.slice(start).filter(op=>op[0]==="bufferData" && op[3]===h.gl.STATIC_DRAW);
  assert.equal(uploads.length,0,"respawning a warm type must not upload static vertices again");
  h.bundle.meshObjects=[];
  const removed=h.gl.ops.length;
  for(let i=0;i<122;i++) h.render();
  assert.ok(h.gl.ops.slice(removed).filter(op=>op[0]==="deleteBuffer").length>=4,"idle pool must release its GPU handles");
  h.renderer.dispose();
});

test("rigid batches separate materials, preserve alpha draws and omit view-culled actors", () => {
  const h = rigidBatchFixture(true, 10);
  h.bundle.materials.push({key:"other",kind:"standard",color:"#336699",renderPass:"opaque",opacity:1});
  h.bundle.materials.push({key:"glass",kind:"standard",color:"#ffffff",renderPass:"alpha",opacity:.2});
  h.bundle.meshObjects.slice(4,7).forEach(obj=>obj.materialIndex=1);
  h.bundle.meshObjects[7].materialIndex = 2;
  h.bundle.meshObjects[8].viewCulled = true;
  h.bundle.meshObjects[9].viewCulled = true;
  h.render();
  assert.deepEqual(h.gl.ops.filter(op=>op[0]==="drawElementsInstanced").map(op=>op[5]).sort(), [3,4]);
  assert.equal(h.gl.ops.filter(op=>op[0]==="drawElements").length, 1, "alpha object keeps the sorted regular draw path");
  h.renderer.dispose();
});

test("shadow depth pass instances the same shared geometry", () => {
  const h = rigidBatchFixture(true, 40);
  h.bundle.lights = [{ kind:"directional",color:"#ffffff",intensity:1,x:0,y:5,z:3,
    directionX:-.4,directionY:-1,directionZ:-.3,castShadow:true,shadowSize:256 }];
  h.render();
  const draws = h.gl.ops.filter(op=>op[0]==="drawElementsInstanced");
  assert.equal(draws.length, 2, "one depth and one color draw for forty casters");
  assert.ok(draws.every(op=>op[5]===40));
  assert.equal(h.gl.ops.filter(op=>op[0]==="drawElements").length, 0);
  h.renderer.dispose();
});

test("side-frustum culling skips off-camera color draws while preserving their shadows", () => {
  const h = rigidBatchFixture(true, 10);
  h.bundle.lights = [{kind:"directional",color:"#ffffff",intensity:1,x:0,y:5,z:3,
    directionX:-.4,directionY:-1,directionZ:-.3,castShadow:true,shadowSize:256}];
  h.bundle.meshObjects.forEach((obj,i) => {
    const x=i<6?0:80;
    obj.modelMatrix[12]=x;
    obj.bounds={minX:x-.2,maxX:x+.2,minY:0,maxY:.2,minZ:-.1,maxZ:.1};
  });
  h.render();
  const counts=h.gl.ops.filter(op=>op[0]==="drawElementsInstanced").map(op=>op[5]).sort();
  assert.deepEqual(counts,[4,6,6],"six visible actors receive color+shadow; four off-camera actors receive shadow only");
  h.renderer.dispose();
});

test("hundreds of fresh instance arrays reuse stream buffers and retire removed batches", () => {
  const h = createWebGLRendererForPost({ fresh: true });
  const api = h.env.context.__gosx_scene3d_api;
  const bundle = api.createSceneRenderBundle(320, 180, "#000000",
    { x: 0, y: 0, z: 6, fov: 72, near: .05, far: 128 },
    [], [], [], [], [], {}, 0, [], [], [], [], [], 0, false);
  bundle.materials = [{ key: "effect", kind: "standard", color: "#ffffff", renderPass: "alpha", opacity: .2 }];
  const gl = h.canvas.getContext("webgl2");
  const render = () => h.renderer.render(bundle, { cssWidth: 320, cssHeight: 180, width: 320, height: 180 });
  function update(frame) {
    bundle.instancedMeshes = [{ id: "live-fx", kind: "box", materialIndex: 0, count: 1,
      transforms: [1,0,0,0, 0,1,0,0, 0,0,1,0, frame/100,0,0,1], colors: [frame % 2 ? "#44ccff" : "#ffcc44"] }];
    render();
  }
  update(0);
  const start = gl.ops.length;
  for (let frame = 1; frame <= 240; frame++) update(frame);
  assert.equal(gl.ops.slice(start).filter(op => op[0] === "createBuffer").length, 0,
    "fresh command arrays must not allocate a new GPU buffer each frame");
  assert.ok(gl.ops.slice(start).filter(op => op[0] === "bufferSubData").length >= 480,
    "reusing buffers must still upload changed transforms and colors");
  const removed = gl.ops.length;
  bundle.instancedMeshes = [];
  // Keep an ordinary drawable so this exercises frame retirement, not dispose.
  bundle.points = [{ id: "keep-rendering", count: 1, positions: new Float32Array([0,0,0]), color: "#fff" }];
  render();
  assert.equal(gl.ops.slice(removed).filter(op => op[0] === "deleteBuffer").length, 2);
  h.renderer.dispose();
});

test("rigid imported mesh snapshots retain attributes and invalidate after a live transform", () => {
  const env = importedMeshRuntime();
  const raw = vm.runInContext(`({
    id: "triangle", kind: "gltf-mesh", wireframe: false,
    material: { kind: "standard", color: "#5b7580" },
    vertices: { count: 3, positions: new Float32Array([0, 0, 0, 1, 0, 0, 0, 1, 0]),
      normals: new Float32Array([0, 0, 1, 0, 0, 1, 0, 0, 1]),
      uvs: new Float32Array([0, 0, 1, 0, 0, 1]),
      tangents: new Float32Array([1, 0, 0, 1, 1, 0, 0, 1, 1, 0, 0, 1]) },
  })`, env.context);
  const model = env.context.__meshTest.normalize(vm.runInContext('({ id: "actor", x: 2, y: 0, z: 0, scaleX: 1, scaleY: 1, scaleZ: 1, static: true })', env.context), 0);
  const matrixCalls = env.context.__meshTest.countMatrices();
  const object = env.context.__meshTest.instantiate(raw, model, "actor", 0, []);
  const smallMatrixCalls = matrixCalls();
  assert.equal(object.vertices.immutable, true);
  assert.equal(object.vertices.revision, 0);
  assert.equal(object.vertices.positions[0], 2);
  const api = env.context.__gosx_scene3d_api;
  const bundle = api.createSceneRenderBundle(320, 180, "#000000",
    { x: 0, y: 0, z: 6, fov: 72, near: .05, far: 128 },
    [object], [], [], [], [], {}, 0, [], [], [], [], [], 0, false, { retainedGeometry: true });
  assert.equal(bundle.retainedMeshObjectCount, 1);
  assert.equal(bundle.worldMeshPositions.length, 0);
  model.x = 5;
  const record = { staticModel: true, model, objectIDs: [object.id] };
  const state = { objects: new Map([[object.id, object]]) };
  assert.equal(env.context.__meshTest.transform(state, record), true);
  assert.equal(object.vertices.revision, 1);
  assert.equal(object.vertices.positions[0], 5);
  assert.equal(raw.vertices.positions[0], 0, "live transforms cannot mutate the shared asset");
  const F32 = vm.runInContext("Float32Array", env.context);
  raw.vertices.count = 3000;
  for (const [key, width] of [["positions", 3], ["normals", 3], ["uvs", 2], ["tangents", 4]]) {
    raw.vertices[key] = new F32(raw.vertices.count * width);
  }
  const beforeLarge = matrixCalls();
  env.context.__meshTest.instantiate(raw, model, "large-actor", 0, []);
  assert.ok(matrixCalls() - beforeLarge <= smallMatrixCalls + 2,
    "increasing the vertex count 1000x must not repeat TRS/inverse matrix construction per vertex");
});

for (const fresh of [true, false]) {
  test(`WebGL ${fresh ? "source" : "generated"} draws HTML texture surfaces without world lines`, async () => {
    const harness = createWebGLRendererForPost({ fresh });
    const api = harness.env.context.__gosx_scene3d_api;
    const html = api.normalizeSceneHTML({ id: "panel", mode: "texture", html: "<b>STATUS</b>",
      surfaceWidth: 2, surfaceHeight: 1, rotationX: -Math.PI / 2,
      textureKey: "data:image/svg+xml,<svg/>", textureWidth: 256, textureHeight: 128,
      textureReady: true }, 0, null);
    const bundle = api.createSceneRenderBundle(320, 180, "#000000",
      { x: 0, y: 0, z: 6, fov: 72, near: .05, far: 128 },
      [], [], [], [html], [], {}, 0, [], [], [], [], [], 0, false);
    assert.equal(bundle.worldVertexCount, 0);
    assert.equal(bundle.surfaces.length, 1);
    const gl = harness.canvas.getContext("webgl2");
    const start = gl.ops.length;
    harness.renderer.render(bundle, { cssWidth: 320, cssHeight: 180, width: 320, height: 180 });
    assert.equal(gl.ops.slice(start).filter(op => op[0] === "drawArrays" && op[1] === gl.TRIANGLES).length, 0,
      "pending HTML textures must not draw a white placeholder");
    await flushAsyncWork();
    harness.renderer.render(bundle, { cssWidth: 320, cssHeight: 180, width: 320, height: 180 });
    const draws = gl.ops.slice(start).filter(op => op[0] === "drawArrays" && op[1] === gl.TRIANGLES && op[3] === 6);
    assert.equal(draws.length, 1, "the panel must issue its own textured triangle draw");
    harness.renderer.dispose();
  });
}


test("instance-only hydration retains the static deck and bounds its cache to the current generation", async () => {
  const env = importedMeshRuntime({ fetchRoutes: {
    "/models/deck.glb": { bytes: buildMinimalGLBBytes() },
    "/models/hero.glb": { bytes: buildMinimalGLBBytes() },
  } });
  runScript(freshFeatureBundleSource("scene3d-gltf"), env.context, "bootstrap-feature-scene3d-gltf.js");
  const api = env.context.__gosx_scene3d_api;
  const state = api.createSceneState({ scene: {
    models: [{ id: "deck", src: "/models/deck.glb", static: true }],
    instancedGLBMeshes: [{ id: "heroes", src: "/models/hero.glb", instances: [{ id: "one", x: 0 }] }],
  } });
  const hydrate = () => env.context.__meshTest.hydrate(state, null);
  assert.equal((await hydrate()).committed, true);
  const find = prefix => Array.from(state.objects.values()).find(object => object.id.startsWith(prefix));
  const deck = find("deck/");
  const initialHero = find("heroes/one/");
  assert.ok(deck && initialHero, "real GLB stages must emit both the static deck and the moving instance");
  assert.equal(state._hydratedModelRecords.staticModels.size, 1);
  for (let x = 1; x <= 8; x++) {
    state.instancedGLBMeshes[0].instances[0].x = x;
    assert.equal((await hydrate()).committed, true);
    assert.equal(find("deck/"), deck);
    assert.equal(find("deck/").vertices.positions, deck.vertices.positions);
    assert.equal(find("heroes/one/").vertices.positions, initialHero.vertices.positions);
    assert.equal(find("heroes/one/").parentMatrix[12], x);
    assert.equal(state._hydratedModelRecords.staticModels.size, 1);
  }
  state.models[0].x = 4;
  await hydrate();
  const movedDeck = find("deck/");
  assert.notEqual(movedDeck.vertices.positions, deck.vertices.positions);
  assert.equal(movedDeck.vertices.positions[0], deck.vertices.positions[0] + 4);
  state.models[0].materialOverride = { color: "#112233" };
  await hydrate();
  const recoloredDeck = find("deck/");
  assert.notEqual(recoloredDeck, movedDeck);
  assert.equal(recoloredDeck.color, "#112233");
  state._modelTextureVariantScope = { key: "different-texture-backend" };
  await hydrate();
  assert.notEqual(find("deck/"), recoloredDeck, "texture backend changes must invalidate staged mesh reuse");
  const committedDeck = find("deck/");
  const committedCache = state._hydratedModelRecords.staticModels;
  state._modelOwner = () => false;
  assert.equal((await hydrate()).stale, true);
  assert.equal(find("deck/"), committedDeck);
  assert.equal(state._hydratedModelRecords.staticModels, committedCache,
    "a stale generation must preserve the committed objects and cache");
  state._modelOwner = () => true;
  state.models[0].animation = "idle";
  await hydrate();
  assert.equal(state._hydratedModelRecords.staticModels.size, 0,
    "animation declarations cannot reuse static stages even when their clip is absent");
  state.models[0].animation = "";
  state.models[0]._live = [{ event: "deck-pose" }];
  await hydrate();
  assert.equal(state._hydratedModelRecords.staticModels.size, 0,
    "live-bound declarations cannot reuse static stages");
  state.models = [];
  await hydrate();
  assert.equal(state._hydratedModelRecords.staticModels.size, 0);
  assert.equal(find("deck/"), undefined);
});

test("spawning and clearing waves preserve survivor wrappers and isolate failed stages", async () => {
  const env=importedMeshRuntime({fetchRoutes:{"/actor.glb":{bytes:buildMinimalGLBBytes()},"/missing.glb":{status:500,body:"unavailable"}}});
  runScript(freshFeatureBundleSource("scene3d-gltf"),env.context,"bootstrap-feature-scene3d-gltf.js");
  const api=env.context.__gosx_scene3d_api;
  const state=api.createSceneState({scene:{models:[{id:"survivor",src:"/actor.glb",x:1}]}});
  const hydrate=()=>env.context.__meshTest.hydrate(state,null);
  const models=values=>{state.models=values.map(env.context.__meshTest.normalize);};
  await hydrate();
  const survivor=Array.from(state.objects.values())[0];
  const vertexTransforms=env.context.__meshTest.countVertexTransforms();
  const vertexCopies=env.context.__meshTest.countVertexCopies();
  for(let wave=0;wave<12;wave++) {
    models([{id:"survivor",src:"/actor.glb",x:wave},...Array.from({length:20},(_,i)=>({id:`wave${wave}-${i}`,src:"/actor.glb",x:i}))]);
    await hydrate();
    assert.equal(state.objects.get(survivor.id),survivor,"spawn must not invalidate every survivor's caches");
    assert.equal(survivor.parentMatrix[12],wave);
    state.models=state.models.slice(0,1); await hydrate();
    assert.equal(state.objects.size,1);
    assert.equal(state.objects.get(survivor.id),survivor);
  }
  assert.equal(vertexTransforms(),0,"new identities must reuse decoded geometry before doing any vertex transforms");
  assert.equal(vertexCopies(),0,"warmed spawn must not copy immutable vertex arrays during normalization");
  const pose=survivor.parentMatrix;
  env.context.__meshTest.failLoad("/missing.glb");
  models([{id:"survivor",src:"/actor.glb",x:99},{id:"bad",src:"/missing.glb"}]);
  assert.equal((await hydrate()).committed,false);
  assert.equal(survivor.parentMatrix,pose,"failed spawn must not move the live survivor");
  models([{id:"survivor",src:"/actor.glb",x:99}]); state._modelOwner=()=>false;
  assert.equal((await hydrate()).stale,true);
  assert.equal(survivor.parentMatrix,pose,"superseded spawn must not mutate the live generation");
});

test("rigid actor pose and membership commands preserve survivors without full hydration", async () => {
  const env = importedMeshRuntime({ fetchRoutes: { "/actor.glb": { bytes: buildMinimalGLBBytes() } } });
  runScript(freshFeatureBundleSource("scene3d-gltf"), env.context, "bootstrap-feature-scene3d-gltf.js");
  const api = env.context.__gosx_scene3d_api;
  const state = api.createSceneState({ scene: { instancedGLBMeshes: [
    { id: "swarm", src: "/actor.glb", instances: [{ id: "one", x: 2 }, { id: "two", x: -2 }] },
  ] } });
  await env.context.__meshTest.hydrate(state, null);
  const generation = state._modelHydrationGeneration;
  const first = Array.from(state.objects.values()).find(o => o.id.startsWith("swarm/one/"));
  const vertices = first.vertices.positions;
  const matrices = env.context.__meshTest.countMatrices();
  for (let frame = 0; frame < 60; frame++) {
    await api.applySceneCommands(state, [{ kind: 11, data: { instancedGLBMeshes: [
      { id: "swarm", src: "/actor.glb", instances: [{ id: "one", x: frame / 10, rotationY: .4 }, { id: "two", x: -2 }] },
    ] } }]);
    assert.equal(state._modelHydrationGeneration, generation);
    assert.equal(state.objects.get(first.id), first);
    assert.equal(first.vertices.positions, vertices);
  }
  assert.equal(matrices(),120,"two actors build one matrix each per submitted pose");
  assert.ok(Math.abs(first.parentMatrix[12] - 5.9) < .00001);
  const second = Array.from(state.objects.values()).find(o => o.id.startsWith("swarm/two/"));
  await api.applySceneCommands(state, [{ kind: 11, data: { instancedGLBMeshes: [
    { id: "swarm", src: "/actor.glb", instances: [{ id: "one", x: 7 }, { id: "three", x: 3 }] },
  ] } }]);
  const third = Array.from(state.objects.values()).find(o => o.id.startsWith("swarm/three/"));
  assert.equal(state._modelHydrationGeneration, generation, "rigid membership uses its incremental transaction");
  assert.equal(state.objects.get(first.id), first, "survivor wrapper identity must remain stable");
  assert.equal(first.parentMatrix[12], 7);
  assert.equal(state.objects.has(second.id), false, "removed membership must leave the committed object map");
  assert.ok(third);
  assert.equal(third.vertices, first.vertices, "new membership reuses the warmed immutable geometry");
  assert.equal(state._hydratedModelRecords.rigidInstances.size, 2);
  const before = first.parentMatrix;
  await api.applySceneCommands(state, [{ kind: 11, data: { instancedGLBMeshes: [
    { id: "swarm", src: "/actor.glb", color: "#ff0000", instances: [{ id: "one", x: 8 }] },
  ] } }]);
  assert.ok(state._modelHydrationGeneration > generation, "material changes still use full atomic hydration");
  assert.equal(state._hydratedModelRecords.rigidInstances.size, 1);
  assert.equal(first.parentMatrix, before, "replacing a collection must not mutate an old committed wrapper");
});

test("rigid membership failures and stale additions preserve the complete committed generation", async () => {
  const env = importedMeshRuntime({ fetchRoutes: {
    "/actor.glb": { bytes: buildMinimalGLBBytes() },
    "/missing.glb": { status: 500, body: "unavailable" },
  } });
  runScript(freshFeatureBundleSource("scene3d-gltf"), env.context, "bootstrap-feature-scene3d-gltf.js");
  const api = env.context.__gosx_scene3d_api;
  const batch = instances => ({ id: "swarm", src: "/actor.glb", instances });
  const state = api.createSceneState({ scene: { instancedGLBMeshes: [batch([{ id: "one", x: 1 }])] } });
  await env.context.__meshTest.hydrate(state, null);
  env.context.__meshTest.failLoad("/missing.glb");
  const one = Array.from(state.objects.values())[0];
  const matrix = one.parentMatrix;
  const committed = state._hydratedModelRecords;
  const failed = await api.applySceneCommands(state, [{ kind: 11, data: { instancedGLBMeshes: [
    batch([{ id: "one", x: 9 }]),
    { id: "bad-family", src: "/missing.glb", instances: [{ id: "bad" }] },
  ] } }]);
  assert.equal(failed[0].committed, false);
  assert.equal(state._hydratedModelRecords, committed);
  assert.equal(state.objects.get(one.id), one);
  assert.equal(one.parentMatrix, matrix, "failed staging must not apply a survivor pose early");

  state._modelOwner = () => true;
  const pending = api.applySceneCommands(state, [{ kind: 11, data: { instancedGLBMeshes: [
    batch([{ id: "one", x: 11 }, { id: "two", x: 2 }]),
  ] } }]);
  state._modelOwner = () => false;
  const stale = await pending;
  assert.equal(stale[0].stale, true);
  assert.equal(state._hydratedModelRecords, committed);
  assert.equal(state.objects.get(one.id), one);
  assert.equal(one.parentMatrix, matrix, "superseded staging must not apply a survivor pose early");
});

test("rigid membership churn retains one survivor and only live records", async () => {
  const env = importedMeshRuntime({ fetchRoutes: { "/actor.glb": { bytes: buildMinimalGLBBytes() } } });
  runScript(freshFeatureBundleSource("scene3d-gltf"), env.context, "bootstrap-feature-scene3d-gltf.js");
  const api = env.context.__gosx_scene3d_api;
  const command = instances => [{ kind: 11, data: { instancedGLBMeshes: [
    { id: "effects", src: "/actor.glb", instances },
  ] } }];
  const state = api.createSceneState({ scene: { instancedGLBMeshes: [
    { id: "effects", src: "/actor.glb", instances: [{ id: "anchor" }, { id: "slot-0" }] },
  ] } });
  await env.context.__meshTest.hydrate(state, null);
  const stages = env.context.__meshTest.countStages();
  const generation = state._modelHydrationGeneration;
  const anchor = Array.from(state.objects.values()).find(o => o.id.startsWith("effects/anchor/"));
  const vertices = anchor.vertices;
  for (let wave = 1; wave <= 100; wave += 1) {
    await api.applySceneCommands(state, command([{ id: "anchor", x: wave / 10 }, { id: "slot-" + wave, x: wave }]));
    assert.equal(state.objects.get(anchor.id), anchor);
    assert.equal(state.objects.size, 2);
    assert.equal(state._hydratedModelRecords.rigidInstances.size, 2);
    assert.equal(state._hydratedModelRecords.objects.length, 2);
  }
  assert.equal(state._modelHydrationGeneration, generation, "bounded churn must not create full hydration generations");
  assert.equal(stages(), 100, "each churn step stages only its one new member, never every survivor");
  assert.equal(anchor.vertices, vertices);
  assert.equal(anchor.parentMatrix[12], 10);
});

test("static membership changes fall back instead of leaving derived objects orphaned", async () => {
  const env = importedMeshRuntime({ fetchRoutes: { "/actor.glb": { bytes: buildMinimalGLBBytes() } } });
  runScript(freshFeatureBundleSource("scene3d-gltf"), env.context, "bootstrap-feature-scene3d-gltf.js");
  const api = env.context.__gosx_scene3d_api;
  const state = api.createSceneState({ scene: {
    models: [{ id: "deck", src: "/actor.glb", static: true }],
    instancedGLBMeshes: [{ id: "effects", src: "/actor.glb", instances: [{ id: "one" }] }],
  } });
  await env.context.__meshTest.hydrate(state, null);
  const generation = state._modelHydrationGeneration;
  const deck = Array.from(state.objects.values()).find(o => o.id.startsWith("deck/"));
  state.models = [];
  await api.applySceneCommands(state, [{ kind: 11, data: { instancedGLBMeshes: [
    { id: "effects", src: "/actor.glb", instances: [{ id: "one" }, { id: "two" }] },
  ] } }]);
  assert.ok(state._modelHydrationGeneration > generation, "changed static membership must use full hydration");
  assert.equal(state.objects.has(deck.id), false);
  assert.equal(state._hydratedModelRecords.staticModels.size, 0);
  assert.equal(state._hydratedModelRecords.rigidInstances.size, 2);
});

test("a logical pool slot can transfer families while an unrelated sibling keeps identity", async () => {
  const env = importedMeshRuntime({ fetchRoutes: { "/actor.glb": { bytes: buildMinimalGLBBytes() } } });
  runScript(freshFeatureBundleSource("scene3d-gltf"), env.context, "bootstrap-feature-scene3d-gltf.js");
  const api = env.context.__gosx_scene3d_api;
  const family = (id, instances) => ({ id, src: "/actor.glb", instances });
  const state = api.createSceneState({ scene: { instancedGLBMeshes: [
    family("shell", [{ id: "sibling" }, { id: "death-slot-7", x: 1 }]),
    family("metal", [{ id: "steady", x: 2 }]),
  ] } });
  await env.context.__meshTest.hydrate(state, null);
  const generation = state._modelHydrationGeneration;
  const sibling = Array.from(state.objects.values()).find(o => o.id.startsWith("shell/sibling/"));
  const oldSlot = Array.from(state.objects.values()).find(o => o.id.startsWith("shell/death-slot-7/"));
  await api.applySceneCommands(state, [{ kind: 11, data: { instancedGLBMeshes: [
    family("shell", [{ id: "sibling", x: 4 }]),
    family("metal", [{ id: "steady", x: 2 }, { id: "death-slot-7", x: 8 }]),
  ] } }]);
  const newSlot = Array.from(state.objects.values()).find(o => o.id.startsWith("metal/death-slot-7/"));
  assert.equal(state._modelHydrationGeneration, generation);
  assert.equal(state.objects.get(sibling.id), sibling);
  assert.equal(sibling.parentMatrix[12], 4);
  assert.equal(state.objects.has(oldSlot.id), false);
  assert.ok(newSlot);
  assert.equal(newSlot.vertices, sibling.vertices);
  assert.equal(state._hydratedModelRecords.rigidInstances.size, 3);
});

test("shared hydration templates keep per-actor IDs and observe nested overrides and texture scopes", async () => {
  const env=importedMeshRuntime({fetchRoutes:{"/actor.glb":{bytes:buildMinimalGLBBytes()}}});
  runScript(freshFeatureBundleSource("scene3d-gltf"),env.context,"bootstrap-feature-scene3d-gltf.js");
  const api=env.context.__gosx_scene3d_api;
  const batch={id:"swarm",src:"/actor.glb",specularColor:[1,.8,.6],instances:[{id:"a",x:1},{id:"b",x:2}]};
  const state=api.createSceneState({scene:{instancedGLBMeshes:[batch]}});
  await env.context.__meshTest.hydrate(state,null);
  const initial=state._modelHydrationGeneration;
  batch.instances.reverse();batch.instances[0].x=5;
  await api.applySceneCommands(state,[{kind:11,data:{instancedGLBMeshes:[batch]}}]);
  assert.equal(state._modelHydrationGeneration,initial,"reordering identical declarations preserves identity");
  assert.equal(state._hydratedModelRecords.rigidInstances.size,2);
  const actorB=Array.from(state.objects.values()).find(o=>o.id.startsWith('swarm/b/'));
  assert.equal(actorB.parentMatrix[12],5);
  batch.specularColor[1]=.25;
  await api.applySceneCommands(state,[{kind:11,data:{instancedGLBMeshes:[batch]}}]);
  assert.ok(state._modelHydrationGeneration>initial,"nested appearance changes create new templates");
  const generation=state._modelHydrationGeneration;
  state._modelTextureVariantScope={key:"changed-scope"};
  await api.applySceneCommands(state,[{kind:11,data:{instancedGLBMeshes:[batch]}}]);
  assert.ok(state._modelHydrationGeneration>generation,"scope remains part of model identity");
});

test("embedded GLB images share one URL across material slots and refresh on re-extraction", () => {
  const env=importedMeshRuntime({});
  let allocations=0;
  env.context.Blob=Blob;
  env.context.URL.createObjectURL=()=>"blob:test/"+(++allocations);
  const source=freshFeatureBundleSource("scene3d-gltf").replaceAll('sceneLoadGLTFModel: sceneLoadGLTFModel,','sceneLoadGLTFModel: sceneLoadGLTFModel, extractForTest: gltfExtractScene,');
  runScript(source,env.context,"bootstrap-feature-scene3d-gltf.js");
  const gltf={asset:{version:"2.0"},scene:0,scenes:[{nodes:[0]}],nodes:[{mesh:0}],
    meshes:[{primitives:[0,1,2].map(material=>({attributes:{POSITION:0},material}))}],
    accessors:[{bufferView:0,componentType:5126,count:3,type:"VEC3"}],
    bufferViews:[{buffer:0,byteOffset:0,byteLength:36},{buffer:0,byteOffset:36,byteLength:4}],
    buffers:[{byteLength:40}],images:[{bufferView:1,mimeType:"image/png"}],
    textures:[{source:0},{source:0},{source:0}],materials:[0,1,2].map(index=>({
      pbrMetallicRoughness:{baseColorTexture:{index},metallicRoughnessTexture:{index}},normalTexture:{index}}))};
  const bytes=new Uint8Array(40);new Float32Array(bytes.buffer,0,9).set([0,0,0,1,0,0,0,1,0]);
  const api=env.context.__gosx_scene3d_gltf_api;
  const first=api.extractForTest(gltf,bytes.buffer);
  assert.equal(first.objects.length,3);assert.equal(allocations,1);
  assert.equal(new Set(first.objects.map(o=>o.material.texture)).size,1);
  assert.equal(first.objects[0].material.texture,first.objects[0].material.normalMap);
  const second=api.extractForTest(gltf,bytes.buffer);
  assert.equal(allocations,2,"new extraction owns new image bytes");
  assert.notEqual(second.objects[0].material.texture,first.objects[0].material.texture);
});
