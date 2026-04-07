
    return {
      runtimeReady(manifest) {
        return hydrateAllIslands(manifest);
      },
      disposePage() {
        for (const islandID of Array.from(window.__gosx.islands.keys())) {
          window.__gosx_dispose_island(islandID);
        }
      },
      disposeIsland: window.__gosx_dispose_island,
    };
  });
})();
