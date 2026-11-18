// An opt-in manifest engine may wait for its disclosure without adding the
// observer machinery to every engine and video page.
function installDeferredEngines() {
  // Deferred mounts listen to every disclosure ancestor, including nested ones.
  // The server fallback remains usable until all of them are open.
  if (window.__gosx.deferredEngines) return window.__gosx.deferredEngines;
  const deferredEngineMounts = new Map();
  const sceneLoads = new Map();
  let generation = 0;
  function clearDeferredEngineMounts() {
    generation++;
    sceneLoads.clear();
    for (const cancel of deferredEngineMounts.values()) cancel();
    deferredEngineMounts.clear();
  }
  function deferEngineUntilOpen(entry, resume, resolveEngineMount) {
    const mount = resolveEngineMount(entry);
    if (!mount) return false;
    const details = disclosureAncestors(mount);
    if (!details.some(node => !node.open)) return false;
    const id = String(entry.id || "");
    let observer = null;
    let detached = false;
    const cancel = function() {
      if (observer) observer.disconnect();
      for (const node of details) node.removeEventListener("toggle", onToggle);
      if (deferredEngineMounts.get(id) === cancel) deferredEngineMounts.delete(id);
    };
    const onToggle = function() {
      if (document.getElementById(entry.mountId || entry.id) !== mount) return;
      for (let node = mount.parentElement; node; node = node.parentElement) {
        if (String(node.tagName || "").toLowerCase() === "details" && !node.open) return;
      }
      cancel();
      resume();
    };
    for (const node of details) node.addEventListener("toggle", onToggle);
    if (typeof MutationObserver === "function" && document.documentElement) {
      observer = new MutationObserver(function() {
        const replacement = document.getElementById(entry.mountId || entry.id);
        if (!replacement) {
          for (const node of details) node.removeEventListener("toggle", onToggle);
          detached = true;
          return;
        }
        const currentDetails = disclosureAncestors(replacement);
        if (replacement === mount && !detached && currentDetails.length === details.length &&
            currentDetails.every((node, index) => node === details[index])) return;
        cancel();
        resume();
      });
      observer.observe(document.documentElement, { childList: true, subtree: true });
    }
    deferredEngineMounts.set(id, cancel);
    return true;
  }

  // A failed network request can retry on the next user gesture. A chunk
  // that already executed requires a page reload.
  function retryOnReopen(entry, attempt, retry) {
    const id = String(entry.id || "");
    const mount = document.getElementById(entry.mountId || entry.id);
    if (!mount) return;
    const details = disclosureAncestors(mount);
    if (details.length === 0) {
      if (attempt > 0) return;
      const timer = setTimeout(function() { cancel(); retry(); }, 500);
      const cancel = function() { clearTimeout(timer); deferredEngineMounts.delete(id); };
      deferredEngineMounts.set(id, cancel);
      return;
    }
    const cancel = function() {
      document.removeEventListener("toggle", onToggle, true);
      deferredEngineMounts.delete(id);
    };
    const onToggle = function(event) {
      const ancestors = disclosureAncestors(document.getElementById(entry.mountId || entry.id));
      if (!ancestors.includes(event.target) || ancestors.some(node => !node.open)) return;
      cancel();
      retry();
    };
    document.addEventListener("toggle", onToggle, true);
    deferredEngineMounts.set(id, cancel);
  }

  function disclosureAncestors(mount) {
    const details = [];
    for (let node = mount && mount.parentElement; node; node = node.parentElement) {
      if (String(node.tagName || "").toLowerCase() === "details") details.push(node);
    }
    return details;
  }

  function start(entry, mount, resolveMount, ensureFeature, attempt = 0) {
    const id = String(entry.id || "");
    const epoch = generation;
    const resume = function(nextAttempt) {
      if (epoch !== generation) return;
      const pending = start(entry, mount, resolveMount, ensureFeature, nextAttempt);
      if (pending) pending.catch(error => console.error("[gosx] deferred engine mount failed:", error));
    };
    if (deferEngineUntilOpen(entry, () => resume(attempt), resolveMount)) return null;
    if (entry.component !== "GoSXScene3D" || !ensureFeature) return mount(entry);
    if (sceneLoads.has(id)) return null;
    sceneLoads.set(id, epoch);
    return loadScene();

    async function loadScene() {
      try {
        await ensureFeature("scene3d");
      } catch (error) {
        if (epoch !== generation) return;
        const reason = String(error?.message || error);
        const executed = reason.includes("ran but did not signal readiness");
        window.__gosx?.reportIssue?.({scope: "engine", type: "feature", component: entry.component,
          source: id, ref: "scene3d", message: "Scene3D feature unavailable: " + reason + (executed ? "; reload the page to retry" : ""), fallback: "server"});
        if (!executed) retryOnReopen(entry, attempt, () => resume(attempt + 1));
        return;
      } finally {
        if (sceneLoads.get(id) === epoch) sceneLoads.delete(id);
      }
      if (epoch !== generation) return;
      window.__gosx?.clearIssues?.(issue => issue.scope === "engine" && issue.type === "feature" && issue.source === id && issue.ref === "scene3d");
      // The user can close or replace the disclosure while the chunk downloads.
      if (!deferEngineUntilOpen(entry, () => resume(attempt), resolveMount)) return mount(entry);
    }
  }

  const helper = {start, clear: clearDeferredEngineMounts, has: id => deferredEngineMounts.has(id) || sceneLoads.has(id)};
  window.__gosx.deferredEngines = helper;
  return helper;
}
