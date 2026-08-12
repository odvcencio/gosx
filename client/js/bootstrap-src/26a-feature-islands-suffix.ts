
    return {
      runtimeReady(manifest) {
        return hydrateAllIslands(manifest);
      },
      disposePage() {
        for (const islandID of Array.from(window.__gosx.islands.keys())) {
          window.__gosx_dispose_island(islandID);
        }
        if (window.__gosx.computeIslands) {
          for (const islandID of Array.from(window.__gosx.computeIslands.keys())) {
            window.__gosx_dispose_compute_island(islandID);
          }
        }
      },
      disposeIsland: window.__gosx_dispose_island,
      disposeComputeIsland: window.__gosx_dispose_compute_island,
    };
  });
})();
