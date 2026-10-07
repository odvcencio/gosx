// Event-owned bursts reuse the existing CPU/WebGL and GPU particle renderers.
interface SceneParticleBurstSpec {
  version: number;
  id: string;
  delay: number;
  duration: number;
  particles: any;
}

(() => {
  const mounts = new WeakMap<object, any>();

  function prepare(spec: SceneParticleBurstSpec): SceneParticleBurstSpec {
    if (!spec || spec.version !== 1 || typeof spec.id !== 'string' || !spec.id.trim() || spec.id.trim() !== spec.id ||
        !Number.isFinite(spec.delay) || spec.delay < 0 || spec.delay > 60 || !Number.isFinite(spec.duration)) throw new TypeError('invalid Scene3D particle burst');
    const p = spec.particles, emitter = p?.emitter, material = p?.material;
    if (!p || !Number.isInteger(p.count) || p.count < 1 || p.count > 4096 || p.id !== 'gosx-burst/' + spec.id ||
        !emitter || emitter.once !== true || !['point', 'sphere', 'disc', 'spiral'].includes(emitter.kind) ||
        !Number.isFinite(emitter.lifetime) || emitter.lifetime <= 0 || emitter.lifetime > 10 || !material ||
        (p.forces !== undefined && !Array.isArray(p.forces)) || (p.forces || []).length > 8) throw new TypeError('invalid Scene3D burst particles');
    const duration = spec.delay + Math.max(.08, emitter.lifetime * .12) + emitter.lifetime * 1.56;
    if (Math.abs(spec.duration - duration) > 1e-8) throw new TypeError('invalid Scene3D burst deadline');
    for (const value of [p.bounds ?? 0, ...['x', 'y', 'z', 'rotationX', 'rotationY', 'rotationZ', 'spinX', 'spinY', 'spinZ', 'radius', 'rate', 'wind', 'scatter'].map(k => emitter[k] ?? 0),
      ...['size', 'sizeEnd', 'opacity', 'opacityEnd', 'minPixelSize', 'maxPixelSize'].map(k => material[k] ?? 0)]) {
      if (!Number.isFinite(value)) throw new TypeError('non-finite Scene3D burst value');
    }
    for (const force of p.forces || []) {
      if (!force || typeof force.kind !== 'string' || !force.kind.trim() || !['strength', 'x', 'y', 'z', 'frequency'].every(k => Number.isFinite(force[k] ?? 0))) throw new TypeError('invalid Scene3D burst force');
    }
    if ((p.bounds ?? 0) < 0 || (emitter.radius ?? 0) < 0 || ['size', 'sizeEnd', 'minPixelSize', 'maxPixelSize'].some(k => (material[k] ?? 0) < 0) ||
        ['opacity', 'opacityEnd'].some(k => (material[k] ?? 0) < 0 || (material[k] ?? 0) > 1)) throw new TypeError('invalid Scene3D burst appearance');
    // Keep a private plan so caller edits cannot change active rendering.
    return { ...spec, particles: JSON.parse(JSON.stringify(p)) };
  }

  function attach(spec: SceneParticleBurstSpec, mount: any, handle: any, alive: () => boolean) {
    const plan = prepare(spec);
    if (!alive() || !mount.isConnected || !mount.__gosxScene3DState) throw new Error('disposed');
    const backend = mount.getAttribute?.('data-gosx-scene3d-backend');
    if (backend && !['webgl', 'webgpu'].includes(backend)) throw new Error('Scene3D particle bursts require WebGL or WebGPU');
    let rec = mounts.get(handle);
    if (!rec) {
      const state = mount.__gosxScene3DState, originalApply = handle.applyCommands, originalDispose = handle.dispose;
      rec = { state, originalApply, originalDispose, bursts: new Map(), owned: new Set(), generation: 0, sequence: 0, closed: false, observer: null, waiters: [] };
      rec.apply = (commands: { kind: number }[]) => {
        if (Array.isArray(commands) && commands.some(command => command.kind === 6)) {
          rec.generation++;
          for (const burst of Array.from(rec.bursts.values()) as any[]) burst.stop('commands', false);
          rec.owned.clear();
        }
        return originalApply.call(handle, commands);
      };
      rec.dispose = () => { dispose(handle); return originalDispose?.call(handle); };
      if (typeof MutationObserver !== 'undefined') {
        rec.observer = new MutationObserver(() => { if (!alive() || !mount.isConnected) dispose(handle); });
        rec.observer.observe(mount, { attributes: true, attributeFilter: ['data-gosx-scene3d-command-ready'] });
        if (mount.parentNode) rec.observer.observe(mount.parentNode, { childList: true });
      }
      mounts.set(handle, rec); handle.applyCommands = rec.apply; handle.dispose = rec.dispose;
    }
    const previous = rec.bursts.get(plan.id);
    const total = (Array.from(rec.bursts.values()) as any[]).reduce((sum: number, burst: any) => sum + burst.plan.particles.count, 0) - (previous?.plan.particles.count || 0) + plan.particles.count;
    if (total > 4096 || (!previous && rec.bursts.size >= 16)) throw new RangeError('Scene3D burst capacity exceeded');
    previous?.stop('replaced', true);
    // A repeated event must create fresh renderer state, even with identical parameters.
    plan.particles.id += '/' + (++rec.sequence);
    const scheduler = window.__gosx.motion.scheduler;
    const media = typeof window.matchMedia === 'function' ? window.matchMedia('(prefers-reduced-motion: reduce)') : null;
    let elapsed = 0, ended = false, started = false, stopTick: (() => void) | null = null;
    let resolveFinished: (result: object) => void, rejectFinished: (error: unknown) => void;
    const finished = new Promise<object>((resolve, reject) => { resolveFinished = resolve; rejectFinished = reject; });
    function cleanup() {
      stopTick?.(); stopTick = null;
      media?.removeEventListener?.('change', reduced);
      if (!media?.removeEventListener) media?.removeListener?.(reduced);
    }
    function write(done?: () => void) {
      if (done) rec.waiters.push({ resolve: done, reject: rejectFinished });
      const generation = rec.generation;
      scheduler.queueWrite(rec, () => {
        const waiters = rec.waiters.splice(0);
        if (rec.closed || rec.generation !== generation || !alive() || !mount.isConnected) { for (const waiter of waiters) waiter.resolve(); return; }
        const compute = (rec.state.computeParticles || []).filter((entry: any) => !rec.owned.has(entry.id));
        const owned = new Set();
        for (const burst of rec.bursts.values()) {
          owned.add(burst.plan.particles.id);
          if (burst.started()) compute.push(burst.plan.particles);
        }
        const commands = [{ kind: 6, data: { points: rec.state.points || [], computeParticles: compute, waterSystems: rec.state.waterSystems || [] } }];
        try {
          const applied = rec.originalApply.call(handle, commands);
          rec.owned = owned;
          Promise.resolve(applied).then(() => { for (const waiter of waiters) waiter.resolve(); }, error => failure(error, waiters));
        } catch (error) { failure(error, waiters); }
      });
    }
    function failure(error: unknown, waiters: any[]) {
      for (const waiter of waiters) waiter.reject(error);
      for (const burst of Array.from(rec.bursts.values()) as any[]) burst.fail(error);
    }
    function fail(error: unknown) {
      if (ended) return;
      ended = true; cleanup(); rec.bursts.delete(plan.id); rejectFinished(error);
    }
    function end(reason: string, remove: boolean, completed = false) {
      if (ended) return;
      ended = true; cleanup();
      if (rec.bursts.get(plan.id) === control) rec.bursts.delete(plan.id);
      const done = () => resolveFinished(completed ? { finished: true, suppressed: reason === 'reduced-motion' } : { finished: false, reason });
      if (remove) write(done); else done();
    }
    function reduced() { if (media?.matches) end('reduced-motion', true, true); }
    const control = { plan, finished, started: () => started, stop: end, fail, cancel: () => end('cancelled', true) };
    rec.bursts.set(plan.id, control); rec.owned.add(plan.particles.id);
    if (media?.addEventListener) media.addEventListener('change', reduced); else media?.addListener?.(reduced);
    if (media?.matches) reduced();
    else {
      if (plan.delay === 0) { started = true; write(); }
      stopTick = scheduler.addContinuous((_now: number, delta: number) => {
        if (ended) return false;
        if (!alive() || !mount.isConnected) { end('disposed', false); return false; }
        elapsed += Math.max(0, Math.min(.1, delta));
        if (!started && elapsed >= plan.delay) { started = true; write(); }
        if (elapsed >= plan.duration) { end('finished', true, true); return false; }
        return true;
      });
    }
    return { finished, cancel: control.cancel };
  }

  function dispose(handle: any) {
    const rec = mounts.get(handle);
    if (!rec) return;
    rec.closed = true;
    for (const burst of Array.from(rec.bursts.values()) as any[]) burst.stop('disposed', false);
    rec.observer?.disconnect();
    if (handle.applyCommands === rec.apply) handle.applyCommands = rec.originalApply;
    if (handle.dispose === rec.dispose) handle.dispose = rec.originalDispose;
    mounts.delete(handle);
  }
  window.__gosx_scene3d_particle_burst_api = { attach, dispose, prepare };
})();
