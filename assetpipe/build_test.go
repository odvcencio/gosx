package assetpipe

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"flag"
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"m31labs.dev/gosx/assetpipe/manifest"
)

var updateBuildGolden = flag.Bool("update-build-golden", false, "rewrite assetpipe/testdata/build_sphere.golden.json")

// sphereFixture is a generated Blender-style export: one indexed UV sphere
// with an embedded base-color PNG, a skin of the requested joint count, and a
// 4x scaled root node so bounds go through a node transform.
type sphereFixture struct {
	rings, segments int
	joints          int
	external        bool // write a .gltf with a separate .bin and .png
}

func (f sphereFixture) triangles() int { return f.rings * f.segments * 2 }

func testPNG(t *testing.T, size int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, size, size))
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			img.Set(x, y, color.RGBA{R: uint8(x * 255 / size), G: uint8(y * 255 / size), B: 128, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// writeSphere writes the fixture to dir/name and returns the file path.
func writeSphere(t *testing.T, dir, name string, f sphereFixture) string {
	t.Helper()
	var positions, normals, uvs []float32
	for r := 0; r <= f.rings; r++ {
		phi := math.Pi * float64(r) / float64(f.rings)
		for s := 0; s <= f.segments; s++ {
			theta := 2 * math.Pi * float64(s) / float64(f.segments)
			x := math.Sin(phi) * math.Cos(theta)
			y := math.Cos(phi)
			z := math.Sin(phi) * math.Sin(theta)
			positions = append(positions, float32(x), float32(y), float32(z))
			normals = append(normals, float32(x), float32(y), float32(z))
			uvs = append(uvs, float32(s)/float32(f.segments), float32(r)/float32(f.rings))
		}
	}
	var indices []uint16
	stride := f.segments + 1
	for r := 0; r < f.rings; r++ {
		for s := 0; s < f.segments; s++ {
			a := uint16(r*stride + s)
			b := uint16((r+1)*stride + s)
			indices = append(indices, a, b, a+1, a+1, b, b+1)
		}
	}
	var bin bytes.Buffer
	view := func(data any, target int) map[string]any {
		for bin.Len()%4 != 0 {
			bin.WriteByte(0)
		}
		offset := bin.Len()
		if err := binary.Write(&bin, binary.LittleEndian, data); err != nil {
			t.Fatal(err)
		}
		return map[string]any{"buffer": 0, "byteOffset": offset, "byteLength": bin.Len() - offset, "target": target}
	}
	views := []map[string]any{view(positions, 34962), view(normals, 34962), view(uvs, 34962), view(indices, 34963)}
	pngBytes := testPNG(t, 64)
	images := []map[string]any{{"name": "albedo"}}
	if f.external {
		if err := os.WriteFile(filepath.Join(dir, "albedo.png"), pngBytes, 0o644); err != nil {
			t.Fatal(err)
		}
		images[0]["uri"] = "albedo.png"
	} else {
		for bin.Len()%4 != 0 {
			bin.WriteByte(0)
		}
		offset := bin.Len()
		bin.Write(pngBytes)
		views = append(views, map[string]any{"buffer": 0, "byteOffset": offset, "byteLength": len(pngBytes)})
		images[0]["bufferView"] = len(views) - 1
		images[0]["mimeType"] = "image/png"
	}
	nodes := []map[string]any{{"mesh": 0, "scale": []float64{4, 4, 4}, "name": "sphere"}}
	roots := []int{0}
	if f.joints > 0 {
		var joints []int
		for i := 0; i < f.joints; i++ {
			nodes = append(nodes, map[string]any{"name": "bone"})
			joints = append(joints, len(nodes)-1)
			roots = append(roots, len(nodes)-1)
		}
		nodes[0]["skin"] = 0
	}
	vertexCount := (f.rings + 1) * (f.segments + 1)
	doc := map[string]any{
		"asset":     map[string]any{"version": "2.0", "generator": "assetpipe-test"},
		"scene":     0,
		"scenes":    []map[string]any{{"nodes": roots}},
		"nodes":     nodes,
		"meshes":    []map[string]any{{"primitives": []map[string]any{{"attributes": map[string]int{"POSITION": 0, "NORMAL": 1, "TEXCOORD_0": 2}, "indices": 3, "material": 0}}}},
		"materials": []map[string]any{{"pbrMetallicRoughness": map[string]any{"baseColorTexture": map[string]int{"index": 0}}}},
		"textures":  []map[string]any{{"source": 0}},
		"images":    images,
		"accessors": []map[string]any{
			{"bufferView": 0, "componentType": 5126, "count": vertexCount, "type": "VEC3", "min": []float64{-1, -1, -1}, "max": []float64{1, 1, 1}},
			{"bufferView": 1, "componentType": 5126, "count": vertexCount, "type": "VEC3"},
			{"bufferView": 2, "componentType": 5126, "count": vertexCount, "type": "VEC2"},
			{"bufferView": 3, "componentType": 5123, "count": len(indices), "type": "SCALAR"},
		},
		"bufferViews": views,
	}
	if f.joints > 0 {
		var joints []int
		for i := 1; i <= f.joints; i++ {
			joints = append(joints, i)
		}
		doc["skins"] = []map[string]any{{"joints": joints}}
	}
	if f.external {
		if err := os.WriteFile(filepath.Join(dir, "sphere.bin"), bin.Bytes(), 0o644); err != nil {
			t.Fatal(err)
		}
		doc["buffers"] = []map[string]any{{"uri": "sphere.bin", "byteLength": bin.Len()}}
		data, _ := json.Marshal(doc)
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, data, 0o644); err != nil {
			t.Fatal(err)
		}
		return path
	}
	doc["buffers"] = []map[string]any{{"byteLength": bin.Len()}}
	chunk, _ := json.Marshal(doc)
	for len(chunk)%4 != 0 {
		chunk = append(chunk, ' ')
	}
	payload := bin.Bytes()
	for len(payload)%4 != 0 {
		payload = append(payload, 0)
	}
	var out bytes.Buffer
	out.WriteString("glTF")
	binary.Write(&out, binary.LittleEndian, uint32(2))
	binary.Write(&out, binary.LittleEndian, uint32(12+8+len(chunk)+8+len(payload)))
	binary.Write(&out, binary.LittleEndian, uint32(len(chunk)))
	binary.Write(&out, binary.LittleEndian, uint32(0x4E4F534A))
	out.Write(chunk)
	binary.Write(&out, binary.LittleEndian, uint32(len(payload)))
	binary.Write(&out, binary.LittleEndian, uint32(0x004E4942))
	out.Write(payload)
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, out.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestBuildModelsWritesGLBLODChainTexturesAndManifest(t *testing.T) {
	src, out := t.TempDir(), t.TempDir()
	fixture := sphereFixture{rings: 24, segments: 32}
	writeSphere(t, src, "hero.glb", fixture)

	got, err := BuildModels(BuildOptions{Inputs: []string{src}, OutputDir: out, LODRatios: []float64{0.5, 0.2}})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Models) != 1 {
		t.Fatalf("models = %d, want 1", len(got.Models))
	}
	model := got.Models[0]
	if model.Name != "hero" || model.URI != "hero.glb" {
		t.Fatalf("model = %+v", model)
	}
	if model.Stats.Triangles != fixture.triangles() || model.Stats.DrawCalls != 1 {
		t.Fatalf("stats = %+v, want %d triangles and 1 draw call", model.Stats, fixture.triangles())
	}
	// The sphere has radius 1 under a 4x node scale: the bounding sphere of its
	// box is half the diagonal of an 8-unit cube, 4*sqrt(3).
	if want := 4 * math.Sqrt(3); math.Abs(model.Radius-want) > 0.01 {
		t.Fatalf("radius = %v, want %v", model.Radius, want)
	}
	if want := mippedRGBA8Bytes(64, 64); model.Stats.TextureBytes != want {
		t.Fatalf("textureBytes = %d, want %d", model.Stats.TextureBytes, want)
	}
	if len(model.LODs) != 3 {
		t.Fatalf("lods = %+v, want the source plus two reductions", model.LODs)
	}
	for i := 1; i < len(model.LODs); i++ {
		prev, cur := model.LODs[i-1], model.LODs[i]
		if cur.Triangles >= prev.Triangles {
			t.Fatalf("lod %d has %d triangles, not fewer than %d", i, cur.Triangles, prev.Triangles)
		}
		if cur.Distance <= prev.Distance {
			t.Fatalf("lod %d distance %v does not exceed %v", i, cur.Distance, prev.Distance)
		}
	}
	if model.LODs[0].Distance != 0 {
		t.Fatalf("lod 0 distance = %v, want 0", model.LODs[0].Distance)
	}
	for _, lod := range model.LODs {
		info, err := os.Stat(filepath.Join(out, lod.URI))
		if err != nil || info.Size() != lod.Bytes {
			t.Fatalf("lod file %s: %v size %v want %d", lod.URI, err, info, lod.Bytes)
		}
	}
	if len(model.Textures) != 1 || len(model.Textures[0].Variants) == 0 || model.Textures[0].Role != "base-color" {
		t.Fatalf("textures = %+v", model.Textures)
	}
	for _, v := range model.Textures[0].Variants {
		if !strings.HasSuffix(v.URI, ".ktx2") {
			t.Fatalf("variant %s is not KTX2", v.URI)
		}
		if _, err := os.Stat(filepath.Join(out, v.URI)); err != nil {
			t.Fatal(err)
		}
	}
	data, err := os.ReadFile(filepath.Join(out, ManifestFileName))
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := manifest.Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := parsed.Model("hero"); !ok {
		t.Fatal("manifest does not list hero")
	}
}

// goldenView drops byte counts, which move with encoder tuning, and keeps
// everything that is a contract: stats, LOD structure, texture variants.
type goldenView struct {
	Name     string
	Radius   float64
	Stats    manifest.Stats
	LODs     []goldenLOD
	Textures []goldenTexture
}

type goldenLOD struct {
	Level     int
	URI       string
	Triangles int
	Ratio     float64
	Distance  float64
}

type goldenTexture struct {
	Image, Role string
	Width       int
	Variants    []string
}

func TestBuildModelsManifestGolden(t *testing.T) {
	src, out := t.TempDir(), t.TempDir()
	writeSphere(t, src, "hero.glb", sphereFixture{rings: 24, segments: 32, joints: 5})
	got, err := BuildModels(BuildOptions{Inputs: []string{src}, OutputDir: out, LODRatios: []float64{0.5, 0.2}})
	if err != nil {
		t.Fatal(err)
	}
	var view []goldenView
	for _, m := range got.Models {
		g := goldenView{Name: m.Name, Radius: m.Radius, Stats: m.Stats}
		g.Stats.TextureBytes = m.Stats.TextureBytes
		for _, l := range m.LODs {
			g.LODs = append(g.LODs, goldenLOD{l.Level, l.URI, l.Triangles, l.Ratio, l.Distance})
		}
		for _, tex := range m.Textures {
			gt := goldenTexture{Image: tex.Image, Role: tex.Role, Width: tex.Width}
			for _, v := range tex.Variants {
				gt.Variants = append(gt.Variants, v.URI)
			}
			g.Textures = append(g.Textures, gt)
		}
		view = append(view, g)
	}
	data, err := json.MarshalIndent(view, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	data = append(data, '\n')
	golden := filepath.Join("testdata", "build_sphere.golden.json")
	if *updateBuildGolden {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(golden, data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatalf("%v (run go test ./assetpipe -run Golden -update-build-golden)", err)
	}
	if !bytes.Equal(data, want) {
		t.Fatalf("manifest view differs from %s:\n%s", golden, data)
	}
}

func TestBuildModelsEnforcesBudgets(t *testing.T) {
	src, out := t.TempDir(), t.TempDir()
	fixture := sphereFixture{rings: 24, segments: 32, joints: 130}
	writeSphere(t, src, "rig.glb", fixture)
	budget := Budget{MaxTriangles: 100, MaxTextureBytes: 1000, MaxJoints: 128, MaxDrawCalls: 0}
	got, err := BuildModels(BuildOptions{Inputs: []string{src}, OutputDir: out, NoLOD: true, NoTextures: true, Budget: budget})
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]manifest.Violation{}
	for _, v := range got.Violations {
		names[v.Budget] = v
	}
	if len(got.Violations) != 3 {
		t.Fatalf("violations = %v, want triangles, textureBytes, joints", got.Violations)
	}
	if v := names["joints"]; v.Actual != 130 || v.Limit != 128 || v.Model != "rig" {
		t.Fatalf("joints violation = %+v", v)
	}
	if v := names["triangles"]; v.Actual != int64(fixture.triangles()) {
		t.Fatalf("triangles violation = %+v", v)
	}
	if _, err := os.Stat(filepath.Join(out, "rig.glb")); err != nil {
		t.Fatalf("a violating model still writes its GLB: %v", err)
	}
	// A model inside every budget reports none.
	ok, err := BuildModels(BuildOptions{Inputs: []string{src}, OutputDir: t.TempDir(), NoLOD: true, NoTextures: true,
		Budget: Budget{MaxTriangles: 1 << 20, MaxTextureBytes: 1 << 30, MaxJoints: 256, MaxDrawCalls: 4}})
	if err != nil || len(ok.Violations) != 0 {
		t.Fatalf("violations = %v, err = %v", ok.Violations, err)
	}
}

func TestBuildModelsConvertsGLTFWithExternalFiles(t *testing.T) {
	src, out := t.TempDir(), t.TempDir()
	writeSphere(t, src, "prop.gltf", sphereFixture{rings: 12, segments: 16, external: true})
	got, err := BuildModels(BuildOptions{Inputs: []string{src}, OutputDir: out, NoLOD: true, NoTextures: true})
	if err != nil {
		t.Fatal(err)
	}
	model := got.Models[0]
	data, err := os.ReadFile(filepath.Join(out, model.URI))
	if err != nil || string(data[:4]) != "glTF" {
		t.Fatalf("output is not a GLB: %v", err)
	}
	if _, err := os.Stat(filepath.Join(out, "albedo.png")); err != nil {
		t.Fatalf("external image was not copied beside the GLB: %v", err)
	}
	if want := mippedRGBA8Bytes(64, 64); model.Stats.TextureBytes != want {
		t.Fatalf("textureBytes = %d, want %d", model.Stats.TextureBytes, want)
	}
}

func TestBuildModelsRejectsDuplicateNames(t *testing.T) {
	src := t.TempDir()
	writeSphere(t, src, "a.glb", sphereFixture{rings: 6, segments: 8})
	_, err := BuildModels(BuildOptions{
		Inputs:    []string{filepath.Join(src, "a.glb")},
		Models:    []BuildInput{{Path: filepath.Join(src, "a.glb")}},
		OutputDir: t.TempDir(), NoLOD: true, NoTextures: true,
	})
	if err == nil || !strings.Contains(err.Error(), "share the name") {
		t.Fatalf("err = %v", err)
	}
}

var updateKTX2Fixture = flag.Bool("update-ktx2-fixture", false, "rewrite client/js/testdata/blender-ktx2/ for the loader end-to-end test")

func TestBuildModelsReferencesKTX2FromEveryGLB(t *testing.T) {
	src, out := t.TempDir(), t.TempDir()
	if err := os.MkdirAll(filepath.Join(src, "props"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeSphere(t, src, "props/crate.glb", sphereFixture{rings: 24, segments: 32})
	got, err := BuildModels(BuildOptions{Inputs: []string{src}, OutputDir: out, LODRatios: []float64{0.5}})
	if err != nil {
		t.Fatal(err)
	}
	model := got.Models[0]
	if len(model.LODs) != 2 || len(model.Textures) != 1 {
		t.Fatalf("model = %+v", model)
	}
	for _, lod := range model.LODs {
		data, err := os.ReadFile(filepath.Join(out, lod.URI))
		if err != nil {
			t.Fatal(err)
		}
		if int64(len(data)) != lod.Bytes {
			t.Fatalf("%s: manifest says %d bytes, file has %d", lod.URI, lod.Bytes, len(data))
		}
		jsonChunk, _, err := splitGLBChunks(data)
		if err != nil {
			t.Fatal(err)
		}
		var doc struct {
			ExtensionsUsed []string `json:"extensionsUsed"`
			Images         []struct {
				BufferView *int `json:"bufferView"`
				Extensions map[string]struct {
					Variants []Variant `json:"variants"`
				} `json:"extensions"`
			} `json:"images"`
		}
		if err := json.Unmarshal(jsonChunk, &doc); err != nil {
			t.Fatal(err)
		}
		if len(doc.Images) != 1 || doc.Images[0].BufferView == nil {
			t.Fatalf("%s must keep the original embedded image as the fallback: %+v", lod.URI, doc.Images)
		}
		ext := doc.Images[0].Extensions[ImageVariantsExtension]
		if len(ext.Variants) == 0 || !strings.Contains(strings.Join(doc.ExtensionsUsed, ","), ImageVariantsExtension) {
			t.Fatalf("%s lacks %s: %+v", lod.URI, ImageVariantsExtension, doc)
		}
		sawBC := false
		for _, v := range ext.Variants {
			// URIs are relative to the GLB, which sits in props/.
			if strings.HasPrefix(v.URI, "props/") || !strings.HasPrefix(v.URI, "crate.textures/") {
				t.Fatalf("variant URI %q is not relative to the GLB", v.URI)
			}
			if _, err := os.Stat(filepath.Join(out, "props", v.URI)); err != nil {
				t.Fatalf("variant file missing: %v", err)
			}
			sawBC = sawBC || strings.HasPrefix(v.Format, "bc")
		}
		if !sawBC {
			t.Fatalf("no BC variant in %+v", ext.Variants)
		}
		if _, err := analyzeGLB(data, nil); err != nil {
			t.Fatalf("%s no longer analyzes: %v", lod.URI, err)
		}
	}

	if *updateKTX2Fixture {
		// The loader test in client/js reads this build. Copy only the files it needs.
		dest := filepath.Join("..", "client", "js", "testdata", "blender-ktx2")
		_ = os.RemoveAll(dest)
		for _, rel := range []string{"props/crate.glb", "props/crate.lod1.glb"} {
			data, err := os.ReadFile(filepath.Join(out, rel))
			if err != nil {
				t.Fatal(err)
			}
			if err := os.MkdirAll(filepath.Join(dest, filepath.Dir(rel)), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dest, rel), data, 0o644); err != nil {
				t.Fatal(err)
			}
		}
		textures, _ := filepath.Glob(filepath.Join(out, "props", "crate.textures", "*.ktx2"))
		for _, p := range textures {
			data, _ := os.ReadFile(p)
			d := filepath.Join(dest, "props", "crate.textures", filepath.Base(p))
			_ = os.MkdirAll(filepath.Dir(d), 0o755)
			if err := os.WriteFile(d, data, 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
}
