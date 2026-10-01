//go:build !js || !wasm

package wasm

import "testing"

func TestWASMManifestIdentityMatchesContract(t *testing.T) {
	if got := ManifestIdentity(); got != wasmManifestIdentity {
		t.Fatalf("WASM manifest identity is stale: got %s, want %s", wasmManifestIdentity, got)
	}
}
