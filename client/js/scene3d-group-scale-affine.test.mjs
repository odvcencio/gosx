import test from "node:test";
import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import vm from "node:vm";
import { createRequire } from "node:module";
import { fileURLToPath } from "node:url";
import rendererSourceSet from "./scene3d-renderer-source-set.js";

const { readSceneRendererBackendSrc } = rendererSourceSet;

const ts = createRequire(new URL("../runtime/package.json", import.meta.url))("typescript");

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const srcDir = path.join(__dirname, "bootstrap-src");
const runtimeDir = path.join(__dirname, "..", "runtime", "scene3d");

function readSource(name) {
  return fs.readFileSync(path.join(srcDir, name), "utf8");
}

function readRuntime(name) {
  return fs.readFileSync(path.join(runtimeDir, name), "utf8");
}

// Match the production TypeScript syntax before evaluating the real loader.
const gltfSource = ts.transpileModule(readRuntime("gltf.ts"), {
  compilerOptions: { target: ts.ScriptTarget.ES2022 },
}).outputText;

function trimBeforeSharedAPI(source) {
  const marker = source.indexOf("// Scene3D shared API");
  assert.ok(marker >= 0);
  return source.slice(0, marker);
}

function createCoreContext({ mount = false } = {}) {
  const sandbox = {
    __gosx_scene3d_api: {},
    console: { warn() {}, error() {}, log() {} },
    Math, JSON, Number, Object, Array, Map, Set, WeakMap,
    Float32Array, Float64Array, Uint8Array, Uint16Array, Uint32Array,
    Int8Array, Int16Array, ArrayBuffer, DataView, TextDecoder, Error,
    String, Boolean, isFinite,
  };
  sandbox.window = sandbox;
  sandbox.globalThis = sandbox;
  const context = vm.createContext(sandbox);
  for (const name of [
    "10-runtime-primitives.ts",
    "10-runtime-scene-utils.ts",
    "11-scene-math.ts",
    "12-scene-geometry.ts",
    "13-scene-material.ts",
    "14-scene-lighting.ts",
    "15-scene-draw-plan.ts",
  ]) {
    vm.runInContext(readSource(name), context, { filename: name });
  }
  vm.runInContext(trimBeforeSharedAPI(readSource("10-runtime-scene-core.ts")), context, { filename: "10-runtime-scene-core.ts" });
  vm.runInContext("Object.assign(window.__gosx_scene3d_api,{sceneNormalizeParentMatrix,sceneNormalizeDirection});",context);
  vm.runInContext(ts.transpileModule(readRuntime("hierarchy.ts"), {compilerOptions:{target:ts.ScriptTarget.ES2022}}).outputText,context,{filename:"hierarchy.ts"});
  vm.runInContext("function sceneGroupTestOrigin(object,time) {const result={};translateScenePointInto(result,0,0,0,object,time);return result;}",context);
  if (mount) {
    vm.runInContext(ts.transpileModule(readRuntime("mount-webgl.ts"), { compilerOptions: { target: ts.ScriptTarget.ES2022 } }).outputText, context, { filename: "mount-webgl.ts" });
  }
  return context;
}

function runJSON(context, expression) {
  return JSON.parse(JSON.stringify(vm.runInContext(expression, context)));
}

test("parent matrices are exact finite clones and explicit null resets fallback state", () => {
  const context = createCoreContext();
  const result = runJSON(context, `(() => {
    const raw = [2,0,0,0, 1,3,0,0, 0,0,-4,0, 10,20,30,1];
    const matrix = sceneNormalizeParentMatrix(raw, null);
    raw[0] = 99;
    return {
      matrix: Array.from(matrix),
      cloned: matrix[0] === 2,
      fallbackCloned: sceneNormalizeParentMatrix(undefined, matrix) !== matrix,
      reset: sceneNormalizeParentMatrix(null, matrix),
      short: sceneNormalizeParentMatrix(raw.slice(0, 15), null),
      stringValue: sceneNormalizeParentMatrix([2,0,0,0, 1,3,0,0, 0,0,-4,0, 10,20,30,"1"], null),
      nullValue: sceneNormalizeParentMatrix([2,0,0,0, 1,3,0,0, 0,0,-4,0, 10,20,30,null], null),
      projective: sceneNormalizeParentMatrix([2,0,0,1, 1,3,0,0, 0,0,-4,0, 10,20,30,1], null),
      singular: sceneNormalizeParentMatrix([1,0,0,0, 1,0,0,0, 0,0,1,0, 0,0,0,1], null),
      small: sceneNormalizeParentMatrix([1e-6,0,0,0, 1e-6,2e-6,0,0, 0,0,-3e-6,0, 0,0,0,1], null),
    };
  })()`);
  assert.equal(result.cloned, true);
  assert.equal(result.fallbackCloned, true);
  assert.equal(result.matrix[0], 2);
  assert.equal(result.reset, null);
  assert.equal(result.short, null);
  assert.equal(result.stringValue, null);
  assert.equal(result.nullValue, null);
  assert.equal(result.projective, null);
  assert.equal(result.singular, null);
  assert.equal(result.small.length, 16);
});

test("browser mesh transform keeps live leaf motion inside the affine parent", () => {
  const context = createCoreContext();
  const result = runJSON(context, `(() => {
    const object = {
      x:1, y:2, z:3, rotationX:0, rotationY:0, rotationZ:0,
      scaleX:2, scaleY:1, scaleZ:1, spinX:0, spinY:0, spinZ:Math.PI/2,
      shiftX:2, shiftY:0, shiftZ:0, driftSpeed:0, driftPhase:0,
      parentMatrix:new Float32Array([2,0,0,0, 0,3,0,0, 0,0,4,0, 10,20,30,1]),
    };
    const point = {x:0,y:0,z:0};
    translateScenePointInto(point, 1, 0, 0, object, 1);
    return { point, matrix:Array.from(sceneObjectModelMatrix(object, 1)) };
  })()`);
  assert.deepEqual(result.point, { x: 16, y: 32, z: 42 });
  assert.deepEqual(result.matrix, [
    0, 6, 0, 0,
    -2, 0, 0, 0,
    0, 0, 4, 0,
    16, 26, 42, 1,
  ]);
});

test("set-transform changes and resets parent matrices without a stale model cache", () => {
  const context = createCoreContext();
  const result = runJSON(context, `(() => {
    const state = {objects:new Map(), labels:new Map(), sprites:new Map(), html:new Map()};
    const object = normalizeSceneObject({id:"box",kind:"box",x:1,y:2,z:3}, 0, null);
    state.objects.set("box", object);
    const first = [2,0,0,0, 0,3,0,0, 0,0,4,0, 10,20,30,1];
    applySceneObjectPatch(state, "box", {parentMatrix:first});
    const added = state.objects.get("box");
    const addedOrigin = Array.from(sceneObjectModelMatrix(added, 0).slice(12, 15));
    first[12] = 999;
    applySceneObjectPatch(state, "box", {parentMatrix:[2,0,0,0, 1,3,0,0, 0,0,4,0, -5,-9,-15,1]});
    const changed = Array.from(sceneObjectModelMatrix(state.objects.get("box"), 0).slice(12, 15));
    applySceneObjectPatch(state, "box", {parentMatrix:null});
    const reset = state.objects.get("box");
    return {addedOrigin, changed, resetMatrix:reset.parentMatrix, resetOrigin:Array.from(sceneObjectModelMatrix(reset, 0).slice(12, 15))};
  })()`);
  assert.deepEqual(result.addedOrigin, [12, 26, 42]);
  assert.deepEqual(result.changed, [-1, -3, -3]);
  assert.equal(result.resetMatrix, null);
  assert.deepEqual(result.resetOrigin, [1, 2, 3]);
});

test("set-transform moves a hydrated Model root while retaining primitive materials and geometry", async () => {
  const context = createCoreContext({ mount: true });
  const result = await vm.runInContext(`(async () => {
    let loads = 0;
    gosxSceneEmit = function() {};
    const vertices = {count:3, immutable:true, revision:0,
      positions:new Float32Array([0,0,0, 1,0,0, 0,1,0]),
      normals:new Float32Array([0,0,1, 0,0,1, 0,0,1])};
    loadSceneModelAsset = async function() {
      loads++;
      return {objects:[{id:'metal',kind:'mesh',color:'#123456',vertices},
        {id:'barrel',kind:'mesh',color:'#abcdef',vertices}],
        skins:[],nodes:[],animations:[],points:[],labels:[],sprites:[],html:[],lights:[]};
    };
    const state = createSceneState({models:[{id:'turret',src:'head.glb',x:12,y:1.16,z:-8}]}, null);
    const hydrated = await hydrateSceneStateModels(state, null);
    const before = Array.from(state.objects.values());
    const geometry = before.map(o => o.vertices);
    const transforms = [];
    for (const rotationY of [Math.PI/2, Math.PI, Math.PI/2]) {
      await applySceneCommands(state, [{kind:SCENE_CMD_SET_TRANSFORM,objectId:'turret',
        data:{x:12,y:1.16,z:-8,rotationY,scaleX:1,scaleY:1,scaleZ:1}}]);
      transforms.push(Array.from(sceneObjectModelMatrix(state.objects.get('turret/metal'),0)));
    }
    const parent = [1,0,0,0, 0,1,0,0, 0,0,1,0, 2,3,4,1];
    await applySceneCommands(state,[{kind:SCENE_CMD_SET_TRANSFORM,objectId:'turret',data:{parentMatrix:parent}}]);
    const parentOrigin = Array.from(sceneObjectModelMatrix(before[0],0).slice(12,15));
    await applySceneCommands(state,[{kind:SCENE_CMD_SET_TRANSFORM,objectId:'turret',data:{parentMatrix:null}}]);
    return {committed:hydrated.committed,loads,transforms,parentOrigin,
      resetOrigin:Array.from(sceneObjectModelMatrix(before[0],0).slice(12,15)),
      sameObjects:before.every(o => state.objects.get(o.id)===o),
      sameGeometry:before.every((o,i) => o.vertices===geometry[i]),
      colors:before.map(o=>o.color),ids:before.map(o=>o.id),
      animations:state._modelAnimations.length,skins:state._modelSkins.length,
      generation:state._modelHydrationGeneration};
  })()`, context);
  const actual = JSON.parse(JSON.stringify(result));
  assert.equal(actual.committed, true);
  assert.equal(actual.loads, 1);
  assert.equal(actual.generation, 1);
  assert.equal(actual.sameObjects, true);
  assert.equal(actual.sameGeometry, true);
  assert.deepEqual(actual.ids, ["turret/metal", "turret/barrel"]);
  assert.deepEqual(actual.colors, ["#123456", "#abcdef"]);
  assert.equal(actual.animations, 0);
  assert.equal(actual.skins, 0);
  assert.ok(Math.abs(actual.transforms[0][0]) < 1e-6);
  assert.ok(Math.abs(actual.transforms[0][2] + 1) < 1e-6);
  assert.ok(Math.abs(actual.transforms[1][0] + 1) < 1e-6);
  assert.deepEqual(actual.transforms[0], actual.transforms[2]);
  assert.deepEqual(actual.parentOrigin.map(Math.round), [14,4,-4]);
  assert.deepEqual(actual.resetOrigin.map(Math.round), [12,1,-8]);
});

test("Model root transform commands preserve fit and supersede pending hydration", async () => {
  const context = createCoreContext({ mount: true });
  const result = await vm.runInContext(`(async () => {
    let loads = 0, release;
    const pending = new Promise(resolve => { release=resolve; });
    gosxSceneEmit = function() {};
    loadSceneModelAsset = async function() {
      if (++loads===1) await pending;
      return {objects:[{id:'mesh',kind:'mesh',vertices:{count:3,immutable:true,revision:0,
        positions:new Float32Array([0,0,0, 1,0,0, 0,1,0])}}],
        bounds:{minX:0,minY:0,minZ:0,maxX:1,maxY:1,maxZ:0},
        skins:[],nodes:[],animations:[],points:[],labels:[],sprites:[],html:[],lights:[]};
    };
    const state=createSceneState({models:[{id:'model',src:'mesh.glb',fit:'contain',fitAlign:'none',bounds:2}]},null);
    const old=hydrateSceneStateModels(state,null);
    await applySceneCommands(state,[{kind:SCENE_CMD_SET_TRANSFORM,objectId:'model',data:{x:5,y:1.16,rotationY:Math.PI/2}}]);
    release();
    const stale=await old;
    const geometry=state.objects.get('model/mesh').vertices;
    const loadedMatrix=Array.from(sceneObjectModelMatrix(state.objects.get('model/mesh'),0));
    await applySceneCommands(state,[{kind:SCENE_CMD_SET_TRANSFORM,objectId:'model',data:{x:6,rotationY:Math.PI/2}}]);
    const object=state.objects.get('model/mesh');
    return {stale:stale.stale,loads,generation:state._modelHydrationGeneration,
      sameGeometry:geometry===object.vertices,loadedMatrix,matrix:Array.from(sceneObjectModelMatrix(object,0))};
  })()`, context);
  assert.equal(result.stale, true);
  assert.equal(result.loads, 2);
  assert.equal(result.generation, 2);
  assert.equal(result.sameGeometry, true);
  assert.equal(result.loadedMatrix[12], 5);
  assert.ok(Math.abs(result.loadedMatrix[13] - 1.16) < 1e-6);
  assert.ok(Math.abs(result.loadedMatrix[2] + 2) < 1e-6);
  assert.equal(result.matrix[12], 6);
  assert.ok(Math.abs(result.matrix[2] + 2) < 1e-6);
});

test("model roots compose the same parent matrix and use inverse-transpose normals", () => {
  const context = createCoreContext({ mount: true });
  const result = runJSON(context, `(() => {
    const model = {
      x:1, y:2, z:3, rotationX:0, rotationY:0, rotationZ:Math.PI/2,
      scaleX:2, scaleY:1, scaleZ:1,
      parentMatrix:new Float32Array([2,0,0,0, 1,3,0,0, 0,0,-4,0, 10,20,30,1]),
    };
    const normalModel = {
      x:0,y:0,z:0,rotationX:0,rotationY:0,rotationZ:0,scaleX:1,scaleY:1,scaleZ:1,
      parentMatrix:model.parentMatrix,
    };
    const normal = sceneNormalizeDirection(sceneObjectTransformNormal(normalModel, {x:1,y:0,z:0}, 0));
    return {
      matrix:Array.from(sceneModelTransformMatrix(model)),
      point:sceneModelTransform({x:1,y:0,z:0}, model, 0),
      normal,
    };
  })()`);
  assert.deepEqual(result.matrix, [
    2, 6, 0, 0,
    -2, 0, 0, 0,
    0, 0, -4, 0,
    14, 26, 18, 1,
  ]);
  assert.deepEqual(result.point, { x: 16, y: 32, z: 18 });
  assert.ok(Math.abs(result.normal.x - 3 / Math.sqrt(10)) < 1e-7);
  assert.ok(Math.abs(result.normal.y + 1 / Math.sqrt(10)) < 1e-7);
  assert.ok(Math.abs(result.normal.z) < 1e-7);
});

test("strict browser schema validates every parent matrix carrier", () => {
  const sandbox = { globalThis: null, window: null, Object, Array, Number, String, Uint8Array, isFinite };
  sandbox.globalThis = sandbox;
  sandbox.window = sandbox;
  const context = vm.createContext(sandbox);
  vm.runInContext(readSource("15-scene-ir-schema-strict.ts"), context, { filename: "15-scene-ir-schema-strict.ts" });
  const families = [
    `({objects:[{id:"object",kind:"box",parentMatrix:__matrix}]})`,
    `({models:[{id:"model",kind:"box",src:"model.glb",parentMatrix:__matrix}]})`,
    `({points:[{id:"points",count:0,parentMatrix:__matrix}]})`,
    `({instancedGLBMeshes:[{id:"batch",src:"model.glb",instances:[{id:"instance",parentMatrix:__matrix}]}]})`,
  ];
  for (const family of families) {
    for (const valid of [
      `[1e150,0,0,0,0,1e150,0,0,0,0,1e150,0,0,0,0,1]`,
      `[-2e-150,0,0,0,1e-150,3e-150,0,0,0,0,4e-150,0,5,6,7,1]`,
      `[9e307,-9e307,0,0,9e307,9e307,0,0,0,0,9e307,0,0,0,0,1]`,
      `[1e-297,0,0,0,0,2e-303,0,0,0,0,2e-303,0,0,0,0,1]`,
      `[1e-297,0,0,0,5e-298,2e-303,0,0,-4e-298,1e-303,2e-303,0,0,0,0,1]`,
      `[1e-297,5e-298,-4e-298,0,0,2e-303,1e-303,0,0,0,2e-303,0,0,0,0,1]`,
      `[-1e-297,0,0,0,5e-298,2e-303,0,0,-4e-298,1e-303,2e-303,0,0,0,0,1]`,
      `[1e-297,0,0,0,0,1e-303,0,0,0,0,1.000001e-303,0,0,0,0,1]`,
    ]) {
      context.__matrix = vm.runInContext(valid, context);
      const diagnostics = vm.runInContext(`__gosx_validate_scene_ir_strict(${family}, {strict:true}).diagnostics`, context);
      assert.ok(!diagnostics.some((item) => item.code === "scene.transform.invalid_parent_matrix"), `${family} rejected ${valid}`);
    }
    for (const invalid of [
      `null`,
      `[1,0,0,0,0,1,0,0,0,0,1,0,0,0,0]`,
      `[1,0,0,0,0,1,0,0,0,0,1,0,0,0,0,"1"]`,
      `[1,0,0,0,0,1,0,0,0,0,1,0,0,0,0,null]`,
      `[1,0,0,0,0,1,0,0,0,0,1,0,0,0,0,NaN]`,
      `[1,0,0,1,0,1,0,0,0,0,1,0,0,0,0,1]`,
      `[1,0,0,0,1,0,0,0,0,0,1,0,0,0,0,1]`,
      `[1e-297,0,0,0,0,1e-303,0,0,0,0,0.999999e-303,0,0,0,0,1]`,
      `[1e-308,0,0,0,0,1e-308,0,0,0,0,2e-320,0,0,0,0,1]`,
    ]) {
      context.__matrix = vm.runInContext(invalid, context);
      const diagnostics = vm.runInContext(`__gosx_validate_scene_ir_strict(${family}, {strict:true}).diagnostics`, context);
      assert.ok(diagnostics.some((item) => item.code === "scene.transform.invalid_parent_matrix"), `${family} accepted ${invalid}`);
    }
  }
});

test("shared affine determinant and inverse agree across extreme anisotropy", () => {
  const context = createCoreContext();
  const result = runJSON(context, `(() => {
    function inspect(matrix) {
      const inverse = new Float64Array(12);
      return {
        without:sceneAffineDeterminant(matrix,0),
        withOut:sceneAffineDeterminant(matrix,0,inverse),
        inverse:Array.from(inverse),
        normalized:!!sceneNormalizeParentMatrix(matrix,null),
      };
    }
    return {
      valid:{
        tiny:inspect([1e-297,0,0,0, 0,2e-303,0,0, 0,0,2e-303,0, 0,0,0,1]),
        shear:inspect([1e-297,0,0,0, 5e-298,2e-303,0,0, -4e-298,1e-303,2e-303,0, 0,0,0,1]),
        transposed:inspect([1e-297,5e-298,-4e-298,0, 0,2e-303,1e-303,0, 0,0,2e-303,0, 0,0,0,1]),
        reflected:inspect([-1e-297,0,0,0, 5e-298,2e-303,0,0, -4e-298,1e-303,2e-303,0, 0,0,0,1]),
        threshold:inspect([1e-297,0,0,0, 0,1e-303,0,0, 0,0,1.000001e-303,0, 0,0,0,1]),
        nearMax:inspect([9e307,-9e307,0,0, 9e307,9e307,0,0, 0,0,9e307,0, 0,0,0,1]),
      },
      invalid:{
        threshold:inspect([1e-297,0,0,0, 0,1e-303,0,0, 0,0,0.999999e-303,0, 0,0,0,1]),
        overflow:inspect([1e-308,0,0,0, 0,1e-308,0,0, 0,0,2e-320,0, 0,0,0,1]),
        singular:inspect([1,0,0,0, 1,0,0,0, 0,0,1,0, 0,0,0,1]),
      },
    };
  })()`);
  for (const [name, value] of Object.entries(result.valid)) {
    assert.notEqual(value.without, 0, `${name} rejected without inverse output`);
    assert.equal(value.withOut, value.without, `${name} output changed determinant acceptance`);
    assert.equal(value.normalized, true, `${name} normalization rejected valid matrix`);
    assert.ok(value.inverse.every(Number.isFinite), `${name} inverse is non-finite`);
    assert.ok(value.inverse.some((entry) => entry !== 0), `${name} inverse collapsed to zero`);
  }
  assert.equal(result.valid.tiny.without, 4e-12);
  assert.ok(Math.abs(result.valid.tiny.inverse[0] / 1e297 - 1) < 1e-15);
  assert.ok(Math.abs(result.valid.tiny.inverse[5] / 5e302 - 1) < 1e-15);
  assert.ok(Math.abs(result.valid.tiny.inverse[10] / 5e302 - 1) < 1e-15);
  assert.equal(result.valid.reflected.without, -4e-12);
  assert.equal(result.valid.nearMax.without, 2);
  assert.ok(result.valid.nearMax.inverse[0] > 0 && result.valid.nearMax.inverse[0] < 1e-307);
  for (const [name, value] of Object.entries(result.invalid)) {
    assert.equal(value.without, 0, `${name} accepted without inverse output`);
    assert.equal(value.withOut, 0, `${name} accepted with inverse output`);
    assert.equal(value.normalized, false, `${name} normalization accepted invalid matrix`);
  }
});

test("instanced CPU picking inverts a sheared basis exactly", () => {
  const context = createCoreContext();
  vm.runInContext(readSource("17-scene-input.ts"), context, { filename: "17-scene-input.ts" });
  const result = runJSON(context, `(() => {
    const s = Math.SQRT1_2;
    const transforms = new Float64Array([2*s,s,0,0, -2*s,s,0,0, 0,0,1,0, 0,0,0,1]);
    const mesh = {id:"sheared",kind:"sphere",radius:1,count:1,pickable:true,transforms};
    const hit = sceneRaycastPickInstancedMeshes({origin:{x:1.3,y:0.7,z:3},dir:{x:0,y:0,z:-1}}, [mesh], 0);
    const miss = sceneRaycastPickInstancedMeshes({origin:{x:2.2,y:0,z:3},dir:{x:0,y:0,z:-1}}, [mesh], 0);
    return {hit:!!hit, miss:!!miss, point:hit && hit.point};
  })()`);
  assert.equal(result.hit, true);
  assert.equal(result.miss, false);
  assert.ok(Math.abs(result.point.x - 1.3) < 1e-12);
  assert.ok(Math.abs(result.point.y - 0.7) < 1e-12);
});

test("instanced CPU picking preserves world distance across large and small affine scales", () => {
  const context = createCoreContext();
  vm.runInContext(readSource("17-scene-input.ts"), context, { filename: "17-scene-input.ts" });
  const result = runJSON(context, `(() => {
    function pick(matrix, origin, dir) {
      const mesh = {id:"scaled",kind:"sphere",radius:1,count:1,pickable:true,transforms:new Float32Array(matrix)};
      const hit = sceneRaycastPickInstancedMeshes({origin,dir},[mesh],0);
      return hit && {distance:hit.distance,local:hit.localPosition};
    }
    function sheared(scale) {
      const n = Math.SQRT1_2, length = scale * Math.sqrt(5);
      const step = {x:-scale*n,y:3*scale*n,z:0};
      return {expected:length,hit:pick(
        [-2*scale,0,0,0, scale,3*scale,0,0, 0,0,4*scale,0, 0,0,0,1],
        {x:2*step.x,y:2*step.y,z:0},
        {x:-step.x/length,y:-step.y/length,z:0})};
    }
    return {
      uniformCutoff:pick([1e6,0,0,0,0,1e6,0,0,0,0,1e6,0,0,0,0,1],{x:0,y:0,z:2e6},{x:0,y:0,z:-1}),
      uniformLarge:pick([1e9,0,0,0,0,1e9,0,0,0,0,1e9,0,0,0,0,1],{x:0,y:0,z:2e9},{x:0,y:0,z:-1}),
      uniformSmall:pick([1e-9,0,0,0,0,1e-9,0,0,0,0,1e-9,0,0,0,0,1],{x:0,y:0,z:2e-9},{x:0,y:0,z:-1}),
      shearedLarge:sheared(1e9),
      shearedSmall:sheared(1e-9),
    };
  })()`);
  function close(actual, expected, relative = 1e-6) {
    assert.ok(Math.abs(actual - expected) <= Math.abs(expected) * relative,
      `${actual} is not within ${relative} of ${expected}`);
  }
  assert.ok(result.uniformCutoff);
  close(result.uniformCutoff.distance, 1e6, 1e-12);
  close(result.uniformCutoff.local.z, 1, 1e-12);
  assert.ok(result.uniformLarge);
  close(result.uniformLarge.distance, 1e9, 1e-12);
  close(result.uniformLarge.local.z, 1, 1e-12);
  assert.ok(result.uniformSmall);
  close(result.uniformSmall.distance, 1e-9);
  close(result.uniformSmall.local.z, 1);
  for (const value of [result.shearedLarge, result.shearedSmall]) {
    assert.ok(value.hit);
    close(value.hit.distance, value.expected);
    close(value.hit.local.x, Math.SQRT1_2);
    close(value.hit.local.y, Math.SQRT1_2);
  }
});

test("WebGL2 and WebGPU Points upload parent times live-local through the shared multiply", () => {
  const webgl = readSceneRendererBackendSrc("webgl");
  const webgpu = readSceneRendererBackendSrc("webgpu");
  assert.match(webgl, /sceneMat4MultiplyInto\(_pointsTilt, entry\.parentMatrix, _pointsModelMat\)/);
  assert.match(webgl, /uniformMatrix4fv\(pp\.uniforms\.modelMatrix, false, entry\.parentMatrix \? _pointsTilt : _pointsModelMat\)/);
  assert.match(webgpu, /sceneMat4MultiplyInto\(_pointsTilt, entry\.parentMatrix, _pointsModelMat\)/);
  assert.match(webgpu, /puF\.set\(pointsModelMat, 0\)/);
});

test("shared affine normal matrix is scale-stable and singular-neutral", () => {
  const context = createCoreContext();
  const result = runJSON(context, `(() => {
    const tiny = [2e-6,0,0,0, 1e-6,3e-6,0,0, 0,0,-4e-6,0, 5,6,7,1];
    const normalMatrix = sceneAffineNormalMatrix(tiny);
    const x = normalMatrix[0], y = normalMatrix[1], z = normalMatrix[2];
    const length = Math.hypot(x,y,z);
    return {
      normal:[x/length,y/length,z/length],
      singular:sceneAffineNormalMatrix([1,0,0,0, 1,0,0,0, 0,0,1,0, 0,0,0,1]),
    };
  })()`);
  assert.ok(Math.abs(result.normal[0] - 3 / Math.sqrt(10)) < 1e-12);
  assert.ok(Math.abs(result.normal[1] + 1 / Math.sqrt(10)) < 1e-12);
  assert.ok(Math.abs(result.normal[2]) < 1e-12);
  assert.deepEqual(result.singular, [1, 0, 0, 0, 1, 0, 0, 0, 1]);
});

test("browser direct, instanced, skinned and morph shaders share affine normal semantics", () => {
  const webgl = readSceneRendererBackendSrc("webgl");
  const webgpu = readSceneRendererBackendSrc("webgpu");
  function assertContract(gl, gpu, uniforms) {
    assert.match(gl, /vec4 q=gosxAffineNormal\(m,a_normal\)/);
    assert.match(gl, /gosxAffineNormal\(m,gosxNormal\)/);
    assert.match(gl, /gosxAffineNormal\(mat3\(u_modelMatrix \* selenaSkinMatrix\), a_normal\)/);
    assert.match(gpu, /gosxAffineNormal\(objectModel, in\.normal\)/);
    assert.equal((gpu.match(/gosxAffineNormal\(model, in\.normal\)/g) || []).length, 1);
    assert.match(gpu, /WGSL_PBR_INSTANCED_CULL_VERTEX = WGSL_PBR_INSTANCED_VERTEX/);
    assert.match(gpu, /let an = gosxAffineNormal\(morph\.model, localNormal\)/);
    assert.match(gpu, /outTangents\[t \+ 3u\].*an\.w/);
    assert.match(uniforms, /sceneAffineNormalMatrix\(webGPUObjectModelMatrix\(owner\)\)/);
    assert.match(gl, /var reflectedDirect = directVertices && sceneAffineDeterminant\(obj\.modelMatrix, 0\) < 0;/);
    assert.match(gl, /if \(reflectedDirect\) gl\.frontFace\(gl\.CW\);[\s\S]*if \(reflectedDirect\) gl\.frontFace\(gl\.CCW\);/);
    assert.match(gpu, /function bindPBRPipeline\(reflected, material = Object.create\(null\)\)/);
    assert.match(gpu, /getPBRPipeline\(blendMode, objectDepthWrite, reflected \? "cw" : "ccw", hasDetail\)/);
    assert.doesNotMatch(gl, /normalize\(mat3\(u_modelMatrix\) \* \(mat3\(selenaSkinMatrix\) \* a_normal\)\)/);
    assert.doesNotMatch(gpu, /morph\.model \* vec4<f32>\(localNormal, 0\.0\)/);
  }
  assertContract(webgl, webgpu, webgpu);
  assert.throws(() => assertContract(webgl.replaceAll("gosxAffineNormal(m,a_normal)", "vec4(normalize(m*a_normal),1.0)"), webgpu, webgpu));
  assert.throws(() => assertContract(webgl, webgpu.replace("gosxAffineNormal(objectModel, in.normal)", "vec4f(normalize(in.normal),1.0)"), webgpu));
  assert.throws(() => assertContract(webgl.replaceAll("gl.frontFace(gl.CW)", "gl.frontFace(gl.CCW)"), webgpu, webgpu));
  assert.throws(() => assertContract(webgl, webgpu.replaceAll('reflected ? "cw" : "ccw"', '"ccw"'), webgpu));
});

test("glTF POINTS and LINES modes 0-3 retain exact affine through animation", () => {
  const context = createCoreContext({ mount: true });
  vm.runInContext(gltfSource, context, { filename: "gltf.ts" });
  const result = runJSON(context, `(() => {
    const buffer = new ArrayBuffer(80);
    new Float32Array(buffer).set([
      1,0,0, 2,0,0, 2,1,0, 1,1,0,
      0,1,
      2,0,0, 0,4,0,
    ]);
    const doc = {
      asset:{version:"2.0"}, buffers:[{byteLength:80}],
      bufferViews:[{buffer:0,byteOffset:0,byteLength:80}],
      accessors:[
        {bufferView:0,byteOffset:0,componentType:5126,count:4,type:"VEC3"},
        {bufferView:0,byteOffset:48,componentType:5126,count:2,type:"SCALAR"},
        {bufferView:0,byteOffset:56,componentType:5126,count:2,type:"VEC3"},
      ],
      meshes:[{name:"affine-modes",primitives:[0,1,2,3].map(mode=>({mode,attributes:{POSITION:0}}))}],
      nodes:[{mesh:0,translation:[2,0,0]}], scenes:[{nodes:[0]}],
      animations:[{samplers:[{input:1,output:2,interpolation:"LINEAR"}],channels:[{sampler:0,target:{node:0,path:"translation"}}]}],
    };
    const asset = gltfExtractScene(doc, buffer);
    const model = {x:0,y:0,z:0,rotationX:0,rotationY:0,rotationZ:0,scaleX:1,scaleY:1,scaleZ:1,
      parentMatrix:new Float64Array([-2,0,0,0, 1,3,0,0, 0,0,1,0, 10,20,30,1])};
    const point = sceneInstantiateModelPointsEntry(asset.points[0], model, "model", 0);
    const lines = asset.objects.map((object,index)=>sceneInstantiateModelObject(object,model,"model",index,null));
    const entries = [point._nodeAnimLive].concat(lines.map(object=>object._nodeAnimLive));
    const root = sceneModelTransformMatrix(model);
    entries.forEach(entry=>{entry.modelMatrix=root; entry.model=model;});
    function firstWorld(object, isPoints) {
      const value = isPoints
        ? {x:object.positions[0],y:object.positions[1],z:object.positions[2]}
        : object.points[0];
      const out = {};
      translateScenePointInto(out,value.x,value.y,value.z,object,0);
      return out;
    }
    const initial = [firstWorld(point,true)].concat(lines.map(object=>firstWorld(object,false)));
    const oldPointPositions = point.positions;
    const animated = sceneTRSToMat4([0,4,0],[0,0,0,1],[1,1,1]);
    gltfApplyNodeAnimPose(entries,new Map([[0,animated]]));
    return {
      ids:[point.id].concat(lines.map(object=>object.id)),
      matrices:[point.parentMatrix].concat(lines.map(object=>object.parentMatrix)).map(value=>Array.from(value)),
      initial,
      animated:[firstWorld(point,true)].concat(lines.map(object=>firstWorld(object,false))),
      uploadedFresh:oldPointPositions!==point.positions,
    };
  })()`);
  assert.deepEqual(result.ids, [
    "model/affine-modes-points-0",
    "model/affine-modes-lines-1",
    "model/affine-modes-lines-2",
    "model/affine-modes-lines-3",
  ]);
  const matrix = [-2,0,0,0, 1,3,0,0, 0,0,1,0, 10,20,30,1];
  for (const carried of result.matrices) assert.deepEqual(carried, matrix);
  for (const world of result.initial) assert.deepEqual(world, {x:4,y:20,z:30});
  for (const world of result.animated) assert.deepEqual(world, {x:12,y:32,z:30});
  assert.equal(result.uploadedFresh, true);
});

test("retained group spin carries off-center children around a shared scaled pivot", () => {
  const context = createCoreContext();
  const result = runJSON(context, `(() => {
    const state={groups:sceneNormalizeGroups([
      {id:"outer",x:5,scaleX:2,scaleY:3,scaleZ:4,spinZ:2},
      {id:"inner",parentNode:"outer",x:2,spinZ:-1,scaleY:2}
    ])};
    const source=normalizeSceneObject({id:"tip",kind:"box",x:1,parentNode:"inner"},0);
    const matrices=sceneGroupMatrices(state,Math.PI/4);
    const first=sceneGroupRecords([source],matrices,"matrix")[0];
    const position=sceneGroupTestOrigin(first,0);
    const parent=Array.from(first.parentMatrix);
    sceneGroupMatrices(state,0);
    const second=sceneGroupRecords([source],matrices,"matrix")[0];
    return {position,parent,source:{x:source.x,parent:source.parentMatrix,spin:source.spinZ},same:first===second,zero:sceneGroupTestOrigin(second,0)};
  })()`);
  assert.ok(Math.abs(result.position.x-(5+3/Math.SQRT2))<1e-10);
  assert.ok(Math.abs(result.position.y-(4+Math.SQRT2))<1e-10);
  assert.deepEqual(result.zero,{x:11,y:0,z:0});
  assert.equal(result.source.x,1);
  assert.equal(result.source.parent,null);
  assert.equal(result.source.spin,0);
  assert.equal(result.same,true,"retain the per-child preparation view");
});

test("group live commands update the boundary without replacing descendants", () => {
  const context=createCoreContext();
  const result=runJSON(context,`(() => {
    const state=createSceneState({scene:{groups:[{id:"rotor",spinZ:1}],objects:[{id:"tip",kind:"box",x:2,parentNode:"rotor"}]}},null);
    const before=state.objects.get("tip");
    applySceneCommands(state,[{kind:2,objectId:"rotor",data:{x:3,spinZ:0}}]);
    const group=state.groups[0], matrices=sceneGroupMatrices(state,10);
    const after=state.objects.get("tip");
    const point=sceneGroupTestOrigin(sceneGroupRecords([after],matrices,"matrix")[0],10);
    applySceneCommands(state,[{kind:15,data:{groups:[]}}]);
    return {same:before===after,group,point,cleared:state.groups.length};
  })()`);
  assert.equal(result.same,true);
  assert.deepEqual(result.point,{x:5,y:0,z:0});
  assert.equal(result.cleared,0);
});

test("group matrices reject invalid hierarchies and nonfinite clocks", () => {
  const context=createCoreContext();
  for (const expression of [
    'sceneNormalizeGroups([{id:"a"},{id:"a"}])',
    'sceneNormalizeGroups([{id:"a",spinX:Infinity}])',
    'sceneGroupMatrices({groups:sceneNormalizeGroups([{id:"a",parentNode:"b"},{id:"b",parentNode:"a"}])},0)',
    'sceneGroupMatrices({groups:sceneNormalizeGroups([{id:"a",parentNode:"missing"}])},0)',
    'sceneGroupMatrices({groups:sceneNormalizeGroups([{id:"a"}])},NaN)',
  ]) assert.throws(()=>vm.runInContext(expression,context));
});

test("group affine prefixes move instances, annotations and light directions", () => {
  const context=createCoreContext();
  const result=runJSON(context,`(() => {
    const state={groups:sceneNormalizeGroups([{id:"g",x:10,spinZ:Math.PI/2,scaleX:2,scaleY:3,scaleZ:4}])};
    const matrices=sceneGroupMatrices(state,1);
    const source={id:"i",parentNode:"g",transforms:new Float32Array([1,0,0,0,0,1,0,0,0,0,1,0,2,0,0,1])};
    const instances=sceneGroupRecords([source],matrices,"instances")[0];
    const light=sceneGroupRecords([{parentNode:"g",x:2,y:0,z:0,directionX:1,directionY:0,directionZ:0}],matrices,"light")[0];
    const label=sceneLabelPoint(sceneGroupRecords([{parentNode:"g",x:2,y:0,z:0}],matrices,"position")[0],1);
    return {instance:Array.from(instances.transforms),original:Array.from(source.transforms),light,label};
  })()`);
  assert.ok(Math.abs(result.instance[12]-10)<1e-6);
  assert.ok(Math.abs(result.instance[13]-4)<1e-6);
  assert.equal(result.original[12],2);
  assert.ok(Math.abs(result.label.x-10)<1e-10);
  assert.ok(Math.abs(result.label.y-4)<1e-10);
  assert.ok(Math.abs(result.light.directionY-1)<1e-10);
});

test("a group motion binding owns the parent pose without copying spin to children", () => {
  const context=createCoreContext();
  const result=runJSON(context,`(() => {
    const bindings=new Map([["rotation",{binding:{target:"sceneNode",node:"rotor",property:"rotation.z"},value:Math.PI/2}]]);
    const state={groups:sceneNormalizeGroups([{id:"rotor",spinZ:4}]),_gosxMotionRuntimeBindings:new Map([["timeline",bindings]])};
    const child=normalizeSceneObject({id:"blade",parentNode:"rotor",kind:"box",x:2},0);
    const matrices=sceneGroupMatrices(state,5);
    const frame=sceneGroupRecords([child],matrices,"matrix")[0];
    return {position:sceneGroupTestOrigin(frame,5),childSpin:child.spinZ,childX:child.x};
  })()`);
  assert.ok(Math.abs(result.position.x)<1e-12);
  assert.ok(Math.abs(result.position.y-2)<1e-12);
  assert.equal(result.childSpin,0);
  assert.equal(result.childX,2);
});

test("a ray through the center of a half-turned scaled rotor tip hits its sphere", () => {
  const context=createCoreContext();
  const result=runJSON(context,`(() => {
    const state={groups:sceneNormalizeGroups([{id:"rig",x:-.4,scaleX:1.2,scaleY:.8,scaleZ:1},{id:"rotor",parentNode:"rig",spinZ:Math.PI/2}])};
    const source=normalizeSceneObject({id:"tip",kind:"sphere",radius:.28,x:2,parentNode:"rotor"},0);
    const object=sceneGroupRecords([source],sceneGroupMatrices(state,2),"matrix")[0];
    const vertices=sphereTriangleMesh(object).positions;
    const matrix=sceneObjectModelMatrix(object,2);
    const ray=sceneScreenToRay(296.3094177246094,291.5,1000,583,{x:0,y:0,z:9,rotationX:0,rotationY:0,rotationZ:0,fov:48});
    const origin=ray.origin,direction=ray.dir;
    let nearest=null;
    for(let i=0;i+8<vertices.length;i+=9) {
      const points=[];
      for(let k=0;k<9;k+=3) {
        const point=sceneMatrixTransformInto({},matrix,vertices[i+k],vertices[i+k+1],vertices[i+k+2],4,true);
        points.push(point);
      }
      const hit=sceneRayIntersectsTriangle(origin,direction,points[0],points[1],points[2]);
      if(hit && (!nearest ||hit.distance<nearest.distance)) nearest=hit;
    }
    return {nearest};
  })()`);
  assert.ok(result.nearest,"animated mesh must remain pickable at a shared triangle edge");
  assert.ok(result.nearest.distance<9.3,`must hit the front surface before the blade: ${JSON.stringify(result.nearest)}`);
});

test("browser group matrices agree with Go for nested spin, base rotation, shear and reflection", () => {
  const fixture=JSON.parse(fs.readFileSync(path.join(__dirname,"testdata/group-spin-affine.json"),"utf8"));
  const context=createCoreContext();
  context.groupParityFixture=fixture;
  const result=runJSON(context,`(() => {
    const state={groups:sceneNormalizeGroups(groupParityFixture.groups)};
    return groupParityFixture.samples.map(sample=>Object.fromEntries([...sceneGroupMatrices(state,sample.time)].map(([id,matrix])=>[id,Array.from(matrix)])));
  })()`);
  fixture.samples.forEach((sample,index)=>{
    for(const [id,want] of Object.entries(sample.matrices))want.forEach((value,component)=>{
      assert.ok(Math.abs(result[index][id][component]-value)<1e-10,`t=${sample.time}, ${id}[${component}] differs from Go`);
    });
  });
});

test("retained crowd poses invalidate only when the group or source pose changes", () => {
  const context=createCoreContext();
  const result=runJSON(context,`(() => {
    const state={groups:sceneNormalizeGroups([{id:"rotor",spinZ:1}])};
    const source=normalizeSceneObject({id:"agent",kind:"box",parentNode:"rotor"},0);
    source._crowdSkin={rev:7,atlas:{},batchID:"agents",parent:"",group:null};
    const first=sceneGroupRecords([source],sceneGroupMatrices(state,0),"matrix")[0];
    const revision0=first._crowdSkin.rev;
    const batch={alive:true};first._crowdSkin.group=batch;
    const second=sceneGroupRecords([source],sceneGroupMatrices(state,1),"matrix")[0];
    const revision1=second._crowdSkin.rev;
    const third=sceneGroupRecords([source],sceneGroupMatrices(state,1),"matrix")[0];
    return {revision0,revision1,revision2:third._crowdSkin.rev,same:first===second && second===third,sourceRevision:source._crowdSkin.rev,sourceGroup:source._crowdSkin.group,keptBatch:third._crowdSkin.group===batch};
  })()`);
  assert.ok(result.revision1>result.revision0);
  assert.equal(result.revision2,result.revision1);
  assert.equal(result.sourceRevision,7);
  assert.equal(result.sourceGroup,null);
  assert.equal(result.same,true);
  assert.equal(result.keptBatch,true);
});


test("first live hierarchy load preserves following transform command order", async () => {
  const context = createCoreContext();
  const pending = vm.runInContext(`(() => {
    const api = window.__gosx_scene3d_hierarchy_api;
    delete window.__gosx_scene3d_hierarchy_api;
    let finishLoad;
    ensureSceneGatedFeatureLoaded = () => new Promise(resolve => {
      finishLoad = () => { window.__gosx_scene3d_hierarchy_api = api; resolve(api); };
    });
    const state = createSceneState({scene:{objects:[]}}, null);
    const work = applySceneCommands(state, [
      {kind:15,data:{groups:[{id:"rotor",spinZ:1}]}},
      {kind:2,objectId:"rotor",data:{x:3,spinZ:0}},
      {kind:0,objectId:"tip",data:{kind:"mesh",props:{id:"tip",kind:"box",x:2,parentNode:"rotor"}}}
    ]);
    const deferred = !state.objects.has("tip");
    finishLoad();
    return work.then(() => ({
      deferred, x:state.groups[0].x, spin:state.groups[0].spinZ,
      point:sceneGroupTestOrigin(sceneGroupRecords([state.objects.get("tip")],sceneGroupMatrices(state,10),"matrix")[0],10)
    }));
  })()`, context);
  const result = JSON.parse(JSON.stringify(await pending));
  assert.equal(result.deferred, true);
  assert.equal(result.x, 3);
  assert.equal(result.spin, 0);
  assert.deepEqual(result.point, {x:5,y:0,z:0});
});

test("group-parented instance batches compose once even when a child matches the group prefix", () => {
 const context=createCoreContext();
 const result=runJSON(context,`(() => {
  const state={groups:sceneNormalizeGroups([{id:"rotor",x:3,y:4,spinZ:Math.PI/2,scaleX:2,scaleY:3}])};
  const hull=normalizeSceneObject({id:"rotor/hull",kind:"box",x:2,parentNode:"rotor"},0);
  const mesh=normalizeSceneInstancedMeshEntry({id:"windows",parentNode:"rotor",kind:"box",count:1,transforms:[1,0,0,0,0,1,0,0,0,0,1,0,1,0,0,1]},0,null);
  const build=t=>createSceneRenderBundle(640,360,"#000",{x:0,y:0,z:20,fov:60,near:.1,far:500},[hull],[],[],[],[],{},t,[],[mesh],[],[],[],0,false,{},state).instancedMeshes[0];
  const a=build(0), first=Array.from(a.transforms.slice(12,15));
  const b=build(1), second=Array.from(b.transforms.slice(12,15));
  const c=build(1);
  return {first,second,reused:b.transforms===c.transforms,revision:b._instanceStreamRevision===c._instanceStreamRevision,local:mesh.transforms[12]};
 })()`);
 assert.deepEqual(result.first,[5,4,0]);
 result.second.forEach((v,i)=>assert.ok(Math.abs(v-[3,6,0][i])<1e-6));
 assert.equal(result.reused,true);assert.equal(result.revision,true);assert.equal(result.local,1);
});

test("retained group motion keeps crowd pose and motion streams local and resident", () => {
 const context=createCoreContext();
 for (const motion of [false,true]) {
  const result=runJSON(context,`(() => {
   const state={groups:sceneNormalizeGroups([{id:"rotor",x:3,y:4,spinZ:Math.PI/2}])};
   const object=normalizeSceneObject({id:"agents/a",kind:"mesh",parentNode:"rotor",vertices:{count:3,positions:new Float32Array([0,0,0,1,0,0,0,1,0])}},0);
   object.vertices.immutable=true;object.parentMatrix=new Float32Array([1,0,0,0,0,1,0,0,0,0,1,0,2,0,0,1]);
   object._crowdSkin={atlas:{},bounds:{minX:0,minY:0,minZ:0,maxX:1,maxY:1,maxZ:1},rows:new Float32Array([1,2,.5]),batchID:"agents",rev:7,group:null,parent:"rotor"};
   if (${motion}) object._crowdMotion={atlas:object._crowdSkin.atlas,clipTable:{count:1},record:new Float32Array(24),dirty:false,bounds:{minX:1,minY:0,minZ:0,maxX:3,maxY:1,maxZ:1}};
   const build=t=>createSceneRenderBundle(640,360,"#000",{x:0,y:0,z:20,fov:60,near:.1,far:500},[object],[],[],[],[],{},t,[],[],[],[],[],0,false,{crowdBatches:true},state).meshObjects[0];
   const a=build(0), revision=a.crowdRevision, transforms=a.crowdTransforms, p0=Array.from(a.crowdParentMatrix);
   const b=build(1);
   return {parent0:p0.slice(12,15),parent1:Array.from(b.crowdParentMatrix),same:a===b,resident:transforms===b.crowdTransforms,revision:revision===b.crowdRevision,local:object.parentMatrix[12]};
  })()`);
  assert.deepEqual(result.parent0,[3,4,0]);
  assert.ok(Math.abs(result.parent1[0])<1e-6);assert.ok(Math.abs(result.parent1[1]-1)<1e-6);
  assert.equal(result.same,true);assert.equal(result.resident,true);assert.equal(result.revision,true);assert.equal(result.local,2);
 }
});
