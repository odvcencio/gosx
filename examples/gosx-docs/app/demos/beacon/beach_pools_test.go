package docs

import (
	"bytes"
	"fmt"
	"math"
	"testing"

	"m31labs.dev/gosx/examples/gosx-docs/internal/beachgen"
	"m31labs.dev/gosx/scene"
)

func TestBeachPoolsConstructionAndTerrain(t *testing.T) {
	nodes := blackglassBeachPools()
	meshes := momentMeshes(t, nodes, 4, 7_400)
	if !bytes.Equal(momentWire(t, nodes), momentWire(t, blackglassBeachPools())) {
		t.Fatal("pools are not deterministic")
	}
	for i, site := range blackglassBeachPoolSites() {
		m := meshes[fmt.Sprintf("tide-pool-%d", i)]
		g := m.Geometry.(scene.CylinderGeometry)
		mat := m.Material.(scene.StandardMaterial)
		if m.Position.X != site[0] || m.Position.Z != site[1] || g.RadiusTop != site[2] || g.Segments != 12 {
			t.Fatal("pool lost its site or cheap disc geometry")
		}
		level := m.Position.Y + g.Height/2
		for s := 0; s < 12; s++ {
			a := 2 * math.Pi * float64(s) / 12
			h := beachgen.TerrainHeight(site[0]+site[2]*math.Cos(a), site[1]+site[2]*math.Sin(a), beachgen.Seed)
			if level < h+.024 || level-h > .22 {
				t.Fatalf("pool %d must sit just above the terrain: water=%g, terrain=%g", i, level, h)
			}
		}
		if mat.Roughness > .05 || mat.Clearcoat != 1 || mat.RimStrength <= 0 || m.CastShadow || !m.ReceiveShadow {
			t.Fatal("pool must retain its view-dependent glint and bounded lighting")
		}
	}
	rim := meshes["tide-pool-rims"].Geometry.(scene.BufferGeometry)
	if len(rim.Positions)/3 != 144 || len(rim.Indices)/3 != 72 {
		t.Fatal("pool rims exceeded their geometry budget")
	}
	for i := 0; i < len(rim.Positions); i += 12 {
		for _, j := range []int{i + 6, i + 9} {
			x, y, z := rim.Positions[j], rim.Positions[j+1], rim.Positions[j+2]
			if math.Abs(y-beachgen.TerrainHeight(x, z, beachgen.Seed)-.018) > .002 {
				t.Fatalf("rim at %g,%g is not terrain-grounded", x, z)
			}
		}
	}
}

func TestBeachPoolSitesAreWalkable(t *testing.T) {
	for _, s := range blackglassBeachPoolSites() {
		checkBeachApproach(t, []scene.Vector3{scene.Vec3(.5, 0, 22), scene.Vec3(s[0], 0, s[1]+2)})
	}
}
