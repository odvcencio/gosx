// Shared Scene3D chunk loading: versioned URLs, CSP policy, coalescing and retry.
var sceneGatedFeaturePromises = Object.create(null);

function gosxConfigureSceneScript(script: HTMLScriptElement, role: string, src: string) {
  if (!script) return;
  script.type = "text/javascript";
  script.setAttribute("type", "text/javascript");
  script.setAttribute("crossorigin", "anonymous");
  script.setAttribute("referrerpolicy", "no-referrer");
  if (role) {
    script.setAttribute("data-gosx-script", role);
  }
  if (src) {
    script.src = src;
    script.setAttribute("src", src);
  }
  if (typeof gosxApplyCurrentScriptNonce === "function") {
    gosxApplyCurrentScriptNonce(script);
  }
}

function resolveSceneSubFeatureURL(datasetKey: string, fallback: string) {
  try {
    var tag = document.querySelector<HTMLScriptElement>('script[data-gosx-script="feature-scene3d"]');
    if (tag && tag.dataset && tag.dataset[datasetKey]) {
      return tag.dataset[datasetKey];
    }
  } catch (_e) {}
  return fallback;
}

function sceneGatedFeatureAPI(kind: string) {
  if (kind === "command") return window.__gosx_scene3d_command_bridge;
  return kind === "decompress" ? (sceneDecompressAPIFunction("sceneDecompressProps") && window.__gosx_scene3d_api) : kind === "zoom" ? window.__gosx_runtime_api.scene3DZoom : window["__gosx_scene3d_" + kind.replace(/-/g, "_") + "_api"];
}

function ensureSceneGatedFeatureLoaded(kind: string, datasetKey: string, fallback: string) {
  const api = sceneGatedFeatureAPI(kind);
  if (api) return Promise.resolve(api);
  if (sceneGatedFeaturePromises[kind]) return sceneGatedFeaturePromises[kind];
  const name = "scene3d-" + kind, url = resolveSceneSubFeatureURL(datasetKey, fallback || "");
  if (!url) return Promise.reject(new Error(name + " chunk URL was not advertised"));
  const promise = new Promise<any>(function(resolve, reject) {
    const script = document.createElement("script"); script.async = false;
    gosxConfigureSceneScript(script, "feature-" + name, url);
    script.onload = function() {
      const loaded = sceneGatedFeatureAPI(kind);
      if (loaded) resolve(loaded); else reject(new Error(name + " chunk loaded but did not publish API"));
    };
    script.onerror = function() { reject(new Error("failed to load " + name + " chunk")); };
    document.head.appendChild(script);
  });
  sceneGatedFeaturePromises[kind] = promise.catch(function(error) {
    delete sceneGatedFeaturePromises[kind];
    if (kind === "webgl" || kind === "webgpu") window["__gosx_scene3d_" + kind + "_feature_promise"] = null;
    throw error;
  });
  return sceneGatedFeaturePromises[kind];
}
