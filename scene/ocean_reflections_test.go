package scene

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestOceanReflectionContract(t *testing.T) {
	for _, mode := range []string{"ssr", "planar", "ssr+planar"} {
		props := Props{Environment: Environment{Ocean: &Ocean{Reflections: &OceanReflections{Mode: " " + strings.ToUpper(mode) + " ", Resolution: 0.25, Strength: 0.8}}}}
		for _, env := range []any{props.SceneIR().Environment, props.CanonicalIR().Environment} {
			wire, err := json.Marshal(env)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(wire), `"reflections":{"mode":"`+mode+`","resolution":0.25,"strength":0.8}`) {
				t.Fatalf("reflection wire: %s", wire)
			}
		}
	}
	for _, r := range []*OceanReflections{nil, {}, {Mode: "invalid"}} {
		if normalizeOceanReflections(r) != nil {
			t.Fatalf("off reflection: %#v", r)
		}
	}
	r := normalizeOceanReflections(&OceanReflections{Mode: "ssr", Resolution: 2, Strength: 3})
	if r.Resolution != 1 || r.Strength != 1 {
		t.Fatalf("clamps: %#v", r)
	}
	r = normalizeOceanReflections(&OceanReflections{Mode: "planar"})
	wire, _ := json.Marshal(r)
	if string(wire) != `{"mode":"planar"}` {
		t.Fatalf("Go must leave defaults to browser: %s", wire)
	}
	wire, _ = json.Marshal(normalizeOcean(&Ocean{}))
	if string(wire) != `{}` {
		t.Fatalf("legacy wire changed: %s", wire)
	}
}
