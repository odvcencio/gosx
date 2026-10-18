import assert from "node:assert/strict";
import fs from "node:fs";
import test from "node:test";
import vm from "node:vm";

const read = file => fs.readFileSync(new URL(file, import.meta.url), "utf8");
const core = read("./bootstrap-src/06-motion-core.ts");
const track = read("../runtime/motion/track.ts"), transport = read("../runtime/motion/transport.ts");
const fixtures = JSON.parse(read("../../motion/testdata/track_signal_golden.json"));
function runtime() {
  let clock = 0, id = 0, reduced;
  const frames = new Map();
  const body = {nodeType: 1, querySelectorAll() {return [];}};
  const document = {body, documentElement: body, hidden: false, addEventListener() {}, removeEventListener() {}};
  const window = {__gosx: {}, console, addEventListener() {},
    matchMedia() {return {matches: false, addEventListener(_name, fn) {reduced = fn;}};},
    requestAnimationFrame(fn) {frames.set(++id, fn); return id;}, cancelAnimationFrame(key) {frames.delete(key);}};
  const context = vm.createContext({window, document, console, performance: {now: () => clock}, setTimeout, clearTimeout, queueMicrotask});
  vm.runInContext(core, context);
  return {motion: window.__gosx.motion, frames,
    load(source = track) {vm.runInContext(source, context);},
    advance(ms = clock) {clock = ms; const callbacks = [...frames.values()]; frames.clear(); callbacks.forEach(fn => fn(ms));},
    reduce(value) {reduced({matches: value});},
  };
}

test("prepared scalar tracks agree with native Eval for easing, holds, step boundaries, stagger and reverse seeks", () => {
  const r = runtime(); r.load();
  for (const fixture of fixtures) {
    const sample = r.motion.createTrackSampler(fixture.signal.frames);
    for (const [time, want] of fixture.samples) {
      assert.ok(Math.abs(sample(time) - want) < 1e-7, `${JSON.stringify(fixture.signal)} at ${time}: ${sample(time)} vs ${want}`);
    }
  }
  assert.equal(r.frames.size, 0, "sampling never owns a clock");
});

test("samplers own their snapshots and reject malformed or excessive tracks", () => {
  const r = runtime(); r.load();
  const keys = [{at: 0, value: 0, ease: {kind: 1, args: [3]}}, {at: 1, value: 8}];
  const sample = r.motion.createTrackSampler(keys);
  keys[0].value = 100; keys[0].ease.args[0] = 1; keys[1].at = 20;
  assert.equal(sample(.5), 1); assert.equal(sample(-1), 0); assert.equal(sample(100), 8);
  for (const bad of [null, [], [{}], Array(4097).fill({}), [{at: 0,value: 1},{at: 0,value: 2}],
    [{at: 0,value: NaN},{at: 1,value: 1}], [{at: 0,value: 1,ease: {kind: 99}},{at: 1,value: 2}],
    [{at: 0,value: 1,ease: {kind: 4,args: [2,0,1,1]}},{at: 1,value: 2}],
    [{at: 0,value: 1,ease: {kind: 5,args: [1.5]}},{at: 1,value: 2}]]) {
    assert.throws(() => r.motion.createTrackSampler(bad));
  }
  for (const bad of [NaN, Infinity, "1"]) assert.throws(() => sample(bad), /finite/);
});

test("DOM bindings compose opacity and transform components, coalesce per binding and cancel pending writes on disposal", () => {
  const r = runtime(), el = {style: {transform: "perspective(600px)",opacity: "1"}};
  const x = r.motion.signal(0), y = r.motion.signal(0), scale = r.motion.signal(1), opacity = r.motion.signal(1);
  const stops = [r.motion.bind(x, el, "transform.x", "px"), r.motion.bind(y, el, "transform.y", "px"),
    r.motion.bind(scale, el, "transform.scale", ""), r.motion.bind(opacity, el, "opacity", "")];
  x.set(10); x.set(12); y.set(24); scale.set(1.5); opacity.set(.4); r.advance();
  assert.equal(el.style.opacity, "0.4");
  assert.equal(el.style.transform, "translate3d(12px, 24px, 0px) scale(1.5) perspective(600px)");
  y.set(-3); x.set(-5); r.advance();
  assert.equal(el.style.transform, "translate3d(-5px, -3px, 0px) scale(1.5) perspective(600px)");
  const before = {...el.style};
  x.set(99); y.set(99); opacity.set(0); stops.forEach(stop => stop()); r.advance();
  assert.deepEqual(el.style, before, "disposed bindings must cancel their already queued writes");
  assert.equal(r.frames.size, 0);
});

function mount(r) {
  const title = {style: {opacity: ""}}, scene = {}, writes = [];
  const program = {version: 1, id: "story", transport: {durationMs: 2000}, signals: [
    {id: "clock", kind: "playhead"},
    {id: "out", kind: "track", input: "clock", frames: [{at: 0,value: 0,ease: {kind: 1,args: [2]}},{at: 2,value: 1}]},
    {id: "gain", kind: "map", input: "out", from: 0,to: 1,min: 0,max: 2},
  ], bindings: [
    {signal: "out", target: "style", selector: ".title", property: "opacity"},
    {signal: "gain", target: "camera", selector: "#scene", property: "position.x"},
    {signal: "gain", target: "materialUniform", selector: "#scene", node: "orb", property: "gain"},
  ]};
  const root = {nodeType: 1, hasAttribute: name => name === "data-gosx-motion-program", getAttribute: () => JSON.stringify(program),
    querySelectorAll(selector) {return selector === ".title" ? [title] : selector === "#scene" ? [scene] : [];}};
  r.motion.attachScene(scene, {write(binding, value) {writes.push([binding.target, value]);},invalidate() {}});
  r.motion.mountPrograms(root);
  return {root, title, writes};
}

test("track and transport chunks tolerate either load order, deferred loading, composition, policy changes and disposal", () => {
  for (const order of ["track-first", "transport-first", "both-first", "late-track"]) {
    const r = runtime();
    if (order === "track-first" || order === "both-first") r.load();
    if (order === "transport-first" || order === "both-first" || order === "late-track") r.load(transport);
    const {root, title, writes} = mount(r);
    if (order === "late-track") r.motion.transport("story").seek(1500);
    if (order !== "track-first" && order !== "both-first") r.load();
    if (order === "track-first") r.load(transport);
    r.advance();
    if (order === "late-track") assert.equal(Number(title.style.opacity), .5625, "late feature must inherit current time");
    const t = r.motion.transport("story");
    for (const [ms, want] of [[500,.0625],[1500,.5625],[500,.0625]]) {
      t.seek(ms); r.advance(ms);
      assert.equal(Number(title.style.opacity), want, order);
      assert.deepEqual(writes.slice(-2), [["camera",want * 2],["materialUniform",want * 2]], order);
    }
    r.reduce(true); r.advance(); assert.equal(Number(title.style.opacity), 1);
    r.reduce(false); t.seek(500); r.advance(); assert.equal(Number(title.style.opacity), .0625);
    r.load(); r.advance(); // Loading the optional chunk twice is harmless.
    r.motion.disposePrograms(root); r.advance();
    const count = writes.length;
    r.reduce(true); r.advance();
    assert.equal(writes.length, count); assert.equal(r.frames.size, 0, "disposed tracks leave no RAF");
  }
});
