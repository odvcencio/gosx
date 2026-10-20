package budget

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
)

func TestHTTPMeasureAllEnforcedCSPPolicies(t *testing.T) {
	body := []byte(`<script nonce="fixture">app()</script><style nonce="fixture">p{color:red}</style>`)
	allow := "default-src 'nonce-fixture'"
	deny := "script-src 'none'; style-src 'nonce-fixture'"
	for _, tc := range []struct {
		name     string
		policies []string
		allowed  bool
	}{
		{"conflicting-lines", []string{allow, deny}, false},
		{"mixed-case-conflicting-lines", []string{allow, "SCRIPT-SRC 'NONE'"}, false},
		{"mixed-case-compatible-lines", []string{allow, "SCRIPT-SRC 'NONCE-fixture'; STYLE-SRC 'NONCE-fixture'"}, true},
		{"conflicting-lines-reversed", []string{deny, allow}, false},
		{"conflicting-list", []string{allow + ", " + deny}, false},
		{"conflicting-list-first-policy", []string{"default-src 'none', " + allow}, false},
		{"conflicting-default-fallback", []string{allow, "default-src 'none'"}, false},
		{"conflicting-element-override", []string{allow, "script-src 'nonce-fixture'; script-src-elem 'none'"}, false},
		{"compatible-lines", []string{allow, "script-src 'nonce-fixture'; style-src 'nonce-fixture'"}, true},
		{"compatible-list", []string{allow + ", " + allow}, true},
		{"compatible-list-directives", []string{"script-src 'nonce-fixture'; style-src 'nonce-fixture', script-src 'nonce-fixture'; style-src 'nonce-fixture'"}, true},
		{"independent-directives", []string{"script-src 'nonce-fixture'", "style-src 'nonce-fixture'"}, true},
		{"unrelated-policy", []string{allow, "img-src 'none'"}, true},
		{"missing-policy", nil, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			opts := testHTTPOptions(t, func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/html")
				for _, policy := range tc.policies {
					w.Header().Add("Content-Security-Policy", policy)
				}
				w.Header().Add("Content-Security-Policy-Report-Only", "default-src 'none'")
				w.Write(body)
			}, body)
			opts.Kind = "html"
			_, err := measureHTTP(context.Background(), opts, testBodyNormalizer)
			if tc.allowed {
				if err != nil {
					t.Fatal("compatible policies rejected", err)
				}
				return
			}
			var typed *InputError
			if !errors.As(err, &typed) || typed.Code != "policy" || typed.Pointer != "/html/nonce" {
				t.Fatal("conflicting or missing policies accepted", err)
			}
		})
	}
}

func TestHTTPMeasureContentEncodingHeaderLists(t *testing.T) {
	body := []byte("fixture()")
	gz, _ := testMeasureEncodings(body)
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
			opts := testHTTPOptions(t, func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/javascript")
				for _, value := range tc.values {
					w.Header().Add("Content-Encoding", value)
				}
				w.Write(gz)
			}, body)
			_, err := measureHTTP(context.Background(), opts, testBodyNormalizer)
			if tc.allowed {
				if err != nil {
					t.Fatal(err)
				}
				return
			}
			var typed *InputError
			if !errors.As(err, &typed) || typed.Code != "policy" || typed.Pointer != "/encoding" {
				t.Fatal("stacked encoding was not rejected explicitly", err)
			}
		})
	}
}

func TestHTTPMeasureSingletonResponseHeaders(t *testing.T) {
	body := []byte("fixture()")
	for _, tc := range []struct {
		name, header, pointer string
		values                []string
		allowed               bool
	}{
		{"conflicting-types", "Content-Type", "/contentType", []string{"text/javascript", "text/html"}, false},
		{"repeated-types", "Content-Type", "/contentType", []string{"text/javascript", "text/javascript"}, false},
		{"conflicting-locations", "Location", "/redirect", []string{"/final", "/other"}, false},
		{"repeated-locations", "Location", "/redirect", []string{"/final", "/final"}, false},
		{"comma-in-location", "Location", "/redirect", []string{"/final,part"}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			requests := 0
			opts := testHTTPOptions(t, func(w http.ResponseWriter, r *http.Request) {
				requests++
				if strings.HasPrefix(r.URL.Path, "/asset.") {
					for _, value := range tc.values {
						w.Header().Add(tc.header, value)
					}
					if tc.header == "Location" {
						w.WriteHeader(http.StatusFound)
						return
					}
				} else {
					w.Header().Set("Content-Type", "text/javascript")
				}
				w.Write(body)
			}, body)
			_, err := measureHTTP(context.Background(), opts, testBodyNormalizer)
			if tc.allowed {
				if err != nil || requests != 2 {
					t.Fatal("single Location containing a comma rejected", err)
				}
				return
			}
			var typed *InputError
			if !errors.As(err, &typed) || typed.Pointer != tc.pointer || requests != 1 {
				t.Fatal("ambiguous singleton accepted or followed", err, requests)
			}
		})
	}
}
