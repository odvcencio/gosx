(function() {
  "use strict";

  const registerFeature = window.__gosx_register_bootstrap_feature;
  if (typeof registerFeature !== "function") {
    console.error("[gosx] runtime bootstrap feature registry missing");
    return;
  }

  registerFeature("hubs", function(api) {
    const setSharedSignalJSON = api.setSharedSignalJSON;
    const gosxNotifySharedSignal = api.gosxNotifySharedSignal;
