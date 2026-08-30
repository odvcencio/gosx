"use strict";

const test = require("node:test");
const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");

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

function createSceneEnvironment(onHydrate) {
  const mount = new FakeElement("div", null);
  mount.id = "scene-hydrate-envelope-root";
  const ssr = new FakeElement("span", null);
  ssr.setAttribute("data-ssr-sentinel", "preserve");
  ssr.textContent = "server fallback";
  mount.appendChild(ssr);

  const env = createContext({
    elements: [mount],
    enableWebGL: true,
    disableCanvas2D: true,
    fetchRoutes: {
      "/runtime.wasm": { bytes: [0, 97, 115, 109] },
      "/scene-program.json": { text: '{"name":"HydrateEnvelope"}' },
    },
    manifest: {
      runtime: { path: "/runtime.wasm" },
      engines: [{
        id: targetID,
        component: "GoSXScene3D",
        kind: "surface",
        mountId: mount.id,
        runtime: "shared",
        props: { width: 320, height: 180, background: "#08151f" },
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

test("Scene3D hydrate applies a valid envelope exactly once before publication", async () => {
  let iteratorReads = 0;
  let publishedDuringApply = false;
  let env;
  let mount;
  const commands = new Proxy([createCommand(7)], {
    get(target, property, receiver) {
      if (property === Symbol.iterator) {
        iteratorReads += 1;
        publishedDuringApply = Boolean(
          mount.__gosxScene3DHandle ||
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

  assert.equal(iteratorReads, 1);
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
