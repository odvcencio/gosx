
    return {
      runtimeReady(manifest) {
        return mountAllControllers(manifest);
      },
      disposePage() {
        if (!window.__gosx.controllers) return;
        for (const controllerID of Array.from(window.__gosx.controllers.keys())) {
          window.__gosx_dispose_controller(controllerID);
        }
      },
      disposeController: window.__gosx_dispose_controller,
    };
  });
})();
