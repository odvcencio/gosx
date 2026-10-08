const assert = require("node:assert/strict");
const fs = require("node:fs");
const vm = require("node:vm");
const source = fs.readFileSync(process.argv[2], "utf8");

async function visit(shared, early, bad) {
  const calls = [];
  const frames = new Map();
  let next = 1;
  const elements = [];
  const draw = Object.fromEntries(["fillRect", "beginPath", "moveTo", "lineTo", "rect", "stroke"].map(name => [name, (...args) => calls.push([name, ...args])]));
  const document = {
    createElement(tag) {
      const events = new Map();
      const element = {
        tag, events,
        setAttribute() {},
        getContext() { return draw; },
        addEventListener(name, fn) { events.set(name, fn); },
        removeEventListener(name, fn) { assert.equal(events.get(name), fn); events.delete(name); },
      };
      elements.push(element);
      return element;
    },
  };
  const window = {
    requestAnimationFrame(fn) { const id = next++; frames.set(id, fn); return id; },
    cancelAnimationFrame(id) { frames.delete(id); },
  };
  let factory;
  if (!early) window.__gosx_register_engine_factory = (name, fn) => { assert.equal(name, "BudgetEngine"); factory = fn; };
  vm.runInNewContext(source, { window, document });
  factory ||= window.__gosx_engine_factories.BudgetEngine;
  const children = [];
  const events = [];
  const ctx = {
    runtimeMode: shared ? "shared" : "",
    mount: { replaceChildren(...nodes) { children.splice(0, children.length, ...nodes); } },
    emit(name, data) { events.push([name, data.usable]); },
    runtime: {
      available() { return bad !== "runtime"; },
      async hydrateFromProgramRef() { calls.push(["hydrate"]); },
      tick() { calls.push(["tick"]); },
      renderFrame() { calls.push(["render"]); return bad === "frame" ? null : { vertexCount: 2, positions: [0, 0, 0, 1, 1, 0] }; },
      dispose() { calls.push(["dispose"]); },
    },
  };
  if (bad === "runtime") {
    await assert.rejects(factory(ctx), /shared runtime unavailable/);
    assert.equal(children.length, 0);
    assert.equal(events.length, 0);
    assert.equal(frames.size, 0);
    return;
  }
  const handle = await factory(ctx);
  assert.equal(children.length, 2);
  assert.equal(events.length, 0, "mounting alone is not a usable first frame");
  const runFrame = time => {
    const [id, callback] = frames.entries().next().value;
    frames.delete(id);
    callback(time);
    return callback;
  };
  if (bad === "frame") {
    assert.throws(() => runFrame(100), /shared frame unavailable/);
    assert.equal(events.length, 0, "missing shared geometry cannot report first tick");
    handle.dispose();
    assert.equal(children.length, 0);
    assert.equal(frames.size, 0);
    return;
  }
  runFrame(100);
  const stale = runFrame(116);
  assert.deepEqual(events, [["first-tick", true]]);
  assert.ok(calls.some(call => call[0] === "stroke"));
  assert.equal(calls.some(call => call[0] === "hydrate"), shared);
  assert.equal(calls.some(call => call[0] === "render"), shared);
  const button = elements[1];
  const click = button.events.get("click");
  click(); click();
  assert.equal(button.textContent, "Count: 2");
  handle.dispose(); handle.dispose();
  assert.equal(frames.size, 0);
  assert.equal(children.length, 0);
  assert.equal(button.events.size, 0);
  assert.equal(calls.filter(call => call[0] === "dispose").length, 1);
  const before = calls.length;
  stale(200); click();
  assert.equal(calls.length, before);
  assert.equal(button.textContent, "Count: 2");
}

(async () => {
  for (const shared of [false, true]) for (const early of [false, true]) await visit(shared, early);
  for (const bad of ["runtime", "frame"]) await visit(true, false, bad);
})().catch(error => { console.error(error); process.exitCode = 1; });
