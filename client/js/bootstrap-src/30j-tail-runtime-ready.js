// 30j — the runtime ready callback the Go WASM binary calls once the runtime
// finishes initialization.
//
// Chunks: bootstrap.js only. Drives 30i hydration and 30b engine mounting.
  // --------------------------------------------------------------------------
  // Runtime ready callback
  // --------------------------------------------------------------------------

  // Called by the Go WASM binary once the runtime has finished initializing
  // and all exported functions (__gosx_hydrate, __gosx_action, etc.) are
  // registered. This is the signal that it is safe to hydrate islands.
  function markRuntimeReady() {
    window.__gosx.ready = true;
    refreshGosxDocumentState("ready");
  }

  function adoptRuntimeTextLayoutGlobal(name, current, adopt) {
    const value = window[name];
    if (typeof value === "function" && value !== current) {
      adopt(value);
      window[name] = current;
    }
  }

  window.__gosx_runtime_ready = function() {
    adoptRuntimeTextLayoutGlobal("__gosx_text_layout", gosxTextLayout, adoptTextLayoutImpl);
    adoptRuntimeTextLayoutGlobal("__gosx_text_layout_metrics", gosxTextLayoutMetrics, adoptTextLayoutMetricsImpl);
    adoptRuntimeTextLayoutGlobal("__gosx_text_layout_ranges", gosxTextLayoutRanges, adoptTextLayoutRangesImpl);
    refreshManagedTextLayouts();
    refreshGosxDocumentState("runtime-ready");
    refreshGosxEnvironmentState("runtime-ready");
    if (!pendingManifest) {
      markRuntimeReady();
      return;
    }

    mountAllEngines(pendingManifest, pendingEngineReuseIDs, pendingIsNavigationBootstrap).then(function() {
      return Promise.all([
        hydrateAllIslands(pendingManifest),
        connectAllHubs(pendingManifest),
        mountAllControllers(pendingManifest),
      ]);
    }).then(function() {
      markRuntimeReady();
      document.dispatchEvent(new CustomEvent("gosx:ready"));
    }).catch(function(e) {
      console.error("[gosx] bootstrap failed:", e);
      markRuntimeReady();
    });
  };
