"use strict";

const test = require("node:test");
const assert = require("node:assert/strict");
const { createBoardWebGPUHarness, bundleBoxes, flushAsyncWork, deviceCallSnapshot, bundleMeshScene } = require("./runtime-test-harness.js");

async function setup({ zeroFrameStamps = false, post = false, timing = false, optIn = false, asyncScopes = false, perf = false, mesh = false, postTail = "toneMapping", passStampsMS = null } = {}) {
  let now = 0;
  const resolves = [];
  const writes = [];
  const h = await createBoardWebGPUHarness({ fresh: true, verboseTelemetry: false,
    performanceNow: () => now, fakeDeviceOptions: { timestampQuery: timing, zeroFrameStamps, passStampsMS },
    configureDevice(device, state, env) {
      env.context.__gosx_scene3d_webgpu_pass_timing = optIn;
      if (asyncScopes) device.popErrorScope = () => Promise.resolve(null);
      if (perf) {
        const fs = require("node:fs"), path = require("node:path"), vm = require("node:vm");
        env.context.WebSocket = class WebSocket { send() {} };
        env.context.requestAnimationFrame = () => 0;
        Object.assign(env.context.performance, { mark() {}, measure() {}, clearMarks() {} });
        vm.runInContext(fs.readFileSync(path.join(__dirname, "../../perf/instrument.js"), "utf8"), env.context);
      }
      const create = device.createCommandEncoder;
      device.createCommandEncoder = function(...args) {
        const encoder = create.apply(this, args);
        const resolve = encoder.resolveQuerySet;
        encoder.resolveQuerySet = function(...args) { resolves.push(args[2]); return resolve.apply(this, args); };
        return encoder;
      };
    },
  });
  const api = h.env.context.__gosx_scene3d_api;
  const bundle = mesh ? bundleMeshScene(api, bundleBoxes(3, 1).map(o => ({ ...o, material: "m", wireframe: false }))) : api.createSceneRenderBundle(640, 480, "#000",
    { x: 0, y: 0, z: 6, fov: 60, near: 0.05, far: 128 },
    bundleBoxes(10, 1).map(o => ({ ...o, castShadow: true, receiveShadow: true })),
    [], [], [], [], {}, 0, [], [], [], [], [], 0, false);
  bundle.lights = [{ kind: "directional", directionX: -1, directionY: -2, directionZ: -1, intensity: 1, castShadow: true, color: "#fff" }];
  if (post) bundle.postEffects = [{ kind: "bloom", threshold: 0.8, intensity: 0.5 }, { kind: postTail }];
  const set = h.mount.setAttribute;
  h.mount.setAttribute = function(name, value) { writes.push([name, value]); return set.call(this, name, value); };
  const frame = async time => { now = time; const result = h.renderer.render(bundle, { width: 640, height: 480 }, { nowMS: time, active: true }); await flushAsyncWork(); return result; };
  for (let i = 0; i < 12; i++) await frame(i * 16);
  return { ...h, bundle, frame, resolves, writes, setNow(time) { now = time; } };
}

test("WebGPU bloom and tone mapping present in seven render passes", async () => {
  const h = await setup({ post: true });
  try {
    const before = deviceCallSnapshot(h.fake);
    await h.frame(192);
    assert.equal(deviceCallSnapshot(h.fake).renderPasses - before.renderPasses, 7);
    const final = h.fake.state.renderPasses.at(-1);
    assert.equal(final.descriptor.colorAttachments[0].view.__kind, "canvasTextureView");
    assert.equal(final.draws[0].pipeline.desc.fragment.targets[0].format, h.configureCalls.at(-1).format);
  } finally { h.renderer.dispose(); }
});

for (const optIn of [false, true]) test(`WebGPU per-pass query resolves require opt-in (${optIn})`, async () => {
  const h = await setup({ timing: true, optIn });
  try {
    const before = h.resolves.length;
    await h.frame(192);
    const resolves = h.resolves.slice(before);
    assert.ok(resolves.includes(2), "adaptive quality retains frame timestamps");
    assert.equal(resolves.includes(8), optIn);
  } finally { h.renderer.dispose(); }
});

test("WebGPU steady frames avoid DOM writes and reuse frame stats", async () => {
  const h = await setup();
  try {
    const stats = h.mount.__gosxScene3DWebGPUStats;
    const seq = stats.frameSeq;
    let before = h.writes.length;
    await h.frame(192);
    assert.equal(h.writes.length - before, 0);
    const other = h.mount.__gosxScene3DWebGPUStats;
    assert.notEqual(other, stats);
    assert.equal(other.frameSeq, seq + 1);
    await h.frame(208);
    assert.equal(h.mount.__gosxScene3DWebGPUStats, stats, "two buffers cover all steady frames");
    before = h.writes.length;
    await h.frame(512);
    assert.ok(h.writes.length - before < 12, "4 Hz mirror writes only changed values");
    assert.equal(Number(h.mount.getAttribute("data-gosx-scene3d-webgpu-frame-seq")), h.mount.__gosxScene3DWebGPUStats.frameSeq);
  } finally { h.renderer.dispose(); }
});

test("WebGPU frame stats count the shadow passes' caster draws", async () => {
  const h = await setup({ mesh: true });
  try {
    // Three boxes cast into one directional light's shadow map. The shadow passes are
    // encoded before the frame's stats are reset, so the count must survive the reset.
    for (const object of h.bundle.meshObjects) object.castShadow = true;
    await h.frame(192); await h.frame(208);
    const casters = h.mount.__gosxScene3DWebGPUStats.shadowCasterDraws;
    assert.ok(casters >= 3, `the published frame counts its caster draws, got ${casters}`);
    await h.frame(224);
    assert.equal(h.mount.__gosxScene3DWebGPUStats.shadowCasterDraws, casters, "both stats buffers carry the count");
    await h.frame(512);
    assert.equal(Number(h.mount.getAttribute("data-gosx-scene3d-webgpu-shadow-caster-draws")), casters);
    h.bundle.lights[0].castShadow = false;
    await h.frame(528); await h.frame(544);
    assert.equal(h.mount.__gosxScene3DWebGPUStats.shadowCasterDraws || 0, 0, "a frame without a shadow pass counts none");
  } finally { h.renderer.dispose(); }
});

test("WebGPU frame attributes report the samples and target format the frame drew with", async () => {
  const h = await setup({ post: true });
  try {
    // The renderer-state attributes are written before the first frame chooses its samples and target.
    h.bundle.msaaSamples = 4;
    for (let t = 512; t <= 576; t += 16) await h.frame(t);
    await h.frame(1024);
    assert.equal(h.mount.__gosxScene3DWebGPUStats.sampleCount, 4);
    assert.equal(h.mount.getAttribute("data-gosx-scene3d-webgpu-sample-count"), "4");
    assert.equal(h.mount.getAttribute("data-gosx-scene3d-webgpu-target-format"), "rgba16float", "post processing draws into a float target");
    h.bundle.msaaSamples = 1; h.bundle.postEffects = [];
    for (let t = 1040; t <= 1104; t += 16) await h.frame(t);
    await h.frame(1536);
    assert.equal(h.mount.getAttribute("data-gosx-scene3d-webgpu-sample-count"), "1");
    assert.equal(h.mount.getAttribute("data-gosx-scene3d-webgpu-target-format"), h.configureCalls.at(-1).format, "without post processing the frame draws into the canvas format");
  } finally { h.renderer.dispose(); }
});

test("WebGPU scaled and unknown trailing effects retain the present blit", async () => {
  const h = await setup({ post: true });
  try {
    for (const scale of [0.5, 1, 0.5, 1]) {
      h.bundle.postFXMaxPixels = scale === 1 ? 0 : 640 * 480 / 4;
      const before = h.fake.state.renderPasses.length;
      await h.frame(512);
      const passes = h.fake.state.renderPasses.slice(before);
      assert.equal(passes.length, scale === 1 ? 7 : 8);
      assert.equal(passes.at(-1).draws[0].pipeline.desc.fragment.module.label,
        scale === 1 ? "post-toneMapping" : "post-present");
    }
    h.bundle.postEffects.push({ kind: "unknown" });
    await h.frame(768);
    assert.equal(h.fake.state.renderPasses.at(-1).draws[0].pipeline.desc.fragment.module.label, "post-present");
    h.bundle.postEffects = [];
    await h.frame(1024);
    assert.equal(h.mount.__gosxScene3DWebGPUStats.postChain, undefined, "prior post results must not survive a post toggle");
  } finally { h.renderer.dispose(); }
});

test("mount watchdog reads current JS progress without DOM attribute reads", () => {
  const fs = require("node:fs"), path = require("node:path"), vm = require("node:vm");
  const source = fs.readFileSync(path.join(__dirname, "../runtime/scene3d/mount.ts"), "utf8");
  const start = source.indexOf("    function readSceneWebGPUProgress()");
  const end = source.indexOf("    /* @ts-expect-error", start);
  const api = vm.createContext({ renderer: { getFrameProgress: () => ({ frameSeq: 123, frameAt: 456 }) },
    sceneDebugAttr() { throw new Error("watchdog must not read throttled DOM progress"); } });
  vm.runInContext(source.slice(start, end), api);
  assert.deepEqual(JSON.parse(JSON.stringify(api.readSceneWebGPUProgress())), { seq: 123, at: 456 });
});

test("WebGPU render truth diagnostics skip unchanged attributes", async () => {
  const h = await setup();
  try {
    h.env.context.__gosx_scene3d_render_truth = true;
    await h.frame(512);
    const previous = new Map(h.writes);
    const before = h.writes.length;
    await h.frame(768);
    const writes = h.writes.slice(before).filter(([name]) => name.startsWith("data-gosx-scene3d-render-"));
    assert.equal(writes.filter(([name, value]) => previous.get(name) === value).length, 0);
    assert.equal(writes.length, 1, "only the advancing uniform clock changes");
  } finally { h.renderer.dispose(); }
});

test("WebGPU async diagnostics mirror without another render", async () => {
  const h = await setup({ timing: true });
  try {
    const seq = h.mount.__gosxScene3DWebGPUStats.frameSeq;
    h.setNow(300);
    await new Promise(resolve => setTimeout(resolve, 270));
    assert.equal(h.mount.getAttribute("data-gosx-scene3d-webgpu-gpu-timing"), "measured");
    assert.equal(h.mount.getAttribute("data-gosx-scene3d-webgpu-gpu-ms"), "4.000");
    assert.equal(h.mount.__gosxScene3DWebGPUStats.frameSeq, seq, "mirroring must not invent submitted frames");
  } finally { h.renderer.dispose(); }
});


test("WebGPU pending frames leave published stats intact with async error scopes", async () => {
  const h = await setup({ asyncScopes: true, mesh: true });
  try {
    let rejectScope;
    h.fake.device.popErrorScope = () => new Promise((_resolve, reject) => { rejectScope = reject; });
    assert.equal(await h.frame(192), "submitted");
    h.fake.device.popErrorScope = () => Promise.resolve(null);
    const stats = h.mount.__gosxScene3DWebGPUStats;
    const snapshot = JSON.parse(JSON.stringify(stats));
    const seq = h.renderer.getFrameProgress().frameSeq;
    assert.equal(snapshot.meshDrawCalls, 3);
    h.bundle.postEffects = [{ kind: "bloom" }, { kind: "toneMapping" }];
    assert.equal(await h.frame(512), "pending");
    assert.deepEqual(JSON.parse(JSON.stringify(h.mount.__gosxScene3DWebGPUStats)), snapshot);
    assert.equal(h.renderer.getFrameProgress().frameSeq, seq);
    assert.equal(Number(h.mount.getAttribute("data-gosx-scene3d-webgpu-mesh-draw-calls")), snapshot.meshDrawCalls);
    assert.equal(h.renderer.diagnostics().lastError, "");
    rejectScope(new Error("delayed frame error"));
    await flushAsyncWork();
    assert.equal(h.renderer.getFrameProgress().frameSeq, seq, "async errors cannot publish an aborted frame");
    assert.equal(h.mount.__gosxScene3DWebGPUStats.meshDrawCalls, 3);
    assert.equal(h.renderer.diagnostics().lastError, "delayed frame error");
    h.setNow(800);
    await new Promise(resolve => setTimeout(resolve, 270));
    assert.equal(h.mount.getAttribute("data-gosx-scene3d-webgpu-last-error"), "delayed frame error");
    for (let i = 0; i < 8; i++) await h.frame(816 + i * 16);
    assert.equal(h.renderer.diagnostics().lastError, "");
  } finally { h.renderer.dispose(); }
});

for (const postTail of ["toneMapping", "fxaa", "vignette", "colorGrade"]) test(`WebGPU direct and scaled output switches submit every frame with async pipeline validation (${postTail})`, async () => {
  const h = await setup({ post: true, asyncScopes: true, postTail });
  try {
    for (const [width, height] of [[1920, 1080], [1921, 1080], [640, 480], [64, 64], [63, 47], [64, 64]]) {
      h.canvas.width = width; h.canvas.height = height;
      assert.equal(await h.frame(512), "submitted", `${width}x${height}`);
    }
  } finally { h.renderer.dispose(); }
});

test("gosx perf collects a GPU frame sample for every measured frame and opts into pass samples", async () => {
  const h = await setup({ post: true, timing: true, perf: true });
  try {
    const events = h.env.context.__gosx_perf.sceneTelemetryEvents;
    const before = events.length;
    const resolves = h.resolves.length;
    for (let i = 0; i < 60; i++) assert.equal(await h.frame(512 + i * 16), "submitted");
    const samples = events.slice(before).filter(e => e.detail?.source === "gpu-timestamp");
    const frames = samples.filter(e => e.detail.scope === "frame");
    const passes = samples.filter(e => e.detail.scope === "passes");
    assert.equal(frames.length, 60);
    assert.equal(new Set(frames.map(e => e.detail.frameSeq)).size, 60);
    assert.equal(passes.length, 60, "delayed pass ring still yields one sample per steady frame");
    assert.ok(h.resolves.slice(resolves).includes(8), "eight pass stamps: shadow, main, the frame-end marker and skinning");
    assert.ok(h.writes.filter(([name]) => name.endsWith("webgpu-gpu-ms")).length < 10);
  } finally { h.renderer.dispose(); }
});

test("WebGPU frame timer that writes zero stamps falls back to the render-pass span", async () => {
  const h = await setup({ timing: true, zeroFrameStamps: true });
  try {
    for (let i = 0; i < 6; i++) await h.frame(192 + i * 16);
    const status = h.renderer.getPerformanceTimingStatus();
    assert.equal(status.source, "gpu-pass-timestamp");
    assert.equal(status.available, true);
    let sample = null;
    for (let i = 0; i < 6 && !sample; i++) { await h.frame(300 + i * 16); sample = h.renderer.pollPerformanceSample(); }
    assert.ok(sample, "the pass span arrives as the performance sample");
    assert.equal(sample.source, "gpu-pass-timestamp");
    assert.equal(sample.gpuMS, 4, "shadow begin to main end in the fake ramp");
    assert.equal(h.mount.getAttribute("data-gosx-scene3d-webgpu-gpu-timing"), "pass-span");
    const before = h.fake.state.computePasses.length;
    await h.frame(600);
    const labels = h.fake.state.computePasses.slice(before).map(p => p.descriptor && p.descriptor.label);
    assert.ok(!labels.includes("gosx-frame-timer-start"), "an unusable frame timer stops stamping");
  } finally { h.renderer.dispose(); }
});

test("WebGPU frame timer with real stamps keeps the frame scope", async () => {
  const h = await setup({ timing: true });
  try {
    await h.frame(192);
    assert.equal(h.renderer.getPerformanceTimingStatus().source, "gpu-timestamp");
  } finally { h.renderer.dispose(); }
});

test("WebGPU pass timing ends the frame with a marker that loads the presented image", async () => {
  const h = await setup({ timing: true, zeroFrameStamps: true, post: true });
  try {
    let sample = null;
    for (let i = 0; i < 12 && !(sample && sample.source === "gpu-pass-timestamp"); i++) { await h.frame(300 + i * 16); sample = h.renderer.pollPerformanceSample(); }
    assert.ok(sample, "a pass sample arrives");
    assert.equal(sample.gpuMS, 5, "the governor reads the frame span: shadow begin to the marker's end");
    assert.equal(sample.scope, "frame-end-marker");
    assert.equal(h.mount.getAttribute("data-gosx-scene3d-webgpu-gpu-pass-frame-ms"), "5.000");
    assert.equal(h.mount.getAttribute("data-gosx-scene3d-webgpu-gpu-pass-tail-ms"), "1.000", "main end to marker end");
    const before = h.fake.state.renderPasses.length;
    await h.frame(700);
    const passes = h.fake.state.renderPasses.slice(before);
    const marker = passes.at(-1).descriptor;
    assert.equal(marker.label, "gosx-frame-end-marker", "the marker is the frame's last render pass");
    assert.equal(marker.colorAttachments[0].loadOp, "load", "it keeps the presented image");
    assert.equal(marker.colorAttachments[0].storeOp, "store");
    const post = passes.filter(p => p.descriptor && p.descriptor.label !== "gosx-frame-end-marker").at(-1).descriptor;
    assert.equal(marker.colorAttachments[0].view, post.colorAttachments[0].view, "it loads the view post-processing wrote last");
  } finally { h.renderer.dispose(); }
});

test("On a tile GPU the governor sheds quality for a GPU-bound post chain and keeps it for CPU-bound frames", async () => {
  const { createAdaptiveQualityHarness } = require("./runtime-test-harness.js");
  async function samplesFrom(passStampsMS, frameGapMS) {
    const h = await setup({ timing: true, zeroFrameStamps: true, post: true, passStampsMS });
    try {
      const out = [];
      for (let i = 0; i < 40 && out.length < 6; i++) {
        await h.frame(300 + i * frameGapMS);
        const s = h.renderer.pollPerformanceSample();
        if (s && s.source === "gpu-pass-timestamp") out.push(s);
      }
      return out;
    } finally { h.renderer.dispose(); }
  }
  // GPU-bound in post: shadows and the main pass end at 6 ms (the scene span
  // alone would keep quality), post runs to 30 ms.
  const gpuBound = await samplesFrom([0, 1, 1.5, 6, 2, 30], 16);
  // CPU-bound: the GPU frame takes 5 ms while the page draws every 40 ms.
  const cpuBound = await samplesFrom([0, 1, 1.5, 4, 4.5, 5], 40);
  assert.ok(gpuBound.length > 0 && gpuBound.every(s => s.gpuMS === 30 && s.scope === "frame-end-marker"), "the frame span reaches the governor, though the marker began before the main pass ended");
  assert.ok(cpuBound.length > 0 && cpuBound.every(s => s.gpuMS === 5), "GPU time, not the page's frame interval");
  for (const [samples, sheds, label, cpuMS, gapMS] of [[gpuBound, true, "GPU-bound frames shed quality", 3, 16], [cpuBound, false, "CPU-bound frames keep quality", 30, 34]]) {
    const q = createAdaptiveQualityHarness({ qualityTier: "full", adaptiveBudget: "gpu" });
    const initial = q.state.activeTier;
    for (let i = 0; i < 40; i++) {
      q.clock.now += gapMS;
      const s = samples[i % samples.length];
      q.renderer.sample = { source: s.source, gpuMS: s.gpuMS, atMS: q.clock.now };
      q.api.sceneUpdateAdaptiveQuality(q.state, q.mount, q.sceneState, {}, q.clock.now - cpuMS, q.clock.now, q.renderer);
    }
    assert.equal(q.state.activeTier !== initial, sheds, label);
  }
});
