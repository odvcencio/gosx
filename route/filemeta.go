package route

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/odvcencio/gosx/server"
)

var fileMetadataCache sync.Map

func addFileMetadata(ctx *RouteContext, files ...string) {
	if ctx == nil {
		return
	}
	for _, file := range files {
		meta, ok := fileMetadata(file)
		if !ok {
			continue
		}
		ctx.SetMetadata(meta)
	}
}

func fileMetadata(file string) (server.Metadata, bool) {
	metaPath := sidecarMetadataPath(file)
	if metaPath == "" {
		return server.Metadata{}, false
	}
	if cached, ok := fileMetadataCache.Load(metaPath); ok {
		meta, _ := cached.(server.Metadata)
		return meta, !isZeroMetadata(meta)
	}

	data, err := os.ReadFile(metaPath)
	if err != nil {
		fileMetadataCache.Store(metaPath, server.Metadata{})
		return server.Metadata{}, false
	}

	var meta server.Metadata
	if err := json.Unmarshal(data, &meta); err != nil {
		fileMetadataCache.Store(metaPath, server.Metadata{})
		return server.Metadata{}, false
	}
	fileMetadataCache.Store(metaPath, meta)
	return meta, !isZeroMetadata(meta)
}

func sidecarMetadataPath(file string) string {
	file = strings.TrimSpace(file)
	if file == "" {
		return ""
	}
	ext := filepath.Ext(file)
	if ext == "" {
		return ""
	}
	candidate := strings.TrimSuffix(file, ext) + ".meta.json"
	if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
		return candidate
	}
	return ""
}

func isZeroMetadata(meta server.Metadata) bool {
	return meta.Title == "" &&
		meta.Description == "" &&
		meta.Canonical == "" &&
		len(meta.Meta) == 0 &&
		len(meta.Links) == 0
}
