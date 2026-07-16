// GoSX standalone prose runtime.
//
// This is the no-bootstrap fallback for generic keyed HTML/block
// reconciliation. Optional products may layer a domain signature on top, but
// the generic stream algorithm belongs to the core prose surface.
(function () {
  "use strict";

  if (typeof window === "undefined" || typeof document === "undefined") return;

  var ELEMENT_NODE = 1;
  var TEXT_NODE = 3;

  function nodeSignature(node, signature) {
    if (typeof signature === "function") return signature(node);
    if (!node) return "";
    if (node.nodeType === ELEMENT_NODE) return node.outerHTML || "";
    return String(node.nodeType) + ":" + String(node.nodeValue || "");
  }

  function parseHTML(html) {
    var template = document.createElement("template");
    template.innerHTML = String(html || "").trim();
    return Array.from(template.content.childNodes).filter(function (node) {
      return node.nodeType !== TEXT_NODE || String(node.nodeValue || "").trim() !== "";
    });
  }

  function reconcileHTMLFallback(root, html, options) {
    if (!root) return { firstChanged: 0, changed: false };
    var next = parseHTML(html);
    var current = Array.from(root.childNodes);
    var firstChanged = 0;
    while (
      firstChanged < current.length
      && firstChanged < next.length
      && nodeSignature(current[firstChanged], options && options.signature) === nodeSignature(next[firstChanged], options && options.signature)
    ) {
      firstChanged += 1;
    }
    for (var index = current.length - 1; index >= next.length; index -= 1) {
      root.removeChild(current[index]);
    }
    for (var replaceIndex = firstChanged; replaceIndex < next.length; replaceIndex += 1) {
      var existing = root.childNodes[replaceIndex];
      if (existing) root.replaceChild(next[replaceIndex], existing);
      else root.appendChild(next[replaceIndex]);
    }
    return {
      firstChanged: firstChanged,
      changed: firstChanged < current.length || firstChanged < next.length,
    };
  }

  function blockKey(node, keyAttribute, fallback) {
    if (!node || typeof node.getAttribute !== "function") return String(fallback);
    return String(node.getAttribute(keyAttribute) || fallback);
  }

  function reconcileBlocksFallback(root, blocks, options) {
    if (!root) return { firstChanged: 0, changed: false };
    var config = options || {};
    var keyAttribute = String(config.keyAttribute || "data-gosx-stream-key");
    var signature = config.signature;
    var currentByKey = new Map();
    Array.from(root.children || []).forEach(function (node, index) {
      currentByKey.set(blockKey(node, keyAttribute, index), node);
    });

    var next = [];
    var keys = [];
    for (var index = 0; index < (blocks || []).length; index += 1) {
      var block = blocks[index] || {};
      var nodes = parseHTML(block.html);
      if (nodes.length !== 1 || nodes[0].nodeType !== ELEMENT_NODE) continue;
      var node = nodes[0];
      var key = String(block.key == null ? (block.id == null ? index : block.id) : block.key);
      node.setAttribute(keyAttribute, key);
      next.push(node);
      keys.push(key);
    }

    var current = Array.from(root.children || []);
    var keySet = new Set(keys);
    var firstChanged = 0;
    while (
      firstChanged < current.length
      && firstChanged < next.length
      && blockKey(current[firstChanged], keyAttribute, firstChanged) === keys[firstChanged]
      && nodeSignature(current[firstChanged], signature) === nodeSignature(next[firstChanged], signature)
    ) {
      firstChanged += 1;
    }

    for (var currentIndex = 0; currentIndex < current.length; currentIndex += 1) {
      if (!keySet.has(blockKey(current[currentIndex], keyAttribute, currentIndex))) current[currentIndex].remove();
    }
    for (var nextIndex = firstChanged; nextIndex < next.length; nextIndex += 1) {
      var nextKey = keys[nextIndex];
      var retained = currentByKey.get(nextKey);
      if (retained && nodeSignature(retained, signature) === nodeSignature(next[nextIndex], signature)) {
        if (root.children[nextIndex] !== retained) root.insertBefore(retained, root.children[nextIndex] || null);
        continue;
      }
      var anchor = root.children[nextIndex];
      if (anchor) root.replaceChild(next[nextIndex], anchor);
      else root.appendChild(next[nextIndex]);
    }
    while (root.children.length > next.length) root.lastElementChild.remove();
    return {
      firstChanged: firstChanged,
      changed: firstChanged < current.length || firstChanged < next.length,
    };
  }

  function streamAPI() {
    return window.__gosx && window.__gosx.stream;
  }

  var proseAPI = {
    reconcileHTML: function (root, html, options) {
      var stream = streamAPI();
      return stream && typeof stream.reconcileHTML === "function"
        ? stream.reconcileHTML(root, html, options || {})
        : reconcileHTMLFallback(root, html, options || {});
    },
    reconcileBlocks: function (root, blocks, options) {
      var stream = streamAPI();
      return stream && typeof stream.reconcileBlocks === "function"
        ? stream.reconcileBlocks(root, blocks, options || {})
        : reconcileBlocksFallback(root, blocks, options || {});
    },
    createBlockStream: function (root, options) {
      var stream = streamAPI();
      if (stream && typeof stream.createBlockStream === "function") {
        return stream.createBlockStream(root, options || {});
      }
      return {
        update: function (blocks) { return proseAPI.reconcileBlocks(root, blocks, options || {}); },
        clear: function () { return proseAPI.reconcileBlocks(root, [], options || {}); },
      };
    },
  };

  window.GosxProse = Object.assign(window.GosxProse || {}, proseAPI);
  window.__gosx = window.__gosx || {};
  window.__gosx.prose = Object.assign(window.__gosx.prose || {}, proseAPI);
})();
