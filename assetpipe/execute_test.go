package assetpipe

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"m31labs.dev/gosx/assetpipe/gltfedit"
	"m31labs.dev/gosx/assetpipe/ibl"
	"m31labs.dev/gosx/render/bundle/ktx2"
)

// writeTestHDR builds a small Radiance file with a constant colour, which
// makes every IBL product predictable.
func writeTestHDR(t *testing.T, width, height int, r, g, b float64) []byte {
	t.Helper()
	var buf bytes.Buffer
	buf.WriteString("#?RADIANCE\nFORMAT=32-bit_rle_rgbe\n\n")
	fmt.Fprintf(&buf, "-Y %d +X %d\n", height, width)
	peak := math.Max(r, math.Max(g, b))
	mantissa, exponent := math.Frexp(peak)
	scale := mantissa * 256 / peak
	quad := [4]byte{byte(r * scale), byte(g * scale), byte(b * scale), byte(exponent + 128)}
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			buf.Write(quad[:])
		}
	}
	return buf.Bytes()
}

func TestExecuteBuildsIBLProducts(t *testing.T) {
	dir := t.TempDir()
	mustWriteBytes(t, filepath.Join(dir, "public", "env", "studio.hdr"), writeTestHDR(t, 32, 16, 2, 1, 0.5))

	report, err := Plan([]string{dir}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	before := findAsset(t, report, "public/env/studio.hdr")
	for _, action := range before.Actions {
		if action.Status != StatusCandidate {
			t.Fatalf("plan action %q has status %q, want candidate", action.Name, action.Status)
		}
	}
	for _, variant := range before.Variants {
		if variant.Exists() {
			t.Fatalf("plan variant %q claims to exist", variant.URI)
		}
	}

	executed, execReport, err := Execute(report, ExecuteOptions{
		Root: dir,
		IBL: IBLOptions{
			CubeSize:       16,
			Samples:        16,
			IrradianceSize: 8,
			BRDFLUTSize:    16,
			BRDFSamples:    64,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if execReport.Totals.Executed != 2 || execReport.Totals.Failed != 0 {
		t.Fatalf("unexpected totals: %+v", execReport.Totals)
	}

	after := findAsset(t, executed, "public/env/studio.hdr")
	for _, action := range after.Actions {
		if action.Status != StatusExecuted {
			t.Fatalf("action %q has status %q after execution", action.Name, action.Status)
		}
	}
	if len(after.Variants) != 4 {
		t.Fatalf("expected four built variants, got %d", len(after.Variants))
	}
	for _, variant := range after.Variants {
		if !variant.Exists() {
			t.Fatalf("variant %q is not marked built", variant.URI)
		}
		info, err := os.Stat(filepath.Join(dir, filepath.FromSlash(variant.URI)))
		if err != nil {
			t.Fatalf("variant %q: %v", variant.URI, err)
		}
		if info.Size() != variant.Bytes {
			t.Fatalf("variant %q reports %d bytes, file holds %d", variant.URI, variant.Bytes, info.Size())
		}
	}

	// The prefiltered cubemap must keep the constant environment at every
	// roughness, including level 0 which copies the source.
	specular, err := os.ReadFile(filepath.Join(dir, "public", "env", "studio.ibl.ktx2"))
	if err != nil {
		t.Fatal(err)
	}
	chain, err := ibl.DecodeCubeKTX2(specular)
	if err != nil {
		t.Fatal(err)
	}
	if len(chain) != 5 {
		t.Fatalf("specular chain holds %d levels, want 5", len(chain))
	}
	for level, mip := range chain {
		colour := mip.Get(0, 0, 0)
		if math.Abs(colour.X-2) > 0.01 || math.Abs(colour.Y-1) > 0.01 || math.Abs(colour.Z-0.5) > 0.01 {
			t.Fatalf("level %d reads %+v, want (2, 1, 0.5)", level, colour)
		}
	}

	// The split-sum table must land as an RG16F container.
	lut, err := os.ReadFile(filepath.Join(dir, "public", "env", "studio.brdf-lut.ktx2"))
	if err != nil {
		t.Fatal(err)
	}
	lutImage, err := ktx2.Parse(lut)
	if err != nil {
		t.Fatal(err)
	}
	if lutImage.Format != ktx2.VkFormatR16G16Sfloat || lutImage.Width != 16 {
		t.Fatalf("unexpected lookup table: %+v", lutImage)
	}

	// The sidecar must name the exact BRDF convention the table follows.
	sidecarBytes, err := os.ReadFile(filepath.Join(dir, "public", "env", "studio.ibl.json"))
	if err != nil {
		t.Fatal(err)
	}
	var sidecar iblSidecar
	if err := json.Unmarshal(sidecarBytes, &sidecar); err != nil {
		t.Fatal(err)
	}
	if sidecar.BRDFModel != ibl.BRDFModel {
		t.Fatalf("sidecar model %q, want %q", sidecar.BRDFModel, ibl.BRDFModel)
	}
	if len(sidecar.RoughnessPerLevel) != 5 || sidecar.RoughnessPerLevel[4] != 1 {
		t.Fatalf("unexpected roughness table: %+v", sidecar.RoughnessPerLevel)
	}
	// A constant environment carries only the band 0 term. Its coefficient is
	// radiance * Y00 * 4*pi, and every higher band cancels.
	wantBand0 := 2 * 0.282095 * 4 * math.Pi
	if math.Abs(sidecar.SphericalHarmonics[0][0]-wantBand0) > 1e-3 {
		t.Fatalf("band 0 red coefficient %.5f, want %.5f", sidecar.SphericalHarmonics[0][0], wantBand0)
	}
	for band := 1; band < 9; band++ {
		if math.Abs(sidecar.SphericalHarmonics[band][0]) > 1e-3 {
			t.Fatalf("band %d should cancel, got %.5f", band, sidecar.SphericalHarmonics[band][0])
		}
	}
	irradiance, err := os.ReadFile(filepath.Join(dir, "public", "env", "studio.irradiance.ktx2"))
	if err != nil {
		t.Fatal(err)
	}
	diffuse, err := ibl.DecodeCubeKTX2(irradiance)
	if err != nil {
		t.Fatal(err)
	}
	colour := diffuse[0].Get(2, 3, 3)
	if math.Abs(colour.X-2) > 0.01 || math.Abs(colour.Y-1) > 0.01 || math.Abs(colour.Z-0.5) > 0.01 {
		t.Fatalf("irradiance reads %+v, want (2, 1, 0.5)", colour)
	}
}

func TestPlanWritesNoFiles(t *testing.T) {
	dir := t.TempDir()
	mustWriteBytes(t, filepath.Join(dir, "env", "studio.hdr"), writeTestHDR(t, 16, 8, 1, 1, 1))
	mustWriteBytes(t, filepath.Join(dir, "models", "grid.glb"), buildGridGLB(t, 8))

	before := listFiles(t, dir)
	if _, err := Plan([]string{dir}, Options{}); err != nil {
		t.Fatal(err)
	}
	after := listFiles(t, dir)
	if len(before) != len(after) {
		t.Fatalf("planning changed the tree: %v -> %v", before, after)
	}
}

func TestExecuteBuildsLODStack(t *testing.T) {
	dir := t.TempDir()
	mustWriteBytes(t, filepath.Join(dir, "models", "grid.glb"), buildGridGLB(t, 16))

	report, err := Plan([]string{dir}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	executed, execReport, err := Execute(report, ExecuteOptions{
		Root: dir,
		Only: []string{"build-lod-stack"},
		LOD:  LODOptions{Ratios: []float64{0.5, 0.25}, MeasureError: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	if execReport.Totals.Executed != 1 {
		t.Fatalf("unexpected totals: %+v (results %+v)", execReport.Totals, execReport.Results)
	}

	asset := findAsset(t, executed, "models/grid.glb")
	lodStatus := ""
	meshoptStatus := ""
	for _, action := range asset.Actions {
		switch action.Name {
		case "build-lod-stack":
			lodStatus = action.Status
		case "meshopt-compress":
			meshoptStatus = action.Status
		}
	}
	if lodStatus != StatusExecuted {
		t.Fatalf("build-lod-stack status %q", lodStatus)
	}
	if meshoptStatus != StatusCandidate {
		t.Fatalf("meshopt-compress must stay a candidate, got %q", meshoptStatus)
	}
	// Variants of actions that did not run must stay plans.
	for _, variant := range asset.Variants {
		if variant.SourceAction == "build-lod-stack" && !variant.Exists() {
			t.Fatalf("LOD variant %q is not marked built", variant.URI)
		}
		if variant.SourceAction != "build-lod-stack" && variant.Exists() {
			t.Fatalf("variant %q claims to exist without an executor", variant.URI)
		}
	}

	sourceTriangles := 16 * 16 * 2
	previous := sourceTriangles
	for level, name := range []string{"models/grid.lod0.glb", "models/grid.lod1.glb"} {
		data, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(name)))
		if err != nil {
			t.Fatal(err)
		}
		document, err := gltfedit.Parse(data, nil)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		indices, err := document.ReadIndices(*document.Meshes[0].Primitives[0].Indices)
		if err != nil {
			t.Fatal(err)
		}
		triangles := len(indices) / 3
		if triangles >= previous {
			t.Fatalf("level %d kept %d triangles, level above kept %d", level, triangles, previous)
		}
		previous = triangles
		positions, components, err := document.ReadAccessor(document.Meshes[0].Primitives[0].Attributes["POSITION"])
		if err != nil {
			t.Fatal(err)
		}
		if components != 3 {
			t.Fatalf("POSITION has %d components", components)
		}
		for i := 1; i < len(positions); i += 3 {
			if math.Abs(positions[i]) > 1e-5 {
				t.Fatalf("%s moved a vertex off the plane: %v", name, positions[i])
			}
		}
	}

	var sidecar lodSidecar
	sidecarBytes, err := os.ReadFile(filepath.Join(dir, "models", "grid.lod.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(sidecarBytes, &sidecar); err != nil {
		t.Fatal(err)
	}
	if len(sidecar.Levels) != 2 || sidecar.SourceTriangles != sourceTriangles {
		t.Fatalf("unexpected sidecar: %+v", sidecar)
	}
	if sidecar.Levels[0].MaxErrorFrac > 1e-6 {
		t.Fatalf("a flat grid must simplify without error, got %g", sidecar.Levels[0].MaxErrorFrac)
	}
}

func TestExecuteSkipsCompressedPrimitive(t *testing.T) {
	dir := t.TempDir()
	glb := buildTestGLB(t, map[string]any{
		"asset":              map[string]any{"version": "2.0"},
		"extensionsUsed":     []string{"KHR_draco_mesh_compression"},
		"extensionsRequired": []string{"KHR_draco_mesh_compression"},
		"meshes": []map[string]any{{
			"primitives": []map[string]any{{
				"attributes": map[string]any{"POSITION": 0},
				"extensions": map[string]any{"KHR_draco_mesh_compression": map[string]any{"bufferView": 0}},
			}},
		}},
		"accessors":   []map[string]any{{"componentType": 5126, "count": 3, "type": "VEC3", "bufferView": 0}},
		"bufferViews": []map[string]any{{"buffer": 0, "byteOffset": 0, "byteLength": 36}},
		"buffers": []map[string]any{{
			"byteLength": 36,
			"uri":        "data:application/octet-stream;base64," + base64.StdEncoding.EncodeToString(make([]byte, 36)),
		}},
	})
	mustWriteBytes(t, filepath.Join(dir, "models", "draco.glb"), glb)

	report, err := Plan([]string{dir}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	executed, execReport, err := Execute(report, ExecuteOptions{Root: dir, Only: []string{"build-lod-stack"}})
	if err != nil {
		t.Fatal(err)
	}
	if execReport.Totals.Executed != 0 || execReport.Totals.Skipped != 1 {
		t.Fatalf("unexpected totals: %+v", execReport.Totals)
	}
	asset := findAsset(t, executed, "models/draco.glb")
	for _, action := range asset.Actions {
		if action.Name == "build-lod-stack" && action.Status != StatusCandidate {
			t.Fatalf("a skipped action must keep its plan status, got %q", action.Status)
		}
	}
	for _, variant := range asset.Variants {
		if variant.Exists() {
			t.Fatalf("skipped action left a claimed variant %q", variant.URI)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "models", "draco.lod0.glb")); !os.IsNotExist(err) {
		t.Fatal("a skipped action must not write a file")
	}
}

func TestExecuteRespectsReadBound(t *testing.T) {
	dir := t.TempDir()
	mustWriteBytes(t, filepath.Join(dir, "env", "big.hdr"), writeTestHDR(t, 64, 32, 1, 1, 1))
	report, err := Plan([]string{dir}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	_, execReport, err := Execute(report, ExecuteOptions{Root: dir, MaxExecuteBytes: 16})
	if err != nil {
		t.Fatal(err)
	}
	if execReport.Totals.Failed == 0 {
		t.Fatalf("expected the read bound to stop the stage: %+v", execReport.Results)
	}
}

func TestSupportedActions(t *testing.T) {
	got := SupportedActions()
	// The list must stay sorted, and it must name every action a stage
	// registered. The test checks membership rather than an exact list, so one
	// new stage does not break the assertion of another.
	if !sort.StringsAreSorted(got) {
		t.Fatalf("supported actions must be sorted: %v", got)
	}
	present := map[string]bool{}
	for _, name := range got {
		if present[name] {
			t.Fatalf("supported actions repeat %q: %v", name, got)
		}
		present[name] = true
	}
	for _, name := range []string{"build-lod-stack", "generate-split-sum-lut", "optimize-mesh", "prefilter-ibl-ggx"} {
		if !present[name] {
			t.Fatalf("supported actions are missing %q: %v", name, got)
		}
	}
}

// buildGridGLB writes a flat n by n grid as a GLB with positions, normals and
// texture coordinates.
func buildGridGLB(t *testing.T, n int) []byte {
	t.Helper()
	var positions, normals, uvs []float32
	for z := 0; z <= n; z++ {
		for x := 0; x <= n; x++ {
			positions = append(positions, float32(x)/float32(n), 0, float32(z)/float32(n))
			normals = append(normals, 0, 1, 0)
			uvs = append(uvs, float32(x)/float32(n), float32(z)/float32(n))
		}
	}
	var indices []uint32
	stride := uint32(n + 1)
	for z := 0; z < n; z++ {
		for x := 0; x < n; x++ {
			a := uint32(z)*stride + uint32(x)
			indices = append(indices, a, a+stride, a+1, a+1, a+stride, a+stride+1)
		}
	}

	var bin bytes.Buffer
	writeFloats := func(values []float32) (int, int) {
		offset := bin.Len()
		for _, value := range values {
			binary.Write(&bin, binary.LittleEndian, value)
		}
		return offset, bin.Len() - offset
	}
	positionOffset, positionLength := writeFloats(positions)
	normalOffset, normalLength := writeFloats(normals)
	uvOffset, uvLength := writeFloats(uvs)
	indexOffset := bin.Len()
	for _, value := range indices {
		binary.Write(&bin, binary.LittleEndian, value)
	}
	indexLength := bin.Len() - indexOffset
	for bin.Len()%4 != 0 {
		bin.WriteByte(0)
	}

	vertexCount := len(positions) / 3
	doc := map[string]any{
		"asset": map[string]any{"version": "2.0"},
		"meshes": []map[string]any{{
			"primitives": []map[string]any{{
				"attributes": map[string]any{"POSITION": 0, "NORMAL": 1, "TEXCOORD_0": 2},
				"indices":    3,
				"mode":       4,
			}},
		}},
		"nodes":  []map[string]any{{"mesh": 0}},
		"scenes": []map[string]any{{"nodes": []int{0}}},
		"accessors": []map[string]any{
			{"bufferView": 0, "componentType": 5126, "count": vertexCount, "type": "VEC3",
				"min": []float64{0, 0, 0}, "max": []float64{1, 0, 1}},
			{"bufferView": 1, "componentType": 5126, "count": vertexCount, "type": "VEC3"},
			{"bufferView": 2, "componentType": 5126, "count": vertexCount, "type": "VEC2"},
			{"bufferView": 3, "componentType": 5125, "count": len(indices), "type": "SCALAR"},
		},
		"bufferViews": []map[string]any{
			{"buffer": 0, "byteOffset": positionOffset, "byteLength": positionLength, "target": 34962},
			{"buffer": 0, "byteOffset": normalOffset, "byteLength": normalLength, "target": 34962},
			{"buffer": 0, "byteOffset": uvOffset, "byteLength": uvLength, "target": 34962},
			{"buffer": 0, "byteOffset": indexOffset, "byteLength": indexLength, "target": 34963},
		},
		"buffers": []map[string]any{{"byteLength": bin.Len()}},
	}
	jsonData, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	for len(jsonData)%4 != 0 {
		jsonData = append(jsonData, ' ')
	}
	payload := bin.Bytes()
	total := 12 + 8 + len(jsonData) + 8 + len(payload)
	var out bytes.Buffer
	out.WriteString("glTF")
	write32(&out, 2)
	write32(&out, uint32(total))
	write32(&out, uint32(len(jsonData)))
	write32(&out, 0x4E4F534A)
	out.Write(jsonData)
	write32(&out, uint32(len(payload)))
	write32(&out, 0x004E4942)
	out.Write(payload)
	return out.Bytes()
}

func listFiles(t *testing.T, dir string) []string {
	t.Helper()
	var out []string
	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() {
			out = append(out, path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(out)
	return out
}
