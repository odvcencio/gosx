package budget

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"m31labs.dev/gosx/internal/pagecaps"
	"m31labs.dev/gosx/server"
)

func testHTMLCompression(encoding string) server.Middleware {
	if encoding == "gzip" {
		return server.GzipMiddleware()
	}
	return server.CompressionMiddleware()
}

func testFlushHTML(t *testing.T, w http.ResponseWriter, body []byte) {
	t.Helper()
	for start := 0; start < len(body); {
		end := min(start+1024, len(body))
		if _, err := w.Write(body[start:end]); err != nil {
			t.Error(err)
			return
		}
		if end < len(body) {
			if err := http.NewResponseController(w).Flush(); err != nil {
				t.Error(err)
				return
			}
		}
		start = end
	}
}

func TestMeasureFlushedHTMLMiddleware(t *testing.T) {
	for _, encoding := range []string{"gzip", "br"} {
		t.Run(encoding, func(t *testing.T) {
			opts, manifest, document, _ := testRouteMeasurement(t)
			content := strings.Repeat("<p>Stable streaming fixture content.</p>", 256)
			build := bytes.Replace(document, []byte("</body>"), []byte(content+`<script nonce="build">fixtureApp()</script></body>`), 1)
			manifest.Assets = manifest.Assets[:1]
			manifest.Assets[0].SHA256 = testMeasureHash(build)
			manifest.Routes[0].PageTypes = []string{"enhanced"}
			caps, err := pagecaps.FromHTML(build)
			if err != nil {
				t.Fatal(err)
			}
			manifest.Routes[0].Capabilities = caps
			if err := os.WriteFile(filepath.Join(opts.DistDir, "counter/index.html"), build, 0600); err != nil {
				t.Fatal(err)
			}
			writeTestFixtureManifest(t, opts.DistDir, manifest)
			var renders atomic.Int64
			handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				nonce := "render-" + strconv.FormatInt(renders.Add(1), 10)
				body := bytes.Replace(build, []byte(`nonce="build"`), []byte(`nonce="`+nonce+`"`), 1)
				w.Header().Set("Content-Type", "text/html")
				w.Header().Set("Content-Security-Policy", "script-src 'nonce-"+nonce+"'")
				testFlushHTML(t, w, body)
			})
			httpServer := httptest.NewServer(testHTMLCompression(encoding)(handler))
			t.Cleanup(httpServer.Close)
			transport := httpServer.Client().Transport
			var observed [][]byte
			opts.BaseURL = httpServer.URL
			opts.Client = &http.Client{Transport: testRoundTrip(func(req *http.Request) (*http.Response, error) {
				response, err := transport.RoundTrip(req)
				if err != nil {
					return nil, err
				}
				wire, err := io.ReadAll(response.Body)
				closeErr := response.Body.Close()
				if err != nil {
					return nil, err
				}
				if closeErr != nil {
					return nil, closeErr
				}
				if strings.Join(response.Header.Values("Content-Encoding"), ",") != encoding {
					t.Error("production middleware did not compress the stream")
				}
				observed = append(observed, wire)
				response.Body = io.NopCloser(bytes.NewReader(wire))
				return response, nil
			})}
			report, err := measureApp(context.Background(), opts, testBodyNormalizer)
			httpServer.Close()
			if err != nil {
				t.Fatal("flushed production HTML rejected", err)
			}
			if renders.Load() != 2 || len(observed) != 2 || len(report.Rows) != 1 {
				t.Fatal("fresh streamed renders were not verified")
			}
			profile := "go-brotli-4"
			if encoding == "gzip" {
				profile = "go-gzip-default"
			}
			raw, err := decodeServedBody(observed[0], encoding)
			if err != nil {
				t.Fatal(err)
			}
			singleWrite, err := encodeServingHTML(raw, encoding, profile)
			if err != nil || bytes.Equal(observed[0], singleWrite) {
				t.Fatal("fixture did not distinguish flushed and single-write encodings", err)
			}
			canonical, err := measureHTML(build, HTMLMeasureOptions{Fields: []HTMLField{{Element: "script", Attribute: "nonce"}}}, testBodyNormalizer)
			if err != nil {
				t.Fatal(err)
			}
			row := report.Rows[0]
			if row.WireBytes != int64(len(observed[0])) || row.Requests != 1 || row.NormalizedBytes != canonical.Sizes.Brotli || !testHTTPPolicy(HTTPMeasurement{Policies: row.Policies}, "html-compressed") {
				t.Fatal("observed wire bytes and canonical normalization were conflated", row)
			}
		})
	}
}

func TestHTTPMeasureLiveHTMLAndReleaseSidecars(t *testing.T) {
	body := []byte(`<html><body>` + strings.Repeat("<p>Stable content.</p>", 256) + `<script nonce="fixture">fixtureApp()</script></body></html>`)
	for _, encoding := range []string{"gzip", "br"} {
		profile := "go-brotli-4"
		if encoding == "gzip" {
			profile = "go-gzip-default"
		}
		singleWrite, err := encodeServingHTML(body, encoding, profile)
		if err != nil {
			t.Fatal(err)
		}
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodGet, "/fixture/", nil)
		request.Header.Set("Accept-Encoding", "br, gzip")
		testHTMLCompression(encoding)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/html")
			testFlushHTML(t, w, body)
		})).ServeHTTP(recorder, request)
		flushed := bytes.Clone(recorder.Body.Bytes())
		if bytes.Equal(flushed, singleWrite) {
			t.Fatal("fixture encodings do not differ")
		}
		for _, tc := range []struct {
			name, failure string
		}{
			{"live-flushed", ""},
			{"release-flushed", ""},
			{"release-mismatch", "/encoding"},
			{"live-undeclared", "/encoding"},
			{"live-wrong-profile", "/encoding"},
			{"live-stacked-coding", "/encoding"},
			{"live-non-html", "/encoding"},
			{"live-body-mismatch", "/body"},
			{"live-csp-mismatch", "/html/nonce"},
			{"live-trailing-data", "/encoding"},
		} {
			t.Run(encoding+"/"+tc.name, func(t *testing.T) {
				wire := flushed
				if tc.name == "release-mismatch" {
					wire = singleWrite
				}
				if tc.name == "live-trailing-data" {
					wire = append(bytes.Clone(flushed), 0)
				}
				csp := "script-src 'nonce-fixture'"
				if tc.name == "live-csp-mismatch" {
					csp = "script-src 'none'"
				}
				opts := testHTTPOptions(t, func(w http.ResponseWriter, r *http.Request) {
					w.Header().Set("Content-Type", "text/html")
					w.Header().Set("Content-Encoding", encoding)
					if tc.name == "live-stacked-coding" {
						w.Header().Add("Content-Encoding", "gzip")
					}
					w.Header().Set("Content-Security-Policy", csp)
					w.Write(wire)
				}, body)
				opts.Kind, opts.Representations = "html", nil
				opts.ServingCompressors = map[string]string{encoding: profile}
				switch tc.name {
				case "release-flushed", "release-mismatch":
					opts.Representations = map[string][]byte{encoding: flushed}
				case "live-undeclared":
					opts.ServingCompressors = nil
				case "live-wrong-profile":
					opts.ServingCompressors[encoding] = "go-unknown"
				case "live-non-html":
					opts.Kind = "js"
				case "live-body-mismatch":
					opts.ExpectedBody = bytes.Replace(body, []byte("Stable"), []byte("Changed"), 1)
					opts.ExpectedSHA256 = testMeasureHash(opts.ExpectedBody)
				}
				result, err := measureHTTP(context.Background(), opts, testBodyNormalizer)
				if tc.failure == "" {
					if err != nil || result.WireBytes != int64(len(wire)) || !bytes.Equal(result.body, body) || !testHTTPPolicy(result, "html-compressed") {
						t.Fatal("valid served representation rejected or miscounted", result, err)
					}
					return
				}
				var typed *InputError
				if !errors.As(err, &typed) || typed.Pointer != tc.failure {
					t.Fatal("live content or release artifact check was bypassed", err)
				}
			})
		}
	}
}
