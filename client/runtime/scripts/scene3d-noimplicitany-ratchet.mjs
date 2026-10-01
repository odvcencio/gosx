#!/usr/bin/env node
// scene3d-noimplicitany-ratchet.mjs — ratchet check for Scene3D noImplicitAny.
//
// tsconfig.scene3d.json type-checks every client/runtime/scene3d/*.ts source
// with noImplicitAny off: the legacy ES5-style parameters in that tree carry
// no type annotations, and turning noImplicitAny on today reports thousands
// of pre-existing implicit-any diagnostics (see
// scene3d-noimplicitany-baseline.json). Rewriting all of them is future
// work, not this change.
//
// This script runs tsc against tsconfig.scene3d.ratchet.json (the same
// program, with noImplicitAny turned on), counts diagnostics per file, and
// compares the count to the checked-in baseline. A file's count may only
// stay the same or go down: a real, non-implicit-any-fixing edit must not
// add new implicit-any surface to an already-checked file, and a file
// dropped to zero must be removed from the baseline entirely so it stays
// covered by noImplicitAny going forward (see tsconfig.scene3d.json's
// "files" list; this script does not add files there for you).
//
// Update the baseline only as a DECREASE, by regenerating it after fixing
// real implicit-any diagnostics:
//   node scripts/scene3d-noimplicitany-ratchet.mjs --write-baseline

import { execFileSync } from "node:child_process";
import { readFileSync, writeFileSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

const here = path.dirname(fileURLToPath(import.meta.url));
const runtimeRoot = path.resolve(here, "..");
const tscBin = path.join(runtimeRoot, "node_modules", ".bin", "tsc");
const baselinePath = path.join(runtimeRoot, "scene3d", "noimplicitany-baseline.json");
const writeBaseline = process.argv.includes("--write-baseline");

function runTSC() {
  try {
    execFileSync(tscBin, ["--project", "tsconfig.scene3d.ratchet.json"], {
      cwd: runtimeRoot,
      encoding: "utf8",
      stdio: ["ignore", "pipe", "pipe"],
    });
    return "";
  } catch (err) {
    // tsc exits non-zero when it reports diagnostics; that is the expected,
    // normal path here (the whole point is to count them).
    return (err.stdout || "") + (err.stderr || "");
  }
}

function countPerFile(output) {
  const counts = new Map();
  const linePattern = /^([^\r\n(]+)\(\d+,\d+\): error TS\d+:/gm;
  let match;
  while ((match = linePattern.exec(output)) !== null) {
    const file = match[1].split(path.sep).join("/");
    counts.set(file, (counts.get(file) || 0) + 1);
  }
  return counts;
}

const output = runTSC();
const actual = countPerFile(output);

if (writeBaseline) {
  const sorted = Object.fromEntries(
    [...actual.entries()].sort(([a], [b]) => a.localeCompare(b))
  );
  writeFileSync(baselinePath, JSON.stringify(sorted, null, 2) + "\n");
  console.log(`Wrote ${baselinePath} with ${actual.size} file(s), ${
    [...actual.values()].reduce((a, b) => a + b, 0)
  } total diagnostic(s).`);
  process.exit(0);
}

const baseline = JSON.parse(readFileSync(baselinePath, "utf8"));

let regressed = false;
const baselineFiles = new Set(Object.keys(baseline));
const actualFiles = new Set(actual.keys());

for (const file of [...baselineFiles].sort()) {
  const before = baseline[file];
  const after = actual.get(file) || 0;
  if (after > before) {
    regressed = true;
    console.error(
      `REGRESSION: ${file} went from ${before} to ${after} noImplicitAny diagnostic(s).`
    );
  }
}

for (const file of [...actualFiles].sort()) {
  if (!baselineFiles.has(file)) {
    regressed = true;
    console.error(
      `REGRESSION: ${file} is new to the noImplicitAny ratchet with ${actual.get(file)} diagnostic(s) (not in baseline).`
    );
  }
}

if (regressed) {
  console.error(
    "\nscene3d noImplicitAny ratchet failed. Fix the new implicit-any diagnostics, " +
    "or if this is a genuine improvement to an existing file's count, regenerate " +
    "the baseline with: node scripts/scene3d-noimplicitany-ratchet.mjs --write-baseline"
  );
  process.exit(1);
}

const totalBefore = Object.values(baseline).reduce((a, b) => a + b, 0);
const totalAfter = [...actual.values()].reduce((a, b) => a + b, 0);
console.log(
  `scene3d noImplicitAny ratchet OK: ${totalAfter} diagnostic(s) (baseline: ${totalBefore}).`
);
if (totalAfter < totalBefore) {
  console.log(
    "Counts improved since the baseline was written. Consider tightening it with " +
    "--write-baseline."
  );
}
