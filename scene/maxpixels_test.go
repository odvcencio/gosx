package scene

import "testing"

// MaxPixels must reach the browser runtime: it is the only knob that can bound a
// fill-bound scene by actual resolution. MaxDevicePixelRatio is blind to display
// size, so a Retina display silently multiplies fragment cost for identical props.
func TestPropsMaxPixelsSerializes(t *testing.T) {
	t.Parallel()

	p := Props{MaxPixels: 3_000_000, MaxDevicePixelRatio: 1.75}
	out := p.LegacyProps()
	got, ok := out["maxPixels"]
	if !ok {
		t.Fatalf("maxPixels missing from composable props: %#v", out)
	}
	if got != 3_000_000 {
		t.Fatalf("maxPixels = %v, want 3000000", got)
	}

	// Zero stays absent so the browser applies its default budget.
	var zero Props
	if _, ok := zero.LegacyProps()["maxPixels"]; ok {
		t.Fatal("maxPixels must be omitted when unset")
	}
}

func TestPropsMaxPixelsUnboundedSerializes(t *testing.T) {
	t.Parallel()
	if got := (Props{MaxPixels: MaxPixelsUnbounded}).LegacyProps()["maxPixels"]; got != -1 {
		t.Fatalf("unbounded maxPixels = %v, want -1", got)
	}
}
