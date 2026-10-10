package budget

import (
	"bytes"
	"compress/gzip"

	"github.com/andybalholm/brotli"
)

// encodeServingHTML builds single-write fixtures. Live response bytes do not
// have to match these encodings.
func encodeServingHTML(body []byte, encoding, compressor string) ([]byte, error) {
	var out bytes.Buffer
	switch {
	case encoding == "br" && compressor == "go-brotli-4":
		writer := brotli.NewWriterOptions(&out, brotli.WriterOptions{Quality: 4, LGWin: 0})
		if _, err := writer.Write(body); err != nil {
			return nil, measureFailure("stale-sidecar", "/encoding")
		}
		if err := writer.Close(); err != nil {
			return nil, measureFailure("stale-sidecar", "/encoding")
		}
	case encoding == "gzip" && (compressor == "go-gzip-default" || compressor == "go-gzip-best"):
		level := gzip.DefaultCompression
		if compressor == "go-gzip-best" {
			level = gzip.BestCompression
		}
		writer, _ := gzip.NewWriterLevel(&out, level)
		writer.Header.OS = 255
		if _, err := writer.Write(body); err != nil {
			return nil, measureFailure("stale-sidecar", "/encoding")
		}
		if err := writer.Close(); err != nil {
			return nil, measureFailure("stale-sidecar", "/encoding")
		}
	default:
		return nil, measureFailure("invalid-input", "/servingCompressor")
	}
	return out.Bytes(), nil
}
