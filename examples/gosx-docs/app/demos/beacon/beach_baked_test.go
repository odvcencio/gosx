package docs

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"math"
	"os"
	"testing"

	"m31labs.dev/gosx/assetpipe/gltfedit"
	"m31labs.dev/gosx/examples/gosx-docs/internal/beachgen"
	"m31labs.dev/gosx/scene"
)

func TestBeachBakedAssetsKeepSurfacesAndWinding(t *testing.T) {
	first, err := BlackglassBeachStaticAssets()
	if err != nil {
		t.Fatal(err)
	}
	second, err := BlackglassBeachStaticAssets()
	if err != nil {
		t.Fatal(err)
	}
	if len(first) != 4 {
		t.Fatal("four static assets required")
	}
	total := 0
	for name, data := range first {
		total += len(data)
		if !bytes.Equal(data, second[name]) {
			t.Fatal("static asset is not deterministic")
		}
		committed, err := os.ReadFile("../../../public/models/blackglass/" + name)
		if err != nil || !bytes.Equal(data, committed) {
			t.Fatalf("%s must be regenerated from its authored surfaces", name)
		}
		doc, err := gltfedit.Parse(data, nil)
		if err != nil {
			t.Fatal(err)
		}
		primitive := doc.Meshes[0].Primitives[0]
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
		transform := doc.Nodes[0]
		for i := range positions {
			positions[i] = positions[i]*transform.Scale[i%3] + transform.Translation[i%3]
		}
		var authored []float64
		switch name {
		case "tide-pool-rims.glb":
			authored = blackglassBeachPools()[3].(scene.Mesh).Geometry.(scene.BufferGeometry).Positions
		case "sun-grotto-arch.glb":
			authored = blackglassBeachGrotto(beachgen.PeriodGolden)[0].(scene.Mesh).Geometry.(scene.BufferGeometry).Positions
		}
		for i, v := range authored {
			if math.Abs(v-positions[i]) > .002 {
				t.Fatalf("%s surface changed at coordinate %d", name, i)
			}
		}
		for i := 0; i < len(indices); i += 3 {
			a, b, c := int(indices[i])*3, int(indices[i+1])*3, int(indices[i+2])*3
			ux, uy, uz := positions[b]-positions[a], positions[b+1]-positions[a+1], positions[b+2]-positions[a+2]
			vx, vy, vz := positions[c]-positions[a], positions[c+1]-positions[a+1], positions[c+2]-positions[a+2]
			nx, ny, nz := uy*vz-uz*vy, uz*vx-ux*vz, ux*vy-uy*vx
			length := math.Sqrt(nx*nx + ny*ny + nz*nz)
			if length == 0 || (nx*normals[a]+ny*normals[a+1]+nz*normals[a+2])/length < .6 {
				t.Fatalf("%s has degenerate or reversed triangle %d", name, i/3)
			}
		}
		if name == "glass-trail-soles.glb" {
			if len(positions)/3 != 48*9 {
				t.Fatal("48 complete print surfaces required")
			}
			for i := 0; i < len(positions); i += 27 {
				x, y, z := positions[i], positions[i+1], positions[i+2]
				if math.Abs(y-beachgen.TerrainHeight(x, z, beachgen.Seed)-.022) > .002 {
					t.Fatal("print surface detached from sand")
				}
			}
		}
		if name == "wreck-keel.glb" {
			prow := false
			for i := 0; i < len(positions); i += 3 {
				prow = prow || math.Abs(positions[i]-wreckX) < .2 && math.Abs(positions[i+2]-(wreckFrontZ-.6)) < .2 &&
					math.Abs(positions[i+1]-beachgen.TerrainHeight(wreckX, wreckFrontZ-.6, beachgen.Seed)-2.7) < .002
			}
			if !prow {
				t.Fatal("merged wreck lost the high prow")
			}
		}
		t.Logf("%s: %d bytes, %d vertices", name, len(data), len(positions)/3)
	}
	if total > 32<<10 {
		t.Fatalf("static assets exceed 32 KB: %d", total)
	}
}

func TestBeachBakedMomentsKeepMaterialsAndWalkData(t *testing.T) {
	want := map[string]string{"tide-pool-rims": "#23292a", "sun-grotto-arch": "#1e2428", "glass-trail-soles": "#090d10", "wreck-keel": "#73533b"}
	roughness := map[string]float64{"tide-pool-rims": .65, "sun-grotto-arch": .92, "glass-trail-soles": .88, "wreck-keel": .94}
	receivesShadow := map[string]bool{"tide-pool-rims": true, "sun-grotto-arch": false, "glass-trail-soles": true, "wreck-keel": true}
	assets, err := BlackglassBeachStaticAssets()
	if err != nil {
		t.Fatal(err)
	}
	for _, node := range blackglassBeachBakedMoments(beachgen.PeriodGolden) {
		model, ok := node.(scene.Model)
		if !ok {
			continue
		}
		if model.Src != blackglassBeachModelRoot+model.ID+".glb" || model.Material != nil || model.ReceiveShadow != receivesShadow[model.ID] || model.Static == nil || !*model.Static {
			t.Fatal("baked moment lost its source, material or grounding")
		}
		data := assets[model.ID+".glb"]
		var root struct {
			Materials []struct {
				PBR struct {
					Color     [4]float64 `json:"baseColorFactor"`
					Roughness float64    `json:"roughnessFactor"`
					Metalness float64    `json:"metallicFactor"`
				} `json:"pbrMetallicRoughness"`
			} `json:"materials"`
		}
		if err := json.Unmarshal(data[20:20+binary.LittleEndian.Uint32(data[12:16])], &root); err != nil {
			t.Fatal(err)
		}
		material := root.Materials[0].PBR
		if material.Roughness != roughness[model.ID] || material.Metalness != 0 || material.Color[3] != 1 {
			t.Fatal("asset lost its PBR material")
		}
		rgb, _ := hex.DecodeString(want[model.ID][1:])
		for i, channel := range rgb {
			c := float64(channel) / 255
			if c <= .04045 {
				c /= 12.92
			} else {
				c = math.Pow((c+.055)/1.055, 2.4)
			}
			if math.Abs(c-material.Color[i]) > 1e-9 {
				t.Fatal("asset changed its authored color")
			}
		}
		delete(want, model.ID)
	}
	if len(want) != 0 {
		t.Fatal("baked moment missing")
	}
	if len(blackglassBeachWalk().Colliders) != len(BlackglassBeachProgram("shore", beachgen.PeriodGolden).Walk.Colliders) {
		t.Fatal("baking changed walk obstacles")
	}
}

func TestBeachRibsKeepSubmillimetrePlacement(t *testing.T) {
	authored := blackglassBeachWreck()[0].(scene.InstancedMesh)
	for _, node := range blackglassBeachBakedMoments(beachgen.PeriodGolden) {
		ribs, ok := node.(scene.InstancedMesh)
		if !ok || ribs.ID != "wreck-ribs" {
			continue
		}
		if ribs.Count != authored.Count {
			t.Fatal("wreck lost ribs")
		}
		for i, p := range ribs.Positions {
			q, r, a := authored.Positions[i], ribs.Rotations[i], authored.Rotations[i]
			for _, delta := range []float64{p.X - q.X, p.Y - q.Y, p.Z - q.Z, r.X - a.X, r.Y - a.Y, r.Z - a.Z,
				ribs.Scales[i].X - authored.Scales[i].X, ribs.Scales[i].Y - authored.Scales[i].Y, ribs.Scales[i].Z - authored.Scales[i].Z} {
				if math.Abs(delta) > .000051 {
					t.Fatal("compact transform changed authored placement")
				}
			}
		}
		return
	}
	t.Fatal("wreck ribs must retain their instanced draw")
}
