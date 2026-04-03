package route

import (
	"mime"
	"net/http"
	neturl "net/url"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/odvcencio/gosx/server"
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

func fileMetadataAssetFromPath(root, filePath string) (FileMetadataAsset, bool, error) {
	rel, err := filepath.Rel(root, filePath)
	if err != nil {
		return FileMetadataAsset{}, false, err
	}
	source := filepath.ToSlash(rel)
	dir := normalizeFileRouteDir(filepath.Dir(source))
	kind, ok := fileMetadataAssetKind(filepath.Base(source), dir == "")
	if !ok {
		return FileMetadataAsset{}, false, nil
	}
	routePath, pattern, params := fileMetadataAssetRoute(dir, filepath.Base(source))
	return FileMetadataAsset{
		Kind:      kind,
		FilePath:  filePath,
		Source:    source,
		Dir:       dir,
		RoutePath: routePath,
		Pattern:   pattern,
		Params:    append([]string(nil), params...),
	}, true, nil
}

func fileMetadataAssetKind(name string, isRoot bool) (FileMetadataAssetKind, bool) {
	name = strings.TrimSpace(strings.ToLower(name))
	switch name {
	case "opengraph-image.png", "opengraph-image.jpg", "opengraph-image.webp":
		return FileMetadataAssetOpenGraphImage, true
	case "twitter-image.png", "twitter-image.jpg", "twitter-image.webp":
		return FileMetadataAssetTwitterImage, true
	}
	if !isRoot {
		return "", false
	}
	switch name {
	case "favicon.ico":
		return FileMetadataAssetFavicon, true
	case "icon.png":
		return FileMetadataAssetIcon, true
	case "apple-icon.png":
		return FileMetadataAssetAppleIcon, true
	case "site.webmanifest":
		return FileMetadataAssetManifest, true
	case "robots.txt":
		return FileMetadataAssetRobots, true
	case "sitemap.xml":
		return FileMetadataAssetSitemap, true
	default:
		return "", false
	}
}

func fileMetadataAssetRoute(dir, name string) (string, string, []string) {
	routePath, pattern, params := buildFileRoute(fileRouteParts(dir))
	suffix := "/" + strings.TrimPrefix(strings.TrimSpace(name), "/")
	if routePath == "/" {
		return suffix, suffix, params
	}
	return routePath + suffix, pattern + suffix, params
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

func serveFileMetadataAsset(asset FileMetadataAsset) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		info, err := os.Stat(asset.FilePath)
		if err != nil || info.IsDir() {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Cache-Control", "public, max-age=0, must-revalidate")
		if contentType := metadataAssetContentType(asset); contentType != "" {
			w.Header().Set("Content-Type", contentType)
		}
		http.ServeFile(w, r, asset.FilePath)
	})
}

func metadataAssetContentType(asset FileMetadataAsset) string {
	switch asset.Kind {
	case FileMetadataAssetManifest:
		return "application/manifest+json; charset=utf-8"
	case FileMetadataAssetRobots:
		return "text/plain; charset=utf-8"
	case FileMetadataAssetSitemap:
		return "application/xml; charset=utf-8"
	default:
		contentType := mime.TypeByExtension(filepath.Ext(asset.FilePath))
		if contentType == "" {
			return ""
		}
		if strings.HasPrefix(contentType, "text/") && !strings.Contains(contentType, "charset=") {
			return contentType + "; charset=utf-8"
		}
		return contentType
	}
}

func metadataFromAssetPath(kind FileMetadataAssetKind, requestPath string) server.Metadata {
	requestPath = strings.TrimSpace(requestPath)
	if requestPath == "" {
		return server.Metadata{}
	}
	switch kind {
	case FileMetadataAssetOpenGraphImage:
		return server.Metadata{
			OpenGraph: &server.OpenGraph{
				Images: []server.MediaAsset{{URL: requestPath}},
			},
		}
	case FileMetadataAssetTwitterImage:
		return server.Metadata{
			Twitter: &server.Twitter{
				Images: []server.MediaAsset{{URL: requestPath}},
			},
		}
	case FileMetadataAssetFavicon:
		return server.Metadata{
			Icons: &server.Icons{
				Icon: []server.IconAsset{{
					URL:  requestPath,
					Type: metadataAssetMIMEType(requestPath),
				}},
			},
		}
	case FileMetadataAssetIcon:
		return server.Metadata{
			Icons: &server.Icons{
				Icon: []server.IconAsset{{
					URL:  requestPath,
					Type: metadataAssetMIMEType(requestPath),
				}},
			},
		}
	case FileMetadataAssetAppleIcon:
		return server.Metadata{
			Icons: &server.Icons{
				Apple: []server.IconAsset{{
					URL:  requestPath,
					Type: metadataAssetMIMEType(requestPath),
				}},
			},
		}
	case FileMetadataAssetManifest:
		return server.Metadata{Manifest: requestPath}
	default:
		return server.Metadata{}
	}
}

func metadataAssetMIMEType(requestPath string) string {
	contentType := mime.TypeByExtension(path.Ext(requestPath))
	return strings.TrimSpace(contentType)
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

func cloneFileMetadataAsset(asset *FileMetadataAsset) *FileMetadataAsset {
	if asset == nil {
		return nil
	}
	cp := *asset
	cp.Params = append([]string(nil), asset.Params...)
	return &cp
}

func cloneFilePageMetadataAssets(assets FilePageMetadataAssets) FilePageMetadataAssets {
	return FilePageMetadataAssets{
		OpenGraphImage: cloneFileMetadataAsset(assets.OpenGraphImage),
		TwitterImage:   cloneFileMetadataAsset(assets.TwitterImage),
	}
}

func sameFileMetadataAsset(left *FileMetadataAsset, right *FileMetadataAsset) bool {
	if left == nil || right == nil {
		return left == right
	}
	return left.Kind == right.Kind && left.Source == right.Source && left.RoutePath == right.RoutePath
}
