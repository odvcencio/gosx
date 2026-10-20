package docs

import (
	"math"
	"testing"

	"m31labs.dev/gosx/assetpipe/gltfedit"
	"m31labs.dev/gosx/examples/gosx-docs/internal/beachgen"
	"m31labs.dev/gosx/scene"
)

func TestBlackglassDiscoveryTargetsAndApproach(t *testing.T) {
	p := BlackglassBeachProgram("pools", beachgen.PeriodGolden)
	interactive := 0
	for _, node := range p.Graph.Nodes {
		switch n := node.(type) {
		case scene.Mesh:
			if n.Interactive {
				interactive++
				if n.Label == "" {
					t.Fatal("pool requires a keyboard/screen-reader name")
				}
			} else if n.Pickable == nil || *n.Pickable {
				t.Fatal("non-interactive scenery must opt out of pointer queries")
			}
		case scene.Model:
			if n.Interactive {
				interactive++
				if n.Label == "" || (n.ID != "shore-handheld" && n.ID != "drift-letter" && n.ID != "tide-bell" && n.ID != "star-dial") {
					t.Fatal("collectible requires a stable labeled target")
				}
			} else if n.Pickable == nil || *n.Pickable {
				t.Fatal("heavy scenery must opt out of pointer queries")
			}
		}
	}
	if interactive != 7 || p.Controls != scene.ControlFirstPerson || p.Camera.PortraitFOV < 90 {
		t.Fatal("seven bounded targets and a useful portrait pool view required")
	}
	checkBeachApproach(t, []scene.Vector3{scene.Vec3(.5, 0, 22), scene.Vec3(blackglassHandheldX, 0, blackglassHandheldZ)})
	h := beachgen.TerrainHeight(blackglassHandheldX, blackglassHandheldZ, beachgen.Seed)
	t.Logf("handheld center: x=%g y=%g z=%g", blackglassHandheldX, h+.13, blackglassHandheldZ)
}

func TestBlackglassDiscoveryAssetsHaveCompleteMaterials(t *testing.T) {
	assets, err := BlackglassBeachStaticAssets()
	if err != nil {
		t.Fatal(err)
	}
	for name, want := range map[string]int{"tide-pool-life.glb": 3, "shore-handheld.glb": 4} {
		doc, err := gltfedit.Parse(assets[name], nil)
		if err != nil {
			t.Fatal(err)
		}
		if len(doc.Meshes) != 1 || len(doc.Meshes[0].Primitives) != want {
			t.Fatal("material groups must share one retained model")
		}
		for _, primitive := range doc.Meshes[0].Primitives {
			positions, _, err := doc.ReadAccessor(primitive.Attributes["POSITION"])
			if err != nil {
				t.Fatal(err)
			}
			normals, _, err := doc.ReadAccessor(primitive.Attributes["NORMAL"])
			if err != nil {
				t.Fatal(err)
			}
			indices, _, err := doc.ReadAccessor(*primitive.Indices)
			if err != nil {
				t.Fatal(err)
			}
			if len(positions)/3 > 700 {
				t.Fatal("small discoveries exceeded their geometry cap")
			}
			for i := 0; i < len(indices); i += 3 {
				a, b, c := int(indices[i])*3, int(indices[i+1])*3, int(indices[i+2])*3
				ux, uy, uz := positions[b]-positions[a], positions[b+1]-positions[a+1], positions[b+2]-positions[a+2]
				vx, vy, vz := positions[c]-positions[a], positions[c+1]-positions[a+1], positions[c+2]-positions[a+2]
				x, y, z := uy*vz-uz*vy, uz*vx-ux*vz, ux*vy-uy*vx
				length := math.Sqrt(x*x + y*y + z*z)
				if length == 0 || (x*normals[a]+y*normals[a+1]+z*normals[a+2])/length < .55 {
					t.Fatalf("%s has a degenerate or reversed face", name)
				}
			}
		}
	}
}
