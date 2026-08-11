// GoSX Client Bootstrap v0.11.0
// Loads shared WASM runtime, fetches per-island programs, hydrates islands
// via event delegation. This is the only JavaScript in a GoSX app.
//
// Expects:
//   - wasm_exec.js loaded before this script (standard Go WASM support)
//   - <script id="gosx-manifest" type="application/json"> with island manifest
//   - WASM exports: __gosx_hydrate, __gosx_action, __gosx_dispose
//   - WASM calls window.__gosx_runtime_ready() when Go runtime is initialized

(function() {
  "use strict";

  const GOSX_VERSION = "0.11.0";

  // --- GoSX runtime namespace ---
  const engineFactories = window.__gosx_engine_factories || Object.create(null);
  window.__gosx_engine_factories = engineFactories;
  window.__gosx_register_engine_factory = function(name, factory) {
    if (!name || typeof factory !== "function") {
      console.error("[gosx] invalid engine factory registration");
      return;
    }
    engineFactories[name] = factory;
  };

  // Lock down factory registration after DOM content loaded
  document.addEventListener("DOMContentLoaded", function() {
    window.__gosx_register_engine_factory = function(name) {
      console.error("[gosx] engine factory registration is closed after init:", name);
    };
    Object.freeze(engineFactories);
  });

  window.__gosx = {
    version: GOSX_VERSION,
    islands: new Map(),   // islandID -> { component, listeners, root }
    computeIslands: new Map(), // compute island ID -> { component }
    engines: new Map(),   // engineID -> { component, kind, mount, handle }
    hubs: new Map(),      // hubID -> { entry, socket, reconnectTimer }
    controllers: new Map(), // controllerID -> { config, listeners, timers }
    textLayouts: new Map(), // textLayoutID -> { element, result, config }
    sharedSignals: {
      values: new Map(),
      subscribers: new Map(),
      nextID: 0,
    },
    input: {
      pending: null,
      frameHandle: 0,
      providers: Object.create(null),
    },
    ready: false,
  };

  function gosxSharedSignalStore() {
    const current = window.__gosx && window.__gosx.sharedSignals;
    if (current && current.values instanceof Map && current.subscribers instanceof Map) {
      return current;
    }
    const store = {
      values: new Map(),
      subscribers: new Map(),
      nextID: 0,
    };
    if (window.__gosx) {
      window.__gosx.sharedSignals = store;
    }
    return store;
  }

  function parseSharedSignalJSON(valueJSON, fallback) {
    if (typeof valueJSON !== "string" || valueJSON === "") {
      return fallback;
    }
    if (valueJSON.startsWith("error:")) {
      return fallback;
    }
    try {
      return JSON.parse(valueJSON);
    } catch (_error) {
      return fallback;
    }
  }

  function gosxReadSharedSignal(name, fallback) {
    const signalName = String(name || "").trim();
    if (!signalName) {
      return fallback;
    }
    const store = gosxSharedSignalStore();
    if (store.values.has(signalName)) {
      return store.values.get(signalName);
    }
    const getter = window.__gosx_get_shared_signal;
    if (typeof getter !== "function") {
      return fallback;
    }
    try {
      const value = parseSharedSignalJSON(getter(signalName), fallback);
      store.values.set(signalName, value);
      return value;
    } catch (_error) {
      return fallback;
    }
  }

  function gosxNotifySharedSignal(name, valueJSON) {
    const signalName = String(name || "").trim();
    if (!signalName) {
      return null;
    }
    const store = gosxSharedSignalStore();
    const value = parseSharedSignalJSON(valueJSON, null);
    store.values.set(signalName, value);
    const listeners = store.subscribers.get(signalName);
    if (!listeners) {
      return null;
    }
    for (const entry of Array.from(listeners.values())) {
      try {
        entry(value, signalName);
      } catch (error) {
        console.error("[gosx] shared signal subscriber failed:", error);
      }
    }
    return null;
  }

  function gosxSubscribeSharedSignal(name, listener, options) {
    const signalName = String(name || "").trim();
    if (!signalName || typeof listener !== "function") {
      return function() {};
    }
    const store = gosxSharedSignalStore();
    let listeners = store.subscribers.get(signalName);
    if (!listeners) {
      listeners = new Map();
      store.subscribers.set(signalName, listeners);
    }
    store.nextID += 1;
    const id = "shared-signal-" + store.nextID;
    listeners.set(id, listener);
    if (!options || options.immediate !== false) {
      listener(gosxReadSharedSignal(signalName, null), signalName);
    }
    return function() {
      const current = store.subscribers.get(signalName);
      if (!current) {
        return;
      }
      current.delete(id);
      if (current.size === 0) {
        store.subscribers.delete(signalName);
      }
    };
  }

  function setSharedSignalJSON(name, valueJSON) {
    const signalName = String(name || "").trim();
    if (!signalName) {
      return null;
    }

    const setSharedSignal = window.__gosx_set_shared_signal;
    if (typeof setSharedSignal === "function") {
      try {
        const result = setSharedSignal(signalName, valueJSON);
        if (typeof result === "string" && result !== "") {
          console.error("[gosx] shared signal update error (" + signalName + "):", result);
          gosxNotifySharedSignal(signalName, valueJSON);
        }
        return result;
      } catch (error) {
        console.error("[gosx] shared signal update error (" + signalName + "):", error);
      }
    }

    gosxNotifySharedSignal(signalName, valueJSON);
    return null;
  }

  function setSharedSignalValue(name, value) {
    return setSharedSignalJSON(name, JSON.stringify(value == null ? null : value));
  }

  window.__gosx_notify_shared_signal = gosxNotifySharedSignal;
  // Public reader, symmetric to __gosx_set_shared_signal: subscribe(name, fn, opts)
  // → unsubscribe fn. Lets declarative modules (data-gosx-region) and apps observe
  // shared signals without poking the store internals.
  window.__gosx_subscribe_shared_signal = gosxSubscribeSharedSignal;

  function gosxIssueStore() {
    const current = window.__gosx.issues;
    if (!current || !Array.isArray(current.entries) || !(current.subscribers instanceof Set)) {
      window.__gosx.issues = {
        nextID: current && Number.isFinite(current.nextID) ? current.nextID : 0,
        entries: current && Array.isArray(current.entries) ? current.entries : [],
        subscribers: current && current.subscribers instanceof Set ? current.subscribers : new Set(),
      };
    }
    return window.__gosx.issues;
  }

  function gosxCloneIssue(issue) {
    const clone = Object.assign({}, issue || {});
    delete clone._element;
    return clone;
  }

  function gosxIssueText(value) {
    const text = String(value == null ? "" : value).trim();
    return text === "[object Object]" ? "" : text;
  }

  function gosxIssueMessage(issue) {
    const message = gosxIssueText(issue && issue.message);
    if (message) {
      return message;
    }
    const errorMessage = gosxIssueText(issue && issue.error && issue.error.message);
    if (errorMessage) {
      return errorMessage;
    }
    const errorText = gosxIssueText(issue && issue.error);
    if (errorText) {
      return errorText;
    }
    return "runtime failure";
  }

  function gosxMarkIssueElement(element, issue) {
    if (!element || typeof element.setAttribute !== "function") {
      return;
    }
    element.setAttribute("data-gosx-runtime-state", "error");
    element.setAttribute("data-gosx-runtime-issue", issue.type);
    if (issue.fallback) {
      element.setAttribute("data-gosx-fallback-active", issue.fallback);
    }
  }

  function gosxClearIssueState(element) {
    if (!element || typeof element.removeAttribute !== "function") {
      return;
    }
    element.setAttribute("data-gosx-runtime-state", "ready");
    element.removeAttribute("data-gosx-runtime-issue");
    element.removeAttribute("data-gosx-fallback-active");
  }

  function gosxIssueMatches(entry, filter) {
    if (filter == null) return true;
    if (typeof filter === "function") return Boolean(filter(gosxCloneIssue(entry)));
    if (typeof filter === "string") return entry.id === filter;
    if (typeof filter !== "object") return false;
    if (filter.id && entry.id !== String(filter.id)) return false;
    for (const key of ["scope", "type", "severity", "component", "ref", "source", "phase"]) {
      if (filter[key] != null && entry[key] !== String(filter[key])) return false;
    }
    if (filter.element && entry._element !== filter.element) return false;
    if (filter.root && entry._element !== filter.root
      && !(typeof filter.root.contains === "function" && filter.root.contains(entry._element))) {
      return false;
    }
    return true;
  }

  function gosxNotifyIssueChange(type, entry) {
    const store = gosxIssueStore();
    const detail = { type, issue: gosxCloneIssue(entry) };
    for (const listener of Array.from(store.subscribers)) {
      try {
        listener(detail);
      } catch (error) {
        console.error("[gosx] diagnostics subscriber failed:", error);
      }
    }
    if (document && typeof document.dispatchEvent === "function" && typeof CustomEvent === "function") {
      document.dispatchEvent(new CustomEvent("gosx:diagnostics", { detail }));
    }
  }

  function gosxReportRuntimeIssue(issue) {
    const store = gosxIssueStore();
    store.nextID += 1;
    const entry = {
      id: "gosx-issue-" + store.nextID,
      scope: gosxIssueText(issue && issue.scope) || "runtime",
      type: gosxIssueText(issue && issue.type) || "runtime",
      severity: gosxIssueText(issue && issue.severity) || "error",
      message: gosxIssueMessage(issue),
      component: gosxIssueText(issue && issue.component),
      ref: gosxIssueText(issue && issue.ref),
      source: gosxIssueText(issue && issue.source),
      phase: gosxIssueText(issue && issue.phase),
      fallback: gosxIssueText(issue && issue.fallback) || "server",
      elementID: gosxIssueText(issue && issue.element && issue.element.id),
      timestamp: Date.now(),
      _element: issue && issue.element ? issue.element : null,
    };
    store.entries.push(entry);
    if (store.entries.length > 100) {
      store.entries.splice(0, store.entries.length - 100);
    }
    gosxMarkIssueElement(issue && issue.element, entry);
    if (document && typeof document.dispatchEvent === "function" && typeof CustomEvent === "function") {
      document.dispatchEvent(new CustomEvent("gosx:error", {
        detail: { issue: gosxCloneIssue(entry) },
      }));
    }
    gosxNotifyIssueChange("report", entry);
    return gosxCloneIssue(entry);
  }

  // One failure policy is shared by core declarative features and optional
  // browser surfaces. The issue fields stay diagnostic-friendly while the
  // caller supplies a small, bounded telemetry payload separately. Aborted
  // work is expected during latest-wins refreshes and lifecycle disposal, so
  // it is deliberately invisible to both channels.
  function gosxReportRuntimeFailure(operation, error, fields) {
    const options = fields || {};
    if (options.signal && options.signal.aborted) return null;
    if (error && String(error.name || "") === "AbortError") return null;
    const phase = String(operation || "request").trim() || "request";
    const message = error && error.message
      ? String(error.message)
      : phase + " request failed";
    const issue = gosxReportRuntimeIssue(Object.assign({
      scope: "runtime",
      type: "request",
      severity: "warning",
      phase,
      message,
      error,
      fallback: "server",
    }, options));
    const telemetry = window.__gosx && window.__gosx.telemetry;
    if (telemetry && typeof telemetry.emit === "function") {
      try {
        telemetry.emit(
          "warn",
          gosxIssueText(options.category) || gosxIssueText(options.scope) || "runtime",
          message,
          Object.assign({ operation: phase }, options.telemetry || {})
        );
      } catch (_err) {
        // Diagnostics must not make fallback runtime paths fail.
      }
    }
    return issue;
  }

  function gosxListIssues() {
    return gosxIssueStore().entries.map(gosxCloneIssue);
  }

  function gosxSubscribeIssues(listener) {
    if (typeof listener !== "function") return function () {};
    const store = gosxIssueStore();
    store.subscribers.add(listener);
    return function () {
      store.subscribers.delete(listener);
    };
  }

  function gosxClearIssues(filter) {
    const store = gosxIssueStore();
    const removed = [];
    const retained = [];
    for (const entry of store.entries) {
      if (!gosxIssueMatches(entry, filter)) {
        retained.push(entry);
        continue;
      }
      removed.push(entry);
      gosxClearIssueState(entry._element);
    }
    store.entries = retained;
    for (const entry of removed) gosxNotifyIssueChange("clear", entry);
    return removed.map(gosxCloneIssue);
  }

  window.__gosx.reportIssue = gosxReportRuntimeIssue;
  window.__gosx.reportFailure = gosxReportRuntimeFailure;
  window.__gosx.listIssues = gosxListIssues;
  window.__gosx.clearIssueState = gosxClearIssueState;
  window.__gosx.clearIssues = gosxClearIssues;
  window.__gosx.subscribeIssues = gosxSubscribeIssues;
  window.__gosx.diagnostics = {
    report: gosxReportRuntimeIssue,
    reportFailure: gosxReportRuntimeFailure,
    list: gosxListIssues,
    clear: gosxClearIssues,
    clearFor(element) {
      return gosxClearIssues({ root: element });
    },
    subscribe: gosxSubscribeIssues,
  };


  function normalizeTextLayoutWritingMode(value) {
    const mode = typeof value === "string" ? value.trim().toLowerCase() : "";
    switch (mode) {
      case "vertical-rl":
      case "vertical-lr":
      case "sideways-rl":
      case "sideways-lr":
      case "horizontal-tb":
        return mode;
      default:
        return "";
    }
  }

  function textLayoutIsVerticalWritingMode(value) {
    const mode = normalizeTextLayoutWritingMode(value);
    return mode === "vertical-rl" || mode === "vertical-lr" || mode === "sideways-rl" || mode === "sideways-lr";
  }

  function normalizeTextLayoutOverflow(value) {
    const mode = typeof value === "string" ? value.trim().toLowerCase() : "";
    return mode === "ellipsis" ? "ellipsis" : "clip";
  }

  function textLayoutNumberValue(value, fallback) {
    const parsed = Number(value);
    return Number.isFinite(parsed) ? parsed : fallback;
  }

  function textLayoutLengthValue(value, fallback) {
    if (typeof value === "number" && Number.isFinite(value)) {
      return value;
    }
    if (typeof value === "string") {
      const trimmed = value.trim().toLowerCase();
      if (!trimmed || trimmed === "none" || trimmed === "auto" || trimmed === "normal" || trimmed === "unset") {
        return fallback;
      }
      const parsed = Number.parseFloat(trimmed);
      return Number.isFinite(parsed) ? parsed : fallback;
    }
    return textLayoutNumberValue(value, fallback);
  }

  function textLayoutComputedStyle(element) {
    if (!element || typeof window.getComputedStyle !== "function") {
      return null;
    }
    try {
      return window.getComputedStyle(element);
    } catch (_error) {
      return null;
    }
  }

  function textLayoutComputedStyleValue(style, propertyName) {
    if (!style || !propertyName) {
      return "";
    }
    if (typeof style.getPropertyValue === "function") {
      const propertyValue = style.getPropertyValue(propertyName);
      if (typeof propertyValue === "string" && propertyValue.trim() !== "") {
        return propertyValue.trim();
      }
    }
    const camelName = propertyName.replace(/-([a-z])/g, function(_match, letter) {
      return String(letter || "").toUpperCase();
    });
    const value = style[camelName];
    return typeof value === "string" ? value.trim() : "";
  }

  function textLayoutLogicalInlineSize(width, height, writingMode) {
    return textLayoutIsVerticalWritingMode(writingMode) ? Math.max(0, sceneNumber(height, 0)) : Math.max(0, sceneNumber(width, 0));
  }

  function textLayoutLogicalBlockSize(width, height, writingMode) {
    return textLayoutIsVerticalWritingMode(writingMode) ? Math.max(0, sceneNumber(width, 0)) : Math.max(0, sceneNumber(height, 0));
  }

  function setStyleValue(style, name, value) {
    if (!style || typeof name !== "string") {
      return;
    }
    const next = String(value);
    if (typeof style.setProperty === "function") {
      if (typeof style.getPropertyValue === "function" && style.getPropertyValue(name) === next) {
        return;
      }
      style.setProperty(name, next);
      return;
    }
    if (style[name] === next) {
      return;
    }
    style[name] = next;
  }

  function setAttrValue(element, name, value) {
    if (!element || typeof element.setAttribute !== "function" || typeof name !== "string" || !name) {
      return;
    }
    if (value == null || value === "") {
      if (typeof element.removeAttribute === "function") {
        if (!element.hasAttribute || !element.hasAttribute(name)) {
          return;
        }
        element.removeAttribute(name);
      }
      return;
    }
    const next = String(value);
    if (typeof element.getAttribute === "function" && element.getAttribute(name) === next) {
      return;
    }
    element.setAttribute(name, next);
  }

  function walkElementTree(root, visit) {
    if (!root) {
      return;
    }
    if (root.nodeType === 1) {
      visit(root);
    }
    const children = root.children || root.childNodes || [];
    for (const child of children) {
      if (child && child.nodeType === 1) {
        walkElementTree(child, visit);
      }
    }
  }

  // --------------------------------------------------------------------------
  // Text-layout engine seam
  // --------------------------------------------------------------------------
  //
  // The browser text-layout engine used to sit in this file: Intl.Segmenter
  // wrappers, CJK line-break prohibition tables, hyphenation, vertical writing
  // mode and ellipsis clamping. It cost 42_751 of the 157_086 minified bytes in
  // bootstrap-runtime.js and 42_738 of the 131_137 in bootstrap-lite.js, and it
  // loaded on every page, even a page that holds no text block. The engine now
  // ships as bootstrap-src/01-textlayout-engine.js. The monolithic bootstrap.js
  // still carries it inline. The selective bundles fetch it as
  // bootstrap-feature-textlayout.js when the document holds a
  // data-gosx-text-layout element, or when the manifest mounts a Scene3D
  // engine, which lays out labels.
  //
  // Core keeps stable forwarders. A caller — or the Scene3D feature chunk,
  // which copies window.__gosx_runtime_api into local variables the moment its
  // script runs — captures a forwarder once and still reaches the engine after
  // the engine arrives.
  //
  // Queries answer with the same empty value the engine gives for text it
  // cannot lay out, so a page without the engine falls back to native CSS
  // wrapping instead of throwing. Commands queue and replay in order.

  const sceneLabelLayoutCacheLimit = 512;

  let textLayoutEngineImpl = null;
  const textLayoutEngineCommandQueue = [];
  const textLayoutInvalidationSubscribers = new Set();
  let textLayoutEngineUnsubscribe = null;
  // Set when a forwarder answered from a fallback. Callers may have cached that
  // answer, so the engine has to invalidate on arrival. Bundles that carry the
  // engine inline register before any query and skip the extra invalidation.
  let textLayoutServedFallback = false;

  function textLayoutEngineInvoke(method, args) {
    const fn = textLayoutEngineImpl ? textLayoutEngineImpl[method] : null;
    if (typeof fn !== "function") {
      return undefined;
    }
    try {
      return fn.apply(null, args);
    } catch (error) {
      console.error("[gosx] text layout engine call failed (" + method + "):", error);
      return undefined;
    }
  }

  // textLayoutEngineQuery answers a layout question. When the engine has not
  // arrived it starts the fetch and answers with the fallback the caller passed.
  // Scene3D reaches this path when a scene gains a Label node after mount: the
  // label draws from the rough fallback for one frame, then the engine
  // registers, bumps the layout revision, and the invalidation listener in
  // 20-scene-mount.js lays the label out again.
  function textLayoutEngineQuery(method, args, fallback) {
    if (textLayoutEngineImpl) {
      const result = textLayoutEngineInvoke(method, args);
      return result === undefined ? null : result;
    }
    textLayoutServedFallback = true;
    loadTextLayoutEngine();
    return fallback === undefined ? null : fallback;
  }

  // textLayoutFallbackResult builds a layout with the shape the engine returns,
  // sized by a rough eight-pixel-per-character estimate — the same estimate
  // sceneMeasureTextWidth uses when no measurer is present. Scene3D label
  // rendering reads result.lines with no null guard, so a caller that beats the
  // engine chunk to the first frame still gets a drawable label.
  function textLayoutFallbackResult(text, lineHeight, options) {
    const source = String(text == null ? "" : text);
    const rawLineHeight = Number(lineHeight);
    const resolvedLineHeight = Math.max(1, Number.isFinite(rawLineHeight) && rawLineHeight > 0 ? rawLineHeight : 1);
    const rawMaxLines = options ? Number(options.maxLines) : 0;
    const maxLines = Number.isFinite(rawMaxLines) && rawMaxLines > 0 ? Math.floor(rawMaxLines) : 0;
    const parts = source.split("\n");
    const lines = [];
    let cursor = 0;
    let maxLineWidth = 0;
    let truncated = false;
    for (let index = 0; index < parts.length; index += 1) {
      if (maxLines > 0 && lines.length >= maxLines) {
        truncated = true;
        break;
      }
      const part = parts[index];
      const width = part.length * 8;
      if (width > maxLineWidth) {
        maxLineWidth = width;
      }
      lines.push({
        start: cursor,
        end: cursor + part.length,
        byteStart: cursor,
        byteEnd: cursor + part.length,
        runeStart: cursor,
        runeEnd: cursor + part.length,
        width,
        text: part,
        hardBreak: index < parts.length - 1,
      });
      cursor += part.length + 1;
    }
    if (lines.length === 0) {
      lines.push({
        start: 0, end: 0, byteStart: 0, byteEnd: 0, runeStart: 0, runeEnd: 0,
        width: 0, text: "", hardBreak: false,
      });
    }
    return {
      lines,
      lineCount: lines.length,
      height: lines.length * resolvedLineHeight,
      maxLineWidth,
      byteLen: source.length,
      runeCount: source.length,
      truncated,
    };
  }

  // textLayoutEngineCommand runs a state-changing call now, or records it and
  // replays it when the engine registers.
  function textLayoutEngineCommand(method, args) {
    if (textLayoutEngineImpl) {
      return textLayoutEngineInvoke(method, args);
    }
    textLayoutEngineCommandQueue.push({ method, args });
    return undefined;
  }

  function notifyTextLayoutInvalidated(revision) {
    for (const listener of Array.from(textLayoutInvalidationSubscribers)) {
      try {
        listener(revision);
      } catch (error) {
        console.error("[gosx] text layout invalidation listener failed:", error);
      }
    }
  }

  window.__gosx_register_text_layout_engine = function(impl) {
    if (!impl || typeof impl !== "object") {
      console.error("[gosx] invalid text layout engine registration");
      return;
    }
    textLayoutEngineImpl = impl;
    // Core owns the subscriber set so a subscription taken before the engine
    // loaded stays live. One bridge listener fans invalidations out.
    if (typeof impl.onInvalidated === "function" && !textLayoutEngineUnsubscribe) {
      textLayoutEngineUnsubscribe = impl.onInvalidated(notifyTextLayoutInvalidated);
    }
    const queued = textLayoutEngineCommandQueue.splice(0);
    for (const command of queued) {
      textLayoutEngineInvoke(command.method, command.args);
    }
    // Bump the layout revision only when a caller already holds a fallback
    // answer. Callers that cache by revision — the Scene3D label cache in
    // 20-scene-mount.js is one — need the bump to drop those rough results and
    // to fire the invalidation listeners that re-render.
    if (textLayoutServedFallback) {
      textLayoutServedFallback = false;
      textLayoutEngineInvoke("invalidate", []);
    }
  };

  window.__gosx_text_layout_engine_ready = function() {
    return textLayoutEngineImpl !== null;
  };

  function gosxTextLayout(text, font, maxWidth, whiteSpace, lineHeight, options) {
    return textLayoutEngineQuery("layout", [text, font, maxWidth, whiteSpace, lineHeight, options],
      textLayoutFallbackResult(text, lineHeight, options));
  }

  function gosxTextLayoutMetrics(text, font, maxWidth, whiteSpace, lineHeight, options) {
    return textLayoutEngineQuery("metrics", [text, font, maxWidth, whiteSpace, lineHeight, options]);
  }

  function gosxTextLayoutRanges(text, font, maxWidth, whiteSpace, lineHeight, options) {
    return textLayoutEngineQuery("ranges", [text, font, maxWidth, whiteSpace, lineHeight, options]);
  }

  function gosxTextLayoutRevision() {
    const revision = textLayoutEngineInvoke("revision", []);
    return typeof revision === "number" ? revision : 0;
  }

  function layoutBrowserText(text, font, maxWidth, whiteSpace, lineHeight, options) {
    return textLayoutEngineQuery("layoutBrowserText", [text, font, maxWidth, whiteSpace, lineHeight, options],
      textLayoutFallbackResult(text, lineHeight, options));
  }

  function applyTextLayoutPresentation(element, config, result, meta) {
    return textLayoutEngineInvoke("applyPresentation", [element, config, result, meta]);
  }

  function onTextLayoutInvalidated(listener) {
    if (typeof listener !== "function") {
      return function() {};
    }
    textLayoutInvalidationSubscribers.add(listener);
    return function() {
      textLayoutInvalidationSubscribers.delete(listener);
    };
  }

  function adoptTextLayoutImpl(candidate) {
    return textLayoutEngineCommand("adoptLayoutImpl", [candidate]);
  }

  function adoptTextLayoutMetricsImpl(candidate) {
    return textLayoutEngineCommand("adoptMetricsImpl", [candidate]);
  }

  function adoptTextLayoutRangesImpl(candidate) {
    return textLayoutEngineCommand("adoptRangesImpl", [candidate]);
  }

  // observeManagedTextLayout always answers with a handle. The engine returns an
  // inert handle for a bad element, so callers never test for null.
  function observeManagedTextLayout(element, options) {
    const handle = textLayoutEngineInvoke("observe", [element, options]);
    if (handle) {
      return handle;
    }
    return { refresh: function() { return null; }, dispose: function() {} };
  }

  function mountManagedTextLayouts(root) {
    return textLayoutEngineCommand("mountAll", [root]);
  }

  function refreshManagedTextLayouts() {
    return textLayoutEngineCommand("refreshAll", []);
  }

  function disposeManagedTextLayout(target) {
    return textLayoutEngineInvoke("dispose", [target]);
  }

  function disposeManagedTextLayouts(root) {
    // Drop queued mounts first. A soft navigation can dispose a page before the
    // engine chunk lands, and replaying a mount for a removed subtree is waste.
    textLayoutEngineCommandQueue.length = 0;
    return textLayoutEngineInvoke("disposeAll", [root]);
  }

  function readManagedTextLayout(element) {
    const record = textLayoutEngineInvoke("read", [element]);
    if (record) {
      return record;
    }
    return element && element.__gosxTextLayout ? element.__gosxTextLayout : null;
  }

  // gosxDocumentHasTextLayout reports whether the document holds a managed text
  // block. The runtime gates the engine fetch on this.
  function gosxDocumentHasTextLayout(root) {
    const scope = root || document;
    if (!scope || typeof scope.querySelector !== "function") {
      return false;
    }
    try {
      return scope.querySelector("[data-gosx-text-layout]") !== null;
    } catch (_error) {
      return false;
    }
  }

  window.__gosx_document_has_text_layout = gosxDocumentHasTextLayout;

  window.__gosx.textLayout = {
    layout: gosxTextLayout,
    metrics: gosxTextLayoutMetrics,
    ranges: gosxTextLayoutRanges,
    revision: gosxTextLayoutRevision,
    observe: observeManagedTextLayout,
    mountAll: mountManagedTextLayouts,
    refresh(target) {
      if (target) {
        const handle = observeManagedTextLayout(target);
        return handle.refresh("manual");
      }
      refreshManagedTextLayouts();
      return null;
    },
    read(element) {
      return readManagedTextLayout(element);
    },
    dispose: disposeManagedTextLayout,
    disposeAll: disposeManagedTextLayouts,
  };

  // Runtime API — exposed for the async Scene3D feature chunk which
  // runs in a separate IIFE and can't access these via closure.
  window.__gosx_runtime_api = {
    setAttrValue,
    setStyleValue,
    gosxSubscribeSharedSignal,
    setSharedSignalValue,
    gosxTextLayoutRevision,
    normalizeTextLayoutOverflow,
    layoutBrowserText,
    applyTextLayoutPresentation,
    onTextLayoutInvalidated,
    sceneLabelLayoutCacheLimit,
    // gosxLowEndHardware lives in 05-document-env.js. The lazily fetched
    // scene3d chunk does not carry that file, so 20-scene-mount.js read the
    // name across an IIFE boundary and threw ReferenceError whenever the
    // environment snapshot carried no lowEndHardware flag.
    gosxLowEndHardware,
    // The engine chunk runs in its own IIFE and reads these back out.
    textLayoutComputedStyle,
    textLayoutComputedStyleValue,
    textLayoutLengthValue,
    textLayoutNumberValue,
    textLayoutLogicalInlineSize,
    textLayoutLogicalBlockSize,
    normalizeTextLayoutWritingMode,
    textLayoutIsVerticalWritingMode,
    walkElementTree,
  };

  // --------------------------------------------------------------------------
  // Text-layout engine chunk loader (bundles with no feature host)
  // --------------------------------------------------------------------------
  //
  // bootstrap-runtime.js routes the chunk through
  // ensureBootstrapFeature("textlayout") in 26-runtime-tail.js, which owns the
  // feature host. bootstrap-lite.js has no host, so core loads the chunk here.
  // bootstrap.js carries the engine inline and never reaches this code.

  let textLayoutEngineLoad = null;

  // gosxBootstrapScriptNonce copies the CSP nonce off a GoSX script tag so the
  // injected chunk passes a strict-CSP page. 10-runtime-scene-core.js holds the
  // same lookup for the bundles that carry it; bootstrap-lite.js does not, so
  // core keeps this copy.
  function gosxBootstrapScriptNonce() {
    const selectors = [
      "script[nonce][data-gosx-script]",
      "script[nonce][data-gosx-navigation]",
      "script[nonce][data-gosx-document-contract]",
      "script[nonce]",
    ];
    const current = document.currentScript;
    const own = current ? (current.nonce || (current.getAttribute && current.getAttribute("nonce")) || "") : "";
    if (own) {
      return String(own);
    }
    for (const selector of selectors) {
      const found = typeof document.querySelector === "function" ? document.querySelector(selector) : null;
      const nonce = found ? (found.nonce || (found.getAttribute && found.getAttribute("nonce")) || "") : "";
      if (nonce) {
        return String(nonce);
      }
    }
    return "";
  }

  function textLayoutEngineChunkURL() {
    const state = window.__gosx && window.__gosx.document && typeof window.__gosx.document.get === "function"
      ? window.__gosx.document.get()
      : null;
    const assets = state && state.assets && state.assets.runtime ? state.assets.runtime : null;
    if (assets && assets.bootstrapFeatureTextLayoutPath) {
      return String(assets.bootstrapFeatureTextLayoutPath).trim();
    }
    const head = document && document.head;
    const nodes = head && head.children ? Array.from(head.children) : [];
    for (const node of nodes) {
      if (!node || String(node.tagName || "").toUpperCase() !== "LINK") {
        continue;
      }
      const rel = String((node.getAttribute && node.getAttribute("rel")) || node.rel || "").toLowerCase();
      const as = String((node.getAttribute && node.getAttribute("as")) || node.as || "").toLowerCase();
      const href = String((node.getAttribute && node.getAttribute("href")) || node.href || "");
      if (rel === "preload" && as === "script" && href.includes("bootstrap-feature-textlayout")) {
        return href;
      }
    }
    return "/gosx/bootstrap-feature-textlayout.js";
  }

  function loadTextLayoutEngine() {
    if (textLayoutEngineImpl) {
      return Promise.resolve(true);
    }
    if (textLayoutEngineLoad) {
      return textLayoutEngineLoad;
    }
    const src = textLayoutEngineChunkURL();
    textLayoutEngineLoad = new Promise(function(resolve) {
      const script = document.createElement("script");
      script.src = src;
      script.setAttribute("data-gosx-script", "feature-textlayout");
      const nonce = gosxBootstrapScriptNonce();
      if (nonce) {
        script.nonce = nonce;
      }
      script.onload = function() {
        // The chunk registers itself when it holds no feature host. When a host
        // exists the host runs the factory, so nothing more happens here.
        resolve(textLayoutEngineImpl !== null);
      };
      script.onerror = function() {
        // Text layout is a progressive enhancement: without the engine the
        // browser wraps the text itself. Warn once and let the page run.
        console.warn("[gosx] failed to load the text layout engine chunk: " + src);
        textLayoutEngineLoad = null;
        resolve(false);
      };
      (document.head || document.documentElement).appendChild(script);
    });
    return textLayoutEngineLoad;
  }

  window.__gosx_load_text_layout_engine = loadTextLayoutEngine;

  function maybeLoadTextLayoutEngineForDocument() {
    if (textLayoutEngineImpl) {
      return;
    }
    if (typeof window.__gosx_register_bootstrap_feature === "function") {
      // A selective runtime bundle owns the decision. Stand aside.
      return;
    }
    if (gosxDocumentHasTextLayout(document)) {
      loadTextLayoutEngine();
    }
  }

  // Wait for the rest of the bundle to run before deciding. bootstrap.js
  // executes the engine a few statements below this one, and bootstrap-runtime.js
  // installs its feature host later in the same bundle. A microtask lands after
  // both, so the check never injects a chunk the bundle already carries.
  if (document.readyState === "loading") {
    document.addEventListener("DOMContentLoaded", maybeLoadTextLayoutEngineForDocument);
  } else if (typeof queueMicrotask === "function") {
    queueMicrotask(maybeLoadTextLayoutEngineForDocument);
  } else {
    Promise.resolve().then(maybeLoadTextLayoutEngineForDocument);
  }
