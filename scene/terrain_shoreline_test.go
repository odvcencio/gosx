package scene

import (
	"bytes"
	"encoding/base64"
	"image/png"
	"math"
	"strings"
	"testing"
)

// islandTerrain is a cone-shaped island: ground height 10 at the centre falling
// linearly to -10 at radius 100, on a 200 m terrain.
func islandTerrain(t *testing.T) *Terrain {
	t.Helper()
	h, err := HeightmapFromFunc(129, 129, func(u, v float64) float64 {
		r := math.Hypot(u-0.5, v-0.5) * 200
		return clampFloat(1-r/100, 0, 1)
	})
	if err != nil {
		t.Fatal(err)
	}
	return &Terrain{Heightmap: h, Width: 200, HeightScale: 20, Position: Vector3{Y: -10}}
}

func decodeShoreline(t *testing.T, b *OceanBathymetry) func(x, z float64) (r, g float64) {
	t.Helper()
	data, ok := strings.CutPrefix(b.Src, "data:image/png;base64,")
	if !ok {
		t.Fatalf("src is not a PNG data URL: %.40s", b.Src)
	}
	raw, err := base64.StdEncoding.DecodeString(data)
	if err != nil {
		t.Fatal(err)
	}
	img, err := png.Decode(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	n := img.Bounds().Dx()
	return func(x, z float64) (float64, float64) {
		i := int((x - b.MinX) / (b.MaxX - b.MinX) * float64(n))
		j := int((z - b.MinZ) / (b.MaxZ - b.MinZ) * float64(n))
		r, g, _, _ := img.At(min(max(i, 0), n-1), min(max(j, 0), n-1)).RGBA()
		return float64(r) / 65535, float64(g) / 65535
	}
}

func TestBakeShorelineDistanceField(t *testing.T) {
	terrain := islandTerrain(t)
	b, err := BakeShoreline(terrain, 0, OceanShoreline{Distance: 20})
	if err != nil {
		t.Fatal(err)
	}
	if b.Encoding != "signed-sqrt" || b.ShoreDistance != 20 || b.ShoreFoam != 3 || b.ShoreTurbidity != 0.35 || b.MinX != -100 || b.MaxZ != 100 {
		t.Fatalf("bathymetry = %+v", b)
	}
	sample := decodeShoreline(t, b)
	// The waterline is the circle of radius 50 (height 0). Distance along +X:
	// G = 0.5 + d/(2*20) with d positive inland.
	for _, c := range []struct{ x, want float64 }{{50, 0.5}, {40, 0.75}, {60, 0.25}, {80, 0}, {20, 1}} {
		_, g := sample(c.x, 0)
		if math.Abs(g-c.want) > 0.04 {
			t.Errorf("G at x=%v is %.3f, want %.3f", c.x, g, c.want)
		}
	}
	// R decodes to the seabed height through the signed-sqrt encoding.
	r, _ := sample(75, 0)
	s := 2*r - 1
	if got := math.Copysign(s*s, s) * b.MaxHeight; math.Abs(got-terrain.HeightAt(75, 0)) > 0.5 {
		t.Fatalf("R at x=75 decodes to %.3f m, terrain is %.3f m", got, terrain.HeightAt(75, 0))
	}
	// The bake is cached per terrain, level and settings.
	again, _ := BakeShoreline(terrain, 0, OceanShoreline{Distance: 20})
	if again.Src != b.Src {
		t.Fatal("second bake differs")
	}
	higher, _ := BakeShoreline(terrain, 4, OceanShoreline{Distance: 20})
	if higher.Src == b.Src {
		t.Fatal("a different water level reused the cached bake")
	}
	if _, err := BakeShoreline(&Terrain{}, 0, OceanShoreline{}); err == nil {
		t.Fatal("a terrain without a heightmap baked a shoreline")
	}
}

func TestDistanceTransformIsExact(t *testing.T) {
	// One land texel in an 8x8 grid: every texel holds its exact squared
	// distance to it.
	land := make([]bool, 64)
	land[3*8+5] = true
	got := squaredDistanceField(land, 8, 2, 3, true)
	for j := 0; j < 8; j++ {
		for i := 0; i < 8; i++ {
			dx, dz := float64(i-5)*2, float64(j-3)*3
			if want := dx*dx + dz*dz; math.Abs(got[j*8+i]-want) > 1e-9 {
				t.Fatalf("texel (%d,%d) = %v, want %v", i, j, got[j*8+i], want)
			}
		}
	}
}

func TestOceanShorelineLowersFromTheTerrain(t *testing.T) {
	terrain := islandTerrain(t)
	props := Props{Environment: Environment{Terrain: terrain, Ocean: &Ocean{Shoreline: &OceanShoreline{Resolution: 64}}}}
	ocean := props.SceneIR().Environment.Ocean
	if ocean == nil || ocean.Bathymetry == nil || ocean.Bathymetry.ShoreDistance != 32 || !strings.HasPrefix(ocean.Bathymetry.Src, "data:image/png") {
		t.Fatalf("ocean = %+v", ocean)
	}
	if ocean.Shoreline == nil || ocean.Shoreline.Resolution != 64 {
		t.Fatalf("shoreline settings = %+v", ocean.Shoreline)
	}
	authored := Props{Environment: Environment{Terrain: terrain, Ocean: &Ocean{Shoreline: &OceanShoreline{},
		Bathymetry: &OceanBathymetry{Src: "seabed.png", MinX: -1, MinZ: -1, MaxX: 1, MaxZ: 1, MinHeight: -1, MaxHeight: 1}}}}
	if got := authored.SceneIR().Environment.Ocean.Bathymetry.Src; got != "seabed.png" {
		t.Fatalf("an authored bathymetry was replaced by %q", got)
	}
	noTerrain := Props{Environment: Environment{Ocean: &Ocean{Shoreline: &OceanShoreline{}}}}
	if noTerrain.SceneIR().Environment.Ocean.Bathymetry != nil {
		t.Fatal("a shoreline baked without a terrain")
	}
}
