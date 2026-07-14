// 26-runtime-blocks.js — generic keyed HTML stream reconciliation.
//
// The algorithm is framework-owned; optional surfaces can provide a signature
// hook when a subtree has domain-specific enhancement state. gosx/editor uses
// that hook for Markdown++ diagrams while keeping the reconciler itself in the
// core bootstrap.
(function () {
  "use strict";

  if (typeof window === "undefined" || typeof document === "undefined") return;
  if (window.__gosx_runtime_blocks) return;

  function defaultNodeSignature(node, options) {
    if (!node) return "";
    if (node.nodeType === 1) {
      const clone = node.cloneNode(true);
      const keyAttribute = options && options.keyAttribute || "data-gosx-stream-key";
      if (clone.removeAttribute) clone.removeAttribute(keyAttribute);
      return clone.outerHTML || "";
    }
    return `${node.nodeType}:${node.nodeValue || ""}`;
  }

  function nodeSignature(node, options) {
    if (options && typeof options.signature === "function") {
      return String(options.signature(node) || "");
    }
    return defaultNodeSignature(node, options);
  }

  function parseHTML(html) {
    const template = document.createElement("template");
    template.innerHTML = String(html || "").trim();
    return Array.from(template.content.childNodes).filter((node) => {
      return node.nodeType !== 3 || String(node.nodeValue || "").trim() !== "";
    });
  }

  function lifecycleActive() {
    const runtimeDOM = window.__gosx_runtime_dom
      || (window.__gosx && window.__gosx.dom);
    if (!runtimeDOM) return false;
    if (typeof runtimeDOM.inTransaction === "function") return !runtimeDOM.inTransaction();
    return runtimeDOM._lifecycleDepth > 0 ? false : true;
  }

  function disposeNode(node) {
    if (!lifecycleActive() || !node) return;
    if (typeof window.__gosx_dispose_runtime_content === "function") {
      window.__gosx_dispose_runtime_content(node, { preserveRegionRoot: false });
    } else if (window.__gosx && window.__gosx.dom && typeof window.__gosx.dom.dispose === "function") {
      window.__gosx.dom.dispose(node, { preserveRegionRoot: false });
    }
  }

  function mountNode(node) {
    if (!lifecycleActive() || !node) return;
    if (typeof window.__gosx_mount_runtime_content === "function") {
      window.__gosx_mount_runtime_content(node);
    } else if (window.__gosx && window.__gosx.dom && typeof window.__gosx.dom.mount === "function") {
      window.__gosx.dom.mount(node);
    }
  }

  function reconcileNodes(root, next, options) {
    const current = Array.from(root.childNodes || []);
    let firstChanged = 0;
    while (
      firstChanged < current.length &&
      firstChanged < next.length &&
      nodeSignature(current[firstChanged], options) === nodeSignature(next[firstChanged], options)
    ) {
      firstChanged += 1;
    }

    for (let index = current.length - 1; index >= next.length; index -= 1) {
      disposeNode(current[index]);
      root.removeChild(current[index]);
    }
    for (let index = firstChanged; index < next.length; index += 1) {
      const existing = root.childNodes[index];
      if (existing) {
        disposeNode(existing);
        root.replaceChild(next[index], existing);
      } else {
        root.appendChild(next[index]);
      }
      mountNode(next[index]);
    }
    return {
      firstChanged,
      changed: firstChanged < current.length || firstChanged < next.length,
    };
  }

  function reconcileHTML(root, html, options) {
    if (!root) return { firstChanged: 0, changed: false };
    return reconcileNodes(root, parseHTML(html), options || {});
  }

  function blockKey(node, keyAttribute) {
    if (!node) return "";
    return String(node.getAttribute ? node.getAttribute(keyAttribute) || "" : "");
  }

  function setBlockKey(node, keyAttribute, key) {
    if (node && typeof node.setAttribute === "function") {
      node.setAttribute(keyAttribute, key);
    }
  }

  function reconcileBlocks(root, blocks, options) {
    if (!root) return { firstChanged: 0, changed: false };
    const config = options || {};
    const keyAttribute = config.keyAttribute || "data-gosx-stream-key";
    const currentByKey = new Map();
    Array.from(root.children || []).forEach((node, index) => {
      currentByKey.set(blockKey(node, keyAttribute) || String(index), node);
    });

    const next = [];
    const keys = [];
    for (const [index, block] of (blocks || []).entries()) {
      const nodes = parseHTML(block && block.html);
      if (nodes.length !== 1 || nodes[0].nodeType !== 1) continue;
      const node = nodes[0];
      const key = String(block.key ?? block.id ?? index);
      setBlockKey(node, keyAttribute, key);
      next.push(node);
      keys.push(key);
    }

    const current = Array.from(root.children || []);
    const desired = [];
    const retained = new Set();
    for (let index = 0; index < next.length; index += 1) {
      const existing = currentByKey.get(keys[index]);
      if (existing && nodeSignature(existing, config) === nodeSignature(next[index], config)) {
        desired.push(existing);
        retained.add(existing);
      } else {
        desired.push(next[index]);
      }
    }

    for (const node of current) {
      if (retained.has(node)) continue;
      disposeNode(node);
      root.removeChild(node);
    }

    let firstChanged = 0;
    while (
      firstChanged < current.length &&
      firstChanged < next.length &&
      blockKey(current[firstChanged], keyAttribute) === keys[firstChanged] &&
      nodeSignature(current[firstChanged], config) === nodeSignature(next[firstChanged], config)
    ) {
      firstChanged += 1;
    }

    for (let index = 0; index < desired.length; index += 1) {
      const node = desired[index];
      const anchor = root.children[index] || null;
      if (node.parentNode === root) {
        if (anchor !== node) root.insertBefore(node, anchor);
        continue;
      }
      if (anchor) root.insertBefore(node, anchor);
      else root.appendChild(node);
      mountNode(node);
    }
    return {
      firstChanged,
      changed: firstChanged < current.length || firstChanged < next.length,
    };
  }

  function createBlockStream(root, options) {
    return {
      update(blocks) {
        return reconcileBlocks(root, blocks, options);
      },
      clear() {
        return reconcileBlocks(root, [], options);
      },
    };
  }

  const api = { reconcileHTML, reconcileBlocks, createBlockStream };
  window.__gosx_runtime_blocks = api;
  window.__gosx_reconcile_html = reconcileHTML;
  window.__gosx_reconcile_blocks = reconcileBlocks;
  window.__gosx_create_block_stream = createBlockStream;
  // Publish the same generic prose contract as the standalone prose asset.
  // Products may layer domain signatures on top, but the keyed stream engine
  // remains one bootstrap-owned implementation whenever bootstrap is present.
  window.GosxProse = Object.assign(window.GosxProse || {}, api);
  if (window.__gosx) {
    window.__gosx.stream = Object.assign(window.__gosx.stream || {}, api);
    window.__gosx.streamBlocks = api;
    window.__gosx.prose = Object.assign(window.__gosx.prose || {}, api);
  }
})();
