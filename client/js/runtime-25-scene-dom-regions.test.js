"use strict";
// Custom post-effect DOM-region measurement, uniform patching, and mount
// lifecycle integration.
//
// Split from the former client/js/runtime.test.js DOM-region series. Shared
// fake DOM and source readers live in ./runtime-test-harness.js.

const test = require("node:test");
const assert = require("node:assert/strict");

const {
  bootstrapFeatureScene3DSource,
  bootstrapScene3DMountSourceFile,
  bootstrapScene3DDOMRegionsSourceFile,
  freshFeatureBundleSource,
  FakeElement,
  createContext,
  installManualRAF,
  runScript,
  flushAsyncWork,
} = require("./runtime-test-harness.js");

function createDOMRegionTrackerHarness(options = {}) {
  const mount = new FakeElement("div", null);
  mount.id = "dom-region-scene";
  mount.width = 400;
  mount.height = 200;
  mount.getBoundingClientRect = () => ({ left: 10, top: 20, right: 410, bottom: 220, width: 400, height: 200 });

  const canvasA = new FakeElement("canvas", null);
  canvasA.setAttribute("data-gosx-scene3d-canvas", "true");
  canvasA.width = 400;
  canvasA.height = 200;
  canvasA.getBoundingClientRect = () => ({ left: 10, top: 20, right: 410, bottom: 220, width: 400, height: 200 });
  mount.appendChild(canvasA);

  const targets = [];
  for (let i = 0; i < (options.targetCount || 1); i += 1) {
    const target = new FakeElement("div", null);
    target.setAttribute("class", "glass-card");
    target.width = 100;
    target.height = 50;
    target.getBoundingClientRect = () => ({
      left: 110 + i * 20,
      top: 70 + i * 10,
      right: 210 + i * 20,
      bottom: 120 + i * 10,
      width: 100,
      height: 50,
    });
    target.computedStyle = { borderRadius: "20px", opacity: "1", display: "block", visibility: "visible" };
    targets.push(target);
  }

  const env = createContext({ elements: [mount].concat(targets) });
  const raf = installManualRAF(env.context);
  const patches = [];
  const renders = [];
  env.context.applyScenePostUniformsCommand = function(state, data) {
    patches.push(JSON.parse(JSON.stringify(data)));
    const entries = Array.isArray(data && data.effects) ? data.effects : [];
    for (const entry of entries) {
      const effect = state.postEffects.find((candidate) => candidate.name === entry.name);
      if (effect) {
        effect.uniforms = Object.assign({}, effect.uniforms, entry.uniforms);
        if (Object.prototype.hasOwnProperty.call(entry, "domRegionBounds")) {
          if (entry.domRegionBounds) effect._domRegionBounds = entry.domRegionBounds;
          else delete effect._domRegionBounds;
        }
      }
    }
  };
  runScript(bootstrapScene3DDOMRegionsSourceFile, env.context, "15d-scene-dom-regions.js");
  const defaultPostEffects = [{
      kind: "customPost",
      name: "Glass",
      uniforms: {},
      domRegions: {
        selector: ".glass-card",
        max: options.max,
        uniforms: options.uniforms || { count: "uCount", aspect: "uAspect", rect: "uRegion%dRect", meta: "uRegion%dMeta" },
        bounds: options.bounds,
      },
    }];
  const state = {
    postEffects: options.postEffects === undefined ? defaultPostEffects : options.postEffects,
  };
  let activeCanvas = canvasA;
  const tracker = env.context.__gosx_scene3d_dom_regions.createTracker(
    mount,
    () => activeCanvas,
    state,
    (reason) => renders.push(reason),
  );
  return {
    env,
    mount,
    canvasA,
    get activeCanvas() { return activeCanvas; },
    set activeCanvas(value) { activeCanvas = value; },
    targets,
    raf,
    patches,
    renders,
    state,
    tracker,
  };
}

test("CustomPost DOMRegions packs rect/meta uniforms in post UV space", async () => {
  const harness = createDOMRegionTrackerHarness();
  harness.raf.flush(16);
  await flushAsyncWork();

  assert.equal(harness.patches.length, 1);
  const uniforms = harness.state.postEffects[0].uniforms;
  assert.equal(uniforms.uCount, 1);
  assert.equal(uniforms.uAspect, 2);
  assert.equal(uniforms.uRect, undefined);
  assert.equal(uniforms.uMeta, undefined);
  assert.deepEqual(Array.from(uniforms.uRegion0Rect), [0.375, 0.375, 0.125, 0.125]);
  assert.deepEqual(Array.from(uniforms.uRegion0Meta), [0.1, 1, 0, 0]);
  assert.equal(harness.renders[0], "custom-post-dom-regions");
});

test("CustomPost DOMRegions caps targets and clears stale slots", async () => {
  const harness = createDOMRegionTrackerHarness({ targetCount: 3, max: 2 });
  harness.raf.flush(16);
  await flushAsyncWork();

  let uniforms = harness.state.postEffects[0].uniforms;
  assert.equal(uniforms.uCount, 2);
  assert.notEqual(uniforms.uRegion1Rect[0], 0);

  harness.targets[1].setAttribute("class", "gone");
  harness.targets[2].setAttribute("class", "gone");
  harness.tracker.schedule();
  harness.raf.flush(32);
  await flushAsyncWork();

  uniforms = harness.state.postEffects[0].uniforms;
  assert.equal(uniforms.uCount, 1);
  assert.deepEqual(Array.from(uniforms.uRegion1Rect), [0, 0, 0, 0]);
  assert.deepEqual(Array.from(uniforms.uRegion1Meta), [0, 0, 0, 0]);
});

test("CustomPost DOMRegions normalizes unsafe slot patterns", () => {
  const harness = createDOMRegionTrackerHarness({
    uniforms: { count: "bad count", aspect: "bad/aspect", rect: "bad slot", meta: "slot%d%dMeta" },
  });
  const config = harness.env.context.__gosx_scene3d_dom_regions.config(harness.state.postEffects[0]);
  assert.equal(config.uniforms.count, "regionCount");
  assert.equal(config.uniforms.aspect, "regionAspect");
  assert.equal(config.uniforms.rect, "region%dRect");
  assert.equal(config.uniforms.meta, "region%dMeta");
  harness.tracker.dispose();
});

test("CustomPost DOMRegions computes padded clipped union bounds", async () => {
  const harness = createDOMRegionTrackerHarness({ targetCount: 2, max: 2, bounds: { mode: "union", paddingPx: 20 } });
  harness.raf.flush(16);
  await flushAsyncWork();

  assert.deepEqual(JSON.parse(JSON.stringify(harness.state.postEffects[0]._domRegionBounds)), {
    mode: "union",
    active: true,
    left: 0.2,
    top: 0.15,
    right: 0.6,
    bottom: 0.65,
    paddingPx: 20,
  });
  assert.equal(harness.mount.getAttribute("data-gosx-scene3d-dom-region-bounds"), "1");
  assert.equal(harness.mount.getAttribute("data-gosx-scene3d-dom-region-bounds-area"), "0.2");
});

test("CustomPost DOMRegions emits inactive bounds for hidden targets", async () => {
  const harness = createDOMRegionTrackerHarness({ bounds: { mode: "union", paddingPx: 12 } });
  harness.targets[0].computedStyle.opacity = "0";
  harness.raf.flush(16);
  await flushAsyncWork();

  assert.equal(harness.state.postEffects[0]._domRegionBounds.mode, "union");
  assert.equal(harness.state.postEffects[0]._domRegionBounds.active, false);
  assert.equal(harness.mount.getAttribute("data-gosx-scene3d-dom-region-bounds"), "0");
});

test("CustomPost DOMRegions source includes runtime bounds merge and backend scissors", () => {
  const sceneSource = freshFeatureBundleSource("scene3d");
  const webgpuSource = freshFeatureBundleSource("scene3d-webgpu");
  const webglSource = freshFeatureBundleSource("scene3d-webgl");
  assert.match(sceneSource, /scenePostDOMRegionPixelBounds/);
  assert.match(sceneSource, /domRegionBounds/);
  assert.match(sceneSource, /_domRegionBounds/);
  assert.match(webgpuSource, /setScissorRect/);
  assert.match(webglSource, /\.scissor\(/);
  assert.match(webglSource, /SCISSOR_TEST/);
  assert.match(webgpuSource, /postDOMRegionBoundedSkips/);
  assert.match(webglSource, /postDOMRegionBoundedSkips/);
});

test("CustomPost DOMRegions coalesces unchanged keys and disposes listeners", async () => {
  const harness = createDOMRegionTrackerHarness();
  assert.equal(harness.raf.count(), 1);
  harness.tracker.schedule();
  assert.equal(harness.raf.count(), 1, "duplicate schedule must coalesce into one rAF");

  harness.raf.flush(16);
  await flushAsyncWork();
  assert.equal(harness.patches.length, 1);

  const observer = harness.env.resizeObservers.at(-1);
  const observe = observer.observe.bind(observer);
  const disconnect = observer.disconnect.bind(observer);
  let observeCalls = 0;
  let disconnectCalls = 0;
  observer.observe = (target) => {
    observeCalls += 1;
    observe(target);
  };
  observer.disconnect = () => {
    disconnectCalls += 1;
    disconnect();
  };

  harness.tracker.schedule();
  harness.raf.flush(32);
  await flushAsyncWork();
  assert.equal(harness.patches.length, 1, "unchanged geometry must not patch again");
  assert.equal(observeCalls, 0, "unchanged targets must not be observed again");
  assert.equal(disconnectCalls, 0, "unchanged targets must not disconnect the observer");

  assert.ok((harness.env.windowListeners.get("scroll") || []).length > 0);
  assert.ok(harness.env.resizeObservers.at(-1).targets.size > 0);
  harness.tracker.dispose();
  assert.equal((harness.env.windowListeners.get("scroll") || []).length, 0);
  assert.equal((harness.env.windowListeners.get("resize") || []).length, 0);
  assert.equal(harness.env.resizeObservers.at(-1).targets.size, 0);
  harness.tracker.schedule();
  assert.equal(harness.raf.count(), 0);
});

test("CustomPost DOMRegions stays observer-free until a region config exists", () => {
  const harness = createDOMRegionTrackerHarness({ postEffects: [] });
  assert.equal(harness.raf.count(), 0);
  assert.equal(harness.env.resizeObservers.length, 0);
  assert.equal((harness.env.windowListeners.get("scroll") || []).length, 0);
  assert.equal((harness.env.windowListeners.get("resize") || []).length, 0);
  assert.equal(harness.mount.getAttribute("data-gosx-scene3d-dom-regions"), "0");

  harness.tracker.configure([{
    kind: "customPost",
    name: "Glass",
    domRegions: { selector: ".glass-card" },
  }]);
  assert.equal(harness.raf.count(), 1);
  assert.equal(harness.env.resizeObservers.length, 1);
  assert.equal((harness.env.windowListeners.get("scroll") || []).length, 1);
  assert.equal((harness.env.windowListeners.get("resize") || []).length, 1);

  harness.tracker.configure([]);
  assert.equal(harness.raf.count(), 0);
  assert.equal((harness.env.windowListeners.get("scroll") || []).length, 0);
  assert.equal((harness.env.windowListeners.get("resize") || []).length, 0);
  assert.equal(harness.mount.getAttribute("data-gosx-scene3d-dom-regions"), "0");

  harness.tracker.configure([{
    kind: "customPost",
    name: "Glass",
    domRegions: { selector: ".glass-card" },
  }]);
  assert.equal(harness.raf.count(), 1);
  assert.equal(harness.env.resizeObservers.length, 2);
  harness.tracker.dispose();
  assert.equal(harness.raf.count(), 0);
  assert.equal((harness.env.windowListeners.get("scroll") || []).length, 0);
  assert.equal((harness.env.windowListeners.get("resize") || []).length, 0);
});

test("CustomPost DOMRegions resolves current canvas after replacement", async () => {
  const harness = createDOMRegionTrackerHarness();
  harness.raf.flush(16);
  await flushAsyncWork();
  assert.equal(harness.state.postEffects[0].uniforms.uRegion0Rect[0], 0.375);

  const canvasB = new FakeElement("canvas", null);
  canvasB.setAttribute("data-gosx-scene3d-canvas", "true");
  canvasB.getBoundingClientRect = () => ({ left: 100, top: 50, right: 300, bottom: 150, width: 200, height: 100 });
  harness.mount.removeChild(harness.canvasA);
  harness.mount.appendChild(canvasB);
  harness.activeCanvas = canvasB;
  harness.tracker.schedule();
  harness.raf.flush(32);
  await flushAsyncWork();

  assert.equal(harness.state.postEffects[0].uniforms.uAspect, 2);
  assert.deepEqual(Array.from(harness.state.postEffects[0].uniforms.uRegion0Rect), [0.3, 0.45, 0.25, 0.25]);
});

test("CustomPost DOMRegions tracker is wired into Scene3D mount lifecycle", () => {
  assert.match(bootstrapFeatureScene3DSource, /__gosx_scene3d_dom_regions/);
  assert.match(bootstrapFeatureScene3DSource, /createSceneCustomPostDOMRegionTracker/);
  assert.match(bootstrapScene3DMountSourceFile, /createSceneCustomPostDOMRegionTracker\(ctx\.mount,\s*function\(\) \{ return canvas; \},\s*sceneState,\s*scheduleRender\)/);
  assert.match(
    bootstrapScene3DMountSourceFile,
    /function commitSceneCanvasReplacement\([\s\S]*?canvas = nextCanvas;[\s\S]*?domRegionTracker\.schedule\(\);[\s\S]*?return true;/,
    "renderer canvas replacement must schedule DOM-region remeasurement",
  );
  assert.match(bootstrapScene3DMountSourceFile, /domRegionTracker\.configure\(sceneState\.postEffects\)/);
  assert.match(bootstrapScene3DMountSourceFile, /domRegionTracker\.dispose\(\)/);
});
