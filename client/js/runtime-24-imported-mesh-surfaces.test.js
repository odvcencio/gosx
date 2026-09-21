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
    "window.__meshTest = { countVertexCopies: function() { const original=sceneNormalizeMeshVertexData; let count=0; sceneNormalizeMeshVertexData=function(value) { if(value && value.positions && value.positions.length) count++; return original(value); }; return function(){return count;}; }, countVertexTransforms: function() { const original=sceneModelTransformMeshFloats; let count=0; sceneModelTransformMeshFloats=function(...args) { count++; return original(...args); }; return function(){return count;}; }, countStages: function() { const original=sceneStageModelHydration; let count=0; sceneStageModelHydration=async function(...args) { count++; return original(...args); }; return function(){return count;}; }, countMembershipWork: function() { const expand=sceneInstancedGLBModelsFromBatches, clone=sceneCloneHydrationModel, key=sceneRigidInstanceHydrationKey; let expansions=0, clones=0, keyIDs=[]; sceneInstancedGLBModelsFromBatches=function(...args){expansions++;return expand(...args);}; sceneCloneHydrationModel=function(...args){clones++;return clone(...args);}; sceneRigidInstanceHydrationKey=function(state,model,...args){keyIDs.push(String(model&&model.id||''));return key(state,model,...args);}; return function(){return {expansions:expansions,clones:clones,keys:keyIDs.length,keyIDs:keyIDs.slice()};}; }, countMaterialInputComparisons: function() { const original=sceneMaterialInputEqual; let count=0; sceneMaterialInputEqual=function(...args){count++;return original(...args);}; return function(){return count;}; }, materialProfile: sceneObjectMaterialProfile, resolveUniforms: sceneResolveMaterialUniforms, prepare: prepareScene, hydrate: hydrateSceneStateModels, normalize: normalizeSceneModel, instantiate: sceneInstantiateModelObject, transform: sceneApplyStaticModelObjectTransform, failLoad: function(src) { const original=loadSceneModelAsset; loadSceneModelAsset=async function(path,...args) { if(path===src) throw new Error('injected stage failure'); return original(path,...args); }; }, countMatrices: function() { const original = sceneObjectModelMatrix; let count = 0; sceneObjectModelMatrix = function(object, time) { count++; return original(object, time); }; return function() { return count; }; }, fallbackReference: function(object,time) { const vertexNormal=function(vertices,index){const offset=index*3;if(!vertices||!vertices.normals||vertices.normals.length<offset+3)return{x:0,y:1,z:0};return{x:sceneNumber(vertices.normals[offset],0),y:sceneNumber(vertices.normals[offset+1],1),z:sceneNumber(vertices.normals[offset+2],0)};}, vertexUV=function(vertices,index){const offset=index*2;if(!vertices||!vertices.uvs||vertices.uvs.length<offset+2)return{x:0,y:0};return{x:sceneNumber(vertices.uvs[offset],0),y:sceneNumber(vertices.uvs[offset+1],0)};}, vertexTangent=function(vertices,index){const offset=index*4;if(!vertices||!vertices.tangents||vertices.tangents.length<offset+4)return{x:1,y:0,z:0,w:1};return{x:sceneNumber(vertices.tangents[offset],1),y:sceneNumber(vertices.tangents[offset+1],0),z:sceneNumber(vertices.tangents[offset+2],0),w:sceneNumber(vertices.tangents[offset+3],1)};}, normalize=function(point){const x=sceneNumber(point&&point.x,0),y=sceneNumber(point&&point.y,0),z=sceneNumber(point&&point.z,0),length=Math.sqrt(x*x+y*y+z*z);return length<=.000001?{x:0,y:1,z:0}:{x:x/length,y:y/length,z:z/length};}, worldNormal=function(vertices,index,normalTransform){const normal=vertexNormal(vertices,index);return normalize(sceneMatrixTransformInto(normal,normalTransform,normal.x,normal.y,normal.z,3,false));}, worldTangent=function(vertices,index,modelMatrix,normal,orientation){const tangent=vertexTangent(vertices,index);sceneMatrixTransformInto(tangent,modelMatrix,tangent.x,tangent.y,tangent.z,4,false);let x=tangent.x,y=tangent.y,z=tangent.z;const normalDot=x*normal.x+y*normal.y+z*normal.z;x-=normal.x*normalDot;y-=normal.y*normalDot;z-=normal.z*normalDot;let length=Math.sqrt(x*x+y*y+z*z);if(length<=.000001){if(Math.abs(normal.x)<=Math.abs(normal.y)&&Math.abs(normal.x)<=Math.abs(normal.z)){x=0;y=-normal.z;z=normal.y;}else if(Math.abs(normal.y)<=Math.abs(normal.z)){x=normal.z;y=0;z=-normal.x;}else{x=-normal.y;y=normal.x;z=0;}length=Math.max(.000001,Math.sqrt(x*x+y*y+z*z));}return{x:x/length,y:y/length,z:z/length,w:tangent.w*orientation};}, vertices=object.vertices, matrix=sceneObjectModelMatrix(object,time||0), linear=sceneObjectMeshBakeLinearState(object,matrix), indices=vertices.indices instanceof Uint32Array&&vertices.indices.length>=3&&vertices.indices.length%3===0?vertices.indices:null, count=indices?indices.length:vertices.count, normals=[],uvs=[],tangents=[];for(let tri=0;tri+2<count;tri+=3){const a=indices?indices[tri]:tri,b=indices?indices[tri+1]:tri+1,c=indices?indices[tri+2]:tri+2,order=linear[9]<0?[a,c,b]:[a,b,c];for(const source of order){const normal=worldNormal(vertices,source,linear),uv=vertexUV(vertices,source),tangent=worldTangent(vertices,source,matrix,normal,linear[9]);normals.push(normal.x,normal.y,normal.z);uvs.push(uv.x,uv.y);tangents.push(tangent.x,tangent.y,tangent.z,tangent.w);}}return{normals,uvs,tangents};}, countFallbackAttributeHelpers: function() { const counts={allocating:0,normalInto:0,worldNormalInto:0,uvInto:0,tangentInto:0,worldTangentInto:0,normalizeInto:0}; for (const name of ['sceneMeshVertexNormal','sceneMeshWorldNormal','sceneMeshVertexUV','sceneMeshVertexTangent','sceneMeshWorldTangent']) { const original=eval(name); eval(name+'=function(...args){counts.allocating++;return original(...args);}'); } for (const pair of [['sceneMeshVertexNormalInto','normalInto'],['sceneMeshWorldNormalInto','worldNormalInto'],['sceneMeshVertexUVInto','uvInto'],['sceneMeshVertexTangentInto','tangentInto'],['sceneMeshWorldTangentInto','worldTangentInto'],['sceneNormalizeDirectionInto','normalizeInto']]) { const original=eval(pair[0]); eval(pair[0]+'=function(...args){counts[pair[1]]++;return original(...args);}'); } return function(){return Object.assign({},counts);}; }, worldBakeCache: sceneWorldBakedGeometryCacheDiagnostics }; window.__gosx_scene3d_available = true;",
  );
  runScript(source, env.context, "bootstrap-feature-scene3d.js");
  return env;
}

function buildWorldBakedBundle(env, objects, timeSeconds = 0, camera = null) {
  return env.context.__gosx_scene3d_api.createSceneRenderBundle(640, 360, "#000000",
    camera || { x: 0, y: 1, z: 8, fov: 72, near: .05, far: 128 },
    objects, [], [], [], [], {}, timeSeconds, [], [], [], [], [], 0, false,
    { retainedGeometry: true, rigidImportedBatches: true });
}

function immutableAuthoredShaderTriangle(env, overrides = {}) {
  const F32 = vm.runInContext("Float32Array", env.context);
  const U32 = vm.runInContext("Uint32Array", env.context);
  return Object.assign({
    id: "authored-baked", kind: "mesh", visible: true, static: true,
    x: 2, y: -1, z: .5, rotationX: 0, rotationY: 0, rotationZ: 0,
    scaleX: 2, scaleY: .5, scaleZ: 1,
    materialKind: "standard", color: "#204060", wireframe: false,
    customVertex: "void main() { gl_Position = vec4(a_position, 1.0); }",
    customFragment: "void main() { gl_FragColor = vec4(1.0); }",
    vertices: {
      count: 3,
      positions: new F32([0,0,0, 1,0,0, 0,1,0]),
      normals: new F32([0,0,1, 0,0,1, 0,0,1]),
      uvs: new F32([0,0, 1,0, 0,1]),
      tangents: new F32([1,0,0,1, 1,0,0,1, 1,0,0,1]),
      indices: new U32([0,1,2]),
      immutable: true, revision: 0, dynamic: false,
    },
  }, overrides);
}

function fallbackHelperCalls(counts) {
  return {
    normalInto: counts.normalInto,
    worldNormalInto: counts.worldNormalInto,
    uvInto: counts.uvInto,
    tangentInto: counts.tangentInto,
    worldTangentInto: counts.worldTangentInto,
    normalizeInto: counts.normalizeInto,
  };
}

test("immutable authored-shader world bake reuses exact finalized attributes without caching live routing", () => {
  const env = importedMeshRuntime();
  const object = immutableAuthoredShaderTriangle(env, { customUniforms: { pulse: .25 } });
  const reference = env.context.__meshTest.fallbackReference(object, 0);
  const helperCounts = env.context.__meshTest.countFallbackAttributeHelpers();
  const first = buildWorldBakedBundle(env, [object]);
  const firstCalls = fallbackHelperCalls(helperCounts());
  assert.deepEqual(firstCalls, {
    normalInto: 3, worldNormalInto: 3, uvInto: 3,
    tangentInto: 3, worldTangentInto: 3, normalizeInto: 3,
  });
  assert.deepEqual(Array.from(first.worldMeshPositions), [2,-1,.5, 4,-1,.5, 2,-.5,.5]);
  assert.deepEqual(Array.from(first.worldMeshNormals), Array.from(new (first.worldMeshNormals.constructor)(reference.normals)));
  assert.deepEqual(Array.from(first.worldMeshUVs), Array.from(new (first.worldMeshUVs.constructor)(reference.uvs)));
  assert.deepEqual(Array.from(first.worldMeshTangents), Array.from(new (first.worldMeshTangents.constructor)(reference.tangents)));
  assert.deepEqual({ ...first.meshObjects[0].bounds }, {
    minX: 2, minY: -1, minZ: .5, maxX: 4, maxY: -.5, maxZ: .5,
  });
  const snapshot = {
    positions: Array.from(first.worldMeshPositions),
    normals: Array.from(first.worldMeshNormals),
    uvs: Array.from(first.worldMeshUVs),
    tangents: Array.from(first.worldMeshTangents),
  };
  first.worldMeshPositions[0] = 999;
  first.worldMeshNormals[0] = 999;
  object.id = "authored-baked-live-id";
  object.color = "#80a0c0";
  object.blendMode = "alpha";
  object.opacity = .75;
  object.customUniforms = { pulse: .75 };
  const second = buildWorldBakedBundle(env, [object], 3.25,
    { x: 0, y: 1, z: 12, fov: 72, near: .05, far: 128 });
  assert.deepEqual(fallbackHelperCalls(helperCounts()), firstCalls, "cache hit skips every per-vertex bake helper");
  assert.deepEqual(Array.from(second.worldMeshPositions), snapshot.positions);
  assert.deepEqual(Array.from(second.worldMeshNormals), snapshot.normals);
  assert.deepEqual(Array.from(second.worldMeshUVs), snapshot.uvs);
  assert.deepEqual(Array.from(second.worldMeshTangents), snapshot.tangents);
  assert.equal(second.meshObjects[0].id, "authored-baked-live-id");
  assert.equal(second.meshObjects[0].renderPass, "alpha");
  assert.equal(second.timeSeconds, 3.25);
  assert.equal(second.materials[0].customUniforms.pulse, .75);
  assert.notEqual(second.meshObjects[0].depthCenter, first.meshObjects[0].depthCenter,
    "camera-relative depth remains live on a geometry hit");
  assert.notDeepEqual(Array.from(second.worldMeshColors), Array.from(first.worldMeshColors),
    "current material color must remain outside the geometry cache");
  assert.deepEqual({ ...env.context.__meshTest.worldBakeCache() }, {
    entries: 1, bytes: 144, maxBytes: 2 * 1024 * 1024, epoch: 2,
  });
});

test("authored-shader world bake invalidates exact transform, revision, attribute, and index changes", () => {
  const env = importedMeshRuntime();
  const F32 = vm.runInContext("Float32Array", env.context);
  const U32 = vm.runInContext("Uint32Array", env.context);
  const object = immutableAuthoredShaderTriangle(env);
  const helperCounts = env.context.__meshTest.countFallbackAttributeHelpers();
  const build = () => buildWorldBakedBundle(env, [object]);
  build();
  build();
  assert.equal(helperCounts().worldNormalInto, 3);

  object.x = 3;
  const moved = build();
  assert.equal(helperCounts().worldNormalInto, 6, "matrix change rebakes");
  assert.equal(moved.worldMeshPositions[0], 3);

  object.vertices.normals = new F32([0,1,0, 0,1,0, 0,1,0]);
  const replacedNormal = build();
  assert.equal(helperCounts().worldNormalInto, 9, "attribute identity change rebakes at the same revision");
  assert.deepEqual(Array.from(replacedNormal.worldMeshNormals.slice(0, 3)), [0,1,0]);

  object.vertices.positions[0] = 2;
  object.vertices.revision += 1;
  const revised = build();
  assert.equal(helperCounts().worldNormalInto, 12, "revision change rebakes in-place attribute edits");
  assert.equal(revised.worldMeshPositions[0], 7);

  object.vertices.indices = new U32([0,2,1]);
  const reindexed = build();
  assert.equal(helperCounts().worldNormalInto, 15, "index identity change rebakes");
  assert.deepEqual(Array.from(reindexed.worldMeshUVs), [0,0, 0,1, 1,0]);

  object.scaleX = -2;
  const reflected = build();
  assert.equal(helperCounts().worldNormalInto, 18, "reflection matrix rebakes");
  assert.deepEqual(Array.from(reflected.worldMeshUVs), [0,0, 1,0, 0,1],
    "negative determinant keeps the established CCW source order adjustment");
  assert.deepEqual(Array.from(reflected.worldMeshTangents).filter((_, index) => index % 4 === 3), [-1,-1,-1]);
});

test("authored-shader world bake observes in-place parent matrices and reuses the new transform", () => {
  const env = importedMeshRuntime();
  const F32 = vm.runInContext("Float32Array", env.context);
  const parentMatrix = new F32([
    1,0,0,0, 0,1,0,0, 0,0,1,0, 0,0,0,1,
  ]);
  const object = immutableAuthoredShaderTriangle(env, {
    id: "parent-matrix-owner", parentMatrix,
    x: 0, y: 0, z: 0, rotationX: 0, rotationY: 0, rotationZ: 0,
    scaleX: 1, scaleY: 1, scaleZ: 1,
  });
  const helperCounts = env.context.__meshTest.countFallbackAttributeHelpers();
  const first = buildWorldBakedBundle(env, [object]);
  buildWorldBakedBundle(env, [object]);
  assert.equal(helperCounts().worldNormalInto, 3);
  parentMatrix[12] = 4;
  const moved = buildWorldBakedBundle(env, [object]);
  assert.equal(helperCounts().worldNormalInto, 6, "in-place parent matrix edit invalidates");
  assert.equal(moved.worldMeshPositions[0], first.worldMeshPositions[0] + 4);
  const repeated = buildWorldBakedBundle(env, [object]);
  assert.equal(helperCounts().worldNormalInto, 6, "the replacement transform becomes the next exact hit");
  assert.deepEqual(Array.from(repeated.worldMeshPositions), Array.from(moved.worldMeshPositions));
});

test("mutable, dirty, and wire authored-shader meshes never enter the world-bake cache", () => {
  for (const fixture of [
    { name: "mutable", mutate: object => { object.vertices.immutable = false; } },
    { name: "dynamic", mutate: object => { object.vertices.dynamic = true; } },
    { name: "dirty", mutate: object => { object.geometryDirty = true; } },
    { name: "wire", mutate: object => { object.wireframe = true; } },
  ]) {
    const env = importedMeshRuntime();
    const object = immutableAuthoredShaderTriangle(env, { id: fixture.name });
    fixture.mutate(object);
    const helperCounts = env.context.__meshTest.countFallbackAttributeHelpers();
    const first = buildWorldBakedBundle(env, [object]);
    const second = buildWorldBakedBundle(env, [object]);
    assert.equal(helperCounts().worldNormalInto, 6, fixture.name);
    assert.equal(env.context.__meshTest.worldBakeCache().entries, 0, fixture.name);
    if (fixture.name === "wire") {
      assert.ok(first.worldPositions.length > 0 && second.worldPositions.length > 0,
        "wire lighting and line geometry remain on the live path");
    }
  }
});

test("world-bake cache keeps hot owners, declines over-budget churn, and releases stale payloads", () => {
  const env = importedMeshRuntime();
  const F32 = vm.runInContext("Float32Array", env.context);
  function largeVertices(vertexCount) {
    const positions = new F32(vertexCount * 3);
    const normals = new F32(vertexCount * 3);
    const uvs = new F32(vertexCount * 2);
    const tangents = new F32(vertexCount * 4);
    for (let index = 0; index < vertexCount; index += 1) {
      const corner = index % 3;
      positions[index * 3] = corner === 1 ? 1 : 0;
      positions[index * 3 + 1] = corner === 2 ? 1 : 0;
      normals[index * 3 + 2] = 1;
      uvs[index * 2] = corner === 1 ? 1 : 0;
      uvs[index * 2 + 1] = corner === 2 ? 1 : 0;
      tangents[index * 4] = 1;
      tangents[index * 4 + 3] = 1;
    }
    return { count: vertexCount, positions, normals, uvs, tangents,
      indices: null, immutable: true, revision: 0, dynamic: false };
  }
  const hot = immutableAuthoredShaderTriangle(env, {
    id: "hot-cache-owner", vertices: largeVertices(30000), x: 0, y: 0, z: 0,
    scaleX: 1, scaleY: 1, scaleZ: 1,
  });
  const churnVertices = largeVertices(15000);
  const helperCounts = env.context.__meshTest.countFallbackAttributeHelpers();
  buildWorldBakedBundle(env, [hot]);
  const hotDiagnostics = env.context.__meshTest.worldBakeCache();
  assert.deepEqual({ entries: hotDiagnostics.entries, bytes: hotDiagnostics.bytes },
    { entries: 1, bytes: 30000 * 48 });

  let lastChurn = null;
  for (let index = 0; index < 12; index += 1) {
    lastChurn = immutableAuthoredShaderTriangle(env, {
      id: `replacement-${index}`, vertices: churnVertices, x: index + 2,
      y: 0, z: 0, scaleX: 1, scaleY: 1, scaleZ: 1,
    });
    const bundle = buildWorldBakedBundle(env, [hot, lastChurn]);
    assert.equal(bundle.meshObjects[0].vertexOffset, 0);
    assert.equal(bundle.meshObjects[1].vertexOffset, 30000);
    const diagnostics = env.context.__meshTest.worldBakeCache();
    assert.equal(diagnostics.entries, 1, "new owners cannot evict the hot record");
    assert.equal(diagnostics.bytes, 30000 * 48);
    assert.ok(diagnostics.bytes <= diagnostics.maxBytes);
  }
  assert.equal(helperCounts().worldNormalInto, 30000 + 12 * 15000,
    "the hot owner hits while each over-budget replacement bakes once");

  for (let index = 0; index < 9; index += 1) buildWorldBakedBundle(env, []);
  assert.deepEqual({
    entries: env.context.__meshTest.worldBakeCache().entries,
    bytes: env.context.__meshTest.worldBakeCache().bytes,
  }, { entries: 0, bytes: 0 }, "stale residency releases its complete payload");

  const beforeReturn = helperCounts().worldNormalInto;
  buildWorldBakedBundle(env, [lastChurn]);
  buildWorldBakedBundle(env, [lastChurn]);
  assert.equal(helperCounts().worldNormalInto - beforeReturn, 15000,
    "a replacement owner can enter the cache after stale bytes are reclaimed");
  assert.equal(env.context.__meshTest.worldBakeCache().bytes, 15000 * 48);
});

test("world-baked mesh attribute scratches preserve exact legacy output without aliasing", () => {
  const env = importedMeshRuntime();
  const F32 = vm.runInContext("Float32Array", env.context);
  const U32 = vm.runInContext("Uint32Array", env.context);
  const vertices = {
    count: 6,
    positions: new F32([0,0,0, 1,0,0, 0,1,0, 1,0,0, 1,1,0, 0,1,0]),
    normals: new F32([0,0,1, 0,1,1, NaN,0,1, 1,0,0]),
    uvs: new F32([0,0, 1,0, .25,1, .5,.25, NaN,.75]),
    tangents: new F32([1,0,0,1, 0,0,0,-1, NaN,1,0,1, 0,1,0,-1]),
    indices: new U32([0,1,2, 3,4,5]),
    dynamic: true,
  };
  const object = {
    id: "baked-reference", kind: "mesh", vertices,
    x: 1.25, y: -.5, z: .75,
    rotationX: .31, rotationY: -.47, rotationZ: .22,
    scaleX: -1.7, scaleY: .65, scaleZ: 1.25,
    materialKind: "standard", color: "#62869a", roughness: .6, metalness: .1,
    selected: true, outlineColor: "#ffd34d", outlineWidth: 2,
    visible: true, pickable: true,
  };
  const reference = env.context.__meshTest.fallbackReference(object, 0);
  const countHelpers = env.context.__meshTest.countFallbackAttributeHelpers();
  const first = buildWorldBakedBundle(env, [object]);
  assert.deepEqual(Array.from(first.worldMeshNormals), Array.from(new F32(reference.normals)));
  assert.deepEqual(Array.from(first.worldMeshUVs), Array.from(new F32(reference.uvs)));
  assert.deepEqual(Array.from(first.worldMeshTangents), Array.from(new F32(reference.tangents)));
  assert.ok(first.worldPositions.length > 0, "wire selection still emits its independent line geometry");
  assert.deepEqual({ ...countHelpers() }, {
    allocating: 0,
    normalInto: 6,
    worldNormalInto: 6,
    uvInto: 6,
    tangentInto: 6,
    worldTangentInto: 6,
    normalizeInto: 6,
  });

  const snapshot = {
    positions: Array.from(first.worldMeshPositions),
    normals: Array.from(first.worldMeshNormals),
    uvs: Array.from(first.worldMeshUVs),
    tangents: Array.from(first.worldMeshTangents),
  };
  const missing = {
    id: "missing-attributes", kind: "mesh", visible: true,
    x: -2, y: 1, z: 0, rotationX: 0, rotationY: .2, rotationZ: 0,
    scaleX: 1, scaleY: 2, scaleZ: .5,
    materialKind: "standard", color: "#ffffff",
    vertices: { count: 3, positions: new F32([0,0,0, 0,1,0, 1,0,0]), dynamic: true },
  };
  const missingReference = env.context.__meshTest.fallbackReference(missing, 0);
  const second = buildWorldBakedBundle(env, [missing, object]);
  assert.deepEqual(Array.from(second.worldMeshNormals.slice(0, 9)), Array.from(new F32(missingReference.normals)));
  assert.deepEqual(Array.from(second.worldMeshUVs.slice(0, 6)), Array.from(new F32(missingReference.uvs)));
  assert.deepEqual(Array.from(second.worldMeshTangents.slice(0, 12)), Array.from(new F32(missingReference.tangents)));
  assert.deepEqual(Array.from(first.worldMeshPositions), snapshot.positions, "later meshes cannot alias prior frame positions");
  assert.deepEqual(Array.from(first.worldMeshNormals), snapshot.normals, "later valid/default mixes cannot overwrite prior normals");
  assert.deepEqual(Array.from(first.worldMeshUVs), snapshot.uvs, "later valid/default mixes cannot overwrite prior UVs");
  assert.deepEqual(Array.from(first.worldMeshTangents), snapshot.tangents, "later valid/default mixes cannot overwrite prior tangents");
});

test("representative 8,184-vertex fallback uses only fixed attribute scratches", () => {
  const env = importedMeshRuntime();
  const F32 = vm.runInContext("Float32Array", env.context);
  const vertexCount = 8184;
  const positions = new F32(vertexCount * 3);
  const normals = new F32(vertexCount * 3);
  const uvs = new F32(vertexCount * 2);
  const tangents = new F32(vertexCount * 4);
  for (let index = 0; index < vertexCount; index += 1) {
    const corner = index % 3;
    positions[index * 3] = corner === 1 ? 1 : 0;
    positions[index * 3 + 1] = corner === 2 ? 1 : 0;
    normals[index * 3 + 2] = 1;
    uvs[index * 2] = corner === 1 ? 1 : 0;
    uvs[index * 2 + 1] = corner === 2 ? 1 : 0;
    tangents[index * 4] = 1;
    tangents[index * 4 + 3] = 1;
  }
  const object = { id: "representative-baked-deck", kind: "mesh", visible: true,
    x: 0, y: 0, z: 0, rotationX: 0, rotationY: 0, rotationZ: 0,
    scaleX: 1, scaleY: 1, scaleZ: 1,
    materialKind: "standard", color: "#888888",
    vertices: { count: vertexCount, positions, normals, uvs, tangents, dynamic: true } };
  const countHelpers = env.context.__meshTest.countFallbackAttributeHelpers();
  const bundle = buildWorldBakedBundle(env, [object]);
  assert.equal(bundle.worldMeshPositions.length, vertexCount * 3);
  assert.deepEqual({ ...countHelpers() }, {
    allocating: 0,
    normalInto: vertexCount,
    worldNormalInto: vertexCount,
    uvInto: vertexCount,
    tangentInto: vertexCount,
    worldTangentInto: vertexCount,
    normalizeInto: vertexCount,
  });
  // Five former helper-result objects per vertex plus three former attribute
  // arrays per triangle cost 6*N allocations. The fallback now owns twelve
  // bounded scratch objects/arrays per mesh: for this fixture the net removal
  // is 6*8,184 - 12 = 49,092 allocations per invocation.
});

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

function earlyRigidFixture(fresh, xs, overrides = {}, rendererOptions = {}) {
  const h = createWebGLRendererForPost(Object.assign({ fresh }, rendererOptions));
  const api = h.env.context.__gosx_scene3d_api;
  const F32 = vm.runInContext("Float32Array", h.env.context);
  const U32 = vm.runInContext("Uint32Array", h.env.context);
  const vertices = { count:3, immutable:true, revision:0, _rigidPool:true,
    positions:new F32([-.1,0,0, .1,0,0, 0,.2,0]),
    normals:new F32([0,0,1, 0,0,1, 0,0,1]),
    uvs:new F32([0,0,1,0,.5,1]), tangents:new F32([1,0,0,1, 1,0,0,1, 1,0,0,1]),
    indices:new U32([0,1,2]) };
  const objects = xs.map((x, index) => Object.assign({
    id:"effect-"+index, kind:"mesh", vertices, _rigidMaterialProfileStable:true,
    _rigidSharedAppearance:true,
    pickable:false, visible:true, castShadow:false, receiveShadow:false,
    materialKind:"standard", color:"#669966", opacity:1, roughness:.5, metalness:0,
    wireframe:false, blendMode:"opaque", renderPass:"opaque", depthWrite:true,
    scaleX:1, scaleY:1, scaleZ:1, x:0, y:0, z:0,
    rotationX:0, rotationY:0, rotationZ:0,
    parentMatrix:new F32([1,0,0,0, 0,1,0,0, 0,0,1,0, x,0,0,1]),
  }, overrides[index] || {}));
  const build = (source = objects, capabilities = { retainedGeometry:true, rigidImportedBatches:true }) =>
    api.createSceneRenderBundle(320,180,"#000000",{x:0,y:0,z:6,fov:72,near:.05,far:128},
      source,[],[],[],[],{},0,[],[],[],[],[],0,false,capabilities);
  return { ...h, api, F32, vertices, objects, build };
}

for (const fresh of [true, false]) {
  test(`WebGL ${fresh ? "source" : "generated"} builds early rigid imported cohorts across 0/1/2 membership`, () => {
    const h = earlyRigidFixture(fresh, [1,2]);
    assert.equal(h.build([]).meshObjects.length, 0);
    const one = h.build([h.objects[0]]);
    assert.equal(one.meshObjects.length, 1);
    assert.equal(one.meshObjects[0]._rigidImportedBatch, true);
    assert.equal(one.meshObjects[0].instanceCount, 1);
    assert.equal(one.meshObjects[0].modelMatrix[12], 1);
    const stableID = one.meshObjects[0].id;
    assert.equal(h.build([]).meshObjects.length,0);
    assert.equal(h.build([h.objects[0]]).meshObjects[0].id,stableID,
      "an empty wave must not discard the recent cohort identity");
    const two = h.build();
    assert.equal(two.meshObjects.length, 1);
    assert.equal(two.meshObjects[0].id, stableID);
    assert.equal(two.meshObjects[0].instanceCount, 2);
    assert.deepEqual(Array.from(two.meshObjects[0].instanceMatrices,matrix=>matrix[12]),[1,2]);
    h.objects[0].parentMatrix[12] = 4;
    const moved = h.build([h.objects[0]]);
    assert.equal(moved.meshObjects[0].id, stableID);
    assert.equal(moved.meshObjects[0].modelMatrix[12], 4,
      "a 1-member cohort must carry the current pose for the ordinary draw fallback");
    const firstPlan=h.api.prepareScene(one,one.camera,{width:320,height:180},null,{});
    const movedPlan=h.api.prepareScene(moved,moved.camera,{width:320,height:180},firstPlan,{});
    assert.equal(movedPlan,firstPlan,"pose-only cohort changes keep the prepared pass identity");
    assert.equal(movedPlan.pbrPasses.opaque[0],moved.meshObjects[0],
      "the cache-hit planner must still route the current cohort transform record");
    h.renderer.dispose();
  });
}

test("early rigid imported cohorts fail closed for unsafe and backend-fallback records", () => {
  const h = earlyRigidFixture(true, [0,1,2,3], {
    1:{pickable:true}, 2:{castShadow:true}, 3:{opacity:"var(--mesh-opacity)"},
  });
  const bundle = h.build();
  assert.equal(bundle.meshObjects.length, 4, "one safe cohort plus three ordinary unsafe records");
  assert.equal(bundle.meshObjects.filter(object=>object._rigidImportedBatch===true).length, 1);
  assert.equal(bundle.meshObjects.filter(object=>object._rigidImportedBatch!==true).length, 3);
  const fallback = h.build([h.objects[0],h.objects[1]], {retainedGeometry:true});
  assert.equal(fallback.meshObjects.length, 2);
  assert.equal(fallback.meshObjects.some(object=>object._rigidImportedBatch===true), false,
    "renderers without the explicit capability retain the per-object contract");
  h.renderer.dispose();
});

test("early rigid imported cohorts require an explicit shared-appearance contract", () => {
  const h=earlyRigidFixture(true,[0,1],{0:{_rigidSharedAppearance:false},1:{_rigidSharedAppearance:false}});
  const ordinary=h.build();
  assert.equal(ordinary.meshObjects.length,2);
  assert.equal(ordinary.meshObjects.some(object=>object._rigidImportedBatch===true),false,
    "the default path keeps individual record IDs available to scene-node CSS");
  h.objects[0]._rigidSharedAppearance=true;
  h.objects[1]._rigidSharedAppearance=true;
  const shared=h.build();
  assert.equal(shared.meshObjects.length,1);
  assert.equal(shared.meshObjects[0]._rigidImportedBatch,true);
  h.renderer.dispose();
});

test("failed instanced shader preflight preserves ordinary imported records", () => {
  const h=earlyRigidFixture(true,[0,1],{}, {rejectShaderSources:["in mat4 a_instanceMatrix;"]});
  assert.equal(h.renderer.supportsRigidImportedBatches,false);
  const bundle=h.build(h.objects,{retainedGeometry:true,
    rigidImportedBatches:h.renderer.supportsRigidImportedBatches});
  assert.equal(bundle.meshObjects.length,2,"shader failure must not collapse records that the fallback cannot draw");
  assert.equal(bundle.meshObjects.some(object=>object._rigidImportedBatch===true),false);
  h.renderer.dispose();
});

test("dynamic registered materials and explicit geometry revisions bypass stale cohort descriptors", () => {
  const h=earlyRigidFixture(true,[0]);
  let pulse=0;
  h.api.registerSceneMaterialProfile("pulse-shell",{shaderData:()=>[1,++pulse,1]});
  const dynamic=Object.assign({},h.objects[0],{materialKind:"pulse-shell"});
  const firstDynamic=h.build([dynamic]);
  const secondDynamic=h.build([dynamic]);
  assert.equal(firstDynamic.meshObjects[0]._rigidImportedBatch,undefined);
  assert.equal(secondDynamic.meshObjects[0]._rigidImportedBatch,undefined);
  assert.ok(secondDynamic.materials[0].shaderData[1]>firstDynamic.materials[0].shaderData[1],
    "a registered shader-data factory keeps its per-frame external-state contract");
  h.api.unregisterSceneMaterialProfile("pulse-shell");
  const first=h.build();
  const firstID=first.meshObjects[0].id;
  h.vertices.revision=1;
  h.vertices.positions[0]=-2;
  const revised=h.build();
  assert.equal(revised.meshObjects[0].geometryRevision,1);
  assert.notEqual(revised.meshObjects[0].id,firstID);
  assert.ok(revised.meshObjects[0].bounds.minX<-1,
    "revision changes must recompute local bounds instead of reusing a cached descriptor");
  h.renderer.dispose();
});

test("early rigid imported cohorts preserve per-instance side-frustum culling and stream cleanup", () => {
  const h = earlyRigidFixture(true, [0,100]);
  let bundle = h.build();
  h.renderer.render(bundle,{cssWidth:320,cssHeight:180,width:320,height:180});
  const firstDraws = h.canvas.getContext("webgl2").ops.filter(op=>op[0]==="drawElements");
  assert.equal(firstDraws.length,1,"one surviving member uses the ordinary retained draw with its current matrix");
  assert.equal(bundle.meshObjects[0].modelMatrix[12],0);
  const stableID = bundle.meshObjects[0].id;
  h.objects[1].parentMatrix[12] = .5;
  bundle = h.build();
  assert.equal(bundle.meshObjects[0].id,stableID);
  h.renderer.render(bundle,{cssWidth:320,cssHeight:180,width:320,height:180});
  assert.equal(h.canvas.getContext("webgl2").ops.filter(op=>op[0]==="drawElementsInstanced").at(-1)[5],2);
  const removedAt = h.canvas.getContext("webgl2").ops.length;
  bundle = h.build([]);
  bundle.points=[{id:"keep",count:1,positions:new h.F32([0,0,0]),color:"#fff"}];
  h.renderer.render(bundle,{cssWidth:320,cssHeight:180,width:320,height:180});
  assert.ok(h.canvas.getContext("webgl2").ops.slice(removedAt).some(op=>op[0]==="deleteBuffer"),
    "zero membership retires the cohort transform stream while retained geometry follows its pool policy");
  const repopulatedAt=h.canvas.getContext("webgl2").ops.length;
  bundle=h.build([h.objects[0],h.objects[1]]);
  assert.equal(bundle.meshObjects[0].id,stableID);
  h.renderer.render(bundle,{cssWidth:320,cssHeight:180,width:320,height:180});
  assert.equal(h.canvas.getContext("webgl2").ops.slice(repopulatedAt)
    .filter(op=>op[0]==="bufferData" && op[3]===h.canvas.getContext("webgl2").STATIC_DRAW).length,0,
    "repopulation reuses retained geometry while creating only a fresh transform stream");
  const resizedAt=h.canvas.getContext("webgl2").ops.length;
  h.renderer.render(h.build([h.objects[0]]),{cssWidth:320,cssHeight:180,width:320,height:180});
  h.renderer.render(h.build([h.objects[0],h.objects[1]]),{cssWidth:320,cssHeight:180,width:320,height:180});
  assert.equal(h.canvas.getContext("webgl2").ops.slice(resizedAt)
    .filter(op=>op[0]==="bufferData" && op[4]===h.canvas.getContext("webgl2").DYNAMIC_DRAW).length,0,
    "1-to-many membership must reuse geometric CPU and GPU transform capacity");
  h.renderer.dispose();
});

test("early rigid cohort identity cache is bounded across immutable appearance replacement", () => {
  const h=earlyRigidFixture(true,[0]);
  const first=h.build().meshObjects[0].id;
  for(let index=1;index<=40;index++) {
    const object=Object.assign({},h.objects[0],{id:"variant-"+index,color:"#"+index.toString(16).padStart(6,"0")});
    assert.equal(h.build([object]).meshObjects.length,1);
  }
  const restored=Object.assign({},h.objects[0],{id:"restored"});
  assert.notEqual(h.build([restored]).meshObjects[0].id,first,
    "the per-geometry cache must evict old appearance keys instead of growing without bound");
  h.renderer.dispose();
});

test("early rigid imported cohorts reduce an 832-member bundle to one planner record", () => {
  const h=earlyRigidFixture(true,Array.from({length:832},(_,i)=>(i%32)*.05-1));
  const batched=h.build();
  const fallback=h.build(h.objects,{retainedGeometry:true});
  assert.equal(batched.meshObjects.length,1);
  assert.equal(batched.meshObjects[0].instanceCount,832);
  assert.equal(fallback.meshObjects.length,832);
  assert.equal(batched.retainedMeshObjectCount,fallback.retainedMeshObjectCount);
  assert.equal(batched.retainedMeshVertexCount,fallback.retainedMeshVertexCount);
  h.renderer.dispose();
});

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

test("pooled swarm geometry retains a returning wave between 32 and 48 MiB", () => {
  const h=rigidBatchFixture(true,1), F32=vm.runInContext("Float32Array",h.env.context);
  const padded=new F32(3*400000);
  padded.set([-.1,0,0,.1,0,0,0,.2,0]);
  const base=h.bundle.meshObjects[0];
  const actors=Array.from({length:4},(_,index)=>Object.assign({},base,{
    id:"heavy-pool-"+index,
    vertices:Object.assign({},base.vertices,{count:400000,positions:padded,normals:padded,_rigidPool:true}),
    vertexCount:400000,
    modelMatrix:new F32(base.modelMatrix),
  }));
  h.bundle.meshObjects=actors;
  h.render();
  const warm=h.renderer.diagnostics().retainedGeometry;
  h.bundle.meshObjects=[];
  h.bundle.points=[{id:"keep",count:1,positions:new F32([0,0,0]),color:"#fff"}];
  h.render();
  const idle=h.renderer.diagnostics().retainedGeometry;
  assert.ok(idle.idleBytes>32*1024*1024,
    `the measured returning wave must exceed the former 32 MiB limit (got ${idle.idleBytes})`);
  assert.ok(idle.idleBytes<=48*1024*1024,"idle residency remains within its bounded 48 MiB limit");
  assert.equal(idle.retirements,warm.retirements,"the complete sub-48 MiB wave must remain resident");
  const start=h.gl.ops.length;
  h.bundle.meshObjects=actors;
  h.render();
  assert.equal(h.gl.ops.slice(start).filter(op=>op[0]==="bufferData" && op[3]===h.gl.STATIC_DRAW).length,0,
    "the returning wave must reuse every retained static vertex stream");
  const beforeRevision=h.renderer.diagnostics().retainedGeometry;
  actors[0].vertices.revision=1;
  actors[0].geometryRevision=1;
  h.render();
  assert.equal(h.renderer.diagnostics().retainedGeometry.revisionInvalidations,beforeRevision.revisionInvalidations+1,
    "an explicit revision must still invalidate pooled geometry");
  h.renderer.dispose();
});

test("instanced GLB sharedAppearance reaches only opted-in rigid wrappers", async () => {
  const env=importedMeshRuntime({fetchRoutes:{"/actor.glb":{bytes:buildMinimalGLBBytes()}}});
  runScript(freshFeatureBundleSource("scene3d-gltf"),env.context,"bootstrap-feature-scene3d-gltf.js");
  const api=env.context.__gosx_scene3d_api;
  const state=api.createSceneState({scene:{instancedGLBMeshes:[
    {id:"shared",src:"/actor.glb",sharedAppearance:true,instances:[{id:"one"}]},
    {id:"styled",src:"/actor.glb",instances:[{id:"two"}]},
  ]}});
  await env.context.__meshTest.hydrate(state,null);
  const objects=Array.from(state.objects.values());
  const shared=objects.find(object=>object.id.startsWith("shared/one/"));
  assert.equal(shared._rigidSharedAppearance,true);
  assert.equal(objects.find(object=>object.id.startsWith("styled/two/"))._rigidSharedAppearance,false);
  await api.applySceneCommands(state,[{kind:11,data:{instancedGLBMeshes:[
    {id:"shared",src:"/actor.glb",instances:[{id:"one"}]},
    {id:"styled",src:"/actor.glb",instances:[{id:"two"}]},
  ]}}]);
  const cleared=Array.from(state.objects.values()).find(object=>object.id.startsWith("shared/one/"));
  assert.notEqual(cleared,shared,"clearing the contract must invalidate the trusted hydration template");
  assert.equal(cleared._rigidSharedAppearance,false,"an omitted replacement field restores exact per-node CSS semantics");
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
  const membershipWork = env.context.__meshTest.countMembershipWork();
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
  const work = membershipWork();
  assert.equal(work.expansions, 1,
    "one normalized InstancedGLB snapshot must serve count detection and the async transaction");
  assert.equal(work.clones, 0, "owned InstancedGLB snapshots must not deep-clone every survivor");
  assert.ok(work.keys > 0, "the new member still receives a validated hydration identity");
  assert.equal(work.keyIDs.includes("swarm/one"), false,
    "the indexed survivor should not regenerate its full JSON hydration key");
  assert.equal(state._hydratedModelRecords.rigidInstances.size, 2);
  const before = first.parentMatrix;
  await api.applySceneCommands(state, [{ kind: 11, data: { instancedGLBMeshes: [
    { id: "swarm", src: "/actor.glb", color: "#ff0000", instances: [{ id: "one", x: 8 }] },
  ] } }]);
  assert.ok(state._modelHydrationGeneration > generation, "material changes still use full atomic hydration");
  assert.equal(state._hydratedModelRecords.rigidInstances.size, 1);
  assert.equal(first.parentMatrix, before, "replacing a collection must not mutate an old committed wrapper");
});

test("rigid imported material profiles skip deep comparison until a supported mutable route is used", async () => {
  const env = importedMeshRuntime({ fetchRoutes: { "/actor.glb": { bytes: buildMinimalGLBBytes() } } });
  runScript(freshFeatureBundleSource("scene3d-gltf"), env.context, "bootstrap-feature-scene3d-gltf.js");
  const api = env.context.__gosx_scene3d_api;
  const state = api.createSceneState({ scene: { instancedGLBMeshes: [
    { id: "swarm", src: "/actor.glb", customUniforms: { pulse: [0.25, 0.75] }, instances: [{ id: "one" }] },
  ] } });
  await env.context.__meshTest.hydrate(state, null);
  const object = Array.from(state.objects.values())[0];
  assert.equal(object._rigidMaterialProfileStable, true);
  const comparisons = env.context.__meshTest.countMaterialInputComparisons();
  const initial = env.context.__meshTest.materialProfile(object);
  assert.equal(env.context.__meshTest.materialProfile(object), initial);
  assert.equal(comparisons(), 0, "engine-owned rigid wrappers must not rescan nested material inputs");

  const uniforms = env.context.__meshTest.resolveUniforms(state, object.id);
  uniforms.pulse[0] = 0.9;
  assert.equal(object._rigidMaterialProfileStable, false, "live inline uniforms permanently invalidate trust");
  const animated = env.context.__meshTest.materialProfile(object);
  assert.notEqual(animated, initial);
  assert.equal(animated.customUniforms.pulse[0], 0.9);
  assert.ok(comparisons() > 0, "mutable wrappers retain the recursive invalidation contract");

  await api.applySceneCommands(state, [{ kind: 3, objectId: object.id, data: { color: "#123456" } }]);
  const patched = state.objects.get(object.id);
  assert.notEqual(patched, object);
  assert.equal(patched._rigidMaterialProfileStable, false);
  assert.equal(env.context.__meshTest.materialProfile(patched).color, "#123456");
});

test("incremental additions retain the normalized nested batch snapshot across await", async () => {
  const env = importedMeshRuntime({ fetchRoutes: { "/actor.glb": { bytes: buildMinimalGLBBytes() } } });
  runScript(freshFeatureBundleSource("scene3d-gltf"), env.context, "bootstrap-feature-scene3d-gltf.js");
  const api = env.context.__gosx_scene3d_api;
  const initial = { id: "swarm", src: "/actor.glb", customUniforms: { pulse: [0.25, 0.75] }, instances: [{ id: "one" }] };
  const state = api.createSceneState({ scene: { instancedGLBMeshes: [initial] } });
  await env.context.__meshTest.hydrate(state, null);
  const next = { id: "swarm", src: "/actor.glb", customUniforms: { pulse: [0.25, 0.75] }, instances: [{ id: "one" }, { id: "two" }] };
  const pending = api.applySceneCommands(state, [{ kind: 11, data: { instancedGLBMeshes: [next] } }]);
  next.customUniforms.pulse[0] = 9;
  await pending;
  const added = Array.from(state.objects.values()).find(o => o.id.startsWith("swarm/two/"));
  assert.equal(added.customUniforms.pulse[0], 0.25,
    "async staging must observe the once-normalized command snapshot, not later authored mutation");
});

test("mixed ordinary and instanced membership snapshots do not alias later matrix caches", async () => {
  const env = importedMeshRuntime({ fetchRoutes: { "/actor.glb": { bytes: buildMinimalGLBBytes() } } });
  runScript(freshFeatureBundleSource("scene3d-gltf"), env.context, "bootstrap-feature-scene3d-gltf.js");
  const api = env.context.__gosx_scene3d_api;
  const state = api.createSceneState({ scene: {
    models: [{ id: "ordinary", src: "/actor.glb", x: 1 }],
    instancedGLBMeshes: [{ id: "effects", src: "/actor.glb", instances: [{ id: "one" }] }],
  } });
  await env.context.__meshTest.hydrate(state, null);
  const ordinary = Array.from(state.objects.values()).find(object => object.id.startsWith("ordinary/"));
  state.models[0].x = 3;
  await api.applySceneCommands(state, [{ kind: 11, data: { instancedGLBMeshes: [
    { id: "effects", src: "/actor.glb", instances: [{ id: "one" }, { id: "two" }] },
  ] } }]);
  const committedMatrix = ordinary.parentMatrix;
  assert.equal(committedMatrix[12], 3);
  state.models[0].x = 9;
  await api.applySceneCommands(state, [{ kind: 11, data: { instancedGLBMeshes: [
    { id: "effects", src: "/actor.glb", instances: [{ id: "one" }, { id: "three" }] },
  ] } }]);
  assert.equal(committedMatrix[12], 3,
    "a persistent ordinary Model matrix cache must not mutate the prior committed snapshot");
  assert.equal(ordinary.parentMatrix[12], 9);
});

test("indexed rigid survivors fall back when a pose becomes mirrored", async () => {
  const makeState = async () => {
    const env = importedMeshRuntime({ fetchRoutes: { "/actor.glb": { bytes: buildMinimalGLBBytes() } } });
    runScript(freshFeatureBundleSource("scene3d-gltf"), env.context, "bootstrap-feature-scene3d-gltf.js");
    const api = env.context.__gosx_scene3d_api;
    const state = api.createSceneState({ scene: { instancedGLBMeshes: [
      { id: "effects", src: "/actor.glb", instances: [{ id: "one", scaleX: 1 }] },
    ] } });
    await env.context.__meshTest.hydrate(state, null);
    return { env, api, state };
  };
  for (const countChanges of [false, true]) {
    const { api, state } = await makeState();
    const generation = state._modelHydrationGeneration;
    const prior = Array.from(state.objects.values())[0];
    const instances = [{ id: "one", scaleX: -1 }];
    if (countChanges) instances.push({ id: "two" });
    await api.applySceneCommands(state, [{ kind: 11, data: { instancedGLBMeshes: [
      { id: "effects", src: "/actor.glb", instances },
    ] } }]);
    assert.ok(state._modelHydrationGeneration > generation,
      `${countChanges ? "count-changing" : "pose-only"} reflection must use full hydration`);
    assert.notEqual(Array.from(state.objects.values())[0], prior,
      "the prior positive-determinant wrapper cannot be reused with mirrored winding");
  }
});

test("832 moving rigid wrappers retain constant-time material profile cache hits", async (t) => {
  const env = importedMeshRuntime({ fetchRoutes: { "/actor.glb": { bytes: buildMinimalGLBBytes() } } });
  runScript(freshFeatureBundleSource("scene3d-gltf"), env.context, "bootstrap-feature-scene3d-gltf.js");
  const api = env.context.__gosx_scene3d_api;
  const instances = Array.from({ length: 832 }, (_, index) => ({ id: "slot-" + index, x: index / 100 }));
  const state = api.createSceneState({ scene: { instancedGLBMeshes: [
    { id: "effects", src: "/actor.glb", customUniforms: { pulse: [0.25, 0.75] }, instances },
  ] } });
  await env.context.__meshTest.hydrate(state, null);
  const objects = Array.from(state.objects.values());
  assert.equal(objects.length, 832);
  for (const object of objects) env.context.__meshTest.materialProfile(object);
  const comparisons = env.context.__meshTest.countMaterialInputComparisons();
  let started = process.hrtime.bigint();
  for (let frame = 0; frame < 60; frame++) {
    for (const object of objects) {
      object.parentMatrix[12] += 0.000001;
      env.context.__meshTest.materialProfile(object);
    }
  }
  const stableMS = Number(process.hrtime.bigint() - started) / 1e6;
  assert.equal(comparisons(), 0, "pose-only frames must perform no nested material comparisons");

  for (const object of objects) object._rigidMaterialProfileStable = false;
  started = process.hrtime.bigint();
  for (let frame = 0; frame < 60; frame++) {
    for (const object of objects) env.context.__meshTest.materialProfile(object);
  }
  const mutableMS = Number(process.hrtime.bigint() - started) / 1e6;
  assert.ok(comparisons() >= 832 * 60, "mutable wrappers must retain recursive comparison coverage");
  t.diagnostic(`49,920 material lookups: rigid=${stableMS.toFixed(2)}ms mutable=${mutableMS.toFixed(2)}ms`);
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

  state._modelOwner = () => true;
  const accepted = await api.applySceneCommands(state, [{ kind: 11, data: { instancedGLBMeshes: [
    batch([{ id: "one", x: 13 }, { id: "two", x: 2 }]),
  ] } }]);
  assert.equal(accepted[0].committed, true);
  const committedMatrix = one.parentMatrix;
  assert.notEqual(committedMatrix, matrix, "a successful transaction owns a fresh matrix snapshot");
  await api.applySceneCommands(state, [{ kind: 11, data: { instancedGLBMeshes: [
    batch([{ id: "one", x: 17 }, { id: "two", x: 3 }]),
  ] } }]);
  assert.equal(committedMatrix[12], 13, "a later command must not mutate a previously committed matrix snapshot");
  assert.equal(one.parentMatrix[12], 17);
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
  const membershipWork = env.context.__meshTest.countMembershipWork();
  const generation = state._modelHydrationGeneration;
  const anchor = Array.from(state.objects.values()).find(o => o.id.startsWith("effects/anchor/"));
  const vertices = anchor.vertices;
  for (let wave = 1; wave <= 100; wave += 1) {
    await api.applySceneCommands(state, command([{ id: "anchor", x: wave / 10 }, { id: "slot-" + wave, x: wave }]));
    assert.equal(state.objects.get(anchor.id), anchor);
    assert.equal(state.objects.size, 2);
    assert.equal(state._hydratedModelRecords.rigidInstances.size, 2);
    assert.equal(state._hydratedModelRecords.rigidInstancesByID.size, 2,
      "the logical-ID index contains only the current live membership");
    assert.equal(state._hydratedModelRecords.objects.length, 2);
  }
  assert.equal(state._modelHydrationGeneration, generation, "bounded churn must not create full hydration generations");
  assert.equal(stages(), 100, "each churn step stages only its one new member, never every survivor");
  const work = membershipWork();
  assert.ok(work.keys > 0);
  assert.equal(work.keyIDs.some(id => id === "effects/anchor"), false,
    "bounded churn generates hydration identity only for admitted members, not the survivor");
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
