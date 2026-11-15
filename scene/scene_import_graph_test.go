//go:build !js || !wasm

package scene

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

// TestSceneJSWasmImportGraphHasNoPNGCodec keeps the PNG codec out of browser
// engines: a js/wasm build of scene must not link image, image/color, image/png,
// compress/zlib, compress/flate or hash/adler32.
func TestSceneJSWasmImportGraphHasNoPNGCodec(t *testing.T) {
	cmd := exec.Command("go", "list", "-deps", ".")
	cmd.Env = append(os.Environ(), "GOOS=js", "GOARCH=wasm", "GOWORK=off", "CGO_ENABLED=0")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("list js/wasm dependencies: %v\n%s", err, out)
	}
	for _, dep := range strings.Fields(string(out)) {
		switch dep {
		case "image", "image/color", "image/png", "compress/zlib", "compress/flate", "hash/adler32":
			t.Errorf("scene links %s into js/wasm builds; keep PNG and zlib encoding and decoding in !js || !wasm files", dep)
		}
	}
}
