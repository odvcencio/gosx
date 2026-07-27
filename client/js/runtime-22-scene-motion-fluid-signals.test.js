"use strict";
// The WASM motion mixer, the managed fluid controls, and the shared-signal
// bindings for camera, selection, gizmo, cursor and hub output.
//
// Split out of client/js/runtime.test.js. Every shared fake, sandbox builder
// and fixture factory lives in ./runtime-test-harness.js.

const test = require("node:test");
const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const vm = require("node:vm");

const {
  bootstrapSource,
  bootstrapScene3DWebGPUSourceFile,
  FakeElement,
  buildSkinnedGLBBytes,
  createContext,
  installManualRAF,
  flushSceneInitialFrameBoundary,
  runScript,
  flushAsyncWork,
  installWasmMotionMixerStub,
} = require("./runtime-test-harness.js");

test("P4-M3 motion mixer: glTF model routes clip add/play/update/destroy through the WASM mixer when the flag is on", async () => {
  const mount = new FakeElement("div", null);
  mount.id = "scene-model-wasm-mixer-root";

  const env = createContext({
    elements: [mount],
    fetchRoutes: {
      "/models/rig.glb": { bytes: buildSkinnedGLBBytes() },
    },
    manifest: {
      engines: [
        {
          id: "gosx-engine-model-wasm-mixer",
          component: "GoSXScene3D",
          kind: "surface",
          mountId: "scene-model-wasm-mixer-root",
          props: {
            width: 640,
            height: 360,
            autoRotate: false,
            models: [
              {
                id: "rig",
                src: "/models/rig.glb",
                animation: "bend",
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

  // Quaternion +90° about Y; the update stub writes it for node 2 (the
  // translation channel's target / skin joint 1).
  const s = Math.SQRT1_2;
  const calls = installWasmMotionMixerStub(env.context, 2, [0, s, 0, s]);

  // Observe the joint matrices the skinning consumes so we can prove the
  // decoded rotation reached buildNodeTransforms + computeJointMatrices.
  const raf = installManualRAF(env.context);
  runScript(bootstrapSource, env.context, "bootstrap.js");
  const animationApi = env.context.__gosx_scene3d_animation_api;
  const originalCreateMixer = animationApi.createMixer;
  let jsMixerCreated = 0;
  animationApi.createMixer = function trackedCreateMixer(...args) {
    jsMixerCreated += 1;
    return originalCreateMixer.apply(this, args);
  };
  let lastJointMatrices = null;
  const originalComputeJointMatrices = animationApi.computeJointMatrices;
  animationApi.computeJointMatrices = function trackedCompute(skin, nodeTransforms) {
    const result = originalComputeJointMatrices.call(this, skin, nodeTransforms);
    lastJointMatrices = result;
    return result;
  };

  await flushAsyncWork();
  await flushSceneInitialFrameBoundary(raf);

  // Mount succeeded and the WASM mixer (not the JS mixer) was created.
  assert.equal(mount.getAttribute("data-gosx-scene3d-mounted"), "true");
  assert.equal(calls.create, 1, "WASM mixer must be created once");
  assert.equal(jsMixerCreated, 0, "JS mixer must NOT be created when the WASM flag is on");

  // The "bend" clip was added with correctly-shaped JSON (node index, property,
  // interpolation, and plain number arrays from the typed channel buffers).
  assert.equal(calls.addClip.length, 1, "exactly one clip added");
  const added = calls.addClip[0];
  assert.equal(added.name, "bend");
  const clip = JSON.parse(added.clipJSON);
  assert.ok(Array.isArray(clip.channels) && clip.channels.length === 1, "clip JSON has one channel");
  const ch = clip.channels[0];
  assert.equal(ch.node, 2, "channel node index preserved");
  assert.equal(ch.property, "translation", "channel property preserved");
  assert.equal(ch.interpolation, "LINEAR", "channel interpolation preserved");
  assert.deepEqual(ch.times, [0, 1], "times flattened to plain numbers");
  assert.deepEqual(ch.values, [0, 0, 0, 0, 0.5, 0], "values flattened to plain numbers");
  assert.equal(typeof clip.duration, "number");

  // Play routed to the WASM mixer with the resolved positional options.
  assert.equal(calls.play.length, 1, "play routed to WASM mixer");
  assert.deepEqual(calls.play[0], {
    handle: 1, name: "bend", fadeIn: 0.12, loop: true, speed: 1.5, weight: 0.75,
  });

  // Drive an animation frame so sceneAdvanceModelAnimations ticks the mixer.
  const updatesBefore = calls.update;
  raf.flush(16);
  await flushAsyncWork();
  raf.flush(32);
  await flushAsyncWork();
  assert.ok(calls.update > updatesBefore, "per-frame pose must call the WASM mixer update");

  // The decoded rotation reached the skinning stage: the skin has 2 joints
  // ([1, 2]); joint index 1 = node 2 carries our +90°-about-Y rotation, so its
  // 16-float block (offset 16) is no longer identity (m[0] flips toward 0).
  assert.ok(lastJointMatrices && lastJointMatrices.length === 32, "joint matrices computed for the 2-joint skin");
  assert.ok(Math.abs(lastJointMatrices[16 + 0]) < 0.001,
    `joint 1 m[0] should be ~0 after a 90° Y rotation, got ${lastJointMatrices[16 + 0]}`);
  assert.ok(Math.abs(lastJointMatrices[16 + 10]) < 0.001,
    `joint 1 m[10] should be ~0 after a 90° Y rotation, got ${lastJointMatrices[16 + 10]}`);

  assert.equal(env.consoleLogs.error.length, 0);
});

test("P4-M3 motion mixer: WASM mixer is destroyed on scene dispose", async () => {
  const mount = new FakeElement("div", null);
  mount.id = "scene-model-wasm-mixer-dispose-root";

  const env = createContext({
    elements: [mount],
    fetchRoutes: { "/models/rig.glb": { bytes: buildSkinnedGLBBytes() } },
    manifest: {
      engines: [
        {
          id: "gosx-engine-model-wasm-mixer-dispose",
          component: "GoSXScene3D",
          kind: "surface",
          mountId: "scene-model-wasm-mixer-dispose-root",
          props: {
            width: 640,
            height: 360,
            autoRotate: false,
            models: [{ id: "rig", src: "/models/rig.glb", animation: "bend", loop: true }],
          },
        },
      ],
    },
  });
  const s = Math.SQRT1_2;
  const calls = installWasmMotionMixerStub(env.context, 2, [0, s, 0, s]);
  installManualRAF(env.context);
  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();
  assert.equal(calls.create, 1, "WASM mixer created");

  // Disposing the engine triggers the scene handle's dispose(), which must free
  // the per-model WASM mixer.
  env.context.__gosx_dispose_engine("gosx-engine-model-wasm-mixer-dispose");
  await flushAsyncWork();

  assert.deepEqual(calls.destroy, [1], "WASM mixer must be destroyed exactly once on dispose");
});

test("P4-M3 motion mixer: clip JSON + packed-pose decode round-trips through the animation API", () => {
  const env = createContext({});
  runScript(bootstrapSource, env.context, "bootstrap.js");
  const api = env.context.__gosx_scene3d_animation_api;
  assert.equal(typeof api.wasmClipJSON, "function", "wasmClipJSON published");
  assert.equal(typeof api.wasmDecodePose, "function", "wasmDecodePose published");

  // Clip JSON: typed channel buffers flatten to plain number arrays and the
  // node index / property / interpolation survive.
  const clip = {
    name: "bend",
    duration: 1,
    channels: [
      {
        targetID: 4,
        targetNode: 4,
        property: "rotation",
        interpolation: "STEP",
        times: new Float32Array([0, 1]),
        values: new Float32Array([0, 0, 0, 1, 0, 1, 0, 0]),
      },
    ],
  };
  const json = JSON.parse(api.wasmClipJSON(clip));
  assert.equal(json.duration, 1);
  assert.equal(json.channels.length, 1);
  assert.equal(json.channels[0].node, 4);
  assert.equal(json.channels[0].property, "rotation");
  assert.equal(json.channels[0].interpolation, "STEP");
  assert.deepEqual(json.channels[0].times, [0, 1]);
  assert.deepEqual(json.channels[0].values, [0, 0, 0, 1, 0, 1, 0, 0]);

  // Packed-pose decode: two writes for node 7 — a rotation (propID 1, arity 4)
  // and a translation (propID 0, arity 3) — must merge into one entry.
  const f = new Float64Array(64);
  let i = 0;
  // rotation write
  f[i++] = 7; f[i++] = 1; f[i++] = 4; f[i++] = 0.1; f[i++] = 0.2; f[i++] = 0.3; f[i++] = 0.9272;
  // translation write
  f[i++] = 7; f[i++] = 0; f[i++] = 3; f[i++] = 5; f[i++] = 6; f[i++] = 7;
  // scale write for a different node
  f[i++] = 9; f[i++] = 2; f[i++] = 3; f[i++] = 2; f[i++] = 2; f[i++] = 2;
  const count = i;

  const animatedTransforms = new env.context.Map();
  const writes = api.wasmDecodePose(f, count, animatedTransforms);
  assert.equal(writes, 3, "three packed writes decoded");

  const node7 = animatedTransforms.get(7);
  assert.ok(node7, "node 7 entry created");
  assert.deepEqual(node7.rotation, [0.1, 0.2, 0.3, 0.9272], "rotation (quat, arity 4) decoded");
  assert.deepEqual(node7.translation, [5, 6, 7], "translation (vec3) merged into same entry");
  assert.equal(node7.scale, undefined, "untouched property left as default");

  const node9 = animatedTransforms.get(9);
  assert.ok(node9, "node 9 entry created");
  assert.deepEqual(node9.scale, [2, 2, 2], "scale (propID 2, vec3) decoded for a separate node");

  // The decoded pose drives the existing (unchanged) skinning path.
  const nodes = [{ children: [1] }, { children: [] }];
  // node 7/9 aren't in this hierarchy; build a tiny one targeting node 0.
  const at2 = new env.context.Map();
  api.wasmDecodePose(
    Float64Array.from([0, 1, 4, 0, Math.SQRT1_2, 0, Math.SQRT1_2]),
    7,
    at2,
  );
  const transforms = api.buildNodeTransforms(nodes, at2, null, [0]);
  const world0 = transforms.get(0);
  assert.ok(world0, "buildNodeTransforms consumed the decoded rotation");
  // +90° about Y → m[0] ~ 0.
  assert.ok(Math.abs(world0[0]) < 0.001, `m[0] ~ 0 after decoded 90° Y rotation, got ${world0[0]}`);
});

test("P4-M3 motion mixer: stays inert when window.__gosx_motion_wasm is unset (JS mixer path)", async () => {
  const mount = new FakeElement("div", null);
  mount.id = "scene-model-wasm-mixer-inert-root";

  const env = createContext({
    elements: [mount],
    fetchRoutes: { "/models/rig.glb": { bytes: buildSkinnedGLBBytes() } },
    manifest: {
      engines: [
        {
          id: "gosx-engine-model-wasm-mixer-inert",
          component: "GoSXScene3D",
          kind: "surface",
          mountId: "scene-model-wasm-mixer-inert-root",
          props: {
            width: 640,
            height: 360,
            autoRotate: false,
            models: [{ id: "rig", src: "/models/rig.glb", animation: "bend", loop: true }],
          },
        },
      ],
    },
  });

  // Exports present but the opt-in flag deliberately NOT set.
  const s = Math.SQRT1_2;
  const calls = installWasmMotionMixerStub(env.context, 2, [0, s, 0, s]);
  env.context.__gosx_motion_wasm = false;

  const raf = installManualRAF(env.context);
  runScript(bootstrapSource, env.context, "bootstrap.js");
  const animationApi = env.context.__gosx_scene3d_animation_api;
  const originalCreateMixer = animationApi.createMixer;
  let jsMixerCreated = 0;
  animationApi.createMixer = function trackedCreateMixer(...args) {
    jsMixerCreated += 1;
    return originalCreateMixer.apply(this, args);
  };
  await flushAsyncWork();
  raf.flush(16);
  await flushAsyncWork();
  raf.flush(32);
  await flushAsyncWork();

  assert.equal(mount.getAttribute("data-gosx-scene3d-mounted"), "true");
  assert.equal(jsMixerCreated, 1, "JS mixer must be used when the flag is off");
  assert.equal(calls.create, 0, "WASM mixer must NOT be created when the flag is off");
  assert.equal(calls.addClip.length, 0, "no clips routed to the WASM mixer when the flag is off");
  assert.equal(calls.update, 0, "WASM mixer update must never run when the flag is off");
  assert.equal(env.consoleLogs.error.length, 0);
});

test("P4-M3 motion mixer: grow-and-retick passes dt=0 to avoid double clock advance", async () => {
  const mount = new FakeElement("div", null);
  mount.id = "scene-model-wasm-mixer-retick-root";

  const env = createContext({
    elements: [mount],
    fetchRoutes: { "/models/rig.glb": { bytes: buildSkinnedGLBBytes() } },
    manifest: {
      engines: [
        {
          id: "gosx-engine-model-wasm-mixer-retick",
          component: "GoSXScene3D",
          kind: "surface",
          mountId: "scene-model-wasm-mixer-retick-root",
          props: {
            width: 640, height: 360, autoRotate: false,
            models: [{ id: "rig", src: "/models/rig.glb", animation: "bend", loop: true }],
          },
        },
      ],
    },
  });

  const s = Math.SQRT1_2;
  // overflowOnNext: when true the next update call triggers the grow path.
  let overflowOnNext = false;
  // dtForOverflowCall / dtForRetickCall record the dt values around the
  // specific grow/re-tick pair we're interested in.
  let dtForOverflowCall = undefined;
  let dtForRetickCall = undefined;
  let overflowSeen = false;

  env.context.__gosx_motion_wasm = true;
  env.context.__gosx_motion_mixer_create = () => 1;
  env.context.__gosx_motion_mixer_add_clip = () => true;
  env.context.__gosx_motion_mixer_play = () => {};
  env.context.__gosx_motion_mixer_stop = () => {};
  env.context.__gosx_motion_mixer_is_playing = () => true;
  env.context.__gosx_motion_mixer_update = (handle, dt, reduced, outU8) => {
    const f = new Float64Array(outU8.buffer, outU8.byteOffset, Math.floor(outU8.byteLength / 8));
    if (overflowOnNext && !overflowSeen) {
      overflowSeen = true;
      dtForOverflowCall = dt;
      // Return a count larger than the current buffer to force a grow + re-tick.
      return f.length + 10;
    }
    if (overflowSeen && dtForRetickCall === undefined) {
      // This is the re-tick immediately after the overflow call.
      dtForRetickCall = dt;
    }
    // Normal: write a minimal valid rotation pose.
    f[0] = 2; f[1] = 1; f[2] = 4; f[3] = 0; f[4] = s; f[5] = 0; f[6] = s;
    return 7;
  };
  env.context.__gosx_motion_mixer_destroy = () => {};

  const raf = installManualRAF(env.context);
  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();
  // Initial render: dt=0 because lastModelAnimationTimeSeconds is null.
  await flushSceneInitialFrameBoundary(raf);
  // Next animation frame: dt > 0. Arm the overflow so the grow path fires with a real dt.
  overflowOnNext = true;
  raf.flush(48);
  await flushAsyncWork();

  assert.ok(overflowSeen, "overflow/grow path must have been triggered");
  assert.ok(dtForOverflowCall > 0, "the overflow call must carry a positive deltaTime");
  assert.equal(dtForRetickCall, 0, "re-tick after grow must pass dt=0 to avoid double clock advance");
  assert.equal(env.consoleLogs.error.length, 0);
});

test("Scene3D WebGPU water retires replaced systems after submitted work drains", () => {
  assert.match(
    bootstrapScene3DWebGPUSourceFile,
    /function retireWaterSystem\(system\) \{/,
    "water runtime should centralize delayed disposal for replaced systems"
  );
  assert.match(
    bootstrapScene3DWebGPUSourceFile,
    /device\.queue\.onSubmittedWorkDone\(\)\.then\(function\(\) \{\s*system\.dispose\(\);/s,
    "replaced water resources should be retired after submitted WebGPU work drains"
  );
  assert.match(
    bootstrapScene3DWebGPUSourceFile,
    /retireWaterSystem\(record\.system\);/,
    "syncWaterSystems should not immediately destroy resources for signature changes"
  );
  assert.match(
    bootstrapScene3DWebGPUSourceFile,
    /if \(system\._gosxDisposed\) return;/,
    "water resource disposal must be idempotent across deferred and renderer teardown paths"
  );
});

test("Scene3D managed fluid controls clamp rounded pool radius like upstream", () => {
  const controlsSource = fs.readFileSync(path.join(__dirname, "bootstrap-src", "19b-scene-control-forms.js"), "utf8");
  const document = {
    documentElement: { setAttribute() {} },
    querySelector() { return null; },
  };
  const context = vm.createContext({
    Date,
    JSON,
    Math,
    Number,
    document,
    performance: { now: () => 0 },
    sceneNumber(value, fallback) {
      const num = Number(value);
      return Number.isFinite(num) ? num : fallback;
    },
    window: { document },
  });
  vm.runInContext(
    controlsSource + "\nwindow.__managedFluidObjectTest = { read: sceneManagedFluidObjectReadControls, effective: sceneManagedFluidObjectEffectivePoolControls, reflect: sceneManagedFluidObjectReflectForm, maxCornerRadius: sceneManagedFluidObjectMaxCornerRadius };",
    context,
    { filename: "19b-scene-control-forms.js" },
  );

  const api = context.window.__managedFluidObjectTest;
  const cornerRadius = { value: "1", max: "1", disabled: false };
  const form = {
    dataset: {},
    elements: {
      paused: { checked: false, disabled: false },
      object: { value: "Sphere", disabled: false },
      gravity: { checked: false, disabled: false },
      densityEnabled: { checked: false, disabled: false },
      density: { value: "0.9", disabled: false },
      poolShape: { value: "Rounded Box", disabled: false },
      cornerRadius,
      poolWidth: { value: "0.5", disabled: false },
      poolHeight: { value: "1", disabled: false },
      poolLength: { value: "0.5", disabled: false },
      followCamera: { checked: false, disabled: false },
    },
    getAttribute(name) { return name === "data-gosx-scene3d-control-subject" ? "water-main" : null; },
    setAttribute() {},
    querySelector() { return null; },
    closest() { return null; },
  };

  assert.equal(api.maxCornerRadius(0.5, 0.5), 0.45);
  assert.equal(api.maxCornerRadius(2, 2), 1.95);
  const controls = api.read(form);
  assert.equal(controls.cornerRadius, 0.45);
  const effective = api.effective(controls);
  assert.equal(effective.poolShape, "Rounded Box");
  assert.equal(effective.poolWidth, 0.5);
  assert.equal(effective.poolHeight, 1);
  assert.equal(effective.poolLength, 0.5);
  assert.equal(effective.cornerRadius, 0.45);

  api.reflect(form, controls, true, { interaction: { profile: "water-object-drop-orbit" } }, { waterSystems: [{ id: "water-main" }] });
  assert.equal(cornerRadius.max, "0.45");
  assert.equal(cornerRadius.value, "0.45");
  assert.equal(form.dataset.cornerRadius, "0.45");
  assert.equal(form.dataset.maxCornerRadius, "0.45");

  cornerRadius.value = "0.1";
  api.reflect(form, Object.assign({}, controls, { poolShape: "Box", cornerRadius: 0.45 }), true, { interaction: { profile: "water-object-drop-orbit" } }, { waterSystems: [{ id: "water-main" }] });
  assert.equal(cornerRadius.value, "0.1");
  assert.equal(form.dataset.cornerRadius, "0");
  assert.equal(form.dataset.maxCornerRadius, "0");
});


test("Scene3D managed fluid controls sync previous position on pool resize like upstream", () => {
  const controlsSource = fs.readFileSync(path.join(__dirname, "bootstrap-src", "19b-scene-control-forms.js"), "utf8");
  const document = {
    documentElement: { setAttribute() {} },
    getElementById() { return null; },
    querySelector() { return null; },
  };
  let nowMs = 0;
  const context = vm.createContext({
    Date,
    JSON,
    Math,
    Number,
    SCENE_CMD_SET_TRANSFORM: 2,
    SCENE_CMD_SET_PARTICLES: 6,
    SCENE_CMD_SET_MODELS: 10,
    document,
    performance: { now: () => nowMs },
    sceneNumber(value, fallback) {
      const num = Number(value);
      return Number.isFinite(num) ? num : fallback;
    },
    window: { document },
  });
  vm.runInContext(
    controlsSource + "\nwindow.__managedFluidPoolResizeTest = { apply: sceneManagedFluidObjectApply };",
    context,
    { filename: "19b-scene-control-forms.js" },
  );

  const profile = {
    objects: {
      Sphere: {
        id: "float-sphere",
        label: "Sphere",
        objectKind: "sphere",
        objectSubtype: "sphere",
        objectX: 0.7,
        objectY: -0.75,
        objectZ: 0,
        objectRadius: 0.25,
        objectHalfSizeX: 0,
        objectHalfSizeY: 0,
        objectHalfSizeZ: 0,
        buoyancyRadius: 0.25,
        floorClearance: 0.25,
        xLimitRadius: 0.25,
        zLimitRadius: 0.25,
        meshYOffset: 0,
        objectDisplacementScale: 1,
        objectDisplacementSpheres: [],
      },
    },
  };
  const attrs = {};
  const form = {
    dataset: {},
    elements: {
      paused: { checked: false, disabled: false },
      object: { value: "Sphere", disabled: false },
      gravity: { checked: false, disabled: false },
      densityEnabled: { checked: false, disabled: false },
      density: { value: "0.9", disabled: false },
      poolShape: { value: "Box", disabled: false },
      cornerRadius: { value: "0.1", max: "1", disabled: false },
      poolWidth: { value: "1", disabled: false },
      poolHeight: { value: "1", disabled: false },
      poolLength: { value: "1", disabled: false },
      followCamera: { checked: false, disabled: false },
    },
    getAttribute(name) {
      if (name === "data-gosx-scene3d-control-data") return JSON.stringify(profile);
      if (name === "data-gosx-scene3d-control-subject") return "water-main";
      return "";
    },
    setAttribute(name, value) { attrs[name] = String(value); },
    querySelector() { return null; },
    closest() { return null; },
  };
  const sceneState = { waterSystems: [{ id: "water-main" }], models: [] };
  let applied = [];
  const applyCommands = commands => { applied = commands; };
  const waterPayload = () => applied.find(command => command.kind === 6).data.waterSystems[0];

  assert.equal(context.window.__managedFluidPoolResizeTest.apply(form, sceneState, applyCommands, {}), true);
  let water = waterPayload();
  assert.equal(water.poolShape, "Box");
  assert.equal(water.objectX, 0.7);
  assert.equal(water.objectY, -0.75);
  assert.equal(water.objectPreviousSet, false);

  form.__gosxScene3DFluidObjectState.objects.Sphere.velocity.y = -3;
  nowMs = 50;
  form.elements.gravity.checked = true;
  form.elements.poolShape.value = "Rounded Box";
  form.elements.poolWidth.value = "0.5";
  form.elements.poolHeight.value = "0.3";
  form.elements.poolLength.value = "0.5";
  form.elements.cornerRadius.value = "0.45";

  assert.equal(context.window.__managedFluidPoolResizeTest.apply(form, sceneState, applyCommands, {}), true);
  water = waterPayload();
  assert.equal(water.poolShape, "Rounded Box");
  assert.equal(water.poolWidth, 0.5);
  assert.equal(water.poolHeight, 0.3);
  assert.equal(water.poolLength, 0.5);
  assert.equal(water.objectX, 0.25);
  assert.ok(Math.abs(water.objectY - -0.05) < 1e-9);
  assert.equal(water.objectPreviousSet, false);
  assert.equal(water.objectPreviousX, 0);
  assert.equal(water.objectPreviousY, 0);
  assert.equal(water.objectPreviousZ, 0);
  assert.equal(form.__gosxScene3DFluidObjectState.objects.Sphere.velocity.y, 0);
});


test("Scene3D managed fluid dragging threads the pre-drag position into the water displacement payload", () => {
  const controlsSource = fs.readFileSync(path.join(__dirname, "bootstrap-src", "19b-scene-control-forms.js"), "utf8");

  // Source-shape guard: the drag handler must hand its pre-drag snapshot to the
  // next objectStep() via the pendingPrevious mechanism. The old code wrote it
  // to objectState.previousPosition — a dead field nothing reads — so 0% of a
  // drag's delta ever reached the displacement kernel (no splash on drag).
  const dragBodyStart = controlsSource.indexOf("function sceneManagedFluidObjectDragObject");
  assert.ok(dragBodyStart > 0, "drag handler must exist");
  const dragBody = controlsSource.slice(dragBodyStart, controlsSource.indexOf("\n  function ", dragBodyStart + 1));
  assert.match(dragBody, /objectState\.pendingPrevious = sceneManagedFluidObjectCopyVec3\(objectState\.position\)/);
  assert.doesNotMatch(dragBody, /objectState\.previousPosition\s*=/);

  // Behavioral: a drag-shaped mutation (pendingPrevious snapshot + in-place
  // position update, exactly what the fixed handler performs) must surface as
  // objectPreviousSet with the full pre-drag position in the water payload.
  const document = {
    documentElement: { setAttribute() {} },
    getElementById() { return null; },
    querySelector() { return null; },
  };
  let nowMs = 0;
  const context = vm.createContext({
    Date,
    JSON,
    Math,
    Number,
    SCENE_CMD_SET_TRANSFORM: 2,
    SCENE_CMD_SET_PARTICLES: 6,
    SCENE_CMD_SET_MODELS: 10,
    document,
    performance: { now: () => nowMs },
    sceneNumber(value, fallback) {
      const num = Number(value);
      return Number.isFinite(num) ? num : fallback;
    },
    window: { document },
  });
  vm.runInContext(
    controlsSource + "\nwindow.__managedFluidDragTest = { apply: sceneManagedFluidObjectApply, objectState: sceneManagedFluidObjectObjectState };",
    context,
    { filename: "19b-scene-control-forms.js" },
  );

  const duck = {
    id: "float-duck",
    label: "Rubber Duck",
    objectKind: "compound",
    objectSubtype: "duck",
    objectX: 0.4,
    objectY: -0.735,
    objectZ: -0.2,
    objectRadius: 0.25,
    objectHalfSizeX: 0,
    objectHalfSizeY: 0,
    objectHalfSizeZ: 0,
    buoyancyRadius: 0.31,
    floorClearance: 0.265,
    xLimitRadius: 0.25,
    zLimitRadius: 0.25,
    meshYOffset: 0,
    objectDisplacementScale: 0.15,
    objectDisplacementSpheres: [
      { offsetX: 0, offsetY: 0, offsetZ: 0, radius: 0.15 },
      { offsetX: 0, offsetY: 0.1, offsetZ: 0.1, radius: 0.08 },
      { offsetX: 0, offsetY: -0.08, offsetZ: -0.05, radius: 0.1 },
    ],
  };
  const profile = { objects: { Duck: duck } };
  const form = {
    dataset: {},
    elements: {
      paused: { checked: false, disabled: false },
      object: { value: "Duck", disabled: false },
      gravity: { checked: false, disabled: false },
      densityEnabled: { checked: false, disabled: false },
      density: { value: "0.9", disabled: false },
      poolShape: { value: "Box", disabled: false },
      cornerRadius: { value: "0.1", max: "1", disabled: false },
      poolWidth: { value: "1", disabled: false },
      poolHeight: { value: "1", disabled: false },
      poolLength: { value: "1", disabled: false },
      followCamera: { checked: false, disabled: false },
    },
    getAttribute(name) {
      if (name === "data-gosx-scene3d-control-data") return JSON.stringify(profile);
      if (name === "data-gosx-scene3d-control-subject") return "water-main";
      return "";
    },
    setAttribute(name, value) { this.dataset[name] = String(value); },
    querySelector() { return null; },
    closest() { return null; },
  };
  const sceneState = { waterSystems: [{ id: "water-main" }], models: [] };
  let applied = [];
  const applyCommands = commands => { applied = commands; };
  const waterPayload = () => applied.find(command => command.kind === 6).data.waterSystems[0];

  nowMs = 500;
  assert.equal(context.window.__managedFluidDragTest.apply(form, sceneState, applyCommands, {}), true);
  const controlState = form.__gosxScene3DFluidObjectState;
  const objectState = context.window.__managedFluidDragTest.objectState(controlState, duck);
  const preDrag = { x: objectState.position.x, y: objectState.position.y, z: objectState.position.z };

  const delta = { x: 0.3, y: 0.05, z: -0.2 };
  if (!objectState.pendingPrevious) {
    objectState.pendingPrevious = { x: objectState.position.x, y: objectState.position.y, z: objectState.position.z };
  }
  objectState.position.x += delta.x;
  objectState.position.y += delta.y;
  objectState.position.z += delta.z;
  objectState.velocity = { x: 0, y: 0, z: 0 };

  nowMs = 501; // fast mousemove: 1ms later
  assert.equal(context.window.__managedFluidDragTest.apply(form, sceneState, applyCommands, {}), true);
  const water = waterPayload();
  assert.equal(water.objectPreviousSet, true, "drag delta must reach the water displacement payload");
  assert.ok(Math.abs(water.objectPreviousX - preDrag.x) < 1e-9);
  assert.ok(Math.abs(water.objectPreviousY - preDrag.y) < 1e-9);
  assert.ok(Math.abs(water.objectPreviousZ - preDrag.z) < 1e-9);
  const capturedMag = Math.hypot(
    water.objectX - water.objectPreviousX,
    water.objectY - water.objectPreviousY,
    water.objectZ - water.objectPreviousZ,
  );
  const dragMag = Math.hypot(delta.x, delta.y, delta.z);
  assert.ok(capturedMag > dragMag * 0.9, `captured ${capturedMag} of drag ${dragMag}`);
});


test("Scene3D managed fluid controls queue every drop event in a fast pointer burst (water-parity/p6 Fix 1)", () => {
  const controlsSource = fs.readFileSync(path.join(__dirname, "bootstrap-src", "19b-scene-control-forms.js"), "utf8");

  // Source-shape guard: the queue must be a bounded ARRAY push (mirroring
  // the existing objectDisplacementEvents 12-entry pattern at
  // sceneManagedFluidObjectQueueObjectDisplacementEvent), not the old
  // single-slot scalar overwrite -- a fast pointer stroke fires one
  // pointermove DOM event per intermediate position, and a scalar slot
  // would silently discard all but the last one.
  assert.match(controlsSource, /controlState\.dropEvents\.push\(/);
  assert.match(controlsSource, /controlState\.dropEvents = controlState\.dropEvents\.slice\(controlState\.dropEvents\.length - 16\)/);
  assert.match(controlsSource, /next\.dropEvents = dropEvents\.map\(sceneManagedFluidObjectClone\)/);

  const document = {
    documentElement: { setAttribute() {} },
    getElementById() { return null; },
    querySelector() { return null; },
  };
  const context = vm.createContext({
    Date,
    JSON,
    Math,
    Number,
    SCENE_CMD_SET_TRANSFORM: 2,
    SCENE_CMD_SET_PARTICLES: 6,
    SCENE_CMD_SET_MODELS: 10,
    document,
    performance: { now: () => 0 },
    sceneNumber(value, fallback) {
      const num = Number(value);
      return Number.isFinite(num) ? num : fallback;
    },
    window: { document },
  });
  vm.runInContext(
    controlsSource + "\nwindow.__managedFluidDropBurstTest = { queueDrop: sceneManagedFluidObjectQueueDrop };",
    context,
    { filename: "19b-scene-control-forms.js" },
  );

  const canvas = {
    tagName: "canvas",
    getBoundingClientRect() { return { left: 0, top: 0, width: 800, height: 600 }; },
  };
  const form = {
    dataset: {},
    elements: {
      paused: { checked: false, disabled: false },
      object: { value: "None", disabled: false },
      poolShape: { value: "Box", disabled: false },
      cornerRadius: { value: "0", max: "1", disabled: false },
      poolWidth: { value: "1", disabled: false },
      poolHeight: { value: "1", disabled: false },
      poolLength: { value: "1", disabled: false },
      followCamera: { checked: false, disabled: false },
    },
    getAttribute(name) {
      if (name === "data-gosx-scene3d-control-data") return JSON.stringify({});
      if (name === "data-gosx-scene3d-control-subject") return "water-main";
      return "";
    },
    setAttribute(name, value) { this.dataset[name] = String(value); },
    querySelector() { return null; },
    closest() { return null; },
  };
  const sceneState = { waterSystems: [{ id: "water-main" }], models: [] };
  let applied = [];
  const applyCommands = commands => { applied = commands; };
  const waterPayload = () => applied.find(command => command.kind === 6).data.waterSystems[0];

  const queueDrop = context.window.__managedFluidDropBurstTest.queueDrop;
  // Fire 20 pointermove-shaped events in one tight synchronous burst --
  // more than a fast drag needs, and enough to exercise the 16-entry cap.
  for (let i = 0; i < 20; i++) {
    const event = { clientX: 100 + i, clientY: 200 };
    assert.equal(queueDrop(form, canvas, sceneState, applyCommands, event, {}), true, `queueDrop must succeed for event ${i}`);
  }

  const water = waterPayload();
  assert.ok(Array.isArray(water.dropEvents), "a burst must emit a dropEvents array");
  assert.equal(water.dropEvents.length, 16, "the queue must cap at 16 like objectDisplacementEvents");
  // Sliding window: the oldest 4 of 20 pushes (ids 1-4) must have been
  // evicted, leaving ids 5-20 -- every one of them, not just the latest.
  assert.equal(water.dropEvents[0].id, 5);
  assert.equal(water.dropEvents[water.dropEvents.length - 1].id, 20);
  for (let i = 0; i < water.dropEvents.length; i++) {
    assert.equal(water.dropEvents[i].id, i + 5, "every queued id in the window must be present and in order");
  }
  // Scalar back-compat fields (still consumed by any caller reading the
  // single-shot dropEventID/dropX/dropZ fields) must mirror the LATEST
  // event, not the first or a stale one.
  const latest = water.dropEvents[water.dropEvents.length - 1];
  assert.equal(water.dropEventID, 20);
  assert.equal(water.dropX, latest.x);
  assert.equal(water.dropZ, latest.z);
});


test("Scene3D managed fluid controls queue outgoing object displacement when selection becomes None", () => {
  const controlsSource = fs.readFileSync(path.join(__dirname, "bootstrap-src", "19b-scene-control-forms.js"), "utf8");
  const document = {
    documentElement: { setAttribute() {} },
    getElementById() { return null; },
    querySelector() { return null; },
  };
  const context = vm.createContext({
    Date,
    JSON,
    Math,
    Number,
    SCENE_CMD_SET_TRANSFORM: 2,
    SCENE_CMD_SET_PARTICLES: 6,
    SCENE_CMD_SET_MODELS: 10,
    document,
    performance: { now: () => 0 },
    sceneNumber(value, fallback) {
      const num = Number(value);
      return Number.isFinite(num) ? num : fallback;
    },
    window: { document },
  });
  vm.runInContext(
    controlsSource + "\nwindow.__managedFluidObjectExitTest = { apply: sceneManagedFluidObjectApply };",
    context,
    { filename: "19b-scene-control-forms.js" },
  );

  const profile = {
    hiddenY: 10,
    inactiveY: 10,
    objects: {
      Sphere: {
        id: "float-sphere",
        label: "Sphere",
        objectKind: "sphere",
        objectHitTest: "sphere",
        objectX: -0.4,
        objectY: -0.75,
        objectZ: 0.2,
        objectRadius: 0.25,
        objectHalfSizeX: 0,
        objectHalfSizeY: 0,
        objectHalfSizeZ: 0,
        buoyancyRadius: 0.25,
        floorClearance: 0.25,
        xLimitRadius: 0.25,
        zLimitRadius: 0.25,
        meshYOffset: 0,
        objectDisplacementScale: 1,
        objectDisplacementSpheres: [],
        mesh: { x: -0.4, y: -0.75, z: 0.2, visible: true },
      },
    },
  };
  const form = {
    dataset: {},
    elements: {
      paused: { checked: true, disabled: false },
      object: { value: "Sphere", disabled: false },
      gravity: { checked: false, disabled: false },
      densityEnabled: { checked: false, disabled: false },
      density: { value: "0.9", disabled: false },
      poolShape: { value: "Box", disabled: false },
      cornerRadius: { value: "0.1", max: "1", disabled: false },
      poolWidth: { value: "1", disabled: false },
      poolHeight: { value: "1", disabled: false },
      poolLength: { value: "1", disabled: false },
      followCamera: { checked: false, disabled: false },
    },
    getAttribute(name) {
      if (name === "data-gosx-scene3d-control-data") return JSON.stringify(profile);
      if (name === "data-gosx-scene3d-control-subject") return "water-main";
      return "";
    },
    setAttribute() {},
    querySelector() { return null; },
    closest() { return null; },
  };
  const sceneState = { waterSystems: [{ id: "water-main" }], models: [] };
  let applied = [];
  const applyCommands = commands => { applied = commands; };
  const waterPayload = () => applied.find(command => command.kind === 6).data.waterSystems[0];

  assert.equal(context.window.__managedFluidObjectExitTest.apply(form, sceneState, applyCommands, {}), true);
  assert.equal(waterPayload().activeObject, "Sphere");
  assert.equal(Array.isArray(waterPayload().objectDisplacementEvents), false);

  form.elements.object.value = "None";
  assert.equal(context.window.__managedFluidObjectExitTest.apply(form, sceneState, applyCommands, {}), true);
  const water = waterPayload();
  assert.equal(water.activeObject, "None");
  assert.equal(water.objectPreviousSet, false);
  assert.equal(water.objectDisplacementEvents.length, 1);
  const event = water.objectDisplacementEvents[0];
  assert.equal(event.id, 1);
  assert.equal(event.activeObject, "Sphere");
  assert.equal(event.objectKind, "sphere");
  assert.equal(event.objectPreviousSet, true);
  assert.equal(event.objectPreviousX, -0.4);
  assert.equal(event.objectPreviousY, -0.75);
  assert.equal(event.objectPreviousZ, 0.2);
  assert.equal(event.objectX, -0.4);
  assert.equal(event.objectY, 10);
  assert.equal(event.objectZ, 0.2);
  assert.equal(event.objectRadius, 0.25);
});


test("Scene3D managed fluid object hit policy matches upstream water objects", () => {
  const controlsSource = fs.readFileSync(path.join(__dirname, "bootstrap-src", "19b-scene-control-forms.js"), "utf8");
  const document = {
    documentElement: { setAttribute() {} },
    querySelector() { return null; },
  };
  const calls = { sphere: 0, box: 0, pick: 0 };
  let pickReturn = { x: 9, y: 8, z: 7 };
  const context = vm.createContext({
    Date,
    JSON,
    Math,
    Number,
    document,
    performance: { now: () => 0 },
    sceneNumber(value, fallback) {
      const num = Number(value);
      return Number.isFinite(num) ? num : fallback;
    },
    sceneScreenToRay() {
      return { origin: { x: 0, y: 0, z: 1 }, direction: { x: 0, y: 0, z: -1 } };
    },
    sceneRayIntersectSphere(_ray, center, radius) {
      calls.sphere++;
      return { x: center.x, y: center.y, z: center.z + radius, distance: 1 };
    },
    sceneRayIntersectAABB(_ray, min) {
      calls.box++;
      return { x: min.x, y: min.y, z: min.z, distance: 1 };
    },
    sceneRaycastPickGroup(_ray, objects) {
      calls.pick++;
      if (!pickReturn) return null;
      return {
        object: objects[0],
        distance: 0.5,
        point: pickReturn,
        worldPosition: pickReturn,
      };
    },
    window: { document },
  });
  vm.runInContext(
    controlsSource + "\nwindow.__managedFluidHitTest = { active: sceneManagedFluidObjectActiveObjectHit, mode: sceneManagedFluidObjectHitTestMode };",
    context,
    { filename: "19b-scene-control-forms.js" },
  );
  const api = context.window.__managedFluidHitTest;
  const profile = {
    objects: {
      TorusKnot: {
        id: "float-torus",
        label: "TorusKnot",
        objectKind: "compound",
        objectSubtype: "torusKnot",
        objectHitTest: "mesh",
        objectX: -0.4,
        objectY: -0.87,
        objectZ: 0.2,
        objectRadius: 0.31,
        buoyancyRadius: 0.31,
        floorClearance: 0.13,
        xLimitRadius: 0.31,
        zLimitRadius: 0.31,
        meshYOffset: 0,
      },
      "Rubber Duck": {
        id: "float-duck",
        label: "Rubber Duck",
        objectKind: "compound",
        objectSubtype: "duck",
        objectHitTest: "mesh",
        objectX: 0.4,
        objectY: -0.735,
        objectZ: -0.2,
        objectRadius: 0.25,
        buoyancyRadius: 0.25,
        floorClearance: 0.265,
        xLimitRadius: 0.25,
        zLimitRadius: 0.25,
        meshYOffset: 0,
      },
    },
  };
  const form = {};
  const canvas = {
    tagName: "canvas",
    getBoundingClientRect() { return { left: 0, top: 0, width: 100, height: 100 }; },
  };
  const sceneState = { camera: { x: 0, y: 0, z: 4, rotationX: 0, rotationY: 0, rotationZ: 0 } };
  const event = { clientX: 50, clientY: 50 };
  const bundle = {
    camera: sceneState.camera,
    meshObjects: [{ id: "float-torus" }],
    worldMeshPositions: [0],
    objects: [{ id: "float-duck" }],
    worldPositions: [0],
  };

  assert.equal(api.mode(profile.objects.TorusKnot), "mesh");
  const torusHit = api.active(form, canvas, sceneState, { object: "TorusKnot" }, profile, event, { getBundle() { return bundle; } });
  assert.deepEqual(torusHit.point, { x: 9, y: 8, z: 7 });
  assert.equal(calls.sphere, 0, "torus mesh hit must not be claimed by the bounding sphere fallback");
  assert.equal(calls.pick, 1);

  pickReturn = null;
  const torusMiss = api.active(form, canvas, sceneState, { object: "TorusKnot" }, profile, event, { getBundle() { return bundle; } });
  assert.equal(torusMiss, null);
  assert.equal(calls.sphere, 0, "torus mesh misses must stay misses like upstream Raycaster misses");
  assert.equal(calls.pick, 2);

  pickReturn = { x: 9, y: 8, z: 7 };
  calls.pick = 0;
  calls.sphere = 0;
  assert.equal(api.mode(profile.objects["Rubber Duck"]), "mesh");
  const duckHit = api.active(form, canvas, sceneState, { object: "Rubber Duck" }, profile, event, { getBundle() { return bundle; } });
  assert.deepEqual(duckHit.point, { x: 9, y: 8, z: 7 });
  assert.equal(calls.sphere, 0);
  assert.equal(calls.pick, 1, "duck must use exact mesh picking so nearby water remains paintable");
});


test("Scene3D managed fluid interactions stop camera inertia before consuming water events", () => {
  const controlsSource = fs.readFileSync(path.join(__dirname, "bootstrap-src", "19b-scene-control-forms.js"), "utf8");
  const document = {
    documentElement: { setAttribute() {} },
    querySelector() { return null; },
  };
  const context = vm.createContext({
    Date,
    JSON,
    Math,
    Number,
    document,
    performance: { now: () => 0 },
    sceneNumber(value, fallback) {
      const num = Number(value);
      return Number.isFinite(num) ? num : fallback;
    },
    sceneScreenToRay() {
      return { origin: { x: 0, y: 1, z: 1 }, direction: { x: 0, y: -1, z: -1 } };
    },
    sceneRayIntersectYPlane() {
      return { x: 0, y: 0, z: 0 };
    },
    window: { document },
  });
  vm.runInContext(
    controlsSource + "\nwindow.__managedFluidStartTest = { start: sceneManagedFluidObjectStartInteraction };",
    context,
    { filename: "19b-scene-control-forms.js" },
  );
  const api = context.window.__managedFluidStartTest;
  const profile = { interaction: { profile: "water-object-drop-orbit", pointerDrops: true }, objects: {} };
  const form = {
    elements: {
      paused: { checked: false, disabled: false },
      object: { value: "None", disabled: false },
      gravity: { checked: false, disabled: false },
      densityEnabled: { checked: false, disabled: false },
      density: { value: "0.9", disabled: false },
      poolShape: { value: "Box", disabled: false },
      cornerRadius: { value: "0.1", disabled: false },
      poolWidth: { value: "1", disabled: false },
      poolHeight: { value: "1", disabled: false },
      poolLength: { value: "1", disabled: false },
      followCamera: { checked: false, disabled: false },
    },
    getAttribute(name) {
      return name === "data-gosx-scene3d-control-data" ? JSON.stringify(profile) : null;
    },
  };
  const canvas = {
    tagName: "canvas",
    getBoundingClientRect() { return { left: 0, top: 0, width: 100, height: 100 }; },
  };
  const sceneState = { camera: { x: 0, y: 0, z: 4 }, waterSystems: [{ id: "water-main" }] };
  let stopCalls = 0;
  const mode = api.start(
    form,
    canvas,
    sceneState,
    { clientX: 50, clientY: 50 },
    { stopCameraInertia() { stopCalls++; return true; } },
  );
  assert.equal(mode, "AddDrops");
  assert.equal(stopCalls, 1);
  assert.equal(form.__gosxScene3DFluidObjectState.pointerMode, "AddDrops");
});


test("Scene3D managed fluid L key light direction points toward camera like upstream", () => {
  const controlsSource = fs.readFileSync(path.join(__dirname, "bootstrap-src", "19b-scene-control-forms.js"), "utf8");
  const document = { documentElement: { setAttribute() {} }, querySelector() { return null; } };
  const context = vm.createContext({
    Date,
    JSON,
    Math,
    Number,
    document,
    performance: { now: () => 0 },
    sceneNumber(value, fallback) { const num = Number(value); return Number.isFinite(num) ? num : fallback; },
    sceneRenderCamera(camera) { return camera; },
    sceneRotatePoint(point, rotationX, rotationY, rotationZ) {
      let x = point.x;
      let y = point.y;
      let z = point.z;
      const sinX = Math.sin(rotationX);
      const cosX = Math.cos(rotationX);
      let nextY = y * cosX - z * sinX;
      let nextZ = y * sinX + z * cosX;
      y = nextY;
      z = nextZ;
      const sinY = Math.sin(rotationY);
      const cosY = Math.cos(rotationY);
      let nextX = x * cosY + z * sinY;
      nextZ = -x * sinY + z * cosY;
      x = nextX;
      z = nextZ;
      const sinZ = Math.sin(rotationZ);
      const cosZ = Math.cos(rotationZ);
      nextX = x * cosZ - y * sinZ;
      nextY = x * sinZ + y * cosZ;
      return { x: nextX, y: nextY, z };
    },
    window: { document },
  });
  vm.runInContext(controlsSource + "\nwindow.__managedFluidLightTest = { light: sceneManagedFluidObjectCameraLightDirection, key: sceneManagedFluidObjectCameraLightKey, changed: sceneManagedFluidObjectLightCameraChanged, dragNormal: sceneManagedFluidObjectCameraDragNormal, zoom: sceneManagedFluidObjectZoomCameraByScale, wheel: sceneManagedFluidObjectZoomCameraByWheel };", context, { filename: "19b-scene-control-forms.js" });
  const api = context.window.__managedFluidLightTest;
  const pitched = api.light({ rotationX: Math.PI / 6, rotationY: 0, rotationZ: 0 });
  assert.ok(Math.abs(pitched.x - 0) < 0.000001, "pitched x=" + pitched.x);
  assert.ok(Math.abs(pitched.y - 0.5) < 0.000001, "pitched y=" + pitched.y);
  assert.ok(Math.abs(pitched.z + Math.sqrt(3) / 2) < 0.000001, "pitched z=" + pitched.z);
  const yawed = api.light({ rotationX: 0, rotationY: Math.PI / 2, rotationZ: 0 });
  assert.ok(Math.abs(yawed.x + 1) < 0.000001, "yawed x=" + yawed.x);
  assert.ok(Math.abs(yawed.y - 0) < 0.000001, "yawed y=" + yawed.y);
  assert.ok(Math.abs(yawed.z - 0) < 0.000001, "yawed z=" + yawed.z);
  const orbitPitched = api.light(null, { pitch: Math.PI / 6, yaw: 0 });
  assert.ok(Math.abs(orbitPitched.x - 0) < 0.000001, "orbit pitched x=" + orbitPitched.x);
  assert.ok(Math.abs(orbitPitched.y - 0.5) < 0.000001, "orbit pitched y=" + orbitPitched.y);
  assert.ok(Math.abs(orbitPitched.z + Math.sqrt(3) / 2) < 0.000001, "orbit pitched z=" + orbitPitched.z);
  const orbitYawed = api.light(null, { pitch: 0, yaw: Math.PI / 2 });
  assert.ok(Math.abs(orbitYawed.x - 1) < 0.000001, "orbit yawed x=" + orbitYawed.x);
  assert.ok(Math.abs(orbitYawed.y - 0) < 0.000001, "orbit yawed y=" + orbitYawed.y);
  assert.ok(Math.abs(orbitYawed.z - 0) < 0.000001, "orbit yawed z=" + orbitYawed.z);
  assert.equal(api.key({ rotationX: Math.PI / 6, rotationY: 0, rotationZ: 0 }), "0.523599|0.000000|0.000000");
  assert.equal(api.changed({ lightCameraKey: "" }, { camera: { rotationX: 0, rotationY: 0, rotationZ: 0 } }, {}), true);
  assert.equal(api.changed({ lightCameraKey: "0.000000|0.000000|0.000000" }, { camera: { rotationX: 0, rotationY: 0, rotationZ: 0 } }, {}), false);
  const normal = api.dragNormal(
    { x: 10, y: 20, z: 30, rotationX: 0, rotationY: Math.PI / 2, rotationZ: 0 },
    { x: 9, y: 19, z: 29 },
  );
  assert.ok(Math.abs(normal.x - 1) < 0.000001, "drag normal x=" + normal.x);
  assert.ok(Math.abs(normal.y - 0) < 0.000001, "drag normal y=" + normal.y);
  assert.ok(Math.abs(normal.z - 0) < 0.000001, "drag normal z=" + normal.z);
  const fallbackNormal = vm.runInContext(
    "(function(){ const saved = sceneRotatePoint; sceneRotatePoint = null; try { return window.__managedFluidLightTest.dragNormal({ x: 0, y: 0, z: 4, rotationX: 0, rotationY: 0, rotationZ: 0 }, { x: 0, y: 0, z: 0 }); } finally { sceneRotatePoint = saved; } })()",
    context,
  );
  assert.ok(Math.abs(fallbackNormal.z + 1) < 0.000001, "fallback drag normal z=" + fallbackNormal.z);
  let nextCamera = null;
  const zoomed = api.zoom({ camera: { x: 0, y: -0.5, z: 4, rotationX: 0, rotationY: 0, rotationZ: 0, fov: 45 } }, {
    getCamera() { return { x: 0, y: -0.5, z: 4, rotationX: 0, rotationY: 0, rotationZ: 0, fov: 45 }; },
    getControlTarget() { return { x: 0, y: -0.5, z: 0 }; },
    setCamera(camera) { nextCamera = camera; return true; },
  }, 0.5, {});
  assert.equal(zoomed, true);
  assert.ok(Math.abs(nextCamera.z - 2) < 0.000001, "zoomed z=" + nextCamera.z);
  nextCamera = null;
  const wheelZoomed = api.wheel({ camera: { x: 0, y: -0.5, z: 4, rotationX: 0, rotationY: 0, rotationZ: 0, fov: 45 } }, {
    getCamera() { return { x: 0, y: -0.5, z: 4, rotationX: 0, rotationY: 0, rotationZ: 0, fov: 45 }; },
    getControlTarget() { return { x: 0, y: -0.5, z: 0 }; },
    setCamera(camera) { nextCamera = camera; return true; },
  }, { deltaY: Math.log(2) * 1000 }, {});
  assert.equal(wheelZoomed, true);
  assert.ok(Math.abs(nextCamera.z - 8) < 0.000001, "wheel zoomed z=" + nextCamera.z);
});

// === P3 camera-input signal: engine applies camera pushed via shared signal ===
test("P3 cameraInputSignal: engine applies camera from shared signal", async () => {
  const mount = new FakeElement("div", null);
  mount.id = "scene-camera-signal-root";

  // Uses orbit controls so getCamera() goes through the orbit state (which IS updated
  // by applySceneControlsCamera) rather than latestBundle.camera (which stays fixed
  // at the initial render-engine return value).
  const env = createContext({
    elements: [mount],
    enableWebGL: true,
    disableCanvas2D: true,
    fetchRoutes: {
      "/runtime.wasm": { bytes: [0, 97, 115, 109] },
      "/scene-camsig-program.json": { text: '{"name":"SceneCamSig"}' },
    },
    manifest: {
      runtime: { path: "/runtime.wasm" },
      engines: [
        {
          id: "gosx-engine-camsig",
          component: "GoSXScene3D",
          kind: "surface",
          mountId: "scene-camera-signal-root",
          runtime: "shared",
          programRef: "/scene-camsig-program.json",
          props: {
            width: 640,
            height: 360,
            background: "#000",
            controls: "orbit",
            controlTarget: { x: 0, y: 0, z: 0 },
            camera: { x: 0, y: 0, z: 6, fov: 72 },
            cameraInputSignal: "$camera",
          },
        },
      ],
    },
    onHydrateEngine: () => "[]",
    onRenderEngine: () => JSON.stringify({
      background: "#000",
      camera: { x: 0, y: 0, z: 6, fov: 72 },
      positions: [],
      colors: [],
      vertexCount: 0,
      worldPositions: [],
      worldColors: [],
      worldVertexCount: 0,
      materials: [],
      objects: [],
      objectCount: 0,
    }),
  });

  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();

  let mounted = env.context.__gosx.engines.get("gosx-engine-camsig");
  for (let attempt = 0; attempt < 5 && !mounted; attempt += 1) {
    await flushAsyncWork();
    mounted = env.context.__gosx.engines.get("gosx-engine-camsig");
  }
  assert.ok(mounted, "expected gosx-engine-camsig to mount");

  const initialCamera = mounted.handle.getCamera();
  assert.ok(Math.abs(initialCamera.z - 6) < 0.1, "initial z=" + initialCamera.z);

  // Push a camera via the shared signal (wrapped in {camera:...} envelope)
  env.context.__gosx_notify_shared_signal("$camera", JSON.stringify({ camera: { x: 1, y: 2, z: 9, fov: 50 } }));
  await flushAsyncWork();

  const updatedCamera = mounted.handle.getCamera();
  assert.ok(Math.abs(updatedCamera.z - 9) < 0.5, "expected z~9 after signal, got z=" + updatedCamera.z);
  assert.equal(env.consoleLogs.error.length, 0);
});

// === P3 selectionInputSignal: engine applies object selection from shared signal ===
test("P3 selectionInputSignal: engine selects object from shared signal", async () => {
  const mount = new FakeElement("div", null);
  mount.id = "scene-selection-signal-root";

  const env = createContext({
    elements: [mount],
    enableWebGL: true,
    disableCanvas2D: true,
    fetchRoutes: {
      "/runtime.wasm": { bytes: [0, 97, 115, 109] },
      "/scene-selsig-program.json": { text: '{"name":"SceneSelSig"}' },
    },
    manifest: {
      runtime: { path: "/runtime.wasm" },
      engines: [
        {
          id: "gosx-engine-selsig",
          component: "GoSXScene3D",
          kind: "surface",
          mountId: "scene-selection-signal-root",
          runtime: "shared",
          programRef: "/scene-selsig-program.json",
          props: {
            width: 640,
            height: 360,
            background: "#000",
            camera: { x: 0, y: 0, z: 6, fov: 72 },
            selectionInputSignal: "$selection",
          },
        },
      ],
    },
    onHydrateEngine: () => "[]",
    onRenderEngine: () => JSON.stringify({
      background: "#000",
      camera: { x: 0, y: 0, z: 6, fov: 72 },
      positions: [],
      colors: [],
      vertexCount: 0,
      worldPositions: [-0.5, -0.5, 0.5, 0.5, -0.5, 0.5],
      worldColors: [0.5, 0.5, 0.5, 1, 0.5, 0.5, 0.5, 1],
      worldVertexCount: 2,
      materials: [{ kind: "flat", color: "#888", opacity: 1, wireframe: false, blendMode: "opaque", emissive: 0 }],
      objects: [
        {
          id: "cube",
          kind: "box",
          pickable: true,
          materialIndex: 0,
          vertexOffset: 0,
          vertexCount: 2,
          static: false,
          bounds: { minX: -0.5, minY: -0.5, minZ: 0.5, maxX: 0.5, maxY: -0.5, maxZ: 0.5 },
        },
      ],
      objectCount: 1,
    }),
  });

  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();

  let mounted = env.context.__gosx.engines.get("gosx-engine-selsig");
  for (let attempt = 0; attempt < 5 && !mounted; attempt += 1) {
    await flushAsyncWork();
    mounted = env.context.__gosx.engines.get("gosx-engine-selsig");
  }
  assert.ok(mounted, "expected gosx-engine-selsig to mount");

  const rendersBefore = env.engineRenderCalls.length;

  // Push a selection signal with a real object id
  env.context.__gosx_notify_shared_signal("$selection", JSON.stringify("cube"));
  await flushAsyncWork();

  // A render should have been scheduled
  assert.ok(env.engineRenderCalls.length > rendersBefore, "expected a render after selection signal");
  assert.equal(env.consoleLogs.error.length, 0);
});

// === P6 gizmoInputSignal: engine switches TransformControls mode live ===
test("P6 gizmoInputSignal: engine toggles gizmo ring visibility from shared signal", async () => {
  const mount = new FakeElement("div", null);
  mount.id = "scene-gizmo-signal-root";

  const env = createContext({
    elements: [mount],
    enableWebGL: true,
    disableCanvas2D: true,
    fetchRoutes: {
      "/runtime.wasm": { bytes: [0, 97, 115, 109] },
      "/scene-gizmosig-program.json": { text: '{"name":"SceneGizmoSig"}' },
    },
    manifest: {
      runtime: { path: "/runtime.wasm" },
      engines: [
        {
          id: "gosx-engine-gizmosig",
          component: "GoSXScene3D",
          kind: "surface",
          mountId: "scene-gizmo-signal-root",
          runtime: "shared",
          programRef: "/scene-gizmosig-program.json",
          props: {
            width: 640,
            height: 360,
            background: "#000",
            camera: { x: 0, y: 0, z: 6, fov: 72 },
            gizmoInputSignal: "$gizmo",
          },
        },
      ],
    },
    onHydrateEngine: () => "[]",
    onRenderEngine: () => JSON.stringify({
      background: "#000",
      camera: { x: 0, y: 0, z: 6, fov: 72 },
      positions: [],
      colors: [],
      vertexCount: 0,
      worldPositions: [-0.5, -0.5, 0.5, 0.5, -0.5, 0.5],
      worldColors: [0.5, 0.5, 0.5, 1, 0.5, 0.5, 0.5, 1],
      worldVertexCount: 2,
      materials: [{ kind: "line-basic", color: "#facc15", opacity: 1, wireframe: false, blendMode: "opaque", emissive: 0 }],
      objects: [
        {
          id: "kiln-transform-controls-ring",
          kind: "lines",
          pickable: false,
          visible: false,
          gizmoRing: true,
          materialIndex: 0,
          vertexOffset: 0,
          vertexCount: 2,
          static: false,
          bounds: { minX: -0.5, minY: -0.5, minZ: 0.5, maxX: 0.5, maxY: -0.5, maxZ: 0.5 },
        },
      ],
      objectCount: 1,
    }),
  });

  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();

  let mounted = env.context.__gosx.engines.get("gosx-engine-gizmosig");
  for (let attempt = 0; attempt < 5 && !mounted; attempt += 1) {
    await flushAsyncWork();
    mounted = env.context.__gosx.engines.get("gosx-engine-gizmosig");
  }
  assert.ok(mounted, "expected gosx-engine-gizmosig to mount");

  const rendersBefore = env.engineRenderCalls.length;

  // Push "rotate" — the baked-hidden ring helper should become visible without
  // a page reload / new SSR round-trip.
  env.context.__gosx_notify_shared_signal("$gizmo", JSON.stringify("rotate"));
  await flushAsyncWork();

  assert.ok(env.engineRenderCalls.length > rendersBefore, "expected a render after gizmo signal");
  assert.equal(env.consoleLogs.error.length, 0);

  const rendersAfterRotate = env.engineRenderCalls.length;

  // Switching back to "translate" should also re-render (ring hides again).
  env.context.__gosx_notify_shared_signal("$gizmo", JSON.stringify("translate"));
  await flushAsyncWork();

  assert.ok(env.engineRenderCalls.length > rendersAfterRotate, "expected a render after switching back to translate");
  assert.equal(env.consoleLogs.error.length, 0);
});

// === P7 gizmoHelper: TransformControls helper group repositions live onto
// the selected object and switches form per gizmo-mode signal ===
//
// Exercises the exact mechanics the mount layer's syncMountedSceneGizmoHelpers
// (20-scene-mount.js) performs, via the same public primitives it calls
// (api.applySceneCommands with kind:2/SET_TRANSFORM routes to
// applySceneObjectPatch — the same function the mount-layer sink uses) — a
// full engine mount isn't needed to exercise this since the mount layer's
// job is entirely "patch already-mounted sceneState objects", and that
// patching + the resulting render-bundle inclusion/exclusion is exactly what
// this test verifies end to end (createSceneState -> patch -> createSceneRenderBundle).
test("P7 gizmoHelper: helper group hides/repositions/switches form via selection + gizmo-mode state", async () => {
  const env = createContext({});
  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();

  const api = env.context.__gosx_scene3d_api;
  const SET_TRANSFORM = 2;

  const state = api.createSceneState({
    scene: {
      objects: [
        { id: "cube-1", kind: "box", x: 5, y: 2, z: -3, rotationY: 0.3, size: 1 },
        // Mirrors scene.go's lowerGizmoAxesHelper (translate form).
        { id: "gizmo-x", kind: "lines", points: [{ x: 0, y: 0, z: 0 }, { x: 1.5, y: 0, z: 0 }], visible: false, gizmoHelper: true, gizmoFormMode: "translate" },
        // Mirrors the rotate-mode ring.
        { id: "gizmo-ring", kind: "lines", points: [{ x: 1.5, y: 0, z: 0 }, { x: 0, y: 1.5, z: 0 }], visible: false, gizmoHelper: true, gizmoFormMode: "rotate", gizmoRing: true },
        // Mirrors lowerGizmoScaleHandles (scale form).
        { id: "gizmo-scale-x", kind: "lines", points: [{ x: -0.1, y: -0.1, z: -0.1 }, { x: 0.1, y: 0.1, z: 0.1 }], visible: false, gizmoHelper: true, gizmoFormMode: "scale" },
      ],
    },
  });

  function syncHelpers(selectedID, mode) {
    const objects = api.sceneStateObjects(state);
    const target = selectedID ? objects.find((o) => o.id === selectedID) : null;
    for (const obj of objects) {
      if (!obj.gizmoHelper) continue;
      const visible = Boolean(target) && obj.gizmoFormMode === mode;
      const data = { visible };
      if (target) {
        data.x = target.x;
        data.y = target.y;
        data.z = target.z;
        data.rotationX = target.rotationX;
        data.rotationY = target.rotationY;
        data.rotationZ = target.rotationZ;
      }
      api.applySceneCommands(state, [{ kind: SET_TRANSFORM, objectId: obj.id, data }]);
    }
  }

  function bundleObjectIDs() {
    const bundle = api.createSceneRenderBundle(
      640, 360, "#000",
      { x: 0, y: 0, z: 6, fov: 60 },
      api.sceneStateObjectsWithMaterials(state),
      [], [], [], [], {}, 0, [], [], [], [], [], 0, false,
    );
    return bundle.objects.map((o) => o.id);
  }

  // Nothing selected yet: every helper piece stays hidden and excluded from
  // the render bundle regardless of mode (P7 DoD: "deselect -> hidden").
  syncHelpers("", "translate");
  let objects = api.sceneStateObjects(state);
  assert.ok(objects.filter((o) => o.gizmoHelper).every((o) => o.visible === false), "no helper piece should be visible with nothing selected");
  let ids = bundleObjectIDs();
  assert.ok(!ids.includes("gizmo-x") && !ids.includes("gizmo-ring") && !ids.includes("gizmo-scale-x"), "hidden helper pieces must not reach the render bundle");

  // Select cube-1 in translate mode: only the translate axis shows, and it —
  // along with every other (still-hidden) helper piece — is repositioned
  // onto the selected object's world transform.
  syncHelpers("cube-1", "translate");
  objects = api.sceneStateObjects(state);
  const axisX = objects.find((o) => o.id === "gizmo-x");
  const ring = objects.find((o) => o.id === "gizmo-ring");
  const scaleX = objects.find((o) => o.id === "gizmo-scale-x");
  assert.equal(axisX.visible, true, "translate axis must be visible in translate mode with a selection");
  assert.equal(ring.visible, false, "ring must stay hidden in translate mode");
  assert.equal(scaleX.visible, false, "scale handle must stay hidden in translate mode");
  for (const obj of [axisX, ring, scaleX]) {
    assert.equal(obj.x, 5, obj.id + ".x should track the selected object");
    assert.equal(obj.y, 2, obj.id + ".y should track the selected object");
    assert.equal(obj.z, -3, obj.id + ".z should track the selected object");
    assert.ok(Math.abs(obj.rotationY - 0.3) < 1e-9, obj.id + ".rotationY should track the selected object");
  }
  ids = bundleObjectIDs();
  assert.ok(ids.includes("gizmo-x"), "visible translate axis must reach the render bundle");
  assert.ok(ids.includes("cube-1"));
  assert.ok(!ids.includes("gizmo-ring"), "hidden ring must be excluded from the render bundle (visible:false line-kind fix)");
  assert.ok(!ids.includes("gizmo-scale-x"), "hidden scale handle must be excluded from the render bundle");

  // Switch to rotate mode (still selected): only the ring shows now.
  syncHelpers("cube-1", "rotate");
  objects = api.sceneStateObjects(state);
  assert.equal(objects.find((o) => o.id === "gizmo-x").visible, false, "translate axis hides when mode switches to rotate");
  assert.equal(objects.find((o) => o.id === "gizmo-ring").visible, true, "ring shows in rotate mode");
  assert.equal(objects.find((o) => o.id === "gizmo-scale-x").visible, false);
  ids = bundleObjectIDs();
  assert.ok(ids.includes("gizmo-ring") && !ids.includes("gizmo-x") && !ids.includes("gizmo-scale-x"));

  // Switch to scale mode (still selected): only the scale handle shows.
  syncHelpers("cube-1", "scale");
  objects = api.sceneStateObjects(state);
  assert.equal(objects.find((o) => o.id === "gizmo-x").visible, false);
  assert.equal(objects.find((o) => o.id === "gizmo-ring").visible, false);
  assert.equal(objects.find((o) => o.id === "gizmo-scale-x").visible, true, "scale handle shows in scale mode");

  // Deselect: everything hides again regardless of mode (form stays "scale").
  syncHelpers("", "scale");
  objects = api.sceneStateObjects(state);
  assert.ok(objects.filter((o) => o.gizmoHelper).every((o) => o.visible === false), "deselecting hides every helper piece");
  ids = bundleObjectIDs();
  assert.ok(!ids.includes("gizmo-x") && !ids.includes("gizmo-ring") && !ids.includes("gizmo-scale-x"));

  assert.equal(env.consoleLogs.error.length, 0);
});

// === P7 sceneGizmoTargetAnchor: world-baked-vertex mesh objects resolve to
// their real position, not the origin ===
//
// Regression test for a real bug found during browser verification: kiln's
// scene objects (editor_viewport.go's sceneMeshNodes) lower as BufferGeometry
// with WORLD-SPACE vertex positions baked directly into vertices.positions —
// x/y/z stay 0 on those objects (see normalizeSceneObject). Naively reading
// target.x/y/z (as translate/rotate/scale-mode-driven objects would expect)
// anchors the gizmo helper group at the origin for every kiln object
// regardless of its real position. sceneGizmoTargetAnchor must detect the
// baked-vertex case and fall back to the vertex bounding-box center instead.
test("P7 sceneGizmoTargetAnchor: falls back to vertex bounding-box center for world-baked mesh objects", async () => {
  const env = createContext({});
  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();

  const api = env.context.__gosx_scene3d_api;
  assert.equal(typeof api.sceneGizmoTargetAnchor, "function", "expected sceneGizmoTargetAnchor to be exposed on __gosx_scene3d_api");

  // Ordinary transform-driven object: x/y/z directly is the anchor.
  const transformObject = api.normalizeSceneObject({ id: "cube", kind: "box", x: 5, y: 2, z: -3, rotationY: 0.3 }, "cube", null);
  const transformAnchor = api.sceneGizmoTargetAnchor(transformObject);
  assert.equal(transformAnchor.x, 5);
  assert.equal(transformAnchor.y, 2);
  assert.equal(transformAnchor.z, -3);
  assert.ok(Math.abs(transformAnchor.rotationY - 0.3) < 1e-9);

  // kiln-shaped world-baked mesh object: x/y/z are 0 (the kiln lowering
  // path never sets them), but vertices.positions carries the real
  // world-space triangle data — a wall centered at (-3.25, 1.5, 0), mirroring
  // exactly what was observed live against a running kiln workspace.
  const bakedObject = api.normalizeSceneObject({
    id: "wall",
    kind: "gltf-mesh",
    vertices: {
      count: 3,
      positions: [
        -4.25, 1.0, -0.5,
        -2.25, 1.0, -0.5,
        -3.25, 2.0, 0.5,
      ],
    },
  }, "wall", null);
  assert.equal(bakedObject.x, 0, "sanity: kiln-shaped objects leave x at 0");
  assert.equal(bakedObject.y, 0, "sanity: kiln-shaped objects leave y at 0");
  const bakedAnchor = api.sceneGizmoTargetAnchor(bakedObject);
  assert.ok(Math.abs(bakedAnchor.x - -3.25) < 1e-9, "expected anchor.x to be the vertex bounding-box center, got " + bakedAnchor.x);
  assert.ok(Math.abs(bakedAnchor.y - 1.5) < 1e-9, "expected anchor.y to be the vertex bounding-box center, got " + bakedAnchor.y);
  assert.ok(Math.abs(bakedAnchor.z - 0) < 1e-9, "expected anchor.z to be the vertex bounding-box center, got " + bakedAnchor.z);

  assert.equal(env.consoleLogs.error.length, 0);
});

// === P5 cursorOutputSignal: normalized pointer published into signal ===
test("P5 cursorOutputSignal: pointermove publishes normalized cursor position", async () => {
  const mount = new FakeElement("div", null);
  mount.id = "scene-cursor-signal-root";

  const env = createContext({
    elements: [mount],
    fetchRoutes: {
      "/runtime.wasm": { bytes: [0, 97, 115, 109] },
      "/scene-cursorsig-program.json": { text: '{"name":"SceneCursorSig"}' },
    },
    manifest: {
      runtime: { path: "/runtime.wasm" },
      engines: [
        {
          id: "gosx-engine-cursorsig",
          component: "GoSXScene3D",
          kind: "surface",
          mountId: "scene-cursor-signal-root",
          runtime: "shared",
          programRef: "/scene-cursorsig-program.json",
          props: {
            width: 640,
            height: 360,
            background: "#000",
            camera: { x: 0, y: 0, z: 6, fov: 72 },
            cursorOutputSignal: "$cursor",
          },
        },
      ],
    },
    onHydrateEngine: () => "[]",
    onRenderEngine: () => JSON.stringify({
      background: "#000",
      camera: { x: 0, y: 0, z: 6, fov: 72 },
      positions: [],
      colors: [],
      vertexCount: 0,
      worldPositions: [-2, -1, 0.1, 2, -1, 0.1],
      worldColors: [0.5, 0.5, 0.5, 1, 0.5, 0.5, 0.5, 1],
      worldVertexCount: 2,
      materials: [{ kind: "flat", color: "#888", opacity: 1, wireframe: false, blendMode: "opaque", emissive: 0 }],
      objects: [
        {
          id: "floor",
          kind: "plane",
          pickable: false,
          materialIndex: 0,
          vertexOffset: 0,
          vertexCount: 2,
          static: true,
          bounds: { minX: -2, minY: -1, minZ: 0.1, maxX: 2, maxY: -1, maxZ: 0.1 },
        },
      ],
      objectCount: 1,
    }),
  });

  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();
  await flushAsyncWork();

  const canvas = mount.firstElementChild;
  assert.ok(canvas, "expected canvas");

  canvas.dispatchEvent({
    type: "pointermove",
    button: 0,
    pointerId: 1,
    clientX: 320,
    clientY: 180,
    preventDefault() {},
    stopPropagation() {},
  });
  await flushAsyncWork();
  await flushAsyncWork();

  assert.ok(env.inputBatchCalls.length > 0, "expected input batch calls after pointermove");
  const lastBatch = JSON.parse(env.inputBatchCalls[env.inputBatchCalls.length - 1][0]);
  const cursor = lastBatch["$cursor"];
  assert.ok(cursor !== undefined, "expected $cursor in batch, got keys: " + Object.keys(lastBatch).join(","));
  assert.ok(typeof cursor === "object" && cursor !== null, "expected $cursor to be an object, got: " + JSON.stringify(cursor));
  assert.ok(cursor.x >= 0 && cursor.x <= 1, "$cursor.x=" + cursor.x + " not in [0,1]");
  assert.ok(cursor.y >= 0 && cursor.y <= 1, "$cursor.y=" + cursor.y + " not in [0,1]");
  assert.equal(env.consoleLogs.error.length, 0);
});

// === P5 cameraOutputSignal: camera published into signal on drag ===
test("P5 cameraOutputSignal: camera drag publishes to output signal", async () => {
  const mount = new FakeElement("div", null);
  mount.id = "scene-camout-signal-root";

  const env = createContext({
    elements: [mount],
    enableWebGL: true,
    disableCanvas2D: true,
    fetchRoutes: {
      "/runtime.wasm": { bytes: [0, 97, 115, 109] },
      "/scene-camout-program.json": { text: '{"name":"SceneCamOut"}' },
    },
    manifest: {
      runtime: { path: "/runtime.wasm" },
      engines: [
        {
          id: "gosx-engine-camout",
          component: "GoSXScene3D",
          kind: "surface",
          mountId: "scene-camout-signal-root",
          runtime: "shared",
          programRef: "/scene-camout-program.json",
          props: {
            width: 640,
            height: 360,
            background: "#000",
            controls: "orbit",
            controlTarget: { x: 0, y: 0, z: 0 },
            camera: { x: 0, y: 0, z: 6, fov: 72 },
            cameraOutputSignal: "$camout",
          },
        },
      ],
    },
    onHydrateEngine: () => "[]",
    onRenderEngine: () => JSON.stringify({
      background: "#000",
      camera: { x: 0, y: 0, z: 6, fov: 72 },
      positions: [],
      colors: [],
      vertexCount: 0,
      worldPositions: [],
      worldColors: [],
      worldVertexCount: 0,
      materials: [],
      objects: [],
      objectCount: 0,
    }),
  });

  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();

  let mounted = env.context.__gosx.engines.get("gosx-engine-camout");
  for (let attempt = 0; attempt < 5 && !mounted; attempt += 1) {
    await flushAsyncWork();
    mounted = env.context.__gosx.engines.get("gosx-engine-camout");
  }
  assert.ok(mounted, "expected gosx-engine-camout to mount");

  const canvas = mount.firstElementChild;
  const inputsBefore = env.inputBatchCalls.length;

  // Drag to move camera
  canvas.dispatchEvent({
    type: "pointerdown",
    button: 0,
    pointerId: 7,
    clientX: 320,
    clientY: 180,
    preventDefault() {},
    stopPropagation() {},
  });
  canvas.dispatchEvent({
    type: "pointermove",
    button: 0,
    buttons: 1,
    pointerId: 7,
    clientX: 400,
    clientY: 120,
    preventDefault() {},
    stopPropagation() {},
  });
  canvas.dispatchEvent({
    type: "pointerup",
    button: 0,
    pointerId: 7,
    clientX: 400,
    clientY: 120,
    preventDefault() {},
    stopPropagation() {},
  });
  await flushAsyncWork();
  await flushAsyncWork();

  // At least one batch should have been emitted with the camera output signal
  assert.ok(env.inputBatchCalls.length > inputsBefore, "expected input batch after drag");
  let cameraKeyFound = false;
  for (const call of env.inputBatchCalls.slice(inputsBefore)) {
    const batch = JSON.parse(call[0]);
    if (batch["$camout"] !== undefined) {
      cameraKeyFound = true;
      const cam = batch["$camout"];
      assert.ok(cam && typeof cam.z === "number", "expected camera object with z field");
      break;
    }
  }
  assert.ok(cameraKeyFound, "expected $camout in an input batch");
  assert.equal(env.consoleLogs.error.length, 0);
});

// === P1 hub outbound binding: direction:"out" sends signal to socket ===
test("P1 hub outbound binding: signal publishes to socket and in-binding still writes shared signal", async () => {
  const sent = [];
  function makeSocket(url) {
    return {
      url,
      readyState: 1,
      closeCalled: false,
      send(raw) {
        sent.push(JSON.parse(raw));
      },
      close() {
        this.closeCalled = true;
      },
    };
  }

  const env = createContext({
    createWebSocket: makeSocket,
    fetchRoutes: {
      "/runtime.wasm": { bytes: [0, 97, 115, 109] },
    },
    manifest: {
      runtime: { path: "/runtime.wasm" },
      hubs: [
        {
          id: "gosx-hub-0",
          name: "cursor-hub",
          path: "/gosx/hub/cursor",
          bindings: [
            { event: "cursor", signal: "$cursor", direction: "out" },
            { event: "state", signal: "$state" },
          ],
        },
      ],
    },
  });

  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();

  assert.equal(env.context.__gosx.hubs.size, 1);
  assert.equal(env.sockets.length, 1);

  // Trigger the onopen (socket starts with readyState:1 so onopen fired at construction)
  // In the test double the socket has readyState:1 already — the onopen is called by
  // attachHubSocketHandlers which triggers bindHubOutputs. We need to fire onopen manually:
  if (typeof env.sockets[0].onopen === "function") {
    env.sockets[0].onopen();
  }
  await flushAsyncWork();

  // Notify the out signal — should trigger socket.send
  env.context.__gosx_notify_shared_signal("$cursor", JSON.stringify({ x: 0.5, y: 0.3 }));
  await flushAsyncWork();

  assert.ok(sent.some((s) => s.event === "cursor" && s.data && s.data.x === 0.5),
    "expected cursor event sent via socket, got: " + JSON.stringify(sent));

  // An "in" binding receiving a message should write the shared signal but NOT fire socket.send for "out" bindings
  const sentBefore = sent.length;
  env.sockets[0].onmessage({ data: JSON.stringify({ event: "state", data: { active: true } }) });
  await flushAsyncWork();

  assert.deepEqual(env.sharedSignalCalls.find((c) => c[0] === "$state"),
    ["$state", '{"active":true}'], "expected $state written via in-binding");

  // "out" binding should NOT consume the inbound message (no extra send for "cursor" event from inbound)
  assert.equal(sent.length, sentBefore, "out binding must not fire from inbound message");

  // Cleanup removes output subscribers
  env.context.__gosx_disconnect_hub("gosx-hub-0");
  assert.equal(env.context.__gosx.hubs.size, 0);
  assert.equal(env.consoleLogs.error.length, 0);
});

test("Selena context-class fields resolve to live per-frame scene state on WebGL", () => {
  const webgl = fs.readFileSync(path.join(__dirname, "bootstrap-src", "16-scene-webgl.js"), "utf8");

  // The per-frame updater exists and derives every reserved name from real
  // scene state: camera, the
  // first directional light (negated into toward-light form), its
  // color x intensity, and environment ambient color x intensity.
  assert.match(webgl, /function sceneSelenaFrameContextUpdate\(cam, lights, environment\)/);
  assert.match(webgl, /cameraPos: \[[\s\S]{0,140}sceneNumber\(cam && cam\.z, 0\)/);
  assert.doesNotMatch(webgl, /-sceneNumber\(cam && cam\.z, 0\)/);
  assert.match(webgl, /if \(String\(light\.kind \|\| ""\)\.toLowerCase\(\) !== "directional"\) continue;/);
  assert.match(webgl, /sunDir = \[-dx \/ len, -dy \/ len, -dz \/ len\];/);
  assert.match(webgl, /ambientRGBA\[0\] \* ambientIntensity/);

  // It refreshes once per frame in the lights block, OUTSIDE hasPBRData —
  // Selena-only scenes still get live context.
  assert.match(webgl, /sceneSelenaFrameContextUpdate\(cam, bundle\.lights, bundle\.environment\);/);

  // selenaUniformValue consults it for context-class fields AFTER the
  // reserved auto-uniforms (time keeps winning) and BEFORE customUniforms
  // (live state beats the material's static fallbacks), with unknown
  // context names falling through.
  const uniformFn = webgl.slice(webgl.indexOf("function selenaUniformValue"));
  const timeAt = uniformFn.indexOf('if (name === "time")');
  const contextAt = uniformFn.indexOf('field.class === "context"');
  const customAt = uniformFn.indexOf("material.customUniforms");
  assert.ok(timeAt >= 0 && contextAt > timeAt && customAt > contextAt,
    "context branch must sit between the time auto-uniform and customUniforms");
  for (const name of ["cameraPos", "sunDir", "sunColor", "ambient"]) {
    assert.match(uniformFn, new RegExp(`if \\(name === "${name}"\\) return sceneSelenaFrameContext\\.${name};`));
  }
});

// Regression guard: cylinder, cone and pyramid drew as wireframes.
// scenePrimitiveTriangleMesh (12-scene-geometry.js) had no case for them, so
// 10-runtime-scene-core.js never set vertices, appendSceneObjectToBundle fell
// through to sceneObjectSegments, 15-scene-draw-plan.js kept the object on the
// line pass, and the WebGPU backend drew it with topology "line-list". All
// three are documented primitive kinds. The plane case was broken too: it
// sliced the first four boxVertices, which all share z = -depth/2, so a solid
// plane was a zero-area strip.
test("Scene3D primitive kinds all build solid triangle meshes", () => {
  const geometry = fs.readFileSync(path.join(__dirname, "bootstrap-src", "12-scene-geometry.js"), "utf8");
  const shared = fs.readFileSync(path.join(__dirname, "bootstrap-src", "16c-scene-shared-pbr.js"), "utf8");
  const context = {
    window: {}, document: {}, console, Math, Number, Float32Array, Array,
    Object, String, Boolean, JSON,
  };
  vm.createContext(context);
  vm.runInContext(
    "(function(){\n" +
    "  function sceneNumber(v, f) { var n = Number(v); return Number.isFinite(n) ? n : f; }\n" +
    shared + "\n" + geometry + "\n" +
    "  globalThis.__primitiveMesh = scenePrimitiveTriangleMesh;\n})();",
    context,
    { filename: "scene-geometry.js" },
  );

  const options = { radius: 0.5, width: 1, height: 1, depth: 1, size: 1, tube: 0.3, segments: 12 };
  for (const kind of ["box", "cube", "plane", "sphere", "torus", "torusknot", "cylinder", "cone", "pyramid"]) {
    const mesh = context.__primitiveMesh(Object.assign({ kind }, options));
    assert.ok(mesh, `${kind} must build a solid triangle mesh, not fall through to the line pass`);
    assert.ok(mesh.count >= 3 && mesh.count % 3 === 0, `${kind} vertex count ${mesh.count} must be whole triangles`);
    assert.equal(mesh.positions.length, mesh.count * 3, `${kind} position stride`);
    assert.equal(mesh.normals.length, mesh.count * 3, `${kind} normal stride`);
    assert.equal(mesh.uvs.length, mesh.count * 2, `${kind} uv stride`);
    assert.ok(Array.from(mesh.positions).every(Number.isFinite), `${kind} positions must be finite`);

    // No axis may collapse. A solid plane spans x and z; a solid box spans all
    // three. Collapse is what the plane bug produced.
    const extent = [0, 1, 2].map((axis) => {
      let min = Infinity, max = -Infinity;
      for (let i = axis; i < mesh.positions.length; i += 3) {
        if (mesh.positions[i] < min) min = mesh.positions[i];
        if (mesh.positions[i] > max) max = mesh.positions[i];
      }
      return max - min;
    });
    const spread = extent.filter((v) => v > 1e-6).length;
    const wanted = kind === "plane" ? 2 : 3;
    assert.ok(spread >= wanted, `${kind} must span ${wanted} axes, spans ${spread} (${extent})`);
  }
});

// A Points cloud and a Sprite must be pickable through the bundle the runtime
// really builds, not only through a bundle a test writes by hand.
//
// scene.TraceGraph picks both families in Go. sceneRaycastPick reached neither
// until it started to call sceneRaycastPickPoints, so a headless test proved a
// hit the browser could not reproduce. The sprite half needs two fields
// appendSceneSpriteToBundle now writes: `world`, because `position` holds the
// projected screen point and a ray needs the world one, and `scale`, because
// spriteRadiusScale in scene/raycast.go grows the hit radius with it.
//
// The camera sits at (0, 0, 4) and looks down -Z, so a pick through the middle of
// the viewport travels the same ray the Go tests use. A particle at (0, 0, -3)
// therefore answers at 7 - 0.1 = 6.9.
test("Scene3D render bundles keep Points and Sprites ray-pickable", async () => {
  const env = createContext({});
  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();

  const api = env.context.__gosx_scene3d_api;
  const camera = { x: 0, y: 0, z: 4, fov: 75, near: 0.05, far: 128 };
  const sprite = api.normalizeSceneSprite({ id: "badge", src: "/badge.png", x: 0, y: 0, z: -3, scale: 2 }, 0, null);
  const bundle = api.createSceneRenderBundle(
    640,
    360,
    "#08151f",
    camera,
    [],
    [],
    [sprite],
    [],
    [],
    { ambientColor: "#ffffff", ambientIntensity: 0.1 },
    0,
    [{ id: "stars", count: 2, positions: [4, 4, -3, 0, 0, -3], size: 2 }],
    [],
    [],
    [],
    [],
    0,
  );

  assert.equal(bundle.sprites.length, 1);
  // Read the fields one by one: the bundle is built inside the script realm, so a
  // deep compare against a literal from this realm fails on the prototype alone.
  assert.equal(bundle.sprites[0].world.x, 0);
  assert.equal(bundle.sprites[0].world.y, 0);
  assert.equal(bundle.sprites[0].world.z, -3);
  assert.equal(bundle.sprites[0].scale, 2);

  // The sprite radius is 0.1 * scale, so the sprite answers first at 6.8.
  const spriteHit = api.sceneRaycastPick(320, 180, 640, 360, bundle.camera, bundle);
  assert.ok(spriteHit, "expected a ray pick on the sprite");
  assert.equal(spriteHit.object.id, "badge");
  assert.equal(spriteHit.kind, "sprite");
  assert.ok(Math.abs(spriteHit.distance - 6.8) < 1e-9, `sprite distance ${spriteHit.distance}`);

  // Drop the sprite and the particle behind it answers at 6.9, with the particle
  // index Go reports through RayHit.InstanceIndex.
  const pointsOnly = Object.assign({}, bundle, { sprites: [] });
  const pointsHit = api.sceneRaycastPick(320, 180, 640, 360, pointsOnly.camera, pointsOnly);
  assert.ok(pointsHit, "expected a ray pick on the point cloud");
  assert.equal(pointsHit.object.id, "stars");
  assert.equal(pointsHit.kind, "points");
  assert.equal(pointsHit.instanceIndex, 1);
  assert.ok(Math.abs(pointsHit.distance - 6.9) < 1e-9, `points distance ${pointsHit.distance}`);
  assert.ok(Math.abs(pointsHit.worldPosition.z - -2.9) < 1e-9, `points hit z ${pointsHit.worldPosition.z}`);
});
