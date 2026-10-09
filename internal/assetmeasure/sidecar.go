package assetmeasure

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"errors"
	"io"

	"github.com/andybalholm/brotli"
)

// VerifySidecar bounds decoded output by the raw body and checks its SHA-256.
// It validates release representations without requiring canonical encoder pins.
func VerifySidecar(raw, encoded []byte, encoding string) error {
	input := bytes.NewReader(encoded)
	var decoded io.Reader
	var gz *gzip.Reader
	switch encoding {
	case "gzip":
		var err error
		gz, err = gzip.NewReader(input)
		if err != nil {
			return errors.New("invalid gzip sidecar")
		}
		defer gz.Close()
		gz.Multistream(false)
		decoded = gz
	case "br":
		decoded = brotli.NewReader(input)
	default:
		return errors.New("unsupported sidecar encoding")
	}
	digest := sha256.New()
	n, err := io.Copy(digest, io.LimitReader(decoded, int64(len(raw))+1))
	if err != nil {
		return errors.New("invalid sidecar stream")
	}
	if n != int64(len(raw)) || !bytes.Equal(digest.Sum(nil), hashBytes(raw)) {
		return errors.New("sidecar does not match raw SHA-256")
	}
	if input.Len() != 0 {
		return errors.New("sidecar contains trailing data")
	}
	return nil
}

func hashBytes(data []byte) []byte { sum := sha256.Sum256(data); return sum[:] }
