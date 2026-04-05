package scene

import (
	"encoding/json"
	"math"
	"math/rand"
	"testing"
)

func TestCompressPointsPositionsReducesSize(t *testing.T) {
	positions := make([]float64, 3000) // 1000 vertices * 3 coords
	rng := rand.New(rand.NewSource(42))
	for i := range positions {
		positions[i] = rng.Float64()*20 - 10
	}

	props := Props{
		Width:       800,
		Height:      600,
		Compression: &Compression{BitWidth: 2},
		Graph: NewGraph(
			Points{
				ID:        "cloud",
				Count:     1000,
				Positions: vec3Slice(positions),
				Color:     "#ffffff",
				Size:      2.0,
			},
		),
	}

	ir := props.SceneIR()
	if len(ir.Points) != 1 {
		t.Fatalf("expected 1 points IR, got %d", len(ir.Points))
	}
	if len(ir.Points[0].CompressedPositions) == 0 {
		t.Fatal("expected compressed positions")
	}
	if ir.Points[0].Positions != nil {
		t.Fatal("expected raw positions to be nil")
	}

	// Compare JSON sizes
	compressed, _ := json.Marshal(ir)

	propsUnc := Props{
		Width:  800,
		Height: 600,
		Graph: NewGraph(
			Points{
				ID:        "cloud",
				Count:     1000,
				Positions: vec3Slice(positions),
				Color:     "#ffffff",
				Size:      2.0,
			},
		),
	}
	irUnc := propsUnc.SceneIR()
	uncompressed, _ := json.Marshal(irUnc)

	ratio := float64(len(compressed)) / float64(len(uncompressed))
	t.Logf("compressed: %d bytes, uncompressed: %d bytes, ratio: %.2f",
		len(compressed), len(uncompressed), ratio)
	if ratio > 0.20 {
		t.Fatalf("compression ratio %.2f exceeds 0.20 threshold", ratio)
	}
}

func TestCompressNoCompressionByDefault(t *testing.T) {
	props := Props{
		Width:  800,
		Height: 600,
		Graph: NewGraph(
			Points{
				ID:        "cloud",
				Count:     10,
				Positions: []Vector3{{1, 2, 3}, {4, 5, 6}},
				Color:     "#fff",
			},
		),
	}
	ir := props.SceneIR()
	if len(ir.Points[0].CompressedPositions) != 0 {
		t.Fatal("expected no compressed positions without Compression set")
	}
	if len(ir.Points[0].Positions) == 0 {
		t.Fatal("expected raw positions to be populated")
	}
}

func TestCompressSmallArrayPassthrough(t *testing.T) {
	// A single-element sizes array (< 2 elements) should not be compressed.
	props := Props{
		Width:       800,
		Height:      600,
		Compression: &Compression{BitWidth: 2},
		Graph: NewGraph(
			Points{
				ID:        "tiny",
				Count:     1,
				Positions: []Vector3{{1, 2, 3}},
				Sizes:     []float64{5.0},
				Color:     "#fff",
			},
		),
	}
	ir := props.SceneIR()
	// Positions has 3 floats (>= 2), so it gets compressed.
	// Sizes has 1 float (< 2), so it stays raw.
	if len(ir.Points[0].CompressedSizes) != 0 {
		t.Fatal("expected no compressed sizes for single-element array")
	}
	if len(ir.Points[0].Sizes) != 1 {
		t.Fatalf("expected 1 raw size, got %d", len(ir.Points[0].Sizes))
	}
}

func TestCompressInstancedMeshTransforms(t *testing.T) {
	rng := rand.New(rand.NewSource(77))
	transforms := make([]float64, 500*16) // 500 instances * 4x4 matrix
	for i := range transforms {
		transforms[i] = rng.Float64()*2 - 1
	}

	props := Props{
		Width:       800,
		Height:      600,
		Compression: &Compression{BitWidth: 2},
		Graph: NewGraph(
			InstancedMesh{
				ID:        "cubes",
				Count:     500,
				Geometry:  BoxGeometry{Width: 1, Height: 1, Depth: 1},
				Material:  FlatMaterial{Color: "#ff0000"},
				Positions: positionsFromTransforms(transforms, 500),
				Rotations: rotationsFromTransforms(transforms, 500),
				Scales:    scalesFromTransforms(transforms, 500),
			},
		),
	}
	ir := props.SceneIR()
	if len(ir.InstancedMeshes) != 1 {
		t.Fatalf("expected 1 instanced mesh, got %d", len(ir.InstancedMeshes))
	}
	if len(ir.InstancedMeshes[0].CompressedTransforms) == 0 {
		t.Fatal("expected compressed transforms")
	}
	if ir.InstancedMeshes[0].Transforms != nil {
		t.Fatal("expected raw transforms to be nil")
	}
}

func TestCompressDecompressRoundTrip(t *testing.T) {
	rng := rand.New(rand.NewSource(123))
	original := make([]float64, 300)
	for i := range original {
		original[i] = rng.Float64()*10 - 5
	}

	chunks := compressFloat64Array(original, 4)
	if len(chunks) == 0 {
		t.Fatal("expected compressed chunks")
	}

	recovered := DecompressFloat64Array(chunks)
	if len(recovered) != len(original) {
		t.Fatalf("length mismatch: got %d, want %d", len(recovered), len(original))
	}

	// At 4-bit quantization with chunk-based compression, max per-element
	// error should be < 10% of the value range. TurboQuant's MSE-optimal
	// codebook keeps average error much lower; this guards against outliers.
	valRange := 10.0 // values span [-5, 5]
	maxErr := 0.0
	for i := range original {
		err := math.Abs(original[i] - recovered[i])
		if err > maxErr {
			maxErr = err
		}
	}
	threshold := valRange * 0.10
	if maxErr > threshold {
		t.Fatalf("max error %.4f exceeds 10%% threshold (%.4f)", maxErr, threshold)
	}
	t.Logf("4-bit round-trip max error: %.4f (threshold %.4f)", maxErr, threshold)
}

func TestCompressChunkedLargeArray(t *testing.T) {
	rng := rand.New(rand.NewSource(456))
	// 10000 floats > sceneChunkSize (4096), forces chunking
	original := make([]float64, 10000)
	for i := range original {
		original[i] = rng.Float64()*6 - 3
	}

	chunks := compressFloat64Array(original, 2)
	if len(chunks) < 2 {
		t.Fatalf("expected >= 2 chunks for 10000 elements, got %d", len(chunks))
	}

	recovered := DecompressFloat64Array(chunks)
	if len(recovered) != len(original) {
		t.Fatalf("length mismatch: got %d, want %d", len(recovered), len(original))
	}
	t.Logf("chunked compression: %d chunks for %d elements", len(chunks), len(original))
}

func TestCompressLegacyPropsEmitsCompressed(t *testing.T) {
	rng := rand.New(rand.NewSource(88))
	positions := make([]float64, 30)
	for i := range positions {
		positions[i] = rng.Float64()
	}

	props := Props{
		Width:       800,
		Height:      600,
		Compression: &Compression{BitWidth: 2},
		Graph: NewGraph(
			Points{
				ID:        "pts",
				Count:     10,
				Positions: vec3Slice(positions),
				Color:     "#fff",
			},
		),
	}

	legacy := props.LegacyProps()
	sceneMap, ok := legacy["scene"].(map[string]any)
	if !ok {
		t.Fatal("expected scene in legacy props")
	}
	pointsList, ok := sceneMap["points"].([]map[string]any)
	if !ok || len(pointsList) == 0 {
		t.Fatal("expected points in scene")
	}
	if _, ok := pointsList[0]["compressedPositions"]; !ok {
		t.Fatal("expected compressedPositions in legacy props")
	}
	if _, ok := pointsList[0]["positions"]; ok {
		t.Fatal("raw positions should not be present when compressed")
	}
}

func TestProgressiveCompression(t *testing.T) {
	rng := rand.New(rand.NewSource(42))
	positions := make([]float64, 300)
	for i := range positions {
		positions[i] = rng.Float64()*20 - 10
	}

	props := Props{
		Width:  800,
		Height: 600,
		Compression: &Compression{
			BitWidth:    4,
			Progressive: true,
			// PreviewBitWidth defaults to 2
		},
		Graph: NewGraph(
			Points{
				ID:        "cloud",
				Count:     100,
				Positions: vec3Slice(positions),
				Color:     "#ffffff",
				Size:      2.0,
			},
		),
	}

	ir := props.SceneIR()
	pt := ir.Points[0]

	// Should have both compressed (4-bit) and preview (2-bit)
	if len(pt.CompressedPositions) == 0 {
		t.Fatal("expected compressed positions")
	}
	if len(pt.PreviewPositions) == 0 {
		t.Fatal("expected preview positions")
	}
	if pt.Positions != nil {
		t.Fatal("expected raw positions to be nil")
	}

	// Preview should be smaller than full
	previewSize := 0
	for _, c := range pt.PreviewPositions {
		previewSize += len(c.Packed)
	}
	fullSize := 0
	for _, c := range pt.CompressedPositions {
		fullSize += len(c.Packed)
	}
	if previewSize >= fullSize {
		t.Errorf("preview (%d bytes) should be smaller than full (%d bytes)", previewSize, fullSize)
	}
	t.Logf("preview: %d bytes (2-bit), full: %d bytes (4-bit)", previewSize, fullSize)

	// Both should decompress to approximately the same data
	previewData := DecompressFloat64Array(pt.PreviewPositions)
	fullData := DecompressFloat64Array(pt.CompressedPositions)
	if len(previewData) != len(fullData) {
		t.Fatalf("length mismatch: preview %d, full %d", len(previewData), len(fullData))
	}

	// Full should be closer to original than preview
	var previewErr, fullErr float64
	for i := range positions {
		pe := math.Abs(positions[i] - previewData[i])
		fe := math.Abs(positions[i] - fullData[i])
		previewErr += pe
		fullErr += fe
	}
	if fullErr >= previewErr {
		t.Errorf("full resolution (err %.4f) should be better than preview (err %.4f)", fullErr, previewErr)
	}
	t.Logf("preview total error: %.2f, full total error: %.2f", previewErr, fullErr)
}

func TestLODCompression(t *testing.T) {
	props := Props{
		Width:  800,
		Height: 600,
		Compression: &Compression{
			BitWidth: 4,
			LOD:      true,
		},
		Graph: NewGraph(
			Points{
				ID:        "cloud",
				Count:     10,
				Positions: []Vector3{{1, 2, 3}, {4, 5, 6}, {7, 8, 9}, {10, 11, 12}},
				Color:     "#fff",
			},
		),
	}

	ir := props.SceneIR()
	pt := ir.Points[0]

	if len(pt.CompressedPositions) == 0 {
		t.Fatal("expected compressed positions")
	}
	if len(pt.PreviewPositions) == 0 {
		t.Fatal("expected preview positions for LOD")
	}

	// Check that compression config is in legacy props
	legacy := props.LegacyProps()
	comp, ok := legacy["compression"].(map[string]any)
	if !ok {
		t.Fatal("expected compression in legacy props")
	}
	if comp["lod"] != true {
		t.Error("expected lod=true in compression config")
	}
	if comp["lodThreshold"] != 20.0 {
		t.Errorf("expected lodThreshold=20, got %v", comp["lodThreshold"])
	}
}

// Helper: convert flat float64 positions to Vector3 slice for Points nodes.
func vec3Slice(flat []float64) []Vector3 {
	out := make([]Vector3, len(flat)/3)
	for i := range out {
		out[i] = Vector3{flat[i*3], flat[i*3+1], flat[i*3+2]}
	}
	return out
}

// Helpers for InstancedMesh test -- extract positions/rotations/scales from flat transforms.
func positionsFromTransforms(flat []float64, count int) []Vector3 {
	out := make([]Vector3, count)
	for i := 0; i < count; i++ {
		base := i * 16
		out[i] = Vector3{flat[base+12], flat[base+13], flat[base+14]}
	}
	return out
}

func rotationsFromTransforms(_ []float64, count int) []Euler {
	out := make([]Euler, count)
	return out
}

func scalesFromTransforms(_ []float64, count int) []Vector3 {
	out := make([]Vector3, count)
	for i := range out {
		out[i] = Vector3{1, 1, 1}
	}
	return out
}
