import test from "node:test";
import assert from "node:assert/strict";
import fs from "node:fs";
import { createRequire } from "node:module";

const require = createRequire(import.meta.url);
const { bootstrapRuntimeSource, bootstrapLiteSource, createContext, runScript, flushAsyncWork, FakeElement } = require("./runtime-test-harness.js");
const services = fs.readFileSync(new URL("./bootstrap-feature-browser-services.js", import.meta.url), "utf8");

test("ordinary runtime pages do not fetch or install optional browser services", async () => {
  for (const source of [bootstrapRuntimeSource, bootstrapLiteSource]) {
    const env = createContext({ manifest: { engines: [] } });
    runScript(source, env.context, "bootstrap.js");
    await flushAsyncWork();
    assert.equal(env.context.__gosx.host.requests?.guard, undefined);
    assert.equal(env.fetchCalls.some(call => String(call.url).includes("browser-services")), false);
  }
});

test("explicit browser services load once from the hashed contract and own native request wrappers", async () => {
  const ref = "/gosx/assets/runtime/bootstrap-feature-browser-services.fixture.js";
  const contract = new FakeElement("script", null);
  contract.id = "gosx-document";
  contract.textContent = JSON.stringify({ version: 1, assets: { bootstrapMode: "full", manifest: true, bootstrapFeatureBrowserServicesPath: ref } });
  const env = createContext({
    elements: [contract],
    fetchRoutes: { [ref]: { text: services }, "/allowed": { text: "allowed" } },
    manifest: { features: ["browser-services", "browser-services"] },
  });
  runScript(bootstrapRuntimeSource, env.context, "bootstrap-runtime.js");
  await flushAsyncWork();
  assert.equal(env.fetchCalls.filter(call => String(call.url) === ref).length, 1);
  const original = env.context.fetch;
  const handle = env.context.__gosx.host.requests.guard({ blockedPaths: ["/blocked"] });
  assert.equal((await env.context.fetch("/blocked?private=true")).status, 204);
  assert.equal(env.fetchCalls.some(call => String(call.url).includes("blocked")), false);
  assert.equal((await env.context.fetch("/allowed")).status, 200);
  handle.dispose();
  handle.dispose();
  assert.equal(env.context.fetch, original);
});
