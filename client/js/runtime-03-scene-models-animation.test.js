"use strict";
// Declarative Scene3D model loading: glTF and GLB parsing, point and line
// primitives, skinning, animation playback and the Selena skin material.
//
// Split out of the former client/js/runtime.test.js, which this set replaces
// and which no longer exists. Every shared fake, sandbox builder and fixture
// factory lives in ./runtime-test-harness.js.

const test = require("node:test");
const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");

const {
  bootstrapSource,
  bootstrapScene3DMountSourceFile,
  FakeWebGLContext,
  FakeElement,
  buildMinimalGLBBytes,
  buildPointLineGLBBytes,
  buildSkinnedGLBBytes,
  createContext,
  installManualRAF,
  flushSceneInitialFrameBoundary,
  runScript,
  flushAsyncWork,
  SELENA_SKINNABLE_VERTEX_GLSL_FIXTURE,
  SELENA_SKINNABLE_FRAGMENT_GLSL_FIXTURE,
  SELENA_SKINNABLE_SHADER_LAYOUT_FIXTURE,
  readBootstrapSrc,
} = require("./runtime-test-harness.js");

test("typed Scene3D host stages progressive model previews and publishes terminal lifecycle states", () => {
  const coreSource = readBootstrapSrc("10-runtime-scene-core.ts");

  assert.match(coreSource, /const progressive = Boolean\(current\.progressive && previewSrc && fullSrc\)/);
  assert.match(coreSource, /src: progressive \? previewSrc/);
  assert.match(bootstrapScene3DMountSourceFile, /function scheduleSceneProgressiveModelLifecycle/);
  assert.match(bootstrapScene3DMountSourceFile, /"preview-ready"/);
  assert.match(bootstrapScene3DMountSourceFile, /"full-preload-failed"/);
  assert.match(bootstrapScene3DMountSourceFile, /"full-settled"/);
  assert.match(bootstrapScene3DMountSourceFile, /cancelSceneProgressiveModelLifecycle/);
});

test("bootstrap loads declarative Scene3D model assets without authored JS", async () => {
  const mount = new FakeElement("div", null);
  mount.id = "scene-model-root";
  const modelStatuses = [];
  mount.addEventListener("gosx:scene3d:model-status", (event) => modelStatuses.push(event.detail));

  const env = createContext({
    elements: [mount],
    fetchRoutes: {
      "/models/runner.gosx3d.json": {
        text: JSON.stringify({
          objects: [
            {
              id: "runner-frame",
              kind: "lines",
              points: [
                { x: -0.8, y: -0.3, z: 0 },
                { x: 0.9, y: -0.3, z: 0 },
                { x: 0.9, y: 0.35, z: 0.2 },
                { x: -0.8, y: 0.35, z: 0.2 },
                { x: -0.2, y: 0.65, z: 0.45 },
                { x: 0.25, y: 0.65, z: 0.45 },
              ],
              segments: [[0, 1], [1, 2], [2, 3], [3, 0], [2, 4], [3, 5], [4, 5]],
              material: {
                kind: "matte",
                color: "#5ca8ff",
              },
            },
          ],
          labels: [
            {
              id: "runner-label",
              text: "Model asset",
              x: 0,
              y: 1.05,
              z: 0.35,
              maxWidth: 160,
            },
          ],
          sprites: [
            {
              id: "runner-card",
              src: "../paper-card.png",
              x: 0,
              y: 0.62,
              z: 0.12,
              width: 1.5,
              height: 1,
              opacity: 0.92,
              priority: 3,
              anchorX: 0.5,
              anchorY: 0.5,
              fit: "cover",
              occlude: false,
            },
          ],
          lights: [
            {
              id: "runner-light",
              kind: "point",
              color: "#ffd48f",
              intensity: 1.15,
              x: 0.4,
              y: 0.9,
              z: 1.2,
              range: 4.8,
              decay: 1.35,
            },
          ],
        }),
      },
    },
    manifest: {
      engines: [
        {
          id: "gosx-engine-model",
          component: "GoSXScene3D",
          kind: "surface",
          mountId: "scene-model-root",
          props: {
            width: 640,
            height: 360,
            background: "#08151f",
            models: [
              {
                id: "runner",
                src: "/models/runner.gosx3d.json",
                x: 1.1,
                y: 0.2,
                z: -0.6,
                rotationY: 0.42,
                scaleX: 1.35,
                scaleY: 1.1,
                scaleZ: 1.2,
                bounds: 2,
                fit: "contain",
                fitAlign: "center",
                materialKind: "glow",
                color: "#ffd48f",
                opacity: 0.74,
                emissive: 0.26,
                blendMode: "additive",
                renderPass: "additive",
                static: true,
              },
            ],
            scene: {
              objects: [
                {
                  id: "guide",
                  kind: "lines",
                  points: [
                    { x: -1, y: -0.8, z: 0 },
                    { x: 1, y: -0.8, z: 0 },
                    { x: 1, y: 0.8, z: 0 },
                    { x: -1, y: 0.8, z: 0 },
                  ],
                  segments: [[0, 1], [1, 2], [2, 3], [3, 0]],
                  color: "#8de1ff",
                },
              ],
            },
          },
        },
      ],
    },
  });
  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();

  assert.equal(env.fetchCalls.some((call) => call.url === "/models/runner.gosx3d.json"), true);
  assert.deepEqual(modelStatuses.map((entry) => entry.status), ["loading", "loaded"]);
  assert.equal(modelStatuses[0].asset, "/models/runner.gosx3d.json");
  assert.equal(modelStatuses[0].cached, false);
  assert.equal(mount.getAttribute("data-gosx-scene3d-model-status"), "loaded");
  assert.equal(mount.getAttribute("data-gosx-scene3d-model-asset"), "/models/runner.gosx3d.json");
  assert.equal(mount.getAttribute("data-gosx-scene3d-model-cache"), "false");
  assert.equal(mount.getAttribute("data-gosx-scene3d-mounted"), "true");
  assert.equal(mount.getAttribute("data-gosx-scene3d-renderer"), "canvas");
  assert.equal(mount.children[0].tagName, "CANVAS");
  assert.equal(mount.children[1].getAttribute("data-gosx-scene3d-label-layer"), "true");
  assert.equal(mount.children[1].children.length, 2);
  const modelLabel = mount.children[1].children.find((child) => child.getAttribute("data-gosx-scene-label") === "runner/runner-label");
  const modelSprite = mount.children[1].children.find((child) => child.getAttribute("data-gosx-scene-sprite") === "runner/runner-card");
  assert.ok(modelLabel);
  assert.equal(modelLabel.textContent, "Model asset");
  assert.ok(modelSprite);
  assert.equal(modelSprite.getAttribute("data-gosx-scene-sprite-fit"), "cover");
  assert.equal(modelSprite.firstChild.getAttribute("src"), "http://localhost:3000/paper-card.png");
  const ctx2d = mount.children[0].getContext("2d");
  assert.ok(ctx2d.ops.some((entry) => entry[0] === "lineTo"));
  assert.equal(env.consoleLogs.error.length, 0);
});

test("bootstrap loads declarative Scene3D GLB model assets through the native renderer path", async () => {
  const mount = new FakeElement("div", null);
  mount.id = "scene-model-glb-root";

  const env = createContext({
    elements: [mount],
    enableWebGL2: true,
    disableCanvas2D: true,
    fetchRoutes: {
      "/models/runner.glb": {
        bytes: buildMinimalGLBBytes(),
      },
    },
    manifest: {
      engines: [
        {
          id: "gosx-engine-model-glb",
          component: "GoSXScene3D",
          kind: "surface",
          mountId: "scene-model-glb-root",
          props: {
            width: 640,
            height: 360,
            background: "#08151f",
            models: [
              {
                id: "runner",
                src: "/models/runner.glb",
                x: 0.35,
                y: 0.1,
                z: -0.4,
                rotationY: 0.2,
                scaleX: 1.1,
                scaleY: 1.1,
                scaleZ: 1.1,
                static: true,
              },
            ],
          },
        },
      ],
    },
  });

  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();

  assert.equal(env.fetchCalls.some((call) => call.url === "/models/runner.glb"), true);
  assert.equal(mount.getAttribute("data-gosx-scene3d-mounted"), "true");
  assert.equal(mount.getAttribute("data-gosx-scene3d-renderer"), "webgl");
  const gl = mount.children[0].getContext("webgl2");
  assert.ok(gl);
  assert.ok(gl.ops.some((entry) => entry[0] === "drawArrays" && entry[1] === gl.TRIANGLES && entry[3] >= 3));
  assert.equal(env.consoleLogs.error.length, 0);
});

test("bootstrap GLB loader extracts Scene3D POINTS and LINES primitives", async () => {
  const env = createContext({
    fetchRoutes: {
      "/models/points-lines.glb": {
        bytes: buildPointLineGLBBytes(),
      },
    },
  });

  runScript(bootstrapSource, env.context, "bootstrap.js");
  const scene = await env.context.__gosx_scene3d_gltf_api.sceneLoadGLTFModel("/models/points-lines.glb");

  assert.equal(scene.points.length, 1);
  assert.equal(scene.objects.length, 1);

  const points = scene.points[0];
  assert.equal(points.id, "sparks");
  assert.equal(points.count, 3);
  assert.equal(points.style, "glow");
  assert.equal(points.blendMode, "additive");
  assert.deepEqual(points.live, ["palette"]);
  assert.equal(ArrayBuffer.isView(points.positions), true);
  assert.equal(ArrayBuffer.isView(points.sizes), true);
  assert.equal(ArrayBuffer.isView(points.colors), true);
  assert.deepEqual(Array.from(points.sizes), [2, 3, 4]);
  assert.equal(points.positions[0], 1);
  assert.equal(points.positions[1], 0.5);
  assert.equal(points.colors[0], 1);
  assert.equal(points.colors[1], 0);
  assert.equal(points.colors[2], 0);
  assert.equal(points.colors[3], 1);
  assert.ok(Math.abs(points.colors[7] - (192 / 255)) < 0.00001);

  const lines = scene.objects[0];
  assert.equal(lines.id, "filament-lines");
  assert.equal(lines.kind, "lines");
  assert.equal(lines.points.length, 3);
  assert.equal(JSON.stringify(lines.lineSegments), JSON.stringify([[0, 1], [1, 2]]));
  assert.equal(env.consoleLogs.error.length, 0);
});

test("bootstrap GLB loader accepts query-stringed GLB model URLs", async () => {
  const env = createContext({
    fetchRoutes: {
      "/models/points-lines.glb?bucket=202604211430": {
        bytes: buildPointLineGLBBytes(),
      },
    },
  });

  runScript(bootstrapSource, env.context, "bootstrap.js");
  const scene = await env.context.__gosx_scene3d_gltf_api.sceneLoadGLTFModel("/models/points-lines.glb?bucket=202604211430");

  assert.equal(scene.points.length, 1);
  assert.equal(scene.points[0].id, "sparks");
  assert.equal(scene.objects.length, 1);
  assert.equal(env.fetchCalls.some((call) => call.url === "/models/points-lines.glb?bucket=202604211430"), true);
  assert.equal(env.consoleLogs.warn.length, 0);
  assert.equal(env.consoleLogs.error.length, 0);
});

test("bootstrap glTF loader resolves external buffers from root-relative model URLs", async () => {
  const env = createContext({
    fetchRoutes: {
      "/models/duck/Duck.gltf": {
        text: JSON.stringify({
          asset: { version: "2.0", generator: "runtime-test" },
          buffers: [{ uri: "Duck0.bin", byteLength: 0 }],
          scene: 0,
          scenes: [{ nodes: [] }],
          nodes: [],
        }),
      },
      "http://localhost:3000/models/duck/Duck0.bin": {
        bytes: [],
      },
    },
  });

  runScript(bootstrapSource, env.context, "bootstrap.js");
  const scene = await env.context.__gosx_scene3d_gltf_api.sceneLoadGLTFModel("/models/duck/Duck.gltf");

  assert.equal(scene.objects.length, 0);
  assert.equal(env.fetchCalls.some((call) => call.url === "/models/duck/Duck.gltf"), true);
  assert.equal(env.fetchCalls.some((call) => call.url === "http://localhost:3000/models/duck/Duck0.bin"), true);
  assert.equal(env.consoleLogs.warn.length, 0);
  assert.equal(env.consoleLogs.error.length, 0);
});

test("bootstrap hydrates Scene3D model POINTS from GLB assets", async () => {
  const mount = new FakeElement("div", null);
  mount.id = "scene-model-glb-points-root";

  const env = createContext({
    elements: [mount],
    enableWebGL2: true,
    disableCanvas2D: true,
    fetchRoutes: {
      "/models/points-lines.glb": {
        bytes: buildPointLineGLBBytes(),
      },
    },
    manifest: {
      engines: [
        {
          id: "gosx-engine-model-glb-points",
          component: "GoSXScene3D",
          kind: "surface",
          mountId: "scene-model-glb-points-root",
          props: {
            width: 640,
            height: 360,
            background: "#08151f",
            camera: { z: 5 },
            models: [
              {
                id: "galaxy",
                src: "/models/points-lines.glb",
                scaleX: 1.25,
                scaleY: 1.25,
                scaleZ: 1.25,
                static: true,
              },
            ],
          },
        },
      ],
    },
  });

  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();

  assert.equal(env.fetchCalls.some((call) => call.url === "/models/points-lines.glb"), true);
  assert.equal(mount.getAttribute("data-gosx-scene3d-renderer"), "webgl");
  const gl = mount.children[0].getContext("webgl2");
  const sentinelIDs = mount.__gosxScene3DSentinels
    ? Array.from(mount.__gosxScene3DSentinels.keys())
    : [];
  assert.equal(sentinelIDs.includes("galaxy/sparks"), true);
  assert.equal(sentinelIDs.includes("scene-object-0"), false);
  assert.equal(sentinelIDs.includes("scene-object-1"), false);
  assert.ok(gl.ops.some((entry) => entry[0] === "drawArrays" && entry[1] === gl.LINES && entry[3] >= 4));
  assert.equal(env.consoleLogs.error.length, 0);
});

test("bootstrap restores GLB Scene3D point layers through the WebGL2 renderer", async () => {
  const mount = new FakeElement("div", null);
  mount.id = "scene-model-glb-points-restore-root";

  const env = createContext({
    elements: [mount],
    enableWebGL2: true,
    fetchRoutes: {
      "/models/points-lines.glb": {
        bytes: buildPointLineGLBBytes(),
      },
    },
    manifest: {
      engines: [
        {
          id: "gosx-engine-model-glb-points-restore",
          component: "GoSXScene3D",
          kind: "surface",
          mountId: "scene-model-glb-points-restore-root",
          props: {
            width: 640,
            height: 360,
            forceWebGL: true,
            autoRotate: false,
            background: "#08151f",
            camera: { z: 5 },
            models: [
              {
                id: "galaxy",
                src: "/models/points-lines.glb",
                static: true,
              },
            ],
          },
        },
      ],
    },
  });
  env.context.WebGL2RenderingContext = FakeWebGLContext;

  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();

  const canvas = mount.children[0];
  const initialGl = canvas.getContext("webgl2");
  assert.ok(
    initialGl.ops.some((entry) => entry[0] === "drawArrays" && entry[1] === initialGl.POINTS && entry[3] === 3),
    "initial WebGL2 renderer must draw GLB point layers",
  );

  canvas.dispatchEvent({ type: "webglcontextlost", preventDefault() {} });
  await flushAsyncWork();
  canvas._webglContext = null;
  canvas.dispatchEvent({ type: "webglcontextrestored" });
  await flushAsyncWork();

  const restoredGl = canvas.getContext("webgl2");
  assert.notEqual(restoredGl, initialGl);
  assert.ok(
    restoredGl.ops.some((entry) => entry[0] === "getUniformLocation" && entry[1] === "u_defaultSize"),
    "restored renderer must use the WebGL2 point shader path",
  );
  assert.ok(
    restoredGl.ops.some((entry) => entry[0] === "drawArrays" && entry[1] === restoredGl.POINTS && entry[3] === 3),
    "restored renderer must draw GLB point layers",
  );
  assert.equal(mount.getAttribute("data-gosx-scene3d-renderer"), "webgl");
  assert.equal(mount.getAttribute("data-gosx-scene3d-renderer-fallback"), null);
  assert.equal(env.consoleLogs.error.length, 0);
});

test("bootstrap requests opaque WebGL canvas for opaque Scene3D backgrounds", async () => {
  const mount = new FakeElement("div", null);
  mount.id = "scene-opaque-canvas-root";

  const env = createContext({
    elements: [mount],
    enableWebGL2: true,
    disableCanvas2D: true,
    manifest: {
      engines: [
        {
          id: "gosx-engine-opaque-canvas",
          component: "GoSXScene3D",
          kind: "surface",
          mountId: "scene-opaque-canvas-root",
          props: {
            width: 640,
            height: 360,
            forceWebGL: true,
            background: "#02030a",
            camera: { z: 8 },
            points: [
              {
                id: "stars",
                count: 1,
                positions: [{ x: 0, y: 0, z: 0 }],
                sizes: [2],
                colors: ["#ffffff"],
              },
            ],
          },
        },
      ],
    },
  });

  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();

  const canvas = mount.children[0];
  const contextCall = canvas.contextCalls.find((call) => call.kind === "webgl2" && call.options && Object.prototype.hasOwnProperty.call(call.options, "alpha"));
  assert.ok(contextCall);
  assert.equal(contextCall.options.alpha, false);
  assert.equal(contextCall.options.premultipliedAlpha, false);
});

test("bootstrap preserves transparent WebGL canvas when Scene3D asks for alpha", async () => {
  const mount = new FakeElement("div", null);
  mount.id = "scene-alpha-canvas-root";

  const env = createContext({
    elements: [mount],
    enableWebGL2: true,
    disableCanvas2D: true,
    manifest: {
      engines: [
        {
          id: "gosx-engine-alpha-canvas",
          component: "GoSXScene3D",
          kind: "surface",
          mountId: "scene-alpha-canvas-root",
          props: {
            width: 640,
            height: 360,
            forceWebGL: true,
            canvasAlpha: true,
            background: "#02030a",
            camera: { z: 8 },
            points: [
              {
                id: "stars",
                count: 1,
                positions: [{ x: 0, y: 0, z: 0 }],
                sizes: [2],
                colors: ["#ffffff"],
              },
            ],
          },
        },
      ],
    },
  });

  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();

  const canvas = mount.children[0];
  const contextCall = canvas.contextCalls.find((call) => call.kind === "webgl2" && call.options && Object.prototype.hasOwnProperty.call(call.options, "alpha"));
  assert.ok(contextCall);
  assert.equal(contextCall.options.alpha, true);
  assert.equal(contextCall.options.premultipliedAlpha, true);
});

test("bootstrap GLB loader extracts skin attributes and evaluates animation joint matrices", async () => {
  const env = createContext({
    fetchRoutes: {
      "/models/rig.glb": {
        bytes: buildSkinnedGLBBytes(),
      },
    },
  });

  runScript(bootstrapSource, env.context, "bootstrap.js");
  const scene = await env.context.__gosx_scene3d_gltf_api.sceneLoadGLTFModel("/models/rig.glb");

  assert.equal(scene.objects.length, 1);
  assert.equal(scene.skins.length, 1);
  assert.equal(scene.nodes.length, 3);
  assert.equal(scene.animations.length, 1);

  const object = scene.objects[0];
  assert.equal(object.skin, scene.skins[0]);
  assert.equal(object.skinIndex, 0);
  assert.equal(object.vertices.count, 3);
  assert.equal(object.vertices.positions[0], 0);
  assert.equal(object.vertices.positions[1], 1);
  assert.deepEqual(Array.from(object.vertices.weights.slice(0, 8)), [1, 0, 0, 0, 0.75, 0.25, 0, 0]);
  assert.deepEqual(Array.from(object.vertices.joints.slice(0, 4)), [0, 1, 0, 0]);
  assert.equal(scene.animations[0].channels[0].targetID, 2);

  const animationApi = env.context.__gosx_scene3d_animation_api;
  const mixer = animationApi.createMixer();
  mixer.addClip(scene.animations[0].name, scene.animations[0]);
  mixer.play("bend", { loop: false, fadeIn: 0 });

  const animatedTransforms = new env.context.Map();
  mixer.update(0.5, function(targetNode, property, value) {
    animatedTransforms.set(targetNode, {
      [property]: Array.from(value),
    });
  });

  const nodeTransforms = animationApi.buildNodeTransforms(scene.nodes, animatedTransforms);
  const jointMatrices = animationApi.computeJointMatrices(scene.skins[0], nodeTransforms);
  assert.equal(jointMatrices.length, 32);
  assert.ok(Math.abs(jointMatrices[16 + 13] - 0.25) < 0.00001);

  const fallbackJointMatrices = animationApi.computeJointMatrices({
    joints: [0],
    inverseBindMatrices: new Float32Array(0),
  }, new env.context.Map());
  assert.equal(fallbackJointMatrices.length, 16);
  assert.equal(fallbackJointMatrices[0], 1);
  assert.equal(fallbackJointMatrices[5], 1);
  assert.equal(fallbackJointMatrices[10], 1);
  assert.equal(fallbackJointMatrices[15], 1);
  assert.equal(animationApi.computeJointMatrices(null, new env.context.Map()).length, 0);
  assert.equal(env.consoleLogs.error.length, 0);
});

test("bootstrap starts Scene3D GLB model animation playback from model props", async () => {
  const mount = new FakeElement("div", null);
  mount.id = "scene-model-skinned-root";

  const env = createContext({
    elements: [mount],
    fetchRoutes: {
      "/models/rig.glb": {
        bytes: buildSkinnedGLBBytes(),
      },
    },
    manifest: {
      engines: [
        {
          id: "gosx-engine-model-skinned",
          component: "GoSXScene3D",
          kind: "surface",
          mountId: "scene-model-skinned-root",
          props: {
            width: 640,
            height: 360,
            autoRotate: false,
            models: [
              {
                id: "rig",
                src: "/models/rig.glb",
                animation: "bend",
                animationSeq: "boot",
                animationSpeed: 1.5,
                animationWeight: 0.75,
                animationFadeInMS: 120,
                loop: true,
              },
            ],
          },
        },
      ],
    },
  });
  const raf = installManualRAF(env.context);

  runScript(bootstrapSource, env.context, "bootstrap.js");
  const animationApi = env.context.__gosx_scene3d_animation_api;
  const originalCreateMixer = animationApi.createMixer;
  const calls = [];
  animationApi.createMixer = function createObservedMixer(...args) {
    const mixer = originalCreateMixer.apply(this, args);
    const originalPlay = mixer.play.bind(mixer);
    mixer.play = function observedPlay(name, options) {
      calls.push(["play", name, options && {
        loop: options.loop,
        speed: options.speed,
        weight: options.weight,
        fadeIn: options.fadeIn,
      }]);
      return originalPlay(name, options);
    };
    return mixer;
  };
  await flushAsyncWork();
  await flushSceneInitialFrameBoundary(raf);

  assert.equal(env.fetchCalls.some((call) => call.url === "/models/rig.glb"), true);
  assert.equal(mount.getAttribute("data-gosx-scene3d-mounted"), "true");
  assert.equal(raf.count(), 1);
  assert.deepEqual(calls, [
    ["play", "bend", { loop: true, speed: 1.5, weight: 0.75, fadeIn: 0.12 }],
  ]);
  assert.equal(env.consoleLogs.error.length, 0);
});

test("bootstrap replays Scene3D GLB model animations when live sequence changes", async () => {
  const mount = new FakeElement("div", null);
  mount.id = "scene-model-live-animation-root";

  const env = createContext({
    elements: [mount],
    fetchRoutes: {
      "/models/rig.glb": {
        bytes: buildSkinnedGLBBytes(),
      },
    },
    manifest: {
      engines: [
        {
          id: "gosx-engine-model-live-animation",
          component: "GoSXScene3D",
          kind: "surface",
          mountId: "scene-model-live-animation-root",
          props: {
            width: 640,
            height: 360,
            autoRotate: false,
            models: [
              {
                id: "rig",
                src: "/models/rig.glb",
                animation: "bend",
                loop: true,
                live: ["attack"],
              },
            ],
          },
        },
      ],
    },
  });
  installManualRAF(env.context);

  runScript(bootstrapSource, env.context, "bootstrap.js");
  const animationApi = env.context.__gosx_scene3d_animation_api;
  const originalCreateMixer = animationApi.createMixer;
  const calls = [];
  animationApi.createMixer = function createObservedMixer(...args) {
    const mixer = originalCreateMixer.apply(this, args);
    const originalPlay = mixer.play.bind(mixer);
    const originalStop = mixer.stop.bind(mixer);
    mixer.play = function observedPlay(name, options) {
      calls.push(["play", name, options && {
        loop: options.loop,
        speed: options.speed,
        weight: options.weight,
        fadeIn: options.fadeIn,
      }]);
      return originalPlay(name, options);
    };
    mixer.stop = function observedStop(name, options) {
      calls.push(["stop", name, options && {
        fadeOut: options.fadeOut,
      }]);
      return originalStop(name, options);
    };
    return mixer;
  };
  await flushAsyncWork();

  env.document.dispatchEvent(new env.context.CustomEvent("gosx:hub:event", {
    detail: { event: "attack", data: { rig: { animation: "bend", animationSeq: "hit-1" } } },
  }));
  env.document.dispatchEvent(new env.context.CustomEvent("gosx:hub:event", {
    detail: { event: "attack", data: { rig: { animation: "bend", animationSeq: "hit-1" } } },
  }));
  env.document.dispatchEvent(new env.context.CustomEvent("gosx:hub:event", {
    detail: { event: "attack", data: { rig: {
      animation: "bend",
      animationSeq: "hit-2",
      animationSpeed: 1.75,
      animationWeight: 0.65,
      animationFadeInMS: 80,
      animationFadeOutMS: 60,
    } } },
  }));

  assert.deepEqual(calls, [
    ["play", "bend", { loop: true, speed: 1, weight: 1, fadeIn: 0 }],
    ["stop", "bend", { fadeOut: 0 }],
    ["play", "bend", { loop: true, speed: 1, weight: 1, fadeIn: 0 }],
    ["stop", "bend", { fadeOut: 0.06 }],
    ["play", "bend", { loop: true, speed: 1.75, weight: 0.65, fadeIn: 0.08 }],
  ]);
  assert.equal(env.consoleLogs.error.length, 0);
});

test("bootstrap starts rigged Scene3D GLB animation from a live event without initial playback", async () => {
  const mount = new FakeElement("div", null);
  mount.id = "scene-model-live-start-root";

  const env = createContext({
    elements: [mount],
    fetchRoutes: {
      "/models/rig.glb": {
        bytes: buildSkinnedGLBBytes(),
      },
    },
    manifest: {
      engines: [
        {
          id: "gosx-engine-model-live-start",
          component: "GoSXScene3D",
          kind: "surface",
          mountId: "scene-model-live-start-root",
          props: {
            width: 640,
            height: 360,
            autoRotate: false,
            models: [
              {
                id: "rig",
                src: "/models/rig.glb",
                loop: true,
                live: ["attack"],
              },
            ],
          },
        },
      ],
    },
  });
  installManualRAF(env.context);

  runScript(bootstrapSource, env.context, "bootstrap.js");
  const animationApi = env.context.__gosx_scene3d_animation_api;
  const originalCreateMixer = animationApi.createMixer;
  const calls = [];
  animationApi.createMixer = function createObservedMixer(...args) {
    const mixer = originalCreateMixer.apply(this, args);
    const originalPlay = mixer.play.bind(mixer);
    mixer.play = function observedPlay(name, options) {
      calls.push(["play", name, options && {
        loop: options.loop,
        speed: options.speed,
        weight: options.weight,
        fadeIn: options.fadeIn,
      }]);
      return originalPlay(name, options);
    };
    return mixer;
  };
  await flushAsyncWork();

  assert.deepEqual(calls, []);
  env.document.dispatchEvent(new env.context.CustomEvent("gosx:hub:event", {
    detail: { event: "attack", data: { rig: {
      animation: "bend",
      animationSeq: "opening-hit",
      animationSpeed: 1.4,
      animationWeight: 0.7,
      animationFadeInMS: 50,
    } } },
  }));

  assert.deepEqual(calls, [
    ["play", "bend", { loop: true, speed: 1.4, weight: 0.7, fadeIn: 0.05 }],
  ]);
  assert.equal(env.consoleLogs.error.length, 0);
});

test("bootstrap uploads skinned GLB joint matrices through WebGL PBR", async () => {
  const mount = new FakeElement("div", null);
  mount.id = "scene-model-skinned-webgl-root";

  const env = createContext({
    elements: [mount],
    enableWebGL2: true,
    disableCanvas2D: true,
    fetchRoutes: {
      "/models/rig.glb": {
        bytes: buildSkinnedGLBBytes(),
      },
    },
    manifest: {
      engines: [
        {
          id: "gosx-engine-model-skinned-webgl",
          component: "GoSXScene3D",
          kind: "surface",
          mountId: "scene-model-skinned-webgl-root",
          props: {
            width: 640,
            height: 360,
            autoRotate: false,
            models: [
              {
                id: "rig",
                src: "/models/rig.glb",
                animation: "bend",
                loop: true,
              },
            ],
          },
        },
      ],
    },
  });
  env.context.WebGL2RenderingContext = FakeWebGLContext;

  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();
  await flushAsyncWork();

  assert.equal(mount.getAttribute("data-gosx-scene3d-renderer"), "webgl");
  const gl = mount.children[0].getContext("webgl2");
  assert.ok(gl.ops.some((entry) => entry[0] === "getAttribLocation" && entry[1] === "a_joints"));
  assert.ok(gl.ops.some((entry) => entry[0] === "getAttribLocation" && entry[1] === "a_weights"));
  assert.ok(gl.ops.some((entry) => entry[0] === "uniform1i" && entry[1] === "u_hasSkin" && entry[2] === 1));
  assert.ok(gl.ops.some((entry) => entry[0] === "uniformMatrix4fv" && entry[1] === "u_modelMatrix" && entry[3] === 16));
  assert.ok(gl.ops.some((entry) => entry[0] === "uniformMatrix4fv" && entry[1] === "u_jointMatrices[0]" && entry[3] === 32));
  assert.ok(gl.ops.some((entry) => entry[0] === "vertexAttribPointer" && entry[1] === 7 && entry[2] === 4));
  assert.ok(gl.ops.some((entry) => entry[0] === "vertexAttribPointer" && entry[1] === 8 && entry[2] === 4));
  assert.equal(env.consoleLogs.error.length, 0);
});

test("bootstrap renders a skinned GLB through the Selena material (default flip keystone)", async () => {
  const mount = new FakeElement("div", null);
  mount.id = "scene-model-skinned-selena-webgl-root";

  const env = createContext({
    elements: [mount],
    enableWebGL2: true,
    disableCanvas2D: true,
    fetchRoutes: {
      "/models/rig.glb": {
        bytes: buildSkinnedGLBBytes(),
      },
    },
    manifest: {
      engines: [
        {
          id: "gosx-engine-model-skinned-selena-webgl",
          component: "GoSXScene3D",
          kind: "surface",
          mountId: "scene-model-skinned-selena-webgl-root",
          props: {
            width: 640,
            height: 360,
            autoRotate: false,
            models: [
              {
                id: "rig",
                src: "/models/rig.glb",
                animation: "bend",
                loop: true,
                shaderBackend: "selena",
                customVertex: SELENA_SKINNABLE_VERTEX_GLSL_FIXTURE,
                customFragment: SELENA_SKINNABLE_FRAGMENT_GLSL_FIXTURE,
                shaderLayout: SELENA_SKINNABLE_SHADER_LAYOUT_FIXTURE,
              },
            ],
          },
        },
      ],
    },
  });
  env.context.WebGL2RenderingContext = FakeWebGLContext;

  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();
  await flushAsyncWork();

  assert.equal(mount.getAttribute("data-gosx-scene3d-renderer"), "webgl");
  const gl = mount.children[0].getContext("webgl2");

  // "rimGain" is only ever looked up by sceneSelenaUniformLocations, so its
  // presence proves the SKINNED Selena program actually compiled and bound —
  // not merely that the object fell back to the standard skinned PBR program
  // (which shares the a_joints/a_weights/u_modelMatrix/u_hasSkin names but
  // never queries Selena's own uniform block fields).
  assert.ok(gl.ops.some((entry) => entry[0] === "getUniformLocation" && entry[1] === "rimGain"),
    "expected the skinned Selena program's rimGain uniform to be resolved");

  // The augmented vertex shader renames position/normal to a_position/a_normal
  // and adds the joint-skin attributes/uniforms alongside them.
  assert.ok(gl.ops.some((entry) => entry[0] === "getAttribLocation" && entry[1] === "a_position"));
  assert.ok(gl.ops.some((entry) => entry[0] === "getAttribLocation" && entry[1] === "a_normal"));
  assert.ok(gl.ops.some((entry) => entry[0] === "getAttribLocation" && entry[1] === "a_joints"));
  assert.ok(gl.ops.some((entry) => entry[0] === "getAttribLocation" && entry[1] === "a_weights"));
  assert.ok(gl.ops.some((entry) => entry[0] === "uniform1i" && entry[1] === "u_hasSkin" && entry[2] === 1));
  assert.ok(gl.ops.some((entry) => entry[0] === "uniformMatrix4fv" && entry[1] === "u_modelMatrix" && entry[3] === 16));
  assert.ok(gl.ops.some((entry) => entry[0] === "uniformMatrix4fv" && entry[1] === "u_jointMatrices[0]" && entry[3] === 32));
  assert.ok(gl.ops.some((entry) => entry[0] === "drawArrays"));

  // No fallback warning: the material compiled+augmented cleanly, so the
  // object drew with Selena rather than silently reverting to built-in PBR.
  assert.equal(env.consoleLogs.warn.length, 0, "expected no fallback warnings, got: " + JSON.stringify(env.consoleLogs.warn));
  assert.equal(env.consoleLogs.error.length, 0);
});

test("scenePBRSelenaSkinAugmentVertex renames position/normal, injects joint-skin GPU code, and validates as GLSL", () => {
  const webgl = fs.readFileSync(path.join(__dirname, "..", "runtime", "scene3d", "webgl.ts"), "utf8");
  const match = webgl.match(/function scenePBRSelenaSkinAugmentVertex\(source\)\s*\{([\s\S]*?)\n  \}/);
  assert.ok(match, "scenePBRSelenaSkinAugmentVertex must be extractable from 16-scene-webgl.js source");

  const fnSrc = "function scenePBRSelenaSkinAugmentVertex(source) {" + match[1] + "\n  }";
  const augment = new Function("return (" + fnSrc + ")")();

  // Non-skinnable shapes (no `position` attribute in the expected form) must
  // return null so the caller safely falls back to the standard PBR path.
  assert.equal(augment(""), null);
  assert.equal(augment("attribute vec2 pointUV;\nvoid main() {}"), null);

  const result = augment(SELENA_SKINNABLE_VERTEX_GLSL_FIXTURE);
  assert.ok(result && typeof result.source === "string");
  assert.equal(result.hasNormal, true);

  const out = result.source;
  // Original attributes renamed to the raw (bind-pose) slots.
  assert.match(out, /attribute vec3 a_position;/);
  assert.match(out, /attribute vec3 a_normal;/);
  assert.doesNotMatch(out, /attribute vec3 position;/);
  assert.doesNotMatch(out, /attribute vec3 normal;/);
  // New skin plumbing declared.
  assert.match(out, /attribute vec4 a_joints;/);
  assert.match(out, /attribute vec4 a_weights;/);
  assert.match(out, /uniform mat4 u_modelMatrix;/);
  assert.match(out, /uniform mat4 u_jointMatrices\[64\];/);
  assert.match(out, /uniform bool u_hasSkin;/);
  // Injected preamble computes WORLD-SPACE position/normal locals before the
  // untouched original body (which still does `mvp * vec4(position, 1.0)`
  // and `normalMatrix * normal` — both now resolve to the injected locals).
  assert.match(out, /vec3 position = selenaSkinWorldPos4\.xyz;/);
  assert.match(out, /vec3 normal = normalize\(mat3\(u_modelMatrix\) \* \(mat3\(selenaSkinMatrix\) \* a_normal\)\);/);
  assert.match(out, /gl_Position = \(mvp \* vec4\(position, 1\.0\)\);/);
  assert.match(out, /vWorldNormal = normalize\(\(normalMatrix \* normal\)\);/);

  // Real syntax/semantic validation of the augmented shader with glslang,
  // when available — the same oracle-grade check cmd/fightdiag uses (naga)
  // for the WGSL side. Skips gracefully in environments without the binary
  // rather than failing the suite on an unrelated tooling gap.
  const { spawnSync } = require("node:child_process");
  const which = spawnSync("which", ["glslangValidator"]);
  if (which.status === 0) {
    const os = require("node:os");
    const tmpFile = path.join(os.tmpdir(), "selena-skinned-fighter-" + process.pid + ".vert");
    fs.writeFileSync(tmpFile, out);
    try {
      const validated = spawnSync("glslangValidator", ["-S", "vert", tmpFile], { encoding: "utf8" });
      assert.equal(validated.status, 0,
        "augmented skinned Selena vertex shader failed glslangValidator: " + validated.stdout + validated.stderr);
    } finally {
      fs.unlinkSync(tmpFile);
    }
  }

  // A position-only material (no normal attribute) must still augment
  // (position skinning is the mandatory half); the normal block is skipped.
  const positionOnly = [
    "attribute vec3 position;",
    "uniform mat4 mvp;",
    "void main() {",
    "  gl_Position = mvp * vec4(position, 1.0);",
    "}",
  ].join("\n");
  const positionOnlyResult = augment(positionOnly);
  assert.ok(positionOnlyResult);
  assert.equal(positionOnlyResult.hasNormal, false);
  assert.doesNotMatch(positionOnlyResult.source, /vec3 normal = /);
});
