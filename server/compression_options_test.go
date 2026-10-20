package server

import (
	"bytes"
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRouteCompressionOptionsPreserveNegotiationAndBody(t *testing.T) {
	body := strings.Repeat(`{"shader":"material","revision":18446744073709551615}`, 200)
	route := CompressionMiddlewareWithOptions(CompressionOptions{GzipOnly: true, GzipLevel: gzip.BestSpeed})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "private, no-store")
		w.Header().Set("Vary", "Origin")
		_, _ = io.WriteString(w, body)
	}))
	for _, tc := range []struct {
		name, accept string
		encoded      bool
	}{{"gzip", "br, gzip", true}, {"excluded", "*;q=1, gzip;q=0", false}, {"wildcard", "*", true}, {"identity", "", false}} {
		t.Run(tc.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			r := httptest.NewRequest("GET", "/scene", nil)
			r.Header.Set("Accept-Encoding", tc.accept)
			route.ServeHTTP(w, r)
			got := w.Body.Bytes()
			if tc.encoded {
				if w.Header().Get("Content-Encoding") != "gzip" {
					t.Fatal(w.Header())
				}
				z, err := gzip.NewReader(bytes.NewReader(got))
				if err != nil {
					t.Fatal(err)
				}
				got, err = io.ReadAll(z)
				_ = z.Close()
				if err != nil {
					t.Fatal(err)
				}
			} else if w.Header().Get("Content-Encoding") != "" {
				t.Fatal(w.Header())
			}
			if string(got) != body || w.Header().Get("Cache-Control") != "private, no-store" {
				t.Fatal("body or privacy changed")
			}
		})
	}
}
