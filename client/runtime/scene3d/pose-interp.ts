// pose-interp.ts — page-side interpolation of timed pose frames.
// @ts-check
//
// A simulation that runs off the main thread (a Web Worker engine) cannot
// write poses inside the renderer's frame. It sends one GSP2 pose frame per
// simulation tick instead, with options.at (the simulation time the poses
// hold, in ms) and options.step (the tick length, in ms); see
// scene.DispatchTimedPoseFrame. This module keeps the newest frames per
// mount and installs the mount's frame hook (frame-owner.ts). Before every
// frame the mount draws, the hook applies the poses interpolated at
//
//   renderAt = frameTime - offset - delay
//
// where offset is the smallest (arrival - at) seen over the last 60 frames,
// which maps simulation time onto the page clock without trusting any one
// late message, and delay is one step plus 2 ms (options.delay overrides
// it). Position and scale blend linearly, each rotation angle along its
// shortest arc, and the clip time linearly while the clip stays the same.
// Instances pair by position inside a batch with the same ID.
//
// Steady state allocates nothing per drawn frame: the blended batches are
// reused. Decoding a frame on arrival allocates, once per tick.
(function() {
  if (typeof window === "undefined" || window.__gosx_scene3d_pose_interpolation) return;
  var LINEAR = ["x", "y", "z", "scaleX", "scaleY", "scaleZ"];
  var ANGLES = ["rotationX", "rotationY", "rotationZ"];
  var KEEP = 8, WINDOW = 60, TAU = Math.PI * 2;

  function nowMS() {
    return typeof performance !== "undefined" && typeof performance.now === "function" ? performance.now() : Date.now();
  }

  function angleLerp(a: number, b: number, t: number) {
    var d = b - a;
    d -= TAU * Math.round(d / TAU);
    return a + d * t;
  }

  function copyInstance(o: any, q: any) {
    for (var k = 0; k < LINEAR.length; k++) o[LINEAR[k]] = q[LINEAR[k]];
    for (var m = 0; m < ANGLES.length; m++) o[ANGLES[m]] = q[ANGLES[m]];
    o.animation = q.animation;
    o.animationTime = q.animationTime;
    o.animationLoop = q.animationLoop;
  }

  // blend writes a + (b - a) * t into out, reusing out's objects. A batch or
  // instance that does not pair up takes b's values.
  function blend(a: any[], b: any[], t: number, out: any[]) {
    out.length = b.length;
    for (var i = 0; i < b.length; i++) {
      var bb = b[i], ba = a && a[i];
      var ob = out[i];
      if (!ob || ob.id !== bb.id) ob = out[i] = { id: bb.id, instances: [] };
      ob.instances.length = bb.instances.length;
      var paired = ba && ba.id === bb.id && ba.instances.length === bb.instances.length;
      for (var j = 0; j < bb.instances.length; j++) {
        var q = bb.instances[j];
        var o = ob.instances[j] || (ob.instances[j] = {});
        o.id = q.id;
        var p = paired ? ba.instances[j] : null;
        if (!p || p.id !== q.id) { copyInstance(o, q); continue; }
        for (var k = 0; k < LINEAR.length; k++) o[LINEAR[k]] = p[LINEAR[k]] + (q[LINEAR[k]] - p[LINEAR[k]]) * t;
        for (var m = 0; m < ANGLES.length; m++) o[ANGLES[m]] = angleLerp(p[ANGLES[m]], q[ANGLES[m]], t);
        o.animation = q.animation;
        o.animationLoop = q.animationLoop;
        o.animationTime = p.animation === q.animation && q.animationTime >= p.animationTime
          ? p.animationTime + (q.animationTime - p.animationTime) * t
          : q.animationTime;
      }
    }
    return out;
  }

  // create returns the interpolator for one command runtime. deps.record
  // resolves a target to { handle } once the mount takes commands, and
  // deps.decode decodes a GSP2 frame.
  function create(deps: { record: (target: any, options: any) => any; decode: (frame: any) => any[] }) {
    var states = new WeakMap<object, any>();

    function stats(state: any) {
      return state.handle.__gosxTimedPoseStats || (state.handle.__gosxTimedPoseStats = {
        frames: 0, applied: 0, holds: 0, rewinds: 0, errors: 0, lastError: "", alpha: 0, offsetMS: 0, delayMS: 0, behindMS: 0,
      });
    }

    function hook(state: any) {
      return function(frameMS: number, _deltaMS: number, released: boolean) {
        if (released) {
          state.unhook = null;
          states.delete(state.handle);
          return 0;
        }
        var frames = state.frames, n = frames.length;
        if (!n) return 0;
        var s = stats(state);
        var renderAt = frameMS - state.offset - state.delay;
        var a = frames[0], b = frames[0], t = 1;
        if (renderAt >= frames[n - 1].at) {
          a = b = frames[n - 1];
          if (n > 1) s.holds++;
        } else if (renderAt > frames[0].at) {
          for (var i = 1; i < n; i++) {
            if (frames[i].at >= renderAt) { a = frames[i - 1]; b = frames[i]; break; }
          }
          t = b.at > a.at ? (renderAt - a.at) / (b.at - a.at) : 1;
        }
        s.behindMS = frames[n - 1].at - renderAt;
        try {
          var result = state.handle.applyPoseFrame(a === b ? b.batches : blend(a.batches, b.batches, t, state.out));
          if (result && result.applied === false) return t;
          s.applied++;
        } catch (error) {
          s.errors++;
          s.lastError = String(error && (error as any).message || error);
        }
        s.alpha = t;
        return t;
      };
    }

    // push records one timed frame. It returns null when the mount cannot
    // take frames yet, so the caller queues it the ordinary way.
    function push(target: any, frame: any, options: any) {
      var at = Number(options.at), step = Number(options.step);
      if (!(at >= 0) || !(step > 0) || !Number.isFinite(at) || !Number.isFinite(step)) {
        throw new TypeError("Scene3D timed pose frame needs a finite at >= 0 and step > 0");
      }
      var rec = deps.record(target, options);
      if (!rec || typeof rec.handle.applyPoseFrame !== "function") return null;
      var batches = deps.decode(frame);
      var handle = rec.handle;
      var state = states.get(handle);
      if (!state) {
        state = { handle: handle, frames: [], lags: [], lagAt: 0, offset: 0, delay: step + 2, out: [], unhook: null };
        states.set(handle, state);
      }
      var s = stats(state);
      var frames = state.frames;
      // A frame at or before the newest one (a simulation that restored an
      // older state and ran again) replaces the frames from its time on.
      if (frames.length && at <= frames[frames.length - 1].at) {
        while (frames.length && frames[frames.length - 1].at >= at) frames.pop();
        s.rewinds++;
      }
      frames.push({ at: at, batches: batches });
      if (frames.length > KEEP) frames.shift();
      var lag = nowMS() - at;
      if (state.lags.length < WINDOW) state.lags.push(lag);
      else { state.lags[state.lagAt] = lag; state.lagAt = (state.lagAt + 1) % WINDOW; }
      state.offset = Math.min.apply(null, state.lags);
      state.delay = Number(options.delay) > 0 ? Number(options.delay) : step + 2;
      s.frames++;
      s.offsetMS = state.offset;
      s.delayMS = state.delay;
      if (!state.unhook) {
        if (typeof handle.setFrameHook !== "function") {
          // An older mount without a frame hook: draw each frame as it comes.
          handle.applyPoseFrame(batches);
          s.applied++;
          return { applied: true, binary: true, timed: false };
        }
        state.unhook = handle.setFrameHook(hook(state));
      }
      return { applied: true, binary: true, timed: true };
    }

    return { push: push, blend: blend };
  }

  window.__gosx_scene3d_pose_interpolation = { create: create, blend: blend };
})();
