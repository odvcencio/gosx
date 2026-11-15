//go:build !js || !wasm

package scene

import (
	"crypto/sha256"
	"encoding/hex"
	"math"
	"testing"
)

func pinnedShorelineTerrain(t *testing.T) *Terrain {
	t.Helper()
	h, err := HeightmapFromFunc(65, 65, func(u, v float64) float64 {
		return clampFloat(0.9-math.Hypot(u-0.45, v-0.55)*1.6+0.1*u, 0, 1)
	})
	if err != nil {
		t.Fatal(err)
	}
	return &Terrain{Heightmap: h, Width: 160, HeightScale: 30, Position: Vector3{Y: -12}}
}

func TestBakeShorelineNativeBytesArePinned(t *testing.T) {
	// The digests depend on the Go toolchain's compress/flate output. To
	// regenerate them, run this test and copy the "got" hashes from the failure
	// messages.
	cases := []struct {
		resolution int
		sum        string
	}{
		{0, "fc899c65484459711af58a90cb711a05264667d46829ff66524dedcb71450776"},
		{32, "6d8984e7f26599207a2caa6579f4724891b889a2b124e23d1d1258748fd79741"},
	}
	for _, c := range cases {
		b, err := BakeShoreline(pinnedShorelineTerrain(t), 0, OceanShoreline{Resolution: c.resolution, Distance: 24})
		if err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256([]byte(b.Src))
		if got := hex.EncodeToString(sum[:]); got != c.sum {
			t.Errorf("resolution %d: sha256 = %s, want %s (len %d)", c.resolution, got, c.sum, len(b.Src))
		}
	}
}
