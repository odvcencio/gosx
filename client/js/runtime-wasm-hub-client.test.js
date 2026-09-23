const test = require("node:test");
const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const vm = require("node:vm");

const source = fs.readFileSync(path.join(__dirname, "../runtime/host/wasm-hub.ts"), "utf8");

function fixture() {
  const sockets = [];
  const context = {
    location: { protocol: "https:", host: "example.test" },
    __gosx: { host: {} },
    WebSocket: class {
      constructor(url) { this.url = url; this.readyState = 0; this.sent = []; sockets.push(this); }
      send(raw) { this.sent.push(raw); }
      close(code, reason) { this.readyState = 3; this.closed = { code, reason }; }
    },
  };
  context.window = context;
  vm.runInNewContext(source, context, { filename: "wasm-hub.ts" });
  return { api: context.__gosx.host.wasmHub, sockets };
}

test("WASM hub transport passes one unparsed frame and close metadata", () => {
  const { api, sockets } = fixture();
  const events = [];
  const handle = api.open("/ws/ABCDEF?resume=token", 128, (...args) => events.push(args));
  const socket = sockets[0];
  assert.equal(socket.url, "wss://example.test/ws/ABCDEF?resume=token");
  socket.readyState = 1;
  socket.onopen();
  const raw = '{"event":"state","data":{"enemies":[1]}}';
  socket.onmessage({ data: raw });
  assert.deepEqual(events.map(event => event[0]), ["open", "message"]);
  assert.equal(events[1][1], raw);
  assert.equal(handle.send('{"event":"input","data":{}}'), true);
  assert.equal(socket.sent.length, 1);
  socket.onclose({ code: 1008, reason: "session superseded" });
  assert.deepEqual(events[2].slice(0, 4), ["close", "", 1008, "session superseded"]);
  handle.dispose();
  assert.equal(handle.state(), 3);
});

test("WASM hub transport bounds UTF-8 text and ignores stale callbacks", () => {
  const { api, sockets } = fixture();
  const events = [];
  const handle = api.open("ws://example.test/ws/X", 8, (...args) => events.push(args));
  const socket = sockets[0];
  socket.readyState = 1;
  assert.equal(handle.send("12345678"), true);
  assert.equal(handle.send("123456789"), false);
  const staleMessage = socket.onmessage;
  socket.onmessage({ data: "ééééé" });
  assert.equal(events[0][0], "error");
  assert.equal(events[0][1], "frame-too-large");
  assert.equal(socket.closed.code, 4009);
  handle.dispose();
  staleMessage({ data: "{}" });
  assert.equal(events.length, 1);
});
