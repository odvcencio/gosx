"use strict";

const test = require("node:test");
const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const vm = require("node:vm");

const source = fs.readFileSync(
  path.join(__dirname, "../runtime/scene3d/mount-webgl.ts"), "utf8",
);

test("WebGL water blends over the depth-tested world in one context", () => {
  const draws = [];
  const gl = {};
  const context = {
    sceneNumber: (value, fallback) => Number(value) || fallback,
    sceneBool: (value, fallback) => value == null ? fallback : Boolean(value),
    window: {
      __gosx_scene3d_webgl_api: {
        createSceneWaterRendererWebGL(receivedGL) {
          assert.equal(receivedGL, gl);
          return {
            render(bundle, viewport, options) { draws.push({ pass: "water", bundle, viewport, options }); },
            dispose() {},
          };
        },
        createScenePBRRendererOrFallback(receivedGL) {
          assert.equal(receivedGL, gl);
          return {
            render(bundle, viewport, options) { draws.push({ pass: "world", bundle, viewport, options }); },
            renderSurfaces(bundle) { draws.push({ pass: "surfaces", bundle }); },
            dispose() {},
          };
        },
      },
    },
  };
  vm.createContext(context);
  vm.runInContext(source, context, { filename: "mount-webgl.ts" });

  const canvas = { getContext: (kind) => kind === "webgl2" ? gl : null };
  const props = { scene: { waterSystems: [{ id: "cove" }], models: [{ id: "cliff" }] } };
  const result = context.createSceneWaterWebGLResult(canvas, props, { tier: "full" }, "");
  assert.equal(result.renderer.isWaterWorldComposite, true);

  const bundle = { background: "#31424b", postEffects: [{ kind: "fxaa" }] };
  const viewport = { width: 640, height: 360 };
  result.renderer.render(bundle, viewport, { nowMS: 42 });

  assert.deepEqual(draws.map(({ pass }) => pass), ["world", "water", "surfaces"]);
  assert.equal(draws[0].bundle.postEffects.length, 0);
  assert.equal(draws[0].options.compositeOverWater, false);
  assert.equal(draws[1].bundle, bundle);
  assert.equal(draws[1].options.compositeWorld, true);
  assert.equal(draws[1].options.clearComposite, false);
  assert.equal(draws[0].viewport, draws[1].viewport);
  assert.equal(draws[2].bundle, bundle);
});
