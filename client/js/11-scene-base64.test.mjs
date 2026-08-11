import test from "node:test";
import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import vm from "node:vm";
import { fileURLToPath } from "node:url";

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const source = fs.readFileSync(
  path.join(__dirname, "bootstrap-src", "11-scene-base64.ts"),
  "utf8",
);

function loadContext(withAtob) {
  const sandbox = { Buffer, Uint8Array };
  if (withAtob) {
    sandbox.atob = (value) => Buffer.from(value, "base64").toString("binary");
  }
  sandbox.globalThis = sandbox;
  const context = vm.createContext(sandbox);
  vm.runInContext(source, context, { filename: "11-scene-base64.ts" });
  return context;
}

function decode(context, encoded) {
  return Array.from(vm.runInContext(`sceneBase64Decode(${JSON.stringify(encoded)})`, context));
}

test("sceneBase64Decode decodes through the browser atob path", () => {
  const context = loadContext(true);
  assert.deepEqual(decode(context, Buffer.from([0, 1, 127, 128, 255]).toString("base64")), [0, 1, 127, 128, 255]);
});

test("sceneBase64Decode keeps the Node fallback for source-level tests", () => {
  const context = loadContext(false);
  assert.deepEqual(decode(context, Buffer.from("GoSX/O0.2", "utf8").toString("base64")), Array.from(Buffer.from("GoSX/O0.2", "utf8")));
});
