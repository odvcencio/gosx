// A failed or missing feature chunk disables only that feature. Every other
// feature still mounts, and the page finishes initialization.
import test from "node:test";
import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import { createRequire } from "node:module";
import { fileURLToPath } from "node:url";
const require = createRequire(import.meta.url);
const here = path.dirname(fileURLToPath(import.meta.url));
const H = require("./runtime-test-harness.js");
const { bootstrapRuntimeSource, createContext, runScript, flushAsyncWork, FakeElement } = H;
const chunkSource = (name) => fs.readFileSync(path.join(here, `bootstrap-feature-${name}.js`), "utf8");

const url = (name) => `/gosx/bootstrap-feature-${name}.js`;
const NON_LEGACY = "probe-fixture";
const GOOD_NON_LEGACY = `(function(){ window.__gosx_register_bootstrap_feature("${NON_LEGACY}", function() { window.__probe_ran = true; return {}; }); })();`;

// A page with an ordinary engine (needs the engines chunk) and a hub (needs the
// hubs chunk), plus an explicit requirement for `extra` when given. `fail`
// maps feature name to "throw" (the chunk fails to evaluate) or "missing" (the
// build did not ship it).
function page({ extra, fail = {} }) {
  const mount = new FakeElement("div", null);
  mount.id = "js-root";
  const contract = new FakeElement("script", null);
  contract.id = "gosx-document";
  contract.textContent = JSON.stringify({ version: 1, assets: { bootstrapMode: "full", manifest: true } });
  const sockets = [];
  const routes = {
    [url("engines")]: { text: chunkSource("engines") },
    [url("hubs")]: { text: chunkSource("hubs") },
    [url("islands")]: { text: chunkSource("islands") },
    [url("controllers")]: { text: chunkSource("controllers") },
    [url(NON_LEGACY)]: { text: GOOD_NON_LEGACY },
  };
  for (const [name, mode] of Object.entries(fail)) {
    if (mode === "throw") routes[url(name)] = { text: "throw new Error('chunk evaluation failed');" };
    else if (name === "textlayout") routes[url(name)] = { text: "throw new Error('chunk missing');" };
    else delete routes[url(name)];
  }
  const manifest = {
    engines: [{ id: "js-engine", component: "JSFixture", kind: "surface", mountId: "js-root" }],
    hubs: [{ id: "hub-1", name: "presence", path: "/gosx/hub/presence", bindings: [{ event: "snapshot", signal: "$presence" }] }],
  };
  if (extra) manifest.features = [extra];
  const env = createContext({
    elements: [contract, mount],
    engineFactories: { JSFixture() { return { dispose() {} }; } },
    createWebSocket(u) {
      const socket = { url: u, close() {} };
      sockets.push(socket);
      return socket;
    },
    fetchRoutes: routes,
    manifest,
  });
  return { env, sockets };
}

async function boot(options) {
  const fixture = page(options);
  runScript(bootstrapRuntimeSource, fixture.env.context, "bootstrap-runtime.js");
  await flushAsyncWork();
  await new Promise((resolve) => setTimeout(resolve, 20));
  await flushAsyncWork();
  return fixture;
}

function failureLogs(env, name) {
  return env.consoleLogs.error.filter((line) => line.includes(name) && line.includes(url(name)));
}

test("all chunks load: behavior is unchanged", async () => {
  const { env, sockets } = await boot({ extra: NON_LEGACY });
  assert.equal(env.context.__gosx.ready, true);
  assert.ok(env.context.__gosx.engines.get("js-engine"), "engine mounts");
  assert.equal(sockets.length, 1, "hub connects");
  assert.equal(env.context.__probe_ran, true, "the non-legacy chunk runs");
  assert.equal(env.consoleLogs.error.length, 0, env.consoleLogs.error.join("\n"));
});

for (const mode of ["throw", "missing"]) {
  for (const name of ["islands", "controllers", "textlayout", NON_LEGACY]) {
    test(`explicit ${name} that fails (${mode}) disables only itself`, async () => {
      const { env, sockets } = await boot({ extra: name, fail: { [name]: mode } });
      assert.equal(env.context.__gosx.ready, true, "page finishes initialization");
      assert.ok(env.context.__gosx.engines.get("js-engine"), "engines still mount");
      assert.equal(sockets.length, 1, "hubs still connect");
      assert.equal(failureLogs(env, name).length, 1, `one error naming ${name} and its URL: ${env.consoleLogs.error.join(" | ")}`);
    });
  }

  test(`engines chunk that fails (${mode}) skips engine mounts but not hubs`, async () => {
    const { env, sockets } = await boot({ fail: { engines: mode } });
    assert.equal(env.context.__gosx.ready, true);
    assert.equal(env.context.__gosx.engines.get("js-engine"), undefined, "engine mounts are skipped");
    assert.equal(sockets.length, 1, "hubs still connect");
    assert.equal(failureLogs(env, "engines").length, 1, env.consoleLogs.error.join(" | "));
  });

  test(`hubs chunk that fails (${mode}) skips hub connections but not engines`, async () => {
    const { env, sockets } = await boot({ fail: { hubs: mode } });
    assert.equal(env.context.__gosx.ready, true);
    assert.ok(env.context.__gosx.engines.get("js-engine"), "engines still mount");
    assert.equal(sockets.length, 0, "hub connections are skipped");
    assert.equal(failureLogs(env, "hubs").length, 1, env.consoleLogs.error.join(" | "));
  });
}
