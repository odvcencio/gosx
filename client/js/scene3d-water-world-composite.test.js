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
  const target = { framebuffer: {}, width: 320, height: 180, linear: true };
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
            render(bundle, viewport, options) {
              draws.push({ pass: "world", bundle, viewport, options });
              options.compositeBeforePost(target);
              draws.push({ pass: "post", bundle });
            },
            renderSurfaces(bundle, target) { draws.push({ pass: "surfaces", bundle, target }); },
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

  assert.deepEqual(draws.map(({ pass }) => pass), ["world", "water", "surfaces", "post"]);
  assert.equal(draws[0].bundle.postEffects, bundle.postEffects);
  assert.equal(draws[0].options.compositeOverWater, false);
  assert.equal(draws[1].bundle, bundle);
  assert.equal(draws[1].options.compositeWorld, true);
  assert.equal(draws[1].options.clearComposite, false);
  assert.equal(draws[1].options.renderTarget, target);
  assert.equal(draws[0].viewport, draws[1].viewport);
  assert.equal(draws[2].bundle, bundle);
  assert.equal(draws[2].target, target);
  assert.deepEqual(Array.from(result.degraded), []);
});

const {
  FakeWebGLContext, createContext, runScript, bootstrapRuntimeSource, freshFeatureBundleSource,
} = require('./runtime-test-harness.js');

function rendererHarness() {
  const env = createContext({ enableWebGL2: true, disableCanvas2D: true });
  env.context.WebGL2RenderingContext = FakeWebGLContext;
  runScript(bootstrapRuntimeSource, env.context, 'bootstrap-runtime.js');
  runScript(freshFeatureBundleSource('scene3d'), env.context, 'scene3d.js');
  runScript(freshFeatureBundleSource('scene3d-webgl'), env.context, 'scene3d-webgl.js');
  const gl = new FakeWebGLContext();
  const canvas = { width: 640, height: 360 };
  const api = env.context.__gosx_scene3d_api;
  const renderer = env.context.__gosx_scene3d_webgl_api.createScenePBRRendererOrFallback(gl, canvas, {});
  assert.ok(renderer);
  const camera = { x: 0, y: 0, z: 6, fov: 60, near: 0.1, far: 100 };
  const bundle = api.createSceneRenderBundle(640, 360, '#000000', camera,
    [], [], [], [], [], {}, 0, [], [], [], [], [], 0, false);
  return { gl, canvas, renderer, bundle, env };
}

test('the real PBR post target includes a composite while models load and follows size changes', () => {
  const { gl, canvas, renderer, bundle } = rendererHarness();
  bundle.postEffects = [{ kind: 'fxaa' }];
  bundle.postFXMaxPixels = 320 * 180;
  const targets = [];
  function compositeBeforePost(target) {
    targets.push(target);
    const bind = gl.ops.filter(op => op[0] === 'bindFramebuffer').at(-1);
    assert.equal(bind[2], target.framebuffer.id);
    assert.ok(target.width * target.height <= bundle.postFXMaxPixels);
    assert.equal(target.linear, true);
    gl.ops.push(['composite']);
  }
  renderer.render(bundle, canvas, { compositeBeforePost });
  assert.equal(targets.length, 1, 'empty model data must not skip the water pass');
  assert.equal(targets[0].width, 320);
  assert.equal(targets[0].height, 180);
  const marker = gl.ops.findIndex(op => op[0] === 'composite');
  assert.ok(gl.ops.slice(marker + 1).some(op => op[0] === 'drawArrays'), 'post processing draws after the composite');
  const old = targets[0].framebuffer;
  bundle.postFXMaxPixels = 160 * 90;
  renderer.render(bundle, canvas, { compositeBeforePost });
  assert.notEqual(targets[1].framebuffer, old);
  assert.ok(gl.ops.some(op => op[0] === 'deleteFramebuffer' && op[1] === old.id));
  assert.equal(targets[1].width, 160);
  assert.equal(targets[1].height, 90);
  bundle.postEffects = [];
  renderer.render(bundle, canvas, { compositeBeforePost(target) {
    assert.equal(target.framebuffer, null);
    assert.equal(target.linear, false);
    assert.equal(target.width, 640);
    assert.equal(target.height, 360);
  }});
  renderer.dispose();
});

test('water restores the shared target after simulation without clearing world depth', () => {
  const { gl, canvas, renderer: world, bundle, env } = rendererHarness();
  gl.HALF_FLOAT = 0x140b;
  gl.FRAMEBUFFER_COMPLETE = 0x8cd5;
  gl.checkFramebufferStatus = () => gl.FRAMEBUFFER_COMPLETE;
  gl.getExtension = name => ['EXT_color_buffer_float', 'OES_texture_float_linear'].includes(name) ? {} : null;
  const entry = { id: 'tide', resolution: 16, surfaceResolution: 4, seedDrops: 0,
    activeObject: 'None', objectKind: 'none', renderPool: false };
  for (const name of ['simulation', 'normal', 'pool', 'surface']) {
    entry[name + 'VertexGLES'] = 'void main() {}';
    entry[name + 'FragmentGLES'] = 'void main() {}';
  }
  const water = env.context.__gosx_scene3d_webgl_api.createSceneWaterRendererWebGL(gl, canvas, entry);
  assert.ok(water);
  bundle.waterSystems = [entry];
  const target = { framebuffer: gl.createFramebuffer(), width: 320, height: 180, linear: true };
  water.render(bundle, canvas, { nowMS: 0, compositeWorld: true, renderTarget: target });
  gl.ops.length = 0;
  water.render(bundle, canvas, { nowMS: 17, compositeWorld: true, renderTarget: target });
  const bind = gl.ops.findLastIndex(op => op[0] === 'bindFramebuffer');
  assert.equal(gl.ops[bind][2], target.framebuffer.id);
  assert.deepEqual(gl.ops.filter(op => op[0] === 'viewport').at(-1), ['viewport', 0, 0, 320, 180]);
  assert.equal(gl.ops.slice(bind).some(op => op[0] === 'clear'), false, 'world depth must survive');
  assert.ok(gl.ops.slice(bind).some(op => op[0] === 'drawArrays'), 'water draws into the shared target');
  water.render(bundle, canvas, { nowMS: 34, compositeWorld: true });
  assert.equal(gl.ops.filter(op => op[0] === 'bindFramebuffer').at(-1)[2], null);
  water.dispose();
  world.dispose();
  assert.equal(gl.ops.some(op => op[0] === 'deleteFramebuffer' && op[1] === target.framebuffer.id), false,
    'the water pass must not own the world target');
});
