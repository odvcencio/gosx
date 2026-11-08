"use strict";

const test = require("node:test");
const assert = require("node:assert/strict");
const {
  createWebGLRendererForPost,
  makeWebGLBundleWithCustomPost,
  countDefaultFramebufferDraws,
  FakeWebGLContext,
  createContext,
  runScript,
  bootstrapRuntimeSource,
  freshFeatureBundleSource,
} = require("./runtime-test-harness.js");

const viewport = { cssWidth: 320, cssHeight: 180, pixelWidth: 320, pixelHeight: 180, pixelRatio: 1 };
const vertex = "attribute vec2 a_position; attribute vec2 a_uv; varying vec2 v_uv; void main() { v_uv = a_uv; gl_Position = vec4(a_position, 0.0, 1.0); }";
const fragment = "#extension GL_EXT_shader_texture_lod : enable\nprecision mediump float; varying vec2 v_uv; uniform sampler2D _sceneColor; uniform float u_lodMarker; void main() { gl_FragColor = texture2DLodEXT(_sceneColor, v_uv, 0.0) + texture2D(_sceneColor, v_uv) * u_lodMarker; }";

function renderPair(vertexGLSL, fragmentGLSL, domRegions) {
  // Model the actual WebGL2 driver's rejection of the ES 1.00 extension.
  const harness = createWebGLRendererForPost({ fresh: true, rejectShaderSources: ["texture2DLodEXT", "GL_EXT_shader_texture_lod"] });
  harness.renderer.render(makeWebGLBundleWithCustomPost({ vertexGLSL, fragmentGLSL, domRegions }), viewport);
  const gl = harness.canvas.getContext("webgl2");
  const program = gl.programs.find(p => p.attached.some(s => s.source.includes("u_lodMarker")));
  return { ...harness, gl, program };
}

function shaderWarnings(harness) {
  // The fake deliberately lacks HDR float targets; that existing intermediate
  // format warning says nothing about whether the authored shader linked.
  return harness.warnLog.filter(w => !w.includes("EXT_color_buffer_float is unavailable"));
}

test("custom post WebGL2: explicit LOD uses core GLSL without identity fallback or a second screen draw", () => {
  const h = renderPair(vertex, fragment);
  try {
    assert.deepEqual(shaderWarnings(h), [], "a rejected LOD extension must not demote the authored pass to identity");
    assert.ok(h.program, "the authored LOD program must link");
    const vs = h.program.attached.find(s => s.type === h.gl.VERTEX_SHADER).source;
    const fs = h.program.attached.find(s => s.type === h.gl.FRAGMENT_SHADER).source;
    assert.match(vs, /^#version 300 es\n/);
    assert.match(fs, /^#version 300 es\n/);
    assert.match(vs, /in vec2 a_position; in vec2 a_uv; out vec2 v_uv;/);
    assert.match(vs, /v_uv = a_uv/);
    assert.match(fs, /textureLod\(_sceneColor, v_uv, 0\.0\)/);
    assert.match(fs, /texture\(_sceneColor, v_uv\)/);
    assert.match(fs, /out vec4 _gosxPostFragColor;/);
    assert.equal(countDefaultFramebufferDraws(h.gl), 1, "the LOD pass must own the visible image");
    h.renderer.render(makeWebGLBundleWithCustomPost({ vertexGLSL: vertex, fragmentGLSL: fragment }), viewport);
    assert.equal(h.gl.programs.filter(p => p.attached.some(s => s.source.includes("u_lodMarker"))).length, 1, "the converted program must remain cached");
  } finally { h.renderer.dispose(); }
});

test("custom post WebGL2: promotion preserves an authored output-name collision", () => {
  const h = renderPair(vertex, fragment.replace("uniform float u_lodMarker;", "uniform float u_lodMarker; float _gosxPostFragColor = 0.0;"));
  try {
    assert.deepEqual(shaderWarnings(h), []);
    assert.ok(h.program);
    const fs = h.program.attached.find(s => s.type === h.gl.FRAGMENT_SHADER).source;
    assert.match(fs, /float _gosxPostFragColor = 0\.0;/);
    assert.match(fs, /out vec4 _gosxPostFragColor_;/);
    assert.match(fs, /_gosxPostFragColor_ = textureLod/);
  } finally { h.renderer.dispose(); }
});

test("custom post WebGL2: generated output and sample names preserve authored LOD suffix collisions", () => {
  const legacy = "attribute vec2 a_position; varying vec2 v_uv; void main() { v_uv = a_position * 0.5 + 0.5; gl_Position = vec4(a_position, 0.0, 1.0); }";
  const h = renderPair(legacy, fragment.replace("uniform float u_lodMarker;",
    "uniform float u_lodMarker; float _gosxPostFragColorLod = 0.0; float _gosxPostTextureLod = 0.0;"), { selector: ".card", max: 1 });
  try {
    assert.deepEqual(shaderWarnings(h), []);
    const fs = h.program.attached.find(s => s.type === h.gl.FRAGMENT_SHADER).source;
    assert.match(fs, /float _gosxPostFragColorLod = 0\.0;/);
    assert.match(fs, /float _gosxPostTextureLod = 0\.0;/);
    assert.match(fs, /out vec4 _gosxPostFragColor_;/);
    assert.match(fs, /vec4 _gosxPostTexture_Lod\(sampler2D/);
    assert.match(fs, /_gosxPostTexture_Lod\(_sceneColor, v_uv, 0.0\)/);
  } finally { h.renderer.dispose(); }
});

test("custom post WebGL2: legacy Selena DOM regions align to top-left UVs without flipping clip geometry or scene samples", () => {
  const legacy = "attribute vec2 a_position; varying vec2 v_uv; void main() { v_uv = a_position * 0.5 + 0.5; gl_Position = vec4(a_position, 0.0, 1.0); }";
  const h = renderPair(legacy, fragment, { selector: ".card", max: 1 });
  try {
    assert.deepEqual(shaderWarnings(h), []);
    assert.ok(h.program);
    const vs = h.program.attached.find(s => s.type === h.gl.VERTEX_SHADER).source;
    const fs = h.program.attached.find(s => s.type === h.gl.FRAGMENT_SHADER).source;
    assert.match(vs, /v_uv = vec2\(a_position.x \* 0.5 \+ 0.5, 0.5 - a_position.y \* 0.5\)/);
    assert.match(vs, /gl_Position = vec4\(a_position, 0.0, 1.0\)/);
    assert.match(fs, /textureLod\(tex, vec2\(uv.x, 1.0 - uv.y\), level\)/);
    assert.match(fs, /_gosxPostTextureLod\(_sceneColor, v_uv, 0.0\)/);
    assert.match(fs, /_gosxPostTexture\(_sceneColor, v_uv\)/);
  } finally { h.renderer.dispose(); }
  const plain = renderPair(legacy, fragment);
  try {
    const vs = plain.program.attached.find(s => s.type === plain.gl.VERTEX_SHADER).source;
    assert.match(vs, /v_uv = a_position \* 0.5 \+ 0.5/);
  } finally { plain.renderer.dispose(); }
});

test("custom post WebGL2: native ES 3.00 and legacy non-LOD payloads keep their source contract", () => {
  for (const [vs, fs] of [
    ["#version 300 es\nprecision highp float; in vec2 a_position; in vec2 a_uv; out vec2 v_uv; void main() { v_uv=a_uv; gl_Position=vec4(a_position,0.0,1.0); }", "#version 300 es\nprecision highp float; in vec2 v_uv; uniform sampler2D _sceneColor; uniform float u_lodMarker; out vec4 color; void main() { color=textureLod(_sceneColor,v_uv,0.0); }"],
    [vertex, fragment.replace("#extension GL_EXT_shader_texture_lod : enable\n", "").replace("texture2DLodEXT(_sceneColor, v_uv, 0.0)", "texture2D(_sceneColor, v_uv)")],
  ]) {
    const h = renderPair(vs, fs);
    try {
      assert.deepEqual(shaderWarnings(h), []);
      assert.ok(h.program);
      const actualVS = h.program.attached.find(s => s.type === h.gl.VERTEX_SHADER).source;
      const actualFS = h.program.attached.find(s => s.type === h.gl.FRAGMENT_SHADER).source;
      if (vs.startsWith("#version")) {
        assert.equal(actualVS, vs);
        assert.equal(actualFS, fs);
      } else {
        assert.match(actualVS, /attribute vec2 a_position/);
        assert.match(actualFS, /gl_FragColor = texture2D/);
        assert.doesNotMatch(actualFS, /#version 300 es/);
      }
    } finally { h.renderer.dispose(); }
  }
});

test("shared synchronous compilation: vertex, fragment and link failures release each owned resource once", () => {
  for (const stage of ["vertex", "fragment", "link"]) {
    const h = createWebGLRendererForPost({ fresh: true });
    const gl = h.canvas.getContext("webgl2");
    const deletedShaders = new Set(), deletedPrograms = new Set();
    const shaderStatus = gl.getShaderParameter.bind(gl), programStatus = gl.getProgramParameter.bind(gl);
    const deleteShader = gl.deleteShader.bind(gl), deleteProgram = gl.deleteProgram.bind(gl);
    gl.getShaderParameter = (shader, param) => {
      const rejected = shader.source.includes("u_failureMarker") &&
        shader.type === (stage === "vertex" ? gl.VERTEX_SHADER : stage === "fragment" ? gl.FRAGMENT_SHADER : -1);
      return rejected ? false : shaderStatus(shader, param);
    };
    gl.getProgramParameter = (program, param) => stage === "link" &&
      program.attached.some(shader => shader.source.includes("u_failureMarker")) ? false : programStatus(program, param);
    gl.deleteShader = shader => {
      assert.ok(!deletedShaders.has(shader), stage + " must not delete a shader twice");
      deletedShaders.add(shader); deleteShader(shader);
    };
    gl.deleteProgram = program => {
      assert.ok(!deletedPrograms.has(program), stage + " must not delete a program twice");
      deletedPrograms.add(program); deleteProgram(program);
    };
    try {
      h.renderer.render(makeWebGLBundleWithCustomPost({
        vertexGLSL: "float u_failureMarker = 0.0; " + vertex,
        fragmentGLSL: fragment.replace("uniform float u_lodMarker;", "uniform float u_lodMarker; uniform float u_failureMarker;"),
      }), viewport);
      const shaders = [...deletedShaders].filter(shader => shader.source.includes("u_failureMarker"));
      assert.equal(shaders.length, stage === "vertex" ? 1 : 2, stage + " cleanup");
      assert.equal([...deletedPrograms].filter(program => program.attached.some(shader => shader.source.includes("u_failureMarker"))).length,
        stage === "link" ? 1 : 0, stage + " program cleanup");
      assert.equal(countDefaultFramebufferDraws(gl), 1, "failure must retain the existing single identity fallback");
      assert(shaderWarnings(h).some(warning => warning.includes(stage === "link" ? "program link failed" : "shader compile failed")),
        "the original failure diagnostic must remain visible");
    } finally { h.renderer.dispose(); }
  }
});

test("shared synchronous compilation: a failed required water link cleans shaders and aborts initialization", () => {
  const env = createContext({ enableWebGL2: true, disableCanvas2D: true });
  env.context.WebGL2RenderingContext = FakeWebGLContext;
  runScript(bootstrapRuntimeSource, env.context, "bootstrap-runtime.js");
  runScript(freshFeatureBundleSource("scene3d"), env.context, "bootstrap-feature-scene3d.js");
  runScript(freshFeatureBundleSource("scene3d-webgl", { exportWaterRendererForTest: true }), env.context, "bootstrap-feature-scene3d-webgl.js");
  const gl = new FakeWebGLContext(), canvas = env.document.createElement("canvas");
  gl.HALF_FLOAT = 0x140B; gl.FRAMEBUFFER_COMPLETE = 0x8CD5;
  gl.checkFramebufferStatus = () => gl.FRAMEBUFFER_COMPLETE;
  gl.getExtension = name => /EXT_color_buffer|OES_texture_float_linear/.test(name) ? {} : null;
  const status = gl.getProgramParameter.bind(gl), destroy = gl.deleteShader.bind(gl), deleted = new Set();
  gl.getProgramParameter = (program, param) => program.attached.some(shader => shader.source.includes("water_failure_marker")) ? false : status(program, param);
  gl.deleteShader = shader => {
    assert.ok(!deleted.has(shader), "failed water links must not delete shaders again after shared cleanup");
    deleted.add(shader); destroy(shader);
  };
  const shader = "void main() {}", rejected = "float water_failure_marker; " + shader;
  const renderer = env.context.__gosx_test_create_water_webgl(gl, canvas, {
    id: "failed-water", resolution: 16, seedDrops: 0,
    simulationVertexGLES: rejected, simulationFragmentGLES: rejected,
    normalVertexGLES: shader, normalFragmentGLES: shader,
  });
  assert.equal(renderer, null, "missing required simulation must fail closed");
  assert.equal([...deleted].filter(shader => shader.source.includes("water_failure_marker")).length, 2);
  assert(env.consoleLogs.warn.some(warning => warning.includes("Water sim (simulation) program link failed")));
});

test("shared disposal: a cached successful custom post releases its program and both shaders once", () => {
  const h = renderPair(vertex, fragment);
  h.renderer.render(makeWebGLBundleWithCustomPost({ vertexGLSL: vertex, fragmentGLSL: fragment }), viewport);
  const shaders = [], programs = [], deleteShader = h.gl.deleteShader.bind(h.gl), deleteProgram = h.gl.deleteProgram.bind(h.gl);
  h.gl.deleteShader = shader => { shaders.push(shader); deleteShader(shader); };
  h.gl.deleteProgram = program => { programs.push(program); deleteProgram(program); };
  h.renderer.dispose();
  assert.equal(programs.filter(program => program === h.program).length, 1);
  for (const shader of h.program.attached) {
    assert.equal(shaders.filter(deleted => deleted === shader).length, 1);
  }
});
