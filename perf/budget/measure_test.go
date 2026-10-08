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
	"sync/atomic"
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
	public := publicTestReport(t).Info
	public.FixtureSHA256, public.ArtifactSHA256 = manifest.CatalogSHA256, &manifest.FixturesSHA256
	return MeasureOptions{App: "fixture", DistDir: dir, BaseURL: server.URL, Client: server.Client(), Public: public}, manifest, document, program
}
func writeTestFixtureManifest(t *testing.T, dir string, manifest *FixtureManifest) {
	t.Helper()
	digest, digestErr := FixtureManifestSHA256(*manifest)
	if digestErr == nil {
		manifest.FixturesSHA256 = digest
	}
	data, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "perf-fixtures.v1.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
}

func TestMeasureAppReturnsReconciledDeclaredClosure(t *testing.T) {
	opts, _, document, program := testRouteMeasurement(t)
	report, err := measureApp(context.Background(), opts, testBodyNormalizer)
	if err != nil {
		t.Fatal(err)
	}
	docSizes, _ := testBodyNormalizer(document)
	programSizes, _ := testBodyNormalizer(program)
	if len(report.Rows) != 1 || len(report.Assets) != 2 || report.Coverage.RoutesMeasured != 1 || report.Coverage.AssetsMeasured != 2 || report.Coverage.Reachability != "known" {
		t.Fatal("coverage did not reflect declared closure")
	}
	row := report.Rows[0]
	if row.Status != "unavailable" || row.ModelStatus != "unknown" || row.NormalizedBytes != docSizes.Brotli || row.FrameworkBytes != 0 || row.AppBytes != docSizes.Brotli || row.AppBytes+row.FrameworkBytes != row.NormalizedBytes || row.PhaseBytes.Critical+row.PhaseBytes.Startup != row.NormalizedBytes || row.WireBytes != int64(len(document)) || row.Requests != 1 || row.PhaseBytes.Dormant != programSizes.Brotli {
		t.Fatal("inventory, phase, ownership or first-render wire sums differ")
	}
	if report.Assets[1].Phase != "dormant" {
		t.Fatal("proved dormant inventory was charged at startup")
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
	for _, name := range []string{"source", "fixtures", "unknown-field", "route-duplicate", "missing-critical", "type", "asset-hash", "sidecar", "route-not-registered", "route-duplicate-input", "app", "capability", "capability-fields"} {
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
			case "capability-fields":
				manifest.Routes[0].Capabilities.Scene3D = true
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
	manifest.Routes[0].Capabilities, _ = pagecaps.FromHTML(build)
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
	manifest.Assets[1].Phase = "startup"
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

func TestMeasureDeclaredPhasesKeepAfterReadyAndDormantOutOfCold(t *testing.T) {
	for _, phase := range []string{"startup", "after-ready", "dormant"} {
		t.Run(phase, func(t *testing.T) {
			opts, manifest, document, program := testRouteMeasurement(t)
			manifest.Assets[1].Phase = phase
			writeTestFixtureManifest(t, opts.DistDir, manifest)
			var docRequests, assetRequests atomic.Int64
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/counter/" {
					docRequests.Add(1)
					w.Header().Set("Content-Type", "text/html")
					w.Write(document)
					return
				}
				assetRequests.Add(1)
				w.Header().Set("Content-Type", "text/javascript")
				w.Write(program)
			}))
			t.Cleanup(server.Close)
			opts.BaseURL, opts.Client = server.URL, server.Client()
			report, err := measureApp(context.Background(), opts, testBodyNormalizer)
			if err != nil {
				t.Fatal(err)
			}
			row := report.Rows[0]
			docSizes, _ := testBodyNormalizer(document)
			programSizes, _ := testBodyNormalizer(program)
			if report.Coverage.Reachability != "known" || docRequests.Load() != 2 || row.PhaseBytes.Critical != docSizes.Brotli || row.FrameworkBytes+row.AppBytes != row.NormalizedBytes {
				t.Fatal("provenance or document/owner totals differ", row)
			}
			if phase == "startup" {
				if row.NormalizedBytes != docSizes.Brotli+programSizes.Brotli || row.Requests != 2 || row.WireBytes != int64(len(document)+len(program)) || row.PhaseBytes.Startup != programSizes.Brotli || row.FrameworkBytes != programSizes.Brotli {
					t.Fatal("startup body discounted", row)
				}
			} else if row.NormalizedBytes != docSizes.Brotli || row.Requests != 1 || row.FrameworkBytes != 0 || row.WireBytes != int64(len(document)) {
				t.Fatal("session inventory entered cold totals", row)
			}
			if phase == "after-ready" && (assetRequests.Load() != 1 || row.PhaseBytes.AfterReady != programSizes.Brotli) {
				t.Fatal("advertised later cost lost", row)
			}
			if phase == "dormant" && (assetRequests.Load() != 0 || row.PhaseBytes.Dormant != programSizes.Brotli) {
				t.Fatal("unused inventory fetched or lost", row)
			}
		})
	}
}

func TestMeasureServedCSSFontModuleClosureAndCriticalContent(t *testing.T) {
	graph := testResourceGraph()
	opts, _, _, _ := testRouteMeasurement(t)
	document := graph.Bodies["app/fixture/html"]
	caps, err := pagecaps.FromHTML(document)
	if err != nil {
		t.Fatal(err)
	}
	graph.Route.App = "fixture"
	graph.Route.SourcePath = "fixture/page.gsx"
	graph.Route.PageTypes = []string{"island"}
	graph.Route.Capabilities = caps
	graph.Route.InputSequenceID = "counter-input"
	manifest := &FixtureManifest{Schema: "gosx.perf-fixtures/v1", Version: 1, SourceSHA: opts.Public.SHA, FixturesSHA256: opts.Public.FixtureSHA256, CatalogSHA256: strings.Repeat("2", 64), Routes: []FixtureRoute{graph.Route}, Assets: graph.Graph.Assets}
	byURL := map[string]buildmanifest.PerfAssetUse{}
	for _, asset := range manifest.Assets {
		file := strings.TrimPrefix(asset.URL, "/")
		if asset.Kind == "html" {
			file = "counter/index.html"
		}
		file = filepath.Join(opts.DistDir, file)
		if err := os.MkdirAll(filepath.Dir(file), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(file, graph.Bodies[asset.ID], 0600); err != nil {
			t.Fatal(err)
		}
		byURL[asset.URL] = asset
	}
	writeTestFixtureManifest(t, opts.DistDir, manifest)
	var fetchedFull atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		asset, ok := byURL[r.URL.Path]
		if !ok {
			w.WriteHeader(404)
			return
		}
		if asset.ID == "framework/runtime/full" {
			fetchedFull.Store(true)
		}
		switch asset.Kind {
		case "html":
			w.Header().Set("Content-Type", "text/html")
		case "css":
			w.Header().Set("Content-Type", "text/css")
		case "js":
			w.Header().Set("Content-Type", "text/javascript")
		case "wasm":
			w.Header().Set("Content-Type", "application/wasm")
		case "font":
			w.Header().Set("Content-Type", "font/woff2")
		case "image":
			w.Header().Set("Content-Type", "image/png")
		default:
			w.Header().Set("Content-Type", "application/octet-stream")
		}
		w.Write(graph.Bodies[asset.ID])
	}))
	t.Cleanup(server.Close)
	opts.BaseURL, opts.Client = server.URL, server.Client()
	report, err := measureApp(context.Background(), opts, testBodyNormalizer)
	if err != nil {
		t.Fatal(err)
	}
	row := report.Rows[0]
	var total, framework, critical, dormant, wireBytes int64
	for _, asset := range manifest.Assets {
		sizes, _ := testBodyNormalizer(graph.Bodies[asset.ID])
		if asset.ID == "framework/runtime/full" {
			dormant += sizes.Brotli
			continue
		}
		total += sizes.Brotli
		wireBytes += int64(len(graph.Bodies[asset.ID]))
		if asset.Owner == "framework" {
			framework += sizes.Brotli
		}
		if asset.ID == "app/fixture/html" || asset.ID == "app/fixture/font" || asset.ID == "app/fixture/hero" {
			critical += sizes.Brotli
		}
	}
	if report.Coverage.Reachability != "known" || fetchedFull.Load() || row.NormalizedBytes != total || row.FrameworkBytes != framework || row.WireBytes != wireBytes || row.Requests != 9 || row.PhaseBytes.Critical != critical || row.PhaseBytes.Startup != total-critical || row.PhaseBytes.Dormant != dormant {
		t.Fatal("served dependency closure, critical content or owner/phase sums differ", row)
	}
}

func TestMeasureUnknownCriticalityRetainsPotentialBodies(t *testing.T) {
	opts, manifest, document, _ := testRouteMeasurement(t)
	document = bytes.Replace(document, []byte("</body>"), []byte(`<img srcset="/a.png 1x,/b.png 2x"></body>`), 1)
	manifest.Assets[0].SHA256 = testMeasureHash(document)
	if err := os.WriteFile(filepath.Join(opts.DistDir, "counter/index.html"), document, 0600); err != nil {
		t.Fatal(err)
	}
	writeTestFixtureManifest(t, opts.DistDir, manifest)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/counter/" {
			w.Header().Set("Content-Type", "text/html")
			w.Write(document)
			return
		}
		w.Header().Set("Content-Type", "text/javascript")
		w.Write([]byte("fixtureRuntime()"))
	}))
	t.Cleanup(server.Close)
	opts.BaseURL, opts.Client = server.URL, server.Client()
	report, err := measureApp(context.Background(), opts, testBodyNormalizer)
	if err != nil {
		t.Fatal(err)
	}
	if report.Coverage.Reachability != "unknown" || report.Rows[0].ReasonCode != "unknown-reachability" || report.Rows[0].FrameworkBytes == 0 || report.Rows[0].PhaseBytes.Dormant != 0 {
		t.Fatal("unresolved criticality excluded potential framework cost", report)
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

func TestMeasurePolicyObservationsReachGate(t *testing.T) {
	opts, _, _, _ := testRouteMeasurement(t)
	report, err := measureApp(context.Background(), opts, testBodyNormalizer)
	if err != nil {
		t.Fatal(err)
	}
	policies := map[string]bool{}
	for _, policy := range report.Rows[0].Policies {
		policies[policy.Name] = policy.Passed
	}
	for _, name := range []string{inlineExecutablePolicy, "no-sync-script", "runtime-hashed", "declared-fetches", "canonical-build", "zero-js"} {
		if !policies[name] {
			t.Fatal("verified policy missing from measurement", name)
		}
	}
}
