// 26-runtime-surfaces.js — generic GoSX-managed browser surfaces.
//
// Optional packages such as gosx/editor can register a surface runtime without
// making the core bootstrap import the package. Server-rendered markup opts in
// with data-gosx-runtime-surface="name". The bootstrap owns discovery,
// navigation remounting, disposal, and the small browser bridge shared by
// surface runtimes.
(function () {
  "use strict";

  if (typeof window === "undefined" || typeof document === "undefined") return;

  function gosxRequestToken() {
    const meta = document.querySelector && document.querySelector('meta[name="csrf-token"]');
    return meta ? String(meta.getAttribute("content") || "") : "";
  }

  function gosxMutatingMethod(method) {
    switch (String(method || "").toUpperCase()) {
      case "POST":
      case "PUT":
      case "PATCH":
      case "DELETE":
        return true;
      default:
        return false;
    }
  }

  function gosxHeadersObject(headers) {
    const result = {};
    if (!headers) return result;
    if (typeof headers.forEach === "function") {
      headers.forEach((value, key) => { result[key] = value; });
      return result;
    }
    if (Array.isArray(headers)) {
      for (const entry of headers) {
        if (Array.isArray(entry) && entry.length >= 2) result[entry[0]] = entry[1];
      }
      return result;
    }
    for (const key of Object.keys(headers)) result[key] = headers[key];
    return result;
  }

  function gosxHasHeader(headers, name) {
    const wanted = String(name || "").toLowerCase();
    return Object.keys(headers).some((key) => String(key).toLowerCase() === wanted);
  }

  function gosxRequest(input, init) {
    if (typeof window.fetch !== "function") {
      return Promise.reject(new Error("fetch is not available"));
    }
    const options = Object.assign({}, init || {});
    const csrf = options.csrf !== false;
    delete options.csrf;
    const headers = gosxHeadersObject(options.headers);
    const method = options.method || (input && input.method) || "GET";
    if (csrf && gosxMutatingMethod(method) && !gosxHasHeader(headers, "X-CSRF-Token")) {
      const token = gosxRequestToken();
      if (token) headers["X-CSRF-Token"] = token;
    }
    if (Object.keys(headers).length > 0) options.headers = headers;
    return window.fetch(input, options);
  }

  function gosxResponseJSON(response) {
    if (!response) return Promise.resolve(null);
    const candidate = typeof response.clone === "function" ? response.clone() : response;
    if (candidate && typeof candidate.json === "function") {
      return Promise.resolve().then(() => candidate.json()).catch(() => null);
    }
    return Promise.resolve(null);
  }

  // Timers and animation frames are part of a surface's lifecycle too. Keep
  // debouncing/coalescing in the framework so an optional surface does not
  // leave work running after navigation or fragment replacement. The key
  // namespace is scoped: two mounted surfaces may both schedule "preview"
  // without cancelling each other.
  function gosxCreateSchedulerScope(parentSignal) {
    const timers = new Map();
    const frames = new Map();
    let disposed = false;
    let parentCleanup = function () {};

    const cancelTimer = (key) => {
      const scheduleKey = String(key || "default");
      const record = timers.get(scheduleKey);
      if (!record) return;
      timers.delete(scheduleKey);
      if (typeof clearTimeout === "function") clearTimeout(record.id);
    };

    const cancelFrame = (key) => {
      const frameKey = String(key || "default");
      const record = frames.get(frameKey);
      if (!record) return;
      frames.delete(frameKey);
      if (record.raf && typeof window.cancelAnimationFrame === "function") {
        window.cancelAnimationFrame(record.id);
      } else if (typeof clearTimeout === "function") {
        clearTimeout(record.id);
      }
    };

    const schedule = (key, callback, delay) => {
      const scheduleKey = String(key || "default");
      cancelTimer(scheduleKey);
      if (disposed || typeof callback !== "function" || typeof setTimeout !== "function") {
        return function () {};
      }
      const record = { id: null };
      const run = () => {
        if (timers.get(scheduleKey) !== record || disposed) return;
        timers.delete(scheduleKey);
        callback();
      };
      const timeout = Number(delay);
      record.id = setTimeout(run, Number.isFinite(timeout) ? Math.max(0, timeout) : 0);
      timers.set(scheduleKey, record);
      return () => {
        if (timers.get(scheduleKey) === record) cancelTimer(scheduleKey);
      };
    };

    const frame = (key, callback) => {
      const frameKey = String(key || "default");
      cancelFrame(frameKey);
      if (disposed || typeof callback !== "function") return function () {};
      const hasRAF = typeof window.requestAnimationFrame === "function";
      if (!hasRAF && typeof setTimeout !== "function") return function () {};
      const record = { id: null, raf: hasRAF };
      const run = (timestamp) => {
        if (frames.get(frameKey) !== record || disposed) return;
        frames.delete(frameKey);
        callback(timestamp);
      };
      record.id = hasRAF
        ? window.requestAnimationFrame(run)
        : setTimeout(() => run(Date.now()), 16);
      frames.set(frameKey, record);
      return () => {
        if (frames.get(frameKey) === record) cancelFrame(frameKey);
      };
    };

    const cancelAll = () => {
      for (const key of Array.from(timers.keys())) cancelTimer(key);
      for (const key of Array.from(frames.keys())) cancelFrame(key);
    };

    const dispose = () => {
      if (disposed) return;
      disposed = true;
      cancelAll();
      parentCleanup();
      parentCleanup = function () {};
    };

    if (parentSignal && typeof parentSignal.addEventListener === "function") {
      const onAbort = () => dispose();
      if (parentSignal.aborted) onAbort();
      else {
        parentSignal.addEventListener("abort", onAbort, { once: true });
        parentCleanup = () => parentSignal.removeEventListener("abort", onAbort);
      }
    }

    return {
      schedule,
      cancel: cancelTimer,
      cancelSchedule: cancelTimer,
      frame,
      cancelFrame,
      cancelAll,
      dispose,
    };
  }

  function gosxLinkAbort(parent, child) {
    if (!parent || !child || typeof parent.addEventListener !== "function") return function () {};
    if (parent.aborted) {
      child.abort();
      return function () {};
    }
    const onAbort = () => child.abort();
    parent.addEventListener("abort", onAbort, { once: true });
    return () => {
      if (typeof parent.removeEventListener === "function") {
        parent.removeEventListener("abort", onAbort);
      }
    };
  }

  // Request coalescing belongs to the framework transport, not to an
  // individual surface. A scope gives each mounted surface its own latest
  // namespace while keeping cancellation and parent lifecycle signals in the
  // core runtime.
  function gosxCreateTransportScope(request, responseJSON, parentSignal) {
    const latest = new Map();

    const scopedRequest = (input, init) => {
      const options = Object.assign({}, init || {});
      if (parentSignal && !options.signal) options.signal = parentSignal;
      return request(input, options);
    };

    const cancelRequest = (key) => {
      const requestKey = String(key || "default");
      const record = latest.get(requestKey);
      if (!record) return;
      latest.delete(requestKey);
      if (record.controller && typeof record.controller.abort === "function") {
        record.controller.abort();
      }
      for (const cleanup of record.cleanups || []) cleanup();
    };

    const requestLatest = (key, input, init) => {
      const requestKey = String(key || "default");
      cancelRequest(requestKey);
      const options = Object.assign({}, init || {});
      const controller = typeof AbortController === "function" ? new AbortController() : null;
      const cleanups = [];
      if (controller) {
        cleanups.push(gosxLinkAbort(parentSignal, controller));
        if (options.signal) cleanups.push(gosxLinkAbort(options.signal, controller));
        options.signal = controller.signal;
      }
      const record = { controller, cleanups };
      if (controller) latest.set(requestKey, record);
      return Promise.resolve()
        .then(() => scopedRequest(input, options))
        .finally(() => {
          for (const cleanup of cleanups) cleanup();
          if (latest.get(requestKey) === record) latest.delete(requestKey);
        });
    };

    const requestJSON = async (input, init) => {
      const response = await scopedRequest(input, init);
      return { response, data: await responseJSON(response) };
    };

    return {
      request: scopedRequest,
      requestLatest,
      cancelRequest,
      json: responseJSON,
      requestJSON,
      dispose() {
        for (const key of Array.from(latest.keys())) cancelRequest(key);
      },
    };
  }

  if (window.__gosx) {
    const existingTransport = window.__gosx.transport && typeof window.__gosx.transport === "object"
      ? window.__gosx.transport
      : {};
    const request = typeof window.__gosx.request === "function"
      ? window.__gosx.request
      : (typeof existingTransport.request === "function" ? existingTransport.request : gosxRequest);
    window.__gosx.request = request;
    if (typeof existingTransport.request !== "function") existingTransport.request = request;
    if (typeof existingTransport.csrfToken !== "function") existingTransport.csrfToken = gosxRequestToken;
    if (typeof existingTransport.json !== "function") existingTransport.json = gosxResponseJSON;
    if (typeof existingTransport.scope !== "function") {
      existingTransport.scope = (parentSignal) => gosxCreateTransportScope(request, existingTransport.json, parentSignal);
    }
    if (typeof existingTransport.requestLatest !== "function") {
      const globalScope = gosxCreateTransportScope(request, existingTransport.json, null);
      existingTransport.requestLatest = globalScope.requestLatest;
      existingTransport.cancelRequest = globalScope.cancelRequest;
      existingTransport.requestJSON = globalScope.requestJSON;
    }
    window.__gosx.transport = existingTransport;

    const existingScheduler = window.__gosx.scheduler && typeof window.__gosx.scheduler === "object"
      ? window.__gosx.scheduler
      : {};
    if (typeof existingScheduler.scope !== "function") {
      existingScheduler.scope = (parentSignal) => gosxCreateSchedulerScope(parentSignal);
    }
    if (typeof existingScheduler.schedule !== "function") {
      const globalScope = gosxCreateSchedulerScope(null);
      existingScheduler.schedule = globalScope.schedule;
      existingScheduler.cancel = globalScope.cancel;
      existingScheduler.cancelSchedule = globalScope.cancelSchedule;
      existingScheduler.frame = globalScope.frame;
      existingScheduler.cancelFrame = globalScope.cancelFrame;
      existingScheduler.cancelAll = globalScope.cancelAll;
    }
    window.__gosx.scheduler = existingScheduler;
  }

  if (window.__gosx_runtime_surfaces) return;

  const factories = window.__gosx_runtime_surface_factories || Object.create(null);
  const mounts = new Map();

  window.__gosx_runtime_surface_factories = factories;
  window.__gosx_runtime_surfaces = mounts;
  if (window.__gosx) window.__gosx.runtimeSurfaces = mounts;

  function surfaceName(root) {
    return String(root && root.getAttribute && root.getAttribute("data-gosx-runtime-surface") || "").trim();
  }

  function surfaceNodes(root) {
    const host = root || document.body || document.documentElement;
    if (!host) return [];
    const nodes = [];
    if (host.getAttribute && surfaceName(host)) nodes.push(host);
    if (host.querySelectorAll) {
      for (const node of Array.from(host.querySelectorAll("[data-gosx-runtime-surface]"))) {
        if (!nodes.includes(node)) nodes.push(node);
      }
    }
    return nodes;
  }

  function surfaceContext(name, root) {
    const controller = typeof AbortController === "function" ? new AbortController() : null;
    const cleanups = new Set();
    const requestControllers = new Map();
    const fallbackListen = (target, type, listener, options) => {
      if (!target || typeof target.addEventListener !== "function") return function () {};
      const config = typeof options === "boolean" ? { capture: options } : Object.assign({}, options || {});
      if (controller) config.signal = controller.signal;
      target.addEventListener(type, listener, config);
      const cleanup = function () {
        if (!controller || !controller.signal.aborted) {
          target.removeEventListener(type, listener, typeof options === "boolean" ? options : config);
        }
        cleanups.delete(cleanup);
      };
      cleanups.add(cleanup);
      return cleanup;
    };
    const request = (input, init) => {
      const options = Object.assign({}, init || {});
      if (controller && !options.signal) options.signal = controller.signal;
      if (window.__gosx && typeof window.__gosx.request === "function") {
        return window.__gosx.request(input, options);
      }
      if (typeof window.fetch !== "function") return Promise.reject(new Error("fetch is not available"));
      return window.fetch(input, options);
    };
    const domScope = window.__gosx && window.__gosx.dom && typeof window.__gosx.dom.scope === "function"
      ? window.__gosx.dom.scope(root, controller ? controller.signal : null)
      : null;
    const listen = domScope ? domScope.listen : fallbackListen;
    const transportScope = window.__gosx && window.__gosx.transport && typeof window.__gosx.transport.scope === "function"
      ? window.__gosx.transport.scope(controller ? controller.signal : null)
      : null;
    const schedulerScope = window.__gosx && window.__gosx.scheduler && typeof window.__gosx.scheduler.scope === "function"
      ? window.__gosx.scheduler.scope(controller ? controller.signal : null)
      : gosxCreateSchedulerScope(controller ? controller.signal : null);
    const cancelRequest = (key) => {
      if (transportScope) {
        transportScope.cancelRequest(key);
        return;
      }
      const record = requestControllers.get(String(key || "default"));
      if (!record) return;
      if (record.controller && typeof record.controller.abort === "function") {
        record.controller.abort();
      }
      if (typeof record.detach === "function") record.detach();
    };
    const requestLatest = (key, input, init) => {
      if (transportScope) return transportScope.requestLatest(key, input, init);
      const requestKey = String(key || "default");
      cancelRequest(requestKey);
      const requestController = typeof AbortController === "function" ? new AbortController() : null;
      let detach = function () {};
      if (requestController && controller) {
        if (controller.signal.aborted) requestController.abort();
        else {
          const onAbort = () => requestController.abort();
          controller.signal.addEventListener("abort", onAbort, { once: true });
          detach = () => controller.signal.removeEventListener("abort", onAbort);
        }
      }
      const record = { controller: requestController, detach };
      if (requestController) requestControllers.set(requestKey, record);
      const options = Object.assign({}, init || {});
      if (requestController) options.signal = requestController.signal;
      return Promise.resolve().then(() => request(input, options)).finally(() => {
        record.detach();
        if (requestControllers.get(requestKey) === record) {
          requestControllers.delete(requestKey);
        }
      });
    };
    const responseJSON = (response) => {
      if (window.__gosx && window.__gosx.transport && typeof window.__gosx.transport.json === "function") {
        return window.__gosx.transport.json(response);
      }
      return gosxResponseJSON(response);
    };
    const requestJSON = async (input, init) => {
      if (transportScope) return transportScope.requestJSON(input, init);
      const response = await request(input, init);
      return { response, data: await responseJSON(response) };
    };
    const navigate = (url, options) => {
      const target = String(url || "").trim();
      if (!target) return Promise.resolve(false);
      const navigation = window.__gosx && window.__gosx.navigation;
      if (navigation && typeof navigation.navigate === "function") {
        return navigation.navigate(target, options);
      }
      if (window.location) {
        window.location.href = target;
      }
      return Promise.resolve(false);
    };
    const reconcile = (target, updater, options) => {
      const dom = window.__gosx && window.__gosx.dom;
      if (dom && typeof dom.reconcile === "function") {
        return dom.reconcile(target, updater, options);
      }
      return typeof updater === "function" ? updater(target) : null;
    };
    const reportIssue = (issue) => {
      if (window.__gosx && typeof window.__gosx.reportIssue === "function") {
        return window.__gosx.reportIssue(Object.assign({
          scope: "runtime-surface",
          component: name,
          element: root,
        }, issue || {}));
      }
      return null;
    };
    const reportFailure = (operation, error, fields) => {
      if (controller && controller.signal.aborted) return null;
      if (error && error.name === "AbortError") return null;
      const phase = String(operation || "request").trim() || "request";
      const message = error && error.message
        ? String(error.message)
        : phase + " request failed";
      const sharedFailure = window.__gosx && window.__gosx.reportFailure;
      if (typeof sharedFailure === "function") {
        return sharedFailure(operation, error, Object.assign({
          scope: "runtime-surface",
          type: "request",
          severity: "warning",
          component: name,
          element: root,
          fallback: "server",
          telemetry: Object.assign({ name }, fields || {}),
        }, fields || {}));
      }
      const issue = reportIssue({
        type: "request",
        phase,
        severity: "warning",
        message,
        error,
        fallback: "server",
      });
      const telemetry = window.__gosx && window.__gosx.telemetry;
      if (telemetry && typeof telemetry.emit === "function") {
        telemetry.emit("warn", "runtime-surface", message, Object.assign({
          name,
          operation: phase,
        }, fields || {}));
      }
      return issue;
    };
    return {
      name,
      root,
      version: String(root && root.getAttribute && root.getAttribute("data-gosx-runtime-surface-version") || "").trim(),
      window,
      document,
      namespace: window.__gosx || null,
      dom: domScope,
      transport: transportScope,
      get navigation() {
        return window.__gosx && window.__gosx.navigation ? window.__gosx.navigation : null;
      },
      get diagnostics() {
        return window.__gosx && window.__gosx.diagnostics ? window.__gosx.diagnostics : null;
      },
      get telemetry() {
        return window.__gosx && window.__gosx.telemetry ? window.__gosx.telemetry : null;
      },
      get prose() {
        return window.__gosx && window.__gosx.prose ? window.__gosx.prose : null;
      },
      get actions() {
        return window.__gosx && window.__gosx.actions ? window.__gosx.actions : null;
      },
      get regions() {
        return window.__gosx && window.__gosx.regions ? window.__gosx.regions : null;
      },
      get stream() {
        return window.__gosx && window.__gosx.stream ? window.__gosx.stream : null;
      },
      fetch: typeof window.fetch === "function" ? request : null,
      request,
      requestLatest,
      cancelRequest,
      json: responseJSON,
      requestJSON,
      scheduler: schedulerScope,
      schedule: schedulerScope.schedule,
      cancelSchedule: schedulerScope.cancelSchedule,
      frame: schedulerScope.frame,
      cancelFrame: schedulerScope.cancelFrame,
      navigate,
      reconcile,
      reportFailure,
      signal: controller ? controller.signal : null,
      listen,
      on: listen,
      query(selector) {
        if (domScope) return domScope.query(selector);
        if (!root || typeof root.querySelector !== "function") return null;
        return root.querySelector(selector);
      },
      queryAll(selector) {
        if (domScope) return domScope.queryAll(selector);
        if (!root || typeof root.querySelectorAll !== "function") return [];
        return Array.from(root.querySelectorAll(selector));
      },
      contains(node) {
        if (domScope) return domScope.contains(node);
        return !!(root && typeof root.contains === "function" && root.contains(node));
      },
      reportIssue,
      dispatch(type, detail) {
        if (domScope) return domScope.dispatch(type, detail);
        if (typeof document.dispatchEvent !== "function" || typeof CustomEvent !== "function") return;
        document.dispatchEvent(new CustomEvent(type, { detail }));
      },
      abort() {
        if (domScope) domScope.dispose();
        if (transportScope) transportScope.dispose();
        if (schedulerScope) schedulerScope.dispose();
        for (const record of Array.from(requestControllers.values())) {
          if (record && record.controller && typeof record.controller.abort === "function") record.controller.abort();
          if (record && typeof record.detach === "function") record.detach();
        }
        requestControllers.clear();
        if (controller) {
          controller.abort();
          cleanups.clear();
          return;
        }
        for (const cleanup of Array.from(cleanups)) cleanup();
      },
    };
  }

  function normalizeHandle(value) {
    if (typeof value === "function") return { dispose: value };
    return value && typeof value === "object" ? value : {};
  }

  function emit(type, detail) {
    if (typeof document.dispatchEvent !== "function" || typeof CustomEvent !== "function") return;
    document.dispatchEvent(new CustomEvent(type, { detail }));
  }

  function observe(level, message, fields) {
    if (typeof window.__gosx_emit !== "function") return;
    window.__gosx_emit(level, "runtime-surface", message, fields || {});
  }

  function mark(root, state, issue) {
    if (!root || typeof root.setAttribute !== "function") return;
    root.setAttribute("data-gosx-runtime-state", state);
    if (issue) root.setAttribute("data-gosx-runtime-issue", issue);
    else root.removeAttribute("data-gosx-runtime-issue");
  }

  function disposeRecord(root, record) {
    if (!record) return;
    try {
      if (record.handle && typeof record.handle.dispose === "function") record.handle.dispose();
    } catch (error) {
      console.error("[gosx] runtime surface dispose failed:", record.name, error);
    }
    record.context.abort();
    mounts.delete(root);
    if (window.__gosx && typeof window.__gosx.clearIssues === "function") {
      window.__gosx.clearIssues({ root: root });
    }
    mark(root, "disposed");
    emit("gosx:runtime-surface:unmount", { name: record.name, root });
    observe("info", "runtime surface unmounted", { name: record.name, version: record.version || "" });
  }

  function disposeRuntimeSurfaces(root) {
    const host = root || document.body || document.documentElement;
    for (const [surfaceRoot, record] of Array.from(mounts.entries())) {
      if (!host || surfaceRoot === host || (host.contains && host.contains(surfaceRoot))) {
        disposeRecord(surfaceRoot, record);
      }
    }
  }

  function mountRuntimeSurface(root) {
    const name = surfaceName(root);
    const factory = name ? factories[name] : null;
    if (!factory) return null;

    const current = mounts.get(root);
    if (current && current.factory === factory) return current.handle;
    if (current) disposeRecord(root, current);

    const context = surfaceContext(name, root);
    if (window.__gosx && typeof window.__gosx.clearIssues === "function") {
      window.__gosx.clearIssues({ root: root });
    }
    try {
      const handle = normalizeHandle(factory(context));
      const record = { name, root, factory, context, handle, version: context.version };
      mounts.set(root, record);
      mark(root, "ready");
      emit("gosx:runtime-surface:mount", { name, root, context, handle, version: context.version });
      observe("info", "runtime surface mounted", { name, version: context.version || "" });
      return handle;
    } catch (error) {
      mark(root, "error", "mount");
      if (window.__gosx && typeof window.__gosx.reportIssue === "function") {
        window.__gosx.reportIssue({
          scope: "runtime-surface",
          type: "mount",
          component: name,
          element: root,
          error,
          fallback: "server",
        });
      } else {
        console.error("[gosx] runtime surface mount failed:", name, error);
      }
      observe("error", "runtime surface mount failed", { name, version: context.version || "" });
      context.abort();
      return null;
    }
  }

  function mountRuntimeSurfaces(root) {
    for (const node of surfaceNodes(root)) mountRuntimeSurface(node);
    return mounts;
  }

  window.__gosx_register_runtime_surface = function (name, factory) {
    const key = String(name || "").trim();
    if (!key || typeof factory !== "function") {
      console.error("[gosx] invalid runtime surface registration");
      return null;
    }
    factories[key] = factory;
    observe("debug", "runtime surface registered", { name: key });
    mountRuntimeSurfaces(document.body || document.documentElement);
    return factory;
  };
  window.__gosx_mount_runtime_surfaces = mountRuntimeSurfaces;
  window.__gosx_dispose_runtime_surfaces = disposeRuntimeSurfaces;
  if (window.__gosx) {
    window.__gosx.runtimeSurfaceAPI = {
      register: window.__gosx_register_runtime_surface,
      mount: mountRuntimeSurfaces,
      dispose: disposeRuntimeSurfaces,
      factories,
      mounts,
    };
  }
})();
