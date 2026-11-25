// Package mediatype owns the one table of file extensions and media types
// that GoSX serves and packs. The table is fixed, not read from the host's
// MIME database, so a Mac and a Linux host name the same file the same way.
package mediatype

import (
	"mime"
	"strings"
)

// table maps a lower-case extension (with the dot) to a media type without
// parameters.
var table = map[string]string{
	".wasm": "application/wasm", ".js": "text/javascript", ".mjs": "text/javascript",
	".css": "text/css", ".json": "application/json", ".webmanifest": "application/manifest+json",
	".txt": "text/plain", ".svg": "image/svg+xml", ".png": "image/png", ".jpg": "image/jpeg",
	".jpeg": "image/jpeg", ".gif": "image/gif", ".webp": "image/webp", ".avif": "image/avif",
	".ico": "image/x-icon", ".glb": "model/gltf-binary", ".gltf": "model/gltf+json",
	".bin": "application/octet-stream", ".woff": "font/woff", ".woff2": "font/woff2",
	".ttf": "font/ttf", ".otf": "font/otf", ".mp3": "audio/mpeg", ".ogg": "audio/ogg",
	".wav": "audio/wav", ".mp4": "video/mp4", ".webm": "video/webm", ".ktx2": "image/ktx2",
	".wgsl": "text/plain",
	// Types Go's own built-in table also knows.
	".html": "text/html", ".htm": "text/html", ".xml": "text/xml", ".pdf": "application/pdf",
}

// ByExtension returns the bare media type for ext (for example ".GLB"), or ""
// when the table does not list it. The match ignores case.
func ByExtension(ext string) string {
	return table[strings.ToLower(ext)]
}

// ForServing returns the Content-Type a server should send for ext. A type
// from the table gets "; charset=utf-8" when it is text/* or the web app
// manifest type. An extension outside the table gets whatever the host's own
// table says, or "".
func ForServing(ext string) string {
	if t := ByExtension(ext); t != "" {
		if strings.HasPrefix(t, "text/") || t == "application/manifest+json" {
			return t + "; charset=utf-8"
		}
		return t
	}
	return mime.TypeByExtension(ext)
}
