// Opt-in measurements for the mesh optimization stage. Set GOSX_MEASURE=1 to
// run them. They report the byte ratio and the error metric of every technique
// on real and synthetic assets, so a reader can see whether a technique pays.
package assetpipe

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// measureAsset copies one asset tree into a temporary root, runs the stage, and
// reports every metric.
func measureAsset(t *testing.T, name string, files map[string][]byte, entry string, opts OptimizeOptions) {
	t.Helper()
	dir := t.TempDir()
	for relative, data := range files {
		mustWriteBytes(t, filepath.Join(dir, filepath.FromSlash(relative)), data)
	}
	report, err := Plan([]string{dir}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	updated, execReport, err := Execute(report, ExecuteOptions{
		Root:     dir,
		Only:     []string{"optimize-mesh"},
		Optimize: opts,
	})
	if err != nil {
		t.Fatal(err)
	}
	elapsed := time.Since(start)

	sourceBytes := int64(len(files[entry]))
	for relative, data := range files {
		if relative != entry && (strings.HasSuffix(relative, ".bin") || strings.HasSuffix(relative, ".glb")) {
			sourceBytes += int64(len(data))
		}
	}
	t.Logf("=== %s ===", name)
	t.Logf("source bytes (document plus buffers): %d, stage time %v", sourceBytes, elapsed)
	for _, result := range execReport.Results {
		if result.Action != "optimize-mesh" {
			continue
		}
		t.Logf("  %s: %s in %dms", result.Path, result.Status, result.DurationMS)
		if result.Reason != "" {
			t.Logf("    reason: %s", result.Reason)
		}
		keys := sortedKeys(result.Metrics)
		for _, key := range keys {
			t.Logf("    %-28s %s", key, result.Metrics[key])
		}
		if result.OutputBytes > 0 && sourceBytes > 0 {
			t.Logf("    whole file ratio against source: %.4f", float64(result.OutputBytes)/float64(sourceBytes))
		}
	}
	for _, warning := range execReport.Warnings {
		t.Logf("  warning: %s", warning)
	}
	for _, asset := range updated.Assets {
		for _, variant := range asset.Variants {
			if variant.SourceAction == "optimize-mesh" && variant.State == VariantBuilt {
				t.Logf("  wrote %s at %d bytes", variant.URI, variant.Bytes)
			}
		}
	}
}

func sortedKeys(values map[string]string) []string {
	out := make([]string, 0, len(values))
	for key := range values {
		out = append(out, key)
	}
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j] < out[j-1]; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}

func TestMeasureOptimizeOnTheDuck(t *testing.T) {
	if os.Getenv("GOSX_MEASURE") == "" {
		t.Skip("set GOSX_MEASURE=1")
	}
	root := "../examples/gosx-docs/public/water/models/duck"
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Skipf("the duck asset is not present: %v", err)
	}
	files := map[string][]byte{}
	for _, entry := range entries {
		data, err := os.ReadFile(filepath.Join(root, entry.Name()))
		if err != nil {
			t.Fatal(err)
		}
		files["models/duck/"+entry.Name()] = data
	}
	measureAsset(t, "Duck.gltf (real asset, external buffer)", files, "models/duck/Duck.gltf",
		OptimizeOptions{Measure: true})
}

func TestMeasureOptimizeOnSyntheticMeshes(t *testing.T) {
	if os.Getenv("GOSX_MEASURE") == "" {
		t.Skip("set GOSX_MEASURE=1")
	}
	cases := []struct {
		name string
		glb  []byte
		opts OptimizeOptions
	}{
		{
			name: "UV sphere 128x256, row order, positions and normals",
			glb:  buildShellsGLB(t, 128, 256, []float64{1.0}),
			opts: OptimizeOptions{Measure: true},
		},
		{
			name: "UV sphere 128x256, quantization off",
			glb:  buildShellsGLB(t, 128, 256, []float64{1.0}),
			opts: OptimizeOptions{Measure: true, SkipQuantize: true},
		},
		{
			name: "UV sphere 128x256, reordering off",
			glb:  buildShellsGLB(t, 128, 256, []float64{1.0}),
			opts: OptimizeOptions{Measure: true, SkipVertexCache: true, SkipOverdraw: true, SkipVertexFetch: true},
		},
		{
			name: "UV sphere 128x256, positions at 8 bits",
			glb:  buildShellsGLB(t, 128, 256, []float64{1.0}),
			opts: OptimizeOptions{Measure: true, PositionBits: 8},
		},
		{
			name: "UV sphere 128x256, mesh with no node (position fold impossible)",
			glb:  sphereGLB(t, 128, 256),
			opts: OptimizeOptions{Measure: true},
		},
		{
			name: "grid 96x96 with texture coordinates",
			glb:  buildGridGLB(t, 96),
			opts: OptimizeOptions{Measure: true},
		},
		{
			name: "grid 48x48 as an unindexed triangle soup",
			glb:  buildSoupGLB(t, 48),
			opts: OptimizeOptions{Measure: true},
		},
		{
			name: "nested shells, overdraw sort on",
			glb:  buildShellsGLB(t, 48, 64, []float64{0.5, 1.0}),
			opts: OptimizeOptions{Measure: true},
		},
		{
			name: "nested shells, overdraw sort off",
			glb:  buildShellsGLB(t, 48, 64, []float64{0.5, 1.0}),
			opts: OptimizeOptions{Measure: true, SkipOverdraw: true},
		},
	}
	for _, item := range cases {
		measureAsset(t, item.name, map[string][]byte{"models/mesh.glb": item.glb}, "models/mesh.glb", item.opts)
	}
}

// buildShellsGLB writes two concentric spheres in one primitive. The outer
// shell hides the inner one, so a front-to-back order lowers overdraw.
func buildShellsGLB(t *testing.T, rings, segments int, radii []float64) []byte {
	t.Helper()
	var positions, normals []float32
	var indices []uint32
	for _, radius := range radii {
		base := uint32(len(positions) / 3)
		for ring := 0; ring <= rings; ring++ {
			phi := math.Pi * float64(ring) / float64(rings)
			for segment := 0; segment < segments; segment++ {
				theta := 2 * math.Pi * float64(segment) / float64(segments)
				x := math.Sin(phi) * math.Cos(theta)
				y := math.Cos(phi)
				z := math.Sin(phi) * math.Sin(theta)
				positions = append(positions, float32(radius*x), float32(radius*y), float32(radius*z))
				normals = append(normals, float32(x), float32(y), float32(z))
			}
		}
		for ring := 0; ring < rings; ring++ {
			for segment := 0; segment < segments; segment++ {
				a := base + uint32(ring*segments+segment)
				b := base + uint32(ring*segments+(segment+1)%segments)
				c := a + uint32(segments)
				d := b + uint32(segments)
				indices = append(indices, a, c, b, b, c, d)
			}
		}
	}

	var payload []byte
	positionOffset := len(payload)
	payload = appendFloat32s(payload, positions)
	normalOffset := len(payload)
	payload = appendFloat32s(payload, normals)
	indexOffset := len(payload)
	payload = appendUint32s(payload, indices)

	count := len(positions) / 3
	doc := map[string]any{
		"asset": map[string]any{"version": "2.0"},
		"meshes": []map[string]any{{
			"primitives": []map[string]any{{
				"attributes": map[string]any{"POSITION": 0, "NORMAL": 1},
				"indices":    2,
				"mode":       4,
			}},
		}},
		"nodes":  []map[string]any{{"mesh": 0}},
		"scenes": []map[string]any{{"nodes": []int{0}}},
		"accessors": []map[string]any{
			{"bufferView": 0, "componentType": 5126, "count": count, "type": "VEC3",
				"min": []float64{-1, -1, -1}, "max": []float64{1, 1, 1}},
			{"bufferView": 1, "componentType": 5126, "count": count, "type": "VEC3"},
			{"bufferView": 2, "componentType": 5125, "count": len(indices), "type": "SCALAR"},
		},
		"bufferViews": []map[string]any{
			{"buffer": 0, "byteOffset": positionOffset, "byteLength": normalOffset - positionOffset, "target": 34962},
			{"buffer": 0, "byteOffset": normalOffset, "byteLength": indexOffset - normalOffset, "target": 34962},
			{"buffer": 0, "byteOffset": indexOffset, "byteLength": len(payload) - indexOffset, "target": 34963},
		},
		"buffers": []map[string]any{{"byteLength": len(payload)}},
	}
	return glbFrom(t, doc, payload)
}

// TestMeasureOptimizeGeometryFidelity reports the world-space error of the whole
// chain on every synthetic asset, measured against the source geometry.
func TestMeasureOptimizeGeometryFidelity(t *testing.T) {
	if os.Getenv("GOSX_MEASURE") == "" {
		t.Skip("set GOSX_MEASURE=1")
	}
	cases := []struct {
		name string
		glb  []byte
	}{
		{name: "sphere 64x128", glb: buildShellsGLB(t, 64, 128, []float64{1.0})},
		{name: "grid 64", glb: buildGridGLB(t, 64)},
		{name: "soup 32", glb: buildSoupGLB(t, 32)},
		{name: "shells 32x48", glb: buildShellsGLB(t, 32, 48, []float64{0.5, 1.0})},
	}
	for _, item := range cases {
		_, optimized, _ := runOptimizeOn(t, item.glb, OptimizeOptions{})
		if optimized == nil {
			t.Errorf("%s: the stage wrote nothing", item.name)
			continue
		}
		sourceTriangles, sourceLow, sourceHigh := flattenGLB(t, item.glb)
		outTriangles, outLow, outHigh := flattenGLB(t, optimized)
		diagonal := 0.0
		for axis := 0; axis < 3; axis++ {
			delta := sourceHigh[axis] - sourceLow[axis]
			diagonal += delta * delta
		}
		diagonal = math.Sqrt(diagonal)
		displacement := nearestVertexDistance(sourceTriangles, outTriangles, diagonal/32)
		areaBefore := totalArea(sourceTriangles)
		areaAfter := totalArea(outTriangles)
		boundsShift := 0.0
		for axis := 0; axis < 3; axis++ {
			boundsShift = math.Max(boundsShift, math.Abs(sourceLow[axis]-outLow[axis]))
			boundsShift = math.Max(boundsShift, math.Abs(sourceHigh[axis]-outHigh[axis]))
		}
		t.Logf("%s: bytes %d -> %d (%.4f), triangles %d -> %d, displacement %.3g (%.3g of the diagonal), area delta %.3g%%, bounds shift %.3g",
			item.name, len(item.glb), len(optimized), float64(len(optimized))/float64(len(item.glb)),
			len(sourceTriangles), len(outTriangles),
			displacement, displacement/diagonal,
			100*(areaAfter-areaBefore)/areaBefore, boundsShift)
		// Welding at a pole merges the vertices of a fan, which turns those
		// triangles into lines. A line draws nothing, so dropping it changes the
		// triangle count without changing the picture. The surface area check
		// below is the real guard.
		if len(outTriangles) > len(sourceTriangles) {
			t.Errorf("%s: the stage invented triangles: %d, want at most %d",
				item.name, len(outTriangles), len(sourceTriangles))
		}
		if math.Abs(areaAfter-areaBefore) > areaBefore*1e-4 {
			t.Errorf("%s: surface area changed by %g%%", item.name, 100*(areaAfter-areaBefore)/areaBefore)
		}
		if displacement > diagonal*1e-4 {
			t.Errorf("%s: displacement %g is above one part in ten thousand of the diagonal", item.name, displacement)
		}
	}
}

// TestMeasureOptimizeInstancingCost reports whether EXT_mesh_gpu_instancing
// pays for itself in bytes at several group sizes. It ships nothing: it answers
// the question the default off setting raises.
func TestMeasureOptimizeInstancingCost(t *testing.T) {
	if os.Getenv("GOSX_MEASURE") == "" {
		t.Skip("set GOSX_MEASURE=1")
	}
	for _, count := range []int{4, 16, 64, 256} {
		source := buildInstancedGLB(t, count)
		plainResult, plainBytes, _ := runOptimizeOn(t, source, OptimizeOptions{})
		instanced, instancedBytes, _ := runOptimizeOn(t, source, OptimizeOptions{EmitInstancing: true})
		t.Logf("%3d copies: source %6d, no instancing %6d, instanced %6d, delta %+d bytes (%s)",
			count, len(source), len(plainBytes), len(instancedBytes),
			len(instancedBytes)-len(plainBytes),
			fmt.Sprintf("groups=%s emitted=%s plainStatus=%s instancedStatus=%s",
				plainResult.Metrics["instanceGroups"], instanced.Metrics["instancesEmitted"],
				plainResult.Status, instanced.Status))
	}
}
