  // Bridge exports for the lazily fetched WebGL chunk
  // (bootstrap-feature-scene3d-webgl.js, see
  // 26j-feature-scene3d-webgl-prefix.ts).
  //
  // 16-scene-webgl.js used to sit in this same IIFE, so it read these symbols
  // lexically. It now runs in its own IIFE and must read them from
  // window.__gosx_scene3d_api instead.
  //
  // Every other bridge symbol the WebGL chunk needs was already published by
  // 10-runtime-scene-core.ts, 11-scene-math.ts, 15c-scene-backend-registry.ts,
  // 16b-scene-compute.js, 15a-scene-postfx-shared.ts or 16b-scene-hdr.ts.
  // This file adds only the ones that had no consumer outside the IIFE before.
  //
  // Keep this file after every module it reads. It runs its assignments
  // immediately, so a `var`-initialized binding declared later would still be
  // undefined here.
  if (typeof window !== "undefined" && window.__gosx_scene3d_api) {
    Object.assign(window.__gosx_scene3d_api, {
      // 10-runtime-scene-core.ts — legacy vertex-color WebGL renderer parts.
      createSceneWebGLProgram: typeof createSceneWebGLProgram === "function" ? createSceneWebGLProgram : undefined,
      createSceneWebGLResources: typeof createSceneWebGLResources === "function" ? createSceneWebGLResources : undefined,
      disposeSceneWebGLRenderer: typeof disposeSceneWebGLRenderer === "function" ? disposeSceneWebGLRenderer : undefined,
      renderSceneWebGLWorldBundle: typeof renderSceneWebGLWorldBundle === "function" ? renderSceneWebGLWorldBundle : undefined,
      sceneCanvasAlpha: typeof sceneCanvasAlpha === "function" ? sceneCanvasAlpha : undefined,
      // 11-scene-math.ts — WebGL2 CPU instance cull.
      instancePassesCullTest: typeof instancePassesCullTest === "function" ? instancePassesCullTest : undefined,
      // 13-scene-material.ts — material identity for program caching.
      normalizeSceneMaterialKind: typeof normalizeSceneMaterialKind === "function" ? normalizeSceneMaterialKind : undefined,
      sceneMaterialProfileKey: typeof sceneMaterialProfileKey === "function" ? sceneMaterialProfileKey : undefined,
      // 15a-scene-postfx-shared.ts — scalar guard shared with the post chain.
      sceneFiniteNumber: typeof sceneFiniteNumber === "function" ? sceneFiniteNumber : undefined,
      // 16c-scene-shared-pbr.ts — light and environment dirty hashes. The
      // WebGL renderer recomputes them per frame to skip unchanged uploads.
      hashLightContent: typeof hashLightContent === "function" ? hashLightContent : undefined,
      hashEnvironmentContent: typeof hashEnvironmentContent === "function" ? hashEnvironmentContent : undefined,
      // 10-runtime-scene-core.ts — typed float coercion. The legacy renderer
      // moved to 16e-scene-webgl-legacy.ts, which now runs in the WebGL chunk,
      // but this helper stayed here because mesh and animation normalization
      // call it on every backend.
      sceneTypedFloatArray: typeof sceneTypedFloatArray === "function" ? sceneTypedFloatArray : undefined,
      // 13-scene-material.ts and 15-scene-draw-plan.ts — the material and
      // draw-pass helpers the legacy renderer reads. They had no consumer
      // outside this IIFE until 16e moved out of the base chunk.
      sceneFallbackMaterialData: typeof sceneFallbackMaterialData === "function" ? sceneFallbackMaterialData : undefined,
      sceneMaterialEmissive: typeof sceneMaterialEmissive === "function" ? sceneMaterialEmissive : undefined,
      sceneMaterialOpacity: typeof sceneMaterialOpacity === "function" ? sceneMaterialOpacity : undefined,
      compareSceneWorldPassEntries: typeof compareSceneWorldPassEntries === "function" ? compareSceneWorldPassEntries : undefined,
      sceneWorldObjectRenderPass: typeof sceneWorldObjectRenderPass === "function" ? sceneWorldObjectRenderPass : undefined,
      sceneWorldObjectRenderable: typeof sceneWorldObjectRenderable === "function" ? sceneWorldObjectRenderable : undefined,
    });
  }
