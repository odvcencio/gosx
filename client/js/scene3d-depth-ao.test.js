"use strict";
const test = require("node:test");
const assert = require("node:assert/strict");
const { createWebGLRendererForPost, makeWebGLBundleWithCustomPost, loadSceneAdaptiveQualityAPI } = require("./runtime-test-harness.js");

test("depth AO binds the scene depth and actual projection, including orthographic cameras", () => {
  for (const camera of [{ x: 0, y: 1, z: 5, fov: 70, near: 0.1, far: 100 },
    { kind: "orthographic", x: 0, y: 1, z: 5, near: 0.1, far: 100, orthoSize: 8 }]) {
    const h = createWebGLRendererForPost({ fresh: true });
    const gl = h.canvas.getContext("webgl2");
    gl.TEXTURE1 = gl.TEXTURE0 + 1;
    gl.deleteRenderbuffer = () => {};
    const matrices = [];
    const upload = gl.uniformMatrix4fv.bind(gl);
    gl.uniformMatrix4fv = (loc, transpose, value) => { if (loc.name === "u_projection") matrices.push(Array.from(value)); upload(loc, transpose, value); };
    const bundle = makeWebGLBundleWithCustomPost();
    bundle.camera = camera;
    // Three consecutive depth readers exercise both scratch targets and depth.
    bundle.postEffects = [{ kind: "ssao", radius: 12, intensity: 0.8, bias: 0.02 },
      { kind: "ssao", radius: 4 }, { kind: "ssao", radius: 2 }, { kind: "fxaa" }];
    h.renderer.render(bundle, { width: 320, height: 180 });
    assert.equal(matrices.length, 3);
    assert.equal(matrices[0][11], camera.kind === "orthographic" ? 0 : -1);
    const attachments = new Map();
    const colorAttachments = new Map();
    let framebuffer = null, unit = gl.TEXTURE0;
    const textures = new Map();
    let checked = 0;
    for (const op of gl.ops) {
      if (op[0] === "bindFramebuffer") framebuffer = op[2];
      if (op[0] === "framebufferTexture2D" && op[2] === gl.DEPTH_ATTACHMENT) attachments.set(framebuffer, op[4]);
      if (op[0] === "framebufferTexture2D" && op[2] === gl.COLOR_ATTACHMENT0) colorAttachments.set(framebuffer, op[4]);
      if (op[0] === "activeTexture") unit = op[1];
      if (op[0] === "bindTexture") textures.set(unit, op[2]);
      if (op[0] === "uniform1i" && op[1] === "u_depthTexture") {
        const depth = textures.get(gl.TEXTURE0 + op[2]);
        assert.ok(depth, "AO samples a real depth texture");
        assert.notEqual(attachments.get(framebuffer), depth, "depth reader has a separate output attachment");
        assert.notEqual(colorAttachments.get(framebuffer), textures.get(gl.TEXTURE0), "color input also differs from the output attachment");
        checked++;
      }
    }
    assert.equal(checked, 3);
    assert.ok(gl.ops.some(op => op[0] === "uniform1f" && op[1] === "u_bias" && op[2] === 0.02));
    assert.ok(h.warnLog.every(w => w.includes("RGBA8 LDR intermediate")));
    h.renderer.dispose();
  }
});

test("AO remains opt-in and its quality rung removes the depth pass", () => {
  const h = createWebGLRendererForPost({ fresh: true });
  h.canvas.getContext("webgl2").deleteRenderbuffer = () => {};
  const api = h.env.context.__gosx_scene3d_api;
  const state = api.createSceneState({ scene: {
    postEffects: [{ kind: "ssao" }, { kind: "toneMapping" }, { kind: "fxaa" }],
    qualityLadder: [{ name: "low", postEffects: ["toneMapping", "fxaa"] },
      { name: "high", postEffects: ["ssao", "toneMapping", "fxaa"] }], qualityStartRung: 0,
  } });
  const quality = loadSceneAdaptiveQualityAPI();
  quality.api.sceneApplyQualityLadderRung(state, { mode: "ladder", rungIndex: 0, ladder: [{ postEffects: ["toneMapping", "fxaa"], layerGroups: [] }] });
  assert.equal(state.postEffects.some(e => e.kind === "ssao"), false);
  const bundle = makeWebGLBundleWithCustomPost();
  bundle.postEffects = state.postEffects;
  h.renderer.render(bundle, { width: 320, height: 180 });
  assert.equal(h.canvas.getContext("webgl2").ops.some(op => op[0] === "uniform1i" && op[1] === "u_depthTexture"), false);
  h.renderer.dispose();
});

test("orthographic off-axis AO faces parallel viewing rays and retains front-side occlusion", () => {
  const h = createWebGLRendererForPost({ fresh: true });
  const gl = h.canvas.getContext("webgl2"); gl.deleteRenderbuffer = () => {};
  try {
    const bundle = makeWebGLBundleWithCustomPost();
    bundle.camera = { kind: "orthographic", x: 0, y: 0, z: 5, orthoSize: 8 };
    bundle.postEffects = [{ kind: "ssao" }]; h.renderer.render(bundle, { width: 320, height: 180 });
    const source = gl.programMatching("postViewNormal").attached.find(s => s.type === gl.FRAGMENT_SHADER).source;
    const expression = source.match(/vec3 viewRay = ([^;]+);/)?.[1] || "p";
    const ray = new Function("u_projection", "p", "vec3", `return ${expression};`)(
      [[], [], [0, 0, 0, 0]], [3, 0, -1], (...v) => v);
    const n = [Math.SQRT1_2, 0, Math.SQRT1_2];
    const facing = n.reduce((sum, v, i) => sum + v * ray[i], 0) > 0 ? n.map(v => -v) : n;
    const occluder = [0.2, 0, 0.2];
    assert.ok(facing[2] > 0, "off-axis sloped plane faces the viewer");
    assert.ok(facing.reduce((sum, v, i) => sum + v * occluder[i], 0) > 0, "front-side occluder contributes AO");
  } finally { h.renderer.dispose(); }
});

test("SSAO tone mapping FXAA allocates only color targets that receive draws", () => {
  const h = createWebGLRendererForPost({ fresh: true });
  const gl = h.canvas.getContext("webgl2"); gl.deleteRenderbuffer = () => {};
  try {
    const bundle = makeWebGLBundleWithCustomPost();
    bundle.postEffects = [{ kind: "ssao" }, { kind: "toneMapping" }, { kind: "fxaa" }];
    h.renderer.render(bundle, { width: 320, height: 180 });
    const targets = new Set(), drawn = new Set(); let framebuffer;
    for (const op of gl.ops) {
      if (op[0] === "bindFramebuffer") framebuffer = op[2];
      if (op[0] === "framebufferTexture2D" && op[2] === gl.COLOR_ATTACHMENT0) targets.add(framebuffer);
      if (op[0] === "drawArrays" || op[0] === "drawElements") drawn.add(framebuffer);
    }
    assert.equal(targets.size, 2, "scene and auxiliary targets suffice");
    assert.ok([...targets].every(t => drawn.has(t)), "no unused full-size scratch allocation");
  } finally { h.renderer.dispose(); }
});


test("WebGPU 4x MSAA resolves covered depth before AO samples it", async () => {
  const { createBoardWebGPUHarness, makePointsBundle } = require("./runtime-test-harness.js");
  const h = await createBoardWebGPUHarness({ fresh: true });
  h.renderer.dispose();
  const renderer = h.env.context.__gosx_scene3d_webgpu_api.createRenderer(h.canvas, { msaaSamples: 4 });
  try {
    const bundle = makePointsBundle({ id: "p", count: 1, positions: [0, 0, 0] });
    bundle.postEffects = [{ kind: "ssao" }, { kind: "toneMapping" }, { kind: "fxaa" }];
    renderer.render(bundle, { width: 64, height: 64 });
    const passes = h.fake.state.renderPasses;
    const resolve = passes.find(p => p.descriptor.label === "gosx-post-depth-resolve");
    assert.ok(resolve, "multisampled depth is resolved each frame");
    const source = resolve.bindGroups[0].group.desc.entries[0].resource;
    const target = resolve.descriptor.depthStencilAttachment.view;
    assert.notEqual(source, target);
    const main = passes.find(p => p.descriptor.depthStencilAttachment?.view === source);
    assert.equal(main.descriptor.depthStencilAttachment.depthStoreOp, "store");
    const ao = passes.find(p => p.pipelines.some(pipeline => pipeline.desc.fragment?.module?.label === "post-ssao"));
    assert.ok(ao);
    assert.ok(passes.indexOf(main) < passes.indexOf(resolve) && passes.indexOf(resolve) < passes.indexOf(ao));
    assert.equal(ao.bindGroups[0].group.desc.entries.find(e => e.binding === 2).resource, target);
    assert.match(resolve.pipelines[0].desc.fragment.module.code, /sample < 4/);
  } finally { renderer.dispose(); }
});
