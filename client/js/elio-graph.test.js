"use strict";
// The generic Elio graph runner: parameter sources, tweens, pass conditions, uniform packing and validation.
// Executors have their own files (elio-graph-webgl2.test.js, elio-graph-webgpu.test.js).
const test = require("node:test"), assert = require("node:assert/strict");
const { runnerContext, testGraph } = require("./elio-graph-test-support.js");

const api = () => runnerContext().window.__gosx_elio_graph;
const decl = (name, source, extra) => Object.assign({ name, type: "f32", source }, extra);
const frame = (time, dt = 1 / 60, camera = {}) => Object.assign({ time, dt, cameraX: 0, cameraY: 0, cameraZ: 0 }, camera);
const evaluator = (params) => api().createParams(params);

test("frame sources supply time, dt and the camera", () => {
  const p = evaluator([decl("t", { kind: "frame", name: "time" }), decl("dt", { kind: "frame", name: "dt" }), decl("cx", { kind: "frame", name: "cameraX" }),
    decl("cy", { kind: "frame", name: "cameraY" }), decl("cz", { kind: "frame", name: "cameraZ" })]);
  p.evaluate(frame(12.5, 0.25, { cameraX: 1, cameraY: 30, cameraZ: -4 }));
  assert.deepEqual([p.current.t, p.current.dt, p.current.cx, p.current.cy, p.current.cz], [12.5, 0.25, 1, 30, -4]);
  assert.throws(() => evaluator([decl("x", { kind: "frame", name: "wallClock" })]).evaluate(frame(0)), /unknown frame source/);
});

test("value sources start at their value and change through setValue", () => {
  const p = evaluator([decl("windSpeed", { kind: "value", value: 9 }), { name: "on", type: "bool", source: { kind: "value", value: 2 } }, { name: "n", type: "u32", source: { kind: "value", value: 3.7 } }]);
  p.evaluate(frame(0));
  assert.deepEqual([p.current.windSpeed, p.current.on, p.current.n], [9, 1, 3], "bool and u32 are coerced");
  p.set("windSpeed", 15);
  p.evaluate(frame(1));
  assert.equal(p.current.windSpeed, 15);
  assert.throws(() => p.set("nope", 1), /not a value parameter/);
  const q = evaluator([decl("t", { kind: "frame", name: "time" })]);
  assert.throws(() => q.set("t", 1), /not a value parameter/, "only value parameters take scene updates");
});

test("expr sources evaluate in declaration order: add, sub, mul, div, mod, floor, min, max", () => {
  const e = (name, op, ...args) => decl(name, { kind: "expr", op, args });
  const p = evaluator([decl("a", { kind: "value", value: 7 }), decl("b", { kind: "value", value: -3 }),
    e("add", "add", "param:a", "param:b"), e("sub", "sub", "param:a", 1), e("mul", "mul", "param:a", "param:b"), e("div", "div", "param:a", 2),
    e("mod", "mod", "param:b", 5), e("floor", "floor", "param:b"), e("min", "min", "param:a", "param:b"), e("max", "max", "param:a", "param:b"),
    e("divzero", "div", "param:a", 0), e("chain", "mul", "param:add", 10)]);
  p.evaluate(frame(0));
  const c = p.current;
  assert.deepEqual([c.add, c.sub, c.mul, c.div, c.mod, c.floor, c.min, c.max, c.divzero, c.chain], [4, 6, -21, 3.5, 2, -3, -3, 7, 0, 40]);
  assert.equal(c.mod, 2, "mod follows the divisor's sign, as in GLSL and WGSL");
  assert.throws(() => evaluator([e("x", "pow", 1, 2)]).evaluate(frame(0)), /unknown op/);
});

test("the loop-time and foam-cell expressions an effect needs come out of expr and accumulate", () => {
  const p = evaluator([decl("time", { kind: "frame", name: "time" }), decl("dt", { kind: "frame", name: "dt" }), decl("loop", { kind: "value", value: 1000 }),
    decl("tmod", { kind: "expr", op: "mod", args: ["param:time", "param:loop"] }),
    decl("driftX", { kind: "value", value: 2 }), decl("waterX", { kind: "accumulate", of: "param:driftX" }),
    decl("camX", { kind: "frame", name: "cameraX" }), decl("rel", { kind: "expr", op: "sub", args: ["param:camX", "param:waterX"] }),
    decl("cell", { kind: "expr", op: "floor", args: ["param:rel", 0.5] })]);
  p.evaluate(frame(1234.5, 0.5, { cameraX: 10 }));
  assert.equal(p.current.tmod, 234.5);
  assert.equal(p.current.waterX, 1, "integrates drift 2 m/s over dt 0.5");
  p.evaluate(frame(1235, 0.5, { cameraX: 10 }));
  assert.equal(p.current.waterX, 2);
  assert.equal(p.current.rel, 8);
  assert.equal(p.current.cell, 8, "floor ignores its second argument");
  p.set("driftX", 0);
  p.evaluate(frame(1235.5, 0.5, { cameraX: 10 }));
  assert.equal(p.current.waterX, 2, "a zero rate holds the offset");
});

test("a changed value tweens over presentation seconds; the first frame after the update starts the clock", () => {
  const p = evaluator([decl("windSpeed", { kind: "value", value: 9 }, { tween: 20 })]);
  p.evaluate(frame(100));
  assert.equal(p.current.windSpeed, 9, "the initial value is not tweened");
  p.set("windSpeed", 29);
  p.evaluate(frame(105));
  assert.equal(p.current.windSpeed, 9, "the tween starts at the first frame after the update");
  p.evaluate(frame(115));
  assert.equal(p.current.windSpeed, 19);
  p.evaluate(frame(125));
  assert.equal(p.current.windSpeed, 29);
  p.evaluate(frame(126));
  assert.equal(p.current.windSpeed, 29);
  // A second change mid-tween restarts from where the value is now.
  p.set("windSpeed", 9);
  p.evaluate(frame(130));
  p.evaluate(frame(140));
  assert.equal(p.current.windSpeed, 19);
  const noTween = evaluator([decl("lambda", { kind: "value", value: 0.6 })]);
  noTween.evaluate(frame(0)); noTween.set("lambda", 1); noTween.evaluate(frame(1));
  assert.equal(noTween.current.lambda, 1, "no tween means no delay");
});

test("changed: is true on the first frame and on every frame a listed parameter moves, including during a tween", () => {
  const p = evaluator([decl("windSpeed", { kind: "value", value: 9 }, { tween: 10 }), decl("seed", { kind: "value", value: 7 }), decl("t", { kind: "frame", name: "time" })]);
  const { holds } = api();
  const when = "changed:windSpeed, seed";
  p.evaluate(frame(0));
  assert.equal(holds(when, p.current, p.changed), true, "first frame");
  p.evaluate(frame(1));
  assert.equal(holds(when, p.current, p.changed), false, "nothing moved (time moves, but it is not listed)");
  p.set("windSpeed", 19);
  p.evaluate(frame(2));
  assert.equal(holds(when, p.current, p.changed), false, "the tween clock starts here; the value has not moved yet");
  p.evaluate(frame(4));
  assert.equal(holds(when, p.current, p.changed), true, "mid-tween");
  p.evaluate(frame(8));
  assert.equal(holds(when, p.current, p.changed), true);
  p.evaluate(frame(12));
  assert.equal(holds(when, p.current, p.changed), true, "arrives");
  p.evaluate(frame(13));
  assert.equal(holds(when, p.current, p.changed), false, "settled");
  p.set("seed", 8);
  p.evaluate(frame(14));
  assert.equal(holds(when, p.current, p.changed), true, "an untweened parameter");
  assert.equal(holds("changed:t", p.current, p.changed), true, "a frame source that moves every frame");
  assert.equal(holds("always", p.current, p.changed), true);
  assert.equal(holds("param:seed", p.current, p.changed), true);
  assert.equal(holds("param:nothing", p.current, p.changed), false);
  assert.throws(() => holds("sometimes", p.current, p.changed), /bad when/);
});

test("uniform packing follows the layout and rejects typos, missing fields and unsupplied parameters", () => {
  const { pack, kernelKey } = api();
  assert.equal(kernelKey("ocean_evolve"), "oceanEvolve");
  assert.equal(kernelKey("fft_gather_64"), "fftGather64");
  const layout = runnerContext().window.__gosx_scene3d_elio_ocean.kernels.oceanEvolve.uniforms[0];
  const field = (name) => layout.fields.find((f) => f.name === name);
  const view = new DataView(pack(layout, { n: 64, t: "param:t", tile: 250, loopPeriod: 1000 }, { t: 12.5 }, "evolve"));
  assert.equal(view.getUint32(field("n").offset, true), 64);
  assert.equal(view.getFloat32(field("t").offset, true), 12.5);
  assert.throws(() => pack(layout, { n: 64, t: 1, tile: 1 }, {}, "evolve"), /loopPeriod/);
  assert.throws(() => pack(layout, { n: 64, t: 1, tile: 1, loopPeriod: 1, tt: 2 }, {}, "evolve"), /no field tt/);
  assert.throws(() => pack(layout, { n: 64, t: "param:missing", tile: 1, loopPeriod: 1 }, {}, "evolve"), /missing was not supplied/);
});

test("check rejects graphs the runner cannot run, before any GPU object exists", () => {
  const { check } = api();
  const module = runnerContext().window.__gosx_scene3d_elio_ocean;
  const ok = testGraph("webgl2");
  check(ok, module, "webgl2");
  const clone = () => JSON.parse(JSON.stringify(ok));
  assert.throws(() => check(ok, module, "webgpu"), /not webgpu/);
  const g1 = clone(); g1.passes[0].when = "changed:nothing"; assert.throws(() => check(g1, module, "webgl2"), /undeclared parameter nothing/);
  const g2 = clone(); g2.passes[0].kernel = "ocean_nothing"; assert.throws(() => check(g2, module, "webgl2"), /no kernel/);
  const g3 = clone(); g3.exports.x = "a0"; assert.throws(() => check(g3, module, "webgl2"), /must name a texture/);
  const g4 = clone(); g4.passes[0].bindings[0].resource = "nope"; assert.throws(() => check(g4, module, "webgl2"), /unknown resource/);
  const g5 = clone(); g5.version = 2; assert.throws(() => check(g5, module, "webgl2"), /version/);
  const g6 = clone(); g6.passes[0].uniforms[0].values.tile = "param:nothing"; assert.throws(() => check(g6, module, "webgl2"), /undeclared parameter/);
});

test("a graph that cannot be built is disabled with a reason, not thrown", () => {
  const ctx = runnerContext(), g = ctx.window.__gosx_elio_graph, module = ctx.window.__gosx_scene3d_elio_ocean;
  const failures = [];
  const r = g.create({ module, backend: "webgl2", tier: "high", host: {}, onFail: (reason) => failures.push(reason) });
  assert.equal(r.failed, "no-graph");
  assert.equal(r.run(frame(0)), false);
  assert.equal(r.export("disp0"), null);
  assert.deepEqual(failures, ["no-graph"]);
  const bad = g.create({ module, backend: "webgl2", graph: Object.assign(testGraph("webgl2"), { version: 9 }), host: {} });
  assert.ok(bad.failed);
  bad.dispose();
});

test("delta sources report the change of another parameter since the last frame, 0 on the first", () => {
  const p = evaluator([decl("cell", { kind: "frame", name: "cameraX" }), { name: "shift", type: "i32", source: { kind: "delta", of: "param:cell" } }]);
  p.evaluate(frame(0, 0.1, { cameraX: 5 }));
  assert.equal(p.current.shift, 0, "first frame");
  p.evaluate(frame(1, 0.1, { cameraX: 8 }));
  assert.equal(p.current.shift, 3);
  p.evaluate(frame(2, 0.1, { cameraX: 8 }));
  assert.equal(p.current.shift, 0);
  p.evaluate(frame(3, 0.1, { cameraX: 6 }));
  assert.equal(p.current.shift, -2);
});

test("f32 parameters stay in float64 until a uniform is packed, so a wrapped water offset keeps its precision", () => {
  const p = evaluator([decl("drift", { kind: "value", value: 3.3 }), decl("dt", { kind: "frame", name: "dt" }), decl("water", { kind: "accumulate", of: "param:drift" }),
    decl("wrapped", { kind: "expr", op: "mod", args: ["param:water", 1600] })]);
  for (let i = 0; i < 3600; i++) p.evaluate(frame(i / 60, 1 / 60));
  assert.ok(Math.abs(p.current.water - 3.3 * 60) < 1e-6, "an hour of 60 Hz frames at 3.3 m/s");
  assert.ok(Math.abs(p.current.wrapped - ((3.3 * 60) % 1600)) < 1e-6);
});

test("buffer parameters hold arrays; a new array marks them changed", () => {
  const p = evaluator([{ name: "probes", type: "vec4<f32>[]", count: 2, source: { kind: "value", value: [1, 2, 3, 4, 5, 6, 7, 8] } }]);
  p.evaluate(frame(0));
  assert.deepEqual(Array.from(p.current.probes), [1, 2, 3, 4, 5, 6, 7, 8]);
  assert.ok(p.current.probes instanceof Float32Array);
  p.evaluate(frame(1));
  assert.equal(p.changed.probes, false);
  p.set("probes", [9, 9, 9, 9]);
  p.evaluate(frame(2));
  assert.equal(p.changed.probes, true);
  assert.deepEqual(Array.from(p.current.probes), [9, 9, 9, 9]);
});

test("repeat passes expand into steps: index values substituted per iteration, swaps after each, nested repeats multiply", () => {
  const { expand } = api();
  const body = { name: "stage", type: "kernel", kernel: "fft_gather_64", entry: "fft_stage", when: "always", bindings: [], uniforms: [{ name: "fft", values: { n: 64, stage: "param:s", axis: "param:a" } }] };
  const steps = expand([{ type: "repeat", name: "axes", count: 2, index: "a", swap: [["x", "y"]], body: [{ type: "repeat", name: "stages", count: 3, index: "s", swap: [["p", "q"]], body: [body] }] }], {}, []);
  const kinds = steps.map((st) => st.swap ? "swap:" + st.swap[0].join("") : "pass");
  assert.deepEqual(kinds, ["pass", "swap:pq", "pass", "swap:pq", "pass", "swap:pq", "swap:xy", "pass", "swap:pq", "pass", "swap:pq", "pass", "swap:pq", "swap:xy"]);
  const values = steps.filter((st) => st.pass).map((st) => [st.pass.uniforms[0].values.axis, st.pass.uniforms[0].values.stage]);
  assert.deepEqual(values, [[0, 0], [0, 1], [0, 2], [1, 0], [1, 1], [1, 2]]);
  assert.equal(body.uniforms[0].values.stage, "param:s", "the source graph is not modified");
  const check = api().check, module = runnerContext().window.__gosx_scene3d_elio_ocean;
  const g = testGraph("webgl2");
  g.passes = [{ type: "repeat", name: "r", count: 2, index: "nothing", body: [] }];
  assert.throws(() => check(g, module, "webgl2"), /declared index parameter/);
});

test("exportParams are read from the runner in float64; a buffer may be exported on webgpu only", () => {
  const ctx = runnerContext(), g = ctx.window.__gosx_elio_graph, module = ctx.window.__gosx_scene3d_elio_ocean;
  const graph = testGraph("webgl2");
  graph.params.push({ name: "water", type: "f32", source: { kind: "value", value: 1234.56789012345 } });
  graph.exportParams = ["water", "lambda"];
  const warn = console.warn; console.warn = () => {};
  try {
    const r = g.create({ module, backend: "webgl2", graph, host: {} });
    assert.ok(r.failed, "no gl in the host");
    graph.exportParams = ["nothing"];
    assert.throws(() => g.check(graph, module, "webgl2"), /exportParams names undeclared/);
    graph.exportParams = [];
    const bufferExport = Object.assign(testGraph("webgl2"), { exports: { probes: "h0" } });
    assert.throws(() => g.check(bufferExport, module, "webgl2"), /must name a texture/);
    const gpu = Object.assign(testGraph("webgpu"), { exports: { probes: "h0" } });
    g.check(gpu, module, "webgpu");
    const fed = testGraph("webgl2"); fed.resources[0].feed = "nothing";
    assert.throws(() => g.check(fed, module, "webgl2"), /fed by undeclared parameter/);
  } finally { console.warn = warn; }
  const p = evaluator([decl("w", { kind: "value", value: 1234.56789012345 })]);
  p.evaluate(frame(0));
  assert.equal(p.current.w, 1234.56789012345);
});
