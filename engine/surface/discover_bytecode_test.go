// Slice X.D tests: confirm Discover routes annotation-free surfaces
// through the bytecode lowering path and surface=wasm surfaces through
// the legacy WASM path, with escape-hatch policy enforcement.

package surface

import (
	"testing"
)

func TestScanSurfaceAnnotationDefaultsToBytecode(t *testing.T) {
	src := []byte(`//gosx:engine surface
//gosx:caps canvas
func Foo() {}
`)
	got := scanSurfaceAnnotation(src)
	if got.backend != backendBytecode {
		t.Errorf("backend = %v, want bytecode", got.backend)
	}
	if got.line != 1 {
		t.Errorf("line = %d, want 1", got.line)
	}
}

func TestScanSurfaceAnnotationDetectsWASM(t *testing.T) {
	src := []byte(`// header
//gosx:engine surface=wasm
//gosx:caps canvas
`)
	got := scanSurfaceAnnotation(src)
	if got.backend != backendWASM {
		t.Errorf("backend = %v, want WASM", got.backend)
	}
	if got.line != 2 {
		t.Errorf("line = %d, want 2", got.line)
	}
}

func TestScanSurfaceAnnotationDetectsSpaceWASM(t *testing.T) {
	// Tolerate `//gosx:engine surface wasm` (space-separated).
	src := []byte(`//gosx:engine surface wasm`)
	got := scanSurfaceAnnotation(src)
	if got.backend != backendWASM {
		t.Errorf("backend = %v, want WASM", got.backend)
	}
}

func TestScanSurfaceAnnotationMissingAnnotation(t *testing.T) {
	src := []byte(`// header only
func Foo() {}
`)
	got := scanSurfaceAnnotation(src)
	if got.backend != backendBytecode {
		t.Errorf("backend = %v, want bytecode (default)", got.backend)
	}
	if got.line != 0 {
		t.Errorf("line = %d, want 0 (no annotation)", got.line)
	}
}
