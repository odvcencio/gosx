package server

import (
	"fmt"
	"net/http"
	"strings"
)

// MetadataDocumentFunc resolves text metadata documents such as robots.txt and sitemap.xml.
type MetadataDocumentFunc func(r *http.Request) (string, error)

// MetadataDocumentHandler serves a text response produced on demand.
func MetadataDocumentHandler(contentType string, resolve MetadataDocumentFunc) http.Handler {
	contentType = normalizeMetadataDocumentContentType(contentType)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if resolve == nil {
			http.NotFound(w, r)
			return
		}
		body, err := resolve(r)
		if err != nil {
			http.Error(w, fmt.Sprintf("metadata document error: %v", err), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", contentType)
		w.Header().Set("Cache-Control", "public, max-age=0, must-revalidate")
		_, _ = w.Write([]byte(body))
	})
}

// StaticMetadataDocumentHandler serves a fixed text metadata document.
func StaticMetadataDocumentHandler(contentType string, body string) http.Handler {
	return MetadataDocumentHandler(contentType, func(*http.Request) (string, error) {
		return body, nil
	})
}

// RobotsHandler serves a robots.txt document.
func RobotsHandler(resolve MetadataDocumentFunc) http.Handler {
	return MetadataDocumentHandler("text/plain; charset=utf-8", resolve)
}

// StaticRobotsHandler serves a fixed robots.txt document.
func StaticRobotsHandler(body string) http.Handler {
	return StaticMetadataDocumentHandler("text/plain; charset=utf-8", body)
}

// SitemapHandler serves a sitemap.xml document.
func SitemapHandler(resolve MetadataDocumentFunc) http.Handler {
	return MetadataDocumentHandler("application/xml; charset=utf-8", resolve)
}

// StaticSitemapHandler serves a fixed sitemap.xml document.
func StaticSitemapHandler(body string) http.Handler {
	return StaticMetadataDocumentHandler("application/xml; charset=utf-8", body)
}

func normalizeMetadataDocumentContentType(contentType string) string {
	contentType = strings.TrimSpace(contentType)
	if contentType == "" {
		return "text/plain; charset=utf-8"
	}
	if strings.HasPrefix(contentType, "text/") && !strings.Contains(contentType, "charset=") {
		return contentType + "; charset=utf-8"
	}
	return contentType
}
