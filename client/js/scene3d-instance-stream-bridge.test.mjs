// Tests for client/runtime/scene3d/instance-stream-bridge.ts, the lazy
// loader mount.ts's handle.applyInstanceStream forwards to.
//
// These tests exercise the loader against a fake window/document (the same
// evaluate-the-IIFE-source style client/js/scene3d-instance-stream.test.mjs
// uses for instance-stream.ts itself), driving the fake <script> element's
// onload/onerror callbacks by hand instead of a real network fetch.

import test from "node:test";
import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const source = fs.readFileSync(
  path.join(__dirname, "..", "runtime", "scene3d", "instance-stream-bridge.ts"),
  "utf8",
);

// makeFakeScriptElement returns a plain object standing in for the
// <script> element document.createElement("script") would return. Property
// assignment (script.src = ..., script.crossOrigin = ...) works the same as
// on a real element; onload/onerror are captured so the test can invoke
// them directly instead of waiting on a real network fetch.
function makeFakeScriptElement() {
  const el = { attrs: {} };
  el.setAttribute = (name, value) => { el.attrs[name] = value; };
  return el;
}

// makeFakeDocument returns a fake document whose createElement("script")
// calls are recorded in `scripts` (in creation order) and whose
// head.appendChild is a no-op recorder — nothing here ever actually
// executes a script; the test drives onload/onerror itself.
function makeFakeDocument(scripts, dataAttrs) {
  return {
    querySelector(_selector) {
      if (!dataAttrs) return null;
      return { dataset: dataAttrs };
    },
    head: {
      appendChild(el) {
        scripts.push(el);
        return el;
      },
    },
  };
}

// loadInstanceStreamBridge evaluates instance-stream-bridge.ts's IIFE
// against a fresh fake window/document pair and returns the window (which
// carries window.__gosx_scene3d_apply_instance_stream_frame once the IIFE
// runs) plus the fake document's createElement so a test can drive the
// script element it captured.
function loadInstanceStreamBridge(dataAttrs) {
  const scripts = [];
  const window = {};
  const document = makeFakeDocument(scripts, dataAttrs);
  document.createElement = (tag) => {
    assert.equal(tag, "script");
    return makeFakeScriptElement();
  };
  const factory = new Function("window", "document", source + "\nreturn window;");
  factory(window, document);
  return { window, scripts };
}

function makeSceneState() {
  return { instancedMeshes: [] };
}

function makeMount() {
  const dispatched = [];
  return {
    dispatchEvent(event) { dispatched.push(event); return true; },
    _dispatched: dispatched,
  };
}

// buildFrameBytes constructs a GSXI-header-shaped frame carrying batchId
// and revision, real enough for instance-stream-bridge.ts's own lightweight
// pendingBatchInfo scan (magic + length-prefixed batch id) to read the
// batch id correctly, and for a test's stub apply() to read the revision
// back out and prove WHICH frame actually applied. It is not a full
// scene.InstanceStreamFrame.Encode-compatible payload (kind/count/payload
// bytes are not meaningful) — see instance-stream.ts's own real decoder,
// exercised against real Go-encoded fixtures in
// scene3d-instance-stream.test.mjs, for that.
function buildFrameBytes(batchId, revision) {
  const idBytes = Buffer.from(batchId, "utf8");
  const idLen = idBytes.length;
  const pad = (4 - (idLen % 4)) % 4;
  const headerBytes = 24;
  const buf = new Uint8Array(headerBytes + idLen + pad);
  const dv = new DataView(buf.buffer);
  buf[0] = 0x47; buf[1] = 0x53; buf[2] = 0x58; buf[3] = 0x49; // "GSXI"
  dv.setUint8(4, 1); // version
  dv.setUint8(5, 0); // kind: transform
  dv.setUint32(8, revision >>> 0, true); // revision low 32 bits
  dv.setUint32(12, 0, true); // revision high 32 bits
  dv.setUint32(16, 0, true); // count
  dv.setUint16(20, idLen, true);
  buf.set(idBytes, headerBytes);
  return buf;
}

function revisionOf(bytes) {
  const dv = new DataView(bytes.buffer, bytes.byteOffset, bytes.byteLength);
  return dv.getUint32(8, true);
}

test("applyInstanceStreamFrame calls the already-loaded apply function directly, with no script fetch", () => {
  const { window, scripts } = loadInstanceStreamBridge();
  let calledWith = null;
  window.__gosx_scene3d_instance_stream_apply = (sceneState, bytes, scheduleRender, mount) => {
    calledWith = { sceneState, bytes, scheduleRender, mount };
    return { applied: true, revision: 1 };
  };

  const sceneState = makeSceneState();
  const mount = makeMount();
  const scheduleRender = () => {};
  const bytes = new Uint8Array([1, 2, 3]);

  const result = window.__gosx_scene3d_apply_instance_stream_frame(sceneState, bytes, scheduleRender, mount);

  assert.deepEqual(result, { applied: true, revision: 1 });
  assert.equal(scripts.length, 0, "the fast (already-loaded) path must not create a script element");
  assert.equal(calledWith.sceneState, sceneState);
  assert.equal(calledWith.bytes, bytes);
  assert.equal(calledWith.mount, mount);
});

test("applyInstanceStreamFrame lazy-loads the chunk on first use, reading the versioned URL from the scene3d script tag", async () => {
  const { window, scripts } = loadInstanceStreamBridge({
    gosxScene3dInstanceStreamUrl: "/gosx/assets/runtime/bootstrap-feature-scene3d-instance-stream.abc123.js",
  });

  const sceneState = makeSceneState();
  const mount = makeMount();
  const bytes = buildFrameBytes("crowd", 1);
  const resultPromise = window.__gosx_scene3d_apply_instance_stream_frame(sceneState, bytes, () => {}, mount);

  assert.equal(scripts.length, 1, "the first call must lazy-load exactly one script");
  assert.equal(scripts[0].src, "/gosx/assets/runtime/bootstrap-feature-scene3d-instance-stream.abc123.js");
  assert.equal(scripts[0].crossOrigin, "anonymous");
  assert.equal(scripts[0].referrerPolicy, "no-referrer");
  assert.equal(typeof resultPromise.then, "function", "a call made before the chunk loads must return a thenable");

  // Simulate the chunk finishing its load: it would have published this
  // global itself (instance-stream.ts's own top-level assignment), which is
  // exactly what the real onload handler reads.
  let appliedWith = null;
  window.__gosx_scene3d_instance_stream_apply = (sceneStateArg, bytesArg, _scheduleRender, mountArg) => {
    appliedWith = { sceneStateArg, bytesArg, mountArg };
    return { applied: true, revision: 7 };
  };
  scripts[0].onload();

  const result = await resultPromise;
  assert.deepEqual(result, { applied: true, revision: 7 });
  assert.equal(appliedWith.sceneStateArg, sceneState);
  assert.deepEqual(Array.from(appliedWith.bytesArg), Array.from(bytes));
  assert.notEqual(appliedWith.bytesArg, bytes, "a frame queued behind the chunk load must be applied from a safe copy, not the caller's original buffer");
  assert.equal(appliedWith.mountArg, mount);
});

test("applyInstanceStreamFrame falls back to the unversioned compat path when no data attribute is present", () => {
  const { scripts } = (() => {
    const { window, scripts } = loadInstanceStreamBridge(); // no dataAttrs -> querySelector returns null
    window.__gosx_scene3d_apply_instance_stream_frame(makeSceneState(), buildFrameBytes("b", 1), () => {}, makeMount());
    return { scripts };
  })();
  assert.equal(scripts.length, 1);
  assert.equal(scripts[0].src, "/gosx/bootstrap-feature-scene3d-instance-stream.js");
});

// Regression test: the pending set behind a stalled chunk load used to grow
// once per frame — copyFrameBytes(bytes) plus a queued closure per call —
// so a page streaming instance transforms every frame (60 fps) during a
// load that stalled for even a second or two queued dozens of full byte
// copies. This proves the pending set instead retains only the NEWEST
// frame per batch: of many frames sent for the SAME batch while the chunk
// is loading, exactly one applies (the last one sent), and every earlier
// one resolves as superseded rather than being queued up and applied late
// with stale data.
test("many frames for one batch during a single load: only the newest frame applies, every earlier one is superseded", async () => {
  const { window, scripts } = loadInstanceStreamBridge();
  const mount = makeMount();
  const sceneState = makeSceneState();

  const frameCount = 50;
  const results = [];
  for (let revision = 1; revision <= frameCount; revision++) {
    results.push(window.__gosx_scene3d_apply_instance_stream_frame(sceneState, buildFrameBytes("crowd-actors", revision), () => {}, mount));
  }

  assert.equal(scripts.length, 1, "every frame for the same batch during the same load must share one script fetch");

  let appliedBytes = null;
  let applyCallCount = 0;
  window.__gosx_scene3d_instance_stream_apply = (_sceneState, bytes) => {
    applyCallCount++;
    appliedBytes = bytes;
    return { applied: true, revision: revisionOf(bytes) };
  };
  scripts[0].onload();

  const settled = await Promise.all(results);
  const applied = settled.filter((r) => r.applied);
  const superseded = settled.filter((r) => !r.applied);

  // Only the newest frame's copy survived to be applied: the real apply
  // function ran exactly once, not once per queued frame, and the pending
  // set held at most one entry for this batch the whole time (see the
  // superseded count below: every OTHER frame was replaced, not retained
  // and applied late).
  assert.equal(applyCallCount, 1, "apply() must run exactly once for many frames of the same batch, not once per frame");
  assert.equal(applied.length, 1);
  assert.equal(superseded.length, frameCount - 1);
  for (const r of superseded) {
    assert.deepEqual(r, { applied: false, reason: "superseded by a newer frame for the same batch" });
  }
  assert.equal(revisionOf(appliedBytes), frameCount, "the applied frame must be the newest one sent, not an earlier queued copy");
});

// Two distinct batches during the same stalled load must not interfere:
// each keeps its own newest-frame slot (keyed by mount + batch id), so
// coalescing one batch's flood of frames never drops or delays the other
// batch's own frame.
test("two distinct batches during a single load: each batch's newest frame applies independently", async () => {
  const { window, scripts } = loadInstanceStreamBridge();
  const mount = makeMount();
  const sceneState = makeSceneState();

  const resultsA = [1, 2, 3].map((revision) =>
    window.__gosx_scene3d_apply_instance_stream_frame(sceneState, buildFrameBytes("batch-a", revision), () => {}, mount));
  const resultsB = [1, 2].map((revision) =>
    window.__gosx_scene3d_apply_instance_stream_frame(sceneState, buildFrameBytes("batch-b", revision), () => {}, mount));

  assert.equal(scripts.length, 1, "frames for different batches during the same load still share one script fetch");

  const appliedRevisions = [];
  window.__gosx_scene3d_instance_stream_apply = (_sceneState, bytes) => {
    appliedRevisions.push(revisionOf(bytes));
    return { applied: true };
  };
  scripts[0].onload();

  const settledA = await Promise.all(resultsA);
  const settledB = await Promise.all(resultsB);

  assert.equal(settledA.filter((r) => r.applied).length, 1, "batch-a must apply exactly once");
  assert.equal(settledB.filter((r) => r.applied).length, 1, "batch-b must apply exactly once");
  assert.deepEqual(appliedRevisions.slice().sort((a, b) => a - b), [2, 3], "the newest revision of each batch (3 for batch-a, 2 for batch-b) must be what applied");
});

test("applyInstanceStreamFrame reports a named error (not a silent drop) when the chunk fails to load", async () => {
  const { window, scripts } = loadInstanceStreamBridge();
  const mount = makeMount();
  const originalError = console.error;
  const errors = [];
  console.error = (...args) => errors.push(args.join(" "));

  let result;
  try {
    const resultPromise = window.__gosx_scene3d_apply_instance_stream_frame(makeSceneState(), buildFrameBytes("crowd", 1), () => {}, mount);
    assert.equal(scripts.length, 1);
    scripts[0].onerror(new Error("network failure"));
    result = await resultPromise;
  } finally {
    console.error = originalError;
  }

  assert.deepEqual(result, { applied: false, reason: "instance-stream chunk failed to load" });
  assert.equal(mount._dispatched.length, 1, "a load failure must dispatch an error event, never fail silently");
  assert.equal(mount._dispatched[0].type, "gosx:scene3d:instance-stream-error");
  assert.equal(mount._dispatched[0].detail.reason, "instance-stream chunk failed to load");
  assert.equal(mount._dispatched[0].detail.batchId, "crowd");
  assert.equal(errors.length, 1);
  assert.match(errors[0], /instance-stream chunk failed to load/);
});

// A load failure with several distinct pending batches must report once
// PER PENDING BATCH, not once per dropped frame: two batches queued behind
// one failed load produce exactly two error events, regardless of how many
// frames each batch sent.
test("a load failure reports once per pending batch, not once per dropped frame", async () => {
  const { window, scripts } = loadInstanceStreamBridge();
  const mount = makeMount();
  const originalError = console.error;
  console.error = () => {};
  try {
    const resultsA = [1, 2, 3].map((revision) =>
      window.__gosx_scene3d_apply_instance_stream_frame(makeSceneState(), buildFrameBytes("batch-a", revision), () => {}, mount));
    const resultB = window.__gosx_scene3d_apply_instance_stream_frame(makeSceneState(), buildFrameBytes("batch-b", 1), () => {}, mount);

    scripts[0].onerror(new Error("network failure"));

    const settledA = await Promise.all(resultsA);
    const settledB = await resultB;

    const failedA = settledA.filter((r) => r.reason === "instance-stream chunk failed to load");
    assert.equal(failedA.length, 1, "only the newest of batch-a's three frames should report the load failure");
    assert.deepEqual(settledB, { applied: false, reason: "instance-stream chunk failed to load" });

    assert.equal(mount._dispatched.length, 2, "exactly one error event per pending batch (batch-a, batch-b), not per frame");
    const reportedBatchIDs = mount._dispatched.map((event) => event.detail.batchId).sort();
    assert.deepEqual(reportedBatchIDs, ["batch-a", "batch-b"]);
  } finally {
    console.error = originalError;
  }
});

test("a load failure lets a later call retry the fetch instead of caching the failure forever", async () => {
  const { window, scripts } = loadInstanceStreamBridge();
  const mount = makeMount();
  const originalError = console.error;
  console.error = () => {};
  try {
    const first = window.__gosx_scene3d_apply_instance_stream_frame(makeSceneState(), buildFrameBytes("b", 1), () => {}, mount);
    scripts[0].onerror(new Error("network failure"));
    await first;

    const second = window.__gosx_scene3d_apply_instance_stream_frame(makeSceneState(), buildFrameBytes("b", 2), () => {}, mount);
    assert.equal(scripts.length, 2, "a retry after a failed load must fetch the script again, not reuse the failed promise");

    window.__gosx_scene3d_instance_stream_apply = () => ({ applied: true, revision: 1 });
    scripts[1].onload();
    const result = await second;
    assert.deepEqual(result, { applied: true, revision: 1 });
  } finally {
    console.error = originalError;
  }
});

// Optional fix: a chunk that executes with no network error but never
// assigns window.__gosx_scene3d_instance_stream_apply (a broken or
// mismatched build) must not poison the loader forever — the next call
// should retry with a fresh fetch instead of forever reusing a resolved
// promise whose apply function is permanently missing.
test("a chunk that loads without publishing its apply function clears the cache so the next call retries", async () => {
  const { window, scripts } = loadInstanceStreamBridge();
  const mount = makeMount();
  const originalError = console.error;
  console.error = () => {};
  try {
    const first = window.__gosx_scene3d_apply_instance_stream_frame(makeSceneState(), buildFrameBytes("b", 1), () => {}, mount);
    // window.__gosx_scene3d_instance_stream_apply is deliberately left
    // undefined here, simulating a chunk that loaded but never published it.
    scripts[0].onload();
    const result = await first;
    assert.deepEqual(result, { applied: false, reason: "instance-stream chunk failed to load" });

    const second = window.__gosx_scene3d_apply_instance_stream_frame(makeSceneState(), buildFrameBytes("b", 2), () => {}, mount);
    assert.equal(scripts.length, 2, "the next call must retry with a fresh fetch instead of reusing the broken load");

    window.__gosx_scene3d_instance_stream_apply = () => ({ applied: true, revision: 2 });
    scripts[1].onload();
    const result2 = await second;
    assert.deepEqual(result2, { applied: true, revision: 2 });
  } finally {
    console.error = originalError;
  }
});
