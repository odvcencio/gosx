// 10-runtime-scene-utils — the shared runtime helpers every route needs:
// manifest loading, browser capability tests, signal queues, gamepad polling,
// and the small numeric and CSS-variable helpers.
//
// Chunks: bootstrap.js, bootstrap-runtime.js, bootstrap-feature-scene3d.js.
// This file used to be the first 1162 lines of 10-runtime-scene-core.js, and
// the build cut it out with two literal source markers. It is now a file, so
// a re-indent cannot change what the selective runtime ships.
// 10-runtime-scene-core.js reads these helpers and must load after it.
  // Pending manifest reference, set during init, consumed when runtime is ready.
  let pendingManifest = null;

  function runtimeReady() {
    return (
      typeof window.__gosx_hydrate === "function" ||
      typeof window.__gosx_action === "function" ||
      typeof window.__gosx_set_shared_signal === "function"
    );
  }

  // --------------------------------------------------------------------------
  // Manifest loading
  // --------------------------------------------------------------------------

  // Parse the inline JSON manifest from #gosx-manifest script tag.
  // Returns the parsed object, or null if missing/malformed.
  //
  // The parse is memoized per element identity: the WebGPU probe and the
  // runtime tail both call this during one boot, and the manifest can be
  // hundreds of kilobytes, so parsing it once instead of once per caller is a
  // real main-thread saving. A soft navigation swaps in a new element, which
  // misses the memo and re-parses.
  //
  // The memoized result is also published as window.__gosx_manifest so code in
  // other bundles (the scene3d feature reads water shader sources, for one)
  // can reuse the parse instead of re-reading and re-parsing the DOM text.
  //
  // When the element carries data-gosx-release, the JSON text is removed from
  // the DOM after the parse: the string is dead weight once the object graph
  // exists. This is opt-in because pages may carry their own scripts that read
  // the element's text later; a page opts in only once every consumer goes
  // through the published parse.
  function loadManifest() {
    const el = document.getElementById("gosx-manifest");
    if (!el) return null;

    const memo = window.__gosx_manifest;
    if (memo && memo.element === el) {
      return memo.value;
    }

    try {
      const raw = el.textContent;
      const value = JSON.parse(raw);
      window.__gosx_manifest = {
        element: el,
        value: value,
        textHasLabel: typeof raw === "string" && raw.indexOf('"label"') >= 0,
      };
      if (el.hasAttribute && el.hasAttribute("data-gosx-release")) {
        el.textContent = "";
      }
      return value;
    } catch (e) {
      console.error("[gosx] failed to parse manifest:", e);
      if (window.__gosx && typeof window.__gosx.reportIssue === "function") {
        window.__gosx.reportIssue({
          scope: "bootstrap",
          type: "manifest",
          source: "gosx-manifest",
          element: el,
          message: "failed to parse gosx manifest",
          error: e,
          fallback: "server",
        });
      }
      return null;
    }
  }

  // --------------------------------------------------------------------------
  // Shader-lib hydrate: inflate shaderLib refs back to inline fields
  // --------------------------------------------------------------------------

  // Registry of {collection, field} pairs that participate in shader-lib dedup.
  // Must mirror the Go shaderLibFields registry in scene/shader_lib.go.
  const SHADER_LIB_FIELDS = [
    { collection: "computeParticles", field: "computeWGSL" },
    { collection: "waterSystems",     field: "seedWGSL" },
    { collection: "waterSystems",     field: "dropWGSL" },
    { collection: "waterSystems",     field: "displacementWGSL" },
    { collection: "waterSystems",     field: "simulationWGSL" },
    { collection: "waterSystems",     field: "normalWGSL" },
    { collection: "waterSystems",     field: "causticsWGSL" },
    { collection: "waterSystems",     field: "poolVertexWGSL" },
    { collection: "waterSystems",     field: "poolFragmentWGSL" },
    { collection: "waterSystems",     field: "surfaceVertexWGSL" },
    { collection: "waterSystems",     field: "surfaceFragmentWGSL" },
    { collection: "waterSystems",     field: "surfaceBelowFragmentWGSL" },
    { collection: "waterSystems",     field: "objectShadowWGSL" },
    { collection: "waterSystems",     field: "objectMeshShadowVertexWGSL" },
    { collection: "waterSystems",     field: "objectMeshShadowFragmentWGSL" },
    { collection: "objects",          field: "customVertex" },
    { collection: "objects",          field: "customFragment" },
    { collection: "objects",          field: "customVertexWGSL" },
    { collection: "objects",          field: "customFragmentWGSL" },
    { collection: "models",           field: "customVertex" },
    { collection: "models",           field: "customFragment" },
    { collection: "models",           field: "customVertexWGSL" },
    { collection: "models",           field: "customFragmentWGSL" },
    // Points authored-material fields (S2).
    { collection: "points",           field: "customVertex" },
    { collection: "points",           field: "customFragment" },
    { collection: "points",           field: "customVertexWGSL" },
    { collection: "points",           field: "customFragmentWGSL" },
    // ComputeParticles render-pass authored-material fields (S3).
    { collection: "computeParticles", field: "renderVertex" },
    { collection: "computeParticles", field: "renderFragment" },
    { collection: "computeParticles", field: "renderVertexWGSL" },
    { collection: "computeParticles", field: "renderFragmentWGSL" },
    // Named material profile authored shader fields (S4 — composable <Material>
    // elements; same envelope as objects/points so one .sel shader can deduplicate
    // across all ~21 galaxy profiles via a single shaderLib entry after dedup).
    { collection: "materials",        field: "customVertex" },
    { collection: "materials",        field: "customFragment" },
    { collection: "materials",        field: "customVertexWGSL" },
    { collection: "materials",        field: "customFragmentWGSL" },
    // InstancedMesh Elio GPU cull kernel — hoisted when ≥2 meshes share the
    // same kernel source. Mirror entry exists in Go shaderLibFields (scene/shader_lib.go).
    { collection: "instancedMeshes",  field: "cullKernelWGSL" },
    { collection: "postEffects",      field: "fragmentWGSL" },
    { collection: "postEffects",      field: "vertexWGSL" },
    { collection: "postEffects",      field: "fragmentGLSL" },
    { collection: "postEffects",      field: "vertexGLSL" },
  ];

  // inflateSceneShaderLib walks a parsed scene object (props.scene), replaces
  // every *Ref field whose id exists in scene.shaderLib with the source string,
  // and removes the shaderLib key. Defensive: missing lib entry leaves the
  // base field absent (downstream builtin fallbacks handle absent shaders).
  // Never throws.
  function inflateSceneShaderLib(scene) {
    if (!scene || typeof scene !== "object") return;
    var lib = scene.shaderLib;
    if (!lib || typeof lib !== "object") return;

    for (var fi = 0; fi < SHADER_LIB_FIELDS.length; fi++) {
      var desc = SHADER_LIB_FIELDS[fi];
      var items = scene[desc.collection];
      if (!Array.isArray(items)) continue;
      var refKey = desc.field + "Ref";
      for (var i = 0; i < items.length; i++) {
        var node = items[i];
        if (!node || typeof node !== "object") continue;
        var id = node[refKey];
        if (typeof id !== "string") continue;
        var src = lib[id];
        if (typeof src === "string") {
          node[desc.field] = src;
        }
        // Always delete the ref key regardless — keep the wire shape clean.
        delete node[refKey];
      }
    }
    delete scene.shaderLib;
  }

  // inflateManifestShaderLibs walks all island and computeIsland entries in a
  // manifest, inflating shaderLib refs in each entry's props.scene. Called once
  // immediately after loadManifest() returns, before the manifest is stashed as
  // pendingManifest. Mutates the manifest in-place.
  function inflateManifestShaderLibs(manifest, options) {
    if (!manifest || typeof manifest !== "object") return;
    var publish = !options || options.publish !== false;
    var allEntries = [];
    if (Array.isArray(manifest.islands)) {
      for (var i = 0; i < manifest.islands.length; i++) allEntries.push(manifest.islands[i]);
    }
    if (Array.isArray(manifest.computeIslands)) {
      for (var i = 0; i < manifest.computeIslands.length; i++) allEntries.push(manifest.computeIslands[i]);
    }
    if (Array.isArray(manifest.engines)) {
      for (var i = 0; i < manifest.engines.length; i++) allEntries.push(manifest.engines[i]);
    }
    for (var j = 0; j < allEntries.length; j++) {
      var entry = allEntries[j];
      if (entry && entry.props && typeof entry.props === "object") {
        inflateSceneShaderLib(entry.props.scene);
      }
    }
    if (publish) {
      publishManifestWaterShaderSources(allEntries);
    }
  }

  function publishManifestWaterShaderSources(entries) {
    if (typeof window === "undefined" || !Array.isArray(entries)) return;
    var fields = [
      "seedWGSL", "dropWGSL", "displacementWGSL", "simulationWGSL", "normalWGSL", "causticsWGSL",
      "poolVertexWGSL", "poolFragmentWGSL", "surfaceVertexWGSL", "surfaceFragmentWGSL", "surfaceBelowFragmentWGSL",
      "objectShadowWGSL", "objectMeshShadowVertexWGSL", "objectMeshShadowFragmentWGSL",
    ];
    var published = window.__gosx_scene3d_water_shader_sources_by_id || {};
    for (var ei = 0; ei < entries.length; ei += 1) {
      var scene = entries[ei] && entries[ei].props && entries[ei].props.scene;
      var systems = scene && Array.isArray(scene.waterSystems) ? scene.waterSystems : [];
      for (var wi = 0; wi < systems.length; wi += 1) {
        var water = systems[wi];
        if (!water || typeof water !== "object") continue;
        var id = typeof water.id === "string" && water.id ? water.id : ("scene-water-" + wi);
        var record = published[id] || { id: id };
        var changed = false;
        for (var fi = 0; fi < fields.length; fi += 1) {
          var name = fields[fi];
          if (typeof water[name] === "string" && water[name].trim()) {
            record[name] = water[name];
            changed = true;
          }
        }
        if (changed) published[id] = record;
      }
    }
    window.__gosx_scene3d_water_shader_sources_by_id = published;
  }

  // --------------------------------------------------------------------------
  // Shared WASM runtime loading
  // --------------------------------------------------------------------------

  // Load the single shared Go WASM binary referenced by the manifest runtime
  // entry. Uses Go's wasm_exec.js `Go` class. The WASM is expected to call
  // window.__gosx_runtime_ready() once it has finished initializing its
  // exported functions (__gosx_hydrate, __gosx_action, etc.).
  async function loadRuntime(runtimeRef) {
    // O4's typed loader owns the direct ABI handshake and binary mailbox
    // surface. Keep this function as the compatibility entry point until O6
    // removes the authored runtime shim entirely.
    const typedLoader = window.__gosx && window.__gosx.runtime && window.__gosx.runtime.loader;
    if (typedLoader && typeof typedLoader.load === "function") {
      return typedLoader.load(runtimeRef);
    }
    if (typeof Go === "undefined") {
      console.error("[gosx] wasm_exec.js must be loaded before bootstrap.js");
      if (window.__gosx && typeof window.__gosx.reportIssue === "function") {
        window.__gosx.reportIssue({
          scope: "bootstrap",
          type: "runtime",
          source: runtimeRef && runtimeRef.path,
          ref: runtimeRef && runtimeRef.path,
          message: "wasm_exec.js must be loaded before bootstrap.js",
          fallback: "server",
        });
      }
      return;
    }

    const go = new Go();

    try {
      const response = await fetchRuntimeResponse(runtimeRef);
      const result = await instantiateRuntimeModule(response, go.importObject);
      // go.run is intentionally not awaited — it resolves when the Go main()
      // exits, but the runtime stays alive via syscall/js callbacks.
      go.run(result.instance);
    } catch (e) {
      console.error("[gosx] failed to load WASM runtime:", e);
      if (window.__gosx && typeof window.__gosx.reportIssue === "function") {
        window.__gosx.reportIssue({
          scope: "bootstrap",
          type: "runtime",
          source: runtimeRef && runtimeRef.path,
          ref: runtimeRef && runtimeRef.path,
          message: "failed to load wasm runtime",
          error: e,
          fallback: "server",
        });
      }
    }
  }

  async function fetchRuntimeResponse(runtimeRef) {
    const response = await fetch(runtimeRef.path);
    if (!response.ok) {
      throw new Error("runtime fetch failed with status " + response.status);
    }
    return response;
  }

  async function instantiateRuntimeModule(response, importObject) {
    if (supportsInstantiateStreaming()) {
      return instantiateRuntimeStreaming(response, importObject);
    }
    return instantiateRuntimeBytes(response, importObject);
  }

  function supportsInstantiateStreaming() {
    return typeof WebAssembly.instantiateStreaming === "function";
  }

  async function instantiateRuntimeStreaming(response, importObject) {
    try {
      return await WebAssembly.instantiateStreaming(response.clone(), importObject);
    } catch (streamErr) {
      return instantiateRuntimeBytes(response, importObject);
    }
  }

  async function instantiateRuntimeBytes(response, importObject) {
    const bytes = await response.arrayBuffer();
    return WebAssembly.instantiate(bytes, importObject);
  }

  // --------------------------------------------------------------------------
  // Island program fetching
  // --------------------------------------------------------------------------

  // Fetch the compiled program data for a single island. Returns an
  // ArrayBuffer (for "wasm" format) or a string (for "json" or other text
  // formats). Returns null on failure.
  async function fetchProgram(programRef, programFormat) {
    try {
      const resp = await fetch(programRef);
      if (!resp.ok) {
        console.error(`[gosx] failed to fetch program ${programRef}: ${resp.status}`);
        return null;
      }

      if (programFormat === "wasm" || programFormat === "bin") {
        return new Uint8Array(await resp.arrayBuffer());
      }
      // Default: return as text (covers json, msgpack-base64, etc.)
      return await resp.text();
    } catch (e) {
      console.error(`[gosx] error fetching program ${programRef}:`, e);
      return null;
    }
  }

  function inferProgramFormat(entry) {
    if (entry.programFormat) return entry.programFormat;
    if (typeof entry.programRef === "string" && entry.programRef.endsWith(".gxi")) {
      return "bin";
    }
    return "json";
  }

  const loadedScriptTags = new Map();

  function gosxScriptNonceValue(script) {
    if (!script) return "";
    return String(script.nonce || (script.getAttribute && script.getAttribute("nonce")) || "");
  }

  function gosxCurrentScriptNonce() {
    var current = gosxScriptNonceValue(document.currentScript);
    if (current) return current;
    var selectors = [
      "script[nonce][data-gosx-script]",
      "script[nonce][data-gosx-navigation]",
      "script[nonce][data-gosx-document-contract]",
      "script[nonce]",
    ];
    for (var i = 0; i < selectors.length; i++) {
      var found = document.querySelector(selectors[i]);
      var nonce = gosxScriptNonceValue(found);
      if (nonce) return nonce;
    }
    return "";
  }

  function gosxApplyCurrentScriptNonce(script) {
    var nonce = gosxCurrentScriptNonce();
    if (nonce && script) {
      script.nonce = nonce;
    }
  }

  function loadScriptTag(src, role) {
    if (!src) return Promise.resolve();
    if (loadedScriptTags.has(src)) {
      return loadedScriptTags.get(src);
    }
    const promise = new Promise(function(resolve, reject) {
      const script = document.createElement("script");
      script.src = src;
      script.setAttribute("src", src);
      script.type = "text/javascript";
      script.setAttribute("type", "text/javascript");
      script.setAttribute("crossorigin", "anonymous");
      script.setAttribute("referrerpolicy", "no-referrer");
      script.setAttribute("data-gosx-script", role || "managed-runtime");
      gosxApplyCurrentScriptNonce(script);
      script.onload = resolve;
      script.onerror = function() {
        reject(new Error("failed to load script: " + src));
      };
      (document.head || document.documentElement).appendChild(script);
    });
    loadedScriptTags.set(src, promise);
    return promise;
  }

  function engineFrame(callback) {
    if (typeof window.requestAnimationFrame === "function") {
      return window.requestAnimationFrame(callback);
    }
    return setTimeout(function() {
      callback(Date.now());
    }, 16);
  }

  function cancelEngineFrame(handle) {
    if (typeof window.cancelAnimationFrame === "function") {
      window.cancelAnimationFrame(handle);
      return;
    }
    clearTimeout(handle);
  }

  function gosxInputState() {
    if (!window.__gosx.input) {
      window.__gosx.input = {
        pending: null,
        frameHandle: 0,
        providers: Object.create(null),
      };
    }
    return window.__gosx.input;
  }

  function queueInputSignal(name, value) {
    if (!name) return;
    const state = gosxInputState();
    if (!state.pending) {
      state.pending = Object.create(null);
    }
    state.pending[name] = value;
    scheduleInputFlush();
  }

  function scheduleInputFlush() {
    const state = gosxInputState();
    if (state.frameHandle) return;
    state.frameHandle = engineFrame(function() {
      state.frameHandle = 0;
      flushInputSignals();
    });
  }

  function flushInputSignals() {
    const state = gosxInputState();
    const payload = state.pending;
    state.pending = null;
    if (!payload) return;

    const setInputBatch = window.__gosx_set_input_batch;
    if (typeof setInputBatch !== "function") {
      for (const [name, value] of Object.entries(payload)) {
        setSharedSignalValue(name, value);
      }
      return;
    }

    try {
      const result = setInputBatch(JSON.stringify(payload));
      if (typeof result === "string" && result !== "") {
        console.error("[gosx] input batch error:", result);
      }
    } catch (e) {
      console.error("[gosx] input batch error:", e);
    }
  }

  function capabilityList(entry) {
    return Array.isArray(entry && entry.capabilities) ? entry.capabilities : [];
  }

  const browserCapabilityCache = Object.create(null);

  function normalizeCapabilityName(value) {
    return String(value == null ? "" : value).trim().toLowerCase();
  }

  function appendCapabilityValues(value, append) {
    if (Array.isArray(value)) {
      for (const item of value) {
        append(item);
      }
      return;
    }
    if (typeof value === "string") {
      for (const item of value.replace(/[,|]/g, " ").split(/\s+/)) {
        append(item);
      }
      return;
    }
    if (value != null) {
      append(value);
    }
  }

  function requiredCapabilityList(entry) {
    const out = [];
    const seen = Object.create(null);
    const append = function(raw) {
      const capability = normalizeCapabilityName(raw);
      if (!capability || seen[capability]) {
        return;
      }
      seen[capability] = true;
      out.push(capability);
    };
    appendCapabilityValues(entry && entry.requiredCapabilities, append);
    appendCapabilityValues(entry && entry.requires, append);
    if (entry && (entry.runtime === "shared" || entry.programRef)) {
      append("wasm");
    }
    return out;
  }

  function runtimeCapabilityStatus(entry) {
    const required = requiredCapabilityList(entry);
    const supported = [];
    const missing = [];
    for (const capability of required) {
      if (browserCapabilitySupported(capability)) {
        supported.push(capability);
      } else {
        missing.push(capability);
      }
    }
    return {
      ok: missing.length === 0,
      requested: capabilityList(entry).slice(),
      required,
      supported,
      missing,
    };
  }

  function engineCapabilityStatus(entry) {
    return runtimeCapabilityStatus(entry);
  }

  function browserCapabilitySupported(capability) {
    const name = normalizeCapabilityName(capability);
    if (!name) {
      return true;
    }
    const dynamicWebGPUFeature = name.indexOf("webgpu:") === 0 || name.indexOf("webgpu-feature:") === 0;
    if (!dynamicWebGPUFeature && Object.prototype.hasOwnProperty.call(browserCapabilityCache, name)) {
      return browserCapabilityCache[name];
    }
    let supported = false;
    try {
      supported = detectBrowserCapability(name);
    } catch (_e) {
      supported = false;
    }
    if (dynamicWebGPUFeature) {
      return Boolean(supported);
    }
    browserCapabilityCache[name] = Boolean(supported);
    return browserCapabilityCache[name];
  }

  function detectBrowserCapability(name) {
    if (name.indexOf("webgpu:adapter-limit:") === 0) {
      return detectWebGPULimitCapability(name.slice("webgpu:adapter-limit:".length), "adapter");
    }
    if (name.indexOf("webgpu:device-limit:") === 0) {
      return detectWebGPULimitCapability(name.slice("webgpu:device-limit:".length), "device");
    }
    if (name.indexOf("webgpu:limit:") === 0) {
      return detectWebGPULimitCapability(name.slice("webgpu:limit:".length), "device");
    }
    if (name.indexOf("webgpu-limit:") === 0) {
      return detectWebGPULimitCapability(name.slice("webgpu-limit:".length), "device");
    }
    if (name.indexOf("webgpu:") === 0) {
      return detectWebGPUFeatureCapability(name.slice("webgpu:".length));
    }
    if (name.indexOf("webgpu-feature:") === 0) {
      return detectWebGPUFeatureCapability(name.slice("webgpu-feature:".length));
    }
    switch (name) {
      case "animation":
        return typeof requestAnimationFrame === "function";
      case "audio":
        return typeof AudioContext === "function" || typeof webkitAudioContext === "function" || canCreateElement("audio");
      case "canvas":
        return canCreateCanvas();
      case "canvas2d":
      case "pixel-surface":
        return canCreateCanvasContext("2d");
      case "clipboard":
        return Boolean(
          typeof navigator !== "undefined" &&
          navigator &&
          navigator.clipboard &&
          typeof navigator.clipboard.writeText === "function"
        );
      case "compute":
        return browserCapabilitySupported("webgpu") || browserCapabilitySupported("webgl2");
      case "fetch":
        return typeof fetch === "function";
      case "gamepad":
        return Boolean(typeof navigator !== "undefined" && navigator && typeof navigator.getGamepads === "function");
      case "keyboard":
      case "pointer":
        return Boolean(document && typeof document.addEventListener === "function");
      case "pointer-lock":
        return Boolean(
          document &&
          (typeof document.exitPointerLock === "function" || "pointerLockElement" in document) &&
          typeof document.createElement === "function" &&
          typeof document.createElement("canvas").requestPointerLock === "function"
        );
      case "storage":
        return canUseLocalStorage();
      case "text-input":
        return canCreateTextInput();
      case "video":
        return canCreateVideo();
      case "wasm":
        return Boolean(typeof WebAssembly === "object" && WebAssembly && (
          typeof WebAssembly.instantiate === "function" ||
          typeof WebAssembly.instantiateStreaming === "function"
        ));
      case "webgl":
        return canCreateCanvasContext("webgl") || canCreateCanvasContext("experimental-webgl") || canCreateCanvasContext("webgl2");
      case "webgl2":
        return canCreateCanvasContext("webgl2");
      case "webgpu":
        return Boolean(typeof navigator !== "undefined" && navigator && navigator.gpu);
      case "worker":
        return typeof Worker === "function";
      default:
        return false;
    }
  }

  function detectWebGPUFeatureCapability(feature) {
    const normalized = normalizeCapabilityName(feature);
    if (!normalized || !browserCapabilitySupported("webgpu")) {
      return false;
    }
    const diagnostics = typeof window !== "undefined" && typeof window.__gosx_scene3d_webgpu_diagnostics === "function"
      ? window.__gosx_scene3d_webgpu_diagnostics()
      : null;
    if (!diagnostics || diagnostics.ready !== true) {
      return false;
    }
    const deviceFeatures = Array.isArray(diagnostics.deviceFeatures) ? diagnostics.deviceFeatures : [];
    const requestedFeatures = Array.isArray(diagnostics.requestedFeatures) ? diagnostics.requestedFeatures : [];
    return deviceFeatures.indexOf(normalized) >= 0 || requestedFeatures.indexOf(normalized) >= 0;
  }

  function detectWebGPULimitCapability(requirement, scope) {
    if (!browserCapabilitySupported("webgpu")) {
      return false;
    }
    const diagnostics = typeof window !== "undefined" && typeof window.__gosx_scene3d_webgpu_diagnostics === "function"
      ? window.__gosx_scene3d_webgpu_diagnostics()
      : null;
    if (!diagnostics || diagnostics.ready !== true) {
      return false;
    }
    const parsed = parseWebGPULimitRequirement(requirement);
    if (!parsed) {
      return false;
    }
    const primary = scope === "adapter" ? diagnostics.adapterLimits : diagnostics.deviceLimits;
    const fallback = scope === "adapter" ? diagnostics.deviceLimits : diagnostics.adapterLimits;
    let actual = lookupWebGPULimit(primary, parsed.name);
    if (!Number.isFinite(actual)) {
      actual = lookupWebGPULimit(fallback, parsed.name);
    }
    if (!Number.isFinite(actual)) {
      return false;
    }
    switch (parsed.operator) {
      case ">":
        return actual > parsed.value;
      case "<":
        return actual < parsed.value;
      case "<=":
        return actual <= parsed.value;
      case "=":
      case "==":
        return actual === parsed.value;
      case ">=":
      default:
        return actual >= parsed.value;
    }
  }

  function parseWebGPULimitRequirement(requirement) {
    const text = String(requirement || "").trim();
    const match = text.match(/^([a-z0-9_.:-]+)\s*(>=|<=|==|>|<|=|:)\s*([0-9]+(?:\.[0-9]+)?)$/i);
    if (!match) {
      return null;
    }
    const value = Number(match[3]);
    if (!Number.isFinite(value)) {
      return null;
    }
    return {
      name: match[1],
      operator: match[2] === ":" ? ">=" : match[2],
      value,
    };
  }

  function lookupWebGPULimit(limits, name) {
    if (!limits || typeof limits !== "object") {
      return NaN;
    }
    const wanted = normalizeWebGPULimitName(name);
    for (const key of Object.keys(limits)) {
      if (normalizeWebGPULimitName(key) !== wanted) {
        continue;
      }
      const value = Number(limits[key]);
      return Number.isFinite(value) ? value : NaN;
    }
    return NaN;
  }

  function normalizeWebGPULimitName(name) {
    return String(name || "").trim().toLowerCase().replace(/[^a-z0-9]/g, "");
  }

  function canCreateElement(tagName) {
    if (!document || typeof document.createElement !== "function") {
      return false;
    }
    return Boolean(document.createElement(tagName));
  }

  function canCreateCanvas() {
    if (!document || typeof document.createElement !== "function") {
      return false;
    }
    const canvas = document.createElement("canvas");
    return Boolean(canvas && typeof canvas.getContext === "function");
  }

  function canCreateCanvasContext(kind) {
    if (!document || typeof document.createElement !== "function") {
      return false;
    }
    const canvas = document.createElement("canvas");
    if (!canvas || typeof canvas.getContext !== "function") {
      return false;
    }
    return Boolean(canvas.getContext(kind));
  }

  function canCreateTextInput() {
    if (!document || typeof document.createElement !== "function") {
      return false;
    }
    const input = document.createElement("input");
    return Boolean(input && typeof input.focus === "function");
  }

  function canCreateVideo() {
    if (!document || typeof document.createElement !== "function") {
      return false;
    }
    const video = document.createElement("video");
    return Boolean(video && typeof video.canPlayType === "function");
  }

  function canUseLocalStorage() {
    try {
      if (!window || !window.localStorage) {
        return false;
      }
      const key = "__gosx_capability_probe__";
      window.localStorage.setItem(key, "1");
      window.localStorage.removeItem(key);
      return true;
    } catch (_e) {
      return false;
    }
  }

  function applyRuntimeCapabilityState(element, scope, status) {
    if (!element || !status) {
      return;
    }
    const prefix = "data-gosx-" + (scope || "runtime") + "-";
    element.setAttribute(prefix + "capability-state", status.ok ? "ready" : "unsupported");
    element.setAttribute(prefix + "required-capabilities", status.required.join(" "));
    element.setAttribute(prefix + "supported-capabilities", status.supported.join(" "));
    if (status.missing.length > 0) {
      element.setAttribute(prefix + "missing-capabilities", status.missing.join(" "));
    } else if (typeof element.removeAttribute === "function") {
      element.removeAttribute(prefix + "missing-capabilities");
    }
  }

  function activateInputProviders(entry) {
    for (const capability of capabilityList(entry)) {
      activateInputProvider(capability, entry);
    }
  }

  function activateInputProvider(capability, entry) {
    const state = gosxInputState();
    const current = state.providers[capability];
    if (current) {
      current.refCount += 1;
      return;
    }

    const provider = createInputProvider(capability, entry);
    if (!provider) {
      return;
    }

    provider.refCount = 1;
    state.providers[capability] = provider;
  }

  function releaseInputProviders(record) {
    for (const capability of capabilityList(record)) {
      releaseInputProvider(capability);
    }
  }

  function releaseInputProvider(capability) {
    const state = gosxInputState();
    const provider = state.providers[capability];
    if (!provider) return;

    provider.refCount -= 1;
    if (provider.refCount > 0) {
      return;
    }

    if (typeof provider.dispose === "function") {
      provider.dispose();
    }
    delete state.providers[capability];
  }

  function createInputProvider(capability, entry) {
    switch (capability) {
      case "keyboard":
        return createKeyboardInputProvider();
      case "pointer":
        return createPointerInputProvider();
      case "gamepad":
        return createGamepadInputProvider();
      case "text-input":
        return createTextInputProvider(entry);
      default:
        return null;
    }
  }

  function createKeyboardInputProvider() {
    const pressed = new Set();

    function onKey(event) {
      const key = normalizeKeyName(event);
      if (!key) return;
      const active = event.type === "keydown";
      if (active) {
        pressed.add(key);
      } else {
        pressed.delete(key);
      }
      queueInputSignal("$input.key." + key, active);
    }

    function onBlur() {
      for (const key of Array.from(pressed)) {
        queueInputSignal("$input.key." + key, false);
      }
      pressed.clear();
    }

    return bindInputProviderListeners([
      [document, "keydown", onKey],
      [document, "keyup", onKey],
      [window, "blur", onBlur],
    ]);
  }

  function createPointerInputProvider() {
    const state = { lastX: null, lastY: null };

    function publishPointer(event) {
      publishPointerSignals(resolvePointerSample(event, state), event);
    }

    function onBlur() {
      resetPointerSignals();
    }

    return bindInputProviderListeners([
      [document, "pointermove", publishPointer],
      [document, "pointerdown", publishPointer],
      [document, "pointerup", publishPointer],
      [window, "blur", onBlur],
    ]);
  }

  function createGamepadInputProvider() {
    let active = true;
    let frameHandle = 0;

    function pollGamepad() {
      if (!active) return;
      const navigatorRef = window.navigator;
      if (navigatorRef && typeof navigatorRef.getGamepads === "function") {
        const pads = navigatorRef.getGamepads() || [];
        let connected = 0;
        for (let i = 0; i < 2; i++) {
          const pad = pads[i];
          if (pad && pad.connected !== false) {
            connected += 1;
            publishGamepadSignals(pad, i);
          } else {
            queueInputSignal("$input.gamepad" + i + ".connected", false);
          }
        }
        queueInputSignal("$input.gamepad.count", connected);
      }
      frameHandle = engineFrame(pollGamepad);
    }

    frameHandle = engineFrame(pollGamepad);

    return {
      dispose() {
        active = false;
        if (frameHandle) {
          cancelEngineFrame(frameHandle);
          frameHandle = 0;
        }
      },
    };
  }

  function createTextInputProvider(entry) {
    var inputEl = null;
    var mount = null;
    var unsubCursorRect = null;
    var unsubClipboard = null;
    var viewportListener = null;

    // Resolve the engine mount element from the entry.
    var mountID = entry && (entry.mountId || entry.id);
    mount = mountID ? document.getElementById(mountID) : null;
    if (!mount) {
      mount = document.body;
    }

    // Create transparent contenteditable for IME/keyboard activation.
    inputEl = document.createElement("div");
    inputEl.contentEditable = "true";
    inputEl.setAttribute("role", "textbox");
    inputEl.setAttribute("aria-multiline", "true");
    inputEl.style.cssText = "position:absolute;opacity:0;width:1px;height:1em;overflow:hidden;white-space:pre;pointer-events:none;z-index:-1";
    if (!mount.style.position || mount.style.position === "static") {
      mount.style.position = "relative";
    }
    mount.appendChild(inputEl);

    function focusInput(e) {
      if (e.target !== inputEl) inputEl.focus();
    }

    mount.addEventListener("mousedown", focusInput);
    mount.addEventListener("touchstart", focusInput);

    // beforeinput — text insertion, deletion, newline
    inputEl.addEventListener("beforeinput", function(e) {
      var type = e.inputType;
      if (type === "insertText" || type === "insertReplacementText") {
        e.preventDefault();
        if (e.data) queueInputSignal("$input.text.inserted", e.data);
      } else if (type === "insertFromPaste") {
        e.preventDefault();
        var text = e.dataTransfer ? e.dataTransfer.getData("text/plain") : "";
        if (text) queueInputSignal("$input.clipboard.paste", text);
      } else if (type === "deleteContentBackward") {
        e.preventDefault();
        queueInputSignal("$input.command", "delete_backward");
      } else if (type === "deleteContentForward") {
        e.preventDefault();
        queueInputSignal("$input.command", "delete_forward");
      } else if (type === "insertLineBreak" || type === "insertParagraph") {
        e.preventDefault();
        queueInputSignal("$input.command", "newline");
      }
      requestAnimationFrame(function() { if (inputEl) inputEl.textContent = ""; });
    });

    // Composition (IME)
    inputEl.addEventListener("compositionstart", function() {
      queueInputSignal("$input.text.composition_active", true);
    });
    inputEl.addEventListener("compositionupdate", function(e) {
      queueInputSignal("$input.text.composing", e.data || "");
    });
    inputEl.addEventListener("compositionend", function(e) {
      queueInputSignal("$input.text.composition_active", false);
      queueInputSignal("$input.text.composing", "");
      if (e.data) queueInputSignal("$input.text.inserted", e.data);
    });

    // Keyboard commands (arrows, shortcuts)
    inputEl.addEventListener("keydown", function(e) {
      if (e.isComposing) return;
      var mod = e.metaKey || e.ctrlKey;
      var shift = e.shiftKey;
      var command = null;

      switch (e.key) {
        case "ArrowUp":    command = shift ? "select_up" : "move_up"; break;
        case "ArrowDown":  command = shift ? "select_down" : "move_down"; break;
        case "ArrowLeft":  command = shift ? "select_left" : "move_left"; break;
        case "ArrowRight": command = shift ? "select_right" : "move_right"; break;
        case "Home":       command = shift ? "select_line_start" : "move_line_start"; break;
        case "End":        command = shift ? "select_line_end" : "move_line_end"; break;
        case "Tab":        command = shift ? "dedent" : "indent"; break;
        case "Escape":     command = "escape"; break;
      }

      if (!command && mod) {
        switch (e.key.toLowerCase()) {
          case "z": command = shift ? "redo" : "undo"; break;
          case "a": command = "select_all"; break;
          case "s": command = "save"; break;
          case "b": command = "bold"; break;
          case "i": command = "italic"; break;
        }
      }

      if (command) {
        e.preventDefault();
        queueInputSignal("$input.command", command);
      }
    });

    // File drop
    mount.addEventListener("dragover", function(e) { e.preventDefault(); });
    mount.addEventListener("drop", function(e) {
      e.preventDefault();
      var files = e.dataTransfer ? e.dataTransfer.files : null;
      if (files && files.length > 0) {
        var file = files[0];
        if (file.type.startsWith("image/")) {
          var reader = new FileReader();
          reader.onload = function() {
            queueInputSignal("$editor.file_drop", JSON.stringify({
              name: file.name, type: file.type, size: file.size, data: reader.result
            }));
          };
          reader.readAsDataURL(file);
        }
      }
    });

    // Mobile keyboard height. Passive: this handler only queues a signal
    // write — it never calls preventDefault. On iOS Safari a non-passive
    // visualViewport listener blocks the scroll thread during keyboard
    // show/hide animations, which manifests as jank and stale canvas
    // frames on the keyboard transition.
    if (window.visualViewport) {
      viewportListener = function() {
        var kh = window.innerHeight - window.visualViewport.height;
        queueInputSignal("$input.keyboard_height", Math.max(0, kh));
      };
      window.visualViewport.addEventListener("resize", viewportListener, { passive: true });
    }

    // Cursor position tracking for IME placement
    unsubCursorRect = gosxSubscribeSharedSignal("$editor.cursor_rect", function(rect) {
      if (!inputEl) return;
      var r = typeof rect === "string" ? JSON.parse(rect) : rect;
      if (r) {
        inputEl.style.left = (r.x || 0) + "px";
        inputEl.style.top = (r.y || 0) + "px";
        inputEl.style.height = (r.height || 20) + "px";
      }
    });

    // Clipboard content sync for copy/cut
    unsubClipboard = gosxSubscribeSharedSignal("$editor.clipboard_content", function(text) {
      if (!inputEl || !text) return;
      inputEl.textContent = text;
      var range = document.createRange();
      range.selectNodeContents(inputEl);
      var sel = window.getSelection();
      sel.removeAllRanges();
      sel.addRange(range);
    });

    inputEl.focus();

    return {
      dispose: function() {
        if (unsubCursorRect) { unsubCursorRect(); unsubCursorRect = null; }
        if (unsubClipboard) { unsubClipboard(); unsubClipboard = null; }
        if (viewportListener && window.visualViewport) {
          window.visualViewport.removeEventListener("resize", viewportListener);
          viewportListener = null;
        }
        if (mount) {
          mount.removeEventListener("mousedown", focusInput);
          mount.removeEventListener("touchstart", focusInput);
        }
        if (inputEl && inputEl.parentNode) {
          inputEl.parentNode.removeChild(inputEl);
        }
        inputEl = null;
        mount = null;
      },
    };
  }

  function bindInputProviderListeners(bindings) {
    for (const binding of bindings) {
      binding[0].addEventListener(binding[1], binding[2]);
    }
    return {
      dispose() {
        for (const binding of bindings) {
          binding[0].removeEventListener(binding[1], binding[2]);
        }
      },
    };
  }

  function normalizeKeyName(event) {
    const raw = event && (event.key || event.code);
    if (!raw) return "";
    return String(raw).trim().toLowerCase();
  }

  function resolvePointerSample(event, state) {
    const previousX = state.lastX == null ? 0 : state.lastX;
    const previousY = state.lastY == null ? 0 : state.lastY;
    const x = sceneNumber(event && event.clientX, previousX);
    const y = sceneNumber(event && event.clientY, previousY);
    const sample = {
      x,
      y,
      deltaX: sceneNumber(event && event.movementX, state.lastX == null ? 0 : x - previousX),
      deltaY: sceneNumber(event && event.movementY, state.lastY == null ? 0 : y - previousY),
      buttons: event && typeof event.buttons !== "undefined" ? sceneNumber(event.buttons, 0) : null,
      button: event && typeof event.button === "number" ? event.button : null,
      active: event ? event.type !== "pointerup" : false,
    };
    state.lastX = x;
    state.lastY = y;
    return sample;
  }

  function publishPointerSignals(sample, event) {
    queueInputSignal("$input.pointer.x", sample.x);
    queueInputSignal("$input.pointer.y", sample.y);
    queueInputSignal("$input.pointer.deltaX", sample.deltaX);
    queueInputSignal("$input.pointer.deltaY", sample.deltaY);
    if (sample.buttons != null) {
      queueInputSignal("$input.pointer.buttons", sample.buttons);
    }
    if (sample.button != null) {
      queueInputSignal("$input.pointer.button" + sample.button, sample.active);
    }
  }

  function resetPointerSignals() {
    queueInputSignal("$input.pointer.deltaX", 0);
    queueInputSignal("$input.pointer.deltaY", 0);
    queueInputSignal("$input.pointer.buttons", 0);
  }

  function publishGamepadSignals(pad, slot) {
    const prefix = "$input.gamepad" + Math.max(0, Math.floor(sceneNumber(slot, 0)));
    const axes = Array.isArray(pad.axes) ? pad.axes : [];
    queueInputSignal(prefix + ".connected", true);
    queueInputSignal(prefix + ".leftX", sceneNumber(axes[0], 0));
    queueInputSignal(prefix + ".leftY", sceneNumber(axes[1], 0));
    queueInputSignal(prefix + ".rightX", sceneNumber(axes[2], 0));
    queueInputSignal(prefix + ".rightY", sceneNumber(axes[3], 0));
    queueInputSignal(prefix + ".dpadUp", gamepadButtonPressed(pad, 12));
    queueInputSignal(prefix + ".dpadDown", gamepadButtonPressed(pad, 13));
    queueInputSignal(prefix + ".dpadLeft", gamepadButtonPressed(pad, 14));
    queueInputSignal(prefix + ".dpadRight", gamepadButtonPressed(pad, 15));
    queueInputSignal(prefix + ".buttonA", gamepadButtonPressed(pad, 0));
    queueInputSignal(prefix + ".buttonB", gamepadButtonPressed(pad, 1));
    queueInputSignal(prefix + ".buttonX", gamepadButtonPressed(pad, 2));
    queueInputSignal(prefix + ".buttonY", gamepadButtonPressed(pad, 3));
    queueInputSignal(prefix + ".buttonLB", gamepadButtonPressed(pad, 4));
    queueInputSignal(prefix + ".buttonRB", gamepadButtonPressed(pad, 5));
  }

  function gamepadButtonPressed(pad, index) {
    return Boolean(pad && pad.buttons && pad.buttons[index] && pad.buttons[index].pressed);
  }

  function sceneNumber(value, fallback) {
    const num = Number(value);
    return Number.isFinite(num) ? num : fallback;
  }

  // Snapshot an array without executing an index accessor, custom iterator,
  // or coercion hook. The caller validates each copied value separately.
  function gosxOwnDataArray(value) {
    const items = Object.getOwnPropertyDescriptors(value);
    const length = items.length.value;
    if (Reflect.ownKeys(items).length - 1 !== length) return null;
    return Array.from({ length }, function(_, index) {
      const item = items[index];
      return item && item.enumerable && "value" in item && item.value;
    });
  }

  // Publish the helpers the lazily fetched Scene3D chunk reads. That chunk
  // runs in its own IIFE and cannot reach this scope, so it used to carry a
  // second copy of this whole file: the Chromium Scene3D route downloaded
  // these 42_000 source bytes twice. This narrow API closes the gap.
  //
  // One copy behaves like the monolith, which has always had one copy: the
  // capability helpers are deterministic, engineFrame wraps
  // requestAnimationFrame, and the input queue keeps its state on
  // window.__gosx.input, which both bundles already shared.
  if (typeof window !== "undefined" && window.__gosx_runtime_api) {
    // loadManifest and gosxApplyCurrentScriptNonce reach the Scene3D chunk
    // only through `typeof X === "function"` guards. A guard turns a missing
    // symbol into silent wrong behaviour, not a crash, so publish both.
    Object.assign(window.__gosx_runtime_api, {
      browserCapabilitySupported,
      gosxApplyCurrentScriptNonce,
      loadManifest,
      cancelEngineFrame,
      engineCapabilityStatus,
      engineFrame,
      publishPointerSignals,
      queueInputSignal,
      runtimeCapabilityStatus,
      sceneNumber,
      ownDataArray: gosxOwnDataArray,
    });
  }
