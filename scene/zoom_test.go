package scene

import "testing"

func TestControlZoomIsOptInAndLowersToRuntime(t *testing.T) {
	if _, ok := (Props{}).LegacyProps()["controlZoom"]; ok {
		t.Fatal("zoom must be absent by default")
	}
	for _, enabled := range []bool{false, true} {
		if got := (Props{ControlZoom: Bool(enabled)}).LegacyProps()["controlZoom"]; got != enabled {
			t.Fatalf("controlZoom = %v, want %v", got, enabled)
		}
	}
}
