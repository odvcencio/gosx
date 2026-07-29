  // Legacy vertex-colour WebGL renderer.
  //
  // This slice used to sit in 10-runtime-scene-core.js, which every Scene3D
  // page downloads. Only a WebGL page ever runs it: createSceneWebGLResult in
  // 20b-scene-mount-webgl-chunk.js calls createSceneWebGLRenderer as the last
  // resort after the PBR factory declines, and the PBR factory itself lives in
  // the WebGL chunk. A WebGPU page therefore paid for a renderer it can never
  // reach.
  //
  // The slice now ships in bootstrap-feature-scene3d-webgl.js, next to the PBR
  // renderer it backs up, and in the monolithic bootstrap.js. The tail of this
  // file republishes the entry points on window.__gosx_scene3d_api, because
  // 20b calls them through that object once the chunk lands.
  //
  // Everything here draws with the plain gl.LINES / gl.TRIANGLES pipeline:
  // the world bundle, the mesh passes, the textured surfaces, the shader
  // programs and the thick-line expansion.

  function createSceneWebGLRenderer(canvas, options) {
    if (!canvas || typeof canvas.getContext !== "function") {
      return null;
    }
    const contextOptions = {
      alpha: false,
      antialias: !(options && options.antialias === false),
      powerPreference: options && options.powerPreference ? options.powerPreference : "high-performance",
      preserveDrawingBuffer: false,
    };
    const gl =
      canvas.getContext("webgl", contextOptions) ||
      canvas.getContext("experimental-webgl", contextOptions) ||
      canvas.getContext("webgl2", contextOptions);
    if (!gl) {
      return null;
    }

    const program = createSceneWebGLProgram(gl);
    const surfaceProgram = createSceneWebGLSurfaceProgram(gl);
    if (!program) {
      return null;
    }

    const resources = createSceneWebGLResources(gl, program, surfaceProgram);
    return {
      kind: "webgl",
      supportsRetainedGeometry: false,
      render(bundle) {
        const geometry = sceneWebGLBundleGeometry(bundle);
        prepareSceneWebGLFrame(gl, canvas, bundle, geometry.usePerspective, resources);
        if (!bundle) {
          return;
        }
        const worldRendered = geometry.usePerspective && renderSceneWebGLWorldBundle(gl, bundle, canvas, resources);
        if (worldRendered) {
          applySceneWebGLBlend(gl, "opaque", resources.stateCache);
          applySceneWebGLDepth(gl, "opaque", resources.stateCache);
          return;
        }
        if (geometry.vertexCount === 0 || !geometry.positions || !geometry.colors) {
          return;
        }
        gl.useProgram(program);
        applySceneWebGLUniforms(gl, bundle, canvas, geometry.usePerspective, resources);
        renderSceneWebGLFallbackBundle(gl, geometry, resources);
      },
      dispose() {
        disposeSceneWebGLRenderer(gl, program, resources);
      },
    };
  }

  function createSceneWebGLResources(gl, program, surfaceProgram) {
    // Thick-line program is compiled lazily (and silently tolerates failure)
    // so a driver that refuses the program just falls back to the legacy
    // gl.LINES path. Buffers exist up-front so the draw path doesn't allocate
    // per frame.
    const thickLineProgram = createSceneThickLineProgram(gl);
    return {
      program,
      surfaceProgram,
      fallbackBuffers: createSceneWebGLBufferSet(gl),
      passBuffers: {
        staticOpaque: createSceneWebGLBufferSet(gl),
        alpha: createSceneWebGLBufferSet(gl),
        additive: createSceneWebGLBufferSet(gl),
        dynamicOpaque: createSceneWebGLBufferSet(gl),
      },
      drawScratch: createSceneWorldDrawScratch(),
      thickLineProgram,
      thickLineBuffers: createSceneThickLineBufferSet(gl),
      thickLineScratch: createSceneThickLineScratch(),
      positionLocation: gl.getAttribLocation(program, "a_position"),
      colorLocation: gl.getAttribLocation(program, "a_color"),
      materialLocation: gl.getAttribLocation(program, "a_material"),
      cameraLocation: gl.getUniformLocation(program, "u_camera"),
      cameraRotationLocation: gl.getUniformLocation(program, "u_camera_rotation"),
      depthRangeLocation: gl.getUniformLocation(program, "u_depth_range"),
      aspectLocation: gl.getUniformLocation(program, "u_aspect"),
      perspectiveLocation: gl.getUniformLocation(program, "u_use_perspective"),
      cameraModeLocation: gl.getUniformLocation(program, "u_camera_mode"),
      orthoLocation: gl.getUniformLocation(program, "u_ortho"),
      surfaceBuffers: createSceneWebGLSurfaceBufferSet(gl),
      surfacePositionLocation: surfaceProgram ? gl.getAttribLocation(surfaceProgram, "a_position") : -1,
      surfaceUVLocation: surfaceProgram ? gl.getAttribLocation(surfaceProgram, "a_uv") : -1,
      surfaceCameraLocation: surfaceProgram ? gl.getUniformLocation(surfaceProgram, "u_camera") : null,
      surfaceCameraRotationLocation: surfaceProgram ? gl.getUniformLocation(surfaceProgram, "u_camera_rotation") : null,
      surfaceDepthRangeLocation: surfaceProgram ? gl.getUniformLocation(surfaceProgram, "u_depth_range") : null,
      surfaceAspectLocation: surfaceProgram ? gl.getUniformLocation(surfaceProgram, "u_aspect") : null,
      surfaceCameraModeLocation: surfaceProgram ? gl.getUniformLocation(surfaceProgram, "u_camera_mode") : null,
      surfaceOrthoLocation: surfaceProgram ? gl.getUniformLocation(surfaceProgram, "u_ortho") : null,
      surfaceTintLocation: surfaceProgram ? gl.getUniformLocation(surfaceProgram, "u_tint") : null,
      surfaceEmissiveLocation: surfaceProgram ? gl.getUniformLocation(surfaceProgram, "u_emissive") : null,
      surfaceTextureLocation: surfaceProgram ? gl.getUniformLocation(surfaceProgram, "u_texture") : null,
      floatType: typeof gl.FLOAT === "number" ? gl.FLOAT : 0x1406,
      arrayBuffer: typeof gl.ARRAY_BUFFER === "number" ? gl.ARRAY_BUFFER : 0x8892,
      staticDraw: typeof gl.STATIC_DRAW === "number" ? gl.STATIC_DRAW : 0x88E4,
      dynamicDraw: typeof gl.DYNAMIC_DRAW === "number" ? gl.DYNAMIC_DRAW : 0x88E8,
      trianglesMode: typeof gl.TRIANGLES === "number" ? gl.TRIANGLES : 0x0004,
      colorBufferBit: typeof gl.COLOR_BUFFER_BIT === "number" ? gl.COLOR_BUFFER_BIT : 0x4000,
      depthBufferBit: typeof gl.DEPTH_BUFFER_BIT === "number" ? gl.DEPTH_BUFFER_BIT : 0x0100,
      linesMode: typeof gl.LINES === "number" ? gl.LINES : 0x0001,
      texture2D: typeof gl.TEXTURE_2D === "number" ? gl.TEXTURE_2D : 0x0DE1,
      texture0: typeof gl.TEXTURE0 === "number" ? gl.TEXTURE0 : 0x84C0,
      rgbaFormat: typeof gl.RGBA === "number" ? gl.RGBA : 0x1908,
      unsignedByte: typeof gl.UNSIGNED_BYTE === "number" ? gl.UNSIGNED_BYTE : 0x1401,
      linearFilter: typeof gl.LINEAR === "number" ? gl.LINEAR : 0x2601,
      clampToEdge: typeof gl.CLAMP_TO_EDGE === "number" ? gl.CLAMP_TO_EDGE : 0x812F,
      textureMinFilter: typeof gl.TEXTURE_MIN_FILTER === "number" ? gl.TEXTURE_MIN_FILTER : 0x2801,
      textureMagFilter: typeof gl.TEXTURE_MAG_FILTER === "number" ? gl.TEXTURE_MAG_FILTER : 0x2800,
      textureWrapS: typeof gl.TEXTURE_WRAP_S === "number" ? gl.TEXTURE_WRAP_S : 0x2802,
      textureWrapT: typeof gl.TEXTURE_WRAP_T === "number" ? gl.TEXTURE_WRAP_T : 0x2803,
      passCache: {
        staticOpaque: {
          key: "",
          vertexCount: 0,
        },
      },
      textureCache: new Map(),
      stateCache: {
        blendMode: "",
        depthMode: "",
      },
    };
  }

  function sceneWebGLBundleGeometry(bundle) {
    const hasWorldLines = Boolean(bundle && bundle.worldVertexCount > 0 && bundle.worldPositions && bundle.worldColors);
    const hasWorldMeshes = Boolean(bundle && bundle.worldMeshVertexCount > 0 && bundle.worldMeshPositions && bundle.worldMeshColors);
    const hasSurfaces = Boolean(bundle && Array.isArray(bundle.surfaces) && bundle.surfaces.length > 0);
    const camera = sceneRenderCamera(bundle && bundle.camera);
    const usePerspective = hasWorldMeshes || hasSurfaces || (hasWorldLines && camera.kind !== "orthographic");
    return {
      usePerspective,
      positions: usePerspective ? bundle.worldPositions : bundle && bundle.positions,
      colors: usePerspective ? bundle.worldColors : bundle && bundle.colors,
      vertexCount: usePerspective ? bundle && bundle.worldVertexCount : bundle && bundle.vertexCount,
    };
  }

  function prepareSceneWebGLFrame(gl, canvas, bundle, usePerspective, resources) {
    const background = sceneColorRGBA(bundle && bundle.background, [0.03, 0.08, 0.12, 1]);
    gl.viewport(0, 0, canvas.width, canvas.height);
    gl.clearColor(background[0], background[1], background[2], background[3]);
    if (usePerspective && typeof gl.clearDepth === "function") {
      gl.clearDepth(1);
    }
    gl.clear(usePerspective ? resources.colorBufferBit | resources.depthBufferBit : resources.colorBufferBit);
  }

  function applySceneWebGLUniforms(gl, bundle, canvas, usePerspective, resources) {
    const aspect = Math.max(0.0001, canvas.width / Math.max(1, canvas.height));
    // Resolve the camera object once per invocation — sceneRenderCamera
    // allocates, and this function used to call it twice (once per uniform
    // group) which doubled GC pressure for no reason.
    const camera = sceneRenderCamera(bundle && bundle.camera);
    if (typeof gl.uniform4f === "function" && resources.cameraLocation) {
      gl.uniform4f(
        resources.cameraLocation,
        camera.x,
        camera.y,
        camera.z,
        camera.fov,
      );
    }
    if (typeof gl.uniform3f === "function" && resources.cameraRotationLocation) {
      gl.uniform3f(
        resources.cameraRotationLocation,
        camera.rotationX,
        camera.rotationY,
        camera.rotationZ,
      );
    }
    if (typeof gl.uniform2f === "function" && resources.depthRangeLocation) {
      gl.uniform2f(resources.depthRangeLocation, camera.near, camera.far);
    }
    if (typeof gl.uniform1f === "function" && resources.aspectLocation) {
      gl.uniform1f(resources.aspectLocation, aspect);
    }
    if (typeof gl.uniform1f === "function" && resources.perspectiveLocation) {
      gl.uniform1f(resources.perspectiveLocation, usePerspective ? 1 : 0);
    }
    if (typeof gl.uniform1f === "function" && resources.cameraModeLocation) {
      gl.uniform1f(resources.cameraModeLocation, camera.kind === "orthographic" ? 1 : 0);
    }
    if (typeof gl.uniform4f === "function" && resources.orthoLocation) {
      const bounds = sceneOrthographicBounds(camera, canvas.width, canvas.height);
      gl.uniform4f(resources.orthoLocation, bounds.left, bounds.right, bounds.top, bounds.bottom);
    }
  }

  // renderSceneWebGLWorldBundle draws the world-space half of a bundle: HTML
  // surfaces, mesh objects, and line segments.
  //
  // options.meshObjects === false suppresses the mesh half. The WebGL2 PBR
  // renderer sets it, because it has ALREADY drawn every mesh object with the
  // object's own authored program (PBR, CustomMaterial, or Selena) and calls
  // this function only for the line and surface work it has no path for.
  //
  // Leaving the mesh half on for that caller re-drew every untextured mesh a
  // SECOND time, with the legacy flat world program and the baked
  // worldMeshColors base color, on top of the correct draw. The over-draw was
  // invisible in isolation because it only runs when the frame also carries
  // world line segments (bundle.worldVertexCount > 0, the gate at the WebGL2
  // call site). One LinesGeometry mesh in the scene was therefore enough to
  // repaint every Selena plane as a flat quad of its companion StandardMaterial
  // color -- with no warning, because nothing had failed.
  //
  // The legacy WebGL1 fallback renderer keeps the default: the mesh half is its
  // ONLY mesh path.
  function renderSceneWebGLWorldBundle(gl, bundle, canvas, resources, options) {
    const drawMeshObjects = !(options && options.meshObjects === false);
    let drew = renderSceneWebGLSurfaces(gl, bundle, canvas, resources, "opaque");
    if (drawMeshObjects) {
      drew = renderSceneWebGLMeshWorldBundle(gl, bundle, canvas, resources) || drew;
    }

    // Dispatch to the thick-line program when any world line has an explicit
    // width > 1 (scene.LinesGeometry.Width on the Go side). This preserves
    // legacy behavior for existing scenes (hairline via gl.LINES) and only
    // activates the new draw path for scenes that opt in. The thick-line
    // path currently draws all world lines as one call with alpha blending
    // and does not respect the draw plan's per-pass (opaque/alpha/additive)
    // blend separation — follow-up work can thread per-pass state through
    // if a production scene needs mixed-blend thick lines.
    if (sceneBundleNeedsThickLines(bundle) && resources.thickLineProgram) {
      const thickDrew = drawSceneThickLines(gl, bundle, canvas, resources);
      if (thickDrew) {
        gl.useProgram(resources.program);
        applySceneWebGLUniforms(gl, bundle, canvas, true, resources);
        drew = renderSceneWebGLSurfaces(gl, bundle, canvas, resources, "alpha") || drew || true;
        drew = renderSceneWebGLSurfaces(gl, bundle, canvas, resources, "additive") || drew;
        return true;
      }
      // Thick-line draw failed (e.g. segment count overflow). Fall through
      // to the legacy gl.LINES path so the scene still renders.
    }

    gl.useProgram(resources.program);
    applySceneWebGLUniforms(gl, bundle, canvas, true, resources);
    if (sceneBundleCanUseBundledPasses(bundle)) {
      const bundledPasses = createSceneWorldWebGLPassesFromBundle(bundle, resources.passBuffers, {
        staticDraw: resources.staticDraw,
        dynamicDraw: resources.dynamicDraw,
      });
      if (bundledPasses.length > 0) {
        drawSceneWebGLPasses(gl, resources.arrayBuffer, resources.floatType, resources.linesMode, resources.positionLocation, resources.colorLocation, resources.materialLocation, bundledPasses, resources.passCache, resources.stateCache);
        drew = true;
        drew = renderSceneWebGLSurfaces(gl, bundle, canvas, resources, "alpha") || drew;
        drew = renderSceneWebGLSurfaces(gl, bundle, canvas, resources, "additive") || drew;
        return true;
      }
    }
    const drawPlan = buildSceneWorldDrawPlan(bundle, resources.drawScratch);
    if (!drawPlan) {
      drew = renderSceneWebGLSurfaces(gl, bundle, canvas, resources, "alpha") || drew;
      drew = renderSceneWebGLSurfaces(gl, bundle, canvas, resources, "additive") || drew;
      return drew;
    }
    const worldPasses = createSceneWorldWebGLPasses(drawPlan, resources.passBuffers, {
      staticDraw: resources.staticDraw,
      dynamicDraw: resources.dynamicDraw,
    });
    drawSceneWebGLPasses(gl, resources.arrayBuffer, resources.floatType, resources.linesMode, resources.positionLocation, resources.colorLocation, resources.materialLocation, worldPasses, resources.passCache, resources.stateCache);
    drew = true;
    drew = renderSceneWebGLSurfaces(gl, bundle, canvas, resources, "alpha") || drew;
    drew = renderSceneWebGLSurfaces(gl, bundle, canvas, resources, "additive") || drew;
    return true;
  }

  function renderSceneWebGLMeshWorldBundle(gl, bundle, canvas, resources) {
    const meshObjects = Array.isArray(bundle && bundle.meshObjects) ? bundle.meshObjects : [];
    if (!meshObjects.length || !bundle || !bundle.worldMeshPositions || !bundle.worldMeshColors) {
      return false;
    }
    const opaque = [];
    const alpha = [];
    const additive = [];
    for (let index = 0; index < meshObjects.length; index += 1) {
      const object = meshObjects[index];
      if (!sceneWorldObjectRenderable(object, bundle.camera)) {
        continue;
      }
      const material = Array.isArray(bundle.materials) ? bundle.materials[object.materialIndex] || null : null;
      const renderPass = sceneWorldObjectRenderPass(object, material);
      const entry = {
        object,
        material,
        order: index,
        depth: sceneNumber(object && object.depthCenter, 0),
      };
      if (renderPass === "alpha") {
        alpha.push(entry);
        continue;
      }
      if (renderPass === "additive") {
        additive.push(entry);
        continue;
      }
      opaque.push(entry);
    }
    if (!opaque.length && !alpha.length && !additive.length) {
      return false;
    }
    gl.useProgram(resources.program);
    applySceneWebGLUniforms(gl, bundle, canvas, true, resources);
    let drew = false;
    drew = renderSceneWebGLMeshWorldPass(gl, bundle, canvas, resources, opaque, "opaque", "opaque") || drew;
    drew = renderSceneWebGLMeshWorldPass(gl, bundle, canvas, resources, alpha, "alpha", "translucent") || drew;
    drew = renderSceneWebGLMeshWorldPass(gl, bundle, canvas, resources, additive, "additive", "translucent") || drew;
    return drew;
  }

  function renderSceneWebGLMeshWorldPass(gl, bundle, canvas, resources, entries, blendMode, depthMode) {
    if (!Array.isArray(entries) || !entries.length) {
      return false;
    }
    if (blendMode !== "opaque") {
      entries.sort(compareSceneWorldPassEntries);
    }
    applySceneWebGLDepth(gl, depthMode, resources.stateCache);
    applySceneWebGLBlend(gl, blendMode, resources.stateCache);
    let drew = false;
    for (const entry of entries) {
      drew = renderSceneWebGLMeshObject(gl, bundle, canvas, resources, entry.object, entry.material) || drew;
    }
    return drew;
  }

  function renderSceneWebGLMeshObject(gl, bundle, canvas, resources, object, material) {
    const vertexOffset = Math.max(0, Math.floor(sceneNumber(object && object.vertexOffset, 0)));
    const vertexCount = Math.max(0, Math.floor(sceneNumber(object && object.vertexCount, 0)));
    if (!vertexCount) {
      return false;
    }
    if (renderSceneWebGLTexturedMeshObject(gl, bundle, canvas, resources, object, material, vertexOffset, vertexCount)) {
      return true;
    }
    gl.useProgram(resources.program);
    applySceneWebGLUniforms(gl, bundle, canvas, true, resources);
    const positions = sceneSliceFloatArray(bundle.worldMeshPositions, vertexOffset * 3, vertexCount * 3);
    const colors = sceneSliceFloatArray(bundle.worldMeshColors, vertexOffset * 4, vertexCount * 4);
    const materials = sceneMeshMaterialArray(vertexCount, material);
    uploadSceneWebGLBuffers(
      gl,
      resources.arrayBuffer,
      object && object.static ? resources.staticDraw : resources.dynamicDraw,
      resources.fallbackBuffers.position,
      resources.fallbackBuffers.color,
      resources.fallbackBuffers.material,
      positions,
      colors,
      materials,
    );
    drawSceneWebGLPrimitives(
      gl,
      resources.arrayBuffer,
      resources.floatType,
      resources.trianglesMode,
      resources.positionLocation,
      resources.colorLocation,
      resources.materialLocation,
      resources.fallbackBuffers.position,
      resources.fallbackBuffers.color,
      resources.fallbackBuffers.material,
      vertexCount,
      3,
    );
    return true;
  }

  function renderSceneWebGLTexturedMeshObject(gl, bundle, canvas, resources, object, material, vertexOffset, vertexCount) {
    const materialTexture = material && typeof material.texture === "string" ? material.texture.trim() : "";
    const objectTexture = object && typeof object.texture === "string" ? object.texture.trim() : "";
    const texture = materialTexture || objectTexture;
    if (!texture || !resources.surfaceProgram || !bundle || !bundle.worldMeshPositions || !bundle.worldMeshUVs) {
      return false;
    }
    const textureRecord = sceneWebGLTextureRecord(gl, resources, texture);
    if (!textureRecord || !textureRecord.texture) {
      return false;
    }
    const positions = sceneSliceFloatArray(bundle.worldMeshPositions, vertexOffset * 3, vertexCount * 3);
    const uv = sceneSliceFloatArray(bundle.worldMeshUVs, vertexOffset * 2, vertexCount * 2);
    gl.useProgram(resources.surfaceProgram);
    applySceneWebGLSurfaceUniforms(gl, bundle, canvas, resources);
    uploadSceneWebGLSurfaceBuffers(gl, resources, {
      positions,
      uv,
    });
    bindSceneWebGLSurfaceTexture(gl, resources, textureRecord);
    applySceneWebGLSurfaceMaterial(gl, resources, material);
    drawSceneWebGLSurface(gl, resources, vertexCount);
    gl.useProgram(resources.program);
    applySceneWebGLUniforms(gl, bundle, canvas, true, resources);
    return true;
  }

  function sceneSliceFloatArray(values, start, count) {
    const safeStart = Math.max(0, Math.floor(sceneNumber(start, 0)));
    const safeCount = Math.max(0, Math.floor(sceneNumber(count, 0)));
    const typed = new Float32Array(safeCount);
    for (let index = 0; index < safeCount; index += 1) {
      typed[index] = sceneNumber(values && values[safeStart + index], 0);
    }
    return typed;
  }

  function sceneMeshMaterialArray(vertexCount, material) {
    const data = sceneMaterialShaderData(material);
    const typed = new Float32Array(Math.max(0, vertexCount) * 3);
    for (let index = 0; index < vertexCount; index += 1) {
      const offset = index * 3;
      typed[offset] = data[0];
      typed[offset + 1] = data[1];
      typed[offset + 2] = data[2];
    }
    return typed;
  }

  function sceneBundleCanUseBundledPasses(bundle) {
    if (!bundle || !Array.isArray(bundle.passes) || bundle.passes.length === 0) {
      return false;
    }
    if (!bundle.sourceCamera) {
      return true;
    }
    return sceneCameraEquivalent(bundle.sourceCamera, bundle.camera);
  }

  function renderSceneWebGLSurfaces(gl, bundle, canvas, resources, renderPass) {
    const surfaces = sceneBundleSurfaceEntries(bundle, renderPass);
    if (!surfaces.length || !resources.surfaceProgram) {
      return false;
    }
    gl.useProgram(resources.surfaceProgram);
    applySceneWebGLSurfaceUniforms(gl, bundle, canvas, resources);
    applySceneWebGLBlend(gl, renderPass === "additive" ? "additive" : (renderPass === "alpha" ? "alpha" : "opaque"), resources.stateCache);
    applySceneWebGLDepth(gl, renderPass === "opaque" ? "opaque" : "translucent", resources.stateCache);
    for (const entry of surfaces) {
      const material = bundle.materials[entry.materialIndex] || null;
      const textureRecord = sceneWebGLTextureRecord(gl, resources, material && material.texture);
      if (!textureRecord || !textureRecord.texture) {
        continue;
      }
      uploadSceneWebGLSurfaceBuffers(gl, resources, entry);
      bindSceneWebGLSurfaceTexture(gl, resources, textureRecord);
      applySceneWebGLSurfaceMaterial(gl, resources, material);
      drawSceneWebGLSurface(gl, resources, entry.vertexCount);
    }
    return true;
  }

  function sceneBundleSurfaceEntries(bundle, renderPass) {
    const surfaces = Array.isArray(bundle && bundle.surfaces) ? bundle.surfaces.slice() : [];
    const filtered = surfaces.filter(function(surface) {
      return surface &&
        !surface.viewCulled &&
        !(surface.sourceKind === "html" && !surface.textureReady) &&
        Math.max(0, Math.floor(sceneNumber(surface.vertexCount, 0))) > 0 &&
        String(surface.renderPass || "opaque") === renderPass;
    });
    if (renderPass !== "opaque") {
      filtered.sort(function(left, right) {
        if (sceneNumber(left.depthCenter, 0) !== sceneNumber(right.depthCenter, 0)) {
          return sceneNumber(right.depthCenter, 0) - sceneNumber(left.depthCenter, 0);
        }
        return String(left.id || "").localeCompare(String(right.id || ""));
      });
    }
    return filtered;
  }

  function applySceneWebGLSurfaceUniforms(gl, bundle, canvas, resources) {
    const aspect = Math.max(0.0001, canvas.width / Math.max(1, canvas.height));
    const camera = sceneRenderCamera(bundle && bundle.camera);
    if (typeof gl.uniform4f === "function" && resources.surfaceCameraLocation) {
      gl.uniform4f(resources.surfaceCameraLocation, camera.x, camera.y, camera.z, camera.fov);
    }
    if (typeof gl.uniform3f === "function" && resources.surfaceCameraRotationLocation) {
      gl.uniform3f(resources.surfaceCameraRotationLocation, camera.rotationX, camera.rotationY, camera.rotationZ);
    }
    if (typeof gl.uniform2f === "function" && resources.surfaceDepthRangeLocation) {
      gl.uniform2f(resources.surfaceDepthRangeLocation, camera.near, camera.far);
    }
    if (typeof gl.uniform1f === "function" && resources.surfaceAspectLocation) {
      gl.uniform1f(resources.surfaceAspectLocation, aspect);
    }
    if (typeof gl.uniform1f === "function" && resources.surfaceCameraModeLocation) {
      gl.uniform1f(resources.surfaceCameraModeLocation, camera.kind === "orthographic" ? 1 : 0);
    }
    if (typeof gl.uniform4f === "function" && resources.surfaceOrthoLocation) {
      const bounds = sceneOrthographicBounds(camera, canvas.width, canvas.height);
      gl.uniform4f(resources.surfaceOrthoLocation, bounds.left, bounds.right, bounds.top, bounds.bottom);
    }
  }

  function uploadSceneWebGLSurfaceBuffers(gl, resources, surface) {
    gl.bindBuffer(resources.arrayBuffer, resources.surfaceBuffers.position);
    gl.bufferData(resources.arrayBuffer, sceneTypedFloatArray(surface && surface.positions), resources.dynamicDraw);
    gl.bindBuffer(resources.arrayBuffer, resources.surfaceBuffers.uv);
    gl.bufferData(resources.arrayBuffer, sceneTypedFloatArray(surface && surface.uv), resources.dynamicDraw);
  }

  function bindSceneWebGLSurfaceTexture(gl, resources, record) {
    if (typeof gl.activeTexture === "function") {
      gl.activeTexture(resources.texture0);
    }
    if (typeof gl.bindTexture === "function") {
      gl.bindTexture(resources.texture2D, record.texture);
    }
    if (typeof gl.uniform1i === "function" && resources.surfaceTextureLocation) {
      gl.uniform1i(resources.surfaceTextureLocation, 0);
    }
  }

  function applySceneWebGLSurfaceMaterial(gl, resources, material) {
    const tint = sceneColorRGBA(material && material.color, [1, 1, 1, 1]);
    tint[3] = clamp01(tint[3] * sceneMaterialOpacity(material));
    if (typeof gl.uniform4f === "function" && resources.surfaceTintLocation) {
      gl.uniform4f(resources.surfaceTintLocation, tint[0], tint[1], tint[2], tint[3]);
    }
    if (typeof gl.uniform1f === "function" && resources.surfaceEmissiveLocation) {
      gl.uniform1f(resources.surfaceEmissiveLocation, sceneMaterialEmissive(material));
    }
  }

  function drawSceneWebGLSurface(gl, resources, vertexCount) {
    if (!vertexCount) {
      return;
    }
    gl.bindBuffer(resources.arrayBuffer, resources.surfaceBuffers.position);
    gl.enableVertexAttribArray(resources.surfacePositionLocation);
    gl.vertexAttribPointer(resources.surfacePositionLocation, 3, resources.floatType, false, 0, 0);
    gl.bindBuffer(resources.arrayBuffer, resources.surfaceBuffers.uv);
    gl.enableVertexAttribArray(resources.surfaceUVLocation);
    gl.vertexAttribPointer(resources.surfaceUVLocation, 2, resources.floatType, false, 0, 0);
    gl.drawArrays(resources.trianglesMode, 0, vertexCount);
  }

  function sceneWebGLTextureRecord(gl, resources, src) {
    const key = typeof src === "string" ? src.trim() : "";
    if (!key || !resources || !resources.textureCache) {
      return null;
    }
    if (resources.textureCache.has(key)) {
      return resources.textureCache.get(key);
    }
    const texture = typeof gl.createTexture === "function" ? gl.createTexture() : null;
    const record = { texture, src: key, loaded: false };
    resources.textureCache.set(key, record);
    if (!texture) {
      record.failed = true;
      if (typeof notifySceneTextureLoaded === "function") {
        notifySceneTextureLoaded(key, false);
      }
      return record;
    }
    initializeSceneWebGLTexture(gl, resources, texture);
    const image = createSceneWebGLImage();
    if (!image) {
      record.failed = true;
      if (typeof notifySceneTextureLoaded === "function") {
        notifySceneTextureLoaded(key, false);
      }
      return record;
    }
    image.onload = function() {
      try {
        uploadSceneWebGLTextureImage(gl, resources, texture, image);
        record.loaded = true;
        if (typeof notifySceneTextureLoaded === "function") {
          notifySceneTextureLoaded(key, true);
        }
      } catch (error) {
        record.failed = true;
        record.error = error && error.message ? error.message : String(error);
        if (typeof notifySceneTextureLoaded === "function") {
          notifySceneTextureLoaded(key, false);
        }
      }
    };
    image.onerror = function() {
      record.failed = true;
      record.error = "image decode failed";
      if (typeof notifySceneTextureLoaded === "function") {
        notifySceneTextureLoaded(key, false);
      }
    };
    image.src = key;
    return record;
  }

  function createSceneWebGLImage() {
    if (typeof Image === "function") {
      return new Image();
    }
    return null;
  }

  function initializeSceneWebGLTexture(gl, resources, texture) {
    if (typeof gl.bindTexture !== "function" || typeof gl.texImage2D !== "function") {
      return;
    }
    gl.bindTexture(resources.texture2D, texture);
    if (typeof gl.texParameteri === "function") {
      gl.texParameteri(resources.texture2D, resources.textureMinFilter, resources.linearFilter);
      gl.texParameteri(resources.texture2D, resources.textureMagFilter, resources.linearFilter);
      gl.texParameteri(resources.texture2D, resources.textureWrapS, resources.clampToEdge);
      gl.texParameteri(resources.texture2D, resources.textureWrapT, resources.clampToEdge);
    }
    gl.texImage2D(resources.texture2D, 0, resources.rgbaFormat, 1, 1, 0, resources.rgbaFormat, resources.unsignedByte, new Uint8Array([255, 255, 255, 255]));
  }

  function uploadSceneWebGLTextureImage(gl, resources, texture, image) {
    if (typeof gl.bindTexture !== "function" || typeof gl.texImage2D !== "function") {
      return;
    }
    gl.bindTexture(resources.texture2D, texture);
    if (typeof gl.texParameteri === "function") {
      gl.texParameteri(resources.texture2D, resources.textureMinFilter, resources.linearFilter);
      gl.texParameteri(resources.texture2D, resources.textureMagFilter, resources.linearFilter);
      gl.texParameteri(resources.texture2D, resources.textureWrapS, resources.clampToEdge);
      gl.texParameteri(resources.texture2D, resources.textureWrapT, resources.clampToEdge);
    }
    gl.texImage2D(resources.texture2D, 0, resources.rgbaFormat, resources.rgbaFormat, resources.unsignedByte, image);
  }

  function renderSceneWebGLFallbackBundle(gl, geometry, resources) {
    applySceneWebGLDepth(gl, "disabled", resources.stateCache);
    applySceneWebGLBlend(gl, "opaque", resources.stateCache);
    uploadSceneWebGLBuffers(
      gl,
      resources.arrayBuffer,
      resources.dynamicDraw,
      resources.fallbackBuffers.position,
      resources.fallbackBuffers.color,
      resources.fallbackBuffers.material,
      geometry.positions,
      geometry.colors,
      sceneFallbackMaterialData(geometry.vertexCount),
    );
    drawSceneWebGLLines(
      gl,
      resources.arrayBuffer,
      resources.floatType,
      resources.linesMode,
      resources.positionLocation,
      resources.colorLocation,
      resources.materialLocation,
      resources.fallbackBuffers.position,
      resources.fallbackBuffers.color,
      resources.fallbackBuffers.material,
      geometry.vertexCount,
      geometry.usePerspective ? 3 : 2,
    );
  }

  function disposeSceneWebGLRenderer(gl, program, resources) {
    if (typeof gl.deleteBuffer === "function") {
      deleteSceneWebGLBufferSet(gl, resources.fallbackBuffers);
      deleteSceneWebGLBufferSet(gl, resources.passBuffers.staticOpaque);
      deleteSceneWebGLBufferSet(gl, resources.passBuffers.alpha);
      deleteSceneWebGLBufferSet(gl, resources.passBuffers.additive);
      deleteSceneWebGLBufferSet(gl, resources.passBuffers.dynamicOpaque);
      deleteSceneWebGLSurfaceBufferSet(gl, resources.surfaceBuffers);
    }
    if (resources && resources.textureCache && typeof gl.deleteTexture === "function") {
      for (const record of resources.textureCache.values()) {
        if (record && record.texture) {
          gl.deleteTexture(record.texture);
        }
      }
    }
    if (typeof gl.deleteProgram === "function") {
      gl.deleteProgram(program);
      if (resources && resources.surfaceProgram) {
        gl.deleteProgram(resources.surfaceProgram);
      }
    }
  }

  function createSceneWebGLBufferSet(gl) {
    return {
      position: gl.createBuffer(),
      color: gl.createBuffer(),
      material: gl.createBuffer(),
    };
  }

  function createSceneWebGLSurfaceBufferSet(gl) {
    return {
      position: gl.createBuffer(),
      uv: gl.createBuffer(),
    };
  }

  function deleteSceneWebGLBufferSet(gl, buffers) {
    if (!buffers) {
      return;
    }
    gl.deleteBuffer(buffers.position);
    gl.deleteBuffer(buffers.color);
    gl.deleteBuffer(buffers.material);
  }

  function deleteSceneWebGLSurfaceBufferSet(gl, buffers) {
    if (!buffers) {
      return;
    }
    gl.deleteBuffer(buffers.position);
    gl.deleteBuffer(buffers.uv);
  }

  function createSceneWorldWebGLPasses(drawPlan, buffers, usages) {
    const passes = [];
    passes.push({
      name: "staticOpaque",
      blend: "opaque",
      depth: "opaque",
      usage: usages.staticDraw,
      cacheSlot: "staticOpaque",
      cacheKey: drawPlan.staticOpaqueKey,
      buffers: buffers.staticOpaque,
      positions: drawPlan.staticOpaquePositions,
      colors: drawPlan.staticOpaqueColors,
      materials: drawPlan.staticOpaqueMaterials,
      vertexCount: drawPlan.staticOpaqueVertexCount,
    });
    passes.push({
      name: "dynamicOpaque",
      blend: "opaque",
      depth: "opaque",
      usage: usages.dynamicDraw,
      buffers: buffers.dynamicOpaque,
      positions: drawPlan.dynamicOpaquePositions,
      colors: drawPlan.dynamicOpaqueColors,
      materials: drawPlan.dynamicOpaqueMaterials,
      vertexCount: drawPlan.dynamicOpaqueVertexCount,
    });
    if (drawPlan.hasAlphaPass) {
      passes.push({
        name: "alpha",
        blend: "alpha",
        depth: "translucent",
        usage: usages.dynamicDraw,
        buffers: buffers.alpha,
        positions: drawPlan.alphaPositions,
        colors: drawPlan.alphaColors,
        materials: drawPlan.alphaMaterials,
        vertexCount: drawPlan.alphaVertexCount,
      });
    }
    if (drawPlan.hasAdditivePass) {
      passes.push({
        name: "additive",
        blend: "additive",
        depth: "translucent",
        usage: usages.dynamicDraw,
        buffers: buffers.additive,
        positions: drawPlan.additivePositions,
        colors: drawPlan.additiveColors,
        materials: drawPlan.additiveMaterials,
        vertexCount: drawPlan.additiveVertexCount,
      });
    }
    return passes;
  }

  function createSceneWorldWebGLPassesFromBundle(bundle, buffers, usages) {
    const sourcePasses = Array.isArray(bundle && bundle.passes) ? bundle.passes : [];
    const passes = [];
    for (const source of sourcePasses) {
      const pass = sceneWorldWebGLPassFromSource(source, buffers, usages);
      if (pass) {
        passes.push(pass);
      }
    }
    return passes;
  }

  function sceneWorldWebGLPassFromSource(source, buffers, usages) {
    const name = sceneWorldWebGLPassName(source);
    if (!name) {
      return null;
    }
    const targetBuffers = buffers[name];
    if (!targetBuffers) {
      return null;
    }
    const isStatic = Boolean(source && source.static);
    const positions = sceneTypedFloatArray(source && source.positions);
    const colors = sceneTypedFloatArray(source && source.colors);
    const materials = sceneTypedFloatArray(source && source.materials);
    return {
      name,
      blend: sceneWorldWebGLPassMode(source && source.blend, "opaque"),
      depth: sceneWorldWebGLPassMode(source && source.depth, "opaque"),
      usage: isStatic ? usages.staticDraw : usages.dynamicDraw,
      cacheSlot: sceneWorldWebGLPassCacheSlot(name, isStatic),
      cacheKey: String(source && source.cacheKey || ""),
      buffers: targetBuffers,
      positions,
      colors,
      materials,
      vertexCount: sceneWorldWebGLPassVertexCount(source, positions, colors, materials),
    };
  }

  function sceneWorldWebGLPassName(source) {
    return String(source && source.name || "");
  }

  function sceneWorldWebGLPassMode(value, fallback) {
    const mode = String(value || fallback);
    return mode || fallback;
  }

  function sceneWorldWebGLPassCacheSlot(name, isStatic) {
    if (!isStatic) {
      return "";
    }
    return name;
  }

  function sceneWorldWebGLPassVertexCount(source, positions, colors, materials) {
    const requested = Math.max(0, Math.floor(sceneNumber(source && source.vertexCount, NaN)));
    const positionCount = Math.floor((positions && positions.length || 0) / 3);
    const colorCount = Math.floor((colors && colors.length || 0) / 4);
    const materialCount = Math.floor((materials && materials.length || 0) / 3);
    const maxCount = Math.max(0, Math.min(positionCount, colorCount, materialCount));
    if (Number.isFinite(requested)) {
      return Math.min(requested, maxCount);
    }
    return maxCount;
  }

  function drawSceneWebGLPasses(gl, arrayBuffer, floatType, linesMode, positionLocation, colorLocation, materialLocation, passes, cache, stateCache) {
    for (const pass of passes) {
      const vertexCount = uploadSceneWebGLPass(gl, arrayBuffer, pass, cache);
      if (!vertexCount) {
        continue;
      }
      applySceneWebGLDepth(gl, pass.depth, stateCache);
      applySceneWebGLBlend(gl, pass.blend, stateCache);
      drawSceneWebGLLines(gl, arrayBuffer, floatType, linesMode, positionLocation, colorLocation, materialLocation, pass.buffers.position, pass.buffers.color, pass.buffers.material, vertexCount, 3);
    }
  }

  function uploadSceneWebGLPass(gl, arrayBuffer, pass, cache) {
    if (!pass || !pass.buffers) {
      return 0;
    }
    if (pass.cacheSlot) {
      const record = cache[pass.cacheSlot] || (cache[pass.cacheSlot] = { key: "", vertexCount: 0 });
      if (record.key !== pass.cacheKey) {
        uploadSceneWebGLBuffers(gl, arrayBuffer, pass.usage, pass.buffers.position, pass.buffers.color, pass.buffers.material, pass.positions, pass.colors, pass.materials);
        record.key = pass.cacheKey;
        record.vertexCount = pass.vertexCount;
      }
      return record.vertexCount;
    }
    if (!pass.vertexCount) {
      return 0;
    }
    uploadSceneWebGLBuffers(gl, arrayBuffer, pass.usage, pass.buffers.position, pass.buffers.color, pass.buffers.material, pass.positions, pass.colors, pass.materials);
    return pass.vertexCount;
  }

  function uploadSceneWebGLBuffers(gl, arrayBuffer, usage, positionBuffer, colorBuffer, materialBuffer, positions, colors, materials) {
    gl.bindBuffer(arrayBuffer, positionBuffer);
    gl.bufferData(arrayBuffer, positions, usage);
    gl.bindBuffer(arrayBuffer, colorBuffer);
    gl.bufferData(arrayBuffer, colors, usage);
    gl.bindBuffer(arrayBuffer, materialBuffer);
    gl.bufferData(arrayBuffer, materials, usage);
  }

  function drawSceneWebGLLines(gl, arrayBuffer, floatType, linesMode, positionLocation, colorLocation, materialLocation, positionBuffer, colorBuffer, materialBuffer, vertexCount, positionSize) {
    drawSceneWebGLPrimitives(gl, arrayBuffer, floatType, linesMode, positionLocation, colorLocation, materialLocation, positionBuffer, colorBuffer, materialBuffer, vertexCount, positionSize);
  }

  function drawSceneWebGLPrimitives(gl, arrayBuffer, floatType, drawMode, positionLocation, colorLocation, materialLocation, positionBuffer, colorBuffer, materialBuffer, vertexCount, positionSize) {
    if (!vertexCount) {
      return;
    }
    gl.bindBuffer(arrayBuffer, positionBuffer);
    gl.enableVertexAttribArray(positionLocation);
    gl.vertexAttribPointer(positionLocation, positionSize, floatType, false, 0, 0);

    gl.bindBuffer(arrayBuffer, colorBuffer);
    gl.enableVertexAttribArray(colorLocation);
    gl.vertexAttribPointer(colorLocation, 4, floatType, false, 0, 0);

    gl.bindBuffer(arrayBuffer, materialBuffer);
    gl.enableVertexAttribArray(materialLocation);
    gl.vertexAttribPointer(materialLocation, 3, floatType, false, 0, 0);

    gl.drawArrays(drawMode, 0, vertexCount);
  }

  function applySceneWebGLBlend(gl, mode, stateCache) {
    if (sceneWebGLStateUnchanged(stateCache, "blendMode", mode)) {
      return;
    }
    const blendConst = typeof gl.BLEND === "number" ? gl.BLEND : 0x0BE2;
    const one = typeof gl.ONE === "number" ? gl.ONE : 1;
    const srcAlpha = typeof gl.SRC_ALPHA === "number" ? gl.SRC_ALPHA : 0x0302;
    const oneMinusSrcAlpha = typeof gl.ONE_MINUS_SRC_ALPHA === "number" ? gl.ONE_MINUS_SRC_ALPHA : 0x0303;
    const config = sceneWebGLBlendConfig(mode, srcAlpha, oneMinusSrcAlpha, one);
    rememberSceneWebGLState(stateCache, "blendMode", mode);
    setSceneWebGLCapability(gl, blendConst, config.enabled);
    if (config.enabled && typeof gl.blendFunc === "function") {
      gl.blendFunc(config.src, config.dst);
    }
  }

  function applySceneWebGLDepth(gl, mode, stateCache) {
    if (sceneWebGLStateUnchanged(stateCache, "depthMode", mode)) {
      return;
    }
    const depthTest = typeof gl.DEPTH_TEST === "number" ? gl.DEPTH_TEST : 0x0B71;
    const lequal = typeof gl.LEQUAL === "number" ? gl.LEQUAL : 0x0203;
    const config = sceneWebGLDepthConfig(mode);
    rememberSceneWebGLState(stateCache, "depthMode", mode);
    setSceneWebGLCapability(gl, depthTest, config.enabled);
    if (!config.enabled) {
      return;
    }
    if (typeof gl.depthFunc === "function") {
      gl.depthFunc(lequal);
    }
    if (typeof gl.depthMask === "function") {
      gl.depthMask(config.mask);
    }
  }

  function sceneWebGLStateUnchanged(stateCache, key, mode) {
    return Boolean(stateCache && stateCache[key] === mode);
  }

  function rememberSceneWebGLState(stateCache, key, mode) {
    if (!stateCache) {
      return;
    }
    stateCache[key] = mode;
  }

  function setSceneWebGLCapability(gl, capability, enabled) {
    if (enabled) {
      if (typeof gl.enable === "function") {
        gl.enable(capability);
      }
      return;
    }
    if (typeof gl.disable === "function") {
      gl.disable(capability);
    }
  }

  function sceneWebGLBlendConfig(mode, srcAlpha, oneMinusSrcAlpha, one) {
    switch (mode) {
    case "alpha":
      return { enabled: true, src: srcAlpha, dst: oneMinusSrcAlpha };
    case "additive":
      return { enabled: true, src: srcAlpha, dst: one };
    default:
      return { enabled: false };
    }
  }

  function sceneWebGLDepthConfig(mode) {
    switch (mode) {
    case "opaque":
      return { enabled: true, mask: true };
    case "translucent":
      return { enabled: true, mask: false };
    default:
      return { enabled: false, mask: false };
    }
  }








  function createSceneWebGLProgram(gl) {
    const vertexSource = [
      "attribute vec3 a_position;",
      "attribute vec4 a_color;",
      "attribute vec3 a_material;",
      "uniform vec4 u_camera;",
      "uniform vec3 u_camera_rotation;",
      "uniform vec2 u_depth_range;",
      "uniform float u_aspect;",
      "uniform float u_use_perspective;",
      "uniform float u_camera_mode;",
      "uniform vec4 u_ortho;",
      "varying vec4 v_color;",
      "varying vec3 v_material;",
      "vec3 inverseRotatePoint(vec3 point, vec3 rotation) {",
      "  float sinZ = sin(-rotation.z);",
      "  float cosZ = cos(-rotation.z);",
      "  float nextX = point.x * cosZ - point.y * sinZ;",
      "  float nextY = point.x * sinZ + point.y * cosZ;",
      "  point = vec3(nextX, nextY, point.z);",
      "  float sinY = sin(-rotation.y);",
      "  float cosY = cos(-rotation.y);",
      "  nextX = point.x * cosY + point.z * sinY;",
      "  float nextZ = -point.x * sinY + point.z * cosY;",
      "  point = vec3(nextX, point.y, nextZ);",
      "  float sinX = sin(-rotation.x);",
      "  float cosX = cos(-rotation.x);",
      "  nextY = point.y * cosX - point.z * sinX;",
      "  nextZ = point.y * sinX + point.z * cosX;",
      "  return vec3(point.x, nextY, nextZ);",
      "}",
      "void main() {",
      "  vec4 clip = vec4(a_position.xy, 0.0, 1.0);",
      "  if (u_use_perspective > 0.5) {",
      "    vec3 local = inverseRotatePoint(vec3(a_position.x - u_camera.x, a_position.y - u_camera.y, a_position.z - u_camera.z), u_camera_rotation);",
      "    float depth = -local.z;",
      "    float nearDepth = max(u_depth_range.x, 0.0001);",
      "    float farDepth = max(u_depth_range.y, nearDepth + 0.0001);",
      "    if (u_camera_mode > 0.5) {",
      "      float ox = ((local.x - u_ortho.x) / max(u_ortho.y - u_ortho.x, 0.0001)) * 2.0 - 1.0;",
      "      float oy = ((local.y - u_ortho.w) / max(u_ortho.z - u_ortho.w, 0.0001)) * 2.0 - 1.0;",
      "      float clipDepth = ((depth - nearDepth) / max(farDepth - nearDepth, 0.0001)) * 2.0 - 1.0;",
      "      clip = vec4(ox, oy, clipDepth, 1.0);",
      "    } else {",
      "      float focal = 1.0 / tan(radians(u_camera.w) * 0.5);",
      "      float rangeInv = 1.0 / (nearDepth - farDepth);",
      "      float clipZ = ((nearDepth + farDepth) * rangeInv) * local.z + (2.0 * nearDepth * farDepth * rangeInv);",
      "      clip = vec4(local.x * focal / max(u_aspect, 0.0001), local.y * focal, clipZ, depth);",
      "    }",
      "  }",
      "  gl_Position = clip;",
      "  v_color = a_color;",
      "  v_material = a_material;",
      "}",
    ].join("\n");
    const fragmentSource = [
      "precision mediump float;",
      "varying vec4 v_color;",
      "varying vec3 v_material;",
      "void main() {",
      "  vec4 color = v_color;",
      "  float kind = floor(v_material.x + 0.5);",
      "  float emissive = max(v_material.y, 0.0);",
      "  float tone = clamp(v_material.z, 0.0, 1.0);",
      "  if (kind > 3.5) {",
      "    color.rgb *= mix(0.78, 1.0, tone);",
      "  } else if (kind > 2.5) {",
      "    color.rgb *= 1.0 + emissive * 0.75;",
      "  } else if (kind > 1.5) {",
      "    color.rgb = mix(color.rgb, vec3(0.92, 0.98, 1.0), 0.28 + tone * 0.16);",
      "    color.a *= 0.84;",
      "  } else if (kind > 0.5) {",
      "    color.rgb = mix(color.rgb, vec3(0.84, 0.94, 1.0), 0.18 + tone * 0.12);",
      "    color.a *= 0.9;",
      "  } else {",
      "    color.rgb *= mix(0.9, 1.0, tone);",
      "  }",
      "  gl_FragColor = vec4(clamp(color.rgb, 0.0, 1.0), clamp(color.a, 0.0, 1.0));",
      "}",
    ].join("\n");

    const vertexShader = createSceneShader(gl, gl.VERTEX_SHADER, vertexSource);
    const fragmentShader = createSceneShader(gl, gl.FRAGMENT_SHADER, fragmentSource);
    if (!vertexShader || !fragmentShader) {
      return null;
    }

    const program = gl.createProgram();
    gl.attachShader(program, vertexShader);
    gl.attachShader(program, fragmentShader);
    gl.linkProgram(program);
    if (!gl.getProgramParameter(program, gl.LINK_STATUS)) {
      console.warn("[gosx] Scene3D WebGL link failed");
      return null;
    }
    return program;
  }

  function createSceneWebGLSurfaceProgram(gl) {
    const vertexSource = [
      "attribute vec3 a_position;",
      "attribute vec2 a_uv;",
      "uniform vec4 u_camera;",
      "uniform vec3 u_camera_rotation;",
      "uniform vec2 u_depth_range;",
      "uniform float u_aspect;",
      "uniform float u_camera_mode;",
      "uniform vec4 u_ortho;",
      "varying vec2 v_uv;",
      "vec3 inverseRotatePoint(vec3 point, vec3 rotation) {",
      "  float sinZ = sin(-rotation.z);",
      "  float cosZ = cos(-rotation.z);",
      "  float nextX = point.x * cosZ - point.y * sinZ;",
      "  float nextY = point.x * sinZ + point.y * cosZ;",
      "  point = vec3(nextX, nextY, point.z);",
      "  float sinY = sin(-rotation.y);",
      "  float cosY = cos(-rotation.y);",
      "  nextX = point.x * cosY + point.z * sinY;",
      "  float nextZ = -point.x * sinY + point.z * cosY;",
      "  point = vec3(nextX, point.y, nextZ);",
      "  float sinX = sin(-rotation.x);",
      "  float cosX = cos(-rotation.x);",
      "  nextY = point.y * cosX - point.z * sinX;",
      "  nextZ = point.y * sinX + point.z * cosX;",
      "  return vec3(point.x, nextY, nextZ);",
      "}",
      "void main() {",
      "  vec3 local = inverseRotatePoint(vec3(a_position.x - u_camera.x, a_position.y - u_camera.y, a_position.z - u_camera.z), u_camera_rotation);",
      "  float depth = -local.z;",
      "  float nearDepth = max(u_depth_range.x, 0.0001);",
      "  float farDepth = max(u_depth_range.y, nearDepth + 0.0001);",
      "  if (u_camera_mode > 0.5) {",
      "    float ox = ((local.x - u_ortho.x) / max(u_ortho.y - u_ortho.x, 0.0001)) * 2.0 - 1.0;",
      "    float oy = ((local.y - u_ortho.w) / max(u_ortho.z - u_ortho.w, 0.0001)) * 2.0 - 1.0;",
      "    float clipDepth = ((depth - nearDepth) / max(farDepth - nearDepth, 0.0001)) * 2.0 - 1.0;",
      "    gl_Position = vec4(ox, oy, clipDepth, 1.0);",
      "  } else {",
      "    float focal = 1.0 / tan(radians(u_camera.w) * 0.5);",
      "    float rangeInv = 1.0 / (nearDepth - farDepth);",
      "    float clipZ = ((nearDepth + farDepth) * rangeInv) * local.z + (2.0 * nearDepth * farDepth * rangeInv);",
      "    gl_Position = vec4(local.x * focal / max(u_aspect, 0.0001), local.y * focal, clipZ, depth);",
      "  }",
      "  v_uv = a_uv;",
      "}",
    ].join("\n");
    const fragmentSource = [
      "precision mediump float;",
      "varying vec2 v_uv;",
      "uniform sampler2D u_texture;",
      "uniform vec4 u_tint;",
      "uniform float u_emissive;",
      "void main() {",
      "  vec4 sampleColor = texture2D(u_texture, v_uv);",
      "  vec3 rgb = sampleColor.rgb * u_tint.rgb;",
      "  rgb *= 1.0 + max(u_emissive, 0.0) * 0.5;",
      "  gl_FragColor = vec4(clamp(rgb, 0.0, 1.0), clamp(sampleColor.a * u_tint.a, 0.0, 1.0));",
      "}",
    ].join("\n");

    const vertexShader = createSceneShader(gl, gl.VERTEX_SHADER, vertexSource);
    const fragmentShader = createSceneShader(gl, gl.FRAGMENT_SHADER, fragmentSource);
    if (!vertexShader || !fragmentShader) {
      return null;
    }

    const program = gl.createProgram();
    gl.attachShader(program, vertexShader);
    gl.attachShader(program, fragmentShader);
    gl.linkProgram(program);
    if (!gl.getProgramParameter(program, gl.LINK_STATUS)) {
      console.warn("[gosx] Scene3D WebGL surface link failed");
      return null;
    }
    return program;
  }

  function createSceneShader(gl, type, source) {
    const shader = gl.createShader(type);
    gl.shaderSource(shader, source);
    gl.compileShader(shader);
    if (!gl.getShaderParameter(shader, gl.COMPILE_STATUS)) {
      console.warn("[gosx] Scene3D WebGL shader compile failed");
      return null;
    }
    return shader;
  }

  // ---- Thick line WebGL program (three.js Line2-style vertex expansion) ----
  //
  // gl.LINES respects only hairline widths on almost every driver. To honor
  // scene.LinesGeometry.Width on WebGL we expand each segment (A, B) into a
  // screen-space quad (4 vertices, 2 triangles) in the vertex shader and
  // offset each vertex perpendicular to the line's screen-space direction
  // by (side * width * 0.5) pixels.
  //
  // The projection math mirrors the legacy createSceneWebGLProgram path so
  // thick lines align exactly with whatever else the legacy world renderer
  // draws. u_viewport is the render target size in pixels (canvas.width /
  // canvas.height) and is used to convert a pixel-space offset back into
  // clip-space via multiplication by clip.w.
  function createSceneThickLineProgram(gl) {
    const vertexSource = [
      "attribute vec3 a_positionA;",
      "attribute vec3 a_positionB;",
      "attribute vec4 a_colorA;",
      "attribute vec4 a_colorB;",
      "attribute float a_side;",
      "attribute float a_endpoint;",
      "attribute float a_width;",
      "uniform vec4 u_camera;",
      "uniform vec3 u_camera_rotation;",
      "uniform vec2 u_depth_range;",
      "uniform float u_aspect;",
      "uniform vec2 u_viewport;",
      "uniform float u_camera_mode;",
      "uniform vec4 u_ortho;",
      "varying vec4 v_color;",
      "vec3 inverseRotatePoint(vec3 point, vec3 rotation) {",
      "  float sinZ = sin(-rotation.z);",
      "  float cosZ = cos(-rotation.z);",
      "  float nextX = point.x * cosZ - point.y * sinZ;",
      "  float nextY = point.x * sinZ + point.y * cosZ;",
      "  point = vec3(nextX, nextY, point.z);",
      "  float sinY = sin(-rotation.y);",
      "  float cosY = cos(-rotation.y);",
      "  nextX = point.x * cosY + point.z * sinY;",
      "  float nextZ = -point.x * sinY + point.z * cosY;",
      "  point = vec3(nextX, point.y, nextZ);",
      "  float sinX = sin(-rotation.x);",
      "  float cosX = cos(-rotation.x);",
      "  nextY = point.y * cosX - point.z * sinX;",
      "  nextZ = point.y * sinX + point.z * cosX;",
      "  return vec3(point.x, nextY, nextZ);",
      "}",
      "vec4 projectEndpoint(vec3 world) {",
      "  vec3 local = inverseRotatePoint(vec3(world.x - u_camera.x, world.y - u_camera.y, world.z - u_camera.z), u_camera_rotation);",
      "  float depth = -local.z;",
      "  float nearDepth = max(u_depth_range.x, 0.0001);",
      "  float farDepth = max(u_depth_range.y, nearDepth + 0.0001);",
      "  if (u_camera_mode > 0.5) {",
      "    float ox = ((local.x - u_ortho.x) / max(u_ortho.y - u_ortho.x, 0.0001)) * 2.0 - 1.0;",
      "    float oy = ((local.y - u_ortho.w) / max(u_ortho.z - u_ortho.w, 0.0001)) * 2.0 - 1.0;",
      "    float clipDepth = ((depth - nearDepth) / max(farDepth - nearDepth, 0.0001)) * 2.0 - 1.0;",
      "    return vec4(ox, oy, clipDepth, 1.0);",
      "  }",
      "  float focal = 1.0 / tan(radians(u_camera.w) * 0.5);",
      "  float rangeInv = 1.0 / (nearDepth - farDepth);",
      "  float clipZ = ((nearDepth + farDepth) * rangeInv) * local.z + (2.0 * nearDepth * farDepth * rangeInv);",
      "  return vec4(local.x * focal / max(u_aspect, 0.0001), local.y * focal, clipZ, depth);",
      "}",
      "void main() {",
      "  vec4 clipA = projectEndpoint(a_positionA);",
      "  vec4 clipB = projectEndpoint(a_positionB);",
      "  vec4 base = mix(clipA, clipB, a_endpoint);",
      // Compute the screen-space direction using post-divide NDC positions
      // scaled by half the viewport. Short-segment guard prevents a NaN when
      // both endpoints collapse onto the same pixel.
      "  vec2 ndcA = clipA.xy / max(clipA.w, 0.0001);",
      "  vec2 ndcB = clipB.xy / max(clipB.w, 0.0001);",
      "  vec2 screenA = ndcA * (u_viewport * 0.5);",
      "  vec2 screenB = ndcB * (u_viewport * 0.5);",
      "  vec2 dir = screenB - screenA;",
      "  float len = length(dir);",
      "  if (len < 0.0001) {",
      "    dir = vec2(1.0, 0.0);",
      "  } else {",
      "    dir = dir / len;",
      "  }",
      "  vec2 normal = vec2(-dir.y, dir.x);",
      // Offset = (side * width/2) pixels → NDC via division by half-viewport
      // → clip space via multiplication by base.w.
      "  vec2 pixelOffset = normal * (a_side * a_width * 0.5);",
      "  vec2 ndcOffset = pixelOffset / max(u_viewport * 0.5, vec2(0.0001));",
      "  vec2 clipOffset = ndcOffset * base.w;",
      "  gl_Position = base + vec4(clipOffset, 0.0, 0.0);",
      "  v_color = mix(a_colorA, a_colorB, a_endpoint);",
      "}",
    ].join("\n");

    const fragmentSource = [
      "precision mediump float;",
      "varying vec4 v_color;",
      "void main() {",
      "  gl_FragColor = vec4(clamp(v_color.rgb, 0.0, 1.0), clamp(v_color.a, 0.0, 1.0));",
      "}",
    ].join("\n");

    const vertexShader = createSceneShader(gl, gl.VERTEX_SHADER, vertexSource);
    const fragmentShader = createSceneShader(gl, gl.FRAGMENT_SHADER, fragmentSource);
    if (!vertexShader || !fragmentShader) {
      return null;
    }
    const program = gl.createProgram();
    gl.attachShader(program, vertexShader);
    gl.attachShader(program, fragmentShader);
    gl.linkProgram(program);
    if (!gl.getProgramParameter(program, gl.LINK_STATUS)) {
      console.warn("[gosx] Scene3D thick-line link failed");
      return null;
    }
    return {
      program,
      positionALocation: gl.getAttribLocation(program, "a_positionA"),
      positionBLocation: gl.getAttribLocation(program, "a_positionB"),
      colorALocation: gl.getAttribLocation(program, "a_colorA"),
      colorBLocation: gl.getAttribLocation(program, "a_colorB"),
      sideLocation: gl.getAttribLocation(program, "a_side"),
      endpointLocation: gl.getAttribLocation(program, "a_endpoint"),
      widthLocation: gl.getAttribLocation(program, "a_width"),
      cameraLocation: gl.getUniformLocation(program, "u_camera"),
      cameraRotationLocation: gl.getUniformLocation(program, "u_camera_rotation"),
      depthRangeLocation: gl.getUniformLocation(program, "u_depth_range"),
      aspectLocation: gl.getUniformLocation(program, "u_aspect"),
      viewportLocation: gl.getUniformLocation(program, "u_viewport"),
      cameraModeLocation: gl.getUniformLocation(program, "u_camera_mode"),
      orthoLocation: gl.getUniformLocation(program, "u_ortho"),
    };
  }

  // Pooled scratch for thick-line vertex expansion. Lives on the renderer
  // resources (resources.thickLineScratch) so it's reused across frames —
  // the previous implementation allocated 8 fresh typed arrays per frame
  // for any scene with thick lines, which on a 60 fps particle-effect
  // scene burned 480 typed-array allocations per second on the GC heap.
  //
  // Growth strategy: geometric (2× current capacity) up to the current
  // segment count. Never shrinks — sustained peak usage stays mapped.
  //
  // Quad layout per segment (4 vertices, 2 triangles):
  //
  //     endpoint=0,side=-1   endpoint=1,side=-1
  //              *──────────*
  //              │          │
  //              │          │
  //              *──────────*
  //     endpoint=0,side=+1   endpoint=1,side=+1
  //
  // All 4 vertices carry the full (positionA, positionB, colorA, colorB)
  // pair so the vertex shader can compute the screen-space direction
  // without touching the index buffer.
  function createSceneThickLineScratch() {
    return {
      segmentCapacity: 0,
      positionsA: new Float32Array(0),
      positionsB: new Float32Array(0),
      colorsA: new Float32Array(0),
      colorsB: new Float32Array(0),
      sides: new Float32Array(0),
      endpoints: new Float32Array(0),
      widths: new Float32Array(0),
      // Three pooled index buffers — one per render pass. Writing them
      // separately lets the draw path issue up to three drawElements
      // calls with different blend/depth states so additive thick lines
      // composite correctly against opaque and alpha passes.
      opaqueIndices: new Uint16Array(0),
      alphaIndices: new Uint16Array(0),
      additiveIndices: new Uint16Array(0),
      opaqueIndexCount: 0,
      alphaIndexCount: 0,
      additiveIndexCount: 0,
    };
  }

  function ensureSceneThickLineScratchCapacity(scratch, segmentCount) {
    if (scratch.segmentCapacity >= segmentCount) {
      return;
    }
    const nextCapacity = Math.max(64, Math.max(scratch.segmentCapacity * 2, segmentCount));
    const totalVerts = nextCapacity * 4;
    scratch.positionsA = new Float32Array(totalVerts * 3);
    scratch.positionsB = new Float32Array(totalVerts * 3);
    scratch.colorsA = new Float32Array(totalVerts * 4);
    scratch.colorsB = new Float32Array(totalVerts * 4);
    scratch.sides = new Float32Array(totalVerts);
    scratch.endpoints = new Float32Array(totalVerts);
    scratch.widths = new Float32Array(totalVerts);
    // Worst case: all segments belong to one pass. Sized for that.
    scratch.opaqueIndices = new Uint16Array(nextCapacity * 6);
    scratch.alphaIndices = new Uint16Array(nextCapacity * 6);
    scratch.additiveIndices = new Uint16Array(nextCapacity * 6);
    scratch.segmentCapacity = nextCapacity;
  }

  // Quad corner layout: shared constants hoisted out of the hot loop.
  // Vertex indices inside a quad: 0 = A-, 1 = A+, 2 = B+, 3 = B-.
  // Triangles: (0,1,2) and (0,2,3).
  const _thickLineQuadEndpoints = [0, 0, 1, 1];
  const _thickLineQuadSides = [-1, 1, 1, -1];

  // expandSceneThickLineIntoScratch walks world line data, writes the
  // expanded per-quad attribute values into pooled scratch, and assigns
  // each segment's 6 triangle indices to the scratch index buffer
  // matching its render pass (0=opaque, 1=alpha, 2=additive). One linear
  // pass through worldPositions — no sorting, no allocations.
  //
  // Returns the total segment count actually processed (may be less than
  // the input when the 16384-segment Uint16 cap is hit; the overflow
  // guard in the draw path routes those scenes back to gl.LINES).
  function expandSceneThickLineIntoScratch(scratch, worldPositions, worldColors, worldLineWidths, worldLinePasses, segmentCount) {
    const safeCount = Math.min(segmentCount, 16384);
    ensureSceneThickLineScratchCapacity(scratch, safeCount);

    const positionsA = scratch.positionsA;
    const positionsB = scratch.positionsB;
    const colorsA = scratch.colorsA;
    const colorsB = scratch.colorsB;
    const sides = scratch.sides;
    const endpoints = scratch.endpoints;
    const widths = scratch.widths;
    const opaqueIndices = scratch.opaqueIndices;
    const alphaIndices = scratch.alphaIndices;
    const additiveIndices = scratch.additiveIndices;

    let opaqueIdx = 0;
    let alphaIdx = 0;
    let additiveIdx = 0;

    for (let seg = 0; seg < safeCount; seg += 1) {
      const posOffset = seg * 6;
      const colorOffset = seg * 8;
      const ax = worldPositions[posOffset];
      const ay = worldPositions[posOffset + 1];
      const az = worldPositions[posOffset + 2];
      const bx = worldPositions[posOffset + 3];
      const by = worldPositions[posOffset + 4];
      const bz = worldPositions[posOffset + 5];
      const caR = worldColors[colorOffset];
      const caG = worldColors[colorOffset + 1];
      const caB = worldColors[colorOffset + 2];
      const caA = worldColors[colorOffset + 3];
      const cbR = worldColors[colorOffset + 4];
      const cbG = worldColors[colorOffset + 5];
      const cbB = worldColors[colorOffset + 6];
      const cbA = worldColors[colorOffset + 7];
      const width = (worldLineWidths && worldLineWidths[seg] > 0) ? worldLineWidths[seg] : 1;

      for (let corner = 0; corner < 4; corner += 1) {
        const vi = seg * 4 + corner;
        const p3 = vi * 3;
        const p4 = vi * 4;
        positionsA[p3] = ax;
        positionsA[p3 + 1] = ay;
        positionsA[p3 + 2] = az;
        positionsB[p3] = bx;
        positionsB[p3 + 1] = by;
        positionsB[p3 + 2] = bz;
        colorsA[p4] = caR;
        colorsA[p4 + 1] = caG;
        colorsA[p4 + 2] = caB;
        colorsA[p4 + 3] = caA;
        colorsB[p4] = cbR;
        colorsB[p4 + 1] = cbG;
        colorsB[p4 + 2] = cbB;
        colorsB[p4 + 3] = cbA;
        sides[vi] = _thickLineQuadSides[corner];
        endpoints[vi] = _thickLineQuadEndpoints[corner];
        widths[vi] = width;
      }

      const base = seg * 4;
      const pass = (worldLinePasses && seg < worldLinePasses.length) ? worldLinePasses[seg] : 0;
      if (pass === 2) {
        additiveIndices[additiveIdx] = base;
        additiveIndices[additiveIdx + 1] = base + 1;
        additiveIndices[additiveIdx + 2] = base + 2;
        additiveIndices[additiveIdx + 3] = base;
        additiveIndices[additiveIdx + 4] = base + 2;
        additiveIndices[additiveIdx + 5] = base + 3;
        additiveIdx += 6;
      } else if (pass === 1) {
        alphaIndices[alphaIdx] = base;
        alphaIndices[alphaIdx + 1] = base + 1;
        alphaIndices[alphaIdx + 2] = base + 2;
        alphaIndices[alphaIdx + 3] = base;
        alphaIndices[alphaIdx + 4] = base + 2;
        alphaIndices[alphaIdx + 5] = base + 3;
        alphaIdx += 6;
      } else {
        opaqueIndices[opaqueIdx] = base;
        opaqueIndices[opaqueIdx + 1] = base + 1;
        opaqueIndices[opaqueIdx + 2] = base + 2;
        opaqueIndices[opaqueIdx + 3] = base;
        opaqueIndices[opaqueIdx + 4] = base + 2;
        opaqueIndices[opaqueIdx + 5] = base + 3;
        opaqueIdx += 6;
      }
    }

    scratch.opaqueIndexCount = opaqueIdx;
    scratch.alphaIndexCount = alphaIdx;
    scratch.additiveIndexCount = additiveIdx;
    return safeCount;
  }

  function createSceneThickLineBufferSet(gl) {
    return {
      positionA: gl.createBuffer(),
      positionB: gl.createBuffer(),
      colorA: gl.createBuffer(),
      colorB: gl.createBuffer(),
      side: gl.createBuffer(),
      endpoint: gl.createBuffer(),
      width: gl.createBuffer(),
      // One GL index buffer per render pass so the draw path can swap
      // between them without re-uploading index data.
      opaqueIndex: gl.createBuffer(),
      alphaIndex: gl.createBuffer(),
      additiveIndex: gl.createBuffer(),
    };
  }

  // Uploads the expanded vertex attributes once (shared across all passes)
  // and uploads each non-empty pass's index buffer. Index uploads are
  // clipped to the used length via subarray so a 16k-capacity scratch
  // running 128 opaque segments only pushes 768 u16s to the GPU, not 96k.
  function uploadSceneThickLineBuffers(gl, resources, scratch, segmentCount) {
    const buffers = resources.thickLineBuffers;
    const arrayBuffer = resources.arrayBuffer;
    const elementArrayBuffer = typeof gl.ELEMENT_ARRAY_BUFFER === "number" ? gl.ELEMENT_ARRAY_BUFFER : 0x8893;
    const usedVerts = segmentCount * 4;

    gl.bindBuffer(arrayBuffer, buffers.positionA);
    gl.bufferData(arrayBuffer, scratch.positionsA.subarray(0, usedVerts * 3), resources.dynamicDraw);
    gl.bindBuffer(arrayBuffer, buffers.positionB);
    gl.bufferData(arrayBuffer, scratch.positionsB.subarray(0, usedVerts * 3), resources.dynamicDraw);
    gl.bindBuffer(arrayBuffer, buffers.colorA);
    gl.bufferData(arrayBuffer, scratch.colorsA.subarray(0, usedVerts * 4), resources.dynamicDraw);
    gl.bindBuffer(arrayBuffer, buffers.colorB);
    gl.bufferData(arrayBuffer, scratch.colorsB.subarray(0, usedVerts * 4), resources.dynamicDraw);
    gl.bindBuffer(arrayBuffer, buffers.side);
    gl.bufferData(arrayBuffer, scratch.sides.subarray(0, usedVerts), resources.dynamicDraw);
    gl.bindBuffer(arrayBuffer, buffers.endpoint);
    gl.bufferData(arrayBuffer, scratch.endpoints.subarray(0, usedVerts), resources.dynamicDraw);
    gl.bindBuffer(arrayBuffer, buffers.width);
    gl.bufferData(arrayBuffer, scratch.widths.subarray(0, usedVerts), resources.dynamicDraw);

    if (scratch.opaqueIndexCount > 0) {
      gl.bindBuffer(elementArrayBuffer, buffers.opaqueIndex);
      gl.bufferData(elementArrayBuffer, scratch.opaqueIndices.subarray(0, scratch.opaqueIndexCount), resources.dynamicDraw);
    }
    if (scratch.alphaIndexCount > 0) {
      gl.bindBuffer(elementArrayBuffer, buffers.alphaIndex);
      gl.bufferData(elementArrayBuffer, scratch.alphaIndices.subarray(0, scratch.alphaIndexCount), resources.dynamicDraw);
    }
    if (scratch.additiveIndexCount > 0) {
      gl.bindBuffer(elementArrayBuffer, buffers.additiveIndex);
      gl.bufferData(elementArrayBuffer, scratch.additiveIndices.subarray(0, scratch.additiveIndexCount), resources.dynamicDraw);
    }
  }

  function drawSceneThickLines(gl, bundle, canvas, resources) {
    const thickProgram = resources.thickLineProgram;
    if (!thickProgram || !thickProgram.program) {
      return false;
    }
    const widths = bundle && bundle.worldLineWidths;
    const passes = bundle && bundle.worldLinePasses;
    const vertexCount = Math.floor(sceneNumber(bundle && bundle.worldVertexCount, 0));
    const segmentCount = Math.floor(vertexCount / 2);
    if (segmentCount <= 0 || !bundle.worldPositions || !bundle.worldColors) {
      return false;
    }
    // Overflow guard: Uint16 indices cap at 65535 → 16384 segments. Falling
    // back to the legacy gl.LINES path lets enormous scenes (particle field
    // fallback bundles) still render without a buffer overflow crash — they
    // just won't honor per-segment width past that cutoff.
    if (segmentCount > 16384) {
      return false;
    }

    const scratch = resources.thickLineScratch;
    const usedSegments = expandSceneThickLineIntoScratch(scratch, bundle.worldPositions, bundle.worldColors, widths, passes, segmentCount);
    uploadSceneThickLineBuffers(gl, resources, scratch, usedSegments);

    gl.useProgram(thickProgram.program);

    const camera = sceneRenderCamera(bundle && bundle.camera);
    if (thickProgram.cameraLocation && typeof gl.uniform4f === "function") {
      gl.uniform4f(thickProgram.cameraLocation, camera.x, camera.y, camera.z, camera.fov);
    }
    if (thickProgram.cameraRotationLocation && typeof gl.uniform3f === "function") {
      gl.uniform3f(thickProgram.cameraRotationLocation, camera.rotationX, camera.rotationY, camera.rotationZ);
    }
    if (thickProgram.depthRangeLocation && typeof gl.uniform2f === "function") {
      gl.uniform2f(thickProgram.depthRangeLocation, camera.near, camera.far);
    }
    if (thickProgram.aspectLocation && typeof gl.uniform1f === "function") {
      const aspect = Math.max(0.0001, canvas.width / Math.max(1, canvas.height));
      gl.uniform1f(thickProgram.aspectLocation, aspect);
    }
    if (thickProgram.viewportLocation && typeof gl.uniform2f === "function") {
      gl.uniform2f(thickProgram.viewportLocation, canvas.width, canvas.height);
    }
    if (thickProgram.cameraModeLocation && typeof gl.uniform1f === "function") {
      gl.uniform1f(thickProgram.cameraModeLocation, camera.kind === "orthographic" ? 1 : 0);
    }
    if (thickProgram.orthoLocation && typeof gl.uniform4f === "function") {
      const bounds = sceneOrthographicBounds(camera, canvas.width, canvas.height);
      gl.uniform4f(thickProgram.orthoLocation, bounds.left, bounds.right, bounds.top, bounds.bottom);
    }

    const arrayBuffer = resources.arrayBuffer;
    const floatType = resources.floatType;
    const buffers = resources.thickLineBuffers;

    gl.bindBuffer(arrayBuffer, buffers.positionA);
    gl.enableVertexAttribArray(thickProgram.positionALocation);
    gl.vertexAttribPointer(thickProgram.positionALocation, 3, floatType, false, 0, 0);

    gl.bindBuffer(arrayBuffer, buffers.positionB);
    gl.enableVertexAttribArray(thickProgram.positionBLocation);
    gl.vertexAttribPointer(thickProgram.positionBLocation, 3, floatType, false, 0, 0);

    gl.bindBuffer(arrayBuffer, buffers.colorA);
    gl.enableVertexAttribArray(thickProgram.colorALocation);
    gl.vertexAttribPointer(thickProgram.colorALocation, 4, floatType, false, 0, 0);

    gl.bindBuffer(arrayBuffer, buffers.colorB);
    gl.enableVertexAttribArray(thickProgram.colorBLocation);
    gl.vertexAttribPointer(thickProgram.colorBLocation, 4, floatType, false, 0, 0);

    gl.bindBuffer(arrayBuffer, buffers.side);
    gl.enableVertexAttribArray(thickProgram.sideLocation);
    gl.vertexAttribPointer(thickProgram.sideLocation, 1, floatType, false, 0, 0);

    gl.bindBuffer(arrayBuffer, buffers.endpoint);
    gl.enableVertexAttribArray(thickProgram.endpointLocation);
    gl.vertexAttribPointer(thickProgram.endpointLocation, 1, floatType, false, 0, 0);

    gl.bindBuffer(arrayBuffer, buffers.width);
    gl.enableVertexAttribArray(thickProgram.widthLocation);
    gl.vertexAttribPointer(thickProgram.widthLocation, 1, floatType, false, 0, 0);

    const elementArrayBuffer = typeof gl.ELEMENT_ARRAY_BUFFER === "number" ? gl.ELEMENT_ARRAY_BUFFER : 0x8893;
    const unsignedShort = typeof gl.UNSIGNED_SHORT === "number" ? gl.UNSIGNED_SHORT : 0x1403;

    // Opaque pass first (depth writes on), then alpha, then additive.
    // Matches the draw plan's standard pass ordering so thick lines
    // composite correctly against triangle meshes drawn by the PBR path.
    if (scratch.opaqueIndexCount > 0) {
      applySceneWebGLDepth(gl, "opaque", resources.stateCache);
      applySceneWebGLBlend(gl, "opaque", resources.stateCache);
      gl.bindBuffer(elementArrayBuffer, buffers.opaqueIndex);
      gl.drawElements(resources.trianglesMode, scratch.opaqueIndexCount, unsignedShort, 0);
    }
    if (scratch.alphaIndexCount > 0) {
      applySceneWebGLDepth(gl, "translucent", resources.stateCache);
      applySceneWebGLBlend(gl, "alpha", resources.stateCache);
      gl.bindBuffer(elementArrayBuffer, buffers.alphaIndex);
      gl.drawElements(resources.trianglesMode, scratch.alphaIndexCount, unsignedShort, 0);
    }
    if (scratch.additiveIndexCount > 0) {
      applySceneWebGLDepth(gl, "translucent", resources.stateCache);
      applySceneWebGLBlend(gl, "additive", resources.stateCache);
      gl.bindBuffer(elementArrayBuffer, buffers.additiveIndex);
      gl.drawElements(resources.trianglesMode, scratch.additiveIndexCount, unsignedShort, 0);
    }
    return true;
  }

  // sceneBundleNeedsThickLines returns true when any world line segment has
  // an explicit width > 1 (LinesGeometry.Width was set on the Go side). False
  // preserves the legacy gl.LINES draw path so pre-v0.15.1 scenes render
  // unchanged on drivers that happily take hairline widths.
  function sceneBundleNeedsThickLines(bundle) {
    const widths = bundle && bundle.worldLineWidths;
    if (!widths || !widths.length) {
      return false;
    }
    for (let i = 0; i < widths.length; i += 1) {
      if (widths[i] > 1) {
        return true;
      }
    }
    return false;
  }

  // Republish the entry points the base scene3d chunk calls once this chunk
  // lands. 20b-scene-mount-webgl-chunk.js reads them through
  // window.__gosx_scene3d_api at call time, never lexically, because the base
  // chunk runs before this file exists.
  if (typeof window !== "undefined" && window.__gosx_scene3d_api) {
    Object.assign(window.__gosx_scene3d_api, {
      createSceneWebGLRenderer: createSceneWebGLRenderer,
      createSceneWebGLProgram: createSceneWebGLProgram,
      createSceneWebGLResources: createSceneWebGLResources,
      disposeSceneWebGLRenderer: disposeSceneWebGLRenderer,
      renderSceneWebGLWorldBundle: renderSceneWebGLWorldBundle,
      sceneMeshMaterialArray: sceneMeshMaterialArray,
      createSceneThickLineScratch: createSceneThickLineScratch,
      expandSceneThickLineIntoScratch: expandSceneThickLineIntoScratch,
      sceneBundleNeedsThickLines: sceneBundleNeedsThickLines,
    });
  }
