// Go-driven vessel pose. Go sends GSV1 frames (scene.EncodeVesselPoseFrame); this
// module decodes them and blends between ticks. No physics runs here.
(function() {
  const channels = new Map();
  const MAGIC = [0x47, 0x53, 0x56, 0x31];
  const teleport = 500, minGap = 1 / 240, maxGap = 1;
  // @ts-ignore TS7006 -- this governed module is also evaluated as plain JS in Node tests.
  const angle = v => Math.atan2(Math.sin(v), Math.cos(v));
  // @ts-ignore TS7006 -- this governed module is also evaluated as plain JS in Node tests.
  function decode(input) {
    const bytes = input instanceof Uint8Array ? input : new Uint8Array(input);
    if (bytes.length < 6 || MAGIC.some((m, i) => bytes[i] !== m)) throw new Error('not a vessel pose frame');
    const view = new DataView(bytes.buffer, bytes.byteOffset, bytes.byteLength), count = view.getUint16(4, true), out = [];
    let at = 6;
    for (let i = 0; i < count; i++) {
      if (at + 3 > bytes.length) throw new Error('truncated vessel pose frame');
      const flags = bytes[at], n = view.getUint16(at + 1, true);
      if (n === 0 || at + 3 + n + 48 > bytes.length) throw new Error('truncated vessel pose frame');
      const id = new TextDecoder().decode(bytes.subarray(at + 3, at + 3 + n)), p = at + 3 + n;
      const pose = {id, snap: (flags & 1) === 1, time: view.getFloat64(p, true), x: view.getFloat64(p + 8, true), y: view.getFloat64(p + 16, true), z: view.getFloat64(p + 24, true),
        heading: view.getFloat32(p + 32, true), pitch: view.getFloat32(p + 36, true), roll: view.getFloat32(p + 40, true), speed: view.getFloat32(p + 44, true)};
      if (![pose.time, pose.x, pose.y, pose.z, pose.heading, pose.pitch, pose.roll, pose.speed].every(Number.isFinite)) throw new Error('non-finite vessel pose');
      out.push(pose);
      at = p + 48;
    }
    if (at !== bytes.length) throw new Error('trailing bytes after vessel pose frame');
    return out;
  }
  // Pose fields in channel storage order.
  const FIELDS = 7, HEADING = 3;
  // @ts-ignore TS7006 -- this governed module is also evaluated as plain JS in Node tests.
  const pack = (p, out) => {out[0] = p.x;out[1] = p.y;out[2] = p.z;out[3] = p.heading;out[4] = p.pitch;out[5] = p.roll;out[6] = p.speed;};
  // A channel keeps the newest tick (to) and the pose that was on screen when it
  // arrived (from), and blends over the measured gap between ticks. It holds the
  // last pose rather than extrapolating when a tick is late.
  // @ts-ignore TS7006 -- this governed module is also evaluated as plain JS in Node tests.
  function create(onPush) {
    const from = new Float64Array(FIELDS), to = new Float64Array(FIELDS), shown = new Float64Array(FIELDS), sample = new Float64Array(FIELDS);
    return {
      ready: false, arrival: 0, duration: .1, time: -Infinity, ticks: 0, dropped: 0,
      // @ts-ignore TS7006 -- plain JS parameters are exercised without transpilation.
      push(p, now) {
        if (this.ready && !p.snap && p.time <= this.time) {this.dropped++;return false;}
        pack(p, sample);
        const jump = this.ready && Math.hypot(p.x - to[0], p.z - to[2]) > teleport;
        if (!this.ready || p.snap || jump) {
          from.set(sample);to.set(sample);shown.set(sample);this.duration = .1;
        } else {
          const gap = Math.min(maxGap, Math.max(minGap, now - this.arrival));
          this.duration = this.ticks > 1 ? this.duration * .7 + gap * .3 : gap;
          from.set(shown);to.set(sample);
        }
        this.ready = true;this.arrival = now;this.time = p.time;this.ticks++;
        if (onPush) onPush();
        return true;
      },
      // Fills out with x, y, z, heading, pitch, roll and speed, or returns null before the first tick.
      // @ts-ignore TS7006 -- plain JS parameters are exercised without transpilation.
      pose(now, out) {
        if (!this.ready) return null;
        const a = Math.max(0, Math.min(1, (now - this.arrival) / this.duration)), r = out || {};
        for (let i = 0; i < FIELDS; i++) shown[i] = from[i] + (i === HEADING ? angle(to[i] - from[i]) : to[i] - from[i]) * a;
        shown[HEADING] = angle(shown[HEADING]);
        r.x = shown[0];r.y = shown[1];r.z = shown[2];r.heading = shown[3];r.pitch = shown[4];r.roll = shown[5];r.speed = shown[6];
        return r;
      },
      // True while ticks are arriving or a blend is unfinished: the scene keeps rendering.
      // @ts-ignore TS7006 -- plain JS parameters are exercised without transpilation.
      active(now) {return this.ready && now - this.arrival < Math.max(1, this.duration * 3);},
    };
  }
  // @ts-ignore TS7006 -- plain JS parameters are exercised without transpilation.
  const key = (mountId, nodeId) => mountId + '\n' + nodeId;
  // @ts-ignore TS7006 -- plain JS parameters are exercised without transpilation.
  function register(mountId, nodeId, channel) {channels.set(key(mountId, nodeId), channel);}
  // @ts-ignore TS7006 -- plain JS parameters are exercised without transpilation.
  function unregister(mountId, nodeId, channel) {if (channels.get(key(mountId, nodeId)) === channel) channels.delete(key(mountId, nodeId));}
  const now = () => (typeof performance !== 'undefined' ? performance.now() : Date.now()) / 1000;
  // True when at least one registered vessel took the frame. Tests may replace api.now.
  // @ts-ignore TS7006 -- plain JS parameters are exercised without transpilation.
  function dispatch(target, bytes) {
    let taken = false;
    const t = api.now(), id = target && typeof target === 'object' ? target.id : target;
    for (const pose of decode(bytes)) {
      const channel = channels.get(key(id, pose.id));
      if (channel && channel.push(pose, t)) taken = true;
    }
    return taken;
  }
  // Binds one mounted vessel to its channel. The mount uses only this object in external mode.
  // @ts-ignore TS7006 -- plain JS parameters are exercised without transpilation.
  function attach(mount, nodeId, onPush) {
    const channel = create(onPush), shown = {};
    register(mount.id, nodeId, channel);
    mount.setAttribute('data-gosx-scene3d-vessel-source', 'go');
    return {
      channel, input: {lookYaw: 0, lookPitch: 0}, controls: {input: {}, refresh() {}, dispose() {}},
      active: () => channel.active(api.now()),
      // @ts-ignore TS7006 -- plain JS parameters are exercised without transpilation.
      apply(state) {const p = channel.pose(api.now(), shown);if (p) Object.assign(state, p);},
      reset() {channel.ready = false;channel.ticks = 0;},
      dispose() {unregister(mount.id, nodeId, channel);},
    };
  }
  const api = {decode, create, register, unregister, attach, now, dispatch};
  window.__gosx_scene3d_vessel_external = api;
  window.__gosx_scene3d_vessel_pose = dispatch;
})();
