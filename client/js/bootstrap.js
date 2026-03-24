// GoSX Client Bootstrap
// Loads the hydration manifest, initializes WASM bundles, and hydrates islands.
// This is the only JavaScript in a GoSX app — everything else is Go/WASM.

(function() {
  "use strict";

  const GOSX_VERSION = "0.1.0";

  // GoSX runtime namespace
  window.__gosx = {
    version: GOSX_VERSION,
    islands: new Map(),
    bundles: new Map(),
    actions: new Map(),
    ready: false,
  };

  // Load and parse the hydration manifest
  async function loadManifest() {
    const el = document.getElementById("gosx-manifest");
    if (!el) return null;

    try {
      return JSON.parse(el.textContent);
    } catch (e) {
      console.error("[gosx] failed to parse manifest:", e);
      return null;
    }
  }

  // Load a WASM bundle
  async function loadBundle(bundleId, ref) {
    if (window.__gosx.bundles.has(bundleId)) {
      return window.__gosx.bundles.get(bundleId);
    }

    const go = new Go(); // requires wasm_exec.js
    const result = await WebAssembly.instantiateStreaming(
      fetch(ref.path),
      go.importObject
    );
    go.run(result.instance);

    window.__gosx.bundles.set(bundleId, { go, instance: result.instance });
    return window.__gosx.bundles.get(bundleId);
  }

  // Hydrate a single island
  function hydrateIsland(entry) {
    const root = document.getElementById(entry.id);
    if (!root) {
      console.warn(`[gosx] island root #${entry.id} not found`);
      return;
    }

    // Skip static islands
    if (entry.static) return;

    // Call the WASM-exported hydrate function
    const hydrateFn = window.__gosx_hydrate;
    if (typeof hydrateFn !== "function") {
      console.error("[gosx] __gosx_hydrate not found — WASM bundle may not have loaded");
      return;
    }

    try {
      hydrateFn(entry.id, entry.component, JSON.stringify(entry.props));
      window.__gosx.islands.set(entry.id, { component: entry.component, active: true });
    } catch (e) {
      console.error(`[gosx] failed to hydrate island ${entry.id}:`, e);
    }

    // Bind events
    if (entry.events) {
      for (const slot of entry.events) {
        bindEvent(root, slot);
      }
    }
  }

  // Bind a DOM event to an action
  function bindEvent(root, slot) {
    const target = slot.targetSelector
      ? root.querySelector(slot.targetSelector)
      : root;

    if (!target) return;

    target.addEventListener(slot.eventType, function(e) {
      if (slot.serverAction) {
        // Server action: POST to action endpoint
        fetch("/gosx/action/" + slot.handlerName, {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({ islandId: root.id, event: slot.eventType }),
        }).then(res => {
          if (!res.ok) console.error(`[gosx] action ${slot.handlerName} failed`);
        });
      } else {
        // Client action: call WASM handler
        const handler = window.__gosx_action;
        if (typeof handler === "function") {
          handler(root.id, slot.handlerName, slot.slotId);
        }
      }
    });
  }

  // Patch the DOM for an island after a signal update
  // Called from WASM when signals change
  window.__gosx_patch = function(islandId, html) {
    const root = document.getElementById(islandId);
    if (!root) return;

    // Morphdom-style patch: replace innerHTML and rebind
    // For v0.1, simple innerHTML replacement; optimize later
    root.innerHTML = html;
  };

  // Register a client action from WASM
  window.__gosx_register_action = function(name, fn) {
    window.__gosx.actions.set(name, fn);
  };

  // Main initialization
  async function init() {
    const manifest = await loadManifest();
    if (!manifest) {
      // No islands on this page — pure server-rendered
      window.__gosx.ready = true;
      return;
    }

    // Load required bundles
    const bundlePromises = [];
    for (const [id, ref] of Object.entries(manifest.bundles)) {
      bundlePromises.push(loadBundle(id, ref));
    }
    await Promise.all(bundlePromises);

    // Hydrate all islands
    for (const island of manifest.islands) {
      hydrateIsland(island);
    }

    window.__gosx.ready = true;
    document.dispatchEvent(new CustomEvent("gosx:ready"));
  }

  // Start when DOM is ready
  if (document.readyState === "loading") {
    document.addEventListener("DOMContentLoaded", init);
  } else {
    init();
  }
})();
