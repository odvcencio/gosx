package server

import (
	"bufio"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"m31labs.dev/gosx"
)

// TestAppPageStreamsShellBeforeDeferredResolves proves that a page route
// registered on App can flush the document shell before a deferred block
// resolves. The dispatcher used to buffer the whole body in an intercept
// writer, so the shell arrived only after the slowest resolver finished.
func TestAppPageStreamsShellBeforeDeferredResolves(t *testing.T) {
	const resolverDelay = 300 * time.Millisecond

	app := New()
	app.Page("GET /stream", func(ctx *Context) gosx.Node {
		return gosx.El("main",
			gosx.El("h1", gosx.Text("shell-marker")),
			ctx.Defer(gosx.Text("loading"), func() (gosx.Node, error) {
				time.Sleep(resolverDelay)
				return gosx.Text("deferred-marker"), nil
			}),
		)
	})

	server := httptest.NewServer(app.Build())
	defer server.Close()

	started := time.Now()
	res, err := http.Get(server.URL + "/stream")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()

	shellAt, err := readUntilMarker(res.Body, "shell-marker")
	if err != nil {
		t.Fatalf("read shell: %v", err)
	}
	shellDelay := shellAt.Sub(started)

	rest, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(rest), "deferred-marker") {
		t.Fatalf("expected the deferred chunk after the shell, got %q", string(rest))
	}
	if shellDelay >= resolverDelay/2 {
		t.Fatalf("expected the shell before the resolver finished, got %v (resolver %v)", shellDelay, resolverDelay)
	}
}

// readUntilMarker reads from body until marker appears and reports the arrival
// time. It reads one byte at a time so the deadline reflects the first flush.
func readUntilMarker(body io.Reader, marker string) (time.Time, error) {
	var seen strings.Builder
	buf := make([]byte, 1)
	for {
		n, err := body.Read(buf)
		if n > 0 {
			seen.Write(buf[:n])
			if strings.Contains(seen.String(), marker) {
				return time.Now(), nil
			}
		}
		if err != nil {
			return time.Time{}, err
		}
	}
}

// TestAppPageRouteCanHijackConnection proves that a page route reaches the real
// connection. A hub upgrade runs as route middleware, and the intercept writer
// used to hide http.Hijacker from it.
func TestAppPageRouteCanHijackConnection(t *testing.T) {
	app := New()
	app.HandlePage(PageRoute{
		Pattern: "GET /upgrade",
		Handler: func(ctx *Context) gosx.Node {
			return gosx.Text("unreachable")
		},
		Middleware: []Middleware{
			func(next http.Handler) http.Handler {
				return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					hijacker, ok := w.(http.Hijacker)
					if !ok {
						http.Error(w, "no hijacker", http.StatusInternalServerError)
						return
					}
					conn, buffered, err := hijacker.Hijack()
					if err != nil {
						http.Error(w, err.Error(), http.StatusInternalServerError)
						return
					}
					defer conn.Close()
					_, _ = buffered.WriteString("HTTP/1.1 101 Switching Protocols\r\nUpgrade: gosx-hub\r\nConnection: Upgrade\r\n\r\n")
					_ = buffered.Flush()
				})
			},
		},
	})

	server := httptest.NewServer(app.Build())
	defer server.Close()

	conn, err := net.Dial("tcp", strings.TrimPrefix(server.URL, "http://"))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if _, err := io.WriteString(conn, "GET /upgrade HTTP/1.1\r\nHost: gosx.local\r\n\r\n"); err != nil {
		t.Fatal(err)
	}
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	status, err := bufio.NewReader(conn).ReadString('\n')
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(status, "101") {
		t.Fatalf("expected an upgrade status line, got %q", status)
	}
}

// TestAppPageHandlerKeepsCustomNotFoundBody proves that a matched page handler
// owns its 404 response. The dispatcher used to read status 404 as "no route
// matched" and replace the body with the generic not-found page.
func TestAppPageHandlerKeepsCustomNotFoundBody(t *testing.T) {
	app := New()
	app.Page("GET /articles/{slug}", func(ctx *Context) gosx.Node {
		ctx.SetStatus(http.StatusNotFound)
		return gosx.El("main", gosx.Text("custom-article-missing"))
	})

	handler := app.Build()
	res := httptest.NewRecorder()
	handler.ServeHTTP(res, httptest.NewRequest(http.MethodGet, "/articles/gone", nil))

	if res.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", res.Code)
	}
	if body := res.Body.String(); !strings.Contains(body, "custom-article-missing") {
		t.Fatalf("expected the handler body, got %q", body)
	}
}

// TestAppAPIHandlerKeepsCustomNotFoundBody proves the same rule for a JSON API
// route that reports 404 with a payload.
func TestAppAPIHandlerKeepsCustomNotFoundBody(t *testing.T) {
	app := New()
	app.API("GET /api/articles/{slug}", func(ctx *Context) (any, error) {
		ctx.SetStatus(http.StatusNotFound)
		return map[string]string{"error": "custom-api-missing"}, nil
	})

	handler := app.Build()
	res := httptest.NewRecorder()
	handler.ServeHTTP(res, httptest.NewRequest(http.MethodGet, "/api/articles/gone", nil))

	if res.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", res.Code)
	}
	if body := res.Body.String(); !strings.Contains(body, "custom-api-missing") {
		t.Fatalf("expected the handler payload, got %q", body)
	}
}

// TestAppRewriteKeepsMethodNotAllowed proves that a rewrite target reports 405.
// rewriteHandled used to read 405 as "no route matched" and answer 404.
func TestAppRewriteKeepsMethodNotAllowed(t *testing.T) {
	app := New()
	app.Page("POST /new", func(ctx *Context) gosx.Node {
		return gosx.Text("created")
	})
	app.Rewrite("/old", "/new")

	handler := app.Build()
	res := httptest.NewRecorder()
	handler.ServeHTTP(res, httptest.NewRequest(http.MethodGet, "/old", nil))

	if res.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405 from the rewrite target, got %d", res.Code)
	}
}

// TestISRColdStartRunsOneRegeneration proves that concurrent cold requests for a
// missing artifact share one render. The first-generation path used to skip the
// refresh lease that the stale path already takes.
func TestISRColdStartRunsOneRegeneration(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "static"), 0755); err != nil {
		t.Fatal(err)
	}
	writeISRManifest(t, root, isrManifest{
		Routes: []isrRoute{{Path: "/", File: "index.html"}},
	})

	app := New()
	app.SetRuntimeRoot(root)
	app.EnableISR()

	var mu sync.Mutex
	renders := 0
	app.Page("GET /", func(ctx *Context) gosx.Node {
		mu.Lock()
		renders++
		mu.Unlock()
		time.Sleep(20 * time.Millisecond)
		return gosx.Text("generated home")
	})

	handler := app.Build()

	const concurrency = 40
	var start sync.WaitGroup
	var done sync.WaitGroup
	start.Add(1)
	done.Add(concurrency)
	for i := 0; i < concurrency; i++ {
		go func() {
			defer done.Done()
			start.Wait()
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.Header.Set("Accept", "text/html")
			handler.ServeHTTP(httptest.NewRecorder(), req)
		}()
	}
	start.Done()
	done.Wait()

	mu.Lock()
	got := renders
	mu.Unlock()
	if got != 1 {
		t.Fatalf("expected one cold render under %d concurrent requests, got %d", concurrency, got)
	}
}

// TestISRWriteArtifactUsesUniqueTempName proves that two concurrent writers do
// not share one temporary path. A shared name lets one writer truncate the file
// that the other renames.
func TestISRWriteArtifactUsesUniqueTempName(t *testing.T) {
	root := t.TempDir()
	store := NewInMemoryISRStore()

	var wg sync.WaitGroup
	var seenMu sync.Mutex
	seen := map[string]struct{}{}
	stop := make(chan struct{})
	var watcher sync.WaitGroup
	watcher.Add(1)
	go func() {
		defer watcher.Done()
		for {
			select {
			case <-stop:
				return
			default:
			}
			entries, err := os.ReadDir(root)
			if err != nil {
				continue
			}
			for _, entry := range entries {
				if !strings.Contains(entry.Name(), ".tmp") {
					continue
				}
				seenMu.Lock()
				seen[entry.Name()] = struct{}{}
				seenMu.Unlock()
			}
		}
	}()

	var writeErr error
	var errMu sync.Mutex
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for round := 0; round < 8; round++ {
				if _, err := store.WriteArtifact(root, "/", "index.html", []byte(strings.Repeat("x", 4096))); err != nil {
					errMu.Lock()
					if writeErr == nil {
						writeErr = err
					}
					errMu.Unlock()
					return
				}
			}
		}()
	}
	wg.Wait()
	close(stop)
	watcher.Wait()

	if writeErr != nil {
		t.Fatalf("concurrent artifact writes failed: %v", writeErr)
	}
	if _, shared := seen["index.html.tmp"]; shared {
		t.Fatalf("expected per-writer temporary names, saw the shared name index.html.tmp")
	}
}

// TestPathVersionStaysBounded proves that per-item invalidation does not grow
// the revalidation store without limit. The store used to keep one entry per
// invalidated path forever and scan them all on every cacheable request.
func TestPathVersionStaysBounded(t *testing.T) {
	store := NewInMemoryRevalidationStore()
	const total = 50000
	for i := 0; i < total; i++ {
		store.RevalidatePath("/blog/post-" + strconv.Itoa(i))
	}
	if size := store.pathVersionCount(); size >= total {
		t.Fatalf("expected the path version map to stay bounded, got %d entries", size)
	}
	// The newest entry must still answer correctly.
	newest := "/blog/post-" + strconv.Itoa(total-1)
	if store.PathVersion(newest) == 0 {
		t.Fatalf("expected the newest invalidation to remain visible")
	}
}

// TestSharedCacheablePageOmitsPerRequestValues proves that a page marked
// publicly cacheable ships no request ID and no nonce. A shared cache would
// otherwise replay one client's values to the next client.
func TestSharedCacheablePageOmitsPerRequestValues(t *testing.T) {
	app := New()
	app.EnableNavigation()
	app.Page("GET /shared", func(ctx *Context) gosx.Node {
		ctx.SetNonce("nonce-should-not-ship")
		ctx.CachePublic(time.Minute)
		return gosx.El("main", gosx.Text("shared"))
	})

	handler := app.Build()
	req := httptest.NewRequest(http.MethodGet, "/shared", nil)
	res := httptest.NewRecorder()
	handler.ServeHTTP(res, req)

	body := res.Body.String()
	if strings.Contains(body, "nonce-should-not-ship") {
		t.Fatalf("expected no nonce in a shared-cacheable body")
	}
	var contract struct {
		Page struct {
			RequestID string `json:"requestID"`
		} `json:"page"`
	}
	payload := documentContractPayload(t, body)
	if err := json.Unmarshal([]byte(payload), &contract); err != nil {
		t.Fatalf("decode document contract: %v (payload %q)", err, payload)
	}
	if contract.Page.RequestID != "" {
		t.Fatalf("expected no request ID in a shared-cacheable body, got %q", contract.Page.RequestID)
	}
}

// TestPrivatePageKeepsPerRequestValues is the control. A private response may
// carry the request ID and the nonce.
func TestPrivatePageKeepsPerRequestValues(t *testing.T) {
	app := New()
	app.Page("GET /private", func(ctx *Context) gosx.Node {
		ctx.SetNonce("private-nonce")
		ctx.CachePrivateData(time.Minute)
		return gosx.El("main", gosx.Text("private"))
	})

	handler := app.Build()
	res := httptest.NewRecorder()
	handler.ServeHTTP(res, httptest.NewRequest(http.MethodGet, "/private", nil))

	if body := res.Body.String(); !strings.Contains(body, "private-nonce") {
		t.Fatalf("expected the nonce on a private response, got %q", body)
	}
}

func documentContractPayload(t *testing.T, body string) string {
	t.Helper()
	const open = `<script id="gosx-document"`
	idx := strings.Index(body, open)
	if idx < 0 {
		t.Fatalf("no document contract script in %q", body)
	}
	rest := body[idx:]
	start := strings.Index(rest, ">")
	end := strings.Index(rest, "</script>")
	if start < 0 || end < 0 || end < start {
		t.Fatalf("malformed document contract script in %q", rest)
	}
	return rest[start+1 : end]
}

// TestDeferredChunkCarriesNonce proves that a streamed chunk's inline script
// carries the request nonce. Without it a nonce-based CSP blocks every
// ctx.Defer and ctx.Suspense boundary.
func TestDeferredChunkCarriesNonce(t *testing.T) {
	app := New()
	app.Page("GET /deferred", func(ctx *Context) gosx.Node {
		ctx.SetNonce("chunk-nonce")
		return gosx.El("main",
			ctx.Defer(gosx.Text("loading"), func() (gosx.Node, error) {
				return gosx.Text("resolved"), nil
			}),
		)
	})

	handler := app.Build()
	res := httptest.NewRecorder()
	handler.ServeHTTP(res, httptest.NewRequest(http.MethodGet, "/deferred", nil))

	body := res.Body.String()
	if !strings.Contains(body, "resolved") {
		t.Fatalf("expected the resolved chunk, got %q", body)
	}
	if !strings.Contains(body, `<script nonce="chunk-nonce">(function(){var slot=`) {
		t.Fatalf("expected the streamed chunk script to carry the nonce, got %q", body)
	}
}

// TestEnableCSPSetsHeadersAndNonce proves that the opt-in policy emits a
// Content-Security-Policy header with a generated nonce and that the same nonce
// reaches the rendered document.
func TestEnableCSPSetsHeadersAndNonce(t *testing.T) {
	app := New()
	app.EnableSecurityPolicy(SecurityPolicy{
		ContentSecurityPolicy:   "default-src 'self'; script-src 'self' 'nonce-{nonce}'",
		FrameOptions:            "DENY",
		StrictTransportSecurity: "max-age=63072000; includeSubDomains",
	})
	app.Page("GET /csp", func(ctx *Context) gosx.Node {
		return gosx.El("main", gosx.Text("csp"))
	})

	handler := app.Build()
	res := httptest.NewRecorder()
	handler.ServeHTTP(res, httptest.NewRequest(http.MethodGet, "/csp", nil))

	policy := res.Header().Get("Content-Security-Policy")
	if policy == "" {
		t.Fatalf("expected a Content-Security-Policy header, got none")
	}
	if strings.Contains(policy, "{nonce}") {
		t.Fatalf("expected the nonce placeholder to be replaced, got %q", policy)
	}
	if res.Header().Get("X-Frame-Options") != "DENY" {
		t.Fatalf("expected X-Frame-Options, got %q", res.Header().Get("X-Frame-Options"))
	}
	if res.Header().Get("Strict-Transport-Security") == "" {
		t.Fatalf("expected Strict-Transport-Security, got none")
	}

	nonce := cspNonceFromPolicy(t, policy)
	if len(nonce) < 16 {
		t.Fatalf("expected a long generated nonce, got %q", nonce)
	}
	if !strings.Contains(res.Body.String(), `nonce="`+nonce+`"`) {
		t.Fatalf("expected the generated nonce on a document script, body %q", res.Body.String())
	}

	second := httptest.NewRecorder()
	handler.ServeHTTP(second, httptest.NewRequest(http.MethodGet, "/csp", nil))
	if cspNonceFromPolicy(t, second.Header().Get("Content-Security-Policy")) == nonce {
		t.Fatalf("expected a fresh nonce per request")
	}
}

// TestSecurityPolicyStaysOptIn proves that the default app emits no CSP. A
// default policy would break apps that already ship inline scripts.
func TestSecurityPolicyStaysOptIn(t *testing.T) {
	app := New()
	app.Page("GET /plain", func(ctx *Context) gosx.Node {
		return gosx.Text("plain")
	})
	handler := app.Build()
	res := httptest.NewRecorder()
	handler.ServeHTTP(res, httptest.NewRequest(http.MethodGet, "/plain", nil))
	if got := res.Header().Get("Content-Security-Policy"); got != "" {
		t.Fatalf("expected no default policy, got %q", got)
	}
}

func cspNonceFromPolicy(t *testing.T, policy string) string {
	t.Helper()
	const marker = "'nonce-"
	idx := strings.Index(policy, marker)
	if idx < 0 {
		t.Fatalf("no nonce in policy %q", policy)
	}
	rest := policy[idx+len(marker):]
	end := strings.Index(rest, "'")
	if end < 0 {
		t.Fatalf("unterminated nonce in policy %q", policy)
	}
	return rest[:end]
}

// TestSharedCacheablePageDropsNoncePolicy proves that a shared-cacheable
// response sends the nonce-free policy. The body carries no nonce on those
// responses, so a nonce source in the header would name a script that the body
// does not contain, and the header itself could not be cached.
func TestSharedCacheablePageDropsNoncePolicy(t *testing.T) {
	app := New()
	app.EnableSecurityPolicy(SecurityPolicy{
		ContentSecurityPolicy: "default-src 'self'; script-src 'self' 'nonce-{nonce}'",
	})
	app.Page("GET /shared-csp", func(ctx *Context) gosx.Node {
		ctx.CachePublic(time.Minute)
		return gosx.El("main", gosx.Text("shared"))
	})

	handler := app.Build()
	res := httptest.NewRecorder()
	handler.ServeHTTP(res, httptest.NewRequest(http.MethodGet, "/shared-csp", nil))

	policy := res.Header().Get("Content-Security-Policy")
	if strings.Contains(policy, "'nonce-") {
		t.Fatalf("expected no nonce source on a shared-cacheable response, got %q", policy)
	}
	if policy != "default-src 'self'; script-src 'self'" {
		t.Fatalf("unexpected shared policy %q", policy)
	}
	if strings.Contains(res.Body.String(), "nonce=") {
		t.Fatalf("expected no nonce attribute in a shared-cacheable body")
	}
}

func TestSharedCacheableAppStripsNonceBakedByHandlerScripts(t *testing.T) {
	app := New()
	app.EnableSecurityPolicy(SecurityPolicy{
		ContentSecurityPolicy: "default-src 'self' 'unsafe-inline'; script-src 'nonce-{nonce}'",
	})
	var issuedNonce string
	app.Page("GET /shared-authored-scripts", func(ctx *Context) gosx.Node {
		issuedNonce = ctx.Nonce()
		ctx.CachePublic(time.Minute)
		ctx.AddHead(ctx.InlineScript("window.appHeadScript = true"))
		return gosx.El("main",
			ctx.InlineScript("window.appBodyScript = true"),
			ctx.JSONScript("app-json", map[string]string{"status": "shared"}),
		)
	})

	res := httptest.NewRecorder()
	app.Build().ServeHTTP(res, httptest.NewRequest(http.MethodGet, "/shared-authored-scripts", nil))

	if issuedNonce == "" {
		t.Fatal("expected the handler to observe a request nonce before CachePublic")
	}
	if got, want := res.Header().Get("Content-Security-Policy"), "default-src 'self' 'unsafe-inline'; script-src 'none'"; got != want {
		t.Fatalf("shared policy = %q, want %q", got, want)
	}
	body := res.Body.String()
	if strings.Contains(body, issuedNonce) || strings.Contains(body, "nonce=") {
		t.Fatalf("shared body retained request nonce %q: %s", issuedNonce, body)
	}
	for _, want := range []string{
		"window.appHeadScript = true",
		"window.appBodyScript = true",
		`id="app-json"`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("shared body missing %q: %s", want, body)
		}
	}
}

// TestSharedCacheableETagRepeatsAcrossRequests proves that two requests for one
// shared-cacheable page share a validator. A per-request value left in the body
// would change the digest on every request.
func TestSharedCacheableETagRepeatsAcrossRequests(t *testing.T) {
	app := New()
	app.Page("GET /stable", func(ctx *Context) gosx.Node {
		ctx.CachePublic(time.Minute)
		return gosx.El("main", gosx.Text("stable"))
	})

	handler := app.Build()
	first := httptest.NewRecorder()
	handler.ServeHTTP(first, httptest.NewRequest(http.MethodGet, "/stable", nil))
	second := httptest.NewRecorder()
	handler.ServeHTTP(second, httptest.NewRequest(http.MethodGet, "/stable", nil))

	if first.Body.String() != second.Body.String() {
		t.Fatalf("expected an identical shared body, got\n%q\nand\n%q", first.Body.String(), second.Body.String())
	}
	if tag := first.Header().Get("ETag"); tag == "" || tag != second.Header().Get("ETag") {
		t.Fatalf("expected one shared validator, got %q and %q", tag, second.Header().Get("ETag"))
	}
}
