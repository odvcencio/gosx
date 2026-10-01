package main

import (
	"fmt"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"sync"
	"testing"

	"m31labs.dev/gosx/server"
)

// docsRoutePaths lists the URL path of every routed directory under app/ (a
// directory with a page.gsx or page.server.go), with dynamic segments filled in.
func docsRoutePaths(t *testing.T, root string) []string {
	t.Helper()
	appDir := filepath.Join(root, "app")
	seen := map[string]bool{}
	err := filepath.WalkDir(appDir, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || (entry.Name() != "page.gsx" && entry.Name() != "page.server.go") {
			return nil
		}
		rel, err := filepath.Rel(appDir, filepath.Dir(path))
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if rel == "." {
			seen["/"] = true
			return nil
		}
		parts := strings.Split(rel, "/")
		for i, part := range parts {
			if strings.HasPrefix(part, "[") && strings.HasSuffix(part, "]") {
				parts[i] = "example"
			}
		}
		seen["/"+strings.Join(parts, "/")] = true
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	paths := make([]string, 0, len(seen))
	for path := range seen {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	return paths
}

// TestEveryDocsRouteServesConcurrently sends concurrent requests to every docs
// route, and a mixed burst across all of them, through the real handler. Run
// with -race it catches page loaders and route helpers that share mutable state
// across requests: the water page once wrote per-request keys into one shared
// map, so 32 concurrent visitors could crash the live site with "fatal error:
// concurrent map writes".
func TestEveryDocsRouteServesConcurrently(t *testing.T) {
	_, thisFile, _, _ := runtime.Caller(0)
	root := server.ResolveAppRoot(thisFile)
	app, err := buildDocsApp(root, "8080")
	if err != nil {
		t.Fatal(err)
	}
	handler := app.Build()
	paths := docsRoutePaths(t, root)
	if len(paths) < 40 {
		t.Fatalf("found %d docs routes, want at least 40: %v", len(paths), paths)
	}
	// Variants that make the loader write different per-request values.
	paths = append(paths,
		"/demos/water?quality=hero&diag=1&dpr=1.9",
		"/demos/water?quality=battery&caustics=0&res=96",
		"/demos/water?quality=balanced&meshRes=64&reflection=0",
	)

	get := func(target string) (int, string) {
		req := httptest.NewRequest(http.MethodGet, target, nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		// Directory routes redirect to their canonical trailing-slash URL.
		// Race the rendered page too, rather than dropping these routes.
		if rec.Code == http.StatusMovedPermanently || rec.Code == http.StatusPermanentRedirect {
			location := rec.Header().Get("Location")
			if strings.HasPrefix(location, "/") && !strings.HasPrefix(location, "//") {
				rec = httptest.NewRecorder()
				handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, location, nil))
			}
		}
		return rec.Code, rec.Body.String()
	}
	// A route that cannot serve at all (for example a missing optional asset)
	// would fail this test for a reason unrelated to races, so only routes that
	// answer 200 when requested alone are raced.
	var routes []string
	for _, path := range paths {
		if code, _ := get(path); code == http.StatusOK {
			routes = append(routes, path)
		} else {
			t.Logf("%s answers %d alone; not raced", path, code)
		}
	}
	if len(routes) < 40 {
		t.Fatalf("only %d routes answer 200; want at least 40", len(routes))
	}

	const workers, perWorker = 8, 2
	var wg sync.WaitGroup
	errs := make(chan string, len(routes)*workers*perWorker)
	for _, path := range routes {
		for worker := 0; worker < workers; worker++ {
			wg.Add(1)
			go func(path string) {
				defer wg.Done()
				for i := 0; i < perWorker; i++ {
					if code, _ := get(path); code != http.StatusOK {
						errs <- fmt.Sprintf("%s answered %d under concurrent load", path, code)
						return
					}
				}
			}(path)
		}
	}
	wg.Wait()
	close(errs)
	for msg := range errs {
		t.Error(msg)
	}
}

var requestIDPattern = regexp.MustCompile(`"requestID":"[^"]*"`)

// One visitor's diagnostic query must never show up in another visitor's page.
// Each page carries a per-request ID, which is the only expected difference
// between two renders of the same URL.
func TestWaterPageKeepsDiagnosticKnobsPerRequest(t *testing.T) {
	_, thisFile, _, _ := runtime.Caller(0)
	root := server.ResolveAppRoot(thisFile)
	app, err := buildDocsApp(root, "8080")
	if err != nil {
		t.Fatal(err)
	}
	handler := app.Build()
	render := func(target string) (int, string) {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, target, nil))
		return rec.Code, requestIDPattern.ReplaceAllString(rec.Body.String(), `"requestID":""`)
	}
	urls := []string{
		"/demos/water",
		"/demos/water?quality=hero&meshRes=77",
		"/demos/water?quality=battery&caustics=0&res=96",
	}
	want := make([]string, len(urls))
	for i, target := range urls {
		code, body := render(target)
		if code != http.StatusOK {
			t.Fatalf("%s answered %d", target, code)
		}
		want[i] = body
	}
	if want[0] == want[1] || want[0] == want[2] || want[1] == want[2] {
		t.Fatal("the diagnostic queries change nothing; the test does not exercise per-request state")
	}
	var wg sync.WaitGroup
	errs := make(chan string, 64)
	for worker := 0; worker < 12; worker++ {
		wg.Add(1)
		go func(worker int) {
			defer wg.Done()
			for i := 0; i < 4; i++ {
				which := (worker + i) % len(urls)
				code, body := render(urls[which])
				if code != http.StatusOK || body != want[which] {
					errs <- fmt.Sprintf("%s served a page that differs from its own reference (status %d): another request's knobs leaked in", urls[which], code)
					return
				}
			}
		}(worker)
	}
	wg.Wait()
	close(errs)
	for msg := range errs {
		t.Error(msg)
	}
}
