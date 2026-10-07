// Finite Scene3D choreography. Loaded only on the first playTimeline call.
interface SceneTweenSpec {
  node?: string;
  camera?: boolean;
  property: string;
  from: number;
  to: number;
  at: number;
  duration: number;
  ease?: { kind: number; args?: number[] };
}
interface SceneTimelineSpec { version: number; id: string; tweens: SceneTweenSpec[] }
interface SceneTimelineContext {
  apply(commands: object[]): unknown;
  alive(): boolean;
  replace(): void;
}

(() => {
  const nodeProperties = new Set(['x', 'y', 'z', 'rotationX', 'rotationY', 'rotationZ', 'scaleX', 'scaleY', 'scaleZ', 'opacity']);
  const cameraProperties = new Set(['x', 'y', 'z', 'rotationX', 'rotationY', 'rotationZ', 'fov']);
  const key = (t: SceneTweenSpec) => JSON.stringify([!!t.camera, t.node || '', t.property]);
  const mounted = new WeakMap<object, any>();
  const attachments = new WeakMap<object, any>();

  function prepare(spec: SceneTimelineSpec): SceneTweenSpec[] {
    if (!spec || spec.version !== 1 || typeof spec.id !== 'string' || !spec.id || !Array.isArray(spec.tweens) || !spec.tweens.length || spec.tweens.length > 1024) throw new TypeError('invalid Scene3D timeline');
    const tracks = spec.tweens.map(t => {
      if (!t || (t.camera !== undefined && typeof t.camera !== 'boolean') ||
          (t.camera ? !cameraProperties.has(t.property) || !!t.node : !nodeProperties.has(t.property) || typeof t.node !== 'string' || !t.node) ||
          ![t.from, t.to, t.at, t.duration].every(Number.isFinite) || t.at < 0 || t.duration < 0 || t.at + t.duration > 86400) throw new TypeError('invalid Scene3D tween');
      const ease = t.ease || { kind: 0 }, args = ease.args || [];
      if (!Number.isInteger(ease.kind) || ease.kind < 0 || ease.kind > 8 || !Array.isArray(args) || !args.every(Number.isFinite) ||
          ([1, 2, 3, 5].includes(ease.kind) && args.length > 0 && args[0] <= 0) ||
          (ease.kind === 4 && (args.length !== 4 || args[0] < 0 || args[0] > 1 || args[2] < 0 || args[2] > 1))) throw new TypeError('invalid Scene3D tween easing');
      return { ...t, ease: { kind: ease.kind, args: args.slice() } };
    }).sort((a, b) => a.at - b.at);
    const ends = new Map<string, { at: number; end: number }>();
    for (const t of tracks) {
      const previous = ends.get(key(t));
      if (previous && (previous.at === t.at || previous.end > t.at)) throw new TypeError('overlapping Scene3D tweens');
      ends.set(key(t), { at: t.at, end: t.at + t.duration });
    }
    return tracks;
  }

  function sample(tracks: SceneTweenSpec[], seconds: number): object[] {
    const patches = new Map<string, { kind: number; objectId?: string; data: Record<string, number> }>();
    const seen = new Set<string>();
    for (const t of tracks) {
      const propertyKey = key(t);
      if (seconds < t.at && seen.has(propertyKey)) continue;
      seen.add(propertyKey);
      const progress = seconds < t.at ? 0 : t.duration === 0 ? 1 : (seconds - t.at) / t.duration;
      const value = t.from + (t.to - t.from) * window.__gosx.motion.ease(progress, t.ease);
      if (!Number.isFinite(value)) throw new RangeError('Scene3D tween sample overflowed');
      const target = t.camera ? 'camera:' : 'node:' + t.node;
      let patch = patches.get(target);
      if (!patch) {
        patch = { kind: t.camera ? 5 : 2, data: {} };
        if (!t.camera) patch.objectId = t.node;
        patches.set(target, patch);
      }
      patch.data[t.property] = value;
    }
    return Array.from(patches.values());
  }

  function play(spec: SceneTimelineSpec, context: SceneTimelineContext) {
    const tracks = prepare(spec), duration = Math.max(...tracks.map(t => t.at + t.duration));
    const scheduler = window.__gosx.motion.scheduler;
    const media = typeof window.matchMedia === 'function' ? window.matchMedia('(prefers-reduced-motion: reduce)') : null;
    let seconds = 0, paused = false, ended = false, cancelled = false, finishing = false, stop: (() => void) | null = null;
    let resolveFinished: (result: object) => void, rejectFinished: (error: unknown) => void;
    const finished = new Promise<object>((resolve, reject) => { resolveFinished = resolve; rejectFinished = reject; });

    function cleanup() {
      if (stop) stop();
      stop = null;
      if (media?.removeEventListener) media.removeEventListener('change', reduced);
      else if (media?.removeListener) media.removeListener(reduced);
    }
    function cancel(reason = 'cancelled') {
      if (ended) return;
      cancelled = ended = true;
      cleanup();
      resolveFinished({ finished: false, reason });
    }
    function write(final = false) {
      let commands: object[];
      try { commands = sample(tracks, seconds); } catch (error) { fail(error); return; }
      scheduler.queueWrite(control, () => {
        if (cancelled) return;
        if (!context.alive()) { cancel('disposed'); return; }
        try {
          const applied = context.apply(commands);
          Promise.resolve(applied).then(() => {
            if (final && !ended) { ended = true; cleanup(); resolveFinished({ finished: true }); }
          }, fail);
        } catch (error) { fail(error); }
      });
    }
    function fail(error: unknown) {
      if (ended) return;
      ended = cancelled = true; cleanup(); rejectFinished(error);
    }
    function finish() {
      if (ended || finishing) return;
      finishing = true;
      seconds = duration; paused = true;
      if (stop) stop(); stop = null;
      write(true);
    }
    function reduced() { if (media?.matches) finish(); }
    function tick(_now: number, delta: number) {
      if (ended || paused) return false;
      if (!context.alive()) { cancel('disposed'); return false; }
      seconds = Math.min(duration, seconds + Math.max(0, Math.min(0.1, delta)));
      if (seconds >= duration) { finish(); return false; }
      write(); return true;
    }
    function resume() {
      if (ended || finishing || !paused) return;
      if (media?.matches || seconds >= duration) { finish(); return; }
      paused = false; stop = scheduler.addContinuous(tick);
    }
    const control = {
      finished, duration,
      get timeSeconds() { return seconds; },
      get paused() { return paused; },
      pause() { if (ended) return; paused = true; if (stop) stop(); stop = null; },
      resume,
      seek(value: number) {
        if (!Number.isFinite(value) || value < 0 || value > duration) throw new RangeError('timeline seek is outside its duration');
        if (ended || finishing) return;
        seconds = value;
        if (media?.matches || seconds >= duration) finish(); else write();
      },
      finish, cancel,
    };
    // Validate the entire replacement before interrupting the previous playback.
    context.replace();
    if (media?.addEventListener) media.addEventListener('change', reduced);
    else if (media?.addListener) media.addListener(reduced);
    if (media?.matches || duration === 0) finish();
    else { write(); stop = scheduler.addContinuous(tick); }
    return control;
  }

  function clear(state: any, handle: object, commands: { kind: number }[]) {
    if (!Array.isArray(commands) || !commands.some(command => [0, 1, 2, 5, 8, 10, 11, 12].includes(command.kind))) return;
    const playback = mounted.get(handle);
    if (playback) playback.cancel('commands');
    state._gosxMotionRuntimeBindings?.delete('timeline');
  }

  // Shared Go/WASM render bundles receive the same bindings as JS scene state.
  function playMounted(spec: SceneTimelineSpec, state: any, handle: object, apply: (state: any, commands: any[]) => unknown,
    controls: any, applyCamera: (controls: any, camera: any) => void, invalidate: (reason: string) => void, alive: () => boolean) {
    if (!alive()) throw new Error('disposed');
    const previous = mounted.get(handle);
    let installed: Map<string, any> | null = null;
    const playback = play(spec, { alive, replace: () => { if (previous) previous.cancel('replaced'); }, apply: commands => {
      const layers = state._gosxMotionRuntimeBindings || (state._gosxMotionRuntimeBindings = new Map());
      const entries = new Map();
      for (const command of commands as { kind: number; objectId?: string; data: Record<string, number> }[]) {
        for (const [property, value] of Object.entries(command.data)) {
          const binding = { target: command.kind === 5 ? 'camera' : 'sceneNode', node: command.objectId || '', property };
          const id = JSON.stringify([binding.target, binding.node, property]);
          entries.set(id, { binding, value });
        }
      }
      layers.set('timeline', entries);
      installed = entries;
      const result = apply(state, commands);
      if ((commands as { kind: number }[]).some(command => command.kind === 5)) applyCamera(controls, state.camera);
      invalidate('timeline');
      return result;
    } });
    // Camera controls own the settled pose; release the camera binding so a
    // subsequent user orbit/pan can move it without a persistent override.
    const releaseCamera = () => {
      if (!installed || state._gosxMotionRuntimeBindings?.get('timeline') !== installed) return;
      for (const [key, entry] of installed) if (entry.binding.target === 'camera') installed.delete(key);
    };
    playback.finished.then(releaseCamera, releaseCamera);
    mounted.set(handle, playback);
    return playback;
  }

  function dispose(handle: object) {
    mounted.get(handle)?.cancel('disposed'); mounted.delete(handle);
    const attachment = attachments.get(handle);
    if (!attachment) return;
    attachment.observer?.disconnect();
    if ((handle as any).applyCommands === attachment.apply) (handle as any).applyCommands = attachment.originalApply;
    if ((handle as any).dispose === attachment.dispose) (handle as any).dispose = attachment.originalDispose;
    attachments.delete(handle);
  }

  // Install the command and disposal adapters only when choreography is used.
  function attach(spec: SceneTimelineSpec, mount: any, handle: any, alive: () => boolean) {
    prepare(spec);
    if (!alive() || !mount?.__gosxScene3DState) throw new Error('disposed');
    const state = mount.__gosxScene3DState;
    let attachment = attachments.get(handle);
    if (!attachment) {
      const originalApply = handle.applyCommands, originalDispose = handle.dispose;
      const apply = (commands: { kind: number }[]) => { clear(state, handle, commands); return originalApply.call(handle, commands); };
      const teardown = () => { dispose(handle); return originalDispose?.call(handle); };
      attachment = { apply, dispose: teardown, originalApply, originalDispose, observer: null };
      if (typeof MutationObserver !== 'undefined') {
        attachment.observer = new MutationObserver(() => { if (!alive() || !mount.isConnected) dispose(handle); });
        attachment.observer.observe(mount, { attributes: true, attributeFilter: ['data-gosx-scene3d-command-ready'] });
        if (mount.parentNode) attachment.observer.observe(mount.parentNode, { childList: true });
      }
      attachments.set(handle, attachment);
      handle.applyCommands = apply; handle.dispose = teardown;
    }
    return playMounted(spec, state, handle, (_state, commands) => attachment.originalApply.call(handle, commands),
      null, (_controls, camera) => handle.setCamera(camera), () => {}, () => alive() && mount.isConnected);
  }

  window.__gosx_scene3d_timeline_api = { play, playMounted, attach, clear, dispose, sample: (spec: SceneTimelineSpec, seconds: number) => {
    if (!Number.isFinite(seconds)) throw new TypeError('timeline sample time must be finite');
    return sample(prepare(spec), seconds);
  } };
})();
