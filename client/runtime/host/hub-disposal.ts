// @ts-check
// GoSX browser host: hub disposal.
// 30f — hub disconnect.
//
// Chunks: bootstrap.js, bootstrap-feature-hubs.js.
// Closes the sockets 30c opened and drops the hub record.
  function disconnectHub(hubID) {
    const record = window.__gosx.hubs.get(hubID);
    if (!record) return;

    if (record.reconnectTimer) {
      clearTimeout(record.reconnectTimer);
      record.reconnectTimer = null;
    }
    if (record.refreshTimer != null) {
      clearTimeout(record.refreshTimer);
      record.refreshTimer = null;
    }
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
    if (record.socket && typeof record.socket.close === "function") {
      try {
        record.socket.close();
      } catch (e) {
        console.error(`[gosx] disconnect error for hub ${hubID}:`, e);
      }
    }

    window.__gosx.hubs.delete(hubID);
  }

  gosxHost.hubs = Object.assign(gosxHost.hubs || {}, { disconnect: disconnectHub });
  gosxHostCompatibility.install("__gosx_disconnect_hub", disconnectHub);
