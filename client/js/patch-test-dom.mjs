// A small namespace-aware DOM shim for the patch applier tests. It models the
// parts of the DOM the applier touches: element namespaces, case-preserving
// tag names for foreign content, namespaced attributes, and child lists.
// patch.test.mjs keeps its own older shim, which has no namespaces.

import fs from "node:fs";
import path from "node:path";
import vm from "node:vm";
import { fileURLToPath } from "node:url";

export const HTML_NS = "http://www.w3.org/1999/xhtml";
export const SVG_NS = "http://www.w3.org/2000/svg";
export const MATHML_NS = "http://www.w3.org/1998/Math/MathML";
export const XLINK_NS = "http://www.w3.org/1999/xlink";
export const XML_NS = "http://www.w3.org/XML/1998/namespace";

const ELEMENT_NODE = 1;
const TEXT_NODE = 3;
const FRAGMENT_NODE = 11;

const here = path.dirname(fileURLToPath(import.meta.url));
export const patchBundleSource = fs.readFileSync(path.join(here, "patch.js"), "utf8");
export const patchAuthoritySource = [
  fs.readFileSync(path.join(here, "../runtime/host/compatibility.ts"), "utf8"),
  fs.readFileSync(path.join(here, "../runtime/host/patch.ts"), "utf8"),
].join("\n");

function nextSiblingOf(node) {
  const siblings = node.parentNode ? node.parentNode.childNodes : [];
  const at = siblings.indexOf(node);
  return at >= 0 && at + 1 < siblings.length ? siblings[at + 1] : null;
}

function previousSiblingOf(node) {
  const siblings = node.parentNode ? node.parentNode.childNodes : [];
  const at = siblings.indexOf(node);
  return at > 0 ? siblings[at - 1] : null;
}

export class FakeText {
  get nextSibling() {
    return nextSiblingOf(this);
  }
  get previousSibling() {
    return previousSiblingOf(this);
  }
  constructor(data) {
    this.nodeType = TEXT_NODE;
    this.data = String(data);
    this.parentNode = null;
    this.childNodes = [];
  }
  get textContent() {
    return this.data;
  }
  set textContent(value) {
    this.data = value == null ? "" : String(value);
  }
}

export class FakeElement {
  // Like the DOM: createElement lowercases and puts the element in the HTML
  // namespace; createElementNS keeps the name as given.
  constructor(namespaceURI, qualifiedName, doc) {
    this.nodeType = ELEMENT_NODE;
    this.namespaceURI = namespaceURI;
    this.localName = namespaceURI === HTML_NS ? String(qualifiedName).toLowerCase() : String(qualifiedName);
    this.tagName = namespaceURI === HTML_NS ? this.localName.toUpperCase() : this.localName;
    this.childNodes = [];
    this.attrNodes = new Map(); // qualified name -> {value, namespaceURI}
    this.parentNode = null;
    this.ownerDocument = doc;
    this.id = "";
  }
  get children() {
    return this.childNodes.filter((kid) => kid.nodeType === ELEMENT_NODE);
  }
  get nextSibling() {
    return nextSiblingOf(this);
  }
  get previousSibling() {
    return previousSiblingOf(this);
  }
  get lastChild() {
    return this.childNodes[this.childNodes.length - 1] || null;
  }
  get firstChild() {
    return this.childNodes[0] || null;
  }
  get firstElementChild() {
    return this.children[0] || null;
  }
  appendChild(node) {
    if (node.nodeType === FRAGMENT_NODE) {
      for (const kid of node.childNodes.slice()) this.appendChild(kid);
      node.childNodes.length = 0;
      return node;
    }
    detach(node);
    node.parentNode = this;
    this.childNodes.push(node);
    return node;
  }
  insertBefore(node, before) {
    if (before == null) return this.appendChild(node);
    detach(node);
    const at = this.childNodes.indexOf(before);
    if (at < 0) return this.appendChild(node);
    node.parentNode = this;
    this.childNodes.splice(at, 0, node);
    return node;
  }
  removeChild(node) {
    const at = this.childNodes.indexOf(node);
    if (at >= 0) {
      this.childNodes.splice(at, 1);
      node.parentNode = null;
    }
    return node;
  }
  replaceChild(next, current) {
    const at = this.childNodes.indexOf(current);
    if (at < 0) return current;
    detach(next);
    next.parentNode = this;
    this.childNodes[this.childNodes.indexOf(current)] = next;
    current.parentNode = null;
    return current;
  }
  setAttribute(name, value) {
    // HTML elements lowercase attribute names; foreign elements keep case.
    const key = this.namespaceURI === HTML_NS ? String(name).toLowerCase() : String(name);
    this.attrNodes.set(key, { value: String(value), namespaceURI: null });
    if (key === "id") this.id = String(value);
  }
  setAttributeNS(namespaceURI, qualifiedName, value) {
    this.attrNodes.set(String(qualifiedName), { value: String(value), namespaceURI });
  }
  getAttribute(name) {
    const key = this.namespaceURI === HTML_NS ? String(name).toLowerCase() : String(name);
    return this.attrNodes.has(key) ? this.attrNodes.get(key).value : null;
  }
  getAttributeNamespace(name) {
    return this.attrNodes.has(name) ? this.attrNodes.get(name).namespaceURI : undefined;
  }
  hasAttribute(name) {
    return this.getAttribute(name) !== null;
  }
  removeAttribute(name) {
    this.attrNodes.delete(this.namespaceURI === HTML_NS ? String(name).toLowerCase() : String(name));
    if (name === "id") this.id = "";
  }
  contains(node) {
    for (let walk = node; walk; walk = walk.parentNode) if (walk === this) return true;
    return false;
  }
  focus() {}
  get textContent() {
    let out = "";
    for (const kid of this.childNodes) out += kid.textContent;
    return out;
  }
  set textContent(value) {
    for (const kid of this.childNodes) kid.parentNode = null;
    this.childNodes.length = 0;
    const text = value == null ? "" : String(value);
    if (text !== "") this.appendChild(new FakeText(text));
  }
}

class FakeFragment {
  constructor() {
    this.nodeType = FRAGMENT_NODE;
    this.childNodes = [];
  }
  appendChild(node) {
    detach(node);
    node.parentNode = this;
    this.childNodes.push(node);
    return node;
  }
}

function detach(node) {
  if (node.parentNode && typeof node.parentNode.removeChild === "function") {
    node.parentNode.removeChild(node);
  }
  node.parentNode = null;
}

export function makeDocument() {
  const byID = new Map();
  const doc = {
    activeElement: null,
    getElementById: (id) => byID.get(id) || null,
    createElement: (tag) => new FakeElement(HTML_NS, tag, doc),
    createElementNS: (ns, tag) => new FakeElement(ns || HTML_NS, tag, doc),
    createTextNode: (data) => new FakeText(data),
    createDocumentFragment: () => new FakeFragment(),
    register: (element) => byID.set(element.id, element),
  };
  doc.body = new FakeElement(HTML_NS, "body", doc);
  doc.documentElement = new FakeElement(HTML_NS, "html", doc);
  return doc;
}

// el builds a tree: el("svg", {ns: SVG_NS}, el("g", {ns: SVG_NS}), "text").
export function el(doc, tag, options = {}, ...kids) {
  const node = new FakeElement(options.ns || HTML_NS, tag, doc);
  for (const [name, value] of Object.entries(options.attrs || {})) node.setAttribute(name, value);
  for (const kid of kids) node.appendChild(typeof kid === "string" ? new FakeText(kid) : kid);
  return node;
}

// loadApplier runs a patch applier source in a fresh context and returns a
// scene: the island wrapper, the component root, and apply(ops).
export function loadScene(source, populate) {
  const doc = makeDocument();
  const warnings = [];
  const sandbox = {
    document: doc,
    Node: { ELEMENT_NODE, TEXT_NODE, COMMENT_NODE: 8, DOCUMENT_FRAGMENT_NODE: FRAGMENT_NODE },
    Map, Set, Array, Math, JSON, String, Number, parseInt, isNaN,
    console: {
      warn: (...args) => warnings.push(args.join(" ")),
      error: (...args) => warnings.push(args.join(" ")),
    },
  };
  sandbox.window = sandbox;
  new vm.Script(source, { filename: "patch-under-test.js" }).runInContext(vm.createContext(sandbox));
  const wrapper = new FakeElement(HTML_NS, "div", doc);
  wrapper.id = "island";
  doc.register(wrapper);
  const root = new FakeElement(HTML_NS, "div", doc);
  wrapper.appendChild(root);
  populate(root, doc);
  return {
    doc,
    root,
    warnings,
    apply(ops) {
      sandbox.__gosx_apply_patches("island", JSON.stringify(ops));
    },
  };
}

// describe prints a tree as "ns:tag[attr]" lines for readable assertions.
export function describe(node) {
  if (node.nodeType === TEXT_NODE) return JSON.stringify(node.data);
  const kids = node.childNodes.map(describe).join(",");
  return `${node.localName}${kids ? "(" + kids + ")" : ""}`;
}
