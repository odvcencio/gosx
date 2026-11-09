// Crowd section state, crowd frames and per-frame selection (lazy chunk
// bootstrap-feature-scene3d-crowd.js).
//
// Section state lives per mount, outside any renderer, so a backend fallback
// keeps what the crowd is doing. Each section is one row of the section
// texture, SCENE_CROWD_SECTION_TEXELS rgba32float texels:
//
//   0  previous action, current action, blend start (scene clock), blend seconds
//   1  excitement before, excitement after, playback rate, flags
//   2  sync epoch (scene clock), sync jitter in cycles, excitement ramp start
//      (scene clock), excitement ramp seconds
//   3..6   shirt palette (linear rgb)
//   7..10  accent palette (linear rgb)
//
// Flags: 1 synchronized to the shared clock, 2 sits out waves, 4 props
// raised, 8 standing section.
//
// A crowd frame (GSC1, scene/crowd_frame.go) carries section changes and waves
// on the sender's clock: a float64 "now", then every time as a float32 offset
// from it. The first frame for a mount fixes the offset to the
// scene clock; later frames keep it unless the two clocks drift apart by more
// than SCENE_CROWD_CLOCK_SLACK seconds (a paused scene clock, for example).
//
// The uniform block is 32 vec4s:
//   0..3   crowd frame to world matrix     4  now, crowd yaw, 0, glow
//   5      legs rgb, skin tone count       6..13 skin tones
//   14..22 clips: start row, segments, cycle seconds, beat
//   23..30 waves: (start, origin turns, speed turns/s, width turns), (laps, 0, 0, 0)
//   31     section count, 0, 0, 0

const SCENE_CROWD_SECTION_TEXELS = 12;
const SCENE_CROWD_UNIFORM_VEC4S = 32;
const SCENE_CROWD_MAX_WAVES = 4;
const SCENE_CROWD_CLOCK_SLACK = 0.25;
const SCENE_CROWD_FLAG_SYNC = 1, SCENE_CROWD_FLAG_NO_WAVE = 2, SCENE_CROWD_FLAG_PROPS = 4, SCENE_CROWD_FLAG_STANDING = 8;

function sceneCrowdLinear(color: string): number[] {
  let hex = typeof color === "string" ? color.trim().replace(/^#/, "") : "";
  if (hex.length === 3 || hex.length === 4) hex = hex.slice(0, 3).split("").map(function(c: string) { return c + c; }).join("");
  const value = /^[0-9a-fA-F]{6}/.test(hex) ? parseInt(hex.slice(0, 6), 16) : 0x808080;
  return [(value >> 16) & 255, (value >> 8) & 255, value & 255].map(function(c: number) {
    const s = c / 255;
    return s <= 0.04045 ? s / 12.92 : Math.pow((s + 0.055) / 1.055, 2.4);
  });
}

// Per-mount state: crowd ID -> { ir, sections, waves, version }.
const sceneCrowdStores = new WeakMap<object, Map<string, any>>();
const sceneCrowdPending = new Map<string, any[]>();
const sceneCrowdClock = new WeakMap<object, any>();

function sceneCrowdMountFor(target: any): any {
  if (target && typeof target === "object" && typeof target.getAttribute === "function") return target;
  const id = typeof target === "string" ? target.trim() : "";
  if (!id || typeof document === "undefined") return null;
  const element = typeof document.getElementById === "function" ? document.getElementById(id) : null;
  if (element) return element;
  const engines = typeof window !== "undefined" && (window as any).__gosx && (window as any).__gosx.engines;
  const engine = engines && typeof engines.get === "function" ? engines.get(id) : null;
  return engine && engine.mount || null;
}

function sceneCrowdStoreMap(mount: any): Map<string, any> {
  let map = sceneCrowdStores.get(mount);
  if (!map) { map = new Map(); sceneCrowdStores.set(mount, map); }
  return map;
}

// sceneCrowdInitialSections packs the declared state of every section.
function sceneCrowdInitialSections(ir: any): Float32Array {
  const sections = ir.sections || [];
  const data = new Float32Array(sections.length * SCENE_CROWD_SECTION_TEXELS * 4);
  for (let i = 0; i < sections.length; i++) {
    const s = sections[i], o = i * SCENE_CROWD_SECTION_TEXELS * 4, action = s.action | 0;
    data.set([action, action, -1e6, 0.4], o);
    const excitement = Number(s.excitement) || 0;
    const flags = SCENE_CROWD_FLAG_PROPS | (s.noWave ? SCENE_CROWD_FLAG_NO_WAVE : 0) | (s.standing ? SCENE_CROWD_FLAG_STANDING : 0);
    data.set([excitement, excitement, 1, flags], o + 4);
    data.set([0, 0.12, -1e6, 0.001], o + 8);
    sceneCrowdWritePalette(data, o, s);
  }
  return data;
}

function sceneCrowdWritePalette(data: Float32Array, o: number, s: any): void {
  const colors = Array.isArray(s.colors) ? s.colors : [], accents = Array.isArray(s.accents) ? s.accents : [];
  for (let k = 0; k < 4; k++) {
    data.set(sceneCrowdLinear(colors[Math.min(k, Math.max(0, colors.length - 1))] || "#888888").concat(1), o + (3 + k) * 4);
    data.set(sceneCrowdLinear(accents[Math.min(k, Math.max(0, accents.length - 1))] || "#cccccc").concat(1), o + (7 + k) * 4);
  }
}

// sceneCrowdStore returns the state for one declared crowd on a mount,
// creating it from the declaration and replaying frames that arrived first.
function sceneCrowdStore(mount: any, ir: any): any {
  const map = sceneCrowdStoreMap(mount);
  let store = map.get(ir.id);
  const ids = (ir.sections || []).map(function(s: any) { return s.id; }).join("\n");
  if (!store || store.ids !== ids) {
    store = { ir, ids, sections: sceneCrowdInitialSections(ir), waves: new Float32Array(SCENE_CROWD_MAX_WAVES * 8), version: 1, lastNow: 0 };
    map.set(ir.id, store);
  } else if (store.ir !== ir) {
    // A redeclared crowd with the same sections keeps its live state; only
    // the palettes follow the new declaration.
    for (let i = 0; i < ir.sections.length; i++) sceneCrowdWritePalette(store.sections, i * SCENE_CROWD_SECTION_TEXELS * 4, ir.sections[i]);
    store.ir = ir; store.version++;
  }
  const key = mount && mount.id;
  const pending = key && sceneCrowdPending.get(key);
  if (pending) {
    sceneCrowdPending.delete(key);
    for (const frame of pending) sceneCrowdApplyDecoded(mount, frame);
  }
  return store;
}

// sceneCrowdDecode parses a GSC1 frame. It checks the whole frame before
// anything changes and throws on a malformed one.
function sceneCrowdDecode(input: any): any {
  const bytes = input instanceof ArrayBuffer ? new Uint8Array(input) : input;
  if (!(bytes instanceof Uint8Array)) throw new TypeError("crowd frame must be an ArrayBuffer or Uint8Array");
  const view = new DataView(bytes.buffer, bytes.byteOffset, bytes.byteLength);
  let at = 0;
  function need(n: number): void { if (at + n > bytes.length) throw new Error("crowd frame is truncated"); }
  need(4);
  if (bytes[0] !== 71 || bytes[1] !== 83 || bytes[2] !== 67 || bytes[3] !== 49) throw new Error("not a GSC1 crowd frame");
  at = 4;
  need(2);
  const idLength = view.getUint16(at, true); at += 2;
  need(idLength);
  const crowd = new TextDecoder().decode(bytes.subarray(at, at + idLength)); at += idLength;
  need(10);
  const sent = view.getFloat64(at, true), now = Number.isFinite(sent) ? sent : 0; at += 8;
  const count = view.getUint16(at, true); at += 2;
  const sections = [];
  for (let i = 0; i < count; i++) {
    need(42);
    const entry = {
      section: view.getUint16(at, true), from: bytes[at + 2], to: bytes[at + 3],
      blendStart: now + view.getFloat32(at + 4, true), blend: view.getFloat32(at + 8, true),
      excitementFrom: view.getFloat32(at + 12, true), excitementTo: view.getFloat32(at + 16, true),
      rate: view.getFloat32(at + 20, true), epoch: now + view.getFloat32(at + 24, true), jitter: view.getFloat32(at + 28, true),
      excitementStart: now + view.getFloat32(at + 32, true), excitementRamp: view.getFloat32(at + 36, true),
      flags: bytes[at + 40],
    };
    at += 42;
    if (entry.from > 8 || entry.to > 8) throw new Error("crowd frame names an unknown action");
    sections.push(entry);
  }
  need(2);
  const waveCount = bytes[at], replace = bytes[at + 1] === 1; at += 2;
  if (waveCount > SCENE_CROWD_MAX_WAVES) throw new Error("crowd frame has more than four waves");
  const waves = [];
  for (let i = 0; i < waveCount; i++) {
    need(20);
    waves.push({ start: now + view.getFloat32(at, true), origin: view.getFloat32(at + 4, true), speed: view.getFloat32(at + 8, true), width: view.getFloat32(at + 12, true), laps: view.getFloat32(at + 16, true) });
    at += 20;
  }
  if (at !== bytes.length) throw new Error("crowd frame has trailing bytes");
  return { crowd, now, sections, waves, replaceWaves: replace };
}

// sceneCrowdOffset maps a sender time to the mount's scene clock.
function sceneCrowdOffset(mount: any, senderNow: number): number {
  const clock = sceneCrowdClock.get(mount) || { scene: 0, offset: null };
  const proposed = clock.scene - senderNow;
  if (clock.offset === null || Math.abs(proposed - clock.offset) > SCENE_CROWD_CLOCK_SLACK) clock.offset = proposed;
  sceneCrowdClock.set(mount, clock);
  return clock.offset;
}

function sceneCrowdApplyDecoded(mount: any, frame: any): boolean {
  const store = sceneCrowdStoreMap(mount).get(frame.crowd);
  if (!store) return false;
  const offset = sceneCrowdOffset(mount, frame.now);
  const n = store.ir.sections.length;
  for (const e of frame.sections) {
    if (e.section >= n) continue;
    const o = e.section * SCENE_CROWD_SECTION_TEXELS * 4, d = store.sections;
    const declared = d[o + 7] & (SCENE_CROWD_FLAG_NO_WAVE | SCENE_CROWD_FLAG_STANDING);
    d[o] = e.from; d[o + 1] = e.to; d[o + 2] = e.blendStart + offset; d[o + 3] = Math.max(0.001, e.blend);
    d[o + 4] = e.excitementFrom; d[o + 5] = e.excitementTo; d[o + 6] = e.rate > 0 ? e.rate : 1;
    d[o + 7] = declared | (e.flags & (SCENE_CROWD_FLAG_SYNC | SCENE_CROWD_FLAG_PROPS));
    d[o + 8] = e.epoch + offset; d[o + 9] = e.jitter; d[o + 10] = e.excitementStart + offset; d[o + 11] = Math.max(0.001, e.excitementRamp);
  }
  if (frame.replaceWaves) store.waves.fill(0);
  for (const w of frame.waves) {
    // Reuse the first finished or empty slot.
    let slot = 0;
    for (let i = 0; i < SCENE_CROWD_MAX_WAVES; i++) {
      const b = i * 8, width = store.waves[b + 3], speed = Math.abs(store.waves[b + 2]);
      const done = width <= 0 || speed <= 0 || speed * (store.lastNow - store.waves[b]) > store.waves[b + 4] + width;
      if (done) { slot = i; break; }
    }
    store.waves.set([w.start + offset, w.origin, w.speed, Math.max(1e-4, w.width), Math.max(1, w.laps), 0, 0, 0], slot * 8);
  }
  store.version++;
  return true;
}

// sceneCrowdApply applies a GSC1 frame (bytes or a decoded object) to the
// crowd on target, a mount element or its ID. A frame for a mount that has not
// drawn its crowd yet waits and applies on the first draw.
function sceneCrowdApply(target: any, frame: any): boolean {
  const decoded = frame instanceof Uint8Array || frame instanceof ArrayBuffer ? sceneCrowdDecode(frame) : frame;
  const mount = sceneCrowdMountFor(target);
  if (mount && sceneCrowdStoreMap(mount).has(decoded.crowd)) return sceneCrowdApplyDecoded(mount, decoded);
  const key = mount && mount.id || (typeof target === "string" ? target.trim() : "");
  if (!key) return false;
  const queue = sceneCrowdPending.get(key) || [];
  queue.push(decoded);
  sceneCrowdPending.set(key, queue.slice(-64));
  return false;
}

// sceneCrowdNoteClock records the scene clock a mount drew at.
function sceneCrowdNoteClock(mount: any, now: number): void {
  const clock = sceneCrowdClock.get(mount) || { scene: 0, offset: null };
  clock.scene = now;
  sceneCrowdClock.set(mount, clock);
}

// sceneCrowdModel is the crowd frame to world matrix (column-major).
function sceneCrowdModel(ir: any): Float32Array {
  const yaw = Number(ir.rotation) || 0, c = Math.cos(yaw), s = Math.sin(yaw);
  return new Float32Array([c, 0, -s, 0, 0, 1, 0, 0, s, 0, c, 0, Number(ir.x) || 0, Number(ir.y) || 0, Number(ir.z) || 0, 1]);
}

function sceneCrowdUniforms(ir: any, store: any, bake: any, model: Float32Array, now: number, out: Float32Array): Float32Array {
  out.fill(0);
  out.set(model, 0);
  out[16] = now; out[17] = Number(ir.rotation) || 0; out[19] = Number(ir.material && ir.material.lightGlow) || 1.5;
  const fan = ir.fan || {};
  const tones = Array.isArray(fan.skinTones) && fan.skinTones.length ? fan.skinTones.slice(0, 8) : ["#c68c5f"];
  out.set(sceneCrowdLinear(fan.legs || "#2b2f3a"), 20); out[23] = tones.length;
  for (let i = 0; i < tones.length; i++) out.set(sceneCrowdLinear(tones[i]).concat(1), 24 + i * 4);
  out.set(bake.clips, 56);
  out.set(store.waves, 92);
  out[124] = (ir.sections || []).length;
  return out;
}

// Frustum planes (a, b, c, d) from a column-major view-projection matrix.
function sceneCrowdPlanes(m: any, out: Float64Array): Float64Array {
  const rows = [0, 1, 2, 3].map(function(i: number) { return [m[i], m[4 + i], m[8 + i], m[12 + i]]; });
  const combos = [[0, 1], [0, -1], [1, 1], [1, -1], [2, 1], [2, -1]];
  for (let p = 0; p < 6; p++) {
    const r = rows[combos[p][0]], sign = combos[p][1];
    const a = rows[3][0] + sign * r[0], b = rows[3][1] + sign * r[1], c = rows[3][2] + sign * r[2], d = rows[3][3] + sign * r[3];
    const len = Math.hypot(a, b, c) || 1;
    out[p * 4] = a / len; out[p * 4 + 1] = b / len; out[p * 4 + 2] = c / len; out[p * 4 + 3] = d / len;
  }
  return out;
}

// sceneCrowdSelect culls the crowd's blocks against the view, grades each
// visible block by its fans' height in pixels, and merges neighbouring blocks
// of one grade into draw ranges. It returns per-tier [first, count] ranges and
// fan counts.
function sceneCrowdSelect(gpu: any, packed: any, ir: any, o: any): any {
  const lod = ir.lod || {};
  const blocks = packed.blocks, spheres = packed.spheres, model = gpu.model;
  const nBlocks = blocks.length / 3;
  if (!gpu.select || gpu.select.tiers.length !== nBlocks) gpu.select = { tiers: new Int8Array(nBlocks), dist: new Float32Array(nBlocks), order: [], planes: new Float64Array(24) };
  const sel = gpu.select, planes = sceneCrowdPlanes(o.viewProj, sel.planes);
  const height = (Number(ir.fan && ir.fan.height) || 1.75) * 1.06;
  const projY = Math.abs(Number(o.proj && o.proj[5]) || 1.5), viewportH = Math.max(1, Number(o.height) || 1);
  const full = Number(lod.fullPixels) || 64, low = Number(lod.lowPixels) || 20, maxFull = Number(lod.maxFull) || 8000;
  const cam = o.camera || {}, cx = Number(cam.x) || 0, cy = Number(cam.y) || 0, cz = Number(cam.z) || 0;
  const fullBlocks: number[] = [];
  for (let b = 0; b < nBlocks; b++) {
    const lx = spheres[b * 4], ly = spheres[b * 4 + 1], lz = spheres[b * 4 + 2], r = spheres[b * 4 + 3];
    const wx = model[0] * lx + model[8] * lz + model[12], wy = ly + model[13], wz = model[2] * lx + model[10] * lz + model[14];
    let visible = true;
    for (let p = 0; p < 6 && visible; p++) visible = planes[p * 4] * wx + planes[p * 4 + 1] * wy + planes[p * 4 + 2] * wz + planes[p * 4 + 3] > -r;
    if (!visible) { sel.tiers[b] = -1; continue; }
    const dist = Math.max(0.5, Math.hypot(wx - cx, wy - cy, wz - cz) - r);
    const pixels = height * projY * viewportH / (2 * dist);
    sel.dist[b] = dist;
    sel.tiers[b] = pixels >= full ? 0 : pixels >= low ? 1 : 2;
    if (sel.tiers[b] === 0) fullBlocks.push(b);
  }
  // The MaxFull budget keeps the nearest blocks on the full mesh.
  let fullFans = 0;
  for (const b of fullBlocks) fullFans += blocks[b * 3 + 1];
  if (fullFans > maxFull) {
    fullBlocks.sort(function(a: number, b: number) { return sel.dist[a] - sel.dist[b]; });
    let kept = 0;
    for (const b of fullBlocks) {
      if (kept + blocks[b * 3 + 1] > maxFull) sel.tiers[b] = 1;
      else kept += blocks[b * 3 + 1];
    }
  }
  const ranges: number[][] = [[], [], []], fans = [0, 0, 0];
  for (let b = 0; b < nBlocks; b++) {
    const tier = sel.tiers[b];
    if (tier < 0) continue;
    const first = blocks[b * 3], count = blocks[b * 3 + 1], list = ranges[tier];
    fans[tier] += count;
    if (list.length && list[list.length - 2] + list[list.length - 1] === first) list[list.length - 1] += count;
    else list.push(first, count);
  }
  return { ranges, fans };
}

// sceneCrowdLevels reports what a crowd is doing at the scene clock: each
// section's excitement and action, and the fan-weighted loudness, for pages
// that drive audio from JavaScript. CrowdDirector.Audio is the Go equivalent.
function sceneCrowdLevels(target: any, crowdID: string): any {
  const mount = sceneCrowdMountFor(target);
  const store = mount && sceneCrowdStoreMap(mount).get(crowdID);
  if (!store) return null;
  const loud = [0.08, 0.3, 1.0, 0.65, 0.75, 0.9, 0.7, 0.45, 0.85];
  const now = store.lastNow, sections = [];
  let total = 0, weighted = 0;
  for (let i = 0; i < store.ir.sections.length; i++) {
    const o = i * SCENE_CROWD_SECTION_TEXELS * 4, d = store.sections;
    const k = Math.min(1, Math.max(0, (now - d[o + 10]) / Math.max(0.001, d[o + 11])));
    const excitement = d[o + 4] + (d[o + 5] - d[o + 4]) * k;
    const fans = store.fans ? store.fans[i] : 0;
    sections.push({ id: store.ir.sections[i].id, action: SCENE_CROWD_ACTIONS[d[o + 1] | 0][0], excitement, fans });
    total += fans; weighted += fans * excitement * loud[d[o + 1] | 0];
  }
  return { now, loudness: total ? weighted / total : 0, sections };
}
