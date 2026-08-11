// 20a — Scene3D backend selection.
//
// Reads the scene bundle plus the browser capability profile and decides which
// GPU backend to try first. 20b resolves the renderer factory that decision
// names.
//
// Depends on 15c-scene-backend-registry.js and the runtime capability helpers
// 26d-feature-scene3d-prefix.js bridges. Nothing here touches a GPU object.
  function gosxSceneEmit(level, msg, fields) {
    try {
      if (typeof window !== "undefined" && typeof window.__gosx_emit === "function") {
        window.__gosx_emit(level, "scene3d", msg, fields || {});
      }
    } catch (_err) {
      /* telemetry must never surface to users */
    }
  }

  const SCENE3D_DEBUG_SCHEMA = "gosx.scene3d.debug.v1";

  function sceneDebugRegistry() {
    if (typeof window === "undefined") {
      return null;
    }
    let registry = window.__gosx_scene3d_debug_registry;
    if (!registry) {
      registry = new Map();
      try {
        Object.defineProperty(window, "__gosx_scene3d_debug_registry", {
          configurable: true,
          value: registry,
        });
      } catch (_err) {
        window.__gosx_scene3d_debug_registry = registry;
      }
    }
    if (!window.__gosx_scene3d_debug || window.__gosx_scene3d_debug.schema !== SCENE3D_DEBUG_SCHEMA) {
      const api = {
        schema: SCENE3D_DEBUG_SCHEMA,
        listSurfaces() {
          return sceneDebugSurfaceRecords(registry).map(function(record) {
            return sceneDebugReadSnapshot(record, "summary");
          }).filter(Boolean);
        },
        inspect(surfaceID) {
          const record = sceneDebugFindSurface(registry, surfaceID);
          return record ? sceneDebugReadSnapshot(record, "full") : null;
        },
        captureFrame(surfaceID) {
          const record = sceneDebugFindSurface(registry, surfaceID);
          if (!record || typeof record.captureFrame !== "function") {
            return { surfaceID: String(surfaceID || ""), dataURL: null, reason: "surface-not-found" };
          }
          try {
            return sceneDebugClone(record.captureFrame(), 4);
          } catch (err) {
            return {
              surfaceID: String(surfaceID || ""),
              dataURL: null,
              reason: "capture-failed",
              error: err && err.message ? String(err.message) : String(err || ""),
            };
          }
        },
        getDiagnostics(surfaceID) {
          const snapshot = this.inspect(surfaceID);
          return snapshot && Array.isArray(snapshot.diagnostics) ? snapshot.diagnostics : [];
        },
        getFeatureMatrix(surfaceID) {
          const snapshot = this.inspect(surfaceID);
          return snapshot && snapshot.features ? snapshot.features : {};
        },
        getGPUResources(surfaceID) {
          const snapshot = this.inspect(surfaceID);
          return snapshot && snapshot.gpuResources ? snapshot.gpuResources : {};
        },
        getLastPick(surfaceID) {
          const snapshot = this.inspect(surfaceID);
          return snapshot && snapshot.lastPick ? snapshot.lastPick : null;
        },
      };
      try {
        Object.defineProperty(window, "__gosx_scene3d_debug", {
          configurable: true,
          value: api,
        });
      } catch (_err) {
        window.__gosx_scene3d_debug = api;
      }
    }
    return registry;
  }

  function sceneDebugSurfaceRecords(registry) {
    const records = [];
    if (!registry || typeof registry.forEach !== "function") {
      return records;
    }
    registry.forEach(function(record) {
      if (record) {
        records.push(record);
      }
    });
    return records;
  }

  function sceneDebugFindSurface(registry, surfaceID) {
    const records = sceneDebugSurfaceRecords(registry);
    if (records.length === 0) {
      return null;
    }
    if (surfaceID == null || String(surfaceID).trim() === "") {
      return records.length === 1 ? records[0] : null;
    }
    const wanted = String(surfaceID);
    for (let i = 0; i < records.length; i += 1) {
      const record = records[i];
      if (record.key === wanted || record.id === wanted || record.mountID === wanted || record.engineID === wanted) {
        return record;
      }
    }
    return null;
  }

  function sceneDebugReadSnapshot(record, mode) {
    if (!record || typeof record.snapshot !== "function") {
      return null;
    }
    try {
      return sceneDebugClone(record.snapshot(mode || "full"), 6);
    } catch (err) {
      return {
        schema: SCENE3D_DEBUG_SCHEMA,
        id: record.id || "",
        mountID: record.mountID || "",
        engineID: record.engineID || "",
        diagnostics: [{
          severity: "error",
          code: "scene.debug.snapshot_failed",
          message: err && err.message ? String(err.message) : String(err || ""),
        }],
      };
    }
  }

  function sceneDebugRegisterSurface(record) {
    const registry = sceneDebugRegistry();
    if (!registry || !record) {
      return function() {};
    }
    const key = String(record.key || record.engineID || record.mountID || ("scene-" + registry.size));
    record.key = key;
    registry.set(key, record);
    return function() {
      registry.delete(key);
    };
  }

  function sceneDebugClone(value, depth, seen) {
    if (value == null || typeof value === "string" || typeof value === "number" || typeof value === "boolean") {
      return value;
    }
    if (typeof value === "function") {
      return undefined;
    }
    if (depth <= 0) {
      return Array.isArray(value) ? [] : {};
    }
    seen = seen || [];
    if (seen.indexOf(value) >= 0) {
      return undefined;
    }
    seen.push(value);
    if (Array.isArray(value)) {
      const out = [];
      for (let i = 0; i < value.length; i += 1) {
        const next = sceneDebugClone(value[i], depth - 1, seen);
        if (next !== undefined) {
          out.push(next);
        }
      }
      seen.pop();
      return out;
    }
    if (typeof ArrayBuffer !== "undefined" && ArrayBuffer.isView && ArrayBuffer.isView(value)) {
      const slice = Array.prototype.slice.call(value, 0, Math.min(value.length || 0, 64));
      seen.pop();
      return {
        type: value.constructor && value.constructor.name ? value.constructor.name : "TypedArray",
        length: value.length || 0,
        preview: slice,
      };
    }
    if (value && value.nodeType === 1) {
      const node = {
        tagName: value.tagName || "",
        id: value.id || "",
      };
      seen.pop();
      return node;
    }
    const out = {};
    for (const key of Object.keys(value)) {
      const next = sceneDebugClone(value[key], depth - 1, seen);
      if (next !== undefined) {
        out[key] = next;
      }
    }
    seen.pop();
    return out;
  }

  function sceneDebugMountID(mount, engineID) {
    const mountID = mount && mount.id ? String(mount.id) : "";
    return mountID || String(engineID || "");
  }

  function sceneDebugAttr(mount, name) {
    if (!mount || typeof mount.getAttribute !== "function") {
      return "";
    }
    const value = mount.getAttribute(name);
    return value == null ? "" : String(value);
  }

  function sceneDebugAttrNumber(mount, name) {
    const value = Number(sceneDebugAttr(mount, name));
    return Number.isFinite(value) ? value : 0;
  }

  function sceneDebugAddFeature(features, name, count) {
    const key = String(name || "").trim();
    const n = Math.max(0, Math.floor(sceneNumber(count == null ? 1 : count, 0)));
    if (!key || n <= 0) {
      return;
    }
    features[key] = (features[key] || 0) + n;
  }

  function sceneDebugKindFeature(prefix, kind, fallback) {
    const value = String(kind || fallback || "").trim().toLowerCase().replace(/[^a-z0-9]+/g, "");
    return value ? prefix + "." + value : "";
  }

  function sceneDebugCollectionArray(value) {
    if (Array.isArray(value)) {
      return value;
    }
    if (value && typeof value.forEach === "function" && typeof value.size === "number") {
      const out = [];
      value.forEach(function(entry) {
        out.push(entry);
      });
      return out;
    }
    return [];
  }

  const SCENE_MOUNT_WATER_SOURCE_ID_FIELDS = ["computeSource", "materialSource"];
  const SCENE_MOUNT_WATER_SOURCE_FILE_MAP_FIELDS = ["computeSourceFiles", "materialSourceFiles"];
  const SCENE_MOUNT_WATER_SHADER_STRING_FIELDS = [
    "seedWGSL", "dropWGSL", "displacementWGSL", "simulationWGSL", "normalWGSL", "causticsWGSL",
    "poolVertexWGSL", "poolFragmentWGSL", "surfaceVertexWGSL", "surfaceFragmentWGSL", "surfaceBelowFragmentWGSL",
    "objectShadowWGSL", "objectMeshShadowVertexWGSL", "objectMeshShadowFragmentWGSL",
  ];

  function sceneWaterMountStringMap(value) {
    if (!value || typeof value !== "object" || Array.isArray(value)) return null;
    const out = {};
    let count = 0;
    for (const key in value) {
      if (!Object.prototype.hasOwnProperty.call(value, key)) continue;
      if (typeof value[key] !== "string") continue;
      out[key] = value[key];
      count += 1;
    }
    return count ? out : null;
  }

  function sceneMountedWaterShaderSources() {
    const out = {};
    const script = typeof document !== "undefined" && document.getElementById ? document.getElementById("gosx-manifest") : null;
    if (!script || !script.textContent) return out;
    try {
      const manifest = JSON.parse(script.textContent);
      const engines = Array.isArray(manifest && manifest.engines) ? manifest.engines : [];
      for (let ei = 0; ei < engines.length; ei += 1) {
        const scene = engines[ei] && engines[ei].props && engines[ei].props.scene;
        const systems = scene && Array.isArray(scene.waterSystems) ? scene.waterSystems : [];
        for (let wi = 0; wi < systems.length; wi += 1) {
          const water = systems[wi];
          if (!water || typeof water !== "object") continue;
          const id = typeof water.id === "string" && water.id ? water.id : ("scene-water-" + wi);
          const record = out[id] || { id };
          let changed = false;
          for (let fi = 0; fi < SCENE_MOUNT_WATER_SOURCE_ID_FIELDS.length; fi += 1) {
            const name = SCENE_MOUNT_WATER_SOURCE_ID_FIELDS[fi];
            if (typeof water[name] === "string" && water[name].trim()) {
              record[name] = water[name];
              changed = true;
            }
          }
          for (let fi = 0; fi < SCENE_MOUNT_WATER_SOURCE_FILE_MAP_FIELDS.length; fi += 1) {
            const name = SCENE_MOUNT_WATER_SOURCE_FILE_MAP_FIELDS[fi];
            const files = sceneWaterMountStringMap(water[name]);
            if (files) {
              record[name] = files;
              changed = true;
            }
          }
          for (let fi = 0; fi < SCENE_MOUNT_WATER_SHADER_STRING_FIELDS.length; fi += 1) {
            const name = SCENE_MOUNT_WATER_SHADER_STRING_FIELDS[fi];
            if (typeof water[name] === "string" && water[name].trim()) {
              record[name] = water[name];
              changed = true;
            }
          }
          if (changed) out[id] = record;
        }
      }
    } catch (_err) {}
    return out;
  }

  function sceneHydrateBundleWaterShaderSources(bundle, sources) {
    if (!bundle || !Array.isArray(bundle.waterSystems) || !sources || typeof sources !== "object") return bundle;
    bundle.waterSystems = bundle.waterSystems.map(function(entry, index) {
      if (!entry || typeof entry !== "object") return entry;
      const id = typeof entry.id === "string" && entry.id ? entry.id : ("scene-water-" + index);
      const source = sources[id] || (Object.keys(sources).length === 1 ? sources[Object.keys(sources)[0]] : null);
      if (!source || typeof source !== "object") return entry;
      let hydrated = null;
      for (let fi = 0; fi < SCENE_MOUNT_WATER_SOURCE_ID_FIELDS.length; fi += 1) {
        const name = SCENE_MOUNT_WATER_SOURCE_ID_FIELDS[fi];
        if (typeof entry[name] === "string" && entry[name].trim()) continue;
        if (typeof source[name] !== "string" || !source[name].trim()) continue;
        if (!hydrated) hydrated = Object.assign({}, entry);
        hydrated[name] = source[name];
      }
      for (let fi = 0; fi < SCENE_MOUNT_WATER_SOURCE_FILE_MAP_FIELDS.length; fi += 1) {
        const name = SCENE_MOUNT_WATER_SOURCE_FILE_MAP_FIELDS[fi];
        if (sceneWaterMountStringMap(entry[name])) continue;
        const files = sceneWaterMountStringMap(source[name]);
        if (!files) continue;
        if (!hydrated) hydrated = Object.assign({}, entry);
        hydrated[name] = files;
      }
      for (let fi = 0; fi < SCENE_MOUNT_WATER_SHADER_STRING_FIELDS.length; fi += 1) {
        const name = SCENE_MOUNT_WATER_SHADER_STRING_FIELDS[fi];
        if (typeof entry[name] === "string" && entry[name].trim()) continue;
        if (typeof source[name] !== "string" || !source[name].trim()) continue;
        if (!hydrated) hydrated = Object.assign({}, entry);
        hydrated[name] = source[name];
      }
      return hydrated || entry;
    });
    return bundle;
  }

  function scenePublishWaterShaderSourcesToMount(mount, canvas, sources) {
    if (canvas) {
      canvas.__gosxScene3DWaterShaderSources = sources;
    }
    if (mount) {
      mount.__gosxScene3DWaterShaderSources = sources;
    }
    const canvasMount = canvas && canvas.parentNode;
    if (canvasMount && canvasMount !== mount) {
      canvasMount.__gosxScene3DWaterShaderSources = sources;
    }
  }

  function publishSceneWaterStateSnapshot(mount, sceneState) {
    if (!mount || typeof mount.setAttribute !== "function") return;
    const systems = Array.isArray(sceneState && sceneState.waterSystems) ? sceneState.waterSystems : [];
    let objectSystems = 0;
    let roundedSystems = 0;
    let causticSystems = 0;
    let reflectionSystems = 0;
    let refractionSystems = 0;
    let activeObject = "";
    let poolShape = "";
    let cornerRadius = 0;
    let poolWidth = 0;
    let poolHeight = 0;
    let poolLength = 0;
    systems.forEach(function(system, index) {
      if (!system || typeof system !== "object") return;
      const shape = String(system.poolShape || "");
      const rounded = shape.toLowerCase().indexOf("rounded") >= 0 && sceneNumber(system.cornerRadius, 0) > 0.0001;
      const objectKind = String(system.objectKind || system.activeObject || "").toLowerCase();
      const hasObject = objectKind !== "" && objectKind !== "none" && objectKind !== "null";
      if (hasObject) objectSystems += 1;
      if (rounded) roundedSystems += 1;
      if (system.caustics) causticSystems += 1;
      if (system.reflection) reflectionSystems += 1;
      if (system.refraction) refractionSystems += 1;
      if (index === 0) {
        activeObject = String(system.activeObject || system.objectKind || "");
        poolShape = shape;
        cornerRadius = sceneNumber(system.cornerRadius, 0);
        poolWidth = sceneNumber(system.poolWidth, 0);
        poolHeight = sceneNumber(system.poolHeight, 0);
        poolLength = sceneNumber(system.poolLength, 0);
      }
    });
    mount.setAttribute("data-gosx-scene3d-water-state-systems", String(systems.length));
    mount.setAttribute("data-gosx-scene3d-water-state-object-systems", String(objectSystems));
    mount.setAttribute("data-gosx-scene3d-water-state-rounded-systems", String(roundedSystems));
    mount.setAttribute("data-gosx-scene3d-water-state-caustic-systems", String(causticSystems));
    mount.setAttribute("data-gosx-scene3d-water-state-reflection-systems", String(reflectionSystems));
    mount.setAttribute("data-gosx-scene3d-water-state-refraction-systems", String(refractionSystems));
    mount.setAttribute("data-gosx-scene3d-water-state-active-object", activeObject);
    mount.setAttribute("data-gosx-scene3d-water-state-pool-shape", poolShape);
    mount.setAttribute("data-gosx-scene3d-water-state-corner-radius", String(cornerRadius));
    mount.setAttribute("data-gosx-scene3d-water-state-pool-width", String(poolWidth));
    mount.setAttribute("data-gosx-scene3d-water-state-pool-height", String(poolHeight));
    mount.setAttribute("data-gosx-scene3d-water-state-pool-length", String(poolLength));
  }

  function sceneWaterSystemsPaused(sceneState) {
    const systems = Array.isArray(sceneState && sceneState.waterSystems) ? sceneState.waterSystems : [];
    return systems.length > 0 && systems.every(function(system) {
      return sceneBool(system && system.paused, false);
    });
  }

  // Keep the always-on water proof surface deliberately small. These four
  // attributes are sufficient for release probes to distinguish a working,
  // advancing renderer from an unsupported, paused, or suspended scene. The
  // much larger shader/source diagnostics remain behind the debug flag below.
  function publishSceneWaterLifecycleState(mount, sceneState, lifecycle, disposed) {
    if (!mount || typeof mount.setAttribute !== "function") return;
    const systems = Array.isArray(sceneState && sceneState.waterSystems) ? sceneState.waterSystems : [];
    if (!systems.length) return;
    const paused = sceneWaterSystemsPaused(sceneState);
    const pageVisible = lifecycle ? lifecycle.pageVisible !== false : true;
    const inViewport = lifecycle ? lifecycle.inViewport !== false : true;
    let state = "running";
    if (disposed) state = "disposed";
    else if (!pageVisible) state = "page-hidden";
    else if (!inViewport) state = "offscreen";
    else if (paused) state = "paused";
    setAttrValue(mount, "data-gosx-scene3d-water-paused", paused ? "true" : "false");
    setAttrValue(mount, "data-gosx-scene3d-water-lifecycle", state);
  }

  function publishSceneWaterRendererState(mount, sceneState, renderer, reason) {
    if (!mount || typeof mount.setAttribute !== "function") return;
    const systems = Array.isArray(sceneState && sceneState.waterSystems) ? sceneState.waterSystems : [];
    if (!systems.length) return;
    const active = !!(renderer && (renderer.kind === "webgpu" || renderer.kind === "webgl"));
    setAttrValue(mount, "data-gosx-scene3d-water-renderer", active ? "active" : "unsupported");
    const unsupportedReason = active ? "" : (reason || "water-renderer-unavailable");
    setAttrValue(mount, "data-gosx-scene3d-water-unsupported-reason", unsupportedReason);
  }

  function recordSceneWaterFrame(mount, bundle, renderer) {
    if (!mount) return;
    if (renderer && renderer.kind === "webgl") {
      setAttrValue(mount, "data-gosx-scene3d-webgl-frame-seq",
        renderer.frameSeq = sceneNumber(renderer.frameSeq, 0) + 1);
      setAttrValue(mount, "data-gosx-scene3d-webgl-frame-at",
        renderer.frameAt = Math.max(Date.now(), sceneNumber(renderer.frameAt, 0) + 1));
    }
    if (!bundle || !Array.isArray(bundle.waterSystems) || !bundle.waterSystems.length) return;
    const next = sceneNumber(mount.__gosxScene3DWaterFrameSeq, 0) + 1;
    mount.__gosxScene3DWaterFrameSeq = next;
    // Keep the exact counter in JS for probes while publishing to DOM at 4 Hz
    // on a 60 FPS scene. This preserves a monotonic liveness signal without a
    // style/MutationObserver-visible attribute write on every frame.
    if (next < 2 || next % 15 === 0) {
      setAttrValue(mount, "data-gosx-scene3d-water-frame-seq", String(next));
    }
    const advancesSimulation = bundle.waterSystems.some(function(system) {
      return !sceneBool(system && system.paused, false);
    });
    if (!advancesSimulation) return;
    const simulationNext = sceneNumber(mount.__gosxScene3DWaterSimulationSeq, 0) + 1;
    mount.__gosxScene3DWaterSimulationSeq = simulationNext;
    if (simulationNext < 2 || simulationNext % 15 === 0) {
      setAttrValue(mount, "data-gosx-scene3d-water-simulation-seq", String(simulationNext));
    }
  }

  function sceneDebugBundleCounts(bundle, state) {
    const meshObjects = Array.isArray(bundle && bundle.meshObjects) ? bundle.meshObjects.length : 0;
    const worldObjects = Array.isArray(bundle && bundle.objects) ? bundle.objects.length : 0;
    const points = Array.isArray(bundle && bundle.points) ? bundle.points.length : 0;
    const instancedMeshes = Array.isArray(bundle && bundle.instancedMeshes) ? bundle.instancedMeshes.length : 0;
    const instancedGLBMeshes = Array.isArray(bundle && bundle.instancedGLBMeshes) ? bundle.instancedGLBMeshes.length : 0;
    const computeParticles = Array.isArray(bundle && bundle.computeParticles) ? bundle.computeParticles.length : 0;
    const waterSystems = Array.isArray(bundle && bundle.waterSystems) ? bundle.waterSystems.length : 0;
    const surfaces = Array.isArray(bundle && bundle.surfaces) ? bundle.surfaces.length : 0;
    const lines = Array.isArray(bundle && bundle.lines) ? bundle.lines.length : 0;
    const labels = Array.isArray(bundle && bundle.labels) ? bundle.labels.length : sceneDebugCollectionArray(state && state.labels).length;
    const sprites = Array.isArray(bundle && bundle.sprites) ? bundle.sprites.length : sceneDebugCollectionArray(state && state.sprites).length;
    const html = Array.isArray(bundle && bundle.html) ? bundle.html.length : sceneDebugCollectionArray(state && state.html).length;
    const lights = Array.isArray(bundle && bundle.lights) ? bundle.lights.length : sceneDebugCollectionArray(state && state.lights).length;
    const postEffects = Array.isArray(bundle && bundle.postEffects) ? bundle.postEffects.length : (Array.isArray(state && state.postEffects) ? state.postEffects.length : 0);
    const materials = Array.isArray(bundle && bundle.materials) ? bundle.materials.length : 0;
    return {
      meshObjects,
      worldObjects,
      points,
      instancedMeshes,
      instancedGLBMeshes,
      computeParticles,
      waterSystems,
      surfaces,
      lines,
      labels,
      sprites,
      html,
      lights,
      postEffects,
      materials,
      drawCalls: meshObjects + worldObjects + points + instancedMeshes + instancedGLBMeshes + computeParticles + waterSystems + surfaces + lines + postEffects,
      worldVertexCount: Math.max(0, Math.floor(sceneNumber(bundle && bundle.worldVertexCount, 0))),
      worldMeshVertexCount: Math.max(0, Math.floor(sceneNumber(bundle && bundle.worldMeshVertexCount, 0))),
    };
  }

  function sceneDebugRoundedNumber(value) {
    const number = sceneNumber(value, 0);
    return Math.round(number * 1000) / 1000;
  }

  function sceneDebugBounds(bounds) {
    if (!bounds || typeof bounds !== "object") {
      return null;
    }
    return {
      minX: sceneDebugRoundedNumber(bounds.minX),
      minY: sceneDebugRoundedNumber(bounds.minY),
      minZ: sceneDebugRoundedNumber(bounds.minZ),
      maxX: sceneDebugRoundedNumber(bounds.maxX),
      maxY: sceneDebugRoundedNumber(bounds.maxY),
      maxZ: sceneDebugRoundedNumber(bounds.maxZ),
    };
  }

  function sceneDebugMaterialSample(bundle, materialIndex) {
    const index = Math.floor(sceneNumber(materialIndex, -1));
    const materials = Array.isArray(bundle && bundle.materials) ? bundle.materials : [];
    if (index < 0 || index >= materials.length) {
      return null;
    }
    const material = materials[index] || {};
    return {
      kind: typeof material.kind === "string" ? material.kind : "",
      color: typeof material.color === "string" ? material.color : "",
      texture: typeof material.texture === "string" ? material.texture : "",
      opacity: sceneDebugRoundedNumber(material.opacity == null ? 1 : material.opacity),
      emissive: sceneDebugRoundedNumber(material.emissive),
      roughness: sceneDebugRoundedNumber(material.roughness),
      metalness: sceneDebugRoundedNumber(material.metalness),
      wireframe: Boolean(material.wireframe),
      shaderSource: typeof material.shaderSource === "string" ? material.shaderSource : "",
      shaderSourceFiles: sceneIsPlainObject(material.shaderSourceFiles) ? sceneDebugClone(material.shaderSourceFiles, 2) : null,
      key: typeof material.key === "string" ? material.key : "",
    };
  }

  function sceneDebugFeatureMatrix(bundle, state, rendererKind) {
    const features = {};
    sceneDebugAddFeature(features, sceneDebugKindFeature("backend", rendererKind, ""), 1);
    const objects = []
      .concat(Array.isArray(bundle && bundle.meshObjects) ? bundle.meshObjects : [])
      .concat(sceneDebugCollectionArray(state && state.objects));
    for (let i = 0; i < objects.length; i += 1) {
      sceneDebugAddFeature(features, sceneDebugKindFeature("geometry", objects[i] && objects[i].kind, "mesh"), 1);
    }
    const instanced = Array.isArray(bundle && bundle.instancedMeshes) ? bundle.instancedMeshes : sceneDebugCollectionArray(state && state.instancedMeshes);
    for (let i = 0; i < instanced.length; i += 1) {
      sceneDebugAddFeature(features, "geometry.instancedMesh", 1);
      sceneDebugAddFeature(features, sceneDebugKindFeature("geometry", instanced[i] && instanced[i].kind, "instanced"), 1);
    }
    sceneDebugAddFeature(features, "geometry.points", Array.isArray(bundle && bundle.points) ? bundle.points.length : 0);
    sceneDebugAddFeature(features, "geometry.lines", Array.isArray(bundle && bundle.lines) ? bundle.lines.length : 0);
    sceneDebugAddFeature(features, "geometry.surface", Array.isArray(bundle && bundle.surfaces) ? bundle.surfaces.length : 0);
    sceneDebugAddFeature(features, "particles.compute", Array.isArray(bundle && bundle.computeParticles) ? bundle.computeParticles.length : 0);
    sceneDebugAddFeature(features, "water.simulation", Array.isArray(bundle && bundle.waterSystems) ? bundle.waterSystems.length : 0);
    const html = Array.isArray(bundle && bundle.html) ? bundle.html : sceneDebugCollectionArray(state && state.html);
    for (let i = 0; i < html.length; i += 1) {
      const mode = String(html[i] && html[i].mode || "dom").trim().toLowerCase() || "dom";
      sceneDebugAddFeature(features, "html." + mode, 1);
    }
    const lights = Array.isArray(bundle && bundle.lights) ? bundle.lights : sceneDebugCollectionArray(state && state.lights);
    for (let i = 0; i < lights.length; i += 1) {
      sceneDebugAddFeature(features, sceneDebugKindFeature("lighting", lights[i] && lights[i].kind, "light"), 1);
      if ((lights[i] && lights[i].castShadow) || sceneNumber(lights[i] && lights[i].shadowSize, 0) > 0) {
        sceneDebugAddFeature(features, "lighting.shadows", 1);
      }
    }
    const postEffects = Array.isArray(bundle && bundle.postEffects) ? bundle.postEffects : (Array.isArray(state && state.postEffects) ? state.postEffects : []);
    for (let i = 0; i < postEffects.length; i += 1) {
      sceneDebugAddFeature(features, sceneDebugKindFeature("postfx", postEffects[i] && (postEffects[i].kind || postEffects[i].type), "unknown"), 1);
    }
    return features;
  }

  function sceneDebugDiagnostics(mount, rendererKind, rendererDiagnostics) {
    const diagnostics = [{
      severity: "info",
      code: "scene.backend.selected",
      message: rendererKind ? "Scene3D renderer selected" : "Scene3D renderer not selected",
      backend: rendererKind || "",
    }];
    const fallback = sceneDebugAttr(mount, "data-gosx-scene3d-renderer-fallback");
    if (fallback) {
      diagnostics.push({
        severity: "warn",
        code: "scene.backend.fallback",
        message: "Scene3D renderer fallback is active",
        backend: rendererKind || "",
        data: { reason: fallback },
      });
    }
    const webgpuError = sceneDebugAttr(mount, "data-gosx-scene3d-webgpu-last-error");
    if (webgpuError) {
      diagnostics.push({
        severity: "error",
        code: "scene.webgpu.render_error",
        message: webgpuError,
        backend: "webgpu",
      });
    }
    const customFallback = sceneDebugAttr(mount, "data-gosx-scene3d-webgpu-custom-material-fallback-reason");
    if (customFallback) {
      diagnostics.push({
        severity: "warn",
        code: "scene.shader.compile_error",
        message: "Custom material fell back to the standard WebGPU material path",
        backend: "webgpu",
        data: { reason: customFallback },
      });
    }
    if (rendererDiagnostics && rendererDiagnostics.ready === false) {
      diagnostics.push({
        severity: "warn",
        code: "scene.webgpu.not_ready",
        message: "WebGPU diagnostics report the device is not ready",
        backend: "webgpu",
      });
    }
    return diagnostics;
  }

  function sceneDebugHTMLTextureStats(labelLayer) {
    return {
      count: sceneDebugAttrNumber(labelLayer, "data-gosx-scene-html-texture-count"),
      ready: sceneDebugAttrNumber(labelLayer, "data-gosx-scene-html-texture-ready"),
      bytes: sceneDebugAttrNumber(labelLayer, "data-gosx-scene-html-texture-bytes"),
      capBytes: sceneDebugAttrNumber(labelLayer, "data-gosx-scene-html-texture-cap-bytes"),
      overBudget: sceneDebugAttrNumber(labelLayer, "data-gosx-scene-html-texture-over-budget"),
      dirty: sceneDebugAttrNumber(labelLayer, "data-gosx-scene-html-texture-dirty"),
      dirtyBytes: sceneDebugAttrNumber(labelLayer, "data-gosx-scene-html-texture-dirty-bytes"),
      pendingUploadBytes: sceneDebugAttrNumber(labelLayer, "data-gosx-scene-html-texture-upload-pending-bytes"),
      disposed: sceneDebugAttrNumber(labelLayer, "data-gosx-scene-html-texture-disposed"),
      disposedBytes: sceneDebugAttrNumber(labelLayer, "data-gosx-scene-html-texture-disposed-bytes"),
      revision: sceneDebugAttrNumber(labelLayer, "data-gosx-scene-html-texture-revision"),
    };
  }

  function sceneDebugGPUResources(mount, canvas, renderer, bundle, viewport, labelLayer, rendererDiagnostics) {
    const counts = sceneDebugBundleCounts(bundle, null);
    const htmlTextures = sceneDebugHTMLTextureStats(labelLayer);
    const webgpuStats = mount && mount.__gosxScene3DWebGPUStats ? mount.__gosxScene3DWebGPUStats : null;
    return {
      backend: renderer && renderer.kind ? renderer.kind : "",
      canvas: {
        width: canvas && canvas.width ? canvas.width : 0,
        height: canvas && canvas.height ? canvas.height : 0,
        cssWidth: sceneNumber(viewport && viewport.cssWidth, 0),
        cssHeight: sceneNumber(viewport && viewport.cssHeight, 0),
        devicePixelRatio: sceneNumber(viewport && viewport.devicePixelRatio, 1),
      },
      drawCalls: counts.drawCalls,
      materials: counts.materials,
      meshObjects: counts.meshObjects,
      worldObjects: counts.worldObjects,
      points: counts.points,
      instancedMeshes: counts.instancedMeshes,
      surfaces: counts.surfaces,
      lines: counts.lines,
      postEffects: counts.postEffects,
      htmlTextures,
      webgpu: {
        stats: sceneDebugClone(webgpuStats, 3),
        diagnostics: sceneDebugClone(rendererDiagnostics, 3),
      },
    };
  }

  // sceneWebGLRendererLooksMasked: true for the well-known generic
  // placeholder strings a browser substitutes for gl.VENDOR/gl.RENDERER
  // when it hasn't (yet) unmasked the plain query — "Generic Renderer" /
  // "Mozilla" (Firefox privacy.resistFingerprinting-style masking),
  // "WebKit WebGL" (Safari) — or empty. Real GPU/driver strings never
  // match these exactly.
  function sceneWebGLRendererLooksMasked(text) {
    const trimmed = String(text || "").trim();
    if (!trimmed) return true;
    const lowered = trimmed.toLowerCase();
    return lowered === "generic renderer" || lowered === "webkit webgl" || lowered === "mozilla" || lowered === "webgl";
  }

  function sceneReadWebGLRendererMetadata(gl) {
    if (!gl || typeof gl.getParameter !== "function") {
      return { vendor: "", renderer: "" };
    }
    let vendor = "";
    let renderer = "";
    // Try the plain (unextended) query FIRST — modern engines increasingly
    // return the real, unmasked string here directly. Firefox has
    // deprecated WEBGL_debug_renderer_info in favor of this and logs
    // "WEBGL_debug_renderer_info is deprecated in Firefox and will be
    // removed. Please use RENDERER." to the console every time the
    // extension is requested — even when the answer it gives is never used
    // downstream — so querying it unconditionally (the old behavior here)
    // spammed that warning on every WebGL mount in Firefox.
    try {
      vendor = String(gl.getParameter(gl.VENDOR) || "").trim();
    } catch (_error) {
      vendor = "";
    }
    try {
      renderer = String(gl.getParameter(gl.RENDERER) || "").trim();
    } catch (_error) {
      renderer = "";
    }
    // Fall back to the debug extension ONLY when the plain query came back
    // masked/empty — older engines that still mask gl.VENDOR/gl.RENDERER
    // by default need it; anything that already returned a real string
    // skips the (deprecated-on-Firefox) extension call entirely.
    if (sceneWebGLRendererLooksMasked(vendor) || sceneWebGLRendererLooksMasked(renderer)) {
      try {
        const debugInfo = typeof gl.getExtension === "function"
          ? gl.getExtension("WEBGL_debug_renderer_info")
          : null;
        if (debugInfo) {
          const unmaskedVendor = String(gl.getParameter(debugInfo.UNMASKED_VENDOR_WEBGL) || "").trim();
          const unmaskedRenderer = String(gl.getParameter(debugInfo.UNMASKED_RENDERER_WEBGL) || "").trim();
          if (unmaskedVendor) vendor = unmaskedVendor;
          if (unmaskedRenderer) renderer = unmaskedRenderer;
        }
      } catch (_error) {
        // Keep whatever the plain query returned.
      }
    }
    return {
      vendor: vendor.trim(),
      renderer: renderer.trim(),
    };
  }

  function sceneWebGLRendererLooksSoftware(metadata) {
    const vendor = metadata && typeof metadata.vendor === "string" ? metadata.vendor : "";
    const renderer = metadata && typeof metadata.renderer === "string" ? metadata.renderer : "";
    const text = (vendor + " " + renderer).trim().toLowerCase();
    if (!text) {
      return false;
    }
    return text.indexOf("swiftshader") !== -1
      || text.indexOf("llvmpipe") !== -1
      || text.indexOf("softpipe") !== -1
      || text.indexOf("lavapipe") !== -1
      || text.indexOf("software") !== -1
      || text.indexOf("microsoft basic render") !== -1
      || text.indexOf("basic render driver") !== -1;
  }

  function sceneProbeWebGLRenderer() {
    if (typeof window === "undefined" || typeof document === "undefined" || !document || typeof document.createElement !== "function") {
      return { available: false, software: false, vendor: "", renderer: "" };
    }
    const gosx = window.__gosx || (window.__gosx = {});
    if (gosx.scene3dWebGLProbe) {
      return gosx.scene3dWebGLProbe;
    }
    const probeCanvas = document.createElement("canvas");
    let gl = null;
    try {
      if (probeCanvas && typeof probeCanvas.getContext === "function") {
        const probeOptions = {
          alpha: false,
          antialias: false,
          preserveDrawingBuffer: false,
          powerPreference: "low-power",
        };
        gl =
          probeCanvas.getContext("webgl2", probeOptions) ||
          probeCanvas.getContext("webgl", probeOptions) ||
          probeCanvas.getContext("experimental-webgl", probeOptions);
      }
      const metadata = sceneReadWebGLRendererMetadata(gl);
      gosx.scene3dWebGLProbe = {
        available: !!gl,
        software: sceneWebGLRendererLooksSoftware(metadata),
        vendor: metadata.vendor,
        renderer: metadata.renderer,
      };
    } catch (_error) {
      gosx.scene3dWebGLProbe = { available: false, software: false, vendor: "", renderer: "" };
    }
    return gosx.scene3dWebGLProbe;
  }

  function sceneRequiresWebGL(props) {
    return sceneBool(props && props.requireWebGL, false);
  }

  function sceneForcesWebGL(props) {
    return sceneBool(props && props.forceWebGL, false) ||
      (typeof window !== "undefined" && (
        window.__gosx_scene3d_force_webgl === true ||
        (typeof window.__gosx_scene3d_force_webgl_requested === "function" && window.__gosx_scene3d_force_webgl_requested())
      ));
  }

  function scenePrefersWebGPU(props) {
    if (!props) {
      return false;
    }
    const value = Object.prototype.hasOwnProperty.call(props, "preferWebGPU")
      ? props.preferWebGPU
      : props.preferWebgpu;
    return sceneBool(value, false);
  }

  function sceneWebGPUOptions(props, capability) {
    const caps = capability && typeof capability === "object" ? capability : {};
    const requestedSamples = Math.max(0, Math.floor(sceneNumber(props && props.msaaSamples, 0)));
    const tierAllowsMSAA = caps.tier === "full" && !caps.lowPower && !caps.reducedData;
    const antialias = requestedSamples > 1
      ? true
      : sceneBool(props && props.antialias, tierAllowsMSAA);
    return {
      antialias,
      msaaSamples: requestedSamples > 1 ? 4 : (antialias ? 4 : 1),
      powerPreference: sceneWebGPUPowerPreference(props && (props.webgpuPowerPreference || props.webGPUPowerPreference || props.webgpuAdapterPowerPreference || props.webGPUAdapterPowerPreference)),
      presentation: sceneWebGPUPresentationOptions(props),
    };
  }

  function sceneWebGPUPresentationOptions(props) {
    const alphaMode = sceneWebGPUAlphaMode(props && (props.webgpuAlphaMode || props.webGPUAlphaMode || props.webgpuCanvasAlphaMode || props.webGPUCanvasAlphaMode));
    const colorSpace = sceneWebGPUColorSpace(props && (props.webgpuColorSpace || props.webGPUColorSpace));
    const toneMappingMode = sceneWebGPUToneMappingMode(props && (props.webgpuToneMapping || props.webGPUToneMapping || props.webgpuToneMappingMode || props.webGPUToneMappingMode));
    return {
      alphaMode,
      colorSpace,
      toneMappingMode,
    };
  }

  function sceneWebGPUAlphaMode(value) {
    const normalized = String(value || "").trim().toLowerCase();
    if (normalized === "opaque" || normalized === "premultiplied") {
      return normalized;
    }
    return "premultiplied";
  }

  function sceneWebGPUColorSpace(value) {
    const normalized = String(value || "").trim().toLowerCase();
    if (normalized === "display-p3" || normalized === "srgb") {
      return normalized;
    }
    return "srgb";
  }

  function sceneWebGPUToneMappingMode(value) {
    const normalized = String(value || "").trim().toLowerCase();
    if (normalized === "extended" || normalized === "standard") {
      return normalized;
    }
    return "";
  }

  function sceneWebGPUPowerPreference(value) {
    const normalized = String(value || "").trim().toLowerCase();
    if (normalized === "high-performance" || normalized === "low-power") {
      return normalized;
    }
    return "";
  }

  function sceneWebGPUUnsupportedLineStyle(entry) {
    if (!entry || typeof entry !== "object") {
      return false;
    }
    const material = entry.material && typeof entry.material === "object" ? entry.material : null;
    const materialKind = String(entry.materialKind || entry.kind || material && material.kind || "").toLowerCase();
    return entry.lineDash === true ||
      material && material.lineDash === true ||
      materialKind === "line-dashed" ||
      materialKind === "dashed";
  }

  function sceneWebGPUUnsupportedLineCollection(list) {
    if (!Array.isArray(list)) {
      return false;
    }
    for (let i = 0; i < list.length; i += 1) {
      const entry = list[i];
      if (!entry || typeof entry !== "object") {
        continue;
      }
      if (sceneWebGPUUnsupportedLineStyle(entry)) {
        return true;
      }
      if (Array.isArray(entry.children) && sceneWebGPUUnsupportedLineCollection(entry.children)) {
        return true;
      }
    }
    return false;
  }

  function sceneWebGPUUnsupportedLineBundle(source) {
    const dashes = source && source.worldLineDashes;
    if (dashes && typeof dashes.length === "number") {
      for (let i = 0; i < dashes.length; i += 1) {
        if (dashes[i]) {
          return true;
        }
      }
    }
    return false;
  }

  function sceneWebGPUFeatureGap(source) {
    const bc = sceneBackendCapsOf(source);
    if (bc && Array.isArray(bc.capable)) {
      return bc.capable.some(function(b) { return String(b).toLowerCase() === "webgpu"; }) ? "" : "backendcaps-excluded";
    }
    const root = source && typeof source === "object" ? source : {};
    const scene = root.scene && typeof root.scene === "object" ? root.scene : null;
    const candidates = scene ? [root, scene] : [root];
    if (sceneWebGPUUnsupportedLineBundle(root)) { return "line-styles"; }
    for (let i = 0; i < candidates.length; i += 1) {
      const item = candidates[i] || {};
      if (sceneWebGPUUnsupportedLineCollection(item.lines) || sceneWebGPUUnsupportedLineCollection(item.objects)) {
        return "line-styles";
      }
    }
    return "";
  }

  function sceneNeedsWebGLForWebGPUCoverage(source) {
    return sceneWebGPUFeatureGap(source) !== "";
  }

  function sceneBackendCapsOf(props) {
    if (!props || typeof props !== "object") return null;
    var s = props.scene;
    if (s && typeof s === "object" && s.backendCaps) return s.backendCaps;
    return props.backendCaps || null; // fallback if caller passes the scene object directly
  }

  function sceneBackendCapsAllowsKind(backendCaps, kind) {
    if (!backendCaps || !Array.isArray(backendCaps.capable)) return true;
    var wanted = String(kind || "").toLowerCase();
    if (wanted === "canvas") wanted = "canvas2d";
    if (wanted === "webgl2") wanted = "webgl";
    for (var i = 0; i < backendCaps.capable.length; i += 1) {
      var candidate = String(backendCaps.capable[i] || "").toLowerCase();
      if (candidate === "canvas") candidate = "canvas2d";
      if (candidate === "webgl2") candidate = "webgl";
      if (candidate === wanted) return true;
    }
    return false;
  }

  function chooseSceneBackend(backendCaps, prefs, availability) {
    const avail = availability && typeof availability === "object" ? availability : {};
    const webgpuAvail = !!avail.webgpu;
    const webglAvail = avail.webgl !== false;
    if (prefs && (prefs.requireWebGL || prefs.forceWebGL)) { return { backend: "webgl", fallbackReason: "", degraded: [] }; }
    if (prefs && prefs.preferCanvas) { return { backend: "canvas2d", fallbackReason: "", degraded: [] }; }
    if (!backendCaps || !Array.isArray(backendCaps.capable)) { return null; }
    const capable = backendCaps.capable;
    const degraded = backendCaps.degraded && typeof backendCaps.degraded === "object" ? backendCaps.degraded : {};
    const reasons = Array.isArray(backendCaps.reasons) ? backendCaps.reasons : [];
    let exclusionReason = "";
    for (let k = 0; k < reasons.length; k += 1) {
      const rk = reasons[k];
      if (rk && String(rk.excludes || "").toLowerCase() === "webgpu" && rk.feature) { exclusionReason = String(rk.feature); break; }
    }
    for (let i = 0; i < capable.length; i += 1) {
      const b = String(capable[i]).toLowerCase();
      if (b === "webgpu") {
        if (webgpuAvail) { return { backend: "webgpu", fallbackReason: "", degraded: Array.isArray(degraded["webgpu"]) ? degraded["webgpu"].map(String) : [] }; }
        continue;
      }
      if (b === "webgl" || b === "webgl2") {
        if (webglAvail) {
          const skipped = capable.slice(0, i).some(function(c) { return String(c).toLowerCase() === "webgpu"; });
          return { backend: "webgl", fallbackReason: skipped ? "webgpu-unavailable" : exclusionReason, degraded: [] };
        }
        continue;
      }
      if (b === "canvas2d" || b === "canvas") { return { backend: "canvas2d", fallbackReason: "", degraded: [] }; }
    }
    return { backend: null, fallbackReason: exclusionReason || "no-capable-backend", degraded: [] };
  }
