"use strict";
const test = require("node:test");
const assert = require("node:assert/strict");
const { createWebGLRendererForPost, makeWebGLBundleWithCustomPost, createBoardWebGPUHarness, makePointsBundle } = require("./runtime-test-harness.js");

const parameterClasses = {
  toneMapping: { mode: ["aces", "reinhard"], exposure: [1, 1.7] },
  colorGrade: { contrast: [1, 1.7], saturation: [1, 0.3], exposure: [1, 1.7] },
  bloom: { threshold: [0.8, 1.7], scale: [0.5, 0.25], radius: [5, 9], intensity: [0.5, 0.7] },
  dof: { focusDistance: [8, 12], aperture: [0.04, 0.07], maxBlur: [8, 12] },
  ssao: { bias: [0.01, 0.07], radius: [4, 7], intensity: [0.55, 0.7] },
  contactShadows: { distance: [1, 1.7], thickness: [0.1, 0.07], bias: [0.01, 0.07], intensity: [0.5, 0.7], direction: [{ x: 0, y: -1, z: 0 }, { x: 1, y: -1, z: 0 }] },
  vignette: { intensity: [1, 0.7] },
  taa: { historyWeight: [0.9, 0.7], clampGamma: [1.25, 1.7], depthThreshold: [0.01, 0.07] },
};

function temporalHarness(options = {}) {
  const h = createWebGLRendererForPost({ fresh: true, ...options });
  const gl = h.canvas.getContext("webgl2");
  gl.TEXTURE1 = gl.TEXTURE0 + 1; gl.FRAMEBUFFER_COMPLETE = 0x8cd5;
  gl.checkFramebufferStatus = () => gl.FRAMEBUFFER_COMPLETE; gl.deleteRenderbuffer = () => {};
  gl.uniform4fv = (loc, values) => gl.uniform4f(loc, ...values);
  if (options.float !== false) {
    const ext = gl.getExtension.bind(gl);
    gl.getExtension = name => name === "EXT_color_buffer_float" ? {} : ext(name);
  }
  const params = [], projections = [], jitters = [], sceneProjections = [];
  const uniform4f = gl.uniform4f.bind(gl), uniformMatrix = gl.uniformMatrix4fv.bind(gl);
  gl.uniform4f = (loc, ...v) => {
    if (loc.name === "u_temporalParams") params.push(v);
    if (loc.name === "u_temporalJitter") jitters.push(v);
    uniform4f(loc, ...v);
  };
  gl.uniformMatrix4fv = (loc, transpose, v) => { if (loc.name === "u_projectionMatrix") sceneProjections.push(Array.from(v)); if (loc.name === "u_projection") projections.push(Array.from(v)); uniformMatrix(loc, transpose, v); };
  const mount = h.env.document.createElement("div"); mount.appendChild(h.canvas);
  const bundle = makeWebGLBundleWithCustomPost();
  bundle.postEffects = [{ kind: "toneMapping" }, { kind: "taa", historyWeight: 0.9, clampGamma: 1.25, depthThreshold: 0.01 }];
  return { ...h, gl, params, projections, jitters, sceneProjections, mount, bundle, frame: () => h.renderer.render(bundle, { width: h.canvas.width, height: h.canvas.height }) };
}

test("TAA uploads current and previous jitter in history UV units", () => {
  const h = temporalHarness();
  try {
    h.frame(); h.frame(); h.frame();
    assert.equal(h.jitters.length, 3);
    assert.deepEqual(h.jitters[0].slice(2), [0, 0]);
    for (let frame = 0; frame < 3; frame++) {
      assert.ok(Math.abs(h.jitters[frame][0] + h.projections[frame][8] * 0.5) < 1e-8);
      assert.ok(Math.abs(h.jitters[frame][1] + h.projections[frame][9] * 0.5) < 1e-8);
      if (frame) assert.deepEqual(h.jitters[frame].slice(2), h.jitters[frame - 1].slice(0, 2));
    }
  } finally { h.renderer.dispose(); }
});

test("TAA jitters the rendered projection and reuses only valid color and depth history", () => {
  const h = temporalHarness();
  h.frame(); h.frame();
  assert.equal(h.mount.getAttribute("data-gosx-scene3d-antialiasing"), "taa");
  assert.deepEqual(h.params.map(p => p[3]), [0, 1]);
  assert.notEqual(h.projections[0][8], h.projections[1][8], "Halton samples move the actual render projection");
  assert.equal(h.gl.ops.filter(op => op[0] === "blitFramebuffer" && op[1] === h.gl.DEPTH_BUFFER_BIT).length, 2);
  h.bundle.camera.x += 10; h.frame();
  assert.equal(h.params.at(-1)[3], 0, "camera cut rejects history");
  h.frame(); assert.equal(h.params.at(-1)[3], 1);
  h.bundle.postEffects[0].exposure = 2; h.frame();
  assert.equal(h.params.at(-1)[3], 0, "effect change rejects history");
  h.canvas.width = 640; h.canvas.height = 360; h.frame();
  assert.equal(h.params.at(-1)[3], 0, "resize rejects history");
  h.bundle.postEffects = []; h.frame();
  h.bundle.postEffects = [{ kind: "taa" }]; h.frame();
  assert.equal(h.params.at(-1)[3], 0, "quality suppression discards history");
  h.renderer.dispose();
});

test("TAA falls back to FXAA without jitter or history if float targets or shader support are absent", () => {
  for (const options of [{ float: false }, { rejectShaderSources: ["u_temporalParams"] }]) {
    const h = temporalHarness(options); h.frame();
    assert.equal(h.mount.getAttribute("data-gosx-scene3d-antialiasing"), "fxaa");
    assert.deepEqual(h.params, []);
    assert.equal(h.gl.ops.filter(op => op[0] === "blitFramebuffer").length, 0);
    h.renderer.dispose();
  }
});

test("WebGPU accepts a TAA quality rung and dispatches FXAA as its supported fallback", async () => {
  const h = await createBoardWebGPUHarness({ fresh: true });
  const bundle = makePointsBundle({ id: "p", count: 1, positions: [0, 0, 0] });
  bundle.postEffects = [{ kind: "taa" }];
  h.renderer.render(bundle, { width: 320, height: 180 });
  const fxaa = h.fake.state.shaderModules.find(m => m.label === "post-fxaa");
  assert.ok(fxaa);
  assert.ok(h.fake.state.renderPasses.flatMap(p => p.draws).some(d => d.pipeline?.desc?.fragment?.module === fxaa));
  h.renderer.dispose();
});

test("disabling TAA keeps FXAA without temporal allocations, reprojection uploads, or history copies", () => {
  const h = temporalHarness();
  h.frame(); h.frame();
  h.bundle.postEffects = [{ kind: "toneMapping" }, { kind: "fxaa" }];
  h.frame();
  const start = h.gl.ops.length, params = h.params.length;
  const extension = h.gl.getExtension;
  let temporalChecks = 0;
  h.gl.getExtension = name => { if (name === "EXT_color_buffer_float") temporalChecks++; return extension(name); };
  h.frame(); h.frame();
  assert.equal(h.mount.getAttribute("data-gosx-scene3d-antialiasing"), "fxaa");
  assert.equal(h.params.length, params);
  assert.equal(temporalChecks, 0);
  assert.equal(h.gl.ops.slice(start).some(op => ["createTexture", "createFramebuffer", "blitFramebuffer"].includes(op[0])), false);
  h.renderer.dispose();
});

for (const [kind, parameters] of Object.entries(parameterClasses)) {
  test(`TAA resets history for every ${kind} parameter`, () => {
    const h = temporalHarness();
    try {
      const effect = { kind };
      for (const [name, [initial]] of Object.entries(parameters)) effect[name] = initial;
      h.bundle.postEffects = kind === "taa" ? [effect] : [effect, { kind: "taa" }];
      h.frame(); h.frame();
      assert.deepEqual(h.params.map(p => p[3]), [0, 1]);
      for (const [name, [, changed]] of Object.entries(parameters)) {
        effect[name] = changed; h.frame();
        assert.equal(h.params.at(-1)[3], 0, `${kind}.${name} change rejects history`);
        assert.equal(h.mount.getAttribute("data-gosx-scene3d-antialiasing"), "fxaa");
        assert.deepEqual(h.sceneProjections.at(-1).slice(8, 10), [0, 0]);
        h.frame(); assert.equal(h.params.at(-1)[3], 1, `${kind}.${name} settles on the next frame`);
      }
      h.bundle.postEffects = JSON.parse(JSON.stringify(h.bundle.postEffects)); h.frame();
      assert.equal(h.params.at(-1)[3], 1, "equivalent descriptors retain history");
    } finally { h.renderer.dispose(); }
  });
}

test("TAA tracks nested future parameters, chain changes, and contact light inputs", () => {
  const h = temporalHarness();
  try {
    const grade = { kind: "colorGrade", future: { curve: [0, 1] } };
    h.bundle.postEffects = [grade, { kind: "contactShadows" }, { kind: "taa" }, { kind: "vignette", intensity: 1 }];
    h.bundle.lights = [{ kind: "directional", directionX: 0, directionY: -1, directionZ: 0 }];
    h.frame(); h.frame();
    grade.future.curve[1] = 2; h.frame();
    assert.equal(h.params.at(-1)[3], 0, "nested parameters are compared by value");
    h.frame(); h.bundle.lights[0].directionX = 1; h.frame();
    assert.equal(h.params.at(-1)[3], 0, "implicit contact shadow sunlight invalidates history");
    h.frame(); h.bundle.postEffects.at(-1).intensity = 0.5; h.frame();
    assert.equal(h.params.at(-1)[3], 1, "downstream color never enters TAA history");
    [h.bundle.postEffects[0], h.bundle.postEffects[1]] = [h.bundle.postEffects[1], h.bundle.postEffects[0]];
    h.frame(); assert.equal(h.params.at(-1)[3], 0, "upstream order invalidates history");
    h.frame(); h.bundle.postEffects.splice(0, 1); h.frame();
    assert.equal(h.params.at(-1)[3], 0, "removing an upstream pass invalidates history");
    h.frame(); h.bundle.postEffects.unshift({ kind: "bloom" }); h.frame();
    assert.equal(h.params.at(-1)[3], 0, "adding an upstream pass invalidates history");
  } finally { h.renderer.dispose(); }
});

test("untracked upstream custom post uses FXAA without jitter and can resume TAA", () => {
  const h = temporalHarness();
  try {
    h.bundle.postEffects = [makeWebGLBundleWithCustomPost().postEffects[0], { kind: "taa" }];
    assert.equal(h.bundle.postEffects[0].kind, "customPost");
    h.frame(); h.frame(); h.frame();
    assert.equal(h.mount.getAttribute("data-gosx-scene3d-antialiasing"), "fxaa");
    assert.deepEqual(h.params, [], "untrackable passes allocate no temporal history");
    assert.ok(h.gl.programMatching("greenLuma"), "the fallback dispatches FXAA");
    assert.ok(h.sceneProjections.every(p => p[8] === 0 && p[9] === 0), "rendered projection is unjittered");
    h.bundle.postEffects = [{ kind: "taa" }]; h.frame(); h.frame();
    assert.equal(h.mount.getAttribute("data-gosx-scene3d-antialiasing"), "taa");
    assert.deepEqual(h.params.map(p => p[3]), [0, 1]);
  } finally { h.renderer.dispose(); }
});

test("contrast, saturation, and exposure edits reset the review reproduction", t => {
  const h = temporalHarness();
  try {
    const grade = { kind: "colorGrade", contrast: 1, saturation: 1, exposure: 1 };
    h.bundle.postEffects = [grade, { kind: "taa" }];
    h.frame(); h.frame();
    for (const parameter of ["contrast", "saturation", "exposure"]) { grade[parameter] = 1.5; h.frame(); }
    const validity = h.params.map(p => p[3]);
    t.diagnostic(`review history validity: ${JSON.stringify(validity)}`);
    assert.deepEqual(validity, [0, 1, 0, 0, 0]);
  } finally { h.renderer.dispose(); }
});

test("WebGPU TAA fallback uploads changed upstream parameters on the next frame", async () => {
  const h = await createBoardWebGPUHarness({ fresh: true });
  try {
    const bundle = makePointsBundle({ id: "p", count: 1, positions: [0, 0, 0] });
    for (const [kind, parameters] of Object.entries(parameterClasses)) {
      if (kind === "taa") continue; // WebGPU has no temporal implementation.
      const effect = { kind };
      for (const [name, [initial]] of Object.entries(parameters)) effect[name] = initial;
      bundle.postEffects = [effect, { kind: "taa" }];
      h.renderer.render(bundle, { width: 320, height: 180 });
      for (const [name, [, changed]] of Object.entries(parameters)) {
        const previous = new Map(h.fake.state.writeBufferCalls.map(w => [w.buffer, Array.from(w.data || [])]));
        const textures = h.fake.state.textures.length;
        effect[name] = changed;
        const start = h.fake.state.writeBufferCalls.length;
        h.renderer.render(bundle, { width: 320, height: 180 });
        if (name === "scale") {
          assert.ok(h.fake.state.textures.length > textures, "bloom scale rebuilds its targets");
        } else {
          assert.ok(h.fake.state.writeBufferCalls.slice(start).some(w =>
            previous.has(w.buffer) && JSON.stringify(previous.get(w.buffer)) !== JSON.stringify(Array.from(w.data || []))),
          `${kind}.${name} reaches a changed uniform upload`);
        }
      }
    }
    assert.equal(h.fake.state.shaderModules.some(m => m.label === "post-taa"), false);
  } finally { h.renderer.dispose(); }
});

test("software TAA pixels stabilize jittered foreground/clear-depth silhouettes and reject disocclusion", t => {
  const { spawnSync } = require("node:child_process");
  const path = require("node:path");
  const python = require("node:fs").existsSync("/usr/bin/python3") ? "/usr/bin/python3" : "python3";
  const h = temporalHarness();
  try {
    h.frame();
    const program = h.gl.programMatching("u_temporalParams");
    const fragment = program.attached.find(shader => shader.type === h.gl.FRAGMENT_SHADER).source;
    const run = spawnSync(python, [path.join(__dirname, "testdata/scene3d-taa-pixels.py")], {
      input: JSON.stringify({ taa: fragment, fxaa: h.gl.programMatching("greenLuma").attached.find(shader => shader.type === h.gl.FRAGMENT_SHADER).source }), encoding: "utf8", timeout: 30000,
      env: { ...process.env, LIBGL_ALWAYS_SOFTWARE: "1", GALLIUM_DRIVER: "llvmpipe" },
    });
    if (run.error?.code === "ENOENT") return t.skip("Python is unavailable for the optional CPU raster test");
    assert.equal(run.status, 0, run.stderr || String(run.error));
    const pixels = JSON.parse(run.stdout);
    if (pixels.skip) return t.skip(pixels.skip);
    t.diagnostic(JSON.stringify(pixels));
  } finally { h.renderer.dispose(); }
});

test("every-frame parameter invalidation renders FXAA with a stable projection", () => {
  const h = temporalHarness();
  try {
    for (let i = 0; i < 8; i++) {
      h.bundle.postEffects[0].exposure = 1 + i * 0.1; h.frame();
      assert.equal(h.mount.getAttribute("data-gosx-scene3d-antialiasing"), "fxaa");
      assert.deepEqual(h.sceneProjections.at(-1).slice(8, 10), [0, 0]);
    }
    assert.ok(h.gl.programMatching("greenLuma"));
    h.frame(); assert.equal(h.mount.getAttribute("data-gosx-scene3d-antialiasing"), "taa");
  } finally { h.renderer.dispose(); }
});

for (const invalidation of ["camera cut", "projection", "resize", "frame gap"]) {
  test(`repeated ${invalidation} invalidation falls back before jitter`, () => {
    const h = temporalHarness();
    try {
      let time = 0; h.env.context.performance.now = () => time;
      h.frame(); h.frame();
      for (let i = 0; i < 4; i++) {
        if (invalidation === "camera cut") h.bundle.camera.x += 3;
        if (invalidation === "projection") h.bundle.camera.fov = 50 + i * 5;
        if (invalidation === "resize") h.canvas.width += 10;
        if (invalidation === "frame gap") time += 300;
        h.frame();
        assert.equal(h.mount.getAttribute("data-gosx-scene3d-antialiasing"), "fxaa");
        assert.deepEqual(h.sceneProjections.at(-1).slice(8, 10), [0, 0]);
      }
    } finally { h.renderer.dispose(); }
  });
}
