// Opt-in zoom shares the controls scheduler; animated scenes keep their frame cap.
interface SceneZoomState { value: number; target: number; min: number; max: number; }
(function() {
  function clamp(value: number, min: number, max: number): number { return Math.max(min, Math.min(max, value)); }
  function create(value: number, min: number, max: number): SceneZoomState {
    return { value: clamp(value, min, max), target: clamp(value, min, max), min, max };
  }
  function change(s: SceneZoomState, delta: number): void {
    if (Number.isFinite(delta)) s.target = clamp(s.target * Math.exp(clamp(delta, -4, 4)), s.min, s.max);
  }
  function advance(s: SceneZoomState, dt: number, reduced: boolean): boolean {
    const before = s.value;
    s.value += (s.target - s.value) * (reduced ? 1 : 1 - Math.exp(-18 * Math.max(0, dt)));
    if (Math.abs(s.target - s.value) < 0.001) s.value = s.target;
    return before !== s.value;
  }
  function wheel(event: WheelEvent, height: number): number {
    return event.deltaY * (event.deltaMode === 1 ? 16 : event.deltaMode === 2 ? height : 1) * (event.ctrlKey ? 0.01 : 0.001);
  }
  function distance(a: { clientX: number; clientY: number }, b: { clientX: number; clientY: number }): number {
    return Math.hypot(a.clientX - b.clientX, a.clientY - b.clientY);
  }
  function pinch(previous: number, next: number): number { return previous > 0 && next > 0 ? Math.log(previous / next) : 0; }
  function scale(fov: number, initial: number): number { return Math.tan(fov * Math.PI / 360) / Math.tan(initial * Math.PI / 360); }
  function setup(canvas: any, props: any, base: any, helpers: any): any {
    if (!props.controlZoom || !base.controller) return base;
    const controller = base.controller, orbit = base.zoomController || controller, read = helpers.read, sync = controller.syncCamera;
    const originalCurrent = controller.currentCamera, originalApply = controller.applyCamera;
    const media = window.matchMedia && window.matchMedia('(prefers-reduced-motion: reduce)');
    const pointers = new Map<number, PointerEvent>(), listeners: Array<[any, string, any, any]> = [];
    let state: SceneZoomState, view = '', initial = 75, frame = 0, last = 0, pinching = false, span = 0, disposed = false;
    const oldAction = canvas.style.touchAction, oldShortcuts = canvas.getAttribute('aria-keyshortcuts');
    canvas.style.touchAction = 'none';
    canvas.setAttribute('aria-keyshortcuts', (oldShortcuts || '') + ' + - = _');
    function select(): void {
      const next = base.state && base.state.mode === 'sailing' ? base.state.cameraMode : orbit.mode;
      if (next === view && state) return;
      view = next; controller.zoomScale = 1;
      if (next === 'orbit') {
        read(); initial = orbit.orbit.radius;
        state = create(initial, orbit.minDistance, orbit.maxDistance);
      } else if (next === 'stern') {
        initial = base.state.length * 1.8;
        state = create(initial, initial * 0.45, initial * 2.5); base.state.followDistance = state.value;
      } else {
        initial = Number(next !== 'wheel' && helpers.fov ? helpers.fov(read()) : read().fov) || 75; state = create(initial, 30, 90);
      }
    }
    function reset(): void { view = ''; select(); }
    base.resetZoom = reset;
    function apply(): void {
      if (view === 'orbit') orbit.orbit.radius = state.value;
      else if (view === 'stern') base.state.followDistance = state.value;
      else controller.zoomScale = scale(state.value, initial);
    }
    function schedule(): void {
      const host = canvas.closest('[data-gosx-scene3d-render-loop]');
      if (!host || host.getAttribute('data-gosx-scene3d-render-loop-wants-animation') !== 'true' || host.getAttribute('data-gosx-scene3d-render-loop') !== 'active') helpers.schedule('controls-zoom');
    }
    function tick(now: number): void {
      frame = 0; if (disposed) return;
      select(); if (advance(state, (now - last) / 1000, !!(media && media.matches))) { apply(); schedule(); }
      last = now;
      if (state.value !== state.target) frame = helpers.requestFrame(tick);
    }
    function zoom(delta: number): void {
      select(); change(state, delta * (Number(props.controlZoomSpeed) || 1)); controller.touched = true; orbit.touched = true;
      if (media && media.matches) { advance(state, 0, true); apply(); schedule(); }
      else if (!frame && state.value !== state.target) { last = helpers.now(); frame = helpers.requestFrame(tick); }
    }
    controller.syncCamera = (camera: any) => { sync(camera); };
    controller.currentCamera = () => {
      select(); const camera = read();
      return view === 'orbit' || view === 'stern' ? camera : Object.assign({}, camera, { fov: state.value, _gosxZoomFOV: true });
    };
    controller.applyCamera = (camera: any) => {
      if (originalApply) originalApply(camera); else helpers.apply(camera);
      reset();
    };
    function listen(target: any, name: string, fn: any, options: any): void {
      target.addEventListener(name, fn, options); listeners.push([target, name, fn, options]);
    }
    listen(canvas, 'wheel', (event: WheelEvent) => { event.preventDefault(); event.stopImmediatePropagation(); zoom(wheel(event, canvas.clientHeight || 800)); }, { passive: false, capture: true });
    listen(canvas, 'keydown', (event: KeyboardEvent) => {
      if (document.activeElement !== canvas) return;
      if (!['+', '=', '-', '_'].includes(event.key)) return;
      event.preventDefault(); event.stopImmediatePropagation(); zoom(event.key === '+' || event.key === '=' ? -0.12 : 0.12);
    }, { capture: true });
    function touch(event: PointerEvent): void {
      if (event.pointerType !== 'touch') return;
      if (event.type === 'pointerdown') {
        pointers.set(event.pointerId, event);
        if (pointers.size === 2) { pinching = true; if (base.cancelTouch) base.cancelTouch(); }
      } else if (event.type === 'pointermove' && pointers.has(event.pointerId)) pointers.set(event.pointerId, event);
      else if (event.type !== 'pointermove') pointers.delete(event.pointerId);
      if (!pinching) return;
      event.preventDefault(); event.stopImmediatePropagation();
      if (event.type === 'pointerdown' && canvas.setPointerCapture) canvas.setPointerCapture(event.pointerId);
      const points = Array.from(pointers.values()), next = points.length === 2 ? distance(points[0], points[1]) : 0;
      if (event.type === 'pointermove' && span && next) zoom(pinch(span, next));
      span = next;
      if (!pointers.size) { pinching = false; span = 0; }
    }
    for (const name of ['pointerdown', 'pointermove', 'pointerup', 'pointercancel']) listen(canvas, name, touch, { passive: false, capture: true });
    listen(window, 'blur', () => { pointers.clear(); pinching = false; span = 0; }, { passive: true });
    if (media && media.addEventListener) listen(media, 'change', () => {
      if (media.matches) { select(); advance(state, 0, true); apply(); schedule(); }
    }, { passive: true });
    const originalReset = base.reset;
    if (originalReset) base.reset = () => { originalReset(); reset(); };
    const dispose = base.dispose;
    base.dispose = () => {
      disposed = true; if (frame) helpers.cancelFrame(frame);
      for (const [target, name, fn, options] of listeners) target.removeEventListener(name, fn, options);
      pointers.clear(); controller.currentCamera = originalCurrent; controller.syncCamera = sync; controller.applyCamera = originalApply;
      controller.zoomScale = 1; canvas.style.touchAction = oldAction;
      if (oldShortcuts == null) canvas.removeAttribute('aria-keyshortcuts'); else canvas.setAttribute('aria-keyshortcuts', oldShortcuts);
      dispose();
    };
    return base;
  }
  window.__gosx_scene3d_zoom_api = { setup, create, change, advance, wheel, distance, pinch, scale };
})();
