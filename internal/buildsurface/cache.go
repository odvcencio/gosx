package buildsurface

import (
	"fmt"
	"os"
	"path/filepath"
)

// cachedWASMPath returns the absolute path where a compiled WASM for the given
// fingerprint is stored inside cacheDir.
func cachedWASMPath(cacheDir, fingerprint string) string {
	return filepath.Join(cacheDir, fingerprint+".wasm")
}

// outputWASMPath returns the public output path for a named surface WASM.
// The filename uses the first 8 hex digits of the fingerprint so the browser
// can use long-lived immutable caching.
func outputWASMPath(outputDir, name, fingerprint string) string {
	short := fingerprint
	if len(short) > 8 {
		short = short[:8]
	}
	return filepath.Join(outputDir, fmt.Sprintf("%s.%s.wasm", name, short))
}

// lookupCache reports whether a cached WASM for fingerprint exists.
func lookupCache(cacheDir, fingerprint string) (string, bool) {
	p := cachedWASMPath(cacheDir, fingerprint)
	if _, err := os.Stat(p); err == nil {
		return p, true
	}
	return "", false
}

// writeCache writes data atomically to cacheDir/<fingerprint>.wasm by writing
// to a .tmp file and renaming.
func writeCache(cacheDir, fingerprint string, data []byte) (string, error) {
	if err := os.MkdirAll(cacheDir, 0755); err != nil {
		return "", fmt.Errorf("create cache dir: %w", err)
	}
	dest := cachedWASMPath(cacheDir, fingerprint)
	tmp := dest + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return "", fmt.Errorf("write cache tmp: %w", err)
	}
	if err := os.Rename(tmp, dest); err != nil {
		_ = os.Remove(tmp)
		return "", fmt.Errorf("rename cache file: %w", err)
	}
	return dest, nil
}

// copyToOutput copies the cached WASM at cachePath to the output file at
// outputPath, creating directories as needed.
func copyToOutput(outputPath, cachePath string) error {
	if err := os.MkdirAll(filepath.Dir(outputPath), 0755); err != nil {
		return fmt.Errorf("create output dir: %w", err)
	}
	data, err := os.ReadFile(cachePath)
	if err != nil {
		return fmt.Errorf("read cached wasm: %w", err)
	}
	if err := os.WriteFile(outputPath, data, 0644); err != nil {
		return fmt.Errorf("write output wasm: %w", err)
	}
	return nil
}
