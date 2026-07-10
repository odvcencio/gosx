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
    const active = Boolean(renderer && (renderer.kind === "webgpu" || renderer.kind === "webgl"));
    setAttrValue(mount, "data-gosx-scene3d-water-renderer", active ? "active" : "unsupported");
    const unsupportedReason = active ? "" : (reason || "water-renderer-unavailable");
    setAttrValue(mount, "data-gosx-scene3d-water-unsupported-reason", unsupportedReason);
  }

  function recordSceneWaterFrame(mount, bundle) {
    if (!mount || typeof mount.setAttribute !== "function" || !bundle ||
        !Array.isArray(bundle.waterSystems) || bundle.waterSystems.length === 0) return;
    const current = Number(mount.__gosxScene3DWaterFrameSeq);
    const next = (Number.isFinite(current) ? current : 0) + 1;
    mount.__gosxScene3DWaterFrameSeq = next;
    // Keep the exact counter in JS for probes while publishing to DOM at 4 Hz
    // on a 60 FPS scene. This preserves a monotonic liveness signal without a
    // style/MutationObserver-visible attribute write on every frame.
    if (next === 1 || next % 15 === 0) {
      setAttrValue(mount, "data-gosx-scene3d-water-frame-seq", String(next));
    }
    const advancesSimulation = bundle.waterSystems.some(function(system) {
      return !sceneBool(system && system.paused, false);
    });
    if (!advancesSimulation) return;
    const simulationCurrent = Number(mount.__gosxScene3DWaterSimulationSeq);
    const simulationNext = (Number.isFinite(simulationCurrent) ? simulationCurrent : 0) + 1;
    mount.__gosxScene3DWaterSimulationSeq = simulationNext;
    if (simulationNext === 1 || simulationNext % 15 === 0) {
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

  function sceneDebugFighterRenderEntries(bundle, limit) {
    const max = Math.max(0, Math.floor(sceneNumber(limit, 24)));
    if (!max) {
      return [];
    }
    const entries = []
      .concat(Array.isArray(bundle && bundle.meshObjects) ? bundle.meshObjects : [])
      .concat(Array.isArray(bundle && bundle.objects) ? bundle.objects : []);
    const samples = [];
    for (let index = 0; index < entries.length && samples.length < max; index += 1) {
      const entry = entries[index];
      const id = String(entry && entry.id || "");
      if (id.indexOf("fighter-") < 0) {
        continue;
      }
      samples.push({
        id,
        kind: String(entry && entry.kind || ""),
        materialIndex: Math.floor(sceneNumber(entry && entry.materialIndex, -1)),
        material: sceneDebugMaterialSample(bundle, entry && entry.materialIndex),
        renderPass: String(entry && entry.renderPass || ""),
        vertexOffset: Math.max(0, Math.floor(sceneNumber(entry && entry.vertexOffset, 0))),
        vertexCount: Math.max(0, Math.floor(sceneNumber(entry && entry.vertexCount, 0))),
        bounds: sceneDebugBounds(entry && entry.bounds),
        viewCulled: Boolean(entry && entry.viewCulled),
        depthCenter: sceneDebugRoundedNumber(entry && entry.depthCenter),
        doubleSided: Boolean(entry && entry.doubleSided),
        static: Boolean(entry && entry.static),
      });
    }
    return samples;
  }

  function sceneDebugFighterVisibleRenderEntries(bundle, limit) {
    const max = Math.max(0, Math.floor(sceneNumber(limit, 24)));
    if (!max) {
      return [];
    }
    const entries = []
      .concat(Array.isArray(bundle && bundle.meshObjects) ? bundle.meshObjects : [])
      .concat(Array.isArray(bundle && bundle.objects) ? bundle.objects : []);
    const samples = [];
    for (let index = 0; index < entries.length && samples.length < max; index += 1) {
      const entry = entries[index];
      const id = String(entry && entry.id || "");
      if (id.indexOf("fighter-") < 0) {
        continue;
      }
      const bounds = entry && entry.bounds;
      const size = bounds && typeof bounds === "object"
        ? Math.max(
          Math.abs(sceneNumber(bounds.maxX, 0) - sceneNumber(bounds.minX, 0)),
          Math.abs(sceneNumber(bounds.maxY, 0) - sceneNumber(bounds.minY, 0)),
          Math.abs(sceneNumber(bounds.maxZ, 0) - sceneNumber(bounds.minZ, 0)),
        )
        : 0;
      if (size <= 0.05) {
        continue;
      }
      samples.push({
        id,
        kind: String(entry && entry.kind || ""),
        materialIndex: Math.floor(sceneNumber(entry && entry.materialIndex, -1)),
        material: sceneDebugMaterialSample(bundle, entry && entry.materialIndex),
        renderPass: String(entry && entry.renderPass || ""),
        vertexOffset: Math.max(0, Math.floor(sceneNumber(entry && entry.vertexOffset, 0))),
        vertexCount: Math.max(0, Math.floor(sceneNumber(entry && entry.vertexCount, 0))),
        bounds: sceneDebugBounds(bounds),
        viewCulled: Boolean(entry && entry.viewCulled),
        depthCenter: sceneDebugRoundedNumber(entry && entry.depthCenter),
        doubleSided: Boolean(entry && entry.doubleSided),
        static: Boolean(entry && entry.static),
      });
    }
    return samples;
  }

  function sceneDebugModelTransformSample(model) {
    const source = model && typeof model === "object" ? model : {};
    return {
      x: sceneDebugRoundedNumber(source.x),
      y: sceneDebugRoundedNumber(source.y),
      z: sceneDebugRoundedNumber(source.z),
      rotationX: sceneDebugRoundedNumber(source.rotationX),
      rotationY: sceneDebugRoundedNumber(source.rotationY),
      rotationZ: sceneDebugRoundedNumber(source.rotationZ),
      scaleX: sceneDebugRoundedNumber(source.scaleX == null ? 1 : source.scaleX),
      scaleY: sceneDebugRoundedNumber(source.scaleY == null ? 1 : source.scaleY),
      scaleZ: sceneDebugRoundedNumber(source.scaleZ == null ? 1 : source.scaleZ),
      opacity: sceneDebugRoundedNumber(source.opacity == null ? 1 : source.opacity),
    };
  }

  function sceneDebugModelObjectSamples(state, objectIDs, limit) {
    const max = Math.max(0, Math.floor(sceneNumber(limit, 4)));
    if (!state || !state.objects || typeof state.objects.get !== "function" || !Array.isArray(objectIDs) || !max) {
      return [];
    }
    const samples = [];
    for (let index = 0; index < objectIDs.length && samples.length < max; index += 1) {
      const id = objectIDs[index];
      const object = state.objects.get(id);
      if (!object) {
        continue;
      }
      samples.push({
        id: String(object.id || id || ""),
        kind: String(object.kind || ""),
        vertexCount: Math.max(0, Math.floor(sceneNumber(object.vertices && object.vertices.count, 0))),
        color: typeof object.color === "string" ? object.color : "",
        texture: typeof object.texture === "string" ? object.texture : "",
        opacity: sceneDebugRoundedNumber(object.opacity == null ? 1 : object.opacity),
        renderPass: String(object.renderPass || ""),
        blendMode: String(object.blendMode || ""),
        static: Boolean(object.static),
        hasLocalVertices: Boolean(object._modelLocalVertices && object._modelLocalVertices.positions),
      });
    }
    return samples;
  }

  function sceneDebugFighterModelRecords(state, limit) {
    const records = Array.isArray(state && state._modelSkins) ? state._modelSkins : [];
    const max = Math.max(0, Math.floor(sceneNumber(limit, 32)));
    if (!max) {
      return [];
    }
    const samples = [];
    for (let index = 0; index < records.length && samples.length < max; index += 1) {
      const record = records[index];
      const id = String(record && record.id || "");
      if (id.indexOf("fighter-") < 0) {
        continue;
      }
      const objectIDs = Array.isArray(record.objectIDs) ? record.objectIDs : [];
      samples.push({
        id,
        live: Array.isArray(record.live) ? record.live.slice() : [],
        staticModel: Boolean(record.staticModel),
        animation: String(record.animation || ""),
        poseDirty: Boolean(record.poseDirty),
        computedPose: String(record.computedPose || ""),
        computedPoseTarget: String(record.computedPoseTargetID || ""),
        computedPoseAlpha: sceneDebugRoundedNumber(record.computedPoseAlpha == null ? 0 : record.computedPoseAlpha),
        computedMorphObjects: Math.max(0, Math.floor(sceneNumber(record.computedMorphObjects, 0))),
        computedMorphVertices: Math.max(0, Math.floor(sceneNumber(record.computedMorphVertices, 0))),
        objectCount: objectIDs.length,
        model: sceneDebugModelTransformSample(record.model),
        objectIDs: objectIDs.slice(0, 4),
        objects: sceneDebugModelObjectSamples(state, objectIDs, 4),
      });
    }
    return samples;
  }

  function sceneDebugFighterSamples(bundle, state) {
    return {
      renderEntries: sceneDebugFighterRenderEntries(bundle, 32),
      visibleRenderEntries: sceneDebugFighterVisibleRenderEntries(bundle, 32),
      modelRecords: sceneDebugFighterModelRecords(state, 32),
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

  function sceneReadWebGLRendererMetadata(gl) {
    if (!gl || typeof gl.getParameter !== "function") {
      return { vendor: "", renderer: "" };
    }
    let vendor = "";
    let renderer = "";
    try {
      const debugInfo = typeof gl.getExtension === "function"
        ? gl.getExtension("WEBGL_debug_renderer_info")
        : null;
      if (debugInfo) {
        vendor = String(gl.getParameter(debugInfo.UNMASKED_VENDOR_WEBGL) || "");
        renderer = String(gl.getParameter(debugInfo.UNMASKED_RENDERER_WEBGL) || "");
      }
    } catch (_error) {
      vendor = "";
      renderer = "";
    }
    if (!vendor) {
      try {
        vendor = String(gl.getParameter(gl.VENDOR) || "");
      } catch (_error) {
        vendor = "";
      }
    }
    if (!renderer) {
      try {
        renderer = String(gl.getParameter(gl.RENDERER) || "");
      } catch (_error) {
        renderer = "";
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
    window.__gosx = window.__gosx || {};
    if (window.__gosx.scene3dWebGLProbe) {
      return window.__gosx.scene3dWebGLProbe;
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
      window.__gosx.scene3dWebGLProbe = {
        available: Boolean(gl),
        software: sceneWebGLRendererLooksSoftware(metadata),
        vendor: metadata.vendor,
        renderer: metadata.renderer,
      };
    } catch (_error) {
      window.__gosx.scene3dWebGLProbe = { available: false, software: false, vendor: "", renderer: "" };
    }
    return window.__gosx.scene3dWebGLProbe;
  }

  function sceneRequiresWebGL(props) {
    return sceneBool(props && props.requireWebGL, false);
  }

  function sceneForcesWebGL(props) {
    return sceneBool(props && props.forceWebGL, false) ||
      (typeof window !== "undefined" && window.__gosx_scene3d_force_webgl === true);
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
    const webgpuAvail = Boolean(avail.webgpu);
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

  function createSceneWebGLResult(canvas, props, capability, fallbackReason) {
    // Water scenes that land on WebGL (e.g. after a WebGPU device loss /
    // watchdog fallback, or any inline webgl selection) must render via the
    // WebGL2 water runtime, not the generic PBR path which cannot draw the
    // simulation.
    if (sceneFirstWaterEntry(props)) {
      var waterResult = createSceneWaterWebGLResult(canvas, props, fallbackReason);
      if (waterResult) {
        return waterResult;
      }
      // A generic PBR/WebGL renderer cannot draw the water simulation. Return
      // an explicit unsupported result so callers never mistake a valid GL
      // context and a blank generic scene for a working water backend.
      return {
        renderer: null,
        fallbackReason: fallbackReason || "",
        unsupportedReason: "water-webgl2-unavailable",
      };
    }
    if (typeof createScenePBRRendererOrFallback === "function") {
      const useCanvasAlpha = sceneCanvasAlpha(props);
      const gl = typeof canvas.getContext === "function" ? canvas.getContext("webgl2", {
        alpha: useCanvasAlpha,
        premultipliedAlpha: useCanvasAlpha,
        antialias: capability.tier === "full" && !capability.lowPower && !capability.reducedData,
        powerPreference: capability.lowPower || capability.tier === "constrained" ? "low-power" : "high-performance",
      }) : null;
      if (gl) {
        const pbrRenderer = createScenePBRRendererOrFallback(gl, canvas, {});
        if (pbrRenderer) { return { renderer: pbrRenderer, fallbackReason: fallbackReason, degraded: [] }; }
      }
    }
    const webglRenderer = createSceneWebGLRenderer(canvas, {
      antialias: capability.tier === "full" && !capability.lowPower && !capability.reducedData,
      powerPreference: capability.lowPower || capability.tier === "constrained" ? "low-power" : "high-performance",
    });
    return webglRenderer ? { renderer: webglRenderer, fallbackReason: fallbackReason, degraded: [] } : null;
  }

  function sceneFirstWaterEntry(props) {
    var scene = props && props.scene && typeof props.scene === "object" ? props.scene : null;
    var systems = scene && Array.isArray(scene.waterSystems) ? scene.waterSystems : null;
    if (!systems || !systems.length) return null;
    return systems[0] || null;
  }

  // createSceneWaterWebGLResult builds the WebGL2 water runtime for a water
  // scene. It is the single construction point shared by (a) the real A3
  // capability-gate fallback (WebGPU unavailable / lost) and (b) the
  // device-loss recovery path.
  function createSceneWaterWebGLResult(canvas, props, fallbackReason) {
    if (typeof createSceneWaterRendererWebGL !== "function") return null;
    var entry = sceneFirstWaterEntry(props);
    if (!entry) return null;
    var gl = typeof canvas.getContext === "function" ? canvas.getContext("webgl2", {
      alpha: false, premultipliedAlpha: false, antialias: true, depth: true,
      powerPreference: "high-performance",
    }) : null;
    if (!gl) return null;
    var renderer = null;
    try {
      renderer = createSceneWaterRendererWebGL(gl, canvas, entry);
    } catch (e) {
      try { console.warn("[gosx] WebGL water renderer failed:", e); } catch (_e) {}
      return null;
    }
    if (!renderer) return null;
    try {
      if (typeof window !== "undefined") {
        window.__gosx_scene3d_webgl_water = true;
      }
    } catch (_e) {}
    return { renderer: renderer, fallbackReason: fallbackReason || "", degraded: [] };
  }

  // sceneWaterWebGLAutoResult is the real A3 capability-gate selection: for a
  // water scene whose active backend resolves to WebGL (because WebGPU is
  // unavailable, failed the probe, or its device was lost), render the water on
  // the WebGL2 water runtime instead of the generic PBR path (which cannot draw
  // the simulation). Returns null when WebGPU will render the scene — WebGPU
  // stays primary — or when this is not a water scene.
  function sceneWaterWebGLAutoResult(canvas, props, capability) {
    if (!sceneFirstWaterEntry(props)) return null;
    var webgpuAvail = typeof sceneWebGPUAvailable === "function" && sceneWebGPUAvailable();
    var backendCaps = sceneBackendCapsOf(props);
    var verdict = chooseSceneBackend(backendCaps, {
      requireWebGL: sceneRequiresWebGL(props),
      forceWebGL: sceneForcesWebGL(props),
      preferCanvas: sceneBool(props && props.preferCanvas, false),
      preferWebGPU: sceneCapabilityWebGPUPreference(props, capability) === "prefer",
    }, { webgpu: webgpuAvail, webgl: true });
    if (!verdict) return null;
    // WebGPU stays primary: defer to the normal path when WebGPU is the
    // resolved + available backend.
    if (verdict.backend === "webgpu" && webgpuAvail) return null;
    // Only intercept when WebGL2 is the active backend for this water scene.
    if (verdict.backend !== "webgl") return null;
    return createSceneWaterWebGLResult(canvas, props, verdict.fallbackReason || "webgpu-unavailable") || {
      renderer: null,
      fallbackReason: verdict.fallbackReason || "webgpu-unavailable",
      unsupportedReason: "water-webgl2-unavailable",
    };
  }

  function createSceneRenderer(canvas, props, capability) {
    // A3: real capability-gate fallback. For a water scene whose backend
    // resolves to WebGL (WebGPU unavailable / probe-failed / device lost), use
    // the WebGL2 water runtime up front so the registry/inline paths don't grab
    // the generic PBR renderer. WebGPU-capable sessions skip this (returns null)
    // and keep WebGPU primary.
    const autoWater = sceneWaterWebGLAutoResult(canvas, props, capability);
    if (autoWater) {
      return autoWater;
    }
    const registryResult = createSceneRendererFromRegistry(canvas, props, capability);
    if (registryResult) {
      return registryResult;
    }

    const webglPreference = sceneCapabilityWebGLPreference(props, capability);
    const webgpuPreference = sceneCapabilityWebGPUPreference(props, capability);
    const webgpuAvail = typeof sceneWebGPUAvailable === "function" && sceneWebGPUAvailable();
    const prefs = {
      requireWebGL: sceneRequiresWebGL(props),
      forceWebGL: sceneForcesWebGL(props),
      preferCanvas: sceneBool(props && props.preferCanvas, false),
      preferWebGPU: webgpuPreference === "prefer",
    };
    const backendCaps = sceneBackendCapsOf(props);
    const verdict = chooseSceneBackend(backendCaps, prefs, { webgpu: webgpuAvail, webgl: true });
    if (verdict) {
      if (verdict.backend === "webgpu" && webgpuAvail && typeof createSceneWebGPURendererOrFallback === "function") {
        const gpuRenderer = createSceneWebGPURendererOrFallback(canvas, sceneWebGPUOptions(props, capability));
        if (gpuRenderer) {
          return { renderer: gpuRenderer, fallbackReason: verdict.fallbackReason || "", degraded: verdict.degraded || [] };
        }
      }
      if (verdict.backend === "webgl" || (verdict.backend === "webgpu" && !webgpuAvail)) {
        const fallback = verdict.backend === "webgpu" ? "webgpu-unavailable" : (verdict.fallbackReason || "");
        return createSceneWebGLResult(canvas, props, capability, fallback);
      }
      if (verdict.backend === "canvas2d") {
        if (sceneRequiresWebGL(props)) { return null; }
        const ctx2d = typeof canvas.getContext === "function" ? canvas.getContext("2d") : null;
        if (!ctx2d) { return null; }
        return { renderer: createSceneCanvasRenderer(ctx2d, canvas), fallbackReason: sceneRendererFallbackReason(props, capability, "canvas"), degraded: [] };
      }
      return null;
    }
    const webgpuFeatureGap = sceneNeedsWebGLForWebGPUCoverage(props);
    if (webgpuPreference === "prefer" && !webgpuFeatureGap && webgpuAvail && typeof createSceneWebGPURendererOrFallback === "function") {
      var gpuRenderer = createSceneWebGPURendererOrFallback(canvas, sceneWebGPUOptions(props, capability));
      if (gpuRenderer) {
        return {
          renderer: gpuRenderer,
          fallbackReason: "",
        };
      }
    }
    if (webglPreference === "prefer" || webglPreference === "force") {
      const webglResult = createSceneWebGLResult(canvas, props, capability, webgpuFeatureGap ? "webgpu-feature-gap" : "");
      if (webglResult) { return webglResult; }
    }
    if (sceneRequiresWebGL(props)) {
      return null;
    }
    const ctx2d = typeof canvas.getContext === "function" ? canvas.getContext("2d") : null;
    if (!ctx2d) {
      return null;
    }
    return {
      renderer: createSceneCanvasRenderer(ctx2d, canvas),
      fallbackReason: sceneRendererFallbackReason(props, capability, "canvas"),
    };
  }

  function createSceneRendererFromRegistry(canvas, props, capability) {
    if (typeof sceneBackendRegistry === "undefined" || !sceneBackendRegistry || typeof sceneBackendRegistry.candidates !== "function") {
      return null;
    }
    const webglPreference = sceneCapabilityWebGLPreference(props, capability);
    const webgpuPreference = sceneCapabilityWebGPUPreference(props, capability);
    const requireWebGL = sceneRequiresWebGL(props);
    const webgpuFeatureGap = sceneNeedsWebGLForWebGPUCoverage(props);
    const backendCaps = sceneBackendCapsOf(props);
    const webgpuAvail = typeof sceneWebGPUAvailable === "function" && sceneWebGPUAvailable();
    const verdict = chooseSceneBackend(backendCaps, {
      requireWebGL, forceWebGL: sceneForcesWebGL(props),
      preferCanvas: sceneBool(props && props.preferCanvas, false), preferWebGPU: webgpuPreference === "prefer",
    }, { webgpu: webgpuAvail, webgl: true });
    const preferWebGPU = verdict ? verdict.backend === "webgpu" : (webgpuPreference === "prefer" && !webgpuFeatureGap);
    const verdictFallback = verdict ? verdict.fallbackReason : "";
    const allowWebGL = (webglPreference === "prefer" || webglPreference === "force")
      && sceneBackendCapsAllowsKind(backendCaps, "webgl");
    const request = {
      props,
      capability,
      webgpu: preferWebGPU && sceneBackendCapsAllowsKind(backendCaps, "webgpu"),
      webgl: allowWebGL,
      webgl2: allowWebGL,
      canvas2d: !requireWebGL && sceneBackendCapsAllowsKind(backendCaps, "canvas2d"),
      preferWebGPU: preferWebGPU && sceneBackendCapsAllowsKind(backendCaps, "webgpu"),
      forceWebGL: webglPreference === "force" && sceneBackendCapsAllowsKind(backendCaps, "webgl"),
    };
    const candidates = sceneBackendRegistry.candidates(request);
    for (const entry of candidates) {
      if (!entry || typeof entry.create !== "function") {
        continue;
      }
      try {
        if (typeof window !== "undefined") {
          window.__gosx_scene3d_backend_attempts = window.__gosx_scene3d_backend_attempts || [];
          window.__gosx_scene3d_backend_attempts.push({
            kind: String(entry.kind || ""),
            webgpuAvailable: webgpuAvail,
            preferWebGPU: preferWebGPU,
            webglPreference: webglPreference,
            webgpuFactoryErrorBefore: String(window.__gosx_scene3d_webgpu_factory_error || ""),
          });
        }
      } catch (_err) {}
      const renderer = entry.create(canvas, props, capability);
      try {
        if (typeof window !== "undefined" && Array.isArray(window.__gosx_scene3d_backend_attempts)) {
          const last = window.__gosx_scene3d_backend_attempts[window.__gosx_scene3d_backend_attempts.length - 1];
          if (last) {
            last.result = renderer ? String(renderer.kind || renderer.type || "renderer") : "";
            last.webgpuFactoryErrorAfter = String(window.__gosx_scene3d_webgpu_factory_error || "");
          }
        }
      } catch (_err) {}
      if (renderer) {
        const isCanvas = entry.kind === "canvas2d" || renderer.kind === "canvas";
        let fallbackReason = isCanvas
          ? sceneRendererFallbackReason(props, capability, "canvas")
          : (verdictFallback || (webgpuFeatureGap && renderer.kind === "webgl" ? "webgpu-feature-gap" : ""));
        return {
          renderer,
          fallbackReason,
          degraded: verdict && renderer.kind === "webgpu" ? (verdict.degraded || []) : [],
        };
      }
    }
    return null;
  }

  const sceneModelAssetCache = new Map();

  function resolveSceneModelAssetURL(baseSrc, value) {
    const raw = typeof value === "string" ? value.trim() : "";
    if (!raw) {
      return "";
    }
    try {
      const baseURL = new URL(baseSrc || "", window.location.href).toString();
      return new URL(raw, baseURL).toString();
    } catch (_error) {
      return raw;
    }
  }

  function resolveSceneModelObjectURLs(baseSrc, rawObject) {
    if (!rawObject || typeof rawObject !== "object") {
      return rawObject;
    }
    const resolved = Object.assign({}, rawObject);
    if (typeof resolved.texture === "string" && resolved.texture.trim()) {
      resolved.texture = resolveSceneModelAssetURL(baseSrc, resolved.texture);
    }
    if (resolved.material && typeof resolved.material === "object") {
      const material = Object.assign({}, resolved.material);
      if (typeof material.texture === "string" && material.texture.trim()) {
        material.texture = resolveSceneModelAssetURL(baseSrc, material.texture);
      }
      resolved.material = material;
    }
    return resolved;
  }

  function sceneModelTransformPoint(point, model) {
    const local = point && typeof point === "object" ? point : { x: 0, y: 0, z: 0 };
    const scaleX = sceneNumber(model && model.scaleX, 1);
    const scaleY = sceneNumber(model && model.scaleY, 1);
    const scaleZ = sceneNumber(model && model.scaleZ, 1);
    const rotated = sceneRotatePoint(
      {
        x: local.x * scaleX,
        y: local.y * scaleY,
        z: local.z * scaleZ,
      },
      sceneNumber(model && model.rotationX, 0),
      sceneNumber(model && model.rotationY, 0),
      sceneNumber(model && model.rotationZ, 0),
    );
    return {
      x: rotated.x + sceneNumber(model && model.x, 0),
      y: rotated.y + sceneNumber(model && model.y, 0),
      z: rotated.z + sceneNumber(model && model.z, 0),
    };
  }

  function sceneModelTransformVector(point, model) {
    const local = point && typeof point === "object" ? point : { x: 0, y: 0, z: 0 };
    return sceneRotatePoint(
      {
        x: local.x * sceneNumber(model && model.scaleX, 1),
        y: local.y * sceneNumber(model && model.scaleY, 1),
        z: local.z * sceneNumber(model && model.scaleZ, 1),
      },
      sceneNumber(model && model.rotationX, 0),
      sceneNumber(model && model.rotationY, 0),
      sceneNumber(model && model.rotationZ, 0),
    );
  }

  function sceneModelMaxScale(model) {
    return Math.max(
      Math.abs(sceneNumber(model && model.scaleX, 1)),
      Math.abs(sceneNumber(model && model.scaleY, 1)),
      Math.abs(sceneNumber(model && model.scaleZ, 1)),
    );
  }

  function sceneModelEffectivelyHidden(model) {
    return Boolean(model && model.visible === false)
      || sceneModelMaxScale(model) <= 0.0015
      || sceneNumber(model && model.opacity, 1) <= 0.0001;
  }

  function sceneApplyModelObjectHiddenState(object, model) {
    if (!object) {
      return;
    }
    object._modelHidden = sceneModelEffectivelyHidden(model);
  }

  function sceneModelRotateDirection(point, model) {
    return sceneRotatePoint(
      point && typeof point === "object" ? point : { x: 0, y: 0, z: 0 },
      sceneNumber(model && model.rotationX, 0),
      sceneNumber(model && model.rotationY, 0),
      sceneNumber(model && model.rotationZ, 0),
    );
  }

  function sceneModelTransformMatrix(model) {
    const rx = sceneNumber(model && model.rotationX, 0);
    const ry = sceneNumber(model && model.rotationY, 0);
    const rz = sceneNumber(model && model.rotationZ, 0);
    const basisX = sceneRotatePoint({ x: sceneNumber(model && model.scaleX, 1), y: 0, z: 0 }, rx, ry, rz);
    const basisY = sceneRotatePoint({ x: 0, y: sceneNumber(model && model.scaleY, 1), z: 0 }, rx, ry, rz);
    const basisZ = sceneRotatePoint({ x: 0, y: 0, z: sceneNumber(model && model.scaleZ, 1) }, rx, ry, rz);
    return new Float32Array([
      basisX.x, basisX.y, basisX.z, 0,
      basisY.x, basisY.y, basisY.z, 0,
      basisZ.x, basisZ.y, basisZ.z, 0,
      sceneNumber(model && model.x, 0), sceneNumber(model && model.y, 0), sceneNumber(model && model.z, 0), 1,
    ]);
  }

  function sceneModelFitMode(value) {
    switch (String(value || "").trim().toLowerCase()) {
      case "bounds":
      case "bound":
      case "contain":
      case "fit":
      case "max-dimension":
        return "contain";
      default:
        return "";
    }
  }

  function sceneModelFitAlign(value) {
    switch (String(value || "").trim().toLowerCase()) {
      case "none":
        return "none";
      case "bottom":
      case "center-bottom":
        return "center-bottom";
      case "center-min-y":
      case "center-min":
      case "water-center-min-y":
        return "center-min-y";
      case "center":
      default:
        return "center";
    }
  }

  function sceneModelWithAssetFit(model, asset) {
    const mode = sceneModelFitMode(model && model.fit);
    const target = Math.max(0, sceneNumber(model && model.bounds, 0));
    const bounds = sceneModelNormalizeAssetBounds(asset && asset.bounds);
    if (!mode || target <= 0 || !bounds || !(bounds.maxDimension > 0)) {
      return model;
    }
    const fitScale = target / bounds.maxDimension;
    if (!Number.isFinite(fitScale) || fitScale <= 0) {
      return model;
    }
    const align = sceneModelFitAlign(model && model.fitAlign);
    const offset = { x: 0, y: 0, z: 0 };
    if (align !== "none") {
      offset.x = -bounds.centerX * fitScale;
      offset.z = -bounds.centerZ * fitScale;
      if (align === "center-bottom") {
        offset.y = -bounds.minY * fitScale;
      } else {
        offset.y = -bounds.centerY * fitScale;
        if (align === "center-min-y") {
          offset.y -= bounds.minY * fitScale;
        }
      }
    }
    const translated = sceneModelTransformVector(offset, model);
    const fitted = Object.assign({}, model, {
      x: sceneNumber(model && model.x, 0) + translated.x,
      y: sceneNumber(model && model.y, 0) + translated.y,
      z: sceneNumber(model && model.z, 0) + translated.z,
      scaleX: sceneNumber(model && model.scaleX, 1) * fitScale,
      scaleY: sceneNumber(model && model.scaleY, 1) * fitScale,
      scaleZ: sceneNumber(model && model.scaleZ, 1) * fitScale,
    });
    fitted._fitScale = fitScale;
    fitted._fitBounds = bounds;
    fitted._fitAlign = align;
    return fitted;
  }

  function sceneModelIdentityBindMatrices(jointCount) {
    const matrices = new Float32Array(Math.max(0, jointCount) * 16);
    for (let index = 0; index < jointCount; index += 1) {
      matrices[index * 16] = 1;
      matrices[index * 16 + 5] = 1;
      matrices[index * 16 + 10] = 1;
      matrices[index * 16 + 15] = 1;
    }
    return matrices;
  }

  function sceneCloneModelSkin(skin) {
    if (!skin || typeof skin !== "object") {
      return null;
    }
    const joints = Array.isArray(skin.joints) ? skin.joints.slice() : [];
    if (!joints.length || joints.length > 64) {
      return null;
    }
    let inverseBindMatrices = skin.inverseBindMatrices instanceof Float32Array
      ? new Float32Array(skin.inverseBindMatrices)
      : sceneTypedFloatArray(skin.inverseBindMatrices);
    if (inverseBindMatrices.length < joints.length * 16) {
      inverseBindMatrices = sceneModelIdentityBindMatrices(joints.length);
    } else if (inverseBindMatrices.length !== joints.length * 16) {
      inverseBindMatrices = inverseBindMatrices.slice(0, joints.length * 16);
    }
    return {
      index: typeof skin.index === "number" ? skin.index : null,
      name: typeof skin.name === "string" ? skin.name : "",
      joints,
      inverseBindMatrices,
      skeleton: skin.skeleton != null ? skin.skeleton : null,
    };
  }

  function sceneCloneModelSkins(skins) {
    return Array.isArray(skins) ? skins.map(sceneCloneModelSkin) : [];
  }

  function sceneCloneModelAnimations(animations) {
    if (!Array.isArray(animations)) {
      return [];
    }
    return animations.map(function(clip, index) {
      const source = clip && typeof clip === "object" ? clip : {};
      const channels = Array.isArray(source.channels) ? source.channels.map(function(channel) {
        const ch = channel && typeof channel === "object" ? channel : {};
        return {
          targetID: ch.targetID != null ? ch.targetID : ch.targetNode,
          targetNode: ch.targetNode != null ? ch.targetNode : ch.targetID,
          property: typeof ch.property === "string" ? ch.property : "translation",
          interpolation: typeof ch.interpolation === "string" && ch.interpolation ? ch.interpolation : "LINEAR",
          times: ch.times instanceof Float32Array ? new Float32Array(ch.times) : sceneTypedFloatArray(ch.times),
          values: ch.values instanceof Float32Array ? new Float32Array(ch.values) : sceneTypedFloatArray(ch.values),
        };
      }) : [];
      return {
        name: typeof source.name === "string" && source.name ? source.name : ("clip-" + index),
        duration: sceneNumber(source.duration, 0),
        channels,
      };
    });
  }

  function sceneModelMaterialOverrideSource(model) {
    if (!model || typeof model !== "object") {
      return null;
    }
    if (model.materialOverride && typeof model.materialOverride === "object") {
      return model.materialOverride;
    }
    const keys = ["material", "materialKind", "color", "texture", "opacity", "emissive", "blendMode", "renderPass", "wireframe", "roughness", "metalness", "clearcoat", "sheen", "transmission", "iridescence", "anisotropy", "customVertex", "customFragment", "customVertexWGSL", "customFragmentWGSL", "customUniforms", "shaderBackend", "shaderLayout", "shaderSource", "shaderSourceFiles"];
    for (let index = 0; index < keys.length; index += 1) {
      if (Object.prototype.hasOwnProperty.call(model, keys[index])) {
        return model;
      }
    }
    return null;
  }

  function sceneAssignMaterialOverride(next, material, sourceKey, targetKey, override) {
    if (!override || !Object.prototype.hasOwnProperty.call(override, sourceKey)) {
      return;
    }
    const key = targetKey || sourceKey;
    next[key] = override[sourceKey];
    if (material) {
      material[key] = override[sourceKey];
    }
  }

  function sceneApplyMaterialOverride(raw, model) {
    const override = sceneModelMaterialOverrideSource(model);
    if (!override) {
      return raw && typeof raw === "object" ? Object.assign({}, raw) : {};
    }
    const next = raw && typeof raw === "object" ? Object.assign({}, raw) : {};
    const material = next.material && typeof next.material === "object"
      ? Object.assign({}, next.material)
      : null;
    const namedMaterialOverride = typeof override.material === "string" && override.material.trim();
    if (typeof override.materialKind === "string" && override.materialKind) {
      next.materialKind = override.materialKind;
      if (typeof next.material === "string") {
        next.material = override.materialKind;
      }
      if (material) {
        material.kind = override.materialKind;
      }
    }
    if (namedMaterialOverride) {
      next.material = override.material.trim();
    }
    sceneAssignMaterialOverride(next, material, "color", "color", override);
    sceneAssignMaterialOverride(next, material, "texture", "texture", override);
    sceneAssignMaterialOverride(next, material, "opacity", "opacity", override);
    sceneAssignMaterialOverride(next, material, "emissive", "emissive", override);
    sceneAssignMaterialOverride(next, material, "blendMode", "blendMode", override);
    sceneAssignMaterialOverride(next, material, "renderPass", "renderPass", override);
    sceneAssignMaterialOverride(next, material, "wireframe", "wireframe", override);
    sceneAssignMaterialOverride(next, material, "roughness", "roughness", override);
    sceneAssignMaterialOverride(next, material, "metalness", "metalness", override);
    sceneAssignMaterialOverride(next, material, "clearcoat", "clearcoat", override);
    sceneAssignMaterialOverride(next, material, "sheen", "sheen", override);
    sceneAssignMaterialOverride(next, material, "transmission", "transmission", override);
    sceneAssignMaterialOverride(next, material, "iridescence", "iridescence", override);
    sceneAssignMaterialOverride(next, material, "anisotropy", "anisotropy", override);
    sceneAssignMaterialOverride(next, material, "customVertex", "customVertex", override);
    sceneAssignMaterialOverride(next, material, "customFragment", "customFragment", override);
    sceneAssignMaterialOverride(next, material, "customVertexWGSL", "customVertexWGSL", override);
    sceneAssignMaterialOverride(next, material, "customFragmentWGSL", "customFragmentWGSL", override);
    sceneAssignMaterialOverride(next, material, "customUniforms", "customUniforms", override);
    sceneAssignMaterialOverride(next, material, "shaderBackend", "shaderBackend", override);
    sceneAssignMaterialOverride(next, material, "shaderLayout", "shaderLayout", override);
    sceneAssignMaterialOverride(next, material, "shaderSource", "shaderSource", override);
    sceneAssignMaterialOverride(next, material, "shaderSourceFiles", "shaderSourceFiles", override);
    if (material && !namedMaterialOverride) {
      next.material = material;
    }
    return next;
  }

  function sceneApplyModelLOD(instanced, model) {
    if (!instanced || !model || !model.lodGroup) {
      return;
    }
    instanced.lodGroup = model.lodGroup;
    instanced.lodLevel = model.lodLevel;
    instanced.lodMinDistance = model.lodMinDistance;
    instanced.lodMaxDistance = model.lodMaxDistance;
  }

  function sceneApplyModelRenderFlags(instanced, model) {
    if (!instanced || !model) {
      return;
    }
    if (typeof model.castShadow === "boolean") {
      instanced.castShadow = model.castShadow;
    }
    if (typeof model.receiveShadow === "boolean") {
      instanced.receiveShadow = model.receiveShadow;
    }
  }

  function sceneApplyModelMaterialName(instanced, model) {
    if (!instanced || !model || typeof model.material !== "string" || !model.material.trim()) {
      return;
    }
    instanced.material = model.material.trim();
  }

  function sceneModelPrimitiveObject(object, model, prefix) {
    const instanced = Object.assign({}, object, {
      id: prefix + "/" + (object.id || "object"),
      x: 0,
      y: 0,
      z: 0,
      rotationX: sceneNumber(object.rotationX, 0) + sceneNumber(model && model.rotationX, 0),
      rotationY: sceneNumber(object.rotationY, 0) + sceneNumber(model && model.rotationY, 0),
      rotationZ: sceneNumber(object.rotationZ, 0) + sceneNumber(model && model.rotationZ, 0),
    });
    const positioned = sceneModelTransformPoint({ x: object.x, y: object.y, z: object.z }, model);
    instanced.x = positioned.x;
    instanced.y = positioned.y;
    instanced.z = positioned.z;
    const scaleX = Math.abs(sceneNumber(model && model.scaleX, 1));
    const scaleY = Math.abs(sceneNumber(model && model.scaleY, 1));
    const scaleZ = Math.abs(sceneNumber(model && model.scaleZ, 1));
    switch (object.kind) {
      case "cube":
        if (Math.abs(scaleX - scaleY) > 0.0001 || Math.abs(scaleX - scaleZ) > 0.0001) {
          instanced.kind = "box";
          instanced.width = sceneNumber(object.size, 1.2) * scaleX;
          instanced.height = sceneNumber(object.size, 1.2) * scaleY;
          instanced.depth = sceneNumber(object.size, 1.2) * scaleZ;
        } else {
          instanced.size = sceneNumber(object.size, 1.2) * scaleX;
        }
        break;
      case "sphere":
        instanced.radius = sceneNumber(object.radius, sceneNumber(object.size, 1.2) / 2) * Math.max(scaleX, scaleY, scaleZ);
        break;
      default:
        instanced.width = sceneNumber(object.width, sceneNumber(object.size, 1.2)) * scaleX;
        instanced.height = sceneNumber(object.height, sceneNumber(object.size, 1.2)) * scaleY;
        instanced.depth = sceneNumber(object.depth, sceneNumber(object.size, 1.2)) * scaleZ;
        break;
    }
    if (model && model.static !== null) {
      instanced.static = Boolean(model.static);
    }
    if (model && typeof model.pickable === "boolean") {
      instanced.pickable = model.pickable;
    }
    sceneApplyModelObjectHiddenState(instanced, model);
    sceneApplyModelMaterialName(instanced, model);
    sceneApplyModelRenderFlags(instanced, model);
    sceneApplyModelLOD(instanced, model);
    return normalizeSceneObject(instanced, prefix);
  }

  function sceneModelLineObject(object, model, prefix) {
    const scaleX = sceneNumber(model && model.scaleX, 1);
    const scaleY = sceneNumber(model && model.scaleY, 1);
    const scaleZ = sceneNumber(model && model.scaleZ, 1);
    const scaled = sceneScaleModelLinePoints(object.points, scaleX, scaleY, scaleZ);
    const positioned = sceneModelTransformPoint({ x: object.x, y: object.y, z: object.z }, model);
    const instanced = Object.assign({}, object, {
      id: prefix + "/" + (object.id || "object"),
      points: scaled,
      lineSegments: sceneCloneModelLineSegments(object.lineSegments),
      x: positioned.x,
      y: positioned.y,
      z: positioned.z,
      rotationX: sceneNumber(object.rotationX, 0) + sceneNumber(model && model.rotationX, 0),
      rotationY: sceneNumber(object.rotationY, 0) + sceneNumber(model && model.rotationY, 0),
      rotationZ: sceneNumber(object.rotationZ, 0) + sceneNumber(model && model.rotationZ, 0),
    });
    if (model && model.static !== null) {
      instanced.static = Boolean(model.static);
    }
    if (model && typeof model.pickable === "boolean") {
      instanced.pickable = model.pickable;
    }
    sceneApplyModelObjectHiddenState(instanced, model);
    sceneApplyModelMaterialName(instanced, model);
    sceneApplyModelRenderFlags(instanced, model);
    sceneApplyModelLOD(instanced, model);
    return normalizeSceneObject(instanced, prefix);
  }

  function sceneScaleModelLinePoints(points, scaleX, scaleY, scaleZ) {
    return Array.isArray(points) ? points.map(function(point) {
      return {
        x: sceneNumber(point && point.x, 0) * scaleX,
        y: sceneNumber(point && point.y, 0) * scaleY,
        z: sceneNumber(point && point.z, 0) * scaleZ,
      };
    }) : [];
  }

  function sceneCloneModelLineSegments(segments) {
    return Array.isArray(segments) ? segments.map(function(pair) {
      return Array.isArray(pair) ? pair.slice(0, 2) : pair;
    }) : [];
  }

  function sceneScaleModelPointPositions(positions, scaleX, scaleY, scaleZ) {
    const source = positions instanceof Float32Array ? positions : sceneTypedFloatArray(positions);
    if (!source.length) {
      return source;
    }
    if (Math.abs(scaleX - 1) < 0.000001 && Math.abs(scaleY - 1) < 0.000001 && Math.abs(scaleZ - 1) < 0.000001) {
      return source;
    }
    const scaled = new Float32Array(source.length);
    for (let i = 0; i + 2 < source.length; i += 3) {
      scaled[i] = source[i] * scaleX;
      scaled[i + 1] = source[i + 1] * scaleY;
      scaled[i + 2] = source[i + 2] * scaleZ;
    }
    return scaled;
  }

  function sceneApplyModelPointOverride(point, model) {
    const override = Object.assign({}, point);
    if (!model || typeof model !== "object") {
      return override;
    }
    if (typeof model.material === "string" && model.material.trim()) {
      override.material = model.material.trim();
    }
    if (typeof model.color === "string" && model.color) {
      override.color = model.color;
    }
    if (typeof model.style === "string" && model.style) {
      override.style = model.style;
    }
    if (model.size != null) {
      override.size = model.size;
    }
    if (model.opacity != null) {
      override.opacity = model.opacity;
    }
    if (typeof model.blendMode === "string" && model.blendMode) {
      override.blendMode = model.blendMode;
    }
    if (model.depthWrite != null) {
      override.depthWrite = model.depthWrite;
    }
    if (model.attenuation != null) {
      override.attenuation = model.attenuation;
    }
    return override;
  }

  function sceneInstantiateModelPointsEntry(rawPoint, model, prefix, index) {
    const source = sceneApplyModelPointOverride(rawPoint, model);
    const normalized = normalizeScenePointsEntry(source, index, null);
    const scaleX = sceneNumber(model && model.scaleX, 1);
    const scaleY = sceneNumber(model && model.scaleY, 1);
    const scaleZ = sceneNumber(model && model.scaleZ, 1);
    const positions = sceneScaleModelPointPositions(normalized._cachedPos || normalized.positions, scaleX, scaleY, scaleZ);
    const positioned = sceneModelTransformPoint({ x: normalized.x, y: normalized.y, z: normalized.z }, model);
    const instanced = Object.assign({}, normalized, {
      id: prefix + "/" + normalized.id,
      positions,
      x: positioned.x,
      y: positioned.y,
      z: positioned.z,
      rotationX: sceneNumber(normalized.rotationX, 0) + sceneNumber(model && model.rotationX, 0),
      rotationY: sceneNumber(normalized.rotationY, 0) + sceneNumber(model && model.rotationY, 0),
      rotationZ: sceneNumber(normalized.rotationZ, 0) + sceneNumber(model && model.rotationZ, 0),
    });
    if (positions instanceof Float32Array) {
      instanced._cachedPos = positions;
    }
    if (normalized._cachedSizes) {
      instanced._cachedSizes = normalized._cachedSizes;
    }
    if (normalized._cachedColors) {
      instanced._cachedColors = normalized._cachedColors;
    }
    return normalizeScenePointsEntry(instanced, instanced.id, normalized);
  }

  function sceneInstantiateModelObject(rawObject, model, prefix, index, skinInstances) {
    const source = sceneApplyMaterialOverride(rawObject, model);
    if (skinInstances && source && source.skinIndex != null && skinInstances[source.skinIndex]) {
      source.skin = skinInstances[source.skinIndex];
    }
    const normalized = normalizeSceneObject(source, index);
    if (normalized.vertices && normalized.vertices.positions && normalized.vertices.count > 0) {
      return sceneModelMeshObject(normalized, model, prefix);
    }
    if (normalized.kind === "lines") {
      return sceneModelLineObject(normalized, model, prefix);
    }
    return sceneModelPrimitiveObject(normalized, model, prefix);
  }

  function sceneModelTransformMeshFloats(values, tupleSize, mapper) {
    const source = values instanceof Float32Array ? values : sceneTypedFloatArray(values);
    const typed = new Float32Array(source.length);
    const safeTupleSize = Math.max(1, Math.floor(sceneNumber(tupleSize, 1)));
    for (let index = 0; index + safeTupleSize - 1 < source.length; index += safeTupleSize) {
      const mapped = mapper(
        sceneNumber(source[index], 0),
        sceneNumber(source[index + 1], 0),
        sceneNumber(source[index + 2], 0),
        safeTupleSize > 3 ? sceneNumber(source[index + 3], 1) : undefined
      );
      typed[index] = sceneNumber(mapped && mapped.x, 0);
      typed[index + 1] = sceneNumber(mapped && mapped.y, 0);
      typed[index + 2] = sceneNumber(mapped && mapped.z, 0);
      if (safeTupleSize > 3) {
        typed[index + 3] = sceneNumber(mapped && mapped.w, 1);
      }
    }
    return typed;
  }

  function sceneModelMeshObject(object, model, prefix) {
    const vertices = object && object.vertices && typeof object.vertices === "object" ? object.vertices : null;
    if (!vertices || !vertices.positions || !vertices.count) {
      return null;
    }
    const instanced = Object.assign({}, object, {
      id: prefix + "/" + (object.id || "object"),
      x: 0,
      y: 0,
      z: 0,
      rotationX: 0,
      rotationY: 0,
      rotationZ: 0,
      spinX: 0,
      spinY: 0,
      spinZ: 0,
      shiftX: 0,
      shiftY: 0,
      shiftZ: 0,
      driftSpeed: 0,
      driftPhase: 0,
    });
    const hasSkin = instanced.skin && typeof instanced.skin === "object";
    if (hasSkin) {
      instanced.vertices = {
        count: Math.max(0, Math.floor(sceneNumber(vertices.count, 0))),
        positions: vertices.positions instanceof Float32Array ? new Float32Array(vertices.positions) : sceneTypedFloatArray(vertices.positions),
        normals: vertices.normals instanceof Float32Array ? new Float32Array(vertices.normals) : sceneTypedFloatArray(vertices.normals),
        uvs: vertices.uvs instanceof Float32Array ? new Float32Array(vertices.uvs) : sceneTypedFloatArray(vertices.uvs),
        tangents: vertices.tangents instanceof Float32Array ? new Float32Array(vertices.tangents) : sceneTypedFloatArray(vertices.tangents),
        joints: vertices.joints instanceof Float32Array ? new Float32Array(vertices.joints) : sceneTypedFloatArray(vertices.joints),
        weights: vertices.weights instanceof Float32Array ? new Float32Array(vertices.weights) : sceneTypedFloatArray(vertices.weights),
      };
    } else {
      instanced.vertices = {
        count: Math.max(0, Math.floor(sceneNumber(vertices.count, 0))),
        positions: sceneModelTransformMeshFloats(vertices.positions, 3, function(x, y, z) {
          return sceneModelTransformPoint({ x: x, y: y, z: z }, model);
        }),
        normals: sceneModelTransformMeshFloats(vertices.normals, 3, function(x, y, z) {
          return sceneNormalizeDirection(sceneModelTransformVector({ x: x, y: y, z: z }, model));
        }),
        uvs: vertices.uvs instanceof Float32Array ? new Float32Array(vertices.uvs) : sceneTypedFloatArray(vertices.uvs),
        tangents: sceneModelTransformMeshFloats(vertices.tangents, 4, function(x, y, z, w) {
          const rotated = sceneNormalizeDirection(sceneModelTransformVector({ x: x, y: y, z: z }, model));
          return { x: rotated.x, y: rotated.y, z: rotated.z, w: sceneNumber(w, 1) };
        }),
        joints: vertices.joints instanceof Float32Array ? new Float32Array(vertices.joints) : sceneTypedFloatArray(vertices.joints),
        weights: vertices.weights instanceof Float32Array ? new Float32Array(vertices.weights) : sceneTypedFloatArray(vertices.weights),
      };
    }
    if (model && model.static !== null) {
      instanced.static = Boolean(model.static);
    }
    if (model && Array.isArray(model._live) && model._live.length > 0) {
      instanced.static = false;
    }
    if (hasSkin && model && model.animation) {
      instanced.static = false;
    }
    if (model && typeof model.pickable === "boolean") {
      instanced.pickable = model.pickable;
    }
    sceneApplyModelObjectHiddenState(instanced, model);
    sceneApplyModelMaterialName(instanced, model);
    sceneApplyModelRenderFlags(instanced, model);
    sceneApplyModelLOD(instanced, model);
    const normalized = normalizeSceneObject(instanced, prefix);
    sceneApplyModelMaterialName(normalized, model);
    if (!hasSkin && normalized && normalized.vertices) {
      normalized._modelLocalVertices = {
        positions: vertices.positions instanceof Float32Array ? new Float32Array(vertices.positions) : sceneTypedFloatArray(vertices.positions),
        normals: vertices.normals instanceof Float32Array ? new Float32Array(vertices.normals) : sceneTypedFloatArray(vertices.normals),
        uvs: vertices.uvs instanceof Float32Array ? new Float32Array(vertices.uvs) : sceneTypedFloatArray(vertices.uvs),
        tangents: vertices.tangents instanceof Float32Array ? new Float32Array(vertices.tangents) : sceneTypedFloatArray(vertices.tangents),
        count: Math.max(0, Math.floor(sceneNumber(vertices.count, 0))),
      };
    }
    return normalized;
  }

  function sceneSkinnedModelLocalBounds(vertices) {
    if (!vertices || !vertices.positions || !vertices.count) {
      return null;
    }
    const cached = vertices._skinnedLocalBounds;
    if (cached) {
      return cached;
    }
    const positions = vertices.positions instanceof Float32Array ? vertices.positions : sceneTypedFloatArray(vertices.positions);
    let minX = Infinity;
    let minY = Infinity;
    let minZ = Infinity;
    let maxX = -Infinity;
    let maxY = -Infinity;
    let maxZ = -Infinity;
    const limit = Math.min(positions.length, Math.max(0, Math.floor(sceneNumber(vertices.count, 0))) * 3);
    for (let index = 0; index + 2 < limit; index += 3) {
      const x = positions[index];
      const y = positions[index + 1];
      const z = positions[index + 2];
      if (x < minX) minX = x;
      if (y < minY) minY = y;
      if (z < minZ) minZ = z;
      if (x > maxX) maxX = x;
      if (y > maxY) maxY = y;
      if (z > maxZ) maxZ = z;
    }
    const bounds = Number.isFinite(minX)
      ? { minX, minY, minZ, maxX, maxY, maxZ }
      : { minX: -1, minY: -1, minZ: -1, maxX: 1, maxY: 1, maxZ: 1 };
    vertices._skinnedLocalBounds = bounds;
    return bounds;
  }

  function sceneInstantiateModelLabel(rawLabel, model, prefix, index) {
    const normalized = normalizeSceneLabel(rawLabel, index);
    const position = sceneModelTransformPoint({ x: normalized.x, y: normalized.y, z: normalized.z }, model);
    return Object.assign({}, normalized, {
      id: prefix + "/" + normalized.id,
      x: position.x,
      y: position.y,
      z: position.z,
    });
  }

  function sceneInstantiateModelLight(rawLight, model, prefix, index) {
    const normalized = normalizeSceneLight(rawLight, index, null);
    if (!normalized) {
      return null;
    }
    const next = Object.assign({}, normalized, {
      id: prefix + "/" + normalized.id,
    });
    if (next.kind === "directional") {
      const rotated = sceneModelRotateDirection({
        x: next.directionX,
        y: next.directionY,
        z: next.directionZ,
      }, model);
      next.directionX = rotated.x;
      next.directionY = rotated.y;
      next.directionZ = rotated.z;
      // Re-stamp the light sub-hash since directionX/Y/Z were just mutated
      // in place after normalizeSceneLight's original stamp. Without this
      // scenePBRLightsHash would read a stale _lightHash and miss content
      // changes for model-rotated directional lights.
      if (typeof hashLightContent === "function") {
        next._lightHash = hashLightContent(next);
      }
      return next;
    }
    const position = sceneModelTransformPoint({ x: next.x, y: next.y, z: next.z }, model);
    next.x = position.x;
    next.y = position.y;
    next.z = position.z;
    if (next.kind === "point") {
      next.range = sceneNumber(next.range, 0) * Math.max(
        Math.abs(sceneNumber(model && model.scaleX, 1)),
        Math.abs(sceneNumber(model && model.scaleY, 1)),
        Math.abs(sceneNumber(model && model.scaleZ, 1)),
      );
    }
    // Re-stamp after the above in-place x/y/z (and range for points)
    // writes — see the directional branch comment above.
    if (typeof hashLightContent === "function") {
      next._lightHash = hashLightContent(next);
    }
    return next;
  }

  function sceneInstantiateModelSprite(rawSprite, model, prefix, index) {
    const normalized = normalizeSceneSprite(rawSprite, index);
    if (!normalized || !normalized.src) {
      return null;
    }
    const position = sceneModelTransformPoint({ x: normalized.x, y: normalized.y, z: normalized.z }, model);
    const shift = sceneModelTransformVector({ x: normalized.shiftX, y: normalized.shiftY, z: normalized.shiftZ }, model);
    const modelScale = sceneModelMaxScale(model);
    return Object.assign({}, normalized, {
      id: prefix + "/" + normalized.id,
      x: position.x,
      y: position.y,
      z: position.z,
      shiftX: shift.x,
      shiftY: shift.y,
      shiftZ: shift.z,
      width: normalized.width * modelScale,
      height: normalized.height * modelScale,
    });
  }

  function sceneInstantiateModelHTML(rawHTML, model, prefix, index) {
    const normalized = normalizeSceneHTML(rawHTML, index);
    if (!normalized || !normalized.html.trim()) {
      return null;
    }
    const position = sceneModelTransformPoint({ x: normalized.x, y: normalized.y, z: normalized.z }, model);
    const shift = sceneModelTransformVector({ x: normalized.shiftX, y: normalized.shiftY, z: normalized.shiftZ }, model);
    const modelScale = sceneModelMaxScale(model);
    return Object.assign({}, normalized, {
      id: prefix + "/" + normalized.id,
      x: position.x,
      y: position.y,
      z: position.z,
      shiftX: shift.x,
      shiftY: shift.y,
      shiftZ: shift.z,
      width: normalized.width * modelScale,
      height: normalized.height * modelScale,
    });
  }

  function sceneModelBoundsNumber(value) {
    const number = typeof value === "number" ? value : Number(value);
    return Number.isFinite(number) ? number : null;
  }

  function sceneModelExpandBounds(bounds, x, y, z) {
    const px = sceneModelBoundsNumber(x);
    const py = sceneModelBoundsNumber(y);
    const pz = sceneModelBoundsNumber(z);
    if (px == null || py == null || pz == null) {
      return bounds;
    }
    const next = bounds || {
      minX: Infinity,
      minY: Infinity,
      minZ: Infinity,
      maxX: -Infinity,
      maxY: -Infinity,
      maxZ: -Infinity,
    };
    if (px < next.minX) next.minX = px;
    if (py < next.minY) next.minY = py;
    if (pz < next.minZ) next.minZ = pz;
    if (px > next.maxX) next.maxX = px;
    if (py > next.maxY) next.maxY = py;
    if (pz > next.maxZ) next.maxZ = pz;
    return next;
  }

  function sceneModelFinalizeBounds(bounds) {
    if (!bounds) {
      return null;
    }
    const minX = sceneModelBoundsNumber(bounds.minX);
    const minY = sceneModelBoundsNumber(bounds.minY);
    const minZ = sceneModelBoundsNumber(bounds.minZ);
    const maxX = sceneModelBoundsNumber(bounds.maxX);
    const maxY = sceneModelBoundsNumber(bounds.maxY);
    const maxZ = sceneModelBoundsNumber(bounds.maxZ);
    if (minX == null || minY == null || minZ == null || maxX == null || maxY == null || maxZ == null) {
      return null;
    }
    if (maxX < minX || maxY < minY || maxZ < minZ) {
      return null;
    }
    const width = maxX - minX;
    const height = maxY - minY;
    const depth = maxZ - minZ;
    return {
      minX,
      minY,
      minZ,
      maxX,
      maxY,
      maxZ,
      width,
      height,
      depth,
      centerX: (minX + maxX) * 0.5,
      centerY: (minY + maxY) * 0.5,
      centerZ: (minZ + maxZ) * 0.5,
      maxDimension: Math.max(width, height, depth),
    };
  }

  function sceneModelNormalizeAssetBounds(value) {
    return value && typeof value === "object" ? sceneModelFinalizeBounds(value) : null;
  }

  function sceneModelExpandPositionArray(bounds, positions, offset) {
    if (!positions || typeof positions.length !== "number") {
      return bounds;
    }
    const source = positions instanceof Float32Array ? positions : sceneTypedFloatArray(positions);
    const ox = sceneNumber(offset && offset.x, 0);
    const oy = sceneNumber(offset && offset.y, 0);
    const oz = sceneNumber(offset && offset.z, 0);
    for (let index = 0; index + 2 < source.length; index += 3) {
      bounds = sceneModelExpandBounds(bounds, source[index] + ox, source[index + 1] + oy, source[index + 2] + oz);
    }
    return bounds;
  }

  function sceneModelExpandPointList(bounds, points, offset) {
    if (!Array.isArray(points)) {
      return bounds;
    }
    const ox = sceneNumber(offset && offset.x, 0);
    const oy = sceneNumber(offset && offset.y, 0);
    const oz = sceneNumber(offset && offset.z, 0);
    for (let index = 0; index < points.length; index += 1) {
      const point = points[index] || {};
      bounds = sceneModelExpandBounds(bounds, sceneNumber(point.x, 0) + ox, sceneNumber(point.y, 0) + oy, sceneNumber(point.z, 0) + oz);
    }
    return bounds;
  }

  function sceneModelExpandPrimitiveBounds(bounds, object) {
    const kind = String(object && object.kind || "").trim().toLowerCase();
    const x = sceneNumber(object && object.x, 0);
    const y = sceneNumber(object && object.y, 0);
    const z = sceneNumber(object && object.z, 0);
    if (kind === "sphere") {
      const radius = Math.max(0, sceneNumber(object.radius, sceneNumber(object.size, 1.2) * 0.5));
      bounds = sceneModelExpandBounds(bounds, x - radius, y - radius, z - radius);
      return sceneModelExpandBounds(bounds, x + radius, y + radius, z + radius);
    }
    if (kind === "box" || kind === "cube") {
      const size = sceneNumber(object.size, 1.2);
      const halfX = Math.max(0, sceneNumber(object.width, size)) * 0.5;
      const halfY = Math.max(0, sceneNumber(object.height, size)) * 0.5;
      const halfZ = Math.max(0, sceneNumber(object.depth, size)) * 0.5;
      bounds = sceneModelExpandBounds(bounds, x - halfX, y - halfY, z - halfZ);
      return sceneModelExpandBounds(bounds, x + halfX, y + halfY, z + halfZ);
    }
    return bounds;
  }

  function sceneModelAssetBounds(record, objects, points) {
    const authored = sceneModelNormalizeAssetBounds(record && record.bounds);
    if (authored && authored.maxDimension > 0) {
      return authored;
    }
    let bounds = null;
    const objectEntries = Array.isArray(objects) ? objects : [];
    for (let index = 0; index < objectEntries.length; index += 1) {
      const object = objectEntries[index] || {};
      if (object.vertices && object.vertices.positions) {
        bounds = sceneModelExpandPositionArray(bounds, object.vertices.positions, object);
      } else if (Array.isArray(object.points)) {
        bounds = sceneModelExpandPointList(bounds, object.points, object);
      } else {
        bounds = sceneModelExpandPrimitiveBounds(bounds, object);
      }
    }
    const pointEntries = Array.isArray(points) ? points : [];
    for (let index = 0; index < pointEntries.length; index += 1) {
      const pointEntry = pointEntries[index] || {};
      bounds = sceneModelExpandPositionArray(bounds, pointEntry._cachedPos || pointEntry.positions, pointEntry);
    }
    return sceneModelFinalizeBounds(bounds);
  }

  function parseSceneModelAsset(raw, src) {
    let payload = raw;
    if (payload && typeof payload === "object" && payload.scene && typeof payload.scene === "object") {
      payload = payload.scene;
    }
    if (Array.isArray(payload)) {
      payload = { objects: payload };
    }
    const record = payload && typeof payload === "object" ? payload : {};
    const objects = Array.isArray(record.objects) ? record.objects.map(function(object) {
      return resolveSceneModelObjectURLs(src, object);
    }) : [];
    const points = Array.isArray(record.points) ? record.points : [];
    const sprites = Array.isArray(record.sprites) ? record.sprites.map(function(sprite) {
      if (!sprite || typeof sprite !== "object") {
        return sprite;
      }
      const resolved = Object.assign({}, sprite);
      resolved.src = resolveSceneModelAssetURL(src, sprite.src);
      return resolved;
    }) : [];
    return {
      src,
      bounds: sceneModelAssetBounds(record, objects, points),
      objects,
      points,
      labels: Array.isArray(record.labels) ? record.labels : [],
      sprites,
      html: Array.isArray(record.html) ? record.html : (Array.isArray(record.htmlOverlays) ? record.htmlOverlays : []),
      lights: Array.isArray(record.lights) ? record.lights : [],
      animations: Array.isArray(record.animations) ? record.animations : [],
      skins: Array.isArray(record.skins) ? record.skins : [],
      nodes: Array.isArray(record.nodes) ? record.nodes : [],
    };
  }

  function sceneModelAssetFormat(src) {
    const raw = typeof src === "string" ? src.trim() : "";
    if (!raw) {
      return "";
    }
    let pathname = raw;
    try {
      pathname = new URL(raw, window.location.href).pathname;
    } catch (_error) {
      pathname = raw.split(/[?#]/, 1)[0];
    }
    const normalized = pathname.toLowerCase();
    if (normalized.endsWith(".glb")) {
      return "glb";
    }
    if (normalized.endsWith(".gltf")) {
      return "gltf";
    }
    return "json";
  }

  // resolveSceneSubFeatureURL reads a hashed sub-feature URL that the
  // island renderer embedded as a data-* attribute on the main scene3d
  // script tag. Using the hashed URL (rather than the unhashed compat
  // URL) lets the browser cache the sub-feature forever, keyed on its
  // content hash. Falls back to the unhashed URL when the attribute
  // isn't present (dev mode, manual integration without the island
  // renderer, etc.).
  function resolveSceneSubFeatureURL(datasetKey, fallback) {
    try {
      var tag = document.querySelector('script[data-gosx-script="feature-scene3d"]');
      if (tag && tag.dataset && tag.dataset[datasetKey]) {
        return tag.dataset[datasetKey];
      }
    } catch (_e) {}
    return fallback;
  }

  // Cached promise for the WebGPU sub-feature chunk. Scene3D now treats
  // WebGPU as the default accelerated backend when the browser exposes it,
  // so the first mount awaits this before choosing its renderer. Failed or
  // unsupported probes still fall through to WebGL/canvas.
  var sceneWebGPUFeaturePromise = null;

  function sceneHasNavigatorWebGPU() {
    return typeof navigator !== "undefined"
      && navigator.gpu
      && typeof navigator.gpu.requestAdapter === "function";
  }

  function ensureWebGPUFeatureLoaded() {
    if (!sceneHasNavigatorWebGPU()) {
      return Promise.resolve(null);
    }
    if (window.__gosx_scene3d_webgpu_api) {
      return Promise.resolve(window.__gosx_scene3d_webgpu_api);
    }
    if (window.__gosx_scene3d_webgpu_feature_promise) {
      return window.__gosx_scene3d_webgpu_feature_promise;
    }
    if (sceneWebGPUFeaturePromise) {
      return sceneWebGPUFeaturePromise;
    }
    sceneWebGPUFeaturePromise = new Promise(function(resolve, reject) {
      var s = document.createElement("script");
      s.async = false;
      s.dataset.gosxScript = "feature-scene3d-webgpu";
      s.src = resolveSceneSubFeatureURL("gosxScene3dWebgpuUrl", "/gosx/bootstrap-feature-scene3d-webgpu.js");
      s.onload = function() {
        if (window.__gosx_scene3d_webgpu_api) {
          resolve(window.__gosx_scene3d_webgpu_api);
        } else {
          sceneWebGPUFeaturePromise = null;
          window.__gosx_scene3d_webgpu_feature_promise = null;
          reject(new Error("scene3d-webgpu chunk loaded but did not publish API"));
        }
      };
      s.onerror = function() {
        sceneWebGPUFeaturePromise = null;
        window.__gosx_scene3d_webgpu_feature_promise = null;
        reject(new Error("failed to load scene3d-webgpu chunk"));
      };
      document.head.appendChild(s);
    });
    window.__gosx_scene3d_webgpu_feature_promise = sceneWebGPUFeaturePromise;
    return sceneWebGPUFeaturePromise;
  }

  function sceneNextFrame() {
    return new Promise(function(resolve) {
      if (typeof window !== "undefined" && typeof window.requestAnimationFrame === "function") {
        window.requestAnimationFrame(function() { resolve(); });
        return;
      }
      setTimeout(resolve, 0);
    });
  }

  async function settlePreferredWebGPUBackend(props, capability) {
    await ensurePreferredWebGPUBackend(props, capability);
    if (typeof sceneWebGPUAvailable === "function" && sceneWebGPUAvailable()) {
      await sceneNextFrame();
    }
  }

  async function ensurePreferredWebGPUBackend(props, capability) {
    if (sceneCapabilityWebGPUPreference(props, capability) !== "prefer") {
      return false;
    }
    if (sceneNeedsWebGLForWebGPUCoverage(props)) {
      return false;
    }
    try {
      var api = await ensureWebGPUFeatureLoaded();
      if (!api) {
        return false;
      }
      if (typeof window.__gosx_scene3d_webgpu_probe_ready === "function") {
        await window.__gosx_scene3d_webgpu_probe_ready();
      }
      return typeof sceneWebGPUAvailable === "function" && sceneWebGPUAvailable();
    } catch (error) {
      console.warn("[gosx] failed to prepare Scene3D WebGPU backend:", error && error.message ? error.message : error);
      return false;
    }
  }

  // Cached promise for the GLTF sub-feature chunk. First call starts the
  // fetch; subsequent calls await the same promise. See 26f-feature-
  // scene3d-gltf-prefix.js for the split rationale.
  var sceneGLTFFeaturePromise = null;

  function ensureGLTFFeatureLoaded() {
    if (window.__gosx_scene3d_gltf_api) {
      return Promise.resolve(window.__gosx_scene3d_gltf_api);
    }
    if (sceneGLTFFeaturePromise) {
      return sceneGLTFFeaturePromise;
    }
    sceneGLTFFeaturePromise = new Promise(function(resolve, reject) {
      var s = document.createElement("script");
      s.async = false;
      s.dataset.gosxScript = "feature-scene3d-gltf";
      s.src = resolveSceneSubFeatureURL("gosxScene3dGltfUrl", "/gosx/bootstrap-feature-scene3d-gltf.js");
      s.onload = function() {
        if (window.__gosx_scene3d_gltf_api) {
          resolve(window.__gosx_scene3d_gltf_api);
        } else {
          reject(new Error("scene3d-gltf chunk loaded but did not publish API"));
        }
      };
      s.onerror = function() {
        sceneGLTFFeaturePromise = null; // allow retry on next attempt
        reject(new Error("failed to load scene3d-gltf chunk"));
      };
      document.head.appendChild(s);
    });
    return sceneGLTFFeaturePromise;
  }

  // Cached promise for the animation sub-feature chunk. Consumers that
  // want to drive keyframe or skeletal animations can await this helper
  // and then use window.__gosx_scene3d_animation_api.
  var sceneAnimationFeaturePromise = null;

  function ensureAnimationFeatureLoaded() {
    if (window.__gosx_scene3d_animation_api) {
      return Promise.resolve(window.__gosx_scene3d_animation_api);
    }
    if (sceneAnimationFeaturePromise) {
      return sceneAnimationFeaturePromise;
    }
    sceneAnimationFeaturePromise = new Promise(function(resolve, reject) {
      var s = document.createElement("script");
      s.async = false;
      s.dataset.gosxScript = "feature-scene3d-animation";
      s.src = resolveSceneSubFeatureURL("gosxScene3dAnimationUrl", "/gosx/bootstrap-feature-scene3d-animation.js");
      s.onload = function() {
        if (window.__gosx_scene3d_animation_api) {
          resolve(window.__gosx_scene3d_animation_api);
        } else {
          reject(new Error("scene3d-animation chunk loaded but did not publish API"));
        }
      };
      s.onerror = function() {
        sceneAnimationFeaturePromise = null;
        reject(new Error("failed to load scene3d-animation chunk"));
      };
      document.head.appendChild(s);
    });
    return sceneAnimationFeaturePromise;
  }

  // Expose the animation lazy-loader for consumers that need to drive
  // keyframe or skeletal clips from outside the main scene mount.
  window.__gosx_ensure_scene3d_animation_loaded = ensureAnimationFeatureLoaded;

  async function loadSceneModelAsset(src) {
    const key = String(src || "").trim();
    if (!key) {
      return parseSceneModelAsset({}, key);
    }
    if (!sceneModelAssetCache.has(key)) {
      sceneModelAssetCache.set(key, (async function() {
        try {
          const format = sceneModelAssetFormat(key);
          if (format === "glb" || format === "gltf") {
            // GLTF parsing lives in a sub-feature chunk that's fetched
            // on demand — the first .glb/.gltf request on a page pays
            // the download + parse cost, subsequent ones reuse the
            // cached module. Pages that never load models never fetch
            // the chunk at all.
            var gltfApi = await ensureGLTFFeatureLoaded();
            return parseSceneModelAsset(gltfApi.gltfSceneToModelAsset(await gltfApi.sceneLoadGLTFModel(key), key), key);
          }
          const response = await fetch(key, { credentials: "same-origin" });
          if (!response || !response.ok) {
            throw new Error("HTTP " + String(response && response.status || 0));
          }
          return parseSceneModelAsset(await response.json(), key);
        } catch (error) {
          console.warn("[gosx] failed to load Scene3D model asset:", key, error && error.message ? error.message : error);
          gosxSceneEmit("warn", "model-asset-load-failed", {
            asset: String(key || ""),
            error: error && error.message ? String(error.message) : String(error),
          });
          return parseSceneModelAsset({}, key);
        }
      })());
    }
    return sceneModelAssetCache.get(key);
  }

  function sceneModelHasSkins(skins) {
    return Array.isArray(skins) && skins.some(function(skin) {
      return Boolean(skin && Array.isArray(skin.joints) && skin.joints.length > 0 && skin.inverseBindMatrices);
    });
  }

  function sceneModelRootNodes(nodes) {
    if (!Array.isArray(nodes) || !nodes.length) {
      return [];
    }
    const childSet = new Set();
    for (let index = 0; index < nodes.length; index += 1) {
      const children = nodes[index] && nodes[index].children;
      if (!Array.isArray(children)) {
        continue;
      }
      for (let childIndex = 0; childIndex < children.length; childIndex += 1) {
        childSet.add(children[childIndex]);
      }
    }
    const roots = [];
    for (let index = 0; index < nodes.length; index += 1) {
      if (!childSet.has(index)) {
        roots.push(index);
      }
    }
    return roots;
  }

  // True when the WASM motion mixer is active for this record (P4-M3, opt-in).
  function sceneModelWasmMixerActive(record) {
    return Boolean(
      record && record.wasmMixer &&
      typeof window !== "undefined" && window.__gosx_motion_wasm &&
      typeof window.__gosx_motion_mixer_update === "function"
    );
  }

  // Drive one WASM-mixer frame: tick the mixer into a reused out buffer
  // (grow-and-retick when the write count exceeds capacity) and decode the
  // packed writes into animatedTransforms via the published animation API.
  function sceneAdvanceWasmModelMixer(record, deltaTime, reduced, animatedTransforms) {
    const api = record.animationApi;
    if (!api || typeof api.wasmDecodePose !== "function") {
      return;
    }
    if (!record._wasmMixerF64 || !record._wasmMixerU8) {
      record._wasmMixerF64 = new Float64Array(2048);
      record._wasmMixerU8 = new Uint8Array(record._wasmMixerF64.buffer);
    }
    const reducedFlag = reduced === true;
    let count = window.__gosx_motion_mixer_update(record.wasmMixer, deltaTime, reducedFlag, record._wasmMixerU8);
    if (count > record._wasmMixerF64.length) {
      record._wasmMixerF64 = new Float64Array(count);
      record._wasmMixerU8 = new Uint8Array(record._wasmMixerF64.buffer);
      // Pass dt=0: the clip clock already advanced on the first call above.
      // Re-emitting at the current time with dt=0 avoids a double clock step.
      count = window.__gosx_motion_mixer_update(record.wasmMixer, 0, reducedFlag, record._wasmMixerU8);
      if (count > record._wasmMixerF64.length) {
        count = record._wasmMixerF64.length;
      }
    }
    api.wasmDecodePose(record._wasmMixerF64, count, animatedTransforms);
  }

  function sceneApplyModelSkinPose(record, deltaTime, reduced) {
    if (!record || !record.animationApi || !record.nodes || !record.skins) {
      return;
    }
    const animatedTransforms = record.animatedTransforms;
    if (animatedTransforms && typeof animatedTransforms.clear === "function") {
      animatedTransforms.clear();
    }
    if (sceneModelWasmMixerActive(record)) {
      sceneAdvanceWasmModelMixer(record, deltaTime, reduced, animatedTransforms);
    } else if (record.mixer) {
      record.mixer.update(deltaTime, function(targetNode, property, value) {
        let entry = animatedTransforms.get(targetNode);
        if (!entry) {
          entry = {};
          animatedTransforms.set(targetNode, entry);
        }
        entry[property] = Array.isArray(value) ? value.slice() : Array.from(value || []);
      });
    }
    const nodeTransforms = record.animationApi.buildNodeTransforms(record.nodes, animatedTransforms, record.rootTransform, record.rootNodes);
    for (let index = 0; index < record.skins.length; index += 1) {
      const skin = record.skins[index];
      if (!skin) {
        continue;
      }
      skin.jointMatrices = record.animationApi.computeJointMatrices(skin, nodeTransforms);
    }
  }

  function sceneOwns(source, key) {
    return Boolean(source && Object.prototype.hasOwnProperty.call(source, key));
  }

  function sceneAnimationNumber(source, key, fallback, min) {
    if (!sceneOwns(source, key)) {
      return fallback;
    }
    const value = sceneNumber(source[key], fallback);
    return Number.isFinite(value) ? Math.max(min, value) : fallback;
  }

  function sceneAnimationMilliseconds(source, key, fallbackSeconds) {
    if (!sceneOwns(source, key)) {
      return fallbackSeconds;
    }
    const value = Number(source[key]);
    return Number.isFinite(value) ? Math.max(0, value) / 1000 : fallbackSeconds;
  }

  function sceneModelAnimationPlayOptions(model, patch, defaults) {
    const fallbackLoop = defaults && typeof defaults.loop === "boolean" ? defaults.loop : true;
    const modelLoop = sceneOwns(model, "loop") ? model.loop !== false : fallbackLoop;
    const loop = sceneOwns(patch, "loop") ? patch.loop !== false : modelLoop;
    const modelSpeed = sceneAnimationNumber(model, "animationSpeed", defaults && defaults.speed !== undefined ? defaults.speed : 1, 0);
    const modelWeight = sceneAnimationNumber(model, "animationWeight", defaults && defaults.weight !== undefined ? defaults.weight : 1, 0);
    return {
      loop,
      speed: sceneAnimationNumber(patch, "animationSpeed", modelSpeed, 0),
      weight: sceneAnimationNumber(patch, "animationWeight", modelWeight, 0),
      fadeIn: sceneAnimationMilliseconds(
        patch,
        "animationFadeInMS",
        sceneAnimationMilliseconds(model, "animationFadeInMS", defaults && defaults.fadeIn !== undefined ? defaults.fadeIn : 0),
      ),
    };
  }

  function sceneModelAnimationStopOptions(model, patch, defaults) {
    return {
      fadeOut: sceneAnimationMilliseconds(
        patch,
        "animationFadeOutMS",
        sceneAnimationMilliseconds(model, "animationFadeOutMS", defaults && defaults.fadeOut !== undefined ? defaults.fadeOut : 0),
      ),
    };
  }

  function sceneApplyModelAnimationControls(record, patch) {
    if (!record || !record.model || !sceneIsPlainObject(patch)) {
      return;
    }
    const keys = ["loop", "animationSpeed", "animationWeight", "animationFadeInMS", "animationFadeOutMS"];
    for (let index = 0; index < keys.length; index += 1) {
      const key = keys[index];
      if (sceneOwns(patch, key)) {
        record.model[key] = patch[key];
      }
    }
  }

  function sceneRegisterModelAnimationRecord(state, record) {
    if (!state || !record || (!record.mixer && !record.wasmMixerActive)) {
      return;
    }
    if (!Array.isArray(state._modelAnimations)) {
      state._modelAnimations = [];
    }
    if (state._modelAnimations.indexOf(record) < 0) {
      state._modelAnimations.push(record);
    }
  }

  function sceneRegisterStaticModelLiveRecord(state, instanceModel, objectIDs) {
    if (!state || !instanceModel || !Array.isArray(instanceModel._live) || instanceModel._live.length === 0 || !Array.isArray(objectIDs) || objectIDs.length === 0) {
      return;
    }
    const modelCopy = Object.assign({}, instanceModel || {});
    const record = {
      id: typeof modelCopy.id === "string" ? modelCopy.id : "",
      model: modelCopy,
      live: modelCopy._live.slice(),
      objectIDs: objectIDs.slice(),
      rootTransform: sceneModelTransformMatrix(modelCopy),
      animation: "",
      animationSeq: "",
      poseDirty: false,
      staticModel: true,
    };
    if (!Array.isArray(state._modelSkins)) {
      state._modelSkins = [];
    }
    state._modelSkins.push(record);
  }

  async function scenePrepareModelSkinPlayback(state, asset, instanceModel, skinInstances, objectIDs) {
    if (!sceneModelHasSkins(skinInstances) || !Array.isArray(asset.nodes) || !asset.nodes.length) {
      return;
    }

    let animationApi = null;
    try {
      animationApi = await ensureAnimationFeatureLoaded();
    } catch (error) {
      console.warn("[gosx] failed to load Scene3D animation support:", error && error.message ? error.message : error);
      return;
    }
    if (!animationApi || typeof animationApi.buildNodeTransforms !== "function" || typeof animationApi.computeJointMatrices !== "function") {
      return;
    }

    const record = {
      id: typeof instanceModel.id === "string" ? instanceModel.id : "",
      model: Object.assign({}, instanceModel || {}),
      live: Array.isArray(instanceModel && instanceModel._live) ? instanceModel._live.slice() : [],
      objectIDs: Array.isArray(objectIDs) ? objectIDs.slice() : [],
      nodes: asset.nodes,
      rootNodes: sceneModelRootNodes(asset.nodes),
      skins: skinInstances,
      animatedTransforms: new Map(),
      rootTransform: sceneModelTransformMatrix(instanceModel),
      animationApi,
      mixer: null,
      animation: "",
      animationSeq: "",
      poseDirty: false,
    };
    if (!Array.isArray(state._modelSkins)) {
      state._modelSkins = [];
    }
    state._modelSkins.push(record);

    const clips = sceneCloneModelAnimations(asset.animations);
    const wantWasmMixer = clips.length > 0
      && typeof window !== "undefined"
      && window.__gosx_motion_wasm
      && typeof window.__gosx_motion_mixer_create === "function"
      && typeof animationApi.wasmClipJSON === "function";
    if (wantWasmMixer) {
      // P4-M3: route glTF clip playback through the Go WASM motion mixer.
      const handle = window.__gosx_motion_mixer_create();
      if (handle >= 1) {
        record.wasmMixer = handle;
        record.wasmMixerActive = true;
        for (let index = 0; index < clips.length; index += 1) {
          const clip = clips[index];
          window.__gosx_motion_mixer_add_clip(handle, clip.name, animationApi.wasmClipJSON(clip));
        }
        sceneRegisterModelAnimationRecord(state, record);
        const requestedAnimation = typeof instanceModel.animation === "string" ? instanceModel.animation.trim() : "";
        if (requestedAnimation) {
          sceneModelRecordPlay(record, requestedAnimation, sceneModelAnimationPlayOptions(instanceModel, null, { loop: true, speed: 1, weight: 1, fadeIn: 0 }));
          if (sceneModelRecordIsPlaying({ animation: requestedAnimation, wasmMixerActive: true, wasmMixer: handle })) {
            record.animation = requestedAnimation;
            record.animationSeq = typeof instanceModel.animationSeq === "string" ? instanceModel.animationSeq : "";
          }
        }
      }
    } else if (typeof animationApi.createMixer === "function") {
      if (clips.length) {
        const mixer = animationApi.createMixer();
        for (let index = 0; index < clips.length; index += 1) {
          const clip = clips[index];
          mixer.addClip(clip.name, clip);
        }
        record.mixer = mixer;
        sceneRegisterModelAnimationRecord(state, record);
        const requestedAnimation = typeof instanceModel.animation === "string" ? instanceModel.animation.trim() : "";
        if (requestedAnimation) {
          mixer.play(requestedAnimation, sceneModelAnimationPlayOptions(instanceModel, null, { loop: true, speed: 1, weight: 1, fadeIn: 0 }));
          if (mixer.isPlaying(requestedAnimation)) {
            record.animation = requestedAnimation;
            record.animationSeq = typeof instanceModel.animationSeq === "string" ? instanceModel.animationSeq : "";
          }
        }
      }
    }

    sceneApplyModelSkinPose(record, 0, false);
  }

  // Route a clip play through the active mixer. opts is the JS-mixer options
  // shape ({loop, speed, weight, fadeIn}); the WASM mixer takes the same values
  // as positional arguments.
  function sceneModelRecordPlay(record, name, opts) {
    const options = opts || {};
    if (record && record.wasmMixerActive) {
      if (typeof window !== "undefined" && typeof window.__gosx_motion_mixer_play === "function") {
        window.__gosx_motion_mixer_play(
          record.wasmMixer,
          name,
          options.fadeIn !== undefined ? options.fadeIn : 0,
          options.loop !== undefined ? options.loop !== false : true,
          options.speed !== undefined ? options.speed : 1,
          options.weight !== undefined ? options.weight : 1
        );
      }
      return;
    }
    if (record && record.mixer) {
      record.mixer.play(name, options);
    }
  }

  // Route a clip stop through the active mixer. opts is the JS-mixer options
  // shape ({fadeOut}); the WASM mixer takes fadeOut positionally.
  function sceneModelRecordStop(record, name, opts) {
    const options = opts || {};
    if (record && record.wasmMixerActive) {
      if (typeof window !== "undefined" && typeof window.__gosx_motion_mixer_stop === "function") {
        window.__gosx_motion_mixer_stop(record.wasmMixer, name, options.fadeOut !== undefined ? options.fadeOut : 0);
      }
      return;
    }
    if (record && record.mixer) {
      record.mixer.stop(name, options);
    }
  }

  // Whether a named clip is playing on the record's active mixer, routed to the
  // WASM mixer when active (P4-M3) and the JS mixer otherwise.
  function sceneModelRecordWasPlaying(record, name) {
    if (!record || !name) {
      return false;
    }
    if (record.wasmMixerActive) {
      return Boolean(
        typeof window !== "undefined" &&
        typeof window.__gosx_motion_mixer_is_playing === "function" &&
        window.__gosx_motion_mixer_is_playing(record.wasmMixer, name)
      );
    }
    return Boolean(record.mixer && record.mixer.isPlaying(name));
  }

  // Whether a record's currently-selected animation is playing.
  function sceneModelRecordIsPlaying(record) {
    return record ? sceneModelRecordWasPlaying(record, record.animation) : false;
  }

  function sceneHasActiveModelAnimations(state) {
    const records = state && Array.isArray(state._modelAnimations) ? state._modelAnimations : [];
    return records.some(function(record) {
      return sceneModelRecordIsPlaying(record);
    });
  }

  function sceneAdvanceModelAnimations(state, deltaTime, reduced) {
    const records = state && Array.isArray(state._modelAnimations) ? state._modelAnimations : [];
    for (let index = 0; index < records.length; index += 1) {
      const record = records[index];
      if (!record) {
        continue;
      }
      const playing = sceneModelRecordIsPlaying(record);
      if (!playing && !record.poseDirty) {
        continue;
      }
      record.poseDirty = false;
      sceneApplyModelSkinPose(record, deltaTime, reduced);
    }
  }

  function sceneModelRecordListensToEvent(record, eventName) {
    return Boolean(record && Array.isArray(record.live) && record.live.indexOf(eventName) >= 0);
  }

  function sceneModelLivePatchForRecord(record, payload) {
    if (!sceneIsPlainObject(payload)) {
      return null;
    }
    if (record && record.id && sceneIsPlainObject(payload[record.id])) {
      return payload[record.id];
    }
    return payload;
  }

  function sceneApplyModelLiveOpacity(state, record, patch) {
    if (!state || !record || !sceneIsPlainObject(patch) || !Object.prototype.hasOwnProperty.call(patch, "opacity")) {
      return false;
    }
    const opacity = Math.max(0, Math.min(1, sceneNumber(patch.opacity, sceneNumber(record.model && record.model.opacity, 1))));
    if (record.model) {
      record.model.opacity = opacity;
    }
    const objectIDs = Array.isArray(record.objectIDs) ? record.objectIDs : [];
    let changed = false;
    for (let index = 0; index < objectIDs.length; index += 1) {
      const object = state.objects && state.objects.get ? state.objects.get(objectIDs[index]) : null;
      if (!object || object.opacity === opacity) {
        continue;
      }
      object.opacity = opacity;
      if (opacity < 1 && (!object.blendMode || object.blendMode === "opaque")) {
        object.blendMode = "alpha";
      }
      changed = true;
    }
    return changed;
  }

  function sceneApplyStaticModelObjectTransform(state, record) {
    if (!state || !record || !record.staticModel || !Array.isArray(record.objectIDs)) {
      return false;
    }
    let changed = false;
    for (let index = 0; index < record.objectIDs.length; index += 1) {
      const object = state.objects && state.objects.get ? state.objects.get(record.objectIDs[index]) : null;
      const local = object && object._modelLocalVertices;
      if (!object || !object.vertices || !local || !local.positions) {
        continue;
      }
      object.vertices.positions = sceneModelTransformMeshFloats(local.positions, 3, function(x, y, z) {
        return sceneModelTransformPoint({ x: x, y: y, z: z }, record.model);
      });
      if (local.normals && local.normals.length) {
        object.vertices.normals = sceneModelTransformMeshFloats(local.normals, 3, function(x, y, z) {
          return sceneNormalizeDirection(sceneModelTransformVector({ x: x, y: y, z: z }, record.model));
        });
      }
      if (local.tangents && local.tangents.length) {
        object.vertices.tangents = sceneModelTransformMeshFloats(local.tangents, 4, function(x, y, z, w) {
          const rotated = sceneNormalizeDirection(sceneModelTransformVector({ x: x, y: y, z: z }, record.model));
          return { x: rotated.x, y: rotated.y, z: rotated.z, w: sceneNumber(w, 1) };
        });
      }
      object.vertices.uvs = local.uvs;
      object.vertices.count = local.count;
      object.static = false;
      sceneApplyModelObjectHiddenState(object, record.model);
      changed = true;
    }
    return changed;
  }

  function sceneComputedPoseName(value) {
    const pose = String(value == null ? "" : value).trim();
    switch (pose) {
      case "guard":
      case "strike":
      case "kick":
      case "hit":
      case "down":
      case "surge":
      case "start":
        return pose;
      case "idle":
      default:
        return "idle";
    }
  }

  function sceneComputedPoseBaseID(id) {
    let base = String(id || "").trim();
    const suffixes = ["-guard", "-strike", "-kick", "-hit", "-down", "-surge", "-start"];
    for (let index = 0; index < suffixes.length; index += 1) {
      const suffix = suffixes[index];
      if (base.length > suffix.length && base.slice(base.length - suffix.length) === suffix) {
        base = base.slice(0, base.length - suffix.length);
        break;
      }
    }
    return base;
  }

  function sceneComputedPoseTargetID(recordID, pose) {
    const base = sceneComputedPoseBaseID(recordID);
    const normalized = sceneComputedPoseName(pose);
    if (!base || normalized === "idle") {
      return base;
    }
    return base + "-" + normalized;
  }

  function sceneComputedPoseRecordByID(state, id) {
    const want = String(id || "").trim();
    if (!want) {
      return null;
    }
    const records = Array.isArray(state && state._modelSkins) ? state._modelSkins : [];
    for (let index = 0; index < records.length; index += 1) {
      const record = records[index];
      if (record && String(record.id || "") === want) {
        return record;
      }
    }
    return null;
  }

  function sceneComputedPoseObject(state, id) {
    return state && state.objects && typeof state.objects.get === "function"
      ? state.objects.get(id)
      : null;
  }

  function sceneComputedPoseLocalVertices(object) {
    const local = object && object._modelLocalVertices;
    if (!local || !local.positions || typeof local.positions.length !== "number") {
      return null;
    }
    const count = Math.max(0, Math.floor(sceneNumber(local.count, 0)));
    if (count <= 0 || local.positions.length < count * 3) {
      return null;
    }
    return local;
  }

  function sceneComputedPoseFloat32Array(value) {
    if (!value || typeof value.length !== "number") {
      return null;
    }
    return value instanceof Float32Array ? value : new Float32Array(value);
  }

  function sceneComputedPoseBlendArray(object, cacheKey, source, target, tupleSize, alpha, normalizeVec3) {
    const sourceArray = sceneComputedPoseFloat32Array(source);
    const targetArray = sceneComputedPoseFloat32Array(target || source);
    const width = Math.max(1, Math.floor(sceneNumber(tupleSize, 1)));
    if (!sourceArray || !targetArray || sourceArray.length < width || targetArray.length < width) {
      return null;
    }
    const limit = Math.min(sourceArray.length, targetArray.length);
    let current = object && object[cacheKey];
    if (!current || current.length !== sourceArray.length) {
      current = new Float32Array(sourceArray);
      if (object) {
        object[cacheKey] = current;
      }
    }
    const t = Math.max(0, Math.min(1, sceneNumber(alpha, 0.45)));
    for (let index = 0; index + width - 1 < limit; index += width) {
      for (let component = 0; component < width; component += 1) {
        current[index + component] += (targetArray[index + component] - current[index + component]) * t;
      }
      if (normalizeVec3 && width >= 3) {
        const x = current[index];
        const y = current[index + 1];
        const z = current[index + 2];
        const length = Math.hypot(x, y, z);
        if (length > 0.000001) {
          current[index] = x / length;
          current[index + 1] = y / length;
          current[index + 2] = z / length;
        }
      }
    }
    return current;
  }

  function sceneComputedPoseApplyObjectMorph(object, sourceLocal, targetLocal, model, alpha) {
    if (!object || !object.vertices || !sourceLocal || !targetLocal) {
      return 0;
    }
    const sourceCount = Math.max(0, Math.floor(sceneNumber(sourceLocal.count, 0)));
    const targetCount = Math.max(0, Math.floor(sceneNumber(targetLocal.count, 0)));
    const count = Math.min(sourceCount, targetCount);
    if (count <= 0 || sourceLocal.positions.length < count * 3 || targetLocal.positions.length < count * 3) {
      return 0;
    }
    const sourcePositions = sourceLocal.positions.length === count * 3
      ? sourceLocal.positions
      : sourceLocal.positions.subarray(0, count * 3);
    const targetPositions = targetLocal.positions.length === count * 3
      ? targetLocal.positions
      : targetLocal.positions.subarray(0, count * 3);
    const morphedPositions = sceneComputedPoseBlendArray(object, "_computedPoseLocalPositions", sourcePositions, targetPositions, 3, alpha, false);
    if (!morphedPositions) {
      return 0;
    }

    object.computedMorph = {
      sourcePositions,
      targetPositions,
      sourceNormals: sourceLocal.normals && sourceLocal.normals.length >= count * 3
        ? (sourceLocal.normals.subarray ? sourceLocal.normals.subarray(0, count * 3) : sourceLocal.normals)
        : null,
      targetNormals: targetLocal.normals && targetLocal.normals.length >= count * 3
        ? (targetLocal.normals.subarray ? targetLocal.normals.subarray(0, count * 3) : targetLocal.normals)
        : null,
      sourceTangents: sourceLocal.tangents && sourceLocal.tangents.length >= count * 4
        ? (sourceLocal.tangents.subarray ? sourceLocal.tangents.subarray(0, count * 4) : sourceLocal.tangents)
        : null,
      targetTangents: targetLocal.tangents && targetLocal.tangents.length >= count * 4
        ? (targetLocal.tangents.subarray ? targetLocal.tangents.subarray(0, count * 4) : targetLocal.tangents)
        : null,
      uvs: sourceLocal.uvs,
      count,
      alpha: Math.max(0, Math.min(1, sceneNumber(alpha, 0.45))),
      modelMatrix: sceneModelTransformMatrix(model),
    };

    object.vertices.positions = sceneModelTransformMeshFloats(morphedPositions, 3, function(x, y, z) {
      return sceneModelTransformPoint({ x: x, y: y, z: z }, model);
    });

    const sourceNormals = sourceLocal.normals && sourceLocal.normals.length >= count * 3
      ? sourceLocal.normals.subarray ? sourceLocal.normals.subarray(0, count * 3) : sourceLocal.normals
      : null;
    const targetNormals = targetLocal.normals && targetLocal.normals.length >= count * 3
      ? targetLocal.normals.subarray ? targetLocal.normals.subarray(0, count * 3) : targetLocal.normals
      : null;
    const morphedNormals = sourceNormals
      ? sceneComputedPoseBlendArray(object, "_computedPoseLocalNormals", sourceNormals, targetNormals || sourceNormals, 3, alpha, true)
      : null;
    if (morphedNormals) {
      object.vertices.normals = sceneModelTransformMeshFloats(morphedNormals, 3, function(x, y, z) {
        return sceneNormalizeDirection(sceneModelTransformVector({ x: x, y: y, z: z }, model));
      });
    }

    const sourceTangents = sourceLocal.tangents && sourceLocal.tangents.length >= count * 4
      ? sourceLocal.tangents.subarray ? sourceLocal.tangents.subarray(0, count * 4) : sourceLocal.tangents
      : null;
    const targetTangents = targetLocal.tangents && targetLocal.tangents.length >= count * 4
      ? targetLocal.tangents.subarray ? targetLocal.tangents.subarray(0, count * 4) : targetLocal.tangents
      : null;
    const morphedTangents = sourceTangents
      ? sceneComputedPoseBlendArray(object, "_computedPoseLocalTangents", sourceTangents, targetTangents || sourceTangents, 4, alpha, true)
      : null;
    if (morphedTangents) {
      object.vertices.tangents = sceneModelTransformMeshFloats(morphedTangents, 4, function(x, y, z, w) {
        const rotated = sceneNormalizeDirection(sceneModelTransformVector({ x: x, y: y, z: z }, model));
        return { x: rotated.x, y: rotated.y, z: rotated.z, w: sceneNumber(w, 1) };
      });
    }

    object.vertices.uvs = sourceLocal.uvs;
    object.vertices.count = count;
    object.static = false;
    sceneApplyModelObjectHiddenState(object, model);
    return count;
  }

  function sceneApplyModelComputedPose(state, record, patch) {
    if (!state || !record || !record.staticModel || !sceneOwns(patch, "computedPose")) {
      return false;
    }
    const pose = sceneComputedPoseName(patch.computedPose);
    const alpha = Math.max(0, Math.min(1, sceneNumber(patch.computedPoseAlpha, pose === "idle" ? 0.32 : 0.52)));
    const targetID = sceneComputedPoseTargetID(record.id, pose);
    const targetRecord = targetID === record.id ? record : sceneComputedPoseRecordByID(state, targetID);
    record.computedPose = pose;
    record.computedPoseAlpha = alpha;
    record.computedPoseTargetID = targetID;
    record.computedMorphObjects = 0;
    record.computedMorphVertices = 0;
    if (!targetRecord || !Array.isArray(record.objectIDs) || !Array.isArray(targetRecord.objectIDs)) {
      return false;
    }

    const count = Math.min(record.objectIDs.length, targetRecord.objectIDs.length);
    let changed = false;
    let morphObjects = 0;
    let morphVertices = 0;
    for (let index = 0; index < count; index += 1) {
      const object = sceneComputedPoseObject(state, record.objectIDs[index]);
      const targetObject = sceneComputedPoseObject(state, targetRecord.objectIDs[index]);
      const sourceLocal = sceneComputedPoseLocalVertices(object);
      const targetLocal = sceneComputedPoseLocalVertices(targetObject) || sourceLocal;
      const vertices = sceneComputedPoseApplyObjectMorph(object, sourceLocal, targetLocal, record.model, alpha);
      if (vertices <= 0) {
        continue;
      }
      changed = true;
      morphObjects += 1;
      morphVertices += vertices;
    }
    record.computedMorphObjects = morphObjects;
    record.computedMorphVertices = morphVertices;
    return changed;
  }

  function sceneApplyModelLivePatch(state, record, patch) {
    if (!record || !record.model || !sceneIsPlainObject(patch)) {
      return false;
    }
    const keys = ["x", "y", "z", "rotationX", "rotationY", "rotationZ", "scaleX", "scaleY", "scaleZ"];
    const hasComputedPose = sceneOwns(patch, "computedPose");
    let changed = sceneApplyModelLiveOpacity(state, record, patch);
    if (sceneOwns(patch, "visible")) {
      const nextVisible = sceneBool(patch.visible, true);
      if (record.model.visible !== nextVisible) {
        record.model.visible = nextVisible;
        changed = true;
      }
    }
    for (let index = 0; index < keys.length; index += 1) {
      const key = keys[index];
      if (!Object.prototype.hasOwnProperty.call(patch, key)) {
        continue;
      }
      const next = sceneNumber(patch[key], sceneNumber(record.model[key], key.indexOf("scale") === 0 ? 1 : 0));
      if (record.model[key] === next) {
        continue;
      }
      record.model[key] = next;
      changed = true;
    }
    if (!changed && !hasComputedPose) {
      return false;
    }
    record.rootTransform = sceneModelTransformMatrix(record.model);
    let computedPoseChanged = false;
    if (hasComputedPose) {
      computedPoseChanged = sceneApplyModelComputedPose(state, record, patch);
    }
    if (record.staticModel && !computedPoseChanged) {
      sceneApplyStaticModelObjectTransform(state, record);
    }
    changed = changed || computedPoseChanged;
    if (!changed) {
      return false;
    }
    record.poseDirty = true;
    return true;
  }

  function sceneApplyModelLiveAnimation(record, patch) {
    if (!record || (!record.mixer && !record.wasmMixerActive) || !sceneIsPlainObject(patch)) {
      return false;
    }
    const hasAnimation = sceneOwns(patch, "animation");
    const hasControls = sceneOwns(patch, "loop")
      || sceneOwns(patch, "animationSpeed")
      || sceneOwns(patch, "animationWeight")
      || sceneOwns(patch, "animationFadeInMS")
      || sceneOwns(patch, "animationFadeOutMS");
    if (!hasAnimation && !hasControls) {
      return false;
    }
    const animation = hasAnimation
      ? (typeof patch.animation === "string" ? patch.animation.trim() : "")
      : record.animation;
    const hasSeq = sceneOwns(patch, "animationSeq");
    const animationSeq = hasSeq ? String(patch.animationSeq == null ? "" : patch.animationSeq) : "";
    const replay = Boolean(hasSeq && animationSeq && record.animation === animation && record.animationSeq !== animationSeq);
    sceneApplyModelAnimationControls(record, patch);
    if (!animation) {
      if (record.animation && sceneModelRecordIsPlaying(record)) {
        const stopOptions = sceneModelAnimationStopOptions(record.model, patch, { fadeOut: 0.05 });
        sceneModelRecordStop(record, record.animation, stopOptions);
        if (stopOptions.fadeOut <= 0) {
          record.animation = "";
        }
      }
      record.animationSeq = animationSeq;
      record.poseDirty = true;
      return true;
    }
    if (record.animation === animation && sceneModelRecordIsPlaying(record) && !replay) {
      if (hasControls) {
        sceneModelRecordPlay(record, animation, sceneModelAnimationPlayOptions(record.model, patch, { loop: true, speed: 1, weight: 1, fadeIn: 0 }));
        record.poseDirty = true;
        return true;
      }
      return false;
    }
    if (record.animation && sceneModelRecordIsPlaying(record)) {
      sceneModelRecordStop(record, record.animation, sceneModelAnimationStopOptions(record.model, patch, { fadeOut: replay ? 0 : 0.05 }));
    }
    sceneModelRecordPlay(record, animation, sceneModelAnimationPlayOptions(record.model, patch, { loop: true, speed: 1, weight: 1, fadeIn: replay ? 0 : 0.04 }));
    if (!sceneModelRecordWasPlaying(record, animation)) {
      return false;
    }
    record.animation = animation;
    record.animationSeq = animationSeq;
    record.poseDirty = true;
    return true;
  }

  function sceneApplyModelLiveEvent(state, eventName, payload) {
    const event = typeof eventName === "string" ? eventName.trim() : "";
    if (!event) {
      return false;
    }
    const records = state && Array.isArray(state._modelSkins) ? state._modelSkins : [];
    let changed = false;
    for (let index = 0; index < records.length; index += 1) {
      const record = records[index];
      if (!sceneModelRecordListensToEvent(record, event)) {
        continue;
      }
      const patch = sceneModelLivePatchForRecord(record, payload);
      changed = sceneApplyModelLivePatch(state, record, patch) || changed;
      changed = sceneApplyModelLiveAnimation(record, patch) || changed;
    }
    return changed;
  }

  function sceneApplyCameraLiveEvent(state, payload) {
    if (!state || !sceneIsPlainObject(payload) || !sceneIsPlainObject(payload.camera)) {
      return false;
    }
    const nextCamera = normalizeSceneCamera(payload.camera, state.camera);
    if (sceneCameraEquivalent(state.camera, nextCamera)) {
      return false;
    }
    state.camera = nextCamera;
    return true;
  }

  function sceneHydrationModels(state, props) {
    const models = Array.isArray(state && state.models) ? state.models : sceneModels(props);
    const instancedGLBMeshes = Array.isArray(state && state.instancedGLBMeshes)
      ? state.instancedGLBMeshes
      : sceneInstancedGLBMeshes(props);
    return models.concat(sceneInstancedGLBModelsFromBatches(instancedGLBMeshes));
  }

  function sceneClearHydratedModelRecords(state) {
    if (!state || !state._hydratedModelRecords) {
      return;
    }
    const records = state._hydratedModelRecords;
    for (const id of (Array.isArray(records.objects) ? records.objects : [])) {
      state.objects.delete(sceneObjectKey(id));
    }
    for (const id of (Array.isArray(records.labels) ? records.labels : [])) {
      state.labels.delete(sceneObjectKey(id));
    }
    for (const id of (Array.isArray(records.sprites) ? records.sprites : [])) {
      state.sprites.delete(sceneObjectKey(id));
    }
    for (const id of (Array.isArray(records.html) ? records.html : [])) {
      state.html.delete(sceneObjectKey(id));
    }
    for (const id of (Array.isArray(records.lights) ? records.lights : [])) {
      state.lights.delete(sceneObjectKey(id));
    }
    if (Array.isArray(records.points) && records.points.length > 0 && Array.isArray(state.points)) {
      const pointIDs = new Set(records.points.map(function(id) { return sceneObjectKey(id); }));
      state.points = state.points.filter(function(point) {
        return !pointIDs.has(sceneObjectKey(point && point.id));
      });
    }
    state._hydratedModelRecords = null;
  }

  // P4-M3: free WASM motion mixers attached to model records before they are
  // dropped (re-hydration or teardown). No-op when the flag is off / no mixers.
  function sceneDestroyModelWasmMixers(records) {
    if (!Array.isArray(records) || typeof window === "undefined" || typeof window.__gosx_motion_mixer_destroy !== "function") {
      return;
    }
    for (let index = 0; index < records.length; index += 1) {
      const record = records[index];
      if (record && record.wasmMixer) {
        window.__gosx_motion_mixer_destroy(record.wasmMixer);
        record.wasmMixer = 0;
        record.wasmMixerActive = false;
      }
    }
  }

  async function hydrateSceneStateModels(state, props) {
    const models = sceneHydrationModels(state, props);
    sceneDestroyModelWasmMixers(state && state._modelSkins);
    state._modelAnimations = [];
    state._modelSkins = [];
    sceneClearHydratedModelRecords(state);
    if (!models.length) {
      return { models: 0, objects: 0, points: 0, labels: 0, sprites: 0, html: 0, lights: 0 };
    }
    const hydrated = { objects: [], points: [], labels: [], sprites: [], html: [], lights: [] };
    let objectCount = 0;
    let pointCount = 0;
    let labelCount = 0;
    let spriteCount = 0;
    let htmlCount = 0;
    let lightCount = 0;
    await Promise.all(models.map(async function(model, modelIndex) {
      const asset = await loadSceneModelAsset(model.src);
      const instanceModel = sceneModelWithAssetFit(model, asset);
      const prefix = model.id || ("scene-model-" + modelIndex);
      const skinInstances = sceneCloneModelSkins(asset.skins);
      const objectIDs = [];
      for (let i = 0; i < asset.objects.length; i += 1) {
        const object = sceneInstantiateModelObject(asset.objects[i], instanceModel, prefix, i, skinInstances);
        if (!object) {
          continue;
        }
        state.objects.set(object.id, object);
        hydrated.objects.push(object.id);
        objectIDs.push(object.id);
        objectCount += 1;
      }
      for (let i = 0; i < asset.points.length; i += 1) {
        const point = sceneInstantiateModelPointsEntry(asset.points[i], instanceModel, prefix, i);
        if (!point || point.count <= 0) {
          continue;
        }
        state.points.push(point);
        hydrated.points.push(point.id);
        pointCount += 1;
      }
      for (let i = 0; i < asset.labels.length; i += 1) {
        const label = sceneInstantiateModelLabel(asset.labels[i], instanceModel, prefix, i);
        if (!label || !label.text.trim()) {
          continue;
        }
        state.labels.set(label.id, label);
        hydrated.labels.push(label.id);
        labelCount += 1;
      }
      for (let i = 0; i < asset.sprites.length; i += 1) {
        const sprite = sceneInstantiateModelSprite(asset.sprites[i], instanceModel, prefix, i);
        if (!sprite) {
          continue;
        }
        state.sprites.set(sprite.id, sprite);
        hydrated.sprites.push(sprite.id);
        spriteCount += 1;
      }
      for (let i = 0; i < asset.html.length; i += 1) {
        const entry = sceneInstantiateModelHTML(asset.html[i], instanceModel, prefix, i);
        if (!entry) {
          continue;
        }
        state.html.set(entry.id, entry);
        hydrated.html.push(entry.id);
        htmlCount += 1;
      }
      for (let i = 0; i < asset.lights.length; i += 1) {
        const light = sceneInstantiateModelLight(asset.lights[i], instanceModel, prefix, i);
        if (!light) {
          continue;
        }
        state.lights.set(light.id, light);
        hydrated.lights.push(light.id);
        lightCount += 1;
      }
      if (sceneModelHasSkins(skinInstances)) {
        await scenePrepareModelSkinPlayback(state, asset, instanceModel, skinInstances, objectIDs);
      } else {
        sceneRegisterStaticModelLiveRecord(state, instanceModel, objectIDs);
      }
    }));
    state._hydratedModelRecords = hydrated;
    return { models: models.length, objects: objectCount, points: pointCount, labels: labelCount, sprites: spriteCount, html: htmlCount, lights: lightCount };
  }

  function normalizeSceneCapabilityTier(value) {
    switch (String(value || "").trim().toLowerCase()) {
      case "constrained":
      case "balanced":
      case "full":
        return String(value).trim().toLowerCase();
      default:
        return "";
    }
  }

  function sceneMediaQueryMatches(query) {
    if (!query || typeof window.matchMedia !== "function") {
      return false;
    }
    try {
      return Boolean(window.matchMedia(query).matches);
    } catch (_error) {
      return false;
    }
  }

  function sceneEnvironmentState() {
    if (window.__gosx
      && window.__gosx.environment
      && typeof window.__gosx.environment.get === "function") {
      return window.__gosx.environment.get();
    }
    return null;
  }

  // sceneExtractCSSVarTransitionTiming scans the original props for materials
  // or environment with a transition config and returns the first timing found.
  // This is stashed on the mount element so the planner can use it as a default
  // when CSS var values change.
  function sceneExtractCSSVarTransitionTiming(props) {
    var scene = props && props.scene;
    if (!scene || typeof scene !== "object") return null;
    var materials = Array.isArray(scene.materials) ? scene.materials : [];
    for (var i = 0; i < materials.length; i++) {
      var m = materials[i];
      if (m && m.transition && typeof m.transition === "object") {
        var update = m.transition.update || m.transition;
        var duration = typeof update.duration === "number" ? update.duration
          : typeof update.duration === "string" ? parseFloat(update.duration) * (update.duration.indexOf("ms") >= 0 ? 1 : 1000)
          : 0;
        if (duration > 0) {
          return { duration: duration, easing: update.easing || "ease-in-out" };
        }
      }
    }
    var env = scene.environment;
    if (env && env.transition && typeof env.transition === "object") {
      var envUpdate = env.transition.update || env.transition;
      var envDuration = typeof envUpdate.duration === "number" ? envUpdate.duration : 0;
      if (envDuration > 0) {
        return { duration: envDuration, easing: envUpdate.easing || "ease-in-out" };
      }
    }
    return null;
  }

  function sceneCapabilityProfile(props) {
    const requestedTier = normalizeSceneCapabilityTier(props && props.capabilityTier);
    const environment = sceneEnvironmentState();
    const navigatorRef = window && window.navigator ? window.navigator : {};
    const webglProbe = sceneBool(props && props.preferWebGL, true) ? sceneProbeWebGLRenderer() : null;
    const softwareWebGL = Boolean(webglProbe && webglProbe.available && webglProbe.software);
    const coarsePointer = environment ? Boolean(environment.coarsePointer) : (sceneMediaQueryMatches("(pointer: coarse)") || sceneMediaQueryMatches("(any-pointer: coarse)"));
    const hover = environment ? Boolean(environment.hover) : (sceneMediaQueryMatches("(hover: hover)") || sceneMediaQueryMatches("(any-hover: hover)"));
    const reducedData = environment ? Boolean(environment.reducedData) : sceneMediaQueryMatches("(prefers-reduced-data: reduce)");
    const lowPower = (environment ? Boolean(environment.lowPower) : false) || softwareWebGL;
    const visualViewportActive = environment ? Boolean(environment.visualViewportActive) : Boolean(window.visualViewport);
    const deviceMemory = sceneNumber(environment && environment.deviceMemory, sceneNumber(navigatorRef && navigatorRef.deviceMemory, 0));
    const hardwareConcurrency = Math.max(0, Math.floor(sceneNumber(environment && environment.hardwareConcurrency, sceneNumber(navigatorRef && navigatorRef.hardwareConcurrency, 0))));
    // Device-capability gate via the single source of truth gosxLowEndHardware
    // (05-document-env), preferring the value already computed in the environment
    // snapshot. This is what previously drifted (OR vs AND) and throttled capable
    // phones to the low-power GPU; deriving it from one helper prevents recurrence.
    const lowEndHardware = environment && typeof environment.lowEndHardware === "boolean"
      ? environment.lowEndHardware
      : gosxLowEndHardware(deviceMemory, hardwareConcurrency);
    const constrainedHardware = lowPower || reducedData || lowEndHardware;

    let tier = requestedTier;
    if (!tier) {
      if ((coarsePointer && constrainedHardware) || reducedData || lowPower) {
        tier = "constrained";
      } else if (coarsePointer) {
        tier = "balanced";
      } else {
        tier = "full";
      }
    }

    return {
      tier,
      coarsePointer,
      hover,
      reducedData,
      lowPower,
      softwareWebGL,
      visualViewportActive,
      deviceMemory,
      hardwareConcurrency,
    };
  }

  function sceneCapabilityWebGLPreference(props, capability) {
    if (sceneRequiresWebGL(props) || sceneForcesWebGL(props)) {
      return "force";
    }
    if (!sceneBool(props && props.preferWebGL, true)) {
      return "disabled";
    }
    if (sceneBool(props && props.preferCanvas, false)) {
      return "avoid";
    }
    if (!capability) {
      return "prefer";
    }
    if (capability.reducedData || capability.lowPower) {
      return "avoid";
    }
    if (capability.tier === "constrained" && capability.coarsePointer) {
      return "avoid";
    }
    return "prefer";
  }

  function sceneCapabilityWebGPUPreference(props, capability) {
    if (sceneRequiresWebGL(props) || sceneForcesWebGL(props) || sceneBool(props && props.preferCanvas, false)) {
      return "disabled";
    }
    if (scenePrefersWebGPU(props)) {
      return "prefer";
    }
    return sceneCapabilityWebGLPreference(props, capability) === "prefer" ? "prefer" : "avoid";
  }

  function sceneRendererFallbackReason(props, capability, rendererKind) {
    if (rendererKind === "webgl") {
      return "";
    }
    if (sceneCapabilityWebGPUPreference(props, capability) === "prefer") {
      return sceneNeedsWebGLForWebGPUCoverage(props) ? "webgpu-feature-gap" : "webgpu-unavailable";
    }
    switch (sceneCapabilityWebGLPreference(props, capability)) {
      case "disabled":
        return "webgl-disabled";
      case "avoid":
        return "environment-constrained";
      default:
        return sceneBool(props && props.preferWebGL, true) ? "webgl-unavailable" : "";
    }
  }

  function sceneCapabilityChanged(prev, next) {
    if (!prev || !next) {
      return true;
    }
    return prev.tier !== next.tier
      || prev.coarsePointer !== next.coarsePointer
      || prev.hover !== next.hover
      || prev.reducedData !== next.reducedData
      || prev.lowPower !== next.lowPower
      || prev.softwareWebGL !== next.softwareWebGL
      || prev.visualViewportActive !== next.visualViewportActive
      || prev.deviceMemory !== next.deviceMemory
      || prev.hardwareConcurrency !== next.hardwareConcurrency;
  }

  function defaultSceneMaxDevicePixelRatio(capability) {
    if (capability && (capability.reducedData || capability.lowPower)) {
      switch (capability.tier) {
        case "constrained":
          return 1.25;
        case "balanced":
          return 1.5;
        default:
          return 1.75;
      }
    }
    switch (capability && capability.tier) {
      case "constrained":
        return 1.5;
      case "balanced":
        return 1.75;
      default:
        return 2;
    }
  }

  function applySceneCapabilityState(mount, props, capability) {
    if (!mount || !capability) {
      return;
    }
    setAttrValue(mount, "data-gosx-scene3d-capability-tier", capability.tier);
    setAttrValue(mount, "data-gosx-scene3d-coarse-pointer", capability.coarsePointer ? "true" : "false");
    setAttrValue(mount, "data-gosx-scene3d-hover", capability.hover ? "true" : "false");
    setAttrValue(mount, "data-gosx-scene3d-reduced-data", capability.reducedData ? "true" : "false");
    setAttrValue(mount, "data-gosx-scene3d-low-power", capability.lowPower ? "true" : "false");
    setAttrValue(mount, "data-gosx-scene3d-software-webgl", capability.softwareWebGL ? "true" : "false");
    setAttrValue(mount, "data-gosx-scene3d-require-webgl", sceneRequiresWebGL(props) ? "true" : "false");
    setAttrValue(mount, "data-gosx-scene3d-visual-viewport", capability.visualViewportActive ? "true" : "false");
    setAttrValue(mount, "data-gosx-scene3d-webgl-preference", sceneCapabilityWebGLPreference(props, capability));
    setAttrValue(mount, "data-gosx-scene3d-webgpu-preference", sceneCapabilityWebGPUPreference(props, capability));
    setAttrValue(mount, "data-gosx-scene3d-device-memory", capability.deviceMemory > 0 ? capability.deviceMemory : "");
    setAttrValue(mount, "data-gosx-scene3d-hardware-concurrency", capability.hardwareConcurrency > 0 ? capability.hardwareConcurrency : "");
  }

  function sceneWebGPULimitKey(name) {
    return String(name || "").trim().toLowerCase().replace(/[^a-z0-9]/g, "");
  }

  function sceneWebGPULimitValue(limits, name) {
    if (!limits || typeof limits !== "object") {
      return "";
    }
    const wanted = sceneWebGPULimitKey(name);
    for (const key of Object.keys(limits)) {
      if (sceneWebGPULimitKey(key) !== wanted) {
        continue;
      }
      const value = Number(limits[key]);
      return Number.isFinite(value) ? value : "";
    }
    return "";
  }

  function sceneWebGPULimitList(limits) {
    if (!limits || typeof limits !== "object") {
      return "";
    }
    return Object.keys(limits).sort().map(function(key) {
      const value = Number(limits[key]);
      return Number.isFinite(value) ? key + "=" + value : "";
    }).filter(Boolean).join(",");
  }

  function applySceneRendererState(mount, renderer, fallbackReason, degraded) {
    if (!mount) {
      return;
    }
    const chosenBackend = renderer && renderer.kind ? renderer.kind : "";
    setAttrValue(mount, "data-gosx-scene3d-renderer", chosenBackend);
    setAttrValue(mount, "data-gosx-scene3d-renderer-fallback", fallbackReason || "");
    // data-gosx-scene3d-backend mirrors the renderer kind (canonical chosen backend name).
    setAttrValue(mount, "data-gosx-scene3d-backend", chosenBackend);
    // data-gosx-scene3d-dropped lists features skipped per the backendCaps degraded verdict.
    setAttrValue(mount, "data-gosx-scene3d-dropped",
      Array.isArray(degraded) && degraded.length > 0 ? degraded.join(",") : "");
    const webgpuDiagnostics = renderer && renderer.kind === "webgpu" && typeof renderer.diagnostics === "function"
      ? renderer.diagnostics()
      : null;
    const webgpuAdapterLimits = webgpuDiagnostics && webgpuDiagnostics.adapterLimits ? webgpuDiagnostics.adapterLimits : null;
    const webgpuDeviceLimits = webgpuDiagnostics && webgpuDiagnostics.deviceLimits ? webgpuDiagnostics.deviceLimits : null;
    const webgpuRequiredLimits = webgpuDiagnostics && webgpuDiagnostics.requiredLimits ? webgpuDiagnostics.requiredLimits : null;
    setAttrValue(mount, "data-gosx-scene3d-webgpu-features", webgpuDiagnostics && Array.isArray(webgpuDiagnostics.requestedFeatures) ? webgpuDiagnostics.requestedFeatures.join(",") : "");
    setAttrValue(mount, "data-gosx-scene3d-webgpu-required-features", webgpuDiagnostics && Array.isArray(webgpuDiagnostics.requiredFeatures) ? webgpuDiagnostics.requiredFeatures.join(",") : "");
    setAttrValue(mount, "data-gosx-scene3d-webgpu-device-features", webgpuDiagnostics && Array.isArray(webgpuDiagnostics.deviceFeatures) ? webgpuDiagnostics.deviceFeatures.join(",") : "");
    setAttrValue(mount, "data-gosx-scene3d-webgpu-required-limits", sceneWebGPULimitList(webgpuRequiredLimits));
    setAttrValue(mount, "data-gosx-scene3d-webgpu-sample-count", webgpuDiagnostics && webgpuDiagnostics.activeSampleCount > 0 ? webgpuDiagnostics.activeSampleCount : "");
    setAttrValue(mount, "data-gosx-scene3d-webgpu-target-format", webgpuDiagnostics && webgpuDiagnostics.targetFormat ? webgpuDiagnostics.targetFormat : "");
    setAttrValue(mount, "data-gosx-scene3d-webgpu-presentation-alpha-mode", webgpuDiagnostics && webgpuDiagnostics.presentationAlphaMode ? webgpuDiagnostics.presentationAlphaMode : "");
    setAttrValue(mount, "data-gosx-scene3d-webgpu-presentation-color-space", webgpuDiagnostics && webgpuDiagnostics.presentationColorSpace ? webgpuDiagnostics.presentationColorSpace : "");
    setAttrValue(mount, "data-gosx-scene3d-webgpu-presentation-tone-mapping", webgpuDiagnostics && webgpuDiagnostics.presentationToneMappingMode ? webgpuDiagnostics.presentationToneMappingMode : "");
    setAttrValue(mount, "data-gosx-scene3d-webgpu-power-preference", webgpuDiagnostics && webgpuDiagnostics.powerPreference ? webgpuDiagnostics.powerPreference : "");
    setAttrValue(mount, "data-gosx-scene3d-webgpu-adapter-limits", sceneWebGPULimitList(webgpuAdapterLimits));
    setAttrValue(mount, "data-gosx-scene3d-webgpu-device-limits", sceneWebGPULimitList(webgpuDeviceLimits));
    setAttrValue(mount, "data-gosx-scene3d-webgpu-adapter-max-texture-2d", sceneWebGPULimitValue(webgpuAdapterLimits, "maxTextureDimension2D"));
    setAttrValue(mount, "data-gosx-scene3d-webgpu-device-max-texture-2d", sceneWebGPULimitValue(webgpuDeviceLimits, "maxTextureDimension2D"));
    setAttrValue(mount, "data-gosx-scene3d-webgpu-adapter-max-buffer-size", sceneWebGPULimitValue(webgpuAdapterLimits, "maxBufferSize"));
    setAttrValue(mount, "data-gosx-scene3d-webgpu-device-max-buffer-size", sceneWebGPULimitValue(webgpuDeviceLimits, "maxBufferSize"));
    setAttrValue(mount, "data-gosx-scene3d-webgpu-adapter-max-compute-workgroup-size-x", sceneWebGPULimitValue(webgpuAdapterLimits, "maxComputeWorkgroupSizeX"));
    setAttrValue(mount, "data-gosx-scene3d-webgpu-device-max-compute-workgroup-size-x", sceneWebGPULimitValue(webgpuDeviceLimits, "maxComputeWorkgroupSizeX"));
    setAttrValue(mount, "data-gosx-scene3d-webgpu-adapter-max-compute-workgroups-per-dimension", sceneWebGPULimitValue(webgpuAdapterLimits, "maxComputeWorkgroupsPerDimension"));
    setAttrValue(mount, "data-gosx-scene3d-webgpu-device-max-compute-workgroups-per-dimension", sceneWebGPULimitValue(webgpuDeviceLimits, "maxComputeWorkgroupsPerDimension"));
    setAttrValue(mount, "data-gosx-scene3d-webgpu-adapter", webgpuDiagnostics && webgpuDiagnostics.adapterInfo ? [
      webgpuDiagnostics.adapterInfo.vendor || "",
      webgpuDiagnostics.adapterInfo.architecture || "",
      webgpuDiagnostics.adapterInfo.device || "",
    ].filter(Boolean).join(" ") : "");
  }

  function showSceneRequiredRendererMessage(mount, props, reason) {
    if (!mount || typeof document === "undefined" || !document || typeof document.createElement !== "function") {
      return;
    }
    const defaultMessage = sceneRequiresWebGL(props)
      ? "Accelerated WebGL is required. Update your browser or enable hardware acceleration."
      : "Scene rendering is unavailable in this browser.";
    const message = String(
      props && props.unsupportedMessage
        ? props.unsupportedMessage
        : defaultMessage
    );
    const wrapper = document.createElement("div");
    wrapper.setAttribute("class", "gosx-scene3d-unsupported");
    wrapper.setAttribute("data-gosx-scene3d-unsupported", "true");
    wrapper.setAttribute("data-gosx-scene3d-unsupported-reason", reason || "webgl-required");
    wrapper.setAttribute("role", "status");
    const text = document.createElement("p");
    text.textContent = message;
    wrapper.appendChild(text);
    mount.appendChild(wrapper);
  }

  function observeSceneCapability(mount, props, capability, onChange) {
    if (!mount || !capability || typeof onChange !== "function") {
      return function() {};
    }
    applySceneCapabilityState(mount, props, capability);
    if (!(window.__gosx.environment && typeof window.__gosx.environment.observe === "function")) {
      return function() {};
    }
    return window.__gosx.environment.observe(function() {
      const next = sceneCapabilityProfile(props);
      if (!sceneCapabilityChanged(capability, next)) {
        return;
      }
      capability.tier = next.tier;
      capability.coarsePointer = next.coarsePointer;
      capability.hover = next.hover;
      capability.reducedData = next.reducedData;
      capability.lowPower = next.lowPower;
      capability.softwareWebGL = next.softwareWebGL;
      capability.visualViewportActive = next.visualViewportActive;
      capability.deviceMemory = next.deviceMemory;
      capability.hardwareConcurrency = next.hardwareConcurrency;
      applySceneCapabilityState(mount, props, capability);
      onChange("capability");
    }, { immediate: false });
  }

  function sceneViewportBase(props) {
    const width = Math.max(240, sceneNumber(props && props.width, 720));
    const height = Math.max(180, sceneNumber(props && props.height, 420));
    const explicitMaxDevicePixelRatio = sceneNumber(props && (props.maxDevicePixelRatio || props.maxPixelRatio), 0);
    return {
      baseWidth: width,
      baseHeight: height,
      aspectRatio: width / Math.max(1, height),
      responsive: sceneBool(props && props.responsive, true),
      explicitMaxDevicePixelRatio,
    };
  }

  function scheduleSceneIdleTask(callback, delayMS) {
    if (typeof callback !== "function") {
      return;
    }
    const delay = Math.max(0, sceneNumber(delayMS, 0));
    const runIdle = function() {
      if (typeof requestIdleCallback === "function") {
        let fired = false;
        const invoke = function(deadline) {
          if (fired) {
            return;
          }
          fired = true;
          callback(deadline);
        };
        requestIdleCallback(invoke, { timeout: 1000 });
        setTimeout(invoke, 1200);
      } else {
        setTimeout(callback, 0);
      }
    };
    if (delay > 0) {
      setTimeout(runIdle, delay);
      return;
    }
    runIdle();
  }

  function sceneCompressionProgressiveDelay(props) {
    const comp = props && props.compression && typeof props.compression === "object" ? props.compression : null;
    if (!comp) {
      return 0;
    }
    return Math.max(0, sceneNumber(
      comp.progressiveDelayMS != null ? comp.progressiveDelayMS : comp.upgradeDelayMS,
      0,
    ));
  }

  function sceneDeferredPostFXDelay(props) {
    return Math.max(0, sceneNumber(
      props && (props.deferPostFXDelayMS != null ? props.deferPostFXDelayMS : props.postFXDelayMS),
      0,
    ));
  }

  function createSceneAdaptiveQualityState(props, base, capability) {
    const adaptiveValue = props && (props.adaptiveQuality != null
      ? props.adaptiveQuality
      : (props.adaptivePerformance != null ? props.adaptivePerformance : props.dynamicQuality));
    const enabled = sceneBool(adaptiveValue, false);
    const targetFrameMS = Math.max(8, Math.min(50, sceneNumber(
      props && (props.adaptiveTargetFrameMS != null ? props.adaptiveTargetFrameMS : props.targetFrameMS),
      capability && capability.tier === "constrained" ? 20 : 16.7,
    )));
    const minProp = props && (props.minDevicePixelRatio != null ? props.minDevicePixelRatio : props.minPixelRatio);
    const minDevicePixelRatio = Math.max(1, Math.min(2, sceneNumber(minProp, 1)));
    const warmupFrames = Math.max(0, Math.floor(sceneNumber(props && props.adaptiveWarmupFrames, 24)));
    const adaptivePostFX = sceneBool(props && props.adaptivePostFX, true);
    return {
      enabled,
      targetFrameMS,
      minDevicePixelRatio,
      warmupFrames,
      adaptivePostFX,
      frameCount: 0,
      badFrames: 0,
      goodFrames: 0,
      ewmaFrameMS: 0,
      lastFrameMS: 0,
      currentMaxDevicePixelRatio: 0,
      postFXSuppressed: false,
      tier: enabled ? "full" : "fixed",
      baseExplicitMaxDevicePixelRatio: sceneNumber(base && base.explicitMaxDevicePixelRatio, 0),
    };
  }

  function sceneAdaptivePostFXSource(sceneState) {
    return Array.isArray(sceneState && sceneState._adaptiveSourcePostEffects)
      ? sceneState._adaptiveSourcePostEffects
      : [];
  }

  function applySceneAdaptiveQualityState(mount, state) {
    if (!mount || !state) {
      return;
    }
    setAttrValue(mount, "data-gosx-scene3d-adaptive-quality", state.enabled ? "true" : "false");
    setAttrValue(mount, "data-gosx-scene3d-quality-tier", state.tier || (state.enabled ? "full" : "fixed"));
    setAttrValue(mount, "data-gosx-scene3d-quality-dpr-cap", state.currentMaxDevicePixelRatio > 0 ? state.currentMaxDevicePixelRatio.toFixed(3) : "");
    setAttrValue(mount, "data-gosx-scene3d-quality-frame-ms", state.lastFrameMS > 0 ? state.lastFrameMS.toFixed(1) : "");
    setAttrValue(mount, "data-gosx-scene3d-quality-postfx-suppressed", state.postFXSuppressed ? "true" : "false");
  }

  function scenePrimeAdaptiveQuality(state, viewport, mount) {
    if (!state || !state.enabled) {
      applySceneAdaptiveQualityState(mount, state);
      return;
    }
    if (!(state.currentMaxDevicePixelRatio > 0)) {
      state.currentMaxDevicePixelRatio = Math.max(
        state.minDevicePixelRatio,
        sceneNumber(viewport && viewport.devicePixelRatio, 1),
      );
    }
    applySceneAdaptiveQualityState(mount, state);
  }

  function sceneApplyAdaptivePostFX(sceneState, adaptiveQuality) {
    if (!sceneState || !adaptiveQuality || !adaptiveQuality.enabled) {
      return false;
    }
    const source = sceneAdaptivePostFXSource(sceneState);
    if (Array.isArray(sceneState._deferredPostEffects) && sceneState._deferredPostEffects.length > 0) {
      sceneState.postEffects = [];
      return false;
    }
    const suppress = adaptiveQuality.adaptivePostFX && adaptiveQuality.postFXSuppressed && source.length > 0;
    const next = suppress ? [] : source;
    const current = Array.isArray(sceneState.postEffects) ? sceneState.postEffects : [];
    if (current.length === next.length && current.every(function(effect, index) { return effect === next[index]; })) {
      return false;
    }
    sceneState.postEffects = next;
    return true;
  }

  function sceneQualityTierForDPR(state) {
    if (!state || !state.enabled) {
      return "fixed";
    }
    if (state.postFXSuppressed) {
      return "survival";
    }
    const current = sceneNumber(state.currentMaxDevicePixelRatio, 1);
    const min = sceneNumber(state.minDevicePixelRatio, 1);
    if (current <= min + 0.01) {
      return "lean";
    }
    if (state.badFrames > 0 || current < sceneNumber(state.baseExplicitMaxDevicePixelRatio, current)) {
      return "balanced";
    }
    return "full";
  }

  function sceneUpdateAdaptiveQuality(state, mount, sceneState, viewport, frameStart) {
    if (!state || !state.enabled) {
      return false;
    }
    const now = typeof performance !== "undefined" && performance.now ? performance.now() : Date.now();
    const frameMS = Math.max(0, now - sceneNumber(frameStart, now));
    if (!isFinite(frameMS)) {
      return false;
    }
    state.frameCount += 1;
    state.lastFrameMS = frameMS;
    state.ewmaFrameMS = state.ewmaFrameMS > 0
      ? state.ewmaFrameMS * 0.84 + frameMS * 0.16
      : frameMS;

    if (state.frameCount <= state.warmupFrames) {
      applySceneAdaptiveQualityState(mount, state);
      return false;
    }

    const target = Math.max(8, sceneNumber(state.targetFrameMS, 16.7));
    const missesBudget = frameMS > target * 1.35 || state.ewmaFrameMS > target * 1.18;
    const severeMiss = frameMS > target * 2.4;
    if (missesBudget) {
      state.badFrames += severeMiss ? 4 : 1;
      state.goodFrames = 0;
    } else if (state.ewmaFrameMS < target * 0.9) {
      state.goodFrames += 1;
      state.badFrames = 0;
    }

    let changed = false;
    let reason = "";
    if (severeMiss || state.badFrames >= 10) {
      const minDPR = Math.max(1, sceneNumber(state.minDevicePixelRatio, 1));
      const currentDPR = state.currentMaxDevicePixelRatio > 0
        ? state.currentMaxDevicePixelRatio
        : sceneNumber(viewport && viewport.devicePixelRatio, 1);
      if (currentDPR > minDPR + 0.01) {
        const step = severeMiss ? 0.25 : 0.125;
        state.currentMaxDevicePixelRatio = Math.max(minDPR, Math.round((currentDPR - step) * 1000) / 1000);
        state.badFrames = 0;
        changed = true;
        reason = "dpr";
      } else if (state.adaptivePostFX && !state.postFXSuppressed && sceneAdaptivePostFXSource(sceneState).length > 0) {
        state.postFXSuppressed = true;
        state.badFrames = 0;
        changed = true;
        reason = "postfx";
      }
    }
    state.tier = sceneQualityTierForDPR(state);
    if (changed) {
      sceneApplyAdaptivePostFX(sceneState, state);
      applyScenePostFXState(mount, sceneState);
      applySceneAdaptiveQualityState(mount, state);
      gosxSceneEmit("warn", "adaptive-quality-downshift", {
        reason,
        frameMS,
        ewmaFrameMS: state.ewmaFrameMS,
        targetFrameMS: state.targetFrameMS,
        dprCap: state.currentMaxDevicePixelRatio,
        postFXSuppressed: state.postFXSuppressed,
      });
      return true;
    }
    applySceneAdaptiveQualityState(mount, state);
    return false;
  }

  function applyScenePostFXState(mount, state) {
    if (!mount || !state) {
      return;
    }
    const deferred = Array.isArray(state._deferredPostEffects) && state._deferredPostEffects.length > 0;
    const enabled = Array.isArray(state.postEffects) && state.postEffects.length > 0;
    setAttrValue(mount, "data-gosx-scene3d-postfx", deferred ? "deferred" : (enabled ? "enabled" : "none"));
  }

  function createSceneStatsOverlay(mount, enabled) {
    if (!mount || !enabled || typeof document.createElement !== "function") {
      return null;
    }
    const element = document.createElement("div");
    element.setAttribute("data-gosx-scene3d-stats", "true");
    element.setAttribute("aria-hidden", "true");
    element.style.position = "absolute";
    element.style.left = "8px";
    element.style.top = "8px";
    element.style.zIndex = "8";
    element.style.pointerEvents = "none";
    element.style.font = "11px/1.35 ui-monospace, SFMono-Regular, Menlo, Consolas, monospace";
    element.style.color = "#d9f7ff";
    element.style.background = "rgba(4, 10, 16, 0.72)";
    element.style.border = "1px solid rgba(141, 225, 255, 0.22)";
    element.style.borderRadius = "6px";
    element.style.padding = "6px 7px";
    element.style.whiteSpace = "pre";
    element.style.backdropFilter = "blur(6px)";
    mount.appendChild(element);
    return {
      element,
      update(bundle, frameStart, renderer, viewport) {
        const now = typeof performance !== "undefined" && performance.now ? performance.now() : Date.now();
        const frameMS = Math.max(0, now - sceneNumber(frameStart, now));
        const fps = frameMS > 0 ? Math.min(999, 1000 / frameMS) : 0;
        const meshes = Array.isArray(bundle && bundle.meshObjects) ? bundle.meshObjects.length : 0;
        const world = Array.isArray(bundle && bundle.objects) ? bundle.objects.length : 0;
        const points = Array.isArray(bundle && bundle.points) ? bundle.points.length : 0;
        const instanced = Array.isArray(bundle && bundle.instancedMeshes) ? bundle.instancedMeshes.length : 0;
        const surfaces = Array.isArray(bundle && bundle.surfaces) ? bundle.surfaces.length : 0;
        const drawCalls = meshes + world + points + instanced + surfaces;
        const materialCount = Array.isArray(bundle && bundle.materials) ? bundle.materials.length : 0;
        element.textContent =
          "fps " + fps.toFixed(0) + "  ms " + frameMS.toFixed(1) + "\n" +
          "draw " + drawCalls + "  mat " + materialCount + "\n" +
          "meshV " + Math.floor(sceneNumber(bundle && bundle.worldMeshVertexCount, 0)) +
          "  lineV " + Math.floor(sceneNumber(bundle && bundle.worldVertexCount, 0)) + "\n" +
          String(renderer && (renderer.type || renderer.kind) || "renderer") +
          "  " + Math.floor(sceneNumber(viewport && viewport.cssWidth, 0)) + "x" + Math.floor(sceneNumber(viewport && viewport.cssHeight, 0));
      },
      dispose() {
        if (element.parentNode === mount) {
          mount.removeChild(element);
        }
      },
    };
  }

  function createSceneInspectorOverlay(mount, enabled, readSnapshot) {
    if (!mount || !enabled || typeof document.createElement !== "function") {
      return null;
    }
    const element = document.createElement("div");
    element.setAttribute("data-gosx-scene3d-inspector", "true");
    element.setAttribute("aria-hidden", "true");
    element.style.position = "absolute";
    element.style.right = "8px";
    element.style.top = "8px";
    element.style.zIndex = "9";
    element.style.pointerEvents = "none";
    element.style.font = "11px/1.35 ui-monospace, SFMono-Regular, Menlo, Consolas, monospace";
    element.style.color = "#edf8f2";
    element.style.background = "rgba(3, 12, 10, 0.78)";
    element.style.border = "1px solid rgba(139, 235, 190, 0.24)";
    element.style.borderRadius = "6px";
    element.style.padding = "7px 8px";
    element.style.whiteSpace = "pre";
    element.style.minWidth = "190px";
    element.style.maxWidth = "280px";
    element.style.backdropFilter = "blur(6px)";
    mount.setAttribute("data-gosx-scene3d-inspector-enabled", "true");
    mount.appendChild(element);

    function update() {
      const snapshot = typeof readSnapshot === "function" ? readSnapshot() : null;
      if (!snapshot) {
        element.textContent = "Scene3D\npending";
        return;
      }
      const counts = snapshot.counts || {};
      const viewport = snapshot.viewport || {};
      const resources = snapshot.gpuResources || {};
      const renderLoop = snapshot.renderLoop || {};
      const htmlTextures = resources.htmlTextures || {};
      const diagnostics = Array.isArray(snapshot.diagnostics) ? snapshot.diagnostics : [];
      let warnCount = 0;
      let errorCount = 0;
      for (let i = 0; i < diagnostics.length; i += 1) {
        const severity = String(diagnostics[i] && diagnostics[i].severity || "").toLowerCase();
        if (severity === "warn" || severity === "warning") {
          warnCount += 1;
        } else if (severity === "error" || severity === "fatal") {
          errorCount += 1;
        }
      }
      const renderer = snapshot.renderer || "unknown";
      const fallback = snapshot.fallbackReason ? " fallback " + snapshot.fallbackReason : "";
      const lastPick = snapshot.lastPick || null;
      const pickTarget = lastPick && (lastPick.targetID || lastPick.objectID || lastPick.hoverID || lastPick.downID || lastPick.id);
      const pickType = lastPick && lastPick.type ? String(lastPick.type) : "pick";
      const meshCount = sceneNumber(counts.meshObjects, 0) + sceneNumber(counts.worldObjects, 0);
      const lines = [
        "Scene3D",
        "backend " + renderer + fallback,
        "ready " + (snapshot.ready ? "yes" : "no") + "  active " + (snapshot.active ? "yes" : "no"),
        "loop " + (renderLoop.active ? "active" : "stopped") + "  " + (renderLoop.reason || "unknown"),
        "view " + Math.floor(sceneNumber(viewport.cssWidth, 0)) + "x" + Math.floor(sceneNumber(viewport.cssHeight, 0)) +
          "  dpr " + sceneNumber(viewport.devicePixelRatio, 1).toFixed(2),
        "draw " + Math.floor(sceneNumber(counts.drawCalls, 0)) + "  mat " + Math.floor(sceneNumber(counts.materials, 0)),
        "mesh " + Math.floor(meshCount) + "  inst " + Math.floor(sceneNumber(counts.instancedMeshes, 0)),
        "html " + Math.floor(sceneNumber(counts.html, 0)) + "  tex " +
          Math.floor(sceneNumber(htmlTextures.ready, 0)) + "/" + Math.floor(sceneNumber(htmlTextures.count, 0)),
        "diag " + diagnostics.length + "  warn " + warnCount + "  err " + errorCount,
      ];
      if (pickTarget) {
        lines.push("pick " + pickType + " " + String(pickTarget));
      }
      element.textContent = lines.join("\n");
    }

    update();
    return {
      element,
      update,
      dispose() {
        mount.removeAttribute("data-gosx-scene3d-inspector-enabled");
        if (element.parentNode === mount) {
          mount.removeChild(element);
        }
      },
    };
  }

  function sceneViewportDevicePixelRatio(props, maxDevicePixelRatio) {
    const environment = sceneEnvironmentState();
    const preferred = sceneNumber(
      props && (props.devicePixelRatio || props.pixelRatio),
      sceneNumber(window && window.devicePixelRatio, sceneNumber(environment && environment.devicePixelRatio, 1)),
    );
    return Math.max(1, Math.min(Math.max(1, maxDevicePixelRatio || 1), preferred));
  }

  function sceneViewportFromMount(mount, props, base, canvas, capability, adaptiveQuality) {
    let cssWidth = base.baseWidth;
    let cssHeight = base.baseHeight;
    const useMeasuredHeight = sceneBool(props && (props.fillHeight || props.responsiveHeight), false);
    if (base.responsive) {
      const mountRect = mount && typeof mount.getBoundingClientRect === "function"
        ? mount.getBoundingClientRect()
        : null;
      const canvasRect = canvas && typeof canvas.getBoundingClientRect === "function"
        ? canvas.getBoundingClientRect()
        : null;
      const measuredCanvasWidth = sceneNumber(canvasRect && canvasRect.width, 0);
      const measuredMountWidth = sceneNumber(mountRect && mountRect.width, 0);
      if (measuredCanvasWidth > 0 && (measuredMountWidth <= 0 || measuredCanvasWidth <= measuredMountWidth * 1.5)) {
        cssWidth = measuredCanvasWidth;
      } else if (measuredMountWidth > 0) {
        cssWidth = measuredMountWidth;
      }
      const measuredHeight = measuredCanvasWidth > 0 && (measuredMountWidth <= 0 || measuredCanvasWidth <= measuredMountWidth * 1.5)
        ? sceneNumber(canvasRect && canvasRect.height, 0)
        : sceneNumber(mountRect && mountRect.height, 0);
      if (useMeasuredHeight && measuredHeight > 0) {
        cssHeight = measuredHeight;
      } else if (cssWidth > 0) {
        cssHeight = cssWidth / Math.max(0.0001, base.aspectRatio);
      }
    }
    cssWidth = Math.max(1, Math.round(cssWidth));
    cssHeight = Math.max(1, Math.round(cssHeight));
    const capabilityMaxDevicePixelRatio = defaultSceneMaxDevicePixelRatio(capability);
    let maxDevicePixelRatio = Math.max(
      1,
      base.explicitMaxDevicePixelRatio > 0
        ? Math.min(base.explicitMaxDevicePixelRatio, capabilityMaxDevicePixelRatio)
        : capabilityMaxDevicePixelRatio,
    );
    if (adaptiveQuality && adaptiveQuality.enabled && adaptiveQuality.currentMaxDevicePixelRatio > 0) {
      maxDevicePixelRatio = Math.max(
        1,
        Math.min(maxDevicePixelRatio, adaptiveQuality.currentMaxDevicePixelRatio),
      );
    }
    const devicePixelRatio = sceneViewportDevicePixelRatio(props, maxDevicePixelRatio);
    return {
      cssWidth,
      cssHeight,
      devicePixelRatio,
      pixelWidth: Math.max(1, Math.round(cssWidth * devicePixelRatio)),
      pixelHeight: Math.max(1, Math.round(cssHeight * devicePixelRatio)),
    };
  }

  function sceneViewportChanged(prev, next) {
    if (!prev || !next) {
      return true;
    }
    return prev.cssWidth !== next.cssWidth
      || prev.cssHeight !== next.cssHeight
      || prev.pixelWidth !== next.pixelWidth
      || prev.pixelHeight !== next.pixelHeight
      || Math.abs(sceneNumber(prev.devicePixelRatio, 1) - sceneNumber(next.devicePixelRatio, 1)) > 0.001;
  }

  function sceneViewportEnvironmentSignature(environment) {
    if (!environment || typeof environment !== "object") {
      return "";
    }
    return [
      sceneNumber(environment.devicePixelRatio, 1).toFixed(3),
      Math.round(sceneNumber(environment.viewportWidth, 0)),
      Math.round(sceneNumber(environment.viewportHeight, 0)),
      Math.round(sceneNumber(environment.visualViewportWidth, 0)),
      Math.round(sceneNumber(environment.visualViewportHeight, 0)),
      environment.visualViewportActive ? "1" : "0",
    ].join("|");
  }

  function applySceneViewport(mount, canvas, labelLayer, viewport, base) {
    if (!mount || !canvas || !viewport) {
      return viewport;
    }
    setAttrValue(mount, "data-gosx-scene3d-css-width", viewport.cssWidth);
    setAttrValue(mount, "data-gosx-scene3d-css-height", viewport.cssHeight);
    setAttrValue(mount, "data-gosx-scene3d-pixel-ratio", viewport.devicePixelRatio);
    setStyleValue(mount.style, "--gosx-scene-css-width", viewport.cssWidth + "px");
    setStyleValue(mount.style, "--gosx-scene-css-height", viewport.cssHeight + "px");
    setStyleValue(mount.style, "--gosx-scene-pixel-ratio", String(viewport.devicePixelRatio));
    canvas.width = viewport.pixelWidth;
    canvas.height = viewport.pixelHeight;
    canvas.setAttribute("width", String(viewport.pixelWidth));
    canvas.setAttribute("height", String(viewport.pixelHeight));
    if (labelLayer) {
      const mountRect = typeof mount.getBoundingClientRect === "function" ? mount.getBoundingClientRect() : null;
      const canvasRect = typeof canvas.getBoundingClientRect === "function" ? canvas.getBoundingClientRect() : null;
      const left = mountRect && canvasRect ? Math.max(0, sceneNumber(canvasRect.left, 0) - sceneNumber(mountRect.left, 0)) : 0;
      const top = mountRect && canvasRect ? Math.max(0, sceneNumber(canvasRect.top, 0) - sceneNumber(mountRect.top, 0)) : 0;
      labelLayer.style.left = left + "px";
      labelLayer.style.top = top + "px";
      labelLayer.style.right = "auto";
      labelLayer.style.bottom = "auto";
      labelLayer.style.width = viewport.cssWidth + "px";
      labelLayer.style.height = viewport.cssHeight + "px";
    }
    if (base && !base.responsive) {
      canvas.style.width = viewport.cssWidth + "px";
      canvas.style.height = viewport.cssHeight + "px";
    } else {
      canvas.style.width = "100%";
      canvas.style.height = "auto";
    }
    return viewport;
  }

  function observeSceneViewport(mount, refresh) {
    if (!mount || typeof refresh !== "function") {
      return function() {};
    }
    let resizeObserver = null;
    let windowResizeListener = null;
    let stopEnvironment = null;

    // Coalesce ResizeObserver / window.resize fires via a microtask flag so
    // rapid-fire events (e.g., Firefox subpixel canvas dim fluctuations during
    // scroll) collapse into at most one refresh per synchronous burst. Without
    // this guard, each fire calls scheduleRender unconditionally and piles
    // renders on top of the already-running rAF loop — observed as progressive
    // scroll jank on Firefox. A microtask-based flag is used rather than rAF
    // so the dedup doesn't leak a pending rAF across viewport activation
    // transitions (see runtime.test.js offscreen-rerender deferral test).
    var resizeRefreshPending = false;
    function scheduleResizeRefresh() {
      if (resizeRefreshPending) {
        return;
      }
      resizeRefreshPending = true;
      if (typeof Promise === "function") {
        Promise.resolve().then(function() {
          resizeRefreshPending = false;
          refresh("resize");
        });
      } else {
        resizeRefreshPending = false;
        refresh("resize");
      }
    }

    if (typeof ResizeObserver === "function") {
      resizeObserver = new ResizeObserver(scheduleResizeRefresh);
      if (typeof resizeObserver.observe === "function") {
        resizeObserver.observe(mount);
      }
    } else if (typeof window.addEventListener === "function") {
      windowResizeListener = scheduleResizeRefresh;
      window.addEventListener("resize", windowResizeListener);
    }

    if (window.__gosx.environment && typeof window.__gosx.environment.observe === "function") {
      let environmentSignature = sceneViewportEnvironmentSignature(sceneEnvironmentState());
      stopEnvironment = window.__gosx.environment.observe(function(environment) {
        const nextSignature = sceneViewportEnvironmentSignature(environment);
        if (environmentSignature === nextSignature) {
          return;
        }
        environmentSignature = nextSignature;
        refresh("environment");
      }, { immediate: false });
    }

    return function() {
      if (resizeObserver && typeof resizeObserver.disconnect === "function") {
        resizeObserver.disconnect();
      }
      if (windowResizeListener && typeof window.removeEventListener === "function") {
        window.removeEventListener("resize", windowResizeListener);
      }
      if (typeof stopEnvironment === "function") {
        stopEnvironment();
      }
    };
  }

  function initialSceneLifecycleState() {
    const environment = sceneEnvironmentState();
    return {
      pageVisible: environment ? Boolean(environment.pageVisible) : String(document && document.visibilityState || "visible").toLowerCase() !== "hidden",
      inViewport: true,
    };
  }

  function initialSceneMotionState(props) {
    const respectReducedMotion = sceneBool(props && props.respectReducedMotion, true);
    const environment = sceneEnvironmentState();
    return {
      respectReducedMotion,
      reducedMotion: respectReducedMotion && environment
        ? Boolean(environment.reducedMotion)
        : sceneMediaQueryMatches("(prefers-reduced-motion: reduce)"),
    };
  }

  function applySceneMotionState(mount, motion) {
    if (!mount || !motion) {
      return;
    }
    setAttrValue(mount, "data-gosx-scene3d-reduced-motion", motion.reducedMotion ? "true" : "false");
  }

  function observeSceneMotion(mount, motion, onChange) {
    if (!mount || !motion || typeof onChange !== "function") {
      return function() {};
    }

    applySceneMotionState(mount, motion);
    if (!motion.respectReducedMotion || !(window.__gosx.environment && typeof window.__gosx.environment.observe === "function")) {
      return function() {};
    }

    return window.__gosx.environment.observe(function(environment) {
      const next = Boolean(environment && environment.reducedMotion);
      if (motion.reducedMotion === next) {
        return;
      }
      motion.reducedMotion = next;
      applySceneMotionState(mount, motion);
      onChange("motion");
    }, { immediate: false });
  }

  function applySceneLifecycleState(mount, lifecycle) {
    if (!mount || !lifecycle) {
      return;
    }
    setAttrValue(mount, "data-gosx-scene3d-page-visible", lifecycle.pageVisible ? "true" : "false");
    setAttrValue(mount, "data-gosx-scene3d-in-viewport", lifecycle.inViewport ? "true" : "false");
    setAttrValue(mount, "data-gosx-scene3d-active", lifecycle.pageVisible && lifecycle.inViewport ? "true" : "false");
  }

  function sceneLifecyclePinnedToViewport(mount) {
    if (!mount || typeof window.getComputedStyle !== "function") {
      return false;
    }
    try {
      const position = String(window.getComputedStyle(mount).position || "").toLowerCase();
      return position === "fixed";
    } catch (_error) {
      return false;
    }
  }

  function observeSceneLifecycle(mount, lifecycle, onChange) {
    if (!mount || !lifecycle || typeof onChange !== "function") {
      return function() {};
    }

    let stopIntersection = null;
    let stopEnvironment = null;

    if (sceneLifecyclePinnedToViewport(mount)) {
      lifecycle.inViewport = true;
    } else if (typeof IntersectionObserver === "function") {
      const observer = new IntersectionObserver(function(entries) {
        for (const entry of entries || []) {
          if (!entry || entry.target !== mount) {
            continue;
          }
          const next = entry.isIntersecting !== false && sceneNumber(entry.intersectionRatio, 1) > 0;
          if (lifecycle.inViewport === next) {
            continue;
          }
          lifecycle.inViewport = next;
          applySceneLifecycleState(mount, lifecycle);
          onChange("intersection");
        }
      }, { threshold: [0, 0.01, 0.25] });
      if (typeof observer.observe === "function") {
        observer.observe(mount);
      }
      stopIntersection = function() {
        if (typeof observer.disconnect === "function") {
          observer.disconnect();
        }
      };
    }

    if (window.__gosx.environment && typeof window.__gosx.environment.observe === "function") {
      stopEnvironment = window.__gosx.environment.observe(function(environment) {
        const next = Boolean(environment && environment.pageVisible);
        if (lifecycle.pageVisible === next) {
          return;
        }
        lifecycle.pageVisible = next;
        applySceneLifecycleState(mount, lifecycle);
        onChange("visibility");
      }, { immediate: false });
    }

    applySceneLifecycleState(mount, lifecycle);
    return function() {
      if (stopIntersection) {
        stopIntersection();
      }
      if (typeof stopEnvironment === "function") {
        stopEnvironment();
      }
    };
  }

  function sceneLabelLayoutKey(label) {
    return [
      gosxTextLayoutRevision(),
      label.text,
      label.font,
      sceneNumber(label.maxWidth, 180),
      Math.max(0, Math.floor(sceneNumber(label.maxLines, 0))),
      normalizeTextLayoutOverflow(label.overflow),
      normalizeSceneLabelWhiteSpace(label.whiteSpace),
      sceneNumber(label.lineHeight, 18),
      normalizeSceneLabelAlign(label.textAlign),
    ].join("\n");
  }

  function sceneMeasureTextWidth(font, text) {
    if (typeof window.__gosx_measure_text_batch !== "function") {
      return String(text || "").length * 8;
    }
    try {
      const raw = window.__gosx_measure_text_batch(font, JSON.stringify([String(text || "")]));
      const widths = typeof raw === "string" ? JSON.parse(raw) : raw;
      return Array.isArray(widths) && widths.length > 0 ? sceneNumber(widths[0], String(text || "").length * 8) : String(text || "").length * 8;
    } catch (_error) {
      return String(text || "").length * 8;
    }
  }

  function fallbackSceneLabelLayout(label) {
    return layoutBrowserText(
      String(label.text || ""),
      label.font,
      sceneNumber(label.maxWidth, 180),
      normalizeSceneLabelWhiteSpace(label.whiteSpace),
      sceneNumber(label.lineHeight, 18),
      {
        maxLines: Math.max(0, Math.floor(sceneNumber(label.maxLines, 0))),
        overflow: normalizeTextLayoutOverflow(label.overflow),
      },
    );
  }

  function layoutSceneLabel(label, layoutCache) {
    const revision = gosxTextLayoutRevision();
    if (layoutCache.__gosxRevision !== revision) {
      layoutCache.clear();
      layoutCache.__gosxRevision = revision;
    }
    const cacheKey = sceneLabelLayoutKey(label);
    if (layoutCache.has(cacheKey)) {
      return {
        key: cacheKey,
        value: layoutCache.get(cacheKey),
      };
    }

    let layout = null;
    if (typeof window.__gosx_text_layout === "function") {
      try {
        layout = window.__gosx_text_layout(
          label.text,
          label.font,
          sceneNumber(label.maxWidth, 180),
          normalizeSceneLabelWhiteSpace(label.whiteSpace),
          sceneNumber(label.lineHeight, 18),
          {
            maxLines: Math.max(0, Math.floor(sceneNumber(label.maxLines, 0))),
            overflow: normalizeTextLayoutOverflow(label.overflow),
          },
        );
      } catch (error) {
        console.error("[gosx] scene label layout failed:", error);
      }
    }

    if (!layout || !Array.isArray(layout.lines)) {
      layout = fallbackSceneLabelLayout(label);
    }
    if (layoutCache.size >= sceneLabelLayoutCacheLimit) {
      const oldest = layoutCache.keys().next();
      if (!oldest.done) {
        layoutCache.delete(oldest.value);
      }
    }
    layoutCache.set(cacheKey, layout);
    return {
      key: cacheKey,
      value: layout,
    };
  }

  const sceneLabelPaddingX = 10;
  const sceneLabelPaddingY = 8;

  function sceneLabelBoxMetrics(label, layout) {
    const contentWidth = Math.max(
      1,
      Math.min(
        sceneNumber(label.maxWidth, 180),
        Math.max(1, Math.ceil(sceneNumber(layout && layout.maxLineWidth, 0) || sceneMeasureTextWidth(label.font, label.text)))
      )
    );
    const contentHeight = Math.max(
      sceneNumber(label.lineHeight, 18),
      Math.ceil(sceneNumber(layout && layout.height, sceneNumber(label.lineHeight, 18)))
    );
    return {
      contentWidth,
      contentHeight,
      totalWidth: contentWidth + (sceneLabelPaddingX * 2),
      totalHeight: contentHeight + (sceneLabelPaddingY * 2),
      maxTotalWidth: Math.max(contentWidth + (sceneLabelPaddingX * 2), sceneNumber(label.maxWidth, 180) + (sceneLabelPaddingX * 2)),
    };
  }

  function sceneLabelBounds(label, metrics) {
    const anchorX = sceneNumber(label.anchorX, 0.5);
    const anchorY = sceneNumber(label.anchorY, 1);
    const anchorPointX = sceneNumber(label.position && label.position.x, 0) + sceneNumber(label.offsetX, 0);
    const anchorPointY = sceneNumber(label.position && label.position.y, 0) + sceneNumber(label.offsetY, 0);
    const left = anchorPointX - (anchorX * metrics.totalWidth);
    const top = anchorPointY - (anchorY * metrics.totalHeight);
    return {
      left,
      top,
      right: left + metrics.totalWidth,
      bottom: top + metrics.totalHeight,
      anchor: { x: anchorPointX, y: anchorPointY },
      center: { x: left + (metrics.totalWidth / 2), y: top + (metrics.totalHeight / 2) },
    };
  }

  function sceneRectArea(box) {
    if (!box) {
      return 0;
    }
    return Math.max(0, box.right - box.left) * Math.max(0, box.bottom - box.top);
  }

  function sceneRectOverlapArea(a, b) {
    if (!a || !b) {
      return 0;
    }
    const overlapX = Math.max(0, Math.min(a.right, b.maxX == null ? b.right : b.maxX) - Math.max(a.left, b.minX == null ? b.left : b.minX));
    const overlapY = Math.max(0, Math.min(a.bottom, b.maxY == null ? b.bottom : b.maxY) - Math.max(a.top, b.minY == null ? b.top : b.minY));
    return overlapX * overlapY;
  }

  function sceneRectsIntersect(a, b) {
    return sceneRectOverlapArea(a, b) > 0;
  }

  function sceneBoundsContainPoint(bounds, point) {
    if (!bounds || !point) {
      return false;
    }
    return point.x >= bounds.minX && point.x <= bounds.maxX && point.y >= bounds.minY && point.y <= bounds.maxY;
  }

  function buildSceneLabelOccluders(bundle, width, height) {
    if (!bundle || !bundle.camera || !Array.isArray(bundle.objects) || !bundle.objects.length) {
      return [];
    }
    const occluders = [];
    for (const object of bundle.objects) {
      if (!object || object.viewCulled) {
        continue;
      }
      const segments = sceneProjectedObjectSegments(bundle, object, width, height);
      if (!segments.length) {
        continue;
      }
      const bounds = sceneProjectedSegmentsBounds(segments);
      if (!bounds) {
        continue;
      }
      occluders.push({
        depth: sceneNumber(object.depthCenter, sceneObjectDepthCenter(object, bundle.camera)),
        bounds,
        hull: sceneProjectedObjectHull(segments),
      });
    }
    occluders.sort(function(a, b) {
      return a.depth - b.depth;
    });
    return occluders;
  }

  function sceneLabelOccluded(entry, occluders) {
    return sceneOverlayOccluded(entry, occluders, entry && entry.label && entry.label.occlude);
  }

  function sceneOverlayOccluded(entry, occluders, occlude) {
    if (!entry || !occlude || !Array.isArray(occluders) || !occluders.length) {
      return false;
    }
    const overlayDepth = sceneNumber(entry && entry.depth, 0);
    for (const occluder of occluders) {
      if (occluder.depth > overlayDepth + 0.05) {
        continue;
      }
      if (!sceneRectsIntersect(entry.box, occluder.bounds)) {
        continue;
      }
      if (scenePointInPolygon(entry.box.anchor, occluder.hull) || sceneBoundsContainPoint(occluder.bounds, entry.box.anchor)) {
        return true;
      }
      if (scenePointInPolygon(entry.box.center, occluder.hull)) {
        return true;
      }
      const overlapRatio = sceneRectOverlapArea(entry.box, occluder.bounds) / Math.max(1, sceneRectArea(entry.box));
      if (overlapRatio >= 0.28) {
        return true;
      }
    }
    return false;
  }

  function sceneLabelPriorityCompare(a, b) {
    const priorityDiff = sceneNumber(b && b.label && b.label.priority, 0) - sceneNumber(a && a.label && a.label.priority, 0);
    if (Math.abs(priorityDiff) > 0.001) {
      return priorityDiff;
    }
    const depthDiff = sceneNumber(a && a.label && a.label.depth, 0) - sceneNumber(b && b.label && b.label.depth, 0);
    if (Math.abs(depthDiff) > 0.001) {
      return depthDiff;
    }
    return sceneNumber(a && a.order, 0) - sceneNumber(b && b.order, 0);
  }

  function prepareSceneLabelEntries(bundle, layoutCache, width, height) {
    const labels = bundle && Array.isArray(bundle.labels) ? bundle.labels : [];
    const occluders = buildSceneLabelOccluders(bundle, width, height);
    const entries = [];
    for (let index = 0; index < labels.length; index += 1) {
      const label = labels[index];
      if (!label || typeof label.text !== "string" || label.text.trim() === "") {
        continue;
      }
      const layout = layoutSceneLabel(label, layoutCache);
      const metrics = sceneLabelBoxMetrics(label, layout.value);
      const box = sceneLabelBounds(label, metrics);
      entries.push({
        id: label.id || ("scene-label-" + index),
        order: index,
        label,
        depth: sceneNumber(label.depth, 0),
        layoutKey: layout.key,
        layout: layout.value,
        metrics,
        box,
        occluded: false,
        hidden: false,
      });
    }

    const sorted = entries.slice().sort(sceneLabelPriorityCompare);
    const occupied = [];
    for (const entry of sorted) {
      entry.occluded = sceneLabelOccluded(entry, occluders);
      if (entry.occluded) {
        entry.hidden = true;
        continue;
      }
      if (normalizeSceneLabelCollision(entry.label.collision) !== "allow") {
        for (const prior of occupied) {
          if (sceneRectsIntersect(entry.box, prior)) {
            entry.hidden = true;
            break;
          }
        }
      }
      if (!entry.hidden) {
        occupied.push(entry.box);
      }
    }

    return entries;
  }

  function sceneSpriteBounds(sprite) {
    const anchorX = sceneNumber(sprite.anchorX, 0.5);
    const anchorY = sceneNumber(sprite.anchorY, 0.5);
    const spriteWidth = Math.max(1, sceneNumber(sprite.width, 1));
    const spriteHeight = Math.max(1, sceneNumber(sprite.height, 1));
    const anchorPointX = sceneNumber(sprite.position && sprite.position.x, 0) + sceneNumber(sprite.offsetX, 0);
    const anchorPointY = sceneNumber(sprite.position && sprite.position.y, 0) + sceneNumber(sprite.offsetY, 0);
    const left = anchorPointX - (anchorX * spriteWidth);
    const top = anchorPointY - (anchorY * spriteHeight);
    return {
      left,
      top,
      right: left + spriteWidth,
      bottom: top + spriteHeight,
      anchor: { x: anchorPointX, y: anchorPointY },
      center: { x: left + (spriteWidth / 2), y: top + (spriteHeight / 2) },
    };
  }

  function sceneSpritePriorityCompare(a, b) {
    const priorityDiff = sceneNumber(b && b.sprite && b.sprite.priority, 0) - sceneNumber(a && a.sprite && a.sprite.priority, 0);
    if (Math.abs(priorityDiff) > 0.001) {
      return priorityDiff;
    }
    const depthDiff = sceneNumber(a && a.sprite && a.sprite.depth, 0) - sceneNumber(b && b.sprite && b.sprite.depth, 0);
    if (Math.abs(depthDiff) > 0.001) {
      return depthDiff;
    }
    return sceneNumber(a && a.order, 0) - sceneNumber(b && b.order, 0);
  }

  function prepareSceneSpriteEntries(bundle, width, height) {
    const sprites = bundle && Array.isArray(bundle.sprites) ? bundle.sprites : [];
    const occluders = buildSceneLabelOccluders(bundle, width, height);
    const entries = [];
    for (let index = 0; index < sprites.length; index += 1) {
      const sprite = sprites[index];
      if (!sprite || typeof sprite.src !== "string" || sprite.src.trim() === "") {
        continue;
      }
      const box = sceneSpriteBounds(sprite);
      entries.push({
        id: sprite.id || ("scene-sprite-" + index),
        order: index,
        sprite,
        depth: sceneNumber(sprite.depth, 0),
        box,
        occluded: false,
        hidden: false,
      });
    }
    const sorted = entries.slice().sort(sceneSpritePriorityCompare);
    for (const entry of sorted) {
      entry.occluded = sceneOverlayOccluded(entry, occluders, entry.sprite && entry.sprite.occlude);
      if (entry.occluded) {
        entry.hidden = true;
      }
    }
    return entries;
  }

  function renderSceneLabelElement(element, label, layoutKey, layout, metrics, box, hidden, occluded) {
    const align = normalizeSceneLabelAlign(label.textAlign);
    const whiteSpace = normalizeSceneLabelWhiteSpace(label.whiteSpace);
    const zIndex = Math.max(1, 1000 + Math.round(sceneNumber(label.priority, 0) * 10) - Math.round(sceneNumber(label.depth, 0) * 10));

    element.setAttribute("data-gosx-scene-label", label.id || "");
    setAttrValue(element, "aria-hidden", "true");
    setAttrValue(element, "class", label.className ? ("gosx-scene-label " + label.className) : "gosx-scene-label");
    setAttrValue(element, "data-gosx-scene-label-collision", normalizeSceneLabelCollision(label.collision));
    setAttrValue(element, "data-gosx-scene-label-occlude", label.occlude ? "true" : "false");
    setAttrValue(element, "data-gosx-scene-label-occluded", occluded ? "true" : "false");
    setAttrValue(element, "data-gosx-scene-label-visibility", hidden ? "hidden" : "visible");
    setAttrValue(element, "data-gosx-scene-label-priority", sceneNumber(label.priority, 0));
    setAttrValue(element, "data-gosx-scene-label-depth", sceneNumber(label.depth, 0));
    setAttrValue(element, "data-gosx-scene-label-truncated", layout && layout.truncated ? "true" : "false");

    applyTextLayoutPresentation(element, {
      font: label.font,
      whiteSpace: whiteSpace,
      lineHeight: sceneNumber(label.lineHeight, 18),
      maxLines: Math.max(0, Math.floor(sceneNumber(label.maxLines, 0))),
      overflow: normalizeTextLayoutOverflow(label.overflow),
      maxWidth: sceneNumber(label.maxWidth, 180),
    }, layout, {
      role: "label",
      surface: "scene3d",
      state: "ready",
      align: align,
      revision: gosxTextLayoutRevision(),
    });

    setStyleValue(element.style, "--gosx-scene-label-left", box.anchor.x + "px");
    setStyleValue(element.style, "--gosx-scene-label-top", box.anchor.y + "px");
    setStyleValue(element.style, "--gosx-scene-label-anchor-x", String(sceneNumber(label.anchorX, 0.5)));
    setStyleValue(element.style, "--gosx-scene-label-anchor-y", String(sceneNumber(label.anchorY, 1)));
    setStyleValue(element.style, "--gosx-scene-label-width", metrics.totalWidth + "px");
    setStyleValue(element.style, "--gosx-scene-label-max-width", metrics.maxTotalWidth + "px");
    setStyleValue(element.style, "--gosx-scene-label-height", metrics.totalHeight + "px");
    setStyleValue(element.style, "--gosx-scene-label-line-height", sceneNumber(label.lineHeight, 18) + "px");
    setStyleValue(element.style, "--gosx-scene-label-align", align);
    setStyleValue(element.style, "--gosx-scene-label-white-space", whiteSpace);
    setStyleValue(element.style, "--gosx-scene-label-font", label.font || '600 13px "IBM Plex Sans", "Segoe UI", sans-serif');
    setStyleValue(element.style, "--gosx-scene-label-color", label.color || "#ecf7ff");
    setStyleValue(element.style, "--gosx-scene-label-background", label.background || "rgba(8, 21, 31, 0.82)");
    setStyleValue(element.style, "--gosx-scene-label-border-color", label.borderColor || "rgba(141, 225, 255, 0.24)");
    setStyleValue(element.style, "--gosx-scene-label-z-index", String(zIndex));
    setStyleValue(element.style, "--gosx-scene-label-depth", String(sceneNumber(label.depth, 0)));
    element.__gosxTextLayout = layout;

    if (element.__gosxLayoutKey === layoutKey) {
      return;
    }

    clearChildren(element);
    const lines = Array.isArray(layout.lines) && layout.lines.length > 0 ? layout.lines : [{ text: label.text }];
    for (const line of lines) {
      const lineElement = document.createElement("div");
      lineElement.setAttribute("data-gosx-scene-label-line", "");
      lineElement.textContent = line && typeof line.text === "string" && line.text !== "" ? line.text : "\u00a0";
      if (whiteSpace !== "normal") {
        lineElement.style.whiteSpace = whiteSpace;
      }
      element.appendChild(lineElement);
    }
    element.__gosxLayoutKey = layoutKey;
  }

  function renderSceneLabels(layer, bundle, layoutCache, elements, width, height) {
    if (!layer) {
      return;
    }

    const labels = prepareSceneLabelEntries(bundle, layoutCache, width, height);
    const active = new Set();

    for (const entry of labels) {
      const id = entry.id;
      active.add(id);
      let element = elements.get(id);
      if (!element) {
        element = document.createElement("div");
        layer.appendChild(element);
        elements.set(id, element);
      }
      renderSceneLabelElement(element, entry.label, entry.layoutKey, entry.layout, entry.metrics, entry.box, entry.hidden, entry.occluded);
    }

    for (const [id, element] of elements.entries()) {
      if (active.has(id)) {
        continue;
      }
      if (element.parentNode === layer) {
        layer.removeChild(element);
      }
      elements.delete(id);
    }
  }

  function renderSceneSpriteElement(element, sprite, box, hidden, occluded) {
    const zIndex = Math.max(1, 1000 + Math.round(sceneNumber(sprite.priority, 0) * 10) - Math.round(sceneNumber(sprite.depth, 0) * 10));
    element.setAttribute("data-gosx-scene-sprite", sprite.id || "");
    setAttrValue(element, "aria-hidden", "true");
    setAttrValue(element, "class", sprite.className ? ("gosx-scene-sprite " + sprite.className) : "gosx-scene-sprite");
    setAttrValue(element, "data-gosx-scene-sprite-fit", normalizeSceneSpriteFit(sprite.fit));
    setAttrValue(element, "data-gosx-scene-sprite-occlude", sprite.occlude ? "true" : "false");
    setAttrValue(element, "data-gosx-scene-sprite-occluded", occluded ? "true" : "false");
    setAttrValue(element, "data-gosx-scene-sprite-visibility", hidden ? "hidden" : "visible");
    setAttrValue(element, "data-gosx-scene-sprite-priority", sceneNumber(sprite.priority, 0));
    setAttrValue(element, "data-gosx-scene-sprite-depth", sceneNumber(sprite.depth, 0));
    setStyleValue(element.style, "--gosx-scene-sprite-left", box.anchor.x + "px");
    setStyleValue(element.style, "--gosx-scene-sprite-top", box.anchor.y + "px");
    setStyleValue(element.style, "--gosx-scene-sprite-anchor-x", String(sceneNumber(sprite.anchorX, 0.5)));
    setStyleValue(element.style, "--gosx-scene-sprite-anchor-y", String(sceneNumber(sprite.anchorY, 0.5)));
    setStyleValue(element.style, "--gosx-scene-sprite-width", Math.max(1, sceneNumber(sprite.width, 1)) + "px");
    setStyleValue(element.style, "--gosx-scene-sprite-height", Math.max(1, sceneNumber(sprite.height, 1)) + "px");
    setStyleValue(element.style, "--gosx-scene-sprite-opacity", String(clamp01(sceneNumber(sprite.opacity, 1))));
    setStyleValue(element.style, "--gosx-scene-sprite-fit", normalizeSceneSpriteFit(sprite.fit));
    setStyleValue(element.style, "--gosx-scene-sprite-z-index", String(zIndex));
    setStyleValue(element.style, "--gosx-scene-sprite-depth", String(sceneNumber(sprite.depth, 0)));

    let image = element.firstChild;
    if (!image || image.tagName !== "IMG") {
      clearChildren(element);
      image = document.createElement("img");
      image.setAttribute("draggable", "false");
      image.setAttribute("alt", "");
      image.setAttribute("aria-hidden", "true");
      element.appendChild(image);
    }
    setAttrValue(image, "src", sprite.src || "");
    setStyleValue(image.style, "objectFit", normalizeSceneSpriteFit(sprite.fit) === "fill" ? "fill" : normalizeSceneSpriteFit(sprite.fit));
  }

  function renderSceneSprites(layer, bundle, elements, width, height) {
    if (!layer) {
      return;
    }

    const sprites = prepareSceneSpriteEntries(bundle, width, height);
    const active = new Set();
    for (const entry of sprites) {
      const id = entry.id;
      active.add(id);
      let element = elements.get(id);
      if (!element) {
        element = document.createElement("div");
        layer.appendChild(element);
        elements.set(id, element);
      }
      renderSceneSpriteElement(element, entry.sprite, entry.box, entry.hidden, entry.occluded);
    }
    for (const [id, element] of elements.entries()) {
      if (active.has(id)) {
        continue;
      }
      if (element.parentNode === layer) {
        layer.removeChild(element);
      }
      elements.delete(id);
    }
  }

  function sceneHTMLBounds(entry) {
    const anchorX = sceneNumber(entry.anchorX, 0.5);
    const anchorY = sceneNumber(entry.anchorY, 0.5);
    const htmlWidth = Math.max(1, sceneNumber(entry.width, 1));
    const htmlHeight = Math.max(1, sceneNumber(entry.height, 1));
    const anchorPointX = sceneNumber(entry.position && entry.position.x, 0) + sceneNumber(entry.offsetX, 0);
    const anchorPointY = sceneNumber(entry.position && entry.position.y, 0) + sceneNumber(entry.offsetY, 0);
    const left = anchorPointX - (anchorX * htmlWidth);
    const top = anchorPointY - (anchorY * htmlHeight);
    return {
      left,
      top,
      right: left + htmlWidth,
      bottom: top + htmlHeight,
      anchor: { x: anchorPointX, y: anchorPointY },
      center: { x: left + (htmlWidth / 2), y: top + (htmlHeight / 2) },
    };
  }

  function sceneHTMLPriorityCompare(a, b) {
    const priorityDiff = sceneNumber(b && b.html && b.html.priority, 0) - sceneNumber(a && a.html && a.html.priority, 0);
    if (Math.abs(priorityDiff) > 0.001) {
      return priorityDiff;
    }
    const depthDiff = sceneNumber(a && a.html && a.html.depth, 0) - sceneNumber(b && b.html && b.html.depth, 0);
    if (Math.abs(depthDiff) > 0.001) {
      return depthDiff;
    }
    return sceneNumber(a && a.order, 0) - sceneNumber(b && b.order, 0);
  }

  function prepareSceneHTMLEntries(bundle, width, height) {
    const htmlEntries = bundle && Array.isArray(bundle.html) ? bundle.html : [];
    const occluders = buildSceneLabelOccluders(bundle, width, height);
    const entries = [];
    for (let index = 0; index < htmlEntries.length; index += 1) {
      const htmlEntry = htmlEntries[index];
      if (!htmlEntry || typeof htmlEntry.html !== "string" || htmlEntry.html.trim() === "") {
        continue;
      }
      const box = sceneHTMLBounds(htmlEntry);
      entries.push({
        id: htmlEntry.id || ("scene-html-" + index),
        order: index,
        html: htmlEntry,
        depth: sceneNumber(htmlEntry.depth, 0),
        box,
        occluded: false,
        hidden: false,
      });
    }
    const sorted = entries.slice().sort(sceneHTMLPriorityCompare);
    for (const entry of sorted) {
      entry.occluded = sceneOverlayOccluded(entry, occluders, entry.html && entry.html.occlude);
      if (entry.occluded) {
        entry.hidden = true;
      }
    }
    return entries;
  }

  function renderSceneHTMLElement(element, htmlEntry, box, hidden, occluded) {
    const zIndex = Math.max(1, 1000 + Math.round(sceneNumber(htmlEntry.priority, 0) * 10) - Math.round(sceneNumber(htmlEntry.depth, 0) * 10));
    element.setAttribute("data-gosx-scene-html", htmlEntry.id || "");
    setAttrValue(element, "class", htmlEntry.className ? ("gosx-scene-html " + htmlEntry.className) : "gosx-scene-html");
    setAttrValue(element, "data-gosx-scene-html-target", htmlEntry.target || "");
    setAttrValue(element, "data-gosx-scene-html-mode", htmlEntry.mode || "dom");
    setAttrValue(element, "data-gosx-scene-html-fallback", htmlEntry.fallback || "");
    setAttrValue(element, "data-gosx-scene-html-fallback-reason", htmlEntry.fallbackReason || "");
    setAttrValue(element, "data-gosx-scene-html-texture-key", htmlEntry.textureKey || "");
    setAttrValue(element, "data-gosx-scene-html-texture-width", sceneNumber(htmlEntry.textureWidth, 0) > 0 ? sceneNumber(htmlEntry.textureWidth, 0) : "");
    setAttrValue(element, "data-gosx-scene-html-texture-height", sceneNumber(htmlEntry.textureHeight, 0) > 0 ? sceneNumber(htmlEntry.textureHeight, 0) : "");
    setAttrValue(element, "data-gosx-scene-html-texture-bytes", sceneNumber(htmlEntry.textureBytes, 0) > 0 ? sceneNumber(htmlEntry.textureBytes, 0) : "");
    setAttrValue(element, "data-gosx-scene-html-texture-cap-bytes", sceneNumber(htmlEntry.textureMaxBytes, 0) > 0 ? sceneNumber(htmlEntry.textureMaxBytes, 0) : "");
    setAttrValue(element, "data-gosx-scene-html-texture-over-budget", htmlEntry.textureOverBudget ? "true" : "false");
    setAttrValue(element, "data-gosx-scene-html-texture-ready", htmlEntry.textureReady ? "true" : "false");
    setAttrValue(element, "data-gosx-scene-html-texture-revision", sceneNumber(htmlEntry.textureRevision, 0) > 0 ? sceneNumber(htmlEntry.textureRevision, 0) : "");
    setAttrValue(element, "data-gosx-scene-html-texture-dirty", htmlEntry.textureDirty ? "true" : "false");
    setAttrValue(element, "data-gosx-scene-html-texture-dirty-bytes", sceneNumber(htmlEntry.textureDirtyBytes, 0) > 0 ? sceneNumber(htmlEntry.textureDirtyBytes, 0) : "");
    setAttrValue(element, "data-gosx-scene-html-texture-upload-pending-bytes", sceneNumber(htmlEntry.texturePendingUploadBytes, 0) > 0 ? sceneNumber(htmlEntry.texturePendingUploadBytes, 0) : "");
    setAttrValue(element, "data-gosx-scene-html-texture-manager", htmlEntry.textureManager || "");
    setAttrValue(element, "data-gosx-scene-html-texture-rasterized", htmlEntry.textureRasterized ? "true" : "false");
    setAttrValue(element, "data-gosx-scene-html-texture-upload-bytes", sceneNumber(htmlEntry.textureUploadBytes, 0) > 0 ? sceneNumber(htmlEntry.textureUploadBytes, 0) : "");
    setAttrValue(element, "data-gosx-scene-html-occlude", htmlEntry.occlude ? "true" : "false");
    setAttrValue(element, "data-gosx-scene-html-occluded", occluded ? "true" : "false");
    setAttrValue(element, "data-gosx-scene-html-visibility", hidden ? "hidden" : "visible");
    setAttrValue(element, "aria-hidden", hidden ? "true" : "false");
    setAttrValue(element, "data-gosx-scene-html-priority", sceneNumber(htmlEntry.priority, 0));
    setAttrValue(element, "data-gosx-scene-html-depth", sceneNumber(htmlEntry.depth, 0));
    setAttrValue(element, "data-gosx-scene-html-pointer-events", normalizeSceneHTMLPointerEvents(htmlEntry.pointerEvents, "none"));
    setStyleValue(element.style, "--gosx-scene-html-left", box.anchor.x + "px");
    setStyleValue(element.style, "--gosx-scene-html-top", box.anchor.y + "px");
    setStyleValue(element.style, "--gosx-scene-html-anchor-x", String(sceneNumber(htmlEntry.anchorX, 0.5)));
    setStyleValue(element.style, "--gosx-scene-html-anchor-y", String(sceneNumber(htmlEntry.anchorY, 0.5)));
    setStyleValue(element.style, "--gosx-scene-html-width", Math.max(1, sceneNumber(htmlEntry.width, 1)) + "px");
    setStyleValue(element.style, "--gosx-scene-html-min-height", Math.max(1, sceneNumber(htmlEntry.height, 1)) + "px");
    setStyleValue(element.style, "--gosx-scene-html-opacity", String(clamp01(sceneNumber(htmlEntry.opacity, 1))));
    setStyleValue(element.style, "--gosx-scene-html-z-index", String(zIndex));
    setStyleValue(element.style, "--gosx-scene-html-depth", String(sceneNumber(htmlEntry.depth, 0)));
    setStyleValue(element.style, "--gosx-scene-html-pointer-events", normalizeSceneHTMLPointerEvents(htmlEntry.pointerEvents, "none"));
    if (element.__gosxHTMLMarkup !== htmlEntry.html) {
      element.innerHTML = htmlEntry.html;
      element.__gosxHTMLMarkup = htmlEntry.html;
    }
  }

  function sceneHTMLTextureTargetID(htmlEntry) {
    if (!htmlEntry || typeof htmlEntry !== "object") {
      return "";
    }
    if (typeof htmlEntry.target === "string" && htmlEntry.target.trim()) {
      return htmlEntry.target.trim();
    }
    if (typeof htmlEntry.targetID === "string" && htmlEntry.targetID.trim()) {
      return htmlEntry.targetID.trim();
    }
    return "";
  }

  function sceneHTMLTextureNumber(value, fallback) {
    const number = Number(value);
    return Number.isFinite(number) ? number : fallback;
  }

  function dispatchSceneHTMLTexturePointer(bundle, elements, detail) {
    if (!bundle || !elements || !detail || typeof detail !== "object") {
      return false;
    }
    const targetID = typeof detail.targetID === "string" ? detail.targetID.trim() : "";
    if (!targetID || !Array.isArray(bundle.html)) {
      return false;
    }
    let dispatched = false;
    for (const htmlEntry of bundle.html) {
      if (!htmlEntry || normalizeSceneHTMLMode(htmlEntry.mode, "dom") !== "texture") {
        continue;
      }
      const htmlTargetID = sceneHTMLTextureTargetID(htmlEntry);
      if (htmlTargetID !== targetID && htmlEntry.id !== targetID) {
        continue;
      }
      const id = htmlEntry.id || "";
      const element = elements.get(id);
      if (!element || typeof element.dispatchEvent !== "function") {
        continue;
      }
      const width = Math.max(1, sceneNumber(htmlEntry.width, 1));
      const height = Math.max(1, sceneNumber(htmlEntry.height, 1));
      const uvX = clamp01(sceneHTMLTextureNumber(detail.uvX, 0));
      const uvY = clamp01(sceneHTMLTextureNumber(detail.uvY, 0));
      const localX = uvX * width;
      const localY = uvY * height;
      const pointerDetail = {
        htmlID: id,
        targetID,
        type: typeof detail.type === "string" ? detail.type : "",
        pointerX: sceneHTMLTextureNumber(detail.pointerX, 0),
        pointerY: sceneHTMLTextureNumber(detail.pointerY, 0),
        uvX,
        uvY,
        localX,
        localY,
        width,
        height,
        fallback: htmlEntry.fallback || (normalizeSceneHTMLMode(htmlEntry.mode, "dom") === "texture" ? "dom-overlay" : ""),
        fallbackReason: htmlEntry.fallbackReason || (normalizeSceneHTMLMode(htmlEntry.mode, "dom") === "texture" ? "html-texture-manager-unavailable" : ""),
        scene: detail,
      };
      setAttrValue(element, "data-gosx-scene-html-hit-type", pointerDetail.type);
      setAttrValue(element, "data-gosx-scene-html-hit-target", pointerDetail.targetID);
      setAttrValue(element, "data-gosx-scene-html-hit-uv-x", pointerDetail.uvX);
      setAttrValue(element, "data-gosx-scene-html-hit-uv-y", pointerDetail.uvY);
      setAttrValue(element, "data-gosx-scene-html-hit-local-x", pointerDetail.localX);
      setAttrValue(element, "data-gosx-scene-html-hit-local-y", pointerDetail.localY);
      setStyleValue(element.style, "--gosx-scene-html-hit-uv-x", String(pointerDetail.uvX));
      setStyleValue(element.style, "--gosx-scene-html-hit-uv-y", String(pointerDetail.uvY));
      setStyleValue(element.style, "--gosx-scene-html-hit-local-x", pointerDetail.localX + "px");
      setStyleValue(element.style, "--gosx-scene-html-hit-local-y", pointerDetail.localY + "px");
      const event = typeof CustomEvent === "function"
        ? new CustomEvent("gosx:scene-html-texture-pointer", { detail: pointerDetail })
        : { type: "gosx:scene-html-texture-pointer", detail: pointerDetail };
      element.dispatchEvent(event);
      dispatched = true;
    }
    return dispatched;
  }

  function createSceneHTMLTextureState() {
    return {
      records: new Map(),
      revision: 0,
      disposed: 0,
      disposedBytes: 0,
      requestRender: null,
    };
  }

  function disposeSceneHTMLTextureState(state) {
    if (!state || !state.records) {
      return;
    }
    state.records.clear();
  }

  function sceneHTMLTextureLifecycleID(html, index) {
    if (html && typeof html.id === "string" && html.id.trim()) {
      return html.id.trim();
    }
    if (html && typeof html.textureKey === "string" && html.textureKey.trim()) {
      return html.textureKey.trim();
    }
    return "scene-html-" + index;
  }

  function sceneHTMLTextureLifecycleSignature(html, record) {
    const textureKey = record && record.textureKey === (html && html.textureKey) && record.sourceKey
      ? record.sourceKey
      : (html && html.textureKey ? html.textureKey : "");
    return [
      textureKey,
      sceneNumber(html && html.textureWidth, 0),
      sceneNumber(html && html.textureHeight, 0),
      sceneNumber(html && html.textureBytes, 0),
      sceneNumber(html && html.textureMaxBytes, 0),
      html && html.html ? html.html : "",
    ].join("|");
  }

  function syncSceneHTMLTextureState(state, entries) {
    const lifecycle = { dirty: 0, dirtyBytes: 0, pendingUploadBytes: 0, disposed: 0, disposedBytes: 0, revision: 0 };
    if (!state || !state.records) {
      return lifecycle;
    }
    const active = new Set();
    for (let index = 0; index < entries.length; index += 1) {
      const html = entries[index] && entries[index].html;
      if (!html || normalizeSceneHTMLMode(html.mode, "dom") !== "texture") {
        continue;
      }
      const id = sceneHTMLTextureLifecycleID(html, index);
      active.add(id);
      let record = state.records.get(id);
      if (!record) {
        record = { id, revision: 0, signature: "", bytes: 0, dirty: false, dirtyBytes: 0, pendingUploadBytes: 0 };
        state.records.set(id, record);
      }
      const signature = sceneHTMLTextureLifecycleSignature(html, record);
      const bytes = Math.max(0, Math.floor(sceneNumber(html.textureBytes, 0)));
      if (record.signature !== signature) {
        record.signature = signature;
        record.revision += 1;
        state.revision += 1;
        record.dirty = !html.textureOverBudget;
        record.dirtyBytes = record.dirty ? bytes : 0;
        record.pendingUploadBytes = record.dirty && !html.textureReady ? bytes : 0;
      }
      record.bytes = bytes;
      record.ready = Boolean(html.textureReady && !html.textureOverBudget);
      if (record.ready) {
        record.dirty = false;
        record.dirtyBytes = 0;
        record.pendingUploadBytes = 0;
      }
      html.textureRevision = record.revision;
      html.textureDirty = record.dirty;
      html.textureDirtyBytes = record.dirtyBytes;
      html.texturePendingUploadBytes = record.pendingUploadBytes;
      html.textureManager = record.manager || "";
      html.textureRasterized = Boolean(record.rasterized);
      html.textureUploadBytes = record.uploadBytes || 0;
      if (record.dirty) {
        lifecycle.dirty += 1;
        lifecycle.dirtyBytes += record.dirtyBytes;
      }
      lifecycle.pendingUploadBytes += record.pendingUploadBytes;
    }
    state.records.forEach(function(record, id) {
      if (active.has(id)) {
        return;
      }
      state.disposed += 1;
      state.disposedBytes += Math.max(0, Math.floor(sceneNumber(record && record.bytes, 0)));
      state.records.delete(id);
    });
    lifecycle.disposed = state.disposed;
    lifecycle.disposedBytes = state.disposedBytes;
    lifecycle.revision = state.revision;
    return lifecycle;
  }

  function sceneHTMLTextureStats(entries, lifecycle) {
    const stats = { bytes: 0, capBytes: 0, overBudget: 0, ready: 0, count: 0, dirty: 0, dirtyBytes: 0, pendingUploadBytes: 0, disposed: 0, disposedBytes: 0, revision: 0 };
    for (const entry of entries || []) {
      const html = entry && entry.html;
      if (!html || normalizeSceneHTMLMode(html.mode, "dom") !== "texture") {
        continue;
      }
      stats.count += 1;
      stats.bytes += Math.max(0, Math.floor(sceneNumber(html.textureBytes, 0)));
      stats.capBytes += Math.max(0, Math.floor(sceneNumber(html.textureMaxBytes, 0)));
      if (html.textureOverBudget) {
        stats.overBudget += 1;
      }
      if (html.textureReady) {
        stats.ready += 1;
      }
    }
    if (lifecycle) {
      stats.dirty = Math.max(0, Math.floor(sceneNumber(lifecycle.dirty, 0)));
      stats.dirtyBytes = Math.max(0, Math.floor(sceneNumber(lifecycle.dirtyBytes, 0)));
      stats.pendingUploadBytes = Math.max(0, Math.floor(sceneNumber(lifecycle.pendingUploadBytes, 0)));
      stats.disposed = Math.max(0, Math.floor(sceneNumber(lifecycle.disposed, 0)));
      stats.disposedBytes = Math.max(0, Math.floor(sceneNumber(lifecycle.disposedBytes, 0)));
      stats.revision = Math.max(0, Math.floor(sceneNumber(lifecycle.revision, 0)));
    }
    return stats;
  }

  function sceneHTMLTextureDataURL(html) {
    const width = Math.max(1, Math.floor(sceneNumber(html && html.textureWidth, 512)));
    const height = Math.max(1, Math.floor(sceneNumber(html && html.textureHeight, 320)));
    const markup = typeof html.html === "string" ? html.html : "";
    if (!markup.trim()) {
      return "";
    }
    const bodyStyle = [
      "box-sizing:border-box",
      "width:" + width + "px",
      "min-height:" + height + "px",
      "font:14px system-ui,-apple-system,BlinkMacSystemFont,Segoe UI,sans-serif",
      "color:#fff",
    ].join(";");
    const svg = [
      '<svg xmlns="http://www.w3.org/2000/svg" width="' + width + '" height="' + height + '" viewBox="0 0 ' + width + " " + height + '">',
      '<foreignObject x="0" y="0" width="100%" height="100%">',
      '<div xmlns="http://www.w3.org/1999/xhtml" style="' + bodyStyle + '">',
      markup,
      "</div></foreignObject></svg>",
    ].join("");
    return "data:image/svg+xml;charset=utf-8," + encodeURIComponent(svg);
  }

  function rasterizeSceneHTMLTextureEntry(textureState, html, element, index) {
    if (!textureState || !textureState.records || !html || normalizeSceneHTMLMode(html.mode, "dom") !== "texture") {
      return false;
    }
    const id = sceneHTMLTextureLifecycleID(html, index || 0);
    const record = textureState.records.get(id);
    if (!record || html.textureOverBudget || !record.dirty) {
      return false;
    }
    const textureKey = sceneHTMLTextureDataURL(html);
    if (!textureKey) {
      record.manager = "unavailable";
      return false;
    }
    record.sourceKey = html.textureKey || ("gosx-html://" + id);
    record.textureKey = textureKey;
    record.manager = "svg-foreignobject";
    record.rasterized = true;
    record.ready = true;
    record.dirty = false;
    record.dirtyBytes = 0;
    record.pendingUploadBytes = 0;
    record.uploadBytes = Math.max(0, Math.floor(sceneNumber(html.textureBytes, 0)));
    html.textureKey = textureKey;
    html.textureReady = true;
    html.textureManager = record.manager;
    html.textureRasterized = true;
    html.textureDirty = false;
    html.textureDirtyBytes = 0;
    html.texturePendingUploadBytes = 0;
    html.textureUploadBytes = record.uploadBytes;
    if (html.fallbackReason === "html-texture-manager-unavailable" || !html.fallbackReason) {
      html.fallbackReason = "html-texture-accessibility-mirror";
    }
    if (element) {
      element.__gosxHTMLTextureKey = textureKey;
    }
    if (typeof textureState.requestRender === "function") {
      textureState.requestRender("html-texture");
    }
    return true;
  }

  function applySceneHTMLTextureRecordsToState(sceneState, textureState) {
    if (!sceneState || !textureState || !textureState.records || typeof sceneStateHTML !== "function") {
      return;
    }
    const entries = sceneStateHTML(sceneState);
    for (let index = 0; index < entries.length; index += 1) {
      const entry = entries[index];
      if (!entry || normalizeSceneHTMLMode(entry.mode, "dom") !== "texture") {
        continue;
      }
      const record = textureState.records.get(sceneHTMLTextureLifecycleID(entry, index));
      if (!record || !record.ready || !record.textureKey) {
        continue;
      }
      entry.textureKey = record.textureKey;
      entry.textureReady = true;
      entry.textureManager = record.manager || "";
      entry.textureRasterized = Boolean(record.rasterized);
      entry.textureUploadBytes = record.uploadBytes || 0;
      entry.textureDirty = false;
      entry.textureDirtyBytes = 0;
      entry.texturePendingUploadBytes = 0;
      if (entry.fallbackReason === "html-texture-manager-unavailable" || !entry.fallbackReason) {
        entry.fallbackReason = "html-texture-accessibility-mirror";
      }
    }
  }

  function setSceneHTMLTextureLayerAttrs(layer, textureStats, entryCount) {
    setAttrValue(layer, "aria-hidden", entryCount > 0 ? "false" : "true");
    setAttrValue(layer, "data-gosx-scene-html-texture-count", textureStats.count > 0 ? textureStats.count : "");
    setAttrValue(layer, "data-gosx-scene-html-texture-ready", textureStats.ready > 0 ? textureStats.ready : "");
    setAttrValue(layer, "data-gosx-scene-html-texture-bytes", textureStats.bytes > 0 ? textureStats.bytes : "");
    setAttrValue(layer, "data-gosx-scene-html-texture-cap-bytes", textureStats.capBytes > 0 ? textureStats.capBytes : "");
    setAttrValue(layer, "data-gosx-scene-html-texture-over-budget", textureStats.overBudget > 0 ? textureStats.overBudget : "");
    setAttrValue(layer, "data-gosx-scene-html-texture-dirty", textureStats.dirty > 0 ? textureStats.dirty : "");
    setAttrValue(layer, "data-gosx-scene-html-texture-dirty-bytes", textureStats.dirtyBytes > 0 ? textureStats.dirtyBytes : "");
    setAttrValue(layer, "data-gosx-scene-html-texture-upload-pending-bytes", textureStats.pendingUploadBytes > 0 ? textureStats.pendingUploadBytes : "");
    setAttrValue(layer, "data-gosx-scene-html-texture-disposed", textureStats.disposed > 0 ? textureStats.disposed : "");
    setAttrValue(layer, "data-gosx-scene-html-texture-disposed-bytes", textureStats.disposedBytes > 0 ? textureStats.disposedBytes : "");
    setAttrValue(layer, "data-gosx-scene-html-texture-revision", textureStats.revision > 0 ? textureStats.revision : "");
  }

  function renderSceneHTML(layer, bundle, elements, width, height, textureState) {
    if (!layer) {
      return;
    }
    const entries = prepareSceneHTMLEntries(bundle, width, height);
    const textureLifecycle = syncSceneHTMLTextureState(textureState, entries);
    const textureStats = sceneHTMLTextureStats(entries, textureLifecycle);
    setSceneHTMLTextureLayerAttrs(layer, textureStats, entries.length);
    const active = new Set();
    let rasterizedAny = false;
    for (const entry of entries) {
      const id = entry.id;
      active.add(id);
      let element = elements.get(id);
      if (!element) {
        element = document.createElement("div");
        layer.appendChild(element);
        elements.set(id, element);
      }
      renderSceneHTMLElement(element, entry.html, entry.box, entry.hidden, entry.occluded);
      if (rasterizeSceneHTMLTextureEntry(textureState, entry.html, element, entry.order)) {
        rasterizedAny = true;
        renderSceneHTMLElement(element, entry.html, entry.box, entry.hidden, entry.occluded);
      }
    }
    for (const [id, element] of elements.entries()) {
      if (active.has(id)) {
        continue;
      }
      if (element.parentNode === layer) {
        layer.removeChild(element);
      }
      elements.delete(id);
    }
    if (rasterizedAny) {
      const nextLifecycle = syncSceneHTMLTextureState(textureState, entries);
      setSceneHTMLTextureLayerAttrs(layer, sceneHTMLTextureStats(entries, nextLifecycle), entries.length);
    }
  }

  function normalizeSceneControlsMode(value) {
    switch (String(value || "").trim().toLowerCase()) {
      case "orbit":
        return "orbit";
      case "first-person":
      case "firstperson":
      case "fps":
        return "first-person";
      case "fly":
      case "free":
      case "free-camera":
        return "fly";
      default:
        return "";
    }
  }

  function sceneControlsTarget(props) {
    const raw = props && props.controlTarget && typeof props.controlTarget === "object" ? props.controlTarget : null;
    return {
      x: sceneNumber(raw && raw.x, 0),
      y: sceneNumber(raw && raw.y, 0),
      z: sceneNumber(raw && raw.z, 0),
    };
  }

  function sceneControlsRotateSpeed(props) {
    return Math.max(0.1, sceneNumber(props && props.controlRotateSpeed, 1));
  }

  function sceneControlsZoomSpeed(props) {
    return Math.max(0.05, sceneNumber(props && props.controlZoomSpeed, 1));
  }

  function sceneControlsLookSpeed(props) {
    return Math.max(0.05, sceneNumber(props && props.controlLookSpeed, sceneNumber(props && props.controlRotateSpeed, 1)));
  }

  function sceneControlsMoveSpeed(props) {
    return Math.max(0.01, sceneNumber(props && props.controlMoveSpeed, 4));
  }

  const SCENE_ORBIT_MAX_SPEED = Math.PI * 6;
  const SCENE_ORBIT_DAMPING = 6;
  const SCENE_ORBIT_STOP_SPEED = (Math.PI / 180) * 0.01;
  const SCENE_ORBIT_DEFAULT_PITCH_LIMIT = 1.4;
  const SCENE_ORBIT_MAX_PITCH_LIMIT = Math.PI / 2 - (Math.PI / 180) * 0.001;
  const SCENE_ORBIT_DEFAULT_MIN_DISTANCE = 0.6;
  const SCENE_ORBIT_DEFAULT_MAX_DISTANCE = 256;

  function sceneOrbitPitchLimit(value) {
    return sceneClamp(
      sceneNumber(value, SCENE_ORBIT_DEFAULT_PITCH_LIMIT),
      0.1,
      SCENE_ORBIT_MAX_PITCH_LIMIT,
    );
  }

  function sceneControlsRotateMode(props) {
    const mode = String(props && props.controlRotateMode || "").trim().toLowerCase();
    if (mode === "pixel-degrees" || mode === "pixel-degree" || mode === "pixels-degrees") {
      return "pixel-degrees";
    }
    return "viewport";
  }

  function sceneControlsPitchLimit(props) {
    return sceneOrbitPitchLimit(props && props.controlPitchLimit);
  }

  function sceneControlsMinDistance(props) {
    return Math.max(0.001, sceneNumber(props && props.controlMinDistance, SCENE_ORBIT_DEFAULT_MIN_DISTANCE));
  }

  function sceneControlsMaxDistance(props, minDistance) {
    return Math.max(
      Math.max(0.001, sceneNumber(minDistance, SCENE_ORBIT_DEFAULT_MIN_DISTANCE)),
      sceneNumber(props && props.controlMaxDistance, SCENE_ORBIT_DEFAULT_MAX_DISTANCE),
    );
  }

  function scenePointerLockRequested(props) {
    return sceneBool(props && props.pointerLock, false);
  }

  function scenePointerLockElement() {
    if (typeof document === "undefined" || !document) {
      return null;
    }
    return document.pointerLockElement ||
      document.mozPointerLockElement ||
      document.webkitPointerLockElement ||
      null;
  }

  function scenePointerLockActive(canvas) {
    return Boolean(canvas && scenePointerLockElement() === canvas);
  }

  function sceneRequestPointerLock(canvas) {
    if (!canvas || typeof canvas.requestPointerLock !== "function") {
      return false;
    }
    try {
      canvas.requestPointerLock({ unadjustedMovement: true });
      return true;
    } catch (_unadjustedError) {
      try {
        canvas.requestPointerLock();
        return true;
      } catch (_error) {
        return false;
      }
    }
  }

  function sceneExitPointerLock(canvas) {
    if (!scenePointerLockActive(canvas)) {
      return;
    }
    const exit = document.exitPointerLock || document.mozExitPointerLock || document.webkitExitPointerLock;
    if (typeof exit !== "function") {
      return;
    }
    try {
      exit.call(document);
    } catch (_error) {}
  }

  function sceneWorldCameraPosition(camera) {
    const normalized = sceneRenderCamera(camera);
    return {
      x: normalized.x,
      y: normalized.y,
      z: -normalized.z,
    };
  }

  function sceneFlyStateFromCamera(camera) {
    const normalized = sceneRenderCamera(camera);
    return {
      position: sceneWorldCameraPosition(normalized),
      yaw: sceneNumber(normalized.rotationY, 0),
      pitch: sceneClamp(sceneNumber(normalized.rotationX, 0), -1.52, 1.52),
      kind: normalized.kind,
      fov: normalized.fov,
      left: normalized.left,
      right: normalized.right,
      top: normalized.top,
      bottom: normalized.bottom,
      zoom: normalized.zoom,
      near: normalized.near,
      far: normalized.far,
    };
  }

  function sceneFlyCamera(state, fallbackCamera) {
    const base = sceneRenderCamera(fallbackCamera);
    const fly = state || sceneFlyStateFromCamera(base);
    const position = fly.position || { x: 0, y: 0, z: -6 };
    return {
      x: sceneNumber(position.x, base.x),
      y: sceneNumber(position.y, base.y),
      z: -sceneNumber(position.z, -base.z),
      kind: base.kind,
      rotationX: sceneClamp(sceneNumber(fly.pitch, base.rotationX), -1.52, 1.52),
      rotationY: sceneNumber(fly.yaw, base.rotationY),
      rotationZ: 0,
      fov: sceneNumber(fly.fov, base.fov),
      left: sceneNumber(fly.left, base.left),
      right: sceneNumber(fly.right, base.right),
      top: sceneNumber(fly.top, base.top),
      bottom: sceneNumber(fly.bottom, base.bottom),
      zoom: sceneNumber(fly.zoom, base.zoom),
      near: sceneNumber(fly.near, base.near),
      far: sceneNumber(fly.far, base.far),
    };
  }

  function sceneOrbitStateFromCamera(camera, target, controls) {
    const normalized = sceneRenderCamera(camera);
    const worldPosition = sceneWorldCameraPosition(normalized);
    const orbitTarget = target || { x: 0, y: 0, z: 0 };
    const offsetX = worldPosition.x - sceneNumber(orbitTarget.x, 0);
    const offsetY = worldPosition.y - sceneNumber(orbitTarget.y, 0);
    const offsetZ = worldPosition.z - sceneNumber(orbitTarget.z, 0);
    const minDistance = Math.max(0.001, sceneNumber(controls && controls.minDistance, SCENE_ORBIT_DEFAULT_MIN_DISTANCE));
    const maxDistance = Math.max(minDistance, sceneNumber(controls && controls.maxDistance, SCENE_ORBIT_DEFAULT_MAX_DISTANCE));
    const radius = sceneClamp(Math.hypot(offsetX, offsetY, offsetZ), minDistance, maxDistance);
    const pitchLimit = sceneOrbitPitchLimit(controls && controls.pitchLimit);
    const pitchRatioLimit = Math.sin(pitchLimit);
    return {
      target: {
        x: sceneNumber(orbitTarget.x, 0),
        y: sceneNumber(orbitTarget.y, 0),
        z: sceneNumber(orbitTarget.z, 0),
      },
      radius,
      minDistance,
      maxDistance,
      pitchLimit,
      yaw: Math.atan2(offsetX, -offsetZ),
      pitch: Math.asin(sceneClamp(offsetY / Math.max(radius, 0.001), -pitchRatioLimit, pitchRatioLimit)),
      kind: normalized.kind,
      fov: normalized.fov,
      left: normalized.left,
      right: normalized.right,
      top: normalized.top,
      bottom: normalized.bottom,
      zoom: normalized.zoom,
      near: normalized.near,
      far: normalized.far,
    };
  }

  function sceneOrbitCamera(state, fallbackCamera) {
    const base = sceneRenderCamera(fallbackCamera);
    const orbit = state || sceneOrbitStateFromCamera(base, { x: 0, y: 0, z: 0 });
    const minDistance = Math.max(0.001, sceneNumber(orbit.minDistance, SCENE_ORBIT_DEFAULT_MIN_DISTANCE));
    const maxDistance = Math.max(minDistance, sceneNumber(orbit.maxDistance, SCENE_ORBIT_DEFAULT_MAX_DISTANCE));
    const radius = sceneClamp(sceneNumber(orbit.radius, 6), minDistance, maxDistance);
    const pitchLimit = sceneOrbitPitchLimit(orbit.pitchLimit);
    const pitch = sceneClamp(sceneNumber(orbit.pitch, 0), -pitchLimit, pitchLimit);
    const yaw = sceneNumber(orbit.yaw, 0);
    const target = orbit.target || { x: 0, y: 0, z: 0 };
    const cosPitch = Math.cos(pitch);
    const worldPosition = {
      x: sceneNumber(target.x, 0) + Math.sin(yaw) * cosPitch * radius,
      y: sceneNumber(target.y, 0) + Math.sin(pitch) * radius,
      z: sceneNumber(target.z, 0) - Math.cos(yaw) * cosPitch * radius,
    };
    const forward = {
      x: sceneNumber(target.x, 0) - worldPosition.x,
      y: sceneNumber(target.y, 0) - worldPosition.y,
      z: sceneNumber(target.z, 0) - worldPosition.z,
    };
    const horizontal = Math.max(0.0001, Math.hypot(forward.x, forward.z));
    return {
      x: worldPosition.x,
      y: worldPosition.y,
      z: -worldPosition.z,
      kind: base.kind,
      rotationX: -Math.atan2(forward.y, horizontal),
      rotationY: Math.atan2(forward.x, forward.z),
      rotationZ: 0,
      fov: sceneNumber(orbit.fov, base.fov),
      left: sceneNumber(orbit.left, base.left),
      right: sceneNumber(orbit.right, base.right),
      top: sceneNumber(orbit.top, base.top),
      bottom: sceneNumber(orbit.bottom, base.bottom),
      zoom: sceneNumber(orbit.zoom, base.zoom),
      near: sceneNumber(orbit.near, base.near),
      far: sceneNumber(orbit.far, base.far),
    };
  }

  function createSceneControls(props) {
    const mode = normalizeSceneControlsMode(props && props.controls);
    if (!mode) {
      return null;
    }
    const minDistance = sceneControlsMinDistance(props);
    const maxDistance = sceneControlsMaxDistance(props, minDistance);
    return {
      mode,
      active: false,
      touched: false,
      pointerId: null,
      lastX: 0,
      lastY: 0,
      rotateMode: sceneControlsRotateMode(props),
      rotateSpeed: sceneControlsRotateSpeed(props),
      zoomSpeed: sceneControlsZoomSpeed(props),
      lookSpeed: sceneControlsLookSpeed(props),
      moveSpeed: sceneControlsMoveSpeed(props),
      minDistance,
      maxDistance,
      pitchLimit: sceneControlsPitchLimit(props),
      pointerLock: mode !== "orbit" && scenePointerLockRequested(props),
      pointerLocked: false,
      orbit: null,
      orbitVelocityYaw: 0,
      orbitVelocityPitch: 0,
      orbitLastMoveMS: 0,
      fly: null,
      keys: new Set(),
      target: sceneControlsTarget(props),
    };
  }

  function sceneOrbitStopInertia(controls) {
    if (!controls) return;
    controls.orbitVelocityYaw = 0;
    controls.orbitVelocityPitch = 0;
  }

  function sceneOrbitInertiaActive(controls) {
    if (!controls || controls.active) return false;
    return Math.abs(sceneNumber(controls.orbitVelocityYaw, 0)) > SCENE_ORBIT_STOP_SPEED ||
      Math.abs(sceneNumber(controls.orbitVelocityPitch, 0)) > SCENE_ORBIT_STOP_SPEED;
  }

  function sceneOrbitApplyInertia(controls, readSourceCamera, deltaSeconds) {
    if (!sceneOrbitInertiaActive(controls)) return false;
    if (!controls.orbit) {
      syncSceneControlsFromSource(controls, readSourceCamera);
    }
    if (!controls.orbit) return false;
    const seconds = Math.max(0.001, Math.min(0.05, sceneNumber(deltaSeconds, 0)));
    controls.orbit.yaw += sceneNumber(controls.orbitVelocityYaw, 0) * seconds;
    const pitchLimit = sceneOrbitPitchLimit(controls.pitchLimit);
    const nextPitch = sceneClamp(
      controls.orbit.pitch + sceneNumber(controls.orbitVelocityPitch, 0) * seconds,
      -pitchLimit,
      pitchLimit,
    );
    if (nextPitch === -pitchLimit || nextPitch === pitchLimit) {
      controls.orbitVelocityPitch = 0;
    }
    controls.orbit.pitch = nextPitch;
    const damping = Math.exp(-SCENE_ORBIT_DAMPING * seconds);
    controls.orbitVelocityYaw *= damping;
    controls.orbitVelocityPitch *= damping;
    if (Math.abs(controls.orbitVelocityYaw) <= SCENE_ORBIT_STOP_SPEED) controls.orbitVelocityYaw = 0;
    if (Math.abs(controls.orbitVelocityPitch) <= SCENE_ORBIT_STOP_SPEED) controls.orbitVelocityPitch = 0;
    controls.touched = true;
    return true;
  }

  function syncSceneControlsFromCamera(controls, camera) {
    if (!controls || controls.active || controls.touched) {
      return;
    }
    if (controls.mode === "orbit") {
      controls.orbit = sceneOrbitStateFromCamera(camera, controls.target, controls);
    } else if (controls.mode === "first-person" || controls.mode === "fly") {
      controls.fly = sceneFlyStateFromCamera(camera);
    }
  }

  function applySceneControlsCamera(controls, camera) {
    if (!controls) {
      return;
    }
    controls.active = false;
    controls.touched = true;
    controls.pointerId = null;
    sceneOrbitStopInertia(controls);
    if (controls.keys && typeof controls.keys.clear === "function") {
      controls.keys.clear();
    }
    if (controls.mode === "orbit") {
      controls.orbit = sceneOrbitStateFromCamera(camera, controls.target, controls);
    } else if (controls.mode === "first-person" || controls.mode === "fly") {
      controls.fly = sceneFlyStateFromCamera(camera);
    }
  }

  function sceneScrollViewportHeight() {
    const scrollingElement = document.scrollingElement || document.documentElement || document.body;
    const visualViewport = window.visualViewport;
    return Math.max(1, sceneNumber(
      visualViewport && visualViewport.height,
      sceneNumber(window.innerHeight, sceneNumber(scrollingElement && scrollingElement.clientHeight, 0)),
    ));
  }

  function sceneScrollTop() {
    const scrollingElement = document.scrollingElement || document.documentElement || document.body;
    const visualViewport = window.visualViewport;
    const visualViewportTop = sceneNumber(visualViewport && visualViewport.pageTop, NaN);
    if (Number.isFinite(visualViewportTop)) {
      return Math.max(0, visualViewportTop);
    }
    return Math.max(0, sceneNumber(
      window.scrollY,
      sceneNumber(window.pageYOffset, sceneNumber(scrollingElement && scrollingElement.scrollTop, 0)),
    ));
  }

  function sceneScrollMax() {
    const scrollingElement = document.scrollingElement || document.documentElement || document.body;
    const scrollHeight = Math.max(
      sceneNumber(scrollingElement && scrollingElement.scrollHeight, 0),
      sceneNumber(document.documentElement && document.documentElement.scrollHeight, 0),
      sceneNumber(document.body && document.body.scrollHeight, 0),
    );
    return Math.max(1, scrollHeight - sceneScrollViewportHeight());
  }

  function sceneUpdateScrollCameraMetrics(scrollCamera, includeMax, activeInput) {
    if (!scrollCamera) {
      return;
    }
    scrollCamera._scrollTop = sceneScrollTop();
    if (includeMax || !Number.isFinite(sceneNumber(scrollCamera._scrollMax, NaN))) {
      scrollCamera._scrollMax = sceneScrollMax();
    }
    if (activeInput) {
      scrollCamera._activeInputUntil = sceneNowMilliseconds() + 180;
    }
  }

  function sceneAdvanceScrollCamera(scrollCamera) {
    if (!scrollCamera || scrollCamera.start === scrollCamera.end) {
      return;
    }
    const scrollTop = sceneNumber(scrollCamera._scrollTop, 0);
    const scrollMax = Math.max(1, sceneNumber(scrollCamera._scrollMax, 1));
    scrollCamera._progress = Math.pow(Math.min(1, Math.max(0, scrollTop / scrollMax)), 0.5);
    var target = scrollCamera._progress || 0;
    var current = sceneNumber(scrollCamera._smoothProgress, target);
    if (sceneNumber(scrollCamera._activeInputUntil, 0) >= sceneNowMilliseconds()) {
      current = target;
    } else if (Math.abs(target - current) < 0.0005) {
      current = target;
    } else {
      current += (target - current) * 0.08;
    }
    scrollCamera._smoothProgress = current;
  }

  function sceneCurrentControlCamera(controls, sourceCamera, scrollCamera) {
    var cam;
    if (controls && controls.mode === "orbit") {
      syncSceneControlsFromCamera(controls, sourceCamera);
      cam = controls.orbit ? sceneOrbitCamera(controls.orbit, sourceCamera) : sceneRenderCamera(sourceCamera);
    } else if (controls && (controls.mode === "first-person" || controls.mode === "fly")) {
      syncSceneControlsFromCamera(controls, sourceCamera);
      cam = controls.fly ? sceneFlyCamera(controls.fly, sourceCamera) : sceneRenderCamera(sourceCamera);
    } else {
      cam = sceneRenderCamera(sourceCamera);
    }
    if (scrollCamera && scrollCamera.start !== scrollCamera.end) {
      var progress = sceneNumber(scrollCamera._smoothProgress, sceneNumber(scrollCamera._progress, 0));
      cam.z = scrollCamera.start + progress * (scrollCamera.end - scrollCamera.start);
    }
    return cam;
  }

  function sceneBundleWithCameraOverride(bundle, camera) {
    if (!bundle) {
      return bundle;
    }
    const targetCamera = sceneRenderCamera(camera);
    const sourceCamera = sceneRenderCamera(bundle.camera);
    if (sceneCameraEquivalent(targetCamera, sourceCamera)) {
      return bundle;
    }
    return Object.assign({}, bundle, {
      camera: targetCamera,
      sourceCamera: sourceCamera,
    });
  }

  function sceneControlsMetrics(readViewport, props) {
    const viewport = typeof readViewport === "function" ? readViewport() : null;
    return {
      width: Math.max(1, sceneViewportValue(viewport, "cssWidth", sceneNumber(props && props.width, 720))),
      height: Math.max(1, sceneViewportValue(viewport, "cssHeight", sceneNumber(props && props.height, 420))),
    };
  }

  function syncSceneControlsFromSource(controls, readSourceCamera) {
    const sourceCamera = typeof readSourceCamera === "function" ? readSourceCamera() : null;
    syncSceneControlsFromCamera(controls, sourceCamera);
  }

  function sceneOrbitStartDrag(controls, canvas, props, readViewport, readSourceCamera, attachDocumentListeners, event) {
    if (controls.active || !scenePointerCanStartDrag(controls, event)) {
      return;
    }
    syncSceneControlsFromSource(controls, readSourceCamera);
    controls.active = true;
    controls.touched = true;
    controls.pointerId = event.pointerId;
    sceneOrbitStopInertia(controls);
    controls.orbitLastMoveMS = sceneNumber(event && event.timeStamp, sceneNowMilliseconds());
    const metrics = sceneControlsMetrics(readViewport, props);
    const point = sceneLocalPointerPoint(event, canvas, metrics.width, metrics.height);
    controls.lastX = point.x;
    controls.lastY = point.y;
    canvas.style.cursor = "grabbing";
    attachDocumentListeners();
    if (typeof canvas.setPointerCapture === "function" && event.pointerId != null) {
      canvas.setPointerCapture(event.pointerId);
    }
    if (typeof event.preventDefault === "function") {
      event.preventDefault();
    }
    if (typeof event.stopPropagation === "function") {
      event.stopPropagation();
    }
  }

  function sceneOrbitMoveDrag(controls, canvas, props, readViewport, readSourceCamera, scheduleRender, event) {
    if (!sceneDragMatchesActivePointer(controls, event)) {
      return;
    }
    const metrics = sceneControlsMetrics(readViewport, props);
    const sample = sceneLocalPointerSample(event, canvas, metrics.width, metrics.height, controls, "move");
    if (!controls.orbit) {
      syncSceneControlsFromSource(controls, readSourceCamera);
    }
    const now = sceneNumber(event && event.timeStamp, sceneNowMilliseconds());
    const seconds = Math.max((now - sceneNumber(controls.orbitLastMoveMS, now)) / 1000, 1 / 240);
    const rotateSpeed = sceneNumber(controls.rotateSpeed, 1);
    const pixelRadians = (Math.PI / 180) * rotateSpeed;
    const deltaYaw = controls.rotateMode === "pixel-degrees"
      ? sample.deltaX * pixelRadians
      : (sample.deltaX / Math.max(metrics.width, 1)) * Math.PI * rotateSpeed;
    const deltaPitch = controls.rotateMode === "pixel-degrees"
      ? sample.deltaY * pixelRadians
      : (sample.deltaY / Math.max(metrics.height, 1)) * Math.PI * rotateSpeed;
    const pitchLimit = sceneOrbitPitchLimit(controls.pitchLimit);
    controls.orbit.yaw += deltaYaw;
    controls.orbit.pitch = sceneClamp(
      controls.orbit.pitch + deltaPitch,
      -pitchLimit,
      pitchLimit,
    );
    controls.orbitVelocityYaw = sceneClamp(deltaYaw / seconds, -SCENE_ORBIT_MAX_SPEED, SCENE_ORBIT_MAX_SPEED);
    controls.orbitVelocityPitch = sceneClamp(deltaPitch / seconds, -SCENE_ORBIT_MAX_SPEED, SCENE_ORBIT_MAX_SPEED);
    controls.orbitLastMoveMS = now;
    if (typeof event.preventDefault === "function") {
      event.preventDefault();
    }
    if (typeof event.stopPropagation === "function") {
      event.stopPropagation();
    }
    scheduleRender("controls");
  }

  function sceneOrbitFinishDrag(controls, canvas, detachDocumentListeners, event, scheduleOrbitInertia) {
    if (!sceneDragMatchesActivePointer(controls, event)) {
      return;
    }
    const pointerId = controls.pointerId;
    const releaseTime = sceneNumber(event && event.timeStamp, sceneNowMilliseconds());
    const releaseDelay = Math.max(0, (releaseTime - sceneNumber(controls.orbitLastMoveMS, releaseTime)) / 1000);
    const releaseDamping = Math.exp(-SCENE_ORBIT_DAMPING * releaseDelay);
    controls.orbitVelocityYaw *= releaseDamping;
    controls.orbitVelocityPitch *= releaseDamping;
    controls.active = false;
    controls.pointerId = null;
    canvas.style.cursor = "grab";
    detachDocumentListeners();
    if (pointerId != null && typeof canvas.releasePointerCapture === "function") {
      try {
        canvas.releasePointerCapture(pointerId);
      } catch (_error) {}
    }
    if (event && typeof event.preventDefault === "function") {
      event.preventDefault();
    }
    if (event && typeof event.stopPropagation === "function") {
      event.stopPropagation();
    }
    if (typeof scheduleOrbitInertia === "function") {
      scheduleOrbitInertia();
    }
  }

  function sceneOrbitApplyWheel(controls, readSourceCamera, scheduleRender, event) {
    syncSceneControlsFromSource(controls, readSourceCamera);
    sceneOrbitStopInertia(controls);
    controls.touched = true;
    controls.orbit.radius = sceneClamp(
      controls.orbit.radius * Math.exp(sceneNumber(event && event.deltaY, 0) * 0.001 * controls.zoomSpeed),
      Math.max(0.001, sceneNumber(controls.minDistance, SCENE_ORBIT_DEFAULT_MIN_DISTANCE)),
      Math.max(sceneNumber(controls.minDistance, SCENE_ORBIT_DEFAULT_MIN_DISTANCE), sceneNumber(controls.maxDistance, SCENE_ORBIT_DEFAULT_MAX_DISTANCE)),
    );
    if (event && typeof event.preventDefault === "function") {
      event.preventDefault();
    }
    if (event && typeof event.stopPropagation === "function") {
      event.stopPropagation();
    }
    scheduleRender("controls");
  }

  function sceneFlyEnsureState(controls, readSourceCamera) {
    if (!controls.fly) {
      syncSceneControlsFromSource(controls, readSourceCamera);
    }
    if (!controls.fly) {
      controls.fly = sceneFlyStateFromCamera(null);
    }
    if (!controls.fly.position) {
      controls.fly.position = { x: 0, y: 0, z: -6 };
    }
    return controls.fly;
  }

  function sceneFlyStartDrag(controls, canvas, props, readViewport, readSourceCamera, attachDocumentListeners, event) {
    if (controls.active || !scenePointerCanStartDrag(controls, event)) {
      return;
    }
    sceneFlyEnsureState(controls, readSourceCamera);
    controls.active = true;
    controls.touched = true;
    controls.pointerId = event.pointerId;
    const metrics = sceneControlsMetrics(readViewport, props);
    const point = sceneLocalPointerPoint(event, canvas, metrics.width, metrics.height);
    controls.lastX = point.x;
    controls.lastY = point.y;
    canvas.style.cursor = "grabbing";
    if (typeof canvas.focus === "function") {
      canvas.focus({ preventScroll: true });
    }
    attachDocumentListeners();
    if (controls.pointerLock) {
      sceneRequestPointerLock(canvas);
      controls.pointerLocked = scenePointerLockActive(canvas);
    }
    if (typeof canvas.setPointerCapture === "function" && event.pointerId != null) {
      canvas.setPointerCapture(event.pointerId);
    }
    if (typeof event.preventDefault === "function") {
      event.preventDefault();
    }
    if (typeof event.stopPropagation === "function") {
      event.stopPropagation();
    }
  }

  function sceneFlyMoveDrag(controls, canvas, props, readViewport, readSourceCamera, scheduleRender, event) {
    if (!sceneDragMatchesActivePointer(controls, event)) {
      return;
    }
    const metrics = sceneControlsMetrics(readViewport, props);
    const pointerLocked = controls.pointerLock && scenePointerLockActive(canvas);
    controls.pointerLocked = pointerLocked;
    const sample = pointerLocked ? {
      deltaX: sceneNumber(event && event.movementX, 0),
      deltaY: sceneNumber(event && event.movementY, 0),
    } : sceneLocalPointerSample(event, canvas, metrics.width, metrics.height, controls, "move");
    const fly = sceneFlyEnsureState(controls, readSourceCamera);
    fly.yaw += (sample.deltaX / Math.max(metrics.width, 1)) * Math.PI * controls.lookSpeed;
    fly.pitch = sceneClamp(
      fly.pitch + (sample.deltaY / Math.max(metrics.height, 1)) * Math.PI * controls.lookSpeed,
      -1.52,
      1.52,
    );
    if (typeof event.preventDefault === "function") {
      event.preventDefault();
    }
    if (typeof event.stopPropagation === "function") {
      event.stopPropagation();
    }
    scheduleRender("controls");
  }

  function sceneFlyFinishDrag(controls, canvas, detachDocumentListeners, event) {
    if (!sceneDragMatchesActivePointer(controls, event)) {
      return;
    }
    if (controls.pointerLock && scenePointerLockActive(canvas) && event && event.type !== "pointerlockchange") {
      if (typeof event.preventDefault === "function") {
        event.preventDefault();
      }
      if (typeof event.stopPropagation === "function") {
        event.stopPropagation();
      }
      return;
    }
    const pointerId = controls.pointerId;
    controls.active = false;
    controls.pointerId = null;
    controls.pointerLocked = false;
    canvas.style.cursor = "crosshair";
    detachDocumentListeners();
    if (pointerId != null && typeof canvas.releasePointerCapture === "function") {
      try {
        canvas.releasePointerCapture(pointerId);
      } catch (_error) {}
    }
    if (event && typeof event.preventDefault === "function") {
      event.preventDefault();
    }
    if (event && typeof event.stopPropagation === "function") {
      event.stopPropagation();
    }
  }

  function sceneFlyKeyCode(event) {
    const code = String(event && (event.code || event.key) || "").toLowerCase();
    switch (code) {
      case "keyw":
      case "w":
      case "arrowup":
        return "forward";
      case "keys":
      case "s":
      case "arrowdown":
        return "back";
      case "keya":
      case "a":
      case "arrowleft":
        return "left";
      case "keyd":
      case "d":
      case "arrowright":
        return "right";
      case "space":
      case " ":
        return "up";
      case "shiftleft":
      case "shiftright":
      case "controlleft":
      case "controlright":
        return "down";
      default:
        return "";
    }
  }

  function sceneFlyApplyMovement(controls, readSourceCamera, deltaSeconds) {
    if (!controls || !controls.keys || controls.keys.size === 0) {
      return false;
    }
    const fly = sceneFlyEnsureState(controls, readSourceCamera);
    const speed = controls.moveSpeed * Math.max(0.001, deltaSeconds || 1 / 60);
    const yaw = sceneNumber(fly.yaw, 0);
    const pitch = controls.mode === "fly" ? sceneNumber(fly.pitch, 0) : 0;
    const cosPitch = Math.cos(pitch);
    const forward = {
      x: Math.sin(yaw) * cosPitch,
      y: -Math.sin(pitch),
      z: -Math.cos(yaw) * cosPitch,
    };
    const right = { x: Math.cos(yaw), y: 0, z: Math.sin(yaw) };
    let dx = 0;
    let dy = 0;
    let dz = 0;
    if (controls.keys.has("forward")) {
      dx += forward.x; dy += forward.y; dz += forward.z;
    }
    if (controls.keys.has("back")) {
      dx -= forward.x; dy -= forward.y; dz -= forward.z;
    }
    if (controls.keys.has("right")) {
      dx += right.x; dz += right.z;
    }
    if (controls.keys.has("left")) {
      dx -= right.x; dz -= right.z;
    }
    if (controls.keys.has("up")) {
      dy += 1;
    }
    if (controls.keys.has("down")) {
      dy -= 1;
    }
    const length = Math.hypot(dx, dy, dz);
    if (length <= 0.0001) {
      return false;
    }
    fly.position.x += (dx / length) * speed;
    fly.position.y += (dy / length) * speed;
    fly.position.z += (dz / length) * speed;
    controls.touched = true;
    return true;
  }

  function setupSceneBuiltInControls(canvas, props, readViewport, readSourceCamera, scheduleRender) {
    const controls = createSceneControls(props);
    if (!canvas || !controls) {
      return {
        controller: controls,
        dispose() {},
      };
    }

    let documentListenersAttached = false;
    let orbitFrame = 0;
    let orbitLastFrameMS = 0;
    let flyFrame = 0;
    let flyLastFrameMS = 0;
    const flyMode = controls.mode === "first-person" || controls.mode === "fly";
    canvas.style.cursor = flyMode ? "crosshair" : "grab";
    canvas.style.touchAction = "none";
    if (flyMode && !canvas.hasAttribute("tabindex")) {
      canvas.setAttribute("tabindex", "0");
    }

    function onPointerLockChange(event) {
      if (!flyMode || !controls.pointerLock) {
        return;
      }
      const locked = scenePointerLockActive(canvas);
      controls.pointerLocked = locked;
      if (!locked && controls.active) {
        sceneFlyFinishDrag(controls, canvas, detachDocumentListeners, event || { type: "pointerlockchange" });
      }
    }

    function attachDocumentListeners() {
      if (documentListenersAttached) {
        return;
      }
      documentListenersAttached = true;
      document.addEventListener("pointermove", onPointerMove);
      document.addEventListener("pointerup", finishPointerDrag);
      document.addEventListener("pointercancel", finishPointerDrag);
    }

    function detachDocumentListeners() {
      if (!documentListenersAttached) {
        return;
      }
      documentListenersAttached = false;
      document.removeEventListener("pointermove", onPointerMove);
      document.removeEventListener("pointerup", finishPointerDrag);
      document.removeEventListener("pointercancel", finishPointerDrag);
    }

    function cancelOrbitInertia() {
      if (orbitFrame) {
        cancelAnimationFrame(orbitFrame);
        orbitFrame = 0;
      }
    }

    function scheduleOrbitInertia() {
      if (flyMode || orbitFrame || !sceneOrbitInertiaActive(controls)) {
        return;
      }
      orbitLastFrameMS = sceneNowMilliseconds();
      const step = function(now) {
        orbitFrame = 0;
        const current = sceneNumber(now, sceneNowMilliseconds());
        const delta = Math.min(0.05, Math.max(0.001, (current - orbitLastFrameMS) / 1000));
        orbitLastFrameMS = current;
        if (sceneOrbitApplyInertia(controls, readSourceCamera, delta)) {
          scheduleRender("controls-inertia");
        }
        if (sceneOrbitInertiaActive(controls)) {
          orbitFrame = requestAnimationFrame(step);
        }
      };
      orbitFrame = requestAnimationFrame(step);
    }

    function onPointerDown(event) {
      if (flyMode) {
        sceneFlyStartDrag(controls, canvas, props, readViewport, readSourceCamera, attachDocumentListeners, event);
      } else {
        cancelOrbitInertia();
        sceneOrbitStartDrag(controls, canvas, props, readViewport, readSourceCamera, attachDocumentListeners, event);
      }
    }

    function onPointerMove(event) {
      if (flyMode) {
        sceneFlyMoveDrag(controls, canvas, props, readViewport, readSourceCamera, scheduleRender, event);
      } else {
        sceneOrbitMoveDrag(controls, canvas, props, readViewport, readSourceCamera, scheduleRender, event);
      }
    }

    function finishPointerDrag(event) {
      if (flyMode) {
        sceneFlyFinishDrag(controls, canvas, detachDocumentListeners, event);
      } else {
        sceneOrbitFinishDrag(controls, canvas, detachDocumentListeners, event, scheduleOrbitInertia);
      }
    }

    function onWheel(event) {
      if (!flyMode) {
        sceneOrbitApplyWheel(controls, readSourceCamera, scheduleRender, event);
      }
    }

    function scheduleFlyMovement() {
      if (!flyMode || flyFrame || controls.keys.size === 0) {
        return;
      }
      flyLastFrameMS = sceneNowMilliseconds();
      const step = function(now) {
        flyFrame = 0;
        const current = sceneNumber(now, sceneNowMilliseconds());
        const delta = Math.min(0.05, Math.max(0.001, (current - flyLastFrameMS) / 1000));
        flyLastFrameMS = current;
        if (sceneFlyApplyMovement(controls, readSourceCamera, delta)) {
          scheduleRender("controls");
        }
        if (controls.keys.size > 0) {
          flyFrame = requestAnimationFrame(step);
        }
      };
      flyFrame = requestAnimationFrame(step);
    }

    function onKeyDown(event) {
      if (!flyMode || (document.activeElement !== canvas && !controls.touched)) {
        return;
      }
      const key = sceneFlyKeyCode(event);
      if (!key) {
        return;
      }
      controls.keys.add(key);
      sceneFlyApplyMovement(controls, readSourceCamera, 1 / 60);
      scheduleFlyMovement();
      scheduleRender("controls");
      if (typeof event.preventDefault === "function") {
        event.preventDefault();
      }
    }

    function onKeyUp(event) {
      if (!flyMode) {
        return;
      }
      const key = sceneFlyKeyCode(event);
      if (!key) {
        return;
      }
      controls.keys.delete(key);
      if (typeof event.preventDefault === "function") {
        event.preventDefault();
      }
    }

    canvas.addEventListener("pointerdown", onPointerDown);
    canvas.addEventListener("pointermove", onPointerMove);
    canvas.addEventListener("pointerup", finishPointerDrag);
    canvas.addEventListener("pointercancel", finishPointerDrag);
    canvas.addEventListener("lostpointercapture", finishPointerDrag);
    canvas.addEventListener("wheel", onWheel);
    if (flyMode) {
      document.addEventListener("keydown", onKeyDown);
      document.addEventListener("keyup", onKeyUp);
      if (controls.pointerLock) {
        document.addEventListener("pointerlockchange", onPointerLockChange);
        document.addEventListener("mozpointerlockchange", onPointerLockChange);
        document.addEventListener("webkitpointerlockchange", onPointerLockChange);
      }
    }

    return {
      controller: controls,
      dispose() {
        detachDocumentListeners();
        cancelOrbitInertia();
        if (flyFrame) {
          cancelAnimationFrame(flyFrame);
          flyFrame = 0;
        }
        canvas.removeEventListener("pointerdown", onPointerDown);
        canvas.removeEventListener("pointermove", onPointerMove);
        canvas.removeEventListener("pointerup", finishPointerDrag);
        canvas.removeEventListener("pointercancel", finishPointerDrag);
        canvas.removeEventListener("lostpointercapture", finishPointerDrag);
        canvas.removeEventListener("wheel", onWheel);
        if (flyMode) {
          sceneExitPointerLock(canvas);
          document.removeEventListener("keydown", onKeyDown);
          document.removeEventListener("keyup", onKeyUp);
          if (controls.pointerLock) {
            document.removeEventListener("pointerlockchange", onPointerLockChange);
            document.removeEventListener("mozpointerlockchange", onPointerLockChange);
            document.removeEventListener("webkitpointerlockchange", onPointerLockChange);
          }
        }
      },
    };
  }

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
    };
  };

  window.__gosx_register_engine_factory("GoSXScene3D", async function(ctx) {
    if (!ctx.mount || typeof document.createElement !== "function") {
      console.warn("[gosx] Scene3D requires a mount element");
      return {};
    }

    const props = ctx.props || {};
    const capability = sceneCapabilityProfile(props);
    const viewportBase = sceneViewportBase(props);
    const adaptiveQuality = createSceneAdaptiveQualityState(props, viewportBase, capability);
    const sceneState = createSceneState(props, capability);
    // The manifest is immutable for the lifetime of an engine mount. Parse its
    // large inline shader payload once instead of once per rendered frame.
    const mountedWaterShaderSources = typeof window !== "undefined" &&
      window.__gosx_scene3d_water_shader_sources_by_id &&
      typeof window.__gosx_scene3d_water_shader_sources_by_id === "object"
      ? window.__gosx_scene3d_water_shader_sources_by_id
      : sceneMountedWaterShaderSources();
    if (ctx.mount && typeof window !== "undefined" && window.__gosx_scene3d_water_shader_sources_by_id) {
      ctx.mount.__gosxScene3DWaterShaderSources = window.__gosx_scene3d_water_shader_sources_by_id;
    }
    const sceneModelHydration = hydrateSceneStateModels(sceneState, props);
    const runtimeScene = ctx.runtimeMode === "shared" && Boolean(ctx.programRef);
    const lifecycle = initialSceneLifecycleState();
    const motion = initialSceneMotionState(props);
    let sceneCSSAnimationUntil = 0;
    let lastModelAnimationTimeSeconds = null;
    // WASM motion seam (P2.4b). Opt-in via window.__gosx_motion_wasm; all state
    // stays inert when the flag is unset. wasmMotionState: 0=unloaded, 1=loaded,
    // -1=disabled (load failed or no program, never retried).
    let wasmMotionState = 0;
    let wasmMotionHandle = 0;
    let wasmMotionTargetRefs = null;
    let wasmMotionPropRefs = null;
    let wasmMotionF64 = null;
    let wasmMotionU8 = null;
    // C3: material-uniform motion seam. Mirrors wasmMotion* but loads
    // props.scene.materialMotionProgram and writes evaluated values into each
    // mesh's customUniforms so selena re-packs them every frame. Same opt-in
    // flag + state lifecycle (0=unloaded, 1=loaded, -1=disabled).
    let wasmMatMotionState = 0;
    let wasmMatMotionHandle = 0;
    let wasmMatMotionTargetRefs = null;
    let wasmMatMotionPropRefs = null;
    let wasmMatMotionF64 = null;
    let wasmMatMotionU8 = null;

    function sceneAnimationState() {
      if (motion.reducedMotion) {
        return { wants: false, reason: "reduced-motion" };
      }
      if (ctx.mount && ctx.mount.__gosxScene3DCSSDynamic && Date.now() < sceneCSSAnimationUntil) {
        return { wants: true, reason: "css-transition" };
      }
      if (sceneHasActiveTransitions(sceneState)) {
        return { wants: true, reason: "scene-transition" };
      }
      if (runtimeScene) {
        return { wants: true, reason: "runtime-program" };
      }
      if (sceneBool(props.autoRotate, false)) {
        return { wants: true, reason: "auto-rotate" };
      }
      if (Array.isArray(sceneState.computeParticles) && sceneState.computeParticles.length > 0) {
        return { wants: true, reason: "compute-particles" };
      }
      if (Array.isArray(sceneState.waterSystems) && sceneState.waterSystems.length > 0) {
        if (sceneWaterSystemsPaused(sceneState)) {
          return { wants: false, reason: "water-paused" };
        }
        return { wants: true, reason: "water-simulation" };
      }
      if (sceneHasActiveModelAnimations(sceneState)) {
        return { wants: true, reason: "model-animation" };
      }
      if (Array.isArray(sceneState.points) && sceneState.points.some(function(p) {
        return sceneNumber(p.spinX, 0) !== 0 || sceneNumber(p.spinY, 0) !== 0 || sceneNumber(p.spinZ, 0) !== 0;
      })) {
        return { wants: true, reason: "point-spin" };
      }
      if (sceneStateObjects(sceneState).some(sceneObjectAnimated)) {
        return { wants: true, reason: "object-animation" };
      }
      if (sceneStateLabels(sceneState).some(sceneLabelAnimated)) {
        return { wants: true, reason: "label-animation" };
      }
      if (sceneStateSprites(sceneState).some(sceneSpriteAnimated)) {
        return { wants: true, reason: "sprite-animation" };
      }
      if (sceneStateHTML(sceneState).some(sceneHTMLAnimated)) {
        return { wants: true, reason: "html-animation" };
      }
      return { wants: false, reason: "static" };
    }

    function sceneShouldAnimate() {
      return sceneAnimationState().wants;
    }

    // Extract CSS var transition timing from materials/environment so the
    // planner can interpolate when resolved var values change. The planner
    // runs on the render bundle which no longer has the original materials
    // array, so we stash the timing on the mount element.
    ctx.mount.__gosxScene3DCSSVarTransition = sceneExtractCSSVarTransitionTiming(props);

    clearChildren(ctx.mount);
    ctx.mount.setAttribute("data-gosx-scene3d-mounted", "true");
    ctx.mount.setAttribute("aria-label", props.ariaLabel || props.label || "Interactive GoSX 3D scene");
    setAttrValue(ctx.mount, "data-gosx-scene3d-controls", normalizeSceneControlsMode(props.controls));
    setAttrValue(ctx.mount, "data-gosx-scene3d-pick-signals", scenePickSignalNamespace(props));
    setAttrValue(ctx.mount, "data-gosx-scene3d-event-signals", sceneEventSignalNamespace(props));
    applySceneCapabilityState(ctx.mount, props, capability);
    if (!ctx.mount.style.position) {
      ctx.mount.style.position = "relative";
    }
    function createSceneMountCanvas() {
      const nextCanvas = document.createElement("canvas");
      nextCanvas.setAttribute("data-gosx-scene3d-canvas", "true");
      nextCanvas.setAttribute("role", "img");
      nextCanvas.setAttribute("aria-label", props.label || "Interactive GoSX 3D scene");
      nextCanvas.style.maxWidth = "100%";
      nextCanvas.style.borderRadius = "inherit";
      nextCanvas.width = viewportBase.baseWidth;
      nextCanvas.height = viewportBase.baseHeight;
      nextCanvas.setAttribute("width", String(viewportBase.baseWidth));
      nextCanvas.setAttribute("height", String(viewportBase.baseHeight));
      return nextCanvas;
    }

    let canvas = createSceneMountCanvas();
    ctx.mount.appendChild(canvas);
    scenePublishWaterShaderSourcesToMount(ctx.mount, canvas, mountedWaterShaderSources);
    setAttrValue(ctx.mount, "data-gosx-scene3d-water-frame-seq",
      Array.isArray(sceneState.waterSystems) && sceneState.waterSystems.length ? "0" : "");
    setAttrValue(ctx.mount, "data-gosx-scene3d-water-simulation-seq",
      Array.isArray(sceneState.waterSystems) && sceneState.waterSystems.length ? "0" : "");

    const labelLayer = document.createElement("div");
    labelLayer.setAttribute("data-gosx-scene3d-label-layer", "true");
    labelLayer.setAttribute("aria-hidden", "true");
    ctx.mount.appendChild(labelLayer);
    const statsOverlay = createSceneStatsOverlay(ctx.mount, sceneBool(props.stats, false));
    let inspectorOverlay = null;

    const sentinelLayer = document.createElement("div");
    sentinelLayer.setAttribute("data-gosx-scene-node-layer", "true");
    sentinelLayer.setAttribute("aria-hidden", "true");
    sentinelLayer.style.position = "absolute";
    sentinelLayer.style.inset = "0";
    sentinelLayer.style.width = "0";
    sentinelLayer.style.height = "0";
    sentinelLayer.style.overflow = "visible";
    sentinelLayer.style.pointerEvents = "none";

    const sceneNodeSentinels = new Map();
    ctx.mount.__gosxScene3DSentinels = sceneNodeSentinels;
    // Live sceneState handle for inspection (debug/test): lets callers read the
    // mutable object/material state — e.g. customUniforms written by the C3
    // material-motion seam — without going through the depth-clamped debug
    // snapshot.
    ctx.mount.__gosxScene3DState = sceneState;
    publishSceneWaterStateSnapshot(ctx.mount, sceneState);
    ctx.mount.__gosxScene3DCSSDynamic = false;
    ctx.mount.__gosxScene3DCSSRevision = 1;
    ctx.mount.__gosxScene3DCSSAnimationUntil = 0;
    applyScenePostFXState(ctx.mount, sceneState);

    await settlePreferredWebGPUBackend(props, capability);

    let viewport = applySceneViewport(ctx.mount, canvas, labelLayer, sceneViewportFromMount(ctx.mount, props, viewportBase, canvas, capability, adaptiveQuality), viewportBase);
    scenePrimeAdaptiveQuality(adaptiveQuality, viewport, ctx.mount);

    const initialRenderer = createSceneRenderer(canvas, props, capability);
    if (!initialRenderer || !initialRenderer.renderer) {
      console.warn("[gosx] Scene3D could not acquire a renderer");
      const unsupportedReason = initialRenderer && initialRenderer.unsupportedReason
        ? initialRenderer.unsupportedReason
        : (sceneRequiresWebGL(props) ? "webgl-required" : "renderer-unavailable");
      applySceneRendererState(ctx.mount, { kind: "unsupported" }, unsupportedReason);
      publishSceneWaterRendererState(ctx.mount, sceneState, null, unsupportedReason);
      publishSceneWaterLifecycleState(ctx.mount, sceneState, lifecycle, false);
      setAttrValue(ctx.mount, "data-gosx-scene3d-ready", "false");
      if (canvas.parentNode === ctx.mount) {
        ctx.mount.removeChild(canvas);
      }
      if (labelLayer.parentNode === ctx.mount) {
        ctx.mount.removeChild(labelLayer);
      }
      if (statsOverlay) {
        statsOverlay.dispose();
      }
      if (sentinelLayer.parentNode) {
        sentinelLayer.parentNode.removeChild(sentinelLayer);
      }
      delete ctx.mount.__gosxScene3DSentinels;
      delete ctx.mount.__gosxScene3DCSSDynamic;
      delete ctx.mount.__gosxScene3DCSSRevision;
      delete ctx.mount.__gosxScene3DCSSAnimationUntil;
      showSceneRequiredRendererMessage(ctx.mount, props, unsupportedReason);
      return {
        dispose() {
          const unsupported = ctx.mount.querySelector
            ? ctx.mount.querySelector("[data-gosx-scene3d-unsupported]")
            : null;
          if (unsupported && unsupported.parentNode === ctx.mount) {
            ctx.mount.removeChild(unsupported);
          }
        },
      };
    }
    if (!sentinelLayer.parentNode) {
      canvas.appendChild(sentinelLayer);
    }
    let renderer = initialRenderer.renderer;
    applySceneRendererState(ctx.mount, renderer, initialRenderer.fallbackReason || "", initialRenderer.degraded || []);
    publishSceneWaterRendererState(ctx.mount, sceneState, renderer, "");
    publishSceneWaterLifecycleState(ctx.mount, sceneState, lifecycle, false);
    let latestBundle = null;
    const labelLayoutCache = new Map();
    const labelElements = new Map();
    const spriteElements = new Map();
    const htmlElements = new Map();
    const htmlTextureState = createSceneHTMLTextureState();
    htmlTextureState.requestRender = scheduleRender;
    let labelRefreshHandle = null;

    function syncSceneNodeSentinels(bundle) {
      const next = new Set();
      collectSceneNodeSentinelIDs(next, bundle && bundle.meshObjects);
      collectSceneNodeSentinelIDs(next, bundle && bundle.objects);
      collectSceneNodeSentinelIDs(next, bundle && bundle.points);
      collectSceneNodeSentinelIDs(next, bundle && bundle.instancedMeshes);
      collectSceneNodeSentinelIDs(next, bundle && bundle.computeParticles);
      collectSceneNodeSentinelIDs(next, bundle && bundle.lights);
      collectSceneNodeSentinelIDs(next, bundle && bundle.labels);
      collectSceneNodeSentinelIDs(next, bundle && bundle.sprites);
      collectSceneNodeSentinelIDs(next, bundle && bundle.html);
      next.forEach(function(id) {
        if (sceneNodeSentinels.has(id)) {
          return;
        }
        const sentinel = document.createElement("div");
        sentinel.setAttribute("data-gosx-scene-node", id);
        sentinel.setAttribute("aria-hidden", "true");
        sentinel.style.position = "absolute";
        sentinel.style.left = "0";
        sentinel.style.top = "0";
        sentinel.style.width = "1px";
        sentinel.style.height = "1px";
        sentinel.style.opacity = "0";
        sentinel.style.pointerEvents = "auto";
        sentinelLayer.appendChild(sentinel);
        sceneNodeSentinels.set(id, sentinel);
      });
      sceneNodeSentinels.forEach(function(sentinel, id) {
        if (next.has(id)) {
          return;
        }
        if (sentinel.parentNode === sentinelLayer) {
          sentinelLayer.removeChild(sentinel);
        }
        sceneNodeSentinels.delete(id);
      });
    }

    function collectSceneNodeSentinelIDs(target, entries) {
      if (!Array.isArray(entries)) {
        return;
      }
      for (let index = 0; index < entries.length; index += 1) {
        const entry = entries[index];
        const id = entry && entry.id;
        if (id != null && String(id).trim() !== "") {
          target.add(String(id));
        }
      }
    }

    const releaseTextLayoutListener = onTextLayoutInvalidated(function() {
      if (disposed || !latestBundle || !sceneCanRender()) {
        return;
      }
      if (labelRefreshHandle != null) {
        return;
      }
      labelRefreshHandle = engineFrame(function() {
        labelRefreshHandle = null;
        if (disposed || !latestBundle) {
          return;
        }
        renderSceneLabels(labelLayer, latestBundle, labelLayoutCache, labelElements, viewport.cssWidth, viewport.cssHeight);
        renderSceneSprites(labelLayer, latestBundle, spriteElements, viewport.cssWidth, viewport.cssHeight);
        renderSceneHTML(labelLayer, latestBundle, htmlElements, viewport.cssWidth, viewport.cssHeight, htmlTextureState);
      });
    });

    let frameHandle = null;
    let scheduledRenderHandle = null;
    let disposed = false;
    let lastScheduledRenderReason = "";
    let lastRenderLoopReason = "initializing";
    const SCENE_RENDER_WATCHDOG_INTERVAL_MS = 2000;
    const SCENE_RENDER_STALL_MS = 6500;
    const SCENE_RENDER_FALLBACK_STALL_MS = 12000;
    let renderWatchdogTimer = null;
    let renderWatchdogLastSeq = -1;
    let renderWatchdogLastAt = 0;
    let renderWatchdogLastAdvanceAt = 0;
    let renderWatchdogRecoveries = 0;
    let renderWatchdogFallbacks = 0;
    let renderWatchdogActiveReason = "";
    let webgpuProbeReadyListener = null;

    // Do not voluntarily lose WebGL while the page is hidden/offscreen.
    // A canvas that has owned WebGL generally cannot switch to a 2D context,
    // so forced loss leaves no useful fallback and some browsers restore late.
    let idleContextTimer = null;
    let contextVoluntarilyLost = false;
    let voluntaryLoseContextExtension = null;

    function sceneRenderLoopSnapshot(reason) {
      const animation = sceneAnimationState();
      let active = frameHandle != null || scheduledRenderHandle != null;
      let loopReason = reason || lastRenderLoopReason || animation.reason || "unknown";
      if (!sceneCanRender()) {
        active = false;
        loopReason = lifecycle.pageVisible ? "offscreen" : "page-hidden";
      } else if (scheduledRenderHandle != null) {
        loopReason = lastScheduledRenderReason || loopReason || "scheduled-render";
      } else if (frameHandle != null) {
        loopReason = animation.reason || loopReason || "animation";
      } else if (!animation.wants) {
        loopReason = animation.reason || "static";
      }
      return {
        active,
        wantsAnimation: animation.wants,
        reason: loopReason,
        scheduled: scheduledRenderHandle != null,
        animationFrame: frameHandle != null,
      };
    }

    function applySceneRenderLoopState(reason) {
      const state = sceneRenderLoopSnapshot(reason);
      lastRenderLoopReason = state.reason || "";
      setAttrValue(ctx.mount, "data-gosx-scene3d-render-loop", state.active ? "active" : "stopped");
      setAttrValue(ctx.mount, "data-gosx-scene3d-render-loop-reason", state.reason || "");
      setAttrValue(ctx.mount, "data-gosx-scene3d-render-loop-wants-animation", state.wantsAnimation ? "true" : "false");
      return state;
    }

    function readSceneWebGPUProgress() {
      const seq = Number(sceneDebugAttr(ctx.mount, "data-gosx-scene3d-webgpu-frame-seq"));
      const at = Number(sceneDebugAttr(ctx.mount, "data-gosx-scene3d-webgpu-frame-at"));
      return {
        seq: Number.isFinite(seq) ? seq : 0,
        at: Number.isFinite(at) ? at : 0,
      };
    }

    function publishSceneRenderWatchdogState(reason, stalledFor) {
      setAttrValue(ctx.mount, "data-gosx-scene3d-render-watchdog", reason ? "recovering" : "ok");
      setAttrValue(ctx.mount, "data-gosx-scene3d-render-watchdog-reason", reason || "");
      setAttrValue(ctx.mount, "data-gosx-scene3d-render-watchdog-stalled-ms", stalledFor > 0 ? Math.round(stalledFor) : "");
      setAttrValue(ctx.mount, "data-gosx-scene3d-render-watchdog-recoveries", renderWatchdogRecoveries || "");
      setAttrValue(ctx.mount, "data-gosx-scene3d-render-watchdog-fallbacks", renderWatchdogFallbacks || "");
    }

    function rendererReportsWebGPUFailure(diagnostics) {
      if (!diagnostics) {
        return "";
      }
      if (diagnostics.deviceLost) {
        return "webgpu-device-lost";
      }
      if (diagnostics.initFailed) {
        return "webgpu-init-failed";
      }
      if (diagnostics.ready === false) {
        return "webgpu-not-ready";
      }
      return "";
    }

    function recoverSceneWebGPURenderer(reason, stalledFor, forceFallback) {
      renderWatchdogRecoveries += 1;
      renderWatchdogActiveReason = reason || "webgpu-stalled";
      publishSceneRenderWatchdogState(renderWatchdogActiveReason, stalledFor || 0);
      gosxSceneEmit("warn", "render-watchdog-recovery", {
        rendererKind: renderer && renderer.kind ? renderer.kind : "",
        reason: renderWatchdogActiveReason,
        stalledForMS: Math.round(stalledFor || 0),
        recoveryCount: renderWatchdogRecoveries,
        forceFallback: !!forceFallback,
      });
      cancelFrame();
      cancelScheduledRender();
      viewportDirty = true;
      if (!forceFallback && renderer && renderer.kind === "webgpu") {
        const recreated = createSceneRenderer(canvas, props, capability);
        const nextRenderer = recreated && recreated.renderer;
        if (nextRenderer && nextRenderer.kind === "webgpu" && nextRenderer !== renderer) {
          if (swapRenderer(nextRenderer, reason || "webgpu-render-stall")) {
            renderLatestSceneBundle(reason || "webgpu-render-stall");
            scheduleRenderWithViewport(reason || "webgpu-render-stall");
            return true;
          }
        } else if (nextRenderer && nextRenderer !== renderer && typeof nextRenderer.dispose === "function") {
          nextRenderer.dispose();
        }
	      }
	      renderWatchdogFallbacks += 1;
	      publishSceneRenderWatchdogState(renderWatchdogActiveReason, stalledFor || 0);
	      if (fallbackSceneRenderer(reason || "webgpu-render-stall")) {
        renderLatestSceneBundle(reason || "webgpu-render-stall");
        scheduleRenderWithViewport(reason || "webgpu-render-stall");
        return true;
      }
      scheduleRenderWithViewport(reason || "webgpu-render-stall");
      return false;
    }

    function handleSceneWebGPUProbeReady() {
      if (disposed || !renderer || renderer.kind !== "webgpu") {
        return;
      }
      const diagnostics = typeof renderer.diagnostics === "function" ? renderer.diagnostics() : null;
      const reason = rendererReportsWebGPUFailure(diagnostics);
      if (!reason) {
        return;
      }
      recoverSceneWebGPURenderer("webgpu-probe-recovered", 0, false);
    }

    if (typeof window !== "undefined" && typeof window.addEventListener === "function") {
      webgpuProbeReadyListener = handleSceneWebGPUProbeReady;
      window.addEventListener("gosx:scene3d:webgpu-probe-ready", webgpuProbeReadyListener);
    }

    function checkSceneRenderWatchdog() {
      if (disposed || !ctx.mount || !renderer || renderer.kind !== "webgpu") {
        return;
      }
      const animation = sceneAnimationState();
      if (!animation.wants || !sceneCanRender()) {
        renderWatchdogLastSeq = -1;
        renderWatchdogLastAt = 0;
        renderWatchdogLastAdvanceAt = 0;
        publishSceneRenderWatchdogState("", 0);
        return;
      }
      const now = typeof performance !== "undefined" && typeof performance.now === "function" ? performance.now() : Date.now();
      const progress = readSceneWebGPUProgress();
      const diagnostics = typeof renderer.diagnostics === "function" ? renderer.diagnostics() : null;
      const failureReason = rendererReportsWebGPUFailure(diagnostics);
      if (failureReason) {
        recoverSceneWebGPURenderer(failureReason, 0, true);
        return;
      }
      if (progress.seq > renderWatchdogLastSeq || progress.at > renderWatchdogLastAt) {
        renderWatchdogLastSeq = progress.seq;
        renderWatchdogLastAt = progress.at;
        renderWatchdogLastAdvanceAt = now;
        renderWatchdogActiveReason = "";
        publishSceneRenderWatchdogState("", 0);
        return;
      }
      if (renderWatchdogLastAdvanceAt <= 0) {
        renderWatchdogLastAdvanceAt = now;
        renderWatchdogLastSeq = progress.seq;
        renderWatchdogLastAt = progress.at;
        return;
      }
      const stalledFor = now - renderWatchdogLastAdvanceAt;
      if (stalledFor < SCENE_RENDER_STALL_MS) {
        return;
      }
      const reason = progress.seq > 0 || progress.at > 0 ? "webgpu-render-stall" : "webgpu-render-not-started";
      const forceFallback = stalledFor >= SCENE_RENDER_FALLBACK_STALL_MS;
      if (recoverSceneWebGPURenderer(reason, stalledFor, forceFallback)) {
        renderWatchdogLastAdvanceAt = now;
      }
    }

    function startSceneRenderWatchdog() {
      if (renderWatchdogTimer != null || typeof setInterval !== "function") {
        return;
      }
      const now = typeof performance !== "undefined" && typeof performance.now === "function" ? performance.now() : Date.now();
      const progress = readSceneWebGPUProgress();
      renderWatchdogLastSeq = progress.seq;
      renderWatchdogLastAt = progress.at;
      renderWatchdogLastAdvanceAt = now;
      publishSceneRenderWatchdogState("", 0);
      renderWatchdogTimer = setInterval(checkSceneRenderWatchdog, SCENE_RENDER_WATCHDOG_INTERVAL_MS);
    }

    function stopSceneRenderWatchdog() {
      if (renderWatchdogTimer != null) {
        clearInterval(renderWatchdogTimer);
        renderWatchdogTimer = null;
      }
    }

    function scheduleIdleContextRelease() {
      clearIdleContextRelease();
      if (disposed || contextVoluntarilyLost) return;
    }

    function clearIdleContextRelease() {
      if (idleContextTimer != null) {
        clearTimeout(idleContextTimer);
        idleContextTimer = null;
      }
    }

    // Watchdog for voluntary-restore: Chrome does NOT always fire
    // `webglcontextrestored` after a voluntary `ext.restoreContext()` call,
    // particularly when the tab was foregrounded but the scene was briefly
    // off-viewport when the idle timer fired. The restore event never lands,
    // the lost stub stays installed, and the canvas is permanently black
    // until navigation. This watchdog force-invokes the restore path if the
    // browser event hasn't fired within WEBGL_VOLUNTARY_RESTORE_WATCHDOG_MS.
    const WEBGL_VOLUNTARY_RESTORE_WATCHDOG_MS = 2000;
    let voluntaryRestoreWatchdogTimer = null;
    let voluntaryRestorePending = false;

    function clearVoluntaryRestoreWatchdog() {
      if (voluntaryRestoreWatchdogTimer != null) {
        clearTimeout(voluntaryRestoreWatchdogTimer);
        voluntaryRestoreWatchdogTimer = null;
      }
      voluntaryRestorePending = false;
    }

    function restoreVoluntarilyLostContext() {
      if (!contextVoluntarilyLost) return;
      contextVoluntarilyLost = false;
      voluntaryRestorePending = true;
      let requested = false;
      let restoreExt = voluntaryLoseContextExtension;
      voluntaryLoseContextExtension = null;
      try {
        if (!restoreExt) {
          const gl = canvas.getContext("webgl2") || canvas.getContext("webgl");
          restoreExt = gl && typeof gl.getExtension === "function"
            ? gl.getExtension("WEBGL_lose_context")
            : null;
        }
        if (restoreExt && typeof restoreExt.restoreContext === "function") {
          restoreExt.restoreContext();
          requested = true;
        }
      } catch (_e) { /* let the browser handle it */ }
      gosxSceneEmit("info", "webgl-voluntary-restore-requested", {
        requested: requested,
      });
      if (voluntaryRestoreWatchdogTimer != null) {
        clearTimeout(voluntaryRestoreWatchdogTimer);
      }
      voluntaryRestoreWatchdogTimer = setTimeout(function () {
        voluntaryRestoreWatchdogTimer = null;
        if (!voluntaryRestorePending || disposed) {
          return;
        }
        voluntaryRestorePending = false;
        gosxSceneEmit("warn", "webgl-voluntary-restore-watchdog", {
          rendererKind: renderer && renderer.kind ? renderer.kind : "",
          forcing: true,
        });
        if (!renderer || renderer.kind === "webgl") {
          // Either the event already fired and wired things up, or we lost
          // the mount — either way, nothing to force.
          return;
        }
        // Event didn't land. Force the restore path directly. Mirrors
        // onWebGLContextRestored without touching contextVoluntarilyLost
        // (already cleared above).
        const swapped = restoreSceneWebGLRenderer("webgl-voluntary-restore-forced");
        if (swapped) {
          viewportDirty = true;
          scheduleRender("webgl-voluntary-restore-forced");
        }
        gosxSceneEmit(swapped ? "info" : "error", "webgl-voluntary-restore-forced", {
          swapped: swapped,
        });
      }, WEBGL_VOLUNTARY_RESTORE_WATCHDOG_MS);
    }

    // Viewport-dirty flag: when false, renderFrame skips the per-frame
    // sceneViewportFromMount + applySceneViewport calls and reuses the
    // cached `viewport` object. Both helpers are layout-flushing — they
    // call mount/canvas.getBoundingClientRect(), forcing the browser to
    // recompute layout synchronously. Doing that every frame burns 1-3 ms
    // on a busy page where no DOM has actually changed. The dirty flag
    // is set to true on:
    //   - initial mount (first frame must measure)
    //   - ResizeObserver fire (canvas or mount size changed)
    //   - window resize fallback
    //   - environment / capability change (DPR update)
    //   - lifecycle / motion observer refresh (safer to re-measure)
    //   - visibility transitions
    // Scroll events do NOT mark the viewport dirty — scrolling doesn't
    // change a fixed-positioned canvas's rect, and non-fixed scenes
    // also don't care about scroll-time position unless the consumer
    // explicitly schedules a refresh.
    let viewportDirty = true;
    let lastAnimationFrameAt = 0;

    function sceneAnimationFrameIntervalMS() {
      var interval = sceneNumber(props && props.frameIntervalMS, 0);
      if (!(interval > 0)) {
        var fps = sceneNumber(props && props.maxFrameRate, 0);
        if (!(fps > 0)) {
          fps = sceneNumber(props && props.maxFPS, 0);
        }
        if (fps > 0) {
          interval = 1000 / Math.min(240, Math.max(1, fps));
        }
      }
      return interval > 0 ? Math.max(1, interval) : 0;
    }

    // Guarded animation-chain scheduler. Eager refreshes may still render
    // promptly; the continuous chain stays single-owner and honors maxFrameRate.
    function scheduleNextAnimationFrame() {
      if (disposed) return;
      if (frameHandle != null) return;
      const animation = sceneAnimationState();
      if (!animation.wants || !sceneCanRender()) {
        applySceneRenderLoopState(animation.reason);
        return;
      }
      frameHandle = engineFrame(function(now) {
        frameHandle = null;
        var interval = sceneAnimationFrameIntervalMS();
        if (interval > 0 && lastAnimationFrameAt > 0 && typeof now === "number" && now - lastAnimationFrameAt < interval - 0.75) {
          scheduleNextAnimationFrame();
          return;
        }
        if (typeof now === "number") {
          lastAnimationFrameAt = now;
        }
        renderFrame(now);
      });
      applySceneRenderLoopState(animation.reason);
    }

	    let sceneRendererRecentlySwapped = false;
	    let sceneRendererLastSwapReason = "";
	    let sceneControlHandle = null;
	    let dragHandle = null;
	    let pickHandle = null;
	    let latestScenePickDetail = null;

	    function swapRenderer(nextRenderer, fallbackReason) {
	      if (!nextRenderer) {
	        return false;
	      }
      const previous = renderer;
      renderer = nextRenderer;
      applySceneRendererState(ctx.mount, renderer, fallbackReason);
      publishSceneWaterRendererState(ctx.mount, sceneState, renderer, "");
      renderWatchdogLastSeq = -1;
      renderWatchdogLastAt = 0;
      renderWatchdogLastAdvanceAt = typeof performance !== "undefined" && typeof performance.now === "function" ? performance.now() : Date.now();
      if (previous && previous !== renderer && typeof previous.dispose === "function") {
        previous.dispose();
      }
      sceneRendererRecentlySwapped = true;
      sceneRendererLastSwapReason = fallbackReason || "";
      gosxSceneEmit("info", "renderer-swap", {
        from: previous && previous.kind ? previous.kind : "",
        to: nextRenderer.kind || "",
        reason: fallbackReason || "",
	      });
	      return true;
	    }

	    function detachSceneCanvasContextListeners(target) {
	      if (!target || typeof target.removeEventListener !== "function") {
	        return;
	      }
	      target.removeEventListener("webglcontextlost", onWebGLContextLost);
	      target.removeEventListener("webglcontextrestored", onWebGLContextRestored);
	    }

	    function attachSceneCanvasContextListeners(target) {
	      if (!target || typeof target.addEventListener !== "function") {
	        return;
	      }
	      target.addEventListener("webglcontextlost", onWebGLContextLost);
	      target.addEventListener("webglcontextrestored", onWebGLContextRestored);
	    }

	    function prepareSceneReplacementCanvas() {
	      const nextCanvas = createSceneMountCanvas();
	      nextCanvas.width = canvas && canvas.width ? canvas.width : viewportBase.baseWidth;
	      nextCanvas.height = canvas && canvas.height ? canvas.height : viewportBase.baseHeight;
	      nextCanvas.setAttribute("width", String(nextCanvas.width));
	      nextCanvas.setAttribute("height", String(nextCanvas.height));
	      if (canvas && canvas.style) {
	        nextCanvas.style.width = canvas.style.width || "";
	        nextCanvas.style.height = canvas.style.height || "";
	        nextCanvas.style.maxWidth = canvas.style.maxWidth || nextCanvas.style.maxWidth;
	        nextCanvas.style.borderRadius = canvas.style.borderRadius || nextCanvas.style.borderRadius;
	      }
	      return nextCanvas;
	    }

	    function commitSceneCanvasReplacement(nextCanvas, reason) {
	      if (!nextCanvas || nextCanvas === canvas || !ctx.mount) {
	        return false;
	      }
	      const previousCanvas = canvas;
	      detachSceneCanvasContextListeners(previousCanvas);
	      if (sentinelLayer && sentinelLayer.parentNode === previousCanvas) {
	        nextCanvas.appendChild(sentinelLayer);
	      }
	      if (previousCanvas && previousCanvas.parentNode === ctx.mount) {
	        ctx.mount.insertBefore(nextCanvas, previousCanvas);
	        ctx.mount.removeChild(previousCanvas);
	      } else if (labelLayer && labelLayer.parentNode === ctx.mount) {
	        ctx.mount.insertBefore(nextCanvas, labelLayer);
	      } else {
	        ctx.mount.appendChild(nextCanvas);
	      }
	      canvas = nextCanvas;
	      attachSceneCanvasContextListeners(canvas);
	      viewportDirty = true;
	      reinstallSceneCanvasInteractionHandles(reason || "canvas-replaced");
	      gosxSceneEmit("info", "renderer-canvas-replaced", {
	        reason: reason || "",
	      });
	      return true;
	    }

	    function sceneFallbackRequiresReplacementCanvas(reason) {
	      return reason === "webgpu-device-lost";
	    }

	    function createFallbackSceneWebGLRenderer(reason) {
	      const useReplacementCanvas = sceneFallbackRequiresReplacementCanvas(reason);
	      if (!useReplacementCanvas) {
	        const currentResult = createSceneWebGLResult(canvas, props, capability, reason || "webgl-fallback");
	        if (currentResult && currentResult.renderer) {
	          return {
	            canvas: canvas,
	            result: currentResult,
	          };
	        }
	      }
	      const nextCanvas = prepareSceneReplacementCanvas();
	      const result = createSceneWebGLResult(nextCanvas, props, capability, reason || "webgl-fallback");
	      if (!result || !result.renderer) {
	        return null;
	      }
	      return {
	        canvas: nextCanvas,
	        result: result,
	      };
	    }

	    function getFallbackCanvas2D(reason) {
	      const useReplacementCanvas = sceneFallbackRequiresReplacementCanvas(reason);
	      if (!useReplacementCanvas) {
	        const current2d = typeof canvas.getContext === "function" ? canvas.getContext("2d") : null;
	        if (current2d) {
	          return {
	            canvas: canvas,
	            ctx2d: current2d,
	          };
	        }
	      }
	      const nextCanvas = prepareSceneReplacementCanvas();
	      const ctx2d = typeof nextCanvas.getContext === "function" ? nextCanvas.getContext("2d") : null;
	      if (!ctx2d) {
	        gosxSceneEmit("warn", "renderer-fallback-unavailable", { reason: reason || "" });
	        return null;
	      }
	      return {
	        canvas: nextCanvas,
	        ctx2d: ctx2d,
	      };
	    }

            function fallbackSceneRenderer(reason) {
              const fallbackReason = reason || "webgl-unavailable";
              const backendCaps = sceneBackendCapsOf(props);
              const allowWebGLFallback = sceneBackendCapsAllowsKind(backendCaps, "webgl");
              const allowCanvasFallback = sceneBackendCapsAllowsKind(backendCaps, "canvas2d");
              const preferCanvasFallback = fallbackReason === "environment-constrained" || fallbackReason === "webgl-context-lost";
              if (!preferCanvasFallback && allowWebGLFallback) {
                const webglFallback = createFallbackSceneWebGLRenderer(fallbackReason);
                if (webglFallback && webglFallback.result && webglFallback.result.renderer) {
                  if (webglFallback.canvas !== canvas) {
                    commitSceneCanvasReplacement(webglFallback.canvas, fallbackReason);
                  }
                  return swapRenderer(webglFallback.result.renderer, fallbackReason);
                }
              }
              if (sceneFirstWaterEntry(props)) {
                // Canvas2D and generic WebGL cannot represent the water
                // simulation. Expose the backend failure instead of swapping
                // to a renderer that would produce a plausible-but-blank demo.
                const waterReason = "water-webgl2-unavailable";
                applySceneRendererState(ctx.mount, renderer, waterReason);
                publishSceneWaterRendererState(ctx.mount, sceneState, null, waterReason);
                gosxSceneEmit("warn", "water-renderer-fallback-unavailable", {
                  reason: fallbackReason,
                });
                return false;
              }
              if (!allowWebGLFallback && !allowCanvasFallback) {
                gosxSceneEmit("warn", "renderer-fallback-disallowed", {
                  reason: fallbackReason,
                  capable: backendCaps && Array.isArray(backendCaps.capable) ? backendCaps.capable.slice() : [],
                });
                applySceneRendererState(ctx.mount, renderer, fallbackReason || "no-capable-backend");
                return false;
              }
              if (sceneRequiresWebGL(props)) {
                gosxSceneEmit("warn", "renderer-fallback-disabled", { reason: reason || "" });
                applySceneRendererState(ctx.mount, renderer, reason || "webgl-required");
                return false;
              }
              if (!allowCanvasFallback) {
                gosxSceneEmit("warn", "renderer-canvas-fallback-disallowed", {
                  reason: fallbackReason,
                  capable: backendCaps && Array.isArray(backendCaps.capable) ? backendCaps.capable.slice() : [],
                });
                applySceneRendererState(ctx.mount, renderer, fallbackReason || "no-capable-backend");
                return false;
              }
              const canvas2dFallback = getFallbackCanvas2D(fallbackReason);
              if (!canvas2dFallback) {
                return false;
              }
              if (canvas2dFallback.canvas !== canvas) {
                commitSceneCanvasReplacement(canvas2dFallback.canvas, fallbackReason);
              }
              return swapRenderer(createSceneCanvasRenderer(canvas2dFallback.ctx2d, canvas2dFallback.canvas), fallbackReason);
            }

    function ensureRendererCanCoverBundle(bundle) {
      if (!renderer || !bundle) {
        return true;
      }
      let feature = "";
      if (typeof renderer.supportsBundle === "function" && renderer.supportsBundle(bundle) === false) {
        feature = "backend-declared";
      }
      if (!feature) {
        return true;
      }
      gosxSceneEmit("warn", "renderer-feature-gap", {
        rendererKind: renderer.kind || "",
        feature,
      });
      return fallbackSceneRenderer("webgpu-feature-gap");
    }

    function renderLatestSceneBundle(reason) {
      if (disposed || !latestBundle || !renderer || typeof renderer.render !== "function" || !sceneCanRender()) {
        return false;
      }
      if (!ensureRendererCanCoverBundle(latestBundle)) {
        return false;
      }
      recordScenePerfCounter("render:" + (reason || "restore"));
      syncSceneNodeSentinels(latestBundle);
      renderer.render(latestBundle, viewport);
      recordSceneWaterFrame(ctx.mount, latestBundle);
      emitRendererWarmup(reason, latestBundle);
      maybeEmitRenderEmpty(latestBundle);
      renderSceneLabels(labelLayer, latestBundle, labelLayoutCache, labelElements, viewport.cssWidth, viewport.cssHeight);
      renderSceneSprites(labelLayer, latestBundle, spriteElements, viewport.cssWidth, viewport.cssHeight);
      renderSceneHTML(labelLayer, latestBundle, htmlElements, viewport.cssWidth, viewport.cssHeight, htmlTextureState);
      return true;
    }

    // emitRendererWarmup: called once per renderer-swap, after the first
    // render on the new renderer. Reports the bundle inventory the fresh
    // renderer just had to deal with so a silent post-restore black canvas
    // can be narrowed down to a specific resource class (PBR mesh count,
    // instanced mesh count, lights, post-fx, etc.) in the server slog.
    function emitRendererWarmup(reason, bundle) {
      gosxSceneEmit("info", "renderer-warmup", {
        rendererKind: renderer && renderer.kind ? renderer.kind : "",
        reason: reason || "",
        bundleMeshObjects: Array.isArray(bundle && bundle.meshObjects) ? bundle.meshObjects.length : 0,
        bundleInstancedMeshes: Array.isArray(bundle && bundle.instancedMeshes) ? bundle.instancedMeshes.length : 0,
        bundlePoints: Array.isArray(bundle && bundle.points) ? bundle.points.length : 0,
        bundleLights: Array.isArray(bundle && bundle.lights) ? bundle.lights.length : 0,
        bundleLabels: Array.isArray(bundle && bundle.labels) ? bundle.labels.length : 0,
        bundleSprites: Array.isArray(bundle && bundle.sprites) ? bundle.sprites.length : 0,
        bundleSurfaces: Array.isArray(bundle && bundle.surfaces) ? bundle.surfaces.length : 0,
        bundleComputeParticles: Array.isArray(bundle && bundle.computeParticles) ? bundle.computeParticles.length : 0,
        bundleWorldVertexCount: Number((bundle && bundle.worldVertexCount) || 0),
        bundleVertexCount: Number((bundle && bundle.vertexCount) || 0),
        bundleHasPostFX: Boolean(bundle && bundle.postEffects && Object.keys(bundle.postEffects).length > 0),
      });
    }

    function restoreSceneWebGLRenderer(reason) {
      const webglPreference = sceneCapabilityWebGLPreference(props, capability);
      if (!(webglPreference === "prefer" || webglPreference === "force")
          || !sceneBackendCapsAllowsKind(sceneBackendCapsOf(props), "webgl")) {
        return false;
      }
      const restoredRenderer = createSceneRenderer(canvas, props, capability);
      const webglRenderer = restoredRenderer && restoredRenderer.renderer;
      if (!webglRenderer || webglRenderer.kind !== "webgl") {
        if (webglRenderer && typeof webglRenderer.dispose === "function") {
          webglRenderer.dispose();
        }
        return false;
      }
      if (!swapRenderer(webglRenderer, reason || restoredRenderer.fallbackReason || "")) {
        return false;
      }
      renderLatestSceneBundle(reason || "webgl-restore");
      return true;
    }

    // Renderer stub used between context-lost and context-restored. Any
    // scheduleRender / rAF callbacks queued before the loss keep calling
    // `renderer.render(...)` — if that still points at the old WebGL
    // renderer, its cached program/buffer handles become stale the instant
    // the browser restores the context (same `gl` object, but all resources
    // invalidated), and every call raises GL_INVALID_OPERATION (1282),
    // silently blacking the canvas. Swapping in this stub before the fallback
    // runs means those queued callbacks harmlessly no-op instead.
    const sceneRendererLostStub = {
      kind: "lost",
      render: function () {},
      dispose: function () {},
    };

    function onWebGLContextLost(event) {
      if (!renderer || renderer.kind !== "webgl") {
        return;
      }
      if (event && typeof event.preventDefault === "function") {
        event.preventDefault();
      }
      gosxSceneEmit("warn", "webgl-context-lost", {
        voluntary: contextVoluntarilyLost === true,
      });
      // Dispose the live WebGL renderer immediately so its closures release
      // every handle (programs, FBOs, cascade textures, IBL cubemaps,
      // post-fx pipeline) before the browser can re-attach a fresh context
      // to the same canvas. Bypass swapRenderer/fallbackSceneRenderer's
      // telemetry so we don't emit a spurious renderer-swap to the stub.
      try {
        if (typeof renderer.dispose === "function") {
          renderer.dispose();
        }
      } catch (_err) {
        /* dispose errors on a lost context are expected */
      }
      renderer = sceneRendererLostStub;
      applySceneRendererState(ctx.mount, renderer, "webgl-context-lost");
      const swapped = fallbackSceneRenderer("webgl-context-lost");
      scheduleRender("webgl-context-lost");
      if (!swapped) {
        gosxSceneEmit("warn", "webgl-context-lost-no-fallback", {});
      }
    }

    function onWebGLContextRestored() {
      const voluntary = contextVoluntarilyLost === true;
      const watchdogPending = voluntaryRestorePending === true;
      contextVoluntarilyLost = false;
      // Natural event landed — cancel any outstanding voluntary-restore
      // watchdog so we don't force-restore on top of the browser's own work.
      clearVoluntaryRestoreWatchdog();
      const swapped = restoreSceneWebGLRenderer("");
      gosxSceneEmit(swapped ? "info" : "warn", "webgl-context-restored", {
        swapped: swapped,
        voluntary: voluntary,
        watchdogPending: watchdogPending,
      });
      if (swapped) {
        viewportDirty = true;
        scheduleRender("webgl-context-restored");
      }
    }

	    attachSceneCanvasContextListeners(canvas);
    startSceneRenderWatchdog();

    function sceneCanRender() {
      return lifecycle.pageVisible && lifecycle.inViewport;
    }

    function sceneWantsAnimation() {
      return sceneShouldAnimate() && sceneCanRender();
    }

    function cancelFrame() {
      if (frameHandle != null) {
        cancelEngineFrame(frameHandle);
        frameHandle = null;
      }
      applySceneRenderLoopState("");
    }

    function cancelScheduledRender() {
      if (scheduledRenderHandle != null) {
        cancelEngineFrame(scheduledRenderHandle);
        scheduledRenderHandle = null;
      }
      applySceneRenderLoopState("");
    }

    function recordScenePerfCounter(name) {
      if (!(typeof window !== "undefined" && window.__gosx_scene3d_perf === true)) {
        return;
      }
      const key = String(name || "unknown");
      const counters = ctx.mount.__gosxScene3DScheduleCounts || Object.create(null);
      counters[key] = (counters[key] || 0) + 1;
      ctx.mount.__gosxScene3DScheduleCounts = counters;
    }

    function scheduleRender(reason) {
      if (disposed) {
        return;
      }
      lastScheduledRenderReason = reason || "refresh";
      recordScenePerfCounter("schedule:" + (reason || "refresh"));
      if (scheduledRenderHandle != null) {
        recordScenePerfCounter("coalesced:" + (reason || "refresh"));
        applySceneRenderLoopState(lastScheduledRenderReason);
        return;
      }
      // Defer the viewport read+write into the RAF callback. The old
      // code called sceneViewportFromMount / applySceneViewport
      // synchronously, which meant every scroll event forced two
      // layout flushes (one read pair before the write, one read pair
      // after, because applySceneViewport both mutates canvas size and
      // reads bounding rects for the label layer). Firefox coalesces
      // scroll events at 30Hz during active touch-scroll, so the
      // flushes stacked up and the browser had to reflow mid-scroll —
      // visible as jank and a frame of stale canvas content after the
      // scroll stopped. iOS Safari has the same symptom.
      //
      // Inside RAF the browser is already in a read phase (style+
      // layout has been resolved), so rect reads are cheap and the
      // subsequent canvas writes batch naturally into the following
      // compositor pass.
      scheduledRenderHandle = engineFrame(function(now) {
        scheduledRenderHandle = null;
        if (disposed) {
          return;
        }
        if (!sceneCanRender()) {
          cancelFrame();
          applySceneRenderLoopState("");
          return;
        }
        // Keep eager refreshes from overlapping an in-flight animation tick.
        cancelFrame();
        lastAnimationFrameAt = typeof now === "number" ? now : 0;
        renderFrame(typeof now === "number" ? now : 0, reason || "refresh");
      });
      applySceneRenderLoopState(lastScheduledRenderReason);
    }

    // Wraps scheduleRender so the caller can opt into marking the
    // viewport dirty. Used by the observers whose triggers imply a
    // physical viewport change (resize, visibility, capability /
    // environment, motion). Other scheduleRender callers (live
    // events, hub events, controls) don't need to force re-measurement
    // and should call scheduleRender directly.
    function scheduleRenderWithViewport(reason) {
      viewportDirty = true;
      scheduleRender(reason);
    }

    function markSceneCSSInvalidated(reason) {
      const revision = Number(ctx.mount && ctx.mount.__gosxScene3DCSSRevision);
      ctx.mount.__gosxScene3DCSSRevision = Number.isFinite(revision) ? revision + 1 : 1;
      const transitionWindow = Math.max(
        sceneCSSTransitionWindowMillis(ctx.mount),
        sceneCSSTransitionWindowMillis(document && document.documentElement)
      );
      if (transitionWindow > 0) {
        sceneCSSAnimationUntil = Date.now() + transitionWindow;
      }
      ctx.mount.__gosxScene3DCSSAnimationUntil = sceneCSSAnimationUntil;
      scheduleRender(reason || "css");
    }

    function sceneCSSTransitionWindowMillis(element) {
      if (!element || typeof window.getComputedStyle !== "function") {
        return 0;
      }
      let style = null;
      try {
        style = window.getComputedStyle(element);
      } catch (_error) {
        style = null;
      }
      if (!style) {
        return 0;
      }
      const durations = sceneCSSParseTimeList(style.transitionDuration || (typeof style.getPropertyValue === "function" ? style.getPropertyValue("transition-duration") : ""));
      const delays = sceneCSSParseTimeList(style.transitionDelay || (typeof style.getPropertyValue === "function" ? style.getPropertyValue("transition-delay") : ""));
      const length = Math.max(durations.length, delays.length);
      let max = 0;
      for (let index = 0; index < length; index += 1) {
        const duration = durations[index % Math.max(1, durations.length)] || 0;
        const delay = delays[index % Math.max(1, delays.length)] || 0;
        max = Math.max(max, duration + delay);
      }
      return max > 0 ? max + 80 : 0;
    }

    function sceneCSSParseTimeList(value) {
      return String(value || "").split(",").map(function(part) {
        const text = part.trim().toLowerCase();
        if (!text) {
          return 0;
        }
        const number = Number.parseFloat(text);
        if (!Number.isFinite(number)) {
          return 0;
        }
        return text.endsWith("ms") ? number : number * 1000;
      });
    }

    function sceneCSSExternalStyleSignatureFromText(value) {
      const items = [];
      const parts = String(value || "").split(";");
      for (let index = 0; index < parts.length; index += 1) {
        const part = parts[index];
        const colon = part.indexOf(":");
        if (colon < 0) {
          continue;
        }
        const name = part.slice(0, colon).trim();
        if (!name || name.indexOf("--gosx-") === 0) {
          continue;
        }
        items.push(name + ":" + part.slice(colon + 1).trim());
      }
      items.sort();
      return items.join(";");
    }

    function sceneCSSExternalStyleSignature(element) {
      const style = element && element.style;
      if (!style) {
        return "";
      }
      const items = [];
      if (typeof style.length === "number" && typeof style.getPropertyValue === "function") {
        for (let index = 0; index < style.length; index += 1) {
          const name = style[index];
          if (!name || String(name).indexOf("--gosx-") === 0) {
            continue;
          }
          items.push(String(name) + ":" + String(style.getPropertyValue(name) || "").trim());
        }
        items.sort();
        return items.join(";");
      }
      return sceneCSSExternalStyleSignatureFromText(style.cssText || "");
    }

    function sceneCSSMutationShouldInvalidate(records) {
      let sawRecord = false;
      for (let index = 0; index < (records || []).length; index += 1) {
        const record = records[index];
        if (!record || record.type !== "attributes") {
          continue;
        }
        sawRecord = true;
        const attributeName = String(record.attributeName || "");
        if (attributeName === "class") {
          return true;
        }
        if (attributeName !== "style") {
          return true;
        }
        const previous = sceneCSSExternalStyleSignatureFromText(record.oldValue || "");
        const current = sceneCSSExternalStyleSignature(record.target);
        if (previous !== current) {
          return true;
        }
      }
      return !sawRecord;
    }

    function observeSceneCSSInvalidation() {
      const releases = [];
      if (typeof MutationObserver === "function") {
        const observer = new MutationObserver(function(records) {
          if (!sceneCSSMutationShouldInvalidate(records)) {
            return;
          }
          markSceneCSSInvalidated("css");
        });
        observer.observe(ctx.mount, {
          attributes: true,
          attributeFilter: ["class", "style"],
          attributeOldValue: true,
          subtree: false,
        });
        if (document && document.documentElement && document.documentElement !== ctx.mount) {
          observer.observe(document.documentElement, {
            attributes: true,
            attributeFilter: ["class", "style"],
            attributeOldValue: true,
            subtree: false,
          });
        }
        releases.push(function() { observer.disconnect(); });
      }
      if (typeof window.matchMedia === "function") {
        const queries = [
          "(prefers-color-scheme: dark)",
          "(prefers-reduced-motion: reduce)",
          "(prefers-contrast: more)",
          "(prefers-reduced-data: reduce)",
        ];
        for (let index = 0; index < queries.length; index += 1) {
          const query = window.matchMedia(queries[index]);
          const listener = function() {
            markSceneCSSInvalidated("css-media");
          };
          if (query && typeof query.addEventListener === "function") {
            query.addEventListener("change", listener);
            releases.push(function(q, l) {
              return function() { q.removeEventListener("change", l); };
            }(query, listener));
          } else if (query && typeof query.addListener === "function") {
            query.addListener(listener);
            releases.push(function(q, l) {
              return function() { q.removeListener(l); };
            }(query, listener));
          }
        }
      }
      return function releaseSceneCSSInvalidation() {
        for (let index = 0; index < releases.length; index += 1) {
          releases[index]();
        }
      };
    }

    function readSceneSourceCamera() {
      if (latestBundle && latestBundle.sourceCamera) {
        return latestBundle.sourceCamera;
      }
      if (latestBundle && latestBundle.camera) {
        return latestBundle.camera;
      }
      return sceneState.camera;
    }

	    function disposeSceneCanvasInteractionHandles() {
	      if (dragHandle && typeof dragHandle.dispose === "function") {
	        dragHandle.dispose();
	      }
	      if (pickHandle && typeof pickHandle.dispose === "function") {
	        pickHandle.dispose();
	      }
	      if (sceneControlHandle && typeof sceneControlHandle.dispose === "function") {
	        sceneControlHandle.dispose();
	      }
	      sceneControlHandle = null;
	      dragHandle = null;
	      pickHandle = null;
	    }

	    function installSceneCanvasInteractionHandles() {
	      sceneControlHandle = setupSceneBuiltInControls(canvas, props, function() {
	        return viewport;
	      }, readSceneSourceCamera, scheduleRender);
	      dragHandle = sceneControlHandle.controller
	        ? { dispose() {} }
	        : setupSceneDragInteractions(canvas, props, function() {
	          return viewport;
	        }, function() {
	          return latestBundle;
	        });
	      pickHandle = setupScenePickInteractions(canvas, props, function() {
	        return viewport;
	      }, function() {
	        return latestBundle;
	      }, function(detail) {
	        latestScenePickDetail = detail ? sceneDebugClone(detail, 4) : null;
	        dispatchSceneHTMLTexturePointer(latestBundle, htmlElements, detail);
	        ctx.emit("scene-interaction", detail);
	      });
	    }

	    function reinstallSceneCanvasInteractionHandles(reason) {
	      disposeSceneCanvasInteractionHandles();
	      installSceneCanvasInteractionHandles();
	      if (reason) {
	        gosxSceneEmit("info", "scene-canvas-interactions-rebound", {
	          reason: reason || "",
	        });
	      }
	    }

	    installSceneCanvasInteractionHandles();

	    let lastPublishedCamera = null;
    let lastAppliedSelectionID = null;
    let applyingSignalCamera = false;
    let applyingSignalSelection = false;
    let lastAppliedGizmoMode = null;
    let applyingSignalGizmoMode = false;

    // syncMountedSceneGizmoHelpers is the shared live-update pass for
    // TransformControls helper meshes (Mesh.GizmoHelper / gizmoHelper:true;
    // see scene.go's lowerTransformControls). Re-run after either the
    // selection signal or the gizmo-mode signal changes, so the two signals
    // together drive every GizmoHelper mesh: hidden while nothing is
    // selected, repositioned onto the selected object's live world
    // transform otherwise, and — of the three baked forms (translate axes
    // triad / rotate ring / scale handle cubes) — only the one whose
    // gizmoFormMode matches the active mode signal shown. No page
    // navigation / SSR round-trip needed for any of it.
    //
    // Also preserves the legacy selection-independent mode-only toggle (P6)
    // for any plain Mesh.GizmoRing=true object that doesn't opt into the
    // full gizmoHelper group.
    function syncMountedSceneGizmoHelpers() {
      const objects = sceneStateObjects(sceneState);
      const mode = lastAppliedGizmoMode || "translate";
      const selID = lastAppliedSelectionID || "";
      let target = null;
      if (selID) {
        for (let i = 0; i < objects.length; i++) {
          if (objects[i].id === selID) {
            target = objects[i];
            break;
          }
        }
      }
      const anchor = target ? sceneGizmoTargetAnchor(target) : null;
      for (let i = 0; i < objects.length; i++) {
        const obj = objects[i];
        if (obj.gizmoHelper) {
          const visible = Boolean(target) && obj.gizmoFormMode === mode;
          const patch = { visible: visible };
          if (anchor) {
            patch.x = anchor.x;
            patch.y = anchor.y;
            patch.z = anchor.z;
            patch.rotationX = anchor.rotationX;
            patch.rotationY = anchor.rotationY;
            patch.rotationZ = anchor.rotationZ;
          }
          applySceneObjectPatch(sceneState, obj.id, patch);
        } else if (obj.gizmoRing) {
          applySceneObjectPatch(sceneState, obj.id, { visible: mode === "rotate" });
        }
      }
    }

    function applyMountedSceneSelection(selectedID) {
      const id = typeof selectedID === "string" ? selectedID : "";
      if (id === lastAppliedSelectionID) return;
      applyingSignalSelection = true;
      const objects = sceneStateObjects(sceneState);
      for (let i = 0; i < objects.length; i++) {
        applySceneObjectPatch(sceneState, objects[i].id, { selected: objects[i].id === id });
      }
      lastAppliedSelectionID = id;
      applyingSignalSelection = false;
      syncMountedSceneGizmoHelpers();
      scheduleRender("signal-selection");
    }

    // applyMountedSceneGizmoMode drives the TransformControls gizmo live off
    // Props.GizmoInputSignal, delegating to syncMountedSceneGizmoHelpers
    // (which also accounts for the current selection) for the actual
    // visibility + reposition work. Mirrors applyMountedSceneSelection
    // above: patch already-mounted objects in place, no server round-trip.
    function applyMountedSceneGizmoMode(mode) {
      const nextMode = typeof mode === "string" ? mode : "";
      if (nextMode === lastAppliedGizmoMode) return;
      applyingSignalGizmoMode = true;
      lastAppliedGizmoMode = nextMode;
      syncMountedSceneGizmoHelpers();
      applyingSignalGizmoMode = false;
      scheduleRender("signal-gizmo-mode");
    }

	    function currentMountedSceneCamera(sourceCamera) {
	      return sceneRenderCamera(sceneCurrentControlCamera(
	        sceneControlHandle && sceneControlHandle.controller,
	        sourceCamera || readSceneSourceCamera(),
	        sceneState._scrollCamera,
	      ));
    }

    function currentMountedSceneOrbitState() {
      const controller = sceneControlHandle && sceneControlHandle.controller;
      if (!controller || controller.mode !== "orbit") {
        return null;
      }
      const sourceCamera = readSceneSourceCamera();
      syncSceneControlsFromCamera(controller, sourceCamera);
      const orbit = controller.orbit || sceneOrbitStateFromCamera(sourceCamera, controller.target, controller);
      if (!orbit) {
        return null;
      }
      const target = orbit.target || controller.target || { x: 0, y: 0, z: 0 };
      return {
        target: {
          x: sceneNumber(target.x, 0),
          y: sceneNumber(target.y, 0),
          z: sceneNumber(target.z, 0),
        },
        radius: sceneNumber(orbit.radius, 0),
        yaw: sceneNumber(orbit.yaw, 0),
        pitch: sceneNumber(orbit.pitch, 0),
      };
    }

    function publishMountedSceneCamera(camera, reason) {
      const nextCamera = sceneRenderCamera(camera);
      if (lastPublishedCamera && sceneCameraEquivalent(lastPublishedCamera, nextCamera)) {
        return;
      }
      lastPublishedCamera = nextCamera;
      ctx.emit("scene-camera", {
        camera: nextCamera,
        reason: reason || "render",
      });
      if (typeof props.cameraOutputSignal === "string" && props.cameraOutputSignal && !applyingSignalCamera) {
        queueInputSignal(props.cameraOutputSignal, nextCamera);
      }
    }

    function applyMountedSceneCamera(camera, reason) {
      if (!sceneIsPlainObject(camera)) {
        return false;
      }
	      const currentCamera = currentMountedSceneCamera();
	      const nextCamera = normalizeSceneCamera(camera, currentCamera);
	      if (sceneCameraEquivalent(currentCamera, nextCamera)) {
	        return false;
	      }
	      sceneState.camera = nextCamera;
	      applySceneControlsCamera(sceneControlHandle && sceneControlHandle.controller, nextCamera);
	      scheduleRender(reason || "camera");
	      publishMountedSceneCamera(nextCamera, reason || "camera");
	      return true;
	    }
    function buildSceneDebugSnapshot(mode) {
      const rendererKind = renderer && renderer.kind ? renderer.kind : "";
      const rendererDiagnostics = renderer && typeof renderer.diagnostics === "function" ? renderer.diagnostics() : null;
      const surfaceID = sceneDebugMountID(ctx.mount, ctx.id);
      const counts = sceneDebugBundleCounts(latestBundle, sceneState);
      const features = sceneDebugFeatureMatrix(latestBundle, sceneState, rendererKind);
      const snapshot = {
        schema: SCENE3D_DEBUG_SCHEMA,
        id: surfaceID,
        mountID: ctx.mount && ctx.mount.id ? String(ctx.mount.id) : "",
        engineID: String(ctx.id || ""),
        component: String(ctx.component || ""),
        renderer: rendererKind,
        fallbackReason: sceneDebugAttr(ctx.mount, "data-gosx-scene3d-renderer-fallback"),
        ready: sceneDebugAttr(ctx.mount, "data-gosx-scene3d-ready") === "true",
        active: sceneDebugAttr(ctx.mount, "data-gosx-scene3d-active") !== "false",
        renderLoop: sceneRenderLoopSnapshot(""),
        controls: normalizeSceneControlsMode(props.controls),
        viewport: {
          cssWidth: sceneNumber(viewport && viewport.cssWidth, 0),
          cssHeight: sceneNumber(viewport && viewport.cssHeight, 0),
          devicePixelRatio: sceneNumber(viewport && viewport.devicePixelRatio, 1),
        },
        counts,
        features,
        diagnostics: sceneDebugDiagnostics(ctx.mount, rendererKind, rendererDiagnostics),
        lastPick: latestScenePickDetail || (pickHandle && typeof pickHandle.getSnapshot === "function" ? pickHandle.getSnapshot() : null),
      };
      if (mode !== "summary") {
        snapshot.camera = currentMountedSceneCamera();
        snapshot.gpuResources = sceneDebugGPUResources(ctx.mount, canvas, renderer, latestBundle, viewport, labelLayer, rendererDiagnostics);
        snapshot.webgpuStats = sceneDebugClone(ctx.mount && ctx.mount.__gosxScene3DWebGPUStats, 3);
        snapshot.waterShaderSources = { sceneState: [], bundle: [] };
        snapshot.rendererDiagnostics = sceneDebugClone(rendererDiagnostics, 3);
        snapshot.fighterSamples = sceneDebugFighterSamples(latestBundle, sceneState);
      }
      return snapshot;
    }
    const releaseSceneDebugSurface = sceneDebugRegisterSurface({
      id: sceneDebugMountID(ctx.mount, ctx.id),
      mountID: ctx.mount && ctx.mount.id ? String(ctx.mount.id) : "",
      engineID: String(ctx.id || ""),
      component: String(ctx.component || ""),
      mount: ctx.mount,
      snapshot: buildSceneDebugSnapshot,
      captureFrame() {
        const surfaceID = sceneDebugMountID(ctx.mount, ctx.id);
        if (!canvas || typeof canvas.toDataURL !== "function") {
          return { surfaceID, dataURL: null, reason: "capture-unavailable" };
        }
        return {
          surfaceID,
          mimeType: "image/png",
          dataURL: canvas.toDataURL("image/png"),
        };
      },
    });
    const inspectorEnabled = sceneBool(
      props.inspector,
      typeof window !== "undefined" && window.__gosx_scene3d_inspector === true,
    );
    inspectorOverlay = createSceneInspectorOverlay(ctx.mount, inspectorEnabled, function() {
      return buildSceneDebugSnapshot("full");
    });
    let pendingMotionData = null;
    let pendingMotionHandle = null;
    // De-dupes the "audio" hub event below by AudioCue.Seq (scene/audio.go),
    // mirroring the fight demo's own lastFeedbackSeq check in 30-tail.js's
    // onHubMessage — a cue redelivered on an unchanged seq is a no-op here.
    let lastSceneAudioSeq = 0;

    function applySceneHubEvent(eventName, data, reason) {
      const cameraChanged = sceneApplyCameraLiveEvent(sceneState, data);
      if (cameraChanged) {
        applySceneControlsCamera(sceneControlHandle.controller, sceneState.camera);
      }
      const modelChanged = sceneApplyModelLiveEvent(sceneState, eventName, data);
      const liveChanged = sceneApplyLiveEvent(sceneState, eventName, data, motion.reducedMotion, sceneNowMilliseconds());
      if (cameraChanged || modelChanged || liveChanged) {
        scheduleRender(reason || "hub-event");
      }
    }

    // applySceneAudioCue is the dedicated "audio" hub-event delivery path
    // documented on scene.AudioCue (scene/audio.go): it lets any
    // server-driven scene fire a sample-clip or synth cue independent of
    // the fight-specific hub input controller's own hard-coded tick
    // parsing (createHubInputController.onHubMessage, 30-tail.js), which
    // remains untouched. window.__gosx.audio and window.__gosx.arcadeAudio
    // are looked up dynamically (not imported) because this file is also
    // compiled standalone into bootstrap-feature-scene3d.js, which does not
    // itself carry either engine's source — both are expected to already
    // be installed on window by whatever base runtime bundle loaded first,
    // same as the pre-existing props.audio manifest wiring below.
    function applySceneAudioCue(data) {
      const cue = data && typeof data === "object" ? data : null;
      if (!cue) {
        return;
      }
      const seq = Math.floor(sceneNumber(cue.seq, 0));
      if (seq > 0) {
        if (seq === lastSceneAudioSeq) {
          return;
        }
        lastSceneAudioSeq = seq;
      }
      const clip = typeof cue.clip === "string" ? cue.clip.trim() : "";
      const gosxAudio = window.__gosx && window.__gosx.audio;
      if (clip && gosxAudio && typeof gosxAudio.play === "function") {
        gosxAudio.play(clip, {
          volume: cue.volume,
          rate: cue.rate,
          loop: Boolean(cue.loop),
          bus: cue.bus,
          pan: cue.pan,
          position: cue.position,
          handle: cue.handle,
        });
        return;
      }
      const arcadeAudio = window.__gosx && window.__gosx.arcadeAudio;
      if (!arcadeAudio) {
        return;
      }
      const synthOptions = { intensity: cue.intensity, pan: cue.pan, depth: cue.depth, rate: cue.rate };
      if (cue.patch && typeof cue.patch === "object" && typeof arcadeAudio.playPatch === "function") {
        arcadeAudio.playPatch(cue.patch, synthOptions);
        return;
      }
      const name = typeof cue.cue === "string" ? cue.cue.trim() : "";
      if (name && name !== "none" && typeof arcadeAudio.play === "function") {
        arcadeAudio.play(name, synthOptions);
      }
    }

    function flushPendingMotionEvent() {
      pendingMotionHandle = null;
      if (disposed || !pendingMotionData) {
        pendingMotionData = null;
        return;
      }
      const data = pendingMotionData;
      pendingMotionData = null;
      applySceneHubEvent("motion", data, "hub-motion");
    }

    const sceneHubListener = function(event) {
      if (disposed) {
        return;
      }
      const detail = event && event.detail && typeof event.detail === "object" ? event.detail : null;
      if (!detail || typeof detail.event !== "string") {
        return;
      }
      if (detail.event === "motion") {
        pendingMotionData = detail.data;
        if (pendingMotionHandle == null) {
          pendingMotionHandle = engineFrame(flushPendingMotionEvent);
        }
        return;
      }
      if (detail.event === "audio") {
        applySceneAudioCue(detail.data);
        return;
      }
      applySceneHubEvent(detail.event, detail.data, "hub-event");
    };
    document.addEventListener("gosx:hub:event", sceneHubListener);

    let unsubCameraSignal = null;
    if (typeof props.cameraInputSignal === "string" && props.cameraInputSignal) {
      unsubCameraSignal = gosxSubscribeSharedSignal(props.cameraInputSignal, function(value) {
        if (disposed) return;
        const cam = (sceneIsPlainObject(value) && sceneIsPlainObject(value.camera)) ? value.camera : value;
        applyingSignalCamera = true;
        applyMountedSceneCamera(cam, "signal-camera");
        applyingSignalCamera = false;
      }, { immediate: false });
    }
    let unsubSelectionSignal = null;
    if (typeof props.selectionInputSignal === "string" && props.selectionInputSignal) {
      unsubSelectionSignal = gosxSubscribeSharedSignal(props.selectionInputSignal, function(value) {
        if (disposed) return;
        const id = typeof value === "string" ? value : (value && value.selectedID);
        applyMountedSceneSelection(id || "");
      }, { immediate: false });
    }
    let unsubGizmoSignal = null;
    if (typeof props.gizmoInputSignal === "string" && props.gizmoInputSignal) {
      unsubGizmoSignal = gosxSubscribeSharedSignal(props.gizmoInputSignal, function(value) {
        if (disposed) return;
        const mode = typeof value === "string" ? value : (value && value.mode);
        applyMountedSceneGizmoMode(mode || "");
      }, { immediate: false });
    }

    // Viewport observer fires on canvas/mount resize. Mark dirty so
    // renderFrame re-measures the rect on the next tick — this is the
    // one place we genuinely need a fresh getBoundingClientRect.
    const releaseViewportObserver = observeSceneViewport(ctx.mount, function(reason) {
      sceneUpdateScrollCameraMetrics(sceneState._scrollCamera, true);
      scheduleRenderWithViewport(reason);
    });
    const releaseCapabilityObserver = observeSceneCapability(ctx.mount, props, capability, function(reason) {
      // Capability change (DPR / WebGL availability shift) invalidates
      // the viewport — mark dirty so the next renderFrame re-measures.
      viewportDirty = true;
      sceneState.capability = capability;
      sceneState.materials = sceneNormalizeMaterialList(sceneState._materialSource, capability);
      const desiredFallback = sceneRendererFallbackReason(props, capability, renderer && renderer.kind);
      const webglPreference = sceneCapabilityWebGLPreference(props, capability);
      if (renderer && renderer.kind === "webgl" && !(webglPreference === "prefer" || webglPreference === "force")) {
        fallbackSceneRenderer(desiredFallback || "environment-constrained");
      } else if (renderer && renderer.kind !== "webgl" && (webglPreference === "prefer" || webglPreference === "force")) {
        if (!restoreSceneWebGLRenderer("")) {
          applySceneRendererState(ctx.mount, renderer, desiredFallback);
        }
      } else {
        applySceneRendererState(ctx.mount, renderer, desiredFallback);
      }
      scheduleRender(reason || "capability");
    });
    const releaseLifecycleObserver = observeSceneLifecycle(ctx.mount, lifecycle, function(reason) {
      publishSceneWaterLifecycleState(ctx.mount, sceneState, lifecycle, false);
      if (!sceneCanRender()) {
        cancelFrame();
        cancelScheduledRender();
        if (labelRefreshHandle != null) {
          cancelEngineFrame(labelRefreshHandle);
          labelRefreshHandle = null;
        }
        scheduleIdleContextRelease();
        return;
      }
      // Visibility/viewport presence transition — the mount may have
      // been offscreen, so force a re-measure on resume.
      clearIdleContextRelease();
      if (contextVoluntarilyLost) {
        restoreVoluntarilyLostContext();
        // The webglcontextrestored event handler will call
        // restoreSceneWebGLRenderer + scheduleRender once the
        // browser finishes restoring the context.
        return;
      }
      scheduleRenderWithViewport(reason || "lifecycle");
    });
    const releaseMotionObserver = observeSceneMotion(ctx.mount, motion, function(reason) {
      cancelFrame();
      cancelScheduledRender();
      // Reduced-motion transition resets render state; safer to re-
      // measure than risk stale canvas dimensions.
      scheduleRenderWithViewport(reason || "motion");
    });
    const releaseSceneCSSObserver = observeSceneCSSInvalidation();
    const releaseManagedControlForms = typeof bindSceneManagedControlForms === "function"
      ? bindSceneManagedControlForms(ctx.mount, sceneState, function(commands) {
          const result = applySceneCommands(sceneState, commands);
          publishSceneWaterStateSnapshot(ctx.mount, sceneState);
          publishSceneWaterLifecycleState(ctx.mount, sceneState, lifecycle, false);
          if (result && typeof result.then === "function") {
            result.then(function() {
              scheduleRender("managed-control-forms-models");
            });
          }
          scheduleRender("managed-control-forms");
        }, {
          getCamera: currentMountedSceneCamera,
            getOrbitState: currentMountedSceneOrbitState,
          setCamera: function(camera) {
            return applyMountedSceneCamera(camera, "managed-control-forms-camera");
          },
          getControlTarget: function() {
            return sceneControlsTarget(props);
          },
          stopCameraInertia: function() {
            if (flyMode) return false;
            cancelOrbitInertia();
            sceneOrbitStopInertia(controls);
            return true;
          },
          getBundle: function() {
            return latestBundle;
          },
        })
      : function() {};

    if (runtimeScene) {
      if (ctx.runtime && ctx.runtime.available()) {
        await applySceneCommands(sceneState, await ctx.runtime.hydrateFromProgramRef());
      } else {
        console.warn("[gosx] Scene3D runtime requested but shared engine runtime is unavailable");
      }
    }

    // WASM motion seam (P2.4b): lazy-load the scene's motion program once, then
    // each frame tick + decode packed transform writes into SET_TRANSFORM
    // commands routed through applySceneCommands (so state re-normalizes). Inert
    // unless window.__gosx_motion_wasm is set and the exports are present.
    //
    // SCOPE: this seam runs ONLY on the JS-sceneState render path — i.e. the
    // createSceneRenderBundle fall-through (onRenderEngine returns ""), and
    // declarative SceneIR scenes that ship motionProgram but render via JS rather
    // than the Go runtime bundle. Production shared-runtime Scene3D scenes call
    // ctx.runtime.renderFrame(), receive a non-empty runtimeBundle, and return at
    // line ~7128 BEFORE reaching this call — so this seam does NOT drive them.
    // Motion for those scenes is computed by motion.Eval inside
    // client/vm/scene_render_bundle.go and is baked into the Go-produced bundle.
    function applyWasmMotionFrame(timeSeconds) {
      if (wasmMotionState < 0) return;
      if (typeof window === "undefined" || !window.__gosx_motion_wasm
          || typeof window.__gosx_motion_load !== "function") {
        return;
      }
      if (wasmMotionState === 0) {
        // The scene IR (carrying motionProgram as base64) rides under
        // props.scene; some callers pass the scene object as props directly.
        const sceneIR = props && typeof props.scene === "object" && props.scene ? props.scene : props;
        const b64 = sceneIR && typeof sceneIR.motionProgram === "string" ? sceneIR.motionProgram : "";
        const handle = b64 ? window.__gosx_motion_load(sceneBase64Decode(b64)) : 0;
        const refs = handle >= 1 && typeof window.__gosx_motion_refs === "function"
          ? window.__gosx_motion_refs(handle) : null;
        if (!refs) { wasmMotionState = -1; return; }
        wasmMotionHandle = handle;
        wasmMotionTargetRefs = refs.target || [];
        wasmMotionPropRefs = refs.prop || [];
        wasmMotionF64 = new Float64Array(256);
        wasmMotionU8 = new Uint8Array(wasmMotionF64.buffer);
        wasmMotionState = 1;
      }
      const reduced = motion.reducedMotion === true;
      let count = window.__gosx_motion_tick(wasmMotionHandle, timeSeconds, reduced, wasmMotionU8);
      if (count > wasmMotionF64.length) {
        wasmMotionF64 = new Float64Array(count);
        wasmMotionU8 = new Uint8Array(wasmMotionF64.buffer);
        count = window.__gosx_motion_tick(wasmMotionHandle, timeSeconds, reduced, wasmMotionU8);
        if (count > wasmMotionF64.length) count = wasmMotionF64.length;
      }
      const f = wasmMotionF64;
      const cmds = [];
      for (let i = 0; i + 3 <= count;) {
        const arity = f[i + 2];
        // motion.ValueArity width: 0 scalar=1,1 vec2=2,2 vec3=3,3+ (vec4/quat/color)=4.
        const width = arity === 0 ? 1 : (arity >= 3 ? 4 : arity + 1);
        const ref = wasmMotionTargetRefs[f[i]];
        const prop = wasmMotionPropRefs[f[i + 1]];
        const c = i + 3;
        if (c + width > count) break;
        i = c + width;
        if (ref == null || prop == null) continue;
        let data = null;
        if (prop === "position" && width >= 3) {
          data = { x: f[c], y: f[c + 1], z: f[c + 2] };
        } else if (prop === "scale" && width >= 3) {
          data = { scaleX: f[c], scaleY: f[c + 1], scaleZ: f[c + 2] };
        } else if (prop === "rotation" && arity === 4) {
          const e = sceneQuatToEulerXYZ(f[c], f[c + 1], f[c + 2], f[c + 3]);
          data = { rotationX: e.x, rotationY: e.y, rotationZ: e.z };
        }
        if (data) cmds.push({ kind: SCENE_CMD_SET_TRANSFORM, objectId: ref, data });
      }
      if (cmds.length > 0) applySceneCommands(sceneState, cmds);
    }

    // C3: motion-evaluated MATERIAL UNIFORM animation. Mirrors
    // applyWasmMotionFrame but loads props.scene.materialMotionProgram (whose
    // tracks target material uniforms: targetRef=mesh id, prop=uniform name).
    // Each frame it ticks at absolute time t, decodes packed
    // [targetID, propID, arity, comps...] records, and writes the evaluated
    // value into the mesh's customUniforms bag (the same bag selena re-packs
    // per frame via sceneSelenaUniformData). MUST run BEFORE the per-frame
    // bundle build so the next createSceneRenderBundle clones the new value.
    // Stateless single-program tick at absolute t, so a grow-and-retick at the
    // same t is safe (no clock-advance concern like the model mixer).
    function applyWasmMaterialMotionFrame(timeSeconds) {
      if (wasmMatMotionState < 0) return;
      if (typeof window === "undefined" || !window.__gosx_motion_wasm
          || typeof window.__gosx_motion_load !== "function") {
        return;
      }
      if (wasmMatMotionState === 0) {
        const sceneIR = props && typeof props.scene === "object" && props.scene ? props.scene : props;
        const b64 = sceneIR && typeof sceneIR.materialMotionProgram === "string" ? sceneIR.materialMotionProgram : "";
        const handle = b64 ? window.__gosx_motion_load(sceneBase64Decode(b64)) : 0;
        if (handle < 1) { wasmMatMotionState = -1; return; }
        const refs = typeof window.__gosx_motion_refs === "function"
          ? window.__gosx_motion_refs(handle) : null;
        if (!refs) { wasmMatMotionState = -1; return; }
        wasmMatMotionHandle = handle;
        wasmMatMotionTargetRefs = refs.target || [];
        wasmMatMotionPropRefs = refs.prop || [];
        wasmMatMotionF64 = new Float64Array(256);
        wasmMatMotionU8 = new Uint8Array(wasmMatMotionF64.buffer);
        wasmMatMotionState = 1;
      }
      const reduced = motion.reducedMotion === true;
      let count = window.__gosx_motion_tick(wasmMatMotionHandle, timeSeconds, reduced, wasmMatMotionU8);
      if (count > wasmMatMotionF64.length) {
        wasmMatMotionF64 = new Float64Array(count);
        wasmMatMotionU8 = new Uint8Array(wasmMatMotionF64.buffer);
        count = window.__gosx_motion_tick(wasmMatMotionHandle, timeSeconds, reduced, wasmMatMotionU8);
        if (count > wasmMatMotionF64.length) count = wasmMatMotionF64.length;
      }
      const f = wasmMatMotionF64;
      for (let i = 0; i + 3 <= count;) {
        const arity = f[i + 2];
        // arity ENUM ordinal → component width: Scalar=0→1, Vec2=1→2, Vec3=2→3,
        // Vec4=3→4, Quat=4→4, Color=5→4.
        const width = arity <= 0 ? 1 : (arity >= 3 ? 4 : arity + 1);
        const meshId = wasmMatMotionTargetRefs[f[i]];
        const uniformName = wasmMatMotionPropRefs[f[i + 1]];
        const c = i + 3;
        if (c + width > count) break;
        i = c + width;
        if (meshId == null || uniformName == null) continue;
        const uniforms = sceneResolveMaterialUniforms(sceneState, meshId);
        if (!uniforms) continue;
        if (width === 1) {
          uniforms[uniformName] = f[c];
        } else {
          const arr = new Array(width);
          for (let k = 0; k < width; k++) arr[k] = f[c + k];
          uniforms[uniformName] = arr;
        }
      }
    }

    function renderFrame(now, reason) {
      if (disposed) return;
      const frameStart = typeof performance !== "undefined" && performance.now ? performance.now() : Date.now();
      const perfEnabled = typeof window !== "undefined" && window.__gosx_scene3d_perf === true;
      recordScenePerfCounter("render:" + (reason || "animation"));
      // Only re-measure the viewport when something has actually
      // invalidated it. Static frames (the common case during continuous
      // animation without DOM changes) reuse the cached `viewport` and
      // skip the 4 getBoundingClientRect layout flushes that used to
      // run every frame.
      if (viewportDirty) {
        const nextViewport = sceneViewportFromMount(ctx.mount, props, viewportBase, canvas, capability, adaptiveQuality);
        viewport = applySceneViewport(ctx.mount, canvas, labelLayer, nextViewport, viewportBase);
        viewportDirty = false;
      }
      if (!sceneCanRender()) {
        cancelFrame();
        return;
      }
      sceneAdvanceScrollCamera(sceneState._scrollCamera);
      const timeSeconds = now / 1000;
      const modelAnimationDelta = lastModelAnimationTimeSeconds == null
        ? 0
        : Math.max(0, Math.min(0.1, timeSeconds - lastModelAnimationTimeSeconds));
      lastModelAnimationTimeSeconds = timeSeconds;
      if (perfEnabled) performance.mark("scene3d-model-animations-start");
      sceneAdvanceModelAnimations(sceneState, modelAnimationDelta, motion.reducedMotion === true);
      if (perfEnabled) {
        performance.mark("scene3d-model-animations-end");
        performance.measure("scene3d-model-animations", "scene3d-model-animations-start", "scene3d-model-animations-end");
        performance.clearMarks("scene3d-model-animations-start");
        performance.clearMarks("scene3d-model-animations-end");
      }
      if (runtimeScene && ctx.runtime && typeof ctx.runtime.renderFrame === "function") {
        const runtimeBundle = ctx.runtime.renderFrame(timeSeconds, viewport.cssWidth, viewport.cssHeight);
        if (runtimeBundle) {
          const effectiveBundle = sceneBundleWithCameraOverride(
            runtimeBundle,
            sceneCurrentControlCamera(sceneControlHandle.controller, runtimeBundle.camera || sceneState.camera, sceneState._scrollCamera),
          );
          effectiveBundle.waterShaderSourcesByID = mountedWaterShaderSources;
          sceneHydrateBundleWaterShaderSources(effectiveBundle, effectiveBundle.waterShaderSourcesByID);
          latestBundle = effectiveBundle;
          publishMountedSceneCamera(effectiveBundle.camera, reason || "render");
          if (!ensureRendererCanCoverBundle(effectiveBundle)) {
            scheduleNextAnimationFrame();
            return;
          }
          syncSceneNodeSentinels(effectiveBundle);
          renderer.render(effectiveBundle, viewport);
          recordSceneWaterFrame(ctx.mount, effectiveBundle);
          renderSceneLabels(labelLayer, effectiveBundle, labelLayoutCache, labelElements, viewport.cssWidth, viewport.cssHeight);
          renderSceneSprites(labelLayer, effectiveBundle, spriteElements, viewport.cssWidth, viewport.cssHeight);
          renderSceneHTML(labelLayer, effectiveBundle, htmlElements, viewport.cssWidth, viewport.cssHeight, htmlTextureState);
          if (statsOverlay) {
            statsOverlay.update(effectiveBundle, frameStart, renderer, viewport);
          }
          if (inspectorOverlay) {
            inspectorOverlay.update();
          }
          if (sceneUpdateAdaptiveQuality(adaptiveQuality, ctx.mount, sceneState, viewport, frameStart)) {
            viewportDirty = true;
          }
          scheduleNextAnimationFrame();
          return;
        }
      }
      if (runtimeScene && ctx.runtime) {
        const commandResult = applySceneCommands(sceneState, ctx.runtime.tick());
        if (commandResult && typeof commandResult.then === "function") {
          commandResult.then(function() {
            scheduleRender("runtime-model-commands");
          });
        }
      }
      applyWasmMotionFrame(timeSeconds);
      // C3: write motion-evaluated material uniforms into customUniforms BEFORE
      // the bundle build below, so the next createSceneRenderBundle (and the
      // selena per-frame re-pack) observes them.
      applyWasmMaterialMotionFrame(timeSeconds);
      sceneAdvanceTransitions(sceneState, now);
      // LOD: swap vertex data based on camera distance before building render bundle.
      if (typeof sceneApplyLOD === "function" && props.compression && props.compression.lod) {
        var cam = sceneCurrentControlCamera(sceneControlHandle.controller, sceneState.camera, sceneState._scrollCamera);
        var camX = cam.x || 0, camY = cam.y || 0, camZ = cam.z || 0;
        for (var li = 0; li < sceneState.points.length; li++) {
          sceneApplyLOD(sceneState.points[li], camX, camY, camZ);
        }
      }
      if (perfEnabled) performance.mark("scene3d-bundle-start");
      const activeCamera = sceneCurrentControlCamera(sceneControlHandle.controller, sceneState.camera, sceneState._scrollCamera);
      applySceneHTMLTextureRecordsToState(sceneState, htmlTextureState);
      latestBundle = createSceneRenderBundle(
        viewport.cssWidth,
        viewport.cssHeight,
        sceneState.background,
        activeCamera,
        sceneStateObjectsWithMaterials(sceneState),
        sceneStateLabels(sceneState),
        sceneStateSprites(sceneState),
        sceneStateHTML(sceneState),
        sceneStateLights(sceneState),
        sceneState.environment,
        timeSeconds,
        sceneStatePointsWithMaterials(sceneState),
        sceneStateInstancedMeshesWithMaterials(sceneState),
        sceneState.computeParticles,
        sceneState.waterSystems,
        sceneState.postEffects,
        sceneState.postFXMaxPixels,
        sceneBool(props && Object.prototype.hasOwnProperty.call(props, "showGrid") ? props.showGrid : (props && props.debugGrid), false),
      );
      latestBundle.waterShaderSourcesByID = mountedWaterShaderSources;
      sceneHydrateBundleWaterShaderSources(latestBundle, latestBundle.waterShaderSourcesByID);
      publishMountedSceneCamera(latestBundle.camera, reason || "render");
      if (perfEnabled) {
        performance.mark("scene3d-bundle-end");
        performance.measure("scene3d-bundle", "scene3d-bundle-start", "scene3d-bundle-end");
        performance.clearMarks("scene3d-bundle-start");
        performance.clearMarks("scene3d-bundle-end");
      }
      if (!ensureRendererCanCoverBundle(latestBundle)) {
        scheduleNextAnimationFrame();
        return;
      }
      syncSceneNodeSentinels(latestBundle);
      renderer.render(latestBundle, viewport);
      recordSceneWaterFrame(ctx.mount, latestBundle);
      maybeEmitRenderEmpty(latestBundle);
      renderSceneLabels(labelLayer, latestBundle, labelLayoutCache, labelElements, viewport.cssWidth, viewport.cssHeight);
      renderSceneSprites(labelLayer, latestBundle, spriteElements, viewport.cssWidth, viewport.cssHeight);
      renderSceneHTML(labelLayer, latestBundle, htmlElements, viewport.cssWidth, viewport.cssHeight, htmlTextureState);
      if (statsOverlay) {
        statsOverlay.update(latestBundle, frameStart, renderer, viewport);
      }
      if (inspectorOverlay) {
        inspectorOverlay.update();
      }
      if (sceneUpdateAdaptiveQuality(adaptiveQuality, ctx.mount, sceneState, viewport, frameStart)) {
        viewportDirty = true;
      }
      scheduleNextAnimationFrame();
    }

    function maybeEmitRenderEmpty(bundle) {
      if (!sceneRendererRecentlySwapped) {
        return;
      }
      sceneRendererRecentlySwapped = false;
      const reason = sceneRendererLastSwapReason;
      sceneRendererLastSwapReason = "";
      const bundleVerts = Number((bundle && bundle.vertexCount) || 0);
      const worldVerts = Number((bundle && bundle.worldVertexCount) || 0);
      const surfaceCount = Array.isArray(bundle && bundle.surfaces) ? bundle.surfaces.length : 0;
      const bundleMeshObjects = Array.isArray(bundle && bundle.meshObjects) ? bundle.meshObjects.length : 0;
      const bundleInstancedMeshes = Array.isArray(bundle && bundle.instancedMeshes) ? bundle.instancedMeshes.length : 0;
      // A bundle with legacy verts, surfaces, OR a modern PBR mesh/instance list
      // means the renderer had something to draw. Only if ALL paths are empty
      // and sceneState itself has drawable content do we call it render-empty.
      if (bundleVerts > 0 || worldVerts > 0 || surfaceCount > 0
          || bundleMeshObjects > 0 || bundleInstancedMeshes > 0) {
        // Bundle had geometry — schedule a canvas-pixel check next tick to
        // confirm something actually landed on the drawing buffer. Gated by
        // GOSX_TELEMETRY feature flag on the client config so we don't probe
        // on every swap in production unless requested.
        scheduleCanvasBlankProbe(reason, {
          bundleMeshObjects,
          bundleInstancedMeshes,
          bundleVerts: bundleVerts + worldVerts,
        });
        return;
      }
      const pointCount = Array.isArray(sceneState.points) ? sceneState.points.length : 0;
      const objectCount = (sceneState.meshObjects ? sceneState.meshObjects.length : 0)
        + (Array.isArray(sceneState.objects) ? sceneState.objects.length : 0);
      const instanceCount = Array.isArray(sceneState.instancedMeshes) ? sceneState.instancedMeshes.length : 0;
      if (pointCount + objectCount + instanceCount === 0) {
        return;
      }
      gosxSceneEmit("error", "render-empty", {
        rendererKind: renderer && renderer.kind ? renderer.kind : "",
        lastSwapReason: reason,
        scenePoints: pointCount,
        sceneObjects: objectCount,
        sceneInstances: instanceCount,
      });
    }

    // scheduleCanvasBlankProbe: readback-based blank-canvas diagnostics are
    // deliberately opt-in. Canvas serialization/readPixels can force GPU
    // synchronization and has caused context loss in active scenes.
    function scheduleCanvasBlankProbe(reason, stats) {
      if (typeof window === "undefined" || !window.__gosx_telemetry_config
          || window.__gosx_telemetry_config.probeCanvasBlank !== true
          || window.__gosx_telemetry_config.allowCanvasReadbackProbe !== true) {
        return;
      }
      if (typeof window.requestAnimationFrame !== "function") {
        return;
      }
      window.requestAnimationFrame(function () {
        window.requestAnimationFrame(function () {
          if (disposed || !renderer || renderer.kind !== "webgl") {
            return;
          }
          if (typeof canvas.toBlob !== "function") {
            return;
          }
          canvas.toBlob(function (blob) {
            if (disposed || !renderer || renderer.kind !== "webgl") {
              return;
            }
            // PNG threshold: a uniform-color 800x461 PNG is ~400-900 bytes;
            // set the floor generously to avoid false positives on sparse scenes.
            const kCanvasBlankPNGBytesThreshold = 1800;
            const byteSize = blob && typeof blob.size === "number" ? blob.size : 0;
            if (byteSize > kCanvasBlankPNGBytesThreshold) {
              return;
            }
            const gl = typeof canvas.getContext === "function"
              ? (canvas.getContext("webgl2") || canvas.getContext("webgl"))
              : null;
            gosxSceneEmit("error", "render-canvas-blank", {
              rendererKind: renderer && renderer.kind ? renderer.kind : "",
              lastSwapReason: reason || "",
              bundleMeshObjects: stats ? stats.bundleMeshObjects : 0,
              bundleInstancedMeshes: stats ? stats.bundleInstancedMeshes : 0,
              bundleVerts: stats ? stats.bundleVerts : 0,
              canvasPngBytes: byteSize,
              canvasPngThreshold: kCanvasBlankPNGBytesThreshold,
              glError: gl && typeof gl.getError === "function" ? gl.getError() : 0,
            });
          }, "image/png");
        });
      });
    }

    await sceneModelHydration;
    scenePrimeInitialTransitions(sceneState, motion.reducedMotion, 0);

    // Defer the first renderFrame to the next frame boundary. Goal: let
    // the browser paint the pre-existing CSS/DOM content (LCP candidate)
    // one frame earlier than it would if renderFrame ran synchronously
    // here.
    //
    // On real hardware this is a small LCP nudge (real browsers show
    // ~0 long tasks during mount — shader compile + buffer upload is
    // typically 50-200ms on a real GPU, well under the 50ms long-task
    // threshold once broken by this deferral).
    //
    // On headless-shell SwiftShader it's a big win: SwiftShader can
    // take 1-2 seconds to compile/fall-back-compile the point shaders,
    // and deferring keeps that entire chunk out of the LCP window so
    // visual regression captures and CI perf profiles aren't dominated
    // by GPU software-emulation latency.
    //
    // Scheduling priority (best → fallback):
    //   1. scheduler.postTask('user-visible') — Chrome 94+, Firefox 126+
    //   2. requestAnimationFrame — universal, paints on next vsync
    //   3. setTimeout(0) — last-resort task-queue defer
    function scheduleInitialRender() {
      if (disposed) return;
      // Defer the first renderFrame to a microtask so the browser has
      // a chance to paint the pre-existing CSS/DOM content (the LCP
      // candidate) before the scene's GL upload kicks off. A microtask
      // runs after the current synchronous mount logic returns but
      // before the next macrotask, which gives the browser its paint
      // opportunity without waiting a full rAF cycle.
      //
      // Using a microtask here instead of rAF also resolves a subtle
      // test / semantic conflict: several runtime.test.js assertions
      // expect the first render to have happened by the time
      // flushAsyncWork() returns (defers-offscreen, prefers-reduced-
      // motion, hydrates-shared-runtime, etc.). Those tests predate the
      // LCP-deferral optimization and were written against a sync-
      // mount-render + rAF-animation-loop pattern. A microtask honors
      // both contracts — tests see the render, LCP is still improved
      // relative to a fully synchronous mount.
      //
      // Once this initial renderFrame runs it falls through to
      // scheduleNextAnimationFrame which handles the normal rAF chain
      // for animated scenes (and correctly does nothing for reduced-
      // motion / non-animated scenes — keeping raf.count at 0 in tests
      // that care).
      Promise.resolve().then(function() {
        if (!disposed) renderFrame(0);
      });
    }
    scheduleInitialRender();

    // Progressive: upgrade from preview to full resolution after first paint.
    if (typeof sceneUpgradeProgressive === "function" && props.compression && props.compression.progressive) {
      scheduleSceneIdleTask(function() {
        sceneUpgradeProgressive(props);
        // Force a re-render with upgraded data
        if (sceneWantsAnimation()) {
          // Animation loop will pick it up
        } else {
          renderFrame(0);
        }
      }, sceneCompressionProgressiveDelay(props));
    }

    if (Array.isArray(sceneState._deferredPostEffects) && sceneState._deferredPostEffects.length > 0) {
      scheduleSceneIdleTask(function() {
        sceneState.postEffects = sceneState._deferredPostEffects;
        sceneState._deferredPostEffects = null;
        sceneApplyAdaptivePostFX(sceneState, adaptiveQuality);
        applyScenePostFXState(ctx.mount, sceneState);
        if (sceneWantsAnimation()) {
          // Animation loop will render the upgraded chain.
        } else {
          renderFrame(0);
        }
      }, sceneDeferredPostFXDelay(props));
    }

    setAttrValue(ctx.mount, "data-gosx-scene3d-ready", "true");
    ctx.emit("mounted", {
      width: viewport.cssWidth,
      height: viewport.cssHeight,
      objects: sceneStateObjects(sceneState).length,
      labels: sceneStateLabels(sceneState).length,
      sprites: sceneStateSprites(sceneState).length,
      html: sceneStateHTML(sceneState).length,
      lights: sceneStateLights(sceneState).length,
      models: sceneModels(props).length,
    });

    // Scroll-driven camera: scroll input should be visible immediately even
    // when an animated scene already has a frame loop running.
    var scrollHandler = null;
    var visualViewportScrollHandler = null;
    if (sceneState._scrollCamera) {
      sceneState._scrollCamera._progress = 0;
      sceneState._scrollCamera._smoothProgress = 0;
      sceneUpdateScrollCameraMetrics(sceneState._scrollCamera, true);
      scrollHandler = function() {
        sceneUpdateScrollCameraMetrics(sceneState._scrollCamera, false, true);
        scheduleRender("scroll");
      };
      window.addEventListener("scroll", scrollHandler, { passive: true });
      // visualViewport listeners are only meaningful on touch devices where
      // the visual viewport can differ from the layout viewport (mobile URL
      // bar animations, virtual keyboard, pinch-zoom). On desktop browsers
      // the visual viewport tracks the window 1:1 and the listeners just
      // add event-handler overhead — on Firefox specifically they've been
      // observed to contribute to sustained-scroll jank because each fire
      // re-wakes the render loop.
      var isTouchDevice =
        (typeof navigator !== "undefined" && navigator.maxTouchPoints > 0) ||
        ("ontouchstart" in window);
      if (
        isTouchDevice &&
        window.visualViewport &&
        typeof window.visualViewport.addEventListener === "function"
      ) {
        visualViewportScrollHandler = function() {
          sceneUpdateScrollCameraMetrics(sceneState._scrollCamera, true, true);
          scheduleRender("visual-viewport");
        };
        window.visualViewport.addEventListener("scroll", visualViewportScrollHandler, { passive: true });
        window.visualViewport.addEventListener("resize", visualViewportScrollHandler, { passive: true });
      }
      sceneAdvanceScrollCamera(sceneState._scrollCamera);
    }

    return {
      applyCommands(commands) {
        const result = applySceneCommands(sceneState, commands);
        publishSceneWaterStateSnapshot(ctx.mount, sceneState);
        publishSceneWaterLifecycleState(ctx.mount, sceneState, lifecycle, false);
        if (result && typeof result.then === "function") {
          result.then(function() {
            scheduleRender("commands-models");
          });
        }
        scheduleRender("commands");
      },
      getCamera() {
        return currentMountedSceneCamera();
      },
      setCamera(camera) {
        return applyMountedSceneCamera(camera, "handle-camera");
      },
      dispose() {
        disposed = true;
        publishSceneWaterLifecycleState(ctx.mount, sceneState, lifecycle, true);
        clearIdleContextRelease();
        clearVoluntaryRestoreWatchdog();
        stopSceneRenderWatchdog();
        if (webgpuProbeReadyListener && typeof window !== "undefined" && typeof window.removeEventListener === "function") {
          window.removeEventListener("gosx:scene3d:webgpu-probe-ready", webgpuProbeReadyListener);
          webgpuProbeReadyListener = null;
        }
        if (scrollHandler) {
          window.removeEventListener("scroll", scrollHandler);
        }
        if (visualViewportScrollHandler && window.visualViewport && typeof window.visualViewport.removeEventListener === "function") {
          window.visualViewport.removeEventListener("scroll", visualViewportScrollHandler);
          window.visualViewport.removeEventListener("resize", visualViewportScrollHandler);
        }
	        detachSceneCanvasContextListeners(canvas);
        document.removeEventListener("gosx:hub:event", sceneHubListener);
        if (unsubCameraSignal) unsubCameraSignal();
        if (unsubSelectionSignal) unsubSelectionSignal();
        if (unsubGizmoSignal) unsubGizmoSignal();
        releaseViewportObserver();
        releaseCapabilityObserver();
        releaseLifecycleObserver();
        releaseMotionObserver();
        releaseSceneCSSObserver();
        releaseManagedControlForms();
        releaseTextLayoutListener();
        releaseSceneDebugSurface();
        dragHandle.dispose();
        pickHandle.dispose();
        sceneControlHandle.dispose();
        renderer.dispose();
        disposeSceneHTMLTextureState(htmlTextureState);
        if (wasmMotionState === 1 && typeof window !== "undefined"
            && typeof window.__gosx_motion_unload === "function") {
          window.__gosx_motion_unload(wasmMotionHandle);
        }
        wasmMotionState = -1;
        wasmMotionHandle = 0;
        // C3: free the material-uniform motion program handle.
        if (wasmMatMotionState === 1 && typeof window !== "undefined"
            && typeof window.__gosx_motion_unload === "function") {
          window.__gosx_motion_unload(wasmMatMotionHandle);
        }
        wasmMatMotionState = -1;
        wasmMatMotionHandle = 0;
        // P4-M3: free any per-model WASM motion mixers created behind the flag.
        sceneDestroyModelWasmMixers(sceneState && sceneState._modelSkins);
        cancelFrame();
        cancelScheduledRender();
        if (pendingMotionHandle != null) {
          cancelEngineFrame(pendingMotionHandle);
          pendingMotionHandle = null;
        }
        if (labelRefreshHandle != null) {
          cancelEngineFrame(labelRefreshHandle);
        }
        if (canvas.parentNode === ctx.mount) {
          ctx.mount.removeChild(canvas);
        }
        if (labelLayer.parentNode === ctx.mount) {
          ctx.mount.removeChild(labelLayer);
        }
        if (statsOverlay) {
          statsOverlay.dispose();
        }
        if (inspectorOverlay) {
          inspectorOverlay.dispose();
        }
        if (sentinelLayer.parentNode) {
          sentinelLayer.parentNode.removeChild(sentinelLayer);
        }
        delete ctx.mount.__gosxScene3DSentinels;
        delete ctx.mount.__gosxScene3DState;
        delete ctx.mount.__gosxScene3DCSSDynamic;
        delete ctx.mount.__gosxScene3DCSSRevision;
        delete ctx.mount.__gosxScene3DCSSAnimationUntil;
      },
    };
  });
