import test from "node:test";
import assert from "node:assert/strict";
import { readSceneRendererBackendSrc } from "./scene3d-renderer-source-set.js";

const source = readSceneRendererBackendSrc("webgpu");
function rendererFunction(name, globals = {}) {
  const start = source.indexOf(`function ${name}(`);
  assert.ok(start >= 0, `missing renderer function ${name}`);
  let depth = 0;
  for (let i = source.indexOf("{", start); i < source.length; i++) {
    if (source[i] === "{") depth++;
    if (source[i] === "}" && --depth === 0) {
      return new Function(...Object.keys(globals), `${source.slice(start, i + 1)}\nreturn ${name};`)(...Object.values(globals));
    }
  }
  throw new Error(`unclosed renderer function ${name}`);
}

const sceneNumber = (value, fallback) => Number.isFinite(Number(value)) ? Number(value) : fallback;
const waterSystemHasObjectTextureSubject = system => !!system?.waterObjectActive && system.waterObjectKind === 3;
const sceneWaterObjectTextureTargetSlots = rendererFunction("sceneWaterObjectTextureTargetSlots", { sceneNumber });

function materialPackingHarness() {
  const system = {
    id: "pond-a", entry: {}, waterResolution: 128,
    waterPoolWidth: 1.5, waterPoolHeight: 0.8, waterPoolLength: 2,
    waterObjectCenter: { x: -0.4, y: -0.87, z: 0.2 },
    waterObjectKind: 3, waterObjectSubtype: 1, waterObjectRadius: 0.31,
    waterLightDir: { x: 1.4, y: 2.8, z: -1.6 },
  };
  const material = {
    customUniforms: {
      causticTexture: "gosx:water:pond-a:caustics",
      poolWidth: 1, poolHeight: 1, poolLength: 1,
      baseColor: [0.78, 0.42, 0.16, 1],
      isTexturePass: 0, texturePassMode: 0, waterObjectTexturePassMode: 0,
    },
    shaderLayout: { uniformBlock: { size: 80, fields: [
      { name: "poolWidth", type: "float", offset: 0 },
      { name: "poolHeight", type: "float", offset: 4 },
      { name: "poolLength", type: "float", offset: 8 },
      { name: "objectRadius", type: "float", offset: 12, class: "context" },
      { name: "objectCenter", type: "vec3", offset: 16, class: "context" },
      { name: "objectKind", type: "float", offset: 28, class: "context" },
      { name: "baseColor", type: "vec4", offset: 32 },
      { name: "isTexturePass", type: "float", offset: 48 },
      { name: "texturePassMode", type: "float", offset: 52 },
      { name: "waterObjectTexturePassMode", type: "float", offset: 56 },
      { name: "lightDir", type: "vec3", offset: 64, class: "context" },
    ] } },
  };
  const g = { sceneNumber,
    waterSystems: new Map([[system.id, { system }]]),
    sceneWaterResolution: () => 256,
    sceneWaterObjectKind: () => 0, sceneWaterObjectSubtype: () => 0,
    sceneWaterPoolShapeRounded: () => false,
    sceneWaterLightVector: () => ({ x: 1, y: 1, z: 1 }),
    selenaFrame: {},
  };
  for (const name of [
    "sceneSelenaMaterialLayout", "sceneSelenaFloatCount", "sceneSelenaUniformDefault",
    "sceneSelenaRenderContextUniformValue", "sceneSelenaMaterialValue",
    "sceneSelenaUniformValue", "sceneSelenaScalar", "sceneSelenaWriteArrayUniformField",
    "sceneSelenaWriteUniformField", "sceneSelenaUniformData",
    "sceneSelenaResourceRef", "sceneSelenaParseResourceRef", "sceneSelenaWaterSystem",
    "sceneWaterObjectTextureSelenaUniforms", "sceneWaterObjectTextureSelenaContext",
    "sceneWaterMainSelenaContext",
  ]) g[name] = rendererFunction(name, g);
  let packed;
  const create = rendererFunction("createSelenaBindGroup", {
    ...g,
    sceneSelenaUniformData: (m, owner, renderContext, frame) => {
      packed = g.sceneSelenaUniformData(m, owner, renderContext, frame);
      return null; // Stop before GPU allocation; packing is the actual implementation.
    },
  });
  return {
    system, material,
    context: mode => g.sceneWaterObjectTextureSelenaContext(system, mode, "test"),
    pack: context => { create(material, {}, {}, context); return Array.from(packed); },
  };
}

function assertLiveObjectUniforms(h, packed) {
  const s = h.system;
  const expected = [s.waterPoolWidth, s.waterPoolHeight, s.waterPoolLength, s.waterObjectRadius,
    s.waterObjectCenter.x, s.waterObjectCenter.y, s.waterObjectCenter.z, s.waterObjectKind,
    ...h.material.customUniforms.baseColor];
  assert.deepEqual(packed.slice(0, 12), Array.from(new Float32Array(expected)));
}

test("water main-scene material packing resolves live context from its own water resource", () => {
  const h = materialPackingHarness();
  for (const kind of [1, 2, 3]) {
    h.system.waterObjectKind = kind;
    const packed = h.pack();
    assertLiveObjectUniforms(h, packed);
    assert.deepEqual(packed.slice(12, 15), [0, 0, 0], "direct draws preserve authored pass flags");
  }
  h.material.customUniforms.isTexturePass = 0.25;
  h.material.customUniforms.texturePassMode = 0.75;
  h.material.customUniforms.waterObjectTexturePassMode = 0.5;
  assert.deepEqual(h.pack().slice(12, 15), [0.25, 0.75, 0.5], "live context must not overwrite authored direct-pass flags");
  h.material.customUniforms.causticTexture = "/ordinary-texture.png";
  const ordinary = h.pack();
  assert.deepEqual(ordinary.slice(0, 3), [1, 1, 1], "ordinary materials retain their own pool-like parameters");
  assert.equal(ordinary[7], 0, "unresolved materials do not inherit another water system's subject");
});

test("water RTT material packing supplies declared live object fields and preserves explicit context", () => {
  const h = materialPackingHarness();
  for (const mode of [1, 2]) {
    const context = h.context(mode);
    const packed = h.pack(context);
    assertLiveObjectUniforms(h, packed);
    assert.deepEqual(packed.slice(12, 15), [1, mode, mode]);
    context.uniforms.objectKind = 2;
    context.uniforms.poolWidth = 2.5;
    context.uniforms.baseColor = [0.2, 0.3, 0.4, 1];
    const explicit = h.pack(context);
    assert.equal(explicit[0], 2.5, "supplied RTT context stays authoritative");
    assert.equal(explicit[7], 2);
    assert.deepEqual(explicit.slice(8, 12), Array.from(new Float32Array([0.2, 0.3, 0.4, 1])));
  }
});

function textureHarness(failedPasses = 0, targetPass) {
  const system = {
    entry: { activeObject: "Rubber Duck" },
    waterObjectActive: true, waterObjectKind: 3, waterObjectSubtype: 2,
    objectViewProjectionMatrix: new Float32Array(16),
    objectReflectionViewProjectionMatrix: new Float32Array(16),
  };
  const globals = {
    sceneNumber, waterSystemHasObjectTextureSubject, sceneWaterObjectTextureTargetSlots,
    sceneWaterActiveObjectID: entry => entry.activeObject,
    sceneWaterObjectKind: () => system.waterObjectKind,
    sceneWaterOpticsFlags: () => ({ object: true, reflection: true, refraction: true }),
    sceneWaterObjectMeshList: () => [{ id: system.entry.activeObject }],
    sceneWaterObjectMeshCandidateProfile: () => "",
    sceneWaterObjectRenderSignature: () => system.entry.activeObject,
    renderWaterObjectTexturePass: () => 0,
    renderWaterObjectMeshTargetPass: targetPass || (() => failedPasses-- > 0
      ? { passes: 0, drawCalls: 0, selenaDrawCalls: 0 }
      : { passes: 1, drawCalls: 1, selenaDrawCalls: 1 }),
    addWaterObjectTextureStats: () => {},
    uploadFrameUniforms: () => {}, uploadWaterReflectionFrameUniforms: () => {}, uploadLights: () => {},
    scratchSelenaViewProjection: new Float32Array(16), WATER_OBJECT_TEXTURE_SIZE: 128,
  };
  const render = rendererFunction("renderWaterObjectSceneTexturePasses", globals);
  const surface = rendererFunction("sceneWaterSurfaceSelenaRenderContext", {
    sceneNumber, waterSystemHasObjectTextureSubject, sceneWaterObjectTextureTargetSlots,
    sceneWaterCameraPosFromCam: () => ({ x: 0, y: 2, z: 3 }),
    sceneWaterPoolShapeRounded: () => false,
    sceneColorRGBA: () => [0.5, 0.8, 0.9, 1],
    sceneWaterSpheresContextArray: () => [], sceneWaterKnotContextArray: () => [],
    scratchSelenaViewProjection: new Float32Array(16),
  });
  return {
    system,
    frame: () => render([{ system }], {}, { meshObjects: [], camera: {} }, [], {}, {}, 800, 600, false),
    enabled: () => surface(system, {}, "test").uniforms.meshTextureEnable,
  };
}

test("water enables real-mesh optics only after the selected shape's required targets render", () => {
  const h = textureHarness();
  assert.equal(h.enabled(), 0);
  h.frame();
  assert.equal(h.enabled(), 0, "one duck view is not a complete target set");
  h.frame();
  assert.equal(h.enabled(), 1, "duck refraction and clipped reflection are ready");
  h.system.entry.activeObject = "TorusKnot";
  h.system.waterObjectSubtype = 1;
  h.frame();
  assert.equal(h.enabled(), 0, "a previous duck target must not make torus optics ready");
  h.frame();
  assert.equal(h.enabled(), 1, "torus refraction and reflected-camera targets are ready");
  h.system.waterObjectActive = false;
  h.system.waterObjectKind = 0;
  h.system.entry.activeObject = "None";
  h.frame();
  assert.equal(h.enabled(), 0);
  h.system.waterObjectActive = true;
  h.system.waterObjectKind = 3;
  h.system.entry.activeObject = "TorusKnot";
  h.frame();
  assert.equal(h.enabled(), 0, "returning from None must warm both views again");
  h.frame();
  assert.equal(h.enabled(), 1);
});

test("water retries a pending mesh-target pass before exposing projected optics", () => {
  const h = textureHarness(1);
  h.frame();
  assert.equal(h.enabled(), 0);
  h.frame();
  assert.equal(h.enabled(), 0, "a failed refraction pass must not be marked ready");
  h.frame();
  assert.equal(h.enabled(), 1, "successful retry completes the required views");
});

test("water projected targets wait for deferred model texture decode and GPU upload", async () => {
  let image, decode, upload, settled = 0, textureLoads = 0;
  const decoded = new Promise(resolve => { decode = resolve; });
  const uploaded = new Promise(resolve => { upload = resolve; });
  const texture = () => ({ createView: () => ({}), destroy() {} });
  const device = { createTexture: texture, queue: { writeTexture() {}, copyExternalImageToTexture() {} } };
  const textureCache = new Map();
  textureCache._gosxGeneration = { onResourceReady() { settled++; } };
  const g = {
    sceneNumber, device, textureCache,
    GPUTextureUsage: { TEXTURE_BINDING: 1, COPY_DST: 2, RENDER_ATTACHMENT: 4 },
    Image: class { constructor() { image = this; this.width = this.height = 512; } },
    createImageBitmap: () => decoded,
    wgpuGenerateTextureMips: () => uploaded,
    wgpuCreateImageTexture: () => ({ texture: texture(), format: "rgba8unorm", levels: 10 }),
    wgpuTextureRecordDisposed: () => false,
  };
  for (const name of ["wgpuTextureDescriptor", "wgpuTextureCacheKey", "wgpuIsKTX2URL", "wgpuNotifyTextureSettled", "wgpuLoadTexture",
    "sceneSelenaMaterialValue", "sceneSelenaParseResourceRef", "sceneSelenaResourceRef", "sceneSelenaTextureURL", "sceneSelenaTextureDescriptors"]) {
    g[name] = rendererFunction(name, g);
  }
  const actualLoad = g.wgpuLoadTexture;
  g.wgpuLoadTexture = (...args) => { textureLoads++; return actualLoad(...args); };
  g.wgpuLoadCubeTexture = () => { throw new Error("this material has no cube upload"); };
  const material = { customUniforms: { modelTexture: "/duck-albedo.png", causticTexture: "gosx:water:pond-a:caustics" } };
  const resource = { pipeline: {}, layout: { textures: [{ name: "modelTexture" }, { name: "causticTexture" }] } };
  let draws = 0;
  const draw = rendererFunction("drawWaterObjectMeshObjects", {
    sceneNumber,
    scenePBRObjectRenderPass: () => "opaque",
    getSelenaPipeline: () => resource,
    WATER_OBJECT_TEXTURE_FORMAT: "rgba8unorm",
    wgpuRequirePipeline: value => value,
    sceneWaterObjectSelenaTexturesPending: (...args) => rendererFunction("sceneWaterObjectSelenaTexturesPending", g)(...args),
    createSelenaBindGroup: () => { actualLoad(device, "/duck-albedo.png", textureCache); return {}; },
    bindWaterObjectSelenaAttributes: () => true,
  });
  const pass = { setPipeline() {}, setBindGroup() {}, draw() { draws++; } };
  const h = textureHarness(0, () => {
    const result = draw(pass, [{ vertexCount: 3 }], {}, [material], {}, {}, 1, {});
    return { passes: result.drawCalls ? 1 : 0, ...result };
  });
  h.frame(); h.frame();
  assert.equal(h.enabled(), 0, "the white placeholder is not a successfully shaded target");
  assert.equal(draws, 0);
  const record = [...textureCache.values()][0];
  assert.equal(record.pending, true);
  image.onload();
  await Promise.resolve();
  h.frame();
  assert.equal(h.enabled(), 0, "image decode is still pending");
  decode({});
  await Promise.resolve(); await Promise.resolve();
  h.frame();
  assert.equal(h.enabled(), 0, "mipmap upload is still pending");
  upload();
  await Promise.resolve(); await Promise.resolve();
  assert.equal(record.loaded, true);
  assert.equal(record.pending, false);
  assert.equal(settled, 1);
  h.frame(); h.frame();
  assert.equal(h.enabled(), 1, "both required views repaint after the real albedo reaches the GPU");
  assert.equal(draws, 2);
  assert.equal(textureCache.size, 1, "the live caustic resource must never become an image load");
  const loadsAfterReady = textureLoads;
  h.frame(); h.frame();
  assert.equal(textureLoads, loadsAfterReady, "settled targets do not become permanent readiness retries");
  record.loaded = false;
  record.failed = true;
  h.system.waterObjectTextureSubject = null;
  h.frame(); h.frame();
  assert.equal(h.enabled(), 1, "a settled upload failure retains the existing fallback rather than retrying forever");
});

test("water invalidates retained optics when quality allocation replaces target textures", () => {
  const start = source.indexOf("    function applySceneWaterQualityProfile(");
  const end = source.indexOf("    function retireWaterSystem(", start);
  const quality = source.slice(start, end);
  const committed = quality.slice(quality.indexOf("      system._cubemapRenderBindGroups = null;"));
  assert.match(committed, /system\.waterObjectTextureSubject = null;/);
  assert.match(committed, /system\.waterObjectShadowSignature = null;/);
  const h = textureHarness();
  h.frame(); h.frame();
  assert.equal(h.enabled(), 1);
  h.system.waterObjectTextureSubject = null;
  h.frame();
  assert.equal(h.enabled(), 0, "new textures need a complete set of successful views");
  h.frame();
  assert.equal(h.enabled(), 1);
});

test("water invalidates a compound shadow once when None is selected, even without caustics", () => {
  const start = source.indexOf("        if (optics.object || optics.caustics");
  const end = source.indexOf("        if (optics.caustics && refreshExpensivePasses)", start);
  assert.ok(start >= 0 && end > start);
  let clears = 0;
  const system = { waterObjectShadowSignature: "duck" };
  const globals = {
    waterSystemHasObjectTextureSubject: () => false,
    sceneWaterObjectMeshList: () => [],
    sceneWaterObjectRenderSignature: () => "none",
    renderWaterObjectMeshShadowPass: () => { throw new Error("None must not draw a mesh shadow"); },
    renderWaterObjectShadowPass: () => { clears++; return { passes: 1 }; },
  };
  const update = new Function(...Object.keys(globals), "system", "optics", "stats", "entry", "bundle", "pbrBuffers", "encoder", source.slice(start, end));
  const run = () => update(...Object.values(globals), system, { object: false, caustics: false }, {}, {}, {}, {}, {});
  run();
  assert.equal(clears, 1, "the retained duck silhouette needs a transparent clear");
  assert.equal(system.waterObjectShadowSignature, "");
  run();
  assert.equal(clears, 1, "an unchanged empty subject must not clear every display frame");
});

test("water counts an encoded empty shadow clear as a successful cached pass", () => {
  const encoded = [];
  const render = rendererFunction("renderWaterObjectShadowPass", { waterSystemHasObjectTextureSubject: () => false });
  const result = render({ beginRenderPass: descriptor => {
    encoded.push(descriptor);
    return { end() {} };
  } }, { objectShadowView: {} });
  assert.equal(encoded.length, 1);
  assert.deepEqual(encoded[0].colorAttachments[0].clearValue, { r: 0, g: 0, b: 0, a: 0 });
  assert.equal(result.passes, 1, "encoded clears must commit the empty-subject signature");
});
