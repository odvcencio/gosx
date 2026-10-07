import test from "node:test";
import assert from "node:assert/strict";
import fs from "node:fs";
import vm from "node:vm";
import { createRequire } from "node:module";
const ts = createRequire(new URL("../runtime/package.json", import.meta.url))("typescript");

const inputRuntime = fs.readFileSync(new URL("../runtime/host/controller-input.ts", import.meta.url), "utf8");
const bridge = ts.transpileModule(fs.readFileSync(new URL("../runtime/scene3d/mount-input.ts", import.meta.url), "utf8"), { compilerOptions: { target: ts.ScriptTarget.ES2022 } }).outputText;

test("Scene3D mount projects scaled pointer coordinates and emits scene.Ray-compatible requests", () => {
  const listeners = new Map(), events = [];
  const mount = { addEventListener: (name, fn) => listeners.set(name, fn), removeEventListener: name => listeners.delete(name), dispatchEvent: e => events.push(e) };
  const context = {
    CustomEvent: class { constructor(type, init) { this.type = type; Object.assign(this, init); } },
    scenePickTargetAtEvent(input, canvas, viewport, bundle) {
      assert.equal(input.clientX, 80); assert.equal(canvas.id, "canvas"); assert.equal(viewport().cssWidth, 320); assert.equal(bundle().camera.z, 4);
      return { pointer: { x: 120, y: 60 }, metrics: { width: 320, height: 180 }, target: { object: { id: "end", kind: "box" }, distance: 3, point: { z: 1 }, instanceIndex: 2 } };
    },
    sceneScreenToRay(x, y, width, height, camera) {
      assert.deepEqual([x, y, width, height, camera.z], [120, 60, 320, 180, 4]);
      return { origin: { z: 4 }, dir: { z: -1 } };
    },
  };
  context.window = { __gosx: { host: { controllers: {} } } };
  context.gosxHost = context.window.__gosx.host;
  vm.createContext(context); vm.runInContext(inputRuntime + "\n" + bridge, context);
  const dispose = context.setupSceneControllerPickBridge(mount, { id: "canvas" }, () => ({ cssWidth: 320 }), () => ({ camera: { z: 4 } }));
  const request = listeners.get("gosx:scene3d:pick-request");
  request({ detail: { requestId: "drag:1", clientX: 80, clientY: 40 } });
  const event = JSON.parse(JSON.stringify(events[0]));
  assert.equal(event.type, "gosx:scene3d:input"); assert.equal(event.bubbles, true);
  assert.deepEqual(event.detail.input.ray, { origin: { z: 4 }, direction: { z: -1 } });
  assert.equal(event.detail.input.requestId, "drag:1"); assert.equal(event.detail.input.hit.id, "end"); assert.equal(event.detail.input.hit.instanceIndex, 2);
  request({ detail: { requestId: "bad", clientX: NaN, clientY: 0 } }); assert.equal(events.length, 1);
  dispose(); assert.equal(listeners.size, 0);
});

test("Scene3D pick bridge follows current camera and reports misses after scene replacement", () => {
  const listeners = new Map(), events = []; let bundle = null;
  const context = { CustomEvent: class { constructor(type, init) { Object.assign(this, init); } },
    scenePickTargetAtEvent: () => ({ pointer: { x: 1, y: 2 }, metrics: { width: 2, height: 4 }, target: null }),
    sceneScreenToRay: (x, y, width, height, camera) => ({ origin: { z: camera.z }, dir: { z: -1 } }),
  };
  context.window = { __gosx: { host: { controllers: {} } } };
  context.gosxHost = context.window.__gosx.host;
  vm.createContext(context); vm.runInContext(inputRuntime + "\n" + bridge, context);
  const mount = { addEventListener: (name, fn) => listeners.set(name, fn), removeEventListener() {}, dispatchEvent: e => events.push(e) };
  context.setupSceneControllerPickBridge(mount, {}, () => ({}), () => bundle);
  const request = () => listeners.get("gosx:scene3d:pick-request")({ detail: { requestId: "drag:2", clientX: 1, clientY: 2 } });
  request(); assert.equal(events.length, 0);
  bundle = { camera: { z: 8 } }; request(); assert.equal(events[0].detail.input.ray.origin.z, 8); assert.equal(events[0].detail.input.hit, null);
});

test("canvas picking owns controller bridge disposal across canvas rebinding", () => {
  const listeners = new Set(), picks = [], requests = [];
  const mount = {
    addEventListener: (_name, listener) => listeners.add(listener),
    removeEventListener: (_name, listener) => listeners.delete(listener),
  };
  const context = {
    window: { __gosx: { host: { controllers: { pickScene: (...args) => requests.push(args) } } } },
    scenePickTargetAtEvent() {},
    sceneScreenToRay() {},
    setupScenePickInteractions(canvas, props, readViewport, readBundle, emit, interactive, pointer) {
      assert.equal(listeners.size, 1, "controller listener is installed with canvas picking");
      const pick = {
        getSnapshot: () => ({ canvas }),
        dispose() {
          assert.equal(this, pick, "preserve the pick handle receiver");
          assert.equal(listeners.size, 0, "release the controller listener before canvas picking");
          picks.push(canvas);
        },
      };
      return pick;
    },
  };
  vm.createContext(context);
  vm.runInContext(bridge, context);
  let bundle = { camera: { z: 4 } };
  const install = canvas => context.setupSceneMountPickInteractions(mount, canvas, {}, () => ({}), () => bundle, () => {}, true, null);
  const firstCanvas = { id: "first" }, secondCanvas = { id: "second" };
  const first = install(firstCanvas);
  assert.equal(first.getSnapshot().canvas, firstCanvas);
  first.dispose();
  const second = install(secondCanvas);
  bundle = { camera: { z: 8 } };
  for (const listener of listeners) listener({ detail: { requestId: "rebound" } });
  assert.equal(requests.length, 1);
  assert.equal(requests[0][2], secondCanvas);
  assert.equal(requests[0][4](), bundle, "rebound requests read current scene state");
  second.dispose();
  assert.equal(listeners.size, 0);
  assert.deepEqual(picks, [firstCanvas, secondCanvas]);
});
