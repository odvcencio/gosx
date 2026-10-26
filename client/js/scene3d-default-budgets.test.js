"use strict";
const test = require("node:test");
const assert = require("node:assert/strict");
const H = require("./runtime-test-harness.js");

async function mountScene(extra = {}, environment = {}) {
  let now = 1000;
  const mount = new H.FakeElement("div", null);
  mount.id = "default-budgets";
  const env = H.createContext({ elements: [mount], enableWebGL2: true,
    disableCanvas2D: true, devicePixelRatio: 2, performanceNow: () => now, ...environment,
    manifest: { engines: [{ id: "budget-engine", component: "GoSXScene3D", kind: "surface", mountId: mount.id,
      props: { width: 1440, height: 900, adaptiveQuality: false,
        scene: { objects: [{ kind: "box", spinY: 0.2 }] }, ...extra } }] } });
  const raf = H.installManualRAF(env.context);
  H.runScript(H.bootstrapSource, env.context, "bootstrap.js");
  for (let i = 0; i < 12; i++) await H.flushAsyncWork();
  raf.flush(now);
  await H.flushAsyncWork();
  const canvas = mount.children[0];
  const renders = () => canvas._webglContext.ops.filter(op => op[0] === "clear").length;
  const tick = time => { now = time; raf.flush(now); };
  return { mount, canvas, tick, renders, env };
}

test("main canvas defaults to at most 2073600 backing pixels at DPR 2", async () => {
  const { canvas } = await mountScene();
  console.log(`default backing pixels: ${canvas.width * canvas.height}`);
  assert.ok(canvas.width * canvas.height <= 2073600);
});

test("pixel budget permits explicit caps and the unbounded sentinel", async () => {
  const { canvas } = await mountScene({ maxPixels: -1 });
  assert.equal(canvas.width * canvas.height, 5184000);
  const capped = await mountScene({ maxPixels: 720000 });
  assert.ok(capped.canvas.width * capped.canvas.height <= 720000);
  const large = await mountScene({ width: 3840, height: 2160 });
  assert.ok(large.canvas.width * large.canvas.height <= 2073600);
});

for (const [name, extra, expected, input] of [
  ["ambient spin", {}, 60],
  ["explicit 120 FPS", { maxFrameRate: 120 }, 120],
  ["active input with ambient spin", { controls: "orbit" }, 120, true],
]) {
  test(`${name} render cadence on 120 Hz rAF`, async () => {
    const s = await mountScene(extra);
    // Settle startup, then count a full second of manual 8.33 ms ticks.
    for (let i = 1; i <= 120; i++) s.tick(1000 + i * 1000 / 120);
    if (input) s.canvas.dispatchEvent({ type: "wheel", deltaY: 10, preventDefault() {} });
    const before = s.renders();
    for (let i = 1; i <= 120; i++) {
      if (input && i % 12 === 0) s.canvas.dispatchEvent({ type: "wheel", deltaY: 10, preventDefault() {} });
      s.tick(2000 + i * 1000 / 120);
    }
    const count = s.renders() - before;
    console.log(`${name} renders/s: ${count}`);
    assert.equal(count, expected);
    if (input) {
      for (let i = 1; i <= 120; i++) s.tick(3000 + i * 1000 / 120);
      const settled = s.renders();
      for (let i = 1; i <= 120; i++) s.tick(4000 + i * 1000 / 120);
      assert.equal(s.renders() - settled, 60, "input window returns to ambient cap");
    }
  });
}


test("transitions retain display cadence alongside ambient spin", async () => {
  const s = await mountScene({ autoRotate: true });
  s.mount.__gosxScene3DState._transitions.push({ startTime: 1000, duration: 10000,
    entry: {}, delta: {}, target: {} });
  for (let i = 1; i <= 120; i++) s.tick(1000 + i * 1000 / 120);
  const before = s.renders();
  for (let i = 1; i <= 120; i++) s.tick(2000 + i * 1000 / 120);
  assert.equal(s.renders() - before, 120);
});

test("Canvas2D draws CSS coordinates at the capped backing scale below DPR 1", async () => {
  const s = await mountScene({ width: 2560, height: 1440, responsive: false },
    { enableWebGL2: false, disableCanvas2D: false, devicePixelRatio: 1 });
  for (let i = 1; i <= 8; i++) {s.tick(1000 + i * 17); await H.flushAsyncWork();}
  const { canvas } = s;
  assert.equal(s.mount.getAttribute("data-gosx-scene3d-css-width"), "2560");
  assert.equal(canvas.width, 1920);
  assert.equal(canvas.height, 1080);
  const scales = canvas._canvasContext.ops.filter(op => op[0] === "scale");
  assert.ok(scales.length > 0, "capped Canvas2D frames must scale their CSS coordinates");
  assert.deepEqual(scales.at(-1), ["scale", 0.75, 0.75]);
});

for (const hz of [60.02, 75, 120, 144]) {
  test(`default ambient cap does not force a vsync divisor at ${hz} Hz`, async () => {
    const s = await mountScene({ framePacing: "vsync-divisor" });
    for (let i = 1; i <= 300; i++) s.tick(1000 + i * 1000 / hz);
    const before = s.renders();
    for (let i = 301; i <= 600; i++) s.tick(1000 + i * 1000 / hz);
    assert.equal(s.mount.getAttribute("data-gosx-scene3d-frame-pacing-k"), "1");
    assert.equal(s.renders() - before, 300);
  });
}

test("an authored minimum DPR overrides only the default pixel budget", async () => {
  const minimum = await mountScene({ minDevicePixelRatio: 2 });
  assert.equal(minimum.canvas.width * minimum.canvas.height, 5184000);
  const modest = await mountScene({ minDevicePixelRatio: 1 });
  assert.equal(modest.canvas.width * modest.canvas.height, 2072298);
  const capped = await mountScene({ minDevicePixelRatio: 2, maxPixels: 2073600 });
  assert.ok(capped.canvas.width * capped.canvas.height <= 2073600);
});

for (const reason of ["motion-frame", "motion-binding", "motion-dispose"]) {
  test(`${reason} does not extend the user-input window`, async () => {
    const s = await mountScene();
    for (let i = 1; i <= 120; i++) s.tick(1000 + i * 1000 / 120);
    s.env.context.__gosx_scene3d_command_bridge = {applyMountedMotionFrame: (_state, _batches, _update, schedule) => schedule(reason)};
    s.mount.__gosxScene3DHandle.applyMotionFrame([]);
    s.tick(2000 + 1000 / 120);
    const before = s.renders();
    for (let i = 2; i <= 13; i++) s.tick(2000 + i * 1000 / 120);
    assert.equal(s.renders() - before, 6);
  });
}
