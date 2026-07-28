package imagediff_test

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"testing"

	"m31labs.dev/gosx/scene/imagediff"
)

func solid(width, height int, c color.RGBA) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			img.SetRGBA(x, y, c)
		}
	}
	return img
}

func fill(img *image.RGBA, minX, minY, maxX, maxY int, c color.RGBA) {
	for y := minY; y <= maxY; y++ {
		for x := minX; x <= maxX; x++ {
			img.SetRGBA(x, y, c)
		}
	}
}

func TestIdenticalImagesReportNoChange(t *testing.T) {
	reference := solid(64, 48, color.RGBA{R: 10, G: 20, B: 30, A: 255})
	candidate := solid(64, 48, color.RGBA{R: 10, G: 20, B: 30, A: 255})
	result, err := imagediff.Compare(reference, candidate, imagediff.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Identical || result.ChangedPixels != 0 || result.Bounds != nil {
		t.Fatalf("identical images reported change: %+v", result)
	}
	if result.ReferenceSHA256 != result.CandidateSHA256 {
		t.Fatalf("identical pixels hashed differently: %s vs %s", result.ReferenceSHA256, result.CandidateSHA256)
	}
	if result.MaxChannelDelta != 0 || result.MeanChannelDelta != 0 {
		t.Fatalf("unexpected deltas: %+v", result)
	}
}

// TestLocalizesTwoSeparateChanges is the capability a frame hash cannot supply:
// it says where the frame changed, not only that it changed.
func TestLocalizesTwoSeparateChanges(t *testing.T) {
	background := color.RGBA{R: 5, G: 5, B: 5, A: 255}
	reference := solid(128, 96, background)
	candidate := solid(128, 96, background)
	fill(candidate, 4, 4, 11, 11, color.RGBA{R: 255, A: 255})    // 64 pixels, top left
	fill(candidate, 90, 60, 109, 79, color.RGBA{G: 255, A: 255}) // 400 pixels, bottom right

	result, err := imagediff.Compare(reference, candidate, imagediff.Options{TileSize: 8})
	if err != nil {
		t.Fatal(err)
	}
	if result.Identical {
		t.Fatal("changed images reported identical")
	}
	if result.ChangedPixels != 64+400 {
		t.Fatalf("changed pixels = %d, want 464", result.ChangedPixels)
	}
	if result.RegionCount != 2 || len(result.Regions) != 2 {
		t.Fatalf("regions = %d (%+v), want 2", result.RegionCount, result.Regions)
	}
	// Regions rank by changed pixel count, so the larger block comes first.
	large, small := result.Regions[0], result.Regions[1]
	if large.ChangedPixels != 400 || large.MinX != 90 || large.MinY != 60 || large.MaxX != 109 || large.MaxY != 79 {
		t.Fatalf("large region = %s", large)
	}
	if small.ChangedPixels != 64 || small.MinX != 4 || small.MinY != 4 || small.MaxX != 11 || small.MaxY != 11 {
		t.Fatalf("small region = %s", small)
	}
	if result.Bounds == nil || result.Bounds.MinX != 4 || result.Bounds.MaxX != 109 {
		t.Fatalf("overall bounds = %+v", result.Bounds)
	}
	if result.Image == nil || result.DiffSHA256 == "" {
		t.Fatal("diff image and hash are required")
	}
}

func TestToleranceSuppressesSmallChange(t *testing.T) {
	reference := solid(32, 32, color.RGBA{R: 100, G: 100, B: 100, A: 255})
	candidate := solid(32, 32, color.RGBA{R: 102, G: 100, B: 100, A: 255})

	strict, err := imagediff.Compare(reference, candidate, imagediff.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if strict.Identical || strict.ChangedPixels != 32*32 || strict.MaxChannelDelta != 2 {
		t.Fatalf("strict compare = %+v", strict)
	}
	relaxed, err := imagediff.Compare(reference, candidate, imagediff.Options{Tolerance: 2})
	if err != nil {
		t.Fatal(err)
	}
	if !relaxed.Identical || relaxed.ChangedPixels != 0 {
		t.Fatalf("tolerant compare = %+v", relaxed)
	}
}

func TestSizeMismatchIsReportedNotHidden(t *testing.T) {
	result, err := imagediff.Compare(solid(32, 32, color.RGBA{A: 255}), solid(16, 32, color.RGBA{A: 255}), imagediff.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if !result.SizeMismatch || result.Identical {
		t.Fatalf("size mismatch must not report identical: %+v", result)
	}
	if result.ComparedPixels != 16*32 {
		t.Fatalf("compared pixels = %d, want the overlap 512", result.ComparedPixels)
	}
}

func TestRegionOrderIsStableAcrossRuns(t *testing.T) {
	background := color.RGBA{A: 255}
	reference := solid(96, 96, background)
	candidate := solid(96, 96, background)
	for _, box := range [][4]int{{0, 0, 7, 7}, {40, 40, 47, 47}, {80, 10, 87, 17}} {
		fill(candidate, box[0], box[1], box[2], box[3], color.RGBA{R: 200, G: 40, A: 255})
	}
	var first imagediff.Result
	for run := 0; run < 8; run++ {
		result, err := imagediff.Compare(reference, candidate, imagediff.Options{TileSize: 8})
		if err != nil {
			t.Fatal(err)
		}
		if run == 0 {
			first = result
			continue
		}
		if result.DiffSHA256 != first.DiffSHA256 || len(result.Regions) != len(first.Regions) {
			t.Fatalf("run %d diverged: %+v vs %+v", run, result, first)
		}
		for i := range result.Regions {
			if result.Regions[i] != first.Regions[i] {
				t.Fatalf("run %d region %d = %s, want %s", run, i, result.Regions[i], first.Regions[i])
			}
		}
	}
	if first.RegionCount != 3 {
		t.Fatalf("region count = %d, want 3", first.RegionCount)
	}
}

func TestPixelHashIgnoresPNGReencoding(t *testing.T) {
	reference := solid(24, 16, color.RGBA{R: 60, G: 90, B: 120, A: 255})
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, reference); err != nil {
		t.Fatal(err)
	}
	decoded, err := png.Decode(bytes.NewReader(encoded.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	result, err := imagediff.Compare(reference, decoded, imagediff.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Identical || result.ReferenceSHA256 != result.CandidateSHA256 {
		t.Fatalf("PNG round trip changed the pixel hash: %+v", result)
	}
}

func TestComparePNGRoundTrip(t *testing.T) {
	reference := solid(32, 32, color.RGBA{A: 255})
	candidate := solid(32, 32, color.RGBA{A: 255})
	fill(candidate, 8, 8, 15, 15, color.RGBA{B: 255, A: 255})
	var refPNG, candPNG bytes.Buffer
	if err := png.Encode(&refPNG, reference); err != nil {
		t.Fatal(err)
	}
	if err := png.Encode(&candPNG, candidate); err != nil {
		t.Fatal(err)
	}
	result, err := imagediff.ComparePNG(refPNG.Bytes(), candPNG.Bytes(), imagediff.Options{TileSize: 8})
	if err != nil {
		t.Fatal(err)
	}
	if result.ChangedPixels != 64 || result.RegionCount != 1 {
		t.Fatalf("PNG compare = %+v", result)
	}
	var diffPNG bytes.Buffer
	if err := result.WritePNG(&diffPNG); err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(diffPNG.Bytes(), []byte("\x89PNG\r\n\x1a\n")) {
		t.Fatal("diff output is not PNG")
	}
}
