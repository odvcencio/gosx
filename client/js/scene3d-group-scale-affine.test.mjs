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
  const webgl = readRuntime("webgl.ts");
  const webgpu = readRuntime("webgpu.ts");
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
  const webgl = readRuntime("webgl.ts");
  const webgpu = readRuntime("webgpu.ts");
  const selena = readSource("16a1-scene-webgpu-selena-uniforms.ts");
  function assertContract(gl, gpu, uniforms) {
    assert.match(gl, /vec4 q=gosxAffineNormal\(m,a_normal\)/);
    assert.match(gl, /gosxAffineNormal\(m,gosxNormal\)/);
    assert.match(gl, /gosxAffineNormal\(mat3\(u_modelMatrix \* selenaSkinMatrix\), a_normal\)/);
    assert.match(gpu, /gosxAffineNormal\(material\.modelMatrix, in\.normal\)/);
    assert.equal((gpu.match(/gosxAffineNormal\(model, in\.normal\)/g) || []).length, 1);
    assert.match(gpu, /WGSL_PBR_INSTANCED_CULL_VERTEX = WGSL_PBR_INSTANCED_VERTEX/);
    assert.match(gpu, /let an = gosxAffineNormal\(morph\.model, localNormal\)/);
    assert.match(gpu, /outTangents\[t \+ 3u\].*an\.w/);
    assert.match(uniforms, /sceneAffineNormalMatrix\(webGPUObjectModelMatrix\(owner\)\)/);
    assert.match(gl, /var reflectedDirect = directVertices && sceneAffineDeterminant\(obj\.modelMatrix, 0\) < 0;/);
    assert.match(gl, /if \(reflectedDirect\) gl\.frontFace\(gl\.CW\);[\s\S]*if \(reflectedDirect\) gl\.frontFace\(gl\.CCW\);/);
    assert.match(gpu, /function bindPBRPipeline\(reflected\)/);
    assert.match(gpu, /getPBRPipeline\(blendMode, depthWrite, reflected \? "cw" : "ccw"\)/);
    assert.doesNotMatch(gl, /normalize\(mat3\(u_modelMatrix\) \* \(mat3\(selenaSkinMatrix\) \* a_normal\)\)/);
    assert.doesNotMatch(gpu, /morph\.model \* vec4<f32>\(localNormal, 0\.0\)/);
  }
  assertContract(webgl, webgpu, selena);
  assert.throws(() => assertContract(webgl.replaceAll("gosxAffineNormal(m,a_normal)", "vec4(normalize(m*a_normal),1.0)"), webgpu, selena));
  assert.throws(() => assertContract(webgl, webgpu.replace("gosxAffineNormal(material.modelMatrix, in.normal)", "vec4f(normalize(in.normal),1.0)"), selena));
  assert.throws(() => assertContract(webgl.replaceAll("gl.frontFace(gl.CW)", "gl.frontFace(gl.CCW)"), webgpu, selena));
  assert.throws(() => assertContract(webgl, webgpu.replaceAll('reflected ? "cw" : "ccw"', '"ccw"'), selena));
});

test("glTF POINTS and LINES modes 0-3 retain exact affine through animation", () => {
  const context = createCoreContext({ mount: true });
  vm.runInContext(readRuntime("gltf.ts"), context, { filename: "gltf.ts" });
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
