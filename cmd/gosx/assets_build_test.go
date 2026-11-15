package main

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"m31labs.dev/gosx/assetpipe"
	"m31labs.dev/gosx/assetpipe/manifest"
)

// gridGLB writes an n by n quad grid (2*n*n triangles) as a GLB.
func gridGLB(t *testing.T, path string, n int) {
	t.Helper()
	var positions []float32
	for y := 0; y <= n; y++ {
		for x := 0; x <= n; x++ {
			positions = append(positions, float32(x), float32(y), 0)
		}
	}
	var indices []uint16
	for y := 0; y < n; y++ {
		for x := 0; x < n; x++ {
			a := uint16(y*(n+1) + x)
			b := a + uint16(n+1)
			indices = append(indices, a, b, a+1, a+1, b, b+1)
		}
	}
	var bin bytes.Buffer
	binary.Write(&bin, binary.LittleEndian, positions)
	indexOffset := bin.Len()
	binary.Write(&bin, binary.LittleEndian, indices)
	for bin.Len()%4 != 0 {
		bin.WriteByte(0)
	}
	doc := map[string]any{
		"asset":  map[string]any{"version": "2.0"},
		"scene":  0,
		"scenes": []map[string]any{{"nodes": []int{0}}},
		"nodes":  []map[string]any{{"mesh": 0}},
		"meshes": []map[string]any{{"primitives": []map[string]any{{"attributes": map[string]int{"POSITION": 0}, "indices": 1}}}},
		"accessors": []map[string]any{
			{"bufferView": 0, "componentType": 5126, "count": (n + 1) * (n + 1), "type": "VEC3", "min": []float64{0, 0, 0}, "max": []float64{float64(n), float64(n), 0}},
			{"bufferView": 1, "componentType": 5123, "count": len(indices), "type": "SCALAR"},
		},
		"bufferViews": []map[string]any{
			{"buffer": 0, "byteOffset": 0, "byteLength": indexOffset, "target": 34962},
			{"buffer": 0, "byteOffset": indexOffset, "byteLength": len(indices) * 2, "target": 34963},
		},
		"buffers": []map[string]any{{"byteLength": bin.Len()}},
	}
	chunk, _ := json.Marshal(doc)
	for len(chunk)%4 != 0 {
		chunk = append(chunk, ' ')
	}
	var out bytes.Buffer
	out.WriteString("glTF")
	binary.Write(&out, binary.LittleEndian, uint32(2))
	binary.Write(&out, binary.LittleEndian, uint32(12+8+len(chunk)+8+bin.Len()))
	binary.Write(&out, binary.LittleEndian, uint32(len(chunk)))
	binary.Write(&out, binary.LittleEndian, uint32(0x4E4F534A))
	out.Write(chunk)
	binary.Write(&out, binary.LittleEndian, uint32(bin.Len()))
	binary.Write(&out, binary.LittleEndian, uint32(0x004E4942))
	out.Write(bin.Bytes())
	if err := os.WriteFile(path, out.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
}

func readBuiltManifest(t *testing.T, dir string) manifest.Manifest {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, assetpipe.ManifestFileName))
	if err != nil {
		t.Fatal(err)
	}
	m, err := manifest.Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func TestRunAssetsBuildWritesGLBLODsAndManifest(t *testing.T) {
	src, out := t.TempDir(), t.TempDir()
	gridGLB(t, filepath.Join(src, "terrain.glb"), 20)

	var stdout bytes.Buffer
	err := runAssetsCommand([]string{"build", "--out", out, "--lods", "0.5,0.25", src}, &stdout)
	if err != nil {
		t.Fatalf("%v\n%s", err, stdout.String())
	}
	m := readBuiltManifest(t, out)
	model, ok := m.Model("terrain")
	if !ok || model.Stats.Triangles != 800 || len(model.LODs) != 3 {
		t.Fatalf("model = %+v", model)
	}
	for _, snippet := range []string{"terrain -> terrain.glb", "800 triangles", "lod1: terrain.lod1.glb", "Manifest: "} {
		if !strings.Contains(stdout.String(), snippet) {
			t.Fatalf("output lacks %q:\n%s", snippet, stdout.String())
		}
	}
}

func TestRunAssetsBuildFailsOverBudgetAndStillWritesManifest(t *testing.T) {
	src, out := t.TempDir(), t.TempDir()
	gridGLB(t, filepath.Join(src, "terrain.glb"), 20)

	var stdout bytes.Buffer
	err := runAssetsCommand([]string{"build", "--out", out, "--no-lod", "--max-triangles", "500", "--max-draw-calls", "4", src}, &stdout)
	if err == nil || !strings.Contains(err.Error(), "1 budget violation") {
		t.Fatalf("err = %v\n%s", err, stdout.String())
	}
	if !strings.Contains(stdout.String(), "over budget: terrain: triangles is 800, budget is 500") {
		t.Fatalf("output = %s", stdout.String())
	}
	m := readBuiltManifest(t, out)
	if len(m.Violations) != 1 || m.Violations[0].Budget != "triangles" {
		t.Fatalf("violations = %+v", m.Violations)
	}
}

func TestRunAssetsBuildReadsSpecFile(t *testing.T) {
	src, out := t.TempDir(), t.TempDir()
	gridGLB(t, filepath.Join(src, "a.glb"), 20)
	gridGLB(t, filepath.Join(src, "b.glb"), 20)
	spec := `{"models":[
	  {"src":"a.glb","name":"alpha","budget":{"maxTriangles":10}},
	  {"src":"b.glb","name":"beta"}],
	 "budget":{"maxTriangles":100000},"lodRatios":[0.5]}`
	specPath := filepath.Join(src, "assets.json")
	if err := os.WriteFile(specPath, []byte(spec), 0o644); err != nil {
		t.Fatal(err)
	}
	var stdout bytes.Buffer
	err := runAssetsCommand([]string{"build", "--out", out, specPath}, &stdout)
	if err == nil || !strings.Contains(err.Error(), "1 budget violation") {
		t.Fatalf("err = %v", err)
	}
	m := readBuiltManifest(t, out)
	if _, ok := m.Model("alpha"); !ok {
		t.Fatalf("models = %+v", m.Models)
	}
	beta, _ := m.Model("beta")
	if len(beta.LODs) != 2 {
		t.Fatalf("spec lodRatios were not applied: %+v", beta.LODs)
	}
	if m.Violations[0].Model != "alpha" {
		t.Fatalf("per-model budget did not apply: %+v", m.Violations)
	}
}

func TestRunAssetsBuildRejectsBadLODRatio(t *testing.T) {
	err := runAssetsCommand([]string{"build", "--lods", "1.5", t.TempDir()}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "--lods") {
		t.Fatalf("err = %v", err)
	}
}

func TestRunAssetsBuildMeshoptShrinksTheGLB(t *testing.T) {
	src, plain, packed := t.TempDir(), t.TempDir(), t.TempDir()
	gridGLB(t, filepath.Join(src, "terrain.glb"), 40)
	var out bytes.Buffer
	if err := runAssetsCommand([]string{"build", "--out", plain, "--no-lod", src}, &out); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	if err := runAssetsCommand([]string{"build", "--out", packed, "--no-lod", "--meshopt", src}, &out); err != nil {
		t.Fatalf("%v\n%s", err, out.String())
	}
	if !strings.Contains(out.String(), "meshopt: ") {
		t.Fatalf("summary lacks the meshopt line:\n%s", out.String())
	}
	before, after := readBuiltManifest(t, plain), readBuiltManifest(t, packed)
	a, _ := before.Model("terrain")
	b, _ := after.Model("terrain")
	if b.Meshopt == nil || b.Bytes >= a.Bytes || b.Stats.Triangles != a.Stats.Triangles {
		t.Fatalf("plain %+v, packed %+v meshopt=%+v", a.Stats, b.Stats, b.Meshopt)
	}
}
