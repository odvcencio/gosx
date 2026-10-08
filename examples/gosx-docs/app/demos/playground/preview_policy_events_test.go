package playground

import "testing"

func TestPlaygroundAllowsGestureEvents(t *testing.T) {
	for _, event := range []string{"wheel", "dblclick", "contextmenu", "lostpointercapture"} {
		if _, ok := playgroundAllowedEvents[event]; !ok {
			t.Errorf("playgroundAllowedEvents lacks %q", event)
		}
	}
}
