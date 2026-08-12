
    return {
      runtimeReady(manifest) {
        return gosxHost.controllers.mountAll(manifest);
      },
      disposePage() {
        if (!window.__gosx.controllers) return;
        for (const controllerID of Array.from(window.__gosx.controllers.keys())) {
            gosxHost.controllers.dispose(controllerID);
        }
      },
      disposeController: window.__gosx_dispose_controller,
    };
  });
})();
