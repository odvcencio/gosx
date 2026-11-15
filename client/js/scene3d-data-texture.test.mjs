// Tests for client/runtime/scene3d/data-texture.ts, the lazy chunk that stores
// host-written Selena data textures and uploads them to WebGPU and WebGL2, and
// for the renderer-side shims and hooks that reach it.
//
// The decoder is checked against bytes the real Go encoder produced
// (client/js/testdata/data-texture-go-writer-fixture, backed by
// scene.DataTextureEncoder) so a bug in one language cannot hide in the other.
// The uploaders run against stand-in devices and contexts that record calls.
// Real-GPU readback is covered by the e2e check named in docs/scene3d-selena.md.

import test from "node:test";
import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import vm from "node:vm";
import { execFileSync } from "node:child_process";
import { fileURLToPath } from "node:url";
import { createRequire } from "node:module";

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const repoRoot = path.join(__dirname, "..", "..");
const load = createRequire(import.meta.url);
const { readSceneRendererBackendSrc } = load("./scene3d-renderer-source-set.js");
const ts = load("../runtime/node_modules/typescript");
const read = (...parts) => fs.readFileSync(path.join(__dirname, ...parts), "utf8");
const chunkSource = read("..", "runtime", "scene3d", "data-texture.ts");
const shimSource = read("bootstrap-src", "16c3-scene-data-texture-shims.ts");
const transpile = (source) => ts.transpileModule(source, { compilerOptions: { target: ts.ScriptTarget.ES2020 } }).outputText;

// loadChunk evaluates the chunk against a fresh window and returns it, with the
// errors it logged and the events it dispatched.
function loadChunk() {
  const errors = [], events = [];
  const window = { dispatchEvent: (event) => { events.push(event); return true; } };
  const console = { error: (...args) => errors.push(args.join(" ")) };
  class CustomEvent { constructor(type, init) { this.type = type; this.detail = init && init.detail; } }
  new Function("window", "console", "CustomEvent", "TextDecoder", "GPUTextureUsage", chunkSource)(
    window, console, CustomEvent, TextDecoder, { TEXTURE_BINDING: 4, COPY_DST: 2 });
  return { api: window.__gosx_scene3d_data_texture_api, window, errors, events };
}

let fixtureCache;
function goFrames() {
  if (!fixtureCache) {
    const stdout = execFileSync(process.env.GOSX_GO || "go", ["run", "./client/js/testdata/data-texture-go-writer-fixture"], { cwd: repoRoot, encoding: "utf8" });
    fixtureCache = Object.fromEntries(JSON.parse(stdout).map((entry) => [entry.name, new Uint8Array(Buffer.from(entry.bytes, "base64"))]));
  }
  return fixtureCache;
}

// half decodes an IEEE binary16 value.
function half(bits) {
  const sign = bits & 0x8000 ? -1 : 1, exp = (bits >> 10) & 31, frac = bits & 1023;
  if (exp === 0) return sign * frac * 2 ** -24;
  if (exp === 31) return frac ? NaN : sign * Infinity;
  return sign * (1 + frac / 1024) * 2 ** (exp - 15);
}

const material = (name, ref) => ({ customUniforms: { [name]: ref } });
const spectrum = (extra = {}) => ({ name: "spectrum", dimension: "2d", format: "r16float", sampleMode: "load", ...extra });

// Frame builder for hand-made (non-Go) frames; mirrors the documented wire format.
function frame({ key = "k", op = 0, format = 0, revision = 1, width = 2, height = 2, x = 0, y = 0, w = 0, h = 0, texels = [], magic = "GSXT", keyLen }) {
  const keyBytes = Buffer.from(key), payload = (44 + keyBytes.length + 3) & ~3;
  const out = new Uint8Array(payload + texels.length), view = new DataView(out.buffer);
  out.set(Buffer.from(magic));
  out[4] = 1; out[5] = op; out[6] = format;
  view.setUint32(8, revision % 2 ** 32, true); view.setUint32(12, Math.floor(revision / 2 ** 32), true);
  [width, height, x, y, w, h].forEach((v, i) => view.setUint32(16 + 4 * i, v, true));
  view.setUint16(40, keyLen === undefined ? keyBytes.length : keyLen, true);
  out.set(keyBytes, 44);
  out.set(texels, payload);
  return out;
}

// --- decode ---

test("parse reads real Go frames: rectangle, whole-texture size, key padding and revision", () => {
  const { api } = loadChunk();
  const frames = goFrames();
  const create = api.parse(frames["ring-create"]);
  assert.deepEqual({ op: create.op, format: create.format, revision: create.revision, width: create.width, height: create.height, key: create.key, n: create.texels.length },
    { op: 1, format: 3, revision: 1, width: 4, height: 2, key: "ring", n: 0 });
  const rect = api.parse(frames["ring-rect"]);
  assert.deepEqual({ op: rect.op, revision: rect.revision, x: rect.x, y: rect.y, w: rect.w, h: rect.h, width: rect.width, n: rect.texels.length },
    { op: 0, revision: 2, x: 1, y: 0, w: 2, h: 2, width: 4, n: 8 });
  assert.deepEqual([...new Uint16Array(rect.texels.slice().buffer)].map(half), [0.25, 0.5, 1, 2]);
  assert.equal(api.parse(frames["tiny-full"]).key, "rgba/tiny");
  assert.equal(api.parse(frames["f32-delete"]).op, 2);
});

test("parse rejects malformed frames with a reason", () => {
  const { api } = loadChunk();
  const ok = { format: 0, width: 2, height: 2, w: 2, h: 1, texels: new Array(8).fill(1) };
  for (const [name, bytes, want] of [
    ["null", null, /not a GSXT/],
    ["short", new Uint8Array(10), /not a GSXT/],
    ["magic", frame({ ...ok, magic: "GSXI" }), /not a GSXT/],
    ["key past the end", frame({ ...ok, keyLen: 900 }), /shorter than its key/],
    ["empty key", frame({ ...ok, key: "" }), /empty key/],
    ["zero revision", frame({ ...ok, revision: 0 }), /revision/],
    ["unknown op", frame({ ...ok, op: 7 }), /unknown op 7/],
    ["unknown format", frame({ ...ok, format: 9 }), /unknown format 9/],
    ["zero size", frame({ ...ok, width: 0 }), /outside the supported range/],
    ["huge size", frame({ ...ok, width: 9000 }), /outside the supported range/],
    ["too many bytes", frame({ ...ok, format: 2, width: 8192, height: 8192 }), /outside the supported range/],
    ["empty rect", frame({ ...ok, w: 0 }), /falls outside/],
    ["rect past edge", frame({ ...ok, x: 1 }), /falls outside/],
    ["short texels", frame({ ...ok, texels: [1, 2, 3] }), /needs 8 bytes, got 3/],
  ]) {
    const result = api.parse(bytes);
    assert.equal(typeof result, "string", name);
    assert.match(result, want, name);
  }
  assert.equal(typeof api.parse(frame({ ...ok })), "object");
});

// --- store: apply ---

test("apply creates, writes, deletes, and drops stale or malformed frames visibly", () => {
  const { api, errors, events } = loadChunk();
  const renders = [];
  const schedule = (reason) => renders.push(reason);
  const frames = goFrames();
  assert.deepEqual(api.apply(frames["ring-create"], schedule), { applied: true, revision: 1 });
  assert.deepEqual(api.info("ring"), { key: "ring", width: 4, height: 2, format: "r16float", seq: 0 });
  assert.equal(api.apply(frames["ring-rect"], schedule).applied, true);
  assert.equal(api.apply(frames["ring-row"], schedule).applied, true);
  assert.equal(api.info("ring").seq, 2);
  assert.deepEqual(renders, ["data-texture", "data-texture", "data-texture"]);
  assert.deepEqual(api.apply(frames["ring-rect"], schedule), { applied: false, reason: "stale-revision" }, "replayed revision");
  assert.equal(renders.length, 3, "a dropped frame schedules no render");

  const mount = { events: [], dispatchEvent(event) { this.events.push(event); return true; } };
  const bad = api.apply(new Uint8Array([1, 2, 3]), schedule, mount);
  assert.equal(bad.applied, false);
  assert.match(bad.reason, /not a GSXT/);
  assert.equal(mount.events[0].type, "gosx:scene3d:data-texture-error", "a rejected frame reports on the mount");
  assert.match(errors[0], /scene3d data texture/);
  api.apply(new Uint8Array([1, 2, 3]), schedule, mount);
  assert.equal(errors.length, 1, "one cause logs once");

  const mismatch = api.apply(frame({ key: "ring", revision: 10, format: 3, width: 8, height: 2, w: 1, h: 1, texels: [0, 0] }), schedule);
  assert.match(mismatch.reason, /describes a 8x2 r16float texture, but the texture is 4x2 r16float; create it again to resize/);
  assert.equal(api.info("ring").width, 4, "a mismatched write leaves the texture alone");

  assert.equal(api.apply(frames["f32-create"], schedule).applied, true);
  assert.equal(api.apply(frames["f32-delete"], schedule).applied, true);
  assert.equal(api.info("f32"), null);
  assert.equal(events.length, 1, "a failure with no mount reports on window");
  assert.equal(events[0].detail.key, "ring");
});

test("a write to an unknown key creates it, and a revision never goes backwards across delete", () => {
  const { api } = loadChunk();
  assert.equal(api.apply(frame({ key: "late", revision: 5, format: 0, width: 2, height: 1, w: 1, h: 1, texels: [1, 2, 3, 4] })).applied, true);
  assert.deepEqual(api.info("late"), { key: "late", width: 2, height: 1, format: "rgba8unorm", seq: 1 });
  assert.equal(api.apply(frame({ key: "late", revision: 6, op: 2 })).applied, true);
  assert.equal(api.apply(frame({ key: "late", revision: 5, format: 0, width: 2, height: 1, op: 1 })).reason, "stale-revision");
  assert.equal(api.apply(frame({ key: "late", revision: 7, format: 0, width: 2, height: 1, op: 1 })).applied, true);
});

test("revisions above 2^32 decode", () => {
  const { api } = loadChunk();
  const revision = 2 ** 40 + 3;
  assert.equal(api.apply(frame({ key: "big", revision, op: 1 })).revision, revision);
});

// --- WebGPU ---

function fakeDevice({ maxTextureDimension2D = 8192 } = {}) {
  const device = { textures: [], writes: [], limits: { maxTextureDimension2D } };
  device.createTexture = (descriptor) => {
    const texture = { descriptor, destroyed: false, view: { of: descriptor.label }, createView() { return this.view; }, destroy() { this.destroyed = true; } };
    device.textures.push(texture);
    return texture;
  };
  device.queue = {
    writeTexture(destination, data, layout, size) {
      device.writes.push({ texture: destination.texture, origin: destination.origin, layout, size, data });
    },
  };
  return device;
}

// texelBytes pulls the rows a recorded writeTexture call would copy.
function rows(write, bytesPerTexel) {
  const out = [];
  for (let row = 0; row < write.size[1]; row++) {
    const start = write.layout.offset + row * write.layout.bytesPerRow;
    out.push(Array.from(write.data.subarray(start, start + write.size[0] * bytesPerTexel)));
  }
  return out;
}

test("WebGPU: creates the texture in the declared format and uploads only the changed rectangle", () => {
  const { api } = loadChunk();
  const frames = goFrames();
  const device = fakeDevice();
  const mat = material("spectrum", "gosx:data:ring");
  assert.equal(api.webgpuView(device, mat, spectrum()), null, "an unwritten key binds the placeholder");
  for (const name of ["ring-create", "ring-rect", "ring-row"]) api.apply(frames[name]);

  const view = api.webgpuView(device, mat, spectrum());
  assert.ok(view);
  assert.equal(device.textures.length, 1);
  assert.deepEqual({ size: device.textures[0].descriptor.size, format: device.textures[0].descriptor.format, usage: device.textures[0].descriptor.usage },
    { size: [4, 2], format: "r16float", usage: 4 | 2 });
  // Two writes happened before the first bind, so the union of their rectangles uploads once.
  assert.equal(device.writes.length, 1);
  const first = device.writes[0];
  assert.deepEqual([first.origin, first.size, first.layout.bytesPerRow, first.layout.rowsPerImage], [[0, 0], [4, 2], 8, 2]);
  const decoded = rows(first, 2).map((row) => {
    const u16 = new Uint16Array(new Uint8Array(row).buffer);
    return Array.from(u16, half);
  });
  assert.deepEqual(decoded, [[0, 0.25, 0.5, 0], [-1, 0, 65504, 0.00006097555160522461]]);

  assert.equal(api.webgpuView(device, mat, spectrum()), view, "a clean texture uploads nothing");
  assert.equal(device.writes.length, 1);

  api.apply(frame({ key: "ring", revision: 4, format: 3, width: 4, height: 2, x: 3, y: 1, w: 1, h: 1, texels: [0x00, 0x3c] }));
  api.webgpuView(device, mat, spectrum());
  assert.equal(device.writes.length, 2);
  const second = device.writes[1];
  assert.deepEqual([second.origin, second.size, second.layout.offset], [[3, 1], [1, 1], 1 * 8 + 3 * 2], "only the dirty rectangle is copied");
  assert.deepEqual(rows(second, 2), [[0x00, 0x3c]]);
  assert.equal(device.textures.length, 1, "a write keeps the texture");
});

test("WebGPU: all five formats map to the matching texture format and texel size", () => {
  const { api } = loadChunk();
  const device = fakeDevice();
  [["rgba8unorm", 0, 4], ["rgba16float", 1, 8], ["rgba32float", 2, 16], ["r16float", 3, 2], ["r32float", 4, 4]].forEach(([name, index, bytes], i) => {
    const key = "f" + i;
    api.apply(frame({ key, op: 1, format: index, width: 3, height: 2 }));
    api.apply(frame({ key, revision: 2, format: index, width: 3, height: 2, x: 1, y: 1, w: 2, h: 1, texels: new Array(2 * bytes).fill(7) }));
    assert.ok(api.webgpuView(device, material("t", "gosx:data:" + key), { name: "t", format: name }), name);
    assert.equal(device.textures.at(-1).descriptor.format, name);
    assert.deepEqual([device.writes.at(-1).origin, device.writes.at(-1).size, device.writes.at(-1).layout.bytesPerRow], [[1, 1], [2, 1], 3 * bytes], name);
  });
});

test("WebGPU: a descriptor without a format means rgba8unorm", () => {
  const { api, errors } = loadChunk();
  api.apply(frame({ key: "img", op: 1, format: 0 }));
  assert.ok(api.webgpuView(fakeDevice(), material("t", "gosx:data:img"), { name: "t" }));
  api.apply(frame({ key: "f", op: 1, format: 3 }));
  assert.equal(api.webgpuView(fakeDevice(), material("t", "gosx:data:f"), { name: "t" }), null);
  assert.match(errors[0], /texture is r16float but the material declares rgba8unorm/);
});

test("WebGPU: format mismatch, oversize texture and non-data values bind the placeholder visibly", () => {
  const { api, errors, events } = loadChunk();
  api.apply(frame({ key: "f", op: 1, format: 4, width: 16, height: 1 }));
  assert.equal(api.webgpuView(fakeDevice(), material("spectrum", "gosx:data:f"), spectrum()), null);
  assert.match(errors[0], /"f": texture is r32float but the material declares r16float/);
  assert.equal(events.at(-1).type, "gosx:scene3d:data-texture-error");
  assert.equal(api.webgpuView(fakeDevice({ maxTextureDimension2D: 8 }), material("spectrum", "gosx:data:f"), { name: "spectrum", format: "r32float" }), null);
  assert.match(errors[1], /exceeds this device's maxTextureDimension2D 8/);
  for (const value of [undefined, "https://example.test/a.png", "gosx:environment:radiance", 3, { resource: "gosx:environment:radiance" }]) {
    assert.equal(api.webgpuView(fakeDevice(), material("spectrum", value), spectrum()), null);
  }
  assert.ok(api.webgpuView(fakeDevice(), material("spectrum", { resource: " gosx:data:f " }), { name: "spectrum", format: "r32float" }), "object and padded references resolve");
});

test("WebGPU: separate devices upload separately, and resize or delete releases the old texture", () => {
  const { api } = loadChunk();
  const a = fakeDevice(), b = fakeDevice(), mat = material("spectrum", "gosx:data:k");
  const tex = spectrum({ format: "rgba8unorm" });
  api.apply(frame({ key: "k", op: 1, format: 0, width: 2, height: 2 }));
  api.apply(frame({ key: "k", revision: 2, format: 0, width: 2, height: 2, w: 2, h: 2, texels: new Array(16).fill(5) }));
  const viewA = api.webgpuView(a, mat, tex), viewB = api.webgpuView(b, mat, tex);
  assert.notEqual(viewA, viewB);
  assert.equal(a.writes.length, 1);
  assert.equal(b.writes.length, 1, "a second device gets the full contents from the CPU copy");

  api.apply(frame({ key: "k", revision: 3, op: 1, format: 0, width: 4, height: 4 }));
  assert.equal(a.textures[0].destroyed, true, "create with a new size destroys the old GPU texture");
  assert.equal(b.textures[0].destroyed, true);
  const resized = api.webgpuView(a, mat, tex);
  assert.notEqual(resized, viewA, "the view changes so the renderer rebuilds its bind group");
  assert.deepEqual(a.textures[1].descriptor.size, [4, 4]);

  api.apply(frame({ key: "k", revision: 4, op: 2 }));
  assert.equal(a.textures[1].destroyed, true);
  assert.equal(api.webgpuView(a, mat, tex), null);
});

test("WebGPU: more writes than the log keeps upload the whole texture", () => {
  const { api } = loadChunk();
  const device = fakeDevice(), mat = material("spectrum", "gosx:data:big");
  const tex = spectrum({ format: "rgba8unorm" });
  api.apply(frame({ key: "big", op: 1, format: 0, width: 64, height: 1 }));
  api.webgpuView(device, mat, tex);
  for (let i = 0; i < 40; i++) api.apply(frame({ key: "big", revision: 2 + i, format: 0, width: 64, height: 1, x: i, w: 1, h: 1, texels: [i, i, i, i] }));
  api.webgpuView(device, mat, tex);
  assert.deepEqual([device.writes.at(-1).origin, device.writes.at(-1).size], [[0, 0], [64, 1]]);
  api.apply(frame({ key: "big", revision: 100, format: 0, width: 64, height: 1, x: 7, w: 2, h: 1, texels: new Array(8).fill(9) }));
  api.webgpuView(device, mat, tex);
  assert.deepEqual([device.writes.at(-1).origin, device.writes.at(-1).size], [[7, 0], [2, 1]], "after a full upload, writes are small again");
});

// --- WebGL2 ---

function fakeGL({ webgl2 = true, maxSize = 4096 } = {}) {
  const C = { TEXTURE_2D: 1, TEXTURE0: 100, TEXTURE_MIN_FILTER: 2, TEXTURE_MAG_FILTER: 3, TEXTURE_WRAP_S: 4, TEXTURE_WRAP_T: 5, NEAREST: 6, CLAMP_TO_EDGE: 7,
    UNPACK_ALIGNMENT: 8, UNPACK_ROW_LENGTH: 9, UNPACK_SKIP_PIXELS: 10, UNPACK_SKIP_ROWS: 11, MAX_TEXTURE_SIZE: 12,
    RGBA: 20, RED: 21, UNSIGNED_BYTE: 22, HALF_FLOAT: 23, FLOAT: 24 };
  if (webgl2) Object.assign(C, { RGBA8: 30, RGBA16F: 31, RGBA32F: 32, R16F: 33, R32F: 34 });
  const calls = [], state = { pixel: { 8: 4, 9: 0, 10: 0, 11: 0 }, textures: new Set() };
  let nextTexture = 1;
  const gl = {
    ...C, calls, state,
    getParameter: (p) => (p === C.MAX_TEXTURE_SIZE ? maxSize : 0),
    createTexture() { const t = { id: nextTexture++ }; state.textures.add(t); return t; },
    deleteTexture(t) { state.textures.delete(t); calls.push(["deleteTexture", t.id]); },
    isTexture: (t) => state.textures.has(t),
    activeTexture: (u) => calls.push(["activeTexture", u]),
    bindTexture: (target, t) => calls.push(["bindTexture", target, t && t.id]),
    texParameteri: (target, name, value) => calls.push(["texParameteri", name, value]),
    pixelStorei(name, value) { state.pixel[name] = value; calls.push(["pixelStorei", name, value]); },
    texImage2D: (...args) => calls.push(["texImage2D", ...args]),
    texSubImage2D: (...args) => calls.push(["texSubImage2D", ...args, { ...state.pixel }]),
    uniform1i: (loc, v) => calls.push(["uniform1i", loc, v]),
    uniform2f: (loc, x, y) => calls.push(["uniform2f", loc, x, y]),
    getUniformLocation: (program, name) => (program.uniforms.includes(name) ? "loc:" + name : null),
  };
  return gl;
}
const callsOf = (gl, name) => gl.calls.filter((c) => c[0] === name);
const info = (extra = {}) => ({ program: { uniforms: ["_texSize_spectrum"] }, layout: { requires: { glTexSizeUniforms: ["_texSize_spectrum"] }, ...extra } });

test("WebGL2: allocates with texImage2D, NEAREST and CLAMP_TO_EDGE, and uploads the rectangle with texSubImage2D", () => {
  const { api } = loadChunk();
  const frames = goFrames();
  const gl = fakeGL(), placeholder = { id: "placeholder" };
  for (const name of ["ring-create", "ring-rect", "ring-row"]) api.apply(frames[name]);
  assert.equal(api.webglBind(gl, info(), 3, "loc:sampler", material("spectrum", "gosx:data:ring"), spectrum(), placeholder), true);

  const alloc = callsOf(gl, "texImage2D");
  assert.deepEqual(alloc.map((c) => c.slice(1)), [[gl.TEXTURE_2D, 0, gl.R16F, 4, 2, 0, gl.RED, gl.HALF_FLOAT, null]]);
  const params = Object.fromEntries(callsOf(gl, "texParameteri").map((c) => [c[1], c[2]]));
  assert.deepEqual(params, { [gl.TEXTURE_MIN_FILTER]: gl.NEAREST, [gl.TEXTURE_MAG_FILTER]: gl.NEAREST, [gl.TEXTURE_WRAP_S]: gl.CLAMP_TO_EDGE, [gl.TEXTURE_WRAP_T]: gl.CLAMP_TO_EDGE });

  const sub = callsOf(gl, "texSubImage2D");
  assert.equal(sub.length, 1);
  const [, target, level, x, y, w, h, format, type, data, pixel] = sub[0];
  assert.deepEqual([target, level, x, y, w, h, format, type], [gl.TEXTURE_2D, 0, 0, 0, 4, 2, gl.RED, gl.HALF_FLOAT]);
  assert.ok(data instanceof Uint16Array, "half floats upload from a Uint16Array");
  assert.deepEqual(pixel, { 8: 1, 9: 4, 10: 0, 11: 0 }, "row length is the texture width so no row copy is needed");
  assert.deepEqual(Array.from(data, half).slice(0, 8), [0, 0.25, 0.5, 0, -1, 0, 65504, 0.00006097555160522461]);
  assert.deepEqual(gl.state.pixel, { 8: 4, 9: 0, 10: 0, 11: 0 }, "pixel-store state is restored");

  // The unit is selected before the texture is created or bound, and the sampler points at it.
  const names = gl.calls.map((c) => c[0]);
  assert.equal(names[0], "activeTexture");
  assert.deepEqual(gl.calls[0], ["activeTexture", gl.TEXTURE0 + 3]);
  assert.ok(names.indexOf("activeTexture") < names.indexOf("bindTexture"));
  assert.deepEqual(callsOf(gl, "uniform1i"), [["uniform1i", "loc:sampler", 3]]);
  assert.deepEqual(gl.calls.filter((c) => c[0] === "bindTexture").at(-1), ["bindTexture", gl.TEXTURE_2D, gl.calls.filter((c) => c[0] === "bindTexture")[0][2]], "the final bind is the data texture");
});

test("WebGL2: a partial write uploads a sub-rectangle with skip offsets; a clean texture uploads nothing", () => {
  const { api } = loadChunk();
  const gl = fakeGL(), mat = material("spectrum", "gosx:data:k"), tex = spectrum({ format: "rgba32float" });
  api.apply(frame({ key: "k", op: 1, format: 2, width: 8, height: 4 }));
  api.webglBind(gl, info(), 0, "s", mat, tex, null);
  assert.equal(callsOf(gl, "texSubImage2D").length, 0, "an unwritten texture needs no upload");
  api.apply(frame({ key: "k", revision: 2, format: 2, width: 8, height: 4, x: 2, y: 1, w: 3, h: 2, texels: new Array(3 * 2 * 16).fill(0) }));
  api.webglBind(gl, info(), 0, "s", mat, tex, null);
  const sub = callsOf(gl, "texSubImage2D");
  assert.equal(sub.length, 1);
  assert.deepEqual([sub[0][3], sub[0][4], sub[0][5], sub[0][6], sub[0][7], sub[0][8]], [2, 1, 3, 2, gl.RGBA, gl.FLOAT]);
  assert.deepEqual(sub[0][10], { 8: 1, 9: 8, 10: 2, 11: 1 });
  assert.ok(sub[0][9] instanceof Float32Array);
  assert.equal(callsOf(gl, "texImage2D")[0][3], gl.RGBA32F);
  api.webglBind(gl, info(), 0, "s", mat, tex, null);
  assert.equal(callsOf(gl, "texSubImage2D").length, 1);
});

test("WebGL2: every format picks its internal format, format and type", () => {
  const { api } = loadChunk();
  const gl = fakeGL();
  const want = [["rgba8unorm", "RGBA8", "RGBA", "UNSIGNED_BYTE", Uint8Array], ["rgba16float", "RGBA16F", "RGBA", "HALF_FLOAT", Uint16Array],
    ["rgba32float", "RGBA32F", "RGBA", "FLOAT", Float32Array], ["r16float", "R16F", "RED", "HALF_FLOAT", Uint16Array], ["r32float", "R32F", "RED", "FLOAT", Float32Array]];
  want.forEach(([name, internal, format, type, array], index) => {
    const key = "f" + index, bytes = [4, 8, 16, 2, 4][index];
    api.apply(frame({ key, op: 1, format: index, width: 2, height: 1 }));
    api.apply(frame({ key, revision: 2, format: index, width: 2, height: 1, w: 2, h: 1, texels: new Array(2 * bytes).fill(1) }));
    gl.calls.length = 0;
    api.webglBind(gl, info(), 0, "s", material("spectrum", "gosx:data:" + key), spectrum({ format: name }), null);
    const alloc = callsOf(gl, "texImage2D")[0], sub = callsOf(gl, "texSubImage2D")[0];
    assert.deepEqual([alloc[3], alloc[7], alloc[8]], [gl[internal], gl[format], gl[type]], name);
    assert.deepEqual([sub[7], sub[8], sub[9].constructor], [gl[format], gl[type], array], name);
  });
});

test("WebGL: sets the _texSize_ uniform only where requires lists it and the program has it", () => {
  const { api } = loadChunk();
  const gl = fakeGL(), mat = material("spectrum", "gosx:data:ring");
  api.apply(goFrames()["ring-create"]);
  api.webglBind(gl, info(), 2, "s", mat, spectrum(), null);
  assert.deepEqual(callsOf(gl, "uniform2f"), [["uniform2f", "loc:_texSize_spectrum", 4, 2]]);
  gl.calls.length = 0;
  api.webglBind(gl, { program: { uniforms: [] }, layout: info().layout }, 2, "s", mat, spectrum(), null);
  assert.equal(callsOf(gl, "uniform2f").length, 0, "the GLES 3.00 program has no such uniform");
  api.webglBind(gl, { program: info().program, layout: {} }, 2, "s", mat, spectrum(), null);
  assert.equal(callsOf(gl, "uniform2f").length, 0, "not listed, not set");
  gl.calls.length = 0;
  api.webglBind(gl, info(), 2, "s", material("spectrum", "gosx:data:missing"), spectrum(), { id: "ph" });
  assert.deepEqual(callsOf(gl, "uniform2f"), [["uniform2f", "loc:_texSize_spectrum", 1, 1]], "the placeholder is 1x1");
  assert.deepEqual(callsOf(gl, "bindTexture").at(-1), ["bindTexture", gl.TEXTURE_2D, "ph"], "the placeholder is bound");
});

test("WebGL: unbound and non-data textures", () => {
  const { api } = loadChunk();
  const gl = fakeGL(), placeholder = { id: "ph" };
  assert.equal(api.webglBind(gl, info(), 1, "s", material("spectrum", "gosx:data:none"), spectrum(), placeholder), true, "the hook owns an unwritten reference");
  assert.deepEqual(callsOf(gl, "uniform1i"), [["uniform1i", "s", 1]]);
  gl.calls.length = 0;
  for (const value of [undefined, "https://example.test/t.png", "gosx:environment:brdf-lut"]) {
    assert.equal(api.webglBind(gl, info(), 1, "s", material("spectrum", value), spectrum(), placeholder), false);
  }
  assert.equal(gl.calls.length, 0, "other textures are left to the ordinary binding path");
});

test("WebGL: a context without WebGL2 formats, or too small, fails visibly and binds the placeholder", () => {
  const { api, errors, events } = loadChunk();
  api.apply(goFrames()["ring-create"]);
  const old = fakeGL({ webgl2: false });
  assert.equal(api.webglBind(old, info(), 0, "s", material("spectrum", "gosx:data:ring"), spectrum(), null), true);
  assert.match(errors[0], /r16float needs a WebGL2 context/);
  assert.equal(events.at(-1).detail.key, "ring");
  assert.equal(callsOf(old, "texImage2D").length, 0);
  const small = fakeGL({ maxSize: 2 });
  api.webglBind(small, info(), 0, "s", material("spectrum", "gosx:data:ring"), spectrum(), null);
  assert.match(errors[1], /exceeds this context's MAX_TEXTURE_SIZE 2/);
});

test("WebGL: a restored context re-creates the texture from the CPU copy", () => {
  const { api } = loadChunk();
  const gl = fakeGL(), mat = material("spectrum", "gosx:data:k"), tex = spectrum({ format: "rgba8unorm" });
  api.apply(frame({ key: "k", op: 1, format: 0, width: 2, height: 1 }));
  api.apply(frame({ key: "k", revision: 2, format: 0, width: 2, height: 1, w: 2, h: 1, texels: [1, 2, 3, 4, 5, 6, 7, 8] }));
  api.webglBind(gl, info(), 0, "s", mat, tex, null);
  gl.state.textures.clear(); // context lost: every handle is invalid
  gl.calls.length = 0;
  api.webglBind(gl, info(), 0, "s", mat, tex, null);
  assert.equal(callsOf(gl, "texImage2D").length, 1);
  assert.deepEqual(Array.from(callsOf(gl, "texSubImage2D")[0][9]), [1, 2, 3, 4, 5, 6, 7, 8]);
});

test("WebGL: delete releases the GL texture", () => {
  const { api } = loadChunk();
  const gl = fakeGL(), mat = material("spectrum", "gosx:data:k");
  api.apply(frame({ key: "k", op: 1, format: 0, width: 1, height: 1 }));
  api.webglBind(gl, info(), 0, "s", mat, spectrum({ format: "rgba8unorm" }), null);
  api.apply(frame({ key: "k", revision: 2, op: 2 }));
  assert.equal(callsOf(gl, "deleteTexture").length, 1);
});

// --- renderer-side shims ---

function fn(source, name) {
  const start = source.indexOf("function " + name + "(");
  assert.ok(start >= 0, name);
  let depth = 0;
  for (let i = source.indexOf("{", start); i < source.length; i++) {
    if (source[i] === "{") depth++;
    if (source[i] === "}" && --depth === 0) return source.slice(start, i + 1);
  }
  throw new Error("unclosed " + name);
}

function shims(api, loader) {
  const window = { __gosx_scene3d_data_texture_api: api, __gosx_scene3d_api: loader };
  const context = vm.createContext({ window, console });
  vm.runInContext(shimSource, context);
  context.window = window;
  return context;
}

test("shims request the chunk once, only for a data reference, and pass through to the API", () => {
  const requests = [];
  const loader = { loadPresentation: (...args) => { requests.push(args); return Promise.resolve(); } };
  const calls = [];
  const ctx = shims(undefined, loader);
  const tex = { name: "spectrum" };
  assert.equal(ctx.sceneDataTextureView({ device: {} }, material("spectrum", "https://example.test/a.png"), tex), undefined);
  assert.equal(ctx.sceneDataTextureBind({}, {}, 0, 0, material("spectrum", "gosx:environment:radiance"), tex, null), undefined);
  assert.equal(requests.length, 0, "no data reference, no request");
  ctx.sceneDataTextureView({ device: {} }, material("spectrum", "gosx:data:a"), tex);
  ctx.sceneDataTextureBind({}, {}, 0, 0, material("spectrum", { resource: "gosx:data:a" }), tex, null);
  assert.deepEqual(requests, [["data-texture", "gosxScene3dDataTextureUrl", ""]], "one request, advertised URL only");

  ctx.window.__gosx_scene3d_data_texture_api = {
    webgpuView: (...args) => { calls.push(["view", ...args]); return "view"; },
    webglBind: (...args) => { calls.push(["bind", ...args]); return true; },
  };
  const device = {}, mat = material("spectrum", "gosx:data:a");
  assert.equal(ctx.sceneDataTextureView({ device }, mat, tex), "view");
  assert.equal(ctx.sceneDataTextureBind("gl", "info", 3, "loc", mat, tex, "ph"), true);
  assert.deepEqual(calls, [["view", device, mat, tex], ["bind", "gl", "info", 3, "loc", mat, tex, "ph"]]);
});

test("a failed chunk load is logged and the shim does not retry every frame", async () => {
  const logged = [];
  const window = { __gosx_scene3d_api: { loadPresentation: () => Promise.reject(new Error("failed to load scene3d-data-texture chunk")) } };
  const context = vm.createContext({ window, console: { error: (...args) => logged.push(args) } });
  vm.runInContext(shimSource, context);
  for (let i = 0; i < 3; i++) context.sceneDataTextureView({}, material("t", "gosx:data:a"), { name: "t" });
  await new Promise((resolve) => setImmediate(resolve));
  assert.equal(logged.length, 1);
  assert.match(String(logged[0][0]), /failed to load scene3d-data-texture chunk/);
});

test("WebGPU samplers: load-only and 32-bit float textures get one shared nearest sampler", () => {
  const created = [];
  const ctx = shims(undefined, {});
  const context = { device: { createSampler: (d) => { created.push(d); return { d }; } } };
  assert.equal(ctx.sceneDataTextureSampler(context, { name: "a" }), null, "plain sampled textures keep the linear sampler");
  assert.equal(ctx.sceneDataTextureSampler(context, { name: "a", format: "r16float", sampleMode: "both" }), null);
  const nearest = ctx.sceneDataTextureSampler(context, { name: "a", format: "r16float", sampleMode: "load" });
  assert.deepEqual(JSON.parse(JSON.stringify(created)), [{ magFilter: "nearest", minFilter: "nearest", mipmapFilter: "nearest" }]);
  assert.equal(ctx.sceneDataTextureSampler(context, { name: "b", format: "r32float", sampleMode: "both" }), nearest);
  assert.equal(ctx.sceneDataTextureSampler(context, { name: "c", format: "rgba32float" }), nearest);
  assert.equal(created.length, 1);
});

test("WebGPU bind-group layout: 32-bit float textures are unfilterable with a non-filtering sampler", () => {
  const source = readSceneRendererBackendSrc("webgpu");
  const layouts = [];
  const context = vm.createContext({
    GPUShaderStage: { VERTEX: 1, FRAGMENT: 2 },
    sceneNumber: (v, d) => (Number.isFinite(v) ? v : d),
    sceneSelenaTextureDescriptors: (l) => l.textures,
    sceneSelenaStorageBufferDescriptors: () => [],
    sceneSelenaStateDescriptors: () => [],
  });
  vm.runInContext(["sceneSelenaWGPUTextureLayout", "sceneSelenaBindGroupLayout"].map((n) => fn(source, n)).join("\n"), context);
  const device = { createBindGroupLayout: (d) => { layouts.push(d); return d; } };
  const descriptors = [["a", undefined], ["b", "rgba8unorm"], ["c", "r16float"], ["d", "rgba16float"], ["e", "r32float"], ["f", "rgba32float"]]
    .map(([name, format], i) => ({ name, dimension: "2d", format, wgsl: { textureBinding: 1 + 2 * i, samplerBinding: 2 + 2 * i } }));
  context.sceneSelenaBindGroupLayout(device, { textures: descriptors, uniformBlock: { size: 16 } });
  const entries = layouts[0].entries;
  const byBinding = (b) => entries.find((e) => e.binding === b);
  const want = ["float", "float", "float", "float", "unfilterable-float", "unfilterable-float"];
  want.forEach((sampleType, i) => {
    assert.equal(byBinding(1 + 2 * i).texture.sampleType, sampleType, descriptors[i].name);
    assert.equal(byBinding(2 + 2 * i).sampler.type, sampleType === "float" ? "filtering" : "non-filtering", descriptors[i].name);
  });
  assert.equal(context.sceneSelenaWGPUTextureLayout({ dimension: "depth2d" }).sampleType, "depth");
  assert.equal(context.sceneSelenaWGPUTextureLayout({ dimension: "cube" }).viewDimension, "cube");
});

test("sceneWebGPUAppendSelenaTextures binds a data texture view and the nearest sampler, else the placeholder", () => {
  const source = readSceneRendererBackendSrc("webgpu");
  const window = {};
  const placeholderView = { name: "placeholder" }, linear = { name: "linear" }, nearest = { name: "nearest" }, dataView = { name: "data" };
  const ctx = vm.createContext({
    window, sceneNumber: (v, d) => (Number.isFinite(Number(v)) ? Number(v) : d), wgpuLoadTexture: () => null, wgpuLoadCubeTexture: () => null,
    sceneSelenaShadowMapView: () => null,
    sceneDataTextureView: (context, mat, tex) => (mat.customUniforms[tex.name] === "gosx:data:spectrum" ? dataView : null),
    sceneDataTextureSampler: (context, tex) => (tex.sampleMode === "load" ? nearest : null),
  });
  vm.runInContext(["sceneWebGPUSelenaEnvironmentSlot", "sceneWebGPUAppendSelenaTextures"].map((n) => fn(source, n)).join("\n") + "\nthis.append = sceneWebGPUAppendSelenaTextures;", ctx);
  const context = { device: {}, textureCache: {}, iblResources: { active: false }, placeholderCubeView: {}, placeholderView, envMapSampler: {}, linearSampler: linear, liveView: () => null, url: () => "" };
  const textures = [spectrum({ wgsl: { textureBinding: 1, samplerBinding: 2 } }), { name: "albedo", dimension: "2d", wgsl: { textureBinding: 3, samplerBinding: 4 } }];
  const entries = [], views = [];
  ctx.append(context, material("spectrum", "gosx:data:spectrum"), textures, entries, views);
  assert.deepEqual(entries.map((e) => [e.binding, e.resource.name]), [[1, "data"], [2, "nearest"], [3, "placeholder"], [4, "linear"]]);
  assert.deepEqual(views.map((v) => v.name), ["data", "placeholder"]);
  const unbound = [];
  ctx.append(context, material("spectrum", "gosx:data:missing"), textures, unbound, []);
  assert.deepEqual(unbound.map((e) => e.resource.name).slice(0, 2), ["placeholder", "nearest"], "an unwritten key binds the placeholder with the same sampler, so the layout still matches");
});

test("the WebGL renderer hands each Selena texture to the data-texture hook before the URL path", () => {
  const body = fn(readSceneRendererBackendSrc("webgl"), "sceneWebGLBindSelenaTextures");
  const hook = body.indexOf("sceneDataTextureBind(gl, info, unit, loc, material, tex, selenaPlaceholderTexture)");
  assert.ok(hook > 0 && hook < body.indexOf("sceneWebGLSelenaEnvironmentSlot(material, tex)") && hook < body.indexOf("sceneSelenaTextureURL(material, tex, i)"));
});

test("handle.applyDataTexture applies synchronously once loaded and copies the bytes while the chunk loads", async () => {
  const source = read("..", "runtime", "scene3d", "mount.ts");
  const line = source.split("\n").find((l) => l.trim().startsWith("applyDataTexture("));
  assert.ok(line, "mount.ts must define handle.applyDataTexture");
  const seen = [], window = {}, loads = [];
  let resolveLoad;
  const handle = vm.runInNewContext(transpile("({" + line.trim().replace(/,$/, "") + "})"), {
    window,
    scheduleRender: "render", mount: "mount",
    ensureSceneGatedFeatureLoaded: (...args) => { loads.push(args); return new Promise((resolve) => { resolveLoad = resolve; }); },
  });
  const buffer = new Uint8Array([1, 2, 3]);
  const pending = handle.applyDataTexture(buffer);
  buffer.fill(9); // the Go writer reuses its buffer as soon as the call returns
  assert.deepEqual(loads, [["data-texture", "gosxScene3dDataTextureUrl", ""]]);
  resolveLoad({ apply: (bytes, render, mount) => { seen.push([Array.from(bytes), render, mount]); return { applied: true }; } });
  assert.deepEqual(await pending, { applied: true });
  assert.deepEqual(seen, [[[1, 2, 3], "render", "mount"]]);
  window.__gosx_scene3d_data_texture_api = { apply: (bytes, render, mount) => ({ sync: Array.from(bytes), render, mount }) };
  assert.deepEqual(JSON.parse(JSON.stringify(handle.applyDataTexture(new Uint8Array([7])))), { sync: [7], render: "render", mount: "mount" });
});
