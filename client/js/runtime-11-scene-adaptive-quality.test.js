"use strict";
// Adaptive quality: the tier controller, the authored QualityLadder, promotion
// and demotion rules, and the LayerGroups visibility filter.
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
  FakeElement,
  createContext,
  runScript,
  flushAsyncWork,
  loadSceneAdaptiveQualityAPI,
  createAdaptiveQualityHarness,
  createQualityLadderHarness,
  THREE_RUNG_LADDER,
  createQualityLadderRAFHarness,
  RAW_TO_GLOW_LADDER,
  readSceneMountSrc,
} = require("./runtime-test-harness.js");

test("Scene3D declarative status bindings expose backend fallback and quality without CSS :has()", () => {
  const { api } = loadSceneAdaptiveQualityAPI();
  const scope = new FakeElement("section", null);
  const mount = new FakeElement("div", null);
  const renderer = new FakeElement("output", null);
  const fallback = new FakeElement("output", null);
  const quality = new FakeElement("output", null);
  scope.setAttribute("data-gosx-scene3d-status-scope", "");
  renderer.setAttribute("data-gosx-scene3d-status", "renderer");
  fallback.setAttribute("data-gosx-scene3d-status", "fallback");
  quality.setAttribute("data-gosx-scene3d-status", "quality");
  scope.appendChild(mount);
  scope.appendChild(renderer);
  scope.appendChild(fallback);
  scope.appendChild(quality);

  mount.setAttribute("data-gosx-scene3d-renderer", "webgl");
  mount.setAttribute("data-gosx-scene3d-renderer-fallback", "webgpu-unavailable");
  mount.setAttribute("data-gosx-scene3d-quality-active", "balanced");
  api.sceneSyncStatusBindings(mount);

  assert.equal(renderer.textContent, "WebGL2");
  assert.equal(renderer.getAttribute("data-state"), "webgl");
  assert.equal(fallback.textContent, "· fallback: WebGPU Unavailable");
  assert.equal(fallback.hidden, false);
  assert.equal(quality.textContent, "Balanced");

  mount.setAttribute("data-gosx-scene3d-renderer-fallback", "");
  api.sceneSyncStatusBindings(mount);
  assert.equal(fallback.textContent, "");
  assert.equal(fallback.hidden, true);
});

test("Scene3D adaptive profiles start balanced and expose exact frame contract", () => {
  const { state, mount } = createAdaptiveQualityHarness();
  assert.equal(state.requestedTier, "balanced");
  assert.equal(state.activeTier, "balanced");
  assert.deepEqual(JSON.parse(JSON.stringify(state.activeProfile)), {
    tier: "balanced", dprCap: 1.25, surfaceResolution: 128,
    causticsResolution: 384, objectShadowResolution: 384,
    objectTextureMaxSide: 384, objectTexturePixelBudget: 442368,
    expensivePassCadence: 2,
  });
  assert.equal(mount.getAttribute("data-gosx-scene3d-quality-requested"), "balanced");
  assert.equal(mount.getAttribute("data-gosx-scene3d-quality-active"), "balanced");
  assert.equal(mount.__gosxScene3DQualityState.profile, state.activeProfile);
  const mountSource = readSceneMountSrc();
  assert.match(mountSource, /qualityEnabled: qualityEnabled,[\s\S]{0,320}qualityProfile: qualityProfile,[\s\S]{0,320}performanceMeasurement: adaptiveQuality\.lastMeasurement/);
});

test("Scene3D adaptive config objects default enabled and disabled mode sends no profile override", () => {
  const { api } = loadSceneAdaptiveQualityAPI();
  const enabled = api.createSceneAdaptiveQualityState({ adaptiveQuality: { tier: "balanced" } }, {}, {});
  const disabled = api.createSceneAdaptiveQualityState({ adaptiveQuality: { enabled: false }, qualityTier: "balanced" }, {}, {});
  assert.equal(enabled.enabled, true);
  assert.equal(disabled.enabled, false);
  const mountSource = readSceneMountSrc();
  assert.match(mountSource, /const qualityProfile = qualityEnabled && adaptiveQuality\.activeProfile[\s\S]{0,100}: null/);
});

test("Scene3D adaptive measurement escapes stale renderer timing and respects authored frame caps", () => {
  const locked = createAdaptiveQualityHarness();
  locked.renderer.pollPerformanceSample = function() { return null; };
  locked.renderer.getPerformanceTimingStatus = function() { return { available: true, active: true, pending: true, source: "gpu-test" }; };
  const before = locked.state.validSamples;
  for (let i = 0; i < 7; i++) locked.sample(99, 34);
  assert.equal(locked.state.validSamples, before, "a healthy timestamp ring gets a bounded window to deliver a sample");
  for (let i = 0; i < 3; i++) locked.sample(99, 34);
  assert.ok(locked.state.validSamples > before, "a renderer timer that never resolves must fall back to display-frame timing");
  assert.equal(locked.state.measurement, "cpu-raf-stale-renderer-timing");
  assert.equal(locked.state.activeTier, "survival", "severely missed display frames must still downshift quality");
  assert.equal(locked.mount.getAttribute("data-gosx-scene3d-quality-renderer-timing-misses"), "10");

  const capped = createAdaptiveQualityHarness({ maxFPS: 30 });
  capped.renderer.pollPerformanceSample = function() { return null; };
  capped.renderer.getPerformanceTimingStatus = function() { return { available: false, active: false, pending: false, source: "none" }; };
  for (let i = 0; i < 24; i++) capped.sample(99, 34);
  assert.equal(capped.state.measurement, "cpu-raf");
  assert.equal(capped.state.activeTier, "balanced", "authored 30 FPS rAF cadence must not trigger a false downshift");

  // maxFrameRate is the key the Scene3D frame limiter honors FIRST (see
  // sceneAnimationFrameIntervalMS), and it is the key real apps author. The
  // budget derivation once read only maxFPS, so a maxFrameRate-capped scene
  // measured its own authored ~33ms cadence against the adaptive target and
  // demoted itself to the floor over and over — on the m31labs galaxy every
  // rung step re-partitioned the point layers and read as gas-body flicker.
  const capdRate = createAdaptiveQualityHarness({ maxFrameRate: 30, adaptiveTargetFrameMS: 28 });
  capdRate.renderer.pollPerformanceSample = function() { return null; };
  capdRate.renderer.getPerformanceTimingStatus = function() { return { available: false, active: false, pending: false, source: "none" }; };
  for (let i = 0; i < 60; i++) capdRate.sample(99, 34);
  assert.equal(capdRate.state.measurement, "cpu-raf");
  assert.equal(capdRate.state.activeTier, "balanced", "authored maxFrameRate cadence must not trigger a false downshift");
  assert.ok(capdRate.state.cpuRAFBudgetMS >= 1000 / 30 - 0.1, "cpu-raf budget must honor maxFrameRate, got " + capdRate.state.cpuRAFBudgetMS);
});

test("Scene3D adaptive controller is hysteretic, cooldown-safe, and recovers one tier", () => {
  const sustained = createAdaptiveQualityHarness();
  for (let i = 0; i < 19; i++) sustained.sample(20);
  assert.equal(sustained.state.activeTier, "balanced");
  sustained.sample(20);
  assert.equal(sustained.state.activeTier, "survival");
  assert.equal(sustained.state.qualityRevision, 1);
  assert.equal(sustained.state.postFXSuppressed, false, "postFX must remain until after survival");
  for (let i = 0; i < 300; i++) sustained.sample(5);
  assert.equal(sustained.state.activeTier, "survival", "cooldown must prevent oscillation");
  sustained.sample(5, 5001);
  assert.equal(sustained.state.activeTier, "balanced", "recovery is one tier and never above requested balanced");

  const severe = createAdaptiveQualityHarness();
  severe.sample(40); severe.sample(40);
  assert.equal(severe.state.activeTier, "balanced");
  severe.sample(40);
  assert.equal(severe.state.activeTier, "survival", "three >2x samples must severe-downshift");
});

test("Scene3D adaptive controller sheds postFX last and bounds DOM telemetry", () => {
  const harness = createAdaptiveQualityHarness({ qualityTier: "full" });
  let writes = 0;
  const originalSet = harness.mount.setAttribute.bind(harness.mount);
  harness.mount.setAttribute = function(name, value) { writes += 1; originalSet(name, value); };
  for (let i = 0; i < 20; i++) harness.sample(20);
  assert.equal(harness.state.activeTier, "balanced");
  assert.equal(harness.state.postFXSuppressed, false);
  harness.sample(20, 5001);
  for (let i = 1; i < 20; i++) harness.sample(20);
  assert.equal(harness.state.activeTier, "survival");
  assert.equal(harness.state.postFXSuppressed, false);
  harness.sample(20, 5001);
  for (let i = 1; i < 20; i++) harness.sample(20);
  assert.equal(harness.state.activeTier, "survival");
  assert.equal(harness.state.postFXSuppressed, true);
  assert.ok(writes < 180, "quality attrs must publish at <=4Hz or transitions, got " + writes);
  assert.equal(harness.mount.__gosxScene3DQualityState.validSamples, harness.state.validSamples);
  assert.equal(harness.mount.__gosxScene3DQualityState.measurement, "gpu-test");
});

test("Scene3D QualityLadder: no ladder authored leaves mode=tier (back-compat)", () => {
  const { api } = loadSceneAdaptiveQualityAPI();
  const noLadder = api.createSceneAdaptiveQualityState({ adaptiveQuality: true, qualityTier: "balanced" }, {}, {});
  assert.equal(noLadder.mode, "tier");
  assert.equal(noLadder.enabled, true, "no ladder authored must leave the dprCap-tier governor's enabled flag untouched");

  const emptyLadder = api.createSceneAdaptiveQualityState({ adaptiveQuality: true, scene: { qualityLadder: [] } }, {}, {});
  assert.equal(emptyLadder.mode, "tier", "an empty qualityLadder array must be treated as no ladder authored");
});

test("Scene3D QualityLadder: authoring a ladder switches mode and disables the dprCap-tier gate (never touches DPR)", () => {
  const { state } = createQualityLadderHarness(THREE_RUNG_LADDER);
  assert.equal(state.mode, "ladder");
  // sceneViewportFromMount's DPR clamp is gated on `adaptiveQuality.enabled`
  // — false here means a ladder-governed mount NEVER restricts DPR via the
  // adaptive-quality path, per the PRIME DIRECTIVE.
  assert.equal(state.enabled, false);
});

test("Scene3D QualityLadder: QualityStartRung is primed before the first render (postEffects admitted-set, not the full authored list)", () => {
  const { sceneState, mount, bloom, tonemap, customLens } = createQualityLadderHarness(THREE_RUNG_LADDER, {
    qualityStartRung: 1,
  });
  // Rung 1 ("glow") admits only "bloom".
  assert.deepEqual(sceneState.postEffects, [bloom]);
  assert.notEqual(sceneState.postEffects.indexOf(bloom), -1);
  assert.equal(sceneState.postEffects.indexOf(tonemap), -1);
  assert.equal(sceneState.postEffects.indexOf(customLens), -1);
  assert.equal(mount.getAttribute("data-gosx-scene3d-quality-rung"), "1");
  assert.equal(mount.getAttribute("data-gosx-scene3d-quality-rung-name"), "glow");
  assert.equal(mount.getAttribute("data-gosx-scene3d-quality-ladder"), "true");
});

test("Scene3D QualityLadder: PROMOTE after N consecutive headroom frames admits the next rung's effects, custom pass source survives verbatim", () => {
  const { state, sceneState, sample, bloom, tonemap, customLens } = createQualityLadderHarness(THREE_RUNG_LADDER, {
    qualityStartRung: 1,
  });
  assert.equal(state.rungIndex, 1);
  assert.equal(state.rungPromoteFrames, 120);
  // Headroom frames: target=16, promoteThreshold=0.7 -> need frameMS < 11.2.
  for (let i = 0; i < 119; i++) sample(5);
  assert.equal(state.rungIndex, 1, "must not promote before rungPromoteFrames consecutive headroom frames");
  sample(5);
  assert.equal(state.rungIndex, 2, "must promote exactly one rung after rungPromoteFrames");
  assert.equal(state.rungReason, "recovered");
  assert.equal(state.rungRevision, 1);
  // Rung 2 ("full") admits all three — the custom pass object is the SAME
  // reference as the author's original source array entry, proving the
  // rung-change filter never rebuilds/clones effect records (compiled
  // Selena pass source, e.g. vertexGLSL/fragmentGLSL, survives verbatim).
  assert.equal(sceneState.postEffects.length, 3);
  assert.notEqual(sceneState.postEffects.indexOf(bloom), -1);
  assert.notEqual(sceneState.postEffects.indexOf(tonemap), -1);
  const survived = sceneState.postEffects[sceneState.postEffects.indexOf(customLens)];
  assert.strictEqual(survived, customLens, "custom pass must be the exact same object reference, not a rebuilt clone");
  assert.equal(survived.vertexGLSL, customLens.vertexGLSL);
  assert.equal(survived.fragmentGLSL, customLens.fragmentGLSL);
});

test("Scene3D QualityLadder: promote is a no-op at the top rung (saturated ladder does not spam transitions)", () => {
  const { state, sample } = createQualityLadderHarness(THREE_RUNG_LADDER, {
    qualityStartRung: 2,
  });
  assert.equal(state.rungIndex, 2);
  for (let i = 0; i < 400; i++) sample(5);
  assert.equal(state.rungIndex, 2, "already at the top rung; must stay put");
  assert.equal(state.rungRevision, 0, "no-op promote attempts must not bump the revision");
});

test("Scene3D QualityLadder: DEMOTE on the dprCap-tier controller's sustained-miss condition (badFrames >= 20), floor rung is raw (postFX off)", () => {
  const { state, sceneState, sample } = createQualityLadderHarness(THREE_RUNG_LADDER, {
    qualityStartRung: 1,
  });
  for (let i = 0; i < 19; i++) sample(20);
  assert.equal(state.rungIndex, 1, "19 bad frames must not yet demote");
  sample(20);
  assert.equal(state.rungIndex, 0, "20th sustained-bad frame must demote exactly one rung");
  assert.equal(state.rungReason, "sustained");
  assert.equal(state.rungRevision, 1);
  // .length check, not deepEqual against a literal [] — the empty-admission
  // branch constructs its array literal inside the vm-sandboxed harness
  // realm (a real cross-realm artifact of THIS test harness only; the
  // production bundle runs in a single realm), which trips
  // assert.deepStrictEqual's realm-sensitive comparison despite both sides
  // printing identically as [].
  assert.equal(sceneState.postEffects.length, 0, "rung 0 (raw) admits no postEffects — the crisp floor, never a blurred low-res composite");
});

test("Scene3D QualityLadder: three severe (>2x budget) frames force an immediate demote", () => {
  const { state, sample } = createQualityLadderHarness(THREE_RUNG_LADDER, {
    qualityStartRung: 2,
  });
  sample(40); sample(40);
  assert.equal(state.rungIndex, 2);
  sample(40);
  assert.equal(state.rungIndex, 1, "three >2x samples must severe-downshift one rung");
  assert.equal(state.rungReason, "severe");
});

test("Scene3D QualityLadder: demote is a no-op at rung 0 (raw floor already reached)", () => {
  const { state, sample } = createQualityLadderHarness(THREE_RUNG_LADDER, {
    qualityStartRung: 0,
  });
  for (let i = 0; i < 200; i++) sample(40);
  assert.equal(state.rungIndex, 0, "already at the raw floor; must stay put");
  assert.equal(state.rungRevision, 0, "no-op demote attempts must not bump the revision");
});

test("Scene3D QualityLadder: hysteresis/cooldown prevent oscillation and recovery promotes exactly one rung", () => {
  const { state, sample } = createQualityLadderHarness(THREE_RUNG_LADDER, {
    qualityStartRung: 2,
  });
  for (let i = 0; i < 20; i++) sample(20);
  assert.equal(state.rungIndex, 1, "sustained bad frames demote from the top rung");
  // Cooldown must suppress further transitions even under continued good
  // frames (the dprCap-tier controller's cooldownMS is reused verbatim).
  for (let i = 0; i < 300; i++) sample(5);
  assert.equal(state.rungIndex, 1, "cooldown must prevent an immediate re-promote");
  sample(5, 5001); // advance past cooldownMS (5000)
  assert.equal(state.rungIndex, 2, "recovery promotes exactly one rung once the cooldown elapses");
});

// --- Promotion rule must differ by measurement source (cpu-raf vs GPU timing) ---
//
// Bug: on a real browser without GPU timestamp-query support (regular Chrome
// stable — the common case), the measurement source is cpu-raf and the rAF
// interval on a vsync-locked display floors at ~1/refreshRate (~16.7ms @60Hz)
// even on a perfectly healthy page — it can never read below
// state.rungPromoteThreshold (0.7) × target, so the ladder could never
// promote past the boot rung in production (observed: postfx stayed "none"
// after 13k+ frames at a locked 60fps). The headless test harness always
// grants gpu timing, which is why this never surfaced in CI.
test("Scene3D QualityLadder: cpu-raf measurement promotes on sustained clean cadence (frameMS <= 1.06x target), not GPU headroom", () => {
  const { state, sample, mount } = createQualityLadderRAFHarness(THREE_RUNG_LADDER, {
    qualityStartRung: 0,
  });
  assert.equal(state.rungPromoteFrames, 120);
  // A perfectly healthy vsync-locked 60Hz page: frameMS ~16.7 against a
  // target of 16 never has "headroom" below 0.7x (11.2ms) — the OLD rule
  // would stay stuck here forever. The raf-cadence rule only requires not
  // exceeding target by more than 6% (<= 16.96ms), which 16.7 satisfies.
  for (let i = 0; i < 119; i++) sample(16.7);
  assert.equal(state.rungIndex, 0, "must not promote before rungPromoteFrames consecutive clean-cadence frames");
  assert.equal(state.measurement, "cpu-raf", "must be measuring via the cpu-raf fallback, not GPU timing");
  sample(16.7);
  assert.equal(state.rungIndex, 1, "must promote exactly one rung after rungPromoteFrames clean-cadence frames");
  assert.equal(state.rungReason, "recovered");
  assert.equal(state.rungPromoteRule, "raf-cadence", "the raf-cadence rule, not gpu-headroom, must have driven this promotion");
  assert.equal(mount.getAttribute("data-gosx-scene3d-quality-promote-rule"), "raf-cadence");
});

test("Scene3D QualityLadder: cpu-raf measurement does NOT promote when frames exceed the clean-cadence ceiling", () => {
  const { state, sample } = createQualityLadderRAFHarness(THREE_RUNG_LADDER, {
    qualityStartRung: 0,
  });
  // 25ms frames on a 16ms target exceed the 1.06x (16.96ms) clean-cadence
  // ceiling every frame — this must never accumulate goodFrames toward a
  // promotion (already at the floor rung, so any spurious demote attempt is
  // also a safe no-op — this asserts the ladder simply never climbs).
  for (let i = 0; i < 200; i++) sample(25);
  assert.equal(state.rungIndex, 0, "must not promote when cpu-raf frames consistently exceed the clean-cadence ceiling");
  assert.equal(state.measurement, "cpu-raf");
  assert.equal(state.rungPromoteRule, "raf-cadence");
});

test("Scene3D QualityLadder: active scroll input does not demote on cpu-raf cadence spikes", () => {
  const harness = createQualityLadderRAFHarness(THREE_RUNG_LADDER, {
    qualityStartRung: 1,
  });
  const { state, sceneState, sample, clock } = harness;
  sceneState._scrollCamera = {
    start: 10,
    end: 4,
    _activeInputUntil: clock.now + 5000,
  };
  for (let i = 0; i < 30; i++) sample(40);
  assert.equal(state.rungIndex, 1, "scroll-time cpu-raf spikes must not demote the ladder");
  assert.equal(state.badFrames, 0);
  assert.equal(state.severeFrames, 0);
  assert.equal(state.measurement, "cpu-raf-scroll-interaction");
  assert.equal(state.rungPromoteRule, "scroll-interaction-held");

  sceneState._scrollCamera._activeInputUntil = clock.now - 1000;
  for (let i = 0; i < 3; i++) sample(40);
  assert.equal(state.rungIndex, 1, "post-scroll cooldown must still hold the current rung");

  sceneState._scrollCamera._activeInputUntil = clock.now - 2000;
  for (let i = 0; i < 3; i++) sample(40);
  assert.equal(state.rungIndex, 0, "real post-scroll severe misses must still demote");
});

test("Scene3D QualityLadder: GPU-measured samples still use the original headroom rule (unchanged)", () => {
  const { state, sample, mount } = createQualityLadderHarness(THREE_RUNG_LADDER, {
    qualityStartRung: 0,
  });
  // 5ms GPU-measured frames against a 16ms target have real headroom well
  // below 0.7x (11.2ms) — the original rule.
  for (let i = 0; i < 119; i++) sample(5);
  assert.equal(state.rungIndex, 0);
  sample(5);
  assert.equal(state.rungIndex, 1, "GPU-headroom rule must still promote exactly as before");
  assert.equal(state.measurement, "gpu-test");
  assert.equal(state.rungPromoteRule, "gpu-headroom", "GPU-measured samples must report the gpu-headroom promote rule");
  assert.equal(mount.getAttribute("data-gosx-scene3d-quality-promote-rule"), "gpu-headroom");
});

test("Scene3D QualityLadder: LayerGroups filter — untagged objects always visible, tagged objects gated by the active rung", () => {
  const { api } = loadSceneAdaptiveQualityAPI();
  const noLadderState = { mode: "tier" };
  assert.equal(api.sceneQualityLadderAdmittedGroups(noLadderState), null, "no ladder governing -> back-compat, no filtering");

  const ladderState = { mode: "ladder", ladder: THREE_RUNG_LADDER.map(function(r, i) {
    return { name: r.name, layerGroups: r.layerGroups || [], postEffects: r.postEffects || [] };
  }), rungIndex: 1 };
  const admitted = api.sceneQualityLadderAdmittedGroups(ladderState);
  assert.deepEqual(admitted, ["particles"]);

  const hero = { id: "hero" }; // untagged
  const farStar = { id: "far-star", qualityGroup: "particles" };
  const decor = { id: "decor", qualityGroup: "far-decor" };
  const objects = [hero, farStar, decor];

  assert.strictEqual(api.sceneFilterObjectsByQualityGroups(objects, null), objects, "no ladder -> same array reference, zero-cost back-compat");

  const filtered = api.sceneFilterObjectsByQualityGroups(objects, admitted);
  assert.deepEqual(filtered, [hero, farStar], "untagged hero always survives; far-decor is not admitted at rung 1");

  const admittedOnly = [hero, farStar];
  const noFilterNeeded = api.sceneFilterObjectsByQualityGroups(admittedOnly, admitted);
  assert.strictEqual(noFilterNeeded, admittedOnly, "nothing dropped -> same array reference, no allocation on the hot per-frame path");
});

test("Scene3D QualityLadder: Points LayerGroups filter — untagged always drawn, tagged gated by rung, name-mapped GLB layers gated via pointQualityGroups", () => {
  const { api } = loadSceneAdaptiveQualityAPI();
  const ladderState = { mode: "ladder", ladder: THREE_RUNG_LADDER.map(function(r, i) {
    return { name: r.name, layerGroups: r.layerGroups || [], postEffects: r.postEffects || [] };
  }), rungIndex: 1 };
  const admitted = api.sceneQualityLadderAdmittedGroups(ladderState);
  assert.deepEqual(admitted, ["particles"]);

  const heroDust = { id: "hero-dust" }; // untagged Points node
  const farDust = { id: "far-dust", qualityGroup: "particles" }; // tagged Points node, admitted
  const decorDust = { id: "decor-dust", qualityGroup: "far-decor" }; // tagged Points node, not admitted at rung 1
  // GLB-extracted point layer: no authored qualityGroup, identified by the
  // SAME `material` field the named-material binding path matches by.
  const nebulaLayer = { id: "nebula-layer-03-points-0", material: "nebula-layer-03" };
  const spareLayer = { id: "spare-layer-points-0", material: "spare-layer" };
  const points = [heroDust, farDust, decorDust, nebulaLayer, spareLayer];
  const pointQualityGroups = new Map([
    ["nebula-layer-03", "particles"], // admitted at rung 1
    ["spare-layer", "far-decor"], // not admitted at rung 1
  ]);

  // No ladder governing -> zero-cost back-compat, same array reference, everything drawn.
  assert.strictEqual(api.sceneFilterPointsByQualityGroups(points, null, pointQualityGroups), points,
    "no ladder -> same array reference, zero-cost back-compat");

  const filtered = api.sceneFilterPointsByQualityGroups(points, admitted, pointQualityGroups);
  assert.deepEqual(Array.from(filtered), [heroDust, farDust, nebulaLayer],
    "untagged points always survive; own qualityGroup gates far-dust/decor-dust; name-mapped nebula layer admitted, spare layer dropped");
  assert.equal(filtered.qualitySkippedCount, 2, "decor-dust and spare-layer were both dropped by the group filter");

  // Own qualityGroup wins over the name-mapped fallback when both are present.
  const overridden = { id: "override", qualityGroup: "particles", material: "spare-layer" };
  assert.equal(api.sceneEffectivePointQualityGroup(overridden, pointQualityGroups), "particles",
    "authored qualityGroup takes precedence over the pointQualityGroups name mapping");

  // Entries with no own tag and no name mapping are untagged (always visible).
  const unmapped = { id: "unmapped", material: "unrelated-layer" };
  assert.equal(api.sceneEffectivePointQualityGroup(unmapped, pointQualityGroups), "");

  const admittedOnly = [heroDust, farDust, nebulaLayer];
  const noFilterNeeded = api.sceneFilterPointsByQualityGroups(admittedOnly, admitted, pointQualityGroups);
  assert.strictEqual(noFilterNeeded, admittedOnly, "nothing dropped -> same array reference, no allocation on the hot per-frame path");
  assert.equal(noFilterNeeded.qualitySkippedCount, 0);
});

test("Scene3D QualityLadder: PointBudgetScale keeps every admitted layer and samples each GLB point array deterministically", () => {
  const { api } = loadSceneAdaptiveQualityAPI();
  const ladderState = { mode: "ladder", ladder: THREE_RUNG_LADDER.map(function(r) {
    return { name: r.name, layerGroups: r.layerGroups || [], postEffects: r.postEffects || [], pointBudgetScale: r.pointBudgetScale || 1 };
  }), rungIndex: 1 };

  assert.equal(api.sceneQualityLadderPointBudgetScale(null), 1);
  assert.equal(api.sceneQualityLadderPointBudgetScale(ladderState), 0.5);

  const core = {
    id: "core-points",
    count: 8,
    positions: new Float32Array([
      0, 0, 0, 1, 0, 0, 2, 0, 0, 3, 0, 0,
      4, 0, 0, 5, 0, 0, 6, 0, 0, 7, 0, 0,
    ]),
    sizes: new Float32Array([1, 2, 3, 4, 5, 6, 7, 8]),
    colors: new Float32Array([
      0, 0, 0, 1, 1, 0, 0, 1, 2, 0, 0, 1, 3, 0, 0, 1,
      4, 0, 0, 1, 5, 0, 0, 1, 6, 0, 0, 1, 7, 0, 0, 1,
    ]),
  };
  const detail = { id: "detail-points", count: 4, positions: new Float32Array(12) };
  const scaled = api.sceneApplyPointBudgetScale([core, detail], 0.5);

  assert.equal(scaled.length, 2, "every admitted layer remains present");
  assert.equal(scaled[0].count, 4);
  assert.equal(scaled[1].count, 2);
  assert.deepEqual(Array.from(scaled[0].positions), [
    1, 0, 0, 3, 0, 0, 5, 0, 0, 7, 0, 0,
  ], "bucket midpoint sampling must preserve spread, not take a prefix");
  assert.deepEqual(Array.from(scaled[0].sizes), [2, 4, 6, 8]);
  assert.equal(scaled.qualityPointBudgetScale, 0.5);
  assert.equal(scaled.qualityPointAuthoredInstances, 12);
  assert.equal(scaled.qualityPointDrawInstances, 6);
  assert.equal(scaled.qualityPointBudgetScaledEntries, 2);
  assert.strictEqual(api.sceneApplyPointBudgetScale([core], 1)[0], core, "scale=1 leaves entries untouched");
});

test("Scene3D QualityLadder: PointBudgetScale refreshes mutable typed-array samples", () => {
  const { api } = loadSceneAdaptiveQualityAPI();
  const entry = {
    id: "dynamic-points",
    count: 4,
    positions: new Float32Array([
      0, 0, 0, 1, 0, 0, 2, 0, 0, 3, 0, 0,
    ]),
    sizes: new Float32Array([1, 2, 3, 4]),
    colors: new Float32Array([
      0, 0, 0, 1, 1, 0, 0, 1, 2, 0, 0, 1, 3, 0, 0, 1,
    ]),
  };

  const first = api.sceneApplyPointBudgetScale([entry], 0.5)[0];
  const firstPositions = first.positions;
  assert.deepEqual(Array.from(first.positions), [1, 0, 0, 3, 0, 0]);

  entry.positions[3] = 10;
  entry.positions[9] = 30;
  entry.sizes[1] = 20;
  entry.sizes[3] = 40;
  entry.colors[4] = 10;
  entry.colors[12] = 30;
  const second = api.sceneApplyPointBudgetScale([entry], 0.5)[0];

  assert.strictEqual(second.positions, firstPositions, "stable output buffer may be reused");
  assert.deepEqual(Array.from(second.positions), [10, 0, 0, 30, 0, 0], "same-source mutations must refresh sampled positions");
  assert.deepEqual(Array.from(second.sizes), [20, 40], "same-source mutations must refresh sampled sizes");
  assert.equal(second.colors[0], 10);
  assert.equal(second.colors[4], 30);
});

test("Scene3D QualityLadder: QualityStartRung on a rung with empty/absent LayerGroups admits everything from frame one", () => {
  const { state, api } = createQualityLadderHarness(RAW_TO_GLOW_LADDER, {
    qualityStartRung: 0,
  });
  // No transition (promotion/demotion) has happened yet — this is the raw
  // init-time rung, exactly as QualityStartRung configured it.
  assert.equal(state.rungIndex, 0);
  assert.equal(state.rungReason, "initial");

  const admitted = api.sceneQualityLadderAdmittedGroups(state);
  assert.equal(admitted, null,
    "an active rung with no LayerGroups authored must yield the admit-all sentinel (null), not an empty-but-truthy array");

  const untagged = { id: "hero" };
  const tagged = { id: "far-star", qualityGroup: "particles" };
  const objects = [untagged, tagged];
  assert.deepEqual(api.sceneFilterObjectsByQualityGroups(objects, admitted), objects,
    "both untagged and tagged mesh objects must draw when the active rung has no LayerGroups, from frame one");

  const heroDust = { id: "hero-dust" };
  const farDust = { id: "far-dust", qualityGroup: "particles" };
  const points = [heroDust, farDust];
  const filteredPoints = api.sceneFilterPointsByQualityGroups(points, admitted, new Map());
  assert.deepEqual(Array.from(filteredPoints), [heroDust, farDust],
    "tagged points must also draw when the active rung has no LayerGroups, from frame one");
  assert.equal(filteredPoints.qualitySkippedCount, 0);
});

test("Scene3D QualityLadder: QualityStartRung on a rung WITH explicit LayerGroups still filters correctly from frame one", () => {
  const { state, api } = createQualityLadderHarness(RAW_TO_GLOW_LADDER, {
    qualityStartRung: 1,
  });
  assert.equal(state.rungIndex, 1);
  assert.equal(state.rungReason, "initial");

  const admitted = api.sceneQualityLadderAdmittedGroups(state);
  assert.deepEqual(admitted, ["particles"]);

  const untagged = { id: "hero" };
  const admittedTag = { id: "far-star", qualityGroup: "particles" };
  const rejectedTag = { id: "decor", qualityGroup: "far-decor" };
  const objects = [untagged, admittedTag, rejectedTag];
  assert.deepEqual(api.sceneFilterObjectsByQualityGroups(objects, admitted), [untagged, admittedTag],
    "an explicit-LayerGroups start rung must still gate tagged objects not in the admitted set");

  const heroDust = { id: "hero-dust" };
  const farDust = { id: "far-dust", qualityGroup: "particles" };
  const decorDust = { id: "decor-dust", qualityGroup: "far-decor" };
  const points = [heroDust, farDust, decorDust];
  const filteredPoints = api.sceneFilterPointsByQualityGroups(points, admitted, new Map());
  assert.deepEqual(Array.from(filteredPoints), [heroDust, farDust],
    "an explicit-LayerGroups start rung must still gate tagged points not in the admitted set");
  assert.equal(filteredPoints.qualitySkippedCount, 1);
});

// --- adaptiveQuality+QualityLadder warning fix ---
test("Scene3D adaptiveQuality+QualityLadder warning fires only for explicit tier config, silent on plain framework defaults", async () => {
  async function mountWithProps(id, sceneProps, extraProps) {
    const mount = new FakeElement("div", null);
    mount.id = id;
    const env = createContext({
      elements: [mount],
      enableWebGL: true,
      disableCanvas2D: true,
      manifest: {
        engines: [
          {
            id: "engine-" + id,
            component: "GoSXScene3D",
            kind: "surface",
            mountId: id,
            jsExport: "GoSXScene3D",
            props: Object.assign({ width: 320, height: 240, autoRotate: false, scene: sceneProps }, extraProps || {}),
            capabilities: ["canvas", "webgl", "animation"],
          },
        ],
      },
    });
    const warnLog = [];
    const origWarn = env.context.console.warn;
    env.context.console.warn = function() {
      warnLog.push(Array.from(arguments).join(" "));
      if (typeof origWarn === "function") origWarn.apply(env.context.console, arguments);
    };
    runScript(bootstrapSource, env.context, "bootstrap.js");
    await flushAsyncWork();
    return { env, mount, warnLog };
  }

  function ladderWarnings(warnLog) {
    return warnLog.filter(function(w) { return w.indexOf("QualityLadder overrides adaptiveQuality") !== -1; });
  }

  const ladder = [{ name: "raw" }, { name: "glow", postEffects: ["bloom"] }];

  // Plain adaptiveQuality:true is every scene's framework-default opt-in
  // (built-in full/balanced/survival presets, no author-configured tier
  // substance) — authoring it alongside a ladder must stay silent.
  const plain = await mountWithProps("scene-ladder-warn-plain", { qualityLadder: ladder }, { adaptiveQuality: true });
  assert.equal(ladderWarnings(plain.warnLog).length, 0,
    "plain adaptiveQuality:true (framework defaults) + ladder must NOT warn");

  // An explicitly requested tier is real authored substance the ladder
  // strands — this must warn.
  const explicitTier = await mountWithProps("scene-ladder-warn-tier", { qualityLadder: ladder }, { adaptiveQuality: true, adaptiveQualityTier: "balanced" });
  assert.equal(ladderWarnings(explicitTier.warnLog).length, 1,
    "explicit adaptiveQualityTier alongside a ladder must warn exactly once");

  // An explicit profile override is likewise real authored substance.
  const explicitProfiles = await mountWithProps("scene-ladder-warn-profiles", { qualityLadder: ladder }, {
    adaptiveQuality: true,
    qualityProfiles: { full: { dprCap: 2 } },
  });
  assert.equal(ladderWarnings(explicitProfiles.warnLog).length, 1,
    "explicit qualityProfiles override alongside a ladder must warn exactly once");

  // No adaptiveQuality authored at all + a ladder must also stay silent
  // (back-compat: nothing to strand).
  const noAdaptive = await mountWithProps("scene-ladder-warn-none", { qualityLadder: ladder }, {});
  assert.equal(ladderWarnings(noAdaptive.warnLog).length, 0,
    "no adaptiveQuality authored + ladder must NOT warn");
});
