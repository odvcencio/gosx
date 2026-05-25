
    return {
      runtimeReady(manifest) {
        return Promise.all([
          mountAllEngines(manifest),
          mountAllSurfaceWASMs(),
        ]);
      },
      disposePage() {
        for (const engineID of Array.from(window.__gosx.engines.keys())) {
          window.__gosx_dispose_engine(engineID);
        }
        for (const id of Array.from(surfaceInstances.keys())) {
          _disposeSurface(id);
        }
      },
      disposeEngine: window.__gosx_dispose_engine,
      engineFrame: window.__gosx_engine_frame,
    };
  });
})();
