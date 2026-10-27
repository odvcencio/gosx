"use strict";
// Adaptive quality suppresses the post chain as its last step, but keeps tone
// mapping so an HDR frame still maps to the display. A chain that is not empty
// draws into the single-sample post target, which drops the antialiasing the
// canvas would have had (its own MSAA, or the private FXAA fallback). These
// tests mount the real bootstrap bundle, let the real controller suppress, and
// read what the real renderer draws.
const test = require("node:test");
const assert = require("node:assert/strict");
const H = require("./runtime-test-harness.js");

const triangle = {id: "triangle", kind: "gltf-mesh", materialKind: "standard", wireframe: false,
  vertices: {positions: [-1, -1, 0, 1, -1, 0, 0, 1, 0], normals: [0, 0, 1, 0, 0, 1, 0, 0, 1]}};

async function mountScene(postEffects, extra = {}) {
  const mount = new H.FakeElement("div", null);
  mount.id = "post-suppression";
  // The controller times its cooldown with performance.now(); drive it from the frame clock.
  const scene = {mount, clock: 32};
  const env = H.createContext({elements: [mount], enableWebGL2: true, disableCanvas2D: true, performanceNow: () => scene.clock,
    manifest: {engines: [{id: "post-engine", component: "GoSXScene3D", kind: "surface", mountId: mount.id,
      props: {width: 640, height: 360, adaptiveQuality: true, qualityTier: "survival", adaptivePostFX: true,
        adaptiveTargetFrameMS: 40, adaptiveWarmupFrames: 0, autoRotate: true, scene: {objects: [triangle], postEffects}, ...extra}}]}});
  env.context.WebGL2RenderingContext = H.FakeWebGLContext;
  const raf = H.installManualRAF(env.context);
  H.runScript(H.bootstrapSource, env.context, "bootstrap.js");
  for (let i = 0; i < 12; i++) await H.flushAsyncWork();
  await H.flushSceneInitialFrameBoundary(raf);
  const canvas = mount.children[0];
  Object.assign(scene, {canvas, gl: canvas._webglContext, raf, env});
  scene.contextOptions = canvas.contextCalls.filter(c => c.kind === "webgl2").at(-1).options;
  return scene;
}

async function frames(scene, count, stepMS) {
  for (let i = 0; i < count; i++) {
    scene.clock += stepMS;
    scene.raf.flush(scene.clock);
    await H.flushAsyncWork();
  }
}

// Frames 100 ms apart miss the 40 ms budget, so the controller runs out of
// tiers (it starts at survival) and suppresses post effects. Frames 16 ms apart
// stay well inside it, whatever the jitter of the fake frame loop.
async function missFrames(scene, count = 40) { await frames(scene, count, 100); }

const attr = (scene, name) => scene.mount.getAttribute("data-gosx-scene3d-" + name);
const chain = scene => Array.from(scene.mount.__gosxScene3DState.postEffects, effect => effect.kind).join(",");

// Draws per built-in post program in one rendered frame. The marker names a
// line of that program's GLSL, so the lookup does not depend on creation order.
const POST_MARKERS = {tone: "vec3 filmic(vec3 x)", fxaa: "float greenLuma(vec3 c)"};
async function renderOneFrame(scene) {
  const first = scene.gl.ops.length;
  await frames(scene, 1, 100);
  const ops = scene.gl.ops.slice(first);
  const ids = Object.fromEntries(Object.entries(POST_MARKERS).map(([name, marker]) => [name, (scene.gl.programMatching(marker) || {}).id]));
  const draws = {tone: 0, fxaa: 0};
  const order = [];
  for (const op of ops) {
    if (op[0] !== "drawArrays") continue;
    for (const name of Object.keys(draws)) if (ids[name] !== undefined && op[4] === ids[name]) { draws[name]++; order.push(name); }
  }
  // The renderer uploads this uniform when its value changes, so read the latest value in the whole log.
  const linear = scene.gl.ops.filter(op => op[0] === "uniform1i" && op[1] === "u_outputLinear");
  return {draws, order, outputLinear: linear.length ? linear.at(-1)[2] : null};
}

test("suppression keeps FXAA after tone mapping on the canvas the private fallback antialiases", async () => {
  const s = await mountScene([{kind: "bloom"}, {kind: "toneMapping", mode: "aces"}]);
  assert.equal(s.contextOptions.antialias, false, "post effects are authored, so the canvas has no MSAA");
  assert.equal(attr(s, "antialiasing"), "none", "before suppression the chain has no edge pass");
  await missFrames(s);
  assert.equal(attr(s, "quality-postfx-suppressed"), "true");
  assert.equal(chain(s), "toneMapping", "the author's chain keeps tone mapping and sheds bloom");
  const frame = await renderOneFrame(s);
  assert.equal(attr(s, "antialiasing"), "fxaa", "main antialiased this frame through the private fallback");
  assert.deepEqual(frame.order, ["tone", "fxaa"], "the frame is tone mapped, then edge smoothed");
  assert.deepEqual(frame.draws, {tone: 1, fxaa: 1});
  assert.equal(frame.outputLinear, 1, "scene and sky draw linear light for the tone-mapping pass");
  assert.equal(s.env.consoleLogs.error.length, 0);
});

test("suppression keeps an authored FXAA once and does not add a second", async () => {
  const s = await mountScene([{kind: "bloom"}, {kind: "toneMapping", mode: "aces"}, {kind: "fxaa"}]);
  assert.equal(attr(s, "antialiasing"), "fxaa");
  await missFrames(s);
  assert.equal(attr(s, "quality-postfx-suppressed"), "true");
  assert.equal(chain(s), "toneMapping,fxaa");
  const frame = await renderOneFrame(s);
  assert.equal(attr(s, "antialiasing"), "fxaa");
  assert.deepEqual(frame.draws, {tone: 1, fxaa: 1});
  assert.deepEqual(frame.order, ["tone", "fxaa"]);
  assert.equal(s.env.consoleLogs.error.length, 0);
});

test("suppression keeps antialiasing on a canvas with its own MSAA", async () => {
  const s = await mountScene([{kind: "bloom"}, {kind: "toneMapping", mode: "aces"}], {msaaSamples: 4});
  assert.equal(s.contextOptions.antialias, true, "an explicit sample count keeps canvas MSAA beside the chain");
  await missFrames(s);
  assert.equal(attr(s, "quality-postfx-suppressed"), "true");
  assert.equal(chain(s), "toneMapping");
  const frame = await renderOneFrame(s);
  assert.equal(attr(s, "antialiasing"), "fxaa", "the single-sample post target drops the canvas MSAA, so an edge pass runs");
  assert.deepEqual(frame.order, ["tone", "fxaa"]);
  assert.equal(s.env.consoleLogs.error.length, 0);
});

test("suppression adds no edge pass to a canvas that never had antialiasing", async () => {
  for (const extra of [{antialias: false}, {msaaSamples: 1}]) {
    const s = await mountScene([{kind: "bloom"}, {kind: "toneMapping", mode: "aces"}], extra);
    await missFrames(s);
    assert.equal(attr(s, "quality-postfx-suppressed"), "true", JSON.stringify(extra));
    assert.equal(chain(s), "toneMapping");
    const frame = await renderOneFrame(s);
    assert.equal(attr(s, "antialiasing"), "none", `${JSON.stringify(extra)}: main drew these frames without edge smoothing`);
    assert.deepEqual(frame.draws, {tone: 1, fxaa: 0});
    assert.equal(s.env.consoleLogs.error.length, 0);
  }
});

test("a chain without tone mapping still suppresses to the display-space FXAA fallback", async () => {
  const s = await mountScene([{kind: "bloom"}, {kind: "fxaa"}]);
  await missFrames(s);
  assert.equal(attr(s, "quality-postfx-suppressed"), "true");
  assert.equal(chain(s), "", "FXAA alone would leave the frame in linear light");
  const frame = await renderOneFrame(s);
  assert.equal(attr(s, "antialiasing"), "fxaa");
  assert.equal(frame.outputLinear, 0, "the fallback filters display colors");
  assert.deepEqual(frame.draws, {tone: 0, fxaa: 1});
  assert.equal(s.env.consoleLogs.error.length, 0);
});

test("a chain with nothing to shed never suppresses", async () => {
  for (const effects of [[{kind: "toneMapping", mode: "aces"}], [{kind: "toneMapping", mode: "aces"}, {kind: "fxaa"}]]) {
    const s = await mountScene(effects);
    const before = chain(s);
    const revision = attr(s, "quality-revision");
    await missFrames(s, 60);
    assert.equal(attr(s, "quality-postfx-suppressed"), "false", before);
    assert.equal(attr(s, "quality-reason"), "initial", "no postfx transition is recorded");
    assert.equal(attr(s, "quality-revision"), revision, "no quality revision is spent");
    assert.equal(chain(s), before);
    assert.equal(s.env.consoleLogs.error.length, 0);
  }
});

test("recovery clears the suppressed state, so the unsuppressed chain draws no appended FXAA", async () => {
  const s = await mountScene([{kind: "bloom"}, {kind: "toneMapping", mode: "aces"}]);
  await missFrames(s);
  assert.equal(attr(s, "quality-postfx-suppressed"), "true");
  assert.equal((await renderOneFrame(s)).draws.fxaa, 1);
  // The slow frames leave the 120-frame window, then 300 clean frames after the 5 s cooldown restore the chain.
  await frames(s, 600, 16);
  assert.equal(attr(s, "quality-postfx-suppressed"), "false", "sustained clean frames restore the chain");
  assert.equal(chain(s), "bloom,toneMapping");
  const frame = await renderOneFrame(s);
  assert.equal(attr(s, "antialiasing"), "none", "the unsuppressed chain has no edge pass, as before");
  assert.deepEqual(frame.draws, {tone: 1, fxaa: 0});
  assert.equal(s.env.consoleLogs.error.length, 0);
});
