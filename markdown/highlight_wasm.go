//go:build js && wasm

package markdown

// highlightCodeImpl is a WASM stub that always returns ("", false).
//
// Full gotreesitter-based highlighting requires loading grammar blobs, which
// are too large to bundle into the WASM binary. The planned approach is
// on-demand HTTP loading: when a code block with language "go" is encountered,
// fetch /grammars/go.blob, deserialise it into a *gotreesitter.Language, then
// parse and highlight as the native implementation does. Until that
// infrastructure is in place, code blocks in WASM builds render as plain
// escaped text.
func highlightCodeImpl(language, source string) (string, bool) {
	return "", false
}
