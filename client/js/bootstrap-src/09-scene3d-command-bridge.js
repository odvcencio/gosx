(function() {
  if (typeof window === "undefined" || window.__gosxScene3DBridge) return;
  window.__gosxScene3DBridge = true;
  var api = (window.__gosx = window.__gosx || {}).scene3d = window.__gosx.scene3d || {};
  var commandPromise = null;
  var recoveryKey = "gosx:scene3d:force-webgl-next";

  function commandURL() {
    try {
      var tag = document.querySelector('script[data-gosx-script="feature-scene3d"]');
      if (tag && tag.dataset && tag.dataset.gosxScene3dCommandUrl) return tag.dataset.gosxScene3dCommandUrl;
    } catch (_e) {}
    return "/gosx/bootstrap-feature-scene3d-command.js";
  }

  function loadCommandBridge() {
    if (window.__gosx_scene3d_command_bridge) return Promise.resolve(window.__gosx_scene3d_command_bridge);
    if (commandPromise) return commandPromise;
    commandPromise = new Promise(function(resolve, reject) {
      var script = document.createElement("script");
      script.src = commandURL();
      script.async = true;
      script.onload = function() { resolve(window.__gosx_scene3d_command_bridge); };
      script.onerror = function(err) {
        commandPromise = null;
        reject(err);
      };
      (document.head || document.documentElement || document.body).appendChild(script);
    });
    return commandPromise;
  }

  window.__gosx_scene3d_apply_command_scripts = function(root) {
    return loadCommandBridge().then(function(bridge) { return bridge && bridge.applyCommandScripts(root); });
  };

  api.dispatchCommands = function(target, commands, options) {
    return loadCommandBridge().then(function(bridge) { return bridge.dispatchCommands(target, commands, options); });
  };
  function forceWebGLRequested() {
    if (window.__gosx_scene3d_force_webgl === true) return true;
    try {
      if (!window.sessionStorage || window.sessionStorage.getItem(recoveryKey) !== "1") return false;
      window.sessionStorage.removeItem(recoveryKey);
      window.__gosx_scene3d_force_webgl = true;
      return true;
    } catch (_e) {
      return false;
    }
  }
  function requestWebGLRecovery(options) {
    var reload = options && options.reload === true;
    window.__gosx_scene3d_force_webgl = true;
    try {
      if (window.sessionStorage) window.sessionStorage.setItem(recoveryKey, "1");
    } catch (_e) {}
    if (reload && window.location && typeof window.location.reload === "function") window.location.reload();
    return { forceWebGL: true, reload: reload };
  }
  function clearWebGLRecovery() {
    window.__gosx_scene3d_force_webgl = false;
    try {
      if (window.sessionStorage) window.sessionStorage.removeItem(recoveryKey);
    } catch (_e) {}
    return { forceWebGL: false };
  }
  api.forceWebGLRequested = api.isWebGLRecoveryActive = forceWebGLRequested;
  api.requestWebGLRecovery = requestWebGLRecovery;
  api.clearWebGLRecovery = clearWebGLRecovery;
  window.__gosx_scene3d_force_webgl_requested = window.__gosx_scene3d_is_webgl_recovery_active = forceWebGLRequested;
  window.__gosx_scene3d_request_webgl_recovery = requestWebGLRecovery;
  window.__gosx_scene3d_clear_webgl_recovery = clearWebGLRecovery;
  window.__gosx_scene3d = api;
  window.__gosx_scene3d_force_webgl_requested();
})();
