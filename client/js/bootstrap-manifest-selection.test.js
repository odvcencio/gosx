"use strict";

const test = require("node:test");
const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const { createContext, runScript } = require("./runtime-test-harness.js");

const corpus = JSON.parse(fs.readFileSync(path.join(__dirname,
  "../../perf/wire/testdata/manifest-selection.json"), "utf8"));
// Execute the authored source itself, not a copy or an extracted function.
const source = fs.readFileSync(path.join(__dirname,
  "bootstrap-src/10-runtime-scene-utils.ts"), "utf8") +
  "\nglobalThis.__corpusLoadManifest = loadManifest;";

function corpusDocument(nodes, document) {
  const children = [];
  function build(node) {
    if (Object.hasOwn(node, "text")) return document.createTextNode(node.text);
    const element = document.createElement(node.tag);
    element.__corpusKey = node.key || "";
    for (const [key, value] of Object.entries(node.attributes || {})) {
      element.setAttribute(key, value);
    }
    // Template content lives in a detached DocumentFragment. The harness's
    // default ID map is not a tree-order lookup and indexes detached nodes;
    // use an actual tree walk for these duplicate/template controls.
    const parent = node.tag === "template"
      ? (element.content = document.createDocumentFragment()) : element;
    for (const child of node.children || []) parent.appendChild(build(child));
    return element;
  }
  for (const node of nodes) {
    const element = build(node);
    children.push(element);
    document.body.appendChild(element);
  }
  document.getElementById = (id) => {
    function find(node) {
      if (node.nodeType === 1 && node.getAttribute("id") === id) return node;
      for (const child of node.childNodes || []) {
        const found = find(child);
        if (found) return found;
      }
      return null;
    }
    return find(document.documentElement);
  };
  return children;
}

function assertSelection(nodes, expected) {
  const env = createContext({});
  corpusDocument(nodes, env.document);
  runScript(source, env.context, "10-runtime-scene-utils.ts");
  const value = env.context.__corpusLoadManifest();
  assert.deepEqual(JSON.parse(JSON.stringify(value)), expected.manifest);
  const candidate = env.document.getElementById("gosx-manifest");
  assert.equal(candidate ? candidate.__corpusKey : "", expected.candidate);
  if (env.context.__gosx_manifest) {
    assert.equal(env.context.__gosx_manifest.element, candidate);
  }
}

for (const fixture of corpus) {
  test("real loadManifest shared corpus: " + fixture.name, () => {
    assertSelection(fixture.document, fixture.expected);
    let child = 0;
    function visit(nodes) {
      for (const node of nodes) {
        if (Object.hasOwn(node, "srcdoc")) {
          assertSelection(node.srcdoc, fixture.childExpected[child++]);
        }
        visit(node.children || []);
      }
    }
    visit(fixture.document);
    assert.equal(child, fixture.childExpected.length);
  });
}
