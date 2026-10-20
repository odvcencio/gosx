//go:build !js || !wasm

package scene

import (
	"bytes"
	"encoding/json"
	"sync"
	"testing"

	"m31labs.dev/selena"
)

func TestSelenaCacheMatchesFreshCompileAndIsolatesResults(t *testing.T) {
	source := []byte(selenaDefaultsSource)
	opts := SelenaMaterialOptions{Uniforms: map[string]any{"gain": float32(2)}}
	cached, layout, err := CompileSelenaMaterial(source, opts)
	if err != nil {
		t.Fatal(err)
	}
	result, err := selena.Compile(source, selena.CompileOptions{Targets: selenaMaterialTargets(opts)})
	if err != nil {
		t.Fatal(err)
	}
	fresh, err := selenaCustomMaterial(result, opts)
	if err != nil {
		t.Fatal(err)
	}
	fresh.VertexWGSL = selenaPointsWGSLRuntimeFrameLayout(fresh.VertexWGSL)
	fresh.FragmentWGSL = selenaPointsWGSLRuntimeFrameLayout(fresh.FragmentWGSL)
	a, _ := json.Marshal(cached)
	b, _ := json.Marshal(fresh)
	if !bytes.Equal(a, b) {
		t.Fatal("cached material wire output changed")
	}
	expected := layout.UniformBlock.Defaults[0].Values[0]
	layout.UniformBlock.Defaults[0].Values[0] = 99
	cached.Uniforms["gain"] = float32(99)
	opts.Uniforms["gain"] = float32(3)
	next, nextLayout, err := CompileSelenaMaterial(source, opts)
	if err != nil {
		t.Fatal(err)
	}
	if next.Uniforms["gain"] != float32(3) || nextLayout.UniformBlock.Defaults[0].Values[0] != expected {
		t.Fatal("cached results or per-call overrides leaked")
	}
}

func TestSelenaCacheConcurrent(t *testing.T) {
	source := []byte(selenaDefaultsSource)
	if _, _, err := CompileSelenaMaterial(source, SelenaMaterialOptions{}); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 5; j++ {
				if _, _, err := CompileSelenaMaterial(source, SelenaMaterialOptions{}); err != nil {
					t.Error(err)
				}
			}
		}()
	}
	wg.Wait()
}
