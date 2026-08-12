"use strict";
// The navigation runtime: page lifecycle hooks, head and body swapping,
// engine reuse, the public command API, link marking, prefetch and forms.
//
// Split out of the former client/js/runtime.test.js, which this set replaces
// and which no longer exists. Every shared fake, sandbox builder and fixture
// factory lives in ./runtime-test-harness.js.

const test = require("node:test");
const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");

const {
  bootstrapSource,
  bootstrapRuntimeSource,
  bootstrapFeatureEnginesSource,
  bootstrapFeatureScene3DSource,
  bootstrapFeatureScene3DCommandSource,
  bootstrapScene3DMountSourceFile,
  stripeBridgeSource,
  navigationSource,
  scene3DCommandFetchRoutes,
  FakeElement,
  FakeFormData,
  createContext,
  runScript,
  flushAsyncWork,
  appendManagedHead,
  buildNavigatedDocument,
  installManualClock,
  installManualTimers,
} = require("./runtime-test-harness.js");

const hubConnectionsSource = [
  fs.readFileSync(path.join(__dirname, "..", "runtime", "host", "compatibility.ts"), "utf8"),
  fs.readFileSync(path.join(__dirname, "..", "runtime", "host", "hubs.ts"), "utf8"),
].join("\n");

test("bootstrap preserves navigation and request installed before it", async () => {
  const env = createContext({ elements: [] });
  runScript(navigationSource, env.context, "navigation_runtime.js");
  const namespace = env.context.__gosx;
  const navigation = env.context.__gosx.navigation;
  const request = function() { return Promise.resolve({ ok: true }); };
  const relay = { send() {} };
  namespace.request = request;
  namespace.relay = relay;

  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();

  assert.equal(env.context.__gosx, namespace);
  assert.equal(env.context.__gosx.navigation, navigation);
  assert.equal(env.context.__gosx.request, request);
  assert.equal(env.context.__gosx.relay, relay);
  assert.equal(env.context.__gosx_page_nav, navigation);
  assert.equal(env.context.__gosx.islands instanceof Map, true);
});

test("bootstrap restores legacy page navigation into the preserved namespace", async () => {
  const env = createContext({ elements: [] });
  const navigation = { navigate() {}, revalidate() {} };
  const namespace = { request() {}, relay: {} };
  env.context.__gosx = namespace;
  env.context.__gosx_page_nav = navigation;

  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();

  assert.equal(env.context.__gosx, namespace);
  assert.equal(namespace.navigation, navigation);
});

test("development bootstrap stub preserves external namespace services and resets owned state", () => {
  const source = fs.readFileSync(
    path.join(__dirname, "..", "..", "server", "runtime_assets.go"),
    "utf8",
  );
  const match = source.match(/const bootstrapStub = `([\s\S]*?)`\n/);
  assert.ok(match, "bootstrapStub source must remain extractable");

  const env = createContext({ elements: [] });
  runScript(navigationSource, env.context, "navigation_runtime.js");
  const namespace = env.context.__gosx;
  const navigation = namespace.navigation;
  const request = function() { return Promise.resolve({ ok: true }); };
  const relay = { configure() {} };
  const staleIslands = new Map([["stale", {}]]);
  namespace.request = request;
  namespace.relay = relay;
  namespace.islands = staleIslands;
  namespace.ready = true;

  runScript(match[1], env.context, "bootstrap-stub.js");

  assert.equal(env.context.__gosx, namespace);
  assert.equal(namespace.navigation, navigation);
  assert.equal(namespace.request, request);
  assert.equal(namespace.relay, relay);
  assert.notEqual(namespace.islands, staleIslands);
  assert.equal(namespace.islands.size, 0);
  assert.equal(namespace.computeIslands.size, 0);
  assert.equal(namespace.ready, true);
});

test("bootstrap exposes page lifecycle hooks and can re-bootstrap after disposal", async () => {
  const wrapper = new FakeElement("div", null);
  const componentRoot = new FakeElement("div", null);

  wrapper.id = "gosx-island-2";
  componentRoot.appendChild(new FakeElement("span", null));
  wrapper.appendChild(componentRoot);

  const env = createContext({
    elements: [wrapper],
    fetchRoutes: {
      "/runtime.wasm": { bytes: [0, 97, 115, 109] },
      "/counter.json": { text: '{"name":"Counter"}' },
    },
    manifest: {
      runtime: { path: "/runtime.wasm" },
      islands: [
        {
          id: "gosx-island-2",
          component: "Counter",
          props: {},
          programRef: "/counter.json",
        },
      ],
    },
  });

  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();

  assert.equal(typeof env.context.__gosx_bootstrap_page, "function");
  assert.equal(typeof env.context.__gosx_dispose_page, "function");
  assert.equal(env.context.__gosx.islands.size, 1);

  await env.context.__gosx_dispose_page();
  assert.equal(env.context.__gosx.islands.size, 0);

  await env.context.__gosx_bootstrap_page();
  await flushAsyncWork();
  assert.equal(env.hydrateCalls.length, 2);
  assert.equal(env.context.__gosx.islands.size, 1);
});

test("navigation runtime swaps managed head/body and calls page lifecycle hooks", async () => {
  const oldMeta = new FakeElement("meta", null);
  oldMeta.setAttribute("name", "description");
  oldMeta.setAttribute("content", "old");

  const link = new FakeElement("a", null);
  link.setAttribute("href", "/docs");
  link.setAttribute("data-gosx-link", "");
  link.textContent = "Docs";

  const oldBody = new FakeElement("div", null);
  oldBody.id = "old-page";
  oldBody.textContent = "old-page";

  const disposeCalls = [];
  const bootstrapCalls = [];
  const parsedDocs = new Map();

  const env = createContext({
    elements: [link, oldBody],
    fetchRoutes: {
      "http://localhost:3000/docs": {
        text: "__PAGE_DOC__",
        url: "http://localhost:3000/docs",
      },
    },
    parseHTML(html) {
      return parsedDocs.get(html);
    },
  });

  env.document.title = "Old";
  appendManagedHead(env.document, [oldMeta]);
  env.context.__gosx_dispose_page = async function() {
    disposeCalls.push("dispose");
  };
  env.context.__gosx_bootstrap_page = async function() {
    bootstrapCalls.push("bootstrap");
  };

  const nextMeta = new FakeElement("meta", null);
  nextMeta.setAttribute("name", "description");
  nextMeta.setAttribute("content", "new");
  const nextBody = new FakeElement("main", null);
  nextBody.id = "new-page";
  nextBody.textContent = "new-page";

  parsedDocs.set("__PAGE_DOC__", buildNavigatedDocument({
    title: "Docs",
    headNodes: [nextMeta],
    bodyNodes: [nextBody],
  }));

  runScript(navigationSource, env.context, "navigation_runtime.js");
  assert.equal(env.context.__gosx.navigation, env.context.__gosx_page_nav);
  assert.equal(typeof env.context.__gosx.navigation.navigate, "function");
  const clickListener = env.document.eventListeners.get("click")[0];
  let prevented = false;
  clickListener({
    type: "click",
    button: 0,
    target: link,
    defaultPrevented: false,
    metaKey: false,
    ctrlKey: false,
    shiftKey: false,
    altKey: false,
    preventDefault() {
      prevented = true;
      this.defaultPrevented = true;
    },
  });
  await flushAsyncWork();

  assert.equal(prevented, true);
  assert.deepEqual(disposeCalls, ["dispose"]);
  assert.deepEqual(bootstrapCalls, ["bootstrap"]);
  assert.equal(env.document.title, "Docs");
  assert.equal(env.context.location.href, "http://localhost:3000/docs");
  assert.equal(env.document.getElementById("new-page").textContent, "new-page");
  assert.equal(env.document.head.childNodes[1].getAttribute("content"), "new");
  assert.equal(env.fetchCalls[0].init.headers["X-GoSX-Navigation"], "1");
  assert.equal(env.document.dispatchedEvents.at(-1).type, "gosx:navigate");
  assert.equal(env.document.activeElement, env.document.getElementById("new-page"));
  assert.equal(env.document.activeElement.getAttribute("tabindex"), "-1");
  assert.equal(env.document.dispatchedEvents.at(-1).detail.focusTargetId, "new-page");
  assert.equal(env.document.body.childNodes.at(-1).textContent, "Docs");
  assert.equal(env.scrollCalls.length, 1);
  assert.equal(env.scrollCalls[0].length, 1);
  assert.equal(env.scrollCalls[0][0].top, 0);
  assert.equal(env.scrollCalls[0][0].left, 0);
  assert.equal(env.scrollCalls[0][0].behavior, "instant");
});

test("navigation revalidate invalidates cache and reconciles the current page without moving history or scroll", async () => {
  const url = "http://localhost:3000/agenda?day=one";
  const oldMain = new FakeElement("main", null);
  oldMain.id = "old-agenda";
  oldMain.textContent = "old";
  const oldMeta = new FakeElement("meta", null);
  oldMeta.setAttribute("name", "description");
  oldMeta.setAttribute("content", "old agenda");
  const parsedDocs = new Map();
  const env = createContext({
    elements: [oldMain],
    fetchRoutes: {
      [url]: { text: "__FRESH_AGENDA__", url },
    },
    parseHTML(html) {
      return parsedDocs.get(html);
    },
  });
  env.context.location.href = url;
  appendManagedHead(env.document, [oldMeta]);
  env.context.__gosx_dispose_page = async function() {};
  env.context.__gosx_bootstrap_page = async function() {};

  const freshMeta = new FakeElement("meta", null);
  freshMeta.setAttribute("name", "description");
  freshMeta.setAttribute("content", "fresh agenda");
  const freshMain = new FakeElement("main", null);
  freshMain.id = "fresh-agenda";
  freshMain.textContent = "fresh";
  parsedDocs.set("__FRESH_AGENDA__", buildNavigatedDocument({
    title: "Fresh agenda",
    headNodes: [freshMeta],
    bodyNodes: [freshMain],
  }));

  const stale = Promise.resolve({ html: "__STALE_AGENDA__", url });
  stale.__gosxCachedAt = Date.now();
  env.context.__gosx_page_cache = new Map([[url, stale]]);
  const historyCalls = [];
  env.context.history = {
    pushState(_state, _title, nextURL) {
      historyCalls.push(["push", String(nextURL)]);
      env.context.location.href = String(nextURL);
    },
    replaceState(_state, _title, nextURL) {
      historyCalls.push(["replace", String(nextURL)]);
      env.context.location.href = String(nextURL);
    },
  };

  runScript(navigationSource, env.context, "navigation_runtime.js");
  assert.equal(typeof env.context.__gosx.navigation.refresh, "function");
  assert.equal(typeof env.context.__gosx.navigation.refreshState, "function");
  assert.equal(typeof env.context.__gosx.navigation.revalidate, "function");
  assert.equal(typeof env.context.__gosx.navigation.getFetchEpoch, "function");
  const initialEpoch = env.context.__gosx.navigation.getFetchEpoch();
  assert.equal(initialEpoch.started, 0);
  assert.equal(initialEpoch.applied, 0);
  const refreshedState = env.context.__gosx.navigation.refresh();
  assert.equal(refreshedState.currentURL, url);
  assert.equal(typeof refreshedState.then, "undefined");
  assert.equal(env.fetchCalls.length, 0);
  assert.equal(env.context.__gosx.navigation.getFetchEpoch().started, initialEpoch.started);
  assert.equal(env.context.__gosx.navigation.getFetchEpoch().applied, initialEpoch.applied);
  assert.equal(env.context.__gosx.navigation.refreshState().currentURL, url);
  assert.equal(await env.context.__gosx.navigation.revalidate(), true);
  await flushAsyncWork();

  assert.equal(env.fetchCalls.length, 1);
  assert.equal(env.context.__gosx.navigation.getFetchEpoch().started, initialEpoch.started + 1);
  assert.equal(env.context.__gosx.navigation.getFetchEpoch().applied, initialEpoch.applied + 1);
  assert.equal(env.fetchCalls[0].url, url);
  assert.notEqual(env.context.__gosx_page_cache.get(url), stale);
  assert.equal(env.document.title, "Fresh agenda");
  assert.equal(env.document.getElementById("fresh-agenda").textContent, "fresh");
  assert.equal(env.document.head.childNodes[1].getAttribute("content"), "fresh agenda");
  assert.deepEqual(historyCalls, [["replace", url]]);
  assert.equal(env.context.location.href, url);
  assert.deepEqual(env.scrollCalls, []);
  assert.equal(env.document.activeElement, env.document.getElementById("fresh-agenda"));
});

test("programmatic navigation soft-fetches only same-origin HTTP URLs", async () => {
  const safeURL = "http://localhost:3000/safe";
  const redirectedURL = "http://localhost:3000/redirected-off-origin";
  const parsedDocs = new Map();
  const safeMain = new FakeElement("main", null);
  safeMain.id = "safe-page";
  parsedDocs.set("__SAFE_PAGE__", buildNavigatedDocument({
    title: "Safe",
    bodyNodes: [safeMain],
  }));
  const env = createContext({
    elements: [],
    fetchRoutes: {
      [safeURL]: { text: "__SAFE_PAGE__", url: safeURL },
      [redirectedURL]: { text: "__ATTACKER_PAGE__", url: "https://attacker.example/page" },
    },
    parseHTML(html) { return parsedDocs.get(html); },
  });
  const assigned = [];
  const replaced = [];
  env.context.location.assign = (url) => assigned.push(String(url));
  env.context.location.replace = (url) => replaced.push(String(url));
  env.context.__gosx_dispose_page = async function() {};
  env.context.__gosx_bootstrap_page = async function() {};
  runScript(navigationSource, env.context, "navigation_runtime.js");

  assert.equal(await env.context.__gosx.navigation.navigate(safeURL), true);
  assert.equal(env.fetchCalls.length, 1);
  assert.equal(env.document.getElementById("safe-page").id, "safe-page");

  await assert.rejects(
    env.context.__gosx.navigation.navigate(redirectedURL),
    /blocked cross-origin navigation response/,
  );
  assert.equal(env.fetchCalls.length, 2);
  assert.equal(env.document.getElementById("safe-page").id, "safe-page");

  assert.equal(await env.context.__gosx.navigation.navigate("https://elsewhere.example/path"), true);
  assert.deepEqual(assigned, ["https://elsewhere.example/path"]);
  assert.equal(env.fetchCalls.length, 2);
  assert.equal(await env.context.__gosx.navigation.navigate("https://elsewhere.example/replaced", { replace: true }), true);
  assert.deepEqual(replaced, ["https://elsewhere.example/replaced"]);
  assert.equal(env.fetchCalls.length, 2);

  const href = env.context.location.href;
  for (const unsafe of [
    "javascript:alert(1)",
    "data:text/html,attacker",
    "vbscript:msgbox(1)",
    "blob:http://localhost:3000/attacker",
    "http://[",
  ]) {
    await assert.rejects(
      env.context.__gosx.navigation.navigate(unsafe),
      /blocked unsafe navigation URL/,
      unsafe,
    );
  }
  assert.equal(env.context.location.href, href);
  assert.deepEqual(assigned, ["https://elsewhere.example/path"]);
  assert.deepEqual(replaced, ["https://elsewhere.example/replaced"]);
  assert.equal(env.fetchCalls.length, 2);
});

test("state-only refresh with a hash stays synchronous and does not advance the fetch epoch", () => {
  const url = "http://localhost:3000/agenda#details";
  const env = createContext({});
  env.context.location.href = url;

  runScript(navigationSource, env.context, "navigation_runtime.js");
  const initialEpoch = env.context.__gosx.navigation.getFetchEpoch();
  const snapshot = env.context.__gosx_page_nav.refresh();

  assert.equal(snapshot.currentURL, url);
  assert.equal(typeof snapshot.then, "undefined");
  assert.equal(env.context.__gosx.navigation.getFetchEpoch().started, initialEpoch.started);
  assert.equal(env.context.__gosx.navigation.getFetchEpoch().applied, initialEpoch.applied);
  assert.deepEqual(env.fetchCalls, []);
});

test("navigation revalidate rejects cleanly so callers can use the documented hard-load fallback", async () => {
  const url = "http://localhost:3000/agenda";
  const oldMain = new FakeElement("main", null);
  oldMain.id = "current-agenda";
  oldMain.textContent = "still current";
  const env = createContext({
    elements: [oldMain],
    fetchRoutes: {
      [url]: { ok: false, status: 503, text: "unavailable", url },
    },
  });
  env.context.location.href = url;
  let reloads = 0;
  env.context.location.reload = function() { reloads += 1; };

  runScript(navigationSource, env.context, "navigation_runtime.js");
  await assert.rejects(
    env.context.__gosx.navigation.revalidate().catch(function(error) {
      env.context.location.reload();
      throw error;
    }),
    /navigation fetch failed with status 503/,
  );

  assert.equal(reloads, 1);
  assert.equal(env.context.location.href, url);
  assert.equal(env.document.getElementById("current-agenda").textContent, "still current");
  assert.equal(env.context.__gosx.navigation.getState().phase, "idle");
  assert.equal(env.context.__gosx.navigation.getFetchEpoch().started, 1);
  assert.equal(env.context.__gosx.navigation.getFetchEpoch().applied, 0);
});

test("navigation fetch epoch stays uncommitted while pending and commits after page apply", async () => {
  const url = "http://localhost:3000/pending";
  let resolveFetch;
  const parsedDocs = new Map();
  const env = createContext({
    fetchRoutes: {
      [url]: () => new Promise((resolve) => { resolveFetch = resolve; }),
    },
    parseHTML(html) { return parsedDocs.get(html); },
  });
  env.context.location.href = url;
  env.context.__gosx_dispose_page = async function() {};
  env.context.__gosx_bootstrap_page = async function() {};
  const main = new FakeElement("main", null);
  main.id = "pending-applied";
  parsedDocs.set("__PENDING_PAGE__", buildNavigatedDocument({
    title: "Pending",
    bodyNodes: [main],
  }));

  runScript(navigationSource, env.context, "navigation_runtime.js");
  const revalidation = env.context.__gosx.navigation.revalidate();
  await Promise.resolve();
  assert.equal(env.context.__gosx.navigation.getFetchEpoch().started, 1);
  assert.equal(env.context.__gosx.navigation.getFetchEpoch().applied, 0);

  resolveFetch({ text: "__PENDING_PAGE__", url });
  assert.equal(await revalidation, true);
  assert.equal(env.context.__gosx.navigation.getFetchEpoch().started, 1);
  assert.equal(env.context.__gosx.navigation.getFetchEpoch().applied, 1);
});

test("navigation fetch epoch does not commit when page application fails", async () => {
  const url = "http://localhost:3000/apply-failure";
  const parsedDocs = new Map();
  const env = createContext({
    fetchRoutes: {
      [url]: { text: "__APPLY_FAILURE_PAGE__", url },
    },
    parseHTML(html) { return parsedDocs.get(html); },
  });
  env.context.location.href = url;
  env.context.__gosx_dispose_page = async function() {
    throw new Error("dispose failed before apply");
  };
  parsedDocs.set("__APPLY_FAILURE_PAGE__", buildNavigatedDocument({
    title: "Apply failure",
    bodyNodes: [new FakeElement("main", null)],
  }));

  runScript(navigationSource, env.context, "navigation_runtime.js");
  await assert.rejects(env.context.__gosx.navigation.revalidate(), /dispose failed before apply/);
  assert.equal(env.context.__gosx.navigation.getFetchEpoch().started, 1);
  assert.equal(env.context.__gosx.navigation.getFetchEpoch().applied, 0);
  env.context.__gosx_dispose_page = async function() {};
});

test("navigation runtime sends typed beacons once per soft navigation path", async () => {
  function beaconScript() {
    const script = new FakeElement("script", null);
    script.setAttribute("type", "application/json");
    script.setAttribute("data-gosx-navigation-beacon", "");
    script.textContent = JSON.stringify({
      name: "first-party-pageview",
      url: "/__internal/attribution/pageview",
      method: "POST",
      credentials: "same-origin",
      keepalive: true,
      pathField: "path",
      navigationIDField: "navigation_id",
    });
    return script;
  }

  const parsedDocs = new Map();
  const link = new FakeElement("a", null);
  link.setAttribute("href", "/docs?tab=runtime");
  link.setAttribute("data-gosx-link", "");
  link.textContent = "Docs";

  const env = createContext({
    elements: [link],
    fetchRoutes: {
      "http://localhost:3000/docs?tab=runtime": {
        text: "__DOCS_PAGE__",
        url: "http://localhost:3000/docs?tab=runtime",
      },
      "/__internal/attribution/pageview": { status: 204, text: "" },
    },
    parseHTML(html) {
      return parsedDocs.get(html);
    },
  });
  appendManagedHead(env.document, [beaconScript()]);

  const samePageLink = new FakeElement("a", null);
  samePageLink.setAttribute("href", "/docs?tab=runtime");
  samePageLink.setAttribute("data-gosx-link", "");
  samePageLink.textContent = "Docs current";

  const nextMain = new FakeElement("main", null);
  nextMain.id = "docs-page";
  nextMain.appendChild(samePageLink);
  parsedDocs.set("__DOCS_PAGE__", buildNavigatedDocument({
    title: "Docs",
    headNodes: [beaconScript()],
    bodyNodes: [nextMain],
  }));

  runScript(navigationSource, env.context, "navigation_runtime.js");
  await env.context.__gosx_page_nav.navigate("http://localhost:3000/docs?tab=runtime");
  await flushAsyncWork();

  const beaconCalls = () => env.fetchCalls.filter((call) => call.url === "/__internal/attribution/pageview");
  assert.equal(beaconCalls().length, 1);
  const first = beaconCalls()[0];
  assert.equal(first.init.method, "POST");
  assert.equal(first.init.credentials, "same-origin");
  assert.equal(first.init.keepalive, true);
  const payload = JSON.parse(first.init.body);
  assert.deepEqual(payload, {
    path: "/docs?tab=runtime",
    navigation_id: payload.navigation_id,
  });
  assert.match(payload.navigation_id, /^[0-9a-f-]{36}$/);

  const clickListener = env.document.eventListeners.get("click")[0];
  clickListener({
    type: "click",
    button: 0,
    target: samePageLink,
    defaultPrevented: false,
    metaKey: false,
    ctrlKey: false,
    shiftKey: false,
    altKey: false,
    preventDefault() {
      this.defaultPrevented = true;
    },
  });
  await flushAsyncWork();

  assert.equal(beaconCalls().length, 1);
});

test("navigation runtime aborts stale fetches and lets the newest navigation win", async () => {
  class TestAbortSignal {
    constructor() {
      this.aborted = false;
      this.listeners = [];
    }

    addEventListener(type, listener) {
      if (type === "abort") this.listeners.push(listener);
    }
  }

  class TestAbortController {
    constructor() {
      this.signal = new TestAbortSignal();
    }

    abort() {
      if (this.signal.aborted) return;
      this.signal.aborted = true;
      for (const listener of this.signal.listeners) listener();
    }
  }

  const slowDoc = buildNavigatedDocument({
    title: "Slow",
    bodyNodes: [new FakeElement("main", null)],
  });
  slowDoc.body.firstChild.id = "slow-page";
  slowDoc.body.firstChild.textContent = "slow";
  const fastDoc = buildNavigatedDocument({
    title: "Fast",
    bodyNodes: [new FakeElement("main", null)],
  });
  fastDoc.body.firstChild.id = "fast-page";
  fastDoc.body.firstChild.textContent = "fast";

  const parsedDocs = new Map([
    ["__SLOW_PAGE__", slowDoc],
    ["__FAST_PAGE__", fastDoc],
  ]);
  const env = createContext({
    elements: [],
    fetchRoutes: {
      "http://localhost:3000/slow": (url, init) => new Promise((resolve, reject) => {
        const timer = setTimeout(() => resolve({ text: "__SLOW_PAGE__", url }), 30);
        init.signal?.addEventListener("abort", () => {
          clearTimeout(timer);
          const error = new Error("navigation aborted");
          error.name = "AbortError";
          reject(error);
        });
      }),
      "http://localhost:3000/fast": { text: "__FAST_PAGE__", url: "http://localhost:3000/fast" },
    },
    parseHTML(html) {
      return parsedDocs.get(html);
    },
  });
  env.context.AbortController = TestAbortController;

  runScript(navigationSource, env.context, "navigation_runtime.js");
  const slow = env.context.__gosx_page_nav.navigate("http://localhost:3000/slow");
  const fast = env.context.__gosx_page_nav.navigate("http://localhost:3000/fast");
  const results = await Promise.all([slow, fast]);

  assert.deepEqual(results, [false, true]);
  assert.equal(env.context.location.href, "http://localhost:3000/fast");
  assert.equal(env.document.getElementById("fast-page").textContent, "fast");
  assert.equal(env.document.getElementById("slow-page"), null);
  assert.equal(env.context.__gosx.navigation.getFetchEpoch().started, 2);
  assert.equal(env.context.__gosx.navigation.getFetchEpoch().applied, 2);
});

test("same-target pending navigations share one fetch and let the newest caller apply", async () => {
  class TestAbortSignal {
    constructor() {
      this.aborted = false;
      this.listeners = [];
    }

    addEventListener(type, listener) {
      if (type === "abort") this.listeners.push(listener);
    }
  }

  class TestAbortController {
    constructor() {
      this.signal = new TestAbortSignal();
    }

    abort() {
      if (this.signal.aborted) return;
      this.signal.aborted = true;
      for (const listener of this.signal.listeners) listener();
    }
  }

  for (const mode of ["ordinary", "revalidate"]) {
    const url = "http://localhost:3000/coalesced-" + mode;
    let resolveFetch;
    const parsedDocs = new Map();
    const main = new FakeElement("main", null);
    main.id = "coalesced-page-" + mode;
    main.textContent = mode;
    parsedDocs.set("__COALESCED_PAGE__", buildNavigatedDocument({
      title: "Coalesced " + mode,
      bodyNodes: [main],
    }));
    const env = createContext({
      fetchRoutes: {
        [url]: (_url, init) => new Promise((resolve, reject) => {
          resolveFetch = resolve;
          init.signal.addEventListener("abort", () => {
            const error = new Error("navigation aborted");
            error.name = "AbortError";
            reject(error);
          });
        }),
      },
      parseHTML(html) { return parsedDocs.get(html); },
    });
    env.context.AbortController = TestAbortController;
    if (mode === "revalidate") env.context.location.href = url;
    env.context.__gosx_dispose_page = async function() {};
    env.context.__gosx_bootstrap_page = async function() {};

    runScript(navigationSource, env.context, "navigation_runtime.js");
    const first = mode === "revalidate"
      ? env.context.__gosx.navigation.revalidate()
      : env.context.__gosx.navigation.navigate(url);
    const newest = mode === "revalidate"
      ? env.context.__gosx.navigation.revalidate()
      : env.context.__gosx.navigation.navigate(url);
    await Promise.resolve();

    assert.equal(env.fetchCalls.length, 1, mode);
    assert.equal(env.context.__gosx.navigation.getState().phase, "pending", mode);
    resolveFetch({ text: "__COALESCED_PAGE__", url });
    assert.deepEqual(await Promise.all([first, newest]), [false, true], mode);
    assert.equal(env.document.getElementById("coalesced-page-" + mode).textContent, mode);
    assert.equal(env.context.__gosx.navigation.getState().phase, "idle", mode);
    const epoch = env.context.__gosx.navigation.getFetchEpoch();
    assert.equal(epoch.started, 1, mode);
    assert.equal(epoch.applied, 1, mode);
  }
});

test("stale same-URL requests cannot evict a newer cached request", async () => {
  for (const staleOutcome of ["reject", "no-store"]) {
    const url = "http://localhost:3000/agenda-race-" + staleOutcome;
    const middleURL = "http://localhost:3000/race-middle-" + staleOutcome;
    const link = new FakeElement("a", null);
    link.setAttribute("href", url);
    link.setAttribute("data-gosx-link", "");
    let settleStale;
    let resolveFresh;
    let routeCalls = 0;
    const parsedDocs = new Map();
    const env = createContext({
      elements: [link],
      fetchRoutes: {
        [url]: () => {
          routeCalls += 1;
          if (routeCalls === 1) {
            return new Promise((resolve, reject) => {
              settleStale = staleOutcome === "reject"
                ? () => reject(new Error("stale request failed"))
                : () => resolve({ text: "__STALE_NO_STORE__", url });
            });
          }
          return new Promise((resolve) => { resolveFresh = resolve; });
        },
        [middleURL]: { text: "__RACE_MIDDLE__", url: middleURL },
      },
      parseHTML(html) { return parsedDocs.get(html); },
    });
    env.context.__gosx_dispose_page = async function() {};
    env.context.__gosx_bootstrap_page = async function() {};
    const noStoreMeta = new FakeElement("meta", null);
    noStoreMeta.setAttribute("name", "gosx-page-cache");
    noStoreMeta.setAttribute("content", "no-store");
    parsedDocs.set("__STALE_NO_STORE__", buildNavigatedDocument({
      title: "Stale",
      headNodes: [noStoreMeta],
      bodyNodes: [new FakeElement("main", null)],
    }));
    parsedDocs.set("__RACE_MIDDLE__", buildNavigatedDocument({
      title: "Middle",
      bodyNodes: [new FakeElement("main", null)],
    }));
    const freshMain = new FakeElement("main", null);
    freshMain.id = "fresh-race-" + staleOutcome;
    freshMain.textContent = "fresh " + staleOutcome;
    parsedDocs.set("__FRESH_RACE__", buildNavigatedDocument({
      title: "Fresh",
      bodyNodes: [freshMain],
    }));

    runScript(navigationSource, env.context, "navigation_runtime.js");
    const stale = env.context.__gosx.navigation.navigate(url, { force: true, revalidate: true });
    assert.equal(await env.context.__gosx.navigation.navigate(middleURL), true, staleOutcome);
    const fresh = env.context.__gosx.navigation.navigate(url, { force: true, revalidate: true });
    await Promise.resolve();
    assert.equal(routeCalls, 2, staleOutcome);

    settleStale();
    const staleResult = await stale;
    assert.equal(staleResult, false, staleOutcome);
    env.document.eventListeners.get("mouseover")[0]({ type: "mouseover", target: link });
    await Promise.resolve();
    assert.equal(routeCalls, 2, staleOutcome + " must retain request B");

    resolveFresh({ text: "__FRESH_RACE__", url });
    assert.equal(await fresh, true, staleOutcome);
    assert.equal(env.context.__gosx_page_cache.has(url), true, staleOutcome);
    assert.equal(
      env.document.getElementById("fresh-race-" + staleOutcome).textContent,
      "fresh " + staleOutcome,
    );
  }
});

test("navigation runtime reports failures through the shared diagnostics policy", async () => {
  const env = createContext({
    elements: [],
    fetchRoutes: {
      "http://localhost:3000/broken": {
        ok: false,
        status: 503,
        text: "unavailable",
        url: "http://localhost:3000/broken",
      },
    },
  });

  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();
  runScript(navigationSource, env.context, "navigation_runtime.js");

  await assert.rejects(
    env.context.__gosx_page_nav.navigate("http://localhost:3000/broken"),
    /navigation fetch failed with status 503/
  );
  const issue = env.context.__gosx.listIssues().find((entry) => entry.scope === "navigation");
  assert.ok(issue);
  assert.equal(issue.type, "navigation");
  assert.equal(issue.severity, "warning");
  assert.equal(issue.phase, "navigation");
  assert.equal(issue.source, "http://localhost:3000/broken");
});

test("navigation runtime loads patch, lifecycle, and managed scripts before page bootstrap", async () => {
  const parsedDocs = new Map();
  const link = new FakeElement("a", null);
  link.setAttribute("href", "/docs/runtime");
  link.setAttribute("data-gosx-link", "");
  link.textContent = "Runtime";

  const env = createContext({
    elements: [link],
    fetchRoutes: {
      "http://localhost:3000/docs/runtime": {
        text: "__SCRIPT_DOC__",
        url: "http://localhost:3000/docs/runtime",
      },
      "http://localhost:3000/patch.js": {
        text: "window.__scriptOrder.push('patch');",
        url: "http://localhost:3000/patch.js",
      },
      "http://localhost:3000/lifecycle.js": {
        text: "window.__scriptOrder.push('lifecycle');",
        url: "http://localhost:3000/lifecycle.js",
      },
      "http://localhost:3000/managed.js": {
        text: "window.__scriptOrder.push('managed');",
        url: "http://localhost:3000/managed.js",
      },
    },
    parseHTML(html) {
      return parsedDocs.get(html);
    },
  });

  env.context.__scriptOrder = [];

  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();

  const originalBootstrap = env.context.__gosx_bootstrap_page;
  env.context.__gosx_bootstrap_page = async function() {
    env.context.__scriptOrder.push("bootstrap");
    return originalBootstrap();
  };
  env.context.__gosx_dispose_page = async function() {
    env.context.__scriptOrder.push("dispose");
  };

  const patchScript = new FakeElement("script", null);
  patchScript.setAttribute("data-gosx-script", "patch");
  patchScript.setAttribute("src", "/patch.js");

  const lifecycleScript = new FakeElement("script", null);
  lifecycleScript.setAttribute("data-gosx-script", "lifecycle");
  lifecycleScript.setAttribute("src", "/lifecycle.js");

  const managedScript = new FakeElement("script", null);
  managedScript.id = "managed-script";
  managedScript.setAttribute("data-gosx-script", "managed");
  managedScript.setAttribute("src", "/managed.js");

  const nextBody = new FakeElement("main", null);
  nextBody.id = "runtime-page";
  nextBody.textContent = "Runtime page";

  parsedDocs.set("__SCRIPT_DOC__", buildNavigatedDocument({
    title: "Runtime",
    headNodes: [patchScript, lifecycleScript],
    bodyNodes: [nextBody, managedScript],
  }));

  runScript(navigationSource, env.context, "navigation_runtime.js");
  await env.context.__gosx_page_nav.navigate("http://localhost:3000/docs/runtime");
  await flushAsyncWork();

  assert.deepEqual(env.context.__scriptOrder, [
    "dispose",
    "patch",
    "lifecycle",
    "managed",
    "bootstrap",
  ]);
  assert.equal(env.document.getElementById("runtime-page").textContent, "Runtime page");
  assert.equal(env.document.getElementById("managed-script"), null);
  assert.deepEqual(
    Array.from(env.context.__gosx.document.get().assets.scripts, (entry) => entry.role),
    ["patch", "lifecycle"],
  );
});

test("navigation runtime replays only opted-in inline scripts after page bootstrap", async () => {
  const parsedDocs = new Map();
  const env = createContext({
    fetchRoutes: {
      "http://localhost:3000/inline-replay": {
        text: "__INLINE_REPLAY_DOC__",
        url: "http://localhost:3000/inline-replay",
      },
    },
    parseHTML(html) {
      return parsedDocs.get(html);
    },
  });

  env.context.__scriptOrder = [];
  env.context.__bootstrapDone = false;
  env.document.inlineScriptLoader = function(script) {
    runScript(script.textContent || "", env.context, "inline-navigation-replay.js");
  };

  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();

  const originalBootstrap = env.context.__gosx_bootstrap_page;
  env.context.__gosx_bootstrap_page = async function(reuseIDs) {
    env.context.__scriptOrder.push("bootstrap");
    await originalBootstrap(reuseIDs);
    env.context.__bootstrapDone = true;
  };
  env.context.__gosx_dispose_page = async function() {
    env.context.__scriptOrder.push("dispose");
  };

  const page = new FakeElement("main", null);
  page.id = "inline-replay-page";
  page.textContent = "Inline replay";

  const unmarkedScript = new FakeElement("script", null);
  unmarkedScript.textContent = "window.__scriptOrder.push('unmarked');";

  const jsonScript = new FakeElement("script", null);
  jsonScript.setAttribute("type", "application/json");
  jsonScript.setAttribute("data-gosx-navigation-replay", "true");
  jsonScript.textContent = "{\"ignored\":true}";

  const replayScript = new FakeElement("script", null);
  replayScript.id = "replay-script";
  replayScript.setAttribute("data-gosx-navigation-replay", "true");
  replayScript.textContent = [
    "window.__inlineReplayCount = (window.__inlineReplayCount || 0) + 1;",
    "window.__scriptOrder.push(window.__bootstrapDone ? 'replay-after-bootstrap' : 'replay-before-bootstrap');",
  ].join("\n");

  parsedDocs.set("__INLINE_REPLAY_DOC__", buildNavigatedDocument({
    title: "Inline Replay",
    bodyNodes: [page, unmarkedScript, jsonScript, replayScript],
  }));

  runScript(navigationSource, env.context, "navigation_runtime.js");
  await env.context.__gosx_page_nav.navigate("http://localhost:3000/inline-replay");
  await flushAsyncWork();

  assert.deepEqual(env.context.__scriptOrder, ["dispose", "bootstrap", "replay-after-bootstrap"]);
  assert.equal(env.context.__inlineReplayCount, 1);
  assert.equal(env.document.getElementById("inline-replay-page").textContent, "Inline replay");
  assert.equal(env.document.getElementById("replay-script").getAttribute("data-gosx-navigation-replayed"), "true");
});

test("navigation runtime replays pre-bootstrap inline scripts before page bootstrap", async () => {
  const parsedDocs = new Map();
  const env = createContext({
    fetchRoutes: {
      "http://localhost:3000/inline-replay-pre": {
        text: "__INLINE_REPLAY_PRE_DOC__",
        url: "http://localhost:3000/inline-replay-pre",
      },
    },
    parseHTML(html) {
      return parsedDocs.get(html);
    },
  });

  env.context.__scriptOrder = [];
  env.context.__bootstrapDone = false;
  env.document.inlineScriptLoader = function(script) {
    runScript(script.textContent || "", env.context, "inline-navigation-replay.js");
  };

  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();

  const originalBootstrap = env.context.__gosx_bootstrap_page;
  env.context.__gosx_bootstrap_page = async function(reuseIDs) {
    env.context.__scriptOrder.push("bootstrap");
    await originalBootstrap(reuseIDs);
    env.context.__bootstrapDone = true;
  };
  env.context.__gosx_dispose_page = async function() {
    env.context.__scriptOrder.push("dispose");
  };

  const page = new FakeElement("main", null);
  page.id = "inline-replay-pre-page";
  page.textContent = "Inline replay pre";

  const preScript = new FakeElement("script", null);
  preScript.id = "replay-pre-script";
  preScript.setAttribute("data-gosx-navigation-replay", "pre-bootstrap");
  preScript.textContent =
    "window.__scriptOrder.push(window.__bootstrapDone ? 'pre-after-bootstrap' : 'pre-before-bootstrap');";

  const postScript = new FakeElement("script", null);
  postScript.id = "replay-post-script";
  postScript.setAttribute("data-gosx-navigation-replay", "true");
  postScript.textContent =
    "window.__scriptOrder.push(window.__bootstrapDone ? 'post-after-bootstrap' : 'post-before-bootstrap');";

  parsedDocs.set("__INLINE_REPLAY_PRE_DOC__", buildNavigatedDocument({
    title: "Inline Replay Pre",
    bodyNodes: [page, preScript, postScript],
  }));

  runScript(navigationSource, env.context, "navigation_runtime.js");
  await env.context.__gosx_page_nav.navigate("http://localhost:3000/inline-replay-pre");
  await flushAsyncWork();

  assert.deepEqual(env.context.__scriptOrder, [
    "dispose",
    "pre-before-bootstrap",
    "bootstrap",
    "post-after-bootstrap",
  ]);
  assert.equal(env.document.getElementById("replay-pre-script").getAttribute("data-gosx-navigation-replayed"), "true");
  assert.equal(env.document.getElementById("replay-post-script").getAttribute("data-gosx-navigation-replayed"), "true");
});

test("navigation runtime caches lifecycle scripts across page transitions", async () => {
  const parsedDocs = new Map();
  const env = createContext({
    fetchRoutes: {
      "http://localhost:3000/docs/a": {
        text: "__DOC_A__",
        url: "http://localhost:3000/docs/a",
      },
      "http://localhost:3000/docs/b": {
        text: "__DOC_B__",
        url: "http://localhost:3000/docs/b",
      },
      "http://localhost:3000/shared-lifecycle.js": {
        text: "window.__sharedLifecycleLoads = (window.__sharedLifecycleLoads || 0) + 1;",
        url: "http://localhost:3000/shared-lifecycle.js",
      },
    },
    parseHTML(html) {
      return parsedDocs.get(html);
    },
  });

  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();

  let bootstrapCount = 0;
  const originalBootstrap = env.context.__gosx_bootstrap_page;
  env.context.__gosx_bootstrap_page = async function() {
    bootstrapCount += 1;
    return originalBootstrap();
  };
  env.context.__gosx_dispose_page = async function() {};

  function lifecycleDoc(title, id) {
    const script = new FakeElement("script", null);
    script.setAttribute("data-gosx-script", "lifecycle");
    script.setAttribute("src", "/shared-lifecycle.js");

    const page = new FakeElement("main", null);
    page.id = id;
    page.textContent = title;

    return buildNavigatedDocument({
      title,
      headNodes: [script],
      bodyNodes: [page],
    });
  }

  parsedDocs.set("__DOC_A__", lifecycleDoc("Page A", "page-a"));
  parsedDocs.set("__DOC_B__", lifecycleDoc("Page B", "page-b"));

  runScript(navigationSource, env.context, "navigation_runtime.js");
  await env.context.__gosx_page_nav.navigate("http://localhost:3000/docs/a");
  await flushAsyncWork();
  await env.context.__gosx_page_nav.navigate("http://localhost:3000/docs/b");
  await flushAsyncWork();

  assert.equal(env.context.__sharedLifecycleLoads, 1);
  assert.equal(
    env.fetchCalls.filter((call) => call.url === "http://localhost:3000/shared-lifecycle.js").length,
    1,
  );
  assert.equal(bootstrapCount, 2);
  assert.equal(env.document.getElementById("page-b").textContent, "Page B");
});

// --- Persistent scene engines across soft navigations (v0.34.0) ---
//
// window.__gosx_reusable_engines (client/js/bootstrap-src/30-tail.js) +
// navigation_runtime.js's replaceBody/adoptOrClone let a soft navigation
// carry an unchanged engine (same component, mountId, and byte-identical
// scene props) across the swap instead of disposing and remounting it —
// the live mount element (and the canvas Scene3D creates inside it) moves
// into the new document unchanged, so its WebGL/WebGPU context survives.

test("navigation runtime reuses an engine with an identical scene: same canvas element survives, no dispose", async () => {
  const mount = new FakeElement("div", null);
  mount.id = "bg-scene";

  const sceneProps = {
    width: 320,
    height: 180,
    autoRotate: false,
    scene: { objects: [{ kind: "box", width: 1, height: 1, depth: 1, color: "#8de1ff" }] },
  };
  const manifestA = {
    engines: [
      {
        id: "bg-scene",
        mountId: "bg-scene",
        component: "GoSXScene3D",
        kind: "surface",
        jsExport: "GoSXScene3D",
        props: sceneProps,
        capabilities: ["canvas", "webgl", "animation"],
      },
    ],
  };

  const parsedDocs = new Map();
  const env = createContext({
    elements: [mount],
    enableWebGL: true,
    manifest: manifestA,
    fetchRoutes: {
      "http://localhost:3000/next": { text: "__REUSE_NEXT__", url: "http://localhost:3000/next" },
    },
    parseHTML(html) {
      return parsedDocs.get(html);
    },
  });

  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();

  const recordBefore = env.context.__gosx.engines.get("bg-scene");
  assert.ok(recordBefore, "expected the Scene3D engine to mount initially");
  const canvasBefore = mount.children[0];
  assert.ok(canvasBefore, "expected Scene3D to create a canvas inside the mount");

  let disposed = false;
  const originalDispose = recordBefore.handle.dispose.bind(recordBefore.handle);
  recordBefore.handle.dispose = function() {
    disposed = true;
    return originalDispose();
  };

  const nextMountPlaceholder = new FakeElement("div", null);
  nextMountPlaceholder.id = "bg-scene";
  const manifestScript = new FakeElement("script", null);
  manifestScript.id = "gosx-manifest";
  manifestScript.textContent = JSON.stringify(manifestA);

  parsedDocs.set("__REUSE_NEXT__", buildNavigatedDocument({
    title: "Next",
    bodyNodes: [nextMountPlaceholder, manifestScript],
  }));

  const events = [];
  env.context.__gosx_emit = function(level, cat, msg, fields) {
    events.push({ level, cat, msg, fields: fields || {} });
  };

  runScript(navigationSource, env.context, "navigation_runtime.js");
  await env.context.__gosx_page_nav.navigate("http://localhost:3000/next");
  await flushAsyncWork();

  assert.equal(disposed, false, "identical-scene navigation must not dispose the engine");
  const liveMount = env.document.getElementById("bg-scene");
  assert.strictEqual(liveMount, mount, "the mount element itself must be the SAME live element, not a clone");
  assert.strictEqual(liveMount.children[0], canvasBefore, "the SAME canvas element must survive the navigation");
  assert.equal(mount.getAttribute("data-gosx-engine-reused"), "true");
  assert.equal(
    events.some((e) => e.msg === "engine-reused-across-navigation" && e.fields.engineID === "bg-scene"),
    true,
  );
  assert.strictEqual(env.context.__gosx.engines.get("bg-scene"), recordBefore, "the SAME engine record must persist");
});

test("stripe-bridge wrapper forwards the reuse Set to the previous __gosx_bootstrap_page and __gosx_dispose_page handlers", async () => {
  const env = createContext({});

  const bootstrapCalls = [];
  const disposeCalls = [];
  env.context.__gosx_bootstrap_page = async function(reuseEngineIDs) {
    bootstrapCalls.push(reuseEngineIDs);
  };
  env.context.__gosx_dispose_page = async function(reuseEngineIDs) {
    disposeCalls.push(reuseEngineIDs);
  };

  runScript(stripeBridgeSource, env.context, "stripe-bridge.js");
  await flushAsyncWork();

  const reuseSet = new env.context.Set(["bg-scene"]);
  await env.context.__gosx_bootstrap_page(reuseSet);
  await env.context.__gosx_dispose_page(reuseSet);

  assert.equal(bootstrapCalls.length, 1);
  assert.strictEqual(bootstrapCalls[0], reuseSet, "stripe-bridge must forward the exact reuse Set to the wrapped bootstrap handler, not drop it");
  assert.equal(disposeCalls.length, 1);
  assert.strictEqual(disposeCalls[0], reuseSet, "stripe-bridge must forward the exact reuse Set to the wrapped dispose handler, not drop it");
});

test("navigation runtime reuses an engine across a soft navigation when stripe-bridge wraps the page lifecycle hooks", async () => {
  const mount = new FakeElement("div", null);
  mount.id = "bg-scene-stripe";

  const sceneProps = {
    width: 320,
    height: 180,
    autoRotate: false,
    scene: { objects: [{ kind: "box", width: 1, height: 1, depth: 1, color: "#8de1ff" }] },
  };
  const manifestA = {
    engines: [
      {
        id: "bg-scene-stripe",
        mountId: "bg-scene-stripe",
        component: "GoSXScene3D",
        kind: "surface",
        jsExport: "GoSXScene3D",
        props: sceneProps,
        capabilities: ["canvas", "webgl", "animation"],
      },
    ],
  };

  const parsedDocs = new Map();
  const env = createContext({
    elements: [mount],
    enableWebGL: true,
    manifest: manifestA,
    fetchRoutes: {
      "http://localhost:3000/next": { text: "__STRIPE_REUSE_NEXT__", url: "http://localhost:3000/next" },
    },
    parseHTML(html) {
      return parsedDocs.get(html);
    },
  });

  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();

  // stripe-bridge loads as a managed script AFTER bootstrap.js on a real
  // page (see server/navigation_runtime.js's script-role ordering), wrapping
  // whatever window.__gosx_bootstrap_page/__gosx_dispose_page bootstrap.js
  // already installed.
  runScript(stripeBridgeSource, env.context, "stripe-bridge.js");
  await flushAsyncWork();

  const recordBefore = env.context.__gosx.engines.get("bg-scene-stripe");
  assert.ok(recordBefore, "expected the Scene3D engine to mount initially");
  const canvasBefore = mount.children[0];
  assert.ok(canvasBefore, "expected Scene3D to create a canvas inside the mount");

  let disposed = false;
  const originalDispose = recordBefore.handle.dispose.bind(recordBefore.handle);
  recordBefore.handle.dispose = function() {
    disposed = true;
    return originalDispose();
  };

  const nextMountPlaceholder = new FakeElement("div", null);
  nextMountPlaceholder.id = "bg-scene-stripe";
  const manifestScript = new FakeElement("script", null);
  manifestScript.id = "gosx-manifest";
  manifestScript.textContent = JSON.stringify(manifestA);

  parsedDocs.set("__STRIPE_REUSE_NEXT__", buildNavigatedDocument({
    title: "Next",
    bodyNodes: [nextMountPlaceholder, manifestScript],
  }));

  const events = [];
  env.context.__gosx_emit = function(level, cat, msg, fields) {
    events.push({ level, cat, msg, fields: fields || {} });
  };

  runScript(navigationSource, env.context, "navigation_runtime.js");
  await env.context.__gosx_page_nav.navigate("http://localhost:3000/next");
  await flushAsyncWork();

  assert.equal(disposed, false, "identical-scene navigation must not dispose the engine, even through the stripe-bridge wrapper");
  const liveMount = env.document.getElementById("bg-scene-stripe");
  assert.strictEqual(liveMount, mount, "the mount element itself must be the SAME live element, not a clone");
  assert.strictEqual(liveMount.children[0], canvasBefore, "the SAME canvas element must survive the navigation");
  assert.equal(
    events.some((e) => e.msg === "engine-reused-across-navigation" && e.fields.engineID === "bg-scene-stripe"),
    true,
    "reuse telemetry must fire when the reuse Set reaches mountAllEngines through the stripe-bridge wrapper",
  );
  assert.equal(
    events.some((e) => e.msg === "engine-remounted" && e.fields.engineID === "bg-scene-stripe"),
    false,
    "an engine carried across the navigation must not also be reported as remounted",
  );
  assert.strictEqual(env.context.__gosx.engines.get("bg-scene-stripe"), recordBefore, "the SAME engine record must persist");
});

test("navigation runtime reuses an engine when incoming scene still carries shaderLib postEffect refs", async () => {
  const mount = new FakeElement("div", null);
  mount.id = "bg-scene-postfx";
  const libID = "sl:postfx001122334455";
  const shader = "@vertex fn vertexMain(@builtin(vertex_index) vi: u32) -> @builtin(position) vec4<f32> { return vec4<f32>(0.0); }\n@fragment fn fragmentMain() -> @location(0) vec4<f32> { return vec4<f32>(1.0); }";

  function makeManifest() {
    return {
      engines: [
        {
          id: "bg-scene-postfx",
          mountId: "bg-scene-postfx",
          component: "GoSXScene3D",
          kind: "surface",
          jsExport: "GoSXScene3D",
          props: {
            width: 320,
            height: 180,
            autoRotate: false,
            scene: {
              objects: [{ kind: "box", width: 1, height: 1, depth: 1, color: "#8de1ff" }],
              postEffects: [{ kind: "customPost", name: "flare-shield", fragmentWGSLRef: libID, vertexWGSLRef: libID }],
              shaderLib: { [libID]: shader },
            },
          },
          capabilities: ["canvas", "webgl", "animation"],
        },
      ],
    };
  }

  const parsedDocs = new Map();
  const env = createContext({
    elements: [mount],
    enableWebGL: true,
    manifest: makeManifest(),
    fetchRoutes: {
      "http://localhost:3000/next-postfx": { text: "__REUSE_POSTFX_NEXT__", url: "http://localhost:3000/next-postfx" },
    },
    parseHTML(html) {
      return parsedDocs.get(html);
    },
  });

  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();

  const recordBefore = env.context.__gosx.engines.get("bg-scene-postfx");
  assert.ok(recordBefore, "expected the Scene3D engine to mount initially");
  const canvasBefore = mount.children[0];
  let disposed = false;
  const originalDispose = recordBefore.handle.dispose.bind(recordBefore.handle);
  recordBefore.handle.dispose = function() {
    disposed = true;
    return originalDispose();
  };

  const nextMountPlaceholder = new FakeElement("div", null);
  nextMountPlaceholder.id = "bg-scene-postfx";
  const manifestScript = new FakeElement("script", null);
  manifestScript.id = "gosx-manifest";
  manifestScript.textContent = JSON.stringify(makeManifest());
  parsedDocs.set("__REUSE_POSTFX_NEXT__", buildNavigatedDocument({
    title: "Next",
    bodyNodes: [nextMountPlaceholder, manifestScript],
  }));

  runScript(navigationSource, env.context, "navigation_runtime.js");
  await env.context.__gosx_page_nav.navigate("http://localhost:3000/next-postfx");
  await flushAsyncWork();

  assert.equal(disposed, false, "raw shaderLib refs must compare equal after incoming manifest inflation");
  const liveMount = env.document.getElementById("bg-scene-postfx");
  assert.strictEqual(liveMount, mount, "the live mount must be carried over");
  assert.strictEqual(liveMount.children[0], canvasBefore, "the existing canvas must survive");
  assert.equal(manifestScript.textContent.includes("fragmentWGSLRef"), true, "incoming DOM manifest text must not be mutated");
});

test("split runtime navigation reuses an identical Scene3D engine and keeps the same canvas", async () => {
  const mount = new FakeElement("div", null);
  mount.id = "bg-scene-split";

  const sceneProps = {
    width: 320,
    height: 180,
    autoRotate: false,
    scene: { objects: [{ kind: "box", width: 1, height: 1, depth: 1, color: "#8de1ff" }] },
  };
  const manifest = {
    engines: [
      {
        id: "bg-scene-split",
        mountId: "bg-scene-split",
        component: "GoSXScene3D",
        kind: "surface",
        jsExport: "GoSXScene3D",
        props: sceneProps,
        capabilities: ["canvas", "webgl", "animation"],
      },
    ],
  };

  const parsedDocs = new Map();
  const env = createContext({
    elements: [mount],
    enableWebGL: true,
    manifest,
    fetchRoutes: scene3DCommandFetchRoutes({
      "/gosx/bootstrap-feature-engines.js": { text: bootstrapFeatureEnginesSource },
      "http://localhost:3000/split-next": { text: "__SPLIT_REUSE_NEXT__", url: "http://localhost:3000/split-next" },
    }),
    parseHTML(html) {
      return parsedDocs.get(html);
    },
  });

  runScript(bootstrapRuntimeSource, env.context, "bootstrap-runtime.js");
  runScript(bootstrapFeatureScene3DSource, env.context, "bootstrap-feature-scene3d.js");
  await flushAsyncWork();

  const recordBefore = env.context.__gosx.engines.get("bg-scene-split");
  assert.ok(recordBefore, "expected split runtime to mount the Scene3D engine");
  assert.equal(typeof env.context.__gosx.scene3d.dispatchCommands, "function");
  assert.equal(typeof env.context.__gosx.scene3d.preloadModel, "function");
  assert.equal(typeof env.context.__gosx.scene3d.setPerformanceTelemetry, "function");
  const canvasBefore = mount.children[0];
  assert.ok(canvasBefore, "expected split Scene3D to create a canvas");

  let disposed = false;
  const originalDispose = recordBefore.handle.dispose.bind(recordBefore.handle);
  recordBefore.handle.dispose = function() {
    disposed = true;
    return originalDispose();
  };

  const nextMountPlaceholder = new FakeElement("div", null);
  nextMountPlaceholder.id = "bg-scene-split";
  const manifestScript = new FakeElement("script", null);
  manifestScript.id = "gosx-manifest";
  manifestScript.textContent = JSON.stringify(manifest);

  parsedDocs.set("__SPLIT_REUSE_NEXT__", buildNavigatedDocument({
    title: "Split Next",
    bodyNodes: [nextMountPlaceholder, manifestScript],
  }));

  const events = [];
  env.context.__gosx_emit = function(level, cat, msg, fields) {
    events.push({ level, cat, msg, fields: fields || {} });
  };

  runScript(navigationSource, env.context, "navigation_runtime.js");
  await env.context.__gosx_page_nav.navigate("http://localhost:3000/split-next");
  await flushAsyncWork();

  assert.equal(disposed, false, "split runtime identical-scene navigation must not dispose the engine");
  const liveMount = env.document.getElementById("bg-scene-split");
  assert.strictEqual(liveMount, mount, "split runtime must move the same mount element");
  assert.strictEqual(liveMount.children[0], canvasBefore, "split runtime must keep the same canvas element");
  assert.equal(liveMount.getAttribute("data-gosx-engine-reused"), "true");
  assert.strictEqual(env.context.__gosx.engines.get("bg-scene-split"), recordBefore, "the same engine record must persist");
  assert.equal(
    events.some((e) => e.msg === "engine-reused-across-navigation" && e.fields.engineID === "bg-scene-split"),
    true,
  );
});

test("Scene3D public command API queues before ready, applies with ack, and rejects absent targets", async () => {
  const env = createContext({ fetchRoutes: scene3DCommandFetchRoutes() });
  runScript(bootstrapRuntimeSource, env.context, "bootstrap-runtime.js");
  runScript(bootstrapFeatureScene3DSource, env.context, "bootstrap-feature-scene3d.js");

  assert.equal(typeof env.context.__gosx.scene3d.dispatchCommands, "function");
  const commands = [{ kind: 14, data: { effects: [{ name: "lens", uniforms: { amount: 0.5 } }] } }];
  const pending = env.context.__gosx.scene3d.dispatchCommands("queued-scene", commands, { timeoutMS: 500 });
  let settled = false;
  pending.then(() => { settled = true; });
  await flushAsyncWork();
  assert.equal(settled, false, "pre-ready command Promise must wait for actual command readiness");

  const mount = new FakeElement("div", env.document);
  mount.id = "queued-scene";
  env.document.body.appendChild(mount);
  const applied = [];
  const handle = {
    __gosxScene3DCommandReady: true,
    applyCommands(received) {
      applied.push(received);
      return Promise.resolve({ ok: true });
    },
  };

  mount.__gosxScene3DHandle = handle;
  const ack = await pending;

  assert.deepEqual(applied, [commands], "queued commands must apply after readiness");
  assert.equal(ack.applied, true);
  assert.equal(ack.revision, 1);
  assert.equal(mount.getAttribute("data-gosx-scene3d-command-revision"), "1");
  assert.equal(mount.getAttribute("data-gosx-scene3d-command-applied-revision"), "1");

  await assert.rejects(
    env.context.__gosx.scene3d.dispatchCommands("missing-scene", commands, { timeoutMS: 1 }),
    /did not become ready: missing-scene/,
  );
});

test("Scene3D public command API flushes queues for distinct engine and mount ids", async () => {
  const env = createContext({ fetchRoutes: scene3DCommandFetchRoutes() });
  runScript(bootstrapRuntimeSource, env.context, "bootstrap-runtime.js");
  runScript(bootstrapFeatureScene3DSource, env.context, "bootstrap-feature-scene3d.js");

  const commands = [{ kind: 14, data: { effects: [{ name: "lens", uniforms: { amount: 0.5 } }] } }];
  const byMount = env.context.__gosx.scene3d.dispatchCommands("scene-mount-id", commands, { timeoutMS: 500 });
  const byEngine = env.context.__gosx.scene3d.dispatchCommands("scene-engine-id", commands, { timeoutMS: 500 });
  const mount = new FakeElement("div", env.document);
  mount.id = "scene-mount-id";
  env.document.body.appendChild(mount);
  let applied = 0;
  const handle = {
    __gosxScene3DCommandReady: true,
    applyCommands() {
      applied += 1;
      return Promise.resolve({ ok: true });
    },
  };

  mount.__gosxScene3DHandle = handle;
  env.context.__gosx.engines.set("scene-engine-id", { component: "GoSXScene3D", handle, mount });
  const ackMount = await byMount;
  const ackEngine = await byEngine;

  assert.equal(applied, 2, "ready marker must flush queues under mount id and engine id");
  assert.equal(ackMount.applied, true);
  assert.equal(ackEngine.applied, true);
  assert.equal(ackMount.revision, 1);
  assert.equal(ackEngine.revision, 2);
});

test("Scene3D public command revisions stay monotonic across navigation reuse", async () => {
  const mount = new FakeElement("div", null);
  mount.id = "revision-scene";
  const manifest = {
    engines: [
      {
        id: "revision-scene",
        mountId: "revision-scene",
        component: "GoSXScene3D",
        kind: "surface",
        jsExport: "GoSXScene3D",
        props: {
          width: 320,
          height: 180,
          autoRotate: false,
          scene: {
            postEffects: [{ kind: "customPost", name: "lens", uniforms: { amount: 0.1 } }],
          },
        },
        capabilities: ["canvas", "webgl", "animation"],
      },
    ],
  };
  const parsedDocs = new Map();
  const env = createContext({
    elements: [mount],
    enableWebGL: true,
    manifest: manifest,
    fetchRoutes: scene3DCommandFetchRoutes({
      "http://localhost:3000/revision-next": { text: "__REVISION_NEXT__", url: "http://localhost:3000/revision-next" },
    }),
    parseHTML(html) {
      return parsedDocs.get(html);
    },
  });

  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();

  const first = await env.context.__gosx.scene3d.dispatchCommands("revision-scene", [
    { kind: 14, data: { effects: [{ name: "lens", uniforms: { amount: 0.2 } }] } },
  ]);
  assert.equal(first.revision, 1);

  const nextMountPlaceholder = new FakeElement("div", null);
  nextMountPlaceholder.id = "revision-scene";
  const manifestScript = new FakeElement("script", null);
  manifestScript.id = "gosx-manifest";
  manifestScript.textContent = JSON.stringify(manifest);
  parsedDocs.set("__REVISION_NEXT__", buildNavigatedDocument({
    title: "Revision Next",
    bodyNodes: [nextMountPlaceholder, manifestScript],
  }));

  runScript(navigationSource, env.context, "navigation_runtime.js");
  await env.context.__gosx_page_nav.navigate("http://localhost:3000/revision-next");
  await flushAsyncWork();

  const liveMount = env.document.getElementById("revision-scene");
  assert.strictEqual(liveMount, mount, "engine mount must be reused");
  const second = await env.context.__gosx.scene3d.dispatchCommands("revision-scene", [
    { kind: 14, data: { effects: [{ name: "lens", uniforms: { amount: 0.3 } }] } },
  ]);
  assert.equal(second.revision, 2);
  assert.equal(liveMount.getAttribute("data-gosx-scene3d-command-applied-revision"), "2");
});

test("Scene3D public command API loads through the authored compat URL and rejects failed chunk loads", async () => {
  const commands = [{ kind: 14, data: { effects: [{ name: "lens", uniforms: { amount: 0.5 } }] } }];
  const env = createContext({
    fetchRoutes: {
      "/gosx/assets/runtime/bootstrap-feature-scene3d-command.hash.js?v=hash": { text: bootstrapFeatureScene3DCommandSource },
    },
  });
  const featureTag = env.document.createElement("script");
  featureTag.setAttribute("data-gosx-script", "feature-scene3d");
  featureTag.setAttribute("data-gosx-scene3d-command-url", "/gosx/assets/runtime/bootstrap-feature-scene3d-command.hash.js?v=hash");
  env.document.head.appendChild(featureTag);
  runScript(bootstrapRuntimeSource, env.context, "bootstrap-runtime.js");
  runScript(bootstrapFeatureScene3DSource, env.context, "bootstrap-feature-scene3d.js");

  const applied = [];
  const ack = await env.context.__gosx.scene3d.dispatchCommands({ id: "ready-scene", __gosxScene3DCommandReady: true, applyCommands: (received) => {
    applied.push(received);
    return Promise.resolve();
  } }, commands);
  assert.equal(ack.applied, true);
  assert.deepEqual(applied, [commands]);
  assert.equal(
    env.fetchCalls.some((call) => call.url === "/gosx/assets/runtime/bootstrap-feature-scene3d-command.hash.js?v=hash"),
    true,
    "dispatch must lazy-load the renderer-authored compat URL",
  );

  const missing = createContext({});
  runScript(bootstrapRuntimeSource, missing.context, "bootstrap-runtime.js");
  runScript(bootstrapFeatureScene3DSource, missing.context, "bootstrap-feature-scene3d.js");
  await assert.rejects(
    missing.context.__gosx.scene3d.dispatchCommands({ id: "ready-scene", __gosxScene3DCommandReady: true, applyCommands() {} }, commands),
    /script not found: \/gosx\/bootstrap-feature-scene3d-command\.js/,
  );
});

test("Scene3D public command API retries lazy command chunk load after failure", async () => {
  const env = createContext({});
  const commands = [{ kind: 14, data: { effects: [{ name: "retry", uniforms: { amount: 1 } }] } }];
  const applied = [];
  let attempts = 0;
  // Count and fail only the command chunk. The base scene3d chunk also warm
  // starts the WebGL chunk on a browser with no navigator.gpu, so a global
  // counter would spend the injected failure on the wrong script.
  env.document.scriptLoader = function(src, scriptElement) {
    env.fetchCalls.push({ url: src, init: {} });
    const isCommandChunk = String(src).indexOf("bootstrap-feature-scene3d-command.js") >= 0;
    if (!isCommandChunk) {
      setTimeout(() => { scriptElement.onload({}); }, 0);
      return;
    }
    attempts += 1;
    setTimeout(() => {
      if (attempts === 1) {
        scriptElement.onerror(new Error("script not found: " + src));
        return;
      }
      runScript(bootstrapFeatureScene3DCommandSource, env.context, src);
      scriptElement.onload({});
    }, 0);
  };
  runScript(bootstrapRuntimeSource, env.context, "bootstrap-runtime.js");
  runScript(bootstrapFeatureScene3DSource, env.context, "bootstrap-feature-scene3d.js");

  const target = {
    id: "retry-scene",
    __gosxScene3DCommandReady: true,
    applyCommands(received) {
      applied.push(received);
      return Promise.resolve();
    },
  };
  await assert.rejects(
    env.context.__gosx.scene3d.dispatchCommands(target, commands),
    /script not found: \/gosx\/bootstrap-feature-scene3d-command\.js/,
  );
  const ack = await env.context.__gosx.scene3d.dispatchCommands(target, commands);

  assert.equal(attempts, 2, "failed lazy-load promise must be cleared so dispatch can retry");
  assert.equal(ack.applied, true);
  assert.deepEqual(applied, [commands]);
});

test("Scene3D WebGL recovery API owns one-shot session persistence and reload", () => {
  function makeSessionStorage(initial) {
    const values = new Map(Object.entries(initial || {}));
    return {
      getItem(key) {
        return values.has(key) ? values.get(key) : null;
      },
      setItem(key, value) {
        values.set(key, String(value));
      },
      removeItem(key) {
        values.delete(key);
      },
    };
  }

  const env = createContext({});
  env.context.sessionStorage = makeSessionStorage({});
  let reloads = 0;
  env.context.location.reload = function() { reloads += 1; };
  runScript(bootstrapRuntimeSource, env.context, "bootstrap-runtime.js");
  runScript(bootstrapFeatureScene3DSource, env.context, "bootstrap-feature-scene3d.js");

  assert.equal(typeof env.context.__gosx.scene3d.requestWebGLRecovery, "function");
  assert.equal(typeof env.context.__gosx_scene3d_request_webgl_recovery, "function");
  assert.equal(typeof env.context.__gosx_scene3d_clear_webgl_recovery, "function");
  assert.equal(typeof env.context.__gosx_scene3d_force_webgl_requested, "function");
  assert.equal(typeof env.context.__gosx_scene3d_is_webgl_recovery_active, "function");
  assert.equal(typeof env.context.__gosx_scene3d.requestWebGLRecovery, "function");
  assert.equal(typeof env.context.__gosx_scene3d.clearWebGLRecovery, "function");
  assert.equal(typeof env.context.__gosx_scene3d.forceWebGLRequested, "function");
  assert.equal(typeof env.context.__gosx_scene3d.isWebGLRecoveryActive, "function");
  assert.equal(typeof env.context.__gosx_scene3d.dispatchCommands, "function");
  assert.equal(env.context.__gosx.scene3d.isWebGLRecoveryActive(), false);
  assert.equal(env.context.__gosx_scene3d_is_webgl_recovery_active(), false);
  const result = env.context.__gosx_scene3d_request_webgl_recovery({ reload: true });
  assert.equal(result.forceWebGL, true);
  assert.equal(result.reload, true);
  assert.equal(reloads, 1, "reload:true must request a reload");
  assert.equal(env.context.__gosx_scene3d_force_webgl, true, "compatibility global remains internally set");
  assert.equal(env.context.__gosx.scene3d.forceWebGLRequested(), true);
  assert.equal(env.context.__gosx_scene3d.forceWebGLRequested(), true);
  assert.equal(env.context.__gosx.scene3d.isWebGLRecoveryActive(), true);
  assert.equal(env.context.__gosx_scene3d_is_webgl_recovery_active(), true);
  assert.equal(env.context.sessionStorage.getItem("gosx:scene3d:force-webgl-next"), "1");

  env.context.__gosx_scene3d.clearWebGLRecovery();
  assert.equal(env.context.__gosx.scene3d.isWebGLRecoveryActive(), false);
  assert.equal(env.context.sessionStorage.getItem("gosx:scene3d:force-webgl-next"), null);

  const envReload = createContext({});
  envReload.context.sessionStorage = makeSessionStorage({ "gosx:scene3d:force-webgl-next": "1" });
  runScript(bootstrapRuntimeSource, envReload.context, "bootstrap-runtime.js");
  runScript(bootstrapFeatureScene3DSource, envReload.context, "bootstrap-feature-scene3d.js");
  assert.equal(envReload.context.sessionStorage.getItem("gosx:scene3d:force-webgl-next"), null, "stored recovery marker must be one-shot");
  assert.equal(envReload.context.__gosx.scene3d.isWebGLRecoveryActive(), true, "one-shot marker activates current page recovery state");
  assert.equal(envReload.context.__gosx.scene3d.forceWebGLRequested(), true);

  assert.match(
    bootstrapScene3DMountSourceFile,
    /__gosx_scene3d_force_webgl_requested/,
    "Scene3D backend selection must consume the framework recovery API",
  );
});

test("Scene3D canonical preloadModel wraps legacy model preload hook", async () => {
  const env = createContext({});
  runScript(bootstrapRuntimeSource, env.context, "bootstrap-runtime.js");
  runScript(bootstrapFeatureScene3DSource, env.context, "bootstrap-feature-scene3d.js");

  assert.equal(typeof env.context.__gosx.scene3d.preloadModel, "function");
  assert.equal(typeof env.context.__gosx_scene3d_preload_model, "function");
  const original = env.context.__gosx_scene3d_preload_model;
  const calls = [];
  env.context.__gosx_scene3d_preload_model = function(src) {
    calls.push(src);
    return Promise.resolve({ src: src, objects: [] });
  };

  const result = await env.context.__gosx.scene3d.preloadModel(" /models/full.glb ");
  assert.equal(calls.length, 1);
  assert.equal(calls[0], " /models/full.glb ");
  assert.equal(result.src, " /models/full.glb ");

  env.context.__gosx_scene3d_preload_model = original;
});

test("Scene3D canonical performance telemetry API owns legacy perf flag", async () => {
  const env = createContext({});
  runScript(bootstrapRuntimeSource, env.context, "bootstrap-runtime.js");
  runScript(bootstrapFeatureScene3DSource, env.context, "bootstrap-feature-scene3d.js");

  assert.equal(typeof env.context.__gosx.scene3d.setPerformanceTelemetry, "function");
  assert.equal(typeof env.context.__gosx.scene3d.isPerformanceTelemetryEnabled, "function");
  assert.equal(env.context.__gosx.scene3d.isPerformanceTelemetryEnabled(), false);
  assert.equal(env.context.__gosx.scene3d.setPerformanceTelemetry(true), true);
  assert.equal(env.context.__gosx_scene3d_perf, true);
  assert.equal(env.context.__gosx.scene3d.isPerformanceTelemetryEnabled(), true);
  assert.equal(env.context.__gosx.scene3d.setPerformanceTelemetry(false), false);
  assert.equal(env.context.__gosx_scene3d_perf, false);
  assert.equal(env.context.__gosx.scene3d.isPerformanceTelemetryEnabled(), false);
});

test("navigation runtime disposes and remounts an engine when the scene payload differs", async () => {
  const mount = new FakeElement("div", null);
  mount.id = "bg-scene-2";

  const sceneA = {
    width: 320,
    height: 180,
    autoRotate: false,
    scene: { objects: [{ kind: "box", width: 1, height: 1, depth: 1, color: "#8de1ff" }] },
  };
  const manifestA = {
    engines: [
      {
        id: "bg-scene-2",
        mountId: "bg-scene-2",
        component: "GoSXScene3D",
        kind: "surface",
        jsExport: "GoSXScene3D",
        props: sceneA,
        capabilities: ["canvas", "webgl", "animation"],
      },
    ],
  };

  const parsedDocs = new Map();
  const env = createContext({
    elements: [mount],
    enableWebGL: true,
    manifest: manifestA,
    fetchRoutes: {
      "http://localhost:3000/changed": { text: "__CHANGED_NEXT__", url: "http://localhost:3000/changed" },
    },
    parseHTML(html) {
      return parsedDocs.get(html);
    },
  });

  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();

  const recordBefore = env.context.__gosx.engines.get("bg-scene-2");
  assert.ok(recordBefore, "expected the Scene3D engine to mount initially");
  assert.ok(mount.children[0], "expected Scene3D to create a canvas inside the mount");

  let disposed = false;
  const originalDispose = recordBefore.handle.dispose.bind(recordBefore.handle);
  recordBefore.handle.dispose = function() {
    disposed = true;
    return originalDispose();
  };

  // Same id/mountId/component, DIFFERENT scene payload (color changed) —
  // must NOT be treated as reusable.
  const sceneB = {
    width: 320,
    height: 180,
    autoRotate: false,
    scene: { objects: [{ kind: "box", width: 1, height: 1, depth: 1, color: "#ff8d8d" }] },
  };
  const manifestB = {
    engines: [
      {
        id: "bg-scene-2",
        mountId: "bg-scene-2",
        component: "GoSXScene3D",
        kind: "surface",
        jsExport: "GoSXScene3D",
        props: sceneB,
        capabilities: ["canvas", "webgl", "animation"],
      },
    ],
  };

  const nextMountPlaceholder = new FakeElement("div", null);
  nextMountPlaceholder.id = "bg-scene-2";
  const manifestScript = new FakeElement("script", null);
  manifestScript.id = "gosx-manifest";
  manifestScript.textContent = JSON.stringify(manifestB);

  parsedDocs.set("__CHANGED_NEXT__", buildNavigatedDocument({
    title: "Next",
    bodyNodes: [nextMountPlaceholder, manifestScript],
  }));

  const events = [];
  env.context.__gosx_emit = function(level, cat, msg, fields) {
    events.push({ level, cat, msg, fields: fields || {} });
  };

  runScript(navigationSource, env.context, "navigation_runtime.js");
  await env.context.__gosx_page_nav.navigate("http://localhost:3000/changed");
  await flushAsyncWork();

  assert.equal(disposed, true, "a changed scene payload must dispose the outgoing engine");
  const liveMount = env.document.getElementById("bg-scene-2");
  assert.notStrictEqual(liveMount, mount, "a changed scene must NOT adopt the old mount element");
  const newRecord = env.context.__gosx.engines.get("bg-scene-2");
  assert.ok(newRecord, "expected the engine to be remounted");
  assert.notStrictEqual(newRecord, recordBefore, "a fresh engine record must replace the disposed one");
  assert.equal(
    events.some((e) => e.msg === "engine-remounted" && e.fields.engineID === "bg-scene-2"),
    true,
  );
  assert.equal(events.some((e) => e.msg === "engine-reused-across-navigation"), false);
});

test("navigation runtime reuses an engine while hub subscriptions disconnect and re-arm cleanly", async () => {
  function makeSocket(url) {
    return {
      url,
      closeCalled: false,
      onmessage: null,
      onclose: null,
      onerror: null,
      close() {
        this.closeCalled = true;
      },
    };
  }

  const mount = new FakeElement("div", null);
  mount.id = "bg-scene-3";

  const sceneProps = {
    width: 320,
    height: 180,
    autoRotate: false,
    scene: { objects: [{ kind: "box", width: 1, height: 1, depth: 1, color: "#8de1ff" }] },
  };
  const manifestA = {
    runtime: { path: "/runtime.wasm" },
    engines: [
      {
        id: "bg-scene-3",
        mountId: "bg-scene-3",
        component: "GoSXScene3D",
        kind: "surface",
        jsExport: "GoSXScene3D",
        props: sceneProps,
        capabilities: ["canvas", "webgl", "animation"],
      },
    ],
    hubs: [
      {
        id: "gosx-hub-nav",
        name: "presence",
        path: "/gosx/hub/presence",
        bindings: [{ event: "snapshot", signal: "$presence" }],
      },
    ],
  };

  const parsedDocs = new Map();
  const env = createContext({
    elements: [mount],
    enableWebGL: true,
    createWebSocket: makeSocket,
    manifest: manifestA,
    fetchRoutes: {
      "/runtime.wasm": { bytes: [0, 97, 115, 109] },
      "http://localhost:3000/hub-next": { text: "__HUB_NEXT__", url: "http://localhost:3000/hub-next" },
    },
    parseHTML(html) {
      return parsedDocs.get(html);
    },
  });

  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();

  assert.equal(env.context.__gosx.hubs.size, 1, "expected the hub to connect on the initial page");
  const recordBefore = env.context.__gosx.engines.get("bg-scene-3");
  assert.ok(recordBefore, "expected the Scene3D engine to mount initially");
  const canvasBefore = mount.children[0];
  const socketBefore = env.sockets[0];
  assert.ok(socketBefore, "expected the hub to open a socket on the initial page");

  let disposed = false;
  const originalDispose = recordBefore.handle.dispose.bind(recordBefore.handle);
  recordBefore.handle.dispose = function() {
    disposed = true;
    return originalDispose();
  };

  const nextMountPlaceholder = new FakeElement("div", null);
  nextMountPlaceholder.id = "bg-scene-3";
  const manifestScript = new FakeElement("script", null);
  manifestScript.id = "gosx-manifest";
  manifestScript.textContent = JSON.stringify(manifestA);

  parsedDocs.set("__HUB_NEXT__", buildNavigatedDocument({
    title: "Next",
    bodyNodes: [nextMountPlaceholder, manifestScript],
  }));

  runScript(navigationSource, env.context, "navigation_runtime.js");
  await env.context.__gosx_page_nav.navigate("http://localhost:3000/hub-next");
  await flushAsyncWork();

  // Engine reuse is unaffected by a hub reconnecting alongside it.
  assert.equal(disposed, false, "identical-scene navigation must not dispose the engine even with a hub present");
  assert.strictEqual(env.document.getElementById("bg-scene-3").children[0], canvasBefore);

  // Hub disconnect+reconnect is unchanged behavior — it must keep working
  // cleanly (exactly one re-arm, no double-connect) alongside engine reuse.
  assert.equal(socketBefore.closeCalled, true, "the outgoing hub socket must be closed on navigation");
  assert.equal(env.context.__gosx.hubs.size, 1, "the hub must be reconnected after navigation");
  assert.equal(env.sockets.length, 2, "exactly one new socket must be opened for the re-armed hub");
  assert.notStrictEqual(env.sockets[1], socketBefore);
});

test("navigation runtime marks current and ancestor links and exposes navigation state", async () => {
  const docsLink = new FakeElement("a", null);
  docsLink.setAttribute("href", "/docs");
  docsLink.setAttribute("data-gosx-link", "");
  docsLink.textContent = "Docs";

  const formsLink = new FakeElement("a", null);
  formsLink.setAttribute("href", "/docs/forms");
  formsLink.setAttribute("data-gosx-link", "");
  formsLink.textContent = "Forms";

  const blogLink = new FakeElement("a", null);
  blogLink.setAttribute("href", "/blog");
  blogLink.setAttribute("data-gosx-link", "");
  blogLink.textContent = "Blog";

  const env = createContext({
    elements: [docsLink, formsLink, blogLink],
  });
  env.context.location.href = "http://localhost:3000/docs/forms";

  runScript(navigationSource, env.context, "navigation_runtime.js");
  await flushAsyncWork();

  assert.equal(docsLink.getAttribute("data-gosx-link-current-policy"), "auto");
  assert.equal(docsLink.getAttribute("data-gosx-link-current"), "ancestor");
  assert.equal(formsLink.getAttribute("data-gosx-link-current-policy"), "auto");
  assert.equal(formsLink.getAttribute("data-gosx-link-current"), "page");
  assert.equal(formsLink.getAttribute("aria-current"), "page");
  assert.equal(blogLink.getAttribute("data-gosx-link-current-policy"), "auto");
  assert.equal(blogLink.getAttribute("data-gosx-link-current"), "none");
  assert.equal(env.document.documentElement.getAttribute("data-gosx-navigation-state"), "idle");
  assert.equal(env.document.documentElement.getAttribute("data-gosx-navigation-current-path"), "/docs/forms");
  assert.equal(env.context.__gosx_page_nav.getState().currentPath, "/docs/forms");
});

test("navigation runtime honors explicit link current policy", async () => {
  const link = new FakeElement("a", null);
  link.setAttribute("href", "/docs/forms");
  link.setAttribute("data-gosx-link", "");
  link.setAttribute("data-gosx-link-current-policy", "none");
  link.setAttribute("data-gosx-link-current", "none");
  link.textContent = "Forms";

  const env = createContext({
    elements: [link],
  });
  env.context.location.href = "http://localhost:3000/docs/forms";

  runScript(navigationSource, env.context, "navigation_runtime.js");
  await flushAsyncWork();

  assert.equal(link.getAttribute("data-gosx-link-current-policy"), "none");
  assert.equal(link.getAttribute("data-gosx-link-current"), "none");
  assert.equal(link.hasAttribute("aria-current"), false);
});

test("navigation runtime prefetches marked links and reuses cached HTML", async () => {
  const link = new FakeElement("a", null);
  link.setAttribute("href", "/prefetch");
  link.setAttribute("data-gosx-link", "");
  link.textContent = "Prefetch";

  const parsedDocs = new Map();
  const env = createContext({
    elements: [link],
    fetchRoutes: {
      "http://localhost:3000/prefetch": {
        text: "__PREFETCH_DOC__",
        url: "http://localhost:3000/prefetch",
      },
    },
    parseHTML(html) {
      return parsedDocs.get(html);
    },
  });

  parsedDocs.set("__PREFETCH_DOC__", buildNavigatedDocument({
    title: "Prefetched",
    bodyNodes: [new FakeElement("div", null)],
  }));

  runScript(navigationSource, env.context, "navigation_runtime.js");

  const overListener = env.document.eventListeners.get("mouseover")[0];
  overListener({ type: "mouseover", target: link });
  await flushAsyncWork();
  assert.equal(env.fetchCalls.length, 1);
  assert.equal(link.getAttribute("data-gosx-prefetch-state"), "ready");

  const clickListener = env.document.eventListeners.get("click")[0];
  clickListener({
    type: "click",
    button: 0,
    target: link,
    defaultPrevented: false,
    metaKey: false,
    ctrlKey: false,
    shiftKey: false,
    altKey: false,
    preventDefault() {},
  });
  await flushAsyncWork();

  assert.equal(env.fetchCalls.length, 1);
  assert.equal(env.document.title, "Prefetched");
});

test("navigation prefetch never requests external or non-HTTP managed links", async () => {
  const external = new FakeElement("a", null);
  external.setAttribute("href", "https://attacker.example/page");
  external.setAttribute("data-gosx-link", "");
  external.setAttribute("data-gosx-prefetch", "render");
  const dangerous = new FakeElement("a", null);
  dangerous.setAttribute("href", "javascript:alert(1)");
  dangerous.setAttribute("data-gosx-link", "");
  dangerous.setAttribute("data-gosx-prefetch", "force");
  const blob = new FakeElement("a", null);
  blob.setAttribute("href", "blob:http://localhost:3000/attacker");
  blob.setAttribute("data-gosx-link", "");
  const env = createContext({ elements: [external, dangerous, blob] });

  runScript(navigationSource, env.context, "navigation_runtime.js");
  const hover = env.document.eventListeners.get("mouseover")[0];
  const focus = env.document.eventListeners.get("focusin")[0];
  for (const link of [external, dangerous, blob]) {
    hover({ type: "mouseover", target: link });
    focus({ type: "focusin", target: link });
  }
  await flushAsyncWork();

  assert.equal(env.fetchCalls.length, 0);
  for (const link of [external, dangerous, blob]) {
    assert.equal(link.getAttribute("data-gosx-prefetch-state"), "idle");
  }
});

test("navigation runtime page cache entries expire after their TTL", async () => {
  const link = new FakeElement("a", null);
  link.setAttribute("href", "/ttl-page");
  link.setAttribute("data-gosx-link", "");
  link.textContent = "TTL page";

  const parsedDocs = new Map();
  const env = createContext({
    elements: [link],
    fetchRoutes: {
      "http://localhost:3000/ttl-page": {
        text: "__TTL_DOC__",
        url: "http://localhost:3000/ttl-page",
      },
    },
    parseHTML(html) {
      return parsedDocs.get(html);
    },
  });

  parsedDocs.set("__TTL_DOC__", buildNavigatedDocument({
    title: "TTL",
    bodyNodes: [new FakeElement("div", null)],
  }));

  const clock = installManualClock(env.context, 0);
  runScript(navigationSource, env.context, "navigation_runtime.js");

  const overListener = env.document.eventListeners.get("mouseover")[0];

  overListener({ type: "mouseover", target: link });
  await flushAsyncWork();
  assert.equal(env.fetchCalls.length, 1);
  assert.equal(link.getAttribute("data-gosx-prefetch-state"), "ready");

  // Well inside the 5-minute TTL: the cached entry still answers a re-hover.
  clock.advance(60 * 1000);
  overListener({ type: "mouseover", target: link });
  await flushAsyncWork();
  assert.equal(env.fetchCalls.length, 1);

  // Past the 5-minute TTL: the entry is a miss, so the runtime refetches.
  clock.advance(5 * 60 * 1000);
  overListener({ type: "mouseover", target: link });
  await flushAsyncWork();
  assert.equal(env.fetchCalls.length, 2);
  assert.equal(env.fetchCalls[1].url, env.fetchCalls[0].url);
});

test("navigation runtime never caches HTML for a page that opts out via meta", async () => {
  const link = new FakeElement("a", null);
  link.setAttribute("href", "/no-store-page");
  link.setAttribute("data-gosx-link", "");
  link.textContent = "No-store page";

  const optOutMeta = new FakeElement("meta", null);
  optOutMeta.setAttribute("name", "gosx-page-cache");
  optOutMeta.setAttribute("content", "no-store");

  const parsedDocs = new Map();
  const env = createContext({
    elements: [link],
    fetchRoutes: {
      "http://localhost:3000/no-store-page": {
        text: "__NO_STORE_DOC__",
        url: "http://localhost:3000/no-store-page",
      },
    },
    parseHTML(html) {
      return parsedDocs.get(html);
    },
  });

  parsedDocs.set("__NO_STORE_DOC__", buildNavigatedDocument({
    title: "No store",
    headNodes: [optOutMeta],
    bodyNodes: [new FakeElement("div", null)],
  }));

  runScript(navigationSource, env.context, "navigation_runtime.js");

  const overListener = env.document.eventListeners.get("mouseover")[0];

  overListener({ type: "mouseover", target: link });
  await flushAsyncWork();
  assert.equal(env.fetchCalls.length, 1);
  assert.equal(link.getAttribute("data-gosx-prefetch-state"), "ready");
  assert.equal(env.context.__gosx_page_cache.size, 0);

  // A second hover has nothing cached to reuse, so it fetches again.
  overListener({ type: "mouseover", target: link });
  await flushAsyncWork();
  assert.equal(env.fetchCalls.length, 2);
  assert.equal(env.context.__gosx_page_cache.size, 0);
});

test("navigation runtime eagerly prefetches render-marked links", async () => {
  const link = new FakeElement("a", null);
  link.setAttribute("href", "/prefetch");
  link.setAttribute("data-gosx-link", "");
  link.setAttribute("data-gosx-prefetch", "render");
  link.textContent = "Prefetch";

  const parsedDocs = new Map();
  const env = createContext({
    elements: [link],
    fetchRoutes: {
      "http://localhost:3000/prefetch": {
        text: "__PREFETCH_RENDER_DOC__",
        url: "http://localhost:3000/prefetch",
      },
    },
    parseHTML(html) {
      return parsedDocs.get(html);
    },
  });

  parsedDocs.set("__PREFETCH_RENDER_DOC__", buildNavigatedDocument({
    title: "Prefetched",
    bodyNodes: [new FakeElement("div", null)],
  }));

  runScript(navigationSource, env.context, "navigation_runtime.js");
  await flushAsyncWork();

  assert.equal(env.fetchCalls.length, 1);
  assert.equal(link.getAttribute("data-gosx-prefetch-state"), "ready");
});

test("navigation runtime skips intent prefetch under reduced-data conditions", async () => {
  const link = new FakeElement("a", null);
  link.setAttribute("href", "/prefetch");
  link.setAttribute("data-gosx-link", "");
  link.textContent = "Prefetch";

  const env = createContext({
    elements: [link],
    matchMedia: {
      "(prefers-reduced-data: reduce)": true,
    },
  });

  runScript(navigationSource, env.context, "navigation_runtime.js");

  const overListener = env.document.eventListeners.get("mouseover")[0];
  overListener({ type: "mouseover", target: link });
  await flushAsyncWork();

  assert.equal(env.fetchCalls.length, 0);
  assert.equal(link.getAttribute("data-gosx-prefetch-state"), "idle");
});

test("navigation runtime leaves non-interceptable links to native handling", async () => {
  const hashLink = new FakeElement("a", null);
  hashLink.setAttribute("href", "#details");
  hashLink.setAttribute("data-gosx-link", "");

  const externalLink = new FakeElement("a", null);
  externalLink.setAttribute("href", "https://example.com/docs");
  externalLink.setAttribute("data-gosx-link", "");

  const downloadLink = new FakeElement("a", null);
  downloadLink.setAttribute("href", "/download");
  downloadLink.setAttribute("data-gosx-link", "");
  downloadLink.setAttribute("download", "");

  const targetLink = new FakeElement("a", null);
  targetLink.setAttribute("href", "/target");
  targetLink.setAttribute("data-gosx-link", "");
  targetLink.setAttribute("target", "_blank");

  const modifiedLink = new FakeElement("a", null);
  modifiedLink.setAttribute("href", "/modified");
  modifiedLink.setAttribute("data-gosx-link", "");

  const env = createContext({
    elements: [hashLink, externalLink, downloadLink, targetLink, modifiedLink],
  });

  runScript(navigationSource, env.context, "navigation_runtime.js");

  const clickListener = env.document.eventListeners.get("click")[0];
  for (const [link, overrides] of [
    [hashLink, {}],
    [externalLink, {}],
    [downloadLink, {}],
    [targetLink, {}],
    [modifiedLink, { ctrlKey: true }],
  ]) {
    let prevented = false;
    clickListener({
      type: "click",
      target: link,
      button: 0,
      ctrlKey: false,
      metaKey: false,
      shiftKey: false,
      altKey: false,
      defaultPrevented: false,
      preventDefault() {
        prevented = true;
        this.defaultPrevented = true;
      },
      ...overrides,
    });
    await flushAsyncWork();
    assert.equal(prevented, false);
  }

  assert.equal(env.fetchCalls.length, 0);
});

test("navigation runtime consumes exact current-page link clicks without soft navigation", async () => {
  const link = new FakeElement("a", null);
  link.setAttribute("href", "/");
  link.setAttribute("data-gosx-link", "");
  link.setAttribute("data-gosx-prefetch", "intent");
  link.textContent = "Home";

  const stableBody = new FakeElement("main", null);
  stableBody.id = "stable-page";
  stableBody.textContent = "Home";

  const disposeCalls = [];
  const bootstrapCalls = [];
  const env = createContext({
    elements: [link, stableBody],
  });
  env.context.__gosx_dispose_page = async function() {
    disposeCalls.push("dispose");
  };
  env.context.__gosx_bootstrap_page = async function() {
    bootstrapCalls.push("bootstrap");
  };

  runScript(navigationSource, env.context, "navigation_runtime.js");

  const overListener = env.document.eventListeners.get("mouseover")[0];
  overListener({ type: "mouseover", target: link });
  await flushAsyncWork();

  let prevented = false;
  const clickListener = env.document.eventListeners.get("click")[0];
  clickListener({
    type: "click",
    target: link,
    button: 0,
    ctrlKey: false,
    metaKey: false,
    shiftKey: false,
    altKey: false,
    defaultPrevented: false,
    preventDefault() {
      prevented = true;
      this.defaultPrevented = true;
    },
  });
  await flushAsyncWork();

  assert.equal(prevented, true, "current managed link should not fall through to a native reload");
  assert.equal(env.fetchCalls.length, 0, "current managed link should not prefetch or fetch itself");
  assert.deepEqual(disposeCalls, []);
  assert.deepEqual(bootstrapCalls, []);
  assert.equal(env.document.getElementById("stable-page"), stableBody);
  assert.equal(env.document.dispatchedEvents.at(-1).type, "gosx:navigate");
  assert.equal(env.document.dispatchedEvents.at(-1).detail.url, "http://localhost:3000/");
  assert.equal(
    JSON.stringify(env.scrollCalls.at(-1)),
    JSON.stringify([{ top: 0, left: 0, behavior: "instant" }]),
  );
});

test("navigation runtime absolutizes managed asset URLs during navigation", async () => {
  const parsedDocs = new Map();
  const env = createContext({
    fetchRoutes: {
      "http://localhost:3000/docs/runtime/index.html": {
        text: "__ASSET_DOC__",
        url: "http://localhost:3000/docs/runtime/index.html",
      },
      "http://localhost:3000/docs/runtime/runtime.js": {
        text: "window.__navScriptLoaded = (window.__navScriptLoaded || 0) + 1;",
        url: "http://localhost:3000/docs/runtime/runtime.js",
      },
    },
    parseHTML(html) {
      return parsedDocs.get(html);
    },
  });

  env.context.location.href = "http://localhost:3000/docs/";
  env.context.__gosx_dispose_page = async function() {};
  env.context.__gosx_bootstrap_page = async function() {};

  const favicon = new FakeElement("link", null);
  favicon.setAttribute("rel", "icon");
  favicon.setAttribute("href", "./favicon.svg");

  const patchScript = new FakeElement("script", null);
  patchScript.setAttribute("data-gosx-script", "patch");
  patchScript.setAttribute("src", "./runtime.js");

  const image = new FakeElement("img", null);
  image.id = "hero";
  image.setAttribute("src", "./hero.png");
  image.setAttribute("srcset", "./hero.png 1x, ./hero@2x.png 2x");

  const form = new FakeElement("form", null);
  form.id = "signup";
  form.setAttribute("action", "./signup");

  const video = new FakeElement("video", null);
  video.id = "promo";
  video.setAttribute("poster", "./poster.jpg");

  parsedDocs.set("__ASSET_DOC__", buildNavigatedDocument({
    title: "Assets",
    headNodes: [favicon, patchScript],
    bodyNodes: [image, form, video],
  }));

  runScript(navigationSource, env.context, "navigation_runtime.js");
  await env.context.__gosx_page_nav.navigate("http://localhost:3000/docs/runtime/index.html");

  assert.equal(env.document.head.childNodes[1].getAttribute("href"), "http://localhost:3000/docs/runtime/favicon.svg");
  assert.equal(env.document.getElementById("hero").getAttribute("src"), "http://localhost:3000/docs/runtime/hero.png");
  assert.equal(
    env.document.getElementById("hero").getAttribute("srcset"),
    "http://localhost:3000/docs/runtime/hero.png 1x, http://localhost:3000/docs/runtime/hero@2x.png 2x",
  );
  assert.equal(env.document.getElementById("signup").getAttribute("action"), "http://localhost:3000/docs/runtime/signup");
  assert.equal(env.document.getElementById("promo").getAttribute("poster"), "http://localhost:3000/docs/runtime/poster.jpg");
  assert.equal(env.fetchCalls[1].url, "http://localhost:3000/docs/runtime/runtime.js");
  assert.equal(env.context.__navScriptLoaded, 1);
});

test("navigation runtime honors explicit a11y markers and hash targets", async () => {
  const parsedDocs = new Map();
  const env = createContext({
    fetchRoutes: {
      "http://localhost:3000/docs/a11y#details": {
        text: "__A11Y_DOC__",
        url: "http://localhost:3000/docs/a11y#details",
      },
    },
    parseHTML(html) {
      return parsedDocs.get(html);
    },
  });

  env.context.__gosx_dispose_page = async function() {};
  env.context.__gosx_bootstrap_page = async function() {};

  const main = new FakeElement("section", null);
  main.id = "main-shell";
  main.setAttribute("data-gosx-main", "");
  main.textContent = "Main shell";

  const announce = new FakeElement("p", null);
  announce.setAttribute("data-gosx-announce", "Accessibility docs");
  announce.textContent = "Ignored body copy";
  main.appendChild(announce);

  const target = new FakeElement("section", null);
  target.id = "details";
  target.textContent = "Deep details";
  main.appendChild(target);

  parsedDocs.set("__A11Y_DOC__", buildNavigatedDocument({
    title: "A11y",
    bodyNodes: [main],
  }));

  runScript(navigationSource, env.context, "navigation_runtime.js");
  await env.context.__gosx_page_nav.navigate("http://localhost:3000/docs/a11y#details");
  await flushAsyncWork();

  const renderedTarget = env.document.getElementById("details");
  assert.equal(env.document.activeElement, renderedTarget);
  assert.equal(renderedTarget.getAttribute("tabindex"), "-1");
  assert.equal(renderedTarget.scrollIntoViewCalls.length, 1);
  assert.equal(renderedTarget.scrollIntoViewCalls[0].length, 1);
  assert.equal(renderedTarget.scrollIntoViewCalls[0][0].behavior, "instant");
  assert.deepEqual(env.scrollCalls, []);
  assert.equal(env.document.body.childNodes.at(-1).textContent, "Accessibility docs");
  assert.equal(env.document.dispatchedEvents.at(-1).detail.announcement, "Accessibility docs");
  assert.equal(env.document.dispatchedEvents.at(-1).detail.focusTargetId, "details");
});

test("navigation runtime preserves scroll when requested and still focuses the target", async () => {
  const parsedDocs = new Map();
  const env = createContext({
    fetchRoutes: {
      "http://localhost:3000/docs/a11y#details": {
        text: "__PRESERVE_SCROLL_DOC__",
        url: "http://localhost:3000/docs/a11y#details",
      },
    },
    parseHTML(html) {
      return parsedDocs.get(html);
    },
  });

  env.context.__gosx_dispose_page = async function() {};
  env.context.__gosx_bootstrap_page = async function() {};

  const main = new FakeElement("section", null);
  main.id = "main-shell";
  main.setAttribute("data-gosx-main", "");
  main.textContent = "Main shell";

  const target = new FakeElement("section", null);
  target.id = "details";
  target.textContent = "Deep details";
  main.appendChild(target);

  parsedDocs.set("__PRESERVE_SCROLL_DOC__", buildNavigatedDocument({
    title: "Preserve Scroll",
    bodyNodes: [main],
  }));

  runScript(navigationSource, env.context, "navigation_runtime.js");
  await env.context.__gosx_page_nav.navigate("http://localhost:3000/docs/a11y#details", {
    preserveScroll: true,
    replace: true,
  });
  await flushAsyncWork();

  const renderedTarget = env.document.getElementById("details");
  assert.equal(renderedTarget.scrollIntoViewCalls.length, 0);
  assert.deepEqual(env.scrollCalls, []);
  assert.equal(env.document.activeElement, renderedTarget);
  assert.equal(env.document.dispatchedEvents.at(-1).type, "gosx:navigate");
  assert.equal(env.document.dispatchedEvents.at(-1).detail.replace, true);
  assert.equal(env.document.dispatchedEvents.at(-1).detail.focusTargetId, "details");
});

test("navigation runtime intercepts managed form submissions and forwards action data", async () => {
  const form = new FakeElement("form", null);
  form.setAttribute("action", "/save");
  form.setAttribute("method", "post");
  form.setAttribute("data-gosx-form", "");

  const input = new FakeElement("input", null);
  input.setAttribute("name", "title");
  input.value = "hello";
  form.appendChild(input);

  const submitter = new FakeElement("button", null);
  submitter.setAttribute("name", "intent");
  submitter.setAttribute("value", "publish");
  form.appendChild(submitter);

  const inputBatchCalls = [];
  const parsedDocs = new Map();
  const env = createContext({
    elements: [form],
    fetchRoutes: {
      "http://localhost:3000/save": {
        text: '{"data":{"$draft.title":"hello"},"redirect":"/done"}',
        url: "http://localhost:3000/save",
      },
      "http://localhost:3000/done": {
        text: "__DONE_DOC__",
        url: "http://localhost:3000/done",
      },
    },
    parseHTML(html) {
      return parsedDocs.get(html);
    },
  });
  env.context.__gosx_set_input_batch = function(payload) {
    inputBatchCalls.push(payload);
    return null;
  };
  env.context.__gosx_dispose_page = async function() {};
  env.context.__gosx_bootstrap_page = async function() {};

  const doneBody = new FakeElement("main", null);
  doneBody.id = "done";
  doneBody.textContent = "done";
  parsedDocs.set("__DONE_DOC__", buildNavigatedDocument({
    title: "Done",
    bodyNodes: [doneBody],
  }));
  const staleDone = Promise.resolve({ html: "__STALE_DONE__", url: "http://localhost:3000/done" });
  staleDone.__gosxCachedAt = Date.now();
  env.context.__gosx_page_cache = new Map([["http://localhost:3000/done", staleDone]]);
  const historyCalls = [];
  env.context.history = {
    pushState(_state, _title, url) {
      historyCalls.push(["push", String(url)]);
      env.context.location.href = String(url);
    },
    replaceState(_state, _title, url) {
      historyCalls.push(["replace", String(url)]);
      env.context.location.href = String(url);
    },
  };

  runScript(navigationSource, env.context, "navigation_runtime.js");

  const submitListener = env.document.eventListeners.get("submit")[0];
  let prevented = false;
  submitListener({
    type: "submit",
    target: form,
    submitter,
    defaultPrevented: false,
    preventDefault() {
      prevented = true;
      this.defaultPrevented = true;
    },
  });
  await flushAsyncWork();

  assert.equal(form.getAttribute("data-gosx-form-mode"), "post");
  assert.equal(prevented, true);
  assert.equal(env.fetchCalls[0].url, "http://localhost:3000/save");
  assert.equal(env.fetchCalls[0].init.method, "POST");
  assert.equal(env.fetchCalls[0].init.headers.Accept, "application/json");
  assert.equal(env.fetchCalls[0].init.body instanceof FakeFormData, true);
  assert.equal(env.fetchCalls[0].init.body.has("title"), true);
  assert.equal(env.fetchCalls[0].init.body.has("intent"), true);
  assert.deepEqual(inputBatchCalls, ['{"$draft.title":"hello"}']);
  assert.equal(env.fetchCalls[1].url, "http://localhost:3000/done");
  assert.notEqual(env.context.__gosx_page_cache.get("http://localhost:3000/done"), staleDone);
  assert.equal(env.context.location.href, "http://localhost:3000/done");
  assert.equal(env.document.getElementById("done").textContent, "done");
  assert.deepEqual(historyCalls, [["push", "http://localhost:3000/done"]]);
  assert.equal(env.scrollCalls.length, 1);
  assert.equal(env.document.dispatchedEvents.at(-1).type, "gosx:form:result");
  assert.equal(form.getAttribute("data-gosx-pending"), null);
  assert.equal(form.getAttribute("data-gosx-form-state"), "idle");
  assert.equal(form.parentNode, null);
});

test("managed action same-URL redirects force a cache-bypassing soft refresh", async () => {
  const pageURL = "http://localhost:3000/agenda";
  const actionURL = "http://localhost:3000/agenda/__actions/move";
  const form = new FakeElement("form", null);
  form.setAttribute("action", actionURL);
  form.setAttribute("method", "post");
  form.setAttribute("data-gosx-form", "");
  form.setAttribute("data-gosx-form-state", "idle");
  const sessionID = new FakeElement("input", null);
  sessionID.setAttribute("name", "session_id");
  sessionID.value = "s-1";
  form.appendChild(sessionID);
  const parsedDocs = new Map();
  const env = createContext({
    elements: [form],
    fetchRoutes: {
      [actionURL]: { text: '{"ok":true,"redirect":"/agenda"}', url: actionURL },
      [pageURL]: { text: "__FRESH_AGENDA_AFTER_MOVE__", url: pageURL },
    },
    parseHTML(html) { return parsedDocs.get(html); },
  });
  env.context.location.href = pageURL;
  env.context.__gosx_dispose_page = async function() {};
  env.context.__gosx_bootstrap_page = async function() {};
  const fresh = new FakeElement("main", null);
  fresh.id = "fresh-agenda-after-move";
  fresh.textContent = "Session moved";
  parsedDocs.set("__FRESH_AGENDA_AFTER_MOVE__", buildNavigatedDocument({
    title: "Agenda",
    bodyNodes: [fresh],
  }));
  const stale = Promise.resolve({ html: "__STALE_AGENDA__", url: pageURL });
  stale.__gosxCachedAt = Date.now();
  env.context.__gosx_page_cache = new Map([[pageURL, stale]]);
  const historyCalls = [];
  env.context.history = {
    pushState(_state, _title, url) {
      historyCalls.push(["push", String(url)]);
      env.context.location.href = String(url);
    },
    replaceState(_state, _title, url) {
      historyCalls.push(["replace", String(url)]);
      env.context.location.href = String(url);
    },
  };

  runScript(navigationSource, env.context, "navigation_runtime.js");
  env.document.eventListeners.get("submit")[0]({
    type: "submit",
    target: form,
    defaultPrevented: false,
    preventDefault() { this.defaultPrevented = true; },
  });
  await flushAsyncWork();

  assert.deepEqual(env.fetchCalls.map((call) => call.url), [actionURL, pageURL]);
  assert.notEqual(env.context.__gosx_page_cache.get(pageURL), stale);
  assert.equal(env.document.getElementById("fresh-agenda-after-move").textContent, "Session moved");
  assert.deepEqual(historyCalls, [["replace", pageURL]]);
  assert.deepEqual(env.scrollCalls, []);
  assert.equal(form.parentNode, null);
  assert.equal(form.getAttribute("data-gosx-form-state"), "idle");
});

test("accepted action redirects replace pre-mutation pending pages with a fresh fetch", async () => {
  class TestAbortSignal {
    constructor() {
      this.aborted = false;
      this.listeners = [];
    }

    addEventListener(type, listener) {
      if (type === "abort") this.listeners.push(listener);
    }
  }

  class TestAbortController {
    constructor() {
      this.signal = new TestAbortSignal();
    }

    abort() {
      if (this.signal.aborted) return;
      this.signal.aborted = true;
      for (const listener of this.signal.listeners) listener();
    }
  }

  for (const sameURL of [true, false]) {
    const label = sameURL ? "same URL" : "different URL";
    const currentURL = sameURL
      ? "http://localhost:3000/mutation-target"
      : "http://localhost:3000/mutation-current";
    const targetURL = "http://localhost:3000/mutation-target";
    const actionURL = "http://localhost:3000/__actions/mutate-" + (sameURL ? "same" : "different");
    const form = new FakeElement("form", null);
    form.setAttribute("action", actionURL);
    form.setAttribute("method", "post");
    form.setAttribute("data-gosx-form", "");
    let rejectStale;
    let staleSignal;
    let targetCalls = 0;
    const parsedDocs = new Map();
    const freshMain = new FakeElement("main", null);
    freshMain.id = "post-mutation-" + (sameURL ? "same" : "different");
    freshMain.textContent = "fresh after mutation";
    parsedDocs.set("__POST_MUTATION__", buildNavigatedDocument({
      title: "Fresh mutation",
      bodyNodes: [freshMain],
    }));
    const env = createContext({
      elements: [form],
      fetchRoutes: {
        [actionURL]: { text: '{"ok":true,"redirect":"/mutation-target"}', url: actionURL },
        [targetURL]: (_url, init) => {
          targetCalls += 1;
          if (targetCalls === 1) {
            staleSignal = init.signal;
            return new Promise((_resolve, reject) => { rejectStale = reject; });
          }
          return { text: "__POST_MUTATION__", url: targetURL };
        },
      },
      parseHTML(html) { return parsedDocs.get(html); },
    });
    env.context.AbortController = TestAbortController;
    env.context.location.href = currentURL;
    env.context.__gosx_dispose_page = async function() {};
    env.context.__gosx_bootstrap_page = async function() {};
    const historyCalls = [];
    env.context.history = {
      pushState(_state, _title, url) {
        historyCalls.push(["push", String(url)]);
        env.context.location.href = String(url);
      },
      replaceState(_state, _title, url) {
        historyCalls.push(["replace", String(url)]);
        env.context.location.href = String(url);
      },
    };

    runScript(navigationSource, env.context, "navigation_runtime.js");
    const stale = sameURL
      ? env.context.__gosx.navigation.revalidate()
      : env.context.__gosx.navigation.navigate(targetURL);
    await Promise.resolve();
    assert.equal(targetCalls, 1, label);

    env.document.eventListeners.get("submit")[0]({
      type: "submit",
      target: form,
      defaultPrevented: false,
      preventDefault() { this.defaultPrevented = true; },
    });
    await flushAsyncWork();

    assert.equal(staleSignal.aborted, true, label);
    assert.equal(targetCalls, 2, label);
    assert.deepEqual(
      env.fetchCalls.map((call) => [call.url, call.init.method || "GET"]),
      [[targetURL, "GET"], [actionURL, "POST"], [targetURL, "GET"]],
      label,
    );
    assert.equal(env.document.getElementById(freshMain.id).textContent, "fresh after mutation", label);
    assert.equal(env.context.__gosx.navigation.getState().phase, "idle", label);
    assert.deepEqual(historyCalls, [[sameURL ? "replace" : "push", targetURL]], label);

    const abortError = new Error("stale pre-mutation page rejected");
    abortError.name = "AbortError";
    rejectStale(abortError);
    assert.equal(await stale, false, label);
    assert.equal(env.context.__gosx_page_cache.has(targetURL), true, label);
    assert.equal(env.document.getElementById(freshMain.id).textContent, "fresh after mutation", label);
  }
});

test("action redirect fetch suppresses an older same-tab hub refresh echo", async () => {
  const pageURL = "http://localhost:3000/agenda";
  const actionURL = "http://localhost:3000/agenda/__actions/move";
  const form = new FakeElement("form", null);
  form.setAttribute("action", actionURL);
  form.setAttribute("method", "post");
  form.setAttribute("data-gosx-form", "");
  let emitHubEvent = function() {};
  const parsedDocs = new Map();
  const env = createContext({
    elements: [form],
    fetchRoutes: {
      [actionURL]() {
        emitHubEvent();
        return { text: '{"ok":true,"redirect":"/agenda"}', url: actionURL };
      },
      [pageURL]: { text: "__AGENDA_AFTER_ACTION__", url: pageURL },
    },
    parseHTML(html) { return parsedDocs.get(html); },
  });
  env.context.location.href = pageURL;
  env.context.__gosx_dispose_page = async function() {};
  env.context.__gosx_bootstrap_page = async function() {};
  const fresh = new FakeElement("main", null);
  fresh.id = "agenda-after-action";
  parsedDocs.set("__AGENDA_AFTER_ACTION__", buildNavigatedDocument({
    title: "Agenda",
    bodyNodes: [fresh],
  }));

  runScript(navigationSource, env.context, "navigation_runtime.js");
  env.context.__gosx.hubs = new Map();
  runScript(
    `(function(){${hubConnectionsSource}\nwindow.__test_applyHubBindings = applyHubBindings;})();`,
    env.context,
    "30c-tail-hub-connections.js",
  );
  const record = {
    entry: {
      id: "gosx-hub-action-echo",
      bindings: [{ event: "agenda.changed", refresh: true, refreshDebounceMs: 180 }],
    },
    socket: { close() {} },
  };
  env.context.__gosx.hubs.set(record.entry.id, record);
  let hubTimer = null;
  let hubDelay = null;
  let capturedEpoch = null;
  emitHubEvent = function() {
    const realSetTimeout = env.context.setTimeout;
    const realClearTimeout = env.context.clearTimeout;
    env.context.setTimeout = function(callback, delay) {
      hubTimer = callback;
      hubDelay = delay;
      return 701;
    };
    env.context.clearTimeout = function() {};
    env.context.__test_applyHubBindings(record, {
      event: "agenda.changed",
      data: { revision: 2 },
    });
    capturedEpoch = record.refreshFetchEpoch;
    env.context.setTimeout = realSetTimeout;
    env.context.clearTimeout = realClearTimeout;
  };

  env.document.eventListeners.get("submit")[0]({
    type: "submit",
    target: form,
    defaultPrevented: false,
    preventDefault() { this.defaultPrevented = true; },
  });
  await flushAsyncWork();

  assert.equal(capturedEpoch, 0);
  assert.equal(env.context.__gosx.navigation.getFetchEpoch().started, 1);
  assert.equal(env.context.__gosx.navigation.getFetchEpoch().applied, 1);
  assert.deepEqual(
    env.fetchCalls.map((call) => [call.url, call.init.method || "GET"]),
    [[actionURL, "POST"], [pageURL, "GET"]],
  );
  assert.equal(typeof hubTimer, "function");
  assert.equal(hubDelay, 180);
  hubTimer();
  await Promise.resolve();
  await Promise.resolve();
  assert.deepEqual(env.fetchCalls.map((call) => call.url), [actionURL, pageURL]);
});

test("Hub refresh waits for a real pending navigation before refreshing its settled page", async () => {
  for (const outcome of ["success", "failure"]) {
    const currentURL = "http://localhost:3000/";
    const pendingURL = "http://localhost:3000/pending-" + outcome;
    let settlePending;
    let pendingSignal;
    let pendingCalls = 0;
    const parsedDocs = new Map();
    const currentMain = new FakeElement("main", null);
    currentMain.id = "current-after-" + outcome;
    const pendingMain = new FakeElement("main", null);
    pendingMain.id = "pending-after-" + outcome;
    parsedDocs.set("__CURRENT_AFTER_PENDING__", buildNavigatedDocument({
      title: "Current",
      bodyNodes: [currentMain],
    }));
    parsedDocs.set("__PENDING_NAVIGATION__", buildNavigatedDocument({
      title: "Pending",
      bodyNodes: [pendingMain],
    }));
    const env = createContext({
      fetchRoutes: {
        [pendingURL]: (_url, init) => {
          pendingCalls += 1;
          if (pendingCalls > 1) return { text: "__PENDING_NAVIGATION__", url: pendingURL };
          pendingSignal = init.signal;
          return new Promise((resolve, reject) => {
            settlePending = outcome === "success"
              ? () => resolve({ text: "__PENDING_NAVIGATION__", url: pendingURL })
              : () => reject(new Error("pending navigation failed"));
          });
        },
        [currentURL]: { text: "__CURRENT_AFTER_PENDING__", url: currentURL },
      },
      parseHTML(html) { return parsedDocs.get(html); },
    });
    env.context.__gosx_dispose_page = async function() {};
    env.context.__gosx_bootstrap_page = async function() {};
    runScript(navigationSource, env.context, "navigation_runtime.js");
    env.context.__gosx.hubs = new Map();
    const timers = installManualTimers(env.context);
    runScript(
      `(function(){${hubConnectionsSource}\nwindow.__test_applyHubBindings = applyHubBindings;})();`,
      env.context,
      "30c-tail-hub-connections.js",
    );
    const record = {
      entry: {
        id: "gosx-hub-pending-" + outcome,
        bindings: [{ event: "changed", refresh: true }],
      },
      socket: { close() {} },
    };
    env.context.__gosx.hubs.set(record.entry.id, record);

    const pending = env.context.__gosx.navigation.navigate(pendingURL);
    await Promise.resolve();
    env.context.__test_applyHubBindings(record, { event: "changed", data: null });
    timers.runDelay(0);
    await Promise.resolve();

    assert.equal(pendingSignal.aborted, false, outcome);
    assert.equal(env.context.__gosx.navigation.getState().phase, "pending", outcome);
    assert.equal(timers.count(), 1, outcome);
    settlePending();
    if (outcome === "success") {
      assert.equal(await pending, true);
    } else {
      await assert.rejects(pending, /pending navigation failed/);
    }

    timers.runDelay(32);
    await flushAsyncWork();
    assert.equal(pendingSignal.aborted, false, outcome);
    assert.equal(env.context.__gosx.navigation.getState().phase, "idle", outcome);
    assert.equal(env.context.location.href, outcome === "success" ? pendingURL : currentURL);
    assert.deepEqual(
      env.fetchCalls.map((call) => call.url),
      outcome === "success" ? [pendingURL, pendingURL] : [pendingURL, currentURL],
      outcome,
    );
    assert.ok(env.document.getElementById(
      outcome === "success" ? "pending-after-success" : "current-after-failure",
    ));
  }
});

test("accepted action redirects never resubmit the POST when soft navigation fails", async () => {
  const cases = [
    {
      name: "different URL status failure",
      pageURL: "http://localhost:3000/current",
      actionURL: "http://localhost:3000/save",
      redirect: "/done",
      pageRoute: {
        ok: false,
        status: 503,
        text: "unavailable",
        url: "http://localhost:3000/done",
      },
      hardMethod: "assign",
      hardURL: "http://localhost:3000/done",
    },
    {
      name: "same URL transport failure",
      pageURL: "http://localhost:3000/agenda",
      actionURL: "http://localhost:3000/agenda/__actions/move",
      redirect: "/agenda",
      pageRoute() {
        throw new Error("page transport failed");
      },
      hardMethod: "replace",
      hardURL: "http://localhost:3000/agenda",
    },
  ];

  for (const scenario of cases) {
    const form = new FakeElement("form", null);
    form.setAttribute("action", scenario.actionURL);
    form.setAttribute("method", "post");
    form.setAttribute("data-gosx-form", "");
    const fetchRoutes = {
      [scenario.actionURL]: {
        text: JSON.stringify({ ok: true, redirect: scenario.redirect }),
        url: scenario.actionURL,
      },
      [scenario.hardURL]: scenario.pageRoute,
    };
    const env = createContext({ elements: [form], fetchRoutes });
    env.context.location.href = scenario.pageURL;
    const hardNavigations = [];
    env.context.location.assign = function(url) {
      hardNavigations.push(["assign", String(url)]);
    };
    env.context.location.replace = function(url) {
      hardNavigations.push(["replace", String(url)]);
    };

    runScript(navigationSource, env.context, "navigation_runtime.js");
    env.document.eventListeners.get("submit")[0]({
      type: "submit",
      target: form,
      defaultPrevented: false,
      preventDefault() { this.defaultPrevented = true; },
    });
    await flushAsyncWork();

    assert.deepEqual(
      env.fetchCalls.map((call) => [call.url, call.init.method || "GET"]),
      [[scenario.actionURL, "POST"], [scenario.hardURL, "GET"]],
      scenario.name,
    );
    assert.equal(form.requestSubmitCalls.length, 0, scenario.name);
    assert.equal(form.submitCalls.length, 0, scenario.name);
    assert.deepEqual(hardNavigations, [[scenario.hardMethod, scenario.hardURL]], scenario.name);
  }
});

test("accepted actions block malformed and unsafe redirect targets without native fallback", async () => {
  const redirects = [
    "https://attacker.example/steal",
    "javascript:alert(1)",
    "http://[invalid",
  ];

  for (const redirect of redirects) {
    const actionURL = "http://localhost:3000/save";
    const form = new FakeElement("form", null);
    form.setAttribute("action", actionURL);
    form.setAttribute("method", "post");
    form.setAttribute("data-gosx-form", "");
    const env = createContext({
      elements: [form],
      fetchRoutes: {
        [actionURL]: { text: JSON.stringify({ ok: true, redirect }), url: actionURL },
      },
    });
    const hardNavigations = [];
    env.context.location.assign = function(url) { hardNavigations.push(["assign", String(url)]); };
    env.context.location.replace = function(url) { hardNavigations.push(["replace", String(url)]); };

    runScript(navigationSource, env.context, "navigation_runtime.js");
    env.document.eventListeners.get("submit")[0]({
      type: "submit",
      target: form,
      defaultPrevented: false,
      preventDefault() { this.defaultPrevented = true; },
    });
    await flushAsyncWork();

    assert.deepEqual(env.fetchCalls.map((call) => call.url), [actionURL], redirect);
    assert.equal(form.requestSubmitCalls.length, 0, redirect);
    assert.equal(form.submitCalls.length, 0, redirect);
    assert.deepEqual(hardNavigations, [], redirect);
  }
});

test("action POST transport failure retains one native submission fallback", async () => {
  const actionURL = "http://localhost:3000/save";
  const form = new FakeElement("form", null);
  form.setAttribute("action", actionURL);
  form.setAttribute("method", "post");
  form.setAttribute("data-gosx-form", "");
  const submitter = new FakeElement("button", null);
  form.appendChild(submitter);
  const env = createContext({
    elements: [form],
    fetchRoutes: {
      [actionURL]() { throw new Error("action transport failed"); },
    },
  });
  const hardNavigations = [];
  env.context.location.assign = function(url) { hardNavigations.push(["assign", String(url)]); };
  env.context.location.replace = function(url) { hardNavigations.push(["replace", String(url)]); };

  runScript(navigationSource, env.context, "navigation_runtime.js");
  env.document.eventListeners.get("submit")[0]({
    type: "submit",
    target: form,
    submitter,
    defaultPrevented: false,
    preventDefault() { this.defaultPrevented = true; },
  });
  await flushAsyncWork();

  assert.deepEqual(env.fetchCalls.map((call) => call.url), [actionURL]);
  assert.equal(form.requestSubmitCalls.length, 1);
  assert.equal(form.requestSubmitCalls[0][0], submitter);
  assert.equal(form.submitCalls.length, 0);
  assert.deepEqual(hardNavigations, []);
});

test("navigation runtime exposes programmatic managed action submission", async () => {
  const main = new FakeElement("main", null);
  main.setAttribute("data-gosx-main", "");
  main.setAttribute("data-gosx-csrf-token", "root-token");

  const env = createContext({
    elements: [main],
    fetchRoutes: {
      "http://localhost:3000/play/__actions/pilot": {
        text: '{"ok":true}',
        url: "http://localhost:3000/play/__actions/pilot",
      },
    },
  });

  runScript(navigationSource, env.context, "navigation_runtime.js");

  assert.equal(typeof env.context.__gosx_submit_action, "function");
  assert.equal(typeof env.context.__gosx_page_nav.submitAction, "function");

  const form = env.context.__gosx_submit_action("/play/__actions/pilot", {
    unitId: "robot-1",
    mode: "manual",
  }, { root: main, keepForm: true });
  await form.__gosxSubmitPromise;

  assert.equal(form.parentNode, main);
  assert.equal(form.getAttribute("action"), "/play/__actions/pilot");
  assert.equal(form.getAttribute("method"), "post");
  assert.equal(form.getAttribute("data-gosx-form"), "");
  assert.equal(form.getAttribute("data-gosx-form-state"), "success");
  assert.equal(env.fetchCalls[0].url, "http://localhost:3000/play/__actions/pilot");
  assert.equal(env.fetchCalls[0].init.method, "POST");
  assert.equal(env.fetchCalls[0].init.headers["X-CSRF-Token"], "root-token");
  assert.deepEqual(env.fetchCalls[0].init.body.values, [
    ["unitId", "robot-1"],
    ["mode", "manual"],
    ["csrf_token", "root-token"],
  ]);
  assert.equal(env.document.dispatchedEvents.at(-1).type, "gosx:form:result");
});

test("managed forms suppress duplicate submissions until their request settles", async () => {
  const actionURL = "http://localhost:3000/save-once";
  const form = new FakeElement("form", null);
  form.setAttribute("action", actionURL);
  form.setAttribute("method", "post");
  form.setAttribute("data-gosx-form", "");
  const status = new FakeElement("p", null);
  status.setAttribute("class", "form-status");
  form.appendChild(status);
  let resolveFirst;
  let calls = 0;
  const env = createContext({
    elements: [form],
    fetchRoutes: {
      [actionURL]: () => {
        calls += 1;
        if (calls === 1) {
          return new Promise((resolve) => { resolveFirst = resolve; });
        }
        return { text: '{"ok":true,"message":"Saved again."}', url: actionURL };
      },
    },
  });
  runScript(navigationSource, env.context, "navigation_runtime.js");
  const submit = env.document.eventListeners.get("submit")[0];
  const event = () => ({
    type: "submit",
    target: form,
    defaultPrevented: false,
    preventDefault() { this.defaultPrevented = true; },
  });
  const firstEvent = event();
  const duplicateEvent = event();

  submit(firstEvent);
  submit(duplicateEvent);
  await Promise.resolve();
  assert.equal(firstEvent.defaultPrevented, true);
  assert.equal(duplicateEvent.defaultPrevented, true);
  assert.equal(calls, 1);
  assert.equal(form.getAttribute("data-gosx-form-state"), "pending");

  resolveFirst({ text: '{"ok":true,"message":"Saved once."}', url: actionURL });
  await flushAsyncWork();
  assert.equal(calls, 1);
  assert.equal(status.textContent, "Saved once.");
  assert.equal(form.getAttribute("data-gosx-form-state"), "success");
  assert.equal(form.requestSubmitCalls.length, 0);
  assert.equal(form.submitCalls.length, 0);

  submit(event());
  await flushAsyncWork();
  assert.equal(calls, 2, "the guard clears after settlement");
  assert.equal(status.textContent, "Saved again.");
});

test("programmatic hidden action forms ignore duplicate submit events while pending", async () => {
  const actionURL = "http://localhost:3000/play/__actions/once";
  const main = new FakeElement("main", null);
  main.setAttribute("data-gosx-main", "");
  let resolveAction;
  let calls = 0;
  const env = createContext({
    elements: [main],
    fetchRoutes: {
      [actionURL]: () => {
        calls += 1;
        return new Promise((resolve) => { resolveAction = resolve; });
      },
    },
  });
  runScript(navigationSource, env.context, "navigation_runtime.js");
  const form = env.context.__gosx_submit_action(actionURL, { id: "one" }, {
    root: main,
    keepForm: true,
  });
  env.document.eventListeners.get("submit")[0]({
    type: "submit",
    target: form,
    defaultPrevented: false,
    preventDefault() { this.defaultPrevented = true; },
  });
  await Promise.resolve();

  assert.equal(form.hidden, true);
  assert.equal(calls, 1);
  resolveAction({ text: '{"ok":true}', url: actionURL });
  await form.__gosxSubmitPromise;
  assert.equal(calls, 1);
  assert.equal(form.getAttribute("data-gosx-form-state"), "success");
});

test("managed action results clear stale form errors and project success locally", async () => {
  const form = new FakeElement("form", null);
  form.setAttribute("action", "/profile");
  form.setAttribute("method", "post");
  form.setAttribute("data-gosx-form", "");
  form.setAttribute("data-gosx-form-state", "idle");
  const name = new FakeElement("input", null);
  name.setAttribute("name", "name");
  name.setAttribute("aria-invalid", "true");
  name.setAttribute("aria-describedby", "name-error");
  const error = new FakeElement("p", null);
  error.id = "name-error";
  error.setAttribute("class", "form-error");
  error.textContent = "Stale error";
  const status = new FakeElement("p", null);
  status.setAttribute("class", "action-message");
  status.textContent = "Old status";
  form.appendChild(name);
  form.appendChild(error);
  form.appendChild(status);

  const env = createContext({
    elements: [form],
    fetchRoutes: {
      "http://localhost:3000/profile": {
        text: '{"ok":true,"message":"Profile saved."}',
        url: "http://localhost:3000/profile",
      },
    },
  });
  runScript(navigationSource, env.context, "navigation_runtime.js");
  env.document.eventListeners.get("submit")[0]({
    type: "submit",
    target: form,
    defaultPrevented: false,
    preventDefault() { this.defaultPrevented = true; },
  });
  await flushAsyncWork();

  assert.equal(form.getAttribute("data-gosx-form-state"), "success");
  assert.equal(name.hasAttribute("aria-invalid"), false);
  assert.equal(error.textContent, "");
  assert.equal(status.textContent, "Profile saved.");
  assert.equal(env.document.querySelector("[data-gosx-announcer]").textContent, "Profile saved.");
});

test("managed validation projects described field errors, focuses first invalid, and respects reduced motion", async () => {
  const form = new FakeElement("form", null);
  form.setAttribute("action", "/register");
  form.setAttribute("method", "post");
  form.setAttribute("data-gosx-form", "");
  const sessionID = new FakeElement("input", null);
  sessionID.setAttribute("type", "hidden");
  sessionID.setAttribute("name", "session_id");
  sessionID.setAttribute("aria-describedby", "session-error");
  const sessionError = new FakeElement("p", null);
  sessionError.id = "session-error";
  sessionError.setAttribute("class", "form-error");
  const disabled = new FakeElement("input", null);
  disabled.setAttribute("name", "locked");
  disabled.setAttribute("disabled", "");
  disabled.setAttribute("aria-describedby", "locked-error");
  const disabledError = new FakeElement("p", null);
  disabledError.id = "locked-error";
  disabledError.setAttribute("class", "form-error");
  const hiddenGroup = new FakeElement("div", null);
  hiddenGroup.setAttribute("hidden", "");
  const hiddenChild = new FakeElement("input", null);
  hiddenChild.setAttribute("name", "hidden_child");
  hiddenChild.setAttribute("aria-describedby", "hidden-child-error");
  const hiddenChildError = new FakeElement("p", null);
  hiddenChildError.id = "hidden-child-error";
  hiddenChildError.setAttribute("class", "form-error");
  hiddenGroup.appendChild(hiddenChild);
  hiddenGroup.appendChild(hiddenChildError);
  const email = new FakeElement("input", null);
  email.setAttribute("name", "email");
  email.setAttribute("aria-describedby", "email-error email-help");
  const emailError = new FakeElement("p", null);
  emailError.id = "email-error";
  emailError.setAttribute("class", "form-error");
  const phone = new FakeElement("input", null);
  phone.setAttribute("name", "phone");
  phone.setAttribute("aria-invalid", "true");
  phone.setAttribute("aria-describedby", "phone-error");
  const phoneError = new FakeElement("p", null);
  phoneError.id = "phone-error";
  phoneError.setAttribute("class", "form-error");
  phoneError.textContent = "Stale phone error";
  const status = new FakeElement("p", null);
  status.setAttribute("class", "form-status");
  form.appendChild(sessionID);
  form.appendChild(sessionError);
  form.appendChild(disabled);
  form.appendChild(disabledError);
  form.appendChild(hiddenGroup);
  form.appendChild(email);
  form.appendChild(emailError);
  form.appendChild(phone);
  form.appendChild(phoneError);
  form.appendChild(status);

  const env = createContext({
    elements: [form],
    prefersReducedMotion: true,
    fetchRoutes: {
      "http://localhost:3000/register": {
        ok: false,
        status: 422,
        text: '{"ok":false,"message":"Check the highlighted fields.","fieldErrors":{"session_id":"Missing session.","locked":"Locked.","hidden_child":"Hidden.","email":"Use a valid email."}}',
        url: "http://localhost:3000/register",
      },
    },
  });
  runScript(navigationSource, env.context, "navigation_runtime.js");
  env.document.eventListeners.get("submit")[0]({
    type: "submit",
    target: form,
    defaultPrevented: false,
    preventDefault() { this.defaultPrevented = true; },
  });
  await flushAsyncWork();

  assert.equal(form.getAttribute("data-gosx-form-state"), "error");
  assert.equal(sessionID.getAttribute("aria-invalid"), "true");
  assert.equal(sessionError.textContent, "Missing session.");
  assert.equal(sessionID.focusCalls.length, 0);
  assert.equal(disabled.focusCalls.length, 0);
  assert.equal(hiddenChild.focusCalls.length, 0);
  assert.equal(email.getAttribute("aria-invalid"), "true");
  assert.equal(emailError.textContent, "Use a valid email.");
  assert.equal(phone.hasAttribute("aria-invalid"), false);
  assert.equal(phoneError.textContent, "");
  assert.equal(status.textContent, "Check the highlighted fields.");
  assert.equal(env.document.activeElement, email);
  assert.equal(email.scrollIntoViewCalls.length, 1);
  assert.equal(email.scrollIntoViewCalls[0][0].behavior, "auto");
  assert.equal(email.scrollIntoViewCalls[0][0].block, "center");
});

test("managed validation focuses invalid controls in form DOM order", async () => {
  const form = new FakeElement("form", null);
  form.setAttribute("action", "/focus-order");
  form.setAttribute("method", "post");
  form.setAttribute("data-gosx-form", "");
  const first = new FakeElement("input", null);
  first.setAttribute("name", "first");
  const firstError = new FakeElement("p", null);
  firstError.setAttribute("class", "form-error");
  firstError.setAttribute("data-gosx-field-error", "first");
  const second = new FakeElement("input", null);
  second.setAttribute("name", "second");
  const secondError = new FakeElement("p", null);
  secondError.setAttribute("class", "form-error");
  secondError.setAttribute("data-gosx-field-error", "second");
  form.appendChild(first);
  form.appendChild(firstError);
  form.appendChild(second);
  form.appendChild(secondError);
  const env = createContext({
    elements: [form],
    fetchRoutes: {
      "http://localhost:3000/focus-order": {
        ok: false,
        status: 422,
        text: '{"ok":false,"fieldErrors":{"second":"Second error.","first":"First error."}}',
        url: "http://localhost:3000/focus-order",
      },
    },
  });

  runScript(navigationSource, env.context, "navigation_runtime.js");
  env.document.eventListeners.get("submit")[0]({
    type: "submit",
    target: form,
    defaultPrevented: false,
    preventDefault() { this.defaultPrevented = true; },
  });
  await flushAsyncWork();

  assert.equal(first.getAttribute("aria-invalid"), "true");
  assert.equal(second.getAttribute("aria-invalid"), "true");
  assert.equal(env.document.activeElement, first);
  assert.equal(first.focusCalls.length, 1);
  assert.equal(second.focusCalls.length, 0);
});

test("managed validation removes only framework-added error descriptions between results", async () => {
  const form = new FakeElement("form", null);
  form.setAttribute("action", "/reuse-error");
  form.setAttribute("method", "post");
  form.setAttribute("data-gosx-form", "");
  const first = new FakeElement("input", null);
  first.setAttribute("name", "first");
  first.setAttribute("aria-describedby", "first-help");
  const help = new FakeElement("p", null);
  help.id = "first-help";
  const second = new FakeElement("input", null);
  second.setAttribute("name", "second");
  const reusedError = new FakeElement("p", null);
  reusedError.setAttribute("class", "form-error");
  form.appendChild(first);
  form.appendChild(help);
  form.appendChild(second);
  form.appendChild(reusedError);
  let calls = 0;
  const env = createContext({
    elements: [form],
    fetchRoutes: {
      "http://localhost:3000/reuse-error": () => {
        calls += 1;
        return {
          ok: false,
          status: 422,
          text: calls === 1
            ? '{"ok":false,"fieldErrors":{"first":"First error."}}'
            : '{"ok":false,"fieldErrors":{"second":"Second error."}}',
          url: "http://localhost:3000/reuse-error",
        };
      },
    },
  });

  runScript(navigationSource, env.context, "navigation_runtime.js");
  const submit = env.document.eventListeners.get("submit")[0];
  const submitForm = () => submit({
    type: "submit",
    target: form,
    defaultPrevented: false,
    preventDefault() { this.defaultPrevented = true; },
  });
  submitForm();
  await flushAsyncWork();
  const errorID = reusedError.id;
  assert.match(errorID, /^gosx-form-error-/);
  assert.equal(first.getAttribute("aria-describedby"), "first-help " + errorID);
  assert.equal(second.hasAttribute("aria-describedby"), false);

  submitForm();
  await flushAsyncWork();
  assert.equal(first.getAttribute("aria-describedby"), "first-help");
  assert.equal(first.hasAttribute("aria-invalid"), false);
  assert.equal(second.getAttribute("aria-describedby"), errorID);
  assert.equal(second.getAttribute("aria-invalid"), "true");
  assert.equal(reusedError.textContent, "Second error.");
});

test("managed validation marks and describes every radio in a field group", async () => {
  const form = new FakeElement("form", null);
  form.setAttribute("action", "/preferences");
  form.setAttribute("method", "post");
  form.setAttribute("data-gosx-form", "");
  const compact = new FakeElement("input", null);
  compact.setAttribute("type", "radio");
  compact.setAttribute("name", "layout");
  compact.setAttribute("value", "compact");
  const spacious = new FakeElement("input", null);
  spacious.setAttribute("type", "radio");
  spacious.setAttribute("name", "layout");
  spacious.setAttribute("value", "spacious");
  const error = new FakeElement("p", null);
  error.setAttribute("class", "form-error");
  error.setAttribute("data-gosx-field-error", "layout");
  form.appendChild(compact);
  form.appendChild(spacious);
  form.appendChild(error);
  const env = createContext({
    elements: [form],
    fetchRoutes: {
      "http://localhost:3000/preferences": {
        ok: false,
        status: 422,
        text: '{"ok":false,"fieldErrors":{"layout":"Choose a layout."}}',
        url: "http://localhost:3000/preferences",
      },
    },
  });

  runScript(navigationSource, env.context, "navigation_runtime.js");
  env.document.eventListeners.get("submit")[0]({
    type: "submit",
    target: form,
    defaultPrevented: false,
    preventDefault() { this.defaultPrevented = true; },
  });
  await flushAsyncWork();

  assert.equal(error.textContent, "Choose a layout.");
  assert.match(error.id, /^gosx-form-error-/);
  for (const radio of [compact, spacious]) {
    assert.equal(radio.getAttribute("aria-invalid"), "true");
    assert.equal(radio.getAttribute("aria-describedby"), error.id);
  }
  assert.equal(env.document.activeElement, compact);
  assert.equal(compact.scrollIntoViewCalls.length, 1);
  assert.equal(spacious.focusCalls.length, 0);
});

test("managed validation skips hidden or disabled first group members when focusing", async () => {
  const scenarios = [
    {
      label: "hidden first",
      slug: "hidden-first",
      configure(control) { control.setAttribute("type", "hidden"); },
    },
    {
      label: "disabled first",
      slug: "disabled-first",
      configure(control) { control.setAttribute("disabled", ""); },
    },
  ];

  for (const scenario of scenarios) {
    const actionURL = "http://localhost:3000/" + scenario.slug;
    const form = new FakeElement("form", null);
    form.setAttribute("action", "/" + scenario.slug);
    form.setAttribute("method", "post");
    form.setAttribute("data-gosx-form", "");
    const first = new FakeElement("input", null);
    first.setAttribute("name", "choice");
    scenario.configure(first);
    const focusable = new FakeElement("input", null);
    focusable.setAttribute("type", "checkbox");
    focusable.setAttribute("name", "choice");
    const error = new FakeElement("p", null);
    error.setAttribute("class", "form-error");
    error.setAttribute("data-gosx-field-error", "choice");
    form.appendChild(first);
    form.appendChild(focusable);
    form.appendChild(error);
    const env = createContext({
      elements: [form],
      fetchRoutes: {
        [actionURL]: {
          ok: false,
          status: 422,
          text: '{"ok":false,"fieldErrors":{"choice":"Choose an option."}}',
          url: actionURL,
        },
      },
    });

    runScript(navigationSource, env.context, "navigation_runtime.js");
    env.document.eventListeners.get("submit")[0]({
      type: "submit",
      target: form,
      defaultPrevented: false,
      preventDefault() { this.defaultPrevented = true; },
    });
    await flushAsyncWork();

    assert.equal(first.getAttribute("aria-invalid"), "true", scenario.label);
    assert.equal(focusable.getAttribute("aria-invalid"), "true", scenario.label);
    assert.equal(first.getAttribute("aria-describedby"), error.id, scenario.label);
    assert.equal(focusable.getAttribute("aria-describedby"), error.id, scenario.label);
    assert.equal(first.focusCalls.length, 0, scenario.label);
    assert.equal(env.document.activeElement, focusable, scenario.label);
    assert.equal(focusable.scrollIntoViewCalls.length, 1, scenario.label);
  }
});

test("concurrent managed forms project only into their submitted form", async () => {
  function formFixture(action) {
    const form = new FakeElement("form", null);
    form.setAttribute("action", action);
    form.setAttribute("method", "post");
    form.setAttribute("data-gosx-form", "");
    const status = new FakeElement("p", null);
    status.setAttribute("class", "form-status");
    form.appendChild(status);
    return { form, status };
  }
  const first = formFixture("/first");
  const second = formFixture("/second");
  const pending = new Map();
  const env = createContext({
    elements: [first.form, second.form],
    fetchRoutes: {
      "http://localhost:3000/first": () => new Promise((resolve) => pending.set("first", resolve)),
      "http://localhost:3000/second": () => new Promise((resolve) => pending.set("second", resolve)),
    },
  });
  runScript(navigationSource, env.context, "navigation_runtime.js");
  const submit = env.document.eventListeners.get("submit")[0];
  for (const form of [first.form, second.form]) {
    submit({
      type: "submit",
      target: form,
      defaultPrevented: false,
      preventDefault() { this.defaultPrevented = true; },
    });
  }
  await Promise.resolve();
  assert.equal(first.form.getAttribute("data-gosx-form-state"), "pending");
  assert.equal(second.form.getAttribute("data-gosx-form-state"), "pending");

  pending.get("second")({ text: '{"ok":true,"message":"Second saved."}' });
  await flushAsyncWork();
  assert.equal(second.status.textContent, "Second saved.");
  assert.equal(second.form.getAttribute("data-gosx-form-state"), "success");
  assert.equal(first.status.textContent, "");
  assert.equal(first.form.getAttribute("data-gosx-form-state"), "pending");

  pending.get("first")({ text: '{"ok":true,"message":"First saved."}' });
  await flushAsyncWork();
  assert.equal(first.status.textContent, "First saved.");
  assert.equal(first.form.getAttribute("data-gosx-form-state"), "success");
  assert.equal(second.status.textContent, "Second saved.");
});

test("managed validation safely handles malicious and missing field names without crossing form boundaries", async () => {
  const maliciousName = 'email\"] [data-pwned="true';
  const form = new FakeElement("form", null);
  form.setAttribute("action", "/unsafe-name");
  form.setAttribute("method", "post");
  form.setAttribute("data-gosx-form", "");
  const control = new FakeElement("input", null);
  control.setAttribute("name", maliciousName);
  const error = new FakeElement("p", null);
  error.setAttribute("class", "form-error");
  error.setAttribute("data-gosx-field-error", maliciousName);
  form.appendChild(control);
  form.appendChild(error);

  const unrelated = new FakeElement("form", null);
  unrelated.setAttribute("data-gosx-form", "");
  const unrelatedError = new FakeElement("p", null);
  unrelatedError.setAttribute("class", "form-error");
  unrelatedError.textContent = "Keep me";
  unrelated.appendChild(unrelatedError);

  const env = createContext({
    elements: [form, unrelated],
    fetchRoutes: {
      "http://localhost:3000/unsafe-name": {
        ok: false,
        status: 422,
        text: JSON.stringify({
          ok: false,
          message: "Invalid.",
          fieldErrors: { [maliciousName]: "Rejected safely.", missing: "No matching control." },
        }),
        url: "http://localhost:3000/unsafe-name",
      },
    },
  });
  runScript(navigationSource, env.context, "navigation_runtime.js");
  env.document.eventListeners.get("submit")[0]({
    type: "submit",
    target: form,
    defaultPrevented: false,
    preventDefault() { this.defaultPrevented = true; },
  });
  await flushAsyncWork();

  assert.equal(form.getAttribute("data-gosx-form-state"), "error");
  assert.equal(control.getAttribute("aria-invalid"), "true");
  assert.equal(error.textContent, "Rejected safely.");
  assert.match(error.id, /^gosx-form-error-/);
  assert.equal(control.getAttribute("aria-describedby"), error.id);
  assert.equal(unrelatedError.textContent, "Keep me");
  assert.equal(unrelated.getAttribute("data-gosx-form-state"), "idle");
});

test("managed form result projection can be explicitly disabled", async () => {
  const form = new FakeElement("form", null);
  form.setAttribute("action", "/custom-projection");
  form.setAttribute("method", "post");
  form.setAttribute("data-gosx-form", "");
  form.setAttribute("data-gosx-form-state", "idle");
  form.setAttribute("data-gosx-form-project", "off");
  const status = new FakeElement("p", null);
  status.setAttribute("class", "form-status");
  status.textContent = "Island-owned";
  form.appendChild(status);
  const env = createContext({
    elements: [form],
    fetchRoutes: {
      "http://localhost:3000/custom-projection": {
        text: '{"ok":true,"message":"Framework result"}',
        url: "http://localhost:3000/custom-projection",
      },
    },
  });
  runScript(navigationSource, env.context, "navigation_runtime.js");
  env.document.eventListeners.get("submit")[0]({
    type: "submit",
    target: form,
    defaultPrevented: false,
    preventDefault() { this.defaultPrevented = true; },
  });
  await flushAsyncWork();

  assert.equal(form.getAttribute("data-gosx-form-state"), "idle");
  assert.equal(status.textContent, "Island-owned");
  assert.equal(env.document.querySelector("[data-gosx-announcer]"), null);
});

test("managed form result projection skips a form detached while its action is pending", async () => {
  const form = new FakeElement("form", null);
  form.setAttribute("action", "/detached");
  form.setAttribute("method", "post");
  form.setAttribute("data-gosx-form", "");
  form.setAttribute("data-gosx-form-state", "idle");
  const status = new FakeElement("p", null);
  status.setAttribute("class", "form-status");
  status.textContent = "Keep detached state";
  form.appendChild(status);
  let resolveAction;
  const env = createContext({
    elements: [form],
    fetchRoutes: {
      "http://localhost:3000/detached": () => new Promise((resolve) => { resolveAction = resolve; }),
    },
  });
  runScript(navigationSource, env.context, "navigation_runtime.js");
  env.document.eventListeners.get("submit")[0]({
    type: "submit",
    target: form,
    defaultPrevented: false,
    preventDefault() { this.defaultPrevented = true; },
  });
  await Promise.resolve();
  form.parentNode.removeChild(form);
  resolveAction({ text: '{"ok":true,"message":"Do not project."}' });
  await flushAsyncWork();

  assert.equal(form.parentNode, null);
  assert.equal(form.getAttribute("data-gosx-form-state"), "idle");
  assert.equal(status.textContent, "Keep detached state");
  assert.equal(env.document.querySelector("[data-gosx-announcer]"), null);
});

test("navigation runtime intercepts managed GET forms and navigates with query params", async () => {
  const form = new FakeElement("form", null);
  form.setAttribute("action", "/search");
  form.setAttribute("method", "get");
  form.setAttribute("data-gosx-form", "");
  form.setAttribute("data-gosx-form-state", "idle");

  const query = new FakeElement("input", null);
  query.setAttribute("name", "q");
  query.value = "scene labels";
  form.appendChild(query);

  const submitter = new FakeElement("button", null);
  submitter.setAttribute("name", "view");
  submitter.setAttribute("value", "list");
  form.appendChild(submitter);

  const parsedDocs = new Map();
  const env = createContext({
    elements: [form],
    fetchRoutes: {
      "http://localhost:3000/search?q=scene+labels&view=list": {
        text: "__SEARCH_DOC__",
        url: "http://localhost:3000/search?q=scene+labels&view=list",
      },
    },
    parseHTML(html) {
      return parsedDocs.get(html);
    },
  });
  env.context.__gosx_dispose_page = async function() {};
  env.context.__gosx_bootstrap_page = async function() {};

  const results = new FakeElement("main", null);
  results.id = "results";
  results.textContent = "results";
  parsedDocs.set("__SEARCH_DOC__", buildNavigatedDocument({
    title: "Search",
    bodyNodes: [results],
  }));

  runScript(navigationSource, env.context, "navigation_runtime.js");

  const submitListener = env.document.eventListeners.get("submit")[0];
  let prevented = false;
  submitListener({
    type: "submit",
    target: form,
    submitter,
    defaultPrevented: false,
    preventDefault() {
      prevented = true;
      this.defaultPrevented = true;
    },
  });
  await flushAsyncWork();

  assert.equal(form.getAttribute("data-gosx-form-mode"), "get");
  assert.equal(prevented, true);
  assert.equal(env.fetchCalls[0].url, "http://localhost:3000/search?q=scene+labels&view=list");
  assert.equal(env.fetchCalls[0].init.headers.Accept, "text/html");
  assert.equal(env.context.location.href, "http://localhost:3000/search?q=scene+labels&view=list");
  assert.equal(env.document.dispatchedEvents.at(-1).type, "gosx:form:navigate");
  assert.equal(env.document.dispatchedEvents.at(-1).detail.method, "GET");
  assert.equal(form.getAttribute("data-gosx-pending"), null);
  assert.equal(form.getAttribute("data-gosx-form-state"), "idle");
});

test("navigation runtime restores prior managed form lifecycle attrs after submit", async () => {
  const form = new FakeElement("form", null);
  form.setAttribute("action", "/search");
  form.setAttribute("method", "get");
  form.setAttribute("data-gosx-form", "");
  form.setAttribute("data-gosx-form-state", "validating");
  form.setAttribute("data-gosx-pending", "queued");

  const query = new FakeElement("input", null);
  query.setAttribute("name", "q");
  query.value = "scene labels";
  form.appendChild(query);

  const submitter = new FakeElement("button", null);
  submitter.setAttribute("name", "view");
  submitter.setAttribute("value", "list");
  form.appendChild(submitter);

  const parsedDocs = new Map();
  const env = createContext({
    elements: [form],
    fetchRoutes: {
      "http://localhost:3000/search?q=scene+labels&view=list": {
        text: "__RESTORE_FORM_DOC__",
        url: "http://localhost:3000/search?q=scene+labels&view=list",
      },
    },
    parseHTML(html) {
      return parsedDocs.get(html);
    },
  });
  env.context.__gosx_dispose_page = async function() {};
  env.context.__gosx_bootstrap_page = async function() {};

  const results = new FakeElement("main", null);
  results.id = "results";
  results.textContent = "results";
  parsedDocs.set("__RESTORE_FORM_DOC__", buildNavigatedDocument({
    title: "Search",
    bodyNodes: [results],
  }));

  runScript(navigationSource, env.context, "navigation_runtime.js");

  const submitListener = env.document.eventListeners.get("submit")[0];
  submitListener({
    type: "submit",
    target: form,
    submitter,
    defaultPrevented: false,
    preventDefault() {
      this.defaultPrevented = true;
    },
  });
  await flushAsyncWork();

  assert.equal(form.getAttribute("data-gosx-pending"), "queued");
  assert.equal(form.getAttribute("data-gosx-form-state"), "validating");
  assert.equal(env.document.dispatchedEvents.at(-1).type, "gosx:form:navigate");
});

test("navigation runtime honors submitter overrides and falls back with native semantics", async () => {
  const form = new FakeElement("form", null);
  form.setAttribute("action", "/save");
  form.setAttribute("method", "post");
  form.setAttribute("data-gosx-form", "");
  form.setAttribute("data-gosx-form-state", "idle");

  const title = new FakeElement("input", null);
  title.setAttribute("name", "title");
  title.value = "hello";
  form.appendChild(title);

  const submitter = new FakeElement("button", null);
  submitter.setAttribute("name", "intent");
  submitter.setAttribute("value", "preview");
  submitter.setAttribute("formaction", "/preview");
  submitter.setAttribute("formmethod", "get");
  submitter.formAction = "http://localhost:3000/preview";
  submitter.formMethod = "get";
  form.appendChild(submitter);

  const parsedDocs = new Map();
  const env = createContext({
    elements: [form],
    fetchRoutes: {
      "http://localhost:3000/preview?title=hello&intent=preview": {
        text: "__PREVIEW_DOC__",
        url: "http://localhost:3000/preview?title=hello&intent=preview",
      },
    },
    parseHTML(html) {
      return parsedDocs.get(html);
    },
  });
  env.context.__gosx_dispose_page = async function() {};
  env.context.__gosx_bootstrap_page = async function() {};

  const preview = new FakeElement("main", null);
  preview.id = "preview";
  preview.textContent = "preview";
  parsedDocs.set("__PREVIEW_DOC__", buildNavigatedDocument({
    title: "Preview",
    bodyNodes: [preview],
  }));

  runScript(navigationSource, env.context, "navigation_runtime.js");

  const submitListener = env.document.eventListeners.get("submit")[0];
  submitListener({
    type: "submit",
    target: form,
    submitter,
    defaultPrevented: false,
    preventDefault() {
      this.defaultPrevented = true;
    },
  });
  await flushAsyncWork();

  assert.equal(env.fetchCalls[0].url, "http://localhost:3000/preview?title=hello&intent=preview");
  assert.equal(env.document.dispatchedEvents.at(-1).detail.method, "GET");

  env.fetchCalls.length = 0;
  submitter.setAttribute("formmethod", "post");
  submitter.setAttribute("formaction", "/missing");
  submitter.formMethod = "post";
  submitter.formAction = "http://localhost:3000/missing";

  submitListener({
    type: "submit",
    target: form,
    submitter,
    defaultPrevented: false,
    preventDefault() {
      this.defaultPrevented = true;
    },
  });
  await flushAsyncWork();

  assert.equal(form.requestSubmitCalls.length, 1);
  assert.equal(form.requestSubmitCalls[0][0], submitter);
  assert.equal(form.hasAttribute("data-gosx-form"), true);
  assert.equal(form.getAttribute("data-gosx-form-state"), "idle");
});

test("navigation runtime ignores default submitter action property when no override attribute exists", async () => {
  const form = new FakeElement("form", null);
  form.setAttribute("action", "/save");
  form.setAttribute("method", "post");
  form.setAttribute("data-gosx-form", "");
  form.setAttribute("data-gosx-form-state", "idle");

  const title = new FakeElement("input", null);
  title.setAttribute("name", "title");
  title.value = "hello";
  form.appendChild(title);

  const submitter = new FakeElement("button", null);
  submitter.setAttribute("name", "intent");
  submitter.setAttribute("value", "publish");
  submitter.formAction = "http://localhost:3000/";
  form.appendChild(submitter);

  const env = createContext({
    elements: [form],
    fetchRoutes: {
      "http://localhost:3000/save": {
        text: '{"ok":true,"message":"saved"}',
        url: "http://localhost:3000/save",
      },
    },
  });

  runScript(navigationSource, env.context, "navigation_runtime.js");

  const submitListener = env.document.eventListeners.get("submit")[0];
  let prevented = false;
  submitListener({
    type: "submit",
    target: form,
    submitter,
    defaultPrevented: false,
    preventDefault() {
      prevented = true;
      this.defaultPrevented = true;
    },
  });
  await flushAsyncWork();

  assert.equal(prevented, true);
  assert.equal(env.fetchCalls[0].url, "http://localhost:3000/save");
  assert.equal(env.fetchCalls[0].init.method, "POST");
  assert.equal(env.document.dispatchedEvents.at(-1).type, "gosx:form:result");
  assert.equal(env.document.dispatchedEvents.at(-1).detail.action, "http://localhost:3000/save");
});

test("navigation runtime honors submitter override attributes without reflected props", async () => {
  const form = new FakeElement("form", null);
  form.setAttribute("action", "/save");
  form.setAttribute("method", "post");
  form.setAttribute("data-gosx-form", "");
  form.setAttribute("data-gosx-form-state", "idle");

  const title = new FakeElement("input", null);
  title.setAttribute("name", "title");
  title.value = "hello";
  form.appendChild(title);

  const submitter = new FakeElement("button", null);
  submitter.setAttribute("name", "intent");
  submitter.setAttribute("value", "preview");
  submitter.setAttribute("formaction", "/preview-attr");
  submitter.setAttribute("formmethod", "get");
  form.appendChild(submitter);

  const parsedDocs = new Map();
  const env = createContext({
    elements: [form],
    fetchRoutes: {
      "http://localhost:3000/preview-attr?title=hello&intent=preview": {
        text: "__PREVIEW_ATTR_DOC__",
        url: "http://localhost:3000/preview-attr?title=hello&intent=preview",
      },
    },
    parseHTML(html) {
      return parsedDocs.get(html);
    },
  });
  env.context.__gosx_dispose_page = async function() {};
  env.context.__gosx_bootstrap_page = async function() {};

  const preview = new FakeElement("main", null);
  preview.id = "preview-attr";
  preview.textContent = "preview";
  parsedDocs.set("__PREVIEW_ATTR_DOC__", buildNavigatedDocument({
    title: "Preview Attr",
    bodyNodes: [preview],
  }));

  runScript(navigationSource, env.context, "navigation_runtime.js");

  const submitListener = env.document.eventListeners.get("submit")[0];
  submitListener({
    type: "submit",
    target: form,
    submitter,
    defaultPrevented: false,
    preventDefault() {
      this.defaultPrevented = true;
    },
  });
  await flushAsyncWork();

  assert.equal(env.fetchCalls[0].url, "http://localhost:3000/preview-attr?title=hello&intent=preview");
  assert.equal(env.document.dispatchedEvents.at(-1).detail.method, "GET");

  env.fetchCalls.length = 0;
  submitter.setAttribute("formtarget", "_blank");
  let prevented = false;

  submitListener({
    type: "submit",
    target: form,
    submitter,
    defaultPrevented: false,
    preventDefault() {
      prevented = true;
      this.defaultPrevented = true;
    },
  });
  await flushAsyncWork();

  assert.equal(prevented, false);
  assert.equal(form.requestSubmitCalls.length, 0);
  assert.equal(env.fetchCalls.length, 0);
});
