package route

import (
	"fmt"
	"m31labs.dev/gosx/internal/basepath"
	"m31labs.dev/gosx/server"
)

// SetBasePath configures a standalone router's public prefix before Build.
// A router mounted in a server.App inherits the app's prefix automatically.
func (r *Router) SetBasePath(value string, options ...server.BasePathOptions) error {
	prefix, err := basepath.Normalize(value)
	if err != nil {
		return err
	}
	if len(options) > 1 {
		return fmt.Errorf("gosx: expected at most one base path option")
	}
	r.basePath = prefix
	r.basePathOptions = server.BasePathOptions{}
	if len(options) == 1 {
		r.basePathOptions = options[0]
	}
	return nil
}
