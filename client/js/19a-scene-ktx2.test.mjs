// KTX2 reader tests for 19a-scene-ktx2.ts.
//
// The oracle is not our own writer. render/bundle/ktx2/testdata holds ten
// containers that KTX-Software 4.4.2 wrote with "ktx create --raw", one per BC
// VkFormat. A round trip through one author's writer and reader proves the pair
// is self-consistent; agreeing with the Khronos reference implementation proves
// the reader is correct.
//
// One limit is worth stating. The reference files are all scheme 0, single
// level, 4x4. The scheme 3 tests therefore re-wrap a Khronos payload in a zlib
// container this file builds, so the BC bytes still come from Khronos while the
// container framing comes from the test. The framing itself is pinned by the
// scheme 0 tests, which read the Khronos level index directly.
//
// The fragment is evaluated in this realm rather than a VM context, so the
// Response and DecompressionStream the inflate path uses are the real ones.

import test from "node:test";
import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import zlib from "node:zlib";
import { execFileSync } from "node:child_process";
import { fileURLToPath } from "node:url";

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const srcDir = path.join(__dirname, "bootstrap-src");
const fixtureDir = path.join(__dirname, "..", "..", "render", "bundle", "ktx2", "testdata");
const ktx2Source = fs.readFileSync(path.join(srcDir, "19a-scene-ktx2.ts"), "utf8");

function loadKTX2() {
  const window = {};
  const factory = new Function("window", ktx2Source + "\nreturn window.__gosx_scene3d_ktx2;");
  return { api: factory(window), window };
}

function readFixture(name) {
  const buffer = fs.readFileSync(path.join(fixtureDir, name));
  return new Uint8Array(buffer);
}

// Every reference container, with the VkFormat, the WebGPU name and the WebGL2
// internal format the reader must report for it.
//
// The VkFormat numbers come from render/bundle/ktx2/ktx2.go. The WebGPU names
// come from ktx2FormatToGPU in render/bundle/ktx2_loader.go composed with
// encodeTextureFormat in render/gpu/jsgpu/encode.go. The WebGL2 numbers come
// from the Khronos glext.h header.
const FIXTURES = [
  { file: "bc1.ktx2", vkFormat: 131, webgpu: "bc1-rgba-unorm", webgl: 0x83F0, blockBytes: 8 },
  { file: "bc1s.ktx2", vkFormat: 132, webgpu: "bc1-rgba-unorm-srgb", webgl: 0x8C4C, blockBytes: 8 },
  { file: "bc1a.ktx2", vkFormat: 133, webgpu: "bc1-rgba-unorm", webgl: 0x83F1, blockBytes: 8 },
  { file: "bc1as.ktx2", vkFormat: 134, webgpu: "bc1-rgba-unorm-srgb", webgl: 0x8C4D, blockBytes: 8 },
  { file: "bc3.ktx2", vkFormat: 137, webgpu: "bc3-rgba-unorm", webgl: 0x83F3, blockBytes: 16 },
  { file: "bc3s.ktx2", vkFormat: 138, webgpu: "bc3-rgba-unorm-srgb", webgl: 0x8C4F, blockBytes: 16 },
  { file: "bc4.ktx2", vkFormat: 139, webgpu: "bc4-r-unorm", webgl: 0x8DBB, blockBytes: 8 },
  { file: "bc5.ktx2", vkFormat: 141, webgpu: "bc5-rg-unorm", webgl: 0x8DBD, blockBytes: 16 },
  { file: "bc7.ktx2", vkFormat: 145, webgpu: "bc7-rgba-unorm", webgl: 0x8E8C, blockBytes: 16 },
  { file: "bc7s.ktx2", vkFormat: 146, webgpu: "bc7-rgba-unorm-srgb", webgl: 0x8E8D, blockBytes: 16 },
];

test("the JS reader consumes key/value metadata emitted by the real Go writer", () => {
  const go = process.env.GOSX_GO || "go";
  const encoded = execFileSync(
    go,
    ["run", "./client/js/testdata/ktx2-go-writer-fixture"],
    { cwd: path.join(__dirname, "..", ".."), encoding: "utf8" },
  ).trim();
  const image = loadKTX2().api.parse(new Uint8Array(Buffer.from(encoded, "base64")));
  assert.equal(image.vkFormat, 83);
  assert.equal(image.width, 1);
  assert.equal(image.height, 1);
  assert.equal(image.faces, 1);
  assert.equal(image.levels.length, 1);
  assert.deepEqual(image.keyValues, {
    GoSXColorSpace: "linear",
    GoSXiblModel: IBL_BRDF_MODEL,
    GoSXiblRole: "brdf-lut",
    KTXwriter: "GoSX assetpipe ktx2 writer",
  });
});

// --- reading the reference containers ---------------------------------------

test("every Khronos reference container parses to its own VkFormat and shape", () => {
  const { api } = loadKTX2();
  for (const fixture of FIXTURES) {
    const image = api.parse(readFixture(fixture.file));
    assert.equal(image.vkFormat, fixture.vkFormat, `${fixture.file}: vkFormat`);
    assert.equal(image.width, 4, `${fixture.file}: width`);
    assert.equal(image.height, 4, `${fixture.file}: height`);
    assert.equal(image.levelCount, 1, `${fixture.file}: levelCount`);
    assert.equal(image.faces, 1, `${fixture.file}: faceCount`);
    assert.equal(image.layers, 1, `${fixture.file}: layerCount normalizes 0 to 1`);
    assert.equal(image.supercompressionScheme, 0, `${fixture.file}: scheme`);
    assert.equal(image.levels.length, 1, `${fixture.file}: level slices`);
    assert.equal(image.levels[0].bytes.byteLength, fixture.blockBytes, `${fixture.file}: level 0 bytes`);
  }
});

test("the level slice matches the bytes the Khronos level index points at", () => {
  const { api } = loadKTX2();
  for (const fixture of FIXTURES) {
    const raw = readFixture(fixture.file);
    const view = new DataView(raw.buffer, raw.byteOffset, raw.byteLength);
    // Read the level index straight out of the file, independent of the reader.
    const offset = Number(view.getBigUint64(80, true));
    const length = Number(view.getBigUint64(88, true));
    const expected = raw.subarray(offset, offset + length);

    const image = api.parse(raw);
    assert.deepEqual(
      Array.from(image.levels[0].bytes),
      Array.from(expected),
      `${fixture.file}: the reader sliced a different payload than the level index names`
    );
  }
});

test("every VkFormat maps to its WebGPU name and its WebGL2 internal format", () => {
  const { api } = loadKTX2();
  for (const fixture of FIXTURES) {
    const info = api.formatInfo(fixture.vkFormat);
    assert.equal(info.webgpuFormat, fixture.webgpu, `vkFormat ${fixture.vkFormat}: WebGPU name`);
    assert.equal(info.webglInternalFormat, fixture.webgl, `vkFormat ${fixture.vkFormat}: WebGL2 internal format`);
    assert.equal(info.blockWidth, 4);
    assert.equal(info.blockHeight, 4);
    assert.equal(info.bytesPerBlock, fixture.blockBytes);
  }
});

test("the assetpipe IBL half-float formats map without pretending to be compressed", () => {
  const { api } = loadKTX2();
  assert.deepEqual(
    api.formatInfo(83),
    {
      webgpuFormat: "rg16float",
      webglInternalFormat: 0x822F,
      blockWidth: 1,
      blockHeight: 1,
      bytesPerBlock: 4,
      webglExtension: "OES_texture_float_linear",
      compressed: false,
      webglFormat: 0x8227,
      webglType: 0x140B,
    },
  );
  assert.deepEqual(
    api.formatInfo(97),
    {
      webgpuFormat: "rgba16float",
      webglInternalFormat: 0x881A,
      blockWidth: 1,
      blockHeight: 1,
      bytesPerBlock: 8,
      webglExtension: "OES_texture_float_linear",
      compressed: false,
      webglFormat: 0x1908,
      webglType: 0x140B,
    },
  );
});

test("an uncompressed VkFormat is refused by name", () => {
  const { api } = loadKTX2();
  // 37 is VK_FORMAT_R8G8B8A8_UNORM. An image element already loads those pixels.
  assert.throws(() => api.formatInfo(37), (error) => error.code === "format");
  assert.throws(() => api.formatInfo(157), (error) => error.code === "format", "ASTC has no built payload");
});

// --- decoding ----------------------------------------------------------------

test("decode fills the block geometry of every reference container", async () => {
  const { api } = loadKTX2();
  for (const fixture of FIXTURES) {
    const image = await api.decode(readFixture(fixture.file));
    assert.equal(image.layout.webgpuFormat, fixture.webgpu);
    assert.equal(image.levels[0].blockColumns, 1, `${fixture.file}: block columns`);
    assert.equal(image.levels[0].blockRows, 1, `${fixture.file}: block rows`);
    assert.equal(image.levels[0].bytes.byteLength, fixture.blockBytes);
  }
});

// buildZlibContainer re-wraps a decoded payload as a supercompression scheme 3
// container with several mip levels.
//
// The BC bytes come from a Khronos file. The framing follows the layout the
// scheme 0 tests already pinned: a 12-byte identifier, a 68-byte header, then a
// 24-byte entry per level.
function buildZlibContainer(vkFormat, width, height, levelPayloads) {
  const identifier = [0xAB, 0x4B, 0x54, 0x58, 0x20, 0x32, 0x30, 0xBB, 0x0D, 0x0A, 0x1A, 0x0A];
  const packed = levelPayloads.map((bytes) => new Uint8Array(zlib.deflateSync(Buffer.from(bytes))));
  const indexBytes = levelPayloads.length * 24;
  const dataStart = 80 + indexBytes;
  let total = dataStart;
  const offsets = [];
  for (let i = 0; i < packed.length; i++) {
    offsets.push(total);
    total += packed[i].byteLength;
  }
  const out = new Uint8Array(total);
  out.set(identifier, 0);
  const view = new DataView(out.buffer);
  view.setUint32(12, vkFormat, true);
  view.setUint32(16, 1, true); // typeSize
  view.setUint32(20, width, true);
  view.setUint32(24, height, true);
  view.setUint32(28, 0, true); // pixelDepth
  view.setUint32(32, 0, true); // layerCount
  view.setUint32(36, 1, true); // faceCount
  view.setUint32(40, levelPayloads.length, true);
  view.setUint32(44, 3, true); // supercompressionScheme: zlib
  for (let i = 0; i < packed.length; i++) {
    const entry = 80 + i * 24;
    view.setBigUint64(entry, BigInt(offsets[i]), true);
    view.setBigUint64(entry + 8, BigInt(packed[i].byteLength), true);
    view.setBigUint64(entry + 16, BigInt(levelPayloads[i].byteLength), true);
    out.set(packed[i], offsets[i]);
  }
  return out;
}

function buildIBLContainer(vkFormat, width, height, faces, levelPayloads, keyValues) {
  const identifier = [0xAB, 0x4B, 0x54, 0x58, 0x20, 0x32, 0x30, 0xBB, 0x0D, 0x0A, 0x1A, 0x0A];
  const encoder = new TextEncoder();
  const kvEntries = Object.entries(keyValues || {}).map(([key, value]) => {
    const keyBytes = encoder.encode(key);
    const valueBytes = encoder.encode(value);
    // Match render/bundle/ktx2.encodeKeyValues exactly: key\0value\0.
    const pairLength = keyBytes.byteLength + 1 + valueBytes.byteLength + 1;
    const paddedLength = (pairLength + 3) & ~3;
    const entry = new Uint8Array(4 + paddedLength);
    new DataView(entry.buffer).setUint32(0, pairLength, true);
    entry.set(keyBytes, 4);
    entry[4 + keyBytes.byteLength] = 0;
    entry.set(valueBytes, 5 + keyBytes.byteLength);
    entry[5 + keyBytes.byteLength + valueBytes.byteLength] = 0;
    return entry;
  });
  const indexEnd = 80 + levelPayloads.length * 24;
  const kvdOffset = indexEnd;
  const kvdLength = kvEntries.reduce((total, entry) => total + entry.byteLength, 0);
  const dataStart = kvdOffset + kvdLength;
  const offsets = [];
  let total = dataStart;
  for (const payload of levelPayloads) {
    offsets.push(total);
    total += payload.byteLength;
  }

  const out = new Uint8Array(total);
  out.set(identifier, 0);
  const view = new DataView(out.buffer);
  view.setUint32(12, vkFormat, true);
  view.setUint32(16, 2, true);
  view.setUint32(20, width, true);
  view.setUint32(24, height, true);
  view.setUint32(28, 0, true);
  view.setUint32(32, 0, true);
  view.setUint32(36, faces, true);
  view.setUint32(40, levelPayloads.length, true);
  view.setUint32(44, 0, true);
  view.setUint32(56, kvdOffset, true);
  view.setUint32(60, kvdLength, true);
  for (let i = 0; i < levelPayloads.length; i++) {
    const entry = 80 + i * 24;
    view.setBigUint64(entry, BigInt(offsets[i]), true);
    view.setBigUint64(entry + 8, BigInt(levelPayloads[i].byteLength), true);
    view.setBigUint64(entry + 16, BigInt(levelPayloads[i].byteLength), true);
  }
  let kvCursor = kvdOffset;
  for (const entry of kvEntries) {
    out.set(entry, kvCursor);
    kvCursor += entry.byteLength;
  }
  for (let i = 0; i < levelPayloads.length; i++) {
    out.set(levelPayloads[i], offsets[i]);
  }
  return out;
}

const IBL_BRDF_MODEL = "ggx-split-sum/smith-schlick-k=alpha-over-2/schlick-fresnel";

test("assetpipe radiance metadata and six-face mip payloads survive decode", async () => {
  const { api } = loadKTX2();
  const level0 = Uint8Array.from({ length: 4 * 4 * 6 * 8 }, (_, index) => index & 0xFF);
  const level1 = Uint8Array.from({ length: 2 * 2 * 6 * 8 }, (_, index) => (index + 17) & 0xFF);
  const image = await api.decode(buildIBLContainer(97, 4, 4, 6, [level0, level1], {
    GoSXiblRole: "radiance",
    GoSXColorSpace: "linear",
    GoSXiblModel: IBL_BRDF_MODEL,
  }));

  assert.equal(image.faces, 6);
  assert.equal(image.levelCount, 2);
  assert.equal(image.layout.webgpuFormat, "rgba16float");
  assert.deepEqual(image.keyValues, {
    GoSXiblRole: "radiance",
    GoSXColorSpace: "linear",
    GoSXiblModel: IBL_BRDF_MODEL,
  });
  assert.equal(image.levels[0].blockColumns, 4);
  assert.equal(image.levels[0].blockRows, 4);
  assert.deepEqual(Array.from(image.levels[1].bytes), Array.from(level1));
});

test("assetpipe BRDF LUT remains a two-channel linear half-float texture", async () => {
  const { api } = loadKTX2();
  const payload = Uint8Array.from({ length: 4 * 4 * 4 }, (_, index) => (index * 3) & 0xFF);
  const image = await api.decode(buildIBLContainer(83, 4, 4, 1, [payload], {
    GoSXiblRole: "brdf-lut",
    GoSXColorSpace: "linear",
    GoSXiblModel: IBL_BRDF_MODEL,
  }));

  assert.equal(image.faces, 1);
  assert.equal(image.layout.webgpuFormat, "rg16float");
  assert.equal(image.layout.compressed, false);
  assert.equal(image.keyValues.GoSXiblRole, "brdf-lut");
  assert.deepEqual(Array.from(image.levels[0].bytes), Array.from(payload));
});

test("supercompression scheme 3 inflates back to the Khronos payload", async () => {
  const { api } = loadKTX2();
  const reference = api.parse(readFixture("bc7.ktx2"));
  const block = reference.levels[0].bytes;

  // An 8x8 BC7 image: level 0 is four blocks, level 1 is one, level 2 rounds a
  // 2x2 level up to one whole block.
  const level0 = new Uint8Array(64);
  for (let i = 0; i < 4; i++) level0.set(block, i * 16);
  const container = buildZlibContainer(145, 8, 8, [level0, block, block]);

  const image = await api.decode(container);
  assert.equal(image.supercompressionScheme, 3);
  assert.equal(image.levels.length, 3);
  assert.deepEqual(Array.from(image.levels[0].bytes), Array.from(level0));
  assert.deepEqual(Array.from(image.levels[1].bytes), Array.from(block));
  assert.equal(image.levels[0].blockColumns, 2);
  assert.equal(image.levels[0].blockRows, 2);
  assert.equal(image.levels[2].width, 2, "a 2x2 level keeps its logical size");
  assert.equal(image.levels[2].blockColumns, 1, "a 2x2 level still costs one whole block");
});

// --- the guards --------------------------------------------------------------

test("Zstandard supercompression is refused by name", async () => {
  const { api } = loadKTX2();
  const raw = readFixture("bc7.ktx2");
  const corrupt = raw.slice();
  new DataView(corrupt.buffer).setUint32(44, 2, true); // scheme 2 is Zstandard
  await assert.rejects(() => api.decode(corrupt), (error) => {
    assert.equal(error.code, "scheme-zstd");
    assert.match(error.message, /Zstandard/);
    return true;
  });
});

test("BasisLZ supercompression is refused by name", async () => {
  const { api } = loadKTX2();
  const corrupt = readFixture("bc7.ktx2").slice();
  new DataView(corrupt.buffer).setUint32(44, 1, true);
  await assert.rejects(() => api.decode(corrupt), (error) => error.code === "scheme-basislz");
});

test("a level offset outside the file is refused", () => {
  const { api } = loadKTX2();
  const corrupt = readFixture("bc7.ktx2").slice();
  // Push level 0 one byte past the end of the file.
  new DataView(corrupt.buffer).setBigUint64(80, BigInt(corrupt.byteLength), true);
  assert.throws(() => api.parse(corrupt), (error) => error.code === "level-range");
});

test("a level offset inside the header is refused", () => {
  const { api } = loadKTX2();
  const corrupt = readFixture("bc7.ktx2").slice();
  // A level may not start inside the header or the level index.
  new DataView(corrupt.buffer).setBigUint64(80, 64n, true);
  assert.throws(() => api.parse(corrupt), (error) => error.code === "level-range");
});

test("a level whose bytes disagree with the block arithmetic is refused", async () => {
  const { api } = loadKTX2();
  const corrupt = readFixture("bc7.ktx2").slice();
  // Claim a 16x16 image while the file still holds one 4x4 block.
  const view = new DataView(corrupt.buffer);
  view.setUint32(20, 16, true);
  view.setUint32(24, 16, true);
  await assert.rejects(() => api.decode(corrupt), (error) => {
    assert.equal(error.code, "level-size");
    return true;
  });
});

test("a file that is not KTX2 is refused", () => {
  const { api } = loadKTX2();
  const corrupt = readFixture("bc7.ktx2").slice();
  corrupt[1] = 0x00;
  assert.throws(() => api.parse(corrupt), (error) => error.code === "identifier");
  assert.throws(() => api.parse(new Uint8Array(16)), (error) => error.code === "truncated");
});

// --- uploading ---------------------------------------------------------------

// recordingDevice captures every WebGPU call the uploader makes.
function recordingDevice() {
  const calls = { createTexture: [], writeTexture: [] };
  return {
    calls,
    createTexture(descriptor) {
      calls.createTexture.push(descriptor);
      return { descriptor };
    },
    queue: {
      writeTexture(destination, data, layout, size) {
        calls.writeTexture.push({ destination, byteLength: data.byteLength, layout, size });
      },
    },
  };
}

test("the WebGPU uploader writes one call per mip level", async () => {
  const { api } = loadKTX2();
  const reference = api.parse(readFixture("bc7.ktx2"));
  const block = reference.levels[0].bytes;
  const level0 = new Uint8Array(64);
  for (let i = 0; i < 4; i++) level0.set(block, i * 16);
  const image = await api.decode(buildZlibContainer(145, 8, 8, [level0, block, block]));

  const device = recordingDevice();
  api.uploadWebGPU(device, image, { label: "test" });

  assert.equal(device.calls.createTexture.length, 1);
  const descriptor = device.calls.createTexture[0];
  assert.equal(descriptor.format, "bc7-rgba-unorm");
  assert.equal(descriptor.mipLevelCount, 3);
  assert.deepEqual(descriptor.size, { width: 8, height: 8, depthOrArrayLayers: 1 });
  assert.equal(descriptor.usage, 0x06, "TEXTURE_BINDING | COPY_DST");

  assert.equal(device.calls.writeTexture.length, 3);
  const [first, , third] = device.calls.writeTexture;
  assert.equal(first.destination.mipLevel, 0);
  assert.equal(first.layout.bytesPerRow, 32, "two blocks of 16 bytes per row");
  assert.equal(first.layout.rowsPerImage, 2);
  assert.deepEqual(first.size, { width: 8, height: 8, depthOrArrayLayers: 1 });
  // WebGPU validates a compressed copy against the PHYSICAL mip size, which
  // rounds up to whole blocks. A 2x2 level therefore copies a 4x4 extent.
  assert.equal(third.destination.mipLevel, 2);
  assert.deepEqual(third.size, { width: 4, height: 4, depthOrArrayLayers: 1 });
});

test("the WebGPU uploader preserves every IBL cube face and mip", async () => {
  const { api } = loadKTX2();
  const level0 = new Uint8Array(4 * 4 * 6 * 8);
  const level1 = new Uint8Array(2 * 2 * 6 * 8);
  const image = await api.decode(buildIBLContainer(97, 4, 4, 6, [level0, level1], {
    GoSXiblRole: "radiance",
    GoSXColorSpace: "linear",
    GoSXiblModel: IBL_BRDF_MODEL,
  }));
  const device = recordingDevice();
  const texture = api.uploadWebGPU(device, image, { label: "ibl-radiance" });

  assert.equal(texture.descriptor.format, "rgba16float");
  assert.equal(texture.descriptor.mipLevelCount, 2);
  assert.deepEqual(texture.descriptor.size, { width: 4, height: 4, depthOrArrayLayers: 6 });
  assert.equal(device.calls.writeTexture.length, 2);
  assert.equal(device.calls.writeTexture[0].layout.bytesPerRow, 32);
  assert.equal(device.calls.writeTexture[0].layout.rowsPerImage, 4);
  assert.deepEqual(device.calls.writeTexture[0].size, {
    width: 4,
    height: 4,
    depthOrArrayLayers: 6,
  });
  assert.deepEqual(device.calls.writeTexture[1].size, {
    width: 2,
    height: 2,
    depthOrArrayLayers: 6,
  });
});

test("the WebGPU uploader refuses an image that was never decoded", () => {
  const { api } = loadKTX2();
  const parsed = api.parse(readFixture("bc7.ktx2"));
  assert.throws(() => api.uploadWebGPU(recordingDevice(), parsed), (error) => error.code === "undecoded");
});

// recordingGL captures every WebGL2 call and grants the named extensions.
function recordingGL(extensions) {
  const granted = new Set(extensions);
  const calls = { compressedTexImage2D: [], texImage2D: [], extensions: [] };
  return {
    calls,
    TEXTURE_2D: 0x0DE1,
    TEXTURE_CUBE_MAP: 0x8513,
    TEXTURE_CUBE_MAP_POSITIVE_X: 0x8515,
    TEXTURE_MAX_LEVEL: 0x813D,
    TEXTURE_WRAP_S: 0x2802,
    TEXTURE_WRAP_T: 0x2803,
    TEXTURE_WRAP_R: 0x8072,
    CLAMP_TO_EDGE: 0x812F,
    getExtension(name) {
      calls.extensions.push(name);
      return granted.has(name) ? {} : null;
    },
    createTexture: () => ({}),
    bindTexture: () => {},
    texParameteri: () => {},
    compressedTexImage2D(target, level, internalFormat, width, height, border, data) {
      calls.compressedTexImage2D.push({ level, internalFormat, width, height, border, byteLength: data.byteLength });
    },
    texImage2D(target, level, internalFormat, width, height, border, format, type, data) {
      calls.texImage2D.push({
        target, level, internalFormat, width, height, border, format, type,
        byteLength: data.byteLength,
      });
    },
  };
}

test("the WebGL2 uploader passes the logical level size and the right internal format", async () => {
  const { api } = loadKTX2();
  const reference = api.parse(readFixture("bc5.ktx2"));
  const block = reference.levels[0].bytes;
  const level0 = new Uint8Array(64);
  for (let i = 0; i < 4; i++) level0.set(block, i * 16);
  const image = await api.decode(buildZlibContainer(141, 8, 8, [level0, block, block]));

  const gl = recordingGL(["EXT_texture_compression_rgtc"]);
  api.uploadWebGL2(gl, image);

  assert.deepEqual(gl.calls.extensions, ["EXT_texture_compression_rgtc"]);
  assert.equal(gl.calls.compressedTexImage2D.length, 3);
  const uploads = gl.calls.compressedTexImage2D;
  assert.equal(uploads[0].internalFormat, 0x8DBD, "COMPRESSED_RED_GREEN_RGTC2_EXT");
  assert.deepEqual([uploads[0].width, uploads[0].height], [8, 8]);
  // WebGL2 wants the LOGICAL level size, unlike WebGPU.
  assert.deepEqual([uploads[2].width, uploads[2].height], [2, 2]);
  assert.equal(uploads[2].byteLength, 16, "a 2x2 level still carries one whole block");
});

test("the WebGL2 uploader refuses a context without the matching extension", async () => {
  const { api } = loadKTX2();
  const image = await api.decode(readFixture("bc7.ktx2"));
  const gl = recordingGL(["WEBGL_compressed_texture_s3tc"]);
  assert.throws(() => api.uploadWebGL2(gl, image), (error) => {
    assert.equal(error.code, "extension");
    assert.match(error.message, /EXT_texture_compression_bptc/);
    return true;
  });
  assert.equal(gl.calls.compressedTexImage2D.length, 0, "nothing may upload without the extension");
});

test("the WebGL2 uploader preserves the native half-float IBL cube faces and mips", async () => {
  const { api } = loadKTX2();
  const image = await api.decode(buildIBLContainer(
    97,
    4,
    4,
    6,
    [new Uint8Array(4 * 4 * 6 * 8), new Uint8Array(2 * 2 * 6 * 8)],
    {
      GoSXiblRole: "radiance",
      GoSXColorSpace: "linear",
      GoSXiblModel: IBL_BRDF_MODEL,
    },
  ));
  const gl = recordingGL(["OES_texture_float_linear"]);
  api.uploadWebGL2(gl, image);

  assert.equal(gl.calls.compressedTexImage2D.length, 0);
  assert.equal(gl.calls.texImage2D.length, 12, "six faces times two mip levels");
  for (let face = 0; face < 6; face++) {
    const upload = gl.calls.texImage2D[face];
    assert.equal(upload.target, gl.TEXTURE_CUBE_MAP_POSITIVE_X + face);
    assert.equal(upload.level, 0);
    assert.equal(upload.internalFormat, 0x881A);
    assert.equal(upload.format, 0x1908);
    assert.equal(upload.type, 0x140B);
    assert.equal(upload.byteLength, 4 * 4 * 8);
  }
  for (let face = 0; face < 6; face++) {
    const upload = gl.calls.texImage2D[6 + face];
    assert.equal(upload.target, gl.TEXTURE_CUBE_MAP_POSITIVE_X + face);
    assert.equal(upload.level, 1);
    assert.equal(upload.byteLength, 2 * 2 * 8);
  }
});

test("the WebGL2 IBL uploader fails visibly without half-float linear filtering", async () => {
  const { api } = loadKTX2();
  const image = await api.decode(buildIBLContainer(
    83,
    2,
    2,
    1,
    [new Uint8Array(2 * 2 * 4)],
    {
      GoSXiblRole: "brdf-lut",
      GoSXColorSpace: "linear",
      GoSXiblModel: IBL_BRDF_MODEL,
    },
  ));
  const gl = recordingGL([]);
  assert.throws(() => api.uploadWebGL2(gl, image), (error) => {
    assert.equal(error.code, "extension");
    assert.match(error.message, /OES_texture_float_linear/);
    return true;
  });
  assert.equal(gl.calls.texImage2D.length, 0);
});

// --- the upload gate ---------------------------------------------------------

test("the upload path stays closed until a renderer registers a loader", () => {
  const { api, window } = loadKTX2();
  assert.equal(api.uploadPathReady(), false, "no renderer has registered a KTX2 texture loader");
  window.__gosx_scene3d_ktx2_texture_loader = () => {};
  assert.equal(api.uploadPathReady(), true);
});
