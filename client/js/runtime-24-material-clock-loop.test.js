"use strict";
// The material-clock animation source.
//
// A material that declares a `time` uniform is animated by the per-frame
// clock the renderer feeds, even when nothing else in the scene moves. The
// m31labs content starfields are the canonical case: layer spin was removed,
// every twinkle and depth-wrap cycle lives in the shader clock, and the loop
// gate knew nothing about it — the scene reported "static" after one frame
// and the whole field froze on screen. These tests pin the new source and the
// boundary around it: a genuinely static scene must still stop the loop.

const test = require("node:test");
const assert = require("node:assert/strict");

const {
  bootstrapSource,
  FakeElement,
  createContext,
  runScript,
  flushAsyncWork,
} = require("./runtime-test-harness.js");

function starfieldLikeManifest(points) {
  return {
    engines: [
      {
        id: "gosx-engine-clock",
        component: "GoSXScene3D",
        kind: "surface",
        mountId: "scene-clock-root",
        jsExport: "GoSXScene3D",
        props: {
          width: 480,
          height: 300,
          autoRotate: false,
          scene: { points: points },
        },
        capabilities: ["canvas", "animation"],
      },
    ],
  };
}

const CLOCK_POINTS = [{
  id: "starfield-like",
  count: 3,
  positions: [0, 0, 0, 4, 0, 0, 0, 4, 0],
  sizes: [2, 2, 2],
  color: "#ffffff",
  // No spin anywhere. The only animation source is the time uniform the
  // authored material declares.
  customUniforms: { time: 0 },
}];

test("a time-uniform material keeps the render loop running", async () => {
  const mount = new FakeElement("div", null);
  mount.id = "scene-clock-root";
  const env = createContext({
    elements: [mount],
    manifest: starfieldLikeManifest(CLOCK_POINTS),
  });

  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();
  await flushAsyncWork();

  assert.equal(mount.getAttribute("data-gosx-scene3d-render-loop-reason"), "material-clock");
  assert.equal(mount.getAttribute("data-gosx-scene3d-render-loop"), "active");
  assert.equal(mount.getAttribute("data-gosx-scene3d-render-loop-wants-animation"), "true");
});

test("a static points scene still stops the loop", async () => {
  const mount = new FakeElement("div", null);
  mount.id = "scene-clock-root";
  const staticPoints = [{
    id: "static-dust",
    count: 3,
    positions: [0, 0, 0, 4, 0, 0, 0, 4, 0],
    sizes: [2, 2, 2],
    color: "#ffffff",
    // Uniforms that carry no clock must not fake an animation source.
    customUniforms: { tintAmount: 0.2 },
  }];
  const env = createContext({
    elements: [mount],
    manifest: starfieldLikeManifest(staticPoints),
  });

  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();
  await flushAsyncWork();

  assert.equal(mount.getAttribute("data-gosx-scene3d-render-loop-reason"), "static");
  assert.equal(mount.getAttribute("data-gosx-scene3d-render-loop"), "stopped");
});

test("a shaderLayout time field also counts as a clock", async () => {
  const mount = new FakeElement("div", null);
  mount.id = "scene-clock-root";
  const layoutPoints = [{
    id: "layout-clocked",
    count: 3,
    positions: [0, 0, 0, 4, 0, 0, 0, 4, 0],
    sizes: [2, 2, 2],
    color: "#ffffff",
    shaderLayout: { uniformBlock: { fields: [{ name: "time", type: "f32" }] } },
  }];
  const env = createContext({
    elements: [mount],
    manifest: starfieldLikeManifest(layoutPoints),
  });

  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();
  await flushAsyncWork();

  assert.equal(mount.getAttribute("data-gosx-scene3d-render-loop-reason"), "material-clock");
  assert.equal(mount.getAttribute("data-gosx-scene3d-render-loop"), "active");
});
