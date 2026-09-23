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
  const bytes = new Uint8Array([9, 9, 9]);
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
    window.__gosx_scene3d_apply_instance_stream_frame(makeSceneState(), new Uint8Array([1]), () => {}, makeMount());
    return { scripts };
  })();
  assert.equal(scripts.length, 1);
  assert.equal(scripts[0].src, "/gosx/bootstrap-feature-scene3d-instance-stream.js");
});

test("two frames that arrive while the chunk is loading share one script fetch and both apply, in order, once it loads", async () => {
  const { window, scripts } = loadInstanceStreamBridge();

  const sceneStateA = makeSceneState();
  const sceneStateB = makeSceneState();
  const mount = makeMount();
  const bytesA = new Uint8Array([1]);
  const bytesB = new Uint8Array([2]);

  const resultA = window.__gosx_scene3d_apply_instance_stream_frame(sceneStateA, bytesA, () => {}, mount);
  const resultB = window.__gosx_scene3d_apply_instance_stream_frame(sceneStateB, bytesB, () => {}, mount);

  assert.equal(scripts.length, 1, "a second call while the first load is in flight must not fetch a second script");

  const applyOrder = [];
  window.__gosx_scene3d_instance_stream_apply = (sceneState, bytes) => {
    applyOrder.push(sceneState === sceneStateA ? "A" : "B");
    return { applied: true, revision: applyOrder.length };
  };
  scripts[0].onload();

  const [a, b] = await Promise.all([resultA, resultB]);
  assert.deepEqual(applyOrder, ["A", "B"], "queued frames must apply in arrival order");
  assert.deepEqual(a, { applied: true, revision: 1 });
  assert.deepEqual(b, { applied: true, revision: 2 });
});

test("applyInstanceStreamFrame reports a named error (not a silent drop) when the chunk fails to load", async () => {
  const { window, scripts } = loadInstanceStreamBridge();
  const mount = makeMount();
  const originalError = console.error;
  const errors = [];
  console.error = (...args) => errors.push(args.join(" "));

  let result;
  try {
    const resultPromise = window.__gosx_scene3d_apply_instance_stream_frame(makeSceneState(), new Uint8Array([1]), () => {}, mount);
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
  assert.equal(errors.length, 1);
  assert.match(errors[0], /instance-stream chunk failed to load/);
});

test("a load failure lets a later call retry the fetch instead of caching the failure forever", async () => {
  const { window, scripts } = loadInstanceStreamBridge();
  const mount = makeMount();
  const originalError = console.error;
  console.error = () => {};
  try {
    const first = window.__gosx_scene3d_apply_instance_stream_frame(makeSceneState(), new Uint8Array([1]), () => {}, mount);
    scripts[0].onerror(new Error("network failure"));
    await first;

    const second = window.__gosx_scene3d_apply_instance_stream_frame(makeSceneState(), new Uint8Array([2]), () => {}, mount);
    assert.equal(scripts.length, 2, "a retry after a failed load must fetch the script again, not reuse the failed promise");

    window.__gosx_scene3d_instance_stream_apply = () => ({ applied: true, revision: 1 });
    scripts[1].onload();
    const result = await second;
    assert.deepEqual(result, { applied: true, revision: 1 });
  } finally {
    console.error = originalError;
  }
});
