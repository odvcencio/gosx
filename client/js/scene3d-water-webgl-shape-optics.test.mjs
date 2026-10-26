import test from "node:test";
import assert from "node:assert/strict";
import vm from "node:vm";
import { readSceneRendererBackendSrc } from "./scene3d-renderer-source-set.js";

const source = readSceneRendererBackendSrc("webgl");
function rendererFunction(name, globals) {
  const start = source.indexOf(`    function ${name}(`);
  const end = source.indexOf("\n    function ", start + 1);
  assert.ok(start >= 0 && end > start, `missing renderer function ${name}`);
  return new Function(...Object.keys(globals), `${source.slice(start, end)}\nreturn ${name};`)(...Object.values(globals));
}

test("WebGL object targets compensate the shared top-origin mesh projector without changing camera matrices", () => {
  let uniforms;
  let draws = 0;
  const gl = new Proxy({}, { get: (_, name) => name === "drawArrays" ? () => draws++ : () => {} });
  const render = rendererFunction("renderMeshTextureTarget", {
    gl, scenePBRProgramReady: () => true, meshUpload: { vao: {}, count: 3 },
    objectTextureMVP: new Float32Array(16), identity4: new Float32Array(16), identity3: new Float32Array(9),
    sceneWaterRenderStateUniform: () => "water", sceneWaterRenderBindSamplers: () => {},
    sceneWaterRenderSetUniforms: (_gl, _program, _desc, value) => { uniforms = value; },
  });
  const vp = new Float32Array([1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, 0.2, 0.1, 0, 1]);
  const original = Array.from(vp);
  const color = [0.78, 0.42, 0.16, 1];
  assert.equal(render({}, {}, { fbo: {}, size: 128 }, vp, 1, null, null, null, [], 1, 1, 1, [], 0.3, 3, color), true);
  const point = [0.2, 0.4, 0, 1];
  const clipY = matrix => matrix[1] * point[0] + matrix[5] * point[1] + matrix[9] * point[2] + matrix[13];
  const glTargetV = clipY(uniforms.mvp) * 0.5 + 0.5;
  const sharedProjectorV = 0.5 - clipY(vp) * 0.5;
  assert.equal(glTargetV, sharedProjectorV, "bottom-origin rasterization must match top-origin projector sampling");
  assert.deepEqual(Array.from(vp), original, "the live direct-draw camera projection stays unchanged");
  assert.equal(uniforms.baseColor, color, "projected targets preserve the authored material color");
  assert.equal(draws, 1);
});

test("WebGL does not report a pending mesh target as rendered", () => {
  const render = rendererFunction("renderMeshTextureTarget", {
    gl: {}, scenePBRProgramReady: () => false,
  });
  assert.equal(render({}, {}, { fbo: {}, size: 128 }, new Float32Array(16), 1), false);
});

function textureHarness() {
  let failures = 0;
  const context = vm.createContext({
    objectTextureSubject: "", objectTextureSlotsReady: [false, false, false],
    meshTexturePassCursor: 0, meshTexturePassCount: 0,
    liveMeshID: "float-duck", isMeshObject: true, meshData: {}, meshProgram: {},
    meshUsesModelTex: true, duckDesc: {}, objectDesc: {}, meshModelTex: {},
    objectRefractionTarget: {}, objectReflectionTarget: {}, objectClippedTarget: {},
    objectRefractionMatrix: [], objectReflectionMatrix: [],
    stateTex: {}, causticTex: {}, liveLightDir: [], livePoolWidth: 1, livePoolLength: 1, livePoolHeight: 1,
    liveCenter: [], liveRadius: 0.3, liveKindNum: 3, liveBaseColor: [1, 1, 1, 1],
    gl: {}, scenePBRProgramReady: () => true, ensureObjectTextureTargets: () => true,
    renderMeshTextureTarget: () => failures-- > 0 ? false : true,
  });
  const start = source.indexOf("      var meshTextureReady = false;");
  const end = source.indexOf("      gl.bindFramebuffer(gl.FRAMEBUFFER, target.framebuffer);", start);
  assert.ok(start >= 0 && end > start);
  return {
    context,
    failNext: () => { failures = 1; },
    frame: (subject = context.liveMeshID) => {
      context.liveMeshID = subject;
      context.isMeshObject = subject === "float-duck" || subject === "float-torus";
      vm.runInContext(source.slice(start, end), context);
      return context.meshTextureReady;
    },
  };
}

test("WebGL selection and None transitions invalidate retained mesh views", () => {
  const h = textureHarness();
  assert.equal(h.frame(), true);
  assert.equal(h.context.meshTexturePassCount, 3);
  assert.equal(h.frame(), true);
  assert.equal(h.context.meshTexturePassCount, 4, "steady state updates one view");
  assert.equal(h.frame("float-torus"), true);
  assert.equal(h.context.meshTexturePassCount, 7, "a new mesh warms all views, not stale duck pixels");
  assert.equal(h.frame(""), false);
  assert.deepEqual(Array.from(h.context.objectTextureSlotsReady), [false, false, false]);
  assert.equal(h.frame("float-torus"), true);
  assert.equal(h.context.meshTexturePassCount, 10, "returning from None warms every view again");
});

test("WebGL exposes only a complete set of successfully rendered mesh views", () => {
  const h = textureHarness();
  h.failNext();
  assert.equal(h.frame(), false);
  assert.equal(h.context.meshTexturePassCount, 2, "only successful target draws count");
  assert.equal(h.frame(), true);
  assert.equal(h.context.meshTexturePassCount, 5);
  h.failNext();
  assert.equal(h.frame(), false, "a failed round-robin update disables projected optics until recovery");
  assert.equal(h.frame(), true);
});

test("WebGL reads the selected shape's authored material color, preserving duck white and safe fallbacks", () => {
  const match = source.match(/  function sceneWaterRenderObjectColor\([\s\S]*?\n  \}/);
  assert.ok(match);
  const color = new Function(`${match[0]}\nreturn sceneWaterRenderObjectColor;`)();
  const orange = [0.78, 0.42, 0.16, 1], white = [1, 1, 1, 1], fallback = [0.52, 0.54, 0.56, 1];
  const bundle = {
    meshObjects: [{ id: "float-torus", materialIndex: 0 }, { id: "float-duck:body", materialIndex: 1 }],
    materials: [{ customUniforms: { baseColor: orange } }, { customUniforms: { baseColor: white } }],
  };
  assert.equal(color(bundle, "float-torus", fallback), orange);
  assert.equal(color(bundle, "float-duck", fallback), white);
  assert.equal(color(bundle, "float-cube", fallback), fallback);
  assert.equal(color(null, "float-torus", fallback), fallback);
  const directStart = source.indexOf("      // ---- 2. object ----");
  const directEnd = source.indexOf("      // ---- 3. surface", directStart);
  assert.equal(source.slice(directStart, directEnd).match(/baseColor: liveBaseColor/g)?.length, 2,
    "both mesh and analytic direct draws use the same authored color");
});

test("WebGL clears a retained shadow for None once, even if the optional shadow program is unavailable", () => {
  let clears = 0;
  const gl = new Proxy({}, { get: (_, name) => {
    if (name === "clear") return () => clears++;
    if (name === "drawArrays" || name === "useProgram") return () => { throw new Error("None needs no shader or draw"); };
    return () => {};
  } });
  const context = vm.createContext({
    gl, isMeshObject: false, liveEntry: {}, liveKindNum: 0, liveMeshID: "", liveCenter: [0, 0, 0],
    liveRadius: 0.3, liveHalf: [0.3, 0.3, 0.3], liveLightDir: [1, 2, -1], livePoolWidth: 1, livePoolLength: 1,
    shadowProgram: null, compoundShadowProgram: {}, shadowTarget: { fbo: {}, size: 128 }, emptyVAO: {},
    lastShadowSignature: "duck", shadowRefreshCount: 0,
    scenePBRProgramReady: () => false, waterShadowSignature: () => "none",
  });
  const start = source.indexOf("      var compoundSphereCount = isMeshObject");
  const end = source.indexOf("      // ---- pre-pass C:", start);
  assert.ok(start >= 0 && end > start);
  const frame = () => vm.runInContext(source.slice(start, end), context);
  frame();
  assert.equal(clears, 1);
  assert.equal(context.lastShadowSignature, "none");
  assert.equal(context.shadowRefreshCount, 1);
  frame();
  assert.equal(clears, 1, "the cached empty footprint does not clear every display frame");
});
