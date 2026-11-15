// optional-chunks.ts — base-side entry points for two opt-in Scene3D chunks.
//
// Shader warm-up (pipeline-warmup.ts, Props.shaderWarmup) and text atlases
// (text-atlas.ts, Props.textAtlases) ship in their own chunks. The page
// advertises a chunk only when a scene declares the prop, and the mount loads
// it before the first frame. These shims are all the base chunk carries: a
// scene without either prop pays two property reads per frame. The monolith
// bootstrap.js carries both modules inline, so their APIs exist at once.

// sceneShaderWarmupFrame returns this frame's warm-up bundle, or null. The
// chunk lowers materials with the base's material profile, passed in here.
function sceneShaderWarmupFrame(mount: any, props: any, renderer: any): any {
  const api: any = props && props.shaderWarmup && (window as any).__gosx_scene3d_warmup_api;
  return api ? api.frame(mount, props, renderer, sceneObjectMaterialProfile) : null;
}

// sceneShaderWarmupSettle records a frame's outcome after render.
function sceneShaderWarmupSettle(meta: any, outcome: any): any {
  const api: any = meta && meta.warmup && (window as any).__gosx_scene3d_warmup_api;
  return api ? api.settle(meta, outcome) : outcome;
}

// sceneRegisterTextAtlases draws the scene's atlases. Atlases that arrive in a
// later prop update load the chunk then and redraw the mount when it lands.
function sceneRegisterTextAtlases(props: any, mount: any): void {
  const list = props && props.textAtlases;
  if (!Array.isArray(list) || !list.length) return;
  const api: any = (window as any).__gosx_scene3d_text_atlas_api;
  if (api) return api.register(list);
  ensureSceneGatedFeatureLoaded("text-atlas", "gosxScene3dTextAtlasUrl", "").then(function() {
    if (mount && typeof mount.dispatchEvent === "function") mount.dispatchEvent(new CustomEvent("gosx:scene3d:program-ready"));
  }, function() {});
}

// sceneTextAtlasURL resolves a "gosx-text-atlas:" texture URL to its drawn
// image. An atlas that is not drawn yet resolves to "", so the renderer binds
// its placeholder instead of fetching the scheme URL. Other URLs pass through.
function sceneTextAtlasURL(url: any): any {
  if (typeof url !== "string" || url.indexOf("gosx-text-atlas:") !== 0) return url;
  const api: any = (window as any).__gosx_scene3d_text_atlas_api;
  return api ? api.url(url) : "";
}

// The lazy renderer chunks read the resolver through the scene API.
((window as any).__gosx_scene3d_api || ((window as any).__gosx_scene3d_api = {})).sceneTextAtlasURL = sceneTextAtlasURL;
