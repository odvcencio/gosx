package texture

import (
	"fmt"
	"math"
	"runtime"
	"sync"
	"sync/atomic"
)

// Filter names a reconstruction kernel.
type Filter int

const (
	// Lanczos3 is the sharpest of the four. Its two negative lobes can push
	// a resampled value outside 0 to 1, so the quantizer clamps.
	Lanczos3 Filter = iota
	// Mitchell is the Mitchell-Netravali kernel with B = C = 1/3. It trades
	// a little sharpness for much less ringing, and is the usual default
	// for photographic minification.
	Mitchell
	// Triangle is bilinear. It never rings and never overshoots.
	Triangle
	// Box averages the covered source area. Only Box reproduces an exact
	// arithmetic mean at a 2:1 ratio, which makes it the reference the
	// resize tests measure the other kernels against.
	Box
)

// String names the filter for a manifest or a metric.
func (f Filter) String() string {
	switch f {
	case Mitchell:
		return "mitchell"
	case Triangle:
		return "triangle"
	case Box:
		return "box"
	default:
		return "lanczos3"
	}
}

// ParseFilter maps a name to a filter. An unknown name selects Lanczos3.
func ParseFilter(name string) Filter {
	switch name {
	case "mitchell":
		return Mitchell
	case "triangle":
		return Triangle
	case "box":
		return Box
	default:
		return Lanczos3
	}
}

// support returns the kernel radius in source samples at a 1:1 ratio.
func (f Filter) support() float64 {
	switch f {
	case Mitchell:
		return 2
	case Triangle:
		return 1
	case Box:
		return 0.5
	default:
		return 3
	}
}

// weight evaluates the kernel at distance x.
func (f Filter) weight(x float64) float64 {
	if x < 0 {
		x = -x
	}
	switch f {
	case Mitchell:
		return mitchell(x)
	case Triangle:
		if x < 1 {
			return 1 - x
		}
		return 0
	case Box:
		if x <= 0.5 {
			return 1
		}
		return 0
	default:
		return lanczos3(x)
	}
}

func lanczos3(x float64) float64 {
	if x < 1e-9 {
		return 1
	}
	if x >= 3 {
		return 0
	}
	pix := math.Pi * x
	return 3 * math.Sin(pix) * math.Sin(pix/3) / (pix * pix)
}

// mitchell evaluates Mitchell-Netravali with B = C = 1/3.
func mitchell(x float64) float64 {
	const b, c = 1.0 / 3.0, 1.0 / 3.0
	x2 := x * x
	switch {
	case x < 1:
		return ((12-9*b-6*c)*x*x2 + (-18+12*b+6*c)*x2 + (6 - 2*b)) / 6
	case x < 2:
		return ((-b-6*c)*x*x2 + (6*b+30*c)*x2 + (-12*b-48*c)*x + (8*b + 24*c)) / 6
	default:
		return 0
	}
}

// contribution is one output sample's weighted source window.
type contribution struct {
	start   int
	weights []float64
}

// buildContributions plans one axis of a separable resample.
//
// The plan widens the kernel when the axis shrinks, so a minification samples
// the whole source area instead of point-sampling every other texel. It clamps
// the window to the source edge and normalizes afterwards, so a constant image
// stays exactly constant at every ratio.
func buildContributions(srcSize, dstSize int, f Filter) []contribution {
	scale := float64(dstSize) / float64(srcSize)
	filterScale := 1.0
	support := f.support()
	if scale < 1 {
		// Minification: widen the kernel to cover the source footprint.
		filterScale = scale
		support /= scale
	}
	out := make([]contribution, dstSize)
	for i := 0; i < dstSize; i++ {
		center := (float64(i)+0.5)/scale - 0.5
		lo := int(math.Ceil(center - support))
		hi := int(math.Floor(center + support))
		if hi < lo {
			hi = lo
		}
		weights := make([]float64, 0, hi-lo+1)
		var total float64
		for s := lo; s <= hi; s++ {
			w := f.weight((float64(s) - center) * filterScale)
			weights = append(weights, w)
			total += w
		}
		if total == 0 {
			// A pathological ratio can zero every weight. Fall back to the
			// nearest source sample so the output stays defined.
			weights = []float64{1}
			lo = int(math.Round(center))
			total = 1
		}
		for j := range weights {
			weights[j] /= total
		}
		out[i] = contribution{start: lo, weights: weights}
	}
	return out
}

// Resize resamples img to dstWidth by dstHeight with a separable filter.
//
// The function works on linear light. It does not touch the alpha mode, so a
// straight-alpha image resamples its colour channels independently of alpha.
// Call Premultiply first when the alpha channel has real structure, otherwise
// transparent texels bleed their colour into the opaque neighbourhood.
func Resize(img *Image, dstWidth, dstHeight int, f Filter) (*Image, error) {
	if img == nil || img.Width < 1 || img.Height < 1 {
		return nil, fmt.Errorf("%w: empty source", ErrShape)
	}
	if dstWidth < 1 || dstHeight < 1 {
		return nil, fmt.Errorf("%w: target %dx%d", ErrShape, dstWidth, dstHeight)
	}
	if dstWidth == img.Width && dstHeight == img.Height {
		return img.Clone(), nil
	}

	// Horizontal pass into an intermediate of the source height.
	//
	// Each row writes only its own slice of the destination, so the rows are
	// independent and the loop parallelizes without a lock.
	horizontal := buildContributions(img.Width, dstWidth, f)
	mid := &Image{Width: dstWidth, Height: img.Height, Alpha: img.Alpha}
	mid.Pix = make([]float32, dstWidth*img.Height*4)
	parallelRows(img.Height, func(y int) {
		srcRow := img.Pix[y*img.Width*4:]
		dstRow := mid.Pix[y*dstWidth*4:]
		for x := 0; x < dstWidth; x++ {
			plan := horizontal[x]
			var r, g, b, a float64
			for j, w := range plan.weights {
				s := clampIndex(plan.start+j, img.Width)
				off := s * 4
				r += w * float64(srcRow[off])
				g += w * float64(srcRow[off+1])
				b += w * float64(srcRow[off+2])
				a += w * float64(srcRow[off+3])
			}
			off := x * 4
			dstRow[off] = float32(r)
			dstRow[off+1] = float32(g)
			dstRow[off+2] = float32(b)
			dstRow[off+3] = float32(a)
		}
	})

	// Vertical pass into the final image.
	vertical := buildContributions(img.Height, dstHeight, f)
	out := &Image{Width: dstWidth, Height: dstHeight, Alpha: img.Alpha}
	out.Pix = make([]float32, dstWidth*dstHeight*4)
	parallelRows(dstHeight, func(y int) {
		plan := vertical[y]
		dstRow := out.Pix[y*dstWidth*4:]
		for x := 0; x < dstWidth; x++ {
			var r, g, b, a float64
			for j, w := range plan.weights {
				s := clampIndex(plan.start+j, img.Height)
				off := (s*dstWidth + x) * 4
				r += w * float64(mid.Pix[off])
				g += w * float64(mid.Pix[off+1])
				b += w * float64(mid.Pix[off+2])
				a += w * float64(mid.Pix[off+3])
			}
			off := x * 4
			dstRow[off] = float32(r)
			dstRow[off+1] = float32(g)
			dstRow[off+2] = float32(b)
			dstRow[off+3] = float32(a)
		}
	})
	return out, nil
}

// parallelRows runs body once per row, spread over the available cores. The
// caller guarantees that each row writes only its own destination slice.
func parallelRows(rows int, body func(row int)) {
	workers := runtime.NumCPU()
	if workers > rows {
		workers = rows
	}
	if workers < 2 {
		for row := 0; row < rows; row++ {
			body(row)
		}
		return
	}
	var next atomic.Int64
	var wg sync.WaitGroup
	for worker := 0; worker < workers; worker++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				row := int(next.Add(1)) - 1
				if row >= rows {
					return
				}
				body(row)
			}
		}()
	}
	wg.Wait()
}

func clampIndex(i, size int) int {
	if i < 0 {
		return 0
	}
	if i >= size {
		return size - 1
	}
	return i
}

// NextPowerOfTwo returns the smallest power of two at or above v.
func NextPowerOfTwo(v int) int {
	if v < 1 {
		return 1
	}
	p := 1
	for p < v {
		p <<= 1
	}
	return p
}

// NearestPowerOfTwo returns the power of two closest to v in log space.
//
// Nearest beats next for a texture. Rounding 1200 up to 2048 nearly triples
// the pixel count for no added detail; rounding to 1024 loses a sixth of the
// edge and keeps the upload honest.
func NearestPowerOfTwo(v int) int {
	if v < 1 {
		return 1
	}
	high := NextPowerOfTwo(v)
	if high == 1 {
		return 1
	}
	low := high / 2
	// Compare in log space: pick low when v*v < low*high.
	if int64(v)*int64(v) < int64(low)*int64(high) {
		return low
	}
	return high
}

// FitPowerOfTwo rounds each edge to the nearest power of two and then caps
// both edges at maxEdge.
//
// Each edge rounds on its own. A non-square source therefore changes its
// aspect ratio slightly, which is what every WebGL1-era pipeline did and what
// a UV-mapped texture tolerates. A maxEdge of zero or less applies no cap.
func FitPowerOfTwo(width, height, maxEdge int) (int, int) {
	w := NearestPowerOfTwo(width)
	h := NearestPowerOfTwo(height)
	if maxEdge <= 0 {
		return w, h
	}
	limit := NextPowerOfTwo(maxEdge)
	if limit > maxEdge {
		limit /= 2
	}
	if limit < 1 {
		limit = 1
	}
	for w > limit {
		w /= 2
	}
	for h > limit {
		h /= 2
	}
	return w, h
}
