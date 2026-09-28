import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import test from "node:test";
import vm from "node:vm";
import { fileURLToPath } from "node:url";
import { evaluateTimeline } from "./motion-evaluator-test-helper.mjs";

const repoRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "../..");
const source = fs.readFileSync(path.join(repoRoot, "client/js/bootstrap-src/06-motion-core.ts"), "utf8");

function createRuntime(globals = {}) {
  let nextFrameID = 0;
  const frames = new Map();
  const listeners = new Map();
  let clock = 0;
  const body = {
    nodeType: 1,
    children: [],
    querySelectorAll() { return []; },
    contains() { return false; },
  };
  const document = {
    body,
    documentElement: body,
    scrollingElement: body,
    addEventListener() {},
    querySelector() { return null; },
  };
  const window = {
    __gosx: {},
    scrollX: 0,
    scrollY: 0,
    innerWidth: 1280,
    innerHeight: 720,
    console,
    matchMedia() { return { matches: false, addEventListener() {} }; },
    addEventListener(name, callback) {
      const set = listeners.get(name) || new Set();
      set.add(callback);
      listeners.set(name, set);
    },
    requestAnimationFrame(callback) {
      const id = ++nextFrameID;
      frames.set(id, callback);
      return id;
    },
    cancelAnimationFrame(id) { frames.delete(id); },
  };
  const windowOverrides = globals.window || {};
  Object.assign(window, windowOverrides);
  const documentListeners = new Map();
  document.addEventListener = function(name, callback) {
    const set = documentListeners.get(name) || new Set();
    set.add(callback);
    documentListeners.set(name, set);
  };
  const context = vm.createContext({
    window,
    document,
    console,
    performance: { now() { return clock; } },
    Date,
    setTimeout,
    clearTimeout,
    ...Object.fromEntries(Object.entries(globals).filter(([key]) => key !== "window")),
  });
  vm.runInContext(source, context, { filename: "06-motion-core.ts" });

  return {
    motion: window.__gosx.motion,
    advance(timestamp) {
      clock = timestamp;
      const pending = Array.from(frames.values());
      frames.clear();
      for (const callback of pending) callback(timestamp);
      return pending.length;
    },
    setTime(timestamp) { clock = timestamp; },
    setScroll(x, y) { window.scrollX = x; window.scrollY = y; },
    dispatchDocument(name, event = {}) { for (const callback of documentListeners.get(name) || []) callback(event); },
    document,
    window,
    frameCount() { return frames.size; },
    listeners,
  };
}

test("JavaScript timeline evaluator matches every Go motion golden sample", () => {
  const directory = path.join(repoRoot, "motion/testdata/golden");
  const files = fs.readdirSync(directory).filter((name) => name.endsWith(".json")).sort();
  assert.ok(files.length > 0, "motion golden corpus is empty");
  for (const file of files) {
    const fixture = JSON.parse(fs.readFileSync(path.join(directory, file), "utf8"));
    for (const sample of fixture.samples) {
      const actual = evaluateTimeline(fixture.timeline, sample.t).flat();
      assert.equal(actual.length, sample.writes.length, `${fixture.name} at t=${sample.t}: write count`);
      for (let i = 0; i < actual.length; i++) {
        const tolerance = fixture.tol || 1e-9;
        assert.ok(Math.abs(actual[i] - sample.writes[i]) <= tolerance,
          `${fixture.name} at t=${sample.t}, writes[${i}]: got ${actual[i]}, want ${sample.writes[i]} ± ${tolerance}`);
      }
    }
  }
});

test("the timeline corpus evaluator stays in test code, outside the core bootstrap", () => {
  assert.match(fs.readFileSync(path.join(path.dirname(fileURLToPath(import.meta.url)), "motion-evaluator-test-helper.mjs"), "utf8"), /function evaluateTimeline/);
  assert.doesNotMatch(source, /function\s+evaluateTimeline/);
  assert.doesNotMatch(source, /motion\.evaluateTimeline/);
});

test("the live spring integrator matches every spring_settle Go golden sample", () => {
  const fixture = JSON.parse(fs.readFileSync(path.join(repoRoot, "motion/testdata/golden/spring_settle.json"), "utf8"));
  const track = fixture.timeline.Children[0].Track;
  const base = track.Gen.Base.F;
  const physics = track.Gen.Spring;
  const runtime = createRuntime();
  const value = runtime.motion.spring(base[0], {
    to: base[1],
    mass: physics.Mass,
    stiffness: physics.Stiffness,
    damping: physics.Damping,
    velocity: physics.Velocity,
  });
  const springDeltas = [];
  runtime.motion.scheduler.on("evaluate", (_now, delta) => springDeltas.push(delta));
  runtime.advance(0);
  let previousStep = 0;
  for (const sample of fixture.samples) {
    const targetStep = Math.round(sample.t * 240);
    for (let step = previousStep + 1; step <= targetStep; step++) runtime.advance(step * 1000 / 240);
    previousStep = targetStep;
    assert.ok(Math.abs(value.get() - sample.writes[3]) <= fixture.tol,
      `live spring at t=${sample.t}: got ${value.get()}, want ${sample.writes[3]} ± ${fixture.tol}`);
  }
  value.dispose();
});

test("the live tween easing matches cubic_bezier_ease Go golden samples", () => {
  const fixture = JSON.parse(fs.readFileSync(path.join(repoRoot, "motion/testdata/golden/cubic_bezier_ease.json"), "utf8"));
  const track = fixture.timeline.Children[0].Track;
  const ease = track.Keys[0].Ease;
  const runtime = createRuntime();
  const value = runtime.motion.tween(0, { duration: 1, ease: { kind: ease.Kind, args: ease.Args } });
  value.to(1);
  runtime.advance(0);
  for (const sample of fixture.samples) {
    if (sample.t > 0) runtime.advance(sample.t * 1000);
    assert.ok(Math.abs(value.get() - sample.writes[3]) <= fixture.tol,
      `live cubic-bezier tween at t=${sample.t}: got ${value.get()}, want ${sample.writes[3]} ± ${fixture.tol}`);
  }
  value.dispose();
});

test("the scheduler runs fixed phases, renders registered scenes, and sleeps when idle", () => {
  const runtime = createRuntime();
  const phases = [];
  let sceneFrames = 0;
  const scheduler = runtime.motion.scheduler;
  scheduler.request(() => phases.push("request"));
  scheduler.on("read", () => phases.push("read"));
  scheduler.on("evaluate", () => phases.push("evaluate"));
  scheduler.on("write", () => phases.push("write"));
  scheduler.registerScene(() => { phases.push("render"); sceneFrames++; }).invalidate();

  assert.equal(runtime.frameCount(), 1);
  runtime.advance(16);
  assert.deepEqual(phases, ["request", "read", "evaluate", "write", "render"]);
  assert.equal(sceneFrames, 1);
  assert.equal(runtime.frameCount(), 0, "a dirty one-shot scene should sleep after rendering");
});

test("scheduler.cancel removes callbacks already in the current frame batch", () => {
  const runtime = createRuntime();
  const calls = [];
  let second = 0;
  runtime.motion.scheduler.request(() => {
    calls.push("first");
    runtime.motion.scheduler.cancel(second);
  });
  second = runtime.motion.scheduler.request(() => calls.push("second"));
  runtime.advance(16);
  assert.deepEqual(calls, ["first"]);
});

test("mountPrograms delegates descendant lookup to querySelectorAll", () => {
  const runtime = createRuntime();
  const program = programElement({ version: 1, id: "native-query", signals: [] });
  let queries = 0;
  const root = {
    nodeType: 1,
    hasAttribute() { return false; },
    querySelectorAll(selector) {
      queries++;
      assert.equal(selector, "[data-gosx-motion-program]");
      return [program];
    },
    get children() { throw new Error("motion program lookup must not walk JS DOM children"); },
  };
  runtime.motion.mountPrograms(root);
  assert.equal(queries, 1);
});

test("cached rects use fixed and sticky ancestry without a document mutation observer", () => {
  const resizeObservers = [];
  const mutationObservers = [];
  let computedStyleCalls = 0;
  let rectCalls = 0;
  class ResizeObserver {
    constructor(callback) { this.callback = callback; this.disconnected = false; resizeObservers.push(this); }
    observe(element) { this.element = element; }
    disconnect() { this.disconnected = true; }
  }
  class MutationObserver {
    constructor(callback) { this.callback = callback; mutationObservers.push(this); }
    observe(root, options) { this.root = root; this.options = options; }
    disconnect() {}
  }
  const fixedAncestor = { nodeType: 1, parentElement: null };
  const stickyAncestor = { nodeType: 1, parentElement: null };
  const element = {
    nodeType: 1,
    parentElement: fixedAncestor,
    getBoundingClientRect() { rectCalls++; return { left: 12, top: 24, width: 80, height: 40, right: 92, bottom: 64 }; },
  };
  const stickyElement = {
    nodeType: 1,
    parentElement: stickyAncestor,
    getBoundingClientRect() {
      const top = Math.max(20, 200 - runtime.window.scrollY);
      return { left: 0, top, width: 40, height: 20, right: 40, bottom: top + 20 };
    },
  };
  let runtime;
  runtime = createRuntime({
    ResizeObserver,
    MutationObserver,
    window: { getComputedStyle(node) {
      computedStyleCalls++;
      return { position: node === fixedAncestor ? "fixed" : node === stickyAncestor ? "sticky" : "static" };
    } },
  });
  const stop = runtime.motion.scheduler.observeRect(element);
  const stopSticky = runtime.motion.scheduler.observeRect(stickyElement);
  runtime.advance(16);
  assert.equal(runtime.motion.scheduler.rect(element).top, 24);
  assert.equal(runtime.motion.scheduler.rect(stickyElement).top, 200);
  assert.equal(mutationObservers.length, 0, "rect caching must not watch the whole document");
  assert.equal(computedStyleCalls, 4, "position ancestry is captured once at observation");

  runtime.setScroll(0, 100);
  for (const callback of runtime.listeners.get("scroll") || []) callback({ target: null });
  runtime.advance(32);
  assert.equal(runtime.motion.scheduler.rect(element).top, 24, "fixed descendants stay anchored to the viewport");
  assert.equal(runtime.motion.scheduler.rect(stickyElement).top, 100);
  runtime.setScroll(0, 220);
  for (const callback of runtime.listeners.get("scroll") || []) callback({ target: null });
  runtime.advance(48);
  assert.equal(runtime.motion.scheduler.rect(stickyElement).top, 20, "sticky descendants track their clamped viewport position");
  assert.equal(computedStyleCalls, 4, "scroll refreshes rects without recomputing position ancestry");

  stop();
  stopSticky();
  assert.equal(resizeObservers[0].disconnected, true);

  const idle = createRuntime();
  for (const callback of idle.listeners.get("scroll") || []) callback({ target: null });
  assert.equal(idle.frameCount(), 0, "scroll must not wake the scheduler without observed motion elements");
});

test("an interrupted spring keeps its current velocity", () => {
  const runtime = createRuntime();
  const value = runtime.motion.spring(0, { to: 1, stiffness: 240, damping: 20 });
  runtime.advance(0);
  runtime.advance(50);
  const before = value.velocity();
  assert.ok(before > 0);

  value.setTarget(0.25);
  assert.equal(value.velocity(), before);
  assert.ok(runtime.frameCount() > 0, "retargeted spring should keep the shared scheduler awake");
  value.dispose();
  runtime.advance(67);
  assert.equal(runtime.frameCount(), 0, "disposed spring should release its scheduler work");
});

test("a stable PinTo binding invalidates a scene only when its target position changes", () => {
  const runtime = createRuntime();
  const rect = (left, top, width, height) => ({ left, top, width, height, right: left + width, bottom: top + height });
  const scene = {
    nodeType: 1,
    getBoundingClientRect() { return rect(0, 0, 600, 400); },
    addEventListener() {},
    removeEventListener() {},
  };
  const target = {
    nodeType: 1,
    getBoundingClientRect() { return rect(100, 120, 80, 50); },
  };
  const selectors = { "#scene": scene, "#target": target };
  const program = {
    nodeType: 1,
    children: [],
    hasAttribute(name) { return name === "data-gosx-motion-program"; },
    getAttribute(name) {
      return name === "data-gosx-motion-program" ? JSON.stringify({
        version: 1,
        id: "pin-test",
        signals: [],
        pins: [{ scene: "#scene", node: "badge", element: "#target" }],
      }) : null;
    },
    querySelectorAll(selector) { return selectors[selector] ? [selectors[selector]] : []; },
  };
  let invalidations = 0;
  let pins = 0;
  runtime.motion.attachScene(scene, {
    write() {},
    pin() { pins++; return pins === 1; },
    invalidate() { invalidations++; },
  });
  invalidations = 0;
  runtime.motion.mountPrograms(program);
  runtime.advance(16);
  assert.equal(pins, 1);
  assert.equal(invalidations, 1, "the initial pin should invalidate its scene");
  assert.equal(runtime.frameCount(), 0, "the initial pin should not keep the scheduler awake");

  runtime.motion.scheduler.invalidateRects();
  runtime.motion.scheduler.wake();
  runtime.advance(32);
  assert.equal(pins, 2, "a dirty rect should recompute the pin");
  assert.equal(invalidations, 1, "an unchanged pin should not invalidate its scene again");
  assert.equal(runtime.frameCount(), 0, "a stable pin should let the scheduler sleep");
});


test("delayed and staggered motion holds its first frame after an idle period", () => {
  const runtime = createRuntime();
  runtime.motion.scheduler.request(() => {});
  runtime.advance(100);
  assert.equal(runtime.frameCount(), 0);
  runtime.setTime(5000);
  runtime.motion.scheduler.now = () => 100;
  const first = { style: {} }, second = { style: {} };
  runtime.motion.animate(first, [{ opacity: 0 }, { opacity: 1 }], { duration: 100, delay: 100, easing: "linear" });
  runtime.motion.animate(second, [{ opacity: 0 }, { opacity: 1 }], { duration: 100, delay: 200, easing: "linear" });
  assert.equal(first.style.opacity, "0", "backwards fill applies immediately");
  assert.equal(second.style.opacity, "0");
  runtime.advance(5050);
  assert.equal(first.style.opacity, "0");
  assert.equal(second.style.opacity, "0");
  runtime.advance(5150);
  assert.equal(Number(first.style.opacity), 0.5);
  assert.equal(second.style.opacity, "0", "later stagger still waits");
  runtime.advance(5250);
  assert.equal(first.style.opacity, "1");
  assert.equal(Number(second.style.opacity), 0.5);
});

test("Motion keeps its initial keyframe during delay, samples intermediate keys, and applies easing", () => {
  const runtime = createRuntime();
  const element = { style: {} };
  runtime.motion.animate(element, [
    { opacity: 0.2 },
    { opacity: 0.4 },
    { opacity: 1 },
  ], { duration: 200, delay: 100, easing: "linear" });
  assert.equal(element.style.opacity, "0.2", "backwards fill writes the first keyframe synchronously");
  runtime.advance(50);
  assert.equal(element.style.opacity, "0.2", "the first keyframe remains during delay");
  runtime.advance(150);
  assert.ok(Math.abs(Number(element.style.opacity) - 0.3) < 1e-9, "the first half samples the middle keyframe segment");

  const eased = { style: {} };
  runtime.motion.animate(eased, [{ opacity: 0 }, { opacity: 1 }], { duration: 100, easing: "ease-out" });
  runtime.advance(200);
  assert.ok(Math.abs(Number(eased.style.opacity) - 0.75) < 1e-9, "ease-out changes the midpoint from linear 0.5 to 0.75");
});

test("a spring waking an idle scheduler starts like a fresh scheduler", () => {
  for (const cancelled of [false, true]) {
    const idle = createRuntime();
    if (cancelled) {
      const stop = idle.motion.scheduler.addContinuous(() => true);
      idle.advance(0);
      idle.advance(16);
      stop();
    } else {
      idle.motion.scheduler.request(() => {});
      idle.advance(16);
    }
    assert.equal(idle.frameCount(), 0);
    const fresh = createRuntime();
    idle.setTime(5000);
    fresh.setTime(5000);
    const woke = idle.motion.spring(0, { to: 100 });
    const initial = fresh.motion.spring(0, { to: 100 });
    const deltas = [];
    idle.motion.scheduler.on("evaluate", (_now, delta) => deltas.push(delta));
    idle.advance(5016);
    fresh.advance(5016);
    assert.equal(woke.get(), initial.get(), "idle time must not advance the spring");
    assert.ok(woke.get() < 10, "the first frame must not jump to its target");
    idle.advance(5032);
    assert.ok(deltas[0] <= 1 / 60, "the first frame after idle must not receive a 250 ms delta");
    assert.ok(deltas[1] > 0 && deltas[1] < 0.03, "the next delta should match a normal display frame");
    woke.dispose();
    initial.dispose();
  }
});

test("reduced-motion static resolves signal bindings to their final values and keeps natural opacity", () => {
  const runtime = createRuntime({ window: { matchMedia() { return { matches: true, addEventListener() {} }; } } });
  const spring = runtime.motion.spring(0, { to: 1, reducedMotion: "static" });
  const tween = runtime.motion.tween(0, { to: 1, duration: 1, reducedMotion: "static" });
  tween.to(1);
  const keys = runtime.motion.timeline([{ at: 0, value: 0 }, { at: 1, value: 1 }], { duration: 1, reducedMotion: "static" });
  assert.equal(spring.get(), 1);
  assert.equal(tween.get(), 1);
  assert.equal(keys.get(), 1);
  assert.equal(runtime.frameCount(), 0, "static values do not schedule animation frames");

  const element = { style: { opacity: "0.6" } };
  runtime.motion.animate(element, [{ opacity: 1 }, { opacity: 0.2 }], { duration: 100, reducedMotion: "static" });
  assert.equal(element.style.opacity, "0.6", "static DOM motion cannot make content less visible than its natural opacity");

  const bound = { style: {} };
  const program = programElement({
    version: 1,
    id: "static-program",
    signals: [{ id: "reveal", kind: "tween", from: 0, to: 1, duration: 0.5, reducedMotion: "static" }],
    bindings: [{ signal: "reveal", target: "style", selector: "#target", property: "opacity" }],
  }, { "#target": bound });
  runtime.motion.mountPrograms(program);
  runtime.advance(0);
  assert.equal(bound.style.opacity, "1", "static program signals bind their final value without animation");
});

test("cancel restores original inline styles and the original CSS transition", () => {
  const runtime = createRuntime();
  const element = { style: { opacity: "0.8", transform: "rotate(1deg)", transition: "opacity 200ms" } };
  const animation = runtime.motion.animate(element, [
    { opacity: 0, transform: "translate3d(0px, 10px, 0px)" },
    { opacity: 1, transform: "translate3d(0px, 0px, 0px)" },
  ], { duration: 100, easing: "linear" });
  assert.equal(element.style.opacity, "0");
  assert.equal(element.style.transition, "none");
  runtime.advance(25);
  animation.cancel();
  runtime.advance(32);
  assert.equal(element.style.opacity, "0.8");
  assert.equal(element.style.transform, "rotate(1deg)");
  assert.equal(element.style.transition, "opacity 200ms");
});

test("derived velocity returns zero after its input stops changing", () => {
  const runtime = createRuntime();
  const input = runtime.motion.signal(0);
  const velocity = runtime.motion.velocity(input);
  input.set(10);
  runtime.motion.scheduler.request(() => {});
  runtime.advance(16);
  assert.ok(velocity.get() > 0);
  runtime.motion.scheduler.request(() => {});
  runtime.advance(32);
  assert.equal(velocity.get(), 0);
});

function programElement(program, selectors = {}) {
  return {
    nodeType: 1,
    children: [],
    hasAttribute(name) { return name === "data-gosx-motion-program"; },
    getAttribute(name) { return name === "data-gosx-motion-program" ? JSON.stringify(program) : null; },
    querySelectorAll(selector) { return selectors[selector] ? [selectors[selector]] : []; },
  };
}

test("disposing a time program releases continuous frame work", () => {
  const runtime = createRuntime();
  const element = programElement({ version: 1, id: "clock", signals: [{ id: "time", kind: "time" }] });
  runtime.motion.mountPrograms(element);
  runtime.advance(0);
  runtime.advance(16);
  assert.equal(runtime.frameCount(), 1);
  runtime.motion.disposePrograms(element);
  runtime.advance(32);
  assert.equal(runtime.frameCount(), 0, "removed programs must let the scheduler sleep");
  assert.equal(runtime.advance(48), 0);
});

test("a time program created after idle starts from the current performance clock", () => {
  const runtime = createRuntime();
  runtime.motion.scheduler.request(() => {});
  runtime.advance(100);
  runtime.setTime(5000);
  const element = programElement({ version: 1, id: "fresh-clock", signals: [{ id: "seconds", kind: "time" }] });
  runtime.motion.mountPrograms(element);
  runtime.advance(5016);
  const seconds = runtime.motion.get("fresh-clock.seconds");
  assert.ok(seconds < 0.1, `first time sample jumped to ${seconds} seconds after idle`);
  runtime.motion.disposePrograms(element);
});

test("program disposal releases shared pointer and pin rect observers only after the last user", () => {
  const observers = [];
  class ResizeObserver {
    constructor(callback) { this.callback = callback; this.disconnected = false; observers.push(this); }
    observe(element) { this.element = element; }
    disconnect() { this.disconnected = true; }
  }
  const runtime = createRuntime({ ResizeObserver });
  const element = {
    nodeType: 1,
    addEventListener() {}, removeEventListener() {},
    getBoundingClientRect() { return { left: 0, top: 0, width: 100, height: 100 }; },
  };
  const scene = { ...element };
  const selectors = { "#target": element, "#scene": scene };
  const pointer = programElement({ version: 1, id: "pointer", signals: [{ id: "x", kind: "pointer", source: { kind: "pointer", selector: "#target", axis: "x" } }] }, selectors);
  const pins = programElement({ version: 1, id: "pins", signals: [], pins: [{ scene: "#scene", node: "badge", element: "#target" }] }, selectors);
  runtime.motion.mountPrograms(pointer);
  runtime.motion.mountPrograms(pins);
  runtime.advance(16);
  assert.equal(observers.length, 2, "shared target should have one resize observer");
  runtime.motion.disposePrograms(pointer);
  assert.equal(observers.filter((observer) => observer.disconnected).length, 0, "pin still owns the shared target");
  runtime.motion.disposePrograms(pins);
  assert.equal(observers.filter((observer) => observer.disconnected).length, 2);
  runtime.motion.disposePrograms(pins);
  assert.equal(observers.filter((observer) => observer.disconnected).length, 2, "disposal is idempotent");
  assert.equal(runtime.motion.scheduler.rect(element), null);
  assert.equal(runtime.motion.scheduler.rect(scene), null);
});

test("program disposal releases adapters attached before or after motion programs", () => {
  const runtime = createRuntime();
  const scene = { nodeType: 1, getBoundingClientRect() { return { left: 0, top: 0, width: 400, height: 200 }; } };
  const target = { nodeType: 1, getBoundingClientRect() { return { left: 20, top: 30, width: 40, height: 20 }; } };
  const selectors = { "#scene": scene, "#target": target };
  const makeAdapter = () => {
    const disposed = [];
    return {
      disposed,
      adapter: {
        write() {},
        pin() { return false; },
        invalidate() {},
        disposeProgram(record) { disposed.push(record.id); },
      },
    };
  };

  const before = makeAdapter();
  runtime.motion.attachScene(scene, before.adapter);
  const pinProgram = programElement({
    version: 1,
    id: "pin-attached-first",
    signals: [],
    pins: [{ scene: "#scene", node: "badge", element: "#target" }],
  }, selectors);
  runtime.motion.mountPrograms(pinProgram);
  runtime.advance(16);
  runtime.motion.disposePrograms(pinProgram);
  assert.deepEqual(before.disposed, ["pin-attached-first"]);

  const after = makeAdapter();
  const bindingProgram = programElement({
    version: 1,
    id: "binding-attached-later",
    signals: [{ id: "clock", kind: "time" }],
    bindings: [{ signal: "clock", target: "sceneNode", selector: "#scene", node: "badge", property: "position.x" }],
  }, selectors);
  runtime.motion.mountPrograms(bindingProgram);
  runtime.motion.attachScene(scene, after.adapter);
  runtime.advance(32);
  runtime.motion.disposePrograms(bindingProgram);
  assert.deepEqual(after.disposed, ["binding-attached-later"]);
});

test("soft navigation disposes time and rect work and lets the scheduler sleep", () => {
  const resizeObservers = [];
  const mutationObservers = [];
  class ResizeObserver {
    constructor(callback) { this.callback = callback; this.disconnected = false; resizeObservers.push(this); }
    observe(element) { this.element = element; }
    disconnect() { this.disconnected = true; }
  }
  class MutationObserver {
    constructor(callback) { this.callback = callback; this.disconnected = false; mutationObservers.push(this); }
    observe(root, options) { this.root = root; this.options = options; }
    disconnect() { this.disconnected = true; }
    trigger(records) { this.callback(records, this); }
  }
  const target = {
    nodeType: 1,
    addEventListener() {}, removeEventListener() {},
    getBoundingClientRect() { return { left: 0, top: 0, width: 100, height: 100 }; },
  };
  const scene = { ...target };
  const runtime = createRuntime({ ResizeObserver, MutationObserver });
  let sharedWrites = 0;
  runtime.window.__gosx_runtime_api = { setSharedSignalValue() { sharedWrites++; } };
  const program = programElement({
    version: 1,
    id: "soft-nav",
    signals: [
      { id: "clock", kind: "time" },
      { id: "pointer", kind: "pointer", source: { kind: "pointer", selector: "#target", axis: "x" } },
    ],
    pins: [{ scene: "#scene", node: "badge", element: "#target" }],
  }, { "#target": target, "#scene": scene });
  runtime.motion.mountPrograms(program);
  runtime.advance(0);
  runtime.advance(16);
  assert.equal(mutationObservers.length, 1, "program lifecycle observes removed nodes for disposal");
  assert.equal(resizeObservers.length, 2);
  assert.equal(runtime.frameCount(), 1, "the time signal keeps the loop awake before navigation");

  runtime.dispatchDocument("gosx:navigate");
  mutationObservers[0].trigger([{ type: "childList", removedNodes: [program] }]);
  const writesAtDispose = sharedWrites;
  assert.equal(resizeObservers.filter((observer) => observer.disconnected).length, 2);
  assert.equal(runtime.frameCount(), 0, "removing the old page releases its continuous frame callback");
  assert.equal(runtime.advance(32), 0);
  assert.equal(sharedWrites, writesAtDispose, "disposed time signals stop publishing values");
});

test("program bindings reject unsafe style properties and units in JavaScript", () => {
  const runtime = createRuntime();
  const element = { style: {} };
  const program = programElement({
    version: 1,
    id: "binding-allowlist",
    signals: [{ id: "value", kind: "signal" }],
    bindings: [
      { signal: "value", target: "style", selector: "#target", property: "cssText" },
      { signal: "value", target: "style", selector: "#target", property: " opacity " },
      { signal: "value", target: "cssVar", selector: "#target", property: "--safe", unit: ";display:none" },
      { signal: "value", target: "cssVar", selector: "#target", property: "--offset", unit: " px " },
      { signal: "value", target: "style", selector: "#target", property: "opacity" },
    ],
  }, { "#target": element });
  runtime.motion.mountPrograms(program);
  runtime.advance(16);
  assert.equal(element.style.cssText, undefined);
  assert.equal(element.style["--safe"], undefined);
  assert.equal(element.style.opacity, "0", "valid bindings continue to write");

  const unsafeProperty = { style: {} };
  runtime.motion.bind(runtime.motion.signal(1), unsafeProperty, "cssText");
  runtime.advance(32);
  assert.equal(unsafeProperty.style.cssText, undefined, "direct DOM bindings share the property allow-list");

  const unsafeUnit = { style: {} };
  runtime.motion.bind(runtime.motion.signal(1), unsafeUnit, "--unsafe", ";display:none");
  runtime.advance(48);
  assert.equal(unsafeUnit.style["--unsafe"], undefined, "direct DOM bindings share the unit allow-list");
});
