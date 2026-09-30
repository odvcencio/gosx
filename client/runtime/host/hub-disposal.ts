// @ts-check
// GoSX browser host: hub disposal.
// 30f — hub disconnect.
//
// Chunks: bootstrap.js, bootstrap-feature-hubs.js.
// Closes the sockets 30c opened and drops the hub record.
  function disconnectHub(hubID) {
    window.__gosx_scene3d_hub_policy?.cancel(hubID);
    const record = window.__gosx.hubs.get(hubID);
    if (!record) return;

    for (const key of ["reconnectTimer", "refreshTimer"]) {
      if (record[key] != null) {
        clearTimeout(record[key]);
        record[key] = null;
      }
    }
    if (typeof stopHubRoundTrip === "function") stopHubRoundTrip(record);
    record.refreshPreserveScroll = null;
    record.refreshEvent = null;
    record.refreshFetchEpoch = null;
    if (record.inputController && typeof record.inputController.dispose === "function") {
      record.inputController.dispose();
      record.inputController = null;
    }
    if (Array.isArray(record.outputUnsubscribers)) {
      record.outputUnsubscribers.forEach(function(fn) { try { fn(); } catch (_) {} });
      record.outputUnsubscribers = null;
    }
    for (const key of ["sceneCommandObservers", "pendingSceneCommands", "lastSceneCommandRevision"]) {
      const map = record[key];
      if (!(map instanceof Map)) continue;
      if (key === "sceneCommandObservers") {
        map.forEach(function(observer) { try { observer.disconnect(); } catch (_) {} });
      }
      map.clear();
    }
    if (typeof record.socket?.close === "function") {
      try {
        record.socket.close();
      } catch (e) {
        console.error(`[gosx] disconnect error for hub ${hubID}:`, e);
      }
    }

    window.__gosx.hubs.delete(hubID);
  }

  Object.assign(gosxHost.hubs ||= {}, { disconnect: disconnectHub });
  gosxHostCompatibility.install("__gosx_disconnect_hub", disconnectHub);
