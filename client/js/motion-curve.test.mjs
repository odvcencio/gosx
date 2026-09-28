import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import test from "node:test";
import vm from "node:vm";
import { fileURLToPath } from "node:url";

const repoRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "../..");
const source = fs.readFileSync(path.join(repoRoot, "client/js/bootstrap-src/06-motion-core.ts"), "utf8");

function createRuntime() {
  let nextFrameID = 0;
  const frames = new Map();
  let clock = 0;
  const body = { nodeType: 1, children: [], querySelectorAll() { return []; }, contains() { return false; } };
  const document = { body, documentElement: body, scrollingElement: body, addEventListener() {}, querySelector() { return null; } };
  const window = {
    __gosx: {}, scrollX: 0, scrollY: 0, innerWidth: 1280, innerHeight: 720, console,
    matchMedia() { return { matches: false, addEventListener() {} }; },
    addEventListener() {},
    requestAnimationFrame(callback) { const id = ++nextFrameID; frames.set(id, callback); return id; },
    cancelAnimationFrame(id) { frames.delete(id); },
  };
  const context = vm.createContext({ window, document, console, performance: { now() { return clock; } }, Date, setTimeout, clearTimeout });
  vm.runInContext(source, context, { filename: "06-motion-core.ts" });
  return {
    motion: window.__gosx.motion,
    advance(timestamp) {
      clock = timestamp;
      const pending = Array.from(frames.values());
      frames.clear();
      for (const callback of pending) callback(timestamp);
    },
  };
}

function programElement(program, selectors = {}) {
  return {
    nodeType: 1,
    children: [],
    hasAttribute(name) { return name === "data-gosx-motion-program"; },
    getAttribute(name) { return name === "data-gosx-motion-program" ? JSON.stringify(program) : null; },
    querySelectorAll(selector) { return selectors[selector] ? [selectors[selector]] : []; },
  };
}

function mountCurve(spec) {
  const runtime = createRuntime();
  const scene = { nodeType: 1, getBoundingClientRect() { return { left: 0, top: 0, width: 400, height: 200 }; } };
  const writes = [];
  runtime.motion.attachScene(scene, {
    write(binding, value) { writes.push({ property: binding.property, value }); },
    pin() { return false; },
    invalidate() {},
    disposeProgram() {},
  });
  const program = programElement({
    version: 1,
    id: "curve",
    signals: [
      { id: "clock", kind: "time" },
      { id: "out", kind: "curve", input: "clock", frames: spec.stops.map(([at, value]) => ({ at, value })), smooth: spec.smooth },
    ],
    bindings: [{ signal: "out", target: "camera", selector: "#scene", property: "position.x" }],
  }, { "#scene": scene });
  runtime.motion.mountPrograms(program);
  return { runtime, writes, program };
}

test("curve signals match every Go CurveValue golden sample", () => {
  const cases = JSON.parse(fs.readFileSync(path.join(repoRoot, "motion/testdata/curve_golden.json"), "utf8"));
  assert.ok(cases.length >= 3);
  for (const fixture of cases) {
    // The time signal starts at zero, so replay the samples at x >= 0. Curves
    // that begin below zero still cover their whole non-negative range.
    const samples = fixture.samples.filter(([x]) => x >= 0);
    assert.ok(samples.length > 5, `${fixture.name}: too few non-negative samples`);
    const { runtime, writes } = mountCurve(fixture);
    runtime.advance(0);
    for (const [x, want] of samples) {
      runtime.advance(x * 1000);
      // A write happens only when the value changes, so the latest write is
      // the value the camera holds at this frame.
      const last = writes.filter((w) => w.property === "position.x").pop();
      assert.ok(last, `${fixture.name} at x=${x}: no camera write`);
      assert.ok(Math.abs(last.value - want) <= 1e-9, `${fixture.name} at x=${x}: got ${last.value}, want ${want}`);
    }
  }
});

test("malformed curve stops leave the signal at zero instead of throwing", () => {
  for (const stops of [[], [[0, 1]], [[1, 1], [1, 2]], [[0, "a"], [1, 2]]]) {
    const { runtime, writes } = mountCurve({ stops, smooth: true });
    runtime.advance(0);
    runtime.advance(500);
    for (const write of writes) assert.equal(write.value, 0, JSON.stringify(stops));
  }
});
