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
				if arch.Normals[base+2] > -.9 || arch.Normals[base+14] < .9 {
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
			if math.Abs(back.Position.Y-beachgen.TerrainHeight(grottoX, grottoBackZ, beachgen.Seed)-1.15) > 1e-9 {
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
	// under the vault; the noon sun faces the solid back and blue is below
	// the horizon. This aligns the period cue with the authored sun system.
	sun := beachgen.PeriodSky(beachgen.PeriodGolden).SunDirection
	x, z := grottoX, 11.7
	y := beachgen.TerrainHeight(x, z, beachgen.Seed) + .025
	d := (grottoFrontZ - z) / sun.Z
	x, y = x+d*sun.X, y+d*sun.Y
	floor := beachgen.TerrainHeight(x, grottoFrontZ, beachgen.Seed)
	ceiling := floor + 2.4*math.Sqrt(1-math.Pow((x-grottoX)/1.8, 2))
	if math.Abs(x-grottoX) > 1.5 || y < floor || y > ceiling || beachgen.PeriodSky(beachgen.PeriodNoon).SunDirection.Z < 0 || beachgen.PeriodSky(beachgen.PeriodBlue).SunDirection.Y >= 0 {
		t.Fatal("grotto opening does not admit the golden-hour sun")
	}
	if len(blackglassBeachGrottoColliders()) != 3 {
		t.Fatal("grotto needs solid sides and a back, with an open entrance")
	}
	checkBeachApproach(t, []scene.Vector3{scene.Vec3(.5, 0, 22), scene.Vec3(-20, 0, 17), scene.Vec3(-20, 0, 5.5), scene.Vec3(grottoX, 0, 5.5), scene.Vec3(grottoX, 0, 11.7)})
}
