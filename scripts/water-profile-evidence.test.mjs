import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import test from "node:test";

import {
  isExpectedDevStreamAbort,
  parseArgs,
  summarizeFrameIntervals,
  validateBudget,
  validateEvidence,
  validateMonotonicProfiles,
} from "./water-profile-evidence.mjs";

const repoRoot = path.resolve(import.meta.dirname, "..");
const budget = JSON.parse(fs.readFileSync(
  path.join(repoRoot, "perf/budgets/water-profile-evidence.json"),
  "utf8",
));

test("water evidence CLI parses reproducibility and hardware options", () => {
  const parsed = parseArgs([
    "--url=http://127.0.0.1:3333/demos/water",
    "--samples", "60",
    "--warmup-frames=12",
    "--width", "1280",
    "--height=720",
    "--environment", "hardware",
    "--enforce-hardware",
    "--headed",
  ]);
  assert.equal(parsed.url, "http://127.0.0.1:3333/demos/water");
  assert.equal(parsed.samples, 60);
  assert.equal(parsed.warmupFrames, 12);
  assert.deepEqual([parsed.width, parsed.height], [1280, 720]);
  assert.equal(parsed.environment, "hardware");
  assert.equal(parsed.enforceHardware, true);
  assert.equal(parsed.headed, true);
});

test("water evidence frame summary reports p50 p95 p99 max and FPS", () => {
  const summary = summarizeFrameIntervals([10, 20, 15, 30, 25]);
  assert.deepEqual(summary, {
    samples: 5,
    meanMS: 20,
    p50MS: 20,
    p95MS: 30,
    p99MS: 30,
    maxMS: 30,
    fps: 50,
  });
});

test("checked-in water evidence budget is complete and monotonic", () => {
  assert.deepEqual(validateBudget(budget), []);
  assert.deepEqual(validateMonotonicProfiles(budget.profiles), []);
});

test("water evidence monotonic check rejects a silent lower-profile upgrade", () => {
  const profiles = structuredClone(budget.profiles);
  profiles.battery.surfaceResolution = profiles.hero.surfaceResolution + 1;
  assert.match(validateMonotonicProfiles(profiles).join("\n"), /battery\.surfaceResolution/);
});

test("water evidence validation requires the six profile/workload cells", () => {
  const validation = validateEvidence({ cases: [], environment: { classification: "unspecified" } }, budget);
  assert.equal(validation.passed, false);
  assert.equal(validation.failures.filter((message) => message.startsWith("missing ")).length, 6);
});

test("software raster never applies hardware timing thresholds", () => {
  const validation = validateEvidence(
    { cases: [], environment: { classification: "software-raster" } },
    budget,
    { environment: "software-raster", enforceHardware: true },
  );
  assert.match(validation.warnings.join("\n"), /hardware thresholds skipped/);
});

test("warm reload stream aborts stay visible without becoming false failures", () => {
  assert.equal(isExpectedDevStreamAbort({
    url: "http://127.0.0.1:3000/gosx/dev/events",
    reason: "net::ERR_ABORTED",
  }), true);
  assert.equal(isExpectedDevStreamAbort({
    url: "http://127.0.0.1:3000/_gosx/client-events",
    reason: "net::ERR_ABORTED",
  }), true);
  assert.equal(isExpectedDevStreamAbort({
    url: "http://127.0.0.1:3000/water/models/duck/Duck.gltf",
    reason: "net::ERR_ABORTED",
  }), false);
});
