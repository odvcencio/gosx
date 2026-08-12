// mount-telemetry.ts — the backend and telemetry globals.
// @ts-check
//
// Publishes window.__gosx_choose_scene_backend,
// window.__gosx_scene_backend_caps_of and window.__gosx_scene3d_telemetry.
// The telemetry snapshot aggregates every mounted scene.

/**
 * @typedef {object} GoSXSceneMountTelemetry
 * @property {string} backend
 * @property {number} frame
 * @property {object} [diagnostics]
 */
  window.__gosx_choose_scene_backend = chooseSceneBackend;
  window.__gosx_scene_backend_caps_of = sceneBackendCapsOf;

  // ---------------------------------------------------------------------------
  // window.__gosx_scene3d_telemetry(mountOrOptions) — telemetry snapshot
  //
  // Legacy calls with a mount (or null, which selects the first mounted scene)
  // return one mount snapshot. Explicit {scope:"mount", mount} does the same.
  // {scope:"page"} returns every live mount registered by the Scene3D debug
  // surface registry. Read-only; zero side-effects.
  //
  // Typed attribute failures and telemetry producer failures stay contained,
  // but are visible as structured diagnostics instead of being swallowed.
  // ---------------------------------------------------------------------------
  window.__gosx_scene3d_telemetry = function sceneTelemSnapshot(mountOrOptions) {
    var options = mountOrOptions
      && typeof mountOrOptions === "object"
      && typeof mountOrOptions.getAttribute !== "function"
      && (Object.prototype.hasOwnProperty.call(mountOrOptions, "scope")
        || Object.prototype.hasOwnProperty.call(mountOrOptions, "mount"))
      ? mountOrOptions
      : null;
    var scope = options ? String(options.scope || "mount").trim().toLowerCase() : "mount";

    function liveMountRecords() {
      var out = [];
      var registry = typeof window !== "undefined" ? window.__gosx_scene3d_debug_registry : null;
      if (registry && typeof registry.forEach === "function") {
        registry.forEach(function(record) {
          if (!record || !record.mount || out.some(function(entry) { return entry.mount === record.mount; })) return;
          out.push({ mount: record.mount, record: record });
        });
        return out;
      }
      if (typeof document === "undefined" || typeof document.querySelectorAll !== "function") return out;
      var mounts = document.querySelectorAll("[data-gosx-scene3d-mounted]");
      for (var i = 0; i < mounts.length; i += 1) {
        if (mounts[i] && !out.some(function(entry) { return entry.mount === mounts[i]; })) {
          out.push({ mount: mounts[i], record: null });
        }
      }
      return out;
    }

    function recordForMount(mount) {
      var registry = typeof window !== "undefined" ? window.__gosx_scene3d_debug_registry : null;
      var found = null;
      if (!registry || typeof registry.forEach !== "function") return null;
      registry.forEach(function(record) {
        if (!found && record && record.mount === mount) found = record;
      });
      return found;
    }

    function producerFailureEntry(producer, err) {
      return {
        severity: "warn",
        code: "scene.telemetry.snapshot_failed",
        message: "Scene3D telemetry producer failed",
        data: {
          producer: producer,
          error: err && err.message ? String(err.message) : String(err || ""),
        },
      };
    }

    function readPageCapabilities(reportFailure) {
      var webgpu = null;
      if (typeof window !== "undefined" && typeof window.__gosx_scene3d_webgpu_diagnostics === "function") {
        try {
          var d = window.__gosx_scene3d_webgpu_diagnostics();
          if (d) {
            webgpu = {
              ready: d.ready,
              adapterAvailable: d.adapterAvailable,
              deviceAvailable: d.deviceAvailable,
              deviceFeatures: Array.isArray(d.deviceFeatures) ? d.deviceFeatures.slice(0, 8) : [],
            };
          }
        } catch (err) {
          if (typeof reportFailure === "function") {
            reportFailure(producerFailureEntry("webgpu-diagnostics", err));
          }
        }
      }
      return { webgpu: webgpu };
    }

    function mountSnapshot(mount, record, pageCapabilities) {
      if (!mount || typeof mount.getAttribute !== "function") return null;
      var parseDiagnostics = [];

      function attributeName(name) { return "data-gosx-scene3d-" + name; }
      function attr(name) {
        var value = mount.getAttribute(attributeName(name));
        return value == null ? null : String(value);
      }
      function invalidAttr(name, value, expected, reason, error) {
        var data = { attribute: attributeName(name), value: value, expected: expected, reason: reason };
        if (error) data.error = error;
        parseDiagnostics.push({
          severity: "warn",
          code: reason === "parse-error" ? "scene.telemetry.parse_error" : "scene.telemetry.invalid_attribute",
          message: reason === "parse-error" ? "Scene3D telemetry attribute could not be parsed" : "Scene3D telemetry attribute is invalid",
          data: data,
        });
      }
      function numAttr(name) {
        var value = attr(name);
        if (value === null) return null;
        var trimmed = value.trim();
        var parsed = trimmed === "" ? NaN : Number(trimmed);
        if (!Number.isFinite(parsed)) {
          invalidAttr(name, value, "finite-number", "invalid-value");
          return null;
        }
        return parsed;
      }
      function boolAttr(name) {
        var value = attr(name);
        if (value === null) return null;
        if (value === "true") return true;
        if (value === "false") return false;
        invalidAttr(name, value, "true-or-false", "invalid-value");
        return null;
      }
      function objectJSONAttr(name) {
        var value = attr(name);
        if (value === null) return null;
        try {
          var parsed = JSON.parse(value);
          if (!parsed || typeof parsed !== "object" || Array.isArray(parsed)) {
            invalidAttr(name, value, "json-object", "invalid-value");
            return null;
          }
          return parsed;
        } catch (err) {
          invalidAttr(name, value, "json-object", "parse-error",
            err && err.message ? String(err.message) : String(err || ""));
          return null;
        }
      }
      function producerFailure(producer, err) {
        parseDiagnostics.push(producerFailureEntry(producer, err));
      }

      var debugSnapshot = null;
      record = record || recordForMount(mount);
      if (record && typeof record.snapshot === "function") {
        try { debugSnapshot = record.snapshot("full"); } catch (err) { producerFailure("debug-surface", err); }
      } else if (typeof window !== "undefined"
          && window.__gosx_scene3d_debug
          && typeof window.__gosx_scene3d_debug.inspect === "function"
          && mount.id) {
        try { debugSnapshot = window.__gosx_scene3d_debug.inspect(String(mount.id)); } catch (err) { producerFailure("debug-api", err); }
      }

      if (!pageCapabilities) {
        pageCapabilities = readPageCapabilities(function(failure) {
          parseDiagnostics.push(failure);
        });
      }

      var liveTelemetry = null;
      var liveHandle = mount.__gosxScene3DHandle;
      if (liveHandle && typeof liveHandle.getTelemetry === "function") {
        try { liveTelemetry = liveHandle.getTelemetry(); } catch (err) { producerFailure("mount-handle", err); }
      }

      var diagnostics = debugSnapshot && Array.isArray(debugSnapshot.diagnostics)
        ? debugSnapshot.diagnostics.slice()
        : [];

      var snapshot = {
        scope: "mount",
        id: debugSnapshot && debugSnapshot.id || "",
        mountID: debugSnapshot && debugSnapshot.mountID || (mount.id ? String(mount.id) : ""),
        engineID: debugSnapshot && debugSnapshot.engineID || "",
        component: debugSnapshot && debugSnapshot.component || "",
        backend: attr("backend"),
        renderer: attr("renderer") || attr("backend"),
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
        cullSurvivors: objectJSONAttr("cull-survivors"),
        webgpuProbeScope: "page",
        pageCapabilities: pageCapabilities,
        // Backward-compatible alias. This is page-scoped probe evidence, not
        // renderer truth for this mount; use rendererDiagnostics for that.
        webgpu: pageCapabilities.webgpu,
        camera: liveTelemetry && liveTelemetry.camera || (debugSnapshot && debugSnapshot.camera) || null,
        orbit: liveTelemetry && liveTelemetry.orbit || null,
        selectionID: liveTelemetry && liveTelemetry.selectionID || "",
        lastPick: liveTelemetry && liveTelemetry.lastPick || (debugSnapshot && debugSnapshot.lastPick) || null,
        rendererStats: liveTelemetry && liveTelemetry.rendererStats || null,
        rendererDiagnostics: debugSnapshot && debugSnapshot.rendererDiagnostics || null,
        diagnostics: diagnostics,
      };
      Array.prototype.push.apply(diagnostics, parseDiagnostics);
      return snapshot;
    }

    if (scope === "page") {
      var pageDiagnostics = [];
      var pageCapabilities = readPageCapabilities(function(failure) {
        pageDiagnostics.push(failure);
      });
      var entries = liveMountRecords();
      var mounts = [];
      var diagnostics = pageDiagnostics.slice();
      for (var i = 0; i < entries.length; i += 1) {
        var snapshot = mountSnapshot(entries[i].mount, entries[i].record, pageCapabilities);
        if (!snapshot) continue;
        mounts.push(snapshot);
        for (var j = 0; j < snapshot.diagnostics.length; j += 1) {
          diagnostics.push(Object.assign({
            surfaceID: snapshot.id,
            mountID: snapshot.mountID,
            engineID: snapshot.engineID,
          }, snapshot.diagnostics[j]));
        }
      }
      return {
        scope: "page",
        mountCount: mounts.length,
        mounts: mounts,
        diagnostics: diagnostics,
        pageCapabilities: pageCapabilities,
      };
    }

    var mount = options ? options.mount : mountOrOptions;
    if (!mount) {
      mount = typeof document !== "undefined" && typeof document.querySelector === "function"
        ? document.querySelector("[data-gosx-scene3d-mounted]")
        : null;
    }
    return mountSnapshot(mount, null);
  };
