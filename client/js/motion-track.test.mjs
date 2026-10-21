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

test("mix selects exact endpoints without cancellation, overflow or vector aliasing", () => {
  const r = runtime();
  for (const [from, to] of [[1e20, 1], [Number.MAX_VALUE, -Number.MAX_VALUE], [0, -0]]) {
    const a = r.motion.signal(from), b = r.motion.signal(to), weight = r.motion.signal(0);
    const mixed = r.motion.mix(a, b, weight);
    assert.ok(Object.is(mixed.get(), from));
    weight.set(1); assert.ok(Object.is(mixed.get(), to), `${from} -> ${to}`);
    weight.set(0); assert.ok(Object.is(mixed.get(), from));
    mixed.dispose();
  }
  const from = [1e20, -0], to = [1, 2], weight = r.motion.signal(0);
  const mixed = r.motion.mix(r.motion.signal(from), r.motion.signal(to), weight);
  assert.deepEqual(Array.from(mixed.get()), from);
  mixed.get()[0] = 42; assert.equal(from[0], 1e20);
  weight.set(1); assert.deepEqual(Array.from(mixed.get()), to);
  mixed.get()[0] = 42; assert.equal(to[0], 1);
  weight.set(.5); assert.equal(mixed.get()[1], 1);
  mixed.dispose();
  assert.equal(r.frames.size, 0);
});

test("prepared scalar tracks agree with native Eval for easing, holds, step boundaries, stagger and reverse seeks", () => {
  const r = runtime(); r.load();
  for (const fixture of fixtures) {
    const sample = r.motion.createTrackSampler(fixture.signal.frames, 0, fixture.signal.interp);
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

const valueFixtures = JSON.parse(read("../../motion/testdata/track_value_golden.json"));

const timelineFixtures = JSON.parse(read("../../scene/testdata/timeline_motion.json"));
test("Go scene plans mounted as motion programs agree with native poses through seeks, cuts and reduced motion", () => {
  const property = name => /^[xyz]$/.test(name) ? `position.${name}` : name.replace(/^(rotation|scale)([XYZ])$/, (_, prefix, axis) => `${prefix}.${axis.toLowerCase()}`);
  for (const deferred of [false, true]) for (const fixture of timelineFixtures) {
    const r = runtime(), scene = {}, latest = new Map();
    r.load(transport); if (!deferred) r.load();
    const root = {nodeType: 1, hasAttribute: name => name === "data-gosx-motion-program", getAttribute: () => JSON.stringify(fixture.program),
      querySelectorAll(selector) {return selector === "#scene" ? [scene] : [];}};
    r.motion.mountPrograms(root);
    if (deferred) r.load();
    r.motion.attachScene(scene, {write(binding, value) {latest.set(`${binding.target}:${binding.node || ""}:${binding.property}`, value);}, invalidate() {}});
    r.advance();
    const playback = r.motion.transport(fixture.program.id);
    const check = (commands, at) => {
      for (const command of commands) for (const [name, want] of Object.entries(command.data)) {
        const key = `${command.kind === 5 ? "camera" : "sceneNode"}:${command.objectId || ""}:${property(name)}`, got = latest.get(key);
        assert.ok(Number.isFinite(got) && Math.abs(got - want) <= 1e-9 * Math.max(1, Math.abs(want)), `${fixture.name} at ${at} ${key}: ${got} vs ${want}`);
      }
    };
    for (const sample of fixture.samples) {
      playback.seek(sample.seconds * 1000); r.advance(); check(sample.commands, sample.seconds);
    }
    r.reduce(true); r.advance(); check(fixture.final);
    r.reduce(false); playback.seek(0); r.advance(); check(fixture.samples[0].commands);
    r.motion.disposePrograms(root); r.advance();
    const settled = new Map(latest);
    r.reduce(true); r.advance(); assert.deepEqual(latest, settled);
    assert.equal(r.frames.size, 0, `${fixture.name}: disposed scope retains no RAF`);
  }
});

test("typed vector, HDR color and shortest-arc quaternion tracks agree with native Eval", () => {
  const r = runtime(); r.load();
  for (const fixture of valueFixtures) {
    const sample = r.motion.createTrackSampler(fixture.signal.frames, fixture.signal.arity, fixture.signal.interp);
    const scratch = new Array(fixture.samples[0].value.length);
    for (const {time, value} of fixture.samples) {
      const got = sample(time, scratch);
      assert.equal(got, scratch, "prepared sampling reuses the caller's output buffer");
      for (let i = 0; i < value.length; i++) assert.ok(Math.abs(got[i] - value[i]) < 1e-7, `arity ${fixture.signal.arity} at ${time}, component ${i}: ${got[i]} vs ${value[i]}`);
    }
    const endpoint = sample(-1); endpoint[0] = 99;
    assert.notEqual(sample(-1)[0], 99, "samples cannot mutate the prepared key snapshots");
  }
  assert.equal(r.frames.size, 0);
});

test("typed samplers reject invalid shapes, values and quaternions", () => {
  const r = runtime(); r.load();
  const keys = value => [{at:0,value},{at:1,value}];
  for (const [frames,arity] of [[keys([1,2]),2],[keys([1,2,NaN]),2],[keys([0,0,0,0]),4],
    [keys([0,0,0,2]),4],[keys(new Array(3)),2],[keys([1,2]),6],[keys([1,2]),1.5]]) {
    assert.throws(() => r.motion.createTrackSampler(frames,arity));
  }
  const frames = keys([1,2,3]), sample = r.motion.createTrackSampler(frames,2);
  frames[0].value[0] = 99;
  assert.equal(sample(.5)[0],1);
  assert.throws(() => sample(.5,[]), /width/);
});

test("spline samplers own tangents, accept quaternion derivatives and reject malformed tangent wire data", () => {
  const r = runtime(); r.load();
  const frames = [{at:0,value:[0,0],outTangent:[1,2]}, {at:2,value:[2,0],inTangent:[1,-2]}];
  const sample = r.motion.createTrackSampler(frames,1,2), out = [0,0];
  assert.equal(sample(1,out),out);
  assert.deepEqual(out,[1,1]);
  frames[0].outTangent[1] = 99; frames[1].inTangent[1] = 99;
  assert.deepEqual(Array.from(sample(1)),[1,1]);
  assert.throws(() => sample(1,[]), /width/);
  for (const tangent of [[1], [1,NaN], [1,Infinity], new Array(2), 2]) {
    assert.throws(() => r.motion.createTrackSampler([{at:0,value:[0,0],outTangent:tangent},{at:1,value:[1,1]}],1,2));
  }
  for (const interp of [-1,4,1.5,"2",NaN,3]) assert.throws(() => r.motion.createTrackSampler(frames,1,interp));
  assert.throws(() => r.motion.createTrackSampler(frames,1), /tangents/);
  const q = r.motion.createTrackSampler([{at:0,value:[1,0,0,0],outTangent:[0,0,0,0]},
    {at:2,value:[-1,0,0,0],inTangent:[0,0,0,0]}],4,2);
  assert.deepEqual(Array.from(q(1)),[0,0,0,1]);
});

test("typed tracks preserve their rest pose across seeks, reduced motion, load order and disposal", () => {
  for (const late of [false,true]) {
    const r = runtime(), title = {style:{color:""}}, scene = {}, writes = [];
    r.load(transport); if (!late) r.load();
    const program = {version:1,id:"typed",transport:{durationMs:2000},signals:[
      {id:"clock",kind:"playhead"},
      {id:"position",kind:"track",input:"clock",arity:2,interp:2,frames:[{at:0,value:[0,1,2],outTangent:[1,1,3]},{at:2,value:[2,3,4],inTangent:[1,1,-1]}]},
      {id:"other",kind:"track",input:"clock",arity:2,frames:[{at:0,value:[2,3,4]},{at:2,value:[4,5,6]}]},
      {id:"weight",kind:"map",input:"clock",from:0,to:2,min:0,max:1},
      {id:"mixed",kind:"mix",a:"position",b:"other",weight:"weight"},
      {id:"tint",kind:"track",input:"clock",arity:5,frames:[{at:0,value:[0,0,0,1]},{at:2,value:[1,.5,0,.5]}]},
    ],bindings:[
      {signal:"mixed",target:"camera",selector:"#scene",property:"position"},
      {signal:"tint",target:"materialUniform",selector:"#scene",node:"orb",property:"glow"},
      {signal:"tint",target:"style",selector:".title",property:"color"},
    ]};
    const root = {nodeType:1,hasAttribute:name=>name==="data-gosx-motion-program",getAttribute:()=>JSON.stringify(program),
      querySelectorAll:selector=>selector===".title"?[title]:selector==="#scene"?[scene]:[]};
    r.motion.attachScene(scene,{write(binding,value){writes.push([binding.target,Array.from(value || [])]);},invalidate(){}});
    r.motion.mountPrograms(root);
    const t = r.motion.transport("typed"); t.seek(1000); if(late)r.load(); r.advance();
    assert.deepEqual(writes.slice(-2),[["camera",[2,3,4.5]],["materialUniform",[.5,.25,0,.75]]]);
    assert.equal(title.style.color,"rgba(128,64,0,0.75)");
    t.seek(500);r.advance();r.reduce(true);r.advance();
    assert.deepEqual(writes.slice(-2),[["camera",[4,5,6]],["materialUniform",[1,.5,0,.5]]]);
    r.reduce(false);t.seek(1000);r.advance();
    assert.equal(title.style.color,"rgba(128,64,0,0.75)");
    r.motion.disposePrograms(root);r.advance();const count=writes.length;r.reduce(true);r.advance();
    assert.equal(writes.length,count);assert.equal(r.frames.size,0);
  }
});
