// GoSX WASM audio reactor host. Backend data defines record/transport layouts;
// project formats, instruments and synthesis remain inside the backend.
class WASMAudioProcessor extends AudioWorkletProcessor {
  constructor({processorOptions: o}) {
    super();
    this.o = o;
    this.b = o.backend;
    this.t = this.b.transport;
    this.active = this.previous = this.pending = this.bankCopy = null;
    this.bank = null;
    this.closed = this.playing = false;
    this.deferredCount = this.engineSample = this.anchorSample = this.anchorTick = 0;
    this.nextBarTick = this.t?.ticksPerBar || 0;
    this.fadeTotal = Math.max(1, Math.floor(sampleRate / 200));
    this.fadeLeft = 0;
    this.callbacks = this.underruns = this.lastStart = 0;
    this.histogram = new Uint32Array(256);
    this.preciseClock = !!globalThis.performance?.now;
    this.clock = this.preciseClock ? performance.now.bind(performance) : Date.now;
    this.durationLimit = 128000 / sampleRate + (this.preciseClock ? .1 : 1);
    this.latencyMS = o.l || 0;
    this.gapLimit = this.latencyMS + this.durationLimit;
    this.messageBuffer = new ArrayBuffer(this.b.messageBytes * this.b.messageCapacity);
    this.messageView = new Uint8Array(this.messageBuffer);
    this.message = {t: 'm', bytes: this.messageBuffer, n: 0};
    this.transfer = [this.messageBuffer];
    this.messageReady = true;
    this.faultMessage = {t: 'f', a: 0};
    this.quantized = new Uint8Array(256);
    this.required = new Uint32Array(256);
    for (const rule of this.b.opcodeCapabilities || []) this.required[rule.opcode] = rule.mask;
    if (this.t) {
      for (const op of this.t.quantizedOpcodes || []) this.quantized[op] = 1;
      this.swap = new Uint8Array(this.b.commandBytes * 2);
      // Seek precedes play, including for reactors that start with a seek fade.
      this.swap[0] = this.t.seek;
      this.swap[this.b.commandBytes] = this.t.play;
      this.swap[1] = this.swap[this.b.commandBytes + 1] = this.t.target;
      this.swapView = new DataView(this.swap.buffer);
    }
    this.port.onmessage = event => {
      if (!this.closed) {
        try { this.receive(event.data); }
        catch (error) { this.fail(error); }
      }
    };
    this.create(o.i, o.info).then(next => {
      if (this.closed) return;
      this.active = next;
      this.bpm = next.tempo;
      this.playing = !this.t;
      this.post({t: 'r', r: o.r, mem: next.x.memory.buffer.byteLength, c: this.preciseClock, p: next.capabilities});
    }, error => this.fail(error));
  }

  post(message, transfer) { if (!this.closed) this.port.postMessage(message, transfer); }
  reject(e, r) { this.post({t: 'x', e, r}); }
  fail(error) {
    if (this.closed) return;
    this.post({t: 'e', e: String(error)});
    this.shutdown();
  }
  shutdown() {
    this.closed = true;
    this.playing = false;
    this.active = this.previous = this.pending = null;
    if (this.bankCopy) { this.bankCopy.reject(new Error('audio: processor closed')); this.bankCopy = null; }
    this.port.onmessage = null;
    this.port.close();
  }

  async create(image, info) {
    const b = this.b;
    if (!info || !Number.isInteger(info.requiredCapabilities) || info.requiredCapabilities < 0 || info.requiredCapabilities > 0xffffffff ||
        (info.requiredCapabilities & ~b.supportedCapabilities) || this.t && !(info.tempoMilli > 0)) {
      throw new Error('audio: invalid image metadata');
    }
    const instance = await WebAssembly.instantiate(this.o.m);
    if (this.closed) throw new Error('audio: processor closed');
    const exports = instance.exports, x = {};
    if (typeof exports._initialize !== 'function') throw new Error('audio: reactor initializer missing');
    exports._initialize();
    const prefix = b.prefix || 'gosx_audio_';
    x.memory = exports.memory;
    for (const name of ['init', 'render', 'out_ptr', 'cmd_ptr', 'cmd_cap', 'cmd_commit', 'msg_ptr', 'msg_drain',
      'capabilities', 'project_alloc', 'bank_alloc', 'bank_install', 'bank_image_ptr', 'bank_image_len']) {
      x[name] = exports[prefix + name];
    }
    for (const name of ['init', 'render', 'out_ptr', 'cmd_ptr', 'cmd_cap', 'cmd_commit', 'msg_ptr', 'msg_drain']) {
      if (typeof x[name] !== 'function') throw new Error('audio: missing export ' + prefix + name);
    }
    if (!(x.memory instanceof WebAssembly.Memory)) throw new Error('audio: reactor memory missing');
    if (x.capabilities !== undefined && typeof x.capabilities !== 'function') throw new Error('audio: invalid capabilities export');
    const capabilities = x.capabilities ? x.capabilities() : 0;
    if (!Number.isInteger(capabilities) || capabilities < 0 || capabilities > 0xffffffff ||
        (capabilities & ~b.supportedCapabilities) || ((capabilities & info.requiredCapabilities) >>> 0) !== info.requiredCapabilities) {
      throw new Error('audio: unsupported reactor capabilities');
    }
    if (b.sharedBank) {
      for (const name of ['bank_alloc', 'bank_install', 'bank_image_ptr', 'bank_image_len']) {
        if (typeof x[name] !== 'function') throw new Error('audio: missing bank export ' + name);
      }
      if (this.bank) {
        const ptr = x.bank_alloc(this.bank.byteLength);
        if (!ptr) throw new Error('audio: bank allocation failed');
        const source = new Uint8Array(this.bank);
        const destination = new Uint8Array(x.memory.buffer, ptr, source.byteLength);
        const chunks = [];
        for (let at = 0; at < source.length; at += 65536) {
          const end = Math.min(at + 65536, source.length);
          chunks.push([source.subarray(at, end), destination.subarray(at, end)]);
        }
        await new Promise((resolve, reject) => { this.bankCopy = {chunks, offset: 0, resolve, reject}; });
        if (this.closed) throw new Error('audio: processor closed');
        if (x.bank_install(sampleRate) !== 0) throw new Error('audio: bank installation failed');
      }
    }
    if (image) {
      if (typeof x.project_alloc !== 'function') throw new Error('audio: image upload unsupported');
      const bytes = new Uint8Array(image), ptr = x.project_alloc(bytes.length);
      if (!ptr) throw new Error('audio: image allocation failed');
      new Uint8Array(x.memory.buffer, ptr, bytes.length).set(bytes);
    }
    if (x.init(sampleRate, b.maxFrames, b.channels) !== 0) throw new Error('audio: reactor initialization failed');
    if (b.sharedBank && !this.bank) {
      const ptr = x.bank_image_ptr(), length = x.bank_image_len();
      if (!ptr || !length) throw new Error('audio: bank export failed');
      this.bank = new Uint8Array(x.memory.buffer, ptr, length).slice().buffer;
    }
    const capacity = x.cmd_cap();
    if (!Number.isInteger(capacity) || capacity < 1 || capacity > 4096) throw new Error('audio: invalid command capacity');
    if (this.t && capacity < 2 || this.deferred && capacity * b.commandBytes !== this.deferred.length) throw new Error('audio: incompatible command capacity');
    const next = {x, capabilities, tempo: info.tempoMilli, capacity, buffer: null, channels: new Array(b.channels)};
    this.refresh(next);
    if (!this.deferred) this.deferred = new Uint8Array(next.commands.length);
    return next;
  }

  refresh(target) {
    const buffer = target.x.memory.buffer;
    if (target.buffer === buffer) return;
    const b = this.b;
    target.commands = new Uint8Array(buffer, target.x.cmd_ptr(), target.capacity * b.commandBytes);
    target.messages = new Uint8Array(buffer, target.x.msg_ptr(), b.messageCapacity * b.messageBytes);
    const ptr = target.x.out_ptr();
    for (let c = 0; c < b.channels; c++) target.channels[c] = new Float32Array(buffer, ptr + c * b.maxFrames * 4, b.maxFrames);
    target.buffer = buffer;
  }

  receive(data) {
    switch (data.t) {
      case 'close': this.shutdown(); break;
      case 'b':
        if (!(data.bytes instanceof ArrayBuffer) || data.bytes.byteLength !== this.b.messageBytes * this.b.messageCapacity) return this.reject('audio: invalid message buffer');
        if (this.messageReady) return this.reject('audio: unexpected message buffer');
        this.messageBuffer = data.bytes;
        this.messageView = new Uint8Array(data.bytes);
        this.message.bytes = this.transfer[0] = data.bytes;
        this.messageReady = true;
        break;
      case 'c': this.commit(this.active, data.bytes); break;
      case 'i': {
        if (this.pending === true) return this.reject('audio: image load in progress', data.r);
        const staged = this.pending;
        this.pending = true;
        this.create(data.i, data.info).then(next => {
          if (this.closed) return;
          if (this.playing && this.t) this.pending = next;
          else this.promote(next);
          this.post({t: 't', r: data.r});
        }, error => {
          if (this.closed) return;
          this.pending = staged;
          if (!this.pending) this.flush();
          this.reject(String(error), data.r);
        });
        break;
      }
      case 'p': this.histogram.fill(0); break;
      case 'q':
        this.latencyMS = Number.isFinite(data.l) && data.l >= 0 ? data.l : 0;
        this.gapLimit = this.latencyMS + this.durationLimit;
        this.post({t: 'q', u: this.underruns, dl: this.durationLimit, gl: this.gapLimit,
          m: this.active?.x.memory.buffer.byteLength || 0, d: this.histogram.slice()});
        break;
    }
  }

  putTick(bytes, at, tick) {
    for (let j = 0; j < 8; j++) { bytes[at + j] = tick; tick = Math.floor(tick / 256); }
  }

  commit(target, bytes, start = 0, length = bytes?.byteLength) {
    const size = this.b.commandBytes;
    if (target) this.refresh(target);
    if (!target || !(bytes instanceof Uint8Array) || !Number.isInteger(length) || length < 0 || length % size ||
        length > target.commands.length || start < 0 || start + length > bytes.byteLength) return this.reject('audio: invalid command batch');
    const count = length / size;
    let queued = 0;
    for (let i = 0; i < count; i++) {
      const op = bytes[start + i * size], mask = this.required[op];
      if (((target.capabilities & mask) >>> 0) !== mask) return this.reject('audio: unsupported command opcode ' + op);
      if (this.pending && this.quantized[op]) queued++;
    }
    if (this.deferredCount + queued > this.deferred.byteLength / size) return this.reject('audio: command queue full');
    let seekTick = null, starts = false, stops = false, committed = 0;
    for (let i = 0; i < count; i++) {
      const at = start + i * size, op = bytes[at];
      if (this.t && target === this.active) {
        if (op === this.t.seek) {
          const u32 = offset => (bytes[offset] | bytes[offset + 1] << 8 | bytes[offset + 2] << 16 | bytes[offset + 3] << 24) >>> 0;
          seekTick = u32(at + this.t.seekBarOffset) * this.t.ticksPerBar + u32(at + this.t.seekTickOffset);
        }
        starts ||= op === this.t.play;
        stops ||= op === this.t.stop;
      }
      const launch = this.quantized[op], queue = launch && this.pending;
      const destination = queue ? this.deferred : target.commands;
      const to = (queue ? this.deferredCount++ : committed++) * size;
      for (let j = 0; j < size; j++) destination[to + j] = bytes[at + j];
      if (launch && !queue) this.putTick(destination, to + this.t.boundaryTickOffset, this.nextBarTick);
    }
    if (committed) target.x.cmd_commit(committed);
    if (target === this.active && this.t) {
      if (seekTick !== null) {
        this.anchorSample = this.engineSample;
        this.anchorTick = seekTick;
        this.nextBarTick = (Math.floor(seekTick / this.t.ticksPerBar) + 1) * this.t.ticksPerBar;
        if (starts) { this.previous = null; this.fadeLeft = this.fadeTotal; }
      }
      if (starts) { this.playing = true; this.post({t: 's', p: true}); }
      if (stops) {
        this.playing = false;
        this.post({t: 's', p: false});
        if (this.pending && this.pending !== true) this.promote(this.pending);
      }
    }
  }

  flush() {
    for (let i = 0; i < this.deferredCount; i++) this.commit(this.active, this.deferred, i * this.b.commandBytes, this.b.commandBytes);
    this.deferredCount = 0;
  }
  promote(next) {
    this.pending = null;
    this.previous = this.playing ? this.active : null;
    this.fadeLeft = this.previous ? this.fadeTotal : 0;
    this.active = next;
    this.bpm = next.tempo;
    this.engineSample = this.anchorSample = this.anchorTick = 0;
    this.nextBarTick = this.t?.ticksPerBar || 0;
    this.flush();
  }
  sampleAtTick(tick) {
    return this.anchorSample + Math.ceil(Math.max(0, tick - this.anchorTick) * 60000 * sampleRate / (this.bpm * this.t.ticksPerBeat));
  }
  swapAtBoundary(tick) {
    if (!this.pending || this.pending === true) { this.nextBarTick += this.t.ticksPerBar; return; }
    const next = this.pending;
    this.pending = null;
    this.refresh(next);
    this.swapView.setUint32(this.t.seekBarOffset, tick / this.t.ticksPerBar, true);
    next.commands.set(this.swap);
    next.x.cmd_commit(2);
    this.previous = this.active;
    this.active = next;
    this.bpm = next.tempo;
    this.engineSample = this.anchorSample = 0;
    this.anchorTick = tick;
    this.nextBarTick = tick + this.t.ticksPerBar;
    this.fadeLeft = this.fadeTotal;
    for (let i = 0; i < this.deferredCount; i++) {
      const at = i * this.b.commandBytes;
      for (let j = 0; j < this.b.commandBytes; j++) next.commands[at + j] = this.deferred[at + j];
      this.putTick(next.commands, at + this.t.boundaryTickOffset, this.nextBarTick);
    }
    if (this.deferredCount) next.x.cmd_commit(this.deferredCount);
    this.deferredCount = 0;
  }

  drain(target, active) {
    if (!target) return false;
    this.refresh(target);
    const count = target.x.msg_drain(), size = this.b.messageBytes;
    if (!Number.isInteger(count) || count < 0 || count > this.b.messageCapacity) throw new Error('audio: invalid message count');
    let fault = false, argument = 0;
    for (let i = 0; i < count; i++) {
      if (this.b.faultOpcode && target.messages[i * size] === this.b.faultOpcode) {
        fault = true;
        const at = i * size + this.b.faultArgumentOffset;
        argument = target.messages[at] | target.messages[at + 1] << 8;
      }
    }
    if (active && this.messageReady && count) {
      const length = count * size;
      for (let i = 0; i < length; i++) this.messageView[i] = target.messages[i];
      this.message.n = length;
      this.messageReady = false;
      this.post(this.message, this.transfer);
    } else if (fault) {
      this.faultMessage.a = argument;
      this.post(this.faultMessage);
    }
    return fault;
  }

  process(inputs, outputs) {
    const channels = outputs[0];
    if (channels) for (let c = 0; c < channels.length; c++) channels[c].fill(0);
    if (this.closed) return false;
    this.durationLimit = (channels?.[0]?.length || 128) * 1000 / sampleRate + (this.preciseClock ? .1 : 1);
    this.gapLimit = this.latencyMS + this.durationLimit;
    const start = this.clock(), gap = this.lastStart ? start - this.lastStart : 0;
    this.lastStart = start;
    this.callbacks++;
    const measuredPlaying = this.playing;
    try {
      if (this.bankCopy) {
        const copy = this.bankCopy, chunk = copy.chunks[copy.offset++];
        // Views are prepared during setup; copying is bounded per block.
        chunk[1].set(chunk[0]);
        if (copy.offset === copy.chunks.length) { this.bankCopy = null; copy.resolve(); }
      }
      if (this.playing && channels) {
        if (channels.length !== this.b.channels) throw new Error('audio: output channel count changed');
        let offset = 0;
        while (offset < channels[0].length) {
          let boundary = this.t ? this.sampleAtTick(this.nextBarTick) : Infinity;
          if (this.t && this.engineSample === boundary) {
            this.swapAtBoundary(this.nextBarTick);
            boundary = this.sampleAtTick(this.nextBarTick);
          }
          const frames = Math.min(this.b.maxFrames, channels[0].length - offset,
            this.pending && this.t ? Math.max(1, boundary - this.engineSample) : Infinity);
          this.active.x.render(frames);
          this.refresh(this.active);
          if (this.previous && this.fadeLeft > 0) { this.previous.x.render(frames); this.refresh(this.previous); }
          for (let i = 0; i < frames; i++) {
            const gain = this.fadeLeft > 0 ? (this.fadeTotal - this.fadeLeft + 1) / this.fadeTotal : 1;
            for (let c = 0; c < channels.length; c++) {
              let value = this.active.channels[c][i];
              if (this.fadeLeft > 0) value = this.previous ? this.previous.channels[c][i] * (1 - gain) + value * gain : value * gain;
              channels[c][offset + i] = value;
            }
            if (this.fadeLeft > 0 && !--this.fadeLeft) this.previous = null;
          }
          this.engineSample += frames;
          offset += frames;
          if (this.t && this.pending && this.engineSample === boundary) this.swapAtBoundary(this.nextBarTick);
          else if (this.t && !this.pending && this.engineSample >= boundary) this.nextBarTick += this.t.ticksPerBar;
        }
        if (!(this.callbacks & 7)) {
          if (this.previous) this.drain(this.previous, false);
          if (this.drain(this.active, true)) {
            this.playing = false;
            for (let c = 0; c < channels.length; c++) channels[c].fill(0);
          }
        }
      }
    } catch (error) {
      if (channels) for (let c = 0; c < channels.length; c++) channels[c].fill(0);
      this.fail(error);
      return false;
    }
    const elapsed = this.clock() - start;
    if (measuredPlaying) {
      this.histogram[Math.min(253, Math.max(0, elapsed * 4 | 0))]++;
      if (elapsed > this.durationLimit) this.histogram[254]++;
      if (gap > this.gapLimit) this.histogram[255]++;
    }
    if (elapsed > this.durationLimit || gap > this.gapLimit) this.underruns++;
    return true;
  }
}
registerProcessor('gosx-wasm-audio', WASMAudioProcessor);
