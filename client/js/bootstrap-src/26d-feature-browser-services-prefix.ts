// Browser services are loaded only by pages that use the typed WASM host.
(function() {
  "use strict";
  const features = window.__gosx_bootstrap_features
    || (window.__gosx_bootstrap_features = Object.create(null));
  features["browser-services"] = function() {
