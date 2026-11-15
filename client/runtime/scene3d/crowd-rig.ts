// Crowd fan rig, action library and meshes (lazy chunk
// bootstrap-feature-scene3d-crowd.js).
//
// The built-in fan is an 11-joint figure of boxes. Each library action is a
// procedural loop of joint angles; sceneCrowdBake samples every action at
// SCENE_CROWD_SEGMENTS + 1 points and stores each joint's skinning matrix in a
// palette laid out like the GLB crowd atlas (animation.ts sceneBuildCrowdAtlas):
// four rgba32float texels per joint (the matrix columns), one row per sample,
// row 0 the bind pose. The vertex shaders skin from it, so no CPU work runs per
// fan per frame.
//
// Three meshes share the skeleton and vertex format:
//   full      18 boxes (body, cap and props), flat-shaded
//   low       10 boxes of 8 vertices; each arm is one box bent by two joints
//   impostor  5 camera-facing quads (body, head, two arms, one prop)
//
// Vertex format, 36 bytes: float32 position xyz, float32 normal xyz,
// uint8 joints[4], unorm8 weights[4], uint8 part[4] (colour slot, prop kind,
// 0, 0). Colour slots: 0 shirt, 1 skin, 2 legs, 3 accent, 4 glow, 5 pole.
// Prop kinds: 0 body, 1 light, 2 scarf, 3 towel, 4 banner, 5 cap (worn by a
// quarter of fans, from their variant).

const SCENE_CROWD_JOINTS = 11;
const SCENE_CROWD_SEGMENTS = 24;
const SCENE_CROWD_VERTEX_BYTES = 36;
// Library order matches scene/crowd_action.go: name, cycle seconds, beat.
const SCENE_CROWD_ACTIONS = [
  ["idle", 4.0, -1], ["stand", 3.0, -1], ["cheer", 0.8, 0.25], ["clap", 0.5, 0.5], ["wave-arms", 1.6, -1],
  ["jump", 0.7, 0.92], ["twirl", 0.6, -1], ["hold-up", 2.4, -1], ["wave", 1.0, 0.5],
];
const SCENE_CROWD_WAVE_ACTION = 8;
const SCENE_CROWD_PARENTS = [-1, 0, 1, 1, 3, 1, 5, 0, 7, 0, 9];
// Joint pivots in the bind pose (metres, a 1.75 m fan facing +Z).
const SCENE_CROWD_PIVOTS = [
  [0, 0.95, 0], [0, 0.97, 0], [0, 1.47, 0],
  [0.22, 1.42, 0], [0.22, 1.13, 0], [-0.22, 1.42, 0], [-0.22, 1.13, 0],
  [0.1, 0.92, 0], [0.1, 0.49, 0], [-0.1, 0.92, 0], [-0.1, 0.49, 0],
];

// A pose: root offset xyz, then XYZ Euler angles per joint.
interface SceneCrowdPose { root: number[]; angles: number[][]; }

function sceneCrowdZeroPose(): SceneCrowdPose {
  const angles: number[][] = [];
  for (let j = 0; j < SCENE_CROWD_JOINTS; j++) angles.push([0, 0, 0]);
  return { root: [0, 0, 0], angles };
}

// Arm pitch about X: 0 hangs down, -PI/2 points forward, -PI points up.
// Abduction about Z swings the left arm (+X side) out for positive values.
function sceneCrowdArms(p: SceneCrowdPose, pitchL: number, abductL: number, elbowL: number, pitchR: number, abductR: number, elbowR: number): void {
  p.angles[3] = [pitchL, 0, abductL]; p.angles[4] = [elbowL, 0, 0];
  p.angles[5] = [pitchR, 0, -abductR]; p.angles[6] = [elbowR, 0, 0];
}

function sceneCrowdSit(p: SceneCrowdPose): void {
  p.root = [0, -0.45, -0.06];
  p.angles[7] = [-1.5, 0, 0.04]; p.angles[8] = [1.5, 0, 0];
  p.angles[9] = [-1.5, 0, -0.04]; p.angles[10] = [1.5, 0, 0];
  sceneCrowdArms(p, -0.45, 0.05, -1.0, -0.45, 0.05, -1.0);
}

const SCENE_CROWD_TAU = Math.PI * 2;

// sceneCrowdActionPose evaluates action a at phase t (0..1).
function sceneCrowdActionPose(a: number, t: number): SceneCrowdPose {
  const p = sceneCrowdZeroPose();
  const s = Math.sin(SCENE_CROWD_TAU * t), c = Math.cos(SCENE_CROWD_TAU * t);
  switch (a) {
    case 0: // idle: seated, a slow look around and a small sway
      sceneCrowdSit(p);
      p.angles[1] = [0.02 * s, 0, 0.03 * Math.sin(SCENE_CROWD_TAU * 2 * t)];
      p.angles[2] = [0.05 * c, 0.3 * s, 0];
      break;
    case 1: // stand: weight shift and a nod
      p.root = [0.025 * s, 0, 0];
      p.angles[1] = [0, 0, -0.04 * s];
      p.angles[2] = [0.08 * Math.sin(SCENE_CROWD_TAU * 2 * t), 0.15 * c, 0];
      sceneCrowdArms(p, -0.12, 0.08, -0.15, -0.12, 0.08, -0.15);
      break;
    case 2: { // cheer: fists pump overhead with a bounce
      const pump = 0.5 + 0.5 * c;
      p.root = [0, 0.05 * pump, 0];
      p.angles[1] = [-0.1, 0, 0];
      p.angles[2] = [-0.25, 0, 0];
      sceneCrowdArms(p, -2.55 - 0.35 * pump, 0.3, -0.9 + 0.8 * pump, -2.55 - 0.35 * pump, 0.3, -0.9 + 0.8 * pump);
      break;
    }
    case 3: { // clap: hands meet in front of the chest at t = 0.5
      const open = 0.5 + 0.5 * c;
      p.angles[1] = [0.05, 0, 0];
      sceneCrowdArms(p, -0.95, -0.15 + 0.5 * open, -1.0, -0.95, -0.15 + 0.5 * open, -1.0);
      break;
    }
    case 4: // wave-arms: both arms overhead, swaying together
      p.angles[1] = [0, 0, 0.1 * s];
      p.root = [0.03 * s, 0, 0];
      sceneCrowdArms(p, -2.9, 0.15 + 0.45 * s, -0.2, -2.9, 0.15 - 0.45 * s, -0.2);
      break;
    case 5: { // jump: crouch, lift off, land at t = 0.92
      let lift = 0, crouch = 0;
      if (t < 0.15) crouch = Math.sin(Math.PI * t / 0.15);
      else if (t < 0.92) lift = Math.sin(Math.PI * (t - 0.15) / 0.77);
      else crouch = Math.sin(Math.PI * (t - 0.92) / 0.08);
      p.root = [0, 0.32 * lift - 0.14 * crouch, 0];
      p.angles[7] = [-0.7 * crouch, 0, 0.03]; p.angles[8] = [1.3 * crouch, 0, 0];
      p.angles[9] = [-0.7 * crouch, 0, -0.03]; p.angles[10] = [1.3 * crouch, 0, 0];
      p.angles[1] = [0.35 * crouch, 0, 0];
      sceneCrowdArms(p, -0.6 - 1.9 * lift, 0.25, -0.3, -0.6 - 1.9 * lift, 0.25, -0.3);
      break;
    }
    case 6: // twirl: the right hand circles overhead
      p.angles[1] = [0, 0.1 * s, 0];
      sceneCrowdArms(p, -0.15, 0.1, -0.2, -2.75 + 0.3 * c, 0.35 * s, -0.25);
      break;
    case 7: // hold-up: both hands up, a slow sway
      p.root = [0.03 * s, 0, 0];
      p.angles[1] = [0, 0, 0.05 * s];
      sceneCrowdArms(p, -2.65, 0.42, -0.25, -2.65, 0.42, -0.25);
      break;
    default: { // wave: rise with arms up at t = 0.5, then sit
      const up = Math.sin(Math.PI * Math.min(1, Math.max(0, t)));
      const rise = up * up * (3 - 2 * up);
      const sit = sceneCrowdZeroPose();
      sceneCrowdSit(sit);
      const top = sceneCrowdZeroPose();
      sceneCrowdArms(top, -2.95, 0.25, -0.1, -2.95, 0.25, -0.1);
      top.angles[2] = [-0.2, 0, 0];
      for (let k = 0; k < 3; k++) p.root[k] = sit.root[k] + (top.root[k] - sit.root[k]) * rise;
      for (let j = 0; j < SCENE_CROWD_JOINTS; j++) for (let k = 0; k < 3; k++) p.angles[j][k] = sit.angles[j][k] + (top.angles[j][k] - sit.angles[j][k]) * rise;
    }
  }
  return p;
}

// Column-major 4x4 helpers.
function sceneCrowdMat(): Float64Array { const m = new Float64Array(16); m[0] = m[5] = m[10] = m[15] = 1; return m; }
function sceneCrowdMul(a: Float64Array, b: Float64Array): Float64Array {
  const o = new Float64Array(16);
  for (let c = 0; c < 4; c++) for (let r = 0; r < 4; r++) {
    o[c * 4 + r] = a[r] * b[c * 4] + a[4 + r] * b[c * 4 + 1] + a[8 + r] * b[c * 4 + 2] + a[12 + r] * b[c * 4 + 3];
  }
  return o;
}
function sceneCrowdTRS(tx: number, ty: number, tz: number, e: number[]): Float64Array {
  const cx = Math.cos(e[0]), sx = Math.sin(e[0]), cy = Math.cos(e[1]), sy = Math.sin(e[1]), cz = Math.cos(e[2]), sz = Math.sin(e[2]);
  // R = Rz * Ry * Rx (X applied first).
  const m = sceneCrowdMat();
  m[0] = cy * cz; m[1] = cy * sz; m[2] = -sy;
  m[4] = sx * sy * cz - cx * sz; m[5] = sx * sy * sz + cx * cz; m[6] = sx * cy;
  m[8] = cx * sy * cz + sx * sz; m[9] = cx * sy * sz - sx * cz; m[10] = cx * cy;
  m[12] = tx; m[13] = ty; m[14] = tz;
  return m;
}

// sceneCrowdSkinMatrices returns the 11 skinning matrices (world * inverse
// bind) of a pose, scaled to height metres.
function sceneCrowdSkinMatrices(pose: SceneCrowdPose, height: number): Float64Array[] {
  const world: Float64Array[] = [];
  const k = height / 1.75;
  for (let j = 0; j < SCENE_CROWD_JOINTS; j++) {
    const pivot = SCENE_CROWD_PIVOTS[j], parent = SCENE_CROWD_PARENTS[j];
    let local: Float64Array;
    if (parent < 0) local = sceneCrowdTRS((pivot[0] + pose.root[0]) * k, (pivot[1] + pose.root[1]) * k, (pivot[2] + pose.root[2]) * k, pose.angles[j]);
    else {
      const pp = SCENE_CROWD_PIVOTS[parent];
      local = sceneCrowdTRS((pivot[0] - pp[0]) * k, (pivot[1] - pp[1]) * k, (pivot[2] - pp[2]) * k, pose.angles[j]);
    }
    world.push(parent < 0 ? local : sceneCrowdMul(world[parent], local));
  }
  return world.map(function(m: Float64Array, j: number) {
    const inv = sceneCrowdMat();
    inv[12] = -SCENE_CROWD_PIVOTS[j][0] * k; inv[13] = -SCENE_CROWD_PIVOTS[j][1] * k; inv[14] = -SCENE_CROWD_PIVOTS[j][2] * k;
    return sceneCrowdMul(m, inv);
  });
}

// sceneCrowdBake builds the palette and its clip table. The palette is
// width x height rgba32float texels: width = 4 per joint, row 0 the bind
// pose, then SCENE_CROWD_SEGMENTS + 1 rows per action. clips holds
// (start row, segments, cycle seconds, beat) per action.
function sceneCrowdBake(height: number): any {
  const width = SCENE_CROWD_JOINTS * 4;
  const rows = 1 + SCENE_CROWD_ACTIONS.length * (SCENE_CROWD_SEGMENTS + 1);
  const data = new Float32Array(width * rows * 4);
  const clips = new Float32Array(SCENE_CROWD_ACTIONS.length * 4);
  function write(row: number, matrices: Float64Array[]): void {
    for (let j = 0; j < SCENE_CROWD_JOINTS; j++) data.set(matrices[j], (row * width + j * 4) * 4);
  }
  write(0, sceneCrowdSkinMatrices(sceneCrowdZeroPose(), height));
  for (let a = 0; a < SCENE_CROWD_ACTIONS.length; a++) {
    const start = 1 + a * (SCENE_CROWD_SEGMENTS + 1);
    clips[a * 4] = start; clips[a * 4 + 1] = SCENE_CROWD_SEGMENTS;
    clips[a * 4 + 2] = SCENE_CROWD_ACTIONS[a][1] as number; clips[a * 4 + 3] = SCENE_CROWD_ACTIONS[a][2] as number;
    for (let i = 0; i <= SCENE_CROWD_SEGMENTS; i++) {
      const t = a === SCENE_CROWD_WAVE_ACTION ? i / SCENE_CROWD_SEGMENTS : (i % SCENE_CROWD_SEGMENTS) / SCENE_CROWD_SEGMENTS;
      write(start + i, sceneCrowdSkinMatrices(sceneCrowdActionPose(a, t), height));
    }
  }
  return { width, height: rows, data, clips, segments: SCENE_CROWD_SEGMENTS, actions: SCENE_CROWD_ACTIONS.length };
}

// Mesh building. A box is given by its centre, size, colour slot and prop
// kind; joints names one joint for the whole box, or [top, bottom] for a box
// whose upper four corners follow one joint and lower four another.
interface SceneCrowdMeshBuilder { v: number[]; i: number[]; }

function sceneCrowdVertex(b: SceneCrowdMeshBuilder, x: number, y: number, z: number, nx: number, ny: number, nz: number, joint: number, slot: number, kind: number): number {
  const index = b.v.length / 12;
  b.v.push(x, y, z, nx, ny, nz, joint, 255, slot, kind, 0, 0);
  return index;
}

function sceneCrowdBox(b: SceneCrowdMeshBuilder, center: number[], size: number[], joints: number[], slot: number, kind: number, flat: boolean): void {
  const hx = size[0] / 2, hy = size[1] / 2, hz = size[2] / 2, cx = center[0], cy = center[1], cz = center[2];
  const top = joints[0], bottom = joints.length > 1 ? joints[1] : joints[0];
  if (!flat) {
    const base = b.v.length / 12;
    for (let k = 0; k < 8; k++) {
      const sx = k & 1 ? 1 : -1, sy = k & 2 ? 1 : -1, sz = k & 4 ? 1 : -1;
      const n = Math.hypot(sx * hy * hz, sy * hx * hz, sz * hx * hy) || 1;
      sceneCrowdVertex(b, cx + sx * hx, cy + sy * hy, cz + sz * hz, sx * hy * hz / n, sy * hx * hz / n, sz * hx * hy / n, sy > 0 ? top : bottom, slot, kind);
    }
    const faces = [[0, 4, 6, 2], [1, 3, 7, 5], [0, 1, 5, 4], [2, 6, 7, 3], [0, 2, 3, 1], [4, 5, 7, 6]];
    for (const f of faces) b.i.push(base + f[0], base + f[1], base + f[2], base + f[0], base + f[2], base + f[3]);
    return;
  }
  // Six faces, four corners each, counter-clockwise seen from outside.
  const faces = [
    [[1, 0, 0], [1, -1, -1], [1, 1, -1], [1, 1, 1], [1, -1, 1]],
    [[-1, 0, 0], [-1, -1, 1], [-1, 1, 1], [-1, 1, -1], [-1, -1, -1]],
    [[0, 1, 0], [-1, 1, -1], [-1, 1, 1], [1, 1, 1], [1, 1, -1]],
    [[0, -1, 0], [-1, -1, 1], [-1, -1, -1], [1, -1, -1], [1, -1, 1]],
    [[0, 0, 1], [-1, -1, 1], [1, -1, 1], [1, 1, 1], [-1, 1, 1]],
    [[0, 0, -1], [1, -1, -1], [-1, -1, -1], [-1, 1, -1], [1, 1, -1]],
  ];
  for (const face of faces) {
    const n = face[0], base = b.v.length / 12;
    for (let k = 1; k <= 4; k++) {
      const c = face[k];
      sceneCrowdVertex(b, cx + c[0] * hx, cy + c[1] * hy, cz + c[2] * hz, n[0], n[1], n[2], c[1] > 0 ? top : bottom, slot, kind);
    }
    b.i.push(base, base + 1, base + 2, base, base + 2, base + 3);
  }
}

// A quad in the fan's XY plane facing +Z, from (x0, y0) to (x1, y1).
function sceneCrowdQuad(b: SceneCrowdMeshBuilder, x0: number, y0: number, x1: number, y1: number, z: number, joints: number[], slot: number, kind: number): void {
  const top = joints[0], bottom = joints.length > 1 ? joints[1] : joints[0];
  const base = b.v.length / 12;
  sceneCrowdVertex(b, x0, y0, z, 0, 0, 1, bottom, slot, kind);
  sceneCrowdVertex(b, x1, y0, z, 0, 0, 1, bottom, slot, kind);
  sceneCrowdVertex(b, x1, y1, z, 0, 0, 1, top, slot, kind);
  sceneCrowdVertex(b, x0, y1, z, 0, 0, 1, top, slot, kind);
  b.i.push(base, base + 1, base + 2, base, base + 2, base + 3);
}

function sceneCrowdProps(b: SceneCrowdMeshBuilder, flat: boolean): void {
  // Light in the right hand, scarf between both hands, towel and banner in the right hand.
  sceneCrowdBox(b, [-0.22, 0.85, 0.03], [0.06, 0.1, 0.03], [6], 4, 1, flat);
  const base = b.v.length / 12;
  sceneCrowdBox(b, [0, 0.84, 0.04], [0.42, 0.13, 0.02], [6], 3, 2, flat);
  for (let k = base; k < b.v.length / 12; k++) if (b.v[k * 12] > 0) b.v[k * 12 + 6] = 4; // left half follows the left hand
  sceneCrowdBox(b, [-0.22, 0.68, 0.03], [0.03, 0.34, 0.22], [6], 3, 3, flat);
  sceneCrowdBox(b, [-0.22, 1.2, 0.03], [0.025, 0.8, 0.025], [6], 5, 4, flat);
  sceneCrowdBox(b, [-0.41, 1.45, 0.03], [0.36, 0.24, 0.015], [6], 3, 4, flat);
}

// sceneCrowdFinish packs a mesh. bodyIndexCount splits its indices: body
// triangles first (drawn with the fan material), then props (the prop
// material).
function sceneCrowdFinish(b: SceneCrowdMeshBuilder, height: number, bodyIndexCount: number): any {
  const k = height / 1.75, count = b.v.length / 12;
  const bytes = new ArrayBuffer(count * SCENE_CROWD_VERTEX_BYTES);
  const f32 = new Float32Array(bytes), u8 = new Uint8Array(bytes);
  for (let i = 0; i < count; i++) {
    const s = i * 12, o = i * 9, u = i * SCENE_CROWD_VERTEX_BYTES;
    f32[o] = b.v[s] * k; f32[o + 1] = b.v[s + 1] * k; f32[o + 2] = b.v[s + 2] * k;
    f32[o + 3] = b.v[s + 3]; f32[o + 4] = b.v[s + 4]; f32[o + 5] = b.v[s + 5];
    u8[u + 24] = b.v[s + 6]; u8[u + 25] = 0; u8[u + 26] = 0; u8[u + 27] = 0;
    u8[u + 28] = 255; u8[u + 29] = 0; u8[u + 30] = 0; u8[u + 31] = 0;
    u8[u + 32] = b.v[s + 8]; u8[u + 33] = b.v[s + 9]; u8[u + 34] = 0; u8[u + 35] = 0;
  }
  return { vertices: new Uint8Array(bytes), indices: new Uint16Array(b.i), vertexCount: count, indexCount: b.i.length, bodyIndexCount };
}

// sceneCrowdMeshes builds the full, low and impostor meshes for a fan of the
// given height.
function sceneCrowdMeshes(height: number): any[] {
  const full: SceneCrowdMeshBuilder = { v: [], i: [] };
  sceneCrowdBox(full, [0, 1.2, 0], [0.38, 0.5, 0.22], [1], 0, 0, true);
  sceneCrowdBox(full, [0, 1.6, 0.01], [0.2, 0.24, 0.22], [2], 1, 0, true);
  sceneCrowdBox(full, [0, 1.74, 0.03], [0.23, 0.06, 0.27], [2], 3, 5, true);
  for (const side of [1, -1]) {
    const up = side > 0 ? 3 : 5, fore = side > 0 ? 4 : 6, thigh = side > 0 ? 7 : 9, shin = side > 0 ? 8 : 10;
    sceneCrowdBox(full, [0.22 * side, 1.28, 0], [0.1, 0.3, 0.11], [up], 0, 0, true);
    sceneCrowdBox(full, [0.22 * side, 0.99, 0], [0.085, 0.28, 0.095], [fore], 1, 0, true);
    sceneCrowdBox(full, [0.1 * side, 0.71, 0], [0.15, 0.44, 0.16], [thigh], 2, 0, true);
    sceneCrowdBox(full, [0.1 * side, 0.27, 0], [0.13, 0.46, 0.14], [shin], 2, 0, true);
    sceneCrowdBox(full, [0.1 * side, 0.03, 0.05], [0.12, 0.06, 0.24], [shin], 2, 0, true);
  }
  const fullBody = full.i.length;
  sceneCrowdProps(full, true);
  const low: SceneCrowdMeshBuilder = { v: [], i: [] };
  sceneCrowdBox(low, [0, 1.2, 0], [0.38, 0.5, 0.22], [1], 0, 0, false);
  sceneCrowdBox(low, [0, 1.62, 0.01], [0.21, 0.27, 0.23], [2], 1, 0, false);
  for (const side of [1, -1]) sceneCrowdBox(low, [0.22 * side, 1.14, 0], [0.1, 0.58, 0.1], side > 0 ? [3, 4] : [5, 6], 0, 0, false);
  sceneCrowdBox(low, [0, 0.49, 0], [0.32, 0.9, 0.16], [7, 8], 2, 0, false);
  const lowBody = low.i.length;
  sceneCrowdProps(low, false);
  const imp: SceneCrowdMeshBuilder = { v: [], i: [] };
  sceneCrowdQuad(imp, -0.2, 0.05, 0.2, 1.45, 0, [1, 8], 0, 0);
  sceneCrowdQuad(imp, -0.11, 1.47, 0.11, 1.75, 0.01, [2], 1, 0);
  sceneCrowdQuad(imp, 0.17, 0.86, 0.27, 1.43, 0.005, [3, 4], 0, 0);
  sceneCrowdQuad(imp, -0.27, 0.86, -0.17, 1.43, 0.005, [5, 6], 0, 0);
  const impBody = imp.i.length;
  sceneCrowdQuad(imp, -0.6, 1.33, -0.2, 1.6, 0.02, [6], 3, 4);
  sceneCrowdQuad(imp, -0.3, 0.76, -0.12, 0.94, 0.02, [6], 4, 1);
  return [sceneCrowdFinish(full, height, fullBody), sceneCrowdFinish(low, height, lowBody), sceneCrowdFinish(imp, height, impBody)];
}

// sceneCrowdMaterials returns the fan and prop material records the renderer
// hooks upload, with the crowd defaults under the authored fields.
const sceneCrowdMaterialCache = new WeakMap<object, any>();
function sceneCrowdMaterials(ir: any): any {
  let cached = sceneCrowdMaterialCache.get(ir);
  if (!cached) {
    const m = ir.material || {};
    cached = {
      fan: Object.assign({ materialKind: "standard", color: "#ffffff", roughness: 0.85, metalness: 0 }, m.fan || {}),
      props: Object.assign({ materialKind: "standard", color: "#ffffff", roughness: 0.7, metalness: 0 }, m.props || {}),
    };
    sceneCrowdMaterialCache.set(ir, cached);
  }
  return cached;
}
