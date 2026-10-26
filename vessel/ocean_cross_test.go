package vessel_test

import (
	"math"
	"testing"

	"m31labs.dev/gosx/scene"
	"m31labs.dev/gosx/vessel"
)

// Sea keeps only the vertical term of the ocean's Gerstner sum. It should stay
// within a modest share of the wave height of the full query.
func TestSeaTracksSceneOceanQuery(t *testing.T) {
	o := scene.Ocean{WaveHeight: 1.2, WaveLength: 40, WindDirection: 30}
	q := scene.NewOceanQuery(o, "high", nil)
	sea := vessel.NewSea(vessel.SeaConfig{WaveHeight: 1.2, WaveLength: 40, WindDirection: 30})
	var sumSq, sumRef float64
	n := 0
	for i := 0; i < 400; i++ {
		x, z, tm := float64(i)*7.3-1500, float64(i)*-4.1+800, float64(i)*0.37
		want := q.Sample(x, z, tm).Position.Y
		got := sea.Height(x, z, tm)
		sumSq += (want - got) * (want - got)
		sumRef += want * want
		n++
	}
	err, ref := math.Sqrt(sumSq/float64(n)), math.Sqrt(sumRef/float64(n))
	t.Logf("rms difference %.3f m against rms wave %.3f m", err, ref)
	if err > 0.35*ref {
		t.Errorf("Sea differs from scene.OceanQuery by %.3f m rms, wave rms %.3f m", err, ref)
	}
}
