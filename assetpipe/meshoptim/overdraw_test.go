package meshoptim

import (
	"math"
	"math/rand"
	"testing"
)

// nestedSpheres builds two concentric spheres. The outer shell hides the inner
// one from every direction, so a back-to-front order shades every pixel twice
// and a front-to-back order shades most pixels once.
func nestedSpheres(rings, segments int, radii []float64) ([]uint32, []float32, int) {
	var positions []float32
	var indices []uint32
	for _, radius := range radii {
		base := uint32(len(positions) / 3)
		for ring := 0; ring <= rings; ring++ {
			phi := math.Pi * float64(ring) / float64(rings)
			for segment := 0; segment < segments; segment++ {
				theta := 2 * math.Pi * float64(segment) / float64(segments)
				positions = append(positions,
					float32(radius*math.Sin(phi)*math.Cos(theta)),
					float32(radius*math.Cos(phi)),
					float32(radius*math.Sin(phi)*math.Sin(theta)))
			}
		}
		for ring := 0; ring < rings; ring++ {
			for segment := 0; segment < segments; segment++ {
				a := base + uint32(ring*segments+segment)
				b := base + uint32(ring*segments+(segment+1)%segments)
				c := a + uint32(segments)
				d := b + uint32(segments)
				indices = append(indices, a, c, b, b, c, d)
			}
		}
	}
	return indices, positions, len(positions) / 3
}

func TestMeasureOverdrawCountsDepthPasses(t *testing.T) {
	// One flat quad drawn once shades every covered pixel exactly once.
	positions := []float32{0, 0, 0, 1, 0, 0, 0, 1, 0, 1, 1, 0}
	indices := []uint32{0, 1, 2, 2, 1, 3}
	stats := MeasureOverdraw(indices, positions, 4, 64)
	if stats.Covered == 0 {
		t.Fatal("a quad must cover pixels")
	}
	if stats.Ratio < 0.99 || stats.Ratio > 1.01 {
		t.Fatalf("a single layer must shade once per pixel, got %.4f", stats.Ratio)
	}
}

func TestMeasureOverdrawSeesLayering(t *testing.T) {
	// Two nested spheres drawn inner first must shade more than the same
	// geometry drawn outer first.
	indices, positions, vertexCount := nestedSpheres(16, 24, []float64{0.5, 1.0})
	inner := len(indices) / 2

	innerFirst := append(append([]uint32(nil), indices[:inner]...), indices[inner:]...)
	outerFirst := append(append([]uint32(nil), indices[inner:]...), indices[:inner]...)

	innerStats := MeasureOverdraw(innerFirst, positions, vertexCount, 128)
	outerStats := MeasureOverdraw(outerFirst, positions, vertexCount, 128)
	t.Logf("inner first ratio %.4f, outer first ratio %.4f", innerStats.Ratio, outerStats.Ratio)
	if outerStats.Ratio >= innerStats.Ratio {
		t.Fatalf("drawing the hiding shell first must shade less: %.4f vs %.4f", outerStats.Ratio, innerStats.Ratio)
	}
}

func TestOptimizeOverdrawKeepsEveryTriangle(t *testing.T) {
	indices, positions, vertexCount := nestedSpheres(12, 20, []float64{0.5, 1.0})
	shuffled := shuffleTriangles(indices, 4)
	ordered := OptimizeVertexCacheTipsify(shuffled, vertexCount)
	sorted := OptimizeOverdraw(ordered, positions, vertexCount, DefaultOverdrawThresholdForTest)

	if len(sorted) != len(ordered) {
		t.Fatalf("index count changed: %d, want %d", len(sorted), len(ordered))
	}
	before := triangleSet(ordered)
	after := triangleSet(sorted)
	if len(before) != len(after) {
		t.Fatalf("distinct triangles changed: %d, want %d", len(after), len(before))
	}
	for key, count := range before {
		if after[key] != count {
			t.Fatalf("triangle %v appears %d times, want %d", key, after[key], count)
		}
	}
}

func TestOptimizeOverdrawBoundsTheCacheCost(t *testing.T) {
	indices, positions, vertexCount := nestedSpheres(16, 24, []float64{0.5, 1.0})
	shuffled := shuffleTriangles(indices, 8)
	ordered := OptimizeVertexCacheTipsify(shuffled, vertexCount)
	sorted := OptimizeOverdraw(ordered, positions, vertexCount, DefaultOverdrawThresholdForTest)

	cacheBefore := ACMR(ordered, vertexCount, DefaultCacheSize)
	cacheAfter := ACMR(sorted, vertexCount, DefaultCacheSize)
	drawBefore := MeasureOverdraw(ordered, positions, vertexCount, 128)
	drawAfter := MeasureOverdraw(sorted, positions, vertexCount, 128)
	t.Logf("ACMR %.4f -> %.4f, overdraw %.4f -> %.4f",
		cacheBefore, cacheAfter, drawBefore.Ratio, drawAfter.Ratio)

	// The sort trades cache locality for fill rate. The trade must stay small.
	if cacheAfter > cacheBefore*1.35 {
		t.Fatalf("the overdraw sort cost too much cache locality: %.4f -> %.4f", cacheBefore, cacheAfter)
	}
}

// DefaultOverdrawThresholdForTest mirrors the stage default without importing
// the pipeline package.
const DefaultOverdrawThresholdForTest = 1.05

func TestOptimizeOverdrawHandlesATinyMesh(t *testing.T) {
	indices := []uint32{0, 1, 2}
	positions := []float32{0, 0, 0, 1, 0, 0, 0, 1, 0}
	sorted := OptimizeOverdraw(indices, positions, 3, 1.05)
	if len(sorted) != 3 {
		t.Fatalf("a single triangle must survive: %v", sorted)
	}
}

func TestFetchOverfetchFallsWithLocality(t *testing.T) {
	// A random access order must overfetch more than a linear one.
	vertexCount := 4096
	linear := make([]uint32, 0, vertexCount*3)
	for i := 0; i < vertexCount; i++ {
		linear = append(linear, uint32(i))
	}
	random := rand.New(rand.NewSource(2))
	scattered := append([]uint32(nil), linear...)
	random.Shuffle(len(scattered), func(i, j int) { scattered[i], scattered[j] = scattered[j], scattered[i] })

	linearFetch := FetchOverfetch(linear, vertexCount, 12, 64)
	scatteredFetch := FetchOverfetch(scattered, vertexCount, 12, 64)
	t.Logf("linear %.4f, scattered %.4f", linearFetch, scatteredFetch)
	if scatteredFetch <= linearFetch {
		t.Fatalf("a scattered order must overfetch more: %.4f vs %.4f", scatteredFetch, linearFetch)
	}
	if linearFetch > 1.05 {
		t.Fatalf("a linear order must reach one fetch per byte, got %.4f", linearFetch)
	}
}
