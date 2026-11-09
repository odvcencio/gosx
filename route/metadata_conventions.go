package route

import (
	neturl "net/url"
	"path"
	"path/filepath"
	"strings"
)

// FileMetadataAssetKind identifies a convention-driven metadata asset.
type FileMetadataAssetKind string

const (
	FileMetadataAssetOpenGraphImage FileMetadataAssetKind = "opengraph_image"
	FileMetadataAssetTwitterImage   FileMetadataAssetKind = "twitter_image"
	FileMetadataAssetFavicon        FileMetadataAssetKind = "favicon"
	FileMetadataAssetIcon           FileMetadataAssetKind = "icon"
	FileMetadataAssetAppleIcon      FileMetadataAssetKind = "apple_icon"
	FileMetadataAssetManifest       FileMetadataAssetKind = "manifest"
	FileMetadataAssetRobots         FileMetadataAssetKind = "robots"
	FileMetadataAssetSitemap        FileMetadataAssetKind = "sitemap"
)

// FileMetadataAsset describes a discovered metadata convention file.
type FileMetadataAsset struct {
	Kind      FileMetadataAssetKind
	FilePath  string
	Source    string
	Dir       string
	RoutePath string
	Pattern   string
	Params    []string
}

// FilePageMetadataAssets holds the nearest discovered page-scoped metadata assets.
type FilePageMetadataAssets struct {
	OpenGraphImage *FileMetadataAsset
	TwitterImage   *FileMetadataAsset
}

func (a FileMetadataAsset) RequestPath(params map[string]string) string {
	pathTemplate := strings.TrimSpace(a.RoutePath)
	if pathTemplate == "" {
		return ""
	}
	if len(a.Params) == 0 || len(params) == 0 {
		return pathTemplate
	}
	resolved := pathTemplate
	for _, name := range a.Params {
		catchAll := "{" + name + "...}"
		if strings.Contains(resolved, catchAll) {
			resolved = strings.ReplaceAll(resolved, catchAll, escapeCatchAllPath(params[name]))
		}
		segment := "{" + name + "}"
		if strings.Contains(resolved, segment) {
			resolved = strings.ReplaceAll(resolved, segment, neturl.PathEscape(strings.TrimSpace(params[name])))
		}
	}
	if !strings.HasPrefix(resolved, "/") {
		resolved = "/" + resolved
	}
	return path.Clean(resolved)
}

func (a FileMetadataAsset) StaticExportPath() (string, bool) {
	if len(a.Params) > 0 || strings.TrimSpace(a.RoutePath) == "" {
		return "", false
	}
	return filepath.FromSlash(strings.TrimPrefix(a.RoutePath, "/")), true
}

func escapeCatchAllPath(raw string) string {
	raw = strings.Trim(raw, "/")
	if raw == "" {
		return ""
	}
	parts := strings.Split(raw, "/")
	for i, part := range parts {
		parts[i] = neturl.PathEscape(strings.TrimSpace(part))
	}
	return strings.Join(parts, "/")
}
