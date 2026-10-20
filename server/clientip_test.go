package server

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"m31labs.dev/gosx/internal/telemetryerr"
)

func TestTrustedProxyResolution(t *testing.T) {
	policy, err := compileProxyPolicy(TrustedProxyPolicy{Proxies: []string{"192.0.2.0/24", "2001:db8:2::/48"}})
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name, peer string
		values     []string
		want       string
	}{
		{"untrusted", "198.51.100.3:4321", []string{"203.0.113.9"}, "198.51.100.3"},
		{"missing", "192.0.2.1:4321", nil, "192.0.2.1"},
		{"single", "192.0.2.1:4321", []string{"198.51.100.8"}, "198.51.100.8"},
		{"spoofed leftmost", "192.0.2.1:4321", []string{"203.0.113.9,198.51.100.8,192.0.2.4"}, "198.51.100.8"},
		{"all trusted", "192.0.2.1:4321", []string{"192.0.2.8,192.0.2.4"}, "192.0.2.8"},
		{"spaces", "192.0.2.1:4321", []string{" \t198.51.100.8 , 192.0.2.4\t"}, "198.51.100.8"},
		{"ipv6", "[2001:db8:2::2]:4321", []string{"2001:db8:1::8,2001:db8:2::4"}, "2001:db8:1::8"},
		{"mapped peer", "[::ffff:192.0.2.1]:4321", []string{"198.51.100.8"}, "198.51.100.8"},
		{"mapped hop", "192.0.2.1:4321", []string{"::ffff:198.51.100.8,::ffff:192.0.2.4"}, "198.51.100.8"},
		{"duplicate", "192.0.2.1:4321", []string{"198.51.100.8", "192.0.2.4"}, "192.0.2.1"},
		{"duplicate reversed", "192.0.2.1:4321", []string{"192.0.2.4", "198.51.100.8"}, "192.0.2.1"},
		{"unknown", "192.0.2.1:4321", []string{"unknown"}, "192.0.2.1"},
		{"obfuscated", "192.0.2.1:4321", []string{"_hidden"}, "192.0.2.1"},
		{"empty", "192.0.2.1:4321", []string{""}, "192.0.2.1"},
		{"leading empty", "192.0.2.1:4321", []string{",198.51.100.8"}, "192.0.2.1"},
		{"trailing empty", "192.0.2.1:4321", []string{"198.51.100.8,"}, "192.0.2.1"},
		{"invalid ignored hop", "192.0.2.1:4321", []string{"invalid,198.51.100.8"}, "192.0.2.1"},
		{"quoted", "192.0.2.1:4321", []string{"\"198.51.100.8\""}, "192.0.2.1"},
		{"port", "192.0.2.1:4321", []string{"198.51.100.8:1234"}, "192.0.2.1"},
		{"brackets", "192.0.2.1:4321", []string{"[2001:db8:1::8]"}, "192.0.2.1"},
		{"bracketed port", "192.0.2.1:4321", []string{"[2001:db8:1::8]:1234"}, "192.0.2.1"},
		{"zone", "192.0.2.1:4321", []string{"fe80::1%eth0"}, "192.0.2.1"},
		{"hostname", "192.0.2.1:4321", []string{"proxy.invalid"}, "192.0.2.1"},
		{"structured forwarded", "192.0.2.1:4321", []string{"for=198.51.100.8"}, "192.0.2.1"},
		{"sixteen hops", "192.0.2.1:4321", []string{"198.51.100.8" + strings.Repeat(",192.0.2.4", 15)}, "198.51.100.8"},
		{"seventeen hops", "192.0.2.1:4321", []string{"198.51.100.8" + strings.Repeat(",192.0.2.4", 16)}, "192.0.2.1"},
		{"at byte cap", "192.0.2.1:4321", []string{"198.51.100.8" + strings.Repeat(" ", 2048-len("198.51.100.8"))}, "198.51.100.8"},
		{"above byte cap", "192.0.2.1:4321", []string{"198.51.100.8" + strings.Repeat(" ", 2049-len("198.51.100.8"))}, "192.0.2.1"},
		{"invalid peer", "bad-peer", []string{"198.51.100.8"}, ""},
		{"zoned peer", "[fe80::1%eth0]:4321", []string{"198.51.100.8"}, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			header := make(http.Header)
			for _, value := range tc.values {
				header.Add("X-Forwarded-For", value)
			}
			got := policy.resolve(requestPeer(tc.peer), header)
			if tc.want == "" {
				if got.IsValid() {
					t.Fatalf("invalid peer resolved to %v", got)
				}
			} else if got != netip.MustParseAddr(tc.want) {
				t.Fatalf("resolved %v, want %s", got, tc.want)
			}
		})
	}
}

func TestTrustedProxyForwardedHeaderSelection(t *testing.T) {
	policy, err := compileProxyPolicy(TrustedProxyPolicy{Proxies: []string{"192.0.2.0/24"}})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name   string
		header http.Header
		want   string
	}{
		{"forwarded only", http.Header{"Forwarded": {"for=203.0.113.9"}}, "192.0.2.1"},
		{"both", http.Header{"Forwarded": {"for=203.0.113.9"}, "X-Forwarded-For": {"198.51.100.8,192.0.2.4"}}, "198.51.100.8"},
		{"repeated forwarded", http.Header{"Forwarded": {"for=203.0.113.9", "for=192.0.2.4"}, "X-Forwarded-For": {"198.51.100.8"}}, "198.51.100.8"},
		{"repeated xff with forwarded", http.Header{"Forwarded": {"for=203.0.113.9"}, "X-Forwarded-For": {"198.51.100.8", "192.0.2.4"}}, "192.0.2.1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := policy.resolve(netip.MustParseAddr("192.0.2.1"), tc.header)
			if got != netip.MustParseAddr(tc.want) {
				t.Fatalf("resolved %v, want %s", got, tc.want)
			}
		})
	}
}

func TestClientIPAppDefaultIgnoresForwardedHeaders(t *testing.T) {
	for _, configured := range []bool{false, true} {
		t.Run(fmt.Sprintf("empty-policy=%t", configured), func(t *testing.T) {
			app := New()
			if configured {
				if err := app.SetTrustedProxies(TrustedProxyPolicy{}); err != nil {
					t.Fatal(err)
				}
			}
			calls := 0
			check := func(r *http.Request) {
				t.Helper()
				calls++
				if got := ClientIP(r); got != netip.MustParseAddr("192.0.2.1") {
					t.Errorf("default resolved %v", got)
				}
			}
			app.UseObserver(RequestObserverFunc(func(event RequestEvent) { check(event.Request) }))
			app.Mount("/client", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				check(r)
				r.RemoteAddr = "203.0.113.9:1234"
				r.Header.Set("X-Forwarded-For", "203.0.113.9")
				check(r)
				w.WriteHeader(204)
			}))
			r := httptest.NewRequest("GET", "/client", nil)
			r.RemoteAddr = "192.0.2.1:4321"
			r.Header.Add("Forwarded", "for=198.51.100.8")
			r.Header.Add("Forwarded", "for=192.0.2.4")
			r.Header.Add("X-Forwarded-For", "198.51.100.8,192.0.2.4")
			app.Build().ServeHTTP(httptest.NewRecorder(), r)
			if calls != 3 {
				t.Fatalf("metadata checks=%d, want 3", calls)
			}
		})
	}
}

func TestTrustedProxySingleHeader(t *testing.T) {
	policy, err := compileProxyPolicy(TrustedProxyPolicy{Proxies: []string{"192.0.2.1"}, Header: "x-real-ip"})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		values []string
		want   string
	}{
		{[]string{"198.51.100.8"}, "198.51.100.8"},
		{[]string{"198.51.100.8,192.0.2.4"}, "192.0.2.1"},
		{[]string{"198.51.100.8", "198.51.100.9"}, "192.0.2.1"},
		{nil, "192.0.2.1"},
	} {
		header := http.Header{"X-Real-Ip": tc.values, "X-Forwarded-For": []string{"203.0.113.9"}}
		got := policy.resolve(netip.MustParseAddr("192.0.2.1"), header)
		if got != netip.MustParseAddr(tc.want) {
			t.Fatalf("values=%v got=%v want=%s", tc.values, got, tc.want)
		}
	}
}

func TestClientIPBareHandlersUsePeerOnly(t *testing.T) {
	if ClientIP(nil).IsValid() {
		t.Fatal("nil request has an address")
	}
	for _, tc := range []struct{ remote, want string }{
		{"198.51.100.8:4321", "198.51.100.8"},
		{"198.51.100.8", "198.51.100.8"},
		{"[2001:db8::8]:4321", "2001:db8::8"},
		{"2001:db8::8", "2001:db8::8"},
		{"[::ffff:198.51.100.8]:4321", "198.51.100.8"},
		{"[fe80::1%eth0]:4321", ""},
		{"peer.invalid:4321", ""},
		{"198.51.100.8:not-a-port", ""},
		{"198.51.100.8:65536", ""},
		{strings.Repeat("x", 129), ""},
	} {
		r := httptest.NewRequest("GET", "/", nil)
		r.RemoteAddr = tc.remote
		r.Header.Set("X-Forwarded-For", "203.0.113.9")
		got := ClientIP(r)
		if tc.want == "" && got.IsValid() || tc.want != "" && got != netip.MustParseAddr(tc.want) {
			t.Errorf("peer=%q got=%v want=%s", tc.remote, got, tc.want)
		}
		if r.RemoteAddr != tc.remote {
			t.Fatal("resolver changed RemoteAddr")
		}
	}
}

func TestTrustedProxyConfiguration(t *testing.T) {
	for _, tc := range []struct {
		name   string
		policy TrustedProxyPolicy
		code   string
	}{
		{"all ipv4", TrustedProxyPolicy{Proxies: []string{"0.0.0.0/0"}}, "all_addresses"},
		{"all ipv6", TrustedProxyPolicy{Proxies: []string{"::/0"}}, "all_addresses"},
		{"split ipv4", TrustedProxyPolicy{Proxies: []string{"0.0.0.0/1", "128.0.0.0/1"}}, "all_addresses"},
		{"split ipv6", TrustedProxyPolicy{Proxies: []string{"::/1", "8000::/1"}}, "all_addresses"},
		{"mapped all", TrustedProxyPolicy{Proxies: []string{"::ffff:0.0.0.0/96"}}, "all_addresses"},
		{"mapped broad", TrustedProxyPolicy{Proxies: []string{"::ffff:192.0.2.1/95"}}, "prefix_invalid"},
		{"bad prefix", TrustedProxyPolicy{Proxies: []string{"192.0.2.1/33"}}, "prefix_invalid"},
		{"hostname", TrustedProxyPolicy{Proxies: []string{"proxy.invalid"}}, "prefix_invalid"},
		{"zone", TrustedProxyPolicy{Proxies: []string{"fe80::1%eth0"}}, "prefix_invalid"},
		{"port", TrustedProxyPolicy{Proxies: []string{"192.0.2.1:4321"}}, "prefix_invalid"},
		{"structured header", TrustedProxyPolicy{Header: "Forwarded"}, "header_invalid"},
		{"header newline", TrustedProxyPolicy{Header: "X-Real-IP\n"}, "header_invalid"},
		{"header colon", TrustedProxyPolicy{Header: "X:IP"}, "header_invalid"},
		{"header unicode", TrustedProxyPolicy{Header: "X-界"}, "header_invalid"},
		{"header cap", TrustedProxyPolicy{Header: strings.Repeat("x", 129)}, "header_invalid"},
		{"prefix cap", TrustedProxyPolicy{Proxies: make([]string, 65)}, "prefix_cap"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			app := New()
			err := app.SetTrustedProxies(tc.policy)
			var config *telemetryerr.ConfigError
			if !errors.As(err, &config) || !errors.Is(err, telemetryerr.ErrInvalidOptions) || config.Code != tc.code {
				t.Fatalf("error=%v config=%+v", err, config)
			}
			if app.trustedProxies != nil {
				t.Fatal("invalid policy published")
			}
		})
	}
	var nilApp *App
	if err := nilApp.SetTrustedProxies(TrustedProxyPolicy{}); err == nil {
		t.Fatal("nil App accepted policy")
	}
	app := New()
	prefixes := make([]string, 64)
	for i := range prefixes {
		prefixes[i] = fmt.Sprintf("192.0.2.%d", i)
	}
	if err := app.SetTrustedProxies(TrustedProxyPolicy{Proxies: prefixes}); err != nil {
		t.Fatal(err)
	}
	prefixes[0] = "198.51.100.8"
	if !app.trustedProxies.trusts(netip.MustParseAddr("192.0.2.0")) || app.trustedProxies.trusts(netip.MustParseAddr("198.51.100.8")) {
		t.Fatal("policy retained mutable configuration")
	}
	previous := app.trustedProxies
	if err := app.SetTrustedProxies(TrustedProxyPolicy{Proxies: []string{"invalid"}}); err == nil || app.trustedProxies != previous {
		t.Fatal("invalid replacement changed policy")
	}
	app.Build()
	if err := app.SetTrustedProxies(TrustedProxyPolicy{}); !errors.Is(err, telemetryerr.ErrAfterBuild) {
		t.Fatalf("late configuration = %v", err)
	}
}

func TestTrustedProxyMappedPrefixesAndNoTrustDefault(t *testing.T) {
	policy, err := compileProxyPolicy(TrustedProxyPolicy{Proxies: []string{"::ffff:192.0.2.1/120"}})
	if err != nil {
		t.Fatal(err)
	}
	if !policy.trusts(netip.MustParseAddr("192.0.2.8")) || policy.trusts(netip.MustParseAddr("198.51.100.8")) {
		t.Fatal("mapped prefix was not normalized")
	}
	for _, config := range []TrustedProxyPolicy{{}, {Header: "X-Real-IP"}} {
		policy, err := compileProxyPolicy(config)
		if err != nil {
			t.Fatal(err)
		}
		peer := netip.MustParseAddr("192.0.2.1")
		if got := policy.resolve(peer, http.Header{policy.header: []string{"198.51.100.8"}}); got != peer {
			t.Fatalf("empty trust resolved %v", got)
		}
	}
}

func TestClientIPCachedWithRequestID(t *testing.T) {
	app := New()
	if err := app.SetTrustedProxies(TrustedProxyPolicy{Proxies: []string{"192.0.2.0/24"}}); err != nil {
		t.Fatal(err)
	}
	var observed *http.Request
	app.Mount("/client", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		observed = r
		metadata, ok := r.Context().Value(requestIDContextKey).(*requestMetadata)
		if !ok || metadata.peer != netip.MustParseAddr("192.0.2.1") || ClientIP(r) != netip.MustParseAddr("198.51.100.8") || RequestID(r) != "test-request" {
			t.Fatalf("metadata=%+v id=%q ip=%v", metadata, RequestID(r), ClientIP(r))
		}
		r.RemoteAddr = "203.0.113.9:1234"
		r.Header.Set("X-Forwarded-For", "203.0.113.9")
		r.Header.Set("X-Request-ID", "mutated")
		if ClientIP(r) != netip.MustParseAddr("198.51.100.8") || RequestID(r) != "test-request" {
			t.Fatal("metadata was recomputed after mutation")
		}
		w.WriteHeader(204)
	}))
	r := httptest.NewRequest("GET", "/client", nil)
	r.RemoteAddr = "192.0.2.1:4321"
	r.Header.Set("X-Forwarded-For", "198.51.100.8")
	r.Header.Set("X-Request-ID", "test-request")
	w := httptest.NewRecorder()
	app.Build().ServeHTTP(w, r)
	if w.Code != 204 || w.Header().Get("X-Request-ID") != "test-request" || observed == r || r.RemoteAddr != "192.0.2.1:4321" || r.Context().Value(requestIDContextKey) != nil {
		t.Fatalf("status=%d id=%q original peer=%q", w.Code, w.Header().Get("X-Request-ID"), r.RemoteAddr)
	}
}

func TestClientIPNestedOwnerReusesMetadata(t *testing.T) {
	outer, inner := New(), New()
	if err := outer.SetTrustedProxies(TrustedProxyPolicy{Proxies: []string{"192.0.2.0/24"}}); err != nil {
		t.Fatal(err)
	}
	var outerRequest *http.Request
	handler := outer.requestIDMiddleware()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		outerRequest = r
		r.RemoteAddr = "203.0.113.9:1234"
		r.Header.Set("X-Forwarded-For", "203.0.113.9")
		r.Header.Set("X-Request-ID", "mutated")
		inner.requestIDMiddleware()(http.HandlerFunc(func(w http.ResponseWriter, nested *http.Request) {
			if nested != outerRequest || ClientIP(nested) != netip.MustParseAddr("198.51.100.8") || RequestID(nested) != "test-request" {
				t.Fatalf("nested metadata changed: same=%v ip=%v id=%q", nested == outerRequest, ClientIP(nested), RequestID(nested))
			}
		})).ServeHTTP(w, r)
	}))
	r := httptest.NewRequest("GET", "/", nil)
	r.RemoteAddr = "192.0.2.1:4321"
	r.Header.Set("X-Forwarded-For", "198.51.100.8")
	r.Header.Set("X-Request-ID", "test-request")
	handler.ServeHTTP(httptest.NewRecorder(), r)
}

func TestClientIPConcurrentRequests(t *testing.T) {
	outer, inner := New(), New()
	if err := outer.SetTrustedProxies(TrustedProxyPolicy{Proxies: []string{"192.0.2.0/24"}}); err != nil {
		t.Fatal(err)
	}
	var observed atomic.Int64
	observer := RequestObserverFunc(func(event RequestEvent) {
		observed.Add(1)
		want := netip.MustParseAddr(event.Request.URL.Query().Get("ip"))
		if got := ClientIP(event.Request); got != want || RequestID(event.Request) != want.String() || event.ID != want.String() {
			t.Errorf("observer metadata: ip=%v request ID=%q event ID=%q, want %v", got, RequestID(event.Request), event.ID, want)
		}
	})
	outer.UseObserver(observer)
	inner.UseObserver(observer)
	outer.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			r.RemoteAddr = "203.0.113.9:1234"
			r.Header.Set("X-Forwarded-For", "203.0.113.9")
			r.Header.Set("X-Request-ID", "mutated")
			next.ServeHTTP(w, r)
		})
	})
	inner.Mount("/client", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		want := netip.MustParseAddr(r.URL.Query().Get("ip"))
		if got := ClientIP(r); got != want || RequestID(r) != want.String() {
			t.Errorf("nested metadata: ip=%v id=%q, want %v", got, RequestID(r), want)
		}
		w.Header().Set("X-Request-ID", "mutated-response")
		w.WriteHeader(204)
	}))
	outer.Mount("/nested/", http.StripPrefix("/nested", inner.Build()))
	handler := outer.Build()
	const requests = 32
	var wg sync.WaitGroup
	for i := range requests {
		wg.Go(func() {
			ip := fmt.Sprintf("198.51.100.%d", i+1)
			r := httptest.NewRequest("GET", "/nested/client?ip="+ip, nil)
			r.RemoteAddr = "192.0.2.1:4321"
			r.Header.Set("X-Forwarded-For", "203.0.113.9,"+ip+",192.0.2.4")
			r.Header.Set("X-Request-ID", ip)
			if err := outer.SetTrustedProxies(TrustedProxyPolicy{}); !errors.Is(err, telemetryerr.ErrAfterBuild) {
				t.Errorf("late configuration=%v", err)
			}
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, r)
			if w.Code != 204 {
				t.Errorf("status=%d, want 204", w.Code)
			}
		})
	}
	wg.Wait()
	if got := observed.Load(); got != 2*requests {
		t.Fatalf("completion events=%d, want %d", got, 2*requests)
	}
}

func TestRequestMetadataAddsNoAllocations(t *testing.T) {
	app := New()
	r := httptest.NewRequest("GET", "/", nil)
	r.RemoteAddr = "192.0.2.1:4321"
	r.Header.Set("X-Request-ID", "test-request")
	w := httptest.NewRecorder()
	consumer := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) { _ = RequestID(r); _ = ClientIP(r) })
	// The original request-ID holder allocated a context, boxed string, and copy.
	baselineFactory := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			id := r.Header.Get(requestIDHeader)
			w.Header().Set(requestIDHeader, id)
			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), requestIDContextKey, id)))
		})
	}
	baseline := baselineFactory(consumer)
	head := app.requestIDMiddleware()(consumer)
	baseAllocs := testing.AllocsPerRun(1000, func() { baseline.ServeHTTP(w, r) })
	headAllocs := testing.AllocsPerRun(1000, func() { head.ServeHTTP(w, r) })
	t.Logf("request metadata allocations: baseline=%g head=%g", baseAllocs, headAllocs)
	if headAllocs > baseAllocs {
		t.Fatalf("metadata added %g allocations", headAllocs-baseAllocs)
	}
}

func FuzzTrustedProxyResolution(f *testing.F) {
	for _, seed := range []struct{ peer, header string }{
		{"192.0.2.1:1234", "198.51.100.8,192.0.2.4"},
		{"[::ffff:192.0.2.1]:1234", "::ffff:198.51.100.8"},
		{"198.51.100.8:1234", "203.0.113.9"},
		{"[fe80::1%eth0]:1234", "198.51.100.8"},
		{"bad-peer", strings.Repeat("x", 2049)},
	} {
		f.Add(seed.peer, seed.header)
	}
	policy, err := compileProxyPolicy(TrustedProxyPolicy{Proxies: []string{"192.0.2.0/24"}})
	if err != nil {
		f.Fatal(err)
	}
	f.Fuzz(func(t *testing.T, peerText, headerText string) {
		peer := requestPeer(peerText)
		header := http.Header{"X-Forwarded-For": []string{headerText}}
		got := policy.resolve(peer, header)
		if !policy.trusts(peer) && got != peer {
			t.Fatal("untrusted peer supplied a client address")
		}
		if got.IsValid() && (got.Is4In6() || got.Zone() != "") {
			t.Fatal("resolved address was not normalized")
		}
		if len(headerText) > maxForwardedBytes && got != peer {
			t.Fatal("oversized header changed client address")
		}
	})
}

func TestClientIPCompletionObserverUsesCachedAddress(t *testing.T) {
	app := New()
	if err := app.SetTrustedProxies(TrustedProxyPolicy{Proxies: []string{"192.0.2.0/24"}}); err != nil {
		t.Fatal(err)
	}
	calls := 0
	app.UseObserver(RequestObserverFunc(func(event RequestEvent) {
		calls++
		if ClientIP(event.Request) != netip.MustParseAddr("198.51.100.8") || RequestID(event.Request) != "test-request" || event.ID != "test-request" {
			t.Fatalf("observer saw uncached metadata: ip=%v id=%q event.ID=%q", ClientIP(event.Request), RequestID(event.Request), event.ID)
		}
	}))
	app.Mount("/client", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if ClientIP(r) != netip.MustParseAddr("198.51.100.8") {
			t.Fatal("handler did not resolve trusted peer")
		}
		r.Header.Set("X-Forwarded-For", "203.0.113.9")
		r.Header.Set("X-Request-ID", "mutated")
		w.Header().Set("X-Request-ID", "mutated-response")
		w.WriteHeader(204)
	}))
	r := httptest.NewRequest("GET", "/client", nil)
	r.RemoteAddr = "192.0.2.1:4321"
	r.Header.Set("X-Forwarded-For", "198.51.100.8")
	r.Header.Set("X-Request-ID", "test-request")
	app.Build().ServeHTTP(httptest.NewRecorder(), r)
	if calls != 1 || r.RemoteAddr != "192.0.2.1:4321" {
		t.Fatalf("calls=%d peer=%q", calls, r.RemoteAddr)
	}
}
