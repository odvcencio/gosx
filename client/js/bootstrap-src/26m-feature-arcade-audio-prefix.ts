(function() {
  "use strict";

  // Standalone arcadeAudio for a page that needs the procedural synth but no
  // hub chunk: a Go WASM engine calls window.__gosx.arcadeAudio.play(...) and
  // scene "audio" events reach it. When another bundle already published the
  // synth, that one stays and this chunk installs nothing.
  window.__gosx = window.__gosx || {};
  const existingArcadeAudio = window.__gosx.arcadeAudio;

  // hubInputNumber is the one hub-transport helper the synth reads.
  function hubInputNumber(value, fallback) {
    const next = Number(value);
    return Number.isFinite(next) ? next : fallback;
  }
