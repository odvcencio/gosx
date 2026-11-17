package server

import (
	"m31labs.dev/gosx/buildmanifest"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type runtimeInfo struct {
	mod  time.Time
	size int64
	hash string
}

var runtimeInfoCache = struct {
	sync.Mutex
	files map[string]runtimeInfo
}{files: make(map[string]runtimeInfo)}

// RuntimeAssetInfo returns the current hash and size of root's island runtime.
// It caches the hash until the file's modification time or size changes.
func RuntimeAssetInfo(root string) (string, int64, error) {
	name, err := filepath.Abs(filepath.Join(root, "build", "gosx-runtime.wasm"))
	if err != nil {
		return "", 0, err
	}
	runtimeInfoCache.Lock()
	defer runtimeInfoCache.Unlock()
	info, err := os.Stat(name)
	if err != nil {
		return "", 0, err
	}
	if cached, ok := runtimeInfoCache.files[name]; ok && cached.size == info.Size() && cached.mod.Equal(info.ModTime()) {
		return cached.hash, cached.size, nil
	}
	data, err := os.ReadFile(name)
	if err != nil {
		return "", 0, err
	}
	hash := buildmanifest.ContentHash(data)
	runtimeInfoCache.files[name] = runtimeInfo{mod: info.ModTime(), size: int64(len(data)), hash: hash}
	return hash, int64(len(data)), nil
}
