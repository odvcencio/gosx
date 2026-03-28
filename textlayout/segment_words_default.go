//go:build !js || !wasm

package textlayout

func segmentWordRunStrings(text string) []string {
	return segmentWordRunStringsFallback(text)
}
