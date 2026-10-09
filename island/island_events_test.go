package island

import "testing"

func TestEventNameToTypeCoversGestureEvents(t *testing.T) {
	for name, want := range map[string]string{
		"onWheel":              "wheel",
		"onDblClick":           "dblclick",
		"onContextMenu":        "contextmenu",
		"onLostPointerCapture": "lostpointercapture",
		"onPointerDown":        "pointerdown",
	} {
		if got := eventNameToType(name); got != want {
			t.Errorf("eventNameToType(%s) = %q, want %q", name, got, want)
		}
	}
}
