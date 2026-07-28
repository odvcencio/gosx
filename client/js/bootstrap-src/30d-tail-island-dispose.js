// 30d — island disposal.
//
// Chunks: bootstrap.js, bootstrap-feature-islands.js.
// Removes the delegated listeners 30a attached, then clears the island entry.
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

  window.__gosx_dispose_compute_island = function(islandID) {
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
  };
