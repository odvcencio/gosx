//go:build !js || !wasm

package rooms

import (
	"net/http"
	"net/url"
	"os"
	"strings"

	docsapp "m31labs.dev/gosx/examples/gosx-docs/app"
	"m31labs.dev/gosx/route"
)

func init() {
	docsapp.RegisterStaticDocsPage(
		"Rooms",
		"A server-authoritative game room with lobby, countdown, bots, prediction and reconnect.",
		route.FileModuleOptions{
			Load: func(ctx *route.RouteContext, _ route.FilePage) (any, error) {
				return map[string]any{"clientSrc": clientSrc(ctx.Request)}, nil
			},
		},
	)
}

// clientSrc is the iframe address: the embedded client page, pointed at this
// site's own room server.
func clientSrc(r *http.Request) string {
	q := url.Values{}
	q.Set("mode", "round")
	q.Set("server", webSocketURL(r))
	q.Set("name", "visitor")
	return "/demos/rooms/client/?" + q.Encode()
}

func webSocketURL(r *http.Request) string {
	scheme, host := "ws", "localhost"
	if r != nil {
		host = r.Host
		if r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https") {
			scheme = "wss"
		}
	}
	if strings.HasPrefix(strings.ToLower(os.Getenv("PUBLIC_URL")), "https://") {
		scheme = "wss"
	}
	return scheme + "://" + host + "/demos/rooms/play/ws"
}
