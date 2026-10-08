package budget

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"m31labs.dev/gosx/buildmanifest"
	"m31labs.dev/gosx/internal/pagecaps"
)

func testRouteMeasurement(t *testing.T) (MeasureOptions, *FixtureManifest, []byte, []byte) {
	t.Helper()
	dir := t.TempDir()
	document := []byte("<!doctype html><html><head><title>Fixture</title></head><body><p>Stable content</p></body></html>")
	program := []byte("fixtureRuntime()")
	hash := testMeasureHash(program)
	programURL := "/gosx/assets/runtime/fixture." + hash[:16] + ".js"
	caps, err := pagecaps.FromHTML(document)
	if err != nil {
		t.Fatal(err)
	}
	manifest := &FixtureManifest{Schema: "gosx.perf-fixtures/v1", Version: 1, SourceSHA: strings.Repeat("a", 40), FixturesSHA256: strings.Repeat("1", 64), CatalogSHA256: strings.Repeat("2", 64),
		Routes: []FixtureRoute{{App: "fixture", RouteTemplate: "/counter/", SourcePath: "fixture/page.gsx", PageTypes: []string{"static"}, Capabilities: caps, CriticalAssetIDs: []string{"app/fixture/html"}, InputSequenceID: "counter-input"}},
		Assets: []buildmanifest.PerfAssetUse{{ID: "app/fixture/html", SHA256: testMeasureHash(document), URL: "/counter/", Owner: "app", Kind: "html", Phase: "critical", Condition: "always", Dependencies: []string{}},
			{ID: "framework/runtime/fixture.js", SHA256: hash, URL: programURL, Owner: "framework", Kind: "js", Phase: "dormant", Condition: "always", Dependencies: []string{}}}}
	for path, body := range map[string][]byte{"counter/index.html": document, "assets/runtime/" + filepath.Base(programURL): program} {
		full := filepath.Join(dir, path)
		if err := os.MkdirAll(filepath.Dir(full), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, body, 0600); err != nil {
			t.Fatal(err)
		}
	}
	writeTestFixtureManifest(t, dir, manifest)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/counter/" {
			w.Header().Set("Content-Type", "text/html")
			w.Write(document)
			return
		}
		if r.URL.Path == programURL {
			w.Header().Set("Content-Type", "text/javascript")
			w.Write(program)
			return
		}
		w.WriteHeader(404)
	}))
	t.Cleanup(server.Close)
	return MeasureOptions{App: "fixture", DistDir: dir, BaseURL: server.URL, Client: server.Client(), Public: publicTestReport(t).Info}, manifest, document, program
}
func writeTestFixtureManifest(t *testing.T, dir string, manifest *FixtureManifest) {
	t.Helper()
	data, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "perf-fixtures.v1.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
}

func TestMeasureAppReturnsConservativeReconciledReport(t *testing.T) {
	opts, _, document, program := testRouteMeasurement(t)
	report, err := measureApp(context.Background(), opts, testBodyNormalizer)
	if err != nil {
		t.Fatal(err)
	}
	docSizes, _ := testBodyNormalizer(document)
	programSizes, _ := testBodyNormalizer(program)
	if len(report.Rows) != 1 || len(report.Assets) != 2 || report.Coverage.RoutesMeasured != 1 || report.Coverage.AssetsMeasured != 2 || report.Coverage.Reachability != "unknown" {
		t.Fatal("coverage was certified without reachability proof")
	}
	row := report.Rows[0]
	if row.Status != "unavailable" || row.ModelStatus != "unknown" || row.NormalizedBytes != docSizes.Brotli+programSizes.Brotli || row.FrameworkBytes != programSizes.Brotli || row.AppBytes != docSizes.Brotli || row.AppBytes+row.FrameworkBytes != row.NormalizedBytes || row.PhaseBytes.Critical+row.PhaseBytes.Startup != row.NormalizedBytes || row.WireBytes != int64(len(document)+len(program)) || row.Requests != 2 {
		t.Fatal("inventory, phase, ownership or first-render wire sums differ")
	}
	if report.Assets[1].Phase != "startup" {
		t.Fatal("unproved dormant declaration excluded potential cost")
	}
	data, err := json.Marshal(Report{Schema: "gosx.budget-report/v1", Info: opts.Public, Mode: "report-only", Rows: report.Rows, Assets: report.Assets, ExceptionIDs: []string{}, Acknowledgments: []Ack{}, Violations: []CountReason{}, Coverage: report.Coverage})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := DecodeRecord(bytes.NewReader(data)); err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(data, []byte(opts.BaseURL)) || bytes.Contains(data, []byte(opts.DistDir)) {
		t.Fatal("private measurement option copied to report")
	}
}

func TestMeasureFixtureContractsAndProvenance(t *testing.T) {
	for _, name := range []string{"source", "fixtures", "unknown-field", "route-duplicate", "missing-critical", "type", "asset-hash", "sidecar", "route-not-registered", "route-duplicate-input", "app", "capability"} {
		t.Run(name, func(t *testing.T) {
			opts, manifest, _, _ := testRouteMeasurement(t)
			switch name {
			case "source":
				opts.Public.SHA = strings.Repeat("b", 40)
			case "fixtures":
				opts.Public.FixtureSHA256 = strings.Repeat("3", 64)
			case "route-duplicate":
				manifest.Routes = append(manifest.Routes, manifest.Routes[0])
			case "missing-critical":
				manifest.Routes[0].CriticalAssetIDs = []string{"app/fixture/missing"}
			case "type":
				manifest.Routes[0].PageTypes = []string{"static-webgpu"}
			case "asset-hash":
				manifest.Assets[1].SHA256 = strings.Repeat("3", 64)
			case "sidecar":
				if err := os.WriteFile(filepath.Join(opts.DistDir, "assets/runtime/"+filepath.Base(manifest.Assets[1].URL)+".br"), []byte("stale"), 0600); err != nil {
					t.Fatal(err)
				}
			case "route-not-registered":
				opts.Routes = []string{"/missing/"}
			case "route-duplicate-input":
				opts.Routes = []string{"/counter/", "/counter/"}
			case "app":
				opts.App = "private-app"
			case "capability":
				manifest.Routes[0].PageTypes = []string{"island"}
			}
			writeTestFixtureManifest(t, opts.DistDir, manifest)
			if name == "unknown-field" {
				data, _ := os.ReadFile(filepath.Join(opts.DistDir, "perf-fixtures.v1.json"))
				data = bytes.Replace(data, []byte(`"version":1`), []byte(`"version":1,"hostname":"example.invalid"`), 1)
				if err := os.WriteFile(filepath.Join(opts.DistDir, "perf-fixtures.v1.json"), data, 0600); err != nil {
					t.Fatal(err)
				}
			}
			_, err := measureApp(context.Background(), opts, testBodyNormalizer)
			var typed *InputError
			if !errors.As(err, &typed) || strings.Contains(err.Error(), "private") || strings.Contains(err.Error(), opts.DistDir) {
				t.Fatal("bad fixture accepted or error leaked", err)
			}
		})
	}
}

func TestMeasureFreshLiveHTMLNormalizesDeclaredNonces(t *testing.T) {
	opts, manifest, document, _ := testRouteMeasurement(t)
	build := bytes.Replace(document, []byte("</head>"), []byte(`<script nonce="build">fixtureApp()</script></head>`), 1)
	manifest.Assets = manifest.Assets[:1]
	manifest.Assets[0].SHA256 = testMeasureHash(build)
	manifest.Routes[0].PageTypes = []string{"enhanced"}
	if err := os.WriteFile(filepath.Join(opts.DistDir, "counter/index.html"), build, 0600); err != nil {
		t.Fatal(err)
	}
	writeTestFixtureManifest(t, opts.DistDir, manifest)
	for _, drift := range []bool{false, true} {
		count := 0
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			count++
			nonce := "first"
			if count > 1 {
				nonce = "second"
			}
			body := bytes.Replace(build, []byte(`nonce="build"`), []byte(`nonce="`+nonce+`"`), 1)
			if drift && count > 1 {
				body = bytes.Replace(body, []byte("Stable content"), []byte("Different content"), 1)
			}
			wire, err := encodeServingHTML(body, "br", "go-brotli-4")
			if err != nil {
				t.Error(err)
				return
			}
			w.Header().Set("Content-Type", "text/html")
			w.Header().Set("Content-Encoding", "br")
			w.Header().Set("Content-Security-Policy", "script-src 'nonce-"+nonce+"'")
			w.Write(wire)
		}))
		opts.BaseURL = server.URL
		opts.Client = server.Client()
		report, err := measureApp(context.Background(), opts, testBodyNormalizer)
		server.Close()
		if drift {
			if err == nil {
				t.Fatal("changed content normalized away")
			}
			continue
		}
		if err != nil || len(report.Rows) != 1 || report.Rows[0].PageType != "enhanced" {
			t.Fatal("declared nonce variation rejected", err)
		}
	}
}

func TestMeasureServingProfileAndPinAdmission(t *testing.T) {
	for _, encoding := range []string{"br", "gzip"} {
		compressor := "go-brotli-4"
		if encoding == "gzip" {
			compressor = "go-gzip-best"
		}
		wire, err := encodeServingHTML([]byte("fixture"), encoding, compressor)
		if err != nil {
			t.Fatal(err)
		}
		raw, err := decodeServedBody(wire, encoding)
		if err != nil || string(raw) != "fixture" {
			t.Fatal("serving profile failed round trip", err)
		}
	}
	if _, err := encodeServingHTML([]byte("fixture"), "br", "private-encoder"); err == nil {
		t.Fatal("unknown serving profile accepted")
	}
	opts, _, _, _ := testRouteMeasurement(t)
	if _, err := Measure(context.Background(), opts); err == nil {
		t.Fatal("production admitted unpinned normalization")
	}
}

func TestMeasurePhysicalAliasesAndRedirects(t *testing.T) {
	opts, manifest, document, program := testRouteMeasurement(t)
	// Two logical roles at one request identity consume one physical body.
	alias := manifest.Assets[1]
	alias.ID = "app/fixture/program"
	alias.Owner = "app"
	manifest.Assets = append(manifest.Assets, alias)
	writeTestFixtureManifest(t, opts.DistDir, manifest)
	result, err := measureApp(context.Background(), opts, testBodyNormalizer)
	if err != nil || len(result.Assets) != 2 || result.Rows[0].Requests != 2 {
		t.Fatal("physical alias counted twice", err)
	}
	// Distinct redirect requests still cost bytes, even with one final body.
	alias = manifest.Assets[1]
	alias.ID = "framework/runtime/alias.js"
	alias.URL = "/gosx/assets/runtime/alias.js"
	manifest.Assets = append(manifest.Assets[:2], alias)
	if err := os.WriteFile(filepath.Join(opts.DistDir, "assets/runtime/alias.js"), program, 0600); err != nil {
		t.Fatal(err)
	}
	writeTestFixtureManifest(t, opts.DistDir, manifest)
	redirect := []byte("redirect fixture")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/counter/" {
			w.Header().Set("Content-Type", "text/html")
			w.Write(document)
			return
		}
		if r.URL.Path == alias.URL {
			w.Header().Set("Location", manifest.Assets[1].URL)
			w.WriteHeader(302)
			w.Write(redirect)
			return
		}
		w.Header().Set("Content-Type", "text/javascript")
		w.Write(program)
	}))
	t.Cleanup(server.Close)
	opts.BaseURL = server.URL
	opts.Client = server.Client()
	result, err = measureApp(context.Background(), opts, testBodyNormalizer)
	if err != nil || result.Rows[0].Requests != 3 || result.Rows[0].WireBytes != int64(len(document)+len(program)+len(redirect)) {
		t.Fatal("redirect cost omitted or final body duplicated", err)
	}
	docSizes, _ := testBodyNormalizer(document)
	programSizes, _ := testBodyNormalizer(program)
	redirectSizes, _ := testBodyNormalizer(redirect)
	row := result.Rows[0]
	if row.NormalizedBytes != docSizes.Brotli+programSizes.Brotli+redirectSizes.Brotli || row.FrameworkBytes != programSizes.Brotli+redirectSizes.Brotli {
		t.Fatal("redirect ownership or canonical sums differ")
	}
}

func TestMeasureExpandsOnlyRequestedBackendAndKeepsCommonGoals(t *testing.T) {
	for _, requested := range []string{"webgpu", "webgl2", "none"} {
		t.Run(requested, func(t *testing.T) {
			opts, manifest, _, _ := testRouteMeasurement(t)
			manifest.Routes[0].PageTypes = []string{"static", "scene3d/js-webgpu", "scene3d/js-webgl2"}
			writeTestFixtureManifest(t, opts.DistDir, manifest)
			opts.Public.Backend = requested
			report, err := measureApp(context.Background(), opts, testBodyNormalizer)
			if err != nil {
				t.Fatal(err)
			}
			want := 2
			if requested == "none" {
				want = 3
			}
			if len(report.Rows) != want || report.Rows[0].PageType != "static" || report.Rows[0].Backend != "none" {
				t.Fatal("common goal or backend expansion differs", report.Rows)
			}
			for _, row := range report.Rows[1:] {
				if requested != "none" && row.Backend != requested {
					t.Fatal("another requested backend was reported", row)
				}
				if validateCellSchema(Cell{App: row.App, RouteTemplate: row.RouteTemplate, PageType: row.PageType, Scenario: row.Scenario, Backend: row.Backend, Metric: "fif", Unit: "ms"}) != nil {
					t.Fatal("expanded row is not a valid public cell", row)
				}
			}
		})
	}
}
