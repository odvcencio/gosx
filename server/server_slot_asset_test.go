package server

import (
	"io"
	runtimehost "m31labs.dev/gosx/client/runtime/host"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestServerSlotAssetHTTP(t *testing.T) {
	s := httptest.NewServer(New().Build())
	defer s.Close()
	for _, method := range []string{http.MethodGet, http.MethodHead} {
		for _, version := range []string{runtimehost.ServerSlotRuntimeHash(), "stale"} {
			req, _ := http.NewRequest(method, s.URL+"/gosx/server-slots.js?v="+version, nil)
			resp, err := s.Client().Do(req)
			if err != nil {
				t.Fatal(err)
			}
			body, err := io.ReadAll(resp.Body)
			resp.Body.Close()
			if err != nil {
				t.Fatal(err)
			}
			if version == "stale" {
				if resp.StatusCode != 404 || resp.Header.Get("Cache-Control") != "" {
					t.Fatal("stale asset cached", resp.Status, resp.Header)
				}
				continue
			}
			if resp.StatusCode != 200 || resp.Header.Get("Cache-Control") != "public, max-age=31536000, immutable" {
				t.Fatal(resp.Status, resp.Header)
			}
			if method == http.MethodGet && string(body) != runtimehost.ServerSlotRuntime() {
				t.Fatal("served asset differs from embedded identity")
			}
			if method == http.MethodHead && len(body) != 0 {
				t.Fatal("HEAD body")
			}
		}
	}
}
