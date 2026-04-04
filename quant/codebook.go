package quant

import (
	"math"
	"sort"
	"sync"
)

type codebook struct {
	centroids  []float32
	boundaries []float32
}

type codebookKey struct {
	dim      int
	bitWidth int
}

var (
	codebookCache   = map[codebookKey]codebook{}
	codebookCacheMu sync.RWMutex
)

// cachedCodebook returns a cached codebook for (dim, bitWidth), computing it
// on first use. Safe for concurrent use.
func cachedCodebook(dim, bitWidth int) codebook {
	key := codebookKey{dim, bitWidth}
	codebookCacheMu.RLock()
	if cb, ok := codebookCache[key]; ok {
		codebookCacheMu.RUnlock()
		return cb
	}
	codebookCacheMu.RUnlock()

	codebookCacheMu.Lock()
	defer codebookCacheMu.Unlock()
	// Double-check after acquiring write lock.
	if cb, ok := codebookCache[key]; ok {
		return cb
	}
	cb := computeCodebook(dim, bitWidth)
	codebookCache[key] = cb
	return cb
}

// betaPDF computes the PDF of the projected coordinate distribution on
// the unit hypersphere S^{d-1}. Each coordinate follows:
//
//	f(x) = Γ(d/2) / (√π · Γ((d-1)/2)) · (1 - x²)^((d-3)/2)
//
// for x ∈ [-1, 1]. Uses log-space arithmetic to avoid overflow/underflow.
func betaPDF(x float64, dim int) float64 {
	if x <= -1 || x >= 1 {
		return 0
	}
	d := float64(dim)
	// Log of normalization constant: log(Γ(d/2)) - log(√π) - log(Γ((d-1)/2))
	lgD2, _ := math.Lgamma(d / 2)
	lgDm12, _ := math.Lgamma((d - 1) / 2)
	logNorm := lgD2 - 0.5*math.Log(math.Pi) - lgDm12

	exponent := (d - 3) / 2
	onemx2 := 1 - x*x
	logBody := exponent * math.Log(onemx2)

	return math.Exp(logNorm + logBody)
}

// simpsonIntegrate evaluates ∫_a^b f(x)dx using composite Simpson's rule
// with n subintervals (n must be even).
func simpsonIntegrate(f func(float64) float64, a, b float64, n int) float64 {
	if n%2 != 0 {
		n++
	}
	h := (b - a) / float64(n)
	sum := f(a) + f(b)
	for i := 1; i < n; i++ {
		x := a + float64(i)*h
		if i%2 == 0 {
			sum += 2 * f(x)
		} else {
			sum += 4 * f(x)
		}
	}
	return sum * h / 3
}

// computeCodebook runs the Lloyd-Max algorithm for optimal scalar
// quantization of the Beta distribution arising from dimension dim,
// using bitWidth bits (2^bitWidth centroids).
func computeCodebook(dim, bitWidth int) codebook {
	k := 1 << uint(bitWidth) // number of levels
	nInteg := 10000          // Simpson integration points

	// Initialize centroids uniformly in [-1, 1].
	centroids := make([]float64, k)
	for i := 0; i < k; i++ {
		centroids[i] = -1 + (2*float64(i)+1)/float64(k)
	}
	sort.Float64s(centroids)

	boundaries := make([]float64, k-1)

	pdf := func(x float64) float64 {
		return betaPDF(x, dim)
	}

	for iter := 0; iter < 200; iter++ {
		// Compute boundaries as midpoints.
		for i := 0; i < k-1; i++ {
			boundaries[i] = (centroids[i] + centroids[i+1]) / 2
		}

		// Recompute centroids as conditional expectations.
		maxDelta := 0.0
		for i := 0; i < k; i++ {
			lo := -1.0
			if i > 0 {
				lo = boundaries[i-1]
			}
			hi := 1.0
			if i < k-1 {
				hi = boundaries[i]
			}

			// E[X | lo <= X <= hi] = ∫ x·f(x)dx / ∫ f(x)dx
			num := simpsonIntegrate(func(x float64) float64 {
				return x * pdf(x)
			}, lo, hi, nInteg)

			den := simpsonIntegrate(pdf, lo, hi, nInteg)

			var newC float64
			if den < 1e-30 {
				// Negligible probability mass; keep midpoint.
				newC = (lo + hi) / 2
			} else {
				newC = num / den
			}

			delta := math.Abs(newC - centroids[i])
			if delta > maxDelta {
				maxDelta = delta
			}
			centroids[i] = newC
		}

		if maxDelta < 1e-10 {
			break
		}
	}

	// Convert to float32.
	c32 := make([]float32, k)
	b32 := make([]float32, k-1)
	for i, v := range centroids {
		c32[i] = float32(v)
	}
	for i, v := range boundaries {
		b32[i] = float32(v)
	}
	return codebook{centroids: c32, boundaries: b32}
}

// nearestCentroid returns the index of the nearest centroid using binary
// search on the sorted boundaries. O(bitWidth) per lookup.
func (c *codebook) nearestCentroid(value float32) int {
	return sort.Search(len(c.boundaries), func(i int) bool {
		return c.boundaries[i] > value
	})
}

// centroidValue returns the centroid at index i.
func (c *codebook) centroidValue(index int) float32 {
	return c.centroids[index]
}

// expectedMSE computes the theoretical per-coordinate MSE:
//
//	Σ_i ∫[b_{i-1} to b_i] (x - c_i)² · f(x) dx
//
// then multiplied by dim for the per-vector MSE.
func (c *codebook) expectedMSE(dim int) float64 {
	k := len(c.centroids)
	nInteg := 10000

	pdf := func(x float64) float64 {
		return betaPDF(x, dim)
	}

	var totalMSE float64
	for i := 0; i < k; i++ {
		lo := -1.0
		if i > 0 {
			lo = float64(c.boundaries[i-1])
		}
		hi := 1.0
		if i < k-1 {
			hi = float64(c.boundaries[i])
		}

		ci := float64(c.centroids[i])
		mse := simpsonIntegrate(func(x float64) float64 {
			d := x - ci
			return d * d * pdf(x)
		}, lo, hi, nInteg)

		totalMSE += mse
	}

	return totalMSE * float64(dim)
}
