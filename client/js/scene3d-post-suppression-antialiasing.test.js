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

// The sky renderer asks for a sampler, which the fake context does not model (scene3d-sky-physical.test.js stubs it the same way).
Object.assign(H.FakeWebGLContext.prototype, {createSampler: () => ({}), deleteSampler() {}, samplerParameteri() {}, bindSampler() {}});
const PHYSICAL_SKY = {sky: {mode: "physical", sunDirection: {x: 0, y: 0.3, z: -1}}};

async function mountScene(postEffects, extra = {}, environment = null) {
  const mount = new H.FakeElement("div", null);
  mount.id = "post-suppression";
  // The controller times its cooldown with performance.now(); drive it from the frame clock.
  const scene = {mount, clock: 32};
  const env = H.createContext({elements: [mount], enableWebGL2: true, disableCanvas2D: true, performanceNow: () => scene.clock,
    manifest: {engines: [{id: "post-engine", component: "GoSXScene3D", kind: "surface", mountId: mount.id,
      props: {width: 640, height: 360, adaptiveQuality: true, qualityTier: "survival", adaptivePostFX: true,
        adaptiveTargetFrameMS: 40, adaptiveWarmupFrames: 0, autoRotate: true, scene: {objects: [triangle], postEffects, ...(environment && {environment})}, ...extra}}]}});
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
  colorGrade: ["uniform float u_saturation"], bloom: ["brightness > u_threshold", "uniform vec2 u_direction", "uniform sampler2D u_bloomTexture"],
  atmosphere: ["float atmosphereHash"]};
async function renderOneFrame(scene) {
  const first = scene.gl.ops.length;
  await frames(scene, 1, 100);
  const ops = scene.gl.ops.slice(first);
  const ids = Object.fromEntries(Object.entries(POST_MARKERS).map(([name, marker]) => [name, (scene.gl.programMatching(marker) || {}).id]));
  const passByProgram = new Map();
  for (const [name, markers] of Object.entries(PASS_MARKERS)) for (const marker of markers) {
    for (const program of scene.gl.programs) if (scene.gl.programShaderSources(program).includes(marker)) passByProgram.set(program.id, name);
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

// Whatever the chain, whenever the controller takes the last step the frame must
// draw fewer post passes than the survival frame did, and otherwise it must be
// unchanged. Returns the outcomes reached, so a sweep can check that it reaches both.
async function sweepFewerPasses(chains, environment) {
  const outcomes = new Set();
  for (const [name, extra] of Object.entries(CANVASES)) {
    for (const effects of chains) {
      const label = `${effects.map(effect => effect.kind)} on the ${name} canvas`;
      const s = await mountScene(effects, extra, environment);
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
  return [...outcomes].sort();
}

test("suppression draws fewer post passes than the survival frame whenever it takes the step", async () => {
  const bloom = {kind: "bloom"}, vignette = {kind: "vignette"}, grade = {kind: "colorGrade"}, fxaa = {kind: "fxaa"};
  const chains = [
    [bloom, tone], [bloom, vignette, tone], [bloom, tone, fxaa], [vignette, tone, fxaa], [grade, tone, fxaa],
    [bloom], [bloom, fxaa], [vignette], [vignette, fxaa], [grade], [fxaa],
    [vignette, tone], [grade, tone], [vignette, grade, tone], [tone], [tone, fxaa],
  ];
  assert.deepEqual(await sweepFewerPasses(chains), [false, true], "the sweep reaches both outcomes");
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

// The renderer drops effects that the mount's chain still holds: god rays and
// film grain at the survival tier, god rays when the scene has no physical sky,
// a specular bloom the draw path cannot run, and any kind it does not know.
// Weighing the mount's chain counted passes that were never drawn, so the
// controller spent its last step to trade the author's vignette for an FXAA
// pass: a frame of vignette and tone mapping became tone mapping and FXAA. The
// controller weighs the chain the renderer drew instead.
const E = kind => ({kind});
const NEVER_DRAWN = {
  "god rays at the survival tier": {effects: [E("godRays"), E("vignette"), tone], sky: true, survival: ["vignette", "tone"]},
  "god rays beside tone mapping": {effects: [E("godRays"), tone], sky: true, survival: ["tone"]},
  "god rays with no physical sky": {effects: [E("godRays"), E("vignette"), tone], sky: false, survival: ["vignette", "tone"]},
  "film grain at the survival tier": {effects: [E("grain"), E("vignette"), E("colorGrade"), tone], sky: false, survival: ["vignette", "colorGrade", "tone"]},
  // The fake context has no EXT_color_buffer_float, the same draw path that scenes with an ocean, water, clouds, lines or points take.
  "a specular bloom the draw path cannot run": {effects: [{kind: "bloom", source: "specular"}, tone], sky: false, survival: ["tone"]},
  "an effect kind no renderer knows": {effects: [E("sharpen"), tone], sky: false, survival: ["tone"]},
};

test("suppression does not spend the last step on effects the renderer never draws", async () => {
  for (const [name, c] of Object.entries(NEVER_DRAWN)) {
    for (const [canvas, extra] of Object.entries({"private fallback": {}, "own MSAA": {msaaSamples: 4}})) {
      const label = `${name} on the ${canvas} canvas`;
      const s = await mountScene(c.effects, extra, c.sky ? PHYSICAL_SKY : null);
      const survival = await renderOneFrame(s);
      assert.deepEqual(survival.post, c.survival, `${label}: the survival frame draws only these passes`);
      if (name.startsWith("a specular")) assert.equal(attr(s, "specular-bloom"), "unsupported-draw-path", label);
      const revision = attr(s, "quality-revision");
      await missFrames(s, 60);
      assert.equal(attr(s, "quality-postfx-suppressed"), "false", `${label}: suppression would swap drawn passes for FXAA`);
      assert.equal(attr(s, "quality-reason"), "initial", `${label}: no postfx transition is recorded`);
      assert.equal(attr(s, "quality-revision"), revision, `${label}: no quality revision is spent`);
      assert.deepEqual((await renderOneFrame(s)).post, survival.post, `${label}: the frame is unchanged`);
      assert.equal(s.env.consoleLogs.error.length, 0, label);
    }
  }
});

test("a scene that draws god rays at the full tier and drops them at survival is not suppressed", async () => {
  const s = await mountScene([E("godRays"), E("vignette"), tone], {qualityTier: "full"}, PHYSICAL_SKY);
  assert.equal(attr(s, "quality-tier"), "full");
  assert.ok((await renderOneFrame(s)).post.includes("atmosphere"), "the full tier draws the god-ray pass");
  // Frames 100 ms apart miss the 40 ms budget: full, then balanced, then survival.
  for (let i = 0; i < 200 && attr(s, "quality-tier") !== "survival"; i++) await frames(s, 1, 100);
  assert.equal(attr(s, "quality-tier"), "survival");
  assert.equal(attr(s, "quality-postfx-suppressed"), "false");
  const survival = await renderOneFrame(s);
  assert.deepEqual(survival.post, ["vignette", "tone"], "the survival tier turns god rays off");
  const revision = attr(s, "quality-revision");
  await missFrames(s, 120);
  assert.equal(attr(s, "quality-postfx-suppressed"), "false", "the vignette stays: FXAA would cost more");
  assert.equal(attr(s, "quality-revision"), revision, "the walk spent two steps and suppression none");
  assert.deepEqual((await renderOneFrame(s)).post, survival.post);
  assert.equal(s.env.consoleLogs.error.length, 0);
});

test("suppression still sheds god rays that the renderer draws at the survival tier", async () => {
  // A balanced device keeps god rays, at 12 samples, at every tier.
  const s = await mountScene([E("godRays"), tone], {capabilityTier: "balanced", antialias: true}, PHYSICAL_SKY);
  const survival = await renderOneFrame(s);
  assert.deepEqual(survival.post, ["atmosphere", "atmosphere", "tone"], "the balanced device draws the two god-ray draws at survival");
  await missFrames(s, 60);
  assert.equal(attr(s, "quality-postfx-suppressed"), "true", "god rays weigh more than the FXAA pass that replaces them");
  assert.equal(chain(s), "toneMapping");
  assert.deepEqual((await renderOneFrame(s)).post, ["tone", "fxaa"]);
  assert.equal(s.env.consoleLogs.error.length, 0);
});

test("suppression draws fewer post passes whenever it takes the step, over chains with effects the renderer drops", async () => {
  const godRays = E("godRays"), grain = E("grain"), vignette = E("vignette"), grade = E("colorGrade"), bloom = E("bloom"), sharpen = E("sharpen");
  const specular = {kind: "bloom", source: "specular"};
  const chains = [
    [godRays], [godRays, tone], [godRays, vignette, tone], [godRays, bloom, tone], [godRays, vignette, grade, tone],
    [grain, tone], [grain, vignette, grade, tone], [grain, bloom, tone],
    [specular, tone], [specular, vignette, tone], [specular], [sharpen], [sharpen, tone], [sharpen, vignette, tone], [sharpen, bloom, tone],
  ];
  assert.deepEqual(await sweepFewerPasses(chains, PHYSICAL_SKY), [false, true], "the sweep reaches both outcomes");
});

test("the renderer reports the chain it drew, after it dropped the effects it cannot run", () => {
  const h = H.createWebGLRendererForPost({fresh: true});
  const mount = h.env.document.createElement("div");
  mount.appendChild(h.canvas);
  assert.equal(h.renderer.drawnPostEffects(), null, "no frame has been drawn");
  const bundle = () => {
    const b = H.makePointsBundle(null);
    b.points = [];
    b.environment.sky = PHYSICAL_SKY.sky;
    b.postEffects = [E("godRays"), E("vignette"), tone, E("sharpen")];
    return b;
  };
  const kinds = () => Array.from(h.renderer.drawnPostEffects(), effect => effect.kind);
  h.renderer.render(bundle(), {width: 320, height: 180}, {atmosphereTier: "survival"});
  assert.deepEqual(kinds(), ["vignette", "toneMapping", "sharpen"], "survival drops the god rays; a kind it does not know stays, and the mount weighs it as nothing");
  h.renderer.render(bundle(), {width: 320, height: 180}, {atmosphereTier: "full"});
  assert.deepEqual(kinds(), ["atmosphere", "vignette", "toneMapping", "sharpen"], "the full tier fuses the god rays into one atmosphere effect");
  assert.ok(h.renderer.drawnPostEffects()[0].rays, "the atmosphere effect carries the rays");
  h.renderer.dispose();
});

// Bloom at a scale below the default 0.5 shrinks three of its four draws. On an
// integrated GPU it then costs about what the FXAA pass costs, so swapping it
// for FXAA saves nothing (between 0.4 percent cheaper and 2 percent dearer at 0.35).
test("suppression keeps a bloom drawn at a small scale and sheds one drawn at the default", async () => {
  for (const [canvas, extra] of Object.entries({"private fallback": {}, "own MSAA": {msaaSamples: 4}})) {
    const small = await mountScene([{kind: "bloom", scale: 0.35}, tone], extra);
    const survival = await renderOneFrame(small);
    assert.deepEqual(survival.post, ["bloom", "bloom", "bloom", "bloom", "tone"], canvas);
    const revision = attr(small, "quality-revision");
    await missFrames(small, 60);
    assert.equal(attr(small, "quality-postfx-suppressed"), "false", `${canvas}: FXAA would replace a bloom that costs about the same`);
    assert.equal(attr(small, "quality-revision"), revision, `${canvas}: no quality revision is spent`);
    assert.deepEqual((await renderOneFrame(small)).post, survival.post, `${canvas}: the frame is unchanged`);
    for (const bloom of [{kind: "bloom", scale: 0.5}, {kind: "bloom", scale: 0.35, mode: "mip"}]) {
      const heavy = await mountScene([bloom, tone], extra);
      await missFrames(heavy, 60);
      assert.equal(attr(heavy, "quality-postfx-suppressed"), "true", `${canvas}: ${JSON.stringify(bloom)} costs more than FXAA`);
      assert.equal(chain(heavy), "toneMapping");
    }
    assert.equal(small.env.consoleLogs.error.length, 0, canvas);
  }
});

// A balanced device keeps haze at every tier, and the scene's haze draws as one
// atmosphere pass that suppression leaves in place. With no tone mapping the
// kept chain is empty but the renderer's chain is not, so a canvas with its own
// MSAA gets an FXAA pass after the haze pass, where an empty chain would draw
// straight to the canvas. Shedding the vignette would then add a dearer pass.
test("suppression counts the haze pass that stays, which gives a canvas with its own MSAA an FXAA pass", async () => {
  const balanced = {capabilityTier: "balanced", msaaSamples: 4};
  const hazy = await mountScene([E("vignette")], balanced, {...PHYSICAL_SKY, haze: {density: 0.002, heightFalloff: 0.04, sunScatter: 0.25}});
  const survival = await renderOneFrame(hazy);
  assert.ok(survival.post.includes("atmosphere") && survival.post.at(-1) === "vignette", `the haze pass runs before the vignette: ${survival.post}`);
  const revision = attr(hazy, "quality-revision");
  await missFrames(hazy, 60);
  assert.equal(attr(hazy, "quality-postfx-suppressed"), "false", "FXAA after the haze pass costs more than the vignette");
  assert.equal(attr(hazy, "quality-revision"), revision);
  assert.deepEqual((await renderOneFrame(hazy)).post, survival.post);
  // Without haze the empty chain draws straight to the MSAA canvas, so shedding the vignette is a pass saved.
  const clear = await mountScene([E("vignette")], balanced, PHYSICAL_SKY);
  await missFrames(clear, 60);
  assert.equal(attr(clear, "quality-postfx-suppressed"), "true");
  assert.deepEqual((await renderOneFrame(clear)).post, []);
  assert.equal(hazy.env.consoleLogs.error.length + clear.env.consoleLogs.error.length, 0);
});
