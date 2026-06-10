  // Strict SceneIR validation for dev/test/CLI-style browser tooling. This
  // module is intentionally separate from the lightweight runtime validator so
  // production mount paths can keep their low-cost checks.
  (function() {
    function validateSceneIRStrict(ir, options) {
      var opts = options || {};
      var diagnostics = [];
      var seenIDs = Object.create(null);
      var knownIDs = Object.create(null);
      var htmlTargets = [];
      var strict = opts.strict !== false;
      var schema = typeof SCENE_IR_SCHEMA === "string" ? SCENE_IR_SCHEMA : "gosx.scene3d.ir.v1";
      if (!ir || typeof ir !== "object") {
        pushSceneStrictDiagnostic(diagnostics, "fatal", "scene.schema.invalid_json", "SceneIR must be an object");
        return { valid: false, diagnostics: diagnostics };
      }
      if (ir.schema && ir.schema !== schema) {
        pushSceneStrictDiagnostic(diagnostics, strict ? "error" : "warn", "scene.schema.version_mismatch", "SceneIR schema does not match the current runtime schema", "schema", "", { got: ir.schema, want: schema });
      }
      validateSceneStrictOptionalInteger(diagnostics, ir.postFXMaxPixels, "postFXMaxPixels", "scene.postfx.invalid_max_pixels", "postFXMaxPixels must not be negative", "", true);
      validateSceneStrictOptionalInteger(diagnostics, ir.shadowMaxPixels, "shadowMaxPixels", "scene.shadow.invalid_max_pixels", "shadowMaxPixels must not be negative", "", true);

      var objects = sceneStrictTopLevelArray(diagnostics, ir, "objects");
      var models = sceneStrictTopLevelArray(diagnostics, ir, "models");
      var points = sceneStrictTopLevelArray(diagnostics, ir, "points");
      var instancedMeshes = sceneStrictTopLevelArray(diagnostics, ir, "instancedMeshes");
      var instancedGLBMeshes = sceneStrictTopLevelArray(diagnostics, ir, "instancedGLBMeshes");
      var computeParticles = sceneStrictTopLevelArray(diagnostics, ir, "computeParticles");
      var animations = sceneStrictTopLevelArray(diagnostics, ir, "animations");
      var labels = sceneStrictTopLevelArray(diagnostics, ir, "labels");
      var sprites = sceneStrictTopLevelArray(diagnostics, ir, "sprites");
      var html = sceneStrictTopLevelArray(diagnostics, ir, "html");
      var lights = sceneStrictTopLevelArray(diagnostics, ir, "lights");
      var postEffects = sceneStrictTopLevelArray(diagnostics, ir, "postEffects");

      objects.forEach(function(object, index) {
        var path = "objects[" + index + "]";
        checkSceneStrictID(diagnostics, seenIDs, knownIDs, object && object.id, path + ".id", strict || !!(object && object.pickable));
        validateSceneStrictPrimitive(diagnostics, object || {}, path);
        validateSceneStrictMaterialScalars(diagnostics, object || {}, path);
        validateSceneStrictLiveFields(diagnostics, object && object.live, path, object && object.id);
      });
      models.forEach(function(model, index) {
        var path = "models[" + index + "]";
        checkSceneStrictID(diagnostics, seenIDs, knownIDs, model && model.id, path + ".id", strict || !!(model && model.pickable));
        validateSceneStrictPrimitive(diagnostics, model || {}, path);
        validateSceneStrictMaterialScalars(diagnostics, model || {}, path);
        validateSceneStrictNonNegativeScalars(diagnostics, model || {}, path, ["animationSpeed", "animationWeight", "animationFadeInMS", "animationFadeOutMS"], "scene.animation.invalid_parameter", "Animation parameter must be finite and non-negative");
        validateSceneStrictLiveFields(diagnostics, model && model.live, path, model && model.id);
        if (!sceneStrictString(model && model.src)) {
          pushSceneStrictDiagnostic(diagnostics, "error", "scene.asset.missing", "Model scene record requires src", path + ".src", model && model.id);
        }
      });
      points.forEach(function(entry, index) {
        validateSceneStrictPoints(diagnostics, seenIDs, knownIDs, entry || {}, "points[" + index + "]");
      });
      instancedMeshes.forEach(function(mesh, index) {
        validateSceneStrictInstancedMesh(diagnostics, seenIDs, knownIDs, mesh || {}, "instancedMeshes[" + index + "]");
      });
      instancedGLBMeshes.forEach(function(mesh, index) {
        validateSceneStrictInstancedGLBMesh(diagnostics, seenIDs, knownIDs, mesh || {}, "instancedGLBMeshes[" + index + "]");
      });
      computeParticles.forEach(function(particles, index) {
        validateSceneStrictComputeParticles(diagnostics, seenIDs, knownIDs, particles || {}, "computeParticles[" + index + "]");
      });
      animations.forEach(function(animation, index) {
        validateSceneStrictAnimation(diagnostics, seenIDs, knownIDs, animation || {}, "animations[" + index + "]");
      });
      labels.forEach(function(label, index) {
        var path = "labels[" + index + "]";
        checkSceneStrictID(diagnostics, seenIDs, knownIDs, label && label.id, path + ".id", true);
        validateSceneStrictLiveFields(diagnostics, label && label.live, path, label && label.id);
      });
      sprites.forEach(function(sprite, index) {
        var path = "sprites[" + index + "]";
        checkSceneStrictID(diagnostics, seenIDs, knownIDs, sprite && sprite.id, path + ".id", true);
        if (!sceneStrictString(sprite && sprite.src)) {
          pushSceneStrictDiagnostic(diagnostics, "warn", "scene.asset.missing", "Sprite has no src", path + ".src", sprite && sprite.id);
        }
        validateSceneStrictLiveFields(diagnostics, sprite && sprite.live, path, sprite && sprite.id);
      });
      html.forEach(function(entry, index) {
        validateSceneStrictHTML(diagnostics, seenIDs, knownIDs, htmlTargets, entry || {}, "html[" + index + "]", opts);
      });
      lights.forEach(function(light, index) {
        validateSceneStrictLight(diagnostics, seenIDs, knownIDs, light || {}, "lights[" + index + "]");
      });
      postEffects.forEach(function(effect, index) {
        validateSceneStrictPostEffect(diagnostics, effect || {}, "postEffects[" + index + "]");
      });
      htmlTargets.forEach(function(target) {
        if (target.id && !knownIDs[target.id]) {
          pushSceneStrictDiagnostic(diagnostics, "warn", "scene.html.target_missing", "HTML target does not match a known SceneIR record ID", target.path, target.ownerID, { target: target.id });
        }
      });
      return {
        valid: diagnostics.every(function(diag) { return diag.severity !== "error" && diag.severity !== "fatal"; }),
        diagnostics: diagnostics
      };
    }

    function validateSceneStrictPrimitive(diagnostics, object, path) {
      ["size", "width", "height", "depth", "radius", "radiusTop", "radiusBottom", "tube"].forEach(function(name) {
        validateSceneStrictOptionalNumber(diagnostics, object[name], path + "." + name, object.id, "scene.primitive.non_finite", "Primitive parameter must be finite", "scene.primitive.invalid_parameter", "Primitive parameter must not be negative");
      });
      ["segments", "radialSegments", "tubularSegments"].forEach(function(name) {
        var value = object[name];
        if (value == null) {
          return;
        }
        if (!sceneStrictIsNonNegativeInteger(value)) {
          pushSceneStrictDiagnostic(diagnostics, "error", "scene.primitive.invalid_segments", "Primitive segment count must be a non-negative integer", path + "." + name, object.id, { value: value });
        }
      });
      if (sceneStrictContains(object.kind, "torus") && sceneStrictNumber(object.tube, 0) > 0 && sceneStrictNumber(object.radius, 0) > 0 && object.tube >= object.radius) {
        pushSceneStrictDiagnostic(diagnostics, "warn", "scene.primitive.torus_tube_large", "Torus tube is greater than or equal to radius; mesh may self-intersect", path + ".tube", object.id, { radius: object.radius, tube: object.tube });
      }
    }

    function validateSceneStrictMaterialScalars(diagnostics, object, path) {
      ["roughness", "metalness", "clearcoat", "sheen", "transmission", "iridescence", "anisotropy"].forEach(function(name) {
        var value = object[name];
        if (value != null && !sceneStrictIsFiniteNumber(value)) {
          pushSceneStrictDiagnostic(diagnostics, "error", "scene.material.non_finite", "Material scalar must be finite", path + "." + name, object.id);
        }
      });
    }

    function validateSceneStrictPoints(diagnostics, seenIDs, knownIDs, entry, path) {
      checkSceneStrictID(diagnostics, seenIDs, knownIDs, entry.id, path + ".id", true);
      validateSceneStrictCount(diagnostics, entry.count, path + ".count", entry.id, "scene.points.invalid_count", "Point layer count must be a non-negative integer");
      var count = sceneStrictCount(entry.count);
      var positionStride = entry.positionStride == null ? 3 : entry.positionStride;
      if (!sceneStrictIsNonNegativeInteger(positionStride)) {
        pushSceneStrictDiagnostic(diagnostics, "error", "scene.points.invalid_stride", "Point positionStride must be a non-negative integer", path + ".positionStride", entry.id, { value: entry.positionStride });
      } else if (positionStride > 0 && positionStride !== 3) {
        pushSceneStrictDiagnostic(diagnostics, "warn", "scene.points.unexpected_stride", "Point positions normally use vec3 stride", path + ".positionStride", entry.id, { value: positionStride });
      }
      validateSceneStrictNumericArray(diagnostics, entry.positions, path + ".positions", entry.id, "scene.points.array_non_finite", "Point positions must be finite");
      validateSceneStrictNumericArray(diagnostics, entry.sizes, path + ".sizes", entry.id, "scene.points.array_non_finite", "Point sizes must be finite");
      validateSceneStrictPointsColorArray(diagnostics, entry.colors, count, path + ".colors", entry.id);
      var rawPositions = sceneStrictArrayLikeLength(entry.positions);
      var compressedPositions = validateSceneStrictCompressedArray(diagnostics, entry.compressedPositions, path + ".compressedPositions", entry.id, true);
      validateSceneStrictCompressedArray(diagnostics, entry.previewPositions, path + ".previewPositions", entry.id, true);
      validateSceneStrictCompressedArray(diagnostics, entry.compressedSizes, path + ".compressedSizes", entry.id, true);
      validateSceneStrictCompressedArray(diagnostics, entry.previewSizes, path + ".previewSizes", entry.id, true);
      if (rawPositions > 0 && rawPositions % 3 !== 0) {
        pushSceneStrictDiagnostic(diagnostics, "error", "scene.points.array_stride", "Point positions must use x/y/z triples", path + ".positions", entry.id, { values: rawPositions });
      }
      if (count > 0 && rawPositions > 0 && rawPositions < count * 3) {
        pushSceneStrictDiagnostic(diagnostics, "error", "scene.points.count_mismatch", "Point count exceeds position triples", path + ".positions", entry.id, { count: count, positions: rawPositions });
      }
      if (count > 0 && rawPositions === 0 && compressedPositions === 0) {
        pushSceneStrictDiagnostic(diagnostics, "error", "scene.points.positions_missing", "Point layer count requires positions or compressedPositions", path + ".positions", entry.id);
      }
      if (compressedPositions > 0) {
        var stride = sceneStrictIsNonNegativeInteger(positionStride) && positionStride > 0 ? positionStride : 3;
        if (compressedPositions % stride !== 0) {
          pushSceneStrictDiagnostic(diagnostics, "error", "scene.points.compressed_stride", "Compressed point positions must align to positionStride", path + ".compressedPositions", entry.id, { values: compressedPositions, positionStride: stride });
        }
        if (count > 0 && compressedPositions < count * stride) {
          pushSceneStrictDiagnostic(diagnostics, "error", "scene.points.count_mismatch", "Point count exceeds compressed position values", path + ".compressedPositions", entry.id, { count: count, values: compressedPositions });
        }
      }
      if (count > 0 && sceneStrictArrayLikeLength(entry.sizes) > 0 && sceneStrictArrayLikeLength(entry.sizes) < count) {
        pushSceneStrictDiagnostic(diagnostics, "error", "scene.points.count_mismatch", "Point count exceeds size values", path + ".sizes", entry.id, { count: count, sizes: sceneStrictArrayLikeLength(entry.sizes) });
      }
      validateSceneStrictNonNegativeScalars(diagnostics, entry, path, ["size", "minPixelSize", "maxPixelSize"], "scene.points.invalid_parameter", "Point scalar must be finite and non-negative");
      validateSceneStrictLiveFields(diagnostics, entry.live, path, entry.id);
    }

    function validateSceneStrictInstancedMesh(diagnostics, seenIDs, knownIDs, mesh, path) {
      checkSceneStrictID(diagnostics, seenIDs, knownIDs, mesh.id, path + ".id", true);
      validateSceneStrictCount(diagnostics, mesh.count != null ? mesh.count : mesh.instanceCount, path + ".count", mesh.id, "scene.instances.invalid_count", "Instanced mesh count must be a non-negative integer");
      validateSceneStrictPrimitive(diagnostics, mesh, path);
      validateSceneStrictMaterialScalars(diagnostics, mesh, path);
      var count = sceneStrictCount(mesh.count != null ? mesh.count : mesh.instanceCount);
      var transforms = sceneStrictArrayLikeLength(mesh.transforms);
      var compressedTransforms = validateSceneStrictCompressedArray(diagnostics, mesh.compressedTransforms, path + ".compressedTransforms", mesh.id, true);
      validateSceneStrictCompressedArray(diagnostics, mesh.previewTransforms, path + ".previewTransforms", mesh.id, true);
      validateSceneStrictNumericArray(diagnostics, mesh.transforms, path + ".transforms", mesh.id, "scene.instances.array_non_finite", "Instanced mesh transforms must be finite");
      if (transforms > 0 && transforms % 16 !== 0) {
        pushSceneStrictDiagnostic(diagnostics, "error", "scene.instances.invalid_transforms", "Instanced mesh transforms must be 4x4 matrices", path + ".transforms", mesh.id, { values: transforms });
      }
      if (count > 0 && transforms > 0 && transforms / 16 !== count) {
        pushSceneStrictDiagnostic(diagnostics, "error", "scene.instances.count_mismatch", "Instanced mesh count does not match transform matrix count", path + ".transforms", mesh.id, { count: count, matrices: transforms / 16 });
      }
      if (compressedTransforms > 0 && compressedTransforms % 16 !== 0) {
        pushSceneStrictDiagnostic(diagnostics, "error", "scene.instances.invalid_transforms", "Compressed instanced mesh transforms must be 4x4 matrices", path + ".compressedTransforms", mesh.id, { values: compressedTransforms });
      }
      if (count > 0 && compressedTransforms > 0 && compressedTransforms / 16 !== count) {
        pushSceneStrictDiagnostic(diagnostics, "error", "scene.instances.count_mismatch", "Instanced mesh count does not match compressed transform matrix count", path + ".compressedTransforms", mesh.id, { count: count, matrices: compressedTransforms / 16 });
      }
      if (count > 0 && transforms === 0 && compressedTransforms === 0) {
        pushSceneStrictDiagnostic(diagnostics, "error", "scene.instances.transforms_missing", "Instanced mesh count requires transforms or compressedTransforms", path + ".transforms", mesh.id);
      }
      validateSceneStrictInstanceColors(diagnostics, mesh.colors, count, path + ".colors", mesh.id);
      validateSceneStrictAttributeArrays(diagnostics, mesh.attributes, count, path + ".attributes", mesh.id);
      validateSceneStrictLiveFields(diagnostics, mesh.live, path, mesh.id);
    }

    function validateSceneStrictInstancedGLBMesh(diagnostics, seenIDs, knownIDs, mesh, path) {
      checkSceneStrictID(diagnostics, seenIDs, knownIDs, mesh.id, path + ".id", true);
      validateSceneStrictMaterialScalars(diagnostics, mesh, path);
      if (!sceneStrictString(mesh.src)) {
        pushSceneStrictDiagnostic(diagnostics, "error", "scene.asset.missing", "Instanced GLB mesh requires src", path + ".src", mesh.id);
      }
      if (mesh.instances != null && !Array.isArray(mesh.instances)) {
        pushSceneStrictDiagnostic(diagnostics, "error", "scene.instances.invalid_instances", "Instanced GLB mesh instances must be an array", path + ".instances", mesh.id);
        return;
      }
      var instances = Array.isArray(mesh.instances) ? mesh.instances : [];
      if (instances.length === 0) {
        pushSceneStrictDiagnostic(diagnostics, "warn", "scene.instances.empty", "Instanced GLB mesh has no instances", path + ".instances", mesh.id);
      }
      instances.forEach(function(instance, index) {
        var instancePath = path + ".instances[" + index + "]";
        checkSceneStrictID(diagnostics, seenIDs, knownIDs, instance && instance.id, instancePath + ".id", false);
        validateSceneStrictFiniteScalars(diagnostics, instance || {}, instancePath, ["x", "y", "z", "scaleX", "scaleY", "scaleZ", "rotationX", "rotationY", "rotationZ"], "scene.instances.non_finite", "Instance transform scalar must be finite", mesh.id);
      });
    }

    function validateSceneStrictComputeParticles(diagnostics, seenIDs, knownIDs, particles, path) {
      checkSceneStrictID(diagnostics, seenIDs, knownIDs, particles.id, path + ".id", true);
      validateSceneStrictCount(diagnostics, particles.count, path + ".count", particles.id, "scene.particles.invalid_count", "Compute particle count must be a non-negative integer");
      validateSceneStrictOptionalNumber(diagnostics, particles.bounds, path + ".bounds", particles.id, "scene.particles.invalid_bounds", "Compute particle bounds must be finite", "scene.particles.invalid_bounds", "Compute particle bounds must not be negative");
      validateSceneStrictFiniteScalars(diagnostics, particles.emitter || {}, path + ".emitter", ["x", "y", "z", "rotationX", "rotationY", "rotationZ", "spinX", "spinY", "spinZ", "wind", "scatter"], "scene.particles.non_finite", "Compute particle emitter scalar must be finite", particles.id);
      validateSceneStrictNonNegativeScalars(diagnostics, particles.emitter || {}, path + ".emitter", ["radius", "rate", "lifetime"], "scene.particles.invalid_parameter", "Compute particle emitter scalar must be finite and non-negative");
      if (particles.emitter && particles.emitter.arms != null && !sceneStrictIsNonNegativeInteger(particles.emitter.arms)) {
        pushSceneStrictDiagnostic(diagnostics, "error", "scene.particles.invalid_parameter", "Compute particle emitter arms must be a non-negative integer", path + ".emitter.arms", particles.id, { value: particles.emitter.arms });
      }
      validateSceneStrictNonNegativeScalars(diagnostics, particles.material || {}, path + ".material", ["size", "sizeEnd", "opacity", "opacityEnd"], "scene.particles.invalid_parameter", "Compute particle material scalar must be finite and non-negative");
      (Array.isArray(particles.forces) ? particles.forces : []).forEach(function(force, index) {
        validateSceneStrictFiniteScalars(diagnostics, force || {}, path + ".forces[" + index + "]", ["strength", "x", "y", "z", "frequency"], "scene.particles.non_finite", "Compute particle force scalar must be finite", particles.id);
      });
      if (particles.computeWGSL != null && typeof particles.computeWGSL !== "string") {
        pushSceneStrictDiagnostic(diagnostics, "warn", "scene.particles.invalid_compute_wgsl", "Compute particle computeWGSL must be a string when present", path + ".computeWGSL", particles.id, { value: particles.computeWGSL });
      }
      if (particles.computeEntry != null && typeof particles.computeEntry !== "string") {
        pushSceneStrictDiagnostic(diagnostics, "warn", "scene.particles.invalid_compute_entry", "Compute particle computeEntry must be a string when present", path + ".computeEntry", particles.id, { value: particles.computeEntry });
      }
      if (particles.computeBackend != null && typeof particles.computeBackend !== "string") {
        pushSceneStrictDiagnostic(diagnostics, "warn", "scene.particles.invalid_compute_backend", "Compute particle computeBackend must be a string when present", path + ".computeBackend", particles.id, { value: particles.computeBackend });
      }
      validateSceneStrictLiveFields(diagnostics, particles.live, path, particles.id);
    }

    function validateSceneStrictAnimation(diagnostics, seenIDs, knownIDs, animation, path) {
      checkSceneStrictID(diagnostics, seenIDs, knownIDs, animation.name, path + ".name", true);
      validateSceneStrictOptionalNumber(diagnostics, animation.duration, path + ".duration", animation.name, "scene.animation.invalid_duration", "Animation duration must be finite", "scene.animation.invalid_duration", "Animation duration must not be negative");
      if (!Array.isArray(animation.channels)) {
        pushSceneStrictDiagnostic(diagnostics, "error", "scene.animation.invalid_channels", "Animation channels must be an array", path + ".channels", animation.name);
        return;
      }
      if (animation.channels.length === 0) {
        pushSceneStrictDiagnostic(diagnostics, "warn", "scene.animation.empty", "Animation has no channels", path + ".channels", animation.name);
      }
      animation.channels.forEach(function(channel, index) {
        validateSceneStrictAnimationChannel(diagnostics, channel || {}, path + ".channels[" + index + "]", animation.name);
      });
    }

    function validateSceneStrictAnimationChannel(diagnostics, channel, path, id) {
      if (!sceneStrictString(channel.property)) {
        pushSceneStrictDiagnostic(diagnostics, "error", "scene.animation.channel_missing", "Animation channel requires property", path + ".property", id);
      }
      if (channel.targetID == null && channel.targetNode == null) {
        pushSceneStrictDiagnostic(diagnostics, "error", "scene.animation.channel_missing", "Animation channel requires targetID or targetNode", path, id);
      }
      if (channel.targetNode != null && !sceneStrictIsNonNegativeInteger(channel.targetNode)) {
        pushSceneStrictDiagnostic(diagnostics, "error", "scene.animation.invalid_target", "Animation targetNode must be a non-negative integer", path + ".targetNode", id, { value: channel.targetNode });
      }
      validateSceneStrictNumericArray(diagnostics, channel.times, path + ".times", id, "scene.animation.invalid_times", "Animation times must be finite");
      validateSceneStrictNumericArray(diagnostics, channel.values, path + ".values", id, "scene.animation.invalid_values", "Animation values must be finite");
      var timeCount = sceneStrictArrayLikeLength(channel.times);
      var valueCount = sceneStrictArrayLikeLength(channel.values);
      var compressedTimeCount = validateSceneStrictCompressedArray(diagnostics, channel.compressedTimes, path + ".compressedTimes", id, true);
      var compressedValueCount = validateSceneStrictCompressedArray(diagnostics, channel.compressedValues, path + ".compressedValues", id, true);
      validateSceneStrictCompressedArray(diagnostics, channel.previewTimes, path + ".previewTimes", id, true);
      validateSceneStrictCompressedArray(diagnostics, channel.previewValues, path + ".previewValues", id, true);
      if (timeCount === 0 && compressedTimeCount === 0) {
        pushSceneStrictDiagnostic(diagnostics, "error", "scene.animation.times_missing", "Animation channel requires times or compressedTimes", path + ".times", id);
      }
      if (valueCount === 0 && compressedValueCount === 0) {
        pushSceneStrictDiagnostic(diagnostics, "error", "scene.animation.values_missing", "Animation channel requires values or compressedValues", path + ".values", id);
      }
      if (timeCount > 0 && !sceneStrictMonotonic(channel.times)) {
        pushSceneStrictDiagnostic(diagnostics, "error", "scene.animation.invalid_times", "Animation times must be monotonic", path + ".times", id);
      }
      if (timeCount > 0 && valueCount > 0 && valueCount < timeCount) {
        pushSceneStrictDiagnostic(diagnostics, "error", "scene.animation.values_mismatch", "Animation values must cover every keyframe time", path + ".values", id, { times: timeCount, values: valueCount });
      }
      if (compressedTimeCount > 0 && compressedValueCount > 0 && compressedValueCount < compressedTimeCount) {
        pushSceneStrictDiagnostic(diagnostics, "error", "scene.animation.values_mismatch", "Compressed animation values must cover every compressed keyframe time", path + ".compressedValues", id, { times: compressedTimeCount, values: compressedValueCount });
      }
    }

    function validateSceneStrictHTML(diagnostics, seenIDs, knownIDs, htmlTargets, html, path, opts) {
      checkSceneStrictID(diagnostics, seenIDs, knownIDs, html.id, path + ".id", true);
      var mode = sceneStrictString(html.mode).toLowerCase() || "dom";
      if (["dom", "texture", "portal", "world", "screen"].indexOf(mode) < 0) {
        pushSceneStrictDiagnostic(diagnostics, "warn", "scene.html.unknown_mode", "HTML surface mode is not part of the formal mode set", path + ".mode", html.id, { mode: html.mode });
      }
      if (sceneStrictString(html.target)) {
        htmlTargets.push({ id: sceneStrictString(html.target), path: path + ".target", ownerID: html.id });
      }
      if (mode === "texture") {
        if (!sceneStrictString(html.fallback)) {
          pushSceneStrictDiagnostic(diagnostics, opts.strict === false ? "warn" : "error", "scene.html.texture_fallback", "Texture-backed HTML must include accessible fallback DOM", path + ".fallback", html.id);
        }
        if (!sceneStrictPositiveInteger(html.textureWidth) || !sceneStrictPositiveInteger(html.textureHeight)) {
          pushSceneStrictDiagnostic(diagnostics, opts.strict === false ? "warn" : "error", "scene.html.texture_size_missing", "Texture-backed HTML should declare texture dimensions", path, html.id);
        }
      }
      ["textureWidth", "textureHeight", "maxTexturePixels"].forEach(function(name) {
        validateSceneStrictOptionalInteger(diagnostics, html[name], path + "." + name, "scene.html.invalid_texture_size", "HTML texture dimensions and caps must be non-negative integers", html.id, true);
      });
      validateSceneStrictOptionalNumber(diagnostics, html.surfaceWidth, path + ".surfaceWidth", html.id, "scene.html.invalid_surface_size", "HTML surface dimensions must be finite", "scene.html.invalid_surface_size", "HTML surface dimensions must not be negative");
      validateSceneStrictOptionalNumber(diagnostics, html.surfaceHeight, path + ".surfaceHeight", html.id, "scene.html.invalid_surface_size", "HTML surface dimensions must be finite", "scene.html.invalid_surface_size", "HTML surface dimensions must not be negative");
      var pixels = sceneStrictNumber(html.textureWidth, 0) * sceneStrictNumber(html.textureHeight, 0);
      var cap = sceneStrictNumber(html.maxTexturePixels, 0) || sceneStrictNumber(opts.maxTexturePixels, 0);
      if (cap > 0 && pixels > cap) {
        pushSceneStrictDiagnostic(diagnostics, "error", "scene.texture.over_budget", "HTML texture dimensions exceed pixel budget", path, html.id, { pixels: pixels, maxTexturePixels: cap });
      }
      validateSceneStrictLiveFields(diagnostics, html.live, path, html.id);
    }

    function validateSceneStrictLight(diagnostics, seenIDs, knownIDs, light, path) {
      checkSceneStrictID(diagnostics, seenIDs, knownIDs, light.id, path + ".id", true);
      validateSceneStrictOptionalInteger(diagnostics, light.shadowSize, path + ".shadowSize", "scene.shadow.invalid_size", "Light shadowSize must be a non-negative integer", light.id, true);
      validateSceneStrictOptionalInteger(diagnostics, light.shadowCascades, path + ".shadowCascades", "scene.shadow.invalid_size", "Light shadowCascades must be a non-negative integer", light.id, true);
      validateSceneStrictOptionalNumber(diagnostics, light.shadowSoftness, path + ".shadowSoftness", light.id, "scene.shadow.invalid_size", "Light shadowSoftness must be finite", "scene.shadow.invalid_size", "Light shadowSoftness must not be negative");
      validateSceneStrictLiveFields(diagnostics, light.live, path, light.id);
    }

    function validateSceneStrictPostEffect(diagnostics, effect, path) {
      var kind = sceneStrictString(effect.kind);
      var type = sceneStrictString(effect.type);
      var normalized = kind || type;
      var known = {
        toneMapping: true,
        tonemap: true,
        bloom: true,
        vignette: true,
        colorGrade: true,
        "color-grade": true,
        ssao: true,
        dof: true
      };
      if (!normalized) {
        pushSceneStrictDiagnostic(diagnostics, "error", "scene.postfx.kind_missing", "Post effect requires kind", path + ".kind");
      } else if (!known[normalized]) {
        pushSceneStrictDiagnostic(diagnostics, "warn", "scene.postfx.unknown_kind", "Post effect kind is not part of the formal effect set", path + ".kind", "", { kind: normalized });
      }
      if (kind && type && kind !== type) {
        pushSceneStrictDiagnostic(diagnostics, "warn", "scene.postfx.type_mismatch", "Post effect kind and type disagree", path + ".type", "", { kind: kind, type: type });
      }
      validateSceneStrictFiniteScalars(diagnostics, effect, path, ["intensity", "threshold", "radius", "scale", "exposure", "contrast", "saturation", "bias", "focusDistance", "aperture", "maxBlur"], "scene.postfx.non_finite", "Post effect scalar must be finite", "");
      if (effect.params != null && typeof effect.params === "object") {
        Object.keys(effect.params).forEach(function(key) {
          if (!sceneStrictIsFiniteNumber(effect.params[key])) {
            pushSceneStrictDiagnostic(diagnostics, "error", "scene.postfx.non_finite", "Post effect param must be finite", path + ".params." + key);
          }
        });
      }
    }

    function validateSceneStrictCompressedArray(diagnostics, chunks, path, id, optional) {
      if (chunks == null) {
        return 0;
      }
      if (!Array.isArray(chunks)) {
        pushSceneStrictDiagnostic(diagnostics, "error", "scene.compression.invalid_array", "Compressed array must be an array of chunks", path, id);
        return 0;
      }
      if (!optional && chunks.length === 0) {
        pushSceneStrictDiagnostic(diagnostics, "error", "scene.compression.empty", "Compressed array must include chunks", path, id);
      }
      var total = 0;
      chunks.forEach(function(chunk, index) {
        var chunkPath = path + "[" + index + "]";
        if (!chunk || typeof chunk !== "object") {
          pushSceneStrictDiagnostic(diagnostics, "error", "scene.compression.invalid_chunk", "Compressed chunk must be an object", chunkPath, id);
          return;
        }
        if (!sceneStrictIsNonNegativeInteger(chunk.count) || chunk.count < 2) {
          pushSceneStrictDiagnostic(diagnostics, "error", "scene.compression.invalid_count", "Compressed chunk count must be an integer of at least two", chunkPath + ".count", id, { value: chunk.count });
        } else {
          total += chunk.count;
        }
        if (chunk.dim != null && !sceneStrictIsNonNegativeInteger(chunk.dim)) {
          pushSceneStrictDiagnostic(diagnostics, "error", "scene.compression.invalid_dim", "Compressed chunk dim must be a non-negative integer", chunkPath + ".dim", id, { value: chunk.dim });
        }
        if (!sceneStrictIsNonNegativeInteger(chunk.bitWidth) || chunk.bitWidth < 1 || chunk.bitWidth > 30) {
          pushSceneStrictDiagnostic(diagnostics, "error", "scene.compression.invalid_bit_width", "Compressed chunk bitWidth must be in range 1..30", chunkPath + ".bitWidth", id, { value: chunk.bitWidth });
        }
        if (!sceneStrictIsFiniteNumber(chunk.norm)) {
          pushSceneStrictDiagnostic(diagnostics, "error", "scene.compression.non_finite", "Compressed chunk norm must be finite", chunkPath + ".norm", id);
        }
        if (!sceneStrictIsFiniteNumber(chunk.maxVal)) {
          pushSceneStrictDiagnostic(diagnostics, "error", "scene.compression.non_finite", "Compressed chunk maxVal must be finite", chunkPath + ".maxVal", id);
        }
        if (!sceneStrictPackedValue(chunk.packed)) {
          pushSceneStrictDiagnostic(diagnostics, "error", "scene.compression.invalid_packed", "Compressed chunk packed payload must be base64, byte array, or Uint8Array", chunkPath + ".packed", id);
        }
      });
      return total;
    }

    function checkSceneStrictID(diagnostics, seenIDs, knownIDs, id, path, required) {
      var key = sceneStrictString(id);
      if (!key) {
        if (required) {
          pushSceneStrictDiagnostic(diagnostics, "error", "scene.id.missing", "Scene record requires a stable ID", path);
        }
        return;
      }
      if (seenIDs[key]) {
        pushSceneStrictDiagnostic(diagnostics, "error", "scene.id.duplicate", "Scene record ID is duplicated", path, key, { firstPath: seenIDs[key] });
        return;
      }
      seenIDs[key] = path;
      knownIDs[key] = true;
    }

    function sceneStrictTopLevelArray(diagnostics, ir, key) {
      if (ir[key] == null) {
        return [];
      }
      if (!Array.isArray(ir[key])) {
        pushSceneStrictDiagnostic(diagnostics, "error", "scene.array.invalid", "SceneIR top-level field must be an array", key);
        return [];
      }
      return ir[key];
    }

    function validateSceneStrictLiveFields(diagnostics, live, path, id) {
      if (live == null) {
        return;
      }
      if (!Array.isArray(live)) {
        pushSceneStrictDiagnostic(diagnostics, "error", "scene.live.invalid", "Live fields must be an array", path + ".live", id);
        return;
      }
      live.forEach(function(field, index) {
        if (!sceneStrictString(field)) {
          pushSceneStrictDiagnostic(diagnostics, "error", "scene.live.invalid", "Live field name must not be empty", path + ".live[" + index + "]", id);
        }
      });
    }

    function validateSceneStrictCount(diagnostics, value, path, id, code, message) {
      if (!sceneStrictIsNonNegativeInteger(value)) {
        pushSceneStrictDiagnostic(diagnostics, "error", code, message, path, id, { value: value });
      }
    }

    function validateSceneStrictOptionalInteger(diagnostics, value, path, code, message, id, nonNegative) {
      if (value == null) {
        return;
      }
      if (!sceneStrictIsFiniteNumber(value) || Math.floor(value) !== value || (nonNegative && value < 0)) {
        pushSceneStrictDiagnostic(diagnostics, "error", code, message, path, id, { value: value });
      }
    }

    function validateSceneStrictOptionalNumber(diagnostics, value, path, id, nonFiniteCode, nonFiniteMessage, negativeCode, negativeMessage) {
      if (value == null) {
        return;
      }
      if (!sceneStrictIsFiniteNumber(value)) {
        pushSceneStrictDiagnostic(diagnostics, "error", nonFiniteCode, nonFiniteMessage, path, id);
      } else if (value < 0) {
        pushSceneStrictDiagnostic(diagnostics, "error", negativeCode, negativeMessage, path, id, { value: value });
      }
    }

    function validateSceneStrictFiniteScalars(diagnostics, object, path, names, code, message, id) {
      names.forEach(function(name) {
        var value = object && object[name];
        if (value != null && !sceneStrictIsFiniteNumber(value)) {
          pushSceneStrictDiagnostic(diagnostics, "error", code, message, path + "." + name, id, { value: value });
        }
      });
    }

    function validateSceneStrictNonNegativeScalars(diagnostics, object, path, names, code, message) {
      names.forEach(function(name) {
        var value = object && object[name];
        if (value != null && (!sceneStrictIsFiniteNumber(value) || value < 0)) {
          pushSceneStrictDiagnostic(diagnostics, "error", code, message, path + "." + name, object && object.id, { value: value });
        }
      });
    }

    function validateSceneStrictNumericArray(diagnostics, value, path, id, code, message) {
      if (value == null) {
        return;
      }
      if (!sceneStrictIsArrayLike(value)) {
        pushSceneStrictDiagnostic(diagnostics, "error", code, message, path, id, { valueType: typeof value });
        return;
      }
      for (var index = 0; index < value.length; index += 1) {
        if (!sceneStrictIsFiniteNumber(value[index])) {
          pushSceneStrictDiagnostic(diagnostics, "error", code, message, path + "[" + index + "]", id);
          return;
        }
      }
    }

    function validateSceneStrictPointsColorArray(diagnostics, colors, count, path, id) {
      if (colors == null) {
        return;
      }
      if (!sceneStrictIsArrayLike(colors)) {
        pushSceneStrictDiagnostic(diagnostics, "error", "scene.points.array_non_finite", "Point colors must be an array or typed numeric array", path, id);
        return;
      }
      if (colors.length === 0) {
        return;
      }
      if (typeof colors[0] === "string") {
        if (count > 0 && colors.length < count) {
          pushSceneStrictDiagnostic(diagnostics, "error", "scene.points.count_mismatch", "Point count exceeds color values", path, id, { count: count, colors: colors.length });
        }
        return;
      }
      validateSceneStrictNumericArray(diagnostics, colors, path, id, "scene.points.array_non_finite", "Point colors must be finite");
      if (count > 0 && colors.length < count * 3) {
        pushSceneStrictDiagnostic(diagnostics, "error", "scene.points.count_mismatch", "Point count exceeds numeric color values", path, id, { count: count, colors: colors.length });
      }
    }

    function validateSceneStrictInstanceColors(diagnostics, colors, count, path, id) {
      if (colors == null) {
        return;
      }
      if (!sceneStrictIsArrayLike(colors)) {
        pushSceneStrictDiagnostic(diagnostics, "error", "scene.instances.array_non_finite", "Instance colors must be an array or typed numeric array", path, id);
        return;
      }
      if (colors.length === 0) {
        return;
      }
      if (typeof colors[0] === "string") {
        if (count > 0 && colors.length < count) {
          pushSceneStrictDiagnostic(diagnostics, "error", "scene.instances.count_mismatch", "Instanced mesh count exceeds color values", path, id, { count: count, colors: colors.length });
        }
        return;
      }
      validateSceneStrictNumericArray(diagnostics, colors, path, id, "scene.instances.array_non_finite", "Instance colors must be finite");
      if (count > 0 && colors.length < count * 3) {
        pushSceneStrictDiagnostic(diagnostics, "error", "scene.instances.count_mismatch", "Instanced mesh count exceeds numeric color values", path, id, { count: count, colors: colors.length });
      }
    }

    function validateSceneStrictAttributeArrays(diagnostics, attributes, count, path, id) {
      if (attributes == null) {
        return;
      }
      if (typeof attributes !== "object" || Array.isArray(attributes)) {
        pushSceneStrictDiagnostic(diagnostics, "error", "scene.instances.invalid_attributes", "Instanced attributes must be an object of numeric arrays", path, id);
        return;
      }
      Object.keys(attributes).forEach(function(key) {
        var value = attributes[key];
        validateSceneStrictNumericArray(diagnostics, value, path + "." + key, id, "scene.instances.array_non_finite", "Instanced attribute values must be finite");
        if (count > 0 && sceneStrictArrayLikeLength(value) > 0 && sceneStrictArrayLikeLength(value) < count) {
          pushSceneStrictDiagnostic(diagnostics, "error", "scene.instances.count_mismatch", "Instanced mesh count exceeds attribute values", path + "." + key, id, { count: count, values: sceneStrictArrayLikeLength(value) });
        }
      });
    }

    function sceneStrictMonotonic(values) {
      if (!sceneStrictIsArrayLike(values)) {
        return false;
      }
      for (var index = 1; index < values.length; index += 1) {
        if (values[index] < values[index - 1]) {
          return false;
        }
      }
      return true;
    }

    function sceneStrictString(value) {
      return typeof value === "string" ? value.trim() : "";
    }

    function sceneStrictNumber(value, fallback) {
      return typeof value === "number" && Number.isFinite(value) ? value : fallback;
    }

    function sceneStrictIsFiniteNumber(value) {
      return typeof value === "number" && Number.isFinite(value);
    }

    function sceneStrictIsNonNegativeInteger(value) {
      return sceneStrictIsFiniteNumber(value) && value >= 0 && Math.floor(value) === value;
    }

    function sceneStrictPositiveInteger(value) {
      return sceneStrictIsFiniteNumber(value) && value > 0 && Math.floor(value) === value;
    }

    function sceneStrictCount(value) {
      return sceneStrictIsNonNegativeInteger(value) ? value : 0;
    }

    function sceneStrictIsArrayLike(value) {
      if (Array.isArray(value)) {
        return true;
      }
      return typeof ArrayBuffer !== "undefined" && ArrayBuffer.isView && ArrayBuffer.isView(value) && typeof value.length === "number";
    }

    function sceneStrictArrayLikeLength(value) {
      return sceneStrictIsArrayLike(value) ? value.length : 0;
    }

    function sceneStrictPackedValue(value) {
      if (typeof value === "string" && value.length > 0) {
        return true;
      }
      if (Array.isArray(value) && value.length > 0) {
        return value.every(function(byte) { return sceneStrictIsNonNegativeInteger(byte) && byte <= 255; });
      }
      return typeof Uint8Array !== "undefined" && value instanceof Uint8Array && value.length > 0;
    }

    function sceneStrictContains(value, needle) {
      return sceneStrictString(value).toLowerCase().indexOf(needle) >= 0;
    }

    function pushSceneStrictDiagnostic(diagnostics, severity, code, message, path, id, data) {
      diagnostics.push({
        severity: severity,
        code: code,
        message: message,
        path: path || "",
        id: id || "",
        data: data || undefined
      });
    }

    var root = typeof globalThis !== "undefined" ? globalThis : (typeof window !== "undefined" ? window : null);
    if (root) {
      try {
        Object.defineProperty(root, "__gosx_validate_scene_ir_strict", {
          value: validateSceneIRStrict,
          writable: false,
          enumerable: false,
          configurable: false
        });
      } catch (_err) {
        root.__gosx_validate_scene_ir_strict = root.__gosx_validate_scene_ir_strict || validateSceneIRStrict;
      }
    }
  })();
