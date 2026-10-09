package budget

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"m31labs.dev/gosx/buildmanifest"
	"m31labs.dev/gosx/internal/pagecaps"
)

func TestCheckStaticDocumentClosureZeroJS(t *testing.T) {
	for _, tc := range []struct {
		name, child string
		reachable   bool
		scripts     int64
		executable  bool
	}{
		{"iframe-module", `<script type="module">window.fixtureReady=1</script>`, true, 1, true},
		{"iframe-event-handler", `<button onclick="window.fixtureReady=1">Run</button>`, true, 0, true},
		{"iframe-script-free", `<p>Child content</p>`, true, 0, false},
		{"dormant-module", `<script type="module">window.fixtureReady=1</script>`, false, 1, true},
		{"dormant-event-handler", `<button onclick="window.fixtureReady=1">Run</button>`, false, 0, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			gate := gateOptions(t)
			file, profile, coefficients := workedFile(t)
			page := file.PageTypes["static"]
			// Static zero-js is mandatory even without configured policies.
			page.RequiredPolicies = []string{}
			file.PageTypes = map[string]PageType{"static": page}
			file.Routes[0].PageTypes = []string{"static"}
			derived, err := Derive(file, profile, coefficients)
			if err != nil {
				t.Fatal(err)
			}
			gate.File, gate.Profile, gate.Coefficients = derived, profile, coefficients
			opts, manifest, _, _ := testRouteMeasurement(t)
			document := []byte(`<!doctype html><html><body><p>Root content</p></body></html>`)
			if tc.reachable {
				document = []byte(`<!doctype html><html><body><iframe src="/child/"></iframe></body></html>`)
			}
			child := []byte(`<!doctype html><html><body>` + tc.child + `</body></html>`)
			childObservation, err := measureHTML(child, HTMLMeasureOptions{}, testBodyNormalizer)
			if err != nil || childObservation.ExecutableScripts != tc.scripts {
				t.Fatal("child script observation differs", err)
			}
			caps, err := pagecaps.FromHTML(document)
			if err != nil {
				t.Fatal(err)
			}
			manifest.Routes[0].Capabilities = caps
			manifest.Assets[0].SHA256 = testMeasureHash(document)
			manifest.Assets = append(manifest.Assets, buildmanifest.PerfAssetUse{ID: "app/fixture/child", SHA256: testMeasureHash(child), URL: "/child/", Owner: "app", Kind: "html", Phase: "dormant", Condition: "always", Dependencies: []string{}})
			for name, body := range map[string][]byte{"counter/index.html": document, "child/index.html": child} {
				file := filepath.Join(opts.DistDir, name)
				if err := os.MkdirAll(filepath.Dir(file), 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(file, body, 0600); err != nil {
					t.Fatal(err)
				}
			}
			manifest.SourceSHA, manifest.CatalogSHA256 = gate.Head.Info.SHA, gate.File.Fixtures.SHA256
			writeTestFixtureManifest(t, opts.DistDir, manifest)
			opts.Public = gate.Head.Info
			opts.Public.ArtifactSHA256 = &manifest.FixturesSHA256
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/html")
				switch r.URL.Path {
				case "/counter/":
					w.Write(document)
				case "/child/":
					if !tc.reachable {
						t.Error("dormant document was fetched")
					}
					w.Write(child)
				default:
					t.Error("undeclared body was fetched")
					w.WriteHeader(http.StatusNotFound)
				}
			}))
			t.Cleanup(server.Close)
			opts.BaseURL, opts.Client = server.URL, server.Client()
			measured, err := measureApp(context.Background(), opts, testBodyNormalizer)
			if err != nil {
				t.Fatal(err)
			}
			requests := int64(1)
			if tc.reachable {
				requests++
			}
			if len(measured.Rows) != 1 || measured.Coverage.Reachability != "known" || measured.Rows[0].Requests != requests {
				t.Fatal("document closure was not measured", measured)
			}
			gate.Head.Info, gate.Head.Rows, gate.Head.Assets, gate.Head.Coverage = opts.Public, measured.Rows, measured.Assets, measured.Coverage
			gate.Base.Rows[0].PageType = "static"
			setGateBytes(gate.Base, 2048, 0)
			out, err := Check(gate)
			if err != nil {
				t.Fatal(err)
			}
			passed := !tc.reachable || !tc.executable
			if passed {
				if !out.Passed || len(out.Violations) != 0 || CheckExitCode(out, nil) != 0 {
					t.Fatal("script-free closure failed", out)
				}
			} else if out.Passed || CheckExitCode(out, nil) != 1 || out.Rows[0].ReasonCode != "policy" || !reflect.DeepEqual(out.Violations, []CountReason{{ReasonCode: "policy", Count: 1}}) {
				t.Fatal("executable child escaped static enforcement", out)
			}
			found := false
			for _, policy := range measured.Rows[0].Policies {
				if policy.Name == "zero-js" {
					found = true
					if policy.Passed != passed {
						t.Fatal("zero-js does not match the document closure", policy)
					}
				}
			}
			if !found {
				t.Fatal("zero-js observation missing")
			}
		})
	}
}
