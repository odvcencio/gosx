"use strict";

// Behavioral parity for the generated, committed minified bootstrap runtime.
// The authored regions suite exercises client/runtime/host/regions.ts directly;
// this test proves the bytes applications actually ship retain the same HTTP
// failure and recovery contract after generation and minification.

const test = require("node:test");
const assert = require("node:assert/strict");

const {
  bootstrapRuntimeSource,
  createContext,
  FakeElement,
  runScript,
} = require("./runtime-test-harness.js");

test("minified regions retain last good DOM on HTTP errors and recover", async () => {
  const region = new FakeElement("div", null);
  region.setAttribute("data-gosx-region", "");
  region.setAttribute("data-gosx-region-url", "/fragment");
  region.innerHTML = "<p>server truth</p>";

  const responses = [
    { ok: false, status: 404, text: "private missing body" },
    { ok: false, status: 503, text: "private upstream body" },
    { ok: true, status: 200, text: "<p>recovered</p>" },
  ];
  const env = createContext({
    elements: [region],
    fetchRoutes: {
      "/fragment": () => responses.shift(),
    },
  });
  const errors = [];
  env.document.addEventListener("gosx:region:error", (event) => {
    errors.push(event.detail);
  });

  runScript(bootstrapRuntimeSource, env.context, "bootstrap-runtime.js");
  const refresh = env.context.__gosx.regions.refresh;

  for (const status of [404, 503]) {
    const pending = refresh(region);
    assert.equal(region.getAttribute("data-gosx-region-state"), "pending");
    assert.equal(region.getAttribute("aria-busy"), "true");
    assert.equal(region.innerHTML, "<p>server truth</p>");
    await pending;

    assert.equal(region.getAttribute("data-gosx-region-state"), "error");
    assert.equal(region.hasAttribute("aria-busy"), false);
    assert.equal(region.hasAttribute("data-gosx-region-request"), false);
    assert.equal(region.innerHTML, "<p>server truth</p>");
    assert.equal(errors.length, status === 404 ? 1 : 2);

    const detail = errors[errors.length - 1];
    assert.equal(detail.element, region);
    assert.equal(detail.url, "/fragment");
    assert.equal(detail.status, status);
    assert.deepEqual(Object.keys(detail).sort(), ["element", "status", "url"]);
    assert.equal("body" in detail, false);
    assert.equal("error" in detail, false);
    assert.equal("headers" in detail, false);
    assert.equal("response" in detail, false);
    assert.equal("statusText" in detail, false);
  }

  const recovery = refresh(region);
  assert.equal(region.getAttribute("data-gosx-region-state"), "pending");
  assert.equal(region.getAttribute("aria-busy"), "true");
  await recovery;

  assert.equal(region.getAttribute("data-gosx-region-state"), "ready");
  assert.equal(region.hasAttribute("aria-busy"), false);
  assert.equal(region.hasAttribute("data-gosx-region-request"), false);
  assert.equal(region.innerHTML, "<p>recovered</p>");
  assert.equal(errors.length, 2, "successful recovery must not emit another error");

  const exposed = errors.map((detail) => ({
    status: detail.status,
    url: detail.url,
  }));
  assert.equal(JSON.stringify(exposed).includes("private"), false);
});
