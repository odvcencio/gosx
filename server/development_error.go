package server

import (
	"html"
	"net/http"
	"os"
	"strings"
)

func developmentErrorsEnabled() bool {
	mode := strings.ToLower(strings.TrimSpace(os.Getenv("GOSX_ENV")))
	return os.Getenv("GOSX_DEV") == "1" && mode != "production" && mode != "prod"
}

// WriteDevelopmentError writes a standalone error page when gosx dev is
// running and the request accepts HTML. It reports whether it wrote a page.
// Production mode and JSON requests leave the response untouched.
func WriteDevelopmentError(w http.ResponseWriter, r *http.Request, err error) bool {
	if !developmentErrorsEnabled() || wantsJSON(r) {
		return false
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(errorStatus(err, 0, http.StatusInternalServerError))
	if r.Method == http.MethodHead {
		return true
	}
	message := "Unknown render error"
	if err != nil {
		message = err.Error()
	}
	// Render independently of the app's document, layout and error component:
	// any of those can be the source of the failure being reported.
	_, _ = w.Write([]byte(`<!doctype html><html lang="en"><head><meta charset="utf-8"><title>GoSX render error</title></head><body><main><h1>GoSX render error</h1><pre>` + html.EscapeString(message) + `</pre><p>Fix the source and save to reload. Error details are shown only in development.</p></main></body></html>`))
	return true
}
