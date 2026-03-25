// GoSX DOM Patch Applier
// Receives structured PatchOp objects from the WASM reconciler and applies them
// to the DOM. This is NOT a differ — the Go WASM side does all diffing; this
// module only executes the resulting operations.
//
// Patch operations match the Go PatchKind enum in vm/ops.go:
//   0 = SetText        4 = RemoveElement
//   1 = SetAttr        5 = ReplaceElement
//   2 = RemoveAttr     6 = Reorder
//   3 = CreateElement  7 = SetValue

(function () {
  "use strict";

  // ---------------------------------------------------------------------------
  // Constants
  // ---------------------------------------------------------------------------

  // PatchKind enum — must stay in sync with Go's vm.PatchKind iota.
  var PATCH_SET_TEXT        = 0;
  var PATCH_SET_ATTR        = 1;
  var PATCH_REMOVE_ATTR     = 2;
  var PATCH_CREATE_ELEMENT  = 3;
  var PATCH_REMOVE_ELEMENT  = 4;
  var PATCH_REPLACE_ELEMENT = 5;
  var PATCH_REORDER         = 6;
  var PATCH_SET_VALUE       = 7;

  // Elements that browsers may insert implicitly (e.g. <tbody> inside <table>).
  // When walking a path we need to skip through these so that the path indices
  // from the Go VDOM still line up with the real DOM.
  var IMPLICIT_ELEMENTS = new Set([
    "TBODY", "THEAD", "TFOOT", "COLGROUP",
  ]);

  // DOM properties that should be set directly on the element rather than via
  // setAttribute.  These are boolean-ish or value-holding properties.
  var PROP_ATTRS = new Set([
    "value", "checked", "selected", "disabled",
  ]);

  // Boolean properties — their presence is what matters, not their string value.
  var BOOL_PROPS = new Set([
    "checked", "selected", "disabled",
  ]);

  // ---------------------------------------------------------------------------
  // Entry point — called from WASM via the bridge
  // ---------------------------------------------------------------------------

  /**
   * Apply a batch of patch operations to an island's DOM subtree.
   *
   * @param {string} islandID  - The DOM id of the island root element.
   * @param {string} patchOpsJSON - JSON-encoded array of PatchOp objects.
   */
  window.__gosx_apply_patches = function (islandID, patchOpsJSON) {
    var root = document.getElementById(islandID);
    if (!root) {
      if (typeof console !== "undefined") {
        console.warn("[gosx/patch] island root #" + islandID + " not found");
      }
      return;
    }

    var ops;
    try {
      ops = JSON.parse(patchOpsJSON);
    } catch (e) {
      if (typeof console !== "undefined") {
        console.error("[gosx/patch] failed to parse patch ops:", e);
      }
      return;
    }

    if (!ops || ops.length === 0) return;

    // Preserve the user's focus and selection so patching doesn't disrupt them.
    var focusState = saveFocus(root);

    // Apply every operation in order.  The WASM side guarantees a safe ordering
    // (e.g. removals go last-to-first so indices stay valid).
    for (var i = 0; i < ops.length; i++) {
      applyPatch(root, ops[i]);
    }

    // Restore focus / cursor position.
    restoreFocus(focusState);
  };

  // ---------------------------------------------------------------------------
  // Path resolution
  // ---------------------------------------------------------------------------

  /**
   * Resolve a slash-separated index path to a DOM node relative to an island
   * root.  For example "0/2/1" means root → child[0] → child[2] → child[1].
   *
   * Returns null if the path is invalid or the target no longer exists.
   *
   * @param {Element} root - The island root element.
   * @param {string}  path - Slash-separated child indices, or "" for root.
   * @returns {Node|null}
   */
  function resolvePath(root, path) {
    if (!path || path === "") return root;

    var indices = path.split("/");
    var node = root;

    for (var i = 0; i < indices.length; i++) {
      var idx = parseInt(indices[i], 10);
      if (isNaN(idx)) return null;

      // Skip browser-implicit wrapper elements (e.g. auto-inserted <tbody>).
      node = skipImplicit(node);

      var children = getEffectiveChildren(node);
      if (idx < 0 || idx >= children.length) return null;
      node = children[idx];
    }

    return node;
  }

  /**
   * If a node has exactly one element child and that child is an implicit
   * wrapper (TBODY, THEAD, etc.), skip into it.  This keeps Go-side indices
   * aligned with the browser DOM.
   *
   * @param {Node} node
   * @returns {Node}
   */
  function skipImplicit(node) {
    // Only element nodes can have .children
    if (node.nodeType !== Node.ELEMENT_NODE) return node;

    if (
      node.children.length === 1 &&
      IMPLICIT_ELEMENTS.has(node.children[0].tagName)
    ) {
      return node.children[0];
    }
    return node;
  }

  /**
   * Return the "effective" child list of a node — elements and text nodes,
   * excluding comment nodes and other non-renderable nodes.  This matches the
   * child list the Go VDOM sees.
   *
   * @param {Node} node
   * @returns {Node[]}
   */
  function getEffectiveChildren(node) {
    var result = [];
    var children = node.childNodes;
    for (var i = 0; i < children.length; i++) {
      var child = children[i];
      if (
        child.nodeType === Node.ELEMENT_NODE ||
        child.nodeType === Node.TEXT_NODE
      ) {
        result.push(child);
      }
    }
    return result;
  }

  // ---------------------------------------------------------------------------
  // Patch dispatcher
  // ---------------------------------------------------------------------------

  /**
   * Apply a single PatchOp to the DOM.
   *
   * @param {Element} root - Island root element.
   * @param {Object}  op   - A PatchOp object (deserialized from JSON).
   */
  function applyPatch(root, op) {
    var target = resolvePath(root, op.path);

    if (!target) {
      if (typeof console !== "undefined") {
        console.warn(
          "[gosx/patch] could not resolve path: " + op.path +
          " (kind=" + op.kind + ")"
        );
      }
      return;
    }

    switch (op.kind) {
      case PATCH_SET_TEXT:
        applySetText(target, op);
        break;

      case PATCH_SET_ATTR:
        applySetAttr(target, op);
        break;

      case PATCH_REMOVE_ATTR:
        applyRemoveAttr(target, op);
        break;

      case PATCH_CREATE_ELEMENT:
        applyCreateElement(target, op);
        break;

      case PATCH_REMOVE_ELEMENT:
        applyRemoveElement(target);
        break;

      case PATCH_REPLACE_ELEMENT:
        applyReplaceElement(target, op);
        break;

      case PATCH_REORDER:
        applyReorder(target, op);
        break;

      case PATCH_SET_VALUE:
        applySetValue(target, op);
        break;

      default:
        if (typeof console !== "undefined") {
          console.warn("[gosx/patch] unknown patch kind: " + op.kind);
        }
    }
  }

  // ---------------------------------------------------------------------------
  // Individual patch operations
  // ---------------------------------------------------------------------------

  /**
   * Kind 0 — SetText: replace the text content of the target node.
   */
  function applySetText(target, op) {
    // Works for both text nodes and element nodes.
    // For text nodes, textContent sets the node's data.
    // For elements, it replaces all children with a single text node.
    target.textContent = op.text;
  }

  /**
   * Kind 1 — SetAttr: set an attribute or DOM property.
   * Boolean properties (checked, disabled, selected) are coerced to booleans.
   * The "value" property is set directly to avoid attribute/property mismatch.
   */
  function applySetAttr(target, op) {
    setAttr(target, op.attrName, op.text);
  }

  /**
   * Kind 2 — RemoveAttr: remove an attribute from the target element.
   * For DOM properties, we also reset the property value.
   */
  function applyRemoveAttr(target, op) {
    var name = op.attrName;

    target.removeAttribute(name);

    // Also clear the corresponding DOM property for property-backed attrs.
    if (BOOL_PROPS.has(name)) {
      target[name] = false;
    } else if (name === "value") {
      target.value = "";
    }
  }

  /**
   * Kind 3 — CreateElement: create a new element and insert it as a child of
   * the target (the path points to the *parent*).
   *
   * op.tag      — tag name for the new element (e.g. "div").
   * op.text     — optional initial text content.
   * op.children — optional [insertIndex] indicating position among siblings.
   */
  function applyCreateElement(target, op) {
    var el = document.createElement(op.tag);

    if (op.text) {
      el.textContent = op.text;
    }

    // Determine insertion position.  op.children[0] is the desired child
    // index; if omitted we append.
    var insertIdx =
      op.children && op.children.length > 0
        ? op.children[0]
        : target.childNodes.length;

    if (insertIdx < target.childNodes.length) {
      target.insertBefore(el, target.childNodes[insertIdx]);
    } else {
      target.appendChild(el);
    }
  }

  /**
   * Kind 4 — RemoveElement: remove the target node from its parent.
   */
  function applyRemoveElement(target) {
    if (target.parentNode) {
      target.parentNode.removeChild(target);
    }
  }

  /**
   * Kind 5 — ReplaceElement: replace the target node with a new element.
   *
   * op.tag  — tag name for the replacement element.
   * op.text — optional text content for the replacement.
   */
  function applyReplaceElement(target, op) {
    var newEl = document.createElement(op.tag);

    if (op.text) {
      newEl.textContent = op.text;
    }

    if (target.parentNode) {
      target.parentNode.replaceChild(newEl, target);
    }
  }

  /**
   * Kind 6 — Reorder: reorder the children of the target element according to
   * the index list in op.children.
   *
   * op.children — array of original child indices in the desired new order.
   *               e.g. [2, 0, 1] means: put old child 2 first, then 0, then 1.
   *
   * We snapshot the current children, build a document fragment in the desired
   * order, then swap the contents.  This is a single reflow.
   */
  function applyReorder(target, op) {
    if (!op.children || op.children.length === 0) return;

    var currentChildren = Array.from(target.childNodes);
    var fragment = document.createDocumentFragment();

    for (var i = 0; i < op.children.length; i++) {
      var idx = op.children[i];
      if (idx >= 0 && idx < currentChildren.length) {
        fragment.appendChild(currentChildren[idx]);
      }
    }

    // Remove any remaining children not included in the new order, then
    // append the reordered fragment.
    while (target.firstChild) {
      target.removeChild(target.firstChild);
    }
    target.appendChild(fragment);
  }

  /**
   * Kind 7 — SetValue: set an input's value while preserving cursor position.
   * This is separate from SetAttr because it needs special cursor handling to
   * avoid disrupting the user while they type.
   */
  function applySetValue(target, op) {
    setInputValue(target, op.text);
  }

  // ---------------------------------------------------------------------------
  // Attribute helpers
  // ---------------------------------------------------------------------------

  /**
   * Set an attribute or DOM property on an element.
   *
   * For "property" attributes (value, checked, selected, disabled) we write to
   * the DOM property directly, because setAttribute("value", ...) only sets the
   * default — it won't update a dirty input.
   *
   * @param {Element} el
   * @param {string}  name  - Attribute name.
   * @param {string}  value - Attribute value as a string.
   */
  function setAttr(el, name, value) {
    if (PROP_ATTRS.has(name)) {
      if (BOOL_PROPS.has(name)) {
        // Boolean: anything except "false" and "" is truthy.
        el[name] = value !== "false" && value !== "";
      } else {
        // "value" — set as string property.
        el[name] = value;
      }
    } else {
      el.setAttribute(name, value);
    }
  }

  // ---------------------------------------------------------------------------
  // Input value with cursor preservation
  // ---------------------------------------------------------------------------

  /**
   * Set an input (or textarea) value while preserving the cursor/selection
   * position.  If the value hasn't changed, this is a no-op — preventing
   * infinite update loops when the input is the source of the signal change.
   *
   * @param {HTMLInputElement|HTMLTextAreaElement} el
   * @param {string} value
   */
  function setInputValue(el, value) {
    // No-op guard: avoid touching the element if the value is already correct.
    // This is critical to prevent event-loop feedback when the user is typing
    // and their input triggers a signal that re-renders the same value.
    if (el.value === value) return;

    // Snapshot cursor position before mutating value.
    var start = el.selectionStart;
    var end = el.selectionEnd;

    el.value = value;

    // Restore cursor only if this element is currently focused and supports
    // selection (inputs of type text/textarea/search/url/tel/password).
    if (document.activeElement === el && start !== null) {
      // Clamp to new value length in case the value got shorter.
      el.selectionStart = Math.min(start, value.length);
      el.selectionEnd = Math.min(end, value.length);
    }
  }

  // ---------------------------------------------------------------------------
  // Focus preservation
  // ---------------------------------------------------------------------------

  /**
   * Capture the currently focused element's identity and selection state so we
   * can restore it after patching.  We record both the element's ID (fast
   * lookup) and its path from the island root (fallback when no ID is set).
   *
   * @param {Element} islandRoot
   * @returns {Object|null} Focus state descriptor, or null if nothing focused.
   */
  function saveFocus(islandRoot) {
    var active = document.activeElement;

    // Nothing interesting focused.
    if (!active || active === document.body || active === document.documentElement) {
      return null;
    }

    // Only save focus for elements inside this island.
    if (!islandRoot.contains(active)) return null;

    var state = {
      id: active.id || null,
      tagName: active.tagName,
      selectionStart: null,
      selectionEnd: null,
      path: null,
    };

    // Capture selection/cursor position if the element supports it.
    try {
      if (active.selectionStart !== undefined) {
        state.selectionStart = active.selectionStart;
        state.selectionEnd = active.selectionEnd;
      }
    } catch (e) {
      // Some input types (color, range, etc.) throw on selectionStart access.
    }

    // Build a path from the island root so we can find the element again even
    // if it doesn't have an ID.
    if (!state.id) {
      state.path = buildPathFromRoot(islandRoot, active);
    }

    return state;
  }

  /**
   * Restore focus and selection after patching.  Best-effort: if the target
   * element was removed or the DOM changed too drastically, we silently skip.
   *
   * @param {Object|null} state - Focus state from saveFocus().
   */
  function restoreFocus(state) {
    if (!state) return;

    var target = null;

    // Strategy 1: look up by ID (fast, reliable).
    if (state.id) {
      target = document.getElementById(state.id);
    }

    // Strategy 2: walk the saved path from island root.
    if (!target && state.path) {
      target = walkPath(state.path);
    }

    if (!target) return;

    // Only refocus if the tag matches — avoids focusing a completely different
    // element that happens to be at the same path after a major re-render.
    if (target.tagName !== state.tagName) return;

    target.focus();

    // Restore cursor / selection.
    if (state.selectionStart !== null && state.selectionStart !== undefined) {
      try {
        target.selectionStart = state.selectionStart;
        target.selectionEnd = state.selectionEnd;
      } catch (e) {
        // Not all focusable elements support selection (e.g. <button>).
      }
    }
  }

  /**
   * Build a path descriptor from an island root down to a descendant element.
   * The path is an array of objects recording each step: the parent element and
   * the child's index among its siblings.
   *
   * We store references to the actual parent nodes rather than just indices so
   * that walkPath can recover even if the DOM was partially mutated.
   *
   * @param {Element} root
   * @param {Element} target
   * @returns {Array|null}
   */
  function buildPathFromRoot(root, target) {
    var path = [];
    var node = target;

    while (node && node !== root) {
      var parent = node.parentNode;
      if (!parent) return null;

      // Find this node's index among its parent's children.
      var idx = childIndex(parent, node);
      if (idx === -1) return null;

      path.unshift({ parentId: parent.id || null, index: idx });
      node = parent;
    }

    // If we never reached root, the target wasn't inside the island.
    if (node !== root) return null;

    // Store root ID so walkPath knows where to start.
    return { rootId: root.id, steps: path };
  }

  /**
   * Walk a saved path to find the target element.  Returns null if the path is
   * no longer valid.
   *
   * @param {Object} path - Path descriptor from buildPathFromRoot().
   * @returns {Element|null}
   */
  function walkPath(path) {
    if (!path || !path.rootId) return null;

    var node = document.getElementById(path.rootId);
    if (!node) return null;

    for (var i = 0; i < path.steps.length; i++) {
      var step = path.steps[i];
      var children = node.childNodes;
      if (step.index < 0 || step.index >= children.length) return null;
      node = children[step.index];
    }

    return node;
  }

  /**
   * Find the index of a child node among its parent's childNodes.
   *
   * @param {Node} parent
   * @param {Node} child
   * @returns {number} Index, or -1 if not found.
   */
  function childIndex(parent, child) {
    var children = parent.childNodes;
    for (var i = 0; i < children.length; i++) {
      if (children[i] === child) return i;
    }
    return -1;
  }

})();
