package route

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestMarshalEnginePropsHoistsJSXSceneShaderLib pins the shader-lib hoisting
// pass to the JSX authoring path.
//
// Scenes written as .gsx children — <Mesh>, <Material>, <Points> — never reach
// SceneIR's typed collections, so the typed hoisting pass cannot see them. They
// arrive at marshalEngineProps as plain maps. Before this pass was wired in,
// every repeated shader string shipped in full once per element: the m31labs.dev
// home page carried one 1,385-byte fragment shader twelve times over, and about
// 46 KB of the manifest was nothing but repeated shader text.
//
// The mechanism to prevent that already existed on both sides — scene.
// ApplyShaderLib on the server, the ref-inflating hydrate step in the browser —
// but nothing called the server half, so it sat dead while every JSX-authored
// scene paid full price. This test is the caller's proof of life.
func TestMarshalEnginePropsHoistsJSXSceneShaderLib(t *testing.T) {
	t.Parallel()

	// Above shaderLibThreshold (1024); repetition is what makes it hoistable.
	shared := strings.Repeat("// shared selena fragment\n", 60)
	if len(shared) < 1024 {
		t.Fatalf("fixture shader is %d bytes, want >= 1024 so it qualifies", len(shared))
	}

	props := map[string]any{
		"scene": map[string]any{
			"objects": []any{
				map[string]any{"name": "a", "customFragmentWGSL": shared},
				map[string]any{"name": "b", "customFragmentWGSL": shared},
			},
			"materials": []any{
				map[string]any{"name": "c", "customFragmentWGSL": shared},
			},
		},
	}

	raw := marshalEngineProps(props)
	if len(raw) == 0 {
		t.Fatal("marshalEngineProps returned nothing")
	}

	// Count the JSON-encoded form: the wire bytes carry the shader with its
	// newlines escaped, so the Go literal never appears verbatim.
	encoded, err := json.Marshal(shared)
	if err != nil {
		t.Fatalf("encode fixture: %v", err)
	}
	if got := strings.Count(string(raw), string(encoded)); got != 1 {
		t.Errorf("shader source appears %d times in the wire bytes, want 1", got)
	}

	var decoded struct {
		Scene struct {
			ShaderLib map[string]string `json:"shaderLib"`
			Objects   []map[string]any  `json:"objects"`
			Materials []map[string]any  `json:"materials"`
		} `json:"scene"`
	}
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("unmarshal engine props: %v", err)
	}

	if len(decoded.Scene.ShaderLib) != 1 {
		t.Fatalf("shaderLib has %d entries, want 1", len(decoded.Scene.ShaderLib))
	}
	var id string
	for k, v := range decoded.Scene.ShaderLib {
		id, _ = k, v
		if v != shared {
			t.Error("shaderLib entry does not hold the original source")
		}
		if !strings.HasPrefix(k, "sl:") {
			t.Errorf("shaderLib id %q lacks the sl: prefix the browser matches on", k)
		}
	}

	// Every element that carried the shader must now carry a ref to it, and
	// must not also carry the inline copy.
	nodes := append(append([]map[string]any{}, decoded.Scene.Objects...), decoded.Scene.Materials...)
	if len(nodes) != 3 {
		t.Fatalf("decoded %d elements, want 3", len(nodes))
	}
	for _, node := range nodes {
		name, _ := node["name"].(string)
		if _, still := node["customFragmentWGSL"]; still {
			t.Errorf("element %q kept the inline shader alongside the ref", name)
		}
		if node["customFragmentWGSLRef"] != id {
			t.Errorf("element %q ref = %v, want %q", name, node["customFragmentWGSLRef"], id)
		}
	}
}

// TestMarshalEnginePropsLeavesSingleUseShaderInline guards the other direction:
// a shader used once must stay where it is. Hoisting it would add a shaderLib
// map and an indirection without removing a single byte.
func TestMarshalEnginePropsLeavesSingleUseShaderInline(t *testing.T) {
	t.Parallel()

	only := strings.Repeat("// lone kernel\n", 100)
	props := map[string]any{
		"scene": map[string]any{
			"objects": []any{map[string]any{"name": "a", "customFragmentWGSL": only}},
		},
	}

	raw := marshalEngineProps(props)
	if strings.Contains(string(raw), "shaderLib") {
		t.Error("single-use shader was hoisted; want it left inline")
	}
	if !strings.Contains(string(raw), "customFragmentWGSL") {
		t.Error("single-use shader lost its inline field")
	}
}

// TestMarshalEnginePropsLeavesRawMessageSceneAlone documents why the pass skips
// a pre-marshaled scene. Those bytes come from SceneIR.marshalWire, which has
// already run the typed hoisting pass; decoding them here would cost a full
// parse of the largest value in the manifest to learn there is nothing to do.
func TestMarshalEnginePropsLeavesRawMessageSceneAlone(t *testing.T) {
	t.Parallel()

	scene := json.RawMessage(`{"objects":[{"name":"a"}]}`)
	raw := marshalEngineProps(map[string]any{"scene": scene})

	if !strings.Contains(string(raw), `"objects"`) {
		t.Fatalf("pre-marshaled scene did not survive: %s", raw)
	}
	if strings.Contains(string(raw), "shaderLib") {
		t.Error("pass touched a json.RawMessage scene")
	}
}
