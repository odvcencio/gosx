package scene

import (
	"encoding/json"
	"math"
	"testing"
)

func TestOceanPresentationWireAndDefaults(t *testing.T) {
	// Absent: no field on the wire.
	wire, err := json.Marshal(normalizeOcean(&Ocean{Level: 1}))
	if err != nil {
		t.Fatal(err)
	}
	if want := `{"level":1}`; string(wire) != want {
		t.Fatalf("an ocean without Presentation changed its wire: %s", wire)
	}
	// Present but empty: zero is the default marker, so the object is empty.
	wire, _ = json.Marshal(normalizeOcean(&Ocean{Presentation: &OceanPresentation{}}))
	if want := `{"presentation":{}}`; string(wire) != want {
		t.Fatalf("empty presentation wire = %s", wire)
	}
	// The spec's JSON names.
	wire, _ = json.Marshal(normalizeOcean(&Ocean{Presentation: &OceanPresentation{TimeScale: 2, WaterDriftX: 1.5, WaterDriftZ: -3, TransitionSeconds: 8}}))
	if want := `{"presentation":{"timeScale":2,"waterDriftX":1.5,"waterDriftZ":-3,"transitionSeconds":8}}`; string(wire) != want {
		t.Fatalf("presentation wire = %s", wire)
	}
}

func TestOceanPresentationClampsAndRejectsNonFinite(t *testing.T) {
	got := normalizeOcean(&Ocean{Presentation: &OceanPresentation{TimeScale: 99, WaterDriftX: 1e6, WaterDriftZ: -1e6, TransitionSeconds: 1e9}}).Presentation
	if got.TimeScale != 4 || got.WaterDriftX != 100 || got.WaterDriftZ != -100 || got.TransitionSeconds != 600 {
		t.Fatalf("clamped: %+v", got)
	}
	got = normalizeOcean(&Ocean{Presentation: &OceanPresentation{TimeScale: -2, WaterDriftX: math.NaN(), WaterDriftZ: math.Inf(1), TransitionSeconds: 0.001}}).Presentation
	if got.TimeScale != 0 || got.WaterDriftX != 0 || got.WaterDriftZ != 0 || got.TransitionSeconds != 0.1 {
		t.Fatalf("lower and non-finite: %+v", got)
	}
}

func TestOceanPresentationRidesSceneIR(t *testing.T) {
	props := Props{Environment: Environment{Ocean: &Ocean{Spectrum: &OceanSpectrum{WindSpeed: 9}, Presentation: &OceanPresentation{TimeScale: 1, WaterDriftZ: 12, TransitionSeconds: 30}}}}
	wire, err := json.Marshal(props.SceneIR().Environment)
	if err != nil {
		t.Fatal(err)
	}
	var decoded struct {
		Ocean struct {
			Presentation OceanPresentation `json:"presentation"`
		} `json:"ocean"`
	}
	if err := json.Unmarshal(wire, &decoded); err != nil {
		t.Fatal(err)
	}
	if p := decoded.Ocean.Presentation; p.TimeScale != 1 || p.WaterDriftZ != 12 || p.TransitionSeconds != 30 {
		t.Fatalf("presentation lost in SceneIR: %s", wire)
	}
}
