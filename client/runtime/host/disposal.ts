// @ts-check
// GoSX browser host: island disposal.
// 30d — island disposal.
//
// Chunks: bootstrap.js, bootstrap-feature-islands.js.
// Removes the delegated listeners 30a attached, then clears the island entry.
  // --------------------------------------------------------------------------
  // Island disposal
  // --------------------------------------------------------------------------

  // Remove all delegated event listeners for an island and clear it from the
  // tracking map. Optionally calls the WASM-side __gosx_dispose if available.
  function disposeIsland(islandID) {
    const record = window.__gosx.islands.get(islandID);
    if (!record) return;

    // Remove delegated listeners from the island root.
    if (record.root && record.listeners) {
      for (const entry of record.listeners) {
        const target = entry.target || record.root;
        target.removeEventListener(entry.type, entry.listener, entry.capture);
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
  }

  function disposeComputeIsland(islandID) {
    const record = window.__gosx.computeIslands && window.__gosx.computeIslands.get(islandID);
    if (!record) return;

    releaseInputProviders(record);

    if (typeof window.__gosx_dispose === "function") {
      try {
        window.__gosx_dispose(islandID);
      } catch (e) {
        console.error(`[gosx] dispose error for compute island ${islandID}:`, e);
      }
    }

    window.__gosx.computeIslands.delete(islandID);
  }

  gosxHost.islands = Object.assign(gosxHost.islands || {}, {
    dispose: disposeIsland,
    disposeCompute: disposeComputeIsland,
  });
  gosxHostCompatibility.install("__gosx_dispose_island", disposeIsland);
  gosxHostCompatibility.install("__gosx_dispose_compute_island", disposeComputeIsland);
