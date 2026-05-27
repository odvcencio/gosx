// Confirm Discover correctly detects (and rejects) the obsolete
// `//gosx:engine surface=wasm` escape-hatch annotation after the
// buildsurface deletion (ADR 0005).

package surface

import (
	"testing"
)

func TestHasWASMEscapeHatchDetectsCanonicalForm(t *testing.T) {
	src := []byte(`// header
//gosx:engine surface=wasm
//gosx:caps canvas
`)
	if !hasWASMEscapeHatch(src) {
		t.Errorf("expected escape-hatch annotation to be detected")
	}
}

func TestHasWASMEscapeHatchDetectsSpaceForm(t *testing.T) {
	src := []byte(`//gosx:engine surface wasm`)
	if !hasWASMEscapeHatch(src) {
		t.Errorf("expected space-separated escape-hatch annotation to be detected")
	}
}

func TestHasWASMEscapeHatchIgnoresPlainEngineAnnotation(t *testing.T) {
	src := []byte(`//gosx:engine surface
//gosx:caps canvas
func Foo() {}
`)
	if hasWASMEscapeHatch(src) {
		t.Errorf("plain //gosx:engine surface should NOT trigger escape-hatch detection")
	}
}

func TestHasWASMEscapeHatchIgnoresMissingAnnotation(t *testing.T) {
	src := []byte(`// header only
func Foo() {}
`)
	if hasWASMEscapeHatch(src) {
		t.Errorf("absence of annotation should NOT trigger escape-hatch detection")
	}
}
