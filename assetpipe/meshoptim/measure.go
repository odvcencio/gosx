package meshoptim

import "math"

// OverdrawStats reports what a depth-only rasterizer measured.
type OverdrawStats struct {
	// Shaded counts the fragments that passed the depth test, so a real
	// renderer would shade them.
	Shaded int64
	// Covered counts the pixels the mesh reached at least once.
	Covered int64
	// Ratio is Shaded divided by Covered. A ratio of 1.0 means every pixel
	// shaded exactly once.
	Ratio float64
}

// measureViews holds the camera directions MeasureOverdraw renders from. The
// eight cube corners reach every octant, and a fixed set keeps the metric
// repeatable.
var measureViews = [][3]float64{
	{1, 1, 1}, {-1, 1, 1}, {1, -1, 1}, {1, 1, -1},
	{-1, -1, 1}, {-1, 1, -1}, {1, -1, -1}, {-1, -1, -1},
}

// MeasureOverdraw rasterizes the mesh with a depth buffer and counts the
// fragments a renderer would shade. It measures the draw order directly rather
// than trusting the sort key that produced it.
//
// The rasterizer projects orthographically, culls back faces, and uses a
// less-than depth test with depth writes, which is the ordinary opaque path.
func MeasureOverdraw(indices []uint32, positions []float32, vertexCount, resolution int) OverdrawStats {
	stats := OverdrawStats{}
	if resolution <= 0 || vertexCount <= 0 || len(indices) < 3 || len(positions) < vertexCount*3 {
		return stats
	}
	depth := make([]float64, resolution*resolution)
	touched := make([]bool, resolution*resolution)

	for _, view := range measureViews {
		right, up, forward := viewBasis(view)
		low, high := projectedBounds(positions, vertexCount, right, up, forward)
		span := math.Max(high[0]-low[0], high[1]-low[1])
		if span <= 0 {
			continue
		}
		scale := float64(resolution-1) / span
		for i := range depth {
			depth[i] = math.Inf(1)
			touched[i] = false
		}
		for i := 0; i+2 < len(indices); i += 3 {
			a := project(positions, indices[i], right, up, forward, low, scale)
			b := project(positions, indices[i+1], right, up, forward, low, scale)
			c := project(positions, indices[i+2], right, up, forward, low, scale)
			// A counter-clockwise winding in screen space faces the camera.
			area := (b[0]-a[0])*(c[1]-a[1]) - (c[0]-a[0])*(b[1]-a[1])
			if area <= 0 {
				continue
			}
			rasterize(a, b, c, area, resolution, depth, touched, &stats)
		}
		for i := range touched {
			if touched[i] {
				stats.Covered++
			}
		}
	}
	if stats.Covered > 0 {
		stats.Ratio = float64(stats.Shaded) / float64(stats.Covered)
	}
	return stats
}

func rasterize(a, b, c [3]float64, area float64, resolution int, depth []float64, touched []bool, stats *OverdrawStats) {
	minX := int(math.Floor(math.Min(a[0], math.Min(b[0], c[0]))))
	maxX := int(math.Ceil(math.Max(a[0], math.Max(b[0], c[0]))))
	minY := int(math.Floor(math.Min(a[1], math.Min(b[1], c[1]))))
	maxY := int(math.Ceil(math.Max(a[1], math.Max(b[1], c[1]))))
	if minX < 0 {
		minX = 0
	}
	if minY < 0 {
		minY = 0
	}
	if maxX > resolution-1 {
		maxX = resolution - 1
	}
	if maxY > resolution-1 {
		maxY = resolution - 1
	}
	inverseArea := 1 / area
	for y := minY; y <= maxY; y++ {
		py := float64(y) + 0.5
		for x := minX; x <= maxX; x++ {
			px := float64(x) + 0.5
			w0 := (b[0]-a[0])*(py-a[1]) - (px-a[0])*(b[1]-a[1])
			w1 := (c[0]-b[0])*(py-b[1]) - (px-b[0])*(c[1]-b[1])
			w2 := (a[0]-c[0])*(py-c[1]) - (px-c[0])*(a[1]-c[1])
			if w0 < 0 || w1 < 0 || w2 < 0 {
				continue
			}
			alpha := w1 * inverseArea
			beta := w2 * inverseArea
			gamma := w0 * inverseArea
			z := alpha*a[2] + beta*b[2] + gamma*c[2]
			pixel := y*resolution + x
			touched[pixel] = true
			if z < depth[pixel] {
				depth[pixel] = z
				stats.Shaded++
			}
		}
	}
}

func project(positions []float32, index uint32, right, up, forward, low [3]float64, scale float64) [3]float64 {
	point := vertexAt(positions, index)
	x := dot(point, right)
	y := dot(point, up)
	z := dot(point, forward)
	return [3]float64{(x - low[0]) * scale, (y - low[1]) * scale, z}
}

func projectedBounds(positions []float32, vertexCount int, right, up, forward [3]float64) ([3]float64, [3]float64) {
	low := [3]float64{math.Inf(1), math.Inf(1), math.Inf(1)}
	high := [3]float64{math.Inf(-1), math.Inf(-1), math.Inf(-1)}
	for vertex := 0; vertex < vertexCount; vertex++ {
		point := vertexAt(positions, uint32(vertex))
		axes := [3]float64{dot(point, right), dot(point, up), dot(point, forward)}
		for axis := 0; axis < 3; axis++ {
			low[axis] = math.Min(low[axis], axes[axis])
			high[axis] = math.Max(high[axis], axes[axis])
		}
	}
	return low, high
}

// viewBasis builds a right-handed basis whose forward axis points from the
// camera into the scene.
func viewBasis(view [3]float64) ([3]float64, [3]float64, [3]float64) {
	length := math.Sqrt(view[0]*view[0] + view[1]*view[1] + view[2]*view[2])
	if length == 0 {
		return [3]float64{1, 0, 0}, [3]float64{0, 1, 0}, [3]float64{0, 0, 1}
	}
	forward := [3]float64{-view[0] / length, -view[1] / length, -view[2] / length}
	helper := [3]float64{0, 1, 0}
	if math.Abs(forward[1]) > 0.9 {
		helper = [3]float64{1, 0, 0}
	}
	right := normalize(crossProduct(helper, forward))
	up := normalize(crossProduct(forward, right))
	return right, up, forward
}

func normalize(v [3]float64) [3]float64 {
	length := math.Sqrt(v[0]*v[0] + v[1]*v[1] + v[2]*v[2])
	if length == 0 {
		return v
	}
	return [3]float64{v[0] / length, v[1] / length, v[2] / length}
}

func dot(a, b [3]float64) float64 {
	return a[0]*b[0] + a[1]*b[1] + a[2]*b[2]
}
