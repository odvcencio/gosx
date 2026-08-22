(function() {
  "use strict";

  let registerFeature = window.__gosx_register_bootstrap_feature;
  if (typeof registerFeature !== "function") {
    // A feature bundle can legitimately execute before the runtime bundle: an
    // app may hint features early (fetchpriority) while the runtime script is
    // emitted late in the document, and `defer` preserves document order. The
    // runtime adopts window.__gosx_bootstrap_features on boot, so registering
    // into that shared object makes load order irrelevant. Dropping the feature
    // here instead silently disabled it — engines never installed its post-FX.
    const shared = window.__gosx_bootstrap_features
      || (window.__gosx_bootstrap_features = Object.create(null));
    registerFeature = function(name, factory) {
      const key = String(name || "").trim();
      if (key && typeof factory === "function") shared[key] = factory;
    };
  }

  registerFeature("islands", function(api) {
    const fetchProgram = api.fetchProgram;
    const inferProgramFormat = api.inferProgramFormat;
    const runtimeCapabilityStatus = api.runtimeCapabilityStatus;
    const applyRuntimeCapabilityState = api.applyRuntimeCapabilityState;
    const activateInputProviders = api.activateInputProviders;
    const releaseInputProviders = api.releaseInputProviders;
    const capabilityList = api.capabilityList;
    const requiredCapabilityList = api.requiredCapabilityList;
