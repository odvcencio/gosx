  function gosxSceneEmit(level, msg, fields) {
    try {
      if (typeof window !== "undefined" && typeof window.__gosx_emit === "function") {
        window.__gosx_emit(level, "scene3d", msg, fields || {});
      }
    } catch (_err) {
      /* telemetry must never surface to users */
    }
  }

  function createSceneRenderer(canvas, props, capability) {
    const registryResult = createSceneRendererFromRegistry(canvas, props, capability);
    if (registryResult) {
      return registryResult;
    }

    const webglPreference = sceneCapabilityWebGLPreference(props, capability);
    if (webglPreference === "prefer" || webglPreference === "force") {
      // forceWebGL must stay on the WebGL stack instead of probing WebGPU first.
      if (webglPreference !== "force" && typeof sceneWebGPUAvailable === "function" && sceneWebGPUAvailable()) {
        var gpuRenderer = createSceneWebGPURendererOrFallback(canvas);
        if (gpuRenderer) {
          return {
            renderer: gpuRenderer,
            fallbackReason: "",
          };
        }
      }
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

  function createSceneRendererFromRegistry(canvas, props, capability) {
    if (typeof sceneBackendRegistry === "undefined" || !sceneBackendRegistry || typeof sceneBackendRegistry.candidates !== "function") {
      return null;
    }
    const webglPreference = sceneCapabilityWebGLPreference(props, capability);
    const request = {
      props,
      capability,
      webgpu: webglPreference === "prefer",
      webgl: webglPreference === "prefer" || webglPreference === "force",
      webgl2: webglPreference === "prefer" || webglPreference === "force",
      canvas2d: true,
      preferWebGPU: webglPreference === "prefer",
      forceWebGL: webglPreference === "force",
    };
    const candidates = sceneBackendRegistry.candidates(request);
    for (const entry of candidates) {
      if (!entry || typeof entry.create !== "function") {
        continue;
      }
      const renderer = entry.create(canvas, props, capability);
      if (renderer) {
        return {
          renderer,
          fallbackReason: entry.kind === "canvas2d" || renderer.kind === "canvas"
            ? sceneRendererFallbackReason(props, capability, "canvas")
            : "",
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
    if (hasSkin && model && model.animation) {
      instanced.static = false;
    }
    if (model && typeof model.pickable === "boolean") {
      instanced.pickable = model.pickable;
    }
    return normalizeSceneObject(instanced, prefix);
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
      return Boolean(skin && skin.joints && skin.inverseBindMatrices);
    });
  }

  function sceneApplyModelSkinPose(record, deltaTime) {
    if (!record || !record.animationApi || !record.nodes || !record.skins) {
      return;
    }
    const animatedTransforms = record.animatedTransforms;
    if (animatedTransforms && typeof animatedTransforms.clear === "function") {
      animatedTransforms.clear();
    }
    if (record.mixer) {
      record.mixer.update(deltaTime, function(targetNode, property, value) {
        let entry = animatedTransforms.get(targetNode);
        if (!entry) {
          entry = {};
          animatedTransforms.set(targetNode, entry);
        }
        entry[property] = Array.isArray(value) ? value.slice() : Array.from(value || []);
      });
    }
    const nodeTransforms = record.animationApi.buildNodeTransforms(record.nodes, animatedTransforms, record.rootTransform);
    for (let index = 0; index < record.skins.length; index += 1) {
      const skin = record.skins[index];
      if (!skin) {
        continue;
      }
      skin.jointMatrices = record.animationApi.computeJointMatrices(skin, nodeTransforms);
    }
  }

  async function scenePrepareModelSkinPlayback(state, asset, model, skinInstances) {
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
      nodes: asset.nodes,
      skins: skinInstances,
      animatedTransforms: new Map(),
      rootTransform: sceneModelTransformMatrix(model),
      animationApi,
      mixer: null,
      animation: "",
    };

    const requestedAnimation = typeof model.animation === "string" ? model.animation.trim() : "";
    if (requestedAnimation && typeof animationApi.createMixer === "function") {
      const clips = sceneCloneModelAnimations(asset.animations);
      if (clips.length) {
        const mixer = animationApi.createMixer();
        for (let index = 0; index < clips.length; index += 1) {
          const clip = clips[index];
          mixer.addClip(clip.name, clip);
        }
        mixer.play(requestedAnimation, { loop: model.loop !== false, fadeIn: 0 });
        if (mixer.isPlaying(requestedAnimation)) {
          record.mixer = mixer;
          record.animation = requestedAnimation;
          if (!Array.isArray(state._modelAnimations)) {
            state._modelAnimations = [];
          }
          state._modelAnimations.push(record);
        }
      }
    }

    sceneApplyModelSkinPose(record, 0);
  }

  function sceneHasActiveModelAnimations(state) {
    const records = state && Array.isArray(state._modelAnimations) ? state._modelAnimations : [];
    return records.some(function(record) {
      return Boolean(record && record.mixer && record.animation && record.mixer.isPlaying(record.animation));
    });
  }

  function sceneAdvanceModelAnimations(state, deltaTime) {
    const records = state && Array.isArray(state._modelAnimations) ? state._modelAnimations : [];
    for (let index = 0; index < records.length; index += 1) {
      const record = records[index];
      if (!record || !record.mixer || !record.animation || !record.mixer.isPlaying(record.animation)) {
        continue;
      }
      sceneApplyModelSkinPose(record, deltaTime);
    }
  }

  async function hydrateSceneStateModels(state, props) {
    const models = sceneModels(props);
    state._modelAnimations = [];
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
      const skinInstances = sceneCloneModelSkins(asset.skins);
      for (let i = 0; i < asset.objects.length; i += 1) {
        const object = sceneInstantiateModelObject(asset.objects[i], model, prefix, i, skinInstances);
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
      await scenePrepareModelSkinPlayback(state, asset, model, skinInstances);
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
    const capabilityMaxDevicePixelRatio = defaultSceneMaxDevicePixelRatio(capability);
    const maxDevicePixelRatio = Math.max(
      1,
      base.explicitMaxDevicePixelRatio > 0
        ? Math.min(base.explicitMaxDevicePixelRatio, capabilityMaxDevicePixelRatio)
        : capabilityMaxDevicePixelRatio,
    );
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

  function sceneUpdateScrollCameraMetrics(scrollCamera, includeMax) {
    if (!scrollCamera) {
      return;
    }
    scrollCamera._scrollTop = sceneScrollTop();
    if (includeMax || !Number.isFinite(sceneNumber(scrollCamera._scrollMax, NaN))) {
      scrollCamera._scrollMax = sceneScrollMax();
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
    if (Math.abs(target - current) < 0.0005) {
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
    let sceneCSSAnimationUntil = 0;
    let lastModelAnimationTimeSeconds = null;

    function sceneShouldAnimate() {
      if (motion.reducedMotion) {
        return false;
      }
      if (ctx.mount && ctx.mount.__gosxScene3DCSSDynamic && Date.now() < sceneCSSAnimationUntil) {
        return true;
      }
      if (sceneHasActiveTransitions(sceneState)) {
        return true;
      }
      if (runtimeScene || sceneBool(props.autoRotate, true)) {
        return true;
      }
      if (Array.isArray(sceneState.computeParticles) && sceneState.computeParticles.length > 0) {
        return true;
      }
      if (sceneHasActiveModelAnimations(sceneState)) {
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

    const sentinelLayer = document.createElement("div");
    sentinelLayer.setAttribute("data-gosx-scene-node-layer", "true");
    sentinelLayer.setAttribute("aria-hidden", "true");
    sentinelLayer.style.position = "absolute";
    sentinelLayer.style.inset = "0";
    sentinelLayer.style.width = "0";
    sentinelLayer.style.height = "0";
    sentinelLayer.style.overflow = "visible";
    sentinelLayer.style.pointerEvents = "none";
    canvas.appendChild(sentinelLayer);

    const sceneNodeSentinels = new Map();
    ctx.mount.__gosxScene3DSentinels = sceneNodeSentinels;
    ctx.mount.__gosxScene3DCSSDynamic = false;
    ctx.mount.__gosxScene3DCSSRevision = 1;
    ctx.mount.__gosxScene3DCSSAnimationUntil = 0;

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
          if (sentinelLayer.parentNode === ctx.mount) {
            ctx.mount.removeChild(sentinelLayer);
          }
          delete ctx.mount.__gosxScene3DSentinels;
          delete ctx.mount.__gosxScene3DCSSDynamic;
          delete ctx.mount.__gosxScene3DCSSRevision;
          delete ctx.mount.__gosxScene3DCSSAnimationUntil;
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
      });
    });

    let frameHandle = null;
    let scheduledRenderHandle = null;
    let disposed = false;

    // Idle context release: when the scene is not renderable (tab hidden
    // or scrolled off-viewport) for longer than IDLE_CONTEXT_RELEASE_MS,
    // voluntarily lose the WebGL context via WEBGL_lose_context. This
    // releases GPU virtual memory that Firefox accumulates for long-lived
    // WebGL contexts — measured at 88-116 GB virtual on a 32 GB machine
    // when a tab with an autoRotate scene stays open for hours. The
    // existing webglcontextrestored handler restores the renderer when
    // the scene becomes renderable again.
    const IDLE_CONTEXT_RELEASE_MS = 30000;
    let idleContextTimer = null;
    let contextVoluntarilyLost = false;

    function scheduleIdleContextRelease() {
      clearIdleContextRelease();
      if (disposed || contextVoluntarilyLost) return;
      idleContextTimer = setTimeout(function() {
        idleContextTimer = null;
        if (disposed || sceneCanRender()) return;
        if (!renderer || renderer.kind !== "webgl") return;
        try {
          const gl = canvas.getContext("webgl2") || canvas.getContext("webgl");
          if (gl) {
            const ext = gl.getExtension("WEBGL_lose_context");
            if (ext) {
              contextVoluntarilyLost = true;
              ext.loseContext();
            }
          }
        } catch (_e) { /* context may already be lost */ }
      }, IDLE_CONTEXT_RELEASE_MS);
    }

    function clearIdleContextRelease() {
      if (idleContextTimer != null) {
        clearTimeout(idleContextTimer);
        idleContextTimer = null;
      }
    }

    function restoreVoluntarilyLostContext() {
      if (!contextVoluntarilyLost) return;
      contextVoluntarilyLost = false;
      try {
        const gl = canvas.getContext("webgl2") || canvas.getContext("webgl");
        if (gl) {
          const ext = gl.getExtension("WEBGL_lose_context");
          if (ext) {
            ext.restoreContext();
          }
        }
      } catch (_e) { /* let the browser handle it */ }
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

    // Guarded animation-frame scheduler. The animation loop was previously
    // just `frameHandle = engineFrame(renderFrame)` at the end of every
    // renderFrame call site — no guard against a second chain starting
    // in parallel. When scheduleRender fires from a scroll event (or any
    // other observer) its rAF callback calls renderFrame, which then
    // scheduled ANOTHER rAF via frameHandle, starting a parallel loop.
    // Each additional scheduleRender kick started another independent
    // chain, and they never merged back.
    //
    // Firefox exposes this dramatically: under programmatic scroll on a
    // scene with active data-kinetic reveal animations, scroll events fire
    // many times per frame, each starting a new chain, and rAF queues
    // depth grows until the main thread is processing 20+ renderFrames per
    // display-refresh tick — measured at 956/s during a 2 s scroll, with
    // the matching rAF gap growing to 51 ms p50 (10-20 fps). Chrome hides
    // some of this via scroll event coalescing but still doubles up (2
    // renderFrames per display tick), so Chrome gets a free speedup too.
    //
    // Fix: schedule via this guarded helper, null frameHandle inside the
    // rAF callback so the next call-site can schedule exactly one chain
    // advance, and have scheduleRender cancel any in-flight frameHandle
    // before calling renderFrame so the eager refresh path doesn't leak
    // into a duplicate chain.
    function scheduleNextAnimationFrame() {
      if (disposed) return;
      if (frameHandle != null) return;
      if (!sceneWantsAnimation()) return;
      frameHandle = engineFrame(function(now) {
        frameHandle = null;
        renderFrame(now);
      });
    }

    let sceneRendererRecentlySwapped = false;
    let sceneRendererLastSwapReason = "";

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
      sceneRendererRecentlySwapped = true;
      sceneRendererLastSwapReason = fallbackReason || "";
      gosxSceneEmit("info", "renderer-swap", {
        from: previous && previous.kind ? previous.kind : "",
        to: nextRenderer.kind || "",
        reason: fallbackReason || "",
      });
      return true;
    }

    function fallbackSceneRenderer(reason) {
      const ctx2d = typeof canvas.getContext === "function" ? canvas.getContext("2d") : null;
      if (!ctx2d) {
        gosxSceneEmit("warn", "renderer-fallback-unavailable", { reason: reason || "" });
        return false;
      }
      return swapRenderer(createSceneCanvasRenderer(ctx2d, canvas), reason || "webgl-unavailable");
    }

    function renderLatestSceneBundle(reason) {
      if (disposed || !latestBundle || !renderer || typeof renderer.render !== "function" || !sceneCanRender()) {
        return false;
      }
      recordScenePerfCounter("render:" + (reason || "restore"));
      syncSceneNodeSentinels(latestBundle);
      renderer.render(latestBundle, viewport);
      maybeEmitRenderEmpty(latestBundle);
      renderSceneLabels(labelLayer, latestBundle, labelLayoutCache, labelElements, viewport.cssWidth, viewport.cssHeight);
      renderSceneSprites(labelLayer, latestBundle, spriteElements, viewport.cssWidth, viewport.cssHeight);
      return true;
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
      if (!swapRenderer(webglRenderer, reason || "")) {
        return false;
      }
      renderLatestSceneBundle(reason || "webgl-restore");
      return true;
    }

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
      const swapped = fallbackSceneRenderer("webgl-context-lost");
      scheduleRender("webgl-context-lost");
      if (!swapped) {
        gosxSceneEmit("warn", "webgl-context-lost-no-fallback", {});
      }
    }

    function onWebGLContextRestored() {
      const voluntary = contextVoluntarilyLost === true;
      contextVoluntarilyLost = false;
      const swapped = restoreSceneWebGLRenderer("");
      gosxSceneEmit(swapped ? "info" : "warn", "webgl-context-restored", {
        swapped: swapped,
        voluntary: voluntary,
      });
      if (swapped) {
        viewportDirty = true;
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
      recordScenePerfCounter("schedule:" + (reason || "refresh"));
      if (scheduledRenderHandle != null) {
        recordScenePerfCounter("coalesced:" + (reason || "refresh"));
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
          return;
        }
        // Cancel any in-flight animation-chain rAF before calling
        // renderFrame directly from this eager-refresh path. Without
        // this, the animation chain's pending rAF from the previous
        // frame fires alongside this one, starting a parallel chain
        // every time scheduleRender is hit. On scroll-heavy pages
        // those parallel chains compound into the duplicate-rAF
        // storm that was visible on Firefox as 20 renderFrame calls
        // per display tick. cancelFrame clears frameHandle; the
        // subsequent renderFrame call ends with scheduleNextAnimationFrame
        // which schedules exactly one fresh chain advance.
        cancelFrame();
        renderFrame(typeof now === "number" ? now : 0, reason || "refresh");
      });
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
    const sceneHubListener = function(event) {
      if (disposed) {
        return;
      }
      const detail = event && event.detail && typeof event.detail === "object" ? event.detail : null;
      if (!detail || typeof detail.event !== "string") {
        return;
      }
      if (sceneApplyLiveEvent(sceneState, detail.event, detail.data, motion.reducedMotion, sceneNowMilliseconds())) {
        scheduleRender("hub-event");
      }
    };
    document.addEventListener("gosx:hub:event", sceneHubListener);

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

    if (runtimeScene) {
      if (ctx.runtime && ctx.runtime.available()) {
        applySceneCommands(sceneState, await ctx.runtime.hydrateFromProgramRef());
      } else {
        console.warn("[gosx] Scene3D runtime requested but shared engine runtime is unavailable");
      }
    }

    function renderFrame(now, reason) {
      if (disposed) return;
      recordScenePerfCounter("render:" + (reason || "animation"));
      // Only re-measure the viewport when something has actually
      // invalidated it. Static frames (the common case during continuous
      // animation without DOM changes) reuse the cached `viewport` and
      // skip the 4 getBoundingClientRect layout flushes that used to
      // run every frame.
      if (viewportDirty) {
        const nextViewport = sceneViewportFromMount(ctx.mount, props, viewportBase, canvas, capability);
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
      sceneAdvanceModelAnimations(sceneState, modelAnimationDelta);
      if (runtimeScene && ctx.runtime && typeof ctx.runtime.renderFrame === "function") {
        const runtimeBundle = ctx.runtime.renderFrame(timeSeconds, viewport.cssWidth, viewport.cssHeight);
        if (runtimeBundle) {
          const effectiveBundle = sceneBundleWithCameraOverride(
            runtimeBundle,
            sceneCurrentControlCamera(sceneControlHandle.controller, runtimeBundle.camera || sceneState.camera, sceneState._scrollCamera),
          );
          latestBundle = effectiveBundle;
          syncSceneNodeSentinels(effectiveBundle);
          renderer.render(effectiveBundle, viewport);
          renderSceneLabels(labelLayer, effectiveBundle, labelLayoutCache, labelElements, viewport.cssWidth, viewport.cssHeight);
          renderSceneSprites(labelLayer, effectiveBundle, spriteElements, viewport.cssWidth, viewport.cssHeight);
          scheduleNextAnimationFrame();
          return;
        }
      }
      if (runtimeScene && ctx.runtime) {
        applySceneCommands(sceneState, ctx.runtime.tick());
      }
      sceneAdvanceTransitions(sceneState, now);
      // LOD: swap vertex data based on camera distance before building render bundle.
      if (typeof sceneApplyLOD === "function" && props.compression && props.compression.lod) {
        var cam = sceneCurrentControlCamera(sceneControlHandle.controller, sceneState.camera, sceneState._scrollCamera);
        var camX = cam.x || 0, camY = cam.y || 0, camZ = cam.z || 0;
        for (var li = 0; li < sceneState.points.length; li++) {
          sceneApplyLOD(sceneState.points[li], camX, camY, camZ);
        }
      }
      latestBundle = createSceneRenderBundle(
        viewport.cssWidth,
        viewport.cssHeight,
        sceneState.background,
        sceneCurrentControlCamera(sceneControlHandle.controller, sceneState.camera, sceneState._scrollCamera),
        sceneStateObjectsWithMaterials(sceneState),
        sceneStateLabels(sceneState),
        sceneStateSprites(sceneState),
        sceneStateLights(sceneState),
        sceneState.environment,
        timeSeconds,
        sceneStatePointsWithMaterials(sceneState),
        sceneState.instancedMeshes,
        sceneState.computeParticles,
        sceneState.postEffects,
      );
      syncSceneNodeSentinels(latestBundle);
      renderer.render(latestBundle, viewport);
      maybeEmitRenderEmpty(latestBundle);
      renderSceneLabels(labelLayer, latestBundle, labelLayoutCache, labelElements, viewport.cssWidth, viewport.cssHeight);
      renderSceneSprites(labelLayer, latestBundle, spriteElements, viewport.cssWidth, viewport.cssHeight);
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
      if (bundleVerts > 0 || worldVerts > 0 || surfaceCount > 0) {
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
      var upgradeTimer = typeof requestIdleCallback === "function" ? requestIdleCallback : setTimeout;
      upgradeTimer(function() {
        sceneUpgradeProgressive(props);
        // Force a re-render with upgraded data
        if (sceneWantsAnimation()) {
          // Animation loop will pick it up
        } else {
          renderFrame(0);
        }
      });
    }

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
    var visualViewportScrollHandler = null;
    if (sceneState._scrollCamera) {
      sceneState._scrollCamera._progress = 0;
      sceneState._scrollCamera._smoothProgress = 0;
      sceneUpdateScrollCameraMetrics(sceneState._scrollCamera, true);
      scrollHandler = function() {
        sceneUpdateScrollCameraMetrics(sceneState._scrollCamera, false);
        if (!sceneWantsAnimation()) {
          scheduleRender("scroll");
        }
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
          sceneUpdateScrollCameraMetrics(sceneState._scrollCamera, true);
          if (!sceneWantsAnimation()) {
            scheduleRender("visual-viewport");
          }
        };
        window.visualViewport.addEventListener("scroll", visualViewportScrollHandler, { passive: true });
        window.visualViewport.addEventListener("resize", visualViewportScrollHandler, { passive: true });
      }
      sceneAdvanceScrollCamera(sceneState._scrollCamera);
    }

    return {
      applyCommands(commands) {
        applySceneCommands(sceneState, commands);
        scheduleRender("commands");
      },
      dispose() {
        disposed = true;
        clearIdleContextRelease();
        if (scrollHandler) {
          window.removeEventListener("scroll", scrollHandler);
        }
        if (visualViewportScrollHandler && window.visualViewport && typeof window.visualViewport.removeEventListener === "function") {
          window.visualViewport.removeEventListener("scroll", visualViewportScrollHandler);
          window.visualViewport.removeEventListener("resize", visualViewportScrollHandler);
        }
        canvas.removeEventListener("webglcontextlost", onWebGLContextLost);
        canvas.removeEventListener("webglcontextrestored", onWebGLContextRestored);
        document.removeEventListener("gosx:hub:event", sceneHubListener);
        releaseViewportObserver();
        releaseCapabilityObserver();
        releaseLifecycleObserver();
        releaseMotionObserver();
        releaseSceneCSSObserver();
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
        if (sentinelLayer.parentNode === ctx.mount) {
          ctx.mount.removeChild(sentinelLayer);
        }
        delete ctx.mount.__gosxScene3DSentinels;
        delete ctx.mount.__gosxScene3DCSSDynamic;
        delete ctx.mount.__gosxScene3DCSSRevision;
        delete ctx.mount.__gosxScene3DCSSAnimationUntil;
      },
    };
  });
