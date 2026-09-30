package beachgen

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"math"
	"testing"

	"m31labs.dev/gosx/assetpipe/gltfedit"
)

func TestGenerateAssets(t *testing.T) {
	first, err := Generate(0xB1AC6A55)
	if err != nil {
		t.Fatal(err)
	}
	second, err := Generate(0xB1AC6A55)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"beach-v2.glb", "beach-v2-albedo.jpg", "beach-v2-mr.png", "beach-v2-height.png", "sand-normal.jpg", "rock-normal.jpg", "stacks-v2.glb", "monolith-v2.glb"}
	if len(first) != len(want) {
		t.Fatalf("got %d output files, want %d", len(first), len(want))
	}
	for _, name := range want {
		got, ok := first[name]
		if !ok {
			t.Fatalf("missing output %q", name)
		}
		if !bytes.Equal(got, second[name]) {
			t.Errorf("%s is not byte-identical across runs", name)
		}
		t.Logf("%s: %d bytes", name, len(got))
	}

	for _, name := range []string{"beach-v2.glb", "stacks-v2.glb", "monolith-v2.glb"} {
		if _, err := gltfedit.Parse(first[name], nil); err != nil {
			t.Errorf("%s does not parse: %v", name, err)
		}
	}
	beach := mustParse(t, first["beach-v2.glb"])
	stacks := mustParse(t, first["stacks-v2.glb"])
	monolith := mustParse(t, first["monolith-v2.glb"])
	if count := positionCount(t, beach); count != 129*129 {
		t.Errorf("beach has %d vertices, want %d", count, 129*129)
	}
	t.Logf("beach-v2.glb: %d vertices, %d in-memory bytes", positionCount(t, beach), len(first["beach-v2.glb"]))
	if count := positionCount(t, stacks); count != 22018 {
		t.Errorf("stacks, boulders, and cliffs have %d vertices, want 22018", count)
	}
	if count := positionCount(t, stacks); count > 36000 {
		t.Errorf("stacks and cliffs have %d vertices, exceeding the 36000-vertex budget", count)
	}
	t.Logf("stacks-v2.glb: %d vertices, %d in-memory bytes", positionCount(t, stacks), len(first["stacks-v2.glb"]))
	if count := positionCount(t, monolith); count < 150 || count > 400 {
		t.Errorf("monolith has %d vertices, want a faceted shard of 150-400", count)
	}
	t.Logf("monolith-v2.glb: %d vertices, %d in-memory bytes", positionCount(t, monolith), len(first["monolith-v2.glb"]))

	beachLow, beachHigh := checkBounds(t, first["beach-v2.glb"], beach, [3]float64{-60.1, -7, -40.1}, [3]float64{60.1, 22, 50.1})
	checkNear(t, "terrain west", beachLow[0], -60, .01)
	checkNear(t, "terrain south", beachLow[2], -40, .01)
	checkNear(t, "terrain east", beachHigh[0], 60, .01)
	checkNear(t, "terrain north", beachHigh[2], 50, .01)
	if beachLow[1] < -5.1 || beachLow[1] > -4.5 || beachHigh[1] < 20.5 || beachHigh[1] > 21.5 {
		t.Errorf("terrain vertical bounds are [%.3f, %.3f], expected the sea basin and ridged headlands", beachLow[1], beachHigh[1])
	}
	stackLow, stackHigh := checkBounds(t, first["stacks-v2.glb"], stacks, [3]float64{-50, -6.1, -52}, [3]float64{54, 21.5, 51})
	checkNear(t, "stack bottoms", stackLow[1], -6, .01)
	if stackHigh[1] < 20.5 || stackHigh[1] > 21.5 {
		t.Errorf("stack and cliff tops reach %.3f m; want the left cliff near 21 m", stackHigh[1])
	}
	monoLow, monoHigh := checkBounds(t, first["monolith-v2.glb"], monolith, [3]float64{-1.2, -.01, -.6}, [3]float64{1.2, 3.5, .6})
	checkNear(t, "monolith bottom", monoLow[1], 0, .01)
	checkNear(t, "monolith apex", monoHigh[1], 3.42, .01)

	checkGLBEncoding(t, first["beach-v2.glb"], beach, true)
	checkGLBEncoding(t, first["stacks-v2.glb"], stacks, false)
	checkRockMaterial(t, first["stacks-v2.glb"])
	checkGLBEncoding(t, first["monolith-v2.glb"], monolith, false)
	if h := TerrainHeight(0, 0, 0xB1AC6A55); math.Abs(h) > .1 {
		t.Errorf("terrain height at (0,0) = %.4f, want within 0.1m of zero", h)
	}
	if h := TerrainHeight(0, -20, 0xB1AC6A55); h >= -2 {
		checkNear(t, "monolith top", monoHigh[1], 3.4+.8*math.Tan(12*math.Pi/180), .02)
		t.Errorf("terrain height at (0,-20) = %.4f, want below -2m", h)
	}

	for name, budget := range map[string]int{"beach-v2.glb": 560 << 10, "stacks-v2.glb": 520 << 10, "monolith-v2.glb": 20 << 10} {
		if len(first[name]) > budget {
			t.Errorf("%s is %d bytes, budget is %d", name, len(first[name]), budget)
		}
	}
	for name, size := range map[string][2]int{"beach-v2-mr.png": {256, 256}, "sand-normal.jpg": {512, 512}, "rock-normal.jpg": {256, 256}} {
		decoded, _, err := image.Decode(bytes.NewReader(first[name]))
		if err != nil {
			t.Errorf("%s is not a decodable image: %v", name, err)
			continue
		}
		if bounds := decoded.Bounds(); bounds.Dx() != size[0] || bounds.Dy() != size[1] {
			t.Errorf("%s is %dx%d, want %dx%d", name, bounds.Dx(), bounds.Dy(), size[0], size[1])
		}
	}
}

func TestHeadlandCliffs(t *testing.T) {
	seed := generatorSeed
	if height := TerrainHeight(-45, 10, seed); height >= 3 {
		t.Errorf("terrain at (-45,10) = %.3fm, want below 3m beneath the cliff mesh", height)
	}
	heightData, err := makeBathymetry(newNoise(seed))
	if err != nil {
		t.Fatal(err)
	}
	img, err := png.Decode(bytes.NewReader(heightData))
	if err != nil {
		t.Fatal(err)
	}
	x := int(math.Round((-45-BathymetryMinX)/(BathymetryMaxX-BathymetryMinX)*BathymetrySize - .5))
	z := int(math.Round((10-BathymetryMinZ)/(BathymetryMaxZ-BathymetryMinZ)*BathymetrySize - .5))
	gray := color.GrayModel.Convert(img.At(x, z)).(color.Gray)
	decoded := BathymetryDecode(float64(gray.Y) / 255)
	if decoded < 4 {
		t.Errorf("bathymetry at (-45,10) decodes to %.2fm, want at least 4m for the cliff", decoded)
	}
	assets, err := Generate(seed)
	if err != nil {
		t.Fatal(err)
	}
	doc := mustParse(t, assets["stacks-v2.glb"])
	positions, _, err := doc.ReadAccessor(doc.Meshes[0].Primitives[0].Attributes["POSITION"])
	if err != nil {
		t.Fatal(err)
	}
	var root struct {
		Nodes []struct {
			Scale       []float64 `json:"scale"`
			Translation []float64 `json:"translation"`
		} `json:"nodes"`
	}
	if err := json.Unmarshal(glbJSON(t, assets["stacks-v2.glb"]), &root); err != nil {
		t.Fatal(err)
	}
	if len(root.Nodes) != 1 {
		t.Fatalf("stacks GLB has %d nodes, want 1", len(root.Nodes))
	}
	node := root.Nodes[0]
	scale := node.Scale[0]
	foundLeftTop := false
	for i := 0; i+2 < len(positions); i += 3 {
		x := positions[i]*scale + node.Translation[0]
		y := positions[i+1]*scale + node.Translation[1]
		if x < -28 && y > 15 {
			foundLeftTop = true
			break
		}
	}
	if !foundLeftTop {
		t.Error("stacks-v2.glb has no left cliff-top vertex with x < -28 and y > 15")
	}
}

func TestBouldersAvoidMonolith(t *testing.T) {
	boulders := boulderSpecs(generatorSeed)
	if len(boulders) != 14 {
		t.Fatalf("got %d boulders, want 14", len(boulders))
	}
	for i, boulder := range boulders {
		if distance := math.Hypot(boulder.x+6.2, boulder.z-2.6); distance < 3 {
			t.Errorf("boulder %d is %.3fm from the monolith, want at least 3m", i, distance)
		}
		for j := 0; j < i; j++ {
			other := boulders[j]
			if distance := math.Hypot(boulder.x-other.x, boulder.z-other.z); distance < 1.5 {
				t.Errorf("boulders %d and %d are %.3fm apart, want at least 1.5m", j, i, distance)
			}
		}
	}
}

func TestBathymetryIncludesStackA(t *testing.T) {
	data, err := makeBathymetry(newNoise(generatorSeed))
	if err != nil {
		t.Fatal(err)
	}
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	x := int(math.Round((-16-BathymetryMinX)/(BathymetryMaxX-BathymetryMinX)*BathymetrySize - .5))
	z := int(math.Round((-32-BathymetryMinZ)/(BathymetryMaxZ-BathymetryMinZ)*BathymetrySize - .5))
	gray := color.GrayModel.Convert(img.At(x, z)).(color.Gray)
	if BathymetryDecode(float64(gray.Y)/255) < 0 {
		t.Errorf("bathymetry at stack A center encodes %.2fm, want at or above sea level", BathymetryDecode(float64(gray.Y)/255))
	}
}

func mustParse(t *testing.T, data []byte) *gltfedit.Document {
	t.Helper()
	doc, err := gltfedit.Parse(data, nil)
	if err != nil {
		t.Fatal(err)
	}
	return doc
}

func positionCount(t *testing.T, doc *gltfedit.Document) int {
	t.Helper()
	if len(doc.Meshes) != 1 || len(doc.Meshes[0].Primitives) != 1 {
		t.Fatalf("expected one mesh with one primitive")
	}
	accessor := doc.Meshes[0].Primitives[0].Attributes["POSITION"]
	if accessor < 0 || accessor >= len(doc.Accessors) {
		t.Fatalf("invalid POSITION accessor %d", accessor)
	}
	return doc.Accessors[accessor].Count
}

func checkBounds(t *testing.T, data []byte, doc *gltfedit.Document, low, high [3]float64) ([3]float64, [3]float64) {
	t.Helper()
	primitive := doc.Meshes[0].Primitives[0]
	positions, _, err := doc.ReadAccessor(primitive.Attributes["POSITION"])
	if err != nil {
		t.Fatal(err)
	}
	var raw struct {
		Nodes []struct {
			Translation []float64 `json:"translation"`
			Scale       []float64 `json:"scale"`
		} `json:"nodes"`
	}
	if err := json.Unmarshal(glbJSON(t, data), &raw); err != nil {
		t.Fatal(err)
	}
	if len(raw.Nodes) != 1 {
		t.Fatalf("expected one node, got %d", len(raw.Nodes))
	}
	node := raw.Nodes[0]
	translation := [3]float64{}
	scale := [3]float64{1, 1, 1}
	copy(translation[:], node.Translation)
	if len(node.Scale) == 1 {
		scale = [3]float64{node.Scale[0], node.Scale[0], node.Scale[0]}
	} else if len(node.Scale) >= 3 {
		copy(scale[:], node.Scale)
	}
	actualLow := [3]float64{math.Inf(1), math.Inf(1), math.Inf(1)}
	actualHigh := [3]float64{math.Inf(-1), math.Inf(-1), math.Inf(-1)}
	for i := 0; i+2 < len(positions); i += 3 {
		for axis := 0; axis < 3; axis++ {
			v := positions[i+axis]*scale[axis] + translation[axis]
			actualLow[axis] = math.Min(actualLow[axis], v)
			actualHigh[axis] = math.Max(actualHigh[axis], v)
		}
	}
	for axis := 0; axis < 3; axis++ {
		if actualLow[axis] < low[axis] || actualHigh[axis] > high[axis] {
			t.Errorf("bounds axis %d are [%.3f, %.3f], expected within [%.3f, %.3f]", axis, actualLow[axis], actualHigh[axis], low[axis], high[axis])
		}
	}
	return actualLow, actualHigh
}

func checkNear(t *testing.T, label string, got, want, tolerance float64) {
	t.Helper()
	if math.Abs(got-want) > tolerance {
		t.Errorf("%s = %.4f, want %.4f ± %.4f", label, got, want, tolerance)
	}
}

func checkGLBEncoding(t *testing.T, data []byte, doc *gltfedit.Document, textured bool) {
	t.Helper()
	primitive := doc.Meshes[0].Primitives[0]
	for name, want := range map[string]struct {
		component  int
		normalized bool
	}{"POSITION": {5122, true}, "NORMAL": {5120, true}, "TEXCOORD_0": {5121, true}} {
		accessor := doc.Accessors[primitive.Attributes[name]]
		if accessor.ComponentType != want.component || accessor.Normalized != want.normalized {
			t.Errorf("%s accessor is component %d normalized=%t; want %d normalized=true", name, accessor.ComponentType, accessor.Normalized, want.component)
		}
	}
	if primitive.Indices == nil || doc.Accessors[*primitive.Indices].ComponentType != 5123 {
		t.Error("primitive indices are not UNSIGNED_SHORT")
	}
	var root struct {
		ExtensionsUsed     []string `json:"extensionsUsed"`
		ExtensionsRequired []string `json:"extensionsRequired"`
		BufferViews        []struct {
			ByteOffset int `json:"byteOffset"`
		} `json:"bufferViews"`
		Materials []struct {
			NormalTexture struct {
				Scale      float64 `json:"scale"`
				Extensions map[string]struct {
					Scale []float64 `json:"scale"`
				} `json:"extensions"`
			} `json:"normalTexture"`
		} `json:"materials"`
		Images []struct {
			BufferView int    `json:"bufferView"`
			MIMEType   string `json:"mimeType"`
		} `json:"images"`
	}
	if err := json.Unmarshal(glbJSON(t, data), &root); err != nil {
		t.Fatal(err)
	}
	if !contains(root.ExtensionsUsed, "KHR_mesh_quantization") || !contains(root.ExtensionsRequired, "KHR_mesh_quantization") {
		t.Error("KHR_mesh_quantization must be used and required")
	}
	for index, view := range root.BufferViews {
		if view.ByteOffset%4 != 0 {
			t.Errorf("bufferView %d starts at unaligned offset %d", index, view.ByteOffset)
		}
	}
	if textured {
		if len(root.Images) != 3 || !contains(root.ExtensionsUsed, "KHR_texture_transform") {
			t.Errorf("terrain GLB has %d embedded images or lacks KHR_texture_transform", len(root.Images))
		}
		for _, embedded := range root.Images {
			if len(root.Materials) != 1 {
				t.Fatalf("terrain has %d materials, want 1", len(root.Materials))
			}
			transform := root.Materials[0].NormalTexture.Extensions["KHR_texture_transform"].Scale
			if root.Materials[0].NormalTexture.Scale != .6 || len(transform) != 2 || transform[0] != 60 || transform[1] != 45 {
				t.Errorf("normal map parameters are scale=%g transform=%v, want 0.6 and [60 45]", root.Materials[0].NormalTexture.Scale, transform)
			}
			if (embedded.MIMEType != "image/png" && embedded.MIMEType != "image/jpeg") || embedded.BufferView < 0 || embedded.BufferView >= len(root.BufferViews) {
				t.Errorf("invalid embedded image reference: %+v", embedded)
			}
		}
	}
}

func checkRockMaterial(t *testing.T, data []byte) {
	t.Helper()
	var root struct {
		ExtensionsUsed []string `json:"extensionsUsed"`
		Images         []struct {
			MIMEType string `json:"mimeType"`
		} `json:"images"`
		Materials []struct {
			PBR struct {
				Metallic  float64 `json:"metallicFactor"`
				Roughness float64 `json:"roughnessFactor"`
			} `json:"pbrMetallicRoughness"`
			Normal struct {
				Scale      float64 `json:"scale"`
				Extensions map[string]struct {
					Scale []float64 `json:"scale"`
				} `json:"extensions"`
			} `json:"normalTexture"`
		} `json:"materials"`
	}
	if err := json.Unmarshal(glbJSON(t, data), &root); err != nil {
		t.Fatal(err)
	}
	if len(root.Images) != 1 || (root.Images[0].MIMEType != "image/png" && root.Images[0].MIMEType != "image/jpeg") || len(root.Materials) != 1 {
		t.Fatalf("rock material has %d embedded images and %d materials", len(root.Images), len(root.Materials))
	}
	material := root.Materials[0]
	transform := material.Normal.Extensions["KHR_texture_transform"].Scale
	if !contains(root.ExtensionsUsed, "KHR_texture_transform") || material.PBR.Metallic != 0 || material.PBR.Roughness != .85 || material.Normal.Scale != .9 || len(transform) != 2 || transform[0] != 3 || transform[1] != 3 {
		t.Errorf("unexpected rock material: %+v with normal transform %v", material, transform)
	}
}

func contains(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}

func glbJSON(t *testing.T, data []byte) []byte {
	t.Helper()
	if len(data) < 20 || string(data[:4]) != "glTF" {
		t.Fatal("expected a GLB")
	}
	length := int(binary.LittleEndian.Uint32(data[12:16]))
	if length < 0 || 20+length > len(data) {
		t.Fatal("invalid GLB JSON chunk length")
	}
	return data[20 : 20+length]
}

func TestBathymetryEncodingRoundTripsNearSeaLevel(t *testing.T) {
	for _, h := range []float64{-8, -2, -0.5, -0.05, 0, 0.05, 0.5, 3.9} {
		v := math.Round(BathymetryEncode(h)*255) / 255
		if got := BathymetryDecode(v); math.Abs(got-h) > 0.02+0.02*math.Abs(h) {
			t.Errorf("height %.3f round-trips to %.3f", h, got)
		}
	}
}

func TestAlbedoIsJPEG(t *testing.T) {
	files, err := Generate(generatorSeed)
	if err != nil {
		t.Fatal(err)
	}
	img, err := jpeg.Decode(bytes.NewReader(files["beach-v2-albedo.jpg"]))
	if err != nil {
		t.Fatal(err)
	}
	if b := img.Bounds(); b.Dx() != 1024 || b.Dy() != 1024 {
		t.Fatalf("albedo is %dx%d", b.Dx(), b.Dy())
	}
}
