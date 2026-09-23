// bootstrap-bridge.d.ts — ambient declarations for the Scene3D type-check
// program only. It ships in no bundle and no browser ever loads it.
//
// cmd/buildbootstrap concatenates each scene3d/*.ts fragment into a chunk.
// That chunk also holds sibling fragments from client/js/bootstrap-src (see
// client/js/bootstrap-src/chunks.json). Those siblings share one function
// scope with the scene3d fragments. They define the names below as `var`
// or `function` bindings, so scene3d/*.ts reads them as ordinary globals
// at runtime. tsconfig.scene3d.json type-checks the scene3d/*.ts fragments
// on their own, without their bootstrap-src neighbors. It needs a stand-in
// for that shared scope. Each declaration below asserts only that the name
// exists; it does not model the bootstrap-src function's real signature.
// Give a name a precise type here (or in its own scene3d/*.ts file) as a
// later, stricter slice narrows it away from `any`.
//
// window carries the same ad hoc bag of custom properties (__gosx_*) that
// the runtime installs from many separately-authored chunks. Model it as
// an open dictionary here, instead of narrowing every read with a cast.
// This widens only the scene3d type-check program. client/runtime/types.d.ts,
// which backs the strict generated-ABI check, stays untouched.
interface Window {
  [name: string]: any;
}

// navigator.gpu is the WebGPU entry point. TypeScript 5.9's bundled DOM lib
// ships no WebGPU types yet, so lib.dom.d.ts's Navigator interface omits it.
interface Navigator {
  gpu?: any;
}

// Pre-standardization pointer-lock vendor prefixes (mount-controls.ts). Every
// shipping browser has carried the unprefixed pointerLockElement /
// exitPointerLock for years, but the fallback reads stay for the handful of
// older embedded WebViews GoSX still supports, so the checked program needs
// them too.
interface Document {
  mozPointerLockElement?: Element | null;
  webkitPointerLockElement?: Element | null;
  mozExitPointerLock?: () => void;
  webkitExitPointerLock?: () => void;
}

// sceneFilterPointsByQualityGroups (mount-quality.ts) and its object
// counterpart both attach a documented own property to the points/objects
// array they return, so a caller can read a point-quality-skipped count off
// the array without a second pass. Array.isArray narrows an `any`-typed
// parameter to `any[]`, which does not carry this expando, so the checked
// program needs it added here once rather than cast at every read site.
interface Array<T> {
  qualitySkippedCount?: number;
}

// WebGPU spec globals. TypeScript 5.9's bundled DOM lib ships no WebGPU
// types yet, so the buffer/texture/shader-stage/map-mode usage bitmasks
// used across the WebGPU renderer and the compute source need a stand-in
// too.
declare var GPUBufferUsage: any;
declare var GPUMapMode: any;
declare var GPUShaderStage: any;
declare var GPUTextureUsage: any;

declare var SCENE_CMD_SET_TRANSFORM: any;
declare var SCENE_IDENTITY_MAT4: any;
declare var SCENE_POST_BLOOM: any;
declare var SCENE_POST_COLOR_GRADE: any;
declare var SCENE_POST_CUSTOM_POST: any;
declare var SCENE_POST_DOF: any;
declare var SCENE_POST_FXAA: any;
declare var SCENE_POST_SSAO: any;
declare var SCENE_POST_TONE_MAPPING: any;
declare var SCENE_POST_VIGNETTE: any;
declare var SCENE_TEXTURE_UNIT_DEFAULT_MAX: any;
declare var SCENE_TEXTURE_UNIT_MATERIALS: any;
declare var _animScratch3: any;
declare var _animScratch4: any;
declare var _externalProbe: any;
declare var _sceneMat4ScratchA: any;
declare var applySceneCommands: any;
declare var applySceneObjectPatch: any;
declare var applyScenePostUniformsCommand: any;
declare var applyTextLayoutPresentation: any;
declare var backendSelectionOrder: any;
declare var bindSceneManagedControlForms: any;
declare var buildSceneWorldDrawPlan: any;
declare var cancelEngineFrame: any;
declare var clamp01: any;
declare var clearChildren: any;
declare var createSceneCanvasRenderer: any;
declare var createSceneCustomPostDOMRegionTracker: any;
declare var createSceneRenderBundle: any;
declare var createSceneState: any;
declare var createSceneThickLineScratch: any;
declare var createSceneWebGLProgram: any;
declare var createSceneWebGLRenderer: any;
declare var createSceneWebGLResources: any;
declare var createSceneWebGLSurfaceProgram: any;
declare var createSceneWebGPURendererOrFallback: any;
declare var createSceneWorldDrawScratch: any;
declare var disposeSceneWebGLRenderer: any;
declare var engineFrame: any;
declare var expandSceneThickLineIntoScratch: any;
declare var extractFrustumPlanesJS: any;
declare var generateInstancedGeometry: any;
declare var gosxApplyCurrentScriptNonce: any;
declare var gosxLowEndHardware: any;
declare var gosxSubscribeSharedSignal: any;
declare var gosxTextLayoutRevision: any;
declare var hashEnvironmentContent: any;
declare var hashLightContent: any;
declare var inflateSceneShaderLib: any;
declare var instancePassesCullTest: any;
declare var layoutBrowserText: any;
declare var loadManifest: any;
declare var normalizeInstancedGeometryKind: any;
declare var normalizeSceneCamera: any;
declare var normalizeSceneHTML: any;
declare var normalizeSceneHTMLMode: any;
declare var normalizeSceneHTMLPointerEvents: any;
declare var normalizeSceneLabel: any;
declare var normalizeSceneLabelAlign: any;
declare var normalizeSceneLabelCollision: any;
declare var normalizeSceneLabelWhiteSpace: any;
declare var normalizeSceneLight: any;
declare var normalizeSceneMaterialKind: any;
declare var normalizeSceneObject: any;
declare var normalizeScenePointsEntry: any;
declare var normalizeSceneSprite: any;
declare var normalizeSceneSpriteFit: any;
declare var normalizeTextLayoutOverflow: any;
declare var notifySceneTextureLoaded: any;
declare var onSceneTextureLoaded: any;
declare var onTextLayoutInvalidated: any;
declare var prepareScene: any;
declare var queueInputSignal: any;
declare var renderSceneWebGLWorldBundle: any;
declare var resolvePostFXFactor: any;
declare var resolveShadowSize: any;
declare var sceneAdvanceTransitions: any;
declare var sceneAffineDeterminant: any;
declare var sceneAffineNormalMatrix: any;
declare var sceneAllocateTextureUnits: any;
declare var sceneApplyLiveEvent: any;
declare var sceneBackendRegistry: any;
declare var sceneBase64Decode: any;
declare var sceneBool: any;
declare var sceneCachedBuffer: any;
declare var sceneCameraEquivalent: any;
declare var sceneCanvasAlpha: any;
declare var sceneClamp: any;
declare var sceneCloneHydrationModel: any;
declare var sceneColorRGBA: any;
declare var sceneDragMatchesActivePointer: any;
declare var sceneEulerMatrixInto: any;
declare var sceneEventSignalNamespace: any;
declare var sceneFiniteNumber: any;
declare var sceneGizmoTargetAnchor: any;
declare var sceneHTMLAnimated: any;
declare var sceneHasActiveTransitions: any;
declare var sceneInstancedGLBHydrationTemplates: any;
declare var sceneInstancedGLBMeshes: any;
declare var sceneInstancedGLBModelsFromBatches: any;
declare var sceneIsNumericTypedArray: any;
declare var sceneIsPlainObject: any;
declare var sceneLabelAnimated: any;
declare var sceneLabelLayoutCacheLimit: any;
declare var sceneLocalPointerPoint: any;
declare var sceneLocalPointerSample: any;
declare var sceneMat4Multiply: any;
declare var sceneMat4MultiplyInto: any;
declare var sceneMat4Ortho2DProj: any;
declare var sceneMat4Ortho2DView: any;
declare var sceneMaterialHasEnabledNumericAlphaCutoff: any;
declare var sceneMaterialUsesAuthoredMeshShader: any;
declare var sceneMatrixTransformInto: any;
declare var sceneModels: any;
declare var sceneNormalizeDirection: any;
declare var sceneNormalizeMaterialAlphaCutoff: any;
declare var sceneNormalizeMaterialList: any;
declare var sceneNowMilliseconds: any;
declare var sceneNumber: any;
declare var sceneObjectAnimated: any;
declare var sceneObjectDepthCenter: any;
declare var sceneObjectKey: any;
declare var sceneObjectMaterialKindValue: any;
declare var sceneObjectMaterialProfile: any;
declare var sceneObjectMaterialValue: any;
declare var sceneObjectModelMatrix: any;
declare var sceneObjectTransformNormal: any;
declare var scenePBRDepthSort: any;
declare var scenePBRObjectRenderPass: any;
declare var scenePBRProjectionMatrix: any;
declare var scenePBRProjectionMatrixForCamera: any;
declare var scenePBRViewMatrix: any;
declare var sceneParseRadianceHDR: any;
declare var scenePickSignalNamespace: any;
declare var scenePointInPolygon: any;
declare var scenePointStyleCode: any;
declare var scenePointerCanStartDrag: any;
declare var scenePostDOMRegionPixelBounds: any;
declare var scenePreparedCommandSequence: any;
declare var scenePrimeInitialTransitions: any;
declare var sceneProjectedObjectHull: any;
declare var sceneProjectedObjectSegments: any;
declare var sceneProjectedSegmentsBounds: any;
declare var sceneQualityLadder: any;
declare var sceneQuatToEulerXYZ: any;
declare var sceneRehydrateModelsAfterCommand: any;
declare var sceneRenderCamera: any;
declare var sceneResolveMaterialUniforms: any;
declare var sceneRotatePoint: any;
declare var sceneSelenaMaterialValue: any;
declare var sceneSelenaUniformData: any;
declare var sceneShadowComputeBounds: any;
declare var sceneShadowLightSpaceMatrix: any;
declare var sceneSpriteAnimated: any;
declare var sceneStateHTML: any;
declare var sceneStateInstancedMeshesWithMaterials: any;
declare var sceneStateLabels: any;
declare var sceneStateLights: any;
declare var sceneStateObjects: any;
declare var sceneStateObjectsWithMaterials: any;
declare var sceneStatePointsWithMaterials: any;
declare var sceneStateSprites: any;
declare var sceneTRSToMat4: any;
declare var sceneTypedFloatArray: any;
declare var sceneViewportValue: any;
declare var sceneWaterAdvanceClock: any;
declare var sceneWaterResetClock: any;
declare var sceneWebGPUDiagnostics: any;
declare var setAttrValue: any;
declare var setStyleValue: any;
declare var setupSceneDragInteractions: any;
declare var setupSceneGizmoDragInteractions: any;
declare var setupScenePickInteractions: any;
declare var webGPUObjectModelMatrix: any;
