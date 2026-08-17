package imagepipe

import (
	"image"
	"testing"
)

func TestResizeScalesToTargetWidthPreservingAspect(t *testing.T) {
	src := newTestImage(1200, 800)
	out, err := Resize(src, 600)
	if err != nil {
		t.Fatalf("Resize: %v", err)
	}
	bounds := out.Bounds()
	if bounds.Dx() != 600 {
		t.Fatalf("width = %d, want 600", bounds.Dx())
	}
	// 1200x800 at half width scales to half height: 400.
	if bounds.Dy() != 400 {
		t.Fatalf("height = %d, want 400 (aspect-correct for 1200x800 -> 600w)", bounds.Dy())
	}
}

func TestResizeAtSourceWidthReturnsUnchanged(t *testing.T) {
	src := newTestImage(500, 300)
	out, err := Resize(src, 500)
	if err != nil {
		t.Fatalf("Resize: %v", err)
	}
	if out != image.Image(src) {
		t.Fatal("Resize at the source's own width should return the same image, not a copy")
	}
}

func TestResizeRefusesToUpscale(t *testing.T) {
	src := newTestImage(400, 300)
	if _, err := Resize(src, 800); err == nil {
		t.Fatal("expected an error when targetWidth exceeds the source width")
	}
}

func TestResizeRejectsNonPositiveWidth(t *testing.T) {
	src := newTestImage(400, 300)
	for _, width := range []int{0, -1, -100} {
		if _, err := Resize(src, width); err == nil {
			t.Fatalf("expected an error for targetWidth=%d", width)
		}
	}
}

func TestResizeNeverExceedsRequestedWidth(t *testing.T) {
	src := newTestImage(1920, 1080)
	for _, width := range []int{320, 480, 750, 1080, 1920} {
		out, err := Resize(src, width)
		if err != nil {
			t.Fatalf("Resize(%d): %v", width, err)
		}
		if out.Bounds().Dx() != width {
			t.Errorf("Resize(%d) width = %d, want %d", width, out.Bounds().Dx(), width)
		}
		if out.Bounds().Dy() > 1080 {
			t.Errorf("Resize(%d) height = %d exceeds source height 1080", width, out.Bounds().Dy())
		}
	}
}
