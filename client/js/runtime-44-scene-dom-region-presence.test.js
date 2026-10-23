"use strict";
// Presence class for CustomPost DOM regions: the document element carries
// presenceClass while a matched region is visible inside the canvas and the
// effect is composited (WebGPU or WebGL backend, post-FX not dropped,
// deferred, none or demoted).
//
// The harness helper below is copied from runtime-25-scene-dom-regions.test.js
// with one addition: options.presenceClass reaches the effect's domRegions.

const test = require("node:test");
const assert = require("node:assert/strict");

const {
  bootstrapScene3DDOMRegionsSourceFile,
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
        presenceClass: options.presenceClass,
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

function stubClassList(element) {
  const classes = new Set();
  element.classList = { add: (c) => classes.add(c), remove: (c) => classes.delete(c), contains: (c) => classes.has(c), get length() { return classes.size; } };
  return element.classList;
}

// The harness MutationObserver fake does not watch attributes; run the observers after an attribute change.
async function settle(h) {
  for (const observer of h.env.mutationObservers.slice()) { if (observer.targets.size) observer.trigger(); }
  await flushAsyncWork();
}

test("adds the presence class while a matched region is visible and the effect is composited", async () => {
  const h = createDOMRegionTrackerHarness({ targetCount: 1, presenceClass: "glass-live" });
  const root = stubClassList(h.env.context.document.documentElement);
  h.mount.setAttribute("data-gosx-scene3d-backend", "webgl");
  h.raf.flush();
  assert.equal(root.contains("glass-live"), true);
  assert.equal(h.mount.getAttribute("data-gosx-scene3d-dom-region-presence"), "Glass:1");
  h.targets[0].computedStyle.display = "none";
  h.tracker._measureNow();
  assert.equal(root.contains("glass-live"), false, "hidden region removes the class");
  assert.equal(h.mount.getAttribute("data-gosx-scene3d-dom-region-presence"), "Glass:0");
});

test("removes the class when post-FX is demoted, dropped or the backend is unknown, and on dispose", async () => {
  const h = createDOMRegionTrackerHarness({ presenceClass: "glass-live" });
  const root = stubClassList(h.env.context.document.documentElement);
  h.mount.setAttribute("data-gosx-scene3d-backend", "webgpu");
  h.raf.flush();
  assert.equal(root.contains("glass-live"), true);
  h.mount.setAttribute("data-gosx-scene3d-webgpu-postfx-demoted", "true");
  await settle(h);
  assert.equal(root.contains("glass-live"), false);
  h.mount.removeAttribute("data-gosx-scene3d-webgpu-postfx-demoted");
  await settle(h);
  assert.equal(root.contains("glass-live"), true);
  h.mount.setAttribute("data-gosx-scene3d-postfx", "dropped");
  await settle(h);
  assert.equal(root.contains("glass-live"), false);
  h.mount.setAttribute("data-gosx-scene3d-postfx", "enabled");
  await settle(h);
  assert.equal(root.contains("glass-live"), true);
  h.mount.setAttribute("data-gosx-scene3d-backend", "");
  await settle(h);
  assert.equal(root.contains("glass-live"), false, "no WebGPU or WebGL backend means no composited effect");
  h.mount.setAttribute("data-gosx-scene3d-backend", "webgl");
  await settle(h);
  assert.equal(root.contains("glass-live"), true);
  h.tracker.dispose();
  assert.equal(root.contains("glass-live"), false);
});

test("a config without presenceClass never touches the document class list or the presence attribute", async () => {
  const h = createDOMRegionTrackerHarness({});
  const root = stubClassList(h.env.context.document.documentElement);
  h.mount.setAttribute("data-gosx-scene3d-backend", "webgl");
  h.raf.flush();
  assert.equal(root.length, 0);
  assert.equal(h.mount.getAttribute("data-gosx-scene3d-dom-region-presence"), null);
});

test("two scenes sharing one presence class keep it until the last one leaves", async () => {
  const a = createDOMRegionTrackerHarness({ presenceClass: "glass-live" });
  const root = stubClassList(a.env.context.document.documentElement);
  a.mount.setAttribute("data-gosx-scene3d-backend", "webgl");
  a.raf.flush();
  // A second tracker in the same document: same context, its own mount.
  const mountB = new FakeElement("div", null);
  mountB.id = "second-scene";
  mountB.getBoundingClientRect = a.mount.getBoundingClientRect;
  mountB.setAttribute("data-gosx-scene3d-backend", "webgl");
  const stateB = { postEffects: JSON.parse(JSON.stringify(a.state.postEffects)) };
  const trackerB = a.env.context.__gosx_scene3d_dom_regions.createTracker(mountB, () => a.canvasA, stateB, () => {});
  a.raf.flush();
  assert.equal(root.contains("glass-live"), true);
  a.tracker.dispose();
  assert.equal(root.contains("glass-live"), true, "the second scene still needs the class");
  trackerB.dispose();
  assert.equal(root.contains("glass-live"), false);
});
