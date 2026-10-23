// Elements that a patch creates after load must land in the namespace of
// their parent. document.createElement always makes an HTML element, so an
// <svg> or <path> made that way is an HTMLUnknownElement and draws nothing.

import assert from "node:assert/strict";
import test from "node:test";
import {
  HTML_NS, SVG_NS, MATHML_NS, XLINK_NS,
  patchAuthoritySource, patchBundleSource,
  el, loadScene,
} from "./patch-test-dom.mjs";

const PATCH_SET_ATTR = 1;
const PATCH_CREATE_ELEMENT = 3;
const PATCH_REPLACE_ELEMENT = 6;

for (const [label, source] of [["authority", patchAuthoritySource], ["bundle", patchBundleSource]]) {
  const create = (path, tag, at) => ({ kind: PATCH_CREATE_ELEMENT, path, tag, children: [at] });

  test(`created svg and its children are SVG elements (${label})`, () => {
    const scene = loadScene(source, (root, doc) => root.appendChild(el(doc, "div")));
    scene.apply([
      create("0", "svg", 0),
      create("0/0", "g", 0),
      create("0/0/0", "path", 0),
      create("0/0", "polyline", 1),
    ]);
    const svg = scene.root.childNodes[0].childNodes[0];
    assert.equal(svg.namespaceURI, SVG_NS);
    for (const node of [svg.childNodes[0], svg.childNodes[0].childNodes[0], svg.childNodes[1]]) {
      assert.equal(node.namespaceURI, SVG_NS, node.localName);
    }
    assert.equal(svg.childNodes[0].childNodes[0].localName, "path");
  });

  test(`foreignObject children return to HTML (${label})`, () => {
    const scene = loadScene(source, (root, doc) => root.appendChild(el(doc, "svg", { ns: SVG_NS })));
    scene.apply([
      create("0", "foreignObject", 0),
      create("0/0", "div", 0),
      create("0/0/0", "span", 0),
    ]);
    const fo = scene.root.childNodes[0].childNodes[0];
    assert.equal(fo.localName, "foreignObject");
    assert.equal(fo.namespaceURI, SVG_NS);
    assert.equal(fo.childNodes[0].namespaceURI, HTML_NS);
    assert.equal(fo.childNodes[0].childNodes[0].namespaceURI, HTML_NS);
  });

  test(`a replaced svg child stays in the SVG namespace (${label})`, () => {
    const scene = loadScene(source, (root, doc) =>
      root.appendChild(el(doc, "svg", { ns: SVG_NS }, el(doc, "circle", { ns: SVG_NS }))));
    scene.apply([{ kind: PATCH_REPLACE_ELEMENT, path: "0/0", tag: "rect" }]);
    const rect = scene.root.childNodes[0].childNodes[0];
    assert.equal(rect.localName, "rect");
    assert.equal(rect.namespaceURI, SVG_NS);
  });

  test(`a replaced HTML element is replaced with an svg in the SVG namespace (${label})`, () => {
    const scene = loadScene(source, (root, doc) => root.appendChild(el(doc, "span")));
    scene.apply([{ kind: PATCH_REPLACE_ELEMENT, path: "0", tag: "svg" }]);
    assert.equal(scene.root.childNodes[0].namespaceURI, SVG_NS);
  });

  test(`math enters the MathML namespace (${label})`, () => {
    const scene = loadScene(source, (root, doc) => root.appendChild(el(doc, "div")));
    scene.apply([create("0", "math", 0), create("0/0", "mfrac", 0)]);
    const math = scene.root.childNodes[0].childNodes[0];
    assert.equal(math.namespaceURI, MATHML_NS);
    assert.equal(math.childNodes[0].namespaceURI, MATHML_NS);
  });

  test(`SVG attributes keep case and xlink:href uses the xlink namespace (${label})`, () => {
    const scene = loadScene(source, (root, doc) => root.appendChild(el(doc, "div")));
    scene.apply([
      create("0", "svg", 0),
      { kind: PATCH_SET_ATTR, path: "0/0", attrName: "viewBox", text: "0 0 10 10" },
      { kind: PATCH_SET_ATTR, path: "0/0", attrName: "preserveAspectRatio", text: "none" },
      create("0/0", "use", 0),
      { kind: PATCH_SET_ATTR, path: "0/0/0", attrName: "xlink:href", text: "#a" },
    ]);
    const svg = scene.root.childNodes[0].childNodes[0];
    assert.equal(svg.getAttribute("viewBox"), "0 0 10 10");
    assert.equal(svg.getAttribute("preserveAspectRatio"), "none");
    const use = svg.childNodes[0];
    assert.equal(use.getAttribute("xlink:href"), "#a");
    assert.equal(use.getAttributeNamespace("xlink:href"), XLINK_NS);
  });
}
