package beachgen

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"image"
	"math"
	"os"
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
	if volume < 2 {
		t.Fatalf("obelisk volume %g; solid obelisk needs at least 2 cubic metres", volume)
	}
	// Every triangle belongs to one optical surface; neither material may
	// leave a hole or duplicate the closed volume at their shared boundary.
	seen := make(map[uint16]bool)
	for _, group := range g.materialIndices {
		for _, index := range group {
			if seen[index] {
				t.Fatal("optical surfaces overlap")
			}
			seen[index] = true
		}
	}
	if len(seen) != len(g.indices) {
		t.Fatal("optical surfaces leave an opening")
	}
}

func TestCommittedObsidianAbsorbsInItsBodyAndTransmitsAtBevels(t *testing.T) {
	data, err := os.ReadFile("../../public/models/blackglass/monolith-v2.glb")
	if err != nil {
		t.Fatal(err)
	}
	var root struct {
		Materials []struct{ Extensions map[string]json.RawMessage }
	}
	n := binary.LittleEndian.Uint32(data[12:16])
	if err := json.Unmarshal(data[20:20+n], &root); err != nil {
		t.Fatal(err)
	}
	if len(root.Materials) != 2 {
		t.Fatal("obsidian needs distinct body and thin bevel surfaces")
	}
	for i, material := range root.Materials {
		var transmission struct{ TransmissionFactor float64 }
		var volume struct{ ThicknessFactor, AttenuationDistance float64 }
		_ = json.Unmarshal(material.Extensions["KHR_materials_transmission"], &transmission)
		_ = json.Unmarshal(material.Extensions["KHR_materials_volume"], &volume)
		if volume.ThicknessFactor <= 0 || volume.AttenuationDistance <= 0 || (i == 0 && transmission.TransmissionFactor > .02) || (i == 1 && transmission.TransmissionFactor < .2) {
			t.Fatal("obsidian lost its dark body or refracted bevels")
		}
	}
}

func TestSwashGeometryStaysSmallAndFollowsWetSand(t *testing.T) {
	g := SwashGeometry(Seed)
	if len(g.Positions)/3 > 100 || len(g.Indices)/3 > 128 {
		t.Fatal("shore wash exceeds its vertex or triangle budget")
	}
	for i := 0; i < len(g.Positions); i += 3 {
		x, y, z := g.Positions[i], g.Positions[i+1], g.Positions[i+2]
		if math.Abs(y-TerrainHeight(x, z, Seed)-.018) > .004 {
			t.Fatalf("foam detached from sand at %g,%g", x, z)
		}
	}
}
