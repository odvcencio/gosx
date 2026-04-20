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
			BitWidth:           4,
			Progressive:        true,
			ProgressiveDelayMS: 750,
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

	legacy := props.LegacyProps()
	comp, ok := legacy["compression"].(map[string]any)
	if !ok {
		t.Fatal("expected compression in legacy props")
	}
	if comp["progressiveDelayMS"] != 750 {
		t.Errorf("progressiveDelayMS = %v, want 750", comp["progressiveDelayMS"])
	}
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

func TestCompressAnimationKeyframes(t *testing.T) {
	rng := rand.New(rand.NewSource(99))

	// Simulate a 32-joint skeleton with 60 keyframes:
	// Each channel has 60 times + 60*4 quaternion values = 300 floats per channel.
	joints := 32
	frames := 60
	channels := make([]AnimationChannel, 0, joints)
	for j := 0; j < joints; j++ {
		times := make([]float64, frames)
		values := make([]float64, frames*4) // quaternion xyzw per frame
		for f := 0; f < frames; f++ {
			times[f] = float64(f) / 30.0 // 30fps
			values[f*4+0] = rng.Float64()*2 - 1
			values[f*4+1] = rng.Float64()*2 - 1
			values[f*4+2] = rng.Float64()*2 - 1
			values[f*4+3] = rng.Float64()*2 - 1
		}
		channels = append(channels, AnimationChannel{
			TargetNode:    j,
			Property:      "rotation",
			Interpolation: "LINEAR",
			Times:         times,
			Values:        values,
		})
	}

	props := Props{
		Width:       800,
		Height:      600,
		Compression: &Compression{BitWidth: 4},
		Graph: NewGraph(
			AnimationClip{
				Name:     "walk",
				Duration: 2.0,
				Channels: channels,
			},
		),
	}

	ir := props.SceneIR()
	if len(ir.Animations) != 1 {
		t.Fatalf("expected 1 animation clip, got %d", len(ir.Animations))
	}
	clip := ir.Animations[0]
	if clip.Name != "walk" {
		t.Fatalf("expected clip name 'walk', got %q", clip.Name)
	}
	if len(clip.Channels) != joints {
		t.Fatalf("expected %d channels, got %d", joints, len(clip.Channels))
	}

	// All channels should be compressed
	for i, ch := range clip.Channels {
		if len(ch.CompressedTimes) == 0 {
			t.Fatalf("channel %d: expected compressed times", i)
		}
		if len(ch.CompressedValues) == 0 {
			t.Fatalf("channel %d: expected compressed values", i)
		}
		if ch.Times != nil {
			t.Fatalf("channel %d: raw times should be nil", i)
		}
		if ch.Values != nil {
			t.Fatalf("channel %d: raw values should be nil", i)
		}
	}

	// Verify round-trip on first channel
	ch0 := clip.Channels[0]
	recoveredTimes := DecompressFloat64Array(ch0.CompressedTimes)
	recoveredValues := DecompressFloat64Array(ch0.CompressedValues)
	if len(recoveredTimes) != frames {
		t.Fatalf("times length mismatch: got %d, want %d", len(recoveredTimes), frames)
	}
	if len(recoveredValues) != frames*4 {
		t.Fatalf("values length mismatch: got %d, want %d", len(recoveredValues), frames*4)
	}

	// Compare JSON sizes
	compressed, _ := json.Marshal(ir)
	propsUnc := Props{
		Width:  800,
		Height: 600,
		Graph: NewGraph(
			AnimationClip{
				Name:     "walk",
				Duration: 2.0,
				Channels: channels,
			},
		),
	}
	irUnc := propsUnc.SceneIR()
	uncompressed, _ := json.Marshal(irUnc)
	ratio := float64(len(compressed)) / float64(len(uncompressed))
	t.Logf("compressed: %d bytes, uncompressed: %d bytes, ratio: %.2f", len(compressed), len(uncompressed), ratio)
	if ratio > 0.25 {
		t.Fatalf("compression ratio %.2f exceeds 0.25 threshold", ratio)
	}
}

func TestCompressAnimationNoCompressionByDefault(t *testing.T) {
	props := Props{
		Width:  800,
		Height: 600,
		Graph: NewGraph(
			AnimationClip{
				Name:     "idle",
				Duration: 1.0,
				Channels: []AnimationChannel{
					{
						TargetNode: 0,
						Property:   "translation",
						Times:      []float64{0, 0.5, 1.0},
						Values:     []float64{0, 0, 0, 1, 2, 3, 0, 0, 0},
					},
				},
			},
		),
	}
	ir := props.SceneIR()
	if len(ir.Animations) != 1 {
		t.Fatalf("expected 1 animation, got %d", len(ir.Animations))
	}
	ch := ir.Animations[0].Channels[0]
	if len(ch.CompressedTimes) != 0 {
		t.Fatal("expected no compressed times without Compression set")
	}
	if len(ch.Times) == 0 {
		t.Fatal("expected raw times to be populated")
	}
	if len(ch.CompressedValues) != 0 {
		t.Fatal("expected no compressed values without Compression set")
	}
	if len(ch.Values) == 0 {
		t.Fatal("expected raw values to be populated")
	}
}

func TestCompressAnimationLegacyPropsEmitsCompressed(t *testing.T) {
	rng := rand.New(rand.NewSource(42))
	times := make([]float64, 30)
	values := make([]float64, 30*3)
	for i := range times {
		times[i] = float64(i) / 30.0
	}
	for i := range values {
		values[i] = rng.Float64()*10 - 5
	}

	props := Props{
		Width:       800,
		Height:      600,
		Compression: &Compression{BitWidth: 2},
		Graph: NewGraph(
			AnimationClip{
				Name:     "bounce",
				Duration: 1.0,
				Channels: []AnimationChannel{
					{
						TargetNode: 0,
						Property:   "translation",
						Times:      times,
						Values:     values,
					},
				},
			},
		),
	}

	legacy := props.LegacyProps()
	sceneMap, ok := legacy["scene"].(map[string]any)
	if !ok {
		t.Fatal("expected scene in legacy props")
	}
	animations, ok := sceneMap["animations"].([]map[string]any)
	if !ok || len(animations) == 0 {
		t.Fatal("expected animations in scene")
	}
	channels, ok := animations[0]["channels"].([]map[string]any)
	if !ok || len(channels) == 0 {
		t.Fatal("expected channels in animation")
	}
	if _, ok := channels[0]["compressedTimes"]; !ok {
		t.Fatal("expected compressedTimes in legacy props")
	}
	if _, ok := channels[0]["times"]; ok {
		t.Fatal("raw times should not be present when compressed")
	}
	if _, ok := channels[0]["compressedValues"]; !ok {
		t.Fatal("expected compressedValues in legacy props")
	}
	if _, ok := channels[0]["values"]; ok {
		t.Fatal("raw values should not be present when compressed")
	}
}

func TestCompressAnimationProgressiveKeyframes(t *testing.T) {
	rng := rand.New(rand.NewSource(77))
	frames := 60
	times := make([]float64, frames)
	values := make([]float64, frames*4) // rotation quaternions
	for i := 0; i < frames; i++ {
		times[i] = float64(i) / 30.0
		values[i*4+0] = rng.Float64()*2 - 1
		values[i*4+1] = rng.Float64()*2 - 1
		values[i*4+2] = rng.Float64()*2 - 1
		values[i*4+3] = rng.Float64()*2 - 1
	}

	props := Props{
		Width:  800,
		Height: 600,
		Compression: &Compression{
			BitWidth:    4,
			Progressive: true,
		},
		Graph: NewGraph(
			AnimationClip{
				Name:     "spin",
				Duration: 2.0,
				Channels: []AnimationChannel{
					{
						TargetNode: 0,
						Property:   "rotation",
						Times:      times,
						Values:     values,
					},
				},
			},
		),
	}

	ir := props.SceneIR()
	ch := ir.Animations[0].Channels[0]

	if len(ch.CompressedValues) == 0 {
		t.Fatal("expected compressed values")
	}
	if len(ch.PreviewValues) == 0 {
		t.Fatal("expected preview values")
	}
	if ch.Values != nil {
		t.Fatal("expected raw values to be nil")
	}

	// Preview should be smaller than full
	previewSize := 0
	for _, c := range ch.PreviewValues {
		previewSize += len(c.Packed)
	}
	fullSize := 0
	for _, c := range ch.CompressedValues {
		fullSize += len(c.Packed)
	}
	if previewSize >= fullSize {
		t.Errorf("preview (%d bytes) should be smaller than full (%d bytes)", previewSize, fullSize)
	}
	t.Logf("preview: %d bytes (2-bit), full: %d bytes (4-bit)", previewSize, fullSize)
}

func TestCompressAnimationKeyframeRoundTrip(t *testing.T) {
	rng := rand.New(rand.NewSource(55))
	frames := 100
	times := make([]float64, frames)
	values := make([]float64, frames*3)
	for i := 0; i < frames; i++ {
		times[i] = float64(i) * 0.033
		values[i*3+0] = rng.Float64()*4 - 2
		values[i*3+1] = rng.Float64()*4 - 2
		values[i*3+2] = rng.Float64()*4 - 2
	}

	compressedTimes := compressFloat64Array(times, 8)
	compressedValues := compressFloat64Array(values, 4)

	recoveredTimes := DecompressFloat64Array(compressedTimes)
	recoveredValues := DecompressFloat64Array(compressedValues)

	if len(recoveredTimes) != frames {
		t.Fatalf("times length mismatch: got %d, want %d", len(recoveredTimes), frames)
	}
	if len(recoveredValues) != frames*3 {
		t.Fatalf("values length mismatch: got %d, want %d", len(recoveredValues), frames*3)
	}

	// Check times: at 8-bit, error should be very small
	maxTimeErr := 0.0
	for i := range times {
		err := math.Abs(times[i] - recoveredTimes[i])
		if err > maxTimeErr {
			maxTimeErr = err
		}
	}
	timeRange := times[frames-1] - times[0]
	if maxTimeErr > timeRange*0.01 {
		t.Fatalf("time max error %.6f exceeds 1%% threshold (%.6f)", maxTimeErr, timeRange*0.01)
	}

	// Check values: at 4-bit, error should be < 10% of range
	maxValErr := 0.0
	for i := range values {
		err := math.Abs(values[i] - recoveredValues[i])
		if err > maxValErr {
			maxValErr = err
		}
	}
	valRange := 4.0
	if maxValErr > valRange*0.10 {
		t.Fatalf("value max error %.4f exceeds 10%% threshold (%.4f)", maxValErr, valRange*0.10)
	}
	t.Logf("8-bit times max error: %.6f, 4-bit values max error: %.4f", maxTimeErr, maxValErr)
}

func TestCompressAnimationEmptyClipSkipped(t *testing.T) {
	props := Props{
		Width:       800,
		Height:      600,
		Compression: &Compression{BitWidth: 2},
		Graph: NewGraph(
			AnimationClip{
				Name:     "",
				Duration: 1.0,
				Channels: []AnimationChannel{
					{TargetNode: 0, Property: "translation", Times: []float64{0}, Values: []float64{1, 2, 3}},
				},
			},
			AnimationClip{
				Name:     "valid",
				Duration: 1.0,
				Channels: []AnimationChannel{},
			},
		),
	}
	ir := props.SceneIR()
	if len(ir.Animations) != 0 {
		t.Fatalf("expected 0 animations (empty name and empty channels should be skipped), got %d", len(ir.Animations))
	}
}
