package assetmeasure

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"time"

	"github.com/andybalholm/brotli"
)

type Sizes struct {
	Raw    int64  `json:"raw"`
	Gzip   int64  `json:"gzip"`
	Brotli int64  `json:"brotli"`
	SHA256 string `json:"sha256"`
}

// Measure normalizes raw bytes under the exact root-module pins. Release
// sidecars never replace these measurements, even when their encoding is smaller.
func Measure(data []byte, pin CompressorPin) (Sizes, error) {
	if err := validatePin(pin); err != nil {
		return Sizes{}, err
	}
	gz, err := gzipCanonical(data, pin.GzipLevel)
	if err != nil {
		return Sizes{}, err
	}
	br, err := brotliCanonical(data, pin.BrotliQuality, pin.BrotliWindow)
	if err != nil {
		return Sizes{}, err
	}
	sum := sha256.Sum256(data)
	return Sizes{Raw: int64(len(data)), Gzip: int64(len(gz)), Brotli: int64(len(br)), SHA256: hex.EncodeToString(sum[:])}, nil
}

func gzipCanonical(data []byte, level int) ([]byte, error) {
	var out bytes.Buffer
	w, err := gzip.NewWriterLevel(&out, level)
	if err != nil {
		return nil, err
	}
	w.Header = gzip.Header{ModTime: time.Time{}, OS: 255}
	if _, err := w.Write(data); err != nil {
		w.Close()
		return nil, err
	}
	if err := w.Close(); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

func brotliCanonical(data []byte, quality, window int) ([]byte, error) {
	var out bytes.Buffer
	w := brotli.NewWriterOptions(&out, brotli.WriterOptions{Quality: quality, LGWin: window})
	if _, err := w.Write(data); err != nil {
		w.Close()
		return nil, err
	}
	if err := w.Close(); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}
