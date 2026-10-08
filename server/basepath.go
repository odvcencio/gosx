package server

import (
	"fmt"
	"m31labs.dev/gosx/internal/basepath"
	"net/http"
)

// BasePathOptions describes how the upstream proxy delivers request paths.
type BasePathOptions struct {
	// ProxyStripsPrefix means the trusted proxy removes the public prefix before
	// forwarding. The default expects the prefix on incoming request paths.
	ProxyStripsPrefix bool
}

// SetBasePath configures a public path prefix before Build. Empty or "/" keeps
// root deployment. Routes remain root-relative; GoSX prefixes emitted URLs.
func (a *App) SetBasePath(value string, options ...BasePathOptions) error {
	prefix, err := basepath.Normalize(value)
	if err != nil {
		return err
	}
	if len(options) > 1 {
		return fmt.Errorf("gosx: expected at most one base path option")
	}
	a.basePath = prefix
	a.basePathOptions = BasePathOptions{}
	if len(options) == 1 {
		a.basePathOptions = options[0]
	}
	return nil
}

// RequestBasePath returns the configured public prefix for this request.
func RequestBasePath(r *http.Request) string { return basepath.FromRequest(r) }

// URL returns a public URL for a local root-relative app path.
func URL(r *http.Request, value string) string { return basepath.URL(RequestBasePath(r), value) }
