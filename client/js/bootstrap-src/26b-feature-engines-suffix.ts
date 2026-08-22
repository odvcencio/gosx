
    return {
      runtimeReady(manifest, reuseEngineIDs, isNavigationBootstrap) {
        return Promise.all([
          mountAllEngines(manifest, reuseEngineIDs, isNavigationBootstrap),
          mountAllEngineSurfaces(),
          mountAllSurfaceKinds(),
        ]);
      },
      disposePage(reuseEngineIDs) {
        const reuseIDs = reuseEngineIDs instanceof Set ? reuseEngineIDs : new Set();
        goWASMEnginePageGeneration += 1;
        for (const pending of Array.from(pendingEngineRuntimes.values())) {
          disposePendingEngine(pending, true);
        }
        for (const engineID of Array.from(window.__gosx.engines.keys())) {
          if (reuseIDs.has(engineID)) {
            const record = window.__gosx.engines.get(engineID);
            if (record && record.mount && typeof record.mount.setAttribute === "function") {
              record.mount.setAttribute("data-gosx-engine-reused", "true");
            }
            if (typeof window !== "undefined" && typeof window.__gosx_emit === "function") {
              window.__gosx_emit("info", "engine", "engine-reused-across-navigation", {
                engineID: String(engineID || ""),
                component: String((record && record.component) || ""),
              });
            }
            continue;
          }
          gosxHost.engines.dispose(engineID);
        }
        for (const id of Array.from(surfaceInstances.keys())) {
          _disposeEngineSurface(id);
        }
      },
      disposeEngine: gosxHost.engines.dispose,
      engineFrame: gosxHost.engines.frame,
    };
  });
})();
