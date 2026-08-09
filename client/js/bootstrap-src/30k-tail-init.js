// 30k — main initialization and bench-mode exports. Starts when the DOM is
// ready.
//
// Chunks: bootstrap.js only. This is the entry point of the monolith.
  // --------------------------------------------------------------------------
  // Main initialization
  // --------------------------------------------------------------------------

  // pendingEngineReuseIDs stashes the current bootstrapPage() call's reuse
  // set for window.__gosx_runtime_ready to read — mirrors the pendingManifest
  // pattern above (window.__gosx_runtime_ready is invoked by the Go-WASM
  // binary itself once it finishes initializing, not by bootstrapPage
  // directly, so the reuse set can't just be a local variable / call
  // argument there). Reset unconditionally at the top of every bootstrapPage
  // call so a later plain call (e.g. a future page's initial load) never
  // sees a stale reuse set from a previous navigation. pendingIsNavigationBootstrap
  // records whether THIS call's ORIGINAL argument was a real Set, captured
  // before pendingEngineReuseIDs coerces a missing/non-Set argument away —
  // that original distinction is what tells a first page load apart from a
  // soft navigation (see mountAllEngines).
  let pendingEngineReuseIDs = new Set();
  let pendingIsNavigationBootstrap = false;

  async function bootstrapPage(reuseEngineIDs) {
    pendingIsNavigationBootstrap = reuseEngineIDs instanceof Set;
    pendingEngineReuseIDs = pendingIsNavigationBootstrap ? reuseEngineIDs : new Set();
    refreshGosxEnvironmentState("bootstrap-page");
    refreshGosxDocumentState("bootstrap-page");
    mountManagedMotion(document.body || document.documentElement);
    mountManagedTextLayouts(document.body || document.documentElement);

    const manifest = loadManifest();
    if (!manifest) {
      // No manifest — pure server-rendered page, no islands to hydrate.
      pendingManifest = null;
      window.__gosx.ready = true;
      refreshGosxDocumentState("ready");
      return;
    }
    // Inflate shaderLib refs in all entry props.scene objects before the manifest
    // is stashed or consumed. Downstream consumers (16a/16b/16) see inline fields
    // exactly as if the scene was never deduplicated.
    inflateManifestShaderLibs(manifest);
    initializeClientIdentity(manifest.clientIdentity);

    // Stash manifest for use when WASM signals readiness.
    pendingManifest = manifest;
    window.__gosx.ready = false;

    const needsRuntimeBridge = manifestNeedsRuntimeBridge(manifest);
    if (needsRuntimeBridge && manifest.runtime && manifest.runtime.path) {
      if (runtimeReady()) {
        window.__gosx_runtime_ready();
      } else {
        await loadRuntime(manifest.runtime);
      }
    } else {
      if (needsRuntimeBridge) {
        console.error("[gosx] missing runtime.path");
      }
      window.__gosx_runtime_ready();
    }
  }

  function manifestNeedsRuntimeBridge(manifest) {
    return manifestHasEntries(manifest, "islands")
      || manifestHasEntries(manifest, "computeIslands")
      || manifestHasEntries(manifest, "hubs")
      || !!(manifest && manifest.clientIdentity)
      || manifestNeedsVideoBridge(manifest)
      || manifestNeedsEngineInputBridge(manifest)
      || (manifestHasEntries(manifest, "engines") && manifest.engines.some(engineUsesSharedRuntime))
      || (manifest.selfDescribingSurfaces || []).some((entry) => (entry.runtime || "shared") === "shared");
  }

  function manifestNeedsEngineInputBridge(manifest) {
    return manifestHasEntries(manifest, "engines") && manifest.engines.some(function(entry) {
      const capabilities = capabilityList(entry);
      return capabilities.includes("keyboard") || capabilities.includes("pointer") || capabilities.includes("gamepad");
    });
  }

  function manifestNeedsVideoBridge(manifest) {
    return manifestHasEntries(manifest, "engines") && manifest.engines.some(function(entry) {
      return entry && entry.kind === "video";
    });
  }

  function manifestHasEntries(manifest, key) {
    return !!(manifest && manifest[key] && manifest[key].length);
  }

  window.__gosx_bootstrap_page = bootstrapPage;
  window.__gosx_dispose_page = disposePage;

  // Bench-mode exports. Activated only when window.__gosx_bench_exports
  // is set to true BEFORE the bundle runs. Zero runtime cost in production
  // — single boolean check per page load, never touches any function
  // reference unless the flag is on. The bench harness at
  // client/js/runtime.bench.js uses these to microbenchmark hot path
  // functions in isolation without standing up the full DOM mount surface.
  if (window.__gosx_bench_exports === true) {
    window.__gosx_bench = {
      // 10-runtime-scene-core.js
      sceneRenderCamera: sceneRenderCamera,
      translateScenePointInto: translateScenePointInto,
      createSceneThickLineScratch: createSceneThickLineScratch,
      expandSceneThickLineIntoScratch: expandSceneThickLineIntoScratch,
      sceneBundleNeedsThickLines: sceneBundleNeedsThickLines,
      // 16-scene-webgl.js — file-scope light/exposure helpers.
      // (The per-frame functions like buildPBRDrawList, drawPBRObjectList,
      // and render live inside createScenePBRRenderer closures and are not
      // exposed here. Measure those via a real Scene3D mount in a follow-up
      // if/when we need end-to-end per-frame numbers.)
      scenePBRLightsHash: scenePBRLightsHash,
      hashLightContent: hashLightContent,
      hashEnvironmentContent: hashEnvironmentContent,
      scenePBRUploadLights: scenePBRUploadLights,
      scenePBRUploadExposure: scenePBRUploadExposure,
      parseVideoVTT: parseVideoVTT,
      videoActiveCues: videoActiveCues,
      videoNormalizeTrackInfo: videoNormalizeTrackInfo,
      videoSubtitleOptions: videoSubtitleOptions,
      videoAudioSourceOptions: videoAudioSourceOptions,
      videoStableTrackIdentity: videoStableTrackIdentity,
      videoSubtitleRefreshPayload: videoSubtitleRefreshPayload,
    };
  }

  // Start when DOM is ready.
  if (document.readyState === "loading") {
    document.addEventListener("DOMContentLoaded", bootstrapPage);
  } else {
    bootstrapPage();
  }
})();
