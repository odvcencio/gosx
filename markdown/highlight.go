package markdown

// highlightCode attempts to syntax-highlight the given source code using
// gotreesitter. It returns the highlighted HTML and true on success, or
// ("", false) if the language is unknown or highlighting is unavailable.
//
// The implementation is platform-specific: native builds use gotreesitter
// grammars directly, while WASM builds stub out to a no-op until on-demand
// grammar blob loading is implemented.
func highlightCode(language string, source string) (string, bool) {
	return highlightCodeImpl(language, source)
}
