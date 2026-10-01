package docs

import (
	"m31labs.dev/gosx/scene"
	"testing"
)

func TestBlackglassZoomCameraModes(t *testing.T) {
	for _, view := range []string{"shore", "glass", "cliff", "ship"} {
		p := BlackglassBeachProgram(view, "golden-hour")
		if p.ControlZoom == nil || !*p.ControlZoom {
			t.Fatalf("%s must enable zoom", view)
		}
		want := scene.ControlFirstPerson
		if view == "glass" || view == "cliff" {
			want = scene.ControlOrbit
		}
		if p.Controls != want {
			t.Fatalf("%s controls = %s, want %s", view, p.Controls, want)
		}
		if p.ControlMinDistance != 2 || p.ControlMaxDistance != 240 {
			t.Fatal("scenic dolly must stay bounded")
		}
	}
}
