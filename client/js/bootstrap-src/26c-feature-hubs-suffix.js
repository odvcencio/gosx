
    return {
      runtimeReady(manifest) {
        return connectAllHubs(manifest);
      },
      disposePage() {
        for (const hubID of window.__gosx.hubs.keys()) {
          window.__gosx_disconnect_hub(hubID);
        }
      },
      disconnectHub: window.__gosx_disconnect_hub,
    };
  });
})();
