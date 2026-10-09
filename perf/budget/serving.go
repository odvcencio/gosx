package budget

// Serving profiles identify supported live HTML encodings. Flush boundaries
// can change the response bytes; observed wire costs stay separate from pinned
// canonical normalization.
func knownServingHTMLCompressor(encoding, compressor string) bool {
	switch encoding {
	case "br":
		return compressor == "go-brotli-4"
	case "gzip":
		return compressor == "go-gzip-default" || compressor == "go-gzip-best"
	default:
		return false
	}
}
