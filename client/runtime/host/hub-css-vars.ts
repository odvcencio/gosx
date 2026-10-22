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

    function formatValue(value, spec) {
      if (typeof value === "number") {
        if (!Number.isFinite(value)) return "";
        const digits = Number(spec.digits) || 0;
        return (digits > 0 ? value.toFixed(digits) : String(value)) + (spec.unit || "");
      }
      if (typeof value === "string") return value.trim();
      if (typeof value === "boolean") return value ? "1" : "0";
      return "";
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
          const text = formatValue(readPath(root, spec.path), spec);
          if (!text) continue;
          if (String(el.style.getPropertyValue(spec.name) || "").trim() === text) continue;
          el.style.setProperty(spec.name, text);
          changed++;
        }
        if (config.liveClass && !liveSent) {
          liveSent = true;
          const live = config.liveClass;
          requestAnimationFrame(function() { if (el.classList) el.classList.add(live); });
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
