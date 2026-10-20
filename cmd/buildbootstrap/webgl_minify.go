package main

// These fields belong to WebGL shader records or renderer-owned caches. They
// never cross the chunk boundary. Public IR fields, GL methods, telemetry,
// shared detail records and dynamically selected VBO slots retain their names.
// Keep this allowlist explicit: a blanket underscore pattern would rename
// fields the base chunk creates, or strings used as dynamic property keys.
const webGLPrivatePropertyPattern = "^(_cpuCullScratchColors|_cpuCullScratchTransforms|_jointMatrixUploadViews|_lastExposure|_lastLightsHash|_lastMaterial|_lastMaterialTextureEpoch|_lastMaterialTexturesReady|_lastOutputLinear|_lastPassHash|_lastToneMapMode|_motionBuffer|_motionDirtyFlags|_motionDirtyRanges|_motionGeneration|_motionMembers|_motionUploadedGeneration|_pbrAttributeViews|_rigidBatchKeyCache|initialize|settled|extension|timer|frame|vertexShader|fragmentShader|initialProgramOwner)$"
