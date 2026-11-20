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
