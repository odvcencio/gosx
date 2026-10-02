"use strict";
const test = require("node:test");
const assert = require("node:assert/strict");
const { createWebGLRendererForPost, makeWebGLBundleWithCustomPost, createBoardWebGPUHarness, makePointsBundle } = require("./runtime-test-harness.js");

test("WebGL contact shadows bind view-space sunlight, bounded parameters, and a separate depth target", () => {
  const h = createWebGLRendererForPost({ fresh: true });
  const gl = h.canvas.getContext("webgl2"); gl.TEXTURE1 = gl.TEXTURE0 + 1; gl.deleteRenderbuffer = () => {};
  const vectors = new Map(); gl.uniform4fv = (loc, value) => vectors.set(loc.name, Array.from(value));
  const bundle = makeWebGLBundleWithCustomPost();
  bundle.lights = [{ kind: "directional", directionX: 0, directionY: -1, directionZ: 0 }];
  bundle.postEffects = [{ kind: "ssao" }, { kind: "contactShadows", distance: 50, thickness: 4, intensity: 0.6, bias: 0.02 }, { kind: "fxaa" }];
  h.renderer.render(bundle, { width: 320, height: 180 });
  assert.deepEqual(vectors.get("u_contactParams"), [10, 2, 0.02, 0.6]);
  assert.ok(gl.ops.some(op => op[0] === "uniform3f" && op[1] === "u_lightDirection" && op[2] === 0 && op[3] === 1 && op[4] === 0));
  assert.equal(gl.ops.filter(op => op[0] === "uniform1i" && op[1] === "u_depthTexture").length, 2);
  h.renderer.dispose();
});

test("WebGPU contact shadows use depth, aligned camera uniforms, and an actual fullscreen dispatch", async () => {
  const h = await createBoardWebGPUHarness({ fresh: true });
  const bundle = makePointsBundle({ id: "p", count: 1, positions: [0, 0, 0] });
  bundle.postEffects = [{ kind: "contactShadows", distance: 1.5, thickness: 0.125, intensity: 0.5, bias: 0.01, direction: { x: 0, y: -1, z: 0 } }];
  h.renderer.render(bundle, { width: 320, height: 180 });
  const shader = h.fake.state.shaderModules.find(m => m.label === "post-contactShadows");
  assert.ok(shader, "the contact shadow shader is compiled");
  assert.match(shader.code, /textureLoad\(depthTex/);
  assert.ok(h.fake.state.renderPasses.flatMap(p => p.draws).some(d => d.pipeline?.desc?.fragment?.module === shader));
  h.renderer.dispose();
});
const path = require("node:path");
const { execFileSync } = require("node:child_process");

const axes = [[0, -1, 0], [0, 1, 0], [-1, 0, 0], [1, 0, 0], [0, 0, -1], [0, 0, 1]];
const wireCases = JSON.parse(execFileSync("go", ["run", "./client/js/testdata/contact-shadow-directions.go"], {
  cwd: path.resolve(__dirname, "../.."), env: { ...process.env, GOWORK: "off" }, encoding: "utf8",
}));

function assertDirection(actual, expected) {
  assert.equal(actual.length, 3);
  actual.forEach((value, i) => assert.ok(Math.abs(value - expected[i]) < 0.000001,
    `component ${i}: got ${value}, expected ${expected[i]}`));
}

function contactBuffers(h) {
  return h.fake.state.renderPasses.filter(pass => pass.draws.some(draw =>
    draw.pipeline?.desc?.fragment?.module?.label === "post-contactShadows"))
    .map(pass => pass.bindGroups.find(binding => binding.slot === 0).group.desc.entries
      .find(entry => entry.binding === 3).resource.buffer);
}

function latestUniforms(h, buffer) {
  return Array.from(h.fake.state.writeBufferCalls.filter(write => write.buffer === buffer).at(-1).data);
}

for (const [axisIndex, axis] of axes.entries()) {
  for (const [sourceIndex, source] of ["authored", "sunlight"].entries()) {
    const wire = wireCases[axisIndex * 2 + sourceIndex];
    const vector = source === "authored" ? wire.postEffects[0].direction : wire.lights[0];
    assert.equal(Object.keys(vector).filter(key => /^([xyz]|direction[XYZ])$/.test(key)).length, 1,
      "the Go wire payload must omit both zero components");
    for (const backend of ["WebGL", "WebGPU"]) {
      test(`${backend} preserves Go JSON ${source} direction (${axis})`, async () => {
        const h = backend === "WebGL" ? createWebGLRendererForPost({ fresh: true }) :
          await createBoardWebGPUHarness({ fresh: true });
        if (backend === "WebGL") {
          const gl = h.canvas.getContext("webgl2");
          gl.TEXTURE1 = gl.TEXTURE0 + 1; gl.deleteRenderbuffer = () => {}; gl.uniform4fv = () => {};
        }
        const bundle = backend === "WebGL" ? makeWebGLBundleWithCustomPost() :
          makePointsBundle({ id: "p", count: 1, positions: [0, 0, 0] });
        bundle.postEffects = wire.postEffects;
        bundle.lights = wire.lights || [];
        try {
          h.renderer.render(bundle, { width: 320, height: 180 });
          const actual = backend === "WebGL" ? h.canvas.getContext("webgl2").ops
            .find(op => op[0] === "uniform3f" && op[1] === "u_lightDirection").slice(2) :
            latestUniforms(h, contactBuffers(h)[0]).slice(16, 19);
          assertDirection(actual, axis.map(value => -value));
        } finally { h.renderer.dispose(); }
      });
    }
  }
}

test("WebGPU contact passes retain separate settings, reuse slot buffers, and destroy them", async () => {
  const h = await createBoardWebGPUHarness({ fresh: true });
  const bundle = makePointsBundle({ id: "p", count: 1, positions: [0, 0, 0] });
  bundle.postEffects = [
    { kind: "contactShadows", distance: 1, thickness: 0.125, bias: 0.015625, intensity: 0.5, direction: { y: -1 } },
    { kind: "fxaa" },
    { kind: "contactShadows", distance: 2, thickness: 0.25, bias: 0.03125, intensity: 0.75, direction: { x: -1 } },
  ];
  let buffers;
  try {
    h.renderer.render(bundle, { width: 320, height: 180 });
    buffers = contactBuffers(h);
    assert.equal(buffers.length, 2, "both contact effects draw");
    assert.notEqual(buffers[0], buffers[1], "uploads before submission must not overwrite another pass");
    assertDirection(latestUniforms(h, buffers[0]).slice(16, 19), [0, 1, 0]);
    assertDirection(latestUniforms(h, buffers[1]).slice(16, 19), [1, 0, 0]);
    assert.deepEqual(latestUniforms(h, buffers[0]).slice(20), [1, 0.125, 0.015625, 0.5]);
    assert.deepEqual(latestUniforms(h, buffers[1]).slice(20), [2, 0.25, 0.03125, 0.75]);
    bundle.postEffects[0].distance = 3;
    bundle.postEffects[2].intensity = 0.25;
    h.renderer.render(bundle, { width: 320, height: 180 });
    assert.deepEqual(contactBuffers(h).slice(-2), buffers, "slots reuse their buffers next frame");
    assert.deepEqual(latestUniforms(h, buffers[0]).slice(20), [3, 0.125, 0.015625, 0.5]);
    assert.deepEqual(latestUniforms(h, buffers[1]).slice(20), [2, 0.25, 0.03125, 0.25]);
  } finally { h.renderer.dispose(); }
  assert.ok(buffers.every(buffer => buffer.destroyed), "dispose releases each slot's uniform storage");
});
