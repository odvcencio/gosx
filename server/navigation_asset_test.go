package server

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/andybalholm/brotli"
	"golang.org/x/net/html"
	"m31labs.dev/gosx"
)

func navigationScriptAttrs(t *testing.T, page string) map[string]string {
	t.Helper()
	z := html.NewTokenizer(strings.NewReader(page))
	var found map[string]string
	for {
		switch z.Next() {
		case html.ErrorToken:
			return found
		case html.StartTagToken:
			token := z.Token()
			if token.Data != "script" {
				continue
			}
			attrs := make(map[string]string, len(token.Attr))
			for _, attr := range token.Attr {
				attrs[attr.Key] = attr.Val
			}
			if attrs["data-gosx-navigation"] != "true" {
				continue
			}
			if found != nil {
				t.Fatal("page emitted more than one navigation script")
			}
			found = attrs
			if z.Next() != html.EndTagToken {
				t.Fatal("navigation script contains an inline runtime")
			}
		}
	}
}

func TestNavigationAssetHashedCompressedAndIntegrity(t *testing.T) {
	app := New()
	app.SetRuntimeRoot(t.TempDir()) // No build or manifest: go run and dev fallback.
	app.EnableNavigation()
	app.Page("GET /", func(ctx *Context) gosx.Node {
		ctx.SetNonce("page-nonce")
		return Link("/next", "Next")
	})
	handler := app.Build()
	page := httptest.NewRecorder()
	handler.ServeHTTP(page, httptest.NewRequest(http.MethodGet, "/", nil))
	attrs := navigationScriptAttrs(t, page.Body.String())
	if attrs == nil || attrs["crossorigin"] != "anonymous" || attrs["nonce"] != "page-nonce" {
		t.Fatalf("navigation attributes = %#v", attrs)
	}
	if _, ok := attrs["defer"]; !ok {
		t.Fatal("navigation script must defer until the document is parsed")
	}
	for _, accept := range []string{"", "gzip", "br, gzip", "br;q=0, gzip", "br;q=0, gzip;q=0"} {
		t.Run("encoding="+accept, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, attrs["src"], nil)
			req.Header.Set("Accept-Encoding", accept)
			res := httptest.NewRecorder()
			handler.ServeHTTP(res, req)
			if res.Code != http.StatusOK {
				t.Fatalf("asset status = %d", res.Code)
			}
			if got := res.Header().Get("Cache-Control"); got != "public, max-age=31536000, immutable" {
				t.Fatalf("Cache-Control = %q", got)
			}
			if !strings.Contains(res.Header().Get("Vary"), "Accept-Encoding") || !strings.HasPrefix(res.Header().Get("Content-Type"), "application/javascript") {
				t.Fatalf("asset headers = %v", res.Header())
			}
			var reader io.Reader = res.Body
			compressedSize := res.Body.Len()
			wantEncoding := ""
			switch accept {
			case "gzip", "br;q=0, gzip":
				wantEncoding = "gzip"
				gz, err := gzip.NewReader(reader)
				if err != nil {
					t.Fatal(err)
				}
				defer gz.Close()
				reader = gz
			case "br, gzip":
				wantEncoding = "br"
				reader = brotli.NewReader(reader)
			}
			if got := res.Header().Get("Content-Encoding"); got != wantEncoding {
				t.Fatalf("Content-Encoding = %q, want %q", got, wantEncoding)
			}
			data, err := io.ReadAll(reader)
			if err != nil {
				t.Fatal(err)
			}
			digest := sha256.Sum256(data)
			if want := "sha256-" + base64.StdEncoding.EncodeToString(digest[:]); attrs["integrity"] != want {
				t.Fatalf("integrity = %q, want %q", attrs["integrity"], want)
			}
			if want := "/gosx/assets/runtime/navigation." + hex.EncodeToString(digest[:8]) + ".js"; attrs["src"] != want {
				t.Fatalf("src = %q, want %q", attrs["src"], want)
			}
			if wantEncoding != "" && compressedSize >= len(data) {
				t.Fatal("compressed asset is not smaller than the runtime")
			}
		})
	}
	get := httptest.NewRecorder()
	handler.ServeHTTP(get, httptest.NewRequest(http.MethodGet, attrs["src"], nil))
	head := httptest.NewRecorder()
	handler.ServeHTTP(head, httptest.NewRequest(http.MethodHead, attrs["src"], nil))
	if head.Code != http.StatusOK || head.Body.Len() != 0 || head.Header().Get("Content-Length") != get.Header().Get("Content-Length") {
		t.Fatalf("HEAD status/body/headers = %d/%d/%v", head.Code, head.Body.Len(), head.Header())
	}
	req := httptest.NewRequest(http.MethodGet, attrs["src"], nil)
	req.Header.Set("If-None-Match", get.Header().Get("ETag"))
	conditional := httptest.NewRecorder()
	handler.ServeHTTP(conditional, req)
	if conditional.Code != http.StatusNotModified {
		t.Fatalf("conditional status = %d", conditional.Code)
	}
	unknown := httptest.NewRecorder()
	handler.ServeHTTP(unknown, httptest.NewRequest(http.MethodGet, "/gosx/assets/runtime/navigation.0000000000000000.js", nil))
	if unknown.Code != http.StatusNotFound || bytes.Contains(unknown.Body.Bytes(), navigationAsset.raw) {
		t.Fatalf("unknown hash status = %d", unknown.Code)
	}
}
