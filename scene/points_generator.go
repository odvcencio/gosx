package scene

import "math"

// Procedural point clouds.
//
// A deterministic point cloud carries no information beyond its recipe: a
// seed, a count, and a handful of scalars. Serializing the expanded arrays
// makes the client pay for data it can derive. PointsGenerator replaces the
// arrays with the recipe and the client expands it at mount time.
//
// The recipe is only useful if both sides agree exactly. Go's math.Sin and
// V8's Math.sin do NOT agree: measured over seeds 0..20000 of the classic
// fract(sin(s*12.9898+78.233)*43758.5453) hash, 19.78% of results differ,
// with a peak divergence of 7.276e-12 (one quantum of the post-multiply
// representation). The divergence is small but unbounded in principle — an
// argument that lands within one quantum of an integer makes the two sides
// disagree about floor(), which moves a point across the full extent of its
// axis. Platform transcendentals are therefore unusable as a shared spec.
//
// This file defines the shared spec instead. canonicalSin, canonicalLog and
// canonicalExp are ports of Go's own pure-Go implementations (src/math),
// restricted to the finite branches a point generator can reach, and written
// so that every floating-point step is individually rounded. The matching
// JavaScript lives in client/js/bootstrap-src/11b-scene-points-generate.js.
// Both sides use only +, -, *, / and comparisons, all of which IEEE-754
// specifies exactly and both languages mandate. The result is bit-identical
// output, verified over the full seed range by TestPointsGeneratorGoldenBits
// and its JavaScript twin.
//
// A note on fused multiply-add: Go permits an implementation to fuse a*b+c
// into a single instruction with only one rounding (spec, "Floating-point
// operators"), and does so on arm64 and ppc64. That would silently break
// parity with JavaScript, which mandates a rounding per operation. An
// explicit float64() conversion of a product forces the intermediate
// rounding and blocks fusion. Every multiply feeding an add below is wrapped
// that way. On amd64 the conversions are no-ops, which is why the canonical
// functions reproduce Go's stdlib results bit-for-bit there.

// PointsGeneratorKind names a procedural point-cloud recipe.
type PointsGeneratorKind = string

// PointsGenBoxScatter scatters Count points through an axis-aligned box.
// Each coordinate is an independent hash draw mapped onto Center ± Extent/2,
// and each size is a power-biased draw between SizeMin and SizeMax. It
// covers the common "field of stars/dust/sparks" layer.
const PointsGenBoxScatter PointsGeneratorKind = "box-scatter"

// PointsGenerator is a procedural replacement for Points.Positions and
// Points.Sizes. When set, the lowered scene carries this descriptor instead
// of the expanded arrays and the client runtime regenerates them at mount.
//
// The hash draw for particle i on lane L is:
//
//	hash01(Seed + i*Stride + Offset<L>)
//
// Stride and the per-lane offsets are explicit rather than implied because
// they are part of the observable output: a layer authored as i*3 with the
// size lane at +7 draws different values than one authored as i*4 with the
// size lane at +3, and existing scenes depend on their exact arrangement.
type PointsGenerator struct {
	// Kind selects the recipe. Empty is treated as PointsGenBoxScatter.
	Kind PointsGeneratorKind
	// Seed offsets every lane index. Two layers that differ only in Seed
	// produce independent clouds.
	Seed int
	// Stride advances the lane index per particle. Zero means 3.
	Stride int
	// OffsetX/Y/Z/Size place each lane within a particle's stride window.
	// Offsets may exceed Stride and may collide with a neighbouring
	// particle's lanes; that is a property of the authored recipe, not a
	// defect, and is reproduced faithfully.
	OffsetX    int
	OffsetY    int
	OffsetZ    int
	OffsetSize int
	// Center is the box midpoint; Extent is its full width per axis. A
	// coordinate is Center + (draw-0.5)*Extent.
	Center Vector3
	Extent Vector3
	// SizeMin/SizeMax bound the per-point size. SizeExponent biases the
	// distribution: 1 is uniform, values above 1 crowd toward SizeMin.
	// Zero is treated as 1, which skips the power entirely.
	SizeMin      float64
	SizeMax      float64
	SizeExponent float64
}

// normalized returns a copy with defaults applied, so Go and the client
// runtime resolve the same effective descriptor from a sparse one.
func (g PointsGenerator) normalized() PointsGenerator {
	out := g
	if out.Kind == "" {
		out.Kind = PointsGenBoxScatter
	}
	if out.Stride == 0 {
		out.Stride = 3
	}
	if out.SizeExponent == 0 {
		out.SizeExponent = 1
	}
	return out
}

// Supported reports whether this descriptor names a recipe the runtime can
// expand. An unsupported descriptor is a build-time authoring error; the
// client degrades to an empty layer rather than rendering garbage.
func (g PointsGenerator) Supported() bool {
	return g.normalized().Kind == PointsGenBoxScatter
}

// lowerIR converts the authored descriptor into its wire form, resolving
// defaults first so the payload never depends on the client agreeing about
// them.
func (g PointsGenerator) lowerIR() *PointsGeneratorIR {
	n := g.normalized()
	return &PointsGeneratorIR{
		Kind:       n.Kind,
		Seed:       n.Seed,
		Stride:     n.Stride,
		OffsetX:    n.OffsetX,
		OffsetY:    n.OffsetY,
		OffsetZ:    n.OffsetZ,
		OffsetSize: n.OffsetSize,
		CenterX:    n.Center.X,
		CenterY:    n.Center.Y,
		CenterZ:    n.Center.Z,
		ExtentX:    n.Extent.X,
		ExtentY:    n.Extent.Y,
		ExtentZ:    n.Extent.Z,
		SizeMin:    n.SizeMin,
		SizeMax:    n.SizeMax,
		SizeExp:    n.SizeExponent,
	}
}

// Generate expands the descriptor into the same positions and sizes the
// client runtime produces. Server-side consumers that genuinely need the
// arrays — tests, GLB baking, geometry queries — call this instead of
// shipping them to the browser.
//
// Returns nil, nil when count is not positive or the kind is unsupported.
func (g PointsGenerator) Generate(count int) ([]Vector3, []float64) {
	if count <= 0 {
		return nil, nil
	}
	n := g.normalized()
	if n.Kind != PointsGenBoxScatter {
		return nil, nil
	}
	positions := make([]Vector3, count)
	sizes := make([]float64, count)
	sizeSpan := n.SizeMax - n.SizeMin
	for i := 0; i < count; i++ {
		base := n.Seed + i*n.Stride
		positions[i] = Vector3{
			X: boxCoord(n.Center.X, n.Extent.X, pointsHash01(base+n.OffsetX)),
			Y: boxCoord(n.Center.Y, n.Extent.Y, pointsHash01(base+n.OffsetY)),
			Z: boxCoord(n.Center.Z, n.Extent.Z, pointsHash01(base+n.OffsetZ)),
		}
		draw := pointsHash01(base + n.OffsetSize)
		if n.SizeExponent != 1 {
			draw = canonicalPow(draw, n.SizeExponent)
		}
		sizes[i] = n.SizeMin + float64(draw*sizeSpan)
	}
	return positions, sizes
}

// boxCoord maps a [0,1) draw onto center ± extent/2.
func boxCoord(center, extent, draw float64) float64 {
	return center + float64(float64(draw-0.5)*extent)
}

// pointsHash01 is the canonical scalar hash: the GLSL-idiom sine hash, with
// the platform sine replaced by canonicalSin so the result is reproducible
// in any conforming environment.
func pointsHash01(seed int) float64 {
	x := float64(canonicalSin(float64(seed)*12.9898+78.233) * 43758.5453)
	return x - math.Floor(x)
}

// pointsHashDomainLimit is the largest sine argument the canonical reduction
// accepts, matching Go's reduceThreshold. Beyond it Go's math.Sin switches to
// Payne-Hanek reduction, which is not ported here; generators stay far below.
const pointsHashDomainLimit = 1 << 29

// canonicalSin ports the Cody-Waite branch of Go's math.sin (src/math/sin.go).
// Arguments at or above pointsHashDomainLimit return NaN rather than silently
// taking an unported path.
func canonicalSin(x float64) float64 {
	const (
		pi4a = 7.85398125648498535156e-1
		pi4b = 3.77489470793079817668e-8
		pi4c = 2.69515142907905952645e-15
		// 4/Pi rounded to float64 (0x3ff45f306dc9c883), written out so the
		// value does not depend on constant-folding precision.
		m4pi = 1.273239544735162542821171882678754627704620361328125
	)
	if x == 0 || math.IsNaN(x) {
		return x
	}
	if math.IsInf(x, 0) {
		return math.NaN()
	}
	sign := false
	if x < 0 {
		x = -x
		sign = true
	}
	if x >= pointsHashDomainLimit {
		return math.NaN()
	}
	j := uint64(x * m4pi)
	y := float64(j)
	if j&1 == 1 {
		j++
		y++
	}
	j &= 7
	z := ((x - float64(y*pi4a)) - float64(y*pi4b)) - float64(y*pi4c)
	if j > 3 {
		sign = !sign
		j -= 4
	}
	zz := z * z
	var r float64
	if j == 1 || j == 2 {
		const (
			c0 = -1.13585365213876817300e-11
			c1 = 2.08757008419747316778e-9
			c2 = -2.75573141792967388112e-7
			c3 = 2.48015872888517179954e-5
			c4 = -1.38888888888730564116e-3
			c5 = 4.16666666666665929218e-2
		)
		p := float64(float64(float64(float64(float64(c0*zz)+c1)*zz+c2)*zz+c3)*zz + c4)
		p = float64(p*zz) + c5
		r = 1.0 - float64(0.5*zz) + float64(float64(zz*zz)*p)
	} else {
		const (
			s0 = 1.58962301576546568060e-10
			s1 = -2.50507477628578072866e-8
			s2 = 2.75573136213857245213e-6
			s3 = -1.98412698295895385996e-4
			s4 = 8.33333333332211858878e-3
			s5 = -1.66666666666666307295e-1
		)
		p := float64(float64(float64(float64(float64(s0*zz)+s1)*zz+s2)*zz+s3)*zz + s4)
		p = float64(p*zz) + s5
		r = z + float64(float64(z*zz)*p)
	}
	if sign {
		r = -r
	}
	return r
}

// canonicalLog ports Go's math.log (src/math/log.go) for finite positive
// arguments.
func canonicalLog(x float64) float64 {
	const (
		ln2Hi = 6.93147180369123816490e-01
		ln2Lo = 1.90821492927058770002e-10
		l1    = 6.666666666666735130e-01
		l2    = 3.999999999940941908e-01
		l3    = 2.857142874366239149e-01
		l4    = 2.222219843214978396e-01
		l5    = 1.818357216161805012e-01
		l6    = 1.531383769920937332e-01
		l7    = 1.479819860511658591e-01
		// Sqrt2/2 rounded to float64.
		halfSqrt2 = 0.707106781186547524400844362104849039
	)
	if math.IsNaN(x) || math.IsInf(x, 1) {
		return x
	}
	if x < 0 {
		return math.NaN()
	}
	if x == 0 {
		return math.Inf(-1)
	}
	f1, ki := math.Frexp(x)
	if f1 < halfSqrt2 {
		f1 *= 2
		ki--
	}
	f := f1 - 1
	k := float64(ki)
	s := f / (2 + f)
	s2 := s * s
	s4 := s2 * s2
	t1 := float64(s2 * (l1 + float64(s4*(l3+float64(s4*(l5+float64(s4*l7)))))))
	t2 := float64(s4 * (l2 + float64(s4*(l4+float64(s4*l6)))))
	r := t1 + t2
	hfsq := float64(float64(0.5*f) * f)
	return float64(k*ln2Hi) - ((hfsq - (float64(s*(hfsq+r)) + float64(k*ln2Lo))) - f)
}

// canonicalExp ports Go's math.exp (src/math/exp.go) for finite arguments.
func canonicalExp(x float64) float64 {
	const (
		ln2Hi     = 6.93147180369123816490e-01
		ln2Lo     = 1.90821492927058770002e-10
		log2e     = 1.44269504088896338700e+00
		overflow  = 7.09782712893383973096e+02
		underflow = -7.45133219101941108420e+02
		nearZero  = 1.0 / (1 << 28)
	)
	switch {
	case math.IsNaN(x):
		return x
	case x > overflow:
		return math.Inf(1)
	case x < underflow:
		return 0
	case -nearZero < x && x < nearZero:
		return 1 + x
	}
	var k int
	if x < 0 {
		k = int(float64(log2e*x) - 0.5)
	} else {
		k = int(float64(log2e*x) + 0.5)
	}
	hi := x - float64(float64(k)*ln2Hi)
	lo := float64(k) * ln2Lo
	return canonicalExpMulti(hi, lo, k)
}

// canonicalExpMulti ports Go's math.expmulti.
func canonicalExpMulti(hi, lo float64, k int) float64 {
	const (
		p1 = 1.66666666666666657415e-01
		p2 = -2.77777777770155933842e-03
		p3 = 6.61375632143793436117e-05
		p4 = -1.65339022054652515390e-06
		p5 = 4.13813679705723846039e-08
	)
	r := hi - lo
	t := r * r
	c := r - float64(t*(p1+float64(t*(p2+float64(t*(p3+float64(t*(p4+float64(t*p5)))))))))
	y := 1 - ((lo - float64(r*c)/(2-c)) - hi)
	return math.Ldexp(y, k)
}

// canonicalPow is the shared spec for x**y, defined as exp(y*log(x)) so it
// needs only the two ported kernels. It is not Go's math.Pow — that routine
// mixes repeated squaring with Exp/Log and is far more surface to port — but
// it agrees with it to within a few ulp, which is orders of magnitude below
// any visible difference in a point size, and unlike math.Pow it is
// reproducible in JavaScript.
func canonicalPow(x, y float64) float64 {
	switch {
	case y == 0 || x == 1:
		return 1
	case y == 1:
		return x
	case math.IsNaN(x) || math.IsNaN(y):
		return math.NaN()
	case x == 0:
		if y < 0 {
			return math.Inf(1)
		}
		return 0
	case x < 0:
		return math.NaN()
	}
	return canonicalExp(float64(y * canonicalLog(x)))
}
