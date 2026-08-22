"use strict";
// The declarative pause/resume contract, end to end through the generated
// bundle.
//
// Pausing used to keep the requestAnimationFrame chain alive at a frozen
// scene clock: every frame recomputed a delta it refused to apply, so a
// paused scene kept burning frame work forever and the loop attributes
// still claimed an active loop. These tests pin the repaired contract:
//
//   1. A toggle click pauses: animation-state flips to paused, the settle
//      render runs, and then the loop reports stopped with reason "paused"
//      and wants-animation false — with ZERO callbacks left scheduled.
//   2. The frozen clock does not move while paused, even across further
//      flushes (there is nothing left to flush).
//   3. Resume (the same click path Enter/Space produce on a real button)
//      schedules rendering immediately, walks the loop back to active, and
//      continues the clock from its frozen value with a zero first delta —
//      paused wall time is never credited to the scene clock.

const test = require("node:test");
const assert = require("node:assert/strict");

const {
  bootstrapSource,
  FakeElement,
  createContext,
  installManualRAF,
  flushSceneInitialFrameBoundary,
  runScript,
  flushAsyncWork,
} = require("./runtime-test-harness.js");

function clockManifest(points) {
  return {
    engines: [
      {
        id: "gosx-engine-declarative-pause",
        component: "GoSXScene3D",
        kind: "surface",
        mountId: "scene-pause-root",
        jsExport: "GoSXScene3D",
        props: {
          width: 480,
          height: 300,
          autoRotate: false,
          scene: { points },
        },
        capabilities: ["canvas", "animation"],
      },
    ],
  };
}

const CLOCK_POINTS = [{
  id: "paused-starfield",
  count: 3,
  positions: [0, 0, 0, 4, 0, 0, 0, 4, 0],
  sizes: [2, 2, 2],
  color: "#ffffff",
  // The only animation source is the declared time uniform, exactly like
  // the material-clock scenes; the toggle must be able to stop its loop.
  customUniforms: { time: 0 },
}];

async function mountPausedHarness() {
  const mount = new FakeElement("div", null);
  mount.id = "scene-pause-root";
  const toggle = new FakeElement("button", null);
  toggle.setAttribute("data-gosx-scene3d-animation-toggle", "");
  // bindSceneAnimationToggle resolves the control scope through
  // mount.closest; the harness DOM has no ancestors above the mount, so
  // stand in the scope with the same surface the runtime consumes.
  mount.closest = function(selector) {
    if (String(selector).indexOf("data-gosx-scene3d-control-scope") === -1) {
      return null;
    }
    return {
      querySelectorAll: function(sel) {
        return String(sel).indexOf("data-gosx-scene3d-animation-toggle") !== -1
          ? [toggle]
          : [];
      },
    };
  };

  const env = createContext({
    elements: [mount],
    manifest: clockManifest(CLOCK_POINTS),
  });
  const raf = installManualRAF(env.context);
  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();
  await flushSceneInitialFrameBoundary(raf);
  return { mount, toggle, raf };
}

test("declarative pause stops the render loop instead of spinning at a frozen clock", async () => {
  const { mount, toggle, raf } = await mountPausedHarness();

  // Playing: the material-clock source keeps the loop active and frames
  // actually flow through requestAnimationFrame.
  assert.equal(mount.getAttribute("data-gosx-scene3d-animation-state"), "playing");
  assert.equal(mount.getAttribute("data-gosx-scene3d-render-loop"), "active");
  assert.equal(mount.getAttribute("data-gosx-scene3d-render-loop-reason"), "material-clock");
  raf.flush(16);
  await flushAsyncWork();
  assert.ok(raf.count() > 0, "a playing scene keeps a frame scheduled");

  // Pause via the generic click contract (what Enter/Space produce on a
  // real button). State flips at once and the settle render is scheduled.
  toggle.dispatchEvent({ type: "click" });
  assert.equal(mount.getAttribute("data-gosx-scene3d-animation-state"), "paused");
  assert.equal(toggle.getAttribute("aria-pressed"), "true");

  // Settle: the toggle's one-off render runs, its tail sees wants=false,
  // and the loop parks for good.
  raf.flush(48);
  await flushAsyncWork();
  assert.equal(mount.getAttribute("data-gosx-scene3d-render-loop"), "stopped");
  assert.equal(mount.getAttribute("data-gosx-scene3d-render-loop-reason"), "paused");
  assert.equal(mount.getAttribute("data-gosx-scene3d-render-loop-wants-animation"), "false");
  assert.equal(raf.count(), 0, "a paused scene must leave zero requestAnimationFrame callbacks scheduled");

  // The frozen clock holds, and nothing schedules behind the stopped loop.
  const frozenClock = mount.getAttribute("data-gosx-scene3d-animation-clock");
  assert.notEqual(frozenClock, null);
  raf.flush(400);
  await flushAsyncWork();
  assert.equal(raf.count(), 0, "flushing wall time while paused must not resurrect the loop");
  assert.equal(mount.getAttribute("data-gosx-scene3d-animation-clock"), frozenClock);
});

test("declarative resume schedules rendering and continues from the frozen pose", async () => {
  const { mount, toggle, raf } = await mountPausedHarness();

  raf.flush(16);
  await flushAsyncWork();
  toggle.dispatchEvent({ type: "click" });
  raf.flush(48);
  await flushAsyncWork();
  assert.equal(mount.getAttribute("data-gosx-scene3d-render-loop"), "stopped");
  const frozenClock = parseFloat(mount.getAttribute("data-gosx-scene3d-animation-clock"));
  assert.ok(Number.isFinite(frozenClock));

  // Resume through the same click path a keyboard activation produces.
  toggle.dispatchEvent({ type: "click" });
  assert.equal(mount.getAttribute("data-gosx-scene3d-animation-state"), "playing");
  assert.equal(toggle.getAttribute("aria-pressed"), "false");
  assert.ok(raf.count() > 0, "resume must schedule a render immediately");

  // First resumed frame: the toggle nulled the per-frame timestamp, so the
  // settle frame's delta is zero no matter how far its timestamp drifted —
  // the paused gap is NOT credited to the clock.
  raf.flush(1000 * 60);
  await flushAsyncWork();
  const resumedClock = parseFloat(mount.getAttribute("data-gosx-scene3d-animation-clock"));
  assert.equal(resumedClock, frozenClock, "the first resumed frame must not advance the frozen clock");
  assert.equal(mount.getAttribute("data-gosx-scene3d-render-loop"), "active");
  assert.equal(mount.getAttribute("data-gosx-scene3d-render-loop-wants-animation"), "true");

  // From there the clock advances by played deltas only.
  raf.flush(1000 * 60 + 32);
  await flushAsyncWork();
  const laterClock = parseFloat(mount.getAttribute("data-gosx-scene3d-animation-clock"));
  assert.ok(laterClock > resumedClock, "the resumed clock advances again");
  assert.ok(laterClock - resumedClock <= 0.25, "played deltas stay inside the clamp");
});
