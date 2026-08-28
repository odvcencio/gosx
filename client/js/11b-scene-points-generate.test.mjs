// Cross-language determinism gate for procedural point clouds.
//
// This is the other half of scene/points_generator_test.go. Both files expand
// the same descriptors and compare against the same fixture
// (scene/testdata/points_generator_golden.json), so a divergence between Go
// and JavaScript fails on whichever side drifted.
//
// The fixture stores SHA-256 digests over the little-endian IEEE-754 bytes of
// the expanded arrays. That makes the assertion sensitive to a single ulp in
// any of the 16200 floats the largest case produces, which is the point: the
// naive fract(sin(...)) hash these replace disagrees between Go and V8 on
// 19.78% of seeds, and nothing coarser than exact bits would catch it.
import test from "node:test";
import assert from "node:assert/strict";
import crypto from "node:crypto";
import fs from "node:fs";
import path from "node:path";
import vm from "node:vm";
import { fileURLToPath } from "node:url";

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const moduleSrc = fs.readFileSync(
  path.join(__dirname, "bootstrap-src", "11b-scene-points-generate.ts"),
  "utf8"
);
const golden = JSON.parse(
  fs.readFileSync(
    path.join(__dirname, "..", "..", "scene", "testdata", "points_generator_golden.json"),
    "utf8"
  )
);

function loadModule() {
  const context = { console };
  vm.createContext(context);
  vm.runInContext(moduleSrc, context);
  return context;
}

// Mirrors scene.float64BitsDigest: SHA-256 over little-endian float64 bytes.
function float64BitsDigest(values) {
  const buf = Buffer.allocUnsafe(values.length * 8);
  for (let i = 0; i < values.length; i++) buf.writeDoubleLE(values[i], i * 8);
  return crypto.createHash("sha256").update(buf).digest("hex");
}

function float64BitsHead(values, n) {
  const out = [];
  for (let i = 0; i < Math.min(n, values.length); i++) {
    const buf = Buffer.allocUnsafe(8);
    buf.writeDoubleLE(values[i], 0);
    out.push(buf.toString("hex"));
  }
  return out;
}

// Translate the Go descriptor (as marshalled into the fixture) into the
// lowered client shape the runtime receives.
function toClientDescriptor(generator) {
  return {
    kind: generator.Kind || "",
    seed: generator.Seed || 0,
    stride: generator.Stride || 0,
    offsetX: generator.OffsetX || 0,
    offsetY: generator.OffsetY || 0,
    offsetZ: generator.OffsetZ || 0,
    offsetSize: generator.OffsetSize || 0,
    centerX: (generator.Center && generator.Center.x) || 0,
    centerY: (generator.Center && generator.Center.y) || 0,
    centerZ: (generator.Center && generator.Center.z) || 0,
    extentX: (generator.Extent && generator.Extent.x) || 0,
    extentY: (generator.Extent && generator.Extent.y) || 0,
    extentZ: (generator.Extent && generator.Extent.z) || 0,
    sizeMin: generator.SizeMin || 0,
    sizeMax: generator.SizeMax || 0,
    sizeExp: generator.SizeExponent || 0,
  };
}

test("client point generation is bit-identical to the Go generator", () => {
  const ctx = loadModule();
  assert.ok(golden.cases.length > 0, "fixture must contain cases");
  for (const testCase of golden.cases) {
    const made = ctx.sceneGeneratePointsArrays(
      toClientDescriptor(testCase.generator),
      testCase.count
    );
    assert.ok(made, `${testCase.name}: generator returned nothing`);
    assert.equal(
      made.positions.length,
      testCase.count * 3,
      `${testCase.name}: position array length`
    );
    assert.equal(made.sizes.length, testCase.count, `${testCase.name}: size array length`);
    assert.equal(
      float64BitsDigest(made.positions),
      testCase.positionsSha256,
      `${testCase.name}: positions diverged from Go\n` +
        `  client head: ${float64BitsHead(made.positions, 9).join(" ")}\n` +
        `  golden head: ${testCase.positionsHead.join(" ")}`
    );
    assert.equal(
      float64BitsDigest(made.sizes),
      testCase.sizesSha256,
      `${testCase.name}: sizes diverged from Go\n` +
        `  client head: ${float64BitsHead(made.sizes, 3).join(" ")}\n` +
        `  golden head: ${testCase.sizesHead.join(" ")}`
    );
  }
});

test("canonical sine avoids the platform Math.sin divergence", () => {
  const ctx = loadModule();
  // Seeds where Go's math.Sin and V8's Math.sin are known to disagree on the
  // hash result. The canonical kernel must not reproduce V8's answer.
  let platformDisagreements = 0;
  for (let seed = 0; seed <= 20000; seed++) {
    const arg = seed * 12.9898 + 78.233;
    const platform = Math.sin(arg) * 43758.5453;
    const canonical = ctx.sceneCanonicalSin(arg) * 43758.5453;
    if (platform - Math.floor(platform) !== canonical - Math.floor(canonical)) {
      platformDisagreements++;
    }
  }
  // If this ever hits zero, V8 changed its sine and the fixture — not this
  // assertion — is the thing that still guarantees correctness.
  assert.ok(
    platformDisagreements > 0,
    "expected the canonical sine to differ from V8's Math.sin somewhere"
  );
});

test("unknown recipes degrade to an empty layer instead of drawing garbage", () => {
  const ctx = loadModule();
  assert.equal(ctx.sceneGeneratePointsArrays({ kind: "spiral-arm" }, 10), null);
  assert.equal(ctx.sceneGeneratePointsArrays(null, 10), null);
  assert.equal(ctx.sceneGeneratePointsArrays({ kind: "box-scatter" }, 0), null);

  const entry = { count: 10, generator: { kind: "spiral-arm" } };
  ctx.sceneGeneratePointsEntry(entry);
  assert.equal(entry.count, 0, "unknown recipe must zero the count");
  assert.equal(entry.generator, undefined, "descriptor must be consumed");
  assert.equal(entry.positions, undefined, "no positions for an unknown recipe");
});

test("explicit arrays win over a descriptor and the entry is left untouched", () => {
  const ctx = loadModule();
  const positions = [1, 2, 3];
  const entry = { count: 1, positions, generator: { kind: "box-scatter" } };
  ctx.sceneGeneratePointsEntry(entry);
  assert.equal(entry.positions, positions, "explicit positions must be preserved");
  assert.deepEqual(entry.generator, { kind: "box-scatter" });
});

test("generated entry gains plain float arrays and drops the descriptor", () => {
  const ctx = loadModule();
  const entry = {
    count: 4,
    generator: {
      kind: "box-scatter", seed: 31, stride: 7,
      offsetX: 0, offsetY: 1, offsetZ: 2, offsetSize: 3,
      extentX: 10, extentY: 10, extentZ: 10,
      sizeMin: 1, sizeMax: 2, sizeExp: 2.4,
    },
  };
  ctx.sceneGeneratePointsEntry(entry);
  assert.equal(entry.generator, undefined);
  assert.equal(entry.positions.length, 12);
  assert.equal(entry.sizes.length, 4);
  for (const value of entry.positions) assert.ok(Number.isFinite(value));
  for (const value of entry.sizes) assert.ok(value >= 1 && value <= 2);
});
