
    return {
      runtimeReady(manifest) {
        return Promise.all([
          mountAllEngines(manifest),
          mountAllEngineSurfaces(),
          mountAllSurfaceKinds(),
        ]);
      },
      disposePage() {
        goWASMEnginePageGeneration += 1;
        for (const pending of Array.from(pendingEngineRuntimes.values())) {
          disposePendingEngine(pending, true);
        }
        for (const engineID of Array.from(window.__gosx.engines.keys())) {
          window.__gosx_dispose_engine(engineID);
        }
        for (const id of Array.from(surfaceInstances.keys())) {
          _disposeEngineSurface(id);
        }
      },
      disposeEngine: window.__gosx_dispose_engine,
      engineFrame: window.__gosx_engine_frame,
    };
  });
})();
