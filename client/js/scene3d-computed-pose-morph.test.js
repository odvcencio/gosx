var test = require("node:test");
var assert = require("node:assert/strict");
var fs = require("node:fs");
var path = require("node:path");
var vm = require("node:vm");

var mountSrc = fs.readFileSync(
  path.join(__dirname, "..", "runtime", "scene3d", "mount-webgl.ts"),
  "utf8"
);
var webgpuSrc = fs.readFileSync(
  path.join(__dirname, "..", "runtime", "scene3d", "webgpu.ts"),
  "utf8"
);

function extractFn(source, name) {
  var start = source.indexOf("function " + name + "(");
  assert.ok(start >= 0, "missing " + name);
  var depth = 0;
  for (var i = source.indexOf("{", start); i < source.length; i++) {
    if (source[i] === "{") depth++;
    else if (source[i] === "}") {
      depth--;
      if (!depth) return source.slice(start, i + 1);
    }
  }
  throw new Error("unbalanced braces in " + name);
}

var fnNames = [
  "sceneComputedPoseFloat32Array",
  "sceneComputedPoseBlendArray",
  "sceneComputedPosePriorArray",
  "sceneComputedPoseApplyObjectMorph",
];

function makeSandbox() {
  var sandbox = {
    sceneNumber: function (value, fallback) {
      var n = typeof value === "number" ? value : Number(value);
      return Number.isFinite(n) ? n : fallback;
    },
    sceneModelTransformMatrix: function () {
      return null;
    },
    sceneModelTransformMeshFloats: function (array, tupleSize, fn) {
      var out = new Float32Array(array.length);
      for (var i = 0; i + tupleSize - 1 < array.length; i += tupleSize) {
        var result = fn(array[i], array[i + 1], array[i + 2], tupleSize === 4 ? array[i + 3] : undefined);
        out[i] = result.x;
        out[i + 1] = result.y;
        out[i + 2] = result.z;
        if (tupleSize === 4) out[i + 3] = result.w;
      }
      return out;
    },
    sceneModelTransformPoint: function (v) {
      return { x: v.x, y: v.y, z: v.z };
    },
    sceneModelTransformVector: function (v) {
      return { x: v.x, y: v.y, z: v.z };
    },
    sceneNormalizeDirection: function (v) {
      var len = Math.hypot(v.x, v.y, v.z);
      if (len < 0.000001) return { x: 0, y: 0, z: 0 };
      return { x: v.x / len, y: v.y / len, z: v.z / len };
    },
    sceneApplyModelObjectHiddenState: function () {},
  };
  var code = fnNames.map(function (name) {
    return extractFn(mountSrc, name);
  }).join("\n");
  vm.runInNewContext(code, sandbox);
  return sandbox;
}

function makeLocal(y, extras) {
  var local = {
    count: 1,
    positions: new Float32Array([0.1, y, 0.2]),
    uvs: new Float32Array([0, 0]),
  };
  if (extras) {
    for (var key in extras) local[key] = extras[key];
  }
  return local;
}

test("computed pose morph: CPU blend, prior copy semantics, and missing attributes", function () {
  var sandbox = makeSandbox();
  var apply = sandbox.sceneComputedPoseApplyObjectMorph;
  var model = { id: "model" };
  var object = {
    vertices: {
      positions: new Float32Array([0.1, 0.075, 0.2]),
      uvs: new Float32Array([0, 0]),
    },
  };
  var source = makeLocal(0.075);
  var target = makeLocal(0.375);

  apply(object, source, target, model, 0.5);
  assert.ok(Math.abs(object.vertices.positions[1] - 0.225) < 1e-6);
  assert.ok(Math.abs(object.computedMorph.targetPositions[1] - 0.375) < 1e-6);
  assert.ok(Math.abs(object.computedMorph.sourcePositions[1] - 0.075) < 1e-6);
  // sourcePositions is an independent copy, not aliased to the in-place cache.
  assert.notEqual(object.computedMorph.sourcePositions, object._computedPoseLocalPositions);

  apply(object, source, target, model, 0.5);
  assert.ok(Math.abs(object.vertices.positions[1] - 0.3) < 1e-6);
  assert.ok(Math.abs(object.computedMorph.sourcePositions[1] - 0.225) < 1e-6);
  assert.ok(Math.abs(object.computedMorph.targetPositions[1] - 0.375) < 1e-6);
});

test("computed pose morph: missing target normals/tangents fall back to authored source", function () {
  var sandbox = makeSandbox();
  var apply = sandbox.sceneComputedPoseApplyObjectMorph;
  var model = { id: "model" };
  var object = {
    vertices: {
      positions: new Float32Array([0.1, 0.075, 0.2]),
      uvs: new Float32Array([0, 0]),
    },
  };
  var source = makeLocal(0.075, {
    normals: new Float32Array([0, 1, 0]),
    tangents: new Float32Array([1, 0, 0, 1]),
  });
  var target = makeLocal(0.375);

  apply(object, source, target, model, 0.5);
  assert.ok(object.computedMorph.sourceNormals, "source normals published");
  assert.ok(object.computedMorph.targetNormals, "target normals fall back to source");
  assert.deepEqual(Array.from(object.computedMorph.targetNormals), Array.from(source.normals));
  assert.ok(object.computedMorph.sourceTangents, "source tangents published");
  assert.ok(object.computedMorph.targetTangents, "target tangents fall back to source");
  assert.deepEqual(Array.from(object.computedMorph.targetTangents), Array.from(source.tangents));
});

test("computed pose morph: absent source attributes publish null", function () {
  var sandbox = makeSandbox();
  var apply = sandbox.sceneComputedPoseApplyObjectMorph;
  var model = { id: "model" };
  var object = {
    vertices: {
      positions: new Float32Array([0.1, 0.075, 0.2]),
      uvs: new Float32Array([0, 0]),
    },
  };
  var source = makeLocal(0.075);
  var target = makeLocal(0.375);

  apply(object, source, target, model, 0.5);
  assert.equal(object.computedMorph.sourceNormals, null);
  assert.equal(object.computedMorph.targetNormals, null);
  assert.equal(object.computedMorph.sourceTangents, null);
  assert.equal(object.computedMorph.targetTangents, null);
});

test("webgpu morph shader: tangent-w line uses mix with no select", function () {
  var start = webgpuSrc.indexOf("var SCENE_COMPUTED_MORPH_SOURCE");
  assert.ok(start >= 0, "missing SCENE_COMPUTED_MORPH_SOURCE");
  var end = webgpuSrc.indexOf("].join(\"\\n\")", start);
  assert.ok(end > start, "unterminated shader array");
  var block = webgpuSrc.slice(start, end);
  var line = block
    .split("\n")
    .map(function (l) { return l.replace(/^\s*"|",?\s*$/g, "").trim(); })
    .find(function (l) { return l.indexOf("outTangents[t + 3u]") >= 0; });
  assert.ok(line, "missing tangent-w output line");
  assert.equal(
    line,
    "outTangents[t + 3u] = mix(sourcePacked[packed + 9u], targetPacked[packed + 9u], a);"
  );
  assert.equal(line.indexOf("select"), -1, "tangent-w line must not use select");
});
