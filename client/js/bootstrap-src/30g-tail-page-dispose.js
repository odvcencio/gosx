// 30g — page disposal and persistent scene engines across soft navigations.
//
// Chunks: bootstrap.js only. Soft navigation calls disposePage, which keeps
// the scene engines the reuse rule allows and disposes the rest.
// Calls into 30d, 30e and 30f.
  async function disposePage(reuseEngineIDs) {
    const reuseIDs = reuseEngineIDs instanceof Set ? reuseEngineIDs : new Set();
    goWASMEnginePageGeneration += 1;
    for (const pending of Array.from(pendingEngineRuntimes.values())) {
      disposePendingEngine(pending, true);
    }
    for (const islandID of Array.from(window.__gosx.islands.keys())) {
      window.__gosx_dispose_island(islandID);
    }
    if (window.__gosx.computeIslands) {
      for (const islandID of Array.from(window.__gosx.computeIslands.keys())) {
        window.__gosx_dispose_compute_island(islandID);
      }
    }
    for (const engineID of Array.from(window.__gosx.engines.keys())) {
      if (reuseIDs.has(engineID)) {
        const record = window.__gosx.engines.get(engineID);
        reportEngineReuseTelemetry(engineID, record && record.component, record && record.mount);
        continue; // carried across the navigation — see window.__gosx_reusable_engines
      }
      window.__gosx_dispose_engine(engineID);
    }
    for (const hubID of Array.from(window.__gosx.hubs.keys())) {
      window.__gosx_disconnect_hub(hubID);
    }
    if (window.__gosx.controllers) {
      for (const controllerID of Array.from(window.__gosx.controllers.keys())) {
        window.__gosx_dispose_controller(controllerID);
      }
    }
    disposeManagedMotion();
    disposeManagedTextLayouts();
    pendingManifest = null;
    window.__gosx.ready = false;
  }

  // --------------------------------------------------------------------------
  // Persistent scene engines across soft navigations
  // --------------------------------------------------------------------------
  //
  // Soft navigation (server/navigation_runtime.js) used to dispose EVERY
  // mounted engine on every navigate() and let bootstrapPage() re-mount from
  // the incoming manifest — so a page-spanning background (e.g. a Scene3D
  // starfield) tore down and rebuilt its WebGL/WebGPU context on every nav,
  // producing a visible re-mount blink even though the scene never actually
  // changed. window.__gosx_reusable_engines lets the navigation runtime
  // identify engines it can carry across a navigation instead: disposePage
  // (above) skips disposing them, replaceBody moves the LIVE mount element
  // into the new body instead of cloning a fresh one (a same-document move
  // preserves the canvas's rendering context; removal+recreation does not),
  // and mountAllEngines skips remounting them.
  //
  // Reuse rule (deliberately conservative — this is a same-document DOM
  // move, not a real diff/patch, so false positives would visibly break the
  // page): an engine is reused ONLY when, for the SAME engine id, the
  // outgoing and incoming manifest entries have the identical component,
  // the identical mountId (the actual DOM element being kept), AND
  // byte-identical serialized `props` (the structural scene payload — geometry,
  // materials, lights, post-FX, quality ladder, everything the server
  // authored). Runtime-only state that lives inside the already-mounted
  // engine instance (camera orbit position, water sim state, in-flight
  // animations, adaptive-quality rung, ...) is intentionally NOT part of the
  // comparison — preserving exactly that state across the navigation is the
  // whole point of reuse. Any other difference (including one the deep
  // serialization comparison can't see, by construction, since it only sees
  // authored props) falls back to the original dispose+remount behavior.
  function scenePayloadIdentical(outgoingEntry, incomingEntry) {
    try {
      return JSON.stringify(outgoingEntry.props || null) === JSON.stringify(incomingEntry.props || null);
    } catch (_e) {
      return false;
    }
  }

  window.__gosx_reusable_engines = function(nextDoc) {
    const reusable = new Set();
    if (!nextDoc || !pendingManifest || !Array.isArray(pendingManifest.engines)) {
      return reusable;
    }
    let nextManifest = null;
    try {
      const el = typeof nextDoc.getElementById === "function" ? nextDoc.getElementById("gosx-manifest") : null;
      if (el) nextManifest = JSON.parse(el.textContent);
    } catch (_e) {
      return reusable;
    }
    if (!nextManifest || !Array.isArray(nextManifest.engines)) {
      return reusable;
    }
    inflateManifestShaderLibs(nextManifest, { publish: false });
    const nextByID = new Map();
    for (const entry of nextManifest.engines) {
      if (entry && entry.id) nextByID.set(String(entry.id), entry);
    }
    for (const outgoingEntry of pendingManifest.engines) {
      if (!outgoingEntry || !outgoingEntry.id) continue;
      const engineID = String(outgoingEntry.id);
      const record = window.__gosx.engines.get(engineID);
      if (!record || record.disposed) continue; // nothing live to carry over
      const incomingEntry = nextByID.get(engineID);
      if (!incomingEntry) continue;
      if (String(outgoingEntry.component || "") !== String(incomingEntry.component || "")) continue;
      if (String(outgoingEntry.mountId || outgoingEntry.id || "") !== String(incomingEntry.mountId || incomingEntry.id || "")) continue;
      if (!scenePayloadIdentical(outgoingEntry, incomingEntry)) continue;
      reusable.add(engineID);
    }
    return reusable;
  };

  function reportEngineReuseTelemetry(engineID, component, mount) {
    if (mount && typeof mount.setAttribute === "function") {
      mount.setAttribute("data-gosx-engine-reused", "true");
    }
    if (typeof window !== "undefined" && typeof window.__gosx_emit === "function") {
      window.__gosx_emit("info", "engine", "engine-reused-across-navigation", {
        engineID: String(engineID || ""),
        component: String(component || ""),
      });
    }
  }

