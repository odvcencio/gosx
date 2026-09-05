"use strict";
const test = require("node:test");
const assert = require("node:assert/strict");
const { createContext, runScript, bootstrapRuntimeSource, freshFeatureBundleSource, flushAsyncWork } = require("./runtime-test-harness.js");

function harness() {
  const env = createContext({});
  const live = new Map(), revoked = [], images = [];
  let serial = 0;
  class ImageURL extends URL {}
  ImageURL.createObjectURL = blob => { const uri = "blob:test-" + (++serial); live.set(uri, blob); return uri; };
  ImageURL.revokeObjectURL = uri => { assert.ok(live.has(uri), "only revoke a live owned URL"); live.delete(uri); revoked.push(uri); };
  Object.assign(env.context, { URL: ImageURL, Blob, Image: class {
    constructor() { this.width = this.height = 2; images.push(this); }
    set src(value) { this.url = value; }
  } });
  runScript(bootstrapRuntimeSource, env.context, "runtime");
  runScript(freshFeatureBundleSource("scene3d").replace("window.__gosx_scene3d_api = {", "window.__gosx_scene3d_api = { clone: sceneCloneData, normalizeImage: normalizeSceneTextureDescriptor, materialKey: sceneMaterialProfileKey, materialIndex: sceneBundleMaterialIndex, hydrate: hydrateSceneStateModels,"), env.context, "scene3d");
  runScript(freshFeatureBundleSource("scene3d-gltf").replace("  function gltfExtractMaterial(", "  window.__extractImageMaterial = gltfExtractMaterial; window.__extractImageScene = gltfExtractScene;\n  function gltfExtractMaterial("), env.context, "gltf");
  runScript(freshFeatureBundleSource("scene3d-webgl").replace("  function scenePBRTextureDescriptor(", "  window.__imageGL = { load: scenePBRLoadTexture, water: sceneWaterRenderLoad2D, findWater: sceneWaterRenderFindBundleMesh, modelWater: sceneWaterModelTexture, selena: sceneSelenaLoadTexture };\n  function scenePBRTextureDescriptor("), env.context, "webgl");
  runScript(freshFeatureBundleSource("scene3d-webgpu").replace("  function wgpuTextureDescriptor(", "  window.__imageGPU = { load: wgpuLoadTexture, selena: wgpuLoadSelenaTexture };\n  function wgpuTextureDescriptor("), env.context, "webgpu");
  const doc = { asset: { version: "2.0" }, buffers: [{ byteLength: 4 }], bufferViews: [{ buffer: 0, byteLength: 4 }],
    images: [{ bufferView: 0, mimeType: "image/png" }], textures: [{ source: 0 }],
    materials: [{ pbrMetallicRoughness: { baseColorTexture: { index: 0 }, metallicRoughnessTexture: { index: 0 } }, normalTexture: { index: 0 } }] };
  const bytes = new Uint8Array([1, 2, 3, 4]);
  const material = () => env.context.__extractImageMaterial(doc, 0, bytes.buffer);
  const api = env.context.__gosx_scene3d_api, loader = env.context.__gosx_scene3d_gltf_api;
  const calls = [];
  const gl = new Proxy({}, { get(_, key) {
    if (/^[A-Z_0-9]+$/.test(key)) return key;
    if (key === "createTexture") return () => ({});
    return (...args) => { calls.push([key, ...args]); };
  } });
  const textures = [];
  const device = { queue: { writeTexture() {}, copyExternalImageToTexture() {} }, createTexture() {
    const tex = { destroyed: 0, destroy() { this.destroyed++; }, createView() { return {}; } }; textures.push(tex); return tex;
  } };
  env.context.GPUTextureUsage = { TEXTURE_BINDING: 1, COPY_DST: 2, RENDER_ATTACHMENT: 4 };
  return { env, live, revoked, images, doc, bytes, material, api, loader, gl, calls, device, textures };
}

test("embedded sources are opaque, authentic, copied once per extraction and survive independent material clones", async () => {
  const h = harness(), mat = h.material(), source = mat.textureDescriptors.baseColor.imageSource;
  assert.equal(h.live.size, 0);
  assert.ok(Object.isFrozen(source));
  assert.deepEqual(Object.keys(source), []);
  assert.throws(() => { source.bytes = new Uint8Array([9]); }, TypeError);
  assert.equal(h.loader.isImageSource(Object.freeze({})), false);
  assert.equal(h.api.normalizeImage({ uri: "", imageSource: {} }), null);
  assert.equal(mat.textureDescriptors.normal.imageSource, source);
  assert.equal(mat.textureDescriptors.roughness.imageSource, mat.textureDescriptors.metalness.imageSource);
  const clone = h.api.clone(mat);
  assert.notEqual(clone, mat); assert.notEqual(clone.textureDescriptors, mat.textureDescriptors);
  assert.equal(clone.textureDescriptors.baseColor.imageSource, source);
  clone.color = "#000000"; assert.notEqual(clone.color, mat.color);
  h.bytes.fill(9); h.doc.images[0].mimeType = "image/jpeg";
  h.doc.bufferViews[0].byteOffset = 1; h.doc.bufferViews[0].byteLength = 2;
  const newer = h.material().textureDescriptors.baseColor.imageSource;
  assert.notEqual(newer, source);
  const oldLease = h.loader.acquireImageSource(source), newLease = h.loader.acquireImageSource(newer);
  assert.deepEqual(Array.from(new Uint8Array(await h.live.get(oldLease.url).arrayBuffer())), [1,2,3,4]);
  assert.equal(h.live.get(newLease.url).type, "image/jpeg");
  assert.deepEqual(Array.from(new Uint8Array(await h.live.get(newLease.url).arrayBuffer())), [9,9]);
  oldLease.release(); oldLease.release(); newLease.release(); assert.equal(h.live.size, 0);
});

test("eight WebGL map roles own decode URLs independently and leave borrowed blobs untouched", () => {
  const h = harness(), source = h.material().textureDescriptors.baseColor.imageSource, cache = new Map();
  for (const role of ["base-color", "normal", "roughness", "metalness", "ambient-occlusion", "emissive", "specular-intensity", "specular-color"]) {
    const descriptor = { uri: "", imageSource: source, role, colorSpace: role === "base-color" ? "srgb" : "linear" };
    const record = h.env.context.__imageGL.load(h.gl, "", cache, descriptor);
    assert.equal(h.live.size, 1); assert.equal(h.env.context.__imageGL.load(h.gl, "", cache, descriptor), record);
    h.images.at(-1).onload(); assert.equal(record.loaded, true); assert.equal(h.live.size, 0);
    assert.equal(record.image, null, "completed cache entry does not retain Image");
  }
  assert.equal(cache.size, 8);
  const borrowed = h.env.context.__imageGL.load(h.gl, "blob:caller-owned", cache);
  assert.equal(h.images.at(-1).crossOrigin, undefined, "WebGL retains its existing external-image request mode");
  h.images.at(-1).onerror(); assert.equal(borrowed.failed, true); assert.equal(h.revoked.includes("blob:caller-owned"), false);
});

function modelFixture(h) {
  const bytes = new Uint8Array(40);
  new Float32Array(bytes.buffer, 0, 9).set([-1,-1,0, 1,-1,0, 0,1,0]); bytes.set([1,2,3,4], 36);
  Object.assign(h.doc, { buffers: [{ byteLength: 40, uri: "/buf.bin" }], bufferViews: [{ buffer: 0, byteOffset: 36, byteLength: 4 }, { buffer: 0, byteLength: 36 }],
    accessors: [{ bufferView: 1, componentType: 5126, count: 3, type: "VEC3" }], meshes: [{ primitives: [{ attributes: { POSITION: 0 }, material: 0 }] }], nodes: [{ mesh: 0 }], scenes: [{ nodes: [0] }], scene: 0 });
  h.env.context.fetch = async url => ({ ok: true, json: async () => String(url).includes(".json") ? { objects: [{ kind: "box" }] } : h.doc, arrayBuffer: async () => bytes.buffer });
  return bytes;
}

test("parse failure after image extraction creates no registered resource and custom/water consumers retain authentic source", () => {
  const h = harness(), bytes = modelFixture(h), material = h.env.context.__extractImageMaterial(h.doc, 0, bytes.buffer);
  const source = material.textureDescriptors.baseColor.imageSource;
  assert.equal(h.api.sceneCustomImageDescriptor(material, "albedo", 0).imageSource, source);
  const bundle = { meshObjects: [{ id: "duck", vertexCount: 3, vertexOffset: 0, materialIndex: 0 }], materials: [material], worldMeshPositions: new Float32Array(9), worldMeshUVs: new Float32Array(6) };
  assert.equal(h.env.context.__imageGL.findWater(bundle, "duck").textureDescriptor.imageSource, source);
  h.doc.meshes[0].primitives.push({ attributes: { POSITION: 0 }, material: 0, extensions: { KHR_draco_mesh_compression: {} } });
  assert.throws(() => h.env.context.__extractImageScene(h.doc, bytes.buffer), /[Dd]raco|KHR_draco/);
  assert.equal(h.live.size, 0);
});

test("public preload survives idle eviction and failed replacement preserves usable committed image source", async () => {
  const h = harness(); modelFixture(h);
  const preload = h.env.context.__gosx.scene3d.preloadModel;
  const asset = await preload("/retained.gltf");
  assert.ok(asset.objects.length);
  const source = asset.objects[0].material.textureDescriptors.baseColor.imageSource;
  for (let i = 0; i < 20; i++) await preload("/other.gltf?v=" + i);
  assert.ok(h.env.context.__gosx.scene3d.modelAssetResidency().entries <= 16);
  const lease = h.loader.acquireImageSource(source); assert.ok(lease); lease.release();
  const state = h.api.createSceneState({ scene: { models: [{ id: "old", src: "/retained.gltf" }] } });
  assert.equal((await h.api.hydrate(state, {})).committed, true);
  const old = Array.from(state.objects.values())[0], oldSource = old.textureDescriptors.baseColor.imageSource;
  const bad = await preload("/bad.json"); Object.defineProperty(bad, "objects", { get() { throw Error("staging fault"); } });
  state.models = [{ id: "good", src: "/replacement.gltf" }, { id: "bad", src: "/bad.json" }];
  assert.equal((await h.api.hydrate(state, {})).committed, false);
  assert.equal(Array.from(state.objects.values())[0], old);
  const record = h.env.context.__imageGL.load(h.gl, "", new Map(), { imageSource: oldSource });
  h.images.at(-1).onload(); assert.equal(record.loaded, true); assert.equal(h.live.size, 0);
});

test("shared image decoder survives a peer cancellation and can be restored from the same retained source", () => {
  const h = harness(), descriptor = h.material().textureDescriptors.baseColor;
  const a = h.env.context.__imageGL.load(h.gl, "", new Map(), descriptor);
  const b = h.env.context.__imageGL.load(h.gl, "", new Map(), descriptor);
  const survivor = h.images.at(-1);
  assert.equal(h.live.size, 2); a.disposed = true; a.releaseImage(); assert.equal(h.live.size, 1);
  survivor.onload(); assert.equal(b.loaded, true); assert.equal(h.live.size, 0);
  const restored = h.env.context.__imageGL.load(h.gl, "", new Map(), descriptor);
  h.images.at(-1).onload(); assert.equal(restored.loaded, true); assert.equal(h.live.size, 0);
});

test("extracted Selena loader uses its owner resolver without a global and respects borrowed overrides", async () => {
  const h = harness(), material = h.material(), texture = { name: "albedo" };
  assert.equal(h.env.context.sceneSelenaTextureURL, undefined);
  h.env.context.createImageBitmap = async () => ({ close() {} });
  let resolved = 0;
  const resolve = (m, t, i) => { resolved++; assert.equal(m, material); assert.equal(t, texture); assert.equal(i, 0); return ""; };
  const record = h.env.context.__imageGPU.selena(h.device, material, texture, 0, new Map(), null, resolve);
  assert.equal(resolved, 1); assert.equal(h.live.size, 1); h.images.at(-1).onload(); await flushAsyncWork(); assert.equal(record.loaded, true);
  const borrowed = h.env.context.__imageGPU.selena(h.device, material, texture, 0, new Map(), null, () => "blob:caller");
  assert.equal(h.images.at(-1).url, "blob:caller"); h.images.at(-1).onerror(); assert.equal(borrowed.failed, true); assert.equal(h.live.size, 0);
  const glRecord = h.env.context.__imageGL.selena(h.gl, material, texture, 0, new Map());
  h.images.at(-1).onload(); assert.equal(glRecord.loaded, true); assert.equal(h.live.size, 0);
});

test("water replacement retires pending image and texture without mutating surviving descriptors", () => {
  const h = harness(), first = h.material().textureDescriptors.baseColor, second = h.material().textureDescriptors.baseColor;
  const a = h.env.context.__imageGL.modelWater(h.gl, { textureDescriptor: first }, null);
  const late = h.images.at(-1).onload;
  const b = h.env.context.__imageGL.modelWater(h.gl, { textureDescriptor: second }, a);
  assert.equal(h.live.size, 1); assert.equal(h.calls.filter(c => c[0] === "deleteTexture").length, 1);
  const before = h.calls.length; late(); assert.equal(h.calls.length, before);
  h.images.at(-1).onload(); assert.equal(h.live.size, 0); b.dispose(); b.dispose();
  assert.equal(h.calls.filter(c => c[0] === "deleteTexture").length, 2);
});

test("material dedup distinguishes different embedded images and reuses the same source", () => {
  const h = harness(), a = h.material(); h.bytes[0] = 99; const b = h.material();
  const bundle = { materials: [] }, lookup = new Map();
  assert.equal(h.api.materialIndex(bundle, lookup, a), 0);
  assert.equal(h.api.materialIndex(bundle, lookup, b), 1, "different embedded image must not reuse first material");
  assert.equal(h.api.materialIndex(bundle, lookup, h.api.clone(a)), 0);
});

test("WebGL decode error, upload throw, stale callback and abandoned water replacement release ownership", () => {
  const h = harness(), descriptor = h.material().textureDescriptors.baseColor;
  const error = h.env.context.__imageGL.load(h.gl, "", new Map(), descriptor);
  h.images.at(-1).onerror(); assert.equal(error.failed, true); assert.equal(h.live.size, 0);
  const throwingGL = new Proxy(h.gl, { get(target, key) { if (key === "texImage2D") return (...args) => { if (args.length === 6) throw Error("upload failed"); }; return target[key]; } });
  const failed = h.env.context.__imageGL.load(throwingGL, "", new Map(), descriptor);
  h.images.at(-1).onload(); assert.equal(failed.failed, true); assert.equal(h.live.size, 0);
  const pending = h.env.context.__imageGL.load(h.gl, "", new Map(), descriptor), late = h.images.at(-1).onload;
  pending.disposed = true; pending.releaseImage(); late(); assert.equal(h.live.size, 0); assert.notEqual(pending.loaded, true);
  const water = {};
  h.env.context.__imageGL.water(h.gl, "", descriptor, water);
  const stale = h.images.at(-1).onload, before = h.calls.length;
  water.dispose(); stale(); assert.equal(h.calls.length, before); assert.equal(h.live.size, 0);
});

test("WebGPU disposal destroys pending upload while bitmap is unresolved and closes its late bitmap once", async () => {
  const h = harness(); let resolve;
  h.env.context.createImageBitmap = () => new Promise(r => { resolve = r; });
  const record = h.env.context.__imageGPU.load(h.device, "", new Map(), h.material().textureDescriptors.baseColor);
  assert.equal(h.images.at(-1).crossOrigin, "anonymous");
  h.images.at(-1).onload(); assert.equal(h.textures.length, 2); assert.equal(h.live.size, 1);
  record.disposed = true; record.releaseImage(); assert.equal(h.live.size, 0); assert.equal(h.textures[1].destroyed, 1);
  let closed = 0; resolve({ close() { closed++; } }); await flushAsyncWork();
  assert.equal(closed, 1); assert.equal(h.textures[1].destroyed, 1); assert.notEqual(record.loaded, true);
});

test("WebGPU upload errors and success release decode ownership while retained source permits reupload", async () => {
  for (const mode of ["success", "reject", "throw", "upload"]) {
    const h = harness(), descriptor = h.material().textureDescriptors.baseColor;
    let closed = 0;
    h.env.context.createImageBitmap = () => { if (mode === "throw") throw Error("bitmap throw"); return mode === "reject" ? Promise.reject(Error("bitmap reject")) : Promise.resolve({ close() { closed++; } }); };
    if (mode === "upload") h.device.queue.copyExternalImageToTexture = () => { throw Error("upload throw"); };
    const record = h.env.context.__imageGPU.load(h.device, "", new Map(), descriptor);
    h.images.at(-1).onload(); await flushAsyncWork(); assert.equal(h.live.size, 0, mode);
    assert.equal(Boolean(record.loaded), mode === "success", mode);
    assert.equal(h.textures[1].destroyed, mode === "success" ? 0 : 1, mode);
    assert.equal(closed, ["success", "upload"].includes(mode) ? 1 : 0, mode);
    if (mode === "success") { const again = h.env.context.__imageGPU.load(h.device, "", new Map(), descriptor); assert.equal(h.live.size, 1); again.disposed = true; again.releaseImage(); }
  }
});

for (const asynchronous of [false, true]) test(`throwing ${asynchronous ? "async GPU" : "sync GL"} ready callback cannot corrupt upload or escape`, async () => {
  const h = harness(), record = {}, descriptor = h.material().textureDescriptors.baseColor;
  let notifications = 0;
  h.api.sceneLoadTextureImage(record, descriptor, "", () => {
    record.loaded = true; return asynchronous ? Promise.resolve() : undefined;
  }, () => { notifications++; throw Error("consumer callback"); });
  assert.doesNotThrow(() => h.images.at(-1).onload());
  await flushAsyncWork();
  assert.equal(notifications, 1); assert.equal(record.loaded, true);
  assert.notEqual(record.failed, true); assert.equal(h.live.size, 0);
});
