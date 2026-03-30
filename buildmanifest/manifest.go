package buildmanifest

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Manifest describes all build outputs for deployment.
type Manifest struct {
	Runtime RuntimeAssets `json:"runtime"`
	Islands []IslandAsset `json:"islands"`
	CSS     []CSSAsset    `json:"css"`
}

type RuntimeAssets struct {
	WASM          HashedAsset `json:"wasm"`
	WASMExec      HashedAsset `json:"wasmExec"`
	Bootstrap     HashedAsset `json:"bootstrap"`
	BootstrapLite HashedAsset `json:"bootstrapLite,omitempty"`
	Patch         HashedAsset `json:"patch"`
	VideoHLS      HashedAsset `json:"videoHLS,omitempty"`
}

type IslandAsset struct {
	Name   string `json:"name"`
	Format string `json:"format"` // "json" or "bin"
	HashedAsset
}

type CSSAsset struct {
	Component string `json:"component"`
	Source    string `json:"source,omitempty"`
	HashedAsset
}

type HashedAsset struct {
	File string `json:"file"`
	Hash string `json:"hash"`
	Size int64  `json:"size"`
}

type RuntimePaths struct {
	WASM          string
	WASMExec      string
	Bootstrap     string
	BootstrapLite string
	Patch         string
	VideoHLS      string
}

// Load reads a build manifest from disk.
func Load(path string) (*Manifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read build manifest %s: %w", path, err)
	}

	var manifest Manifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return nil, fmt.Errorf("decode build manifest %s: %w", path, err)
	}
	return &manifest, nil
}

// RuntimeURLs returns the public URLs for the shared runtime assets.
func (m *Manifest) RuntimeURLs(assetBaseURL string) RuntimePaths {
	return RuntimePaths{
		WASM:          AssetURL(assetBaseURL, "runtime", m.Runtime.WASM.File),
		WASMExec:      AssetURL(assetBaseURL, "runtime", m.Runtime.WASMExec.File),
		Bootstrap:     AssetURL(assetBaseURL, "runtime", m.Runtime.Bootstrap.File),
		BootstrapLite: AssetURL(assetBaseURL, "runtime", m.Runtime.BootstrapLite.File),
		Patch:         AssetURL(assetBaseURL, "runtime", m.Runtime.Patch.File),
		VideoHLS:      AssetURL(assetBaseURL, "runtime", m.Runtime.VideoHLS.File),
	}
}

// IslandAssetByName returns the hashed island asset for a component, if any.
func (m *Manifest) IslandAssetByName(componentName string) (IslandAsset, bool) {
	for _, asset := range m.Islands {
		if asset.Name == componentName {
			return asset, true
		}
	}
	return IslandAsset{}, false
}

// IslandURL returns the public URL for an island program asset.
func (m *Manifest) IslandURL(assetBaseURL string, asset IslandAsset) string {
	return AssetURL(assetBaseURL, "islands", asset.File)
}

// CSSURL returns the public URL for a CSS asset.
func (m *Manifest) CSSURL(assetBaseURL string, asset CSSAsset) string {
	return AssetURL(assetBaseURL, "css", asset.File)
}

// CSSAssetBySource returns the hashed CSS asset for a source-relative path, if any.
func (m *Manifest) CSSAssetBySource(source string) (CSSAsset, bool) {
	for _, asset := range m.CSS {
		if asset.Source == source {
			return asset, true
		}
	}
	return CSSAsset{}, false
}

// AssetURL joins the mounted public asset root with a build output file.
func AssetURL(assetBaseURL, bucket, file string) string {
	if file == "" {
		return ""
	}

	base := strings.TrimRight(assetBaseURL, "/")
	suffix := bucket + "/" + strings.TrimLeft(file, "/")
	if base == "" {
		return "/" + suffix
	}
	return base + "/" + suffix
}

// ExportFilePath returns the stable static-export file path for a route.
func ExportFilePath(routePath string) string {
	routePath = strings.TrimSpace(routePath)
	if routePath == "" || routePath == "/" {
		return "index.html"
	}
	clean := strings.Trim(routePath, "/")
	return filepath.Join(filepath.FromSlash(clean), "index.html")
}
