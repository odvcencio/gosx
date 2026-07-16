import test from "node:test";
import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import vm from "node:vm";
import { fileURLToPath } from "node:url";

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const moduleSrc = fs.readFileSync(path.join(__dirname, "prose-runtime.js"), "utf8");

test("editor prose adapter delegates keyed reconciliation to core with its diagram signature", () => {
  const calls = [];
  const stream = {
    reconcileHTML(root, html, options) {
      calls.push({ method: "html", root, html, options });
      return "html-result";
    },
    reconcileBlocks(root, blocks, options) {
      calls.push({ method: "blocks", root, blocks, options });
      return "blocks-result";
    },
    createBlockStream(root, options) {
      calls.push({ method: "create", root, options });
      return "stream-result";
    },
  };
  const window = { __gosx: { stream } };
  const context = { window, console };
  vm.createContext(context);
  vm.runInContext(moduleSrc, context);

  const root = {};
  assert.equal(window.GosxProse.reconcileHTML(root, "<p>x</p>"), "html-result");
  assert.equal(window.GosxProse.reconcileBlocks(root, []), "blocks-result");
  assert.equal(window.GosxProse.createBlockStream(root), "stream-result");
  assert.equal(calls.length, 3);
  assert.equal(calls[0].options.keyAttribute, "data-gosx-prose-key");
  assert.equal(typeof calls[0].options.signature, "function");
  assert.equal(calls[0].options, calls[1].options);
});
