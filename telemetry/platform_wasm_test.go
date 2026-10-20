//go:build js && wasm

package telemetry

import (
	"errors"
	"testing"

	"m31labs.dev/gosx/server"
)

func TestEnableWASMRejectsNativeOwnership(t *testing.T) {
	t.Setenv("GOSX_TELEMETRY", "on")
	if handle, err := Enable(server.New(), Options{}); handle != nil || !errors.Is(err, ErrUnsupportedPlatform) {
		t.Fatal(handle, err)
	}
	if handle, err := Enable(server.New(), Options{Disabled: true}); err != nil || handle.Enabled() {
		t.Fatal(err)
	}
}
