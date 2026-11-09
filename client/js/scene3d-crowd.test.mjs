// Environment.Crowds runtime (crowd*.ts): Go layout parity, instance packing,
// the baked bone palette, mesh winding, crowd frames and the clock offset,
// block culling and level of detail, and the composed shaders, which Naga and
// glslangValidator check when they are installed.
import test from "node:test";
import assert from "node:assert/strict";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import vm from "node:vm";
import { execFileSync } from "node:child_process";
import { createRequire } from "node:module";

const require = createRequire(import.meta.url);
const ts = createRequire(new URL("../runtime/package.json", import.meta.url))("typescript");
const { readSceneRendererBackendSrc } = require("./scene3d-renderer-source-set.js");
const { createContext } = require("./runtime-test-harness.js");
const read = (name) => fs.readFileSync(new URL("../runtime/scene3d/" + name, import.meta.url), "utf8");
const transpile = (source) => ts.transpileModule(source, { compilerOptions: { target: ts.ScriptTarget.ES2022 } }).outputText;
const sources = ["crowd-layout.ts", "crowd-rig.ts", "crowd-state.ts", "crowd-glsl.ts", "crowd-wgsl.ts", "crowd.ts"];
const code = sources.map((name) => transpile(read(name))).join("\n");
const fixture = JSON.parse(fs.readFileSync(new URL("./testdata/scene3d-crowd-fixture.json", import.meta.url), "utf8"));

function runtime(extra = {}) {
  const window = {};
  const context = vm.createContext({ window, Math, Float32Array, Float64Array, Uint8Array, Uint16Array, Uint32Array, Int8Array, ArrayBuffer, DataView,
    TextDecoder, Number, Array, Object, String, JSON, Error, TypeError, Map, Set, WeakMap, performance, ...extra });
  vm.runInContext(code + `
    globalThis.c = { sceneCrowdLayout, sceneCrowdPack, sceneCrowdHash, sceneCrowdBake, sceneCrowdMeshes, sceneCrowdSkinMatrices,
      sceneCrowdActionPose, sceneCrowdDecode, sceneCrowdApply, sceneCrowdStore, sceneCrowdNoteClock, sceneCrowdSelect, sceneCrowdModel,
      sceneCrowdUniforms, sceneCrowdInitialSections, sceneCrowdLevels, sceneCrowdFragmentGLSL, sceneCrowdFragmentWGSL, sceneCrowdVertexWGSL,
      SCENE_CROWD_GLSL_VERTEX, SCENE_CROWD_ACTIONS, SCENE_CROWD_SECTION_TEXELS, SCENE_CROWD_BLOCK_SEATS };`, context);
  return { c: context.c, window };
}

function digest(seats) {
  let h = 2166136261 >>> 0;
  for (const s of seats) for (const v of [s.seat, s.section, s.row, s.seed, s.color, s.accent, s.prop, s.variant, s.phase, s.eagerness]) {
    let x = v >>> 0;
    for (let k = 0; k < 4; k++) { h = Math.imul((h ^ (x & 255)) >>> 0, 16777619) >>> 0; x >>>= 8; }
  }
  return h >>> 0;
}

function expand(c, ir) {
  const seats = [];
  const n = c.sceneCrowdLayout(ir, (seat) => seats.push({ ...seat }));
  return { n, seats };
}

const bowl = () => fixture.crowds.find((entry) => entry.ir.id === "bowl");

test("browser seats match scene.Crowd.Layout from the Go fixture", () => {
  const { c } = runtime();
  assert.equal(fixture.crowds.length, 2);
  for (const entry of fixture.crowds) {
    const { n, seats } = expand(c, entry.ir);
    assert.equal(n, entry.count, entry.ir.id + " seat count");
    assert.equal(entry.ir.seats, entry.count);
    assert.equal(digest(seats), entry.digest, entry.ir.id + " digest over every seat's integer fields");
    for (const want of entry.seats) {
      const got = seats[want.Index];
      for (const [a, b] of [["seat", "Seat"], ["section", "Section"], ["stand", "Stand"], ["row", "Row"], ["seed", "Seed"], ["color", "Color"], ["accent", "Accent"], ["prop", "Prop"], ["variant", "Variant"], ["phase", "Phase"], ["eagerness", "Eagerness"]]) {
        assert.equal(got[a], want[b], `${entry.ir.id} seat ${want.Index} ${a}`);
      }
      // Go and the browser share the arithmetic; trig may differ in the last bit.
      for (const [a, b] of [["x", want.Position.x || 0], ["y", want.Position.y || 0], ["z", want.Position.z || 0], ["yaw", want.Yaw], ["wave", want.Wave]]) {
        assert.ok(Math.abs(got[a] - b) < 1e-9, `${entry.ir.id} seat ${want.Index} ${a}: ${got[a]} vs ${b}`);
      }
    }
  }
});

test("a crowd frame Go encoded decodes to the same fields", () => {
  const { c } = runtime();
  const bytes = Uint8Array.from(Buffer.from(fixture.frame, "base64"));
  const got = c.sceneCrowdDecode(bytes), want = fixture.frameFields;
  assert.equal(got.crowd, want.Crowd);
  assert.equal(got.now, want.Now);
  assert.equal(got.sections.length, want.Sections.length);
  const names = c.SCENE_CROWD_ACTIONS.map((a) => a[0]);
  want.Sections.forEach((w, i) => {
    const g = got.sections[i];
    assert.equal(g.section, w.Section);
    assert.equal(names[g.from], w.From); assert.equal(names[g.to], w.To);
    for (const [a, b] of [["blendStart", "BlendStart"], ["blend", "Blend"], ["excitementFrom", "ExcitementFrom"], ["excitementTo", "ExcitementTo"], ["rate", "Rate"], ["epoch", "Epoch"], ["jitter", "Jitter"], ["excitementStart", "ExcitementStart"], ["excitementRamp", "ExcitementRamp"]]) {
      assert.ok(Math.abs(g[a] - w[b]) < 1e-5, `section ${w.Section} ${a}: ${g[a]} vs ${w[b]}`);
    }
    assert.equal(Boolean(g.flags & 1), w.Synced); assert.equal(Boolean(g.flags & 4), w.Props);
  });
  assert.equal(got.waves.length, 1);
  assert.ok(Math.abs(got.waves[0].start - want.Waves[0].Start) < 1e-5 && Math.abs(got.waves[0].speed - want.Waves[0].Speed) < 1e-6);
});

test("hash and action table match the Go side", () => {
  const { c } = runtime();
  for (const [input, want] of Object.entries(fixture.hashes)) assert.equal(c.sceneCrowdHash(Number(input)), want, "hash " + input);
  assert.equal(c.SCENE_CROWD_ACTIONS.length, fixture.actions.length);
  fixture.actions.forEach((action, i) => {
    assert.equal(c.SCENE_CROWD_ACTIONS[i][0], action.Action);
    assert.equal(c.SCENE_CROWD_ACTIONS[i][1], action.Cycle);
    assert.equal(c.SCENE_CROWD_ACTIONS[i][2], action.Beat);
    assert.equal(action.Index, i);
  });
});

test("packing writes one record per fan and blocks that tile each section", () => {
  const { c } = runtime();
  const ir = bowl().ir;
  const packed = c.sceneCrowdPack(ir);
  const { seats } = expand(c, ir);
  assert.equal(packed.count, seats.length);
  assert.equal(packed.records.byteLength, seats.length * 32);
  assert.equal(packed.mismatch, false);
  const f32 = new Float32Array(packed.records.buffer, packed.records.byteOffset, seats.length * 8);
  const u16 = new Uint16Array(packed.records.buffer, packed.records.byteOffset, seats.length * 16);
  const u8 = packed.records;
  for (const i of [0, 1, 777, seats.length - 1]) {
    const s = seats[i];
    assert.equal(f32[i * 8], Math.fround(s.x)); assert.equal(f32[i * 8 + 3], Math.fround(s.yaw)); assert.equal(f32[i * 8 + 4], Math.fround(s.wave));
    assert.equal(u16[i * 16 + 10], s.section); assert.equal(u16[i * 16 + 11], s.phase);
    assert.deepEqual([u8[i * 32 + 24], u8[i * 32 + 25], u8[i * 32 + 26], u8[i * 32 + 27], u8[i * 32 + 28]], [s.color, s.accent, s.prop, s.variant, s.eagerness]);
  }
  let next = 0;
  for (let b = 0; b < packed.blocks.length / 3; b++) {
    const first = packed.blocks[b * 3], count = packed.blocks[b * 3 + 1], section = packed.blocks[b * 3 + 2];
    assert.equal(first, next, "blocks are contiguous");
    assert.ok(count >= 1 && count <= c.SCENE_CROWD_BLOCK_SEATS);
    const [x, y, z, r] = Array.from(packed.spheres.slice(b * 4, b * 4 + 4));
    for (let i = first; i < first + count; i++) {
      assert.equal(seats[i].section, section, "a block holds one section");
      assert.ok(Math.hypot(seats[i].x - x, seats[i].y - y, seats[i].z - z) <= r, "the sphere holds the fan");
    }
    next = first + count;
  }
  assert.equal(next, seats.length);
  let fans = 0;
  for (const n of packed.sectionFans) fans += n;
  assert.equal(fans, seats.length);
});

test("the palette bakes every action as a closed loop of finite matrices", () => {
  const { c } = runtime();
  const bake = c.sceneCrowdBake(1.75);
  assert.equal(bake.width, 44);
  assert.equal(bake.height, 1 + 9 * 25);
  for (const v of bake.data) assert.ok(Number.isFinite(v));
  // Row 0, the bind pose, skins every joint with the identity.
  for (let j = 0; j < 11; j++) for (let e = 0; e < 16; e++) {
    assert.ok(Math.abs(bake.data[(j * 4) * 4 + e] - (e % 5 === 0 ? 1 : 0)) < 1e-6, `bind joint ${j} element ${e}`);
  }
  const rowOf = (a, i) => bake.clips[a * 4] + i;
  const texel = (row, j) => bake.data.subarray((row * bake.width + j * 4) * 4, (row * bake.width + j * 4) * 4 + 16);
  for (let a = 0; a < 8; a++) {
    assert.equal(bake.clips[a * 4 + 1], 24);
    for (let j = 0; j < 11; j++) assert.deepEqual(Array.from(texel(rowOf(a, 24), j)), Array.from(texel(rowOf(a, 0), j)), `action ${a} loops`);
  }
  // The hip moves: seated idle lowers it, a jump lifts it at mid-air.
  const hipY = (a, t) => c.sceneCrowdSkinMatrices(c.sceneCrowdActionPose(a, t), 1.75)[0][13] + 0.95;
  assert.ok(hipY(0, 0) < 0.6, "idle sits");
  assert.ok(Math.abs(hipY(1, 0) - 0.95) < 0.05, "stand stands");
  assert.ok(hipY(5, 0.5) > 1.15, "jump leaves the ground");
  // The wave clip rises with hands overhead at its middle and sits at both ends.
  const hand = (a, t) => { const m = c.sceneCrowdSkinMatrices(c.sceneCrowdActionPose(a, t), 1.75)[4]; return m[1] * 0.22 + m[5] * 0.86 + m[13]; };
  assert.ok(hand(8, 0.5) > 1.9 && hand(8, 0) < 1.0 && hand(8, 1) < 1.0, "wave hand heights " + [hand(8, 0), hand(8, 0.5), hand(8, 1)]);
  assert.ok(hand(2, 0) > 1.9, "cheer raises the fists");
});

test("meshes are closed, outward-wound and use the 11-joint skeleton", () => {
  const { c } = runtime();
  const meshes = c.sceneCrowdMeshes(1.75);
  assert.equal(meshes.length, 3);
  const tris = meshes.map((m) => m.indexCount / 3);
  assert.ok(tris[0] > tris[1] && tris[1] > tris[2] && tris[2] <= 12, "full > low > impostor: " + tris);
  for (const mesh of meshes) {
    const f32 = new Float32Array(mesh.vertices.buffer, mesh.vertices.byteOffset, mesh.vertexCount * 9), u8 = mesh.vertices;
    for (let v = 0; v < mesh.vertexCount; v++) {
      assert.ok(u8[v * 36 + 24] < 11, "joint index");
      assert.ok(u8[v * 36 + 32] <= 5 && u8[v * 36 + 33] <= 5, "slot and prop kind");
    }
    for (let i = 0; i < mesh.indexCount; i += 3) {
      const [a, b, d] = [mesh.indices[i], mesh.indices[i + 1], mesh.indices[i + 2]];
      assert.ok(a < mesh.vertexCount && b < mesh.vertexCount && d < mesh.vertexCount);
      const p = (k) => [f32[k * 9], f32[k * 9 + 1], f32[k * 9 + 2]];
      const [p0, p1, p2] = [p(a), p(b), p(d)];
      const e1 = p1.map((x, k) => x - p0[k]), e2 = p2.map((x, k) => x - p0[k]);
      const n = [e1[1] * e2[2] - e1[2] * e2[1], e1[2] * e2[0] - e1[0] * e2[2], e1[0] * e2[1] - e1[1] * e2[0]];
      const vn = [0, 1, 2].map((k) => f32[a * 9 + 3 + k] + f32[b * 9 + 3 + k] + f32[d * 9 + 3 + k]);
      assert.ok(n[0] * vn[0] + n[1] * vn[1] + n[2] * vn[2] > 0, "triangle " + i / 3 + " faces its normals");
    }
  }
});

function crowdFrame({ crowd = "bowl", now = 0, sections = [], waves = [], replace = false }) {
  const id = new TextEncoder().encode(crowd);
  const size = 4 + 2 + id.length + 8 + 2 + sections.length * 42 + 2 + waves.length * 20;
  const buf = new ArrayBuffer(size), view = new DataView(buf), bytes = new Uint8Array(buf);
  bytes.set([71, 83, 67, 49]); let at = 4;
  view.setUint16(at, id.length, true); at += 2; bytes.set(id, at); at += id.length;
  view.setFloat64(at, now, true); at += 8; view.setUint16(at, sections.length, true); at += 2;
  for (const s of sections) {
    view.setUint16(at, s.section, true); bytes[at + 2] = s.from; bytes[at + 3] = s.to;
    [s.blendStart - now, s.blend, s.excitementFrom, s.excitementTo, s.rate, s.epoch - now, s.jitter, (s.excitementStart ?? s.blendStart) - now, s.excitementRamp ?? 1].forEach((v, k) => view.setFloat32(at + 4 + k * 4, v, true));
    bytes[at + 40] = s.flags; at += 42;
  }
  bytes[at] = waves.length; bytes[at + 1] = replace ? 1 : 0; at += 2;
  for (const w of waves) { [w.start - now, w.origin, w.speed, w.width, w.laps].forEach((v, k) => view.setFloat32(at + k * 4, v, true)); at += 20; }
  return bytes;
}

test("crowd frames decode strictly and map the sender clock onto the scene clock", () => {
  const { c } = runtime();
  const mount = { id: "stadium", getAttribute() { return null; } };
  const ir = bowl().ir;
  const frame = crowdFrame({ now: 5, sections: [{ section: 2, from: 0, to: 3, blendStart: 6, blend: 0.5, excitementFrom: 0.1, excitementTo: 0.9, rate: 1.25, epoch: 6, jitter: 0.05, flags: 1 | 4 }],
    waves: [{ start: 7, origin: 0.25, speed: 0.1, width: 0.05, laps: 2 }] });
  assert.throws(() => c.sceneCrowdDecode(frame.subarray(0, frame.length - 1)), /truncated/);
  assert.throws(() => c.sceneCrowdDecode(new Uint8Array([1, 2, 3, 4])), /GSC1/);
  const padded = new Uint8Array(frame.length + 1); padded.set(frame);
  assert.throws(() => c.sceneCrowdDecode(padded), /trailing/);
  // A frame that arrives before the crowd draws waits for it.
  assert.equal(c.sceneCrowdApply(mount, frame), false);
  c.sceneCrowdNoteClock(mount, 100);
  const store = c.sceneCrowdStore(mount, ir);
  const o = 2 * c.SCENE_CROWD_SECTION_TEXELS * 4, d = store.sections;
  assert.deepEqual([d[o], d[o + 1], d[o + 2], d[o + 3]], [0, 3, 101, 0.5], "blend start moved by the clock offset (100 - 5)");
  assert.ok(Math.abs(d[o + 4] - 0.1) < 1e-6 && Math.abs(d[o + 5] - 0.9) < 1e-6 && d[o + 6] === 1.25);
  assert.equal(d[o + 7], 1 | 4, "synced, props raised, declared flags kept");
  assert.equal(d[o + 8], 101);
  assert.deepEqual([d[o + 10], d[o + 11]], [101, 1], "excitement ramp start and length");
  assert.deepEqual(Array.from(store.waves.slice(0, 5)).map((v) => Math.round(v * 1000) / 1000), [102, 0.25, 0.1, 0.05, 2]);
  // A later frame within the clock slack keeps the first offset.
  c.sceneCrowdNoteClock(mount, 110);
  c.sceneCrowdApply(mount, crowdFrame({ now: 15.1, sections: [{ section: 2, from: 3, to: 2, blendStart: 15.1, blend: 0.3, excitementFrom: 0.9, excitementTo: 1, rate: 1, epoch: 0, jitter: 0, flags: 4 }] }));
  assert.ok(Math.abs(d[o + 2] - 110.1) < 1e-4, "offset kept: " + d[o + 2]);
  // The standing flag from the declaration survives frames.
  const standing = ir.sections.findIndex((s) => s.standing);
  assert.ok(standing >= 0);
  assert.equal(store.sections[standing * c.SCENE_CROWD_SECTION_TEXELS * 4 + 7] & 8, 8);
});

test("levels report fan-weighted loudness for audio", () => {
  const { c } = runtime({ document: { getElementById: () => null } });
  const mount = { id: "levels", getAttribute() { return null; } };
  const ir = bowl().ir;
  const store = c.sceneCrowdStore(mount, ir);
  store.fans = c.sceneCrowdPack(ir).sectionFans;
  store.lastNow = 50;
  const quiet = c.sceneCrowdLevels(mount, "bowl");
  assert.ok(quiet.loudness < 0.05);
  for (let i = 0; i < ir.sections.length; i++) { const o = i * c.SCENE_CROWD_SECTION_TEXELS * 4; store.sections.set([2, 2, 0, 0.5, 1, 1], o); store.sections.set([0, 0.04, 0, 0.5], o + 8); }
  const loud = c.sceneCrowdLevels(mount, "bowl");
  assert.ok(loud.loudness > 0.95, "everyone cheering: " + loud.loudness);
  assert.equal(loud.sections[0].action, "cheer");
});

function lookAt(eye, target, fovY, aspect) {
  const f = [target[0] - eye[0], target[1] - eye[1], target[2] - eye[2]];
  const fl = Math.hypot(...f); f.forEach((v, i) => (f[i] = v / fl));
  let s = [f[1] * 0 - f[2] * 1, f[2] * 0 - f[0] * 0, f[0] * 1 - f[1] * 0];
  const sl = Math.hypot(...s); s = s.map((v) => v / sl);
  const u = [s[1] * f[2] - s[2] * f[1], s[2] * f[0] - s[0] * f[2], s[0] * f[1] - s[1] * f[0]];
  const view = [s[0], u[0], -f[0], 0, s[1], u[1], -f[1], 0, s[2], u[2], -f[2], 0,
    -(s[0] * eye[0] + s[1] * eye[1] + s[2] * eye[2]), -(u[0] * eye[0] + u[1] * eye[1] + u[2] * eye[2]), f[0] * eye[0] + f[1] * eye[1] + f[2] * eye[2], 1];
  const t = 1 / Math.tan(fovY / 2), near = 0.5, far = 2000;
  const proj = [t / aspect, 0, 0, 0, 0, t, 0, 0, 0, 0, (far + near) / (near - far), -1, 0, 0, 2 * far * near / (near - far), 0];
  const vp = new Float32Array(16);
  for (let col = 0; col < 4; col++) for (let row = 0; row < 4; row++) {
    let v = 0; for (let k = 0; k < 4; k++) v += proj[k * 4 + row] * view[col * 4 + k]; vp[col * 4 + row] = v;
  }
  return { viewProj: vp, proj: new Float32Array(proj), camera: { x: eye[0], y: eye[1], z: eye[2] } };
}

test("selection culls blocks outside the view and grades the rest by height on screen", () => {
  const { c } = runtime();
  const ir = bowl().ir, packed = c.sceneCrowdPack(ir), gpu = { model: c.sceneCrowdModel(ir) };
  // A broadcast camera high over the +Z sideline, looking at midfield.
  const wide = c.sceneCrowdSelect(gpu, packed, ir, { ...lookAt([0, 40, 110], [0, 0, 0], Math.PI / 3, 16 / 9), height: 1080 });
  const visible = wide.fans[0] + wide.fans[1] + wide.fans[2];
  assert.ok(visible > packed.count * 0.4 && visible < packed.count, "a broadcast view culls the stand behind it: " + visible + " of " + packed.count);
  assert.ok(wide.fans[2] > wide.fans[0], "most fans are impostors from far away");
  // Ranges merge neighbours: far fewer draws than blocks.
  const draws = wide.ranges.reduce((n, list) => n + list.length / 2, 0);
  assert.ok(draws < packed.blocks.length / 3 / 2, "draws " + draws);
  // Down in the stands, the nearest fans get the full mesh, capped by MaxFull.
  const close = c.sceneCrowdSelect({ model: gpu.model }, packed, { ...ir, lod: { ...ir.lod, maxFull: 500 } }, { ...lookAt([0, 6, 48], [0, 3, 30], Math.PI / 3, 16 / 9), height: 1080 });
  assert.ok(close.fans[0] > 0 && close.fans[0] <= 500, "full fans " + close.fans[0]);
  assert.ok(close.fans[1] > 0, "the budget demotes the rest to the low mesh");
});

test("uniform block layout", () => {
  const { c } = runtime();
  const ir = bowl().ir, bake = c.sceneCrowdBake(1.75);
  const store = { waves: new Float32Array(32), sections: c.sceneCrowdInitialSections(ir) };
  store.waves.set([3, 0.5, 0.1, 0.04, 2]);
  const out = c.sceneCrowdUniforms(ir, store, bake, c.sceneCrowdModel(ir), 12.5, new Float32Array(128));
  assert.equal(out[15], 1); assert.equal(out[16], 12.5);
  assert.equal(out[23], ir.fan.skinTones.length);
  assert.deepEqual(Array.from(out.slice(56, 60)), [1, 24, 4, -1], "idle clip");
  assert.ok(Math.abs(out[93] - 0.5) < 1e-6 && out[96] === 2, "wave A and laps");
  assert.equal(out[124], ir.sections.length);
});

function rendererContext(backend) {
  const env = createContext({});
  for (const file of ["10-runtime-primitives.ts", "10-runtime-scene-utils.ts", "11-scene-math.ts", "13-scene-material.ts", "16c1-scene-detail.ts"]) {
    vm.runInContext(fs.readFileSync(new URL("./bootstrap-src/" + file, import.meta.url), "utf8"), env.context);
  }
  const core = fs.readFileSync(new URL("./bootstrap-src/10-runtime-scene-core.ts", import.meta.url), "utf8");
  vm.runInContext(core.slice(0, core.indexOf("// Scene3D shared API")), env.context);
  vm.runInContext(transpile(readSceneRendererBackendSrc(backend)), env.context);
  return env.context;
}

function tool(name, args) {
  try { execFileSync(name, args, { stdio: "ignore" }); return true; } catch { return false; }
}
const hasNaga = tool("naga", ["--version"]);
const hasGlslang = tool("glslangValidator", ["--version"]);

test("the WGSL crowd pass composes with the renderer's frame structs and PBR fragment", { skip: hasNaga ? false : "naga is not on PATH" }, () => {
  const { c } = runtime();
  const ctx = rendererContext("webgpu");
  const frameStructs = vm.runInContext("WGSL_FRAME_STRUCTS", ctx), fragment = vm.runInContext("WGSL_PBR_FRAGMENT", ctx);
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), "gosx-crowd-"));
  try {
    for (const tier of [0, 1, 2]) {
      const file = path.join(dir, `crowd-vertex-${tier}.wgsl`);
      fs.writeFileSync(file, c.sceneCrowdVertexWGSL(frameStructs).replace("override CROWD_TIER: u32 = 0u;", `const CROWD_TIER: u32 = ${tier}u;`));
      execFileSync("naga", [file], { stdio: "pipe" });
    }
    // The shipped bundle strips indentation and renames shader locals; the
    // patch must hold for both forms.
    const shipped = fragment.split("\n").map((line) => line.trim()).join("\n").replace(/\balphaEnabled\b/g, "_r").replace(/\bcoverage\b/g, "_x");
    for (const [name, source] of [["source", fragment], ["shipped", shipped]]) {
      const frag = path.join(dir, "crowd-fragment-" + name + ".wgsl");
      const patched = c.sceneCrowdFragmentWGSL(source);
      assert.match(patched, /@location\(6\) @interpolate\(flat\) crowdGlow: vec4f/);
      assert.equal(patched.split("crowdGlowed(in, gosxPBRColor(in))").length - 1, 2, name + ": both entry points add the glow");
      fs.writeFileSync(frag, patched);
      execFileSync("naga", [frag], { stdio: "pipe" });
    }
  } finally {
    fs.rmSync(dir, { recursive: true, force: true });
  }
});

test("the GLSL crowd pass compiles against the renderer's PBR fragment", { skip: hasGlslang ? false : "glslangValidator is not on PATH" }, () => {
  const { c } = runtime();
  const ctx = rendererContext("webgl");
  const fragment = vm.runInContext("SCENE_PBR_FRAGMENT_SOURCE", ctx);
  const patched = c.sceneCrowdFragmentGLSL(fragment);
  assert.match(patched, /flat in vec4 v_crowdGlow;/);
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), "gosx-crowd-"));
  try {
    for (const tier of [0, 1, 2]) {
      const file = path.join(dir, `crowd-${tier}.vert`);
      fs.writeFileSync(file, c.SCENE_CROWD_GLSL_VERTEX.replace("precision highp int;", "precision highp int;\n#define CROWD_TIER " + tier));
      execFileSync("glslangValidator", [file], { stdio: "pipe" });
    }
    const frag = path.join(dir, "crowd.frag");
    fs.writeFileSync(frag, patched);
    execFileSync("glslangValidator", [frag], { stdio: "pipe" });
    // Every fragment input is a vertex output.
    const outputs = new Set([...c.SCENE_CROWD_GLSL_VERTEX.matchAll(/\bout\s+(?:\w+\s+)?\w+\s+(v_\w+);/g)].map((m) => m[1]));
    for (const m of patched.matchAll(/^\s*(?:flat\s+)?in\s+\w+\s+(v_\w+);/gm)) assert.ok(outputs.has(m[1]), "vertex shader writes " + m[1]);
  } finally {
    fs.rmSync(dir, { recursive: true, force: true });
  }
});

test("renderer hooks stay single statements on existing lines", () => {
  const gl = readSceneRendererBackendSrc("webgl"), gpu = readSceneRendererBackendSrc("webgpu");
  assert.equal(gl.match(/window\.__gosx_scene3d_crowd_api\?\.webgl\(gl, \{/g).length, 1);
  assert.equal(gpu.match(/window\.__gosx_scene3d_crowd_api\?\.webgpu\(mainPass, \{/g).length, 1);
  assert.equal(gl.match(/window\.__gosx_scene3d_crowd_api\?\.dispose\(gl\)/g).length, 1);
  assert.equal(gpu.match(/window\.__gosx_scene3d_crowd_api\?\.dispose\(device\)/g).length, 1);
  // The anchors the fragment patches need.
  assert.ok(gl.includes("void gosxApplyCustomFragment(inout vec3 color, inout float opacity, vec3 normal, vec3 worldPosition, vec2 uv) {}"));
  assert.equal(gpu.match(/gosxPBRColor\(in\)/g).length, 2, "two fragment entry points call gosxPBRColor(in)");
});
