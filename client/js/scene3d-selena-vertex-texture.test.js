"use strict";

// Selena vertex-stage texture sampling (sampleLevel in an authored vertex()):
// the host must make the texture visible to the vertex stage on WebGPU and
// must gate the GLSL ES 1.00 artifact on MAX_VERTEX_TEXTURE_IMAGE_UNITS on WebGL.
// The fixture is the compiled output of Selena's testdata/conformance/
// vertex-texture.sel (heightMap is read by vertex(), tint only by surface()).

const test = require("node:test");
const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");

const {
  bootstrapSource,
  FakeWebGLContext,
  createContext,
  runScript,
  flushAsyncWork,
  createBoardWebGPUHarness,
} = require("./runtime-test-harness.js");

const fixture = JSON.parse(fs.readFileSync(path.join(__dirname, "testdata", "selena-vertex-texture.json"), "utf8"));

const VERTEX = 1;
const FRAGMENT = 2;

test("compiled Selena descriptor marks only the vertex-sampled texture vertexStage", () => {
  const byName = Object.fromEntries(fixture.layout.textures.map((t) => [t.name, t]));
  assert.equal(byName.heightMap.vertexStage, true);
  assert.equal(byName.tint.vertexStage, undefined);
  assert.equal(fixture.layout.requires.glVertexTextureUnits, 1);
});

for (const stageConstants of [true, false]) {
  test(`WebGPU Selena vertex texture visibility with${stageConstants ? "" : "out"} stage constants`, async () => {
    const harness = await createBoardWebGPUHarness({ fresh: true, fakeDeviceOptions: { validateBindings: true } });
    if (!stageConstants) delete harness.env.context.GPUShaderStage;
    const api = harness.env.context.__gosx_scene3d_api;
    const state = api.createSceneState({
      scene: {
        materials: [{
          name: "height-displace",
          kind: "custom",
          shaderBackend: "selena",
          customVertex: fixture.glslVertex,
          customFragment: fixture.glslFragment,
          customVertexWGSL: fixture.wgsl,
          customFragmentWGSL: fixture.wgsl,
          shaderLayout: fixture.layout,
          customUniforms: { heightMap: "/height.png", tint: "/tint.png", scale: 0.25, lod: 0 },
        }],
        objects: [{ id: "plane", kind: "box", width: 1, height: 1, depth: 1, x: 0, y: 0, z: 0, material: "height-displace", wireframe: false }],
      },
    }, { tier: "full" });
    const bundle = api.createSceneRenderBundle(
      64, 64, "#000000",
      { x: 0, y: 0, z: 4, fov: 60, near: 0.05, far: 128 },
      api.sceneStateObjectsWithMaterials(state), [], [], [], [], {}, 0, [], [], [], [], [], 0, false, { retainedGeometry: true },
    );
    for (let frame = 0; frame < 3; frame++) {
      harness.renderer.render(bundle, { width: 64, height: 64 }, { nowMS: frame * 16, active: true });
      await flushAsyncWork();
    }

    const pipeline = harness.fake.state.renderPipelines.find((p) => p.desc && String(p.desc.label || "").indexOf("gosx-selena-") === 0);
    assert.ok(pipeline, "expected a compiled gosx-selena-* render pipeline");
    const entries = pipeline.desc.layout.desc.bindGroupLayouts[0].desc.entries;
    const at = (binding) => entries.find((e) => e.binding === binding);
    // heightMap texture(1)+sampler(2): vertex-visible. tint texture(3)+sampler(4): fragment only.
    assert.equal(at(1).visibility, VERTEX | FRAGMENT, "heightMap texture");
    assert.equal(at(2).visibility, VERTEX | FRAGMENT, "heightMap sampler");
    assert.equal(at(3).visibility, FRAGMENT, "tint texture");
    assert.equal(at(4).visibility, FRAGMENT, "tint sampler");
  });

}

// Renders one Selena mesh with the vertex-texture fixture on a fake WebGL2
// context that reports `vertexUnits` for MAX_VERTEX_TEXTURE_IMAGE_UNITS, and
// returns the GLSL sources the renderer handed to the driver.
async function webglShaderSources(vertexUnits, requires) {
  const env = createContext({ enableWebGL2: true, disableCanvas2D: true });
  env.context.WebGL2RenderingContext = FakeWebGLContext;
  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();
  const api = env.context.__gosx_scene3d_api;
  const backend = api.sceneBackendRegistry.select({ webgl: true, webgl2: true, webgpu: false, canvas: false, canvas2d: false });
  const canvas = env.document.createElement("canvas");
  canvas.width = 64;
  canvas.height = 64;
  env.document.body.appendChild(canvas);
  const gl = canvas.getContext("webgl2");
  const getParameter = gl.getParameter.bind(gl);
  gl.getParameter = (param) => (param === gl.MAX_VERTEX_TEXTURE_IMAGE_UNITS ? vertexUnits : getParameter(param));
  const sources = [];
  const shaderSource = gl.shaderSource.bind(gl);
  gl.shaderSource = (shader, source) => { sources.push(String(source)); return shaderSource(shader, source); };
  const locations = { position: 0, normal: 1, uv: 2 };
  const getAttribLocation = gl.getAttribLocation.bind(gl);
  gl.getAttribLocation = (program, name) => (name in locations ? locations[name] : getAttribLocation(program, name));
  const renderer = backend.create(canvas, { background: "#000000" }, { tier: "full" });
  const layout = JSON.parse(JSON.stringify(fixture.layout));
  if (requires !== undefined) layout.requires = requires;
  const F32 = require("node:vm").runInContext("Float32Array", env.context);
  const object = {
    id: "plane", kind: "mesh", x: 0, y: 0, z: 0, scaleX: 1, scaleY: 1, scaleZ: 1,
    rotationX: 0, rotationY: 0, rotationZ: 0, spinX: 0, spinY: 0, spinZ: 0,
    driftPhase: 0, driftSpeed: 0, shiftX: 0, shiftY: 0, shiftZ: 0,
    materialKind: "custom", shaderBackend: "selena",
    customVertex: fixture.glslVertex, customFragment: fixture.glslFragment,
    shaderLayout: layout, customUniforms: { heightMap: "/height.png", tint: "/tint.png" },
    color: "#ffffff", opacity: 1, wireframe: false, castShadow: false, receiveShadow: false,
    vertices: {
      count: 4, positions: new F32(12), normals: new F32(12), uvs: new F32(8), tangents: new F32(16),
      indices: new Uint32Array([0, 1, 2, 0, 2, 3]), immutable: true, revision: 0, dynamic: false,
    },
  };
  const bundle = api.createSceneRenderBundle(64, 64, "#000000", { x: 0, y: 0, z: 6, fov: 72, near: 0.05, far: 128 },
    [object], [], [], [], [], {}, undefined, 0, [], [], [], [], [], 0, false, { retainedGeometry: true });
  renderer.render(bundle, { cssWidth: 64, cssHeight: 64, pixelWidth: 64, pixelHeight: 64, pixelRatio: 1 });
  renderer.dispose();
  return sources;
}

test("WebGL Selena material needing a vertex texture unit compiles when the device has one", async () => {
  const sources = await webglShaderSources(16);
  assert.ok(sources.some((src) => src.indexOf("texture2DLod(heightMap") >= 0), "the Selena vertex shader must reach the driver");
});

test("WebGL Selena material fails closed when the device has no vertex texture units", async () => {
  const sources = await webglShaderSources(0);
  assert.ok(!sources.some((src) => src.indexOf("texture2DLod(heightMap") >= 0), "the Selena vertex shader must not be compiled on a device with 0 vertex texture units");
});

test("WebGL Selena materials without a vertex texture requirement ignore the device limit", async () => {
  const sources = await webglShaderSources(0, {});
  assert.ok(sources.some((src) => src.indexOf("texture2DLod(heightMap") >= 0));
});
