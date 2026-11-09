"use strict";
// Props.shaderWarmup: the mount hands the renderer a synthetic bundle of the
// declared materials and variants until a frame submits with all of them
// linked, and publishes progress on the mount for loading screens.
const test = require("node:test");
const assert = require("node:assert/strict");
const H = require("./runtime-test-harness.js");

const COMPLETION_STATUS_KHR = 0x91B1;

// A WebGL2 context with KHR_parallel_shader_compile. Programs built from the
// warm-up probe shader stay incomplete until the test sets complete, like a
// driver still compiling in the background; the base programs finish at once.
class ParallelCompileGL extends H.FakeWebGLContext {
  constructor(options) {
    super(options);
    this.sources = [];
    this.complete = false;
  }
  shaderSource(shader, source) {
    super.shaderSource(shader, source);
    this.sources.push(source);
  }
  getExtension(name) {
    if (name === "KHR_parallel_shader_compile") return { COMPLETION_STATUS_KHR };
    return super.getExtension(name);
  }
  getProgramParameter(program, param) {
    if (param === COMPLETION_STATUS_KHR) {
      return this.complete || !(program.attached || []).some(shader => String(shader.source || "").includes("warm-up-probe"));
    }
    return super.getProgramParameter(program, param);
  }
}

const WARM_FRAGMENT = "#version 300 es\nprecision highp float;\nout vec4 fragColor;\nvoid main() { fragColor = vec4(0.25, 0.5, 0.75, 1.0); } // warm-up-probe";
const WARM_VERTEX = "#version 300 es\nin vec3 position;\nuniform mat4 mvp;\nvoid main() { gl_Position = mvp * vec4(position, 1.0); }";

function selenaRecord() {
  return {
    materialKind: "custom",
    shaderBackend: "selena",
    customVertex: WARM_VERTEX,
    customFragment: WARM_FRAGMENT,
    customVertexWGSL: "@vertex fn vertexMain() -> @builtin(position) vec4f { return vec4f(0.0); }",
    customFragmentWGSL: "@fragment fn fragmentMain() -> @location(0) vec4f { return vec4f(1.0); }",
    shaderLayout: {
      schemaVersion: "selena.descriptor.v1",
      languageVersion: "selena.lang.v1",
      material: "WarmProbe",
      kind: "mesh",
      entryPoints: { vertex: "vertexMain", fragment: "fragmentMain" },
      attributes: [{ location: 0, name: "position", type: "vec3" }],
      textures: [],
      uniformBlock: { size: 64, fields: [{ name: "mvp", type: "mat4", offset: 0, size: 64 }], defaults: [] },
      wgsl: { group: 0, binding: 0 },
      metal: { buffer: 0 },
    },
  };
}

async function mountScene(props, gl) {
  let now = 1000;
  const mount = new H.FakeElement("div", null);
  mount.id = "warmup-scene";
  const env = H.createContext({ elements: [mount], enableWebGL2: true, disableCanvas2D: true,
    performanceNow: () => now,
    manifest: { engines: [{ id: "warmup-engine", component: "GoSXScene3D", kind: "surface", mountId: mount.id,
      props: Object.assign({ width: 320, height: 180, adaptiveQuality: false, forceWebGL: true,
        scene: { objects: [{ kind: "box", id: "seed" }] } }, props) }] } });
  env.context.WebGL2RenderingContext = H.FakeWebGLContext;
  if (typeof env.context.Event !== "function") env.context.Event = env.context.CustomEvent;
  env.document.createWebGL2Context = () => gl;
  const events = [];
  mount.addEventListener("gosx:scene3d:shader-progress", event => events.push(event.detail));
  const raf = H.installManualRAF(env.context);
  H.runScript(H.bootstrapSource, env.context, "bootstrap.js");
  for (let i = 0; i < 12; i++) await H.flushAsyncWork();
  // A browser context names its canvas, and the renderer's program-ready
  // event bubbles from it to the mount. The fake DOM does not bubble, so the
  // canvas forwards its events to the mount here.
  const canvas = mount.children[0];
  const ownDispatch = canvas.dispatchEvent.bind(canvas);
  canvas.dispatchEvent = event => { ownDispatch(event); return mount.dispatchEvent(event); };
  gl.canvas = canvas;
  const tick = async () => { now += 17; raf.flush(now); await H.flushAsyncWork(); };
  await tick();
  return { mount, env, events, tick };
}

const warmup = () => ({ shaderWarmup: { materials: [
  { material: selenaRecord() },
  { material: selenaRecord(), skinned: true, doubleSided: true },
  { material: { materialKind: "standard", color: "#ff3366", opacity: 0.5 } },
] } });

test("declared Selena and standard variants compile before the first frame and report progress", async () => {
  const gl = new ParallelCompileGL({});
  const s = await mountScene(warmup(), gl);
  for (let i = 0; i < 3; i++) await s.tick();
  assert.equal(s.mount.getAttribute("data-gosx-scene3d-shaders"), "compiling");
  const total = Number(s.mount.getAttribute("data-gosx-scene3d-shaders-total"));
  assert.ok(total >= 3, "the warm-up must submit its programs on the first frame, total=" + total);
  const done = Number(s.mount.getAttribute("data-gosx-scene3d-shaders-done"));
  assert.equal(total - done, 2, "the static and skinned Selena programs are still linking");
  // The warm-up Selena fragment was compiled although no object uses it yet,
  // once for the static variant and once for the skinned variant.
  assert.equal(gl.sources.filter(source => source.includes("warm-up-probe")).length, 2);
  // The frame waits for every program, so nothing has been drawn.
  assert.equal(gl.ops.filter(op => op[0] === "drawArrays" || op[0] === "drawElements").length, 0);

  gl.complete = true;
  for (let i = 0; i < 4; i++) await s.tick();

  assert.equal(s.mount.getAttribute("data-gosx-scene3d-shaders"), "ready");
  assert.equal(s.mount.getAttribute("data-gosx-scene3d-shaders-done"), s.mount.getAttribute("data-gosx-scene3d-shaders-total"));
  const last = s.events.at(-1);
  assert.equal(last.ready, true);
  assert.equal(last.backend, "webgl");
  assert.equal(last.done, last.total);
  assert.ok(s.events.some(detail => !detail.ready && detail.done < detail.total), "progress must be reported while compiling");
});

test("after warm-up, frames no longer carry the warm-up bundle", async () => {
  const gl = new ParallelCompileGL({});
  gl.complete = true;
  const s = await mountScene(warmup(), gl);
  for (let i = 0; i < 3; i++) await s.tick();
  assert.equal(s.mount.getAttribute("data-gosx-scene3d-shaders"), "ready");
  const compiled = gl.sources.length;
  const events = s.events.length;
  for (let i = 0; i < 5; i++) await s.tick();
  assert.equal(gl.sources.length, compiled, "a ready warm-up must not compile again");
  assert.equal(s.events.length, events, "a ready warm-up must not publish again");
});

test("a scene without shaderWarmup publishes no shader progress", async () => {
  const gl = new ParallelCompileGL({});
  gl.complete = true;
  const s = await mountScene({}, gl);
  for (let i = 0; i < 3; i++) await s.tick();
  assert.equal(s.mount.getAttribute("data-gosx-scene3d-shaders"), null);
  assert.equal(s.events.length, 0);
  assert.equal(gl.sources.filter(source => source.includes("warm-up-probe")).length, 0);
});
