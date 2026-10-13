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
	v := &Vessel{NodeID: "clipper", Position: Vec3(18, 0, -42), Heading: .14, WindStrength: 8, LODs: []VesselLOD{{NodeID: "clipper-low", Distance: 100}}, WakeTexture: "/foam.png", QuickBoard: true, OpenWater: true}
	props := Props{Vessel: v, Walk: &Walk{Surfaces: []WalkSurface{{X: 25, Y: 2.5, Z: -20, SizeX: 2.5, SizeZ: 20}}}}
	for _, wire := range []map[string]any{props.LegacyProps(), props.GoSXSpreadProps()} {
		b, err := json.Marshal(wire)
		if err != nil {
			t.Fatal(err)
		}
		for _, want := range []string{`"vessel"`, `"nodeId":"clipper"`, `"wakeTexture":"/foam.png"`, `"quickBoard":true`, `"openWater":true`, `"surfaces"`} {
			if !strings.Contains(string(b), want) {
				t.Fatalf("missing %s", want)
			}
		}
	}
}

func TestVesselNavigationWirePreservesZeroCommands(t *testing.T) {
	plain, _ := json.Marshal(Vessel{NodeID: "ship"})
	if strings.Contains(string(plain), `"navigation"`) {
		t.Fatal("navigation must remain opt-in")
	}
	for _, command := range []VesselCommand{{NodeID: "ship", Action: "course"}, {NodeID: "ship", Action: "trim"}} {
		wire, err := json.Marshal(command)
		if err != nil || !strings.Contains(string(wire), `"bearing":0`) || !strings.Contains(string(wire), `"trim":0`) {
			t.Fatalf("zero bearing and lowered sails must survive the wire: %s, %v", wire, err)
		}
	}
}
