package imagepipe

import "testing"

func TestProcessReturnsIntrinsicDimensionsAndOneVariantPerWidthFormatPair(t *testing.T) {
	dir := t.TempDir()
	path := writeTestPNG(t, dir, "source", 1200, 800)

	widths := Ladder(1200, gosxDefaultCandidates)
	formats := []Format{FormatWebP, FormatPNG}

	dims, variants, err := Process(path, widths, formats, EncodeOptions{})
	if err != nil {
		t.Fatalf("Process: %v", err)
	}
	if dims != (Dimensions{Width: 1200, Height: 800}) {
		t.Fatalf("dims = %+v, want {1200 800}", dims)
	}
	if len(variants) != len(widths)*len(formats) {
		t.Fatalf("got %d variants, want %d (%d widths x %d formats)", len(variants), len(widths)*len(formats), len(widths), len(formats))
	}

	seen := make(map[[2]any]bool, len(variants))
	for _, v := range variants {
		key := [2]any{v.Width, v.Format}
		if seen[key] {
			t.Fatalf("duplicate variant for width=%d format=%s", v.Width, v.Format)
		}
		seen[key] = true
		if v.Width > dims.Width {
			t.Errorf("variant width %d exceeds intrinsic width %d", v.Width, dims.Width)
		}
		if len(v.Data) == 0 {
			t.Errorf("variant width=%d format=%s has no data", v.Width, v.Format)
		}
	}
	for _, width := range widths {
		for _, format := range formats {
			if !seen[[2]any{width, format}] {
				t.Errorf("missing variant width=%d format=%s", width, format)
			}
		}
	}
}

func TestProcessNeverUpscalesEvenWhenCallerPassesAWidthAboveIntrinsic(t *testing.T) {
	dir := t.TempDir()
	path := writeTestPNG(t, dir, "small-source", 300, 200)

	// A caller that forgot to run its widths through Ladder first should
	// get a clear error, not a silently upscaled image.
	_, _, err := Process(path, []int{300, 600}, []Format{FormatPNG}, EncodeOptions{})
	if err == nil {
		t.Fatal("expected Process to error on a width above the source's intrinsic width")
	}
}

func TestProcessRejectsUnreadableSource(t *testing.T) {
	if _, _, err := Process("/nonexistent/source.png", []int{100}, []Format{FormatPNG}, EncodeOptions{}); err == nil {
		t.Fatal("expected an error for a nonexistent source path")
	}
}

func TestProcessSingleWidthMatchingIntrinsicProducesOneVariantPerFormat(t *testing.T) {
	dir := t.TempDir()
	path := writeTestJPEG(t, dir, "same-width", 500, 500)

	dims, variants, err := Process(path, []int{500}, []Format{FormatWebP, FormatJPEG}, EncodeOptions{})
	if err != nil {
		t.Fatalf("Process: %v", err)
	}
	if dims.Width != 500 || dims.Height != 500 {
		t.Fatalf("dims = %+v, want 500x500", dims)
	}
	if len(variants) != 2 {
		t.Fatalf("got %d variants, want 2", len(variants))
	}
}
