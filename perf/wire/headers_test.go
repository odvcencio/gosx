package wire

import (
	"bytes"
	"compress/gzip"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestFetchCacheControlAcrossHeaderLines(t *testing.T) {
	for _, values := range [][]string{
		{"public, max-age=60", "private, no-store, immutable"},
		{"public, max-age=60, private, no-store, immutable"},
	} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			for _, value := range values {
				w.Header().Add("Cache-Control", value)
			}
			w.Write([]byte("fixture"))
		}))
		t.Cleanup(srv.Close)
		res, _, _, _, err := fetchOnce(context.Background(), srv.Client(), MobileUserAgent, srv.URL, "navigation")
		if err != nil {
			t.Fatal(err)
		}
		if res.CacheControl != strings.Join(values, ", ") || !res.Immutable || (Route{Document: res}).EvaluatePolicies()[PolicyHTMLShareable].Pass {
			t.Fatal("a Cache-Control line was omitted", res)
		}
	}
}

func TestFetchContentEncodingHeaderLists(t *testing.T) {
	var compressed bytes.Buffer
	writer := gzip.NewWriter(&compressed)
	if _, err := writer.Write([]byte("fixture")); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name    string
		values  []string
		allowed bool
	}{
		{"single", []string{"gzip"}, true},
		{"case-and-whitespace", []string{" GZip "}, true},
		{"stacked-lines", []string{"gzip", "br"}, false},
		{"stacked-list", []string{"gzip, br"}, false},
		{"repeated-coding", []string{"gzip", "gzip"}, false},
		{"unsupported-second-coding", []string{"gzip", "zstd"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				for _, value := range tc.values {
					w.Header().Add("Content-Encoding", value)
				}
				w.Write(compressed.Bytes())
			}))
			t.Cleanup(srv.Close)
			res, body, _, _, err := fetchOnce(context.Background(), srv.Client(), MobileUserAgent, srv.URL, "navigation")
			if tc.allowed {
				if err != nil || string(body) != "fixture" || res.ContentEncoding != "gzip" {
					t.Fatal("single coding rejected or decoded incorrectly", err)
				}
			} else if err == nil || !strings.Contains(err.Error(), "unsupported content encoding") {
				t.Fatal("stacked encoding was not rejected explicitly", err)
			}
		})
	}
}

func TestFetchSingletonLocationHeader(t *testing.T) {
	for _, values := range [][]string{{"/first", "/second"}, {"/first", "/first"}, {"/first,part"}} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			for _, value := range values {
				w.Header().Add("Location", value)
			}
			w.WriteHeader(http.StatusFound)
		}))
		t.Cleanup(srv.Close)
		client := *srv.Client()
		client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
		_, _, _, location, err := fetchOnce(context.Background(), &client, MobileUserAgent, srv.URL, "navigation")
		if len(values) == 1 {
			if err != nil || location == nil || location.Path != values[0] {
				t.Fatal("single Location containing a comma rejected", err)
			}
		} else if err == nil || !strings.Contains(err.Error(), "multiple Location") {
			t.Fatal("ambiguous redirect accepted", err)
		}
	}
}
