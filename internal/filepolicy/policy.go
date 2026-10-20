// Package filepolicy identifies immutable production application files.
package filepolicy

import (
	"m31labs.dev/gosx/env"
	"os"
	"strings"
)

func Immutable() bool {
	mode := strings.ToLower(env.Mode())
	return os.Getenv("GOSX_DEV") != "1" && (mode == "production" || mode == "prod")
}
