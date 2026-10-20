package docs

import (
	"bytes"
	"math"
	"testing"

	"m31labs.dev/gosx/examples/gosx-docs/internal/beachgen"
	"m31labs.dev/gosx/scene"
)

func TestBeachGrottoConstructionAndTerrain(t *testing.T) {
	for _, period := range beachgen.Periods {
		t.Run(period, func(t *testing.T) {
			nodes := blackglassBeachGrotto(period)
			meshes := momentMeshes(t, "grotto", nodes)
			if !bytes.Equal(momentWire(t, nodes), momentWire(t, blackglassBeachGrotto(period))) {
				t.Fatal("grotto is not deterministic")
			}
			arch := meshes["sun-grotto-arch"].Geometry.(scene.BufferGeometry)
			if len(arch.Positions)/3 != 128 || len(arch.Indices)/3 != 64 {
				t.Fatal("grotto must retain its eight-sided vault")
			}
			// The sea-facing opening and inner vault need inward-facing
			// triangles; otherwise culling hides the cave from a walker.
			for i := 0; i < 8; i++ {
				base := i * 48
				if arch.Normals[base] < .9 || arch.Normals[base+12] > -.9 {
					t.Fatal("grotto front/back winding is reversed")
				}
				if i == 3 && (arch.Normals[base+25] < .8 || arch.Normals[base+37] > -.8) {
					t.Fatal("vault normals must face the exterior sky and interior walker")
				}
			}
			// Both inner and outer feet attach at the front and back.
			for _, j := range []int{0, 9, 12, 15, 7*48 + 3, 7*48 + 6, 7*48 + 18, 7*48 + 21} {
				x, y, z := arch.Positions[j], arch.Positions[j+1], arch.Positions[j+2]
				if math.Abs(y-beachgen.TerrainHeight(x, z, beachgen.Seed)) > .002 {
					t.Fatal("arch foot is not terrain-grounded")
				}
			}
			back := meshes["sun-grotto-back"]
			if math.Abs(back.Position.Y-beachgen.TerrainHeight(grottoBackX, grottoZ, beachgen.Seed)-1.15) > 1e-9 {
				t.Fatal("back wall must be terrain-grounded")
			}
			patch := meshes["sun-grotto-patch"]
			if patch.Visible == nil || *patch.Visible != (period == beachgen.PeriodGolden) || patch.CastShadow || patch.Material.(scene.StandardMaterial).Emissive >= 1.6 {
				t.Fatal("warm patch must appear only at golden hour without adding bloom/shadow work")
			}
			floor := patch.Geometry.(scene.BufferGeometry)
			for i := 0; i < len(floor.Positions); i += 3 {
				x, y, z := floor.Positions[i], floor.Positions[i+1], floor.Positions[i+2]
				if math.Abs(y-beachgen.TerrainHeight(x, z, beachgen.Seed)-.025) > .002 {
					t.Fatal("warm floor patch must follow the terrain")
				}
			}
		})
	}
}

func TestBeachGrottoSunAndWalkApproach(t *testing.T) {
	// Trace the golden sun from the rear floor to the opening. It passes
	// under the vault; the noon sun hits the roof and blue is below
	// the horizon. This aligns the period cue with the authored sun system.
	clearsVault := func(period string) bool {
		sun := beachgen.PeriodSky(period).SunDirection
		x, z := grottoBackX+1.5, grottoZ-1
		y := beachgen.TerrainHeight(x, z, beachgen.Seed) + .025
		d := (grottoFrontX - x) / sun.X
		z, y = z+d*sun.Z, y+d*sun.Y
		if d <= 0 || math.Abs(z-grottoZ) > 1.5 {
			return false
		}
		floor := beachgen.TerrainHeight(grottoFrontX, z, beachgen.Seed)
		ceiling := floor + 2.4*math.Sqrt(1-math.Pow((z-grottoZ)/1.8, 2))
		return y >= floor && y <= ceiling
	}
	if !clearsVault(beachgen.PeriodGolden) || clearsVault(beachgen.PeriodNoon) || clearsVault(beachgen.PeriodBlue) {
		t.Fatal("only the golden-hour sun must reach the grotto floor through its opening")
	}
	if len(blackglassBeachGrottoColliders()) != 3 {
		t.Fatal("grotto needs solid sides and a back, with an open entrance")
	}
	checkBeachApproach(t, []scene.Vector3{scene.Vec3(.5, 0, 22), scene.Vec3(.5, 0, 12), scene.Vec3(-18, 0, 12), scene.Vec3(grottoFrontX-1.2, 0, grottoZ)})
}
