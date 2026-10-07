// presentation.ts — demand-loaded timeline and particle-burst coordination.
// Ordinary command and binary-frame scenes do not load this authority.
// @ts-check

(function() {
  if (typeof window === "undefined" || window.__gosx_scene3d_presentation_api) return;

  function key(target: any, options: any) {
    if (options && typeof options.engineID === "string" && options.engineID.trim()) return options.engineID.trim();
    if (typeof target === "string" && target.trim()) return target.trim();
    if (target && typeof target.id === "string" && target.id.trim()) return target.id.trim();
    return "";
  }

  function ready(handle: any) {
    return Boolean(handle && handle.__gosxScene3DCommandReady === true && typeof handle.applyCommands === "function");
  }

  function record(target: any, options: any) {
    if (ready(target)) return { handle: target, mount: null };
    if (target && ready(target.__gosxScene3DHandle)) return { handle: target.__gosxScene3DHandle, mount: target };
    var id = key(target, options || {});
    var mount: any = id && document && typeof document.getElementById === "function" ? document.getElementById(id) : null;
    if (mount && ready(mount.__gosxScene3DHandle)) return { handle: mount.__gosxScene3DHandle, mount: mount };
    var engine = id && window.__gosx && window.__gosx.engines && typeof window.__gosx.engines.get === "function" ? window.__gosx.engines.get(id) : null;
    return engine && ready(engine.handle) ? { handle: engine.handle, mount: engine.mount || mount || null } : null;
  }

  function withReadyRecord<T>(target: unknown, opts: any, kind: string, applyReady: (rec: any) => T | PromiseLike<T>, timeoutMS: number): Promise<T> {
    const id = key(target, opts);
    const deadline = Date.now() + timeoutMS;
    return new Promise((resolve, reject) => {
      function poll() {
        try {
          const rec = record(target, opts);
          if (rec) { resolve(applyReady(rec)); return; }
        } catch (error) { reject(error); return; }
        if (!id) return reject(new Error("Scene3D " + kind + " target is not ready and has no stable id"));
        if (Date.now() >= deadline) return reject(new Error("Scene3D " + kind + " target did not become ready: " + id));
        setTimeout(poll, 16);
      }
      poll();
    });
  }

  type PresentationName = "timeline" | "burst";
  type PresentationOptions = { engineID?: string; timeoutMS?: number };
  type PresentationAPI = { attach(value: unknown, mount: any, handle: any, ownsMount: () => boolean): unknown };
  type PresentationFeature = { method: string; chunk: string; datasetKey: string; prepare?: () => unknown };
  const presentations: Record<PresentationName, PresentationFeature> = {
    timeline: { method: "playTimeline", chunk: "timeline", datasetKey: "gosxScene3dTimelineUrl" },
    burst: {
      method: "burstParticles", chunk: "particle-burst", datasetKey: "gosxScene3dParticleBurstUrl",
      prepare: () => window.__gosx_ensure_scene3d_compute_loaded(),
    },
  };

  function loadPresentation(name: PresentationName): Promise<PresentationAPI> {
    const feature = presentations[name];
    return window.__gosx_scene3d_api.ensureFeatureLoaded(feature.chunk, feature.datasetKey, "")
      .then((api: PresentationAPI) => Promise.resolve(feature.prepare && feature.prepare()).then(() => api));
  }

  // Optional presentations share readiness and ownership. Their chunks retain
  // their own playback, cancellation and scheduler lifecycle implementations.
  function playPresentation(name: PresentationName, target: unknown, value: unknown, options?: PresentationOptions) {
    const opts = options || {}, method = presentations[name].method;
    const timeout = opts.timeoutMS ?? 10000;
    if (!Number.isFinite(timeout)) return Promise.reject(new TypeError("Scene3D " + name + " timeout must be finite"));
    return withReadyRecord(target, opts, name, rec => {
      // Custom ready handles may implement presentations without a mount.
      if (!rec.mount && typeof rec.handle[method] === "function") {
        return Promise.resolve().then(() => rec.handle[method](value));
      }
      const mount = rec.mount || Array.from(document.querySelectorAll('[data-gosx-scene3d-command-ready]')).find(candidate => Reflect.get(candidate, "__gosxScene3DHandle") === rec.handle);
      if (!mount) throw new Error("Scene3D " + name + " mount is unavailable");
      return loadPresentation(name).then(api => api.attach(value, mount, rec.handle, () => mount.__gosxScene3DHandle === rec.handle));
    }, Math.max(0, timeout));
  }

  function playTimeline(target: unknown, timeline: unknown, options?: PresentationOptions) {
    return playPresentation("timeline", target, timeline, options);
  }

  function burstParticles(target: unknown, burst: unknown, options?: PresentationOptions) {
    return playPresentation("burst", target, burst, options);
  }

  window.__gosx_scene3d_presentation_api = { playTimeline, burstParticles };
})();
