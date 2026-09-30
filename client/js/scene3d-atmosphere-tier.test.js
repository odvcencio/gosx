"use strict";
// The atmosphere quality tier is capped by the device capability tier: the
// adaptive ladder measures GPU time, which cannot see a CPU-bound device.
const test = require("node:test");
const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const vm = require("node:vm");

const source = fs.readFileSync(path.join(__dirname, "bootstrap-src", "16d-scene-atmosphere.ts"), "utf8");
const context = { sceneNumber: (v, d) => (Number.isFinite(Number(v)) ? Number(v) : d), sceneIsPlainObject: (v) => Boolean(v) && typeof v === "object" };
vm.createContext(context);
vm.runInContext(source + "\n;globalThis.__tier = sceneAtmosphereTier;", context);
const tier = context.__tier;
const ladder = (rungIndex) => ({ mode: "ladder", rungIndex, ladder: [0, 1, 2, 3] });

test("a full device follows the adaptive ladder", () => {
  assert.equal(tier(ladder(0), "full"), "full");
  assert.equal(tier(ladder(1), "full"), "balanced");
  assert.equal(tier(ladder(3), "full"), "survival");
  assert.equal(tier(null, "full"), null);
});

test("a balanced device never runs the full atmosphere", () => {
  assert.equal(tier(ladder(0), "balanced"), "balanced");
  assert.equal(tier(ladder(3), "balanced"), "survival");
  assert.equal(tier(null, "balanced"), "balanced");
});

test("a constrained device runs survival", () => {
  assert.equal(tier(ladder(0), "constrained"), "survival");
  assert.equal(tier(null, "constrained"), "survival");
});
