package route

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"m31labs.dev/gosx"
	"m31labs.dev/gosx/island"
	"m31labs.dev/gosx/server"
)

// The benchmarks below lock in the .gsx server-render hot path. Each request
// used to re-read the file, re-hash the bytes, re-parse every {expr} hole and
// copy three maps per loop binding. Keep these benchmarks so a regression on
// any of those four costs shows up as a jump in B/op and allocs/op.

const benchGSXListSource = `package bench

func Page() Node {
	return <main class="page">
		<h1>{data.title}</h1>
		<If when={data.show}>
			<p class="lede">{data.label}</p>
		</If>
		<ul class="items">
			<Each as="item" index="i" of={data.items}>
				<li data-index={i} class="item">
					<span class="name">{item.name}</span>
					<span class="score">{item.score + 1}</span>
				</li>
			</Each>
		</ul>
	</main>
}
`

func benchGSXWriteSource(tb testing.TB, source string) string {
	tb.Helper()
	root := tb.TempDir()
	path := filepath.Join(root, "page.gsx")
	if err := os.WriteFile(path, []byte(source), 0o644); err != nil {
		tb.Fatal(err)
	}
	return path
}

func benchGSXItems(count int) []map[string]any {
	items := make([]map[string]any, 0, count)
	for i := 0; i < count; i++ {
		items = append(items, map[string]any{
			"name":  fmt.Sprintf("item-%d", i),
			"score": i,
		})
	}
	return items
}

// benchGSXDeepSource nests `depth` <div> elements so the benchmark measures the
// per-node byte traffic of the renderer walk, not expression evaluation.
func benchGSXDeepSource(depth int) string {
	var b strings.Builder
	b.WriteString("package bench\n\nfunc Page() Node {\n\treturn ")
	for i := 0; i < depth; i++ {
		fmt.Fprintf(&b, `<div class="d%d">`, i)
	}
	b.WriteString("<span>{data.title}</span>")
	for i := 0; i < depth; i++ {
		b.WriteString("</div>")
	}
	b.WriteString("\n}\n")
	return b.String()
}

func benchGSXRender(b *testing.B, path string, data any) {
	ctx := &RouteContext{Data: data}
	page := FilePage{FilePath: path, Pattern: "/", Source: "page.gsx"}

	// Warm the compile cache so the benchmark measures steady-state request
	// cost, which is what production pays.
	if _, err := DefaultFileRenderer(ctx, page); err != nil {
		b.Fatal(err)
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		node, err := DefaultFileRenderer(ctx, page)
		if err != nil {
			b.Fatal(err)
		}
		if node.IsZero() {
			b.Fatal("empty node")
		}
	}
}

func BenchmarkGSXPageZeroItems(b *testing.B) {
	path := benchGSXWriteSource(b, benchGSXListSource)
	benchGSXRender(b, path, map[string]any{
		"title": "Bench",
		"show":  true,
		"label": "visible",
		"items": benchGSXItems(0),
	})
}

func BenchmarkGSXPage100Items(b *testing.B) {
	path := benchGSXWriteSource(b, benchGSXListSource)
	benchGSXRender(b, path, map[string]any{
		"title": "Bench",
		"show":  true,
		"label": "visible",
		"items": benchGSXItems(100),
	})
}

func BenchmarkGSXPageDepth100(b *testing.B) {
	path := benchGSXWriteSource(b, benchGSXDeepSource(100))
	benchGSXRender(b, path, map[string]any{"title": "Bench"})
}

// BenchmarkGSXPageOutputSize reports the rendered byte count of the depth-100
// case so the amplification ratio stays visible next to B/op.
func BenchmarkGSXPageOutputSize(b *testing.B) {
	path := benchGSXWriteSource(b, benchGSXDeepSource(100))
	ctx := &RouteContext{Data: map[string]any{"title": "Bench"}}
	page := FilePage{FilePath: path, Pattern: "/", Source: "page.gsx"}
	node, err := DefaultFileRenderer(ctx, page)
	if err != nil {
		b.Fatal(err)
	}
	outputBytes := float64(len(gosx.RenderHTML(node)))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := DefaultFileRenderer(ctx, page); err != nil {
			b.Fatal(err)
		}
	}
	b.StopTimer()
	// Report after the loop. ResetTimer drops metrics reported before it.
	b.ReportMetric(outputBytes, "output_bytes")
}

// benchManifestJSON mirrors a production build.json closely enough to measure
// the decode cost that NewRenderer used to pay twice per request.
const benchManifestJSON = `{
  "runtime": {
    "wasm": {"file": "runtime.a1b2c3.wasm", "hash": "a1b2c3", "size": 4194304},
    "wasmIslands": {"file": "islands.d4e5f6.wasm", "hash": "d4e5f6", "size": 2097152},
    "wasmExec": {"file": "wasm_exec.111.js", "hash": "111", "size": 18000},
    "standardGoWasmExec": {"file": "standard-go-wasm_exec.222.js", "hash": "222", "size": 21000},
    "patch": {"file": "patch.333.js", "hash": "333", "size": 9000},
    "bootstrap": {"file": "bootstrap.444.js", "hash": "444", "size": 61000},
    "bootstrapLite": {"file": "bootstrap-lite.555.js", "hash": "555", "size": 12000},
    "bootstrapRuntime": {"file": "bootstrap-runtime.666.js", "hash": "666", "size": 24000},
    "bootstrapFeatureIslands": {"file": "bootstrap-feature-islands.777.js", "hash": "777", "size": 15000},
    "bootstrapFeatureEngines": {"file": "bootstrap-feature-engines.888.js", "hash": "888", "size": 17000},
    "bootstrapFeatureHubs": {"file": "bootstrap-feature-hubs.999.js", "hash": "999", "size": 8000},
    "bootstrapFeatureScene3d": {"file": "bootstrap-feature-scene3d.aaa.js", "hash": "aaa", "size": 91000},
    "videoHls": {"file": "hls.min.bbb.js", "hash": "bbb", "size": 400000}
  },
  "islands": [
    {"name": "Counter", "file": "counter.111.json", "format": "json", "hash": "111", "size": 900},
    {"name": "Tabs", "file": "tabs.222.json", "format": "json", "hash": "222", "size": 1400},
    {"name": "Search", "file": "search.333.json", "format": "json", "hash": "333", "size": 2100},
    {"name": "Nav", "file": "nav.444.json", "format": "json", "hash": "444", "size": 1200}
  ],
  "css": []
}`

// BenchmarkIslandRendererConstruction measures the per-request fixed cost that
// server.NewPageRuntime pays. NewRenderer used to call
// loadDefaultBuildManifest twice, and each call re-read and re-decoded
// build.json. The measured cost was 419 allocations and 47.4 KB per request.
func BenchmarkIslandRendererConstruction(b *testing.B) {
	dir := b.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "build.json"), []byte(benchManifestJSON), 0o644); err != nil {
		b.Fatal(err)
	}
	island.ResetBuildManifestCache()
	island.SetManifestRoot(dir)
	b.Cleanup(func() {
		island.ResetManifestRoot()
		island.ResetBuildManifestCache()
	})

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if r := island.NewRenderer("bench"); r == nil {
			b.Fatal("nil renderer")
		}
	}
}

// BenchmarkPageRuntimeConstruction measures the whole per-request runtime
// registry, which wraps island.NewRenderer.
func BenchmarkPageRuntimeConstruction(b *testing.B) {
	dir := b.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "build.json"), []byte(benchManifestJSON), 0o644); err != nil {
		b.Fatal(err)
	}
	island.ResetBuildManifestCache()
	island.SetManifestRoot(dir)
	b.Cleanup(func() {
		island.ResetManifestRoot()
		island.ResetBuildManifestCache()
	})

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if rt := server.NewPageRuntime(); rt == nil {
			b.Fatal("nil runtime")
		}
	}
}

// benchCachedPageRouter serves one cached page whose body is stable across
// requests, so a conditional request can match the validator.
func benchCachedPageRouter() http.Handler {
	router := NewRouter()
	router.Add(Route{
		Pattern: "/cached",
		Handler: func(ctx *RouteContext) gosx.Node {
			ctx.CachePublic(time.Minute)
			ctx.CacheTag("bench-pages")
			return gosx.El("main",
				gosx.Attrs(gosx.Attr("class", "page")),
				gosx.El("h1", gosx.Text("Cached")),
				gosx.El("p", gosx.Text(strings.Repeat("body text. ", 64))),
			)
		},
	})
	return router.Build()
}

// BenchmarkCachedPage200 measures a cached page that returns a full body. The
// ETag now describes the body, so the response pays one extra copy of the
// rendered document for the digest.
func BenchmarkCachedPage200(b *testing.B) {
	handler := benchCachedPageRouter()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		res := httptest.NewRecorder()
		handler.ServeHTTP(res, httptest.NewRequest(http.MethodGet, "/cached", nil))
		if res.Code != http.StatusOK {
			b.Fatalf("status %d", res.Code)
		}
	}
}

// BenchmarkCachedPage304 measures a conditional request that matches. An honest
// content-derived validator needs the body, so a 304 costs one render. It still
// saves the body transfer.
func BenchmarkCachedPage304(b *testing.B) {
	handler := benchCachedPageRouter()

	warm := httptest.NewRecorder()
	handler.ServeHTTP(warm, httptest.NewRequest(http.MethodGet, "/cached", nil))
	etag := warm.Header().Get("ETag")
	if etag == "" {
		b.Fatalf("expected an etag in %v", warm.Header())
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		req := httptest.NewRequest(http.MethodGet, "/cached", nil)
		req.Header.Set("If-None-Match", etag)
		res := httptest.NewRecorder()
		handler.ServeHTTP(res, req)
		if res.Code != http.StatusNotModified {
			b.Fatalf("status %d", res.Code)
		}
	}
}

// BenchmarkUncachedPage200 is the control: a page with no cache policy must skip
// the validator work entirely.
func BenchmarkUncachedPage200(b *testing.B) {
	router := NewRouter()
	router.Add(Route{
		Pattern: "/plain",
		Handler: func(ctx *RouteContext) gosx.Node {
			return gosx.El("main",
				gosx.Attrs(gosx.Attr("class", "page")),
				gosx.El("h1", gosx.Text("Plain")),
				gosx.El("p", gosx.Text(strings.Repeat("body text. ", 64))),
			)
		},
	})
	handler := router.Build()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		res := httptest.NewRecorder()
		handler.ServeHTTP(res, httptest.NewRequest(http.MethodGet, "/plain", nil))
		if res.Code != http.StatusOK {
			b.Fatalf("status %d", res.Code)
		}
	}
}

// BenchmarkGSXExprEval isolates the {expr} evaluation path. Before the parsed
// expression cache this called go/parser.ParseExpr on every invocation.
func BenchmarkGSXExprEval(b *testing.B) {
	env := fileRenderEnv{values: map[string]any{
		"data": map[string]any{"count": 41, "name": "gosx"},
	}}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if v := evalFileExpr("data.count + 1", env); v == nil {
			b.Fatal("nil value")
		}
	}
}

// BenchmarkGSXRenderEnvWithValue isolates the loop-binding cost. Before the
// copy-on-write env this copied three maps per call.
func BenchmarkGSXRenderEnvWithValue(b *testing.B) {
	env := fileRenderEnv{
		values:     map[string]any{"data": nil, "params": nil, "query": nil, "session": nil, "flash": nil, "flashes": nil, "actions": nil, "action": nil, "csrf": nil, "user": nil, "page": nil, "request": nil},
		funcs:      map[string]any{"len": fileEvalLen, "asset": nil, "stylesheet": nil, "flashValue": nil},
		components: map[string]any{},
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		scope := env.withValue("item", i)
		scope = scope.withValue("i", i)
		if v := evalIdent("item", scope); v == nil {
			b.Fatal("missing binding")
		}
	}
}
