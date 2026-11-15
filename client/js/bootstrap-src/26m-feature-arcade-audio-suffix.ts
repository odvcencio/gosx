
  if (existingArcadeAudio) window.__gosx.arcadeAudio = existingArcadeAudio;
  // RequireFeature("arcade-audio") loads this chunk by name and needs a factory.
  if (window.__gosx_register_bootstrap_feature) window.__gosx_register_bootstrap_feature("arcade-audio", () => ({}));
})();
