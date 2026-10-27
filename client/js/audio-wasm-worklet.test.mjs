// Exercise the reusable reactor host with a non-Cicada mono backend, unusual
// buffer sizes, memory growth, musical replacement, real buffer transfers, and
// a page that is slow to return the message buffer.
import {test} from 'node:test';
import assert from 'node:assert/strict';
import {readFileSync} from 'node:fs';
import vm from 'node:vm';

const source = readFileSync(new URL('../../game/audio/worklet/processor.js', import.meta.url), 'utf8');
const tick = () => new Promise(resolve => setImmediate(resolve));
const backend = {prefix: 'test_', maxFrames: 64, channels: 1, commandBytes: 16,
  messageBytes: 4, messageCapacity: 8, supportedCapabilities: 3, faultOpcode: 7, faultArgumentOffset: 2};
// One tick is one beat is one bar, so at 120000 milli-BPM and 1 kHz a bar is 500 samples.
const transport = {play: 1, stop: 2, seek: 3, target: 255, ticksPerBeat: 1, ticksPerBar: 1,
  seekBarOffset: 4, seekTickOffset: 8, boundaryTickOffset: 8, quantizedOpcodes: [9]};
const command = op => { const bytes = new Uint8Array(16); bytes[0] = op; return bytes; };

async function fixture({b = backend, rate = 1000, tempo = 120000, capabilities = 0, grow = false, required = 0, failRender = false, poll = false} = {}) {
  const messages = [], reactors = [];
  let Processor, closed = false;
  const port = {
    onmessage: null,
    postMessage(message, transfer = []) { messages.push(structuredClone(message, {transfer})); },
    close() { closed = true; },
  };
  const wasm = {Memory: WebAssembly.Memory, instantiate: async () => {
    const memory = new WebAssembly.Memory({initial: 1}), calls = [];
    let level = .25, once = false;
    const x = {
      memory, _initialize() {}, test_capabilities() { return capabilities; },
      test_init(r, frames, channels) { assert.equal(frames, b.maxFrames); assert.equal(channels, 1); return 0; },
      test_project_alloc() { return 2048; },
      test_out_ptr() { return 4096; }, test_cmd_ptr() { return 1024; }, test_cmd_cap() { return 8; },
      test_msg_ptr() { return 3072; },
      // `ring` queues records as an engine does: one call moves at most messageCapacity of them into the
      // region and the rest wait. Tests that fill the region by hand set `messages` instead.
      test_msg_drain() {
        x.drains++;
        if (!x.ring.length) { const n = x.messages || 0; x.messages = 0; return n; }
        const n = Math.min(x.ring.length, b.messageCapacity);
        for (let i = 0; i < n; i++) new Uint8Array(memory.buffer, 3072 + i * b.messageBytes, b.messageBytes).set(x.ring.shift());
        return n;
      },
      test_cmd_commit(n) { calls.push(new Uint8Array(memory.buffer, 1024, n * b.commandBytes).slice()); return 0; },
      test_render(n) {
        if (failRender) throw new Error('render failed');
        if (grow && !once) { memory.grow(1); once = true; }
        const imageLevel = new Uint8Array(memory.buffer)[2048];
        if (imageLevel) level = imageLevel / 100;
        new Float32Array(memory.buffer, 4096, b.maxFrames).fill(level, 0, n);
        x.frames.push(n);
      }, frames: [], ring: [], drains: 0, faulted: 0, polls: 0,
    };
    // The optional msg_fault export: true reports `faulted`, a function replaces it, anything else is passed as given.
    if (poll) x.test_msg_fault = poll === true ? () => { x.polls++; return x.faulted; } : poll;
    reactors.push({x, calls});
    return {exports: x};
  }};
  const context = {sampleRate: rate, performance, WebAssembly: wasm, Uint8Array, Uint32Array, Float32Array,
    ArrayBuffer, DataView, Date, Math, Number, Error, Promise, String,
    AudioWorkletProcessor: class {constructor() {this.port = port;}},
    registerProcessor(name, cls) {assert.equal(name, 'gosx-wasm-audio'); Processor = cls;}};
  vm.runInNewContext(source, context);
  const p = new Processor({processorOptions: {m: {}, backend: b, info: {tempoMilli: tempo, requiredCapabilities: required}}});
  for (let i = 0; i < 10 && !messages.some(m => m.t === 'r' || m.t === 'e'); i++) await tick();
  const send = data => port.onmessage?.({data});
  const render = frames => {const output = new Float32Array(frames); const alive = p.process([], [[output]]); return {output, alive};};
  return {p, port, messages, reactors, send, render, isClosed: () => closed};
}

// A scripted page for the message path. The engine emits [1, seq low, seq high, 0] and a fault is
// [7, 0, code low, code high]; fault() queues that record and sets the state a msg_fault export reports.
// With options.ring the engine's ring holds that many records: the first record that does not fit ends it
// with an overflow fault (code 11), and a faulted engine emits nothing more.
// page() decodes each batch into `seen` and gives the buffer back unless release is false, as a stalled
// main thread would. drain() is one drain: eight callbacks. It returns the last block rendered.
async function pager(options) {
  const f = await fixture(options), x = f.reactors[0].x, seen = [], held = [];
  let sent = 0, read = 0;
  return {f, x, seen,
    emit(count) {
      for (let i = 0; i < count && !x.faulted; i++) {
        if (options?.ring && x.ring.length >= options.ring) this.fault(11);
        else x.ring.push([1, sent & 255, sent >> 8, 0]), sent++;
      }
    },
    fault(code) { x.ring.push([7, 0, code & 255, code >> 8]); x.faulted = 0x10000 | code; },
    drain() { let out; for (let i = 0; i < 8; i++) out = f.render(128).output; return out; },
    page(release = true) {
      for (;;) {
        for (; read < f.messages.length; read++) {
          const m = f.messages[read];
          if (m.t !== 'm') continue;
          const bytes = new Uint8Array(m.bytes, 0, m.n);
          for (let at = 0; at < m.n; at += 4) {
            const fault = bytes[at] === 7;
            seen.push(fault ? 'fault ' + (bytes[at + 2] | bytes[at + 3] << 8) : bytes[at + 1] | bytes[at + 2] << 8);
          }
          held.push(m.bytes);
        }
        if (!release || !held.length) return;
        f.send({t: 'b', bytes: held.shift()});
      }
    },
  };
}

test('a mono backend renders variable quanta and refreshes grown memory', async () => {
  const f = await fixture({grow: true});
  assert.ok(f.messages.some(m => m.t === 'r'));
  const result = f.render(192);
  assert.ok(result.output.every(v => v === .25));
  assert.deepEqual(f.reactors[0].x.frames, [64, 64, 64]);
  const commands = new Uint8Array(16); commands[0] = 42;
  f.send({t: 'c', bytes: commands});
  assert.equal(f.reactors[0].calls[0][0], 42);
  f.send({t: 'close'});
  const stopped = f.render(128);
  assert.equal(stopped.alive, false);
  assert.ok(stopped.output.every(v => v === 0));
  assert.equal(f.isClosed(), true);
});

test('capability and batch validation rejects before writing any commands', async () => {
  const b = {...backend, opcodeCapabilities: [{opcode: 22, mask: 1}]};
  const f = await fixture({b});
  const batch = new Uint8Array(32); batch[0] = 42; batch[16] = 22;
  f.send({t: 'c', bytes: batch});
  f.send({t: 'c', bytes: new Uint8Array(17)});
  assert.equal(f.reactors[0].calls.length, 0);
  assert.equal(f.messages.filter(m => m.t === 'x').length, 2);
  const bad = await fixture({required: 1});
  assert.ok(bad.messages.some(m => m.t === 'e'));
  assert.equal(bad.render(128).alive, false);
});

test('messages transfer and return while faults remain observable under backpressure', async () => {
  const f = await fixture();
  const x = f.reactors[0].x;
  new Uint8Array(x.memory.buffer, 3072, 4).set([1, 2, 3, 4]); x.messages = 1;
  for (let i = 0; i < 8; i++) f.render(128);
  const message = f.messages.find(m => m.t === 'm');
  assert.equal(message.n, 4);
  assert.deepEqual([...new Uint8Array(message.bytes).slice(0, 4)], [1, 2, 3, 4]);
  new Uint8Array(x.memory.buffer, 3072, 4).set([7, 0, 9, 0]); x.messages = 1;
  for (let i = 0; i < 8; i++) f.render(128);
  assert.ok(f.messages.some(m => m.t === 'f' && m.a === 9));
  f.send({t: 'b', bytes: message.bytes});
  assert.ok(!f.messages.some(m => m.t === 'x'));
  assert.ok(f.render(128).output.every(v => v === 0));
});

for (const poll of [false, true]) {
  test(`a page that holds the message buffer through several drains still gets every message once, in order${poll ? ' when the reactor exports msg_fault' : ''}`, async () => {
    const p = await pager({poll});
    p.emit(3); p.drain(); p.page(false);
    assert.deepEqual(p.seen, [0, 1, 2]);
    // The page keeps the buffer through five more drains while the engine keeps emitting.
    for (let i = 0; i < 5; i++) { p.emit(3); p.drain(); p.page(false); }
    assert.deepEqual(p.seen, [0, 1, 2], 'nothing can be delivered while the buffer is out');
    if (poll) assert.ok(p.x.polls > 0, 'a healthy reactor is polled while draining is paused');
    // Returning the buffer sends the parked batch at once. The 12 messages still queued in the engine then
    // follow in two more batches, because one drain moves at most 8 records.
    p.page();
    for (let i = 0; i < 5; i++) { p.drain(); p.page(); }
    assert.deepEqual(p.seen, Array.from({length: 18}, (_, i) => i));
    assert.ok(!p.f.messages.some(m => m.t === 'x' || m.t === 'e' || m.t === 'f'));
  });
}

test('a fault is reported at once while the page holds the buffer and the messages around it are kept', async () => {
  const p = await pager();
  p.emit(1); p.drain(); p.page(false);
  p.emit(1); p.x.ring.push([7, 0, 9, 0]); p.emit(1); p.drain();
  assert.ok(p.f.messages.some(m => m.t === 'f' && m.a === 9), 'a fault must not wait for the page to return the buffer');
  assert.ok(p.f.render(128).output.every(v => v === 0));
  p.page();
  assert.deepEqual(p.seen, [0, 1, 'fault 9', 2]);
});

test('later drains leave messages in the engine ring while the page holds the buffer, and its overflow fault arrives last', async () => {
  const p = await pager();
  p.emit(2); p.drain(); p.page(false);
  for (let i = 0; i < 4; i++) { p.emit(2); p.drain(); }
  // One drain went to the page and one batch is parked; the other six messages are still queued.
  assert.equal(p.x.drains, 2);
  assert.equal(p.x.ring.length, 6);
  // An engine that fills its ring ends it with a fault record, which must not be lost either.
  p.x.ring.push([7, 0, 11, 0]);
  p.page();
  for (let i = 0; i < 3; i++) { p.drain(); p.page(); }
  assert.deepEqual(p.seen, [0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 'fault 11']);
  assert.ok(p.f.render(128).output.every(v => v === 0), 'the fault stops playback');
});

test('without msg_fault, a fault queued after the parked batch is found at the first drain after the buffer returns', async () => {
  const p = await pager();
  p.emit(1); p.drain(); p.page(false);
  p.emit(1); p.drain();
  p.fault(9);
  // The processor has stopped draining and cannot see the record, so it reports nothing and keeps playing.
  for (let i = 0; i < 20; i++) assert.ok(p.drain().some(v => v !== 0), 'playback goes on while the fault is unseen');
  assert.ok(!p.f.messages.some(m => m.t === 'f'));
  p.page();
  assert.deepEqual(p.seen, [0, 1]);
  assert.ok(p.drain().every(v => v === 0), 'the first drain after b finds the fault and stops playback');
  p.page();
  assert.deepEqual(p.seen, [0, 1, 'fault 9']);
});

test('a reactor that exports msg_fault has a later fault reported at once even if the page never returns the buffer', async () => {
  const p = await pager({poll: true});
  p.emit(1); p.drain(); p.page(false);
  p.emit(1); p.drain();
  // The page never returns the buffer. A healthy reactor is polled at every drain point and keeps playing.
  for (let i = 0; i < 50; i++) { p.emit(2); assert.ok(p.drain().some(v => v !== 0), 'a healthy reactor keeps playing'); }
  assert.ok(!p.f.messages.some(m => m.t === 'f'));
  p.fault(9);
  assert.ok(p.drain().every(v => v === 0), 'playback stops at the first drain after the fault');
  assert.deepEqual(p.f.messages.filter(m => m.t === 'f'), [{t: 'f', a: 9}]);
  for (let i = 0; i < 100; i++) assert.ok(p.drain().every(v => v === 0));
  assert.equal(p.f.messages.filter(m => m.t === 'f').length, 1, 'the fault is not repeated');
  assert.equal(p.x.polls, 51, 'polled until it reported the fault, then left alone');
  assert.equal(p.x.drains, 2, 'polling drains nothing while the page holds the buffer');
  assert.equal(p.x.ring.length, 101, '100 messages and the fault record are still queued in the reactor');
  p.page();
  assert.deepEqual(p.seen, [0, 1], 'the parked batch arrives the moment the page returns the buffer');
  // Playback has stopped, but the processor goes on draining the faulted reactor at its drain points (every
  // eighth callback), one batch of 8 for each returned buffer: 13 batches carry the 101 queued records, and
  // the 14th drain point finds the ring empty.
  for (let i = 0; i < 7; i++) p.f.render(128);
  assert.equal(p.x.drains, 2, 'draining waits for the next drain point');
  p.f.render(128);
  assert.equal(p.x.drains, 3);
  p.page();
  for (let i = 0; i < 12; i++) { p.drain(); p.page(); }
  assert.deepEqual(p.seen, [...Array.from({length: 102}, (_, i) => i), 'fault 9'], 'every queued message and the fault record arrive, in order');
  assert.equal(p.x.ring.length, 0);
  assert.equal(p.x.drains, 15);
  assert.ok(p.drain().every(v => v === 0));
  assert.equal(p.x.drains, 16, 'the next drain point finds the ring empty');
  for (let i = 0; i < 20; i++) { assert.ok(p.drain().every(v => v === 0)); p.page(); }
  assert.equal(p.x.drains, 16, 'an empty ring ends the draining');
  assert.equal(p.x.polls, 51, 'the reactor is no longer polled either');
  assert.equal(p.f.messages.filter(m => m.t === 'f').length, 1, 'the fault is reported once as f');
  assert.ok(!p.f.messages.some(m => m.t === 'x' || m.t === 'e'));
});

for (const poll of [false, true]) {
  test(`a ring that fills during a long stall delivers every message before its overflow fault${poll ? ' when the reactor exports msg_fault' : ''}`, async () => {
    const p = await pager({poll, ring: 20});
    p.emit(2); p.drain(); p.page(false);
    p.emit(2); p.drain();
    // The page stalls for 30 drains while the engine emits 3 messages in each. After 20 queued records the
    // ring is full, so the next record ends it with the overflow fault and the engine says nothing more.
    for (let i = 0; i < 30; i++) { p.emit(3); p.drain(); }
    assert.equal(p.x.ring.length, 21, '20 messages and the overflow fault are queued');
    assert.equal(p.x.drains, 2);
    assert.deepEqual(p.seen, [0, 1]);
    assert.deepEqual(p.f.messages.filter(m => m.t === 'f'), poll ? [{t: 'f', a: 11}] : [], poll ? 'the fault is reported while the page stalls' : 'the fault waits in the ring');
    assert.ok(p.f.render(128).output.some(v => v !== 0) !== poll, poll ? 'playback has stopped' : 'playback goes on, as the processor cannot see the fault');
    p.page();
    for (let i = 0; i < 6; i++) { p.drain(); p.page(); }
    assert.deepEqual(p.seen, [...Array.from({length: 24}, (_, i) => i), 'fault 11']);
    assert.equal(p.x.ring.length, 0);
    assert.ok(p.drain().every(v => v === 0), 'the fault stops playback');
    assert.ok(!p.f.messages.some(m => m.t === 'x' || m.t === 'e'));
  });
}

test('a play command after a fault found by msg_fault hands the rest of the ring back to normal draining', async () => {
  const p = await pager({b: {...backend, transport}, poll: true});
  p.f.send({t: 'c', bytes: command(1)});
  p.emit(1); p.drain(); p.page(false);
  p.emit(1); p.drain();
  p.emit(4); p.fault(9); p.drain();
  assert.deepEqual(p.f.messages.filter(m => m.t === 'f'), [{t: 'f', a: 9}]);
  p.page();
  assert.deepEqual(p.seen, [0, 1]);
  assert.equal(p.x.drains, 2);
  p.f.send({t: 'c', bytes: command(1)});
  p.drain(); p.page();
  assert.deepEqual(p.seen, [0, 1, 2, 3, 4, 5, 'fault 9'], 'restarted playback drains what was left, once and in order');
  assert.equal(p.x.drains, 3);
  for (let i = 0; i < 10; i++) { p.drain(); p.page(); }
  assert.equal(p.x.drains, 3, 'the fault record in a drained batch stops playback and the draining, as before');
  assert.equal(p.f.messages.filter(m => m.t === 'f').length, 1, 'the delivered fault is not reported again');
  assert.ok(!p.f.messages.some(m => m.t === 'x' || m.t === 'e'));
});

test('an image replacement after a fault found by msg_fault leaves the faulted reactor and its queue behind', async () => {
  const p = await pager({b: {...backend, transport}, poll: true});
  p.f.send({t: 'c', bytes: command(1)});
  p.emit(1); p.drain(); p.page(false);
  p.emit(1); p.drain();
  p.emit(4); p.fault(9); p.drain();
  p.f.send({t: 'i', i: new Uint8Array([75]).buffer, info: {tempoMilli: 120000, requiredCapabilities: 0}, r: 'next'});
  for (let i = 0; i < 10 && !p.f.messages.some(m => m.t === 't'); i++) await tick();
  assert.ok(p.f.messages.some(m => m.t === 't'), 'the replacement is installed at once because playback has stopped');
  p.page();
  for (let i = 0; i < 10; i++) { p.drain(); p.page(); }
  assert.deepEqual(p.seen, [0, 1], 'the parked batch was already drained; the five records still queued in the retired reactor are discarded');
  assert.equal(p.x.ring.length, 5);
  assert.equal(p.x.drains, 2);
  assert.equal(p.f.reactors[1].x.drains, 0, 'the new reactor is not drained on the old one\'s behalf');
  assert.ok(!p.f.messages.some(m => m.t === 'x' || m.t === 'e'));
});

test('msg_fault must be a function and report 0 or 0x10000 plus a 16-bit argument', async () => {
  const bad = await fixture({poll: 1});
  assert.ok(bad.messages.some(m => m.t === 'e' && /invalid msg_fault export/.test(m.e)));
  for (const state of [-1, 9, 0xffff, 0x20000, 1.5, '65545']) {
    const p = await pager({poll: () => state});
    p.emit(1); p.drain(); p.page(false);
    p.emit(1); p.drain();
    p.drain();
    assert.ok(p.f.messages.some(m => m.t === 'e' && /invalid fault state/.test(m.e)), String(state));
    assert.ok(!p.f.messages.some(m => m.t === 'f'));
  }
  const ok = await pager({poll: () => 0x10000});
  ok.emit(1); ok.drain(); ok.page(false);
  ok.emit(1); ok.drain(); ok.drain();
  assert.deepEqual(ok.f.messages.filter(m => m.t === 'f'), [{t: 'f', a: 0}], 'a fault argument of 0 is still a fault');
});

test('a render fault silences the complete block and stops the reactor once', async () => {
  const f = await fixture({failRender: true});
  const result = f.render(128);
  assert.equal(result.alive, false);
  assert.ok(result.output.every(v => v === 0));
  f.render(128);
  assert.equal(f.messages.filter(m => m.t === 'e').length, 1);
});

test('image replacement waits for a musical boundary and crossfades without a gap', async () => {
  const f = await fixture({b: {...backend, transport}});
  f.send({t: 'c', bytes: command(1)});
  const image = new Uint8Array([75]);
  f.send({t: 'i', i: image.buffer, info: {tempoMilli: 120000, requiredCapabilities: 0}, r: 'next'});
  for (let i = 0; i < 10 && !f.messages.some(m => m.t === 't'); i++) await tick();
  assert.ok(f.messages.some(m => m.t === 't'));
  f.send({t: 'c', bytes: command(9)});
  const result = f.render(512).output;
  assert.ok(result.slice(0, 500).every(v => v === .25));
  assert.ok(result[500] > .25 && result[500] < .75);
  assert.equal(result[511], .75);
  assert.equal(f.reactors[1].calls[0][0], 3);
  assert.equal(f.reactors[1].calls[0][16], 1);
  assert.equal(new DataView(f.reactors[1].calls[1].buffer).getBigUint64(8, true), 2n);
});

// Known limit, pinned until a follow-up drains a retired reactor: an image replacement retires the old reactor
// at a bar boundary, and what it still has queued never reaches the page. The processor drains the retired
// reactor only at a drain point inside the 5 ms crossfade, and then for faults only. At 1 kHz a bar of
// 120000 milli-BPM is 500 samples, so the swap falls in the fourth callback and the crossfade ends before
// the eighth callback's drain point; 58823 milli-BPM makes the bar 1021 samples, so the swap is 3 samples
// before the end of the eighth callback and that drain point falls inside the crossfade.
for (const [name, tempo, drains] of [['no drain point falls inside the crossfade', 120000, 0], ['a drain point falls inside the crossfade', 58823, 1]]) {
  test(`an image replacement discards what its retired reactor still has queued: ${name}`, async () => {
    const p = await pager({b: {...backend, transport}, tempo});
    p.f.send({t: 'c', bytes: command(1)});
    p.emit(2); p.fault(9);
    p.f.send({t: 'i', i: new Uint8Array([75]).buffer, info: {tempoMilli: tempo, requiredCapabilities: 0}, r: 'next'});
    for (let i = 0; i < 10 && !p.f.messages.some(m => m.t === 't'); i++) await tick();
    assert.ok(p.f.messages.some(m => m.t === 't'));
    p.f.reactors[1].x.ring.push([1, 100, 0, 0], [1, 101, 0, 0]);
    for (let i = 0; i < 6; i++) { p.drain(); p.page(); }
    assert.deepEqual(p.seen, [100, 101], 'only the new reactor\'s messages reach the page');
    assert.equal(p.x.drains, drains);
    assert.equal(p.x.ring.length, drains ? 0 : 3, drains ? 'the retired ring was drained and its messages thrown away' : 'the retired ring was never drained');
    assert.deepEqual(p.f.messages.filter(m => m.t === 'f'), drains ? [{t: 'f', a: 9}] : [], 'a drain of the retired reactor reports its fault only');
    assert.ok(!p.f.messages.some(m => m.t === 'x' || m.t === 'e'));
  });
}
