"use strict";

const { readSceneRendererBackendSrc } = require("./scene3d-renderer-source-set.js");
// Scene3D camera math and the shared draw planner: projection, view matrices,
// depth conventions, screen-to-ray, draw sorting and bounds culling.
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
  bootstrapRuntimeSource,
  FakeElement,
  createContext,
  freshFeatureBundleSource,
  runScript,
  flushAsyncWork,
  makeSceneApiEnv,
} = require("./runtime-test-harness.js");

test("bootstrap Scene3D bounds depth uses PBR camera forward depth", async () => {
  const api = await makeSceneApiEnv();
  const camera = { x: 0, y: 0, z: 500, fov: 72, near: 0.05, far: 2000 };
  const visible = { minX: -1, minY: -1, minZ: -604, maxX: 1, maxY: 1, maxZ: -604 };
  const behind = { minX: -1, minY: -1, minZ: 604, maxX: 1, maxY: 1, maxZ: 604 };

  const depth = api.sceneBoundsDepthMetrics(visible, camera);
  assert.equal(depth.near, 1104);
  assert.equal(depth.far, 1104);
  assert.equal(depth.center, 1104);
  assert.equal(api.sceneBoundsViewCulled(visible, camera), false);
  assert.equal(api.sceneBoundsViewCulled(behind, camera), true);
});

test("bootstrap Scene3D bounds culling supports rotated cameras", async () => {
  const api = await makeSceneApiEnv();
  const camera = { x: 0, y: 0, z: 0, rotationY: Math.PI / 2, fov: 72, near: 0.05, far: 128 };
  const visible = { minX: -10, minY: -1, minZ: -1, maxX: -10, maxY: 1, maxZ: 1 };
  const behind = { minX: 10, minY: -1, minZ: -1, maxX: 10, maxY: 1, maxZ: 1 };

  assert.equal(api.sceneBoundsDepthMetrics(visible, camera).center, 10);
  assert.equal(api.sceneBoundsViewCulled(visible, camera), false);
  assert.equal(api.sceneBoundsViewCulled(behind, camera), true);
});

test("bootstrap Scene3D projection uses positive forward depth", async () => {
  const api = await makeSceneApiEnv();
  const camera = { x: 10, y: 2, z: 500, fov: 90, near: 0.1, far: 2000 };

  const front = api.sceneProjectPoint({ x: 10, y: 2, z: -500 }, camera, 200, 100);
  assert.ok(front, "expected translated point in front of camera to project");
  assert.equal(front.x, 100);
  assert.equal(front.y, 50);
  assert.equal(front.depth, 1000);
  assert.equal(api.sceneWorldPointDepth({ x: 10, y: 2, z: -500 }, camera), 1000);

  assert.equal(api.sceneProjectPoint({ x: 10, y: 2, z: 600 }, camera, 200, 100), null);
  assert.equal(api.sceneWorldPointDepth({ x: 10, y: 2, z: 600 }, camera), -100);
});

test("bootstrap Scene3D projection supports rotated cameras", async () => {
  const api = await makeSceneApiEnv();
  const camera = { x: 0, y: 0, z: 0, rotationY: Math.PI / 2, fov: 90, near: 0.1, far: 128 };

  const front = api.sceneProjectPoint({ x: -10, y: 0, z: 0 }, camera, 200, 100);
  assert.ok(front, "expected camera rotated toward -X to project -X point");
  assert.ok(Math.abs(front.x - 100) < 1e-9);
  assert.ok(Math.abs(front.y - 50) < 1e-9);
  assert.ok(Math.abs(front.depth - 10) < 1e-9);
  assert.equal(api.sceneProjectPoint({ x: 10, y: 0, z: 0 }, camera, 200, 100), null);
});

test("bootstrap Scene3D PBR view matrix matches scalar camera-local transform", async () => {
  const api = await makeSceneApiEnv();
  assert.equal(typeof api.scenePBRViewMatrix, "function");
  assert.equal(typeof api.sceneCameraLocalPoint, "function");

  function transformPoint(matrix, point) {
    return {
      x: matrix[0] * point.x + matrix[4] * point.y + matrix[8] * point.z + matrix[12],
      y: matrix[1] * point.x + matrix[5] * point.y + matrix[9] * point.z + matrix[13],
      z: matrix[2] * point.x + matrix[6] * point.y + matrix[10] * point.z + matrix[14],
    };
  }
  function assertClose(actual, expected, label) {
    assert.ok(Math.abs(actual.x - expected.x) < 1e-6, label + " x got " + actual.x + " want " + expected.x);
    assert.ok(Math.abs(actual.y - expected.y) < 1e-6, label + " y got " + actual.y + " want " + expected.y);
    assert.ok(Math.abs(actual.z - expected.z) < 1e-6, label + " z got " + actual.z + " want " + expected.z);
  }

  const cases = [
    {
      label: "yaw-pi-over-two",
      camera: { x: 0, y: 0, z: 0, rotationY: Math.PI / 2, fov: 72, near: 0.05, far: 128 },
      points: [{ x: -10, y: 0, z: 0 }, { x: 10, y: 0, z: 0 }],
    },
    {
      label: "combined-rotation-translation",
      camera: { x: 3, y: -2, z: 5, rotationX: 0.37, rotationY: -0.61, rotationZ: 0.23, fov: 72, near: 0.05, far: 128 },
      points: [{ x: -4, y: 7, z: -11 }, { x: 8, y: -3, z: 2 }],
    },
  ];

  for (const item of cases) {
    const matrix = api.scenePBRViewMatrix(item.camera);
    for (const point of item.points) {
      const fromMatrix = transformPoint(matrix, point);
      const scalar = api.sceneCameraLocalPoint(point, item.camera);
      assertClose(fromMatrix, scalar, item.label);
    }
  }

  const yawMatrix = api.scenePBRViewMatrix(cases[0].camera);
  assert.ok(Math.abs(transformPoint(yawMatrix, { x: -10, y: 0, z: 0 }).z + 10) < 1e-6);
});

test("bootstrap Scene3D orthographic projection uses camera depth range", async () => {
  const api = await makeSceneApiEnv();
  const camera = {
    kind: "orthographic",
    x: 0, y: 0, z: 50,
    left: -10, right: 10, bottom: -5, top: 5,
    near: 1, far: 100,
  };

  const front = api.sceneProjectPoint({ x: 0, y: 0, z: 0 }, camera, 200, 100);
  assert.ok(front, "expected orthographic point in front");
  assert.equal(front.x, 100);
  assert.equal(front.y, 50);
  assert.equal(front.depth, 50);
  assert.equal(api.sceneProjectPoint({ x: 0, y: 0, z: 49.5 }, camera, 200, 100), null);
  assert.equal(api.sceneProjectPoint({ x: 0, y: 0, z: -60 }, camera, 200, 100), null);
});

test("bootstrap Scene3D screen-to-ray center uses world eye and negative local Z", async () => {
  const api = await makeSceneApiEnv();

  const perspective = api.sceneScreenToRay(100, 50, 200, 100, { x: 3, y: 4, z: 5, fov: 90, near: 0.1, far: 100 });
  assert.equal(perspective.origin.x, 3);
  assert.equal(perspective.origin.y, 4);
  assert.equal(perspective.origin.z, 5);
  assert.ok(Math.abs(perspective.dir.x) < 1e-9);
  assert.ok(Math.abs(perspective.dir.y) < 1e-9);
  assert.ok(Math.abs(perspective.dir.z + 1) < 1e-9);

  const orthographic = api.sceneScreenToRay(100, 50, 200, 100, {
    kind: "orthographic",
    x: 3, y: 4, z: 5,
    left: -10, right: 10, bottom: -5, top: 5,
    near: 2, far: 100,
  });
  assert.equal(orthographic.origin.x, 3);
  assert.equal(orthographic.origin.y, 4);
  assert.equal(orthographic.origin.z, 3);
  assert.ok(Math.abs(orthographic.dir.x) < 1e-9);
  assert.ok(Math.abs(orthographic.dir.y) < 1e-9);
  assert.ok(Math.abs(orthographic.dir.z + 1) < 1e-9);
});

test("bootstrap Scene3D draw sorting uses no-bounds positive depth", async () => {
  const api = await makeSceneApiEnv();
  const camera = { x: 0, y: 0, z: 0, fov: 72, near: 0.05, far: 128 };
  const bundle = {
    camera,
    materials: [{ kind: "flat", color: "#ffffff", opacity: 0.5, wireframe: true, renderPass: "alpha" }],
    worldPositions: new Float32Array([
      -1, 0, -2,
      1, 0, -2,
      -1, 0, -10,
      1, 0, -10,
    ]),
    worldColors: new Float32Array([
      1, 0, 0, 1,
      1, 0, 0, 1,
      0, 0, 1, 1,
      0, 0, 1, 1,
    ]),
    objects: [
      { id: "near", kind: "line", materialIndex: 0, vertexOffset: 0, vertexCount: 2, static: false },
      { id: "far", kind: "line", materialIndex: 0, vertexOffset: 2, vertexCount: 2, static: false },
    ],
  };

  const plan = api.buildSceneWorldDrawPlan(bundle);
  assert.equal(api.sceneWorldPointDepth({ x: 0, y: 0, z: -10 }, camera), 10);
  assert.equal(api.sceneWorldPointDepth({ x: 0, y: 0, z: -2 }, camera), 2);
  assert.deepEqual(Array.from(plan.alphaPositions), [-1, 0, -10, 1, 0, -10, -1, 0, -2, 1, 0, -2]);
});

test("bootstrap Canvas fallback projection drops points behind camera", async () => {
  const api = await makeSceneApiEnv();
  const camera = { x: 0, y: 0, z: 5, fov: 90, near: 0.1, far: 20 };

  const front = api.sceneProjectPoint({ x: 0, y: 0, z: 0 }, camera, 200, 100);
  assert.ok(front, "expected Canvas projection to keep front point");
  assert.equal(front.x, 100);
  assert.equal(front.y, 50);
  assert.equal(front.depth, 5);
  assert.equal(api.sceneProjectPoint({ x: 0, y: 0, z: 6 }, camera, 200, 100), null);
});

test("bootstrap Scene3D world planner keeps front bounds and drops behind bounds", async () => {
  const api = await makeSceneApiEnv();
  const camera = { x: 0, y: 0, z: 500, fov: 72, near: 0.05, far: 2000 };
  const bundle = {
    camera,
    materials: [{ kind: "flat", color: "#ffffff", opacity: 1, wireframe: true, renderPass: "opaque" }],
    worldPositions: new Float32Array([
      -1, 0, -604,
      1, 0, -604,
      -1, 0, 604,
      1, 0, 604,
    ]),
    worldColors: new Float32Array([
      1, 1, 1, 1,
      1, 1, 1, 1,
      1, 1, 1, 1,
      1, 1, 1, 1,
    ]),
    objects: [
      {
        id: "visible-line",
        kind: "line",
        materialIndex: 0,
        vertexOffset: 0,
        vertexCount: 2,
        static: true,
        bounds: { minX: -1, minY: 0, minZ: -604, maxX: 1, maxY: 0, maxZ: -604 },
      },
      {
        id: "behind-line",
        kind: "line",
        materialIndex: 0,
        vertexOffset: 2,
        vertexCount: 2,
        static: true,
        bounds: { minX: -1, minY: 0, minZ: 604, maxX: 1, maxY: 0, maxZ: 604 },
      },
    ],
  };

  const plan = api.buildSceneWorldDrawPlan(bundle);
  assert.equal(plan.staticOpaqueVertexCount, 2);
  assert.deepEqual(Array.from(plan.staticOpaquePositions), [-1, 0, -604, 1, 0, -604]);
});

test("bootstrap Scene3D WebGL shaders use shared camera depth contract", () => {
  // The legacy vertex-colour renderer and its shaders left
  // 10-runtime-scene-core.js for 16e-scene-webgl-legacy.ts, which ships in the
  // WebGL chunk instead of on every Scene3D page. Read the file that holds the
  // shaders now, and keep every assertion the depth contract had.
  const core = fs.readFileSync(path.join(__dirname, "bootstrap-src", "16e-scene-webgl-legacy.ts"), "utf8");

  assert.match(core, /uniform vec2 u_depth_range;/);
  assert.match(core, /a_position\.z - u_camera\.z/);
  assert.match(core, /world\.z - u_camera\.z/);
  assert.match(core, /float depth = -local\.z;/);
  assert.match(core, /float clipZ = \(\(nearDepth \+ farDepth\) \* rangeInv\) \* local\.z \+ \(2\.0 \* nearDepth \* farDepth \* rangeInv\);/);
  assert.match(core, /vec4\(local\.x \* focal \/ max\(u_aspect, 0\.0001\), local\.y \* focal, clipZ, depth\)/);
  assert.match(core, /float clipDepth = \(\(depth - nearDepth\) \/ max\(farDepth - nearDepth, 0\.0001\)\) \* 2\.0 - 1\.0/);
  assert.match(core, /depthRangeLocation: gl\.getUniformLocation\(program, "u_depth_range"\)/);
  assert.match(core, /surfaceDepthRangeLocation: surfaceProgram \? gl\.getUniformLocation\(surfaceProgram, "u_depth_range"\)/);
  assert.match(core, /gl\.uniform2f\(resources\.depthRangeLocation, camera\.near, camera\.far\)/);
  assert.match(core, /gl\.uniform2f\(resources\.surfaceDepthRangeLocation, camera\.near, camera\.far\)/);
  assert.match(core, /gl\.uniform2f\(thickProgram\.depthRangeLocation, camera\.near, camera\.far\)/);
  assert.doesNotMatch(core, /vec4\(2\.0, 2\.0, 0\.0, 1\.0\)/);
  assert.doesNotMatch(core, /depth <= nearDepth \|\| depth >= farDepth/);
  assert.doesNotMatch(core, /clipDepth = clamp/);
  assert.doesNotMatch(core, /a_position\.z \+ u_camera\.z/);
  assert.doesNotMatch(core, /world\.z \+ u_camera\.z/);
});

test("bootstrap Scene3D WebGL and WebGPU consume shared PBR view matrix", () => {
  const webgl = readSceneRendererBackendSrc("webgl");
  const webgpu = readSceneRendererBackendSrc("webgpu");

  assert.match(webgl, /scenePBRViewMatrix\(cam, scratchViewMatrix\)/);
  assert.match(webgl, /gl\.uniformMatrix4fv\(uniforms\.viewMatrix, false, viewMatrix\)/);
  assert.match(webgpu, /scenePBRViewMatrix\(cam, scratchViewMatrix\)/);
  assert.match(webgpu, /f\.set\(scratchViewMatrix, 0\)/);
  assert.match(webgpu, /sceneMat4MultiplyInto\(scratchSelenaViewProjection, scratchProjMatrix, scratchViewMatrix\)/);
});

test("bootstrap Scene3D PBR cameraPos uniforms use world eye position", () => {
  const webgl = readSceneRendererBackendSrc("webgl");
  const webgpu = readSceneRendererBackendSrc("webgpu");

  assert.match(webgl, /vec3 V = normalize\(u_cameraPosition - v_worldPosition\);/);
  assert.match(webgl, /gl\.uniform3f\(uniforms\.cameraPosition, cam\.x, cam\.y, cam\.z\)/);
  assert.match(webgl, /gl\.uniform3f\(targetUniforms\.cameraPosition, _frameCam\.x, _frameCam\.y, _frameCam\.z\)/);
  assert.match(webgl, /gl\.uniform3f\(ip\.uniforms\.cameraPosition, _frameCam\.x, _frameCam\.y, _frameCam\.z\)/);
  assert.doesNotMatch(webgl, /cameraPosition, [^;\n]*-cam\.z/);
  assert.doesNotMatch(webgl, /cameraPosition, [^;\n]*-_frameCam\.z/);

  assert.match(webgpu, /let V = normalize\(frame\.cameraPos - in\.worldPos\);/);
  assert.match(webgpu, /camPosZ = cam\.z; \/\/ cameraPos\.z is the world-space eye position\./);
  assert.match(webgpu, /f\[34\] = camPosZ;\s*\/\/ cameraPos\.z \(3D: world eye; ortho2d: 0/);
  assert.match(webgpu, /f\[34\] = eye\.z;/);
  assert.match(webgpu, /z: cam\.mode === "ortho2d" \? 0 : sceneNumber\(cam\.z, 0\)/);
  assert.doesNotMatch(webgpu, /camPosZ = -cam\.z/);
  assert.doesNotMatch(webgpu, /f\[34\] = -eye\.z/);
});

test("bootstrap resolves Scene3D CSS custom properties in the planner", async () => {
  let computedStyleCalls = 0;
  const env = createContext({
    getComputedStyle(element) {
      computedStyleCalls += 1;
      return element && element.computedStyle ? element.computedStyle : {};
    },
  });
  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();

  const api = env.context.__gosx_scene3d_api;
  const mount = new FakeElement("div", null);
  mount.computedStyle = {
    "--scene-core-color": "#5eead4",
    "--scene-core-roughness": "0.3",
    "--scene-ambient-intensity": "0.2",
    "--scene-filter": "bloom(threshold 0.8 intensity 1.1) vignette(intensity 0.5)",
  };
  const starsSentinel = new FakeElement("div", null);
  starsSentinel.computedStyle = {
    "--point-size": "2.5",
  };
  const sentinels = new Map([["stars", starsSentinel]]);
  const bundle = {
    bundleVersion: api.SCENE_RENDER_BUNDLE_VERSION,
    camera: { x: 0, y: 0, z: 6, fov: 72, near: 0.05, far: 128 },
    environment: { ambientIntensity: 0 },
    materials: [
      { name: "core", color: "var(--scene-core-color)", roughness: "var(--scene-core-roughness, 0.4)" },
    ],
    meshObjects: [
      { id: "hero", kind: "box", materialIndex: 0, vertexOffset: 0, vertexCount: 3, depthCenter: 4 },
    ],
    objects: [],
    points: [{ id: "stars", count: 3, color: "#ffffff", size: 1 }],
    worldPositions: new Float32Array(0),
    worldColors: new Float32Array(0),
    worldMeshPositions: new Float32Array(9),
    worldMeshNormals: new Float32Array(9),
  };
  const viewport = { cssWidth: 320, cssHeight: 180, pixelWidth: 320, pixelHeight: 180, pixelRatio: 1 };
  const prepared = api.prepareScene(bundle, bundle.camera, viewport, null, { mount, sentinels, revision: 1 });
  const firstComputedStyleCalls = computedStyleCalls;

  assert.equal(prepared.ir.materials[0].color, "#5eead4");
  assert.equal(prepared.ir.materials[0].roughness, 0.3);
  assert.equal(prepared.ir.environment.ambientIntensity, 0.2);
  assert.equal(prepared.ir.points[0].size, 2.5);
  assert.equal(prepared.ir.postEffects.length, 2);
  assert.equal(JSON.stringify(prepared.ir.postEffects[0]), JSON.stringify({ kind: "bloom", threshold: 0.8, intensity: 1.1 }));

  const cached = api.prepareScene(bundle, bundle.camera, viewport, prepared, { mount, sentinels, revision: 1 });
  assert.equal(cached, prepared);
  assert.equal(computedStyleCalls, firstComputedStyleCalls);

  const cachedAgain = api.prepareScene(bundle, bundle.camera, viewport, cached, { mount, sentinels, revision: 1 });
  assert.equal(cachedAgain.ir.points[0], cached.ir.points[0]);
  assert.equal(computedStyleCalls, firstComputedStyleCalls);

  mount.computedStyle["--scene-core-color"] = "#1e3a8a";
  const staleRevision = api.prepareScene(bundle, bundle.camera, viewport, cachedAgain, { mount, sentinels, revision: 1 });
  assert.equal(staleRevision, cachedAgain);
  assert.equal(staleRevision.ir.materials[0].color, "#5eead4");
  assert.equal(computedStyleCalls, firstComputedStyleCalls);

  const updated = api.prepareScene(bundle, bundle.camera, viewport, staleRevision, { mount, sentinels, revision: 2 });
  assert.notEqual(updated, prepared);
  assert.equal(updated.ir.materials[0].color, "#1e3a8a");
  assert.ok(computedStyleCalls > firstComputedStyleCalls);
});

test("bootstrap Scene3D planner invalidates cached spot-light attenuation and cone fields", async () => {
  const env = createContext({});
  runScript(bootstrapRuntimeSource, env.context, "bootstrap-runtime.js");
  runScript(freshFeatureBundleSource("scene3d"), env.context, "bootstrap-feature-scene3d.js");
  await flushAsyncWork();
  const api = env.context.__gosx_scene3d_api;
  const viewport = { cssWidth: 320, cssHeight: 180, pixelWidth: 320, pixelHeight: 180, pixelRatio: 1 };
  const light = {
    id: "key",
    kind: "spot",
    color: "#ffffff",
    intensity: 1,
    x: 0,
    y: 3,
    z: 2,
    directionX: 0,
    directionY: -1,
    directionZ: 0,
    angle: 0.5,
    penumbra: 0.1,
    range: 6,
    decay: 2,
  };

  for (const [field, value] of [["range", 12], ["decay", 3], ["angle", 0.75], ["penumbra", 0.4]]) {
    const bundle = {
      bundleVersion: api.SCENE_RENDER_BUNDLE_VERSION,
      camera: { x: 0, y: 0, z: 6, fov: 72, near: 0.05, far: 128 },
      environment: {},
      lights: [Object.assign({}, light)],
      materials: [],
      meshObjects: [],
      objects: [],
      points: [],
      worldPositions: new Float32Array(0),
      worldColors: new Float32Array(0),
      worldMeshPositions: new Float32Array(0),
      worldMeshNormals: new Float32Array(0),
    };
    const prepared = api.prepareScene(bundle, bundle.camera, viewport, null);
    bundle.lights = [Object.assign({}, light, { [field]: value })];

    const updated = api.prepareScene(bundle, bundle.camera, viewport, prepared);
    assert.notEqual(updated, prepared, field + " mutation must invalidate the prepared scene");
    assert.notEqual(updated.signature, prepared.signature, field + " mutation must change the signature");
    assert.equal(updated.rebuilds, prepared.rebuilds + 1);
    assert.equal(api.prepareScene(bundle, bundle.camera, viewport, updated), updated);
  }
});
