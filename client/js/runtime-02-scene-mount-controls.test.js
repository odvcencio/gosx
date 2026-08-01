"use strict";
// Scene3D mount lifecycle: frame-boundary ordering, disposal, the command
// bridge, the WASM motion seam and the built-in orbit / first-person controls.
//
// Split out of the former client/js/runtime.test.js, which this set replaces
// and which no longer exists. Every shared fake, sandbox builder and fixture
// factory lives in ./runtime-test-harness.js.

const test = require("node:test");
const assert = require("node:assert/strict");
const path = require("node:path");

const {
  bootstrapSource,
  bootstrapRuntimeSource,
  bootstrapFeatureEnginesSource,
  bootstrapFeatureScene3DSource,
  FakeElement,
  createContext,
  installManualRAF,
  runScript,
  flushAsyncWork,
  mountMotionSeamScene,
  motionMeshExtents,
  mountMaterialMotionScene,
  FakeWebGLContext,
} = require("./runtime-test-harness.js");

test("bootstrap hydrates shared-runtime Scene3D programs", async () => {
  const mount = new FakeElement("div", null);
  mount.id = "scene-runtime-root";

  const env = createContext({
    elements: [mount],
    enableWebGL: true,
    disableCanvas2D: true,
    fetchRoutes: {
      "/runtime.wasm": { bytes: [0, 97, 115, 109] },
      "/scene-program.json": { text: '{"name":"GeometryZoo"}' },
    },
    manifest: {
      runtime: { path: "/runtime.wasm" },
      engines: [
        {
          id: "gosx-engine-rt",
          component: "GoSXScene3D",
          kind: "surface",
          mountId: "scene-runtime-root",
          runtime: "shared",
          props: { width: 640, height: 360, background: "#08151f" },
          programRef: "/scene-program.json",
        },
      ],
    },
    onHydrateEngine: () => JSON.stringify([
      { kind: 5, objectId: 0, data: { x: 0, y: 0, z: 6, fov: 75 } },
      {
        kind: 0,
        objectId: 1,
        data: {
          kind: "mesh",
          geometry: "sphere",
          material: "flat",
          props: { x: 0, y: 0, z: 0, radius: 1.2, color: "#8de1ff", spinY: 0.35 },
        },
      },
    ]),
    onRenderEngine: () => JSON.stringify({
      background: "#08151f",
      camera: { x: 0.1, y: -0.05, z: 6.2, fov: 72 },
      lines: [
        {
          from: { x: 10, y: 12 },
          to: { x: 120, y: 96 },
          color: "#8de1ff",
          lineWidth: 1.8,
        },
      ],
      positions: [-0.9, 0.93, -0.2, 0.47],
      colors: [0.55, 0.88, 1, 1, 0.55, 0.88, 1, 1],
      vertexCount: 2,
      worldPositions: [
        -2.4, -1.5, 0.1, 2.4, -1.5, 0.1,
        -0.8, 0.2, 0.5, 0.7, 0.9, 1.1,
        -1.2, -0.4, 0.2, 1.1, 0.6, 1.4,
      ],
      worldColors: [
        0.25, 0.33, 0.41, 1, 0.25, 0.33, 0.41, 1,
        0.78, 0.92, 1, 1, 0.78, 0.92, 1, 1,
        0.55, 0.88, 1, 1, 0.55, 0.88, 1, 1,
      ],
      worldVertexCount: 6,
      materials: [
        { kind: "flat", color: "#35556a", opacity: 1, wireframe: true, blendMode: "opaque", emissive: 0 },
        { kind: "glass", color: "#c7f0ff", opacity: 0.45, wireframe: true, blendMode: "alpha", emissive: 0.05 },
        { kind: "glow", color: "#8de1ff", opacity: 0.7, wireframe: true, blendMode: "additive", emissive: 0.4 },
      ],
      objects: [
        { id: "floor", kind: "plane", materialIndex: 0, vertexOffset: 0, vertexCount: 2, static: true },
        { id: "shield", kind: "box", materialIndex: 1, vertexOffset: 2, vertexCount: 2, static: false },
        { id: "orb", kind: "sphere", materialIndex: 2, vertexOffset: 4, vertexCount: 2, static: false },
      ],
      labels: [
        {
          id: "orb-label",
          text: "Orbit node\nShared runtime",
          position: { x: 318, y: 132 },
          depth: 7.2,
          maxWidth: 188,
          font: '600 13px "IBM Plex Sans", "Segoe UI", sans-serif',
          lineHeight: 18,
          whiteSpace: "pre-wrap",
          textAlign: "center",
        },
      ],
      objectCount: 3,
    }),
  });
  const textLayoutCalls = [];
  env.context.__gosx_text_layout = (...args) => {
    textLayoutCalls.push(args);
    return {
      lines: [{ text: "Orbit node" }, { text: "Shared runtime" }],
      lineCount: 2,
      height: 36,
      maxLineWidth: 94,
    };
  };

  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();

  assert.equal(env.engineHydrateCalls.length, 1);
  assert.deepEqual(env.engineHydrateCalls[0].slice(0, 3), [
    "gosx-engine-rt",
    "GoSXScene3D",
    '{"width":640,"height":360,"background":"#08151f"}',
  ]);
  assert.equal(
    env.fetchCalls.some((call) => call.url === "/scene-program.json"),
    true,
  );
  assert.equal(mount.children[0].tagName, "CANVAS");
  assert.equal(mount.getAttribute("data-gosx-scene3d-renderer"), "webgl");
  assert.equal(env.engineRenderCalls.length > 0, true);
  assert.equal(env.engineTickCalls.length, 0);
  const gl = mount.children[0].getContext("webgl2") || mount.children[0].getContext("webgl");
  assert.ok(gl.ops.some((entry) => entry[0] === "uniform4f" && entry[1] === "u_camera"));
  assert.ok(gl.ops.some((entry) => entry[0] === "vertexAttribPointer" && entry[2] === 3));
  assert.ok(gl.ops.some((entry) => entry[0] === "vertexAttribPointer" && entry[1] === 2 && entry[2] === 3));
  assert.ok(gl.ops.filter((entry) => entry[0] === "drawArrays").length >= 2);
  assert.ok(gl.ops.some((entry) => entry[0] === "enable" && entry[1] === gl.BLEND));
  assert.ok(gl.ops.some((entry) => entry[0] === "enable" && entry[1] === gl.DEPTH_TEST));
  assert.ok(gl.ops.some((entry) => entry[0] === "clear" && entry[1] === (gl.COLOR_BUFFER_BIT | gl.DEPTH_BUFFER_BIT)));
  assert.ok(gl.ops.some((entry) => entry[0] === "depthMask" && entry[1] === true));
  assert.ok(gl.ops.some((entry) => entry[0] === "depthMask" && entry[1] === false));
  assert.ok(gl.ops.some((entry) => entry[0] === "bufferData" && entry[4] === gl.STATIC_DRAW));
  assert.ok(gl.ops.some((entry) => entry[0] === "bufferData" && entry[4] === gl.DYNAMIC_DRAW));
  assert.ok(gl.ops.some((entry) => entry[0] === "blendFunc" && entry[1] === gl.SRC_ALPHA && entry[2] === gl.ONE_MINUS_SRC_ALPHA));
  assert.ok(gl.ops.some((entry) => entry[0] === "blendFunc" && entry[1] === gl.SRC_ALPHA && entry[2] === gl.ONE));
  assert.equal(mount.children.length, 2);
  assert.equal(mount.children[1].getAttribute("data-gosx-scene3d-label-layer"), "true");
  assert.equal(mount.children[1].children.length, 1);
  assert.equal(mount.children[1].children[0].textContent, "Orbit nodeShared runtime");
  assert.equal(textLayoutCalls.length, 1);
  assert.equal(textLayoutCalls[0][0], "Orbit node\nShared runtime");
  assert.equal(textLayoutCalls[0][1], '600 13px "IBM Plex Sans", "Segoe UI", sans-serif');
  assert.equal(textLayoutCalls[0][2], 188);
  assert.equal(textLayoutCalls[0][3], "pre-wrap");
  assert.equal(textLayoutCalls[0][4], 18);
  assert.equal(textLayoutCalls[0][5].maxLines, 0);
  assert.equal(textLayoutCalls[0][5].overflow, "clip");

  env.context.__gosx_dispose_engine("gosx-engine-rt");
  assert.deepEqual(env.engineDisposeCalls, [["gosx-engine-rt"]]);
});

test("Scene3D initial render waits for the second frame boundary", async () => {
  const mount = new FakeElement("div", null);
  mount.id = "scene-initial-frame-root";
  mount.width = 320;

  const env = createContext({
    elements: [mount],
    enableWebGL: true,
    disableCanvas2D: true,
    devicePixelRatio: 2,
    fetchRoutes: {
      "/runtime.wasm": { bytes: [0, 97, 115, 109] },
      "/scene-initial-frame-program.json": { text: '{"name":"InitialFrame"}' },
    },
    manifest: {
      runtime: { path: "/runtime.wasm" },
      engines: [
        {
          id: "gosx-engine-initial-frame",
          component: "GoSXScene3D",
          kind: "surface",
          mountId: "scene-initial-frame-root",
          runtime: "shared",
          props: {
            width: 320,
            height: 180,
            background: "#08151f",
            scrollCameraStart: 10,
            scrollCameraEnd: 4,
          },
          programRef: "/scene-initial-frame-program.json",
        },
      ],
    },
    onHydrateEngine: () => "[]",
    onRenderEngine: () => JSON.stringify({
      background: "#08151f",
      camera: { x: 0, y: 0, z: 6, fov: 72 },
      positions: [-0.5, 0, 0.5, 0],
      colors: [0.55, 0.88, 1, 1, 0.55, 0.88, 1, 1],
      vertexCount: 2,
      objectCount: 0,
    }),
  });
  env.context.__gosx_scene3d_perf = true;
  const raf = installManualRAF(env.context);

  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();
  await Promise.resolve();

  assert.equal(env.engineHydrateCalls.length, 1);
  assert.equal(env.engineRenderCalls.length, 0, "initial render must not run in the current task or microtasks");
  assert.equal(mount.getAttribute("data-gosx-scene3d-ready"), "false");
  assert.equal(mount.getAttribute("data-gosx-scene3d-mounted"), null);
  assert.equal(raf.count(), 1);

  const mounted = env.context.__gosx.engines.get("gosx-engine-initial-frame");
  assert.ok(mounted, "command handle can exist before scene-ready");
  assert.equal(mount.getAttribute("data-gosx-scene3d-command-ready"), "true");
  await mounted.handle.applyCommands([
    { kind: 0, objectId: "queued-point", data: { kind: "point", props: { id: "queued-point", x: 1, y: 2, z: 3 } } },
  ]);
  mounted.handle.updateSceneProps({ maxPixelRatio: 1.5 });
  env.context.scrollY = 900;
  env.context.dispatchEvent({ type: "scroll" });
  await flushAsyncWork();

  assert.equal(env.engineRenderCalls.length, 0, "pre-boundary commands, prop updates, and scroll must not render");
  assert.equal(raf.count(), 1, "pre-boundary scheduleRender calls must not queue a first-frame render");

  raf.flush(16);
  await Promise.resolve();

  assert.equal(env.engineRenderCalls.length, 0, "first frame gives the browser its paint opportunity");
  assert.equal(mount.getAttribute("data-gosx-scene3d-ready"), "false");
  assert.equal(mount.getAttribute("data-gosx-scene3d-mounted"), null);
  assert.equal(raf.count(), 1);

  raf.flush(32);
  await Promise.resolve();

  assert.equal(env.engineRenderCalls.length, 1, "second frame performs the initial Scene3D render");
  assert.equal(mount.getAttribute("data-gosx-scene3d-ready"), "true");
  assert.equal(mount.getAttribute("data-gosx-scene3d-mounted"), "true");
  assert.equal(mount.__gosxScene3DScheduleCounts["schedule:commands"], 1);
  assert.equal(mount.__gosxScene3DScheduleCounts["schedule:update-props"], 1);
  assert.equal(mount.__gosxScene3DScheduleCounts["schedule:scroll"], 1);
});

function createDeclarativeMaterialAnimationEnv(options = {}) {
  const mount = new FakeElement("div", null);
  mount.id = options.mountId || "scene-material-animation-root";
  const props = Object.assign({
    width: 320,
    height: 180,
    background: "#08151f",
    materialAnimation: Boolean(options.materialAnimation),
    scene: {
      labels: [
        {
          id: "clock-label",
          text: "Clock",
          x: 0,
          y: 0,
          z: 0,
          maxWidth: 120,
        },
      ],
    },
  }, options.props || {});
  const env = createContext({
    elements: [mount],
    enableWebGL: options.enableWebGL2 ? false : true,
    enableWebGL2: Boolean(options.enableWebGL2),
    disableCanvas2D: true,
    prefersReducedMotion: Boolean(options.prefersReducedMotion),
    performanceNow: options.performanceNow,
    manifest: {
      engines: [
        {
          id: options.engineId || "gosx-engine-material-animation",
          component: "GoSXScene3D",
          kind: "surface",
          mountId: mount.id,
          props,
        },
      ],
    },
  });
  if (options.enableWebGL2) {
    env.context.WebGL2RenderingContext = FakeWebGLContext;
  }
  return { env, mount };
}

async function mountDeclarativeSceneWithRAF(options = {}) {
  const setup = createDeclarativeMaterialAnimationEnv(options);
  const raf = installManualRAF(setup.env.context);
  runScript(bootstrapSource, setup.env.context, "bootstrap.js");
  await flushAsyncWork();
  raf.flush(16);
  await Promise.resolve();
  raf.flush(32);
  await Promise.resolve();
  return Object.assign({ raf }, setup);
}

test("Scene3D materialAnimation keeps declarative material frames running without autoRotate", async () => {
  const { mount, raf } = await mountDeclarativeSceneWithRAF({ materialAnimation: true });

  assert.equal(mount.getAttribute("data-gosx-scene3d-render-loop"), "active");
  assert.equal(mount.getAttribute("data-gosx-scene3d-render-loop-reason"), "material-animation");
  assert.equal(mount.getAttribute("data-gosx-scene3d-render-loop-wants-animation"), "true");
  assert.equal(raf.count(), 1, "materialAnimation owns one continuous frame");
  assert.equal(mount.__gosxScene3DState.camera.x, 0);
  assert.equal(mount.__gosxScene3DState.camera.y, 0);
  assert.equal(mount.__gosxScene3DState.camera.z, 6);

  raf.flush(48);
  await Promise.resolve();

  assert.equal(raf.count(), 1, "materialAnimation must not stack frame handles");
  assert.equal(mount.__gosxScene3DState.camera.x, 0);
  assert.equal(mount.__gosxScene3DState.camera.y, 0);
  assert.equal(mount.__gosxScene3DState.camera.z, 6);
});

test("Scene3D materialAnimation uploads authored point time on each WebGL frame", async () => {
  let nowMS = 1000;
  const vertexGLSL = [
    "attribute vec3 a_position;",
    "attribute float a_size;",
    "attribute vec4 a_color;",
    "uniform mat4 u_viewMatrix;",
    "uniform mat4 u_projectionMatrix;",
    "uniform mat4 u_modelMatrix;",
    "uniform float time;",
    "uniform float brightness;",
    "void main() {",
    "  gl_Position = u_projectionMatrix * u_viewMatrix * u_modelMatrix * vec4(a_position, 1.0);",
    "  gl_PointSize = max(1.0, a_size + brightness + sin(time));",
    "}",
  ].join("\n");
  const fragmentGLSL = [
    "precision mediump float;",
    "uniform float time;",
    "uniform float brightness;",
    "void main() {",
    "  gl_FragColor = vec4(abs(sin(time)) * brightness, 0.25, 0.5, 1.0);",
    "}",
  ].join("\n");
  const { env, mount, raf } = await mountDeclarativeSceneWithRAF({
    materialAnimation: true,
    enableWebGL2: true,
    performanceNow: () => nowMS,
    props: {
      scene: {
        points: [
          {
            id: "twinkle-points",
            count: 2,
            positions: [0, 0, 0, 1, 0, 0],
            sizes: [1, 1],
            colors: [1, 1, 1, 1, 0.5, 0.5, 1, 1],
            customVertex: vertexGLSL,
            customFragment: fragmentGLSL,
            customUniforms: { time: 99, brightness: 1.25 },
          },
        ],
      },
    },
  });
  const gl = mount.children[0].getContext("webgl2") || mount.children[0].getContext("webgl");
  nowMS = 2160;
  raf.flush(48);
  await Promise.resolve();
  nowMS = 3320;
  raf.flush(64);
  await Promise.resolve();

  const authoredProgram = gl.programMatching("abs(sin(time))");
  assert.ok(authoredProgram, "authored point GLSL program must compile");
  const authoredDraws = gl.ops.filter((entry) =>
    entry[0] === "drawArrays" && entry[1] === gl.POINTS && entry[4] === authoredProgram.id);
  assert.equal(authoredDraws.length >= 3, true, "materialAnimation must draw authored points across frames");
  const timeUploads = gl.ops
    .filter((entry) => entry[0] === "uniform1f" && entry[1] === "time")
    .map((entry) => entry[2]);
  assert.ok(timeUploads.includes(1), "initial frame must upload authored point time");
  assert.ok(timeUploads.includes(2.16), "frame two must upload updated authored point time");
  assert.ok(timeUploads.includes(3.32), "frame three must upload updated authored point time");
  assert.equal(timeUploads.includes(99), false, "customUniforms.time must not shadow runtime-owned time");
  assert.equal(mount.getAttribute("data-gosx-scene3d-render-loop-reason"), "material-animation");
  assert.equal(env.consoleLogs.warn.filter((m) => m.includes("Points authored") && m.includes("falling back")).length, 0);
});

test("Scene3D materialAnimation is opt-in for declarative static scenes", async () => {
  const { mount, raf } = await mountDeclarativeSceneWithRAF({ materialAnimation: false });

  assert.equal(mount.getAttribute("data-gosx-scene3d-render-loop"), "stopped");
  assert.equal(mount.getAttribute("data-gosx-scene3d-render-loop-reason"), "static");
  assert.equal(mount.getAttribute("data-gosx-scene3d-render-loop-wants-animation"), "false");
  assert.equal(raf.count(), 0);
});

test("Scene3D materialAnimation honors reduced motion and offscreen lifecycle", async () => {
  const reduced = await mountDeclarativeSceneWithRAF({
    mountId: "scene-material-animation-reduced",
    engineId: "gosx-engine-material-animation-reduced",
    materialAnimation: true,
    prefersReducedMotion: true,
  });

  assert.equal(reduced.mount.getAttribute("data-gosx-scene3d-reduced-motion"), "true");
  assert.equal(reduced.mount.getAttribute("data-gosx-scene3d-render-loop"), "stopped");
  assert.equal(reduced.mount.getAttribute("data-gosx-scene3d-render-loop-reason"), "reduced-motion");
  assert.equal(reduced.raf.count(), 0);

  const active = await mountDeclarativeSceneWithRAF({
    mountId: "scene-material-animation-lifecycle",
    engineId: "gosx-engine-material-animation-lifecycle",
    materialAnimation: true,
  });

  assert.equal(active.env.intersectionObservers.length, 1);
  assert.equal(active.raf.count(), 1);
  active.env.intersectionObservers[0].trigger([
    { target: active.mount, isIntersecting: false, intersectionRatio: 0 },
  ]);
  await flushAsyncWork();

  assert.equal(active.mount.getAttribute("data-gosx-scene3d-active"), "false");
  assert.equal(active.mount.getAttribute("data-gosx-scene3d-render-loop"), "stopped");
  assert.equal(active.mount.getAttribute("data-gosx-scene3d-render-loop-reason"), "offscreen");
  assert.equal(active.raf.count(), 0);

  active.env.intersectionObservers[0].trigger([
    { target: active.mount, isIntersecting: true, intersectionRatio: 1 },
  ]);
  await flushAsyncWork();
  active.raf.flush(64);
  await Promise.resolve();

  assert.equal(active.mount.getAttribute("data-gosx-scene3d-active"), "true");
  assert.equal(active.mount.getAttribute("data-gosx-scene3d-render-loop"), "active");
  assert.equal(active.mount.getAttribute("data-gosx-scene3d-render-loop-reason"), "material-animation");
  assert.equal(active.raf.count(), 1);
});

test("Scene3D scroll camera offset moves camera by absolute scroll pixels", async () => {
  const mount = new FakeElement("div", null);
  mount.id = "scene-scroll-offset-root";

  const env = createContext({
    elements: [mount],
    enableWebGL: true,
    disableCanvas2D: true,
    manifest: {
      engines: [
        {
          id: "gosx-engine-scroll-offset",
          component: "GoSXScene3D",
          kind: "surface",
          mountId: "scene-scroll-offset-root",
          props: {
            width: 320,
            height: 180,
            background: "#08151f",
            camera: { x: 1, y: 2, z: 520, fov: 52, near: 1, far: 2400 },
            scrollCameraOffset: { y: -0.06 },
          },
        },
      ],
    },
  });
  const raf = installManualRAF(env.context);

  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();
  raf.flush(16);
  await Promise.resolve();
  raf.flush(32);
  await Promise.resolve();

  const mounted = env.context.__gosx.engines.get("gosx-engine-scroll-offset");
  assert.ok(mounted, "expected mounted Scene3D engine");
  assert.equal(mounted.handle.getCamera().y, 2);

  env.context.scrollY = 200;
  env.context.dispatchEvent({ type: "scroll" });
  assert.equal(mounted.handle.getCamera().y, -10);
  assert.equal(mounted.handle.getCamera().x, 1);
  assert.equal(mounted.handle.getCamera().z, 520);
});

test("Scene3D disposal cancels pending initial render before frame two", async () => {
  const mount = new FakeElement("div", null);
  mount.id = "scene-initial-dispose-root";

  const env = createContext({
    elements: [mount],
    enableWebGL: true,
    disableCanvas2D: true,
    fetchRoutes: {
      "/runtime.wasm": { bytes: [0, 97, 115, 109] },
      "/scene-initial-dispose-program.json": { text: '{"name":"InitialDispose"}' },
    },
    manifest: {
      runtime: { path: "/runtime.wasm" },
      engines: [
        {
          id: "gosx-engine-initial-dispose",
          component: "GoSXScene3D",
          kind: "surface",
          mountId: "scene-initial-dispose-root",
          runtime: "shared",
          props: { width: 320, height: 180, background: "#08151f" },
          programRef: "/scene-initial-dispose-program.json",
        },
      ],
    },
    onHydrateEngine: () => "[]",
    onRenderEngine: () => JSON.stringify({
      background: "#08151f",
      camera: { x: 0, y: 0, z: 6, fov: 72 },
      positions: [-0.5, 0, 0.5, 0],
      colors: [0.55, 0.88, 1, 1, 0.55, 0.88, 1, 1],
      vertexCount: 2,
      objectCount: 0,
    }),
  });
  const raf = installManualRAF(env.context);

  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();

  assert.equal(env.engineRenderCalls.length, 0);
  assert.equal(mount.getAttribute("data-gosx-scene3d-ready"), "false");
  assert.equal(mount.getAttribute("data-gosx-scene3d-mounted"), null);
  raf.flush(16);
  await Promise.resolve();
  assert.equal(env.engineRenderCalls.length, 0);
  assert.equal(raf.count(), 1);

  env.context.__gosx_dispose_engine("gosx-engine-initial-dispose");
  assert.equal(raf.count(), 0, "dispose must cancel the queued second-frame render");

  raf.flush(32);
  await Promise.resolve();

  assert.equal(env.engineRenderCalls.length, 0);
  assert.equal(mount.getAttribute("data-gosx-scene3d-ready"), "false");
  assert.equal(mount.getAttribute("data-gosx-scene3d-mounted"), null);
});

// Regression test for the split feature-bundle build: bootstrap-feature-scene3d.js
// runs in its own IIFE (see 26d-feature-scene3d-prefix.js), separate from the
// runtime bundle's closure that declares `sceneLabelLayoutCacheLimit`
// (00-textlayout.js). Before the fix, layoutSceneLabel() in 20-scene-mount.js
// threw "ReferenceError: sceneLabelLayoutCacheLimit is not defined" the first
// time ANY scene with a Label node laid out text under this bundle
// combination — silently breaking every comment pin / scene label in
// production, since apps load bootstrap-runtime.js + bootstrap-feature-scene3d.js,
// never the monolithic bootstrap.js (which happened to mask the bug because
// all files share one closure there).
//
// Mirrors kiln's comment-pin flow exactly: mount a plain (non-shared-runtime)
// GoSXScene3D engine via the split bundles, then drive a Label into the scene
// via the SAME public handle.applyCommands([{kind:0 /* SCENE_CMD_CREATE_OBJECT */,
// objectId, data:{kind:"label", props:{...}}}]) primitive kiln's pin sync (and
// P6's declarative scene commands) use — see kiln/app/workspace_comments.go.
test("Scene3D label layout does not throw under the split runtime+scene3d feature bundles", async () => {
  const mount = new FakeElement("div", null);
  mount.id = "scene-split-bundle-label-root";

  const env = createContext({
    elements: [mount],
    enableWebGL: true,
    disableCanvas2D: true,
    fetchRoutes: {
      "/gosx/bootstrap-feature-engines.js": { text: bootstrapFeatureEnginesSource },
    },
    manifest: {
      engines: [
        {
          id: "gosx-engine-split-label",
          component: "GoSXScene3D",
          kind: "surface",
          mountId: "scene-split-bundle-label-root",
          props: {
            width: 320,
            height: 180,
            background: "#08151f",
            scene: { objects: [] },
          },
        },
      ],
    },
  });

  const uncaughtErrors = [];
  env.context.addEventListener("error", (event) => {
    uncaughtErrors.push(event && event.error ? event.error : event);
  });

  // The exact bundle combo real apps load: the runtime chunk (which owns
  // window.__gosx_runtime_api / sceneLabelLayoutCacheLimit) followed by the
  // async Scene3D feature chunk — NOT the monolithic bootstrap.js.
  runScript(bootstrapRuntimeSource, env.context, "bootstrap-runtime.js");
  runScript(bootstrapFeatureScene3DSource, env.context, "bootstrap-feature-scene3d.js");
  await flushAsyncWork();

  assert.deepEqual(uncaughtErrors, [], "mounting an empty scene must not throw");
  assert.equal(env.consoleLogs.error.length, 0, "expected no console.error, got: " + JSON.stringify(env.consoleLogs.error));

  const engineState = env.context.__gosx.engines.get("gosx-engine-split-label");
  assert.ok(engineState, "expected engine to mount");
  assert.equal(typeof engineState.handle.applyCommands, "function");

  // This is the call that threw ReferenceError before the fix: any Label
  // node laid out under the split bundles crashed layoutSceneLabel().
  engineState.handle.applyCommands([
    {
      kind: 0,
      objectId: "ws-comment-pin-1",
      data: {
        kind: "label",
        props: {
          id: "ws-comment-pin-1",
          text: "Pin comment",
          className: "ws-comment-pin",
          x: 1,
          y: 2,
          z: 3,
        },
      },
    },
  ]);
  await flushAsyncWork();

  assert.deepEqual(uncaughtErrors, [], "applyCommands([label]) must not throw ReferenceError");
  assert.equal(env.consoleLogs.error.length, 0, "expected no console.error after applyCommands, got: " + JSON.stringify(env.consoleLogs.error));
  assert.equal(mount.getAttribute("data-gosx-scene3d-renderer"), "webgl");
  assert.equal(mount.children.length, 2, "canvas + label layer must both mount");
  assert.equal(mount.children[1].getAttribute("data-gosx-scene3d-label-layer"), "true");
  assert.equal(mount.children[1].children.length, 1, "the applyCommands label must render");
  assert.equal(mount.children[1].children[0].textContent, "Pin comment");

  env.context.__gosx_dispose_engine("gosx-engine-split-label");
});

test("Scene3D mount command bridge applies only increasing revisions and reports completion", async () => {
  const mount = new FakeElement("div", null);
  mount.id = "scene-mount-command-bridge";
  const env = createContext({
    elements: [mount],
    enableWebGL: true,
    disableCanvas2D: true,
    manifest: { engines: [{
      id: "gosx-engine-command-bridge",
      component: "GoSXScene3D",
      kind: "surface",
      mountId: mount.id,
      props: { width: 320, height: 180, scene: { objects: [] } },
    }] },
  });
  const applied = [];
  mount.addEventListener("gosx:scene3d:commands-applied", (event) => applied.push(event.detail));
  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();

  const createLabel = (id) => ({ kind: 0, objectId: id, data: { kind: "label", props: { id, text: id } } });
  mount.dispatchEvent(new env.context.CustomEvent("gosx:scene3d:commands", {
    detail: { revision: 2, commands: [createLabel("accepted")] },
  }));
  mount.dispatchEvent(new env.context.CustomEvent("gosx:scene3d:commands", {
    detail: { revision: 1, commands: [createLabel("stale")] },
  }));
  await flushAsyncWork();

  assert.equal(applied.length, 1);
  assert.equal(applied[0].revision, 2);
  assert.equal(applied[0].commandCount, 1);
  assert.equal(mount.children[1].children.length, 1);
  assert.equal(mount.children[1].children[0].textContent, "accepted");

  env.context.__gosx_dispose_engine("gosx-engine-command-bridge");
  mount.dispatchEvent(new env.context.CustomEvent("gosx:scene3d:commands", {
    detail: { revision: 3, commands: [createLabel("disposed")] },
  }));
  assert.equal(applied.length, 1, "dispose must remove the mount listener");
});

// Forces the JS-sceneState fall-through path via onRenderEngine: () => "" (so
// ctx.runtime.renderFrame returns "" and the runtime-bundle early-return is
// skipped). applyWasmMotionFrame only runs on this path, not for production
// shared-runtime scenes whose Go bundle is returned by ctx.runtime.renderFrame.
test("Scene3D WASM motion seam applies a quaternion rotation through SET_TRANSFORM (sceneState fall-through path)", async () => {
  // Quaternion for +90° about Y: (0, sin45, 0, cos45). Decoded → rotationY = π/2
  // → world-space X/Z extents swap (the box's 1.5 half-width moves to Z).
  const s = Math.SQRT1_2;
  const tick = (handle, t, reduced, outU8) => {
    const f = new Float64Array(outU8.buffer, outU8.byteOffset, outU8.byteLength / 8);
    // packed: [targetID, propID, arity(quat=4), qx, qy, qz, qw]
    f[0] = 0; f[1] = 0; f[2] = 4; f[3] = 0; f[4] = s; f[5] = 0; f[6] = s;
    return 7;
  };
  const { gl } = await mountMotionSeamScene(true, tick);
  assert.ok(gl, "expected a WebGL context");

  const ext = motionMeshExtents(gl);
  // Pre-rotation the box reaches |x|≈1.5, |z|≈0.2. After a 90° Y rotation the
  // position extents swap, proving decode → quat → Euler → applySceneCommands
  // mutated the stored object's rotationY and the renderer consumed it.
  assert.ok(ext.maxAbsZ > 1.0, `expected rotated z-extent > 1.0, got ${ext.maxAbsZ}`);
  assert.ok(ext.maxAbsX < 1.0, `expected collapsed x-extent < 1.0, got ${ext.maxAbsX}`);
});

// Forces the JS-sceneState fall-through path via onRenderEngine: () => "" (same
// as above). Verifies that applyWasmMotionFrame exits immediately when the
// opt-in flag is absent, even on this fall-through path.
test("Scene3D WASM motion seam stays inert when the flag is unset (sceneState fall-through path)", async () => {
  let tickCalls = 0;
  const tick = () => { tickCalls += 1; return 7; };
  // motionFlag=false: window.__gosx_motion_wasm is never set, so even though the
  // exports could be present, applyWasmMotionFrame returns before any motion
  // work. mountMotionSeamScene still installs the tick stub through tickFn so we
  // can prove it is never invoked.
  const mount = new FakeElement("div", null);
  mount.id = "scene-motion-inert-root";
  const env = createContext({
    elements: [mount],
    enableWebGL: true,
    disableCanvas2D: true,
    fetchRoutes: {
      "/runtime.wasm": { bytes: [0, 97, 115, 109] },
      "/scene-motion.json": { text: '{"name":"MotionSeam"}' },
    },
    manifest: {
      runtime: { path: "/runtime.wasm" },
      engines: [
        {
          id: "gosx-engine-motion-inert",
          component: "GoSXScene3D",
          kind: "surface",
          mountId: "scene-motion-inert-root",
          runtime: "shared",
          programRef: "/scene-motion.json",
          props: { width: 640, height: 360, background: "#08151f", scene: { motionProgram: "AAEC" } },
        },
      ],
    },
    onHydrateEngine: () => JSON.stringify([
      { kind: 5, objectId: 0, data: { x: 0, y: 0, z: 8, fov: 75 } },
      {
        kind: 0,
        objectId: "cube",
        data: {
          kind: "box",
          geometry: "box",
          material: "flat",
          props: { x: 0, y: 0, z: 0, width: 3, height: 0.4, depth: 0.4, color: "#8de1ff" },
        },
      },
    ]),
    onRenderEngine: () => "",
  });
  // Exports present but the opt-in flag is deliberately NOT set.
  env.context.__gosx_motion_load = () => 1;
  env.context.__gosx_motion_refs = () => ({ target: ["cube"], prop: ["rotation"] });
  env.context.__gosx_motion_tick = tick;
  env.context.__gosx_motion_unload = () => {};

  const raf = installManualRAF(env.context);
  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();
  raf.flush(16);
  await flushAsyncWork();
  raf.flush(32);
  await flushAsyncWork();

  const gl = mount.children[0].getContext("webgl");
  const ext = motionMeshExtents(gl);
  assert.equal(tickCalls, 0, "motion tick must not run when the flag is unset");
  assert.ok(ext.maxAbsX > 1.0, `expected unrotated x-extent > 1.0, got ${ext.maxAbsX}`);
  assert.ok(ext.maxAbsZ < 1.0, `expected unrotated z-extent < 1.0, got ${ext.maxAbsZ}`);
});

test("Scene3D WASM material-motion seam writes evaluated color into customUniforms (sceneState fall-through path)", async () => {
  const { state, color } = await mountMaterialMotionScene(true);
  assert.ok(state && state.objects && typeof state.objects.get === "function", "expected a live sceneState handle on the mount");
  const obj = state.objects.get("glow-cube");
  assert.ok(obj, "expected the glow-cube object in sceneState");
  assert.ok(obj.customUniforms && typeof obj.customUniforms === "object", "expected customUniforms on glow-cube");
  // Decode → customUniforms write: the Color record (arity 5 → width 4) lands
  // as a 4-element array under the uniform name "emissive".
  assert.deepEqual(obj.customUniforms.emissive, color,
    "motion-evaluated color must be written into customUniforms.emissive");
});

test("Scene3D WASM material-motion seam stays inert when the flag is unset (sceneState fall-through path)", async () => {
  const { state, tickCalls } = await mountMaterialMotionScene(false);
  assert.equal(tickCalls(), 0, "material-motion tick must not run when the flag is unset");
  const obj = state.objects.get("glow-cube");
  assert.ok(obj, "expected the glow-cube object in sceneState");
  // customUniforms.emissive stays at its hydrated default — proving non-breaking
  // inert behavior with the exports present but the opt-in flag absent.
  assert.deepEqual(obj.customUniforms.emissive, [0, 0, 0, 0],
    "customUniforms must be untouched when the motion flag is unset");
});

test("Scene3D drag only starts when the pointer lands on a shape in shared runtime mode", async () => {
  const mount = new FakeElement("div", null);
  mount.id = "scene-fallback-root";

  const env = createContext({
    elements: [mount],
    fetchRoutes: {
      "/runtime.wasm": { bytes: [0, 97, 115, 109] },
      "/scene-drag-program.json": { text: '{"name":"SceneDrag"}' },
    },
    manifest: {
      runtime: { path: "/runtime.wasm" },
      engines: [
        {
          id: "gosx-engine-fallback",
          component: "GoSXScene3D",
          kind: "surface",
          mountId: "scene-fallback-root",
          runtime: "shared",
          programRef: "/scene-drag-program.json",
          props: {
            width: 640,
            height: 360,
            background: "#08151f",
            autoRotate: false,
            dragToRotate: true,
            dragSignalNamespace: "$scene.test.drag",
            camera: { x: 0, y: 0, z: 6, fov: 72 },
          },
        },
      ],
    },
    onHydrateEngine: () => "[]",
    onRenderEngine: () => JSON.stringify({
      background: "#08151f",
      camera: { z: 6, fov: 72 },
      positions: [],
      colors: [],
      vertexCount: 0,
      worldPositions: [
        -2.4, -1.5, 0.1, 2.4, -1.5, 0.1,
        -0.8, 0.2, 0.5, 0.7, 0.9, 1.1,
      ],
      worldColors: [
        0.25, 0.33, 0.41, 1, 0.25, 0.33, 0.41, 1,
        0.78, 0.92, 1, 1, 0.78, 0.92, 1, 1,
      ],
      worldVertexCount: 4,
      materials: [
        { kind: "flat", color: "#35556a", opacity: 1, wireframe: true, blendMode: "opaque", emissive: 0 },
        { kind: "flat", color: "#8de1ff", opacity: 1, wireframe: true, blendMode: "opaque", emissive: 0 },
      ],
      objects: [
        {
          id: "floor",
          kind: "plane",
          materialIndex: 0,
          vertexOffset: 0,
          vertexCount: 2,
          static: true,
          bounds: { minX: -2.4, minY: -1.5, minZ: 0.1, maxX: 2.4, maxY: -1.5, maxZ: 0.1 },
        },
        {
          id: "shape",
          kind: "box",
          materialIndex: 1,
          vertexOffset: 2,
          vertexCount: 2,
          static: false,
          bounds: { minX: -0.8, minY: 0.2, minZ: 0.5, maxX: 0.7, maxY: 0.9, maxZ: 1.1 },
        },
      ],
      objectCount: 2,
    }),
  });

  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();

  const canvas = mount.children[0];
  assert.equal(canvas.tagName, "CANVAS");
  assert.equal(canvas.style.cursor, "grab");
  assert.equal((env.document.eventListeners.get("pointermove") || []).length, 0);
  assert.equal((env.document.eventListeners.get("pointerup") || []).length, 0);
  assert.equal((env.document.eventListeners.get("pointercancel") || []).length, 0);

  canvas.dispatchEvent({
    type: "pointerdown",
    button: 0,
    pointerId: 1,
    clientX: 56,
    clientY: 320,
    preventDefault() {},
    stopPropagation() {},
  });
  await flushAsyncWork();
  assert.equal(canvas.style.cursor, "grab");
  assert.equal(canvas._capturedPointerID, null);
  assert.equal(env.inputBatchCalls.length, 0);
  assert.equal((env.document.eventListeners.get("pointermove") || []).length, 0);

  canvas.dispatchEvent({
    type: "pointerdown",
    button: 0,
    pointerId: 2,
    clientX: 320,
    clientY: 160,
    preventDefault() {},
    stopPropagation() {},
  });
  await flushAsyncWork();

  assert.equal(canvas.style.cursor, "grabbing");
  assert.equal(canvas._capturedPointerID, 2);
  assert.equal((env.document.eventListeners.get("pointermove") || []).length, 1);
  assert.equal((env.document.eventListeners.get("pointerup") || []).length, 1);
  assert.equal((env.document.eventListeners.get("pointercancel") || []).length, 1);

  canvas.dispatchEvent({
    type: "pointerdown",
    button: 0,
    pointerId: 3,
    clientX: 320,
    clientY: 160,
    preventDefault() {},
    stopPropagation() {},
  });
  await flushAsyncWork();

  assert.equal(canvas._capturedPointerID, 2);

  canvas.dispatchEvent({
    type: "pointermove",
    button: 0,
    buttons: 1,
    pointerId: 2,
    clientX: 360,
    clientY: 130,
    preventDefault() {},
    stopPropagation() {},
  });
  await flushAsyncWork();

  assert.equal(env.inputBatchCalls.length > 0, true);
  const dragBatch = JSON.parse(env.inputBatchCalls[env.inputBatchCalls.length - 1][0]);
  assert.equal(dragBatch["$scene.test.drag.active"], true);
  assert.equal(dragBatch["$scene.test.drag.x"] > 0, true);
  assert.equal(dragBatch["$scene.test.drag.y"] > 0, true);
  assert.equal(dragBatch["$scene.test.drag.targetIndex"], 1);

  canvas.dispatchEvent({
    type: "pointerup",
    button: 0,
    pointerId: 2,
    clientX: 360,
    clientY: 130,
    preventDefault() {},
    stopPropagation() {},
  });
  await flushAsyncWork();
  assert.equal(canvas.style.cursor, "grab");
  assert.equal(canvas._capturedPointerID, null);
  assert.equal((env.document.eventListeners.get("pointermove") || []).length, 0);
  assert.equal((env.document.eventListeners.get("pointerup") || []).length, 0);
  assert.equal((env.document.eventListeners.get("pointercancel") || []).length, 0);
  const releaseBatch = JSON.parse(env.inputBatchCalls[env.inputBatchCalls.length - 1][0]);
  assert.equal(releaseBatch["$scene.test.drag.active"], false);
  assert.equal(releaseBatch["$scene.test.drag.targetIndex"], -1);
});

test("bootstrap drives shared-runtime Scene3D orbit controls without authored JS", async () => {
  const mount = new FakeElement("div", null);
  mount.id = "scene-shared-orbit-root";

  const env = createContext({
    elements: [mount],
    enableWebGL: true,
    disableCanvas2D: true,
    fetchRoutes: {
      "/runtime.wasm": { bytes: [0, 97, 115, 109] },
      "/scene-orbit-program.json": { text: '{"name":"SceneOrbit"}' },
    },
    manifest: {
      runtime: { path: "/runtime.wasm" },
      engines: [
        {
          id: "gosx-engine-orbit",
          component: "GoSXScene3D",
          kind: "surface",
          mountId: "scene-shared-orbit-root",
          runtime: "shared",
          programRef: "/scene-orbit-program.json",
          props: {
            width: 640,
            height: 360,
            background: "#08151f",
            autoRotate: false,
            controls: "orbit",
            controlTarget: { x: 0, y: 0.2, z: 0.8 },
            controlRotateMode: "pixel-degrees",
            controlRotateDirection: "grab",
            controlMinDistance: 2,
            controlMaxDistance: 10,
            controlPitchLimit: Math.PI / 2 - (Math.PI / 180) * 0.001,
            camera: { x: 0, y: 0.2, z: 6, fov: 72, near: 0.05, far: 128 },
          },
        },
      ],
    },
    onHydrateEngine: () => "[]",
    onRenderEngine: () => JSON.stringify({
      background: "#08151f",
      camera: { x: 0, y: 0.2, z: 6, fov: 72, near: 0.05, far: 128 },
      positions: [],
      colors: [],
      vertexCount: 0,
      worldPositions: [
        -1.2, -0.7, 0.1, 1.2, -0.7, 0.1,
        0.1, -0.2, 0.1, 0.1, 1.4, 1.5,
      ],
      worldColors: [
        0.55, 0.88, 1, 1, 0.55, 0.88, 1, 1,
        0.78, 0.92, 1, 1, 0.78, 0.92, 1, 1,
      ],
      worldVertexCount: 4,
      materials: [
        { kind: "flat", color: "#8de1ff", opacity: 1, wireframe: true, blendMode: "opaque", renderPass: "opaque", emissive: 0 },
      ],
      objects: [
        {
          id: "frame",
          kind: "box",
          materialIndex: 0,
          renderPass: "opaque",
          vertexOffset: 0,
          vertexCount: 4,
          static: true,
          bounds: { minX: -1.2, minY: -0.7, minZ: 0.1, maxX: 1.2, maxY: 1.4, maxZ: 1.5 },
          depthNear: 6.1,
          depthFar: 7.5,
          depthCenter: 6.8,
        },
      ],
      passes: [
        {
          name: "staticOpaque",
          blend: "opaque",
          depth: "opaque",
          static: true,
          cacheKey: "orbit-static",
          positions: [
            -1.2, -0.7, 0.1, 1.2, -0.7, 0.1,
            0.1, -0.2, 0.1, 0.1, 1.4, 1.5,
          ],
          colors: [
            0.55, 0.88, 1, 1, 0.55, 0.88, 1, 1,
            0.78, 0.92, 1, 1, 0.78, 0.92, 1, 1,
          ],
          materials: [
            0, 0, 1,
            0, 0, 1,
            0, 0, 1,
            0, 0, 1,
          ],
          vertexCount: 4,
        },
      ],
      objectCount: 1,
    }),
  });
  const cameraEvents = [];
  env.document.addEventListener("gosx:engine:scene-camera", (event) => cameraEvents.push(event.detail));

  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();

  const canvas = mount.firstElementChild;
  assert.ok(canvas);
  let mounted = env.context.__gosx.engines.get("gosx-engine-orbit");
  for (let attempt = 0; attempt < 5 && !mounted; attempt += 1) {
    await flushAsyncWork();
    mounted = env.context.__gosx.engines.get("gosx-engine-orbit");
  }
  assert.ok(
    mounted,
    "expected gosx-engine-orbit to mount; keys=" +
      JSON.stringify(Array.from(env.context.__gosx.engines.keys())) +
      " attrs=" + JSON.stringify(Object.fromEntries(mount.attributes)) +
      " warn=" + JSON.stringify(env.consoleLogs.warn) +
      " error=" + JSON.stringify(env.consoleLogs.error),
  );
  assert.equal(typeof mounted.handle.getCamera, "function");
  assert.equal(typeof mounted.handle.setCamera, "function");
  const orbitTarget = { x: 0, y: 0.2, z: 0.8 };
  const cameraDistanceToTarget = (camera) => Math.hypot(
    camera.x - orbitTarget.x,
    camera.y - orbitTarget.y,
    camera.z - orbitTarget.z,
  );
  const initialCamera = mounted.handle.getCamera();
  assert.equal(Math.round(initialCamera.fov), 72);
  assert.ok(Math.abs(initialCamera.rotationX) < 0.0001);
  assert.ok(Math.abs(initialCamera.rotationY) < 0.0001);
  const projectedOrbitTarget = env.context.__gosx_scene3d_api.sceneProjectPoint(orbitTarget, initialCamera, 640, 360);
  assert.ok(projectedOrbitTarget, `orbit target should project in front of the initial camera: ${JSON.stringify(initialCamera)}`);
  assert.ok(Math.abs(projectedOrbitTarget.x - 320) < 0.001, `orbit target x should be centered: ${JSON.stringify(projectedOrbitTarget)}`);
  assert.ok(Math.abs(projectedOrbitTarget.y - 180) < 0.001, `orbit target y should be centered: ${JSON.stringify(projectedOrbitTarget)}`);

  canvas.dispatchEvent({
    type: "pointerdown",
    button: 0,
    pointerId: 9,
    clientX: 320,
    clientY: 180,
    preventDefault() {},
    stopPropagation() {},
  });
  canvas.dispatchEvent({
    type: "pointermove",
    button: 0,
    buttons: 1,
    pointerId: 9,
    clientX: 410,
    clientY: 120,
    preventDefault() {},
    stopPropagation() {},
  });
  canvas.dispatchEvent({
    type: "pointerup",
    button: 0,
    pointerId: 9,
    clientX: 410,
    clientY: 120,
    preventDefault() {},
    stopPropagation() {},
  });
  await flushAsyncWork();

  const draggedCamera = mounted.handle.getCamera();
  assert.ok(
    Math.abs(draggedCamera.rotationX - initialCamera.rotationX) > 0.01 ||
    Math.abs(draggedCamera.rotationY - initialCamera.rotationY) > 0.01 ||
    Math.abs(draggedCamera.x - initialCamera.x) > 0.01 ||
    Math.abs(draggedCamera.z - initialCamera.z) > 0.01,
  );
  assert.ok(draggedCamera.x < orbitTarget.x, `grab-right must orbit camera left, got ${JSON.stringify(draggedCamera)}`);
  assert.ok(draggedCamera.y < orbitTarget.y, `grab-up must move the camera down so the grabbed scene tracks upward, got ${JSON.stringify(draggedCamera)}`);

  const cameraBeforeZoom = mounted.handle.getCamera();
  canvas.dispatchEvent({
    type: "wheel",
    deltaY: -120,
    preventDefault() {},
    stopPropagation() {},
  });
  await flushAsyncWork();

  const zoomedCamera = mounted.handle.getCamera();
  assert.equal(Math.round(zoomedCamera.fov), 72);
  assert.ok(cameraDistanceToTarget(zoomedCamera) < cameraDistanceToTarget(cameraBeforeZoom));

  canvas.dispatchEvent({
    type: "wheel",
    deltaY: 10000,
    preventDefault() {},
    stopPropagation() {},
  });
  await flushAsyncWork();
  assert.ok(cameraDistanceToTarget(mounted.handle.getCamera()) <= 10.001);

  canvas.dispatchEvent({
    type: "wheel",
    deltaY: -10000,
    preventDefault() {},
    stopPropagation() {},
  });
  await flushAsyncWork();
  assert.ok(cameraDistanceToTarget(mounted.handle.getCamera()) >= 1.999);

  const handled = mounted.handle.setCamera({ x: 1, y: 1.5, z: 8, fov: 60, near: 0.1, far: 256 });
  assert.equal(handled, true);
  await flushAsyncWork();

  const handleCamera = mounted.handle.getCamera();
  assert.ok(Math.abs(handleCamera.x - 1) < 0.001);
  assert.ok(Math.abs(handleCamera.y - 1.5) < 0.001);
  assert.ok(Math.abs(handleCamera.z - 8) < 0.001);
  assert.equal(Math.round(handleCamera.fov), 60);
  assert.equal(
    cameraEvents.some((event) => event && event.detail && event.detail.reason === "handle-camera"),
    true,
  );

  assert.equal(mount.getAttribute("data-gosx-scene3d-controls"), "orbit");
  assert.equal(env.consoleLogs.warn.length, 0);
  assert.equal(env.consoleLogs.error.length, 0);
});

test("bootstrap drives shared-runtime Scene3D first-person controls without authored JS", async () => {
  const mount = new FakeElement("div", null);
  mount.id = "scene-shared-fps-root";

  const env = createContext({
    elements: [mount],
    enableWebGL: true,
    disableCanvas2D: true,
    fetchRoutes: {
      "/runtime.wasm": { bytes: [0, 97, 115, 109] },
      "/scene-fps-program.json": { text: '{"name":"SceneFPS"}' },
    },
    manifest: {
      runtime: { path: "/runtime.wasm" },
      engines: [
        {
          id: "gosx-engine-fps",
          component: "GoSXScene3D",
          kind: "surface",
          mountId: "scene-shared-fps-root",
          runtime: "shared",
          programRef: "/scene-fps-program.json",
          props: {
            width: 640,
            height: 360,
            background: "#08151f",
            autoRotate: false,
            controls: "first-person",
            pointerLock: true,
            controlMoveSpeed: 6,
            controlLookSpeed: 1,
            camera: { x: 0, y: 1.6, z: 6, fov: 72, near: 0.05, far: 128 },
          },
        },
      ],
    },
    onHydrateEngine: () => "[]",
    onRenderEngine: () => JSON.stringify({
      background: "#08151f",
      camera: { x: 0, y: 1.6, z: 6, fov: 72, near: 0.05, far: 128 },
      positions: [],
      colors: [],
      vertexCount: 0,
      worldPositions: [
        -1.2, -0.7, 0.1, 1.2, -0.7, 0.1,
        0.1, -0.2, 0.1, 0.1, 1.4, 1.5,
      ],
      worldColors: [
        0.55, 0.88, 1, 1, 0.55, 0.88, 1, 1,
        0.78, 0.92, 1, 1, 0.78, 0.92, 1, 1,
      ],
      worldVertexCount: 4,
      materials: [
        { kind: "flat", color: "#8de1ff", opacity: 1, wireframe: true, blendMode: "opaque", renderPass: "opaque", emissive: 0 },
      ],
      objects: [
        {
          id: "frame",
          kind: "box",
          materialIndex: 0,
          renderPass: "opaque",
          vertexOffset: 0,
          vertexCount: 4,
          static: true,
          bounds: { minX: -1.2, minY: -0.7, minZ: 0.1, maxX: 1.2, maxY: 1.4, maxZ: 1.5 },
          depthNear: 6.1,
          depthFar: 7.5,
          depthCenter: 6.8,
        },
      ],
      passes: [
        {
          name: "staticOpaque",
          blend: "opaque",
          depth: "opaque",
          static: true,
          cacheKey: "fps-static",
          positions: [
            -1.2, -0.7, 0.1, 1.2, -0.7, 0.1,
            0.1, -0.2, 0.1, 0.1, 1.4, 1.5,
          ],
          colors: [
            0.55, 0.88, 1, 1, 0.55, 0.88, 1, 1,
            0.78, 0.92, 1, 1, 0.78, 0.92, 1, 1,
          ],
          materials: [
            0, 0, 1,
            0, 0, 1,
            0, 0, 1,
            0, 0, 1,
          ],
          vertexCount: 4,
        },
      ],
      objectCount: 1,
    }),
  });

  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();

  const canvas = mount.firstElementChild;
  const gl = canvas.getContext("webgl");
  const initialCamera = gl.ops.filter((entry) => entry[0] === "uniform4f" && entry[1] === "u_camera").at(-1);
  const initialRotation = gl.ops.filter((entry) => entry[0] === "uniform3f" && entry[1] === "u_camera_rotation").at(-1);
  assert.ok(initialCamera);
  assert.ok(initialRotation);

  canvas.dispatchEvent({
    type: "pointerdown",
    button: 0,
    pointerId: 4,
    clientX: 320,
    clientY: 180,
    preventDefault() {},
    stopPropagation() {},
  });
  canvas.dispatchEvent({
    type: "pointermove",
    button: 0,
    buttons: 1,
    pointerId: 4,
    clientX: 390,
    clientY: 160,
    movementX: 70,
    movementY: -20,
    preventDefault() {},
    stopPropagation() {},
  });
  canvas.dispatchEvent({
    type: "pointerup",
    button: 0,
    pointerId: 4,
    clientX: 390,
    clientY: 160,
    preventDefault() {},
    stopPropagation() {},
  });
  await flushAsyncWork();

  const draggedRotation = gl.ops.filter((entry) => entry[0] === "uniform3f" && entry[1] === "u_camera_rotation").at(-1);
  assert.ok(draggedRotation);
  assert.notDeepEqual(draggedRotation, initialRotation);
  assert.equal(canvas.pointerLockCalls.length, 1);
  assert.equal(env.document.pointerLockElement, canvas);

  env.document.dispatchEvent({
    type: "keydown",
    code: "KeyW",
    preventDefault() {},
  });
  env.document.dispatchEvent({
    type: "keyup",
    code: "KeyW",
    preventDefault() {},
  });
  await flushAsyncWork();

  const movedCamera = gl.ops.filter((entry) => entry[0] === "uniform4f" && entry[1] === "u_camera").at(-1);
  assert.ok(movedCamera);
  assert.notEqual(movedCamera[4], initialCamera[4]);
  assert.equal(mount.getAttribute("data-gosx-scene3d-controls"), "first-person");
  assert.equal(canvas.getAttribute("tabindex"), "0");
  assert.equal(env.consoleLogs.warn.length, 0);
  assert.equal(env.consoleLogs.error.length, 0);
});
