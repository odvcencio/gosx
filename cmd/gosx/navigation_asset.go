package main

import (
	"fmt"
	"os"
	"path/filepath"
)

// Use the app's SDK artifact, just like the bootstrap bundles, so its server
// binary and build manifest agree even when the CLI uses a different SDK.
func writeNavigationRuntimeAsset(runtimeDir, gosxRoot string) (HashedAsset, error) {
	data, err := os.ReadFile(filepath.Join(gosxRoot, "client", "runtime", "host", "navigation-runtime.min.js"))
	if err != nil {
		return HashedAsset{}, fmt.Errorf("read navigation runtime: %w", err)
	}
	if err := os.MkdirAll(runtimeDir, 0755); err != nil {
		return HashedAsset{}, err
	}
	asset, err := writeHashed(runtimeDir, "navigation", ".js", data)
	if err != nil {
		return HashedAsset{}, fmt.Errorf("write navigation runtime: %w", err)
	}
	return withRuntimeIntegrity(asset, data), nil
}
