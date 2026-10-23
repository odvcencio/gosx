// @ts-check
// Hub payload to CSS custom properties. Loads only for a page whose manifest
// carries a hub binding with cssVars (hydrate.HubBinding.CSSVars).
(function() {
  "use strict";
  const register = window.__gosx_register_bootstrap_feature;
  if (typeof register !== "function") return;
  register("hub-css-vars", function() {
    /** @type {any[]} */
    let appliers = [];
    /** @type {any} */
    let listener = null;

    function readPath(value, path) {
      let current = value;
      for (const key of String(path).split(".")) {
        if (current == null || typeof current !== "object" || !Object.prototype.hasOwnProperty.call(current, key)) return undefined;
        current = current[key];
      }
      return current;
    }

    // Values come from the network and may end up inside a URL-loading CSS
    // property, so each kind accepts a narrow grammar. Every kind rejects
    // control characters and values over 256 characters. The plain and ident
    // kinds also reject parentheses, backslashes and quotes, so a payload can
    // not build url(), image-set() or an escaped function name.
    const MAX_VALUE_LENGTH = 256;
    const NAME_RE = /^--[A-Za-z_][A-Za-z0-9_-]*$/;
    const PATH_RE = /^[A-Za-z0-9_]+(\.[A-Za-z0-9_]+)*$/;
    const UNIT_RE = /^[A-Za-z%]{0,16}$/;
    const KINDS = ["", "number", "length", "color", "ident", "string"];
    const NUMBER_RE = /^-?(\d+\.?\d*|\.\d+)(e[+-]?\d+)?$/i;
    const LENGTH_RE = /^-?(\d+\.?\d*|\.\d+)(e[+-]?\d+)?(%|[a-z]{1,5})?$/i;
    const COLOR_RE = /^(#[0-9a-f]{3,8}|[a-z]{3,30}|(rgb|rgba|hsl|hsla|hwb|lab|lch|oklab|oklch)\([-+0-9.,%\s\/a-z]*\))$/i;
    const IDENT_RE = /^-?[A-Za-z_][A-Za-z0-9_-]*$/;
    const CONTROL_RE = /[\u0000-\u001f\u007f]/;
    const PLAIN_BAD_RE = /[()\\"'`;{}<>]/;
    const STRING_BAD_RE = /[\\"'`<>]/;

    function formatValue(value, spec) {
      const kind = spec.kind || "";
      let text = "";
      let unit = "";
      if (typeof value === "number") {
        if (!Number.isFinite(value)) return "";
        const digits = Number(spec.digits) || 0;
        text = digits > 0 ? value.toFixed(digits) : String(value);
        unit = spec.unit || "";
      } else if (typeof value === "string") {
        text = value.trim();
      } else if (typeof value === "boolean") {
        text = value ? "1" : "0";
      }
      if (!text || text.length + unit.length > MAX_VALUE_LENGTH || CONTROL_RE.test(text)) return "";
      switch (kind) {
        case "number": return NUMBER_RE.test(text) ? text + unit : "";
        case "length": return LENGTH_RE.test(text + unit) ? text + unit : "";
        case "color": return COLOR_RE.test(text) ? text : "";
        case "ident": return IDENT_RE.test(text) ? text : "";
        case "string": return STRING_BAD_RE.test(text) ? "" : '"' + text + '"';
        default: return PLAIN_BAD_RE.test(text) ? "" : text + unit;
      }
    }

    function validSpec(spec) {
      return Boolean(spec) && NAME_RE.test(String(spec.name || "")) && PATH_RE.test(String(spec.path || "")) &&
        UNIT_RE.test(String(spec.unit || "")) && KINDS.indexOf(spec.kind || "") >= 0;
    }

    function flashSkipped() {
      try {
        if (typeof window.matchMedia === "function" && window.matchMedia("(prefers-reduced-motion: reduce)").matches) return true;
      } catch (_error) { /* treat a failing media query as no preference */ }
      if (document.visibilityState === "hidden") return true;
      return String((window.location && window.location.search) || "").indexOf("__gosx_visual_seed") >= 0;
    }

    function createApplier(entry, binding) {
      const config = binding.cssVars;
      let flashTimer = null;
      let liveSent = false;
      let lastFlashKey;
      let flashed = null;
      let liveEl = null;

      function target() {
        if (!config.target) return document.documentElement;
        try { return document.querySelector(config.target); } catch (_error) { return null; }
      }

      function apply(data) {
        const el = target();
        if (!el || !el.style) return;
        const root = config.root ? readPath(data, config.root) : data;
        if (root == null || typeof root !== "object") return;
        let changed = 0;
        for (const spec of config.vars) {
          if (!validSpec(spec)) continue;
          const text = formatValue(readPath(root, spec.path), spec);
          if (!text) continue;
          if (String(el.style.getPropertyValue(spec.name) || "").trim() === text) continue;
          el.style.setProperty(spec.name, text);
          changed++;
        }
        if (config.liveClass && !liveSent) {
          liveSent = true;
          liveEl = el;
          const live = config.liveClass;
          requestAnimationFrame(function() { if (liveEl === el && el.classList) el.classList.add(live); });
        }
        if (!changed || !config.flashClass || !el.classList) return;
        if (config.flashWhen) {
          const key = JSON.stringify(readPath(data, config.flashWhen));
          if (key === lastFlashKey) return;
          lastFlashKey = key;
        }
        if (flashSkipped()) return;
        const cls = config.flashClass;
        el.classList.remove(cls);
        void el.offsetWidth;
        el.classList.add(cls);
        flashed = el;
        if (flashTimer) clearTimeout(flashTimer);
        flashTimer = setTimeout(function() {
          flashTimer = null;
          el.classList.remove(cls);
        }, Number(config.flashMs) || 1000);
      }

      function dispose() {
        if (flashTimer) { clearTimeout(flashTimer); flashTimer = null; }
        if (flashed && flashed.classList && config.flashClass) flashed.classList.remove(config.flashClass);
        flashed = null;
        if (liveEl && liveEl.classList && config.liveClass) liveEl.classList.remove(config.liveClass);
        liveEl = null;
      }

      return { entryID: entry.id, event: binding.event, apply: apply, dispose: dispose };
    }

    function onHubEvent(event) {
      const detail = event && event.detail;
      if (!detail || !detail.data || typeof detail.data !== "object") return;
      for (const applier of appliers) {
        if (applier.entryID === detail.hubID && applier.event === detail.event) applier.apply(detail.data);
      }
    }

    function dispose() {
      for (const applier of appliers) applier.dispose();
      appliers = [];
      if (listener) {
        document.removeEventListener("gosx:hub:event", listener);
        listener = null;
      }
    }

    return {
      runtimeReady(manifest) {
        dispose();
        for (const entry of (manifest && manifest.hubs) || []) {
          for (const binding of entry.bindings || []) {
            if (binding && binding.cssVars && binding.direction !== "out" && Array.isArray(binding.cssVars.vars)) {
              appliers.push(createApplier(entry, binding));
            }
          }
        }
        if (appliers.length) {
          listener = onHubEvent;
          document.addEventListener("gosx:hub:event", listener);
        }
      },
      disposePage: dispose,
    };
  });
})();
