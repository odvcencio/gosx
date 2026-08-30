"use strict";

const test = require("node:test");
const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const vm = require("node:vm");

const {
  bootstrapSource,
  FakeElement,
  createContext,
  runScript,
  flushAsyncWork,
} = require("./runtime-test-harness.js");

const targetID = "gosx-engine-hydrate-envelope";

function createCommand(objectID) {
  return {
    kind: 0,
    objectId: objectID,
    data: {
      kind: "mesh",
      geometry: "box",
      material: "flat",
      props: { color: "#8de1ff" },
      children: [],
      static: false,
    },
  };
}

function createEnvelope(commands = []) {
  return {
    version: 1,
    surfaceKind: "scene3d",
    outputKind: "scene3d.commands",
    targetId: targetID,
    mode: "initial",
    commands,
  };
}

function createDeferredRoute() {
  let resolve;
  const promise = new Promise((resolvePromise) => {
    resolve = resolvePromise;
  });
  return { promise, resolve };
}

function modelAssetResponse(id) {
  return {
    text: JSON.stringify({
      objects: [{ id, kind: "box", width: 1, height: 1, depth: 1 }],
    }),
  };
}

function createSceneEnvironment(onHydrate, options = {}) {
  const mount = new FakeElement("div", null);
  mount.id = "scene-hydrate-envelope-root";
  const ssr = new FakeElement("span", null);
  ssr.setAttribute("data-ssr-sentinel", "preserve");
  ssr.textContent = "server fallback";
  mount.appendChild(ssr);

  const env = createContext({
    elements: [mount].concat(options.elements || []),
    enableWebGL: true,
    enableWebGPU: Boolean(options.enableWebGPU),
    disableCanvas2D: true,
    fetchRoutes: Object.assign({
      "/runtime.wasm": { bytes: [0, 97, 115, 109] },
      "/scene-program.json": { text: '{"name":"HydrateEnvelope"}' },
    }, options.fetchRoutes || {}),
    manifest: {
      runtime: { path: "/runtime.wasm" },
      engines: [{
        id: targetID,
        component: "GoSXScene3D",
        kind: "surface",
        mountId: mount.id,
        runtime: "shared",
        props: Object.assign({ width: 320, height: 180, background: "#08151f" }, options.props || {}),
        programRef: "/scene-program.json",
      }],
    },
    onHydrate,
    onRenderEngine: () => "",
  });
  return { env, mount, ssr };
}

async function waitFor(predicate, label) {
  for (let attempt = 0; attempt < 40; attempt += 1) {
    if (predicate()) return;
    await flushAsyncWork();
  }
  assert.fail("timed out waiting for " + label);
}

const malformedEnvelopes = [
  ["null", () => null],
  ["string", () => "[]"],
  ["array", () => []],
  ["non-plain object", () => new Date(0)],
  ["custom null-root prototype", () => Object.assign(Object.create(Object.create(null)), createEnvelope())],
  ["missing field", () => {
    const value = createEnvelope();
    delete value.mode;
    return value;
  }],
  ["extra field", () => Object.assign(createEnvelope(), { extra: true })],
  ["non-enumerable extra field", () => {
    const value = createEnvelope();
    Object.defineProperty(value, "hidden", { value: true, enumerable: false });
    return value;
  }],
  ["symbol extra field", () => {
    const value = createEnvelope();
    value[Symbol("hidden")] = true;
    return value;
  }],
  ["non-enumerable required field", () => {
    const value = createEnvelope();
    Object.defineProperty(value, "mode", { value: "initial", enumerable: false });
    return value;
  }],
  ["accessor required field", () => {
    const value = createEnvelope();
    Object.defineProperty(value, "mode", { get() { return "initial"; }, enumerable: true });
    return value;
  }],
  ["substituted top-level field", () => {
    const value = createEnvelope();
    delete value.commands;
    Object.defineProperty(value, "commands", { value: [], enumerable: false });
    value.extra = true;
    return value;
  }],
  ["unknown version", () => Object.assign(createEnvelope(), { version: 2 })],
  ["wrong surface", () => Object.assign(createEnvelope(), { surfaceKind: "canvas2d" })],
  ["wrong output kind", () => Object.assign(createEnvelope(), { outputKind: "scene3d.patch" })],
  ["wrong target", () => Object.assign(createEnvelope(), { targetId: "other" })],
  ["wrong mode", () => Object.assign(createEnvelope(), { mode: "incremental" })],
  ["commands not an array", () => Object.assign(createEnvelope(), { commands: {} })],
  ["command not an object", () => createEnvelope([null])],
  ["unknown command kind", () => createEnvelope([{ kind: 7, objectId: 1, data: {} }])],
  ["fractional command kind", () => createEnvelope([{ kind: 2.5, objectId: 1, data: {} }])],
  ["negative object id", () => createEnvelope([{ kind: 2, objectId: -1, data: {} }])],
  ["fractional object id", () => createEnvelope([{ kind: 2, objectId: 1.5, data: {} }])],
  ["unexpected command field", () => createEnvelope([{ kind: 2, objectId: 1, data: {}, extra: true }])],
  ["missing command data", () => createEnvelope([{ kind: 2, objectId: 1 }])],
  ["remove command data", () => createEnvelope([{ kind: 1, objectId: 1, data: {} }])],
  ["non-object command data", () => createEnvelope([{ kind: 2, objectId: 1, data: [] }])],
  ["invalid particle data", () => createEnvelope([{ kind: 6, objectId: 1, data: [null] }])],
  ["invalid create data", () => createEnvelope([{
    kind: 0,
    objectId: 1,
    data: { kind: "mesh", geometry: "box", material: "flat", props: {}, children: [] },
  }])],
];

for (const [name, makeEnvelope] of malformedEnvelopes) {
  test("Scene3D hydrate rejects " + name + " before mount mutation", async () => {
    const { env, mount, ssr } = createSceneEnvironment(() => makeEnvelope());

    runScript(bootstrapSource, env.context, "bootstrap.js");
    await waitFor(() => env.engineDisposeCalls.length === 1, "failed hydrate disposal");

    assert.equal(env.hydrateCalls.length, 1);
    assert.deepEqual(env.hydrateCalls[0].slice(0, 3), [
      "scene3d",
      targetID,
      "GoSXScene3D",
    ]);
    assert.deepEqual(env.engineDisposeCalls, [[targetID]]);
    assert.equal(env.context.__gosx.engines.size, 0);
    assert.strictEqual(mount.firstChild, ssr);
    assert.equal(mount.childNodes.length, 1);
    assert.equal(mount.__gosxScene3DState, undefined);
    assert.equal(mount.__gosxScene3DHandle, undefined);
    assert.equal(mount.querySelector("canvas"), null);
  });
}

test("Scene3D hydrate shape validation never invokes an expected-field accessor", async () => {
  let getterReads = 0;
  const value = createEnvelope();
  Object.defineProperty(value, "mode", {
    get() {
      getterReads += 1;
      return "initial";
    },
    enumerable: true,
  });
  const { env, mount, ssr } = createSceneEnvironment(() => value);

  runScript(bootstrapSource, env.context, "bootstrap.js");
  await waitFor(() => env.engineDisposeCalls.length === 1, "accessor envelope disposal");

  assert.equal(getterReads, 0);
  assert.strictEqual(mount.firstChild, ssr);
  assert.equal(mount.__gosxScene3DState, undefined);
  assert.equal(mount.__gosxScene3DHandle, undefined);
});

test("Scene3D hydrate rejects a custom prototype forged with Object constructor", async () => {
  const prototype = Object.create(null);
  Object.defineProperty(prototype, "constructor", { value: Object });
  Object.defineProperty(prototype, "polluted", { value: true, enumerable: true });
  const value = Object.assign(Object.create(prototype), createEnvelope());
  const { env, mount, ssr } = createSceneEnvironment(() => value);

  runScript(bootstrapSource, env.context, "bootstrap.js");
  await waitFor(
    () => env.engineDisposeCalls.length === 1 || env.context.__gosx.engines.has(targetID),
    "forged prototype disposition",
  );
  const accepted = env.context.__gosx.engines.has(targetID);
  if (accepted) env.context.__gosx_dispose_engine(targetID);

  assert.equal(accepted, false);
  assert.strictEqual(mount.firstChild, ssr);
  assert.equal(mount.__gosxScene3DState, undefined);
  assert.equal(mount.__gosxScene3DHandle, undefined);
});

const nestedAccessorEnvelopes = [
  ["command array index", () => {
    let getterReads = 0;
    const commands = [];
    Object.defineProperty(commands, "0", {
      get() {
        getterReads += 1;
        return createCommand(1);
      },
      enumerable: true,
    });
    return { value: createEnvelope(commands), getterReads: () => getterReads };
  }],
  ["command array custom iterator", () => {
    let iteratorCalls = 0;
    const commands = [createCommand(1)];
    Object.defineProperty(commands, Symbol.iterator, {
      value: function* () {
        iteratorCalls += 1;
        yield { kind: 7, objectId: 1, data: {} };
      },
    });
    return { value: createEnvelope(commands), getterReads: () => iteratorCalls };
  }],
  ["coercible command kind", () => {
    let getterReads = 0;
    const kind = {};
    Object.defineProperty(kind, Symbol.toPrimitive, {
      get() {
        getterReads += 1;
        return () => 0;
      },
    });
    return {
      value: createEnvelope([{ kind, objectId: 1, data: createCommand(1).data }]),
      getterReads: () => getterReads,
    };
  }],
  ["prototype constructor", () => {
    let getterReads = 0;
    const prototype = Object.create(null);
    Object.defineProperty(prototype, "constructor", {
      get() {
        getterReads += 1;
        return Object;
      },
      enumerable: false,
    });
    Object.defineProperty(prototype, "polluted", { value: true, enumerable: true });
    return {
      value: Object.assign(Object.create(prototype), createEnvelope()),
      getterReads: () => getterReads,
    };
  }],
  ["command kind", () => {
    let getterReads = 0;
    const command = { objectId: 1, data: {} };
    Object.defineProperty(command, "kind", {
      get() {
        getterReads += 1;
        return 2;
      },
      enumerable: true,
    });
    return { value: createEnvelope([command]), getterReads: () => getterReads };
  }],
  ["command object id", () => {
    let getterReads = 0;
    const command = { kind: 2, data: {} };
    Object.defineProperty(command, "objectId", {
      get() {
        getterReads += 1;
        return 1;
      },
      enumerable: true,
    });
    return { value: createEnvelope([command]), getterReads: () => getterReads };
  }],
  ["command data", () => {
    let getterReads = 0;
    const command = { kind: 2, objectId: 1 };
    Object.defineProperty(command, "data", {
      get() {
        getterReads += 1;
        return {};
      },
      enumerable: true,
    });
    return { value: createEnvelope([command]), getterReads: () => getterReads };
  }],
  ["create payload", () => {
    let getterReads = 0;
    const data = {
      geometry: "box",
      material: "flat",
      props: {},
      children: [],
      static: false,
    };
    Object.defineProperty(data, "kind", {
      get() {
        getterReads += 1;
        return "mesh";
      },
      enumerable: true,
    });
    return {
      value: createEnvelope([{ kind: 0, objectId: 1, data }]),
      getterReads: () => getterReads,
    };
  }],
];

for (const [name, makeEnvelope] of nestedAccessorEnvelopes) {
  test("Scene3D hydrate rejects " + name + " accessors without invocation", async () => {
    const probe = makeEnvelope();
    const { env, mount, ssr } = createSceneEnvironment(() => probe.value);

    runScript(bootstrapSource, env.context, "bootstrap.js");
    await waitFor(
      () => env.engineDisposeCalls.length === 1 || env.context.__gosx.engines.has(targetID),
      name + " accessor disposition",
    );
    const accepted = env.context.__gosx.engines.has(targetID);
    if (accepted) env.context.__gosx_dispose_engine(targetID);

    assert.equal(probe.getterReads(), 0);
    assert.equal(accepted, false);
    assert.strictEqual(mount.firstChild, ssr);
    assert.equal(mount.__gosxScene3DState, undefined);
    assert.equal(mount.__gosxScene3DHandle, undefined);
  });
}

test("Scene3D hydrate accepts a genuine cross-realm JSON envelope", async () => {
  const serialized = JSON.stringify(createEnvelope([createCommand(33)]));
  const value = vm.runInNewContext("JSON.parse(" + JSON.stringify(serialized) + ")");
  const { env, mount } = createSceneEnvironment(() => value);

  runScript(bootstrapSource, env.context, "bootstrap.js");
  await waitFor(
    () => env.context.__gosx.engines.has(targetID) && mount.__gosxScene3DState,
    "cross-realm Scene3D handle",
  );

  assert.equal(mount.__gosxScene3DState.objects.has("33"), true);
  assert.equal(mount.__gosxScene3DHandle.__gosxScene3DCommandReady, true);
  env.context.__gosx_dispose_engine(targetID);
});

test("Scene3D hydrate applies a descriptor snapshot without consuming the original iterable", async () => {
  let iteratorReads = 0;
  let publishedDuringApply = false;
  let env;
  let mount;
  const commands = new Proxy([createCommand(7)], {
    get(target, property, receiver) {
      if (property === Symbol.iterator) {
        iteratorReads += 1;
        publishedDuringApply = Boolean(
          mount.__gosxScene3DState || mount.__gosxScene3DHandle ||
          (env.context.__gosx.engines && env.context.__gosx.engines.has(targetID)),
        );
      }
      return Reflect.get(target, property, receiver);
    },
  });
  ({ env, mount } = createSceneEnvironment(() => createEnvelope(commands)));

  runScript(bootstrapSource, env.context, "bootstrap.js");
  await waitFor(
    () => env.context.__gosx.engines.has(targetID) && mount.__gosxScene3DState,
    "published Scene3D handle",
  );

  assert.equal(iteratorReads, 0);
  assert.equal(publishedDuringApply, false);
  assert.equal(env.hydrateCalls.length, 1);
  assert.equal(env.engineHydrateCalls.length, 0);
  assert.deepEqual(env.hydrateCalls[0], [
    "scene3d",
    targetID,
    "GoSXScene3D",
    '{"width":320,"height":180,"background":"#08151f"}',
    '{"name":"HydrateEnvelope"}',
    "json",
  ]);
  assert.equal(mount.__gosxScene3DState.objects.has("7"), true);
  assert.equal(mount.__gosxScene3DState.objects.has(7), false);
  assert.equal(mount.__gosxScene3DHandle.__gosxScene3DCommandReady, true);
});

test("a stale blocked hydrate cannot apply or publish after a same-id winner", async () => {
  let resolveFirst;
  let hydrateCount = 0;
  const firstEnvelope = createEnvelope([createCommand(11)]);
  const secondEnvelope = createEnvelope([createCommand(22)]);
  const { env, mount } = createSceneEnvironment(() => {
    hydrateCount += 1;
    if (hydrateCount === 1) {
      return new Promise((resolve) => {
        resolveFirst = resolve;
      });
    }
    return secondEnvelope;
  });

  runScript(bootstrapSource, env.context, "bootstrap.js");
  await waitFor(() => env.hydrateCalls.length === 1, "first blocked hydrate");
  assert.equal(mount.__gosxScene3DState, undefined);

  env.context.__gosx_runtime_ready();
  await waitFor(
    () => env.hydrateCalls.length === 2 && env.context.__gosx.engines.has(targetID),
    "replacement hydrate",
  );
  assert.equal(mount.__gosxScene3DState.objects.has("22"), true);
  assert.equal(mount.__gosxScene3DState.objects.has("11"), false);
  assert.deepEqual(env.engineDisposeCalls, [[targetID]]);

  resolveFirst(firstEnvelope);
  await flushAsyncWork();

  assert.equal(env.context.__gosx.engines.size, 1);
  assert.equal(env.context.__gosx.engines.has(targetID), true);
  assert.equal(mount.__gosxScene3DState.objects.has("22"), true);
  assert.equal(mount.__gosxScene3DState.objects.has("11"), false);
  assert.deepEqual(env.engineDisposeCalls, [[targetID]]);
});

test("a post-hydrate same-id replacement cannot lose winner state to stale cleanup", async () => {
  let env;
  let hydrateCount = 0;
  let mount;
  ({ env, mount } = createSceneEnvironment(() => {
    hydrateCount += 1;
    if (hydrateCount === 1) {
      setTimeout(() => env.context.__gosx_runtime_ready(), 0);
    }
    return createEnvelope([createCommand(hydrateCount === 1 ? 11 : 22)]);
  }, {
    enableWebGPU: true,
    props: { preferWebGPU: true },
  }));

  runScript(bootstrapSource, env.context, "bootstrap.js");
  await waitFor(
    () => hydrateCount === 2 && env.context.__gosx.engines.has(targetID) &&
      mount.__gosxScene3DState && mount.__gosxScene3DHandle,
    "post-hydrate replacement winner",
  );

  const winnerRecord = env.context.__gosx.engines.get(targetID);
  const winnerState = mount.__gosxScene3DState;
  const winnerHandle = mount.__gosxScene3DHandle;
  const winnerCanvas = mount.querySelector("canvas");
  await flushAsyncWork();
  await flushAsyncWork();

  assert.equal(hydrateCount, 2);
  assert.equal(env.context.__gosx.engines.size, 1);
  assert.strictEqual(env.context.__gosx.engines.get(targetID), winnerRecord);
  assert.strictEqual(mount.__gosxScene3DState, winnerState);
  assert.strictEqual(mount.__gosxScene3DHandle, winnerHandle);
  assert.strictEqual(mount.querySelector("canvas"), winnerCanvas);
  assert.strictEqual(winnerRecord.handle, winnerHandle);
  assert.equal(winnerState.objects.has("22"), true);
  assert.equal(winnerState.objects.has("11"), false);
  assert.deepEqual(env.engineDisposeCalls, [[targetID]]);
});

test("a stale post-owner model settlement preserves winner debug and inspector ownership", async () => {
  const modelURL = "/models/deferred-owner.gosx3d.json";
  const route = createDeferredRoute();
  const controlForm = new FakeElement("form", null);
  controlForm.setAttribute("data-gosx-scene3d-control-form", "fluid-object");
  let hydrateCount = 0;
  const { env, mount } = createSceneEnvironment(() => {
    hydrateCount += 1;
    return createEnvelope([createCommand(hydrateCount === 1 ? 11 : 22)]);
  }, {
    props: {
      inspector: true,
      scene: {
        objects: [],
        models: [{ id: "deferred", src: modelURL }],
      },
    },
    fetchRoutes: {
      [modelURL]: () => route.promise,
    },
    elements: [controlForm],
  });

  let routeResolved = false;
  try {
    runScript(bootstrapSource, env.context, "bootstrap.js");
    await waitFor(
      () => env.fetchCalls.filter((call) => call.url === modelURL).length === 1 &&
        env.context.__gosx_scene3d_debug_registry &&
        env.context.__gosx_scene3d_debug_registry.size === 1,
      "first post-owner model hydration",
    );
    const firstDebugRecord = env.context.__gosx_scene3d_debug_registry.get(targetID);
    assert.ok(firstDebugRecord);
    assert.equal(mount.getAttribute("data-gosx-scene3d-inspector-enabled"), "true");

    env.context.__gosx_runtime_ready();
    await waitFor(
      () => hydrateCount === 2 &&
        env.context.__gosx_scene3d_debug_registry.get(targetID) !== firstDebugRecord,
      "replacement post-owner model hydration",
    );
    const winnerDebugRecord = env.context.__gosx_scene3d_debug_registry.get(targetID);
    const pendingInspectors = mount.querySelectorAll("[data-gosx-scene3d-inspector]");
    const winnerInspector = pendingInspectors[pendingInspectors.length - 1];
    const pendingCanvases = mount.querySelectorAll("canvas");
    const winnerCanvas = pendingCanvases[pendingCanvases.length - 1];
    const winnerControls = controlForm.__gosxScene3DFluidObjectControls;
    const winnerControlState = controlForm.__gosxScene3DFluidObjectState;
    assert.ok(winnerDebugRecord);
    assert.ok(winnerInspector);
    assert.ok(winnerCanvas);
    assert.ok(winnerControls);
    assert.ok(winnerControlState);

    route.resolve(modelAssetResponse("mesh"));
    routeResolved = true;
    await waitFor(
      () => env.context.__gosx.engines.has(targetID) && mount.__gosxScene3DState &&
        mount.__gosxScene3DState.objects.has("22"),
      "post-owner replacement winner",
    );
    const winnerEngineRecord = env.context.__gosx.engines.get(targetID);
    const winnerState = mount.__gosxScene3DState;
    const winnerHandle = mount.__gosxScene3DHandle;
    await flushAsyncWork();

    assert.equal(hydrateCount, 2);
    assert.strictEqual(env.context.__gosx.engines.get(targetID), winnerEngineRecord);
    assert.strictEqual(winnerEngineRecord.handle, winnerHandle);
    assert.strictEqual(mount.__gosxScene3DState, winnerState);
    assert.strictEqual(mount.__gosxScene3DHandle, winnerHandle);
    assert.strictEqual(mount.querySelector("canvas"), winnerCanvas);
    assert.equal(mount.querySelectorAll("canvas").length, 1);
    assert.strictEqual(env.context.__gosx_scene3d_debug_registry.get(targetID), winnerDebugRecord);
    assert.ok(env.context.__gosx_scene3d_debug.inspect(targetID));
    assert.equal(mount.getAttribute("data-gosx-scene3d-inspector-enabled"), "true");
    assert.strictEqual(mount.querySelector("[data-gosx-scene3d-inspector]"), winnerInspector);
    assert.equal(mount.querySelectorAll("[data-gosx-scene3d-inspector]").length, 1);
    assert.strictEqual(controlForm.__gosxScene3DFluidObjectControls, winnerControls);
    assert.strictEqual(controlForm.__gosxScene3DFluidObjectState, winnerControlState);
    assert.equal(winnerState.objects.has("11"), false);
    assert.equal(winnerState.objects.has("22"), true);
    assert.deepEqual(env.engineDisposeCalls, [[targetID]]);
  } finally {
    if (!routeResolved) {
      route.resolve(modelAssetResponse("mesh"));
      await flushAsyncWork();
    }
    if (env.context.__gosx.engines.has(targetID)) env.context.__gosx_dispose_engine(targetID);
  }
});

test("a reverse-order stale model settlement cannot publish over the same-id winner", async () => {
  const oldURL = "/models/deferred-old-owner.gosx3d.json";
  const winnerURL = "/models/deferred-winner-owner.gosx3d.json";
  const oldRoute = createDeferredRoute();
  const winnerRoute = createDeferredRoute();
  const assetEvents = [];
  const hydrationEvents = [];
  let hydrateCount = 0;
  const { env, mount } = createSceneEnvironment(() => createEnvelope([
    createCommand(++hydrateCount === 1 ? 11 : 22),
  ]), {
    props: {
      scene: {
        objects: [],
        models: [{ id: "old-owner", src: oldURL }],
      },
    },
    fetchRoutes: {
      [oldURL]: () => oldRoute.promise,
      [winnerURL]: () => winnerRoute.promise,
    },
  });
  mount.addEventListener("gosx:scene3d:model-status", (event) => assetEvents.push(event.detail));
  mount.addEventListener("gosx:scene3d:model-hydration-status", (event) => hydrationEvents.push(event.detail));

  let oldResolved = false;
  let winnerResolved = false;
  try {
    runScript(bootstrapSource, env.context, "bootstrap.js");
    await waitFor(
      () => env.fetchCalls.filter((call) => call.url === oldURL).length === 1,
      "old owner model request",
    );
    env.context.__gosx_manifest.value.engines[0].props.scene.models = [
      { id: "winner-owner", src: winnerURL },
    ];
    env.context.__gosx_runtime_ready();
    await waitFor(
      () => env.fetchCalls.filter((call) => call.url === winnerURL).length === 1,
      "winner owner model request",
    );

    winnerRoute.resolve(modelAssetResponse("winner-mesh"));
    winnerResolved = true;
    await waitFor(
      () => env.context.__gosx.engines.has(targetID) && mount.__gosxScene3DState &&
        mount.__gosxScene3DState.objects.has("22") &&
        mount.getAttribute("data-gosx-scene3d-model-asset") === winnerURL,
      "reverse-order replacement winner",
    );
    const winnerState = mount.__gosxScene3DState;
    const winnerHandle = mount.__gosxScene3DHandle;
    const winnerDebug = env.context.__gosx_scene3d_debug_registry.get(targetID);
    const assetEventCount = assetEvents.length;
    const hydrationEventCount = hydrationEvents.length;

    oldRoute.resolve(modelAssetResponse("old-mesh"));
    oldResolved = true;
    await flushAsyncWork();
    await flushAsyncWork();

    assert.equal(hydrateCount, 2);
    assert.strictEqual(mount.__gosxScene3DState, winnerState);
    assert.strictEqual(mount.__gosxScene3DHandle, winnerHandle);
    assert.strictEqual(env.context.__gosx_scene3d_debug_registry.get(targetID), winnerDebug);
    assert.equal(mount.getAttribute("data-gosx-scene3d-model-asset"), winnerURL);
    assert.equal(mount.getAttribute("data-gosx-scene3d-model-id"), "winner-owner");
    assert.equal(assetEvents.length, assetEventCount);
    assert.equal(hydrationEvents.length, hydrationEventCount);
    assert.equal(winnerState.objects.has("11"), false);
    assert.equal(winnerState.objects.has("22"), true);
    assert.deepEqual(env.engineDisposeCalls, [[targetID]]);
  } finally {
    if (!oldResolved) oldRoute.resolve(modelAssetResponse("old-mesh"));
    if (!winnerResolved) winnerRoute.resolve(modelAssetResponse("winner-mesh"));
    await flushAsyncWork();
    if (env.context.__gosx.engines.has(targetID)) env.context.__gosx_dispose_engine(targetID);
  }
});

test("no-code Scene3D rejects before hydrate, canvas creation, or registration", () => {
  const source = fs.readFileSync(
    path.join(__dirname, "bootstrap-src", "26b-feature-engines-prefix.ts"),
    "utf8",
  );
  const start = source.indexOf("async function mountSurfaceKind");
  const end = source.indexOf("function _startCanvasSurfaceRAF", start);
  const mountSurfaceKind = source.slice(start, end);
  const rejection = mountSurfaceKind.indexOf('surfaceKind === "scene3d"');

  assert.notEqual(start, -1);
  assert.notEqual(end, -1);
  assert.notEqual(rejection, -1);
  assert.ok(rejection < mountSurfaceKind.indexOf("window.__gosx_hydrate"));
  assert.ok(rejection < mountSurfaceKind.indexOf("_ensureSurfaceCanvas"));
  assert.ok(rejection < mountSurfaceKind.indexOf("surfaceInstances.set"));
});
