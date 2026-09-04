"use strict";

const test = require("node:test");
const assert = require("node:assert/strict");
const childProcess = require("node:child_process");
const path = require("node:path");

const {
  bootstrapLiteSource,
  stripeBridgeSource,
  FakeElement,
  createContext,
  runScript,
  flushAsyncWork,
} = require("./runtime-test-harness.js");

function stripeSurface(kind, id, action, children = []) {
  const root = new FakeElement(kind === "embedded" ? "div" : "section", null);
  root.id = id;
  root.setAttribute("data-gosx-runtime-surface", kind === "elements"
    ? "stripe-elements"
    : (kind === "embedded" ? "stripe-embedded-checkout" : "stripe-checkout"));
  root.setAttribute("data-gosx-runtime-surface-version", "1");
  const config = new FakeElement("script", null);
  config.id = id + "-config";
  config.textContent = JSON.stringify({
    publishableKey: "pk_test_public",
    sessionAction: action,
  });
  root.setAttribute("data-gosx-stripe-config-id", config.id);
  root.appendChild(config);
  for (const child of children) root.appendChild(child);
  return root;
}

function paymentMount(id) {
  const mount = new FakeElement("div", null);
  mount.id = id;
  mount.setAttribute("data-gosx-stripe-element", "payment");
  const config = new FakeElement("script", null);
  config.id = id + "-config";
  config.textContent = JSON.stringify({ options: { layout: "tabs" } });
  mount.setAttribute("data-gosx-stripe-config-id", config.id);
  return { mount, config };
}

function confirmControl(id) {
  const control = new FakeElement("button", null);
  control.id = id;
  control.setAttribute("type", "button");
  control.setAttribute("aria-busy", "false");
  control.setAttribute("data-gosx-stripe-confirm-control", "");
  const config = new FakeElement("script", null);
  config.id = id + "-config";
  config.textContent = JSON.stringify({ method: "confirmPayment", returnPath: "/checkout/return" });
  control.setAttribute("data-gosx-stripe-config-id", config.id);
  return { control, config };
}

function installStripeFake(context, captures) {
  context.Stripe = function(key, options) {
    captures.instances.push({ key, options });
    return {
      elements(init) {
        captures.elementsOptions.push(init);
        return {
          create(type, elementOptions) {
            const listeners = new Map();
            const element = {
              type,
              elementOptions,
              mount(target) { captures.mounts.push(target); },
              destroy() { captures.destroyed.push(type); },
              on(name, listener) { listeners.set(name, listener); },
              off(name, listener) {
                if (listeners.get(name) === listener) listeners.delete(name);
              },
              fire(name, value) {
                const listener = listeners.get(name);
                if (listener) listener(value);
              },
            };
            captures.elements.push(element);
            return element;
          },
          async submit() { return {}; },
        };
      },
      async confirmPayment() {
        captures.confirmCalls += 1;
        return { paymentIntent: { client_secret: "RAW_CONFIRM_RESULT_SECRET" } };
      },
      async initEmbeddedCheckout(init) {
        captures.embeddedInit.push(init);
        await init.fetchClientSecret();
        return {
          mount(target) { captures.embeddedMounts.push(target); },
          destroy() { captures.embeddedDestroyed += 1; },
        };
      },
      async initCheckout(init) {
        captures.checkoutInit.push(init);
        return {
          on() {},
          off() {},
          loadActions: async () => ({ type: "success", actions: { confirm: async () => ({ type: "success" }) } }),
        };
      },
    };
  };
}

function newCaptures() {
  return {
    instances: [],
    elementsOptions: [],
    elements: [],
    mounts: [],
    destroyed: [],
    embeddedInit: [],
    embeddedMounts: [],
    embeddedDestroyed: 0,
    checkoutInit: [],
    confirmCalls: 0,
  };
}

async function bootStripe(env, captures) {
  installStripeFake(env.context, captures);
  runScript(bootstrapLiteSource, env.context, "bootstrap-lite.js");
  await flushAsyncWork();
  runScript(stripeBridgeSource, env.context, "stripe-bridge.js");
  await flushAsyncWork();
}

test("Stripe bridge keeps a strict CSP and lifecycle authority boundary", () => {
  assert.match(stripeBridgeSource, /https:\/\/js\.stripe\.com\/clover\/stripe\.js/);
  for (const forbidden of [
    "eval(",
    "new Function",
    "config.stripeJS",
    "spec.headers",
    "spec.body",
    'install("__gosx_bootstrap_page"',
    'install("__gosx_dispose_page"',
  ]) {
    assert.equal(stripeBridgeSource.includes(forbidden), false, "bridge must not contain " + forbidden);
  }
});

test("a rendered initial document executes runtime, Stripe.js, then bridge and mounts once", async () => {
  const repoRoot = path.resolve(__dirname, "../..");
  const html = childProcess.execFileSync("go", ["run", "./stripeui/testdata/renderdocument"], {
    cwd: repoRoot,
    encoding: "utf8",
  });
  const scripts = Array.from(html.matchAll(/<script\b([^>]*)>/g))
    .map((match) => ({
      src: /\bsrc="([^"]+)"/.exec(match[1])?.[1] || "",
      nonce: /\bnonce="([^"]+)"/.exec(match[1])?.[1] || "",
    }))
    .filter((script) => script.src);
  assert.deepEqual(scripts.map((script) => script.src), [
    "/gosx/bootstrap-lite.js",
    "https://js.stripe.com/clover/stripe.js",
    "/gosx/stripe-bridge.js",
  ]);
  assert.deepEqual(scripts.map((script) => script.nonce), [
    "strict-csp-nonce",
    "strict-csp-nonce",
    "strict-csp-nonce",
  ]);

  const child = paymentMount("payment-emitted-order");
  const root = stripeSurface("elements", "elements-emitted-order", "/session/emitted-order", [child.config, child.mount]);
  const env = createContext({
    elements: [root],
    fetchRoutes: {
      "/session/emitted-order": { text: JSON.stringify({ clientSecret: "secret_emitted_order" }), headers: { "content-type": "application/json" } },
    },
  });
  const captures = newCaptures();
  for (const script of scripts) {
    if (script.src === "/gosx/bootstrap-lite.js") runScript(bootstrapLiteSource, env.context, script.src);
    else if (script.src === "https://js.stripe.com/clover/stripe.js") installStripeFake(env.context, captures);
    else if (script.src === "/gosx/stripe-bridge.js") runScript(stripeBridgeSource, env.context, script.src);
    await flushAsyncWork();
  }
  assert.equal(env.fetchCalls.filter((call) => call.url === "/session/emitted-order").length, 1);
  assert.equal(captures.mounts.length, 1);
  assert.equal(root.getAttribute("data-gosx-stripe-state"), "ready");
});

test("repeated bridge execution keeps an existing surface mounted exactly once", async () => {
  const child = paymentMount("payment-idempotent");
  const root = stripeSurface("elements", "elements-idempotent", "/session/idempotent", [child.config, child.mount]);
  const env = createContext({
    elements: [root],
    fetchRoutes: {
      "/session/idempotent": { text: JSON.stringify({ clientSecret: "secret_idempotent" }), headers: { "content-type": "application/json" } },
    },
  });
  const captures = newCaptures();
  await bootStripe(env, captures);
  runScript(stripeBridgeSource, env.context, "stripe-bridge-second.js");
  await flushAsyncWork();
  assert.equal(env.fetchCalls.filter((call) => call.url === "/session/idempotent").length, 1);
  assert.equal(captures.mounts.length, 1);
  assert.equal(captures.destroyed.length, 0);
  assert.equal(env.context.__gosx.runtimeSurfaces.size, 1);
});

test("Elements requests its secret through scoped same-origin transport and emits redacted events", async () => {
  const child = paymentMount("payment-one");
  const confirm = confirmControl("confirm-one");
  const unrelated = new FakeElement("div", null);
  unrelated.id = "unrelated-control";
  unrelated.setAttribute("role", "group");
  unrelated.setAttribute("data-gosx-stripe-confirm", "confirmPayment");
  const root = stripeSurface("elements", "elements-one", "/account/__actions/stripe-session", [child.config, child.mount, confirm.config, confirm.control, unrelated]);
  const csrf = new FakeElement("meta", null);
  csrf.setAttribute("name", "csrf-token");
  csrf.setAttribute("content", "csrf-token-public");
  const env = createContext({
    elements: [csrf, root],
    fetchRoutes: {
      "/account/__actions/stripe-session": {
        text: JSON.stringify({ clientSecret: "pi_secret_BROWSER_ONLY" }),
        headers: { "content-type": "application/json" },
      },
    },
  });
  const captures = newCaptures();
  await bootStripe(env, captures);

  const request = env.fetchCalls.find((call) => call.url === "/account/__actions/stripe-session");
  assert.ok(request, "expected the fixed session action to be called");
  assert.equal(request.init.method, "POST");
  assert.equal(request.init.headers["X-CSRF-Token"], "csrf-token-public");
  assert.equal(request.init.signal.aborted, false);
  assert.equal(captures.elementsOptions[0].clientSecret, "pi_secret_BROWSER_ONLY");
  assert.equal(captures.mounts[0], child.mount);

  captures.elements[0].fire("change", {
    complete: true,
    empty: false,
    client_secret: "RAW_VENDOR_EVENT_SECRET",
    value: { card: "4242424242424242" },
  });
  captures.elements[0].fire("loaderror", {
    error: { code: "loader_failed", raw: "RAW_VENDOR_ERROR_SECRET" },
  });
  unrelated.dispatchEvent({ type: "click", preventDefault() {} });
  await flushAsyncWork();
  assert.equal(captures.confirmCalls, 0, "a legacy role=group target must not confirm payment");
  confirm.control.dispatchEvent({ type: "click", preventDefault() {} });
  await flushAsyncWork();
  assert.equal(captures.confirmCalls, 1);
  assert.equal(confirm.control.getAttribute("aria-busy"), "false");
  const stripeEvents = env.document.dispatchedEvents.filter((event) => event.type.indexOf("gosx:stripe:") === 0);
  const serialized = JSON.stringify(stripeEvents.map((event) => ({ type: event.type, detail: event.detail })));
  assert.match(serialized, /gosx:stripe:status/);
  assert.match(serialized, /"complete":true/);
  assert.match(serialized, /element-load_failed/);
  for (const forbidden of ["RAW_VENDOR_EVENT_SECRET", "RAW_VENDOR_ERROR_SECRET", "RAW_CONFIRM_RESULT_SECRET", "4242424242424242", '"payload"', '"result"']) {
    assert.equal(serialized.includes(forbidden), false, "events must not contain " + forbidden);
  }
  assert.match(serialized, /gosx:stripe:complete/);
  assert.match(serialized, /"authoritative":false/);
});

test("invalid and query-bearing session actions fail closed without fetching", async () => {
  for (const action of ["https://evil.example/session", "/session?tenant=secret", "//evil.example/session"]) {
    const root = stripeSurface("elements", "invalid-" + action.length, action);
    const env = createContext({ elements: [root] });
    const captures = newCaptures();
    await bootStripe(env, captures);
    assert.equal(env.fetchCalls.some((call) => String(call.url).includes("session")), false);
    assert.equal(root.getAttribute("data-gosx-stripe-state"), "error");
    const serialized = JSON.stringify(env.document.dispatchedEvents
      .filter((event) => event.type.indexOf("gosx:stripe:") === 0)
      .map((event) => event.detail));
    assert.equal(serialized.includes("evil.example"), false);
    assert.equal(serialized.includes("tenant=secret"), false);
  }
});

test("multiple Stripe surfaces own independent lifecycle and disposal", async () => {
  const firstChild = paymentMount("payment-first");
  const secondChild = paymentMount("payment-second");
  const first = stripeSurface("elements", "elements-first", "/session/first", [firstChild.config, firstChild.mount]);
  const second = stripeSurface("elements", "elements-second", "/session/second", [secondChild.config, secondChild.mount]);
  const env = createContext({
    elements: [first, second],
    fetchRoutes: {
      "/session/first": { text: JSON.stringify({ clientSecret: "secret_first" }), headers: { "content-type": "application/json" } },
      "/session/second": { text: JSON.stringify({ clientSecret: "secret_second" }), headers: { "content-type": "application/json" } },
    },
  });
  const captures = newCaptures();
  await bootStripe(env, captures);
  assert.equal(captures.mounts.length, 2);
  assert.equal(env.context.__gosx.runtimeSurfaces.size, 2);

  env.context.__gosx_dispose_runtime_surfaces(first);
  assert.equal(captures.destroyed.length, 1);
  assert.equal(env.context.__gosx.runtimeSurfaces.size, 1);
  assert.equal(first.getAttribute("data-gosx-stripe-state"), "disposed");
  assert.equal(second.getAttribute("data-gosx-stripe-state"), "ready");
});

test("disposing a surface aborts an in-flight session request", async () => {
  let release;
  const response = new Promise((resolve) => { release = resolve; });
  const child = paymentMount("payment-pending");
  const root = stripeSurface("elements", "elements-pending", "/session/pending", [child.config, child.mount]);
  const env = createContext({
    elements: [root],
    fetchRoutes: { "/session/pending": async () => response },
  });
  const captures = newCaptures();
  installStripeFake(env.context, captures);
  runScript(bootstrapLiteSource, env.context, "bootstrap-lite.js");
  await flushAsyncWork();
  runScript(stripeBridgeSource, env.context, "stripe-bridge.js");
  await flushAsyncWork();
  const request = env.fetchCalls.find((call) => call.url === "/session/pending");
  assert.ok(request);
  env.context.__gosx_dispose_runtime_surfaces(root);
  assert.equal(request.init.signal.aborted, true);
  release({ text: JSON.stringify({ clientSecret: "late_secret" }), headers: { "content-type": "application/json" } });
  await flushAsyncWork();
  assert.equal(captures.mounts.length, 0);
  assert.equal(root.getAttribute("data-gosx-stripe-state"), "disposed");
});

test("embedded Checkout receives its secret callback without exposing raw completion data", async () => {
  const root = stripeSurface("embedded", "embedded-one", "/session/embedded");
  const env = createContext({
    elements: [root],
    fetchRoutes: {
      "/session/embedded": { text: JSON.stringify({ ok: true, data: { clientSecret: "cs_secret_browser_only" } }), headers: { "content-type": "application/json" } },
    },
  });
  const captures = newCaptures();
  await bootStripe(env, captures);
  assert.equal(captures.embeddedInit.length, 1);
  assert.equal(captures.embeddedMounts[0], root);
  const events = JSON.stringify(env.document.dispatchedEvents
    .filter((event) => event.type.indexOf("gosx:stripe:") === 0)
    .map((event) => event.detail));
  assert.equal(events.includes("cs_secret_browser_only"), false);
});
