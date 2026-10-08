// command-bridge.ts — Scene3D browser command loading and recovery host.
// @ts-check

/**
 * @typedef {object} GoSXScene3DCommandBridge
 * @property {(target: unknown, commands: unknown[], options?: object) => Promise<unknown>} dispatchCommands
 * @property {(target: unknown, frame: ArrayBuffer|Uint8Array, options?: object) => Promise<unknown>} dispatchPoseFrame
 * @property {(target: unknown, frame: ArrayBuffer|Uint8Array, options?: object) => Promise<unknown>} dispatchMotionFrame
 * @property {(root: ParentNode) => unknown} applyCommandScripts
 */

(function() {
  if (typeof window === "undefined" || window.__gosxScene3DBridge) return;
  window.__gosxScene3DBridge = true;
  var api = (window.__gosx = window.__gosx || {}).scene3d = window.__gosx.scene3d || {};
  var recoveryKey = "gosx:scene3d:force-webgl-next";

  function loadCommandBridge() {
    return ensureSceneGatedFeatureLoaded("command", "gosxScene3dCommandUrl", "/gosx/bootstrap-feature-scene3d-command.js");
  }

  window.__gosx_scene3d_apply_command_scripts = function(root) {
    return loadCommandBridge().then(function(bridge) { return bridge && bridge.applyCommandScripts(root); });
  };

  // All public command and presentation calls share one demand-loaded bridge.
  ["dispatchCommands", "dispatchPoseFrame", "dispatchMotionFrame", "playTimeline", "burstParticles"].forEach(function(method) {
    api[method] = function() {
      const args = arguments;
      return loadCommandBridge().then(function(bridge) { return bridge[method](...args); });
    };
  });
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
