import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import test from "node:test";
import vm from "node:vm";
import { fileURLToPath } from "node:url";

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
  const context = vm.createContext({
    window,
    document,
    console,
    performance: { now() { return clock; } },
    Date,
    setTimeout,
    clearTimeout,
    ...globals,
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
    frameCount() { return frames.size; },
    listeners,
  };
}

test("JavaScript timeline evaluator matches every Go motion golden sample", () => {
  const directory = path.join(repoRoot, "motion/testdata/golden");
  const files = fs.readdirSync(directory).filter((name) => name.endsWith(".json")).sort();
  assert.ok(files.length > 0, "motion golden corpus is empty");
  const { motion } = createRuntime();

  for (const file of files) {
    const fixture = JSON.parse(fs.readFileSync(path.join(directory, file), "utf8"));
    for (const sample of fixture.samples) {
      const actual = motion.evaluateTimeline(fixture.timeline, sample.t).flat();
      assert.equal(actual.length, sample.writes.length, `${fixture.name} at t=${sample.t}: write count`);
      for (let i = 0; i < actual.length; i++) {
        const tolerance = fixture.tol || 1e-9;
        assert.ok(Math.abs(actual[i] - sample.writes[i]) <= tolerance,
          `${fixture.name} at t=${sample.t}, writes[${i}]: got ${actual[i]}, want ${sample.writes[i]} ± ${tolerance}`);
      }
    }
  }
});

test("the scheduler runs fixed phases, renders registered scenes, and sleeps when idle", () => {
  const runtime = createRuntime();
  const phases = [];
  let sceneFrames = 0;
  const scheduler = runtime.motion.scheduler;
  scheduler.on("read", () => phases.push("read"));
  scheduler.on("evaluate", () => phases.push("evaluate"));
  scheduler.on("write", () => phases.push("write"));
  scheduler.registerScene(() => { phases.push("render"); sceneFrames++; }).invalidate();

  assert.equal(runtime.frameCount(), 1);
  runtime.advance(16);
  assert.deepEqual(phases, ["read", "evaluate", "write", "render"]);
  assert.equal(sceneFrames, 1);
  assert.equal(runtime.frameCount(), 0, "a dirty one-shot scene should sleep after rendering");
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
    idle.advance(5016);
    fresh.advance(5016);
    assert.equal(woke.get(), initial.get(), "idle time must not advance the spring");
    assert.ok(woke.get() < 10, "the first frame must not jump to its target");
    woke.dispose();
    initial.dispose();
  }
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
