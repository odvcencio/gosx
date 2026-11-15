//go:build !js || !wasm

package scene

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

// TestSceneJSWasmImportGraphHasNoPNGCodec keeps the PNG codec out of browser
// engines: a js/wasm build of scene must not link image, image/color or
// image/png.
func TestSceneJSWasmImportGraphHasNoPNGCodec(t *testing.T) {
	cmd := exec.Command("go", "list", "-deps", ".")
	cmd.Env = append(os.Environ(), "GOOS=js", "GOARCH=wasm", "GOWORK=off", "CGO_ENABLED=0")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("list js/wasm dependencies: %v\n%s", err, out)
	}
	for _, dep := range strings.Fields(string(out)) {
		switch dep {
		case "image", "image/color", "image/png":
			t.Errorf("scene links %s into js/wasm builds; keep PNG encoding and decoding in !js || !wasm files", dep)
		}
	}
}
