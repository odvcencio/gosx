// Shared Scene3D chunk loading: versioned URLs, CSP policy, coalescing and retry.
var sceneGatedFeaturePromises = Object.create(null), sceneGatedFeatureReported = Object.create(null);

function resolveSceneSubFeatureURL(datasetKey: string, fallback: string) {
  try {
    var tag = document.querySelector<HTMLScriptElement>('script[data-gosx-script="feature-scene3d"]') || document.querySelector<HTMLScriptElement>('script[data-gosx-script="bootstrap"]');
    if (tag && tag.dataset && tag.dataset[datasetKey]) {
      return tag.dataset[datasetKey];
    }
  } catch (_e) {}
  return fallback;
}

function sceneGatedFeatureAPI(kind: string) {
  if (kind === "experience") return window.__gosx_runtime_api.scene3DExperience;
  if (kind === "command") return window.__gosx_scene3d_command_bridge;
  if (kind === "timeline" || kind === "particle-burst") return window.__gosx_scene3d_api?.[kind];
  return kind === "decompress" ? (sceneDecompressAPIFunction("sceneDecompressProps") && window.__gosx_scene3d_api) : kind === "zoom" ? window.__gosx_runtime_api.scene3DZoom : window["__gosx_scene3d_" + kind.replace(/-/g, "_") + "_api"];
}

// A chunk the scene needs but cannot get is reported once per chunk, by name,
// through the runtime issue list: always for a failed fetch or integrity check
// or a missing API, and for a missing URL when the caller passes required
// (it checked the scene content first). Some callers ask speculatively and
// read "not advertised" as "not needed". Callers that draw without the chunk
// still catch the rejection, so the report is what makes the failure visible.
function sceneGatedFeatureFailed(name: string, url: string, error: any) {
  if (!sceneGatedFeatureReported[name]) {
    sceneGatedFeatureReported[name] = true;
    const message = String(error && error.message || error);
    console.error("[gosx] " + message + (url ? " (" + url + ")" : ""));
    window.__gosx?.reportIssue?.({ scope: "scene3d", type: "chunk", component: "GoSXScene3D", ref: name, source: url, message, error, fallback: "none" });
  }
  return Promise.reject(error);
}

function ensureSceneGatedFeatureLoaded(kind: string, datasetKey: string, fallback: string, required?: boolean) {
  const api = sceneGatedFeatureAPI(kind);
  if (api) return Promise.resolve(api);
  if (sceneGatedFeaturePromises[kind]) return sceneGatedFeaturePromises[kind];
  const name = "scene3d-" + kind, url = resolveSceneSubFeatureURL(datasetKey, fallback || "");
  if (!url) { const missing = new Error(name + " chunk URL was not advertised"); return required ? sceneGatedFeatureFailed(name, "", missing) : Promise.reject(missing); }
  const promise = new Promise<any>(function(resolve, reject) {
    const script = document.createElement("script"); script.async = false;
    script.type = "text/javascript";
    script.setAttribute("type", "text/javascript");
    script.setAttribute("crossorigin", "anonymous");
    script.setAttribute("referrerpolicy", "no-referrer");
    script.setAttribute("data-gosx-script", "feature-" + name);
    const integrity = resolveSceneSubFeatureURL(datasetKey.replace(/Url$/, "Integrity"), "");
    if (integrity) script.setAttribute("integrity", integrity);
    script.src = url; script.setAttribute("src", url);
    if (typeof gosxApplyCurrentScriptNonce === "function") gosxApplyCurrentScriptNonce(script);
    script.onload = function() {
      const loaded = sceneGatedFeatureAPI(kind);
      if (loaded) resolve(loaded); else reject(new Error(name + " chunk loaded but did not publish API"));
    };
    script.onerror = function(error) { reject((error as any)?.message ? error : new Error("failed to load " + name + " chunk")); };
    document.head.appendChild(script);
  });
  sceneGatedFeaturePromises[kind] = promise.catch(function(error) {
    delete sceneGatedFeaturePromises[kind];
    if (kind === "webgl" || kind === "webgpu") window["__gosx_scene3d_" + kind + "_feature_promise"] = null;
    return sceneGatedFeatureFailed(name, url, error);
  });
  return sceneGatedFeaturePromises[kind];
}
