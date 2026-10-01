"use strict";

const test = require("node:test");
const assert = require("node:assert/strict");
const {
  createBoardWebGPUHarness, waterPerfShapeScene, makeBundleWithCustomPost, makePointsBundle, flushAsyncWork,
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

for (const kind of ["contactShadows", "ssao", "dof", "customPost"]) {
  test(`WebGPU ${kind} consumes current depth across MSAA transitions and resize`, async () => {
    const h = await harnessWithTargets();
    const bundle = makePointsBundle({ id: "p", count: 1, positions: [0, 0, 0] });
    bundle.postEffects = [kind === "customPost" ? makeBundleWithCustomPost({
      fragmentWGSL: "@group(0) @binding(2) var sceneDepth: texture_depth_2d; @fragment fn fragmentMain() -> @location(0) vec4f { return vec4f(textureLoad(sceneDepth, vec2i(0), 0)); }",
    }).postEffects[0] : { kind }];
    if (kind === "customPost") {
      h.renderer.render(bundle, { width: 64, height: 64 });
      await flushAsyncWork();
    }
    try {
      for (const [samples, size] of [[4, 64], [1, 64], [4, 64], [4, 96], [1, 96]]) {
        const start = h.fake.state.renderPasses.length;
        bundle.msaaSamples = samples;
        h.canvas.width = h.canvas.height = size;
        h.renderer.render(bundle, { width: size, height: size });
        const passes = h.fake.state.renderPasses.slice(start);
        const main = passes.find(p => p.descriptor.depthStencilAttachment && p.descriptor.colorAttachments.length);
        const resolve = passes.find(p => p.descriptor.label === "gosx-post-depth-resolve");
        const sampled = h.fake.state.textures.find(t => !t.destroyed && t.desc.format === "depth24plus" && !t.desc.sampleCount && t.desc.size[0] === size);
        assert.equal(main.descriptor.depthStencilAttachment.view.texture.desc.sampleCount || 1, samples);
        checkTargets(h, passes);
        if (samples === 4) {
          assert.ok(resolve, "4x must write sampled depth each frame, including the first and after 1x");
          assert.ok(passes.indexOf(resolve) > passes.indexOf(main));
          assert.equal(resolve.descriptor.depthStencilAttachment.view.texture, sampled);
          assert.equal(resolve.descriptor.depthStencilAttachment.depthStoreOp, "store");
          assert.equal(resolve.descriptor.colorAttachments.length, 0);
          assert.equal(resolve.bindGroups[0].group.desc.entries[0].resource, main.descriptor.depthStencilAttachment.view);
          const pipeline = resolve.draws[0].pipeline.desc;
          assert.equal(pipeline.depthStencil.depthWriteEnabled, true);
          assert.equal(pipeline.depthStencil.depthCompare, "always");
          assert.equal(pipeline.multisample?.count || 1, 1);
          assert.match(pipeline.fragment.module.code, /texture_depth_multisampled_2d/);
          assert.match(pipeline.fragment.module.code, /@builtin\(frag_depth\)/);
          assert.match(pipeline.fragment.module.code, /sample < 4/);
          assert.match(pipeline.fragment.module.code, /min\(depth, textureLoad/);
          assert.equal(resolve.draws[0].vertexCount, 4);
        } else {
          assert.equal(resolve, undefined, "1x writes sampled depth directly");
          assert.equal(main.descriptor.depthStencilAttachment.view.texture, sampled);
        }
        const consumers = passes.filter(p => p.bindGroups.some(b => b.group.desc.entries.some(e => e.resource?.texture === sampled)));
        assert.equal(consumers.length, 1, "the resolved effect must bind the sampled scene depth");
        for (const consumer of consumers) assert.ok(passes.indexOf(consumer) > passes.indexOf(resolve || main));
      }
      assert.equal(h.fake.state.renderPipelines.filter(p => p.desc.label === "gosx-post-depth-resolve").length, 1,
        "resize and sample-count changes reuse the resolve pipeline");
    } finally { h.renderer.dispose(); }
  });
}

test("WebGPU color-only MSAA post chains skip depth resolution", async () => {
  const h = await harnessWithTargets();
  const bundle = makePointsBundle({ id: "p", count: 1, positions: [0, 0, 0] });
  bundle.msaaSamples = 4;
  bundle.postEffects = [{ kind: "fxaa" }, { kind: "colorGrade" }];
  try {
    h.renderer.render(bundle, { width: 64, height: 64 });
    assert.ok(!h.fake.state.renderPasses.some(p => p.descriptor.label === "gosx-post-depth-resolve"));
  } finally { h.renderer.dispose(); }
});
