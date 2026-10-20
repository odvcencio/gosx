package scene

import (
	"encoding/json"
	"math"
	"testing"
)

func TestDayCycleNightLightingContract(t *testing.T) {
	day := DayCycle{SunLightID: "sun", NightLightID: "night", NightIntensity: .28, NightEnvIntensity: .075}
	if err := day.Validate(); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(day)
	if err != nil {
		t.Fatal(err)
	}
	var wire map[string]any
	if err := json.Unmarshal(data, &wire); err != nil {
		t.Fatal(err)
	}
	if wire["nightLightId"] != "night" || wire["nightIntensity"] != .28 || wire["nightEnvIntensity"] != .075 {
		t.Fatalf("night lighting lost in transport: %s", data)
	}
	for _, value := range []float64{-1, 31, math.NaN(), math.Inf(1)} {
		invalid := day
		invalid.NightIntensity = value
		if invalid.Validate() == nil {
			t.Errorf("accepted night intensity %g", value)
		}
	}
	for _, value := range []float64{-1, 5, math.NaN(), math.Inf(1)} {
		invalid := day
		invalid.NightEnvIntensity = value
		if invalid.Validate() == nil {
			t.Errorf("accepted night ambient intensity %g", value)
		}
	}
	data, err = json.Marshal(DayCycle{SunLightID: "sun"})
	if err != nil {
		t.Fatal(err)
	}
	clear(wire)
	if err := json.Unmarshal(data, &wire); err != nil {
		t.Fatal(err)
	}
	if _, exists := wire["nightLightId"]; exists {
		t.Fatal("ordinary day cycles must keep night lighting opt-in")
	}
}
