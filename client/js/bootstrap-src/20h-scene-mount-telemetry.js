// 20h — the backend and telemetry globals.
//
// Publishes window.__gosx_choose_scene_backend,
// window.__gosx_scene_backend_caps_of and window.__gosx_scene3d_telemetry.
// The telemetry snapshot aggregates every mounted scene.
  window.__gosx_choose_scene_backend = chooseSceneBackend;
  window.__gosx_scene_backend_caps_of = sceneBackendCapsOf;

  // ---------------------------------------------------------------------------
  // window.__gosx_scene3d_telemetry(mountOrNull) — aggregated telemetry snapshot
  //
  // Returns a plain object aggregating runtime details by reading
  // data-gosx-scene3d-* attributes on the scene mount element, plus parsed
  // cull-survivors JSON and a compact slice of webgpu diagnostics when
  // available. Read-only; zero side-effects; returns null when no mounted scene.
  //
  // If mountOrNull is null, finds the first [data-gosx-scene3d-mounted] element.
  // ---------------------------------------------------------------------------
  window.__gosx_scene3d_telemetry = function sceneTelemSnapshot(mountOrNull) {
    var mount = mountOrNull;
    if (!mount) {
      mount = typeof document !== "undefined"
        ? document.querySelector("[data-gosx-scene3d-mounted]")
        : null;
    }
    if (!mount || typeof mount.getAttribute !== "function") return null;

    function attr(name) { return mount.getAttribute("data-gosx-scene3d-" + name); }
    function numAttr(name) { var v = attr(name); return v !== null ? parseFloat(v) : null; }
    function boolAttr(name) { var v = attr(name); return v === "true" ? true : v === "false" ? false : null; }

    // Parse cull-survivors JSON safely.
    var cullSurvivorsRaw = attr("cull-survivors");
    var cullSurvivors = null;
    if (cullSurvivorsRaw) {
      try { cullSurvivors = JSON.parse(cullSurvivorsRaw); } catch (_e) {}
    }

    // Compact WebGPU diagnostics slice (only when available).
    var wgpuDiag = null;
    if (typeof window.__gosx_scene3d_webgpu_diagnostics === "function") {
      try {
        var d = window.__gosx_scene3d_webgpu_diagnostics();
        if (d) {
          wgpuDiag = {
            ready: d.ready,
            adapterAvailable: d.adapterAvailable,
            deviceAvailable: d.deviceAvailable,
            deviceFeatures: Array.isArray(d.deviceFeatures) ? d.deviceFeatures.slice(0, 8) : [],
          };
        }
      } catch (_e) {}
    }

    var liveTelemetry = null;
    var liveHandle = mount.__gosxScene3DHandle;
    if (liveHandle && typeof liveHandle.getTelemetry === "function") {
      try { liveTelemetry = liveHandle.getTelemetry(); } catch (_e) {}
    }

    return {
      backend: attr("backend"),
      ready: boolAttr("ready"),
      mounted: boolAttr("mounted"),
      inViewport: boolAttr("in-viewport"),
      capabilityTier: attr("capability-tier"),
      pixelRatio: numAttr("pixel-ratio"),
      qualityFrameMs: numAttr("quality-frame-ms"),
      qualityDprCap: numAttr("quality-dpr-cap"),
      qualityPostfxSuppressed: boolAttr("quality-postfx-suppressed"),
      adaptiveQuality: attr("adaptive-quality"),
      renderLoopReason: attr("render-loop-reason"),
      renderWatchdogReason: attr("render-watchdog-reason"),
      dropped: attr("dropped"),
      deviceMemory: numAttr("device-memory"),
      hardwareConcurrency: numAttr("hardware-concurrency"),
      cullSurvivors: cullSurvivors,
      webgpu: wgpuDiag,
      camera: liveTelemetry && liveTelemetry.camera || null,
      orbit: liveTelemetry && liveTelemetry.orbit || null,
      selectionID: liveTelemetry && liveTelemetry.selectionID || "",
      lastPick: liveTelemetry && liveTelemetry.lastPick || null,
      rendererStats: liveTelemetry && liveTelemetry.rendererStats || null,
    };
  };

