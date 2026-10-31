package textlayout

import "testing"

func TestApproximateFontSizeReadsThePixelSize(t *testing.T) {
	for _, tc := range []struct {
		font string
		want float64
	}{
		{"16px Inter", 16},
		{"bold 12.5PX serif", 12.5},
		{"  italic 9px/1.2 Georgia", 9},
		{"Inter", 20},
		{"0px Inter", 20},
	} {
		if got := approximateFontSize(tc.font, 20); got != tc.want {
			t.Errorf("approximateFontSize(%q) = %v, want %v", tc.font, got, tc.want)
		}
	}
}
