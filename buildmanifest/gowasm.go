package buildmanifest

import (
	"encoding/hex"
	"fmt"
)

// ValidGoWASMName reports whether name is a portable build.goWASM identifier.
// Names start with a lowercase letter and contain lowercase letters, digits,
// underscores or hyphens. They are logical bundle names, not package paths.
func ValidGoWASMName(name string) bool {
	if name == "" || name[0] < 'a' || name[0] > 'z' {
		return false
	}
	for i := 1; i < len(name); i++ {
		c := name[i]
		if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '_' || c == '-') {
			return false
		}
	}
	return true
}

func validGoWASMAsset(name string, asset HashedAsset) bool {
	digest, err := hex.DecodeString(asset.Hash)
	return ValidGoWASMName(name) && err == nil && len(digest) == 8 &&
		asset.Hash == hex.EncodeToString(digest) &&
		asset.File == name+"."+asset.Hash+".wasm" && asset.Size >= 8
}

// ValidateGoWASMAssets rejects malformed names and filenames before a manifest
// can provide URLs or filesystem paths for application modules.
func (m *Manifest) ValidateGoWASMAssets() error {
	if m == nil {
		return fmt.Errorf("nil build manifest")
	}
	for name, asset := range m.GoWASM {
		if !validGoWASMAsset(name, asset) {
			return fmt.Errorf("invalid Go WASM asset %q: %q", name, asset.File)
		}
	}
	return nil
}

// GoWASMURL resolves a configured application module using the same asset base
// as RuntimeURLs. An unknown or malformed entry returns an empty string.
func (m *Manifest) GoWASMURL(assetBaseURL, name string) string {
	if m == nil {
		return ""
	}
	asset, ok := m.GoWASM[name]
	if !ok || !validGoWASMAsset(name, asset) {
		return ""
	}
	return AssetURL(assetBaseURL, "go-wasm", asset.File)
}
