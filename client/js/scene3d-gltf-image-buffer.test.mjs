import test from "node:test";
import assert from "node:assert/strict";
import fs from "node:fs";
import vm from "node:vm";

const source = fs.readFileSync(new URL("../runtime/scene3d/gltf.ts", import.meta.url), "utf8");
const payload = [0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a];

function fixture() {
  const bytes = new Uint8Array(7 + payload.length + 5);
  bytes.fill(0xee); bytes.set(payload, 7);
  const buffers = [new ArrayBuffer(1), bytes.buffer];
  return {
    bytes, buffers, image: { bufferView: 0, mimeType: "image/png" },
    doc: { buffers: buffers.map(buffer => ({ byteLength: buffer.byteLength })),
      bufferViews: [{ buffer: 1, byteOffset: 7, byteLength: payload.length }] },
  };
}

function loader(BlobConstructor = Blob) {
  const blobs = [];
  const context = vm.createContext({ ArrayBuffer, Uint8Array, Blob: BlobConstructor,
    URL: { createObjectURL(blob) { blobs.push(blob); return "blob:fixture-" + blobs.length; } } });
  vm.runInContext(source, context);
  return { context, blobs, create: context.gltfCreateBlobURLFromBufferView };
}

const blobBytes = async blob => Array.from(new Uint8Array(await blob.arrayBuffer()));

test("embedded images give Blob a bounded source view without a preliminary byte copy", async () => {
  const f = fixture();
  let preliminaryCopies = 0, observed = false;
  f.bytes.buffer.slice = function(...args) {
    preliminaryCopies++;
    return ArrayBuffer.prototype.slice.apply(this, args);
  };
  class InspectingBlob extends Blob {
    constructor(parts, options) {
      assert.equal(preliminaryCopies, 0, "the loader must leave byte snapshotting to Blob");
      assert.equal(parts.length, 1);
      assert.ok(parts[0] instanceof Uint8Array);
      assert.equal(parts[0].buffer, f.bytes.buffer);
      assert.equal(parts[0].byteOffset, 7);
      assert.equal(parts[0].byteLength, payload.length);
      observed = true;
      super(parts, options);
    }
  }
  const r = loader(InspectingBlob);
  assert.equal(r.create(f.doc, f.image, f.buffers), "blob:fixture-1");
  assert.equal(observed, true);
  assert.equal(r.blobs[0].size, payload.length);
  assert.equal(r.blobs[0].type, "image/png");
  f.bytes.fill(0);
  assert.deepEqual(await blobBytes(r.blobs[0]), payload, "Blob must own an immutable snapshot, excluding both sentinels");
});

test("invalid embedded image bounds retain their errors before Blob creation", () => {
  const r = loader();
  const cases = [
    [f => f.doc.bufferViews[0].buffer = 2, "glTF image bufferView 0 references unavailable buffer 2"],
    [f => f.doc.bufferViews[0].byteOffset = -1, "glTF bufferView 0 has invalid byteOffset -1"],
    [f => f.doc.bufferViews[0].byteLength = 14, "glTF bufferView 0 exceeds buffer 1 bounds"],
    [f => f.doc.buffers[1].byteLength = 14, "glTF bufferView 0 exceeds buffer 1 bounds"],
    [f => f.doc.buffers[1].byteLength = 21, "glTF buffer 1 is shorter than declared byteLength 21"],
  ];
  for (const [alter, message] of cases) {
    const f = fixture(); alter(f);
    assert.throws(() => r.create(f.doc, f.image, f.buffers), error => error.message === message);
  }
  assert.equal(r.blobs.length, 0);
});

test("image snapshots and URLs are shared within an extraction and refreshed for the next one", async () => {
  const f = fixture(), r = loader();
  r.context.gltfExtractScene(f.doc, f.buffers);
  const first = r.create(f.doc, f.image, f.buffers);
  f.bytes.fill(42, 7, 7 + payload.length);
  assert.equal(r.create(f.doc, f.image, f.buffers), first);
  assert.equal(r.blobs.length, 1);
  assert.deepEqual(await blobBytes(r.blobs[0]), payload);
  r.context.gltfExtractScene(f.doc, f.buffers);
  assert.notEqual(r.create(f.doc, f.image, f.buffers), first);
  assert.equal(r.blobs.length, 2);
  assert.deepEqual(await blobBytes(r.blobs[1]), payload.map(() => 42));
});
