package budget

import (
	"context"
	"fmt"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"m31labs.dev/gosx/buildmanifest"
	"m31labs.dev/gosx/internal/pagecaps"
)

var coveragePageTypes = []string{"static", "scene3d/js", "scene3d/js-webgpu", "scene3d/js-webgl2"}

func backendCoverageOptions(t *testing.T) CheckOptions {
	t.Helper()
	opts := gateOptions(t)
	file, profile, coefficients := workedFile(t)
	file.PageTypes = map[string]PageType{"static": file.PageTypes["static"], "scene3d/js": file.PageTypes["scene3d/js"]}
	for _, backend := range []string{"webgpu", "webgl2"} {
		page := file.PageTypes["scene3d/js"]
		page.Backend, page.CoefficientSet = backend, "scene-"+backend
		set := coefficients.Sets[0]
		set.ID, set.Backend = page.CoefficientSet, backend
		coefficients.Sets = append(coefficients.Sets, set)
		file.PageTypes["scene3d/js-"+backend] = page
	}
	for name, page := range file.PageTypes {
		page.RequiredPolicies = []string{}
		file.PageTypes[name] = page
	}
	file.Routes[0].PageTypes = []string{"scene3d/js-webgpu"}
	derived, err := Derive(file, profile, coefficients)
	if err != nil {
		t.Fatal(err)
	}
	opts.File, opts.Profile, opts.Coefficients = derived, profile, coefficients
	return opts
}

func TestCheckSeparateBackendRouteCoverage(t *testing.T) {
	for _, backend := range []string{"webgpu", "webgl2"} {
		for _, reportOnly := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/report-only=%t", backend, reportOnly), func(t *testing.T) {
				opts := backendCoverageOptions(t)
				opts.ReportOnly = reportOnly
				opts.File.Routes = []RouteRule{
					{App: "fixture", RouteTemplate: "/gpu/", PageTypes: []string{"scene3d/js-webgpu"}, Source: "detected", Scenario: "hard-cold"},
					{App: "fixture", RouteTemplate: "/gl/", PageTypes: []string{"scene3d/js-webgl2"}, Source: "detected", Scenario: "hard-cold"},
				}
				for _, report := range []*Report{opts.Head, opts.Base} {
					report.Info.Backend = backend
					report.Rows[0].PageType, report.Rows[0].Backend = "scene3d/js-"+backend, backend
					report.Rows[0].RouteTemplate = "/gpu/"
					if backend == "webgl2" {
						report.Rows[0].RouteTemplate = "/gl/"
					}
				}
				out, err := Check(opts)
				if err != nil {
					t.Fatal(err)
				}
				if !out.Passed || len(out.Rows) != 1 || out.Coverage.RoutesExpected != 1 || out.Coverage.RoutesMeasured != 1 || CheckExitCode(out, err) != 0 {
					t.Fatalf("eligible route rejected: coverage=%+v violations=%v", out.Coverage, out.Violations)
				}
				// Counting the incompatible route cannot claim eligible coverage.
				opts.Head.Coverage.RoutesExpected, opts.Head.Coverage.RoutesMeasured = 2, 2
				out, err = Check(opts)
				if err != nil {
					t.Fatal(err)
				}
				if out.Passed || !hasGateViolation(out, "capability") {
					t.Fatalf("ineligible coverage accepted: coverage=%+v violations=%v", out.Coverage, out.Violations)
				}
				opts.Head.Coverage.RoutesExpected, opts.Head.Coverage.RoutesMeasured = 1, 0
				opts.Head.Rows = []Row{}
				out, err = Check(opts)
				if err != nil {
					t.Fatal(err)
				}
				if out.Passed || !hasGateViolation(out, "capability") {
					t.Fatalf("missing eligible route accepted: coverage=%+v violations=%v", out.Coverage, out.Violations)
				}
			})
		}
	}
}

// Route masks form an independent oracle: bits 0/1 are common page types,
// bit 2 is WebGPU and bit 3 is WebGL2. Neither selection helper is used here.
func TestCheckAndMeasureGeneratedBackendRouteCoverage(t *testing.T) {
	routeSets := [][]int{{4, 8}, {12}, {1, 4, 8, 5, 10, 15}, {4}, {8}, {1, 2, 3}}
	rng := rand.New(rand.NewSource(55006))
	for i := 0; i < 64; i++ {
		masks := make([]int, 1+rng.Intn(8))
		for j := range masks {
			masks[j] = 1 + rng.Intn(15)
		}
		routeSets = append(routeSets, masks)
	}
	for i, masks := range routeSets {
		t.Run(fmt.Sprintf("set-%02d", i), func(t *testing.T) {
			gate := backendCoverageOptions(t)
			gate.File.Routes = []RouteRule{}
			for j, mask := range masks {
				rule := RouteRule{App: "fixture", RouteTemplate: fmt.Sprintf("/route-%d/", j), PageTypes: []string{}, Source: "detected", Scenario: "hard-cold"}
				for bit, name := range coveragePageTypes {
					if mask&(1<<bit) != 0 {
						rule.PageTypes = append(rule.PageTypes, name)
					}
				}
				gate.File.Routes = append(gate.File.Routes, rule)
			}
			opts := backendCoverageFixture(t, gate)
			for _, backend := range []string{"", "none", "webgpu", "webgl2"} {
				t.Run("backend="+backend, func(t *testing.T) {
					eligibleMask := 15
					if backend == "webgpu" {
						eligibleMask = 7
					} else if backend == "webgl2" {
						eligibleMask = 11
					}
					wantRoutes, wantRows := map[string]bool{}, map[string]bool{}
					for j, mask := range masks {
						rule := gate.File.Routes[j]
						if mask&eligibleMask != 0 {
							wantRoutes[rule.App+"|"+rule.RouteTemplate] = true
						}
						for bit, name := range coveragePageTypes {
							if mask&eligibleMask&(1<<bit) != 0 {
								row := Row{App: rule.App, RouteTemplate: rule.RouteTemplate, PageType: name, Scenario: rule.Scenario, Backend: gate.File.PageTypes[name].Backend}
								wantRows[growthRowKey(row)] = true
							}
						}
					}
					if got := expectedCheckRows(gate.File, backend); !reflect.DeepEqual(got, wantRows) {
						t.Fatalf("expected rows differ: masks=%v backend=%q got=%v want=%v", masks, backend, got, wantRows)
					}
					opts.Public.Backend = backend
					measured, err := measureApp(context.Background(), opts, testBodyNormalizer)
					if err != nil {
						t.Fatal(err)
					}
					gotRoutes, gotRows := map[string]bool{}, map[string]bool{}
					for _, row := range measured.Rows {
						gotRoutes[row.App+"|"+row.RouteTemplate] = true
						gotRows[growthRowKey(row)] = true
					}
					wantCount := int64(len(wantRoutes))
					if !reflect.DeepEqual(gotRoutes, wantRoutes) || !reflect.DeepEqual(gotRows, wantRows) || len(measured.Rows) != len(wantRows) || measured.Coverage.RoutesExpected != wantCount || measured.Coverage.RoutesMeasured != wantCount {
						t.Fatalf("measured coverage differs: masks=%v backend=%q coverage=%+v routes=%v want=%v", masks, backend, measured.Coverage, gotRoutes, wantRoutes)
					}
					gate.Head.Info, gate.Head.Rows, gate.Head.Assets, gate.Head.Coverage = opts.Public, measured.Rows, measured.Assets, measured.Coverage
					gate.Base.Info.Backend, gate.Base.Info.ArtifactSHA256 = backend, opts.Public.ArtifactSHA256
					gate.Base.Rows, gate.Base.Assets, gate.Base.Coverage = measured.Rows, measured.Assets, measured.Coverage
					out, err := Check(gate)
					if err != nil {
						t.Fatal(err)
					}
					// Other budget verdicts are independent of coverage. In particular,
					// a scene measured without a backend has unknown reachability.
					if hasGateViolation(out, "capability") || len(out.Rows) != len(wantRows) || out.Coverage.RoutesExpected != wantCount || out.Coverage.RoutesMeasured != wantCount {
						t.Fatalf("checker disagrees with measurement: masks=%v backend=%q coverage=%+v violations=%v", masks, backend, out.Coverage, out.Violations)
					}
				})
			}
		})
	}
	t.Logf("seed=55006 route sets=%d backend cases=%d", len(routeSets), len(routeSets)*4)
}

func backendCoverageFixture(t *testing.T, gate CheckOptions) MeasureOptions {
	t.Helper()
	dir := t.TempDir()
	manifest := &FixtureManifest{Schema: "gosx.perf-fixtures/v1", Version: 1, SourceSHA: gate.Head.Info.SHA, CatalogSHA256: gate.File.Fixtures.SHA256}
	bodies := map[string][]byte{}
	for i, rule := range gate.File.Routes {
		document := []byte("<!doctype html><html><body><p>Static content</p></body></html>")
		for _, name := range rule.PageTypes {
			if name != "static" {
				document = []byte(`<!doctype html><html><body><main data-gosx-scene3d>Scene</main></body></html>`)
				break
			}
		}
		caps, err := pagecaps.FromHTML(document)
		if err != nil {
			t.Fatal(err)
		}
		id := fmt.Sprintf("app/fixture/html-%d", i)
		manifest.Routes = append(manifest.Routes, FixtureRoute{App: rule.App, RouteTemplate: rule.RouteTemplate, SourcePath: "fixture/page.gsx", PageTypes: rule.PageTypes, Capabilities: caps, CriticalAssetIDs: []string{id}, InputSequenceID: "counter-input"})
		manifest.Assets = append(manifest.Assets, buildmanifest.PerfAssetUse{ID: id, SHA256: testMeasureHash(document), URL: rule.RouteTemplate, Owner: "app", Kind: "html", Phase: "critical", Condition: "always", Dependencies: []string{}})
		bodies[rule.RouteTemplate] = document
		file := filepath.Join(dir, rule.RouteTemplate, "index.html")
		if err := os.MkdirAll(filepath.Dir(file), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(file, document, 0600); err != nil {
			t.Fatal(err)
		}
	}
	writeTestFixtureManifest(t, dir, manifest)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if body, ok := bodies[r.URL.Path]; ok {
			w.Header().Set("Content-Type", "text/html")
			w.Write(body)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(server.Close)
	public := gate.Head.Info
	public.BaseArtifactSHA256 = manifest.FixturesSHA256
	public.ArtifactSHA256 = &manifest.FixturesSHA256
	return MeasureOptions{App: "fixture", DistDir: dir, BaseURL: server.URL, Client: server.Client(), Public: public}
}
