// Contract tests for the core server-fragment replacement lifecycle.
import test from "node:test";
import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import vm from "node:vm";
import { fileURLToPath } from "node:url";

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const moduleSrc = fs.readFileSync(
  path.join(__dirname, "bootstrap-src", "26-runtime-dom.js"),
  "utf8"
);

test("runtime DOM replacement owns dispose, swap, and enhancement ordering", () => {
  const calls = [];
  const events = [];
  const telemetry = [];
  let html = "<p>old</p>";
  const target = {
    id: "fragment",
    get innerHTML() { return html; },
    set innerHTML(value) { html = value; calls.push("swap"); },
  };
  const body = { id: "body" };
  const document = {
    body,
    documentElement: body,
    dispatchEvent(event) { events.push(event); },
  };
  const window = {
    __gosx: {
      reportIssue() {},
      motion: {
      disposeAll(root) {
        calls.push("dispose-motion");
        assert.equal(root, target);
      },
      mountAll(root) {
        calls.push("mount-motion");
        assert.equal(root, target);
      },
    },
    textLayout: {
      disposeAll(root) {
        calls.push("dispose-text");
        assert.equal(root, target);
      },
      mountAll(root) {
        calls.push("mount-text");
        assert.equal(root, target);
      },
      },
    },
    __gosx_emit(level, category, message, fields) {
      telemetry.push({ level, category, message, fields });
    },
    __gosx_dispose_declarative_regions(root, options) {
      calls.push("dispose-regions");
      assert.equal(root, target);
      assert.equal(options.preserveRoot, true);
    },
    __gosx_dispose_runtime_surfaces(root) {
      calls.push("dispose-surfaces");
      assert.equal(root, target);
    },
    __gosx_mount_stream_templates(root) {
      calls.push("mount-streams");
      assert.equal(root, target);
    },
    __gosx_apply_scene_command_scripts(root) {
      calls.push("apply-scene");
      assert.equal(root, target);
    },
    __gosx_mount_runtime_surfaces(root) {
      calls.push("mount-surfaces");
      assert.equal(root, target);
    },
    __gosx_mount_declarative_regions(root) {
      calls.push("mount-regions");
      assert.equal(root, target);
    },
  };
  class CustomEvent {
    constructor(type, init = {}) {
      this.type = type;
      this.detail = init.detail;
    }
  }
  const context = { window, document, CustomEvent, AbortController, console };
  vm.createContext(context);
  vm.runInContext(moduleSrc, context);

  const replaced = context.window.__gosx_replace_runtime_content(target, "<p>new</p>");
  assert.equal(replaced, true);
  assert.equal(html, "<p>new</p>");
  assert.deepEqual(calls, [
    "dispose-motion",
    "dispose-text",
    "dispose-regions",
    "dispose-surfaces",
    "swap",
    "mount-streams",
    "apply-scene",
    "mount-motion",
    "mount-text",
    "mount-surfaces",
    "mount-regions",
  ]);
  assert.equal(events.map((event) => event.type).join(","), "gosx:runtime:content:before,gosx:runtime:content:after");
  assert.equal(telemetry.at(-1).category, "runtime-dom");
  assert.equal(context.window.__gosx.dom.replace, context.window.__gosx_replace_runtime_content);
  assert.equal(typeof context.window.__gosx.dom.reconcile, "function");
});

test("runtime DOM reconciliation brackets product updates with lifecycle cleanup", () => {
  const calls = [];
  const events = [];
  const target = { id: "preview" };
  const body = { id: "body" };
  const document = {
    body,
    documentElement: body,
    dispatchEvent(event) { events.push(event); },
  };
  const window = {
    __gosx: {},
    __gosx_dispose_runtime_surfaces(root) {
      assert.equal(root, target);
      calls.push("dispose");
    },
    __gosx_mount_runtime_surfaces(root) {
      assert.equal(root, target);
      calls.push("mount");
    },
  };
  class CustomEvent {
    constructor(type, init = {}) {
      this.type = type;
      this.detail = init.detail;
    }
  }
  const context = { window, document, CustomEvent, AbortController, console };
  vm.createContext(context);
  vm.runInContext(moduleSrc, context);

  const result = context.window.__gosx.dom.reconcile(target, (root) => {
    assert.equal(root, target);
    calls.push("update");
    return { firstChanged: 2, changed: true };
  });

  assert.deepEqual(calls, ["dispose", "update", "mount"]);
  assert.equal(result.firstChanged, 2);
  assert.equal(result.changed, true);
  assert.equal(result.lifecycle, "mounted");
  assert.equal(events.map((event) => event.type).join(","), "gosx:runtime:content:before,gosx:runtime:content:after");
});

test("runtime DOM fragment replacement disposes the marker and mounts every inserted element", () => {
  const calls = [];
  const events = [];
  const inserted = { nodeType: 1, id: "resolved" };
  const fragment = { childNodes: [inserted] };
  const target = { id: "slot" };
  const parent = {
    replaceChild(next, previous) {
      assert.equal(next, fragment);
      assert.equal(previous, target);
      calls.push("swap");
    },
  };
  target.parentNode = parent;
  const document = {
    body: { id: "body" },
    documentElement: { id: "body" },
    dispatchEvent(event) { events.push(event); },
  };
  const window = {
    __gosx: {
      motion: {
        disposeAll(root) { assert.equal(root, target); calls.push("dispose-motion"); },
        mountAll(root) { assert.equal(root, inserted); calls.push("mount-motion"); },
      },
      textLayout: {
        disposeAll(root) { assert.equal(root, target); calls.push("dispose-text"); },
        mountAll(root) { assert.equal(root, inserted); calls.push("mount-text"); },
      },
    },
    __gosx_dispose_declarative_regions(root, options) {
      assert.equal(root, target);
      assert.equal(options.preserveRoot, false);
      calls.push("dispose-regions");
    },
    __gosx_dispose_runtime_surfaces(root) {
      assert.equal(root, target);
      calls.push("dispose-surfaces");
    },
    __gosx_mount_runtime_surfaces(root) {
      assert.equal(root, inserted);
      calls.push("mount-surfaces");
    },
    __gosx_mount_declarative_regions(root) {
      assert.equal(root, inserted);
      calls.push("mount-regions");
    },
  };
  class CustomEvent {
    constructor(type, init = {}) {
      this.type = type;
      this.detail = init.detail;
    }
  }
  const context = { window, document, CustomEvent, AbortController, console };
  vm.createContext(context);
  vm.runInContext(moduleSrc, context);

  assert.equal(context.window.__gosx.dom.replaceFragment(target, fragment), true);
  assert.deepEqual(calls, [
    "dispose-motion",
    "dispose-text",
    "dispose-regions",
    "dispose-surfaces",
    "swap",
    "mount-motion",
    "mount-text",
    "mount-surfaces",
    "mount-regions",
  ]);
  assert.equal(events.map((event) => event.type).join(","), "gosx:runtime:content:before,gosx:runtime:content:after");
  assert.equal(context.window.__gosx_replace_runtime_fragment, context.window.__gosx.dom.replaceFragment);
});

test("scoped DOM bridge owns queries, events, dispatch, and parent abort cleanup", () => {
  const removed = [];
  const dispatched = [];
  const listeners = new Map();
  const target = {
    ownerDocument: {
      dispatchEvent(event) { dispatched.push(event); },
    },
    addEventListener(type, listener, options) {
      listeners.set(type, { listener, options });
    },
    removeEventListener(type, listener) {
      removed.push(type);
      if (listeners.get(type)?.listener === listener) listeners.delete(type);
    },
    querySelector(selector) { return selector === ".child" ? "child" : null; },
    querySelectorAll(selector) { return selector === ".child" ? ["child", "other"] : []; },
    contains(node) { return node === "child"; },
  };
  const body = { id: "body" };
  const document = {
    body,
    documentElement: body,
    dispatchEvent(event) { dispatched.push(event); },
  };
  const window = { __gosx: {} };
  class CustomEvent {
    constructor(type, init = {}) {
      this.type = type;
      this.detail = init.detail;
    }
  }
  const context = { window, document, CustomEvent, AbortController, console };
  vm.createContext(context);
  vm.runInContext(moduleSrc, context);

  const parent = new AbortController();
  const scope = context.window.__gosx.dom.scope(target, parent.signal);
  assert.equal(scope.root, target);
  assert.equal(scope.query(".child"), "child");
  assert.equal(Array.from(scope.queryAll(".child")).join(","), "child,other");
  assert.equal(scope.contains("child"), true);
  assert.equal(scope.contains("missing"), false);

  const listener = () => {};
  scope.listen(target, "input", listener);
  assert.equal(listeners.get("input").options.signal, scope.signal);
  scope.dispatch("gosx:test", { value: 1 });
  assert.equal(dispatched[0].type, "gosx:test");
  assert.deepEqual(dispatched[0].detail, { value: 1 });

  parent.abort();
  assert.equal(scope.signal.aborted, true);
  assert.deepEqual(removed, ["input"]);
  assert.equal(listeners.size, 0);
});

test("scoped DOM bridge owns and removes portal nodes outside the surface root", () => {
  const removed = [];
  const portal = {
    parentNode: {
      removeChild(node) {
        removed.push(node);
        node.parentNode = null;
      },
    },
  };
  const target = { id: "surface" };
  const document = {
    body: target,
    documentElement: target,
    dispatchEvent() {},
  };
  const window = { __gosx: {} };
  class CustomEvent {
    constructor(type, init = {}) {
      this.type = type;
      this.detail = init.detail;
    }
  }
  const context = { window, document, CustomEvent, AbortController, console };
  vm.createContext(context);
  vm.runInContext(moduleSrc, context);

  const scope = context.window.__gosx.dom.scope(target);
  const release = scope.portal(portal);
  assert.equal(typeof release, "function");
  release();
  assert.deepEqual(removed, [portal]);

  const secondPortal = {
    parentNode: {
      removeChild(node) {
        removed.push(node);
        node.parentNode = null;
      },
    },
  };
  scope.own(secondPortal);
  scope.dispose();
  assert.deepEqual(removed, [portal, secondPortal]);
});
