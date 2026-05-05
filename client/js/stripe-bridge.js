// GoSX Stripe bridge.
// Loads Stripe.js directly from js.stripe.com and mounts only the Stripe
// surfaces declared by server-rendered GoSX components.
(function() {
  "use strict";

  const DEFAULT_STRIPE_JS = "https://js.stripe.com/clover/stripe.js";
  const CONFIG_ATTR = "data-gosx-stripe-config";
  const CONFIG_ID_ATTR = "data-gosx-stripe-config-id";
  const SURFACE_ATTR = "data-gosx-stripe-surface";
  const ELEMENT_ATTR = "data-gosx-stripe-element";
  const CHECKOUT_ATTR = "data-gosx-stripe-checkout";
  const CHECKOUT_ELEMENT_ATTR = "data-gosx-stripe-checkout-element";
  const EMBEDDED_ATTR = "data-gosx-stripe-embedded-checkout";
  const REDIRECT_ATTR = "data-gosx-stripe-redirect";
  const CONFIRM_ATTR = "data-gosx-stripe-confirm";
  const CHECKOUT_CONFIRM_ATTR = "data-gosx-stripe-checkout-confirm";
  const STATUS_ATTR = "data-gosx-stripe-state";
  const DEFAULT_ELEMENT_EVENTS = [
    "ready",
    "change",
    "focus",
    "blur",
    "click",
    "escape",
    "loaderror",
    "loaderstart",
    "networkschange",
    "confirm",
    "cancel",
    "shippingaddresschange",
    "shippingratechange",
  ];

  const state = window.__gosx_stripe || {
    version: "0.1.0",
    stripePromise: null,
    stripeInstances: new Map(),
    records: new Map(),
  };
  window.__gosx_stripe = state;

  function emit(name, detail) {
    if (typeof document.dispatchEvent !== "function" || typeof CustomEvent !== "function") return;
    document.dispatchEvent(new CustomEvent("gosx:stripe:" + name, { detail: detail || {} }));
  }

  function setState(el, value, message) {
    if (!el || typeof el.setAttribute !== "function") return;
    el.setAttribute(STATUS_ATTR, value || "idle");
    if (message) {
      el.setAttribute("data-gosx-stripe-message", String(message));
    } else {
      el.removeAttribute("data-gosx-stripe-message");
    }
  }

  function readJSONScript(id) {
    if (!id) return {};
    const script = document.getElementById(id);
    if (!script) return {};
    try {
      return JSON.parse(script.textContent || "{}") || {};
    } catch (error) {
      console.error("[gosx-stripe] invalid JSON config", id, error);
      return {};
    }
  }

  function readInlineJSON(el) {
    const raw = el && el.getAttribute(CONFIG_ATTR);
    if (!raw) return {};
    try {
      return JSON.parse(raw) || {};
    } catch (error) {
      console.error("[gosx-stripe] invalid inline JSON config", error);
      return {};
    }
  }

  function configFor(el) {
    return Object.assign({}, readJSONScript(el && el.getAttribute(CONFIG_ID_ATTR)), readInlineJSON(el));
  }

  function ensureScript(src) {
    src = src || DEFAULT_STRIPE_JS;
    if (window.Stripe) return Promise.resolve(window.Stripe);
    if (state.stripePromise) return state.stripePromise;
    state.stripePromise = new Promise(function(resolve, reject) {
      const existing = findScript(src);
      if (existing) {
        existing.addEventListener("load", function() {
          window.Stripe ? resolve(window.Stripe) : reject(new Error("Stripe.js loaded without window.Stripe"));
        }, { once: true });
        existing.addEventListener("error", function() {
          reject(new Error("failed to load Stripe.js"));
        }, { once: true });
        if (window.Stripe) resolve(window.Stripe);
        return;
      }

      const script = document.createElement("script");
      script.src = src;
      script.async = true;
      script.setAttribute("data-gosx-script", "managed");
      script.setAttribute("data-gosx-script-load", "dom");
      script.onload = function() {
        script.setAttribute("data-gosx-script-loaded", "true");
        window.Stripe ? resolve(window.Stripe) : reject(new Error("Stripe.js loaded without window.Stripe"));
      };
      script.onerror = function() {
        reject(new Error("failed to load Stripe.js"));
      };
      (document.head || document.documentElement).appendChild(script);
    });
    return state.stripePromise;
  }

  function findScript(src) {
    const absolute = absolutize(src);
    const scripts = document.querySelectorAll("script[src]");
    for (const script of scripts) {
      if (absolutize(script.getAttribute("src")) === absolute) return script;
    }
    return null;
  }

  function absolutize(src) {
    try {
      return new URL(src, window.location.href).href;
    } catch (_) {
      return src || "";
    }
  }

  async function stripeFor(config) {
    const StripeCtor = await ensureScript(config.stripeJS || config.stripeJs || DEFAULT_STRIPE_JS);
    const key = String(config.publishableKey || "").trim();
    if (!key) throw new Error("missing Stripe publishable key");
    const options = config.stripeOptions && typeof config.stripeOptions === "object" ? config.stripeOptions : {};
    const cacheKey = key + "\n" + JSON.stringify(options || {});
    if (!state.stripeInstances.has(cacheKey)) {
      state.stripeInstances.set(cacheKey, StripeCtor(key, options));
    }
    return state.stripeInstances.get(cacheKey);
  }

  function ownSurfaceElement(root, selector) {
    const nodes = root.querySelectorAll(selector);
    return Array.prototype.filter.call(nodes, function(node) {
      return node.closest("[" + SURFACE_ATTR + "],[" + CHECKOUT_ATTR + "]") === root;
    });
  }

  function formPayload(source) {
    if (!source || source.tagName !== "FORM" || typeof FormData !== "function") return null;
    const out = {};
    const form = new FormData(source);
    form.forEach(function(value, key) {
      if (Object.prototype.hasOwnProperty.call(out, key)) {
        if (!Array.isArray(out[key])) out[key] = [out[key]];
        out[key].push(value);
      } else {
        out[key] = value;
      }
    });
    return out;
  }

  async function fetchValue(spec, fallbackMethod, source) {
    if (!spec || !spec.url) return null;
    const init = {
      method: spec.method || fallbackMethod || "POST",
      headers: Object.assign({ "Content-Type": "application/json" }, spec.headers || {}),
    };
    if (Object.prototype.hasOwnProperty.call(spec, "body")) {
      init.body = typeof spec.body === "string" ? spec.body : JSON.stringify(spec.body || {});
    } else {
      const payload = formPayload(source);
      if (payload) init.body = JSON.stringify(payload);
    }
    const response = await fetch(spec.url, init);
    const data = await response.json().catch(function() { return {}; });
    if (!response.ok) throw new Error(data.error || "Stripe endpoint failed");
    return data;
  }

  async function resolveClientSecret(config) {
    if (config.clientSecret) return config.clientSecret;
    const data = await fetchValue(config.clientSecretRequest || config.fetchClientSecret, "POST");
    return data && (data.clientSecret || data.client_secret);
  }

  async function mountElementsSurface(root) {
    if (state.records.has(root)) disposeRoot(root);
    const config = configFor(root);
    setState(root, "loading");
    try {
      const stripe = await stripeFor(config);
      const elementsOptions = Object.assign({}, config.elementsOptions || {});
      const clientSecret = await resolveClientSecret(config);
      if (clientSecret) elementsOptions.clientSecret = clientSecret;
      const elements = stripe.elements(elementsOptions);
      const record = {
        kind: "elements",
        root,
        stripe,
        elements,
        mounted: [],
        listeners: [],
      };
      state.records.set(root, record);

      for (const el of ownSurfaceElement(root, "[" + ELEMENT_ATTR + "]")) {
        mountElement(record, el, el.getAttribute(ELEMENT_ATTR), false);
      }
      for (const form of ownSurfaceElement(root, "[" + CONFIRM_ATTR + "]")) {
        bindElementsConfirm(record, form);
      }
      setState(root, "ready");
      emit("ready", { kind: "elements", root: root.id || "" });
    } catch (error) {
      setState(root, "error", error && error.message);
      emit("error", { kind: "elements", root: root.id || "", error });
      console.error("[gosx-stripe] elements surface failed", error);
    }
  }

  function mountElement(record, el, type, checkoutMode) {
    const config = configFor(el);
    const options = config.options || {};
    const factory = checkoutMode ? record.checkout : record.elements;
    let element;
    if (checkoutMode) {
      const method = config.create || el.getAttribute("data-gosx-stripe-create") || checkoutCreateMethod(type);
      if (!factory || typeof factory[method] !== "function") {
        throw new Error("Stripe Checkout element factory not found: " + method);
      }
      element = factory[method](options);
    } else {
      element = factory.create(type, options);
    }
    element.mount(el);
    setState(el, "ready");
    record.mounted.push(element);
    bindElementEvents(el, element, config.events);
  }

  function checkoutCreateMethod(type) {
    switch (String(type || "").toLowerCase()) {
    case "payment":
    case "payment-element":
      return "createPaymentElement";
    case "express-checkout":
    case "expresscheckoutelement":
      return "createExpressCheckoutElement";
    case "billing-address":
      return "createBillingAddressElement";
    case "shipping-address":
      return "createShippingAddressElement";
    default:
      return type || "";
    }
  }

  function bindElementEvents(mount, element, events) {
    const names = Array.isArray(events) && events.length ? events : DEFAULT_ELEMENT_EVENTS;
    for (const name of names) {
      if (!name || typeof element.on !== "function") continue;
      try {
        element.on(name, function(event) {
          emit("event", {
            element: mount.id || "",
            type: mount.getAttribute(ELEMENT_ATTR) || mount.getAttribute(CHECKOUT_ELEMENT_ATTR) || "",
            event: name,
            payload: event || null,
          });
        });
      } catch (_) {}
    }
  }

  function bindElementsConfirm(record, form) {
    const config = configFor(form);
    const listener = async function(event) {
      event.preventDefault();
      setState(form, "submitting");
      try {
        if (config.submit !== false && record.elements && typeof record.elements.submit === "function") {
          const submit = await record.elements.submit();
          if (submit && submit.error) throw submit.error;
        }
        const method = config.method || form.getAttribute(CONFIRM_ATTR) || "confirmPayment";
        const args = elementsConfirmArgs(record, config);
        const result = await record.stripe[method](args);
        if (result && result.error) throw result.error;
        setState(form, "complete");
        emit("complete", { kind: "elements", method, result });
      } catch (error) {
        setState(form, "error", error && error.message);
        emit("error", { kind: "elements", error });
      }
    };
    form.addEventListener("submit", listener);
    record.listeners.push(function() { form.removeEventListener("submit", listener); });
  }

  function elementsConfirmArgs(record, config) {
    if (Array.isArray(config.args)) return config.args;
    const args = Object.assign({}, config.params || {});
    if (!Object.prototype.hasOwnProperty.call(args, "elements")) args.elements = record.elements;
    if (config.clientSecret) args.clientSecret = config.clientSecret;
    const confirmParams = Object.assign({}, config.confirmParams || {});
    if (config.returnUrl || config.return_url) confirmParams.return_url = config.returnUrl || config.return_url;
    if (Object.keys(confirmParams).length) args.confirmParams = confirmParams;
    if (config.redirect) args.redirect = config.redirect;
    return args;
  }

  async function mountEmbeddedCheckout(root) {
    if (state.records.has(root)) disposeRoot(root);
    const config = configFor(root);
    setState(root, "loading");
    try {
      const stripe = await stripeFor(config);
      const init = Object.assign({}, config.init || {});
      const clientSecret = await resolveClientSecret(config);
      if (clientSecret) init.clientSecret = clientSecret;
      if (!init.clientSecret && (config.clientSecretRequest || config.fetchClientSecret)) {
        const spec = config.clientSecretRequest || config.fetchClientSecret;
        init.fetchClientSecret = function() {
          return fetchValue(spec, "POST").then(function(data) {
            return data.clientSecret || data.client_secret;
          });
        };
      }
      const checkout = await stripe.initEmbeddedCheckout(init);
      checkout.mount(root);
      state.records.set(root, { kind: "embedded-checkout", root, checkout, mounted: [], listeners: [] });
      setState(root, "ready");
      emit("ready", { kind: "embedded-checkout", root: root.id || "" });
    } catch (error) {
      setState(root, "error", error && error.message);
      emit("error", { kind: "embedded-checkout", root: root.id || "", error });
      console.error("[gosx-stripe] embedded checkout failed", error);
    }
  }

  async function mountCheckoutSurface(root) {
    if (state.records.has(root)) disposeRoot(root);
    const config = configFor(root);
    setState(root, "loading");
    try {
      const stripe = await stripeFor(config);
      const init = Object.assign({}, config.init || {});
      const clientSecret = await resolveClientSecret(config);
      if (clientSecret) init.clientSecret = clientSecret;
      if (config.elementsOptions) init.elementsOptions = config.elementsOptions;
      const checkout = await stripe.initCheckout(init);
      const record = { kind: "checkout", root, stripe, checkout, mounted: [], listeners: [] };
      state.records.set(root, record);

      if (checkout && typeof checkout.on === "function") {
        checkout.on("change", function(session) {
          emit("checkout-change", { root: root.id || "", session });
        });
      }
      for (const el of ownSurfaceElement(root, "[" + CHECKOUT_ELEMENT_ATTR + "]")) {
        mountElement(record, el, el.getAttribute(CHECKOUT_ELEMENT_ATTR), true);
      }
      for (const button of ownSurfaceElement(root, "[" + CHECKOUT_CONFIRM_ATTR + "]")) {
        bindCheckoutConfirm(record, button);
      }
      setState(root, "ready");
      emit("ready", { kind: "checkout", root: root.id || "" });
    } catch (error) {
      setState(root, "error", error && error.message);
      emit("error", { kind: "checkout", root: root.id || "", error });
      console.error("[gosx-stripe] checkout surface failed", error);
    }
  }

  function bindCheckoutConfirm(record, button) {
    const config = configFor(button);
    const listener = async function(event) {
      event.preventDefault();
      setState(button, "submitting");
      try {
        const result = await checkoutActions(record);
        if (result.type !== "success") throw result.error || new Error("checkout actions unavailable");
        const args = Object.assign({}, config.params || {});
        const confirmation = await result.actions.confirm(args);
        if (confirmation && confirmation.type === "error") throw confirmation.error;
        setState(button, "complete");
        emit("complete", { kind: "checkout", result: confirmation || null });
      } catch (error) {
        setState(button, "error", error && error.message);
        emit("error", { kind: "checkout", error });
      }
    };
    button.addEventListener(button.tagName === "FORM" ? "submit" : "click", listener);
    record.listeners.push(function() {
      button.removeEventListener(button.tagName === "FORM" ? "submit" : "click", listener);
    });
  }

  function checkoutActions(record) {
    if (!record.actionsPromise) {
      record.actionsPromise = record.checkout.loadActions();
    }
    return record.actionsPromise;
  }

  async function bindRedirect(root) {
    if (state.records.has(root)) disposeRoot(root);
    const config = configFor(root);
    const record = { kind: "redirect", root, mounted: [], listeners: [] };
    state.records.set(root, record);
    const listener = async function(event) {
      event.preventDefault();
      setState(root, "submitting");
      try {
        const stripe = await stripeFor(config);
        let sessionId = config.sessionId || config.session_id;
        let url = config.url;
        if (!sessionId && !url && (config.sessionRequest || config.fetchSession)) {
          const data = await fetchValue(config.sessionRequest || config.fetchSession, "POST", root);
          sessionId = data.sessionId || data.session_id || data.id;
          url = data.url;
        }
        if (sessionId) {
          const result = await stripe.redirectToCheckout({ sessionId });
          if (result && result.error) throw result.error;
          return;
        }
        if (url) {
          window.location.href = url;
          return;
        }
        throw new Error("missing Checkout session id or url");
      } catch (error) {
        setState(root, "error", error && error.message);
        emit("error", { kind: "redirect", error });
      }
    };
    root.addEventListener(root.tagName === "FORM" ? "submit" : "click", listener);
    record.listeners.push(function() {
      root.removeEventListener(root.tagName === "FORM" ? "submit" : "click", listener);
    });
  }

  function disposeRoot(root) {
    const record = state.records.get(root);
    if (!record) return;
    for (const release of record.listeners || []) {
      try { release(); } catch (_) {}
    }
    for (const mounted of record.mounted || []) {
      try {
        if (mounted && typeof mounted.destroy === "function") mounted.destroy();
        else if (mounted && typeof mounted.unmount === "function") mounted.unmount();
      } catch (_) {}
    }
    try {
      if (record.checkout && typeof record.checkout.destroy === "function") record.checkout.destroy();
      else if (record.checkout && typeof record.checkout.unmount === "function") record.checkout.unmount();
    } catch (_) {}
    state.records.delete(root);
  }

  async function mountAll(root) {
    const scope = root || document;
    const tasks = [];
    for (const node of scope.querySelectorAll("[" + SURFACE_ATTR + "]")) tasks.push(mountElementsSurface(node));
    for (const node of scope.querySelectorAll("[" + EMBEDDED_ATTR + "]")) tasks.push(mountEmbeddedCheckout(node));
    for (const node of scope.querySelectorAll("[" + CHECKOUT_ATTR + "]")) tasks.push(mountCheckoutSurface(node));
    for (const node of scope.querySelectorAll("[" + REDIRECT_ATTR + "]")) tasks.push(bindRedirect(node));
    await Promise.all(tasks);
  }

  async function disposeAll() {
    for (const root of Array.from(state.records.keys())) disposeRoot(root);
  }

  state.mountAll = mountAll;
  state.disposeAll = disposeAll;
  state.stripeFor = stripeFor;

  const previousBootstrap = window.__gosx_bootstrap_page;
  const previousDispose = window.__gosx_dispose_page;

  window.__gosx_bootstrap_page = async function() {
    if (typeof previousBootstrap === "function") await previousBootstrap();
    await mountAll(document);
  };

  window.__gosx_dispose_page = async function() {
    await disposeAll();
    if (typeof previousDispose === "function") await previousDispose();
  };

  if (document.readyState === "loading") {
    document.addEventListener("DOMContentLoaded", function() { mountAll(document); }, { once: true });
  } else {
    mountAll(document);
  }
})();
