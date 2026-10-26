// Opt-in preparation uses the mounted backend's existing decoded/model/map caches.
// It never draws actors, owns shared resources, or polls the GPU on a frame loop.
interface SceneModelPreparationSpec { id: string; src: string; rigidGeometry?: boolean }
const SCENE_MODEL_PREPARATION_SLOTS = [
  ['texture', 'baseColor', 'base-color', 'srgb'],
  ['normalMap', 'normal', 'normal', 'linear'],
  ['roughnessMap', 'roughness', 'roughness', 'linear'],
  ['metalnessMap', 'metalness', 'metalness', 'linear'],
  ['emissiveMap', 'emissive', 'emissive', 'srgb'],
  ['occlusionMap', 'occlusion', 'ambient-occlusion', 'linear'],
  ['specularIntensityMap', 'specularIntensity', 'specular-intensity', 'linear'],
  ['specularColorMap', 'specularColor', 'specular-color', 'srgb'],
  ['thicknessMap', 'thickness', 'data', 'linear'],
];
function sceneModelPreparationSnapshot(requests: SceneModelPreparationSpec[]) {
  if (!Array.isArray(requests) || !requests.length || requests.length > 32) throw new TypeError('Scene3D preparation requires 1–32 models');
  const ids = new Set<string>();
  return requests.map(req => {
    if (!req || typeof req.id !== 'string' || !req.id.trim() || ids.has(req.id) || typeof req.src !== 'string' || !req.src.trim() || req.rigidGeometry !== undefined && typeof req.rigidGeometry !== 'boolean') throw new TypeError('invalid Scene3D preparation model');
    ids.add(req.id);
    return { id: req.id, src: req.src.trim(), rigidGeometry: req.rigidGeometry };
  });
}
function sceneModelPreparationMaps(asset: any, renderer: any, records: Set<any>) {
  for (const object of asset.objects) {
    const material = object.material && typeof object.material === 'object' ? object.material : object;
    for (const [prop, slot, role, colorSpace] of SCENE_MODEL_PREPARATION_SLOTS) {
      const descriptor = material.textureDescriptors?.[slot];
      const uri = descriptor?.uri?.trim() || material[prop];
      if (!uri) continue;
      const record = renderer.texturePreparation.load(uri, descriptor, role, colorSpace, true);
      if (!record) throw new Error('Scene3D material map unavailable: ' + uri);
      records.add(record);
    }
  }
}
function scenePrepareModels(mount: any, requests: SceneModelPreparationSpec[], signal: AbortSignal, selectedScope?: any) {
  const selected = sceneModelPreparationSnapshot(requests), state = mount?.__gosxScene3DState;
  const handle = mount?.__gosxScene3DHandle, renderer = state?._crowdRenderer;
  const canvas = mount?.querySelector('canvas');
  if (!state || !handle || !canvas || typeof renderer?.texturePreparation?.load !== 'function') return Promise.reject(new Error('Scene3D preparation renderer unavailable'));
  const records = new Set<any>();
  return new Promise((resolve, reject) => {
    let done = false, decoded = false;
    const alive = () => mount.isConnected && mount.__gosxScene3DState === state && mount.__gosxScene3DHandle === handle && state._crowdRenderer === renderer;
    function close(error: any = null) {
      if (done) return;
      done = true;
      canvas.removeEventListener('gosx:scene3d:resource-ready', check);
      signal?.removeEventListener('abort', cancel);
      error ? reject(error) : resolve(selectedScope
        ? { models: selected.length, textures: records.size, records: Array.from(records) }
        : { models: selected.length, textures: records.size });
    }
    function cancel() { close(new DOMException('Scene3D preparation canceled', 'AbortError')); }
    function check() {
      if (done) return;
      if (signal?.aborted) return cancel();
      if (renderer.texturePreparation.generation.disposed || !alive()) return close(new Error('Scene3D preparation mount disposed or replaced'));
      let pending = !decoded;
      for (const record of records) {
        if (record.disposed || record.generation?.disposed) return close(new Error('Scene3D material map disposed: ' + record.src));
        if (record.failed) return close(new Error('Scene3D material map failed: ' + record.src + ': ' + (record.error || 'upload failed')));
        if (!record.loaded) pending = true;
      }
      if (!pending) close();
    }
    canvas.addEventListener('gosx:scene3d:resource-ready', check);
    signal?.addEventListener('abort', cancel, { once: true });
    // Surface owns mount removal/replacement throughout chunk loading and this wait.
    check();
    if (done) return;
    Promise.all(selected.map(async req => {
      const asset = await window.__gosx_scene3d_preload_model(req.src, { variantScope: selectedScope || state._modelTextureVariantScope });
      check();
      if (done) return;
      if (!asset?.objects?.length) throw new Error('Scene3D model failed or has no geometry: ' + req.id + ': ' + req.src);
      if (req.rigidGeometry) {
        const geometry = window.__gosx_scene3d_api.modelGeometry;
        if (geometry.skin(asset.skins) || geometry.weights(asset) || geometry.nodes(asset) ||
          ['points', 'labels', 'sprites', 'html', 'lights'].some(key => asset[key].length) ||
          asset.objects.some((raw: any) => !raw.vertices || raw.vertices.count <= 0 || raw.skin || raw._morphAnim || raw._nodeAnim) ||
          asset.objects.some((raw: any) => !geometry.prepare(raw))) throw new Error('Not rigid: ' + req.id);
      }
      sceneModelPreparationMaps(asset, renderer, records);
    })).then(() => { decoded = true; check(); }, close);
  });
}
window.__gosx_scene3d_model_preparation_api = { prepare: scenePrepareModels };
