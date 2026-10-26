package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// Stable source paths let TinyGo reuse its package cache. The workspace itself
// is always rebuilt from current sources; compiled artifacts remain TinyGo's
// responsibility. Contention or an unavailable cache retains temporary builds.
func tinyGoScratchDirectory(gosxRoot string, tags ...string) (string, func(), error) {
	if cache, err := runtimeCacheRoot(); err == nil {
		if dir, release, ok := tryTinyGoWorkspace(cache, gosxRoot, tags...); ok {
			return dir, release, nil
		}
	}
	dir, err := os.MkdirTemp("", "gosx-tinygo-wasm-*")
	if err != nil {
		return "", nil, fmt.Errorf("create tinygo scratch module: %w", err)
	}
	return dir, func() { _ = os.RemoveAll(dir) }, nil
}

func tryTinyGoWorkspace(cache, gosxRoot string, tags ...string) (string, func(), bool) {
	cache, err := filepath.Abs(filepath.Join(cache, "tinygo-build"))
	if err != nil {
		return "", nil, false
	}
	source, err := filepath.Abs(gosxRoot)
	if err != nil {
		return "", nil, false
	}
	if err := os.MkdirAll(cache, 0700); err != nil {
		return "", nil, false
	}
	root, err := os.OpenRoot(cache)
	if err != nil {
		return "", nil, false
	}
	// The key names a workspace, not an artifact. Every use replaces its files.
	digest := sha256.Sum256([]byte(source + "\x00" + strings.Join(tags, "\x00")))
	key := hex.EncodeToString(digest[:16])
	lock, err := openBudgetLock(root, key+".lock")
	if err != nil {
		root.Close()
		return "", nil, false
	}
	if !tryTinyGoWorkspaceLock(lock) {
		lock.Close()
		root.Close()
		return "", nil, false
	}
	ready := false
	defer func() {
		if !ready {
			releaseTinyGoWorkspaceLock(lock)
			lock.Close()
			root.Close()
		}
	}()
	if err := root.RemoveAll(key); err != nil {
		return "", nil, false
	}
	if err := root.Mkdir(key, 0700); err != nil {
		return "", nil, false
	}
	ready = true
	var once sync.Once
	return filepath.Join(cache, key), func() {
		once.Do(func() {
			_ = root.RemoveAll(key)
			releaseTinyGoWorkspaceLock(lock)
			lock.Close()
			root.Close()
		})
	}, true
}
