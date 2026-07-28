"use strict";
// WebGPU board bundles and the Selena water passes, including the compiled
// fixture parity checks and the per-frame device-call shape budgets.
//
// Split out of the former client/js/runtime.test.js, which this set replaces
// and which no longer exists. Every shared fake, sandbox builder and fixture
// factory lives in ./runtime-test-harness.js.

const test = require("node:test");
const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const vm = require("node:vm");

const {
  bootstrapRuntimeSource,
  FakeWebGLContext,
  createContext,
  runScript,
  flushAsyncWork,
  goBoardBundleRectsJSON,
  goBoardBundleMixedJSON,
  makeFakeGPUDevice,
  freshFeatureBundleSource,
  createBoardWebGPUHarness,
  mainRenderPasses,
  waterPoolSelenaFixture,
  waterSurfaceSelenaFixture,
  waterSurfaceBelowSelenaFixture,
  waterCausticsSelenaFixture,
  waterObjectMaterialSelenaFixture,
  waterDuckMaterialSelenaFixture,
  waterObjectShadowSelenaFixture,
  waterCompoundShadowSelenaFixture,
  waterObjectMeshShadowSelenaFixture,
  waterSeedSelenaFixture,
  waterDropSelenaFixture,
  waterDisplacementSelenaFixture,
  waterSimulationSelenaFixture,
  waterNormalSelenaFixture,
  waterSelenaFrameEntry,
  waterSelenaFieldFloats,
  waterSelenaLastUniformWrite,
  waterPerfShapeScene,
  renderWaterPerfShapeFrames,
  waterComputeKernelPipeline,
  assertWaterComputeKernelBindings,
} = require("./runtime-test-harness.js");

test("Selena water surface samples only the projected texture required by each mesh subtype", () => {
  const wgsl = waterSurfaceSelenaFixture.wgsl;
  assert.equal((wgsl.match(/if \(\(objActive && \(u\.objectKind < 2\.5\)\)\) \{/g) || []).length, 2,
    "analytic refraction and reflection work must both be excluded from the duck/torus path");
  assert.equal((wgsl.match(/textureSampleLevel\(causticTexture/g) || []).length, 2,
    "analytic object caustics must remain hit-conditional explicit-LOD samples");
  assert.doesNotMatch(wgsl, /textureSample\(object(?:Refraction|Reflection|ClippedReflection)Tex/,
    "projected mesh targets must use explicit LOD so Selena may keep them in nonuniform hit/subtype branches");
  assert.match(wgsl, /if \(\(meshRefrHit < 100000\.0\)\) \{[\s\S]*?textureSampleLevel\(objectRefractionTex/,
    "refraction target fetch must be guarded by the mesh bounds hit");
  assert.match(wgsl, /if \(\(\(u\.objectSubtype > 0\.5\) && \(u\.objectSubtype < 1\.5\)\)\) \{[\s\S]*?textureSampleLevel\(objectReflectionTex/,
    "torus reflection target fetch must live only in the torus subtype branch");
  assert.match(wgsl, /\} else \{[\s\S]*?textureSampleLevel\(objectClippedReflectionTex/,
    "duck/glTF clipped-reflection fetch must live only in the non-torus branch");
  assert.equal((wgsl.match(/textureSampleLevel\(object(?:Refraction|Reflection|ClippedReflection)Tex/g) || []).length, 3,
    "compiled surface must contain exactly one guarded fetch for each projected target");
});

test("compiled Selena pool keeps box and rounded geometry on the same open-vessel winding", () => {
  const wgsl = waterPoolSelenaFixture.wgsl;
  assert.match(wgsl, /let uf = select\(select\(select\(0\.0, 1\.0, \(cu == 5\.0\)\), 1\.0, \(cu == 2\.0\)\), 1\.0, \(cu == 1\.0\)\)/,
    "compiled box U corners must reverse the old exterior-facing triangle order");
  assert.match(wgsl, /let vf = select\(select\(select\(0\.0, 1\.0, \(cu == 5\.0\)\), 1\.0, \(cu == 4\.0\)\), 1\.0, \(cu == 1\.0\)\)/,
    "compiled box V corners must make the floor face up and walls face inward");
});

test("Scene3D WebGPU pool pass routes through the generic Selena render path with matching bindings", async () => {
  // { fresh: true } builds the scene3d + scene3d-webgpu chunks straight from
  // bootstrap-src/*.js (see freshFeatureBundleSource) so this test exercises
  // the pool-pass Selena-routing source edits directly, without regenerating
  // the committed bootstrap-feature-*.js bundle artifacts. { validateBindings:
  // true } turns on makeFakeGPUDevice's structural binding-mismatch gate: any
  // @group/@binding drift between the pool WGSL and the bind group
  // layout/bind group the renderer builds throws immediately.
  const harness = await createBoardWebGPUHarness({
    fresh: true,
    fakeDeviceOptions: { validateBindings: true },
  });
  const api = harness.env.context.__gosx_scene3d_api;
  assert.ok(api && typeof api.createSceneState === "function", "scene3d chunk must publish createSceneState");

  const waterEntry = {
    id: "water-main",
    resolution: 16,
    surfaceResolution: 3,
    poolShape: "Box",
    poolWidth: 1,
    poolLength: 1,
    poolHeight: 1,
    cornerRadius: 0,
    tileTexture: "",
    causticsResolution: 4,
    objectShadowResolution: 4,
    lightDirectionX: 2,
    lightDirectionY: 2,
    lightDirectionZ: -1,
    materialBackend: "selena",
    // The real, Selena-compiled pool WGSL + descriptor under test. Everything
    // else on this entry is the OLD hand-written-WGSL water contract (left
    // completely alone -- this test only exercises the additive pool slot).
    poolSelenaWGSL: waterPoolSelenaFixture.wgsl,
    shaderDescriptors: { pool: waterPoolSelenaFixture.layout },
  };

  // Drive the entry through the SAME normalization the production runtime
  // uses (createSceneState -> sceneWaterSystems -> normalizeSceneWaterSystemEntry
  // in 10-runtime-scene-core.js), proving the poolSelenaWGSL/shaderDescriptors
  // plumbing survives that layer, not just a hand-built bundle.
  const sceneState = api.createSceneState({ scene: { waterSystems: [waterEntry] } });
  assert.equal(sceneState.waterSystems.length, 1);
  assert.equal(sceneState.waterSystems[0].poolSelenaWGSL, waterPoolSelenaFixture.wgsl, "poolSelenaWGSL must survive normalizeSceneWaterSystemEntry");
  assert.ok(
    sceneState.waterSystems[0].shaderDescriptors && sceneState.waterSystems[0].shaderDescriptors.pool,
    "shaderDescriptors.pool must survive normalizeSceneWaterSystemEntry",
  );

  harness.canvas.width = 64;
  harness.canvas.height = 64;
  assert.doesNotThrow(() => {
    harness.renderer.render(sceneState, { width: 64, height: 64 });
  }, "render() must not throw -- a throw here means the fake device's validator caught a @group/@binding mismatch between the pool WGSL and the renderer's bind group layout/bind group");

  const mount = harness.mount;
  assert.equal(mount.getAttribute("data-gosx-scene3d-webgpu-water-selena-pool-passes"), "1", "the pool pass must have been routed through the generic Selena path exactly once");
  assert.equal(mount.getAttribute("data-gosx-scene3d-webgpu-water-selena-pool-fallbacks"), "0", "the Selena pool path must not have fallen back to the hand-written pipeline");
  assert.equal(mount.getAttribute("data-gosx-scene3d-webgpu-water-pool-passes"), "1");

  // Structural corroboration: find the compiled Selena pool render pipeline
  // and its bind group layout, and confirm the layout carries every binding
  // the pool descriptor declares (uniform block, 3 textures/samplers, the
  // StateGrid uniform, and the state storage buffer) -- i.e. the new
  // sceneSelenaBindGroupLayout `state`/`grid` support actually ran, it wasn't
  // silently skipped.
  const poolPipeline = harness.fake.state.renderPipelines.find(
    (p) => p.desc && p.desc.label && String(p.desc.label).indexOf("WaterPool") >= 0,
  );
  assert.ok(poolPipeline, "expected a compiled gosx-selena-*-WaterPool-* render pipeline");
  const poolBGL = poolPipeline.desc.layout.desc.bindGroupLayouts[0];
  // Array.from (the OUTER realm's, not the sandboxed vm context's) copies the
  // vm-context entries into plain outer-realm values -- they were built by
  // code running inside env.context's vm.Context, so without this copy
  // assert.deepEqual's cross-realm reference-equality check fails even when
  // the values are identical (Node's util.isDeepStrictEqual distinguishes
  // exotic objects from a different realm).
  const poolBindings = Array.from(poolBGL.desc.entries, (e) => e.binding).sort((a, b) => a - b);
  assert.deepEqual(poolBindings, [0, 1, 2, 3, 4, 5, 6, 7, 8], "pool bind group layout must expose uniform(0) + 3 textures/samplers(1-6) + grid(7) + state(8)");

  const poolBindGroup = harness.fake.state.bindGroups.find((bg) => bg.desc && bg.desc.layout === poolBGL);
  assert.ok(poolBindGroup, "expected a bind group built against the pool bind group layout");
  const boundBindings = Array.from(poolBindGroup.desc.entries, (e) => e.binding).sort((a, b) => a - b);
  assert.deepEqual(boundBindings, poolBindings, "the bind group's actual entries must match its layout's declared bindings exactly");
});

test("normalizeSceneWaterSystemEntry passes every migrated pass's Selena WGSL slot + descriptor key through", async () => {
  // {fresh: true} -- this test exercises the NEW bootstrap-src edits
  // (10-runtime-scene-core.js's normalizeSceneWaterSystemEntry whitelist)
  // directly; the committed bootstrap.js bundle predates them (see the pool
  // test's {fresh:true} comment above for why).
  const harness = await createBoardWebGPUHarness({ fresh: true });
  const api = harness.env.context.__gosx_scene3d_api;

  const waterEntry = waterSelenaFrameEntry();
  const state = api.createSceneState({ scene: { waterSystems: [waterEntry] } });
  assert.equal(state.waterSystems.length, 1);
  const normalized = state.waterSystems[0];
  assert.equal(normalized.surfaceSelenaWGSL, waterSurfaceSelenaFixture.wgsl);
  assert.equal(normalized.surfaceBelowSelenaWGSL, waterSurfaceBelowSelenaFixture.wgsl);
  assert.equal(normalized.causticsSelenaWGSL, waterCausticsSelenaFixture.wgsl);
  assert.equal(normalized.objectShadowSelenaWGSL, waterObjectShadowSelenaFixture.wgsl);
  assert.equal(normalized.compoundShadowSelenaWGSL, waterCompoundShadowSelenaFixture.wgsl);
  assert.equal(normalized.objectMeshShadowSelenaWGSL, waterObjectMeshShadowSelenaFixture.wgsl);
  assert.ok(normalized.shaderDescriptors && normalized.shaderDescriptors.surface, "shaderDescriptors.surface must survive normalization");
  assert.ok(normalized.shaderDescriptors.surfaceBelow, "shaderDescriptors.surfaceBelow must survive normalization");
  assert.ok(normalized.shaderDescriptors.caustics, "shaderDescriptors.caustics must survive normalization");
  assert.ok(normalized.shaderDescriptors.objectShadow, "shaderDescriptors.objectShadow must survive normalization");
  assert.ok(normalized.shaderDescriptors.compoundShadow, "shaderDescriptors.compoundShadow must survive normalization");
  assert.ok(normalized.shaderDescriptors.objectMeshShadow, "shaderDescriptors.objectMeshShadow must survive normalization");
});

test("Scene3D WebGPU water surface/surface-below/caustics passes route through the generic Selena render path", async () => {
  // { fresh: true } / { validateBindings: true } -- see the pool test above
  // for why both are needed here.
  const harness = await createBoardWebGPUHarness({
    fresh: true,
    fakeDeviceOptions: { validateBindings: true },
  });
  const api = harness.env.context.__gosx_scene3d_api;

  // No active object: exercises the surface/surface-below/caustics passes'
  // "no live displacement subject" defaults (objectKind/objectCount/opticsEnable
  // all fall back to 0), leaving the compound-shadow/object-mesh-shadow
  // passes (which require an active kind===3 object) untouched -- those are
  // covered by the next test.
  //
  // HDR water color/tileTexture/cubeMap/activeObject are set to non-default,
  // asserted-against values below: the linear HDR color feeds WaterSurface's
  // `param waterColor` without display-referred hex clamping
  // in 16a-scene-webgpu.js); tileTexture/cubeMap exercise the
  // literal-URL-texture path (as opposed to a gosx:water:* live resource ref);
  // the active Sphere object drives opticsEnable to 1.
  const waterEntry = waterSelenaFrameEntry({
    shallowColor: "#224466",
    aboveWaterColorR: 0.25,
    aboveWaterColorG: 1,
    aboveWaterColorB: 1.25,
    tileTexture: "/water/tiles.jpg",
    cubeMap: "/water/",
    activeObject: "Sphere",
    objectKind: "sphere",
    objectX: -0.4,
    objectY: -0.75,
    objectZ: 0.2,
    objectRadius: 0.25,
    objectDisplacementScale: 1,
  });
  const sceneState = api.createSceneState({ scene: { waterSystems: [waterEntry] } });

  harness.canvas.width = 64;
  harness.canvas.height = 64;
  assert.doesNotThrow(() => {
    harness.renderer.render(sceneState, { width: 64, height: 64 });
  }, "render() must not throw -- a throw here means the fake device's validator caught a @group/@binding mismatch");

  const mount = harness.mount;
  // Surface pass fires once per side (above + below) per system.
  assert.equal(mount.getAttribute("data-gosx-scene3d-webgpu-water-selena-surface-passes"), "2", "surface + surface-below must both have routed through the Selena path");
  assert.equal(mount.getAttribute("data-gosx-scene3d-webgpu-water-selena-surface-fallbacks"), "0");
  assert.equal(mount.getAttribute("data-gosx-scene3d-webgpu-water-selena-caustic-passes"), "1", "caustics must have routed through the Selena path");
  assert.equal(mount.getAttribute("data-gosx-scene3d-webgpu-water-selena-caustic-fallbacks"), "0");

  // Structural corroboration mirroring the pool test: the compiled surface
  // pipeline's bind group layout must expose the full descriptor binding
  // set (uniform + 6 textures/samplers + grid + state -- surface.sel has
  // tile/caustic/sky/refraction/reflection/clippedReflection), and the bind
  // group built against it must match exactly (proves the cube-texture
  // dimension support sceneSelenaBindGroupLayout/createSelenaBindGroup added
  // for "sky" actually ran).
  const surfacePipeline = harness.fake.state.renderPipelines.find(
    (p) => p.desc && p.desc.label && String(p.desc.label).indexOf("WaterSurface-") >= 0 && String(p.desc.label).indexOf("WaterSurfaceBelow") < 0,
  );
  assert.ok(surfacePipeline, "expected a compiled gosx-selena-*-WaterSurface-* render pipeline");
  const surfaceBGL = surfacePipeline.desc.layout.desc.bindGroupLayouts[0];
  const surfaceBindings = Array.from(surfaceBGL.desc.entries, (e) => e.binding).sort((a, b) => a - b);
  assert.deepEqual(surfaceBindings, [0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14], "surface bind group layout must expose uniform(0) + 6 textures/samplers(1-12) + grid(13) + state(14)");
  const skyEntry = surfaceBGL.desc.entries.find((e) => e.binding === 5);
  assert.ok(skyEntry && skyEntry.texture && skyEntry.texture.viewDimension === "cube", "the sky texture binding must be viewDimension:\"cube\"");
  const stateEntry = surfaceBGL.desc.entries.find((e) => e.binding === 14);
  assert.ok(stateEntry && stateEntry.texture && stateEntry.texture.sampleType === "unfilterable-float", "stateAt must bind the Selena-declared rgba32float sampled texture, not a storage buffer");

  const surfaceBindGroup = harness.fake.state.bindGroups.find((bg) => bg.desc && bg.desc.layout === surfaceBGL);
  assert.ok(surfaceBindGroup, "expected a bind group built against the surface bind group layout");
  const surfaceBoundBindings = Array.from(surfaceBindGroup.desc.entries, (e) => e.binding).sort((a, b) => a - b);
  assert.deepEqual(surfaceBoundBindings, surfaceBindings, "the surface bind group's actual entries must match its layout's declared bindings exactly");
  const boundState = surfaceBindGroup.desc.entries.find((e) => e.binding === 14);
  assert.equal(boundState && boundState.resource && boundState.resource.__kind, "textureView", "stateAt texture binding must receive the live sampled state view");

  // Caustics renders into its own offscreen (no depth attachment) target:
  // the compiled pipeline must NOT carry a depthStencil state.
  const causticsPipeline = harness.fake.state.renderPipelines.find(
    (p) => p.desc && p.desc.label && String(p.desc.label).indexOf("WaterCaustics") >= 0,
  );
  assert.ok(causticsPipeline, "expected a compiled gosx-selena-*-WaterCaustics-* render pipeline");
  assert.equal(causticsPipeline.desc.depthStencil, undefined, "the caustics pipeline must omit depthStencil (its render target has no depth attachment)");

  // VALUES GATE (root-cause regression test): capture the actual
  // queue.writeBuffer bytes for the surface pass's uniform block and assert
  // on the packed values, not just structural bind-group shape. Before the
  // sceneWaterSurfaceSelenaRenderContext fix this test would have PASSED
  // structurally (bind group built fine, no @group/@binding mismatch) while
  // silently packing waterColor as (0,0,0) -- surface.sel multiplies the
  // ENTIRE refracted branch (pool floor/walls/caustics/submerged objects) by
  // waterColor whenever the refraction ray points down into the water (the
  // common case looking down at the pool), which is the near-black surface
  // regression. A zero waterColor is indistinguishable from a legitimately
  // dark shallowColor at the bind-group-shape level, so only a values
  // assertion catches it.
  const surfaceFloats = waterSelenaLastUniformWrite(harness.fake, surfaceBindGroup);
  const waterColor = waterSelenaFieldFloats(waterSurfaceSelenaFixture.layout, surfaceFloats, "waterColor", 3);
  assert.ok(waterColor.some((c) => c > 0.01), "waterColor must not be packed as (0,0,0) -- surface.sel's `param waterColor` was left unwired, blacking out the refracted branch");
  assert.ok(Math.abs(waterColor[0] - 0.25) < 1e-5, "waterColor.r must preserve the linear HDR contract");
  assert.ok(Math.abs(waterColor[1] - 1) < 1e-5, "waterColor.g must preserve the linear HDR contract");
  assert.ok(Math.abs(waterColor[2] - 1.25) < 1e-5, "waterColor.b must remain above 1 instead of being clamped through a hex color");

  const lightDir = waterSelenaFieldFloats(waterSurfaceSelenaFixture.layout, surfaceFloats, "lightDir", 3);
  const lightLen = Math.sqrt(lightDir[0] * lightDir[0] + lightDir[1] * lightDir[1] + lightDir[2] * lightDir[2]);
  assert.ok(lightLen > 0.5, "lightDir must be a nonzero vector reaching the surface pass (entry lightDirection(2,2,-1))");
  assert.deepEqual(lightDir.map((c) => Math.round(c * 1e4) / 1e4), [0.6667, 0.6667, -0.3333], "lightDir must equal the normalized entry.lightDirection(2,2,-1), not zero");

  const opticsEnable = waterSelenaFieldFloats(waterSurfaceSelenaFixture.layout, surfaceFloats, "opticsEnable", 1)[0];
  assert.equal(opticsEnable, 1, "opticsEnable must be 1 with an active displacement object (Sphere)");

  // normalScale: secondary parity gap alongside waterColor (see
  // sceneWaterSurfaceSelenaRenderContext's comment) -- WaterSystem's
  // waterSelenaFrameEntry fixture doesn't override normalScale, so the
  // default-config value (1.0) must reach the buffer either via the live
  // entry.normalScale forward or the compiled descriptor default; assert the
  // forwarded value explicitly so a future normalScale override is covered.
  const normalScale = waterSelenaFieldFloats(waterSurfaceSelenaFixture.layout, surfaceFloats, "normalScale", 1)[0];
  assert.equal(normalScale, 1, "normalScale must reach the surface pass uniform buffer (entry.normalScale, default 1.0)");

  // Every real (non-mesh-RTT-dependent) texture binding must resolve to a
  // LIVE view, not silently collapse to the shared 1x1 placeholder. With
  // tileTexture/cubeMap URLs supplied and the caustics pass having rendered
  // this frame, tile(1)/caustic(3)/sky(5) must each be genuinely distinct
  // resources; objectRefractionTex(7)/objectReflectionTex(9)/
  // objectClippedReflectionTex(11) legitimately fall back to the SAME shared
  // placeholder here (a plain Sphere, kind<2.5, never populates the
  // mesh-projected RTT targets) -- comparing against that known-placeholder
  // group is a robust way to prove the other three are NOT placeholders
  // without reaching into renderer-private state.
  // The fake device's createTexture().createView() mints a FRESH wrapper
  // object on every call (even for the same underlying texture), so identity
  // is compared via textureId (the fake's real per-texture identity marker),
  // not object reference equality.
  function textureIdFor(binding) {
    const entry = surfaceBindGroup.desc.entries.find((e) => e.binding === binding);
    assert.ok(entry, "expected a texture entry at binding " + binding);
    assert.ok(entry.resource && entry.resource.__kind === "textureView", "expected binding " + binding + " to resolve to a textureView resource");
    return entry.resource.textureId;
  }
  const tileId = textureIdFor(1);
  const causticId = textureIdFor(3);
  const skyId = textureIdFor(5);
  const refractionId = textureIdFor(7);
  const reflectionId = textureIdFor(9);
  const clippedReflectionId = textureIdFor(11);
  assert.equal(reflectionId, refractionId, "sanity: with no active mesh-RTT subject, objectReflectionTex shares the same placeholder as objectRefractionTex");
  assert.equal(clippedReflectionId, refractionId, "sanity: with no active mesh-RTT subject, objectClippedReflectionTex shares the same placeholder as objectRefractionTex");
  assert.notEqual(tileId, refractionId, "tileTexture must resolve to the loaded /water/tiles.jpg view, not fall back to the placeholder");
  assert.notEqual(causticId, refractionId, "causticTexture must resolve to the live gosx:water:*:caustics render-target view, not fall back to the placeholder");
  assert.notEqual(skyId, refractionId, "sky must resolve to the loaded cube-map view, not fall back to the placeholder");
});

test("Scene3D WebGPU water compound-shadow / object-mesh-shadow passes route through the generic Selena render path (G1 array-uniform packing)", async () => {
  const harness = await createBoardWebGPUHarness({
    fresh: true,
    fakeDeviceOptions: { validateBindings: true },
  });
  const api = harness.env.context.__gosx_scene3d_api;

  // Two known displacement spheres, chosen so their (offsetX,offsetY,offsetZ,
  // radius) values pass through sceneWaterDisplacementSpheres UNCHANGED
  // (poolWidth=poolLength=1 makes halfWidth=halfLength=1, so the
  // pool-relative normalization divides by 1) -- this is the G1 test: known
  // array input -> expected float offsets in the packed uniform buffer.
  const sphere0 = { offsetX: 0.2, offsetY: 0.05, offsetZ: -0.1, radius: 0.08 };
  const sphere1 = { offsetX: -0.15, offsetY: 0.02, offsetZ: 0.12, radius: 0.05 };

  const waterEntry = waterSelenaFrameEntry({
    activeObject: "Rubber Duck",
    objectKind: "compound",
    objectSubtype: "duck",
    objectX: 0,
    objectY: 0,
    objectZ: 0,
    objectDisplacementScale: 1,
    objectDisplacementSpheres: [sphere0, sphere1],
  });

  const state = api.createSceneState({
    scene: {
      materials: [{
        name: "water-duck-material",
        kind: "custom",
        shaderBackend: "selena",
        customVertexWGSL: waterDuckMaterialSelenaFixture.wgsl,
        customFragmentWGSL: waterDuckMaterialSelenaFixture.wgsl,
        shaderLayout: waterDuckMaterialSelenaFixture.layout,
        customUniforms: {
          poolHeight: 1,
          baseColor: [1, 1, 1, 1],
          isTexturePass: 0,
          texturePassMode: 0,
          lightDir: [2, 3, -1],
          grid: 4,
          water: "gosx:water:water-main:state",
        },
      }],
      objects: [
        { id: "float-duck", kind: "sphere", radius: 0.1, x: 0, y: 0, z: 0, material: "water-duck-material", castShadow: true, wireframe: false },
      ],
      waterSystems: [waterEntry],
    },
  }, { tier: "full" });

  const objects = api.sceneStateObjectsWithMaterials(state);
  const bundle = api.createSceneRenderBundle(
    64, 64, "#000000",
    { x: 0, y: 0, z: 4, fov: 60, near: 0.05, far: 128 },
    objects, [], [], [], [], {}, 0, [], [], [], state.waterSystems, [], 0, false,
  );

  harness.canvas.width = 64;
  harness.canvas.height = 64;
  assert.doesNotThrow(() => {
    harness.renderer.render(bundle, { width: 64, height: 64 }, { nowMS: 0, active: true });
    harness.renderer.render(bundle, { width: 64, height: 64 }, { nowMS: 17, active: true });
  }, "render() must not throw -- a throw here means the fake device's validator caught a @group/@binding mismatch");

  const mount = harness.mount;
  assert.equal(mount.getAttribute("data-gosx-scene3d-webgpu-water-selena-object-mesh-shadow-passes"), "1", "object-mesh-shadow must have routed through the Selena path");
  assert.equal(mount.getAttribute("data-gosx-scene3d-webgpu-water-selena-object-mesh-shadow-fallbacks"), "0");

  // The active object is a compound (kind 3) subject with mesh geometry, so
  // renderWaterObjectMeshShadowPass's meshShadow.passes>0 branch runs and
  // renderWaterObjectShadowPass (compound-shadow's OWN post-kind pass) is
  // never reached this frame -- matching the raw hand-written shader's own
  // control flow (a mesh subject always prefers the projected-mesh shadow).
  // compound-shadow's post-kind pipeline/bind-group path is exercised
  // directly below instead, against a hand-built material+renderContext,
  // proving the SAME getSelenaPostPipeline/createSelenaPostBindGroup
  // machinery works end-to-end with a real G1 array context field.
  assert.equal(mount.getAttribute("data-gosx-scene3d-webgpu-water-selena-object-shadow-passes"), "0");

  // G1 corroboration: find the compound-shadow UserUniforms write (576
  // bytes == 144 floats, per waterCompoundShadowSelenaFixture.layout's
  // uniformBlock.size) and confirm the "spheres" array field (offset 64
  // bytes -> float index 16, stride 16 bytes -> 4 floats/element) carries
  // sphere0/sphere1 at their exact expected offsets. This proves
  // sceneSelenaWriteArrayUniformField's std140 element-stride math, not just
  // that SOME uniform buffer was written.
  //
  // The compound-shadow post-kind pass itself isn't drawn this frame (see
  // above), but object-mesh-shadow's mesh-kind pass ALSO carries a "spheres"-
  // shaped array field? No -- object-mesh-shadow has none. So drive
  // compound-shadow's bind group directly through the SAME public entry
  // point the renderer itself uses: build a synthetic second frame where the
  // mesh subject is ABSENT (no matching float-duck mesh object), forcing
  // renderWaterObjectShadowPass's compound-shadow branch to run.
  const bundleNoMesh = api.createSceneRenderBundle(
    64, 64, "#000000",
    { x: 0, y: 0, z: 4, fov: 60, near: 0.05, far: 128 },
    [], [], [], [], [], {}, 0, [], [], [], state.waterSystems, [], 0, false,
  );
  assert.doesNotThrow(() => {
    harness.renderer.render(bundleNoMesh, { width: 64, height: 64 });
  }, "render() must not throw on the mesh-less frame");
  assert.equal(mount.getAttribute("data-gosx-scene3d-webgpu-water-selena-object-shadow-passes"), "1", "compound-shadow must have routed through the Selena post-kind path once the mesh subject is absent");
  assert.equal(mount.getAttribute("data-gosx-scene3d-webgpu-water-selena-object-shadow-fallbacks"), "0");

  const spheresWrite = harness.fake.state.writeBufferCalls.find((call) => call.data && call.data.length === 144);
  assert.ok(spheresWrite, "expected a 576-byte (144-float) UserUniforms write for the compound-shadow material");
  const floats = Array.from(spheresWrite.data);
  const sphereBase = 16; // 64 bytes / 4
  // Compare against Math.fround (the value re-rounded through float32) since
  // spheresWrite.data is itself a Float32Array -- sphere0.offsetX (0.2) is
  // not exactly representable, so a plain equality would fail on the
  // representable-vs-source-literal rounding, not a packing bug.
  assert.equal(floats[sphereBase + 0], Math.fround(sphere0.offsetX));
  assert.equal(floats[sphereBase + 1], Math.fround(sphere0.offsetY));
  assert.equal(floats[sphereBase + 2], Math.fround(sphere0.offsetZ));
  assert.equal(floats[sphereBase + 3], Math.fround(sphere0.radius));
  assert.equal(floats[sphereBase + 4], Math.fround(sphere1.offsetX));
  assert.equal(floats[sphereBase + 5], Math.fround(sphere1.offsetY));
  assert.equal(floats[sphereBase + 6], Math.fround(sphere1.offsetZ));
  assert.equal(floats[sphereBase + 7], Math.fround(sphere1.radius));
  // Everything past the 2 real spheres must be zero-filled, not garbage.
  assert.equal(floats[sphereBase + 8], 0);
  assert.equal(floats[sphereBase + 4 * 31], 0);
});

test("Scene3D WebGPU water object-material/duck-material meshes route through the generic Selena render path", async () => {
  const harness = await createBoardWebGPUHarness({
    fresh: true,
    fakeDeviceOptions: { validateBindings: true },
  });
  const api = harness.env.context.__gosx_scene3d_api;

  const waterEntry = waterSelenaFrameEntry();

  const state = api.createSceneState({
    scene: {
      materials: [
        {
          name: "water-object-material",
          kind: "custom",
          shaderBackend: "selena",
          customVertexWGSL: waterObjectMaterialSelenaFixture.wgsl,
          customFragmentWGSL: waterObjectMaterialSelenaFixture.wgsl,
          shaderLayout: waterObjectMaterialSelenaFixture.layout,
          customUniforms: {
            poolHeight: 1,
            poolWidth: 1,
            poolLength: 1,
            baseColor: [0.5, 0.5, 0.5, 1],
            isTexturePass: 0,
            texturePassMode: 0,
            lightDir: [2, 3, -1],
            grid: 4,
            water: "gosx:water:water-main:state",
            causticTexture: "gosx:water:water-main:caustics",
          },
        },
        {
          name: "water-duck-material",
          kind: "custom",
          shaderBackend: "selena",
          customVertexWGSL: waterDuckMaterialSelenaFixture.wgsl,
          customFragmentWGSL: waterDuckMaterialSelenaFixture.wgsl,
          shaderLayout: waterDuckMaterialSelenaFixture.layout,
          customUniforms: {
            poolHeight: 1,
            poolWidth: 1,
            poolLength: 1,
            baseColor: [1, 1, 1, 1],
            isTexturePass: 0,
            texturePassMode: 0,
            lightDir: [2, 3, -1],
            modelTexture: "/water/models/duck/DuckCM.png",
            grid: 4,
            water: "gosx:water:water-main:state",
            causticTexture: "gosx:water:water-main:caustics",
          },
        },
      ],
      objects: [
        { id: "float-sphere", kind: "sphere", radius: 0.25, x: -0.4, y: -0.75, z: 0.2, material: "water-object-material", wireframe: false },
        { id: "float-duck", kind: "sphere", radius: 0.1, x: 0.4, y: -0.7, z: -0.2, material: "water-duck-material", wireframe: false },
      ],
      waterSystems: [waterEntry],
    },
  });

  const objects = api.sceneStateObjectsWithMaterials(state);
  const bundle = api.createSceneRenderBundle(
    64, 64, "#000000",
    { x: 0, y: 0, z: 4, fov: 60, near: 0.05, far: 128 },
    objects, [], [], [], [], {}, 0, [], [], [], state.waterSystems, [], 0, false,
  );

  harness.canvas.width = 64;
  harness.canvas.height = 64;
  assert.doesNotThrow(() => {
    harness.renderer.render(bundle, { width: 64, height: 64 });
  }, "render() must not throw -- a throw here means the fake device's validator caught a @group/@binding mismatch");

  // Both materials are drawn through drawPBRObjects' fully-generic Selena
  // mesh path (getSelenaPipeline/createSelenaBindGroup) -- the SAME path
  // pool/surface use, generalized by sceneSelenaGridUniformData's
  // material.customUniforms.grid fallback (drawPBRObjects supplies no
  // renderContext at all, unlike the water-system-owned passes above).
  const objectMatPipeline = harness.fake.state.renderPipelines.find(
    (p) => p.desc && p.desc.label && String(p.desc.label).indexOf("WaterObjectMaterial") >= 0,
  );
  assert.ok(objectMatPipeline, "expected a compiled gosx-selena-*-WaterObjectMaterial-* render pipeline");
  const duckMatPipeline = harness.fake.state.renderPipelines.find(
    (p) => p.desc && p.desc.label && String(p.desc.label).indexOf("WaterDuckMaterial") >= 0,
  );
  assert.ok(duckMatPipeline, "expected a compiled gosx-selena-*-WaterDuckMaterial-* render pipeline");

  // Object material samples the real caustic target; duck adds its albedo
  // texture. Both then bind the state grid and sampled height texture.
  const objectMatBGL = objectMatPipeline.desc.layout.desc.bindGroupLayouts[0];
  const objectMatBindings = Array.from(objectMatBGL.desc.entries, (e) => e.binding).sort((a, b) => a - b);
  assert.deepEqual(objectMatBindings, [0, 1, 2, 3, 4], "object-material bind group layout must expose uniform(0) + caustic(1-2) + grid(3) + state(4)");

  const duckMatBGL = duckMatPipeline.desc.layout.desc.bindGroupLayouts[0];
  const duckMatBindings = Array.from(duckMatBGL.desc.entries, (e) => e.binding).sort((a, b) => a - b);
  assert.deepEqual(duckMatBindings, [0, 1, 2, 3, 4, 5, 6], "duck-material bind group layout must expose uniform(0) + model/caustic textures(1-4) + grid(5) + state(6)");

  const objectMatBindGroup = harness.fake.state.bindGroups.find((bg) => bg.desc && bg.desc.layout === objectMatBGL);
  assert.ok(objectMatBindGroup, "expected a bind group built against the object-material bind group layout");
  const duckMatBindGroup = harness.fake.state.bindGroups.find((bg) => bg.desc && bg.desc.layout === duckMatBGL);
  assert.ok(duckMatBindGroup, "expected a bind group built against the duck-material bind group layout");

  // VALUES GATE: object-material/duck-material draw through drawPBRObjects,
  // which supplies NO renderContext at all (see createSelenaBindGroup(mat,
  // selenaResource, obj) in 16a-scene-webgpu.js -- only 3 args), so every
  // uniform field must resolve via material.customUniforms or a compiled
  // descriptor default. lightDir is a `context` field with NO compiled
  // default and baseColor is a `param` with NO literal default either -- both
  // fall to sceneSelenaUniformValue's zero fallback if customUniforms is ever
  // dropped, which reads as "TorusKnot pure white" / "duck flat/unlit" in a
  // real browser (lightDir=(0,0,0) normalizes to NaN; baseColor=(0,0,0,0)
  // zeroes the albedo). Assert the actual packed bytes carry the entry
  // config, not that value-free zero.
  const objectMatFloats = waterSelenaLastUniformWrite(harness.fake, objectMatBindGroup);
  const objectBaseColor = waterSelenaFieldFloats(waterObjectMaterialSelenaFixture.layout, objectMatFloats, "baseColor", 4);
  assert.deepEqual(objectBaseColor, [0.5, 0.5, 0.5, 1], "object-material baseColor must match the entry config (0.5,0.5,0.5,1), not the default-white/zero fallback");
  const objectLightDir = waterSelenaFieldFloats(waterObjectMaterialSelenaFixture.layout, objectMatFloats, "lightDir", 3);
  assert.deepEqual(objectLightDir, [2, 3, -1], "object-material lightDir must be the nonzero entry config (2,3,-1), not zero");
  const objectIsTexturePass = waterSelenaFieldFloats(waterObjectMaterialSelenaFixture.layout, objectMatFloats, "isTexturePass", 1)[0];
  assert.equal(objectIsTexturePass, 0, "isTexturePass must be 0 on the main-scene draw (only the RTT reflection/refraction/clipped passes set it)");

  const duckMatFloats = waterSelenaLastUniformWrite(harness.fake, duckMatBindGroup);
  const duckBaseColor = waterSelenaFieldFloats(waterDuckMaterialSelenaFixture.layout, duckMatFloats, "baseColor", 4);
  assert.deepEqual(duckBaseColor, [1, 1, 1, 1], "duck-material baseColor must match the entry config (1,1,1,1)");
  const duckLightDir = waterSelenaFieldFloats(waterDuckMaterialSelenaFixture.layout, duckMatFloats, "lightDir", 3);
  assert.deepEqual(duckLightDir, [2, 3, -1], "duck-material lightDir must be the nonzero entry config (2,3,-1), not zero");
  const duckIsTexturePass = waterSelenaFieldFloats(waterDuckMaterialSelenaFixture.layout, duckMatFloats, "isTexturePass", 1)[0];
  assert.equal(duckIsTexturePass, 0, "isTexturePass must be 0 on the main-scene draw");

  // duck-material's own modelTexture binding (1) must be present and bound
  // to a texture view (the DuckCM.png URL is supplied above) -- corroborates
  // "duck renders with its texture" is expected to keep working, isolating
  // the reported "flat/unlit" symptom to lighting/shading values rather than
  // a missing texture resource (the surface test above already exercises the
  // stronger "resolves to a live, non-placeholder view" assertion).
  const modelTexEntry = duckMatBindGroup.desc.entries.find((e) => e.binding === 1);
  assert.ok(modelTexEntry && modelTexEntry.resource && modelTexEntry.resource.__kind === "textureView", "expected duck-material's modelTexture entry at binding 1 to be a texture view");
  const objectCausticEntry = objectMatBindGroup.desc.entries.find((e) => e.binding === 1);
  assert.ok(objectCausticEntry && objectCausticEntry.resource && objectCausticEntry.resource.__kind === "textureView", "object material must sample the live caustic texture instead of treating a water-slope channel as caustic intensity");
});

test("[perf-shape] moving duck alternates only its two required RTT targets", async () => {
  const harness = await createBoardWebGPUHarness({
    fresh: true,
    verboseTelemetry: false,
    fakeDeviceOptions: { validateBindings: true },
  });
  const api = harness.env.context.__gosx_scene3d_api;
  const { state, objects } = waterPerfShapeScene(api, true, 192);
  state.waterSystems[0].seedDrops = 0;
  harness.canvas.width = 64;
  harness.canvas.height = 64;

  const objectTargetLabels = [];
  // The first frame hydrates the water resources; the following six frames
  // exercise the continuously-invalidated scheduler.
  for (let frame = 0; frame < 7; frame += 1) {
    // Camera motion changes the RTT signature every frame, reproducing the
    // continuously-moving/gravity duck failure mode without relying on a
    // browser or asynchronous model animation.
    const bundle = api.createSceneRenderBundle(
      64, 64, "#000000",
      { x: frame * 0.01, y: 0, z: 4, fov: 60, near: 0.05, far: 128 },
      objects, [], [], [], [], {}, frame * 0.016, [], [], [], state.waterSystems, [], 0, false,
    );
    const passStart = harness.fake.state.renderPasses.length;
    harness.renderer.render(bundle, { width: 64, height: 64 }, {
      nowMS: frame * 17,
      displayDeltaMS: frame === 0 ? 0 : 17,
      active: true,
      qualityTier: "balanced",
      qualityRevision: 0,
    });
    for (const pass of harness.fake.state.renderPasses.slice(passStart)) {
      const label = pass && pass.descriptor && pass.descriptor.label;
      if (label && String(label).indexOf("gosx-water-object-mesh-") === 0 && String(label).indexOf("shadow") < 0) {
        objectTargetLabels.push(label);
      }
    }
  }

  assert.deepEqual(objectTargetLabels, [
    "gosx-water-object-mesh-refraction-pass",
    "gosx-water-object-mesh-clipped-reflection-pass",
    "gosx-water-object-mesh-refraction-pass",
    "gosx-water-object-mesh-clipped-reflection-pass",
    "gosx-water-object-mesh-refraction-pass",
    "gosx-water-object-mesh-clipped-reflection-pass",
  ], "a changing signature must not reset the duck to refraction or render its unused full-reflection target");
});

test("[perf-shape] adaptive survival quality cadences moving duck RTT work", async () => {
  const harness = await createBoardWebGPUHarness({ fresh: true, verboseTelemetry: false });
  const api = harness.env.context.__gosx_scene3d_api;
  const { state, objects } = waterPerfShapeScene(api, true, 192);
  state.waterSystems[0].seedDrops = 0;
  harness.canvas.width = 64;
  harness.canvas.height = 64;
  const labels = [];
  const labelFrames = [];
  const refractionMatrices = [];
  const reflectionMatrices = [];
  const survival = {
    tier: "survival",
    dprCap: 1,
    surfaceResolution: 96,
    causticsResolution: 256,
    objectShadowResolution: 256,
    objectTextureMaxSide: 256,
    objectTexturePixelBudget: 196608,
    expensivePassCadence: 3,
  };

  for (let frame = 0; frame < 10; frame += 1) {
    const bundle = api.createSceneRenderBundle(
      64, 64, "#000000",
      { x: frame * 0.01, y: 0, z: 4, fov: 60, near: 0.05, far: 128 },
      objects, [], [], [], [], {}, frame * 0.016, [], [], [], state.waterSystems, [], 0, false,
    );
    const start = harness.fake.state.renderPasses.length;
    harness.renderer.render(bundle, { width: 64, height: 64 }, {
      nowMS: frame * 17,
      displayDeltaMS: frame === 0 ? 0 : 17,
      active: true,
      qualityEnabled: true,
      qualityRevision: 1,
      qualityProfile: survival,
    });
    const surfacePipeline = harness.fake.state.renderPipelines.find(
      (p) => p.desc && p.desc.label && String(p.desc.label).indexOf("WaterSurface-") >= 0 && String(p.desc.label).indexOf("WaterSurfaceBelow") < 0,
    );
    assert.ok(surfacePipeline, "expected the Selena surface pipeline by frame " + frame);
    const surfaceBGL = surfacePipeline.desc.layout.desc.bindGroupLayouts[0];
    const surfaceBindGroup = harness.fake.state.bindGroups.slice().reverse().find((bg) => bg.desc && bg.desc.layout === surfaceBGL);
    assert.ok(surfaceBindGroup, "expected the Selena surface bind group by frame " + frame);
    const surfaceFloats = waterSelenaLastUniformWrite(harness.fake, surfaceBindGroup);
    refractionMatrices.push(waterSelenaFieldFloats(waterSurfaceSelenaFixture.layout, surfaceFloats, "refractionMatrix", 16));
    reflectionMatrices.push(waterSelenaFieldFloats(waterSurfaceSelenaFixture.layout, surfaceFloats, "reflectionMatrix", 16));
    for (const pass of harness.fake.state.renderPasses.slice(start)) {
      const label = pass && pass.descriptor && pass.descriptor.label;
      if (label === "gosx-water-object-mesh-refraction-pass" || label === "gosx-water-object-mesh-clipped-reflection-pass") {
        labels.push(label);
        labelFrames.push(frame);
      }
    }
  }

  assert.deepEqual(labels, [
    "gosx-water-object-mesh-refraction-pass",
    "gosx-water-object-mesh-clipped-reflection-pass",
    "gosx-water-object-mesh-refraction-pass",
  ], "survival cadence=3 must retain only one duck RTT every third eligible frame while preserving target alternation");
  assert.deepEqual(labelFrames, [1, 4, 7], "RTT cadence must be independent from the every-frame moving-camera signature");
  assert.deepEqual(refractionMatrices[2], refractionMatrices[1], "a skipped frame must retain the matrix paired with the refraction texture");
  assert.deepEqual(refractionMatrices[4], refractionMatrices[1], "rendering clipped reflection must not overwrite the retained refraction matrix");
  assert.deepEqual(reflectionMatrices[5], reflectionMatrices[4], "a skipped frame must retain the matrix paired with the clipped-reflection texture");
  assert.deepEqual(reflectionMatrices[6], reflectionMatrices[4], "all cadence skips must preserve the retained clipped-reflection matrix");
});

test("Scene3D WebGPU submits only the water surface side facing the camera", async () => {
  const harness = await createBoardWebGPUHarness({ fresh: true, verboseTelemetry: false });
  const api = harness.env.context.__gosx_scene3d_api;
  const { state, objects } = waterPerfShapeScene(api, true, 192);
  state.waterSystems[0].seedDrops = 0;
  harness.canvas.width = 64;
  harness.canvas.height = 64;

  function renderAtCameraY(cameraY, nowMS) {
    const bundle = api.createSceneRenderBundle(
      64, 64, "#000000", { x: 0, y: cameraY, z: 4, fov: 60, near: 0.05, far: 128 },
      objects, [], [], [], [], {}, nowMS / 1000, [], [], [], state.waterSystems, [], 0, false,
    );
    harness.renderer.render(bundle, { width: 64, height: 64 }, { nowMS, active: true });
    return harness.mount.__gosxScene3DWebGPUStats;
  }

  const above = renderAtCameraY(1, 0);
  assert.equal(above.waterSurfaceAboveDrawCalls, 1);
  assert.equal(above.waterSurfaceBelowDrawCalls, 0);
  assert.equal(above.waterDrawVertices, above.waterVertices, "above-water cameras must submit the grid once");

  const below = renderAtCameraY(-1, 17);
  assert.equal(below.waterSurfaceAboveDrawCalls, 0);
  assert.equal(below.waterSurfaceBelowDrawCalls, 1);
  assert.equal(below.waterDrawVertices, below.waterVertices, "below-water cameras must submit the grid once");
});

test("Scene3D fake WebGPU water executes fixed ticks, normals, and queued events exactly", async () => {
  const harness = await createBoardWebGPUHarness({ fresh: true, verboseTelemetry: false });
  const api = harness.env.context.__gosx_scene3d_api;
  const { state, objects } = waterPerfShapeScene(api, false, 192);
  const entry = state.waterSystems[0];
  entry.seedDrops = 0;
  entry.activeObject = "None";
  entry.objectKind = "none";
  entry.objectDisplacementScale = 0;
  harness.canvas.width = 64;
  harness.canvas.height = 64;

  function renderAt(nowMS) {
    const bundle = api.createSceneRenderBundle(
      64, 64, "#000000",
      { x: 0, y: 0, z: 4, fov: 60, near: 0.05, far: 128 },
      objects, [], [], [], [], {}, nowMS / 1000, [], [], [], state.waterSystems, [], 0, false,
    );
    harness.renderer.render(bundle, { width: 64, height: 64 }, {
      nowMS,
      displayDeltaMS: 0,
      active: true,
      qualityTier: "balanced",
      qualityRevision: 0,
    });
    return harness.mount.__gosxScene3DWebGPUStats;
  }

  let stats = renderAt(0);
  assert.equal(stats.waterSimulationTicks, 0, "first frame only anchors the clock");
  assert.equal(stats.waterSurfaceResolution, 192, "adaptive-disabled frame metadata must preserve authored WebGPU topology");
  assert.equal(stats.waterSolverSubsteps, 0);
  assert.equal(stats.waterNormalDispatches, 0, "zero ticks must not recompute normals");
  assert.equal(stats.waterSampledStateCopies, 1, "first frame must initialize the sampled Selena state mirror");
  assert.equal(stats.waterSampledStateSyncSeq, 1);
  assert.equal(harness.fake.state.copyBufferToTextureCalls.length, 1);

  stats = renderAt(8);
  assert.equal(stats.waterSimulationTicks, 0, "120Hz display-only frame must not advance 60Hz simulation");
  assert.equal(stats.waterNormalDispatches, 0);
  assert.equal(stats.waterSampledStateCopies, 0, "display-only frames must reuse the sampled state texture");

  stats = renderAt(17);
  assert.equal(stats.waterSimulationTicks, 1);
  assert.equal(stats.waterSolverSubsteps, 2, "one fixed tick must execute two solver substeps");
  assert.equal(stats.waterNormalDispatches, 1, "N ticks must batch into one normal recompute");
  assert.equal(stats.waterSampledStateCopies, 1, "a simulation tick must refresh sampled state exactly once after normals");

  stats = renderAt(51);
  assert.equal(stats.waterSimulationCatchUpCap, 1);
  assert.equal(stats.waterSimulationTicks, 1);
  assert.equal(stats.waterSolverSubsteps, 2, "a slow frame must remain bounded to two solver substeps");
  assert.equal(stats.waterDroppedTicksThisFrame, 1, "elapsed excess must remain observable instead of becoming more GPU work");
  assert.equal(stats.waterNormalDispatches, 1, "a slow frame still recomputes normals once");

  entry.paused = true;
  entry.dropEventID = 7;
  entry.dropX = 0.25;
  entry.dropZ = -0.2;
  entry.objectDisplacementEvents = [{
    id: 9,
    activeObject: "Sphere",
    objectKind: "sphere",
    objectX: 0.1,
    objectY: -0.5,
    objectZ: 0.2,
    objectPreviousSet: true,
    objectPreviousX: 0,
    objectPreviousY: -0.5,
    objectPreviousZ: 0.2,
    objectRadius: 0.2,
    objectDisplacementScale: 1,
  }];
  harness.renderer.setLifecycle({ nowMS: 60, active: true, paused: true });
  stats = renderAt(1000);
  assert.equal(stats.waterLastDropEventID, 0, "paused drop ID must remain unconsumed");
  assert.equal(stats.waterLastObjectDisplacementEventID, 0, "paused object event ID must remain unconsumed");
  assert.equal(stats.waterSimulationTicks, 0);
  assert.equal(stats.waterNormalDispatches, 0);

  entry.paused = false;
  harness.renderer.setLifecycle({ nowMS: 1100, active: true, paused: false });
  stats = renderAt(1100);
  assert.equal(stats.waterSimulationTicks, 0, "resume frame must anchor without catch-up");
  assert.equal(stats.waterLastDropEventID, 0, "resume anchor must not consume queued drops without a tick");
  assert.equal(stats.waterLastObjectDisplacementEventID, 0, "resume anchor must not consume queued object events without a tick");
  stats = renderAt(1117);
  assert.equal(stats.waterSimulationTicks, 1, "first post-resume fixed tick must drain queued events");
  assert.equal(stats.waterLastDropEventID, 7, "queued drop must process once after resume");
  assert.equal(stats.waterLastObjectDisplacementEventID, 9, "queued object event must process once after resume");
});

test("Scene3D fake WebGPU water drains an ENTIRE queued dropEvents burst in one tick, not just the latest (water-parity/p6 Fix 1)", async () => {
  const harness = await createBoardWebGPUHarness({ fresh: true, verboseTelemetry: false });
  const api = harness.env.context.__gosx_scene3d_api;
  const { state, objects } = waterPerfShapeScene(api, false, 192);
  const entry = state.waterSystems[0];
  entry.seedDrops = 0;
  entry.activeObject = "None";
  entry.objectKind = "none";
  entry.objectDisplacementScale = 0;
  harness.canvas.width = 64;
  harness.canvas.height = 64;

  function renderAt(nowMS) {
    const bundle = api.createSceneRenderBundle(
      64, 64, "#000000",
      { x: 0, y: 0, z: 4, fov: 60, near: 0.05, far: 128 },
      objects, [], [], [], [], {}, nowMS / 1000, [], [], [], state.waterSystems, [], 0, false,
    );
    harness.renderer.render(bundle, { width: 64, height: 64 }, {
      nowMS, displayDeltaMS: 0, active: true, qualityTier: "balanced", qualityRevision: 0,
    });
    return harness.mount.__gosxScene3DWebGPUStats;
  }

  renderAt(0); // anchor the fixed clock

  // Simulate a fast drag: 5 drops queued between two rendered frames, the
  // shape sceneManagedFluidObjectQueueDrop's bounded controlState.dropEvents
  // array produces (19b-scene-control-forms.js). Before Fix 1 this would
  // have coalesced to a single scalar dropEventID/dropX/dropZ and only the
  // LAST drop would ever reach the simulation.
  entry.dropEvents = [
    { id: 1, x: -0.6, z: -0.6, radius: 0.03, strength: 0.01 },
    { id: 2, x: -0.3, z: -0.3, radius: 0.03, strength: 0.01 },
    { id: 3, x: 0.0, z: 0.0, radius: 0.03, strength: 0.01 },
    { id: 4, x: 0.3, z: 0.3, radius: 0.03, strength: 0.01 },
    { id: 5, x: 0.6, z: 0.6, radius: 0.03, strength: 0.01 },
  ];
  const stats = renderAt(17); // one 60Hz tick past the anchor
  assert.equal(stats.waterSimulationTicks, 1, "must actually tick to drain events");
  assert.equal(stats.waterLastDropEventID, 5, "every queued id must be consumed, not just the first");
  assert.equal(stats.waterDropDispatches, 5, "every one of the 5 queued drops must get its own compute dispatch this frame");

  // A second frame with no new events must be a no-op: already-consumed ids
  // must not redispatch (the array is re-supplied unchanged every command,
  // per normalizeSceneWaterOneShotEvents's fallback -- see
  // 10-runtime-scene-core.js).
  const stats2 = renderAt(34);
  assert.equal(stats2.waterDropDispatches, 0, "already-consumed ids must not redispatch");
  assert.equal(stats2.waterLastDropEventID, 5);
});

test("Scene3D fake WebGPU quality transitions preserve simulation buffers and authored caps", async () => {
  const harness = await createBoardWebGPUHarness({ fresh: true, verboseTelemetry: false });
  const api = harness.env.context.__gosx_scene3d_api;
  const { state, objects } = waterPerfShapeScene(api, false, 192);
  const entry = state.waterSystems[0];
  entry.seedDrops = 0;
  entry.causticsResolution = 512;
  entry.objectShadowResolution = 512;
  entry.objectTextureResolution = 512;
  entry.objectTexturePixelBudget = 786432;
  harness.canvas.width = 64;
  harness.canvas.height = 64;
  function renderProfile(nowMS, revision, profile) {
    const bundle = api.createSceneRenderBundle(
      64, 64, "#000000",
      { x: 0, y: 0, z: 4, fov: 60, near: 0.05, far: 128 },
      objects, [], [], [], [], {}, nowMS / 1000, [], [], [], state.waterSystems, [], 0, false,
    );
    harness.renderer.render(bundle, { width: 64, height: 64 }, { nowMS, active: true, qualityEnabled: true, qualityRevision: revision, qualityProfile: profile });
    return harness.mount.__gosxScene3DWebGPUStats;
  }
  const balanced = { tier: "balanced", dprCap: 1.25, surfaceResolution: 128, causticsResolution: 384, objectShadowResolution: 384, objectTextureMaxSide: 384, objectTexturePixelBudget: 442368, expensivePassCadence: 2 };
  let stats = renderProfile(0, 1, balanced);
  assert.equal(stats.waterQualityTier, "balanced");
  assert.equal(stats.waterSurfaceResolution, 128);
  assert.equal(stats.waterActiveCausticsResolution, 384);
  assert.equal(stats.waterActiveObjectShadowResolution, 384);
  const simulationBuffers = harness.fake.state.buffers.filter((buffer) => buffer.size === 192 * 192 * 16);
  assert.equal(simulationBuffers.length, 2);

  const survival = { tier: "survival", dprCap: 1, surfaceResolution: 96, causticsResolution: 256, objectShadowResolution: 256, objectTextureMaxSide: 256, objectTexturePixelBudget: 196608, expensivePassCadence: 3 };
  stats = renderProfile(17, 2, survival);
  assert.equal(stats.waterQualityTier, "survival");
  assert.equal(stats.waterSurfaceResolution, 96);
  assert.equal(stats.waterActiveCausticsResolution, 256);
  assert.equal(stats.waterActiveObjectShadowResolution, 256);
  assert.equal(harness.fake.state.buffers.filter((buffer) => buffer.size === 192 * 192 * 16).length, 2, "profile transition must not allocate replacement simulation buffers");
  assert.ok(simulationBuffers.every((buffer) => !buffer.destroyed), "profile transition must preserve live simulation buffers");
  assert.equal(stats.waterSimulationTickSeq, 1, "profile transition must advance the existing fixed clock without reseeding");

  const oversized = { tier: "full", dprCap: 2, surfaceResolution: 320, causticsResolution: 1024, objectShadowResolution: 1024, objectTextureMaxSide: 1024, objectTexturePixelBudget: 3_145_728, expensivePassCadence: 1 };
  stats = renderProfile(34, 3, oversized);
  assert.equal(stats.waterSurfaceResolution, 192, "surface cap cannot exceed authored simulation topology");
  assert.equal(stats.waterActiveCausticsResolution, 512, "full profile cannot exceed authored caustics cap");
  assert.equal(stats.waterActiveObjectShadowResolution, 512, "full profile cannot exceed authored shadow cap");
  assert.ok(simulationBuffers.every((buffer) => !buffer.destroyed));
  assert.equal(harness.renderer.pollPerformanceSample(), null, "timestamp-unavailable devices must fall back safely without blocking");
  assert.equal(harness.mount.getAttribute("data-gosx-scene3d-webgpu-gpu-timing"), "timer-unavailable");
});

test("Scene3D fake WebGPU ignores supplied quality profiles when adaptive quality is disabled", async () => {
  const harness = await createBoardWebGPUHarness({ fresh: true, verboseTelemetry: false });
  const api = harness.env.context.__gosx_scene3d_api;
  const { state, objects } = waterPerfShapeScene(api, false, 192);
  const entry = state.waterSystems[0];
  entry.seedDrops = 0;
  entry.causticsResolution = 512;
  entry.objectShadowResolution = 512;
  harness.canvas.width = 64;
  harness.canvas.height = 64;
  const bundle = api.createSceneRenderBundle(
    64, 64, "#000000", { x: 0, y: 0, z: 4, fov: 60, near: 0.05, far: 128 },
    objects, [], [], [], [], {}, 0, [], [], [], state.waterSystems, [], 0, false,
  );
  harness.renderer.render(bundle, { width: 64, height: 64 }, {
    nowMS: 0,
    active: true,
    qualityEnabled: false,
    qualityRevision: 99,
    qualityProfile: {
      tier: "survival", surfaceResolution: 96, causticsResolution: 256,
      objectShadowResolution: 256, objectTextureMaxSide: 256,
      objectTexturePixelBudget: 196608, expensivePassCadence: 3,
    },
  });
  const stats = harness.mount.__gosxScene3DWebGPUStats;
  assert.equal(stats.waterQualityTier, "full");
  assert.equal(stats.waterQualityRevision, 0);
  assert.equal(stats.waterSurfaceResolution, 192);
  assert.equal(stats.waterActiveCausticsResolution, 512);
  assert.equal(stats.waterActiveObjectShadowResolution, 512);
});

test("Scene3D fake WebGPU timestamp ring is supported and nonblocking", async () => {
  const harness = await createBoardWebGPUHarness({ fresh: true, verboseTelemetry: false, fakeDeviceOptions: { timestampQuery: true } });
  const api = harness.env.context.__gosx_scene3d_api;
  const { state, objects } = waterPerfShapeScene(api, false, 192);
  state.waterSystems[0].seedDrops = 0;
  harness.canvas.width = 64;
  harness.canvas.height = 64;
  for (let frame = 0; frame < 3; frame++) {
    const bundle = api.createSceneRenderBundle(
      64, 64, "#000000", { x: 0, y: 0, z: 4, fov: 60, near: 0.05, far: 128 },
      objects, [], [], [], [], {}, frame * 0.017, [], [], [], state.waterSystems, [], 0, false,
    );
    harness.renderer.render(bundle, { width: 64, height: 64 }, { nowMS: frame * 17, active: true });
    if (frame < 2) assert.equal(harness.renderer.pollPerformanceSample(), null, "timestamp polling must not wait for unresolved GPU work");
  }
  await flushAsyncWork();
  const status = harness.renderer.getPerformanceTimingStatus();
  assert.equal(status.available, true);
  assert.equal(status.active, true);
  const sample = harness.renderer.pollPerformanceSample();
  assert.equal(sample.source, "gpu-timestamp");
  assert.equal(sample.gpuMS, 4);
  assert.equal(harness.mount.getAttribute("data-gosx-scene3d-webgpu-gpu-timing"), "measured");
  assert.equal(harness.mount.getAttribute("data-gosx-scene3d-webgpu-gpu-ms"), "4.000");
});

test("Scene3D WebGPU advertised timestamp queries fall back when encoder timestamps are absent", async () => {
  const harness = await createBoardWebGPUHarness({
    fresh: true,
    verboseTelemetry: false,
    fakeDeviceOptions: { timestampQuery: true, timestampEncoder: false },
  });
  const api = harness.env.context.__gosx_scene3d_api;
  const { state, objects } = waterPerfShapeScene(api, true, 192);
  const bundle = api.createSceneRenderBundle(
    64, 64, "#000000", { x: 0, y: 1, z: 4, fov: 60, near: 0.05, far: 128 },
    objects, [], [], [], [], {}, 0, [], [], [], state.waterSystems, [], 0, false,
  );
  harness.renderer.render(bundle, { width: 64, height: 64 }, { nowMS: 0, active: true });
  assert.deepEqual(
    JSON.parse(JSON.stringify(harness.renderer.getPerformanceTimingStatus())),
    { available: false, active: false, pending: false, failed: false, source: "gpu-timestamp" },
  );
  assert.equal(harness.mount.getAttribute("data-gosx-scene3d-webgpu-gpu-timing"), "timer-unavailable");
  assert.equal(harness.renderer.pollPerformanceSample(), null);
});

test("Scene3D fake WebGPU timing partial allocation failure destroys candidates and exposes CPU fallback", async () => {
  const harness = await createBoardWebGPUHarness({
    fresh: true,
    verboseTelemetry: false,
    fakeDeviceOptions: { timestampQuery: true },
  });
  const device = harness.fake.device;
  const originalCreateBuffer = device.createBuffer.bind(device);
  const querySet = { destroyed: false, destroy() { this.destroyed = true; } };
  device.createQuerySet = function() { return querySet; };
  const timingBuffers = [];
  device.createBuffer = function(desc) {
    const isTimingBuffer = desc && desc.size === 16 && (desc.usage === (0x200 | 0x4) || desc.usage === (0x8 | 0x1));
    if (!isTimingBuffer) return originalCreateBuffer(desc);
    if (timingBuffers.length === 1) throw new Error("injected timestamp readback allocation failure");
    const buffer = originalCreateBuffer(desc);
    timingBuffers.push(buffer);
    return buffer;
  };

  const api = harness.env.context.__gosx_scene3d_api;
  const { state, objects } = waterPerfShapeScene(api, false, 192);
  const bundle = api.createSceneRenderBundle(
    64, 64, "#000000", { x: 0, y: 0, z: 4, fov: 60, near: 0.05, far: 128 },
    objects, [], [], [], [], {}, 0, [], [], [], state.waterSystems, [], 0, false,
  );
  assert.doesNotThrow(() => harness.renderer.render(bundle, { width: 64, height: 64 }, { nowMS: 0, active: true }));
  assert.equal(querySet.destroyed, true, "partially-created query set must be destroyed");
  assert.equal(timingBuffers.length, 1);
  assert.equal(timingBuffers[0].destroyed, true, "partially-created timing buffer must be destroyed");
  assert.deepEqual(
    JSON.parse(JSON.stringify(harness.renderer.getPerformanceTimingStatus())),
    { available: false, active: false, pending: false, failed: true, source: "gpu-timestamp" },
  );
  assert.equal(harness.renderer.pollPerformanceSample(), null, "failed GPU timing must fall back without blocking");
  assert.equal(harness.mount.getAttribute("data-gosx-scene3d-webgpu-gpu-timing"), "failed");
});

test("Scene3D WebGL water binds the full Selena object contract", () => {
  const source = fs.readFileSync(path.join(__dirname, "bootstrap-src", "16-scene-webgl.js"), "utf8");
  assert.match(source, /mvp: mvp, modelMatrix: identity4, normalMatrix: identity3/,
    "direct analytic objects must receive an identity model matrix when their vertices are already world-baked");
  assert.match(source, /name: "causticTexture", target: gl\.TEXTURE_2D, tex: causticTex/,
    "direct and projected objects must sample the live caustic target rather than aliasing the state texture");
  assert.match(source, /poolWidth: livePoolWidth, poolLength: livePoolLength, poolHeight: livePoolHeight/,
    "object shading must receive live pool dimensions for refraction and caustic projection");
  assert.match(source, /name: "objectShadowTexture", target: gl\.TEXTURE_2D, tex: shadowTex/,
    "the caustics pass must bind objectShadowTexture to the live shadow RTT, not leave it aliased to texture unit 0 (the state texture)");
  assert.match(source, /causticIntensity: sceneWaterNum\(liveEntry\.causticIntensity, 0\.2\)/,
    "the caustics pass must explicitly upload causticIntensity (compiled default 0.2) -- WebGL2's uniform application has no automatic default fallback the way WebGPU's generic Selena resolver does, so omitting it here zeros the entire caustic pattern");
  assert.match(source, /objectShadowTexelSize: 1 \/ Math\.max\(1, shadowTarget \? shadowTarget\.size : authoredShadowSize\)/,
    "the caustics pass must forward a live, nonzero objectShadowTexelSize for its soft-shadow tap sampling");
});

test("Scene3D fake WebGL water executes fixed ticks, normals, and queued events exactly", () => {
  const env = createContext({ enableWebGL2: true, disableCanvas2D: true });
  env.context.WebGL2RenderingContext = FakeWebGLContext;
  runScript(bootstrapRuntimeSource, env.context, "bootstrap-runtime.js");
  runScript(freshFeatureBundleSource("scene3d"), env.context, "bootstrap-feature-scene3d.js");
  // The WebGL2 water runtime moved into the lazily fetched WebGL chunk. Load
  // the chunk the way ensureWebGLFeatureLoaded does, then read the factory the
  // chunk publishes.
  runScript(
    freshFeatureBundleSource("scene3d-webgl", { exportWaterRendererForTest: true }),
    env.context,
    "bootstrap-feature-scene3d-webgl.js",
  );
  const createWaterRenderer = env.context.__gosx_test_create_water_webgl;
  assert.equal(typeof createWaterRenderer, "function");

  const canvas = env.document.createElement("canvas");
  canvas.width = 64;
  canvas.height = 64;
  const gl = new FakeWebGLContext();
  gl.HALF_FLOAT = 0x140B;
  gl.FRAMEBUFFER_COMPLETE = 0x8CD5;
  gl.QUERY_RESULT_AVAILABLE = 0x8867;
  gl.QUERY_RESULT = 0x8866;
  gl.checkFramebufferStatus = function() { return gl.FRAMEBUFFER_COMPLETE; };
  gl.drawElements = function(mode, count, type, offset) { gl.ops.push(["drawElements", mode, count, type, offset]); };
  const timerExt = { TIME_ELAPSED_EXT: 0x88BF, GPU_DISJOINT_EXT: 0x8FBB };
  let timerAvailable = false;
  let timerDisjoint = false;
  let timerSeq = 0;
  gl.createQuery = function() { return { id: ++timerSeq }; };
  gl.deleteQuery = function(query) { gl.ops.push(["deleteQuery", query && query.id]); };
  gl.beginQuery = function(target, query) { gl.ops.push(["beginQuery", target, query.id]); };
  gl.endQuery = function(target) { gl.ops.push(["endQuery", target]); };
  gl.getQueryParameter = function(_query, param) {
    if (param === gl.QUERY_RESULT_AVAILABLE) return timerAvailable;
    if (param === gl.QUERY_RESULT) return 4_000_000;
    return 0;
  };
  const originalGetParameter = gl.getParameter.bind(gl);
  gl.getParameter = function(param) { return param === timerExt.GPU_DISJOINT_EXT ? timerDisjoint : originalGetParameter(param); };
  const originalGetExtension = gl.getExtension.bind(gl);
  gl.getExtension = function(name) {
    if (name === "EXT_color_buffer_float" || name === "EXT_color_buffer_half_float" || name === "OES_texture_float_linear") return {};
    if (name === "EXT_disjoint_timer_query_webgl2") return timerExt;
    return originalGetExtension(name);
  };
  canvas._webglContext = gl;

  const shader = "void main() {}";
  const entry = {
    id: "water-main",
    resolution: 192,
    seedDrops: 0,
    paused: false,
    activeObject: "None",
    objectKind: "none",
    simulationVertexGLES: shader,
    simulationFragmentGLES: shader,
    normalVertexGLES: shader,
    normalFragmentGLES: shader,
    dropVertexGLES: shader,
    dropFragmentGLES: shader,
    displacementVertexGLES: shader,
    displacementFragmentGLES: shader,
    poolVertexGLES: shader,
    poolFragmentGLES: shader,
    surfaceVertexGLES: shader,
    surfaceFragmentGLES: shader,
    causticsVertexGLES: shader,
    causticsFragmentGLES: shader,
    objectShadowVertexGLES: shader,
    objectShadowFragmentGLES: shader,
    causticsResolution: 512,
    objectShadowResolution: 512,
    objectTextureResolution: 512,
    objectTexturePixelBudget: 786432,
    // caustics uses the REAL Selena-compiled layout (not a bare stub) so its
    // uniformBlock.defaults (causticIntensity=0.2) and context fields
    // (objectShadowTexelSize) are present, exercising the same
    // sceneWaterRenderSetUniforms path the causticIntensity/
    // objectShadowTexture regression assertions below depend on.
    shaderDescriptors: { normal: waterNormalSelenaFixture.layout, caustics: waterCausticsSelenaFixture.layout },
  };
  const renderer = createWaterRenderer(gl, canvas, entry);
  assert.ok(renderer, "fake WebGL water renderer must initialize");
  const bundle = { waterSystems: [entry], camera: { x: 0, y: 0, z: 4, fov: 60, near: 0.05, far: 128 } };
  const viewport = { width: 64, height: 64 };

  renderer.render(bundle, viewport, { nowMS: 0, active: true });
  let stats = renderer.getStats();
  assert.equal(stats.waterSimulationTicksLastFrame, 0);
  assert.equal(stats.waterQualityAdaptiveEnabled, false, "missing qualityEnabled must preserve authored WebGL resources");
  assert.equal(stats.waterSimulationPasses, 0);
  assert.equal(stats.waterNormalPasses, 0);

  renderer.render(bundle, viewport, { nowMS: 8, active: true });
  stats = renderer.getStats();
  assert.equal(stats.waterSimulationPasses, 0, "120Hz display-only frame must not run WebGL simulation");
  assert.equal(stats.waterNormalPasses, 0);

  renderer.render(bundle, viewport, { nowMS: 17, active: true });
  stats = renderer.getStats();
  assert.equal(stats.waterSimulationTicksLastFrame, 1);
  assert.equal(stats.waterSimulationPasses, 2);
  assert.equal(stats.waterNormalPasses, 1);
  assert.ok(gl.ops.some((op) => op[0] === "uniform1f" && op[1] === "cellSizeX" && Math.abs(op[2] - 2 / 192) < 1e-7), "WebGL normal pass must receive physical X cell spacing");
  assert.ok(gl.ops.some((op) => op[0] === "uniform1f" && op[1] === "cellSizeZ" && Math.abs(op[2] - 2 / 192) < 1e-7), "WebGL normal pass must receive physical Z cell spacing");
  // P1.5 regression: causticIntensity (a `param` with a compiled default of
  // 0.2 in caustics.sel) has no automatic default-fallback in WebGL2's
  // hand-rolled sceneWaterRenderSetUniforms (unlike WebGPU's generic Selena
  // uniform resolver). Leaving it unset zeroed the caustic pattern
  // (areaFocus = oldArea/newArea*causticIntensity) every frame, crushing the
  // "wet" diffuse term surface.sel adds for every submerged surface -- the
  // root cause of the washed-out WebGL water this milestone fixes. Assert
  // the WebGL caustics pass explicitly uploads it (and the objectShadowTexelSize
  // context field + objectShadowTexture sampler caustics.sel's soft-shadow
  // read needs) so this cannot silently regress to zero again.
  assert.ok(gl.ops.some((op) => op[0] === "uniform1f" && op[1] === "causticIntensity" && Math.abs(op[2] - 0.2) < 1e-6), "WebGL caustics pass must upload the compiled causticIntensity default, not leave it at the GL zero default");
  assert.ok(gl.ops.some((op) => op[0] === "uniform1f" && op[1] === "objectShadowTexelSize" && op[2] > 0), "WebGL caustics pass must upload a nonzero objectShadowTexelSize");
  assert.ok(gl.ops.some((op) => op[0] === "uniform1i" && op[1] === "objectShadowTexture"), "WebGL caustics pass must bind the objectShadowTexture sampler instead of leaving it aliased to unit 0");

  renderer.render(bundle, viewport, { nowMS: 51, active: true });
  stats = renderer.getStats();
  assert.equal(stats.waterSimulationCatchUpCap, 1);
  assert.equal(stats.waterSimulationTicksLastFrame, 1);
  assert.equal(stats.waterDroppedSimulationTicksLastFrame, 1);
  assert.equal(stats.waterSimulationPasses, 4, "a slow frame remains bounded to two solver passes");
  assert.equal(stats.waterNormalPasses, 2, "a slow frame adds one normal pass");

  entry.paused = true;
  entry.dropEventID = 7;
  entry.objectDisplacementEvents = [{ id: 9, objectKind: "sphere", objectRadius: 0.2 }];
  renderer.setLifecycle({ nowMS: 60, active: true, paused: true });
  renderer.render(bundle, viewport, { nowMS: 1000, active: true });
  stats = renderer.getStats();
  assert.equal(stats.waterSimulationPasses, 4);
  assert.equal(stats.waterNormalPasses, 2);
  assert.equal(stats.waterLastDropEventID, 0, "paused WebGL drop ID must remain unconsumed");
  assert.equal(stats.waterLastObjectDisplacementEventID, 0, "paused WebGL object event ID must remain unconsumed");

  entry.paused = false;
  renderer.setLifecycle({ nowMS: 1100, active: true, paused: false });
  renderer.render(bundle, viewport, { nowMS: 1100, active: true });
  stats = renderer.getStats();
  assert.equal(stats.waterSimulationTicksLastFrame, 0, "resume frame must anchor without catch-up");
  assert.equal(stats.waterLastDropEventID, 0, "resume anchor must not consume queued drops without a tick");
  assert.equal(stats.waterLastObjectDisplacementEventID, 0, "resume anchor must not consume queued object events without a tick");
  renderer.render(bundle, viewport, { nowMS: 1117, active: true });
  stats = renderer.getStats();
  assert.equal(stats.waterSimulationTicksLastFrame, 1, "first post-resume fixed tick must drain queued events");
  assert.equal(stats.waterNormalDirty, false, "queued events and the fixed step must finish with fresh normals");
  assert.equal(stats.waterLastDropEventID, 7);
  assert.equal(stats.waterLastObjectDisplacementEventID, 9);

  assert.equal(renderer.pollPerformanceSample(), null, "timer polling must be nonblocking while the query is unavailable");
  timerAvailable = true;
  const timerSample = renderer.pollPerformanceSample();
  assert.equal(timerSample.source, "webgl-timer");
  assert.equal(timerSample.gpuMS, 4);
  timerDisjoint = true;
  assert.equal(renderer.pollPerformanceSample(), null, "disjoint timing must be discarded and reset safely");
  timerDisjoint = false;

  renderer.render(bundle, viewport, {
    nowMS: 1117, active: true, qualityEnabled: true, qualityRevision: 1,
    qualityProfile: { tier: "balanced", dprCap: 1.25, surfaceResolution: 128, causticsResolution: 384, objectShadowResolution: 384, objectTextureMaxSide: 384, objectTexturePixelBudget: 442368, expensivePassCadence: 2 },
  });
  stats = renderer.getStats();
  assert.equal(stats.waterQualityTier, "balanced");
  assert.equal(stats.waterSurfaceGridResolution, 128);
  assert.equal(stats.waterCausticsResolution, 384);
  assert.equal(stats.waterObjectShadowResolution, 384);
  assert.equal(stats.waterSimulationResolution, 192);
  const simulationTickSeq = stats.waterSimulationTickSeq;

  renderer.render(bundle, viewport, {
    nowMS: 1134, active: true, qualityEnabled: true, qualityRevision: 2,
    qualityProfile: { tier: "survival", dprCap: 1, surfaceResolution: 96, causticsResolution: 256, objectShadowResolution: 256, objectTextureMaxSide: 256, objectTexturePixelBudget: 196608, expensivePassCadence: 3 },
  });
  stats = renderer.getStats();
  assert.equal(stats.waterQualityTier, "survival");
  assert.equal(stats.waterSurfaceGridResolution, 96);
  assert.equal(stats.waterCausticsResolution, 256);
  assert.equal(stats.waterObjectShadowResolution, 256);
  assert.equal(stats.waterSimulationResolution, 192, "quality transitions must preserve authored simulation topology");
  assert.ok(stats.waterSimulationTickSeq > simulationTickSeq, "quality transition must preserve and advance the existing clock");

  renderer.render(bundle, viewport, {
    nowMS: 1151, active: true, qualityEnabled: true, qualityRevision: 3,
    qualityProfile: { tier: "full", dprCap: 2, surfaceResolution: 320, causticsResolution: 1024, objectShadowResolution: 1024, objectTextureMaxSide: 1024, objectTexturePixelBudget: 3_145_728, expensivePassCadence: 1 },
  });
  stats = renderer.getStats();
  assert.equal(stats.waterSurfaceGridResolution, 192, "full profile must remain bounded by the authored surface topology");
  assert.equal(stats.waterCausticsResolution, 512, "full profile must not exceed authored caustics resolution");
  assert.equal(stats.waterObjectShadowResolution, 512, "full profile must not exceed authored shadow resolution");

  // water-parity/p6 Fix 1: a fast drag queues MULTIPLE drops between two
  // rendered frames (entry.dropEvents, see sceneManagedFluidObjectQueueDrop
  // in 19b-scene-control-forms.js) -- the WebGL queueWaterEvents/
  // drainWaterEvents Map-based drain (16-scene-webgl.js) must consume every
  // one of them in the same tick, not just entry.dropEventID's scalar
  // latest. Appended at the end (a fresh, later nowMS not reused anywhere
  // above) rather than interleaved earlier, so it can't perturb this test's
  // existing delta-from-previous-nowMS tick-timing assertions.
  entry.dropEvents = [
    { id: 8, x: -0.5, z: -0.5, radius: 0.03, strength: 0.01 },
    { id: 9, x: -0.25, z: -0.25, radius: 0.03, strength: 0.01 },
    { id: 10, x: 0, z: 0, radius: 0.03, strength: 0.01 },
    { id: 11, x: 0.25, z: 0.25, radius: 0.03, strength: 0.01 },
    { id: 12, x: 0.5, z: 0.5, radius: 0.03, strength: 0.01 },
  ];
  renderer.render(bundle, viewport, { nowMS: 1168, active: true });
  stats = renderer.getStats();
  assert.equal(stats.waterLastDropEventID, 12, "every queued drop id must be consumed, not just the first (8) or none");
  assert.equal(stats.waterDropEventsPending, 0, "the whole burst must drain in one tick, leaving nothing pending");

  renderer.dispose();
});

test("[perf-shape] Scene3D WebGPU water: steady-state per-frame device calls stay flat for Sphere and Rubber Duck alike", async () => {
  const FRAME_COUNT = 8;
  const [sphere, duck] = await Promise.all([
    renderWaterPerfShapeFrames(false, FRAME_COUNT),
    renderWaterPerfShapeFrames(true, FRAME_COUNT),
  ]);

  function assertSteadyState(label, deltas) {
    // Frame 0 is the warmup frame: pipelines/shader modules compile and the
    // bind-group/uniform-buffer pools fill for the first time here, so it is
    // EXPECTED to carry the compile + first-build cost. Frames 1..N-1 are
    // steady state -- every pipeline is memoized (getSelenaPipeline's
    // material-stamp + content-keyed Map) and every bind group / uniform
    // buffer should be served from the owner-keyed cache (createSelenaBindGroup's
    // pool, wgpuCachedTrackedBuffer), so these frames must show ZERO new
    // pipeline/shader-module compiles and a CONSTANT (not growing)
    // createBindGroup/writeBuffer count frame over frame.
    // The first active frame anchors the fixed clock, so simulation/normal
    // pipelines are first compiled on frame 1. Frames 2..N are steady state.
    for (let i = 2; i < deltas.length; i += 1) {
      const d = deltas[i];
      assert.equal(d.renderPipelines, 0, label + " frame " + i + ": createRenderPipeline must be 0 in steady state (got " + d.renderPipelines + ") -- a pipeline cache key is unstable");
      assert.equal(d.computePipelines, 0, label + " frame " + i + ": createComputePipeline must be 0 in steady state (got " + d.computePipelines + ")");
      assert.equal(d.shaderModules, 0, label + " frame " + i + ": createShaderModule must be 0 in steady state (got " + d.shaderModules + ")");
    }
    // Bind-group count must plateau: frame N's count must equal frame 1's
    // count exactly (not grow) once the pools are warm. A water ping-pong
    // state buffer legitimately alternates between 2 buffer identities, which
    // createSelenaBindGroup's pool already accommodates (capped at 4 entries),
    // so the STEADY count itself may be reached over 2 frames, but it must not
    // grow without bound over 5.
    const steadyBindGroups = deltas.slice(2).map((d) => d.bindGroups);
    const maxBindGroups = Math.max(...steadyBindGroups);
    const lastBindGroups = steadyBindGroups[steadyBindGroups.length - 1];
    assert.ok(
      lastBindGroups <= maxBindGroups,
      label + ": createBindGroup count must plateau, not keep growing -- per-frame deltas were " + JSON.stringify(steadyBindGroups),
    );
    // writeBuffer volume: once caches are warm, only per-frame UNIFORM values
    // (small: a few hundred bytes per material/pass) should be re-written --
    // never the combined-scene PBR mesh attribute buffer (positions/normals/
    // uvs/tangents; ensurePBRSceneAttributeBuffers) full-reuploaded every
    // frame, which is what a `bundle`-identity-keyed cache produces since
    // createSceneRenderBundle hands back a brand-new bundle object every
    // render() call (see wgpuStablePBRAttributeBuffer's comment). Every
    // water-demo float-* object is configured with zero drift/bob/spin (see
    // program.go), i.e. genuinely static once placed, so a steady frame's
    // writeBuffer volume must be a small fraction of the warmup frame's (which
    // legitimately re-derives + uploads the mesh once) -- not comparable to
    // it, which would mean the mesh soup is being re-uploaded whole every
    // single frame instead of once.
    const warmupBytes = deltas[0].writeBufferBytes;
    const steadyBytes = deltas.slice(2).map((d) => d.writeBufferBytes);
    for (let i = 0; i < steadyBytes.length; i += 1) {
      assert.ok(
        steadyBytes[i] < warmupBytes * 0.25,
        label + " frame " + (i + 1) + ": steady-state writeBuffer bytes (" + steadyBytes[i] + ") must be well below the warmup frame's (" + warmupBytes +
        ") -- a value this close to the warmup frame means the combined-scene PBR mesh vertex buffer is being fully re-uploaded every frame instead of once",
      );
    }
    return { steadyBindGroups, steadyBytes };
  }

  const sphereShape = assertSteadyState("Sphere", sphere.deltas);
  const duckShape = assertSteadyState("Rubber Duck", duck.deltas);

  // Surface the raw numbers (this is the actual diagnostic output the task
  // asks for -- sphere vs duck steady-state call counts).
  console.log(
    "[perf-shape] sphere deltas=" + JSON.stringify(sphere.deltas) +
    " duck deltas=" + JSON.stringify(duck.deltas),
  );

  // The duck is allowed to cost MORE per frame than the sphere (extra RTT
  // passes/mesh-shadow pass are real, intentional, intrinsic work) -- but
  // whatever its steady-state bind-group count is, it must be the SAME across
  // every steady frame, exactly like the sphere. Assert both scenarios'
  // steady-state bind-group counts are internally constant (already checked
  // above) and print the sphere/duck ratio so a regression that reintroduces
  // per-frame churn (ratio scaling with frame index instead of staying flat)
  // is visible even if an individual frame's absolute count wouldn't trip the
  // plateau check above.
  const sphereCached = sphereShape.steadyBindGroups.slice(-3);
  const duckCached = duckShape.steadyBindGroups.slice(-3);
  assert.ok(sphereCached.every((n) => n === sphereCached[0]), "sphere cached createBindGroup count must be identical every frame: " + JSON.stringify(sphereCached));
  assert.ok(duckCached.every((n) => n === duckCached[0]), "duck cached createBindGroup count must be identical every frame: " + JSON.stringify(duckCached));
  const sphereCachedPasses = sphere.deltas.slice(-3).map((d) => d.renderPasses);
  const duckCachedPasses = duck.deltas.slice(-3).map((d) => d.renderPasses);
  assert.deepEqual(duckCachedPasses, sphereCachedPasses,
    "stationary duck RTT and shadow targets must be reused after their three-slot refresh: duck=" + JSON.stringify(duckCachedPasses) + " sphere=" + JSON.stringify(sphereCachedPasses));
});

test("[perf-shape] Scene3D WebGPU balanced water skips stationary displacement and cadences retained prepasses", async () => {
  const { deltas } = await renderWaterPerfShapeFrames(false, 3, 192);
  // Frame 0 anchors the fixed clock and establishes the footprint. Each later
  // 60 Hz tick needs only two integration stages plus one normal pass; a
  // stationary displacement pass would make this four. P3 fusion
  // (water-parity-campaign) batches the two integration substeps plus the
  // trailing normal reconstruction into ONE compute pass (was 3 separate
  // passes), so the one-time seed dispatch (its own, unfused, pass) plus the
  // fused sim+normal pass is 2 passes on the seeding tick, and just the fused
  // pass (1) on every steady stationary tick after that.
  assert.equal(deltas[1].computePasses, 2,
    "first fixed tick should include the one-time authored seed pass plus one fused (2 integration substeps + normal) pass");
  assert.equal(deltas[2].computePasses, 1,
    "steady stationary ticks should drop to the single fused (2 integration substeps + normal) pass");
  // The fake sphere has no mesh RTT resources, so retained prepass work stays
  // absent and the render shape remains flat across the cadence boundary.
  assert.equal(deltas[2].renderPasses, deltas[1].renderPasses,
    "balanced retained-pass work should stay bounded: " + JSON.stringify(deltas));
});

test("[perf-shape] Scene3D WebGPU keeps exact frame proof while throttling broad DOM telemetry", async () => {
  let nowMS = 0;
  const harness = await createBoardWebGPUHarness({
    fresh: true,
    verboseTelemetry: false,
    performanceNow: () => nowMS,
  });
  const api = harness.env.context.__gosx_scene3d_api;
  const { state, objects } = waterPerfShapeScene(api, false, 192);
  harness.canvas.width = 64;
  harness.canvas.height = 64;

  let attributeWrites = 0;
  const originalSetAttribute = harness.mount.setAttribute.bind(harness.mount);
  harness.mount.setAttribute = function(name, value) {
    attributeWrites += 1;
    return originalSetAttribute(name, value);
  };
  function renderFrame(time) {
    const bundle = api.createSceneRenderBundle(
      64, 64, "#000000",
      { x: 0, y: 0, z: 4, fov: 60, near: 0.05, far: 128 },
      objects, [], [], [], [], {}, time, [], [], [], state.waterSystems, [], 0, false,
    );
    harness.renderer.render(bundle, { width: 64, height: 64 });
  }

  renderFrame(0);
  const firstFrameWrites = attributeWrites;
  assert.ok(firstFrameWrites > 50, "first frame should publish the full diagnostic snapshot, got " + firstFrameWrites);
  nowMS = 16;
  renderFrame(0.016);
  const secondFrameWrites = attributeWrites - firstFrameWrites;
  nowMS = 32;
  renderFrame(0.032);
  const thirdFrameWrites = attributeWrites - firstFrameWrites - secondFrameWrites;
  assert.ok(secondFrameWrites <= 12, "second frame should only publish essential proof attrs, got " + secondFrameWrites);
  assert.ok(thirdFrameWrites <= 12, "third frame should only publish essential proof attrs, got " + thirdFrameWrites);
  assert.equal(harness.mount.__gosxScene3DWebGPUStats.frameSeq, 3, "in-memory stats must advance exactly every frame");
  assert.equal(harness.mount.__gosxScene3DWebGPUProof.frameSeq, 3, "essential proof must advance exactly every frame");
  assert.equal(Number(harness.mount.getAttribute("data-gosx-scene3d-webgpu-frame-seq")), 3, "essential DOM frame sequence must stay prompt");

  harness.env.context.__gosx_scene3d_webgpu_telemetry = true;
  const beforeVerbose = attributeWrites;
  nowMS = 48;
  renderFrame(0.048);
  assert.ok(attributeWrites - beforeVerbose > 50, "explicit diagnostics should publish the broad attribute surface immediately");
  assert.equal(harness.mount.__gosxScene3DWebGPUStats.frameSeq, 4);
});

test("[perf-shape] Scene3D WebGPU wgpuStablePBRAttributeBuffer still re-uploads every frame a mesh object's world geometry actually changes", async () => {
  // Correctness guard for wgpuStablePBRAttributeBuffer's content-compare skip
  // (16a-scene-webgpu.js): a spinning object's worldMeshPositions are CPU-
  // baked fresh every frame from object.spinX * timeSeconds (see
  // translateScenePointInto in 10-runtime-scene-core.js), so they genuinely
  // differ frame to frame -- unlike the water demo's static float-* objects,
  // this must NOT hit the "unchanged content" fast path.
  const harness = await createBoardWebGPUHarness({ fresh: true });
  const api = harness.env.context.__gosx_scene3d_api;
  const state = api.createSceneState({
    scene: {
      materials: [{ name: "spin-mat", kind: "standard", color: "#8de1ff" }],
      // Explicit vertices (not a bare kind:"box"/"sphere" primitive, which
      // sceneObjectHasTriangleMesh only recognizes as a triangle mesh once it
      // carries its own vertices -- otherwise it renders as a procedural
      // line/outline object, a different, irrelevant path) so this object
      // actually flows through appendSceneMeshObjectToBundle's per-vertex
      // world-space bake (the CPU path whose OUTPUT feeds
      // ensurePBRSceneAttributeBuffers/wgpuStablePBRAttributeBuffer).
      objects: [{
        id: "spin-tri",
        kind: "mesh",
        spinX: 1,
        spinY: 0.7,
        material: "spin-mat",
        vertices: {
          count: 3,
          positions: [0, 1, 0, -1, -1, 0, 1, -1, 0],
          normals: [0, 0, 1, 0, 0, 1, 0, 0, 1],
          uvs: [0.5, 1, 0, 0, 1, 0],
        },
      }],
    },
  });
  const objects = api.sceneStateObjectsWithMaterials(state);
  harness.canvas.width = 64;
  harness.canvas.height = 64;

  const bytesPerFrame = [];
  for (let frame = 0; frame < 4; frame += 1) {
    const bundle = api.createSceneRenderBundle(
      64, 64, "#000000",
      { x: 0, y: 0, z: 4, fov: 60, near: 0.05, far: 128 },
      objects, [], [], [], [], {}, frame * 0.25, [], [], [], [], [], 0, false,
    );
    const beforeBytes = harness.fake.state.writeBufferCalls.reduce((s, c) => s + (c.data && c.data.byteLength || 0), 0);
    harness.renderer.render(bundle, { width: 64, height: 64 });
    const afterBytes = harness.fake.state.writeBufferCalls.reduce((s, c) => s + (c.data && c.data.byteLength || 0), 0);
    bytesPerFrame.push(afterBytes - beforeBytes);
  }
  // Every frame (not just the warmup frame) must re-upload -- the object
  // genuinely rotates every frame, so bytesPerFrame must NOT collapse toward
  // ~0 the way the static water-demo case does (see the sibling perf-shape
  // test above): each frame's PBR mesh reupload is comparable in size to the
  // first (allow generous slack for unrelated small per-frame uniform noise,
  // but it must stay the same ORDER of magnitude, not crater to near-zero).
  for (let i = 1; i < bytesPerFrame.length; i += 1) {
    assert.ok(
      bytesPerFrame[i] > bytesPerFrame[0] * 0.5,
      "frame " + i + ": writeBuffer bytes (" + bytesPerFrame[i] + ") collapsed relative to frame 0 (" + bytesPerFrame[0] +
      ") even though the object spins every frame -- wgpuStablePBRAttributeBuffer's content-compare must not skip a real geometry change",
    );
  }
});

test("Scene3D WebGPU water compute kernels route through the generic Selena feedback-compute path with matching bindings", async () => {
  // {fresh:true, fakeDeviceOptions:{validateBindings:true}} -- this test
  // exercises the NEW getSelenaComputePipeline/createSelenaComputeBindGroup/
  // dispatchWaterComputeStage bootstrap-src edits directly, with the fake
  // device's structural binding-mismatch gate extended to createComputePipeline
  // (validateComputePipelineDesc above) turned on: any @group/@binding drift
  // between a kernel's WGSL and the bind-group-layout/bind-group the renderer
  // builds for it throws immediately.
  const harness = await createBoardWebGPUHarness({
    fresh: true,
    fakeDeviceOptions: { validateBindings: true },
  });
  const api = harness.env.context.__gosx_scene3d_api;
  assert.ok(api && typeof api.createSceneState === "function", "scene3d chunk must publish createSceneState");

  const waterEntry = {
    id: "water-main",
    resolution: 16,
    surfaceResolution: 3,
    poolShape: "Box",
    poolWidth: 1,
    poolLength: 1,
    poolHeight: 1,
    cornerRadius: 0,
    tileTexture: "",
    causticsResolution: 4,
    objectShadowResolution: 4,
    lightDirectionX: 2,
    lightDirectionY: 2,
    lightDirectionZ: -1,
    materialBackend: "selena",
    // Configuration that exercises every one of the 5 kernels on the FIRST
    // rendered frame: seedDrops>0 fires the once-only seed dispatch
    // (system.seeded starts false); dropEventID>0 fires the interactive drop
    // dispatch (system.lastDropEventID starts 0); an active sphere object
    // fires the continuous displacement dispatch (system.waterObjectActive);
    // simulation (x2) and normal always dispatch every unpaused frame.
    seedDrops: 4,
    dropEventID: 1,
    activeObject: "Sphere",
    objectKind: "sphere",
    objectX: -0.4,
    objectY: -0.75,
    objectZ: 0.2,
    objectRadius: 0.25,
    objectDisplacementScale: 1,
    // The real, Selena-compiled compute WGSL + descriptors under test.
    // Everything else on this entry is the OLD hand-written-WGSL/hardcoded
    // compute contract (left completely alone -- this test only exercises
    // the additive seed/drop/displacement/simulation/normal Selena slots).
    seedSelenaWGSL: waterSeedSelenaFixture.wgsl,
    dropSelenaWGSL: waterDropSelenaFixture.wgsl,
    displacementSelenaWGSL: waterDisplacementSelenaFixture.wgsl,
    simulationSelenaWGSL: waterSimulationSelenaFixture.wgsl,
    normalSelenaWGSL: waterNormalSelenaFixture.wgsl,
    shaderDescriptors: {
      seed: waterSeedSelenaFixture.layout,
      drop: waterDropSelenaFixture.layout,
      displacement: waterDisplacementSelenaFixture.layout,
      simulation: waterSimulationSelenaFixture.layout,
      normal: waterNormalSelenaFixture.layout,
    },
  };

  // Drive the entry through the SAME normalization the production runtime
  // uses (createSceneState -> sceneWaterSystems -> normalizeSceneWaterSystemEntry
  // in 10-runtime-scene-core.js), proving the 5 new *SelenaWGSL slots survive
  // that layer, not just a hand-built bundle.
  const sceneState = api.createSceneState({ scene: { waterSystems: [waterEntry] } });
  assert.equal(sceneState.waterSystems.length, 1);
  const normalized = sceneState.waterSystems[0];
  assert.equal(normalized.surfaceResolution, 3, "surface topology must remain independent from the simulation grid");
  assert.equal(normalized.seedSelenaWGSL, waterSeedSelenaFixture.wgsl, "seedSelenaWGSL must survive normalizeSceneWaterSystemEntry");
  assert.equal(normalized.dropSelenaWGSL, waterDropSelenaFixture.wgsl, "dropSelenaWGSL must survive normalizeSceneWaterSystemEntry");
  assert.equal(normalized.displacementSelenaWGSL, waterDisplacementSelenaFixture.wgsl, "displacementSelenaWGSL must survive normalizeSceneWaterSystemEntry");
  assert.equal(normalized.simulationSelenaWGSL, waterSimulationSelenaFixture.wgsl, "simulationSelenaWGSL must survive normalizeSceneWaterSystemEntry");
  assert.equal(normalized.normalSelenaWGSL, waterNormalSelenaFixture.wgsl, "normalSelenaWGSL must survive normalizeSceneWaterSystemEntry");
  assert.ok(
    normalized.shaderDescriptors && normalized.shaderDescriptors.seed && normalized.shaderDescriptors.normal,
    "shaderDescriptors for the compute kernels must survive normalizeSceneWaterSystemEntry",
  );

  harness.canvas.width = 64;
  harness.canvas.height = 64;
  assert.doesNotThrow(() => {
    harness.renderer.render(sceneState, { width: 64, height: 64 }, { nowMS: 0, active: true, qualityTier: "full" });
    harness.renderer.render(sceneState, { width: 64, height: 64 }, { nowMS: 17, active: true, qualityTier: "full" });
  }, "render() must not throw -- a throw here means the fake device's validator caught a @group/@binding mismatch between a compute kernel's WGSL and the renderer's bind group layout/bind group");

  const mount = harness.mount;
  // The Selena compute path fired: every kernel found its WGSL+descriptor,
  // dispatched through getSelenaComputePipeline/createSelenaComputeBindGroup,
  // and none fell back to the hardcoded/authored pipeline.
  assert.equal(mount.getAttribute("data-gosx-scene3d-webgpu-water-selena-compute-systems"), "1", "the system must be counted once for having Selena compute kernels configured");
  assert.equal(mount.getAttribute("data-gosx-scene3d-webgpu-water-selena-compute-fallbacks"), "0", "no kernel should have fallen back to the hardcoded/authored compute pipeline");
  const selenaComputeDispatches = Number(mount.getAttribute("data-gosx-scene3d-webgpu-water-selena-compute-dispatches"));
  // The anchor frame performs no simulation mutation. Its queued seed, drop,
  // and displacement drain on the first fixed tick alongside 2x simulation
  // and one normal dispatch.
  assert.equal(selenaComputeDispatches, 6, "expected 6 Selena dispatches on the first fixed tick (3 queued events+2xsimulation+normal)");
  assert.equal(mount.getAttribute("data-gosx-scene3d-webgpu-water-compute-dispatches"), "6", "waterComputeDispatches must equal the Selena count -- nothing fell back");

  // Structural corroboration per kernel: the compiled pipeline's bind group
  // layout carries EXACTLY the descriptor's bindings (grid=0, inState=1,
  // outState=2, UserUniforms=3 when the kernel has any param/context field),
  // and a real bind group
  // was built against that exact layout.
  assertWaterComputeKernelBindings(harness.fake, "WaterSimSeed", [0, 1, 2, 3]);
  assertWaterComputeKernelBindings(harness.fake, "WaterSimDrop", [0, 1, 2, 3]);
  assertWaterComputeKernelBindings(harness.fake, "WaterSimDisplace", [0, 1, 2, 3]);
  assertWaterComputeKernelBindings(harness.fake, "WaterSimStep", [0, 1, 2, 3]);
  assertWaterComputeKernelBindings(harness.fake, "WaterSimNormal", [0, 1, 2, 3]);
  const normalPipeline = waterComputeKernelPipeline(harness.fake, "WaterSimNormal");
  const normalLayout = normalPipeline.desc.layout.desc.bindGroupLayouts[0];
  const normalGroup = harness.fake.state.bindGroups.find((bg) => bg.desc && bg.desc.layout === normalLayout);
  const normalUniform = normalGroup.desc.entries.find((entry) => entry.binding === 3).resource.buffer;
  const normalUniformWrite = harness.fake.state.writeBufferCalls.find((call) => call.buffer === normalUniform);
  assert.ok(normalUniformWrite, "physical water-cell spacing must be uploaded to the Selena normal kernel");
  assert.ok(Math.abs(normalUniformWrite.data[0] - 0.125) < 1e-7, "cellSizeX must be 2*poolWidth/resolution");
  assert.ok(Math.abs(normalUniformWrite.data[1] - 0.125) < 1e-7, "cellSizeZ must be 2*poolLength/resolution");
  assert.equal(mount.__gosxScene3DWebGPUStats.waterSurfaceResolution, 3, "render topology must use authored surfaceResolution, not simulation resolution");
});

test("16a render() adapts a Go-marshaled ortho-2D board bundle and draws its rect quads (zero-copy seam)", async () => {
  const harness = await createBoardWebGPUHarness();
  const bundle = JSON.parse(goBoardBundleRectsJSON);
  const objectsRef = bundle.objects;
  const worldPositionsRef = bundle.worldPositions;
  const worldNormalsRef = bundle.worldNormals;
  const worldUVsRef = bundle.worldUVs;

  harness.renderer.render(bundle, {});

  // (a) Zero-copy aliasing: meshObjects IS the Go objects array (identity).
  assert.equal(bundle.meshObjects, objectsRef, "meshObjects must alias bundle.objects by identity");
  // The native-vocabulary FIELDS keep their references, so the 26b1 painter's
  // reads (objects/worldPositions/materials[i].color) see unchanged data.
  // (Re-marshaling an adapted bundle is NOT protected — the seam adds
  // meshObjects/worldMesh* aliases and materializes elided zeros in place.)
  assert.equal(bundle.objects, objectsRef);
  assert.equal(bundle.worldPositions, worldPositionsRef);
  assert.equal(bundle.worldNormals, worldNormalsRef);
  assert.equal(bundle.worldUVs, worldUVsRef);
  // worldMesh* aliases hold the same geometry. 16a's attribute getter is
  // allowed to canonicalize the FIELD to a typed array in place (its normal
  // caching for any scene bundle), so assert data, not constructor identity.
  assert.equal(bundle.worldMeshPositions.length, 36, "positions alias must expose all 12 vertices");
  assert.equal(bundle.worldMeshPositions[0], 16);
  assert.equal(bundle.worldMeshPositions[1], 24);
  assert.equal(bundle.worldMeshPositions[2], 0);
  assert.equal(bundle.worldMeshPositions[33], 280);
  assert.equal(bundle.worldMeshPositions[34], 150);
  assert.equal(bundle.worldMeshPositions[35], 0);
  assert.equal(bundle.worldMeshNormals.length, 36);
  assert.equal(bundle.worldMeshNormals[2], 1, "board normals are +Z");
  assert.equal(bundle.worldMeshUVs.length, 24);

  // (b) The draw reached the PBR object path: the main pass recorded one
  // 6-vertex draw per rect — INCLUDING card-a, whose vertexOffset:0 and
  // materialIndex:0 were elided by Go's omitempty and must be restored by
  // the seam (16a gates objects on Number.isFinite(vertexOffset)).
  const mains = mainRenderPasses(harness.fake);
  assert.equal(mains.length, 1, "exactly one main render pass");
  const main = mains[0];
  assert.deepEqual(
    main.draws.map((draw) => draw.vertexCount),
    [6, 6],
    "both rects must draw 6 vertices each",
  );
  assert.ok(main.ended, "main pass must end");
  assert.ok(harness.fake.state.submitCount >= 1, "frame must submit");

  // Vertex data upload: the packed positions buffer write carries the Go quad
  // vertices (12 verts * 3 floats).
  const positionsWrite = harness.fake.state.writeBufferCalls.find(
    (call) => call.data && call.data.length === 36 && call.data[0] === 16 && call.data[1] === 24 && call.data[33] === 280,
  );
  assert.ok(positionsWrite, "positions upload must carry the Go worldPositions quads");

  // (3) background: the generic clear-color path reads bundle.background.
  // "#102030" → rgba(16/255, 32/255, 48/255, 1).
  const clear = main.descriptor.colorAttachments[0].clearValue;
  assert.ok(Math.abs(clear.r - 16 / 255) < 1e-6, "clear r from bundle.background");
  assert.ok(Math.abs(clear.g - 32 / 255) < 1e-6, "clear g from bundle.background");
  assert.ok(Math.abs(clear.b - 48 / 255) < 1e-6, "clear b from bundle.background");
  assert.equal(clear.a, 1);

  // Published frame stats observe the adapted meshObjects.
  assert.equal(harness.mount.getAttribute("data-gosx-scene3d-webgpu-mesh-objects"), "2");
  assert.equal(harness.mount.getAttribute("data-gosx-scene3d-webgpu-line-entries"), "0");

  // Idempotency: the host re-renders the same bundle object every frame. The
  // !bundle.meshObjects re-entry guard must keep the aliases (and 16a's
  // typed-array canonicalization) stable instead of re-clobbering them.
  const typedPositionsAfterFirstFrame = bundle.worldMeshPositions;
  harness.renderer.render(bundle, {});
  assert.equal(bundle.meshObjects, objectsRef, "meshObjects identity must survive re-render");
  assert.equal(
    bundle.worldMeshPositions,
    typedPositionsAfterFirstFrame,
    "re-render must not re-alias worldMeshPositions over the canonicalized typed array",
  );
  const mainsAfterSecond = mainRenderPasses(harness.fake);
  assert.equal(mainsAfterSecond.length, 2, "second frame must draw again");
  assert.deepEqual(mainsAfterSecond[1].draws.map((draw) => draw.vertexCount), [6, 6]);
});

test("16a render() draws board rect+line+sprite quads from the Go GPU bundle (M1 slice 2A)", async () => {
  const harness = await createBoardWebGPUHarness();
  const bundle = JSON.parse(goBoardBundleMixedJSON);
  const linesRef = bundle.lines;
  const labelsRef = bundle.labels;
  const spritesRef = bundle.sprites;

  // Fixture contract: the Go attach appended the line/sprite quads in
  // painter z-order with per-primitive materials.
  assert.equal(bundle.objects.length, 4, "rect + 2 line quads + sprite quad");
  assert.deepEqual(
    bundle.objects.map((obj) => obj.kind),
    ["rect", "line", "line", "sprite"],
    "objects ride in painter z-order (rects, lines, sprites)",
  );
  assert.equal(bundle.materials.length, 4);
  const spriteMaterial = bundle.materials[bundle.objects[3].materialIndex];
  assert.equal(spriteMaterial.kind, "sprite");
  assert.equal(spriteMaterial.texture, "/logo.png", "sprite material carries Texture=Src (the 2B wire contract)");
  assert.equal(spriteMaterial.shaderBackend, undefined, "sprite material has NO Selena fields (it rides the default PBR object path)");

  // The Go wire shape (lineWidth 1 from the typed CanvasBoardNode path) must
  // remain inside 16a's supported envelope at mount-selection time too.
  assert.equal(harness.renderer.supportsBundle(bundle), true, "board wire bundle must stay WebGPU-supported");

  harness.renderer.render(bundle, {});

  // All four quads draw 6 vertices each through the meshObjects path, in
  // array order — GPU z-order parity with the 26b1 painter.
  const mains = mainRenderPasses(harness.fake);
  assert.equal(mains.length, 1);
  assert.deepEqual(mains[0].draws.map((draw) => draw.vertexCount), [6, 6, 6, 6], "rect, both lines, and the sprite each draw their quad");
  assert.equal(harness.mount.getAttribute("data-gosx-scene3d-webgpu-mesh-objects"), "4");
  assert.equal(harness.mount.getAttribute("data-gosx-scene3d-webgpu-line-entries"), "0", "line quads draw as mesh objects, not via a line pipeline");

  // Rect + line draws share the one Selena BoardFill pipeline; the sprite
  // draw goes through the default PBR pipeline (sprites use no Selena shader).
  const selenaPipelines = harness.fake.state.renderPipelines.filter(
    (pipeline) => pipeline.desc && typeof pipeline.desc.label === "string" && pipeline.desc.label.startsWith("gosx-selena-BoardFill"),
  );
  assert.equal(selenaPipelines.length, 1, "flat rect/line materials share one BoardFill pipeline");
  for (const draw of mains[0].draws.slice(0, 3)) {
    assert.equal(draw.pipeline, selenaPipelines[0], "rect/line quads draw through the BoardFill pipeline");
  }
  assert.notEqual(mains[0].draws[3].pipeline, selenaPipelines[0], "the sprite quad draws through the default (PBR) pipeline, not the BoardFill selena one");

  // The wire payloads are left exactly as marshaled (labels draw in 2C; the
  // lines/sprites records stay for the painter path and diagnostics).
  assert.equal(bundle.lines, linesRef);
  assert.equal(bundle.labels, labelsRef);
  assert.equal(bundle.sprites, spritesRef);
  assert.equal(bundle.lines.length, 2);
  assert.equal(bundle.lines[0].from.x, 216);
  assert.equal(bundle.lines[0].lineWidth, 1);
});

test("16a board sprite texture lifecycle: placeholder on first frame, image upload after load, per-URL cache", async () => {
  const harness = await createBoardWebGPUHarness();
  const bundle = JSON.parse(goBoardBundleMixedJSON);

  // First frame: the sprite's material.texture is "/logo.png". 16a resolves
  // it through wgpuLoadTexture, which immediately creates a 1×1 white
  // placeholder texture (so the sprite shows a white box, not garbage, while
  // the image loads) and kicks off an async Image load.
  harness.renderer.render(bundle, {});
  const placeholderWrites = harness.fake.state.writeTextureCalls.filter(
    (call) => call.data && call.data.length === 4 && call.data[0] === 255 && call.data[1] === 255 && call.data[2] === 255 && call.data[3] === 255,
  );
  // Two 1×1 white writes: 16a's init-time global placeholder AND the per-URL
  // sprite placeholder. The sprite one is what proves the placeholder path ran
  // for "/logo.png" before the image resolved.
  assert.ok(placeholderWrites.length >= 2, "the sprite URL must get a 1×1 white placeholder upload before its image loads");
  assert.ok(harness.env.imageLoads.includes("/logo.png"), "the sprite URL must start an Image load");
  assert.equal(harness.fake.state.copyExternalCalls.length, 0, "no real image upload yet on the first frame (still loading)");

  // Drain the FakeImage onload (setTimeout(0)) + createImageBitmap microtask:
  // the resolved image is uploaded via copyExternalImageToTexture into a fresh
  // texture, replacing the placeholder in the per-URL cache.
  await flushAsyncWork();
  assert.equal(harness.fake.state.copyExternalCalls.length, 1, "the loaded image must upload exactly once via copyExternalImageToTexture");

  const imageLoadCountAfterFirst = harness.env.imageLoads.filter((src) => src === "/logo.png").length;
  const copyCountAfterFirst = harness.fake.state.copyExternalCalls.length;

  // Second frame, same bundle/URL: the textureCache is keyed by URL, so
  // wgpuLoadTexture must return the cached record without constructing another
  // Image or issuing another upload.
  harness.renderer.render(bundle, {});
  await flushAsyncWork();
  assert.equal(
    harness.env.imageLoads.filter((src) => src === "/logo.png").length,
    imageLoadCountAfterFirst,
    "re-rendering the same sprite URL must hit the per-URL cache (no second Image load)",
  );
  assert.equal(
    harness.fake.state.copyExternalCalls.length,
    copyCountAfterFirst,
    "cached texture must not re-upload on the second frame",
  );
});

test("16a board sprite draws on the default PBR pipeline with its albedo texture bound; rects/lines stay on BoardFill", async () => {
  const harness = await createBoardWebGPUHarness();
  const bundle = JSON.parse(goBoardBundleMixedJSON);

  // Render once (placeholder), flush the load, render again so the material
  // bind group is rebuilt with the resolved (non-placeholder) albedo view.
  harness.renderer.render(bundle, {});
  await flushAsyncWork();
  harness.renderer.render(bundle, {});

  const mains = mainRenderPasses(harness.fake);
  const main = mains[mains.length - 1];

  // Draw order parity with the 26b1 painter: rect, both lines, then the
  // sprite — four 6-vertex quads.
  assert.deepEqual(main.draws.map((draw) => draw.vertexCount), [6, 6, 6, 6], "rect, both lines, and the sprite each draw their quad");

  // The flat rect/line materials share the one BoardFill selena pipeline; the
  // sprite carries no Selena fields, so it falls through to the default PBR
  // object pipeline (cullMode "none").
  const selenaPipelines = harness.fake.state.renderPipelines.filter(
    (pipeline) => pipeline.desc && typeof pipeline.desc.label === "string" && pipeline.desc.label.startsWith("gosx-selena-BoardFill"),
  );
  assert.equal(selenaPipelines.length, 1, "rect/line flat materials share one BoardFill pipeline");
  for (const draw of main.draws.slice(0, 3)) {
    assert.equal(draw.pipeline, selenaPipelines[0], "rect/line quads draw through the BoardFill pipeline");
  }
  const spriteDraw = main.draws[3];
  assert.notEqual(spriteDraw.pipeline, selenaPipelines[0], "the sprite draws through the default PBR pipeline, not BoardFill");
  assert.match(spriteDraw.pipeline.desc.label, /gosx-pbr-/, "the sprite pipeline is the PBR object pipeline");
  assert.equal(spriteDraw.pipeline.desc.primitive.cullMode, "none", "the PBR board pipeline disables culling so the +Z board quad stays visible");

  // The sprite's material bind group (the last PBR material bind group bound
  // before the sprite draw, slot 1) must carry the resolved albedo texture
  // view at binding 1 — a recorded copyExternalImageToTexture target, NOT the
  // init-time placeholder. This proves the texture reached the draw.
  const copyTargetIds = new Set(
    harness.fake.state.copyExternalCalls.map((call) => call.texture && call.texture.id).filter((id) => id != null),
  );
  assert.equal(copyTargetIds.size, 1, "exactly one uploaded sprite texture");
  const materialBindGroups = main.bindGroups.filter(
    (entry) => entry.slot === 1 && entry.group && entry.group.desc && Array.isArray(entry.group.desc.entries),
  );
  assert.ok(materialBindGroups.length >= 1, "the sprite draw binds a PBR material bind group at slot 1");
  const spriteBindGroup = materialBindGroups[materialBindGroups.length - 1].group;
  const albedoEntry = spriteBindGroup.desc.entries.find((entry) => entry.binding === 1);
  assert.ok(albedoEntry && albedoEntry.resource && albedoEntry.resource.__kind === "textureView", "binding 1 is the albedo texture view");
  assert.ok(copyTargetIds.has(albedoEntry.resource.textureId), "the bound albedo view is the uploaded sprite texture, not the placeholder");

  // Sprite quads draw as mesh objects, never via a line pipeline.
  assert.equal(harness.mount.getAttribute("data-gosx-scene3d-webgpu-line-entries"), "0");
});

test("16a adaptOrtho2DBoardBundle gate: aliases exactly once, only for ortho-2D board bundles", async () => {
  const harness = await createBoardWebGPUHarness();

  // (1) Exact-identity aliasing, observed before any draw: a board bundle
  // with positions but WITHOUT normals fails 16a's hasPBRData gate and
  // returns before the typed-array canonicalization — the aliases must be
  // the very same array objects.
  const partial = {
    camera: { mode: "ortho2d", z: 1, near: -1, far: 1 },
    materials: [{ kind: "flat", color: "#fff", unlit: true }],
    objects: [{ id: "r", kind: "rect", vertexCount: 6, bounds: { maxX: 10, maxY: 10 } }],
    worldPositions: [0, 0, 0, 10, 0, 0, 10, 10, 0, 0, 0, 0, 10, 10, 0, 0, 10, 0],
  };
  harness.renderer.render(partial, {});
  assert.equal(partial.meshObjects, partial.objects, "meshObjects must alias objects by identity");
  assert.equal(partial.worldMeshPositions, partial.worldPositions, "worldMeshPositions must alias worldPositions by identity (zero-copy)");
  assert.equal(partial.worldMeshNormals, undefined, "absent worldNormals alias to undefined");
  assert.equal(mainRenderPasses(harness.fake).length, 0, "no PBR data → no main pass");

  // (2) Non-ortho2d bundles are untouched even when they carry native-shaped
  // objects/worldPositions.
  const perspective = {
    camera: { x: 0, y: 0, z: 6 },
    materials: [{ kind: "flat", color: "#fff" }],
    objects: [{ id: "r", kind: "rect", vertexOffset: 0, vertexCount: 6 }],
    worldPositions: [0, 0, 0, 1, 0, 0, 1, 1, 0, 0, 0, 0, 1, 1, 0, 0, 1, 0],
    worldColors: [1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1],
    worldNormals: [0, 0, 1, 0, 0, 1, 0, 0, 1, 0, 0, 1, 0, 0, 1, 0, 0, 1],
  };
  harness.renderer.render(perspective, {});
  assert.equal(perspective.meshObjects, undefined, "non-ortho2d bundles must not gain meshObjects");
  assert.equal(perspective.worldMeshPositions, undefined, "non-ortho2d bundles must not gain worldMesh aliases");

  // (3) Bundles that already carry meshObjects (scene-core vocabulary) keep
  // them — the adapter must not re-alias over an existing draw source.
  const presetMeshObjects = [{ id: "pre", vertexOffset: 0, vertexCount: 3 }];
  const presetPositions = [0, 0, 0, 1, 0, 0, 0, 1, 0];
  const already = {
    camera: { mode: "ortho2d", z: 1, near: -1, far: 1 },
    materials: [],
    objects: [{ id: "ignored", vertexOffset: 0, vertexCount: 6 }],
    meshObjects: presetMeshObjects,
    worldMeshPositions: presetPositions,
    worldPositions: [9, 9, 9],
  };
  harness.renderer.render(already, {});
  assert.equal(already.meshObjects, presetMeshObjects, "existing meshObjects must keep identity");
  assert.equal(already.worldMeshPositions, presetPositions, "existing worldMeshPositions must keep identity");

  // (4) ortho2d with no objects → nothing to alias.
  const empty = {
    camera: { mode: "ortho2d", z: 1, near: -1, far: 1 },
    objects: [],
  };
  harness.renderer.render(empty, {});
  assert.equal(empty.meshObjects, undefined, "empty board bundles gain no aliases");
});

test("16a board rects draw through the Selena BoardFill pipeline: custom WGSL module, one pipeline for N materials, baseColor uniforms", async () => {
  const harness = await createBoardWebGPUHarness();
  const bundle = JSON.parse(goBoardBundleRectsJSON);

  // Fixture contract: the Go attach (bundle2d.AttachBoardGPUGeometry →
  // attachBoardFillMaterials) flowed the Selena fields through the wire in
  // exactly the names sceneSelenaIsMaterial reads.
  assert.equal(bundle.materials.length, 2);
  for (const material of bundle.materials) {
    assert.equal(material.shaderBackend, "selena");
    assert.match(material.customVertexWGSL, /vertexMain/);
    assert.match(material.customFragmentWGSL, /baseColor/);
    assert.ok(material.shaderLayout && Array.isArray(material.shaderLayout.uniformBlock.fields));
    assert.equal(material.customUniforms.baseColor.length, 3);
  }

  harness.renderer.render(bundle, {});

  // (1) The BoardFill WGSL reached createShaderModule verbatim.
  const fillModules = harness.fake.state.shaderModules.filter(
    (module) => typeof module.code === "string" && module.code.includes("baseColor") && module.code.includes("fragmentMain"),
  );
  assert.equal(fillModules.length, 1, "the BoardFill WGSL must compile into exactly one shader module");

  // (2) Pipeline sharing: the two materials carry IDENTICAL WGSL/layout and
  // differ only in customUniforms.baseColor, so getSelenaPipeline's
  // content-based cache must create exactly ONE selena pipeline (the values
  // ride per-object bind groups instead).
  const selenaPipelines = harness.fake.state.renderPipelines.filter(
    (pipeline) => pipeline.desc && typeof pipeline.desc.label === "string" && pipeline.desc.label.startsWith("gosx-selena-BoardFill"),
  );
  assert.equal(selenaPipelines.length, 1, "N same-WGSL materials must share one selena pipeline");

  // (3) Both rect draws were issued with the selena pipeline bound — the
  // default PBR pipeline never draws. (The engine pre-binds the PBR pipeline
  // at the top of the opaque block before drawPBRObjects switches per object;
  // that bind carries no draw, so assert at the draw level, not bind order.)
  const mains = mainRenderPasses(harness.fake);
  assert.equal(mains.length, 1);
  const main = mains[0];
  assert.deepEqual(main.draws.map((draw) => draw.vertexCount), [6, 6], "both rects draw their quads");
  for (const draw of main.draws) {
    assert.equal(draw.pipeline, selenaPipelines[0], "every rect draw must go through the shared selena pipeline");
  }

  // (4) The theme colors reached the GPU as baseColor uniform bytes: the
  // 128-byte selena uniform block (32 floats; baseColor vec3 at offset 112 →
  // floats 28..30) is written once per rect, on distinct per-object buffers.
  const uniformWrites = harness.fake.state.writeBufferCalls.filter(
    (call) => call.data && call.data.length === 32,
  );
  assert.equal(uniformWrites.length, 2, "one selena uniform write per rect material");
  assert.notEqual(uniformWrites[0].buffer, uniformWrites[1].buffer, "each object owns its uniform buffer (N bind groups)");
  const approx = (got, want, label) => assert.ok(Math.abs(got - want) < 1e-6, label + ": " + got + " vs " + want);
  // #3a86ff → (58, 134, 255)/255
  approx(uniformWrites[0].data[28], 58 / 255, "card-a baseColor.r");
  approx(uniformWrites[0].data[29], 134 / 255, "card-a baseColor.g");
  approx(uniformWrites[0].data[30], 255 / 255, "card-a baseColor.b");
  // #ffbe0b → (255, 190, 11)/255
  approx(uniformWrites[1].data[28], 255 / 255, "card-b baseColor.r");
  approx(uniformWrites[1].data[29], 190 / 255, "card-b baseColor.g");
  approx(uniformWrites[1].data[30], 11 / 255, "card-b baseColor.b");
  // The MVP (floats 0..15) rides the same block — the ortho-2D viewProjection
  // must be present (non-zero diagonal), proving the 2D camera reached the
  // selena uniform path.
  assert.notEqual(uniformWrites[0].data[0], 0, "selena mvp must carry the ortho-2D projection");
});

test("16a splits SUBMITTED mesh draws from CULLED ones (mesh-draw-calls / mesh-view-culled)", async () => {
  // Regression coverage for the telemetry gap that let three Selena mesh
  // planes on m31labs.dev read data-gosx-scene3d-webgpu-mesh-objects="3" for
  // ~two weeks while a camera-depth sign error CPU-frustum-culled them to
  // zero drawn pixels: mesh-objects counts the bundle (SUBMITTED + CULLED
  // together), so it never moved. mesh-draw-calls/mesh-view-culled split
  // the two apart.
  const harness = await createBoardWebGPUHarness();
  const bundle = {
    camera: { x: 0, y: 0, z: 6, fov: 60, near: 0.1, far: 100 },
    environment: {},
    materials: [{ kind: "flat", color: "#ffffff", opacity: 1, renderPass: "opaque" }],
    meshObjects: [
      { id: "visible", kind: "box", materialIndex: 0, vertexOffset: 0, vertexCount: 3, viewCulled: false, depthCenter: 4 },
      { id: "culled", kind: "box", materialIndex: 0, vertexOffset: 3, vertexCount: 3, viewCulled: true, depthCenter: 8 },
    ],
    objects: [],
    worldPositions: new Float32Array(0),
    worldColors: new Float32Array(0),
    worldMeshPositions: new Float32Array(18),
    worldMeshNormals: new Float32Array(18),
  };

  harness.renderer.render(bundle, {});

  const mains = mainRenderPasses(harness.fake);
  assert.equal(mains.length, 1, "exactly one main render pass");
  assert.equal(mains[0].draws.length, 1, "buildDrawList excludes the viewCulled object — only the visible one reaches pass.draw()");

  // mesh-objects is the pre-existing bundle count: it includes the culled
  // object (the exact ambiguity that misled the original investigation).
  assert.equal(harness.mount.getAttribute("data-gosx-scene3d-webgpu-mesh-objects"), "2");
  // mesh-draw-calls: SUBMITTED only.
  assert.equal(harness.mount.getAttribute("data-gosx-scene3d-webgpu-mesh-draw-calls"), "1");
  // mesh-view-culled: CULLED only.
  assert.equal(harness.mount.getAttribute("data-gosx-scene3d-webgpu-mesh-view-culled"), "1");
});

test("16a Selena mesh draws request cullMode:none only for doubleSided:true objects", async () => {
  const harness = await createBoardWebGPUHarness();
  const selenaMaterial = JSON.parse(goBoardBundleRectsJSON).materials[0];
  const bundle = {
    camera: { x: 0, y: 0, z: 6, fov: 60, near: 0.1, far: 100 },
    environment: {},
    materials: [selenaMaterial],
    meshObjects: [
      { id: "single-sided", kind: "box", materialIndex: 0, vertexOffset: 0, vertexCount: 3, viewCulled: false },
      { id: "double-sided", kind: "box", materialIndex: 0, vertexOffset: 3, vertexCount: 3, viewCulled: false, doubleSided: true },
    ],
    objects: [],
    worldPositions: new Float32Array(0),
    worldColors: new Float32Array(0),
    worldMeshPositions: new Float32Array(18),
    worldMeshNormals: new Float32Array(18),
  };

  harness.renderer.render(bundle, {});

  const mains = mainRenderPasses(harness.fake);
  assert.equal(mains.length, 1);
  const draws = mains[0].draws;
  assert.equal(draws.length, 2, "both mesh objects draw");
  assert.equal(
    draws[0].pipeline.desc.primitive.cullMode, "back",
    "absent doubleSided keeps the Selena pipeline's existing back-face cull default",
  );
  assert.equal(
    draws[1].pipeline.desc.primitive.cullMode, "none",
    "doubleSided:true draws through a cullMode:none Selena pipeline",
  );
});

test("16a Selena skinned mesh draws preserve per-object doubleSided cull mode", async () => {
  const harness = await createBoardWebGPUHarness();
  const selenaMaterial = JSON.parse(goBoardBundleRectsJSON).materials[0];
  const identity = new Float32Array([
    1, 0, 0, 0,
    0, 1, 0, 0,
    0, 0, 1, 0,
    0, 0, 0, 1,
  ]);
  function skinnedMesh(id, doubleSided, depthCenter) {
    return {
      id,
      kind: "gltf-mesh",
      materialIndex: 0,
      vertexOffset: 0,
      vertexCount: 3,
      viewCulled: false,
      depthCenter,
      doubleSided,
      directVertices: true,
      modelMatrix: identity,
      skin: { jointMatrices: identity },
      vertices: {
        count: 3,
        positions: new Float32Array(9),
        joints: new Float32Array(12),
        weights: new Float32Array([
          1, 0, 0, 0,
          1, 0, 0, 0,
          1, 0, 0, 0,
        ]),
      },
    };
  }
  const bundle = {
    camera: { x: 0, y: 0, z: 6, fov: 60, near: 0.1, far: 100 },
    environment: {},
    materials: [selenaMaterial],
    meshObjects: [
      skinnedMesh("single-sided-skin", false, 4),
      skinnedMesh("double-sided-skin", true, 5),
    ],
    objects: [],
    worldPositions: new Float32Array(0),
    worldColors: new Float32Array(0),
    worldMeshPositions: new Float32Array(0),
    worldMeshNormals: new Float32Array(0),
  };

  harness.renderer.render(bundle, {});

  const mains = mainRenderPasses(harness.fake);
  assert.equal(mains.length, 1);
  assert.deepEqual(
    mains[0].draws.map((draw) => draw.pipeline.desc.primitive.cullMode),
    ["back", "none"],
    "skinned Selena pipelines must cache and draw distinct single- and double-sided variants",
  );
});
