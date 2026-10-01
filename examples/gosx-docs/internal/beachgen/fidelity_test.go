package beachgen

import (
	"bytes"
	"image"
	"math"
	"testing"
)

func TestShorelineHasNoHeightOrSlopeStep(t *testing.T) {
	const delta = .001
	left := (shorelineHeight(0) - shorelineHeight(-delta)) / delta
	right := (shorelineHeight(delta) - shorelineHeight(0)) / delta
	if math.Abs(left-right) > .001 || left <= 0 || right <= 0 {
		t.Fatalf("shore slopes disagree: %g / %g", left, right)
	}
}

func TestBasaltAtlasKeepsWetBasesDarkAndSmooth(t *testing.T) {
	albedo, mr, err := makeRockAtlas(Seed)
	if err != nil {
		t.Fatal(err)
	}
	colorMap, _, err := image.Decode(bytes.NewReader(albedo))
	if err != nil {
		t.Fatal(err)
	}
	roughMap, _, err := image.Decode(bytes.NewReader(mr))
	if err != nil {
		t.Fatal(err)
	}
	for tile := 0; tile < 5; tile++ {
		x := tile*rockAtlasTile + rockAtlasTile/2
		_, wet, _, _ := roughMap.At(x, 8).RGBA()
		_, dry, _, _ := roughMap.At(x, rockAtlasTile-8).RGBA()
		wr, wg, wb, _ := colorMap.At(x, 8).RGBA()
		dr, dg, db, _ := colorMap.At(x, rockAtlasTile-8).RGBA()
		if wet >= dry || wr+wg+wb >= dr+dg+db || dr+dg+db > 3*64*257 {
			t.Fatalf("basalt %d lost dark, polished wetness or charcoal crown", tile)
		}
	}
}

func TestObeliskIsClosedAndHasOpticalVolume(t *testing.T) {
	g, err := monolithGeometry()
	if err != nil {
		t.Fatal(err)
	}
	volume := 0.0
	for i := 0; i < len(g.indices); i += 3 {
		point := func(j int) vec3 {
			k := int(g.indices[i+j]) * 3
			return vec3{g.positions[k], g.positions[k+1], g.positions[k+2]}
		}
		volume += dot(point(0), cross(point(1), point(2))) / 6
	}
	if volume < 4 {
		t.Fatalf("obelisk volume %g; optical slab needs at least 4 cubic metres", volume)
	}
}
