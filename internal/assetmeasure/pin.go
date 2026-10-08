// Package assetmeasure separates canonical normalization from release encodings.
package assetmeasure

import (
	"errors"
	"runtime/debug"
)

type CompressorPin struct {
	GoVersion, BrotliVersion               string
	GzipLevel, BrotliQuality, BrotliWindow int
}

var readBuildInfo = debug.ReadBuildInfo

func validatePin(pin CompressorPin) error {
	if pin != (CompressorPin{GoVersion: "1.26.0", BrotliVersion: "v1.2.1", GzipLevel: 9, BrotliQuality: 11, BrotliWindow: 0}) {
		return errors.New("unsupported canonical compressor pin")
	}
	info, ok := readBuildInfo()
	if !ok || info.GoVersion != "go"+pin.GoVersion {
		return errors.New("canonical build identity is unavailable")
	}
	for _, dep := range info.Deps {
		if dep.Path != "github.com/andybalholm/brotli" {
			continue
		}
		if dep.Replace != nil || dep.Version != pin.BrotliVersion {
			return errors.New("selected Brotli module does not match pin")
		}
		return nil
	}
	return errors.New("selected Brotli module identity is unavailable")
}
