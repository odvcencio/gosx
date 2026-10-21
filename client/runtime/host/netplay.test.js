const test = require("node:test");
const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const vm = require("node:vm");

const source = fs.readFileSync(path.join(__dirname, "netplay.ts"), "utf8");

class FakeChannel {
  constructor(label, init) {
    this.label = label; this.init = init; this.readyState = "connecting";
    this.bufferedAmount = 0; this.sent = []; this.binaryType = "blob";
  }
  send(data) {
    if (this.readyState !== "open") throw new Error("InvalidStateError");
    this.sent.push(Uint8Array.from(data));
  }
  close() { this.readyState = "closed"; if (this.onclose) this.onclose({}); }
  open() { this.readyState = "open"; if (this.onopen) this.onopen({}); }
  deliver(bytes) { if (this.onmessage) this.onmessage({ data: Uint8Array.from(bytes).buffer }); }
}

class FakePC {
  constructor(config) {
    this.config = config; this.calls = []; this.connectionState = "new"; this.closed = false;
    FakePC.last = this; FakePC.all.push(this);
  }
  createDataChannel(label, init) { this.channel = new FakeChannel(label, init); return this.channel; }
  async createOffer() { this.calls.push("createOffer"); return { type: "offer", sdp: "v=0\r\nm=application\r\n" }; }
  async setLocalDescription(d) {
    this.calls.push("setLocalDescription"); this.localDescription = d;
    // A real browser starts gathering here, before the caller continues.
    if (this.onicecandidate) this.onicecandidate({ candidate: { candidate: "candidate:1 1 udp 1 1.2.3.4 5 typ host", sdpMid: "0", sdpMLineIndex: 0, usernameFragment: "uf" } });
  }
  async setRemoteDescription(d) { this.calls.push("setRemoteDescription"); this.remoteDescription = d; }
  async addIceCandidate(c) { this.calls.push("addIceCandidate:" + (c && c.candidate)); }
  close() { this.closed = true; this.connectionState = "closed"; }
  endGathering() { if (this.onicecandidate) this.onicecandidate({ candidate: null }); }
  setState(s) { this.connectionState = s; if (this.onconnectionstatechange) this.onconnectionstatechange({}); }
}
FakePC.all = [];

function boot(opts = {}) {
  FakePC.all = [];
  const win = {};
  const context = vm.createContext({
    window: win, console, Uint8Array, Int32Array, ArrayBuffer, JSON, Promise, Map, Math, Number, String, Object, Array,
    queueMicrotask,
    RTCPeerConnection: opts.noRTC ? undefined : FakePC,
  });
  win.RTCPeerConnection = context.RTCPeerConnection;
  let factory;
  win.__gosx_register_bootstrap_feature = (name, f) => { assert.equal(name, "netplay"); factory = f; };
  vm.runInContext(source, context, { filename: "netplay.ts" });
  return { win, np: win.__gosx_netplay, factory };
}

const plain = v => JSON.parse(JSON.stringify(v));
const tick = () => new Promise(resolve => setImmediate(resolve));

function connect(np, extra = {}) {
  const signals = [], states = [];
  const id = np.open({
    session: "s1", iceServers: [{ urls: ["stun:stun.example.org"] }],
    onSignal: (event, data) => signals.push({ event, data: JSON.parse(data) }),
    onState: s => states.push(s), ...extra,
  });
  return { id, signals, states, pc: FakePC.last };
}

test("open builds an unordered zero-retransmit negotiated channel", async () => {
  const { np } = boot();
  const c = connect(np);
  assert.ok(c.id > 0);
  assert.deepEqual(plain(c.pc.config.iceServers), [{ urls: ["stun:stun.example.org"] }]);
  assert.equal(c.pc.channel.label, "netplay");
  assert.deepEqual(plain(c.pc.channel.init), { ordered: false, maxRetransmits: 0, negotiated: true, id: 0 });
  assert.equal(c.pc.channel.binaryType, "arraybuffer");
  assert.equal(np.state(c.id), "connecting");
});

test("the offer goes out before any candidate, then candidates follow", async () => {
  const { np } = boot();
  const c = connect(np);
  await tick();
  c.pc.endGathering();
  assert.deepEqual(c.signals.map(s => s.event), ["netplay.offer", "netplay.candidate", "netplay.candidate"]);
  assert.equal(c.signals[0].data.session, "s1");
  assert.match(c.signals[0].data.sdp, /^v=0/);
  assert.deepEqual(c.signals[1].data, { session: "s1", candidate: "candidate:1 1 udp 1 1.2.3.4 5 typ host", sdpMid: "0", sdpMLineIndex: 0, usernameFragment: "uf" });
  assert.equal(c.signals[2].data.candidate, "");
});

test("relay candidates wait for the answer, then apply in order", async () => {
  const { np } = boot();
  const c = connect(np);
  await tick();
  np.signal(c.id, "netplay.candidate", JSON.stringify({ session: "s1", candidate: "candidate:9 1 udp 1 5.6.7.8 5 typ host", sdpMid: "0", sdpMLineIndex: 0 }));
  await tick();
  assert.ok(!c.pc.calls.some(x => x.startsWith("addIceCandidate")));
  np.signal(c.id, "netplay.answer", JSON.stringify({ session: "s1", sdp: "v=0\r\nanswer" }));
  await tick();
  const order = c.pc.calls.filter(x => x === "setRemoteDescription" || x.startsWith("addIceCandidate"));
  assert.deepEqual(order, ["setRemoteDescription", "addIceCandidate:candidate:9 1 udp 1 5.6.7.8 5 typ host"]);
  assert.equal(c.pc.remoteDescription.type, "answer");
});

test("bad signals are ignored", async () => {
  const { np } = boot();
  const c = connect(np);
  await tick();
  np.signal(c.id, "netplay.answer", "not json");
  np.signal(c.id, "netplay.answer", JSON.stringify({ session: "other", sdp: "v=0" }));
  np.signal(c.id, "netplay.answer", JSON.stringify({ session: "s1", sdp: 5 }));
  np.signal(c.id, "netplay.nope", "{}");
  np.signal(999, "netplay.answer", "{}");
  await tick();
  assert.ok(!c.pc.calls.includes("setRemoteDescription"));
  assert.equal(np.state(c.id), "connecting");
});

test("send and recv move datagrams once the channel is open", async () => {
  const { np } = boot();
  const c = connect(np);
  await tick();
  const out = Uint8Array.from([1, 2, 3, 4, 5]);
  assert.equal(np.send(c.id, out, 5), false, "closed channel refuses");
  c.pc.channel.open();
  assert.equal(np.state(c.id), "open");
  assert.deepEqual(c.states, ["open"]);
  assert.equal(np.send(c.id, out, 3), true);
  assert.deepEqual(Array.from(c.pc.channel.sent[0]), [1, 2, 3]);
  assert.equal(np.send(c.id, new Uint8Array(1201), 1201), false, "over 1200 bytes");
  c.pc.channel.bufferedAmount = 17 * 1024;
  assert.equal(np.send(c.id, out, 3), false, "congested channel drops");
  assert.equal(np.stats(c.id).sendDropped, 2);

  const dst = new Uint8Array(1200);
  assert.equal(np.recv(c.id, dst), -1);
  c.pc.channel.deliver([9, 8, 7]);
  c.pc.channel.deliver([6]);
  assert.equal(np.recv(c.id, dst), 3);
  assert.deepEqual(Array.from(dst.subarray(0, 3)), [9, 8, 7]);
  assert.equal(np.recv(c.id, dst), 1);
  assert.equal(np.recv(c.id, dst), -1);
});

test("receive queue is bounded and oversize datagrams drop", async () => {
  const { np } = boot();
  const c = connect(np);
  c.pc.channel.open();
  for (let i = 0; i < 200; i++) c.pc.channel.deliver([i & 255]);
  c.pc.channel.deliver(new Uint8Array(1201));
  const st = np.stats(c.id);
  assert.equal(st.received, 128);
  assert.equal(st.receiveDropped, 73);
  const dst = new Uint8Array(1200);
  let n = 0;
  while (np.recv(c.id, dst) >= 0) n++;
  assert.equal(n, 128);
});

test("connection failure and close are reported once", async () => {
  const { np } = boot();
  const c = connect(np);
  await tick();
  c.pc.setState("failed");
  assert.equal(np.state(c.id), "failed");
  assert.deepEqual(c.states, ["failed"]);
  c.pc.setState("closed");
  assert.deepEqual(c.states, ["failed"]);
  assert.equal(c.pc.closed, true, "failed connection is released");
});

test("an open channel that drops reports closed", async () => {
  const { np } = boot();
  const c = connect(np);
  c.pc.channel.open();
  c.pc.channel.close();
  assert.deepEqual(c.states, ["open", "closed"]);
  assert.equal(np.state(c.id), "closed");
});

test("close releases the connection and later calls are harmless", async () => {
  const { np } = boot();
  const c = connect(np);
  c.pc.channel.open();
  np.close(c.id);
  assert.equal(c.pc.closed, true);
  assert.equal(np.state(c.id), "closed");
  assert.equal(np.send(c.id, new Uint8Array(2), 2), false);
  assert.equal(np.recv(c.id, new Uint8Array(8)), -1);
  np.close(c.id);
  np.signal(c.id, "netplay.answer", "{}");
});

test("without RTCPeerConnection open reports failure", async () => {
  const { np } = boot({ noRTC: true });
  assert.equal(np.supported(), false);
  const states = [];
  const id = np.open({ session: "s1", onSignal() {}, onState: s => states.push(s) });
  assert.equal(id, 0);
  assert.deepEqual(states, ["failed"]);
});

test("a throwing offer step fails the connection", async () => {
  const { np } = boot();
  const orig = FakePC.prototype.createOffer;
  FakePC.prototype.createOffer = async function () { throw new Error("boom"); };
  try {
    const c = connect(np);
    await tick();
    assert.equal(np.state(c.id), "failed");
    assert.deepEqual(c.states, ["failed"]);
  } finally { FakePC.prototype.createOffer = orig; }
});

test("the feature closes every connection on page disposal", async () => {
  const { np, factory } = boot();
  const a = connect(np), b = connect(np);
  const feature = factory({});
  feature.disposePage();
  assert.equal(a.pc.closed && b.pc.closed, true);
  assert.equal(np.state(a.id), "closed");
});

test("session and server limits are enforced", async () => {
  const { np } = boot();
  const ids = [];
  for (let i = 0; i < 8; i++) ids.push(connect(np).id);
  assert.ok(ids.every(x => x > 0));
  const states = [];
  assert.equal(np.open({ session: "s1", onSignal() {}, onState: s => states.push(s) }), 0, "ninth connection refused");
  assert.deepEqual(states, ["failed"]);
});
