// Crowd API (lazy chunk bootstrap-feature-scene3d-crowd.js, loaded when a
// scene declares environment.crowds). The renderers call webgl and webgpu
// once per frame and dispose when they release their context; pages and Go
// (scene.DispatchCrowdFrame) reach apply through
// window.__gosx.scene3d.dispatchCrowdFrame.

if (typeof window !== "undefined") {
  (window as any).__gosx_scene3d_crowd_api = {
    webgl: sceneCrowdGLDraw,
    webgpu: sceneCrowdGPUDraw,
    dispose: function(context: any) { if (context) { sceneCrowdGLDispose(context); sceneCrowdGPUDispose(context); } },
    apply: sceneCrowdApply,
    decode: sceneCrowdDecode,
    levels: sceneCrowdLevels,
    layout: sceneCrowdLayout,
    pack: sceneCrowdPack,
    bake: sceneCrowdBake,
    meshes: sceneCrowdMeshes,
    select: sceneCrowdSelect,
    actions: SCENE_CROWD_ACTIONS.map(function(entry: any[]) { return entry[0]; }),
    stats: null,
  };
}
