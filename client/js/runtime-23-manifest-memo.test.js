"use strict";
// Manifest parse memoization and opt-in text release.
//
// The inline #gosx-manifest JSON can reach hundreds of kilobytes, and before
// memoization the WebGPU probe and the runtime tail each ran their own full
// JSON.parse of it during one boot. The parse now happens once per element,
// the result is published as window.__gosx_manifest for other bundles, and a
// page that sets data-gosx-release on the script tag additionally has the
// JSON text dropped from the DOM after the parse — the string is dead weight
// once the object graph exists.

const test = require("node:test");
const assert = require("node:assert/strict");

const {
  bootstrapSource,
  createContext,
  runScript,
  flushAsyncWork,
} = require("./runtime-test-harness.js");

const MEMO_MANIFEST = {
  runtime: { path: "/runtime.wasm" },
  islands: [
    {
      id: "gosx-island-memo",
      component: "Counter",
      props: { initial: 1 },
      programRef: "/counter.json",
    },
  ],
};

function memoContext() {
  return createContext({
    fetchRoutes: {
      "/runtime.wasm": { bytes: [0, 97, 115, 109] },
      "/counter.json": { text: '{"name":"Counter"}' },
    },
    manifest: MEMO_MANIFEST,
    onAction: () => 1,
  });
}

test("boot publishes the memoized manifest parse as window.__gosx_manifest", async () => {
  const env = memoContext();
  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();

  const memo = env.context.__gosx_manifest;
  assert.ok(memo, "boot must publish the parse");
  assert.equal(memo.element, env.context.document.getElementById("gosx-manifest"));
  assert.equal(memo.value.islands[0].id, "gosx-island-memo");
  // This manifest carries no "label" text, and the flag is captured at parse
  // time so feature selection still works after a text release.
  assert.equal(memo.textHasLabel, false);
});

test("manifest text is retained by default", async () => {
  const env = memoContext();
  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();

  const el = env.context.document.getElementById("gosx-manifest");
  assert.ok(el.textContent.length > 0, "without data-gosx-release the JSON text must stay in the DOM");
});

test("data-gosx-release drops the manifest text after the parse", async () => {
  const env = memoContext();
  const el = env.context.document.getElementById("gosx-manifest");
  el.setAttribute("data-gosx-release", "");
  const originalLength = el.textContent.length;
  assert.ok(originalLength > 0);

  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();

  assert.equal(el.textContent, "", "released text must leave the DOM");
  // The parse must still have succeeded and been published: releasing the
  // text must never cost the boot its manifest.
  const memo = env.context.__gosx_manifest;
  assert.ok(memo && memo.value, "memo survives the release");
  assert.equal(memo.value.islands[0].component, "Counter");

  // The boot must be indistinguishable from a retained boot: same ready flag,
  // same published manifest shape.
  const retained = memoContext();
  runScript(bootstrapSource, retained.context, "bootstrap.js");
  await flushAsyncWork();
  assert.equal(env.context.__gosx.ready, retained.context.__gosx.ready, "release must not change boot outcome");
  assert.deepEqual(
    Object.keys(memo.value).sort(),
    Object.keys(retained.context.__gosx_manifest.value).sort()
  );
});

test("textHasLabel is captured before the text is released", async () => {
  const env = createContext({
    fetchRoutes: {
      "/runtime.wasm": { bytes: [0, 97, 115, 109] },
    },
    manifest: {
      runtime: { path: "/runtime.wasm" },
      islands: [],
      // A property whose serialized form contains "label", as a scene label
      // entry would.
      engines: [{ component: "Probe", mountId: "x", props: { label: "sun" } }],
    },
  });
  const el = env.context.document.getElementById("gosx-manifest");
  el.setAttribute("data-gosx-release", "");

  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();

  const memo = env.context.__gosx_manifest;
  assert.ok(memo);
  assert.equal(memo.textHasLabel, true, "label flag must come from the pre-release text");
  assert.equal(el.textContent, "");
});
