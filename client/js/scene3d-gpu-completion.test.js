"use strict";
const test = require("node:test");
const assert = require("node:assert/strict");
const vm = require("node:vm");
const { readSceneRendererBackendSrc } = require("./scene3d-renderer-source-set.js");
const source = readSceneRendererBackendSrc("webgl").split("  // @ts-check")[0];

function harness(options = {}) {
  let now = 10, current = null, disjoint = false, lost = false;
  const queries = [];
  const gl = {
    QUERY_RESULT_AVAILABLE: 1, QUERY_RESULT: 2, CURRENT_QUERY: 3,
    getExtension: () => options.unavailable ? null : { TIME_ELAPSED_EXT: 4, GPU_DISJOINT_EXT: 5 },
    createQuery() {
      if (queries.length === options.failAt) throw new Error("allocation");
      const q = { ready: false, ns: 2_500_000, deleted: false }; queries.push(q); return q;
    },
    deleteQuery(q) { q.deleted = true; },
    getParameter: () => disjoint,
    getQuery: () => current,
    isContextLost: () => lost,
    getQueryParameter(q, field) {
      if (field === gl.QUERY_RESULT_AVAILABLE) return q.ready;
      assert.equal(q.ready, true, "never read a pending query"); return q.ns;
    },
    beginQuery(_target, q) { assert.equal(current, null, "elapsed queries cannot nest"); current = q; },
    endQuery() { current = null; },
  };
  const ctx = { performance: { now: () => now } }; vm.createContext(ctx); vm.runInContext(source, ctx);
  return { timer: ctx.createSceneWebGLFrameTimer(gl), gl, queries,
    time: v => now = v, disjoint: v => disjoint = v, lost: () => lost = true };
}

test("WebGL frame timer is bounded, nonblocking, and does not consume public snapshots", () => {
  const h = harness();
  for (let i = 0; i < 3; i++) h.timer.end(h.timer.begin());
  assert.equal(h.timer.begin(), null);
  assert.equal(h.queries.length, 3);
  assert.equal(h.timer.snapshot().status, "pending");
  h.queries[2].ready = true;
  assert.equal(h.timer.sample().gpuMS, 2.5);
  assert.equal(h.timer.snapshot().frameSeq, 3);
  assert.equal(h.timer.sample(), null);
  // An older completion cannot replace the last frame.
  h.queries[0].ready = true; h.timer.poll();
  assert.equal(h.timer.snapshot().frameSeq, 3);
  h.time(1011);
  assert.equal(h.timer.snapshot().status, "stale");
  assert.equal(h.timer.snapshot().gpuMS, null);
  h.timer.dispose();
  assert.equal(h.timer.snapshot().status, "disposed");
  assert.ok(h.queries.every(q => q.deleted));
});

test("WebGL disjoint invalidates all pending samples and recovers on a new frame", () => {
  const h = harness(); h.timer.end(h.timer.begin());
  h.queries[0].ready = true;
  assert.equal(h.timer.snapshot().status, "measured");
  h.timer.end(h.timer.begin()); h.disjoint(true); h.timer.poll();
  assert.equal(h.timer.sample(), null);
  assert.ok(h.queries.slice(0, 3).every(q => q.deleted));
  h.disjoint(false);
  assert.equal(h.timer.snapshot().status, "disjoint");
  assert.equal(h.timer.snapshot().gpuMS, null);
  h.timer.end(h.timer.begin()); h.queries.at(-3).ready = true;
  assert.equal(h.timer.snapshot().status, "measured");
});

test("WebGL nested water cannot begin a query inside the world's frame query", () => {
  const h = harness(), outer = h.timer.begin();
  const ctx = { performance: { now: () => 10 } }; vm.createContext(ctx); vm.runInContext(source, ctx);
  const water = ctx.createSceneWebGLFrameTimer(h.gl);
  assert.equal(water.begin(), null);
  water.end(null); h.timer.end(outer);
  assert.ok(water.begin());
});

test("WebGL missing timers, allocation failure, and context loss do not invent GPU values", () => {
  const absent = harness({ unavailable: true });
  assert.equal(absent.timer.snapshot().status, "unavailable");
  assert.equal(absent.timer.begin(), null);
  assert.equal(absent.timer.snapshot().gpuMS, null);
  const failed = harness({ failAt: 1 });
  assert.equal(failed.timer.snapshot().status, "failed");
  assert.ok(failed.queries.every(q => q.deleted));
  const lost = harness(); lost.timer.end(lost.timer.begin()); lost.lost();
  assert.equal(lost.timer.snapshot().status, "failed");
  assert.equal(lost.timer.snapshot().gpuMS, null);
});
