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
//
// Discrete actions reuse existing idempotent HTTP endpoints whose results the
// server re-broadcasts over the hub, so bound islands re-render with no response
// handling here. Streaming/outbound state uses BindHub outbound bindings, not this.
//
// CSRF: actionFetch attaches X-CSRF-Token to POST/PUT/PATCH/DELETE requests
// when the page carries a <meta name="csrf-token"> tag — the mirror of
// m31labs.dev/gosx/session.Manager.Protect's expected header (session.go's
// Protect reads r.Header.Get("X-CSRF-Token"), falling back to a csrf_token
// form field only for non-JSON requests; actionFetch's Accept is always
// "application/json", so the header is the only path that reaches it). Apps
// without Protect mounted never render the meta tag, so gosxCSRFToken()
// returns "" and no header is sent — unchanged behavior, no parallel
// mechanism. GET requests (Protect's csrfProtectedMethod ignores them) never
// get the header, matching the server's own method filter.
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

  function gosxCSRFToken() {
    var meta = document.querySelector('meta[name="csrf-token"]');
    return meta ? meta.getAttribute("content") || "" : "";
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

  function actionFetch(el, method, url, body) {
    var opts = { method: method, headers: { Accept: "application/json" } };
    if (isMutatingMethod(method)) {
      var token = gosxCSRFToken();
      if (token) opts.headers["X-CSRF-Token"] = token;
    }
    if (body !== undefined) {
      opts.headers["Content-Type"] = "application/x-www-form-urlencoded";
      opts.body = body;
    }
    if (el && "disabled" in el) el.disabled = true;
    return fetch(url, opts)
      .then(function (r) {
        // Re-enable on settle (success OR failure), not just on failure: a
        // persistent submit button (composer send, comment Pin, suggest) must
        // be usable again after a successful 2xx. Buttons that re-render away
        // (accept/dismiss) are replaced anyway, so re-enabling is harmless.
        if (el && "disabled" in el) el.disabled = false;
        if (!r.ok) console.warn("[gosx] action failed", method, url, r.status);
        return r;
      })
      .catch(function (err) {
        if (el && "disabled" in el) el.disabled = false;
        console.warn("[gosx] action error", method, url, err);
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

  document.addEventListener(
    "click",
    function (e) {
      var t = e.target;
      if (!t || !t.closest) return;
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
})();
