//go:build windows && (amd64 || arm64)

package desktop

import (
	"net/http"
	"strings"
	"testing"
)

// TestMatchRoutePrefix verifies the prefix semantics App.Serve applies to
// URI routing. Catches regressions in the glob handling without needing a
// live WebView2 session.
func TestMatchRoutePrefix(t *testing.T) {
	cases := []struct {
		prefix string
		uri    string
		want   bool
	}{
		{"app://assets/*", "app://assets/index.html", true},
		{"app://assets/*", "app://assets/css/main.css", true},
		{"app://assets/*", "app://assets/", false}, // trailing slash requires a path segment
		{"app://assets/*", "app://other/foo", false},
		{"app://assets/index.html", "app://assets/index.html", true},
		{"app://assets/index.html", "app://assets/index.htm", false},
		{"*", "app://anywhere", true},
	}
	for _, c := range cases {
		if got := matchRoutePrefix(c.prefix, c.uri); got != c.want {
			t.Errorf("matchRoutePrefix(%q, %q) = %v, want %v",
				c.prefix, c.uri, got, c.want)
		}
	}
}

// TestFormatResponseHeaders verifies the http.Header → CRLF-joined string
// conversion the WebView2 CreateWebResourceResponse ABI expects. Header
// order is non-deterministic across Go map iterations; we check field
// presence rather than byte equality.
func TestFormatResponseHeaders(t *testing.T) {
	h := http.Header{}
	h.Set("Content-Type", "application/json; charset=utf-8")
	h.Set("Cache-Control", "no-store")
	h.Add("Set-Cookie", "a=1")
	h.Add("Set-Cookie", "b=2")

	got := formatResponseHeaders(h)
	for _, line := range []string{
		"Content-Type: application/json; charset=utf-8",
		"Cache-Control: no-store",
		"Set-Cookie: a=1",
		"Set-Cookie: b=2",
	} {
		if !strings.Contains(got, line) {
			t.Errorf("header string missing %q:\n%s", line, got)
		}
	}
	if strings.Count(got, "\r\n")+1 != strings.Count(got, ":") {
		// One CRLF between each "Name: value" pair; count should match.
		t.Errorf("expected CRLFs between header lines, got: %q", got)
	}
}
