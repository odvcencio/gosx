//go:build js && wasm

package scene

import (
	"errors"
	"testing"
)

// js/wasm builds do not link the Selena compiler, so every compile function
// says so instead of compiling.
func TestSelenaCompileUnavailableInBrowserBuilds(t *testing.T) {
	source := []byte(`material Plain { surface(g) -> color { return rgb(0.5, 0.5, 0.5) } }`)
	for name, compile := range map[string]func() error{
		"CompileSelenaMaterial": func() error { _, _, err := CompileSelenaMaterial(source, SelenaMaterialOptions{}); return err },
		"CompileSelenaBundle":   func() error { _, err := CompileSelenaBundle(source); return err },
		"CompileSelenaPoints":   func() error { _, _, err := CompileSelenaPoints(source, SelenaMaterialOptions{}); return err },
		"CompileSelenaParticleRender": func() error {
			_, _, err := CompileSelenaParticleRender(source, SelenaMaterialOptions{})
			return err
		},
		"CompileSelenaPost": func() error { _, _, err := CompileSelenaPost(source, SelenaMaterialOptions{}); return err },
	} {
		if err := compile(); !errors.Is(err, ErrSelenaCompileUnavailable) {
			t.Errorf("%s error = %v, want ErrSelenaCompileUnavailable", name, err)
		}
	}
}
