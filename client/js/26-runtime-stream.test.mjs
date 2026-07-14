// Contract tests for bootstrap-owned streamed-template consumption.
import test from "node:test";
import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import vm from "node:vm";
import { fileURLToPath } from "node:url";

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const moduleSrc = fs.readFileSync(
  path.join(__dirname, "bootstrap-src", "26-runtime-stream.js"),
  "utf8"
);

function makeTarget(id) {
  return {
    id,
    parentNode: {
      replaceChild(next, previous) {
        this.replaced = { next, previous };
      },
    },
    replaceWith(next) {
      this.replaced = next;
    },
  };
}

function makeTemplate(targetID) {
  const content = { cloneNode: () => ({ kind: "fragment", targetID }) };
  const template = {
    content,
    getAttribute(name) {
      return name === "data-gosx-stream-target" ? targetID : null;
    },
  };
  template.remove = () => { template.removed = true; };
  return template;
}

function runModule(template, target, options = {}) {
  const events = [];
  const telemetry = [];
  const document = {
    body: { querySelectorAll: () => [template] },
    documentElement: { querySelectorAll: () => [template] },
    getElementById(id) { return id === target.id ? target : null; },
    dispatchEvent(event) { events.push(event); },
  };
  class CustomEvent {
    constructor(type, init = {}) {
      this.type = type;
      this.detail = init.detail;
    }
  }
  const window = {
    __gosx: options.gosx || {},
    __gosx_emit(level, category, message, fields) {
      telemetry.push({ level, category, message, fields });
    },
  };
  const context = { window, document, CustomEvent, console };
  vm.createContext(context);
  vm.runInContext(moduleSrc, context);
  return { context, events, telemetry };
}

test("stream templates replace their target through the bootstrap contract", () => {
  const target = makeTarget("slot-1");
  const template = makeTemplate("slot-1");
  const { context, events, telemetry } = runModule(template, target);

  const consumed = context.window.__gosx_mount_stream_templates(context.document.body);
  assert.equal(consumed, 1);
  assert.equal(target.replaced.kind, "fragment");
  assert.equal(template.removed, true);
  assert.equal(events.at(-1).type, "gosx:stream:consume");
  assert.equal(telemetry.at(-1).message, "stream template consumed");
  assert.equal(context.window.__gosx.stream.consume, context.window.__gosx_mount_stream_templates);
});

test("stream templates remain pending when their target is not present", () => {
  const target = makeTarget("other-slot");
  const template = makeTemplate("missing-slot");
  const { context } = runModule(template, target);

  assert.equal(context.window.__gosx_mount_stream_templates(context.document.body), 0);
  assert.equal(target.replaced, undefined);
});

test("stream templates delegate fragment replacement to the core DOM lifecycle", () => {
  const target = makeTarget("slot-1");
  const template = makeTemplate("slot-1");
  const calls = [];
  const { context } = runModule(template, target, {
    gosx: {
      dom: {
        replaceFragment(receivedTarget, content) {
          calls.push({ receivedTarget, content });
          return true;
        },
      },
    },
  });

  assert.equal(context.window.__gosx.stream.consume(context.document.body), 1);
  assert.equal(calls.length, 1);
  assert.equal(calls[0].receivedTarget, target);
  assert.equal(calls[0].content.kind, "fragment");
  assert.equal(template.removed, true);
  assert.equal(target.replaced, undefined);
});
