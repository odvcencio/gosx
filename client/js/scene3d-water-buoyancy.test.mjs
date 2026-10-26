import test from "node:test";
import assert from "node:assert/strict";
import fs from "node:fs";
import vm from "node:vm";

const controlsSource = fs.readFileSync(new URL("./bootstrap-src/19b-scene-control-forms.ts", import.meta.url), "utf8");

function fixture(globals = {}) {
  let now = 1000;
  const context = vm.createContext({
    Date,
    JSON,
    Math,
    Number,
    performance: { now: () => now },
    sceneNumber(value, fallback) {
      const number = Number(value);
      return Number.isFinite(number) ? number : fallback;
    },
    window: {},
    ...globals,
  });
  vm.runInContext(controlsSource + `
    globalThis.fluidTest = {
      observeSelection: sceneManagedFluidObjectObserveSelection,
      objectState: sceneManagedFluidObjectObjectState,
      objectStep: sceneManagedFluidObjectObjectStep,
      stepPhysics: sceneManagedFluidObjectStepPhysics,
      clamp: sceneManagedFluidObjectClampObjectPosition,
      activeObjectHit: sceneManagedFluidObjectActiveObjectHit,
      hitMatchesConfig: sceneManagedFluidObjectHitMatchesConfig,
    };
  `, context, { filename: "19b-scene-control-forms.ts" });
  const sphere = {
    id: "float-sphere", label: "Sphere", objectKind: "sphere",
    objectX: 0.37, objectY: -0.75, objectZ: -0.22,
    objectRadius: 0.25, buoyancyRadius: 0.31,
    floorClearance: 0.25, xLimitRadius: 0.25, zLimitRadius: 0.25,
  };
  const duck = {
    id: "float-duck", label: "Rubber Duck", objectKind: "compound", objectSubtype: "duck", objectHitTest: "mesh",
    objectX: 0.4, objectY: -0.1, objectZ: -0.2,
    objectRadius: 0.32, buoyancyRadius: 0.12,
    floorClearance: 0, selectionMinY: -0.1,
    xLimitRadius: 0.32, zLimitRadius: 0.32,
  };
  const cube = {
    ...sphere, id: "float-cube", label: "Cube", objectKind: "cube",
    objectHalfSizeX: 0.25, objectHalfSizeY: 0.25, objectHalfSizeZ: 0.25,
  };
  const profile = {
    physics: { gravityY: -4, bounce: 0.7, defaultBuoyancyScale: 1.1 },
    objects: { Sphere: sphere, "Rubber Duck": duck, Cube: cube },
  };
  const controls = {
    object: "Sphere", gravity: true, paused: false,
    densityEnabled: false, density: 0.9,
    poolWidth: 1, poolHeight: 1, poolLength: 1, poolShape: "Box",
  };
  const state = { objects: {}, lastStepTime: 0 };
  return { api: context.fluidTest, profile, controls, state, sphere, duck, cube, advance(ms) { now += ms; } };
}

function snapshot(value) {
  return JSON.parse(JSON.stringify(value));
}

function releaseAt(f, y, seconds = 6) {
  const object = {
    position: { x: 0.2, y, z: -0.2 },
    velocity: { x: 0, y: 0, z: 0 },
  };
  const samples = [snapshot(object)];
  for (let index = 0; index < seconds * 120; index += 1) {
    f.api.stepPhysics(object, f.duck, f.controls, f.profile, 1 / 120);
    samples.push(snapshot(object));
  }
  return samples;
}

test("water buoyancy: duck selection guard preserves shared x/z without becoming a physical floor", () => {
  const f = fixture();
  f.api.observeSelection(f.state, f.controls, f.profile);
  f.controls.object = "Rubber Duck";
  f.api.observeSelection(f.state, f.controls, f.profile);
  const duckState = f.api.objectState(f.state, f.duck);
  assert.deepEqual(snapshot(duckState.position), { x: 0.37, y: -0.1, z: -0.22 });

  // A normal drag updates position/pendingPrevious before the next object step.
  // Re-observing the same selection must not clamp the duck to its switch guard.
  duckState.pendingPrevious = snapshot(duckState.position);
  duckState.position.y = -0.45;
  f.api.observeSelection(f.state, f.controls, f.profile);
  const step = f.api.objectStep(f.state, f.controls, f.duck, f.profile, {});
  assert.equal(step.position.y, -0.45);
  assert.equal(step.previous.y, -0.1);
  assert.equal(step.previousSet, true);
  f.state.transitionPending = false;
  f.advance(1000 / 120);
  const released = f.api.objectStep(f.state, f.controls, f.duck, f.profile, {});
  assert.ok(released.position.y > -0.45, "released submerged duck must start rising");
  assert.ok(released.position.y < -0.1, "selection-only guard must not pin a dunked duck to the waterline");
});

test("water buoyancy: genuine deep dunk rises, overshoots equilibrium, and bobs without touching the floor", () => {
  const f = fixture();
  const samples = releaseAt(f, -0.45);
  const ys = samples.map(sample => sample.position.y);
  const equilibrium = f.duck.buoyancyRadius - 2 * f.duck.buoyancyRadius / f.profile.physics.defaultBuoyancyScale;
  assert.ok(samples[120].position.y > -0.3, "dunked duck must visibly recover after release");
  assert.ok(Math.max(...ys) > equilibrium + 0.05, "inertia must carry recovery above the equilibrium line");
  assert.ok(samples.some(sample => sample.velocity.y > 0.1), "recovery needs actual upward velocity");
  assert.ok(samples.some(sample => sample.position.y > equilibrium && sample.velocity.y < -0.1), "bob must reverse downward after overshoot");
  assert.ok(Math.min(...ys) > -f.controls.poolHeight + 0.1, "recovery must not be a pool-floor bounce");
  assert.ok(samples.every(sample => sample.position.x === 0.2 && sample.position.z === -0.2), "vertical buoyancy must not inject horizontal drift");
});

test("water buoyancy: a raised release descends under gravity before returning upward", () => {
  const f = fixture();
  const samples = releaseAt(f, 0.25, 6);
  assert.ok(samples[60].position.y < 0, "raised duck must cross the waterline after release");
  assert.ok(samples[60].velocity.y < 0, "raised release must genuinely accelerate downward");
  assert.ok(samples.some(sample => sample.position.y < -0.2 && sample.velocity.y > 0.1), "submerged duck must recover upward");
  assert.ok(samples.every(sample => sample.position.y > -f.controls.poolHeight), "this release must not rely on floor collision");
});

test("water buoyancy: gravity off or paused does not integrate position or velocity", () => {
  for (const override of [{ gravity: false }, { paused: true }]) {
    const f = fixture();
    Object.assign(f.controls, override);
    const object = { position: { x: 0.2, y: -0.45, z: -0.2 }, velocity: { x: 0, y: -0.5, z: 0 } };
    const before = snapshot(object);
    for (let index = 0; index < 120; index += 1) f.api.stepPhysics(object, f.duck, f.controls, f.profile, 1 / 120);
    assert.deepEqual(snapshot(object), before);
  }
});

test("water buoyancy: a held dunk stays put and release resumes with only the next frame of gravity", () => {
  const f = fixture();
  f.api.observeSelection(f.state, f.controls, f.profile);
  f.controls.object = "Rubber Duck";
  f.api.observeSelection(f.state, f.controls, f.profile);
  f.api.objectStep(f.state, f.controls, f.duck, f.profile, {});
  f.state.transitionPending = false;
  const duckState = f.api.objectState(f.state, f.duck);
  duckState.position.y = -0.45;
  duckState.velocity = { x: 0, y: 0, z: 0 };
  f.state.pointerMode = "MoveObject";
  const held = snapshot(duckState);
  for (let frame = 0; frame < 120; frame += 1) {
    f.advance(1000 / 60);
    const step = f.api.objectStep(f.state, f.controls, f.duck, f.profile, {});
    assert.deepEqual(snapshot(step.position), held.position, "gravity must not move a pointer-held dunk");
    assert.deepEqual(snapshot(duckState.velocity), held.velocity, "holding must not accumulate hidden fall/rise velocity");
  }
  assert.ok(Math.abs(f.state.lastStepTime - 3) < 1e-12, "held frames must still advance the physics clock");

  f.state.pointerMode = "";
  f.advance(1000 / 120);
  const release = f.api.objectStep(f.state, f.controls, f.duck, f.profile, {});
  const expected = { position: { ...held.position }, velocity: { ...held.velocity } };
  f.api.stepPhysics(expected, f.duck, f.controls, f.profile, 1 / 120);
  assert.ok(release.position.y > -0.45, "buoyant recovery must start on the first released frame");
  assert.ok(Math.abs(release.position.y - expected.position.y) < 1e-12, "release must not catch up the time spent held");
  assert.ok(Math.abs(duckState.velocity.y - expected.velocity.y) < 1e-12, "release velocity must reflect only one normal frame");
});

test("water buoyancy: shapes without a selection guard retain previous shared-position behavior", () => {
  const f = fixture();
  f.api.observeSelection(f.state, f.controls, f.profile);
  f.api.objectState(f.state, f.sphere).position.y = -0.52;
  f.controls.object = "Cube";
  f.api.observeSelection(f.state, f.controls, f.profile);
  assert.deepEqual(snapshot(f.api.objectState(f.state, f.cube).position), { x: 0.37, y: -0.52, z: -0.22 });
});

test("water buoyancy: the duck still collides with the physical pool floor", () => {
  const f = fixture();
  const position = { x: 2, y: -2, z: -2 };
  f.api.clamp(position, f.duck, f.controls);
  assert.ok(Math.abs(position.x - 0.68) < 1e-12);
  assert.equal(position.y, -1);
  assert.ok(Math.abs(position.z + 0.68) < 1e-12);
  const object = { position: { x: 0, y: -0.999, z: 0 }, velocity: { x: 0, y: -1, z: 0 } };
  f.api.stepPhysics(object, f.duck, f.controls, f.profile, 1 / 60);
  assert.equal(object.position.y, -1);
  assert.ok(object.velocity.y > 0, "physical floor contact still applies the authored bounce");
});

function pickingFixture() {
  const calls = { sphere: 0, box: 0, groups: [] };
  let hitPoint = { x: 0.4, y: 0.12, z: -0.2 };
  let forcedHitObject = null;
  const f = fixture({
    sceneScreenToRay() {
      return { origin: { x: 0, y: 0, z: 4 }, direction: { x: 0, y: 0, z: -1 } };
    },
    sceneRayIntersectSphere() {
      calls.sphere += 1;
      return { x: 0.4, y: -0.1, z: -0.2, distance: 1 };
    },
    sceneRayIntersectAABB() {
      calls.box += 1;
      return { x: 0.4, y: -0.1, z: -0.2, distance: 1 };
    },
    sceneRaycastPickGroup(ray, objects, positions, startIndex, uvs) {
      calls.groups.push({ ray, objects, positions, startIndex, uvs });
      if (!hitPoint) return null;
      return {
        object: forcedHitObject || objects[objects.length - 1],
        distance: 0.5,
        point: hitPoint,
        worldPosition: hitPoint,
      };
    },
  });
  f.controls.object = "Rubber Duck";
  const form = {};
  const canvas = {
    tagName: "canvas",
    getBoundingClientRect() { return { left: 0, top: 0, width: 100, height: 100 }; },
  };
  const sceneState = { camera: { x: 0, y: 0, z: 4, rotationX: 0, rotationY: 0, rotationZ: 0 } };
  return {
    ...f,
    calls,
    miss() { hitPoint = null; },
    forceHitObject(object) { forcedHitObject = object; },
    hit(bundle) {
      return f.api.activeObjectHit(form, canvas, sceneState, f.controls, f.profile, { clientX: 50, clientY: 50 }, {
        getBundle() { return bundle; },
      });
    },
  };
}

test("water picking: loaded duck primitive IDs are accepted as selected-model descendants", () => {
  const f = pickingFixture();
  const primitive = { id: "float-duck/LOD3spShape-prim-0" };
  const hit = f.hit({ meshObjects: [primitive], worldMeshPositions: [0] });
  assert.ok(hit, "a visible decoded duck primitive must be draggable");
  assert.equal(hit.hit.object, primitive);
  assert.deepEqual(hit.point, { x: 0.4, y: 0.12, z: -0.2 });
  assert.equal(f.calls.sphere, 0);
  assert.equal(f.calls.box, 0);
});

test("water picking: every selected model primitive reaches its exact raycast group", () => {
  for (const [entriesKey, positionsKey, uvsKey] of [
    ["meshObjects", "worldMeshPositions", "worldMeshUVs"],
    ["objects", "worldPositions", null],
  ]) {
    const f = pickingFixture();
    const selected = [
      { id: "float-duck/LOD3spShape-prim-0" },
      { id: "float-duck/LOD3spShape-prim-1" },
      { id: "float-duck/details/bill-prim-0" },
    ];
    const unrelated = [{ id: "pool" }, { id: "float-duckling/body-prim-0" }, { id: "float-torus" }];
    const positions = [1, 2, 3];
    const uvs = [0, 1];
    const bundle = { [entriesKey]: [unrelated[0], selected[0], unrelated[1], selected[1], selected[2], unrelated[2]], [positionsKey]: positions };
    if (uvsKey) bundle[uvsKey] = uvs;
    const hit = f.hit(bundle);
    assert.ok(hit, `${entriesKey} descendant group must produce the actual primitive hit`);
    assert.equal(hit.hit.object, selected[2]);
    assert.equal(f.calls.groups.length, 1);
    assert.deepEqual(Array.from(f.calls.groups[0].objects), selected, "all selected primitives, and only those primitives, must be raycast");
    assert.equal(f.calls.groups[0].positions, positions);
    assert.equal(f.calls.groups[0].uvs, uvsKey ? uvs : null);
  }
});

test("water picking: unrelated and near-prefix IDs cannot claim the selected duck", () => {
  const f = pickingFixture();
  for (const id of ["float-duck", "float-duck/LOD3spShape-prim-0", "float-duck/details/bill-prim-0"]) {
    assert.equal(f.api.hitMatchesConfig({ object: { id } }, f.duck), true, id);
  }
  const rejected = ["", "float-duckling", "float-duckling/body", "float-duck-2/prim-0", "float-duck2/prim-0", "other/float-duck", "float-torus"];
  for (const id of rejected) assert.equal(f.api.hitMatchesConfig({ object: { id } }, f.duck), false, id);
  assert.equal(f.hit({ meshObjects: rejected.map(id => ({ id })), worldMeshPositions: [0] }), null);
  assert.equal(f.calls.groups.length, 0, "unrelated meshes must not even enter selected-object raycasting");

  // The final hit guard must remain in place even if a picker returns a bad ID.
  f.forceHitObject({ id: "float-duckling/body" });
  assert.equal(f.hit({ meshObjects: [{ id: "float-duck/LOD3spShape-prim-0" }], worldMeshPositions: [0] }), null);
});

test("water picking: a genuine duck mesh miss stays a miss without analytic sphere fallback", () => {
  const f = pickingFixture();
  f.miss();
  const hit = f.hit({
    meshObjects: [{ id: "float-duck/LOD3spShape-prim-0" }, { id: "float-duck/LOD3spShape-prim-1" }],
    worldMeshPositions: [0],
  });
  assert.equal(hit, null, "nearby water must stay available for ripple input when duck triangles are missed");
  assert.equal(f.calls.groups.length, 1);
  assert.equal(f.calls.sphere, 0, "a bounding sphere must not turn a triangle miss into a duck drag");
  assert.equal(f.calls.box, 0);
});
