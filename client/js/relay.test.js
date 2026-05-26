// Unit tests for client/js/relay.js — exercises the JS side of the
// cross-frame postMessage relay defined by ADR 0009.
//
// Run via: node --test client/js/relay.test.js
//
// Plan section B — see plans/2026-05-26-iframe-cross-frame-signal-transport.md.

const test = require("node:test");
const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const vm = require("node:vm");

const relaySource = fs.readFileSync(path.join(__dirname, "relay.js"), "utf8");

// makeRelayContext sets up a minimal browser-like sandbox: window, console,
// a message-listener registry, and an origin. Returns the sandbox plus a
// helper to dispatch incoming postMessage events. Peer windows are simple
// objects with their own postMessage spy.
function makeRelayContext(opts) {
  opts = opts || {};
  const messageListeners = [];
  const window = {
    location: { origin: opts.origin || "https://storefront.example" },
    __gosx: {},
    addEventListener: function(type, fn) {
      if (type === "message") {
        messageListeners.push(fn);
      }
    },
    parent: null,
    frames: { length: 0 },
  };
  // Self-reference like a real browser window.
  window.window = window;
  window.parent = window; // default: top-level. Tests override below.
  const console = {
    warn: function() {},
    error: function() {},
  };
  const ctx = { window: window, console: console };
  vm.createContext(ctx);
  vm.runInContext(relaySource, ctx);

  function dispatchMessage(event) {
    for (const fn of messageListeners) {
      fn(event);
    }
  }

  function makePeerWindow(origin) {
    const peer = {
      sent: [],
      postMessage: function(msg, targetOrigin) {
        peer.sent.push({ msg: msg, targetOrigin: targetOrigin });
      },
      _origin: origin,
    };
    return peer;
  }

  return {
    window: window,
    dispatchMessage: dispatchMessage,
    makePeerWindow: makePeerWindow,
    messageListeners: messageListeners,
  };
}

// B.1: __gosx.relay.send posts the expected message shape to peers after
// the relay is configured with a matching prefix.
test("send posts gosx:shared-signal message to registered peers", () => {
  const env = makeRelayContext({ origin: "https://storefront.example" });
  const peer = env.makePeerWindow("https://editor.example");
  env.window.__gosx_relay_configure([
    { prefix: "$preview.", allowedOrigin: "https://editor.example" },
  ]);
  env.window.__gosx_relay_register_peer(peer, "https://editor.example");

  env.window.__gosx_relay_send("$preview.block.hero.visible", "true");

  assert.equal(peer.sent.length, 1, "peer should receive 1 message");
  assert.equal(peer.sent[0].msg.type, "gosx:shared-signal");
  assert.equal(peer.sent[0].msg.name, "$preview.block.hero.visible");
  assert.equal(peer.sent[0].msg.valueJSON, "true");
  assert.equal(peer.sent[0].msg.origin, "https://storefront.example");
  assert.equal(peer.sent[0].targetOrigin, "https://editor.example");
});

// B.2: send is a no-op for non-relayed prefixes.
test("send is no-op for non-relayed prefixes", () => {
  const env = makeRelayContext({});
  const peer = env.makePeerWindow("https://editor.example");
  env.window.__gosx_relay_configure([
    { prefix: "$preview.", allowedOrigin: "https://editor.example" },
  ]);
  env.window.__gosx_relay_register_peer(peer, "https://editor.example");

  env.window.__gosx_relay_send("$selection.block.hero", '"sel"');

  assert.equal(peer.sent.length, 0, "non-relayed signal must not post");
});

// B.3: inbound gosx:shared-signal messages route to __gosx_relay_dispatch_inbound
// when the prefix matches and origin is allowed.
test("inbound message dispatches to __gosx_relay_dispatch_inbound", () => {
  const env = makeRelayContext({});
  const dispatched = [];
  env.window.__gosx_relay_dispatch_inbound = function(name, valueJSON, origin) {
    dispatched.push({ name: name, valueJSON: valueJSON, origin: origin });
  };
  env.window.__gosx_relay_configure([
    { prefix: "$preview.", allowedOrigin: "https://editor.example" },
  ]);

  env.dispatchMessage({
    data: {
      type: "gosx:shared-signal",
      name: "$preview.field.title",
      valueJSON: '"hello"',
      origin: "https://editor.example",
    },
    origin: "https://editor.example",
  });

  assert.equal(dispatched.length, 1);
  assert.equal(dispatched[0].name, "$preview.field.title");
  assert.equal(dispatched[0].valueJSON, '"hello"');
  assert.equal(dispatched[0].origin, "https://editor.example");
});

// B.4: messages with non-matching type are ignored.
test("inbound message with wrong type is ignored", () => {
  const env = makeRelayContext({});
  const dispatched = [];
  env.window.__gosx_relay_dispatch_inbound = function(name, valueJSON, origin) {
    dispatched.push({ name: name, valueJSON: valueJSON, origin: origin });
  };
  env.window.__gosx_relay_configure([
    { prefix: "$preview.", allowedOrigin: "*" },
  ]);

  env.dispatchMessage({
    data: { type: "other:message", name: "$preview.x", valueJSON: "1" },
    origin: "https://editor.example",
  });

  assert.equal(dispatched.length, 0);
});

// B.5: inbound message from disallowed origin is dropped.
test("inbound message from disallowed origin is dropped", () => {
  const env = makeRelayContext({});
  const dispatched = [];
  env.window.__gosx_relay_dispatch_inbound = function(name, valueJSON, origin) {
    dispatched.push({ name: name, valueJSON: valueJSON, origin: origin });
  };
  env.window.__gosx_relay_configure([
    { prefix: "$preview.", allowedOrigin: "https://editor.example" },
  ]);

  env.dispatchMessage({
    data: {
      type: "gosx:shared-signal",
      name: "$preview.x",
      valueJSON: "1",
      origin: "https://attacker.example",
    },
    origin: "https://attacker.example",
  });

  assert.equal(dispatched.length, 0, "disallowed origin must not dispatch");
});

// B.6: dev-mode "*" origin accepts any origin AND emits a console warning.
test("dev-mode * origin accepts any origin and warns", () => {
  let warned = false;
  const env = makeRelayContext({});
  env.window.console = {
    warn: function() { warned = true; },
    error: function() {},
  };
  // Reload module with the new console wired in.
  vm.runInContext(relaySource, vm.createContext(Object.assign({}, env)));
  // Reset and re-run with proper console wired:
  const ctx = { window: env.window, console: { warn: function() { warned = true; }, error: function() {} } };
  vm.createContext(ctx);
  vm.runInContext(relaySource, ctx);

  const dispatched = [];
  env.window.__gosx_relay_dispatch_inbound = function(name, valueJSON, origin) {
    dispatched.push({ name: name });
  };
  env.window.__gosx_relay_configure([
    { prefix: "$preview.", allowedOrigin: "*" },
  ]);

  assert.equal(warned, true, "dev-mode wildcard should emit console.warn");

  env.dispatchMessage({
    data: {
      type: "gosx:shared-signal",
      name: "$preview.x",
      valueJSON: "1",
      origin: "https://anywhere.example",
    },
    origin: "https://anywhere.example",
  });
  assert.equal(dispatched.length, 1, "dev-mode should accept any origin");
});

// B.7: inbound messages arriving before WASM Bridge is ready are buffered
// and replayed on flush.
test("inbound messages buffer pre-bridge and flush on demand", () => {
  const env = makeRelayContext({});
  env.window.__gosx_relay_configure([
    { prefix: "$preview.", allowedOrigin: "*" },
  ]);

  // No __gosx_relay_dispatch_inbound yet — message should buffer.
  env.dispatchMessage({
    data: {
      type: "gosx:shared-signal",
      name: "$preview.early",
      valueJSON: "42",
      origin: "https://editor.example",
    },
    origin: "https://editor.example",
  });

  const dispatched = [];
  env.window.__gosx_relay_dispatch_inbound = function(name, valueJSON, origin) {
    dispatched.push({ name: name, valueJSON: valueJSON });
  };

  env.window.__gosx_relay_flush_inbound();

  assert.equal(dispatched.length, 1);
  assert.equal(dispatched[0].name, "$preview.early");
  assert.equal(dispatched[0].valueJSON, "42");
});

// B.8: idempotent configure — repeating the same (prefix, origin) does not
// duplicate state.
test("configure is idempotent for repeated entries", () => {
  const env = makeRelayContext({});
  env.window.__gosx_relay_configure([
    { prefix: "$preview.", allowedOrigin: "https://editor.example" },
  ]);
  env.window.__gosx_relay_configure([
    { prefix: "$preview.", allowedOrigin: "https://editor.example" },
  ]);
  assert.equal(env.window.__gosx.relay.configs.length, 1);
});
