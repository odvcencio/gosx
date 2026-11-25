  var WATER_MAX_DISPLACEMENT_SPHERES = 32;
  var WATER_CAUSTICS_TEXTURE_FORMAT = "rgba8unorm";
  var WATER_CAUSTICS_TEXTURE_SIZE = 1024;
  var WATER_OBJECT_TEXTURE_FORMAT = "rgba8unorm";
  var WATER_OBJECT_TEXTURE_SIZE = 256;
  var WATER_OBJECT_TEXTURE_MAX_SIZE = 2048;
  var WATER_OBJECT_TEXTURE_TARGET_COUNT = 3;
  var WATER_OBJECT_SHADOW_TEXTURE_SIZE = 256;
  // M5 at-rest gating (water-parity-campaign). waterRestEnergy is a CHEAP
  // (CPU-only, no GPU readback) proxy for the simulation's kinetic energy:
  // it starts at 1.0 on any disturbance and is multiplied by the SAME
  // `damping` coefficient the simulation kernel itself applies to velocity
  // every substep it actually runs (see WGSL_COMMON... "info.y = (info.y +
  // ...) * params.damping" and sceneWaterUniformData's damping field), so it
  // tracks the real physical decay rate rather than an arbitrary timer.
  // WATER_REST_ENERGY_EPSILON is the threshold below which the residual
  // ripple is visually and physically negligible (with the demo's default
  // damping=0.995 this is ~1300 substeps, ~11s of undisturbed real time --
  // deliberately calm, not twitchy, since resting is a background
  // GPU-cost win, not a latency-sensitive UI state). If an author's
  // `damping` is tuned close to 1.0 (near-lossless), energy decays too
  // slowly to ever cross this threshold and the system correctly never
  // rests (a genuinely undamped simulation should not visually freeze).
  // WATER_REST_MIN_QUIET_MS is a defense-in-depth floor alongside the
  // energy check: even if a future damping/authoring change made the
  // energy estimate decay unexpectedly fast, a system cannot rest sooner
  // than this many real milliseconds after its last disturbance.
  var WATER_REST_ENERGY_EPSILON = 0.001;
  var WATER_REST_MIN_QUIET_MS = 1200;
  // M6 per-frame churn audit -- see waterUniformSnapshotChanged's comment
  // below (near sceneWaterUniformData). Word indices [0, this) in
  // waterUniformScratch are resolution/cellCount/seedDrops/frameIndex/
  // deltaTime/timeSeconds and are excluded from the "did anything
  // meaningful change" comparison.
  var WATER_UNIFORM_VOLATILE_WORDS = 6;
  function sceneWebGPUWaterDebugSkipsUpdate(mode) {
    return mode === "no-water" || mode === "no-update";
  }

  function sceneWebGPUWaterDebugSkipsDraw(mode) {
    return mode === "compute-only" || mode === "no-draw" || sceneWebGPUWaterDebugSkipsUpdate(mode);
  }

  function sceneWaterResolution(value) {
    var raw = Math.floor(sceneNumber(value, 256));
    if (!Number.isFinite(raw) || raw <= 0) raw = 256;
    // A sampled render-state texture is refreshed with copyBufferToTexture.
    // rgba32float rows are 16 bytes/texel and WebGPU requires bytesPerRow to
    // be 256-byte aligned, so GPU water grids use 16-texel increments.
    return Math.max(16, Math.min(512, Math.ceil(raw / 16) * 16));
  }

  function sceneWaterSurfaceResolution(entry, simulationResolution) {
    var raw = Math.floor(sceneNumber(entry && entry.surfaceResolution, simulationResolution));
    if (!Number.isFinite(raw) || raw < 2) raw = simulationResolution;
    return Math.max(2, Math.min(512, raw));
  }

  function sceneWaterCausticsResolution(entry) {
    var raw = Math.floor(sceneNumber(entry && entry.causticsResolution, WATER_CAUSTICS_TEXTURE_SIZE));
    if (!Number.isFinite(raw) || raw <= 0) raw = WATER_CAUSTICS_TEXTURE_SIZE;
    return Math.max(64, Math.min(2048, raw));
  }

  function sceneWaterObjectTextureResolution(entry) {
    var raw = Math.floor(sceneNumber(entry && entry.objectTextureResolution, WATER_OBJECT_TEXTURE_SIZE));
    if (!Number.isFinite(raw) || raw <= 0) raw = WATER_OBJECT_TEXTURE_SIZE;
    return Math.max(64, Math.min(2048, raw));
  }

  function sceneWaterObjectTextureResolutionMode(entry) {
    var mode = typeof (entry && entry.objectTextureResolutionMode) === "string"
      ? entry.objectTextureResolutionMode.trim().toLowerCase()
      : "";
    if (mode === "auto" || mode === "upstream") return "viewport";
    if (mode === "viewport") return "viewport";
    return "fixed";
  }

  function sceneWaterObjectTexturePixelBudget(entry) {
    var raw = Math.floor(sceneNumber(entry && entry.objectTexturePixelBudget, 0));
    if (!Number.isFinite(raw) || raw <= 0) return 0;
    return Math.max(WATER_OBJECT_TEXTURE_TARGET_COUNT, raw);
  }

  function sceneWaterObjectTextureClampToPixelBudget(size, pixelBudget) {
    var width = Math.max(1, Math.floor(sceneNumber(size && size.width, WATER_OBJECT_TEXTURE_SIZE)));
    var height = Math.max(1, Math.floor(sceneNumber(size && size.height, WATER_OBJECT_TEXTURE_SIZE)));
    var budget = Math.max(0, Math.floor(sceneNumber(pixelBudget, 0)));
    var totalPixels = width * height * WATER_OBJECT_TEXTURE_TARGET_COUNT;
    if (budget > 0 && totalPixels > budget) {
      var scale = Math.sqrt(budget / totalPixels);
      width = Math.max(1, Math.floor(width * scale));
      height = Math.max(1, Math.floor(height * scale));
    }
    return {
      mode: size && size.mode || "fixed",
      width: width,
      height: height,
      resolution: Math.max(width, height),
      pixelBudget: budget,
    };
  }

  function sceneWaterObjectTextureTargetSize(entry, width, height) {
    var mode = sceneWaterObjectTextureResolutionMode(entry);
    var pixelBudget = sceneWaterObjectTexturePixelBudget(entry);
    if (mode === "viewport") {
      var targetWidth = Math.max(1, Math.floor(sceneNumber(width, WATER_OBJECT_TEXTURE_SIZE)));
      var targetHeight = Math.max(1, Math.floor(sceneNumber(height, WATER_OBJECT_TEXTURE_SIZE)));
      var maxSide = Math.max(targetWidth, targetHeight, 1);
      var scale = Math.min(1, WATER_OBJECT_TEXTURE_MAX_SIZE / maxSide);
      targetWidth = Math.max(1, Math.floor(targetWidth * scale));
      targetHeight = Math.max(1, Math.floor(targetHeight * scale));
      return sceneWaterObjectTextureClampToPixelBudget({
        mode: mode,
        width: targetWidth,
        height: targetHeight,
        resolution: Math.max(targetWidth, targetHeight),
      }, pixelBudget);
    }
    var fixed = sceneWaterObjectTextureResolution(entry);
    return sceneWaterObjectTextureClampToPixelBudget({ mode: mode, width: fixed, height: fixed, resolution: fixed }, pixelBudget);
  }

  function sceneWaterObjectShadowResolution(entry) {
    var raw = Math.floor(sceneNumber(entry && entry.objectShadowResolution, WATER_OBJECT_SHADOW_TEXTURE_SIZE));
    if (!Number.isFinite(raw) || raw <= 0) raw = WATER_OBJECT_SHADOW_TEXTURE_SIZE;
    return Math.max(64, Math.min(2048, raw));
  }

  function sceneWaterVector3(value, fallback) {
    var fb = fallback || { x: 0, y: 1, z: 0 };
    if (Array.isArray(value)) {
      return {
        x: sceneNumber(value[0], fb.x),
        y: sceneNumber(value[1], fb.y),
        z: sceneNumber(value[2], fb.z),
      };
    }
    if (value && typeof value === "object") {
      return {
        x: sceneNumber(value.x, fb.x),
        y: sceneNumber(value.y, fb.y),
        z: sceneNumber(value.z, fb.z),
      };
    }
    return { x: fb.x, y: fb.y, z: fb.z };
  }

  function sceneWaterLightVector(entry, fallback) {
    var fb = fallback || { x: 0.3, y: 0.9, z: 0.45 };
    if (entry && typeof entry === "object") {
      if (entry.lightDirection != null) {
        return sceneWaterVector3(entry.lightDirection, fb);
      }
      if (
        Object.prototype.hasOwnProperty.call(entry, "lightDirectionX") ||
        Object.prototype.hasOwnProperty.call(entry, "lightDirectionY") ||
        Object.prototype.hasOwnProperty.call(entry, "lightDirectionZ")
      ) {
        return {
          x: sceneNumber(entry.lightDirectionX, fb.x),
          y: sceneNumber(entry.lightDirectionY, fb.y),
          z: sceneNumber(entry.lightDirectionZ, fb.z),
        };
      }
    }
    return sceneWaterVector3(null, fb);
  }

  function sceneWaterObjectKind(entry) {
    var raw = "";
    if (entry && typeof entry.objectKind === "string" && entry.objectKind) {
      raw = entry.objectKind;
    } else if (entry && typeof entry.activeObject === "string" && entry.activeObject) {
      raw = entry.activeObject;
    }
    var value = String(raw || "").trim().toLowerCase();
    if (!value || value === "none" || value === "no_object") return 0;
    if (value.indexOf("sphere") >= 0 || value.indexOf("ball") >= 0) return 1;
    if (value.indexOf("cube") >= 0 || value.indexOf("box") >= 0) return 2;
    if (value.indexOf("compound") >= 0 || value.indexOf("mesh") >= 0 || value.indexOf("torus") >= 0 || value.indexOf("duck") >= 0) return 3;
    return 0;
  }

  function sceneWaterObjectSubtype(entry, kind) {
    if (!entry || kind !== 3) return 0;
    var raw = [
      entry.objectSubtype,
      entry.activeObject,
      entry.label,
      entry.id,
      entry.src,
      entry.objectKind,
    ].filter(Boolean).join(" ").toLowerCase();
    if (raw.indexOf("torus") >= 0 || raw.indexOf("knot") >= 0) return 1;
    if (raw.indexOf("duck") >= 0 || raw.indexOf("mesh") >= 0 || raw.indexOf("gltf") >= 0 || raw.indexOf("glb") >= 0) return 2;
    return 0;
  }

  function sceneWaterDisplacementSphereSignature(spheres) {
    if (!Array.isArray(spheres) || spheres.length === 0) return "";
    return spheres.slice(0, WATER_MAX_DISPLACEMENT_SPHERES).map(function(sphere) {
      if (Array.isArray(sphere)) {
        return [
          sceneNumber(sphere[0], 0).toFixed(5),
          sceneNumber(sphere[1], 0).toFixed(5),
          sceneNumber(sphere[2], 0).toFixed(5),
          sceneNumber(sphere[3], 0).toFixed(5),
        ].join(",");
      }
      var offset = sphere && sphere.offset && typeof sphere.offset === "object" ? sphere.offset : {};
      return [
        sceneNumber(sphere && Object.prototype.hasOwnProperty.call(sphere, "offsetX") ? sphere.offsetX : offset.x, 0).toFixed(5),
        sceneNumber(sphere && Object.prototype.hasOwnProperty.call(sphere, "offsetY") ? sphere.offsetY : offset.y, 0).toFixed(5),
        sceneNumber(sphere && Object.prototype.hasOwnProperty.call(sphere, "offsetZ") ? sphere.offsetZ : offset.z, 0).toFixed(5),
        sceneNumber(sphere && sphere.radius, 0).toFixed(5),
      ].join(",");
    }).join(";");
  }

  function sceneWaterObjectMotionSignature(entry, kind) {
    if (!entry || !kind) return "";
    return [
      kind,
      sceneNumber(entry.objectX, 0).toFixed(5),
      sceneNumber(entry.objectY, 0).toFixed(5),
      sceneNumber(entry.objectZ, 0).toFixed(5),
      sceneBool(entry.objectPreviousSet, false) ? "1" : "0",
      sceneNumber(entry.objectPreviousX, 0).toFixed(5),
      sceneNumber(entry.objectPreviousY, 0).toFixed(5),
      sceneNumber(entry.objectPreviousZ, 0).toFixed(5),
      sceneNumber(entry.poolWidth, 1).toFixed(5),
      sceneNumber(entry.poolLength, 1).toFixed(5),
      sceneNumber(entry.objectRadius, 0).toFixed(5),
      sceneNumber(entry.objectHalfSizeX, 0).toFixed(5),
      sceneNumber(entry.objectHalfSizeY, 0).toFixed(5),
      sceneNumber(entry.objectHalfSizeZ, 0).toFixed(5),
      sceneNumber(entry.objectDriftX, 0).toFixed(5),
      sceneNumber(entry.objectDriftY, 0).toFixed(5),
      sceneNumber(entry.objectDriftZ, 0).toFixed(5),
      sceneNumber(entry.objectBobAmplitude, 0).toFixed(5),
      sceneNumber(entry.objectBobSpeed, 0).toFixed(5),
      sceneNumber(entry.objectDisplacementScale, 1).toFixed(5),
      sceneWaterObjectSubtype(entry, kind),
      sceneWaterDisplacementSphereSignature(entry.objectDisplacementSpheres),
    ].join("|");
  }

  function sceneWaterObjectExplicitPreviousSignature(entry, kind) {
    if (!entry || !kind || !sceneBool(entry.objectPreviousSet, false)) return "";
    return [
      kind,
      sceneNumber(entry.objectPreviousX, 0).toFixed(5),
      sceneNumber(entry.objectPreviousY, 0).toFixed(5),
      sceneNumber(entry.objectPreviousZ, 0).toFixed(5),
      sceneNumber(entry.objectX, 0).toFixed(5),
      sceneNumber(entry.objectY, 0).toFixed(5),
      sceneNumber(entry.objectZ, 0).toFixed(5),
    ].join("|");
  }

  function sceneWaterObjectCenter(entry, timeSeconds) {
    var time = sceneNumber(timeSeconds, 0);
    var bobSpeed = sceneNumber(entry && entry.objectBobSpeed, 0);
    var bob = Math.sin(time * (bobSpeed > 0 ? bobSpeed : 1)) * sceneNumber(entry && entry.objectBobAmplitude, 0);
    return {
      x: sceneNumber(entry && entry.objectX, 0) + Math.sin(time * 0.73) * sceneNumber(entry && entry.objectDriftX, 0),
      y: sceneNumber(entry && entry.objectY, 0) + bob + Math.sin(time * 0.41) * sceneNumber(entry && entry.objectDriftY, 0),
      z: sceneNumber(entry && entry.objectZ, 0) + Math.cos(time * 0.67) * sceneNumber(entry && entry.objectDriftZ, 0),
    };
  }

  function sceneWaterNormalizeObjectCenter(center, poolWidth, poolLength) {
    var halfWidth = Math.max(0.0001, poolWidth);
    var halfLength = Math.max(0.0001, poolLength);
    return {
      x: sceneNumber(center && center.x, 0) / halfWidth,
      y: sceneNumber(center && center.y, 0),
      z: sceneNumber(center && center.z, 0) / halfLength,
    };
  }

  function sceneWaterDisplacementSpheres(entry, poolWidth, poolLength) {
    var source = entry && Array.isArray(entry.objectDisplacementSpheres) ? entry.objectDisplacementSpheres : [];
    if (source.length === 0) return [];
    var halfWidth = Math.max(0.0001, poolWidth);
    var halfLength = Math.max(0.0001, poolLength);
    var out = [];
    for (var i = 0; i < source.length && out.length < WATER_MAX_DISPLACEMENT_SPHERES; i++) {
      var raw = source[i];
      var offset = raw && raw.offset && typeof raw.offset === "object" ? raw.offset : {};
      var x = 0;
      var y = 0;
      var z = 0;
      var radius = 0;
      if (Array.isArray(raw)) {
        x = sceneNumber(raw[0], 0);
        y = sceneNumber(raw[1], 0);
        z = sceneNumber(raw[2], 0);
        radius = sceneNumber(raw[3], 0);
      } else {
        x = sceneNumber(raw && Object.prototype.hasOwnProperty.call(raw, "offsetX") ? raw.offsetX : offset.x, 0);
        y = sceneNumber(raw && Object.prototype.hasOwnProperty.call(raw, "offsetY") ? raw.offsetY : offset.y, 0);
        z = sceneNumber(raw && Object.prototype.hasOwnProperty.call(raw, "offsetZ") ? raw.offsetZ : offset.z, 0);
        radius = sceneNumber(raw && raw.radius, 0);
      }
      if (radius <= 0) continue;
      out.push({
        x: x / halfWidth,
        y: y,
        z: z / halfLength,
        radius: Math.max(0.0001, radius) / halfLength,
      });
    }
    return out;
  }

  function sceneWaterObjectDisplacementEvents(entry) {
    var source = entry && Array.isArray(entry.objectDisplacementEvents) ? entry.objectDisplacementEvents : [];
    return source.filter(function(event) { return event && typeof event === "object"; });
  }

  function sceneWaterObjectDisplacementEventID(event) {
    return Math.max(0, Math.floor(sceneNumber(event && event.id, 0)));
  }

  function sceneWaterObjectDisplacementEventEntry(entry, event) {
    var next = Object.assign({}, entry || {}, event || {});
    next.objectPreviousSet = true;
    return next;
  }

  function sceneWaterDropEvents(entry) {
    var source = entry && Array.isArray(entry.dropEvents) ? entry.dropEvents : [];
    return source.filter(function(event) { return event && typeof event === "object"; });
  }

  function sceneWaterDropEventID(event) {
    return Math.max(0, Math.floor(sceneNumber(event && event.id, 0)));
  }

  // sceneWaterDropEventEntry: unlike sceneWaterObjectDisplacementEventEntry,
  // a queued drop's field names ({id,x,z,radius,strength}) don't line up
  // 1:1 with the entry's drop fields (dropX/dropZ/dropEventRadius/
  // dropEventStrength), so this maps them explicitly instead of a blind
  // Object.assign merge.
  function sceneWaterDropEventEntry(entry, event) {
    var next = Object.assign({}, entry || {});
    next.dropX = Math.max(-1, Math.min(1, sceneNumber(event && event.x, sceneNumber(entry && entry.dropX, 0))));
    next.dropZ = Math.max(-1, Math.min(1, sceneNumber(event && event.z, sceneNumber(entry && entry.dropZ, 0))));
    next.dropEventRadius = Math.max(0.0001, sceneNumber(event && event.radius, sceneNumber(entry && entry.dropEventRadius, sceneNumber(entry && entry.dropRadius, 0.03))));
    next.dropEventStrength = sceneNumber(event && event.strength, sceneNumber(entry && entry.dropEventStrength, sceneNumber(entry && entry.dropStrength, 0.01)));
    return next;
  }

  // sceneWaterSystemSignature decides whether a WaterSystem's GPU resources
  // need to be rebuilt (resolution/texture-size/seed/drop params changed).
  // It no longer folds in any hand-written per-entry WGSL text -- Selena is
  // the sole primary shader source (see the *SelenaWGSL data slots /
  // getWaterPoolSelenaDraw & co. below) ahead of the builtin
  // SCENE_WATER_*_SOURCE fallback, neither of which is entry-authored.
  function sceneWaterSystemSignature(entry, width, height) {
    var resolution = sceneWaterResolution(entry && entry.resolution);
    var authoredSurfaceResolution = sceneWaterSurfaceResolution(entry, resolution);
    return [
      resolution,
      authoredSurfaceResolution,
      Math.max(0, Math.floor(sceneNumber(entry && entry.seedDrops, 7))),
      sceneNumber(entry && entry.dropRadius, 0.03).toFixed(5),
      sceneNumber(entry && entry.dropStrength, 0.01).toFixed(5),
    ].join("|");
  }

  function sceneWaterShaderSourcesFromEntries(entries) {
    var sourceMap = {};
    var source = Array.isArray(entries) ? entries : [];
    var fields = [
      "seedWGSL", "dropWGSL", "displacementWGSL", "simulationWGSL", "normalWGSL", "causticsWGSL",
      "poolVertexWGSL", "poolFragmentWGSL", "surfaceVertexWGSL", "surfaceFragmentWGSL", "surfaceBelowFragmentWGSL",
      "objectShadowWGSL", "objectMeshShadowVertexWGSL", "objectMeshShadowFragmentWGSL",
    ];
    for (var i = 0; i < source.length; i += 1) {
      var entry = source[i];
      if (!entry || typeof entry !== "object") continue;
      var id = typeof entry.id === "string" && entry.id ? entry.id : ("scene-water-" + i);
      var record = sourceMap[id] || { id: id };
      var changed = false;
      for (var f = 0; f < fields.length; f += 1) {
        var name = fields[f];
        if (typeof entry[name] === "string" && entry[name].trim()) {
          record[name] = entry[name];
          changed = true;
        }
      }
      if (changed) sourceMap[id] = record;
    }
    return sourceMap;
  }

  function sceneHydrateWaterEntriesFromSources(entries, sources) {
    if (!Array.isArray(entries) || !sources || typeof sources !== "object") return entries;
    var keys = Object.keys(sources);
    if (!keys.length) return entries;
    var fields = [
      "seedWGSL", "dropWGSL", "displacementWGSL", "simulationWGSL", "normalWGSL", "causticsWGSL",
      "poolVertexWGSL", "poolFragmentWGSL", "surfaceVertexWGSL", "surfaceFragmentWGSL", "surfaceBelowFragmentWGSL",
      "objectShadowWGSL", "objectMeshShadowVertexWGSL", "objectMeshShadowFragmentWGSL",
    ];
    return entries.map(function(entry, index) {
      if (!entry || typeof entry !== "object") return entry;
      var id = typeof entry.id === "string" && entry.id ? entry.id : ("scene-water-" + index);
      var source = sources[id] || (keys.length === 1 ? sources[keys[0]] : null);
      if (!source || typeof source !== "object") return entry;
      var hydrated = null;
      for (var f = 0; f < fields.length; f += 1) {
        var name = fields[f];
        if (typeof entry[name] === "string" && entry[name].trim()) continue;
        if (typeof source[name] !== "string" || !source[name].trim()) continue;
        if (!hydrated) hydrated = Object.assign({}, entry);
        hydrated[name] = source[name];
      }
      return hydrated || entry;
    });
  }

  // sceneWaterSurfaceSourceBytes is a generic length-summer over whichever
  // legacy hand-written WGSL fields (if any) happen to be present on a
  // bundle/manifest water-source record; retained only for
  // sceneWaterManifestShaderSourceStats' diagnostic byte counters above.
  function sceneWaterSurfaceSourceBytes(record) {
    if (!record || typeof record !== "object") return 0;
    var total = 0;
    [
      "surfaceVertexWGSL",
      "surfaceFragmentWGSL",
      "surfaceBelowFragmentWGSL",
    ].forEach(function(name) {
      if (typeof record[name] === "string" && record[name].trim()) {
        total += record[name].trim().length;
      }
    });
    return total;
  }

  function wgpuSceneWaterPoolShapeRounded(entry) {
    if (!entry || typeof entry.poolShape !== "string") return false;
    var value = entry.poolShape.trim().toLowerCase();
    return value === "rounded box" || value === "rounded" || value === "roundbox";
  }

  function sceneWaterOpticsFlags(entry, objectState) {
    return {
      caustics: sceneBool(entry && entry.caustics, true),
      reflection: sceneBool(entry && entry.reflection, true),
      refraction: sceneBool(entry && entry.refraction, true),
      object: !!(objectState && objectState.kind > 0 && objectState.displacementScale > 0),
    };
  }

  // sharedPass (optional): when provided, the dispatch is recorded into an
  // ALREADY-OPEN GPUComputePassEncoder instead of opening/closing its own --
  // used to fuse the per-tick simulation substeps and the trailing normal
  // reconstruction into one compute pass (see the water-sim/normal fusion
  // block below). WebGPU auto-synchronizes storage-buffer read-after-write
  // hazards between successive dispatchWorkgroups() calls in program order,
  // whether they're in the same pass or different passes, so batching
  // dispatches into a shared pass changes nothing about visibility -- only
  // the number of beginComputePass/end() calls encoded.
  function dispatchWaterPass(encoder, system, pipeline, sharedPass) {
    if (!system || !pipeline) return 0;
    var pass = sharedPass || (encoder && encoder.beginComputePass({ label: "gosx-water-pass" }));
    if (!pass) return 0;
    pass.setPipeline(wgpuRequirePipeline(pipeline));
    pass.setBindGroup(0, system.computeBindGroups[system.activeIndex]);
    pass.dispatchWorkgroups(Math.ceil(system.cellCount / 64));
    if (!sharedPass) pass.end();
    system.activeIndex = system.activeIndex === 0 ? 1 : 0;
    return 1;
  }

  function sceneWaterActiveObjectID(entry) {
    var raw = "";
    if (entry && typeof entry.objectID === "string" && entry.objectID) raw = entry.objectID;
    else if (entry && typeof entry.objectId === "string" && entry.objectId) raw = entry.objectId;
    if (raw) return raw;
    var active = String(entry && entry.activeObject || entry && entry.objectKind || "").trim().toLowerCase();
    if (active.indexOf("sphere") >= 0) return "float-sphere";
    if (active.indexOf("cube") >= 0 || active.indexOf("box") >= 0) return "float-cube";
    if (active.indexOf("torus") >= 0) return "float-torus";
    if (active.indexOf("duck") >= 0 || active.indexOf("mesh") >= 0) return "float-duck";
    return "";
  }

  function sceneWaterMeshObjectID(obj) {
    if (!obj || typeof obj !== "object") return "";
    return String(
      obj.id ||
      obj.nodeId ||
      obj.sourceId ||
      obj.ownerId ||
      obj.modelId ||
      obj.name ||
      ""
    );
  }

  function sceneWaterObjectMeshMatches(obj, targetID) {
    if (!targetID) return false;
    var id = sceneWaterMeshObjectID(obj);
    if (!id) return false;
    if (id === targetID) return true;
    return id.indexOf(targetID + ":") === 0 || id.indexOf(targetID + "/") === 0 || id.indexOf(targetID + "#") === 0;
  }

  function sceneWaterObjectMeshKindMatches(obj, entry) {
    if (!obj || !obj.castShadow) return false;
    var kind = String(obj.kind || "").trim().toLowerCase();
    var active = String(entry && entry.activeObject || entry && entry.objectKind || "").trim().toLowerCase();
    var waterKind = sceneWaterObjectKind(entry);
    if (waterKind === 1) return kind.indexOf("sphere") >= 0;
    if (waterKind === 2) return kind.indexOf("box") >= 0 || kind.indexOf("cube") >= 0;
    if (active.indexOf("torus") >= 0) return kind.indexOf("torus") >= 0;
    if (active.indexOf("duck") >= 0 || active.indexOf("mesh") >= 0) {
      var id = sceneWaterMeshObjectID(obj).toLowerCase();
      return id.indexOf("duck") >= 0 || kind.indexOf("model") >= 0 || kind.indexOf("mesh") >= 0;
    }
    return false;
  }

  function sceneWaterObjectMeshCandidateProfile(bundle, entry, materials) {
    var targetID = sceneWaterActiveObjectID(entry);
    var objects = Array.isArray(bundle && bundle.meshObjects) ? bundle.meshObjects : [];
    var materialList = Array.isArray(materials) ? materials : (Array.isArray(bundle && bundle.materials) ? bundle.materials : []);
    var parts = [];
    for (var i = 0; i < objects.length && parts.length < 8; i++) {
      var obj = objects[i] || {};
      var materialIndex = Math.max(0, Math.floor(sceneNumber(obj.materialIndex, 0)));
      var material = materialList[materialIndex] || {};
      var materialName = String(material.name || material.id || obj.material || material.kind || material.materialKind || "?");
      var materialBackend = sceneWebGPUIsSelenaMaterial(material) ? "selena" : String(material.shaderBackend || material.kind || material.materialKind || "pbr");
      parts.push([
        sceneWaterMeshObjectID(obj) || "?",
        String(obj.kind || "?"),
        obj.castShadow ? "shadow" : "no-shadow",
        obj.viewCulled ? "culled" : "visible",
        "mat=" + materialName,
        "backend=" + materialBackend,
        String(Math.max(0, Math.floor(sceneNumber(obj.vertexCount, 0)))),
      ].join(":"));
    }
    return (targetID || "?") + "|" + parts.join(",");
  }

  function sceneWaterObjectMeshList(bundle, entry) {
    var targetID = sceneWaterActiveObjectID(entry);
    var objects = Array.isArray(bundle && bundle.meshObjects) ? bundle.meshObjects : [];
    var selected = [];
    if (targetID) {
      for (var i = 0; i < objects.length; i++) {
        var obj = objects[i];
        if (!obj) continue;
        if (!Number.isFinite(obj.vertexOffset) || !Number.isFinite(obj.vertexCount) || obj.vertexCount <= 0) continue;
        if (sceneWaterObjectMeshMatches(obj, targetID)) selected.push(obj);
      }
    }
    if (selected.length > 0) return selected;
    for (var j = 0; j < objects.length; j++) {
      var fallback = objects[j];
      if (!fallback) continue;
      if (!Number.isFinite(fallback.vertexOffset) || !Number.isFinite(fallback.vertexCount) || fallback.vertexCount <= 0) continue;
      if (sceneWaterObjectMeshKindMatches(fallback, entry)) selected.push(fallback);
    }
    return selected;
  }

  // includeCamera (M4 audit): the two callers below deliberately disagree.
  // The object-shadow pass (renderWaterObjectShadowPass /
  // renderWaterObjectMeshShadowPass) calls this with includeCamera=false --
  // the shadow it caches is a LIGHT-space projection (light direction +
  // object transform + pool extents only, see
  // sceneWaterObjectMeshShadowUniformData below: light/poolWidth/poolLength,
  // no eye/view/projection field at all), so it is camera-independent by
  // construction and re-rendering a 1024x1024-class RTT every frame while
  // only the camera orbits would be pure waste. The object-texture
  // (reflection/refraction) pass at its OTHER call site legitimately passes
  // includeCamera=true: those RTTs render the mesh's reflection FROM the
  // camera's own eye position (getWaterObjectTextureRenderContext feeds
  // cameraPos/viewProjection), so they must re-render whenever the camera
  // moves. Do not "simplify" this to one shared value -- see
  // TestWaterObjectShadowSignatureExcludesCamera (program_test.go) for the
  // regression lock.
  function sceneWaterObjectRenderSignature(system, entry, bundle, objectList, includeCamera) {
    var center = (system && system.waterObjectCenter) || {};
    var half = (system && system.waterObjectHalfSize) || {};
    var light = (system && system.waterLightDir) || {};
    var parts = [
      sceneWaterActiveObjectID(entry), sceneWaterObjectKind(entry),
      sceneNumber(center.x, 0), sceneNumber(center.y, 0), sceneNumber(center.z, 0),
      sceneNumber(half.x, 0), sceneNumber(half.y, 0), sceneNumber(half.z, 0),
      sceneNumber(system && system.waterObjectRadius, 0),
      sceneNumber(light.x, 0), sceneNumber(light.y, 0), sceneNumber(light.z, 0),
    ];
    if (includeCamera) {
      var camera = (bundle && bundle.camera) || {};
      parts.push(camera.mode || "", sceneNumber(camera.x, 0), sceneNumber(camera.y, 0), sceneNumber(camera.z, 0),
        sceneNumber(camera.targetX, 0), sceneNumber(camera.targetY, 0), sceneNumber(camera.targetZ, 0),
        sceneNumber(camera.fov, 0), sceneNumber(camera.near, 0), sceneNumber(camera.far, 0));
    }
    for (var i = 0; i < objectList.length; i++) {
      var obj = objectList[i] || {};
      parts.push(sceneWaterMeshObjectID(obj), sceneNumber(obj.vertexOffset, 0), sceneNumber(obj.vertexCount, 0));
    }
    return parts.join("|");
  }

  function sceneWaterObjectTextureSelenaUniforms(system, texturePassMode) {
    var mode = texturePassMode === 0 ? 0 : texturePassMode === 2 ? 2 : 1;
    var entry = system && system.entry || {};
    var resolution = Math.max(1, sceneNumber(system && system.waterResolution, system && system.resolution ? system.resolution : sceneWaterResolution(entry && entry.resolution)));
    var poolWidth = Math.max(0.01, sceneNumber(system && system.waterPoolWidth, sceneNumber(entry && entry.poolWidth, 1.0)));
    var poolHeight = Math.max(0.01, sceneNumber(system && system.waterPoolHeight, sceneNumber(entry && entry.poolHeight, 1.0)));
    var poolLength = Math.max(0.01, sceneNumber(system && system.waterPoolLength, sceneNumber(entry && entry.poolLength, 1.0)));
    var rounded = wgpuSceneWaterPoolShapeRounded(entry);
    var maxCornerRadius = Math.max(0, Math.min(poolWidth, poolLength) - 0.001);
    var cornerRadius = Math.max(0, sceneNumber(system && system.waterCornerRadius, rounded ? Math.max(0, Math.min(maxCornerRadius, sceneNumber(entry && entry.cornerRadius, 0))) : 0));
    var light = system && system.waterLightDir ? system.waterLightDir : sceneWaterLightVector(entry, { x: 0.3, y: 0.9, z: 0.45 });
    var lightLen = Math.sqrt(light.x * light.x + light.y * light.y + light.z * light.z) || 1;
    var kind = Math.max(0, Math.floor(sceneNumber(system && system.waterObjectKind, sceneWaterObjectKind(entry))));
    var subtype = Math.max(0, Math.floor(sceneNumber(system && system.waterObjectSubtype, sceneWaterObjectSubtype(entry, kind))));
    var radius = Math.max(0.0001, sceneNumber(system && system.waterObjectRadius, sceneNumber(entry && entry.objectRadius, kind === 1 ? 0.25 : 0.31)));
    var center = system && system.waterObjectCenter || {};
    return {
      isTexturePass: mode ? [1, 0, 0, 0] : undefined,
      texturePassMode: mode ? [mode, 0, 0, 0] : undefined,
      waterObjectTexturePassMode: mode ? [mode, 0, 0, 0] : undefined,
      lightDir: [light.x / lightLen, light.y / lightLen, light.z / lightLen, 0],
      poolWidth, poolHeight, poolLength,
      objectKind: kind,
      objectRadius: radius,
      objectCenter: [sceneNumber(center.x, sceneNumber(entry.objectX, 0)), sceneNumber(center.y, sceneNumber(entry.objectY, 0)), sceneNumber(center.z, sceneNumber(entry.objectZ, 0))],
      poolSize: [poolWidth, poolHeight, poolLength, cornerRadius],
      params: [resolution, radius, kind, subtype],
    };
  }

  function sceneWaterObjectTextureSelenaContext(system, texturePassMode, targetName) {
    var mode = texturePassMode === 0 ? 0 : texturePassMode === 2 ? 2 : 1;
    var target = String(targetName || "target").trim() || "target";
    var waterID = String(system && (system.id || system.entry && system.entry.id) || "water-system");
    return {
      kind: "water-object-texture",
      uniformSlotSuffix: ["water-object-texture", waterID, target, mode].join("-"),
      uniforms: sceneWaterObjectTextureSelenaUniforms(system, mode),
    };
  }

  function waterSystemUsesProjectedObjectTextures(system) {
    if (!system || !system.waterObjectActive) return false;
    var entry = system.entry || {};
    var kind = Math.max(0, Math.floor(sceneNumber(system.waterObjectKind, sceneWaterObjectKind(entry))));
    return kind === 3;
  }

  function waterSystemHasObjectTextureSubject(system) {
    return waterSystemUsesProjectedObjectTextures(system);
  }

  function sceneWaterObjectTextureTargetSlots(system) {
    var subtype = Math.max(0, Math.floor(sceneNumber(system && system.waterObjectSubtype, 0)));
    // Match the upstream optical contract: torus uses refraction + reflected
    // camera, glTF/mesh (duck) uses refraction + clipped reflection. Unknown
    // compound subjects retain all three targets as the conservative path.
    if (subtype === 1) return [0, 1];
    if (subtype === 2) return [0, 2];
    return [0, 1, 2];
  }

  function sceneWaterNormalizeReflectionDirection(point) {
    var x = sceneNumber(point && point.x, 0);
    var y = sceneNumber(point && point.y, 0);
    var z = sceneNumber(point && point.z, 0);
    var length = Math.sqrt(x * x + y * y + z * z);
    if (length <= 0.000001) return { x: 0, y: 0, z: 1 };
    return { x: x / length, y: y / length, z: z / length };
  }

  function sceneWaterReflectionCameraForward(cam) {
    var x = 0;
    var y = 0;
    var z = 1;

    var sinX = Math.sin(cam.rotationX);
    var cosX = Math.cos(cam.rotationX);
    var nextY = y * cosX - z * sinX;
    var nextZ = y * sinX + z * cosX;
    y = nextY;
    z = nextZ;

    var sinY = Math.sin(cam.rotationY);
    var cosY = Math.cos(cam.rotationY);
    var nextX = x * cosY + z * sinY;
    nextZ = -x * sinY + z * cosY;
    x = nextX;
    z = nextZ;

    var sinZ = Math.sin(cam.rotationZ);
    var cosZ = Math.cos(cam.rotationZ);
    nextX = x * cosZ - y * sinZ;
    nextY = x * sinZ + y * cosZ;

    return sceneWaterNormalizeReflectionDirection({ x: nextX, y: nextY, z: z });
  }

  function sceneWaterCameraWorldPosition(camera) {
    var cam = sceneRenderCamera(camera);
    return { x: cam.x, y: cam.y, z: cam.z };
  }

  function sceneWaterCameraWorldDirection(camera) {
    var cam = sceneRenderCamera(camera);
    var forward = sceneWaterReflectionCameraForward(cam);
    return sceneWaterNormalizeReflectionDirection({ x: -forward.x, y: -forward.y, z: -forward.z });
  }

  function sceneWaterMirrorWaterPoint(point) {
    return {
      x: sceneNumber(point && point.x, 0),
      y: -sceneNumber(point && point.y, 0),
      z: sceneNumber(point && point.z, 0),
    };
  }

  function sceneWaterReflectionCameraUp(camera) {
    var up = sceneWaterNormalizeReflectionDirection({
      x: sceneNumber(camera && camera.upX, 0),
      y: sceneNumber(camera && camera.upY, 1),
      z: sceneNumber(camera && camera.upZ, 0),
    });
    return { x: up.x, y: -up.y, z: up.z };
  }

  function sceneWaterLookAtViewMatrix(eye, target, up, out) {
    var zx = sceneNumber(eye && eye.x, 0) - sceneNumber(target && target.x, 0);
    var zy = sceneNumber(eye && eye.y, 0) - sceneNumber(target && target.y, 0);
    var zz = sceneNumber(eye && eye.z, 0) - sceneNumber(target && target.z, 0);
    var length = Math.sqrt(zx * zx + zy * zy + zz * zz);
    if (length <= 0.000001) {
      zx = 0;
      zy = 0;
      zz = 1;
    } else {
      zx /= length;
      zy /= length;
      zz /= length;
    }

    var upv = sceneWaterNormalizeReflectionDirection(up);
    var xx = upv.y * zz - upv.z * zy;
    var xy = upv.z * zx - upv.x * zz;
    var xz = upv.x * zy - upv.y * zx;
    length = Math.sqrt(xx * xx + xy * xy + xz * xz);
    if (length <= 0.000001) {
      upv = Math.abs(zy) < 0.999 ? { x: 0, y: 1, z: 0 } : { x: 1, y: 0, z: 0 };
      xx = upv.y * zz - upv.z * zy;
      xy = upv.z * zx - upv.x * zz;
      xz = upv.x * zy - upv.y * zx;
      length = Math.sqrt(xx * xx + xy * xy + xz * xz);
    }
    if (length <= 0.000001) {
      xx = 1;
      xy = 0;
      xz = 0;
    } else {
      xx /= length;
      xy /= length;
      xz /= length;
    }

    var yx = zy * xz - zz * xy;
    var yy = zz * xx - zx * xz;
    var yz = zx * xy - zy * xx;

    out[0] = xx;
    out[1] = yx;
    out[2] = zx;
    out[3] = 0;
    out[4] = xy;
    out[5] = yy;
    out[6] = zy;
    out[7] = 0;
    out[8] = xz;
    out[9] = yz;
    out[10] = zz;
    out[11] = 0;
    out[12] = -(xx * eye.x + xy * eye.y + xz * eye.z);
    out[13] = -(yx * eye.x + yy * eye.y + yz * eye.z);
    out[14] = -(zx * eye.x + zy * eye.y + zz * eye.z);
    out[15] = 1;
    return out;
  }

  function addWaterObjectTextureStats(stats, system, passCount, targetCount, meshDrawCalls, fallbackPasses, selenaDrawCalls) {
    var targetWidth = Math.max(0, system && (system.objectTextureWidth || system.objectTextureResolution) || 0);
    var targetHeight = Math.max(0, system && (system.objectTextureHeight || system.objectTextureResolution) || 0);
    stats.waterObjectTexturePasses += Math.max(0, passCount || 0);
    stats.waterObjectTextureTargets += Math.max(0, targetCount || 0);
    stats.waterObjectTexturePixels += Math.max(0, targetCount || 0) * targetWidth * targetHeight;
    stats.waterObjectTextureWidth = Math.max(stats.waterObjectTextureWidth || 0, targetWidth);
    stats.waterObjectTextureHeight = Math.max(stats.waterObjectTextureHeight || 0, targetHeight);
    stats.waterObjectTexturePixelBudget = Math.max(stats.waterObjectTexturePixelBudget || 0, Math.max(0, system && system.objectTexturePixelBudget || 0));
    stats.waterObjectTextureMeshPasses += Math.max(0, passCount || 0) - Math.max(0, fallbackPasses || 0);
    stats.waterObjectTextureMeshDrawCalls += Math.max(0, meshDrawCalls || 0);
    stats.waterObjectTextureSelenaDrawCalls += Math.max(0, selenaDrawCalls || 0);
    stats.waterObjectTextureFallbackPasses += Math.max(0, fallbackPasses || 0);
  }

  // sceneWaterSelenaResourceRef builds a "gosx:water:<id>:<slot>" reference
  // string, the resource-ref convention sceneSelenaResourceRef/
  // sceneSelenaLiveTextureView/sceneSelenaLiveBuffer already parse for the
  // water-object custom materials AND for every water-system-owned Selena
  // pass below (pool, surface, surface-below, caustics, ...).
  function sceneWaterSelenaResourceRef(system, slot) {
    var id = String((system && system.id) || (system && system.entry && system.entry.id) || "water-main");
    return "gosx:water:" + id + ":" + slot;
  }

  // sceneWaterSpheresContextArray packs system.waterObjectSpheres (cached by
  // sceneWaterObjectState, the SAME array sceneWaterWriteObjectSphereBuffer
  // uploads to the hand-written objectSpheres SSBO) into the flat
  // Float32Array(WATER_MAX_DISPLACEMENT_SPHERES*4) shape the G1 array-uniform
  // packer (sceneSelenaWriteArrayUniformField) expects for a
  // `context { spheres : array<vec4,32> }` field -- used by surface/
  // surface-below/caustics/compound-shadow. Mirrors
  // sceneWaterWriteObjectSphereBuffer's per-sphere (x,y,z,radius) packing
  // exactly, so the Selena context array and the hand-written SSBO carry
  // byte-identical sphere data for the same frame.
  function sceneWaterSpheresContextArray(system) {
    var spheres = (system && Array.isArray(system.waterObjectSpheres)) ? system.waterObjectSpheres : [];
    var out = new Float32Array(WATER_MAX_DISPLACEMENT_SPHERES * 4);
    for (var i = 0; i < spheres.length && i < WATER_MAX_DISPLACEMENT_SPHERES; i++) {
      var sphere = spheres[i] || {};
      var offset = i * 4;
      out[offset] = sceneNumber(sphere.x, 0);
      out[offset + 1] = sceneNumber(sphere.y, 0);
      out[offset + 2] = sceneNumber(sphere.z, 0);
      out[offset + 3] = Math.max(0.0001, sceneNumber(sphere.radius, 0));
    }
    return out;
  }

  // sceneWaterSelenaUsesPass gates a pass on its Selena WGSL+descriptor both
  // being present (a pass-specific caller may AND in additional conditions,
  // e.g. sceneWaterPoolUsesSelena's "not rounded" check below).
  function sceneWaterSelenaUsesPass(entry, wgslField, descKey) {
    var wgsl = entry && typeof entry[wgslField] === "string" ? entry[wgslField].trim() : "";
    if (!wgsl) return false;
    var descriptors = entry && entry.shaderDescriptors;
    var layout = descriptors && typeof descriptors === "object" ? descriptors[descKey] : null;
    return !!(layout && typeof layout === "object" && layout.uniformBlock);
  }

  // sceneWaterSelenaMaterial resolves+memoizes the Selena "material"
  // envelope the generic getSelenaPipeline/createSelenaBindGroup (mesh kind)
  // and getSelenaPostPipeline/createSelenaPostBindGroup (post kind) paths
  // expect: a single combined vertex+fragment WGSL module (every Selena
  // water shader emits one module with both entry points, unlike the
  // hand-written contract which often splits vertex/fragment into two WGSL
  // sources) plus its host binding descriptor (entry.shaderDescriptors[descKey],
  // already compiled for the WebGL2 path -- Selena's bindings.Layout is
  // backend-agnostic, see selena_glsl.go / selena_wgsl_binding_test.go
  // TestWaterSelenaWGSLDescriptorMatchesBindings). Returns null when either
  // the WGSL or descriptor is missing, so the caller falls back to the
  // hand-written path. The resolved material is memoized on
  // `system[memoSlot]` (one stable object per pass per system, so
  // getSelenaPipeline's per-material pipeline memo and
  // createSelenaBindGroup's per-owner bind-group pool stay warm across
  // frames); callers still assign a FRESH customUniforms object every call
  // (uniform VALUES are per-frame, only the WGSL/layout identity is stable).
  function sceneWaterSelenaMaterial(system, entry, wgslField, descKey, memoSlot) {
    if (!system) return null;
    var wgsl = entry && typeof entry[wgslField] === "string" ? entry[wgslField].trim() : "";
    if (!wgsl) return null;
    var descriptors = entry && entry.shaderDescriptors;
    var layout = descriptors && typeof descriptors === "object" ? descriptors[descKey] : null;
    if (!layout || typeof layout !== "object" || !layout.uniformBlock) return null;

    var material = system[memoSlot];
    if (!material || material._gosxSelenaWGSLSrc !== wgsl || material._gosxSelenaLayoutRef !== layout) {
      material = { shaderBackend: "selena", customVertexWGSL: wgsl, customFragmentWGSL: wgsl, shaderLayout: layout };
      material._gosxSelenaWGSLSrc = wgsl;
      material._gosxSelenaLayoutRef = layout;
      system[memoSlot] = material;
    }
    return material;
  }

  // sceneWaterPoolSelenaRenderContext builds the per-frame name→value
  // uniform map (renderContext.uniforms) plus the StateGrid size
  // (renderContext.grid) for the pool pass. It reads the ALREADY-COMPUTED
  // per-frame derivations sceneWaterUniformData stamps onto the system
  // (waterPoolWidth/Height/Length, waterLightDir, waterResolution) rather
  // than recomputing them, so the Selena path and the hand-written path see
  // byte-identical pool geometry/lighting inputs for the same entry config.
  // mvp/normalMatrix are NOT included here: sceneSelenaUniformValue already
  // supplies those automatically (reserved auto-uniforms, priority before
  // renderContext.uniforms) from scratchSelenaViewProjection.
  // sceneWaterPoolSelenaRenderContext builds the per-frame name→value
  // uniform map (renderContext.uniforms) plus the StateGrid size
  // (renderContext.grid) for the pool pass. It reads the ALREADY-COMPUTED
  // per-frame derivations sceneWaterUniformData stamps onto the system
  // (waterPoolWidth/Height/Length, waterCornerRadius, waterLightDir,
  // waterResolution) rather than recomputing them, so the Selena path and
  // the hand-written path see byte-identical pool geometry/lighting inputs
  // for the same entry config. cornerRadius/poolShape feed pool.sel's own
  // `poolShape > 0.5 && cornerRadius > 0.0001` gate (mirroring the
  // hand-written pool.vertex.sel contract exactly): poolShape is derived
  // straight from entry.poolShape (wgpuSceneWaterPoolShapeRounded), independent
  // of whatever radius clamping sceneWaterUniformData already applied to
  // waterCornerRadius, so the shader's own gate is the single source of
  // truth for whether the rounded geometry is actually active.
  function sceneWaterPoolSelenaRenderContext(system) {
    var light = (system && system.waterLightDir) || { x: 0.3, y: 0.9, z: 0.45 };
    var entry = (system && system.entry) || {};
    return {
      uniformSlotSuffix: "water-pool-" + String((system && system.id) || "water"),
      uniforms: {
        poolWidth: sceneNumber(system && system.waterPoolWidth, 1),
        poolLength: sceneNumber(system && system.waterPoolLength, 1),
        poolHeight: sceneNumber(system && system.waterPoolHeight, 1),
        cornerRadius: sceneNumber(system && system.waterCornerRadius, 0),
        poolShape: wgpuSceneWaterPoolShapeRounded(entry) ? 1 : 0,
        lightDir: [sceneNumber(light.x, 0.3), sceneNumber(light.y, 0.9), sceneNumber(light.z, 0.45)],
      },
      grid: sceneNumber(system && system.waterResolution, 256),
    };
  }

  // sceneWaterPoolUsesSelena gates the new path: the Selena WGSL+descriptor
  // must both be present. pool.sel now ports the rounded-corner geometry
  // (see pool.sel's header comment), so both the box and rounded shapes
  // route through the Selena path; `rounded` is accepted for call-site
  // symmetry with sceneWaterSelenaUsesPass callers but no longer used to
  // force a fallback.
  function sceneWaterPoolUsesSelena(entry, rounded) {
    return sceneWaterSelenaUsesPass(entry, "poolSelenaWGSL", "pool");
  }

  // sceneWaterCameraPosFromCam extracts the water shaders' "cameraPos"
  // convention from the normalized camera object. It matches the
  // world-space eye position in FrameUniforms.cameraPos.
  function sceneWaterCameraPosFromCam(cam) {
    if (!cam) return { x: 0, y: 0, z: 0 };
    return {
      x: sceneNumber(cam.x, 0),
      y: sceneNumber(cam.y, 0),
      z: cam.mode === "ortho2d" ? 0 : sceneNumber(cam.z, 0),
    };
  }

  function sceneWaterSurfaceUsesSelena(entry) {
    return sceneWaterSelenaUsesPass(entry, "surfaceSelenaWGSL", "surface");
  }

  function sceneWaterSurfaceBelowUsesSelena(entry) {
    return sceneWaterSelenaUsesPass(entry, "surfaceBelowSelenaWGSL", "surfaceBelow");
  }

  function sceneWaterCausticsSelenaMaterial(system, entry) {
    var material = sceneWaterSelenaMaterial(system, entry, "causticsSelenaWGSL", "caustics", "_selenaCausticsMaterial");
    if (!material) return null;
    // objectShadowTexture: the SAME "gosx:water:<id>:shadow" resource-ref
    // slot water-pool.sel already binds as `shadowTexture` (resolves to
    // system.objectShadowView, sceneSelenaLiveTextureView's "shadow"/
    // "objectShadow" case). Closes M2's meshShadowTextureOcclusion gap --
    // see caustics.sel's header RESOLVED note.
    material.customUniforms = {
      height: sceneWaterSelenaResourceRef(system, "state"),
      objectShadowTexture: sceneWaterSelenaResourceRef(system, "shadow"),
    };
    return material;
  }

  function sceneWaterCausticsUsesSelena(entry) {
    return sceneWaterSelenaUsesPass(entry, "causticsSelenaWGSL", "caustics");
  }

  // objectCenterY joined the shared context base for object-shadow.sel's P2
  // (water-parity-campaign) analytic rewrite, which reconstructs a 3D
  // world-space floor point and needs the object's full 3D center (not just
  // X/Z) for the sphereSoftShadow/cubeOcclusion terms. compound-shadow.sel's
  // compiled layout has no "objectCenterY" uniform field, so sharing this
  // base with sceneWaterCompoundShadowSelenaRenderContext is harmless --
  // sceneSelenaUniformData only reads uniforms.* keys that are actually
  // present in the COMPILED layout's fields (extra keys are ignored).
  function sceneWaterObjectShadowSelenaContextBase(system) {
    var light = (system && system.waterLightDir) || { x: 0.3, y: 0.9, z: 0.45 };
    var center = (system && system.waterObjectCenter) || { x: 0, y: 0, z: 0 };
    return {
      lightDir: [sceneNumber(light.x, 0.3), sceneNumber(light.y, 0.9), sceneNumber(light.z, 0.45)],
      objectCenterX: sceneNumber(center.x, 0),
      objectCenterY: sceneNumber(center.y, 0),
      objectCenterZ: sceneNumber(center.z, 0),
    };
  }

  function sceneWaterObjectShadowSelenaMaterial(system, entry) {
    var material = sceneWaterSelenaMaterial(system, entry, "objectShadowSelenaWGSL", "objectShadow", "_selenaObjectShadowMaterial");
    if (!material) return null;
    var half = (system && system.waterObjectHalfSize) || { x: 0, y: 0, z: 0 };
    material.customUniforms = {
      objectKind: sceneNumber(system && system.waterObjectKind, 0),
      objectEnabled: (system && system.waterObjectActive) ? 1 : 0,
      poolWidth: sceneNumber(system && system.waterPoolWidth, 1.5),
      poolLength: sceneNumber(system && system.waterPoolLength, 1.5),
      // poolHeight: the P2 analytic rewrite reconstructs a 3D floor point at
      // y = -poolHeight (matching caustics.sel's own floor-plane convention,
      // including its identical sceneNumber(...,1) fallback -- see
      // sceneWaterCausticsSelenaRenderContext above) instead of working
      // purely in UV space, so the live pool depth must reach the shader
      // instead of pinning to the compiled default.
      poolHeight: sceneNumber(system && system.waterPoolHeight, 1),
      objectRadius: sceneNumber(system && system.waterObjectRadius, 0.1),
      objectHalfX: sceneNumber(half.x, 0.1),
      objectHalfY: sceneNumber(half.y, 0.1),
      objectHalfZ: sceneNumber(half.z, 0.1),
    };
    return material;
  }

  function sceneWaterObjectShadowSelenaRenderContext(system) {
    return {
      uniformSlotSuffix: "water-object-shadow-" + String((system && system.id) || "water"),
      uniforms: sceneWaterObjectShadowSelenaContextBase(system),
    };
  }

  function sceneWaterCompoundShadowSelenaMaterial(system, entry) {
    var material = sceneWaterSelenaMaterial(system, entry, "compoundShadowSelenaWGSL", "compoundShadow", "_selenaCompoundShadowMaterial");
    if (!material) return null;
    material.customUniforms = {
      sphereCount: sceneNumber(system && system.waterObjectSphereCount, 0),
      objectEnabled: (system && system.waterObjectActive) ? 1 : 0,
      objectTop: sceneNumber(system && system.waterObjectCenter && system.waterObjectCenter.y, 0) + Math.max(
        sceneNumber(system && system.waterObjectHalfSize && system.waterObjectHalfSize.y, 0),
        sceneNumber(system && system.waterObjectRadius, 0)
      ),
      poolWidth: sceneNumber(system && system.waterPoolWidth, 1.5),
      poolLength: sceneNumber(system && system.waterPoolLength, 1.5),
    };
    return material;
  }

  function sceneWaterObjectShadowUsesSelena(entry) {
    return sceneWaterSelenaUsesPass(entry, "objectShadowSelenaWGSL", "objectShadow");
  }

  function sceneWaterCompoundShadowUsesSelena(entry) {
    return sceneWaterSelenaUsesPass(entry, "compoundShadowSelenaWGSL", "compoundShadow");
  }

  function sceneWaterObjectMeshShadowSelenaMaterial(system, entry) {
    var material = sceneWaterSelenaMaterial(system, entry, "objectMeshShadowSelenaWGSL", "objectMeshShadow", "_selenaObjectMeshShadowMaterial");
    if (!material) return null;
    material.customUniforms = {
      poolHalfW: sceneNumber(system && system.waterPoolWidth, 1.5),
      poolHalfL: sceneNumber(system && system.waterPoolLength, 1.5),
    };
    return material;
  }

  function sceneWaterObjectMeshShadowSelenaRenderContext(system) {
    var light = (system && system.waterLightDir) || { x: 0.3, y: 0.9, z: 0.45 };
    return {
      uniformSlotSuffix: "water-object-mesh-shadow-" + String((system && system.id) || "water"),
      uniforms: {
        lightDir: [sceneNumber(light.x, 0.3), sceneNumber(light.y, 0.9), sceneNumber(light.z, 0.45)],
      },
    };
  }

  function sceneWaterObjectMeshShadowUsesSelena(entry) {
    return sceneWaterSelenaUsesPass(entry, "objectMeshShadowSelenaWGSL", "objectMeshShadow");
  }

  // GPU-faithful hash mirroring the WebGPU/WebGL seedDrops kernel's
  // hash01(n)=fract(sin(n)*k) (see 16-scene-webgl.js's identical
  // sceneWaterHash01/sceneWaterBuildSeedDrops -- duplicated here rather than
  // shared because bootstrap-feature-scene3d-webgpu.js's build bundles
  // 16a-scene-webgpu.js WITHOUT 16-scene-webgl.js, see build-bootstrap.mjs).
  function sceneWaterSelenaHash01(n) {
    var v = Math.sin(n) * 43758.5453123;
    return v - Math.floor(v);
  }

  // sceneWaterSelenaSeedDropsContextArray builds the seed kernel's
  // `context { drops : array<vec4,64> }` value: per-drop hashed UV centers
  // (host-side, since seed.sel's context contract carries pre-computed drops
  // rather than a loop-local RNG) with the polarity folded into a signed
  // strength component, replicating the hardcoded seedDrops kernel's
  // hash01(jf*12.9898+seedSalt+0.173)/hash01(jf*78.233+seedSalt*1.371+0.719)
  // + select(1,-1,(j&1u)==0u) math exactly (SCENE_WATER_COMPUTE_SOURCE
  // above). `.z` is unused by seed.sel's body (only `.xy`/`.w` are read) but
  // packed as 0 for a well-formed vec4.
  function sceneWaterSelenaSeedDropsContextArray(count, seedSalt, dropStrength) {
    var n = Math.max(0, Math.min(64, Math.floor(count)));
    var out = new Float32Array(64 * 4);
    for (var j = 0; j < n; j++) {
      var jf = j + 1;
      out[j * 4 + 0] = sceneWaterSelenaHash01(jf * 12.9898 + seedSalt + 0.173);
      out[j * 4 + 1] = sceneWaterSelenaHash01(jf * 78.233 + seedSalt * 1.371 + 0.719);
      out[j * 4 + 2] = 0;
      out[j * 4 + 3] = dropStrength * ((j & 1) === 0 ? -1 : 1);
    }
    return out;
  }

  // sceneWaterDisplacementSpheresContextArray packs a plain JS sphere list
  // ({x,y,z,radius}[]) into the flat Float32Array(WATER_MAX_DISPLACEMENT_
  // SPHERES*4) shape the G1 array-uniform packer expects for the
  // displacement kernel's `context { spheres : array<vec4,32> }` field.
  // Deliberately separate from sceneWaterSpheresContextArray (which reads
  // system.waterObjectSpheres): the displacement kernel's per-dispatch
  // spheres come from system._waterComputeObjectState.spheres (see
  // sceneWaterDisplacementSelenaRenderContext below), which stays correct
  // for the one-shot objectDisplacementEvents path where
  // system.waterObjectSpheres is NOT refreshed (sceneWaterObjectState skips
  // all system-field mutation when called with transientObject:true).
  function sceneWaterDisplacementSpheresContextArray(list) {
    var spheres = Array.isArray(list) ? list : [];
    var out = new Float32Array(WATER_MAX_DISPLACEMENT_SPHERES * 4);
    for (var i = 0; i < spheres.length && i < WATER_MAX_DISPLACEMENT_SPHERES; i++) {
      var sphere = spheres[i] || {};
      var offset = i * 4;
      out[offset] = sceneNumber(sphere.x, 0);
      out[offset + 1] = sceneNumber(sphere.y, 0);
      out[offset + 2] = sceneNumber(sphere.z, 0);
      out[offset + 3] = Math.max(0.0001, sceneNumber(sphere.radius, 0));
    }
    return out;
  }

  // sceneWaterComputeStageFields maps a compute stage name to its additive
  // WaterSystem entry slots: wgslField is the entry-level field name (e.g.
  // "seedSelenaWGSL", matching WaterSystemIR's json tag / page.gsx's prop
  // name -- NOT the Go-side WaterDemoData top-level key, which carries a
  // "water" prefix, e.g. data.waterSeedSelenaWGSL, see selena_glsl.go's
  // waterSelenaComputeWGSLData/page.gsx's seedSelenaWGSL={data.waterSeedSelenaWGSL}),
  // descKey matching shaderDescriptors.
  function sceneWaterComputeStageFields(stage) {
    switch (stage) {
    case "seed": return { wgslField: "seedSelenaWGSL", descKey: "seed", memoSlot: "_selenaSeedMaterial" };
    case "drop": return { wgslField: "dropSelenaWGSL", descKey: "drop", memoSlot: "_selenaDropMaterial" };
    case "displacement": return { wgslField: "displacementSelenaWGSL", descKey: "displacement", memoSlot: "_selenaDisplacementMaterial" };
    case "simulation": return { wgslField: "simulationSelenaWGSL", descKey: "simulation", memoSlot: "_selenaSimulationMaterial" };
    case "normal": return { wgslField: "normalSelenaWGSL", descKey: "normal", memoSlot: "_selenaNormalMaterial" };
    default: return null;
    }
  }

  function sceneWaterComputeStageUsesSelena(entry, stage) {
    var fields = sceneWaterComputeStageFields(stage);
    if (!fields) return false;
    return sceneWaterSelenaUsesPass(entry, fields.wgslField, fields.descKey);
  }

  // sceneWaterSeedSelenaRenderContext: seed kernel context = P{dropRadius} +
  // C{dropCount,drops[64]} (design doc 3.5). dropCount/dropStrength/
  // dropRadius derivations mirror sceneWaterUniformData's byte-packed
  // seedDrops/dropStrength/dropRadius clamps exactly (waterUniformScratchU[2],
  // waterUniformScratchF[8]/[9]) so the Selena path reproduces the SAME
  // per-drop values as the hardcoded seedDrops kernel for a given seedSalt.
  function sceneWaterSeedSelenaRenderContext(system, entry) {
    var count = Math.max(0, Math.min(64, Math.floor(sceneNumber(entry && entry.seedDrops, 7))));
    var dropStrength = Math.max(-1, Math.min(1, sceneNumber(entry && entry.dropStrength, 0.01)));
    var dropRadius = Math.max(0.0001, Math.min(0.5, sceneNumber(entry && entry.dropRadius, 0.03)));
    var seedSalt = Math.max(0, sceneNumber(system && system.seedSalt, 0));
    return {
      uniformSlotSuffix: "water-seed-" + String((system && system.id) || "water"),
      uniforms: {
        dropRadius: dropRadius,
        dropCount: count,
        drops: sceneWaterSelenaSeedDropsContextArray(count, seedSalt, dropStrength),
      },
    };
  }

  // sceneWaterDropSelenaRenderContext: drop kernel context = C{dropCenter,
  // dropRadius,dropStrength}, sourced from the SAME entry.dropX/dropZ/
  // dropEventRadius/dropEventStrength fields (with dropRadius/dropStrength
  // fallback) sceneWaterUniformData packs into interactiveDrop.xyzw
  // (waterUniformScratchF[48]-[51]).
  function sceneWaterDropSelenaRenderContext(system, entry) {
    var dropX = Math.max(-1, Math.min(1, sceneNumber(entry && entry.dropX, 0)));
    var dropZ = Math.max(-1, Math.min(1, sceneNumber(entry && entry.dropZ, 0)));
    var dropRadius = Math.max(0.0001, Math.min(0.5, sceneNumber(entry && entry.dropEventRadius, sceneNumber(entry && entry.dropRadius, 0.03))));
    var dropStrength = Math.max(-1, Math.min(1, sceneNumber(entry && entry.dropEventStrength, sceneNumber(entry && entry.dropStrength, 0.01))));
    return {
      uniformSlotSuffix: "water-drop-" + String((system && system.id) || "water"),
      uniforms: {
        dropCenter: [dropX, dropZ],
        dropRadius: dropRadius,
        dropStrength: dropStrength,
      },
    };
  }

  // sceneWaterDisplacementSelenaRenderContext: displacement kernel context =
  // C{objectKind,displacementScale,objectCenter,objectPrevCenter,
  // objectRadius,objectHalfSize,sphereCount,spheres[32]}. Reads
  // system._waterComputeObjectState (stashed by sceneWaterUniformData every
  // call, including transientObject:true event calls) rather than the
  // system.waterObject* fields the render passes use, because those fields
  // are NOT refreshed during a one-shot objectDisplacementEvents dispatch
  // (sceneWaterObjectState skips system mutation when system is null) --
  // the stashed objectState is always the exact object the immediately-
  // preceding sceneWaterUniformData call computed, for both the continuous
  // per-frame dispatch and each individual event dispatch.
  function sceneWaterDisplacementSelenaRenderContext(system) {
    var state = (system && system._waterComputeObjectState) || {};
    var center = state.center || { x: 0, y: 0, z: 0 };
    var previous = state.previous || center;
    var half = state.halfSize || { x: 0, y: 0, z: 0 };
    var spheres = Array.isArray(state.spheres) ? state.spheres : [];
    return {
      uniformSlotSuffix: "water-displacement-" + String((system && system.id) || "water"),
      uniforms: {
        objectKind: sceneNumber(state.kind, 0),
        displacementScale: sceneNumber(state.displacementScale, 0),
        objectCenter: [sceneNumber(center.x, 0), sceneNumber(center.y, 0), sceneNumber(center.z, 0)],
        objectPrevCenter: [sceneNumber(previous.x, 0), sceneNumber(previous.y, 0), sceneNumber(previous.z, 0)],
        objectRadius: Math.max(0.0001, sceneNumber(state.radius, 0.0001)),
        objectHalfSize: [sceneNumber(half.x, 0), sceneNumber(half.y, 0), sceneNumber(half.z, 0)],
        sphereCount: Math.min(WATER_MAX_DISPLACEMENT_SPHERES, spheres.length),
        spheres: sceneWaterDisplacementSpheresContextArray(spheres),
      },
    };
  }

  // sceneWaterSimulationSelenaRenderContext: simulation kernel has no
  // context fields, only params waveSpeed/damping (simulation.sel:5-6) --
  // still supplied via renderContext.uniforms (the packer doesn't
  // distinguish param vs context provenance, see sceneSelenaUniformValue),
  // clamped exactly like the hardcoded path's waterUniformScratchF[6]/[7].
  function sceneWaterSimulationSelenaRenderContext(system, entry) {
    return {
      uniformSlotSuffix: "water-simulation-" + String((system && system.id) || "water"),
      uniforms: {
        waveSpeed: Math.max(0, Math.min(2, sceneNumber(entry && entry.waveSpeed, 1.0))),
        damping: Math.max(0, Math.min(1, sceneNumber(entry && entry.damping, 0.995))),
      },
    };
  }

  // Physical cell spacing is part of the normal contract: height deltas are
  // world-space values, so unit-length X/Z tangents would flatten the water
  // by resolution/(2*poolExtent) and suppress both optics and caustics.
  function sceneWaterNormalSelenaRenderContext(system) {
    var resolution = Math.max(1, sceneNumber(system && system.resolution, 256));
    var poolWidth = Math.max(0.0001, sceneNumber(system && system.waterPoolWidth, 1));
    var poolLength = Math.max(0.0001, sceneNumber(system && system.waterPoolLength, 1));
    return {
      uniformSlotSuffix: "water-normal-" + String((system && system.id) || "water"),
      uniforms: {
        cellSizeX: 2 * poolWidth / resolution,
        cellSizeZ: 2 * poolLength / resolution,
      },
    };
  }

  function sceneWaterComputeStageRenderContext(system, entry, stage) {
    switch (stage) {
    case "seed": return sceneWaterSeedSelenaRenderContext(system, entry);
    case "drop": return sceneWaterDropSelenaRenderContext(system, entry);
    case "displacement": return sceneWaterDisplacementSelenaRenderContext(system);
    case "simulation": return sceneWaterSimulationSelenaRenderContext(system, entry);
    case "normal": return sceneWaterNormalSelenaRenderContext(system);
    default: return null;
    }
  }

  // dispatchWaterPassSelena mirrors dispatchWaterPass's dispatch math and
  // ping-pong swap exactly (Math.ceil(system.cellCount/64) workgroups,
  // system.activeIndex toggling 0<->1 after the pass), but binds an
  // explicit {pipeline,bindGroup} pair (built fresh per kernel per frame from
  // the live renderContext) instead of the hardcoded
  // system.computeBindGroups[system.activeIndex] pair. sharedPass mirrors
  // dispatchWaterPass's sharedPass parameter -- see its comment.
  function dispatchWaterPassSelena(encoder, system, draw, sharedPass) {
    if (!system || !draw || !draw.pipeline || !draw.bindGroup) return 0;
    var pass = sharedPass || (encoder && encoder.beginComputePass({ label: "gosx-water-selena-compute-pass" }));
    if (!pass) return 0;
    pass.setPipeline(wgpuRequirePipeline(draw.pipeline));
    pass.setBindGroup(0, draw.bindGroup);
    pass.dispatchWorkgroups(Math.ceil(system.cellCount / 64));
    if (!sharedPass) pass.end();
    system.activeIndex = system.activeIndex === 0 ? 1 : 0;
    return 1;
  }

  // Selena render materials lower stateAt(uv) to textureLoad so vertex and
  // fragment reads share the texture cache. The simulation itself remains a
  // storage-buffer compute pipeline. Refresh the sampled mirror after the
  // final normal pass, in the same command encoder, so all following
  // caustic/pool/surface passes observe the exact active ping-pong state.
  function syncWaterSampledState(encoder, system) {
    if (!encoder || typeof encoder.copyBufferToTexture !== "function" || !system) return 0;
    var resolution = Math.max(16, Math.floor(sceneNumber(system.resolution, 16)));
    var source = system.activeIndex === 0 ? system.bufferA : system.bufferB;
    var destination = system.activeIndex === 0 ? system.stateTextureA : system.stateTextureB;
    if (!source || !destination) return 0;
    encoder.copyBufferToTexture(
      { buffer: source, bytesPerRow: resolution * 16, rowsPerImage: resolution },
      { texture: destination },
      [resolution, resolution, 1]
    );
    system.stateTextureSyncSeq = Math.max(0, Math.floor(sceneNumber(system.stateTextureSyncSeq, 0))) + 1;
    return 1;
  }
  function sceneWaterObjectState(system, entry, timeSeconds, poolWidth, poolLength) {
    var kind = sceneWaterObjectKind(entry);
    if (!kind) {
      if (system) {
        system.waterObjectMoved = system.waterObjectActive === true;
        system.waterObjectKind = 0;
        system.waterObjectLabel = "";
        system.waterObjectActive = false;
        system.waterObjectSphereCount = 0;
        system.waterObjectHalfSize = { x: 0, y: 0, z: 0 };
        system.waterObjectCenter = { x: 0, y: 0, z: 0 };
        system.waterObjectDisplacementScale = 0;
        system.waterObjectSpheres = [];
      }
      return {
        kind: 0,
        center: { x: 0, y: 0, z: 0 },
        previous: { x: 0, y: 0, z: 0 },
        halfSize: { x: 0, y: 0, z: 0 },
        radius: 0,
        displacementScale: 0,
        subtype: 0,
        spheres: [],
      };
    }
    var currentWorld = sceneWaterObjectCenter(entry, timeSeconds);
    var current = sceneWaterNormalizeObjectCenter(currentWorld, poolWidth, poolLength);
    var signature = sceneWaterObjectMotionSignature(entry, kind);
    var lastCenter = system && system.waterObjectPrevious;
    var objectMoved = !lastCenter || system.waterObjectSignature !== signature ||
      current.x !== lastCenter.x || current.y !== lastCenter.y || current.z !== lastCenter.z;
    var previous = current;
    var explicitPreviousSignature = sceneWaterObjectExplicitPreviousSignature(entry, kind);
    if (system && explicitPreviousSignature && system.waterObjectExplicitPreviousSignature !== explicitPreviousSignature) {
      previous = sceneWaterNormalizeObjectCenter({
        x: sceneNumber(entry && entry.objectPreviousX, currentWorld.x),
        y: sceneNumber(entry && entry.objectPreviousY, currentWorld.y),
        z: sceneNumber(entry && entry.objectPreviousZ, currentWorld.z),
      }, poolWidth, poolLength);
      system.waterObjectExplicitPreviousSignature = explicitPreviousSignature;
    } else if (system && system.waterObjectSignature === signature && system.waterObjectPrevious) {
      previous = system.waterObjectPrevious;
    }
    var halfWidth = Math.max(0.0001, poolWidth);
    var halfLength = Math.max(0.0001, poolLength);
    var radius = sceneNumber(entry && entry.objectRadius, 0);
    if (radius <= 0) radius = kind === 1 ? 0.25 : 0.31;
    var halfSizeX = sceneNumber(entry && entry.objectHalfSizeX, 0);
    var halfSizeY = sceneNumber(entry && entry.objectHalfSizeY, 0);
    var halfSizeZ = sceneNumber(entry && entry.objectHalfSizeZ, 0);
    if (kind === 2) {
      if (halfSizeX <= 0) halfSizeX = radius;
      if (halfSizeY <= 0) halfSizeY = radius;
      if (halfSizeZ <= 0) halfSizeZ = radius;
    }
    var spheres = kind === 3 ? sceneWaterDisplacementSpheres(entry, poolWidth, poolLength) : [];
    var subtype = sceneWaterObjectSubtype(entry, kind);
    var active = kind === 1 || kind === 2 || spheres.length > 0;
    var normalizedHalfSize = {
      x: Math.max(0, halfSizeX) / halfWidth,
      y: Math.max(0, halfSizeY),
      z: Math.max(0, halfSizeZ) / halfLength,
    };
    var normalizedDisplacementScale = Math.max(0, sceneNumber(entry && entry.objectDisplacementScale, 1));
    if (system) {
      system.waterObjectMoved = objectMoved;
      system.waterObjectSignature = signature;
      system.waterObjectPrevious = current;
      system.waterObjectKind = active ? kind : 0;
      system.waterObjectActive = active;
      system.waterObjectSphereCount = spheres.length;
      system.waterObjectSubtype = active ? subtype : 0;
      system.waterObjectRadius = active ? Math.max(0.0001, radius) : 0;
      system.waterObjectLabel = kind === 1 ? "sphere" : kind === 2 ? "cube" : subtype === 1 ? "torus-knot" : subtype === 2 ? "mesh" : "compound";
      // Selena general-pass render-context support (surface/surfaceBelow/
      // caustics/compound-shadow context fields: objectHalf/objectCenter/
      // spheres[]): cache these the same way waterObjectRadius/waterObjectKind
      // already are, so per-pass Selena render-context builders read live
      // per-frame values instead of recomputing this function's math.
      system.waterObjectHalfSize = normalizedHalfSize;
      system.waterObjectCenter = current;
      system.waterObjectDisplacementScale = normalizedDisplacementScale;
      system.waterObjectSpheres = spheres;
    }
    return {
      kind: kind,
      center: current,
      previous: previous,
      halfSize: normalizedHalfSize,
      radius: Math.max(0.0001, radius) / halfLength,
      displacementScale: normalizedDisplacementScale,
      subtype: subtype,
      spheres: spheres,
    };
  }

  function dispatchWaterObjectDisplacementEvents(system, entry, encoder, pipeline, currentTime, waterState: any, device: any) {
    if (!system || !encoder || !pipeline) {
      return { dispatches: 0, selena: 0, selenaFallback: 0, lastID: Math.max(0, Math.floor(sceneNumber(system && system.lastObjectDisplacementEventID, 0))) };
    }
    var events = sceneWaterObjectDisplacementEvents(entry);
    if (!events.length) {
      return { dispatches: 0, selena: 0, selenaFallback: 0, lastID: Math.max(0, Math.floor(sceneNumber(system.lastObjectDisplacementEventID, 0))) };
    }
    var lastID = Math.max(0, Math.floor(sceneNumber(system.lastObjectDisplacementEventID, 0)));
    var nextLastID = lastID;
    var dispatches = 0;
    var selenaDispatches = 0;
    var selenaFallbacks = 0;
    for (var i = 0; i < events.length; i++) {
      var event = events[i];
      var id = sceneWaterObjectDisplacementEventID(event);
      if (id <= lastID) continue;
      var eventEntry = sceneWaterObjectDisplacementEventEntry(entry, event);
      var kind = sceneWaterObjectKind(eventEntry);
      if (!kind) {
        nextLastID = Math.max(nextLastID, id);
        continue;
      }
      device.queue.writeBuffer(system.uniformBuffer, 0, sceneWaterUniformData(system, eventEntry, 0, currentTime, { transientObject: true }, waterState, device));
      // Routed through dispatchWaterComputeStage (not dispatchWaterPass
      // directly) so a per-event displacement dispatch ALSO gets the
      // descriptor-driven Selena compute path, using system.
      // _waterComputeObjectState -- freshly stashed by the
      // sceneWaterUniformData call immediately above -- for this event's
      // own object state (see sceneWaterDisplacementSelenaRenderContext).
      var eventResult = dispatchWaterComputeStage(encoder, system, eventEntry, "displacement", pipeline, undefined, waterState, device);
      var eventDispatches = eventResult.dispatches;
      selenaDispatches += eventResult.selena;
      selenaFallbacks += eventResult.selenaFallback;
      if (eventDispatches > 0) {
        dispatches += eventDispatches;
        nextLastID = Math.max(nextLastID, id);
      }
    }
    if (nextLastID > lastID) system.lastObjectDisplacementEventID = nextLastID;
    return { dispatches: dispatches, selena: selenaDispatches, selenaFallback: selenaFallbacks, lastID: nextLastID };
  }

  // dispatchWaterDropEvents mirrors dispatchWaterObjectDisplacementEvents
  // immediately above, but for the queued-drop trail (see
  // sceneManagedFluidObjectQueueDrop in 19b-scene-control-forms.ts): a fast
  // pointer stroke can fire several drops between two rendered frames, and
  // upstream (evanw) injects one per DOM event, so a single-slot scalar
  // (entry.dropEventID/dropX/dropZ) would silently coalesce a burst into
  // one ripple. entry.dropEvents carries every drop queued since the last
  // consumed id; each gets its own per-event uniform write + "drop" compute
  // dispatch, same shape as the per-event displacement path. The legacy
  // scalar dispatch below (entry.dropEventID) stays intact as the fallback
  // for any caller that still only supplies the single-shot fields.

  function dispatchWaterDropEvents(system, entry, encoder, pipeline, currentTime, waterState: any, device: any) {
    if (!system || !encoder || !pipeline) {
      return { dispatches: 0, selena: 0, selenaFallback: 0, lastID: Math.max(0, Math.floor(sceneNumber(system && system.lastDropEventID, 0))) };
    }
    var events = sceneWaterDropEvents(entry);
    if (!events.length) {
      return { dispatches: 0, selena: 0, selenaFallback: 0, lastID: Math.max(0, Math.floor(sceneNumber(system.lastDropEventID, 0))) };
    }
    var lastID = Math.max(0, Math.floor(sceneNumber(system.lastDropEventID, 0)));
    var nextLastID = lastID;
    var dispatches = 0;
    var selenaDispatches = 0;
    var selenaFallbacks = 0;
    for (var i = 0; i < events.length; i++) {
      var event = events[i];
      var id = sceneWaterDropEventID(event);
      if (id <= lastID) continue;
      var eventEntry = sceneWaterDropEventEntry(entry, event);
      device.queue.writeBuffer(system.uniformBuffer, 0, sceneWaterUniformData(system, eventEntry, 0, currentTime, { transientObject: true }, waterState, device));
      var eventResult = dispatchWaterComputeStage(encoder, system, eventEntry, "drop", pipeline, undefined, waterState, device);
      var eventDispatches = eventResult.dispatches;
      selenaDispatches += eventResult.selena;
      selenaFallbacks += eventResult.selenaFallback;
      if (eventDispatches > 0) {
        dispatches += eventDispatches;
        nextLastID = Math.max(nextLastID, id);
      }
    }
    if (nextLastID > lastID) system.lastDropEventID = nextLastID;
    return { dispatches: dispatches, selena: selenaDispatches, selenaFallback: selenaFallbacks, lastID: nextLastID };
  }

  // M6 per-frame churn audit (water-parity-campaign), noted but NOT fixed
  // here: like the uniform "commit" write (see waterUniformSnapshotChanged),
  // this re-uploads objectSphereBuffer unconditionally on EVERY
  // sceneWaterUniformData call (both the per-frame {transientObject:true}
  // pack and the per-tick "commit" pack, i.e. up to twice a tick) even when
  // the compound-sphere layout hasn't moved. Deferring a dedup here rather
  // than bolting on a second ad hoc snapshot comparator: this buffer is
  // consumed as a STORAGE binding (sceneSelenaLiveBuffer's "objectSpheres"
  // case) by more than one pass with different freshness requirements (the
  // continuous per-frame object-displacement dispatch above legitimately
  // needs it live while dragging), and getting that gating wrong silently
  // corrupts a live displacement compute run rather than just staling a
  // cosmetic shimmer term the way the uniform-block skip's fallback-only
  // tradeoff does. Worth a follow-up with dedicated verification, not a
  // same-pass copy-paste of this file's other dedup.
  function sceneWaterWriteObjectSphereBuffer(system, spheres, waterState: any, device: any) {
    if (!system || !system.objectSphereBuffer) return;
    waterState.waterObjectSphereScratch.fill(0);
    var source = Array.isArray(spheres) ? spheres : [];
    for (var i = 0; i < source.length && i < WATER_MAX_DISPLACEMENT_SPHERES; i++) {
      var sphere = source[i] || {};
      var offset = i * 4;
      waterState.waterObjectSphereScratch[offset] = sceneNumber(sphere.x, 0);
      waterState.waterObjectSphereScratch[offset + 1] = sceneNumber(sphere.y, 0);
      waterState.waterObjectSphereScratch[offset + 2] = sceneNumber(sphere.z, 0);
      waterState.waterObjectSphereScratch[offset + 3] = Math.max(0.0001, sceneNumber(sphere.radius, 0));
    }
    device.queue.writeBuffer(system.objectSphereBuffer, 0, waterState.waterObjectSphereScratch);
  }

  // sceneWaterManifestShaderSources/sceneWaterManifestShaderSourceStats/
  // sceneWaterShaderSourcesFromEntries/sceneHydrateWaterEntriesFromSources
  // remain as a generic bundle/manifest water-source diagnostic layer (they
  // still report on any of the 14 legacy hand-written WGSL field names that
  // happen to show up on a bundle/manifest entry). They are no longer
  // consulted to build/select a render or compute pipeline -- that "authored
  // data-prop WGSL" resolution tier (sceneWaterAuthoredShaderSource and the
  // per-pass sceneWaterAuthored*Pipeline builders) has been removed now that
  // every WebGPU water pass resolves Selena-primary -> builtin-fallback (see
  // getWaterPoolPipeline / getWaterRenderPipeline / renderWaterCausticsPass /
  // renderWaterObjectShadowPass / renderWaterObjectMeshShadowPass /
  // dispatchWaterComputeStage below).
  function sceneWaterManifestShaderSources(waterState: any, canvas: any) {
    if (waterState.waterManifestShaderSourcesByID && waterState.waterManifestShaderSourcesByID.size > 0) return waterState.waterManifestShaderSourcesByID;
    var mountSources = canvas && (canvas.__gosxScene3DWaterShaderSources || (canvas.parentNode && canvas.parentNode.__gosxScene3DWaterShaderSources));
    var published = mountSources || (typeof window !== "undefined" ? window.__gosx_scene3d_water_shader_sources_by_id : null);
    var doc = canvas && canvas.ownerDocument
      ? canvas.ownerDocument
      : (typeof window !== "undefined" && window.document
        ? window.document
        : (typeof document !== "undefined" ? document : null));
    var scan = doc && doc.querySelectorAll;
    var manifestScript = scan && doc.getElementById ? doc.getElementById("gosx-manifest") : null;
    // The bootstrap runtime publishes its manifest parse; reusing it skips a
    // re-parse, and on a page that opted into data-gosx-release the DOM text
    // is empty and the published parse is the only remaining copy.
    var manifestMemo = typeof window !== "undefined" ? window.__gosx_manifest : null;
    var scripts = scan ? (doc.scripts || doc.querySelectorAll("script")) : null;
    // The scan reads the manifest text (megabytes with a large crowd), so an empty result is kept too, until the
    // published map or its size, the manifest element or its parse, or the page's script count changes.
    var key = waterState.waterManifestScanKey, size = published && typeof published === "object" ? Object.keys(published).length : 0;
    var memoValue = manifestMemo && manifestMemo.value, scriptCount = scripts ? scripts.length : -1;
    if (waterState.waterManifestShaderSourcesByID && key[0] === published && key[1] === size && key[2] === manifestScript && key[3] === memoValue && key[4] === scriptCount) return waterState.waterManifestShaderSourcesByID;
    key[0] = published; key[1] = size; key[2] = manifestScript; key[3] = memoValue; key[4] = scriptCount;
    waterState.waterManifestShaderSourcesByID = new Map();
    if (published && typeof published === "object") {
      var ids = Object.keys(published);
      for (var pi = 0; pi < ids.length; pi += 1) {
        var publishedRecord = published[ids[pi]];
        if (publishedRecord && typeof publishedRecord === "object") {
          waterState.waterManifestShaderSourcesByID.set(ids[pi], publishedRecord);
        }
      }
      if (waterState.waterManifestShaderSourcesByID.size > 0) return waterState.waterManifestShaderSourcesByID;
    }
    if (!scan) return waterState.waterManifestShaderSourcesByID;
    var fields = [
      "seedWGSL", "dropWGSL", "displacementWGSL", "simulationWGSL", "normalWGSL", "causticsWGSL",
      "poolVertexWGSL", "poolFragmentWGSL", "surfaceVertexWGSL", "surfaceFragmentWGSL", "surfaceBelowFragmentWGSL",
      "objectShadowWGSL", "objectMeshShadowVertexWGSL", "objectMeshShadowFragmentWGSL",
    ];
    function ingestManifestValue(manifest) {
      try {
        var engines = manifest && Array.isArray(manifest.engines) ? manifest.engines : [];
        for (var ei = 0; ei < engines.length; ei += 1) {
          var scene = engines[ei] && engines[ei].props && engines[ei].props.scene;
          var systems = scene && Array.isArray(scene.waterSystems) ? scene.waterSystems : [];
          for (var wi = 0; wi < systems.length; wi += 1) {
            var water = systems[wi];
            if (!water || typeof water !== "object") continue;
            var id = typeof water.id === "string" && water.id ? water.id : ("scene-water-" + wi);
            var record = waterState.waterManifestShaderSourcesByID.get(id) || {};
            for (var fi = 0; fi < fields.length; fi += 1) {
              var name = fields[fi];
              if (typeof water[name] === "string" && water[name].trim()) record[name] = water[name];
            }
            waterState.waterManifestShaderSourcesByID.set(id, record);
          }
        }
      } catch (_err) {}
    }
    function ingestManifestText(text) {
      if (!text || text.indexOf("waterSystems") < 0 || text.indexOf("WGSL") < 0) return;
      try { ingestManifestValue(JSON.parse(text)); } catch (_err) {}
    }
    if (manifestScript && manifestMemo && manifestMemo.element === manifestScript && manifestMemo.value) {
      ingestManifestValue(manifestMemo.value);
      if (waterState.waterManifestShaderSourcesByID.size > 0) return waterState.waterManifestShaderSourcesByID;
    }
    ingestManifestText(manifestScript && manifestScript.textContent || "");
    if (waterState.waterManifestShaderSourcesByID.size > 0) return waterState.waterManifestShaderSourcesByID;
    for (var si = 0; si < scripts.length; si += 1) {
      ingestManifestText(scripts[si] && scripts[si].textContent || "");
    }
    return waterState.waterManifestShaderSourcesByID;
  }

  function sceneWaterManifestShaderSourceStats(waterState: any, canvas: any) {
    var sourceMap = sceneWaterManifestShaderSources(waterState, canvas);
    var stats = { systems: 0, fields: 0, causticSourceBytes: 0, surfaceSourceBytes: 0 };
    sourceMap.forEach(function(record) {
      stats.systems += 1;
      for (var name in record) {
        if (!Object.prototype.hasOwnProperty.call(record, name)) continue;
        if (typeof record[name] !== "string" || !record[name].trim()) continue;
        stats.fields += 1;
        if (name === "causticsWGSL") {
          stats.causticSourceBytes = Math.max(stats.causticSourceBytes, record[name].trim().length);
        }
      }
      stats.surfaceSourceBytes = Math.max(stats.surfaceSourceBytes, sceneWaterSurfaceSourceBytes(record));
    });
    return stats;
  }

  function sceneWaterUniformData(system, entry, deltaTime, timeSeconds, options, waterState: any, device: any) {
    var transientObject = !!(options && options.transientObject);
    var resolution = system && system.resolution ? system.resolution : sceneWaterResolution(entry && entry.resolution);
    var cellCount = resolution * resolution;
    var light = sceneWaterLightVector(entry, { x: 0.3, y: 0.9, z: 0.45 });
    var lightLen = Math.sqrt(light.x * light.x + light.y * light.y + light.z * light.z) || 1;
    var shallow = sceneColorRGBA(entry && entry.shallowColor, [0.48, 0.82, 0.92, 1]);
    var deep = sceneColorRGBA(entry && entry.deepColor, [0.03, 0.18, 0.34, 1]);
    var poolWidth = Math.max(0.01, sceneNumber(entry && entry.poolWidth, 1.0));
    var poolHeight = Math.max(0.01, sceneNumber(entry && entry.poolHeight, 1.0));
    var poolLength = Math.max(0.01, sceneNumber(entry && entry.poolLength, 1.0));
    var rounded = wgpuSceneWaterPoolShapeRounded(entry);
    var maxCornerRadius = Math.max(0, Math.min(poolWidth, poolLength) - 0.001);
    var cornerRadius = rounded ? Math.max(0, Math.min(maxCornerRadius, sceneNumber(entry && entry.cornerRadius, 0))) : 0;
    var objectState = sceneWaterObjectState(transientObject ? null : system, entry, timeSeconds, poolWidth, poolLength);
    var optics = sceneWaterOpticsFlags(entry, objectState);
    if (system) {
      system.waterResolution = resolution;
      system.waterPoolWidth = poolWidth;
      system.waterPoolHeight = poolHeight;
      system.waterPoolLength = poolLength;
      system.waterCornerRadius = cornerRadius;
      system.waterLightDir = { x: light.x / lightLen, y: light.y / lightLen, z: light.z / lightLen };
      // Selena general-pass render-context support (surface/surfaceBelow/
      // caustics context fields: opticsEnable): cache the SAME optics flags
      // object computed for the hand-written WaterUniforms pack, the same
      // way waterLightDir already is, so per-pass Selena render-context
      // builders read it instead of recomputing sceneWaterOpticsFlags.
      system.waterOpticsFlags = optics;
      // Selena feedback-compute render-context support (displacement
      // kernel's objectKind/displacementScale/objectCenter/objectPrevCenter/
      // objectRadius/objectHalfSize/sphereCount/spheres context fields):
      // stash the JUST-COMPUTED objectState verbatim rather than re-deriving
      // it from the waterObject* fields above, which are NOT updated during
      // a one-shot displacement event (sceneWaterObjectState skips all
      // system-field mutation when called with transientObject:true, i.e.
      // system param null). This stash is always fresh at the point a
      // displacement dispatch is issued because every call site invokes
      // sceneWaterUniformData immediately before it (see
      // dispatchWaterObjectDisplacementEvents and the per-frame continuous
      // displacement dispatch in updateWaterSystems).
      system._waterComputeObjectState = objectState;
    }
    waterState.waterUniformScratchU[0] = resolution;
    waterState.waterUniformScratchU[1] = cellCount;
    waterState.waterUniformScratchU[2] = Math.max(0, Math.floor(sceneNumber(entry && entry.seedDrops, 7)));
    waterState.waterUniformScratchU[3] = Math.max(0, Math.floor(system && system.frameIndex || 0));
    waterState.waterUniformScratchF[4] = Math.max(0, Math.min(0.1, sceneNumber(deltaTime, 0)));
    waterState.waterUniformScratchF[5] = sceneNumber(timeSeconds, 0);
    waterState.waterUniformScratchF[6] = Math.max(0, Math.min(2, sceneNumber(entry && entry.waveSpeed, 1.0)));
    waterState.waterUniformScratchF[7] = Math.max(0, Math.min(1, sceneNumber(entry && entry.damping, 0.995)));
    waterState.waterUniformScratchF[8] = Math.max(0.0001, Math.min(0.5, sceneNumber(entry && entry.dropRadius, 0.03)));
    waterState.waterUniformScratchF[9] = Math.max(-1, Math.min(1, sceneNumber(entry && entry.dropStrength, 0.01)));
    waterState.waterUniformScratchF[10] = Math.max(0.01, Math.min(16, sceneNumber(entry && entry.normalScale, 1.0)));
    waterState.waterUniformScratchF[11] = poolWidth;
    waterState.waterUniformScratchF[12] = poolHeight;
    waterState.waterUniformScratchF[13] = poolLength;
    waterState.waterUniformScratchF[14] = cornerRadius;
    waterState.waterUniformScratchF[15] = rounded ? 1 : 0;
    waterState.waterUniformScratchF[16] = light.x / lightLen;
    waterState.waterUniformScratchF[17] = light.y / lightLen;
    waterState.waterUniformScratchF[18] = light.z / lightLen;
    waterState.waterUniformScratchF[19] = 1;
    waterState.waterUniformScratchF[20] = shallow[0];
    waterState.waterUniformScratchF[21] = shallow[1];
    waterState.waterUniformScratchF[22] = shallow[2];
    waterState.waterUniformScratchF[23] = shallow[3];
    waterState.waterUniformScratchF[24] = deep[0];
    waterState.waterUniformScratchF[25] = deep[1];
    waterState.waterUniformScratchF[26] = deep[2];
    waterState.waterUniformScratchF[27] = deep[3];
    waterState.waterUniformScratchF[28] = objectState.center.x;
    waterState.waterUniformScratchF[29] = objectState.center.y;
    waterState.waterUniformScratchF[30] = objectState.center.z;
    waterState.waterUniformScratchF[31] = 1;
    waterState.waterUniformScratchF[32] = objectState.previous.x;
    waterState.waterUniformScratchF[33] = objectState.previous.y;
    waterState.waterUniformScratchF[34] = objectState.previous.z;
    waterState.waterUniformScratchF[35] = 1;
    waterState.waterUniformScratchF[36] = objectState.halfSize.x;
    waterState.waterUniformScratchF[37] = objectState.halfSize.y;
    waterState.waterUniformScratchF[38] = objectState.halfSize.z;
    waterState.waterUniformScratchF[39] = objectState.radius;
    waterState.waterUniformScratchF[40] = objectState.kind;
    waterState.waterUniformScratchF[41] = objectState.displacementScale;
    waterState.waterUniformScratchF[42] = Math.min(WATER_MAX_DISPLACEMENT_SPHERES, objectState.spheres ? objectState.spheres.length : 0);
    waterState.waterUniformScratchF[43] = objectState.subtype || 0;
    waterState.waterUniformScratchF[44] = optics.caustics ? 1 : 0;
    waterState.waterUniformScratchF[45] = optics.reflection ? 1 : 0;
    waterState.waterUniformScratchF[46] = optics.refraction ? 1 : 0;
    waterState.waterUniformScratchF[47] = optics.object ? 1 : 0;
    waterState.waterUniformScratchF[48] = Math.max(-1, Math.min(1, sceneNumber(entry && entry.dropX, 0)));
    waterState.waterUniformScratchF[49] = Math.max(-1, Math.min(1, sceneNumber(entry && entry.dropZ, 0)));
    waterState.waterUniformScratchF[50] = Math.max(0.0001, Math.min(0.5, sceneNumber(entry && entry.dropEventRadius, sceneNumber(entry && entry.dropRadius, 0.03))));
    waterState.waterUniformScratchF[51] = Math.max(-1, Math.min(1, sceneNumber(entry && entry.dropEventStrength, sceneNumber(entry && entry.dropStrength, 0.01))));
    waterState.waterUniformScratchF[52] = Math.max(0, sceneNumber(system && system.seedSalt, 0));
    sceneWaterWriteObjectSphereBuffer(system, objectState.spheres, waterState, device);
    return waterState.waterUniformScratch;
  }

  // M6 per-frame churn audit (water-parity-campaign). The "commit" write in
  // updateWaterSystems (device.queue.writeBuffer(system.uniformBuffer, 0,
  // sceneWaterUniformData(...)), inside its `if (hasSimulationTick)` block)
  // re-packs and re-uploads this 256-byte WaterUniforms block every
  // simulation tick, even when the only thing that changed is the header:
  // word indices [0,6) are resolution/cellCount/seedDrops/frameIndex/
  // deltaTime/timeSeconds (WATER_UNIFORM_VOLATILE_WORDS), which are either
  // near-static or -- frameIndex/deltaTime/timeSeconds -- LITERALLY always
  // different every tick by construction. waterUniformSnapshotChanged
  // compares everything FROM word index 6 onward (waveSpeed, damping, drop
  // params, pool/light/shallow/deep-color/object/optics/drop-event fields,
  // seedSalt, plus any future field appended to the struct) against the
  // system's last-written snapshot using raw Uint32 bit comparison (NaN-safe,
  // no float-equality edge cases), and the caller skips the GPU writeBuffer
  // call entirely when nothing in that range moved -- which, per M5's
  // at-rest gating, is the common case once a system settles (config/object
  // state stays put for many consecutive ticks even though the clock keeps
  // advancing).
  //
  // Deliberately NOT applied to the OTHER writeBuffer call at this same call
  // site's sibling (the `{transientObject: true}` pack immediately above the
  // hasSimulationTick block): that write primes uniforms a one-shot drop/
  // displacement compute dispatch reads in the SAME frame it's issued, so
  // it's write-then-immediately-consumed, not a steady-state re-upload.
  //
  // Tradeoff, documented rather than silently accepted: system.uniformBuffer
  // is read ONLY by the hand-written fallback compute/render pipelines
  // (createWaterComputeBindGroup/createWaterRenderBindGroup/
  // createWaterCausticsBindGroup bind binding 0 to it) -- every Selena
  // render pass gets its own uniform buffer via sceneSelenaUniformData with
  // its own live `time` context field (selenaFrame.time), unaffected by
  // this skip. Selena is the sole primary path (the demo's own tests assert
  // zero authored/fallback occurrences in the golden path), so skipping this
  // upload is a no-op almost always; IF a backend ever falls all the way
  // through to the hand-written pipelines, a skipped tick leaves that
  // fallback's params.time-driven effects (e.g. the caustics fallback
  // fragment's shimmer term) one tick stale until the next real change --
  // a minor cosmetic staleness in an already-degraded emergency tier, not a
  // functional break.
  function waterUniformSnapshotChanged(system, waterState: any) {
    if (!system) return true;
    var last = system.waterUniformLastWords;
    if (!last || last.length !== waterState.waterUniformScratchU.length) {
      system.waterUniformLastWords = new Uint32Array(waterState.waterUniformScratchU.length);
      system.waterUniformLastWords.set(waterState.waterUniformScratchU);
      return true;
    }
    var changed = false;
    for (var i = WATER_UNIFORM_VOLATILE_WORDS; i < waterState.waterUniformScratchU.length; i++) {
      if (last[i] !== waterState.waterUniformScratchU[i]) { changed = true; break; }
    }
    if (changed) last.set(waterState.waterUniformScratchU);
    return changed;
  }

  function createWaterComputeBindGroup(system, readBuffer, writeBuffer, waterState: any, device: any) {
    return device.createBindGroup({
      label: "gosx-water-compute-bg",
      layout: waterState.waterComputeBindGroupLayout,
      entries: [
        { binding: 0, resource: { buffer: system.uniformBuffer } },
        { binding: 1, resource: { buffer: readBuffer } },
        { binding: 2, resource: { buffer: writeBuffer } },
        { binding: 3, resource: { buffer: system.objectSphereBuffer } },
      ],
    });
  }

  function createWaterRenderBindGroup(system, buffer, waterState: any, device: any) {
    var entry = system && system.entry || {};
    var cubeRecord = entry.cubeMap ? wgpuLoadCubeTexture(device, entry.cubeMap, waterState.gpuRes.textureCache) : null;
    var cubeLoaded = Boolean(cubeRecord && cubeRecord.loaded && cubeRecord.view);
    var cubePending = Boolean(cubeRecord && cubeRecord.pending && !cubeRecord.loaded && !cubeRecord.failed);
    var cubeFailed = Boolean(cubeRecord && cubeRecord.failed);
    /* @ts-expect-error TS2554 -- this call omits trailing arguments the JS caller has always been able to omit */ var tileURL = typeof entry.tileTexture === "string" ? entry.tileTexture.trim() : "";
    var tileRecord = tileURL ? wgpuLoadTexture(device, tileURL, waterState.gpuRes.textureCache) : null;
    var tileLoaded = Boolean(tileRecord && tileRecord.loaded && tileRecord.view);
    var tilePending = Boolean(tileRecord && tileRecord.pending && !tileRecord.loaded && !tileRecord.failed);
    var tileFailed = Boolean(tileRecord && tileRecord.failed);
    if (system) {
      system.waterSkyCubeRequested = !!(entry && entry.cubeMap);
      system.waterSkyCubeLoaded = cubeLoaded;
      system.waterSkyCubePending = cubePending;
      system.waterSkyCubeFailed = cubeFailed;
      system.waterSurfaceTileRequested = !!tileURL;
      system.waterSurfaceTileLoaded = tileLoaded;
      system.waterSurfaceTilePending = tilePending;
      system.waterSurfaceTileFailed = tileFailed;
    }
    return device.createBindGroup({
      label: "gosx-water-render-bg",
      layout: waterState.waterRenderBindGroupLayout,
      entries: [
        { binding: 0, resource: { buffer: system.uniformBuffer } },
        { binding: 1, resource: { buffer: buffer } },
        { binding: 2, resource: waterState.gpuRes.linearSampler },
        { binding: 3, resource: system.causticsView || waterState.gpuRes.placeholderView },
        { binding: 4, resource: system.objectReflectionView || waterState.gpuRes.placeholderView },
        { binding: 5, resource: system.objectClippedReflectionView || waterState.gpuRes.placeholderView },
        { binding: 6, resource: system.objectRefractionView || waterState.gpuRes.placeholderView },
        { binding: 7, resource: cubeRecord && cubeRecord.view ? cubeRecord.view : waterState.gpuRes.placeholderCubeView },
        { binding: 8, resource: { buffer: system.objectTextureMatrixBuffer } },
        { binding: 9, resource: tileLoaded ? tileRecord.view : waterState.gpuRes.placeholderView },
        { binding: 10, resource: { buffer: system.objectSphereBuffer } },
      ],
    });
  }

  // Per-frame leak guard: the surface (cubemap path) and pool bind groups were
  // rebuilt EVERY frame, and WebGPU bind groups have no destroy() (GC-only), so
  // at 60fps they accumulated until the device OOM'd ("not enough memory left").
  // Their resources are stable per-system except the ping-pong buffer (2 variants)
  // and the async sky-cube / tile views — so cache 2 variants and rebuild only
  // when those async views change (a handful of times, not per frame).
  function getWaterRenderBindGroupCached(system, waterState: any, device: any) {
    var entry = system && system.entry || {};
    if (!entry.cubeMap) return system.renderBindGroups[system.activeIndex];
    var cubeRecord = wgpuLoadCubeTexture(device, entry.cubeMap, waterState.gpuRes.textureCache);
    /* @ts-expect-error TS2554 -- this call omits trailing arguments the JS caller has always been able to omit */ var tileURL = typeof entry.tileTexture === "string" ? entry.tileTexture.trim() : "";
    var tileRecord = tileURL ? wgpuLoadTexture(device, tileURL, waterState.gpuRes.textureCache) : null;
    var cubeView = (cubeRecord && cubeRecord.view) || null;
    var tileView = (tileRecord && tileRecord.view) || null;
    var cache = system._cubemapRenderBindGroups;
    if (!cache || cache.cubeView !== cubeView || cache.tileView !== tileView) {
      cache = system._cubemapRenderBindGroups = {
        cubeView: cubeView,
        tileView: tileView,
        groups: [
          createWaterRenderBindGroup(system, system.bufferA, waterState, device),
          createWaterRenderBindGroup(system, system.bufferB, waterState, device),
        ],
      };
    }
    return cache.groups[system.activeIndex];
  }

  function getWaterPoolBindGroupCached(system, waterState: any, device: any) {
    if (!system) return null;
    var entry = system.entry || {};
    /* @ts-expect-error TS2554 -- this call omits trailing arguments the JS caller has always been able to omit */ var tileURL = typeof entry.tileTexture === "string" ? entry.tileTexture.trim() : "";
    var tileRecord = tileURL ? wgpuLoadTexture(device, tileURL, waterState.gpuRes.textureCache) : null;
    var tileView = (tileRecord && tileRecord.view) || null;
    var cache = system._poolBindGroups;
    if (!cache || cache.tileView !== tileView) {
      cache = system._poolBindGroups = {
        tileView: tileView,
        groups: [
          createWaterPoolBindGroup(system, system.bufferA, waterState, device),
          createWaterPoolBindGroup(system, system.bufferB, waterState, device),
        ],
      };
    }
    return cache.groups[system.activeIndex];
  }

  function writeWaterObjectTextureMatrices(system, waterState: any, uniformState: any, device: any) {
    if (!system || !system.objectTextureMatrixBuffer) return;
    var viewMatrix = system.objectViewProjectionReady ? system.objectViewProjectionMatrix : null;
    waterState.waterObjectTextureMatrixScratch.set(viewMatrix || uniformState.scratchSelenaViewProjection, 0);
    var reflectionMatrix = system.objectReflectionViewProjectionReady ? system.objectReflectionViewProjectionMatrix : null;
    waterState.waterObjectTextureMatrixScratch.set(reflectionMatrix || uniformState.scratchSelenaViewProjection, 16);
    device.queue.writeBuffer(system.objectTextureMatrixBuffer, 0, waterState.waterObjectTextureMatrixScratch);
  }

  function createWaterPoolBindGroup(system, buffer, waterState: any, device: any) {
    if (!system) return null;
    var activeBuffer = buffer || (system.activeIndex === 0 ? system.bufferA : system.bufferB);
    var entry = system.entry || {};
    /* @ts-expect-error TS2554 -- this call omits trailing arguments the JS caller has always been able to omit */ var tileURL = typeof entry.tileTexture === "string" ? entry.tileTexture.trim() : "";
    var tileRecord = tileURL ? wgpuLoadTexture(device, tileURL, waterState.gpuRes.textureCache) : null;
    var tileLoaded = Boolean(tileRecord && tileRecord.loaded && tileRecord.view);
    var tilePending = Boolean(tileRecord && tileRecord.pending && !tileRecord.loaded && !tileRecord.failed);
    var tileFailed = Boolean(tileRecord && tileRecord.failed);
    system.waterPoolTileRequested = !!tileURL;
    system.waterPoolTileLoaded = tileLoaded;
    system.waterPoolTilePending = tilePending;
    system.waterPoolTileFailed = tileFailed;
    return device.createBindGroup({
      label: "gosx-water-pool-bg",
      layout: waterState.waterPoolBindGroupLayout,
      entries: [
        { binding: 0, resource: { buffer: system.uniformBuffer } },
        { binding: 1, resource: { buffer: activeBuffer } },
        { binding: 2, resource: waterState.waterTileSampler || waterState.gpuRes.linearSampler },
        { binding: 3, resource: system.causticsView || waterState.gpuRes.placeholderView },
        { binding: 4, resource: system.objectShadowView || waterState.gpuRes.placeholderView },
        { binding: 5, resource: tileLoaded ? tileRecord.view : waterState.gpuRes.placeholderView },
      ],
    });
  }

  function createWaterCausticsBindGroup(system, buffer, waterState: any, device: any) {
    return device.createBindGroup({
      label: "gosx-water-caustics-bg",
      layout: waterState.waterCausticsBindGroupLayout,
      entries: [
        { binding: 0, resource: { buffer: system.uniformBuffer } },
        { binding: 1, resource: { buffer: buffer } },
        { binding: 2, resource: { buffer: system.objectSphereBuffer } },
        { binding: 3, resource: waterState.gpuRes.linearSampler },
        { binding: 4, resource: system.objectShadowView || waterState.gpuRes.placeholderView },
      ],
    });
  }

  function createWaterObjectTextureBindGroup(system, waterState: any, device: any) {
    return device.createBindGroup({
      label: "gosx-water-object-textures-bg",
      layout: waterState.waterObjectTextureBindGroupLayout,
      entries: [
        { binding: 0, resource: { buffer: system.uniformBuffer } },
        { binding: 1, resource: { buffer: system.objectSphereBuffer } },
      ],
    });
  }

  function createWaterObjectMeshShadowBindGroup(system, waterState: any, device: any) {
    if (!system || !system.objectMeshShadowUniformBuffer || !waterState.waterObjectMeshShadowBindGroupLayout) return null;
    return device.createBindGroup({
      label: "gosx-water-object-mesh-shadow-bg",
      layout: waterState.waterObjectMeshShadowBindGroupLayout,
      entries: [
        { binding: 0, resource: { buffer: system.objectMeshShadowUniformBuffer } },
      ],
    });
  }

  function sceneWaterObjectMeshShadowUniformData(system, waterState: any) {
    var entry = system && system.entry || {};
    var light = sceneWaterLightVector(entry, { x: 0.3, y: 0.9, z: 0.45 });
    var lightLen = Math.sqrt(light.x * light.x + light.y * light.y + light.z * light.z) || 1;
    var poolWidth = Math.max(0.001, sceneNumber(entry && entry.poolWidth, 1.0));
    var poolLength = Math.max(0.001, sceneNumber(entry && entry.poolLength, 1.0));
    waterState.waterObjectMeshShadowUniformScratch[0] = light.x / lightLen;
    waterState.waterObjectMeshShadowUniformScratch[1] = light.y / lightLen;
    waterState.waterObjectMeshShadowUniformScratch[2] = light.z / lightLen;
    waterState.waterObjectMeshShadowUniformScratch[3] = 1;
    waterState.waterObjectMeshShadowUniformScratch[4] = Math.max(0.0001, poolWidth);
    waterState.waterObjectMeshShadowUniformScratch[5] = Math.max(0.0001, poolLength);
    waterState.waterObjectMeshShadowUniformScratch[6] = 0;
    waterState.waterObjectMeshShadowUniformScratch[7] = 0;
    return waterState.waterObjectMeshShadowUniformScratch;
  }

  function createSceneWaterSystem(scopedDevice, entry, width, height, waterState: any, meshState: any, device: any) {
    var resolution = sceneWaterResolution(entry && entry.resolution);
    var authoredSurfaceResolution = sceneWaterSurfaceResolution(entry, resolution);
    var causticsResolution = sceneWaterCausticsResolution(entry);
    var objectTextureSize = sceneWaterObjectTextureTargetSize(entry, width, height);
    var objectTextureWidth = objectTextureSize.width;
    var objectTextureHeight = objectTextureSize.height;
    var objectTextureResolution = objectTextureSize.resolution;
    var objectShadowResolution = sceneWaterObjectShadowResolution(entry);
    var cellCount = resolution * resolution;
    var stateBytes = cellCount * 16;
    var stateBufferUsage = GPUBufferUsage.STORAGE | GPUBufferUsage.COPY_DST | GPUBufferUsage.COPY_SRC;
    var bufferA = wgpuCreateTrackedBuffer(meshState, device, stateBufferUsage, stateBytes);
    var bufferB = wgpuCreateTrackedBuffer(meshState, device, stateBufferUsage, stateBytes);
    var stateTextureUsage = GPUTextureUsage.TEXTURE_BINDING | GPUTextureUsage.COPY_DST;
    var stateTextureA = scopedDevice.createTexture({
      label: "gosx-water-state-sampled-a",
      size: [resolution, resolution, 1],
      format: "rgba32float",
      usage: stateTextureUsage,
    });
    var stateTextureB = scopedDevice.createTexture({
      label: "gosx-water-state-sampled-b",
      size: [resolution, resolution, 1],
      format: "rgba32float",
      usage: stateTextureUsage,
    });
    var uniformBuffer = wgpuCreateTrackedBuffer(meshState, device, GPUBufferUsage.UNIFORM | GPUBufferUsage.COPY_DST, 256);
    var objectSphereBuffer = wgpuCreateTrackedBuffer(meshState, device, GPUBufferUsage.STORAGE | GPUBufferUsage.COPY_DST, WATER_MAX_DISPLACEMENT_SPHERES * 16);
    var objectTextureMatrixBuffer = wgpuCreateTrackedBuffer(meshState, device, GPUBufferUsage.UNIFORM | GPUBufferUsage.COPY_DST, 128);
    var objectMeshShadowUniformBuffer = wgpuCreateTrackedBuffer(meshState, device, GPUBufferUsage.UNIFORM | GPUBufferUsage.COPY_DST, 32);
    var causticsTexture = scopedDevice.createTexture({
      label: "gosx-water-caustics-target",
      size: [causticsResolution, causticsResolution, 1],
      format: WATER_CAUSTICS_TEXTURE_FORMAT,
      usage: GPUTextureUsage.RENDER_ATTACHMENT | GPUTextureUsage.TEXTURE_BINDING | GPUTextureUsage.COPY_DST,
    });
    var causticsView = causticsTexture.createView();
    var objectReflectionTexture = scopedDevice.createTexture({
      label: "gosx-water-object-reflection-target",
      size: [objectTextureWidth, objectTextureHeight, 1],
      format: WATER_OBJECT_TEXTURE_FORMAT,
      usage: GPUTextureUsage.RENDER_ATTACHMENT | GPUTextureUsage.TEXTURE_BINDING,
    });
    var objectClippedReflectionTexture = scopedDevice.createTexture({
      label: "gosx-water-object-clipped-reflection-target",
      size: [objectTextureWidth, objectTextureHeight, 1],
      format: WATER_OBJECT_TEXTURE_FORMAT,
      usage: GPUTextureUsage.RENDER_ATTACHMENT | GPUTextureUsage.TEXTURE_BINDING,
    });
    var objectRefractionTexture = scopedDevice.createTexture({
      label: "gosx-water-object-refraction-target",
      size: [objectTextureWidth, objectTextureHeight, 1],
      format: WATER_OBJECT_TEXTURE_FORMAT,
      usage: GPUTextureUsage.RENDER_ATTACHMENT | GPUTextureUsage.TEXTURE_BINDING,
    });
    var objectTextureDepthTexture = scopedDevice.createTexture({
      label: "gosx-water-object-texture-depth",
      size: [objectTextureWidth, objectTextureHeight, 1],
      format: "depth24plus",
      usage: GPUTextureUsage.RENDER_ATTACHMENT,
    });
    var objectShadowTexture = scopedDevice.createTexture({
      label: "gosx-water-object-shadow-target",
      size: [objectShadowResolution, objectShadowResolution, 1],
      format: WATER_OBJECT_TEXTURE_FORMAT,
      usage: GPUTextureUsage.RENDER_ATTACHMENT | GPUTextureUsage.TEXTURE_BINDING,
    });
    var system = {
      entry: entry,
      resolution: resolution,
      qualityTier: "full",
      qualityRevision: 0,
      qualityDPRCap: 1,
      qualityAllocationPending: false,
      qualityAllocationFailures: 0,
      qualityAllocationConsecutiveFailures: 0,
      qualityAllocationNextFrame: 0,
      qualityAllocationDesiredKey: "",
      authoredSurfaceResolution: authoredSurfaceResolution,
      surfaceResolution: authoredSurfaceResolution,
      expensivePassCadence: 1,
      causticsResolution: causticsResolution,
      objectTextureResolution: objectTextureResolution,
      objectTextureWidth: objectTextureWidth,
      objectTextureHeight: objectTextureHeight,
      objectTextureResolutionMode: objectTextureSize.mode,
      objectTexturePixelBudget: objectTextureSize.pixelBudget,
      objectShadowResolution: objectShadowResolution,
      cellCount: cellCount,
      vertexCount: Math.max(0, (authoredSurfaceResolution - 1) * (authoredSurfaceResolution - 1) * 6),
      bufferA: bufferA,
      bufferB: bufferB,
      stateTextureA: stateTextureA,
      stateTextureB: stateTextureB,
      stateTextureViewA: stateTextureA.createView(),
      stateTextureViewB: stateTextureB.createView(),
      stateTextureSyncSeq: 0,
      uniformBuffer: uniformBuffer,
      objectSphereBuffer: objectSphereBuffer,
      objectTextureMatrixBuffer: objectTextureMatrixBuffer,
      objectViewProjectionMatrix: new Float32Array(16),
      objectViewProjectionReady: false,
      objectReflectionViewProjectionMatrix: new Float32Array(16),
      objectReflectionViewProjectionReady: false,
      objectMeshShadowUniformBuffer: objectMeshShadowUniformBuffer,
      causticsTexture: causticsTexture,
      causticsView: causticsView,
      objectReflectionTexture: objectReflectionTexture,
      objectReflectionView: objectReflectionTexture.createView(),
      objectClippedReflectionTexture: objectClippedReflectionTexture,
      objectClippedReflectionView: objectClippedReflectionTexture.createView(),
      objectRefractionTexture: objectRefractionTexture,
      objectRefractionView: objectRefractionTexture.createView(),
      objectTextureDepthTexture: objectTextureDepthTexture,
      objectTextureDepthView: objectTextureDepthTexture.createView(),
      objectShadowTexture: objectShadowTexture,
      objectShadowView: objectShadowTexture.createView(),
      activeIndex: 0,
      frameIndex: 0,
      waterClock: {},
      waterNormalDispatchSeq: 0,
      waterExpensiveCadenceBucket: null,
      waterObjectTextureFrameSeq: 0,
      seeded: false,
      seedSalt: Number.isFinite(Number(entry && entry.seedSalt)) ? Number(entry.seedSalt) : Math.random() * 4096,
      lastDropEventID: 0,
      dropDispatchCount: 0,
      // M5 at-rest gating -- see WATER_REST_ENERGY_EPSILON's comment above.
      // A freshly created system starts AWAKE (energy 1.0, no quiet time
      // banked yet): seedDrops (default 20) fires on the first tick below,
      // so it should decay/settle like any other disturbance rather than
      // being born already "at rest".
      waterRestEnergy: 1.0,
      waterLastDisturbanceMS: 0,
      waterAtRest: false,
      /* @ts-expect-error TS2339 -- this object literal grows fields after construction; TypeScript does not apply evolving-object inference to .ts files (only to checkJs .js files) */ dispose: function() {
        /* @ts-expect-error TS2339 -- this object literal grows fields after construction; TypeScript does not apply evolving-object inference to .ts files (only to checkJs .js files) */ if (system._gosxDisposed) return;
        system._gosxDisposed = true;
        if (bufferA && typeof bufferA.destroy === "function") {
          meshState.pointsEntryGPUBuffers.delete(bufferA);
          bufferA.destroy();
        }
        if (bufferB && typeof bufferB.destroy === "function") {
          meshState.pointsEntryGPUBuffers.delete(bufferB);
          bufferB.destroy();
        }
        if (stateTextureA && typeof stateTextureA.destroy === "function") stateTextureA.destroy();
        if (stateTextureB && typeof stateTextureB.destroy === "function") stateTextureB.destroy();
        if (uniformBuffer && typeof uniformBuffer.destroy === "function") {
          meshState.pointsEntryGPUBuffers.delete(uniformBuffer);
          uniformBuffer.destroy();
        }
        if (objectSphereBuffer && typeof objectSphereBuffer.destroy === "function") {
          meshState.pointsEntryGPUBuffers.delete(objectSphereBuffer);
          objectSphereBuffer.destroy();
        }
        if (objectTextureMatrixBuffer && typeof objectTextureMatrixBuffer.destroy === "function") {
          meshState.pointsEntryGPUBuffers.delete(objectTextureMatrixBuffer);
          objectTextureMatrixBuffer.destroy();
        }
        if (objectMeshShadowUniformBuffer && typeof objectMeshShadowUniformBuffer.destroy === "function") {
          meshState.pointsEntryGPUBuffers.delete(objectMeshShadowUniformBuffer);
          objectMeshShadowUniformBuffer.destroy();
        }
        if (system.causticsTexture && typeof system.causticsTexture.destroy === "function") {
          system.causticsTexture.destroy();
        }
        if (system.objectReflectionTexture && typeof system.objectReflectionTexture.destroy === "function") {
          system.objectReflectionTexture.destroy();
        }
        if (system.objectClippedReflectionTexture && typeof system.objectClippedReflectionTexture.destroy === "function") {
          system.objectClippedReflectionTexture.destroy();
        }
        if (system.objectRefractionTexture && typeof system.objectRefractionTexture.destroy === "function") {
          system.objectRefractionTexture.destroy();
        }
        if (system.objectTextureDepthTexture && typeof system.objectTextureDepthTexture.destroy === "function") {
          system.objectTextureDepthTexture.destroy();
        }
        if (system.objectShadowTexture && typeof system.objectShadowTexture.destroy === "function") {
          system.objectShadowTexture.destroy();
        }
      },
    /* @ts-expect-error TS2339 -- this object literal grows fields after construction; TypeScript does not apply evolving-object inference to .ts files (only to checkJs .js files) */ };
    system.computeBindGroups = [
      createWaterComputeBindGroup(system, bufferA, bufferB, waterState, device),
      createWaterComputeBindGroup(system, bufferB, bufferA, waterState, device),
    /* @ts-expect-error TS2339 -- this object literal grows fields after construction; TypeScript does not apply evolving-object inference to .ts files (only to checkJs .js files) */ ];
    system.renderBindGroups = [
      createWaterRenderBindGroup(system, bufferA, waterState, device),
      createWaterRenderBindGroup(system, bufferB, waterState, device),
    /* @ts-expect-error TS2339 -- this object literal grows fields after construction; TypeScript does not apply evolving-object inference to .ts files (only to checkJs .js files) */ ];
    system.causticsBindGroups = [
      createWaterCausticsBindGroup(system, bufferA, waterState, device),
      createWaterCausticsBindGroup(system, bufferB, waterState, device),
    /* @ts-expect-error TS2339 -- this object literal grows fields after construction; TypeScript does not apply evolving-object inference to .ts files (only to checkJs .js files) */ ];
    /* @ts-expect-error TS2339 -- this object literal grows fields after construction; TypeScript does not apply evolving-object inference to .ts files (only to checkJs .js files) */ system.objectTextureBindGroup = createWaterObjectTextureBindGroup(system, waterState, device);
    /* @ts-expect-error TS2339 -- this object literal grows fields after construction; TypeScript does not apply evolving-object inference to .ts files (only to checkJs .js files) */ system.objectMeshShadowBindGroup = createWaterObjectMeshShadowBindGroup(system, waterState, device);
    system._qualityResourceKey = [causticsResolution, objectShadowResolution, objectTextureWidth, objectTextureHeight, objectTextureSize.pixelBudget].join("|");
    return system;
  }

  function retireWaterRenderTextures(textures, timingState: any, device: any) {
    var list = (textures || []).filter(Boolean);
    if (!list.length) return;
    function destroyAll() {
      for (var i = 0; i < list.length; i++) {
        if (list[i] && typeof list[i].destroy === "function") list[i].destroy();
      }
    }
    if (device && device.queue && typeof device.queue.onSubmittedWorkDone === "function") {
      device.queue.onSubmittedWorkDone().then(destroyAll).catch(destroyAll);
    } else {
      timingState.deferredWaterTextureRetirements.push({
        textures: list,
        retireAfterFrame: timingState.gpuTimingFrameSeq + 3,
      });
    }
  }

  function applySceneWaterQualityProfile(system, profile, revision, width, height, waterState: any, timingState: any, device: any) {
    if (!system) return;
    var source = profile && typeof profile === "object" ? profile : {};
    var tier = source.tier === "survival" || source.tier === "balanced" ? source.tier : "full";
    var authoredSurfaceResolution = Math.max(2, Math.floor(sceneNumber(system.authoredSurfaceResolution, system.resolution)));
    var surfaceResolution = Math.max(2, Math.min(authoredSurfaceResolution, Math.floor(sceneNumber(source.surfaceResolution, tier === "survival" ? 96 : tier === "balanced" ? 128 : authoredSurfaceResolution))));
    var authoredCausticsResolution = sceneWaterCausticsResolution(system.entry);
    var authoredObjectShadowResolution = sceneWaterObjectShadowResolution(system.entry);
    var causticsResolution = Math.max(64, Math.min(authoredCausticsResolution, Math.floor(sceneNumber(source.causticsResolution, authoredCausticsResolution))));
    var objectShadowResolution = Math.max(64, Math.min(authoredObjectShadowResolution, Math.floor(sceneNumber(source.objectShadowResolution, authoredObjectShadowResolution))));
    var baseObjectSize = sceneWaterObjectTextureTargetSize(system.entry, width, height);
    var objectMaxSide = Math.max(64, Math.floor(sceneNumber(source.objectTextureMaxSide, Math.max(baseObjectSize.width, baseObjectSize.height))));
    var objectScale = Math.min(1, objectMaxSide / Math.max(1, baseObjectSize.width, baseObjectSize.height));
    var objectTextureSize = sceneWaterObjectTextureClampToPixelBudget({
      mode: baseObjectSize.mode,
      width: Math.max(1, Math.floor(baseObjectSize.width * objectScale)),
      height: Math.max(1, Math.floor(baseObjectSize.height * objectScale)),
    }, Math.max(0, Math.floor(sceneNumber(source.objectTexturePixelBudget, baseObjectSize.pixelBudget))));
    var profileRevision = Math.max(0, Math.floor(sceneNumber(revision, 0)));
    var key = [causticsResolution, objectShadowResolution, objectTextureSize.width, objectTextureSize.height, objectTextureSize.pixelBudget].join("|");
    system.qualityTier = tier;
    system.qualityRevision = profileRevision;
    system.qualityDPRCap = Math.max(0.25, sceneNumber(source.dprCap, 1));
    system.surfaceResolution = surfaceResolution;
    system.vertexCount = Math.max(0, (surfaceResolution - 1) * (surfaceResolution - 1) * 6);
    system.expensivePassCadence = Math.max(1, Math.floor(sceneNumber(source.expensivePassCadence, tier === "survival" ? 3 : tier === "balanced" ? 2 : 1)));
    if (system._qualityResourceKey === key) {
      system.qualityAllocationPending = false;
      system.qualityAllocationConsecutiveFailures = 0;
      system.qualityAllocationNextFrame = 0;
      system.qualityAllocationDesiredKey = key;
      return;
    }
    if (system.qualityAllocationDesiredKey !== key) {
      system.qualityAllocationDesiredKey = key;
      system.qualityAllocationConsecutiveFailures = 0;
      system.qualityAllocationNextFrame = 0;
    }
    if (system.qualityAllocationPending && timingState.webGPUFrameSeq < system.qualityAllocationNextFrame) return;

    var oldState = {
      causticsTexture: system.causticsTexture, causticsView: system.causticsView,
      objectReflectionTexture: system.objectReflectionTexture, objectReflectionView: system.objectReflectionView,
      objectClippedReflectionTexture: system.objectClippedReflectionTexture, objectClippedReflectionView: system.objectClippedReflectionView,
      objectRefractionTexture: system.objectRefractionTexture, objectRefractionView: system.objectRefractionView,
      objectTextureDepthTexture: system.objectTextureDepthTexture, objectTextureDepthView: system.objectTextureDepthView,
      objectShadowTexture: system.objectShadowTexture, objectShadowView: system.objectShadowView,
      causticsResolution: system.causticsResolution, objectShadowResolution: system.objectShadowResolution,
      objectTextureWidth: system.objectTextureWidth, objectTextureHeight: system.objectTextureHeight,
      objectTextureResolution: system.objectTextureResolution, objectTexturePixelBudget: system.objectTexturePixelBudget,
      renderBindGroups: system.renderBindGroups, causticsBindGroups: system.causticsBindGroups,
    };
    var candidates = [];
    function createCandidateTexture(desc) {
      var texture = device.createTexture(desc);
      candidates.push(texture);
      return texture;
    }
    function objectColorTarget(label) {
      return createCandidateTexture({ label: label, size: [objectTextureSize.width, objectTextureSize.height, 1], format: WATER_OBJECT_TEXTURE_FORMAT, usage: GPUTextureUsage.RENDER_ATTACHMENT | GPUTextureUsage.TEXTURE_BINDING });
    }
    try {
      system.causticsTexture = createCandidateTexture({ label: "gosx-water-caustics-target", size: [causticsResolution, causticsResolution, 1], format: WATER_CAUSTICS_TEXTURE_FORMAT, usage: GPUTextureUsage.RENDER_ATTACHMENT | GPUTextureUsage.TEXTURE_BINDING | GPUTextureUsage.COPY_DST });
      system.causticsView = system.causticsTexture.createView();
      system.objectReflectionTexture = objectColorTarget("gosx-water-object-reflection-target");
      system.objectReflectionView = system.objectReflectionTexture.createView();
      system.objectClippedReflectionTexture = objectColorTarget("gosx-water-object-clipped-reflection-target");
      system.objectClippedReflectionView = system.objectClippedReflectionTexture.createView();
      system.objectRefractionTexture = objectColorTarget("gosx-water-object-refraction-target");
      system.objectRefractionView = system.objectRefractionTexture.createView();
      system.objectTextureDepthTexture = createCandidateTexture({ label: "gosx-water-object-texture-depth", size: [objectTextureSize.width, objectTextureSize.height, 1], format: "depth24plus", usage: GPUTextureUsage.RENDER_ATTACHMENT });
      system.objectTextureDepthView = system.objectTextureDepthTexture.createView();
      system.objectShadowTexture = createCandidateTexture({ label: "gosx-water-object-shadow-target", size: [objectShadowResolution, objectShadowResolution, 1], format: WATER_OBJECT_TEXTURE_FORMAT, usage: GPUTextureUsage.RENDER_ATTACHMENT | GPUTextureUsage.TEXTURE_BINDING });
      system.objectShadowView = system.objectShadowTexture.createView();
      system.causticsResolution = causticsResolution;
      system.objectShadowResolution = objectShadowResolution;
      system.objectTextureWidth = objectTextureSize.width;
      system.objectTextureHeight = objectTextureSize.height;
      system.objectTextureResolution = objectTextureSize.resolution;
      system.objectTexturePixelBudget = objectTextureSize.pixelBudget;
      system.renderBindGroups = [createWaterRenderBindGroup(system, system.bufferA, waterState, device), createWaterRenderBindGroup(system, system.bufferB, waterState, device)];
      system.causticsBindGroups = [createWaterCausticsBindGroup(system, system.bufferA, waterState, device), createWaterCausticsBindGroup(system, system.bufferB, waterState, device)];
    } catch (qualityResourceError) {
      Object.keys(oldState).forEach(function(name) { system[name] = oldState[name]; });
      candidates.forEach(function(texture) { if (texture && typeof texture.destroy === "function") texture.destroy(); });
      system.qualityAllocationFailures += 1;
      system.qualityAllocationConsecutiveFailures += 1;
      system.qualityAllocationPending = true;
      system.qualityAllocationNextFrame = timingState.webGPUFrameSeq + Math.min(60,
        Math.pow(2, Math.min(6, system.qualityAllocationConsecutiveFailures - 1)));
      return;
    }
    system._cubemapRenderBindGroups = null;
    system._poolBindGroups = null;
    system.waterObjectTextureSubject = null;
    system.waterObjectShadowSignature = null;
    system.waterExpensiveCadenceBucket = null;
    system._qualityResourceKey = key;
    system.qualityAllocationPending = false;
    system.qualityAllocationConsecutiveFailures = 0;
    system.qualityAllocationNextFrame = 0;
    system.qualityAllocationDesiredKey = key;
    retireWaterRenderTextures([
      oldState.causticsTexture, oldState.objectReflectionTexture, oldState.objectClippedReflectionTexture,
      oldState.objectRefractionTexture, oldState.objectTextureDepthTexture, oldState.objectShadowTexture,
    ], timingState, device);
  }

  function retireWaterSystem(system, waterState: any, timingState: any, device: any) {
    if (!system || typeof system.dispose !== "function" || system._gosxDisposed) return;
    system._gosxRetireSerial = ++waterState.waterSystemRetireSerial;
    if (device && device.queue && typeof device.queue.onSubmittedWorkDone === "function") {
      device.queue.onSubmittedWorkDone().then(function() {
        system.dispose();
      }).catch(function() {
        system.dispose();
      });
      return;
    }
    timingState.deferredWaterSystemRetirements.push({ system: system, retireAfterFrame: timingState.gpuTimingFrameSeq + 3 });
  }

  function disposeWaterSystems(waterState: any) {
    for (const record of waterState.waterSystems.values()) {
      if (record && record.system && typeof record.system.dispose === "function") {
        try { record.system.dispose(); } catch (_err) {}
      }
    }
    waterState.waterSystems.clear();
    waterState.lastWaterTimeSeconds = null;
  }

  function syncWaterSystems(entries, width, height, waterState: any, meshState: any, timingState: any, device: any) {
    var activeIds = new Set();
    var records = [];
    var sourceEntries = Array.isArray(entries) ? entries : [];
    for (var i = 0; i < sourceEntries.length; i++) {
      var entry = sourceEntries[i];
      if (!entry || typeof entry !== "object") continue;
      var id = typeof entry.id === "string" && entry.id ? entry.id : ("scene-water-" + i);
      var record = waterState.waterSystems.get(id);
      var signature = sceneWaterSystemSignature(entry, width, height);
      activeIds.add(id);
      if (!record || record.signature !== signature) {
        if (record && record.system && typeof record.system.dispose === "function") {
          retireWaterSystem(record.system, waterState, timingState, device);
        }
        record = {
          signature: signature,
          system: createSceneWaterSystem(device, entry, width, height, waterState, meshState, device),
        };
        if (record.system) record.system.id = id;
        waterState.waterSystems.set(id, record);
      } else if (record.system) {
        record.system.entry = entry;
        record.system.id = id;
      }
      if (record && record.system) {
        records.push(record);
      }
    }
    for (const [id, record] of waterState.waterSystems.entries()) {
      if (!activeIds.has(id)) {
        if (record && record.system && typeof record.system.dispose === "function") {
          retireWaterSystem(record.system, waterState, timingState, device);
        }
        waterState.waterSystems.delete(id);
      }
    }
    return records;
  }

  // sceneWaterKnotContextArray packs the 65-point trefoil torus-knot
  // polyline that surface.sel / surface-below.sel's SDF sphere-trace reads
  // via `context { knot : array<vec4,65> }`. The points depend on NOTHING in
  // the scene (pure function of the loop index — the shaders used to rebuild
  // them per FRAGMENT, ~260 transcendentals plus a 1040-byte dynamically
  // indexed private array that collapsed GPU occupancy), so they are computed
  // once here and cached module-level. Formula is byte-identical to the old
  // in-shader loop: theta = ki/64 * 2pi, rad = 0.17*(2+cos(3*theta))*0.5,
  // point = (rad*cos(2*theta), -0.17*sin(3*theta)*0.5, rad*sin(2*theta), 0).
  function sceneWaterKnotContextArray(waterState: any) {
    var out = waterState.sceneWaterKnotArrayCache;
    if (!out) {
      // .w of every element stays 0 (Float32Array zero-init), matching the
      // old vec4f(..., 0.0) fourth component.
      waterState.sceneWaterKnotArrayCache = out = new Float32Array(65 * 4);
      for (var ki = 0; ki <= 64; ki++) {
        var theta = ki / 64.0 * 6.283185307;
        var rad = 0.17 * (2.0 + Math.cos(3.0 * theta)) * 0.5;
        var offset = ki * 4;
        out[offset] = rad * Math.cos(2.0 * theta);
        out[offset + 1] = -0.17 * Math.sin(3.0 * theta) * 0.5;
        out[offset + 2] = rad * Math.sin(2.0 * theta);
      }
    }
    return out;
  }

  // getWaterSelenaMeshDraw resolves the {pipeline, bindGroup} pair for a
  // mesh/mesh+state-kind Selena water pass, or null if the pipeline/bind
  // group could not be built (caller falls back to the hand-written path;
  // this can happen the same way any authored-shader path can fail: WGSL
  // validation rejection, memoized as a failure by getSelenaPipeline, or a
  // live resource -- state/texture/grid -- not being ready yet).
  // pipelineOptions is forwarded to getSelenaPipeline as-is (cullMode/
  // targetFormat/sampleCount/depthStencil/labelSuffix); blendMode/depthWrite
  // default to "opaque"/true; pass them explicitly to override. Water
  // surfaces are alpha blended but deliberately retain depth writes so
  // pool/object intersections remain stable.
  function getWaterSelenaMeshDraw(material, renderContext, system, pipelineOptions, waterState: any, device: any) {
    if (!material) return null;
    var opts = pipelineOptions || {};
    var blendMode = typeof opts.blendMode === "string" && opts.blendMode ? opts.blendMode : "opaque";
    var depthWrite = Object.prototype.hasOwnProperty.call(opts, "depthWrite") ? opts.depthWrite : true;
    var selenaResource = getSelenaPipeline(waterState.pipeState, waterState.selenaState, device, material, blendMode, depthWrite, opts);
    if (!selenaResource) return null;
    var bindGroup = createSelenaBindGroup(waterState.meshState, waterState.selenaState, device, material, selenaResource, system, renderContext);
    if (!bindGroup) return null;
    // attrs is forwarded (not just pipeline/bindGroup) so a per-object draw
    // loop with its own vertex-buffer binding convention (e.g.
    // drawWaterObjectProjectedShadowObjectsSelena's reuse of
    // bindWaterObjectSelenaAttributes below) can bind attributes without a
    // second getSelenaPipeline call.
    return { pipeline: selenaResource.pipeline, bindGroup: bindGroup, attrs: selenaResource.attrs };
  }

  // getWaterSelenaPostDraw mirrors getWaterSelenaMeshDraw for a post-kind
  // Selena water pass (object-shadow/compound-shadow below).
  function getWaterSelenaPostDraw(material, renderContext, system, pipelineOptions, waterState: any, device: any) {
    if (!material) return null;
    var selenaResource = getSelenaPostPipeline(waterState.pipeState, waterState.selenaState, device, material, pipelineOptions);
    if (!selenaResource) return null;
    var bindGroup = createSelenaPostBindGroup(waterState.meshState, waterState.selenaState, device, waterState.gpuRes.dummyShadowView, material, selenaResource, system, renderContext);
    if (!bindGroup) return null;
    return { pipeline: selenaResource.pipeline, bindGroup: bindGroup };
  }

  // --- Pool pass (the original template) ----------------------------------

  // sceneWaterPoolSelenaMaterial builds (and memoizes on the system) the
  // pool pass's Selena material + its per-frame customUniforms (live
  // render-target resource refs + the tile texture URL).
  function sceneWaterPoolSelenaMaterial(system, entry, waterState: any, device: any) {
    var material = sceneWaterSelenaMaterial(system, entry, "poolSelenaWGSL", "pool", "_selenaPoolMaterial");
    if (!material) return null;

    // Live water render targets (caustics/shadow) are resolved through the
    // SAME gosx:water:<id>:<slot> resource-ref mechanism the water-object
    // custom materials already use; the state heightfield resolves through
    // the descriptor's `state` name ("height", matching pool.sel's
    // `state height`). The tile texture is a plain user-config URL (not a
    // live GPU resource), so it is supplied as a literal string, exactly
    // like createWaterPoolBindGroup's tileTexture handling below.
    var tileURL = entry && typeof entry.tileTexture === "string" ? entry.tileTexture.trim() : "";
    material.customUniforms = {
      tileTexture: tileURL,
      causticTexture: sceneWaterSelenaResourceRef(system, "caustics"),
      shadowTexture: sceneWaterSelenaResourceRef(system, "shadow"),
      height: sceneWaterSelenaResourceRef(system, "state"),
    };

    // Mirror createWaterPoolBindGroup's tile-texture bookkeeping so
    // diagnostics (waterPoolTileTexture* stats) stay accurate regardless of
    // which pool path rendered this frame. wgpuLoadTexture is memoized by
    /* @ts-expect-error TS2554 -- this call omits trailing arguments the JS caller has always been able to omit */ // URL in textureCache, so this is not a duplicate fetch.
    var tileRecord = tileURL ? wgpuLoadTexture(device, tileURL, waterState.gpuRes.textureCache) : null;
    system.waterPoolTileRequested = !!tileURL;
    system.waterPoolTileLoaded = Boolean(tileRecord && tileRecord.loaded && tileRecord.view);
    system.waterPoolTilePending = Boolean(tileRecord && tileRecord.pending && !tileRecord.loaded && !tileRecord.failed);
    system.waterPoolTileFailed = Boolean(tileRecord && tileRecord.failed);

    return material;
  }

  // getWaterPoolSelenaDraw resolves the {pipeline, bindGroup} pair for the
  // generic Selena pool path, or null if the pipeline/bind group could not
  // be built (caller falls back to the hand-written pool path).
  function getWaterPoolSelenaDraw(system, entry, waterState: any, device: any) {
    var material = sceneWaterPoolSelenaMaterial(system, entry, waterState, device);
    var renderContext = sceneWaterPoolSelenaRenderContext(system);
    return getWaterSelenaMeshDraw(material, renderContext, system, { cullMode: "back" }, waterState, device);
  }

  // --- Surface / surface-below passes -------------------------------------
  //
  // Drawn by drawWaterSurfaceSide below (WaterSystem-owned code, one call
  // per "above"/"below" side per system, mirroring the hand-written
  // getWaterRenderPipeline(system, side)/getWaterRenderBindGroupCached
  // pattern it sits alongside).

  // sceneWaterSurfaceSelenaMaterial builds the surface pass's Selena
  // material + customUniforms: the tile/caustic/refraction/reflection/
  // clipped-reflection live resource refs, the "sky" cube map (a plain URL,
  // like pool's tileTexture -- resolved via the cube-texture path
  // sceneSelenaBindGroupLayout/createSelenaBindGroup added for
  // dimension:"cube" textures), and the "height" state ref. Also mirrors
  // createWaterRenderBindGroup's tile/sky-cube loaded-state bookkeeping so
  // diagnostics (waterSurfaceTile*/waterSkyCube* stats) stay accurate
  // regardless of which surface path rendered this frame.
  function sceneWaterSurfaceSelenaMaterial(system, entry, wgslField, descKey, memoSlot, waterState: any, device: any) {
    var material = sceneWaterSelenaMaterial(system, entry, wgslField, descKey, memoSlot);
    if (!material) return null;
    var tileURL = entry && typeof entry.tileTexture === "string" ? entry.tileTexture.trim() : "";
    var cubeURL = entry && typeof entry.cubeMap === "string" ? entry.cubeMap.trim() : "";
    material.customUniforms = {
      tileTexture: tileURL,
      causticTexture: sceneWaterSelenaResourceRef(system, "caustics"),
      sky: cubeURL,
      objectRefractionTex: sceneWaterSelenaResourceRef(system, "refraction"),
      objectReflectionTex: sceneWaterSelenaResourceRef(system, "reflection"),
      objectClippedReflectionTex: sceneWaterSelenaResourceRef(system, "clippedReflection"),
      height: sceneWaterSelenaResourceRef(system, "state"),
    /* @ts-expect-error TS2554 -- this call omits trailing arguments the JS caller has always been able to omit */ };
    var tileRecord = tileURL ? wgpuLoadTexture(device, tileURL, waterState.gpuRes.textureCache) : null;
    var cubeRecord = cubeURL ? wgpuLoadCubeTexture(device, cubeURL, waterState.gpuRes.textureCache) : null;
    system.waterSurfaceTileRequested = !!tileURL;
    system.waterSurfaceTileLoaded = Boolean(tileRecord && tileRecord.loaded && tileRecord.view);
    system.waterSurfaceTilePending = Boolean(tileRecord && tileRecord.pending && !tileRecord.loaded && !tileRecord.failed);
    system.waterSurfaceTileFailed = Boolean(tileRecord && tileRecord.failed);
    system.waterSkyCubeRequested = !!cubeURL;
    system.waterSkyCubeLoaded = Boolean(cubeRecord && cubeRecord.loaded && cubeRecord.view);
    system.waterSkyCubePending = Boolean(cubeRecord && cubeRecord.pending && !cubeRecord.loaded && !cubeRecord.failed);
    system.waterSkyCubeFailed = Boolean(cubeRecord && cubeRecord.failed);
    return material;
  }

  // sceneWaterSurfaceSelenaRenderContext builds the per-frame renderContext
  // shared by the surface AND surface-below passes (surface-below is the
  // exact same field set minus refractionMatrix/reflectionMatrix/waterColor,
  // which the generic packer simply won't find in surface-below's
  // descriptor -- extra renderContext.uniforms entries a material's fields
  // don't reference are harmless, see sceneSelenaUniformData's
  // per-declared-field loop).
  //
  // waterColor is an AUTHOR `param` on WaterSurface (surface.sel), not a
  // `context` field, but the generic uniform packer doesn't distinguish
  // param/context -- it just needs a value from renderContext.uniforms OR
  // material.customUniforms OR a compiled descriptor default, and
  // WaterSurface's `param waterColor : vec3` has no literal default (so the
  // descriptor carries none either), which used to leave it packed as
  // (0,0,0) via sceneSelenaUniformValue's final "zero" fallback. surface.sel
  // multiplies the ENTIRE refracted branch (pool floor/walls/caustics/
  // submerged objects) by waterColor whenever the refraction ray points
  // down into the water -- the common case when looking down at the pool --
  // so a zeroed waterColor blacked out refraction/caustics/pool-interior
  // through the surface, matching the reported near-black regression. Mirror
  // WebGL2's createWaterRenderBindGroup-equivalent call site
  // (16-scene-webgl.js's `sceneWaterRenderHexColor(entry.shallowColor, ...)`)
  // using the shared sceneColorRGBA helper (11-scene-math.ts) both renderers
  // already use elsewhere (see sceneWaterUniformData's `shallow` derivation
  // above), so both backends tint the underwater refraction from the SAME
  // entry.shallowColor config.
  function sceneWaterSurfaceSelenaRenderContext(system, camera, uniformSlotName, waterState: any) {
    var entry = (system && system.entry) || {};
    var light = (system && system.waterLightDir) || { x: 0.3, y: 0.9, z: 0.45 };
    var half = (system && system.waterObjectHalfSize) || { x: 0, y: 0, z: 0 };
    var center = (system && system.waterObjectCenter) || { x: 0, y: 0, z: 0 };
    var optics = (system && system.waterOpticsFlags) || {};
    var camPos = sceneWaterCameraPosFromCam(camera);
    var refractionMatrix = (system && system.objectViewProjectionReady) ? system.objectViewProjectionMatrix : waterState.uniformState.scratchSelenaViewProjection;
    var reflectionMatrix = (system && system.objectReflectionViewProjectionReady) ? system.objectReflectionViewProjectionMatrix : waterState.uniformState.scratchSelenaViewProjection;
    var fallbackWaterColor = sceneColorRGBA(entry.shallowColor, [0.48, 0.82, 0.92, 1]);
    var hasHDRWaterColor = sceneNumber(entry.aboveWaterColorR, 0) !== 0 || sceneNumber(entry.aboveWaterColorG, 0) !== 0 || sceneNumber(entry.aboveWaterColorB, 0) !== 0;
    var waterColor = hasHDRWaterColor
      ? [sceneNumber(entry.aboveWaterColorR, 0.25), sceneNumber(entry.aboveWaterColorG, 1), sceneNumber(entry.aboveWaterColorB, 1.25)]
      : fallbackWaterColor;
    return {
      uniformSlotSuffix: uniformSlotName + "-" + String((system && system.id) || "water"),
      uniforms: {
        poolWidth: sceneNumber(system && system.waterPoolWidth, 1),
        poolLength: sceneNumber(system && system.waterPoolLength, 1),
        poolHeight: sceneNumber(system && system.waterPoolHeight, 1),
        cornerRadius: sceneNumber(system && system.waterCornerRadius, 0),
        poolShape: wgpuSceneWaterPoolShapeRounded(entry) ? 1 : 0,
        // normalScale has a compiled descriptor default (1.0, matching
        // surface.sel's own `param normalScale : float = 1.0`), so this
        // omission was never a zeroing bug like waterColor -- but it DID
        // silently ignore a live entry.normalScale override the way
        // WebGL2's sceneWaterUniformData packs it. Forward it explicitly
        // (mirrors the caustics render context's identical fix above).
        normalScale: sceneNumber(entry.normalScale, 1.0),
        objectRadius: sceneNumber(system && system.waterObjectRadius, 0.3),
        opticsCaustic: optics.caustics ? 1 : 0,
        gridResolution: sceneNumber(system && system.surfaceResolution, sceneNumber(system && system.waterResolution, 256)),
        objectKind: sceneNumber(system && system.waterObjectKind, 0),
        objectSubtype: sceneNumber(system && system.waterObjectSubtype, 0),
        objectCount: sceneNumber(system && system.waterObjectSphereCount, 0),
        meshTextureEnable: waterSystemHasObjectTextureSubject(system) && sceneWaterObjectTextureTargetSlots(system).every(slot => (system.waterObjectTextureReadySlots & (1 << slot)) !== 0) ? 1 : 0,
        opticsEnable: optics.object ? 1 : 0,
        waterColor: [waterColor[0], waterColor[1], waterColor[2]],
        lightDir: [sceneNumber(light.x, 0.3), sceneNumber(light.y, 0.9), sceneNumber(light.z, 0.45)],
        cameraPos: [camPos.x, camPos.y, camPos.z],
        objectCenter: [sceneNumber(center.x, 0), sceneNumber(center.y, 0), sceneNumber(center.z, 0)],
        objectHalf: [sceneNumber(half.x, 0), sceneNumber(half.y, 0), sceneNumber(half.z, 0)],
        refractionMatrix: refractionMatrix,
        reflectionMatrix: reflectionMatrix,
        spheres: sceneWaterSpheresContextArray(system),
        knot: sceneWaterKnotContextArray(waterState),
      },
      grid: sceneNumber(system && system.waterResolution, 256),
    };
  }

  // getWaterSurfaceSelenaDraw / getWaterSurfaceBelowSelenaDraw resolve the
  // {pipeline, bindGroup} pair for the "above"/"below" surface passes.
  // Alpha-blended, depth-writing, single-sided cull (opposite winding per
  // side) -- matching upstream WaterSurfacePass and the hand-written
  // descriptor (fragment blend:"alpha", depthWriteEnabled:true,
  // cullMode: side==="below"?"back":"front").
  function getWaterSurfaceSelenaDraw(system, entry, camera, waterState: any, device: any) {
    var material = sceneWaterSurfaceSelenaMaterial(system, entry, "surfaceSelenaWGSL", "surface", "_selenaSurfaceMaterial", waterState, device);
    var renderContext = sceneWaterSurfaceSelenaRenderContext(system, camera, "water-surface", waterState);
    return getWaterSelenaMeshDraw(material, renderContext, system, { blendMode: "alpha", depthWrite: true, cullMode: "front" }, waterState, device);
  }

  function getWaterSurfaceBelowSelenaDraw(system, entry, camera, waterState: any, device: any) {
    var material = sceneWaterSurfaceSelenaMaterial(system, entry, "surfaceBelowSelenaWGSL", "surfaceBelow", "_selenaSurfaceBelowMaterial", waterState, device);
    var renderContext = sceneWaterSurfaceSelenaRenderContext(system, camera, "water-surface-below", waterState);
    return getWaterSelenaMeshDraw(material, renderContext, system, { blendMode: "alpha", depthWrite: true, cullMode: "back" }, waterState, device);
  }

  // --- Caustics pass -------------------------------------------------------
  //
  // Drawn by renderWaterCausticsPass below into its own offscreen
  // WATER_CAUSTICS_TEXTURE_FORMAT target (no depth attachment, no MSAA) --
  // depthStencil:false + sampleCount:1 mirror the hand-written
  // waterCausticsPipeline's descriptor exactly.

  // normalScale: WaterCaustics (like WaterSurface/WaterSurfaceBelow above)
  // declares `param normalScale : float = 1.0` -- a compiled descriptor
  // default exists, so omitting it here isn't a zeroing bug like waterColor
  // was (no live default -> sceneSelenaUniformValue's default-lookup step
  // finds the compiled 1.0 and uses it), but it DOES silently ignore a
  // live entry.normalScale override (WebGL2's sceneWaterUniformData packs
  // the live value into the hand-written WaterUniforms buffer at the same
  // index the caustics/surface passes read). Forward it explicitly so a
  // user-configured WaterSystem normalScale prop actually reaches the
  // Selena caustics pass instead of silently pinning to the shader's own
  // default.
  function sceneWaterCausticsSelenaRenderContext(system, waterState: any) {
    var entry = (system && system.entry) || {};
    var light = (system && system.waterLightDir) || { x: 0.3, y: 0.9, z: 0.45 };
    var center = (system && system.waterObjectCenter) || { x: 0, y: 0, z: 0 };
    var half = (system && system.waterObjectHalfSize) || { x: 0, y: 0, z: 0 };
    var optics = (system && system.waterOpticsFlags) || {};
    return {
      uniformSlotSuffix: "water-caustics-" + String((system && system.id) || "water"),
      uniforms: {
        poolWidth: sceneNumber(system && system.waterPoolWidth, 1),
        poolLength: sceneNumber(system && system.waterPoolLength, 1),
        poolHeight: sceneNumber(system && system.waterPoolHeight, 1),
        normalScale: sceneNumber(entry.normalScale, 1.0),
        opticsEnable: optics.caustics ? 1 : 0,
        gridResolution: sceneNumber(system && system.surfaceResolution, 201),
        resolution: sceneNumber(system && system.waterResolution, 256),
        time: waterState.selenaState.frame.time,
        objectKind: sceneNumber(system && system.waterObjectKind, 0),
        objectCount: sceneNumber(system && system.waterObjectSphereCount, 0),
        lightDir: [sceneNumber(light.x, 0.3), sceneNumber(light.y, 0.9), sceneNumber(light.z, 0.45)],
        objectCenter: [sceneNumber(center.x, 0), sceneNumber(center.y, 0), sceneNumber(center.z, 0)],
        objectHalfRadius: [sceneNumber(half.x, 0), sceneNumber(half.y, 0), sceneNumber(half.z, 0), sceneNumber(system && system.waterObjectRadius, 0)],
        spheres: sceneWaterSpheresContextArray(system),
        // Texel spacing for caustics.sel's objectShadowTexture 9-tap soft
        // sample (M2), forwarded live so a resized shadow RTT
        // (system.objectShadowResolution, WaterSystem's objectShadowResolution
        // prop) keeps the soft-shadow footprint texel-correct instead of
        // silently pinning to the shader's own compiled default.
        objectShadowTexelSize: 1.0 / Math.max(1, sceneNumber(system && system.objectShadowResolution, WATER_OBJECT_SHADOW_TEXTURE_SIZE)),
      },
      grid: sceneNumber(system && system.waterResolution, 256),
    };
  }

  // getWaterCausticsSelenaDraw projects the authored water grid into the
  // caustics target. See drawCount usage in renderWaterCausticsPass.
  function getWaterCausticsSelenaDraw(system, entry, waterState: any, device: any) {
    var material = sceneWaterCausticsSelenaMaterial(system, entry);
    var renderContext = sceneWaterCausticsSelenaRenderContext(system, waterState);
    return getWaterSelenaMeshDraw(material, renderContext, system, {
      targetFormat: WATER_CAUSTICS_TEXTURE_FORMAT,
      sampleCount: 1,
      depthStencil: false,
      labelSuffix: "water-caustics",
    }, waterState, device);
  }

  // --- Object shadow / compound shadow passes (post kind) -----------------
  //
  // Drawn by renderWaterObjectShadowPass below into system.objectShadowView
  // (WATER_OBJECT_TEXTURE_FORMAT, no depth attachment) -- getSelenaPostPipeline
  // never adds a depthStencil state, matching the hand-written
  // waterObjectShadowPipeline exactly. The raw hand-written contract
  // branches on objectParams.x (kind) INSIDE one shader; Selena splits this
  // into two materials (WaterObjectShadow for kind<2.5 -- sphere/cube --
  // and WaterCompoundShadow for kind>=2.5 -- the up-to-32-sphere compound
  // proxy), so the HOST selects the material by kind instead.

  function sceneWaterCompoundShadowSelenaRenderContext(system) {
    /* @ts-expect-error TS2339 -- this object literal grows fields after construction; TypeScript does not apply evolving-object inference to .ts files (only to checkJs .js files) */ var base = sceneWaterObjectShadowSelenaContextBase(system);
    base.spheres = sceneWaterSpheresContextArray(system);
    return {
      uniformSlotSuffix: "water-compound-shadow-" + String((system && system.id) || "water"),
      uniforms: base,
    };
  }

  // getWaterObjectShadowSelenaDraw picks WaterObjectShadow or
  // WaterCompoundShadow by the system's active object kind (mirroring the
  // raw hand-written shader's `objectParams.x >= 2.5` branch) and draws it
  // through the generic Selena post path.
  function getWaterObjectShadowSelenaDraw(system, entry, waterState: any, device: any) {
    var kind = sceneNumber(system && system.waterObjectKind, 0);
    var pipelineOptions = { targetFormat: WATER_OBJECT_TEXTURE_FORMAT, labelSuffix: "water-object-shadow" };
    if (kind >= 2.5) {
      if (!sceneWaterCompoundShadowUsesSelena(entry)) return null;
      var compoundMaterial = sceneWaterCompoundShadowSelenaMaterial(system, entry);
      var compoundContext = sceneWaterCompoundShadowSelenaRenderContext(system);
      return getWaterSelenaPostDraw(compoundMaterial, compoundContext, system, pipelineOptions, waterState, device);
    }
    if (!sceneWaterObjectShadowUsesSelena(entry)) return null;
    var material = sceneWaterObjectShadowSelenaMaterial(system, entry);
    var renderContext = sceneWaterObjectShadowSelenaRenderContext(system);
    return getWaterSelenaPostDraw(material, renderContext, system, pipelineOptions, waterState, device);
  }

  // --- Object mesh-shadow pass (projected mesh, mesh kind) ----------------
  //
  // Drawn by renderWaterObjectMeshShadowPass/drawWaterObjectProjectedShadowObjects
  // below into system.objectShadowView (WATER_OBJECT_TEXTURE_FORMAT, no
  // depth attachment, cullMode:"none" -- matching the hand-written
  // waterObjectMeshShadowPipeline exactly). Unlike the other water-system
  // passes above, this material/render-context pair is resolved ONCE per
  // system per frame (not per projected object): mvp/normalMatrix are
  // declared but UNUSED by object-mesh-shadow.sel's body (confirmed by
  // TestWaterSelenaWGSLDescriptorMatchesBindings's WGSL parse), and
  // lightDir/poolHalfW/poolHalfL don't vary per object, so every object in
  // the projected-shadow list shares one bind group; only the position
  // vertex buffer changes per object (bindWaterObjectSelenaAttributes,
  // reused from the object-texture RTT draw path).

  function getWaterObjectMeshShadowSelenaDraw(system, entry, waterState: any, device: any) {
    var material = sceneWaterObjectMeshShadowSelenaMaterial(system, entry);
    var renderContext = sceneWaterObjectMeshShadowSelenaRenderContext(system);
    return getWaterSelenaMeshDraw(material, renderContext, system, {
      targetFormat: WATER_OBJECT_TEXTURE_FORMAT,
      sampleCount: 1,
      depthStencil: false,
      cullMode: "none",
      labelSuffix: "water-object-mesh-shadow",
    }, waterState, device);
  }

  // -----------------------------------------------------------------------
  // The five water simulation COMPUTE kernels (seed/drop/displacement/
  // simulation/normal), routed through the generic descriptor-driven Selena
  // feedback-compute path above (getSelenaComputePipeline /
  // createSelenaComputeBindGroup) instead of the hardcoded
  // waterComputeBindGroupLayout/waterSeedPipeline/.../SCENE_WATER_COMPUTE_SOURCE
  // pipelines below. Each kernel falls back to the builtin hardcoded
  // pipeline (passed in as fallbackPipeline; the hand-written
  // data-prop-authored compute pipeline tier has been retired) when its
  // Selena WGSL+descriptor aren't present on the entry, or the Selena
  // pipeline/bind group failed to
  // build -- mirroring the render passes' sceneWaterXxxUsesSelena/
  // getWaterXxxSelenaDraw fallback convention exactly (see
  // renderWaterCausticsPass above). Dispatch math (ceil(cellCount/64)) and
  // ping-pong (system.activeIndex toggling bufferA/bufferB) are UNCHANGED
  // from the hardcoded path -- only pipeline/bind-group sourcing moves to
  // the descriptor.
  // -----------------------------------------------------------------------

  // getWaterSelenaComputeDraw resolves the {pipeline, bindGroup} pair for a
  // feedback-kind Selena water compute kernel, or null if the pipeline/bind
  // group could not be built (caller falls back to the resolved authored-or-
  // hardcoded pipeline). Mirrors getWaterSelenaMeshDraw/getWaterSelenaPostDraw.
  function getWaterSelenaComputeDraw(material, renderContext, system, inBuf, outBuf, waterState: any, device: any) {
    if (!material) return null;
    var selenaResource = getSelenaComputePipeline(waterState.selenaState, device, material);
    if (!selenaResource) return null;
    var bindGroup = createSelenaComputeBindGroup(waterState.meshState, waterState.selenaState, device, system, selenaResource, inBuf, outBuf, renderContext);
    if (!bindGroup) return null;
    return { pipeline: selenaResource.pipeline, bindGroup: bindGroup };
  }

  function prepareWaterComputePipelines(system: any, entry: any, waterState: any, device: any) {
    var resources = [];
    for (var stage of ["seed", "drop", "displacement", "simulation", "normal"]) {
      var fields = sceneWaterComputeStageFields(stage);
      var material = sceneWaterSelenaMaterial(system, entry, fields.wgslField, fields.descKey, fields.memoSlot);
      var resource = material && getSelenaComputePipeline(waterState.selenaState, device, material);
      if (resource) resources.push(resource.pipeline);
    }
    for (var pipeline of resources) wgpuRequirePipeline(pipeline);
  }

  function getWaterComputeStageSelenaDraw(system, entry, stage, readBuffer, writeBuffer, waterState: any, device: any) {
    var fields = sceneWaterComputeStageFields(stage);
    if (!fields) return null;
    var material = sceneWaterSelenaMaterial(system, entry, fields.wgslField, fields.descKey, fields.memoSlot);
    if (!material) return null;
    var renderContext = sceneWaterComputeStageRenderContext(system, entry, stage);
    return getWaterSelenaComputeDraw(material, renderContext, system, readBuffer, writeBuffer, waterState, device);
  }

  // dispatchWaterComputeStage is the single per-stage entry point every
  // seed/drop/displacement/simulation/normal call site below uses: tries the
  // descriptor-driven Selena compute path first, falling back to
  // fallbackPipeline (the builtin hardcoded compute pipeline; the
  // hand-written data-prop-authored compute pipeline tier has been
  // retired) when Selena's WGSL+descriptor aren't present on the entry, or
  // the Selena pipeline/bind group failed to
  // build. Returns {dispatches, selena, selenaFallback} so call sites can
  // fold all three into the existing waterComputeDispatches/
  // waterAuthoredComputeDispatches stats plus the new
  // waterSelenaComputeDispatches/waterSelenaComputeFallbacks counters.
  // sharedPass (optional) mirrors dispatchWaterPass/dispatchWaterPassSelena's
  // parameter of the same name: when set, this stage's dispatch is recorded
  // into that already-open compute pass instead of opening/closing its own.
  // The P3 water-sim/normal fusion (see the runWaterSim block below) is the
  // sole caller that passes sharedPass, batching the tick's 2 simulation
  // substeps plus the trailing normal reconstruction into one pass.
  function dispatchWaterComputeStage(encoder, system, entry, stage, fallbackPipeline, sharedPass, waterState: any, device: any) {
    if (!encoder && !sharedPass) return { dispatches: 0, selena: 0, selenaFallback: 0 };
    if (!system) return { dispatches: 0, selena: 0, selenaFallback: 0 };
    if (sceneWaterComputeStageUsesSelena(entry, stage)) {
      var readBuffer = system.activeIndex === 0 ? system.bufferA : system.bufferB;
      var writeBuffer = system.activeIndex === 0 ? system.bufferB : system.bufferA;
      var draw = getWaterComputeStageSelenaDraw(system, entry, stage, readBuffer, writeBuffer, waterState, device);
      if (draw) {
        var n = dispatchWaterPassSelena(encoder, system, draw, sharedPass);
        return { dispatches: n, selena: n, selenaFallback: 0 };
      }
      return { dispatches: dispatchWaterPass(encoder, system, fallbackPipeline, sharedPass), selena: 0, selenaFallback: 1 };
    }
    return { dispatches: dispatchWaterPass(encoder, system, fallbackPipeline, sharedPass), selena: 0, selenaFallback: 0 };
  }

  // getWaterPoolPipeline: builtin-only now. The hand-written
  // data-prop-authored pool pipeline tier (entry.poolVertexWGSL/
  // poolFragmentWGSL) has been retired -- the pool pass resolves
  // Selena-primary (see sceneWaterPoolUsesSelena/getWaterPoolSelenaDraw in
  // drawWaterPoolEntries below) falling through to this builtin
  // waterPoolVertexModule/waterPoolFragmentModule
  // (SCENE_WATER_POOL_*_SOURCE) pipeline as the last-resort safety net.
  // system/forceBuiltin are accepted (unused) to keep existing call sites
  // stable.
  function getWaterPoolPipeline(system, forceBuiltin, waterState: any, device: any) {
    var sampleCount = Math.max(1, Math.floor(waterState.pipeState.activeSampleCount || 1));
    var cacheKey = ["pool", waterState.pipeState.targetFormat, sampleCount].join("\x00");
    var cached = waterState.waterPoolPipelineCache[cacheKey];
    if (cached) return cached;
    var record = {
      pipeline: wgpuCreateValidatedPipeline(device, "render", {
        label: "gosx-water-pool-pass",
        layout: waterState.waterPoolPipelineLayout,
        vertex: { module: waterState.waterPoolVertexModule, entryPoint: "vertexMain", buffers: [] },
        fragment: {
          module: waterState.waterPoolFragmentModule,
          entryPoint: "fragmentMain",
          targets: [{ format: waterState.pipeState.targetFormat }],
        },
        primitive: { topology: "triangle-list", cullMode: "back" },
        multisample: { count: sampleCount },
        depthStencil: {
          format: "depth24plus",
          depthWriteEnabled: true,
          depthCompare: "less-equal",
        },
      }),
      authored: false,
      authoredVertex: false,
      authoredFragment: false,
      failed: false,
    };
    waterState.waterPoolPipelineCache[cacheKey] = record;
    return record;
  }

  function drawWaterPoolEntries(renderPass, records, frameBindGroup, waterState: any, device: any) {
    var roundedPoolVertexCount = 44 * 9;
    var stats = {
      waterPoolPasses: 0,
      waterPoolDrawCalls: 0,
      waterPoolDrawVertices: 0,
      waterPoolTileTextureLoaded: 0,
      waterPoolTileTextureFallbacks: 0,
      waterPoolTileTexturePending: 0,
      waterPoolTileTextureFailed: 0,
      waterAuthoredPoolPasses: 0,
      waterAuthoredPoolVertexPasses: 0,
      waterAuthoredPoolFragmentPasses: 0,
      waterAuthoredPoolFallbacks: 0,
      // Proof-of-concept: pool pass routed through the generic
      // descriptor-driven Selena WebGPU render path instead of the
      // hand-written waterPool*WGSL pipeline. See sceneWaterPoolUsesSelena /
      // getWaterPoolSelenaDraw above. Every other pass is unaffected.
      waterSelenaPoolPasses: 0,
      waterSelenaPoolFallbacks: 0,
    };
    if (!renderPass || !Array.isArray(records) || records.length === 0 || !frameBindGroup) return stats;
    renderPass.setBindGroup(0, frameBindGroup);
    var frameGroupBound = true;
    var activePipeline = null;
    for (var i = 0; i < records.length; i++) {
      var system = records[i] && records[i].system;
      if (!system) continue;
      var entry = system.entry || {};
      if (entry.renderPool === false || entry.poolPass === false) continue;
      var rounded = wgpuSceneWaterPoolShapeRounded(entry) && sceneNumber(entry.cornerRadius, 0) > 0.0001;

      if (sceneWaterPoolUsesSelena(entry, rounded)) {
        var selenaDraw = getWaterPoolSelenaDraw(system, entry, waterState, device);
        if (selenaDraw) {
          if (selenaDraw.pipeline !== activePipeline) {
            renderPass.setPipeline(wgpuRequirePipeline(selenaDraw.pipeline));
            activePipeline = selenaDraw.pipeline;
          }
          // The generic Selena pipeline layout has exactly ONE bind group
          // (group 0 carries mvp/params/textures/state together -- see
          // sceneSelenaBindGroupLayout), unlike the hand-written pool
          // pipeline's two groups (0=frame, 1=material). Overwriting group
          // 0 here is why frameGroupBound is tracked and restored below
          // before any subsequent hand-written-path draw in this loop.
          renderPass.setBindGroup(0, selenaDraw.bindGroup);
          frameGroupBound = false;
          // pool.sel now ports the rounded-corner geometry too, exactly
          // like the hand-written path's `vertexCount` below: the box
          // shape only ever needs 30 vertices, but the rounded vertex()
          // branch indexes up to roundedPoolVertexCount (44*9 = 396), so a
          // rounded pass must draw that many for every index the shader
          // branches on to actually be issued.
          var selenaVertexCount = rounded ? roundedPoolVertexCount : 30;
          renderPass.draw(selenaVertexCount);
          stats.waterPoolPasses += 1;
          stats.waterPoolDrawCalls += 1;
          stats.waterPoolDrawVertices += selenaVertexCount;
          stats.waterSelenaPoolPasses += 1;
          if (system.waterPoolTileLoaded) {
            stats.waterPoolTileTextureLoaded += 1;
          } else if (system.waterPoolTileRequested) {
            stats.waterPoolTileTextureFallbacks += 1;
            if (system.waterPoolTilePending) stats.waterPoolTileTexturePending += 1;
            if (system.waterPoolTileFailed) stats.waterPoolTileTextureFailed += 1;
          }
          continue;
        }
        stats.waterSelenaPoolFallbacks += 1;
        // Fall through to the hand-written path below (e.g. Selena pipeline
        // validation failed, or a live resource wasn't ready yet).
      }

      if (!frameGroupBound) {
        renderPass.setBindGroup(0, frameBindGroup);
        frameGroupBound = true;
        activePipeline = null;
      }
      var pipelineRecord = getWaterPoolPipeline(system, undefined, waterState, device);
      if (!pipelineRecord || !pipelineRecord.pipeline) continue;
      if (pipelineRecord.pipeline !== activePipeline) {
        renderPass.setPipeline(wgpuRequirePipeline(pipelineRecord.pipeline));
        activePipeline = pipelineRecord.pipeline;
      }
      var vertexCount = rounded ? roundedPoolVertexCount : 30;
      var bindGroup = getWaterPoolBindGroupCached(system, waterState, device);
      if (!bindGroup) continue;
      renderPass.setBindGroup(1, bindGroup);
      renderPass.draw(vertexCount);
      stats.waterPoolPasses += 1;
      stats.waterPoolDrawCalls += 1;
      stats.waterPoolDrawVertices += vertexCount;
      if (pipelineRecord.authored) stats.waterAuthoredPoolPasses += 1;
      if (pipelineRecord.authoredVertex) stats.waterAuthoredPoolVertexPasses += 1;
      if (pipelineRecord.authoredFragment) stats.waterAuthoredPoolFragmentPasses += 1;
      if (pipelineRecord.failed) stats.waterAuthoredPoolFallbacks += 1;
      if (system.waterPoolTileLoaded) {
        stats.waterPoolTileTextureLoaded += 1;
      } else if (system.waterPoolTileRequested) {
        stats.waterPoolTileTextureFallbacks += 1;
        if (system.waterPoolTilePending) stats.waterPoolTileTexturePending += 1;
        if (system.waterPoolTileFailed) stats.waterPoolTileTextureFailed += 1;
      }
    }
    return stats;
  }

  // getWaterRenderPipeline: builtin-only now. The hand-written
  // data-prop-authored surface pipeline tier (entry.surfaceVertexWGSL/
  // surfaceFragmentWGSL/surfaceBelowFragmentWGSL) has been retired -- each
  // surface side resolves Selena-primary (see sceneWaterSurfaceUsesSelena/
  // sceneWaterSurfaceBelowUsesSelena/getWaterSurfaceSelenaDraw/
  // getWaterSurfaceBelowSelenaDraw in drawWaterSurfaceSide below) falling
  // through to this builtin waterRenderVertexModule/waterRenderFragmentModule/
  // waterRenderBelowFragmentModule (SCENE_WATER_RENDER_*_SOURCE) pipeline as
  // the last-resort safety net. system/forceBuiltin are accepted (unused) to
  // keep existing call sites stable.
  function getWaterRenderPipeline(system, surfaceSide, forceBuiltin, waterState: any, device: any) {
    var sampleCount = Math.max(1, Math.floor(waterState.pipeState.activeSampleCount || 1));
    var side = surfaceSide === "below" ? "below" : "above";
    var cacheKey = [side, "alpha", sampleCount, waterState.pipeState.targetFormat].join("\x00");
    var cached = waterState.waterRenderPipelineCache.get(cacheKey);
    if (cached && cached.pipeline) return cached;
    var vertexModule = waterState.waterRenderVertexModule;
    var fragmentModule = side === "below" ? waterState.waterRenderBelowFragmentModule : waterState.waterRenderFragmentModule;
    var descriptor = {
      label: side === "below" ? "gosx-water-render-below" : "gosx-water-render-above",
      layout: waterState.waterRenderPipelineLayout,
      vertex: { module: vertexModule, entryPoint: "vertexMain", buffers: [] },
      fragment: {
        module: fragmentModule,
        entryPoint: "fragmentMain",
        targets: [{ format: waterState.pipeState.targetFormat, blend: wgpuBlendState("alpha") }],
      },
      primitive: { topology: "triangle-list", cullMode: side === "below" ? "back" : "front" },
      multisample: { count: sampleCount },
      depthStencil: {
        format: "depth24plus",
        depthWriteEnabled: true,
        depthCompare: "less-equal",
      },
    };
    var pipeline = wgpuCreateValidatedPipeline(device, "render", descriptor);
    var record = { pipeline: pipeline, authored: false, authoredVertex: false, failed: false, pending: false };
    waterState.waterRenderPipelineCache.set(cacheKey, record);
    return record;
  }

  function drawWaterSurfaceSide(renderPass, records, frameBindGroup, side, stats, camera, waterState: any, device: any) {
    renderPass.setBindGroup(0, frameBindGroup);
    // frameGroupBound tracks whether group 0 currently holds frameBindGroup
    // or a Selena material bind group (the generic Selena pipeline layout
    // has exactly ONE bind group, unlike the hand-written surface pipeline's
    // two groups: 0=frame, 1=material -- see drawWaterPoolEntries' identical
    // pattern/comment).
    var frameGroupBound = true;
    var activePipeline = null;
    for (var i = 0; i < records.length; i++) {
      var system = records[i] && records[i].system;
      if (!system || system.vertexCount <= 0) continue;
      var entry = system.entry || {};

      var usesSelena = side === "below" ? sceneWaterSurfaceBelowUsesSelena(entry) : sceneWaterSurfaceUsesSelena(entry);
      if (usesSelena) {
        var selenaDraw = side === "below"
          ? getWaterSurfaceBelowSelenaDraw(system, entry, camera, waterState, device)
          : getWaterSurfaceSelenaDraw(system, entry, camera, waterState, device);
        if (selenaDraw) {
          writeWaterObjectTextureMatrices(system, waterState, waterState.uniformState, device);
          if (selenaDraw.pipeline !== activePipeline) {
            renderPass.setPipeline(wgpuRequirePipeline(selenaDraw.pipeline));
            activePipeline = selenaDraw.pipeline;
          }
          renderPass.setBindGroup(0, selenaDraw.bindGroup);
          frameGroupBound = false;
          renderPass.draw(system.vertexCount);
          stats.waterDrawCalls += 1;
          stats.waterDrawVertices += system.vertexCount;
          stats.waterSurfaceMeshResolution = system.surfaceMeshResolution;
          stats.waterSelenaSurfacePasses += 1;
          if (system.waterSkyCubeLoaded) {
            stats.waterSkyCubeTextureLoaded += 1;
          } else if (system.waterSkyCubeRequested) {
            stats.waterSkyCubeTextureFallbacks += 1;
            if (system.waterSkyCubePending) stats.waterSkyCubeTexturePending += 1;
            if (system.waterSkyCubeFailed) stats.waterSkyCubeTextureFailed += 1;
          }
          if (side === "below") {
            stats.waterSurfaceBelowDrawCalls += 1;
            stats.waterSurfaceBelowDrawVertices += system.vertexCount;
          } else {
            stats.waterSurfaceAboveDrawCalls += 1;
            stats.waterSurfaceAboveDrawVertices += system.vertexCount;
          }
          continue;
        }
        stats.waterSelenaSurfaceFallbacks += 1;
        // Fall through to the hand-written path below.
      }

      if (!frameGroupBound) {
        renderPass.setBindGroup(0, frameBindGroup);
        frameGroupBound = true;
        activePipeline = null;
      }
      var pipelineRecord = getWaterRenderPipeline(system, side, undefined, waterState, device);
      if (!pipelineRecord || !pipelineRecord.pipeline) {
        if (pipelineRecord && pipelineRecord.pending) {
          stats.waterAuthoredSurfacePendingDrawCalls += 1;
        }
        if (pipelineRecord && pipelineRecord.failed) {
          stats.waterAuthoredSurfaceFallbacks += 1;
          stats.waterAuthoredSurfaceFallbackReason = waterState.waterAuthoredSurfacePipelineLastError;
        }
        pipelineRecord = getWaterRenderPipeline(null, side, true, waterState, device);
      }
      if (!pipelineRecord || !pipelineRecord.pipeline) continue;
      writeWaterObjectTextureMatrices(system, waterState, waterState.uniformState, device);
      if (pipelineRecord.pipeline !== activePipeline) {
        renderPass.setPipeline(wgpuRequirePipeline(pipelineRecord.pipeline));
        activePipeline = pipelineRecord.pipeline;
      }
      if (pipelineRecord.authored) {
        stats.waterAuthoredSurfaceSystems += 1;
        stats.waterAuthoredSurfaceDrawCalls += 1;
      }
      if (pipelineRecord.authoredVertex) {
        stats.waterAuthoredSurfaceVertexDrawCalls += 1;
      }
      if (pipelineRecord.failed) {
        stats.waterAuthoredSurfaceFallbacks += 1;
        stats.waterAuthoredSurfaceFallbackReason = waterState.waterAuthoredSurfacePipelineLastError;
      }
      var bindGroup = getWaterRenderBindGroupCached(system, waterState, device);
      renderPass.setBindGroup(1, bindGroup);
      renderPass.draw(system.vertexCount);
      stats.waterDrawCalls += 1;
      stats.waterDrawVertices += system.vertexCount;
      if (system.waterSkyCubeLoaded) {
        stats.waterSkyCubeTextureLoaded += 1;
      } else if (system.waterSkyCubeRequested) {
        stats.waterSkyCubeTextureFallbacks += 1;
        if (system.waterSkyCubePending) stats.waterSkyCubeTexturePending += 1;
        if (system.waterSkyCubeFailed) stats.waterSkyCubeTextureFailed += 1;
      }
      if (side === "below") {
        stats.waterSurfaceBelowDrawCalls += 1;
        stats.waterSurfaceBelowDrawVertices += system.vertexCount;
      } else {
        stats.waterSurfaceAboveDrawCalls += 1;
        stats.waterSurfaceAboveDrawVertices += system.vertexCount;
      }
    }
  }

  function drawWaterSystemEntries(renderPass, records, frameBindGroup, camera, waterState: any, device: any) {
    var stats = {
      waterDrawCalls: 0,
      waterDrawEntries: 0,
      waterDrawVertices: 0,
      waterSurfaceMeshResolution: 0,
      waterSurfaceAboveDrawCalls: 0,
      waterSurfaceAboveDrawVertices: 0,
      waterSurfaceBelowDrawCalls: 0,
      waterSurfaceBelowDrawVertices: 0,
      waterAuthoredSurfaceSystems: 0,
      waterAuthoredSurfaceDrawCalls: 0,
      waterAuthoredSurfaceVertexDrawCalls: 0,
      waterAuthoredSurfacePendingDrawCalls: 0,
      waterAuthoredSurfaceFallbacks: 0,
      waterAuthoredSurfaceFallbackReason: "",
      waterSkyCubeTextureLoaded: 0,
      waterSkyCubeTextureFallbacks: 0,
      waterSkyCubeTexturePending: 0,
      waterSkyCubeTextureFailed: 0,
      // Surface pass routed through the generic descriptor-driven Selena
      // WebGPU render path. See sceneWaterSurfaceUsesSelena/
      // getWaterSurfaceSelenaDraw above.
      waterSelenaSurfacePasses: 0,
      waterSelenaSurfaceFallbacks: 0,
    };
    if (!Array.isArray(records) || records.length === 0) return stats;
    for (var i = 0; i < records.length; i++) {
      var system = records[i] && records[i].system;
      if (system && system.vertexCount > 0) stats.waterDrawEntries += 1;
    }
    // A heightfield has no overhangs: from a camera clearly above or below
    // its zero plane only one face can contribute. Previously both sides
    // submitted the full tessellated grid and relied on face culling after
    // vertex processing. That doubled the water vertex load precisely when
    // compound objects (for example a glTF duck) also needed optical work.
    // Keep both sides only in the narrow surface crossing band.
    var waterCamera = sceneRenderCamera(camera);
    var cameraY = sceneNumber(waterCamera && waterCamera.y, 0);
    if (cameraY >= -0.025) drawWaterSurfaceSide(renderPass, records, frameBindGroup, "above", stats, camera, waterState, device);
    if (cameraY <= 0.025) drawWaterSurfaceSide(renderPass, records, frameBindGroup, "below", stats, camera, waterState, device);
    return stats;
  }
  function renderWaterCausticsPass(encoder, system, waterState: any, device: any) {
    if (!encoder || !system || !system.causticsView) {
      return { passes: 0, authored: false, failed: false, sourceBytes: 0, selena: 0, selenaFallback: 0 };
    }
    var entry = system.entry || {};
    // Caustics pass routed through the generic descriptor-driven Selena
    // WebGPU render path. See sceneWaterCausticsUsesSelena/
    // getWaterCausticsSelenaDraw above. Falls through to the hand-written
    // waterCausticsPipeline path below when Selena isn't usable (e.g. WGSL
    // validation rejection, memoized as a failure by getSelenaPipeline).
    var selenaFallback = 0;
    if (sceneWaterCausticsUsesSelena(entry)) {
      var selenaDraw = getWaterCausticsSelenaDraw(system, entry, waterState, device);
      if (selenaDraw) {
        var selenaPass = encoder.beginRenderPass({
          label: "gosx-water-caustics-pass",
          colorAttachments: [{
            view: system.causticsView,
            loadOp: "clear",
            storeOp: "store",
            clearValue: { r: 0, g: 0, b: 0, a: 1 },
          }],
        });
        selenaPass.setPipeline(wgpuRequirePipeline(selenaDraw.pipeline));
        selenaPass.setBindGroup(0, selenaDraw.bindGroup);
        // Selena caustics projects the same authored water topology as the
        // visible surface, matching the reference's tessellated light grid.
        selenaPass.draw(system.vertexCount);
        selenaPass.end();
        return { passes: 1, authored: false, failed: false, sourceBytes: 0, selena: 1, selenaFallback: 0 };
      }
      selenaFallback = 1;
    }
    if (!waterState.waterCausticsPipeline) {
      return { passes: 0, authored: false, failed: false, sourceBytes: 0, selena: 0, selenaFallback: selenaFallback };
    }
    // Falls through here only when Selena isn't usable: builtin
    // waterCausticsPipeline (SCENE_WATER_CAUSTICS_FRAGMENT_SOURCE) is the
    // last-resort safety-net fallback now that the hand-written
    // data-prop-authored caustics pipeline tier has been retired.
    var pipeline = waterState.waterCausticsPipeline;
    var pass = encoder.beginRenderPass({
      label: "gosx-water-caustics-pass",
      colorAttachments: [{
        view: system.causticsView,
        loadOp: "clear",
        storeOp: "store",
        clearValue: { r: 0, g: 0, b: 0, a: 1 },
      }],
    });
    pass.setPipeline(wgpuRequirePipeline(pipeline));
    pass.setBindGroup(0, system.causticsBindGroups[system.activeIndex]);
    pass.draw(3);
    pass.end();
    return { passes: 1, authored: false, failed: false, sourceBytes: 0, selena: 0, selenaFallback: selenaFallback };
  }

  function bindWaterObjectMeshVertexBuffers(pass, obj, pbrBuffers, waterState: any, device: any) {
    if (!pass || !obj || !pbrBuffers) return false;
    var offset = obj.vertexOffset;
    var count = obj.vertexCount;
    var isSkinned = webGPUObjectIsSkinned(obj);
    var computedMorphRecord = !isSkinned ? webGPUObjectComputedMorphDrawRecord(obj) : null;
    if (isSkinned) {
      return webGPUBindElioSkinnedBuffers(waterState.meshState, device, pass, obj, count);
    }
    if (computedMorphRecord) {
      if (!webGPUBindComputedMorphBuffer(pass, 0, computedMorphRecord.positionBuffer, count, 3)) return false;
      if (!webGPUBindComputedMorphBuffer(pass, 1, computedMorphRecord.normalBuffer, count, 3)) return false;
      if (!webGPUBindSceneMeshVertexBuffer(pass, 2, pbrBuffers && pbrBuffers.uvs, offset, count)) return false;
      if (!webGPUBindComputedMorphBuffer(pass, 3, computedMorphRecord.tangentBuffer, count, 4)) return false;
      return true;
    }
    if (!webGPUBindSceneMeshVertexBuffer(pass, 0, pbrBuffers && pbrBuffers.positions, offset, count)) return false;
    if (!webGPUBindSceneMeshVertexBuffer(pass, 1, pbrBuffers && pbrBuffers.normals, offset, count)) return false;
    if (!webGPUBindSceneMeshVertexBuffer(pass, 2, pbrBuffers && pbrBuffers.uvs, offset, count)) return false;
    if (!webGPUBindSceneMeshVertexBuffer(pass, 3, pbrBuffers && pbrBuffers.tangents, offset, count)) return false;
    return true;
  }

  function bindWaterObjectSelenaAttribute(pass, attr, obj, pbrBuffers, waterState: any, device: any) {
    if (!pass || !attr || !obj || !pbrBuffers) return false;
    var count = obj.vertexCount;
    var offset = obj.vertexOffset;
    if (webGPUObjectIsSkinned(obj)) return webGPUBindElioSelenaAttribute(waterState.meshState, device, pass, obj, attr, obj.vertexCount);
    var computedRecord = webGPUObjectComputedMorphDrawRecord(obj);
    if (attr.source === "positions") {
      if (computedRecord && webGPUBindComputedMorphBuffer(pass, attr.slot, computedRecord.positionBuffer, count, 3)) return true;
      return webGPUBindSceneMeshVertexBuffer(pass, attr.slot, pbrBuffers && pbrBuffers.positions, offset, count);
    }
    if (attr.source === "normals") {
      if (computedRecord && webGPUBindComputedMorphBuffer(pass, attr.slot, computedRecord.normalBuffer, count, 3)) return true;
      return webGPUBindSceneMeshVertexBuffer(pass, attr.slot, pbrBuffers && pbrBuffers.normals, offset, count);
    }
    if (attr.source === "uvs") {
      return webGPUBindSceneMeshVertexBuffer(pass, attr.slot, pbrBuffers && pbrBuffers.uvs, offset, count);
    }
    if (attr.source === "tangents") {
      if (computedRecord && webGPUBindComputedMorphBuffer(pass, attr.slot, computedRecord.tangentBuffer, count, 4)) return true;
      return webGPUBindSceneMeshVertexBuffer(pass, attr.slot, pbrBuffers && pbrBuffers.tangents, offset, count);
    }
    if (attr.source === "custom") {
      return webGPUBindRetainedMeshAttribute(waterState.meshState, device, pass, attr.slot, obj, "custom:" + attr.name, attr.components);
    }
    return false;
  }

  function bindWaterObjectSelenaAttributes(pass, resource, obj, pbrBuffers, waterState: any, device: any) {
    var attrs = resource && Array.isArray(resource.attrs) ? resource.attrs : [];
    for (var ai = 0; ai < attrs.length; ai++) {
      if (!bindWaterObjectSelenaAttribute(pass, attrs[ai], obj, pbrBuffers, waterState, device)) return false;
    }
    return attrs.length > 0;
  }

  // Never retain a white upload placeholder in a cached projected view.
  function sceneWaterObjectSelenaTexturesPending(material, layout, waterState: any, device: any) {
    var textures = wgpuSceneSelenaTextureDescriptors(layout);
    for (var i = 0; i < textures.length; i++) {
      var texture = textures[i];
      if (sceneSelenaResourceRef(material, texture)) continue;
      var url = wgpuSceneSelenaTextureURL(material, texture, i);
      var record = url && (texture.dimension === "cube" ? wgpuLoadCubeTexture(device, url, waterState.gpuRes.textureCache) : wgpuLoadTexture(device, url, waterState.gpuRes.textureCache, null, undefined, undefined));
      if (record && record.pending && !record.loaded && !record.failed) return true;
    }
    return false;
  }

  function drawWaterObjectMeshObjects(pass, objectList, bundle, materials, frameBindGroup, pbrBuffers, texturePassMode, renderContext, waterState: any, device: any) {
    if (!pass || !Array.isArray(objectList) || objectList.length === 0 || !pbrBuffers) return { drawCalls: 0, selenaDrawCalls: 0 };
    var drawCalls = 0;
    var selenaDrawCalls = 0;
    var currentPipelineKey = "";
    var lastMaterialIndex = -1;
    var lastReceiveShadow = null;

    for (var i = 0; i < objectList.length; i++) {
      var obj = objectList[i];
      var matIndex = sceneNumber(obj && obj.materialIndex, 0);
      var mat = materials[matIndex] || null;
      var renderPassKind = scenePBRObjectRenderPass(obj, mat);
      var blendMode = renderPassKind === "additive" ? "additive" : "alpha";
      var depthWrite = renderPassKind !== "alpha" && renderPassKind !== "additive";
      var selenaResource = getSelenaPipeline(waterState.pipeState, waterState.selenaState, device, mat, blendMode, depthWrite, {
        targetFormat: WATER_OBJECT_TEXTURE_FORMAT,
        sampleCount: 1,
        labelSuffix: "water-object-texture",
      });
      if (selenaResource) {
        if (sceneWaterObjectSelenaTexturesPending(mat, selenaResource.layout, waterState, device)) return { drawCalls: 0, selenaDrawCalls: 0 };
        var selenaKey = "selena:" + texturePassMode + ":" + (mat && mat.key || matIndex) + ":" + blendMode + ":" + (depthWrite ? "1" : "0");
        if (currentPipelineKey !== selenaKey) {
          pass.setPipeline(wgpuRequirePipeline(selenaResource.pipeline));
          currentPipelineKey = selenaKey;
          lastMaterialIndex = -1;
          lastReceiveShadow = null;
        }
        var selenaBG = createSelenaBindGroup(waterState.meshState, waterState.selenaState, device, mat, selenaResource, obj, renderContext);
        if (selenaBG && bindWaterObjectSelenaAttributes(pass, selenaResource, obj, pbrBuffers, waterState, device)) {
          pass.setBindGroup(0, selenaBG);
          pass.draw(obj.vertexCount);
          drawCalls += 1;
          selenaDrawCalls += 1;
          continue;
        }
      }
      var pipelineKey = texturePassMode + ":" + blendMode + ":" + (depthWrite ? "1" : "0");
      if (currentPipelineKey !== pipelineKey) {
        var pipeline = getWaterObjectMeshPipeline(waterState.pipeState, device, texturePassMode, blendMode, depthWrite);
        if (!pipeline) continue;
        pass.setPipeline(wgpuRequirePipeline(pipeline));
        pass.setBindGroup(0, frameBindGroup);
        currentPipelineKey = pipelineKey;
        lastMaterialIndex = -1;
        lastReceiveShadow = null;
      }

      var receiveShadow = false;
      if (matIndex !== lastMaterialIndex || receiveShadow !== lastReceiveShadow) {
        pass.setBindGroup(1, createMaterialBindGroup(waterState.uniformState, waterState.gpuRes, waterState.meshState, device, mat, receiveShadow, mat || obj), [0]);
        lastMaterialIndex = matIndex;
        lastReceiveShadow = receiveShadow;
      }

      var count = obj.vertexCount;
      if (bindWaterObjectMeshVertexBuffers(pass, obj, pbrBuffers, waterState, device)) {
        pass.draw(count);
        drawCalls += 1;
      }
    }
    return { drawCalls: drawCalls, selenaDrawCalls: selenaDrawCalls };
  }

  function drawWaterObjectProjectedShadowObjects(pass, objectList, pbrBuffers, waterState: any, device: any) {
    if (!pass || !Array.isArray(objectList) || objectList.length === 0 || !pbrBuffers) return 0;
    var drawCalls = 0;
    for (var i = 0; i < objectList.length; i++) {
      var obj = objectList[i];
      if (!obj || obj.viewCulled) continue;
      var count = obj.vertexCount;
      if (!Number.isFinite(count) || count <= 0) continue;
      if (bindWaterObjectMeshVertexBuffers(pass, obj, pbrBuffers, waterState, device)) {
        pass.draw(count);
        drawCalls += 1;
      }
    }
    return drawCalls;
  }

  // drawWaterObjectProjectedShadowObjectsSelena mirrors
  // drawWaterObjectProjectedShadowObjects for the generic Selena
  // object-mesh-shadow path: the same object list/culling/vertex-count
  // checks, but binding vertex attributes via bindWaterObjectSelenaAttributes
  // (object-mesh-shadow.sel declares ONLY a "position" attribute, unlike the
  // hand-written pipeline's full 4-slot PBR vertex layout) against the ONE
  // {pipeline,bindGroup,attrs} resource resolved once per system per frame
  // by getWaterObjectMeshShadowSelenaDraw (see its comment: lightDir/
  // poolHalfW/poolHalfL don't vary per object, so every object shares one
  // bind group).
  function drawWaterObjectProjectedShadowObjectsSelena(pass, objectList, pbrBuffers, selenaDraw, waterState: any, device: any) {
    if (!pass || !Array.isArray(objectList) || objectList.length === 0 || !pbrBuffers || !selenaDraw) return 0;
    var drawCalls = 0;
    for (var i = 0; i < objectList.length; i++) {
      var obj = objectList[i];
      if (!obj || obj.viewCulled) continue;
      var count = obj.vertexCount;
      if (!Number.isFinite(count) || count <= 0) continue;
      if (bindWaterObjectSelenaAttributes(pass, selenaDraw, obj, pbrBuffers, waterState, device)) {
        pass.draw(count);
        drawCalls += 1;
      }
    }
    return drawCalls;
  }

  function renderWaterObjectMeshTargetPass(encoder, system, view, objectList, bundle, materials, frameBindGroup, pbrBuffers, texturePassMode, label, targetName, waterState: any, device: any) {
    if (!encoder || !system || !view || !system.objectTextureDepthView || !Array.isArray(objectList) || objectList.length === 0) return 0;
    var pass = encoder.beginRenderPass({
      label: label || "gosx-water-object-mesh-pass",
      colorAttachments: [{
        view: view,
        loadOp: "clear",
        storeOp: "store",
        clearValue: { r: 0, g: 0, b: 0, a: 0 },
      }],
      depthStencilAttachment: {
        view: system.objectTextureDepthView,
        depthLoadOp: "clear",
        depthClearValue: 1.0,
        depthStoreOp: "store",
      },
    });
    var renderContext = sceneWaterObjectTextureSelenaContext(system, texturePassMode, targetName || label);
    var drawResult = drawWaterObjectMeshObjects(pass, objectList, bundle, materials, frameBindGroup, pbrBuffers, texturePassMode, renderContext, waterState, device);
    var drawCalls = drawResult && drawResult.drawCalls || 0;
    pass.end();
    return {
      passes: drawCalls > 0 ? 1 : 0,
      drawCalls: drawCalls,
      selenaDrawCalls: drawResult && drawResult.selenaDrawCalls || 0,
    };
  }

  function renderWaterObjectTexturePass(encoder, system, waterState: any) {
    if (!encoder || !system || !waterState.waterObjectTexturePipeline || !system.objectTextureBindGroup) return 0;
    if (!system.objectReflectionView || !system.objectClippedReflectionView || !system.objectRefractionView) return 0;
    var hasSubject = waterSystemHasObjectTextureSubject(system);
    var pass = encoder.beginRenderPass({
      label: "gosx-water-object-texture-pass",
      colorAttachments: [
        {
          view: system.objectReflectionView,
          loadOp: "clear",
          storeOp: "store",
          clearValue: { r: 0, g: 0, b: 0, a: 0 },
        },
        {
          view: system.objectClippedReflectionView,
          loadOp: "clear",
          storeOp: "store",
          clearValue: { r: 0, g: 0, b: 0, a: 0 },
        },
        {
          view: system.objectRefractionView,
          loadOp: "clear",
          storeOp: "store",
          clearValue: { r: 0, g: 0, b: 0, a: 0 },
        },
      ],
    });
    if (hasSubject) {
      pass.setPipeline(wgpuRequirePipeline(waterState.waterObjectTexturePipeline));
      pass.setBindGroup(0, system.objectTextureBindGroup);
      pass.draw(3);
    }
    pass.end();
    return hasSubject ? 1 : 0;
  }

  function renderWaterObjectShadowPass(encoder, system, waterState: any, device: any) {
    if (!encoder || !system || !system.objectShadowView) {
      return { passes: 0, authored: false, failed: false, selena: 0, selenaFallback: 0 };
    }
    var entry = system.entry || {};
    var hasSubject = waterSystemHasObjectTextureSubject(system);
    var pass = encoder.beginRenderPass({
      label: "gosx-water-object-shadow-pass",
      colorAttachments: [{
        view: system.objectShadowView,
        loadOp: "clear",
        storeOp: "store",
        clearValue: { r: 0, g: 0, b: 0, a: 0 },
      }],
    });
    // Object-shadow/compound-shadow pass routed through the generic
    // descriptor-driven Selena WebGPU post-kind render path. See
    // getWaterObjectShadowSelenaDraw above (it selects WaterObjectShadow vs
    // WaterCompoundShadow by the system's active object kind, mirroring the
    // raw hand-written shader's own objectParams.x branch). Falls through to
    // the hand-written waterObjectShadowPipeline path below when Selena
    // isn't usable.
    var selena = 0;
    var selenaFallback = 0;
    var drewSelena = false;
    var pipelineRecord = null;
    if (hasSubject) {
      var selenaDraw = getWaterObjectShadowSelenaDraw(system, entry, waterState, device);
      if (selenaDraw) {
        pass.setPipeline(wgpuRequirePipeline(selenaDraw.pipeline));
        pass.setBindGroup(0, selenaDraw.bindGroup);
        pass.draw(3);
        selena = 1;
        drewSelena = true;
      } else {
        selenaFallback = 1;
      }
    }
    if (hasSubject && !drewSelena && waterState.waterObjectShadowPipeline && system.objectTextureBindGroup) {
      // Builtin waterObjectShadowPipeline (SCENE_WATER_OBJECT_SHADOW_FRAGMENT_SOURCE)
      // is the last-resort safety-net fallback now that the hand-written
      // data-prop-authored object-shadow pipeline tier has been retired.
      pass.setPipeline(wgpuRequirePipeline(waterState.waterObjectShadowPipeline));
      pass.setBindGroup(0, system.objectTextureBindGroup);
      pass.draw(3);
    }
    pass.end();
    return {
      passes: 1,
      authored: !!(pipelineRecord && pipelineRecord.authored && hasSubject && !drewSelena),
      failed: !!(pipelineRecord && pipelineRecord.failed),
      selena: selena,
      selenaFallback: selenaFallback,
    };
  }

  function renderWaterObjectMeshShadowPass(encoder, system, objectList, pbrBuffers, waterState: any, device: any) {
    if (!encoder || !system || !system.objectShadowView) {
      return { passes: 0, drawCalls: 0, authored: false, failed: false, selena: 0, selenaFallback: 0 };
    }
    if (!waterSystemHasObjectTextureSubject(system) || !Array.isArray(objectList) || objectList.length === 0 || !pbrBuffers) {
      return { passes: 0, drawCalls: 0, authored: false, failed: false, selena: 0, selenaFallback: 0 };
    }
    var entry = system.entry || {};
    var pass = encoder.beginRenderPass({
      label: "gosx-water-object-mesh-shadow-pass",
      colorAttachments: [{
        view: system.objectShadowView,
        loadOp: "clear",
        storeOp: "store",
        clearValue: { r: 0, g: 0, b: 0, a: 0 },
      }],
    });
    // Object-mesh-shadow pass routed through the generic descriptor-driven
    // Selena WebGPU render path. See getWaterObjectMeshShadowSelenaDraw /
    // drawWaterObjectProjectedShadowObjectsSelena above. Falls through to
    // the hand-written waterObjectMeshShadowPipeline path below when Selena
    // isn't usable.
    var selena = 0;
    var selenaFallback = 0;
    var drawCalls = 0;
    var pipelineRecord = null;
    if (sceneWaterObjectMeshShadowUsesSelena(entry)) {
      var selenaDraw = getWaterObjectMeshShadowSelenaDraw(system, entry, waterState, device);
      if (selenaDraw) {
        pass.setPipeline(wgpuRequirePipeline(selenaDraw.pipeline));
        pass.setBindGroup(0, selenaDraw.bindGroup);
        drawCalls = drawWaterObjectProjectedShadowObjectsSelena(pass, objectList, pbrBuffers, selenaDraw, waterState, device);
        selena = 1;
      } else {
        selenaFallback = 1;
      }
    }
    if (!selena && waterState.waterObjectMeshShadowPipeline && system.objectMeshShadowBindGroup && system.objectMeshShadowUniformBuffer) {
      // Builtin waterObjectMeshShadowPipeline (SCENE_WATER_OBJECT_MESH_SHADOW_*_SOURCE)
      // is the last-resort safety-net fallback now that the hand-written
      // data-prop-authored object-mesh-shadow pipeline tier has been retired.
      device.queue.writeBuffer(system.objectMeshShadowUniformBuffer, 0, sceneWaterObjectMeshShadowUniformData(system, waterState));
      pass.setPipeline(wgpuRequirePipeline(waterState.waterObjectMeshShadowPipeline));
      pass.setBindGroup(0, system.objectMeshShadowBindGroup);
      drawCalls = drawWaterObjectProjectedShadowObjects(pass, objectList, pbrBuffers, waterState, device);
    }
    pass.end();
    return {
      passes: drawCalls > 0 ? 1 : 0,
      drawCalls: drawCalls,
      authored: !!(pipelineRecord && pipelineRecord.authored && drawCalls > 0),
      failed: !!(pipelineRecord && pipelineRecord.failed),
      selena: selena,
      selenaFallback: selenaFallback,
    };
  }

  function renderWaterObjectSceneTexturePasses(records, encoder, bundle, materials, frameBindGroup, pbrBuffers, width, height, toneMap, waterState: any, device: any) {
    var stats = {
      waterObjectTexturePasses: 0,
      waterObjectTextureTargets: 0,
      waterObjectTexturePixels: 0,
      waterObjectTextureWidth: 0,
      waterObjectTextureHeight: 0,
      waterObjectTexturePixelBudget: 0,
      waterObjectTextureMeshPasses: 0,
      waterObjectTextureMeshDrawCalls: 0,
      waterObjectTextureSelenaDrawCalls: 0,
      waterObjectTextureFallbackPasses: 0,
      waterObjectTextureCandidateObjects: 0,
      waterObjectTextureSelectedObjects: 0,
      waterObjectTextureFallbackMissingObjects: 0,
      waterObjectTextureFallbackMissingResources: 0,
      waterObjectTextureCandidateProfile: "",
    };
    if (!encoder || !Array.isArray(records) || records.length === 0) return stats;
    var restoredFrame = false;
    for (var i = 0; i < records.length; i++) {
      var system = records[i] && records[i].system;
      if (!system) continue;
      var entry = system.entry || {};
      var textureSubject = waterSystemHasObjectTextureSubject(system) ? sceneWaterActiveObjectID(entry) || "mesh" : "";
      if (system.waterObjectTextureSubject !== textureSubject) {
        system.waterObjectTextureSubject = textureSubject;
        system.waterObjectTextureReadySlots = 0;
        system.waterObjectTextureSignature = null;
      }
      if (!textureSubject) continue;
      var optics = sceneWaterOpticsFlags(entry, {
        kind: sceneWaterObjectKind(entry),
        displacementScale: Math.max(0, sceneNumber(entry.objectDisplacementScale, 1)),
      });
      if (!optics.object && !optics.reflection && !optics.refraction) continue;
      var objectList = sceneWaterObjectMeshList(bundle, entry);
      stats.waterObjectTextureCandidateObjects += Array.isArray(bundle && bundle.meshObjects) ? bundle.meshObjects.length : 0;
      stats.waterObjectTextureSelectedObjects += objectList.length;
      if (!stats.waterObjectTextureCandidateProfile) {
        stats.waterObjectTextureCandidateProfile = sceneWaterObjectMeshCandidateProfile(bundle, entry, materials);
      }
      if (!objectList.length || !pbrBuffers || !frameBindGroup) {
        if (!objectList.length) stats.waterObjectTextureFallbackMissingObjects += 1;
        if (!pbrBuffers || !frameBindGroup) stats.waterObjectTextureFallbackMissingResources += 1;
        /* @ts-expect-error TS2554 -- this call omits trailing arguments the JS caller has always been able to omit */ var fallbackPasses = renderWaterObjectTexturePass(encoder, system, waterState);
        if (fallbackPasses > 0) addWaterObjectTextureStats(stats, system, fallbackPasses, fallbackPasses * 3, 0, fallbackPasses);
        continue;
      }

      var textureSignature = sceneWaterObjectRenderSignature(system, entry, bundle, objectList, true);
      var targetSlots = sceneWaterObjectTextureTargetSlots(system);
      if (system.waterObjectTextureSignature !== textureSignature) {
        system.waterObjectTextureSignature = textureSignature;
        system.waterObjectTextureRefreshRemaining = targetSlots.length;
      }
      var refreshRemaining = Math.max(0, Math.floor(sceneNumber(system.waterObjectTextureRefreshRemaining, 0)));
      if (refreshRemaining <= 0) continue;

      // The adaptive profile already cadences caustics/shadow work; apply
      // that same contract to projected mesh targets. A stressed GPU at
      // balanced/survival quality must not keep paying a viewport-sized duck
      // RTT on every display frame. This sequence is independent from the
      // changing render signature, otherwise motion would bypass cadence by
      // marking the target dirty every frame.
      var objectTextureFrameSeq = Math.max(0, Math.floor(sceneNumber(system.waterObjectTextureFrameSeq, 0)));
      system.waterObjectTextureFrameSeq = objectTextureFrameSeq + 1;
      var objectTextureCadence = Math.max(1, Math.floor(sceneNumber(system.expensivePassCadence, 1)));
      if (objectTextureFrameSeq % objectTextureCadence !== 0) continue;

      var targetWidth = Math.max(1, system.objectTextureWidth || system.objectTextureResolution || WATER_OBJECT_TEXTURE_SIZE);
      var targetHeight = Math.max(1, system.objectTextureHeight || system.objectTextureResolution || WATER_OBJECT_TEXTURE_SIZE);
      // Round-robin independently from refreshRemaining. Continuous object
      // or camera movement changes the signature every frame; deriving the
      // slot from refreshRemaining used to reset moving ducks to refraction
      // forever. The persistent cursor keeps all subtype-required targets
      // fresh while still rendering at most one RTT per frame.
      var refreshCursor = Math.max(0, Math.floor(sceneNumber(system.waterObjectTextureRefreshCursor, 0)));
      var passSlot = targetSlots[refreshCursor % targetSlots.length];
      var emptyPass = { passes: 0, drawCalls: 0, selenaDrawCalls: 0 };
      // A retained texture must stay paired with the projection matrix used
      // to render it. Update only the matrix for the target issued this
      // frame; skipped/alternating targets keep both their pixels and their
      // matching matrix instead of falling through to the current scratch
      // camera and visibly swimming across the water.
      var refraction = emptyPass;
      var reflection = emptyPass;
      var clipped = emptyPass;
      if (passSlot === 0) {
        uploadFrameUniforms(waterState.uniformState, waterState.gpuRes, device, bundle && bundle.camera, targetWidth, targetHeight, false);
        if (system.objectViewProjectionMatrix) {
          system.objectViewProjectionMatrix.set(waterState.uniformState.scratchSelenaViewProjection);
          system.objectViewProjectionReady = true;
        /* @ts-expect-error TS2322 -- renderWaterObjectMeshTargetPass can also return the empty-pass sentinel 0 */ }
        refraction = renderWaterObjectMeshTargetPass(
          encoder,
          system,
          system.objectRefractionView,
          objectList,
          bundle,
          materials,
          frameBindGroup,
          pbrBuffers,
          1,
          "gosx-water-object-mesh-refraction-pass",
          "refraction", waterState, device
        );
      } else {
        uploadWaterReflectionFrameUniforms(waterState.uniformState, waterState.gpuRes, device, bundle && bundle.camera, targetWidth, targetHeight, false);
        if (system.objectReflectionViewProjectionMatrix) {
          system.objectReflectionViewProjectionMatrix.set(waterState.uniformState.scratchSelenaViewProjection);
          system.objectReflectionViewProjectionReady = true;
        }
        /* @ts-expect-error TS2322 -- renderWaterObjectMeshTargetPass can also return the empty-pass sentinel 0 */ if (passSlot === 1) {
          reflection = renderWaterObjectMeshTargetPass(
            encoder,
            system,
            system.objectReflectionView,
            objectList,
            bundle,
            materials,
            frameBindGroup,
            pbrBuffers,
            1,
            "gosx-water-object-mesh-reflection-pass",
            "reflection", waterState, device
          );
        /* @ts-expect-error TS2322 -- renderWaterObjectMeshTargetPass can also return the empty-pass sentinel 0 */ } else {
          clipped = renderWaterObjectMeshTargetPass(
            encoder,
            system,
            system.objectClippedReflectionView,
            objectList,
            bundle,
            materials,
            frameBindGroup,
            pbrBuffers,
            2,
            "gosx-water-object-mesh-clipped-reflection-pass",
            "clipped-reflection", waterState, device
          );
        }
      }
      restoredFrame = true;
      var passCount = refraction.passes + reflection.passes + clipped.passes;
      if (passCount > 0) system.waterObjectTextureReadySlots |= 1 << passSlot;
      var drawCalls = refraction.drawCalls + reflection.drawCalls + clipped.drawCalls;
      var selenaDrawCalls = refraction.selenaDrawCalls + reflection.selenaDrawCalls + clipped.selenaDrawCalls;
      if (passCount > 0) addWaterObjectTextureStats(stats, system, passCount, passCount, drawCalls, 0, selenaDrawCalls);
      system.waterObjectTextureRefreshCursor = (refreshCursor + 1) % targetSlots.length;
      system.waterObjectTextureRefreshRemaining = Math.max(0, refreshRemaining - (passCount > 0 ? 1 : 0));
    }
    if (restoredFrame) {
      uploadFrameUniforms(waterState.uniformState, waterState.gpuRes, device, bundle && bundle.camera, width, height, toneMap);
      uploadLights(waterState.uniformState, waterState.gpuRes, device, bundle && bundle.lights, wgpuBundlePhysicalLights(bundle));
    }
    return stats;
  }

  function updateWaterSystems(entries, encoder, nowMS, lifecycleActive, qualityProfile, qualityRevision, bundle, pbrBuffers, width, height, waterState: any, device: any) {
    waterState.activeWaterShaderSourcesByID = null;
    var canvasWaterShaderSources = waterState.canvas && (waterState.canvas.__gosxScene3DWaterShaderSources || (waterState.canvas.parentNode && waterState.canvas.parentNode.__gosxScene3DWaterShaderSources));
    var bundleWaterShaderSources = bundle && bundle.waterShaderSourcesByID && typeof bundle.waterShaderSourcesByID === "object"
      ? bundle.waterShaderSourcesByID
      : canvasWaterShaderSources;
    if (bundleWaterShaderSources && typeof bundleWaterShaderSources === "object") {
      waterState.activeWaterShaderSourcesByID = new Map();
      Object.keys(bundleWaterShaderSources).forEach(function(id) {
        var record = bundleWaterShaderSources[id];
        if (record && typeof record === "object") waterState.activeWaterShaderSourcesByID.set(id, record);
      });
    }
    var currentNowMS = Number.isFinite(nowMS) ? nowMS : 0;
    var currentTime = currentNowMS / 1000;
    var waterLifecycleActive = lifecycleActive !== false;
    var records = syncWaterSystems(entries, width, height, waterState, waterState.meshState, waterState.timingState, device);
    var stats = {
      records: records,
      waterSystems: records.length,
      waterCells: 0,
      waterVertices: 0,
      waterComputeDispatches: 0,
      waterSimulationTicks: 0,
      waterSolverSubsteps: 0,
      waterDroppedTicks: 0,
      waterDroppedTicksThisFrame: 0,
      waterSimulationCatchUpCap: 1,
      waterSimulationTickSeq: 0,
      waterSolverSubstepSeq: 0,
      waterNormalDispatches: 0,
      waterNormalDispatchSeq: 0,
      waterSampledStateCopies: 0,
      waterSampledStateSyncSeq: 0,
      // M5 at-rest gating (water-parity-campaign) -- see
      // WATER_REST_ENERGY_EPSILON's comment above for the mechanism.
      // waterAtRestSystems: how many systems are parked (skipping sim
      // substeps/normal/state-copy/caustics THIS frame) right now.
      // waterRestSubstepsSkipped: cumulative solver substeps NOT dispatched
      // because their system was at rest -- the direct measure of the win.
      waterAtRestSystems: 0,
      waterRestSubstepsSkipped: 0,
      // M6 per-frame churn audit -- see waterUniformSnapshotChanged's
      // comment (near sceneWaterUniformData). Direct measure of the
      // uniform-upload dedup: how many "commit" writeBuffer calls actually
      // hit the GPU (waterUniformUploads) vs were skipped because nothing
      // but the volatile time/frameIndex header changed (waterUniformUploadsSkipped).
      waterUniformUploads: 0,
      waterUniformUploadsSkipped: 0,
      waterQualityTier: "full",
      waterQualityRevision: 0,
      waterSurfaceResolution: 0,
      waterActiveCausticsResolution: 0,
      waterActiveObjectShadowResolution: 0,
      waterActiveObjectTextureWidth: 0,
      waterActiveObjectTextureHeight: 0,
      waterActiveObjectTexturePixelBudget: 0,
      waterQualityAllocationPending: 0,
      waterQualityAllocationFailures: 0,
      waterQualityAllocationRetryFrame: 0,
      waterQualityDPRCap: Infinity,
      waterExpensivePassCadence: 1,
      waterAuthoredComputeSystems: 0,
      waterAuthoredComputeDispatches: 0,
      waterAuthoredComputeFallbacks: 0,
      // Compute kernels routed through the generic descriptor-driven Selena
      // feedback-compute path (getSelenaComputePipeline/
      // createSelenaComputeBindGroup, dispatchWaterComputeStage above).
      // waterSelenaComputeSystems counts a system once if ANY of its 5
      // kernels have Selena WGSL+descriptor configured (mirrors
      // waterAuthoredComputeSystems' "was authored, not necessarily
      // dispatched-this-frame" semantics); waterSelenaComputeDispatches/
      // waterSelenaComputeFallbacks aggregate across all 5 kernels and every
      // dispatch call site (continuous + one-shot events), mirroring the
      // render passes' waterSelenaXxxPasses/waterSelenaXxxFallbacks pattern.
      waterSelenaComputeSystems: 0,
      waterSelenaComputeDispatches: 0,
      waterSelenaComputeFallbacks: 0,
      waterDropDispatches: 0,
      waterDropDispatchTotal: 0,
      waterLastDropEventID: 0,
      waterObjectSystems: 0,
      waterObjectDispatches: 0,
      waterObjectEventDispatches: 0,
      waterLastObjectDisplacementEventID: 0,
      waterObjectSpheres: 0,
      waterRoundedSystems: 0,
      waterCornerRadius: 0,
      waterLightDirX: 0,
      waterLightDirY: 0,
      waterLightDirZ: 0,
      waterCausticSystems: 0,
      waterCausticPasses: 0,
      waterCausticTexturePixels: 0,
      waterAuthoredCausticSystems: 0,
      waterAuthoredCausticPasses: 0,
      waterAuthoredCausticFallbacks: 0,
      waterAuthoredCausticFallbackReason: "",
      waterAuthoredCausticSourceBytes: 0,
      waterEntryCausticSourceBytes: 0,
      waterResolvedCausticSourceBytes: 0,
      waterAuthoredSurfaceSourceBytes: 0,
      waterEntrySurfaceSourceBytes: 0,
      waterResolvedSurfaceSourceBytes: 0,
      waterManifestShaderSystems: 0,
      waterManifestShaderFields: 0,
      waterManifestCausticSourceBytes: 0,
      waterManifestSurfaceSourceBytes: 0,
      waterBundleShaderSystems: waterState.activeWaterShaderSourcesByID ? waterState.activeWaterShaderSourcesByID.size : 0,
      waterBundleCausticSourceBytes: 0,
      waterBundleSurfaceSourceBytes: 0,
      waterObjectTexturePasses: 0,
      waterObjectTextureTargets: 0,
      waterObjectTexturePixels: 0,
      waterObjectTextureWidth: 0,
      waterObjectTextureHeight: 0,
      waterObjectTexturePixelBudget: 0,
      waterObjectTextureMeshPasses: 0,
      waterObjectTextureMeshDrawCalls: 0,
      waterObjectTextureSelenaDrawCalls: 0,
      waterObjectTextureFallbackPasses: 0,
      waterObjectTextureCandidateObjects: 0,
      waterObjectTextureSelectedObjects: 0,
      waterObjectTextureFallbackMissingObjects: 0,
      waterObjectTextureFallbackMissingResources: 0,
      waterObjectTextureCandidateProfile: "",
      waterObjectShadowPasses: 0,
      waterObjectShadowTexturePixels: 0,
      waterObjectShadowMeshPasses: 0,
      waterObjectShadowMeshDrawCalls: 0,
      waterAuthoredObjectShadowPasses: 0,
      waterAuthoredObjectShadowFallbacks: 0,
      waterAuthoredObjectMeshShadowPasses: 0,
      waterAuthoredObjectMeshShadowFallbacks: 0,
      waterObjectShadowFallbackPasses: 0,
      waterObjectShadowFallbackMissingObjects: 0,
      waterObjectShadowFallbackMissingResources: 0,
      waterReflectionSystems: 0,
      waterRefractionSystems: 0,
      waterObjectOpticsSystems: 0,
      // Caustics/object-shadow/compound-shadow/object-mesh-shadow passes
      // routed through the generic descriptor-driven Selena WebGPU render
      // path. See sceneWaterCausticsUsesSelena/getWaterCausticsSelenaDraw,
      // getWaterObjectShadowSelenaDraw, getWaterObjectMeshShadowSelenaDraw
      // above (the surface/surface-below/pool equivalents are aggregated by
      // drawWaterSystemEntries/drawWaterPoolEntries instead).
      waterSelenaCausticPasses: 0,
      waterSelenaCausticFallbacks: 0,
      waterSelenaObjectShadowPasses: 0,
      waterSelenaObjectShadowFallbacks: 0,
      waterSelenaObjectMeshShadowPasses: 0,
      waterSelenaObjectMeshShadowFallbacks: 0,
    };
    var manifestShaderStats = sceneWaterManifestShaderSourceStats(waterState, waterState.canvas);
    stats.waterManifestShaderSystems = manifestShaderStats.systems;
    stats.waterManifestShaderFields = manifestShaderStats.fields;
    stats.waterManifestCausticSourceBytes = manifestShaderStats.causticSourceBytes;
    stats.waterManifestSurfaceSourceBytes = manifestShaderStats.surfaceSourceBytes;
    if (waterState.activeWaterShaderSourcesByID) {
      waterState.activeWaterShaderSourcesByID.forEach(function(record) {
        stats.waterBundleCausticSourceBytes = Math.max(
          stats.waterBundleCausticSourceBytes,
          typeof record.causticsWGSL === "string" ? record.causticsWGSL.trim().length : 0
        );
        stats.waterBundleSurfaceSourceBytes = Math.max(
          stats.waterBundleSurfaceSourceBytes,
          sceneWaterSurfaceSourceBytes(record)
        );
      });
    }
    for (var i = 0; i < records.length; i++) {
      var system = records[i].system;
      if (!system) continue;
      if (qualityProfile && typeof qualityProfile === "object") {
        applySceneWaterQualityProfile(system, qualityProfile, qualityRevision, width, height, waterState, waterState.timingState, device);
      }
      var entry = system.entry || {};
      stats.waterEntryCausticSourceBytes = Math.max(
        stats.waterEntryCausticSourceBytes,
        typeof entry.causticsWGSL === "string" ? entry.causticsWGSL.trim().length : 0
      );
      // waterResolvedCausticSourceBytes/waterResolvedSurfaceSourceBytes/
      // waterAuthoredSurfaceSourceBytes stay 0 -- there is no more authored
      // (data-prop) WGSL resolution tier to measure; see
      // sceneWaterSystemSignature's comment above.
      stats.waterEntrySurfaceSourceBytes = Math.max(
        stats.waterEntrySurfaceSourceBytes,
        sceneWaterSurfaceSourceBytes(entry)
      );
      // Builtin pipelines only: the hand-written data-prop-authored compute
      // pipeline tier (sceneWaterAuthoredComputePipeline) has been removed
      // now that Selena is the sole primary compute WGSL source ahead of
      // these SCENE_WATER_COMPUTE_SOURCE-derived builtins (see
      // dispatchWaterComputeStage's Selena-first resolution below).
      var seedCompute = { pipeline: waterState.waterSeedPipeline, authored: false, failed: false };
      var dropCompute = { pipeline: waterState.waterDropPipeline, authored: false, failed: false };
      var displacementCompute = { pipeline: waterState.waterDisplacementPipeline, authored: false, failed: false };
      var simulationCompute = { pipeline: waterState.waterStepPipeline, authored: false, failed: false };
      var normalCompute = { pipeline: waterState.waterNormalPipeline, authored: false, failed: false };
      if (seedCompute.authored || dropCompute.authored || displacementCompute.authored || simulationCompute.authored || normalCompute.authored) {
        stats.waterAuthoredComputeSystems += 1;
      }
      if (seedCompute.failed) stats.waterAuthoredComputeFallbacks += 1;
      if (dropCompute.failed) stats.waterAuthoredComputeFallbacks += 1;
      if (displacementCompute.failed) stats.waterAuthoredComputeFallbacks += 1;
      if (simulationCompute.failed) stats.waterAuthoredComputeFallbacks += 1;
      if (normalCompute.failed) stats.waterAuthoredComputeFallbacks += 1;
      if (
        sceneWaterComputeStageUsesSelena(entry, "seed") ||
        sceneWaterComputeStageUsesSelena(entry, "drop") ||
        sceneWaterComputeStageUsesSelena(entry, "displacement") ||
        sceneWaterComputeStageUsesSelena(entry, "simulation") ||
        sceneWaterComputeStageUsesSelena(entry, "normal")
      ) {
        stats.waterSelenaComputeSystems += 1;
      }
      if (wgpuSceneWaterPoolShapeRounded(entry)) {
        stats.waterRoundedSystems += 1;
        stats.waterCornerRadius = Math.max(stats.waterCornerRadius, Math.max(0, sceneNumber(entry.cornerRadius, 0)));
      }
      var optics = sceneWaterOpticsFlags(entry, {
        kind: sceneWaterObjectKind(entry),
        displacementScale: Math.max(0, sceneNumber(entry.objectDisplacementScale, 1)),
      });
      if (optics.caustics) stats.waterCausticSystems += 1;
      if (optics.reflection) stats.waterReflectionSystems += 1;
      if (optics.refraction) stats.waterRefractionSystems += 1;
      if (optics.object) stats.waterObjectOpticsSystems += 1;
      // Resolve every authored compute stage before advancing the clock or
      // consuming events. A pending compilation must leave this tick intact.
      prepareWaterComputePipelines(system, entry, waterState, device);
      var waterPaused = sceneBool(entry && entry.paused, false);
      var waterClock = waterState.waterClockAPI.sceneWaterAdvanceClock(system.waterClock, currentNowMS, waterLifecycleActive, waterPaused, {
        simulationHz: 60,
        // Bound each present to the reference model's two solver passes.
        // Any elapsed excess remains visible as dropped-tick telemetry.
        maxCatchUpTicks: 1,
        solverSubsteps: 2,
      });
      var canConsumeWaterState = waterLifecycleActive && !waterPaused;
      var hasSimulationTick = canConsumeWaterState && waterClock.ticks > 0;
      var fixedDeltaSeconds = waterClock.tickSeconds || (1 / 60);
      stats.waterSimulationTicks += Math.max(0, waterClock.ticks || 0);
      stats.waterSolverSubsteps += Math.max(0, waterClock.substeps || 0);
      stats.waterDroppedTicks += Math.max(0, waterClock.droppedTicks || 0);
      stats.waterDroppedTicksThisFrame += Math.max(0, waterClock.dropped || 0);
      stats.waterSimulationTickSeq += Math.max(0, waterClock.tickSeq || 0);
      stats.waterSolverSubstepSeq += Math.max(0, waterClock.solverSubstepSeq || 0);
      device.queue.writeBuffer(system.uniformBuffer, 0, sceneWaterUniformData(
        system, entry, fixedDeltaSeconds, currentTime,
        { transientObject: true }, waterState, device
      ));
      if (system.waterLightDir) {
        stats.waterLightDirX = sceneNumber(system.waterLightDir.x, 0);
        stats.waterLightDirY = sceneNumber(system.waterLightDir.y, 0);
        stats.waterLightDirZ = sceneNumber(system.waterLightDir.z, 0);
      }
      stats.waterCells += system.cellCount;
      stats.waterVertices += system.vertexCount;
      stats.waterQualityTier = system.qualityTier || "full";
      stats.waterQualityRevision = Math.max(stats.waterQualityRevision, system.qualityRevision || 0);
      stats.waterSurfaceResolution = Math.max(stats.waterSurfaceResolution, system.surfaceResolution || 0);
      stats.waterActiveCausticsResolution = Math.max(stats.waterActiveCausticsResolution, system.causticsResolution || 0);
      stats.waterActiveObjectShadowResolution = Math.max(stats.waterActiveObjectShadowResolution, system.objectShadowResolution || 0);
      stats.waterActiveObjectTextureWidth = Math.max(stats.waterActiveObjectTextureWidth, system.objectTextureWidth || 0);
      stats.waterActiveObjectTextureHeight = Math.max(stats.waterActiveObjectTextureHeight, system.objectTextureHeight || 0);
      stats.waterActiveObjectTexturePixelBudget = Math.max(stats.waterActiveObjectTexturePixelBudget, system.objectTexturePixelBudget || 0);
      if (system.qualityAllocationPending) stats.waterQualityAllocationPending += 1;
      stats.waterQualityAllocationFailures += Math.max(0, system.qualityAllocationFailures || 0);
      stats.waterQualityAllocationRetryFrame = Math.max(stats.waterQualityAllocationRetryFrame, system.qualityAllocationNextFrame || 0);
      stats.waterQualityDPRCap = Math.min(stats.waterQualityDPRCap, system.qualityDPRCap || 1);
      stats.waterExpensivePassCadence = Math.max(stats.waterExpensivePassCadence, system.expensivePassCadence || 1);
      var waterStateDirty = false;
      if (hasSimulationTick && !system.seeded) {
        if (Math.max(0, Math.floor(sceneNumber(entry.seedDrops, 7))) > 0) {
          var seedResult = dispatchWaterComputeStage(encoder, system, entry, "seed", seedCompute.pipeline, undefined, waterState, device);
          stats.waterComputeDispatches += seedResult.dispatches;
          stats.waterSelenaComputeDispatches += seedResult.selena;
          stats.waterSelenaComputeFallbacks += seedResult.selenaFallback;
          waterStateDirty = waterStateDirty || seedResult.dispatches > 0;
          if (seedCompute.authored && seedResult.selena === 0) stats.waterAuthoredComputeDispatches += seedResult.dispatches;
        }
        system.seeded = true;
      }
      // Queued multi-drop trail (see dispatchWaterDropEvents above): drains
      // every drop queued since the last consumed id, one uniform write +
      // compute dispatch each, so a fast stroke's whole burst lands in this
      // one frame instead of thinning to the single latest drop. Runs
      // BEFORE the legacy scalar block below so its system.lastDropEventID
      // update makes that block's `!== dropEventID` check a no-op on the
      // same frame (entry.dropEventID mirrors the newest queued event id),
      // avoiding a double dispatch of the same drop.
      var dropEventsResult = hasSimulationTick
        ? dispatchWaterDropEvents(system, entry, encoder, dropCompute.pipeline, currentTime, waterState, device)
        : { dispatches: 0, selena: 0, selenaFallback: 0 };
      stats.waterSelenaComputeDispatches += dropEventsResult.selena;
      stats.waterSelenaComputeFallbacks += dropEventsResult.selenaFallback;
      if (dropEventsResult.dispatches > 0) {
        /* @ts-expect-error TS2339 -- this object literal grows fields after construction; TypeScript does not apply evolving-object inference to .ts files (only to checkJs .js files) */ system.dropDispatchCount = Math.max(0, Math.floor(sceneNumber(system.dropDispatchCount, 0))) + dropEventsResult.dispatches;
        stats.waterLastDropEventID = Math.max(stats.waterLastDropEventID, dropEventsResult.lastID || 0);
        stats.waterDropDispatches += dropEventsResult.dispatches;
        stats.waterComputeDispatches += dropEventsResult.dispatches;
        waterStateDirty = true;
        if (dropCompute.authored && dropEventsResult.selena === 0) stats.waterAuthoredComputeDispatches += dropEventsResult.dispatches;
      }
      var dropEventID = Math.max(0, Math.floor(sceneNumber(entry.dropEventID, 0)));
      if (hasSimulationTick && dropEventID > 0 && system.lastDropEventID !== dropEventID) {
        var dropResult = dispatchWaterComputeStage(encoder, system, entry, "drop", dropCompute.pipeline, undefined, waterState, device);
        var dropDispatches = dropResult.dispatches;
        stats.waterSelenaComputeDispatches += dropResult.selena;
        stats.waterSelenaComputeFallbacks += dropResult.selenaFallback;
        if (dropDispatches > 0) {
          system.lastDropEventID = dropEventID;
          system.dropDispatchCount = Math.max(0, Math.floor(sceneNumber(system.dropDispatchCount, 0))) + dropDispatches;
          stats.waterLastDropEventID = Math.max(stats.waterLastDropEventID, dropEventID);
          stats.waterDropDispatches += dropDispatches;
          stats.waterComputeDispatches += dropDispatches;
          waterStateDirty = true;
          if (dropCompute.authored && dropResult.selena === 0) stats.waterAuthoredComputeDispatches += dropDispatches;
        }
      }
      stats.waterLastDropEventID = Math.max(stats.waterLastDropEventID, Math.max(0, Math.floor(sceneNumber(system.lastDropEventID, 0))));
      stats.waterDropDispatchTotal += Math.max(0, Math.floor(sceneNumber(system.dropDispatchCount, 0)));
      var objectEventStats = hasSimulationTick
        ? dispatchWaterObjectDisplacementEvents(system, entry, encoder, displacementCompute.pipeline, currentTime, waterState, device)
        : { dispatches: 0, selena: 0, selenaFallback: 0 };
      stats.waterSelenaComputeDispatches += objectEventStats.selena;
      stats.waterSelenaComputeFallbacks += objectEventStats.selenaFallback;
      if (objectEventStats.dispatches > 0) {
        stats.waterObjectEventDispatches += objectEventStats.dispatches;
        stats.waterObjectDispatches += objectEventStats.dispatches;
        stats.waterComputeDispatches += objectEventStats.dispatches;
        waterStateDirty = true;
        if (displacementCompute.authored && objectEventStats.selena === 0) stats.waterAuthoredComputeDispatches += objectEventStats.dispatches;
      }
      stats.waterLastObjectDisplacementEventID = Math.max(stats.waterLastObjectDisplacementEventID, Math.max(0, Math.floor(sceneNumber(system.lastObjectDisplacementEventID, 0))));
      if (hasSimulationTick) {
        // Commit current/previous object state exactly once per simulation
        // tick frame, after transient one-shot event uniforms are consumed.
        // Zero-tick display frames leave the previous center untouched.
        // M6: pack once, then skip the actual GPU upload when nothing but
        // the volatile time/frameIndex header changed -- see
        // waterUniformSnapshotChanged's comment (near sceneWaterUniformData).
        var commitUniformData = sceneWaterUniformData(system, entry, fixedDeltaSeconds, currentTime, undefined, waterState, device);
        if (waterUniformSnapshotChanged(system, waterState)) {
          device.queue.writeBuffer(system.uniformBuffer, 0, commitUniformData);
          stats.waterUniformUploads += 1;
        } else {
          stats.waterUniformUploadsSkipped += 1;
        }
        if ((system.waterObjectActive || (system.waterObjectKind || 0) > 0) && system.waterObjectMoved) {
          stats.waterObjectSystems += 1;
          stats.waterObjectSpheres += Math.max(0, system.waterObjectSphereCount || 0);
          var objectResult = dispatchWaterComputeStage(encoder, system, entry, "displacement", displacementCompute.pipeline, undefined, waterState, device);
          var objectDispatches = objectResult.dispatches;
          stats.waterObjectDispatches += objectDispatches;
          stats.waterComputeDispatches += objectDispatches;
          stats.waterSelenaComputeDispatches += objectResult.selena;
          stats.waterSelenaComputeFallbacks += objectResult.selenaFallback;
          waterStateDirty = waterStateDirty || objectDispatches > 0;
          if (displacementCompute.authored && objectResult.selena === 0) stats.waterAuthoredComputeDispatches += objectDispatches;
        }
        // M5 at-rest gating (water-parity-campaign): decide whether to
        // actually run this tick's substeps/normal/state-copy/caustics, or
        // to retain the last-rendered textures. waterStateDirty is fully
        // finalized as of this line (seed/drop/queued object event/
        // continuous drag all landed above), so a disturbance dispatched
        // EARLIER in this same frame wakes the system immediately -- no
        // one-frame lag. See WATER_REST_ENERGY_EPSILON's comment for the
        // energy-proxy mechanism and WATER_REST_MIN_QUIET_MS for the floor.
        if (waterStateDirty) {
          system.waterRestEnergy = 1.0;
          system.waterLastDisturbanceMS = currentNowMS;
          system.waterAtRest = false;
        }
        var runWaterSim = !system.waterAtRest;
        if (runWaterSim) {
          // P3 fusion (water-parity-campaign): batch the tick's simulation
          // substeps AND the trailing normal reconstruction into ONE
          // compute pass instead of 3 separate beginComputePass/end()
          // sequences (2 substeps + 1 normal). This only cuts command-
          // encoding overhead, not GPU work or visibility: WebGPU
          // auto-synchronizes storage-buffer read-after-write hazards
          // between dispatchWorkgroups() calls in submission order
          // regardless of pass boundaries, so the normal dispatch -- issued
          // after every substep dispatch in program order, sharing this
          // same pass -- still observes exactly the post-final-substep
          // height field it did as a separate pass. copyBufferToTexture
          // (syncWaterSampledState) cannot be recorded while a pass is
          // open, so it stays its own encoder step after this pass ends.
          var waterSimPass = encoder.beginComputePass({ label: "gosx-water-sim-normal-pass" });
          for (var waterTick = 0; waterTick < waterClock.ticks; waterTick++) {
            for (var solverStep = 0; solverStep < 2; solverStep++) {
              var stepResult = dispatchWaterComputeStage(encoder, system, entry, "simulation", simulationCompute.pipeline, waterSimPass, waterState, device);
              stats.waterComputeDispatches += stepResult.dispatches;
              stats.waterSelenaComputeDispatches += stepResult.selena;
              stats.waterSelenaComputeFallbacks += stepResult.selenaFallback;
              if (simulationCompute.authored && stepResult.selena === 0) {
                stats.waterAuthoredComputeDispatches += stepResult.dispatches;
              }
            }
          }
          // Decay the cheap energy proxy by exactly the damping the
          // simulation kernel itself just applied, once per substep
          // actually dispatched this frame.
          var restDamping = Math.max(0, Math.min(1, sceneNumber(entry && entry.damping, 0.995)));
          var substepsRun = Math.max(0, waterClock.ticks) * 2;
          system.waterRestEnergy = Math.max(0, sceneNumber(system.waterRestEnergy, 1)) * Math.pow(restDamping, substepsRun);
          var quietMS = currentNowMS - Math.max(0, sceneNumber(system.waterLastDisturbanceMS, 0));
          if (!waterStateDirty && system.waterRestEnergy <= WATER_REST_ENERGY_EPSILON && quietMS >= WATER_REST_MIN_QUIET_MS) {
            system.waterAtRest = true;
          }
          // hasSimulationTick is guaranteed true here (runWaterSim is only
          // ever assigned inside the hasSimulationTick branch above), so
          // this folds the normal dispatch into the SAME pass unconditionally
          // -- equivalent to the former separate `if (hasSimulationTick &&
          // runWaterSim)` gate, now with a shared pass instead of its own.
          var normalResult = dispatchWaterComputeStage(encoder, system, entry, "normal", normalCompute.pipeline, waterSimPass, waterState, device);
          waterSimPass.end();
          var normalDispatches = normalResult.dispatches;
          stats.waterComputeDispatches += normalDispatches;
          stats.waterSelenaComputeDispatches += normalResult.selena;
          stats.waterSelenaComputeFallbacks += normalResult.selenaFallback;
          stats.waterNormalDispatches += normalDispatches;
          system.waterNormalDispatchSeq += normalDispatches;
          if (normalCompute.authored && normalResult.selena === 0) stats.waterAuthoredComputeDispatches += normalDispatches;
        } else {
          stats.waterRestSubstepsSkipped += Math.max(0, waterClock.ticks) * 2;
        }
      } else {
        var runWaterSim = false;
      }
      if (system.waterAtRest) stats.waterAtRestSystems += 1;
      if ((hasSimulationTick && runWaterSim) || system.stateTextureSyncSeq === 0) {
        stats.waterSampledStateCopies += syncWaterSampledState(encoder, system);
      }
      stats.waterSampledStateSyncSeq += Math.max(0, Math.floor(sceneNumber(system.stateTextureSyncSeq, 0)));
      stats.waterNormalDispatchSeq += Math.max(0, system.waterNormalDispatchSeq || 0);
      var expensivePassCadence = Math.max(1, system.expensivePassCadence || 1);
      var expensiveCadenceBucket = Math.floor(Math.max(0, waterClock.tickSeq || 0) / expensivePassCadence);
      // M5: while at rest and not freshly disturbed this frame, do not let
      // the cadence bucket's natural drift (it advances off the wall clock
      // regardless of whether we chose to consume ticks) force a caustics
      // refresh -- system.waterExpensiveCadenceBucket is deliberately left
      // stale below so the very next refresh (on wake) is unconditional.
      var refreshExpensivePasses = waterStateDirty || (!system.waterAtRest && system.waterExpensiveCadenceBucket !== expensiveCadenceBucket);
      if (refreshExpensivePasses) system.waterExpensiveCadenceBucket = expensiveCadenceBucket;
      if (optics.object || optics.caustics || system.waterObjectShadowSignature != null) {
        var objectShadowPasses = 0;
        var meshShadow = { passes: 0, drawCalls: 0 };
        var hasShadowSubject = waterSystemHasObjectTextureSubject(system);
        var objectList = hasShadowSubject ? sceneWaterObjectMeshList(bundle, entry) : [];
        // M4: includeCamera=false -- the object-shadow RTT is a light-space
        // projection (see sceneWaterObjectRenderSignature's comment above),
        // so orbiting the camera must NOT invalidate this cache.
        var shadowSignature = hasShadowSubject ? sceneWaterObjectRenderSignature(system, entry, bundle, objectList, false) : "";
        var refreshObjectShadow = system.waterObjectShadowSignature !== shadowSignature;
        if (refreshObjectShadow && hasShadowSubject && objectList.length > 0 && pbrBuffers) {
          meshShadow = renderWaterObjectMeshShadowPass(encoder, system, objectList, pbrBuffers, waterState, device);
        }
        if (meshShadow.passes > 0) {
          objectShadowPasses = meshShadow.passes;
          stats.waterObjectShadowMeshPasses += meshShadow.passes;
          /* @ts-expect-error TS2339 -- this object literal grows fields after construction; TypeScript does not apply evolving-object inference to .ts files (only to checkJs .js files) */ stats.waterObjectShadowMeshDrawCalls += meshShadow.drawCalls;
          /* @ts-expect-error TS2339 -- this object literal grows fields after construction; TypeScript does not apply evolving-object inference to .ts files (only to checkJs .js files) */ if (meshShadow.authored) stats.waterAuthoredObjectMeshShadowPasses += meshShadow.passes;
          /* @ts-expect-error TS2339 -- this object literal grows fields after construction; TypeScript does not apply evolving-object inference to .ts files (only to checkJs .js files) */ if (meshShadow.failed) stats.waterAuthoredObjectMeshShadowFallbacks += 1;
          /* @ts-expect-error TS2339 -- this object literal grows fields after construction; TypeScript does not apply evolving-object inference to .ts files (only to checkJs .js files) */ stats.waterSelenaObjectMeshShadowPasses += meshShadow.selena || 0;
          stats.waterSelenaObjectMeshShadowFallbacks += meshShadow.selenaFallback || 0;
        } else if (refreshObjectShadow) {
          if (hasShadowSubject && objectList.length === 0) stats.waterObjectShadowFallbackMissingObjects += 1;
          if (hasShadowSubject && !pbrBuffers) stats.waterObjectShadowFallbackMissingResources += 1;
          var shadowResult = renderWaterObjectShadowPass(encoder, system, waterState, device);
          objectShadowPasses = shadowResult && shadowResult.passes || 0;
          if (shadowResult && shadowResult.authored) stats.waterAuthoredObjectShadowPasses += objectShadowPasses;
          if (shadowResult && shadowResult.failed) stats.waterAuthoredObjectShadowFallbacks += 1;
          stats.waterObjectShadowFallbackPasses += objectShadowPasses;
          stats.waterSelenaObjectShadowPasses += (shadowResult && shadowResult.selena) || 0;
          stats.waterSelenaObjectShadowFallbacks += (shadowResult && shadowResult.selenaFallback) || 0;
        }
        stats.waterObjectShadowPasses += objectShadowPasses;
        if (objectShadowPasses > 0) {
          system.waterObjectShadowSignature = shadowSignature;
          stats.waterObjectShadowTexturePixels += Math.max(0, system.objectShadowResolution || 0) * Math.max(0, system.objectShadowResolution || 0);
        }
      }
      if (optics.caustics && refreshExpensivePasses) {
        var causticResult = renderWaterCausticsPass(encoder, system, waterState, device);
        var causticPasses = causticResult && causticResult.passes || 0;
        stats.waterAuthoredCausticSourceBytes = Math.max(stats.waterAuthoredCausticSourceBytes, causticResult && causticResult.sourceBytes || 0);
        stats.waterCausticPasses += causticPasses;
        if (causticResult && causticResult.authored) {
          stats.waterAuthoredCausticSystems += 1;
          stats.waterAuthoredCausticPasses += causticPasses;
        }
        if (causticResult && causticResult.failed) {
          stats.waterAuthoredCausticFallbacks += 1;
          stats.waterAuthoredCausticFallbackReason = waterState.waterAuthoredCausticsPipelineLastError;
        }
        if (causticPasses > 0) {
          stats.waterCausticTexturePixels += Math.max(0, system.causticsResolution || 0) * Math.max(0, system.causticsResolution || 0);
        }
        stats.waterSelenaCausticPasses += (causticResult && causticResult.selena) || 0;
        stats.waterSelenaCausticFallbacks += (causticResult && causticResult.selenaFallback) || 0;
      }
      system.frameIndex += 1;
    }
    if (!Number.isFinite(stats.waterQualityDPRCap)) stats.waterQualityDPRCap = 1;
    return stats;
  }
  function ensureWaterPipelines(waterState: any, pipeState: any, device: any) {
    if (waterState.waterComputeShaderModule) return;
    waterState.waterComputeShaderModule = device.createShaderModule({ label: "gosx-water-compute", code: SCENE_WATER_COMPUTE_SOURCE });
    waterState.waterSeedPipeline = wgpuCreateValidatedPipeline(device, "compute", {
      label: "gosx-water-seed-drops",
      layout: waterState.waterComputePipelineLayout,
      compute: { module: waterState.waterComputeShaderModule, entryPoint: "seedDrops" },
    });
    waterState.waterDropPipeline = wgpuCreateValidatedPipeline(device, "compute", {
      label: "gosx-water-add-drop",
      layout: waterState.waterComputePipelineLayout,
      compute: { module: waterState.waterComputeShaderModule, entryPoint: "addDrop" },
    });
    waterState.waterDisplacementPipeline = wgpuCreateValidatedPipeline(device, "compute", {
      label: "gosx-water-displace-object",
      layout: waterState.waterComputePipelineLayout,
      compute: { module: waterState.waterComputeShaderModule, entryPoint: "displaceObject" },
    });
    waterState.waterStepPipeline = wgpuCreateValidatedPipeline(device, "compute", {
      label: "gosx-water-step",
      layout: waterState.waterComputePipelineLayout,
      compute: { module: waterState.waterComputeShaderModule, entryPoint: "stepSimulation" },
    });
    waterState.waterNormalPipeline = wgpuCreateValidatedPipeline(device, "compute", {
      label: "gosx-water-normals",
      layout: waterState.waterComputePipelineLayout,
      compute: { module: waterState.waterComputeShaderModule, entryPoint: "updateNormals" },
    });
    waterState.waterRenderVertexModule = device.createShaderModule({ label: "gosx-water-render-vert", code: SCENE_WATER_RENDER_VERTEX_SOURCE });
    waterState.waterRenderFragmentModule = device.createShaderModule({ label: "gosx-water-render-frag", code: SCENE_WATER_RENDER_FRAGMENT_SOURCE });
    waterState.waterRenderBelowFragmentModule = device.createShaderModule({ label: "gosx-water-render-below-frag", code: SCENE_WATER_RENDER_BELOW_FRAGMENT_SOURCE });
    waterState.waterPoolVertexModule = device.createShaderModule({ label: "gosx-water-pool-vert", code: SCENE_WATER_POOL_VERTEX_SOURCE });
    waterState.waterPoolFragmentModule = device.createShaderModule({ label: "gosx-water-pool-frag", code: SCENE_WATER_POOL_FRAGMENT_SOURCE });
    waterState.waterCausticsVertexModule = device.createShaderModule({ label: "gosx-water-caustics-vert", code: SCENE_WATER_CAUSTICS_VERTEX_SOURCE });
    waterState.waterCausticsFragmentModule = device.createShaderModule({ label: "gosx-water-caustics-frag", code: SCENE_WATER_CAUSTICS_FRAGMENT_SOURCE });
    waterState.waterCausticsPipeline = wgpuCreateValidatedPipeline(device, "render", {
      label: "gosx-water-caustics-pass",
      layout: waterState.waterCausticsPipelineLayout,
      vertex: { module: waterState.waterCausticsVertexModule, entryPoint: "vertexMain", buffers: [] },
      fragment: {
        module: waterState.waterCausticsFragmentModule,
        entryPoint: "fragmentMain",
        targets: [{ format: WATER_CAUSTICS_TEXTURE_FORMAT }],
      },
      primitive: { topology: "triangle-list" },
    });
    waterState.waterObjectTextureVertexModule = device.createShaderModule({ label: "gosx-water-object-texture-vert", code: SCENE_WATER_OBJECT_TEXTURE_VERTEX_SOURCE });
    waterState.waterObjectTextureFragmentModule = device.createShaderModule({ label: "gosx-water-object-texture-frag", code: SCENE_WATER_OBJECT_TEXTURE_FRAGMENT_SOURCE });
    waterState.waterObjectShadowFragmentModule = device.createShaderModule({ label: "gosx-water-object-shadow-frag", code: SCENE_WATER_OBJECT_SHADOW_FRAGMENT_SOURCE });
    waterState.waterObjectMeshShadowVertexModule = device.createShaderModule({ label: "gosx-water-object-mesh-shadow-vert", code: SCENE_WATER_OBJECT_MESH_SHADOW_VERTEX_SOURCE });
    waterState.waterObjectMeshShadowFragmentModule = device.createShaderModule({ label: "gosx-water-object-mesh-shadow-frag", code: SCENE_WATER_OBJECT_MESH_SHADOW_FRAGMENT_SOURCE });
    pipeState.waterObjectMeshRefractionFragmentModule = device.createShaderModule({ label: "gosx-water-object-mesh-texture-frag", code: sceneWaterObjectMeshFragmentSource(1) });
    pipeState.waterObjectMeshClippedFragmentModule = device.createShaderModule({ label: "gosx-water-object-mesh-clipped-frag", code: sceneWaterObjectMeshFragmentSource(2) });
    waterState.waterObjectTexturePipeline = wgpuCreateValidatedPipeline(device, "render", {
      label: "gosx-water-object-texture-pass",
      layout: waterState.waterObjectTexturePipelineLayout,
      vertex: { module: waterState.waterObjectTextureVertexModule, entryPoint: "vertexMain", buffers: [] },
      fragment: {
        module: waterState.waterObjectTextureFragmentModule,
        entryPoint: "fragmentMain",
        targets: [
          { format: WATER_OBJECT_TEXTURE_FORMAT },
          { format: WATER_OBJECT_TEXTURE_FORMAT },
          { format: WATER_OBJECT_TEXTURE_FORMAT },
        ],
      },
      primitive: { topology: "triangle-list" },
    });
    waterState.waterObjectShadowPipeline = wgpuCreateValidatedPipeline(device, "render", {
      label: "gosx-water-object-shadow-pass",
      layout: waterState.waterObjectTexturePipelineLayout,
      vertex: { module: waterState.waterObjectTextureVertexModule, entryPoint: "vertexMain", buffers: [] },
      fragment: {
        module: waterState.waterObjectShadowFragmentModule,
        entryPoint: "shadowMain",
        targets: [{ format: WATER_OBJECT_TEXTURE_FORMAT }],
      },
      primitive: { topology: "triangle-list" },
    });
    waterState.waterObjectMeshShadowPipeline = wgpuCreateValidatedPipeline(device, "render", {
      label: "gosx-water-object-mesh-shadow-pass",
      layout: waterState.waterObjectMeshShadowPipelineLayout,
      vertex: { module: waterState.waterObjectMeshShadowVertexModule, entryPoint: "vertexMain", buffers: WGPU_PBR_VERTEX_LAYOUT },
      fragment: {
        module: waterState.waterObjectMeshShadowFragmentModule,
        entryPoint: "fragmentMain",
        targets: [{ format: WATER_OBJECT_TEXTURE_FORMAT }],
      },
      primitive: { topology: "triangle-list", cullMode: "none" },
    });
  }
  function setLifecycle(waterState: any, state: any) {
    var lifecycle = state && typeof state === "object" ? state : {};
    var nowMS = Number.isFinite(lifecycle.nowMS)
      ? lifecycle.nowMS
      : ((typeof performance !== "undefined" && typeof performance.now === "function") ? performance.now() : Date.now());
    var active = lifecycle.active !== false && lifecycle.disposed !== true;
    var paused = lifecycle.paused === true || lifecycle.disposed === true;
    for (var record of waterState.waterSystems.values()) {
      var system = record && record.system;
      if (!system || !system.waterClock) continue;
      waterState.waterClockAPI.sceneWaterResetClock(system.waterClock, nowMS, active, paused || sceneBool(system.entry && system.entry.paused, false));
    }
  }
