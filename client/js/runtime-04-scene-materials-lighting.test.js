"use strict";
// Scene3D materials, shadows and lighting: texture-unit budgets, CSM cascades,
// environment maps, sprites, HTML overlays, pick signals and IR diff commands.
//
// Split out of the former client/js/runtime.test.js, which this set replaces
// and which no longer exists. Every shared fake, sandbox builder and fixture
// factory lives in ./runtime-test-harness.js.

const test = require("node:test");
const assert = require("node:assert/strict");
const path = require("node:path");
const vm = require("node:vm");

const {
  bootstrapSource,
  bootstrapRuntimeSource,
  bootstrapFeatureEnginesSource,
  FakeWebGLContext,
  FakeElement,
  buildMinimalGLBBytes,
  buildSkinnedGLBBytes,
  createContext,
  runScript,
  flushAsyncWork,
  freshFeatureBundleSource,
} = require("./runtime-test-harness.js");

function createDeferredModelRoute() {
  let resolve;
  let reject;
  const promise = new Promise((resolvePromise, rejectPromise) => {
    resolve = resolvePromise;
    reject = rejectPromise;
  });
  return { promise, resolve, reject };
}

function modelAssetJSON(id) {
  return {
    text: JSON.stringify({
      objects: [{ id, kind: "box", width: 1, height: 1, depth: 1 }],
    }),
  };
}

test("bootstrap allocates Scene3D texture units without CSM and IBL collisions", () => {
  const env = createContext({});
  runScript(bootstrapSource, env.context, "bootstrap.js");

  const api = env.context.window.__gosx_scene3d_resource_api;
  assert.equal(typeof api.allocateTextureUnits, "function");

  const twoShadowLayout = api.allocateTextureUnits({ shadowCount: 2, ibl: true, maxUnits: 16 });
  assert.deepEqual({ ...twoShadowLayout.material },
    { albedo: 0, normal: 1, roughness: 2, metalness: 3, emissive: 4, occlusion: 5,
      specularIntensity: 6, specularColor: 7 });
  assert.deepEqual(Array.from(twoShadowLayout.shadows), [8, 9]);
  assert.deepEqual({ ...twoShadowLayout.ibl }, { irradiance: 10, radiance: 11, brdfLUT: 12 });

  const defaultLayout = api.allocateTextureUnits({ shadowCount: 2, ibl: true });
  assert.deepEqual(Array.from(defaultLayout.shadows), [8, 9]);
  assert.deepEqual({ ...defaultLayout.ibl }, { irradiance: 10, radiance: 11, brdfLUT: 12 });

  const csmLayout = api.allocateTextureUnits({ shadowCount: 4, ibl: true, maxUnits: 16 });
  assert.deepEqual(Array.from(csmLayout.shadows), [8, 9, 10, 11]);
  assert.deepEqual({ ...csmLayout.ibl }, { irradiance: 12, radiance: 13, brdfLUT: 14 });

  // With the specular-colour unit reserved the shared pool starts at 8, so a
  // 10-unit budget fits only the eight material samplers: IBL needs three
  // shared units and is disabled with a warning instead of colliding.
  const constrained = api.allocateTextureUnits({ shadowCount: 6, ibl: true, maxUnits: 10 });
  assert.deepEqual(Array.from(constrained.shadows), []);
  assert.equal(constrained.ibl, null);
  assert.equal(constrained.warnings.length > 0, true);
});

test("bootstrap resolves Scene3D shadow and IBL resource budgets", () => {
  const env = createContext({});
  runScript(bootstrapSource, env.context, "bootstrap.js");

  const api = env.context.window.__gosx_scene3d_resource_api;
  const budget = api.resolveTextureMemoryBudget({
    shadowCount: 4,
    shadowSize: 1024,
    ibl: true,
  });

  assert.equal(budget.totalBytes <= 26 * 1024 * 1024, true);
  assert.equal(budget.ibl, true);
  assert.equal(budget.iblProfile.sourceFaceSize <= 256, true);
  assert.equal(budget.warnings.some((msg) => msg.includes("IBL profile downscaled")), true);

  const halfFloat = api.resolveIBLRenderTargetMode({
    getExtension(name) {
      return name === "EXT_color_buffer_half_float" ? { name } : null;
    },
  });
  assert.equal(halfFloat.mode, "half-float");

  const fallback = api.resolveIBLRenderTargetMode({ getExtension() { return null; } }, { lowPower: true });
  assert.equal(fallback.mode, "ldr-fallback");
  assert.equal(fallback.profile.sourceFaceSize, 128);

  const disabled = api.resolveIBLRenderTargetMode({ getExtension() { return null; } }, { allowLDRFallback: false });
  assert.equal(disabled.mode, "disabled");

  const rawHDR = Buffer.concat([
    Buffer.from("#?RADIANCE\nFORMAT=32-bit_rle_rgbe\n\n-Y 1 +X 1\n", "ascii"),
    Buffer.from([128, 64, 32, 129]),
  ]);
  const parsed = api.parseRadianceHDR(rawHDR.buffer.slice(rawHDR.byteOffset, rawHDR.byteOffset + rawHDR.byteLength));
  assert.equal(parsed.width, 1);
  assert.equal(parsed.height, 1);
  assert.ok(Math.abs(parsed.data[0] - 1) < 0.00001);
  assert.ok(Math.abs(parsed.data[1] - 0.5) < 0.00001);
  assert.ok(Math.abs(parsed.data[2] - 0.25) < 0.00001);
});

test("bootstrap hashes shadow passes with cascade-sensitive inputs", () => {
  const env = createContext({});
  runScript(bootstrapSource, env.context, "bootstrap.js");

  const api = env.context.window.__gosx_scene3d_resource_api;
  assert.equal(typeof api.shadowPassHash, "function");

  const matrix = new Float32Array(16);
  matrix[0] = 1;
  matrix[5] = 1;
  matrix[10] = 1;
  matrix[15] = 1;
  const casters = [
    { castShadow: true, vertexOffset: 0, vertexCount: 6, depthNear: 1, depthFar: 3 },
  ];
  const base = api.shadowPassHash(matrix, casters, {
    cascadeIndex: 0,
    splitNear: 0.1,
    splitFar: 10,
    shadowSize: 1024,
  });

  assert.notEqual(base, api.shadowPassHash(matrix, casters, {
    cascadeIndex: 1,
    splitNear: 0.1,
    splitFar: 10,
    shadowSize: 1024,
  }));
  assert.notEqual(base, api.shadowPassHash(matrix, casters, {
    cascadeIndex: 0,
    splitNear: 0.1,
    splitFar: 25,
    shadowSize: 1024,
  }));
  assert.notEqual(base, api.shadowPassHash(matrix, casters, {
    cascadeIndex: 0,
    splitNear: 0.1,
    splitFar: 10,
    shadowSize: 512,
  }));
});

test("bootstrap computes CSM cascade splits blending uniform and log", () => {
  const env = createContext({});
  runScript(bootstrapSource, env.context, "bootstrap.js");

  const api = env.context.window.__gosx_scene3d_resource_api;
  assert.equal(typeof api.computeCascadeSplits, "function");

  const uniform = api.computeCascadeSplits(1, 101, 4, 0);
  // Uniform scheme: splits at 26, 51, 76, 101.
  assert.ok(Math.abs(uniform[0] - 26) < 0.001, "uniform[0]=" + uniform[0]);
  assert.ok(Math.abs(uniform[3] - 101) < 0.001, "uniform[3]=" + uniform[3]);

  const log = api.computeCascadeSplits(1, 100, 4, 1);
  // Log scheme over [1,100] with 4 splits: 100^(1/4) ≈ 3.162 factor.
  assert.ok(Math.abs(log[0] - 3.162) < 0.01);
  assert.ok(Math.abs(log[3] - 100) < 0.001);

  const practical = api.computeCascadeSplits(1, 100, 4, 0.5);
  // Last split always equals far regardless of lambda.
  assert.ok(Math.abs(practical[3] - 100) < 0.001);
  // Practical splits should be monotonically increasing.
  for (let i = 1; i < practical.length; i++) {
    assert.ok(practical[i] > practical[i - 1], "splits must be increasing");
  }

  // Single cascade returns just far.
  const one = api.computeCascadeSplits(1, 50, 1, 0.5);
  assert.equal(one.length, 1);
  assert.ok(Math.abs(one[0] - 50) < 0.001);
});

test("bootstrap fits light-space ortho around a known frustum", () => {
  const env = createContext({});
  runScript(bootstrapSource, env.context, "bootstrap.js");

  const api = env.context.window.__gosx_scene3d_resource_api;
  assert.equal(typeof api.fitLightSpaceOrtho, "function");

  // Identity view: camera at origin, looking down -Z. Near=1, Far=10, fov=90, aspect=1
  // gives a symmetric frustum. Corners lie in world space since view is identity.
  const identity = new Float32Array([
    1, 0, 0, 0,
    0, 1, 0, 0,
    0, 0, 1, 0,
    0, 0, 0, 1,
  ]);
  const corners = api.frustumSubCorners(identity, 90, 1, 1, 10);
  assert.equal(corners.length, 24);
  // Near corners at z = -1, far at z = -10 (camera looks -Z).
  assert.ok(Math.abs(corners[2] - -1) < 0.001);    // near TL z
  assert.ok(Math.abs(corners[14] - -10) < 0.001);  // far TL z
  // Near half-extent at z=1, fov 90, aspect 1: tan(45)=1 → ±1 in x and y.
  assert.ok(Math.abs(Math.abs(corners[0]) - 1) < 0.001);
  assert.ok(Math.abs(Math.abs(corners[1]) - 1) < 0.001);

  // Fit a light looking straight down (light dir = (0,-1,0)) — the ortho
  // matrix should transform any world point inside the frustum to NDC within
  // [-1,1]^3.
  const lightMatrix = api.fitLightSpaceOrtho([0, -1, 0], corners, 0);
  // Centroid of the 8 corners — guaranteed to be inside the fit box, so
  // transformed NDC should be ~origin (within floating-point tolerance).
  let cx = 0, cy = 0, cz = 0;
  for (let i = 0; i < 8; i++) {
    cx += corners[i * 3];
    cy += corners[i * 3 + 1];
    cz += corners[i * 3 + 2];
  }
  cx /= 8; cy /= 8; cz /= 8;

  // Apply mat4 (column-major) * vec4(cx, cy, cz, 1).
  const w = lightMatrix[3] * cx + lightMatrix[7] * cy + lightMatrix[11] * cz + lightMatrix[15];
  const x = (lightMatrix[0] * cx + lightMatrix[4] * cy + lightMatrix[8] * cz + lightMatrix[12]) / w;
  const y = (lightMatrix[1] * cx + lightMatrix[5] * cy + lightMatrix[9] * cz + lightMatrix[13]) / w;
  const z = (lightMatrix[2] * cx + lightMatrix[6] * cy + lightMatrix[10] * cz + lightMatrix[14]) / w;
  assert.ok(Math.abs(x) < 0.2, "centroid x in NDC: " + x);
  assert.ok(Math.abs(y) < 0.2, "centroid y in NDC: " + y);
  assert.ok(z >= -1.01 && z <= 1.01, "centroid z in NDC [-1,1]: " + z);

  // All 8 corners should transform into NDC cube [-1,1]^3.
  for (let i = 0; i < 8; i++) {
    const px = corners[i * 3];
    const py = corners[i * 3 + 1];
    const pz = corners[i * 3 + 2];
    const ww = lightMatrix[3] * px + lightMatrix[7] * py + lightMatrix[11] * pz + lightMatrix[15];
    const nx = (lightMatrix[0] * px + lightMatrix[4] * py + lightMatrix[8] * pz + lightMatrix[12]) / ww;
    const ny = (lightMatrix[1] * px + lightMatrix[5] * py + lightMatrix[9] * pz + lightMatrix[13]) / ww;
    const nz = (lightMatrix[2] * px + lightMatrix[6] * py + lightMatrix[10] * pz + lightMatrix[14]) / ww;
    assert.ok(nx >= -1.01 && nx <= 1.01, "corner " + i + " x in NDC: " + nx);
    assert.ok(ny >= -1.01 && ny <= 1.01, "corner " + i + " y in NDC: " + ny);
    assert.ok(nz >= -1.01 && nz <= 1.01, "corner " + i + " z in NDC: " + nz);
  }
});

test("bootstrap snaps CSM light-space ortho to shadow texels", () => {
  const env = createContext({});
  runScript(bootstrapSource, env.context, "bootstrap.js");

  const api = env.context.window.__gosx_scene3d_resource_api;
  const identity = new Float32Array([
    1, 0, 0, 0,
    0, 1, 0, 0,
    0, 0, 1, 0,
    0, 0, 0, 1,
  ]);
  const corners = api.frustumSubCorners(identity, 90, 1, 1, 10);
  const shifted = new Float32Array(corners);
  for (let i = 0; i < 8; i++) {
    shifted[i * 3] += 0.001;
  }

  const unsnappedA = api.fitLightSpaceOrtho([0, -1, 0], corners, 0);
  const unsnappedB = api.fitLightSpaceOrtho([0, -1, 0], shifted, 0);
  const snappedA = api.fitLightSpaceOrtho([0, -1, 0], corners, 0, 1024);
  const snappedB = api.fitLightSpaceOrtho([0, -1, 0], shifted, 0, 1024);

  let unsnappedDiff = 0;
  let snappedDiff = 0;
  for (let i = 0; i < 16; i++) {
    unsnappedDiff += Math.abs(unsnappedA[i] - unsnappedB[i]);
    snappedDiff += Math.abs(snappedA[i] - snappedB[i]);
  }
  assert.ok(unsnappedDiff > 0.00001, "unsnapped matrix should move with sub-texel camera shifts: " + unsnappedDiff);
  assert.ok(snappedDiff < 0.000001, "snapped matrix should stay stable below one texel: " + snappedDiff + " vs " + unsnappedDiff);
});

test("bootstrap binds Scene3D environment maps for WebGL PBR", async () => {
  const mount = new FakeElement("div", null);
  mount.id = "scene-envmap-root";

  const env = createContext({
    elements: [mount],
    enableWebGL2: true,
    disableCanvas2D: true,
    manifest: {
      engines: [
        {
          id: "gosx-engine-envmap",
          component: "GoSXScene3D",
          kind: "surface",
          mountId: "scene-envmap-root",
          props: {
            width: 640,
            height: 360,
            background: "#08151f",
            environment: {
              envMap: "/hdri/studio.png",
              envIntensity: 1.25,
              envRotation: 0.5,
            },
            scene: {
              environment: {
                envMap: "/hdri/studio.png",
                envIntensity: 1.25,
                envRotation: 0.5,
              },
              objects: [
                {
                  id: "chrome-ball",
                  kind: "sphere",
                  radius: 1,
                  materialKind: "pbr",
                  color: "#ffffff",
                  metalness: 1,
                  roughness: 0.15,
                  vertices: {
                    count: 3,
                    positions: [0, 1, 0, -1, -1, 0, 1, -1, 0],
                    normals: [0, 0, 1, 0, 0, 1, 0, 0, 1],
                    uvs: [0.5, 1, 0, 0, 1, 0],
                  },
                },
              ],
            },
          },
        },
      ],
    },
  });
  env.context.WebGL2RenderingContext = FakeWebGLContext;
  env.context.window.__gosx_scene3d_perf = true;

  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();
  await flushAsyncWork();

  assert.equal(env.imageLoads.includes("/hdri/studio.png"), true);
  assert.equal(mount.getAttribute("data-gosx-scene3d-renderer"), "webgl");
  assert.equal(mount.children[0].listenerCount("gosx:scene3d:resource-ready"), 1);
  const gl = mount.children[0].getContext("webgl2");
  assert.ok(
    mount.__gosxScene3DScheduleCounts["schedule:resource-ready"] >= 1,
    "a static scene must schedule a post-load frame when its environment texture becomes ready",
  );
  assert.ok(gl.ops.some((entry) => entry[0] === "uniform1i" && entry[1] === "u_hasEnvMap" && entry[2] === 1));
  assert.ok(gl.ops.some((entry) => entry[0] === "uniform1i" && entry[1] === "u_envMap" && entry[2] === 10));
  assert.ok(gl.ops.some((entry) => entry[0] === "uniform1f" && entry[1] === "u_envIntensity" && entry[2] === 1.25));
  assert.ok(gl.ops.some((entry) => entry[0] === "uniform1f" && entry[1] === "u_envRotation" && entry[2] === 0.5));
  assert.equal(env.consoleLogs.error.length, 0);
});

test("bootstrap keeps Scene3D CSM shadow units ahead of IBL units", async () => {
  const mount = new FakeElement("div", null);
  mount.id = "scene-csm-ibl-root";

  const env = createContext({
    elements: [mount],
    enableWebGL2: true,
    disableCanvas2D: true,
    manifest: {
      engines: [
        {
          id: "gosx-engine-csm-ibl",
          component: "GoSXScene3D",
          kind: "surface",
          mountId: "scene-csm-ibl-root",
          props: {
            width: 640,
            height: 360,
            camera: { x: 0, y: 0, z: 6, near: 0.1, far: 100, fov: 72 },
            environment: {
              envMap: "/hdri/studio.png",
              envIntensity: 1,
            },
            scene: {
              lights: [
                {
                  id: "sun",
                  kind: "directional",
                  castShadow: true,
                  shadowCascades: 4,
                  shadowSize: 256,
                  shadowSoftness: 0.05,
                  directionX: 0.2,
                  directionY: -1,
                  directionZ: -0.35,
                },
              ],
              objects: [
                {
                  id: "shadow-triangle",
                  kind: "gltf-mesh",
                  materialKind: "pbr",
                  castShadow: true,
                  receiveShadow: true,
                  vertices: {
                    count: 3,
                    positions: [0, 1, 0, -1, -1, 0, 1, -1, 0],
                    normals: [0, 0, 1, 0, 0, 1, 0, 0, 1],
                    uvs: [0.5, 1, 0, 0, 1, 0],
                  },
                },
              ],
            },
          },
        },
      ],
    },
  });
  env.context.WebGL2RenderingContext = FakeWebGLContext;

  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();
  await flushAsyncWork();

  assert.equal(env.imageLoads.includes("/hdri/studio.png"), true);
  assert.equal(mount.getAttribute("data-gosx-scene3d-renderer"), "webgl");
  const gl = mount.children[0].getContext("webgl2");
  assert.equal(gl.ops.filter((entry) => entry[0] === "createFramebuffer").length, 4);
  assert.ok(gl.ops.some((entry) => entry[0] === "uniform1i" && entry[1] === "u_shadowCascades0" && entry[2] === 4));
  assert.ok(gl.ops.some((entry) => entry[0] === "uniform1i" && entry[1] === "u_shadowMap0_0" && entry[2] === 8));
  assert.ok(gl.ops.some((entry) => entry[0] === "uniform1i" && entry[1] === "u_shadowMap0_1" && entry[2] === 9));
  assert.ok(gl.ops.some((entry) => entry[0] === "uniform1i" && entry[1] === "u_shadowMap0_2" && entry[2] === 10));
  assert.ok(gl.ops.some((entry) => entry[0] === "uniform1i" && entry[1] === "u_shadowMap0_3" && entry[2] === 11));
  assert.ok(gl.ops.some((entry) => entry[0] === "uniform1i" && entry[1] === "u_envMap" && entry[2] === 12));
  assert.ok(gl.ops.some((entry) => entry[0] === "uniform1fv" && entry[1] === "u_shadowCascadeSplits0" && entry[2] === 4));
  assert.equal(env.consoleLogs.error.length, 0);
});

test("bootstrap fetches Radiance HDR Scene3D environment maps for WebGL PBR", async () => {
  const mount = new FakeElement("div", null);
  mount.id = "scene-hdr-envmap-root";
  const rawHDR = Buffer.concat([
    Buffer.from("#?RADIANCE\nFORMAT=32-bit_rle_rgbe\n\n-Y 1 +X 1\n", "ascii"),
    Buffer.from([128, 64, 32, 129]),
  ]);

  const env = createContext({
    elements: [mount],
    enableWebGL2: true,
    disableCanvas2D: true,
    createWebGL2Context() {
      const gl = new FakeWebGLContext();
      const getExtension = gl.getExtension.bind(gl);
      gl.getExtension = function(name) {
        return name === "OES_texture_float_linear" ? { name } : getExtension(name);
      };
      return gl;
    },
    fetchRoutes: {
      "/hdri/studio.hdr": {
        bytes: Array.from(rawHDR),
      },
    },
    manifest: {
      engines: [
        {
          id: "gosx-engine-hdr-envmap",
          component: "GoSXScene3D",
          kind: "surface",
          mountId: "scene-hdr-envmap-root",
          props: {
            width: 320,
            height: 180,
            environment: {
              envMap: "/hdri/studio.hdr",
              envIntensity: 0.75,
            },
            objects: [
              {
                id: "hdr-triangle",
                kind: "gltf-mesh",
                materialKind: "pbr",
                vertices: {
                  count: 3,
                  positions: [0, 1, 0, -1, -1, 0, 1, -1, 0],
                  normals: [0, 0, 1, 0, 0, 1, 0, 0, 1],
                  uvs: [0.5, 1, 0, 0, 1, 0],
                },
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

  assert.equal(env.fetchCalls.some((call) => call.url === "/hdri/studio.hdr"), true);
  assert.equal(env.imageLoads.includes("/hdri/studio.hdr"), false);
  const gl = mount.children[0].getContext("webgl2");
  assert.ok(gl.ops.filter((entry) => entry[0] === "texImage2D").length >= 2);
  assert.ok(gl.ops.some((entry) => entry[0] === "uniform1i" && entry[1] === "u_envMap" && entry[2] === 10));
  assert.equal(env.consoleLogs.error.length, 0);
});

test("bootstrap keeps GLB Scene3D assets visible on canvas fallback", async () => {
  const mount = new FakeElement("div", null);
  mount.id = "scene-model-glb-canvas-root";

  const env = createContext({
    elements: [mount],
    fetchRoutes: {
      "/models/runner.glb": {
        bytes: buildMinimalGLBBytes(),
      },
    },
    manifest: {
      engines: [
        {
          id: "gosx-engine-model-glb-canvas",
          component: "GoSXScene3D",
          kind: "surface",
          mountId: "scene-model-glb-canvas-root",
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
  assert.equal(mount.getAttribute("data-gosx-scene3d-renderer"), "canvas");
  const ctx2d = mount.children[0].getContext("2d");
  const lineCount = ctx2d.ops.filter((entry) => entry[0] === "lineTo").length;
  assert.ok(lineCount >= 3);
  assert.equal(env.consoleLogs.error.length, 0);
});

test("bootstrap renders model-relative Scene3D textures without authored JS", async () => {
  const mount = new FakeElement("div", null);
  mount.id = "scene-model-texture-root";

  const env = createContext({
    elements: [mount],
    enableWebGL: true,
    disableCanvas2D: true,
    fetchRoutes: {
      "/models/panel.gosx3d.json": {
        text: JSON.stringify({
          objects: [
            {
              id: "panel",
              kind: "plane",
              width: 1.55,
              height: 1.02,
              x: 0,
              y: 0.6,
              z: 0.4,
              material: {
                kind: "flat",
                color: "#f7fbff",
                texture: "./paper-card.png",
                wireframe: false,
              },
            },
          ],
        }),
      },
    },
    manifest: {
      engines: [
        {
          id: "gosx-engine-model-texture",
          component: "GoSXScene3D",
          kind: "surface",
          mountId: "scene-model-texture-root",
          props: {
            width: 640,
            height: 360,
            background: "#08151f",
            camera: { x: 0, y: 0, z: 6, fov: 72 },
            models: [
              {
                id: "panel-asset",
                src: "/models/panel.gosx3d.json",
                x: 0.25,
                y: 0.1,
                z: -0.4,
                rotationY: 0.12,
              },
            ],
          },
        },
      ],
    },
  });

  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();
  await flushAsyncWork();
  for (let attempt = 0; attempt < 8 && !env.imageLoads.includes("http://localhost:3000/models/paper-card.png"); attempt += 1) {
    await flushAsyncWork();
  }

  assert.equal(env.fetchCalls.some((call) => call.url === "/models/panel.gosx3d.json"), true);
  const modelTextureState = mount.__gosxScene3DState;
  const modelTextureDebug = modelTextureState && modelTextureState.objects
    ? Array.from(modelTextureState.objects.values()).map((object) => ({ id: object.id, kind: object.kind, material: object.material, texture: object.texture, materialKind: object.materialKind, wireframe: object.wireframe }))
    : [];
  assert.equal(env.imageLoads.includes("http://localhost:3000/models/paper-card.png"), true, "imageLoads=" + JSON.stringify(env.imageLoads) + " state=" + JSON.stringify(modelTextureDebug));
  assert.equal(mount.getAttribute("data-gosx-scene3d-renderer"), "webgl");
  const gl = mount.children[0].getContext("webgl");
  assert.ok(gl.ops.some((entry) => entry[0] === "createTexture"));
  assert.ok(gl.ops.some((entry) => entry[0] === "uniform1i" && entry[1] === "u_texture" && entry[2] === 0));
  assert.ok(gl.ops.some((entry) => entry[0] === "vertexAttribPointer" && entry[1] === 3 && entry[2] === 2));
  assert.ok(gl.ops.some((entry) => entry[0] === "texImage2D" && entry[2] === 9));
  assert.ok(gl.ops.some((entry) => entry[0] === "texImage2D" && entry[2] === 6));
  assert.ok(gl.ops.some((entry) => entry[0] === "drawArrays" && entry[1] === gl.TRIANGLES && entry[3] === 6));
  assert.equal(env.consoleLogs.warn.length, 0);
  assert.equal(env.consoleLogs.error.length, 0);
});

test("bootstrap renders declarative Scene3D sprite billboards without authored JS", async () => {
  const mount = new FakeElement("div", null);
  mount.id = "scene-sprite-root";

  const env = createContext({
    elements: [mount],
    manifest: {
      engines: [
        {
          id: "gosx-engine-sprite",
          component: "GoSXScene3D",
          kind: "surface",
          mountId: "scene-sprite-root",
          props: {
            width: 640,
            height: 360,
            background: "#08151f",
            scene: {
              objects: [
                {
                  id: "hero",
                  kind: "box",
                  width: 1.4,
                  height: 1.1,
                  depth: 0.9,
                  x: 0,
                  y: 0.2,
                  z: 0.3,
                  color: "#8de1ff",
                },
              ],
              sprites: [
                {
                  id: "card",
                  src: "/paper-card.png",
                  x: 0.15,
                  y: 1.3,
                  z: 0.5,
                  width: 1.55,
                  height: 1.02,
                  scale: 1,
                  opacity: 0.94,
                  priority: 3,
                  anchorX: 0.5,
                  anchorY: 0.5,
                  fit: "cover",
                  occlude: true,
                },
              ],
            },
            camera: { x: 0, y: 0, z: 6, fov: 72 },
          },
        },
      ],
    },
  });

  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();

  const labelLayer = mount.children[1];
  assert.equal(labelLayer.getAttribute("data-gosx-scene3d-label-layer"), "true");
  assert.equal(labelLayer.children.length, 1);
  const sprite = labelLayer.children[0];
  assert.equal(sprite.getAttribute("data-gosx-scene-sprite"), "card");
  assert.equal(sprite.getAttribute("data-gosx-scene-sprite-fit"), "cover");
  assert.equal(sprite.getAttribute("data-gosx-scene-sprite-occlude"), "true");
  assert.equal(sprite.firstChild.tagName, "IMG");
  assert.equal(sprite.firstChild.getAttribute("src"), "/paper-card.png");
  assert.equal(env.consoleLogs.error.length, 0);
});

test("bootstrap renders declarative Scene3D HTML overlays on the canvas backend", async () => {
  const mount = new FakeElement("div", null);
  mount.id = "scene-html-root";

  const env = createContext({
    elements: [mount],
    devicePixelRatio: 2,
    manifest: {
      engines: [
        {
          id: "gosx-engine-html",
          component: "GoSXScene3D",
          kind: "surface",
          mountId: "scene-html-root",
          props: {
            width: 640,
            height: 360,
            background: "#08151f",
            autoRotate: false,
            scene: {
              objects: [
                { id: "hero", kind: "box", width: 1.4, height: 1.1, depth: 0.9, x: 0, y: 0, z: 0.3, color: "#8de1ff" },
              ],
              html: [
                {
                  id: "hud-card",
                  target: "hero",
                  mode: "texture",
                  className: "hud-card",
                  html: '<section class="hud"><strong>HTML</strong> <span>scene</span></section>',
                  x: 0,
                  y: 1.2,
                  z: 0.2,
                  width: 1.6,
                  height: 0.8,
                  pointerEvents: "auto",
                  priority: 4,
                },
              ],
            },
            camera: { x: 0, y: 0, z: 6, fov: 72 },
          },
          capabilities: ["canvas"],
        },
      ],
    },
  });
  env.document.styleSheets = [
    { cssRules: [{ type: 1, cssText: ".hud { display:grid; color:#8de1ff; }" }] },
  ];

  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();

  const labelLayer = mount.children[1];
  const html = labelLayer.children.find((child) => child.getAttribute("data-gosx-scene-html") === "hud-card");
  assert.equal(mount.getAttribute("data-gosx-scene3d-renderer"), "canvas");
  assert.equal(labelLayer.getAttribute("data-gosx-scene3d-label-layer"), "true");
  assert.equal(labelLayer.getAttribute("aria-hidden"), "false");
  assert.ok(html);
  assert.equal(html.getAttribute("class"), "gosx-scene-html hud-card");
  assert.equal(html.getAttribute("data-gosx-scene-html-target"), "hero");
  assert.equal(html.getAttribute("data-gosx-scene-html-mode"), "texture");
  assert.equal(html.getAttribute("data-gosx-scene-html-fallback"), "dom-overlay");
  assert.equal(html.getAttribute("data-gosx-scene-html-fallback-reason"), "html-texture-accessibility-mirror");
  assert.ok(html.getAttribute("data-gosx-scene-html-texture-key").startsWith("data:image/svg+xml;charset=utf-8,"));
  assert.equal(html.getAttribute("data-gosx-scene-html-texture-width"), "512");
  assert.equal(html.getAttribute("data-gosx-scene-html-texture-height"), "320");
  assert.equal(html.getAttribute("data-gosx-scene-html-texture-bytes"), "655360");
  assert.equal(html.getAttribute("data-gosx-scene-html-texture-cap-bytes"), "4194304");
  assert.equal(html.getAttribute("data-gosx-scene-html-texture-ready"), "true");
  assert.equal(html.getAttribute("data-gosx-scene-html-texture-revision"), "1");
  assert.equal(html.getAttribute("data-gosx-scene-html-texture-dirty"), "false");
  assert.equal(html.getAttribute("data-gosx-scene-html-texture-dirty-bytes"), null);
  assert.equal(html.getAttribute("data-gosx-scene-html-texture-upload-pending-bytes"), "2621440");
  assert.equal(html.getAttribute("data-gosx-scene-html-texture-manager"), "svg-foreignobject");
  assert.equal(html.getAttribute("data-gosx-scene-html-texture-rasterized"), "true");
  assert.equal(html.getAttribute("data-gosx-scene-html-texture-renderer-ready"), "false");
  assert.equal(html.getAttribute("data-gosx-scene-html-texture-upload-state"), "pending");
  assert.equal(html.getAttribute("data-gosx-scene-html-texture-upload-failed"), "false");
  assert.equal(html.getAttribute("data-gosx-scene-html-texture-upload-bytes"), "2621440");
  assert.equal(html.getAttribute("data-gosx-scene-html-texture-raster-width"), "1024");
  assert.equal(html.getAttribute("data-gosx-scene-html-texture-raster-height"), "640");
  assert.equal(html.getAttribute("data-gosx-scene-html-texture-pixel-ratio"), "2");
  assert.equal(html.getAttribute("data-gosx-scene-html-texture-style-sheets"), "1");
  assert.equal(html.getAttribute("data-gosx-scene-html-texture-font-state"), "unavailable");
  assert.equal(html.getAttribute("data-gosx-scene-html-texture-mirror"), "false");
  assert.match(
    decodeURIComponent(html.getAttribute("data-gosx-scene-html-texture-key")),
    /\.hud \{ display:grid; color:#8de1ff; \}/,
  );
  assert.equal(labelLayer.getAttribute("data-gosx-scene-html-texture-count"), "1");
  assert.equal(labelLayer.getAttribute("data-gosx-scene-html-texture-ready"), "1");
  assert.equal(labelLayer.getAttribute("data-gosx-scene-html-texture-bytes"), "655360");
  assert.equal(labelLayer.getAttribute("data-gosx-scene-html-texture-dirty"), null);
  assert.equal(labelLayer.getAttribute("data-gosx-scene-html-texture-dirty-bytes"), null);
  assert.equal(labelLayer.getAttribute("data-gosx-scene-html-texture-upload-pending-bytes"), "2621440");
  assert.equal(labelLayer.getAttribute("data-gosx-scene-html-texture-revision"), "1");
  assert.equal(html.getAttribute("data-gosx-scene-html-pointer-events"), "auto");
  assert.equal(html.getAttribute("aria-hidden"), "false");
  assert.equal(html.innerHTML, '<section class="hud"><strong>HTML</strong> <span>scene</span></section>');
  assert.equal(html.textContent, "HTML scene");
  assert.equal(html.style["--gosx-scene-html-pointer-events"], "auto");
  assert.equal(env.context.__gosx_scene3d_html.schema, "gosx.scene3d.html.v1");
  assert.equal(env.context.__gosx_scene3d_html.invalidate("hud-card"), true);
  assert.equal(env.context.__gosx_scene3d_html.invalidateStyles(), true);
  assert.deepEqual(
    JSON.parse(JSON.stringify(env.context.__gosx_scene3d_html.styleState())),
    {
      revision: 1,
      styleSheets: 1,
      blockedSheets: 0,
      fontFaces: 0,
      fontBytes: 0,
      fontState: "unavailable",
    },
  );
  assert.equal(env.consoleLogs.error.length, 0);
});

test("bootstrap rerasterizes static HTML textures after async webfonts settle", async () => {
  const mount = new FakeElement("div", null);
  mount.id = "scene-html-font-root";
  const disposedMount = new FakeElement("div", null);
  disposedMount.id = "scene-html-font-disposed-root";
  let resolveFont;
  const fontResponse = new Promise((resolve) => {
    resolveFont = resolve;
  });
  const env = createContext({
    elements: [mount, disposedMount],
    fetchRoutes: {
      "/fonts/panel.woff2": () => fontResponse,
    },
    manifest: {
      engines: [
        {
          id: "gosx-engine-html-font",
          component: "GoSXScene3D",
          kind: "surface",
          mountId: "scene-html-font-root",
          props: {
            width: 640,
            height: 360,
            autoRotate: false,
            scene: {
              html: [
                {
                  id: "font-panel",
                  mode: "texture",
                  className: "font-panel",
                  html: "<strong>Async font</strong>",
                  width: 1.6,
                  height: 0.8,
                },
              ],
            },
            camera: { x: 0, y: 0, z: 6, fov: 72 },
          },
          capabilities: ["canvas"],
        },
        {
          id: "gosx-engine-html-font-disposed",
          component: "GoSXScene3D",
          kind: "surface",
          mountId: "scene-html-font-disposed-root",
          props: {
            width: 320,
            height: 180,
            autoRotate: false,
            scene: {
              html: [
                {
                  id: "disposed-font-panel",
                  mode: "texture",
                  html: "<strong>Disposed font</strong>",
                  width: 1,
                  height: 0.5,
                },
              ],
            },
          },
          capabilities: ["canvas"],
        },
      ],
    },
  });
  env.context.btoa = (value) => Buffer.from(value, "binary").toString("base64");
  env.context.window.__gosx_scene3d_perf = true;
  env.document.styleSheets = [
    {
      cssRules: [
        { type: 5, cssText: '@font-face { font-family: "Panel"; src: url("/fonts/panel.woff2"); }' },
        { type: 1, cssText: '.font-panel { font-family: "Panel"; }' },
      ],
    },
  ];

  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();

  const labelLayer = mount.children[1];
  const html = labelLayer.children.find((child) => child.getAttribute("data-gosx-scene-html") === "font-panel");
  const loadingRevision = Number(html.getAttribute("data-gosx-scene-html-texture-revision"));
  const loadingKey = html.getAttribute("data-gosx-scene-html-texture-key");
  assert.equal(html.getAttribute("data-gosx-scene-html-texture-font-state"), "loading");
  assert.equal(mount.__gosxScene3DScheduleCounts["schedule:html-texture-fonts"], undefined);
  env.context.__gosx_dispose_engine("gosx-engine-html-font-disposed");
  const disposedFontSchedules = disposedMount.__gosxScene3DScheduleCounts["schedule:html-texture-fonts"] || 0;

  resolveFont({ bytes: [0, 1, 2, 3, 4, 5] });
  for (
    let attempt = 0;
    attempt < 8 && env.context.__gosx_scene3d_html.styleState().fontState !== "ready";
    attempt += 1
  ) {
    await flushAsyncWork();
  }
  assert.equal(env.context.__gosx_scene3d_html.styleState().fontState, "ready");
  await flushAsyncWork();

  assert.equal(
    html.getAttribute("data-gosx-scene-html-texture-font-state"),
    "ready",
    JSON.stringify({
      styleState: env.context.__gosx_scene3d_html.styleState(),
      scheduleCounts: mount.__gosxScene3DScheduleCounts,
    }),
  );
  assert.equal(Number(html.getAttribute("data-gosx-scene-html-texture-revision")), loadingRevision + 1);
  assert.notEqual(html.getAttribute("data-gosx-scene-html-texture-key"), loadingKey);
  assert.ok(
    mount.__gosxScene3DScheduleCounts["schedule:html-texture-fonts"] >= 1,
    "a static scene must schedule a frame after asynchronous fonts settle",
  );
  assert.equal(
    disposedMount.__gosxScene3DScheduleCounts["schedule:html-texture-fonts"] || 0,
    disposedFontSchedules,
    "font settlement must not retain or wake a disposed texture state",
  );
  assert.match(
    decodeURIComponent(html.getAttribute("data-gosx-scene-html-texture-key")),
    /data:font\/woff2;base64,AAECAwQF/,
  );
  assert.equal(env.consoleLogs.error.length, 0);
});

test("bootstrap clips HTML texture mirrors only after renderer upload succeeds", async () => {
  const mount = new FakeElement("div", null);
  mount.id = "scene-html-upload-root";
  const env = createContext({
    elements: [mount],
    manifest: {
      engines: [
        {
          id: "gosx-engine-html-upload",
          component: "GoSXScene3D",
          kind: "surface",
          mountId: "scene-html-upload-root",
          props: {
            width: 640,
            height: 360,
            scene: {
              html: [
                { id: "upload-ok", mode: "texture", html: "<strong>Uploaded</strong>", width: 1.6, height: 0.8 },
                { id: "upload-fails", mode: "texture", html: "<strong>Fallback</strong>", width: 1.6, height: 0.8 },
              ],
            },
            camera: { x: 0, y: 0, z: 6, fov: 72 },
          },
          capabilities: ["canvas"],
        },
      ],
    },
  });

  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();

  const labelLayer = mount.children[1];
  const uploaded = labelLayer.children.find((child) => child.getAttribute("data-gosx-scene-html") === "upload-ok");
  const fallback = labelLayer.children.find((child) => child.getAttribute("data-gosx-scene-html") === "upload-fails");
  assert.equal(uploaded.getAttribute("data-gosx-scene-html-texture-mirror"), "false");
  assert.equal(fallback.getAttribute("data-gosx-scene-html-texture-mirror"), "false");
  assert.equal(uploaded.getAttribute("data-gosx-scene-html-texture-upload-state"), "pending");
  assert.equal(fallback.getAttribute("data-gosx-scene-html-texture-upload-state"), "pending");

  const api = env.context.__gosx_scene3d_api;
  api.notifySceneTextureLoaded(uploaded.getAttribute("data-gosx-scene-html-texture-key"), true);
  api.notifySceneTextureLoaded(fallback.getAttribute("data-gosx-scene-html-texture-key"), false);
  await flushAsyncWork();

  assert.equal(uploaded.getAttribute("data-gosx-scene-html-texture-renderer-ready"), "true");
  assert.equal(uploaded.getAttribute("data-gosx-scene-html-texture-upload-state"), "ready");
  assert.equal(uploaded.getAttribute("data-gosx-scene-html-texture-upload-failed"), "false");
  assert.equal(uploaded.getAttribute("data-gosx-scene-html-texture-mirror"), "true");
  assert.equal(fallback.getAttribute("data-gosx-scene-html-texture-renderer-ready"), "false");
  assert.equal(fallback.getAttribute("data-gosx-scene-html-texture-upload-state"), "failed");
  assert.equal(fallback.getAttribute("data-gosx-scene-html-texture-upload-failed"), "true");
  assert.equal(fallback.getAttribute("data-gosx-scene-html-texture-mirror"), "false");
  assert.equal(fallback.getAttribute("aria-hidden"), "false");
  assert.equal(labelLayer.getAttribute("data-gosx-scene-html-texture-uploaded"), "1");
  assert.equal(labelLayer.getAttribute("data-gosx-scene-html-texture-upload-failed"), "1");
  assert.equal(env.consoleLogs.error.length, 0);
});

test("bootstrap emits ready HTML texture surfaces into render bundles", async () => {
  const env = createContext({});
  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();

  const api = env.context.__gosx_scene3d_api;
  const bundle = api.createSceneRenderBundle(
    640,
    360,
    "#08151f",
    { x: 0, y: 0, z: 6, fov: 72, near: 0.05, far: 128 },
    [],
    [],
    [],
    [
      api.normalizeSceneHTML({
        id: "panel",
        target: "console",
        mode: "texture",
        html: "<button>Inspect</button>",
        x: 0,
        y: 0,
        z: 0,
        surfaceWidth: 1.6,
        surfaceHeight: 0.9,
        rotationX: -Math.PI / 2,
        rotationY: 0.2,
        spinY: 0.4,
        textureWidth: 256,
        textureHeight: 128,
        textureReady: true,
        pointerEvents: "auto",
      }, 0, null),
    ],
    [],
    { ambientColor: "#ffffff", ambientIntensity: 0.1 },
    0,
    [],
    [],
    [],
    [],
    [],
    921600,
  );

  assert.equal(bundle.html.length, 1);
  assert.equal(bundle.html[0].textureBytes, 131072);
  assert.equal(bundle.html[0].textureReady, true);
  assert.equal(bundle.html[0].rotationX, -Math.PI / 2);
  assert.equal(bundle.html[0].rotationY, 0.2);
  assert.equal(bundle.html[0].spinY, 0.4);
  assert.equal(bundle.surfaces.length, 1);
  assert.equal(bundle.surfaces[0].sourceKind, "html");
  assert.equal(bundle.surfaces[0].sourceID, "panel");
  assert.equal(bundle.surfaces[0].textureKey, "gosx-html://panel");
  assert.equal(bundle.surfaces[0].textureBytes, 131072);
  assert.equal(bundle.surfaces[0].textureReady, true);
  assert.equal(bundle.surfaces[0].contentWidth, 256);
  assert.equal(bundle.surfaces[0].contentHeight, 128);
  assert.equal(new Set(Array.from(bundle.surfaces[0].positions, (value) => value.toFixed(9))).size > 2, true);
  assert.equal(bundle.materials[bundle.surfaces[0].materialIndex].texture, "gosx-html://panel");

  const pending = api.createSceneRenderBundle(
    640,
    360,
    "#08151f",
    { x: 0, y: 0, z: 6, fov: 72, near: 0.05, far: 128 },
    [],
    [],
    [],
    [
      api.normalizeSceneHTML({
        id: "pending-panel",
        mode: "texture",
        html: "<button>Pending</button>",
        textureReady: false,
        textureWidth: 4096,
        textureHeight: 4096,
        maxTexturePixels: 1024,
      }, 0, null),
    ],
    [],
    { ambientColor: "#ffffff", ambientIntensity: 0.1 },
    0,
    [],
    [],
    [],
    [],
    [],
    921600,
  );
  assert.equal(pending.surfaces.length, 0);
  assert.equal(pending.html[0].textureOverBudget, true);
  assert.equal(pending.html[0].fallbackReason, "html-texture-memory-cap");
});

test("bootstrap keeps solid mesh bundles off the legacy wire-line path", async () => {
  const env = createContext({});
  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();

  const api = env.context.__gosx_scene3d_api;
  const camera = { x: 0, y: 0, z: 4, fov: 60, near: 0.05, far: 128 };
  const baseMesh = {
    id: "solid-gltf",
    kind: "gltf-mesh",
    materialKind: "standard",
    color: "#d8b4fe",
    wireframe: false,
    vertices: {
      positions: new Float32Array([
        -0.6, 0, 0,
        0.6, 0, 0,
        0, 1, 0,
      ]),
      normals: new Float32Array([
        0, 0, 1,
        0, 0, 1,
        0, 0, 1,
      ]),
      uvs: new Float32Array([0, 0, 1, 0, 0.5, 1]),
      tangents: new Float32Array([
        1, 0, 0, 1,
        1, 0, 0, 1,
        1, 0, 0, 1,
      ]),
      count: 3,
    },
  };

  const solidBundle = api.createSceneRenderBundle(
    320,
    180,
    "#08151f",
    camera,
    [baseMesh],
    [],
    [],
    [],
    [],
    {},
    0,
    [],
    [],
    [],
    [],
    [],
    0,
  );

  assert.equal(solidBundle.meshObjects.length, 1);
  assert.equal(solidBundle.objects.length, 0);
  assert.equal(solidBundle.worldVertexCount, 0);
  assert.equal(solidBundle.worldPositions.length, 0);
  assert.equal(solidBundle.worldMeshVertexCount, 3);

  const wireBundle = api.createSceneRenderBundle(
    320,
    180,
    "#08151f",
    camera,
    [Object.assign({}, baseMesh, { id: "wire-gltf", wireframe: true })],
    [],
    [],
    [],
    [],
    {},
    0,
    [],
    [],
    [],
    [],
    [],
    0,
  );

  assert.equal(wireBundle.meshObjects.length, 1);
  assert.equal(wireBundle.objects.length, 1);
  assert.equal(wireBundle.worldVertexCount, 6);
  assert.equal(wireBundle.worldLineWidths[0], 0);

  const selenaBundle = api.createSceneRenderBundle(
    320,
    180,
    "#08151f",
    camera,
    [Object.assign({}, baseMesh, {
      id: "selena-gltf",
      materialKind: "custom",
      shaderBackend: "selena",
      wireframe: true,
    })],
    [],
    [],
    [],
    [],
    {},
    0,
    [],
    [],
    [],
    [],
    [],
    0,
  );

  assert.equal(selenaBundle.meshObjects.length, 1);
  assert.equal(selenaBundle.objects.length, 0);
  assert.equal(selenaBundle.worldVertexCount, 0);
});

test("bootstrap skips invisible Scene3D mesh objects before packing render bundles", async () => {
  const env = createContext({});
  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();

  const api = env.context.__gosx_scene3d_api;
  const camera = { x: 0, y: 0, z: 4, fov: 60, near: 0.05, far: 128 };
  const vertices = {
    positions: new Float32Array([
      -0.6, 0, 0,
      0.6, 0, 0,
      0, 1, 0,
    ]),
    normals: new Float32Array([
      0, 0, 1,
      0, 0, 1,
      0, 0, 1,
    ]),
    uvs: new Float32Array([0, 0, 1, 0, 0.5, 1]),
    tangents: new Float32Array([
      1, 0, 0, 1,
      1, 0, 0, 1,
      1, 0, 0, 1,
    ]),
    count: 3,
  };

  const bundle = api.createSceneRenderBundle(
    320,
    180,
    "#08151f",
    camera,
    [
      { id: "visible", kind: "gltf-mesh", materialKind: "standard", color: "#d8b4fe", wireframe: false, vertices },
      { id: "opacity-hidden", kind: "gltf-mesh", materialKind: "standard", color: "#d8b4fe", opacity: 0, wireframe: false, vertices },
      { id: "scale-hidden", kind: "gltf-mesh", materialKind: "standard", color: "#d8b4fe", scaleX: 0.001, scaleY: 0.001, scaleZ: 0.001, wireframe: false, vertices },
      { id: "explicit-hidden", kind: "gltf-mesh", materialKind: "standard", color: "#d8b4fe", visible: false, wireframe: false, vertices },
      { id: "model-hidden", kind: "gltf-mesh", materialKind: "standard", color: "#d8b4fe", _modelHidden: true, wireframe: false, vertices },
    ],
    [],
    [],
    [],
    [],
    {},
    0,
    [],
    [],
    [],
    [],
    [],
    0,
  );

  assert.equal(bundle.meshObjects.length, 1);
  assert.equal(bundle.meshObjects[0].id, "visible");
  assert.equal(bundle.worldMeshVertexCount, 3);
  assert.equal(bundle.worldMeshPositions.length, 9);
});

test("bootstrap preserves model-hidden Scene3D mesh state through normalization", async () => {
  const env = createContext({});
  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();

  const api = env.context.__gosx_scene3d_api;
  const hidden = api.normalizeSceneObject({
    id: "model-hidden",
    kind: "cube",
    _modelHidden: true,
  }, 0);
  assert.equal(hidden._modelHidden, true);

  const retained = api.normalizeSceneObject({ id: "model-hidden", kind: "cube" }, 0, hidden);
  assert.equal(retained._modelHidden, true);

  const cleared = api.normalizeSceneObject({
    id: "model-hidden",
    kind: "cube",
    _modelHidden: false,
  }, 0, hidden);
  assert.equal(cleared._modelHidden, false);
});

test("bootstrap emits declarative Scene3D pick signals without authored JS", async () => {
  const mount = new FakeElement("div", null);
  mount.id = "scene-pick-root";

  const env = createContext({
    elements: [mount],
    fetchRoutes: {
      "/runtime.wasm": { bytes: [0, 97, 115, 109] },
      "/scene-pick-program.json": { text: '{"name":"ScenePick"}' },
    },
    manifest: {
      runtime: { path: "/runtime.wasm" },
      engines: [
        {
          id: "gosx-engine-pick",
          component: "GoSXScene3D",
          kind: "surface",
          mountId: "scene-pick-root",
          runtime: "shared",
          programRef: "/scene-pick-program.json",
          props: {
            width: 640,
            height: 360,
            background: "#08151f",
            pickSignalNamespace: "$scene.pick",
            eventSignalNamespace: "$scene.event",
            camera: { x: 0, y: 0, z: 6, fov: 72 },
          },
        },
      ],
    },
    onHydrateEngine: () => "[]",
    onRenderEngine: () => JSON.stringify({
      background: "#08151f",
      camera: { z: 6, fov: 72 },
      positions: [],
      colors: [],
      vertexCount: 0,
      worldPositions: [
        -2.4, -1.5, 0.1, 2.4, -1.5, 0.1,
        -0.8, 0.2, 0.5, 0.7, 0.9, 1.1,
      ],
      worldColors: [
        0.25, 0.33, 0.41, 1, 0.25, 0.33, 0.41, 1,
        0.78, 0.92, 1, 1, 0.78, 0.92, 1, 1,
      ],
      worldVertexCount: 4,
      materials: [
        { kind: "flat", color: "#35556a", opacity: 1, wireframe: true, blendMode: "opaque", emissive: 0 },
        { kind: "flat", color: "#8de1ff", opacity: 1, wireframe: true, blendMode: "opaque", emissive: 0 },
      ],
      objects: [
        {
          id: "floor",
          kind: "plane",
          pickable: false,
          materialIndex: 0,
          vertexOffset: 0,
          vertexCount: 2,
          static: true,
          bounds: { minX: -2.4, minY: -1.5, minZ: 0.1, maxX: 2.4, maxY: -1.5, maxZ: 0.1 },
        },
        {
          id: "shape",
          kind: "box",
          pickable: true,
          materialIndex: 1,
          vertexOffset: 2,
          vertexCount: 2,
          static: false,
          bounds: { minX: -0.8, minY: 0.2, minZ: 0.5, maxX: 0.7, maxY: 0.9, maxZ: 1.1 },
        },
      ],
      html: [
        {
          id: "shape-panel",
          target: "shape",
          mode: "texture",
          html: "<button>Inspect</button>",
          position: { x: 320, y: 160 },
          depth: 5.5,
        rayOriginX: 0,
        rayOriginY: 0,
        rayOriginZ: 0,
        rayDirX: 0,
        rayDirY: 0,
        rayDirZ: 0,
          width: 200,
          height: 100,
          textureWidth: 256,
          textureHeight: 128,
          pointerEvents: "auto",
          fallback: "dom-overlay",
          fallbackReason: "html-texture-manager-unavailable",
        },
      ],
      objectCount: 2,
    }),
  });
  const interactionEvents = [];
  env.document.addEventListener("gosx:engine:scene-interaction", (event) => interactionEvents.push(event.detail));

  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();
  await flushAsyncWork();

  const canvas = mount.children[0];
  assert.equal(mount.getAttribute("data-gosx-scene3d-pick-signals"), "$scene.pick");
  assert.equal(mount.getAttribute("data-gosx-scene3d-event-signals"), "$scene.event");
  const labelLayer = mount.children[1];
  const htmlSurface = labelLayer.children.find((child) => child.getAttribute("data-gosx-scene-html") === "shape-panel");
  assert.ok(htmlSurface);
  assert.equal(htmlSurface.getAttribute("data-gosx-scene-html-target"), "shape");
  const textureEvents = [];
  htmlSurface.addEventListener("gosx:scene-html-texture-pointer", (event) => textureEvents.push(event.detail));

  canvas.dispatchEvent({
    type: "pointermove",
    button: 0,
    pointerId: 4,
    clientX: 320,
    clientY: 160,
    preventDefault() {},
    stopPropagation() {},
  });
  await flushAsyncWork();
  await flushAsyncWork();

  let batch = JSON.parse(env.inputBatchCalls[env.inputBatchCalls.length - 1][0]);
  assert.equal(batch["$scene.pick.hovered"], true);
  assert.equal(batch["$scene.pick.hoverIndex"], 1);
  assert.equal(batch["$scene.pick.hoverID"], "shape");
  assert.equal(batch["$scene.pick.selected"], false);
  assert.equal(batch["$scene.event.revision"], 1);
  assert.equal(batch["$scene.event.type"], "hover");
  assert.equal(batch["$scene.event.targetIndex"], 1);
  assert.equal(batch["$scene.event.targetID"], "shape");
  assert.equal(batch["$scene.event.targetKind"], "box");
  assert.equal(batch["$scene.event.targetInstanceIndex"], -1);
  assert.equal(batch["$scene.event.targetPrimitiveIndex"], -1);
  assert.equal(batch["$scene.event.targetTriangleIndex"], -1);
  assert.equal(batch["$scene.event.worldX"], 0);
  assert.equal(batch["$scene.event.localX"], 0);
  assert.equal(batch["$scene.event.uvX"], 0);
  assert.ok(Math.abs(batch["$scene.event.depth"] - 5.2) < 1e-6, "expected fallback pick depth");
  assert.equal(batch["$scene.event.hovered"], true);
  assert.equal(batch["$scene.event.hoverKind"], "box");
  assert.equal(batch["$scene.event.object.shape.hovered"], true);
  assert.equal(textureEvents.length, 1);
  assert.equal(textureEvents[0].htmlID, "shape-panel");
  assert.equal(textureEvents[0].targetID, "shape");
  assert.equal(textureEvents[0].type, "hover");
  assert.equal(textureEvents[0].fallback, "dom-overlay");
  assert.equal(textureEvents[0].fallbackReason, "html-texture-accessibility-mirror");
  assert.equal(textureEvents[0].uvX, 0);
  assert.equal(textureEvents[0].localX, 0);
  assert.equal(htmlSurface.getAttribute("data-gosx-scene-html-hit-target"), "shape");
  assert.equal(htmlSurface.getAttribute("data-gosx-scene-html-hit-uv-x"), "0");
  const debugPick = env.context.__gosx_scene3d_debug.getLastPick("scene-pick-root");
  assert.equal(debugPick.type, "hover");
  assert.equal(debugPick.targetID, "shape");
  assert.equal(debugPick.uvX, 0);
  const hoverEvent = JSON.parse(JSON.stringify(interactionEvents[0]));
  assert.ok(Math.abs(hoverEvent.detail.depth - 5.2) < 1e-6, "expected fallback event depth");
  hoverEvent.detail.depth = 5.2;
  assert.deepEqual(hoverEvent, {
    engineID: "gosx-engine-pick",
    component: "GoSXScene3D",
    detail: {
      type: "hover",
      revision: 1,
      targetIndex: 1,
      targetID: "shape",
      targetKind: "box",
      targetInstanceIndex: -1,
      targetPrimitiveIndex: -1,
      targetTriangleIndex: -1,
      worldX: 0,
      worldY: 0,
      worldZ: 0,
      localX: 0,
      localY: 0,
      localZ: 0,
      uvX: 0,
      uvY: 0,
      depth: 5.2,
        rayOriginX: 0,
        rayOriginY: 0,
        rayOriginZ: 0,
        rayDirX: 0,
        rayDirY: 0,
        rayDirZ: 0,
      hovered: true,
      hoverIndex: 1,
      hoverID: "shape",
      hoverKind: "box",
      down: false,
      downIndex: -1,
      downID: "",
      downKind: "",
      selected: false,
      selectedIndex: -1,
      selectedID: "",
      selectedKind: "",
      clickCount: 0,
      pointerX: 320,
      pointerY: 160,
    },
  });
  const hoverBatchCount = env.inputBatchCalls.length;

  canvas.dispatchEvent({
    type: "pointermove",
    button: 0,
    pointerId: 4,
    clientX: 320,
    clientY: 160,
    preventDefault() {},
    stopPropagation() {},
  });
  await flushAsyncWork();
  await flushAsyncWork();

  assert.equal(env.inputBatchCalls.length, hoverBatchCount);

  canvas.dispatchEvent({
    type: "pointerdown",
    button: 0,
    pointerId: 4,
    clientX: 320,
    clientY: 160,
    preventDefault() {},
    stopPropagation() {},
  });
  await flushAsyncWork();

  batch = JSON.parse(env.inputBatchCalls[env.inputBatchCalls.length - 1][0]);
  assert.equal(batch["$scene.pick.down"], true);
  assert.equal(batch["$scene.pick.downID"], "shape");
  assert.equal(batch["$scene.event.type"], "down");
  assert.equal(batch["$scene.event.down"], true);
  assert.equal(batch["$scene.event.downID"], "shape");
  assert.equal(batch["$scene.event.object.shape.down"], true);

  canvas.dispatchEvent({
    type: "pointerup",
    button: 0,
    pointerId: 4,
    clientX: 320,
    clientY: 160,
    preventDefault() {},
    stopPropagation() {},
  });
  await flushAsyncWork();

  batch = JSON.parse(env.inputBatchCalls[env.inputBatchCalls.length - 1][0]);
  assert.equal(batch["$scene.pick.down"], false);
  assert.equal(batch["$scene.pick.selected"], true);
  assert.equal(batch["$scene.pick.selectedIndex"], 1);
  assert.equal(batch["$scene.pick.selectedID"], "shape");
  assert.equal(batch["$scene.pick.clickCount"], 1);
  assert.equal(batch["$scene.event.type"], "select");
  assert.equal(batch["$scene.event.selected"], true);
  assert.equal(batch["$scene.event.selectedID"], "shape");
  assert.equal(batch["$scene.event.object.shape.down"], false);
  assert.equal(batch["$scene.event.object.shape.selected"], true);
  assert.equal(batch["$scene.event.object.shape.clickCount"], 1);

  canvas.dispatchEvent({
    type: "pointermove",
    button: 0,
    pointerId: 5,
    clientX: 48,
    clientY: 332,
    preventDefault() {},
    stopPropagation() {},
  });
  await flushAsyncWork();

  batch = JSON.parse(env.inputBatchCalls[env.inputBatchCalls.length - 1][0]);
  assert.equal(batch["$scene.pick.hovered"], false);
  assert.equal(batch["$scene.pick.hoverIndex"], -1);
  assert.equal(batch["$scene.pick.hoverID"], "");
  assert.equal(batch["$scene.pick.selectedID"], "shape");
  assert.equal(batch["$scene.event.type"], "leave");
  assert.equal(batch["$scene.event.hovered"], false);
  assert.equal(batch["$scene.event.object.shape.hovered"], false);

  canvas.dispatchEvent({
    type: "pointerdown",
    button: 0,
    pointerId: 5,
    clientX: 48,
    clientY: 332,
    preventDefault() {},
    stopPropagation() {},
  });
  canvas.dispatchEvent({
    type: "pointerup",
    button: 0,
    pointerId: 5,
    clientX: 48,
    clientY: 332,
    preventDefault() {},
    stopPropagation() {},
  });
  await flushAsyncWork();

  batch = JSON.parse(env.inputBatchCalls[env.inputBatchCalls.length - 1][0]);
  assert.equal(batch["$scene.pick.selected"], false);
  assert.equal(batch["$scene.pick.selectedIndex"], -1);
  assert.equal(batch["$scene.pick.selectedID"], "");
  assert.equal(batch["$scene.pick.clickCount"], 1);
  assert.equal(batch["$scene.event.type"], "deselect");
  assert.equal(batch["$scene.event.selected"], false);
  assert.equal(batch["$scene.event.object.shape.selected"], false);
  assert.equal(interactionEvents.at(-1).detail.type, "deselect");
});

test("bootstrap stamps and validates Scene3D IR bundles", async () => {
  const env = createContext({});
  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();

  const api = env.context.__gosx_scene3d_api;
  assert.equal(api.SCENE_IR_VERSION, 1);
  assert.equal(typeof api.validateSceneIR, "function");

  const bundle = api.createSceneRenderBundle(
    320,
    180,
    "#08151f",
    { x: 0, y: 0, z: 6, fov: 72, near: 0.05, far: 128 },
    [],
    [],
    [],
    [],
    [],
    { ambientColor: "#ffffff", ambientIntensity: 0.1 },
    0,
    [],
    [],
    [],
    [],
    [],
    921600,
  );
  assert.equal(bundle.bundleVersion, api.SCENE_RENDER_BUNDLE_VERSION);
  assert.equal(bundle.postFXMaxPixels, 921600);
  assert.equal(bundle.vertexCount, 0);
  assert.equal(bundle.lines.length, 0);
  assert.equal(JSON.stringify(api.validateSceneIR(bundle)), JSON.stringify({ valid: true, errors: [] }));

  const gridBundle = api.createSceneRenderBundle(
    96,
    96,
    "#08151f",
    { x: 0, y: 0, z: 6, fov: 72, near: 0.05, far: 128 },
    [],
    [],
    [],
    [],
    [],
    { ambientColor: "#ffffff", ambientIntensity: 0.1 },
    0,
    [],
    [],
    [],
    [],
    [],
    0,
    true,
  );
  assert.equal(gridBundle.lines.length, 6);
  assert.equal(gridBundle.vertexCount, 12);

  const invalid = api.validateSceneIR({
    version: 1,
    camera: { near: 2, far: 1 },
    environment: {},
    nodes: [{ kind: "points", points: { count: -1 } }],
  });
  assert.equal(invalid.valid, false);
  assert.ok(invalid.errors.some((entry) => entry.includes("camera.far")));
  assert.ok(invalid.errors.some((entry) => entry.includes("points.count")));

  const emptyCanonical = api.validateSceneIR({
    schema: api.SCENE_IR_SCHEMA,
    version: 1,
    camera: { near: 0.05, far: 128 },
    environment: { fogDensity: 0.001 },
    lights: [{ kind: "directional" }],
  });
  assert.equal(JSON.stringify(emptyCanonical), JSON.stringify({ valid: true, errors: [] }));

  const badSchema = api.validateSceneIR({
    schema: "gosx.scene3d.ir.v0",
    version: 1,
    camera: { near: 0.05, far: 128 },
  });
  assert.equal(badSchema.valid, false);
  assert.ok(badSchema.errors.some((entry) => entry.includes("schema must be gosx.scene3d.ir.v1")));

  const mismatched = api.validateSceneIR({
    version: 1,
    camera: { near: 0.05, far: 128 },
    nodes: [{ kind: "mesh", points: { count: 1 } }],
  });
  assert.equal(mismatched.valid, false);
  assert.ok(mismatched.errors.some((entry) => entry.includes(".mesh is required")));

  const negativeInstanced = api.validateSceneIR({
    version: 1,
    camera: { near: 0.05, far: 128 },
    nodes: [{ kind: "instanced-mesh", instancedMesh: { count: -1 } }],
  });
  assert.equal(negativeInstanced.valid, false);
  assert.ok(negativeInstanced.errors.some((entry) => entry.includes("instancedMesh.count")));
});

test("bootstrap applies Scene3D post-effect diff commands", async () => {
  const env = createContext({});
  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();

  const api = env.context.__gosx_scene3d_api;
  const state = api.createSceneState({
    scene: {
      postEffects: [{ kind: "bloom", threshold: 0.8, intensity: 0.2 }],
      postFXMaxPixels: 4096,
    },
  });

  api.applySceneCommands(state, [{
    kind: 7,
    data: {
      postEffects: [{ kind: "dof", focusDistance: 6, aperture: 0.08, maxBlur: 5 }],
      postFXMaxPixels: 16384,
    },
  }]);

  assert.equal(state.postEffects.length, 1);
  assert.equal(state.postEffects[0].kind, "dof");
  assert.equal(state.postEffects[0].focusDistance, 6);
  assert.equal(state.postEffects[0].aperture, 0.08);
  assert.equal(state.postEffects[0].maxBlur, 5);
  assert.equal(state.postFXMaxPixels, 16384);
  assert.equal(state._deferredPostEffects, null);
  assert.deepEqual(state._adaptiveSourcePostEffects, state.postEffects);
});

// Regression pin: normalizeScenePostEffect must hand the render backends the
// EXACT kind spelling their `switch (effect.kind)` chains compare against.
// It used to lowercase kind, which rewrote the three camelCase kinds
// ("customPost", "toneMapping", "colorGrade") into forms that matched no case
// in either backend, so those passes silently became no-ops while every health
// signal (effect present in state, post-effect count attribute, post chain
// running) still read green.
test("Scene3D post-effect kinds keep their canonical camelCase spelling through normalization", async () => {
  const env = createContext({});
  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();

  const api = env.context.__gosx_scene3d_api;
  // The canonical spellings the backends dispatch on, exported by the scene API.
  assert.equal(api.SCENE_POST_CUSTOM_POST, "customPost");
  assert.equal(api.SCENE_POST_TONE_MAPPING, "toneMapping");
  assert.equal(api.SCENE_POST_COLOR_GRADE, "colorGrade");

  // (a) The author path (props → createSceneState).
  const state = api.createSceneState({
    scene: {
      postEffects: [
        { kind: "customPost", name: "galaxy-liquid-glass", fragmentWGSL: "//frag", vertexWGSL: "//vert" },
        { kind: "toneMapping", exposure: 1.2, mode: "aces" },
        { kind: "colorGrade", exposure: 1, contrast: 1.1 },
        { kind: "bloom", threshold: 0.8 },
        { kind: "ssao", radius: 4 },
      ],
    },
  });
  // Array.from re-homes the vm-context array into this realm so deepStrictEqual
  // compares contents rather than cross-realm Array prototypes.
  assert.deepEqual(
    Array.from(state.postEffects, (effect) => effect.kind),
    [api.SCENE_POST_CUSTOM_POST, api.SCENE_POST_TONE_MAPPING, api.SCENE_POST_COLOR_GRADE, "bloom", "ssao"],
    "createSceneState must preserve the canonical kind spelling for every effect",
  );
  assert.equal(state.postEffects[0].fragmentWGSL, "//frag", "the custom pass keeps its authored shader");

  // (b) The command path (SCENE_CMD_SET_POST_EFFECTS re-normalizes from scratch).
  api.applySceneCommands(state, [{
    kind: 7,
    data: {
      postEffects: [
        { kind: "customPost", name: "galaxy-liquid-glass", fragmentWGSL: "//frag" },
        { kind: "toneMapping", exposure: 1.0 },
      ],
    },
  }]);
  assert.deepEqual(
    Array.from(state.postEffects, (effect) => effect.kind),
    [api.SCENE_POST_CUSTOM_POST, api.SCENE_POST_TONE_MAPPING],
    "applySceneCommands must preserve the canonical kind spelling too",
  );

  // (c) Author spelling is still matched case-insensitively and folded back to
  // canonical, so existing lowercase/hyphenated authoring keeps working.
  const aliased = api.createSceneState({
    scene: {
      postEffects: [
        { kind: "custompost", name: "a", fragmentWGSL: "//f" },
        { kind: "CustomPost", name: "b", fragmentWGSL: "//f" },
        { kind: "tonemapping" },
        { kind: "color-grade" },
      ],
    },
  });
  assert.deepEqual(
    Array.from(aliased.postEffects, (effect) => effect.kind),
    ["customPost", "customPost", "toneMapping", "colorGrade"],
    "aliases must fold to the canonical spelling the backends dispatch on",
  );

  // (d) An unknown kind keeps the author's exact spelling rather than being
  // case-folded into something no backend can dispatch.
  const unknown = api.createSceneState({ scene: { postEffects: [{ kind: "myCoolPass" }] } });
  assert.equal(unknown.postEffects[0].kind, "myCoolPass");
});

test("bootstrap applies Scene3D environment diff commands", async () => {
  const env = createContext({});
  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();

  const api = env.context.__gosx_scene3d_api;
  const state = api.createSceneState({
    scene: {
      environment: { ambientColor: "#ffffff", ambientIntensity: 0.1, exposure: 1 },
    },
  });

  api.applySceneCommands(state, [{
    kind: 13,
    data: {
      environment: {
        ambientColor: "#f5fbff",
        ambientIntensity: 0.35,
        exposure: 1.2,
        toneMapping: "aces",
      },
    },
  }]);

  assert.equal(state.environment.ambientColor, "#f5fbff");
  assert.equal(state.environment.ambientIntensity, 0.35);
  assert.equal(state.environment.exposure, 1.2);
  assert.equal(state.environment.toneMapping, "aces");
});

test("bootstrap applies Scene3D particle and instanced diff commands", async () => {
  const env = createContext({});
  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();

  const api = env.context.__gosx_scene3d_api;
  const state = api.createSceneState({
    scene: {
      points: [{ id: "old-points", count: 1, positions: [0, 0, 0], color: "#ffffff" }],
      instancedMeshes: [{ id: "old-batch", kind: "box", count: 1, transforms: new Array(16).fill(0) }],
    },
  });

  api.applySceneCommands(state, [
    {
      kind: 6,
      data: {
        points: [{ id: "stars", count: 2, positions: [0, 0, 0, 1, 1, 1], color: "#77c6ff", minPixelSize: 2 }],
        computeParticles: [{ id: "field", count: 4, emitter: { kind: "sphere", radius: 2 }, material: { color: "#fff", size: 3 } }],
      },
    },
    {
      kind: 8,
      data: {
        instancedMeshes: [{ id: "debris", kind: "torusGeometry", count: 2, transforms: new Array(32).fill(0), radius: 1.2, tube: 0.2 }],
      },
    },
  ]);

  assert.equal(state.points.length, 1);
  assert.equal(state.points[0].id, "stars");
  assert.equal(state.points[0].count, 2);
  assert.equal(state.points[0].minPixelSize, 2);
  assert.equal(state.computeParticles.length, 1);
  assert.equal(state.computeParticles[0].id, "field");
  assert.equal(state.computeParticles[0].emitter.radius, 2);
  assert.equal(state.instancedMeshes.length, 1);
  assert.equal(state.instancedMeshes[0].id, "debris");
  assert.equal(state.instancedMeshes[0].kind, "torus");
  assert.equal(state.instancedMeshes[0].radius, 1.2);
  assert.equal(state.instancedMeshes[0].tube, 0.2);
});

test("bootstrap preserves ComputeParticles authored render fields through scene normalization", async () => {
  const env = createContext({});
  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();

  const api = env.context.__gosx_scene3d_api;
  const renderVertexWGSL = "@vertex fn vertexStorageMain(@builtin(vertex_index) vi: u32) -> @builtin(position) vec4<f32> { return vec4<f32>(0.0); }";
  const renderFragmentWGSL = "@fragment fn fragmentMain() -> @location(0) vec4<f32> { return vec4<f32>(1.0); }";
  const computeWGSL = "@compute @workgroup_size(1) fn simulate() {}";
  const state = api.createSceneState({
    scene: {
      computeParticles: [{
        id: "authored-compute",
        count: 8,
        emitter: { kind: "point" },
        material: { color: "#ffffff" },
        computeWGSL,
        computeEntry: "simulate",
        computeBackend: "elio",
        renderVertexWGSL,
        renderFragmentWGSL,
        renderUniforms: { brightness: 1.25 },
        renderShaderBackend: "selena",
        renderShaderLayout: {
          material: "GalaxyParticleRender",
          entryPoints: { vertexStorage: "vertexStorageMain" },
        },
      }],
    },
  });

  assert.equal(state.computeParticles.length, 1);
  const cp = state.computeParticles[0];
  assert.equal(cp.computeWGSL, computeWGSL, "computeWGSL must survive normalization");
  assert.equal(cp.computeEntry, "simulate", "computeEntry must survive normalization");
  assert.equal(cp.computeBackend, "elio", "computeBackend must survive normalization");
  assert.equal(cp.renderVertexWGSL, renderVertexWGSL, "renderVertexWGSL must survive normalization");
  assert.equal(cp.renderFragmentWGSL, renderFragmentWGSL, "renderFragmentWGSL must survive normalization");
  assert.equal(cp.renderShaderBackend, "selena", "renderShaderBackend must survive normalization");
  assert.ok(cp.renderUniforms && typeof cp.renderUniforms === "object", "renderUniforms must survive normalization");
  assert.equal(cp.renderUniforms.brightness, 1.25, "renderUniforms value must survive normalization");
  assert.ok(cp.renderShaderLayout && typeof cp.renderShaderLayout === "object", "renderShaderLayout must survive normalization");
  assert.equal(cp.renderShaderLayout.material, "GalaxyParticleRender", "renderShaderLayout.material must survive normalization");
  assert.equal(cp.renderShaderLayout.entryPoints.vertexStorage, "vertexStorageMain", "renderShaderLayout.entryPoints must survive normalization");
});

test("bootstrap applies Scene3D material diff commands", async () => {
  const env = createContext({});
  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();

  const api = env.context.__gosx_scene3d_api;
  const state = api.createSceneState({
    scene: {
      materials: [{ name: "hero", kind: "standard", color: "#ffffff" }],
      objects: [{ id: "cube", kind: "box", material: "hero" }],
    },
  }, { tier: "constrained" });

  api.applySceneCommands(state, [{
    kind: 9,
    data: {
      materials: [{
        name: "hero",
        kind: "custom",
        color: "#f5c76b",
        customFragmentWGSL: "fn gosx_fragment() -> vec4f { return vec4f(1.0); }",
        variants: {
          constrained: { color: "#94a3b8", opacity: 0.4 },
        },
      }],
    },
  }]);

  assert.equal(state.materials.length, 1);
  assert.equal(state.materials[0].kind, "custom");
  assert.equal(state.materials[0].variantKey, "constrained");
  assert.equal(state.materials[0].color, "#94a3b8");
  assert.equal(state.materials[0].opacity, 0.4);
  assert.equal(state._materialSource[0].color, "#f5c76b");

  const objects = api.sceneStateObjectsWithMaterials(state);
  assert.equal(objects[0].materialKind, "custom");
  assert.equal(objects[0].color, "#94a3b8");
  assert.equal(objects[0].customFragmentWGSL, "fn gosx_fragment() -> vec4f { return vec4f(1.0); }");
});

test("bootstrap applies Scene3D model, instanced GLB, and animation diff commands", async () => {
  const env = createContext({
    fetchRoutes: {
      "/models/card.gosx3d.json": {
        text: JSON.stringify({
          objects: [
            { id: "card", kind: "box", width: 1, height: 1, depth: 0.1, color: "#ffffff" },
          ],
        }),
      },
    },
  });
  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();

  const api = env.context.__gosx_scene3d_api;
  const state = api.createSceneState({ scene: { objects: [] } });

  await api.applySceneCommands(state, [{
    kind: 10,
    data: {
      models: [{
        id: "hero",
        src: "/models/card.gosx3d.json",
        x: 1,
        materialKind: "standard",
        roughness: 0.32,
      }],
    },
  }]);

  assert.equal(state.models.length, 1);
  assert.equal(state.models[0].id, "hero");
  assert.equal(state.objects.has("hero/card"), true);
  assert.equal(state.objects.get("hero/card").roughness, 0.32);

  await api.applySceneCommands(state, [{
    kind: 10,
    data: { models: [] },
  }]);
  assert.equal(state.models.length, 0);
  assert.equal(state.objects.has("hero/card"), false);

  await api.applySceneCommands(state, [{
    kind: 11,
    data: {
      instancedGLBMeshes: [{
        id: "batch",
        src: "/models/card.gosx3d.json",
        materialKind: "glow",
        metalness: 0.7,
        instances: [
          { id: "a", x: 0, scale: 1 },
          { id: "b", x: 2, scale: 1 },
        ],
      }],
    },
  }]);

  assert.equal(state.instancedGLBMeshes.length, 1);
  assert.equal(state.instancedGLBMeshes[0].instances.length, 2);
  assert.equal(state.objects.has("batch/a/card"), true);
  assert.equal(state.objects.has("batch/b/card"), true);
  assert.equal(state.objects.get("batch/a/card").materialKind, "glow");
  assert.equal(state.objects.get("batch/a/card").metalness, 0.7);

  api.applySceneCommands(state, [{
    kind: 12,
    data: {
      animations: [{
        name: "pulse",
        duration: 1,
        channels: [{ targetNode: "batch/a/card", property: "rotationY", times: [0, 1], values: [0, 3.14] }],
      }],
    },
  }]);
  assert.equal(state.animations.length, 1);
  assert.equal(state.animations[0].name, "pulse");
  assert.equal(state.animations[0].channels[0].targetNode, "batch/a/card");
});

test("bootstrap keeps authored zero-opacity model meshes in mesh bundle", async () => {
  const vertices = {
    positions: [
      -0.5, -0.5, 0,
       0.5, -0.5, 0,
       0.0,  0.5, 0,
    ],
    normals: [
      0, 0, 1,
      0, 0, 1,
      0, 0, 1,
    ],
    uvs: [
      0, 0,
      1, 0,
      0.5, 1,
    ],
    count: 3,
  };
  const env = createContext({
    fetchRoutes: {
      "/models/triangle.gosx3d.json": {
        text: JSON.stringify({
          objects: [{ id: "tri", kind: "mesh", vertices }],
        }),
      },
    },
  });
  runScript(bootstrapRuntimeSource, env.context, "bootstrap-runtime.js");
  runScript(freshFeatureBundleSource("scene3d"), env.context, "bootstrap-feature-scene3d.js");
  await flushAsyncWork();

  const api = env.context.__gosx_scene3d_api;
  const state = api.createSceneState({ scene: { objects: [] } });
  await api.applySceneCommands(state, [{
    kind: 10,
    data: {
      models: [
        { id: "selena", src: "/models/triangle.gosx3d.json", opacity: 0, shaderBackend: "selena" },
        {
          id: "custom",
          src: "/models/triangle.gosx3d.json",
          opacity: 0,
          customFragmentWGSL: "@fragment fn fragmentMain() -> @location(0) vec4<f32> { return vec4f(0.0, 1.0, 0.0, 1.0); }",
        },
        { id: "plain", src: "/models/triangle.gosx3d.json", opacity: 0 },
      ],
    },
  }]);

  assert.equal(state.objects.get("selena/tri")._modelHidden, false);
  assert.equal(state.objects.get("custom/tri")._modelHidden, false);
  assert.equal(state.objects.get("plain/tri")._modelHidden, true);

  const bundle = api.createSceneRenderBundle(
    64, 64, "#000000",
    { x: 0, y: 0, z: 4, fov: 60, near: 0.05, far: 128 },
    api.sceneStateObjectsWithMaterials(state), [], [], [], [], {}, 0, [], [], [], [], [], 0, false,
  );

  assert.deepEqual(Array.from(bundle.meshObjects, (object) => object.id), [
    "selena/tri",
    "custom/tri",
  ]);
  assert.equal(bundle.meshObjects.some((object) => object.id === "plain/tri"), false);
});

async function assertNewestSceneModelHydrationWins(resolveNewestFirst) {
  const routeA = createDeferredModelRoute();
  const routeB = createDeferredModelRoute();
  const env = createContext({
    fetchRoutes: {
      "/models/deferred-a.gosx3d.json": () => routeA.promise,
      "/models/deferred-b.gosx3d.json": () => routeB.promise,
    },
  });
  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();

  const api = env.context.__gosx_scene3d_api;
  const state = api.createSceneState({ scene: { objects: [] } });
  const mount = new FakeElement("div", null);
  const assetStatuses = [];
  const hydrationStatuses = [];
  mount.addEventListener("gosx:scene3d:model-status", (event) => assetStatuses.push(event.detail));
  mount.addEventListener("gosx:scene3d:model-hydration-status", (event) => hydrationStatuses.push(event.detail));
  state._modelStatusMount = mount;

  const hydrationA = api.applySceneCommands(state, [{
    kind: 10,
    data: { models: [{ id: "generation-a", src: "/models/deferred-a.gosx3d.json" }] },
  }]);
  const hydrationB = api.applySceneCommands(state, [{
    kind: 10,
    data: { models: [{ id: "generation-b", src: "/models/deferred-b.gosx3d.json" }] },
  }]);

  assert.deepEqual(Array.from(state.objects.keys()), [], "neither pending generation may mutate live records");
  if (resolveNewestFirst) {
    routeB.resolve(modelAssetJSON("mesh-b"));
    await hydrationB;
    assert.deepEqual(Array.from(state.objects.keys()), ["generation-b/mesh-b"]);
    routeA.resolve(modelAssetJSON("mesh-a"));
    await hydrationA;
  } else {
    routeA.resolve(modelAssetJSON("mesh-a"));
    await hydrationA;
    assert.deepEqual(Array.from(state.objects.keys()), [], "a superseded generation may not commit even before the newest load completes");
    routeB.resolve(modelAssetJSON("mesh-b"));
    await hydrationB;
  }

  assert.deepEqual(Array.from(state.objects.keys()), ["generation-b/mesh-b"]);
  assert.deepEqual(Array.from(state._hydratedModelRecords.objects), ["generation-b/mesh-b"]);
  assert.equal(state._modelHydrationGeneration, 2);
  assert.equal(mount.getAttribute("data-gosx-scene3d-model-hydration-status"), "committed");
  assert.equal(mount.getAttribute("data-gosx-scene3d-model-hydration-generation"), "2");
  assert.equal(mount.getAttribute("data-gosx-scene3d-model-hydration-committed"), "true");
  assert.equal(mount.getAttribute("data-gosx-scene3d-model-hydration-stale"), "false");
  assert.equal(hydrationStatuses.filter((entry) => entry.status === "committed").length, 1);
  assert.equal(hydrationStatuses.some((entry) => entry.status === "stale"), false,
    "a stale completion must not overwrite the newest mount-scoped status");
  assert.deepEqual(
    assetStatuses.filter((entry) => entry.status === "loaded").map((entry) => entry.modelID),
    ["generation-b"],
    "a stale asset completion must not publish a misleading final load status",
  );
}

test("Scene3D model hydration commits only the newest generation when B resolves before A", async () => {
  await assertNewestSceneModelHydrationWins(true);
});

test("Scene3D model hydration commits only the newest generation when A resolves before B", async () => {
  await assertNewestSceneModelHydrationWins(false);
});

test("Scene3D supersession during skin setup discards the stale mixer and live records", async () => {
  const env = createContext({
    fetchRoutes: {
      "/models/superseded-rig.glb": { bytes: buildSkinnedGLBBytes() },
      "/models/superseding-model.gosx3d.json": modelAssetJSON("latest-mesh"),
    },
  });
  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();

  const api = env.context.__gosx_scene3d_api;
  const state = api.createSceneState({ scene: { objects: [] } });
  const mount = new FakeElement("div", null);
  const hydrationStatuses = [];
  mount.addEventListener("gosx:scene3d:model-hydration-status", (event) => hydrationStatuses.push(event.detail));
  state._modelStatusMount = mount;

  const animationAPI = env.context.__gosx_scene3d_animation_api;
  const originalCreateMixer = animationAPI.createMixer;
  const originalComputeJointMatrices = animationAPI.computeJointMatrices;
  let disposedMixers = 0;
  let hydrationB = null;
  animationAPI.createMixer = function trackedStaleMixer() {
    const mixer = originalCreateMixer.call(animationAPI);
    const originalDispose = mixer.dispose;
    mixer.dispose = function disposeTrackedMixer() {
      disposedMixers += 1;
      return originalDispose.call(mixer);
    };
    return mixer;
  };
  animationAPI.computeJointMatrices = function supersedeDuringSkin(skin, transforms) {
    if (!hydrationB) {
      hydrationB = api.applySceneCommands(state, [{
        kind: 10,
        data: {
          models: [{
            id: "latest-model",
            src: "/models/superseding-model.gosx3d.json",
          }],
        },
      }]);
    }
    return originalComputeJointMatrices.call(animationAPI, skin, transforms);
  };

  try {
    const hydrationA = api.applySceneCommands(state, [{
      kind: 10,
      data: {
        models: [{
          id: "stale-rig",
          src: "/models/superseded-rig.glb",
          animation: "bend",
        }],
      },
    }]);
    await hydrationA;
    assert.ok(hydrationB, "skin setup must trigger the superseding generation");
    await hydrationB;

    assert.equal(disposedMixers, 1, "the superseded generation's staged mixer must be disposed");
    assert.deepEqual(Array.from(state.objects.keys()), ["latest-model/latest-mesh"]);
    assert.deepEqual(Array.from(state._hydratedModelRecords.objects), ["latest-model/latest-mesh"]);
    assert.deepEqual(Array.from(state._modelSkins), []);
    assert.deepEqual(Array.from(state._modelAnimations), []);
    assert.equal(mount.getAttribute("data-gosx-scene3d-model-hydration-status"), "committed");
    assert.equal(mount.getAttribute("data-gosx-scene3d-model-hydration-generation"), "2");
    assert.equal(hydrationStatuses.filter((entry) => entry.status === "committed").length, 1);
    assert.equal(hydrationStatuses.some((entry) => entry.status === "stale"), false);
  } finally {
    animationAPI.createMixer = originalCreateMixer;
    animationAPI.computeJointMatrices = originalComputeJointMatrices;
  }
});

test("Scene3D multi-model hydration stages out-of-order loads and commits in declaration order", async () => {
  const routeFirst = createDeferredModelRoute();
  const routeSecond = createDeferredModelRoute();
  const env = createContext({
    fetchRoutes: {
      "/models/declaration-first.gosx3d.json": () => routeFirst.promise,
      "/models/declaration-second.gosx3d.json": () => routeSecond.promise,
    },
  });
  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();

  const api = env.context.__gosx_scene3d_api;
  const state = api.createSceneState({ scene: { objects: [] } });
  const hydration = api.applySceneCommands(state, [{
    kind: 10,
    data: {
      models: [
        { id: "declared-first", src: "/models/declaration-first.gosx3d.json" },
        { id: "declared-second", src: "/models/declaration-second.gosx3d.json" },
      ],
    },
  }]);

  routeSecond.resolve(modelAssetJSON("mesh"));
  await flushAsyncWork();
  assert.deepEqual(Array.from(state.objects.keys()), [],
    "a resolved model must remain staged until every declaration is ready");

  routeFirst.resolve(modelAssetJSON("mesh"));
  await hydration;
  assert.deepEqual(Array.from(state.objects.keys()), [
    "declared-first/mesh",
    "declared-second/mesh",
  ]);
  assert.deepEqual(Array.from(state._hydratedModelRecords.objects), [
    "declared-first/mesh",
    "declared-second/mesh",
  ]);
});

test("Scene3D throwing skin hydration rolls back staged records and mixers, then recovers", async () => {
  const routeRig = createDeferredModelRoute();
  const mount = new FakeElement("div", null);
  mount.id = "scene-model-transaction-root";
  const hydrationStatuses = [];
  mount.addEventListener("gosx:scene3d:model-hydration-status", (event) => hydrationStatuses.push(event.detail));
  const env = createContext({
    elements: [mount],
    fetchRoutes: {
      "/models/transaction-rig.glb": () => routeRig.promise,
      "/models/transaction-recovery.gosx3d.json": modelAssetJSON("recovered-mesh"),
    },
    manifest: {
      engines: [{
        id: "gosx-engine-model-transaction",
        component: "GoSXScene3D",
        kind: "surface",
        mountId: "scene-model-transaction-root",
        props: {
          width: 480,
          height: 300,
          scene: {
            objects: [{ id: "authored-baseline", kind: "box" }],
            models: [{ id: "broken-rig", src: "/models/transaction-rig.glb", animation: "bend" }],
          },
        },
      }],
    },
  });

  const unhandled = [];
  const onUnhandled = (error) => unhandled.push(error);
  process.on("unhandledRejection", onUnhandled);
  let originalComputeJointMatrices;
  let originalCreateMixer;
  let disposedMixers = 0;
  try {
    runScript(bootstrapSource, env.context, "bootstrap.js");
    await flushAsyncWork();
    assert.equal(env.fetchCalls.some((call) => call.url === "/models/transaction-rig.glb"), true);

    const animationAPI = env.context.__gosx_scene3d_animation_api;
    originalComputeJointMatrices = animationAPI.computeJointMatrices;
    originalCreateMixer = animationAPI.createMixer;
    animationAPI.computeJointMatrices = function throwingSkinStage() {
      throw new Error("transactional-skin-stage-boom");
    };
    animationAPI.createMixer = function trackedMixer() {
      return {
        addClip() {},
        play() {},
        stop() {},
        update() {},
        isPlaying() { return false; },
        dispose() { disposedMixers += 1; },
      };
    };

    routeRig.resolve({ bytes: buildSkinnedGLBBytes() });
    for (let attempt = 0; attempt < 8; attempt += 1) {
      await flushAsyncWork();
    }

    const mounted = env.context.__gosx.engines.get("gosx-engine-model-transaction");
    assert.ok(mounted, "the scene must finish mounting after a real skin-stage throw");
    const state = mount.__gosxScene3DState;
    assert.ok(state);
    assert.deepEqual(Array.from(state.objects.keys()), ["authored-baseline"]);
    assert.equal(state._hydratedModelRecords, undefined);
    assert.deepEqual(Array.from(state._modelSkins || []), []);
    assert.deepEqual(Array.from(state._modelAnimations || []), []);
    assert.equal(disposedMixers, 1, "the failed generation's staged mixer must be disposed");
    assert.equal(mount.getAttribute("data-gosx-scene3d-model-hydration-status"), "failed");
    assert.equal(mount.getAttribute("data-gosx-scene3d-model-hydration-failure-stage"), "skin");
    assert.equal(mount.getAttribute("data-gosx-scene3d-mounted"), "true");
    assert.deepEqual(unhandled, [], "hydration rejection handling must be attached before the mount awaits it");

    animationAPI.computeJointMatrices = originalComputeJointMatrices;
    animationAPI.createMixer = originalCreateMixer;
    await mounted.handle.applyCommands([{
      kind: 10,
      data: {
        models: [{
          id: "recovered-model",
          src: "/models/transaction-recovery.gosx3d.json",
        }],
      },
    }]);
    assert.deepEqual(Array.from(state.objects.keys()), [
      "authored-baseline",
      "recovered-model/recovered-mesh",
    ]);
    assert.equal(mount.getAttribute("data-gosx-scene3d-model-hydration-status"), "committed");
    assert.equal(hydrationStatuses.some((entry) => entry.status === "failed" && entry.stage === "skin"), true);
    assert.equal(hydrationStatuses.some((entry) => entry.status === "committed" && entry.generation === 2), true);
  } finally {
    process.removeListener("unhandledRejection", onUnhandled);
    if (env.context.__gosx_scene3d_animation_api) {
      if (originalComputeJointMatrices) {
        env.context.__gosx_scene3d_animation_api.computeJointMatrices = originalComputeJointMatrices;
      }
      if (originalCreateMixer) {
        env.context.__gosx_scene3d_animation_api.createMixer = originalCreateMixer;
      }
    }
  }
});

test("Scene3D model asset cache evicts transient failures so the same source can retry", async () => {
  let attempts = 0;
  const env = createContext({
    fetchRoutes: {
      "/models/transient-retry.gosx3d.json": () => {
        attempts += 1;
        return attempts === 1
          ? { ok: false, status: 503 }
          : modelAssetJSON("retry-success");
      },
    },
  });
  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();

  const first = await env.context.__gosx_scene3d_preload_model("/models/transient-retry.gosx3d.json");
  const second = await env.context.__gosx_scene3d_preload_model("/models/transient-retry.gosx3d.json");
  assert.equal(first.objects.length, 0);
  assert.equal(second.objects.length, 1);
  assert.equal(second.objects[0].id, "retry-success");
  assert.equal(attempts, 2);
});

test("Scene3D unsupported renderer fences initial model hydration before late assets resolve", async () => {
  const route = createDeferredModelRoute();
  const mount = new FakeElement("div", null);
  mount.id = "scene-model-unsupported-fence";
  let state = null;
  Object.defineProperty(mount, "__gosxScene3DState", {
    configurable: true,
    get() { return state; },
    set(value) { state = value; },
  });
  const assetStatuses = [];
  const hydrationStatuses = [];
  mount.addEventListener("gosx:scene3d:model-status", (event) => assetStatuses.push(event.detail));
  mount.addEventListener("gosx:scene3d:model-hydration-status", (event) => hydrationStatuses.push(event.detail));
  const env = createContext({
    elements: [mount],
    disableCanvas2D: true,
    fetchRoutes: {
      "/gosx/bootstrap-feature-engines.js": { text: bootstrapFeatureEnginesSource },
      "/models/unsupported-late.gosx3d.json": () => route.promise,
    },
    manifest: {
      engines: [{
        id: "gosx-engine-model-unsupported-fence",
        component: "GoSXScene3D",
        kind: "surface",
        mountId: mount.id,
        props: {
          width: 320,
          height: 180,
          models: [{ id: "late", src: "/models/unsupported-late.gosx3d.json" }],
        },
      }],
    },
  });

  runScript(bootstrapRuntimeSource, env.context, "bootstrap-runtime.js");
  runScript(freshFeatureBundleSource("scene3d"), env.context, "bootstrap-feature-scene3d.js");
  await flushAsyncWork();
  assert.equal(state, null, "unsupported private state must never be published");
  await flushAsyncWork();
  assert.equal(mount.getAttribute("data-gosx-scene3d-renderer"), "unsupported");
  assert.equal(mount.__gosxScene3DHandle, undefined);

  route.resolve(modelAssetJSON("must-not-commit"));
  for (let attempt = 0; attempt < 5; attempt += 1) await flushAsyncWork();
  assert.equal(state, null);
  assert.equal(assetStatuses.some((entry) => entry.status === "loaded"), false);
  assert.equal(hydrationStatuses.some((entry) => entry.status === "committed"), false);
});

test("Scene3D disposal fences a deferred SetModels hydration before late commit or status", async () => {
  const route = createDeferredModelRoute();
  const mount = new FakeElement("div", null);
  mount.id = "scene-model-dispose-fence";
  const assetStatuses = [];
  const hydrationStatuses = [];
  mount.addEventListener("gosx:scene3d:model-status", (event) => assetStatuses.push(event.detail));
  mount.addEventListener("gosx:scene3d:model-hydration-status", (event) => hydrationStatuses.push(event.detail));
  const env = createContext({
    elements: [mount],
    fetchRoutes: {
      "/gosx/bootstrap-feature-engines.js": { text: bootstrapFeatureEnginesSource },
      "/models/disposed-late.gosx3d.json": () => route.promise,
    },
    manifest: {
      engines: [{
        id: "gosx-engine-model-dispose-fence",
        component: "GoSXScene3D",
        kind: "surface",
        mountId: mount.id,
        props: {
          width: 320,
          height: 180,
          scene: { objects: [{ id: "authored-baseline", kind: "box" }] },
        },
      }],
    },
  });

  runScript(bootstrapRuntimeSource, env.context, "bootstrap-runtime.js");
  runScript(freshFeatureBundleSource("scene3d"), env.context, "bootstrap-feature-scene3d.js");
  await flushAsyncWork();
  const mounted = env.context.__gosx.engines.get("gosx-engine-model-dispose-fence");
  assert.ok(mounted);
  const state = mount.__gosxScene3DState;
  const hydration = mounted.handle.applyCommands([{
    kind: 10,
    data: { models: [{ id: "late", src: "/models/disposed-late.gosx3d.json" }] },
  }]);
  await flushAsyncWork();
  const generationBeforeDispose = state._modelHydrationGeneration;
  env.context.__gosx_dispose_engine("gosx-engine-model-dispose-fence");
  assert.ok(state._modelHydrationGeneration > generationBeforeDispose);

  route.resolve(modelAssetJSON("must-not-commit"));
  await hydration;
  for (let attempt = 0; attempt < 5; attempt += 1) await flushAsyncWork();
  assert.deepEqual(Array.from(state.objects.keys()), ["authored-baseline"]);
  assert.equal(assetStatuses.some((entry) => entry.status === "loaded"), false);
  assert.equal(hydrationStatuses.some((entry) => entry.status === "committed" && entry.generation > 1), false);
});

test("bootstrap prepares Scene3D pass plans and cached buffers through shared planner", async () => {
  const env = createContext({});
  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();

  const api = env.context.__gosx_scene3d_api;
  assert.equal(typeof api.prepareScene, "function");
  assert.equal(typeof api.sceneCachedBuffer, "function");

  const bundle = {
    bundleVersion: api.SCENE_RENDER_BUNDLE_VERSION,
    camera: { x: 0, y: 0, z: 6, fov: 72, near: 0.05, far: 128 },
    environment: {},
    materials: [
      { kind: "flat", opacity: 1, renderPass: "opaque" },
      { kind: "glass", opacity: 0.5, renderPass: "alpha" },
    ],
    meshObjects: [
      { id: "near", kind: "box", materialIndex: 1, vertexOffset: 0, vertexCount: 3, depthCenter: 4 },
      { id: "far", kind: "box", materialIndex: 1, vertexOffset: 3, vertexCount: 3, depthCenter: 8 },
      { id: "solid", kind: "box", materialIndex: 0, vertexOffset: 6, vertexCount: 3, depthCenter: 6 },
    ],
    objects: [],
    worldPositions: new Float32Array(0),
    worldColors: new Float32Array(0),
    worldMeshPositions: new Float32Array(27),
    worldMeshNormals: new Float32Array(27),
  };
  const viewport = { cssWidth: 320, cssHeight: 180, pixelWidth: 320, pixelHeight: 180, pixelRatio: 1 };
  const prepared = api.prepareScene(bundle, bundle.camera, viewport, null);
  assert.equal(JSON.stringify(prepared.pbrPasses.opaque.map((entry) => entry.id)), JSON.stringify(["solid"]));
  assert.equal(JSON.stringify(prepared.pbrPasses.alpha.map((entry) => entry.id)), JSON.stringify(["far", "near"]));
  assert.equal(JSON.stringify(api.scenePreparedCommandSequence(prepared)), JSON.stringify([
    { op: "drawMesh", pass: "opaque", id: "solid", kind: "box", vertexOffset: 6, vertexCount: 3 },
    { op: "drawMesh", pass: "alpha", id: "far", kind: "box", vertexOffset: 3, vertexCount: 3 },
    { op: "drawMesh", pass: "alpha", id: "near", kind: "box", vertexOffset: 0, vertexCount: 3 },
  ]));
  assert.equal(prepared.shadowPassHash, api.prepareScene(bundle, bundle.camera, viewport, prepared).shadowPassHash);
  assert.equal(api.prepareScene(bundle, bundle.camera, viewport, prepared), prepared);

  bundle.points = [{ id: "stars", count: 5, color: "#ffffff", size: 1 }];
  const withPoints = api.prepareScene(bundle, bundle.camera, viewport, prepared);
  assert.notEqual(withPoints, prepared);
  assert.equal(api.scenePreparedCommandSequence(withPoints).at(-1).count, 5);
  bundle.points = [{ id: "stars", count: 8, color: "#5eead4", size: 1.5 }];
  const updatedPoints = api.prepareScene(bundle, bundle.camera, viewport, withPoints);
  assert.notEqual(updatedPoints, withPoints);
  assert.equal(api.scenePreparedCommandSequence(updatedPoints).at(-1).count, 8);

  bundle.environment = { fogColor: "#0b142a", fogDensity: 0.0003 };
  const withFog = api.prepareScene(bundle, bundle.camera, viewport, updatedPoints);
  assert.equal(withFog, updatedPoints);
  assert.equal(withFog.ir.environment.fogDensity, 0.0003);

  bundle.points = [{ id: "stars", count: 8, color: "#5eead4", size: 1.5, positions: [0, 0, 0, 1, 1, 1] }];
  const withPointGeometry = api.prepareScene(bundle, bundle.camera, viewport, withFog);
  assert.equal(withPointGeometry, withFog);
  assert.equal(withPointGeometry.ir.points[0].positions.length, 6);

  const cache = new WeakMap();
  const typed = new Float32Array([1, 2, 3]);
  let uploads = 0;
  const first = api.sceneCachedBuffer(cache, typed, () => ({ id: "buffer" }), () => { uploads += 1; });
  const second = api.sceneCachedBuffer(cache, typed, () => ({ id: "next" }), () => { uploads += 1; });
  assert.equal(first, second);
  assert.equal(uploads, 1);

  const owner = {};
  const small = new Float32Array([1]);
  const large = new Float32Array([1, 2, 3, 4]);
  const smallHandle = api.sceneCachedBuffer(
    owner,
    small,
    () => ({ id: "small", size: small.byteLength }),
    () => {},
    { slot: "gpuBuffer" }
  );
  const largeHandle = api.sceneCachedBuffer(
    owner,
    large,
    () => ({ id: "unused" }),
    (handle, data, state) => state.bytesChanged && data.byteLength > handle.size
      ? { id: "large", size: data.byteLength }
      : handle,
    { slot: "gpuBuffer" }
  );
  assert.equal(smallHandle.id, "small");
  assert.equal(largeHandle.id, "large");
  assert.equal(owner.gpuBuffer, largeHandle);
});
