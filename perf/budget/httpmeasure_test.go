package budget

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func testHTTPOptions(t *testing.T, handler http.HandlerFunc, body []byte) HTTPMeasureOptions {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	gz, br := testMeasureEncodings(body)
	return HTTPMeasureOptions{Client: server.Client(), BaseURL: server.URL, URL: "/asset." + testMeasureHash(body)[:16] + ".js", Kind: "js", ExpectedBody: body, ExpectedSHA256: testMeasureHash(body), Representations: map[string][]byte{"gzip": gz, "br": br}}
}
func testHTTPPolicy(result HTTPMeasurement, name string) bool {
	for _, policy := range result.Policies {
		if policy.Name == name {
			return policy.Passed
		}
	}
	return false
}

func TestHTTPMeasureServedEncodingIsSeparateFromCanonical(t *testing.T) {
	body := []byte(strings.Repeat("const fixtureValue=1;\n", 20))
	gz, br := testMeasureEncodings(body)
	for _, encoding := range []string{"identity", "gzip", "br"} {
		t.Run(encoding, func(t *testing.T) {
			wire := body
			if encoding == "gzip" {
				wire = gz
			}
			if encoding == "br" {
				wire = br
			}
			opts := testHTTPOptions(t, func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Accept-Encoding") != "br, gzip" {
					t.Error("encoding negotiation missing")
				}
				w.Header().Set("Content-Type", "text/javascript")
				w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
				w.Header().Set("Content-Encoding", encoding)
				w.Write(wire)
			}, body)
			result, err := measureHTTP(context.Background(), opts, testBodyNormalizer)
			if err != nil {
				t.Fatal(err)
			}
			canonical, _ := testBodyNormalizer(body)
			if result.Sizes != canonical || result.WireBytes != int64(len(wire)) || result.Requests != 1 || !testHTTPPolicy(result, "immutable-hashed") || !testHTTPPolicy(result, "served-matches-build") || testHTTPPolicy(result, "assets-compressed") != (encoding != "identity") {
				t.Fatal("wire and normalized measurements conflated")
			}
			if !bytes.Equal(result.body, body) {
				t.Fatal("decoded body differs")
			}
		})
	}

}

func TestHTTPMeasureRedirectBodyAndCookieAccounting(t *testing.T) {
	body := []byte("fixture()")
	redirectBody := []byte("redirect-body")
	opts := testHTTPOptions(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/asset.") {
			w.Header().Set("Location", "/final")
			w.Header().Add("Set-Cookie", "fixture=1")
			w.WriteHeader(302)
			w.Write(redirectBody)
			return
		}
		w.Header().Set("Content-Type", "application/javascript")
		w.Write(body)
	}, body)
	result, err := measureHTTP(context.Background(), opts, testBodyNormalizer)
	if err != nil {
		t.Fatal(err)
	}
	expected, _ := testBodyNormalizer(redirectBody)
	if result.Requests != 2 || result.WireBytes != int64(len(body)+len(redirectBody)) || len(result.RedirectSizes) != 1 || result.RedirectSizes[0] != expected || testHTTPPolicy(result, "no-cookie") {
		t.Fatal("redirect costs or cookie were omitted")
	}
}

func TestHTTPMeasureGLTFContentTypesStayBoundToModelBodies(t *testing.T) {
	for _, media := range []string{"model/gltf-binary", "model/gltf+json", "application/octet-stream", "text/javascript", "text/html"} {
		t.Run(media, func(t *testing.T) {
			body := []byte("model fixture")
			opts := testHTTPOptions(t, func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", media)
				w.Write(body)
			}, body)
			opts.Kind = "model"
			_, err := measureHTTP(context.Background(), opts, testBodyNormalizer)
			allowed := media == "model/gltf-binary" || media == "model/gltf+json" || media == "application/octet-stream"
			if (err == nil) != allowed {
				t.Fatal("model MIME policy accepted the wrong representation", err)
			}
			if allowed {
				opts.Kind = "program"
				_, err = measureHTTP(context.Background(), opts, testBodyNormalizer)
				if media != "application/octet-stream" && err == nil {
					t.Fatal("model MIME type admitted for an island program")
				}
			}
		})
	}
}

func TestHTTPMeasureRejectsWrongRepresentationsAndPolicies(t *testing.T) {
	body := []byte("fixture()")
	gz, _ := testMeasureEncodings(body)
	for _, name := range []string{"raw-hash", "sidecar", "undeclared-sidecar", "trailing", "encoding", "mime", "status", "cross-origin", "loop", "length", "nonce", "auto-decoded"} {
		t.Run(name, func(t *testing.T) {
			responseBody := body
			if name == "nonce" {
				responseBody = []byte(`<script nonce="fixture">app()</script>`)
			}
			opts := testHTTPOptions(t, func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/javascript")
				switch name {
				case "raw-hash":
					w.Write([]byte("different()"))
					return
				case "sidecar", "undeclared-sidecar", "trailing":
					w.Header().Set("Content-Encoding", "gzip")
					if name == "trailing" {
						w.Write(append(append([]byte{}, gz...), 0))
						return
					}
					w.Write(gz)
					return
				case "encoding":
					w.Header().Set("Content-Encoding", "zstd")
				case "mime":
					w.Header().Set("Content-Type", "text/html")
				case "status":
					w.WriteHeader(500)
				case "cross-origin":
					w.Header().Set("Location", "https://example.invalid/private")
					w.WriteHeader(302)
					return
				case "loop":
					w.Header().Set("Location", r.URL.Path)
					w.WriteHeader(302)
					return
				case "length":
					w.Header().Set("Content-Length", "100")
				case "nonce":
					w.Header().Set("Content-Type", "text/html")
					w.Header().Set("Content-Security-Policy", "script-src 'nonce-other'")
				}
				w.Write(responseBody)
			}, responseBody)
			switch name {
			case "sidecar":
				opts.Representations["gzip"] = []byte("stale")
			case "undeclared-sidecar":
				opts.Representations = nil
			case "nonce":
				bodyNonce := []byte(`<script nonce="fixture">app()</script>`)
				opts.ExpectedBody = bodyNonce
				opts.ExpectedSHA256 = testMeasureHash(bodyNonce)
				opts.Kind = "html"
			case "auto-decoded":
				opts.Client = &http.Client{Transport: testRoundTrip(func(*http.Request) (*http.Response, error) {
					return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"text/javascript"}}, Body: io.NopCloser(bytes.NewReader(body)), ContentLength: int64(len(body)), Uncompressed: true}, nil
				})}
			}
			_, err := measureHTTP(context.Background(), opts, testBodyNormalizer)
			var typed *InputError
			if !errors.As(err, &typed) || typed.Pointer == "" || strings.Contains(err.Error(), "private") {
				t.Fatal("invalid response accepted or leaked", err)
			}
			if name == "nonce" && typed.Pointer != "/html/nonce" {
				t.Fatal("nonce check was not exercised", err)
			}
		})
	}
}

type testRoundTrip func(*http.Request) (*http.Response, error)

func (f testRoundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestHTTPMeasureRefusesPrivateInputAndCancellation(t *testing.T) {
	body := []byte("fixture()")
	requests := 0
	opts := testHTTPOptions(t, func(w http.ResponseWriter, r *http.Request) {
		requests++
		w.Header().Set("Content-Type", "text/javascript")
		w.Write(body)
	}, body)
	for _, value := range []string{"https://example.invalid/private", "//example.invalid/private", "/asset?q=private", "/asset#private", "/../asset", "/asset%2fprivate"} {
		invalid := opts
		invalid.URL = value
		if _, err := measureHTTP(context.Background(), invalid, testBodyNormalizer); err == nil {
			t.Fatal("invalid request identity accepted")
		}
	}
	if requests != 0 {
		t.Fatal("invalid input started a request")
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := measureHTTP(cancelled, opts, testBodyNormalizer); err == nil || strings.Contains(err.Error(), "http") {
		t.Fatal("cancellation error leaked", err)
	}
	if _, err := MeasureHTTP(context.Background(), opts); err == nil {
		t.Fatal("production accepted unpinned compressor")
	}
}

func TestHTTPMeasureCachePolicyNeedsTheHashedFilename(t *testing.T) {
	body := []byte("fixture()")
	for _, name := range []string{"no-immutable", "wrong-hash", "hash-directory", "cookie", "private-html"} {
		t.Run(name, func(t *testing.T) {
			opts := testHTTPOptions(t, func(w http.ResponseWriter, r *http.Request) {
				media := "text/javascript"
				cache := "public, max-age=31536000, immutable"
				if name == "no-immutable" {
					cache = "public, max-age=31536000"
				}
				if name == "private-html" {
					media, cache = "text/html", "private, no-store"
				}
				if name == "cookie" {
					w.Header().Add("Set-Cookie", "fixture=1")
				}
				w.Header().Set("Content-Type", media)
				w.Header().Set("Cache-Control", cache)
				w.Write(body)
			}, body)
			if name == "wrong-hash" {
				opts.URL = "/asset.1111111111111111.js"
			}
			if name == "hash-directory" {
				opts.URL = "/" + opts.ExpectedSHA256 + "/asset.js"
			}
			if name == "private-html" {
				opts.Kind = "html"
			}
			result, err := measureHTTP(context.Background(), opts, testBodyNormalizer)
			if err != nil {
				t.Fatal(err)
			}
			switch name {
			case "cookie":
				if testHTTPPolicy(result, "no-cookie") {
					t.Fatal("cookie ignored")
				}
			case "private-html":
				if testHTTPPolicy(result, "html-shareable") {
					t.Fatal("private HTML treated as shareable")
				}
			default:
				if testHTTPPolicy(result, "immutable-hashed") {
					t.Fatal("cache policy accepted unverified filename/directive")
				}
			}
		})
	}
}

func TestHTTPMeasureHTMLCacheControlAcrossHeaderLines(t *testing.T) {
	body := []byte("<html>fixture</html>")
	for _, tc := range []struct {
		name      string
		headers   []string
		shareable bool
	}{
		{"public", []string{"public, max-age=60"}, true},
		{"single-line", []string{"public, max-age=60, private, no-store"}, false},
		{"separate-lines", []string{"public, max-age=60", "private, no-store"}, false},
		{"separate-private", []string{"public, max-age=60", "private"}, false},
		{"separate-no-store", []string{"public, max-age=60", "no-store"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			opts := testHTTPOptions(t, func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/html")
				for _, value := range tc.headers {
					w.Header().Add("Cache-Control", value)
				}
				w.Write(body)
			}, body)
			opts.Kind = "html"
			result, err := measureHTTP(context.Background(), opts, testBodyNormalizer)
			if err != nil {
				t.Fatal(err)
			}
			if len(result.header.Values("Cache-Control")) != len(tc.headers) {
				t.Fatal("response did not preserve separate header lines")
			}
			if got := testHTTPPolicy(result, "html-shareable"); got != tc.shareable {
				t.Fatalf("html-shareable = %v, want %v", got, tc.shareable)
			}
		})
	}
}
