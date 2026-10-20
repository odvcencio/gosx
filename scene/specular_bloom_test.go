package scene

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestBloomSourceRoundTripAndDefaultCompatibility(t *testing.T) {
	for _, source := range []BloomSource{"", BloomSourceColor, "unknown", BloomSourceSpecular} {
		effects := (PostFX{Effects: []PostEffect{Bloom{Source: source, Mode: "mip", Strength: .1}}}).sceneIR()
		raw, err := json.Marshal(effects[0])
		if err != nil {
			t.Fatal(err)
		}
		var got BloomIR
		if err = json.Unmarshal(raw, &got); err != nil {
			t.Fatal(err)
		}
		want := ""
		if source == BloomSourceSpecular {
			want = "specular"
		}
		if got.Source != want {
			t.Fatalf("source %q -> %s", source, raw)
		}
		again, _ := json.Marshal(got)
		if string(raw) != string(again) {
			t.Fatalf("unstable roundtrip %s -> %s", raw, again)
		}
		if want == "" && strings.Contains(string(raw), `"source"`) {
			t.Fatal("default adds wire payload")
		}
	}
}

func TestSelenaSpecularVariantsPreserveCoverageAndHoist(t *testing.T) {
	m, _, err := CompileSelenaMaterial([]byte(`material Plain { surface(g)->vec4 { return vec4f(9.0,8.0,7.0,0.2) } }`), SelenaMaterialOptions{SpecularMRT: true})
	if err != nil {
		t.Fatal(err)
	}
	if m.SpecularFragmentGLSL == "" || m.SpecularFragmentWGSL == "" {
		t.Fatal("missing alternate fragment artifacts")
	}
	if !strings.Contains(m.SpecularFragmentWGSL, "fragmentMainSpecular") {
		t.Fatal("missing MRT entrypoint")
	}
	if strings.Contains(m.FragmentGLSL, "layout(location = 1)") {
		t.Fatal("ordinary source changed")
	}
	props := m.legacyMaterial()
	if props["specularFragmentGLSL"] != strings.TrimSpace(m.SpecularFragmentGLSL) || props["specularFragmentWGSL"] != strings.TrimSpace(m.SpecularFragmentWGSL) {
		t.Fatal("legacy transport lost variants")
	}
	source := strings.Repeat(m.SpecularFragmentWGSL, 10)
	ir := SceneIR{Objects: []ObjectIR{{ID: "a", SpecularFragmentWGSL: source}, {ID: "b", SpecularFragmentWGSL: source}}}
	raw, err := json.Marshal(ir)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(raw), "fragmentMainSpecular") != 10 {
		t.Fatal("repeated specular shader was not hoisted")
	}
	var restored SceneIR
	if err = json.Unmarshal(raw, &restored); err != nil {
		t.Fatal(err)
	}
	if restored.Objects[0].SpecularFragmentWGSL != source || restored.Objects[1].SpecularFragmentWGSL != source {
		t.Fatal("hoisted variants did not inflate")
	}
}

func TestSelenaPrimitiveRemainsSolidAfterFallbackReplacement(t *testing.T) {
	compiled, _, err := CompileSelenaMaterial([]byte(`material Paint { surface(g)->color { return rgb(0.5,0.6,0.7) } }`), SelenaMaterialOptions{SpecularMRT: true})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name string
		wire *bool
		want bool
	}{{"default", nil, false}, {"solid", Bool(false), false}, {"wireframe", Bool(true), true}} {
		t.Run(tc.name, func(t *testing.T) {
			material := compiled
			// Replacing fallback properties must not turn an untextured
			// compiled surface into the browser's legacy wire primitive.
			material.StandardMaterial = StandardMaterial{Color: "#abcdef", Wireframe: tc.wire}
			props := Props{Graph: NewGraph(Mesh{ID: "painted-edge", Geometry: BoxGeometry{Width: 2, Height: .1, Depth: .1}, Material: material})}
			ir := props.SceneIR()
			if len(ir.Objects) != 1 {
				t.Fatalf("objects = %d", len(ir.Objects))
			}
			object := ir.Objects[0]
			if object.Kind != "box" || object.Wireframe == nil || *object.Wireframe != tc.want {
				t.Fatalf("primitive kind=%s wireframe=%v, want box/%v", object.Kind, object.Wireframe, tc.want)
			}
			if object.SpecularFragmentGLSL == "" {
				t.Fatal("compiled coverage-preserving MRT fragment lost")
			}
			legacy := props.LegacyProps()["scene"].(map[string]any)["objects"].([]map[string]any)[0]
			if legacy["wireframe"] != tc.want || material.legacyMaterial()["wireframe"] != tc.want {
				t.Fatalf("legacy solid/explicit-wire contract diverged: %#v", legacy["wireframe"])
			}
		})
	}
}
