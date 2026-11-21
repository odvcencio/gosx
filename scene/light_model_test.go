package scene

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestEnvironmentLightModelReachesTheRenderer(t *testing.T) {
	p := Props{Environment: Environment{LightModel: " physical ", AmbientIntensity: .1}}
	env, ok := p.SceneIR().legacyProps()["environment"].(map[string]any)
	if !ok {
		t.Fatalf("environment missing from the lowered props")
	}
	if env["lightModel"] != "physical" {
		t.Fatalf(`environment.lightModel = %v, want "physical"`, env["lightModel"])
	}
	raw, err := json.Marshal(Props{Environment: Environment{AmbientIntensity: .1}}.SceneIR())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "lightModel") {
		t.Fatalf("an unset light model must not be written: %s", raw)
	}
}
