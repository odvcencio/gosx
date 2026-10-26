import assert from "node:assert/strict";
import fs from "node:fs";
import test from "node:test";
import vm from "node:vm";

const source = file => fs.readFileSync(new URL(file, import.meta.url), "utf8");
const core = source("./bootstrap-src/06-motion-core.ts"), transport = source("../runtime/motion/transport.ts");
const scene = source("../runtime/motion/easing.ts") + source("../runtime/motion/scene.ts");
const golden = JSON.parse(source("../../scene/testdata/command-transition.json"));
const plain = value => JSON.parse(JSON.stringify(value));
function runtime() {
  let clock = 0, id = 0, reduce;
  const frames = new Map(), observers = [];
  class Observer {constructor(fn) {this.callback = fn; observers.push(this);} observe() {} disconnect() {this.disconnected = true;}}
  const body = {nodeType: 1, querySelectorAll() {return [];}};
  const document = {body, documentElement: body, hidden: false, addEventListener() {}, removeEventListener() {}};
  const window = {__gosx: {}, console, addEventListener() {}, matchMedia: () => ({matches: false, addEventListener(_name, fn) {reduce = fn;}}),
    requestAnimationFrame(fn) {frames.set(++id, fn); return id;}, cancelAnimationFrame(id) {frames.delete(id);}};
  const json = {parse: JSON.parse, stringify: JSON.stringify};
  const context = vm.createContext({window, document, MutationObserver: Observer, console, AbortController, TextEncoder, CSS: {supports: () => true}, JSON: json, performance: {now: () => clock}, setTimeout, clearTimeout});
  vm.runInContext(core, context);
  return {window, motion: window.__gosx.motion, frames, observers, json,
    load(code = scene) {vm.runInContext(code, context);},
    reduce(value) {reduce({matches: value});},
    advance() {clock += 16; const pending = [...frames.values()]; frames.clear(); pending.forEach(fn => fn(clock));},
    async flush() {for (let i = 0; i < 30; i++) {await Promise.resolve(); this.advance();}}};
}
const mount = () => ({getAttribute() {return null;}, __gosxScene3DHandle: {setAnimationClock() {}, getAnimationClock() {return {timeSeconds: 0, paused: true, pending: false};}}});
const deferred = () => {let resolve, reject; const promise = new Promise((a,b) => {resolve=a;reject=b;}); return {promise, resolve, reject};};

test("native/browser golden poses are absolute, immutable, compiled once and reversible", () => {
  const r = runtime(); r.load();
  const from = plain(golden.from), p = r.motion.prepareSceneTransition(from, golden.to);
  from[0].data.x = 999;
  r.json.stringify = () => {throw Error("serialized while sampling");};
  const mid = p.sample(.5); assert.deepEqual(plain(mid), golden.midpoint);
  assert.equal(p.sample(.5), mid, "same absolute pose reuses its prepared result");
  assert.throws(() => {mid[0].data.x = 999;}, TypeError);
  p.sample(1); p.sample(0); assert.deepEqual(plain(p.sample(.5)), golden.midpoint);
  assert.equal(p.sample(-100), p.sample(0)); assert.equal(p.sample(100), p.sample(1));
  assert.throws(() => p.sample(NaN), /finite/); assert.equal(r.frames.size, 0);
});

test("retained upserts reset omitted zeros, keep valid topology and use explicit removals", () => {
  const r = runtime(); r.load();
  const create = (geometry, props) => ({kind: 0, objectId: "edge", data: {geometry, props}});
  const a = create("lines", {points: [{x: 1}], lineSegments: [[0,1]]}), b = create("lines", {points: [{x: 5,z: 4}], lineSegments: [[1,0]]});
  const p = r.motion.prepareSceneTransition([{kind: 1, objectId: "edge"}, a], [b]);
  assert.equal(p.sample(.5).length, 1); const props = p.sample(.5)[0].data.props;
  assert.deepEqual(plain(props.points), [{x: 3,y: 0,z: 2}]); assert.deepEqual(plain(props.lineSegments), [[0,1]]);
  assert.equal(p.sample(0)[0].data.props.scaleX, 1); assert.equal(a.data.props.z, undefined);
  const shape = r.motion.prepareSceneTransition([a], [create("sphere", {radius: 2})]);
  assert.equal(shape.sample(.5)[0].data.geometry, "lines"); assert.equal(shape.sample(1)[0].data.geometry, "sphere");
  const remove = r.motion.prepareSceneTransition([a], [{kind: 1, objectId: "edge"}]);
  assert.equal(remove.sample(.5)[0].kind, 0); assert.equal(remove.sample(1)[0].kind, 1);
  const partial = r.motion.prepareSceneTransition([a], []); assert.deepEqual(plain(partial.sample(1)), plain(partial.sample(0)));
});

test("unmatched and discrete payloads receive the same bounded validation as interpolated data", () => {
  const r = runtime(); r.load(); let deep = 1; for (let i = 0; i < 34; i++) deep = [deep];
  for (const data of [deep, {z: Infinity}, JSON.parse('{"constructor":{"polluted":true}}'), {text: "x".repeat(4*1024*1024)}]) {
    assert.throws(() => r.motion.prepareSceneTransition([], [{kind: 5, data}]));
  }
  assert.throws(() => r.motion.prepareSceneTransition([], Array(10001).fill({kind: 5,data: {z: 1}})), /10000/);
  assert.throws(() => r.motion.prepareSceneTransition([], [{kind: 2,data: {x: 1}}]), /invalid/);
});

test("deferred mounts coalesce to the latest pose, wait for application and retry replaced handles", async () => {
  const r = runtime(); r.load(); const ready = deferred(), first = deferred(), el = mount(), calls = [];
  r.window.__gosx.scene3d = {whenReady: () => ready.promise, dispatchCommands(_mount, commands) {calls.push(commands); return calls.length === 1 ? first.promise : Promise.resolve();}};
  const binding = r.motion.bindSceneTransition(el, r.motion.prepareSceneTransition(golden.from, golden.to));
  binding.sample(0); binding.sample(.25); binding.sample(.75);
  assert.equal(calls.length, 0); ready.resolve(true); await r.flush();
  assert.equal(calls.length, 1); assert.equal(calls[0][0].data.x, 5);
  binding.sample(1); binding.sample(.5); let settled = false; const waiting = binding.settled().then(() => {settled = true;});
  await r.flush(); assert.equal(settled, false); assert.equal(calls.length, 1);
  first.resolve(); await waiting; assert.equal(calls.length, 2); assert.equal(calls[1][0].data.x, 4);
  binding.sample(.5); await binding.settled(); assert.equal(calls.length, 2, "unchanged sample does not resubmit commands");
  el.__gosxScene3DHandle = {}; r.observers.at(-1).callback(); await binding.settled(); assert.equal(calls.length, 3, "replacement receives the current absolute pose");
  r.reduce(true); await binding.settled(); assert.equal(calls.at(-1)[0].data.x, 6);
  r.reduce(false); await binding.settled(); assert.equal(calls.at(-1)[0].data.x, 4);
  binding.dispose(); await assert.rejects(binding.settled(), /disposed/); assert.equal(r.observers.at(-1).disconnected, true);
});

test("binding aborts pending work and propagates application errors without orphaned writes", async () => {
  const r = runtime(); r.load(); const el = mount(), plan = r.motion.prepareSceneTransition(golden.from, golden.to); let signal;
  r.window.__gosx.scene3d = {whenReady(_mount, options) {signal = options.signal; return new Promise((_resolve, reject) => signal.addEventListener("abort", () => reject(Error("aborted"))));}};
  const binding = r.motion.bindSceneTransition(el, plan); binding.sample(.5); binding.dispose();
  await assert.rejects(binding.settled(), /disposed/); assert.equal(signal.aborted, true);
  r.window.__gosx.scene3d = {whenReady: async () => true, dispatchCommands: async () => {throw Error("asset failed");}};
  const broken = r.motion.bindSceneTransition(el, plan); broken.sample(.5); await assert.rejects(broken.settled(), /asset failed/); broken.dispose();
});

test("Go scene bindings keep CSS-compiled signals, join transport capture and tolerate either chunk order", async () => {
  for (const order of [[scene,transport], [transport,scene]]) {
    const r = runtime(), el = mount(), applied = [], pending = deferred(); let blocked = true;
    r.window.__gosx.scene3d = {whenReady: async () => true, dispatchCommands: async (_mount, commands) => {if (blocked) await pending.promise; applied.push(commands);}};
    const spec = {version: 1,id: "story",transport: {durationMs: 1000},signals: [{id: "clock",kind: "playhead"}],cssCompiled: [0],bindings: [{signal: "clock",target: "style",selector: "#none",property: "opacity"}, {signal: "clock",target: "sceneTransition",property: "pose",selector: "#form",transition: {version: 1,from: golden.from,to: golden.to}}]};
    const root = {nodeType: 1,hasAttribute: name => name === "data-gosx-motion-program",getAttribute: () => JSON.stringify(spec),querySelectorAll: selector => selector === "#form" ? [el] : [],querySelector: selector => selector === "#form" ? el : null};
    r.motion.mountPrograms(root); for (const code of order) r.load(code);
    const t = r.motion.transport("story"); t.seek(500);
    let finished = false; const waiting = t.settled().then(() => {finished = true;});
    await r.flush(); assert.equal(finished, false, "capture must await its pose producer");
    blocked = false; pending.resolve(); await r.flush(); await waiting;
    assert.equal(applied.at(-1)[0].data.x, 4);
    t.seek(0); r.motion.disposePrograms(root); await r.flush(); assert.equal(r.motion.transport("story"), null);
    assert.equal(applied.at(-1)[0].data.x, 4, "disposed queued writes cannot apply");
  }
});


test("replacement during a failed submission reapplies the latest pose to the new handle", async () => {
  const r = runtime(); r.load(); const el = mount(), first = deferred(), calls = [];
  r.window.__gosx.scene3d = {whenReady: async () => true, dispatchCommands: async (_mount, commands) => {calls.push(commands); if (calls.length === 1) await first.promise;}};
  const binding = r.motion.bindSceneTransition(el, r.motion.prepareSceneTransition(golden.from, golden.to));
  binding.sample(.25); await r.flush();
  el.__gosxScene3DHandle = {}; r.observers.at(-1).callback(); binding.sample(.75);
  first.reject(Error("old mount disposed")); await binding.settled();
  assert.equal(calls.length, 2); assert.equal(calls[1][0].data.x, 5); binding.dispose();
});

test("disposal cancels a pre-bridge scheduler wait", async () => {
  const r = runtime(); r.load();
  const binding = r.motion.bindSceneTransition(mount(), r.motion.prepareSceneTransition(golden.from, golden.to));
  binding.sample(.5); assert.ok(r.frames.size); binding.dispose();
  await assert.rejects(binding.settled(), /disposed/); assert.equal(r.frames.size, 0);
});


test("a removed mount reports a bounded failure instead of retrying a disposed owner forever", async () => {
  const r = runtime(); r.load(); const el = mount(); let removed = false, attempts = 0;
  r.window.__gosx.scene3d = {whenReady: async () => {attempts++; if (removed) throw Error("mount removed"); return true;},dispatchCommands: async () => {}};
  const binding = r.motion.bindSceneTransition(el, r.motion.prepareSceneTransition(golden.from, golden.to));
  binding.sample(.5); await binding.settled();
  removed = true; el.__gosxScene3DHandle = null; r.observers.at(-1).callback();
  await assert.rejects(binding.settled(), /mount removed/);
  assert.equal(attempts, 2); assert.equal(r.frames.size, 0); binding.dispose();
});
