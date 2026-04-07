
    return {
      runtimeReady(manifest) {
        return mountAllEngines(manifest);
      },
      disposePage() {
        for (const engineID of Array.from(window.__gosx.engines.keys())) {
          window.__gosx_dispose_engine(engineID);
        }
      },
      disposeEngine: window.__gosx_dispose_engine,
      engineFrame: window.__gosx_engine_frame,
    };
  });
})();
