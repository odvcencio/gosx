package budget

import (
	"context"
	"errors"
	"fmt"
	"html"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"m31labs.dev/gosx/internal/pagecaps"
)

func TestMeasureScriptExecutionAdmission(t *testing.T) {
	// MIME Sniffing §4.6's full JavaScript essence list. MIME parameters are
	// ignored by the measurement contract; module remains an exact token.
	// https://mimesniff.spec.whatwg.org/#javascript-mime-type
	// HTML §4.12.1.1 defines type/language precedence and classic nomodule.
	// https://html.spec.whatwg.org/multipage/scripting.html#prepare-the-script-element
	mimes := []string{
		"text/javascript", "application/javascript", "application/ecmascript",
		"application/x-ecmascript", "application/x-javascript", "text/ecmascript",
		"text/javascript1.0", "text/javascript1.1", "text/javascript1.2",
		"text/javascript1.3", "text/javascript1.4", "text/javascript1.5",
		"text/jscript", "text/livescript", "text/x-ecmascript", "text/x-javascript",
	}
	cases := []struct {
		name, attributes string
		executes         bool
	}{
		{"absent", "", true}, {"empty", `type=""`, true},
		{"module", `type="module"`, true}, {"module-case", `type="MoDuLe"`, true},
		{"module-nomodule", `type="module" nomodule`, true},
		{"json", `type="application/json"`, false}, {"template", `type="text/template"`, false},
		{"module-parameter", `type="module; charset=utf-8"`, false},
		{"module-shaped", `type="text/module; charset=utf-8"`, false},
		{"module-whitespace", `type=" module "`, false},
		{"whitespace-type", "type=\" \t\r\n\f\"", false},
		{"unicode-whitespace", `type=" text/javascript "`, false},
		{"absent-nomodule", `nomodule`, false}, {"empty-nomodule", `type="" nomodule="false"`, false},
		{"language-empty", `language=""`, true}, {"language-js", `language="JavaScript"`, true},
		{"language-ecmascript", `language="ecmascript"`, true},
		{"language-legacy", `language="JavaScript1.5"`, true},
		{"language-unsupported", `language="vbscript"`, false},
		{"language-parameter", `language="javascript; charset=utf-8"`, false},
		{"language-whitespace", `language="javascript "`, false},
		{"language-nomodule", `language="javascript" nomodule`, false},
		{"type-overrides-language", `type="text/javascript" language="vbscript"`, true},
		{"empty-overrides-language", `type="" language="vbscript"`, true},
		{"inert-overrides-language", `type="application/json" language="javascript"`, false},
	}
	for _, typ := range mimes {
		for _, variant := range []string{typ, strings.ToUpper(typ), " \t" + typ + "; charset=utf-8 "} {
			cases = append(cases, struct {
				name, attributes string
				executes         bool
			}{typ + "/" + variant, `type="` + html.EscapeString(variant) + `"`, true})
		}
		cases = append(cases, struct {
			name, attributes string
			executes         bool
		}{typ + "/nomodule", fmt.Sprintf("type=%q nomodule", typ), false})
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			opts, manifest, document, _ := testRouteMeasurement(t)
			body := []byte(strings.Replace(string(document), "</body>", `<script nonce="fixture" `+tc.attributes+`>app()</script></body>`, 1))
			manifest.Assets = manifest.Assets[:1]
			manifest.Assets[0].SHA256 = testMeasureHash(body)
			caps, err := pagecaps.FromHTML(body)
			if err != nil {
				t.Fatal(err)
			}
			manifest.Routes[0].Capabilities = caps
			if err := os.WriteFile(filepath.Join(opts.DistDir, "counter/index.html"), body, 0600); err != nil {
				t.Fatal(err)
			}
			served := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/html")
				w.Header().Set("Content-Security-Policy", "script-src 'nonce-fixture'")
				w.Write(body)
			}))
			t.Cleanup(served.Close)
			opts.BaseURL, opts.Client = served.URL, served.Client()
			measured, err := measureHTML(body, HTMLMeasureOptions{}, testBodyNormalizer)
			if err != nil {
				t.Fatal(err)
			}
			wantCount, wantInline := int64(0), int64(0)
			if tc.executes {
				wantCount, wantInline = 1, int64(len("app()"))
			}
			if measured.ExecutableScripts != wantCount || measured.InlineAppScriptMax != wantInline {
				t.Errorf("script measurement: count=%d inline=%d, want count=%d inline=%d", measured.ExecutableScripts, measured.InlineAppScriptMax, wantCount, wantInline)
			}
			for _, declared := range []string{"enhanced", "static"} {
				manifest.Routes[0].PageTypes = []string{declared}
				writeTestFixtureManifest(t, opts.DistDir, manifest)
				report, err := measureApp(context.Background(), opts, testBodyNormalizer)
				wantAdmission := tc.executes == (declared == "enhanced")
				if wantAdmission {
					if err != nil || len(report.Rows) != 1 {
						t.Errorf("%s declaration rejected: %v", declared, err)
					}
				} else {
					var input *InputError
					if !errors.As(err, &input) || input.Code != "capability" || input.Pointer != "/routes/pageTypes" {
						t.Errorf("%s declaration did not get the route capability error: %v", declared, err)
					}
				}
			}
		})
	}
	t.Logf("%d script forms, %d route admission checks", len(cases), len(cases)*2)
}
