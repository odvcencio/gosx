"use strict";
// Scene3D scene normalization: named materials, material profiles, CPU compute
// particles, live point buffers, update transitions and mesh raycasting.
//
// Split out of client/js/runtime.test.js. Every shared fake, sandbox builder
// and fixture factory lives in ./runtime-test-harness.js.

const test = require("node:test");
const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");

const {
  bootstrapSource,
  bootstrapRuntimeSource,
  bootstrapFeatureScene3DSource,
  bootstrapFeatureScene3DComputeSource,
  bootstrapSourceMapSource,
  createContext,
  runScript,
  flushAsyncWork,
  bootstrapChunkSources,
  readSceneMountSrc,
} = require("./runtime-test-harness.js");

test("bootstrap normalizes orthographic Scene3D cameras, LOD, lights, and custom line materials", async () => {
  const env = createContext({});
  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();

  const api = env.context.__gosx_scene3d_api;
  const camera = api.sceneRenderCamera({
    kind: "orthographic",
    x: 1,
    y: 2,
    z: 8,
    left: -4,
    right: 4,
    top: 3,
    bottom: -3,
    zoom: 2,
    near: 0.1,
    far: 90,
  });

  assert.equal(camera.kind, "orthographic");
  assert.equal(camera.left, -4);
  assert.equal(camera.zoom, 2);

  const object = api.normalizeSceneObject({
    id: "path",
    kind: "lines",
    materialKind: "line-dashed",
    color: "#ffffff",
    lineDash: true,
    dashSize: 6,
    gapSize: 2,
    lineWidth: 3,
    points: [{ x: 0, y: 0, z: 0 }, { x: 1, y: 0, z: 0 }],
    lineSegments: [[0, 1]],
  }, 0, null);
  const nearLOD = api.normalizeSceneObject({
    id: "near-lod",
    kind: "box",
    lodGroup: "ship-lod",
    lodLevel: 0,
    lodMinDistance: 0,
    lodMaxDistance: 4,
    clearcoat: 0.6,
    sheen: 0.3,
  }, 1, null);
  const farLOD = api.normalizeSceneObject({
    id: "far-lod",
    kind: "box",
    lodGroup: "ship-lod",
    lodLevel: 1,
    lodMinDistance: 4,
    lodMaxDistance: 0,
    transmission: 0.25,
    iridescence: 0.5,
    anisotropy: -0.2,
  }, 2, null);
  const rectLight = api.normalizeSceneLight({ kind: "rect-area", width: 3, height: 2, intensity: 1.2 }, 0, null);
  const probe = api.normalizeSceneLight({ kind: "light-probe", color: "#ddeeff", intensity: 0.4 }, 1, null);
  const bundle = api.createSceneRenderBundle(
    320,
    180,
    "#08151f",
    camera,
    [object, nearLOD, farLOD],
    [],
    [],
    [],
    [rectLight, probe],
    {},
    0,
    [],
    [],
    [],
    [],
    [{ kind: "dof", focusDistance: 8, aperture: 0.05, maxBlur: 7 }],
    0,
  );

  assert.equal(bundle.camera.kind, "orthographic");
  assert.equal(bundle.materials[0].kind, "line-dashed");
  assert.equal(bundle.materials[0].lineDash, true);
  assert.equal(bundle.materials.some((material) => material.clearcoat === 0.6), false);
  assert.equal(bundle.materials.some((material) => material.transmission === 0.25), true);
  assert.equal(bundle.worldLineWidths[0], 3);
  assert.equal(bundle.worldLineDashes[0], true);
  assert.equal(rectLight.kind, "rect-area");
  assert.equal(rectLight.width, 3);
  assert.equal(probe.kind, "light-probe");
  assert.equal(bundle.objects.some((entry) => entry.id === "near-lod"), false);
  assert.equal(bundle.objects.some((entry) => entry.id === "far-lod"), true);
  assert.deepEqual(bundle.postEffects, [{ kind: "dof", focusDistance: 8, aperture: 0.05, maxBlur: 7 }]);
});

test("bootstrap applies named Scene3D materials to point layers", async () => {
  const env = createContext({});
  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();

  const api = env.context.__gosx_scene3d_api;
  assert.equal(typeof api.sceneStatePointsWithMaterials, "function");

  const state = api.createSceneState({
    scene: {
      materials: [
        { name: "core", color: "var(--galaxy-core-inner)", opacity: "var(--galaxy-core-opacity)" },
      ],
      points: [
        { id: "galaxy", count: 1, material: "core", color: "#ffffff", opacity: 0.1, blendMode: "additive" },
      ],
    },
  });
  const points = api.sceneStatePointsWithMaterials(state);
  const again = api.sceneStatePointsWithMaterials(state);

  assert.equal(points[0].color, "var(--galaxy-core-inner)");
  assert.equal(points[0].opacity, "var(--galaxy-core-opacity)");
  assert.equal(points[0].blendMode, "additive");
  assert.equal(again[0], points[0]);
});

test("bootstrap selects Scene3D material variants from capability tier", async () => {
  const env = createContext({});
  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();

  const api = env.context.__gosx_scene3d_api;
  const props = {
    scene: {
      materials: [
        {
          name: "core",
          color: "#77c6ff",
          opacity: 0.9,
          variants: {
            constrained: {
              color: "#cbd5e1",
              opacity: 0.35,
            },
          },
        },
      ],
      points: [
        { id: "galaxy", count: 1, material: "core", color: "#ffffff", opacity: 0.1 },
      ],
    },
  };

  const constrained = api.createSceneState(props, { tier: "constrained" });
  const constrainedPoints = api.sceneStatePointsWithMaterials(constrained);
  assert.equal(constrained.materials[0].variantKey, "constrained");
  assert.equal(constrainedPoints[0].color, "#cbd5e1");
  assert.equal(constrainedPoints[0].opacity, 0.35);

  const full = api.createSceneState(props, { tier: "full" });
  const fullPoints = api.sceneStatePointsWithMaterials(full);
  assert.equal(full.materials[0].variantKey, "");
  assert.equal(fullPoints[0].color, "#77c6ff");
  assert.equal(fullPoints[0].opacity, 0.9);
});

test("bootstrap preserves named Scene3D custom WGSL materials through object bundles", async () => {
  const env = createContext({});
  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();

  const api = env.context.__gosx_scene3d_api;
  assert.equal(typeof api.sceneStateObjectsWithMaterials, "function");

  const state = api.createSceneState({
    scene: {
      materials: [
        {
          name: "shader-card",
          kind: "custom",
          color: "#f5c76b",
          customFragmentWGSL: "fn gosx_fragment() -> vec4f { return vec4f(1.0); }",
          customUniforms: { pulse: 0.75 },
          shaderBackend: "selena",
          shaderLayout: { schemaVersion: "selena.descriptor.v1", material: "ShaderCard" },
          variants: {
            constrained: {
              color: "#94a3b8",
            },
          },
        },
      ],
      objects: [
        { id: "hero", kind: "box", material: "shader-card", size: 1 },
      ],
    },
  }, { tier: "constrained" });

  const objects = api.sceneStateObjectsWithMaterials(state);
  assert.equal(objects[0].materialKind, "custom");
  assert.equal(objects[0].color, "#94a3b8");
  assert.equal(objects[0].customFragmentWGSL, "fn gosx_fragment() -> vec4f { return vec4f(1.0); }");
  assert.equal(objects[0].customUniforms.pulse, 0.75);
  assert.equal(objects[0].shaderBackend, "selena");
  assert.equal(objects[0].shaderLayout.material, "ShaderCard");

  const bundle = api.createSceneRenderBundle(
    320,
    180,
    "#08151f",
    { x: 0, y: 0, z: 6, fov: 72, near: 0.05, far: 128 },
    objects,
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

  assert.equal(bundle.materials[0].kind, "custom");
  assert.equal(bundle.materials[0].color, "#94a3b8");
  assert.equal(bundle.materials[0].customFragmentWGSL, "fn gosx_fragment() -> vec4f { return vec4f(1.0); }");
  assert.equal(bundle.materials[0].customUniforms.pulse, 0.75);
  assert.equal(bundle.materials[0].shaderBackend, "selena");
  assert.equal(bundle.materials[0].shaderLayout.schemaVersion, "selena.descriptor.v1");
});

test("bootstrap applies named Scene3D materials to instanced mesh bundles", async () => {
  const env = createContext({});
  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();

  const api = env.context.__gosx_scene3d_api;
  assert.equal(typeof api.sceneStateInstancedMeshesWithMaterials, "function");

  const state = api.createSceneState({
    scene: {
      materials: [
        {
          name: "batch-shader",
          kind: "custom",
          color: "#f5c76b",
          opacity: 0.7,
          customFragmentWGSL: "fn gosx_fragment() -> vec4f { return vec4f(0.4); }",
          customUniforms: { heat: 0.42 },
          variants: {
            constrained: {
              color: "#94a3b8",
              opacity: 0.35,
            },
          },
        },
      ],
      instancedMeshes: [
        {
          id: "debris",
          kind: "box",
          count: 2,
          material: "batch-shader",
          transforms: new Array(32).fill(0),
          colors: ["#ffcc66", "#66ccff"],
        },
      ],
    },
  }, { tier: "constrained" });

  const meshes = api.sceneStateInstancedMeshesWithMaterials(state);
  assert.equal(meshes[0].materialKind, "custom");
  assert.equal(meshes[0].color, "#94a3b8");
  assert.equal(meshes[0].opacity, 0.35);
  assert.equal(meshes[0].customFragmentWGSL, "fn gosx_fragment() -> vec4f { return vec4f(0.4); }");
  assert.equal(meshes[0].customUniforms.heat, 0.42);
  assert.deepEqual(meshes[0].colors, ["#ffcc66", "#66ccff"]);

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
    {},
    0,
    [],
    meshes,
    [],
    [],
    [],
    0,
  );

  assert.equal(bundle.instancedMeshes[0].materialIndex, 0);
  assert.equal(bundle.materials[0].kind, "custom");
  assert.equal(bundle.materials[0].color, "#94a3b8");
  assert.equal(bundle.materials[0].opacity, 0.35);
  assert.equal(bundle.materials[0].customFragmentWGSL, "fn gosx_fragment() -> vec4f { return vec4f(0.4); }");
  assert.equal(bundle.materials[0].customUniforms.heat, 0.42);
});

test("bootstrap registers Scene3D material profiles through the shared API", async () => {
  const env = createContext({});
  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();

  const api = env.context.__gosx_scene3d_api;
  assert.equal(typeof api.registerSceneMaterialProfile, "function");
  assert.equal(typeof api.unregisterSceneMaterialProfile, "function");

  const registered = api.registerSceneMaterialProfile("cloth", {
    opacity: 0.64,
    emissive: 0.19,
    blendMode: "alpha",
    shaderData: [7, 0.19, 0.44],
    key: "cloth-v1",
  });
  assert.equal(registered.kind, "cloth");
  assert.equal(api.listSceneMaterialProfiles().some((profile) => profile.kind === "cloth"), true);

  const object = api.normalizeSceneObject({
    id: "panel",
    kind: "box",
    materialKind: "cloth",
    color: "#d8b4fe",
    size: 1,
  }, 0, null);
  const bundle = api.createSceneRenderBundle(
    320,
    180,
    "#08151f",
    { x: 0, y: 0, z: 6, fov: 72, near: 0.05, far: 128 },
    [object],
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

  const material = bundle.materials.find((entry) => entry.kind === "cloth");
  assert.ok(material, JSON.stringify(bundle.materials));
  assert.equal(material.opacity, 0.64);
  assert.equal(material.blendMode, "alpha");
  assert.equal(material.renderPass, "alpha");
  assert.equal(material.emissive, 0.19);
  assert.deepEqual(Array.from(material.shaderData), [7, 0.19, 0.44]);

  assert.equal(api.unregisterSceneMaterialProfile("cloth"), true);
});

test("bootstrap registers Scene3D CPU particle force handlers through the shared API", async () => {
  const env = createContext({});
  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();

  const api = env.context.__gosx_scene3d_api;
  assert.equal(typeof api.registerSceneParticleForce, "function");
  assert.equal(typeof api.createSceneParticleSystem, "function");

  assert.equal(api.registerSceneParticleForce("lift", (ctx) => ({ y: ctx.force.strength * ctx.deltaTime })), true);
  const system = api.createSceneParticleSystem(null, {
    id: "spark",
    count: 1,
    emitter: { kind: "point", lifetime: 10 },
    forces: [{ kind: "lift", strength: 2 }],
    material: {},
  });
  system.update(1, 1);

  assert.ok(system.positions[1] > 1.9, Array.from(system.positions).join(","));
  assert.equal(api.listSceneParticleForces().some((force) => force.kind === "lift" && force.handler), true);
  assert.equal(api.unregisterSceneParticleForce("lift"), true);
});

test("selective Scene3D bootstrap exposes CPU compute particles for WebGL", async () => {
  const env = createContext({});
  runScript(bootstrapRuntimeSource, env.context, "bootstrap-runtime.js");
  runScript(bootstrapFeatureScene3DSource, env.context, "bootstrap-feature-scene3d.js");
  await flushAsyncWork();

  // The base chunk carries no particle code any more. Prove that first, then
  // load the compute chunk and prove it fills the same API slots. A scene with
  // no particle system and no instanced mesh never reaches this fetch.
  assert.equal(typeof env.context.__gosx_scene3d_api.createSceneParticleSystem, "undefined",
    "the base scene3d chunk must not carry the particle systems");
  runScript(bootstrapFeatureScene3DComputeSource, env.context, "bootstrap-feature-scene3d-compute.js");
  await flushAsyncWork();

  const api = env.context.__gosx_scene3d_api;
  assert.equal(typeof api.sceneComputeSystemSignature, "function");
  assert.equal(typeof api.createSceneParticleSystem, "function");

  const system = api.createSceneParticleSystem(null, {
    id: "selective-sparks",
    count: 1,
    emitter: { kind: "point", lifetime: 1 },
    forces: [{ kind: "wind", x: 1, strength: 1 }],
    material: {},
  });
  system.update(0.016, 0.016);

  assert.equal(system.positions.length, 3);
});

test("Scene3D CPU compute particles use a soft lifetime opacity envelope", async () => {
  const env = createContext({});
  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();

  const api = env.context.__gosx_scene3d_api;
  const system = api.createSceneParticleSystem(null, {
    id: "soft-birth",
    count: 1,
    emitter: { kind: "point", lifetime: 10 },
    forces: [],
    material: { opacity: 1, opacityEnd: 0 },
  });

  system.update(0.01, 0.01);
  assert.ok(system.opacities[0] < 0.04, String(system.opacities[0]));

  system.update(1.2, 1.21);
  assert.ok(system.opacities[0] > 0.75, String(system.opacities[0]));
});

test("Scene3D CPU compute particles can emit a one-shot burst", async () => {
  const env = createContext({});
  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();

  const api = env.context.__gosx_scene3d_api;
  const system = api.createSceneParticleSystem(null, {
    id: "one-shot",
    count: 1,
    emitter: { kind: "point", lifetime: 0.4, once: true },
    forces: [{ kind: "wind", x: 1, strength: 1 }],
    material: { opacity: 1, opacityEnd: 0 },
  });

  system.update(0.02, 0.02);
  assert.ok(system.opacities[0] > 0, String(system.opacities[0]));

  system.update(1.0, 1.02);
  assert.equal(system.opacities[0], 0);

  system.update(1.0, 2.02);
  assert.equal(system.opacities[0], 0);
  assert.equal(system.positions[0], 0);
});

test("Scene3D CPU one-shot particles retire when leaving bounds", async () => {
  const env = createContext({});
  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();

  const api = env.context.__gosx_scene3d_api;
  const system = api.createSceneParticleSystem(null, {
    id: "one-shot-bounds",
    count: 1,
    bounds: 0.02,
    emitter: { kind: "point", lifetime: 10, once: true },
    forces: [{ kind: "wind", x: 1, strength: 3 }],
    material: { opacity: 1, opacityEnd: 0 },
  });

  system.update(0.02, 0.02);
  assert.ok(system.opacities[0] > 0, String(system.opacities[0]));

  system.update(0.1, 0.12);
  assert.equal(system.opacities[0], 0);
  assert.equal(system.positions[0], 0);

  system.update(1.0, 1.12);
  assert.equal(system.opacities[0], 0);
});

test("bootstrap applies Scene3D live point buffers outside update tweens", async () => {
  const env = createContext({});
  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();

  const api = env.context.__gosx_scene3d_api;
  const state = api.createSceneState({
    scene: {
      points: [
        {
          id: "galaxy",
          count: 2,
          positions: [0, 0, 0, 1, 0, 0],
          sizes: [1, 1],
          colors: ["#000000", "#111111"],
          opacity: 0.5,
          live: ["galaxy:node:galaxy"],
          transition: { update: { duration: 1200, easing: "linear" } },
        },
      ],
    },
  });
  const entry = state.points[0];
  entry._cachedColors = new Float32Array([0, 0, 0, 1, 0.1, 0.1, 0.1, 1]);

  const changed = api.sceneApplyLiveEvent(state, "galaxy:node:galaxy", {
    colors: ["#ff0000", "#00ff00"],
  }, false, 10);

  assert.equal(changed, true);
  assert.deepEqual(entry.colors, ["#ff0000", "#00ff00"]);
  assert.equal(entry._cachedColors, null);
  assert.equal(state._transitions.length, 0);
});

test("bootstrap keeps Scene3D live point buffers out of scalar update transitions", async () => {
  const env = createContext({});
  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();

  const api = env.context.__gosx_scene3d_api;
  const state = api.createSceneState({
    scene: {
      points: [
        {
          id: "galaxy",
          count: 2,
          positions: [0, 0, 0, 1, 0, 0],
          sizes: [1, 1],
          colors: ["#000000", "#111111"],
          opacity: 0.5,
          live: ["galaxy:node:galaxy"],
          transition: { update: { duration: 1200, easing: "linear" } },
        },
      ],
    },
  });
  const entry = state.points[0];
  entry._cachedColors = new Float32Array([0, 0, 0, 1, 0.1, 0.1, 0.1, 1]);
  const payload = {
    colors: ["#ff0000", "#00ff00"],
    opacity: 0.9,
  };

  const changed = api.sceneApplyLiveEvent(state, "galaxy:node:galaxy", payload, false, 10);

  assert.equal(changed, true);
  assert.deepEqual(entry.colors, ["#ff0000", "#00ff00"]);
  assert.equal(entry._cachedColors, null);
  assert.equal(entry.opacity, 0.5);
  assert.equal(Object.prototype.hasOwnProperty.call(payload, "__eventName"), false);
  assert.equal(state._transitions.length, 1);
  const transition = state._transitions[0];
  assert.equal(Object.prototype.hasOwnProperty.call(transition.target, "colors"), false);
  assert.equal(Object.prototype.hasOwnProperty.call(transition.target, "positions"), false);
  assert.equal(Object.prototype.hasOwnProperty.call(transition.target, "sizes"), false);
  assert.equal(Object.prototype.hasOwnProperty.call(transition.target, "_cachedColors"), false);
  assert.equal(Object.prototype.hasOwnProperty.call(transition.delta, "colors"), false);
  assert.equal(Object.prototype.hasOwnProperty.call(transition.delta, "positions"), false);
  assert.equal(Object.prototype.hasOwnProperty.call(transition.delta, "sizes"), false);
  assert.equal(transition.delta.opacity.__from, 0.5);
  assert.equal(transition.delta.opacity.__to, 0.9);
  assert.equal(transition.delta.opacity.__key, "opacity");

  api.sceneAdvanceTransitions(state, 1210);
  assert.equal(entry.opacity, 0.9);
  assert.deepEqual(entry.colors, ["#ff0000", "#00ff00"]);
});

test("bootstrap treats explicit zero Scene3D update timing as instant", async () => {
  const env = createContext({});
  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();

  const api = env.context.__gosx_scene3d_api;
  const state = api.createSceneState({
    scene: {
      points: [
        {
          id: "galaxy",
          count: 1,
          positions: [0, 0, 0],
          colors: ["#ffffff"],
          opacity: 0.2,
          live: ["galaxy:node:galaxy"],
          transition: {
            in: { duration: 3200, easing: "ease-in-out" },
            update: { easing: "ease-in-out" },
          },
        },
      ],
    },
  });

  const changed = api.sceneApplyLiveEvent(state, "galaxy:node:galaxy", {
    opacity: 0.8,
  }, false, 10);

  assert.equal(changed, true);
  assert.equal(state._transitions.length, 0);
  assert.equal(state.points[0].opacity, 0.8);
});

test("bootstrap keeps Scene3D initial point buffers out of entry transitions", async () => {
  const env = createContext({});
  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();

  const api = env.context.__gosx_scene3d_api;
  const state = api.createSceneState({
    scene: {
      points: [
        {
          id: "galaxy",
          count: 2,
          positions: [0, 0, 0, 1, 0, 0],
          sizes: [1, 2],
          colors: ["#000000", "#111111"],
          opacity: 0.5,
          transition: { in: { duration: 1200, easing: "linear" } },
          inState: { opacity: 0 },
        },
      ],
    },
  });

  const entry = state.points[0];
  api.scenePrimeInitialTransitions(state, false, 0);

  assert.equal(state._transitions.length, 1);
  assert.deepEqual(entry.positions, [0, 0, 0, 1, 0, 0]);
  assert.deepEqual(entry.sizes, [1, 2]);
  assert.deepEqual(entry.colors, ["#000000", "#111111"]);
  const transition = state._transitions[0];
  assert.equal(Object.prototype.hasOwnProperty.call(transition.target, "positions"), false);
  assert.equal(Object.prototype.hasOwnProperty.call(transition.target, "sizes"), false);
  assert.equal(Object.prototype.hasOwnProperty.call(transition.target, "colors"), false);
  assert.equal(Object.prototype.hasOwnProperty.call(transition.delta, "positions"), false);
  assert.equal(Object.prototype.hasOwnProperty.call(transition.delta, "sizes"), false);
  assert.equal(Object.prototype.hasOwnProperty.call(transition.delta, "colors"), false);
  assert.equal(transition.delta.opacity.__from, 0);
  assert.equal(transition.delta.opacity.__to, 0.5);
});

test("bootstrap keeps Scene3D CSS transition diagnostics opt-in", () => {
  const source = fs.readFileSync(path.join(__dirname, "bootstrap-src", "15b-scene-planner.js"), "utf8");

  assert.match(source, /function sceneCSSDebugLog\(\)/);
  assert.match(source, /__gosx_scene3d_css_debug/);
  assert.doesNotMatch(source, /console\.log\("\[gosx:css-transition\]/);
});

test("bootstrap observes inherited root CSS var mutations for Scene3D", () => {
  const source = readSceneMountSrc();

  assert.match(source, /observer\.observe\(document\.documentElement,\s*\{/);
  assert.match(source, /attributeOldValue:\s*true/);
  assert.match(source, /sceneCSSMutationShouldInvalidate\(records\)/);
  assert.match(source, /name\.indexOf\("--gosx-"\)\s*===\s*0/);
  assert.match(source, /sceneCSSTransitionWindowMillis\(document && document\.documentElement\)/);
});

test("bootstrap gates Scene3D viewport refreshes to viewport-shaped environment changes", () => {
  const source = readSceneMountSrc();

  assert.match(source, /function sceneViewportEnvironmentSignature\(environment\)/);
  assert.match(source, /sceneNumber\(environment\.devicePixelRatio,\s*1\)/);
  assert.match(source, /Math\.round\(sceneNumber\(environment\.visualViewportHeight,\s*0\)\)/);
  assert.doesNotMatch(source, /environment\.visualViewportOffsetTop/);
  assert.match(source, /if \(environmentSignature === nextSignature\) \{\s*return;\s*\}/);
});

test("bootstrap skips redundant runtime style and attribute writes", () => {
  const source = fs.readFileSync(path.join(__dirname, "bootstrap-src", "00-textlayout.js"), "utf8");

  assert.match(source, /style\.getPropertyValue\(name\) === next/);
  assert.match(source, /style\.setProperty\(name,\s*next\)/);
  assert.match(source, /element\.getAttribute\(name\) === next/);
  assert.match(source, /element\.setAttribute\(name,\s*next\)/);
});

test("bootstrap derives selective runtime utilities from the Scene3D core source", () => {
  const builder = fs.readFileSync(path.join(__dirname, "..", "..", "cmd", "buildbootstrap", "main.go"), "utf8");
  const core = fs.readFileSync(path.join(__dirname, "bootstrap-src", "10-runtime-scene-core.js"), "utf8");
  const utils = fs.readFileSync(path.join(__dirname, "bootstrap-src", "10-runtime-scene-utils.js"), "utf8");
  const primitives = fs.readFileSync(path.join(__dirname, "bootstrap-src", "10-runtime-primitives.js"), "utf8");

  // The selective runtime bundle carries the runtime-utils head as a real
  // file. The build used to cut it out of the scene core with two literal
  // source markers, so a rename or a re-indent changed what shipped.
  assert.deepEqual(
    bootstrapChunkSources("bootstrap-runtime.js").filter((s) => s.includes("10-runtime-scene")),
    ["bootstrap-src/10-runtime-scene-utils.js"],
  );
  // The scene3d chunk carries no copy of the utils file. It bridges the ten
  // names it reads from window.__gosx_runtime_api instead, so the Chromium
  // Scene3D route downloads those helpers once, not twice.
  assert.deepEqual(
    bootstrapChunkSources("bootstrap-feature-scene3d.js").filter((s) => s.includes("10-runtime-scene")),
    ["bootstrap-src/10-runtime-scene-core.js"],
  );
  const scene3dPrefix = fs.readFileSync(
    path.join(__dirname, "bootstrap-src", "26d-feature-scene3d-prefix.js"), "utf8",
  );
  for (const name of [
    "browserCapabilitySupported", "cancelEngineFrame", "engineCapabilityStatus", "engineFrame",
    "gosxApplyCurrentScriptNonce", "loadManifest", "publishPointerSignals", "queueInputSignal",
    "runtimeCapabilityStatus", "sceneNumber",
  ]) {
    assert.match(scene3dPrefix, new RegExp(`var ${name} = runtimeApi\\.${name}`), `${name} must be bridged`);
    assert.match(utils, new RegExp(`__gosx_runtime_api\\.${name} = ${name};`), `${name} must be published`);
  }
  assert.doesNotMatch(core, /function sceneBool\(/);
  assert.doesNotMatch(core, /function clearChildren\(/);
  assert.match(primitives, /function sceneBool\(/);
  assert.match(primitives, /function clearChildren\(/);
  assert.equal(
    (bootstrapSourceMapSource("bootstrap.js.map", "bootstrap-src/12-scene-geometry.js").match(/function sceneSegmentResolution\(/g) || []).length,
    1,
  );
  assert.equal(
    (bootstrapSourceMapSource("bootstrap-feature-scene3d.js.map", "bootstrap-src/12-scene-geometry.js").match(/function sceneSegmentResolution\(/g) || []).length,
    1,
  );
});

test("bootstrap keeps WebGL and WebGPU Scene3D command logs in parity", async () => {
  const env = createContext({});
  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();

  const api = env.context.__gosx_scene3d_api;
  assert.equal(typeof api.sceneWebGLCommandSequence, "function");
  assert.equal(typeof api.sceneWebGPUCommandSequence, "function");
  assert.equal(typeof api.sceneRaycastPick, "function");
  assert.equal(env.context.__gosx_scene3d_raycast, api.sceneRaycastPick);

  const bundle = {
    bundleVersion: api.SCENE_RENDER_BUNDLE_VERSION,
    camera: { x: 0, y: 0, z: 6, fov: 72, near: 0.05, far: 128 },
    environment: {},
    materials: [
      { kind: "flat", opacity: 1, renderPass: "opaque" },
      { kind: "glass", opacity: 0.5, renderPass: "alpha" },
      { kind: "glow", opacity: 0.7, renderPass: "additive" },
    ],
    meshObjects: [
      { id: "near", kind: "box", materialIndex: 1, vertexOffset: 0, vertexCount: 3, depthCenter: 4 },
      { id: "far", kind: "box", materialIndex: 1, vertexOffset: 3, vertexCount: 3, depthCenter: 8 },
      { id: "solid", kind: "sphere", materialIndex: 0, vertexOffset: 6, vertexCount: 3, depthCenter: 6 },
      { id: "spark", kind: "plane", materialIndex: 2, vertexOffset: 9, vertexCount: 3, depthCenter: 7 },
    ],
    objects: [],
    worldPositions: new Float32Array(0),
    worldColors: new Float32Array(0),
    worldMeshPositions: new Float32Array(36),
    worldMeshNormals: new Float32Array(36),
    points: [
      { id: "stars", count: 5 },
    ],
    instancedMeshes: [
      { id: "debris", kind: "box", count: 3 },
    ],
  };
  const viewport = { cssWidth: 320, cssHeight: 180, pixelWidth: 320, pixelHeight: 180, pixelRatio: 1 };
  const expected = api.scenePreparedCommandSequence(api.prepareScene(bundle, bundle.camera, viewport, null));

  assert.deepEqual(api.sceneWebGLCommandSequence(bundle, viewport), expected);
  assert.deepEqual(api.sceneWebGPUCommandSequence(bundle, viewport), expected);
  assert.deepEqual(api.sceneWebGPUCommandSequence(bundle, viewport), api.sceneWebGLCommandSequence(bundle, viewport));
});

test("bootstrap raycasts Scene3D mesh triangles and returns the nearest hit", async () => {
  const env = createContext({});
  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();

  const api = env.context.__gosx_scene3d_api;
  const positions = new Float32Array([
    -1, -1, 0,
    1, -1, 0,
    0, 1, 0,
    -1, -1, 2,
    1, -1, 2,
    0, 1, 2,
  ]);
  const uvs = new Float32Array([
    0, 0,
    1, 0,
    0.5, 1,
    0, 0,
    1, 0,
    0.5, 1,
  ]);
  const bundle = {
    camera: { x: 0, y: 0, z: 6, fov: 90, near: 0.1, far: 100 },
    meshObjects: [
      {
        id: "far-triangle",
        kind: "mesh",
        pickable: true,
        vertexOffset: 3,
        vertexCount: 3,
        bounds: { minX: -1, minY: -1, minZ: 1.9, maxX: 1, maxY: 1, maxZ: 2.1 },
      },
      {
        id: "near-triangle",
        kind: "mesh",
        pickable: true,
        vertexOffset: 0,
        vertexCount: 3,
        bounds: { minX: -1, minY: -1, minZ: -0.1, maxX: 1, maxY: 1, maxZ: 0.1 },
      },
    ],
    worldMeshPositions: positions,
    worldMeshUVs: uvs,
    objects: [],
    worldPositions: new Float32Array(0),
  };

  const hit = api.sceneRaycastPick(100, 100, 200, 200, bundle.camera, bundle);
  assert.ok(hit, "expected raycast hit");
  assert.equal(hit.object.id, "far-triangle");
  assert.equal(hit.index, 0);
  assert.ok(Math.abs(hit.distance - 4) < 1e-6, "expected far triangle distance, got " + hit.distance);
  assert.equal(hit.point.x, 0);
  assert.equal(hit.point.y, 0);
  assert.equal(hit.point.z, 2);
  assert.equal(hit.worldPosition.x, 0);
  assert.equal(hit.localPosition.z, 2);
  assert.equal(hit.triangleIndex, 0);
  assert.equal(hit.primitiveIndex, 0);
  assert.equal(hit.instanceIndex, -1);
  assert.ok(Math.abs(hit.uv.x - 0.5) < 1e-6, "expected interpolated hit uv.x, got " + hit.uv.x);
  assert.ok(Math.abs(hit.uv.y - 0.5) < 1e-6, "expected interpolated hit uv.y, got " + hit.uv.y);
});

test("bootstrap projects Scene3D pointer rays onto horizontal planes", async () => {
  const env = createContext({});
  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();

  const api = env.context.__gosx_scene3d_api;
  assert.equal(typeof api.sceneScreenToRay, "function");
  assert.equal(typeof api.sceneRayIntersectYPlane, "function");
  assert.equal(typeof api.sceneRayIntersectPlane, "function");
  assert.equal(typeof api.sceneRayIntersectSphere, "function");
  assert.equal(typeof api.sceneRayIntersectAABB, "function");

  const screenRay = api.sceneScreenToRay(100, 100, 200, 200, { x: 0, y: 2, z: 6, rotationX: -0.4, fov: 90, near: 0.1, far: 100 });
  assert.ok(screenRay && Number.isFinite(screenRay.origin.x) && Number.isFinite(screenRay.dir.z));

  const ray = { origin: { x: 0, y: 2, z: 6 }, dir: { x: 0.2, y: -1, z: -0.4 } };
  const hit = api.sceneRayIntersectYPlane(ray, 0);
  assert.ok(hit, "expected ray-plane hit");
  assert.ok(hit.distance > 0, "expected positive hit distance");
  assert.ok(Math.abs(hit.y) < 1e-9, "expected y=0 plane hit, got " + hit.y);
  assert.ok(Number.isFinite(hit.x));
  assert.ok(Number.isFinite(hit.z));
  const tiltedHit = api.sceneRayIntersectPlane(ray, { x: 0, y: 1, z: 0 }, { x: 0, y: 1, z: 1 });
  assert.ok(tiltedHit, "expected arbitrary ray-plane hit");
  assert.ok(tiltedHit.distance > 0, "expected positive arbitrary plane hit distance");
  assert.ok(Number.isFinite(tiltedHit.x));
  assert.ok(Number.isFinite(tiltedHit.y));
  assert.ok(Number.isFinite(tiltedHit.z));
  const sphereHit = api.sceneRayIntersectSphere(ray, { x: 0, y: 0, z: 5.2 }, 1);
  assert.ok(sphereHit, "expected ray-sphere hit");
  const boxHit = api.sceneRayIntersectAABB(ray, { x: -1, y: -1, z: 4 }, { x: 1, y: 1, z: 8 });
  assert.ok(boxHit, "expected ray-AABB hit");
});

test("bootstrap preserves Scene3D instanced colors and custom attributes", async () => {
  const env = createContext({});
  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();

  const api = env.context.__gosx_scene3d_api;
  const state = api.createSceneState({
    scene: {
      instancedMeshes: [
        {
          id: "muzzle-flashes",
          kind: "sphere",
          count: 2,
          transforms: new Array(32).fill(0),
          colors: ["#ffcc66", "#66ccff"],
          attributes: { heat: [1, 0.35] },
        },
      ],
    },
  });

  assert.equal(state.instancedMeshes.length, 1);
  assert.deepEqual(state.instancedMeshes[0].colors, ["#ffcc66", "#66ccff"]);
  assert.deepEqual(state.instancedMeshes[0].attributes.heat, [1, 0.35]);
});

test("bootstrap preserves Scene3D instanced primitive parameters", async () => {
  const env = createContext({});
  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();

  const api = env.context.__gosx_scene3d_api;
  const state = api.createSceneState({
    scene: {
      instancedMeshes: [
        {
          id: "native-torus",
          kind: "TorusGeometry",
          count: 1,
          radius: 1.25,
          tube: 0.2,
          radialSegments: 40,
          tubularSegments: 12,
        },
        {
          id: "native-cone",
          kind: "ConeGeometry",
          count: 1,
          radiusBottom: 0.75,
          height: 2.5,
          segments: 28,
        },
      ],
    },
  });

  assert.equal(state.instancedMeshes[0].kind, "torus");
  assert.equal(state.instancedMeshes[0].radius, 1.25);
  assert.equal(state.instancedMeshes[0].tube, 0.2);
  assert.equal(state.instancedMeshes[0].radialSegments, 40);
  assert.equal(state.instancedMeshes[0].tubularSegments, 12);
  assert.equal(state.instancedMeshes[1].kind, "cone");
  assert.equal(state.instancedMeshes[1].radiusBottom, 0.75);
  assert.equal(state.instancedMeshes[1].height, 2.5);
  assert.equal(state.instancedMeshes[1].segments, 28);

  const torus = api.generateInstancedGeometry("torusGeometry", {
    radius: 1.25,
    tube: 0.2,
    radialSegments: 20,
    tubularSegments: 10,
  });
  const cylinder = api.generateInstancedGeometry("cylinderGeometry", {
    radiusTop: 0.4,
    radiusBottom: 0.8,
    height: 2,
    segments: 12,
  });
  const cone = api.generateInstancedGeometry("coneGeometry", {
    radiusBottom: 0.8,
    height: 2,
    segments: 12,
  });

  assert.equal(torus.vertexCount, 20 * 10 * 6);
  assert.equal(cylinder.vertexCount, 12 * 12);
  assert.equal(cone.vertexCount, 12 * 6);
  assert.equal(torus.positions.length, torus.vertexCount * 3);
  assert.equal(cylinder.normals.length, cylinder.vertexCount * 3);
  assert.equal(cone.uvs.length, cone.vertexCount * 2);
});
