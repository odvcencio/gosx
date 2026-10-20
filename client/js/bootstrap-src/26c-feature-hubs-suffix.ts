
    return {
      runtimeReady(manifest) {
        return gosxHost.hubs.connectAll(manifest);
      },
      disposePage(_reuseIDs, nextDoc) {
        gosxHost.hubs.preparePage(nextDoc);
      },
      disconnectHub: window.__gosx_disconnect_hub,
    };
  });
})();
