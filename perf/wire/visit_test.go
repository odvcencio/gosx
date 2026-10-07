package wire

import (
	"context"
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const visitScript = "/gosx/navigation.0123456789abcdef.js"

func visitTestServer(t *testing.T, inline bool, script string) (*httptest.Server, *atomic.Int64) {
	t.Helper()
	var downloads atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == visitScript {
			downloads.Add(1)
			w.Header().Set("Content-Type", "application/javascript")
			w.Header().Set("Cache-Control", "public, max-age=3600, immutable")
			fmt.Fprint(w, script)
			return
		}
		w.Header().Set("Content-Type", "text/html")
		if inline {
			fmt.Fprintf(w, "<script>%s</script><p>page</p>", script)
		} else {
			fmt.Fprintf(w, `<script src="%s"></script><p>page</p>`, visitScript)
		}
	}))
	t.Cleanup(srv.Close)
	return srv, &downloads
}

func TestVisitCreditsInlineToExternalMove(t *testing.T) {
	script := strings.Repeat("navigation();", 256)
	inline, _ := visitTestServer(t, true, script)
	external, downloads := visitTestServer(t, false, script)
	var totals [2]int64
	for i, srv := range []*httptest.Server{inline, external} {
		var visit Visit
		for page, route := range []string{"/a", "/b"} {
			r, err := Crawl(context.Background(), Options{Visit: &visit}, "app", srv.URL, route)
			if err != nil {
				t.Fatal(err)
			}
			totals[i] += r.TotalWireBytes
			if i == 0 {
				if r.InlineScriptBytes != int64(len(script)) || r.WireBytes[KindDocument] < int64(len(script)) || r.WireBytes[KindScript] != 0 || r.Requests != 1 {
					t.Fatalf("inline page: %+v", r)
				}
				if r.TotalWireBytes != r.Document.WireBytes || r.FrameworkJSWireBytes != 0 {
					t.Fatalf("inline bytes must be counted exactly in HTML, not estimated again: %+v", r)
				}
				continue
			}
			wantJS, wantRequests := int64(len(script)), 2
			if page > 0 {
				wantJS, wantRequests = 0, 1
			}
			if r.WireBytes[KindScript] != wantJS || r.FrameworkJSWireBytes != wantJS || r.Requests != wantRequests || r.TotalWireBytes != r.Document.WireBytes+wantJS {
				t.Fatalf("external page %d: %+v", page, r)
			}
			if len(r.Resources) != 1 || r.Resources[0].CacheHit != (page > 0) || !r.Resources[0].Hashed || !r.Resources[0].Immutable || r.Resources[0].DecodedBytes != int64(len(script)) {
				t.Fatalf("cache metadata lost: %+v", r.Resources)
			}
		}
	}
	if totals[1] >= totals[0] || downloads.Load() != 1 {
		t.Fatalf("inline=%d external=%d asset downloads=%d; want one download and lower visit bytes", totals[0], totals[1], downloads.Load())
	}
}

func TestVisitBiggerPayloadStillFailsUnchangedBudget(t *testing.T) {
	small, _ := visitTestServer(t, false, strings.Repeat("navigation();", 128))
	large, _ := visitTestServer(t, false, strings.Repeat("navigation();", 512))
	before, err := Crawl(context.Background(), Options{Visit: &Visit{}}, "app", small.URL, "/a")
	if err != nil {
		t.Fatal(err)
	}
	budget := Update(Budget{Schema: BudgetSchema}, []Route{before}, false)
	if findings := Check(budget, []Route{before}); len(findings) != 0 {
		t.Fatalf("baseline must pass: %v", findings)
	}
	after, err := Crawl(context.Background(), Options{Visit: &Visit{}}, "app", large.URL, "/a")
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range Check(budget, []Route{after}) {
		if f.Subject == MetricTotalWireBytes && strings.Contains(f.Message, "over the limit") {
			return
		}
	}
	t.Fatalf("larger cold payload escaped unchanged total ceiling %d: %+v", before.TotalWireBytes, after)
}

func TestVisitOnlyReusesFreshHashedAssets(t *testing.T) {
	for _, tc := range []struct {
		name, asset string
		headers     http.Header
		cached      bool
	}{
		{"fresh", visitScript, http.Header{"Cache-Control": {"public, max-age=3600, immutable"}}, true},
		{"private_browser_cache", visitScript, http.Header{"Cache-Control": {"private, max-age=3600"}}, true},
		{"unhashed", "/navigation.js", http.Header{"Cache-Control": {"public, max-age=3600, immutable"}}, false},
		{"immutable_without_freshness", visitScript, http.Header{"Cache-Control": {"immutable"}}, false},
		{"no_store", visitScript, http.Header{"Cache-Control": {"no-store, max-age=3600, immutable"}}, false},
		{"no_cache", visitScript, http.Header{"Cache-Control": {"no-cache, max-age=3600, immutable"}}, false},
		{"zero_age", visitScript, http.Header{"Cache-Control": {"max-age=0, immutable"}}, false},
		{"expired_age", visitScript, http.Header{"Cache-Control": {"max-age=60, immutable"}, "Age": {"61"}}, false},
		{"expired_date", visitScript, http.Header{"Cache-Control": {"max-age=60, immutable"}, "Date": {time.Now().Add(-time.Hour).UTC().Format(http.TimeFormat)}}, false},
		{"expires", visitScript, http.Header{"Expires": {time.Now().Add(time.Hour).UTC().Format(http.TimeFormat)}}, true},
		{"vary_encoding", visitScript, http.Header{"Cache-Control": {"max-age=3600"}, "Vary": {"Accept-Encoding, User-Agent"}}, true},
		{"vary_cookie_unchanged", visitScript, http.Header{"Cache-Control": {"max-age=3600"}, "Vary": {"Cookie"}}, true},
		{"vary_absent_header", visitScript, http.Header{"Cache-Control": {"max-age=3600"}, "Vary": {"Accept-Language"}}, true},
		{"vary_star", visitScript, http.Header{"Cache-Control": {"max-age=3600"}, "Vary": {"*"}}, false},
		{"cookie_policy", visitScript, http.Header{"Cache-Control": {"max-age=3600, immutable"}, "Set-Cookie": {"session=1"}}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var downloads atomic.Int64
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == tc.asset {
					downloads.Add(1)
					for key, values := range tc.headers {
						w.Header()[key] = values
					}
					fmt.Fprint(w, "navigation();")
					return
				}
				fmt.Fprintf(w, `<script src="%s"></script>`, tc.asset)
			}))
			defer srv.Close()
			var visit Visit
			for i := 0; i < 2; i++ {
				r, err := Crawl(context.Background(), Options{Visit: &visit}, "app", srv.URL, "/page")
				if err != nil {
					t.Fatal(err)
				}
				wantRequests := 2
				if i > 0 && tc.cached {
					wantRequests = 1
				}
				if r.Requests != wantRequests || r.Resources[0].CacheHit != (i > 0 && tc.cached) {
					t.Fatalf("page %d: %+v", i, r)
				}
				if tc.name == "cookie_policy" && r.EvaluatePolicies()[PolicyNoCookie].Pass {
					t.Fatal("cache hit hid the cookie policy failure")
				}
			}
			wantDownloads := int64(2)
			if tc.cached {
				wantDownloads = 1
			}
			if downloads.Load() != wantDownloads {
				t.Fatalf("downloads=%d want=%d", downloads.Load(), wantDownloads)
			}
		})
	}
}

func TestVisitVaryCookieMatchesRequestState(t *testing.T) {
	var downloads atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == visitScript {
			downloads.Add(1)
			w.Header().Set("Cache-Control", "max-age=3600, immutable")
			w.Header().Set("Vary", "Cookie")
			fmt.Fprint(w, "navigation();")
			return
		}
		if r.URL.Path != "/last" {
			http.SetCookie(w, &http.Cookie{Name: "visitor", Value: r.URL.Path, Path: "/"})
		}
		fmt.Fprintf(w, `<script src="%s"></script>`, visitScript)
	}))
	defer srv.Close()
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar}
	var visit Visit
	for i, route := range []string{"/first", "/changed", "/last"} {
		r, err := Crawl(context.Background(), Options{Visit: &visit, Client: client}, "app", srv.URL, route)
		if err != nil {
			t.Fatal(err)
		}
		if r.Requests != []int{2, 2, 1}[i] || r.Resources[0].CacheHit != (i == 2) {
			t.Fatalf("%s: %+v", route, r)
		}
	}
	if downloads.Load() != 2 {
		t.Fatalf("downloads=%d want=2 for two distinct cookie variants", downloads.Load())
	}
}

func TestVisitLazyProbeDoesNotWarmEagerCache(t *testing.T) {
	var downloads atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case visitScript:
			downloads.Add(1)
			w.Header().Set("Cache-Control", "max-age=3600, immutable")
			fmt.Fprint(w, "navigation();")
		case "/lazy":
			fmt.Fprintf(w, `<div data-gosx-runtime-url="%s"></div>`, visitScript)
		default:
			fmt.Fprintf(w, `<script src="%s"></script>`, visitScript)
		}
	}))
	defer srv.Close()
	var visit Visit
	for i, route := range []string{"/lazy", "/eager", "/later"} {
		r, err := Crawl(context.Background(), Options{Visit: &visit}, "app", srv.URL, route)
		if err != nil {
			t.Fatal(err)
		}
		wantRequests := []int{1, 2, 1}[i]
		if r.Requests != wantRequests {
			t.Fatalf("%s: requests=%d want=%d", route, r.Requests, wantRequests)
		}
	}
	if downloads.Load() != 2 {
		t.Fatalf("downloads=%d want=2 (one diagnostic probe and one eager download)", downloads.Load())
	}
}

func TestVisitCountsRedirectsAndChangedHashes(t *testing.T) {
	const changed = "/gosx/navigation.fedcba9876543210.js"
	var redirects, oldDownloads, newDownloads atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/alias":
			redirects.Add(1)
			http.Redirect(w, r, visitScript+"#runtime", http.StatusFound)
		case visitScript:
			oldDownloads.Add(1)
			w.Header().Set("Cache-Control", "max-age=3600, immutable")
			fmt.Fprint(w, "navigation();")
		case changed:
			newDownloads.Add(1)
			w.Header().Set("Cache-Control", "max-age=3600, immutable")
			fmt.Fprint(w, strings.Repeat("navigation();", 128))
		case "/third":
			fmt.Fprintf(w, `<script src="%s"></script>`, changed)
		default:
			fmt.Fprint(w, `<script src="/alias"></script>`)
		}
	}))
	defer srv.Close()
	var visit Visit
	for i, route := range []string{"/first", "/second", "/third"} {
		r, err := Crawl(context.Background(), Options{Visit: &visit}, "app", srv.URL, route)
		if err != nil {
			t.Fatal(err)
		}
		if r.Requests != []int{3, 2, 2}[i] || (r.WireBytes[KindRedirect] > 0) != (i < 2) {
			t.Fatalf("%s: %+v", route, r)
		}
		if i == 1 && r.WireBytes[KindScript] != 0 || i == 2 && r.WireBytes[KindScript] != int64(len(strings.Repeat("navigation();", 128))) {
			t.Fatalf("%s: cached or changed payload cost=%d", route, r.WireBytes[KindScript])
		}
	}
	if redirects.Load() != 2 || oldDownloads.Load() != 1 || newDownloads.Load() != 1 {
		t.Fatalf("redirects=%d old=%d new=%d", redirects.Load(), oldDownloads.Load(), newDownloads.Load())
	}
}
