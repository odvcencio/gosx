package beachgen

import (
	"bytes"
	"image"
	"math"
	"testing"
)

func TestCoastLifeAssetsDeterministicAndBounded(t *testing.T) {
	a, err := CoastLifeAssets(Seed)
	if err != nil {
		t.Fatal(err)
	}
	b, err := CoastLifeAssets(Seed)
	if err != nil {
		t.Fatal(err)
	}
	total := 0
	for _, name := range []string{"dune-grass.glb", "tideline-wrack.glb"} {
		if !bytes.Equal(a[name], b[name]) {
			t.Fatalf("unstable %s", name)
		}
		doc := mustParse(t, a[name])
		checkGLBEncoding(t, a[name], doc, false)
		total += len(a[name])
		t.Logf("%s: %d bytes, %d vertices", name, len(a[name]), positionCount(t, doc))
	}
	if total > 64<<10 {
		t.Fatalf("scatter uses %d bytes; cap is 64 KB", total)
	}
	if len(a["rock-rough.jpg"]) > 4<<10 {
		t.Fatal("basalt roughness exceeds 4 KB")
	}
}

func TestScatterNormalsFiniteAndPlantsKeepApproachClear(t *testing.T) {
	grass := duneGrass(Seed)
	for _, g := range []*geometry{grass, tidelineWrack(Seed)} {
		for i := 0; i < len(g.normals); i += 3 {
			length := math.Sqrt(g.normals[i]*g.normals[i] + g.normals[i+1]*g.normals[i+1] + g.normals[i+2]*g.normals[i+2])
			if math.Abs(length-1) > 1e-8 {
				t.Fatalf("invalid normal %g", length)
			}
		}
	}
	for i := 0; i < len(grass.positions); i += 3 {
		x, z := grass.positions[i], grass.positions[i+2]
		if math.Abs(x+5) < 4.5 || z < 12 || z > 40 {
			t.Fatalf("grass blocks the approach at %g,%g", x, z)
		}
	}
}

func TestWetSandRemainsGlossyBesideMonolith(t *testing.T) {
	data, err := makeMetalRoughness(newNoise(Seed))
	if err != nil {
		t.Fatal(err)
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	sample := func(x, z float64) float64 {
		px, py := int(math.Round((x+60)/120*255)), int(math.Round((z+40)/90*255))
		_, g, _, _ := img.At(px, py).RGBA()
		return float64(g) / 65535
	}
	if wet, dry := sample(MonolithX, MonolithZ), sample(0, 16); wet > .15 || dry < .65 || dry-wet < .5 {
		t.Fatalf("waterline roughness %g, dry sand %g", wet, dry)
	}
}
