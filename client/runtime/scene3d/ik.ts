  // ik.ts — inverse kinematics for the animation graph chunk: analytic
  // two-bone chains (legs, arms) with pole vectors, foot placement against
  // game-supplied ground with pelvis drop, and look-at / aim chains.
  //
  // The solver works on the evaluator's posed skeleton (animation-graph.ts):
  // world matrices are current, rotations change in local space, and only
  // the moved subtree's world matrices are recomputed. A world-space rotation
  // R applied to node n with parent world rotation P becomes the local change
  // L' = P^-1 R P L. That identity assumes uniform scale on the parent chain,
  // which holds for exported skeletons (Mixamo's 0.01 armature scale is
  // uniform).

  const IK_EPS = 1e-6;
  // Foot and pelvis offsets ease toward their targets with this time
  // constant (seconds), so stepping onto a ledge does not pop.
  const IK_SMOOTH_SECONDS = 0.06;

  // Math.hypot is several times slower than a square root in V8.
  function ikLen3(x: number, y: number, z: number) { return Math.sqrt(x * x + y * y + z * z); }

  const ikV = new Float64Array(16);
  const ikQ = new Float64Array(8);
  const ikP = new Float64Array(4);
  const ikTarget = new Float64Array(3);

  function ikRotateVec(out: Float64Array, qx: number, qy: number, qz: number, qw: number, x: number, y: number, z: number) {
    const tx = 2 * (qy * z - qz * y), ty = 2 * (qz * x - qx * z), tz = 2 * (qx * y - qy * x);
    out[0] = x + qw * tx + (qy * tz - qz * ty);
    out[1] = y + qw * ty + (qz * tx - qx * tz);
    out[2] = z + qw * tz + (qx * ty - qy * tx);
  }

  // World position of a node (translation column of its world matrix).
  function ikPos(m: any, node: number, out: Float64Array, o: number) {
    const w = m.world, b = node * 16;
    out[o] = w[b + 12]; out[o + 1] = w[b + 13]; out[o + 2] = w[b + 14];
  }

  // Rotation part of a 4x4 world matrix, scale removed, as a quaternion.
  function ikWorldRot(mat: ArrayLike<number>, base: number, out: Float64Array, o: number) {
    const sx = ikLen3(mat[base], mat[base + 1], mat[base + 2]) || 1;
    const sy = ikLen3(mat[base + 4], mat[base + 5], mat[base + 6]) || 1;
    const sz = ikLen3(mat[base + 8], mat[base + 9], mat[base + 10]) || 1;
    const m00 = mat[base] / sx, m10 = mat[base + 1] / sx, m20 = mat[base + 2] / sx;
    const m01 = mat[base + 4] / sy, m11 = mat[base + 5] / sy, m21 = mat[base + 6] / sy;
    const m02 = mat[base + 8] / sz, m12 = mat[base + 9] / sz, m22 = mat[base + 10] / sz;
    const trace = m00 + m11 + m22;
    let x, y, z, w;
    if (trace > 0) {
      const s = 0.5 / Math.sqrt(trace + 1);
      w = 0.25 / s; x = (m21 - m12) * s; y = (m02 - m20) * s; z = (m10 - m01) * s;
    } else if (m00 > m11 && m00 > m22) {
      const s = 2 * Math.sqrt(1 + m00 - m11 - m22);
      w = (m21 - m12) / s; x = 0.25 * s; y = (m01 + m10) / s; z = (m02 + m20) / s;
    } else if (m11 > m22) {
      const s = 2 * Math.sqrt(1 + m11 - m00 - m22);
      w = (m02 - m20) / s; x = (m01 + m10) / s; y = 0.25 * s; z = (m12 + m21) / s;
    } else {
      const s = 2 * Math.sqrt(1 + m22 - m00 - m11);
      w = (m10 - m01) / s; x = (m02 + m20) / s; y = (m12 + m21) / s; z = 0.25 * s;
    }
    const len = Math.sqrt(x * x + y * y + z * z + w * w) || 1;
    out[o] = x / len; out[o + 1] = y / len; out[o + 2] = z / len; out[o + 3] = w / len;
  }

  // Rotation taking direction (ax, ay, az) toward (bx, by, bz) by weight
  // times the angle between them (exact, not nlerp, so limits and partial
  // weights hold for large angles), written to out[o..o+4).
  function ikFromTo(out: Float64Array, o: number, ax: number, ay: number, az: number, bx: number, by: number, bz: number, weight: number) {
    const la = ikLen3(ax, ay, az), lb = ikLen3(bx, by, bz);
    if (la < IK_EPS || lb < IK_EPS || !(weight > 0)) { out[o] = 0; out[o + 1] = 0; out[o + 2] = 0; out[o + 3] = 1; return; }
    ax /= la; ay /= la; az /= la; bx /= lb; by /= lb; bz /= lb;
    let x = ay * bz - az * by, y = az * bx - ax * bz, z = ax * by - ay * bx;
    const d = ax * bx + ay * by + az * bz;
    let s = ikLen3(x, y, z);
    if (s < 1e-9) {
      if (d > 0) { out[o] = 0; out[o + 1] = 0; out[o + 2] = 0; out[o + 3] = 1; return; }
      // Opposite directions: any axis perpendicular to a.
      x = 0; y = -az; z = ay;
      if (ikLen3(y, z, 0) < IK_EPS) { x = az; y = 0; z = -ax; }
      s = ikLen3(x, y, z);
    }
    const half = 0.5 * weight * Math.atan2(ikLen3(ay * bz - az * by, az * bx - ax * bz, ax * by - ay * bx), d);
    const k = Math.sin(half) / s;
    out[o] = x * k; out[o + 1] = y * k; out[o + 2] = z * k; out[o + 3] = Math.cos(half);
  }

  // Apply world rotation R (ikQ[0..4)) to node: L' = P^-1 R P L.
  function ikRotateNode(m: any, node: number, R: Float64Array, ro: number) {
    const p = m.parent[node];
    if (p >= 0 && m.reached[p]) ikWorldRot(m.world, p * 16, ikP, 0);
    else if (m.root) ikWorldRot(m.root, 0, ikP, 0);
    else { ikP[0] = 0; ikP[1] = 0; ikP[2] = 0; ikP[3] = 1; }
    // tmp = R * P
    agQuatMul(ikQ, 4, R, ro, ikP, 0);
    // tmp = P^-1 * tmp
    agQuatDelta(ikQ, 4, ikP, 0, ikQ, 4);
    // L' = tmp * L
    agQuatMul(m.r, node * 4, ikQ, 4, m.r, node * 4);
    agNormalizeQuat(m.r, node * 4);
    m.flags[node] |= 2;
  }

  function ikRefresh(m: any, node: number) {
    const from = m.pos[node];
    if (from >= 0) agWorldRange(m, from, m.end[from], m.root);
  }

  // Solve a two-bone chain a -> b -> c toward target (tx, ty, tz). The end
  // position blends from its animated position by weight. The bend plane
  // contains the pole when given, else the animated mid joint. Out-of-reach
  // targets stretch the chain straight toward the target.
  function ikTwoBone(m: any, a: number, b: number, c: number, tx: number, ty: number, tz: number, pole: Float64Array | null, weight: number) {
    const v = ikV;
    ikPos(m, a, v, 0); ikPos(m, b, v, 3); ikPos(m, c, v, 6);
    const ax = v[0], ay = v[1], az = v[2];
    const l1 = ikLen3(v[3] - ax, v[4] - ay, v[5] - az), l2 = ikLen3(v[6] - v[3], v[7] - v[4], v[8] - v[5]);
    if (l1 < IK_EPS || l2 < IK_EPS) return;
    tx = v[6] + (tx - v[6]) * weight; ty = v[7] + (ty - v[7]) * weight; tz = v[8] + (tz - v[8]) * weight;
    let ux = tx - ax, uy = ty - ay, uz = tz - az;
    let d = ikLen3(ux, uy, uz);
    if (d < IK_EPS) return;
    ux /= d; uy /= d; uz /= d;
    d = Math.min(Math.max(d, Math.abs(l1 - l2) + 1e-4), l1 + l2 - 1e-4);
    // Bend direction: with a pole, the part of (pole - a) perpendicular to
    // u. Without one, keep the animated bend: the part of (b - a)
    // perpendicular to the animated a -> c line, reprojected onto the new
    // line, so a knee that bends forward in the clip still bends forward.
    let vx, vy, vz;
    if (pole) {
      vx = pole[0] - ax; vy = pole[1] - ay; vz = pole[2] - az;
    } else {
      let cx0 = v[6] - ax, cy0 = v[7] - ay, cz0 = v[8] - az;
      const lc = ikLen3(cx0, cy0, cz0) || 1;
      cx0 /= lc; cy0 /= lc; cz0 /= lc;
      const bx0 = v[3] - ax, by0 = v[4] - ay, bz0 = v[5] - az;
      const k0 = bx0 * cx0 + by0 * cy0 + bz0 * cz0;
      vx = bx0 - cx0 * k0; vy = by0 - cy0 * k0; vz = bz0 - cz0 * k0;
    }
    let dot = vx * ux + vy * uy + vz * uz;
    vx -= ux * dot; vy -= uy * dot; vz -= uz * dot;
    let lv = ikLen3(vx, vy, vz);
    if (lv < IK_EPS) {
      // Straight animated chain or pole on the target line: bend toward the
      // animated knee, then toward any perpendicular.
      vx = v[3] - ax; vy = v[4] - ay; vz = v[5] - az;
      dot = vx * ux + vy * uy + vz * uz;
      vx -= ux * dot; vy -= uy * dot; vz -= uz * dot;
      lv = ikLen3(vx, vy, vz);
      if (lv < IK_EPS) { vx = -uy; vy = ux; vz = 0; lv = ikLen3(vx, vy, 0); if (lv < IK_EPS) { vx = 0; vy = -uz; vz = uy; lv = ikLen3(vy, vz, 0) || 1; } }
    }
    vx /= lv; vy /= lv; vz /= lv;
    const cos = Math.max(-1, Math.min(1, (l1 * l1 + d * d - l2 * l2) / (2 * l1 * d)));
    const sin = Math.sqrt(1 - cos * cos);
    const bx = ax + l1 * (cos * ux + sin * vx), by = ay + l1 * (cos * uy + sin * vy), bz = az + l1 * (cos * uz + sin * vz);
    const cx = ax + d * ux, cy = ay + d * uy, cz = az + d * uz;
    ikFromTo(ikQ, 0, v[3] - ax, v[4] - ay, v[5] - az, bx - ax, by - ay, bz - az, 1);
    ikRotateNode(m, a, ikQ, 0);
    ikRefresh(m, a);
    ikPos(m, b, v, 3); ikPos(m, c, v, 6);
    ikFromTo(ikQ, 0, v[6] - v[3], v[7] - v[4], v[8] - v[5], cx - v[3], cy - v[4], cz - v[5], 1);
    ikRotateNode(m, b, ikQ, 0);
    ikRefresh(m, b);
  }

  // Rotate a look-at chain so the pivot's forward axis points at target.
  // Each bone takes its weight's share of the rotation still remaining; the
  // last bone finishes it. The aim direction is clamped to maxAngle from the
  // animated forward direction.
  function ikLookAt(m: any, chain: any, tx: number, ty: number, tz: number, weight: number) {
    const v = ikV, q = ikQ, pivot = chain.pivot, al = chain.axisLocal;
    ikWorldRot(m.world, pivot * 16, ikP, 0);
    ikRotateVec(v, ikP[0], ikP[1], ikP[2], ikP[3], al[0], al[1], al[2]);
    ikPos(m, pivot, v, 3);
    let dx = tx - v[3], dy = ty - v[4], dz = tz - v[5];
    const ld = ikLen3(dx, dy, dz);
    if (ld < IK_EPS) return;
    dx /= ld; dy /= ld; dz /= ld;
    const fx = v[0], fy = v[1], fz = v[2];
    const angle = Math.acos(Math.max(-1, Math.min(1, fx * dx + fy * dy + fz * dz)));
    if (angle > chain.maxAngle) {
      ikFromTo(q, 0, fx, fy, fz, dx, dy, dz, chain.maxAngle / angle);
      ikRotateVec(v, q[0], q[1], q[2], q[3], fx, fy, fz);
      dx = v[0]; dy = v[1]; dz = v[2];
    }
    const bones = chain.bones, weights = chain.weights;
    let remaining = 0;
    for (let i = 0; i < bones.length; i++) remaining += weights.length ? weights[i] : 1;
    for (let i = 0; i < bones.length; i++) {
      const share = weights.length ? weights[i] : 1;
      const f = i === bones.length - 1 || remaining <= IK_EPS ? 1 : share / remaining;
      remaining -= share;
      ikWorldRot(m.world, pivot * 16, ikP, 0);
      ikRotateVec(v, ikP[0], ikP[1], ikP[2], ikP[3], al[0], al[1], al[2]);
      ikFromTo(q, 0, v[0], v[1], v[2], dx, dy, dz, f * weight);
      ikRotateNode(m, bones[i], q, 0);
      // Only the path down to the pivot matters for the next bone; the full
      // subtree refreshes once after the loop.
      ikRefreshPath(m, chain.path, chain.pathStart[i]);
    }
    ikRefresh(m, bones[0]);
  }

  // Recompute world matrices along a top-down node path from index start.
  function ikRefreshPath(m: any, path: Int32Array, start: number) {
    const local = m.localScratch;
    for (let k = start; k < path.length; k++) {
      const node = path[k], p = m.parent[node];
      agTRS(local, 0, m.t, node * 3, m.r, node * 4, m.s, node * 3);
      if (p >= 0 && m.reached[p]) agMul(m.world, node * 16, m.world, p * 16, local, 0);
      else if (m.root) agMul(m.world, node * 16, m.root, 0, local, 0);
      else m.world.set(local, node * 16);
    }
  }

  function ikSmooth(current: number, target: number, dt: number) {
    if (!(dt > 0)) return current;
    return current + (target - current) * (1 - Math.exp(-dt / IK_SMOOTH_SECONDS));
  }

  // agSolveIK runs after layering and root-motion handling, on current world
  // matrices. Order: pelvis drop, foot chains, other two-bone chains, look-at
  // chains. Smoothing advances only on a new frame (holder.serial).
  function agSolveIK(m: any, holder: any, rig: any) {
    if (!rig) return;
    const data = holder.data;
    const fresh = rig.lastSerial !== holder.serial;
    // The first frame snaps offsets to their targets; later frames ease.
    const first = rig.lastSerial < 0;
    rig.lastSerial = holder.serial;
    const dt = fresh ? data.dt : 0;
    const chains = rig.ik, looks = rig.lookAt, v = ikV;
    for (let i = 0; i < chains.length; i++) chains[i].input = null;
    for (let i = 0; i < looks.length; i++) looks[i].input = null;
    for (let k = 0; k < data.ikCount; k++) {
      const e = data.ik[k], c = chains[e.chain];
      if (c && c.valid && e.weight > 0) c.input = e;
    }
    for (let k = 0; k < data.lookCount; k++) {
      const e = data.look[k], c = looks[e.chain];
      if (c && c.valid && e.weight > 0) c.input = e;
    }
    // Animated end positions, before any IK: the probe points for ground
    // raycasts, and the default bend reference.
    for (let i = 0; i < chains.length; i++) {
      const c = chains[i];
      if (!c.valid) continue;
      ikPos(m, c.c, c.feedback, 0);
      for (let k = 0; k < 3; k++) c.feedback[3 + k] = c.feedback[6 + k] = c.feedback[k];
    }
    // Foot offsets: the animation assumes flat ground at the model origin;
    // each planted foot moves by (ground height - origin height), weighted.
    // The pelvis follows the lowest target offset (not the eased foot
    // offsets), so each value eases exactly once.
    const originY = m.root ? m.root[13] : 0;
    let drop = 0;
    for (let i = 0; i < chains.length; i++) {
      const c = chains[i];
      if (!c.valid || !c.foot) continue;
      const e = c.input;
      const target = e && (e.flags & 4) ? (e.height - originY) * e.weight : 0;
      if (first) c.offset = target;
      else if (fresh) c.offset = ikSmooth(c.offset, target, dt);
      if (target < drop) drop = target;
    }
    if (rig.hips >= 0 && rig.maxDrop > 0) {
      const want = Math.max(drop, -rig.maxDrop);
      if (first) rig.pelvis = want;
      else if (fresh) rig.pelvis = ikSmooth(rig.pelvis, want, dt);
      if (Math.abs(rig.pelvis) > 1e-7) {
        // World offset (0, pelvis, 0) into the hips' parent space.
        const p = m.parent[rig.hips];
        const pm = p >= 0 && m.reached[p] ? m.world : m.root;
        const base = p >= 0 && m.reached[p] ? p * 16 : 0;
        if (pm) {
          // Inverse of the parent's linear part applied to (0, d, 0): for a
          // rotation-and-uniform-scale matrix that is R^T (0, d, 0) / s^2.
          const s2 = pm[base + 4] * pm[base + 4] + pm[base + 5] * pm[base + 5] + pm[base + 6] * pm[base + 6] || 1;
          const d = rig.pelvis / s2;
          m.t[rig.hips * 3] += pm[base + 1] * d;
          m.t[rig.hips * 3 + 1] += pm[base + 5] * d;
          m.t[rig.hips * 3 + 2] += pm[base + 9] * d;
        } else {
          m.t[rig.hips * 3 + 1] += rig.pelvis;
        }
        m.flags[rig.hips] |= 1;
        // Moving the hips by d in world space moves its whole subtree by the
        // same world translation, so shift translation columns instead of
        // recomputing every matrix.
        const from = m.pos[rig.hips], to = from >= 0 ? m.end[from] : from, w = m.world;
        for (let k = from; k < to; k++) w[m.order[k] * 16 + 13] += rig.pelvis;
      }
    }
    for (let i = 0; i < chains.length; i++) {
      const c = chains[i];
      if (!c.valid || !c.foot || !c.input || !(c.input.flags & 4)) continue;
      const e = c.input, fb = c.feedback;
      ikTwoBone(m, c.a, c.b, c.c, fb[0], fb[1] + c.offset, fb[2], e.flags & 2 ? e.pole : null, 1);
      if (c.align) {
        ikWorldRot(m.world, c.c * 16, ikP, 0);
        ikFromTo(ikQ, 0, 0, 1, 0, e.normal[0], e.normal[1], e.normal[2], e.weight);
        ikRotateNode(m, c.c, ikQ, 0);
        ikRefresh(m, c.c);
      }
    }
    for (let i = 0; i < chains.length; i++) {
      const c = chains[i], e = c.input;
      if (!c.valid || !e || !(e.flags & 9) || (c.foot && (e.flags & 4))) continue;
      const t = ikTarget;
      if (e.flags & 8) {
        // A bone of another model: resolved from its pose this frame.
        if (!agBoneTarget(holder, e, t)) continue;
      } else {
        t[0] = e.target[0]; t[1] = e.target[1]; t[2] = e.target[2];
      }
      c.feedback[6] = t[0]; c.feedback[7] = t[1]; c.feedback[8] = t[2];
      ikTwoBone(m, c.a, c.b, c.c, t[0], t[1], t[2], e.flags & 2 ? e.pole : null, e.weight);
    }
    for (let i = 0; i < looks.length; i++) {
      const c = looks[i];
      if (!c.valid) continue;
      if (c.input) ikLookAt(m, c, c.input.target[0], c.input.target[1], c.input.target[2], c.input.weight);
      ikWorldRot(m.world, c.pivot * 16, ikP, 0);
      ikRotateVec(v, ikP[0], ikP[1], ikP[2], ikP[3], c.axisLocal[0], c.axisLocal[1], c.axisLocal[2]);
      ikPos(m, c.pivot, c.feedback, 0);
      c.feedback[3] = v[0]; c.feedback[4] = v[1]; c.feedback[5] = v[2];
    }
    for (let i = 0; i < chains.length; i++) {
      const c = chains[i];
      if (c.valid) ikPos(m, c.c, c.feedback, 3);
    }
  }
