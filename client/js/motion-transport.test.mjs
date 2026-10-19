import assert from "node:assert/strict";
import fs from "node:fs";
import test from "node:test";
import vm from "node:vm";

const source = ["./bootstrap-src/06-motion-core.ts", "../runtime/motion/transport.ts"].map(file => fs.readFileSync(new URL(file, import.meta.url), "utf8")).join("\n");

function runtime(options = {}) {
  const frames = new Map(), events = new Map(), observers = [];
  class Observer { constructor(callback) { this.callback = callback; observers.push(this); } observe() {} disconnect() {} }
  let id = 0, clock = 0, reduce;
  const body = { nodeType: 1, programs: [], querySelectorAll(selector) { return selector === "[data-gosx-motion-program]" ? this.programs : []; } };
  const document = { body, documentElement: body, hidden: false,
    addEventListener(name, fn) { let set = events.get(name); if (!set) events.set(name, set = new Set()); set.add(fn); },
    removeEventListener(name, fn) { events.get(name)?.delete(fn); } };
  const window = { __gosx: {}, console, addEventListener() {},
    matchMedia() { return { matches: false, addEventListener(_name, fn) { reduce = fn; } }; },
    requestAnimationFrame(fn) { frames.set(++id, fn); return id; }, cancelAnimationFrame(id) { frames.delete(id); } };
  vm.runInContext(source, vm.createContext({ window, document, console, performance: { now: () => clock }, setTimeout, clearTimeout, queueMicrotask, MutationObserver: options.observe ? Observer : undefined }));
  return { motion: window.__gosx.motion, document, frames,
    async deliver(changes) { for (const observer of observers.slice()) observer.callback(changes); await Promise.resolve(); },
    advance(ms) { clock = ms; const pending = Array.from(frames.values()); frames.clear(); pending.forEach(fn => fn(ms)); },
    hidden(value) { document.hidden = value; events.get("visibilitychange")?.forEach(fn => fn()); },
    reduce(value) { reduce({matches: value}); },
  };
}

test("shared transport seeks labels, reverses, pauses and leaves no idle RAF", () => {
  const r = runtime(), t = r.motion.createTransport({durationMs: 1000, labels: {middle: 500}});
  const samples = [];
  t.attach({sample: ms => samples.push(ms)});
  assert.equal(r.frames.size, 0);
  t.play(); r.advance(0); r.advance(100);
  assert.equal(t.state().time, 100);
  t.seek("middle"); assert.equal(t.state().time, 500); assert.equal(t.state().paused, true);
  r.advance(10000); assert.equal(t.state().time, 500);
  t.reverse(); r.advance(10100); r.advance(10200); assert.equal(t.state().time, 400);
  r.advance(20000); assert.equal(t.state().time, 150, "background gaps are bounded");
  r.advance(20200); assert.equal(t.state().time, 0); assert.equal(r.frames.size, 0);
  t.seek(300); t.replay(); assert.equal(t.state().time, 0); assert.equal(t.state().paused, true);
  t.play(); r.advance(21000); r.advance(21100); assert.equal(t.state().time, 100);
  r.hidden(true); r.advance(60000); r.hidden(false); r.advance(61000); r.advance(61100);
  assert.equal(t.state().time, 200, "hidden time never catches up");
  t.pause(); assert.equal(r.frames.size, 0);
  assert.throws(() => t.seek("missing"), /position or label/);
  assert.throws(() => t.seek(Infinity), /position/);
  t.seek(5000); assert.equal(t.state().time, 1000);
  t.dispose(); assert.equal(r.frames.size, 0); assert.throws(() => t.play(), /disposed/);
  assert.ok(samples.includes(400));
});

test("reduced motion settles the transport and never allocates a continuous frame", () => {
  const r = runtime(), t = r.motion.createTransport({durationMs: 900});
  t.play(); r.advance(0); r.advance(100); r.reduce(true);
  assert.equal(t.state().time, 900); assert.equal(r.frames.size, 0);
  t.replay(); assert.equal(t.state().time, 900); assert.equal(r.frames.size, 0);
  t.dispose();
});

test("playback samples dynamic duration once per tick and keeps it live outside playback", () => {
  const r = runtime(); let calls = 0, span = 1000;
  const t = r.motion.createTransport({durationMs() { calls++; return span; }});
  t.subscribe(state => assert.ok(state.duration > 0));
  t.play(); const before = calls; r.advance(0);
  assert.equal(calls - before, 1);
  span = 2000; assert.equal(t.duration(), 2000);
  t.dispose();
});

test("settled follows an asynchronous reduced-motion policy change", async () => {
  const r = runtime(), t = r.motion.createTransport({durationMs: 1000});
  t.seek(200);
  const pending = t.settled({timeoutMS: 100});
  r.advance(0); await Promise.resolve(); r.reduce(true);
  for (let i = 1; i < 20; i++) { await Promise.resolve(); r.advance(i * 16); }
  await pending;
  assert.equal(t.state().time, 1000);
  t.dispose();
});

test("Go-authored playhead reuses DOM, camera and uniform bindings and rewinds exactly", () => {
  const r = runtime(), writes = [];
  const title = {style: {opacity: ""}}, scene = {};
  const program = {version: 1, id: "story", transport: {durationMs: 2000}, signals: [
    {id: "clock", kind: "playhead"},
    {id: "gain", kind: "curve", input: "clock", frames: [{at: 0, value: 0}, {at: 2, value: 1}]},
  ], bindings: [
    {signal: "gain", target: "style", selector: ".title", property: "opacity"},
    {signal: "gain", target: "camera", selector: "#scene", property: "position.x"},
    {signal: "gain", target: "materialUniform", selector: "#scene", node: "plane", property: "gain"},
  ]};
  const root = { nodeType: 1, hasAttribute: name => name === "data-gosx-motion-program", getAttribute: () => JSON.stringify(program),
    querySelectorAll(selector) { return selector === ".title" ? [title] : selector === "#scene" ? [scene] : []; } };
  r.motion.attachScene(scene, { write(binding, value) { writes.push([binding.target, value]); }, invalidate() {} });
  r.motion.mountPrograms(root);
  const t = r.motion.transport("story");
  for (const [ms, expected] of [[500, .25], [1500, .75], [500, .25]]) {
    t.seek(ms); r.advance(ms);
    assert.equal(Number(title.style.opacity), expected);
    assert.deepEqual(writes.slice(-2), [["camera", expected], ["materialUniform", expected]]);
  }
  r.motion.disposePrograms(root); assert.equal(r.motion.transport("story"), null);
  assert.throws(() => t.seek(0), /disposed/);
});

test("settled waits for adapters and rejects a concurrent seek or timeout", async () => {
  const r = runtime(), t = r.motion.createTransport({durationMs: 1000});
  let finish;
  const detach = t.attach({sample() {}, settled: () => new Promise(resolve => { finish = resolve; })});
  t.seek(200);
  const pending = t.settled();
  r.advance(0); await Promise.resolve();
  assert.equal(typeof finish, "function"); finish();
  for (let i = 1; i < 8; i++) { await Promise.resolve(); r.advance(i * 16); }
  await pending;
  const changed = t.settled(); r.advance(200); await Promise.resolve(); t.seek(500); finish();
  for (let i = 1; i < 8; i++) { await Promise.resolve(); r.advance(200 + i * 16); }
  await assert.rejects(changed, /changed while settling/);
  detach();
  await assert.rejects(t.settled({timeoutMS: 1}), /did not settle/);
  t.dispose();
});

test("scene capture waits for producers then submits a fresh frame even at the same clock", async () => {
  const r = runtime(), t = r.motion.createTransport({durationMs: 1000}), mount = {};
  let finish, produced = false, submissions = 0, clock = {timeSeconds: .2, paused: true, pending: false};
  const producer = t.attach({sample() {}, settled: () => new Promise(resolve => { finish = resolve; })});
  const unbind = r.motion.bindTransportScene(t, mount);
  t.seek(200);
  const pending = t.settled({timeoutMS: 1000});
  r.advance(0); await Promise.resolve();
  assert.equal(typeof finish, "function");
  mount.__gosxScene3DHandle = {
    getAnimationClock: () => clock,
    setAnimationClock(next) {
      assert.equal(produced, true, "a renderer receipt must follow producer acceptance");
      submissions++; clock = {...next, paused: true, pending: true};
      r.motion.scheduler.request(() => { clock.pending = false; });
    },
  };
  produced = true; finish();
  for (let i = 1; i < 24; i++) { await Promise.resolve(); r.advance(i * 16); }
  await pending; assert.equal(submissions, 1);
  producer();
  const samePose = t.settled({timeoutMS: 1000});
  for (let i = 24; i < 48; i++) { await Promise.resolve(); r.advance(i * 16); }
  await samePose; assert.equal(submissions, 2, "same-time material writes need a new submitted frame");
  unbind(); t.dispose();
});

test("late scopes attach, replace their transport with the signal graph, and dispose on removal", async () => {
  const r = runtime({observe: true}), title = {style: {opacity: ""}};
  let spec = {version: 1, id: "late", transport: {durationMs: 1000}, signals: [{id: "clock", kind: "playhead"}],
    bindings: [{signal: "clock", target: "style", selector: ".title", property: "opacity"}]};
  const root = {nodeType: 1, isConnected: true, hasAttribute: name => name === "data-gosx-motion-program",
    getAttribute: () => JSON.stringify(spec), querySelectorAll: selector => selector === ".title" ? [title] : []};
  r.document.body.programs.push(root);
  await r.deliver([{type: "childList", addedNodes: [root], removedNodes: []}]);
  const first = r.motion.transport("late");
  assert.ok(first, "the optional module must hydrate the first late scope");
  first.seek(250); r.advance(0); assert.equal(Number(title.style.opacity), .25);
  spec = {...spec, transport: {durationMs: 2000}};
  await r.deliver([{type: "attributes", target: root, attributeName: "data-gosx-motion-program", addedNodes: [], removedNodes: []}]);
  const replacement = r.motion.transport("late");
  assert.notEqual(replacement, first); assert.equal(replacement.duration(), 2000);
  assert.throws(() => first.play(), /disposed/);
  replacement.seek(500); r.advance(16); assert.equal(Number(title.style.opacity), .5);
  root.isConnected = false; r.document.body.programs.length = 0;
  await r.deliver([{type: "childList", addedNodes: [], removedNodes: [root]}]);
  assert.equal(r.motion.transport("late"), null); assert.throws(() => replacement.play(), /disposed/);
});


test("queued scene writes follow the current attachment and ignore detached adapters", () => {
  const r = runtime(), mount = {}, writes = [];
  const spec = {version: 1,id: "binding",signals: [{id: "clock",kind: "time"}],bindings: [{signal: "clock",target: "camera",selector: "#scene",property: "position.x"}]};
  const root = {nodeType: 1,hasAttribute: () => true,getAttribute: () => JSON.stringify(spec),querySelectorAll: selector => selector === "#scene" ? [mount] : []};
  r.motion.mountPrograms(root);
  const detach = r.motion.attachScene(mount, {write() {writes.push("old");}});
  detach();
  r.motion.attachScene(mount, {write() {writes.push("new");}});
  r.advance(16); assert.deepEqual(writes, ["new"]);
  r.motion.disposePrograms(root);
});
