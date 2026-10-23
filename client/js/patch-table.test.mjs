// Rows inside <table> must patch the nodes the island's tree means.
//
// The island's tree has table > tr. The browser's HTML parser moves a <tr>
// that is a direct child of <table> into an implicit <tbody>, so the live DOM
// was table > tbody > tr and patch paths pointed at the wrong nodes. The
// server now writes the <tbody> itself and marks it data-gosx-implicit; the
// runtime treats a marked <tbody> as transparent.

import assert from "node:assert/strict";
import test from "node:test";
import {
  patchAuthoritySource, patchBundleSource,
  el, loadScene, describe,
} from "./patch-test-dom.mjs";

const SET_TEXT = 0;
const CREATE_ELEMENT = 3;
const CREATE_TEXT = 4;
const REMOVE_ELEMENT = 5;
const REORDER = 7;

const MARK = { attrs: { "data-gosx-implicit": "" } };

// row builds <tr><td>label</td></tr>.
const row = (doc, label) => el(doc, "tr", {}, el(doc, "td", {}, label));

// A server-rendered table as the browser holds it after parsing the markup the
// island renderer writes: thead, a marked tbody holding the rows, and an
// optional tfoot. Island tree: table > [thead, tr..., tfoot].
function serverTable({ head = true, foot = true, rows = ["a", "b", "c"] } = {}) {
  return (root, doc) => {
    const kids = [];
    if (head) kids.push(el(doc, "thead", {}, el(doc, "tr", {}, el(doc, "th", {}, "head"))));
    if (rows.length > 0) kids.push(el(doc, "tbody", MARK, ...rows.map((label) => row(doc, label))));
    if (foot) kids.push(el(doc, "tfoot", {}, el(doc, "tr", {}, el(doc, "td", {}, "foot"))));
    root.appendChild(el(doc, "table", {}, ...kids));
  };
}

const labels = (scene) => {
  const table = scene.root.childNodes[0];
  const out = [];
  const walk = (node) => {
    for (const kid of node.childNodes) {
      if (kid.localName === "tr") out.push(kid.textContent);
      else if (kid.nodeType === 1) walk(kid);
    }
  };
  walk(table);
  return out;
};

// physical lists the table's direct children, to prove where rows went.
const physical = (scene) => scene.root.childNodes[0].childNodes.map((kid) => kid.localName);

for (const [label, source] of [["authority", patchAuthoritySource], ["bundle", patchBundleSource]]) {
  test(`a text patch reaches the right row beside thead and tfoot (${label})`, () => {
    const scene = loadScene(source, serverTable());
    // Island tree: 0 thead, 1 a, 2 b, 3 c, 4 tfoot.
    scene.apply([
      { kind: SET_TEXT, path: "0/2/0/0", text: "B!" },
      { kind: SET_TEXT, path: "0/4/0/0", text: "foot!" },
      { kind: SET_TEXT, path: "0/0/0/0", text: "head!" },
    ]);
    assert.deepEqual(labels(scene), ["head!", "a", "B!", "c", "foot!"]);
    assert.deepEqual(scene.warnings, []);
  });

  test(`an inserted row lands in the row group at the right place (${label})`, () => {
    const scene = loadScene(source, serverTable());
    scene.apply([
      { kind: CREATE_ELEMENT, path: "0", tag: "tr", children: [2] },
      { kind: CREATE_ELEMENT, path: "0/2", tag: "td", children: [0] },
      { kind: CREATE_TEXT, path: "0/2/0", text: "new", children: [0] },
    ]);
    assert.deepEqual(labels(scene), ["head", "a", "new", "b", "c", "foot"]);
    // The row went into the marked tbody, not beside it.
    assert.deepEqual(physical(scene), ["thead", "tbody", "tfoot"]);
  });

  test(`a row appended before tfoot joins the row group (${label})`, () => {
    const scene = loadScene(source, serverTable());
    scene.apply([
      { kind: CREATE_ELEMENT, path: "0", tag: "tr", children: [4] },
      { kind: CREATE_ELEMENT, path: "0/4", tag: "td", children: [0] },
      { kind: CREATE_TEXT, path: "0/4/0", text: "last", children: [0] },
      { kind: SET_TEXT, path: "0/5/0/0", text: "foot!" },
    ]);
    assert.deepEqual(labels(scene), ["head", "a", "b", "c", "last", "foot!"]);
    assert.deepEqual(physical(scene), ["thead", "tbody", "tfoot"]);
  });

  test(`a row appended at the very end joins the row group (${label})`, () => {
    const scene = loadScene(source, serverTable({ foot: false }));
    scene.apply([
      { kind: CREATE_ELEMENT, path: "0", tag: "tr", children: [4] },
      { kind: CREATE_ELEMENT, path: "0/4", tag: "td", children: [0] },
      { kind: CREATE_TEXT, path: "0/4/0", text: "last", children: [0] },
    ]);
    assert.deepEqual(labels(scene), ["head", "a", "b", "c", "last"]);
    assert.deepEqual(physical(scene), ["thead", "tbody"]);
  });

  test(`removing rows removes the right ones (${label})`, () => {
    const scene = loadScene(source, serverTable());
    scene.apply([
      { kind: REMOVE_ELEMENT, path: "0/2" },
      { kind: REMOVE_ELEMENT, path: "0/1" },
    ]);
    assert.deepEqual(labels(scene), ["head", "c", "foot"]);
  });

  test(`removing every row and adding one back works (${label})`, () => {
    const scene = loadScene(source, serverTable());
    scene.apply([
      { kind: REMOVE_ELEMENT, path: "0/3" },
      { kind: REMOVE_ELEMENT, path: "0/2" },
      { kind: REMOVE_ELEMENT, path: "0/1" },
      { kind: CREATE_ELEMENT, path: "0", tag: "tr", children: [1] },
      { kind: CREATE_ELEMENT, path: "0/1", tag: "td", children: [0] },
      { kind: CREATE_TEXT, path: "0/1/0", text: "again", children: [0] },
    ]);
    assert.deepEqual(labels(scene), ["head", "again", "foot"]);
    assert.deepEqual(physical(scene), ["thead", "tbody", "tfoot"]);
  });

  test(`reordering a table's rows reorders the right nodes (${label})`, () => {
    const scene = loadScene(source, serverTable({ head: false, foot: false }));
    // Island tree: table > [a, b, c]; every child is a keyed row.
    scene.apply([
      { kind: REORDER, path: "0", children: [2, 0, 1] },
      { kind: SET_TEXT, path: "0/0/0/0", text: "C!" },
    ]);
    assert.deepEqual(labels(scene), ["C!", "a", "b"]);
    assert.deepEqual(physical(scene), ["tbody"]);
  });

  test(`reordering rows beside thead and tfoot keeps their places (${label})`, () => {
    const scene = loadScene(source, serverTable());
    scene.apply([{ kind: REORDER, path: "0", children: [0, 3, 2, 1, 4] }]);
    assert.deepEqual(labels(scene), ["head", "c", "b", "a", "foot"]);
    assert.deepEqual(physical(scene), ["thead", "tbody", "tfoot"]);
  });

  test(`a table with no rows takes rows directly and keeps patching (${label})`, () => {
    const scene = loadScene(source, serverTable({ rows: [], head: false, foot: false }));
    scene.apply([
      { kind: CREATE_ELEMENT, path: "0", tag: "tr", children: [0] },
      { kind: CREATE_ELEMENT, path: "0/0", tag: "td", children: [0] },
      { kind: CREATE_TEXT, path: "0/0/0", text: "one", children: [0] },
      { kind: CREATE_ELEMENT, path: "0", tag: "tr", children: [1] },
      { kind: CREATE_ELEMENT, path: "0/1", tag: "td", children: [0] },
      { kind: CREATE_TEXT, path: "0/1/0", text: "two", children: [0] },
      { kind: SET_TEXT, path: "0/0/0/0", text: "ONE" },
      { kind: REMOVE_ELEMENT, path: "0/1" },
    ]);
    assert.deepEqual(labels(scene), ["ONE"]);
  });

  test(`an explicit tbody in the island tree is an ordinary child (${label})`, () => {
    const scene = loadScene(source, (root, doc) =>
      root.appendChild(el(doc, "table", {}, el(doc, "tbody", {}, row(doc, "x"), row(doc, "y")))));
    // Island tree: table > tbody > [x, y].
    scene.apply([{ kind: SET_TEXT, path: "0/0/1/0/0", text: "Y!" }]);
    assert.deepEqual(labels(scene), ["x", "Y!"]);
    assert.deepEqual(scene.warnings, []);
  });

  test(`rows keep their order across many mixed operations (${label})`, () => {
    const scene = loadScene(source, serverTable({ rows: ["a", "b", "c", "d"] }));
    scene.apply([
      { kind: REMOVE_ELEMENT, path: "0/2" },
      { kind: CREATE_ELEMENT, path: "0", tag: "tr", children: [1] },
      { kind: CREATE_ELEMENT, path: "0/1", tag: "td", children: [0] },
      { kind: CREATE_TEXT, path: "0/1/0", text: "n", children: [0] },
      { kind: REORDER, path: "0", children: [0, 2, 1, 3, 4, 5] },
      { kind: SET_TEXT, path: "0/1/0/0", text: "a2" },
    ]);
    assert.deepEqual(labels(scene), ["head", "a2", "n", "c", "d", "foot"].map((x) => x));
  });
}
