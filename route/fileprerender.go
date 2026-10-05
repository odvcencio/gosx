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
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodGet || r.Method == http.MethodHead {
				dynamic := module.Load != nil || len(module.Actions) > 0
				if !page.Config.PrerenderEnabled(!dynamic) {
					w.Header().Set("X-GoSX-Prerender", "skip")
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
