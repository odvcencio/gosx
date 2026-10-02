package server

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func writeCompressionFixture(t *testing.T, target string, raw []byte, variants string) map[string][]byte {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, raw, 0644); err != nil {
		t.Fatal(err)
	}
	encoded := map[string][]byte{}
	if strings.Contains(variants, "br") {
		encoded["br"] = encodeBrotliResponse(t, raw)
	}
	if strings.Contains(variants, "gzip") {
		encoded["gzip"] = encodeGzipResponse(t, raw)
	}
	for encoding, body := range encoded {
		ext := ".br"
		if encoding == "gzip" {
			ext = ".gz"
		}
		if err := os.WriteFile(target+ext, body, 0644); err != nil {
			t.Fatal(err)
		}
	}
	return encoded
}

func TestCompressionFileServing(t *testing.T) {
	for _, kind := range []string{"public", "isr"} {
		for _, tc := range []struct {
			name, variants, accept, want string
			disabled                     bool
		}{
			{"brotli", "br,gzip", "br, gzip", "br", false},
			{"gzip", "br,gzip", "gzip", "gzip", false},
			{"brotli-rejected", "br,gzip", "br;q=0, gzip", "gzip", false},
			{"all-rejected", "br,gzip", "br;q=0, gzip;q=0", "", false},
			{"identity", "br,gzip", "identity", "", false},
			{"missing-header", "br,gzip", "", "", false},
			{"gzip-sidecar-fallback", "gzip", "br, gzip", "gzip", false},
			{"dynamic-brotli", "", "br, gzip", "br", false},
			{"dynamic-gzip", "", "gzip", "gzip", false},
			{"disabled", "br,gzip", "br, gzip", "", true},
		} {
			t.Run(kind+"/"+tc.name, func(t *testing.T) {
				root := t.TempDir()
				target, url := filepath.Join(root, "public", "styles.css"), "/styles.css"
				if kind == "isr" {
					target, url = filepath.Join(root, "static", "index.html"), "/"
					writeISRManifest(t, root, isrManifest{Pages: []string{"/"}, Routes: []isrRoute{{Path: "/", File: "index.html"}}})
				}
				raw := bytes.Repeat([]byte("text response body "), 256)
				encoded := writeCompressionFixture(t, target, raw, tc.variants)
				app := New()
				app.SetPublicDir(filepath.Join(root, "public"))
				app.SetRuntimeRoot(root)
				if kind == "isr" {
					app.EnableISR()
				}
				if tc.disabled {
					app.DisableCompression()
				}
				r := httptest.NewRequest("GET", url, nil)
				r.Header.Set("Accept", "text/html")
				r.Header.Set("Accept-Encoding", tc.accept)
				w := httptest.NewRecorder()
				app.Build().ServeHTTP(w, r)
				headers := w.Result().Header
				if headers.Get("Content-Encoding") != tc.want {
					t.Fatalf("encoding = %q, want %q", headers.Get("Content-Encoding"), tc.want)
				}
				if !bytes.Equal(decodeCompressedResponse(t, tc.want, w.Body.Bytes()), raw) {
					t.Fatal("file body changed")
				}
				if kind == "isr" && headers.Get("X-Gosx-Isr") != "HIT" {
					t.Fatal("static export did not serve through ISR")
				}
				if variant, ok := encoded[tc.want]; ok {
					if !bytes.Equal(w.Body.Bytes(), variant) || headers.Get("Content-Length") != strconv.Itoa(len(variant)) {
						t.Fatal("sidecar bytes were not served directly")
					}
				} else if tc.want != "" && headers.Get("Content-Length") != "" {
					t.Fatal("dynamic fallback retained identity content length")
				}
				if !tc.disabled && !strings.Contains(strings.Join(headers.Values("Vary"), ","), "Accept-Encoding") {
					t.Fatal("file response missing Vary")
				}
			})
		}
	}
}

func TestCompressionFileSkips(t *testing.T) {
	for _, kind := range []string{"public", "isr"} {
		for _, method := range []string{"HEAD", "GET"} {
			t.Run(kind+"/"+method, func(t *testing.T) {
				root := t.TempDir()
				target, url := filepath.Join(root, "public", "styles.css"), "/styles.css"
				if kind == "isr" {
					target, url = filepath.Join(root, "static", "index.html"), "/"
					writeISRManifest(t, root, isrManifest{Pages: []string{"/"}, Routes: []isrRoute{{Path: "/", File: "index.html"}}})
				}
				raw := bytes.Repeat([]byte("page body "), 256)
				writeCompressionFixture(t, target, raw, "br,gzip")
				app := New()
				app.SetPublicDir(filepath.Join(root, "public"))
				app.SetRuntimeRoot(root)
				app.EnableISR()
				r := httptest.NewRequest(method, url, nil)
				r.Header.Set("Accept", "text/html")
				r.Header.Set("Accept-Encoding", "br, gzip")
				if method == "GET" {
					r.Header.Set("Range", "bytes=0-9")
				}
				w := httptest.NewRecorder()
				app.Build().ServeHTTP(w, r)
				if w.Result().Header.Get("Content-Encoding") != "" {
					t.Fatal("HEAD and Range must bypass sidecars")
				}
				if method == "GET" && (w.Code != 206 || !bytes.Equal(w.Body.Bytes(), raw[:10])) {
					t.Fatal("range did not refer to identity bytes")
				}
				if method == "HEAD" && w.Body.Len() != 0 {
					t.Fatal("HEAD returned a body")
				}
			})
		}
	}
}

func TestCompressionPublicSidecarPolicy(t *testing.T) {
	for _, policy := range []string{"symlink", "excluded", "stale"} {
		t.Run(policy, func(t *testing.T) {
			root := t.TempDir()
			target := filepath.Join(root, "public", "styles.css")
			raw := bytes.Repeat([]byte(".page {color: black;} "), 128)
			writeCompressionFixture(t, target, raw, "br")
			app := New()
			app.SetPublicDir(filepath.Join(root, "public"))
			switch policy {
			case "symlink":
				if err := os.Remove(target + ".br"); err != nil {
					t.Fatal(err)
				}
				outside := filepath.Join(root, "outside.br")
				if err := os.WriteFile(outside, []byte("private bytes"), 0644); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(outside, target+".br"); err != nil {
					t.Fatal(err)
				}
			case "excluded":
				app.publicPolicy.Exclude = []string{"public/styles.css.br"}
			case "stale":
				old := time.Now().Add(-time.Hour)
				if err := os.Chtimes(target+".br", old, old); err != nil {
					t.Fatal(err)
				}
			}
			r := httptest.NewRequest("GET", "/styles.css", nil)
			r.Header.Set("Accept-Encoding", "br")
			w := httptest.NewRecorder()
			app.Build().ServeHTTP(w, r)
			if !bytes.Equal(decodeCompressedResponse(t, "br", w.Body.Bytes()), raw) || w.Result().Header.Get("Content-Length") != "" {
				t.Fatal("unsafe or stale sidecar must fall back to dynamic compression")
			}
		})
	}
}

func TestCompressionISRRegenerationRemovesVariants(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "index.html")
	raw := bytes.Repeat([]byte("old page "), 256)
	writeCompressionFixture(t, target, raw, "br,gzip")
	store := NewInMemoryISRStore()
	newBody := bytes.Repeat([]byte("new page "), 256)
	info, err := store.WriteArtifact(root, "/", "index.html", newBody)
	if err != nil {
		t.Fatal(err)
	}
	for _, encoding := range []string{"br", "gzip"} {
		if _, err := store.ReadCompressedArtifact(root, "/", "index.html", encoding, info.ModTime); err == nil {
			t.Fatal("regenerated HTML retained an obsolete variant")
		}
	}
	artifact, err := store.ReadArtifact(root, "/", "index.html")
	if err != nil || !bytes.Equal(artifact.Body, newBody) {
		t.Fatal("regeneration did not preserve the new identity body")
	}
}

func TestCompressionPublicSidecarTypesAndSize(t *testing.T) {
	for _, tc := range []struct {
		name string
		size int
		want string
	}{{"small.css", 1000, ""}, {"app.wasm", 4096, ""}, {"extensionless", 4096, "br"}} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			writeCompressionFixture(t, filepath.Join(root, tc.name), bytes.Repeat([]byte("a"), tc.size), "br,gzip")
			app := New()
			app.SetPublicDir(root)
			r := httptest.NewRequest("GET", "/"+tc.name, nil)
			r.Header.Set("Accept-Encoding", "br, gzip")
			w := httptest.NewRecorder()
			app.Build().ServeHTTP(w, r)
			if w.Result().Header.Get("Content-Encoding") != tc.want {
				t.Fatalf("encoding = %q, want %q", w.Result().Header.Get("Content-Encoding"), tc.want)
			}
		})
	}
}

func TestCompressionFilePreconditions(t *testing.T) {
	for _, kind := range []string{"public", "isr"} {
		for _, encoding := range []string{"br", "gzip"} {
			t.Run(kind+"/"+encoding, func(t *testing.T) {
				root := t.TempDir()
				target, path := filepath.Join(root, "public", "styles.css"), "/styles.css"
				if kind == "isr" {
					target, path = filepath.Join(root, "static", "index.html"), "/"
					writeISRManifest(t, root, isrManifest{Pages: []string{"/"}, Routes: []isrRoute{{Path: "/", File: "index.html"}}})
				}
				writeCompressionFixture(t, target, bytes.Repeat([]byte("page body "), 256), "br,gzip")
				app := New()
				app.SetPublicDir(filepath.Join(root, "public"))
				app.SetRuntimeRoot(root)
				if kind == "isr" {
					app.EnableISR()
				}
				app.Use(func(next http.Handler) http.Handler {
					return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						w.Header().Set("ETag", `"page"`)
						next.ServeHTTP(w, r)
					})
				})
				srv := httptest.NewServer(app.Build())
				defer srv.Close()
				for _, tc := range []struct {
					name, header, value string
					status              int
				}{
					{"if-match", "If-Match", `"different"`, http.StatusPreconditionFailed},
					{"if-unmodified-since", "If-Unmodified-Since", time.Now().Add(-time.Hour).UTC().Format(http.TimeFormat), http.StatusPreconditionFailed},
					{"if-none-match", "If-None-Match", `W/"page"`, http.StatusNotModified},
					{"if-modified-since", "If-Modified-Since", time.Now().Add(time.Hour).UTC().Format(http.TimeFormat), http.StatusNotModified},
				} {
					t.Run(tc.name, func(t *testing.T) {
						req, err := http.NewRequest(http.MethodGet, srv.URL+path, nil)
						if err != nil {
							t.Fatal(err)
						}
						req.Header.Set("Accept", "text/html")
						req.Header.Set("Accept-Encoding", encoding)
						req.Header.Set(tc.header, tc.value)
						res, err := srv.Client().Do(req)
						if err != nil {
							t.Fatal(err)
						}
						defer res.Body.Close()
						body, err := io.ReadAll(res.Body)
						if err != nil {
							t.Fatalf("reading conditional response: %v", err)
						}
						if res.StatusCode != tc.status || len(body) != 0 {
							t.Fatalf("status = %d, body length = %d; want %d with no body", res.StatusCode, len(body), tc.status)
						}
						if res.Header.Get("Content-Encoding") != "" || (res.Header.Get("Content-Length") != "" && res.Header.Get("Content-Length") != "0") {
							t.Fatalf("bodyless response advertises compressed bytes: %v", res.Header)
						}
						if !strings.Contains(strings.Join(res.Header.Values("Vary"), ","), "Accept-Encoding") {
							t.Fatal("conditional response missing Vary")
						}
					})
				}
			})
		}
	}
}
