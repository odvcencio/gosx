
    return {
      runtimeReady(manifest) {
        return gosxHost.hubs.connectAll(manifest);
      },
      disposePage() {
        window.__gosx_scene3d_hub_policy?.clear();
        for (const hubID of Array.from(window.__gosx.hubs.keys())) {
          gosxHost.hubs.disconnect(hubID);
        }
      },
      disconnectHub: window.__gosx_disconnect_hub,
    };
  });
})();
