(function() {
  "use strict";

  const KEY = "__gosx_runtime_watch_transport";

  function currentPath() {
    if (!window.location) {
      return "/";
    }
    return String(window.location.pathname || "/")
      + String(window.location.search || "")
      + String(window.location.hash || "");
  }

  function text(value, fallback) {
    if (typeof value === "string" && value.trim() !== "") {
      return value.trim();
    }
    if (value == null) {
      return fallback;
    }
    return String(value);
  }

  function watchRoot() {
    return document.querySelector("[data-runtime-watch]");
  }

  function setField(root, name, value) {
    if (!root) {
      return;
    }
    const node = root.querySelector('[data-runtime-watch-field="' + name + '"]');
    if (node) {
      node.textContent = value;
    }
  }

  function snapshot(trigger) {
    const documentAPI = window.__gosx && window.__gosx.document;
    const state = documentAPI && typeof documentAPI.get === "function" ? documentAPI.get() : null;
    const runtime = state && state.assets && state.assets.runtime ? state.assets.runtime : {};
    const enhancement = state && state.enhancement ? state.enhancement : {};
    const scripts = state && state.assets && Array.isArray(state.assets.scripts) ? state.assets.scripts : [];

    return {
      trigger: text(trigger, "load"),
      page: state && state.page ? text(state.page.pattern || state.page.path, currentPath()) : currentPath(),
      path: currentPath(),
      navigation: enhancement.navigation ? "enabled" : "disabled",
      bootstrap: text(runtime.bootstrapMode, enhancement.bootstrap ? "bootstrap" : "none"),
      ready: enhancement.ready ? "ready" : "booting",
      scripts: String(scripts.length),
    };
  }

  function render(trigger) {
    const root = watchRoot();
    if (!root) {
      return;
    }
    const state = snapshot(trigger);
    root.setAttribute("data-runtime-watch-ready", state.ready);
    setField(root, "trigger", state.trigger);
    setField(root, "page", state.page);
    setField(root, "path", state.path);
    setField(root, "navigation", state.navigation);
    setField(root, "bootstrap", state.bootstrap);
    setField(root, "scripts", state.scripts);
  }

  function install() {
    render("load");
    if (document.readyState === "loading") {
      document.addEventListener("DOMContentLoaded", function onReady() {
        document.removeEventListener("DOMContentLoaded", onReady);
        render("dom:ready");
      });
    }
    document.addEventListener("gosx:ready", function() {
      render("gosx:ready");
    });
    document.addEventListener("gosx:navigate", function() {
      render("gosx:navigate");
    });
    if (window.__gosx && window.__gosx.document && typeof window.__gosx.document.observe === "function") {
      window.__gosx.document.observe(function(_state, reason) {
        render("document:" + text(reason, "refresh"));
      });
    }
  }

  if (window[KEY] && typeof window[KEY].render === "function") {
    window[KEY].render("reenter");
    return;
  }

  window[KEY] = { render };
  install();
})();
