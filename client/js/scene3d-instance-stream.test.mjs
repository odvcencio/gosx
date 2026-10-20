// Tests for client/runtime/scene3d/instance-stream.ts, the opt-in binary
// per-frame instance-transform fast path.
//
// The decode half is checked against bytes the REAL Go encoder produced
// (client/js/testdata/instance-stream-go-writer-fixture, backed by
// scene.InstanceStreamFrame.Encode in scene/instance_stream.go) rather than a
// JS-side reimplementation of the wire format, for the same reason
// 19a-scene-ktx2.test.mjs reads real KTX2-Software containers: a round trip
// through one author's own encoder and decoder only proves the pair agrees
// with itself, not that either is right.
//
// The apply half (applyInstanceStreamFrame, published as
// window.__gosx_scene3d_instance_stream_apply) is exercised against a fake
// sceneState/mount pair standing in for mount.ts's closure state, the same
// style client/js/07-declarative-regions.test.mjs uses for regions.ts.

import test from "node:test";
import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import { execFileSync } from "node:child_process";
import { fileURLToPath } from "node:url";

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const repoRoot = path.join(__dirname, "..", "..");
const source = fs.readFileSync(
  path.join(__dirname, "..", "runtime", "scene3d", "instance-stream.ts"),
  "utf8",
);

// loadInstanceStream evaluates instance-stream.ts's IIFE against a fresh fake
// window/document pair and returns the globals it publishes. A fresh
// evaluation per call gives each test its own window, matching one page
// load of the chunk; within one such load, apply's per-batch revision
// counter lives on the sceneState object a caller passes in (one per
// mount), not on this shared window -- see the two-mounts test below.
function loadInstanceStream(documentStub) {
  const window = {};
  const factory = new Function(
    "window",
    "document",
    "TextDecoder",
    source + "\nreturn window;",
  );
  factory(window, documentStub || {}, typeof TextDecoder !== "undefined" ? TextDecoder : undefined);
  return {
    decode: window.__gosx_scene3d_instance_stream_bridge.decode,
    apply: window.__gosx_scene3d_instance_stream_apply,
    dispatchInstanceStream: window.__gosx_scene3d_instance_stream_bridge.dispatchInstanceStream,
  };
}

function goFixtureFrames() {
  const go = process.env.GOSX_GO || "go";
  const stdout = execFileSync(
    go,
    ["run", "./client/js/testdata/instance-stream-go-writer-fixture"],
    { cwd: repoRoot, encoding: "utf8" },
  );
  const entries = JSON.parse(stdout);
  const byName = {};
  for (const entry of entries) {
    byName[entry.name] = new Uint8Array(Buffer.from(entry.bytes, "base64"));
  }
  return byName;
}

function sequentialFloats(n) {
  const out = new Float32Array(n);
  for (let i = 0; i < n; i++) out[i] = i * 0.5;
  return out;
}

test("decodeInstanceStreamFrame reads a real Go-encoded aligned-id transform frame", () => {
  const { decode } = loadInstanceStream();
  const frames = goFixtureFrames();
  const frame = decode(frames["aligned-id-transform"]);
  assert.ok(frame, "expected a decoded frame");
  assert.equal(frame.batchId, "crowd-actors");
  assert.equal(frame.revision, 3);
  assert.equal(frame.kind, 0);
  assert.equal(frame.stride, 16);
  assert.equal(frame.count, 2);
  assert.deepEqual(Array.from(frame.data), Array.from(sequentialFloats(2 * 16)));
});

test("decodeInstanceStreamFrame reads a real Go-encoded non-4-aligned-id frame", () => {
  const { decode } = loadInstanceStream();
  const frames = goFixtureFrames();
  const frame = decode(frames["padded-id-transform-color"]);
  assert.ok(frame, "expected a decoded frame");
  assert.equal(frame.batchId, "ab");
  assert.equal(frame.revision, 9001);
  assert.equal(frame.kind, 1);
  assert.equal(frame.stride, 20);
  assert.equal(frame.count, 1);
  assert.deepEqual(Array.from(frame.data), Array.from(sequentialFloats(20)));
});

test("decodeInstanceStreamFrame reads a real Go-encoded zero-instance frame", () => {
  const { decode } = loadInstanceStream();
  const frames = goFixtureFrames();
  const frame = decode(frames["zero-count-skinned-pose"]);
  assert.ok(frame, "expected a decoded frame");
  assert.equal(frame.batchId, "crowd");
  assert.equal(frame.revision, 1);
  assert.equal(frame.kind, 2);
  assert.equal(frame.count, 0);
  assert.equal(frame.data.length, 0);
});

test("decodeInstanceStreamFrame rejects bytes that are not a recognized frame", () => {
  const { decode } = loadInstanceStream();
  assert.equal(decode(new Uint8Array([1, 2, 3])), null);
  assert.equal(decode(new Uint8Array(30)), null); // right length range, wrong magic
  assert.equal(decode(null), null);
});

function makeSceneState(instancedMeshes) {
  return { instancedMeshes: instancedMeshes || [] };
}

function makeMount() {
  const dispatched = [];
  return {
    dispatchEvent(event) { dispatched.push(event); return true; },
    _dispatched: dispatched,
  };
}

test("applyInstanceStreamFrame writes bytes straight into the mounted batch's transforms and schedules a render", () => {
  const { apply } = loadInstanceStream();
  const frames = goFixtureFrames();
  const entry = { id: "crowd-actors", count: 2 };
  const sceneState = makeSceneState([entry]);
  const mount = makeMount();
  const scheduled = [];
  const scheduleRender = (reason) => scheduled.push(reason);

  const result = apply(sceneState, frames["aligned-id-transform"], scheduleRender, mount);

  assert.deepEqual(result, { applied: true, revision: 3 });
  assert.ok(entry.transforms instanceof Float32Array, "transforms should be a retained Float32Array");
  assert.equal(entry.transforms.length, 32);
  assert.deepEqual(Array.from(entry.transforms), Array.from(sequentialFloats(32)));
  assert.deepEqual(scheduled, ["instance-stream"]);
  assert.equal(mount._dispatched.length, 0, "a successful apply reports no error event");
});

test("applyInstanceStreamFrame reuses the same retained Float32Array across frames of equal count", () => {
  const { apply, decode } = loadInstanceStream();
  const frames = goFixtureFrames();
  const entry = { id: "crowd-actors", count: 2 };
  const sceneState = makeSceneState([entry]);
  const scheduled = [];

  apply(sceneState, frames["aligned-id-transform"], (r) => scheduled.push(r), makeMount());
  const firstBuffer = entry.transforms;

  // A second frame for the same batch, same count, higher revision: the Go
  // fixture only ships one instance of this shape, so build a second frame
  // by hand from the same wire layout (id "crowd-actors" is already
  // 4-byte-aligned, matching the fixture's own choice) with different values
  // and a higher revision, proving the buffer identity survives an update
  // and is not reallocated on every apply (the mechanism that lets the
  // render path skip re-deriving a fresh typed array every frame).
  const header = frames["aligned-id-transform"].slice(0, 24 + 12); // header + "crowd-actors" (already 4-aligned)
  const nextRevision = new Uint8Array(header);
  new DataView(nextRevision.buffer).setUint32(8, 4, true); // revision low 32 bits -> 4
  const payload = new Uint8Array(32 * 4);
  new DataView(payload.buffer).setFloat32(0, 99, true);
  const secondFrame = new Uint8Array(nextRevision.length + payload.length);
  secondFrame.set(nextRevision, 0);
  secondFrame.set(payload, nextRevision.length);
  assert.ok(decode(secondFrame), "hand-built second frame must still decode");

  const result = apply(sceneState, secondFrame, (r) => scheduled.push(r), makeMount());
  assert.equal(result.applied, true);
  assert.equal(entry.transforms, firstBuffer, "buffer identity must be retained across same-count frames");
  assert.equal(entry.transforms[0], 99);
});

test("applyInstanceStreamFrame drops a stale or replayed revision without touching state", () => {
  const { apply } = loadInstanceStream();
  const frames = goFixtureFrames();
  const entry = { id: "crowd-actors", count: 2 };
  const sceneState = makeSceneState([entry]);
  const scheduled = [];
  const scheduleRender = (r) => scheduled.push(r);

  apply(sceneState, frames["aligned-id-transform"], scheduleRender, makeMount());
  const buffer = entry.transforms;
  scheduled.length = 0;

  const replay = apply(sceneState, frames["aligned-id-transform"], scheduleRender, makeMount());
  assert.deepEqual(replay, { applied: false, reason: "stale-revision" });
  assert.equal(entry.transforms, buffer);
  assert.deepEqual(scheduled, []);
});

// Regression test: the per-batch revision counter used to live in a
// module-scope Map, shared by every mount this chunk's one page-level IIFE
// ever sees. A batch id is author-chosen per component instance, not
// page-unique, so two independent Scene3D mounts that both name their
// InstancedMesh batch "crowd-actors" shared one counter: the second mount's
// first frame, at the same revision the first mount had already recorded,
// was wrongly rejected as a stale replay. The counter now lives on each
// call's sceneState, so two mounts sharing a batch id -- and even replaying
// the exact same revision -- apply independently.
test("applyInstanceStreamFrame tracks revisions per mount (sceneState), not globally, for two mounts sharing a batch id", () => {
  const { apply } = loadInstanceStream();
  const frames = goFixtureFrames();

  const entryA = { id: "crowd-actors", count: 2 };
  const sceneStateA = makeSceneState([entryA]);
  const entryB = { id: "crowd-actors", count: 2 };
  const sceneStateB = makeSceneState([entryB]);

  const resultA = apply(sceneStateA, frames["aligned-id-transform"], () => {}, makeMount());
  assert.deepEqual(resultA, { applied: true, revision: 3 });

  // Same batch id, same revision, but a DIFFERENT mount's sceneState: a
  // module-scope revision tracker would see revision 3 already recorded for
  // "crowd-actors" (from mount A above) and reject this as stale. A
  // per-sceneState tracker has never seen "crowd-actors" for mount B, so it
  // must apply.
  const resultB = apply(sceneStateB, frames["aligned-id-transform"], () => {}, makeMount());
  assert.deepEqual(resultB, { applied: true, revision: 3 });
  assert.ok(entryB.transforms instanceof Float32Array);
  assert.deepEqual(Array.from(entryB.transforms), Array.from(sequentialFloats(32)));

  // Mount A's own replay of the same revision is still correctly rejected:
  // per-mount tracking must not turn off the stale-revision guard entirely.
  const replayA = apply(sceneStateA, frames["aligned-id-transform"], () => {}, makeMount());
  assert.deepEqual(replayA, { applied: false, reason: "stale-revision" });
});

test("applyInstanceStreamFrame fails named (not silently) for an unsupported kind", () => {
  const { apply } = loadInstanceStream();
  const frames = goFixtureFrames();
  const sceneState = makeSceneState([{ id: "crowd", count: 0 }]);
  const mount = makeMount();
  const originalError = console.error;
  const errors = [];
  console.error = (...args) => errors.push(args.join(" "));
  try {
    const result = apply(sceneState, frames["zero-count-skinned-pose"], () => {}, mount);
    assert.equal(result.applied, false);
    assert.match(result.reason, /unsupported instance-stream kind 2/);
    assert.equal(mount._dispatched.length, 1);
    assert.equal(mount._dispatched[0].type, "gosx:scene3d:instance-stream-error");
    assert.match(mount._dispatched[0].detail.reason, /unsupported instance-stream kind 2/);
    assert.equal(mount._dispatched[0].detail.batchId, "crowd");
    assert.equal(errors.length, 1);
    assert.match(errors[0], /unsupported instance-stream kind 2/);
  } finally {
    console.error = originalError;
  }
});

test("applyInstanceStreamFrame fails named for an unknown batch id", () => {
  const { apply } = loadInstanceStream();
  const frames = goFixtureFrames();
  const sceneState = makeSceneState([{ id: "some-other-batch", count: 2 }]);
  const mount = makeMount();
  const originalError = console.error;
  console.error = () => {};
  try {
    const result = apply(sceneState, frames["aligned-id-transform"], () => {}, mount);
    assert.equal(result.applied, false);
    assert.match(result.reason, /unknown instance-stream batch id/);
    assert.equal(mount._dispatched[0].detail.batchId, "crowd-actors");
  } finally {
    console.error = originalError;
  }
});

test("applyInstanceStreamFrame fails named for a count mismatch instead of truncating or padding", () => {
  const { apply } = loadInstanceStream();
  const frames = goFixtureFrames();
  const entry = { id: "crowd-actors", count: 3 }; // fixture frame carries 2
  const sceneState = makeSceneState([entry]);
  const mount = makeMount();
  const originalError = console.error;
  console.error = () => {};
  try {
    const result = apply(sceneState, frames["aligned-id-transform"], () => {}, mount);
    assert.equal(result.applied, false);
    assert.match(result.reason, /instance-stream count 2 does not match mounted batch count 3/);
    assert.equal(entry.transforms, undefined, "a rejected frame must not touch the batch");
  } finally {
    console.error = originalError;
  }
});

test("applyInstanceStreamFrame fails named for a malformed frame", () => {
  const { apply } = loadInstanceStream();
  const sceneState = makeSceneState([{ id: "crowd-actors", count: 2 }]);
  const mount = makeMount();
  const originalError = console.error;
  console.error = () => {};
  try {
    const result = apply(sceneState, new Uint8Array([1, 2, 3]), () => {}, mount);
    assert.equal(result.applied, false);
    assert.match(result.reason, /malformed or unrecognized instance-stream frame/);
  } finally {
    console.error = originalError;
  }
});

// --- benchmark: binary apply vs. an equivalent JSON round trip ---
//
// This is not the full applySceneInstancedMeshesCommand pipeline (that pulls
// in the whole scene-core module graph); it isolates the mechanism this fast
// path actually replaces for a per-frame transform-only update: JSON-encode
// then JSON-decode a transforms array of the same shape MountCommandBatch
// would carry, versus decodeInstanceStreamFrame + applyInstanceStreamFrame
// on the equivalent bytes. See scene/instance_stream_bench_test.go for the
// Go encode-side benchmark this apply-side benchmark complements.
function benchJSONRoundTrip(count) {
  const transforms = Array.from(sequentialFloats(count * 16));
  const payload = JSON.stringify({ revision: 1, commands: [{ kind: 8, data: { instancedMeshes: [{ id: "crowd-actors", count, kind: "box", transforms }] } }] });
  const start = process.hrtime.bigint();
  const parsed = JSON.parse(payload);
  const out = parsed.commands[0].data.instancedMeshes[0].transforms.slice();
  const end = process.hrtime.bigint();
  assert.equal(out.length, count * 16);
  return Number(end - start) / 1e6; // ms
}

function benchBinaryApply(apply, count, revision) {
  const entry = { id: "crowd-actors", count };
  const sceneState = makeSceneState([entry]);
  const data = sequentialFloats(count * 16);
  const header = new Uint8Array(24 + 12); // "crowd-actors" is 12 bytes, 4-aligned
  header.set([0x47, 0x53, 0x58, 0x49], 0);
  header[4] = 1;
  header[5] = 0;
  const dv = new DataView(header.buffer);
  // Each call below builds its own fresh sceneState (one simulated mount
  // per call), so the per-sceneState revision tracker never sees a repeat
  // batch id and the revision argument does not strictly need to increase
  // across calls; it still does here, matching what a real per-frame
  // caller sends.
  dv.setUint32(8, revision, true);
  dv.setUint32(16, count, true);
  dv.setUint16(20, 12, true);
  header.set(Buffer.from("crowd-actors", "utf8"), 24);
  const bytes = new Uint8Array(header.length + data.byteLength);
  bytes.set(header, 0);
  bytes.set(new Uint8Array(data.buffer), header.length);

  const start = process.hrtime.bigint();
  const result = apply(sceneState, bytes, () => {}, makeMount());
  const end = process.hrtime.bigint();
  assert.equal(result.applied, true, JSON.stringify(result));
  return Number(end - start) / 1e6; // ms
}

test("apply-side benchmark: binary instance-stream vs. an equivalent JSON round trip", (t) => {
  const { apply } = loadInstanceStream();
  let revision = 0;
  for (const count of [180, 2000]) {
    const jsonMs = benchJSONRoundTrip(count);
    const binaryMs = benchBinaryApply(apply, count, ++revision);
    t.diagnostic(`instances=${count}: JSON round trip=${jsonMs.toFixed(3)}ms, binary apply=${binaryMs.toFixed(3)}ms`);
  }
});
