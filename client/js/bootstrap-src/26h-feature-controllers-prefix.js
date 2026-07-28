(function() {
  "use strict";

  let registerFeature = window.__gosx_register_bootstrap_feature;
  if (typeof registerFeature !== "function") {
    const shared = window.__gosx_bootstrap_features
      || (window.__gosx_bootstrap_features = Object.create(null));
    registerFeature = function(name, factory) {
      const key = String(name || "").trim();
      if (key && typeof factory === "function") shared[key] = factory;
    };
  }

  registerFeature("controllers", function(api) {
    const gosxReadSharedSignal = api.gosxReadSharedSignal;
    const gosxSubscribeSharedSignal = api.gosxSubscribeSharedSignal;
    const setSharedSignalValue = api.setSharedSignalValue;
