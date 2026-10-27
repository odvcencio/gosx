// Cutaway ("dollhouse") view of a vessel: hide or ghost everything above a deck,
// optionally slice the hull with a plane. The renderer does the clipping; this
// module only keeps two world-space planes up to date as the ship moves.
//
// Contract with a renderer (WebGL and WebGPU implement it):
//  - object.cutaway is a Float32Array(12): three vec4. [0..3] decks-above plane,
//    [4..7] hull-section plane, [8] fade (0 hides, else the share of pixels kept).
//    A point p is cut when dot(plane.xyz, p) + plane.w > 0. All zeros cuts nothing.
//  - object.cutawayCrowd is the same array's first 8 floats. A crowd parented to
//    this vessel culls any agent whose origin is on the cut side of either plane.
//  - Add the backend name to BACKENDS once its shaders honor both.
(function() {
  const BACKENDS = ['webgl', 'webgpu'], controllers = new Map();
  // @ts-ignore TS7006 -- this governed module is also evaluated as plain JS in Node tests.
  const finite = v => typeof v === 'number' && Number.isFinite(v);
  // @ts-ignore TS7006 -- this governed module is also evaluated as plain JS in Node tests.
  function normalize(c, count) {
    if (!c || typeof c !== 'object') return null;
    const out = {enabled: c.enabled === true, deck: Math.max(0, Math.min(count - 1, Math.floor(finite(c.deck) ? c.deck : count - 1))), height: finite(c.height) ? c.height : NaN,
      fade: c.above === 'fade' ? (finite(c.fadeAlpha) && c.fadeAlpha > 0 ? Math.min(1, c.fadeAlpha) : .15) : 0, section: {on: false, axis: 'x', position: 0, side: 1}};
    const s = c.section;
    if (s && (s.axis === 'x' || s.axis === 'z') && finite(s.position) && (s.side === 1 || s.side === -1)) out.section = {on: true, axis: s.axis, position: s.position, side: s.side};
    return out;
  }
  // @ts-ignore TS7006 -- this governed module is also evaluated as plain JS in Node tests.
  function create(decks, mount, schedule, initial) {
    const cut = new Float32Array(12), crowd = cut.subarray(0, 8), saved = new WeakMap();
    let state = normalize({}, 1), ready = '';
    // @ts-ignore TS7006 -- plain JS parameters are exercised without transpilation.
    function set(next) {
      const n = normalize(next, decks.list.length);
      if (!n) return false;
      state = n;ready = '';
      if (schedule) schedule('vessel-cutaway');
      return true;
    }
    function supported() {
      const backend = mount.getAttribute('data-gosx-scene3d-backend');
      return !backend || BACKENDS.includes(backend);
    }
    // Ship-local height of the cut for the chosen deck: the ceiling of that deck.
    function height() {
      if (finite(state.height)) return state.height;
      // @ts-ignore TS7006 -- plain JS parameters are exercised without transpilation.
      const i = decks.list.findIndex(d => d.index === state.deck), here = decks.list[i], above = decks.list[i + 1];
      return here ? (above ? above.y : here.y + 3.3) - .35 : Infinity;
    }
    // Recomputes the planes from the ship pose (decks.update must have run).
    // @ts-ignore TS7006 -- plain JS parameters are exercised without transpilation.
    function update(frame) {
      if (!state.enabled || !supported()) {
        if (ready === 'off') return;
        cut.fill(0);ready = 'off';
        mount.setAttribute('data-gosx-scene3d-cutaway', state.enabled ? 'unsupported' : 'off');
        return;
      }
      const m = frame.axes, o = frame.origin, h = height();
      // Decks above: n is the ship's up axis, so the cut tilts with roll and pitch.
      cut[0] = m[3];cut[1] = m[4];cut[2] = m[5];cut[3] = -(m[3] * o.x + m[4] * o.y + m[5] * o.z) - h;
      cut[4] = cut[5] = cut[6] = cut[7] = 0;
      if (state.section.on) {
        const s = state.section, k = s.axis === 'x' ? 0 : 6;
        cut[4] = s.side * m[k];cut[5] = s.side * m[k + 1];cut[6] = s.side * m[k + 2];
        cut[7] = -s.side * (m[k] * o.x + m[k + 1] * o.y + m[k + 2] * o.z) - s.side * s.position;
      }
      if (!finite(h)) cut.fill(0, 0, 4);
      cut[8] = state.fade;cut[9] = cut[10] = cut[11] = 0;
      if (ready !== 'on') {mount.setAttribute('data-gosx-scene3d-cutaway', 'on');ready = 'on';}
    }
    // Applies the current cutaway to one model part. Called for every part of the ship each frame.
    // @ts-ignore TS7006 -- plain JS parameters are exercised without transpilation.
    function touch(object) {
      const on = state.enabled && supported();
      if (on) {
        if (!saved.has(object)) saved.set(object, object.castShadow);
        // The clipped parts must not still shadow the decks that stay visible.
        object.cutaway = cut;object.cutawayCrowd = crowd;object.castShadow = false;
      } else if (saved.has(object)) {
        object.castShadow = saved.get(object);saved.delete(object);object.cutaway = object.cutawayCrowd = null;
      }
    }
    if (initial) set(initial);
    return {set, update, touch, cut, crowd, get state() {return state;}};
  }
  // @ts-ignore TS7006 -- plain JS parameters are exercised without transpilation.
  const key = (mountId, nodeId) => mountId + '\n' + nodeId;
  // @ts-ignore TS7006 -- plain JS parameters are exercised without transpilation.
  function register(mountId, nodeId, controller) {controllers.set(key(mountId, nodeId), controller);}
  // @ts-ignore TS7006 -- plain JS parameters are exercised without transpilation.
  function unregister(mountId, nodeId, controller) {if (controllers.get(key(mountId, nodeId)) === controller) controllers.delete(key(mountId, nodeId));}
  // Go calls this with the cutaway as a JSON string. True when a vessel took it.
  // @ts-ignore TS7006 -- plain JS parameters are exercised without transpilation.
  function dispatch(target, nodeId, config) {
    const c = controllers.get(key(target && typeof target === 'object' ? target.id : target, nodeId));
    if (!c) return false;
    return c.set(typeof config === 'string' ? JSON.parse(config) : config);
  }
  window.__gosx_scene3d_vessel_cutaway_api = {create, normalize, register, unregister, dispatch, BACKENDS};
  window.__gosx_scene3d_vessel_cutaway = dispatch;
})();
