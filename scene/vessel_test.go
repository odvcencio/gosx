package scene

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestVesselPropsAreOptInAndPreserveWalkSurfaces(t *testing.T) {
	plain, _ := json.Marshal(Props{}.GoSXSpreadProps())
	if strings.Contains(string(plain), `"vessel"`) {
		t.Fatal("vessel leaked into plain props")
	}
	v := &Vessel{NodeID: "clipper", Position: Vec3(18, 0, -42), Heading: .14, WindStrength: 8, LODs: []VesselLOD{{NodeID: "clipper-low", Distance: 100}}, WakeTexture: "/foam.png"}
	props := Props{Vessel: v, Walk: &Walk{Surfaces: []WalkSurface{{X: 25, Y: 2.5, Z: -20, SizeX: 2.5, SizeZ: 20}}}}
	for _, wire := range []map[string]any{props.LegacyProps(), props.GoSXSpreadProps()} {
		b, err := json.Marshal(wire)
		if err != nil {
			t.Fatal(err)
		}
		for _, want := range []string{`"vessel"`, `"nodeId":"clipper"`, `"wakeTexture":"/foam.png"`, `"surfaces"`} {
			if !strings.Contains(string(b), want) {
				t.Fatalf("missing %s", want)
			}
		}
	}
}
