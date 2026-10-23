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
  const r = g.create({ module, backend: "webgl2", tier: "ultra", host: {}, onFail: (reason) => failures.push(reason) });
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

test("repeat passes expand as Elio's graph.Expand does: named <repeat>[i].<pass>, indexes substituted, swaps persisting and cancelling", () => {
  const { expand } = api();
  const kernel = (name, src, dst, values, extra) => Object.assign({ name, type: "kernel", kernel: "fft_gather_64", entry: "fft_stage", when: "always",
    bindings: [{ name: "src", resource: src, level: 0 }, { name: "dst", resource: dst, level: 0 }], uniforms: [{ name: "fft", values }] }, extra);
  const res = ["p", "q", "x", "y"].map((name) => ({ name, kind: "buffer", elementType: "vec4<f32>", count: 4096, width: 64 }));
  const body = kernel("stage", "p", "q", { n: 64, stage: "param:s", axis: "param:a" });
  const graph = { params: [], resources: res, passes: [{ name: "axes", type: "repeat", count: 2, index: "a", swap: [["x", "y"]], when: "changed:t", body: [
    { name: "stages", type: "repeat", count: 3, index: "s", swap: [["p", "q"]], body: [body, kernel("other", "x", "y", { n: 64, stage: 0, axis: "param:a" })] }] }] };
  const out = Array.from(expand(graph));
  assert.deepEqual(out.map((p) => p.name), ["axes[0].stages[0].stage", "axes[0].stages[0].other", "axes[0].stages[1].stage", "axes[0].stages[1].other", "axes[0].stages[2].stage", "axes[0].stages[2].other",
    "axes[1].stages[0].stage", "axes[1].stages[0].other", "axes[1].stages[1].stage", "axes[1].stages[1].other", "axes[1].stages[2].stage", "axes[1].stages[2].other"]);
  const stage = out.filter((p) => /\.stage$/.test(p.name));
  assert.deepEqual(stage.map((p) => [p.uniforms[0].values.axis, p.uniforms[0].values.stage]), [[0, 0], [0, 1], [0, 2], [1, 0], [1, 1], [1, 2]]);
  assert.deepEqual(stage.slice(0, 3).map((p) => p.bindings.map((b) => b.resource).join()), ["p,q", "q,p", "p,q"], "p and q swap after each stage");
  assert.deepEqual(stage.slice(3).map((p) => p.bindings.map((b) => b.resource).join()), ["q,p", "p,q", "q,p"], "three swaps leave the pair exchanged until the outer repeat's next iteration");
  const other = out.filter((p) => /\.other$/.test(p.name));
  assert.deepEqual(other.map((p) => p.bindings[0].resource), ["x", "x", "x", "y", "y", "y"], "the outer swap applies after its iteration");
  assert.ok(out.every((p) => p.when === "changed:t"), "a repeat's condition applies to passes that have none");
  assert.equal(body.uniforms[0].values.stage, "param:s", "the source graph is not modified");
  assert.throws(() => expand(Object.assign({}, graph, { passes: [Object.assign({}, graph.passes[0], { swap: [["x", "y"]], count: 1 })] })), /swaps are not restored/);
  const bad = (edit) => expand(Object.assign({}, graph, { passes: [Object.assign({}, graph.passes[0], edit)] }));
  assert.throws(() => bad({ count: 0 }), /want at least 1/);
  assert.throws(() => bad({ index: "" }), /index/);
  assert.throws(() => bad({ body: [] }), /body is empty/);
  assert.throws(() => expand(Object.assign({}, graph, { params: [{ name: "a" }] })), /names a declared param/);
});

test("IndexExpr values and level bindings follow Elio: i + offset, or max(shr >> (i + offset), 1)", () => {
  const { expand } = api();
  const mip = { name: "mip", type: "kernel", kernel: "ocean_mip", entry: "ocean_mip", when: "always", dispatch: [16, 1, 1],
    bindings: [{ name: "src", resource: "d", level: 0, levelAt: { index: "l" } }, { name: "dst", resource: "d", level: 0, levelAt: { index: "l", offset: 1 } }],
    uniforms: [{ name: "mip", values: { srcLevel: 0, dstWidth: { index: "l", offset: 1, shr: 64 }, dstHeight: { index: "l", offset: 1, shr: 64 }, pad: 0 } }] };
  const out = Array.from(expand({ params: [], resources: [{ name: "d", kind: "texture", format: "rgba16float", width: 64, height: 64, mipLevels: 7 }], passes: [{ name: "mips", type: "repeat", count: 6, index: "l", body: [mip] }] }));
  assert.deepEqual(out.map((p) => [p.bindings[0].level, p.bindings[1].level, p.uniforms[0].values.dstWidth]), [[0, 1, 32], [1, 2, 16], [2, 3, 8], [3, 4, 4], [4, 5, 2], [5, 6, 1]]);
  assert.ok(out.every((p) => !("levelAt" in p.bindings[0])));
  assert.throws(() => expand({ params: [], resources: [], passes: [mip] }), /not in scope/);
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

test("sin and cos are unary expr ops over radians", () => {
  const e = (name, op, ...args) => decl(name, { kind: "expr", op, args });
  const p = evaluator([decl("h", { kind: "value", value: Math.PI / 6 }), e("s", "sin", "param:h"), e("c", "cos", "param:h"), e("s0", "sin", 0), e("c0", "cos", 0),
    e("fwdX", "mul", "param:s", 10), e("fwdZ", "mul", "param:c", 10)]);
  p.evaluate(frame(0));
  assert.ok(Math.abs(p.current.s - 0.5) < 1e-12 && Math.abs(p.current.c - Math.sqrt(3) / 2) < 1e-12);
  assert.deepEqual([p.current.s0, p.current.c0], [0, 1]);
  assert.ok(Math.abs(p.current.fwdX - 5) < 1e-12, "forward is (sin, cos) in (x, z)");
});

test("bool parameters read as 0 and 1 in expr, delta and accumulate", () => {
  const p = evaluator([{ name: "on", type: "bool", source: { kind: "value", value: 0 } }, decl("dt", { kind: "frame", name: "dt" }),
    decl("scaled", { kind: "expr", op: "mul", args: ["param:on", 5] }), { name: "edge", type: "i32", source: { kind: "delta", of: "param:on" } },
    { name: "reset", type: "u32", source: { kind: "expr", op: "max", args: ["param:edge", 0] } }, decl("age", { kind: "accumulate", of: "param:on" })]);
  p.evaluate(frame(0, 0.5));
  assert.deepEqual([p.current.scaled, p.current.edge, p.current.reset, p.current.age], [0, 0, 0, 0]);
  p.set("on", true);
  p.evaluate(frame(1, 0.5));
  assert.deepEqual([p.current.on, p.current.scaled, p.current.edge, p.current.reset, p.current.age], [1, 5, 1, 1, 0.5], "switching on is a rising edge of 1 and the accumulator integrates 1 per second");
  p.evaluate(frame(2, 0.5));
  assert.deepEqual([p.current.edge, p.current.reset, p.current.age], [0, 0, 1]);
  p.set("on", 0);
  p.evaluate(frame(3, 0.5));
  assert.deepEqual([p.current.edge, p.current.reset], [-1, 0], "switching off is a falling edge; reset is its positive part");
});

test("node sources read a scene node's pose from the host each frame; speed is the horizontal velocity of its last two poses unless the node reports one", () => {
  const p = evaluator([decl("hullX", { kind: "node", node: "hull", field: "x" }), decl("hullZ", { kind: "node", node: "hull", field: "z" }),
    decl("hullHeading", { kind: "node", node: "hull", field: "heading" }), decl("hullSpeed", { kind: "node", node: "hull", field: "speed" }),
    decl("otherX", { kind: "node", node: "other", field: "x" })]);
  const poses = { ship: { x: 10, z: -5, heading: 1.5 }, tug: { x: 3, z: 4, heading: 0, speed: 7 } };
  const f = (time, dt, extra) => Object.assign(frame(time, dt), { nodePose: (id) => poses[id] || null }, extra);
  p.setNodes({ hull: "ship", other: "tug" });
  p.evaluate(f(0, 0.5));
  assert.deepEqual([p.current.hullX, p.current.hullZ, p.current.hullHeading, p.current.hullSpeed, p.current.otherX], [10, -5, 1.5, 0, 3], "no earlier pose, no speed");
  poses.ship = { x: 13, z: -1, heading: 1.5 };
  p.evaluate(f(0.5, 0.5));
  assert.equal(p.current.hullSpeed, 10, "(3, 4) metres in 0.5 s is 10 m/s");
  poses.ship = { x: 13, z: -1, heading: 1.5 };
  p.evaluate(f(1, 0.5));
  assert.equal(p.current.hullSpeed, 0, "stopped");
  poses.ship = { x: 13, z: -1, heading: 1.5, speed: 4.5 };
  p.evaluate(f(1.5, 0.5));
  assert.equal(p.current.hullSpeed, 4.5, "a reported speed wins");
  // A node that is not there holds its last value; an unmapped logical name is taken as a node id.
  delete poses.ship;
  p.evaluate(f(2, 0.5));
  assert.deepEqual([p.current.hullX, p.current.hullZ, p.current.hullSpeed], [13, -1, 0], "missing node: last position, no speed");
  p.setNodes({});
  const q = evaluator([decl("x", { kind: "node", node: "tug", field: "x" })]);
  q.evaluate(f(0, 0.5));
  assert.equal(q.current.x, 3, "unmapped name is the node id itself");
  assert.throws(() => evaluator([decl("x", { kind: "node", node: "a", field: "roll" })]).evaluate(f(0, 0.5)), /unknown node field/);
  // Without a host lookup every node field is 0 (a graph on a host that has no scene nodes still runs).
  const r = evaluator([decl("x", { kind: "node", node: "a", field: "x" })]);
  r.evaluate(frame(0));
  assert.equal(r.current.x, 0);
});
