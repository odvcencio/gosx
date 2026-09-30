package scene

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestSkyCloudContract(t *testing.T) {
	props := Props{Environment: Environment{Sky: &Sky{Mode: "physical", Clouds: &SkyClouds{Coverage: 0.4, Altitude: 1800, Scale: 2200, Speed: 7, Direction: 30, Opacity: 0.7}}}}
	for _, env := range []any{props.SceneIR().Environment, props.CanonicalIR().Environment} {
		wire, err := json.Marshal(env)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(wire), `"clouds":{"coverage":0.4,"altitude":1800,"scale":2200,"speed":7,"direction":30,"opacity":0.7}`) {
			t.Fatalf("wire: %s", wire)
		}
	}
	cloud := normalizeSkyClouds(&SkyClouds{Coverage: 2, Altitude: 99, Scale: 99999, Speed: 200, Direction: -90, Opacity: 2})
	if cloud.Coverage != 1 || cloud.Altitude != 100 || cloud.Scale != 20000 || cloud.Speed != 100 || cloud.Direction != 270 || cloud.Opacity != 1 {
		t.Fatalf("clamps: %#v", cloud)
	}
	wire, _ := json.Marshal(normalizeSkyClouds(&SkyClouds{}))
	if string(wire) != `{"coverage":0}` {
		t.Fatalf("unset defaults: %s", wire)
	}
	if normalizeSkyClouds(nil) != nil {
		t.Fatal("nil clouds changed")
	}
	wire, _ = json.Marshal(normalizeSky(&Sky{Mode: "gradient", TopColor: "#123456"}))
	if strings.Contains(string(wire), "clouds") {
		t.Fatalf("old scene changed: %s", wire)
	}
}
