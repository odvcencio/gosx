import test from "node:test";
import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import vm from "node:vm";
import { fileURLToPath } from "node:url";

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const srcDir = path.join(__dirname, "bootstrap-src");
const runtimeDir = path.join(__dirname, "..", "runtime", "scene3d");

function readSource(name) {
  return fs.readFileSync(path.join(srcDir, name), "utf8");
}

function readRuntime(name) {
  return fs.readFileSync(path.join(runtimeDir, name), "utf8");
}

function trimBeforeSharedAPI(source) {
  const marker = source.indexOf("// Scene3D shared API");
  assert.ok(marker >= 0);
  return source.slice(0, marker);
}

function createCoreContext({ mount = false } = {}) {
  const sandbox = {
    console: { warn() {}, error() {}, log() {} },
    Math, JSON, Number, Object, Array, Map, Set, WeakMap,
    Float32Array, Float64Array, Uint8Array, Uint32Array, ArrayBuffer,
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
  ]) {
    vm.runInContext(readSource(name), context, { filename: name });
  }
  vm.runInContext(trimBeforeSharedAPI(readSource("10-runtime-scene-core.ts")), context, { filename: "10-runtime-scene-core.ts" });
  if (mount) {
    vm.runInContext(readRuntime("mount-webgl.ts"), context, { filename: "mount-webgl.ts" });
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
    };
  })()`);
  assert.equal(result.cloned, true);
  assert.equal(result.fallbackCloned, true);
  assert.equal(result.matrix[0], 2);
  assert.equal(result.reset, null);
  assert.equal(result.short, null);
  assert.equal(result.stringValue, null);
  assert.equal(result.nullValue, null);
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
    for (const invalid of [
      `null`,
      `[1,0,0,0,0,1,0,0,0,0,1,0,0,0,0]`,
      `[1,0,0,0,0,1,0,0,0,0,1,0,0,0,0,"1"]`,
      `[1,0,0,0,0,1,0,0,0,0,1,0,0,0,0,null]`,
      `[1,0,0,0,0,1,0,0,0,0,1,0,0,0,0,NaN]`,
    ]) {
      context.__matrix = vm.runInContext(invalid, context);
      const diagnostics = vm.runInContext(`__gosx_validate_scene_ir_strict(${family}, {strict:true}).diagnostics`, context);
      assert.ok(diagnostics.some((item) => item.code === "scene.transform.invalid_parent_matrix"), `${family} accepted ${invalid}`);
    }
  }
});

test("WebGL2 and WebGPU Points upload parent times live-local through the shared multiply", () => {
  const webgl = readRuntime("webgl.ts");
  const webgpu = readRuntime("webgpu.ts");
  assert.match(webgl, /sceneMat4MultiplyInto\(_pointsTilt, entry\.parentMatrix, _pointsModelMat\)/);
  assert.match(webgl, /uniformMatrix4fv\(pp\.uniforms\.modelMatrix, false, entry\.parentMatrix \? _pointsTilt : _pointsModelMat\)/);
  assert.match(webgpu, /sceneMat4MultiplyInto\(_pointsTilt, entry\.parentMatrix, _pointsModelMat\)/);
  assert.match(webgpu, /puF\.set\(pointsModelMat, 0\)/);
});
