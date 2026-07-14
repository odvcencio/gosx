// 06-declarative-actions.js — bootstrap-owned declarative interaction attributes.
//
// A single set of capturing document listeners drives discrete client intents
// with NO per-page JS, so GoSX apps stay fully declarative (this mirrors the
// data-gosx-motion subsystem in 05-document-env.js — attribute-driven, global
// observer/listener, zero app script). Self-contained IIFE; the listeners
// resolve runtime globals (window.__gosx_set_shared_signal) lazily so load order
// relative to the tail is immaterial.
//
//   data-gosx-action="POST /url"    element/button → fetch(url, {Accept: json}); no reload.
//   <form data-gosx-action[="..."]>  submit → fetch URLSearchParams(FormData);
//                                    method/url default to the form's method/action.
//   data-gosx-reset                  on a data-gosx-action form → clear text inputs on 2xx.
//   data-gosx-submit-on="change"     input → el.form.requestSubmit() on change.
//   data-gosx-set="$signal"          element → on click, set the shared signal to
//                                    data-gosx-set-value (or "").
//   data-gosx-action-event="name"    dispatch the result under a custom event name.
//   data-gosx-action-signal="$name"  write result.value to a shared signal.
//   data-gosx-action-target="#id"     replace a target with result.html.
//   data-gosx-toggle-target="#id"     toggle an attribute on another element.
//   data-gosx-toggle-attribute="open" attribute name (defaults to data-gosx-open).
//   data-gosx-toggle-close="#id"      remove the configured attribute.
//   data-gosx-disclosure-target="#id" open an accessible modal/disclosure.
//   data-gosx-disclosure-close="#id"  close it and restore trigger focus.
//   data-gosx-bind-source="selector"   project attributes from a selected source
//                                     into descendants using data-gosx-bind-text
//                                     and data-gosx-bind-attr="to:from".
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
  if (typeof document === "undefined" || window.__gosxDeclarativeActions) return;
  window.__gosxDeclarativeActions = true;

  function setSignal(name, value) {
    if (!name) return;
    if (typeof window.__gosx_set_shared_signal === "function") {
      try {
        window.__gosx_set_shared_signal(name, JSON.stringify(value));
      } catch (e) {
        console.warn("[gosx] declarative set", name, e);
      }
    }
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

  function actionFetch(el, method, url, body) {
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
          applyActionResult(el, result);
          var detail = {
            element: el,
            method: method,
            url: url,
            ok: !!r.ok,
            status: r.status || 0,
            response: r,
            result: result,
          };
          dispatchActionEvent("gosx:action:result", detail);
          if (el && typeof el.getAttribute === "function") {
            var eventName = el.getAttribute("data-gosx-action-event");
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

  function applyActionResult(el, result) {
    if (!el || !result) return;
    var signal = typeof el.getAttribute === "function" ? el.getAttribute("data-gosx-action-signal") : "";
    var value = actionResultValue(result);
    if (signal && value !== undefined) setSignal(signal, value);

    var selector = typeof el.getAttribute === "function" ? el.getAttribute("data-gosx-action-target") : "";
    var html = actionResultHTML(result);
    if (!selector || !html || typeof document.querySelector !== "function") return;
    var target = document.querySelector(selector);
    if (!target) return;
    if (typeof window.__gosx_replace_runtime_content === "function") {
      window.__gosx_replace_runtime_content(target, html);
      return;
    }
    if (typeof window.__gosx_dispose_runtime_surfaces === "function") {
      window.__gosx_dispose_runtime_surfaces(target);
    }
    target.innerHTML = html;
    if (typeof window.__gosx_mount_stream_templates === "function") {
      window.__gosx_mount_stream_templates(target);
    }
    if (typeof window.__gosx_mount_runtime_surfaces === "function") {
      window.__gosx_mount_runtime_surfaces(target);
    }
    if (typeof window.__gosx_mount_declarative_regions === "function") {
      window.__gosx_mount_declarative_regions(target);
    }
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

  function disclosurePanel(trigger) {
    var selector = trigger && trigger.getAttribute && (
      trigger.getAttribute("data-gosx-disclosure-target") ||
      trigger.getAttribute("data-gosx-disclosure-close") ||
      trigger.getAttribute("data-gosx-disclosure-backdrop")
    );
    return targetFor(selector);
  }

  function disclosureSelector(panel) {
    if (!panel) return "";
    var id = panel.getAttribute && panel.getAttribute("id");
    return id ? "#" + id : "";
  }

  function disclosureTrigger(panel) {
    var selector = disclosureSelector(panel);
    return selector ? targetFor('[data-gosx-disclosure-target="' + selector + '"]') : null;
  }

  function setDisclosureHidden(panel, hidden) {
    if (!panel) return;
    panel.hidden = hidden;
    if (hidden && typeof panel.setAttribute === "function") panel.setAttribute("hidden", "");
    else if (!hidden && typeof panel.removeAttribute === "function") panel.removeAttribute("hidden");
    var selector = disclosureSelector(panel);
    if (!selector || typeof document.querySelectorAll !== "function") return;
    document.querySelectorAll('[data-gosx-disclosure-backdrop="' + selector + '"]').forEach(function (backdrop) {
      backdrop.hidden = hidden;
      if (hidden) backdrop.setAttribute("hidden", "");
      else backdrop.removeAttribute("hidden");
    });
  }

  function openDisclosure(trigger) {
    var panel = disclosurePanel(trigger);
    if (!panel) return false;
    panel.__gosxPreviousFocus = document.activeElement || trigger;
    setDisclosureHidden(panel, false);
    trigger.setAttribute("aria-expanded", "true");
    var focusTarget = panel.querySelector && panel.querySelector("[data-gosx-disclosure-initial-focus]");
    if (!focusTarget && panel.querySelector) {
      focusTarget = panel.querySelector('button, a[href], input, select, textarea, [tabindex]:not([tabindex="-1"])');
    }
    if (focusTarget && typeof focusTarget.focus === "function") focusTarget.focus();
    return true;
  }

  function closeDisclosure(panel) {
    if (!panel) return false;
    setDisclosureHidden(panel, true);
    var trigger = disclosureTrigger(panel);
    if (trigger) trigger.setAttribute("aria-expanded", "false");
    var previous = panel.__gosxPreviousFocus || trigger;
    if (previous && typeof previous.focus === "function") previous.focus();
    panel.__gosxPreviousFocus = null;
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
      var disclosureClose = t.closest("[data-gosx-disclosure-close], [data-gosx-disclosure-backdrop]");
      if (disclosureClose) {
        if (disclosureClose.tagName !== "A" || !disclosureClose.getAttribute("href")) e.preventDefault();
        closeDisclosure(disclosurePanel(disclosureClose));
        return;
      }
      var disclosureOpen = t.closest("[data-gosx-disclosure-target]");
      if (disclosureOpen) {
        e.preventDefault();
        openDisclosure(disclosureOpen);
        return;
      }
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
      var spec = (f.getAttribute("data-gosx-action") || "").trim();
      var a = spec
        ? parseAction(spec, f.getAttribute("method") || "POST")
        : { method: (f.getAttribute("method") || "POST").toUpperCase(), url: f.getAttribute("action") || "" };
      if (!a.url) return;
      var submit = f.querySelector("[type=submit]");
      actionFetch(submit, a.method, a.url, new URLSearchParams(new FormData(f))).then(function (r) {
        if (r && r.ok && f.hasAttribute("data-gosx-reset")) {
          f.querySelectorAll("input[type=text]").forEach(function (i) {
            i.value = "";
          });
        }
      });
    },
    true
  );

  document.addEventListener("keydown", function (e) {
    if (!e) return;
    var panel = document.activeElement && document.activeElement.closest
      ? document.activeElement.closest("[data-gosx-disclosure]")
      : null;
    if (e.key === "Escape") {
      if (panel) {
        e.preventDefault();
        closeDisclosure(panel);
      }
      if (typeof document.querySelectorAll === "function") {
        document.querySelectorAll("[data-gosx-toggle-target][aria-expanded='true']").forEach(function (trigger) {
          toggleAttribute(trigger, true);
        });
      }
      return;
    }
    if (e.key !== "Tab" || !panel || !panel.querySelectorAll) return;
    var focusable = panel.querySelectorAll('a[href], button:not([disabled]), input:not([disabled]), select:not([disabled]), textarea:not([disabled]), [tabindex]:not([tabindex="-1"])');
    if (!focusable.length) return;
    var first = focusable[0];
    var last = focusable[focusable.length - 1];
    if (e.shiftKey && document.activeElement === first) {
      e.preventDefault();
      last.focus();
    } else if (!e.shiftKey && document.activeElement === last) {
      e.preventDefault();
      first.focus();
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
    openDisclosure: openDisclosure,
    closeDisclosure: closeDisclosure,
  };
  window.__gosx = window.__gosx || {};
  window.__gosx.actions = Object.assign(window.__gosx.actions || {}, actionsAPI);
  window.__gosx_declarative_actions = window.__gosx.actions;
})();
