// @ts-check
// GoSX browser host: engine disposal.
// 30e — engine disposal.
//
// Chunks: bootstrap.js, bootstrap-feature-engines.js.
// Tears down what 30b mounted, including pending Go-WASM engine runtimes.
  function disposeEngine(engineID) {
    const pending = pendingEngineRuntimes.get(engineID);
    if (pending) disposePendingEngine(pending, true);

    const record = window.__gosx.engines.get(engineID);
    if (!record) return;
    window.__gosx.engines.delete(engineID);
    if (record.disposed) return;
    record.disposed = true;
    if (record.moduleRecord) record.moduleRecord.mountedIDs.delete(engineID);

    releaseInputProviders(record);

    if (record.runtime && typeof record.runtime.dispose === "function") {
      try {
        record.runtime.dispose();
      } catch (e) {
        console.error(`[gosx] runtime dispose error for engine ${engineID}:`, e);
      }
    }

    if (record.handle && typeof record.handle.dispose === "function") {
      try {
        record.handle.dispose();
      } catch (e) {
        console.error(`[gosx] dispose error for engine ${engineID}:`, e);
      }
    }
    if (record.fallbackSnapshot) {
      restoreGoWASMEngineFallback(record.mount, record.fallbackSnapshot);
    }
  }

  function hostEngineFrame(engineID) {
    const pending = pendingEngineRuntimes.get(engineID);
    if (pending && pending.runtime && typeof pending.runtime.frame === "function") {
      return pending.runtime.frame();
    }
    const record = window.__gosx.engines.get(engineID);
    if (!record || !record.runtime || typeof record.runtime.frame !== "function") {
      return null;
    }
    return record.runtime.frame();
  }

  gosxHost.engines = Object.assign(gosxHost.engines || {}, {
    dispose: disposeEngine,
    frame: hostEngineFrame,
  });
  gosxHostCompatibility.install("__gosx_dispose_engine", disposeEngine);
  gosxHostCompatibility.install("__gosx_engine_frame", hostEngineFrame);
