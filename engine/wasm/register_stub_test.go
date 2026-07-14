//go:build !js || !wasm

package wasm

import (
	"errors"
	"testing"
)

func TestRegisterNativeStub(t *testing.T) {
	if err := Register("", func(Context) (Handle, error) { return nil, nil }); err == nil {
		t.Fatal("expected an empty component to fail")
	}
	if err := Register("Fixture", nil); err == nil {
		t.Fatal("expected a nil factory to fail")
	}
	if err := Register("Fixture", func(Context) (Handle, error) { return nil, nil }); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("expected ErrUnsupported, got %v", err)
	}
}
