"use strict";
const test = require("node:test");
const assert = require("node:assert/strict");
const vm = require("node:vm");
const { readSceneRendererBackendSrc } = require("./scene3d-renderer-source-set.js");

function slices() {
  const source = readSceneRendererBackendSrc("webgl");
  const start = source.indexOf("  function scenePBRVertexSlice(");
  const end = source.indexOf("  function createScenePBRRenderer(", start);
  assert.ok(start >= 0 && end > start);
  const context = vm.createContext({ Float32Array, Number });
  vm.runInContext(source.slice(start, end), context);
  return { slice: (source, offset, count, stride) => context.scenePBRVertexSlice(source, offset, count, stride, new Float32Array(count * stride)) };
}

test("WebGL draws Float32 position, normal, UV and tangent ranges without copying", () => {
  for (const stride of [2, 3, 4]) {
    const { slice } = slices();
    const source = new Float32Array(Array.from({ length: stride * 8 }, (_, i) => i / 7));
    const range = slice(source, 2, 3, stride, "positions");
    assert.deepEqual([...range], [...source.slice(2 * stride, 5 * stride)]);
    assert.equal(range.buffer, source.buffer);
    assert.equal(range.byteOffset, source.byteOffset + 2 * stride * source.BYTES_PER_ELEMENT);
    source[2 * stride] = 17;
    assert.equal(range[0], 17, "subsequent dynamic geometry updates remain visible");
  }
});

test("Float32 views respect an existing buffer offset and support empty ranges", () => {
  const { slice } = slices();
  const source = new Float32Array([90, 91, 1, 2, 3, 4, 5, 6]).subarray(2);
  assert.deepEqual([...slice(source, 1, 1, 3, "normals")], [4, 5, 6]);
  assert.equal(slice(source, 2, 0, 3, "positions").length, 0);
});

test("legacy, Float64 and incomplete vertex streams retain conversion and zero padding", () => {
  for (const [source, offset, expected] of [
    [["1", undefined, "3", 4], 0, [1, 0, 3]],
    [new Float64Array([1 / 3, 2 / 3, 1]), 0, [...new Float32Array([1 / 3, 2 / 3, 1])]],
    [new Float32Array([1, 2, 3, 4]), 1, [4, 0, 0]],
    [null, 0, [0, 0, 0]],
    [new Float32Array([1, 2, 3]), -1, [0, 0, 0]],
  ]) {
    const { slice } = slices();
    assert.deepEqual([...slice(source, offset, 1, 3, "positions")], expected);
  }
});
