package budget

import (
	"errors"
	"net/http"
	"strings"
	"testing"
)

func TestHTTPMeasureOpaqueContentTypes(t *testing.T) {
	// RFC 9110 §8.3.1: opaque assets still require a media type with both
	// type and subtype. Semantic media types cannot hide in the opaque kind.
	for _, tc := range []struct {
		name, kind string
		values     []string
		allowed    bool
	}{
		{"markdown", "other", []string{"text/markdown; charset=utf-8"}, true},
		{"webmanifest", "other", []string{"application/manifest+json; charset=utf-8"}, true},
		{"custom-media-type", "other", []string{"application/vnd.fixture+binary; version=1"}, true},
		{"mixed-case-and-ows", "other", []string{" \tTeXt/MaRkDoWn; CHARSET=\"utf-8\"; ;\t"}, true},
		{"missing", "other", nil, false},
		{"empty", "other", []string{""}, false},
		{"no-subtype", "other", []string{"markdown"}, false},
		{"empty-subtype", "other", []string{"text/"}, false},
		{"extra-slash", "other", []string{"text/markdown/extra"}, false},
		{"invalid-token", "other", []string{"text/mark down"}, false},
		{"invalid-parameter", "other", []string{"text/markdown; charset"}, false},
		{"multiple-values", "other", []string{"text/markdown", "application/json"}, false},
		{"javascript-as-html", "js", []string{"text/html"}, false},
		{"javascript", "js", []string{"application/javascript"}, true},
		{"program-as-markdown", "program", []string{"text/markdown"}, false},
		{"css-as-markdown", "css", []string{"text/markdown"}, false},
		{"wasm-as-manifest", "wasm", []string{"application/manifest+json"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := []byte("opaque fixture bytes")
			measurement, err := generatedMeasure(t, body, tc.kind, "identity", http.Header{"Content-Type": tc.values})
			if tc.allowed {
				if err != nil {
					t.Fatal(err)
				}
				if measurement.Sizes.Raw != int64(len(body)) || measurement.WireBytes != int64(len(body)) || measurement.Requests != 1 || !testHTTPPolicy(measurement, "served-matches-build") {
					t.Fatal("opaque MIME acceptance changed verified body accounting")
				}
				return
			}
			var input *InputError
			if !errors.As(err, &input) || input.Code != "policy" || input.Reference != "measure" || input.Pointer != "/contentType" {
				t.Fatalf("want Content-Type policy error, got %v", err)
			}
		})
	}
}

func TestHTTPMeasureOtherRejectsSemanticContentTypes(t *testing.T) {
	// MIME Sniffing §4.6: every JavaScript MIME essence, including legacy
	// names, denotes executable content. HTTP parameters do not change that.
	for _, media := range []string{
		"text/html", "text/css", "application/wasm",
		"text/javascript", "application/javascript", "application/ecmascript",
		"application/x-ecmascript", "application/x-javascript", "text/ecmascript",
		"text/javascript1.0", "text/javascript1.1", "text/javascript1.2",
		"text/javascript1.3", "text/javascript1.4", "text/javascript1.5",
		"text/jscript", "text/livescript", "text/x-ecmascript", "text/x-javascript",
	} {
		for _, value := range []string{media, strings.ToUpper(media) + "; charset=utf-8"} {
			t.Run(value, func(t *testing.T) {
				_, err := generatedMeasure(t, []byte("fixture bytes"), "other", "identity", http.Header{"Content-Type": {value}})
				var input *InputError
				if !errors.As(err, &input) || input.Code != "policy" || input.Pointer != "/contentType" {
					t.Fatalf("semantic body admitted as opaque: %v", err)
				}
			})
		}
	}
}
