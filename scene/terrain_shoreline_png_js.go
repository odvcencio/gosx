//go:build js && wasm

package scene

// encodeShorelinePNG writes the texels as an uncompressed PNG, so the browser
// engine does not link the PNG codec.
func encodeShorelinePNG(rgb []byte, n int) ([]byte, error) {
	return encodeStoredPNG(rgb, n, n), nil
}
