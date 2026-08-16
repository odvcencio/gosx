// mount.ts — the GoSXScene3D engine factory.
// @ts-check
//
// This is the mount closure: it builds the canvas, drives the render loop,
// applies live scene updates, and disposes everything on teardown. It reads
// every other 20x file.
//
// The former single 20-scene-mount.js was 10_127 lines and 43 percent of the
// base Scene3D chunk. Backend selection (20a), the WebGL chunk loader (20b),
// the quality ladder (20c), the development overlays (20d), the viewport
// observers (20e), the DOM overlay (20f), the camera controls (20g) and the
// telemetry globals (20h) are now files. Four of them are gate candidates:
// the server already knows whether a scene needs them.

/**
 * @typedef {object} GoSXSceneEngineMountContext
 * @property {HTMLElement} mount
 * @property {object} props
 * @property {() => void} [dispose]
 */
  window.__gosx_register_engine_factory("GoSXScene3D", async function(ctx) {
    if (!ctx.mount || typeof document.createElement !== "function") {
      console.warn("[gosx] Scene3D requires a mount element");
      return {};
    }

    const props = ctx.props || {};
    const capability = sceneCapabilityProfile(props);
    const viewportBase = sceneViewportBase(props);
    const adaptiveQuality = createSceneAdaptiveQualityState(props, viewportBase, capability);
    // createSceneState decodes every compressed array as its first statement,
    // so the decompress chunk must land first. A scene with plain float arrays
    // and no generator descriptor fetches nothing and resolves at once.
    await settleSceneDecompressFeature(props);
    // The assetpipe IBL contract points at KTX2 half-float products. Its reader
    // currently shares the glTF sub-feature chunk, so settle that tiny upload
    // dependency before either renderer freezes its resource layouts.
    await settleSceneIBLFeature(props);
    const sceneState = createSceneState(props, capability);
    sceneState._modelStatusMount = ctx.mount;
    // Model fetches start immediately, but glTF texture URI selection waits for
    // this mount's actual renderer. The scope key is available now for honest
    // cache separation even while its context Promise is still pending.
    sceneState._modelTextureVariantScope = createSceneModelTextureVariantScope();
    // The manifest is immutable for the lifetime of an engine mount. Parse its
    // large inline shader payload once instead of once per rendered frame.
    const mountedWaterShaderSources = typeof window !== "undefined" &&
      window.__gosx_scene3d_water_shader_sources_by_id &&
      typeof window.__gosx_scene3d_water_shader_sources_by_id === "object"
      ? window.__gosx_scene3d_water_shader_sources_by_id
      : sceneMountedWaterShaderSources();
    if (ctx.mount && typeof window !== "undefined" && window.__gosx_scene3d_water_shader_sources_by_id) {
      ctx.mount.__gosxScene3DWaterShaderSources = window.__gosx_scene3d_water_shader_sources_by_id;
    }
    // Attach the terminal rejection handler at creation time. Most hydration
    // failures are returned as structured transactional outcomes, but this
    // guard also covers an unexpected producer/listener exception without
    // leaving a long gap in which the browser can report unhandledrejection.
    const sceneModelHydration = Promise.resolve(hydrateSceneStateModels(sceneState, props)).catch(function(error) {
      console.warn("[gosx] Scene3D model hydration failed; mounting without the affected model(s):",
        error && error.message ? error.message : error);
      gosxSceneEmit("warn", "model-hydration-failed", {
        generation: Math.max(0, Math.floor(sceneNumber(sceneState && sceneState._modelHydrationGeneration, 0))),
        committed: false,
        stale: false,
        stage: "unexpected",
        error: error && error.message ? String(error.message) : String(error),
      });
      return {
        generation: Math.max(0, Math.floor(sceneNumber(sceneState && sceneState._modelHydrationGeneration, 0))),
        outcome: "failed",
        committed: false,
        stale: false,
        failureStage: "unexpected",
      };
    });
    const runtimeScene = ctx.runtimeMode === "shared" && Boolean(ctx.programRef);
    const lifecycle = initialSceneLifecycleState();
    const motion = initialSceneMotionState(props);
    let sceneCSSAnimationUntil = 0;
    let lastModelAnimationTimeSeconds = null;
    // WASM motion seam (P2.4b). Opt-in via window.__gosx_motion_wasm; all state
    // stays inert when the flag is unset. wasmMotionState: 0=unloaded, 1=loaded,
    // -1=disabled (load failed or no program, never retried).
    let wasmMotionState = 0;
    let wasmMotionHandle = 0;
    let wasmMotionTargetRefs = null;
    let wasmMotionPropRefs = null;
    let wasmMotionF64 = null;
    let wasmMotionU8 = null;
    // C3: material-uniform motion seam. Mirrors wasmMotion* but loads
    // props.scene.materialMotionProgram and writes evaluated values into each
    // mesh's customUniforms so selena re-packs them every frame. Same opt-in
    // flag + state lifecycle (0=unloaded, 1=loaded, -1=disabled).
    let wasmMatMotionState = 0;
    let wasmMatMotionHandle = 0;
    let wasmMatMotionTargetRefs = null;
    let wasmMatMotionPropRefs = null;
    let wasmMatMotionF64 = null;
    let wasmMatMotionU8 = null;

    function sceneAnimationState() {
      if (motion.reducedMotion) {
        return { wants: false, reason: "reduced-motion" };
      }
      if (ctx.mount && ctx.mount.__gosxScene3DCSSDynamic && Date.now() < sceneCSSAnimationUntil) {
        return { wants: true, reason: "css-transition" };
      }
      if (sceneHasActiveTransitions(sceneState)) {
        return { wants: true, reason: "scene-transition" };
      }
      if (runtimeScene) {
        return { wants: true, reason: "runtime-program" };
      }
      if (sceneBool(props.autoRotate, false)) {
        return { wants: true, reason: "auto-rotate" };
      }
      if (Array.isArray(sceneState.computeParticles) && sceneState.computeParticles.length > 0) {
        return { wants: true, reason: "compute-particles" };
      }
      if (Array.isArray(sceneState.waterSystems) && sceneState.waterSystems.length > 0) {
        if (sceneWaterSystemsPaused(sceneState)) {
          return { wants: false, reason: "water-paused" };
        }
        return { wants: true, reason: "water-simulation" };
      }
      if (sceneHasActiveModelAnimations(sceneState)) {
        return { wants: true, reason: "model-animation" };
      }
      if (Array.isArray(sceneState.points) && sceneState.points.some(function(p) {
        return sceneNumber(p.spinX, 0) !== 0 || sceneNumber(p.spinY, 0) !== 0 || sceneNumber(p.spinZ, 0) !== 0;
      })) {
        return { wants: true, reason: "point-spin" };
      }
      if (sceneHasTimeDrivenMaterials(sceneState)) {
        return { wants: true, reason: "material-clock" };
      }
      if (sceneStateObjects(sceneState).some(sceneObjectAnimated)) {
        return { wants: true, reason: "object-animation" };
      }
      if (sceneStateLabels(sceneState).some(sceneLabelAnimated)) {
        return { wants: true, reason: "label-animation" };
      }
      if (sceneStateSprites(sceneState).some(sceneSpriteAnimated)) {
        return { wants: true, reason: "sprite-animation" };
      }
      if (sceneStateHTML(sceneState).some(sceneHTMLAnimated)) {
        return { wants: true, reason: "html-animation" };
      }
      return { wants: false, reason: "static" };
    }

    function sceneShouldAnimate() {
      return sceneAnimationState().wants;
    }

    // A material that declares a `time` uniform is animated by the per-frame
    // clock the renderer feeds (WGSL user.time / GLSL uniform float time /
    // selena `param time`), even when nothing else in the scene moves. The
    // content starfields are the canonical case: their layer spin was removed
    // and every twinkle and depth-wrap cycle lives in the shader clock, so
    // without this source the loop reported "static" after one frame and the
    // whole field froze. Detection is structural — a customUniforms map or a
    // shaderLayout uniform field named "time" — never a source-text scan.
    function sceneEntryDeclaresTimeUniform(entry) {
      if (!entry || typeof entry !== "object") {
        return false;
      }
      const uniforms = entry.customUniforms || entry.uniforms;
      if (uniforms && typeof uniforms === "object" && Object.prototype.hasOwnProperty.call(uniforms, "time")) {
        return true;
      }
      const layout = entry.shaderLayout;
      const block = layout && layout.uniformBlock;
      const fields = block && (block.fields || block.Fields);
      if (Array.isArray(fields)) {
        for (let i = 0; i < fields.length; i++) {
          const field = fields[i];
          if (field && (field.name === "time" || field.Name === "time")) {
            return true;
          }
        }
      }
      return false;
    }

    function sceneHasTimeDrivenMaterials(state) {
      // The normalized scene state strips authored-material fields (see
      // normalizeScenePointsEntry's whitelist), so the raw wire scene in
      // props is the source of truth for uniform declarations. The state
      // pools are still scanned as a fallback for callers that assembled
      // entries programmatically.
      const rawScene = props && props.scene && typeof props.scene === "object" ? props.scene : null;
      const pools = [
        rawScene && rawScene.points,
        rawScene && rawScene.materials,
        rawScene && rawScene.objects,
        rawScene && rawScene.models,
        state && state.points,
        state && state.materials,
        state ? sceneStateObjects(state) : null,
      ];
      for (let p = 0; p < pools.length; p++) {
        const pool = pools[p];
        if (!Array.isArray(pool)) {
          continue;
        }
        for (let i = 0; i < pool.length; i++) {
          if (sceneEntryDeclaresTimeUniform(pool[i])) {
            return true;
          }
        }
      }
      return false;
    }

    // Extract CSS var transition timing from materials/environment so the
    // planner can interpolate when resolved var values change. The planner
    // runs on the render bundle which no longer has the original materials
    // array, so we stash the timing on the mount element.
    ctx.mount.__gosxScene3DCSSVarTransition = sceneExtractCSSVarTransitionTiming(props);

    clearChildren(ctx.mount);
    const readyAttr = "data-gosx-scene3d-ready";
    const mountedAttr = "data-gosx-scene3d-mounted";
    // Opt-in first-content reveal: when the mount declares
    // data-gosx-scene3d-reveal-class, the runtime adds that class to the
    // document element (and stamps revealedAttr on the mount) after the
    // first frame that had drawable content — so pure CSS can fade out a
    // static boot placeholder without any app-authored JS. The class is
    // removed again on dispose so soft navigation starts clean.
    const revealedAttr = "data-gosx-scene3d-revealed";
    const revealClass = String(ctx.mount.getAttribute("data-gosx-scene3d-reveal-class") || "").trim();
    ctx.mount.setAttribute("aria-label", props.ariaLabel || props.label || "Interactive GoSX 3D scene");
    setAttrValue(ctx.mount, readyAttr, "false");
    setAttrValue(ctx.mount, revealedAttr, "false");
    setAttrValue(ctx.mount, "data-gosx-scene3d-controls", normalizeSceneControlsMode(props.controls));
    setAttrValue(ctx.mount, "data-gosx-scene3d-pick-signals", scenePickSignalNamespace(props));
    setAttrValue(ctx.mount, "data-gosx-scene3d-event-signals", sceneEventSignalNamespace(props));
    applySceneCapabilityState(ctx.mount, props, capability);
    if (!ctx.mount.style.position) {
      ctx.mount.style.position = "relative";
    }
    function createSceneMountCanvas() {
      const nextCanvas = document.createElement("canvas");
      nextCanvas.setAttribute("data-gosx-scene3d-canvas", "true");
      nextCanvas.setAttribute("role", "img");
      nextCanvas.setAttribute("aria-label", props.label || "Interactive GoSX 3D scene");
      nextCanvas.style.maxWidth = "100%";
      nextCanvas.style.borderRadius = "inherit";
      nextCanvas.width = viewportBase.baseWidth;
      nextCanvas.height = viewportBase.baseHeight;
      nextCanvas.setAttribute("width", String(viewportBase.baseWidth));
      nextCanvas.setAttribute("height", String(viewportBase.baseHeight));
      return nextCanvas;
    }

    let canvas = createSceneMountCanvas();
    ctx.mount.appendChild(canvas);
    scenePublishWaterShaderSourcesToMount(ctx.mount, canvas, mountedWaterShaderSources);
    setAttrValue(ctx.mount, "data-gosx-scene3d-water-frame-seq",
      Array.isArray(sceneState.waterSystems) && sceneState.waterSystems.length ? "0" : "");
    setAttrValue(ctx.mount, "data-gosx-scene3d-water-simulation-seq",
      Array.isArray(sceneState.waterSystems) && sceneState.waterSystems.length ? "0" : "");

    const labelLayer = document.createElement("div");
    labelLayer.setAttribute("data-gosx-scene3d-label-layer", "true");
    labelLayer.setAttribute("aria-hidden", "true");
    ctx.mount.appendChild(labelLayer);
    const statsOverlay = createSceneStatsOverlay(ctx.mount, sceneBool(props.stats, false));
    let inspectorOverlay = null;

    const sentinelLayer = document.createElement("div");
    sentinelLayer.setAttribute("data-gosx-scene-node-layer", "true");
    sentinelLayer.setAttribute("aria-hidden", "true");
    sentinelLayer.style.position = "absolute";
    sentinelLayer.style.inset = "0";
    sentinelLayer.style.width = "0";
    sentinelLayer.style.height = "0";
    sentinelLayer.style.overflow = "visible";
    sentinelLayer.style.pointerEvents = "none";

    const sceneNodeSentinels = new Map();
    ctx.mount.__gosxScene3DSentinels = sceneNodeSentinels;
    // Live sceneState handle for inspection (debug/test): lets callers read the
    // mutable object/material state — e.g. customUniforms written by the C3
    // material-motion seam — without going through the depth-clamped debug
    // snapshot.
    ctx.mount.__gosxScene3DState = sceneState;
    publishSceneWaterStateSnapshot(ctx.mount, sceneState);
    ctx.mount.__gosxScene3DCSSDynamic = false;
    ctx.mount.__gosxScene3DCSSRevision = 1;
    ctx.mount.__gosxScene3DCSSAnimationUntil = 0;
    applyScenePostFXState(ctx.mount, sceneState);

    await settlePreferredWebGPUBackend(props, capability);
    // WebGL now lives in its own chunk. Settle it too, so a WebGL page has the
    // renderer, the registry entry and the water runtime before the first
    // createSceneRenderer call. This resolves immediately on a WebGPU page and
    // on the monolith.
    await settlePreferredWebGLBackend(props, capability);
    // The compute chunk carries the particle systems and the GPU instanced
    // cull. Settle it too, so a particle scene draws its particles on the
    // first frame instead of skipping them. A scene with no particles and no
    // instanced mesh resolves immediately and fetches nothing.
    await settleSceneComputeFeature(sceneState);

    let viewport = applySceneViewport(ctx.mount, canvas, labelLayer, sceneViewportFromMount(ctx.mount, props, viewportBase, canvas, capability, adaptiveQuality), viewportBase);
    scenePrimeAdaptiveQuality(adaptiveQuality, viewport, ctx.mount, sceneState);

    const initialRenderer = createSceneRenderer(canvas, props, capability);
    if (!initialRenderer || !initialRenderer.renderer) {
      // Initial hydration was deliberately started before backend acquisition.
      // Fence it before returning an unsupported handle so a late asset can
      // only finish as stale and release its staged resources.
      invalidateSceneModelHydration(sceneState);
      settleSceneModelTextureVariantScope(
        sceneState._modelTextureVariantScope,
        sceneModelTextureVariantContextForRenderer(null)
      );
      publishSceneModelTextureVariantContext(ctx.mount, sceneState._modelTextureVariantScope);
      console.warn("[gosx] Scene3D could not acquire a renderer");
      const unsupportedReason = initialRenderer && initialRenderer.unsupportedReason
        ? initialRenderer.unsupportedReason
        : (sceneRequiresWebGL(props) ? "webgl-required" : "renderer-unavailable");
      applySceneRendererState(ctx.mount, { kind: "unsupported" }, unsupportedReason);
      publishSceneWaterRendererState(ctx.mount, sceneState, null, unsupportedReason);
      publishSceneWaterLifecycleState(ctx.mount, sceneState, lifecycle, false);
      setAttrValue(ctx.mount, readyAttr, "false");
      if (canvas.parentNode === ctx.mount) {
        ctx.mount.removeChild(canvas);
      }
      if (labelLayer.parentNode === ctx.mount) {
        ctx.mount.removeChild(labelLayer);
      }
      if (statsOverlay) {
        statsOverlay.dispose();
      }
      if (sentinelLayer.parentNode) {
        sentinelLayer.parentNode.removeChild(sentinelLayer);
      }
      delete ctx.mount.__gosxScene3DSentinels;
      delete ctx.mount.__gosxScene3DState;
      delete ctx.mount.__gosxScene3DTextureVariantContext;
      delete ctx.mount.__gosxScene3DCSSDynamic;
      delete ctx.mount.__gosxScene3DCSSRevision;
      delete ctx.mount.__gosxScene3DCSSAnimationUntil;
      showSceneRequiredRendererMessage(ctx.mount, props, unsupportedReason);
      return {
        dispose() {
          const unsupported = ctx.mount.querySelector
            ? ctx.mount.querySelector("[data-gosx-scene3d-unsupported]")
            : null;
          if (unsupported && unsupported.parentNode === ctx.mount) {
            ctx.mount.removeChild(unsupported);
          }
        },
      };
    }
    if (!sentinelLayer.parentNode) {
      canvas.appendChild(sentinelLayer);
    }
    let renderer = initialRenderer.renderer;
    settleSceneModelTextureVariantScope(
      sceneState._modelTextureVariantScope,
      sceneModelTextureVariantContextForRenderer(renderer)
    );
    publishSceneModelTextureVariantContext(ctx.mount, sceneState._modelTextureVariantScope);
    applySceneRendererState(ctx.mount, renderer, initialRenderer.fallbackReason || "", initialRenderer.degraded || []);
    publishSceneWaterRendererState(ctx.mount, sceneState, renderer, "");
    publishSceneWaterLifecycleState(ctx.mount, sceneState, lifecycle, false);
    let latestBundle = null;
    let lastSceneRenderNowMS = null;
    let rendererLifecycleActive = null;
    let rendererLifecyclePaused = null;

    function sceneFrameNowMS(value) {
      const now = value == null ? NaN : Number(value);
      if (Number.isFinite(now) && now >= 0) return now;
      return typeof performance !== "undefined" && performance.now ? performance.now() : Date.now();
    }

    function sceneWaterPausedForLifecycle() {
      return sceneWaterSystemsPaused(sceneState);
    }

    function notifySceneRendererLifecycle(reason, force, disposing) {
      const active = !disposing && sceneCanRender();
      const paused = sceneWaterPausedForLifecycle();
      if (!force && rendererLifecycleActive === active && rendererLifecyclePaused === paused) return;
      if (adaptiveQuality && active && !paused && (rendererLifecycleActive !== true || rendererLifecyclePaused === true)) {
        adaptiveQuality.resumePending = true;
        adaptiveQuality.lastRAFNowMS = null;
      }
      rendererLifecycleActive = active;
      rendererLifecyclePaused = paused;
      if (!active || paused || disposing) lastSceneRenderNowMS = null;
      if (renderer && typeof renderer.setLifecycle === "function") {
        renderer.setLifecycle({
          nowMS: sceneFrameNowMS(null),
          active: active,
          paused: paused,
          disposed: Boolean(disposing),
          reason: reason || "lifecycle",
        });
      }
    }

    function createSceneRenderFrameMeta(now) {
      const nowMS = sceneFrameNowMS(now);
      const active = sceneCanRender();
      const displayDeltaMS = active && lastSceneRenderNowMS != null && nowMS >= lastSceneRenderNowMS
        ? nowMS - lastSceneRenderNowMS
        : 0;
      lastSceneRenderNowMS = active ? nowMS : null;
      const qualityEnabled = Boolean(adaptiveQuality && adaptiveQuality.enabled);
      const qualityProfile = qualityEnabled && adaptiveQuality.activeProfile
        ? adaptiveQuality.activeProfile
        : null;
      return {
        nowMS: nowMS,
        displayDeltaMS: displayDeltaMS,
        active: active,
        qualityEnabled: qualityEnabled,
        qualityTier: adaptiveQuality && adaptiveQuality.tier ? adaptiveQuality.tier : "fixed",
        qualityRevision: Math.max(0, Math.floor(sceneNumber(adaptiveQuality && adaptiveQuality.qualityRevision, 0))),
        qualityProfile: qualityProfile,
        qualityRequestedTier: adaptiveQuality.requestedTier,
        qualityActiveTier: adaptiveQuality.activeTier,
        performanceMeasurement: adaptiveQuality.lastMeasurement,
      };
    }

    notifySceneRendererLifecycle("initial", true, false);
    const labelLayoutCache = new Map();
    const labelElements = new Map();
    const spriteElements = new Map();
    const htmlElements = new Map();
    const htmlTextureState = createSceneHTMLTextureState();
    htmlTextureState.requestRender = scheduleRender;
    const releaseTextureLoadListener = typeof onSceneTextureLoaded === "function"
      ? onSceneTextureLoaded(function(src, loaded) {
        settleSceneHTMLTextureUpload(htmlTextureState, src, loaded);
        scheduleRender(loaded === false ? "texture-failed" : "texture-loaded");
      })
      : null;
    let labelRefreshHandle = null;

    function syncSceneNodeSentinels(bundle) {
      const next = new Set();
      collectSceneNodeSentinelIDs(next, bundle && bundle.meshObjects);
      collectSceneNodeSentinelIDs(next, bundle && bundle.objects);
      collectSceneNodeSentinelIDs(next, bundle && bundle.points);
      collectSceneNodeSentinelIDs(next, bundle && bundle.instancedMeshes);
      collectSceneNodeSentinelIDs(next, bundle && bundle.computeParticles);
      collectSceneNodeSentinelIDs(next, bundle && bundle.lights);
      collectSceneNodeSentinelIDs(next, bundle && bundle.labels);
      collectSceneNodeSentinelIDs(next, bundle && bundle.sprites);
      collectSceneNodeSentinelIDs(next, bundle && bundle.html);
      next.forEach(function(id) {
        if (sceneNodeSentinels.has(id)) {
          return;
        }
        const sentinel = document.createElement("div");
        sentinel.setAttribute("data-gosx-scene-node", id);
        sentinel.setAttribute("aria-hidden", "true");
        sentinel.style.position = "absolute";
        sentinel.style.left = "0";
        sentinel.style.top = "0";
        sentinel.style.width = "1px";
        sentinel.style.height = "1px";
        sentinel.style.opacity = "0";
        sentinel.style.pointerEvents = "auto";
        sentinelLayer.appendChild(sentinel);
        sceneNodeSentinels.set(id, sentinel);
      });
      sceneNodeSentinels.forEach(function(sentinel, id) {
        if (next.has(id)) {
          return;
        }
        if (sentinel.parentNode === sentinelLayer) {
          sentinelLayer.removeChild(sentinel);
        }
        sceneNodeSentinels.delete(id);
      });
    }

    function collectSceneNodeSentinelIDs(target, entries) {
      if (!Array.isArray(entries)) {
        return;
      }
      for (let index = 0; index < entries.length; index += 1) {
        const entry = entries[index];
        const id = entry && entry.id;
        if (id != null && String(id).trim() !== "") {
          target.add(String(id));
        }
      }
    }

    const releaseTextLayoutListener = onTextLayoutInvalidated(function() {
      if (disposed || !latestBundle || !sceneCanRender()) {
        return;
      }
      if (labelRefreshHandle != null) {
        return;
      }
      labelRefreshHandle = engineFrame(function() {
        labelRefreshHandle = null;
        if (disposed || !latestBundle) {
          return;
        }
        renderSceneLabels(labelLayer, latestBundle, labelLayoutCache, labelElements, viewport.cssWidth, viewport.cssHeight);
        renderSceneSprites(labelLayer, latestBundle, spriteElements, viewport.cssWidth, viewport.cssHeight);
        renderSceneHTML(labelLayer, latestBundle, htmlElements, viewport.cssWidth, viewport.cssHeight, htmlTextureState);
      });
    });

    let frameHandle = null;
    let renderHandle = null;
    let initHandle = null;
    let initPending = true;
    let initReason = "";
    let readySent = false;
    let revealSent = false;
    let disposed = false;
    let lastRenderReason = "";
    let lastRenderLoopReason = "initializing";
    const SCENE_RENDER_WATCHDOG_INTERVAL_MS = 2000;
    const SCENE_RENDER_STALL_MS = 6500;
    const SCENE_RENDER_FALLBACK_STALL_MS = 12000;
    // SCENE_WEBGPU_FRAME_ERROR_STREAK_THRESHOLD: consecutive erroring WebGPU
    // frames (see 16a-scene-webgpu.js's reportWebGPUFrameError /
    // diagnostics().frameErrorStreak) before checkSceneWebGPUFrameErrorResilience
    // acts. A genuinely persistent failure (memory-tight browser, poisoned
    // post-FX target) blows past this on the very first SCENE_RENDER_WATCHDOG_INTERVAL_MS
    // poll (2s of frames at any real frame rate); the threshold exists to
    // ignore a single transient validation blip, not to add meaningful delay.
    const SCENE_WEBGPU_FRAME_ERROR_STREAK_THRESHOLD = 30;
    // SCENE_WEBGPU_FRAME_ERROR_RESTORE_STREAK_THRESHOLD: consecutive CLEAN
    // WebGPU frames (diagnostics().frameCleanStreak, the complement of
    // frameErrorStreak above) required before checkSceneWebGPUPostFXRestore
    // re-enables a demoted post-FX chain. Deliberately ten times the trip
    // threshold: the trip fires on the FIRST 30-frame bad streak, so a
    // restore threshold anywhere near that would let a borderline scene
    // flap demote/restore every couple of seconds. Requiring 10x the clean
    // frames the trip needed to fail makes oscillation structurally
    // impossible — reaching restore proves the renderer ran clean for ten
    // times longer than it ran broken.
    const SCENE_WEBGPU_FRAME_ERROR_RESTORE_STREAK_THRESHOLD = 300;
    // SCENE_WEBGPU_POSTFX_MAX_DEMOTIONS: after this many demote/restore
    // cycles in one mount's session, stop attempting automatic restore and
    // leave post-FX off for the rest of the session (see
    // checkSceneWebGPUPostFXRestore). A scene that keeps tripping the
    // ladder despite the 10x clean-streak margin above has a problem the
    // ladder cannot fix — a driver bug, or a resource that gets reallocated
    // the same broken way every time — and further cycles would only burn
    // GPU work repeating a fight it keeps losing. Each restore attempt
    // below this cap also requires a progressively longer clean streak
    // (postFXDemotionCount x the base threshold), so a scene has to prove
    // increasing stability to earn each additional restore.
    const SCENE_WEBGPU_POSTFX_MAX_DEMOTIONS = 3;
    let renderWatchdogTimer = null;
    let renderWatchdogLastSeq = -1;
    let renderWatchdogLastAt = 0;
    let renderWatchdogLastAdvanceAt = 0;
    let renderWatchdogRecoveries = 0;
    let renderWatchdogFallbacks = 0;
    let renderWatchdogActiveReason = "";
    // renderWatchdogDeviceLostInfo: { reason, message, adapterInfo } read off
    // the OLD renderer's diagnostics().deviceLostInfo (see 16a-scene-webgpu.js's
    // lastDeviceLostInfo) the moment recoverSceneWebGPURenderer decides to
    // act. destroy/driver-reset/OOM/internal-error/local-dispose used to all
    // collapse into the single reason string "webgpu-device-lost" with no
    // way to tell them apart in production — this is the detail that lets
    // gosxSceneEmit("render-watchdog-recovery", ...) and the DOM attribute
    // below actually distinguish them.
    let renderWatchdogDeviceLostInfo = null;
    // postFXDemotionCount: how many times THIS mount has demoted post-FX
    // this session (see checkSceneWebGPUFrameErrorResilience /
    // checkSceneWebGPUPostFXRestore below). Persists across a WebGPU
    // renderer swap within the same mount (recoverSceneWebGPURenderer
    // constructs a fresh renderer closure whose own postFXForceDisabled
    // starts false, but the SESSION's demotion history — and therefore how
    // hard it has to work to earn the next restore — should not reset with
    // it).
    let postFXDemotionCount = 0;
    let webgpuProbeReadyListener = null;

    // Do not voluntarily lose WebGL while the page is hidden/offscreen.
    // A canvas that has owned WebGL generally cannot switch to a 2D context,
    // so forced loss leaves no useful fallback and some browsers restore late.
    let idleContextTimer = null;
    let contextVoluntarilyLost = false;
    let voluntaryLoseContextExtension = null;

    function sceneRenderLoopSnapshot(reason) {
      const animation = sceneAnimationState();
      let active = frameHandle != null || renderHandle != null;
      let loopReason = reason || lastRenderLoopReason || animation.reason || "unknown";
      if (!sceneCanRender()) {
        active = false;
        loopReason = lifecycle.pageVisible ? "offscreen" : "page-hidden";
      } else if (renderHandle != null) {
        loopReason = lastRenderReason || loopReason || "scheduled-render";
      } else if (frameHandle != null) {
        loopReason = animation.reason || loopReason || "animation";
      } else if (!animation.wants) {
        loopReason = animation.reason || "static";
      }
      return {
        active,
        wantsAnimation: animation.wants,
        reason: loopReason,
        scheduled: renderHandle != null,
        animationFrame: frameHandle != null,
      };
    }

    function applySceneRenderLoopState(reason) {
      const state = sceneRenderLoopSnapshot(reason);
      lastRenderLoopReason = state.reason || "";
      setAttrValue(ctx.mount, "data-gosx-scene3d-render-loop", state.active ? "active" : "stopped");
      setAttrValue(ctx.mount, "data-gosx-scene3d-render-loop-reason", state.reason || "");
      setAttrValue(ctx.mount, "data-gosx-scene3d-render-loop-wants-animation", state.wantsAnimation ? "true" : "false");
      return state;
    }

    function readSceneWebGPUProgress() {
      const seq = Number(sceneDebugAttr(ctx.mount, "data-gosx-scene3d-webgpu-frame-seq"));
      const at = Number(sceneDebugAttr(ctx.mount, "data-gosx-scene3d-webgpu-frame-at"));
      return {
        seq: Number.isFinite(seq) ? seq : 0,
        at: Number.isFinite(at) ? at : 0,
      };
    }

    function publishSceneRenderWatchdogState(reason, stalledFor) {
      setAttrValue(ctx.mount, "data-gosx-scene3d-render-watchdog", reason ? "recovering" : "ok");
      setAttrValue(ctx.mount, "data-gosx-scene3d-render-watchdog-reason", reason || "");
      setAttrValue(ctx.mount, "data-gosx-scene3d-render-watchdog-stalled-ms", stalledFor > 0 ? Math.round(stalledFor) : "");
      setAttrValue(ctx.mount, "data-gosx-scene3d-render-watchdog-recoveries", renderWatchdogRecoveries || "");
      setAttrValue(ctx.mount, "data-gosx-scene3d-render-watchdog-fallbacks", renderWatchdogFallbacks || "");
      // Published only alongside an active reason (mirrors -reason above) so
      // a screenshot harness can read WHY a webgpu-device-lost recovery
      // happened without needing telemetry — see renderWatchdogDeviceLostInfo.
      setAttrValue(ctx.mount, "data-gosx-scene3d-webgpu-device-lost-reason",
        reason && renderWatchdogDeviceLostInfo ? renderWatchdogDeviceLostInfo.reason || "" : "");
    }

    function rendererReportsWebGPUFailure(diagnostics) {
      if (!diagnostics) {
        return "";
      }
      if (diagnostics.deviceLost) {
        return "webgpu-device-lost";
      }
      if (diagnostics.initFailed) {
        return "webgpu-init-failed";
      }
      if (diagnostics.ready === false) {
        return "webgpu-not-ready";
      }
      return "";
    }

    function recoverSceneWebGPURenderer(reason, stalledFor, forceFallback) {
      renderWatchdogRecoveries += 1;
      renderWatchdogActiveReason = reason || "webgpu-stalled";
      // Read the OLD renderer's loss detail before it gets swapped away
      // below (see 16a-scene-webgpu.js's lastDeviceLostInfo / diagnostics()
      // .deviceLostInfo, and adapterInfo, which diagnostics() already
      // carries through from the shared probe snapshot).
      const priorDiagnostics = renderer && typeof renderer.diagnostics === "function" ? renderer.diagnostics() : null;
      const priorLostInfo = priorDiagnostics && priorDiagnostics.deviceLostInfo;
      renderWatchdogDeviceLostInfo = priorLostInfo ? {
        reason: priorLostInfo.reason || "",
        message: priorLostInfo.message || "",
        adapterInfo: (priorDiagnostics && priorDiagnostics.adapterInfo) || null,
      } : null;
      publishSceneRenderWatchdogState(renderWatchdogActiveReason, stalledFor || 0);
      gosxSceneEmit("warn", "render-watchdog-recovery", {
        rendererKind: renderer && renderer.kind ? renderer.kind : "",
        reason: renderWatchdogActiveReason,
        stalledForMS: Math.round(stalledFor || 0),
        recoveryCount: renderWatchdogRecoveries,
        forceFallback: !!forceFallback,
        deviceLostReason: renderWatchdogDeviceLostInfo ? renderWatchdogDeviceLostInfo.reason : "",
        deviceLostMessage: renderWatchdogDeviceLostInfo ? renderWatchdogDeviceLostInfo.message : "",
        adapterInfo: renderWatchdogDeviceLostInfo ? renderWatchdogDeviceLostInfo.adapterInfo : null,
      });
      cancelFrame();
      cancelScheduledRender();
      viewportDirty = true;
      if (!forceFallback && renderer && renderer.kind === "webgpu") {
        const recreated = createSceneRenderer(canvas, props, capability);
        const nextRenderer = recreated && recreated.renderer;
        if (nextRenderer && nextRenderer.kind === "webgpu" && nextRenderer !== renderer) {
          if (swapRenderer(nextRenderer, reason || "webgpu-render-stall")) {
            renderLatestSceneBundle(reason || "webgpu-render-stall");
            scheduleRenderWithViewport(reason || "webgpu-render-stall");
            return true;
          }
        } else if (nextRenderer && nextRenderer !== renderer && typeof nextRenderer.dispose === "function") {
          nextRenderer.dispose();
        }
	      }
	      renderWatchdogFallbacks += 1;
	      publishSceneRenderWatchdogState(renderWatchdogActiveReason, stalledFor || 0);
	      if (fallbackSceneRenderer(reason || "webgpu-render-stall")) {
        renderLatestSceneBundle(reason || "webgpu-render-stall");
        scheduleRenderWithViewport(reason || "webgpu-render-stall");
        return true;
      }
      scheduleRenderWithViewport(reason || "webgpu-render-stall");
      return false;
    }

    // recoverSceneWebGPUFromWebGLFallback climbs back onto WebGPU after a
    // runtime WebGPU failure (device loss / persistent frame errors) forced
    // a fallback to WebGL. Without this, a later gosx:scene3d:webgpu-probe-ready
    // (the probe re-acquired a working device) was silently ignored, because
    // handleSceneWebGPUProbeReady's own guard required the CURRENT renderer
    // to already be "webgpu" — a session that fell back once stayed on
    // WebGL for the rest of the page even after the GPU came back. Measured
    // on the live site: the probe recovered a working device at ~t=12.7s
    // and the mount was still on WebGL at t=30s.
    //
    // Only re-attempts when the fallback that put us on WebGL was itself a
    // WebGPU failure (sceneFallbackRequiresReplacementCanvas's reason set —
    // "webgpu-device-lost" / "webgpu-persistent-frame-error"), not an
    // intentional preference (forceWebGL, environment-constrained, etc.).
    //
    // The current canvas is already tainted to WebGL — the fallback that
    // put us here replaced it with a fresh canvas before configuring a
    // WebGL context on it (same sceneFallbackRequiresReplacementCanvas
    // reason set), and a canvas that has had getContext("webgl2") called on
    // it can never return a working getContext("webgpu") afterward. So this
    // needs its OWN fresh, as-yet-untainted trial canvas.
    // createSceneWebGPURendererOrFallback only touches that trial canvas
    // (calls getContext("webgpu") on it) once sceneWebGPUAvailable() is
    // already true, so a failed attempt here never taints the LIVE canvas
    // still on screen — the trial canvas is simply discarded, exactly like
    // createFallbackSceneWebGLRenderer already does for its own trial
    // canvas on the WebGPU-losing side of this same recovery machinery.
    function recoverSceneWebGPUFromWebGLFallback(reason) {
      if (disposed || !renderer || renderer.kind !== "webgl") {
        return false;
      }
      // Read the PERSISTENT data-gosx-scene3d-renderer-fallback attribute
      // (set by applySceneRendererState inside swapRenderer), not the
      // sceneRendererLastSwapReason variable that also records this --
      // maybeEmitRenderEmpty reads-and-clears that variable on the very
      // next rendered frame after ANY swap (it needs a one-shot signal for
      // its own render-empty check), so by the time a LATER probe-ready
      // event fires, that variable already reads "" even though this is
      // still, right now, a WebGPU-failure fallback.
      if (!sceneFallbackRequiresReplacementCanvas(sceneDebugAttr(ctx.mount, "data-gosx-scene3d-renderer-fallback"))) {
        return false;
      }
      const trialCanvas = prepareSceneReplacementCanvas();
      const nextRenderer = createSceneWebGPURendererOrFallback(trialCanvas, sceneWebGPUOptions(props, capability));
      if (!nextRenderer || nextRenderer.kind !== "webgpu") {
        if (nextRenderer && typeof nextRenderer.dispose === "function") {
          nextRenderer.dispose();
        }
        return false;
      }
      const swapReason = reason || "webgpu-probe-recovered";
      commitSceneCanvasReplacement(trialCanvas, swapReason);
      if (!swapRenderer(nextRenderer, swapReason)) {
        return false;
      }
      renderWatchdogRecoveries += 1;
      gosxSceneEmit("warn", "render-watchdog-recovery", {
        rendererKind: "webgl",
        reason: swapReason,
        stalledForMS: 0,
        recoveryCount: renderWatchdogRecoveries,
        forceFallback: false,
      });
      viewportDirty = true;
      renderLatestSceneBundle(swapReason);
      scheduleRenderWithViewport(swapReason);
      return true;
    }

    function handleSceneWebGPUProbeReady() {
      if (disposed || !renderer) {
        return;
      }
      if (renderer.kind === "webgl") {
        recoverSceneWebGPUFromWebGLFallback("webgpu-probe-recovered");
        return;
      }
      if (renderer.kind !== "webgpu") {
        return;
      }
      const diagnostics = typeof renderer.diagnostics === "function" ? renderer.diagnostics() : null;
      const reason = rendererReportsWebGPUFailure(diagnostics);
      if (!reason) {
        return;
      }
      recoverSceneWebGPURenderer("webgpu-probe-recovered", 0, false);
    }

    if (typeof window !== "undefined" && typeof window.addEventListener === "function") {
      webgpuProbeReadyListener = handleSceneWebGPUProbeReady;
      window.addEventListener("gosx:scene3d:webgpu-probe-ready", webgpuProbeReadyListener);
    }

    // escalateSceneWebGPUToFallback swaps to the WebGL backend at runtime
    // via the existing fallbackSceneRenderer machinery (same path
    // webgl-context-lost and webgpu-device-lost already use) — a full
    // engine re-mount on the same canvas/mount, no page reload required.
    // Shared by both callers below: a demoted (post-FX already torn down)
    // renderer that is STILL erroring persistently, either read from a
    // fresh diagnostics snapshot or discovered synchronously when
    // disablePostProcessing() itself declines to act.
    function escalateSceneWebGPUToFallback(streak, diagnostics) {
      gosxSceneEmit("warn", "webgpu-persistent-frame-error-fallback", {
        frameErrorStreak: streak,
        lastError: (diagnostics && diagnostics.lastError) || "",
      });
      if (fallbackSceneRenderer("webgpu-persistent-frame-error")) {
        renderLatestSceneBundle("webgpu-persistent-frame-error");
        scheduleRenderWithViewport("webgpu-persistent-frame-error");
      }
      return true;
    }

    // checkSceneWebGPUPostFXRestore is the RESTORE step of the resilience
    // ladder (see checkSceneWebGPUFrameErrorResilience below) — the way
    // back that disablePostProcessing never had before this change. Called
    // only while diagnostics.postFXDisabled is true and the error streak
    // has NOT crossed the trip threshold this poll (the caller handles that
    // escalation case itself). Re-enables post-FX once the renderer has
    // produced enough consecutive CLEAN frames in a row — see
    // SCENE_WEBGPU_FRAME_ERROR_RESTORE_STREAK_THRESHOLD and
    // SCENE_WEBGPU_POSTFX_MAX_DEMOTIONS above for why the threshold scales
    // with postFXDemotionCount and why restore attempts are capped.
    function checkSceneWebGPUPostFXRestore(diagnostics) {
      if (postFXDemotionCount <= 0 || postFXDemotionCount > SCENE_WEBGPU_POSTFX_MAX_DEMOTIONS) {
        return false;
      }
      const cleanStreak = Math.max(0, Math.floor(sceneNumber(diagnostics.frameCleanStreak, 0)));
      const restoreThreshold = SCENE_WEBGPU_FRAME_ERROR_RESTORE_STREAK_THRESHOLD * postFXDemotionCount;
      if (cleanStreak < restoreThreshold) {
        return false;
      }
      if (typeof renderer.enablePostProcessing !== "function" || !renderer.enablePostProcessing()) {
        return false;
      }
      setAttrValue(ctx.mount, "data-gosx-scene3d-webgpu-postfx-demoted", "");
      gosxSceneEmit("info", "webgpu-postfx-restored", {
        cleanStreak: cleanStreak,
        restoreThreshold: restoreThreshold,
        demotionCount: postFXDemotionCount,
      });
      viewportDirty = true;
      renderLatestSceneBundle("webgpu-postfx-restored");
      scheduleRenderWithViewport("webgpu-postfx-restored");
      return true;
    }

    // checkSceneWebGPUFrameErrorResilience reacts to PERSISTENT per-frame
    // WebGPU validation/OOM errors — the failure mode a stalled-frame-seq
    // watchdog (checkSceneRenderWatchdog below) cannot see, because the
    // render loop keeps completing and advancing frame-seq every frame even
    // though every frame is invalid (a memory-tight browser session that
    // failed to allocate a post-FX/HDR target once and then reused the
    // poisoned resource forever — "Buffer with '' label is invalid" on
    // every frame, black canvas, no recovery).
    //
    // Three-step ladder, cheapest/least-disruptive first:
    //   1. DEMOTE: tear down the post-FX chain (disablePostProcessing) and
    //      retry raw rendering — the base scene without post-FX was fine for
    //      the whole session up to the point post-FX's allocation failed, so
    //      this alone recovers the common case.
    //   2. RESTORE: once a demoted scene runs clean for long enough (see
    //      checkSceneWebGPUPostFXRestore above), re-enable post-FX. This is
    //      the additive "way back" — DEMOTE alone left a scene permanently
    //      without post effects for the rest of the session.
    //   3. FALLBACK: if raw rendering (post-FX already torn down) STILL
    //      errors persistently instead of settling into a clean streak,
    //      escalate to the WebGL backend (escalateSceneWebGPUToFallback).
    // Returns true when it took an action this poll (caller should skip the
    // rest of the stall-detection logic for this cycle).
    function checkSceneWebGPUFrameErrorResilience(diagnostics) {
      if (!diagnostics || !renderer || renderer.kind !== "webgpu") {
        return false;
      }
      const streak = Math.max(0, Math.floor(sceneNumber(diagnostics.frameErrorStreak, 0)));
      if (diagnostics.postFXDisabled) {
        if (streak >= SCENE_WEBGPU_FRAME_ERROR_STREAK_THRESHOLD) {
          // Already demoted (post-FX torn down) and STILL erroring
          // persistently — post-FX was not the (sole) problem. Escalate.
          return escalateSceneWebGPUToFallback(streak, diagnostics);
        }
        return checkSceneWebGPUPostFXRestore(diagnostics);
      }
      if (streak < SCENE_WEBGPU_FRAME_ERROR_STREAK_THRESHOLD) {
        return false;
      }
      if (typeof renderer.disablePostProcessing === "function" && renderer.disablePostProcessing()) {
        postFXDemotionCount += 1;
        setAttrValue(ctx.mount, "data-gosx-scene3d-webgpu-postfx-demoted", "true");
        gosxSceneEmit("warn", "webgpu-postfx-demoted", {
          frameErrorStreak: streak,
          lastError: diagnostics.lastError || "",
          demotionCount: postFXDemotionCount,
        });
        viewportDirty = true;
        renderLatestSceneBundle("webgpu-postfx-demoted");
        scheduleRenderWithViewport("webgpu-postfx-demoted");
        return true;
      }
      // disablePostProcessing() declined even though this diagnostics
      // snapshot read postFXDisabled === false — the renderer's state
      // changed between the read and the call. Treat it the same as the
      // already-demoted-and-still-erroring case above.
      return escalateSceneWebGPUToFallback(streak, diagnostics);
    }

    function checkSceneRenderWatchdog() {
      if (disposed || !ctx.mount || !renderer || renderer.kind !== "webgpu") {
        return;
      }
      const animation = sceneAnimationState();
      if (!animation.wants || !sceneCanRender()) {
        renderWatchdogLastSeq = -1;
        renderWatchdogLastAt = 0;
        renderWatchdogLastAdvanceAt = 0;
        publishSceneRenderWatchdogState("", 0);
        return;
      }
      const now = typeof performance !== "undefined" && typeof performance.now === "function" ? performance.now() : Date.now();
      const progress = readSceneWebGPUProgress();
      const diagnostics = typeof renderer.diagnostics === "function" ? renderer.diagnostics() : null;
      const failureReason = rendererReportsWebGPUFailure(diagnostics);
      if (failureReason) {
        recoverSceneWebGPURenderer(failureReason, 0, true);
        return;
      }
      if (checkSceneWebGPUFrameErrorResilience(diagnostics)) {
        return;
      }
      if (progress.seq > renderWatchdogLastSeq || progress.at > renderWatchdogLastAt) {
        renderWatchdogLastSeq = progress.seq;
        renderWatchdogLastAt = progress.at;
        renderWatchdogLastAdvanceAt = now;
        renderWatchdogActiveReason = "";
        renderWatchdogDeviceLostInfo = null;
        publishSceneRenderWatchdogState("", 0);
        return;
      }
      if (renderWatchdogLastAdvanceAt <= 0) {
        renderWatchdogLastAdvanceAt = now;
        renderWatchdogLastSeq = progress.seq;
        renderWatchdogLastAt = progress.at;
        return;
      }
      const stalledFor = now - renderWatchdogLastAdvanceAt;
      if (stalledFor < SCENE_RENDER_STALL_MS) {
        return;
      }
      const reason = progress.seq > 0 || progress.at > 0 ? "webgpu-render-stall" : "webgpu-render-not-started";
      const forceFallback = stalledFor >= SCENE_RENDER_FALLBACK_STALL_MS;
      if (recoverSceneWebGPURenderer(reason, stalledFor, forceFallback)) {
        renderWatchdogLastAdvanceAt = now;
      }
    }

    function startSceneRenderWatchdog() {
      if (renderWatchdogTimer != null || typeof setInterval !== "function") {
        return;
      }
      const now = typeof performance !== "undefined" && typeof performance.now === "function" ? performance.now() : Date.now();
      const progress = readSceneWebGPUProgress();
      renderWatchdogLastSeq = progress.seq;
      renderWatchdogLastAt = progress.at;
      renderWatchdogLastAdvanceAt = now;
      publishSceneRenderWatchdogState("", 0);
      renderWatchdogTimer = setInterval(checkSceneRenderWatchdog, SCENE_RENDER_WATCHDOG_INTERVAL_MS);
    }

    function stopSceneRenderWatchdog() {
      if (renderWatchdogTimer != null) {
        clearInterval(renderWatchdogTimer);
        renderWatchdogTimer = null;
      }
    }

    function scheduleIdleContextRelease() {
      clearIdleContextRelease();
      if (disposed || contextVoluntarilyLost) return;
    }

    function clearIdleContextRelease() {
      if (idleContextTimer != null) {
        clearTimeout(idleContextTimer);
        idleContextTimer = null;
      }
    }

    // Watchdog for voluntary-restore: Chrome does NOT always fire
    // `webglcontextrestored` after a voluntary `ext.restoreContext()` call,
    // particularly when the tab was foregrounded but the scene was briefly
    // off-viewport when the idle timer fired. The restore event never lands,
    // the lost stub stays installed, and the canvas is permanently black
    // until navigation. This watchdog force-invokes the restore path if the
    // browser event hasn't fired within WEBGL_VOLUNTARY_RESTORE_WATCHDOG_MS.
    const WEBGL_VOLUNTARY_RESTORE_WATCHDOG_MS = 2000;
    let voluntaryRestoreWatchdogTimer = null;
    let voluntaryRestorePending = false;

    function clearVoluntaryRestoreWatchdog() {
      if (voluntaryRestoreWatchdogTimer != null) {
        clearTimeout(voluntaryRestoreWatchdogTimer);
        voluntaryRestoreWatchdogTimer = null;
      }
      voluntaryRestorePending = false;
    }

    function restoreVoluntarilyLostContext() {
      if (!contextVoluntarilyLost) return;
      contextVoluntarilyLost = false;
      voluntaryRestorePending = true;
      let requested = false;
      let restoreExt = voluntaryLoseContextExtension;
      voluntaryLoseContextExtension = null;
      try {
        if (!restoreExt) {
          const gl = canvas.getContext("webgl2") || canvas.getContext("webgl");
          restoreExt = gl && typeof gl.getExtension === "function"
            ? gl.getExtension("WEBGL_lose_context")
            : null;
        }
        if (restoreExt && typeof restoreExt.restoreContext === "function") {
          restoreExt.restoreContext();
          requested = true;
        }
      } catch (_e) { /* let the browser handle it */ }
      gosxSceneEmit("info", "webgl-voluntary-restore-requested", {
        requested: requested,
      });
      if (voluntaryRestoreWatchdogTimer != null) {
        clearTimeout(voluntaryRestoreWatchdogTimer);
      }
      voluntaryRestoreWatchdogTimer = setTimeout(function () {
        voluntaryRestoreWatchdogTimer = null;
        if (!voluntaryRestorePending || disposed) {
          return;
        }
        voluntaryRestorePending = false;
        gosxSceneEmit("warn", "webgl-voluntary-restore-watchdog", {
          rendererKind: renderer && renderer.kind ? renderer.kind : "",
          forcing: true,
        });
        if (!renderer || renderer.kind === "webgl") {
          // Either the event already fired and wired things up, or we lost
          // the mount — either way, nothing to force.
          return;
        }
        // Event didn't land. Force the restore path directly. Mirrors
        // onWebGLContextRestored without touching contextVoluntarilyLost
        // (already cleared above).
        const swapped = restoreSceneWebGLRenderer("webgl-voluntary-restore-forced");
        if (swapped) {
          viewportDirty = true;
          scheduleRender("webgl-voluntary-restore-forced");
        }
        gosxSceneEmit(swapped ? "info" : "error", "webgl-voluntary-restore-forced", {
          swapped: swapped,
        });
      }, WEBGL_VOLUNTARY_RESTORE_WATCHDOG_MS);
    }

    // Viewport-dirty flag: when false, renderFrame skips the per-frame
    // sceneViewportFromMount + applySceneViewport calls and reuses the
    // cached `viewport` object. Both helpers are layout-flushing — they
    // call mount/canvas.getBoundingClientRect(), forcing the browser to
    // recompute layout synchronously. Doing that every frame burns 1-3 ms
    // on a busy page where no DOM has actually changed. The dirty flag
    // is set to true on:
    //   - initial mount (first frame must measure)
    //   - ResizeObserver fire (canvas or mount size changed)
    //   - window resize fallback
    //   - environment / capability change (DPR update)
    //   - lifecycle / motion observer refresh (safer to re-measure)
    //   - visibility transitions
    // Scroll events do NOT mark the viewport dirty — scrolling doesn't
    // change a fixed-positioned canvas's rect, and non-fixed scenes
    // also don't care about scroll-time position unless the consumer
    // explicitly schedules a refresh.
    let viewportDirty = true;
    let lastAnimationFrameAt = 0;

    function sceneAnimationFrameIntervalMS() {
      var interval = sceneNumber(props && props.frameIntervalMS, 0);
      if (!(interval > 0)) {
        var fps = sceneNumber(props && props.maxFrameRate, 0);
        if (!(fps > 0)) {
          fps = sceneNumber(props && props.maxFPS, 0);
        }
        if (fps > 0) {
          interval = 1000 / Math.min(240, Math.max(1, fps));
        }
      }
      return interval > 0 ? Math.max(1, interval) : 0;
    }

    // Guarded animation-chain scheduler. Eager refreshes may still render
    // promptly; the continuous chain stays single-owner and honors maxFrameRate.
    function scheduleNextAnimationFrame() {
      if (disposed) return;
      if (frameHandle != null) return;
      const animation = sceneAnimationState();
      if (!animation.wants || !sceneCanRender()) {
        applySceneRenderLoopState(animation.reason);
        return;
      }
      frameHandle = engineFrame(function(now) {
        frameHandle = null;
        var interval = sceneAnimationFrameIntervalMS();
        if (interval > 0 && lastAnimationFrameAt > 0 && typeof now === "number" && now - lastAnimationFrameAt < interval - 0.75) {
          scheduleNextAnimationFrame();
          return;
        }
        if (typeof now === "number") {
          lastAnimationFrameAt = now;
        }
        renderFrame(now);
      });
      applySceneRenderLoopState(animation.reason);
    }

	    let sceneRendererRecentlySwapped = false;
	    let sceneRendererLastSwapReason = "";
	    let sceneControlHandle = null;
	    let dragHandle = null;
	    let gizmoDragHandle = null;
	    let pickHandle = null;
	    let latestScenePickDetail = null;

	    function swapRenderer(nextRenderer, fallbackReason) {
	      if (!nextRenderer) {
	        return false;
	      }
      const previous = renderer;
      renderer = nextRenderer;
      const variantScopeChange = replaceSceneModelTextureVariantScope(sceneState, renderer);
      publishSceneModelTextureVariantContext(ctx.mount, variantScopeChange.scope);
      applySceneRendererState(ctx.mount, renderer, fallbackReason);
      publishSceneWaterRendererState(ctx.mount, sceneState, renderer, "");
      notifySceneRendererLifecycle(fallbackReason || "renderer-swap", true, false);
      renderWatchdogLastSeq = -1;
      renderWatchdogLastAt = 0;
      renderWatchdogLastAdvanceAt = typeof performance !== "undefined" && typeof performance.now === "function" ? performance.now() : Date.now();
      if (previous && previous !== renderer && typeof previous.dispose === "function") {
        previous.dispose();
      }
      sceneRendererRecentlySwapped = true;
      sceneRendererLastSwapReason = fallbackReason || "";
      gosxSceneEmit("info", "renderer-swap", {
        from: previous && previous.kind ? previous.kind : "",
        to: nextRenderer.kind || "",
        reason: fallbackReason || "",
		      });
      if (variantScopeChange.changed && sceneHydrationModels(sceneState, null).length > 0) {
        const variantHydration = sceneRehydrateModelsAfterCommand(sceneState);
        if (variantHydration && typeof variantHydration.then === "function") {
          variantHydration.then(function(result) {
            if (!disposed && result && result.committed) {
              scheduleRender("renderer-texture-variants");
            }
          }, function(error) {
            gosxSceneEmit("warn", "model-variant-rehydrate-failed", {
              backend: variantScopeChange.scope && variantScopeChange.scope.context
                ? variantScopeChange.scope.context.backend
                : "",
              error: error && error.message ? String(error.message) : String(error || ""),
            });
          });
        }
      }
		      return true;
	    }

	    function detachSceneCanvasContextListeners(target) {
	      if (!target || typeof target.removeEventListener !== "function") {
	        return;
	      }
	      target.removeEventListener("webglcontextlost", onWebGLContextLost);
	      target.removeEventListener("webglcontextrestored", onWebGLContextRestored);
	      target.removeEventListener("gosx:scene3d:resource-ready", onSceneResourceReady);
	    }

	    function attachSceneCanvasContextListeners(target) {
	      if (!target || typeof target.addEventListener !== "function") {
	        return;
	      }
	      target.addEventListener("webglcontextlost", onWebGLContextLost);
	      target.addEventListener("webglcontextrestored", onWebGLContextRestored);
	      target.addEventListener("gosx:scene3d:resource-ready", onSceneResourceReady);
	    }

	    function onSceneResourceReady() {
	      if (!disposed) {
	        scheduleRender("resource-ready");
	      }
	    }

	    function prepareSceneReplacementCanvas() {
	      const nextCanvas = createSceneMountCanvas();
	      nextCanvas.width = canvas && canvas.width ? canvas.width : viewportBase.baseWidth;
	      nextCanvas.height = canvas && canvas.height ? canvas.height : viewportBase.baseHeight;
	      nextCanvas.setAttribute("width", String(nextCanvas.width));
	      nextCanvas.setAttribute("height", String(nextCanvas.height));
	      if (canvas && canvas.style) {
	        nextCanvas.style.width = canvas.style.width || "";
	        nextCanvas.style.height = canvas.style.height || "";
	        nextCanvas.style.maxWidth = canvas.style.maxWidth || nextCanvas.style.maxWidth;
	        nextCanvas.style.borderRadius = canvas.style.borderRadius || nextCanvas.style.borderRadius;
	      }
	      return nextCanvas;
	    }

	    function commitSceneCanvasReplacement(nextCanvas, reason) {
	      if (!nextCanvas || nextCanvas === canvas || !ctx.mount) {
	        return false;
	      }
	      const previousCanvas = canvas;
	      detachSceneCanvasContextListeners(previousCanvas);
	      if (sentinelLayer && sentinelLayer.parentNode === previousCanvas) {
	        nextCanvas.appendChild(sentinelLayer);
	      }
	      if (previousCanvas && previousCanvas.parentNode === ctx.mount) {
	        ctx.mount.insertBefore(nextCanvas, previousCanvas);
	        ctx.mount.removeChild(previousCanvas);
	      } else if (labelLayer && labelLayer.parentNode === ctx.mount) {
	        ctx.mount.insertBefore(nextCanvas, labelLayer);
	      } else {
	        ctx.mount.appendChild(nextCanvas);
	      }
	      canvas = nextCanvas;
	      attachSceneCanvasContextListeners(canvas);
	      viewportDirty = true;
	      if (domRegionTracker) {
	        domRegionTracker.schedule();
	      }
	      reinstallSceneCanvasInteractionHandles(reason || "canvas-replaced");
	      gosxSceneEmit("info", "renderer-canvas-replaced", {
	        reason: reason || "",
	      });
	      return true;
	    }

	    function sceneFallbackRequiresReplacementCanvas(reason) {
	      // A canvas that already had getContext("webgpu") called on it is
	      // permanently tainted to that context type — getContext("webgl2")
	      // on the SAME canvas returns null regardless of whether the WebGPU
	      // device itself is still alive. webgpu-persistent-frame-error's
	      // canvas was claimed by the (still-alive-but-broken) WebGPU
	      // renderer exactly like webgpu-device-lost's was, so it needs the
	      // same fresh-canvas treatment.
	      return reason === "webgpu-device-lost" || reason === "webgpu-persistent-frame-error";
	    }

    // WebGL fallback chunk state for this mount: "idle", "loading" or
    // "settled". The ladder defers to the network at most once. After the
    // chunk settles, whether or not it published a renderer, the ladder runs
    // straight through to canvas2d.
    let sceneWebGLFallbackChunkState = sceneWebGLRendererFactory() ? "settled" : "idle";

    // deferSceneWebGLFallback keeps the WebGPU -> device-loss -> WebGL ->
    // canvas2d ladder intact when WebGL sits in a lazily fetched chunk.
    //
    // A Chromium page that loses its WebGPU device reaches the WebGL rung with
    // the chunk absent. Without this hop the ladder would skip WebGL and land
    // on canvas2d, or on nothing at all, which trades graceful degradation for
    // bytes. Instead: fetch the chunk, then re-enter fallbackSceneRenderer.
    // The second pass finds the factory and swaps the renderer for real.
    //
    // Returns true when it started the fetch, so the caller reports that the
    // ladder took an action. The mount draws nothing for one round trip, which
    // is the same state a dead GPU device already left it in.
    function deferSceneWebGLFallback(reason) {
      if (sceneWebGLFallbackChunkState !== "idle") {
        return false;
      }
      sceneWebGLFallbackChunkState = "loading";
      gosxSceneEmit("info", "webgl-fallback-chunk-fetch", { reason: reason || "" });
      ensureWebGLFeatureLoaded().then(function() {
        sceneWebGLFallbackChunkState = "settled";
        if (disposed) {
          return;
        }
        if (fallbackSceneRenderer(reason)) {
          renderLatestSceneBundle(reason);
          scheduleRenderWithViewport(reason);
          return;
        }
        gosxSceneEmit("warn", "webgl-fallback-chunk-unusable", { reason: reason || "" });
      }).catch(function(error) {
        sceneWebGLFallbackChunkState = "settled";
        gosxSceneEmit("warn", "webgl-fallback-chunk-failed", {
          reason: reason || "",
          error: error && error.message ? String(error.message) : String(error || ""),
        });
        if (disposed) {
          return;
        }
        // The chunk is unreachable. Re-enter the ladder so it continues down
        // to canvas2d instead of leaving the mount on a dead renderer.
        if (fallbackSceneRenderer(reason)) {
          renderLatestSceneBundle(reason);
          scheduleRenderWithViewport(reason);
        }
      });
      return true;
    }

	    function createFallbackSceneWebGLRenderer(reason) {
	      const useReplacementCanvas = sceneFallbackRequiresReplacementCanvas(reason);
	      if (!useReplacementCanvas) {
	        const currentResult = createSceneWebGLResult(canvas, props, capability, reason || "webgl-fallback");
	        if (currentResult && currentResult.renderer) {
	          return {
	            canvas: canvas,
	            result: currentResult,
	          };
	        }
	      }
	      const nextCanvas = prepareSceneReplacementCanvas();
	      const result = createSceneWebGLResult(nextCanvas, props, capability, reason || "webgl-fallback");
	      if (!result || !result.renderer) {
	        return null;
	      }
	      return {
	        canvas: nextCanvas,
	        result: result,
	      };
	    }

	    function getFallbackCanvas2D(reason) {
	      const useReplacementCanvas = sceneFallbackRequiresReplacementCanvas(reason);
	      if (!useReplacementCanvas) {
	        const current2d = typeof canvas.getContext === "function" ? canvas.getContext("2d") : null;
	        if (current2d) {
	          return {
	            canvas: canvas,
	            ctx2d: current2d,
	          };
	        }
	      }
	      const nextCanvas = prepareSceneReplacementCanvas();
	      const ctx2d = typeof nextCanvas.getContext === "function" ? nextCanvas.getContext("2d") : null;
	      if (!ctx2d) {
	        gosxSceneEmit("warn", "renderer-fallback-unavailable", { reason: reason || "" });
	        return null;
	      }
	      return {
	        canvas: nextCanvas,
	        ctx2d: ctx2d,
	      };
	    }

            function fallbackSceneRenderer(reason) {
              const fallbackReason = reason || "webgl-unavailable";
              const backendCaps = sceneBackendCapsOf(props);
              const allowWebGLFallback = sceneBackendCapsAllowsKind(backendCaps, "webgl");
              // __gosx_scene3d_require_gpu (see /docs/debugging-scene3d on the
              // gosx-docs site) lets a capture harness refuse the Canvas2D swap
              // outright, without touching the production DefaultPolicy. It only
              // removes the Canvas2D fallback below -- the WebGL fallback attempt
              // right after this block still runs, because WebGL is a real GPU
              // backend too.
              const requireGPUOnly = typeof window !== "undefined" && window.__gosx_scene3d_require_gpu === true;
              const allowCanvasFallback = sceneBackendCapsAllowsKind(backendCaps, "canvas2d") && !requireGPUOnly;
              const preferCanvasFallback = fallbackReason === "environment-constrained" || fallbackReason === "webgl-context-lost";
              if (!preferCanvasFallback && allowWebGLFallback) {
                // WebGL ships as a lazily fetched chunk. Fetch it before the
                // ladder gives up on this rung, otherwise a WebGPU device loss
                // would drop straight to canvas2d.
                if (!sceneWebGLRendererFactory() && deferSceneWebGLFallback(fallbackReason)) {
                  return true;
                }
                const webglFallback = createFallbackSceneWebGLRenderer(fallbackReason);
                if (webglFallback && webglFallback.result && webglFallback.result.renderer) {
                  if (webglFallback.canvas !== canvas) {
                    commitSceneCanvasReplacement(webglFallback.canvas, fallbackReason);
                  }
                  return swapRenderer(webglFallback.result.renderer, fallbackReason);
                }
              }
              if (sceneFirstWaterEntry(props)) {
                // Canvas2D and generic WebGL cannot represent the water
                // simulation. Expose the backend failure instead of swapping
                // to a renderer that would produce a plausible-but-blank demo.
                const waterReason = "water-webgl2-unavailable";
                applySceneRendererState(ctx.mount, renderer, waterReason);
                publishSceneWaterRendererState(ctx.mount, sceneState, null, waterReason);
                gosxSceneEmit("warn", "water-renderer-fallback-unavailable", {
                  reason: fallbackReason,
                });
                return false;
              }
              if (!allowWebGLFallback && !allowCanvasFallback) {
                gosxSceneEmit("warn", "renderer-fallback-disallowed", {
                  reason: fallbackReason,
                  capable: backendCaps && Array.isArray(backendCaps.capable) ? backendCaps.capable.slice() : [],
                });
                applySceneRendererState(ctx.mount, renderer, fallbackReason || "no-capable-backend");
                return false;
              }
              if (sceneRequiresWebGL(props)) {
                gosxSceneEmit("warn", "renderer-fallback-disabled", { reason: reason || "" });
                applySceneRendererState(ctx.mount, renderer, reason || "webgl-required");
                return false;
              }
              if (!allowCanvasFallback) {
                gosxSceneEmit("warn", "renderer-canvas-fallback-disallowed", {
                  reason: fallbackReason,
                  capable: backendCaps && Array.isArray(backendCaps.capable) ? backendCaps.capable.slice() : [],
                });
                applySceneRendererState(ctx.mount, renderer, fallbackReason || "no-capable-backend");
                return false;
              }
              const canvas2dFallback = getFallbackCanvas2D(fallbackReason);
              if (!canvas2dFallback) {
                return false;
              }
              if (canvas2dFallback.canvas !== canvas) {
                commitSceneCanvasReplacement(canvas2dFallback.canvas, fallbackReason);
              }
              return swapRenderer(createSceneCanvasRenderer(canvas2dFallback.ctx2d, canvas2dFallback.canvas), fallbackReason);
            }

    function ensureRendererCanCoverBundle(bundle) {
      if (!renderer || !bundle) {
        return true;
      }
      let feature = "";
      if (typeof renderer.supportsBundle === "function" && renderer.supportsBundle(bundle) === false) {
        feature = "backend-declared";
      }
      if (!feature) {
        return true;
      }
      gosxSceneEmit("warn", "renderer-feature-gap", {
        rendererKind: renderer.kind || "",
        feature,
      });
      return fallbackSceneRenderer("webgpu-feature-gap");
    }

    function renderLatestSceneBundle(reason) {
      if (disposed || !latestBundle || !renderer || typeof renderer.render !== "function" || !sceneCanRender()) {
        return false;
      }
      // The ladder condemned this renderer and is waiting on the WebGL chunk.
      // Drawing with a dead GPU device for that round trip buys nothing and can
      // throw out of a rAF callback, so skip the frame. The renderer object
      // stays in place, because swapRenderer still owns disposing it.
      if (sceneWebGLFallbackChunkState === "loading") {
        return false;
      }
      // A retained bundle is backend-specific. Never replay it after a
      // renderer/context/device swap onto a renderer that cannot consume local
      // geometry (or vice versa); the scheduled render below rebuilds the
      // bundle with the new renderer's capability.
      if (
        Boolean(latestBundle.retainedGeometryEnabled) !==
        Boolean(renderer.supportsRetainedGeometry === true)
      ) {
        scheduleRenderWithViewport(reason || "retained-geometry-capability-change");
        return false;
      }
      if (!ensureRendererCanCoverBundle(latestBundle)) {
        return false;
      }
      recordScenePerfCounter("render:" + (reason || "restore"));
      syncSceneNodeSentinels(latestBundle);
      renderer.render(latestBundle, viewport, createSceneRenderFrameMeta(null));
      recordSceneWaterFrame(ctx.mount, latestBundle);
      emitRendererWarmup(reason, latestBundle);
      maybeEmitRenderEmpty(latestBundle);
      renderSceneLabels(labelLayer, latestBundle, labelLayoutCache, labelElements, viewport.cssWidth, viewport.cssHeight);
      renderSceneSprites(labelLayer, latestBundle, spriteElements, viewport.cssWidth, viewport.cssHeight);
      renderSceneHTML(labelLayer, latestBundle, htmlElements, viewport.cssWidth, viewport.cssHeight, htmlTextureState);
      return true;
    }

    // emitRendererWarmup: called once per renderer-swap, after the first
    // render on the new renderer. Reports the bundle inventory the fresh
    // renderer just had to deal with so a silent post-restore black canvas
    // can be narrowed down to a specific resource class (PBR mesh count,
    // instanced mesh count, lights, post-fx, etc.) in the server slog.
    function emitRendererWarmup(reason, bundle) {
      gosxSceneEmit("info", "renderer-warmup", {
        rendererKind: renderer && renderer.kind ? renderer.kind : "",
        reason: reason || "",
        bundleMeshObjects: Array.isArray(bundle && bundle.meshObjects) ? bundle.meshObjects.length : 0,
        bundleInstancedMeshes: Array.isArray(bundle && bundle.instancedMeshes) ? bundle.instancedMeshes.length : 0,
        bundlePoints: Array.isArray(bundle && bundle.points) ? bundle.points.length : 0,
        bundleLights: Array.isArray(bundle && bundle.lights) ? bundle.lights.length : 0,
        bundleLabels: Array.isArray(bundle && bundle.labels) ? bundle.labels.length : 0,
        bundleSprites: Array.isArray(bundle && bundle.sprites) ? bundle.sprites.length : 0,
        bundleSurfaces: Array.isArray(bundle && bundle.surfaces) ? bundle.surfaces.length : 0,
        bundleComputeParticles: Array.isArray(bundle && bundle.computeParticles) ? bundle.computeParticles.length : 0,
        bundleWorldVertexCount: Number((bundle && bundle.worldVertexCount) || 0),
        bundleVertexCount: Number((bundle && bundle.vertexCount) || 0),
        bundleHasPostFX: Boolean(bundle && bundle.postEffects && Object.keys(bundle.postEffects).length > 0),
      });
    }

    function restoreSceneWebGLRenderer(reason) {
      const webglPreference = sceneCapabilityWebGLPreference(props, capability);
      if (!(webglPreference === "prefer" || webglPreference === "force")
          || !sceneBackendCapsAllowsKind(sceneBackendCapsOf(props), "webgl")) {
        return false;
      }
      const restoredRenderer = createSceneRenderer(canvas, props, capability);
      const webglRenderer = restoredRenderer && restoredRenderer.renderer;
      if (!webglRenderer || webglRenderer.kind !== "webgl") {
        if (webglRenderer && typeof webglRenderer.dispose === "function") {
          webglRenderer.dispose();
        }
        return false;
      }
      if (!swapRenderer(webglRenderer, reason || restoredRenderer.fallbackReason || "")) {
        return false;
      }
      renderLatestSceneBundle(reason || "webgl-restore");
      return true;
    }

    // Renderer stub used between context-lost and context-restored. Any
    // scheduleRender / rAF callbacks queued before the loss keep calling
    // `renderer.render(...)` — if that still points at the old WebGL
    // renderer, its cached program/buffer handles become stale the instant
    // the browser restores the context (same `gl` object, but all resources
    // invalidated), and every call raises GL_INVALID_OPERATION (1282),
    // silently blacking the canvas. Swapping in this stub before the fallback
    // runs means those queued callbacks harmlessly no-op instead.
    const sceneRendererLostStub = {
      kind: "lost",
      render: function () {},
      dispose: function () {},
    };

    function onWebGLContextLost(event) {
      if (!renderer || renderer.kind !== "webgl") {
        return;
      }
      if (event && typeof event.preventDefault === "function") {
        event.preventDefault();
      }
      gosxSceneEmit("warn", "webgl-context-lost", {
        voluntary: contextVoluntarilyLost === true,
      });
      // Dispose the live WebGL renderer immediately so its closures release
      // every handle (programs, FBOs, cascade textures, IBL cubemaps,
      // post-fx pipeline) before the browser can re-attach a fresh context
      // to the same canvas. Bypass swapRenderer/fallbackSceneRenderer's
      // telemetry so we don't emit a spurious renderer-swap to the stub.
      try {
        if (typeof renderer.dispose === "function") {
          renderer.dispose();
        }
      } catch (_err) {
        /* dispose errors on a lost context are expected */
      }
      renderer = sceneRendererLostStub;
      applySceneRendererState(ctx.mount, renderer, "webgl-context-lost");
      const swapped = fallbackSceneRenderer("webgl-context-lost");
      scheduleRender("webgl-context-lost");
      if (!swapped) {
        const variantScopeChange = replaceSceneModelTextureVariantScope(sceneState, sceneRendererLostStub);
        publishSceneModelTextureVariantContext(ctx.mount, variantScopeChange.scope);
        gosxSceneEmit("warn", "webgl-context-lost-no-fallback", {});
      }
    }

    function onWebGLContextRestored() {
      const voluntary = contextVoluntarilyLost === true;
      const watchdogPending = voluntaryRestorePending === true;
      contextVoluntarilyLost = false;
      // Natural event landed — cancel any outstanding voluntary-restore
      // watchdog so we don't force-restore on top of the browser's own work.
      clearVoluntaryRestoreWatchdog();
      const swapped = restoreSceneWebGLRenderer("");
      gosxSceneEmit(swapped ? "info" : "warn", "webgl-context-restored", {
        swapped: swapped,
        voluntary: voluntary,
        watchdogPending: watchdogPending,
      });
      if (swapped) {
        viewportDirty = true;
        scheduleRender("webgl-context-restored");
      }
    }

	    attachSceneCanvasContextListeners(canvas);
    startSceneRenderWatchdog();

    function sceneCanRender() {
      return lifecycle.pageVisible && lifecycle.inViewport;
    }

    function sceneWantsAnimation() {
      return sceneShouldAnimate() && sceneCanRender();
    }

    function cancelFrame() {
      if (frameHandle != null) {
        cancelEngineFrame(frameHandle);
        frameHandle = null;
      }
      applySceneRenderLoopState("");
    }

    function cancelScheduledRender() {
      if (renderHandle != null) {
        cancelEngineFrame(renderHandle);
        renderHandle = null;
      }
      applySceneRenderLoopState("");
    }

    function recordScenePerfCounter(name) {
      if (!(typeof window !== "undefined" && window.__gosx_scene3d_perf === true)) {
        return;
      }
      const key = String(name || "unknown");
      const counters = ctx.mount.__gosxScene3DScheduleCounts || Object.create(null);
      counters[key] = (counters[key] || 0) + 1;
      ctx.mount.__gosxScene3DScheduleCounts = counters;
    }

    function scheduleRender(reason) {
      if (disposed) {
        return;
      }
      lastRenderReason = reason || "refresh";
      recordScenePerfCounter("schedule:" + (reason || "refresh"));
      // A runtime program or a scene command can add a particle system or an
      // instanced mesh after the mount settled. Ask for the compute chunk
      // again here; ensureComputeFeatureLoaded caches, so a scene that already
      // has it, or never needs it, pays two array checks.
      requestSceneComputeFeatureIfNeeded(sceneState);
      if (initPending) {
        initReason = lastRenderReason;
        applySceneRenderLoopState(lastRenderReason);
        return;
      }
      if (renderHandle != null) {
        recordScenePerfCounter("coalesced:" + (reason || "refresh"));
        applySceneRenderLoopState(lastRenderReason);
        return;
      }
      // Defer the viewport read+write into the RAF callback. The old
      // code called sceneViewportFromMount / applySceneViewport
      // synchronously, which meant every scroll event forced two
      // layout flushes (one read pair before the write, one read pair
      // after, because applySceneViewport both mutates canvas size and
      // reads bounding rects for the label layer). Firefox coalesces
      // scroll events at 30Hz during active touch-scroll, so the
      // flushes stacked up and the browser had to reflow mid-scroll —
      // visible as jank and a frame of stale canvas content after the
      // scroll stopped. iOS Safari has the same symptom.
      //
      // Inside RAF the browser is already in a read phase (style+
      // layout has been resolved), so rect reads are cheap and the
      // subsequent canvas writes batch naturally into the following
      // compositor pass.
      renderHandle = engineFrame(function(now) {
        renderHandle = null;
        if (disposed) {
          return;
        }
        if (!sceneCanRender()) {
          cancelFrame();
          applySceneRenderLoopState("");
          return;
        }
        // Keep eager refreshes from overlapping an in-flight animation tick.
        cancelFrame();
        lastAnimationFrameAt = typeof now === "number" ? now : 0;
        renderFrame(typeof now === "number" ? now : 0, lastRenderReason || reason || "refresh");
      });
      applySceneRenderLoopState(lastRenderReason);
    }

    // Wraps scheduleRender so the caller can opt into marking the
    // viewport dirty. Used by the observers whose triggers imply a
    // physical viewport change (resize, visibility, capability /
    // environment, motion). Other scheduleRender callers (live
    // events, hub events, controls) don't need to force re-measurement
    // and should call scheduleRender directly.
    function scheduleRenderWithViewport(reason) {
      viewportDirty = true;
      scheduleRender(reason);
    }

    const domRegionTracker = typeof createSceneCustomPostDOMRegionTracker === "function"
      ? createSceneCustomPostDOMRegionTracker(ctx.mount, function() { return canvas; }, sceneState, scheduleRender)
      : null;

    function markSceneCSSInvalidated(reason) {
      const revision = Number(ctx.mount && ctx.mount.__gosxScene3DCSSRevision);
      ctx.mount.__gosxScene3DCSSRevision = Number.isFinite(revision) ? revision + 1 : 1;
      const transitionWindow = Math.max(
        sceneCSSTransitionWindowMillis(ctx.mount),
        sceneCSSTransitionWindowMillis(document && document.documentElement)
      );
      if (transitionWindow > 0) {
        sceneCSSAnimationUntil = Date.now() + transitionWindow;
      }
      ctx.mount.__gosxScene3DCSSAnimationUntil = sceneCSSAnimationUntil;
      scheduleRender(reason || "css");
    }

    function sceneCSSTransitionWindowMillis(element) {
      if (!element || typeof window.getComputedStyle !== "function") {
        return 0;
      }
      let style = null;
      try {
        style = window.getComputedStyle(element);
      } catch (_error) {
        style = null;
      }
      if (!style) {
        return 0;
      }
      const durations = sceneCSSParseTimeList(style.transitionDuration || (typeof style.getPropertyValue === "function" ? style.getPropertyValue("transition-duration") : ""));
      const delays = sceneCSSParseTimeList(style.transitionDelay || (typeof style.getPropertyValue === "function" ? style.getPropertyValue("transition-delay") : ""));
      const length = Math.max(durations.length, delays.length);
      let max = 0;
      for (let index = 0; index < length; index += 1) {
        const duration = durations[index % Math.max(1, durations.length)] || 0;
        const delay = delays[index % Math.max(1, delays.length)] || 0;
        max = Math.max(max, duration + delay);
      }
      return max > 0 ? max + 80 : 0;
    }

    function sceneCSSParseTimeList(value) {
      return String(value || "").split(",").map(function(part) {
        const text = part.trim().toLowerCase();
        if (!text) {
          return 0;
        }
        const number = Number.parseFloat(text);
        if (!Number.isFinite(number)) {
          return 0;
        }
        return text.endsWith("ms") ? number : number * 1000;
      });
    }

    function sceneCSSExternalStyleSignatureFromText(value) {
      const items = [];
      const parts = String(value || "").split(";");
      for (let index = 0; index < parts.length; index += 1) {
        const part = parts[index];
        const colon = part.indexOf(":");
        if (colon < 0) {
          continue;
        }
        const name = part.slice(0, colon).trim();
        if (!name || name.indexOf("--gosx-") === 0) {
          continue;
        }
        items.push(name + ":" + part.slice(colon + 1).trim());
      }
      items.sort();
      return items.join(";");
    }

    function sceneCSSExternalStyleSignature(element) {
      const style = element && element.style;
      if (!style) {
        return "";
      }
      const items = [];
      if (typeof style.length === "number" && typeof style.getPropertyValue === "function") {
        for (let index = 0; index < style.length; index += 1) {
          const name = style[index];
          if (!name || String(name).indexOf("--gosx-") === 0) {
            continue;
          }
          items.push(String(name) + ":" + String(style.getPropertyValue(name) || "").trim());
        }
        items.sort();
        return items.join(";");
      }
      return sceneCSSExternalStyleSignatureFromText(style.cssText || "");
    }

    function sceneCSSMutationShouldInvalidate(records) {
      let sawRecord = false;
      for (let index = 0; index < (records || []).length; index += 1) {
        const record = records[index];
        if (!record || record.type !== "attributes") {
          continue;
        }
        sawRecord = true;
        const attributeName = String(record.attributeName || "");
        if (attributeName === "class") {
          return true;
        }
        if (attributeName !== "style") {
          return true;
        }
        const previous = sceneCSSExternalStyleSignatureFromText(record.oldValue || "");
        const current = sceneCSSExternalStyleSignature(record.target);
        if (previous !== current) {
          return true;
        }
      }
      return !sawRecord;
    }

    function observeSceneCSSInvalidation() {
      const releases = [];
      if (typeof MutationObserver === "function") {
        const observer = new MutationObserver(function(records) {
          if (!sceneCSSMutationShouldInvalidate(records)) {
            return;
          }
          markSceneCSSInvalidated("css");
        });
        observer.observe(ctx.mount, {
          attributes: true,
          attributeFilter: ["class", "style"],
          attributeOldValue: true,
          subtree: false,
        });
        if (document && document.documentElement && document.documentElement !== ctx.mount) {
          observer.observe(document.documentElement, {
            attributes: true,
            attributeFilter: ["class", "style"],
            attributeOldValue: true,
            subtree: false,
          });
        }
        releases.push(function() { observer.disconnect(); });
      }
      if (typeof window.matchMedia === "function") {
        const queries = [
          "(prefers-color-scheme: dark)",
          "(prefers-reduced-motion: reduce)",
          "(prefers-contrast: more)",
          "(prefers-reduced-data: reduce)",
        ];
        for (let index = 0; index < queries.length; index += 1) {
          const query = window.matchMedia(queries[index]);
          const listener = function() {
            markSceneCSSInvalidated("css-media");
          };
          if (query && typeof query.addEventListener === "function") {
            query.addEventListener("change", listener);
            releases.push(function(q, l) {
              return function() { q.removeEventListener("change", l); };
            }(query, listener));
          } else if (query && typeof query.addListener === "function") {
            query.addListener(listener);
            releases.push(function(q, l) {
              return function() { q.removeListener(l); };
            }(query, listener));
          }
        }
      }
      return function releaseSceneCSSInvalidation() {
        for (let index = 0; index < releases.length; index += 1) {
          releases[index]();
        }
      };
    }

    function readSceneSourceCamera() {
      if (latestBundle && latestBundle.sourceCamera) {
        return latestBundle.sourceCamera;
      }
      if (latestBundle && latestBundle.camera) {
        return latestBundle.camera;
      }
      return sceneState.camera;
    }

	    function disposeSceneCanvasInteractionHandles() {
	      if (dragHandle && typeof dragHandle.dispose === "function") {
	        dragHandle.dispose();
	      }
	      if (pickHandle && typeof pickHandle.dispose === "function") {
	        pickHandle.dispose();
	      }
	      if (sceneControlHandle && typeof sceneControlHandle.dispose === "function") {
	        sceneControlHandle.dispose();
	      }
	      sceneControlHandle = null;
	      dragHandle = null;
	      if (gizmoDragHandle && typeof gizmoDragHandle.dispose === "function") {
	        gizmoDragHandle.dispose();
	      }
	      gizmoDragHandle = null;
	      pickHandle = null;
	    }

	    function installSceneCanvasInteractionHandles() {
	      pickHandle = setupScenePickInteractions(canvas, props, function() {
	        return viewport;
	      }, function() {
	        return latestBundle;
	      }, function(detail) {
	        latestScenePickDetail = detail ? sceneDebugClone(detail, 4) : null;
	        dispatchSceneHTMLTexturePointer(latestBundle, htmlElements, detail);
	        ctx.emit("scene-interaction", detail);
	        if (ctx.mount && typeof ctx.mount.dispatchEvent === "function") {
	          const inputDetail = { kind: "pick", input: detail ? sceneDebugClone(detail, 4) : null };
	          const inputEvent = typeof CustomEvent === "function"
	            ? new CustomEvent("gosx:scene3d:input", { detail: inputDetail, bubbles: true })
	            : { type: "gosx:scene3d:input", detail: inputDetail };
	          ctx.mount.dispatchEvent(inputEvent);
	        }
	      });
	      // Gizmo drags own pointer-down near an active TransformControls form.
	      // Registered before the camera controls so stopImmediatePropagation can
	      // reserve the gesture; presses away from the gizmo fall through.
	      gizmoDragHandle = setupSceneGizmoDragInteractions(canvas, props, function() {
	        return viewport;
	      }, function() {
	        return latestBundle;
	      }, function() {
	        if (!lastAppliedSelectionID || !lastAppliedGizmoMode) return null;
	        const objects = sceneStateObjects(sceneState);
	        let target = null;
	        for (let i = 0; i < objects.length; i++) {
	          if (objects[i].id === lastAppliedSelectionID) { target = objects[i]; break; }
	        }
	        if (!target) return null;
	        const anchor = sceneGizmoTargetAnchor(target);
	        return { targetID: lastAppliedSelectionID, mode: lastAppliedGizmoMode, anchor: anchor };
	      }, function(payload) {
	        if (payload && payload.phase !== "start" && payload.mode === "translate" && payload.position) {
	          applySceneObjectPatch(sceneState, payload.target, { x: payload.position.x, y: payload.position.y, z: payload.position.z });
	          syncMountedSceneGizmoHelpers();
	          scheduleRender("gizmo-drag");
	        }
	        if (typeof props.gizmoOutputSignal === "string" && props.gizmoOutputSignal) {
	          queueInputSignal(props.gizmoOutputSignal, payload);
	        }
	        if (ctx.mount && typeof ctx.mount.dispatchEvent === "function") {
	          const gizmoDetail = { kind: "gizmo-commit", input: payload };
	          const gizmoEvent = typeof CustomEvent === "function"
	            ? new CustomEvent("gosx:scene3d:input", { detail: gizmoDetail, bubbles: true })
	            : { type: "gosx:scene3d:input", detail: gizmoDetail };
	          ctx.mount.dispatchEvent(gizmoEvent);
	        }
	      });
	      // Picking owns pointer-down on authored targets. Register it before
	      // orbit controls so preventDefault can reserve the gesture; background
	      // drags still fall through to camera navigation.
	      sceneControlHandle = setupSceneBuiltInControls(canvas, props, function() {
	        return viewport;
	      }, readSceneSourceCamera, scheduleRender);
	      dragHandle = sceneControlHandle.controller
	        ? { dispose() {} }
	        : setupSceneDragInteractions(canvas, props, function() {
	          return viewport;
	        }, function() {
	          return latestBundle;
	        });
	    }

	    function reinstallSceneCanvasInteractionHandles(reason) {
	      disposeSceneCanvasInteractionHandles();
	      installSceneCanvasInteractionHandles();
	      if (reason) {
	        gosxSceneEmit("info", "scene-canvas-interactions-rebound", {
	          reason: reason || "",
	        });
	      }
	    }

	    installSceneCanvasInteractionHandles();

	    // Page-owned simulation/engine programs use this mount-scoped event seam
	    // to apply typed Scene3D diffs. A monotonic revision rejects late async
	    // search results and replayed batches without exposing renderer internals.
	    let lastMountCommandRevision = 0;
	    function emitMountCommandsApplied(revision, commandCount) {
	      if (!ctx.mount || typeof ctx.mount.dispatchEvent !== "function") return;
	      const detail = { revision, commandCount };
	      const event = typeof CustomEvent === "function"
	        ? new CustomEvent("gosx:scene3d:commands-applied", { detail, bubbles: true })
	        : { type: "gosx:scene3d:commands-applied", detail };
	      ctx.mount.dispatchEvent(event);
	    }
	    function onMountCommands(event) {
	      const detail = event && event.detail && typeof event.detail === "object" ? event.detail : {};
	      const revision = Number(detail.revision);
	      const commands = Array.isArray(detail.commands) ? detail.commands : null;
	      if (!Number.isSafeInteger(revision) || revision <= 0 || revision <= lastMountCommandRevision || !commands) return;
	      lastMountCommandRevision = revision;
	      // applyMountedSceneCommands always returns a Promise, so the async
	      // dispatch below always applies; there is no synchronous fallback.
	      applyMountedSceneCommands(commands, "mount-commands").then(function() {
	        emitMountCommandsApplied(revision, commands.length);
	      });
	    }
	    if (ctx.mount && typeof ctx.mount.addEventListener === "function") {
	      ctx.mount.addEventListener("gosx:scene3d:commands", onMountCommands);
	    }

	    let lastPublishedCamera = null;
    let lastAppliedSelectionID = null;
    let applyingSignalCamera = false;
    let applyingSignalSelection = false;
    let lastAppliedGizmoMode = null;
    let applyingSignalGizmoMode = false;

    // syncMountedSceneGizmoHelpers is the shared live-update pass for
    // TransformControls helper meshes (Mesh.GizmoHelper / gizmoHelper:true;
    // see scene.go's lowerTransformControls). Re-run after either the
    // selection signal or the gizmo-mode signal changes, so the two signals
    // together drive every GizmoHelper mesh: hidden while nothing is
    // selected, repositioned onto the selected object's live world
    // transform otherwise, and — of the three baked forms (translate axes
    // triad / rotate ring / scale handle cubes) — only the one whose
    // gizmoFormMode matches the active mode signal shown. No page
    // navigation / SSR round-trip needed for any of it.
    //
    // Also preserves the legacy selection-independent mode-only toggle (P6)
    // for any plain Mesh.GizmoRing=true object that doesn't opt into the
    // full gizmoHelper group.
    function syncMountedSceneGizmoHelpers() {
      const objects = sceneStateObjects(sceneState);
      const mode = lastAppliedGizmoMode || "translate";
      const selID = lastAppliedSelectionID || "";
      let target = null;
      if (selID) {
        for (let i = 0; i < objects.length; i++) {
          if (objects[i].id === selID) {
            target = objects[i];
            break;
          }
        }
      }
      const anchor = target ? sceneGizmoTargetAnchor(target) : null;
      for (let i = 0; i < objects.length; i++) {
        const obj = objects[i];
        if (obj.gizmoHelper) {
          const visible = Boolean(target) && obj.gizmoFormMode === mode;
          const patch = { visible: visible };
          if (anchor) {
            patch.x = anchor.x;
            patch.y = anchor.y;
            patch.z = anchor.z;
            patch.rotationX = anchor.rotationX;
            patch.rotationY = anchor.rotationY;
            patch.rotationZ = anchor.rotationZ;
          }
          applySceneObjectPatch(sceneState, obj.id, patch);
        } else if (obj.gizmoRing) {
          applySceneObjectPatch(sceneState, obj.id, { visible: mode === "rotate" });
        }
      }
    }

    function applyMountedSceneSelection(selectedID) {
      const id = typeof selectedID === "string" ? selectedID : "";
      if (id === lastAppliedSelectionID) return;
      applyingSignalSelection = true;
      const objects = sceneStateObjects(sceneState);
      for (let i = 0; i < objects.length; i++) {
        applySceneObjectPatch(sceneState, objects[i].id, { selected: objects[i].id === id });
      }
      lastAppliedSelectionID = id;
      applyingSignalSelection = false;
      syncMountedSceneGizmoHelpers();
      scheduleRender("signal-selection");
    }

    // applyMountedSceneGizmoMode drives the TransformControls gizmo live off
    // Props.GizmoInputSignal, delegating to syncMountedSceneGizmoHelpers
    // (which also accounts for the current selection) for the actual
    // visibility + reposition work. Mirrors applyMountedSceneSelection
    // above: patch already-mounted objects in place, no server round-trip.
    function applyMountedSceneGizmoMode(mode) {
      const nextMode = typeof mode === "string" ? mode : "";
      if (nextMode === lastAppliedGizmoMode) return;
      applyingSignalGizmoMode = true;
      lastAppliedGizmoMode = nextMode;
      syncMountedSceneGizmoHelpers();
      applyingSignalGizmoMode = false;
      scheduleRender("signal-gizmo-mode");
    }

	    function currentMountedSceneCamera(sourceCamera) {
	      return sceneRenderCamera(sceneCurrentControlCamera(
	        sceneControlHandle && sceneControlHandle.controller,
	        sourceCamera || readSceneSourceCamera(),
	        sceneState._scrollCamera,
	      ));
    }

    function currentMountedSceneOrbitState() {
      const controller = sceneControlHandle && sceneControlHandle.controller;
      if (!controller || controller.mode !== "orbit") {
        return null;
      }
      const sourceCamera = readSceneSourceCamera();
      syncSceneControlsFromCamera(controller, sourceCamera);
      const orbit = controller.orbit || sceneOrbitStateFromCamera(sourceCamera, controller.target, controller);
      if (!orbit) {
        return null;
      }
      const target = orbit.target || controller.target || { x: 0, y: 0, z: 0 };
      return {
        target: {
          x: sceneNumber(target.x, 0),
          y: sceneNumber(target.y, 0),
          z: sceneNumber(target.z, 0),
        },
        radius: sceneNumber(orbit.radius, 0),
        yaw: sceneNumber(orbit.yaw, 0),
        pitch: sceneNumber(orbit.pitch, 0),
      };
    }

    function publishMountedSceneCamera(camera, reason) {
      const nextCamera = sceneRenderCamera(camera);
      if (lastPublishedCamera && sceneCameraEquivalent(lastPublishedCamera, nextCamera)) {
        return;
      }
      lastPublishedCamera = nextCamera;
      ctx.emit("scene-camera", {
        camera: nextCamera,
        reason: reason || "render",
      });
      if (typeof props.cameraOutputSignal === "string" && props.cameraOutputSignal && !applyingSignalCamera) {
        queueInputSignal(props.cameraOutputSignal, nextCamera);
      }
    }

    function applyMountedSceneCamera(camera, reason) {
      if (!sceneIsPlainObject(camera)) {
        return false;
      }
	      const currentCamera = currentMountedSceneCamera();
	      const nextCamera = normalizeSceneCamera(camera, currentCamera);
	      if (sceneCameraEquivalent(currentCamera, nextCamera)) {
	        return false;
	      }
	      sceneState.camera = nextCamera;
	      applySceneControlsCamera(sceneControlHandle && sceneControlHandle.controller, nextCamera);
	      scheduleRender(reason || "camera");
	      publishMountedSceneCamera(nextCamera, reason || "camera");
	      return true;
	    }
    function buildSceneDebugSnapshot(mode) {
      const rendererKind = renderer && renderer.kind ? renderer.kind : "";
      const rendererDiagnostics = renderer && typeof renderer.diagnostics === "function" ? renderer.diagnostics() : null;
      const surfaceID = sceneDebugMountID(ctx.mount, ctx.id);
      const counts = sceneDebugBundleCounts(latestBundle, sceneState);
      const features = sceneDebugFeatureMatrix(latestBundle, sceneState, rendererKind);
      const snapshot = {
        schema: SCENE3D_DEBUG_SCHEMA,
        id: surfaceID,
        mountID: ctx.mount && ctx.mount.id ? String(ctx.mount.id) : "",
        engineID: String(ctx.id || ""),
        component: String(ctx.component || ""),
        renderer: rendererKind,
        fallbackReason: sceneDebugAttr(ctx.mount, "data-gosx-scene3d-renderer-fallback"),
        ready: sceneDebugAttr(ctx.mount, readyAttr) === "true",
        active: sceneDebugAttr(ctx.mount, "data-gosx-scene3d-active") !== "false",
        renderLoop: sceneRenderLoopSnapshot(""),
        controls: normalizeSceneControlsMode(props.controls),
        viewport: {
          cssWidth: sceneNumber(viewport && viewport.cssWidth, 0),
          cssHeight: sceneNumber(viewport && viewport.cssHeight, 0),
          devicePixelRatio: sceneNumber(viewport && viewport.devicePixelRatio, 1),
        },
        counts,
        features,
        diagnostics: sceneDebugDiagnostics(ctx.mount, rendererKind, rendererDiagnostics),
        lastPick: latestScenePickDetail || (pickHandle && typeof pickHandle.getSnapshot === "function" ? pickHandle.getSnapshot() : null),
      };
      if (mode !== "summary") {
        snapshot.camera = currentMountedSceneCamera();
        snapshot.gpuResources = sceneDebugGPUResources(ctx.mount, canvas, renderer, latestBundle, viewport, labelLayer, rendererDiagnostics);
        snapshot.webgpuStats = sceneDebugClone(ctx.mount && ctx.mount.__gosxScene3DWebGPUStats, 3);
        snapshot.waterShaderSources = { sceneState: [], bundle: [] };
        snapshot.rendererDiagnostics = sceneDebugClone(rendererDiagnostics, 3);
      }
      return snapshot;
    }
    const releaseSceneDebugSurface = sceneDebugRegisterSurface({
      id: sceneDebugMountID(ctx.mount, ctx.id),
      mountID: ctx.mount && ctx.mount.id ? String(ctx.mount.id) : "",
      engineID: String(ctx.id || ""),
      component: String(ctx.component || ""),
      mount: ctx.mount,
      snapshot: buildSceneDebugSnapshot,
      captureFrame() {
        const surfaceID = sceneDebugMountID(ctx.mount, ctx.id);
        if (!canvas || typeof canvas.toDataURL !== "function") {
          return { surfaceID, dataURL: null, reason: "capture-unavailable" };
        }
        return {
          surfaceID,
          mimeType: "image/png",
          dataURL: canvas.toDataURL("image/png"),
        };
      },
    });
    const inspectorEnabled = sceneBool(
      props.inspector,
      typeof window !== "undefined" && window.__gosx_scene3d_inspector === true,
    );
    inspectorOverlay = createSceneInspectorOverlay(ctx.mount, inspectorEnabled, function() {
      return buildSceneDebugSnapshot("full");
    });
    let pendingMotionData = null;
    let pendingMotionHandle = null;
    // De-dupes the "audio" hub event below by AudioCue.Seq (scene/audio.go),
    // mirroring the fight demo's own lastFeedbackSeq check in 30-tail.js's
    // onHubMessage — a cue redelivered on an unchanged seq is a no-op here.
    let lastSceneAudioSeq = 0;

    function applySceneHubEvent(eventName, data, reason) {
      const cameraChanged = sceneApplyCameraLiveEvent(sceneState, data);
      if (cameraChanged) {
        applySceneControlsCamera(sceneControlHandle.controller, sceneState.camera);
      }
      const modelChanged = sceneApplyModelLiveEvent(sceneState, eventName, data);
      const liveChanged = sceneApplyLiveEvent(sceneState, eventName, data, motion.reducedMotion, sceneNowMilliseconds());
      if (cameraChanged || modelChanged || liveChanged) {
        scheduleRender(reason || "hub-event");
      }
    }

    // applySceneAudioCue is the dedicated "audio" hub-event delivery path
    // documented on scene.AudioCue (scene/audio.go): it lets any
    // server-driven scene fire a sample-clip or synth cue independent of
    // the fight-specific hub input controller's own hard-coded tick
    // parsing (createHubInputController.onHubMessage, 30-tail.js), which
    // remains untouched. window.__gosx.audio and window.__gosx.arcadeAudio
    // are looked up dynamically (not imported) because this file is also
    // compiled standalone into bootstrap-feature-scene3d.js, which does not
    // itself carry either engine's source — both are expected to already
    // be installed on window by whatever base runtime bundle loaded first,
    // same as the pre-existing props.audio manifest wiring below.
    function applySceneAudioCue(data) {
      const cue = data && typeof data === "object" ? data : null;
      if (!cue) {
        return;
      }
      const seq = Math.floor(sceneNumber(cue.seq, 0));
      if (seq > 0) {
        if (seq === lastSceneAudioSeq) {
          return;
        }
        lastSceneAudioSeq = seq;
      }
      const clip = typeof cue.clip === "string" ? cue.clip.trim() : "";
      const gosxAudio = window.__gosx && window.__gosx.audio;
      if (clip && gosxAudio && typeof gosxAudio.play === "function") {
        gosxAudio.play(clip, {
          volume: cue.volume,
          rate: cue.rate,
          loop: Boolean(cue.loop),
          bus: cue.bus,
          pan: cue.pan,
          position: cue.position,
          handle: cue.handle,
        });
        return;
      }
      const arcadeAudio = window.__gosx && window.__gosx.arcadeAudio;
      if (!arcadeAudio) {
        return;
      }
      const synthOptions = { intensity: cue.intensity, pan: cue.pan, depth: cue.depth, rate: cue.rate };
      if (cue.patch && typeof cue.patch === "object" && typeof arcadeAudio.playPatch === "function") {
        arcadeAudio.playPatch(cue.patch, synthOptions);
        return;
      }
      const name = typeof cue.cue === "string" ? cue.cue.trim() : "";
      if (name && name !== "none" && typeof arcadeAudio.play === "function") {
        arcadeAudio.play(name, synthOptions);
      }
    }

    function flushPendingMotionEvent() {
      pendingMotionHandle = null;
      if (disposed || !pendingMotionData) {
        pendingMotionData = null;
        return;
      }
      const data = pendingMotionData;
      pendingMotionData = null;
      applySceneHubEvent("motion", data, "hub-motion");
    }

    const sceneHubListener = function(event) {
      if (disposed) {
        return;
      }
      const detail = event && event.detail && typeof event.detail === "object" ? event.detail : null;
      if (!detail || typeof detail.event !== "string") {
        return;
      }
      if (detail.event === "motion") {
        pendingMotionData = detail.data;
        if (pendingMotionHandle == null) {
          pendingMotionHandle = engineFrame(flushPendingMotionEvent);
        }
        return;
      }
      if (detail.event === "audio") {
        applySceneAudioCue(detail.data);
        return;
      }
      applySceneHubEvent(detail.event, detail.data, "hub-event");
    };
    document.addEventListener("gosx:hub:event", sceneHubListener);

    let unsubCameraSignal = null;
    if (typeof props.cameraInputSignal === "string" && props.cameraInputSignal) {
      unsubCameraSignal = gosxSubscribeSharedSignal(props.cameraInputSignal, function(value) {
        if (disposed) return;
        const cam = (sceneIsPlainObject(value) && sceneIsPlainObject(value.camera)) ? value.camera : value;
        applyingSignalCamera = true;
        applyMountedSceneCamera(cam, "signal-camera");
        applyingSignalCamera = false;
      }, { immediate: false });
    }
    let unsubSelectionSignal = null;
    if (typeof props.selectionInputSignal === "string" && props.selectionInputSignal) {
      unsubSelectionSignal = gosxSubscribeSharedSignal(props.selectionInputSignal, function(value) {
        if (disposed) return;
        const id = typeof value === "string" ? value : (value && value.selectedID);
        applyMountedSceneSelection(id || "");
      }, { immediate: false });
    }
    let unsubGizmoSignal = null;
    if (typeof props.gizmoInputSignal === "string" && props.gizmoInputSignal) {
      unsubGizmoSignal = gosxSubscribeSharedSignal(props.gizmoInputSignal, function(value) {
        if (disposed) return;
        const mode = typeof value === "string" ? value : (value && value.mode);
        applyMountedSceneGizmoMode(mode || "");
      }, { immediate: false });
    }

    // Viewport observer fires on canvas/mount resize. Mark dirty so
    // renderFrame re-measures the rect on the next tick — this is the
    // one place we genuinely need a fresh getBoundingClientRect.
    const releaseViewportObserver = observeSceneViewport(ctx.mount, function(reason) {
      sceneUpdateScrollCameraMetrics(sceneState._scrollCamera, true);
      scheduleRenderWithViewport(reason);
    });
    const releaseCapabilityObserver = observeSceneCapability(ctx.mount, props, capability, function(reason) {
      // Capability change (DPR / WebGL availability shift) invalidates
      // the viewport — mark dirty so the next renderFrame re-measures.
      viewportDirty = true;
      sceneState.capability = capability;
      sceneState.materials = sceneNormalizeMaterialList(sceneState._materialSource, capability);
      const desiredFallback = sceneRendererFallbackReason(props, capability, renderer && renderer.kind);
      const webglPreference = sceneCapabilityWebGLPreference(props, capability);
      if (renderer && renderer.kind === "webgl" && !(webglPreference === "prefer" || webglPreference === "force")) {
        fallbackSceneRenderer(desiredFallback || "environment-constrained");
      } else if (renderer && renderer.kind !== "webgl" && (webglPreference === "prefer" || webglPreference === "force")) {
        if (!restoreSceneWebGLRenderer("")) {
          applySceneRendererState(ctx.mount, renderer, desiredFallback);
        }
      } else {
        applySceneRendererState(ctx.mount, renderer, desiredFallback);
      }
      scheduleRender(reason || "capability");
    });
    const releaseLifecycleObserver = observeSceneLifecycle(ctx.mount, lifecycle, function(reason) {
      publishSceneWaterLifecycleState(ctx.mount, sceneState, lifecycle, false);
      notifySceneRendererLifecycle(reason || "lifecycle", false, false);
      if (!sceneCanRender()) {
        cancelFrame();
        cancelScheduledRender();
        if (labelRefreshHandle != null) {
          cancelEngineFrame(labelRefreshHandle);
          labelRefreshHandle = null;
        }
        scheduleIdleContextRelease();
        return;
      }
      // Visibility/viewport presence transition — the mount may have
      // been offscreen, so force a re-measure on resume.
      clearIdleContextRelease();
      if (contextVoluntarilyLost) {
        restoreVoluntarilyLostContext();
        // The webglcontextrestored event handler will call
        // restoreSceneWebGLRenderer + scheduleRender once the
        // browser finishes restoring the context.
        return;
      }
      scheduleRenderWithViewport(reason || "lifecycle");
    });
    const releaseMotionObserver = observeSceneMotion(ctx.mount, motion, function(reason) {
      cancelFrame();
      cancelScheduledRender();
      // Reduced-motion transition resets render state; safer to re-
      // measure than risk stale canvas dimensions.
      scheduleRenderWithViewport(reason || "motion");
    });
    const releaseSceneCSSObserver = observeSceneCSSInvalidation();
    const releaseManagedControlForms = typeof bindSceneManagedControlForms === "function"
      ? bindSceneManagedControlForms(ctx.mount, sceneState, function(commands) {
          const result = applySceneCommands(sceneState, commands);
          publishSceneWaterStateSnapshot(ctx.mount, sceneState);
          publishSceneWaterLifecycleState(ctx.mount, sceneState, lifecycle, false);
          notifySceneRendererLifecycle("managed-control-forms", false, false);
          if (result && typeof result.then === "function") {
            result.then(function() {
              scheduleRender("managed-control-forms-models");
            });
          }
          scheduleRender("managed-control-forms");
        }, {
          getCamera: currentMountedSceneCamera,
            getOrbitState: currentMountedSceneOrbitState,
          setCamera: function(camera) {
            return applyMountedSceneCamera(camera, "managed-control-forms-camera");
          },
          getControlTarget: function() {
            return sceneControlsTarget(props);
          },
          stopCameraInertia: function() {
            if (!sceneControlHandle || typeof sceneControlHandle.stopInertia !== "function") {
              return false;
            }
            return sceneControlHandle.stopInertia();
          },
          getBundle: function() {
            return latestBundle;
          },
        })
      : function() {};

    if (runtimeScene) {
      if (ctx.runtime && ctx.runtime.available()) {
        await applySceneCommands(sceneState, await ctx.runtime.hydrateFromProgramRef());
      } else {
        console.warn("[gosx] shared engine runtime unavailable");
      }
    }

    // WASM motion seam (P2.4b): lazy-load the scene's motion program once, then
    // each frame tick + decode packed transform writes into SET_TRANSFORM
    // commands routed through applySceneCommands (so state re-normalizes). Inert
    // unless window.__gosx_motion_wasm is set and the exports are present.
    //
    // SCOPE: this seam runs ONLY on the JS-sceneState render path — i.e. the
    // createSceneRenderBundle fall-through (onRenderEngine returns ""), and
    // declarative SceneIR scenes that ship motionProgram but render via JS rather
    // than the Go runtime bundle. Production shared-runtime Scene3D scenes call
    // ctx.runtime.renderFrame(), receive a non-empty runtimeBundle, and return at
    // line ~7128 BEFORE reaching this call — so this seam does NOT drive them.
    // Motion for those scenes is computed by motion.Eval inside
    // client/vm/scene_render_bundle.go and is baked into the Go-produced bundle.
    function applyWasmMotionFrame(timeSeconds) {
      if (wasmMotionState < 0) return;
      if (typeof window === "undefined" || !window.__gosx_motion_wasm
          || typeof window.__gosx_motion_load !== "function") {
        return;
      }
      if (wasmMotionState === 0) {
        // The scene IR (carrying motionProgram as base64) rides under
        // props.scene; some callers pass the scene object as props directly.
        const sceneIR = props && typeof props.scene === "object" && props.scene ? props.scene : props;
        const b64 = sceneIR && typeof sceneIR.motionProgram === "string" ? sceneIR.motionProgram : "";
        const handle = b64 ? window.__gosx_motion_load(sceneBase64Decode(b64)) : 0;
        const refs = handle >= 1 && typeof window.__gosx_motion_refs === "function"
          ? window.__gosx_motion_refs(handle) : null;
        if (!refs) { wasmMotionState = -1; return; }
        wasmMotionHandle = handle;
        wasmMotionTargetRefs = refs.target || [];
        wasmMotionPropRefs = refs.prop || [];
        wasmMotionF64 = new Float64Array(256);
        wasmMotionU8 = new Uint8Array(wasmMotionF64.buffer);
        wasmMotionState = 1;
      }
      const reduced = motion.reducedMotion === true;
      let count = window.__gosx_motion_tick(wasmMotionHandle, timeSeconds, reduced, wasmMotionU8);
      if (count > wasmMotionF64.length) {
        wasmMotionF64 = new Float64Array(count);
        wasmMotionU8 = new Uint8Array(wasmMotionF64.buffer);
        count = window.__gosx_motion_tick(wasmMotionHandle, timeSeconds, reduced, wasmMotionU8);
        if (count > wasmMotionF64.length) count = wasmMotionF64.length;
      }
      const f = wasmMotionF64;
      const cmds = [];
      for (let i = 0; i + 3 <= count;) {
        const arity = f[i + 2];
        // motion.ValueArity width: 0 scalar=1,1 vec2=2,2 vec3=3,3+ (vec4/quat/color)=4.
        const width = arity === 0 ? 1 : (arity >= 3 ? 4 : arity + 1);
        const ref = wasmMotionTargetRefs[f[i]];
        const prop = wasmMotionPropRefs[f[i + 1]];
        const c = i + 3;
        if (c + width > count) break;
        i = c + width;
        if (ref == null || prop == null) continue;
        let data = null;
        if (prop === "position" && width >= 3) {
          data = { x: f[c], y: f[c + 1], z: f[c + 2] };
        } else if (prop === "scale" && width >= 3) {
          data = { scaleX: f[c], scaleY: f[c + 1], scaleZ: f[c + 2] };
        } else if (prop === "rotation" && arity === 4) {
          const e = sceneQuatToEulerXYZ(f[c], f[c + 1], f[c + 2], f[c + 3]);
          data = { rotationX: e.x, rotationY: e.y, rotationZ: e.z };
        }
        if (data) cmds.push({ kind: SCENE_CMD_SET_TRANSFORM, objectId: ref, data });
      }
      if (cmds.length > 0) applySceneCommands(sceneState, cmds);
    }

    // C3: motion-evaluated MATERIAL UNIFORM animation. Mirrors
    // applyWasmMotionFrame but loads props.scene.materialMotionProgram (whose
    // tracks target material uniforms: targetRef=mesh id, prop=uniform name).
    // Each frame it ticks at absolute time t, decodes packed
    // [targetID, propID, arity, comps...] records, and writes the evaluated
    // value into the mesh's customUniforms bag (the same bag selena re-packs
    // per frame via sceneSelenaUniformData). MUST run BEFORE the per-frame
    // bundle build so the next createSceneRenderBundle clones the new value.
    // Stateless single-program tick at absolute t, so a grow-and-retick at the
    // same t is safe (no clock-advance concern like the model mixer).
    function applyWasmMaterialMotionFrame(timeSeconds) {
      if (wasmMatMotionState < 0) return;
      if (typeof window === "undefined" || !window.__gosx_motion_wasm
          || typeof window.__gosx_motion_load !== "function") {
        return;
      }
      if (wasmMatMotionState === 0) {
        const sceneIR = props && typeof props.scene === "object" && props.scene ? props.scene : props;
        const b64 = sceneIR && typeof sceneIR.materialMotionProgram === "string" ? sceneIR.materialMotionProgram : "";
        const handle = b64 ? window.__gosx_motion_load(sceneBase64Decode(b64)) : 0;
        if (handle < 1) { wasmMatMotionState = -1; return; }
        const refs = typeof window.__gosx_motion_refs === "function"
          ? window.__gosx_motion_refs(handle) : null;
        if (!refs) { wasmMatMotionState = -1; return; }
        wasmMatMotionHandle = handle;
        wasmMatMotionTargetRefs = refs.target || [];
        wasmMatMotionPropRefs = refs.prop || [];
        wasmMatMotionF64 = new Float64Array(256);
        wasmMatMotionU8 = new Uint8Array(wasmMatMotionF64.buffer);
        wasmMatMotionState = 1;
      }
      const reduced = motion.reducedMotion === true;
      let count = window.__gosx_motion_tick(wasmMatMotionHandle, timeSeconds, reduced, wasmMatMotionU8);
      if (count > wasmMatMotionF64.length) {
        wasmMatMotionF64 = new Float64Array(count);
        wasmMatMotionU8 = new Uint8Array(wasmMatMotionF64.buffer);
        count = window.__gosx_motion_tick(wasmMatMotionHandle, timeSeconds, reduced, wasmMatMotionU8);
        if (count > wasmMatMotionF64.length) count = wasmMatMotionF64.length;
      }
      const f = wasmMatMotionF64;
      for (let i = 0; i + 3 <= count;) {
        const arity = f[i + 2];
        // arity ENUM ordinal → component width: Scalar=0→1, Vec2=1→2, Vec3=2→3,
        // Vec4=3→4, Quat=4→4, Color=5→4.
        const width = arity <= 0 ? 1 : (arity >= 3 ? 4 : arity + 1);
        const meshId = wasmMatMotionTargetRefs[f[i]];
        const uniformName = wasmMatMotionPropRefs[f[i + 1]];
        const c = i + 3;
        if (c + width > count) break;
        i = c + width;
        if (meshId == null || uniformName == null) continue;
        const uniforms = sceneResolveMaterialUniforms(sceneState, meshId);
        if (!uniforms) continue;
        if (width === 1) {
          uniforms[uniformName] = f[c];
        } else {
          const arr = new Array(width);
          for (let k = 0; k < width; k++) arr[k] = f[c + k];
          uniforms[uniformName] = arr;
        }
      }
    }

    function publishReady() {
      if (readySent || disposed) {
        return;
      }
      readySent = true;
      setAttrValue(ctx.mount, readyAttr, "true");
      setAttrValue(ctx.mount, mountedAttr, "true");
      ctx.emit("mounted", {
        width: viewport.cssWidth,
        height: viewport.cssHeight,
        objects: sceneStateObjects(sceneState).length,
        labels: sceneStateLabels(sceneState).length,
        sprites: sceneStateSprites(sceneState).length,
        html: sceneStateHTML(sceneState).length,
        lights: sceneStateLights(sceneState).length,
        models: sceneModels(props).length,
      });
    }

    // sceneFrameHasContent mirrors maybeEmitRenderEmpty's notion of a
    // drawable frame: legacy verts, surfaces, or a modern PBR mesh/instance
    // list on the bundle, else declared points/objects on sceneState (the
    // points path draws from state, not the bundle lists).
    function sceneFrameHasContent(bundle) {
      if (bundle) {
        if (Number(bundle.vertexCount || 0) > 0 || Number(bundle.worldVertexCount || 0) > 0) {
          return true;
        }
        if ((Array.isArray(bundle.surfaces) && bundle.surfaces.length > 0)
            || (Array.isArray(bundle.meshObjects) && bundle.meshObjects.length > 0)
            || (Array.isArray(bundle.instancedMeshes) && bundle.instancedMeshes.length > 0)) {
          return true;
        }
      }
      if (Array.isArray(sceneState.points) && sceneState.points.length > 0) {
        return true;
      }
      if (sceneState.meshObjects && sceneState.meshObjects.length > 0) {
        return true;
      }
      return Array.isArray(sceneState.objects) && sceneState.objects.length > 0;
    }

    function publishRevealed(bundle) {
      if (revealSent || disposed || !sceneFrameHasContent(bundle)) {
        return;
      }
      revealSent = true;
      setAttrValue(ctx.mount, revealedAttr, "true");
      if (revealClass && document.documentElement) {
        document.documentElement.classList.add(revealClass);
      }
    }

    function renderFrame(now, reason) {
      if (initPending) {
        initReason = reason || initReason || "refresh";
        applySceneRenderLoopState(initReason);
        return;
      }
      if (disposed) return;
      const frameStart = typeof performance !== "undefined" && performance.now ? performance.now() : Date.now();
      const perfEnabled = typeof window !== "undefined" && window.__gosx_scene3d_perf === true;
      recordScenePerfCounter("render:" + (reason || "animation"));
      // Only re-measure the viewport when something has actually
      // invalidated it. Static frames (the common case during continuous
      // animation without DOM changes) reuse the cached `viewport` and
      // skip the 4 getBoundingClientRect layout flushes that used to
      // run every frame.
      if (viewportDirty) {
        const nextViewport = sceneViewportFromMount(ctx.mount, props, viewportBase, canvas, capability, adaptiveQuality);
        viewport = applySceneViewport(ctx.mount, canvas, labelLayer, nextViewport, viewportBase);
        viewportDirty = false;
      }
      if (!sceneCanRender()) {
        cancelFrame();
        return;
      }
      sceneAdvanceScrollCamera(sceneState._scrollCamera);
      const timeSeconds = now / 1000;
      const modelAnimationDelta = lastModelAnimationTimeSeconds == null
        ? 0
        : Math.max(0, Math.min(0.1, timeSeconds - lastModelAnimationTimeSeconds));
      lastModelAnimationTimeSeconds = timeSeconds;
      if (perfEnabled) performance.mark("scene3d-model-animations-start");
      sceneAdvanceModelAnimations(sceneState, modelAnimationDelta, motion.reducedMotion === true);
      if (perfEnabled) {
        performance.mark("scene3d-model-animations-end");
        performance.measure("scene3d-model-animations", "scene3d-model-animations-start", "scene3d-model-animations-end");
        performance.clearMarks("scene3d-model-animations-start");
        performance.clearMarks("scene3d-model-animations-end");
      }
      if (runtimeScene && ctx.runtime && typeof ctx.runtime.renderFrame === "function") {
        const runtimeBundle = ctx.runtime.renderFrame(timeSeconds, viewport.cssWidth, viewport.cssHeight);
        if (runtimeBundle) {
          const effectiveBundle = sceneBundleWithCameraOverride(
            runtimeBundle,
            sceneCurrentControlCamera(sceneControlHandle.controller, runtimeBundle.camera || sceneState.camera, sceneState._scrollCamera),
          );
          effectiveBundle.waterShaderSourcesByID = mountedWaterShaderSources;
          sceneHydrateBundleWaterShaderSources(effectiveBundle, effectiveBundle.waterShaderSourcesByID);
          latestBundle = effectiveBundle;
          publishMountedSceneCamera(effectiveBundle.camera, reason || "render");
          if (!ensureRendererCanCoverBundle(effectiveBundle)) {
            scheduleNextAnimationFrame();
            return;
          }
          syncSceneNodeSentinels(effectiveBundle);
          renderer.render(effectiveBundle, viewport, createSceneRenderFrameMeta(now));
          recordSceneWaterFrame(ctx.mount, effectiveBundle);
          renderSceneLabels(labelLayer, effectiveBundle, labelLayoutCache, labelElements, viewport.cssWidth, viewport.cssHeight);
          renderSceneSprites(labelLayer, effectiveBundle, spriteElements, viewport.cssWidth, viewport.cssHeight);
          renderSceneHTML(labelLayer, effectiveBundle, htmlElements, viewport.cssWidth, viewport.cssHeight, htmlTextureState);
          if (statsOverlay) {
            statsOverlay.update(effectiveBundle, frameStart, renderer, viewport);
          }
          if (inspectorOverlay) {
            inspectorOverlay.update();
          }
          if (sceneUpdateAdaptiveQuality(adaptiveQuality, ctx.mount, sceneState, viewport, frameStart, now, renderer)) {
            viewportDirty = true;
            scheduleRender("quality-transition");
          }
          publishReady();
          publishRevealed(effectiveBundle);
          notifySceneProgressiveModelRenderCommitted(sceneState);
          scheduleNextAnimationFrame();
          return;
        }
      }
      if (runtimeScene && ctx.runtime) {
        const commandResult = applySceneCommands(sceneState, ctx.runtime.tick());
        notifySceneRendererLifecycle("runtime-tick", false, false);
        if (commandResult && typeof commandResult.then === "function") {
          commandResult.then(function() {
            scheduleRender("runtime-model-commands");
          });
        }
      }
      applyWasmMotionFrame(timeSeconds);
      // C3: write motion-evaluated material uniforms into customUniforms BEFORE
      // the bundle build below, so the next createSceneRenderBundle (and the
      // selena per-frame re-pack) observes them.
      applyWasmMaterialMotionFrame(timeSeconds);
      sceneAdvanceTransitions(sceneState, now);
      // LOD: swap vertex data based on camera distance before building render bundle.
      // sceneApplyLOD lives in the lazily fetched decompress chunk. Resolve it
      // through the API object each frame; the mount awaited the chunk before
      // it built the state, so this lookup finds it.
      var applyLOD = sceneDecompressAPIFunction("sceneApplyLOD");
      if (applyLOD && props.compression && props.compression.lod) {
        var cam = sceneCurrentControlCamera(sceneControlHandle.controller, sceneState.camera, sceneState._scrollCamera);
        var camX = cam.x || 0, camY = cam.y || 0, camZ = cam.z || 0;
        for (var li = 0; li < sceneState.points.length; li++) {
          applyLOD(sceneState.points[li], camX, camY, camZ);
        }
      }
      if (perfEnabled) performance.mark("scene3d-bundle-start");
      const activeCamera = sceneCurrentControlCamera(sceneControlHandle.controller, sceneState.camera, sceneState._scrollCamera);
      applySceneHTMLTextureRecordsToState(sceneState, htmlTextureState);
      const pointQualityGroups = sceneQualityLadderAdmittedGroups(adaptiveQuality);
      const pointBudgetScale = sceneQualityLadderPointBudgetScale(adaptiveQuality);
      const budgetedPoints = sceneApplyPointBudgetScale(
        sceneFilterPointsByQualityGroups(sceneStatePointsWithMaterials(sceneState), pointQualityGroups, sceneState.pointQualityGroups),
        pointBudgetScale,
      );
      const qualityScaledComputeParticles = sceneScaleComputeParticlesByQualityRung(sceneState.computeParticles, adaptiveQuality);
      const computeQualityScale = sceneQualityLadderComputeBudgetScale(adaptiveQuality);
      const computeQualitySourceInstances = sceneComputeParticlesInstanceCount(sceneState.computeParticles);
      const computeQualityActiveInstances = sceneComputeParticlesInstanceCount(qualityScaledComputeParticles);
      latestBundle = createSceneRenderBundle(
        viewport.cssWidth,
        viewport.cssHeight,
        sceneState.background,
        activeCamera,
        sceneFilterObjectsByQualityGroups(sceneStateObjectsWithMaterials(sceneState), sceneQualityLadderAdmittedGroups(adaptiveQuality)),
        sceneStateLabels(sceneState),
        sceneStateSprites(sceneState),
        sceneStateHTML(sceneState),
        sceneStateLights(sceneState),
        sceneState.environment,
        timeSeconds,
        budgetedPoints,
        sceneStateInstancedMeshesWithMaterials(sceneState),
        qualityScaledComputeParticles,
        sceneState.waterSystems,
        sceneState.postEffects,
        sceneState.postFXMaxPixels,
        sceneBool(props && Object.prototype.hasOwnProperty.call(props, "showGrid") ? props.showGrid : (props && props.debugGrid), false),
        { retainedGeometry: Boolean(renderer && renderer.supportsRetainedGeometry === true) },
      );
      latestBundle.waterShaderSourcesByID = mountedWaterShaderSources;
      sceneHydrateBundleWaterShaderSources(latestBundle, latestBundle.waterShaderSourcesByID);
      publishMountedSceneCamera(latestBundle.camera, reason || "render");
      // point-quality-skipped: entries dropped by sceneFilterPointsByQualityGroups
      // this frame (0 when no ladder is active or nothing was tagged). Same
      // filtered bundle.points array both the WebGPU (16a-scene-webgpu.js
      // drawPointsEntries) and WebGL (16-scene-webgl.js drawPointsEntries)
      // backends draw from, so this single attribute covers both.
      setAttrValue(ctx.mount, "data-gosx-scene3d-point-quality-skipped", String(Array.isArray(latestBundle.points) ? (latestBundle.points.qualitySkippedCount || 0) : 0));
      setAttrValue(ctx.mount, "data-gosx-scene3d-point-budget-scale", String(Array.isArray(latestBundle.points) ? (latestBundle.points.qualityPointBudgetScale || 1) : 1));
      setAttrValue(ctx.mount, "data-gosx-scene3d-point-budget-authored-instances", String(Array.isArray(latestBundle.points) ? Math.max(0, latestBundle.points.qualityPointAuthoredInstances || 0) : 0));
      setAttrValue(ctx.mount, "data-gosx-scene3d-point-budget-draw-instances", String(Array.isArray(latestBundle.points) ? Math.max(0, latestBundle.points.qualityPointDrawInstances || 0) : 0));
      setAttrValue(ctx.mount, "data-gosx-scene3d-point-budget-scaled-entries", String(Array.isArray(latestBundle.points) ? Math.max(0, latestBundle.points.qualityPointBudgetScaledEntries || 0) : 0));
      setAttrValue(ctx.mount, "data-gosx-scene3d-compute-quality-scale", String(computeQualityScale));
      setAttrValue(ctx.mount, "data-gosx-scene3d-compute-quality-source-instances", String(computeQualitySourceInstances));
      setAttrValue(ctx.mount, "data-gosx-scene3d-compute-quality-active-instances", String(computeQualityActiveInstances));
      setAttrValue(ctx.mount, "data-gosx-scene3d-compute-quality-reduced-instances",
        String(Math.max(0, computeQualitySourceInstances - computeQualityActiveInstances)));
      if (perfEnabled) {
        performance.mark("scene3d-bundle-end");
        performance.measure("scene3d-bundle", "scene3d-bundle-start", "scene3d-bundle-end");
        performance.clearMarks("scene3d-bundle-start");
        performance.clearMarks("scene3d-bundle-end");
      }
      if (!ensureRendererCanCoverBundle(latestBundle)) {
        scheduleNextAnimationFrame();
        return;
      }
      syncSceneNodeSentinels(latestBundle);
      renderer.render(latestBundle, viewport, createSceneRenderFrameMeta(now));
      recordSceneWaterFrame(ctx.mount, latestBundle);
      maybeEmitRenderEmpty(latestBundle);
      renderSceneLabels(labelLayer, latestBundle, labelLayoutCache, labelElements, viewport.cssWidth, viewport.cssHeight);
      renderSceneSprites(labelLayer, latestBundle, spriteElements, viewport.cssWidth, viewport.cssHeight);
      renderSceneHTML(labelLayer, latestBundle, htmlElements, viewport.cssWidth, viewport.cssHeight, htmlTextureState);
      if (statsOverlay) {
        statsOverlay.update(latestBundle, frameStart, renderer, viewport);
      }
      if (inspectorOverlay) {
        inspectorOverlay.update();
      }
      if (sceneUpdateAdaptiveQuality(adaptiveQuality, ctx.mount, sceneState, viewport, frameStart, now, renderer)) {
        viewportDirty = true;
        scheduleRender("quality-transition");
      }
      publishReady();
      publishRevealed(latestBundle);
      notifySceneProgressiveModelRenderCommitted(sceneState);
      scheduleNextAnimationFrame();
    }

    function maybeEmitRenderEmpty(bundle) {
      if (!sceneRendererRecentlySwapped) {
        return;
      }
      sceneRendererRecentlySwapped = false;
      const reason = sceneRendererLastSwapReason;
      sceneRendererLastSwapReason = "";
      const bundleVerts = Number((bundle && bundle.vertexCount) || 0);
      const worldVerts = Number((bundle && bundle.worldVertexCount) || 0);
      const surfaceCount = Array.isArray(bundle && bundle.surfaces) ? bundle.surfaces.length : 0;
      const bundleMeshObjects = Array.isArray(bundle && bundle.meshObjects) ? bundle.meshObjects.length : 0;
      const bundleInstancedMeshes = Array.isArray(bundle && bundle.instancedMeshes) ? bundle.instancedMeshes.length : 0;
      // A bundle with legacy verts, surfaces, OR a modern PBR mesh/instance list
      // means the renderer had something to draw. Only if ALL paths are empty
      // and sceneState itself has drawable content do we call it render-empty.
      if (bundleVerts > 0 || worldVerts > 0 || surfaceCount > 0
          || bundleMeshObjects > 0 || bundleInstancedMeshes > 0) {
        // Bundle had geometry — schedule a canvas-pixel check next tick to
        // confirm something actually landed on the drawing buffer. Gated by
        // GOSX_TELEMETRY feature flag on the client config so we don't probe
        // on every swap in production unless requested.
        scheduleCanvasBlankProbe(reason, {
          bundleMeshObjects,
          bundleInstancedMeshes,
          bundleVerts: bundleVerts + worldVerts,
        });
        return;
      }
      const pointCount = Array.isArray(sceneState.points) ? sceneState.points.length : 0;
      const objectCount = (sceneState.meshObjects ? sceneState.meshObjects.length : 0)
        + (Array.isArray(sceneState.objects) ? sceneState.objects.length : 0);
      const instanceCount = Array.isArray(sceneState.instancedMeshes) ? sceneState.instancedMeshes.length : 0;
      if (pointCount + objectCount + instanceCount === 0) {
        return;
      }
      gosxSceneEmit("error", "render-empty", {
        rendererKind: renderer && renderer.kind ? renderer.kind : "",
        lastSwapReason: reason,
        scenePoints: pointCount,
        sceneObjects: objectCount,
        sceneInstances: instanceCount,
      });
    }

    // scheduleCanvasBlankProbe: readback-based blank-canvas diagnostics are
    // deliberately opt-in. Canvas serialization/readPixels can force GPU
    // synchronization and has caused context loss in active scenes.
    function scheduleCanvasBlankProbe(reason, stats) {
      if (typeof window === "undefined" || !window.__gosx_telemetry_config
          || window.__gosx_telemetry_config.probeCanvasBlank !== true
          || window.__gosx_telemetry_config.allowCanvasReadbackProbe !== true) {
        return;
      }
      if (typeof window.requestAnimationFrame !== "function") {
        return;
      }
      window.requestAnimationFrame(function () {
        window.requestAnimationFrame(function () {
          if (disposed || !renderer || renderer.kind !== "webgl") {
            return;
          }
          if (typeof canvas.toBlob !== "function") {
            return;
          }
          canvas.toBlob(function (blob) {
            if (disposed || !renderer || renderer.kind !== "webgl") {
              return;
            }
            // PNG threshold: a uniform-color 800x461 PNG is ~400-900 bytes;
            // set the floor generously to avoid false positives on sparse scenes.
            const kCanvasBlankPNGBytesThreshold = 1800;
            const byteSize = blob && typeof blob.size === "number" ? blob.size : 0;
            if (byteSize > kCanvasBlankPNGBytesThreshold) {
              return;
            }
            const gl = typeof canvas.getContext === "function"
              ? (canvas.getContext("webgl2") || canvas.getContext("webgl"))
              : null;
            gosxSceneEmit("error", "render-canvas-blank", {
              rendererKind: renderer && renderer.kind ? renderer.kind : "",
              lastSwapReason: reason || "",
              bundleMeshObjects: stats ? stats.bundleMeshObjects : 0,
              bundleInstancedMeshes: stats ? stats.bundleInstancedMeshes : 0,
              bundleVerts: stats ? stats.bundleVerts : 0,
              canvasPngBytes: byteSize,
              canvasPngThreshold: kCanvasBlankPNGBytesThreshold,
              glError: gl && typeof gl.getError === "function" ? gl.getError() : 0,
            });
          }, "image/png");
        });
      });
    }

    // The handler above is attached at Promise creation, so awaiting here is
    // safe even when loading, instantiation, skin setup, or status listeners
    // fail. The mount continues with the prior committed generation (or no
    // model-derived records on initial hydration).
    await sceneModelHydration;
    scenePrimeInitialTransitions(sceneState, motion.reducedMotion, 0);

    // Defer the first Scene3D render until after a first-paint boundary.
    function scheduleInitialRender() {
      if (disposed) return;
      initHandle = engineFrame(function() {
        initHandle = null;
        if (disposed) return;
        initHandle = engineFrame(function(now) {
          initHandle = null;
          if (disposed) return;
          initPending = false;
          renderFrame(typeof now === "number" ? now : 0, initReason || "");
        });
      });
    }
    scheduleInitialRender();

    // Progressive: upgrade from preview to full resolution after first paint.
    if (sceneDecompressAPIFunction("sceneUpgradeProgressive") && props.compression && props.compression.progressive) {
      scheduleSceneIdleTask(function() {
        var upgrade = sceneDecompressAPIFunction("sceneUpgradeProgressive");
        if (!upgrade) return;
        upgrade(props);
        // Force a re-render with upgraded data
        if (sceneWantsAnimation()) {
          // Animation loop will pick it up
        } else {
          scheduleRender("progressive-upgrade");
        }
      }, sceneCompressionProgressiveDelay(props));
    }

    if (Array.isArray(sceneState._deferredPostEffects) && sceneState._deferredPostEffects.length > 0) {
      scheduleSceneIdleTask(function() {
        sceneState.postEffects = sceneState._deferredPostEffects;
        sceneState._deferredPostEffects = null;
        sceneApplyAdaptivePostFX(sceneState, adaptiveQuality);
        applyScenePostFXState(ctx.mount, sceneState);
        if (domRegionTracker) {
          domRegionTracker.configure(sceneState.postEffects);
        }
        if (sceneWantsAnimation()) {
          // Animation loop will render the upgraded chain.
        } else {
          scheduleRender("deferred-postfx");
        }
      }, sceneDeferredPostFXDelay(props));
    }

    // Scroll-driven camera: scroll input should be visible immediately even
    // when an animated scene already has a frame loop running.
    var scrollHandler = null;
    var visualViewportScrollHandler = null;
    if (sceneState._scrollCamera) {
      sceneState._scrollCamera._progress = 0;
      sceneState._scrollCamera._smoothProgress = 0;
      sceneUpdateScrollCameraMetrics(sceneState._scrollCamera, true);
      scrollHandler = function() {
        sceneUpdateScrollCameraMetrics(sceneState._scrollCamera, false, true);
        scheduleRender("scroll");
      };
      window.addEventListener("scroll", scrollHandler, { passive: true });
      // visualViewport listeners are only meaningful on touch devices where
      // the visual viewport can differ from the layout viewport (mobile URL
      // bar animations, virtual keyboard, pinch-zoom). On desktop browsers
      // the visual viewport tracks the window 1:1 and the listeners just
      // add event-handler overhead — on Firefox specifically they've been
      // observed to contribute to sustained-scroll jank because each fire
      // re-wakes the render loop.
      var isTouchDevice =
        (typeof navigator !== "undefined" && navigator.maxTouchPoints > 0) ||
        ("ontouchstart" in window);
      if (
        isTouchDevice &&
        window.visualViewport &&
        typeof window.visualViewport.addEventListener === "function"
      ) {
        visualViewportScrollHandler = function() {
          sceneUpdateScrollCameraMetrics(sceneState._scrollCamera, true, true);
          scheduleRender("visual-viewport");
        };
        window.visualViewport.addEventListener("scroll", visualViewportScrollHandler, { passive: true });
        window.visualViewport.addEventListener("resize", visualViewportScrollHandler, { passive: true });
      }
      sceneAdvanceScrollCamera(sceneState._scrollCamera);
    }

    function applyProgressiveSceneModels(models) {
      const result = applySceneCommands(sceneState, [{ kind: 10, data: { models } }]);
      applyScenePostFXState(ctx.mount, sceneState);
      if (domRegionTracker) {
        domRegionTracker.configure(sceneState.postEffects);
      }
      publishSceneWaterStateSnapshot(ctx.mount, sceneState);
      publishSceneWaterLifecycleState(ctx.mount, sceneState, lifecycle, false);
      notifySceneRendererLifecycle("progressive-models", false, false);
      if (result && typeof result.then === "function") {
        scheduleRender("progressive-models");
        return result.then(function(outcome) {
          scheduleRender("progressive-models-hydrated");
          return sceneModelHydrationOutcomeDetail(outcome) || outcome;
        });
      }
      scheduleRender("progressive-models");
      return Promise.resolve({
        generation: Math.max(0, Math.floor(sceneNumber(sceneState && sceneState._modelHydrationGeneration, 0))),
        committed: true,
        stale: false,
      });
    }

    function sceneCommandsSetModels(commands) {
      if (!Array.isArray(commands)) return false;
      for (let index = 0; index < commands.length; index += 1) {
        const command = commands[index];
        if (!command || typeof command !== "object") continue;
        if (command.kind === 10) return true;
      }
      return false;
    }

    function scheduleMountedProgressiveModelLifecycle(initialHydration) {
      return scheduleSceneProgressiveModelLifecycle(sceneState, ctx.mount, initialHydration, applyProgressiveSceneModels, {
        canRender: sceneCanRender,
        renderTimeoutMS: sceneNumber(props && props.progressiveModelRenderTimeoutMS, 2000),
        restorePreview(models) {
          sceneState.models = models;
          applySceneCommands(sceneState, [{ kind: 10, data: { models } }]);
          scheduleRender("progressive-models-restore-preview");
        },
      });
    }

    function applyMountedSceneCommands(commands, reason) {
      const setModelsCommands = sceneCommandsSetModels(commands);
      if (setModelsCommands) {
        cancelSceneProgressiveModelLifecycle(sceneState);
      }
      const result = applySceneCommands(sceneState, commands);
      applyScenePostFXState(ctx.mount, sceneState);
      if (domRegionTracker) {
        domRegionTracker.configure(sceneState.postEffects);
      }
      publishSceneWaterStateSnapshot(ctx.mount, sceneState);
      publishSceneWaterLifecycleState(ctx.mount, sceneState, lifecycle, false);
      notifySceneRendererLifecycle(reason || "commands", false, false);
      if (result && typeof result.then === "function") {
        scheduleRender(reason || "commands");
        return result.then(function() {
          scheduleRender((reason || "commands") + "-async");
          if (setModelsCommands) {
            scheduleMountedProgressiveModelLifecycle(result);
          }
          return { applied: true };
        });
      }
      scheduleRender(reason || "commands");
      if (setModelsCommands) {
        scheduleMountedProgressiveModelLifecycle(Promise.resolve({
          generation: Math.max(0, Math.floor(sceneNumber(sceneState && sceneState._modelHydrationGeneration, 0))),
          committed: true,
          stale: false,
        }));
      }
      return Promise.resolve({ applied: true });
    }

    const handle = {
      applyCommands(commands) {
        return applyMountedSceneCommands(commands, "commands");
      },
      getCamera() {
        return currentMountedSceneCamera();
      },
      getTelemetry() {
        return {
          camera: currentMountedSceneCamera(),
          orbit: currentMountedSceneOrbitState(),
          selectionID: lastAppliedSelectionID || "",
          lastPick: latestScenePickDetail || (pickHandle && typeof pickHandle.getSnapshot === "function" ? pickHandle.getSnapshot() : null),
          rendererStats: renderer && typeof renderer.getStats === "function" ? renderer.getStats() : null,
        };
      },
      setCamera(camera) {
        return applyMountedSceneCamera(camera, "handle-camera");
      },
      // updateSceneProps merges a partial props object into the mount's
      // live props in place. Most Scene3D state (models, points, postFX,
      // camera, ...) is already reachable through applyCommands(), which
      // re-derives sceneState from the command payload. A handful of
      // fields are instead read once at mount time into closure-captured
      // derived state — currently just maxDevicePixelRatio/maxPixelRatio,
      // baked into viewportBase — so patching props alone would not take
      // effect. This is the minimal escape hatch for those: it mutates the
      // live props object and, for viewport-affecting keys, recomputes
      // viewportBase and forces a re-measured render. Used by progressive
      // single-engine upgrades (preview -> full in place, no re-mount) to
      // restore the full-quality device pixel ratio after the preview
      // phase intentionally capped it for a cheap first paint.
      //
      // G1: postFXMaxPixels is a second escape-hatch key, live-patched
      // WITHOUT going through applyCommands()'s CommandSetPostEffects path
      // (applyScenePostEffectsCommand in 10-runtime-scene-core.js) — that
      // command rebuilds sceneState.postEffects from a caller-supplied raw
      // effects array, and when the caller only wants to change the pixel
      // cap (no effects payload in hand) it destructively rebuilds an EMPTY
      // effects list, dropping any compiled custom Selena pass source.
      // Writing sceneState.postFXMaxPixels directly, here, leaves
      // sceneState.postEffects completely untouched — non-destructive by
      // construction — and is sufficient on its own: both postfx backends
      // already read sceneState.postFXMaxPixels fresh off the render bundle
      // every frame and resize their offscreen targets when the scaled dims
      // change (createScenePostProcessor.begin in 16-scene-webgl.js;
      // ensureFBOs/getSceneTarget in 16a-scene-webgpu.js).
      updateSceneProps(partial) {
        if (disposed || !partial || typeof partial !== "object") {
          return;
        }
        let touchedViewport = false;
        let touchedPostFXMaxPixels = false;
        for (const key in partial) {
          if (!Object.prototype.hasOwnProperty.call(partial, key)) {
            continue;
          }
          props[key] = partial[key];
          if (key === "maxDevicePixelRatio" || key === "maxPixelRatio") {
            touchedViewport = true;
          } else if (key === "postFXMaxPixels") {
            touchedPostFXMaxPixels = true;
          }
        }
        if (touchedPostFXMaxPixels) {
          const nextPostFXMaxPixels = Math.max(0, Math.floor(sceneNumber(partial.postFXMaxPixels, sceneState.postFXMaxPixels)));
          if (nextPostFXMaxPixels !== sceneState.postFXMaxPixels) {
            sceneState.postFXMaxPixels = nextPostFXMaxPixels;
            applyScenePostFXState(ctx.mount, sceneState);
          }
        }
        if (Object.prototype.hasOwnProperty.call(partial, "postEffects") && domRegionTracker) {
          domRegionTracker.configure(sceneState.postEffects);
        }
        if (touchedViewport) {
          const nextBase = sceneViewportBase(props);
          viewportBase.baseWidth = nextBase.baseWidth;
          viewportBase.baseHeight = nextBase.baseHeight;
          viewportBase.aspectRatio = nextBase.aspectRatio;
          viewportBase.responsive = nextBase.responsive;
          viewportBase.explicitMaxDevicePixelRatio = nextBase.explicitMaxDevicePixelRatio;
          scheduleRenderWithViewport("update-props");
        } else {
          scheduleRender("update-props");
        }
      },
      dispose() {
        disposed = true;
        if (revealSent && revealClass && document.documentElement) {
          document.documentElement.classList.remove(revealClass);
        }
        cancelSceneProgressiveModelLifecycle(sceneState);
        // Supersede any SetModels/initial staging still waiting on I/O before
        // releasing committed records. Its terminal generation check will
        // dispose staged mixers and refuse all late status/scene mutation.
        invalidateSceneModelHydration(sceneState);
        initPending = false;
        if (initHandle != null) {
          cancelEngineFrame(initHandle);
          initHandle = null;
        }
	        if (ctx.mount && typeof ctx.mount.removeEventListener === "function") {
	          ctx.mount.removeEventListener("gosx:scene3d:commands", onMountCommands);
	        }
        handle.__gosxScene3DCommandReady = false;
        publishSceneWaterLifecycleState(ctx.mount, sceneState, lifecycle, true);
        notifySceneRendererLifecycle("dispose", true, true);
        clearIdleContextRelease();
        clearVoluntaryRestoreWatchdog();
        stopSceneRenderWatchdog();
        if (webgpuProbeReadyListener && typeof window !== "undefined" && typeof window.removeEventListener === "function") {
          window.removeEventListener("gosx:scene3d:webgpu-probe-ready", webgpuProbeReadyListener);
          webgpuProbeReadyListener = null;
        }
        if (scrollHandler) {
          window.removeEventListener("scroll", scrollHandler);
        }
        if (visualViewportScrollHandler && window.visualViewport && typeof window.visualViewport.removeEventListener === "function") {
          window.visualViewport.removeEventListener("scroll", visualViewportScrollHandler);
          window.visualViewport.removeEventListener("resize", visualViewportScrollHandler);
        }
	        detachSceneCanvasContextListeners(canvas);
        document.removeEventListener("gosx:hub:event", sceneHubListener);
        if (unsubCameraSignal) unsubCameraSignal();
        if (unsubSelectionSignal) unsubSelectionSignal();
        if (unsubGizmoSignal) unsubGizmoSignal();
        releaseViewportObserver();
        releaseCapabilityObserver();
        releaseLifecycleObserver();
        releaseMotionObserver();
        releaseSceneCSSObserver();
        if (domRegionTracker) {
          domRegionTracker.dispose();
        }
        releaseManagedControlForms();
        releaseTextLayoutListener();
        releaseSceneDebugSurface();
        dragHandle.dispose();
        pickHandle.dispose();
        sceneControlHandle.dispose();
        renderer.dispose();
        disposeSceneHTMLTextureState(htmlTextureState);
        if (typeof releaseTextureLoadListener === "function") {
          releaseTextureLoadListener();
        }
        if (wasmMotionState === 1 && typeof window !== "undefined"
            && typeof window.__gosx_motion_unload === "function") {
          window.__gosx_motion_unload(wasmMotionHandle);
        }
        wasmMotionState = -1;
        wasmMotionHandle = 0;
        // C3: free the material-uniform motion program handle.
        if (wasmMatMotionState === 1 && typeof window !== "undefined"
            && typeof window.__gosx_motion_unload === "function") {
          window.__gosx_motion_unload(wasmMatMotionHandle);
        }
        wasmMatMotionState = -1;
        wasmMatMotionHandle = 0;
        // P4-M3: free any per-model WASM motion mixers created behind the flag.
        sceneDestroyModelWasmMixers(sceneState && sceneState._modelSkins);
        cancelFrame();
        cancelScheduledRender();
        if (pendingMotionHandle != null) {
          cancelEngineFrame(pendingMotionHandle);
          pendingMotionHandle = null;
        }
        if (labelRefreshHandle != null) {
          cancelEngineFrame(labelRefreshHandle);
        }
        if (canvas.parentNode === ctx.mount) {
          ctx.mount.removeChild(canvas);
        }
        if (labelLayer.parentNode === ctx.mount) {
          ctx.mount.removeChild(labelLayer);
        }
        if (statsOverlay) {
          statsOverlay.dispose();
        }
        if (inspectorOverlay) {
          inspectorOverlay.dispose();
        }
        if (sentinelLayer.parentNode) {
          sentinelLayer.parentNode.removeChild(sentinelLayer);
        }
        delete ctx.mount.__gosxScene3DSentinels;
        delete ctx.mount.__gosxScene3DState;
        delete ctx.mount.__gosxScene3DTextureVariantContext;
        delete ctx.mount.__gosxScene3DCSSDynamic;
        delete ctx.mount.__gosxScene3DCSSRevision;
        delete ctx.mount.__gosxScene3DCSSAnimationUntil;
        delete ctx.mount.__gosxScene3DHandle;
        if (typeof ctx.mount.removeAttribute === "function") {
          ctx.mount.removeAttribute("data-gosx-scene3d-command-ready");
          ctx.mount.removeAttribute("data-gosx-scene3d-command-revision");
          ctx.mount.removeAttribute("data-gosx-scene3d-command-applied-revision");
        }
      },
    };
    // Mirror the returned handle directly on the mount element. The
    // window.__gosx.engines registry entry for this engine is written by the
    // GENERIC declarative-engine mounting path AFTER this factory's promise
    // resolves (a separate module, loaded/initialized independently of this
    // Scene3D chunk) — under some load-order timings that registration can
    // lag well behind (or, rarely, never observe) this factory's own
    // completion. Callers that need the handle as soon as this mount is
    // interactive (e.g. an app-level progressive-upgrade script) should
    // prefer reading it from here over window.__gosx.engines.get(id).handle.
    handle.__gosxScene3DCommandReady = true;
    ctx.mount.__gosxScene3DHandle = handle;
    if (typeof ctx.mount.setAttribute === "function") {
      ctx.mount.setAttribute("data-gosx-scene3d-command-ready", "true");
    }
    scheduleMountedProgressiveModelLifecycle(sceneModelHydration);
    return handle;
  });
