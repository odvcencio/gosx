// 30h — browser capability probe for manifest entries.
//
// Chunks: bootstrap.js, bootstrap-feature-islands.js,
// bootstrap-feature-engines.js. Both the hydration path (30i) and the engine
// mount path (30b) call entryRequiresAsyncWebGPUProbe, so both chunks carry
// this file. The marker-based build shipped it twice without saying so.
  // --------------------------------------------------------------------------
  // Hydration
  // --------------------------------------------------------------------------

  // Hydrate a single island: fetch its program data, call __gosx_hydrate,
  // and set up event delegation on the island root element.
  function entryRequiresAsyncWebGPUProbe(entry) {
    const required = requiredCapabilityList(entry);
    return required.some((capability) => capability.indexOf("webgpu:") === 0 || capability.indexOf("webgpu-feature:") === 0);
  }

  async function prepareRuntimeCapabilityProbe(entry) {
    if (!entryRequiresAsyncWebGPUProbe(entry)) {
      return;
    }
    if (typeof window !== "undefined" && typeof window.__gosx_scene3d_webgpu_probe_ready === "function") {
      await window.__gosx_scene3d_webgpu_probe_ready();
    }
  }
