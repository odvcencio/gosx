"use strict";
// WebGL2 buffer reuse and invalidation, pass caches, native Scene3D engines,
// projected labels, renderer preference and the custom post-effect passes.
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
  FakeWebGLContext,
  FakeElement,
  createContext,
  installManualTimers,
  runScript,
  flushAsyncWork,
  CUSTOM_POST_TIME_LAYOUT_FIXTURE,
} = require("./runtime-test-harness.js");

test("bootstrap releases replaced static point WebGL buffers on live updates", async () => {
  const env = createContext({
    enableWebGL2: true,
    disableCanvas2D: true,
  });
  env.context.WebGL2RenderingContext = FakeWebGLContext;
  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();

  const api = env.context.__gosx_scene3d_api;
  const registry = api.sceneBackendRegistry;
  const backend = registry.select({
    webgl: true,
    webgl2: true,
    webgpu: false,
    canvas: false,
    canvas2d: false,
  });
  const canvas = env.document.createElement("canvas");
  canvas.width = 320;
  canvas.height = 180;
  const renderer = backend.create(canvas, { background: "#000000" }, { tier: "full" });
  assert.equal(renderer && renderer.type, "webgl-pbr");

  const point = {
    id: "stars",
    count: 2,
    positions: [0, 0, 0, 1, 0, 0],
    sizes: [1, 1],
    colors: ["#ffffff", "#88ccff"],
    style: "focus",
    size: 1,
    opacity: 1,
    blendMode: "additive",
    depthWrite: false,
    attenuation: true,
  };
  const bundle = {
    bundleVersion: api.SCENE_RENDER_BUNDLE_VERSION,
    background: "#000000",
    camera: { x: 0, y: 0, z: 6, fov: 72, near: 0.05, far: 128 },
    environment: {},
    points: [point],
    instancedMeshes: [],
    computeParticles: [],
    objects: [],
    meshObjects: [],
    materials: [],
    labels: [],
    sprites: [],
    lights: [],
    positions: new Float32Array(0),
    colors: new Float32Array(0),
    worldPositions: new Float32Array(0),
    worldColors: new Float32Array(0),
    worldLineWidths: new Float32Array(0),
    worldMeshPositions: new Float32Array(0),
    worldMeshColors: new Float32Array(0),
    worldMeshNormals: new Float32Array(0),
    worldMeshUVs: new Float32Array(0),
    worldMeshTangents: new Float32Array(0),
    vertexCount: 0,
    worldVertexCount: 0,
    postEffects: [],
  };
  const viewport = { cssWidth: 320, cssHeight: 180, pixelWidth: 320, pixelHeight: 180, pixelRatio: 1 };

  renderer.render(bundle, viewport);
  point.colors = ["#ff6677", "#66ffee"];
  point._cachedColors = null;
  renderer.render(bundle, viewport);

  const colorUploads = canvas.getContext("webgl2").ops.filter((entry) => (
    entry[0] === "bufferData" &&
    entry[3] === 8 &&
    entry[4] === canvas.getContext("webgl2").STATIC_DRAW
  ));
  const deletes = canvas.getContext("webgl2").ops.filter((entry) => entry[0] === "deleteBuffer");
  assert.equal(colorUploads.length, 2);
  assert.ok(deletes.some((entry) => entry[1] === colorUploads[0][2]));

  renderer.dispose();
});

test("bootstrap reuses static point WebGL buffers across transient point records", async () => {
  const env = createContext({
    enableWebGL2: true,
    disableCanvas2D: true,
  });
  env.context.WebGL2RenderingContext = FakeWebGLContext;
  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();

  const api = env.context.__gosx_scene3d_api;
  const registry = api.sceneBackendRegistry;
  const backend = registry.select({
    webgl: true,
    webgl2: true,
    webgpu: false,
    canvas: false,
    canvas2d: false,
  });
  const canvas = env.document.createElement("canvas");
  canvas.width = 320;
  canvas.height = 180;
  const renderer = backend.create(canvas, { background: "#000000" }, { tier: "full" });
  assert.equal(renderer && renderer.type, "webgl-pbr");

  const positions = new Float32Array([0, 0, 0, 1, 0, 0]);
  const sizes = new Float32Array([1, 1]);
  const colors = new Float32Array([1, 1, 1, 1, 0.5, 0.8, 1, 1]);
  const nextColors = new Float32Array([1, 0.4, 0.5, 1, 0.4, 1, 0.9, 1]);
  const viewport = { cssWidth: 320, cssHeight: 180, pixelWidth: 320, pixelHeight: 180, pixelRatio: 1 };

  function bundleWith(pointColors) {
    return {
      bundleVersion: api.SCENE_RENDER_BUNDLE_VERSION,
      background: "#000000",
      camera: { x: 0, y: 0, z: 6, fov: 72, near: 0.05, far: 128 },
      environment: {},
      points: [{
        id: "stars",
        count: 2,
        positions,
        sizes,
        colors: pointColors || colors,
        style: "focus",
        size: 1,
        opacity: 1,
        blendMode: "additive",
        depthWrite: false,
        attenuation: true,
      }],
      instancedMeshes: [],
      computeParticles: [],
      objects: [],
      meshObjects: [],
      materials: [],
      labels: [],
      sprites: [],
      lights: [],
      positions: new Float32Array(0),
      colors: new Float32Array(0),
      worldPositions: new Float32Array(0),
      worldColors: new Float32Array(0),
      worldLineWidths: new Float32Array(0),
      worldMeshPositions: new Float32Array(0),
      worldMeshColors: new Float32Array(0),
      worldMeshNormals: new Float32Array(0),
      worldMeshUVs: new Float32Array(0),
      worldMeshTangents: new Float32Array(0),
      vertexCount: 0,
      worldVertexCount: 0,
      postEffects: [],
    };
  }

  const gl = canvas.getContext("webgl2");
  const staticUploadCount = () => gl.ops.filter((entry) => entry[0] === "bufferData" && entry[4] === gl.STATIC_DRAW).length;
  const colorUploads = () => gl.ops.filter((entry) => (
    entry[0] === "bufferData" &&
    entry[3] === 8 &&
    entry[4] === gl.STATIC_DRAW
  ));

  renderer.render(bundleWith(colors), viewport);
  const firstUploadCount = staticUploadCount();
  const firstColorBufferID = colorUploads()[0][2];

  renderer.render(bundleWith(colors), viewport);
  renderer.render(bundleWith(colors), viewport);
  assert.equal(staticUploadCount(), firstUploadCount);

  renderer.render(bundleWith(nextColors), viewport);
  assert.equal(staticUploadCount(), firstUploadCount + 1);
  assert.ok(gl.ops.some((entry) => entry[0] === "deleteBuffer" && entry[1] === firstColorBufferID));

  const afterPaletteUploadCount = staticUploadCount();
  renderer.render(bundleWith(nextColors), viewport);
  assert.equal(staticUploadCount(), afterPaletteUploadCount);

  renderer.dispose();
});

test("bootstrap reuses static opaque Scene3D buffers across dynamic-only runtime updates", async () => {
  const mount = new FakeElement("div", null);
  mount.id = "scene-static-cache-root";
  let renderIndex = 0;

  const env = createContext({
    elements: [mount],
    enableWebGL: true,
    disableCanvas2D: true,
    fetchRoutes: {
      "/runtime.wasm": { bytes: [0, 97, 115, 109] },
      "/scene-static-cache-program.json": { text: '{"name":"StaticCache"}' },
    },
    manifest: {
      runtime: { path: "/runtime.wasm" },
      engines: [
        {
          id: "gosx-engine-static-cache",
          component: "GoSXScene3D",
          kind: "surface",
          mountId: "scene-static-cache-root",
          runtime: "shared",
          props: { width: 640, height: 360, background: "#08151f" },
          programRef: "/scene-static-cache-program.json",
        },
      ],
    },
    onHydrateEngine: () => "[]",
    onRenderEngine: () => {
      renderIndex += 1;
      const shieldZ = renderIndex === 1 ? 1 : 1.5;
      return JSON.stringify({
        background: "#08151f",
        camera: { x: 0, y: 0, z: 6, fov: 72, near: 0.05, far: 128 },
        positions: [],
        colors: [],
        vertexCount: 0,
        worldPositions: [
          -2, 0, 0, 2, 0, 0,
          -1, 0.5, shieldZ, 1, 0.5, shieldZ,
        ],
        worldColors: [
          0.3, 0.4, 0.5, 1, 0.3, 0.4, 0.5, 1,
          0.8, 0.95, 1, 1, 0.8, 0.95, 1, 1,
        ],
        worldVertexCount: 4,
        materials: [
          { kind: "flat", color: "#35556a", opacity: 1, wireframe: true, blendMode: "opaque", emissive: 0 },
          { kind: "glass", color: "#c7f0ff", opacity: 0.45, wireframe: true, blendMode: "alpha", emissive: 0.05 },
        ],
        objects: [
          {
            id: "floor",
            kind: "plane",
            materialIndex: 0,
            vertexOffset: 0,
            vertexCount: 2,
            static: true,
            bounds: { minX: -2, minY: 0, minZ: 0, maxX: 2, maxY: 0, maxZ: 0 },
            depthNear: 6,
            depthFar: 6,
            depthCenter: 6,
            viewCulled: false,
          },
          {
            id: "shield",
            kind: "plane",
            materialIndex: 1,
            vertexOffset: 2,
            vertexCount: 2,
            static: false,
            bounds: { minX: -1, minY: 0.5, minZ: shieldZ, maxX: 1, maxY: 0.5, maxZ: shieldZ },
            depthNear: 6 + shieldZ,
            depthFar: 6 + shieldZ,
            depthCenter: 6 + shieldZ,
            viewCulled: false,
          },
        ],
        objectCount: 2,
      });
    },
  });

  let rafCount = 0;
  // Allow four frame callbacks. Async shared-runtime mount setup can consume
  // the first wait before the first paint-boundary callback is queued.
  env.context.requestAnimationFrame = (callback) => {
    if (rafCount >= 4) return 0;
    rafCount += 1;
    return setTimeout(() => callback(rafCount * 16), 0);
  };
  env.context.cancelAnimationFrame = (handle) => clearTimeout(handle);

  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();
  await flushAsyncWork();

  const gl = mount.children[0].getContext("webgl");
  assert.ok(env.engineRenderCalls.length >= 2);
  assert.equal(gl.ops.filter((entry) => entry[0] === "bufferData" && entry[2] === 4).length, 1);
});

test("bootstrap invalidates static opaque Scene3D buffers when camera clip state changes", async () => {
  const mount = new FakeElement("div", null);
  mount.id = "scene-static-camera-root";
  let renderIndex = 0;

  const env = createContext({
    elements: [mount],
    enableWebGL: true,
    disableCanvas2D: true,
    fetchRoutes: {
      "/runtime.wasm": { bytes: [0, 97, 115, 109] },
      "/scene-static-camera-program.json": { text: '{"name":"StaticCamera"}' },
    },
    manifest: {
      runtime: { path: "/runtime.wasm" },
      engines: [
        {
          id: "gosx-engine-static-camera",
          component: "GoSXScene3D",
          kind: "surface",
          mountId: "scene-static-camera-root",
          runtime: "shared",
          props: { width: 640, height: 360, background: "#08151f" },
          programRef: "/scene-static-camera-program.json",
        },
      ],
    },
    onHydrateEngine: () => "[]",
    onRenderEngine: () => {
      renderIndex += 1;
      const cameraZ = renderIndex === 1 ? 6 : 5.5;
      return JSON.stringify({
        background: "#08151f",
        camera: { x: 0, y: 0, z: cameraZ, fov: 72, near: 0.05, far: 128 },
        positions: [],
        colors: [],
        vertexCount: 0,
        worldPositions: [
          -2, 0, 0, 2, 0, 0,
        ],
        worldColors: [
          0.3, 0.4, 0.5, 1, 0.3, 0.4, 0.5, 1,
        ],
        worldVertexCount: 2,
        materials: [
          { key: "flat|#35556a|1.000|true|opaque|opaque|0.000", kind: "flat", color: "#35556a", opacity: 1, wireframe: true, blendMode: "opaque", renderPass: "opaque", emissive: 0 },
        ],
        objects: [
          {
            id: "floor",
            kind: "plane",
            materialIndex: 0,
            vertexOffset: 0,
            vertexCount: 2,
            static: true,
            bounds: { minX: -2, minY: 0, minZ: 0, maxX: 2, maxY: 0, maxZ: 0 },
            depthNear: cameraZ,
            depthFar: cameraZ,
            depthCenter: cameraZ,
            viewCulled: false,
          },
        ],
        objectCount: 1,
      });
    },
  });

  let rafCount = 0;
  // Allow four frame callbacks. Async shared-runtime mount setup can consume
  // the first wait before the first paint-boundary callback is queued.
  env.context.requestAnimationFrame = (callback) => {
    if (rafCount >= 4) return 0;
    rafCount += 1;
    return setTimeout(() => callback(rafCount * 16), 0);
  };
  env.context.cancelAnimationFrame = (handle) => clearTimeout(handle);

  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();
  await flushAsyncWork();

  const gl = mount.children[0].getContext("webgl");
  assert.ok(env.engineRenderCalls.length >= 2);
  assert.equal(gl.ops.filter((entry) => entry[0] === "bufferData" && entry[2] === 4).length, 2);
});

test("bootstrap invalidates static opaque Scene3D buffers when shared-runtime lighting changes", async () => {
  const mount = new FakeElement("div", null);
  mount.id = "scene-static-lighting-root";
  let renderIndex = 0;

  const env = createContext({
    elements: [mount],
    enableWebGL: true,
    disableCanvas2D: true,
    fetchRoutes: {
      "/runtime.wasm": { bytes: [0, 97, 115, 109] },
      "/scene-static-lighting-program.json": { text: '{"name":"StaticLighting"}' },
    },
    manifest: {
      runtime: { path: "/runtime.wasm" },
      engines: [
        {
          id: "gosx-engine-static-lighting",
          component: "GoSXScene3D",
          kind: "surface",
          mountId: "scene-static-lighting-root",
          runtime: "shared",
          props: { width: 640, height: 360, background: "#08151f" },
          programRef: "/scene-static-lighting-program.json",
        },
      ],
    },
    onHydrateEngine: () => "[]",
    onRenderEngine: () => {
      renderIndex += 1;
      const warm = renderIndex === 1 ? 0.35 : 0.92;
      return JSON.stringify({
        background: "#08151f",
        camera: { x: 0, y: 0, z: 6, fov: 72, near: 0.05, far: 128 },
        positions: [],
        colors: [],
        vertexCount: 0,
        worldPositions: [
          -2, 0, 0, 2, 0, 0,
        ],
        worldColors: [
          warm, 0.42, 0.5, 1, warm, 0.42, 0.5, 1,
        ],
        worldVertexCount: 2,
        materials: [
          { key: "flat|#808080|1.000|true|opaque|opaque|0.000", kind: "flat", color: "#808080", opacity: 1, wireframe: true, blendMode: "opaque", renderPass: "opaque", emissive: 0 },
        ],
        objects: [
          {
            id: "hero",
            kind: "box",
            materialIndex: 0,
            vertexOffset: 0,
            vertexCount: 2,
            static: true,
            bounds: { minX: -2, minY: 0, minZ: 0, maxX: 2, maxY: 0, maxZ: 0 },
            depthNear: 6,
            depthFar: 6,
            depthCenter: 6,
            viewCulled: false,
          },
        ],
        objectCount: 1,
      });
    },
  });

  let rafCount = 0;
  // Allow four frame callbacks. Async shared-runtime mount setup can consume
  // the first wait before the first paint-boundary callback is queued.
  env.context.requestAnimationFrame = (callback) => {
    if (rafCount >= 4) return 0;
    rafCount += 1;
    return setTimeout(() => callback(rafCount * 16), 0);
  };
  env.context.cancelAnimationFrame = (handle) => clearTimeout(handle);

  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();
  await flushAsyncWork();

  const gl = mount.children[0].getContext("webgl");
  assert.ok(env.engineRenderCalls.length >= 2);
  assert.equal(gl.ops.filter((entry) => entry[0] === "bufferData" && entry[2] === 4).length, 2);
});

test("bootstrap prefers engine-batched Scene3D pass payloads when present", async () => {
  const mount = new FakeElement("div", null);
  mount.id = "scene-pass-bundle-root";

  const env = createContext({
    elements: [mount],
    enableWebGL: true,
    disableCanvas2D: true,
    fetchRoutes: {
      "/runtime.wasm": { bytes: [0, 97, 115, 109] },
      "/scene-pass-bundle-program.json": { text: '{"name":"PassBundle"}' },
    },
    manifest: {
      runtime: { path: "/runtime.wasm" },
      engines: [
        {
          id: "gosx-engine-pass-bundle",
          component: "GoSXScene3D",
          kind: "surface",
          mountId: "scene-pass-bundle-root",
          runtime: "shared",
          props: { width: 640, height: 360, background: "#08151f" },
          programRef: "/scene-pass-bundle-program.json",
        },
      ],
    },
    onHydrateEngine: () => "[]",
    onRenderEngine: () => JSON.stringify({
      background: "#08151f",
      camera: { x: 0, y: 0, z: 6, fov: 72 },
      positions: [],
      colors: [],
      vertexCount: 0,
      worldPositions: [
        -9, 0, 0, -8, 0, 0,
      ],
      worldColors: [
        1, 0, 0, 1, 1, 0, 0, 1,
      ],
      worldVertexCount: 2,
      materials: [
        { key: "flat|#35556a|1.000|true|opaque|opaque|0.000", kind: "flat", color: "#35556a", opacity: 1, wireframe: true, blendMode: "opaque", renderPass: "opaque", emissive: 0, shaderData: [0, 0, 1] },
      ],
      objects: [
        { id: "floor", kind: "plane", materialIndex: 0, renderPass: "opaque", vertexOffset: 0, vertexCount: 2, static: true, depthCenter: 6, viewCulled: false },
      ],
      passes: [
        {
          name: "staticOpaque",
          blend: "opaque",
          depth: "opaque",
          static: true,
          cacheKey: "engine-pass-key",
          positions: [1, 0, 0, 2, 0, 0],
          colors: [0.3, 0.4, 0.5, 1, 0.3, 0.4, 0.5, 1],
          materials: [0, 0, 1, 0, 0, 1],
          vertexCount: 2,
        },
      ],
    }),
  });

  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();

  const gl = mount.children[0].getContext("webgl");
  assert.deepEqual(gl.bufferUploads.get(4), [1, 0, 0, 2, 0, 0]);
});

test("legacy WebGL pass cache uploads first static pass when cacheKey is empty", () => {
  const legacyWebGL = fs.readFileSync(path.join(__dirname, "bootstrap-src", "16e-scene-webgl-legacy.js"), "utf8");
  assert.match(legacyWebGL, /key:\s*null,\s*\n\s*vertexCount:\s*0/);
  assert.match(legacyWebGL, /record\.key !== pass\.cacheKey \|\|/);
  assert.match(legacyWebGL, /record\.vertexCount !== vertexCount \|\|/);
  assert.match(legacyWebGL, /record\.positionByteLength !== positionByteLength \|\|/);
  assert.match(legacyWebGL, /sceneWebGLPassBufferTooSmall\(gl, arrayBuffer, pass\.buffers\.position, positionByteLength\)/);
});

test("bootstrap keeps static Scene3D bundle-pass caches isolated per pass", async () => {
  const mount = new FakeElement("div", null);
  mount.id = "scene-pass-cache-root";
  let renderIndex = 0;

  const env = createContext({
    elements: [mount],
    enableWebGL: true,
    disableCanvas2D: true,
    fetchRoutes: {
      "/runtime.wasm": { bytes: [0, 97, 115, 109] },
      "/scene-pass-cache-program.json": { text: '{"name":"PassCache"}' },
    },
    manifest: {
      runtime: { path: "/runtime.wasm" },
      engines: [
        {
          id: "gosx-engine-pass-cache",
          component: "GoSXScene3D",
          kind: "surface",
          mountId: "scene-pass-cache-root",
          runtime: "shared",
          props: { width: 640, height: 360, background: "#08151f" },
          programRef: "/scene-pass-cache-program.json",
        },
      ],
    },
    onHydrateEngine: () => "[]",
    onRenderEngine: () => {
      renderIndex += 1;
      return JSON.stringify({
        background: "#08151f",
        camera: { x: 0, y: 0, z: 6, fov: 72, near: 0.05, far: 128 },
        positions: [],
        colors: [],
        vertexCount: 0,
        worldPositions: [
          1, 0, 0, 2, 0, 0,
        ],
        worldColors: [
          0.3, 0.4, 0.5, 1, 0.3, 0.4, 0.5, 1,
        ],
        worldVertexCount: 2,
        materials: [],
        objects: [],
        objectCount: 0,
        passes: [
          {
            name: "staticOpaque",
            blend: "opaque",
            depth: "opaque",
            static: true,
            cacheKey: "shared-engine-pass-key",
            positions: [1, 0, 0, 2, 0, 0],
            colors: [0.3, 0.4, 0.5, 1, 0.3, 0.4, 0.5, 1],
            materials: [0, 0, 1, 0, 0, 1],
            vertexCount: 2,
          },
          {
            name: "alpha",
            blend: "alpha",
            depth: "translucent",
            static: true,
            cacheKey: "shared-engine-pass-key",
            positions: [-4, 0, 2, -3, 0, 2],
            colors: [0.9, 0.8, 0.5, 1, 0.9, 0.8, 0.5, 1],
            materials: [2, 0.05, 0.7, 2, 0.05, 0.7],
            vertexCount: 2,
          },
        ],
      });
    },
  });

  let rafCount = 0;
  // Allow four frame callbacks. Async shared-runtime mount setup can consume
  // the first wait before the first paint-boundary callback is queued.
  env.context.requestAnimationFrame = (callback) => {
    if (rafCount >= 4) return 0;
    rafCount += 1;
    return setTimeout(() => callback(rafCount * 16), 0);
  };
  env.context.cancelAnimationFrame = (handle) => clearTimeout(handle);

  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();
  await flushAsyncWork();

  const gl = mount.children[0].getContext("webgl");
  assert.ok(env.engineRenderCalls.length >= 2);
  assert.deepEqual(gl.bufferUploads.get(4), [1, 0, 0, 2, 0, 0]);
  assert.deepEqual(gl.bufferUploads.get(7), [-4, 0, 2, -3, 0, 2]);
  assert.equal(
    gl.ops.filter((entry) => entry[0] === "bufferData" && entry[4] === gl.STATIC_DRAW && (entry[2] === 4 || entry[2] === 7)).length,
    2,
  );
});

test("bootstrap clamps engine-batched Scene3D pass vertex counts to uploaded geometry", async () => {
  const mount = new FakeElement("div", null);
  mount.id = "scene-pass-clamp-root";

  const env = createContext({
    elements: [mount],
    enableWebGL: true,
    disableCanvas2D: true,
    fetchRoutes: {
      "/runtime.wasm": { bytes: [0, 97, 115, 109] },
      "/scene-pass-clamp-program.json": { text: '{"name":"PassClamp"}' },
    },
    manifest: {
      runtime: { path: "/runtime.wasm" },
      engines: [
        {
          id: "gosx-engine-pass-clamp",
          component: "GoSXScene3D",
          kind: "surface",
          mountId: "scene-pass-clamp-root",
          runtime: "shared",
          props: { width: 640, height: 360, background: "#08151f" },
          programRef: "/scene-pass-clamp-program.json",
        },
      ],
    },
    onHydrateEngine: () => "[]",
    onRenderEngine: () => JSON.stringify({
      background: "#08151f",
      camera: { x: 0, y: 0, z: 6, fov: 72 },
      positions: [],
      colors: [],
      vertexCount: 0,
      worldPositions: [
        1, 0, 0, 2, 0, 0,
      ],
      worldColors: [
        0.3, 0.4, 0.5, 1, 0.3, 0.4, 0.5, 1,
      ],
      worldVertexCount: 2,
      materials: [],
      objects: [],
      objectCount: 0,
      passes: [
        {
          name: "dynamicOpaque",
          blend: "opaque",
          depth: "opaque",
          positions: [1, 0, 0, 2, 0, 0],
          colors: [0.3, 0.4, 0.5, 1, 0.3, 0.4, 0.5, 1],
          materials: [0, 0, 1, 0, 0, 1],
          vertexCount: 99,
        },
      ],
    }),
  });

  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();

  const gl = mount.children[0].getContext("webgl");
  assert.ok(gl.ops.some((entry) => entry[0] === "drawArrays" && entry[3] === 2));
  assert.ok(!gl.ops.some((entry) => entry[0] === "drawArrays" && entry[3] === 99));
});

test("bootstrap reuses opaque Scene3D WebGL state transitions within a frame", async () => {
  const mount = new FakeElement("div", null);
  mount.id = "scene-opaque-state-root";

  const env = createContext({
    elements: [mount],
    enableWebGL: true,
    disableCanvas2D: true,
    fetchRoutes: {
      "/runtime.wasm": { bytes: [0, 97, 115, 109] },
      "/scene-opaque-state-program.json": { text: '{"name":"OpaqueState"}' },
    },
    manifest: {
      runtime: { path: "/runtime.wasm" },
      engines: [
        {
          id: "gosx-engine-opaque-state",
          component: "GoSXScene3D",
          kind: "surface",
          mountId: "scene-opaque-state-root",
          runtime: "shared",
          props: { width: 640, height: 360, background: "#08151f" },
          programRef: "/scene-opaque-state-program.json",
        },
      ],
    },
    onHydrateEngine: () => "[]",
    onRenderEngine: () => JSON.stringify({
      background: "#08151f",
      camera: { x: 0, y: 0, z: 6, fov: 72 },
      positions: [],
      colors: [],
      vertexCount: 0,
      worldPositions: [
        -2, 0, 0, 2, 0, 0,
      ],
      worldColors: [
        0.4, 0.5, 0.6, 1, 0.4, 0.5, 0.6, 1,
      ],
      worldVertexCount: 2,
      materials: [
        { kind: "flat", color: "#35556a", opacity: 1, wireframe: true, blendMode: "opaque", emissive: 0 },
      ],
      objects: [
        { id: "floor", kind: "plane", materialIndex: 0, vertexOffset: 0, vertexCount: 2, static: true, depthCenter: 6, viewCulled: false },
      ],
      objectCount: 1,
    }),
  });

  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();

  const gl = mount.children[0].getContext("webgl");
  assert.equal(gl.ops.filter((entry) => entry[0] === "disable" && entry[1] === gl.BLEND).length, 1);
  assert.equal(gl.ops.filter((entry) => entry[0] === "enable" && entry[1] === gl.DEPTH_TEST).length, 1);
  assert.equal(gl.ops.filter((entry) => entry[0] === "depthMask" && entry[1] === true).length, 1);
});

test("bootstrap depth-sorts alpha Scene3D objects before upload", async () => {
  const mount = new FakeElement("div", null);
  mount.id = "scene-alpha-root";

  const env = createContext({
    elements: [mount],
    enableWebGL: true,
    disableCanvas2D: true,
    fetchRoutes: {
      "/runtime.wasm": { bytes: [0, 97, 115, 109] },
      "/scene-alpha-program.json": { text: '{"name":"AlphaDepth"}' },
    },
    manifest: {
      runtime: { path: "/runtime.wasm" },
      engines: [
        {
          id: "gosx-engine-alpha",
          component: "GoSXScene3D",
          kind: "surface",
          mountId: "scene-alpha-root",
          runtime: "shared",
          props: { width: 640, height: 360, background: "#08151f" },
          programRef: "/scene-alpha-program.json",
        },
      ],
    },
    onHydrateEngine: () => "[]",
    onRenderEngine: () => JSON.stringify({
      background: "#08151f",
      camera: { x: 0, y: 0, z: 6, fov: 72 },
      positions: [],
      colors: [],
      vertexCount: 0,
      worldPositions: [
        4, 0, -2, 3, 0, -2,
        -4, 0, 2, -3, 0, 2,
      ],
      worldColors: [
        0.3, 0.6, 0.9, 1, 0.3, 0.6, 0.9, 1,
        0.9, 0.8, 0.5, 1, 0.9, 0.8, 0.5, 1,
      ],
      worldVertexCount: 4,
      materials: [
        { key: "glass|#c7f0ff|0.450|true|alpha|alpha|0.050", kind: "glass", color: "#c7f0ff", opacity: 0.45, wireframe: true, blendMode: "opaque", emissive: 0.05, shaderData: [2, 0.05, 0.7] },
      ],
      objects: [
        { id: "near-static", kind: "plane", materialIndex: 0, renderPass: "alpha", vertexOffset: 0, vertexCount: 2, static: true, depthCenter: 4 },
        { id: "far-dynamic", kind: "plane", materialIndex: 0, renderPass: "alpha", vertexOffset: 2, vertexCount: 2, static: false, depthCenter: 8 },
      ],
      objectCount: 2,
    }),
  });

  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();

  const gl = mount.children[0].getContext("webgl");
  assert.deepEqual(gl.bufferUploads.get(7), [
    -4, 0, 2, -3, 0, 2,
    4, 0, -2, 3, 0, -2,
  ]);
  assert.ok(gl.ops.some((entry) => entry[0] === "blendFunc" && entry[1] === gl.SRC_ALPHA && entry[2] === gl.ONE_MINUS_SRC_ALPHA));
  assert.ok(gl.ops.some((entry) => entry[0] === "drawArrays" && entry[3] === 4));
});

test("bootstrap uploads engine-clipped Scene3D segments directly", async () => {
  const mount = new FakeElement("div", null);
  mount.id = "scene-clip-root";

  const env = createContext({
    elements: [mount],
    enableWebGL: true,
    disableCanvas2D: true,
    fetchRoutes: {
      "/runtime.wasm": { bytes: [0, 97, 115, 109] },
      "/scene-clip-program.json": { text: '{"name":"NearClip"}' },
    },
    manifest: {
      runtime: { path: "/runtime.wasm" },
      engines: [
        {
          id: "gosx-engine-clip",
          component: "GoSXScene3D",
          kind: "surface",
          mountId: "scene-clip-root",
          runtime: "shared",
          props: { width: 640, height: 360, background: "#08151f" },
          programRef: "/scene-clip-program.json",
        },
      ],
    },
    onHydrateEngine: () => "[]",
    onRenderEngine: () => JSON.stringify({
      background: "#08151f",
      camera: { x: 0, y: 0, z: 6, fov: 72 },
      positions: [],
      colors: [],
      vertexCount: 0,
      worldPositions: [
        -1.475, 0, -5.95, 2, 0, 1,
      ],
      worldColors: [
        0.7, 0.9, 1, 1, 0.7, 0.9, 1, 1,
      ],
      worldVertexCount: 2,
      materials: [
        { kind: "flat", color: "#8de1ff", opacity: 1, wireframe: true, blendMode: "opaque", emissive: 0 },
      ],
      objects: [
        { id: "clip-line", kind: "line", materialIndex: 0, vertexOffset: 0, vertexCount: 2, static: true, depthCenter: 3.5 },
      ],
      objectCount: 1,
    }),
  });

  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();

  const gl = mount.children[0].getContext("webgl");
  const clipped = gl.bufferUploads.get(4);
  assert.equal(clipped.length, 6);
  assert.ok(Math.abs(clipped[0] + 1.475) < 0.001);
  assert.ok(Math.abs(clipped[1]) < 0.001);
  assert.ok(Math.abs(clipped[2] + 5.95) < 0.001);
  assert.deepEqual(clipped.slice(3), [2, 0, 1]);
  assert.ok(gl.ops.some((entry) => entry[0] === "drawArrays" && entry[3] === 2));
});

test("bootstrap honors engine-side Scene3D view-cull metadata", async () => {
  const mount = new FakeElement("div", null);
  mount.id = "scene-metadata-cull-root";

  const env = createContext({
    elements: [mount],
    enableWebGL: true,
    disableCanvas2D: true,
    fetchRoutes: {
      "/runtime.wasm": { bytes: [0, 97, 115, 109] },
      "/scene-metadata-cull-program.json": { text: '{"name":"MetadataCull"}' },
    },
    manifest: {
      runtime: { path: "/runtime.wasm" },
      engines: [
        {
          id: "gosx-engine-metadata-cull",
          component: "GoSXScene3D",
          kind: "surface",
          mountId: "scene-metadata-cull-root",
          runtime: "shared",
          props: { width: 640, height: 360, background: "#08151f" },
          programRef: "/scene-metadata-cull-program.json",
        },
      ],
    },
    onHydrateEngine: () => "[]",
    onRenderEngine: () => JSON.stringify({
      background: "#08151f",
      camera: { x: 0, y: 0, z: 6, fov: 72, near: 0.05, far: 128 },
      positions: [],
      colors: [],
      vertexCount: 0,
      worldPositions: [
        -1, 0, 0.5, 1, 0, 0.5,
      ],
      worldColors: [
        0.7, 0.9, 1, 1, 0.7, 0.9, 1, 1,
      ],
      worldVertexCount: 2,
      materials: [
        { kind: "flat", color: "#8de1ff", opacity: 1, wireframe: true, blendMode: "opaque", emissive: 0 },
      ],
      objects: [
        { id: "metadata-hidden", kind: "line", materialIndex: 0, vertexOffset: 0, vertexCount: 2, static: true, viewCulled: true, depthCenter: 6.5 },
      ],
      objectCount: 1,
    }),
  });

  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();

  const gl = mount.children[0].getContext("webgl");
  assert.deepEqual(gl.bufferUploads.get(4) || [], []);
  assert.equal(gl.ops.some((entry) => entry[0] === "drawArrays"), false);
});

test("bootstrap mounts native Scene3D engines without extra scripts", async () => {
  const mount = new FakeElement("div", null);
  mount.id = "scene-root";
  mount.appendChild(new FakeElement("p", null));

  const env = createContext({
    elements: [mount],
    manifest: {
      engines: [
        {
          id: "gosx-engine-2",
          component: "GoSXScene3D",
          kind: "surface",
          mountId: "scene-root",
          jsExport: "GoSXScene3D",
          props: {
            width: 640,
            height: 360,
            autoRotate: false,
            scene: {
              objects: [
                { kind: "cube", size: 1.5, x: 0, y: 0, z: 0, color: "#8de1ff" },
              ],
            },
          },
          capabilities: ["canvas", "animation"],
        },
      ],
    },
  });

  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();

  assert.equal(env.context.__gosx.ready, true);
  assert.equal(env.context.__gosx.engines.size, 1);
  assert.equal(mount.children.length, 2);
  assert.equal(mount.firstElementChild.tagName, "CANVAS");
  assert.equal(mount.firstElementChild.getAttribute("width"), "640");
  assert.equal(mount.firstElementChild.getAttribute("height"), "360");
  assert.equal(mount.children[1].getAttribute("data-gosx-scene3d-label-layer"), "true");
  assert.equal(mount.children[1].children.length, 0);

  env.context.__gosx_dispose_engine("gosx-engine-2");
  assert.equal(env.context.__gosx.engines.size, 0);
  assert.equal(mount.children.length, 0);
  assert.equal(env.consoleLogs.warn.length, 0);
  assert.equal(env.consoleLogs.error.length, 0);
});

test("bootstrap renders mixed native Scene3D primitives", async () => {
  const mount = new FakeElement("div", null);
  mount.id = "scene-primitives";

  const env = createContext({
    elements: [mount],
    manifest: {
      engines: [
        {
          id: "gosx-engine-3",
          component: "GoSXScene3D",
          kind: "surface",
          mountId: "scene-primitives",
          jsExport: "GoSXScene3D",
          props: {
            width: 520,
            height: 320,
            autoRotate: false,
            scene: {
              objects: [
                { kind: "box", width: 1.8, height: 1.2, depth: 1.1, x: -1.6, y: 0.1, z: -0.2, color: "#8de1ff" },
                { kind: "sphere", radius: 0.8, x: 0.2, y: 0.15, z: 0.6, color: "#ffd48f", segments: 10 },
                { kind: "pyramid", width: 1.4, height: 1.8, depth: 1.4, x: 1.9, y: -0.2, z: 0.4, color: "#b8ffb0" },
                { kind: "plane", width: 5.2, depth: 3.8, y: -1.6, z: 0.3, color: "#35556a" },
              ],
              labels: [
                {
                  id: "zoo-label",
                  text: "Geometry zoo\nBrowser-measured overlay copy",
                  x: 0.2,
                  y: 1.4,
                  z: 0.9,
                  maxWidth: 120,
                  whiteSpace: "pre-wrap",
                },
              ],
            },
          },
          capabilities: ["canvas", "animation"],
        },
      ],
    },
  });

  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();

  assert.equal(env.context.__gosx.ready, true);
  assert.equal(mount.children.length, 2);
  const canvas = mount.firstElementChild;
  assert.equal(canvas.tagName, "CANVAS");

  const ctx2d = canvas.getContext("2d");
  const strokeCount = ctx2d.ops.filter((entry) => entry[0] === "stroke").length;
  const lineCount = ctx2d.ops.filter((entry) => entry[0] === "lineTo").length;
  const labelLayer = mount.children[1];
  assert.equal(canvas.getAttribute("width"), "520");
  assert.equal(canvas.getAttribute("height"), "320");
  assert.equal(mount.getAttribute("data-gosx-scene3d-mounted"), "true");
  assert.equal(labelLayer.getAttribute("data-gosx-scene3d-label-layer"), "true");
  assert.equal(labelLayer.children.length, 1);
  assert.equal(labelLayer.children[0].getAttribute("data-gosx-text-layout-role"), "label");
  assert.equal(labelLayer.children[0].getAttribute("data-gosx-text-layout-surface"), "scene3d");
  assert.equal(labelLayer.children[0].getAttribute("data-gosx-text-layout-state"), "ready");
  assert.equal(labelLayer.children[0].getAttribute("data-gosx-scene-label-visibility"), "visible");
  assert.equal(labelLayer.children[0].children.length >= 2, true);
  assert.equal(labelLayer.children[0].textContent, "Geometry zooBrowser-measured overlay copy");
  assert.equal(env.context.__gosx.textLayout.read(labelLayer.children[0]).lineCount >= 2, true);
  assert.ok(lineCount >= 12);
  assert.ok(strokeCount >= 1);
  assert.equal(env.consoleLogs.warn.length, 0);
  assert.equal(env.consoleLogs.error.length, 0);
});

test("bootstrap exposes read-only Scene3D debug API for mounted surfaces", async () => {
  const mount = new FakeElement("div", null);
  mount.id = "scene-debug-root";

  const env = createContext({
    elements: [mount],
    manifest: {
      engines: [
        {
          id: "gosx-engine-debug",
          component: "GoSXScene3D",
          kind: "surface",
          mountId: "scene-debug-root",
          jsExport: "GoSXScene3D",
          props: {
            width: 480,
            height: 300,
            autoRotate: false,
            scene: {
              objects: [
                { id: "debug-cube", kind: "cube", size: 1, color: "#8de1ff" },
              ],
              html: [
                {
                  id: "debug-panel",
                  target: "debug-cube",
                  mode: "texture",
                  html: "<p>Debug</p>",
                  fallback: "Debug",
                  textureWidth: 64,
                  textureHeight: 64,
                },
              ],
              lights: [
                { id: "debug-sun", kind: "directional", intensity: 1 },
              ],
              postEffects: [
                { kind: "bloom", threshold: 0.9 },
              ],
            },
          },
          capabilities: ["canvas", "animation"],
        },
      ],
    },
  });

  env.context.__gosx_scene3d_inspector = true;
  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();
  await flushAsyncWork();

  const api = env.context.__gosx_scene3d_debug;
  assert.ok(api, "expected Scene3D debug API");
  assert.equal(api.schema, "gosx.scene3d.debug.v1");

  const surfaces = api.listSurfaces();
  assert.equal(surfaces.length, 1);
  assert.equal(surfaces[0].id, "scene-debug-root");
  assert.equal(surfaces[0].engineID, "gosx-engine-debug");
  assert.equal(surfaces[0].renderer, mount.getAttribute("data-gosx-scene3d-renderer"));
  assert.equal(surfaces[0].ready, true);
  assert.ok(surfaces[0].features["geometry.cube"] >= 1);
  assert.equal(surfaces[0].features["html.texture"], 1);
  assert.equal(surfaces[0].features["postfx.bloom"], 1);

  const report = api.inspect("gosx-engine-debug");
  assert.equal(report.mountID, "scene-debug-root");
  assert.equal(report.counts.html, 1);
  assert.equal(report.gpuResources.canvas.width, 480);
  assert.equal(report.gpuResources.canvas.height, 300);
  assert.deepEqual(JSON.parse(JSON.stringify(report.waterShaderSources)), { sceneState: [], bundle: [] });
  assert.equal(report.diagnostics[0].code, "scene.backend.selected");
  assert.equal(report.renderLoop.active, false);
  assert.equal(report.renderLoop.reason, "static");
  assert.equal(mount.getAttribute("data-gosx-scene3d-render-loop"), "stopped");
  assert.equal(mount.getAttribute("data-gosx-scene3d-render-loop-reason"), "static");

  assert.deepEqual(api.getFeatureMatrix("scene-debug-root"), report.features);
  assert.equal(api.getGPUResources("scene-debug-root").canvas.width, 480);
  assert.equal(api.getDiagnostics("scene-debug-root")[0].code, "scene.backend.selected");
  assert.equal(api.getLastPick("scene-debug-root"), null);
  assert.equal(api.captureFrame("scene-debug-root").reason, "capture-unavailable");

  const inspector = mount.children.find((child) => child.getAttribute("data-gosx-scene3d-inspector") === "true");
  assert.ok(inspector, "expected opt-in Scene3D inspector overlay");
  assert.equal(mount.getAttribute("data-gosx-scene3d-inspector-enabled"), "true");
  assert.match(inspector.textContent, /Scene3D/);
  assert.match(inspector.textContent, /backend/);
  assert.match(inspector.textContent, /loop stopped/);
  assert.match(inspector.textContent, /draw/);
  assert.match(inspector.textContent, /html 1/);

  env.context.__gosx_dispose_engine("gosx-engine-debug");
  assert.equal(api.listSurfaces().length, 0);
  assert.equal(mount.children.some((child) => child.getAttribute("data-gosx-scene3d-inspector") === "true"), false);
});

test("bootstrap routes native Scene3D material profiles through WebGL pass planning", async () => {
  const mount = new FakeElement("div", null);
  mount.id = "scene-native-materials";

  const env = createContext({
    elements: [mount],
    enableWebGL: true,
    disableCanvas2D: true,
    manifest: {
      engines: [
        {
          id: "gosx-engine-native-materials",
          component: "GoSXScene3D",
          kind: "surface",
          mountId: "scene-native-materials",
          jsExport: "GoSXScene3D",
          props: {
            width: 640,
            height: 360,
            background: "#08151f",
            camera: { x: 0, y: 0, z: 6, near: 0.05, far: 128, fov: 72 },
            scene: {
              objects: [
                {
                  id: "floor",
                  kind: "plane",
                  width: 6.2,
                  depth: 4.8,
                  y: -1.8,
                  z: 0.3,
                  color: "#35556a",
                  materialKind: "flat",
                },
                {
                  id: "glass-orb",
                  kind: "sphere",
                  radius: 0.82,
                  x: -1.35,
                  y: 0.2,
                  z: 0.85,
                  color: "#c7f0ff",
                  materialKind: "glass",
                  opacity: 0.45,
                  emissive: 0.05,
                },
                {
                  id: "glow-orb",
                  kind: "sphere",
                  radius: 0.74,
                  x: 1.45,
                  y: 0.46,
                  z: 1.62,
                  color: "#8de1ff",
                  materialKind: "glow",
                  opacity: 0.72,
                  emissive: 0.4,
                },
              ],
            },
          },
          capabilities: ["webgl", "animation"],
        },
      ],
    },
  });

  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();

  const gl = mount.children[0].getContext("webgl");
  assert.ok(!gl.bufferUploads.has(1));
  assert.ok(Array.isArray(gl.bufferUploads.get(4)) && gl.bufferUploads.get(4).length > 0);
  assert.ok(Array.isArray(gl.bufferUploads.get(7)) && gl.bufferUploads.get(7).length > 0);
  assert.ok(Array.isArray(gl.bufferUploads.get(10)) && gl.bufferUploads.get(10).length > 0);
  assert.ok(gl.ops.some((entry) => entry[0] === "blendFunc" && entry[1] === gl.SRC_ALPHA && entry[2] === gl.ONE_MINUS_SRC_ALPHA));
  assert.ok(gl.ops.some((entry) => entry[0] === "blendFunc" && entry[1] === gl.SRC_ALPHA && entry[2] === gl.ONE));
  assert.ok(gl.ops.filter((entry) => entry[0] === "drawArrays" && entry[3] > 0).length >= 3);
  assert.equal(env.consoleLogs.warn.length, 0);
  assert.equal(env.consoleLogs.error.length, 0);
});

test("bootstrap tints native Scene3D geometry with declarative lights and environment", async () => {
  const mount = new FakeElement("div", null);
  mount.id = "scene-native-lighting";

  const env = createContext({
    elements: [mount],
    enableWebGL: true,
    disableCanvas2D: true,
    manifest: {
      engines: [
        {
          id: "gosx-engine-native-lighting",
          component: "GoSXScene3D",
          kind: "surface",
          mountId: "scene-native-lighting",
          jsExport: "GoSXScene3D",
          props: {
            width: 640,
            height: 360,
            background: "#08151f",
            camera: { x: 0, y: 0, z: 6, near: 0.05, far: 128, fov: 72 },
            scene: {
              environment: {
                ambientColor: "#f4fbff",
                ambientIntensity: 0.14,
                skyColor: "#b9deff",
                skyIntensity: 0.12,
                groundColor: "#102030",
                groundIntensity: 0.04,
              },
              lights: [
                {
                  id: "sun",
                  kind: "directional",
                  color: "#fff1d6",
                  intensity: 1.25,
                  directionX: 0.3,
                  directionY: -1,
                  directionZ: -0.35,
                },
              ],
              objects: [
                {
                  id: "hero",
                  kind: "box",
                  width: 1.8,
                  height: 1.2,
                  depth: 1.2,
                  color: "#808080",
                  materialKind: "flat",
                },
              ],
            },
          },
          capabilities: ["webgl", "animation"],
        },
      ],
    },
  });

  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();

  const gl = mount.children[0].getContext("webgl");
  const uploadedColors = gl.bufferUploads.get(5);
  assert.ok(Array.isArray(uploadedColors) && uploadedColors.length > 0);
  assert.ok(uploadedColors[0] > uploadedColors[2]);
  assert.equal(env.consoleLogs.warn.length, 0);
  assert.equal(env.consoleLogs.error.length, 0);
});

test("bootstrap respects static Scene3D camera clip props for label projection", async () => {
  const mount = new FakeElement("div", null);
  mount.id = "scene-camera-clip";

  const env = createContext({
    elements: [mount],
    manifest: {
      engines: [
        {
          id: "gosx-engine-camera-clip",
          component: "GoSXScene3D",
          kind: "surface",
          mountId: "scene-camera-clip",
          jsExport: "GoSXScene3D",
          props: {
            width: 520,
            height: 320,
            autoRotate: false,
            camera: { x: 0, y: 0, z: 6, fov: 72, near: 4, far: 5 },
            scene: {
              labels: [
                {
                  id: "clipped-label",
                  text: "Too near",
                  x: -0.5,
                  y: 0.3,
                  z: 0,
                  maxWidth: 96,
                },
                {
                  id: "visible-label",
                  text: "Visible depth",
                  x: 0.5,
                  y: 0.6,
                  z: 1.5,
                  maxWidth: 120,
                },
              ],
            },
          },
          capabilities: ["canvas"],
        },
      ],
    },
  });

  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();

  const labelLayer = mount.children[1];
  assert.equal(labelLayer.children.length, 1);
  assert.equal(labelLayer.children[0].getAttribute("data-gosx-scene-label"), "visible-label");
  assert.equal(labelLayer.children[0].textContent, "Visible depth");
});

test("bootstrap gives Scene3D labels a shared text-layout CSS contract and custom classes", async () => {
  const mount = new FakeElement("div", null);
  mount.id = "scene-label-contract";

  const env = createContext({
    elements: [mount],
    manifest: {
      engines: [
        {
          id: "gosx-engine-label-contract",
          component: "GoSXScene3D",
          kind: "surface",
          mountId: "scene-label-contract",
          jsExport: "GoSXScene3D",
          props: {
            width: 520,
            height: 320,
            autoRotate: false,
            scene: {
              labels: [
                {
                  id: "hero-chip",
                  className: "hero-chip tone-accent",
                  text: "supercalifragilisticgosx",
                  x: 0,
                  y: 0.8,
                  z: 0.2,
                  maxWidth: 72,
                  maxLines: 1,
                  overflow: "ellipsis",
                  priority: 3,
                },
              ],
            },
          },
          capabilities: ["canvas", "animation"],
        },
      ],
    },
  });

  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();

  const label = mount.children[1].children[0];
  assert.equal(label.getAttribute("class"), "gosx-scene-label hero-chip tone-accent");
  assert.equal(label.getAttribute("data-gosx-text-layout-role"), "label");
  assert.equal(label.getAttribute("data-gosx-text-layout-surface"), "scene3d");
  assert.equal(label.getAttribute("data-gosx-scene-label-priority"), "3");
  assert.equal(label.getAttribute("data-gosx-scene-label-collision"), "avoid");
  assert.equal(label.getAttribute("data-gosx-scene-label-visibility"), "visible");
  assert.equal(label.getAttribute("data-gosx-text-layout-overflow"), "ellipsis");
  assert.equal(typeof env.context.__gosx.textLayout.read(label).lineCount, "number");
});

test("bootstrap hides lower-priority Scene3D labels when collision avoidance overlaps", async () => {
  const mount = new FakeElement("div", null);
  mount.id = "scene-label-collision";

  const env = createContext({
    elements: [mount],
    manifest: {
      engines: [
        {
          id: "gosx-engine-label-collision",
          component: "GoSXScene3D",
          kind: "surface",
          mountId: "scene-label-collision",
          jsExport: "GoSXScene3D",
          props: {
            width: 520,
            height: 320,
            autoRotate: false,
            scene: {
              labels: [
                {
                  id: "primary-label",
                  text: "Primary label",
                  x: 0,
                  y: 0.4,
                  z: 0.2,
                  maxWidth: 132,
                  priority: 5,
                },
                {
                  id: "secondary-label",
                  text: "Secondary label",
                  x: 0,
                  y: 0.4,
                  z: 0.2,
                  maxWidth: 132,
                  priority: 1,
                },
              ],
            },
          },
          capabilities: ["canvas", "animation"],
        },
      ],
    },
  });

  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();

  const labelLayer = mount.children[1];
  assert.equal(labelLayer.children.length, 2);
  const primary = labelLayer.children[0].getAttribute("data-gosx-scene-label") === "primary-label" ? labelLayer.children[0] : labelLayer.children[1];
  const secondary = primary === labelLayer.children[0] ? labelLayer.children[1] : labelLayer.children[0];
  assert.equal(primary.getAttribute("data-gosx-scene-label-visibility"), "visible");
  assert.equal(secondary.getAttribute("data-gosx-scene-label-visibility"), "hidden");
});

test("bootstrap marks occluded Scene3D labels when scene geometry covers their anchor", async () => {
  const mount = new FakeElement("div", null);
  mount.id = "scene-label-occlusion";

  const env = createContext({
    elements: [mount],
    manifest: {
      engines: [
        {
          id: "gosx-engine-label-occlusion",
          component: "GoSXScene3D",
          kind: "surface",
          mountId: "scene-label-occlusion",
          jsExport: "GoSXScene3D",
          props: {
            width: 520,
            height: 320,
            autoRotate: false,
            scene: {
              objects: [
                { kind: "box", width: 2.8, height: 2.2, depth: 2.2, x: 0, y: 0, z: 0.2, color: "#8de1ff" },
              ],
              labels: [
                {
                  id: "occluded-label",
                  text: "Covered label",
                  x: 0,
                  y: 0,
                  z: 0.2,
                  maxWidth: 140,
                  offsetY: 0,
                  occlude: true,
                },
              ],
            },
          },
          capabilities: ["canvas", "animation"],
        },
      ],
    },
  });

  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();

  const label = mount.children[1].children[0];
  assert.equal(label.getAttribute("data-gosx-scene-label-occluded"), "true");
  assert.equal(label.getAttribute("data-gosx-scene-label-visibility"), "hidden");
});

test("bootstrap prefers WebGL Scene3D rendering when available", async () => {
  const mount = new FakeElement("div", null);
  mount.id = "scene-webgl";

  const env = createContext({
    elements: [mount],
    enableWebGL: true,
    disableCanvas2D: true,
    manifest: {
      engines: [
        {
          id: "gosx-engine-webgl",
          component: "GoSXScene3D",
          kind: "surface",
          mountId: "scene-webgl",
          jsExport: "GoSXScene3D",
          props: {
            width: 480,
            height: 300,
            autoRotate: false,
            scene: {
              objects: [
                { kind: "box", width: 1.4, height: 1.1, depth: 1.2, x: -0.8, y: 0, z: 0, color: "#8de1ff" },
                { kind: "sphere", radius: 0.7, x: 1.1, y: 0.2, z: 0.8, color: "#ffd48f" },
              ],
            },
          },
          capabilities: ["canvas", "webgl", "animation"],
        },
      ],
    },
  });

  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();

  const canvas = mount.firstElementChild;
  assert.equal(canvas.tagName, "CANVAS");
  assert.equal(mount.getAttribute("data-gosx-scene3d-renderer"), "webgl");

  const gl = canvas.getContext("webgl");
  assert.ok(gl.ops.some((entry) => entry[0] === "bufferData" && entry[3] > 0));
  assert.ok(gl.ops.some((entry) => entry[0] === "drawArrays" && entry[3] > 0));
  assert.equal(env.consoleLogs.warn.length, 0);
  assert.equal(env.consoleLogs.error.length, 0);
});

test("bootstrap prefers canvas Scene3D rendering on software WebGL backends", async () => {
  const mount = new FakeElement("div", null);
  mount.id = "scene-software-webgl";

  const env = createContext({
    elements: [mount],
    enableWebGL: true,
    manifest: {
      engines: [
        {
          id: "gosx-engine-software-webgl",
          component: "GoSXScene3D",
          kind: "surface",
          mountId: "scene-software-webgl",
          jsExport: "GoSXScene3D",
          props: {
            width: 480,
            height: 300,
            autoRotate: false,
            scene: {
              objects: [
                { kind: "box", width: 1.4, height: 1.1, depth: 1.2, x: 0, y: 0, z: 0, color: "#8de1ff" },
              ],
            },
          },
          capabilities: ["canvas", "webgl", "animation"],
        },
      ],
    },
    createWebGLContext: () => new FakeWebGLContext({
      vendor: "Google Inc. (Google)",
      renderer: "ANGLE (Google, Vulkan 1.3.0 (SwiftShader Device (Subzero) (0x0000C0DE)), SwiftShader driver)",
    }),
  });

  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();

  assert.equal(mount.getAttribute("data-gosx-scene3d-capability-tier"), "constrained");
  assert.equal(mount.getAttribute("data-gosx-scene3d-low-power"), "true");
  assert.equal(mount.getAttribute("data-gosx-scene3d-software-webgl"), "true");
  assert.equal(mount.getAttribute("data-gosx-scene3d-webgl-preference"), "avoid");
  assert.equal(mount.getAttribute("data-gosx-scene3d-renderer"), "canvas");
  assert.equal(mount.getAttribute("data-gosx-scene3d-renderer-fallback"), "environment-constrained");
});

// --- WEBGL_debug_renderer_info: only query when the plain gl.VENDOR/
// gl.RENDERER came back masked/empty — Firefox logs a deprecation warning
// on every getExtension("WEBGL_debug_renderer_info") call, even when the
// caller never uses the result. ---
test("bootstrap never queries WEBGL_debug_renderer_info when the plain gl.VENDOR/gl.RENDERER already return real strings", async () => {
  const mount = new FakeElement("div", null);
  mount.id = "scene-webgl-unmasked-renderer";
  let glInstance = null;

  const env = createContext({
    elements: [mount],
    enableWebGL: true,
    manifest: {
      engines: [
        {
          id: "gosx-engine-webgl-unmasked-renderer",
          component: "GoSXScene3D",
          kind: "surface",
          mountId: "scene-webgl-unmasked-renderer",
          jsExport: "GoSXScene3D",
          props: {
            width: 480,
            height: 300,
            autoRotate: false,
            scene: {
              objects: [
                { kind: "box", width: 1.4, height: 1.1, depth: 1.2, x: 0, y: 0, z: 0, color: "#8de1ff" },
              ],
            },
          },
          capabilities: ["canvas", "webgl", "animation"],
        },
      ],
    },
    createWebGLContext: () => {
      glInstance = new FakeWebGLContext({
        vendor: "Google Inc. (Google)",
        renderer: "ANGLE (Google, Vulkan 1.3.0 (SwiftShader Device (Subzero) (0x0000C0DE)), SwiftShader driver)",
      });
      return glInstance;
    },
  });

  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();

  assert.ok(glInstance, "expected a WebGL context to have been created");
  assert.equal(glInstance.debugRendererInfoRequests, 0,
    "the plain gl.VENDOR/gl.RENDERER query already returned real strings — WEBGL_debug_renderer_info must never be requested");
  // The plain-query path alone must still correctly detect a software
  // (SwiftShader) renderer — proving the fallback isn't needed for
  // correctness here, only for masked/older engines.
  assert.equal(mount.getAttribute("data-gosx-scene3d-software-webgl"), "true");
});

test("bootstrap falls back to WEBGL_debug_renderer_info only when the plain query is masked, and uses its unmasked result", async () => {
  const mount = new FakeElement("div", null);
  mount.id = "scene-webgl-masked-renderer";
  let glInstance = null;

  const env = createContext({
    elements: [mount],
    enableWebGL: true,
    manifest: {
      engines: [
        {
          id: "gosx-engine-webgl-masked-renderer",
          component: "GoSXScene3D",
          kind: "surface",
          mountId: "scene-webgl-masked-renderer",
          jsExport: "GoSXScene3D",
          props: {
            width: 480,
            height: 300,
            autoRotate: false,
            scene: {
              objects: [
                { kind: "box", width: 1.4, height: 1.1, depth: 1.2, x: 0, y: 0, z: 0, color: "#8de1ff" },
              ],
            },
          },
          capabilities: ["canvas", "webgl", "animation"],
        },
      ],
    },
    createWebGLContext: () => {
      glInstance = new FakeWebGLContext({
        // Plain gl.VENDOR/gl.RENDERER masked, exactly like a browser that
        // hasn't unmasked the plain query (e.g. privacy.resistFingerprinting).
        vendor: "Mozilla",
        renderer: "Generic Renderer",
        // Real strings, only revealed through WEBGL_debug_renderer_info's
        // UNMASKED_VENDOR_WEBGL/UNMASKED_RENDERER_WEBGL.
        unmaskedVendor: "Google Inc. (Google)",
        unmaskedRenderer: "ANGLE (Google, Vulkan 1.3.0 (SwiftShader Device (Subzero) (0x0000C0DE)), SwiftShader driver)",
      });
      return glInstance;
    },
  });

  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();

  assert.ok(glInstance, "expected a WebGL context to have been created");
  assert.equal(glInstance.debugRendererInfoRequests, 1,
    "a masked plain query must fall back to WEBGL_debug_renderer_info exactly once");
  // Detection must reflect the UNMASKED (real) renderer string, not the
  // masked "Generic Renderer" placeholder.
  assert.equal(mount.getAttribute("data-gosx-scene3d-software-webgl"), "true");
});

test("bootstrap requires WebGL for Scene3D when requested", async () => {
  const mount = new FakeElement("div", null);
  mount.id = "scene-required-webgl";

  const env = createContext({
    elements: [mount],
    manifest: {
      engines: [
        {
          id: "gosx-engine-required-webgl",
          component: "GoSXScene3D",
          kind: "surface",
          mountId: "scene-required-webgl",
          jsExport: "GoSXScene3D",
          props: {
            width: 480,
            height: 300,
            autoRotate: false,
            requireWebGL: true,
            unsupportedMessage: "Update your browser or enable hardware acceleration.",
            scene: {
              objects: [
                { kind: "box", width: 1.4, height: 1.1, depth: 1.2, x: 0, y: 0, z: 0, color: "#8de1ff" },
              ],
            },
          },
          capabilities: ["canvas", "webgl", "animation"],
        },
      ],
    },
  });

  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();

  assert.equal(mount.getAttribute("data-gosx-scene3d-require-webgl"), "true");
  assert.equal(mount.getAttribute("data-gosx-scene3d-renderer"), "unsupported");
  assert.equal(mount.getAttribute("data-gosx-scene3d-renderer-fallback"), "webgl-required");
  assert.equal(mount.children.length, 1);
  assert.equal(mount.children[0].getAttribute("data-gosx-scene3d-unsupported"), "true");
  assert.equal(mount.children[0].textContent, "Update your browser or enable hardware acceleration.");
});

test("Scene3D water unsupported state does not claim a rendered mount", async () => {
  const mount = new FakeElement("div", null);
  mount.id = "scene-water-unsupported";

  const env = createContext({
    elements: [mount],
    disableCanvas2D: true,
    manifest: {
      engines: [
        {
          id: "gosx-engine-water-unsupported",
          component: "GoSXScene3D",
          kind: "surface",
          mountId: "scene-water-unsupported",
          jsExport: "GoSXScene3D",
          props: {
            width: 480,
            height: 300,
            scene: {
              backendCaps: { capable: ["webgl"] },
              waterSystems: [
                { id: "pool", kind: "pool", width: 4, height: 2, length: 4 },
              ],
            },
          },
          capabilities: ["canvas", "webgl", "animation"],
        },
      ],
    },
  });

  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();

  assert.equal(mount.getAttribute("data-gosx-scene3d-renderer"), "unsupported");
  assert.equal(mount.getAttribute("data-gosx-scene3d-water-renderer"), "unsupported");
  assert.equal(mount.getAttribute("data-gosx-scene3d-water-unsupported-reason"), "water-webgl2-unavailable");
  assert.equal(mount.getAttribute("data-gosx-scene3d-ready"), "false");
  assert.equal(mount.getAttribute("data-gosx-scene3d-mounted"), null);
  assert.equal(mount.querySelector("[data-gosx-scene3d-unsupported]") != null, true);
});

test("bootstrap honors required WebGL over software-renderer canvas preference", async () => {
  const mount = new FakeElement("div", null);
  mount.id = "scene-required-software-webgl";

  const env = createContext({
    elements: [mount],
    enableWebGL: true,
    disableCanvas2D: true,
    manifest: {
      engines: [
        {
          id: "gosx-engine-required-software-webgl",
          component: "GoSXScene3D",
          kind: "surface",
          mountId: "scene-required-software-webgl",
          jsExport: "GoSXScene3D",
          props: {
            width: 480,
            height: 300,
            autoRotate: false,
            requireWebGL: true,
            scene: {
              objects: [
                { kind: "box", width: 1.4, height: 1.1, depth: 1.2, x: 0, y: 0, z: 0, color: "#8de1ff" },
              ],
            },
          },
          capabilities: ["canvas", "webgl", "animation"],
        },
      ],
    },
    createWebGLContext: () => new FakeWebGLContext({
      vendor: "Google Inc. (Google)",
      renderer: "ANGLE (Google, Vulkan 1.3.0 (SwiftShader Device (Subzero) (0x0000C0DE)), SwiftShader driver)",
    }),
  });

  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();

  assert.equal(mount.getAttribute("data-gosx-scene3d-software-webgl"), "true");
  assert.equal(mount.getAttribute("data-gosx-scene3d-require-webgl"), "true");
  assert.equal(mount.getAttribute("data-gosx-scene3d-webgl-preference"), "force");
  assert.equal(mount.getAttribute("data-gosx-scene3d-renderer"), "webgl");
  assert.equal(mount.getAttribute("data-gosx-scene3d-renderer-fallback"), null);
});

test("Scene3D defers postfx until idle delay", async () => {
  const mount = new FakeElement("div", null);
  mount.id = "scene-webgl-deferred-postfx";

  const env = createContext({
    elements: [mount],
    enableWebGL: true,
    disableCanvas2D: true,
    manifest: {
      engines: [
        {
          id: "gosx-engine-webgl-deferred-postfx",
          component: "GoSXScene3D",
          kind: "surface",
          mountId: "scene-webgl-deferred-postfx",
          jsExport: "GoSXScene3D",
          props: {
            width: 480,
            height: 300,
            autoRotate: false,
            deferPostFX: true,
            deferPostFXDelayMS: 40,
            scene: {
              postEffects: [
                { kind: "bloom", threshold: 0.7, intensity: 0.5 },
                { kind: "toneMapping", mode: "aces", exposure: 1 },
              ],
              points: [
                {
                  id: "stars",
                  count: 3,
                  positions: [0, 0, 0, 1, 1, 0, -1, 1, 0],
                  color: "#ffffff",
                  size: 1,
                },
              ],
            },
          },
          capabilities: ["canvas", "webgl", "animation"],
        },
      ],
    },
  });
  const timers = installManualTimers(env.context);
  env.context.requestIdleCallback = () => 1;

  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();

  assert.equal(mount.getAttribute("data-gosx-scene3d-postfx"), "deferred");

  assert.equal(timers.runDelay(40), 1);
  assert.equal(mount.getAttribute("data-gosx-scene3d-postfx"), "deferred");
  assert.equal(timers.runDelay(1200), 1);
  await flushAsyncWork();

  assert.equal(mount.getAttribute("data-gosx-scene3d-postfx"), "enabled");
});

test("WebGL customPost receives reserved auto-uniforms: time is nonzero and advances with the clock", async () => {
  const mount = new FakeElement("div", null);
  mount.id = "scene-webgl-custompost-reserved-uniforms";

  let fakeNowMS = 4000; // a real page never starts its clock at exactly 0
  const env = createContext({
    elements: [mount],
    enableWebGL: true,
    enableWebGL2: true,
    disableCanvas2D: true,
    performanceNow: () => fakeNowMS,
    manifest: {
      engines: [
        {
          id: "gosx-engine-custompost-reserved-uniforms",
          component: "GoSXScene3D",
          kind: "surface",
          mountId: "scene-webgl-custompost-reserved-uniforms",
          jsExport: "GoSXScene3D",
          props: {
            width: 320,
            height: 200,
            autoRotate: true,
            scene: {
              postEffects: [
                {
                  kind: "customPost",
                  name: "timed-lens",
                  stage: "beforeTonemap",
                  vertexGLSL: "attribute vec2 a_position; varying vec2 v_uv; void main() { v_uv = a_position * 0.5 + 0.5; gl_Position = vec4(a_position, 0.0, 1.0); }",
                  fragmentGLSL: "precision mediump float; uniform sampler2D _sceneColor; uniform float time; uniform float amount; uniform vec3 tint; varying vec2 v_uv; void main() { gl_FragColor = vec4(tint * amount * sin(time), 1.0); }",
                  shaderLayout: CUSTOM_POST_TIME_LAYOUT_FIXTURE,
                  // Author map carries ONLY the non-reserved param. `time` and
                  // `tint` must come from the engine clock and the compiled
                  // layout defaults respectively.
                  uniforms: { amount: 0.75 },
                },
              ],
            },
          },
          capabilities: ["canvas", "webgl", "animation"],
        },
      ],
    },
  });
  env.context.WebGL2RenderingContext = FakeWebGLContext;

  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();

  assert.equal(mount.getAttribute("data-gosx-scene3d-renderer"), "webgl",
    "this regression test is meaningless unless the WebGL backend actually ran");

  const gl = mount.children[0].getContext("webgl2") || mount.children[0].getContext("webgl");
  assert.ok(gl, "expected a fake WebGL context on the scene canvas");
  const uniform1fValues = (name) => gl.ops
    .filter((entry) => entry[0] === "uniform1f" && entry[1] === name)
    .map((entry) => entry[2]);

  const firstTimes = uniform1fValues("time");
  assert.ok(firstTimes.length > 0,
    "the custom post pass must upload the reserved `time` uniform at least once");
  // THE REGRESSION: before the fix every one of these was 0, because
  // applyCustomPost only ever read effect.uniforms and `time` is not in it.
  assert.ok(firstTimes.every((value) => value > 0),
    "reserved `time` must resolve to the engine clock, got: " + JSON.stringify(firstTimes));
  assert.ok(firstTimes.includes(4), "time must be performance.now()/1000, expected 4, got: " + JSON.stringify(firstTimes));

  // Author-supplied non-reserved params keep working.
  assert.ok(uniform1fValues("amount").includes(0.75),
    "author-supplied `amount` must still reach the pass");
  // Compiled layout defaults now apply to fields absent from the author map.
  // Before the fix these were skipped outright and silently read 0 in GLSL.
  assert.ok(gl.ops.some((entry) => entry[0] === "uniform3f" && entry[1] === "tint"
      && entry[2] === 0.25 && entry[3] === 0.5 && entry[4] === 0.75),
    "compiled layout default for `tint` must upload as a vec3");

  // Advance the clock and drive more frames: `time` must track it, which is
  // what makes an animated post effect animate at all.
  const opsBefore = gl.ops.length;
  fakeNowMS = 9000;
  await flushAsyncWork();
  await flushAsyncWork();

  const laterTimes = gl.ops
    .slice(opsBefore)
    .filter((entry) => entry[0] === "uniform1f" && entry[1] === "time")
    .map((entry) => entry[2]);
  assert.ok(laterTimes.length > 0, "expected further frames to re-upload `time`");
  assert.ok(laterTimes.includes(9),
    "reserved `time` must advance with the clock, got: " + JSON.stringify(laterTimes));
  assert.equal(env.consoleLogs.error.length, 0);
});

test("WebGL post processor is constructed with the Selena uniform resolver injected", () => {
  const webgl = fs.readFileSync(path.join(__dirname, "..", "runtime", "scene3d", "webgl.ts"), "utf8");
  // The injection is what supplies reserved auto-uniforms to custom post
  // passes; without it applyCustomPost silently falls back to author values
  // only. Mirrors the WebGPU side's wgpuCreatePostProcessor(..., sceneSelenaUniformData).
  assert.match(webgl, /function createScenePostProcessor\(gl, resolveSelenaUniform\)/);
  assert.match(webgl, /postProcessor = createScenePostProcessor\(gl, selenaUniformValue\);/);
  assert.match(webgl, /resolveSelenaUniform\(material, layout, field, null\)/);
  // applyCustomPost must NOT go back to reading the author map directly.
  assert.doesNotMatch(webgl, /hasOwnProperty\.call\(uniforms, field\.name\) \? uniforms\[field\.name\] : null/);
});

// --- G1: live-patchable postFXMaxPixels ---
test("Scene3D handle.updateSceneProps({postFXMaxPixels}) live-patches non-destructively: custom pass source survives, the live value changes, a no-op update is cheap", async () => {
  const mount = new FakeElement("div", null);
  mount.id = "scene-webgl-postfx-max-pixels";
  const customVertexGLSL = "attribute vec2 a_position; void main() { gl_Position = vec4(a_position, 0.0, 1.0); }";
  const customFragmentGLSL = "precision mediump float; void main() { gl_FragColor = vec4(1.0); }";

  const env = createContext({
    elements: [mount],
    enableWebGL: true,
    disableCanvas2D: true,
    manifest: {
      engines: [
        {
          id: "gosx-engine-postfx-max-pixels",
          component: "GoSXScene3D",
          kind: "surface",
          mountId: "scene-webgl-postfx-max-pixels",
          jsExport: "GoSXScene3D",
          props: {
            width: 480,
            height: 300,
            autoRotate: false,
            scene: {
              postFXMaxPixels: 921600, // 720p
              postEffects: [
                {
                  kind: "customPost",
                  name: "test-lens",
                  vertexGLSL: customVertexGLSL,
                  fragmentGLSL: customFragmentGLSL,
                  stage: "beforeTonemap",
                },
              ],
            },
          },
          capabilities: ["canvas", "webgl", "animation"],
        },
      ],
    },
  });

  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();

  let mounted = env.context.__gosx.engines.get("gosx-engine-postfx-max-pixels");
  for (let attempt = 0; attempt < 5 && !mounted; attempt += 1) {
    await flushAsyncWork();
    mounted = env.context.__gosx.engines.get("gosx-engine-postfx-max-pixels");
  }
  assert.ok(mounted, "expected the scene3d engine to mount");
  assert.equal(typeof mounted.handle.updateSceneProps, "function");

  const state = mount.__gosxScene3DState;
  assert.ok(state, "expected the sceneState back-door on the mount");
  assert.equal(state.postFXMaxPixels, 921600);
  const effectsBefore = state.postEffects;
  assert.equal(effectsBefore.length, 1);
  assert.equal(effectsBefore[0].vertexGLSL, customVertexGLSL);
  assert.equal(effectsBefore[0].fragmentGLSL, customFragmentGLSL);
  assert.equal(mount.getAttribute("data-gosx-scene3d-postfx-max-pixels"), "921600");

  // (b) the live value changes.
  mounted.handle.updateSceneProps({ postFXMaxPixels: 518400 }); // 540p
  assert.equal(state.postFXMaxPixels, 518400, "sceneState.postFXMaxPixels must update live");
  assert.equal(mount.getAttribute("data-gosx-scene3d-postfx-max-pixels"), "518400", "the confirmation attr must reflect the live value");

  // (a) the compiled custom Selena pass source survives — same array
  // reference (non-destructive), GLSL untouched. applyScenePostEffectsCommand
  // (the CommandSetPostEffects path) would have rebuilt this array from a
  // caller-supplied raw effects payload and, absent one, dropped it entirely
  // — updateSceneProps's postFXMaxPixels branch never calls that path.
  assert.strictEqual(state.postEffects, effectsBefore, "postFXMaxPixels update must be non-destructive to postEffects");
  assert.equal(state.postEffects[0].vertexGLSL, customVertexGLSL);
  assert.equal(state.postEffects[0].fragmentGLSL, customFragmentGLSL);

  // (c) a no-op update (same value) is cheap: no DOM write, no postEffects churn.
  let writes = 0;
  const originalSet = mount.setAttribute.bind(mount);
  mount.setAttribute = function(name, value) { writes += 1; originalSet(name, value); };
  const effectsRefBeforeNoop = state.postEffects;
  mounted.handle.updateSceneProps({ postFXMaxPixels: 518400 });
  assert.equal(writes, 0, "a same-value postFXMaxPixels update must not touch the DOM");
  assert.strictEqual(state.postEffects, effectsRefBeforeNoop);
  assert.equal(state.postFXMaxPixels, 518400);
});

// --- SCENE_CMD_SET_POST_UNIFORMS: non-destructive per-frame uniform patching ---
test("Scene3D SCENE_CMD_SET_POST_UNIFORMS patches named CustomPost uniforms non-destructively: values change, shader payload untouched, counters bump; unknown name is a safe no-op with a miss counter", async () => {
  const mount = new FakeElement("div", null);
  mount.id = "scene-webgl-post-uniforms";
  const customVertexGLSL = "attribute vec2 a_position; void main() { gl_Position = vec4(a_position, 0.0, 1.0); }";
  const customFragmentGLSL = "precision mediump float; uniform float uIntensity; void main() { gl_FragColor = vec4(vec3(uIntensity), 1.0); }";

  const env = createContext({
    elements: [mount],
    enableWebGL: true,
    disableCanvas2D: true,
    manifest: {
      engines: [
        {
          id: "gosx-engine-post-uniforms",
          component: "GoSXScene3D",
          kind: "surface",
          mountId: "scene-webgl-post-uniforms",
          jsExport: "GoSXScene3D",
          props: {
            width: 480,
            height: 300,
            autoRotate: false,
            scene: {
              postEffects: [
                {
                  kind: "customPost",
                  name: "test-lens",
                  vertexGLSL: customVertexGLSL,
                  fragmentGLSL: customFragmentGLSL,
                  stage: "beforeTonemap",
                  uniforms: { uIntensity: 0.25 },
                },
              ],
            },
          },
          capabilities: ["canvas", "webgl", "animation"],
        },
      ],
    },
  });

  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();

  let mounted = env.context.__gosx.engines.get("gosx-engine-post-uniforms");
  for (let attempt = 0; attempt < 5 && !mounted; attempt += 1) {
    await flushAsyncWork();
    mounted = env.context.__gosx.engines.get("gosx-engine-post-uniforms");
  }
  assert.ok(mounted, "expected the scene3d engine to mount");
  assert.equal(typeof mounted.handle.applyCommands, "function");

  const state = mount.__gosxScene3DState;
  assert.ok(state, "expected the sceneState back-door on the mount");
  assert.equal(state.postEffects.length, 1);
  const passBefore = state.postEffects[0];
  assert.equal(passBefore.name, "test-lens");
  assert.equal(passBefore.uniforms.uIntensity, 0.25);
  assert.equal(mount.getAttribute("data-gosx-scene3d-post-uniform-patches"), "0");
  assert.equal(mount.getAttribute("data-gosx-scene3d-post-uniform-patch-misses"), "0");

  // Kind 14: SCENE_CMD_SET_POST_UNIFORMS (see the const in 10-runtime-scene-core.js —
  // 11 is already SCENE_CMD_SET_INSTANCED_GLB_MESHES so this new command could
  // not reuse it without colliding with the existing wire protocol).
  const SCENE_CMD_SET_POST_UNIFORMS = 14;
  mounted.handle.applyCommands([
    { kind: SCENE_CMD_SET_POST_UNIFORMS, data: { effects: [{ name: "test-lens", uniforms: { uIntensity: 0.9, uNew: 3 } }] } },
  ]);

  // (a) same array, same pass object — non-destructive; only .uniforms changed.
  assert.equal(state.postEffects.length, 1, "no pass added/removed");
  assert.strictEqual(state.postEffects[0], passBefore, "pass object identity preserved (non-destructive patch)");
  assert.equal(state.postEffects[0].uniforms.uIntensity, 0.9, "existing uniform patched");
  assert.equal(state.postEffects[0].uniforms.uNew, 3, "new uniform key merged in");
  assert.equal(state.postEffects[0].vertexGLSL, customVertexGLSL, "shader payload (vertexGLSL) untouched");
  assert.equal(state.postEffects[0].fragmentGLSL, customFragmentGLSL, "shader payload (fragmentGLSL) untouched");
  assert.equal(state.postEffects[0].stage, "beforeTonemap", "non-uniform field untouched");
  assert.equal(mount.getAttribute("data-gosx-scene3d-post-uniform-patches"), "1", "applied counter bumped");
  assert.equal(mount.getAttribute("data-gosx-scene3d-post-uniform-patch-misses"), "0");

  // (b) unknown name is a safe no-op with a miss counter — no pass touched.
  mounted.handle.applyCommands([
    { kind: SCENE_CMD_SET_POST_UNIFORMS, data: { effects: [{ name: "does-not-exist", uniforms: { x: 1 } }] } },
  ]);
  assert.equal(state.postEffects.length, 1);
  assert.strictEqual(state.postEffects[0], passBefore, "unmatched patch must not touch the existing pass");
  assert.equal(state.postEffects[0].uniforms.uIntensity, 0.9, "unmatched patch leaves prior uniforms alone");
  assert.equal(state.postEffects[0].uniforms.x, undefined, "unmatched patch's uniforms never land anywhere");
  assert.equal(mount.getAttribute("data-gosx-scene3d-post-uniform-patches"), "1", "applied counter unchanged on a miss");
  assert.equal(mount.getAttribute("data-gosx-scene3d-post-uniform-patch-misses"), "1", "miss counter bumped");
});

// The incident: a Selena-material plane drew with the built-in PBR fallback
// instead of its compiled program whenever a LinesGeometry/GlowMaterial mesh
// shared the frame. Nothing warned; the renderer reported success. Two
// investigations missed it because every existing assertion checked only that
// SOME draw happened, never WHICH program was bound for it.
//
// "landingPadTint" is a uniform only the Selena shader declares, so the fake
// context can name the Selena program by its shader text.
const SELENA_LANDING_PAD_VERTEX_GLSL = [
  "attribute vec3 position;",
  "attribute vec3 normal;",
  "uniform mat4 mvp;",
  "uniform mat3 normalMatrix;",
  "uniform vec3 landingPadTint;",
  "varying vec3 vWorldNormal;",
  "",
  "void main() {",
  "  vWorldNormal = normalize((normalMatrix * normal));",
  "  gl_Position = (mvp * vec4(position, 1.0));",
  "}",
].join("\n");

const SELENA_LANDING_PAD_FRAGMENT_GLSL = [
  "precision mediump float;",
  "uniform mat4 mvp;",
  "uniform mat3 normalMatrix;",
  "uniform vec3 landingPadTint;",
  "varying vec3 vWorldNormal;",
  "",
  "void main() {",
  "  gl_FragColor = vec4(landingPadTint, 1.0);",
  "}",
].join("\n");

const SELENA_LANDING_PAD_LAYOUT = {
  schemaVersion: "selena.descriptor.v1",
  languageVersion: "selena.lang.v1",
  material: "LandingPad",
  kind: "mesh",
  entryPoints: { vertex: "vertexMain", fragment: "fragmentMain" },
  attributes: [
    { location: 0, name: "position", type: "vec3" },
    { location: 1, name: "normal", type: "vec3" },
  ],
  textures: [],
  uniformBlock: {
    size: 128,
    fields: [
      { name: "mvp", type: "mat4", offset: 0, size: 64 },
      { name: "normalMatrix", type: "mat3", offset: 64, size: 48 },
      { name: "landingPadTint", type: "vec3", offset: 112, size: 12 },
    ],
    defaults: [
      { name: "landingPadTint", type: "vec3", values: [1, 0, 1] },
    ],
  },
  wgsl: { group: 0, binding: 0 },
  metal: { buffer: 0 },
};

// mountSelenaLandingPadScene mounts one Selena-material plane, optionally
// alongside a LinesGeometry/GlowMaterial mesh. Everything else is held equal
// so the lines mesh is the only variable between the two cases.
async function mountSelenaLandingPadScene(options) {
  const opts = options || {};
  const mount = new FakeElement("div", null);
  mount.id = "scene-selena-landing-pad-root";

  const material = {
    name: "landing-pad",
    kind: "custom",
    // The companion StandardMaterial color. When the renderer substitutes the
    // built-in material, THIS is the color that reaches the framebuffer -- the
    // downstream investigation proved the substitution by changing exactly this
    // field and watching the output follow it.
    color: "#f8f8f8",
    wireframe: false,
    shaderBackend: "selena",
    customVertex: SELENA_LANDING_PAD_VERTEX_GLSL,
    customFragment: SELENA_LANDING_PAD_FRAGMENT_GLSL,
    shaderLayout: SELENA_LANDING_PAD_LAYOUT,
    customUniforms: { landingPadTint: [1, 0, 1] },
  };
  if (opts.incompleteSelena) {
    // Declares Selena, carries no usable envelope: sceneSelenaIsMaterial says
    // no, nothing fails to compile, and the object draws standard PBR.
    delete material.shaderLayout;
  }

  const materials = [material];
  const objects = [
    {
      id: "landing-pad",
      kind: "plane",
      material: "landing-pad",
      width: 4,
      depth: 4,
      x: 0,
      y: 0,
      z: 0,
      wireframe: false,
    },
  ];
  if (opts.withLines) {
    materials.push({ name: "orbit-glow", kind: "glow", color: "#8de1ff" });
    objects.push({
      id: "orbit-wire",
      kind: "lines",
      material: "orbit-glow",
      points: [
        { x: -2, y: 1, z: 0 },
        { x: 2, y: 1, z: 0 },
        { x: 2, y: 1, z: 2 },
      ],
      lineSegments: [[0, 1], [1, 2]],
      lineWidth: 2,
    });
  }

  const rejectShaderSources = opts.rejectShaderSources || [];
  const env = createContext({
    elements: [mount],
    createWebGL2Context: () => new FakeWebGLContext({ rejectShaderSources }),
    disableCanvas2D: true,
    manifest: {
      engines: [
        {
          id: "gosx-engine-selena-landing-pad",
          component: "GoSXScene3D",
          kind: "surface",
          mountId: "scene-selena-landing-pad-root",
          props: {
            width: 640,
            height: 360,
            background: "#08151f",
            camera: { x: 0, y: 3, z: 8, fov: 72 },
            scene: { materials, objects },
          },
        },
      ],
    },
  });
  env.context.WebGL2RenderingContext = FakeWebGLContext;
  if (opts.renderTruth) {
    env.context.__gosx_scene3d_render_truth = true;
  }

  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();
  await flushAsyncWork();

  // Guard the guard: a Canvas2D fallback runs no shaders at all, so every
  // assertion below would pass vacuously against a scene that never reached
  // WebGL.
  assert.equal(mount.getAttribute("data-gosx-scene3d-renderer"), "webgl",
    "the scene must actually reach the WebGL backend");
  const gl = mount.children[0].getContext("webgl2");
  assert.ok(gl, "expected a fake WebGL2 context on the scene canvas");
  return { env, mount, gl };
}

// selenaLandingPadDrawSummary splits the frame's triangle draws by the program
// that was bound when each one was issued.
function selenaLandingPadDrawSummary(gl) {
  const selenaProgram = gl.programMatching("landingPadTint");
  const triangleDraws = gl.ops.filter((entry) => entry[0] === "drawArrays" && entry[1] === gl.TRIANGLES);
  const selenaID = selenaProgram ? selenaProgram.id : null;
  return {
    selenaProgram,
    triangleDraws,
    selenaDraws: triangleDraws.filter((entry) => selenaID !== null && entry[4] === selenaID),
    otherDraws: triangleDraws.filter((entry) => selenaID === null || entry[4] !== selenaID),
  };
}

test("Scene3D WebGL never repaints a Selena mesh with the fallback material when a lines mesh shares the frame", async () => {
  // Control: the Selena plane alone. Proves the fixture compiles and draws
  // through Selena, so a failure in the mixed case cannot be blamed on it.
  const solo = await mountSelenaLandingPadScene({ withLines: false });
  const soloSummary = selenaLandingPadDrawSummary(solo.gl);
  assert.ok(soloSummary.selenaProgram, "the Selena program must link for the plane-only scene");
  assert.equal(soloSummary.selenaDraws.length, 1,
    "plane-only control: the Selena plane must draw once, with the Selena program");
  assert.equal(soloSummary.otherDraws.length, 0,
    "plane-only control: no other program may draw triangles");

  // The trigger: add one LinesGeometry/GlowMaterial mesh, change nothing else.
  const mixed = await mountSelenaLandingPadScene({ withLines: true });
  const mixedSummary = selenaLandingPadDrawSummary(mixed.gl);
  assert.ok(mixedSummary.selenaProgram,
    "the Selena program must still link when a lines mesh shares the frame");
  assert.equal(mixedSummary.selenaDraws.length, 1,
    "the Selena plane must still draw with its compiled program");

  // The defect. Before the fix the legacy immediate-mode world-mesh path
  // (renderSceneWebGLMeshWorldBundle) re-drew the SAME quad with the flat world
  // program and the material's companion base color, on top of the correct
  // Selena draw -- but only when the frame also carried world line segments,
  // which is why neither the plane alone nor the lines alone reproduced it.
  assert.equal(mixedSummary.otherDraws.length, 0,
    "no second program may repaint the Selena mesh; extra triangle draws: " +
    JSON.stringify(mixedSummary.otherDraws));

  // And the substitution counter must stay clean: nothing fell back here.
  assert.equal(mixed.env.consoleLogs.warn.length, 0,
    "a healthy Selena draw must not warn, got: " + JSON.stringify(mixed.env.consoleLogs.warn));
});

test("Scene3D WebGL reports a material fallback through render truth and the console", async () => {
  // Case 1: a real compile failure. The fake context rejects any shader whose
  // source mentions landingPadTint, which is exactly what a driver that refuses
  // the GLSL does.
  const failed = await mountSelenaLandingPadScene({
    withLines: true,
    renderTruth: true,
    rejectShaderSources: ["landingPadTint"],
  });
  assert.equal(failed.mount.getAttribute("data-gosx-scene3d-render-backend"), "webgl",
    "render truth must be published for the WebGL backend");
  const failedCount = Number(failed.mount.getAttribute("data-gosx-scene3d-render-mesh-material-fallback"));
  assert.ok(failedCount >= 1,
    "a Selena shader the driver rejected must raise the material-fallback counter, got " + failedCount);
  assert.match(failed.mount.getAttribute("data-gosx-scene3d-render-mesh-material-fallback-detail"),
    /selena-compile@LandingPad/,
    "the detail attribute must name the reason and the material");
  assert.ok(failed.env.consoleLogs.warn.some((line) => String(line).includes("Selena shader compilation failed")),
    "the compile failure must warn on the console, got: " + JSON.stringify(failed.env.consoleLogs.warn));

  // Case 2: the silent one. The material DECLARES Selena but carries no usable
  // envelope, so nothing fails to compile and the old code said nothing at all.
  const incomplete = await mountSelenaLandingPadScene({
    withLines: true,
    renderTruth: true,
    incompleteSelena: true,
  });
  const incompleteCount = Number(incomplete.mount.getAttribute("data-gosx-scene3d-render-mesh-material-fallback"));
  assert.ok(incompleteCount >= 1,
    "an incomplete Selena envelope must raise the material-fallback counter, got " + incompleteCount);
  assert.match(incomplete.mount.getAttribute("data-gosx-scene3d-render-mesh-material-fallback-detail"),
    /selena-not-usable/,
    "the detail attribute must distinguish an unusable envelope from a compile failure");
  assert.ok(incomplete.env.consoleLogs.warn.some((line) => String(line).includes("Selena material is incomplete")),
    "an incomplete Selena material must warn on the console, got: " + JSON.stringify(incomplete.env.consoleLogs.warn));

  // The counter is per FRAME, not per cache miss: ensureSelenaProgram caches
  // its failure, so a compile-time-only counter would read a healthy zero on
  // every frame after the first while the wrong material kept drawing.
  const healthy = await mountSelenaLandingPadScene({ withLines: true, renderTruth: true });
  assert.equal(healthy.mount.getAttribute("data-gosx-scene3d-render-mesh-material-fallback"), "0",
    "a healthy frame must report zero material fallbacks");
  assert.equal(healthy.mount.getAttribute("data-gosx-scene3d-render-mesh-material-fallback-detail"), "");
});
