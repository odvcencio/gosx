"use strict";
const test = require("node:test");
const assert = require("node:assert/strict");
const {createWebGLRendererForPost, makeWebGLBundleWithCustomPost, renderPreparedFrame} = require("./runtime-test-harness.js");

test("WebGL custom shader uploads paused and reversed scene time while wall time advances", async () => {
  const {renderer, canvas} = createWebGLRendererForPost({fresh: true});
  const gl = canvas.getContext("webgl2");
  const bundle = makeWebGLBundleWithCustomPost({
    name: "seekable",
    vertexGLSL: "#version 300 es\nin vec2 a_position; out vec2 v_uv; void main(){v_uv=a_position*.5+.5;gl_Position=vec4(a_position,0.,1.);}",
    fragmentGLSL: "#version 300 es\nprecision highp float; uniform float time; in vec2 v_uv; out vec4 color; void main(){color=vec4(v_uv,sin(time),1.);}",
    uniforms: {time: -99},
    shaderLayout: {uniformBlock: {fields: [{name: "time", type: "float"}]}},
  });
  const viewport = {cssWidth: 320, cssHeight: 180, pixelWidth: 320, pixelHeight: 180, pixelRatio: 1};
  let nowMS = 10000;
  try {
    for (const seconds of [1.25, 3.5, 1.25, 1.25]) {
      nowMS += 1000;
      await renderPreparedFrame(renderer, bundle, viewport, {nowMS, sceneTime: seconds});
      const uploads = gl.ops.filter(op => op[0] === "uniform1f" && op[1] === "time");
      assert.equal(uploads.at(-1)?.[2], seconds, "assert the shader upload, not a published data attribute");
    }
  } finally { renderer.dispose(); }
});

test("authored points share Selena scalar, vector and matrix uploads with a reserved clock", async () => {
  const {renderer, canvas} = createWebGLRendererForPost({fresh: true});
  const gl = canvas.getContext("webgl2"), bundle = makeWebGLBundleWithCustomPost();
  bundle.postEffects = [];
  Object.assign(bundle.points[0], {
    customVertex: "attribute vec3 position; uniform float time; void main(){gl_Position=vec4(position,1.);gl_PointSize=1.;}",
    customFragment: "precision mediump float; void main(){gl_FragColor=vec4(1.);}",
    customUniforms: {time: -99, gain: .75, single: [.5], pair: [1,2], triple: [1,2,3], quad: [1,2,3,4],
      basis: [1,0,0,0,1,0,0,0,1], transform: [1,0,0,0,0,1,0,0,0,0,1,0,0,0,0,1]},
  });
  try {
    await renderPreparedFrame(renderer, bundle, {cssWidth:320,cssHeight:180,pixelWidth:320,pixelHeight:180,pixelRatio:1}, {nowMS:9000,sceneTime:1.25});
    for (const [op, name, values] of [["uniform1f","gain",[.75]], ["uniform1f","single",[.5]],
      ["uniform2f","pair",[1,2]], ["uniform3f","triple",[1,2,3]], ["uniform4f","quad",[1,2,3,4]]]) {
      const upload = gl.ops.find(item => item[0] === op && item[1] === name);
      assert.deepEqual(upload?.slice(2), values, name);
    }
    assert.ok(gl.ops.some(item => item[0] === "uniformMatrix3fv" && item[1] === "basis"));
    assert.ok(gl.ops.some(item => item[0] === "uniformMatrix4fv" && item[1] === "transform"));
    assert.equal(gl.ops.filter(item => item[0] === "uniform1f" && item[1] === "time").at(-1)?.[2], 1.25);
  } finally { renderer.dispose(); }
});
