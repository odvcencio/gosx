
    return {
      runtimeReady(manifest) {
        return gosxHost.hubs.connectAll(manifest);
      },
      disposePage() {
        for (const hubID of Array.from(window.__gosx.hubs.keys())) {
          gosxHost.hubs.disconnect(hubID);
        }
      },
      disconnectHub: window.__gosx_disconnect_hub,
    };
  });
})();
