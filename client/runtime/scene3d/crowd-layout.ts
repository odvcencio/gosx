// Crowd seat layout (lazy chunk bootstrap-feature-scene3d-crowd.js).
//
// The browser twin of scene/crowd_layout.go. Given a normalized CrowdIR it
// expands the same seats in the same order with the same arithmetic, so a page
// carries the stand description and seed, not one record per fan.
// scene3d-crowd.test.mjs checks the expansion against a Go-written fixture.
//
// sceneCrowdPack writes the GPU instance records (32 bytes per fan) and the
// culling blocks in one pass, without allocating per fan:
//
//   bytes  0..15  float32 x, y, z, yaw      (crowd frame)
//   bytes 16..19  float32 wave              (azimuth in turns)
//   bytes 20..23  uint16 section, phase
//   bytes 24..27  uint8 shirt slot, accent slot, prop, variant
//   bytes 28..31  uint8 eagerness, 0, 0, 0

const SCENE_CROWD_RECORD_BYTES = 32;
const SCENE_CROWD_BLOCK_SEATS = 128;
const SCENE_CROWD_MAX_SEATS = 200000;
const SCENE_CROWD_MAX_SECTIONS = 1024;
const SCENE_CROWD_THRESHOLD_ONE = 16777216;
const SCENE_CROWD_ROLL = { fill: 1, color: 2, accent: 3, prop: 4, variant: 5, phase: 6, eager: 7, yaw: 8, jitterX: 9, jitterZ: 10 };

// lowbias32, as crowdHash in Go and crowdHash in the shaders.
function sceneCrowdHash(value: number): number {
  let x = value >>> 0;
  x ^= x >>> 16;
  x = Math.imul(x, 0x7feb352d) >>> 0;
  x ^= x >>> 15;
  x = Math.imul(x, 0x846ca68b) >>> 0;
  x ^= x >>> 16;
  return x >>> 0;
}

function sceneCrowdRoll(seed: number, purpose: number): number {
  return sceneCrowdHash((seed ^ sceneCrowdHash(purpose)) >>> 0) >>> 8;
}

interface SceneCrowdPiece {
  corner: boolean; base: number; length: number;
  px: number; pz: number; tx: number; tz: number; nx: number; nz: number;
  cx: number; cz: number; radius: number; angle: number; sign: number;
}

function sceneCrowdPieceAt(p: SceneCrowdPiece, s: number, d: number, out: Float64Array): void {
  if (!p.corner) {
    out[0] = p.px + p.tx * s + p.nx * d; out[1] = p.pz + p.tz * s + p.nz * d; out[2] = p.nx; out[3] = p.nz;
    return;
  }
  const theta = p.angle + p.sign * (s / p.radius);
  const c = Math.cos(theta), sn = Math.sin(theta), r = p.radius + d;
  out[0] = p.cx + r * c; out[1] = p.cz + r * sn; out[2] = c; out[3] = sn;
}

function sceneCrowdPieceFactor(p: SceneCrowdPiece, d: number): number {
  return p.corner ? (p.radius + d) / p.radius : 1;
}

function sceneCrowdPieceSplit(p: SceneCrowdPiece, a: number, b: number): SceneCrowdPiece {
  const q = Object.assign({}, p);
  q.length = b - a;
  if (p.corner) q.angle = p.angle + p.sign * (a / p.radius);
  else { q.px = p.px + p.tx * a; q.pz = p.pz + p.tz * a; }
  return q;
}

function sceneCrowdNewPiece(fields: Partial<SceneCrowdPiece>): SceneCrowdPiece {
  return Object.assign({ corner: false, base: 0, length: 0, px: 0, pz: 0, tx: 0, tz: 0, nx: 0, nz: 0, cx: 0, cz: 0, radius: 0, angle: 0, sign: 1 }, fields);
}

function sceneCrowdBowlLoop(s: any): { pieces: SceneCrowdPiece[]; total: number } {
  const a = s.halfWidth, b = s.halfDepth, c = s.corner;
  const ax = a - c, bz = b - c, half = Math.PI / 2, cx = s.centerX || 0, cz = s.centerZ || 0;
  const raw = [
    sceneCrowdNewPiece({ px: cx + a, pz: cz, tx: 0, tz: 1, nx: 1, nz: 0, length: bz }),
    sceneCrowdNewPiece({ corner: true, cx: cx + ax, cz: cz + bz, radius: c, angle: 0, sign: 1, length: half * c }),
    sceneCrowdNewPiece({ px: cx + ax, pz: cz + b, tx: -1, tz: 0, nx: 0, nz: 1, length: 2 * ax }),
    sceneCrowdNewPiece({ corner: true, cx: cx - ax, cz: cz + bz, radius: c, angle: half, sign: 1, length: half * c }),
    sceneCrowdNewPiece({ px: cx - a, pz: cz + bz, tx: 0, tz: -1, nx: -1, nz: 0, length: 2 * bz }),
    sceneCrowdNewPiece({ corner: true, cx: cx - ax, cz: cz - bz, radius: c, angle: Math.PI, sign: 1, length: half * c }),
    sceneCrowdNewPiece({ px: cx - ax, pz: cz - b, tx: 1, tz: 0, nx: 0, nz: -1, length: 2 * ax }),
    sceneCrowdNewPiece({ corner: true, cx: cx + ax, cz: cz - bz, radius: c, angle: 3 * half, sign: 1, length: half * c }),
    sceneCrowdNewPiece({ px: cx + a, pz: cz - bz, tx: 0, tz: 1, nx: 1, nz: 0, length: bz }),
  ];
  const pieces: SceneCrowdPiece[] = [];
  let base = 0;
  for (const piece of raw) {
    if (piece.length <= 1e-9) continue;
    piece.base = base; base += piece.length;
    pieces.push(piece);
  }
  return { pieces, total: base };
}

function sceneCrowdPath(s: any): { pieces: SceneCrowdPiece[]; length: number; closed: boolean } {
  if (s.shape === "line") {
    const x0 = s.x0 || 0, z0 = s.z0 || 0, dx = (s.x1 || 0) - x0, dz = (s.z1 || 0) - z0;
    const length = Math.sqrt(dx * dx + dz * dz);
    const tx = dx / length, tz = dz / length;
    let nx = -tz, nz = tx;
    const mx = x0 + dx / 2 - (s.facingX || 0), mz = z0 + dz / 2 - (s.facingZ || 0);
    if (nx * mx + nz * mz < 0) { nx = -nx; nz = -nz; }
    return { pieces: [sceneCrowdNewPiece({ px: x0, pz: z0, tx, tz, nx, nz, length })], length, closed: false };
  }
  if (s.shape === "arc") {
    const start = s.startAngle || 0, sweep = (s.endAngle || 0) - start;
    const length = Math.abs(sweep) * s.radius;
    return { pieces: [sceneCrowdNewPiece({ corner: true, cx: s.centerX || 0, cz: s.centerZ || 0, radius: s.radius, angle: start, sign: sweep < 0 ? -1 : 1, length })], length, closed: false };
  }
  const loop = sceneCrowdBowlLoop(s);
  const loopStart = s.loopStart || 0, loopEnd = s.loopEnd || 0;
  if (loopStart === loopEnd) return { pieces: loop.pieces, length: loop.total, closed: true };
  const from = loopStart * loop.total;
  let to = loopEnd * loop.total;
  if (to <= from) to += loop.total;
  const out: SceneCrowdPiece[] = [];
  let base = 0;
  for (let lap = 0; lap < 2; lap++) {
    const offset = lap * loop.total;
    for (const piece of loop.pieces) {
      const start = piece.base + offset, end = start + piece.length;
      const a = Math.max(start, from), b = Math.min(end, to);
      if (b - a <= 1e-9) continue;
      const cut = sceneCrowdPieceSplit(piece, a - start, b - start);
      cut.base = base; base += cut.length;
      out.push(cut);
    }
  }
  return { pieces: out, length: base, closed: false };
}

function sceneCrowdPathPoint(pieces: SceneCrowdPiece[], u0: number, u1: number, q: number, d: number, out: Float64Array): void {
  let last: SceneCrowdPiece | null = null, lastOffset = 0;
  for (const piece of pieces) {
    const a = Math.max(piece.base, u0), b = Math.min(piece.base + piece.length, u1);
    if (b <= a) continue;
    const f = sceneCrowdPieceFactor(piece, d), run = (b - a) * f;
    last = piece; lastOffset = b - piece.base;
    if (q <= run) { sceneCrowdPieceAt(piece, a - piece.base + q / f, d, out); return; }
    q -= run;
  }
  if (last) sceneCrowdPieceAt(last, lastOffset, d, out);
}

function sceneCrowdSpans(ir: any, stand: any): number[] {
  let total = 0;
  for (let k = 0; k < stand.sectionCount; k++) total += ir.sections[stand.firstSection + k].span;
  const out = [0];
  let running = 0;
  for (let k = 0; k < stand.sectionCount; k++) {
    running += ir.sections[stand.firstSection + k].span;
    out.push(running / total);
  }
  out[stand.sectionCount] = 1;
  return out;
}

function sceneCrowdSlot(roll: number, thresholds: number[]): number {
  for (let i = 0; i < thresholds.length; i++) if (roll < thresholds[i]) return i;
  return 0;
}

function sceneCrowdPropFor(roll: number, thresholds: number[]): number {
  for (let i = 0; i < 4; i++) if (roll < thresholds[i]) return i + 1;
  return 0;
}

// The seat record handed to sceneCrowdLayout's emit callback. One object is
// reused for every seat; copy what you keep.
interface SceneCrowdSeat {
  index: number; seat: number; section: number; stand: number; row: number;
  x: number; y: number; z: number; yaw: number; wave: number; seed: number;
  color: number; accent: number; prop: number; variant: number; phase: number; eagerness: number;
}

// sceneCrowdLayout expands every occupied seat of a normalized crowd in
// instance order. It returns the seat count, or -1 when the crowd passes
// SCENE_CROWD_MAX_SEATS.
function sceneCrowdLayout(ir: any, emit: (seat: SceneCrowdSeat) => void): number {
  const seat: SceneCrowdSeat = { index: 0, seat: 0, section: 0, stand: 0, row: 0, x: 0, y: 0, z: 0, yaw: 0, wave: 0, seed: 0, color: 0, accent: 0, prop: 0, variant: 0, phase: 0, eagerness: 0 };
  const point = new Float64Array(4);
  const crowdSeed = (ir.seed || 0) >>> 0;
  let seatNumber = 0, index = 0, overflow = false;
  function nextSeed(): number {
    const seed = sceneCrowdHash((crowdSeed + Math.imul(seatNumber + 1, 0x9e3779b9)) >>> 0);
    seatNumber++;
    return seed;
  }
  function place(seed: number, standIndex: number, sectionIndex: number, row: number, x: number, y: number, z: number, yaw: number): void {
    const sec = ir.sections[sectionIndex];
    if (sceneCrowdRoll(seed, SCENE_CROWD_ROLL.fill) >= sec.fill) return;
    if (index >= SCENE_CROWD_MAX_SEATS) { overflow = true; return; }
    yaw += (sceneCrowdRoll(seed, SCENE_CROWD_ROLL.yaw) / SCENE_CROWD_THRESHOLD_ONE - 0.5) * 0.14;
    let wave = Math.atan2(z, x) / (2 * Math.PI);
    if (wave < 0) wave += 1;
    if (wave >= 1) wave = 0;
    seat.index = index; seat.seat = seatNumber - 1; seat.section = sectionIndex; seat.stand = standIndex; seat.row = row;
    seat.x = x; seat.y = y; seat.z = z; seat.yaw = yaw; seat.wave = wave; seat.seed = seed;
    seat.color = sceneCrowdSlot(sceneCrowdRoll(seed, SCENE_CROWD_ROLL.color), sec.colorThresholds);
    seat.accent = sceneCrowdSlot(sceneCrowdRoll(seed, SCENE_CROWD_ROLL.accent), sec.accentThresholds);
    seat.prop = sceneCrowdPropFor(sceneCrowdRoll(seed, SCENE_CROWD_ROLL.prop), sec.propThresholds);
    seat.variant = sceneCrowdRoll(seed, SCENE_CROWD_ROLL.variant) >>> 16;
    seat.phase = sceneCrowdRoll(seed, SCENE_CROWD_ROLL.phase) >>> 8;
    seat.eagerness = sceneCrowdRoll(seed, SCENE_CROWD_ROLL.eager) >>> 16;
    index++;
    emit(seat);
  }
  const stands = Array.isArray(ir.stands) ? ir.stands : [];
  for (let si = 0; si < stands.length && !overflow; si++) {
    const stand = stands[si];
    const bounds = sceneCrowdSpans(ir, stand);
    if (stand.shape === "floor") {
      const g = 1 / Math.sqrt(stand.density);
      const nx = Math.floor(2 * stand.halfWidth / g + 1e-9), nz = Math.floor(2 * stand.halfDepth / g + 1e-9);
      if (nx < 1 || nz < 1) continue;
      const x0 = (stand.centerX || 0) - stand.halfWidth + (2 * stand.halfWidth - nx * g) / 2;
      const z0 = (stand.centerZ || 0) - stand.halfDepth + (2 * stand.halfDepth - nz * g) / 2;
      for (let k = 0; k < stand.sectionCount; k++) {
        const c0 = Math.floor(bounds[k] * nx + 0.5), c1 = Math.floor(bounds[k + 1] * nx + 0.5);
        for (let row = 0; row < nz; row++) for (let col = c0; col < c1; col++) {
          const seed = nextSeed();
          const jx = (sceneCrowdRoll(seed, SCENE_CROWD_ROLL.jitterX) / SCENE_CROWD_THRESHOLD_ONE - 0.5) * 0.6;
          const jz = (sceneCrowdRoll(seed, SCENE_CROWD_ROLL.jitterZ) / SCENE_CROWD_THRESHOLD_ONE - 0.5) * 0.6;
          const x = x0 + (col + 0.5 + jx) * g, z = z0 + (row + 0.5 + jz) * g;
          place(seed, si, stand.firstSection + k, row, x, stand.frontHeight || 0, z, Math.atan2((stand.facingX || 0) - x, (stand.facingZ || 0) - z));
        }
      }
      continue;
    }
    const path = sceneCrowdPath(stand);
    const half = stand.aisleWidth / 2, width = stand.seatWidth;
    for (let k = 0; k < stand.sectionCount; k++) {
      const section = stand.firstSection + k;
      const u0 = bounds[k] * path.length, u1 = bounds[k + 1] * path.length;
      const left = !path.closed && k === 0 ? 0 : half;
      const right = !path.closed && k === stand.sectionCount - 1 ? 0 : half;
      for (let row = 0; row < stand.rows; row++) {
        const d = (row + 0.5) * stand.rowDepth;
        const y = stand.frontHeight + row * stand.rowRise;
        let rowLength = 0;
        for (const piece of path.pieces) {
          const a = Math.max(piece.base, u0), b = Math.min(piece.base + piece.length, u1);
          if (b > a) rowLength += (b - a) * sceneCrowdPieceFactor(piece, d);
        }
        const usable = rowLength - left - right;
        const n = Math.floor(usable / width + 1e-9);
        if (n < 1) continue;
        const margin = left + (usable - n * width) / 2;
        for (let j = 0; j < n; j++) {
          sceneCrowdPathPoint(path.pieces, u0, u1, margin + (j + 0.5) * width, d, point);
          place(nextSeed(), si, section, row, point[0], y, point[1], Math.atan2(-point[2], -point[3]));
        }
      }
    }
  }
  return overflow ? -1 : index;
}

// sceneCrowdPack expands the crowd into its GPU instance records and culling
// blocks. A block is at most SCENE_CROWD_BLOCK_SEATS consecutive fans of one
// section; blocks stores (first, count, section) and spheres the block's
// bounding sphere in the crowd frame (x, y, z, radius).
function sceneCrowdPack(ir: any): any {
  const sections = Array.isArray(ir.sections) ? ir.sections.length : 0;
  if (sections < 1 || sections > SCENE_CROWD_MAX_SECTIONS) return { error: "crowd has " + sections + " sections" };
  const expected = Math.max(0, Math.min(SCENE_CROWD_MAX_SEATS, ir.seats | 0));
  let capacity = Math.max(1, expected);
  let bytes = new ArrayBuffer(capacity * SCENE_CROWD_RECORD_BYTES);
  let f32 = new Float32Array(bytes), u16 = new Uint16Array(bytes), u8 = new Uint8Array(bytes);
  const blocks: number[] = [], spheres: number[] = [];
  const sectionFans = new Uint32Array(sections);
  let blockFirst = 0, blockSection = -1, minX = 0, minY = 0, minZ = 0, maxX = 0, maxY = 0, maxZ = 0;
  function closeBlock(end: number): void {
    if (blockSection < 0 || end <= blockFirst) return;
    blocks.push(blockFirst, end - blockFirst, blockSection);
    const cx = (minX + maxX) / 2, cy = (minY + maxY) / 2, cz = (minZ + maxZ) / 2;
    // Pad by a fan's height so raised arms and jumps stay inside.
    spheres.push(cx, cy + 0.9, cz, Math.hypot(maxX - cx, maxY - cy, maxZ - cz) + 1.4);
  }
  const count = sceneCrowdLayout(ir, function(seat: SceneCrowdSeat) {
    const i = seat.index;
    if (i >= capacity) {
      capacity *= 2;
      const grown = new ArrayBuffer(capacity * SCENE_CROWD_RECORD_BYTES);
      new Uint8Array(grown).set(u8);
      bytes = grown; f32 = new Float32Array(bytes); u16 = new Uint16Array(bytes); u8 = new Uint8Array(bytes);
    }
    if (seat.section !== blockSection || i - blockFirst >= SCENE_CROWD_BLOCK_SEATS) {
      closeBlock(i);
      blockFirst = i; blockSection = seat.section;
      minX = maxX = seat.x; minY = maxY = seat.y; minZ = maxZ = seat.z;
    } else {
      if (seat.x < minX) minX = seat.x; if (seat.x > maxX) maxX = seat.x;
      if (seat.y < minY) minY = seat.y; if (seat.y > maxY) maxY = seat.y;
      if (seat.z < minZ) minZ = seat.z; if (seat.z > maxZ) maxZ = seat.z;
    }
    const o = i * 8;
    f32[o] = seat.x; f32[o + 1] = seat.y; f32[o + 2] = seat.z; f32[o + 3] = seat.yaw; f32[o + 4] = seat.wave;
    u16[o * 2 + 10] = seat.section; u16[o * 2 + 11] = seat.phase;
    const b = i * SCENE_CROWD_RECORD_BYTES;
    u8[b + 24] = seat.color; u8[b + 25] = seat.accent; u8[b + 26] = seat.prop; u8[b + 27] = seat.variant;
    u8[b + 28] = seat.eagerness; u8[b + 29] = 0; u8[b + 30] = 0; u8[b + 31] = 0;
    sectionFans[seat.section]++;
  });
  if (count < 0) return { error: "crowd passes " + SCENE_CROWD_MAX_SEATS + " seats" };
  closeBlock(count);
  return {
    count,
    records: new Uint8Array(bytes, 0, count * SCENE_CROWD_RECORD_BYTES),
    blocks: new Uint32Array(blocks),
    spheres: new Float32Array(spheres),
    sectionFans,
    mismatch: expected !== count,
  };
}
