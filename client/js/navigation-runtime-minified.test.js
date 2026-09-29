"use strict";
// Proves the committed, minified navigation runtime artifact
// (client/runtime/host/navigation-runtime.min.js) parses and boots the same
// way its readable source does (gosx#221).
//
// Every behavioral navigation test in this suite (runtime-14-navigation.
// test.js and friends) runs against navigationSource — compatibility.ts and
// navigation.ts, unminified — never against this artifact. That is
// deliberate: the source is what a person reads and what a diff shows, and
// cmd/buildbootstrap's --check gate (make test-js) is what proves the
// committed .min.js stays byte-current with it. This file's only job is the
// one thing --check cannot prove: that the minified bytes are still valid,
// executable JavaScript whose top-level IIFE runs to completion and installs
// the same public surface the source installs.

const test = require("node:test");
const assert = require("node:assert/strict");

const {
  navigationSource,
  navigationRuntimeMinifiedSource,
  FakeElement,
  appendManagedHead,
  buildNavigatedDocument,
  createContext,
  runScript,
} = require("./runtime-test-harness.js");

test("the minified navigation runtime artifact parses and boots", () => {
  const env = createContext({ elements: [] });
  assert.doesNotThrow(() => {
    runScript(navigationRuntimeMinifiedSource, env.context, "navigation-runtime.min.js");
  });

  const navigation = env.context.__gosx && env.context.__gosx.navigation;
  assert.equal(typeof navigation, "object", "the IIFE must publish window.__gosx.navigation");
  assert.equal(typeof navigation.navigate, "function");
  assert.equal(typeof navigation.submitAction, "function");
  assert.equal(typeof navigation.getState, "function");
  assert.equal(typeof navigation.refresh, "function");
  assert.equal(typeof navigation.revalidate, "function");

  // The compatibility adapter's ambient names (compatibility.ts, the first
  // half of this artifact) must resolve too: navigation.ts calls
  // gosxHostCompatibility.install for both at the end of its own IIFE.
  assert.equal(env.context.__gosx_page_nav, navigation, "__gosx_page_nav must be installed and alias the same navigation object");
  assert.equal(typeof env.context.__gosx_submit_action, "function");
});

test("the external navigation runtime persists through pages that omit it", async () => {
  const parsedDocs = new Map();
  const env = createContext({
    elements: [],
    fetchRoutes: {
      "http://localhost:3000/plain": { text: "__PLAIN__", url: "http://localhost:3000/plain" },
      "http://localhost:3000/next": { text: "__NEXT__", url: "http://localhost:3000/next" },
    },
    parseHTML(html) { return parsedDocs.get(html); },
  });
  env.document.readyState = "interactive"; // A defer script runs after parsing.
  const script = new FakeElement("script", null);
  script.setAttribute("data-gosx-navigation", "true");
  script.setAttribute("src", "/gosx/assets/runtime/navigation.0123456789abcdef.js");
  script.setAttribute("nonce", "original-nonce");
  appendManagedHead(env.document, [script]);

  parsedDocs.set("__PLAIN__", buildNavigatedDocument({ title: "Plain", bodyNodes: [] }));
  const incomingScript = script.cloneNode(true);
  incomingScript.setAttribute("nonce", "next-page-nonce");
  const link = new FakeElement("a", null);
  link.setAttribute("href", "/plain");
  link.setAttribute("data-gosx-link", "true");
  parsedDocs.set("__NEXT__", buildNavigatedDocument({ title: "Next", headNodes: [incomingScript], bodyNodes: [link] }));
  runScript(navigationRuntimeMinifiedSource, env.context, "navigation-runtime.min.js");
  const navigation = env.context.__gosx.navigation;
  const listenerCount = env.document.eventListeners.get("click").length;

  assert.equal(await navigation.navigate("http://localhost:3000/plain"), true);
  assert.equal(script.parentNode, env.document.head, "a page without the tag keeps the original runtime");
  assert.equal(await navigation.navigate("http://localhost:3000/next"), true);
  assert.equal(env.context.__gosx.navigation, navigation);
  assert.equal(script.parentNode, env.document.head);
  assert.equal(script.getAttribute("nonce"), "original-nonce");
  assert.equal(env.document.head.querySelectorAll('script[data-gosx-navigation="true"]').length, 1);
  assert.equal(env.document.eventListeners.get("click").length, listenerCount);
  assert.equal(env.document.body.querySelector('a[data-gosx-link="true"]').getAttribute("data-gosx-link-state"), "idle");
});

test("the minified artifact publishes the same navigation API surface as the source", () => {
  const sourceEnv = createContext({ elements: [] });
  runScript(navigationSource, sourceEnv.context, "navigation_runtime_source.js");
  const sourceKeys = Object.keys(sourceEnv.context.__gosx.navigation).sort();

  const minifiedEnv = createContext({ elements: [] });
  runScript(navigationRuntimeMinifiedSource, minifiedEnv.context, "navigation-runtime.min.js");
  const minifiedKeys = Object.keys(minifiedEnv.context.__gosx.navigation).sort();

  // Minification renames local identifiers (function and variable names
  // inside the IIFE), but it must never rename an object literal's own
  // property keys — those are the public command API gosx#221 promises stays
  // identical. A drift here would mean a minifier pass mangled properties
  // (mangleProps), which this build must never enable.
  assert.deepEqual(minifiedKeys, sourceKeys);
});
