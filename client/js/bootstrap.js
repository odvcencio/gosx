// GoSX Client Bootstrap v0.2.0
// Loads shared WASM runtime, fetches per-island programs, hydrates islands
// via event delegation. This is the only JavaScript in a GoSX app.
//
// Expects:
//   - wasm_exec.js loaded before this script (standard Go WASM support)
//   - <script id="gosx-manifest" type="application/json"> with island manifest
//   - WASM exports: __gosx_hydrate, __gosx_action, __gosx_dispose
//   - WASM calls window.__gosx_runtime_ready() when Go runtime is initialized

(function() {
  "use strict";

  const GOSX_VERSION = "0.2.0";

  // --- GoSX runtime namespace ---
  window.__gosx = {
    version: GOSX_VERSION,
    islands: new Map(),   // islandID -> { component, listeners, root }
    ready: false,
  };

  // Pending manifest reference, set during init, consumed when runtime is ready.
  let pendingManifest = null;

  // --------------------------------------------------------------------------
  // Manifest loading
  // --------------------------------------------------------------------------

  // Parse the inline JSON manifest from #gosx-manifest script tag.
  // Returns the parsed object, or null if missing/malformed.
  function loadManifest() {
    const el = document.getElementById("gosx-manifest");
    if (!el) return null;

    try {
      return JSON.parse(el.textContent);
    } catch (e) {
      console.error("[gosx] failed to parse manifest:", e);
      return null;
    }
  }

  // --------------------------------------------------------------------------
  // Shared WASM runtime loading
  // --------------------------------------------------------------------------

  // Load the single shared Go WASM binary referenced by the manifest runtime
  // entry. Uses Go's wasm_exec.js `Go` class. The WASM is expected to call
  // window.__gosx_runtime_ready() once it has finished initializing its
  // exported functions (__gosx_hydrate, __gosx_action, etc.).
  async function loadRuntime(runtimeRef) {
    if (typeof Go === "undefined") {
      console.error("[gosx] wasm_exec.js must be loaded before bootstrap.js");
      return;
    }

    const go = new Go();

    try {
      const result = await WebAssembly.instantiateStreaming(
        fetch(runtimeRef.path),
        go.importObject
      );
      // go.run is intentionally not awaited — it resolves when the Go main()
      // exits, but the runtime stays alive via syscall/js callbacks.
      go.run(result.instance);
    } catch (e) {
      console.error("[gosx] failed to load WASM runtime:", e);
    }
  }

  // --------------------------------------------------------------------------
  // Island program fetching
  // --------------------------------------------------------------------------

  // Fetch the compiled program data for a single island. Returns an
  // ArrayBuffer (for "wasm" format) or a string (for "json" or other text
  // formats). Returns null on failure.
  async function fetchProgram(programRef, programFormat) {
    try {
      const resp = await fetch(programRef);
      if (!resp.ok) {
        console.error(`[gosx] failed to fetch program ${programRef}: ${resp.status}`);
        return null;
      }

      if (programFormat === "wasm") {
        return await resp.arrayBuffer();
      }
      // Default: return as text (covers json, msgpack-base64, etc.)
      return await resp.text();
    } catch (e) {
      console.error(`[gosx] error fetching program ${programRef}:`, e);
      return null;
    }
  }

  // --------------------------------------------------------------------------
  // Event delegation
  // --------------------------------------------------------------------------

  // Event types that are delegated on each island root element.
  const DELEGATED_EVENTS = [
    "click", "input", "change", "submit",
    "keydown", "keyup", "focus", "blur",
  ];

  // Extract a small payload from a DOM event for forwarding to WASM.
  function extractEventData(e) {
    const data = { type: e.type };

    switch (e.type) {
      case "input":
      case "change":
        if (e.target && e.target.value !== undefined) {
          data.value = e.target.value;
        }
        break;
      case "keydown":
      case "keyup":
        data.key = e.key;
        break;
      case "submit":
        // Prevent default form submission — the WASM handler decides what to do.
        e.preventDefault();
        break;
      // click, focus, blur: no extra data needed beyond type
    }

    return data;
  }

  // Walk up the DOM from `target` to `root` (inclusive) looking for the
  // nearest element with a `data-gosx-handler` attribute. Returns the
  // attribute value or null.
  function findHandler(target, root) {
    let el = target;
    while (el && el !== root.parentNode) {
      if (el.hasAttribute && el.hasAttribute("data-gosx-handler")) {
        return el.getAttribute("data-gosx-handler");
      }
      el = el.parentNode;
    }
    return null;
  }

  // Attach ONE delegated listener per event type on `islandRoot`. Each
  // listener walks the ancestor chain from event.target to the root looking
  // for a `data-gosx-handler` attribute. If found, it calls the WASM-side
  // __gosx_action(islandID, handlerName, eventDataJSON).
  //
  // Returns an array of { type, listener } objects so callers can remove them.
  function setupEventDelegation(islandRoot, islandID) {
    const entries = [];

    for (const eventType of DELEGATED_EVENTS) {
      const listener = function(e) {
        const handlerName = findHandler(e.target, islandRoot);
        if (!handlerName) return; // No handler on ancestor chain — ignore.

        const actionFn = window.__gosx_action;
        if (typeof actionFn !== "function") {
          console.error("[gosx] __gosx_action not available — WASM may not be ready");
          return;
        }

        const eventData = extractEventData(e);
        try {
          actionFn(islandID, handlerName, JSON.stringify(eventData));
        } catch (err) {
          console.error(`[gosx] action error (${islandID}/${handlerName}):`, err);
        }
      };

      // Use capture for focus/blur since they don't bubble.
      const useCapture = (eventType === "focus" || eventType === "blur");
      islandRoot.addEventListener(eventType, listener, useCapture);
      entries.push({ type: eventType, listener, capture: useCapture });
    }

    return entries;
  }

  // --------------------------------------------------------------------------
  // Island disposal
  // --------------------------------------------------------------------------

  // Remove all delegated event listeners for an island and clear it from the
  // tracking map. Optionally calls the WASM-side __gosx_dispose if available.
  window.__gosx_dispose_island = function(islandID) {
    const record = window.__gosx.islands.get(islandID);
    if (!record) return;

    // Remove delegated listeners from the island root.
    if (record.root && record.listeners) {
      for (const entry of record.listeners) {
        record.root.removeEventListener(entry.type, entry.listener, entry.capture);
      }
    }

    // Notify WASM side if dispose function is available.
    if (typeof window.__gosx_dispose === "function") {
      try {
        window.__gosx_dispose(islandID);
      } catch (e) {
        console.error(`[gosx] dispose error for ${islandID}:`, e);
      }
    }

    window.__gosx.islands.delete(islandID);
  };

  // --------------------------------------------------------------------------
  // Hydration
  // --------------------------------------------------------------------------

  // Hydrate a single island: fetch its program data, call __gosx_hydrate,
  // and set up event delegation on the island root element.
  async function hydrateIsland(entry) {
    const root = document.getElementById(entry.id);
    if (!root) {
      console.warn(`[gosx] island root #${entry.id} not found in DOM`);
      return;
    }

    // Skip purely static islands (no client interactivity).
    if (entry.static) return;

    // Determine program format (default to "wasm").
    const programFormat = entry.programFormat || "wasm";

    // Fetch the island's program data.
    let programData = null;
    if (entry.programRef) {
      programData = await fetchProgram(entry.programRef, programFormat);
      if (programData === null) {
        console.error(`[gosx] skipping island ${entry.id} — program fetch failed`);
        return;
      }
    }

    // Call the WASM-exported hydrate function.
    const hydrateFn = window.__gosx_hydrate;
    if (typeof hydrateFn !== "function") {
      console.error("[gosx] __gosx_hydrate not available — cannot hydrate island", entry.id);
      return;
    }

    try {
      hydrateFn(
        entry.id,                           // islandID
        entry.component,                    // componentName
        JSON.stringify(entry.props || {}),  // propsJSON
        programData,                        // program data (ArrayBuffer or string)
        programFormat                       // program format identifier
      );
    } catch (e) {
      console.error(`[gosx] failed to hydrate island ${entry.id}:`, e);
      return;
    }

    // Set up delegated event listeners on the island root.
    const listeners = setupEventDelegation(root, entry.id);

    // Track the island.
    window.__gosx.islands.set(entry.id, {
      component: entry.component,
      root: root,
      listeners: listeners,
    });
  }

  // Hydrate all islands from the manifest. Called once the WASM runtime
  // signals readiness via __gosx_runtime_ready.
  async function hydrateAllIslands(manifest) {
    if (!manifest.islands || manifest.islands.length === 0) return;

    // Hydrate islands concurrently — each is independent.
    const promises = manifest.islands.map(function(entry) {
      return hydrateIsland(entry).catch(function(e) {
        console.error(`[gosx] unexpected error hydrating ${entry.id}:`, e);
      });
    });

    await Promise.all(promises);
  }

  // --------------------------------------------------------------------------
  // Runtime ready callback
  // --------------------------------------------------------------------------

  // Called by the Go WASM binary once the runtime has finished initializing
  // and all exported functions (__gosx_hydrate, __gosx_action, etc.) are
  // registered. This is the signal that it is safe to hydrate islands.
  window.__gosx_runtime_ready = function() {
    if (!pendingManifest) {
      window.__gosx.ready = true;
      return;
    }

    hydrateAllIslands(pendingManifest).then(function() {
      window.__gosx.ready = true;
      document.dispatchEvent(new CustomEvent("gosx:ready"));
    }).catch(function(e) {
      console.error("[gosx] hydration failed:", e);
      window.__gosx.ready = true;
    });
  };

  // --------------------------------------------------------------------------
  // Main initialization
  // --------------------------------------------------------------------------

  async function init() {
    const manifest = loadManifest();
    if (!manifest) {
      // No manifest — pure server-rendered page, no islands to hydrate.
      window.__gosx.ready = true;
      return;
    }

    // Stash manifest for use when WASM signals readiness.
    pendingManifest = manifest;

    // Load the shared WASM runtime. The runtime will call
    // __gosx_runtime_ready() when it is initialized, which triggers
    // island hydration.
    if (manifest.runtime && manifest.runtime.path) {
      await loadRuntime(manifest.runtime);
    } else {
      // No runtime specified — mark ready immediately (static-only page).
      window.__gosx.ready = true;
    }
  }

  // Start when DOM is ready.
  if (document.readyState === "loading") {
    document.addEventListener("DOMContentLoaded", init);
  } else {
    init();
  }
})();
