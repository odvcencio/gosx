  const sceneModelAssetCache = new Map();
  const sceneModelAssetReady = new Set();
  // Count bound, not a heap/GPU byte measurement. Live acquisitions are pinned;
  // only unused parsed assets participate in this insertion-ordered idle LRU.
  const sceneModelAssetIdle = new Map();
  const sceneModelAssetIdleLimit = 16;

  function sceneModelAssetResidency() {
    let pending = 0, owners = 0;
    sceneModelAssetCache.forEach(function(entry) {
      if (!entry.settled) pending++;
      owners += entry.owners.size;
    });
    return { entries: sceneModelAssetCache.size, idle: sceneModelAssetIdle.size,
      pending, owners, idleLimit: sceneModelAssetIdleLimit };
  }

  function sceneForgetModelAsset(entry) {
    if (sceneModelAssetCache.get(entry.key) !== entry) return;
    sceneModelAssetCache.delete(entry.key);
    sceneModelAssetReady.delete(entry.key);
    sceneModelAssetIdle.delete(entry.key);
  }

  function sceneReleaseModelAsset(lease) {
    if (lease.released) return;
    lease.released = true;
    const entry = lease.entry;
    entry.owners.delete(lease);
    if (lease.state) lease.state._modelAssetLeases.delete(lease);
    if (lease.token) lease.token._modelAssetLeases.delete(lease);
    if (entry.owners.size) return;
    if (!entry.settled) {
      entry.cancelled = true;
      if (entry.controller) entry.controller.abort();
      entry.cancel();
      sceneForgetModelAsset(entry);
    } else if (entry.error || entry.scope && entry.scope.retired) {
      sceneForgetModelAsset(entry);
    } else if (sceneModelAssetCache.get(entry.key) === entry) {
      sceneModelAssetIdle.delete(entry.key);
      sceneModelAssetIdle.set(entry.key, entry);
      while (sceneModelAssetIdle.size > sceneModelAssetIdleLimit) {
        sceneForgetModelAsset(sceneModelAssetIdle.values().next().value);
      }
    }
  }

  function sceneReleaseModelAssetOwner(owner, matches) {
    if (!owner || !owner._modelAssetLeases) return;
    Array.from(owner._modelAssetLeases).forEach(function(lease) {
      if (!matches || matches(lease)) sceneReleaseModelAsset(lease);
    });
  }

  function sceneModelAcquisitionCurrent(meta) {
    return sceneModelHydrationIsCurrent(meta) && !(meta && meta.token &&
      !sceneProgressiveModelTokenCurrent(meta.state, meta.token));
  }

  function retireSceneModelTextureVariantScope(scope) {
    if (!scope) return;
    scope.retired = true;
    // Unblock any renderer settlement waiter without borrowing another mount's
    // backend. Cancelled acquisitions still fence parsing/publication.
    settleSceneModelTextureVariantScope(scope, null);
    sceneModelAssetCache.forEach(function(entry) {
      if (entry.scope === scope && !entry.owners.size) sceneForgetModelAsset(entry);
    });
  }
  async function loadSceneModelAsset(src, mount, hydrationMeta) {
    const key = String(src || "").trim();
    if (!key) {
      return parseSceneModelAsset({}, key);
    }
    const format = sceneModelAssetFormat(key);
    const variantScope = hydrationMeta && hydrationMeta.variantScope
      ? hydrationMeta.variantScope
      : (hydrationMeta && hydrationMeta.state ? hydrationMeta.state._modelTextureVariantScope : null);
    const cacheKey = (format === "glb" || format === "gltf") && variantScope && variantScope.key
      ? String(variantScope.key) + "\u0000" + key
      : key;
    const statusMeta = hydrationMeta
      ? Object.assign({}, hydrationMeta, { variantScope })
      : { variantScope };
    let cached = sceneModelAssetCache.has(cacheKey);
    if (!sceneModelAcquisitionCurrent(hydrationMeta) || variantScope && variantScope.retired) return parseSceneModelAsset({}, key);
    if (sceneModelAcquisitionCurrent(hydrationMeta)) {
      publishSceneModelAssetStatus(mount, "loading", key, cached, "", statusMeta);
    }
    // Status listeners run synchronously and may revoke owners or replace the
    // entry. Never start abandoned work or dereference a pre-event cache hit.
    if (!sceneModelAcquisitionCurrent(hydrationMeta) || variantScope && variantScope.retired) return parseSceneModelAsset({}, key);
    cached = sceneModelAssetCache.has(cacheKey);
    if (!cached) {
      const entry = { key: cacheKey, scope: variantScope, owners: new Set(), settled: false, cancelled: false,
        controller: typeof AbortController === "function" ? new AbortController() : null, promise: null, cancel: null, error: null };
      const cancelledResult = { asset: parseSceneModelAsset({}, key), error: new Error("model acquisition cancelled") };
      const cancelled = new Promise(function(resolve) { entry.cancel = function() { resolve(cancelledResult); }; });
      const signal = entry.controller ? entry.controller.signal : null;
      function assertCurrent() { if (entry.cancelled) throw cancelledResult.error; }
      const loadPromise = (async function() {
        try {
          if (format === "glb" || format === "gltf") {
            // GLTF parsing lives in a sub-feature chunk that's fetched
            // on demand — the first .glb/.gltf request on a page pays
            // the download + parse cost, subsequent ones reuse the
            // cached module. Pages that never load models never fetch
            // the chunk at all.
            var gltfApi = await ensureGLTFFeatureLoaded();
            assertCurrent();
            const variantContext = variantScope ? variantScope.ready : null;
            const model = await gltfApi.sceneLoadGLTFModel(key, variantContext, signal);
            assertCurrent();
            const asset = parseSceneModelAsset(gltfApi.gltfSceneToModelAsset(
              model,
              key
            ), key);
            return { asset, error: null };
          }
          const response = await fetch(key, { credentials: "same-origin", signal });
          assertCurrent();
          if (!response || !response.ok) {
            throw new Error("HTTP " + String(response && response.status || 0));
          }
          const json = await response.json();
          assertCurrent();
          const asset = parseSceneModelAsset(json, key);
          return { asset, error: null };
        } catch (error) {
          if (entry.cancelled) return cancelledResult;
          console.warn("[gosx] failed to load Scene3D model asset:", key, error && error.message ? error.message : error);
          gosxSceneEmit("warn", "model-asset-load-failed", {
            asset: String(key || ""),
            error: error && error.message ? String(error.message) : String(error),
          });
          return { asset: parseSceneModelAsset({}, key), error };
        }
      })();
      entry.promise = Promise.race([loadPromise, cancelled]).then(function(result) {
        entry.settled = true;
        entry.error = result.error;
        if (result.error) sceneForgetModelAsset(entry);
        else if (sceneModelAssetCache.get(cacheKey) === entry) sceneModelAssetReady.add(cacheKey);
        return result;
      });
      sceneModelAssetCache.set(cacheKey, entry);
    }
    const entry = sceneModelAssetCache.get(cacheKey);
    const state = hydrationMeta && hydrationMeta.state;
    const token = hydrationMeta && hydrationMeta.token;
    const lease = { entry, state, token, generation: hydrationMeta && hydrationMeta.generation,
      modelIndex: hydrationMeta && hydrationMeta.modelIndex, released: false };
    entry.owners.add(lease);
    sceneModelAssetIdle.delete(cacheKey);
    if (state) {
      if (!state._modelAssetLeases) state._modelAssetLeases = new Set();
      state._modelAssetLeases.add(lease);
    }
    if (token) {
      if (!token._modelAssetLeases) token._modelAssetLeases = new Set();
      token._modelAssetLeases.add(lease);
    }
    const result = await entry.promise;
    if (!state || result.error || !sceneModelAcquisitionCurrent(hydrationMeta)) sceneReleaseModelAsset(lease);
    if (sceneModelAcquisitionCurrent(hydrationMeta)) {
      if (result.error) {
        publishSceneModelAssetStatus(mount, "error", key, cached,
          result.error && result.error.message ? result.error.message : "model asset failed to load", statusMeta);
      } else {
        publishSceneModelAssetStatus(mount, cached ? "cached" : "loaded", key, cached, "", statusMeta);
      }
    }
    return result.asset;
  }
