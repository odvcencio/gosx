// Markdown++ prose adapter layered on the core GoSX prose runtime.
//
// The generic standalone fallback is served by m31labs.dev/gosx/prose. This
// adapter contributes only the editor-owned diagram signature and key
// attribute; the editor does not carry a second reconciliation algorithm.
(function () {
  "use strict";

  if (typeof window === "undefined") return;

  var ELEMENT_NODE = 1;

  function nodeSignature(node) {
    if (!node) return "";
    if (node.nodeType === ELEMENT_NODE) {
      var clone = node.cloneNode(true);
      clone.removeAttribute("data-gosx-prose-key");
      var diagrams = [];
      if (clone.matches && clone.matches(".mdpp-diagram")) diagrams.push(clone);
      if (clone.querySelectorAll) diagrams.push.apply(diagrams, clone.querySelectorAll(".mdpp-diagram"));
      for (var index = 0; index < diagrams.length; index += 1) {
        var diagram = diagrams[index];
        var source = diagram.querySelector(".mdpp-diagram-source pre code") || diagram.querySelector("pre code");
        diagram.replaceChildren();
        diagram.removeAttribute("data-mdpp-diagram-state");
        diagram.setAttribute("data-gosx-prose-diagram", source ? source.textContent : "");
      }
      return clone.outerHTML || "";
    }
    return String(node.nodeType) + ":" + String(node.nodeValue || "");
  }

  var streamOptions = {
    keyAttribute: "data-gosx-prose-key",
    signature: nodeSignature,
  };

  var baseProse = window.__gosx && window.__gosx.prose
    ? window.__gosx.prose
    : (window.GosxProse || null);

  function coreProse() {
    if (baseProse) return baseProse;
    return window.__gosx && window.__gosx.stream ? window.__gosx.stream : null;
  }

  function missingRuntime() {
    return { firstChanged: 0, changed: false };
  }

  var adapter = {
    reconcileHTML: function (root, html) {
      var prose = coreProse();
      return prose && typeof prose.reconcileHTML === "function"
        ? prose.reconcileHTML(root, html, streamOptions)
        : missingRuntime();
    },
    reconcileBlocks: function (root, blocks) {
      var prose = coreProse();
      return prose && typeof prose.reconcileBlocks === "function"
        ? prose.reconcileBlocks(root, blocks, streamOptions)
        : missingRuntime();
    },
    createBlockStream: function (root) {
      var prose = coreProse();
      if (prose && typeof prose.createBlockStream === "function") {
        return prose.createBlockStream(root, streamOptions);
      }
      return { update: missingRuntime, clear: missingRuntime };
    },
  };

  window.GosxProse = Object.assign(window.GosxProse || {}, adapter);
  if (window.__gosx) window.__gosx.prose = Object.assign(window.__gosx.prose || {}, adapter);
})();
