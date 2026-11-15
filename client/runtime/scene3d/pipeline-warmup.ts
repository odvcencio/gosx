// pipeline-warmup.ts — compile declared shaders before the first frame draws.
//
// Props.shaderWarmup lists materials and draw variants that a scene uses later:
// objects that spawn, a material swap, a skinned or instanced copy. Until the
// renderer reports every program ready, each frame's metadata carries one
// synthetic bundle of those variants. The backends prepare it beside the real
// bundle, so WebGL2 links through KHR_parallel_shader_compile and WebGPU
// through createRenderPipelineAsync, and the frame waits while any program is
// pending. A first use after boot then finds its program in the cache.
//
// Progress is published on the mount for loading screens and game/boot:
// data-gosx-scene3d-shaders (compiling | ready), -shaders-done, -shaders-total,
// and the "gosx:scene3d:shader-progress" event with {done, total, failed, ready}.
//
// This module ships in the opt-in bootstrap-feature-scene3d-warmup.js chunk,
// which a page advertises only for scenes that set Props.shaderWarmup (the
// monolith carries it inline). The mount loads it before the first frame and
// reaches it through the shims in optional-chunks.ts.
(function() {
const sceneShaderWarmups = new WeakMap<any, any>();
const SCENE_WARMUP_IDENTITY = [1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1];

// sceneShaderWarmupBundle lowers the authored list into the bundle shape both
// prepare passes read. Records have three vertices so backend predicates that
// require geometry (skinning, direct attributes) accept them; nothing draws them.
function sceneShaderWarmupBundle(spec: any, sceneObjectMaterialProfile: any): any {
  const sceneApi: any = (window as any).__gosx_scene3d_api || {};
  const sceneWorldObjectRenderPass = sceneApi.sceneWorldObjectRenderPass;
  const scenePBRObjectRenderPass = sceneApi.scenePBRObjectRenderPass;
  const entries = Array.isArray(spec && spec.materials) ? spec.materials : [];
  const bundle: any = { materials: [], meshObjects: [], instancedMeshes: [], lights: [] };
  const passes: any = { opaque: [], alpha: [], additive: [] };
  entries.forEach(function(entry: any, i: number) {
    if (!entry || typeof entry !== "object") return;
    const source = Object.assign({ kind: "mesh" }, entry.material || {}, { id: "__gosx-warmup-" + i });
    const material = sceneObjectMaterialProfile(source);
    const materialIndex = bundle.materials.push(material) - 1;
    const renderPass = sceneWorldObjectRenderPass(source, material);
    const castShadow = entry.castShadow === true;
    if (castShadow) bundle.lights = [{ kind: "directional", castShadow: true }];
    if (entry.instances > 0) {
      bundle.instancedMeshes.push({ id: source.id, kind: "box", materialIndex, renderPass, castShadow, count: Math.floor(entry.instances) });
      return;
    }
    const skinned = entry.skinned === true;
    const vertices: any = { count: 3, positions: new Float32Array(9) };
    if (skinned) { vertices.joints = new Float32Array(12); vertices.weights = new Float32Array(12); }
    const record: any = {
      id: source.id, kind: "mesh", materialIndex, renderPass, castShadow, doubleSided: entry.doubleSided === true,
      vertices, directVertices: true, vertexOffset: 0, vertexCount: 3, modelMatrix: SCENE_WARMUP_IDENTITY,
      skin: skinned ? { jointMatrices: new Float32Array(SCENE_WARMUP_IDENTITY) } : null,
    };
    bundle.meshObjects.push(record);
    passes[scenePBRObjectRenderPass(record, material)].push(record);
  });
  // WebGPU prepares from precomputed passes; the hash keys its variant cache.
  bundle.prepared = { pbrPasses: passes, pipelineVariantHash: "warmup:" + entries.length };
  return bundle;
}

function sceneShaderWarmupPublish(mount: any, state: any, counts: any) {
  const done = Math.max(0, counts.total - counts.pending);
  const key = state.phase + ":" + done + ":" + counts.total + ":" + counts.failed;
  if (key === state.published) return;
  state.published = key;
  if (typeof mount.setAttribute === "function") {
    mount.setAttribute("data-gosx-scene3d-shaders", state.phase);
    mount.setAttribute("data-gosx-scene3d-shaders-done", String(done));
    mount.setAttribute("data-gosx-scene3d-shaders-total", String(counts.total));
  }
  if (typeof mount.dispatchEvent === "function" && typeof CustomEvent === "function") {
    mount.dispatchEvent(new CustomEvent("gosx:scene3d:shader-progress", { bubbles: true, detail: {
      done, total: counts.total, failed: counts.failed, ready: state.phase === "ready", backend: state.backend,
    } }));
  }
}

function sceneShaderWarmupCounts(renderer: any): any {
  const counts = typeof renderer.shaderPrograms === "function" && renderer.shaderPrograms() || {};
  return { total: counts.total | 0, pending: counts.pending | 0, failed: counts.failed | 0 };
}

// sceneShaderWarmupStep returns the warm-up bundle for this frame, or null.
// A replacement renderer (WebGPU device loss, WebGL fallback) restarts the
// warm-up.
function sceneShaderWarmupStep(mount: any, props: any, renderer: any, profile: any): any {
  const spec = props && props.shaderWarmup;
  if (!spec || !mount || !renderer) return null;
  let state = sceneShaderWarmups.get(mount);
  if (!state || state.renderer !== renderer) {
    state = { renderer, phase: "compiling", published: "", backend: String(renderer.kind || ""), bundle: sceneShaderWarmupBundle(spec, profile), mount, started: Date.now() };
    state.bundle.warmupState = state;
    sceneShaderWarmups.set(mount, state);
  }
  if (state.phase === "ready") return null;
  sceneShaderWarmupPublish(mount, state, sceneShaderWarmupCounts(renderer));
  return state.bundle;
}

// sceneShaderWarmupFinish runs after render. A submitted frame that carried the
// warm-up bundle proves every program it requested is linked, because both
// backends return "pending" while any program is still compiling. Until then,
// a poll publishes progress each animation frame: the mount itself waits for
// the backend's ready event and starts no frames while programs link.
function sceneShaderWarmupFinish(meta: any, outcome: any): any {
  const state = meta && meta.warmup && meta.warmup.warmupState;
  if (!state || state.phase === "ready") return outcome;
  const counts = sceneShaderWarmupCounts(state.renderer);
  if (outcome === "submitted") {
    state.phase = "ready";
    counts.pending = 0;
  } else if (!state.poll) {
    sceneShaderWarmupPoll(state);
  }
  sceneShaderWarmupPublish(state.mount, state, counts);
  return outcome;
}

function sceneShaderWarmupPoll(state: any) {
  const next = typeof requestAnimationFrame === "function"
    ? requestAnimationFrame
    : function(callback: any) { return setTimeout(callback, 50); };
  state.poll = true;
  next(function() {
    state.poll = false;
    // A disposed or stuck mount stops polling after two minutes; the boot
    // gate's own stall timer reports a warm-up that never finishes.
    if (state.phase === "ready" || sceneShaderWarmups.get(state.mount) !== state || state.mount.isConnected === false || Date.now() - state.started > 120000) return;
    sceneShaderWarmupPublish(state.mount, state, sceneShaderWarmupCounts(state.renderer));
    sceneShaderWarmupPoll(state);
  });
}

(window as any).__gosx_scene3d_warmup_api = { frame: sceneShaderWarmupStep, settle: sceneShaderWarmupFinish };
})();
