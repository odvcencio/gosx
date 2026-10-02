"use strict";
const test = require("node:test");
const assert = require("node:assert/strict");
const { loadSceneAdaptiveQualityAPI, FakeElement, createWebGLRendererForPost, makeWebGLBundleWithCustomPost } = require("./runtime-test-harness.js");

const gatedKinds = ["ssao", "contactShadows", "taa"];
function prime(tier, mobile, adaptive = false) {
  const h = loadSceneAdaptiveQualityAPI();
  h.context.navigator = { userAgent: mobile ? "Android Mobile" : "Desktop" };
  const quality = h.api.createSceneAdaptiveQualityState({ qualityTier: tier, adaptiveQuality: adaptive }, {}, {});
  const source = [...gatedKinds.map(kind => ({ kind })), { kind: "toneMapping" }, { kind: "fxaa" }];
  const scene = { postEffects: source, _adaptiveSourcePostEffects: source };
  const mount = new FakeElement("div", null);
  h.api.scenePrimeAdaptiveQuality(quality, {}, mount, scene);
  return { ...h, quality, scene, source, mount };
}

test("post tier admission excludes expensive effects before the first frame, including fixed tiers", () => {
  for (const tier of ["balanced", "survival", "low"]) for (const adaptive of [false, true]) {
    const h = prime(tier, false, adaptive);
    assert.deepEqual(Array.from(h.scene.postEffects, e => e.kind), ["toneMapping", "fxaa"]);
    assert.equal(h.source.length, gatedKinds.length + 2, "authored effects survive tier changes");
  }
  assert.equal(prime("high", false).scene.postEffects.length, gatedKinds.length + 2);
  assert.equal(prime("full", false).scene.postEffects.length, gatedKinds.length + 2);
});

test("mobile excludes expensive effects even when the renderer reports high GPU headroom", () => {
  const h = prime("full", true, true);
  assert.deepEqual(Array.from(h.scene.postEffects, e => e.kind), ["toneMapping", "fxaa"]);
  assert.equal(h.quality.activeTier, "full", "post admission does not change scene resolution or geometry");
});

test("a limited TAA-only chain retains one spatial edge pass", () => {
  const h = prime("balanced", true);
  h.scene._adaptiveSourcePostEffects = [{ kind: "taa" }];
  h.api.scenePrimeAdaptiveQuality(h.quality, {}, h.mount, h.scene);
  assert.deepEqual(Array.from(h.scene.postEffects, e => e.kind), ["fxaa"]);
});

test("an explicit quality ladder admits effects and removes them again without losing authored state", () => {
  const h = prime("balanced", true);
  h.quality.mode = "ladder";
  h.quality.ladder = [{ postEffects: ["toneMapping", "fxaa"] }, { postEffects: [...gatedKinds, "toneMapping", "fxaa"] }];
  for (const [rungIndex, count] of [[1, gatedKinds.length + 2], [0, 2], [1, gatedKinds.length + 2]]) {
    h.quality.rungIndex = rungIndex;
    h.api.scenePrimeAdaptiveQuality(h.quality, {}, h.mount, h.scene);
    assert.equal(h.scene.postEffects.length, count);
  }
});

test("a gated WebGL chain creates no effect targets, depth uploads, or extra draws on steady frames", () => {
  const h = createWebGLRendererForPost({ fresh: true });
  const gl = h.canvas.getContext("webgl2");
  gl.deleteRenderbuffer = () => {};
  const bundle = makeWebGLBundleWithCustomPost();
  bundle.postEffects = prime("balanced", false).scene.postEffects;
  h.renderer.render(bundle, { width: 320, height: 180 });
  const start = gl.ops.length;
  h.renderer.render(bundle, { width: 320, height: 180 });
  const ops = gl.ops.slice(start);
  assert.equal(ops.some(op => ["createTexture", "createFramebuffer", "blitFramebuffer"].includes(op[0])), false);
  assert.equal(ops.some(op => op[0].startsWith("uniform") && ["u_depthTexture", "u_projection", "u_temporalParams", "u_contactParams"].includes(op[1])), false);
  h.renderer.dispose();
});
