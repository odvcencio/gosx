// Tests for client/js/patch.js — the DOM patch applier.
//
// The applier memoizes resolved paths for the life of one batch. A stale
// cache entry patches the wrong element in silence, so these tests compare
// the batched applier against two independent oracles:
//
//   1. The same op list replayed as one-op batches. Each batch starts with an
//      empty cache, so no entry can survive a structural change.
//   2. A reference applier written here that resolves every path with a plain
//      uncached walk, the way patch.js did before the cache landed.
//
// The file also measures how many child nodes the applier visits, which is the
// cost the cache removes.

import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import test from "node:test";
import vm from "node:vm";
import { fileURLToPath } from "node:url";

const here = path.dirname(fileURLToPath(import.meta.url));
const patchSource = fs.readFileSync(path.join(here, "patch.js"), "utf8");
const patchAuthoritySource = [
  fs.readFileSync(path.join(here, "../runtime/host/compatibility.ts"), "utf8"),
  fs.readFileSync(path.join(here, "../runtime/host/patch.ts"), "utf8"),
].join("\n");

const ELEMENT_NODE = 1;
const TEXT_NODE = 3;
const COMMENT_NODE = 8;
const FRAGMENT_NODE = 11;

// ---------------------------------------------------------------------------
// Instrumented DOM shim
// ---------------------------------------------------------------------------

// Stats counts the work the applier does against the child lists. A real
// browser hands out a live NodeList, so a full copy per step is a real cost;
// the shim charges one visit per indexed read instead.
function newStats() {
  return { childNodesReads: 0, childVisits: 0, childrenReads: 0 };
}

class FakeText {
  constructor(data, stats) {
    this.nodeType = TEXT_NODE;
    this.data = String(data);
    this.parentNode = null;
    this.stats = stats;
    this.ownerDocument = null;
  }

  get textContent() {
    return this.data;
  }

  set textContent(value) {
    this.data = value == null ? "" : String(value);
  }

  get childNodes() {
    return [];
  }
}

class FakeComment {
  constructor(data, stats) {
    this.nodeType = COMMENT_NODE;
    this.data = String(data);
    this.parentNode = null;
    this.stats = stats;
  }

  get childNodes() {
    return [];
  }
}

// countingList returns a proxy that charges one visit per indexed read. The
// length read is free; only walking children costs.
function countingList(items, stats) {
  return new Proxy(items, {
    get(target, key) {
      if (typeof key === "string" && /^\d+$/.test(key)) {
        stats.childVisits += 1;
      }
      return target[key];
    },
  });
}

class FakeElement {
  constructor(tagName, stats, doc) {
    this.nodeType = ELEMENT_NODE;
    this.tagName = String(tagName).toUpperCase();
    this.kids = [];
    this.attrs = new Map();
    this.parentNode = null;
    this.stats = stats;
    this.ownerDocument = doc;
    this.id = "";
    this.value = undefined;
  }

  get childNodes() {
    this.stats.childNodesReads += 1;
    return countingList(this.kids, this.stats);
  }

  get children() {
    this.stats.childrenReads += 1;
    return this.kids.filter((kid) => kid.nodeType === ELEMENT_NODE);
  }

  get firstChild() {
    return this.kids.length > 0 ? this.kids[0] : null;
  }

  get firstElementChild() {
    for (const kid of this.kids) {
      if (kid.nodeType === ELEMENT_NODE) return kid;
    }
    return null;
  }

  appendChild(node) {
    if (node.nodeType === FRAGMENT_NODE) {
      for (const kid of node.kids.slice()) this.appendChild(kid);
      node.kids.length = 0;
      return node;
    }
    detach(node);
    node.parentNode = this;
    this.kids.push(node);
    return node;
  }

  insertBefore(node, before) {
    if (before == null) return this.appendChild(node);
    const at = this.kids.indexOf(before);
    if (at < 0) return this.appendChild(node);
    detach(node);
    node.parentNode = this;
    this.kids.splice(at, 0, node);
    return node;
  }

  removeChild(node) {
    const at = this.kids.indexOf(node);
    if (at >= 0) {
      this.kids.splice(at, 1);
      node.parentNode = null;
    }
    return node;
  }

  replaceChild(next, current) {
    const at = this.kids.indexOf(current);
    if (at < 0) return current;
    detach(next);
    next.parentNode = this;
    this.kids[at] = next;
    current.parentNode = null;
    return current;
  }

  setAttribute(name, value) {
    this.attrs.set(name, String(value));
    if (name === "id") this.id = String(value);
  }

  getAttribute(name) {
    return this.attrs.has(name) ? this.attrs.get(name) : null;
  }

  removeAttribute(name) {
    this.attrs.delete(name);
    if (name === "id") this.id = "";
  }

  contains(node) {
    let walk = node;
    while (walk) {
      if (walk === this) return true;
      walk = walk.parentNode;
    }
    return false;
  }

  focus() {}

  get textContent() {
    let out = "";
    for (const kid of this.kids) {
      if (kid.nodeType === TEXT_NODE) out += kid.data;
      else if (kid.nodeType === ELEMENT_NODE) out += kid.textContent;
    }
    return out;
  }

  set textContent(value) {
    for (const kid of this.kids) kid.parentNode = null;
    this.kids.length = 0;
    const text = value == null ? "" : String(value);
    if (text !== "") {
      this.appendChild(new FakeText(text, this.stats));
    }
  }
}

class FakeFragment {
  constructor(stats) {
    this.nodeType = FRAGMENT_NODE;
    this.kids = [];
    this.stats = stats;
  }

  appendChild(node) {
    detach(node);
    node.parentNode = this;
    this.kids.push(node);
    return node;
  }
}

function detach(node) {
  if (node.parentNode && typeof node.parentNode.removeChild === "function") {
    node.parentNode.removeChild(node);
  }
  node.parentNode = null;
}

function makeDocument(stats) {
  const byID = new Map();
  const doc = {
    activeElement: null,
    getElementById(id) {
      return byID.get(id) || null;
    },
    createElement(tag) {
      return new FakeElement(tag, stats, doc);
    },
    createTextNode(data) {
      const node = new FakeText(data, stats);
      node.ownerDocument = doc;
      return node;
    },
    createDocumentFragment() {
      return new FakeFragment(stats);
    },
    register(element) {
      byID.set(element.id, element);
    },
  };
  doc.body = new FakeElement("body", stats, doc);
  doc.documentElement = new FakeElement("html", stats, doc);
  return doc;
}

// loadApplier runs patch.js inside a fresh context and returns the applier plus
// its document, stats and warning log.
function loadApplier(source = patchSource) {
  const stats = newStats();
  const doc = makeDocument(stats);
  const warnings = [];
  const sandbox = {
    document: doc,
    Node: {
      ELEMENT_NODE,
      TEXT_NODE,
      COMMENT_NODE,
      DOCUMENT_FRAGMENT_NODE: FRAGMENT_NODE,
    },
    Map,
    Set,
    Array,
    Math,
    JSON,
    String,
    Number,
    parseInt,
    isNaN,
    console: {
      warn: (...args) => warnings.push(args.join(" ")),
      error: (...args) => warnings.push(args.join(" ")),
    },
  };
  sandbox.window = sandbox;
  const context = vm.createContext(sandbox);
  new vm.Script(source, { filename: "patch-authority.js" }).runInContext(context);
  return { apply: sandbox.__gosx_apply_patches, doc, stats, warnings, sandbox };
}

// ---------------------------------------------------------------------------
// Reference applier — plain uncached walk, no memoization
// ---------------------------------------------------------------------------

const IMPLICIT = new Set(["TBODY", "THEAD", "TFOOT", "COLGROUP"]);
const PROP_ATTRS = new Set(["value", "checked", "selected", "disabled"]);
const BOOL_PROPS = new Set(["checked", "selected", "disabled"]);

function refSkipImplicit(node) {
  if (node.nodeType !== ELEMENT_NODE) return node;
  const elements = node.kids.filter((kid) => kid.nodeType === ELEMENT_NODE);
  if (elements.length === 1 && IMPLICIT.has(elements[0].tagName)) {
    return elements[0];
  }
  return node;
}

function refEffectiveChildren(node) {
  const kids = node.kids || [];
  return kids.filter(
    (kid) => kid.nodeType === ELEMENT_NODE || kid.nodeType === TEXT_NODE,
  );
}

function refResolve(root, pathText) {
  if (!pathText || pathText === "") return root;
  let node = root;
  for (const part of String(pathText).split("/")) {
    const idx = parseInt(part, 10);
    if (Number.isNaN(idx)) return null;
    node = refSkipImplicit(node);
    const kids = refEffectiveChildren(node);
    if (idx < 0 || idx >= kids.length) return null;
    node = kids[idx];
  }
  return node;
}

function refApplyOne(doc, root, op) {
  let target = refResolve(root, op.path);
  if (!target && op.kind === 0) {
    const parts = String(op.path || "").split("/");
    const rawIndex = parts.pop();
    const idx = parseInt(rawIndex, 10);
    if (Number.isNaN(idx) || idx < 0) return;
    const parent = refResolve(root, parts.join("/"));
    if (!parent || parent.nodeType !== ELEMENT_NODE) return;
    const kids = refEffectiveChildren(parent);
    if (idx > kids.length) return;
    const created = doc.createTextNode("");
    if (idx >= kids.length) parent.appendChild(created);
    else parent.insertBefore(created, kids[idx]);
    target = created;
  }
  if (!target) return;

  const insertAt = () =>
    op.children && op.children.length > 0 ? op.children[0] : target.kids.length;

  switch (op.kind) {
    case 0:
      target.textContent = op.text;
      break;
    case 1:
      refSetAttr(target, op.attrName, op.text);
      break;
    case 2:
      target.removeAttribute(op.attrName);
      if (BOOL_PROPS.has(op.attrName)) target[op.attrName] = false;
      else if (op.attrName === "value") target.value = "";
      break;
    case 3: {
      const created = doc.createElement(op.tag);
      if (op.text) created.textContent = op.text;
      const at = insertAt();
      if (at < target.kids.length) target.insertBefore(created, target.kids[at]);
      else target.appendChild(created);
      break;
    }
    case 4: {
      const created = doc.createTextNode(op.text == null ? "" : String(op.text));
      const at = insertAt();
      if (at < target.kids.length) target.insertBefore(created, target.kids[at]);
      else target.appendChild(created);
      break;
    }
    case 5:
      if (target.parentNode) target.parentNode.removeChild(target);
      break;
    case 6: {
      const created = doc.createElement(op.tag);
      if (op.text) created.textContent = op.text;
      if (target.parentNode) target.parentNode.replaceChild(created, target);
      break;
    }
    case 7: {
      if (!op.children || op.children.length === 0) break;
      const snapshot = target.kids.slice();
      const ordered = [];
      for (const idx of op.children) {
        if (idx >= 0 && idx < snapshot.length) ordered.push(snapshot[idx]);
      }
      for (const kid of target.kids) kid.parentNode = null;
      target.kids.length = 0;
      for (const kid of ordered) target.appendChild(kid);
      break;
    }
    case 8:
      if (target.value !== op.text) target.value = op.text;
      break;
    case 9:
      target.textContent = op.text == null ? "" : String(op.text);
      break;
    default:
      break;
  }
}

function refSetAttr(el, name, value) {
  if (PROP_ATTRS.has(name)) {
    if (BOOL_PROPS.has(name)) el[name] = value !== "false" && value !== "";
    else el[name] = value;
  } else {
    el.setAttribute(name, value);
  }
}

// ---------------------------------------------------------------------------
// Scenario helpers
// ---------------------------------------------------------------------------

// buildScene creates the island wrapper plus a component root, then lets the
// caller populate the root. Every applier instance gets an identical tree.
function buildScene(populate, source = patchSource) {
  const loaded = loadApplier(source);
  const wrapper = new FakeElement("div", loaded.stats, loaded.doc);
  wrapper.id = "island";
  loaded.doc.register(wrapper);
  const root = new FakeElement("div", loaded.stats, loaded.doc);
  wrapper.appendChild(root);
  populate(root, loaded.doc, loaded.stats);
  return { ...loaded, wrapper, root };
}

function runAuthority(populate, ops) {
  const scene = buildScene(populate, patchAuthoritySource);
  scene.apply("island", JSON.stringify(ops));
  return scene;
}

function serialize(node) {
  if (node.nodeType === TEXT_NODE) return JSON.stringify(node.data);
  if (node.nodeType === COMMENT_NODE) return "<!--" + node.data + "-->";
  const attrs = [...node.attrs.entries()]
    .sort((a, b) => (a[0] < b[0] ? -1 : 1))
    .map(([name, value]) => ` ${name}="${value}"`)
    .join("");
  const value = node.value === undefined ? "" : ` @value=${JSON.stringify(node.value)}`;
  const kids = node.kids.map(serialize).join("");
  return `<${node.tagName.toLowerCase()}${attrs}${value}>${kids}</${node.tagName.toLowerCase()}>`;
}

// runBatched applies every op in a single batch, so the cache spans all ops.
function runBatched(populate, ops) {
  const scene = buildScene(populate);
  scene.apply("island", JSON.stringify(ops));
  return scene;
}

// runPerOp applies each op in its own batch. Each batch starts with an empty
// cache, so this is the uncached behaviour expressed through the same code.
function runPerOp(populate, ops) {
  const scene = buildScene(populate);
  for (const op of ops) {
    scene.apply("island", JSON.stringify([op]));
  }
  return scene;
}

// runReference applies every op through the uncached reference walker.
function runReference(populate, ops) {
  const scene = buildScene(populate);
  for (const op of ops) {
    refApplyOne(scene.doc, scene.root, op);
  }
  return scene;
}

// assertMatchesOracles proves the batched applier lands on the same DOM as both
// oracles for the given scenario.
function assertMatchesOracles(label, populate, ops) {
  const batched = runBatched(populate, ops);
  const perOp = runPerOp(populate, ops);
  const reference = runReference(populate, ops);
  assert.equal(
    serialize(batched.root),
    serialize(perOp.root),
    `${label}: batched cache diverged from one-op-per-batch`,
  );
  assert.equal(
    serialize(batched.root),
    serialize(reference.root),
    `${label}: batched cache diverged from the uncached reference walk`,
  );
  return batched;
}

// ---------------------------------------------------------------------------
// Scenarios
// ---------------------------------------------------------------------------

function emptyList(root, doc, stats) {
  const ul = new FakeElement("ul", stats, doc);
  root.appendChild(ul);
}

// listBuildOps mirrors what reconcile.go emits for a fresh list of `rows` rows,
// each row holding `cells` cells with one text child.
function listBuildOps(rows, cells) {
  const ops = [];
  for (let r = 0; r < rows; r++) {
    ops.push({ kind: 3, path: "0", tag: "li", children: [r] });
    ops.push({ kind: 1, path: `0/${r}`, attrName: "data-row", text: String(r) });
    for (let c = 0; c < cells; c++) {
      ops.push({ kind: 3, path: `0/${r}`, tag: "span", children: [c] });
      ops.push({ kind: 4, path: `0/${r}/${c}`, text: `r${r}c${c}`, children: [0] });
    }
  }
  return ops;
}

function seededList(rows) {
  return (root, doc, stats) => {
    const ul = new FakeElement("ul", stats, doc);
    root.appendChild(ul);
    for (let r = 0; r < rows; r++) {
      const li = new FakeElement("li", stats, doc);
      li.setAttribute("data-row", String(r));
      li.appendChild(new FakeText(`row ${r}`, stats));
      ul.appendChild(li);
    }
  };
}

function seededListWithComments(rows) {
  return (root, doc, stats) => {
    const ul = new FakeElement("ul", stats, doc);
    root.appendChild(ul);
    ul.appendChild(new FakeComment("lead", stats));
    for (let r = 0; r < rows; r++) {
      const li = new FakeElement("li", stats, doc);
      li.appendChild(new FakeText(`row ${r}`, stats));
      ul.appendChild(li);
      ul.appendChild(new FakeComment(`after ${r}`, stats));
    }
  };
}

function seededTable(rows) {
  return (root, doc, stats) => {
    const table = new FakeElement("table", stats, doc);
    const tbody = new FakeElement("tbody", stats, doc);
    table.appendChild(tbody);
    root.appendChild(table);
    for (let r = 0; r < rows; r++) {
      const tr = new FakeElement("tr", stats, doc);
      const td = new FakeElement("td", stats, doc);
      td.appendChild(new FakeText(`cell ${r}`, stats));
      tr.appendChild(td);
      tbody.appendChild(tr);
    }
  };
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

test("patch applier builds a list identically with and without the batch cache", () => {
  assertMatchesOracles("list build", emptyList, listBuildOps(12, 3));
});

test("patch applier keeps cached paths correct across creates in the middle", () => {
  const ops = [
    { kind: 3, path: "0", tag: "li", children: [0] },
    { kind: 4, path: "0/0", text: "inserted first", children: [0] },
    { kind: 0, path: "0/1", text: "was row 0" },
    { kind: 3, path: "0", tag: "li", children: [1] },
    { kind: 4, path: "0/1", text: "inserted second", children: [0] },
    { kind: 0, path: "0/2", text: "still row 0" },
    { kind: 0, path: "0/3", text: "still row 1" },
  ];
  assertMatchesOracles("mid-list create", seededList(3), ops);
});

test("patch applier keeps cached paths correct across removals", () => {
  const ops = [
    { kind: 0, path: "0/0/0", text: "before removal" },
    { kind: 5, path: "0/1" },
    { kind: 0, path: "0/1/0", text: "row 2 shifted down" },
    { kind: 5, path: "0/0" },
    { kind: 0, path: "0/0/0", text: "row 2 shifted twice" },
  ];
  assertMatchesOracles("removal", seededList(4), ops);
});

test("patch applier keeps cached paths correct across a replace", () => {
  const ops = [
    { kind: 0, path: "0/1/0", text: "touch before replace" },
    { kind: 6, path: "0/1", tag: "section" },
    { kind: 1, path: "0/1", attrName: "data-kind", text: "replaced" },
    { kind: 3, path: "0/1", tag: "b", children: [0] },
    { kind: 4, path: "0/1/0", text: "fresh child", children: [0] },
    { kind: 0, path: "0/2/0", text: "sibling untouched by replace" },
  ];
  assertMatchesOracles("replace", seededList(3), ops);
});

test("patch applier keeps cached paths correct across a reorder", () => {
  const ops = [
    { kind: 0, path: "0/0/0", text: "pre-reorder" },
    { kind: 7, path: "0", children: [2, 0, 1] },
    { kind: 0, path: "0/0/0", text: "now first" },
    { kind: 0, path: "0/1/0", text: "now second" },
    { kind: 0, path: "0/2/0", text: "now third" },
  ];
  assertMatchesOracles("reorder", seededList(3), ops);
});

test("patch applier keeps cached paths correct when set-text drops children", () => {
  const ops = [
    { kind: 3, path: "0/0", tag: "em", children: [1] },
    { kind: 4, path: "0/0/1", text: "extra", children: [0] },
    { kind: 0, path: "0/0", text: "flattened" },
    { kind: 0, path: "0/0/0", text: "the single remaining text node" },
  ];
  assertMatchesOracles("set-text flatten", seededList(2), ops);
});

test("patch applier skips comment nodes the same way with a warm cache", () => {
  const ops = [
    { kind: 0, path: "0/0/0", text: "row 0" },
    { kind: 0, path: "0/1/0", text: "row 1" },
    { kind: 3, path: "0", tag: "li", children: [1] },
    { kind: 0, path: "0/2/0", text: "row 1 after shift" },
    { kind: 5, path: "0/0" },
    { kind: 0, path: "0/1/0", text: "row 1 after removal" },
  ];
  assertMatchesOracles("comments", seededListWithComments(3), ops);
});

test("patch applier still skips implicit tbody with a warm cache", () => {
  const ops = [
    { kind: 0, path: "0/0/0/0", text: "cell 0 patched" },
    { kind: 0, path: "0/1/0/0", text: "cell 1 patched" },
    { kind: 3, path: "0/0", tag: "td", children: [1] },
    { kind: 4, path: "0/0/1", text: "second cell", children: [0] },
    { kind: 0, path: "0/2/0/0", text: "cell 2 patched" },
  ];
  assertMatchesOracles("implicit tbody", seededTable(3), ops);
});

test("patch applier recreates a missing text target and keeps the cache honest", () => {
  const populate = (root, doc, stats) => {
    const box = new FakeElement("div", stats, doc);
    root.appendChild(box);
  };
  const ops = [
    { kind: 0, path: "0/0", text: "created" },
    { kind: 0, path: "0/0", text: "updated" },
    { kind: 3, path: "0", tag: "span", children: [1] },
    { kind: 0, path: "0/0", text: "still the box" },
  ];
  const scene = assertMatchesOracles("missing text target", populate, ops);
  assert.deepEqual(scene.warnings, []);
});

test("patch applier tolerates malformed and out-of-range paths", () => {
  const ops = [
    { kind: 0, path: "0/x", text: "ignored" },
    { kind: 0, path: "0/", text: "ignored" },
    { kind: 0, path: "0/99/0", text: "ignored" },
    { kind: 0, path: "0/0/0", text: "applied" },
  ];
  const batched = runBatched(seededList(2), ops);
  const perOp = runPerOp(seededList(2), ops);
  assert.equal(serialize(batched.root), serialize(perOp.root));
  assert.equal(batched.root.kids[0].kids[0].textContent, "applied");
});

test("patch applier sets attributes, values and removals identically", () => {
  const populate = (root, doc, stats) => {
    const form = new FakeElement("form", stats, doc);
    const input = new FakeElement("input", stats, doc);
    input.value = "old";
    input.setAttribute("placeholder", "gone");
    form.appendChild(input);
    root.appendChild(form);
  };
  const ops = [
    { kind: 8, path: "0/0", attrName: "value", text: "new" },
    { kind: 2, path: "0/0", attrName: "placeholder" },
    { kind: 1, path: "0/0", attrName: "aria-label", text: "field" },
  ];
  assertMatchesOracles("attributes", populate, ops);
});

test("authored patch treats boolean attributes as presence regardless of value text", () => {
  const populate = (root, doc, stats) => {
    const input = new FakeElement("input", stats, doc);
    input.disabled = false;
    input.required = false;
    root.appendChild(input);
  };
  const scene = runAuthority(populate, [
    { kind: 1, path: "0", attrName: "disabled", text: "false" },
    { kind: 1, path: "0", attrName: "required", text: "" },
  ]);
  const input = scene.root.kids[0];
  assert.equal(input.getAttribute("disabled"), "");
  assert.equal(input.getAttribute("required"), "");
  assert.equal(input.disabled, true);
  assert.equal(input.required, true);
});

test("authored patch maps reflected boolean property names and clears them on removal", () => {
  const populate = (root, doc, stats) => {
    const input = new FakeElement("input", stats, doc);
    input.readOnly = false;
    root.appendChild(input);
  };
  const scene = runAuthority(populate, [
    { kind: 1, path: "0", attrName: "readonly", text: "true" },
    { kind: 2, path: "0", attrName: "readonly" },
  ]);
  const input = scene.root.kids[0];
  assert.equal(input.getAttribute("readonly"), null);
  assert.equal(input.readOnly, false);
});

test("authored patch preserves hidden=until-found and writes value as a live property", () => {
  const populate = (root, doc, stats) => {
    const input = new FakeElement("input", stats, doc);
    input.value = "old";
    root.appendChild(input);
  };
  const scene = runAuthority(populate, [
    { kind: 1, path: "0", attrName: "hidden", text: "until-found" },
    { kind: 1, path: "0", attrName: "value", text: "new" },
  ]);
  const input = scene.root.kids[0];
  assert.equal(input.getAttribute("hidden"), "until-found");
  assert.equal(input.getAttribute("value"), null);
  assert.equal(input.value, "new");
});

test("patch applier survives a long randomized op stream", () => {
  // A deterministic pseudo-random stream mixes creates, removals, replaces,
  // reorders and text writes so cache invalidation faces interleaved kinds.
  let seed = 20260726;
  const next = (bound) => {
    seed = (seed * 1103515245 + 12345) & 0x7fffffff;
    return seed % bound;
  };
  const ops = [];
  for (let i = 0; i < 400; i++) {
    const row = next(6);
    switch (next(6)) {
      case 0:
        ops.push({ kind: 3, path: "0", tag: "li", children: [row] });
        break;
      case 1:
        ops.push({ kind: 4, path: `0/${row}`, text: `t${i}`, children: [0] });
        break;
      case 2:
        ops.push({ kind: 5, path: `0/${row}` });
        break;
      case 3:
        ops.push({ kind: 6, path: `0/${row}`, tag: "p", text: `p${i}` });
        break;
      case 4:
        ops.push({ kind: 7, path: "0", children: [2, 0, 1] });
        break;
      default:
        ops.push({ kind: 0, path: `0/${row}`, text: `s${i}` });
        break;
    }
  }
  assertMatchesOracles("randomized stream", seededList(6), ops);
});

test("patch applier visits far fewer child nodes with the batch cache", () => {
  const rows = 100;
  const cells = 3;
  const ops = listBuildOps(rows, cells);

  const batched = runBatched(emptyList, ops);
  const perOp = runPerOp(emptyList, ops);

  assert.equal(serialize(batched.root), serialize(perOp.root));

  const report = (label, stats) =>
    `${label}: ops=${ops.length} childNodes reads=${stats.childNodesReads} ` +
    `child visits=${stats.childVisits}`;
  // Printed so a regression shows the numbers, not just a failed threshold.
  console.log(report("cached  ", batched.stats));
  console.log(report("uncached", perOp.stats));

  // The uncached walk is quadratic in row count; the cached walk is linear.
  // 400 created nodes need about one visit each once prefixes memoize.
  const created = rows * (1 + cells * 2);
  assert.ok(
    batched.stats.childVisits < created * 2,
    `cached child visits should stay near linear, got ${batched.stats.childVisits} ` +
      `for ${created} created nodes`,
  );
  assert.ok(
    batched.stats.childVisits * 10 < perOp.stats.childVisits,
    `cache should cut child visits by more than 10x, got ` +
      `${batched.stats.childVisits} vs ${perOp.stats.childVisits}`,
  );
});
