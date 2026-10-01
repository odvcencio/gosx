"use strict";
const test = require("node:test");
const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const vm = require("node:vm");
const source = fs.readFileSync(path.join(__dirname, "../runtime/scene3d/mount.ts"), "utf8");
const start = source.indexOf("function sceneAnimationFrameGate(");
const end = source.indexOf("\n}\n", start) + 2;
const gate = vm.runInNewContext(source.slice(start, end) + ";sceneAnimationFrameGate");

for (const hz of [60, 90, 100, 120, 144, 240]) {
  test(`60 FPS cap keeps its rate on a ${hz} Hz display`, () => {
    const interval = 1000 / 60;
    let phase = 1000, count = 0, last = 1000;
    const gaps = [];
    for (let i = 1; i <= hz * 10; i++) {
      const now = 1000 + i * 1000 / hz;
      const result = gate(now, phase, interval, interval);
      phase = result.atMS;
      if (result.shouldRender) { count++; gaps.push(now - last); last = now; }
    }
    assert.equal(count, 600);
    assert.ok(Math.max(...gaps) <= Math.ceil(hz / 60) * 1000 / hz + 0.01);
  });
}

test("late frames discard missed intervals without a burst", () => {
  const resumed = gate(10000, 1000, 1000 / 60, 1000 / 60);
  assert.equal(resumed.atMS, 10000);
  assert.equal(resumed.shouldRender, true);
  assert.equal(gate(10010, resumed.atMS, 1000 / 60, 1000 / 60).shouldRender, false);
});

test("cap changes, clock reversal, and uncapped frames start a new phase", () => {
  for (const args of [[1010, 1000, 100, 20], [900, 1000, 20, 20], [1001, 1000, 0, 0]]) {
    const result = gate(...args);
    assert.equal(result.shouldRender, true);
    assert.equal(result.atMS, args[0]);
  }
});

test("sub-ms timestamp jitter does not halve a 60 Hz rate", () => {
  let phase = 1000, count = 0;
  for (let i = 1; i <= 600; i++) {
    const result = gate(1000 + i * 1000 / 60 + (i % 2 ? -0.2 : 0.2), phase, 1000 / 60, 1000 / 60);
    phase = result.atMS;
    count += Number(result.shouldRender);
  }
  assert.equal(count, 600);
});
