import test from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import vm from 'node:vm';
import { webcrypto, createHash } from 'node:crypto';

const driver = readFileSync(new URL('../gameboy/assets/runtime.js', import.meta.url), 'utf8');
const source = driver.replace(/const runtimeURL=[^\n]+\n/, '').replace(/const \{default:init[^\n]+\n/, '');
async function handheld({storage=new Map(),embedded=false,quota=false}={}) {
  const element = () => ({ dataset: {}, value: '3', listeners: {}, addEventListener(name, fn) { this.listeners[name] = fn; }, setAttribute() {}, focus() {} });
  const canvas = element();
  canvas.getContext = () => ({ createImageData: () => ({ data: new Uint8ClampedArray(160 * 144 * 4) }), putImageData() {} });
  const controls = Object.fromEntries(['data-status', 'data-sound', 'data-speed', 'data-rom', 'data-import-save', 'data-save', 'data-screen-prompt', 'data-hint'].map(key => [key, element()]));
  const document = { body: { dataset: {} }, hidden: false, listeners: {}, querySelector: selector => selector === 'canvas' ? canvas : controls[selector.slice(1, -1)], querySelectorAll: () => [], addEventListener(name, fn) { this.listeners[name] = fn; } };
  let rafID = 0;
  const frames = new Map(), sources = [];
  class GameBoy {
    static verify_rom() { return true; }
    constructor() { this.frames = 0; this.keys = new Set(); this.inputs = []; }
    load_unsafe() {} load_rom_wa() { return { free() {} }; } load_boot_state() {} free() {}
    next_frame() { this.frames++; this.inputs.push([...this.keys]); } ppu_frame() { return this.frames; }
    frame_buffer_eager() { return new Uint8Array(160 * 144 * 4); }
    ram_data_eager() { return this.ram || new Uint8Array(8); } set_ram_data(data) { this.ram=new Uint8Array(data); } key_press(key) { this.keys.add(key); } key_lift(key) { this.keys.delete(key); }
    audio_buffer_eager() { return new Int16Array(960); } audio_channels() { return 2; } audio_sampling_rate() { return 48000; }
  }
  const StateManager={
    save_wa(gb) {const data=new Uint8Array(8);data.set([66,79,83,67]);new DataView(data.buffer).setUint32(4,gb.frames,true);return data;},
    format_wa(data) {return data[0]===66?1:2;},
    validate_wa(data) {if(data.length!==8||data[0]!==66)throw Error('Corrupt checkpoint');},
    load_wa(data,gb) {gb.frames=new DataView(data.buffer,data.byteOffset,data.byteLength).getUint32(4,true);},
  };
  const messages=[],window=element(),parent=embedded?{postMessage:data=>messages.push(data)}:window;window.postMessage=data=>messages.push(data);
  const localStorage={getItem:key=>storage.get(key)||null,setItem:(key,value)=>{if(quota&&key.includes('pocket-state'))throw Error('Quota');storage.set(key,value);},removeItem:key=>storage.delete(key)};
  class AudioContext {
    state = 'running'; currentTime = 1; destination = {};
    async resume() { this.state = 'running'; } async suspend() { this.state = 'suspended'; }
    createBuffer(channels, count, rate) { return { duration: count / rate, getChannelData: () => new Float32Array(count) }; }
    createBufferSource() {
      const s = { playbackRate: { value: 1 }, connect() {}, start(at) { this.at = at; }, stop() { this.stopped = true; } }; sources.push(s); return s;
    }
  }
  const context = vm.createContext({ document, window, parent, localStorage, btoa, atob, StateManager,SaveStateFormat:{Bosc:1},StateFormat:{Full:3},FromGbOptions:{new:()=>({})}, location: { origin: 'https://coast.test' }, init: async () => {}, GameBoy, GameBoyMode: { Dmg: 0, Cgb: 1 }, PadKey: { A: 1, Start: 2 }, AudioContext, crypto: webcrypto,
    requestAnimationFrame: fn => { frames.set(++rafID, fn); return rafID; }, cancelAnimationFrame: id => frames.delete(id), AbortSignal,
    fetch: async url => { assert.equal(url, '/api/blackglass-cartridge', 'empty deployments must not fetch a ROM'); return { ok: true, json: async () => ({ available: false }) }; } });
  const instrumented=source.replace('return {dispose(){', 'globalThis.pocket = {load,play,tick,key,frames:()=>gb?.ppu_frame()||0,inputs:()=>gb.inputs,machine:()=>gb,checkpoint,hash:()=>romHash};return {dispose(){');
  vm.runInContext(instrumented,context);
  await context.window.__gosx_engine_factories.GoSXGameBoy({mount:document,props:{batteryPrefix:'gosx.blackglass.cartridge.',checkpointPrefix:'gosx.blackglass.pocket-state.v1.',protocol:'gosx-handheld'}});
  const rom = new Uint8Array(32768); let checksum = 0; for (let i = 0x134; i < 0x14d; i++) checksum = (checksum - 1) & 255; rom[0x14d] = checksum;
  return { context, controls, canvas, document, sources, frames, rom, storage, messages, window, parent, api: context.pocket };
}

test('empty handheld schedules no emulation and advances the cartridge clock at 3× or 1×', async () => {
  const h = await handheld();
  assert.equal(h.frames.size, 0); assert.equal(h.canvas.dataset.playing, 'false'); assert.equal(h.canvas.dataset.emulationSpeed, '3');
  await h.api.load(h.rom, 'timing-probe.gb');
  for (let i = 0; i <= 60; i++) h.api.tick(100 + i * 1000 / 60);
  const triple = h.api.frames(); assert.ok(triple >= 179 && triple <= 183, `${triple} frames over one second at 3×`);
  h.controls['data-speed'].value = '1'; h.controls['data-speed'].listeners.change();
  for (let i = 0; i <= 60; i++) h.api.tick(2000 + i * 1000 / 60);
  const normal = h.api.frames() - triple; assert.ok(normal >= 59 && normal <= 61, `${normal} frames at 1×`);
  assert.ok(triple / normal > 2.9 && triple / normal < 3.1);
  h.api.play(false); const stopped = h.api.frames(); h.api.tick(50000); assert.equal(h.api.frames(), stopped);
  h.api.play(true); h.api.tick(50000); assert.equal(h.api.frames() - stopped, 1, 'resume must not simulate the time spent paused');
});

test('3× audio consumes its buffers at 3× and changing speed or hiding cancels queued audio', async () => {
  const h = await handheld(); await h.api.load(h.rom, 'audio-probe.gb');
  await h.controls['data-sound'].listeners.click(); h.api.tick(100);
  assert.equal(h.sources.at(-1).playbackRate.value, 3);
  const tripleSource = h.sources.at(-1); h.controls['data-speed'].value = '1'; h.controls['data-speed'].listeners.change();
  assert.equal(tripleSource.stopped, true); h.api.tick(200); assert.equal(h.sources.at(-1).playbackRate.value, 1);
  const normalSource = h.sources.at(-1); h.document.hidden = true; h.document.listeners.visibilitychange();
  assert.equal(normalSource.stopped, true); const stopped = h.api.frames(); h.api.tick(50000); assert.equal(h.api.frames(), stopped);
  h.document.hidden = false; h.document.listeners.visibilitychange(); h.api.tick(50000); assert.equal(h.api.frames() - stopped, 1);
});
test('brief taps reach a cartridge frame and pause clears held or queued inputs', async () => {
  const h = await handheld(); await h.api.load(h.rom, 'input-probe.gb');
  h.api.key('Start', true); h.api.key('Start', false); h.api.tick(100);
  assert.deepEqual(Array.from(h.api.inputs()[0]), [2]);
  assert.deepEqual(Array.from(h.api.inputs()[1]), []);
  h.api.key('A', true); h.api.key('A', false); h.api.play(false); h.api.play(true); h.api.tick(5000);
  assert.deepEqual(Array.from(h.api.inputs().at(-1)), [], 'putting away must release pending taps');
});


test('putting away checkpoints the full machine and repeated pickup keeps the same emulator',async()=>{
 const h=await handheld();await h.api.load(h.rom,'yellow-probe.gb');for(let i=0;i<12;i++)h.api.tick(100+i*17);
 const machine=h.api.machine(),position=h.api.frames();h.api.play(false);
 const stored=JSON.parse(h.storage.get('gosx.blackglass.pocket-state.v1.'+h.api.hash()));assert.equal(stored.romHash,h.api.hash());assert.equal(stored.speed,3);
 assert.equal(h.canvas.dataset.checkpoint,'saved');h.api.tick(90000);assert.equal(h.api.frames(),position);
 for(let i=0;i<3;i++){h.api.play(true);h.api.tick(100000+i*1000);assert.equal(h.api.machine(),machine);h.api.play(false);}
 const saved=h.api.frames(),fresh=await handheld({storage:h.storage});await fresh.api.load(fresh.rom,'yellow-probe.gb');
 assert.equal(fresh.api.frames(),saved);assert.equal(fresh.canvas.dataset.checkpoint,'restored');assert.match(fresh.controls['data-status'].textContent,/checkpoint restored/);
});

test('a closed pocket cannot start a late cartridge load, and messages require its actual parent origin',async()=>{
 const h=await handheld({embedded:true});assert.ok(h.messages.some(m=>m.type==='gosx-handheld-ready'));
 await h.api.load(h.rom,'late-probe.gb');assert.equal(h.frames.size,0);assert.equal(h.canvas.dataset.playing,'false');
 const message=h.window.listeners.message;
 message({origin:'https://other.test',source:h.parent,data:{type:'gosx-handheld',action:'resume'}});assert.equal(h.frames.size,0);
 message({origin:'https://coast.test',source:{},data:{type:'gosx-handheld',action:'resume'}});assert.equal(h.frames.size,0);
 message({origin:'https://coast.test',source:h.parent,data:{type:'gosx-handheld',action:'resume'}});assert.equal(h.canvas.dataset.playing,'true');
});

test('checkpoints are cartridge-specific and corrupt state falls back to the battery save',async()=>{
 const h=await handheld();await h.api.load(h.rom,'first.gb');h.api.tick(100);h.api.play(false);const hash=h.api.hash();
 const other=new Uint8Array(h.rom);other[0x150]=1;const fresh=await handheld({storage:h.storage});await fresh.api.load(other,'other.gb');assert.equal(fresh.api.frames(),0);
 h.storage.set('gosx.blackglass.pocket-state.v1.'+hash,'{"version":1,"core":"boytacean-0.13.2","romHash":"'+hash+'","data":"broken"}');
 const fallback=await handheld({storage:h.storage});await fallback.api.load(h.rom,'first.gb');assert.equal(fallback.api.frames(),0);assert.equal(fallback.canvas.dataset.checkpoint,'invalid');
 assert.match(fallback.controls['data-status'].textContent,/battery save loaded/);fallback.api.tick(100);assert.ok(fallback.api.frames()>0);
});

test('storage quota leaves the previous checkpoint intact and live pocket play still resumes',async()=>{
 const h=await handheld();await h.api.load(h.rom,'quota.gb');h.api.tick(100);h.api.play(false);const previous=h.storage.get('gosx.blackglass.pocket-state.v1.'+h.api.hash());
 const full=await handheld({storage:h.storage,quota:true});await full.api.load(full.rom,'quota.gb');full.api.tick(100);full.api.play(false);
 assert.equal(full.storage.get('gosx.blackglass.pocket-state.v1.'+full.api.hash()),previous);assert.equal(full.canvas.dataset.checkpoint,'unavailable');
 const at=full.api.frames();full.api.play(true);full.api.tick(50000);assert.ok(full.api.frames()>at);
});

test('importing a battery save invalidates the old exact-state checkpoint',async()=>{
 const h=await handheld();await h.api.load(h.rom,'import.gb');for(let i=0;i<8;i++)h.api.tick(100+i*17);h.api.play(false);
 const key='gosx.blackglass.pocket-state.v1.'+h.api.hash();assert.ok(h.storage.has(key));
 const ram=new Uint8Array(8).fill(7);await h.api.load(h.rom,'import.gb',ram);
 assert.equal(h.api.frames(),0);assert.equal(h.storage.has(key),false);assert.deepEqual(Array.from(h.api.machine().ram_data_eager()),Array.from(ram));
});
