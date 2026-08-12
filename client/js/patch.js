// GoSX DOM Patch Applier
// Receives structured PatchOp objects from the WASM reconciler and applies them
// to the DOM. This is NOT a differ — the Go WASM side does all diffing; this
// module only executes the resulting operations.
//
// Patch operations match the Go PatchKind enum in vm/ops.go:
//   0 = SetText        5 = RemoveElement
//   1 = SetAttr        6 = ReplaceElement
//   2 = RemoveAttr     7 = Reorder
//   3 = CreateElement  8 = SetValue
//   4 = CreateText     9 = SetHTML

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
  var PATCH_CREATE_TEXT     = 4;
  var PATCH_REMOVE_ELEMENT  = 5;
  var PATCH_REPLACE_ELEMENT = 6;
  var PATCH_REORDER         = 7;
  var PATCH_SET_VALUE       = 8;
  var PATCH_SET_HTML        = 9;

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

  // Missing paths can happen briefly when a high-frequency signal races a DOM
  // subtree replacement. The operation is already safe to skip; warn once per
  // path/kind so live game loops do not flood the console.
  var missingPatchPathWarnings = new Set();
  var MAX_MISSING_PATCH_WARNINGS = 64;

  // ---------------------------------------------------------------------------
  // Entry point — called from WASM via the bridge
  // ---------------------------------------------------------------------------

  /** Apply decoded patch operations to an island's DOM subtree. */
  function applyPatchOperations(islandID, ops) {
    var islandWrapper = document.getElementById(islandID);
    if (!islandWrapper) {
      if (typeof console !== "undefined") {
        console.warn("[gosx/patch] island root #" + islandID + " not found");
      }
      return;
    }
    // The island wrapper (id="gosx-island-N") contains the component root
    // as its first element child. Patch paths are relative to the component
    // root, not the wrapper.
    var root = islandWrapper.firstElementChild || islandWrapper;

    if (!ops || ops.length === 0) return;

    // Preserve the user's focus and selection so patching doesn't disrupt them.
    var focusState = saveFocus(root);

    // Apply every operation in order.  The WASM side guarantees a safe ordering
    // (e.g. removals go last-to-first so indices stay valid).
    beginPathCache(root);
    try {
      for (var i = 0; i < ops.length; i++) {
        applyPatch(root, ops[i]);
      }
    } finally {
      endPathCache();
    }

    // Restore focus / cursor position.
    restoreFocus(focusState);
  }

  /**
   * Apply a batch of patch operations from the compatibility JSON boundary.
   *
   * @param {string} islandID  - The DOM id of the island root element.
   * @param {string} patchOpsJSON - JSON-encoded array of PatchOp objects.
   */
  window.__gosx_apply_patches = function (islandID, patchOpsJSON) {
    var ops;
    try {
      ops = JSON.parse(patchOpsJSON);
    } catch (e) {
      if (typeof console !== "undefined") {
        console.error("[gosx/patch] failed to parse patch ops:", e);
      }
      return;
    }
    applyPatchOperations(islandID, ops);
  };

  /**
   * Apply the O4 binary patch mailbox. The browser decodes the payload once,
   * then reuses the exact same DOM applier as the JSON compatibility path.
   *
   * @param {string} islandID - Callback id retained for old bridge callers.
   * @param {Uint8Array|ArrayBuffer} mailbox - GoSX mailbox response.
   */
  window.__gosx_apply_patch_mailbox = function (islandID, mailbox) {
    try {
      var decoded = decodePatchMailbox(mailbox);
      var decodedIslandID = decoded.islandID || islandID;
      if (islandID && decodedIslandID !== islandID) {
        throw new Error("runtime patch mailbox island id mismatch");
      }
      applyPatchOperations(decodedIslandID, decoded.patches);
    } catch (e) {
      if (typeof console !== "undefined") {
        console.error("[gosx/patch] failed to decode patch mailbox:", e);
      }
    }
  };

  function decodePatchMailbox(input) {
    var runtimeMailbox = window.__gosx_runtime_mailbox;
    if (runtimeMailbox && typeof runtimeMailbox.decodePatchMailbox === "function") {
      return runtimeMailbox.decodePatchMailbox(input);
    }
    return decodePatchMailboxFallback(input);
  }

  // Keep patch.js independently loadable for pages that cache it before the
  // bootstrap chunk. This small fallback is byte-compatible with
  // client/runtime/wasm/mailbox.ts and is removed with the authored shim in O6.
  function decodePatchMailboxFallback(input) {
    var bytes = input instanceof Uint8Array ? input : new Uint8Array(input);
    var headerBytes = 24;
    if (bytes.byteLength < headerBytes) throw new Error("runtime mailbox is shorter than its header");
    var view = new DataView(bytes.buffer, bytes.byteOffset, bytes.byteLength);
    var magic = view.getUint32(0, true);
    var version = view.getUint16(4, true);
    var header = {
      magic: magic,
      version: version,
      opcode: view.getUint16(6, true),
      requestID: view.getUint32(8, true),
      payloadSize: view.getUint32(12, true),
      status: view.getInt32(16, true),
      flags: view.getUint32(20, true),
    };
    if (magic !== 0x4d585347 || version !== 1) throw new Error("runtime mailbox header is invalid");
    if (header.opcode !== 3 || (header.flags & 1) === 0) throw new Error("runtime mailbox is not a patch response");
    if (headerBytes + header.payloadSize !== bytes.byteLength) throw new Error("runtime mailbox length is invalid");
    var cursor = headerBytes;
    function stringValue() {
      if (cursor + 4 > view.byteLength) throw new Error("runtime mailbox string length is truncated");
      var length = view.getUint32(cursor, true);
      cursor += 4;
      if (length > view.byteLength - cursor) throw new Error("runtime mailbox string is truncated");
      var chars = "";
      for (var i = 0; i < length; i++) chars += String.fromCharCode(view.getUint8(cursor + i));
      cursor += length;
      return chars;
    }
    var result = { header: header, islandID: stringValue(), patches: [] };
    if (cursor + 4 > view.byteLength) throw new Error("runtime mailbox patch count is truncated");
    var count = view.getUint32(cursor, true);
    cursor += 4;
    for (var patchIndex = 0; patchIndex < count; patchIndex++) {
      if (cursor >= view.byteLength) throw new Error("runtime mailbox patch is truncated");
      var patch = {
        kind: view.getUint8(cursor++),
        path: stringValue(),
        tag: stringValue(),
        text: stringValue(),
        attrName: stringValue(),
        children: [],
      };
      if (cursor + 4 > view.byteLength) throw new Error("runtime mailbox child count is truncated");
      var childCount = view.getUint32(cursor, true);
      cursor += 4;
      if (childCount > (view.byteLength - cursor) / 4) throw new Error("runtime mailbox child list is truncated");
      for (var childIndex = 0; childIndex < childCount; childIndex++) {
        patch.children.push(view.getInt32(cursor, true));
        cursor += 4;
      }
      result.patches.push(patch);
    }
    if (cursor !== view.byteLength) throw new Error("runtime mailbox patch payload has trailing bytes");
    return result;
  }

  // ---------------------------------------------------------------------------
  // Path resolution cache
  // ---------------------------------------------------------------------------
  //
  // Ops arrive in depth-first order, so one batch repeats the same path
  // prefixes many times. Uncached, every op re-walks from the island root and
  // every step copies the child list: a 100-row list costs about 41,000
  // child-node visits to place 400 nodes. The cache holds one entry per
  // resolved prefix for the life of one batch, and each entry keeps a cursor
  // so a forward walk resumes where the last one stopped. One list build then
  // costs one pass over the children, not one pass per row.
  //
  // A stale entry patches the wrong element in silence, so every op that
  // changes a child list must dirty the entry owning that list. applyPatch
  // names the dirtied entry per kind. Attribute and value ops change no child
  // list and dirty nothing.

  // Root entry of the batch in flight, or null between batches. A null cache
  // sends every resolve down the plain walk.
  var pathCache = null;

  // Marks a path the fast walker refuses. Only slash-separated decimal digits
  // take the cached route; anything else falls back to the lenient parseInt
  // walk so odd paths keep their old behaviour.
  var MALFORMED = { malformed: true };

  /**
   * Build a cache entry.
   *
   * box is skipImplicit(node) — the node that owns the child indices. kids maps
   * effective child index to entry. cursor* memoize the last child resolved
   * here. attached is true only for entries reachable from the cache root; a
   * detached entry reads fine but its position is unknown, so dirtying it
   * dirties the whole cache.
   *
   * @param {Node} node
   * @param {boolean} attached
   */
  function newCacheEntry(node, attached) {
    return {
      node: node,
      attached: attached,
      box: null,
      kids: null,
      cursorIndex: -1,
      cursorRaw: -1,
      cursorNode: null,
    };
  }

  /** Install a fresh cache for one apply pass. */
  function beginPathCache(root) {
    pathCache = newCacheEntry(root, true);
  }

  /** Drop the cache so no entry outlives the batch that built it. */
  function endPathCache() {
    pathCache = null;
  }

  /**
   * Forget what an entry knows about its children. Clearing kids releases every
   * descendant, so one call covers the whole subtree below the changed list.
   *
   * @param {Object|null} entry
   */
  function dropChildState(entry) {
    if (!entry) return;
    // Unknown position: dirty the root rather than leave a stale descendant.
    if (!entry.attached) entry = pathCache;
    if (!entry) return;
    entry.box = null;
    entry.kids = null;
    entry.cursorIndex = -1;
    entry.cursorRaw = -1;
    entry.cursorNode = null;
  }

  /**
   * Keep the child state after a node was appended past every existing child.
   * An append shifts no index, so the memoized children and the cursor stay
   * correct — this is what makes a list build linear.
   *
   * One risk: appending the first element child can make skipImplicit pick a
   * <tbody>, which remaps every index. Drop the child state in that case.
   *
   * @param {Object|null} entry
   */
  function noteChildAppended(entry) {
    if (!entry) return;
    if (!entry.attached) {
      dropChildState(pathCache);
      return;
    }
    if (entry.box === null) return; // nothing memoized yet
    if (entry.box !== entry.node || skipImplicit(entry.node) !== entry.node) {
      dropChildState(entry);
    }
  }

  /**
   * Dirty the entry that owns `path`. Removing or replacing a node changes its
   * parent's child list, not its own.
   *
   * @param {Element} root
   * @param {string} path
   */
  function dropParentChildState(root, path) {
    if (pathCache === null) return;
    var text = String(path == null ? "" : path);
    var cut = text.lastIndexOf("/");
    if (cut < 0) {
      dropChildState(pathCache);
      return;
    }
    dropChildState(resolveEntry(root, text.slice(0, cut)));
  }

  /** Return skipImplicit(entry.node), computed once per child-list version. */
  function entryBox(entry) {
    if (entry.box === null) entry.box = skipImplicit(entry.node);
    return entry.box;
  }

  /**
   * Return the effective child of `entry` at `index`, or null. Effective
   * children are elements and text nodes, matching the Go VDOM's child list.
   * The scan resumes from the cursor when index sits at or after it, so a
   * left-to-right build visits each child once.
   *
   * @param {Object} entry
   * @param {number} index
   * @returns {Node|null}
   */
  function effectiveChildAt(entry, index) {
    if (!(index >= 0)) return null;
    var box = entryBox(entry);
    var children = box.childNodes;
    if (!children) return null;
    var total = children.length;
    var elementType = Node.ELEMENT_NODE;
    var textType = Node.TEXT_NODE;

    var raw = 0;
    var count = 0;
    if (entry.cursorIndex >= 0 && entry.cursorIndex <= index) {
      if (entry.cursorIndex === index) return entry.cursorNode;
      raw = entry.cursorRaw + 1;
      count = entry.cursorIndex + 1;
    }

    for (; raw < total; raw++) {
      var child = children[raw];
      var kind = child.nodeType;
      if (kind !== elementType && kind !== textType) continue;
      if (count === index) {
        entry.cursorIndex = index;
        entry.cursorRaw = raw;
        entry.cursorNode = child;
        return child;
      }
      count++;
    }
    return null;
  }

  /** Return the memoized cache entry for the child at `index`, or null. */
  function childEntry(entry, index) {
    var kids = entry.kids;
    if (kids !== null) {
      var cached = kids.get(index);
      if (cached !== undefined) return cached;
    }
    var node = effectiveChildAt(entry, index);
    if (node === null) return null;
    var created = newCacheEntry(node, true);
    if (kids === null) {
      kids = new Map();
      entry.kids = kids;
    }
    kids.set(index, created);
    return created;
  }

  /**
   * Walk `path` through the cache, memoizing every prefix. Returns the entry,
   * null when the path does not resolve, or MALFORMED when the path is not a
   * plain slash-separated digit list.
   *
   * @param {string} path
   * @returns {Object|null}
   */
  function walkCachedPath(path) {
    var entry = pathCache;
    var len = path.length;
    var i = 0;
    for (;;) {
      var value = 0;
      var digits = 0;
      while (i < len) {
        var code = path.charCodeAt(i);
        if (code === 47) break; // '/'
        if (code < 48 || code > 57) return MALFORMED;
        value = value * 10 + (code - 48);
        digits++;
        i++;
      }
      if (digits === 0) return MALFORMED;
      entry = childEntry(entry, value);
      if (entry === null) return null;
      if (i >= len) return entry;
      i++; // step over the separator
    }
  }

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
    var entry = resolveEntry(root, path);
    return entry === null ? null : entry.node;
  }

  /**
   * Resolve a path to a cache entry, which mutating callers need so they can
   * dirty it. Rejected paths and resolves made between batches get a detached
   * entry: correct to read, safe to dirty.
   *
   * @param {Element} root
   * @param {string} path
   * @returns {Object|null}
   */
  function resolveEntry(root, path) {
    if (!path || path === "") {
      return pathCache !== null ? pathCache : newCacheEntry(root, false);
    }
    if (pathCache !== null) {
      var entry = walkCachedPath(path);
      if (entry !== MALFORMED) return entry;
    }
    var node = walkPathUncached(root, path);
    return node === null ? null : newCacheEntry(node, false);
  }

  /**
   * Walk a path with no cache. The original resolver, kept for malformed paths
   * and for resolves made outside a batch.
   *
   * @param {Element} root
   * @param {string} path
   * @returns {Node|null}
   */
  function walkPathUncached(root, path) {
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
   * Resolve the parent node and final child index for a slash-separated path.
   *
   * @param {Element} root
   * @param {string} path
   * @returns {{parent: Node, index: number}|null}
   */
  function resolveParentPath(root, path) {
    if (!path || path === "") return null;

    var parts = String(path).split("/");
    var rawIndex = parts.pop();
    var idx = parseInt(rawIndex, 10);
    if (isNaN(idx) || idx < 0) return null;

    var parentPath = parts.join("/");
    var parent = resolvePath(root, parentPath);
    if (!parent || parent.nodeType !== Node.ELEMENT_NODE) return null;

    return { parent: parent, index: idx };
  }

  /**
   * Browsers may omit empty text nodes that exist in the Go VDOM. If a SetText
   * targets one of those paths, recreate the missing text node so high-frequency
   * signal patches stay deterministic instead of becoming noisy no-ops.
   *
   * @param {Element} root
   * @param {string} path
   * @returns {Node|null}
   */
  function createMissingTextTarget(root, path) {
    var resolved = resolveParentPath(root, path);
    if (!resolved) return null;

    var children = getEffectiveChildren(resolved.parent);
    if (resolved.index > children.length) return null;

    var doc = resolved.parent.ownerDocument ||
      (typeof document !== "undefined" ? document : null);
    if (!doc || typeof doc.createTextNode !== "function") return null;

    var textNode = doc.createTextNode("");
    if (resolved.index >= children.length) {
      resolved.parent.appendChild(textNode);
    } else {
      resolved.parent.insertBefore(textNode, children[resolved.index]);
    }
    // The insert changed the parent's child list. Dirty it so later ops see
    // the new node instead of a cached pre-insert sibling.
    dropParentChildState(root, path);
    return textNode;
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
    var entry = resolveEntry(root, op.path);
    var target = entry === null ? null : entry.node;

    if (!target && op.kind === PATCH_SET_TEXT) {
      target = createMissingTextTarget(root, op.path);
      // The fresh text node owns no children, so there is nothing to dirty.
      entry = null;
    }

    if (!target) {
      warnUnresolvedPath(op);
      return;
    }

    switch (op.kind) {
      case PATCH_SET_TEXT:
        applySetText(target, op);
        // textContent replaces every child of an element target.
        dropChildState(entry);
        break;

      case PATCH_SET_ATTR:
        applySetAttr(target, op);
        break;

      case PATCH_REMOVE_ATTR:
        applyRemoveAttr(target, op);
        break;

      case PATCH_CREATE_ELEMENT:
        // An insert before the end shifts every later sibling; an append
        // shifts nothing, so the memoized children survive it.
        if (applyCreateElement(target, op)) noteChildAppended(entry);
        else dropChildState(entry);
        break;

      case PATCH_CREATE_TEXT:
        if (applyCreateText(target, op)) noteChildAppended(entry);
        else dropChildState(entry);
        break;

      case PATCH_REMOVE_ELEMENT:
        applyRemoveElement(target);
        // The target leaves its parent's child list.
        dropParentChildState(root, op.path);
        break;

      case PATCH_REPLACE_ELEMENT:
        applyReplaceElement(target, op);
        // A different node now sits at this index of the parent.
        dropParentChildState(root, op.path);
        break;

      case PATCH_REORDER:
        applyReorder(target, op);
        dropChildState(entry);
        break;

      case PATCH_SET_VALUE:
        applySetValue(target, op);
        break;

      case PATCH_SET_HTML:
        applySetHTML(target, op);
        dropChildState(entry);
        break;

      default:
        if (typeof console !== "undefined") {
          console.warn("[gosx/patch] unknown patch kind: " + op.kind);
        }
    }
  }

  /** Warn once per path/kind pair that failed to resolve. */
  function warnUnresolvedPath(op) {
    var warnKey = String(op.path || "") + ":" + String(op.kind);
    if (
      typeof console !== "undefined" &&
      !missingPatchPathWarnings.has(warnKey) &&
      missingPatchPathWarnings.size < MAX_MISSING_PATCH_WARNINGS
    ) {
      missingPatchPathWarnings.add(warnKey);
      console.warn(
        "[gosx/patch] could not resolve path: " + op.path +
        " (kind=" + op.kind + ")"
      );
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
   *
   * Returns true when the node landed after every existing child.
   */
  function applyCreateElement(target, op) {
    var el = document.createElement(op.tag);

    if (op.text) {
      el.textContent = op.text;
    }

    return insertChildAt(target, el, op);
  }

  /**
   * Kind 4 — CreateText: create a new text node and insert it as a child of
   * the target (the path points to the *parent*).
   *
   * Returns true when the node landed after every existing child.
   */
  function applyCreateText(target, op) {
    var text = document.createTextNode(op.text == null ? "" : String(op.text));

    return insertChildAt(target, text, op);
  }

  /**
   * Insert `node` into `target` at the offset the op carries. op.children[0]
   * holds the wanted offset; a missing list means append. The child list is
   * read once, because a live NodeList read is not free.
   *
   * @param {Node} target
   * @param {Node} node
   * @param {Object} op
   * @returns {boolean} true when the node landed after every existing child.
   */
  function insertChildAt(target, node, op) {
    var children = target.childNodes;
    var count = children.length;
    var insertIdx =
      op.children && op.children.length > 0 ? op.children[0] : count;

    if (insertIdx < count) {
      target.insertBefore(node, children[insertIdx]);
      return false;
    }
    target.appendChild(node);
    return true;
  }

  /**
   * Kind 5 — RemoveElement: remove the target node from its parent.
   */
  function applyRemoveElement(target) {
    if (target.parentNode) {
      target.parentNode.removeChild(target);
    }
  }

  /**
   * Kind 6 — ReplaceElement: replace the target node with a new element.
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
   * Kind 7 — Reorder: reorder the children of the target element according to
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
   * Kind 8 — SetValue: set an input's value while preserving cursor position.
   * This is separate from SetAttr because it needs special cursor handling to
   * avoid disrupting the user while they type.
   */
  function applySetValue(target, op) {
    setInputValue(target, op.text);
  }

  /**
   * Kind 9 — SetHTML:
   * assign as text content to avoid HTML injection from malformed patch data.
   */
  function applySetHTML(target, op) {
    target.textContent = op.text == null ? "" : String(op.text);
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
