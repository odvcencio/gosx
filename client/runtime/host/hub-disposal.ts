// @ts-check
// GoSX browser host: hub disposal.
// 30f — hub disconnect.
//
// Chunks: bootstrap.js, bootstrap-feature-hubs.js.
// Page bindings and transport connections have separate lifetimes.
  function releaseHubBindings(record) {
    if (record.refreshTimer != null) {
      clearTimeout(record.refreshTimer);
      record.refreshTimer = null;
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
    if (record.sceneCommandObservers instanceof Map) {
      record.sceneCommandObservers.forEach(function(observer) {
        try { observer.disconnect(); } catch (_) {}
      });
      record.sceneCommandObservers.clear();
    }
    if (record.pendingSceneCommands instanceof Map) record.pendingSceneCommands.clear();
    if (record.lastSceneCommandRevision instanceof Map) record.lastSceneCommandRevision.clear();
  }

  function closeHubConnection(record) {
    // Retire the record before close(): even synchronous or queued socket
    // callbacks must see that this was a deliberate disconnect.
    record.disposed = true;
    record.refCount = 0;
    window.__gosx.hubs.forEach(function(current, id) {
      if (current === record) window.__gosx.hubs.delete(id);
    });
    if (record.identity && hubConnections.get(record.identity) === record) {
      hubConnections.delete(record.identity);
    }
    if (record.reconnectTimer != null) {
      clearTimeout(record.reconnectTimer);
      record.reconnectTimer = null;
    }
    releaseHubBindings(record);
    if (record.socket && typeof record.socket.close === "function") {
      record.socket.onopen = record.socket.onmessage = record.socket.onclose = record.socket.onerror = null;
      try {
        record.socket.close();
      } catch (e) {
        console.error(`[gosx] disconnect error for hub ${record.entry.id}:`, e);
      }
    }
  }

  function disconnectHub(hubID) {
    const record = window.__gosx.hubs.get(hubID);
    if (!record) return;
    window.__gosx.hubs.delete(hubID);
    if (record.entries) {
      record.entries.delete(hubID);
      record.refCount = record.entries.size;
      if (record.refCount > 0) {
        refreshHubBindings(record);
        return;
      }
    }
    closeHubConnection(record);
  }

  function prepareHubPage(nextDoc) {
    let manifest = null;
    try {
      const el = nextDoc && nextDoc.getElementById("gosx-manifest");
      if (el) manifest = JSON.parse(el.textContent);
    } catch (_) {}
    const wanted = new Set((manifest && manifest.hubs || []).filter(canConnectHub).map(hubIdentity));
    for (const record of Array.from(hubConnections.values())) {
      if (!wanted.has(record.identity)) {
        closeHubConnection(record);
      } else {
        record.suspended = true;
        releaseHubBindings(record);
      }
    }
  }

  gosxHost.hubs = Object.assign(gosxHost.hubs || {}, { disconnect: disconnectHub, preparePage: prepareHubPage });
  gosxHostCompatibility.install("__gosx_disconnect_hub", disconnectHub);
