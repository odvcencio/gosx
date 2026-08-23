// Unit tests for bootstrap-src/05-document-env.js's declarative motion-split
// feature (data-gosx-motion-split="char|word|line" + data-gosx-motion-stagger).
// Runs the real module in a node:vm against a minimal hand-rolled DOM stub,
// mirroring the pattern in 06-declarative-actions.test.mjs.
//
// 05-document-env.js is authored to run inside the bootstrap bundle, after
// 00-textlayout.js has already defined a handful of shared DOM helpers
// (setAttrValue, setStyleValue, walkElementTree) as top-level functions in
// the same closure. Loading the whole of 00-textlayout.js would also pull in
// its font/canvas-dependent top-level init calls, so this harness instead
// supplies verbatim copies of just the three helpers 05 needs, then the real
// 05-document-env.js source, in the same vm context (see 07's harness for
// the same "load a dependency module first" shape).
//
// gosxMotionSplitUnits and playManagedMotionUnits are internal to the
// module -- unlike the declarative-actions/regions modules, 05 does not put
// them on window.__gosx.motion's public surface (mountAll / observe /
// dispose / disposeAll only). A final small script run in the same context
// captures the live top-level function bindings vm.runInContext leaves
// behind, onto window.__test_hooks, so tests can call the split + stagger
// engine directly instead of driving it through the full
// MutationObserver/IntersectionObserver-based attribute-mounting pipeline.
import test from "node:test";
import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import vm from "node:vm";
import { fileURLToPath } from "node:url";

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const moduleSrc = fs.readFileSync(
  path.join(__dirname, "bootstrap-src", "05-document-env.js"),
  "utf8"
);

// Verbatim from bootstrap-src/00-textlayout.js.
const helperSrc = `
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
`;

const exposeSrc = `
  window.__test_hooks = {
    gosxMotionSplitUnits,
    playManagedMotionUnits,
    normalizeGosxMotionSplit,
    GOSX_MOTION_UNIT_CLASS,
    GOSX_MOTION_SPLIT_ATTR,
  };
`;

// --- minimal DOM stub -------------------------------------------------

class FakeTextNode {
  constructor(text) {
    this.nodeType = 3;
    this.textContent = text;
  }
}

class FakeElement {
  constructor(tag) {
    this.nodeType = 1;
    this.tagName = String(tag || "div").toUpperCase();
    this.childNodes = [];
    this._attrs = {};
    this.style = {};
    this.className = "";
  }
  get textContent() {
    return this.childNodes.map((node) => node.textContent || "").join("");
  }
  set textContent(value) {
    this.childNodes = value ? [new FakeTextNode(String(value))] : [];
  }
  appendChild(node) {
    // A DocumentFragment's children move into the parent on append, same as
    // real DOM -- gosxMotionSplitUnits relies on this to flush its fragment.
    if (node && node.nodeType === 11) {
      for (const child of node.childNodes.slice()) {
        this.childNodes.push(child);
      }
      return node;
    }
    this.childNodes.push(node);
    return node;
  }
  getAttribute(name) {
    return Object.prototype.hasOwnProperty.call(this._attrs, name) ? this._attrs[name] : null;
  }
  hasAttribute(name) {
    return Object.prototype.hasOwnProperty.call(this._attrs, name);
  }
  setAttribute(name, value) {
    this._attrs[name] = String(value);
  }
  removeAttribute(name) {
    delete this._attrs[name];
  }
  // Supports only the ".class-name" form gosxMotionSplitUnits issues.
  querySelectorAll(selector) {
    const cls = selector.startsWith(".") ? selector.slice(1) : null;
    const out = [];
    const walk = (node) => {
      for (const child of node.childNodes || []) {
        if (child && child.nodeType === 1) {
          if (cls && String(child.className || "").split(/\s+/).filter(Boolean).includes(cls)) {
            out.push(child);
          }
          walk(child);
        }
      }
    };
    walk(this);
    return out;
  }
}

function makeTextEl(text) {
  const el = new FakeElement("div");
  el.textContent = text;
  return el;
}

function makeDocument() {
  return {
    createDocumentFragment() {
      return {
        nodeType: 11,
        childNodes: [],
        appendChild(node) {
          this.childNodes.push(node);
          return node;
        },
      };
    },
    createElement(tag) {
      return new FakeElement(tag);
    },
    createTextNode(text) {
      return new FakeTextNode(text);
    },
    addEventListener() {},
    removeEventListener() {},
    dispatchEvent() {},
    getElementById() {
      return null;
    },
    visibilityState: "visible",
    title: "",
  };
}

// runModule loads the helpers, the real 05-document-env.js source, and the
// internal-hooks capture script into one shared vm context, then returns the
// exposed hooks plus the raw context for anything else a test might need.
function runModule() {
  const ctx = { console };
  ctx.document = makeDocument();
  ctx.window = {
    __gosx: {},
    addEventListener() {},
    navigator: {},
  };
  ctx.window.document = ctx.document;
  class CustomEvent {
    constructor(type, init = {}) {
      this.type = type;
      this.detail = init.detail;
    }
  }
  ctx.CustomEvent = CustomEvent;
  vm.createContext(ctx);
  vm.runInContext(helperSrc, ctx);
  vm.runInContext(moduleSrc, ctx);
  vm.runInContext(exposeSrc, ctx);
  return ctx.window.__test_hooks;
}

// A no-op Animation-shaped return value: playManagedMotionUnits only reads
// .cancel and .finished.then off it, and none of these tests await settling.
function fakeAnimation() {
  return {
    cancel() {},
    finished: { then() {} },
  };
}

// --- char mode ----------------------------------------------------------

test("char mode: one unit per non-whitespace character, whitespace stays plain text, textContent unchanged", () => {
  const hooks = runModule();
  const original = "  Hi\tthere,  friend!  ";
  const el = makeTextEl(original);

  const units = hooks.gosxMotionSplitUnits(el, "char");

  const nonWhitespaceCount = original.replace(/\s+/g, "").length;
  assert.equal(units.length, nonWhitespaceCount);
  for (const unit of units) {
    assert.equal(unit.tagName, "SPAN");
    assert.equal(unit.className, hooks.GOSX_MOTION_UNIT_CLASS);
    assert.equal(unit.style.display, "inline-block");
    assert.equal(unit.textContent.length, 1);
    assert.doesNotMatch(unit.textContent, /\s/);
  }

  // Whitespace must survive as plain text nodes, not spans, so word spacing
  // and line breaking are unaffected by the split.
  const textNodes = el.childNodes.filter((node) => node.nodeType === 3);
  assert.ok(textNodes.length > 0, "expected at least one whitespace text node");
  for (const node of textNodes) {
    assert.match(node.textContent, /^\s+$/);
  }

  assert.equal(el.textContent, original, "reconstructed textContent must be byte-identical to the source");
});

// --- word mode ------------------------------------------------------------

test("word mode: one unit per word, spacing preserved, textContent unchanged", () => {
  const hooks = runModule();
  const original = "Hello  brave new\tworld";
  const el = makeTextEl(original);

  const units = hooks.gosxMotionSplitUnits(el, "word");

  // `units` is an Array from the vm context's realm; Array.from(...) here
  // runs in this file's realm so the .map result below can be compared
  // against a plain array literal without deepStrictEqual's cross-realm
  // prototype check tripping (it treats same-shape, different-realm arrays
  // as not equal).
  assert.deepEqual(Array.from(units).map((unit) => unit.textContent), ["Hello", "brave", "new", "world"]);
  for (const unit of units) {
    assert.equal(unit.tagName, "SPAN");
    assert.equal(unit.className, hooks.GOSX_MOTION_UNIT_CLASS);
  }
  assert.equal(el.textContent, original, "reconstructed textContent must be byte-identical to the source");
});

// --- line mode --------------------------------------------------------

test("line mode: one unit per newline-delimited line", () => {
  const hooks = runModule();
  const original = "First line\nSecond line\nThird line";
  const el = makeTextEl(original);

  const units = hooks.gosxMotionSplitUnits(el, "line");

  assert.deepEqual(Array.from(units).map((unit) => unit.textContent), ["First line", "Second line", "Third line"]);
  for (const unit of units) {
    assert.equal(unit.tagName, "SPAN");
    assert.equal(unit.className, hooks.GOSX_MOTION_UNIT_CLASS);
  }
});

// BUG (found while testing, implementation left unchanged): unlike char and
// word mode, line mode's split does not preserve its separators. It splits
// on `text.split(/\r?\n/)` with no capturing group, so the newline
// characters between lines are discarded rather than kept as plain text
// nodes. Rebuilding the element from the returned spans therefore loses both
// the newlines AND any inter-line spacing -- textContent goes from
// "First line\nSecond line" to "First lineSecond line" (line 3 words run
// together with no separator at all, not even a line break). Char and word
// mode do not have this defect: both use a capturing split
// (`text.split(/(\s+)/)` for word) that keeps whitespace as sibling text
// nodes. This test pins the current (lossy) behavior; it is not asserting
// this is correct.
test("line mode currently drops the newline separators between lines (known gap, not fixed here)", () => {
  const hooks = runModule();
  const original = "First line\nSecond line\nThird line";
  const el = makeTextEl(original);

  hooks.gosxMotionSplitUnits(el, "line");

  assert.equal(
    el.textContent,
    "First lineSecond lineThird line",
    "documents that line-mode reconstruction currently loses the newline separators"
  );
  assert.notEqual(el.textContent, original, "if this ever equals `original`, the gap above has been fixed upstream");
});

test("line mode falls back to sentence splitting when the text has no newlines, and that path is lossless", () => {
  const hooks = runModule();
  const original = "First sentence. Second sentence! Third?";
  const el = makeTextEl(original);

  const units = hooks.gosxMotionSplitUnits(el, "line");

  assert.deepEqual(Array.from(units).map((unit) => unit.textContent), ["First sentence. ", "Second sentence! ", "Third?"]);
  assert.equal(el.textContent, original, "the sentence-fallback regex keeps trailing punctuation/space, so no content is lost");
});

// --- idempotency ------------------------------------------------------

test("idempotency: re-running the same split mode does not nest spans (soft-navigation replay guard)", () => {
  const hooks = runModule();
  const original = "abc def";
  const el = makeTextEl(original);

  const firstPass = hooks.gosxMotionSplitUnits(el, "char");
  const childCountAfterFirst = el.childNodes.length;
  assert.equal(el.getAttribute(hooks.GOSX_MOTION_SPLIT_ATTR), "char");

  const secondPass = hooks.gosxMotionSplitUnits(el, "char");

  assert.equal(el.childNodes.length, childCountAfterFirst, "a second split must not add any new top-level nodes");
  assert.deepEqual(secondPass, firstPass, "a second split with the same mode returns the existing units, unchanged");
  for (const unit of el.querySelectorAll("." + hooks.GOSX_MOTION_UNIT_CLASS)) {
    // No unit should contain another unit -- i.e. no nested
    // .gosx-motion-unit spans from a re-wrap of already-split output.
    assert.equal(unit.querySelectorAll("." + hooks.GOSX_MOTION_UNIT_CLASS).length, 0);
  }
});

// --- empty / whitespace-only text --------------------------------------

test("empty text produces no units and does not throw", () => {
  const hooks = runModule();
  const el = makeTextEl("");
  assert.doesNotThrow(() => {
    const units = hooks.gosxMotionSplitUnits(el, "char");
    assert.equal(units.length, 0);
  });
  assert.equal(el.hasAttribute(hooks.GOSX_MOTION_SPLIT_ATTR), false);
});

test("whitespace-only text produces no units and does not throw", () => {
  const hooks = runModule();
  const el = makeTextEl("   \n\t  ");
  assert.doesNotThrow(() => {
    const units = hooks.gosxMotionSplitUnits(el, "word");
    assert.equal(units.length, 0);
  });
  assert.equal(el.hasAttribute(hooks.GOSX_MOTION_SPLIT_ATTR), false);
});

// --- invalid split value -------------------------------------------------

test("an invalid split value normalizes to no-split", () => {
  const hooks = runModule();
  assert.equal(hooks.normalizeGosxMotionSplit("sentence"), "");
  assert.equal(hooks.normalizeGosxMotionSplit("CHAR"), "char", "recognized values still normalize case-insensitively");
  assert.equal(hooks.normalizeGosxMotionSplit(""), "");
  assert.equal(hooks.normalizeGosxMotionSplit(undefined), "");
});

// --- stagger + fail-safe fill:"both" -------------------------------------

test("stagger: unit N receives delay = base delay + stagger * N", () => {
  const hooks = runModule();
  const el = makeTextEl("abcd");
  const units = hooks.gosxMotionSplitUnits(el, "char");
  assert.equal(units.length, 4);

  const calls = [];
  units.forEach((unit, index) => {
    unit.animate = (keyframes, options) => {
      calls.push({ index, keyframes, options });
      return fakeAnimation();
    };
  });

  const record = {
    element: el,
    config: {
      preset: "fade",
      duration: 220,
      delay: 50,
      easing: "linear",
      stagger: 30,
    },
  };
  hooks.playManagedMotionUnits(record, units);

  assert.equal(calls.length, 4);
  calls.forEach((call, index) => {
    assert.equal(call.options.delay, 50 + 30 * index, `unit ${index} delay`);
    assert.equal(call.options.duration, 220);
    assert.equal(call.options.easing, "linear");
  });

  // The record tracks the animation with the largest delay -- normally the
  // last unit -- as its representative "when does this settle" animation.
  assert.equal(record.unitAnimations.length, 4);
});

test("stagger of 0 gives every unit the same (base) delay", () => {
  const hooks = runModule();
  const el = makeTextEl("xyz");
  const units = hooks.gosxMotionSplitUnits(el, "char");

  const calls = [];
  units.forEach((unit) => {
    unit.animate = (keyframes, options) => {
      calls.push(options);
      return fakeAnimation();
    };
  });

  hooks.playManagedMotionUnits(
    { element: el, config: { preset: "fade", duration: 200, delay: 10, easing: "linear", stagger: 0 } },
    units
  );

  assert.deepEqual(calls.map((options) => options.delay), [10, 10, 10]);
});

test("fail-safe: every unit animation is created with fill: \"both\" (no CSS pre-hide class)", () => {
  // This is the whole reason the feature is built this way: Web Animations
  // with fill:"both" holds the pre-animation state for the delay and the
  // post-animation state after finishing, entirely inside the animation
  // itself. There is deliberately no CSS class that pre-hides the text, so
  // if the animation never starts (JS error, animate() unsupported, etc.)
  // the text is simply readable -- never permanently invisible.
  const hooks = runModule();
  const el = makeTextEl("word");
  const units = hooks.gosxMotionSplitUnits(el, "char");

  const calls = [];
  units.forEach((unit) => {
    unit.animate = (keyframes, options) => {
      calls.push(options);
      return fakeAnimation();
    };
  });

  hooks.playManagedMotionUnits(
    { element: el, config: { preset: "fade", duration: 220, delay: 0, easing: "linear", stagger: 20 } },
    units
  );

  assert.equal(calls.length, units.length);
  for (const options of calls) {
    assert.equal(options.fill, "both");
  }
});

test("playManagedMotionUnits with no units settles the record to finished without calling animate", () => {
  const hooks = runModule();
  const el = makeTextEl("unused");
  const record = { element: el, config: { preset: "fade", duration: 200, delay: 0, easing: "linear", stagger: 0 } };
  assert.doesNotThrow(() => {
    hooks.playManagedMotionUnits(record, []);
  });
  assert.equal(el.getAttribute("data-gosx-motion-state"), "finished");
});
