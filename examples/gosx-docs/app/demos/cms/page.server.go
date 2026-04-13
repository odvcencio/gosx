package docs

import (
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/odvcencio/gosx/action"
	docsapp "github.com/odvcencio/gosx/examples/gosx-docs/app"
	"github.com/odvcencio/gosx/examples/gosx-docs/app/demos/democtl"
	"github.com/odvcencio/gosx/route"
)

// publishStore is an in-memory store for the last published CMS state.
type publishStore struct {
	mu    sync.Mutex
	count int
	at    time.Time
}

// snapshot returns the last-publish count and a human-readable timestamp.
// Returns (0, "Never published") if nothing has been published yet.
func (s *publishStore) snapshot() (int, string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.at.IsZero() {
		return 0, "Never published"
	}
	return s.count, s.at.Format("15:04:05")
}

// save records a new publish event with the given block count.
func (s *publishStore) save(count int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.count = count
	s.at = time.Now()
}

var (
	cmsStore   publishStore
	cmsLimiter = democtl.NewLimiter(2, 10)
)

// cmsClientIP extracts a stable rate-limiting key from the HTTP request.
// It prefers the first entry of X-Forwarded-For (set by reverse proxies) and
// falls back to the host part of RemoteAddr. Same behavior as
// playground/compile_handler.go:clientIPFromRequest.
func cmsClientIP(r *http.Request) string {
	if r == nil {
		return "cms"
	}
	if fwd := r.Header.Get("X-Forwarded-For"); fwd != "" {
		// X-Forwarded-For may be "client, proxy1, proxy2" — take the first.
		if comma := strings.IndexByte(fwd, ','); comma >= 0 {
			fwd = fwd[:comma]
		}
		return strings.TrimSpace(fwd)
	}
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return host
	}
	return r.RemoteAddr
}

func init() {
	docsapp.RegisterStaticDocsPage("CMS Editor",
		"Block editor with inline editing and unified publish.",
		route.FileModuleOptions{
			Load: func(ctx *route.RouteContext, page route.FilePage) (any, error) {
				count, at := cmsStore.snapshot()
				return map[string]any{
					"blocks": defaultCMSBlocks(),
					"status": map[string]any{
						"count": count,
						"at":    at,
					},
				}, nil
			},
			Actions: route.FileActions{
				"publish": func(ctx *action.Context) error {
					if !cmsLimiter.Allow(cmsClientIP(ctx.Request)) {
						ctx.ValidationFailure("Slow down — try again in a moment.", nil)
						return nil
					}
					// Form-encoded POSTs don't carry a JSON body — count the
					// blocks from the default set as "current content".
					count := len(defaultCMSBlocks())
					cmsStore.save(count)
					n, _ := cmsStore.snapshot()
					return ctx.Success(
						fmt.Sprintf("Published %d blocks", n),
						map[string]any{"count": n},
					)
				},
			},
		},
	)
}

// defaultCMSBlocks returns the seed content shown in the editor on load.
// Note: key is "kind" instead of "type" because "type" is a Go keyword and
// cannot be accessed via selector syntax (block.type) in GSX file-mode templates
// which use go/parser to evaluate expressions.
func defaultCMSBlocks() []map[string]string {
	return []map[string]string{
		{"kind": "hero", "title": "Welcome to GoSX", "subtitle": "The Go-native web platform"},
		{"kind": "feature", "title": "Server Rendering", "body": "Every page renders on the server first."},
		{"kind": "quote", "text": "One language, full stack.", "author": "GoSX"},
	}
}
