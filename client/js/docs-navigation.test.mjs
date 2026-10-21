import test from "node:test";
import assert from "node:assert/strict";
import fs from "node:fs";
import vm from "node:vm";

const source = fs.readFileSync(new URL("../../examples/gosx-docs/public/docs-navigation.js", import.meta.url), "utf8");

test("Escape closes only the open docs menu and returns keyboard focus", () => {
  const handlers = [];
  let focused = 0;
  let prevented = 0;
  const window = { location: { hash: "#nav-overlay" } };
  const document = {
    addEventListener(type, handler) {
      assert.equal(type, "keydown");
      handlers.push(handler);
    },
    querySelector(selector) {
      assert.equal(selector, '[aria-controls="nav-overlay"]');
      return { focus() { focused++; } };
    },
  };
  const context = vm.createContext({ window, document });
  vm.runInContext(source, context);
  vm.runInContext(source, context);
  assert.equal(handlers.length, 1, "retained navigation must not duplicate listeners");
  const key = value => ({ key: value, preventDefault() { prevented++; } });
  handlers[0](key("Enter"));
  assert.equal(window.location.hash, "#nav-overlay");
  handlers[0](key("Escape"));
  assert.equal(window.location.hash, "main-content");
  assert.equal(focused, 1);
  assert.equal(prevented, 1);
  window.location.hash = "#other-dialog";
  handlers[0](key("Escape"));
  assert.equal(window.location.hash, "#other-dialog");
  assert.equal(prevented, 1, "do not steal Escape from other demos or dialogs");
});
