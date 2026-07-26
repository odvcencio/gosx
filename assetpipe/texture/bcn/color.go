package bcn

import "math"

// vec3 holds one colour in stored code units from 0 to 255.
type vec3 struct{ x, y, z float64 }

func (v vec3) add(o vec3) vec3      { return vec3{v.x + o.x, v.y + o.y, v.z + o.z} }
func (v vec3) sub(o vec3) vec3      { return vec3{v.x - o.x, v.y - o.y, v.z - o.z} }
func (v vec3) scale(f float64) vec3 { return vec3{v.x * f, v.y * f, v.z * f} }
func (v vec3) dot(o vec3) float64   { return v.x*o.x + v.y*o.y + v.z*o.z }

// squaredDistance returns the squared error between two colours. The encoder
// weights the three channels the same, so it minimizes exactly the measurement
// the tests report. A luma-weighted metric would raise the perceived quality and
// lower every reported number, which would hide the gain instead of proving it.
func (v vec3) squaredDistance(o vec3) float64 {
	dx, dy, dz := v.x-o.x, v.y-o.y, v.z-o.z
	return dx*dx + dy*dy + dz*dz
}

// nearest5 maps an 8-bit target to the 5-bit endpoint code whose expansion sits
// closest to it. A plain scale-and-round is off by one code for some targets,
// because the bit replication of the expansion is not a uniform scale.
var nearest5 = func() [256]uint8 {
	var out [256]uint8
	for target := range out {
		best, bestErr := 0, 1<<30
		for code, expanded := range expand5 {
			d := int(expanded) - target
			if d < 0 {
				d = -d
			}
			if d < bestErr {
				bestErr, best = d, code
			}
		}
		out[target] = uint8(best)
	}
	return out
}()

// nearest6 maps an 8-bit target to the closest 6-bit endpoint code.
var nearest6 = func() [256]uint8 {
	var out [256]uint8
	for target := range out {
		best, bestErr := 0, 1<<30
		for code, expanded := range expand6 {
			d := int(expanded) - target
			if d < 0 {
				d = -d
			}
			if d < bestErr {
				bestErr, best = d, code
			}
		}
		out[target] = uint8(best)
	}
	return out
}()

// quantize565 rounds one colour to the nearest RGB565 endpoint, channel by
// channel.
func quantize565(c vec3) uint16 {
	r := nearest5[clampCode(c.x)]
	g := nearest6[clampCode(c.y)]
	b := nearest5[clampCode(c.z)]
	return uint16(r)<<11 | uint16(g)<<5 | uint16(b)
}

// expand565f expands one RGB565 endpoint to code units.
func expand565f(v uint16) vec3 {
	c := unpack565(v)
	return vec3{float64(c.R), float64(c.G), float64(c.B)}
}

// classTransparent marks a texel the block must decode as transparent black.
const classTransparent = 0xFF

// colourWeights holds the position of each palette class between the endpoints.
var (
	weightsFour  = [4]float64{0, 1.0 / 3, 2.0 / 3, 1}
	weightsThree = [3]float64{0, 0.5, 1}
)

// Index maps from a palette class to the stored 2-bit index.
//
// A class counts from the endpoint at weight zero. The stored index depends on
// which endpoint the block writes first, and the endpoint order also selects the
// mode. So a swap changes both.
var (
	// Four-colour mode with color0 at weight zero.
	mapFour = [4]uint8{0, 2, 3, 1}
	// Four-colour mode with the endpoints swapped so color0 > color1 holds.
	mapFourSwap = [4]uint8{1, 3, 2, 0}
	// Three-colour mode with color0 at weight zero.
	mapThree = [3]uint8{0, 2, 1}
	// Three-colour mode with the endpoints swapped so color0 <= color1 holds.
	mapThreeSwap = [3]uint8{1, 2, 0}
)

// colorFit holds one candidate colour block.
type colorFit struct {
	sse   float64
	a, b  uint16 // quantized endpoints, a at weight zero
	three bool   // three-entry palette
	class [16]uint8
}

// wholeCode rounds one interpolated channel to a whole code.
//
// The function skips the range check of roundCode on purpose. Every value it sees
// is a weighted mean of two endpoint channels that already sit between 0 and 255,
// so no value can leave the range, and this runs in the hottest loop of the
// package.
func wholeCode(v float64) float64 { return float64(int(v + 0.5)) }

// palette builds the decode entries of one endpoint pair in weight order.
//
// The entries round to whole codes, which is what the decoder in decode.go
// produces. Scoring an unrounded palette would rank candidates by a number no
// decoder computes.
func palette(a, b uint16, three bool) ([4]vec3, int) {
	va, vb := expand565f(a), expand565f(b)
	var pal [4]vec3
	pal[0] = va
	if three {
		pal[1] = vec3{
			wholeCode((va.x + vb.x) * 0.5),
			wholeCode((va.y + vb.y) * 0.5),
			wholeCode((va.z + vb.z) * 0.5),
		}
		pal[2] = vb
		return pal, 3
	}
	const third = 1.0 / 3
	pal[1] = vec3{
		wholeCode((2*va.x + vb.x) * third),
		wholeCode((2*va.y + vb.y) * third),
		wholeCode((2*va.z + vb.z) * third),
	}
	pal[2] = vec3{
		wholeCode((va.x + 2*vb.x) * third),
		wholeCode((va.y + 2*vb.y) * third),
		wholeCode((va.z + 2*vb.z) * third),
	}
	pal[3] = vb
	return pal, 4
}

// evalPair scores one quantized endpoint pair and assigns every texel.
//
// mask marks the texels that must decode as transparent black. Those texels take
// no part in the score, because the block cannot store their colour.
//
// The function picks the class of a texel by projecting it onto the endpoint line
// instead of measuring the distance to all three or four entries. The entries are
// evenly spaced along that line, so the projection names the nearest one, and the
// projection costs one dot product instead of four distance measurements. This is
// the hottest loop in the package, and the search calls it hundreds of times for
// each block.
//
// The stored entries round to whole codes, so they sit up to half a code off the
// even spacing. A texel almost exactly between two entries can therefore land on
// the farther one. The score stays honest either way, because it measures the
// distance to the entry the block really stores.
func evalPair(texels *[16]vec3, mask uint16, a, b uint16, three bool) colorFit {
	pal, count := palette(a, b, three)
	fit := colorFit{a: a, b: b, three: three}
	origin := pal[0]
	ax := pal[count-1].x - origin.x
	ay := pal[count-1].y - origin.y
	az := pal[count-1].z - origin.z
	last := float64(count - 1)
	length := ax*ax + ay*ay + az*az
	scale := 0.0
	if length > 1e-12 {
		scale = last / length
	}
	for i := range texels {
		if mask>>uint(i)&1 != 0 {
			fit.class[i] = classTransparent
			continue
		}
		dx := texels[i].x - origin.x
		dy := texels[i].y - origin.y
		dz := texels[i].z - origin.z
		position := (dx*ax + dy*ay + dz*az) * scale
		class := 0
		if position >= last {
			class = count - 1
		} else if position > 0 {
			class = int(position + 0.5)
		}
		entry := pal[class]
		ex := texels[i].x - entry.x
		ey := texels[i].y - entry.y
		ez := texels[i].z - entry.z
		fit.sse += ex*ex + ey*ey + ez*ez
		fit.class[i] = uint8(class)
	}
	return fit
}

// solveColor finds the endpoint pair with the smallest squared error while every
// texel keeps its class.
//
// The reasoning matches solveBC4. With the class of each texel fixed, its
// position between the endpoints is a known weight, so the squared error is a
// quadratic in the two endpoint vectors and its minimum solves one two by two
// system for each channel.
func solveColor(texels *[16]vec3, fit *colorFit) (vec3, vec3, bool) {
	weights := weightsFour[:]
	if fit.three {
		weights = weightsThree[:]
	}
	var a2, ab, b2 float64
	var ax, bx vec3
	used := 0
	for i, class := range fit.class {
		if class == classTransparent {
			continue
		}
		used++
		w := weights[class]
		a := 1 - w
		a2 += a * a
		ab += a * w
		b2 += w * w
		ax = ax.add(texels[i].scale(a))
		bx = bx.add(texels[i].scale(w))
	}
	if used < 2 {
		return vec3{}, vec3{}, false
	}
	det := a2*b2 - ab*ab
	if math.Abs(det) < 1e-9 {
		return vec3{}, vec3{}, false
	}
	lo := ax.scale(b2 / det).sub(bx.scale(ab / det))
	hi := bx.scale(a2 / det).sub(ax.scale(ab / det))
	return lo, hi, true
}

// leastSquaresColor runs the refinement rounds on one candidate.
//
// Each round solves for the endpoints, quantizes them, and assigns the texels
// again. The round stops as soon as the error stops dropping, so the function
// never returns a worse fit than the one it started from.
func leastSquaresColor(texels *[16]vec3, mask uint16, fit colorFit, rounds int) colorFit {
	for round := 0; round < rounds; round++ {
		lo, hi, ok := solveColor(texels, &fit)
		if !ok {
			return fit
		}
		a, b := quantize565(lo), quantize565(hi)
		if a == fit.a && b == fit.b {
			return fit
		}
		next := evalPair(texels, mask, a, b, fit.three)
		if next.sse >= fit.sse {
			return fit
		}
		fit = next
	}
	return fit
}

// endpointChannels lists the three subfields of an RGB565 endpoint as a shift
// and a mask.
var endpointChannels = [3]struct {
	shift uint
	width uint16
}{
	{11, 0x1F}, // red
	{5, 0x3F},  // green
	{0, 0x1F},  // blue
}

// nudge565 moves one channel of an endpoint by delta. It reports false at the
// end of the range.
func nudge565(endpoint uint16, shift uint, width uint16, delta int) (uint16, bool) {
	value := int((endpoint>>shift)&width) + delta
	if value < 0 || value > int(width) {
		return endpoint, false
	}
	return (endpoint & ^(width << shift)) | uint16(value)<<shift, true
}

// polishColor walks one-step changes of the six endpoint channels until nothing
// improves.
//
// The step exists because the least-squares solve works on real endpoints and
// then quantizes to RGB565. The nearest quantized pair is not always the best
// quantized pair: moving one endpoint by one code shifts both interpolated
// palette entries, and that can pay for the endpoint error it costs. The effect
// grows as the block gets smoother, because then the quantization is the whole
// error. TestBC1ApproachesLocalOptimum measured 15 percent of squared error on a
// smooth banded image before this step existed.
//
// Each sweep costs twelve candidate evaluations. The loop stops as soon as a
// sweep finds nothing, so a block that is already at a local optimum pays one
// sweep.
func polishColor(texels *[16]vec3, mask uint16, fit colorFit, sweeps int) colorFit {
	for sweep := 0; sweep < sweeps; sweep++ {
		improved := false
		for _, channel := range endpointChannels {
			for _, delta := range [2]int{-1, 1} {
				for endpoint := 0; endpoint < 2; endpoint++ {
					a, b := fit.a, fit.b
					target := &a
					if endpoint == 1 {
						target = &b
					}
					moved, ok := nudge565(*target, channel.shift, channel.width, delta)
					if !ok {
						continue
					}
					*target = moved
					candidate := evalPair(texels, mask, a, b, fit.three)
					if candidate.sse < fit.sse {
						fit = candidate
						improved = true
					}
				}
			}
		}
		if !improved {
			break
		}
	}
	return fit
}

// smaller and larger replace math.Min and math.Max in the hot paths.
//
// The standard library pair handles NaN and negative zero, which costs a real
// function call. The values here are colour codes from 0 to 255, so a plain
// comparison is both correct and several times faster. A profile put math.archMin
// and math.archMax at a sixth of the whole encode before this change.
func smaller(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}

func larger(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}

// blockStats returns the mean, the bounding box and the count of the texels the
// fit may use.
func blockStats(texels *[16]vec3, mask uint16) (mean, lo, hi vec3, count int) {
	lo = vec3{math.Inf(1), math.Inf(1), math.Inf(1)}
	hi = vec3{math.Inf(-1), math.Inf(-1), math.Inf(-1)}
	for i := range texels {
		if mask>>uint(i)&1 != 0 {
			continue
		}
		t := texels[i]
		count++
		mean = mean.add(t)
		lo = vec3{smaller(lo.x, t.x), smaller(lo.y, t.y), smaller(lo.z, t.z)}
		hi = vec3{larger(hi.x, t.x), larger(hi.y, t.y), larger(hi.z, t.z)}
	}
	if count == 0 {
		return vec3{}, vec3{}, vec3{}, 0
	}
	mean = mean.scale(1 / float64(count))
	return mean, lo, hi, count
}

// principalAxis returns the dominant eigenvector of the colour covariance.
//
// A per-channel bounding box assumes the colours of a block spread along the
// diagonal of the cube. They rarely do. The dominant eigenvector points along
// the real spread, so projecting onto it and taking the extremes gives two
// endpoints whose line passes through the colours instead of beside them.
//
// A few power iterations reach the dominant eigenvector because the covariance
// matrix is symmetric and positive semi-definite, so its eigenvalues are real
// and non-negative. Eight rounds are enough for a 4x4 block: the error falls by
// the eigenvalue ratio each round, and a block whose ratio is near one has no
// dominant axis worth finding.
func principalAxis(texels *[16]vec3, mask uint16, mean vec3) (vec3, bool) {
	var xx, xy, xz, yy, yz, zz float64
	for i := range texels {
		if mask>>uint(i)&1 != 0 {
			continue
		}
		d := texels[i].sub(mean)
		xx += d.x * d.x
		xy += d.x * d.y
		xz += d.x * d.z
		yy += d.y * d.y
		yz += d.y * d.z
		zz += d.z * d.z
	}
	if xx+yy+zz < 1e-9 {
		return vec3{}, false
	}
	// Start from the row with the largest norm. That row already points near
	// the dominant axis, so the iteration converges from a sane guess even
	// when two eigenvalues are close.
	rows := [3]vec3{{xx, xy, xz}, {xy, yy, yz}, {xz, yz, zz}}
	axis := rows[0]
	if rows[1].dot(rows[1]) > axis.dot(axis) {
		axis = rows[1]
	}
	if rows[2].dot(rows[2]) > axis.dot(axis) {
		axis = rows[2]
	}
	for round := 0; round < 8; round++ {
		next := vec3{axis.dot(rows[0]), axis.dot(rows[1]), axis.dot(rows[2])}
		length := math.Sqrt(next.dot(next))
		if length < 1e-12 {
			break
		}
		axis = next.scale(1 / length)
	}
	length := math.Sqrt(axis.dot(axis))
	if length < 1e-12 {
		return vec3{}, false
	}
	return axis.scale(1 / length), true
}
