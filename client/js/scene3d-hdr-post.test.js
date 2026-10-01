"use strict";

const test = require("node:test");
const assert = require("node:assert/strict");
const {
  createBoardWebGPUHarness, waterPerfShapeScene, makeBundleWithCustomPost, flushAsyncWork,
} = require("./runtime-test-harness.js");

async function harnessWithTargets() {
  const h = await createBoardWebGPUHarness({ fresh: true });
  const create = h.fake.device.createTexture;
  h.fake.device.createTexture = function(desc) {
    const texture = create.call(this, desc);
    texture.createView = () => ({ texture });
    return texture;
  };
  h.fake.device.createRenderPipelineAsync = desc => Promise.resolve(h.fake.device.createRenderPipeline(desc));
  return h;
}

function checkTargets(h, passes) {
  const presentationFormat = h.configureCalls.at(-1).format;
  for (const pass of passes) {
    const attachment = pass.descriptor.colorAttachments?.[0];
    if (!attachment) continue;
    const texture = attachment.view.texture;
    const format = texture?.desc.format || presentationFormat;
    for (const draw of [...pass.draws, ...pass.drawIndexeds, ...pass.drawIndirects]) {
      if (!draw.pipeline?.desc?.fragment) continue;
      assert.equal(draw.pipeline.desc.fragment.targets[0].format, format,
        "each draw pipeline must match its attachment format");
      assert.equal(draw.pipeline.desc.multisample?.count || 1, texture?.desc.sampleCount || 1);
    }
    if (attachment.resolveTarget?.texture) {
      assert.equal(attachment.resolveTarget.texture.desc.format, format);
    }
  }
}

test("HDR post targets survive water, MSAA, resize, and post toggles", async () => {
  const h = await harnessWithTargets();
  const api = h.env.context.__gosx_scene3d_api;
  const { state, objects } = waterPerfShapeScene(api, false, 96);
  const bundle = api.createSceneRenderBundle(64, 64, "#000000",
    { x: 0, y: 1, z: 4, fov: 60, near: 0.05, far: 128 },
    objects, [], [], [], [], {}, 0, [], [], [], state.waterSystems, [], 0, false);
  const effects = [{ kind: "bloom", threshold: 1.06 }, { kind: "toneMapping", mode: "aces" }, { kind: "fxaa" }];
  for (const [size, samples, post] of [[64, 1, true], [64, 4, true], [64, 4, false], [64, 4, true], [96, 4, true]]) {
    const start = h.fake.state.renderPasses.length;
    h.canvas.width = h.canvas.height = size;
    bundle.msaaSamples = samples;
    bundle.postEffects = post ? effects : [];
    h.renderer.render(bundle, { width: size, height: size });
    const passes = h.fake.state.renderPasses.slice(start);
    checkTargets(h, passes);
    if (post) {
      const hdr = h.fake.state.textures.filter(t => !t.destroyed && t.desc.format === "rgba16float");
      assert.equal(hdr.filter(t => t.desc.size[0] === size && !t.desc.sampleCount).length, 2,
        "scene and auxiliary textures retain HDR radiance");
      assert.equal(hdr.filter(t => t.desc.size[0] === size / 2).length, 2,
        "both bloom textures retain values above the threshold");
      const last = passes.at(-1);
      assert.equal(last.descriptor.colorAttachments[0].view.__kind, "canvasTextureView");
      assert.equal(last.draws[0].pipeline.desc.fragment.module.label, "post-present");
    }
    assert.notEqual(h.configureCalls.at(-1).format, "rgba16float", "the canvas keeps its presentation format");
  }
  h.renderer.dispose();
  assert.ok(h.fake.state.textures.filter(t => t.desc.format === "rgba16float").every(t => t.destroyed));
});

test("custom post and its pending fallback use HDR until presentation", async () => {
  const h = await harnessWithTargets();
  const bundle = makeBundleWithCustomPost({ fragmentWGSL: "@fragment fn fragmentMain() -> @location(0) vec4f { return vec4f(4.0); }" });
  h.canvas.width = 320; h.canvas.height = 180;
  h.renderer.render(bundle, { width: 320, height: 180 });
  await flushAsyncWork();
  const start = h.fake.state.renderPasses.length;
  h.renderer.render(bundle, { width: 320, height: 180 });
  const passes = h.fake.state.renderPasses.slice(start);
  checkTargets(h, passes);
  const custom = passes.flatMap(p => p.draws).find(d => d.pipeline?.desc?.label === "gosx-selena-post-test-lens");
  assert.ok(custom, "the resolved custom post shader must draw");
  assert.equal(custom.pipeline.desc.fragment.targets[0].format, "rgba16float");
  assert.equal(passes.at(-1).descriptor.colorAttachments[0].view.__kind, "canvasTextureView");
  h.renderer.dispose();
});
