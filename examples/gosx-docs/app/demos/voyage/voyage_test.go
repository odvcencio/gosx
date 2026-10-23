package docs

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"reflect"
	"strings"
	"testing"

	"m31labs.dev/gosx/scene"
	"m31labs.dev/gosx/vessel"
)

func TestVoyageApparentWaterSpeedFormula(t *testing.T) {
	u := vessel.Expedition().ServiceSpeed
	want := map[int]float64{1: u, 10: 2 * u, 60: u * (1 + math.Log10(60)), 360: u * (1 + math.Log10(360)), 1000: 4 * u}
	last := 0.0
	for _, speed := range voyageSpeeds {
		got := VoyageApparentWaterSpeed(speed)
		if math.Abs(got-want[speed]) > 1e-9 {
			t.Errorf("%dx: apparent speed %.3f, want %.3f", speed, got, want[speed])
		}
		if got < last {
			t.Errorf("%dx slower than the speed before it", speed)
		}
		last = got
	}
	if VoyageApparentWaterSpeed(100000) != 4*u || VoyageApparentWaterSpeed(0) != u {
		t.Error("the apparent speed must be capped at four ship speeds and floored at one")
	}
}

func TestVoyageOceanKeepsWavesNaturalAndDriftsTheWater(t *testing.T) {
	for _, state := range voyageStates {
		for _, speed := range voyageSpeeds {
			ocean := voyageOcean(state, speed)
			want := scene.OceanPreset(state)
			want.Seed = 7
			if !reflect.DeepEqual(*ocean.Spectrum, want) {
				t.Errorf("%s %dx: spectrum %+v, want the %s preset", state, speed, *ocean.Spectrum, state)
			}
			p := ocean.Presentation
			if p.TimeScale != 1 {
				t.Errorf("%s %dx: TimeScale %v; waves must keep natural time at every game speed", state, speed, p.TimeScale)
			}
			if p.WaterDriftX != 0 || math.Abs(p.WaterDriftZ-VoyageApparentWaterSpeed(speed)) > 1e-9 {
				t.Errorf("%s %dx: drift (%v, %v)", state, speed, p.WaterDriftX, p.WaterDriftZ)
			}
		}
	}
}

func TestVoyagePeriodsCoverEverySeaStateAndSpeedAndReplaceTheEnvironment(t *testing.T) {
	config := VoyageProgram("quarter", "moderate-1x").Presentation
	if len(config.Periods) != len(voyageStates)*len(voyageSpeeds) {
		t.Fatalf("%d periods", len(config.Periods))
	}
	for _, period := range config.Periods {
		state, speed := voyageParsePeriod(period.ID)
		if voyagePeriodID(state, speed) != period.ID {
			t.Fatalf("period id %q does not round trip", period.ID)
		}
		if len(period.Commands) != 1 || period.Commands[0].Kind != scene.CommandSetEnvironment {
			t.Fatalf("%s: commands %+v", period.ID, period.Commands)
		}
		wire, _ := json.Marshal(period.Commands[0])
		var decoded struct {
			Data struct {
				Environment struct {
					Ocean struct {
						Spectrum     struct{ WindSpeed float64 }
						SwellWaves   []json.RawMessage
						Presentation struct{ TimeScale, WaterDriftZ float64 }
					}
				}
			}
		}
		if err := json.Unmarshal(wire, &decoded); err != nil {
			t.Fatal(err)
		}
		ocean := decoded.Data.Environment.Ocean
		if ocean.Spectrum.WindSpeed != scene.OceanPreset(state).WindSpeed || len(ocean.SwellWaves) == 0 {
			t.Errorf("%s: the replacement environment lacks its spectrum or swell waves: %s", period.ID, wire[:200])
		}
		if ocean.Presentation.TimeScale != 1 || math.Abs(ocean.Presentation.WaterDriftZ-VoyageApparentWaterSpeed(speed)) > 1e-9 {
			t.Errorf("%s: presentation %+v", period.ID, ocean.Presentation)
		}
	}
	if state, speed := voyageParsePeriod("nonsense"); state != "moderate" || speed != 1 {
		t.Errorf("unknown period resolves to %s %dx", state, speed)
	}
}

func TestVoyageShipIsExpeditionSizeAndTheVesselNamesIt(t *testing.T) {
	props := voyageProps("quarter", "calm-1x")
	hull := vessel.Expedition()
	if props.Vessel == nil || props.Vessel.NodeID != "ship" || props.Vessel.Length != hull.Length || props.Vessel.Beam != hull.Beam || props.Vessel.Draft != hull.Draft {
		t.Fatalf("vessel %+v does not describe the expedition hull", props.Vessel)
	}
	ir := props.SceneIR()
	wire, _ := json.Marshal(ir)
	for _, id := range []string{"ship/hull", "ship/bow", "ship/deck", "ship/deck-upper", "ship/bridge", "ship/funnel"} {
		if !strings.Contains(string(wire), `"`+id+`"`) {
			t.Errorf("scene has no %s", id)
		}
	}
	for _, node := range voyageShip() {
		mesh := node.(scene.Mesh)
		if mesh.Position != (scene.Vector3{}) {
			t.Errorf("%s carries a position, which the vessel runtime resets: bake it into the vertices", mesh.ID)
		}
		if !strings.HasPrefix(mesh.ID, "ship/") {
			t.Errorf("%s is not a ship part", mesh.ID)
		}
	}
}

func TestVoyageIsPureGoWithAPhysicalSkyAndNoPageScript(t *testing.T) {
	props := VoyageProgram("waterline", "storm-1000x")
	sky := props.Environment.Sky
	if sky == nil || sky.Mode != "physical" {
		t.Fatal("the voyage needs a physical sky")
	}
	wire, err := json.Marshal(props.SceneIR())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(wire), "<script") {
		t.Fatal("scene carries script text")
	}
	for _, file := range []string{"page.gsx", "page.server.go", "voyage.go"} {
		source, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(source), "<script") {
			t.Errorf("%s: an app never needs page JavaScript", file)
		}
	}
	if props.Presentation.InitialPeriod != "storm-1000x" || props.Presentation.InitialView != "waterline" {
		t.Errorf("presentation starts at %s/%s", props.Presentation.InitialView, props.Presentation.InitialPeriod)
	}
}

func TestVoyageDrawsAWakeBehindTheExpeditionShip(t *testing.T) {
	hull := vessel.Expedition()
	for _, state := range voyageStates {
		for _, speed := range voyageSpeeds {
			props := VoyageProgram("quarter", state+"-"+fmt.Sprint(speed)+"x")
			ocean := props.Environment.Ocean
			if ocean.Wake == nil || !ocean.Wake.Enabled {
				t.Fatalf("%s %dx: no wake", state, speed)
			}
			if props.Vessel == nil || ocean.Wake.NodeID != props.Vessel.NodeID || ocean.Wake.NodeID != "ship" {
				t.Errorf("%s %dx: the wake follows %q, the vessel is %+v", state, speed, ocean.Wake.NodeID, props.Vessel)
			}
			if ocean.Wake.Length != hull.Length || ocean.Wake.Beam != hull.Beam {
				t.Errorf("%s %dx: wake hull %v x %v, want the expedition hull %v x %v", state, speed, ocean.Wake.Length, ocean.Wake.Beam, hull.Length, hull.Beam)
			}
		}
	}
}
