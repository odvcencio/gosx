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
  return { mount, toggle, raf, env };
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

test("declarative pause state follows reduced-motion preference changes", async () => {
  const { mount, toggle, raf, env } = await mountPausedHarness();

  // A live preference transition while playing must park the loop and then
  // resume without charging the preference gap to the scene clock. The first
  // frame after the transition is the deterministic zero-baseline proof;
  // the following frame proves ordinary progress resumes without a click.
  raf.flush(16);
  await flushAsyncWork();
  const playingClock = mount.getAttribute("data-gosx-scene3d-animation-clock");
  env.matchMedia("(prefers-reduced-motion: reduce)").dispatch(true);
  await flushAsyncWork();
  assert.equal(mount.getAttribute("data-gosx-scene3d-animation-state"), "reduced-motion");
  assert.equal(toggle.getAttribute("disabled"), "disabled");
  raf.flush(48);
  await flushAsyncWork();
  assert.equal(mount.getAttribute("data-gosx-scene3d-animation-clock"), playingClock);
  assert.equal(raf.count(), 0, "reduced motion must park a playing loop");

  env.matchMedia("(prefers-reduced-motion: reduce)").dispatch(false);
  await flushAsyncWork();
  assert.equal(mount.getAttribute("data-gosx-scene3d-animation-state"), "playing");
  assert.equal(toggle.getAttribute("disabled"), null);
  raf.flush(1000);
  await flushAsyncWork();
  assert.equal(mount.getAttribute("data-gosx-scene3d-animation-clock"), playingClock);
  assert.equal(mount.getAttribute("data-gosx-scene3d-render-loop"), "active");
  raf.flush(1032);
  await flushAsyncWork();
  assert.ok(
    parseFloat(mount.getAttribute("data-gosx-scene3d-animation-clock")) > parseFloat(playingClock),
    "the second frame after lifting reduced motion must advance the clock",
  );

  // A user pause must survive a temporary reduced-motion preference, and the
  // control must become usable again without silently resuming the scene.
  // Keep the synthetic timestamps monotonic so this models real RAF time.
  raf.flush(1048);
  await flushAsyncWork();
  toggle.dispatchEvent({ type: "click" });
  raf.flush(1080);
  await flushAsyncWork();
  const frozenClock = mount.getAttribute("data-gosx-scene3d-animation-clock");
  assert.equal(mount.getAttribute("data-gosx-scene3d-animation-state"), "paused");
  assert.equal(toggle.getAttribute("aria-pressed"), "true");

  // A live reduced-motion preference supersedes the user-facing mode and
  // disables the control, but must not erase the user's paused choice.
  env.matchMedia("(prefers-reduced-motion: reduce)").dispatch(true);
  await flushAsyncWork();
  assert.equal(mount.getAttribute("data-gosx-scene3d-animation-state"), "reduced-motion");
  assert.equal(toggle.getAttribute("disabled"), "disabled");
  assert.equal(toggle.getAttribute("aria-pressed"), "true");
  // The observer may request one viewport-settle frame to refresh renderer
  // state; after that frame the reduced-motion loop must be parked.
  raf.flush(1120);
  await flushAsyncWork();
  assert.equal(raf.count(), 0, "reduced motion must leave the loop stopped");
  assert.equal(mount.getAttribute("data-gosx-scene3d-animation-clock"), frozenClock);

  // When the preference is lifted, the mount returns to the preserved paused
  // state and the control becomes usable again.
  env.matchMedia("(prefers-reduced-motion: reduce)").dispatch(false);
  await flushAsyncWork();
  assert.equal(mount.getAttribute("data-gosx-scene3d-animation-state"), "paused");
  assert.equal(toggle.getAttribute("disabled"), null);
  assert.equal(toggle.getAttribute("aria-pressed"), "true");
  raf.flush(1168);
  await flushAsyncWork();
  assert.equal(raf.count(), 0, "lifting reduced motion must not resume a user-paused scene");

  // Resuming after the preference transition still gets a zero-baseline first
  // frame, so the preference gap cannot become a scene-clock jump.
  toggle.dispatchEvent({ type: "click" });
  assert.equal(mount.getAttribute("data-gosx-scene3d-animation-state"), "playing");
  raf.flush(2200);
  await flushAsyncWork();
  assert.equal(mount.getAttribute("data-gosx-scene3d-animation-clock"), frozenClock);
  assert.equal(mount.getAttribute("data-gosx-scene3d-render-loop"), "active");
});
