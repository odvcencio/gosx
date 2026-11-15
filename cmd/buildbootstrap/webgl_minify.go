package main

// These fields belong to WebGL shader records or renderer-owned caches, and
// esbuild renames them in bootstrap-feature-scene3d-webgl.js only. Public IR
// fields, GL methods, telemetry, shared detail records and dynamically
// selected VBO slots retain their names.
//
// A name belongs here only while no other chunk uses it as a property name:
// the other chunk keeps the authored name, so the two would stop meeting.
// TestWebGLRenamedPropertiesStayInsideTheWebGLChunk enforces that. Six names
// left the list for it:
//
//   - frame: the Selena lighting chunk publishes {frame, value, shadowSlot},
//     and the renamed call threw on every frame once that chunk loaded.
//   - vertexShader, fragmentShader: the glTF and WebGL post-effect chunks
//     delete the shaders of a program record this chunk creates.
//   - extension: sceneResolveIBLRenderTargetMode returns it through
//     window.__gosx_scene3d_resource_api.
//   - settled, timer: other chunks have unrelated properties of these names.
//     They did not collide, and nothing but the reader's care kept it so.
//
// Keep this allowlist explicit: a blanket underscore pattern would rename
// fields the base chunk creates, or strings used as dynamic property keys.
const webGLPrivatePropertyPattern = "^(_cpuCullScratchColors|_cpuCullScratchTransforms|_jointMatrixUploadViews|_lastExposure|_lastLightsHash|_lastMaterial|_lastMaterialTextureEpoch|_lastMaterialTexturesReady|_lastOutputLinear|_lastPassHash|_lastToneMapMode|_motionBuffer|_motionDirtyFlags|_motionDirtyRanges|_motionGeneration|_motionMembers|_motionUploadedGeneration|_pbrAttributeViews|_rigidBatchKeyCache|initialize|initialProgramOwner)$"
