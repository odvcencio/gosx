  // Procedural point clouds — the client half of scene.PointsGenerator.
  //
  // A deterministic point cloud is a recipe, not data. When a points entry
  // arrives with a `generator` descriptor instead of `positions`/`sizes`,
  // this expands the recipe into the exact same arrays the server would have
  // sent, before the render pipeline ever looks at the entry.
  //
  // Exactness is the whole contract, and platform transcendentals cannot
  // provide it: Go's math.Sin and V8's Math.sin disagree on 19.78% of the
  // seeds this hash uses. So the sine, log and exp below are ports of Go's
  // own pure-Go kernels (src/math), matching scene/points_generator.go step
  // for step. They use only +, -, *, / and comparisons — operations IEEE-754
  // pins down exactly and both languages round identically after each step —
  // so the output is bit-identical to the Go side on every engine.
  //
  // Do not "simplify" these into Math.sin/Math.pow. That silently reintroduces
  // platform drift, and the failure mode is points landing in different places
  // than the server computed.

  var SCENE_POINTS_HASH_DOMAIN_LIMIT = 536870912; // 1<<29, Go's reduceThreshold
  var SCENE_POINTS_SMALLEST_NORMAL = 2.2250738585072014e-308;

  var scenePointsFloatView = null;
  var scenePointsIntView = null;

  function scenePointsBits(x) {
    if (scenePointsFloatView === null) {
      var buf = new ArrayBuffer(8);
      scenePointsFloatView = new Float64Array(buf);
      scenePointsIntView = new Uint32Array(buf);
    }
    scenePointsFloatView[0] = x;
    // [low, high] on little-endian; every engine we target is little-endian.
    return scenePointsIntView;
  }

  function scenePointsFromBits(low, high) {
    scenePointsBits(0);
    scenePointsIntView[0] = low;
    scenePointsIntView[1] = high;
    return scenePointsFloatView[0];
  }

  // Port of Go's math.normalize.
  function scenePointsNormalize(x) {
    if (Math.abs(x) < SCENE_POINTS_SMALLEST_NORMAL) {
      return { frac: x * 4503599627370496 /* 1<<52 */, exp: -52 };
    }
    return { frac: x, exp: 0 };
  }

  // Port of Go's math.Frexp for finite non-zero input.
  function scenePointsFrexp(f) {
    if (f === 0 || !isFinite(f) || Number.isNaN(f)) return { frac: f, exp: 0 };
    var n = scenePointsNormalize(f);
    var bits = scenePointsBits(n.frac);
    var high = bits[1];
    var exp = n.exp + ((high >>> 20) & 0x7ff) - 1023 + 1;
    // Clear the exponent field and set it to -1 + bias (0x3fe).
    var newHigh = (high & ~0x7ff00000) | (0x3fe << 20);
    return { frac: scenePointsFromBits(bits[0], newHigh >>> 0), exp: exp };
  }

  // Port of Go's math.Ldexp.
  function scenePointsLdexp(frac, exp) {
    if (frac === 0 || !isFinite(frac) || Number.isNaN(frac)) return frac;
    var n = scenePointsNormalize(frac);
    exp += n.exp;
    var bits = scenePointsBits(n.frac);
    var high = bits[1];
    exp += ((high >>> 20) & 0x7ff) - 1023;
    if (exp < -1075) return frac < 0 ? -0 : 0;
    if (exp > 1023) return frac < 0 ? -Infinity : Infinity;
    var m = 1;
    if (exp < -1022) {
      exp += 53;
      m = 1.0 / 9007199254740992 /* 1<<53 */;
    }
    var newHigh = (high & ~0x7ff00000) | ((exp + 1023) << 20);
    return m * scenePointsFromBits(bits[0], newHigh >>> 0);
  }

  // Port of the Cody-Waite branch of Go's math.sin.
  function sceneCanonicalSin(x) {
    var PI4A = 7.85398125648498535156e-1;
    var PI4B = 3.77489470793079817668e-8;
    var PI4C = 2.69515142907905952645e-15;
    var M4PI = 1.273239544735162542821171882678754627704620361328125;
    if (x === 0 || Number.isNaN(x)) return x;
    if (!isFinite(x)) return NaN;
    var sign = false;
    if (x < 0) { x = -x; sign = true; }
    if (x >= SCENE_POINTS_HASH_DOMAIN_LIMIT) return NaN;
    var j = Math.floor(x * M4PI);
    var y = j;
    if (j % 2 === 1) { j++; y++; }
    j = j % 8;
    var z = ((x - y * PI4A) - y * PI4B) - y * PI4C;
    if (j > 3) { sign = !sign; j -= 4; }
    var zz = z * z;
    var r;
    if (j === 1 || j === 2) {
      var c0 = -1.13585365213876817300e-11, c1 = 2.08757008419747316778e-9,
          c2 = -2.75573141792967388112e-7, c3 = 2.48015872888517179954e-5,
          c4 = -1.38888888888730564116e-3, c5 = 4.16666666666665929218e-2;
      var pc = ((((c0 * zz + c1) * zz + c2) * zz + c3) * zz + c4);
      pc = pc * zz + c5;
      r = 1.0 - 0.5 * zz + zz * zz * pc;
    } else {
      var s0 = 1.58962301576546568060e-10, s1 = -2.50507477628578072866e-8,
          s2 = 2.75573136213857245213e-6, s3 = -1.98412698295895385996e-4,
          s4 = 8.33333333332211858878e-3, s5 = -1.66666666666666307295e-1;
      var ps = ((((s0 * zz + s1) * zz + s2) * zz + s3) * zz + s4);
      ps = ps * zz + s5;
      r = z + z * zz * ps;
    }
    return sign ? -r : r;
  }

  // Port of Go's math.log for finite positive input.
  function sceneCanonicalLog(x) {
    var Ln2Hi = 6.93147180369123816490e-01, Ln2Lo = 1.90821492927058770002e-10;
    var L1 = 6.666666666666735130e-01, L2 = 3.999999999940941908e-01,
        L3 = 2.857142874366239149e-01, L4 = 2.222219843214978396e-01,
        L5 = 1.818357216161805012e-01, L6 = 1.531383769920937332e-01,
        L7 = 1.479819860511658591e-01;
    var HALF_SQRT2 = 0.707106781186547524400844362104849039;
    if (Number.isNaN(x) || x === Infinity) return x;
    if (x < 0) return NaN;
    if (x === 0) return -Infinity;
    var fe = sceneCanonicalFrexpForLog(x);
    var f1 = fe.frac, ki = fe.exp;
    if (f1 < HALF_SQRT2) { f1 *= 2; ki--; }
    var f = f1 - 1;
    var k = ki;
    var s = f / (2 + f);
    var s2 = s * s;
    var s4 = s2 * s2;
    var t1 = s2 * (L1 + s4 * (L3 + s4 * (L5 + s4 * L7)));
    var t2 = s4 * (L2 + s4 * (L4 + s4 * L6));
    var R = t1 + t2;
    var hfsq = 0.5 * f * f;
    return k * Ln2Hi - ((hfsq - (s * (hfsq + R) + k * Ln2Lo)) - f);
  }

  function sceneCanonicalFrexpForLog(x) {
    return scenePointsFrexp(x);
  }

  // Port of Go's math.exp.
  function sceneCanonicalExp(x) {
    var Ln2Hi = 6.93147180369123816490e-01, Ln2Lo = 1.90821492927058770002e-10;
    var Log2e = 1.44269504088896338700e+00;
    var Overflow = 7.09782712893383973096e+02;
    var Underflow = -7.45133219101941108420e+02;
    var NearZero = 1.0 / 268435456; // 2**-28
    if (Number.isNaN(x)) return x;
    if (x > Overflow) return Infinity;
    if (x < Underflow) return 0;
    if (-NearZero < x && x < NearZero) return 1 + x;
    var k;
    if (x < 0) {
      k = Math.trunc(Log2e * x - 0.5);
    } else {
      k = Math.trunc(Log2e * x + 0.5);
    }
    var hi = x - k * Ln2Hi;
    var lo = k * Ln2Lo;
    return sceneCanonicalExpMulti(hi, lo, k);
  }

  // Port of Go's math.expmulti.
  function sceneCanonicalExpMulti(hi, lo, k) {
    var P1 = 1.66666666666666657415e-01, P2 = -2.77777777770155933842e-03,
        P3 = 6.61375632143793436117e-05, P4 = -1.65339022054652515390e-06,
        P5 = 4.13813679705723846039e-08;
    var r = hi - lo;
    var t = r * r;
    var c = r - t * (P1 + t * (P2 + t * (P3 + t * (P4 + t * P5))));
    var y = 1 - ((lo - (r * c) / (2 - c)) - hi);
    return scenePointsLdexp(y, k);
  }

  // x**y defined as exp(y*log(x)) — matches scene.canonicalPow exactly.
  function sceneCanonicalPow(x, y) {
    if (y === 0 || x === 1) return 1;
    if (y === 1) return x;
    if (Number.isNaN(x) || Number.isNaN(y)) return NaN;
    if (x === 0) return y < 0 ? Infinity : 0;
    if (x < 0) return NaN;
    return sceneCanonicalExp(y * sceneCanonicalLog(x));
  }

  // The canonical scalar hash — matches scene.pointsHash01.
  function scenedPointsHash01(seed) {
    var x = sceneCanonicalSin(seed * 12.9898 + 78.233) * 43758.5453;
    return x - Math.floor(x);
  }

  function scenePointsBoxCoord(center, extent, draw) {
    return center + (draw - 0.5) * extent;
  }

  // Expand a generator descriptor into flat positions and sizes. Returns null
  // when the descriptor names a recipe this runtime does not know, so the
  // caller can degrade the layer instead of drawing undefined data.
  function sceneGeneratePointsArrays(gen, count) {
    if (!gen || !(count > 0)) return null;
    var kind = gen.kind || "box-scatter";
    if (kind !== "box-scatter") return null;
    var stride = gen.stride || 3;
    var seed = gen.seed || 0;
    var ox = gen.offsetX || 0, oy = gen.offsetY || 0,
        oz = gen.offsetZ || 0, os = gen.offsetSize || 0;
    var cx = gen.centerX || 0, cy = gen.centerY || 0, cz = gen.centerZ || 0;
    var ex = gen.extentX || 0, ey = gen.extentY || 0, ez = gen.extentZ || 0;
    var sizeMin = gen.sizeMin || 0;
    var sizeMax = gen.sizeMax || 0;
    var sizeExp = gen.sizeExp || 1;
    var sizeSpan = sizeMax - sizeMin;
    var positions = new Array(count * 3);
    var sizes = new Array(count);
    for (var i = 0; i < count; i++) {
      var base = seed + i * stride;
      positions[i * 3] = scenePointsBoxCoord(cx, ex, scenedPointsHash01(base + ox));
      positions[i * 3 + 1] = scenePointsBoxCoord(cy, ey, scenedPointsHash01(base + oy));
      positions[i * 3 + 2] = scenePointsBoxCoord(cz, ez, scenedPointsHash01(base + oz));
      var draw = scenedPointsHash01(base + os);
      if (sizeExp !== 1) draw = sceneCanonicalPow(draw, sizeExp);
      sizes[i] = sizeMin + draw * sizeSpan;
    }
    return { positions: positions, sizes: sizes };
  }

  // Expand a points entry in place. Mirrors sceneDecompressPointsEntry: the
  // entry leaves this function holding plain float arrays, or holding a zero
  // count if the recipe was not understood.
  function sceneGeneratePointsEntry(entry) {
    if (!entry || !entry.generator || entry.positions) return;
    var count = Math.max(0, Math.floor(entry.count || 0));
    var made = sceneGeneratePointsArrays(entry.generator, count);
    if (!made) {
      // Unknown recipe: draw nothing rather than indexing past an empty
      // buffer. The layer disappears; the rest of the scene is unaffected.
      entry.count = 0;
      delete entry.generator;
      return;
    }
    entry.positions = made.positions;
    if (!entry.sizes) entry.sizes = made.sizes;
    delete entry.generator;
  }
