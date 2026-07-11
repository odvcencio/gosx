package docs

import (
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"m31labs.dev/gosx/action"
	docsapp "m31labs.dev/gosx/examples/gosx-docs/app"
	"m31labs.dev/gosx/examples/gosx-docs/app/demos/democtl"
	"m31labs.dev/gosx/route"
)

// publishStore is an in-memory store for the last published CMS state.
type publishStore struct {
	mu     sync.Mutex
	count  int
	at     time.Time
	blocks []map[string]string
}

// snapshot returns the last-publish count and a human-readable timestamp.
// Returns (0, "Never published") if nothing has been published yet.
func (s *publishStore) snapshot() ([]map[string]string, int, string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.at.IsZero() {
		return cloneCMSBlocks(defaultCMSBlocks()), 0, "Not published yet"
	}
	return cloneCMSBlocks(s.blocks), s.count, s.at.Format("15:04:05")
}

// save records a new publish event with the given block count.
func (s *publishStore) save(blocks []map[string]string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.blocks = cloneCMSBlocks(blocks)
	s.count = len(blocks)
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
		"Edit structured blocks and publish the validated document through a GoSX server Action.",
		route.FileModuleOptions{
			Load: func(ctx *route.RouteContext, page route.FilePage) (any, error) {
				blocks, count, at := cmsStore.snapshot()
				return map[string]any{
					"blocks": blocks,
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
					blocks, fieldErrors := cmsBlocksFromForm(ctx.FormData)
					if len(fieldErrors) > 0 {
						ctx.ValidationFailure("Fix the highlighted content before publishing.", fieldErrors)
						return nil
					}
					cmsStore.save(blocks)
					_, n, _ := cmsStore.snapshot()
					return ctx.Success(
						fmt.Sprintf("Published %d blocks", n),
						map[string]any{"count": n},
					)
				},
			},
		},
	)
}

func cmsBlocksFromForm(values map[string]string) ([]map[string]string, map[string]string) {
	value := func(name string) string { return strings.TrimSpace(values[name]) }
	blocks := []map[string]string{
		{"kind": "hero", "title": value("hero_title"), "subtitle": value("hero_subtitle")},
		{"kind": "feature", "title": value("feature_title"), "body": value("feature_body")},
		{"kind": "quote", "text": value("quote_text"), "author": value("quote_author")},
	}
	errs := map[string]string{}
	for _, name := range []string{"hero_title", "feature_title", "quote_text"} {
		if value(name) == "" {
			errs[name] = "Required"
		}
	}
	limits := map[string]int{"hero_title": 120, "hero_subtitle": 240, "feature_title": 120, "feature_body": 1000, "quote_text": 500, "quote_author": 120}
	for name, limit := range limits {
		if len(value(name)) > limit {
			errs[name] = fmt.Sprintf("Must be %d characters or fewer", limit)
		}
	}
	return blocks, errs
}

func cloneCMSBlocks(blocks []map[string]string) []map[string]string {
	out := make([]map[string]string, len(blocks))
	for i, block := range blocks {
		out[i] = make(map[string]string, len(block))
		for key, value := range block {
			out[i][key] = value
		}
	}
	return out
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
