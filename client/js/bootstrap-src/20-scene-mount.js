  function createSceneRenderer(canvas, props, capability) {
    const webglPreference = sceneCapabilityWebGLPreference(props, capability);
    if (webglPreference === "prefer" || webglPreference === "force") {
      // WebGPU: deferred — canvas.getContext("webgpu") taints the canvas,
      // preventing WebGL2 fallback. WebGPU activation requires async adapter
      // probing before touching the canvas. For now, WebGL2 PBR handles all
      // rendering; WebGPU upgrades in a future release with async renderer swap.
      if (typeof createScenePBRRendererOrFallback === "function") {
        const gl = typeof canvas.getContext === "function" ? canvas.getContext("webgl2", {
          alpha: true,
          premultipliedAlpha: false,
          antialias: capability.tier === "full" && !capability.lowPower && !capability.reducedData,
          powerPreference: capability.lowPower || capability.tier === "constrained" ? "low-power" : "high-performance",
        }) : null;
        if (gl) {
          const pbrRenderer = createScenePBRRendererOrFallback(gl, canvas, {});
          if (pbrRenderer) {
            return {
              renderer: pbrRenderer,
              fallbackReason: "",
            };
          }
        }
      }
      const webglRenderer = createSceneWebGLRenderer(canvas, {
        antialias: capability.tier === "full" && !capability.lowPower && !capability.reducedData,
        powerPreference: capability.lowPower || capability.tier === "constrained" ? "low-power" : "high-performance",
      });
      if (webglRenderer) {
        return {
          renderer: webglRenderer,
          fallbackReason: "",
        };
      }
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

  function sceneModelRotateDirection(point, model) {
    return sceneRotatePoint(
      point && typeof point === "object" ? point : { x: 0, y: 0, z: 0 },
      sceneNumber(model && model.rotationX, 0),
      sceneNumber(model && model.rotationY, 0),
      sceneNumber(model && model.rotationZ, 0),
    );
  }

  function sceneModelMaterialOverrideSource(model) {
    return model && model.materialOverride && typeof model.materialOverride === "object"
      ? model.materialOverride
      : null;
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
    if (typeof override.materialKind === "string" && override.materialKind) {
      next.materialKind = override.materialKind;
      if (typeof next.material === "string") {
        next.material = override.materialKind;
      }
      if (material) {
        material.kind = override.materialKind;
      }
    }
    sceneAssignMaterialOverride(next, material, "color", "color", override);
    sceneAssignMaterialOverride(next, material, "texture", "texture", override);
    sceneAssignMaterialOverride(next, material, "opacity", "opacity", override);
    sceneAssignMaterialOverride(next, material, "emissive", "emissive", override);
    sceneAssignMaterialOverride(next, material, "blendMode", "blendMode", override);
    sceneAssignMaterialOverride(next, material, "renderPass", "renderPass", override);
    sceneAssignMaterialOverride(next, material, "wireframe", "wireframe", override);
    if (material) {
      next.material = material;
    }
    return next;
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

  function sceneInstantiateModelObject(rawObject, model, prefix, index) {
    const source = sceneApplyMaterialOverride(rawObject, model);
    const normalized = normalizeSceneObject(source, index);
    if (normalized.kind === "lines") {
      return sceneModelLineObject(normalized, model, prefix);
    }
    return sceneModelPrimitiveObject(normalized, model, prefix);
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

  function parseSceneModelAsset(raw, src) {
    let payload = raw;
    if (payload && typeof payload === "object" && payload.scene && typeof payload.scene === "object") {
      payload = payload.scene;
    }
    if (Array.isArray(payload)) {
      payload = { objects: payload };
    }
    const record = payload && typeof payload === "object" ? payload : {};
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
      objects: Array.isArray(record.objects) ? record.objects.map(function(object) {
        return resolveSceneModelObjectURLs(src, object);
      }) : [],
      labels: Array.isArray(record.labels) ? record.labels : [],
      sprites,
      lights: Array.isArray(record.lights) ? record.lights : [],
    };
  }

  async function loadSceneModelAsset(src) {
    const key = String(src || "").trim();
    if (!key) {
      return parseSceneModelAsset({}, key);
    }
    if (!sceneModelAssetCache.has(key)) {
      sceneModelAssetCache.set(key, (async function() {
        try {
          const response = await fetch(key, { credentials: "same-origin" });
          if (!response || !response.ok) {
            throw new Error("HTTP " + String(response && response.status || 0));
          }
          return parseSceneModelAsset(await response.json(), key);
        } catch (error) {
          console.warn("[gosx] failed to load Scene3D model asset:", key, error && error.message ? error.message : error);
          return parseSceneModelAsset({}, key);
        }
      })());
    }
    return sceneModelAssetCache.get(key);
  }

  async function hydrateSceneStateModels(state, props) {
    const models = sceneModels(props);
    if (!models.length) {
      return { models: 0, objects: 0, labels: 0, sprites: 0, lights: 0 };
    }
    let objectCount = 0;
    let labelCount = 0;
    let spriteCount = 0;
    let lightCount = 0;
    await Promise.all(models.map(async function(model, modelIndex) {
      const asset = await loadSceneModelAsset(model.src);
      const prefix = model.id || ("scene-model-" + modelIndex);
      for (let i = 0; i < asset.objects.length; i += 1) {
        const object = sceneInstantiateModelObject(asset.objects[i], model, prefix, i);
        if (!object) {
          continue;
        }
        state.objects.set(object.id, object);
        objectCount += 1;
      }
      for (let i = 0; i < asset.labels.length; i += 1) {
        const label = sceneInstantiateModelLabel(asset.labels[i], model, prefix, i);
        if (!label || !label.text.trim()) {
          continue;
        }
        state.labels.set(label.id, label);
        labelCount += 1;
      }
      for (let i = 0; i < asset.sprites.length; i += 1) {
        const sprite = sceneInstantiateModelSprite(asset.sprites[i], model, prefix, i);
        if (!sprite) {
          continue;
        }
        state.sprites.set(sprite.id, sprite);
        spriteCount += 1;
      }
      for (let i = 0; i < asset.lights.length; i += 1) {
        const light = sceneInstantiateModelLight(asset.lights[i], model, prefix, i);
        if (!light) {
          continue;
        }
        state.lights.set(light.id, light);
        lightCount += 1;
      }
    }));
    return { models: models.length, objects: objectCount, labels: labelCount, sprites: spriteCount, lights: lightCount };
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

  function sceneCapabilityProfile(props) {
    const requestedTier = normalizeSceneCapabilityTier(props && props.capabilityTier);
    const environment = sceneEnvironmentState();
    const navigatorRef = window && window.navigator ? window.navigator : {};
    const coarsePointer = environment ? Boolean(environment.coarsePointer) : (sceneMediaQueryMatches("(pointer: coarse)") || sceneMediaQueryMatches("(any-pointer: coarse)"));
    const hover = environment ? Boolean(environment.hover) : (sceneMediaQueryMatches("(hover: hover)") || sceneMediaQueryMatches("(any-hover: hover)"));
    const reducedData = environment ? Boolean(environment.reducedData) : sceneMediaQueryMatches("(prefers-reduced-data: reduce)");
    const lowPower = environment ? Boolean(environment.lowPower) : false;
    const visualViewportActive = environment ? Boolean(environment.visualViewportActive) : Boolean(window.visualViewport);
    const deviceMemory = sceneNumber(environment && environment.deviceMemory, sceneNumber(navigatorRef && navigatorRef.deviceMemory, 0));
    const hardwareConcurrency = Math.max(0, Math.floor(sceneNumber(environment && environment.hardwareConcurrency, sceneNumber(navigatorRef && navigatorRef.hardwareConcurrency, 0))));
    const constrainedHardware = lowPower || reducedData || (deviceMemory > 0 && deviceMemory <= 4) || (hardwareConcurrency > 0 && hardwareConcurrency <= 4);

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
      visualViewportActive,
      deviceMemory,
      hardwareConcurrency,
    };
  }

  function sceneCapabilityWebGLPreference(props, capability) {
    if (!sceneBool(props && props.preferWebGL, true)) {
      return "disabled";
    }
    if (sceneBool(props && props.forceWebGL, false)) {
      return "force";
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

  function sceneRendererFallbackReason(props, capability, rendererKind) {
    if (rendererKind === "webgl") {
      return "";
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
    setAttrValue(mount, "data-gosx-scene3d-visual-viewport", capability.visualViewportActive ? "true" : "false");
    setAttrValue(mount, "data-gosx-scene3d-webgl-preference", sceneCapabilityWebGLPreference(props, capability));
    setAttrValue(mount, "data-gosx-scene3d-device-memory", capability.deviceMemory > 0 ? capability.deviceMemory : "");
    setAttrValue(mount, "data-gosx-scene3d-hardware-concurrency", capability.hardwareConcurrency > 0 ? capability.hardwareConcurrency : "");
  }

  function applySceneRendererState(mount, renderer, fallbackReason) {
    if (!mount) {
      return;
    }
    setAttrValue(mount, "data-gosx-scene3d-renderer", renderer && renderer.kind ? renderer.kind : "");
    setAttrValue(mount, "data-gosx-scene3d-renderer-fallback", fallbackReason || "");
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

  function sceneViewportDevicePixelRatio(props, maxDevicePixelRatio) {
    const environment = sceneEnvironmentState();
    const preferred = sceneNumber(
      props && (props.devicePixelRatio || props.pixelRatio),
      sceneNumber(window && window.devicePixelRatio, sceneNumber(environment && environment.devicePixelRatio, 1)),
    );
    return Math.max(1, Math.min(Math.max(1, maxDevicePixelRatio || 1), preferred));
  }

  function sceneViewportFromMount(mount, props, base, canvas, capability) {
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
    const maxDevicePixelRatio = Math.max(1, base.explicitMaxDevicePixelRatio > 0 ? base.explicitMaxDevicePixelRatio : defaultSceneMaxDevicePixelRatio(capability));
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

    if (typeof ResizeObserver === "function") {
      resizeObserver = new ResizeObserver(function() {
        refresh("resize");
      });
      if (typeof resizeObserver.observe === "function") {
        resizeObserver.observe(mount);
      }
    } else if (typeof window.addEventListener === "function") {
      windowResizeListener = function() {
        refresh("resize");
      };
      window.addEventListener("resize", windowResizeListener);
    }

    if (window.__gosx.environment && typeof window.__gosx.environment.observe === "function") {
      stopEnvironment = window.__gosx.environment.observe(function() {
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

  function observeSceneLifecycle(mount, lifecycle, onChange) {
    if (!mount || !lifecycle || typeof onChange !== "function") {
      return function() {};
    }

    let stopIntersection = null;
    let stopEnvironment = null;

    if (typeof IntersectionObserver === "function") {
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

  function normalizeSceneControlsMode(value) {
    switch (String(value || "").trim().toLowerCase()) {
      case "orbit":
        return "orbit";
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

  function sceneWorldCameraPosition(camera) {
    const normalized = sceneRenderCamera(camera);
    return {
      x: normalized.x,
      y: normalized.y,
      z: -normalized.z,
    };
  }

  function sceneOrbitStateFromCamera(camera, target) {
    const normalized = sceneRenderCamera(camera);
    const worldPosition = sceneWorldCameraPosition(normalized);
    const orbitTarget = target || { x: 0, y: 0, z: 0 };
    const offsetX = worldPosition.x - sceneNumber(orbitTarget.x, 0);
    const offsetY = worldPosition.y - sceneNumber(orbitTarget.y, 0);
    const offsetZ = worldPosition.z - sceneNumber(orbitTarget.z, 0);
    const radius = Math.max(0.6, Math.hypot(offsetX, offsetY, offsetZ));
    return {
      target: {
        x: sceneNumber(orbitTarget.x, 0),
        y: sceneNumber(orbitTarget.y, 0),
        z: sceneNumber(orbitTarget.z, 0),
      },
      radius,
      yaw: Math.atan2(offsetX, -offsetZ),
      pitch: Math.asin(sceneClamp(offsetY / radius, -0.98, 0.98)),
      fov: normalized.fov,
      near: normalized.near,
      far: normalized.far,
    };
  }

  function sceneOrbitCamera(state, fallbackCamera) {
    const base = sceneRenderCamera(fallbackCamera);
    const orbit = state || sceneOrbitStateFromCamera(base, { x: 0, y: 0, z: 0 });
    const radius = Math.max(0.6, sceneNumber(orbit.radius, 6));
    const pitch = sceneClamp(sceneNumber(orbit.pitch, 0), -1.4, 1.4);
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
      rotationX: -Math.atan2(forward.y, horizontal),
      rotationY: Math.atan2(forward.x, forward.z),
      rotationZ: 0,
      fov: sceneNumber(orbit.fov, base.fov),
      near: sceneNumber(orbit.near, base.near),
      far: sceneNumber(orbit.far, base.far),
    };
  }

  function createSceneControls(props) {
    const mode = normalizeSceneControlsMode(props && props.controls);
    if (!mode) {
      return null;
    }
    return {
      mode,
      active: false,
      touched: false,
      pointerId: null,
      lastX: 0,
      lastY: 0,
      rotateSpeed: sceneControlsRotateSpeed(props),
      zoomSpeed: sceneControlsZoomSpeed(props),
      orbit: null,
      target: sceneControlsTarget(props),
    };
  }

  function syncSceneControlsFromCamera(controls, camera) {
    if (!controls || controls.mode !== "orbit" || controls.active || controls.touched) {
      return;
    }
    controls.orbit = sceneOrbitStateFromCamera(camera, controls.target);
  }

  function sceneCurrentControlCamera(controls, sourceCamera, scrollCamera) {
    var cam;
    if (controls && controls.mode === "orbit") {
      syncSceneControlsFromCamera(controls, sourceCamera);
      cam = controls.orbit ? sceneOrbitCamera(controls.orbit, sourceCamera) : sceneRenderCamera(sourceCamera);
    } else {
      cam = sceneRenderCamera(sourceCamera);
    }
    if (scrollCamera && scrollCamera.start !== scrollCamera.end) {
      // Smooth interpolation — mimics GSAP ScrollTrigger scrub:1.
      var target = scrollCamera._progress || 0;
      var current = scrollCamera._smoothProgress || 0;
      current += (target - current) * 0.08;
      scrollCamera._smoothProgress = current;
      cam.z = scrollCamera.start + current * (scrollCamera.end - scrollCamera.start);
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
    controls.orbit.yaw += (sample.deltaX / Math.max(metrics.width, 1)) * Math.PI * controls.rotateSpeed;
    controls.orbit.pitch = sceneClamp(
      controls.orbit.pitch + (sample.deltaY / Math.max(metrics.height, 1)) * Math.PI * controls.rotateSpeed,
      -1.4,
      1.4,
    );
    if (typeof event.preventDefault === "function") {
      event.preventDefault();
    }
    if (typeof event.stopPropagation === "function") {
      event.stopPropagation();
    }
    scheduleRender("controls");
  }

  function sceneOrbitFinishDrag(controls, canvas, detachDocumentListeners, event) {
    if (!sceneDragMatchesActivePointer(controls, event)) {
      return;
    }
    const pointerId = controls.pointerId;
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
  }

  function sceneOrbitApplyWheel(controls, readSourceCamera, scheduleRender, event) {
    syncSceneControlsFromSource(controls, readSourceCamera);
    controls.touched = true;
    controls.orbit.radius = sceneClamp(
      controls.orbit.radius * Math.exp(sceneNumber(event && event.deltaY, 0) * 0.001 * controls.zoomSpeed),
      0.6,
      256,
    );
    if (event && typeof event.preventDefault === "function") {
      event.preventDefault();
    }
    if (event && typeof event.stopPropagation === "function") {
      event.stopPropagation();
    }
    scheduleRender("controls");
  }

  function setupSceneBuiltInControls(canvas, props, readViewport, readSourceCamera, scheduleRender) {
    const controls = createSceneControls(props);
    if (!canvas || !controls || controls.mode !== "orbit") {
      return {
        controller: controls,
        dispose() {},
      };
    }

    let documentListenersAttached = false;
    canvas.style.cursor = "grab";
    canvas.style.touchAction = "none";

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

    function onPointerDown(event) {
      sceneOrbitStartDrag(controls, canvas, props, readViewport, readSourceCamera, attachDocumentListeners, event);
    }

    function onPointerMove(event) {
      sceneOrbitMoveDrag(controls, canvas, props, readViewport, readSourceCamera, scheduleRender, event);
    }

    function finishPointerDrag(event) {
      sceneOrbitFinishDrag(controls, canvas, detachDocumentListeners, event);
    }

    function onWheel(event) {
      sceneOrbitApplyWheel(controls, readSourceCamera, scheduleRender, event);
    }

    canvas.addEventListener("pointerdown", onPointerDown);
    canvas.addEventListener("pointermove", onPointerMove);
    canvas.addEventListener("pointerup", finishPointerDrag);
    canvas.addEventListener("pointercancel", finishPointerDrag);
    canvas.addEventListener("lostpointercapture", finishPointerDrag);
    canvas.addEventListener("wheel", onWheel);

    return {
      controller: controls,
      dispose() {
        detachDocumentListeners();
        canvas.removeEventListener("pointerdown", onPointerDown);
        canvas.removeEventListener("pointermove", onPointerMove);
        canvas.removeEventListener("pointerup", finishPointerDrag);
        canvas.removeEventListener("pointercancel", finishPointerDrag);
        canvas.removeEventListener("lostpointercapture", finishPointerDrag);
        canvas.removeEventListener("wheel", onWheel);
      },
    };
  }

  window.__gosx_register_engine_factory("GoSXScene3D", async function(ctx) {
    if (!ctx.mount || typeof document.createElement !== "function") {
      console.warn("[gosx] Scene3D requires a mount element");
      return {};
    }

    const props = ctx.props || {};
    const capability = sceneCapabilityProfile(props);
    const viewportBase = sceneViewportBase(props);
    const sceneState = createSceneState(props);
    const sceneModelHydration = hydrateSceneStateModels(sceneState, props);
    const runtimeScene = ctx.runtimeMode === "shared" && Boolean(ctx.programRef);
    const lifecycle = initialSceneLifecycleState();
    const motion = initialSceneMotionState(props);

    function sceneShouldAnimate() {
      if (motion.reducedMotion) {
        return false;
      }
      if (runtimeScene || sceneBool(props.autoRotate, true)) {
        return true;
      }
      if (Array.isArray(sceneState.computeParticles) && sceneState.computeParticles.length > 0) {
        return true;
      }
      if (Array.isArray(sceneState.points) && sceneState.points.some(function(p) {
        return sceneNumber(p.spinX, 0) !== 0 || sceneNumber(p.spinY, 0) !== 0 || sceneNumber(p.spinZ, 0) !== 0;
      })) {
        return true;
      }
      return sceneStateObjects(sceneState).some(sceneObjectAnimated) ||
        sceneStateLabels(sceneState).some(sceneLabelAnimated) ||
        sceneStateSprites(sceneState).some(sceneSpriteAnimated);
    }

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
    const canvas = document.createElement("canvas");
    canvas.setAttribute("data-gosx-scene3d-canvas", "true");
    canvas.setAttribute("role", "img");
    canvas.setAttribute("aria-label", props.label || "Interactive GoSX 3D scene");
    canvas.style.maxWidth = "100%";
    canvas.style.borderRadius = "inherit";
    canvas.width = viewportBase.baseWidth;
    canvas.height = viewportBase.baseHeight;
    canvas.setAttribute("width", String(viewportBase.baseWidth));
    canvas.setAttribute("height", String(viewportBase.baseHeight));
    ctx.mount.appendChild(canvas);

    const labelLayer = document.createElement("div");
    labelLayer.setAttribute("data-gosx-scene3d-label-layer", "true");
    labelLayer.setAttribute("aria-hidden", "true");
    ctx.mount.appendChild(labelLayer);

    let viewport = applySceneViewport(ctx.mount, canvas, labelLayer, sceneViewportFromMount(ctx.mount, props, viewportBase, canvas, capability), viewportBase);

    const initialRenderer = createSceneRenderer(canvas, props, capability);
    if (!initialRenderer || !initialRenderer.renderer) {
      console.warn("[gosx] Scene3D could not acquire a renderer");
      return {
        dispose() {
          if (canvas.parentNode === ctx.mount) {
            ctx.mount.removeChild(canvas);
          }
          if (labelLayer.parentNode === ctx.mount) {
            ctx.mount.removeChild(labelLayer);
          }
        },
      };
    }
    let renderer = initialRenderer.renderer;
    applySceneRendererState(ctx.mount, renderer, initialRenderer.fallbackReason || "");
    let latestBundle = null;
    const labelLayoutCache = new Map();
    const labelElements = new Map();
    const spriteElements = new Map();
    let labelRefreshHandle = null;
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
      });
    });

    let frameHandle = null;
    let scheduledRenderHandle = null;
    let disposed = false;

    function swapRenderer(nextRenderer, fallbackReason) {
      if (!nextRenderer) {
        return false;
      }
      const previous = renderer;
      renderer = nextRenderer;
      applySceneRendererState(ctx.mount, renderer, fallbackReason);
      if (previous && previous !== renderer && typeof previous.dispose === "function") {
        previous.dispose();
      }
      return true;
    }

    function fallbackSceneRenderer(reason) {
      const ctx2d = typeof canvas.getContext === "function" ? canvas.getContext("2d") : null;
      if (!ctx2d) {
        return false;
      }
      return swapRenderer(createSceneCanvasRenderer(ctx2d, canvas), reason || "webgl-unavailable");
    }

    function restoreSceneWebGLRenderer(reason) {
      const webglPreference = sceneCapabilityWebGLPreference(props, capability);
      if (!(webglPreference === "prefer" || webglPreference === "force")) {
        return false;
      }
      const webglRenderer = createSceneWebGLRenderer(canvas, {
        antialias: capability.tier === "full" && !capability.lowPower && !capability.reducedData,
        powerPreference: capability.lowPower || capability.tier === "constrained" ? "low-power" : "high-performance",
      });
      if (!webglRenderer) {
        return false;
      }
      return swapRenderer(webglRenderer, reason || "");
    }

    function onWebGLContextLost(event) {
      if (!renderer || renderer.kind !== "webgl") {
        return;
      }
      if (event && typeof event.preventDefault === "function") {
        event.preventDefault();
      }
      fallbackSceneRenderer("webgl-context-lost");
      scheduleRender("webgl-context-lost");
    }

    function onWebGLContextRestored() {
      if (restoreSceneWebGLRenderer("")) {
        scheduleRender("webgl-context-restored");
      }
    }

    canvas.addEventListener("webglcontextlost", onWebGLContextLost);
    canvas.addEventListener("webglcontextrestored", onWebGLContextRestored);

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
    }

    function cancelScheduledRender() {
      if (scheduledRenderHandle != null) {
        cancelEngineFrame(scheduledRenderHandle);
        scheduledRenderHandle = null;
      }
    }

    function scheduleRender(reason) {
      if (disposed) {
        return;
      }
      const nextViewport = sceneViewportFromMount(ctx.mount, props, viewportBase, canvas, capability);
      if (sceneViewportChanged(viewport, nextViewport)) {
        viewport = applySceneViewport(ctx.mount, canvas, labelLayer, nextViewport, viewportBase);
      }
      if (!sceneCanRender()) {
        cancelFrame();
        cancelScheduledRender();
        return;
      }
      if (scheduledRenderHandle != null) {
        return;
      }
      scheduledRenderHandle = engineFrame(function(now) {
        scheduledRenderHandle = null;
        renderFrame(typeof now === "number" ? now : 0, reason || "refresh");
      });
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

    const sceneControlHandle = setupSceneBuiltInControls(canvas, props, function() {
      return viewport;
    }, readSceneSourceCamera, scheduleRender);
    const dragHandle = sceneControlHandle.controller
      ? { dispose() {} }
      : setupSceneDragInteractions(canvas, props, function() {
        return viewport;
      }, function() {
        return latestBundle;
      });
    const pickHandle = setupScenePickInteractions(canvas, props, function() {
      return viewport;
    }, function() {
      return latestBundle;
    }, function(detail) {
      ctx.emit("scene-interaction", detail);
    });

    const releaseViewportObserver = observeSceneViewport(ctx.mount, scheduleRender);
    const releaseCapabilityObserver = observeSceneCapability(ctx.mount, props, capability, function(reason) {
      const nextViewport = sceneViewportFromMount(ctx.mount, props, viewportBase, canvas, capability);
      if (sceneViewportChanged(viewport, nextViewport)) {
        viewport = applySceneViewport(ctx.mount, canvas, labelLayer, nextViewport, viewportBase);
      }
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
      if (!sceneCanRender()) {
        cancelFrame();
        cancelScheduledRender();
        if (labelRefreshHandle != null) {
          cancelEngineFrame(labelRefreshHandle);
          labelRefreshHandle = null;
        }
        return;
      }
      scheduleRender(reason || "lifecycle");
    });
    const releaseMotionObserver = observeSceneMotion(ctx.mount, motion, function(reason) {
      cancelFrame();
      cancelScheduledRender();
      scheduleRender(reason || "motion");
    });

    if (runtimeScene) {
      if (ctx.runtime && ctx.runtime.available()) {
        applySceneCommands(sceneState, await ctx.runtime.hydrateFromProgramRef());
      } else {
        console.warn("[gosx] Scene3D runtime requested but shared engine runtime is unavailable");
      }
    }

    function renderFrame(now) {
      if (disposed) return;
      viewport = applySceneViewport(ctx.mount, canvas, labelLayer, sceneViewportFromMount(ctx.mount, props, viewportBase, canvas, capability), viewportBase);
      if (!sceneCanRender()) {
        cancelFrame();
        return;
      }
      const timeSeconds = now / 1000;
      if (runtimeScene && ctx.runtime && typeof ctx.runtime.renderFrame === "function") {
        const runtimeBundle = ctx.runtime.renderFrame(timeSeconds, viewport.cssWidth, viewport.cssHeight);
        if (runtimeBundle) {
          const effectiveBundle = sceneBundleWithCameraOverride(
            runtimeBundle,
            sceneCurrentControlCamera(sceneControlHandle.controller, runtimeBundle.camera || sceneState.camera, sceneState._scrollCamera),
          );
          latestBundle = effectiveBundle;
          renderer.render(effectiveBundle, viewport);
          renderSceneLabels(labelLayer, effectiveBundle, labelLayoutCache, labelElements, viewport.cssWidth, viewport.cssHeight);
          renderSceneSprites(labelLayer, effectiveBundle, spriteElements, viewport.cssWidth, viewport.cssHeight);
          if (sceneWantsAnimation()) {
            frameHandle = engineFrame(renderFrame);
          }
          return;
        }
      }
      if (runtimeScene && ctx.runtime) {
        applySceneCommands(sceneState, ctx.runtime.tick());
      }
      latestBundle = createSceneRenderBundle(
        viewport.cssWidth,
        viewport.cssHeight,
        sceneState.background,
        sceneCurrentControlCamera(sceneControlHandle.controller, sceneState.camera, sceneState._scrollCamera),
        sceneStateObjects(sceneState),
        sceneStateLabels(sceneState),
        sceneStateSprites(sceneState),
        sceneStateLights(sceneState),
        sceneState.environment,
        timeSeconds,
        sceneState.points,
        sceneState.instancedMeshes,
        sceneState.computeParticles,
      );
      renderer.render(latestBundle, viewport);
      renderSceneLabels(labelLayer, latestBundle, labelLayoutCache, labelElements, viewport.cssWidth, viewport.cssHeight);
      renderSceneSprites(labelLayer, latestBundle, spriteElements, viewport.cssWidth, viewport.cssHeight);
      if (sceneWantsAnimation()) {
        frameHandle = engineFrame(renderFrame);
      }
    }

    await sceneModelHydration;
    renderFrame(0);

    ctx.emit("mounted", {
      width: viewport.cssWidth,
      height: viewport.cssHeight,
      objects: sceneStateObjects(sceneState).length,
      labels: sceneStateLabels(sceneState).length,
      sprites: sceneStateSprites(sceneState).length,
      lights: sceneStateLights(sceneState).length,
      models: sceneModels(props).length,
    });

    // Scroll-driven camera: just track progress, the animation loop picks it up.
    var scrollHandler = null;
    if (sceneState._scrollCamera) {
      sceneState._scrollCamera._progress = 0;
      sceneState._scrollCamera._smoothProgress = 0;
      scrollHandler = function() {
        var scrollMax = Math.max(1, document.documentElement.scrollHeight - window.innerHeight);
        sceneState._scrollCamera._progress = Math.pow(Math.min(1, Math.max(0, window.scrollY / scrollMax)), 0.5);
      };
      window.addEventListener("scroll", scrollHandler, { passive: true });
      scrollHandler();
    }

    return {
      applyCommands(commands) {
        applySceneCommands(sceneState, commands);
        scheduleRender("commands");
      },
      dispose() {
        disposed = true;
        if (scrollHandler) {
          window.removeEventListener("scroll", scrollHandler);
        }
        canvas.removeEventListener("webglcontextlost", onWebGLContextLost);
        canvas.removeEventListener("webglcontextrestored", onWebGLContextRestored);
        releaseViewportObserver();
        releaseCapabilityObserver();
        releaseLifecycleObserver();
        releaseMotionObserver();
        releaseTextLayoutListener();
        dragHandle.dispose();
        pickHandle.dispose();
        sceneControlHandle.dispose();
        renderer.dispose();
        cancelFrame();
        cancelScheduledRender();
        if (labelRefreshHandle != null) {
          cancelEngineFrame(labelRefreshHandle);
        }
        if (canvas.parentNode === ctx.mount) {
          ctx.mount.removeChild(canvas);
        }
        if (labelLayer.parentNode === ctx.mount) {
          ctx.mount.removeChild(labelLayer);
        }
      },
    };
  });
