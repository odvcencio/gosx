"use strict";
const test = require("node:test");
const assert = require("node:assert/strict");
const vm = require("node:vm");
const { freshFeatureBundleSource, createBoardWebGPUHarness, flushAsyncWork, makeFakeGPUDevice, deviceCallSnapshot, deviceCallDelta } = require("./runtime-test-harness.js");
const { readSceneRendererBackendSrc } = require("./scene3d-renderer-source-set.js");
function loaderHarness(width, height, configure) {
  const images = [];
  const win = { __gosx_scene3d_api: {}, navigator: {}, console,
    __gosx_scene3d_webgpu_probe: () => ({ adapter: false, device: null, ready: true }),
    GPUTextureUsage: { COPY_DST: 2, TEXTURE_BINDING: 4, RENDER_ATTACHMENT: 16 },
    GPUShaderStage: { VERTEX: 1, FRAGMENT: 2 },
    Image: class { constructor() { this.width = width; this.height = height; images.push(this); } set src(value) { this.url = value; } },
    createImageBitmap: image => Promise.resolve({ width: image.width, height: image.height, close() {} }),
  };
  win.window = win; win.globalThis = win;
  const context = vm.createContext(win);
  const source = freshFeatureBundleSource("scene3d-webgpu"), end = source.lastIndexOf("})();");
  vm.runInContext(source.slice(0, end) + "window.textureLoader = wgpuLoadTexture;\n" + source.slice(end), context);
  const fake = makeFakeGPUDevice({});
  const create = fake.device.createTexture.bind(fake.device);
  const views = [];
  fake.device.createTexture = desc => {
    if (desc.size[0] === 0 || desc.size[1] === 0) throw new TypeError("invalid image dimensions");
    const texture = create(desc), original = texture.createView.bind(texture);
    texture.createView = descriptor => { views.push({ texture, descriptor }); return original(descriptor); };
    return texture;
  };
  if (configure) configure(fake.device, fake.state);
  const cache = vm.runInContext("new Map()", context);
  async function load(uri, colorSpace = "srgb", device = fake.device, textureCache = cache) {
    const record = win.textureLoader(device, uri, textureCache, { uri, colorSpace, role: "base-color", view: "2d" });
    images.at(-1).onload();
    for (let i = 0; i < 12; i++) await Promise.resolve();
    return record;
  }
  return { fake, win, cache, load, views };
}

for (const [width, height] of [[0, 0], [0, 8], [8, 0]]) {
  test(`WebGPU image ${width}x${height} fails without allocating an invalid texture`, async () => {
    const h = loaderHarness(width, height);
    const record = await h.load("/empty.png");
    assert.equal(record.pending, false);
    assert.equal(record.loaded, false);
    assert.equal(record.failed, true);
    assert.equal(h.fake.state.textures.length, 1);
    assert.notEqual(record.texture.destroyed, true);
    assert.equal(h.fake.state.copyExternalCalls.length, 0);
  });
}

test("WebGPU mip validation failures retain the placeholder and invalidate the pipeline cache", async () => {
  const trace = [];
  let failed = false;
  const h = loaderHarness(8, 4, device => {
    device.pushErrorScope = filter => trace.push("push:" + filter);
    device.popErrorScope = () => {
      trace.push("pop");
      const error = failed ? null : { message: "invalid mip pipeline" };
      failed = true;
      return Promise.resolve(error);
    };
  });
  let closed = 0;
  h.win.ImageBitmap = class { constructor(image) { this.width = image.width; this.height = image.height; } close() { closed++; } };
  h.win.createImageBitmap = image => Promise.resolve(new h.win.ImageBitmap(image));
  const record = await h.load("/bad-validation.png");
  await flushAsyncWork();
  assert.deepEqual(trace, ["push:validation", "pop"]);
  assert.equal(record.loaded, false);
  assert.equal(record.pending, false);
  assert.equal(record.failed, true);
  assert.equal(record.texture, h.fake.state.textures[0]);
  assert.equal(h.fake.state.textures[1].destroyed, true);
  assert.equal(closed, 1, "validation failure closes the decoded bitmap");
  const recovered = await h.load("/good-validation.png");
  assert.equal(recovered.loaded, true);
  assert.equal(h.fake.state.renderPipelines.length, 2, "a validation-failed pipeline cannot be reused");
});

test("WebGPU overlapping image loads close scopes immediately and await their own validation", async () => {
  const trace = [], settle = [];
  const h = loaderHarness(8, 4, device => {
    device.pushErrorScope = filter => trace.push("push:" + filter);
    device.popErrorScope = () => {
      trace.push("pop");
      return new Promise(resolve => settle.push(resolve));
    };
  });
  const first = await h.load("/first.png"), second = await h.load("/second.png");
  assert.deepEqual(trace, ["push:validation", "pop", "push:validation", "pop"]);
  assert.equal(first.pending, true); assert.equal(second.pending, true);
  assert.equal(first.loaded, false); assert.equal(second.loaded, false);
  settle[1](null); await flushAsyncWork();
  assert.equal(second.loaded, true); assert.equal(first.pending, true);
  settle[0]({ message: "first texture failed validation" }); await flushAsyncWork();
  assert.equal(first.failed, true); assert.equal(first.pending, false);
  assert.equal(second.failed, false);
});

test("WebGPU rejected mip validation marks the texture failed", async () => {
  const h = loaderHarness(8, 4, device => { device.popErrorScope = () => Promise.reject(new Error("scope failed")); });
  const record = await h.load("/rejected.png");
  await flushAsyncWork();
  assert.equal(record.failed, true); assert.equal(record.pending, false); assert.equal(record.loaded, false);
  assert.equal(h.fake.state.textures[1].destroyed, true);
});

test("WebGPU equirect reflections use explicit LOD and upload the loaded map's max mip", async () => {
  const source = readSceneRendererBackendSrc("webgpu");
  assert.match(source, /textureSampleLevel\(envMapTex, envMapSampler, envEquirectUV\(Nr\), maxLod\)/);
  assert.match(source, /textureSampleLevel\(envMapTex, envMapSampler, envEquirectUV\(Rr\), roughness \* maxLod\)/);
  assert.doesNotMatch(source, /textureSample\(envMapTex,/);
  const h = await createBoardWebGPUHarness({ fresh: true, configureDevice(device, state, env) {
    env.context.Image = class { constructor() { this.width = 8; this.height = 4; } set src(value) { Promise.resolve().then(() => this.onload()); } };
  } });
  try {
    const api = h.env.context.__gosx_scene3d_api;
    const object = api.normalizeSceneObject({ id: "reflection", kind: "sphere", color: "#ffffff", metalness: 1 }, 0, null);
    const bundle = api.createSceneRenderBundle(64, 64, "#000000", { x: 0, y: 0, z: 5, fov: 60 }, [object], [], [], [], [], { envMap: "/env.png" }, 0, [], [], [], [], [], 0, false);
    for (let frame = 0; frame < 3; frame++) { h.renderer.render(bundle, { width: 64, height: 64 }, { nowMS: frame * 16, active: true }); await flushAsyncWork(); }
    const uploaded = () => h.fake.state.writeBufferCalls.filter(w => w.data.byteLength === 80).at(-1).data;
    assert.equal(new Uint32Array(uploaded().buffer)[16], 1);
    assert.equal(uploaded()[17], 3);
    bundle.environment.envMap = "";
    h.renderer.render(bundle, { width: 64, height: 64 }, { nowMS: 48, active: true }); await flushAsyncWork();
    assert.equal(uploaded()[17], 0, "an inactive map clears its old max LOD");
  } finally { h.renderer.dispose(); }
});
for (const [width, height] of [[256, 128], [13, 5], [1, 16], [1, 1]]) {
  test(`WebGPU image ${width}x${height} generates its full mip chain once`, async () => {
    const h = loaderHarness(width, height), before = deviceCallSnapshot(h.fake);
    const record = await h.load("/checker.png");
    const count = Math.floor(Math.log2(Math.max(width, height))) + 1;
    const cost = deviceCallDelta(before, deviceCallSnapshot(h.fake));
    console.log(`image ${width}x${height}`, JSON.stringify({ ...cost, textures: h.fake.state.textures.length, externalCopies: h.fake.state.copyExternalCalls.length }));
    assert.equal(record.loaded, true);
    assert.equal(record.texture.desc.mipLevelCount, count);
    assert.equal(cost.renderPasses, count - 1);
    assert.equal(cost.bindGroups, count - 1);
    assert.equal(cost.renderPipelines, count > 1 ? 1 : 0);
    assert.equal(h.fake.state.copyExternalCalls.length, 1);
    assert.equal(cost.writeBufferBytes, 0);
    const sources = h.views.filter(v => v.descriptor?.mipLevelCount === 1 && v.descriptor.baseMipLevel < count - 1);
    assert.ok(sources.length >= count - 1);
    for (let frame = 0; frame < 5; frame++) assert.equal(h.win.textureLoader(h.fake.device, "/checker.png", h.cache,
      { uri: "/checker.png", colorSpace: "srgb", role: "base-color", view: "2d" }), record);
    assert.deepEqual(deviceCallSnapshot(h.fake), { ...before, ...Object.fromEntries(Object.entries(cost).map(([k, v]) => [k, before[k] + v])) });
    assert.equal(h.fake.state.copyExternalCalls.length, 1);
  });
}
test("WebGPU mip pipelines cache per device and format across image textures", async () => {
  const h = loaderHarness(8, 4);
  await h.load("/one.png");
  await h.load("/two.png");
  assert.equal(h.fake.state.renderPipelines.length, 1);
  await h.load("/linear.png", "linear");
  assert.equal(h.fake.state.renderPipelines.length, 2);
  assert.deepEqual(h.fake.state.renderPipelines.map(p => p.desc.fragment.targets[0].format), ["rgba8unorm-srgb", "rgba8unorm"]);
  assert.equal(h.fake.state.renderPasses.length, 9);
});
test("WebGPU mip generation failure keeps the placeholder and reports the image failed", async () => {
  const h = loaderHarness(16, 16);
  h.fake.device.createRenderPipeline = () => { throw new Error("mip pipeline failed"); };
  const record = await h.load("/failed.png");
  assert.equal(record.loaded, false);
  assert.equal(record.pending, false);
  assert.equal(record.failed, true);
  assert.equal(record.texture, h.fake.state.textures[0]);
  assert.equal(h.fake.state.textures[1].destroyed, true);
  assert.equal(h.fake.state.renderPasses.length, 0);
});
test("WebGPU mip pipeline state is rebuilt for a replacement device", async () => {
  const first = loaderHarness(8, 8), restored = makeFakeGPUDevice({});
  await first.load("/same.png");
  await first.load("/same.png", "srgb", restored.device, new Map());
  assert.equal(first.fake.state.renderPipelines.length, 1);
  assert.equal(restored.state.renderPipelines.length, 1);
  assert.notEqual(first.fake.state.renderPipelines[0], restored.state.renderPipelines[0]);
});
