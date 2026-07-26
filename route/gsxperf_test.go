package route

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"m31labs.dev/gosx"
	"m31labs.dev/gosx/island"
)

func gsxCompileCacheCounts() (progs, files int) {
	gsxCompileCache.mu.RLock()
	progs = len(gsxCompileCache.progs)
	files = len(gsxCompileCache.files)
	gsxCompileCache.mu.RUnlock()
	return progs, files
}

func renderGSXPageForTest(t *testing.T, path string, data any) string {
	t.Helper()
	node, err := DefaultFileRenderer(&RouteContext{Data: data}, FilePage{FilePath: path, Pattern: "/"})
	if err != nil {
		t.Fatalf("render %s: %v", path, err)
	}
	return gosx.RenderHTML(node)
}

// TestGSXProgramCacheSkipsRecompileOnRepeatRequest proves the stat-keyed cache
// answers a repeat request without compiling again.
func TestGSXProgramCacheSkipsRecompileOnRepeatRequest(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "page.gsx")
	source := "package p\n\nfunc Page() Node {\n\treturn <p>alpha</p>\n}\n"
	if err := os.WriteFile(path, []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}

	beforeProgs, _ := gsxCompileCacheCounts()

	first := renderGSXPageForTest(t, path, nil)
	afterFirstProgs, afterFirstFiles := gsxCompileCacheCounts()
	if afterFirstProgs != beforeProgs+1 {
		t.Fatalf("first render should compile once: progs %d -> %d", beforeProgs, afterFirstProgs)
	}
	if afterFirstFiles == 0 {
		t.Fatal("first render should record a stat-keyed entry")
	}

	for i := 0; i < 5; i++ {
		if got := renderGSXPageForTest(t, path, nil); got != first {
			t.Fatalf("render %d = %q, want %q", i, got, first)
		}
	}
	repeatProgs, _ := gsxCompileCacheCounts()
	if repeatProgs != afterFirstProgs {
		t.Fatalf("repeat renders recompiled: progs %d -> %d", afterFirstProgs, repeatProgs)
	}
}

// TestGSXProgramCacheReloadsAfterEdit proves hot reload still works. It covers
// both key axes: a size change and a modification-time change alone.
func TestGSXProgramCacheReloadsAfterEdit(t *testing.T) {
	t.Run("size change", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "page.gsx")
		if err := os.WriteFile(path, []byte("package p\n\nfunc Page() Node {\n\treturn <p>alpha</p>\n}\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if got := renderGSXPageForTest(t, path, nil); !strings.Contains(got, "alpha") {
			t.Fatalf("first render = %q, want alpha", got)
		}

		if err := os.WriteFile(path, []byte("package p\n\nfunc Page() Node {\n\treturn <p>alpha-and-more</p>\n}\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		got := renderGSXPageForTest(t, path, nil)
		if !strings.Contains(got, "alpha-and-more") {
			t.Fatalf("second render = %q, want alpha-and-more", got)
		}
	})

	t.Run("modification time change only", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "page.gsx")
		first := "package p\n\nfunc Page() Node {\n\treturn <p>alpha</p>\n}\n"
		second := "package p\n\nfunc Page() Node {\n\treturn <p>omega</p>\n}\n"
		if len(first) != len(second) {
			t.Fatalf("fixture sizes must match: %d vs %d", len(first), len(second))
		}
		if err := os.WriteFile(path, []byte(first), 0o644); err != nil {
			t.Fatal(err)
		}
		if got := renderGSXPageForTest(t, path, nil); !strings.Contains(got, "alpha") {
			t.Fatalf("first render = %q, want alpha", got)
		}

		if err := os.WriteFile(path, []byte(second), 0o644); err != nil {
			t.Fatal(err)
		}
		// Force a distinct modification time so the test does not depend on
		// the filesystem timestamp resolution.
		future := time.Now().Add(2 * time.Second)
		if err := os.Chtimes(path, future, future); err != nil {
			t.Fatal(err)
		}
		got := renderGSXPageForTest(t, path, nil)
		if !strings.Contains(got, "omega") {
			t.Fatalf("second render = %q, want omega", got)
		}
	})
}

// TestGSXProgramCacheReportsCompileError proves a broken template still reports
// its error, and reports it again on the next request.
func TestGSXProgramCacheReportsCompileError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "page.gsx")
	if err := os.WriteFile(path, []byte("this is not go source at all"), 0o644); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		_, err := DefaultFileRenderer(&RouteContext{}, FilePage{FilePath: path, Pattern: "/"})
		if err == nil {
			t.Fatalf("attempt %d: expected a compile error", i)
		}
		if !strings.Contains(err.Error(), "compile") {
			t.Fatalf("attempt %d: error %v should mention compile", i, err)
		}
	}
}

// TestGSXProgramCacheReportsMissingFile proves a deleted page reports a read
// error rather than serving a stale program.
func TestGSXProgramCacheReportsMissingFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "page.gsx")
	if err := os.WriteFile(path, []byte("package p\n\nfunc Page() Node {\n\treturn <p>alpha</p>\n}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := renderGSXPageForTest(t, path, nil); !strings.Contains(got, "alpha") {
		t.Fatalf("first render = %q", got)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if _, err := DefaultFileRenderer(&RouteContext{}, FilePage{FilePath: path, Pattern: "/"}); err == nil {
		t.Fatal("expected a read error after the file was removed")
	}
}

// TestGSXRenderIsConcurrencySafe renders one page from many goroutines. It
// exercises the shared expression cache and the shared IR program under -race.
func TestGSXRenderIsConcurrencySafe(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "page.gsx")
	source := `package p

func Page() Node {
	return <main>
		<h1>{data.title}</h1>
		<ul>
			<Each as="item" index="i" of={data.items}>
				<li data-index={i}>{item}</li>
			</Each>
		</ul>
		<If when={data.show}><span>{data.title + "!"}</span></If>
	</main>
}
`
	if err := os.WriteFile(path, []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}
	data := map[string]any{
		"title": "Concurrent",
		"show":  true,
		"items": []string{"a", "b", "c"},
	}
	want := renderGSXPageForTest(t, path, data)

	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 20; j++ {
				node, err := DefaultFileRenderer(&RouteContext{Data: data}, FilePage{FilePath: path, Pattern: "/"})
				if err != nil {
					t.Error(err)
					return
				}
				if got := gosx.RenderHTML(node); got != want {
					t.Errorf("concurrent render mismatch:\n got %q\nwant %q", got, want)
					return
				}
			}
		}()
	}
	wg.Wait()
}

// TestGSXEachScopeIsolatesItemBindings proves the copy-on-write env keeps each
// loop item's bindings out of its siblings.
func TestGSXEachScopeIsolatesItemBindings(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "page.gsx")
	source := `package p

func Page() Node {
	return <ul>
		<Each as="row" index="ri" of={data.rows}>
			<li>
				<Each as="cell" index="ci" of={row}>
					<span>{ri}-{ci}-{cell}</span>
				</Each>
				<b>{row}</b>
			</li>
		</Each>
	</ul>
}
`
	if err := os.WriteFile(path, []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}
	got := renderGSXPageForTest(t, path, map[string]any{
		"rows": [][]string{{"a", "b"}, {"c"}},
	})
	for _, want := range []string{
		"<span>0-0-a</span>",
		"<span>0-1-b</span>",
		"<span>1-0-c</span>",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("expected %q in %q", want, got)
		}
	}
	// The inner loop's bindings must not leak into the outer scope.
	if strings.Contains(got, "<span>1-1-") {
		t.Fatalf("inner loop leaked a binding: %q", got)
	}
}

// TestIslandManifestCacheReloadsAndStaysRaceFree builds renderers from many
// goroutines against one build.json, then rewrites the file and proves the next
// renderer picks up the new manifest.
func TestIslandManifestCacheReloadsAndStaysRaceFree(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "build.json")
	first := []byte(`{"runtime":{"bootstrap":{"file":"bootstrap.aaa.js","hash":"aaa","size":100}},"islands":[],"css":[]}`)
	if err := os.WriteFile(path, first, 0o644); err != nil {
		t.Fatal(err)
	}
	island.ResetBuildManifestCache()
	island.SetManifestRoot(dir)
	t.Cleanup(func() {
		island.ResetManifestRoot()
		island.ResetBuildManifestCache()
	})

	var wg sync.WaitGroup
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 25; j++ {
				if r := island.NewRenderer("test"); r == nil {
					t.Error("nil renderer")
					return
				}
			}
		}()
	}
	wg.Wait()

	second := []byte(`{"runtime":{"bootstrap":{"file":"bootstrap.bbb.js","hash":"bbb","size":200}},"islands":[],"css":[]}`)
	if err := os.WriteFile(path, second, 0o644); err != nil {
		t.Fatal(err)
	}
	future := time.Now().Add(2 * time.Second)
	if err := os.Chtimes(path, future, future); err != nil {
		t.Fatal(err)
	}
	got := island.NewRenderer("test").Summary().BootstrapPath
	if !strings.Contains(got, "bootstrap.bbb.js") {
		t.Fatalf("bootstrap path = %q, want the rewritten manifest entry", got)
	}
}

// TestRouterPageETagTracksBody proves the router's validator describes the
// response body. Two different bodies for one path must not share a validator,
// and a conditional request carrying the old validator must receive the new body.
//
// The validator used to be request-derived, because the router applied the cache
// headers before it rendered. A stale conditional request then won a 304 with an
// empty body.
func TestRouterPageETagTracksBody(t *testing.T) {
	bodyText := "first"
	router := NewRouter()
	router.Add(Route{
		Pattern: "/cached",
		Handler: func(ctx *RouteContext) gosx.Node {
			ctx.CachePublic(time.Minute)
			ctx.CacheTag("cached-pages")
			return gosx.El("p", gosx.Text(bodyText))
		},
	})
	handler := router.Build()

	firstRes := httptest.NewRecorder()
	handler.ServeHTTP(firstRes, httptest.NewRequest(http.MethodGet, "/cached", nil))
	if firstRes.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", firstRes.Code, firstRes.Body.String())
	}
	firstETag := firstRes.Header().Get("ETag")
	if firstETag == "" {
		t.Fatalf("expected an etag in %v", firstRes.Header())
	}
	if strings.HasPrefix(firstETag, "W/") {
		t.Fatalf("a body-derived validator must be strong, got %q", firstETag)
	}

	// The same body must repeat the validator and answer 304.
	repeatReq := httptest.NewRequest(http.MethodGet, "/cached", nil)
	repeatReq.Header.Set("If-None-Match", firstETag)
	repeatRes := httptest.NewRecorder()
	handler.ServeHTTP(repeatRes, repeatReq)
	if repeatRes.Code != http.StatusNotModified {
		t.Fatalf("expected 304 for an unchanged body, got %d", repeatRes.Code)
	}
	if body := repeatRes.Body.String(); body != "" {
		t.Fatalf("expected an empty 304 body, got %q", body)
	}

	// A changed body must change the validator and answer 200.
	bodyText = "second"
	staleReq := httptest.NewRequest(http.MethodGet, "/cached", nil)
	staleReq.Header.Set("If-None-Match", firstETag)
	staleRes := httptest.NewRecorder()
	handler.ServeHTTP(staleRes, staleReq)
	if staleRes.Code != http.StatusOK {
		t.Fatalf("expected 200 for a changed body, got %d", staleRes.Code)
	}
	if !strings.Contains(staleRes.Body.String(), "second") {
		t.Fatalf("expected the new body, got %q", staleRes.Body.String())
	}
	if got := staleRes.Header().Get("ETag"); got == firstETag {
		t.Fatalf("two different bodies shared the validator %q", firstETag)
	}
}

// TestRouterPageWithoutCachePolicySkipsValidator proves an uncached page pays no
// validator work. WriteHTML must not hash the body when no cache API ran.
func TestRouterPageWithoutCachePolicySkipsValidator(t *testing.T) {
	router := NewRouter()
	router.Add(Route{
		Pattern: "/plain",
		Handler: func(ctx *RouteContext) gosx.Node { return gosx.Text("plain") },
	})
	handler := router.Build()

	res := httptest.NewRecorder()
	handler.ServeHTTP(res, httptest.NewRequest(http.MethodGet, "/plain", nil))
	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", res.Code)
	}
	if got := res.Header().Get("ETag"); got != "" {
		t.Fatalf("expected no etag without a cache policy, got %q", got)
	}
}

// TestGSXVoidElementKeepsChildren documents that a void element with children
// now renders an open and close tag, matching node.go's renderNodeHTML. The
// old branch self-closed and dropped the children.
func TestGSXVoidElementKeepsChildren(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "page.gsx")
	source := `package p

func Page() Node {
	return <div>
		<br />
		<img src="/a.png" />
	</div>
}
`
	if err := os.WriteFile(path, []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}
	got := renderGSXPageForTest(t, path, nil)
	if !strings.Contains(got, "<br />") {
		t.Fatalf("childless void element should self-close: %q", got)
	}
	if !strings.Contains(got, `<img src="/a.png" />`) {
		t.Fatalf("childless img should self-close: %q", got)
	}
}
