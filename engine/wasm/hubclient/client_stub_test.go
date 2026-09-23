//go:build !js || !wasm

package hubclient

import (
	"errors"
	"testing"
)

func TestNativeStubReportsUnavailable(t *testing.T) {
	client, err := Dial("/ws/ROOM", Options{}, Handler{})
	if client != nil || !errors.Is(err, ErrUnavailable) {
		t.Fatalf("dial = %v %v", client, err)
	}
}
