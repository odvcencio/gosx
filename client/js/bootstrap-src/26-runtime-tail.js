  // --------------------------------------------------------------------------
  // Selective runtime bootstrap
  // --------------------------------------------------------------------------

  const bootstrapFeatureFactories = window.__gosx_bootstrap_features || Object.create(null);
  const activeBootstrapFeatures = new Map();
  let pendingFeatureLoad = Promise.resolve([]);

  window.__gosx_bootstrap_features = bootstrapFeatureFactories;
  window.__gosx_register_bootstrap_feature = function(name, factory) {
    const featureName = String(name || "").trim();
    if (!featureName || typeof factory !== "function") {
      console.error("[gosx] invalid bootstrap feature registration");
      return;
    }
    bootstrapFeatureFactories[featureName] = factory;
  };

  function hasAttributeName(el, attr) {
    return Boolean(el && el.hasAttribute && el.hasAttribute(attr));
  }

  function bootstrapFeatureAPI() {
    return {
      engineFactories,
      fetchProgram,
      inferProgramFormat,
      engineFrame,
      cancelEngineFrame,
      capabilityList,
      requiredCapabilityList,
      runtimeCapabilityStatus,
      engineCapabilityStatus,
      browserCapabilitySupported,
      applyRuntimeCapabilityState,
      activateInputProviders,
      releaseInputProviders,
      clearChildren,
      sceneNumber,
      sceneBool,
      gosxReadSharedSignal,
      gosxNotifySharedSignal,
      gosxSubscribeSharedSignal,
      setSharedSignalJSON,
      setSharedSignalValue,
    };
  }

  function runtimeFeatureAssets() {
    if (window.__gosx && window.__gosx.document && typeof window.__gosx.document.get === "function") {
      const state = window.__gosx.document.get();
      if (state && state.assets && state.assets.runtime) {
        return state.assets.runtime;
      }
    }
    return {};
  }

  function runtimeFeaturePreloadPath(fileName) {
    const head = document && document.head;
    const nodes = head && head.children ? Array.from(head.children) : [];
    for (const node of nodes) {
      if (!node || String(node.tagName || "").toUpperCase() !== "LINK") {
        continue;
      }
      const rel = String((node.getAttribute && node.getAttribute("rel")) || node.rel || "").toLowerCase();
      const as = String((node.getAttribute && node.getAttribute("as")) || node.as || "").toLowerCase();
      const href = String((node.getAttribute && node.getAttribute("href")) || node.href || "");
      if (rel === "preload" && as === "script" && href.includes(fileName)) {
        return href;
      }
    }
    return "";
  }

  function bootstrapFeatureURL(name) {
    const assets = runtimeFeatureAssets();
    switch (name) {
      case "islands":
        return String(assets.bootstrapFeatureIslandsPath || runtimeFeaturePreloadPath("bootstrap-feature-islands") || "/gosx/bootstrap-feature-islands.js").trim();
      case "engines":
        return String(assets.bootstrapFeatureEnginesPath || runtimeFeaturePreloadPath("bootstrap-feature-engines") || "/gosx/bootstrap-feature-engines.js").trim();
      case "hubs":
        return String(assets.bootstrapFeatureHubsPath || runtimeFeaturePreloadPath("bootstrap-feature-hubs") || "/gosx/bootstrap-feature-hubs.js").trim();
      case "scene3d":
        return assets && assets.bootstrapFeatureScene3dPath;
      default:
        return "";
    }
  }

  async function ensureBootstrapFeature(name) {
    if (activeBootstrapFeatures.has(name)) {
      return activeBootstrapFeatures.get(name);
    }

    // Scene3D is loaded via an async <script> tag emitted by the Go renderer,
    // not dynamically by the runtime. Wait for it to signal readiness.
    if (name === "scene3d") {
      if (!window.__gosx_scene3d_available) {
        await new Promise(function(resolve) {
          if (window.__gosx_scene3d_available) { resolve(); return; }
          var prev = window.__gosx_scene3d_loaded;
          window.__gosx_scene3d_loaded = function() {
            if (typeof prev === "function") { prev(); }
            resolve();
          };
        });
      }
      var scene3dFeature = { name: "scene3d" };
      activeBootstrapFeatures.set(name, scene3dFeature);
      return scene3dFeature;
    }

    let factory = bootstrapFeatureFactories[name];
    if (!factory) {
      const jsRef = bootstrapFeatureURL(name);
      if (!jsRef) {
        return null;
      }
      await loadScriptTag(jsRef);
      factory = bootstrapFeatureFactories[name];
    }

    if (typeof factory !== "function") {
      console.error("[gosx] missing bootstrap feature:", name);
      return null;
    }

    try {
      const feature = factory(bootstrapFeatureAPI()) || {};
      activeBootstrapFeatures.set(name, feature);
      return feature;
    } catch (error) {
      console.error("[gosx] failed to initialize bootstrap feature " + name + ":", error);
      return null;
    }
  }

  function manifestFeatureNames(manifest) {
    const names = [];
    if (manifestHasEntries(manifest, "engines")) {
      names.push("engines");
      // Check if any engine is GoSXScene3D
      for (var i = 0; i < manifest.engines.length; i++) {
        if (manifest.engines[i].component === "GoSXScene3D") {
          names.push("scene3d");
          break;
        }
      }
    }
    if (manifestHasEntries(manifest, "hubs")) {
      names.push("hubs");
    }
    if (manifestHasEntries(manifest, "islands") || manifestHasEntries(manifest, "computeIslands")) {
      names.push("islands");
    }
    return names;
  }

  function manifestHasEntries(manifest, key) {
    return Boolean(manifest && manifest[key] && manifest[key].length > 0);
  }

  function manifestNeedsWASMRuntime(manifest) {
    return manifestHasEntries(manifest, "islands") || manifestHasEntries(manifest, "computeIslands") || manifestNeedsSharedEngineRuntime(manifest);
  }

  function manifestNeedsSharedEngineRuntime(manifest) {
    if (!manifestHasEntries(manifest, "engines")) {
      return false;
    }
    return manifest.engines.some(function(entry) {
      return entry && entry.runtime === "shared";
    });
  }

  function setSharedSignalJSON(name, valueJSON) {
    const signalName = String(name || "").trim();
    if (!signalName) {
      return null;
    }

    const setSharedSignal = window.__gosx_set_shared_signal;
    if (typeof setSharedSignal === "function") {
      try {
        const result = setSharedSignal(signalName, valueJSON);
        if (typeof result === "string" && result !== "") {
          console.error("[gosx] shared signal update error (" + signalName + "):", result);
          gosxNotifySharedSignal(signalName, valueJSON);
        }
        return result;
      } catch (error) {
        console.error("[gosx] shared signal update error (" + signalName + "):", error);
      }
    }

    gosxNotifySharedSignal(signalName, valueJSON);
    return null;
  }

  function setSharedSignalValue(name, value) {
    return setSharedSignalJSON(name, JSON.stringify(value == null ? null : value));
  }

  function ensureManifestFeatures(manifest) {
    const names = manifestFeatureNames(manifest);
    if (names.length === 0) {
      return Promise.resolve([]);
    }
    return Promise.all(names.map(function(name) {
      return ensureBootstrapFeature(name);
    })).then(function(features) {
      return features.filter(Boolean);
    });
  }

  async function runRuntimeReadyForPendingManifest() {
    if (typeof window.__gosx_text_layout === "function" && window.__gosx_text_layout !== gosxTextLayout) {
      adoptTextLayoutImpl(window.__gosx_text_layout);
      window.__gosx_text_layout = gosxTextLayout;
    }
    if (typeof window.__gosx_text_layout_metrics === "function" && window.__gosx_text_layout_metrics !== gosxTextLayoutMetrics) {
      adoptTextLayoutMetricsImpl(window.__gosx_text_layout_metrics);
      window.__gosx_text_layout_metrics = gosxTextLayoutMetrics;
    }
    if (typeof window.__gosx_text_layout_ranges === "function" && window.__gosx_text_layout_ranges !== gosxTextLayoutRanges) {
      adoptTextLayoutRangesImpl(window.__gosx_text_layout_ranges);
      window.__gosx_text_layout_ranges = gosxTextLayoutRanges;
    }
    refreshManagedTextLayouts();
    refreshGosxDocumentState("runtime-ready");
    refreshGosxEnvironmentState("runtime-ready");
    if (!pendingManifest) {
      window.__gosx.ready = true;
      refreshGosxDocumentState("ready");
      return;
    }

    const manifest = pendingManifest;
    const features = await pendingFeatureLoad;
    await Promise.all(features.map(function(feature) {
      if (!feature || typeof feature.runtimeReady !== "function") {
        return null;
      }
      return feature.runtimeReady(manifest);
    }));
    window.__gosx.ready = true;
    refreshGosxDocumentState("ready");
    document.dispatchEvent(new CustomEvent("gosx:ready"));
  }

  window.__gosx_runtime_ready = function() {
    runRuntimeReadyForPendingManifest().catch(function(error) {
      console.error("[gosx] bootstrap failed:", error);
      window.__gosx.ready = true;
      refreshGosxDocumentState("ready");
    });
  };

  async function disposePage() {
    for (const feature of Array.from(activeBootstrapFeatures.values())) {
      if (feature && typeof feature.disposePage === "function") {
        feature.disposePage();
      }
    }
    disposeManagedMotion();
    disposeManagedTextLayouts();
    pendingManifest = null;
    pendingFeatureLoad = Promise.resolve([]);
    window.__gosx.ready = false;
  }

  async function bootstrapPage() {
    refreshGosxEnvironmentState("bootstrap-page");
    refreshGosxDocumentState("bootstrap-page");
    mountManagedMotion(document.body || document.documentElement);
    mountManagedTextLayouts(document.body || document.documentElement);

    const manifest = loadManifest();
    if (!manifest) {
      pendingManifest = null;
      pendingFeatureLoad = Promise.resolve([]);
      window.__gosx.ready = true;
      refreshGosxDocumentState("ready");
      return;
    }

    pendingManifest = manifest;
    pendingFeatureLoad = ensureManifestFeatures(manifest);
    window.__gosx.ready = false;

    if (manifestNeedsWASMRuntime(manifest)) {
      if (!manifest.runtime || !manifest.runtime.path) {
        console.error("[gosx] islands and shared runtime engines require manifest.runtime.path");
        window.__gosx_runtime_ready();
        return;
      }
      if (runtimeReady()) {
        window.__gosx_runtime_ready();
        return;
      }
      await Promise.all([
        pendingFeatureLoad,
        loadRuntime(manifest.runtime),
      ]);
      return;
    }

    await pendingFeatureLoad;
    window.__gosx_runtime_ready();
  }

  window.__gosx_bootstrap_page = bootstrapPage;
  window.__gosx_dispose_page = disposePage;

  if (document.readyState === "loading") {
    document.addEventListener("DOMContentLoaded", bootstrapPage);
  } else {
    bootstrapPage();
  }
})();
