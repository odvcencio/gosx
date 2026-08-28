package docs

import (
	"net/url"
	"os"
	"path"
	"strings"
	"time"

	"m31labs.dev/gosx"
)

const defaultPublicBaseURL = "https://gosx.m31labs.dev"

// PublicBaseURL returns the canonical public origin for this docs site.
// Production sets PUBLIC_URL explicitly; the stable public origin is used as
// a fallback so metadata and the sitemap never depend on a request Host header.
func PublicBaseURL() string {
	raw := strings.TrimSpace(os.Getenv("PUBLIC_URL"))
	parsed, err := url.Parse(raw)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return defaultPublicBaseURL
	}
	parsed.Path = strings.TrimRight(parsed.Path, "/")
	parsed.RawPath = ""
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return strings.TrimRight(parsed.String(), "/")
}

// PublicSiteURL returns a canonical absolute URL on the configured public
// origin. It normalizes the route path and deliberately drops query strings.
func PublicSiteURL(routePath string) string {
	if parsed, err := url.Parse(strings.TrimSpace(routePath)); err == nil {
		routePath = parsed.Path
	}
	clean := path.Clean("/" + strings.TrimSpace(routePath))
	if clean == "." || clean == "" {
		clean = "/"
	}
	return PublicBaseURL() + clean
}

// SiteBuildInfo is the render-safe deployment identity shared by the site
// shell and the machine-readable status endpoint. Build fields are supplied by
// the immutable deployment; local development reports an honest unknown
// revision rather than inventing one.
func SiteBuildInfo() map[string]string {
	return map[string]string{
		"frameworkVersion": "v" + gosx.Version,
		"revision":         siteRevision(os.Getenv("GOSX_DOCS_REVISION")),
		"builtAt":          siteBuildTime(os.Getenv("GOSX_DOCS_BUILT_AT")),
		"publicURL":        PublicBaseURL(),
	}
}

func siteRevision(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "unknown"
	}
	if len(value) > 64 {
		value = value[:64]
	}
	for _, char := range value {
		if (char < '0' || char > '9') && (char < 'a' || char > 'f') && (char < 'A' || char > 'F') {
			return "unknown"
		}
	}
	return value
}

func siteBuildTime(value string) string {
	parsed, err := time.Parse(time.RFC3339, strings.TrimSpace(value))
	if err != nil {
		return ""
	}
	return parsed.UTC().Format(time.RFC3339)
}
