// Contract tests for the generic GoSX runtime-surface registry.
import test from "node:test";
import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import vm from "node:vm";
import { fileURLToPath } from "node:url";

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const moduleSrc = [
  fs.readFileSync(path.join(__dirname, "..", "runtime", "host", "compatibility.ts"), "utf8"),
  fs.readFileSync(path.join(__dirname, "..", "runtime", "host", "facade.ts"), "utf8"),
].join("\n");
const domModuleSrc = fs.readFileSync(
  path.join(__dirname, "..", "runtime", "host", "dom.ts"),
  "utf8"
);

function makeElement(attrs = {}, children = []) {
  const listeners = new Map();
  const el = {
    _attrs: { ...attrs },
    children,
    listeners,
    parentNode: null,
    getAttribute(name) { return Object.prototype.hasOwnProperty.call(this._attrs, name) ? this._attrs[name] : null; },
    hasAttribute(name) { return Object.prototype.hasOwnProperty.call(this._attrs, name); },
    setAttribute(name, value) { this._attrs[name] = String(value); },
    removeAttribute(name) { delete this._attrs[name]; },
    addEventListener(type, listener) { listeners.set(type, listener); },
    removeEventListener(type, listener) { if (listeners.get(type) === listener) listeners.delete(type); },
    querySelectorAll(selector) {
      if (selector !== "[data-gosx-runtime-surface]") return [];
      const out = [];
      const walk = (node) => {
        for (const child of node.children || []) {
          child.parentNode = node;
          if (child.hasAttribute("data-gosx-runtime-surface")) out.push(child);
          walk(child);
        }
      };
      walk(this);
      return out;
    },
    contains(node) {
      if (node === this) return true;
      return (this.children || []).some((child) => child === node || child.contains(node));
    },
  };
  return el;
}

function assertNoUnhandledRejectionDuring(t, work) {
  const unhandled = [];
  const listener = (reason) => unhandled.push(reason);
  process.on("unhandledRejection", listener);
  return Promise.resolve()
    .then(work)
    .then(() => flushAsyncSurfaceWork())
    .finally(() => {
      process.off("unhandledRejection", listener);
      assert.deepEqual(unhandled, []);
    });
}

function runModule(body, options = {}) {
  const events = [];
  const telemetry = [];
  const document = {
    body,
    documentElement: body,
    dispatchEvent(event) { events.push(event); },
    querySelector: options.querySelector || (() => null),
  };
  const window = {
    __gosx: {
      reportIssue() {},
      ...(options.reportFailure ? { reportFailure: options.reportFailure } : {}),
    },
    __gosx_emit(level, category, message, fields) {
      telemetry.push({ level, category, message, fields });
    },
    fetch: options.fetch || (() => Promise.resolve({})),
  };
  class CustomEvent {
    constructor(type, init = {}) {
      this.type = type;
      this.detail = init.detail;
    }
  }
  const context = { window, document, CustomEvent, AbortController, console, setTimeout, clearTimeout };
  vm.createContext(context);
  vm.runInContext(moduleSrc, context);
  if (options.withDOM) vm.runInContext(domModuleSrc, context);
  return { context, events, telemetry };
}

function deferred() {
  let resolve;
  let reject;
  const promise = new Promise((res, rej) => {
    resolve = res;
    reject = rej;
  });
  return { promise, resolve, reject };
}

async function flushAsyncSurfaceWork() {
  await Promise.resolve();
  await new Promise((resolve) => setTimeout(resolve, 0));
}

test("runtime surfaces mount late registrations and expose the GoSX bridge", () => {
  const surface = makeElement({ "data-gosx-runtime-surface": "editor", "data-gosx-runtime-surface-version": "1" });
  const body = makeElement({}, [surface]);
  const { context, events, telemetry } = runModule(body, { withDOM: true });
  let mounted = 0;
  let disposed = 0;
  let receivedContext;

  context.window.__gosx_register_runtime_surface("editor", (surfaceContext) => {
    receivedContext = surfaceContext;
    mounted += 1;
    return { dispose: () => { disposed += 1; } };
  });

  assert.equal(mounted, 1);
  assert.equal(surface.getAttribute("data-gosx-runtime-state"), "ready");
  assert.equal(receivedContext.root, surface);
  assert.equal(receivedContext.name, "editor");
  assert.equal(receivedContext.version, "1");
  assert.equal(receivedContext.namespace, context.window.__gosx);
  assert.equal(typeof receivedContext.fetch, "function");
  assert.equal(typeof receivedContext.request, "function");
  assert.equal(typeof context.window.__gosx.dom.scope, "function");
  assert.equal(typeof context.window.__gosx.transport.scope, "function");
  assert.equal(typeof context.window.__gosx.transport.requestLatest, "function");
  assert.equal(typeof context.window.__gosx.scheduler.scope, "function");
  assert.equal(typeof receivedContext.listen, "function");
  assert.equal(receivedContext.on, receivedContext.listen);
  assert.equal(receivedContext.dom.root, surface);
  assert.equal(typeof receivedContext.dom.portal, "function");
  assert.equal(typeof receivedContext.transport.request, "function");
  assert.equal(typeof receivedContext.scheduler.schedule, "function");
  assert.equal(typeof receivedContext.scheduler.frame, "function");
  assert.equal(typeof receivedContext.reconcile, "function");
  assert.equal(receivedContext.navigation, null);
  assert.equal(receivedContext.diagnostics, null);
  assert.equal(receivedContext.telemetry, null);
  assert.equal(receivedContext.prose, null);
  assert.equal(receivedContext.actions, null);
  assert.equal(receivedContext.regions, null);
  assert.equal(receivedContext.stream, null);
  assert.equal(events.some((event) => event.type === "gosx:runtime-surface:mount"), true);
  assert.equal(telemetry.some((event) => event.message === "runtime surface registered"), true);
  assert.equal(telemetry.some((event) => event.message === "runtime surface mounted"), true);

  context.window.__gosx_dispose_runtime_surfaces(body);
  assert.equal(disposed, 1);
  assert.equal(surface.getAttribute("data-gosx-runtime-state"), "disposed");
  assert.equal(events.some((event) => event.type === "gosx:runtime-surface:unmount"), true);
  assert.equal(telemetry.some((event) => event.message === "runtime surface unmounted"), true);
});

test("surface contexts publish shared runtime services and navigate through core", async () => {
  const surface = makeElement({ "data-gosx-runtime-surface": "editor" });
  const body = makeElement({}, [surface]);
  const navigation = {
    navigate(url, options) {
      return { url, options };
    },
  };
  const diagnostics = { report() {} };
  const telemetryEvents = [];
  const telemetry = { emit(...args) { telemetryEvents.push(args); } };
  const prose = { reconcileHTML() {} };
  const actions = { run() {} };
  const regions = { refresh() {} };
  const stream = { reconcileHTML() {} };
  const { context } = runModule(body, { withDOM: true });
  const issues = [];
  context.window.__gosx.reportIssue = (issue) => {
    issues.push(issue);
    return issue;
  };
  context.window.__gosx.navigation = navigation;
  context.window.__gosx.diagnostics = diagnostics;
  context.window.__gosx.telemetry = telemetry;
  context.window.__gosx.prose = prose;
  context.window.__gosx.actions = actions;
  context.window.__gosx.regions = regions;
  context.window.__gosx.stream = stream;

  let receivedContext;
  context.window.__gosx_register_runtime_surface("editor", (surfaceContext) => {
    receivedContext = surfaceContext;
    return { dispose() {} };
  });

  assert.equal(receivedContext.navigation, navigation);
  assert.equal(receivedContext.diagnostics, diagnostics);
  assert.equal(receivedContext.telemetry, telemetry);
  assert.equal(receivedContext.prose, prose);
  assert.equal(receivedContext.actions, actions);
  assert.equal(receivedContext.regions, regions);
  assert.equal(receivedContext.stream, stream);
  assert.deepEqual(await receivedContext.navigate("/after", { replace: true }), {
    url: "/after",
    options: { replace: true },
  });
  const reconciled = receivedContext.reconcile(surface, () => ({ changed: true }));
  assert.equal(reconciled.lifecycle, "mounted");
  const issue = receivedContext.reportFailure("preview", new Error("preview unavailable"), { mode: "preview" });
  assert.equal(issue.type, "request");
  assert.equal(issue.phase, "preview");
  assert.equal(issue.severity, "warning");
  assert.equal(issue.element, surface);
  assert.equal(issues.length, 1);
  assert.equal(telemetryEvents[0][0], "warn");
  assert.equal(telemetryEvents[0][1], "runtime-surface");
  assert.equal(telemetryEvents[0][3].mode, "preview");
  receivedContext.reportFailure("preview", { name: "AbortError", message: "stale" });
  assert.equal(issues.length, 1);
});

test("surface failure reporting delegates to the shared core policy", () => {
  const surface = makeElement({ "data-gosx-runtime-surface": "editor" });
  const body = makeElement({}, [surface]);
  const failures = [];
  const { context } = runModule(body, {
    withDOM: true,
    reportFailure(operation, error, fields) {
      failures.push({ operation, error, fields });
      return { delegated: true };
    },
  });
  let receivedContext;
  context.window.__gosx_register_runtime_surface("editor", (surfaceContext) => {
    receivedContext = surfaceContext;
    return { dispose() {} };
  });

  const result = receivedContext.reportFailure("preview", new Error("unavailable"), { mode: "preview" });
  assert.deepEqual(result, { delegated: true });
  assert.equal(failures.length, 1);
  assert.equal(failures[0].operation, "preview");
  assert.equal(failures[0].fields.scope, "runtime-surface");
  assert.equal(failures[0].fields.component, "editor");
  assert.equal(failures[0].fields.element, surface);
  assert.equal(failures[0].fields.telemetry.mode, "preview");
});

test("runtime surfaces are remounted after disposal when the page changes", () => {
  const first = makeElement({ "data-gosx-runtime-surface": "editor" });
  const second = makeElement({ "data-gosx-runtime-surface": "editor" });
  const body = makeElement({}, [first]);
  const { context, events } = runModule(body);
  let mounted = 0;

  context.window.__gosx_register_runtime_surface("editor", () => {
    mounted += 1;
    return { dispose() {} };
  });
  assert.equal(mounted, 1);

  body.children = [second];
  context.window.__gosx_mount_runtime_surfaces(body);
  assert.equal(mounted, 2);
  assert.equal(second.getAttribute("data-gosx-runtime-state"), "ready");
});

test("runtime surface mount reserves the record before factory reentry", () => {
  const surface = makeElement({ "data-gosx-runtime-surface": "editor" });
  const body = makeElement({}, [surface]);
  const { context, events } = runModule(body);
  let mounted = 0;
  let disposed = 0;

  context.window.__gosx_register_runtime_surface("editor", () => {
    mounted += 1;
    context.window.__gosx_mount_runtime_surfaces(body);
    return { dispose: () => { disposed += 1; } };
  });

  assert.equal(mounted, 1);
  assert.equal(context.window.__gosx_runtime_surfaces.get(surface).status, "ready");
  context.window.__gosx_dispose_runtime_surfaces(body);
  assert.equal(disposed, 1);
});

test("runtime surface disposal is exactly once through reentry and registration replacement", () => {
  const surface = makeElement({ "data-gosx-runtime-surface": "editor" });
  const body = makeElement({}, [surface]);
  const { context, events } = runModule(body);
  let oldDisposed = 0;
  let nextMounted = 0;
  let nextDisposed = 0;
  const firstFactory = () => ({
    dispose() {
      oldDisposed += 1;
      context.window.__gosx_dispose_runtime_surfaces(body);
      context.window.__gosx_register_runtime_surface("editor", () => {
        nextMounted += 1;
        return { dispose: () => { nextDisposed += 1; } };
      });
    },
  });

  context.window.__gosx_register_runtime_surface("editor", firstFactory);
  context.window.__gosx_dispose_runtime_surfaces(body);

  assert.equal(oldDisposed, 1);
  assert.equal(nextMounted, 0);
  assert.equal(context.window.__gosx_runtime_surfaces.has(surface), false);
  assert.equal(surface.getAttribute("data-gosx-runtime-state"), "disposed");
  assert.deepEqual(events.map((event) => event.type), [
    "gosx:runtime-surface:mount",
    "gosx:runtime-surface:unmount",
  ]);

  context.window.__gosx_mount_runtime_surfaces(body);
  assert.equal(nextMounted, 1);
  assert.equal(context.window.__gosx_runtime_surfaces.get(surface).status, "ready");
  assert.equal(surface.getAttribute("data-gosx-runtime-state"), "ready");
  context.window.__gosx_dispose_runtime_surfaces(body);
  assert.equal(nextDisposed, 1);
});

test("runtime surface disposer registration replacement is not overwritten by stale outer mount", () => {
  const surface = makeElement({ "data-gosx-runtime-surface": "editor" });
  const body = makeElement({}, [surface]);
  const { context } = runModule(body);
  let firstDisposed = 0;
  let secondDisposed = 0;
  let thirdMounted = 0;
  let thirdDisposed = 0;

  context.window.__gosx_register_runtime_surface("editor", () => ({
    dispose() {
      firstDisposed += 1;
      context.window.__gosx_register_runtime_surface("editor", () => {
        thirdMounted += 1;
        return { dispose: () => { thirdDisposed += 1; } };
      });
    },
  }));
  const secondFactory = () => ({ dispose: () => { secondDisposed += 1; } });
  context.window.__gosx_register_runtime_surface("editor", secondFactory);

  assert.equal(firstDisposed, 1);
  assert.equal(thirdMounted, 0);
  assert.equal(context.window.__gosx_runtime_surfaces.has(surface), false);
  assert.equal(surface.getAttribute("data-gosx-runtime-state"), "disposed");

  context.window.__gosx_mount_runtime_surfaces(body);
  const record = context.window.__gosx_runtime_surfaces.get(surface);
  assert.notEqual(record.factory, secondFactory);
  assert.equal(record.status, "ready");
  context.window.__gosx_dispose_runtime_surfaces(body);
  assert.equal(secondDisposed, 0);
  assert.equal(thirdDisposed, 1);
});

test("runtime surface same-factory mount from disposer is suppressed until later mount", () => {
  const surface = makeElement({ "data-gosx-runtime-surface": "editor" });
  const body = makeElement({}, [surface]);
  const { context } = runModule(body);
  let mounted = 0;
  let disposed = 0;
  let reentered = false;
  const factory = () => {
    mounted += 1;
    return {
      dispose() {
        disposed += 1;
        if (!reentered) {
          reentered = true;
          context.window.__gosx_mount_runtime_surfaces(body);
        }
      },
    };
  };

  context.window.__gosx_register_runtime_surface("editor", factory);
  context.window.__gosx_dispose_runtime_surfaces(body);

  assert.equal(mounted, 1);
  assert.equal(disposed, 1);
  assert.equal(context.window.__gosx_runtime_surfaces.has(surface), false);
  assert.equal(surface.getAttribute("data-gosx-runtime-state"), "disposed");

  context.window.__gosx_mount_runtime_surfaces(body);
  assert.equal(mounted, 2);
  assert.equal(context.window.__gosx_runtime_surfaces.get(surface).status, "ready");
  context.window.__gosx_dispose_runtime_surfaces(body);
  assert.equal(disposed, 2);
});

test("runtime surface abort-listener reentry cannot overwrite disposal ownership", () => {
  const surface = makeElement({ "data-gosx-runtime-surface": "editor" });
  const body = makeElement({}, [surface]);
  const { context, events } = runModule(body);
  let mounted = 0;
  let disposed = 0;
  let aborts = 0;
  let replacementMounted = 0;
  let replacementDisposed = 0;

  context.window.__gosx_register_runtime_surface("editor", (surfaceContext) => {
    mounted += 1;
    surfaceContext.signal.addEventListener("abort", () => {
      aborts += 1;
      context.window.__gosx_register_runtime_surface("editor", () => {
        replacementMounted += 1;
        return { dispose: () => { replacementDisposed += 1; } };
      });
      context.window.__gosx_mount_runtime_surfaces(body);
    }, { once: true });
    return { dispose: () => { disposed += 1; } };
  });

  context.window.__gosx_dispose_runtime_surfaces(body);

  assert.equal(mounted, 1);
  assert.equal(disposed, 1);
  assert.equal(aborts, 1);
  assert.equal(replacementMounted, 0);
  assert.equal(context.window.__gosx_runtime_surfaces.has(surface), false);
  assert.equal(surface.getAttribute("data-gosx-runtime-state"), "disposed");
  assert.deepEqual(events.map((event) => event.type), [
    "gosx:runtime-surface:mount",
    "gosx:runtime-surface:unmount",
  ]);

  context.window.__gosx_mount_runtime_surfaces(body);
  assert.equal(replacementMounted, 1);
  assert.equal(context.window.__gosx_runtime_surfaces.get(surface).status, "ready");
  context.window.__gosx_dispose_runtime_surfaces(body);
  assert.equal(replacementDisposed, 1);
});

test("runtime surface self-disposal cleans late sync handles once", () => {
  const surface = makeElement({ "data-gosx-runtime-surface": "editor" });
  const body = makeElement({}, [surface]);
  const { context, events } = runModule(body);
  let disposed = 0;

  context.window.__gosx_register_runtime_surface("editor", () => {
    context.window.__gosx_dispose_runtime_surfaces(body);
    return { dispose: () => { disposed += 1; } };
  });

  assert.equal(disposed, 1);
  assert.equal(context.window.__gosx_runtime_surfaces.has(surface), false);
  assert.equal(surface.getAttribute("data-gosx-runtime-state"), "disposed");
  assert.equal(events.some((event) => event.type === "gosx:runtime-surface:mount"), false);
});

test("runtime surface sync factory identity drift releases stale handle", () => {
  const surface = makeElement({ "data-gosx-runtime-surface": "editor" });
  const body = makeElement({}, [surface]);
  const { context, events } = runModule(body);
  let staleDisposed = 0;
  let viewerMounted = 0;

  context.window.__gosx_register_runtime_surface("editor", () => {
    surface.setAttribute("data-gosx-runtime-surface", "viewer");
    context.window.__gosx_register_runtime_surface("viewer", () => {
      viewerMounted += 1;
      return { dispose() {} };
    });
    return { dispose: () => { staleDisposed += 1; } };
  });

  assert.equal(staleDisposed, 1);
  assert.equal(viewerMounted, 0);
  assert.equal(context.window.__gosx_runtime_surfaces.has(surface), false);
  assert.equal(surface.getAttribute("data-gosx-runtime-state"), "disposed");
  assert.equal(events.some((event) => event.type === "gosx:runtime-surface:mount"), false);

  context.window.__gosx_mount_runtime_surfaces(body);
  assert.equal(viewerMounted, 1);
  assert.equal(surface.getAttribute("data-gosx-runtime-state"), "ready");
});

test("runtime surface stale promise returned after self-dispose is still observed", async (t) => {
  const surface = makeElement({ "data-gosx-runtime-surface": "editor" });
  const body = makeElement({}, [surface]);
  const { context, events } = runModule(body);
  const pending = deferred();
  let lateDisposed = 0;

  await assertNoUnhandledRejectionDuring(t, () => {
    context.window.__gosx_register_runtime_surface("editor", () => {
      context.window.__gosx_dispose_runtime_surfaces(body);
      return pending.promise;
    });
  });

  assert.equal(context.window.__gosx_runtime_surfaces.has(surface), false);
  assert.equal(surface.getAttribute("data-gosx-runtime-state"), "disposed");
  assert.equal(events.some((event) => event.type === "gosx:runtime-surface:mount"), false);

  pending.resolve({ dispose: () => { lateDisposed += 1; } });
  await pending.promise;
  await flushAsyncSurfaceWork();

  assert.equal(lateDisposed, 1);
  assert.equal(context.window.__gosx_runtime_surfaces.has(surface), false);
  assert.equal(events.some((event) => event.type === "gosx:runtime-surface:mount"), false);
});

test("runtime surface stale promise rejection after self-dispose is observed without stale error", async (t) => {
  const surface = makeElement({ "data-gosx-runtime-surface": "editor" });
  const body = makeElement({}, [surface]);
  const { context, events } = runModule(body);
  const pending = deferred();
  const failures = [];
  context.window.__gosx.reportIssue = (issue) => failures.push(issue);

  await assertNoUnhandledRejectionDuring(t, () => {
    context.window.__gosx_register_runtime_surface("editor", () => {
      context.window.__gosx_dispose_runtime_surfaces(body);
      return pending.promise;
    });
    pending.reject(new Error("late stale failure"));
  });

  assert.equal(context.window.__gosx_runtime_surfaces.has(surface), false);
  assert.equal(surface.getAttribute("data-gosx-runtime-state"), "disposed");
  assert.equal(failures.length, 0);
  assert.equal(events.some((event) => event.type === "gosx:runtime-surface:mount"), false);
});

test("runtime surface live mount failure callbacks cannot reenter ownership", () => {
  const surface = makeElement({ "data-gosx-runtime-surface": "editor" });
  const body = makeElement({}, [surface]);
  const { context, events } = runModule(body);
  const failures = [];
  let aborts = 0;
  let issueReplacementMounted = 0;
  let abortReplacementMounted = 0;
  context.window.__gosx.reportIssue = (issue) => {
    failures.push(issue);
    context.window.__gosx_register_runtime_surface("editor", () => {
      issueReplacementMounted += 1;
      return { dispose() {} };
    });
  };

  context.window.__gosx_register_runtime_surface("editor", (surfaceContext) => {
    surfaceContext.signal.addEventListener("abort", () => {
      aborts += 1;
      context.window.__gosx_register_runtime_surface("editor", () => {
        abortReplacementMounted += 1;
        return { dispose() {} };
      });
      context.window.__gosx_mount_runtime_surfaces(body);
    }, { once: true });
    throw new Error("mount failed");
  });

  assert.equal(failures.length, 1);
  assert.equal(aborts, 1);
  assert.equal(issueReplacementMounted, 0);
  assert.equal(abortReplacementMounted, 0);
  assert.equal(context.window.__gosx_runtime_surfaces.has(surface), false);
  assert.equal(surface.getAttribute("data-gosx-runtime-state"), "error");
  assert.equal(events.some((event) => event.type === "gosx:runtime-surface:mount"), false);

  context.window.__gosx_mount_runtime_surfaces(body);
  assert.equal(abortReplacementMounted, 1);
  assert.equal(surface.getAttribute("data-gosx-runtime-state"), "ready");
});

test("runtime surface async failure observers cannot skip abort or reject unhandled", async (t) => {
  const surface = makeElement({ "data-gosx-runtime-surface": "editor" });
  const body = makeElement({}, [surface]);
  const { context } = runModule(body);
  const pending = deferred();
  let aborts = 0;
  context.window.__gosx.reportIssue = () => {
    throw new Error("issue observer failed");
  };
  context.window.__gosx_emit = () => {
    throw new Error("telemetry observer failed");
  };

  await assertNoUnhandledRejectionDuring(t, () => {
    context.window.__gosx_register_runtime_surface("editor", (surfaceContext) => {
      surfaceContext.signal.addEventListener("abort", () => { aborts += 1; }, { once: true });
      return pending.promise;
    });
    pending.reject(new Error("async mount failed"));
  });

  assert.equal(aborts, 1);
  assert.equal(context.window.__gosx_runtime_surfaces.has(surface), false);
  assert.equal(surface.getAttribute("data-gosx-runtime-state"), "error");
});

test("runtime surface clearIssues exceptions do not strand pending ownership", () => {
  const surface = makeElement({ "data-gosx-runtime-surface": "editor" });
  const body = makeElement({}, [surface]);
  const { context } = runModule(body);
  let disposed = 0;
  let mounted = 0;
  context.window.__gosx.clearIssues = () => {
    throw new Error("clear failed");
  };

  assert.doesNotThrow(() => {
    context.window.__gosx_register_runtime_surface("editor", () => {
      mounted += 1;
      return { dispose: () => { disposed += 1; } };
    });
  });

  assert.equal(mounted, 1);
  assert.equal(surface.getAttribute("data-gosx-runtime-state"), "ready");
  context.window.__gosx_dispose_runtime_surfaces(body);
  assert.equal(disposed, 1);
  assert.equal(context.window.__gosx_runtime_surfaces.has(surface), false);
  assert.equal(surface.getAttribute("data-gosx-runtime-state"), "disposed");
});

test("runtime surface clearIssues reentry prevents stale factory and preserves replacement", () => {
  const surface = makeElement({ "data-gosx-runtime-surface": "editor" });
  const body = makeElement({}, [surface]);
  const { context, events } = runModule(body);
  let editorMounted = 0;
  let viewerMounted = 0;
  let reentered = false;

  context.window.__gosx.clearIssues = ({ root }) => {
    if (reentered) return;
    reentered = true;
    context.window.__gosx_dispose_runtime_surfaces(root);
    surface.setAttribute("data-gosx-runtime-surface", "viewer");
    context.window.__gosx_register_runtime_surface("viewer", () => {
      viewerMounted += 1;
      return { dispose() {} };
    });
    context.window.__gosx_mount_runtime_surfaces(body);
  };

  context.window.__gosx_register_runtime_surface("editor", () => {
    editorMounted += 1;
    return { dispose() {} };
  });

  assert.equal(editorMounted, 0);
  assert.equal(viewerMounted, 1);
  assert.equal(context.window.__gosx_runtime_surfaces.get(surface).name, "viewer");
  assert.equal(context.window.__gosx_runtime_surfaces.get(surface).status, "ready");
  assert.equal(surface.getAttribute("data-gosx-runtime-state"), "ready");
  assert.deepEqual(events.map((event) => event.detail && event.detail.name), ["viewer"]);
});

test("runtime surface failure observers cannot skip abort or reject unhandled", async (t) => {
  const surface = makeElement({ "data-gosx-runtime-surface": "editor" });
  const body = makeElement({}, [surface]);
  const { context } = runModule(body);
  let aborts = 0;
  context.window.__gosx.reportIssue = () => {
    throw new Error("issue observer failed");
  };
  context.window.__gosx_emit = () => {
    throw new Error("telemetry observer failed");
  };

  await assertNoUnhandledRejectionDuring(t, () => {
    context.window.__gosx_register_runtime_surface("editor", (surfaceContext) => {
      surfaceContext.signal.addEventListener("abort", () => { aborts += 1; }, { once: true });
      throw new Error("mount failed");
    });
  });

  assert.equal(aborts, 1);
  assert.equal(context.window.__gosx_runtime_surfaces.has(surface), false);
  assert.equal(surface.getAttribute("data-gosx-runtime-state"), "error");
});

test("runtime surface throwing scope disposer cannot skip other scope cleanup or main abort", () => {
  const surface = makeElement({ "data-gosx-runtime-surface": "editor" });
  const body = makeElement({}, [surface]);
  const { context } = runModule(body);
  let transportDisposed = 0;
  let schedulerDisposed = 0;
  let mainAborted = 0;
  context.window.__gosx.dom = {
    scope() {
      return {
        listen() { return function () {}; },
        dispose() { throw new Error("dom cleanup failed"); },
      };
    },
  };
  context.window.__gosx.transport = {
    scope() {
      return {
        request() { return Promise.resolve({}); },
        requestJSON() { return Promise.resolve({}); },
        requestLatest() { return Promise.resolve({}); },
        cancelRequest() {},
        dispose() { transportDisposed += 1; },
      };
    },
  };
  context.window.__gosx.scheduler = {
    scope() {
      return {
        schedule() { return null; },
        cancelSchedule() {},
        frame() { return null; },
        cancelFrame() {},
        dispose() { schedulerDisposed += 1; },
      };
    },
  };

  context.window.__gosx_register_runtime_surface("editor", (surfaceContext) => {
    surfaceContext.signal.addEventListener("abort", () => { mainAborted += 1; }, { once: true });
    return { dispose() {} };
  });
  assert.doesNotThrow(() => context.window.__gosx_dispose_runtime_surfaces(body));

  assert.equal(transportDisposed, 1);
  assert.equal(schedulerDisposed, 1);
  assert.equal(mainAborted, 1);
  assert.equal(context.window.__gosx_runtime_surfaces.has(surface), false);
  assert.equal(surface.getAttribute("data-gosx-runtime-state"), "disposed");
});

test("runtime surface throwing then getter disposes returned handle once", () => {
  const surface = makeElement({ "data-gosx-runtime-surface": "editor" });
  const body = makeElement({}, [surface]);
  const { context } = runModule(body);
  let disposed = 0;

  context.window.__gosx_register_runtime_surface("editor", () => ({
    dispose() { disposed += 1; },
    get then() {
      throw new Error("bad then getter");
    },
  }));

  assert.equal(disposed, 1);
  assert.equal(context.window.__gosx_runtime_surfaces.has(surface), false);
  assert.equal(surface.getAttribute("data-gosx-runtime-state"), "error");
});

test("runtime surface async handles settle before and after disposal without leaks", async () => {
  const before = makeElement({ "data-gosx-runtime-surface": "editor" });
  const after = makeElement({ "data-gosx-runtime-surface": "editor" });
  const body = makeElement({}, [before, after]);
  const { context, events } = runModule(body);
  const first = deferred();
  const second = deferred();
  let calls = 0;
  let disposed = 0;

  context.window.__gosx_register_runtime_surface("editor", () => {
    calls += 1;
    return calls === 1 ? first.promise : second.promise;
  });

  assert.equal(before.getAttribute("data-gosx-runtime-state"), "pending");
  first.resolve({ dispose: () => { disposed += 1; } });
  await first.promise;
  await flushAsyncSurfaceWork();
  assert.equal(before.getAttribute("data-gosx-runtime-state"), "ready");

  context.window.__gosx_dispose_runtime_surfaces(after);
  second.resolve({ dispose: () => { disposed += 1; } });
  await second.promise;
  await flushAsyncSurfaceWork();

  assert.equal(disposed, 1);
  assert.equal(context.window.__gosx_runtime_surfaces.has(after), false);
  assert.equal(after.getAttribute("data-gosx-runtime-state"), "disposed");
  assert.equal(events.filter((event) => event.type === "gosx:runtime-surface:mount").length, 1);
});

test("runtime surface async rejection records live errors and retry succeeds", async () => {
  const surface = makeElement({ "data-gosx-runtime-surface": "editor" });
  const body = makeElement({}, [surface]);
  const { context } = runModule(body);
  const failures = [];
  const pending = deferred();
  let calls = 0;
  context.window.__gosx.reportIssue = (issue) => failures.push(issue);

  context.window.__gosx_register_runtime_surface("editor", () => {
    calls += 1;
    if (calls === 1) return pending.promise;
    return { dispose() {} };
  });

  pending.reject(new Error("load failed"));
  await assert.rejects(pending.promise, /load failed/);
  await flushAsyncSurfaceWork();
  assert.equal(surface.getAttribute("data-gosx-runtime-state"), "error");
  assert.equal(context.window.__gosx_runtime_surfaces.has(surface), false);
  assert.equal(failures.length, 1);

  context.window.__gosx_mount_runtime_surfaces(body);
  assert.equal(calls, 2);
  assert.equal(surface.getAttribute("data-gosx-runtime-state"), "ready");
});

test("runtime surface pending async identity drift ignores stale settlement", async () => {
  const surface = makeElement({ "data-gosx-runtime-surface": "editor" });
  const body = makeElement({}, [surface]);
  const { context, events } = runModule(body);
  const pending = deferred();
  let staleDisposed = 0;
  let viewerMounted = 0;

  context.window.__gosx_register_runtime_surface("editor", () => pending.promise);
  surface.setAttribute("data-gosx-runtime-surface", "viewer");
  context.window.__gosx_register_runtime_surface("viewer", () => {
    viewerMounted += 1;
    return { dispose() {} };
  });
  assert.equal(viewerMounted, 0);
  assert.equal(surface.getAttribute("data-gosx-runtime-state"), "pending");

  pending.resolve({ dispose: () => { staleDisposed += 1; } });
  await pending.promise;
  await flushAsyncSurfaceWork();

  assert.equal(staleDisposed, 1);
  assert.equal(viewerMounted, 0);
  assert.equal(context.window.__gosx_runtime_surfaces.has(surface), false);
  assert.equal(events.filter((event) => event.type === "gosx:runtime-surface:mount").length, 0);

  context.window.__gosx_mount_runtime_surfaces(body);
  assert.equal(viewerMounted, 1);
  assert.equal(context.window.__gosx_runtime_surfaces.get(surface).status, "ready");
});

test("runtime surface pending async identity rejection is stale", async () => {
  const surface = makeElement({ "data-gosx-runtime-surface": "editor" });
  const body = makeElement({}, [surface]);
  const { context } = runModule(body);
  const pending = deferred();
  const failures = [];
  context.window.__gosx.reportIssue = (issue) => failures.push(issue);

  context.window.__gosx_register_runtime_surface("editor", () => pending.promise);
  surface.setAttribute("data-gosx-runtime-surface-version", "2");
  pending.reject(new Error("stale version"));
  await assert.rejects(pending.promise, /stale version/);
  await flushAsyncSurfaceWork();

  assert.equal(failures.length, 0);
  assert.equal(context.window.__gosx_runtime_surfaces.has(surface), false);
  assert.equal(surface.getAttribute("data-gosx-runtime-state"), "disposed");
});

test("runtime surface disconnected pending rejection releases without reporting error", async () => {
  const surface = makeElement({ "data-gosx-runtime-surface": "editor" });
  const body = makeElement({}, [surface]);
  const { context } = runModule(body);
  const pending = deferred();
  const failures = [];
  context.window.__gosx.reportIssue = (issue) => failures.push(issue);

  context.window.__gosx_register_runtime_surface("editor", () => pending.promise);
  body.children = [];
  pending.reject(new Error("detached"));
  await assert.rejects(pending.promise, /detached/);
  await flushAsyncSurfaceWork();

  assert.equal(failures.length, 0);
  assert.equal(context.window.__gosx_runtime_surfaces.has(surface), false);
  assert.equal(surface.getAttribute("data-gosx-runtime-state"), "disposed");
});

test("runtime surface stale async rejection after replacement is ignored", async () => {
  const surface = makeElement({ "data-gosx-runtime-surface": "editor" });
  const body = makeElement({}, [surface]);
  const { context } = runModule(body);
  const stale = deferred();
  const failures = [];
  context.window.__gosx.reportIssue = (issue) => failures.push(issue);

  context.window.__gosx_register_runtime_surface("editor", () => stale.promise);
  context.window.__gosx_register_runtime_surface("editor", () => ({ dispose() {} }));
  stale.reject(new Error("stale load failed"));
  await assert.rejects(stale.promise, /stale load failed/);
  await flushAsyncSurfaceWork();

  assert.equal(failures.length, 0);
  assert.equal(surface.getAttribute("data-gosx-runtime-state"), "disposed");
  assert.equal(context.window.__gosx_runtime_surfaces.has(surface), false);

  context.window.__gosx_mount_runtime_surfaces(body);
  assert.equal(surface.getAttribute("data-gosx-runtime-state"), "ready");
});

test("runtime surface stale async completion after replacement is disposed and ignored", async () => {
  const surface = makeElement({ "data-gosx-runtime-surface": "editor" });
  const body = makeElement({}, [surface]);
  const { context, events } = runModule(body);
  const stale = deferred();
  let staleDisposed = 0;
  let currentDisposed = 0;

  context.window.__gosx_register_runtime_surface("editor", () => stale.promise);
  context.window.__gosx_register_runtime_surface("editor", () => ({ dispose: () => { currentDisposed += 1; } }));

  assert.equal(surface.getAttribute("data-gosx-runtime-state"), "pending");
  stale.resolve({ dispose: () => { staleDisposed += 1; } });
  await stale.promise;
  await flushAsyncSurfaceWork();

  assert.equal(staleDisposed, 1);
  assert.equal(context.window.__gosx_runtime_surfaces.has(surface), false);
  assert.equal(events.filter((event) => event.type === "gosx:runtime-surface:mount").length, 0);

  context.window.__gosx_mount_runtime_surfaces(body);
  assert.equal(context.window.__gosx_runtime_surfaces.get(surface).status, "ready");
  assert.equal(events.filter((event) => event.type === "gosx:runtime-surface:mount").length, 1);
  context.window.__gosx_dispose_runtime_surfaces(body);
  assert.equal(currentDisposed, 1);
});

test("runtime surface thenable access errors fail without leaving pending records", () => {
  const surface = makeElement({ "data-gosx-runtime-surface": "editor" });
  const body = makeElement({}, [surface]);
  const { context } = runModule(body);
  const failures = [];
  context.window.__gosx.reportIssue = (issue) => failures.push(issue);

  context.window.__gosx_register_runtime_surface("editor", () => ({
    get then() {
      throw new Error("bad thenable");
    },
  }));

  assert.equal(surface.getAttribute("data-gosx-runtime-state"), "error");
  assert.equal(context.window.__gosx_runtime_surfaces.has(surface), false);
  assert.equal(failures.length, 1);
});

test("runtime surface then getter is read once for custom thenables", async () => {
  const surface = makeElement({ "data-gosx-runtime-surface": "editor" });
  const body = makeElement({}, [surface]);
  const { context } = runModule(body);
  let thenReads = 0;

  context.window.__gosx_register_runtime_surface("editor", () => ({
    get then() {
      thenReads += 1;
      return (resolve) => resolve({ dispose() {} });
    },
  }));
  await flushAsyncSurfaceWork();

  assert.equal(thenReads, 1);
  assert.equal(surface.getAttribute("data-gosx-runtime-state"), "ready");
});

test("runtime surface ready attribute callback can replace owner before mount event", () => {
  const surface = makeElement({ "data-gosx-runtime-surface": "editor" });
  const body = makeElement({}, [surface]);
  const { context, events } = runModule(body);
  let editorDisposed = 0;
  let viewerMounted = 0;
  const originalSetAttribute = surface.setAttribute;
  surface.setAttribute = function(name, value) {
    originalSetAttribute.call(this, name, value);
    if (name === "data-gosx-runtime-state" && value === "ready" && surface.getAttribute("data-gosx-runtime-surface") === "editor") {
      surface.setAttribute("data-gosx-runtime-surface", "viewer");
      context.window.__gosx_register_runtime_surface("viewer", () => {
        viewerMounted += 1;
        return { dispose() {} };
      });
    }
  };

  context.window.__gosx_register_runtime_surface("editor", () => ({ dispose: () => { editorDisposed += 1; } }));

  assert.equal(editorDisposed, 1);
  assert.equal(viewerMounted, 0);
  assert.equal(context.window.__gosx_runtime_surfaces.has(surface), false);
  assert.equal(events.some((event) => event.type === "gosx:runtime-surface:mount"), false);

  context.window.__gosx_mount_runtime_surfaces(body);
  assert.equal(viewerMounted, 1);
  assert.equal(surface.getAttribute("data-gosx-runtime-state"), "ready");
});

test("runtime surface ready attribute disposal remount releases old handle once", () => {
  const surface = makeElement({
    "data-gosx-runtime-surface": "editor",
    "data-gosx-runtime-issue": "old",
  });
  const body = makeElement({}, [surface]);
  const { context, events } = runModule(body);
  let editorDisposed = 0;
  let viewerMounted = 0;
  const originalSetAttribute = surface.setAttribute;
  surface.setAttribute = function(name, value) {
    originalSetAttribute.call(this, name, value);
    if (name === "data-gosx-runtime-state" && value === "ready" && surface.getAttribute("data-gosx-runtime-surface") === "editor") {
      context.window.__gosx_dispose_runtime_surfaces(surface);
      surface.setAttribute("data-gosx-runtime-surface", "viewer");
      context.window.__gosx_register_runtime_surface("viewer", () => {
        viewerMounted += 1;
        return { dispose() {} };
      });
      context.window.__gosx_mount_runtime_surfaces(body);
    }
  };

  context.window.__gosx_register_runtime_surface("editor", () => ({ dispose: () => { editorDisposed += 1; } }));

  assert.equal(editorDisposed, 1);
  assert.equal(viewerMounted, 1);
  assert.deepEqual(events.map((event) => event.detail && event.detail.name), ["viewer"]);
  assert.equal(context.window.__gosx_runtime_surfaces.get(surface).status, "ready");
  assert.equal(surface.getAttribute("data-gosx-runtime-state"), "ready");
  assert.equal(surface.getAttribute("data-gosx-runtime-issue"), null);
});

test("runtime surface mount listener reentry cannot leave stale telemetry", () => {
  const surface = makeElement({ "data-gosx-runtime-surface": "editor" });
  const body = makeElement({}, [surface]);
  const { context, events, telemetry } = runModule(body);
  let editorDisposed = 0;
  let viewerMounted = 0;
  context.document.dispatchEvent = (event) => {
    events.push(event);
    if (event.type === "gosx:runtime-surface:mount" && event.detail.name === "editor") {
      surface.setAttribute("data-gosx-runtime-surface", "viewer");
      context.window.__gosx_dispose_runtime_surfaces(surface);
      context.window.__gosx_register_runtime_surface("viewer", () => {
        viewerMounted += 1;
        return { dispose() {} };
      });
    }
  };

  context.window.__gosx_register_runtime_surface("editor", () => ({ dispose: () => { editorDisposed += 1; } }));

  assert.equal(editorDisposed, 1);
  assert.equal(viewerMounted, 1);
  assert.equal(context.window.__gosx_runtime_surfaces.get(surface).status, "ready");
  assert.equal(telemetry.some((entry) => entry.message === "runtime surface mounted" && entry.fields.name === "editor"), false);
  assert.equal(surface.getAttribute("data-gosx-runtime-state"), "ready");
});

test("runtime surface version metadata changes remount with a fresh context", () => {
  const surface = makeElement({
    "data-gosx-runtime-surface": "editor",
    "data-gosx-runtime-surface-version": "1",
  });
  const body = makeElement({}, [surface]);
  const { context } = runModule(body);
  const versions = [];
  let disposed = 0;

  context.window.__gosx_register_runtime_surface("editor", (surfaceContext) => {
    versions.push(surfaceContext.version);
    return { dispose: () => { disposed += 1; } };
  });

  surface.setAttribute("data-gosx-runtime-surface-version", "2");
  context.window.__gosx_mount_runtime_surfaces(body);

  assert.deepEqual(versions, ["1", "2"]);
  assert.equal(disposed, 1);
  assert.equal(context.window.__gosx_runtime_surfaces.get(surface).version, "2");
});

test("runtime surface version identity trims metadata like context", () => {
  const surface = makeElement({
    "data-gosx-runtime-surface": "editor",
    "data-gosx-runtime-surface-version": " 1 ",
  });
  const body = makeElement({}, [surface]);
  const { context } = runModule(body);
  let mounted = 0;

  context.window.__gosx_register_runtime_surface("editor", () => {
    mounted += 1;
    return { dispose() {} };
  });
  context.window.__gosx_mount_runtime_surfaces(body);

  assert.equal(mounted, 1);
  assert.equal(context.window.__gosx_runtime_surfaces.get(surface).version, "1");
});

test("runtime surface pending disposal is scoped per independent root", async () => {
  const first = makeElement({ "data-gosx-runtime-surface": "editor" });
  const second = makeElement({ "data-gosx-runtime-surface": "editor" });
  const body = makeElement({}, [first, second]);
  const { context } = runModule(body);
  const firstPending = deferred();
  const secondPending = deferred();
  let calls = 0;
  let firstDisposed = 0;
  let secondDisposed = 0;

  context.window.__gosx_register_runtime_surface("editor", () => {
    calls += 1;
    return calls === 1 ? firstPending.promise : secondPending.promise;
  });

  context.window.__gosx_dispose_runtime_surfaces(first);
  firstPending.resolve({ dispose: () => { firstDisposed += 1; } });
  secondPending.resolve({ dispose: () => { secondDisposed += 1; } });
  await Promise.all([firstPending.promise, secondPending.promise]);
  await flushAsyncSurfaceWork();

  assert.equal(firstDisposed, 1);
  assert.equal(secondDisposed, 0);
  assert.equal(context.window.__gosx_runtime_surfaces.has(first), false);
  assert.equal(context.window.__gosx_runtime_surfaces.get(second).status, "ready");
  context.window.__gosx_dispose_runtime_surfaces(second);
  assert.equal(secondDisposed, 1);
});

test("runtime surface pending disconnected root does not publish ready", async () => {
  const surface = makeElement({ "data-gosx-runtime-surface": "editor" });
  const body = makeElement({}, [surface]);
  const { context, events } = runModule(body);
  const pending = deferred();
  let disposed = 0;

  context.window.__gosx_register_runtime_surface("editor", () => pending.promise);
  body.children = [];
  context.window.__gosx_dispose_runtime_surfaces(body);
  pending.resolve({ dispose: () => { disposed += 1; } });
  await pending.promise;
  await flushAsyncSurfaceWork();

  assert.equal(disposed, 1);
  assert.equal(context.window.__gosx_runtime_surfaces.has(surface), false);
  assert.equal(surface.getAttribute("data-gosx-runtime-state"), "disposed");
  assert.equal(events.some((event) => event.type === "gosx:runtime-surface:mount"), false);
});

test("core request transport owns CSRF defaults and preserves explicit headers", async () => {
  const calls = [];
  const body = makeElement();
  const meta = { getAttribute(name) { return name === "content" ? "csrf-from-core" : null; } };
  const { context } = runModule(body, {
    querySelector(selector) {
      return selector === 'meta[name="csrf-token"]' ? meta : null;
    },
    fetch(input, init) {
      calls.push({ input, init });
      return Promise.resolve({ json: () => Promise.resolve({ ok: true }) });
    },
  });

  assert.equal(typeof context.window.__gosx.request, "function");
  assert.equal(typeof context.window.__gosx.transport.json, "function");

  await context.window.__gosx.request("/save", { method: "POST" });
  assert.equal(calls[0].init.headers["X-CSRF-Token"], "csrf-from-core");

  await context.window.__gosx.request("/save", {
    method: "POST",
    headers: { "x-csrf-token": "csrf-from-form" },
  });
  assert.equal(calls[1].init.headers["x-csrf-token"], "csrf-from-form");
  assert.equal(calls[1].init.headers["X-CSRF-Token"], undefined);

  await context.window.__gosx.request("/read", { method: "GET" });
  assert.equal(calls[2].init.headers, undefined);
});

test("surface requests inherit the surface abort signal through core transport", async () => {
  const calls = [];
  const surface = makeElement({ "data-gosx-runtime-surface": "editor" });
  const body = makeElement({}, [surface]);
  const { context } = runModule(body, {
    fetch(input, init) {
      calls.push({ input, init });
      return Promise.resolve({});
    },
  });
  let receivedContext;

  context.window.__gosx_register_runtime_surface("editor", (surfaceContext) => {
    receivedContext = surfaceContext;
    return { dispose() {} };
  });
  await receivedContext.request("/preview", { method: "POST" });

  assert.equal(calls.length, 1);
  assert.equal(calls[0].init.signal, receivedContext.signal);
});

test("surface latest requests cancel stale work and expose shared response JSON", async () => {
  const calls = [];
  const surface = makeElement({ "data-gosx-runtime-surface": "editor" });
  const body = makeElement({}, [surface]);
  const { context } = runModule(body, {
    fetch(input, init) {
      calls.push({ input, init });
      return new Promise((resolve, reject) => {
        const abort = () => {
          const error = new Error("request aborted");
          error.name = "AbortError";
          reject(error);
        };
        if (init.signal && init.signal.aborted) {
          abort();
          return;
        }
        if (init.signal) init.signal.addEventListener("abort", abort, { once: true });
        if (input === "/second") resolve({ ok: true });
      });
    },
  });
  let receivedContext;

  context.window.__gosx_register_runtime_surface("editor", (surfaceContext) => {
    receivedContext = surfaceContext;
    return { dispose() {} };
  });

  const first = receivedContext.requestLatest("preview", "/first", { method: "POST" });
  const second = receivedContext.requestLatest("preview", "/second", { method: "POST" });
  await assert.rejects(first, (error) => error && error.name === "AbortError");
  await second;

  assert.equal(calls.length, 2);
  assert.equal(calls[0].init.signal.aborted, true);
  assert.notEqual(calls[0].init.signal, calls[1].init.signal);
  const decoded = await receivedContext.json({
    clone() {
      return { json: () => Promise.resolve({ html: "<p>ok</p>" }) };
    },
  });
  assert.deepEqual(decoded, { html: "<p>ok</p>" });
});

test("transport scopes isolate latest-request cancellation and inherit lifecycle aborts", async () => {
  const calls = [];
  const body = makeElement();
  const { context } = runModule(body, {
    fetch(input, init) {
      calls.push({ input, init });
      return new Promise((resolve, reject) => {
        const abort = () => {
          const error = new Error("request aborted");
          error.name = "AbortError";
          reject(error);
        };
        if (init.signal && init.signal.aborted) return abort();
        if (init.signal) init.signal.addEventListener("abort", abort, { once: true });
        if (input === "/second") resolve({ ok: true });
      });
    },
  });
  const firstController = new AbortController();
  const first = context.window.__gosx.transport.scope(firstController.signal);
  const second = context.window.__gosx.transport.scope();
  const stale = first.requestLatest("preview", "/first");
  const current = second.requestLatest("preview", "/first");
  firstController.abort();
  await assert.rejects(stale, (error) => error && error.name === "AbortError");
  assert.equal(calls[0].init.signal.aborted, true);
  assert.equal(calls.length, 2);
  second.cancelRequest("preview");
  await assert.rejects(current, (error) => error && error.name === "AbortError");
});

test("surface scheduler coalesces keyed work and cancels it on unmount", async () => {
  const surface = makeElement({ "data-gosx-runtime-surface": "editor" });
  const body = makeElement({}, [surface]);
  const { context } = runModule(body, { withDOM: true });
  const calls = [];
  let receivedContext;

  context.window.__gosx_register_runtime_surface("editor", (surfaceContext) => {
    receivedContext = surfaceContext;
    return { dispose() {} };
  });

  receivedContext.schedule("preview", () => calls.push("stale"), 0);
  receivedContext.schedule("preview", () => calls.push("current"), 0);
  await new Promise((resolve) => setTimeout(resolve, 0));
  assert.deepEqual(calls, ["current"]);

  receivedContext.schedule("after-unmount", () => calls.push("disposed"), 0);
  context.window.__gosx_dispose_runtime_surfaces(body);
  await new Promise((resolve) => setTimeout(resolve, 0));
  assert.deepEqual(calls, ["current"]);
});
