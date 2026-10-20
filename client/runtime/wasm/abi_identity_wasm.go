//go:build js && wasm

package wasm

// ManifestIdentity returns the host-verified contract digest without hashing
// the fixed ABI descriptor each time the browser requests a handshake.
func ManifestIdentity() string {
	return wasmManifestIdentity
}
