"use strict";

const test = require("node:test");
const assert = require("node:assert/strict");
const {
  bootstrapSource, bootstrapRuntimeSource, bootstrapFeatureEnginesSource,
  FakeElement, createContext, installManualRAF, runScript, flushAsyncWork,
  sharedSignalValue,
} = require("./runtime-test-harness.js");

function pad(index = 0, pressed = true) {
  return {
    index, connected: true, axes: [0.7, -0.4, 0.2, -0.8],
    buttons: Array.from({ length: 16 }, (_, i) => ({ pressed: pressed && (i === 3 || i === 12) })),
  };
}

async function mountInput(split, options = {}) {
  const state = { pads: options.pads || [], scans: 0, fail: false };
  const mounts = Array.from({ length: options.engines || 1 }, (_, i) => {
    const mount = new FakeElement("div", null);
    mount.id = "gamepad-input-" + i;
    return mount;
  });
  const env = createContext({
    elements: mounts,
    getGamepads() {
      state.scans++;
      if (state.fail) throw new Error("gamepad access denied");
      return state.pads;
    },
    engineFactories: { GamepadInput() { return { dispose() {} }; } },
    fetchRoutes: {
      "/gosx/bootstrap-feature-engines.js": { text: bootstrapFeatureEnginesSource },
      "/runtime.wasm": { bytes: [0, 97, 115, 109] },
    },
    // Mirror the shared runtime's signal publication at its existing ABI seam.
    // Selective native-engine pages use the regular shared-signal fallback.
    onSetInputBatch(payload) {
      for (const [name, value] of Object.entries(JSON.parse(payload))) {
        env.context.__gosx_notify_shared_signal(name, JSON.stringify(value));
      }
    },
    onSetSharedSignal(name, payload) { env.context.__gosx_notify_shared_signal(name, payload); },
    manifest: { runtime: { path: "/runtime.wasm" }, engines: mounts.map((mount, i) => ({
      id: "gosx-gamepad-input-" + i, component: "GamepadInput", kind: "surface",
      mountId: mount.id, capabilities: ["keyboard", "gamepad"],
    })) },
  });
  env.context.console.info = env.context.console.log;
  if (options.hidden) env.document.hidden = true;
  if (options.api === "missing") delete env.context.navigator.getGamepads;
  if (options.api === "throwing") state.fail = true;
  if (options.api === "inaccessible") {
    Object.defineProperty(env.context.navigator, "getGamepads", {
      configurable: true, get() { throw new Error("gamepad getter denied"); },
    });
  }
  const raf = installManualRAF(env.context);
  runScript(split ? bootstrapRuntimeSource : bootstrapSource, env.context, split ? "bootstrap-runtime.js" : "bootstrap.js");
  await flushAsyncWork();
  assert.equal(env.context.__gosx.engines.size, mounts.length);
  let time = 0;
  return {
    env, state, raf,
    step() { raf.flush(time += 16); },
    drain() {
      for (let i = 0; i < 6 && raf.count(); i++) raf.flush(time += 16);
      assert.equal(raf.count(), 0, "polling and one-shot input delivery settle");
    },
    signal(name) { return sharedSignalValue(env, "$input." + name); },
    event(name) { env.context.dispatchEvent({ type: name }); },
    hidden(hidden) { env.document.hidden = hidden; env.document.dispatchEvent({ type: "visibilitychange" }); },
    dispose(i = 0) { env.context.__gosx_dispose_engine("gosx-gamepad-input-" + i); },
  };
}

function assertCleared(fixture, slot = 0) {
  const prefix = "gamepad" + slot + ".";
  assert.equal(fixture.signal(prefix + "connected"), false);
  for (const key of ["leftX", "leftY", "rightX", "rightY"]) assert.equal(fixture.signal(prefix + key), 0, key);
  for (const key of ["dpadUp", "dpadDown", "dpadLeft", "dpadRight", "buttonA", "buttonB", "buttonX", "buttonY", "buttonLB", "buttonRB"]) {
    assert.equal(fixture.signal(prefix + key), false, key);
  }
}

function assertInitialNeutral(fixture) {
  fixture.drain();
  assert.equal(fixture.signal("gamepad.count"), 0);
  assert.equal(fixture.signal("gamepad0.connected"), false);
  assert.equal(fixture.signal("gamepad1.connected"), false);
}

for (const split of [false, true]) {
  const mode = split ? "selective" : "monolithic";

  test(mode + " gamepad startup scans once and sleeps without a controller", async () => {
    const f = await mountInput(split);
    assert.equal(f.state.scans, 1);
    assertInitialNeutral(f);
    assert.equal(f.raf.count(), 0);
    for (let i = 0; i < 8; i++) f.step();
    assert.equal(f.state.scans, 1, "idle engine does not read navigator every frame");
    f.dispose();
    assert.equal(f.raf.count(), 0);
    assert.deepEqual(f.env.consoleLogs.error, []);
  });

  test(mode + " gamepad publishes initially held input in the existing two slots", async () => {
    const f = await mountInput(split, { pads: [pad(), pad(1), pad(2)] });
    assert.equal(f.state.scans, 1, "already-connected controllers need no connection event");
    f.step();
    assert.equal(f.signal("gamepad.count"), 2);
    for (const slot of [0, 1]) {
      assert.equal(f.signal("gamepad" + slot + ".buttonY"), true);
      assert.equal(f.signal("gamepad" + slot + ".dpadUp"), true);
      assert.equal(f.signal("gamepad" + slot + ".leftX"), 0.7);
    }
    assert.equal(f.signal("gamepad2.connected"), undefined, "do not broaden published controller slots");
    f.dispose();
    f.drain();
    assertCleared(f, 0); assertCleared(f, 1);
    assert.equal(f.signal("gamepad.count"), 0);
  });

  test(mode + " gamepad connection coalesces polls and disconnect clears held input", async () => {
    const f = await mountInput(split);
    assertInitialNeutral(f);
    f.state.pads = [pad()];
    for (let i = 0; i < 5; i++) f.event("gamepadconnected");
    assert.equal(f.raf.count(), 1);
    assert.equal(f.state.scans, 1);
    f.step(); f.step();
    assert.equal(f.state.scans, 3, "one poll per connected frame despite duplicate wakeups");
    assert.equal(f.signal("gamepad0.buttonY"), true, "first held press is not primed away");
    f.step();
    assert.equal(f.signal("gamepad0.buttonY"), true, "held button remains delivered");
    f.state.pads = [pad(0, false)]; f.step(); f.step();
    assert.equal(f.signal("gamepad0.buttonY"), false, "button release is delivered");
    f.state.pads = [pad()]; f.step(); f.step();
    // Disconnection must preserve another provider's pending batch while
    // clearing every gamepad channel that could otherwise remain stuck.
    f.env.document.dispatchEvent({ type: "keydown", key: "W" });
    f.state.pads = []; f.event("gamepaddisconnected"); f.drain();
    assertCleared(f);
    assert.equal(f.signal("gamepad.count"), 0);
    assert.equal(f.signal("key.w"), true);
    const scans = f.state.scans;
    f.step(); f.step(); assert.equal(f.state.scans, scans);
  });

  test(mode + " hidden gamepad provider sleeps, clears input and rescans on return", async () => {
    const f = await mountInput(split, { pads: [pad()], hidden: true });
    assert.equal(f.state.scans, 0, "hidden initial mount does not poll");
    assertInitialNeutral(f);
    f.event("gamepadconnected"); assert.equal(f.raf.count(), 0);
    f.hidden(false); f.step(); f.step();
    assert.equal(f.signal("gamepad0.buttonY"), true);
    const scans = f.state.scans;
    f.hidden(true); f.drain();
    assert.equal(f.state.scans, scans, "only the existing input-clear flush may run hidden");
    assertCleared(f); assert.equal(f.signal("gamepad.count"), 0);
    f.event("gamepadconnected"); f.event("gamepaddisconnected"); f.step();
    assert.equal(f.state.scans, scans);
    f.hidden(false); f.hidden(false); assert.equal(f.raf.count(), 1);
    f.step(); f.step(); assert.equal(f.signal("gamepad0.buttonY"), true);
    f.dispose(); f.drain();
  });

  test(mode + " shared gamepad provider retains one poll until final release and remounts", async () => {
    const f = await mountInput(split, { pads: [pad()], engines: 2 });
    const provider = f.env.context.__gosx.input.providers.gamepad;
    assert.equal(provider.refCount, 2);
    assert.equal(f.state.scans, 1, "second engine shares the first provider scan");
    assert.equal(f.env.windowListeners.get("gamepadconnected").length, 1);
    f.step(); f.dispose(0);
    assert.equal(provider.refCount, 1);
    const scans = f.state.scans; f.step();
    assert.equal(f.state.scans, scans + 1);
    assert.equal(f.signal("gamepad0.buttonY"), true);
    f.dispose(1); f.drain(); assertCleared(f);
    assert.equal(f.env.context.__gosx.input.providers.gamepad, undefined);
    assert.equal(f.env.windowListeners.get("gamepadconnected").length, 0);
    assert.equal(f.env.windowListeners.get("gamepaddisconnected").length, 0);
    const stopped = f.state.scans;
    f.event("gamepadconnected"); f.hidden(true); f.hidden(false); f.step();
    assert.equal(f.state.scans, stopped, "released provider cannot restart from stale listeners");
    f.env.context.__gosx_mount_late_engine_factory("GamepadInput");
    await flushAsyncWork(); f.step();
    assert.equal(f.env.context.__gosx.input.providers.gamepad.refCount, 2);
    assert.equal(f.signal("gamepad0.buttonY"), true, "remount publishes an already held press");
    f.dispose(0); f.dispose(1); f.drain();
    assert.deepEqual(f.env.consoleLogs.error, []);
  });

  for (const api of ["missing", "throwing", "inaccessible"]) {
    test(mode + " unavailable gamepad API sleeps without an engine error: " + api, async () => {
      const f = await mountInput(split, { api });
      assertInitialNeutral(f);
      assert.equal(f.raf.count(), 0);
      const scans = f.state.scans; f.step(); f.step();
      assert.equal(f.state.scans, scans);
      assert.deepEqual(f.env.consoleLogs.error, []);
    });
  }

  test(mode + " lost gamepad permission clears input and can recover on a lifecycle wake", async () => {
    const f = await mountInput(split, { pads: [pad()] });
    f.step(); assert.equal(f.signal("gamepad0.buttonY"), true);
    f.state.fail = true; f.drain(); assertCleared(f);
    assert.equal(f.signal("gamepad.count"), 0);
    const scans = f.state.scans; f.step(); assert.equal(f.state.scans, scans);
    f.state.fail = false; f.event("gamepadconnected"); f.step(); f.step();
    assert.equal(f.signal("gamepad0.buttonY"), true);
    assert.deepEqual(f.env.consoleLogs.error, []);
    f.dispose(); f.drain();
  });
}
