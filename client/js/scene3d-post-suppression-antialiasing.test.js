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
// Every post program a chain can hold here, for the ordered list of passes in a frame.
const PASS_MARKERS = {tone: ["vec3 filmic(vec3 x)"], fxaa: ["float greenLuma(vec3 c)"], vignette: ["float vignette = 1.0 - smoothstep"],
  colorGrade: ["uniform float u_saturation"], bloom: ["brightness > u_threshold", "uniform vec2 u_direction", "uniform sampler2D u_bloomTexture"]};
async function renderOneFrame(scene) {
  const first = scene.gl.ops.length;
  await frames(scene, 1, 100);
  const ops = scene.gl.ops.slice(first);
  const ids = Object.fromEntries(Object.entries(POST_MARKERS).map(([name, marker]) => [name, (scene.gl.programMatching(marker) || {}).id]));
  const passByProgram = new Map();
  for (const [name, markers] of Object.entries(PASS_MARKERS)) for (const marker of markers) {
    const program = scene.gl.programMatching(marker);
    if (program) passByProgram.set(program.id, name);
  }
  const draws = {tone: 0, fxaa: 0};
  const order = [], post = [];
  for (const op of ops) {
    if (op[0] !== "drawArrays") continue;
    for (const name of Object.keys(draws)) if (ids[name] !== undefined && op[4] === ids[name]) { draws[name]++; order.push(name); }
    if (passByProgram.has(op[4])) post.push(passByProgram.get(op[4]));
  }
  // The renderer uploads this uniform when its value changes, so read the latest value in the whole log.
  const linear = scene.gl.ops.filter(op => op[0] === "uniform1i" && op[1] === "u_outputLinear");
  return {draws, order, post, links: ops.filter(op => op[0] === "linkProgram").length, outputLinear: linear.length ? linear.at(-1)[2] : null};
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

// The FXAA pass that suppression appends is a cost too. It reads nine texels per
// pixel where a vignette or a color grade reads one, so a suppression that sheds
// only those and then draws FXAA does the frame no good. It spends the last
// quality step (a revision, the 5 s cooldown and a transition event), removes
// the author's effect, and draws no fewer passes than before. The controller
// asks the renderer whether it will add the pass, and does not suppress unless
// something heavier than a single-pass color effect leaves.
const tone = {kind: "toneMapping", mode: "aces"};
const CANVASES = {"private fallback": {}, "own MSAA": {msaaSamples: 4}, "no antialiasing": {antialias: false}};

test("suppression does not trade a vignette or color grade for the FXAA pass the fallback adds", async () => {
  for (const effects of [[{kind: "vignette"}, tone], [{kind: "colorGrade"}, tone], [{kind: "vignette"}, {kind: "colorGrade"}, tone]]) {
    for (const [name, extra] of Object.entries(CANVASES).filter(([, extra]) => extra.antialias !== false)) {
      const label = `${effects.map(effect => effect.kind)} on the ${name} canvas`;
      const s = await mountScene(effects, extra);
      const survival = await renderOneFrame(s);
      const revision = attr(s, "quality-revision");
      await missFrames(s, 60);
      assert.equal(attr(s, "quality-postfx-suppressed"), "false", `${label}: suppression would swap the effect for FXAA`);
      assert.equal(attr(s, "quality-reason"), "initial", `${label}: no postfx transition is recorded`);
      assert.equal(attr(s, "quality-revision"), revision, `${label}: no quality revision is spent`);
      const after = await renderOneFrame(s);
      assert.deepEqual(after.post, survival.post, `${label}: the frame draws the survival chain`);
      assert.deepEqual(survival.post.slice(-1), ["tone"]);
      assert.equal(s.env.consoleLogs.error.length, 0);
    }
  }
});

test("suppression sheds a single-pass color effect on a canvas that gets no FXAA", async () => {
  const s = await mountScene([{kind: "vignette"}, tone], {antialias: false});
  const survival = await renderOneFrame(s);
  assert.deepEqual(survival.post, ["vignette", "tone"]);
  await missFrames(s);
  assert.equal(attr(s, "quality-postfx-suppressed"), "true", "nothing is added, so the vignette is a pass saved");
  assert.equal(chain(s), "toneMapping");
  const after = await renderOneFrame(s);
  assert.deepEqual(after.post, ["tone"]);
  assert.equal(s.env.consoleLogs.error.length, 0);
});

test("suppression draws fewer post passes than the survival frame whenever it takes the step", async () => {
  const bloom = {kind: "bloom"}, vignette = {kind: "vignette"}, grade = {kind: "colorGrade"}, fxaa = {kind: "fxaa"};
  const chains = [
    [bloom, tone], [bloom, vignette, tone], [bloom, tone, fxaa], [vignette, tone, fxaa], [grade, tone, fxaa],
    [bloom], [bloom, fxaa], [vignette], [vignette, fxaa], [grade], [fxaa],
    [vignette, tone], [grade, tone], [vignette, grade, tone], [tone], [tone, fxaa],
  ];
  const outcomes = new Set();
  for (const [name, extra] of Object.entries(CANVASES)) {
    for (const effects of chains) {
      const label = `${effects.map(effect => effect.kind)} on the ${name} canvas`;
      const s = await mountScene(effects, extra);
      const survival = await renderOneFrame(s);
      await missFrames(s, 60);
      const suppressed = attr(s, "quality-postfx-suppressed") === "true";
      const after = await renderOneFrame(s);
      outcomes.add(suppressed);
      if (suppressed) assert.ok(after.post.length < survival.post.length, `${label}: suppressed to ${after.post}, from ${survival.post}`);
      else assert.deepEqual(after.post, survival.post, `${label}: not suppressed, so the frame is unchanged`);
      assert.equal(s.env.consoleLogs.error.length, 0, label);
    }
  }
  assert.deepEqual([...outcomes].sort(), [false, true], "the sweep reaches both outcomes");
});

test("suppression with no tone mapping does not replace a vignette with the fallback's FXAA", async () => {
  const fallback = await mountScene([{kind: "vignette"}]);
  const survival = await renderOneFrame(fallback);
  assert.deepEqual(survival.post, ["vignette"]);
  await missFrames(fallback, 60);
  assert.equal(attr(fallback, "quality-postfx-suppressed"), "false", "FXAA alone would read nine texels where the vignette reads one");
  assert.deepEqual((await renderOneFrame(fallback)).post, ["vignette"]);
  // Where the empty chain draws straight to the canvas, shedding the vignette is a pass saved.
  for (const extra of [{antialias: false}, {msaaSamples: 4}]) {
    const s = await mountScene([{kind: "vignette"}], extra);
    await missFrames(s, 60);
    assert.equal(attr(s, "quality-postfx-suppressed"), "true", JSON.stringify(extra));
    assert.deepEqual((await renderOneFrame(s)).post, [], JSON.stringify(extra));
  }
});

test("a canvas with its own MSAA compiles the FXAA program before suppression needs it", async () => {
  const fxaaMarker = POST_MARKERS.fxaa;
  const s = await mountScene([{kind: "bloom"}, tone], {msaaSamples: 4});
  assert.ok(s.gl.programMatching(fxaaMarker), "the suppressed chain will draw FXAA, so the program exists from startup");
  await missFrames(s);
  assert.equal(attr(s, "quality-postfx-suppressed"), "true");
  const frame = await renderOneFrame(s);
  assert.deepEqual(frame.post, ["tone", "fxaa"]);
  assert.equal(frame.links, 0, "the first suppressed frame links no program on a device that is already over budget");
  // A canvas that adds no FXAA does not pay for the program.
  const plain = await mountScene([{kind: "bloom"}, tone], {antialias: false});
  assert.equal(plain.gl.programMatching(fxaaMarker), null);
});
