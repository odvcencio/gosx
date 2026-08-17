"use strict";
// Viewport response and telemetry: resize, DPR change, context loss and
// restore, the client-event emitter, and visibility-driven render gating.
//
// Split out of the former client/js/runtime.test.js, which this set replaces
// and which no longer exists. Every shared fake, sandbox builder and fixture
// factory lives in ./runtime-test-harness.js.

const test = require("node:test");
const assert = require("node:assert/strict");
const path = require("node:path");

const {
  bootstrapSource,
  FakeWebGLContext,
  FakeElement,
  createContext,
  installManualRAF,
  flushSceneInitialFrameBoundary,
  installManualTimers,
  runScript,
  flushAsyncWork,
  telemetryPostBodies,
  telemetryEvents,
  resolveSceneViewportForTest,
} = require("./runtime-test-harness.js");

test("Scene3D viewport honors minDevicePixelRatio as a sharpness floor after capability selection", () => {
  const viewport = resolveSceneViewportForTest({
    width: 390,
    height: 844,
    minDevicePixelRatio: 1.85,
    maxDevicePixelRatio: 2,
    maxPixels: 2073600,
  }, {
    devicePixelRatio: 1.85,
    capability: { tier: "constrained", lowPower: true },
  });
  assert.equal(viewport.devicePixelRatio, 1.85);
  assert.ok(viewport.pixelWidth >= 720, "mobile backing width must stay at or above 720px");
  assert.ok(viewport.pixelWidth * viewport.pixelHeight <= 2073600, "1080p max-pixel budget must remain a hard cap");
});

test("Scene3D viewport constrains the DPR floor with maxPixels and authored maxDevicePixelRatio", () => {
  const pixelCapped = resolveSceneViewportForTest({
    width: 1000,
    height: 500,
    minDevicePixelRatio: 1.8,
    maxDevicePixelRatio: 2,
    maxPixels: 720000,
  }, { devicePixelRatio: 2 });
  assert.ok(Math.abs(pixelCapped.devicePixelRatio - 1.2) < 0.001);
  assert.equal(pixelCapped.pixelWidth * pixelCapped.pixelHeight, 720000);

  const authoredMaxCapped = resolveSceneViewportForTest({
    width: 390,
    height: 844,
    minDevicePixelRatio: 1.85,
    maxDevicePixelRatio: 1.5,
    maxPixels: 2073600,
  }, { devicePixelRatio: 2 });
  assert.equal(authoredMaxCapped.devicePixelRatio, 1.5);
});

test("Scene3D viewport preserves low-power DPR caps unless an authored floor raises them", () => {
  const capped = resolveSceneViewportForTest({ width: 390, height: 844 }, {
    devicePixelRatio: 3,
    capability: { tier: "constrained", lowPower: true },
  });
  assert.equal(capped.devicePixelRatio, 1.25);

  const floored = resolveSceneViewportForTest({
    width: 390,
    height: 844,
    minDevicePixelRatio: 1.85,
    maxDevicePixelRatio: 2,
  }, {
    devicePixelRatio: 3,
    capability: { tier: "constrained", lowPower: true },
  });
  assert.equal(floored.devicePixelRatio, 1.85);
});

test("bootstrap keeps Scene3D responsive across resize and DPR changes", async () => {
  const mount = new FakeElement("div", null);
  mount.id = "scene-responsive";
  mount.width = 520;

  const env = createContext({
    elements: [mount],
    devicePixelRatio: 1,
    manifest: {
      engines: [
        {
          id: "gosx-engine-responsive",
          component: "GoSXScene3D",
          kind: "surface",
          mountId: "scene-responsive",
          jsExport: "GoSXScene3D",
          props: {
            width: 520,
            height: 320,
            autoRotate: false,
            scene: {
              labels: [
                {
                  id: "center-label",
                  text: "Center label",
                  x: 0,
                  y: 0,
                  z: 0.5,
                  offsetY: 0,
                  maxWidth: 140,
                },
              ],
            },
          },
          capabilities: ["canvas", "animation"],
        },
      ],
    },
  });

  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();

  const canvas = mount.firstElementChild;
  const label = mount.children[1].children[0];
  const initialLeft = label.style["--gosx-scene-label-left"];
  assert.equal(canvas.getAttribute("width"), "520");
  assert.equal(canvas.style.width, "100%");
  assert.equal(mount.getAttribute("data-gosx-scene3d-pixel-ratio"), "1");

  mount.width = 260;
  env.context.devicePixelRatio = 2;
  env.resizeObservers[0].trigger([mount]);
  await flushAsyncWork();

  assert.equal(canvas.getAttribute("width"), "520");
  assert.equal(canvas.getAttribute("height"), "320");
  assert.equal(canvas.style.width, "100%");
  assert.equal(canvas.style.height, "auto");
  assert.equal(mount.getAttribute("data-gosx-scene3d-css-width"), "260");
  assert.equal(mount.getAttribute("data-gosx-scene3d-css-height"), "160");
  assert.equal(mount.getAttribute("data-gosx-scene3d-pixel-ratio"), "2");
  assert.notEqual(label.style["--gosx-scene-label-left"], initialLeft);
});

test("bootstrap prefers canvas Scene3D rendering on constrained coarse-pointer devices", async () => {
  const mount = new FakeElement("div", null);
  mount.id = "scene-constrained-mobile";

  const env = createContext({
    elements: [mount],
    devicePixelRatio: 3,
    deviceMemory: 4,
    hardwareConcurrency: 4,
    enableWebGL: true,
    matchMedia: {
      "(pointer: coarse)": true,
    },
    manifest: {
      engines: [
        {
          id: "gosx-engine-constrained-mobile",
          component: "GoSXScene3D",
          kind: "surface",
          mountId: "scene-constrained-mobile",
          jsExport: "GoSXScene3D",
          props: {
            width: 480,
            height: 300,
            autoRotate: false,
            scene: {
              objects: [
                { kind: "box", width: 1.4, height: 1.1, depth: 1.2, x: 0, y: 0, z: 0, color: "#8de1ff" },
              ],
            },
          },
        },
      ],
    },
  });

  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();

  assert.equal(mount.getAttribute("data-gosx-scene3d-capability-tier"), "constrained");
  assert.equal(mount.getAttribute("data-gosx-scene3d-coarse-pointer"), "true");
  assert.equal(mount.getAttribute("data-gosx-scene3d-low-power"), "true");
  assert.equal(mount.getAttribute("data-gosx-scene3d-webgl-preference"), "avoid");
  assert.equal(mount.getAttribute("data-gosx-scene3d-pixel-ratio"), "1.25");
  assert.equal(mount.getAttribute("data-gosx-scene3d-renderer"), "canvas");
  assert.equal(mount.getAttribute("data-gosx-scene3d-renderer-fallback"), "environment-constrained");
});

test("bootstrap reconfigures Scene3D renderer when environment constraints change", async () => {
  const mount = new FakeElement("div", null);
  mount.id = "scene-capability-reconfigure";

  const env = createContext({
    elements: [mount],
    enableWebGL: true,
    manifest: {
      engines: [
        {
          id: "gosx-engine-capability-reconfigure",
          component: "GoSXScene3D",
          kind: "surface",
          mountId: "scene-capability-reconfigure",
          jsExport: "GoSXScene3D",
          props: {
            width: 480,
            height: 300,
            autoRotate: false,
            scene: {
              objects: [
                { kind: "box", width: 1.4, height: 1.1, depth: 1.2, x: 0, y: 0, z: 0, color: "#8de1ff" },
              ],
            },
          },
        },
      ],
    },
  });

  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();

  assert.equal(mount.getAttribute("data-gosx-scene3d-renderer"), "webgl");
  assert.equal(mount.getAttribute("data-gosx-scene3d-renderer-fallback"), null);

  env.matchMedia("(prefers-reduced-data: reduce)").dispatch(true);
  await flushAsyncWork();

  assert.equal(mount.getAttribute("data-gosx-scene3d-reduced-data"), "true");
  assert.equal(mount.getAttribute("data-gosx-scene3d-webgl-preference"), "avoid");
  assert.equal(mount.getAttribute("data-gosx-scene3d-renderer"), "canvas");
  assert.equal(mount.getAttribute("data-gosx-scene3d-renderer-fallback"), "environment-constrained");

  env.matchMedia("(prefers-reduced-data: reduce)").dispatch(false);
  await flushAsyncWork();

  assert.equal(mount.getAttribute("data-gosx-scene3d-reduced-data"), "false");
  assert.equal(mount.getAttribute("data-gosx-scene3d-webgl-preference"), "prefer");
  assert.equal(mount.getAttribute("data-gosx-scene3d-renderer"), "webgl");
  assert.equal(mount.getAttribute("data-gosx-scene3d-renderer-fallback"), null);
});

test("bootstrap falls back from WebGL and restores Scene3D rendering after context events", async () => {
  const mount = new FakeElement("div", null);
  mount.id = "scene-webgl-fallback";

  const env = createContext({
    elements: [mount],
    enableWebGL: true,
    manifest: {
      engines: [
        {
          id: "gosx-engine-webgl-fallback",
          component: "GoSXScene3D",
          kind: "surface",
          mountId: "scene-webgl-fallback",
          jsExport: "GoSXScene3D",
          props: {
            width: 480,
            height: 300,
            autoRotate: false,
            scene: {
              objects: [
                { kind: "box", width: 1.4, height: 1.1, depth: 1.2, x: 0, y: 0, z: 0, color: "#8de1ff" },
              ],
            },
          },
        },
      ],
    },
  });

  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();

  const canvas = mount.children[0];
  const ctx2d = canvas.getContext("2d");
  let prevented = false;

  assert.equal(mount.getAttribute("data-gosx-scene3d-renderer"), "webgl");
  canvas.dispatchEvent({
    type: "webglcontextlost",
    preventDefault() {
      prevented = true;
    },
  });
  await flushAsyncWork();

  assert.equal(prevented, true);
  assert.equal(mount.getAttribute("data-gosx-scene3d-renderer"), "canvas");
  assert.equal(mount.getAttribute("data-gosx-scene3d-renderer-fallback"), "webgl-context-lost");
  assert.ok(ctx2d.ops.some((entry) => entry[0] === "fillRect"));

  const lostGl = canvas.getContext("webgl");
  canvas._webglContext = null;
  canvas.dispatchEvent({ type: "webglcontextrestored" });
  const restoredGl = canvas.getContext("webgl");

  assert.notEqual(restoredGl, lostGl);
  assert.ok(
    restoredGl.ops.some((entry) => entry[0] === "bufferData" && entry[3] > 0),
    "restored renderer must upload geometry buffers to the new GL context",
  );
  assert.ok(
    restoredGl.ops.some((entry) => entry[0] === "drawArrays" && entry[3] > 0),
    "restored renderer must draw against the new GL context",
  );
  await flushAsyncWork();

  assert.equal(mount.getAttribute("data-gosx-scene3d-renderer"), "webgl");
  assert.equal(mount.getAttribute("data-gosx-scene3d-renderer-fallback"), null);
});

test("bootstrap installs a client-event telemetry emitter that POSTs to /_gosx/client-events", async () => {
  const env = createContext({
    fetchRoutes: {
      "/_gosx/client-events": { status: 204, text: "" },
    },
  });
  env.context.__gosx_telemetry_config = { flushInterval: 0 };

  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();

  assert.equal(typeof env.context.__gosx_emit, "function", "__gosx_emit should be installed");
  assert.equal(typeof env.context.__gosx.telemetry.emit, "function");
  assert.equal(typeof env.context.__gosx.telemetry.flush, "function");
  assert.equal(env.context.__gosx.telemetry.session(), env.context.__gosx_telemetry_session());
  assert.equal(typeof env.context.__gosx.telemetry.snapshot, "function");
  assert.equal(env.context.__gosx.telemetry.enabled, true);

  env.context.__gosx_emit("warn", "test", "hello world", { k: "v" });
  env.context.__gosx_telemetry_flush();
  await flushAsyncWork();

  const events = telemetryEvents(env);
  assert.equal(events.length, 1, "expected one event, got: " + JSON.stringify(events));
  assert.equal(events[0].cat, "test");
  assert.equal(events[0].msg, "hello world");
  assert.equal(events[0].lvl, "warn");
  assert.deepEqual(events[0].fields, { k: "v" });
  assert.ok(events[0].ua, "first batch should include userAgent");

  const bodies = telemetryPostBodies(env);
  assert.ok(bodies[0].sid && bodies[0].sid.startsWith("s_"), "sid should be generated");
});

test("bootstrap telemetry flushes on scheduled timer", async () => {
  const env = createContext({
    fetchRoutes: {
      "/_gosx/client-events": { status: 204, text: "" },
    },
  });
  env.context.__gosx_telemetry_config = { flushInterval: 10 };

  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();

  env.context.__gosx_emit("info", "timer-test", "tick", {});

  await new Promise((resolve) => setTimeout(resolve, 40));
  await flushAsyncWork();

  const events = telemetryEvents(env);
  assert.equal(events.length, 1, "expected one event after timer fired, got: " + JSON.stringify(events));
  assert.equal(events[0].cat, "timer-test");
});

test("bootstrap telemetry drops into no-op when disabled via config", async () => {
  const env = createContext({
    fetchRoutes: {
      "/_gosx/client-events": { status: 204, text: "" },
    },
  });
  env.context.__gosx_telemetry_config = {
    enabled: false,
    flushInterval: 0,
    maxBatch: Infinity,
    maxQueue: Infinity,
  };

  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();

  env.context.__gosx_emit("warn", "x", "should-not-ship", {});
  env.context.__gosx_telemetry_flush();
  await flushAsyncWork();

  assert.equal(telemetryEvents(env).length, 0, "disabled telemetry must not POST");
  assert.equal(env.context.__gosx.telemetry.enabled, false);
  assert.equal(env.context.__gosx.telemetry.session(), "");
  assert.deepEqual(
    Object.assign({}, env.context.__gosx.telemetry.snapshot()),
    {
      enabled: false,
      session: "",
      queueDepth: 0,
      queueCapacity: 200,
      batchCapacity: 20,
      emittedEvents: 0,
      attemptedEvents: 0,
      attemptedBatches: 0,
      dispatchedEvents: 0,
      dispatchedBatches: 0,
      browserAcceptedEvents: 0,
      browserAcceptedBatches: 0,
      serverAcceptedEvents: 0,
      serverAcceptedBatches: 0,
      droppedOverflowEvents: 0,
      droppedSerializationEvents: 0,
      failedEvents: 0,
      failedBatches: 0,
      beaconFailures: 0,
      fetchFailures: 0,
      pendingRequests: 0,
      lastFlushAt: 0,
      lastFlushReason: "",
      lastFailureAt: 0,
      lastFailureReason: "",
    },
  );
});

test("bootstrap telemetry captures uncaught window errors", async () => {
  const env = createContext({
    fetchRoutes: {
      "/_gosx/client-events": { status: 204, text: "" },
    },
  });
  env.context.__gosx_telemetry_config = { flushInterval: 0 };

  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();

  env.context.dispatchEvent({
    type: "error",
    message: "Test uncaught",
    filename: "app.js",
    lineno: 7,
    colno: 3,
    error: { stack: "Error: Test uncaught\n    at app.js:7:3" },
  });
  env.context.__gosx_telemetry_flush();
  await flushAsyncWork();

  const events = telemetryEvents(env);
  assert.equal(events.length, 1);
  assert.equal(events[0].cat, "runtime");
  assert.equal(events[0].lvl, "error");
  assert.equal(events[0].msg, "Test uncaught");
  assert.equal(events[0].fields.filename, "app.js");
  assert.equal(events[0].fields.lineno, 7);
});

test("bootstrap scene3d emits telemetry for webgl context-lost and context-restored", async () => {
  const mount = new FakeElement("div", null);
  mount.id = "scene-telemetry-ctx";

  const env = createContext({
    elements: [mount],
    enableWebGL: true,
    fetchRoutes: {
      "/_gosx/client-events": { status: 204, text: "" },
    },
    manifest: {
      engines: [
        {
          id: "gosx-engine-telemetry-ctx",
          component: "GoSXScene3D",
          kind: "surface",
          mountId: "scene-telemetry-ctx",
          jsExport: "GoSXScene3D",
          props: {
            width: 480,
            height: 300,
            autoRotate: false,
            scene: {
              objects: [
                { kind: "box", width: 1.4, height: 1.1, depth: 1.2, x: 0, y: 0, z: 0, color: "#8de1ff" },
              ],
            },
          },
        },
      ],
    },
  });
  env.context.__gosx_telemetry_config = { flushInterval: 0 };

  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();

  const canvas = mount.children[0];
  canvas.dispatchEvent({ type: "webglcontextlost", preventDefault() {} });
  await flushAsyncWork();
  canvas.dispatchEvent({ type: "webglcontextrestored" });
  await flushAsyncWork();

  env.context.__gosx_telemetry_flush();
  await flushAsyncWork();

  const events = telemetryEvents(env);
  const scene3dMsgs = events.filter((ev) => ev.cat === "scene3d").map((ev) => ev.msg);
  assert.ok(
    scene3dMsgs.some((msg) => msg === "webgl-context-lost"),
    "expected scene3d/webgl-context-lost telemetry, got: " + scene3dMsgs.join(", "),
  );
  assert.ok(
    scene3dMsgs.some((msg) => msg === "webgl-context-restored"),
    "expected scene3d/webgl-context-restored telemetry, got: " + scene3dMsgs.join(", "),
  );
  const restored = events.find((ev) => ev.cat === "scene3d" && ev.msg === "webgl-context-restored");
  assert.equal(restored && restored.fields && restored.fields.swapped, true, "context-restored should report swapped=true");

  const renderEmpty = events.find((ev) => ev.cat === "scene3d" && ev.msg === "render-empty");
  assert.equal(
    renderEmpty,
    undefined,
    "restored renderer must produce non-empty bundle (render-empty should not fire), got: " + JSON.stringify(renderEmpty),
  );

  const warmup = events.find((ev) => ev.cat === "scene3d" && ev.msg === "renderer-warmup");
  assert.ok(warmup, "renderer-warmup should fire after restore, events: " + JSON.stringify(events));
  assert.equal(warmup.fields.rendererKind, "webgl");
  assert.ok(warmup.fields.bundleMeshObjects >= 0, "warmup reports mesh object count");
});

test("bootstrap keeps hidden Scene3D WebGL contexts alive instead of voluntary loss", async () => {
  const mount = new FakeElement("div", null);
  mount.id = "scene-hidden-webgl";
  let canvas = null;
  let lost = false;
  let loseCalls = 0;
  let restoreCalls = 0;
  const extension = {
    loseContext() {
      loseCalls += 1;
      lost = true;
      canvas.dispatchEvent({ type: "webglcontextlost", preventDefault() {} });
    },
    restoreContext() {
      restoreCalls += 1;
      lost = false;
      canvas._webglContext = null;
    },
  };

  const env = createContext({
    elements: [mount],
    enableWebGL: true,
    disableCanvas2D: true,
    createWebGLContext: () => {
      const gl = new FakeWebGLContext();
      gl.getExtension = (name) => {
        if (name !== "WEBGL_lose_context") {
          return null;
        }
        return lost ? null : extension;
      };
      return gl;
    },
    fetchRoutes: {
      "/_gosx/client-events": { status: 204, text: "" },
    },
    manifest: {
      engines: [
        {
          id: "gosx-engine-hidden-webgl",
          component: "GoSXScene3D",
          kind: "surface",
          mountId: "scene-hidden-webgl",
          jsExport: "GoSXScene3D",
          props: {
            width: 480,
            height: 300,
            autoRotate: true,
            scene: {
              objects: [
                { kind: "box", width: 1.4, height: 1.1, depth: 1.2, x: 0, y: 0, z: 0, color: "#8de1ff" },
              ],
            },
          },
        },
      ],
    },
  });
  env.context.__gosx_telemetry_config = { flushInterval: 0 };
  const timers = installManualTimers(env.context);
  installManualRAF(env.context);

  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();
  canvas = mount.children[0];

  env.document.visibilityState = "hidden";
  env.document.dispatchEvent({ type: "visibilitychange" });
  await flushAsyncWork();
  assert.equal(timers.runDelay(30000), 0, "hidden scenes should not schedule voluntary WebGL loss");
  await flushAsyncWork();

  assert.equal(loseCalls, 0);
  assert.equal(mount.getAttribute("data-gosx-scene3d-renderer"), "webgl");

  env.document.visibilityState = "visible";
  env.document.dispatchEvent({ type: "visibilitychange" });
  await flushAsyncWork();

  assert.equal(restoreCalls, 0);
  assert.equal(mount.getAttribute("data-gosx-scene3d-renderer"), "webgl");
  env.context.__gosx_telemetry_flush();
  await flushAsyncWork();
  const requested = telemetryEvents(env).find((ev) => ev.cat === "scene3d" && ev.msg === "webgl-voluntary-restore-requested");
  assert.equal(requested, undefined);
});

test("scene3d render-empty does NOT fire on restore when bundle has meshObjects (modern PBR path)", async () => {
  // Regression: the pre-alpha.21 detector only inspected legacy vertex/surface
  // fields on the bundle. If the PBR path populated only meshObjects (no
  // legacy verts), the detector fell through, counted sceneState objects, and
  // fired a FALSE POSITIVE render-empty. After alpha.21 the early-return
  // also considers bundle.meshObjects and bundle.instancedMeshes.
  const mount = new FakeElement("div", null);
  mount.id = "scene-modern-pbr-probe";
  const env = createContext({
    elements: [mount],
    enableWebGL: true,
    fetchRoutes: {
      "/_gosx/client-events": { status: 204, text: "" },
    },
    manifest: {
      engines: [
        {
          id: "gosx-engine-modern-pbr",
          component: "GoSXScene3D",
          kind: "surface",
          mountId: "scene-modern-pbr-probe",
          jsExport: "GoSXScene3D",
          props: {
            width: 480,
            height: 300,
            autoRotate: false,
            scene: {
              objects: [
                { kind: "box", width: 1, height: 1, depth: 1, x: 0, y: 0, z: 0, color: "#fff" },
              ],
            },
          },
        },
      ],
    },
  });
  env.context.__gosx_telemetry_config = { flushInterval: 0 };
  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();

  // Simulate the PBR-only bundle shape by stripping the legacy vertex fields
  // from every bundle the runtime hands the renderer. The bundle will still
  // carry meshObjects (populated from sceneState.objects). Post-restore the
  // detector must treat that as "geometry is present" and skip render-empty.
  const api = env.context.__gosx_scene3d_api;
  const origCreateBundle = api.createSceneRenderBundle;
  api.createSceneRenderBundle = function (...args) {
    const bundle = origCreateBundle.apply(this, args);
    return Object.assign({}, bundle, {
      vertexCount: 0,
      worldVertexCount: 0,
      surfaces: [],
      meshObjects: bundle.meshObjects && bundle.meshObjects.length > 0
        ? bundle.meshObjects
        : [{ id: "synthetic-pbr-box", material: "pbr", transform: null, geometry: { vertexCount: 36 } }],
    });
  };

  const canvas = mount.children[0];
  canvas.dispatchEvent({ type: "webglcontextlost", preventDefault() {} });
  await flushAsyncWork();
  canvas.dispatchEvent({ type: "webglcontextrestored" });
  await flushAsyncWork();

  env.context.__gosx_telemetry_flush();
  await flushAsyncWork();

  const events = telemetryEvents(env);
  const renderEmpty = events.find((ev) => ev.cat === "scene3d" && ev.msg === "render-empty");
  assert.equal(
    renderEmpty,
    undefined,
    "render-empty must not fire when bundle has meshObjects (modern PBR), got: " + JSON.stringify(renderEmpty),
  );
  const warmup = events.find((ev) => ev.cat === "scene3d" && ev.msg === "renderer-warmup");
  assert.ok(warmup, "renderer-warmup should fire after restore, events: " + JSON.stringify(events));
  assert.equal(warmup.fields.rendererKind, "webgl");
});

test("bootstrap telemetry flushes via sendBeacon on visibility hidden", async () => {
  const env = createContext({
    fetchRoutes: {
      "/_gosx/client-events": { status: 204, text: "" },
    },
  });
  const beaconCalls = [];
  env.context.navigator.sendBeacon = function (url, body) {
    beaconCalls.push({ url, body });
    return true;
  };
  env.context.__gosx_telemetry_config = { flushInterval: 30000 };
  const timers = installManualTimers(env.context);

  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();

  env.context.__gosx_emit("info", "visibility-test", "bye", {});
  assert.equal(timers.count(), 1, "telemetry emit should schedule one delayed flush");
  env.document.visibilityState = "hidden";
  env.document.dispatchEvent({ type: "visibilitychange" });
  await flushAsyncWork();

  assert.equal(timers.count(), 0, "visibility beacon flush should clear the delayed flush timer");
  assert.equal(beaconCalls.length, 1, "expected one beacon call, got: " + JSON.stringify(beaconCalls));
  assert.equal(beaconCalls[0].url, "/_gosx/client-events");
  const parsed = JSON.parse(beaconCalls[0].body);
  assert.equal(parsed.events[0].cat, "visibility-test");
  assert.equal(telemetryPostBodies(env).length, 0, "should prefer beacon over fetch when available");
});

test("bootstrap telemetry drains every queued batch via beacon when visibility becomes hidden", async () => {
  const env = createContext({
    fetchRoutes: {
      "/_gosx/client-events": { status: 204, text: "" },
    },
  });
  const beaconBodies = [];
  env.context.navigator.sendBeacon = function (_url, body) {
    beaconBodies.push(JSON.parse(body));
    return true;
  };
  env.context.__gosx_telemetry_config = {
    flushInterval: 30000,
    maxBatch: 2.9,
    maxQueue: 5.9,
  };
  const timers = installManualTimers(env.context);

  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();

  for (let i = 0; i < 5; i += 1) {
    env.context.__gosx_emit("info", "hidden-drain", "event-" + i, { i });
  }
  assert.equal(timers.count(), 1);

  env.document.visibilityState = "hidden";
  env.document.dispatchEvent({ type: "visibilitychange" });
  await flushAsyncWork();

  assert.equal(timers.count(), 0);
  assert.deepEqual(beaconBodies.map((body) => body.events.length), [2, 2, 1]);
  assert.deepEqual(
    beaconBodies.flatMap((body) => body.events.map((event) => event.msg)),
    ["event-0", "event-1", "event-2", "event-3", "event-4"],
  );
  const snapshot = env.context.__gosx.telemetry.snapshot();
  assert.equal(snapshot.queueDepth, 0);
  assert.equal(snapshot.queueCapacity, 5);
  assert.equal(snapshot.batchCapacity, 2);
  assert.equal(snapshot.attemptedEvents, 5);
  assert.equal(snapshot.attemptedBatches, 3);
  assert.equal(snapshot.dispatchedEvents, 5);
  assert.equal(snapshot.browserAcceptedEvents, 5);
  assert.equal(snapshot.browserAcceptedBatches, 3);
  assert.equal(snapshot.serverAcceptedEvents, 0, "beacon acceptance must not claim server delivery");
  assert.equal(snapshot.failedEvents, 0);
  assert.equal(snapshot.lastFlushReason, "visibility-hidden");
});

test("bootstrap telemetry reports queue overflow and returns immutable snapshots", async () => {
  const env = createContext({
    fetchRoutes: {
      "/_gosx/client-events": { status: 204, text: "" },
    },
  });
  env.context.__gosx_telemetry_config = {
    flushInterval: 30000,
    maxBatch: 2,
    maxQueue: 3,
  };
  installManualTimers(env.context);

  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();

  for (let i = 0; i < 5; i += 1) {
    env.context.__gosx_emit("info", "overflow", "event-" + i, {});
  }

  const snapshot = env.context.__gosx.telemetry.snapshot();
  assert.equal(Object.isFrozen(snapshot), true);
  assert.equal(snapshot.enabled, true);
  assert.equal(snapshot.session, env.context.__gosx.telemetry.session());
  assert.equal(snapshot.queueDepth, 3);
  assert.equal(snapshot.queueCapacity, 3);
  assert.equal(snapshot.batchCapacity, 2);
  assert.equal(snapshot.emittedEvents, 5);
  assert.equal(snapshot.droppedOverflowEvents, 2);
  assert.equal(snapshot.lastFailureReason, "queue-overflow");
  assert.equal(Reflect.set(snapshot, "queueDepth", 999), false);

  const nextSnapshot = env.context.__gosx.telemetry.snapshot();
  assert.notEqual(nextSnapshot, snapshot, "snapshot must not expose a live stats object");
  assert.equal(nextSnapshot.queueDepth, 3);
});

test("bootstrap telemetry records rejected beacons while successful fetch fallback proves server acceptance", async () => {
  const env = createContext({
    fetchRoutes: {
      "/_gosx/client-events": { status: 204, text: "" },
    },
  });
  let beaconCalls = 0;
  env.context.navigator.sendBeacon = function () {
    beaconCalls += 1;
    return false;
  };
  env.context.__gosx_telemetry_config = { flushInterval: 30000 };
  installManualTimers(env.context);

  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();

  env.context.__gosx_emit("warn", "beacon-fallback", "fallback", {});
  env.document.visibilityState = "hidden";
  env.document.dispatchEvent({ type: "visibilitychange" });
  await flushAsyncWork();

  assert.equal(beaconCalls, 1);
  assert.equal(telemetryEvents(env).length, 1);
  const snapshot = env.context.__gosx.telemetry.snapshot();
  assert.equal(snapshot.beaconFailures, 1);
  assert.equal(snapshot.dispatchedEvents, 1);
  assert.equal(snapshot.serverAcceptedEvents, 1);
  assert.equal(snapshot.serverAcceptedBatches, 1);
  assert.equal(snapshot.browserAcceptedEvents, 0);
  assert.equal(snapshot.failedEvents, 0, "successful fallback keeps the logical batch successful");
  assert.equal(snapshot.lastFailureReason, "beacon-rejected");
});

test("bootstrap telemetry records rejected fetch requests and pending request depth", async () => {
  let rejectRequest;
  const env = createContext({
    fetchRoutes: {
      "/_gosx/client-events": () => new Promise((_resolve, reject) => {
        rejectRequest = reject;
      }),
    },
  });
  env.context.__gosx_telemetry_config = { flushInterval: 30000 };
  installManualTimers(env.context);

  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();

  env.context.__gosx_emit("error", "fetch-failure", "network down", {});
  env.context.__gosx_telemetry_flush();
  assert.equal(env.context.__gosx.telemetry.snapshot().pendingRequests, 1);

  rejectRequest(new Error("offline"));
  await flushAsyncWork();

  const snapshot = env.context.__gosx.telemetry.snapshot();
  assert.equal(snapshot.pendingRequests, 0);
  assert.equal(snapshot.attemptedEvents, 1);
  assert.equal(snapshot.dispatchedEvents, 1);
  assert.equal(snapshot.serverAcceptedEvents, 0);
  assert.equal(snapshot.fetchFailures, 1);
  assert.equal(snapshot.failedEvents, 1);
  assert.equal(snapshot.failedBatches, 1);
  assert.equal(snapshot.lastFailureReason, "fetch-rejected");
  assert.ok(snapshot.lastFailureAt > 0);
});

test("bootstrap telemetry drops unserializable batches without throwing or recursively emitting", async () => {
  const env = createContext({
    fetchRoutes: {
      "/_gosx/client-events": { status: 204, text: "" },
    },
  });
  env.context.__gosx_telemetry_config = { flushInterval: 30000 };
  installManualTimers(env.context);

  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();

  const circular = {};
  circular.self = circular;
  env.context.__gosx_emit("error", "serialization", "circular", circular);
  assert.doesNotThrow(() => env.context.__gosx_telemetry_flush());
  await flushAsyncWork();

  const snapshot = env.context.__gosx.telemetry.snapshot();
  assert.equal(snapshot.queueDepth, 0);
  assert.equal(snapshot.emittedEvents, 1);
  assert.equal(snapshot.attemptedEvents, 1);
  assert.equal(snapshot.dispatchedEvents, 0);
  assert.equal(snapshot.droppedSerializationEvents, 1);
  assert.equal(snapshot.failedEvents, 1);
  assert.equal(snapshot.failedBatches, 1);
  assert.equal(snapshot.lastFailureReason, "serialization-error");
  assert.equal(telemetryPostBodies(env).length, 0);
});

test("bootstrap keeps Scene3D static when autoRotate is omitted", async () => {
  const mount = new FakeElement("div", null);
  mount.id = "scene-static-default";

  const env = createContext({
    elements: [mount],
    manifest: {
      engines: [
        {
          id: "gosx-engine-static-default",
          component: "GoSXScene3D",
          kind: "surface",
          mountId: "scene-static-default",
          jsExport: "GoSXScene3D",
          props: {
            width: 480,
            height: 300,
            scene: {
              objects: [
                { kind: "box", width: 1.4, height: 1.1, depth: 1.2, x: 0, y: 0, z: 0, color: "#8de1ff" },
              ],
            },
          },
          capabilities: ["canvas", "animation"],
        },
      ],
    },
  });
  const raf = installManualRAF(env.context);

  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();
  assert.equal(raf.count(), 1, "initial paint boundary should be pending before first Scene3D render");
  await flushSceneInitialFrameBoundary(raf);

  assert.equal(mount.getAttribute("data-gosx-scene3d-mounted"), "true");
  assert.equal(mount.firstElementChild.tagName, "CANVAS");
  assert.equal(raf.count(), 0, "omitted autoRotate should not start a continuous animation loop");
});

test("bootstrap respects prefers-reduced-motion for Scene3D animation loops", async () => {
  const mount = new FakeElement("div", null);
  mount.id = "scene-reduced-motion";

  const env = createContext({
    elements: [mount],
    prefersReducedMotion: true,
    manifest: {
      engines: [
        {
          id: "gosx-engine-reduced-motion",
          component: "GoSXScene3D",
          kind: "surface",
          mountId: "scene-reduced-motion",
          jsExport: "GoSXScene3D",
          props: {
            width: 480,
            height: 300,
            autoRotate: true,
            scene: {
              objects: [
                { kind: "box", width: 1.4, height: 1.1, depth: 1.2, x: 0, y: 0, z: 0, color: "#8de1ff" },
              ],
            },
          },
          capabilities: ["canvas", "animation"],
        },
      ],
    },
  });
  const raf = installManualRAF(env.context);

  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();
  await flushSceneInitialFrameBoundary(raf);

  assert.equal(mount.getAttribute("data-gosx-scene3d-reduced-motion"), "true");
  assert.equal(raf.count(), 0);

  env.matchMedia("(prefers-reduced-motion: reduce)").dispatch(false);
  await flushAsyncWork();

  assert.equal(mount.getAttribute("data-gosx-scene3d-reduced-motion"), "false");
  assert.equal(raf.count(), 1);
});

test("animated Scene3D scroll camera renders immediately on scroll input", async () => {
  const mount = new FakeElement("div", null);
  mount.id = "scene-scroll-camera-active";
  mount.width = 640;
  mount.height = 360;

  const env = createContext({
    elements: [mount],
    enableWebGL: true,
    visualViewport: false,
    manifest: {
      engines: [
        {
          id: "gosx-engine-scroll-camera-active",
          component: "GoSXScene3D",
          kind: "surface",
          mountId: "scene-scroll-camera-active",
          jsExport: "GoSXScene3D",
          props: {
            width: 640,
            height: 360,
            autoRotate: true,
            scrollCameraStart: 10,
            scrollCameraEnd: 4,
            scene: {
              objects: [
                { kind: "box", width: 1.4, height: 1.1, depth: 1.2, x: 0, y: 0, z: 0, color: "#8de1ff" },
              ],
            },
          },
          capabilities: ["canvas", "animation"],
        },
      ],
    },
  });
  env.context.__gosx_scene3d_perf = true;
  env.context.innerHeight = 1000;
  env.document.documentElement.scrollHeight = 2000;
  const raf = installManualRAF(env.context);

  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();
  await flushSceneInitialFrameBoundary(raf);

  const canvas = mount.children[0];
  const gl = canvas.getContext("webgl");
  const cameraZ = () => {
    const calls = gl.ops.filter((entry) => entry[0] === "uniform4f" && entry[1] === "u_camera");
    return calls[calls.length - 1][4];
  };
  assert.equal(cameraZ(), 10);

  env.context.scrollY = 900;
  env.context.dispatchEvent({ type: "scroll" });
  assert.equal(mount.__gosxScene3DScheduleCounts["schedule:scroll"], 1);

  raf.flush(32);
  await flushAsyncWork();

  assert.ok(cameraZ() < 5, "scroll camera should jump near target instead of easing slowly; z=" + cameraZ());
});

test("bootstrap rerenders shared-runtime Scene3D with responsive viewport dimensions", async () => {
  const mount = new FakeElement("div", null);
  mount.id = "scene-runtime-responsive";
  mount.width = 640;
  const renderArgs = [];

  const env = createContext({
    elements: [mount],
    fetchRoutes: {
      "/runtime.wasm": { bytes: [0, 97, 115, 109] },
      "/scene-responsive-runtime.json": { text: '{"name":"ResponsiveScene"}' },
    },
    manifest: {
      runtime: { path: "/runtime.wasm" },
      engines: [
        {
          id: "gosx-engine-runtime-responsive",
          component: "GoSXScene3D",
          kind: "surface",
          mountId: "scene-runtime-responsive",
          runtime: "shared",
          programRef: "/scene-responsive-runtime.json",
          props: {
            width: 640,
            height: 360,
            autoRotate: false,
            background: "#08151f",
          },
        },
      ],
    },
    onHydrateEngine: () => "[]",
    onRenderEngine: (...args) => {
      renderArgs.push(args);
      return JSON.stringify({
        background: "#08151f",
        camera: { x: 0, y: 0, z: 6, fov: 72 },
        positions: [],
        colors: [],
        vertexCount: 0,
        worldPositions: [],
        worldColors: [],
        worldVertexCount: 0,
        objects: [],
        labels: [],
      });
    },
  });

  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();

  assert.deepEqual(renderArgs[0].slice(2, 4), [640, 360]);

  mount.width = 320;
  env.resizeObservers[0].trigger([mount]);
  await flushAsyncWork();

  const last = renderArgs[renderArgs.length - 1];
  assert.deepEqual(last.slice(2, 4), [320, 180]);
});

test("bootstrap rerenders shared-runtime Scene3D on visual viewport scroll changes", async () => {
  const mount = new FakeElement("div", null);
  mount.id = "scene-runtime-viewport-scroll";
  mount.width = 640;
  const renderArgs = [];

  const env = createContext({
    elements: [mount],
    fetchRoutes: {
      "/runtime.wasm": { bytes: [0, 97, 115, 109] },
      "/scene-viewport-scroll.json": { text: '{"name":"ViewportScrollScene"}' },
    },
    manifest: {
      runtime: { path: "/runtime.wasm" },
      engines: [
        {
          id: "gosx-engine-viewport-scroll",
          component: "GoSXScene3D",
          kind: "surface",
          mountId: "scene-runtime-viewport-scroll",
          runtime: "shared",
          programRef: "/scene-viewport-scroll.json",
          props: {
            width: 640,
            height: 360,
            autoRotate: false,
            background: "#08151f",
          },
        },
      ],
    },
    onHydrateEngine: () => "[]",
    onRenderEngine: (...args) => {
      renderArgs.push(args);
      return JSON.stringify({
        background: "#08151f",
        camera: { x: 0, y: 0, z: 6, fov: 72 },
        positions: [],
        colors: [],
        vertexCount: 0,
        worldPositions: [],
        worldColors: [],
        worldVertexCount: 0,
        objects: [],
        labels: [],
      });
    },
  });

  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();

  const initialRenderCount = renderArgs.length;
  assert.equal(initialRenderCount > 0, true);
  assert.equal(env.visualViewport.listenerCount("scroll") >= 1, true);

  env.visualViewport.dispatchEvent({ type: "scroll" });
  await flushAsyncWork();

  assert.equal(renderArgs.length > initialRenderCount, true);
});

test("bootstrap pauses animated Scene3D when the page is hidden and resumes on visibilitychange", async () => {
  const mount = new FakeElement("div", null);
  mount.id = "scene-page-visibility";

  const env = createContext({
    elements: [mount],
    manifest: {
      engines: [
        {
          id: "gosx-engine-page-visibility",
          component: "GoSXScene3D",
          kind: "surface",
          mountId: "scene-page-visibility",
          jsExport: "GoSXScene3D",
          props: {
            width: 480,
            height: 300,
            autoRotate: true,
            scene: {
              objects: [
                { kind: "box", width: 1.6, height: 1.2, depth: 1.2, x: 0, y: 0, z: 0, color: "#8de1ff" },
              ],
            },
          },
          capabilities: ["canvas", "animation"],
        },
      ],
    },
  });
  const raf = installManualRAF(env.context);

  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();
  await flushSceneInitialFrameBoundary(raf);

  assert.equal(mount.getAttribute("data-gosx-scene3d-page-visible"), "true");
  assert.equal(mount.getAttribute("data-gosx-scene3d-active"), "true");
  assert.equal(raf.count(), 1);

  env.document.visibilityState = "hidden";
  env.document.dispatchEvent({ type: "visibilitychange" });
  await flushAsyncWork();

  assert.equal(mount.getAttribute("data-gosx-scene3d-page-visible"), "false");
  assert.equal(mount.getAttribute("data-gosx-scene3d-active"), "false");
  assert.equal(raf.count(), 0);

  env.document.visibilityState = "visible";
  env.document.dispatchEvent({ type: "visibilitychange" });
  await flushAsyncWork();

  assert.equal(mount.getAttribute("data-gosx-scene3d-page-visible"), "true");
  assert.equal(mount.getAttribute("data-gosx-scene3d-active"), "true");
  assert.equal(raf.count(), 1);

  raf.flush(16);
  assert.equal(raf.count(), 1);
});

test("bootstrap defers offscreen shared-runtime Scene3D rerenders until the mount re-enters the viewport", async () => {
  const mount = new FakeElement("div", null);
  mount.id = "scene-intersection-runtime";
  mount.width = 640;
  const renderArgs = [];

  const env = createContext({
    elements: [mount],
    fetchRoutes: {
      "/runtime.wasm": { bytes: [0, 97, 115, 109] },
      "/scene-intersection-runtime.json": { text: '{"name":"IntersectionScene"}' },
    },
    manifest: {
      runtime: { path: "/runtime.wasm" },
      engines: [
        {
          id: "gosx-engine-intersection-runtime",
          component: "GoSXScene3D",
          kind: "surface",
          mountId: "scene-intersection-runtime",
          runtime: "shared",
          programRef: "/scene-intersection-runtime.json",
          props: {
            width: 640,
            height: 360,
            autoRotate: false,
            background: "#08151f",
          },
        },
      ],
    },
    onHydrateEngine: () => "[]",
    onRenderEngine: (...args) => {
      renderArgs.push(args);
      return JSON.stringify({
        background: "#08151f",
        camera: { x: 0, y: 0, z: 6, fov: 72 },
        positions: [],
        colors: [],
        vertexCount: 0,
        worldPositions: [],
        worldColors: [],
        worldVertexCount: 0,
        objects: [],
        labels: [],
      });
    },
  });
  const raf = installManualRAF(env.context);

  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();
  await flushSceneInitialFrameBoundary(raf);

  assert.equal(renderArgs.length, 1);
  assert.equal(env.intersectionObservers.length, 1);
  assert.equal(mount.getAttribute("data-gosx-scene3d-in-viewport"), "true");
  assert.equal(raf.count(), 1);

  env.intersectionObservers[0].trigger([
    { target: mount, isIntersecting: false, intersectionRatio: 0 },
  ]);
  await flushAsyncWork();

  assert.equal(mount.getAttribute("data-gosx-scene3d-in-viewport"), "false");
  assert.equal(mount.getAttribute("data-gosx-scene3d-active"), "false");
  assert.equal(raf.count(), 0);

  mount.width = 320;
  env.resizeObservers[0].trigger([mount]);
  await flushAsyncWork();

  assert.equal(renderArgs.length, 1);

  env.intersectionObservers[0].trigger([
    { target: mount, isIntersecting: true, intersectionRatio: 1 },
  ]);
  await flushAsyncWork();

  assert.equal(mount.getAttribute("data-gosx-scene3d-in-viewport"), "true");
  assert.equal(mount.getAttribute("data-gosx-scene3d-active"), "true");
  assert.equal(raf.count(), 1);

  raf.flush(16);
  await flushAsyncWork();

  const last = renderArgs[renderArgs.length - 1];
  assert.deepEqual(last.slice(2, 4), [320, 180]);
});

test("Scene3D webgl loss recovery rebuilds without a contextrestored event", async () => {
  // Chrome does not guarantee `webglcontextrestored` after an involuntary
  // loss, and in a real browser the Canvas2D stand-in lands on a REPLACEMENT
  // canvas (the original is context-tainted), detaching the only restored
  // listener with it. Before the recovery watchdog either path left the
  // scene degraded forever. The watchdog must rebuild a real WebGL renderer
  // on its own — on a fresh canvas when the original context stays lost —
  // with no restored event ever firing.
  const mount = new FakeElement("div", null);
  mount.id = "scene-webgl-loss-recovery";
  let now = 0;
  const env = createContext({
    elements: [mount],
    enableWebGL: true,
    performanceNow: () => now,
    fetchRoutes: {
      "/_gosx/client-events": { status: 204, text: "" },
    },
    manifest: {
      engines: [
        {
          id: "gosx-engine-webgl-loss-recovery",
          component: "GoSXScene3D",
          kind: "surface",
          mountId: "scene-webgl-loss-recovery",
          jsExport: "GoSXScene3D",
          props: {
            width: 480,
            height: 300,
            autoRotate: false,
            forceWebGL: true,
            scene: {
              objects: [
                { kind: "box", width: 1.4, height: 1.1, depth: 1.2, x: 0, y: 0, z: 0, color: "#8de1ff" },
              ],
            },
          },
        },
      ],
    },
  });
  const timers = installManualTimers(env.context);
  const raf = installManualRAF(env.context);

  runScript(bootstrapSource, env.context, "bootstrap.js");
  timers.runDelay(0);
  await flushAsyncWork();
  await flushSceneInitialFrameBoundary(raf);

  const originalCanvas = mount.children[0];
  assert.equal(mount.getAttribute("data-gosx-scene3d-renderer"), "webgl");
  const lostGL = originalCanvas.getContext("webgl");
  originalCanvas.dispatchEvent({ type: "webglcontextlost", preventDefault() {} });
  await flushAsyncWork();
  assert.equal(mount.getAttribute("data-gosx-scene3d-renderer"), "canvas");

  // The original context never comes back: getContext keeps handing out the
  // same lost context, exactly like a browser that never restores.
  lostGL.isContextLost = () => true;

  // Hidden-tab time must not spend recovery attempts.
  env.context.document.visibilityState = "hidden";
  now = 60000;
  timers.runInterval(2000);
  now = 120000;
  timers.runInterval(2000);
  assert.equal(mount.getAttribute("data-gosx-scene3d-renderer"), "canvas");

  env.context.document.visibilityState = "visible";
  now = 130000;
  timers.runInterval(2000); // arms the eligibility baseline
  assert.equal(mount.getAttribute("data-gosx-scene3d-renderer"), "canvas");
  now = 134100;
  timers.runInterval(2000); // first attempt after the 4s base delay
  await flushAsyncWork();

  assert.equal(mount.getAttribute("data-gosx-scene3d-renderer"), "webgl");
  assert.equal(mount.getAttribute("data-gosx-scene3d-renderer-fallback"), null);
  const recoveredCanvas = mount.children[0];
  assert.notEqual(recoveredCanvas, originalCanvas);
  const recoveredGL = recoveredCanvas.getContext("webgl");
  assert.notEqual(recoveredGL, lostGL);
  assert.ok(
    recoveredGL.ops.some((entry) => entry[0] === "bufferData" && entry[3] > 0),
    "recovered renderer must upload geometry buffers to the fresh GL context",
  );
});
