package wire

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/andybalholm/brotli"
)

func TestIsHashedURL(t *testing.T) {
	for _, tc := range []struct {
		url  string
		want bool
	}{
		{"/gosx/assets/runtime/bootstrap-runtime.ea9b6d52e2d52cbb.js", true},
		{"/gosx/assets/islands/Counter.edd8623a71ceed87.gxi", true},
		{"/gosx/assets/runtime/gosx-runtime-core.2958a9d4d5694136.wasm", true},
		{"/assets/app-3f2a9c1d.min.js", true},
		{"/gosx/bootstrap.js", false},
		{"/gosx/bootstrap.js?v=abc12345", false},
		{"/styles.css", false},
		{"/fonts/Inter-400.woff2", false},
		{"/water/tiles.jpg", false},
	} {
		if got := IsHashedURL(tc.url); got != tc.want {
			t.Errorf("IsHashedURL(%q) = %v, want %v", tc.url, got, tc.want)
		}
	}
}

func brotliBytes(t *testing.T, data string) []byte {
	t.Helper()
	var b bytes.Buffer
	w := brotli.NewWriterLevel(&b, 5)
	if _, err := w.Write([]byte(data)); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func testServer(t *testing.T) *httptest.Server {
	t.Helper()
	runtimeJS := strings.Repeat("console.log('runtime');", 200)
	wasm := strings.Repeat("\x00asm", 500)
	mux := http.NewServeMux()
	mux.HandleFunc("/page", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Header().Set("Cache-Control", "public, max-age=0, must-revalidate")
		http.SetCookie(w, &http.Cookie{Name: "s", Value: "1"})
		w.Write([]byte(`<!doctype html><html><head>
<link rel="stylesheet" href="/site.css">
<link rel="preload" href="/gosx/rt.0123456789abcdef.wasm" as="fetch">
<script>` + strings.Repeat("x", 3000) + `</script>
<script>small()</script>
<script id="gosx-manifest" type="application/json">{"runtime":{"path":"/gosx/rt.0123456789abcdef.wasm"},"islands":[{"programRef":"/gosx/islands/C.0123456789abcdef.gxi"}],"props":{"path":"/not-an-asset"}}</script>
<script type="application/ld+json">{"@type":"Thing"}</script>
</head><body><img src="/hero.jpg" alt=""><img loading="lazy" src="/below.jpg" alt=""><img src="data:image/gif;base64,R0lGOD" alt="">
<div data-gosx-scene3d-gltf-url="/gosx/lazy-gltf.js" data-gosx-other="/gosx/not-a-url-attr.js"></div>
<script defer src="/gosx/boot.js"></script></body></html>`))
	})
	mux.HandleFunc("/gosx/lazy-gltf.js", func(w http.ResponseWriter, r *http.Request) {
		http.SetCookie(w, &http.Cookie{Name: "lazy", Value: "1"})
		w.Write([]byte(strings.Repeat("lazy();", 300)))
	})
	mux.HandleFunc("/hero.jpg", func(w http.ResponseWriter, r *http.Request) {
		w.Write(bytes.Repeat([]byte{0xff}, 3000))
	})
	mux.HandleFunc("/below.jpg", func(w http.ResponseWriter, r *http.Request) {
		t.Error("lazy image fetched")
	})
	mux.HandleFunc("/site.css", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/css")
		w.Write([]byte("body{}"))
	})
	mux.HandleFunc("/gosx/boot.js", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		if strings.Contains(strings.Join(r.Header.Values("Accept-Encoding"), ", "), "br") {
			w.Header().Set("Content-Encoding", "br")
			w.Write(brotliBytes(t, runtimeJS))
			return
		}
		w.Write([]byte(runtimeJS))
	})
	mux.HandleFunc("/gosx/rt.0123456789abcdef.wasm", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		w.Write([]byte(wasm))
	})
	mux.HandleFunc("/gosx/islands/C.0123456789abcdef.gxi", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		w.Write([]byte("prog"))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func TestCrawlCountsTheLoadPath(t *testing.T) {
	srv := testServer(t)
	r, err := Crawl(context.Background(), Options{}, "app", srv.URL, "/page")
	if err != nil {
		t.Fatal(err)
	}
	// document + css + wasm (preload and manifest dedupe to one) + gxi +
	// eager image + boot.js; the lazy and data: images are not fetched.
	if r.Requests != 6 {
		for _, res := range r.Resources {
			t.Logf("resource %s %s", res.URL, res.Kind)
		}
		t.Fatalf("requests = %d, want 6", r.Requests)
	}
	if r.InlineScriptMax != 3000 || r.InlineScriptBytes != 3000+int64(len("small()")) {
		t.Fatalf("inline script max=%d total=%d", r.InlineScriptMax, r.InlineScriptBytes)
	}
	if r.LazyWireBytes != int64(len(strings.Repeat("lazy();", 300))) {
		t.Fatalf("on-demand bytes = %d", r.LazyWireBytes)
	}
	var lazy *Resource
	for i := range r.Resources {
		if r.Resources[i].URL == "/gosx/lazy-gltf.js" {
			lazy = &r.Resources[i]
		}
	}
	if lazy == nil || lazy.Kind != KindLazyScript || !lazy.SetCookie {
		t.Fatalf("on-demand chunk not measured for policies: %+v", lazy)
	}
	if !strings.Contains(r.EvaluatePolicies()[PolicyRuntimeHashed].Reason, "/gosx/lazy-gltf.js") && !strings.Contains(r.EvaluatePolicies()[PolicyRuntimeHashed].Reason, "more") {
		t.Fatalf("unhashed on-demand chunk passed runtime-hashed: %q", r.EvaluatePolicies()[PolicyRuntimeHashed].Reason)
	}
	if r.InlineDataBytes == 0 {
		t.Fatal("inline JSON data bytes not counted")
	}
	var boot Resource
	for _, res := range r.Resources {
		if res.URL == "/gosx/boot.js" {
			boot = res
		}
	}
	if boot.ContentEncoding != "br" || boot.WireBytes >= boot.DecodedBytes || boot.DecodedBytes != int64(len(strings.Repeat("console.log('runtime');", 200))) {
		t.Fatalf("boot.js wire=%d decoded=%d enc=%q; want brotli wire bytes below decoded bytes", boot.WireBytes, boot.DecodedBytes, boot.ContentEncoding)
	}
	if r.WireBytes[KindWASM] != 2000 || r.WireBytes[KindProgram] != 4 || r.WireBytes[KindStyle] != 6 || r.WireBytes[KindImage] != 3000 {
		t.Fatalf("wire bytes by kind = %v", r.WireBytes)
	}
	var sum int64
	for _, v := range r.WireBytes {
		sum += v
	}
	if sum != r.TotalWireBytes {
		t.Fatalf("kinds sum to %d, total %d", sum, r.TotalWireBytes)
	}
	// Framework bytes: boot.js + wasm + gxi + all inline script (HTML is
	// uncompressed, so inline bytes count in full).
	want := boot.WireBytes + 2000 + 4 + r.InlineScriptBytes
	if r.FrameworkJSWireBytes != want {
		t.Fatalf("framework bytes = %d, want %d", r.FrameworkJSWireBytes, want)
	}

	pol := r.EvaluatePolicies()
	for p, wantPass := range map[string]bool{
		PolicyHTMLCompressed:   false,
		PolicyAssetsCompressed: false, // the 2000-byte WASM is uncompressed
		PolicyNoCookie:         false,
		PolicyImmutableHashed:  false, // boot.js is immutable but unhashed
		PolicyRuntimeHashed:    false,
		PolicyNoInlineRuntime:  false,
		PolicyHTMLShareable:    true,
	} {
		if pol[p].Pass != wantPass {
			t.Errorf("policy %s pass=%v (%s), want %v", p, pol[p].Pass, pol[p].Reason, wantPass)
		}
	}
}

func TestEagerReferenceWinsOverOnDemand(t *testing.T) {
	for name, body := range map[string]string{
		"lazy first":  `<div data-gosx-x-url="/gosx/a.js"></div><script src="/gosx/a.js"></script>`,
		"eager first": `<script src="/gosx/a.js"></script><div data-gosx-x-url="/gosx/a.js"></div>`,
	} {
		mux := http.NewServeMux()
		mux.HandleFunc("/p", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(body)) })
		mux.HandleFunc("/gosx/a.js", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("a();")) })
		srv := httptest.NewServer(mux)
		r, err := Crawl(context.Background(), Options{}, "app", srv.URL, "/p")
		srv.Close()
		if err != nil {
			t.Fatal(err)
		}
		if r.Requests != 2 || r.WireBytes[KindScript] != 4 || r.LazyWireBytes != 0 {
			t.Errorf("%s: requests=%d js=%d lazy=%d, want 2, 4, 0", name, r.Requests, r.WireBytes[KindScript], r.LazyWireBytes)
		}
	}
}

func TestCrawlCountsEveryRedirectHop(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/start", func(w http.ResponseWriter, r *http.Request) {
		http.SetCookie(w, &http.Cookie{Name: "hop", Value: "1"})
		http.Redirect(w, r, "/page/", http.StatusFound)
	})
	mux.HandleFunc("/page/", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`<!doctype html><link rel="stylesheet" href="/old.css"><p>hi</p>`))
	})
	mux.HandleFunc("/old.css", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/new.css", http.StatusMovedPermanently)
	})
	mux.HandleFunc("/new.css", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("p{}"))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	r, err := Crawl(context.Background(), Options{}, "app", srv.URL, "/start")
	if err != nil {
		t.Fatal(err)
	}
	// start (302) + page + old.css (301) + new.css
	if r.Requests != 4 {
		t.Fatalf("requests = %d, want 4 (%+v)", r.Requests, r.Resources)
	}
	if r.WireBytes[KindRedirect] == 0 {
		t.Fatalf("redirect bodies not counted: %v", r.WireBytes)
	}
	if r.EvaluatePolicies()[PolicyNoCookie].Pass {
		t.Fatal("a cookie set on a redirect hop passed no-cookie")
	}
}

func TestOnDemandRedirectStaysOutOfInitialLoad(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/p", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`<!doctype html><div data-gosx-x-url="/gosx/old.js"></div>`))
	})
	mux.HandleFunc("/gosx/old.js", func(w http.ResponseWriter, r *http.Request) {
		http.SetCookie(w, &http.Cookie{Name: "hop", Value: "1"})
		http.Redirect(w, r, "/gosx/new.js", http.StatusFound)
	})
	mux.HandleFunc("/gosx/new.js", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("lazy();")) })
	srv := httptest.NewServer(mux)
	defer srv.Close()
	r, err := Crawl(context.Background(), Options{}, "app", srv.URL, "/p")
	if err != nil {
		t.Fatal(err)
	}
	if r.Requests != 1 || r.TotalWireBytes != r.Document.WireBytes || r.WireBytes[KindRedirect] != 0 {
		t.Fatalf("on-demand redirect counted in initial load: requests=%d total=%d doc=%d redirect=%d",
			r.Requests, r.TotalWireBytes, r.Document.WireBytes, r.WireBytes[KindRedirect])
	}
	var hop *Resource
	var chunk int64
	for i := range r.Resources {
		switch r.Resources[i].Kind {
		case KindRedirect:
			hop = &r.Resources[i]
		case KindLazyScript:
			chunk = r.Resources[i].WireBytes
		}
	}
	if hop == nil || !hop.SetCookie {
		t.Fatalf("on-demand redirect hop not listed for policies: %+v", r.Resources)
	}
	if r.LazyWireBytes != chunk+hop.WireBytes || hop.WireBytes == 0 {
		t.Fatalf("lazy bytes = %d, want chunk %d + hop %d", r.LazyWireBytes, chunk, hop.WireBytes)
	}
	if r.EvaluatePolicies()[PolicyNoCookie].Pass {
		t.Fatal("a cookie set on an on-demand redirect hop passed no-cookie")
	}
}

func TestFrameworkRedirectKeepsRuntimePolicy(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/p", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`<!doctype html><script src="/gosx/boot.js"></script>`))
	})
	mux.HandleFunc("/gosx/boot.js", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/assets/boot.js", http.StatusFound)
	})
	mux.HandleFunc("/assets/boot.js", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("boot();")) })
	srv := httptest.NewServer(mux)
	defer srv.Close()
	r, err := Crawl(context.Background(), Options{}, "app", srv.URL, "/p")
	if err != nil {
		t.Fatal(err)
	}
	got := r.EvaluatePolicies()[PolicyRuntimeHashed]
	if got.Pass || !strings.Contains(got.Reason, "/assets/boot.js") {
		t.Fatalf("framework script redirected to an unhashed URL passed runtime-hashed: %+v", got)
	}
}

func TestRedirectIntoFrameworkCountsFrameworkBytes(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/p", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`<!doctype html><script src="/app.js"></script>`))
	})
	mux.HandleFunc("/app.js", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/gosx/runtime.js", http.StatusFound)
	})
	mux.HandleFunc("/gosx/runtime.js", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("runtime();")) })
	srv := httptest.NewServer(mux)
	defer srv.Close()
	r, err := Crawl(context.Background(), Options{}, "app", srv.URL, "/p")
	if err != nil {
		t.Fatal(err)
	}
	if r.FrameworkJSWireBytes != int64(len("runtime();")) {
		t.Fatalf("framework bytes = %d, want %d", r.FrameworkJSWireBytes, len("runtime();"))
	}
}

func TestCheckUpdateAndRatchet(t *testing.T) {
	srv := testServer(t)
	r, err := Crawl(context.Background(), Options{}, "app", srv.URL, "/page")
	if err != nil {
		t.Fatal(err)
	}
	measured := []Route{r}
	b := Budget{Schema: BudgetSchema, Tolerance: Tolerance{BytesPercent: 2, BytesMin: 512}}

	if f := Check(b, measured); len(f) != 1 || f[0].Subject != "route" {
		t.Fatalf("unbudgeted route findings = %v", f)
	}

	b = Update(b, measured, false)
	if f := Check(b, measured); len(f) != 0 {
		t.Fatalf("fresh budget findings = %v", f)
	}
	rb := b.Apps["app"]["/page"]
	if !contains(rb.Require, PolicyHTMLShareable) || contains(rb.Require, PolicyNoCookie) {
		t.Fatalf("require = %v", rb.Require)
	}

	// Over the limit.
	over := b
	over.Apps = map[string]AppBudget{"app": {"/page": withLimit(rb, MetricTotalWireBytes, r.TotalWireBytes-1)}}
	if f := Check(over, measured); len(f) != 1 || !strings.Contains(f[0].Message, "over the limit") {
		t.Fatalf("over-limit findings = %v", f)
	}

	// Stale: far above the measurement.
	stale := b
	stale.Apps = map[string]AppBudget{"app": {"/page": withLimit(rb, MetricTotalWireBytes, r.TotalWireBytes*2)}}
	if f := Check(stale, measured); len(f) != 1 || !strings.Contains(f[0].Message, "lower the limit") {
		t.Fatalf("stale findings = %v", f)
	}
	// Update lowers the stale limit and keeps the fresh ones.
	if got := Update(stale, measured, false).Apps["app"]["/page"].Limits[MetricTotalWireBytes]; got != rb.Limits[MetricTotalWireBytes] {
		t.Fatalf("updated stale limit = %d, want %d", got, rb.Limits[MetricTotalWireBytes])
	}
	// Requests have no slack.
	reqs := b
	reqs.Apps = map[string]AppBudget{"app": {"/page": withLimit(rb, MetricRequests, int64(r.Requests)+1)}}
	if f := Check(reqs, measured); len(f) != 1 || f[0].Subject != MetricRequests {
		t.Fatalf("request findings = %v", f)
	}

	// A required policy that fails.
	req := rb
	req.Require = append(append([]string{}, rb.Require...), PolicyNoCookie)
	strict := b
	strict.Apps = map[string]AppBudget{"app": {"/page": req}}
	if f := Check(strict, measured); len(f) != 1 || f[0].Subject != PolicyNoCookie {
		t.Fatalf("policy findings = %v", f)
	}

	// A passing policy that is not required.
	loose := rb
	loose.Require = nil
	lb := b
	lb.Apps = map[string]AppBudget{"app": {"/page": loose}}
	if f := Check(lb, measured); len(f) != 1 || f[0].Subject != PolicyHTMLShareable {
		t.Fatalf("unrequired passing policy findings = %v", f)
	}

	// Ratchet: raising a limit or dropping a policy needs a reason.
	head := b
	head.Apps = map[string]AppBudget{"app": {"/page": withLimit(loose, MetricTotalWireBytes, rb.Limits[MetricTotalWireBytes]+1)}}
	if f := Ratchet(b, head); len(f) != 2 {
		t.Fatalf("ratchet findings = %v", f)
	}
	reasoned := head.Apps["app"]["/page"]
	reasoned.Raise = map[string]string{MetricTotalWireBytes: "adds a font", PolicyHTMLShareable: "page is personalized"}
	head.Apps = map[string]AppBudget{"app": {"/page": reasoned}}
	if f := Ratchet(b, head); len(f) != 0 {
		t.Fatalf("reasoned ratchet findings = %v", f)
	}
	if f := Ratchet(b, b); len(f) != 0 {
		t.Fatalf("identical ratchet findings = %v", f)
	}

	// Round trip.
	data, err := b.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	back, err := ParseBudget(data)
	if err != nil {
		t.Fatal(err)
	}
	if f := Check(back, measured); len(f) != 0 {
		t.Fatalf("round-tripped budget findings = %v", f)
	}
	if _, err := ParseBudget([]byte(`{"schema":"gosx.wire-budget/v1","apps":{"a":{"/":{"limits":{"nope":1}}}}}`)); err == nil {
		t.Fatal("unknown limit accepted")
	}
}

func withLimit(rb RouteBudget, metric string, v int64) RouteBudget {
	limits := map[string]int64{}
	for k, val := range rb.Limits {
		limits[k] = val
	}
	limits[metric] = v
	rb.Limits = limits
	return rb
}
