// GoSX Stripe managed surfaces.
// Stripe.js is loaded directly from js.stripe.com. GoSX owns only lifecycle,
// same-origin session transport, and redacted UX events around Stripe's secure
// iframe surfaces.
(function() {
  "use strict";

  const DEFAULT_STRIPE_JS = "https://js.stripe.com/clover/stripe.js";
  const CONFIG_ID_ATTR = "data-gosx-stripe-config-id";
  const ELEMENT_ATTR = "data-gosx-stripe-element";
  const CHECKOUT_ELEMENT_ATTR = "data-gosx-stripe-checkout-element";
  const CONFIRM_ATTR = "data-gosx-stripe-confirm";
  const CHECKOUT_CONFIRM_ATTR = "data-gosx-stripe-checkout-confirm";
  const STATUS_ATTR = "data-gosx-stripe-state";
  const SURFACE_ELEMENTS = "stripe-elements";
  const SURFACE_EMBEDDED = "stripe-embedded-checkout";
  const SURFACE_CHECKOUT = "stripe-checkout";
  const ALLOWED_ELEMENT_EVENTS = ["ready", "change", "focus", "blur", "loaderror"];

  if (!gosxHost.surfaces || typeof gosxHost.surfaces.register !== "function") {
    console.error("[gosx-stripe] runtime surfaces are unavailable; load the GoSX bootstrap before the Stripe bridge");
    return;
  }

  const state = gosxHost.stripe || {
    version: "1.0.0",
    stripePromise: null,
    stripeInstances: new Map(),
    records: new Map(),
  };
  state.version = "1.0.0";
  gosxHost.stripe = state;
  gosxHostCompatibility.install("__gosx_stripe", state);

  function boundedID(value) {
    return String(value || "").replace(/[^a-zA-Z0-9_.:-]/g, "").slice(0, 96);
  }

  function safeErrorCode(phase) {
    const value = String(phase || "stripe")
      .replace(/[^a-zA-Z0-9_.-]/g, "_")
      .slice(0, 48);
    return (value || "stripe") + "_failed";
  }

  function detailFor(record, phase, extra) {
    return Object.assign({
      kind: record.kind,
      root: boundedID(record.root && record.root.id),
      phase: boundedID(phase),
    }, extra || {});
  }

  function emit(record, name, phase, extra) {
    if (!record || record.disposed) return;
    record.context.dispatch("gosx:stripe:" + name, detailFor(record, phase, extra));
  }

  function setState(element, value) {
    if (!element || typeof element.setAttribute !== "function") return;
    element.setAttribute(STATUS_ATTR, boundedID(value || "idle"));
    element.removeAttribute("data-gosx-stripe-message");
  }

  function reportError(record, phase, error, element) {
    if (!record || record.disposed || isAborted(record)) return;
    const target = element || record.root;
    setState(target, "error");
    emit(record, "error", phase, {
      element: boundedID(target && target.id),
      code: safeErrorCode(phase),
    });
    if (record.context && typeof record.context.reportFailure === "function") {
      record.context.reportFailure("stripe-" + boundedID(phase), new Error("Stripe surface operation failed"), {
        provider: "stripe",
        kind: record.kind,
        code: safeErrorCode(phase),
      });
    }
  }

  function readJSONScript(id) {
    if (!id) return {};
    const script = document.getElementById(id);
    if (!script) return {};
    try {
      return JSON.parse(script.textContent || "{}") || {};
    } catch (_) {
      return {};
    }
  }

  function configFor(element) {
    return readJSONScript(element && element.getAttribute(CONFIG_ID_ATTR));
  }

  function findStripeScript() {
    const scripts = document.querySelectorAll("script[src]");
    for (const script of scripts) {
      try {
        if (new URL(script.getAttribute("src"), window.location.href).href === DEFAULT_STRIPE_JS) return script;
      } catch (_) {}
    }
    return null;
  }

  function ensureStripeScript() {
    if (window.Stripe) return Promise.resolve(window.Stripe);
    if (state.stripePromise) return state.stripePromise;
    state.stripePromise = new Promise(function(resolve, reject) {
      const existing = findStripeScript();
      if (existing) {
        existing.addEventListener("load", function() {
          window.Stripe ? resolve(window.Stripe) : reject(new Error("stripe_constructor_missing"));
        }, { once: true });
        existing.addEventListener("error", function() { reject(new Error("stripe_script_failed")); }, { once: true });
        if (window.Stripe) resolve(window.Stripe);
        return;
      }
      const script = document.createElement("script");
      script.src = DEFAULT_STRIPE_JS;
      script.setAttribute("src", DEFAULT_STRIPE_JS);
      script.async = true;
      script.type = "text/javascript";
      script.setAttribute("type", "text/javascript");
      script.setAttribute("crossorigin", "anonymous");
      script.setAttribute("referrerpolicy", "no-referrer");
      script.setAttribute("data-gosx-script", "managed");
      script.setAttribute("data-gosx-script-load", "dom");
      script.onload = function() {
        script.setAttribute("data-gosx-script-loaded", "true");
        window.Stripe ? resolve(window.Stripe) : reject(new Error("stripe_constructor_missing"));
      };
      script.onerror = function() { reject(new Error("stripe_script_failed")); };
      (document.head || document.documentElement).appendChild(script);
    });
    return state.stripePromise;
  }

  async function stripeFor(config) {
    const StripeCtor = await ensureStripeScript();
    const key = String(config.publishableKey || "").trim();
    if (!key) throw new Error("publishable_key_missing");
    const options = config.stripeOptions && typeof config.stripeOptions === "object" ? config.stripeOptions : {};
    const cacheKey = key + "\n" + JSON.stringify(options);
    if (!state.stripeInstances.has(cacheKey)) state.stripeInstances.set(cacheKey, StripeCtor(key, options));
    return state.stripeInstances.get(cacheKey);
  }

  function sessionAction(raw) {
    const authored = String(raw || "");
    if (!authored || authored.trim() !== authored || authored[0] !== "/" || authored.indexOf("//") === 0) return "";
    if (authored.indexOf("\\") >= 0 || authored.indexOf("?") >= 0 || authored.indexOf("#") >= 0) return "";
    try {
      const target = new URL(authored, window.location.href);
      if (target.origin !== window.location.origin || target.search || target.hash) return "";
      return target.pathname;
    } catch (_) {
      return "";
    }
  }

  async function requestClientSecret(record, action) {
    const target = sessionAction(action);
    if (!target) throw new Error("session_action_invalid");
    const result = await record.context.requestJSON(target, {
      method: "POST",
      headers: { "Accept": "application/json", "Content-Type": "application/json" },
      body: "{}",
    });
    if (isAborted(record)) throw new Error("AbortError");
    if (!result.response || !result.response.ok) throw new Error("session_action_failed");
    const envelope = result.data && typeof result.data === "object" ? result.data : {};
    const data = envelope.data && typeof envelope.data === "object" ? envelope.data : envelope;
    const secret = String(data.clientSecret || data.client_secret || "");
    if (!secret || secret.length > 4096) throw new Error("client_secret_missing");
    return secret;
  }

  function isAborted(record) {
    return !!(record.disposed || record.context.signal && record.context.signal.aborted);
  }

  function ownSurfaceElements(root, selector) {
    const nodes = root.querySelectorAll(selector);
    return Array.prototype.filter.call(nodes, function(node) {
      let current = node;
      while (current) {
        if (current.getAttribute && current.getAttribute("data-gosx-runtime-surface")) return current === root;
        current = current.parentNode;
      }
      return false;
    });
  }

  function bindElementEvents(record, mount, element) {
    for (const name of ALLOWED_ELEMENT_EVENTS) {
      if (!element || typeof element.on !== "function") continue;
      const listener = function(event) {
        const elementID = boundedID(mount.id);
        if (name === "ready") {
          setState(mount, "ready");
          emit(record, "ready", "element", { element: elementID });
          return;
        }
        if (name === "loaderror") {
          reportError(record, "element-load", event && event.error, mount);
          return;
        }
        const detail = { element: elementID, status: name };
        if (name === "change") {
          detail.complete = !!(event && event.complete);
          detail.empty = !!(event && event.empty);
        }
        emit(record, "status", "element", detail);
      };
      try {
        element.on(name, listener);
        record.releases.push(function() {
          if (typeof element.off === "function") element.off(name, listener);
        });
      } catch (_) {}
    }
  }

  function mountElement(record, mount, type, checkoutMode) {
    const config = configFor(mount);
    const options = config.options && typeof config.options === "object" ? config.options : {};
    const factory = checkoutMode ? record.checkout : record.elements;
    let element;
    if (checkoutMode) {
      const method = config.create || checkoutCreateMethod(type);
      if (!factory || typeof factory[method] !== "function") throw new Error("checkout_element_factory_missing");
      element = factory[method](options);
    } else {
      element = factory.create(type, options);
    }
    if (isAborted(record)) {
      if (element && typeof element.destroy === "function") element.destroy();
      return;
    }
    element.mount(mount);
    record.mounted.push(element);
    bindElementEvents(record, mount, element);
  }

  function checkoutCreateMethod(type) {
    switch (String(type || "").toLowerCase()) {
    case "payment":
    case "payment-element":
      return "createPaymentElement";
    case "express-checkout":
    case "expresscheckout":
      return "createExpressCheckoutElement";
    case "billing-address":
      return "createBillingAddressElement";
    case "shipping-address":
      return "createShippingAddressElement";
    default:
      return String(type || "");
    }
  }

  function returnURL(path) {
    const target = sessionAction(path);
    return target ? new URL(target, window.location.origin).href : "";
  }

  function bindElementsConfirm(record, control) {
    const config = configFor(control);
    const listener = async function(event) {
      event.preventDefault();
      setState(control, "submitting");
      emit(record, "status", "confirm", { element: boundedID(control.id), status: "submitting" });
      try {
        if (config.submit !== false && record.elements && typeof record.elements.submit === "function") {
          const submitted = await record.elements.submit();
          if (submitted && submitted.error) throw submitted.error;
        }
        if (isAborted(record)) return;
        const method = config.method === "confirmSetup" ? "confirmSetup" : "confirmPayment";
        const args = { elements: record.elements };
        const target = returnURL(config.returnPath);
        if (target) args.confirmParams = { return_url: target };
        if (config.redirect === "always" || config.redirect === "if_required") args.redirect = config.redirect;
        const result = await record.stripe[method](args);
        if (result && result.error) throw result.error;
        if (isAborted(record)) return;
        setState(control, "complete");
        emit(record, "complete", "confirm", {
          element: boundedID(control.id),
          method,
          authoritative: false,
        });
      } catch (error) {
        reportError(record, "confirm", error, control);
      }
    };
    record.context.listen(control, "click", listener);
  }

  async function mountElements(record) {
    const config = configFor(record.root);
    setState(record.root, "loading");
    const values = await Promise.all([stripeFor(config), requestClientSecret(record, config.sessionAction)]);
    if (isAborted(record)) return;
    record.stripe = values[0];
    const options = Object.assign({}, config.elementsOptions || {}, { clientSecret: values[1] });
    record.elements = record.stripe.elements(options);
    for (const mount of ownSurfaceElements(record.root, "[" + ELEMENT_ATTR + "]")) {
      mountElement(record, mount, mount.getAttribute(ELEMENT_ATTR), false);
    }
    for (const control of ownSurfaceElements(record.root, "[" + CONFIRM_ATTR + "]")) bindElementsConfirm(record, control);
    setState(record.root, "ready");
    emit(record, "ready", "surface");
  }

  async function mountEmbedded(record) {
    const config = configFor(record.root);
    setState(record.root, "loading");
    record.stripe = await stripeFor(config);
    if (isAborted(record)) return;
    const checkout = await record.stripe.initEmbeddedCheckout({
      fetchClientSecret: function() { return requestClientSecret(record, config.sessionAction); },
    });
    if (isAborted(record)) {
      if (checkout && typeof checkout.destroy === "function") checkout.destroy();
      return;
    }
    record.checkout = checkout;
    checkout.mount(record.root);
    setState(record.root, "ready");
    emit(record, "ready", "surface");
  }

  function checkoutActions(record) {
    if (!record.actionsPromise) record.actionsPromise = record.checkout.loadActions();
    return record.actionsPromise;
  }

  function bindCheckoutConfirm(record, button) {
    const listener = async function(event) {
      event.preventDefault();
      setState(button, "submitting");
      emit(record, "status", "confirm", { element: boundedID(button.id), status: "submitting" });
      try {
        const result = await checkoutActions(record);
        if (!result || result.type !== "success" || !result.actions || typeof result.actions.confirm !== "function") {
          throw new Error("checkout_actions_unavailable");
        }
        const confirmation = await result.actions.confirm({});
        if (confirmation && confirmation.type === "error") throw confirmation.error;
        if (isAborted(record)) return;
        setState(button, "complete");
        emit(record, "complete", "confirm", {
          element: boundedID(button.id),
          authoritative: false,
        });
      } catch (error) {
        reportError(record, "confirm", error, button);
      }
    };
    record.context.listen(button, "click", listener);
  }

  async function mountCheckout(record) {
    const config = configFor(record.root);
    setState(record.root, "loading");
    const values = await Promise.all([stripeFor(config), requestClientSecret(record, config.sessionAction)]);
    if (isAborted(record)) return;
    record.stripe = values[0];
    const init = { clientSecret: values[1] };
    if (config.elementsOptions) init.elementsOptions = config.elementsOptions;
    record.checkout = await record.stripe.initCheckout(init);
    if (isAborted(record)) return;
    if (record.checkout && typeof record.checkout.on === "function") {
      const listener = function() { emit(record, "status", "checkout", { status: "changed" }); };
      record.checkout.on("change", listener);
      record.releases.push(function() {
        if (typeof record.checkout.off === "function") record.checkout.off("change", listener);
      });
    }
    for (const mount of ownSurfaceElements(record.root, "[" + CHECKOUT_ELEMENT_ATTR + "]")) {
      mountElement(record, mount, mount.getAttribute(CHECKOUT_ELEMENT_ATTR), true);
    }
    for (const button of ownSurfaceElements(record.root, "[" + CHECKOUT_CONFIRM_ATTR + "]")) {
      bindCheckoutConfirm(record, button);
    }
    setState(record.root, "ready");
    emit(record, "ready", "surface");
  }

  function disposeRecord(record) {
    if (!record || record.disposed) return;
    record.disposed = true;
    for (const release of record.releases.splice(0)) {
      try { release(); } catch (_) {}
    }
    for (const mounted of record.mounted.splice(0)) {
      try {
        if (mounted && typeof mounted.destroy === "function") mounted.destroy();
        else if (mounted && typeof mounted.unmount === "function") mounted.unmount();
      } catch (_) {}
    }
    try {
      if (record.checkout && typeof record.checkout.destroy === "function") record.checkout.destroy();
      else if (record.checkout && typeof record.checkout.unmount === "function") record.checkout.unmount();
    } catch (_) {}
    if (state.records.get(record.root) === record) state.records.delete(record.root);
    setState(record.root, "disposed");
  }

  function surfaceFactory(kind, mount) {
    return function(context) {
      const existing = state.records.get(context.root);
      if (existing) disposeRecord(existing);
      const record = {
        kind,
        root: context.root,
        context,
        disposed: false,
        mounted: [],
        releases: [],
        stripe: null,
        elements: null,
        checkout: null,
        actionsPromise: null,
      };
      state.records.set(record.root, record);
      Promise.resolve()
        .then(function() { return mount(record); })
        .catch(function(error) { reportError(record, "mount", error); });
      return { dispose: function() { disposeRecord(record); } };
    };
  }

  state.stripeFor = stripeFor;
  state.dispose = disposeRecord;
  gosxHost.surfaces.register(SURFACE_ELEMENTS, surfaceFactory("elements", mountElements));
  gosxHost.surfaces.register(SURFACE_EMBEDDED, surfaceFactory("embedded-checkout", mountEmbedded));
  gosxHost.surfaces.register(SURFACE_CHECKOUT, surfaceFactory("checkout", mountCheckout));
})();
