  // animation-graph.ts — evaluates animation graph frames (GSA1) from Go's
  // motion/animgraph package.
  //
  // Go owns the graph logic: states, transitions, blend weights, clip times,
  // events and curves. Each frame names, per model and per layer, the clips to
  // sample with their times and weights, plus IK and look-at inputs. This file
  // samples those clips, blends each layer inside its bone mask (override or
  // additive), strips root motion, solves IK (ik.ts), and writes joint
  // matrices. It ships in its own lazy chunk, so no budgeted renderer chunk
  // grows: it reaches the mount through the existing clip-frame path
  // (handle.applyClipFrame -> animationApi.applyClipFrame), which it wraps.
  //
  // Hot-path rules: typed arrays only, no closures or Map lookups per channel,
  // nothing allocated per frame once a model is bound.

  const AG_MAX_LAYERS = 8, AG_MAX_SAMPLES = 16, AG_MAX_IK = 8, AG_MAX_LOOK = 4;
  const AG_FEEDBACK_FLOATS = 3 + 6 * AG_MAX_IK + 6 * AG_MAX_LOOK + 4;
  const AG_MARKER = "__gosxAnimationGraph";

  // ---------------------------------------------------------------------------
  // Matrix helpers (column-major, flat arrays with offsets)
  // ---------------------------------------------------------------------------

  function agTRS(out: Float32Array, o: number, t: Float64Array, ti: number, r: Float64Array, ri: number, s: Float64Array, si: number) {
    const x = r[ri], y = r[ri + 1], z = r[ri + 2], w = r[ri + 3];
    const x2 = x + x, y2 = y + y, z2 = z + z;
    const xx = x * x2, xy = x * y2, xz = x * z2, yy = y * y2, yz = y * z2, zz = z * z2;
    const wx = w * x2, wy = w * y2, wz = w * z2;
    const sx = s[si], sy = s[si + 1], sz = s[si + 2];
    out[o] = (1 - (yy + zz)) * sx; out[o + 1] = (xy + wz) * sx; out[o + 2] = (xz - wy) * sx; out[o + 3] = 0;
    out[o + 4] = (xy - wz) * sy; out[o + 5] = (1 - (xx + zz)) * sy; out[o + 6] = (yz + wx) * sy; out[o + 7] = 0;
    out[o + 8] = (xz + wy) * sz; out[o + 9] = (yz - wx) * sz; out[o + 10] = (1 - (xx + yy)) * sz; out[o + 11] = 0;
    out[o + 12] = t[ti]; out[o + 13] = t[ti + 1]; out[o + 14] = t[ti + 2]; out[o + 15] = 1;
  }

  function agMul(out: Float32Array, o: number, a: ArrayLike<number>, ao: number, b: ArrayLike<number>, bo: number) {
    for (let c = 0; c < 4; c++) {
      const b0 = b[bo + c * 4], b1 = b[bo + c * 4 + 1], b2 = b[bo + c * 4 + 2], b3 = b[bo + c * 4 + 3];
      out[o + c * 4] = a[ao] * b0 + a[ao + 4] * b1 + a[ao + 8] * b2 + a[ao + 12] * b3;
      out[o + c * 4 + 1] = a[ao + 1] * b0 + a[ao + 5] * b1 + a[ao + 9] * b2 + a[ao + 13] * b3;
      out[o + c * 4 + 2] = a[ao + 2] * b0 + a[ao + 6] * b1 + a[ao + 10] * b2 + a[ao + 14] * b3;
      out[o + c * 4 + 3] = a[ao + 3] * b0 + a[ao + 7] * b1 + a[ao + 11] * b2 + a[ao + 15] * b3;
    }
  }

  const AG_IDENTITY = new Float32Array([1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1]);

  // ---------------------------------------------------------------------------
  // Quaternion blending on Float64Array offsets
  // ---------------------------------------------------------------------------

  // Normalized lerp along the shortest arc: out = normalize(a + (b' - a) t),
  // where b' is b or -b, whichever is nearer a. Pose blending uses nlerp: it is
  // cheaper than slerp, and for the small per-frame blend angles of animation
  // layers the difference is far below a pixel.
  function agNlerp(out: Float64Array, o: number, a: Float64Array, ao: number, b: Float64Array, bo: number, t: number) {
    const ax = a[ao], ay = a[ao + 1], az = a[ao + 2], aw = a[ao + 3];
    let bx = b[bo], by = b[bo + 1], bz = b[bo + 2], bw = b[bo + 3];
    if (ax * bx + ay * by + az * bz + aw * bw < 0) { bx = -bx; by = -by; bz = -bz; bw = -bw; }
    const x = ax + (bx - ax) * t, y = ay + (by - ay) * t, z = az + (bz - az) * t, w = aw + (bw - aw) * t;
    const len = Math.sqrt(x * x + y * y + z * z + w * w);
    if (len < 1e-12) { out[o] = 0; out[o + 1] = 0; out[o + 2] = 0; out[o + 3] = 1; return; }
    out[o] = x / len; out[o + 1] = y / len; out[o + 2] = z / len; out[o + 3] = w / len;
  }

  // out = a * b (Hamilton product).
  function agQuatMul(out: Float64Array, o: number, a: Float64Array, ao: number, b: Float64Array, bo: number) {
    const ax = a[ao], ay = a[ao + 1], az = a[ao + 2], aw = a[ao + 3];
    const bx = b[bo], by = b[bo + 1], bz = b[bo + 2], bw = b[bo + 3];
    out[o] = aw * bx + ax * bw + ay * bz - az * by;
    out[o + 1] = aw * by - ax * bz + ay * bw + az * bx;
    out[o + 2] = aw * bz + ax * by - ay * bx + az * bw;
    out[o + 3] = aw * bw - ax * bx - ay * by - az * bz;
  }

  // out = conj(a) * b, the rotation that takes a to b.
  function agQuatDelta(out: Float64Array, o: number, a: Float64Array, ao: number, b: Float64Array, bo: number) {
    const ax = -a[ao], ay = -a[ao + 1], az = -a[ao + 2], aw = a[ao + 3];
    const bx = b[bo], by = b[bo + 1], bz = b[bo + 2], bw = b[bo + 3];
    out[o] = aw * bx + ax * bw + ay * bz - az * by;
    out[o + 1] = aw * by - ax * bz + ay * bw + az * bx;
    out[o + 2] = aw * bz + ax * by - ay * bx + az * bw;
    out[o + 3] = aw * bw - ax * bx - ay * by - az * bz;
  }

  // ---------------------------------------------------------------------------
  // Clip binding and channel evaluation
  // ---------------------------------------------------------------------------

  // A bound channel: node index, property code (0 translation, 1 rotation,
  // 2 scale), interpolation code (0 linear, 1 step, 2 cubic spline), key
  // arrays, a search hint, and the channel value at time zero for additive
  // layers.
  function agBindChannel(channel: any, nodeCount: number) {
    const node = channel.targetID != null ? channel.targetID : channel.targetNode;
    const code = channel.property === "translation" ? 0 : channel.property === "rotation" ? 1 : channel.property === "scale" ? 2 : -1;
    if (code < 0 || !Number.isInteger(node) || node < 0 || node >= nodeCount || !channel.times || !channel.values || channel.times.length === 0) return null;
    const interp = channel.interpolation === "STEP" ? 1 : channel.interpolation === "CUBICSPLINE" ? 2 : 0;
    const width = code === 1 ? 4 : 3;
    const keyStride = interp === 2 ? width * 3 : width;
    if (channel.values.length < channel.times.length * keyStride) return null;
    const bound = { node, code, interp, width, times: channel.times, values: channel.values, hint: 0, ref: new Float64Array(4) };
    agEvalChannel(bound, channel.times[0], bound.ref);
    return bound;
  }

  // Bind a clip list. Names resolve exactly, and a name without "|" also
  // matches the part after "|" of a Blender "Armature|Action" export name.
  function agBindClips(clips: any[], nodeCount: number, into: any) {
    for (let i = 0; i < clips.length; i++) {
      const clip = clips[i];
      if (!clip || !Array.isArray(clip.channels)) continue;
      const channels: any[] = [];
      for (let c = 0; c < clip.channels.length; c++) {
        const bound = agBindChannel(clip.channels[c], nodeCount);
        if (bound) channels.push(bound);
      }
      const name = typeof clip.name === "string" ? clip.name : "";
      const binding = { name, duration: Number(clip.duration) > 0 ? Number(clip.duration) : 0, channels };
      const index = into.list.length;
      into.list.push(binding);
      if (name && !into.byName.has(name)) into.byName.set(name, index);
      const bar = name.lastIndexOf("|");
      if (bar >= 0 && !into.byName.has(name.slice(bar + 1))) into.byName.set(name.slice(bar + 1), index);
    }
    return into;
  }

  // Index i with times[i] <= time < times[i + 1], from the previous index when
  // time moved a little; otherwise binary search.
  function agSeek(ch: any, time: number) {
    const times = ch.times, last = times.length - 1;
    let i = ch.hint;
    if (i >= last) i = 0;
    if (times[i] <= time && time < times[i + 1]) return i;
    if (i + 2 <= last && times[i + 1] <= time && time < times[i + 2]) { ch.hint = i + 1; return i + 1; }
    let lo = 0, hi = last - 1;
    while (lo < hi) {
      const mid = (lo + hi + 1) >> 1;
      if (times[mid] <= time) lo = mid; else hi = mid - 1;
    }
    ch.hint = lo;
    return lo;
  }

  // Evaluate a bound channel at time into out[0..width). Same rules as the
  // sampled clip path: clamp outside the keys, slerp for linear rotations,
  // glTF 2.0 Appendix C Hermite for cubic splines (rotations normalized).
  function agEvalChannel(ch: any, time: number, out: Float64Array) {
    const times = ch.times, values = ch.values, width = ch.width;
    const cubic = ch.interp === 2, stride = cubic ? width * 3 : width, off = cubic ? width : 0;
    const last = times.length - 1;
    if (last < 1 || time <= times[0]) {
      for (let k = 0; k < width; k++) out[k] = values[off + k];
      return;
    }
    if (time >= times[last]) {
      const start = last * stride + off;
      for (let k = 0; k < width; k++) out[k] = values[start + k];
      return;
    }
    const i = agSeek(ch, time);
    const t0 = times[i], t1 = times[i + 1], dt = t1 - t0;
    const u = dt > 0 ? (time - t0) / dt : 0;
    const s0 = i * stride + off, s1 = (i + 1) * stride + off;
    if (ch.interp === 1) {
      for (let k = 0; k < width; k++) out[k] = values[s0 + k];
      return;
    }
    if (cubic) {
      const u2 = u * u, u3 = u2 * u;
      const h00 = 2 * u3 - 3 * u2 + 1, h10 = u3 - 2 * u2 + u, h01 = -2 * u3 + 3 * u2, h11 = u3 - u2;
      const m0 = i * stride + 2 * width, m1 = (i + 1) * stride;
      for (let k = 0; k < width; k++) {
        out[k] = h00 * values[s0 + k] + h10 * values[m0 + k] * dt + h01 * values[s1 + k] + h11 * values[m1 + k] * dt;
      }
      if (width === 4) agNormalizeQuat(out, 0);
      return;
    }
    if (width === 4) {
      agSlerpKeys(out, values, s0, s1, u);
      return;
    }
    for (let k = 0; k < width; k++) out[k] = values[s0 + k] + (values[s1 + k] - values[s0 + k]) * u;
  }

  function agNormalizeQuat(q: Float64Array, o: number) {
    const len = Math.sqrt(q[o] * q[o] + q[o + 1] * q[o + 1] + q[o + 2] * q[o + 2] + q[o + 3] * q[o + 3]);
    if (len < 1e-12) { q[o] = 0; q[o + 1] = 0; q[o + 2] = 0; q[o + 3] = 1; return; }
    q[o] /= len; q[o + 1] /= len; q[o + 2] /= len; q[o + 3] /= len;
  }

  // Interpolate two rotation keys. glTF asks for slerp; for keys whose
  // quaternions have dot >= 0.95 (under about 36 degrees of rotation apart,
  // which covers keys baked at 24-60 fps) normalized lerp differs from slerp
  // by under 0.05 degrees and skips acos and two sines, so slerp runs only
  // for wider gaps.
  function agSlerpKeys(out: Float64Array, v: ArrayLike<number>, a: number, b: number, t: number) {
    const ax = v[a], ay = v[a + 1], az = v[a + 2], aw = v[a + 3];
    let bx = v[b], by = v[b + 1], bz = v[b + 2], bw = v[b + 3];
    let dot = ax * bx + ay * by + az * bz + aw * bw;
    if (dot < 0) { dot = -dot; bx = -bx; by = -by; bz = -bz; bw = -bw; }
    let w0 = 1 - t, w1 = t;
    if (dot < 0.95) {
      const theta = Math.acos(dot), sin = Math.sin(theta);
      w0 = Math.sin((1 - t) * theta) / sin;
      w1 = Math.sin(t * theta) / sin;
      out[0] = ax * w0 + bx * w1; out[1] = ay * w0 + by * w1; out[2] = az * w0 + bz * w1; out[3] = aw * w0 + bw * w1;
      return;
    }
    const x = ax * w0 + bx * w1, y = ay * w0 + by * w1, z = az * w0 + bz * w1, w = aw * w0 + bw * w1;
    const inv = 1 / Math.sqrt(x * x + y * y + z * z + w * w);
    out[0] = x * inv; out[1] = y * inv; out[2] = z * inv; out[3] = w * inv;
  }

  function agSampleTime(time: number, duration: number, loop: boolean) {
    if (!(time > 0)) return 0;
    if (!(duration > 0)) return 0;
    return loop ? time % duration : Math.min(time, duration);
  }

  // ---------------------------------------------------------------------------
  // Rig registry (static per graph, defined once per page by Go)
  // ---------------------------------------------------------------------------

  const agRigs = new Map<number, any>();

  function agDefineRig(hash: any, json: any) {
    if (!Number.isInteger(hash) || hash <= 0 || hash > 0xffffffff || typeof json !== "string") return false;
    try {
      const rig = JSON.parse(json);
      if (!rig || typeof rig !== "object" || rig.v !== 1) return false;
      rig.hash = hash;
      agRigs.set(hash, rig);
      return true;
    } catch (_error) {
      return false;
    }
  }

  // ---------------------------------------------------------------------------
  // Frame decoder
  // ---------------------------------------------------------------------------

  // createDecoder returns decode(bytes) -> frame. The frame object and every
  // nested object are reused by the next call. Strings are interned by bytes,
  // so steady-state decoding allocates nothing. The whole frame validates
  // before the caller sees it.
  function agCreateDecoder() {
    const textDecoder = typeof TextDecoder === "function" ? new TextDecoder("utf-8", { fatal: true }) : null;
    const f32 = new Float32Array(1), u8v = new Uint8Array(f32.buffer);
    const strings: any[] = [];
    const names: string[] = [];
    const frame: any = { models: [] as any[], count: 0 };
    let bytes = new Uint8Array(0), at = 0;
    function need(n: number) { if (n > bytes.length - at) throw new RangeError("truncated animation graph frame"); }
    function u8() { need(1); return bytes[at++]; }
    function u16() { need(2); const v = bytes[at] | (bytes[at + 1] << 8); at += 2; return v; }
    function u32() { need(4); const v = (bytes[at] | (bytes[at + 1] << 8) | (bytes[at + 2] << 16)) + bytes[at + 3] * 16777216; at += 4; return v; }
    function f() {
      need(4);
      u8v[0] = bytes[at]; u8v[1] = bytes[at + 1]; u8v[2] = bytes[at + 2]; u8v[3] = bytes[at + 3];
      at += 4;
      const v = f32[0];
      if (!Number.isFinite(v)) throw new TypeError("non-finite animation graph frame value");
      return v;
    }
    function text() {
      const n = u16();
      if (!n) throw new TypeError("empty animation graph frame string");
      need(n);
      for (let i = 0; i < strings.length; i++) {
        const known = strings[i].bytes;
        if (known.length !== n) continue;
        let j = 0;
        while (j < n && known[j] === bytes[at + j]) j++;
        if (j === n) { at += n; return strings[i].text; }
      }
      const slice = bytes.subarray(at, at + n);
      const value = textDecoder ? textDecoder.decode(slice) : String.fromCharCode.apply(null, Array.from(slice));
      if (strings.length < 1024) strings.push({ bytes: slice.slice(), text: value });
      at += n;
      return value;
    }
    function model(i: number) {
      let m = frame.models[i];
      if (!m) {
        m = { id: "", rigHash: 0, dt: 0, layerCount: 0, layers: [] as any[], ikCount: 0, ik: [] as any[], lookCount: 0, look: [] as any[] };
        for (let l = 0; l < AG_MAX_LAYERS; l++) {
          const layer = { weight: 0, count: 0, samples: [] as any[] };
          for (let s = 0; s < AG_MAX_SAMPLES; s++) layer.samples.push({ name: "", time: 0, weight: 0, loop: false });
          m.layers.push(layer);
        }
        for (let k = 0; k < AG_MAX_IK; k++) m.ik.push({ chain: 0, flags: 0, weight: 0, target: new Float64Array(3), pole: new Float64Array(3), height: 0, normal: new Float64Array(3) });
        for (let k = 0; k < AG_MAX_LOOK; k++) m.look.push({ chain: 0, weight: 0, target: new Float64Array(3) });
        frame.models[i] = m;
      }
      return m;
    }
    return function decode(input: any) {
      // Views from another realm (a worker or test sandbox) fail instanceof,
      // so accept any ArrayBuffer view and re-view its bytes.
      if (input instanceof Uint8Array) bytes = input;
      else if (ArrayBuffer.isView(input)) bytes = new Uint8Array(input.buffer, input.byteOffset, input.byteLength);
      else if (input && Object.prototype.toString.call(input) === "[object ArrayBuffer]") bytes = new Uint8Array(input);
      else throw new TypeError("animation graph frame must be an ArrayBuffer or Uint8Array");
      at = 0;
      need(8);
      if (bytes[0] !== 71 || bytes[1] !== 83 || bytes[2] !== 65 || bytes[3] !== 49) throw new TypeError("unsupported animation graph frame version");
      at = 4;
      const nameCount = u16();
      if (nameCount > 0x8000) throw new RangeError("too many animation graph clip names");
      for (let n = 0; n < nameCount; n++) names[n] = text();
      const count = u16();
      for (let i = 0; i < count; i++) {
        const m = model(i);
        m.id = text();
        for (let d = 0; d < i; d++) if (frame.models[d].id === m.id) throw new TypeError("duplicate animation graph model ID");
        m.rigHash = u32();
        m.dt = f();
        if (m.dt < 0) throw new RangeError("negative animation graph frame dt");
        m.layerCount = u8();
        if (m.layerCount > AG_MAX_LAYERS) throw new RangeError("too many animation graph layers");
        for (let l = 0; l < m.layerCount; l++) {
          const layer = m.layers[l];
          layer.weight = f();
          layer.count = u8();
          if (layer.count > AG_MAX_SAMPLES) throw new RangeError("too many animation graph samples");
          for (let s = 0; s < layer.count; s++) {
            const sample = layer.samples[s];
            const ref = u16();
            if (ref >= nameCount) throw new RangeError("unknown animation graph clip name");
            sample.name = names[ref];
            sample.time = f();
            sample.weight = f();
            const flags = u8();
            if (sample.time < 0 || sample.weight < 0 || flags > 1) throw new RangeError("invalid animation graph sample");
            sample.loop = flags === 1;
          }
          if (layer.weight < 0) throw new RangeError("negative animation graph layer weight");
        }
        m.ikCount = u8();
        if (m.ikCount > AG_MAX_IK) throw new RangeError("too many animation graph IK entries");
        for (let k = 0; k < m.ikCount; k++) {
          const ik = m.ik[k];
          ik.chain = u8(); ik.flags = u8(); ik.weight = f();
          if (ik.chain >= AG_MAX_IK || ik.flags > 7 || ik.weight < 0) throw new RangeError("invalid animation graph IK entry");
          if (ik.flags & 1) { ik.target[0] = f(); ik.target[1] = f(); ik.target[2] = f(); }
          if (ik.flags & 2) { ik.pole[0] = f(); ik.pole[1] = f(); ik.pole[2] = f(); }
          if (ik.flags & 4) { ik.height = f(); ik.normal[0] = f(); ik.normal[1] = f(); ik.normal[2] = f(); }
        }
        m.lookCount = u8();
        if (m.lookCount > AG_MAX_LOOK) throw new RangeError("too many animation graph look-at entries");
        for (let k = 0; k < m.lookCount; k++) {
          const look = m.look[k];
          look.chain = u8(); look.weight = f();
          look.target[0] = f(); look.target[1] = f(); look.target[2] = f();
          if (look.chain >= AG_MAX_LOOK || look.weight < 0) throw new RangeError("invalid animation graph look-at entry");
        }
      }
      if (at !== bytes.length) throw new RangeError("trailing animation graph frame data");
      frame.count = count;
      return frame;
    };
  }

  // Held frame data per model ID, on the mount's scene state. A holder
  // outlives model records, so a re-hydrated record poses from the last frame.
  function agHolder(state: any, id: string) {
    const holders = state._animGraphHolders || (state._animGraphHolders = new Map());
    let holder = holders.get(id);
    if (!holder) {
      holder = { id, serial: 0, data: null as any, feedback: new Float64Array(AG_FEEDBACK_FLOATS), feedbackCount: 0 };
      holders.set(id, holder);
    }
    return holder;
  }

  function agCopyModel(dst: any, src: any) {
    if (!dst) {
      dst = { rigHash: 0, dt: 0, layerCount: 0, layers: [] as any[], ikCount: 0, ik: [] as any[], lookCount: 0, look: [] as any[] };
      for (let l = 0; l < AG_MAX_LAYERS; l++) {
        const layer = { weight: 0, count: 0, samples: [] as any[] };
        for (let s = 0; s < AG_MAX_SAMPLES; s++) layer.samples.push({ name: "", time: 0, weight: 0, loop: false, clip: -1 });
        dst.layers.push(layer);
      }
      for (let k = 0; k < AG_MAX_IK; k++) dst.ik.push({ chain: 0, flags: 0, weight: 0, target: new Float64Array(3), pole: new Float64Array(3), height: 0, normal: new Float64Array(3) });
      for (let k = 0; k < AG_MAX_LOOK; k++) dst.look.push({ chain: 0, weight: 0, target: new Float64Array(3) });
    }
    dst.rigHash = src.rigHash; dst.dt = src.dt;
    dst.layerCount = src.layerCount;
    for (let l = 0; l < src.layerCount; l++) {
      const a = src.layers[l], b = dst.layers[l];
      b.weight = a.weight; b.count = a.count;
      for (let s = 0; s < a.count; s++) {
        const x = a.samples[s], y = b.samples[s];
        if (y.name !== x.name) { y.name = x.name; y.clip = -1; }
        y.time = x.time; y.weight = x.weight; y.loop = x.loop;
      }
    }
    dst.ikCount = src.ikCount;
    for (let k = 0; k < src.ikCount; k++) {
      const a = src.ik[k], b = dst.ik[k];
      b.chain = a.chain; b.flags = a.flags; b.weight = a.weight; b.height = a.height;
      b.target.set(a.target); b.pole.set(a.pole); b.normal.set(a.normal);
    }
    dst.lookCount = src.lookCount;
    for (let k = 0; k < src.lookCount; k++) {
      const a = src.look[k], b = dst.look[k];
      b.chain = a.chain; b.weight = a.weight; b.target.set(a.target);
    }
    return dst;
  }

  // ---------------------------------------------------------------------------
  // Per-model evaluator
  // ---------------------------------------------------------------------------

  // Root nodes for root-motion stripping: glTF scene roots and the topmost
  // joint of each skin (the same rule as the sampled clip path).
  function agRootNodes(nodes: any[], parent: Int32Array, skins: any[], roots: number[]) {
    const out = new Uint8Array(nodes.length);
    for (let i = 0; i < roots.length; i++) if (roots[i] >= 0 && roots[i] < nodes.length) out[roots[i]] = 1;
    for (let s = 0; s < skins.length; s++) {
      const joints = skins[s] && skins[s].joints;
      if (!joints) continue;
      const set = new Set<number>(joints);
      for (let j = 0; j < joints.length; j++) if (!set.has(parent[joints[j]])) out[joints[j]] = 1;
    }
    return out;
  }

  // agCreateModel sizes every buffer the evaluator writes, once per record.
  function agCreateModel(record: any, clips: any[]) {
    const nodes: any[] = Array.isArray(record.nodes) ? record.nodes : [];
    const n = nodes.length;
    const parent = new Int32Array(n).fill(-1);
    for (let i = 0; i < n; i++) {
      const kids = nodes[i] && nodes[i].children;
      if (kids) for (let k = 0; k < kids.length; k++) if (kids[k] >= 0 && kids[k] < n) parent[kids[k]] = i;
    }
    let roots: number[] = Array.isArray(record.rootNodes) && record.rootNodes.length ? record.rootNodes.slice() : [];
    if (!roots.length) for (let i = 0; i < n; i++) if (parent[i] < 0) roots.push(i);
    const order: number[] = [], ends: number[] = [];
    const reached = new Uint8Array(n);
    const pos = new Int32Array(n).fill(-1);
    function walk(index: number) {
      if (index < 0 || index >= n || reached[index] || !nodes[index]) return;
      reached[index] = 1;
      const at = order.length;
      order.push(index); ends.push(0);
      pos[index] = at;
      const kids = nodes[index].children || [];
      for (let k = 0; k < kids.length; k++) walk(kids[k]);
      ends[at] = order.length;
    }
    for (let r = 0; r < roots.length; r++) walk(roots[r]);
    const m: any = {
      record, nodes, n,
      order: Int32Array.from(order), end: Int32Array.from(ends), pos, parent, reached,
      authT: new Float64Array(n * 3), authR: new Float64Array(n * 4), authS: new Float64Array(n * 3),
      matrix: new Float32Array(n * 16), hasMatrix: new Uint8Array(n),
      t: new Float64Array(n * 3), r: new Float64Array(n * 4), s: new Float64Array(n * 3), flags: new Uint8Array(n),
      lt: new Float64Array(n * 3), lr: new Float64Array(n * 4), ls: new Float64Array(n * 3), lflags: new Uint8Array(n), lw: new Float64Array(n * 3),
      world: new Float32Array(n * 16), localScratch: new Float32Array(16), root: null as any,
      v: new Float64Array(4), q: new Float64Array(4),
      clips: agBindClips(clips || [], n, { list: [] as any[], byName: new Map<string, number>() }),
      rootSet: null as any,
      skins: [] as any[],
      rig: null as any, rigHash: -1,
      entries: [] as any[],
      evalMicros: 0,
    };
    for (let i = 0; i < n; i++) {
      const node = nodes[i] || {};
      const tr = node.translation || [0, 0, 0], ro = node.rotation || [0, 0, 0, 1], sc = node.scale || [1, 1, 1];
      for (let a = 0; a < 3; a++) { m.authT[i * 3 + a] = tr[a]; m.authS[i * 3 + a] = sc[a]; }
      for (let b = 0; b < 4; b++) m.authR[i * 4 + b] = ro[b];
      if (node.matrix && node.matrix.length === 16) { m.matrix.set(node.matrix, i * 16); m.hasMatrix[i] = 1; }
      m.world.set(AG_IDENTITY, i * 16);
    }
    const skins: any[] = Array.isArray(record.skins) ? record.skins : [];
    m.rootSet = agRootNodes(nodes, parent, skins, roots);
    for (let s = 0; s < skins.length; s++) {
      const skin = skins[s];
      const joints: any[] = skin && Array.isArray(skin.joints) ? skin.joints : [];
      let buffer = skin && skin._jointMatricesBuffer;
      if (joints.length && (!buffer || buffer.length !== joints.length * 16)) buffer = skin._jointMatricesBuffer = new Float32Array(joints.length * 16);
      const ibmSrc = skin && skin.inverseBindMatrices && typeof skin.inverseBindMatrices.length === "number" ? skin.inverseBindMatrices : null;
      const ibm = new Float32Array(joints.length * 16);
      for (let j = 0; j < joints.length; j++) ibm.set(ibmSrc && j * 16 + 16 <= ibmSrc.length ? ibmSrc.subarray(j * 16, j * 16 + 16) : AG_IDENTITY, j * 16);
      m.skins.push({ skin, joints: Int32Array.from(joints.map((v: any) => (Number.isInteger(v) && v >= 0 && v < n ? v : -1))), ibm, buffer });
    }
    return m;
  }

  // Bind (or rebind) the model's rig: masks, layer modes, IK and look-at
  // bones. The humanoid bone map comes from animation-rig.ts.
  function agBindRig(m: any, hash: number) {
    if (m.rigHash === hash) return m.rig;
    m.rigHash = hash;
    const rig = hash ? agRigs.get(hash) : null;
    m.rig = rig ? agResolveRig(m, rig) : null;
    return m.rig;
  }

  function agResolveSamples(m: any, data: any) {
    for (let l = 0; l < data.layerCount; l++) {
      const layer = data.layers[l];
      for (let s = 0; s < layer.count; s++) {
        const sample = layer.samples[s];
        if (sample.clip < 0 || sample.clipList !== m.clips.list) {
          const index = m.clips.byName.get(sample.name);
          sample.clip = index === undefined ? -2 : index;
          sample.clipList = m.clips.list;
        }
      }
    }
  }

  // Blend one sampled channel value (m.v) into the layer scratch with the
  // sample weight w, by running weight: the first sample copies, later
  // samples move toward their value by w / (accumulated + w).
  function agLayerAccumulate(m: any, ch: any, w: number, additive: boolean, refPose: any) {
    const node = ch.node, code = ch.code, bit = 1 << code, wi = node * 3 + code;
    const v = m.v;
    if (additive) {
      // Convert to an offset from the reference pose before blending.
      if (code === 1) {
        const ref = refPose ? refPose.r : ch.ref, ro = refPose ? node * 4 : 0;
        agQuatDelta(m.q, 0, ref, ro, v, 0);
        v[0] = m.q[0]; v[1] = m.q[1]; v[2] = m.q[2]; v[3] = m.q[3];
      } else if (code === 0) {
        const ref = refPose ? refPose.t : ch.ref, ro = refPose ? node * 3 : 0;
        v[0] -= ref[ro]; v[1] -= ref[ro + 1]; v[2] -= ref[ro + 2];
      } else {
        const ref = refPose ? refPose.s : ch.ref, ro = refPose ? node * 3 : 0;
        for (let k = 0; k < 3; k++) v[k] = ref[ro + k] !== 0 ? v[k] / ref[ro + k] : 1;
      }
    }
    if (!(m.lflags[node] & bit)) {
      m.lflags[node] |= bit;
      m.lw[wi] = w;
      if (code === 1) { m.lr[node * 4] = v[0]; m.lr[node * 4 + 1] = v[1]; m.lr[node * 4 + 2] = v[2]; m.lr[node * 4 + 3] = v[3]; }
      else { const dst = code === 0 ? m.lt : m.ls; dst[node * 3] = v[0]; dst[node * 3 + 1] = v[1]; dst[node * 3 + 2] = v[2]; }
      return;
    }
    const tw = w / (m.lw[wi] + w);
    m.lw[wi] += w;
    if (code === 1) agNlerp(m.lr, node * 4, m.lr, node * 4, v, 0, tw);
    else {
      const dst = code === 0 ? m.lt : m.ls, o = node * 3;
      dst[o] += (v[0] - dst[o]) * tw; dst[o + 1] += (v[1] - dst[o + 1]) * tw; dst[o + 2] += (v[2] - dst[o + 2]) * tw;
    }
  }

  // Combine the layer scratch into the pose with weight lw times the mask.
  function agLayerApply(m: any, lw: number, mask: Float32Array | null, additive: boolean) {
    const n = m.n;
    for (let node = 0; node < n; node++) {
      const f = m.lflags[node];
      if (!f) continue;
      const w = mask ? lw * mask[node] : lw;
      if (!(w > 0)) continue;
      if (additive) {
        if (f & 1) { const o = node * 3; m.t[o] += m.lt[o] * w; m.t[o + 1] += m.lt[o + 1] * w; m.t[o + 2] += m.lt[o + 2] * w; }
        if (f & 2) {
          const q = m.q, o = node * 4;
          if (w >= 1) { q[0] = m.lr[o]; q[1] = m.lr[o + 1]; q[2] = m.lr[o + 2]; q[3] = m.lr[o + 3]; }
          else { q[0] = 0; q[1] = 0; q[2] = 0; q[3] = 1; agNlerp(q, 0, q, 0, m.lr, o, w); }
          agQuatMul(m.r, o, m.r, o, q, 0);
          agNormalizeQuat(m.r, o);
        }
        if (f & 4) { const o = node * 3; for (let k = 0; k < 3; k++) m.s[o + k] *= 1 + (m.ls[o + k] - 1) * w; }
      } else if (w >= 1) {
        if (f & 1) { const o = node * 3; m.t[o] = m.lt[o]; m.t[o + 1] = m.lt[o + 1]; m.t[o + 2] = m.lt[o + 2]; }
        if (f & 2) { const o = node * 4; m.r[o] = m.lr[o]; m.r[o + 1] = m.lr[o + 1]; m.r[o + 2] = m.lr[o + 2]; m.r[o + 3] = m.lr[o + 3]; }
        if (f & 4) { const o = node * 3; m.s[o] = m.ls[o]; m.s[o + 1] = m.ls[o + 1]; m.s[o + 2] = m.ls[o + 2]; }
      } else {
        if (f & 1) { const o = node * 3; for (let k = 0; k < 3; k++) m.t[o + k] += (m.lt[o + k] - m.t[o + k]) * w; }
        if (f & 2) agNlerp(m.r, node * 4, m.r, node * 4, m.lr, node * 4, w);
        if (f & 4) { const o = node * 3; for (let k = 0; k < 3; k++) m.s[o + k] += (m.ls[o + k] - m.s[o + k]) * w; }
      }
      m.flags[node] |= f;
    }
  }

  // Recompute world matrices for preorder positions [from, to).
  function agWorldRange(m: any, from: number, to: number, root: any) {
    const order = m.order, world = m.world, parent = m.parent, local = m.localScratch;
    for (let k = from; k < to; k++) {
      const node = order[k], o = node * 16;
      const p = parent[node];
      if (m.flags[node] === 0 && m.hasMatrix[node]) {
        for (let e = 0; e < 16; e++) local[e] = m.matrix[o + e];
      } else {
        agTRS(local, 0, m.t, node * 3, m.r, node * 4, m.s, node * 3);
      }
      if (p >= 0 && m.reached[p]) agMul(world, o, world, p * 16, local, 0);
      else if (root) agMul(world, o, root, 0, local, 0);
      else for (let e = 0; e < 16; e++) world[o + e] = local[e];
    }
  }

  // agEvaluate poses model m from held frame data. It is a pure function of
  // the data, the clips, the rig and record.rootTransform, except for the IK
  // smoothing state, which advances only when holder.serial changes.
  function agEvaluate(m: any, holder: any) {
    const start = typeof performance !== "undefined" ? performance.now() : 0;
    const data = holder.data;
    const n = m.n;
    m.t.set(m.authT); m.r.set(m.authR); m.s.set(m.authS);
    m.flags.fill(0);
    const rig = agBindRig(m, data.rigHash);
    agResolveSamples(m, data);
    const layers = Math.min(data.layerCount, AG_MAX_LAYERS);
    for (let l = 0; l < layers; l++) {
      const layer = data.layers[l];
      if (!(layer.weight > 0) || layer.count === 0) continue;
      const cfg = rig && rig.layers[l];
      const additive = Boolean(cfg && cfg.additive);
      const refPose = additive && cfg.refClip >= 0 ? agRefPose(m, cfg) : null;
      m.lflags.fill(0);
      for (let s = 0; s < layer.count; s++) {
        const sample = layer.samples[s];
        if (sample.clip < 0 || !(sample.weight > 0)) continue;
        const clip = m.clips.list[sample.clip];
        const time = agSampleTime(sample.time, clip.duration, sample.loop);
        const channels = clip.channels;
        for (let c = 0; c < channels.length; c++) {
          agEvalChannel(channels[c], time, m.v);
          agLayerAccumulate(m, channels[c], sample.weight, additive, refPose);
        }
      }
      agLayerApply(m, Math.min(1, layer.weight), cfg ? cfg.mask : null, additive);
    }
    if (!(m.record.model && m.record.model.rootMotion === true)) {
      for (let node = 0; node < n; node++) {
        if ((m.flags[node] & 1) && m.rootSet[node]) {
          m.t[node * 3] = m.authT[node * 3];
          m.t[node * 3 + 2] = m.authT[node * 3 + 2];
        }
      }
    }
    const root = m.record.rootTransform || null;
    m.root = root;
    agWorldRange(m, 0, m.order.length, root);
    agSolveIK(m, holder, rig);
    for (let s = 0; s < m.skins.length; s++) {
      const ent = m.skins[s];
      for (let j = 0; j < ent.joints.length; j++) {
        const jn = ent.joints[j];
        agMul(ent.buffer, j * 16, jn >= 0 && m.reached[jn] ? m.world : AG_IDENTITY, jn >= 0 && m.reached[jn] ? jn * 16 : 0, ent.ibm, j * 16);
      }
      if (ent.skin) ent.skin.jointMatrices = ent.buffer;
    }
    m.evalMicros = typeof performance !== "undefined" ? (performance.now() - start) * 1000 : 0;
    agWriteFeedback(m, holder, rig);
  }

  // A layer's reference pose for additive blending when the layer names a
  // reference clip: that clip sampled at refTime, authored pose elsewhere.
  function agRefPose(m: any, cfg: any) {
    if (cfg.refPose && cfg.refPoseClips === m.clips.list) return cfg.refPose;
    const ref = { t: Float64Array.from(m.authT), r: Float64Array.from(m.authR), s: Float64Array.from(m.authS) };
    const index = m.clips.byName.get(cfg.refClipName);
    if (index !== undefined) {
      const clip = m.clips.list[index];
      const out = new Float64Array(4);
      for (let c = 0; c < clip.channels.length; c++) {
        const ch = clip.channels[c];
        agEvalChannel(ch, agSampleTime(cfg.refTime, clip.duration, false), out);
        const dst = ch.code === 0 ? ref.t : ch.code === 1 ? ref.r : ref.s;
        dst.set(out.subarray(0, ch.width), ch.node * ch.width);
      }
    }
    cfg.refPose = ref;
    cfg.refPoseClips = m.clips.list;
    return ref;
  }

  function agWriteFeedback(m: any, holder: any, rig: any) {
    const fb = holder.feedback;
    const chains = rig ? rig.ik.length : 0, looks = rig ? rig.lookAt.length : 0;
    fb[0] = rig ? m.rigHash : 0;
    fb[1] = chains; fb[2] = looks;
    let at = 3;
    for (let i = 0; i < chains; i++) {
      const c = rig.ik[i];
      for (let k = 0; k < 6; k++) fb[at + k] = c.feedback[k];
      at += 6;
    }
    for (let i = 0; i < looks; i++) {
      const c = rig.lookAt[i];
      for (let k = 0; k < 6; k++) fb[at + k] = c.feedback[k];
      at += 6;
    }
    let rootNode = rig && rig.hips >= 0 ? rig.hips : -1;
    if (rootNode < 0) for (let k = 0; k < m.order.length; k++) if (m.rootSet[m.order[k]]) { rootNode = m.order[k]; break; }
    const w = m.world, o = rootNode >= 0 ? rootNode * 16 : -1;
    fb[at] = o >= 0 ? w[o + 12] : 0; fb[at + 1] = o >= 0 ? w[o + 13] : 0; fb[at + 2] = o >= 0 ? w[o + 14] : 0;
    fb[at + 3] = m.evalMicros;
    holder.feedbackCount = at + 4;
  }

  // ---------------------------------------------------------------------------
  // Record hookup
  // ---------------------------------------------------------------------------

  // The graph sampler replaces record.clipSampler. pose() is the allocation-
  // free skinning path; sample() serves models with morph targets or rigid
  // node animation, which take the general Map-based path.
  function agInstallSampler(state: any, record: any, holder: any) {
    if (record.clipSampler && record.clipSampler.__animGraph) return record.clipSampler;
    const fallback = record.clipSampler || null;
    const graph: any = { __animGraph: true, model: null as any, holder, fallback };
    graph.pose = function(rec: any) {
      const m = agModelFor(graph, rec);
      if (!m || !holder.data) return fallback ? fallback.pose(rec) : false;
      if ((rec.morphTargets && rec.morphTargets.length > 0) || (rec.nodeAnimTargets && rec.nodeAnimTargets.length > 0)) return false;
      agEvaluate(m, holder);
      return true;
    };
    graph.sample = function(layers: any, count: number, output: Map<any, any>) {
      const m = agModelFor(graph, record);
      if (!m || !holder.data) return fallback ? fallback.sample(layers, count, output) : output;
      agEvaluate(m, holder);
      const entries = m.entries;
      for (let node = 0; node < m.n; node++) {
        const f = m.flags[node];
        if (!f) continue;
        let entry = entries[node];
        if (!entry) entry = entries[node] = { translation: [0, 0, 0], rotation: [0, 0, 0, 1], scale: [1, 1, 1] };
        for (let k = 0; k < 3; k++) { entry.translation[k] = m.t[node * 3 + k]; entry.scale[k] = m.s[node * 3 + k]; }
        for (let k = 0; k < 4; k++) entry.rotation[k] = m.r[node * 4 + k];
        output.set(node, entry);
      }
      return output;
    };
    record.clipSampler = graph;
    return graph;
  }

  function agModelFor(graph: any, record: any) {
    if (graph.model && graph.model.record === record) return graph.model;
    const clips = record.__animGraphClips;
    if (!clips) {
      agLoadRecordClips(record);
      return null;
    }
    graph.model = agCreateModel(record, clips);
    return graph.model;
  }

  // Fallback for a record created before this chunk could capture its clips
  // (the animation chunk loaded first): parse the model's animations again.
  function agLoadRecordClips(record: any) {
    if (record.__animGraphClipLoad) return;
    const api = window.__gosx_scene3d_gltf_api;
    const src = record.model && typeof record.model.src === "string" ? record.model.src : "";
    if (!api || typeof api.sceneLoadGLTFModel !== "function" || !src) return;
    record.__animGraphClipLoad = Promise.resolve(api.sceneLoadGLTFModel(src)).then(function(asset: any) {
      record.__animGraphClips = asset && Array.isArray(asset.animations) ? asset.animations : [];
      record.poseDirty = true;
    }, function(error: any) {
      console.warn("[gosx] animation graph could not load clips for", src, error && error.message ? error.message : error);
    });
  }

  function agRecordIndex(state: any) {
    const skins = Array.isArray(state._modelSkins) ? state._modelSkins : [];
    let index = state._animGraphIndex;
    if (!index || index.skins !== skins || index.length !== skins.length) {
      index = { skins, length: skins.length, byID: new Map<string, any>() };
      for (let i = 0; i < skins.length; i++) {
        const r = skins[i];
        if (r && r.id && r.clipSampler) index.byID.set(r.id, r);
      }
      state._animGraphIndex = index;
    }
    return index;
  }

  const AG_APPLIED = [{ applied: true, pending: false }, { applied: true, pending: true }];

  // agApply runs inside the mount's applyClipFrame with the mount's own scene
  // state and pose function, so it can find records and re-skin them.
  function agApply(state: any, frame: any, applyPose: any) {
    const pending = Boolean(state._modelHydrationPromise || state._modelHydrationUncommitted);
    const index = agRecordIndex(state);
    for (let i = 0; i < frame.count; i++) {
      const record = index.byID.get(frame.models[i].id);
      if (!record && !pending) return { applied: false, reason: "unknown-model", id: frame.models[i].id };
    }
    for (let i = 0; i < frame.count; i++) {
      const src = frame.models[i];
      const holder = agHolder(state, src.id);
      holder.data = agCopyModel(holder.data, src);
      holder.serial++;
      const record = index.byID.get(src.id);
      if (!record) continue;
      agInstallSampler(state, record, holder);
      applyPose(record, 0, false);
    }
    return AG_APPLIED[pending ? 1 : 0];
  }

  // Wrap the animation API once it exists: capture each sampled record's
  // clips, install the graph sampler on records that already have frame data,
  // and route graph frames that arrive through applyClipFrame.
  function agPatchAnimationAPI(api: any) {
    if (!api || api.__animGraphPatched) return;
    api.__animGraphPatched = true;
    const configure = api.configureSampledRecord;
    if (typeof configure === "function") {
      api.configureSampledRecord = function(state: any, record: any, clips: any[], instanceModel: any) {
        const result = configure.apply(this, arguments);
        if (record) {
          record.__animGraphClips = clips;
          const holders = state && state._animGraphHolders;
          const holder = holders && record.id ? holders.get(record.id) : null;
          if (holder && holder.data) agInstallSampler(state, record, holder);
        }
        return result;
      };
    }
    const applyClipFrame = api.applyClipFrame;
    api.applyClipFrame = function(state: any, models: any, applyPose: any) {
      if (models && models[AG_MARKER] === true) {
        const result = agApply(state, models.frame, applyPose);
        models.result = result;
        if (models.handle) agStates.set(models.handle, state);
        return result;
      }
      return applyClipFrame.apply(this, arguments);
    };
  }

  // The animation chunk assigns window.__gosx_scene3d_animation_api when it
  // runs. If it has not run yet, patch the object as it is assigned, so
  // records created later carry their clips.
  function agHookAnimationAPI() {
    const existing = window.__gosx_scene3d_animation_api;
    if (existing) { agPatchAnimationAPI(existing); return; }
    let value: any = undefined;
    try {
      Object.defineProperty(window, "__gosx_scene3d_animation_api", {
        configurable: true,
        enumerable: true,
        get() { return value; },
        set(next) { value = next; agPatchAnimationAPI(next); },
      });
    } catch (_error) {}
  }

  // ---------------------------------------------------------------------------
  // Public API on window.__gosx.scene3d
  // ---------------------------------------------------------------------------

  const agStates = new WeakMap<any, any>();
  const agDecode = agCreateDecoder();
  const agMarker: any = { [AG_MARKER]: true, frame: null, result: null, handle: null };
  let agCommandRequested = false;

  function agHandle(target: any) {
    const ok = (h: any) => Boolean(h && h.__gosxScene3DCommandReady === true && typeof h.applyClipFrame === "function");
    if (ok(target)) return target;
    if (target && ok(target.__gosxScene3DHandle)) return target.__gosxScene3DHandle;
    const id = typeof target === "string" ? target.trim() : target && typeof target.id === "string" ? target.id.trim() : "";
    if (!id) return null;
    const mount = typeof document !== "undefined" && document.getElementById ? document.getElementById(id) as any : null;
    if (mount && ok(mount.__gosxScene3DHandle)) return mount.__gosxScene3DHandle;
    const engines = window.__gosx && window.__gosx.engines;
    const engine = engines && typeof engines.get === "function" ? engines.get(id) : null;
    return engine && ok(engine.handle) ? engine.handle : null;
  }

  const AG_NOT_READY = { applied: false, pending: false, reason: "not-ready" };

  // dispatchAnimationFrame(target, bytes) applies one frame synchronously.
  // It returns {applied, pending} or {applied: false, reason}; "not-ready"
  // means retry with the next frame (the command bridge or the target mount is
  // still loading).
  function agDispatch(target: any, bytes: any) {
    const handle = agHandle(target);
    if (!handle) return AG_NOT_READY;
    if (!window.__gosx_scene3d_command_bridge) {
      if (!agCommandRequested && window.__gosx && window.__gosx.scene3d && typeof window.__gosx.scene3d.whenReady === "function") {
        agCommandRequested = true;
        Promise.resolve(window.__gosx.scene3d.whenReady(target)).catch(function() { agCommandRequested = false; });
      }
      return AG_NOT_READY;
    }
    const animationApi = window.__gosx_scene3d_animation_api;
    if (!animationApi || typeof animationApi.applyClipFrame !== "function") return AG_NOT_READY;
    agPatchAnimationAPI(animationApi);
    let frame;
    try {
      frame = agDecode(bytes);
    } catch (error: any) {
      return { applied: false, pending: false, reason: String(error && error.message || error) };
    }
    agMarker.frame = frame; agMarker.result = null; agMarker.handle = handle;
    try {
      handle.applyClipFrame(agMarker);
    } catch (error: any) {
      const reason = agMarker.result && agMarker.result.reason ? agMarker.result.reason : String(error && error.message || error);
      return { applied: false, pending: false, reason };
    }
    return agMarker.result || AG_NOT_READY;
  }

  // animationFeedback(target, modelID, out) copies the model's last feedback
  // into out (a Float64Array) and returns the number of values, or 0.
  function agFeedback(target: any, id: any, out: any) {
    const handle = agHandle(target);
    const state = handle ? agStates.get(handle) : null;
    const holder = state && state._animGraphHolders ? state._animGraphHolders.get(String(id)) : null;
    if (!holder || !holder.feedbackCount || !out || typeof out.length !== "number") return 0;
    const count = Math.min(holder.feedbackCount, out.length);
    for (let i = 0; i < count; i++) out[i] = holder.feedback[i];
    return count;
  }

  if (typeof window !== "undefined") {
    agHookAnimationAPI();
    const namespace = window.__gosx || (window.__gosx = {});
    const scene3d = namespace.scene3d || (namespace.scene3d = {});
    scene3d.defineAnimationRig = agDefineRig;
    scene3d.dispatchAnimationFrame = agDispatch;
    scene3d.animationFeedback = agFeedback;
  }
