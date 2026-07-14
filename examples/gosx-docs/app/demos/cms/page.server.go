package docs

import (
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"m31labs.dev/gosx/action"
	docsapp "m31labs.dev/gosx/examples/gosx-docs/app"
	"m31labs.dev/gosx/examples/gosx-docs/app/demos/democtl"
	"m31labs.dev/gosx/route"
)

// cmsMaxBlocks caps how many blocks a single publish request may submit.
// Defense in depth only: the demo has no server-side reordering or removal,
// and normal use (clicking the palette a reasonable number of times) never
// approaches this, but a raw HTTP client could otherwise post an unbounded
// block_count and force the server to loop arbitrarily.
const cmsMaxBlocks = 60

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
					_, n, at := cmsStore.snapshot()
					return ctx.Success(
						fmt.Sprintf("Published %d blocks", n),
						map[string]any{
							"count":  n,
							"at":     at,
							"byKind": cmsSummarizeBlocks(blocks),
						},
					)
				},
			},
		},
	)
}

// cmsBlocksFromForm reconstructs the submitted draft from indexed form
// fields: a "block_count" field gives the number of blocks, and each block i
// carries "block_<i>_kind" plus kind-specific fields ("block_<i>_title" /
// "_subtitle" for hero, "_title" / "_body" for feature, "_text" / "_author"
// for quote). This indexed scheme — rather than the single fixed hero/
// feature/quote field set the demo shipped with originally — is what lets
// the client add any number of blocks of any kind and have them all survive
// a real publish.
func cmsBlocksFromForm(values map[string]string) ([]map[string]string, map[string]string) {
	value := func(name string) string { return strings.TrimSpace(values[name]) }
	errs := map[string]string{}

	count, convErr := strconv.Atoi(value("block_count"))
	if convErr != nil || count < 0 {
		count = 0
	}
	if count > cmsMaxBlocks {
		count = cmsMaxBlocks
	}
	if count == 0 {
		errs["block_count"] = "Add at least one block before publishing."
		return nil, errs
	}

	limits := map[string]int{"title": 120, "subtitle": 240, "body": 1000, "text": 500, "author": 120}
	checkField := func(field, role string, required bool) string {
		v := value(field)
		if required && v == "" {
			errs[field] = "Required"
		}
		if limit, ok := limits[role]; ok && len(v) > limit {
			errs[field] = fmt.Sprintf("Must be %d characters or fewer", limit)
		}
		return v
	}

	blocks := make([]map[string]string, 0, count)
	for i := 0; i < count; i++ {
		prefix := fmt.Sprintf("block_%d_", i)
		switch value(prefix + "kind") {
		case "hero":
			title := checkField(prefix+"title", "title", true)
			subtitle := checkField(prefix+"subtitle", "subtitle", false)
			blocks = append(blocks, map[string]string{"kind": "hero", "title": title, "subtitle": subtitle})
		case "feature":
			title := checkField(prefix+"title", "title", true)
			body := checkField(prefix+"body", "body", false)
			blocks = append(blocks, map[string]string{"kind": "feature", "title": title, "body": body})
		case "quote":
			text := checkField(prefix+"text", "text", true)
			author := checkField(prefix+"author", "author", false)
			blocks = append(blocks, map[string]string{"kind": "quote", "text": text, "author": author})
		default:
			errs[prefix+"kind"] = "Unknown block type"
		}
	}
	return blocks, errs
}

// cmsSummarizeBlocks counts published blocks by kind, always reporting the
// three known kinds (even at zero) so the client can render a stable summary.
func cmsSummarizeBlocks(blocks []map[string]string) map[string]int {
	summary := map[string]int{"hero": 0, "feature": 0, "quote": 0}
	for _, block := range blocks {
		summary[block["kind"]]++
	}
	return summary
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
