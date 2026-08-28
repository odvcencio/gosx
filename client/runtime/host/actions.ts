// @ts-check
// GoSX browser host: declarative interaction attributes.
// 06-declarative-actions.js — bootstrap-owned declarative interaction attributes.
//
// A single set of capturing document listeners drives discrete client intents
// with NO per-page JS, so GoSX apps stay fully declarative (this mirrors the
// data-gosx-motion subsystem in 05-document-env.ts — attribute-driven, global
// observer/listener, zero app script). Self-contained IIFE; the listeners
// resolve runtime globals (window.__gosx_set_shared_signal,
// window.__gosx_notify_shared_signal) lazily so load order relative to the
// tail is immaterial.
//
// A shared-signal write (data-gosx-set, data-gosx-action-signal) never
// depends on a WASM engine being on the page (gosx#233). setSignal below
// prefers the engine's own window.__gosx_set_shared_signal when the page
// installed one, and falls back to window.__gosx_notify_shared_signal — the
// same JS-only writer 00-textlayout.js already uses for its own
// shared-signal store — the moment that hook is absent or reports an error.
// Every subscriber, whether it reads through
// window.__gosx_subscribe_shared_signal (regions.ts, navigation.ts) or the
// engine's own store, sees the same value either way.
//
//   data-gosx-action="POST /url"    element/button → fetch(url, {Accept: json}); no reload.
//   <form data-gosx-action[="..."]>  submit → fetch URLSearchParams(FormData);
//                                    method/url default to the form's method/action.
//   data-gosx-reset                  on a data-gosx-action form → clear text inputs on 2xx.
//   data-gosx-submit-on="change"     input → el.form.requestSubmit() on change.
//   data-gosx-set="$signal"          element → on click, set the shared signal to
//                                    data-gosx-set-value (or ""). No WASM engine required.
//   data-gosx-action-event="name"    dispatch the result under a custom event name.
//   data-gosx-action-signal="$name"  write result.value to a shared signal. No WASM
//                                    engine required.
//   data-gosx-action-target="#id"     replace a target with result.html.
//   data-gosx-toggle-target="#id"     toggle an attribute on another element.
//   data-gosx-toggle-attribute="open" attribute name (defaults to data-gosx-open).
//   data-gosx-toggle-close="#id"      remove the configured attribute.
//   data-gosx-bind-source="selector"   project attributes from a selected source
//                                     into descendants using data-gosx-bind-text
//                                     and data-gosx-bind-attr="to:from".
//
// disclosure.ts exclusively owns data-gosx-disclosure-* and publishes its
// authority as window.__gosx.disclosure.
//
// Discrete actions reuse existing idempotent HTTP endpoints whose results the
// server re-broadcasts over the hub, so bound islands re-render with no response
// handling here. Streaming/outbound state uses BindHub outbound bindings, not this.
//
// CSRF: the core request transport attaches X-CSRF-Token to
// POST/PUT/PATCH/DELETE requests when the page carries a <meta name="csrf-token">
// tag — the mirror of
// m31labs.dev/gosx/session.Manager.Protect's expected header (session.go's
// Protect reads r.Header.Get("X-CSRF-Token"), falling back to a csrf_token
// form field only for non-JSON requests; actionFetch's Accept is always
// "application/json", so the header is the only path that reaches it). Apps
// without Protect mounted never render the meta tag, so no header is sent.
// GET requests (Protect's csrfProtectedMethod ignores them) never get the
// header, matching the server's own method filter.
(function () {
  gosxHost.state = gosxHost.state || {};
  if (typeof document === "undefined" || gosxHost.state.declarativeActions) return;
  gosxHost.state.declarativeActions = true;

  // notifySharedSignalFallback is the JS-only half of the shared-signal
  // write path (gosx#233). window.__gosx_notify_shared_signal is
  // 00-textlayout.js's own store writer — the same one it uses when ITS
  // engine hook is absent — so a subscriber reached through
  // window.__gosx_subscribe_shared_signal sees an identical update whether
  // a WASM engine ever mounted or not. 00-textlayout.js loads before
  // actions.ts in every bundle that carries this file (bootstrap.js,
  // bootstrap-lite.js, bootstrap-runtime.js — see cmd/buildbootstrap's
  // outputs), so the guard below only ever fires for a page that loads
  // actions.ts through some other path.
  function notifySharedSignalFallback(name, valueJSON) {
    var notify = window.__gosx_notify_shared_signal;
    if (typeof notify === "function") {
      notify(name, valueJSON);
      return;
    }
    console.warn("[gosx] no shared signal writer installed; \"" + name + "\" was not written");
  }

  function setSignal(name, value) {
    if (!name) return;
    var valueJSON = JSON.stringify(value);
    var setSharedSignal = window.__gosx_set_shared_signal;
    if (typeof setSharedSignal === "function") {
      try {
        var result = setSharedSignal(name, valueJSON);
        if (typeof result === "string" && result !== "") {
          console.warn("[gosx] declarative set", name, result);
          notifySharedSignalFallback(name, valueJSON);
        }
        return;
      } catch (e) {
        console.warn("[gosx] declarative set", name, e);
      }
    }
    notifySharedSignalFallback(name, valueJSON);
  }

  function isMutatingMethod(method) {
    switch (String(method || "").toUpperCase()) {
      case "POST":
      case "PUT":
      case "PATCH":
      case "DELETE":
        return true;
      default:
        return false;
    }
  }

  function gosxActionRequest(url, opts) {
    if (window.__gosx && typeof window.__gosx.request === "function") {
      return window.__gosx.request(url, opts);
    }
    // Keep isolated action fragments compatible when the full core bootstrap
    // is intentionally absent. The standard path above owns this policy.
    var fallback = Object.assign({}, opts || {});
    var headers = Object.assign({}, fallback.headers || {});
    if (isMutatingMethod(fallback.method) && !Object.keys(headers).some(function (key) {
      return String(key).toLowerCase() === "x-csrf-token";
    })) {
      var meta = document.querySelector('meta[name="csrf-token"]');
      var token = meta ? meta.getAttribute("content") || "" : "";
      if (token) headers["X-CSRF-Token"] = token;
    }
    fallback.headers = headers;
    return fetch(url, fallback);
  }

  var SUBMITTER_ATTRS = {
    formAction: "formaction",
    formMethod: "formmethod",
  };

  function actionFetch(el, method, url, body, contractEl) {
    var actionEl = contractEl || el;
    var opts = { method: method, headers: { Accept: "application/json" } };
    if (body !== undefined) {
      opts.headers["Content-Type"] = "application/x-www-form-urlencoded";
      opts.body = body;
    }
    if (el && "disabled" in el) el.disabled = true;
    return gosxActionRequest(url, opts)
      .then(function (r) {
        // Re-enable on settle (success OR failure), not just on failure: a
        // persistent submit button (composer send, comment Pin, suggest) must
        // be usable again after a successful 2xx. Buttons that re-render away
        // (accept/dismiss) are replaced anyway, so re-enabling is harmless.
        if (el && "disabled" in el) el.disabled = false;
        if (!r.ok) {
          console.warn("[gosx] action failed", method, url, r.status);
          reportActionFailure(
            "action response",
            new Error("action failed with status " + (r.status || 0)),
            {
              element: el,
              source: url,
              telemetry: { method: method, url: url, status: r.status || 0 },
            }
          );
        }
        return responsePayload(r).then(function (result) {
          applyActionResult(actionEl, result, el);
          var detail = {
            element: el,
            actionElement: actionEl,
            method: method,
            url: url,
            ok: !!r.ok,
            status: r.status || 0,
            response: r,
            result: result,
          };
          dispatchActionEvent("gosx:action:result", detail);
          if (actionEl && typeof actionEl.getAttribute === "function") {
            var eventName = actionEl.getAttribute("data-gosx-action-event");
            if (eventName) dispatchActionEvent(eventName, detail);
          }
          if (!r.ok) dispatchActionEvent("gosx:action:error", detail);
          observeAction(r.ok ? "info" : "warn", r.ok ? "action completed" : "action failed", {
            method: method,
            url: url,
            status: r.status || 0,
          });
          return r;
        });
      })
      .catch(function (err) {
        if (el && "disabled" in el) el.disabled = false;
        console.warn("[gosx] action error", method, url, err);
        dispatchActionEvent("gosx:action:error", {
          element: el,
          method: method,
          url: url,
          ok: false,
          status: 0,
          error: err,
        });
        reportActionFailure("action request", err, {
          element: el,
          source: url,
          telemetry: { method: method, url: url },
        });
        observeAction("error", "action request failed", { method: method, url: url });
        return null;
      });
  }

  // "POST /url" → {method,url}; "/url" → {fallbackMethod, url}.
  function parseAction(spec, fallbackMethod) {
    var s = String(spec || "").trim();
    var sp = s.indexOf(" ");
    if (sp > 0) return { method: s.slice(0, sp).toUpperCase(), url: s.slice(sp + 1).trim() };
    return { method: (fallbackMethod || "POST").toUpperCase(), url: s };
  }

  function dispatchActionEvent(type, detail) {
    if (typeof document.dispatchEvent !== "function" || typeof CustomEvent !== "function") return;
    document.dispatchEvent(new CustomEvent(type, { detail: detail }));
  }

  function observeAction(level, message, fields) {
    if (typeof window.__gosx_emit !== "function") return;
    window.__gosx_emit(level, "action", message, fields || {});
  }

  function reportActionFailure(operation, error, fields) {
    if (window.__gosx && typeof window.__gosx.reportFailure === "function") {
      return window.__gosx.reportFailure(operation, error, Object.assign({
        scope: "action",
        type: "action",
        fallback: "server",
      }, fields || {}));
    }
    if (window.__gosx && typeof window.__gosx.reportIssue === "function") {
      return window.__gosx.reportIssue(Object.assign({
        scope: "action",
        type: "action",
        severity: "warning",
        error,
        fallback: "server",
      }, fields || {}));
    }
    return null;
  }

  function responsePayload(response) {
    if (!response) return Promise.resolve(null);
    if (window.__gosx && window.__gosx.transport && typeof window.__gosx.transport.json === "function") {
      return window.__gosx.transport.json(response);
    }
    var candidate = typeof response.clone === "function" ? response.clone() : response;
    if (candidate && typeof candidate.json === "function") {
      return candidate.json().catch(function () { return null; });
    }
    return Promise.resolve(null);
  }

  function actionResultValue(result) {
    if (result && Object.prototype.hasOwnProperty.call(result, "value")) return result.value;
    if (result && result.data && Object.prototype.hasOwnProperty.call(result.data, "value")) return result.data.value;
    return undefined;
  }

  function actionResultHTML(result) {
    if (result && typeof result.html === "string") return result.html;
    if (result && result.data && typeof result.data.html === "string") return result.data.html;
    return "";
  }

  function focusAfterAction(target, trigger) {
    if (!target || !trigger) return;
    if (trigger.isConnected && typeof trigger.focus === "function") {
      try {
        trigger.focus({ preventScroll: true });
      } catch (_) {
        trigger.focus();
      }
      return;
    }
    if (typeof target.querySelector !== "function") return;
    var next = target.querySelector('button:not([disabled]), a[href], input:not([disabled]), select:not([disabled]), textarea:not([disabled]), [tabindex]:not([tabindex="-1"])');
    if (!next || typeof next.focus !== "function") return;
    try {
      next.focus({ preventScroll: true });
    } catch (_) {
      next.focus();
    }
  }

  function applyActionResult(el, result, trigger) {
    if (!el || !result) return;
    var signal = typeof el.getAttribute === "function" ? el.getAttribute("data-gosx-action-signal") : "";
    var value = actionResultValue(result);
    if (signal && value !== undefined) setSignal(signal, value);

    var selector = typeof el.getAttribute === "function" ? el.getAttribute("data-gosx-action-target") : "";
    var html = actionResultHTML(result);
    if (!selector || !html || typeof document.querySelector !== "function") return;
    var target = document.querySelector(selector);
    if (!target) return;
    if (typeof gosxHostCompatibility.read("__gosx_replace_runtime_content") === "function") {
      // gosxHost.dom.replace is a forwarding shim that always exists (see
      // compatibility.ts), so the ambient name it forwards to is the only
      // reliable signal that dom.ts is actually loaded. Once it is,
      // gosxHost.dom.replace owns the whole replace lifecycle, including its
      // own failure path (see dom.ts replaceRuntimeContent): it disposes the
      // old subtree, and on failure may have already written the new HTML
      // before returning false. Return unconditionally so a failed managed
      // replace does not fall through to the unmanaged dispose/innerHTML/
      // stream-consume/mount path below and double-apply on top of it.
      gosxHost.dom.replace(target, html);
      focusAfterAction(target, trigger);
      return;
    }
    if (gosxHost.surfaces && typeof gosxHost.surfaces.dispose === "function") {
      gosxHost.surfaces.dispose(target);
    }
    target.innerHTML = html;
    if (gosxHost.stream && typeof gosxHost.stream.consume === "function") {
      gosxHost.stream.consume(target);
    }
    if (gosxHost.surfaces && typeof gosxHost.surfaces.mount === "function") {
      gosxHost.surfaces.mount(target);
    }
    if (gosxHost.regions && typeof gosxHost.regions.mount === "function") {
      gosxHost.regions.mount(target);
    }
    focusAfterAction(target, trigger);
  }

  function submitterAttribute(submitter, name) {
    if (!submitter) return "";
    var attrName = SUBMITTER_ATTRS[name] || String(name || "").toLowerCase();
    if (!submitter.hasAttribute || !submitter.hasAttribute(attrName)) return "";
    var property = submitter[name];
    if (typeof property === "string" && property) return property;
    return typeof submitter.getAttribute === "function" ? String(submitter.getAttribute(attrName) || "") : "";
  }

  function formActionSpec(form, submitter) {
    var spec = (form.getAttribute("data-gosx-action") || "").trim();
    if (spec) return parseAction(spec, submitterAttribute(submitter, "formMethod") || form.getAttribute("method") || "POST");
    return {
      method: (submitterAttribute(submitter, "formMethod") || form.getAttribute("method") || "POST").toUpperCase(),
      url: submitterAttribute(submitter, "formAction") || form.getAttribute("action") || "",
    };
  }

  function formBody(form, submitter) {
    var formData = new FormData(form);
    var name = submitter && (submitter.name || (submitter.getAttribute && submitter.getAttribute("name")));
    if (name && (!formData.has || !formData.has(name))) {
      var value = submitter.value || (submitter.getAttribute && submitter.getAttribute("value")) || "";
      if (typeof formData.append === "function") formData.append(name, value);
    }
    return new URLSearchParams(formData);
  }

  function captureFormState(form) {
    if (!form || !form.getAttribute) return { pending: null, state: null };
    return {
      pending: form.getAttribute("data-gosx-pending"),
      state: form.getAttribute("data-gosx-form-state"),
    };
  }

  function setFormPending(form) {
    if (!form || !form.setAttribute) return;
    form.setAttribute("data-gosx-pending", "true");
    form.setAttribute("data-gosx-form-state", "pending");
  }

  function restoreFormState(form, snapshot) {
    if (!form || !form.setAttribute) return;
    var previous = snapshot || { pending: null, state: null };
    if (previous.pending == null) {
      if (form.removeAttribute) form.removeAttribute("data-gosx-pending");
    } else {
      form.setAttribute("data-gosx-pending", previous.pending);
    }
    form.setAttribute("data-gosx-form-state", previous.state == null ? "idle" : previous.state);
  }

  function formContractElement(form, submitter) {
    if (!submitter || !submitter.hasAttribute) return form;
    if (submitter.hasAttribute("data-gosx-action-target") ||
      submitter.hasAttribute("data-gosx-action-event") ||
      submitter.hasAttribute("data-gosx-action-signal")) {
      return submitter;
    }
    return form;
  }

  function targetFor(selector) {
    if (!selector || typeof document.querySelector !== "function") return null;
    try { return document.querySelector(selector); } catch (_) { return null; }
  }

  function toggleAttribute(trigger, forceClose) {
    var selector = trigger.getAttribute(forceClose ? "data-gosx-toggle-close" : "data-gosx-toggle-target");
    if (forceClose && !selector) selector = trigger.getAttribute("data-gosx-toggle-target");
    var target = targetFor(selector);
    if (!target) return false;
    var attribute = trigger.getAttribute("data-gosx-toggle-attribute") || "data-gosx-open";
    var open = forceClose ? false : !target.hasAttribute(attribute);
    if (open) target.setAttribute(attribute, "true");
    else target.removeAttribute(attribute);
    var controller = forceClose
      ? targetFor('[data-gosx-toggle-target="' + selector + '"]')
      : trigger;
    if (controller && typeof controller.setAttribute === "function") {
      controller.setAttribute("aria-expanded", open ? "true" : "false");
    }
    return true;
  }

  function bindAttribute(target, spec, source) {
    String(spec || "").split(";").forEach(function (mapping) {
      var split = mapping.indexOf(":");
      if (split < 1) return;
      var targetName = mapping.slice(0, split).trim();
      var sourceName = mapping.slice(split + 1).trim();
      var value = source.getAttribute(sourceName);
      if (!targetName) return;
      if (value === null || value === "") target.removeAttribute(targetName);
      else target.setAttribute(targetName, value);
    });
  }

  function bindSource(root) {
    var selector = root.getAttribute("data-gosx-bind-source");
    var source = targetFor(selector);
    if (!source) return false;
    var targets = [root];
    if (typeof root.querySelectorAll === "function") {
      targets = targets.concat(Array.prototype.slice.call(root.querySelectorAll("[data-gosx-bind-text], [data-gosx-bind-attr]")));
    }
    targets.forEach(function (target) {
      var textAttribute = target.getAttribute && target.getAttribute("data-gosx-bind-text");
      if (textAttribute) {
        var value = source.getAttribute(textAttribute);
        if (value !== null) target.textContent = value;
      }
      var attributeSpec = target.getAttribute && target.getAttribute("data-gosx-bind-attr");
      if (attributeSpec) bindAttribute(target, attributeSpec, source);
    });
    return true;
  }

  function refreshBindings() {
    if (typeof document.querySelectorAll !== "function") return;
    document.querySelectorAll("[data-gosx-bind-source]").forEach(bindSource);
  }

  document.addEventListener(
    "click",
    function (e) {
      var t = e.target;
      if (!t || !t.closest) return;
      var toggleClose = t.closest("[data-gosx-toggle-close]");
      if (toggleClose) {
        toggleAttribute(toggleClose, true);
        return;
      }
      var toggle = t.closest("[data-gosx-toggle-target]");
      if (toggle) {
        e.preventDefault();
        toggleAttribute(toggle, false);
        return;
      }
      var setEl = t.closest("[data-gosx-set]");
      if (setEl) {
        e.preventDefault();
        setSignal(setEl.getAttribute("data-gosx-set"), setEl.getAttribute("data-gosx-set-value") || "");
        return;
      }
      var act = t.closest("[data-gosx-action]");
      if (act && act.tagName !== "FORM") {
        e.preventDefault();
        var a = parseAction(act.getAttribute("data-gosx-action"), "POST");
        if (a.url) actionFetch(act, a.method, a.url);
      }
    },
    true
  );

  document.addEventListener(
    "submit",
    function (e) {
      var f = e.target;
      if (!f || !f.matches || !f.matches("form[data-gosx-action]")) return;
      e.preventDefault();
      var submit = e.submitter || f.querySelector("[type=submit]");
      var a = formActionSpec(f, submit);
      if (!a.url) return;
      var previous = captureFormState(f);
      setFormPending(f);
      actionFetch(submit, a.method, a.url, formBody(f, submit), formContractElement(f, submit)).then(function (r) {
        if (r && r.ok && f.hasAttribute("data-gosx-reset")) {
          f.querySelectorAll("input[type=text]").forEach(function (i) {
            i.value = "";
          });
        }
      }).finally(function () {
        restoreFormState(f, previous);
      });
    },
    true
  );

  document.addEventListener("keydown", function (e) {
    if (!e) return;
    if (e.key === "Escape") {
      if (typeof document.querySelectorAll === "function") {
        document.querySelectorAll("[data-gosx-toggle-target][aria-expanded='true']").forEach(function (trigger) {
          toggleAttribute(trigger, true);
        });
      }
    }
  }, true);

  document.addEventListener("gosx:navigate", function () {
    refreshBindings();
    if (typeof setTimeout === "function") setTimeout(refreshBindings, 0);
  });
  if (document.readyState === "loading") {
    document.addEventListener("DOMContentLoaded", refreshBindings, { once: true });
  }
  refreshBindings();

  document.addEventListener(
    "change",
    function (e) {
      var t = e.target;
      if (t && t.matches && t.matches("[data-gosx-submit-on='change']") && t.form && t.form.requestSubmit) {
        t.form.requestSubmit();
      }
    },
    true
  );

  var actionsAPI = {
    run: actionFetch,
    parse: parseAction,
    applyResult: applyActionResult,
    dispatch: dispatchActionEvent,
    refreshBindings: refreshBindings,
  };
  window.__gosx.actions = Object.assign(window.__gosx.actions || {}, actionsAPI);
  gosxHost.actions = window.__gosx.actions;
  gosxHostCompatibility.install("__gosx_declarative_actions", gosxHost.actions);
})();
