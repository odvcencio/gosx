(function() {
  "use strict";

  const registerFeature = window.__gosx_register_bootstrap_feature;
  if (typeof registerFeature !== "function") {
    console.error("[gosx] runtime bootstrap feature registry missing");
    return;
  }

  registerFeature("islands", function(api) {
    const fetchProgram = api.fetchProgram;
    const inferProgramFormat = api.inferProgramFormat;
