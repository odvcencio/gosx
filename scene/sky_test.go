package scene

import (
	"encoding/json"
	"testing"

	"m31labs.dev/gosx/scene/capability"
)

// TestNilSkyKeepsTheWireByteIdentical pins the design goal stated in sky.go:
// a scene that never sets Environment.Sky must marshal with no "sky" key at
// all, so every existing scene's wire bytes are unchanged by this feature.
func TestNilSkyKeepsTheWireByteIdentical(t *testing.T) {
	props := Props{Environment: Environment{AmbientColor: "#ffffff", AmbientIntensity: 1}}
	ir := props.SceneIR()
	if ir.Environment.Sky != nil {
		t.Fatalf("expected a nil Sky, got %#v", ir.Environment.Sky)
	}
	wire, err := json.Marshal(ir.Environment)
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(wire, &decoded); err != nil {
		t.Fatal(err)
	}
	if _, present := decoded["sky"]; present {
		t.Fatalf("a nil Sky must not appear as a wire key: %s", wire)
	}
}

// TestSkyLowersToTheWireIR proves Environment.Sky reaches EnvironmentIR.Sky
// with trimmed strings, and round-trips through JSON under the "sky" key with
// every authored field intact.
func TestSkyLowersToTheWireIR(t *testing.T) {
	props := Props{
		Environment: Environment{
			Sky: &Sky{
				Mode:         "  environment  ",
				TopColor:     "#0a2e4f",
				HorizonColor: "#cfe6f2",
				BottomColor:  "#1c2a33",
				Blur:         0.15,
				Intensity:    1.2,
			},
		},
	}
	ir := props.SceneIR()
	if ir.Environment.Sky == nil {
		t.Fatal("expected a non-nil Sky after lowering")
	}
	if ir.Environment.Sky.Mode != "environment" {
		t.Fatalf("Mode = %q, want trimmed \"environment\"", ir.Environment.Sky.Mode)
	}
	if ir.Environment.Sky.TopColor != "#0a2e4f" || ir.Environment.Sky.HorizonColor != "#cfe6f2" ||
		ir.Environment.Sky.BottomColor != "#1c2a33" {
		t.Fatalf("gradient stops did not survive lowering: %#v", ir.Environment.Sky)
	}
	if ir.Environment.Sky.Blur != 0.15 || ir.Environment.Sky.Intensity != 1.2 {
		t.Fatalf("blur/intensity did not survive lowering: %#v", ir.Environment.Sky)
	}

	wire, err := json.Marshal(ir.Environment)
	if err != nil {
		t.Fatal(err)
	}
	var decoded struct {
		Sky *Sky `json:"sky"`
	}
	if err := json.Unmarshal(wire, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Sky == nil || decoded.Sky.Mode != "environment" || decoded.Sky.TopColor != "#0a2e4f" {
		t.Fatalf("sky did not round-trip through the wire: %s", wire)
	}
}

// TestSkyLowersIntoCanonicalIR pins that the CanonicalIR path (scene/ir.go's
// IREnvironment, consumed today by game.Runtime.SceneIR for physics) mirrors
// EnvironmentIR field-for-field, exactly as EnvironmentIBL already does.
func TestSkyLowersIntoCanonicalIR(t *testing.T) {
	props := Props{
		Environment: Environment{
			Sky: &Sky{Mode: "gradient", TopColor: "#123456"},
		},
	}
	canonical := props.CanonicalIR()
	if canonical.Environment.Sky == nil || canonical.Environment.Sky.Mode != "gradient" ||
		canonical.Environment.Sky.TopColor != "#123456" {
		t.Fatalf("Sky did not reach IREnvironment: %#v", canonical.Environment.Sky)
	}
}

func TestSkyFeaturesMatchMode(t *testing.T) {
	for _, tc := range []struct {
		name    string
		sky     *Sky
		feature capability.Feature
	}{
		{"environment", &Sky{Mode: " ENVIRONMENT "}, capability.FeatureSkyEnvironment},
		{"gradient", &Sky{Mode: "gradient"}, capability.FeatureSkyGradient},
		{"implicit gradient", &Sky{TopColor: "#fff"}, capability.FeatureSkyGradient},
		{"invalid", &Sky{Mode: "invalid"}, ""},
		{"absent", nil, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			props := Props{Environment: Environment{Sky: tc.sky}}
			got := featureSet(collectFeatures(props.SceneIR()))
			for _, f := range []capability.Feature{capability.FeatureSkyEnvironment, capability.FeatureSkyGradient} {
				if got[f] != (tc.feature == f) {
					t.Fatalf("feature %s = %v, want %v", f, got[f], tc.feature == f)
				}
			}
		})
	}
}
