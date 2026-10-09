package budget

import (
	"bytes"
	"compress/gzip"

	"github.com/andybalholm/brotli"
)

// Declared live HTML encoders are separate from quality-11 normalization. They
// verify the wire representation of a fresh body after its content is checked.
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
		// Fresh production responses use the middleware's default level;
		// explicitly declared best-compression outputs keep their own profile.
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
