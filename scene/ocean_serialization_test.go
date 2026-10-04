package scene

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestOceanSurvivesPropsSerialization(t *testing.T) {
	for _, tc := range []struct {
		name        string
		environment Environment
	}{
		{"ocean-only", Environment{Ocean: &Ocean{Level: 2, DeepColor: " #123456 "}}},
		{"with-lighting", Environment{AmbientColor: "#ffffff", Ocean: &Ocean{Level: 2, DeepColor: " #123456 "}}},
		{"default-ocean", Environment{Ocean: &Ocean{}}},
		{"absent", Environment{AmbientColor: "#ffffff"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			props := Props{Environment: tc.environment}
			for name, value := range map[string]any{
				"marshal": props, "legacy": props.LegacyProps(), "spread": props.GoSXSpreadProps(),
			} {
				t.Run(name, func(t *testing.T) {
					wire, err := json.Marshal(value)
					if err != nil {
						t.Fatal(err)
					}
					var got struct {
						Scene struct {
							Environment map[string]json.RawMessage `json:"environment"`
						} `json:"scene"`
					}
					if err := json.Unmarshal(wire, &got); err != nil {
						t.Fatal(err)
					}
					ocean, present := got.Scene.Environment["ocean"]
					if present != (tc.environment.Ocean != nil) {
						t.Fatalf("ocean presence = %v: %s", present, wire)
					}
					if !present {
						return
					}
					var decoded Ocean
					if err := json.Unmarshal(ocean, &decoded); err != nil {
						t.Fatal(err)
					}
					if want := normalizeOcean(tc.environment.Ocean); !reflect.DeepEqual(&decoded, want) {
						t.Fatalf("ocean = %#v, want %#v", &decoded, want)
					}
				})
			}
		})
	}
}

func TestOceanRemovalCommandsEmitNull(t *testing.T) {
	for name, commands := range map[string][]Command{
		"typed-scene":     {SetSceneEnvironmentCommand(EnvironmentIR{})},
		"typed-canonical": {SetIREnvironmentCommand(IREnvironment{})},
		"diff-scene":      DiffCommands(SceneIR{Environment: EnvironmentIR{Ocean: &Ocean{}}}, SceneIR{}),
		"diff-canonical":  DiffIRCommands(IR{Environment: IREnvironment{Ocean: &Ocean{}}}, IR{}),
	} {
		t.Run(name, func(t *testing.T) {
			wire, err := json.Marshal(commands)
			if err != nil {
				t.Fatal(err)
			}
			var got []struct {
				Data struct {
					Environment map[string]json.RawMessage `json:"environment"`
				} `json:"data"`
			}
			if err := json.Unmarshal(wire, &got); err != nil {
				t.Fatal(err)
			}
			if len(got) != 1 || string(got[0].Data.Environment["ocean"]) != "null" {
				t.Fatalf("removal missing: %s", wire)
			}
		})
	}
}
