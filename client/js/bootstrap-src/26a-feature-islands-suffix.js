
    return {
      runtimeReady(manifest) {
        return gosxHost.hydration.hydrateAll(manifest);
      },
      disposePage() {
        for (const islandID of Array.from(window.__gosx.islands.keys())) {
          gosxHost.islands.dispose(islandID);
        }
        if (window.__gosx.computeIslands) {
          for (const islandID of Array.from(window.__gosx.computeIslands.keys())) {
            gosxHost.islands.disposeCompute(islandID);
          }
        }
      },
      disposeIsland: window.__gosx_dispose_island,
      disposeComputeIsland: window.__gosx_dispose_compute_island,
    };
  });
})();
