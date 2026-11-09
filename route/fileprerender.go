package route

import (
	"net/http"
	"os"
)

// filePrerenderMiddleware checks the resolved hooks in the export subprocess,
// before a loader or route middleware can evaluate request-time data.
func filePrerenderMiddleware(page FilePage, module FileModule) Middleware {
	return func(next http.Handler) http.Handler {
		if os.Getenv("GOSX_STATIC_EXPORT") != "1" {
			return next
		}
		staticOnly := os.Getenv("GOSX_STATIC_EXPORT_ONLY") == "1"
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodGet || r.Method == http.MethodHead {
				dynamic := module.Load != nil || len(module.Actions) > 0
				if staticOnly {
					// A server-less export has no request time, so a loader runs once
					// at build time. Actions cannot run at all; the exporter reports
					// those pages instead of dropping them.
					dynamic = len(module.Actions) > 0
				}
				if !page.Config.PrerenderEnabled(!dynamic) {
					reason := "config"
					if page.Config.Prerender == nil {
						reason = "actions"
					}
					w.Header().Set("X-GoSX-Prerender", "skip")
					w.Header().Set("X-GoSX-Prerender-Reason", reason)
					w.WriteHeader(http.StatusNoContent)
					return
				}
				if module.Load != nil {
					w.Header().Set("X-GoSX-Prerender", "load")
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}
