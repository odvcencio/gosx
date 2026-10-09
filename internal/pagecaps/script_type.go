package pagecaps

import (
	"mime"
	"strings"
)

// ExecutableScriptType recognizes modules and JavaScript MIME types. Both
// capability detection and inline byte accounting use this definition.
func ExecutableScriptType(typ string) bool {
	typ = strings.ToLower(strings.TrimSpace(typ))
	if typ != "" && typ != "module" {
		mediaType, _, err := mime.ParseMediaType(typ)
		if err != nil {
			return false
		}
		typ = mediaType
	}
	switch typ {
	case "", "module", "application/javascript", "application/ecmascript", "application/x-javascript", "application/x-ecmascript", "text/javascript", "text/ecmascript", "text/jscript", "text/livescript", "text/x-javascript", "text/x-ecmascript", "text/javascript1.0", "text/javascript1.1", "text/javascript1.2", "text/javascript1.3", "text/javascript1.4", "text/javascript1.5":
		return true
	}
	return false
}
