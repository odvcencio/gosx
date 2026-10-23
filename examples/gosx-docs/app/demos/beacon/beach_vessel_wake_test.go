package docs

import "testing"

func TestBlackglassClipperLeavesAWake(t *testing.T) {
	for _, view := range []string{"shore", "cliff", "ship"} {
		program := BlackglassBeachProgram(view, "")
		ocean := program.Environment.Ocean
		if ocean.Wake == nil || !ocean.Wake.Enabled {
			t.Fatalf("%s: the ocean has no wake", view)
		}
		vessel := blackglassBeachVessel()
		if ocean.Wake.NodeID != vessel.NodeID {
			t.Fatalf("%s: wake node %q, vessel %q", view, ocean.Wake.NodeID, vessel.NodeID)
		}
		if ocean.Wake.Length != vessel.Length || ocean.Wake.Beam != vessel.Beam {
			t.Errorf("%s: wake hull %v x %v, vessel %v x %v", view, ocean.Wake.Length, ocean.Wake.Beam, vessel.Length, vessel.Beam)
		}
	}
}
