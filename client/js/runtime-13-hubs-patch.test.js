"use strict";
// Hub connections, hub input encoding for the fight and arcade lobbies, and
// the DOM patch applier.
//
// Split out of the former client/js/runtime.test.js, which this set replaces
// and which no longer exists. Every shared fake, sandbox builder and fixture
// factory lives in ./runtime-test-harness.js.

const test = require("node:test");
const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");

const {
  bootstrapSource,
  patchSource,
  TEXT_NODE,
  FakeElement,
  createContext,
  runScript,
  flushAsyncWork,
  installManualRAF,
} = require("./runtime-test-harness.js");

test("bootstrap does not load engine JS via jsRef (eval escape-hatch removed)", async () => {
  const mount = new FakeElement("div", null);
  mount.id = "escape-root";

  const env = createContext({
    elements: [mount],
    manifest: {
      engines: [
        {
          id: "gosx-engine-1",
          component: "SpecialCanvas",
          kind: "surface",
          mountId: "escape-root",
          props: { mode: "escape" },
        },
      ],
    },
  });

  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();

  assert.equal(env.context.__gosx.ready, true);
  // Engine does not mount because no factory is registered for "SpecialCanvas".
  assert.equal(env.context.__gosx.engines.size, 0);
  assert.deepEqual(env.engineMounts, []);
});

test("bootstrap connects hubs and forwards events into shared signals", async () => {
  function makeSocket(url) {
    return {
      url,
      closeCalled: false,
      onmessage: null,
      onclose: null,
      onerror: null,
      close() {
        this.closeCalled = true;
      },
    };
  }

  const env = createContext({
    createWebSocket: makeSocket,
    fetchRoutes: {
      "/runtime.wasm": { bytes: [0, 97, 115, 109] },
    },
    manifest: {
      runtime: { path: "/runtime.wasm" },
      hubs: [
        {
          id: "gosx-hub-0",
          name: "presence",
          path: "/gosx/hub/presence",
          bindings: [
            { event: "snapshot", signal: "$presence" },
          ],
        },
      ],
    },
  });

  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();

  assert.equal(env.context.__gosx.ready, true);
  assert.equal(env.context.__gosx.hubs.size, 1);
  assert.equal(env.sockets.length, 1);
  assert.equal(env.sockets[0].url, "ws://localhost:3000/gosx/hub/presence");

  env.sockets[0].onmessage({
    data: JSON.stringify({ event: "snapshot", data: { count: 2 } }),
  });

  assert.deepEqual(env.sharedSignalCalls, [
    ["$presence", '{"count":2}'],
  ]);
  assert.equal(env.sockets[0].binaryType, "arraybuffer");

  env.sockets[0].onmessage({
    data: {
      text: async () => JSON.stringify({ event: "snapshot", data: { count: 3 } }),
    },
  });
  await flushAsyncWork();

  env.sockets[0].onmessage({
    data: new Uint8Array([1, 2, 3]).buffer,
  });
  await flushAsyncWork();

  assert.deepEqual(env.sharedSignalCalls, [
    ["$presence", '{"count":2}'],
    ["$presence", '{"count":3}'],
  ]);

  env.context.__gosx_disconnect_hub("gosx-hub-0");
  assert.equal(env.context.__gosx.hubs.size, 0);
  assert.equal(env.sockets[0].closeCalled, true);
  assert.equal(env.consoleLogs.error.length, 0);
});

test("bootstrap hub input sends fighting game snapshots", async () => {
  const sent = [];
  function makeSocket(url) {
    return {
      url,
      readyState: 1,
      closeCalled: false,
      send(raw) {
        sent.push(JSON.parse(raw));
      },
      close() {
        this.closeCalled = true;
      },
    };
  }

  const env = createContext({
    createWebSocket: makeSocket,
    getGamepads: () => [{
      connected: true,
      axes: [1, 0, 0, 0],
      buttons: [{ pressed: true }, { pressed: false }, { pressed: false }, { pressed: false }],
    }],
    fetchRoutes: {
      "/runtime.wasm": { bytes: [0, 97, 115, 109] },
    },
    manifest: {
      runtime: { path: "/runtime.wasm" },
      hubs: [
        {
          id: "gosx-hub-0",
          name: "fight",
          path: "/ws/fight/abc",
          bindings: [],
          input: {
            mode: "fighting",
            event: "input",
            readyEvent: "ready",
            signal: "$fightInput",
            player: 1,
            slotToken: "slot-one",
            sendEveryMs: 16,
          },
        },
      ],
    },
  });

  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();

  assert.equal(sent[0].event, "ready");
  assert.deepEqual(sent[0].data, { player: 1, slotToken: "slot-one" });
  const input = sent.find((message) => message.event === "input");
  assert.ok(input, "expected an input message");
  assert.equal(input.data.dir, 3);
  assert.equal(input.data.btn, 1);
  assert.equal(input.data.player, 1);
  assert.equal(input.data.slotToken, "slot-one");
  assert.ok(env.sharedSignalCalls.some((call) => call[0] === "$fightInput" && call[1].includes("GAMEPAD LINKED")));

  env.context.__gosx_disconnect_hub("gosx-hub-0");
  assert.equal(env.sockets[0].closeCalled, true);
});

test("bootstrap hub input spectator mode readies without sending fighter inputs", async () => {
  const sent = [];
  function makeSocket(url) {
    return {
      url,
      readyState: 1,
      closeCalled: false,
      send(raw) {
        sent.push(JSON.parse(raw));
      },
      close() {
        this.closeCalled = true;
      },
    };
  }

  const env = createContext({
    createWebSocket: makeSocket,
    getGamepads: () => [{
      connected: true,
      axes: [1, 0, 0, 0],
      buttons: [{ pressed: true }, { pressed: false }, { pressed: false }, { pressed: false }],
    }],
    fetchRoutes: {
      "/runtime.wasm": { bytes: [0, 97, 115, 109] },
    },
    manifest: {
      runtime: { path: "/runtime.wasm" },
      hubs: [
        {
          id: "gosx-hub-0",
          name: "fight",
          path: "/ws/fight/cpu-duel",
          bindings: [],
          input: {
            mode: "fighting",
            event: "input",
            readyEvent: "ready",
            signal: "$fightInput",
            player: 1,
            slotToken: "spectator-slot",
            spectator: true,
            sendEveryMs: 16,
          },
        },
      ],
    },
  });

  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();

  assert.deepEqual(sent, [{ event: "ready", data: { player: 1, slotToken: "spectator-slot" } }]);
  assert.ok(env.sharedSignalCalls.some((call) => call[0] === "$fightInput" && call[1].includes("CPU DUEL")));

  env.context.__gosx_disconnect_hub("gosx-hub-0");
  assert.equal(env.sockets[0].closeCalled, true);
});

test("bootstrap hub input turns fight events into bounded gamepad feedback", async () => {
  const sent = [];
  const effects = [];
  let audioStarts = 0;
  let forcedStops = 0;
  const panValues = [];
  class FakeAudioNode {
    constructor(kind) {
      this.kind = kind;
      this.connections = [];
    }

    connect(target) {
      this.connections.push(target);
      return target;
    }

    disconnect() {}
  }
  class FakeArcadeAudioContext {
    constructor() {
      this.currentTime = 0;
      this.destination = new FakeAudioNode("destination");
    }

    resume() {
      return Promise.resolve();
    }

    createOscillator() {
      const source = new FakeAudioNode("oscillator");
      source.frequency = { value: 0 };
      source.start = () => {
        audioStarts++;
      };
      source.stop = (when) => {
        if (when === 0) forcedStops++;
      };
      return source;
    }

    createGain() {
      const gain = new FakeAudioNode("gain");
      gain.gain = { value: 1 };
      return gain;
    }

    createStereoPanner() {
      const panner = new FakeAudioNode("panner");
      let value = 0;
      panner.pan = {};
      Object.defineProperty(panner.pan, "value", {
        get() {
          return value;
        },
        set(next) {
          value = next;
          panValues.push(next);
        },
      });
      return panner;
    }
  }
  const pad = {
    connected: true,
    axes: [0, 0, 0, 0],
    buttons: Array.from({ length: 16 }, () => ({ pressed: false, value: 0 })),
    vibrationActuator: {
      playEffect(type, options) {
        effects.push({ type, options });
        return Promise.resolve();
      },
    },
  };
  function makeSocket(url) {
    return {
      url,
      readyState: 1,
      closeCalled: false,
      send(raw) {
        sent.push(JSON.parse(raw));
      },
      close() {
        this.closeCalled = true;
      },
    };
  }

  const env = createContext({
    AudioContext: FakeArcadeAudioContext,
    createWebSocket: makeSocket,
    getGamepads: () => [pad],
    fetchRoutes: {
      "/runtime.wasm": { bytes: [0, 97, 115, 109] },
    },
    manifest: {
      runtime: { path: "/runtime.wasm" },
      hubs: [
        {
          id: "gosx-hub-0",
          name: "fight",
          path: "/ws/fight/abc",
          bindings: [],
          input: {
            mode: "fighting",
            event: "input",
            readyEvent: "ready",
            signal: "$fightInput",
            player: 1,
            sendEveryMs: 16,
          },
        },
      ],
    },
  });

  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();

  env.sockets[0].onmessage({
    data: JSON.stringify({
      event: "tick",
      data: {
        event: { seq: 1, kind: "hit", damage: 120, counter: true, attacker: 1, defender: 2 },
        audio: { seq: 1, cue: "counter", intensity: 0.95, pan: 0.5, depth: 0.2, phaseCue: "fight" },
      },
    }),
  });
  await flushAsyncWork();

  assert.equal(effects.length, 1);
  assert.equal(effects[0].type, "dual-rumble");
  assert.ok(effects[0].options.duration <= 160);
  assert.ok(effects[0].options.strongMagnitude > effects[0].options.weakMagnitude);
  assert.ok(audioStarts > 0, "expected fight audio to start voices");
  assert.ok(panValues.some((value) => Math.abs(value - 0.5) < 0.000001), "expected server-provided pan to drive audio");
  const startsAfterFirst = audioStarts;

  env.sockets[0].onmessage({
    data: JSON.stringify({
      event: "tick",
      data: {
        event: { seq: 1, kind: "hit", damage: 120, counter: true },
        audio: { seq: 1, cue: "counter", intensity: 0.95, pan: 0.5, phaseCue: "fight" },
      },
    }),
  });
  await flushAsyncWork();
  assert.equal(effects.length, 1, "same server event seq should not replay haptics");
  assert.equal(audioStarts, startsAfterFirst, "same server event seq should not replay audio");

  env.sockets[0].onmessage({
    data: JSON.stringify({ event: "tick", data: { event: { seq: 2, kind: "block", damage: 0, blocked: true } } }),
  });
  await flushAsyncWork();
  assert.equal(effects.length, 2);
  assert.ok(effects[1].options.weakMagnitude >= effects[1].options.strongMagnitude * 0.8);

  for (let seq = 3; seq < 18; seq += 1) {
    env.sockets[0].onmessage({
      data: JSON.stringify({
        event: "tick",
        data: {
          event: { seq, kind: "hit", damage: 140, punish: true },
          audio: { seq, cue: "punish", intensity: 1, pan: -0.35 },
        },
      }),
    });
  }
  await flushAsyncWork();
  assert.ok(forcedStops > 0, "audio voice cap should cull older arcade voices");

  env.context.__gosx_disconnect_hub("gosx-hub-0");
  assert.equal(env.sockets[0].closeCalled, true);
});

test("bootstrap hub arcade-select owns lobby identity and menu state", async () => {
  const sent = [];
  function makeSocket(url) {
    return {
      url,
      readyState: 1,
      closeCalled: false,
      send(raw) {
        sent.push(JSON.parse(raw));
      },
      close() {
        this.closeCalled = true;
      },
    };
  }

  const env = createContext({
    createWebSocket: makeSocket,
    fetchRoutes: {
      "/runtime.wasm": { bytes: [0, 97, 115, 109] },
    },
    manifest: {
      runtime: { path: "/runtime.wasm" },
      clientIdentity: {
        storageKey: "feralsurge.clientId",
        cookieName: "fs_client_id",
        headerName: "X-Feral-Surge-Client-ID",
        prefix: "fs-",
      },
      hubs: [
        {
          id: "gosx-hub-0",
          name: "lobby",
          path: "/ws/lobby",
          bindings: [{ event: "lobby_state", signal: "$lobby" }],
          input: {
            mode: "arcade-select",
            event: "queue",
            readyEvent: "join",
            trainingEvent: "dequeue",
            signal: "$landing",
            attractSignal: "$attract",
            lobbySignal: "$lobby",
            vsSignal: "$vs",
            username: "Ada",
            sendEveryMs: 90,
          },
        },
      ],
    },
  });

  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();

  assert.equal(sent[0].event, "join");
  assert.equal(sent[0].data.name, "Ada");
  assert.match(sent[0].data.clientId, /^fs-/);
  assert.equal(env.context.__gosx.identity.headerName, "X-Feral-Surge-Client-ID");
  assert.ok(env.sharedSignalCalls.some((call) => call[0] === "$landing" && call[1].includes("PICK A FIGHTER")));
  assert.ok(env.sharedSignalCalls.some((call) => call[0] === "$attract" && call[1].includes("title")));

  env.sockets[0].onmessage({ data: JSON.stringify({ event: "queued", data: { queueSize: 2 } }) });
  await flushAsyncWork();
  assert.ok(env.sharedSignalCalls.some((call) => call[0] === "$landing" && call[1].includes("MATCHMAKING")));
  assert.ok(env.sharedSignalCalls.some((call) => call[0] === "$lobby" && call[1].includes('"size":2')));

  env.context.__gosx_disconnect_hub("gosx-hub-0");
  assert.equal(env.sockets[0].closeCalled, true);
});

test("patch applier updates text nodes and treats setHTML as text", async () => {
  const wrapper = new FakeElement("div", null);
  const componentRoot = new FakeElement("div", null);
  const counter = new FakeElement("span", null);
  const htmlSink = new FakeElement("pre", null);

  wrapper.id = "gosx-island-patch";
  counter.textContent = "0";
  htmlSink.textContent = "";
  componentRoot.appendChild(counter);
  componentRoot.appendChild(htmlSink);
  wrapper.appendChild(componentRoot);

  const env = createContext({
    elements: [wrapper],
  });

  runScript(patchSource, env.context, "patch.js");
  env.context.__gosx_apply_patches(
    "gosx-island-patch",
    JSON.stringify([
      { kind: 0, path: "0", text: "1" },
      { kind: 9, path: "1", text: "<strong>safe</strong>" },
    ]),
  );

  assert.equal(counter.textContent, "1");
  assert.equal(htmlSink.textContent, "<strong>safe</strong>");
  assert.equal(htmlSink.children.length, 0);
});

test("patch applier recreates missing empty text targets", async () => {
  const wrapper = new FakeElement("div", null);
  const componentRoot = new FakeElement("div", null);
  const chip = new FakeElement("div", null);

  wrapper.id = "gosx-island-empty-text";
  componentRoot.appendChild(chip);
  wrapper.appendChild(componentRoot);

  const env = createContext({
    elements: [wrapper],
  });

  runScript(patchSource, env.context, "patch.js");
  env.context.__gosx_apply_patches(
    "gosx-island-empty-text",
    JSON.stringify([{ kind: 0, path: "0/0", text: "THROW" }]),
  );

  assert.equal(chip.textContent, "THROW");
  assert.equal(chip.childNodes.length, 1);
  assert.equal(chip.childNodes[0].nodeType, TEXT_NODE);
  assert.equal(env.consoleLogs.warn.length, 0);
});

test("bootstrap reconnects hubs whose sockets died while the page was frozen", async () => {
  // A page restored from the back-forward cache resumes with its WebSockets
  // already torn down (Chrome logs "WebSocket connection failed: Page entered
  // Back-Forward Cache" on freeze). Only the socket's close event schedules a
  // reconnect, and a frozen page is not guaranteed to deliver it — the hub
  // then stays dead and every hub-bound signal stops updating until the
  // reader reloads by hand. pageshow must repair the connection from the
  // socket's actual state.
  function makeSocket(url) {
    return {
      url,
      readyState: 1,
      closeCalled: false,
      onmessage: null,
      onclose: null,
      onerror: null,
      close() { this.closeCalled = true; },
    };
  }

  const env = createContext({
    createWebSocket: makeSocket,
    fetchRoutes: { "/runtime.wasm": { bytes: [0, 97, 115, 109] } },
    manifest: {
      hubs: [
        { id: "gosx-hub-0", name: "presence", path: "/gosx/hub/presence", bindings: [] },
      ],
    },
  });

  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();
  assert.equal(env.sockets.length, 1);

  // Live socket: pageshow must not churn the connection.
  env.context.dispatchEvent({ type: "pageshow", persisted: false });
  await flushAsyncWork();
  assert.equal(env.sockets.length, 1, "a live hub socket must not be reconnected");

  // Freeze/restore: the browser closed the socket without dispatching close.
  env.sockets[0].readyState = 3;
  env.context.dispatchEvent({ type: "pageshow", persisted: true });
  await flushAsyncWork();

  assert.equal(env.sockets.length, 2, "a closed hub socket must be reconnected on pageshow");
  assert.equal(env.context.__gosx.hubs.size, 1, "the reconnect replaces the record, never duplicates it");
  assert.equal(env.context.__gosx.hubs.get("gosx-hub-0").socket, env.sockets[1]);
  assert.equal(env.sockets[1].url, "ws://localhost:3000/gosx/hub/presence");
});

// Fighting-mode input: key and touch edges send in the same event turn, and
// the ready event can wait for the Scene3D mount.
function createFightInputEnv(inputOverrides, extra) {
  const sent = [];
  const sockets = [];
  function makeSocket(url) {
    const socket = {
      url,
      readyState: 1,
      closeCalled: false,
      send(raw) {
        sent.push(JSON.parse(raw));
      },
      close() {
        this.closeCalled = true;
      },
    };
    sockets.push(socket);
    return socket;
  }
  const env = createContext(Object.assign({
    createWebSocket: makeSocket,
    fetchRoutes: {
      "/runtime.wasm": { bytes: [0, 97, 115, 109] },
    },
    manifest: {
      runtime: { path: "/runtime.wasm" },
      hubs: [
        {
          id: "gosx-hub-0",
          name: "fight",
          path: "/ws/fight/abc",
          bindings: [],
          input: Object.assign({
            mode: "fighting",
            event: "input",
            readyEvent: "ready",
            player: 1,
            slotToken: "slot-one",
            sendEveryMs: 100,
            touchRoot: "#touch-controls",
          }, inputOverrides),
        },
      ],
    },
  }, extra || {}));
  return { env, sent, sockets };
}

function keyEvent(type, key, code, extra) {
  return Object.assign({ type, key, code, repeat: false, target: null, preventDefault() {} }, extra);
}

function inputsOf(sent) {
  return sent.filter((message) => message.event === "input").map((message) => message.data);
}

test("fighting input sends key edges in the same event turn", async () => {
  const { env, sent } = createFightInputEnv({});
  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();
  const document = env.context.document;
  const before = inputsOf(sent).length;

  document.dispatchEvent(keyEvent("keydown", "d", "KeyD"));
  let inputs = inputsOf(sent);
  assert.equal(inputs.length, before + 1, "press sends immediately");
  assert.equal(inputs[inputs.length - 1].dir, 3);
  assert.equal(inputs[inputs.length - 1].slotToken, "slot-one");

  document.dispatchEvent(keyEvent("keydown", "d", "KeyD", { repeat: true }));
  assert.equal(inputsOf(sent).length, before + 1, "key repeat does not resend");

  document.dispatchEvent(keyEvent("keydown", "u", "KeyU"));
  inputs = inputsOf(sent);
  assert.equal(inputs.length, before + 2);
  assert.deepEqual([inputs[inputs.length - 1].dir, inputs[inputs.length - 1].btn], [3, 1], "each send carries full held state");

  document.dispatchEvent(keyEvent("keyup", "d", "KeyD"));
  document.dispatchEvent(keyEvent("keyup", "u", "KeyU"));
  inputs = inputsOf(sent);
  assert.equal(inputs.length, before + 4, "release sends immediately");
  assert.deepEqual([inputs[inputs.length - 1].dir, inputs[inputs.length - 1].btn], [0, 0]);

  document.dispatchEvent(keyEvent("keydown", "x", "KeyX"));
  assert.equal(inputsOf(sent).length, before + 4, "unmapped keys send nothing");

  env.context.__gosx_disconnect_hub("gosx-hub-0");
});

test("fighting input ignores edges that do not change the sent state and editable targets", async () => {
  const { env, sent } = createFightInputEnv({});
  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();
  const document = env.context.document;
  const editable = { closest: (selector) => (/input/.test(selector) ? {} : null) };

  document.dispatchEvent(keyEvent("keydown", "d", "KeyD"));
  const after = inputsOf(sent).length;
  // ArrowRight maps to the same direction, so the full state is unchanged.
  document.dispatchEvent(keyEvent("keydown", "ArrowRight", "ArrowRight"));
  assert.equal(inputsOf(sent).length, after, "identical consecutive state is not resent");

  document.dispatchEvent(keyEvent("keydown", "j", "KeyJ", { target: editable }));
  assert.equal(inputsOf(sent).length, after, "typing in an input field does not send");

  env.context.__gosx_disconnect_hub("gosx-hub-0");
});

test("fighting input sends touch control edges and flushes after blur releases keys", async () => {
  const touchRoot = new FakeElement("div", null);
  touchRoot.id = "touch-controls";
  const button = new FakeElement("button", null);
  button.dataset.btn = "lp";
  button.setAttribute("data-btn", "lp");
  touchRoot.appendChild(button);
  const { env, sent } = createFightInputEnv({}, { elements: [touchRoot] });
  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();
  const document = env.context.document;
  const before = inputsOf(sent).length;

  document.dispatchEvent({ type: "pointerdown", pointerId: 1, target: button, preventDefault() {} });
  let inputs = inputsOf(sent);
  assert.equal(inputs.length, before + 1);
  assert.equal(inputs[inputs.length - 1].btn, 1);
  document.dispatchEvent({ type: "pointerup", pointerId: 1, target: button, preventDefault() {} });
  inputs = inputsOf(sent);
  assert.equal(inputs.length, before + 2);
  assert.equal(inputs[inputs.length - 1].btn, 0);

  document.dispatchEvent(keyEvent("keydown", "d", "KeyD"));
  const held = inputsOf(sent).length;
  env.context.window.dispatchEvent({ type: "blur" });
  inputs = inputsOf(sent);
  assert.equal(inputs.length, held + 1, "blur releases held keys and sends the release");
  assert.equal(inputs[inputs.length - 1].dir, 0);

  env.context.__gosx_disconnect_hub("gosx-hub-0");
});

test("fighting input disableEdgeSend keeps timer-only sending", async () => {
  const { env, sent } = createFightInputEnv({ disableEdgeSend: true });
  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();
  const before = inputsOf(sent).length;
  env.context.document.dispatchEvent(keyEvent("keydown", "d", "KeyD"));
  assert.equal(inputsOf(sent).length, before, "no send until the next timer pump");
  env.context.__gosx_disconnect_hub("gosx-hub-0");
});

test("fighting input readyWhen scene-committed waits for the scene and two frames, once per socket", async () => {
  const mount = new FakeElement("div", null);
  mount.id = "arena";
  const { env, sent, sockets } = createFightInputEnv({ readyWhen: "scene-committed" }, { elements: [mount] });
  const raf = installManualRAF(env.context);
  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();
  const readies = () => sent.filter((message) => message.event === "ready");
  const record = env.context.__gosx.hubs.get("gosx-hub-0");
  const pump = () => record.inputController.flush();

  pump();
  raf.flush();
  raf.flush();
  assert.equal(readies().length, 0, "socket open alone is not enough");

  mount.setAttribute("data-gosx-scene3d-ready", "true");
  pump();
  raf.flush();
  raf.flush();
  assert.equal(readies().length, 0, "ready without model hydration is not enough");

  mount.setAttribute("data-gosx-scene3d-model-hydration-committed", "true");
  pump();
  pump();
  assert.equal(raf.count(), 1, "one pending frame chain per socket");
  raf.flush();
  assert.equal(readies().length, 0, "first frame does not send");
  raf.flush();
  assert.equal(readies().length, 1, "second frame sends ready");
  assert.deepEqual(readies()[0].data, { player: 1, slotToken: "slot-one" });
  pump();
  raf.flush();
  raf.flush();
  assert.equal(readies().length, 1, "at most one ready per socket");

  // A reconnect swaps record.socket; the new socket gets its own ready.
  const reconnect = Object.assign({}, sockets[0], { sent: true });
  reconnect.send = sockets[0].send;
  record.socket = reconnect;
  pump();
  raf.flush();
  raf.flush();
  assert.equal(readies().length, 2, "a new socket sends ready again");

  env.context.__gosx_disconnect_hub("gosx-hub-0");
});

test("fighting input readyWhen scene-committed drops ready when the scene unmounts before the second frame", async () => {
  const mount = new FakeElement("div", null);
  mount.id = "arena";
  mount.setAttribute("data-gosx-scene3d-ready", "true");
  mount.setAttribute("data-gosx-scene3d-model-hydration-committed", "true");
  const { env, sent } = createFightInputEnv({ readyWhen: "scene-committed" }, { elements: [mount] });
  const raf = installManualRAF(env.context);
  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();
  const record = env.context.__gosx.hubs.get("gosx-hub-0");
  record.inputController.flush();
  raf.flush();
  mount.setAttribute("data-gosx-scene3d-model-hydration-committed", "false");
  raf.flush();
  assert.equal(sent.filter((message) => message.event === "ready").length, 0);
  mount.setAttribute("data-gosx-scene3d-model-hydration-committed", "true");
  record.inputController.flush();
  raf.flush();
  raf.flush();
  assert.equal(sent.filter((message) => message.event === "ready").length, 1);
  env.context.__gosx_disconnect_hub("gosx-hub-0");
});
