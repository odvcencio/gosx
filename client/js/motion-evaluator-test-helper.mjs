// Test-only port of the Go timeline evaluator. The live frame scheduler uses
// signals and the spring integrator in 06-motion-core.ts; this helper exists
// only to keep coverage for the packed timeline/write-buffer golden corpus.
const number = (value, fallback = 0) => Number.isFinite(Number(value)) ? Number(value) : fallback;
const clamp = (value, min, max) => Math.max(min, Math.min(max, value));

function bezier(u, c1, c2) {
  const v = 1 - u;
  return 3 * v * v * u * c1 + 3 * v * u * u * c2 + u * u * u;
}

function bezierDerivative(u, x1, x2) {
  return 3 * x1 * (1 - u) * (1 - 3 * u) + 3 * x2 * u * (2 - 3 * u) + 3 * u * u;
}

function ease(t, raw = {}) {
  const kind = number(raw.Kind ?? raw.kind);
  const args = Array.isArray(raw.Args) ? raw.Args : (Array.isArray(raw.args) ? raw.args : []);
  const x = clamp(t, 0, 1);
  if (x <= 0 || x >= 1) return x;
  if (kind === 1) return Math.pow(x, args.length ? args[0] : 2);
  if (kind === 2) return 1 - Math.pow(1 - x, args.length ? args[0] : 2);
  if (kind === 3) {
    const power = args.length ? args[0] : 2;
    return x < 0.5 ? 0.5 * Math.pow(2 * x, power) : 1 - 0.5 * Math.pow(2 - 2 * x, power);
  }
  if (kind === 4) {
    const [x1, y1, x2, y2] = args.length >= 4 ? args : [0, 0, 0, 0];
    let u = x;
    for (let i = 0; i < 8; i++) {
      const error = bezier(u, x1, x2) - x;
      const derivative = bezierDerivative(u, x1, x2);
      if (Math.abs(derivative) < 1e-10) break;
      u = clamp(u - error / derivative, 0, 1);
      if (Math.abs(error) < 1e-7) break;
    }
    if (Math.abs(bezier(u, x1, x2) - x) > 1e-5) {
      let lo = 0, hi = 1;
      for (let i = 0; i < 30; i++) {
        const mid = (lo + hi) * 0.5;
        if (bezier(mid, x1, x2) < x) lo = mid;
        else hi = mid;
        if (hi - lo < 1e-7) break;
      }
      u = (lo + hi) * 0.5;
    }
    return bezier(u, y1, y2);
  }
  if (kind === 5) {
    const steps = args.length && args[0] >= 1 ? args[0] : 4;
    return Math.floor(x * steps) / steps;
  }
  if (kind >= 6 && kind <= 8) {
    const s = args.length ? args[0] : 1.70158;
    if (kind === 6) return (s + 1) * x * x * x - s * x * x;
    if (kind === 7) {
      const q = x - 1;
      return 1 + (s + 1) * q * q * q + s * q * q;
    }
    const s2 = s * 1.525, c3 = s2 + 1;
    if (x < 0.5) {
      const q = 2 * x;
      return 0.5 * (c3 * q * q * q - s2 * q * q);
    }
    const q = 2 * x - 2;
    return 0.5 * (c3 * q * q * q + s2 * q * q) + 1;
  }
  return x;
}

function components(value, arity) {
  const width = arity === 0 ? 1 : arity === 1 ? 2 : arity === 2 ? 3 : 4;
  const floats = value && Array.isArray(value.F) ? value.F : [];
  return Array.from({ length: width }, (_, index) => number(floats[index]));
}

function slerp(a, b, t) {
  let bx = b[0], by = b[1], bz = b[2], bw = b[3];
  let dot = a[0] * bx + a[1] * by + a[2] * bz + a[3] * bw;
  if (dot < 0) { bx = -bx; by = -by; bz = -bz; bw = -bw; dot = -dot; }
  if (dot > 0.9995) {
    const values = [a[0] + t * (bx - a[0]), a[1] + t * (by - a[1]), a[2] + t * (bz - a[2]), a[3] + t * (bw - a[3])];
    const inverse = 1 / Math.sqrt(values.reduce((sum, value) => sum + value * value, 0) || 1);
    return values.map((value) => value * inverse);
  }
  dot = clamp(dot, -1, 1);
  const theta = Math.acos(dot), sinTheta = Math.sin(theta);
  const wa = Math.sin((1 - t) * theta) / sinTheta, wb = Math.sin(t * theta) / sinTheta;
  return [wa * a[0] + wb * bx, wa * a[1] + wb * by, wa * a[2] + wb * bz, wa * a[3] + wb * bw];
}

function lerpValue(a, b, t, arity) {
  const av = components(a, arity), bv = components(b, arity);
  if (arity === 4) return slerp(av, bv, t);
  return av.map((value, index) => value + (bv[index] - value) * t);
}

function cubicValue(a, outTangent, b, inTangent, delta, t, arity) {
  const av = components(a, arity), ov = components(outTangent, arity);
  const bv = components(b, arity), iv = components(inTangent, arity);
  const t2 = t * t, t3 = t2 * t;
  const h00 = 2 * t3 - 3 * t2 + 1, h10 = t3 - 2 * t2 + t;
  const h01 = -2 * t3 + 3 * t2, h11 = t3 - t2;
  const out = av.map((value, index) => h00 * value + delta * h10 * ov[index] + h01 * bv[index] + delta * h11 * iv[index]);
  if (arity === 4) {
    const magnitude = Math.sqrt(out.reduce((sum, value) => sum + value * value, 0));
    if (magnitude < 1e-15) return [0, 0, 0, 1];
    return out.map((value) => value / magnitude);
  }
  return out;
}

function springDuration(spring) {
  const mass = number(spring.Mass ?? spring.mass, 1) || 1;
  const stiffness = number(spring.Stiffness ?? spring.stiffness, 100) || 100;
  const damping = number(spring.Damping ?? spring.damping, 10) || 10;
  const omega0 = Math.sqrt(stiffness / mass);
  const zeta = damping / (2 * Math.sqrt(stiffness * mass));
  const rate = zeta >= 1 ? omega0 * (zeta - Math.sqrt(zeta * zeta - 1)) : zeta * omega0;
  if (rate <= 0) return 10;
  const result = zeta >= 1
    ? 1.4 * ((-Math.log(1e-3) + Math.log(1 + rate * (-Math.log(1e-3) / rate))) / rate)
    : 1.4 * (-Math.log(1e-3) / rate);
  return Math.min(10, result);
}

function springValue(from, to, t, spring) {
  if (t <= 0) return from;
  if (t >= springDuration(spring)) return to;
  const mass = number(spring.Mass ?? spring.mass, 1) || 1;
  const stiffness = number(spring.Stiffness ?? spring.stiffness, 100) || 100;
  const damping = number(spring.Damping ?? spring.damping, 10) || 10;
  let velocity = number(spring.Velocity ?? spring.velocity, 0), value = from;
  for (let i = 0, steps = Math.floor(t / (1 / 240)); i < steps; i++) {
    const force = -stiffness * (value - to) - damping * velocity;
    velocity += (force / mass) * (1 / 240);
    value += velocity * (1 / 240);
  }
  return value;
}

export function evaluateTimeline(timeline, time, reducedMotion = false) {
  const writes = [];
  function visit(current, baseOffset) {
    for (const child of current && (current.Children || current.children) || []) {
      if (!child) continue;
      const at = child.At || child.at || {};
      const positionKind = number(at.Kind ?? at.kind);
      const start = baseOffset + (positionKind === 0 ? number(at.Val ?? at.val) : 0);
      const track = child.Track || child.track;
      if (track) {
        const generator = track.Gen || track.gen;
        const targetID = number(track.TargetID ?? track.targetID), propID = number(track.PropID ?? track.propID);
        if (generator && number(generator.Kind ?? generator.kind) === 2) {
          const base = generator.Base || generator.base || {};
          const values = components(base, number(base.Arity ?? base.arity));
          const spring = generator.Spring || generator.spring || {};
          writes.push([targetID, propID, 0, reducedMotion ? number(values[1]) : springValue(number(values[0]), number(values[1]), time, spring)]);
        } else {
          const keys = track.Keys || track.keys || [];
          if (keys.length) {
            const first = keys[0], last = keys[keys.length - 1];
            const firstValue = first.Value || first.value || {}, lastValue = last.Value || last.value || {};
            const arity = number(firstValue.Arity ?? firstValue.arity);
            let value;
            if (reducedMotion) value = components(lastValue, arity);
            else {
              const local = time - start;
              if (local <= number(first.T ?? first.t)) value = components(firstValue, arity);
              else if (local >= number(last.T ?? last.t)) value = components(lastValue, arity);
              else {
                let index = 0;
                while (index < keys.length - 2 && number(keys[index + 1].T ?? keys[index + 1].t) <= local) index++;
                const ka = keys[index], kb = keys[index + 1];
                const ta = number(ka.T ?? ka.t), tb = number(kb.T ?? kb.t);
                const alpha = (local - ta) / (tb - ta);
                const interpolation = number(track.Interp ?? track.interp);
                const va = ka.Value || ka.value || {}, vb = kb.Value || kb.value || {};
                if (interpolation === 1) value = components(va, arity);
                else if (interpolation === 2 && (ka.OutTangent || ka.outTangent) && (kb.InTangent || kb.inTangent)) {
                  value = cubicValue(va, ka.OutTangent || ka.outTangent, vb, kb.InTangent || kb.inTangent, tb - ta, alpha, arity);
                } else {
                  const easeSpec = ka.Ease || ka.ease || track.Ease || track.ease || {};
                  value = lerpValue(va, vb, ease(alpha, easeSpec), arity);
                }
              }
            }
            writes.push([targetID, propID, arity, ...value]);
          }
        }
      }
      const sub = child.Sub || child.sub;
      if (sub) visit(sub, start);
    }
  }
  visit(timeline, 0);
  return writes;
}
