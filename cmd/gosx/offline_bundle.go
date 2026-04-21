package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type offlineAssetManifest struct {
	SchemaVersion int                  `json:"schemaVersion"`
	CacheVersion  string               `json:"cacheVersion"`
	Files         []offlineAssetRecord `json:"files"`
}

type offlineAssetRecord struct {
	Path        string `json:"path"`
	Size        int64  `json:"size"`
	SHA256      string `json:"sha256"`
	CachePolicy string `json:"cachePolicy"`
}

func stageOfflineAssetBundle(projectDir, distDir string) error {
	offlineDir := filepath.Join(distDir, "offline")
	if err := os.RemoveAll(offlineDir); err != nil {
		return err
	}
	if err := os.MkdirAll(offlineDir, 0755); err != nil {
		return err
	}

	copies := []struct {
		src string
		dst string
	}{
		{filepath.Join(distDir, "assets"), filepath.Join(offlineDir, "assets")},
		{filepath.Join(projectDir, "app"), filepath.Join(offlineDir, "app")},
		{filepath.Join(projectDir, "public"), filepath.Join(offlineDir, "public")},
		{filepath.Join(distDir, "static"), filepath.Join(offlineDir, "static")},
	}
	for _, c := range copies {
		if err := copyDirIfPresent(c.src, c.dst); err != nil {
			return err
		}
	}
	for _, name := range []string{"build.json", "export.json"} {
		if err := copyFileIfPresent(filepath.Join(distDir, name), filepath.Join(offlineDir, name)); err != nil {
			return err
		}
	}

	manifest, err := buildOfflineAssetManifest(offlineDir)
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(offlineDir, "offline-manifest.json"), data, 0644)
}

func buildOfflineAssetManifest(root string) (offlineAssetManifest, error) {
	var records []offlineAssetRecord
	if err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		if filepath.Base(path) == "offline-manifest.json" {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		hash, err := fileSHA256(path)
		if err != nil {
			return err
		}
		records = append(records, offlineAssetRecord{
			Path:        rel,
			Size:        info.Size(),
			SHA256:      hash,
			CachePolicy: offlineCachePolicy(rel),
		})
		return nil
	}); err != nil {
		return offlineAssetManifest{}, err
	}
	sort.Slice(records, func(i, j int) bool { return records[i].Path < records[j].Path })
	return offlineAssetManifest{
		SchemaVersion: 1,
		CacheVersion:  offlineCacheVersion(records),
		Files:         records,
	}, nil
}

func fileSHA256(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

func offlineCachePolicy(path string) string {
	switch {
	case strings.HasPrefix(path, "assets/"):
		return "immutable"
	case path == "build.json", path == "export.json":
		return "versioned"
	default:
		return "first-launch"
	}
}

func offlineCacheVersion(records []offlineAssetRecord) string {
	h := sha256.New()
	for _, record := range records {
		fmt.Fprintf(h, "%s\x00%s\x00%d\x00", record.Path, record.SHA256, record.Size)
	}
	return hex.EncodeToString(h.Sum(nil))[:16]
}
