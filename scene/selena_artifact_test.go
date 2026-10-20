package scene

import (
	"m31labs.dev/selena"
	"reflect"
	"testing"
)

func TestMaterialFromSelenaMatchesCompiler(t *testing.T) {
	for _, source := range []string{selenaDefaultsSource, `material ColorPass kind post { surface(pixel) -> color { return rgb(0.2, 0.4, 0.6) } }`} {
		opts := SelenaMaterialOptions{Standard: StandardMaterial{Color: "#abcabc"}, Uniforms: map[string]any{"gain": float32(2)}, Targets: selena.AllTargets()}
		result, err := selena.Compile([]byte(source), selena.CompileOptions{Targets: selena.AllTargets()})
		if err != nil {
			t.Fatal(err)
		}
		compiled, _, err := CompileSelenaMaterial([]byte(source), opts)
		if err != nil {
			t.Fatal(err)
		}
		// A shipped bundle contains no AST or intermediate representation.
		artifacts := selena.Result{Layout: result.Layout, Artifacts: result.Artifacts}
		material, err := MaterialFromSelena(artifacts, opts)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(material, compiled) {
			t.Fatal("precompiled artifact changed host contract")
		}
		if _, err := MaterialFromSelena(selena.Result{Layout: result.Layout}, opts); err == nil {
			t.Fatal("missing browser artifacts accepted")
		}
	}
	if _, err := MaterialFromSelena(selena.Result{}, SelenaMaterialOptions{}); err == nil {
		t.Fatal("missing layout accepted")
	}
}
