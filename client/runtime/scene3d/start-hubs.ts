// Scene-bound hub readiness is loaded with the opt-in startup policy.
  const pendingSceneHubs = new Map();
  let pendingSceneHubObserver = null;

  function stopPendingSceneHubObserverIfIdle() {
    if (pendingSceneHubs.size !== 0 || !pendingSceneHubObserver) return;
    pendingSceneHubObserver.disconnect();
    pendingSceneHubObserver = null;
  }

  function cancelPendingSceneHub() {
    const hubID = arguments[0];
    if (hubID == null) return;
    pendingSceneHubs.delete(String(hubID));
    stopPendingSceneHubObserverIfIdle();
  }

  function cancelAllPendingSceneHubs() {
    pendingSceneHubs.clear();
    stopPendingSceneHubObserverIfIdle();
  }

  function connectPendingSceneHubs() {
    if (pendingSceneHubs.size === 0) return;
    for (const entry of Array.from(pendingSceneHubs.values())) {
      if (!hubSceneBindingsReady(entry.hub)) continue;
      pendingSceneHubs.delete(entry.id);
      entry.connect(entry.hub);
    }
    stopPendingSceneHubObserverIfIdle();
  }

  function waitForHubSceneBindings() {
    const entry = arguments[0];
    pendingSceneHubs.set(entry.id, { id: entry.id, hub: entry, connect: arguments[1] });
    if (!pendingSceneHubObserver && typeof MutationObserver === "function" && document.documentElement) {
      pendingSceneHubObserver = new MutationObserver(connectPendingSceneHubs);
      pendingSceneHubObserver.observe(document.documentElement, {
        attributes: true,
        attributeFilter: ["data-gosx-scene3d-command-ready"],
        childList: true,
        subtree: true,
      });
    }
    // Check after observing to cover a scene that became ready between the
    // initial manifest pass and observer registration.
    connectPendingSceneHubs();
  }

  function hubSceneBindingsReady() {
    const entry = arguments[0];
    const bindings = entry && Array.isArray(entry.bindings) ? entry.bindings : [];
    for (let i = 0; i < bindings.length; i += 1) {
      const binding = bindings[i];
      if (!binding || (!binding.sceneCommands && !binding.sceneInput)) continue;
      const mountID = String(binding.sceneMountId || "").trim();
      const mount = mountID ? document.getElementById(mountID) : null;
      if (!mount || mount.getAttribute("data-gosx-scene3d-command-ready") !== "true") return false;
    }
    return true;
  }

  window.__gosx_scene3d_hub_policy = {
    defer() {
      if (hubSceneBindingsReady.call(null, arguments[0])) return false;
      waitForHubSceneBindings.apply(null, arguments);
      return true;
    },
    cancel: cancelPendingSceneHub,
    clear: cancelAllPendingSceneHubs,
  };
