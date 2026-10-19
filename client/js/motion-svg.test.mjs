import assert from "node:assert/strict";
import fs from "node:fs";
import test from "node:test";
import vm from "node:vm";

const core = fs.readFileSync(new URL("./bootstrap-src/06-motion-core.ts", import.meta.url), "utf8");
const svgSource = fs.readFileSync(new URL("../runtime/motion/easing.ts", import.meta.url), "utf8") + fs.readFileSync(new URL("../runtime/motion/svg.ts", import.meta.url), "utf8");
const transport = fs.readFileSync(new URL("../runtime/motion/transport.ts", import.meta.url), "utf8");

class Element {
  constructor(tag, attrs = {}, children = []) {
    this.tagName = tag; this.attrs = {...attrs}; this.children = children; this.nodeType = 1;
    this.namespaceURI = "http://www.w3.org/2000/svg";
    children.forEach(child => child.parent = this);
    const styles = new Map();
    this.style = {getPropertyValue: key => styles.get(key)?.[0] || "", getPropertyPriority: key => styles.get(key)?.[1] || "",
      setProperty: (key, value, priority = "") => styles.set(key, [value, priority]), removeProperty: key => styles.delete(key)};
  }
  getAttribute(key) { return this.attrs[key] ?? null; }
  hasAttribute(key) { return key in this.attrs; }
  setAttribute(key, value) { this.attrs[key] = String(value); }
  closest(selector) { return selector === "[data-morph-id]" && this.hasAttribute("data-morph-id") ? this : this.parent?.closest(selector); }
  querySelectorAll(selector) {
    const all = this.children.flatMap(child => [child, ...child.querySelectorAll("*")]);
    return all.filter(el => selector === "*" || selector.startsWith("#") && el.attrs.id === selector.slice(1) || selector.startsWith("[") && el.hasAttribute(selector.slice(1,-1)) || selector.split(",").includes(el.tagName));
  }
  querySelector(selector) { return this.querySelectorAll(selector)[0] || null; }
}
const shape = (tag, attrs) => new Element(tag, attrs);
const snapshot = (id, children) => new Element("svg", {id}, children);

function runtime(options = {}) {
  const body = new Element("body"), frames = new Map(), observers = [];
  let id = 0, clock = 0, reduced;
  const document = {body, documentElement: body, addEventListener() {}, removeEventListener() {}};
  const window = {__gosx: {}, console, addEventListener() {}, matchMedia: () => ({matches: false, addEventListener(_name, fn) {reduced = fn;}}),
    requestAnimationFrame(fn) {frames.set(++id, fn); return id;}, cancelAnimationFrame: id => frames.delete(id)};
  class Observer {constructor(fn) {observers.push(fn);} observe() {} disconnect() {}}
  const context = vm.createContext({window, document, console, MutationObserver: Observer, performance: {now: () => clock}, CSS: {supports: () => options.css === true}, setTimeout, clearTimeout});
  vm.runInContext(core, context);
  return {motion: window.__gosx.motion, body, frames, observers,
    load(source = svgSource) {vm.runInContext(source, context);},
    reduce(value) {reduced({matches: value});},
    advance(ms) {clock = ms; const queued = [...frames.values()]; frames.clear(); queued.forEach(fn => fn(ms));}};
}

test("SVG morph samples exact endpoints, seeks backward and restores authored opacity on disposal", () => {
  const r = runtime(); r.load();
  const fromShape = shape("rect", {"data-morph-id": "actor", x: "1e1", width: "20", transform: "translate(0 0)"});
  const toShape = shape("rect", {"data-morph-id": "actor", x: "30", width: "60", transform: "translate(20 10)"});
  const newcomer = shape("circle", {"data-morph-id": "new", r: "8"});
  newcomer.style.setProperty("opacity", "0.4", "important");
  const from = snapshot("before", [fromShape]), to = snapshot("after", [toShape, newcomer]);
  const transition = r.motion.createSVGMorph(from, to);
  transition.sample(.5); assert.equal(toShape.getAttribute("x"), "20"); assert.equal(toShape.getAttribute("width"), "40");
  assert.equal(toShape.getAttribute("transform"), "translate(10 5)"); assert.equal(newcomer.style.getPropertyValue("opacity"), "0.2");
  transition.sample(1); assert.equal(toShape.getAttribute("width"), "60"); assert.equal(newcomer.style.getPropertyValue("opacity"), "0.4");
  transition.sample(0); assert.equal(toShape.getAttribute("x"), "1e1");
  transition.sample(.5); assert.equal(toShape.getAttribute("width"), "40");
  assert.equal(fromShape.getAttribute("width"), "20", "source snapshots are immutable");
  transition.dispose(); assert.equal(toShape.getAttribute("x"), "30"); assert.equal(newcomer.style.getPropertyPriority("opacity"), "important");
  transition.sample(0); assert.equal(toShape.getAttribute("x"), "30"); assert.equal(r.frames.size, 0, "SVG samples never own a RAF");
});

test("incompatible topology, arc flags and missing geometry retain valid destination paths and fade", () => {
  const r = runtime(); r.load();
  for (const [a, b] of [["M0 0L10 10", "M0 0C1 2 3 4 5 6"], ["M0 0A10 10 0 0 0 20 20", "M0 0A20 20 0 1 1 40 40"], [null, "M0 0L10 10"], ["M0 0L1e999 0", "M0 0L10 0"]]) {
    const source = shape("path", a ? {d: a} : {}), target = shape("path", {d: b});
    const transition = r.motion.createSVGMorph(snapshot("a", [source]), snapshot("b", [target]));
    transition.sample(.5); assert.equal(target.getAttribute("d"), b); assert.equal(target.parent.style.getPropertyValue("opacity"), "0.5");
    transition.sample(1); assert.equal(target.parent.style.getPropertyValue("opacity"), ""); transition.dispose();
  }
  assert.throws(() => r.motion.createSVGMorph(null, null), /distinct SVG/);
});

test("reduced motion settles SVG bindings and restores them without scheduling animation", () => {
  const r = runtime(); r.load();
  const from = snapshot("a", [shape("circle", {r: "2"})]), to = snapshot("b", [shape("circle", {r: "10"})]);
  const transition = r.motion.createSVGMorph(from, to);
  transition.sample(.25); assert.equal(to.children[0].getAttribute("r"), "4");
  r.reduce(true); assert.equal(to.children[0].getAttribute("r"), "10"); transition.sample(0); assert.equal(to.children[0].getAttribute("r"), "10");
  r.reduce(false); assert.equal(to.children[0].getAttribute("r"), "2");
  assert.throws(() => transition.sample(NaN), /finite/); transition.dispose();
});

test("Go SVG bindings mount before or after optional features and dispose with the shared program", () => {
  for (const late of [false, true]) {
    const r = runtime({css: true});
    const from = snapshot("a", [shape("rect", {width: "20"})]), to = snapshot("b", [shape("rect", {width: "100"})]);
    const spec = {version: 1, id: "story", transport: {durationMs: 1000}, signals: [{id: "clock", kind: "playhead"}], cssCompiled: [0], bindings: [{signal: "clock", target: "style", selector: "#b", property: "opacity"}], svgMorphs: [{signal: "clock", from: "#a", to: "#b"}]};
    const root = new Element("div", {"data-gosx-motion-program": JSON.stringify(spec)}, [from, to]);
    if (!late) r.load();
    r.body.children.push(root); r.motion.mountPrograms(root);
    if (late) r.load();
    r.load(transport);
    const t = r.motion.transport("story"); t.seek(500); r.advance(0);
    assert.equal(to.children[0].getAttribute("width"), "60");
    t.seek(0); r.advance(16); assert.equal(to.children[0].getAttribute("width"), "20");
    const control = new Element("button", {"data-gosx-transport-action": "pause", "data-gosx-transport": "story"});
    control.disabled = true; r.observers[0]([{addedNodes: [control]}]);
    assert.equal(control.disabled, false, "controls added after the scope still enhance");
    t.seek(500); r.motion.disposePrograms(root); r.advance(32);
    assert.equal(to.children[0].getAttribute("width"), "100", "queued samples cannot write after disposal");
    assert.equal(r.motion.transport("story"), null);
    assert.equal(r.observers.length, 1, "optional features reuse one lifecycle observer");
  }
});
