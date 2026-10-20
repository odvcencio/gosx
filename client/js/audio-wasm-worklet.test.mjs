// Exercise the reusable reactor host with a non-Cicada mono backend, unusual
// buffer sizes, memory growth, musical replacement, and real buffer transfers.
import {test} from 'node:test';
import assert from 'node:assert/strict';
import {readFileSync} from 'node:fs';
import vm from 'node:vm';

const source = readFileSync(new URL('../../game/audio/worklet/processor.js', import.meta.url), 'utf8');
const tick = () => new Promise(resolve => setImmediate(resolve));
const backend = {prefix: 'test_', maxFrames: 64, channels: 1, commandBytes: 16,
  messageBytes: 4, messageCapacity: 8, supportedCapabilities: 3, faultOpcode: 7, faultArgumentOffset: 2};

async function fixture({b = backend, rate = 1000, capabilities = 0, grow = false, required = 0, failRender = false} = {}) {
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
      test_msg_ptr() { return 3072; }, test_msg_drain() { const n = x.messages || 0; x.messages = 0; return n; },
      test_cmd_commit(n) { calls.push(new Uint8Array(memory.buffer, 1024, n * b.commandBytes).slice()); return 0; },
      test_render(n) {
        if (failRender) throw new Error('render failed');
        if (grow && !once) { memory.grow(1); once = true; }
        const imageLevel = new Uint8Array(memory.buffer)[2048];
        if (imageLevel) level = imageLevel / 100;
        new Float32Array(memory.buffer, 4096, b.maxFrames).fill(level, 0, n);
        x.frames.push(n);
      }, frames: [],
    };
    reactors.push({x, calls});
    return {exports: x};
  }};
  const context = {sampleRate: rate, performance, WebAssembly: wasm, Uint8Array, Uint32Array, Float32Array,
    ArrayBuffer, DataView, Date, Math, Number, Error, Promise, String,
    AudioWorkletProcessor: class {constructor() {this.port = port;}},
    registerProcessor(name, cls) {assert.equal(name, 'gosx-wasm-audio'); Processor = cls;}};
  vm.runInNewContext(source, context);
  const p = new Processor({processorOptions: {m: {}, backend: b, info: {tempoMilli: 120000, requiredCapabilities: required}}});
  for (let i = 0; i < 10 && !messages.some(m => m.t === 'r' || m.t === 'e'); i++) await tick();
  const send = data => port.onmessage?.({data});
  const render = frames => {const output = new Float32Array(frames); const alive = p.process([], [[output]]); return {output, alive};};
  return {p, port, messages, reactors, send, render, isClosed: () => closed};
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

test('a render fault silences the complete block and stops the reactor once', async () => {
  const f = await fixture({failRender: true});
  const result = f.render(128);
  assert.equal(result.alive, false);
  assert.ok(result.output.every(v => v === 0));
  f.render(128);
  assert.equal(f.messages.filter(m => m.t === 'e').length, 1);
});

test('image replacement waits for a musical boundary and crossfades without a gap', async () => {
  const b = {...backend, transport: {play: 1, stop: 2, seek: 3, target: 255, ticksPerBeat: 1,
    ticksPerBar: 1, seekBarOffset: 4, seekTickOffset: 8, boundaryTickOffset: 8, quantizedOpcodes: [9]}};
  const f = await fixture({b});
  const play = new Uint8Array(16); play[0] = 1;
  f.send({t: 'c', bytes: play});
  const image = new Uint8Array([75]);
  f.send({t: 'i', i: image.buffer, info: {tempoMilli: 120000, requiredCapabilities: 0}, r: 'next'});
  for (let i = 0; i < 10 && !f.messages.some(m => m.t === 't'); i++) await tick();
  assert.ok(f.messages.some(m => m.t === 't'));
  const launch = new Uint8Array(16); launch[0] = 9;
  f.send({t: 'c', bytes: launch});
  const result = f.render(512).output;
  assert.ok(result.slice(0, 500).every(v => v === .25));
  assert.ok(result[500] > .25 && result[500] < .75);
  assert.equal(result[511], .75);
  assert.equal(f.reactors[1].calls[0][0], 3);
  assert.equal(f.reactors[1].calls[0][16], 1);
  assert.equal(new DataView(f.reactors[1].calls[1].buffer).getBigUint64(8, true), 2n);
});
