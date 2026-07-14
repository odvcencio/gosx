// 26-runtime-dom.js — one core lifecycle for replacing server-rendered HTML.
//
// Actions, regions, and future browser surfaces can replace a fragment without
// each reimplementing the same dispose → swap → enhance sequence. The bridge
// deliberately stays HTML-first: callers can continue to provide complete
// server-rendered fallback markup and opt into the runtime only when bootstrap
// is present.
(function () {
  "use strict";

  if (typeof window === "undefined" || typeof document === "undefined") return;
  if (window.__gosx_runtime_dom) return;

  function linkAbort(parent, child) {
    if (!parent || !child || typeof parent.addEventListener !== "function") return function () {};
    if (parent.aborted) {
      child.abort();
      return function () {};
    }
    const onAbort = () => child.abort();
    parent.addEventListener("abort", onAbort, { once: true });
    return () => {
      if (typeof parent.removeEventListener === "function") parent.removeEventListener("abort", onAbort);
    };
  }

  // A scoped DOM bridge gives optional surfaces the same lifecycle discipline
  // as the core HTML replacement path. It is intentionally small: products
  // own their semantics, while GoSX owns listener cleanup, query scoping, and
  // parent-signal composition.
  function createDOMScope(root, parentSignal) {
    const host = root || document.body || document.documentElement;
    const controller = typeof AbortController === "function" ? new AbortController() : null;
    const cleanups = new Set();
    const ownedNodes = new Map();
    const parentCleanup = linkAbort(parentSignal, controller);
    const removeOwnedNode = (node) => {
      if (!node) return;
      if (node.parentNode && typeof node.parentNode.removeChild === "function") {
        node.parentNode.removeChild(node);
      } else if (typeof node.remove === "function") {
        node.remove();
      }
    };
    const own = (node, options) => {
      if (!node) return function () {};
      const config = options || {};
      const record = { node, remove: config.remove !== false };
      ownedNodes.set(node, record);
      const release = () => {
        if (ownedNodes.get(node) !== record) return;
        ownedNodes.delete(node);
        if (record.remove) removeOwnedNode(node);
      };
      return release;
    };
    const listen = (target, type, listener, options) => {
      if (!target || typeof target.addEventListener !== "function") return function () {};
      const config = typeof options === "boolean" ? { capture: options } : Object.assign({}, options || {});
      if (controller) config.signal = controller.signal;
      target.addEventListener(type, listener, config);
      const cleanup = () => {
        target.removeEventListener(type, listener, typeof options === "boolean" ? options : config);
        cleanups.delete(cleanup);
      };
      cleanups.add(cleanup);
      return cleanup;
    };
    const clearScope = () => {
      for (const cleanup of Array.from(cleanups)) cleanup();
      cleanups.clear();
      for (const record of Array.from(ownedNodes.values())) {
        if (record.remove) removeOwnedNode(record.node);
      }
      ownedNodes.clear();
      parentCleanup();
    };
    const dispose = () => {
      if (controller) {
        controller.abort();
        return;
      }
      clearScope();
    };
    if (controller) controller.signal.addEventListener("abort", clearScope, { once: true });
    return {
      root: host,
      signal: controller ? controller.signal : parentSignal || null,
      listen,
      on: listen,
      query(selector) {
        return host && typeof host.querySelector === "function" ? host.querySelector(selector) : null;
      },
      queryAll(selector) {
        return host && typeof host.querySelectorAll === "function" ? Array.from(host.querySelectorAll(selector)) : [];
      },
      contains(node) {
        return !!(host && typeof host.contains === "function" && host.contains(node));
      },
      // Own a node mounted outside the surface root (for example, a dialog
      // portaled to document.body). Releasing the returned handle removes it;
      // disposing the scope releases every owned node automatically.
      own,
      portal: own,
      dispatch(type, detail) {
        const targetDocument = host && host.ownerDocument ? host.ownerDocument : document;
        if (targetDocument && typeof targetDocument.dispatchEvent === "function" && typeof CustomEvent === "function") {
          targetDocument.dispatchEvent(new CustomEvent(type, { detail }));
        }
      },
      dispose,
      abort: dispose,
    };
  }

  function emit(type, detail) {
    if (typeof document.dispatchEvent !== "function" || typeof CustomEvent !== "function") return;
    document.dispatchEvent(new CustomEvent(type, { detail }));
  }

  function observe(level, message, fields) {
    if (typeof window.__gosx_emit !== "function") return;
    window.__gosx_emit(level, "runtime-dom", message, fields || {});
  }

  function reportRuntimeDOMFailure(operation, target, error, fields) {
    const metadata = Object.assign({
      scope: "runtime-dom",
      type: "replace",
      fallback: "server",
      element: target,
    }, fields || {});
    if (window.__gosx && typeof window.__gosx.reportFailure === "function") {
      return window.__gosx.reportFailure(operation, error, metadata);
    }
    if (window.__gosx && typeof window.__gosx.reportIssue === "function") {
      return window.__gosx.reportIssue(Object.assign({ error }, metadata));
    }
    console.error("[gosx] runtime DOM operation failed:", error);
    return null;
  }

  function disposeRuntimeContent(root, options) {
    const host = root || document.body || document.documentElement;
    const config = options || {};
    if (window.__gosx && window.__gosx.motion && typeof window.__gosx.motion.disposeAll === "function") {
      window.__gosx.motion.disposeAll(host);
    }
    if (window.__gosx && window.__gosx.textLayout && typeof window.__gosx.textLayout.disposeAll === "function") {
      window.__gosx.textLayout.disposeAll(host);
    }
    if (typeof window.__gosx_dispose_declarative_regions === "function") {
      window.__gosx_dispose_declarative_regions(host, {
        preserveRoot: config.preserveRegionRoot === true,
      });
    }
    if (typeof window.__gosx_dispose_runtime_surfaces === "function") {
      window.__gosx_dispose_runtime_surfaces(host);
    }
    return host;
  }

  function mountRuntimeContent(root) {
    const host = root || document.body || document.documentElement;
    if (!host) return null;
    if (typeof window.__gosx_mount_stream_templates === "function") {
      window.__gosx_mount_stream_templates(host);
    }
    if (typeof window.__gosx_apply_scene_command_scripts === "function") {
      window.__gosx_apply_scene_command_scripts(host);
    }
    if (window.__gosx && window.__gosx.motion && typeof window.__gosx.motion.mountAll === "function") {
      window.__gosx.motion.mountAll(host);
    }
    if (window.__gosx && window.__gosx.textLayout && typeof window.__gosx.textLayout.mountAll === "function") {
      window.__gosx.textLayout.mountAll(host);
    }
    if (typeof window.__gosx_mount_runtime_surfaces === "function") {
      window.__gosx_mount_runtime_surfaces(host);
    }
    if (typeof window.__gosx_mount_declarative_regions === "function") {
      window.__gosx_mount_declarative_regions(host);
    }
    return host;
  }

  function replaceRuntimeContent(target, html) {
    if (!target || typeof target.innerHTML !== "string") return false;
    emit("gosx:runtime:content:before", { target });
    observe("debug", "runtime content replacement started", { target: target.id || "" });
    disposeRuntimeContent(target, { preserveRegionRoot: true });
    try {
      target.innerHTML = String(html == null ? "" : html);
      mountRuntimeContent(target);
      emit("gosx:runtime:content:after", { target });
      observe("info", "runtime content replacement completed", { target: target.id || "" });
      return true;
    } catch (error) {
      reportRuntimeDOMFailure("replace", target, error);
      emit("gosx:runtime:content:error", { target, error });
      observe("error", "runtime content replacement failed", { target: target.id || "" });
      return false;
    }
  }

  // Replace a marker element with a DocumentFragment while preserving the
  // framework-owned lifecycle. HTML replacement can keep a region root and
  // mount one host; streamed templates replace the marker itself and may
  // contain several top-level nodes, so each inserted element becomes a new
  // mount root after the old marker is disposed.
  function replaceRuntimeFragment(target, fragment) {
    if (!target || !target.parentNode || !fragment) return false;
    const parent = target.parentNode;
    const inserted = fragment.childNodes ? Array.from(fragment.childNodes) : [];
    const detail = { target, mode: "fragment" };
    emit("gosx:runtime:content:before", detail);
    observe("debug", "runtime fragment replacement started", { target: target.id || "" });
    disposeRuntimeContent(target, { preserveRegionRoot: false });
    try {
      if (typeof target.replaceWith === "function") {
        target.replaceWith(fragment);
      } else if (typeof parent.replaceChild === "function") {
        parent.replaceChild(fragment, target);
      } else {
        return false;
      }
      for (const node of inserted) {
        if (node && node.nodeType === 1) mountRuntimeContent(node);
      }
      const after = Object.assign({}, detail, { nodes: inserted });
      emit("gosx:runtime:content:after", after);
      observe("info", "runtime fragment replacement completed", {
        target: target.id || "",
        nodes: inserted.length,
      });
      return true;
    } catch (error) {
      reportRuntimeDOMFailure("replace-fragment", target, error, { type: "stream" });
      emit("gosx:runtime:content:error", Object.assign({}, detail, { error }));
      observe("error", "runtime fragment replacement failed", { target: target.id || "" });
      return false;
    }
  }

  // Wrap a product-owned DOM reconciliation in the same lifecycle used for
  // server fragment replacement. The updater runs after old enhancements are
  // disposed and before the new subtree is mounted, so streaming surfaces do
  // not need to manually close dialogs, abort work, or re-run sibling
  // enhancers after changing their HTML.
  function reconcileRuntimeContent(target, updater, options) {
    if (!target || typeof updater !== "function") return null;
    const config = options || {};
    const detail = { target, mode: "reconcile" };
    emit("gosx:runtime:content:before", detail);
    observe("debug", "runtime content reconciliation started", { target: target.id || "" });
    disposeRuntimeContent(target, {
      preserveRegionRoot: config.preserveRegionRoot !== false,
    });
    const existingDOM = window.__gosx_runtime_dom;
    if (existingDOM && typeof existingDOM.beginTransaction === "function") {
      existingDOM.beginTransaction();
    }
    try {
      const result = updater(target);
      mountRuntimeContent(target);
      emit("gosx:runtime:content:after", Object.assign({}, detail, { result }));
      observe("info", "runtime content reconciliation completed", { target: target.id || "" });
      if (result && typeof result === "object") {
        return Object.assign({}, result, { lifecycle: "mounted" });
      }
      return { changed: true, result, lifecycle: "mounted" };
    } catch (error) {
      reportRuntimeDOMFailure("reconcile", target, error, { type: "reconcile" });
      emit("gosx:runtime:content:error", Object.assign({}, detail, { error }));
      observe("error", "runtime content reconciliation failed", { target: target.id || "" });
      return null;
    } finally {
      if (existingDOM && typeof existingDOM.endTransaction === "function") {
        existingDOM.endTransaction();
      }
    }
  }

  window.__gosx_mount_runtime_content = mountRuntimeContent;
  window.__gosx_dispose_runtime_content = disposeRuntimeContent;
  window.__gosx_replace_runtime_content = replaceRuntimeContent;
  window.__gosx_replace_runtime_fragment = replaceRuntimeFragment;
  window.__gosx_reconcile_runtime_content = reconcileRuntimeContent;
  const existingDOM = window.__gosx && window.__gosx.dom && typeof window.__gosx.dom === "object"
    ? window.__gosx.dom
    : {};
  window.__gosx_runtime_dom = Object.assign(existingDOM, {
    mount: mountRuntimeContent,
    dispose: disposeRuntimeContent,
    replace: replaceRuntimeContent,
    replaceFragment: replaceRuntimeFragment,
    reconcile: reconcileRuntimeContent,
    scope: createDOMScope,
    beginTransaction() {
      existingDOM._lifecycleDepth = Number(existingDOM._lifecycleDepth || 0) + 1;
    },
    endTransaction() {
      existingDOM._lifecycleDepth = Math.max(0, Number(existingDOM._lifecycleDepth || 0) - 1);
    },
    inTransaction() {
      return Number(existingDOM._lifecycleDepth || 0) > 0;
    },
  });
  if (window.__gosx) window.__gosx.dom = window.__gosx_runtime_dom;
})();
