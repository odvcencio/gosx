package scene

import (
	"encoding/json"
	"math"
	"reflect"
	"strings"
	"testing"
)

func TestOceanWakeIsOffByDefaultAndAbsentFromTheWire(t *testing.T) {
	ocean := normalizeOcean(&Ocean{Spectrum: &OceanSpectrum{}})
	if ocean.Wake != nil || ocean.GraphNodes != nil {
		t.Fatalf("wake %v nodes %v without an authored wake", ocean.Wake, ocean.GraphNodes)
	}
	p := ocean.GraphParams
	if p["wakeEnabled"] != 0 || p["hullLength"] != 200 || p["hullBeam"] != 30 {
		t.Fatalf("defaults wakeEnabled %v hullLength %v hullBeam %v, want 0, 200, 30", p["wakeEnabled"], p["hullLength"], p["hullBeam"])
	}
	wire, _ := json.Marshal(normalizeOcean(&Ocean{Level: 2}))
	if string(wire) != `{"level":2}` {
		t.Fatalf("a plain ocean's wire changed: %s", wire)
	}
}

func TestOceanWakeBindsAGraphNodeAndFillsTheHullParams(t *testing.T) {
	ocean := normalizeOcean(&Ocean{Spectrum: &OceanSpectrum{}, Wake: &OceanWake{NodeID: "  ship ", Enabled: true, Length: 230, Beam: 28}})
	if ocean.Wake == nil || ocean.Wake.NodeID != "ship" {
		t.Fatalf("wake %+v", ocean.Wake)
	}
	if got := ocean.GraphNodes[OceanGraphHullNode]; got != "ship" || len(ocean.GraphNodes) != 1 {
		t.Fatalf("graph nodes %v, want {%s: ship}", ocean.GraphNodes, OceanGraphHullNode)
	}
	p := ocean.GraphParams
	if p["wakeEnabled"] != 1 || p["hullLength"] != 230 || p["hullBeam"] != 28 {
		t.Fatalf("params %v", p)
	}
	wire, err := json.Marshal(ocean)
	if err != nil {
		t.Fatal(err)
	}
	var back map[string]any
	_ = json.Unmarshal(wire, &back)
	wake := back["wake"].(map[string]any)
	if wake["nodeId"] != "ship" || wake["enabled"] != true || wake["length"] != float64(230) || wake["beam"] != float64(28) {
		t.Fatalf("wire %s", wire)
	}
	if nodes := back["graphNodes"].(map[string]any); nodes["hull"] != "ship" {
		t.Fatalf("graphNodes on the wire: %s", wire)
	}
}

func TestOceanWakeNeedsANodeToBeEnabledAndKeepsItsSizesInRange(t *testing.T) {
	cases := []struct {
		name         string
		wake         *OceanWake
		enabled      float64
		length, beam float64
	}{
		{"no node", &OceanWake{Enabled: true}, 0, 200, 30},
		{"blank node", &OceanWake{NodeID: "   ", Enabled: true}, 0, 200, 30},
		{"disabled", &OceanWake{NodeID: "ship"}, 0, 200, 30},
		{"too small", &OceanWake{NodeID: "ship", Enabled: true, Length: 1, Beam: 0.1}, 1, 5, 1},
		{"too large", &OceanWake{NodeID: "ship", Enabled: true, Length: 9000, Beam: 900}, 1, 500, 80},
		{"not finite", &OceanWake{NodeID: "ship", Enabled: true, Length: math.NaN(), Beam: math.Inf(1)}, 1, 200, 30},
		{"negative", &OceanWake{NodeID: "ship", Enabled: true, Length: -4, Beam: -2}, 1, 200, 30},
	}
	for _, c := range cases {
		p := normalizeOcean(&Ocean{Spectrum: &OceanSpectrum{}, Wake: c.wake}).GraphParams
		if p["wakeEnabled"] != c.enabled || p["hullLength"] != c.length || p["hullBeam"] != c.beam {
			t.Errorf("%s: wakeEnabled %v hullLength %v hullBeam %v, want %v %v %v", c.name, p["wakeEnabled"], p["hullLength"], p["hullBeam"], c.enabled, c.length, c.beam)
		}
	}
	// A node with no wake enabled is still bound, so the runner can read its pose once the wake is switched on by an update.
	nodes := normalizeOcean(&Ocean{Spectrum: &OceanSpectrum{}, Wake: &OceanWake{NodeID: "ship"}}).GraphNodes
	if nodes[OceanGraphHullNode] != "ship" {
		t.Fatalf("nodes %v", nodes)
	}
	if nodes := normalizeOcean(&Ocean{Spectrum: &OceanSpectrum{}, Wake: &OceanWake{Enabled: true}}).GraphNodes; nodes != nil {
		t.Fatalf("no node, no binding: %v", nodes)
	}
}

func TestOceanWakeIsIgnoredWithoutASpectrumAndGraphNodesAreReplaced(t *testing.T) {
	ocean := normalizeOcean(&Ocean{Wake: &OceanWake{NodeID: "ship", Enabled: true}, GraphNodes: map[string]string{"hull": "x"}})
	if ocean.GraphParams != nil || ocean.GraphNodes != nil {
		t.Fatalf("graph data without a spectrum: %v %v", ocean.GraphParams, ocean.GraphNodes)
	}
	replaced := normalizeOcean(&Ocean{Spectrum: &OceanSpectrum{}, GraphNodes: map[string]string{"hull": "forged", "extra": "y"}})
	if replaced.GraphNodes != nil {
		t.Fatalf("authored graph nodes survived: %v", replaced.GraphNodes)
	}
}

func TestOceanWakeDebugViewIsAnOptInDiagnosticThatSurvivesNormalization(t *testing.T) {
	plain := normalizeOcean(&Ocean{Spectrum: &OceanSpectrum{}, Wake: &OceanWake{NodeID: "ship", Enabled: true}})
	if plain.Wake.Debug {
		t.Fatal("the debug view is off by default")
	}
	wire, _ := json.Marshal(plain)
	if strings.Contains(string(wire), "debug") {
		t.Fatalf("an unset debug flag stays off the wire: %s", wire)
	}
	debug := normalizeOcean(&Ocean{Spectrum: &OceanSpectrum{}, Wake: &OceanWake{NodeID: "ship", Enabled: true, Debug: true}})
	if !debug.Wake.Debug {
		t.Fatal("debug lost")
	}
	wire, _ = json.Marshal(debug)
	var back struct{ Wake struct{ Debug bool } }
	_ = json.Unmarshal(wire, &back)
	if !back.Wake.Debug {
		t.Fatalf("debug missing from the wire: %s", wire)
	}
	// The debug view changes no graph parameter: it is a presentation choice.
	if !reflect.DeepEqual(plain.GraphParams, debug.GraphParams) {
		t.Fatalf("graph params differ: %v vs %v", plain.GraphParams, debug.GraphParams)
	}
}
