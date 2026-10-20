package docs

import (
	"bytes"
	"encoding/json"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"m31labs.dev/gosx/assetpipe/gltfedit"
	"m31labs.dev/gosx/examples/gosx-docs/internal/beachgen"
)

func TestOffshoreAssetsMatchSourceAndStayBounded(t *testing.T) {
	assets, err := BlackglassBeachOffshoreAssets()
	if err != nil {
		t.Fatal(err)
	}
	total := 0
	for name, data := range assets {
		committed, err := os.ReadFile("../../../public/models/blackglass/" + name)
		if err != nil || !bytes.Equal(committed, data) {
			t.Fatalf("%s must be regenerated from Go source", name)
		}
		total += len(data)
		if filepath.Ext(name) != ".glb" {
			continue
		}
		doc, err := gltfedit.Parse(data, nil)
		if err != nil {
			t.Fatal(err)
		}
		for _, p := range doc.Meshes[0].Primitives {
			positions, _, err := doc.ReadAccessor(p.Attributes["POSITION"])
			if err != nil {
				t.Fatal(err)
			}
			if len(positions)/3 > 22000 {
				t.Fatalf("%s exceeds bounded vertex budget", name)
			}
			normals, _, err := doc.ReadAccessor(p.Attributes["NORMAL"])
			if err != nil {
				t.Fatal(err)
			}
			for i, v := range positions {
				if math.IsNaN(v) || math.IsInf(v, 0) {
					t.Fatal("nonfinite island geometry")
				}
				if name == "offshore-islands.glb" && i%3 == 1 && normals[i] <= 0 {
					t.Fatal("island terrain must face upward")
				}
			}
		}
		t.Logf("%s: %d bytes", name, len(data))
	}
	if len(assets) != 7 || total > 900000 {
		t.Fatalf("offshore assets count/size: %d / %d", len(assets), total)
	}
}

func TestOffshoreVoyagesLandWalkDiscoverAndReturn(t *testing.T) {
	root, err := filepath.Abs("../../../../..")
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(map[string]any{"vessel": blackglassBeachVessel(), "walk": blackglassBeachWalk(), "ocean": map[string]any{"level": 0}, "finds": json.RawMessage(blackglassIslandFindSites()), "harborExit": json.RawMessage(blackglassHarborExit()), "places": json.RawMessage(blackglassBeachChartPlaces())})
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("node", filepath.Join(root, "client/js/testdata/blackglass-islands.cjs"), root)
	cmd.Stdin = bytes.NewReader(data)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("island voyage: %v\n%s", err, out)
	}
	t.Logf("%s", out)
	if len(blackglassBeachWalk().Grounds) != 3 {
		t.Fatal("islands need sparse ground patches")
	}
	for _, island := range beachgen.OffshoreIslands() {
		if blackglassIslandFindPosition(island).Y <= 0 {
			t.Fatal("find must be on dry land")
		}
	}
}
