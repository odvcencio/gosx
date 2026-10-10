package budget

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"m31labs.dev/gosx/buildmanifest"
	"m31labs.dev/gosx/client/runtime/host"
	"m31labs.dev/gosx/internal/pagecaps"
	"m31labs.dev/gosx/server"
)

func TestMeasureNavigationUsesRevisionSpecificBodies(t *testing.T) {
	for _, cause := range []string{"base", "legacy-current", "staged-current", "tampered-current", "stale-current"} {
		t.Run(cause, func(t *testing.T) {
			dir := t.TempDir()
			body := []byte("/* synthetic earlier navigation revision */")
			assetURL := "/gosx/assets/runtime/navigation." + testMeasureHash(body) + ".js"
			if cause != "base" {
				body, assetURL = []byte(host.NavigationRuntime), host.NavigationRuntimePath
			}
			file := filepath.Join(dir, "assets/runtime", filepath.Base(assetURL))
			if cause != "legacy-current" {
				if err := os.MkdirAll(filepath.Dir(file), 0700); err != nil {
					t.Fatal(err)
				}
				staged := body
				if cause == "tampered-current" {
					staged = []byte("/* changed */")
				}
				if err := os.WriteFile(file, staged, 0600); err != nil {
					t.Fatal(err)
				}
				var encoded bytes.Buffer
				writer := gzip.NewWriter(&encoded)
				if cause == "stale-current" {
					writer.Write([]byte("/* stale */"))
				} else {
					writer.Write(body)
				}
				writer.Close()
				if err := os.WriteFile(file+".gz", encoded.Bytes(), 0600); err != nil {
					t.Fatal(err)
				}
			}
			root, err := os.OpenRoot(dir)
			if err != nil {
				t.Fatal(err)
			}
			defer root.Close()
			got, encodings, err := readFixtureBody(root, assetURL, "js")
			if cause == "tampered-current" || cause == "stale-current" {
				if err == nil {
					t.Fatal("staged navigation corruption was hidden by embedded bytes")
				}
			} else if err != nil || !bytes.Equal(got, body) || len(encodings["gzip"]) == 0 {
				t.Fatal("revision-specific navigation missing", err)
			}
		})
	}
}

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

func testMeasuredResourceGraph(t *testing.T, graph ReachabilityOptions, redirects map[string]string) (MeasureOptions, map[string]*atomic.Int64) {
	t.Helper()
	opts, _, _, _ := testRouteMeasurement(t)
	route := graph.Route
	route.App, route.SourcePath, route.InputSequenceID = "fixture", "fixture/page.gsx", "counter-input"
	var err error
	route.Capabilities, err = pagecaps.FromHTML(graph.Bodies["app/fixture/html"])
	if err != nil {
		t.Fatal(err)
	}
	route.PageTypes, err = pagecaps.Classify(route.Capabilities, false)
	if err != nil {
		t.Fatal(err)
	}
	manifest := &FixtureManifest{Schema: "gosx.perf-fixtures/v1", Version: 1, SourceSHA: opts.Public.SHA, CatalogSHA256: opts.Public.FixtureSHA256, Routes: []FixtureRoute{route}, Assets: graph.Graph.Assets}
	byURL := map[string]buildmanifest.PerfAssetUse{}
	requests := map[string]*atomic.Int64{}
	for _, asset := range manifest.Assets {
		file := strings.TrimPrefix(asset.URL, "/")
		if asset.Kind == "html" {
			file = strings.Trim(file, "/") + "/index.html"
		}
		file = filepath.Join(opts.DistDir, file)
		if err := os.MkdirAll(filepath.Dir(file), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(file, graph.Bodies[asset.ID], 0600); err != nil {
			t.Fatal(err)
		}
		byURL[asset.URL], requests[asset.URL] = asset, &atomic.Int64{}
	}
	writeTestFixtureManifest(t, opts.DistDir, manifest)
	opts.Public.ArtifactSHA256 = &manifest.FixturesSHA256
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		asset, ok := byURL[r.URL.Path]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		requests[r.URL.Path].Add(1)
		if target, ok := redirects[r.URL.Path]; ok {
			w.Header().Set("Location", target)
			w.WriteHeader(http.StatusFound)
			w.Write([]byte("redirect fixture"))
			return
		}
		media := map[string]string{"html": "text/html", "js": "text/javascript", "css": "text/css", "image": "image/png"}
		w.Header().Set("Content-Type", media[asset.Kind])
		w.Write(graph.Bodies[asset.ID])
	}))
	t.Cleanup(server.Close)
	opts.BaseURL, opts.Client = server.URL, server.Client()
	return opts, requests
}

func TestMeasureDeferredHTMLKeepsResourcesAfterReady(t *testing.T) {
	bodies := map[string][]byte{
		"app/fixture/html":   []byte(`<p>Fixture document</p>`),
		"app/fixture/script": []byte(`fetch("/counter/later/")`),
		"app/fixture/later":  []byte(`<img src="./image.png">`),
		"app/fixture/image":  []byte("fixture image"),
	}
	assets := []buildmanifest.PerfAssetUse{
		graphAsset("app/fixture/html", "/counter/", "html", "critical", "always", bodies["app/fixture/html"]),
		graphAsset("app/fixture/script", "/entry.js", "js", "after-ready", "interaction", bodies["app/fixture/script"], "app/fixture/later"),
		graphAsset("app/fixture/later", "/counter/later/", "html", "dormant", "always", bodies["app/fixture/later"]),
		graphAsset("app/fixture/image", "/counter/later/image.png", "image", "dormant", "always", bodies["app/fixture/image"]),
	}
	graph := ReachabilityOptions{Graph: &buildmanifest.PerfAssetUses{Version: 1, Assets: assets}, Bodies: bodies, Route: FixtureRoute{RouteTemplate: "/counter/", CriticalAssetIDs: []string{"app/fixture/html"}}}
	opts, _ := testMeasuredResourceGraph(t, graph, nil)
	report, err := measureApp(context.Background(), opts, testBodyNormalizer)
	if err != nil {
		t.Fatal(err)
	}
	doc, _ := testBodyNormalizer(bodies["app/fixture/html"])
	var afterReady int64
	for _, id := range []string{"app/fixture/script", "app/fixture/later", "app/fixture/image"} {
		sizes, _ := testBodyNormalizer(bodies[id])
		afterReady += sizes.Brotli
	}
	row := report.Rows[0]
	if row.NormalizedBytes != doc.Brotli || row.PhaseBytes.Startup != 0 || row.PhaseBytes.AfterReady != afterReady || row.WireBytes != int64(len(bodies["app/fixture/html"])) || row.Requests != 1 {
		t.Fatal("deferred HTML promoted a descendant into cold totals", row)
	}
}

func TestMeasureDormantRedirectTargetRetainsPhysicalOwnership(t *testing.T) {
	for _, frameworkFirst := range []bool{false, true} {
		t.Run(map[bool]string{false: "app-first", true: "framework-first"}[frameworkFirst], func(t *testing.T) {
			bodies := map[string][]byte{
				"app/fixture/html":         []byte(`<p>Fixture document</p>`),
				"app/fixture/alias":        []byte(`const framework = 1`),
				"framework/runtime/target": []byte(`const framework = 1`),
				"framework/runtime/other":  []byte(`const framework = 1`),
			}
			assets := []buildmanifest.PerfAssetUse{
				graphAsset("app/fixture/html", "/counter/", "html", "critical", "always", bodies["app/fixture/html"]),
				graphAsset("app/fixture/alias", "/alias.js", "js", "startup", "always", bodies["app/fixture/alias"]),
				graphAsset("framework/runtime/target", "/target.js", "js", "dormant", "always", bodies["framework/runtime/target"]),
				graphAsset("framework/runtime/other", "/other.js", "js", "dormant", "always", bodies["framework/runtime/other"]),
			}
			if frameworkFirst {
				assets[1], assets[2] = assets[2], assets[1]
			}
			graph := ReachabilityOptions{Graph: &buildmanifest.PerfAssetUses{Version: 1, Assets: assets}, Bodies: bodies, Route: FixtureRoute{RouteTemplate: "/counter/", CriticalAssetIDs: []string{"app/fixture/html"}}}
			opts, requests := testMeasuredResourceGraph(t, graph, map[string]string{"/alias.js": "/target.js"})
			report, err := measureApp(context.Background(), opts, testBodyNormalizer)
			if err != nil {
				t.Fatal(err)
			}
			doc, _ := testBodyNormalizer(bodies["app/fixture/html"])
			program, _ := testBodyNormalizer(bodies["app/fixture/alias"])
			redirect, _ := testBodyNormalizer([]byte("redirect fixture"))
			row := report.Rows[0]
			if row.FrameworkBytes != program.Brotli || row.AppBytes != doc.Brotli+redirect.Brotli || row.NormalizedBytes != row.FrameworkBytes+row.AppBytes || row.PhaseBytes.Startup != program.Brotli+redirect.Brotli || row.PhaseBytes.Dormant != program.Brotli || row.Requests != 3 || row.WireBytes != int64(len(bodies["app/fixture/html"])+len(bodies["app/fixture/alias"])+len("redirect fixture")) || requests["/target.js"].Load() != 1 || requests["/other.js"].Load() != 0 {
				t.Fatal("redirect target duplicated, lost ownership or changed transfer costs", row)
			}
			for _, asset := range report.Assets {
				if asset.ID == "framework/runtime/target" && asset.Phase != "startup" {
					t.Fatal("reached framework inventory still reported dormant", asset)
				}
			}
		})
	}
}

func TestMeasureAlternateDeclaredURLKeepsIndependentPhase(t *testing.T) {
	graph := testAlternateURLGraph()
	opts, requests := testMeasuredResourceGraph(t, graph, nil)
	report, err := measureApp(context.Background(), opts, testBodyNormalizer)
	if err != nil {
		t.Fatal("declared alternate URL rejected:", err)
	}
	doc, _ := testBodyNormalizer(graph.Bodies["app/fixture/html"])
	css, _ := testBodyNormalizer(graph.Bodies["app/fixture/css"])
	child, _ := testBodyNormalizer(graph.Bodies["app/fixture/child"])
	row := report.Rows[0]
	if report.Coverage.Reachability != "known" || row.NormalizedBytes != doc.Brotli+css.Brotli+child.Brotli || row.PhaseBytes.Dormant != css.Brotli || row.Requests != 3 || requests["/original/site.css"].Load() != 0 || requests["/alternate/site.css"].Load() != 1 || requests["/alternate/child.css"].Load() != 1 {
		t.Fatal("alternate URL used another declaration's phase or reference base", row)
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

func TestMeasureFreshGzipHTMLWithoutSidecar(t *testing.T) {
	opts, manifest, document, _ := testRouteMeasurement(t)
	var content strings.Builder
	for i := 0; i < 256; i++ {
		content.WriteString("<p>Fixture content " + strconv.Itoa(i) + ": " + strings.Repeat("stable ", i%13+1) + "</p>")
	}
	build := bytes.Replace(document, []byte("</body>"), []byte(content.String()+`<script nonce="build">fixtureApp()</script></body>`), 1)
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
	count := 0
	var firstBody []byte
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count++
		nonce := "render-" + strconv.Itoa(count)
		body := bytes.Replace(build, []byte(`nonce="build"`), []byte(`nonce="`+nonce+`"`), 1)
		if count == 1 {
			firstBody = bytes.Clone(body)
		}
		w.Header().Set("Content-Type", "text/html")
		w.Header().Set("Content-Security-Policy", "script-src 'nonce-"+nonce+"'")
		w.Write(body)
	})
	httpServer := httptest.NewServer(server.GzipMiddleware()(handler))
	t.Cleanup(httpServer.Close)
	opts.BaseURL, opts.Client = httpServer.URL, httpServer.Client()
	report, err := measureApp(context.Background(), opts, testBodyNormalizer)
	httpServer.Close()
	if err != nil {
		t.Fatal("fresh production gzip HTML rejected:", err)
	}
	compress := func(level int) []byte {
		var out bytes.Buffer
		writer, err := gzip.NewWriterLevel(&out, level)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := writer.Write(firstBody); err != nil {
			t.Fatal(err)
		}
		if err := writer.Close(); err != nil {
			t.Fatal(err)
		}
		return out.Bytes()
	}
	wire := compress(gzip.DefaultCompression)
	if bytes.Equal(wire, compress(gzip.BestCompression)) {
		t.Fatal("gzip fixture does not distinguish production and normalization levels")
	}
	if count != 2 || len(report.Rows) != 1 || report.Rows[0].PageType != "enhanced" || report.Rows[0].WireBytes != int64(len(wire)) || !testHTTPPolicy(HTTPMeasurement{Policies: report.Rows[0].Policies}, "html-compressed") {
		t.Fatal("fresh gzip renders or wire accounting differ")
	}
}

func TestMeasureRedirectOwnershipIsDeclarationOrderIndependent(t *testing.T) {
	opts, manifest, document, program := testRouteMeasurement(t)
	doc, framework := manifest.Assets[0], manifest.Assets[1]
	alias := framework
	alias.ID, alias.Owner, alias.URL = "app/fixture/alias.js", "app", "/gosx/assets/runtime/alias.js"
	alias.Phase = "startup"
	if err := os.WriteFile(filepath.Join(opts.DistDir, "assets/runtime/alias.js"), program, 0600); err != nil {
		t.Fatal(err)
	}
	redirect := []byte("app redirect fixture")
	httpServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case doc.URL:
			w.Header().Set("Content-Type", "text/html")
			w.Write(document)
		case alias.URL:
			w.Header().Set("Location", framework.URL)
			w.WriteHeader(http.StatusFound)
			w.Write(redirect)
		case framework.URL:
			w.Header().Set("Content-Type", "text/javascript")
			w.Write(program)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(httpServer.Close)
	opts.BaseURL, opts.Client = httpServer.URL, httpServer.Client()
	docSizes, _ := testBodyNormalizer(document)
	programSizes, _ := testBodyNormalizer(program)
	redirectSizes, _ := testBodyNormalizer(redirect)
	var reports []AppReport
	for _, appFirst := range []bool{true, false} {
		manifest.Assets = []buildmanifest.PerfAssetUse{doc, framework, alias}
		if appFirst {
			manifest.Assets[1], manifest.Assets[2] = alias, framework
		}
		writeTestFixtureManifest(t, opts.DistDir, manifest)
		report, err := measureApp(context.Background(), opts, testBodyNormalizer)
		if err != nil {
			t.Fatal(err)
		}
		if len(report.Rows) != 1 {
			t.Fatal("route report missing")
		}
		row := report.Rows[0]
		if row.FrameworkBytes != programSizes.Brotli || row.AppBytes != docSizes.Brotli+redirectSizes.Brotli || row.NormalizedBytes != row.FrameworkBytes+row.AppBytes || row.PhaseBytes.Critical != docSizes.Brotli || row.PhaseBytes.Startup != programSizes.Brotli+redirectSizes.Brotli || row.Requests != 3 || row.WireBytes != int64(len(document)+len(program)+len(redirect)) {
			t.Errorf("app-first=%v: resolved-body ownership or redirect accounting differs: %+v", appFirst, row)
		}
		reports = append(reports, report)
	}
	if !reflect.DeepEqual(reports[0], reports[1]) {
		t.Fatal("changing declaration order changed the report")
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
	graph.Route.App, graph.Route.SourcePath = "fixture", "fixture/page.gsx"
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
	opts.Public.ArtifactSHA256 = &manifest.FixturesSHA256
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

func TestMeasureUnscannedSrcdocRetainsDeclaredRuntime(t *testing.T) {
	for _, tc := range []struct {
		name, sandbox string
	}{
		{"unsandboxed", ""},
		{"scripts-allowed", ` sandbox="allow-scripts"`},
		{"scripts-token-list", " sandbox=\"allow-forms\tALLOW-SCRIPTS\nallow-same-origin\""},
		{"sandbox-present", ` sandbox`},
		{"sandbox-empty", ` sandbox=""`},
		{"sandbox-other-tokens", ` sandbox="allow-same-origin allow-forms"`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			document := []byte(`<iframe srcdoc="&lt;script src='/runtime.js'&gt;&lt;/script&gt;"` + tc.sandbox + `></iframe>`)
			runtime := []byte(`const fixtureRuntime = true;`)
			graph := ReachabilityOptions{
				Graph: &buildmanifest.PerfAssetUses{Version: 1, Assets: []buildmanifest.PerfAssetUse{
					graphAsset("app/fixture/html", "/counter/", "html", "critical", "always", document),
					graphAsset("framework/runtime/fixture", "/runtime.js", "js", "dormant", "always", runtime),
				}},
				Bodies: map[string][]byte{"app/fixture/html": document, "framework/runtime/fixture": runtime},
				Route:  FixtureRoute{RouteTemplate: "/counter/", CriticalAssetIDs: []string{"app/fixture/html"}},
			}
			opts, requests := testMeasuredResourceGraph(t, graph, nil)
			report, err := measureApp(context.Background(), opts, testBodyNormalizer)
			if err != nil {
				t.Fatal(err)
			}
			docSizes, _ := testBodyNormalizer(document)
			runtimeSizes, _ := testBodyNormalizer(runtime)
			wantReachability, wantPhase := "unknown", "startup"
			wantStartup, wantDormant, wantFramework, wantFetches := runtimeSizes.Brotli, int64(0), runtimeSizes.Brotli, int64(1)
			if len(report.Rows) != 1 || len(report.Assets) != 2 {
				t.Fatal("route or runtime inventory missing", report)
			}
			row := report.Rows[0]
			if report.Coverage.Reachability != wantReachability || row.PhaseBytes.Critical != docSizes.Brotli || row.PhaseBytes.Startup != wantStartup || row.PhaseBytes.Dormant != wantDormant || row.NormalizedBytes != docSizes.Brotli+wantStartup || row.AppBytes != docSizes.Brotli || row.FrameworkBytes != wantFramework || row.Requests != 1+wantFetches || row.WireBytes != int64(len(document))+wantFetches*int64(len(runtime)) || requests["/runtime.js"].Load() != wantFetches {
				t.Fatalf("srcdoc runtime accounting differs: coverage=%s row=%+v fetches=%d", report.Coverage.Reachability, row, requests["/runtime.js"].Load())
			}
			if report.Assets[1].Phase != wantPhase {
				t.Fatalf("runtime phase=%s want %s", report.Assets[1].Phase, wantPhase)
			}
			if row.ReasonCode != "unknown-reachability" {
				t.Fatal("uncertain closure was not reported", row.ReasonCode)
			}
		})
	}
}

func TestMeasureSandboxedSrcdocRetainsDeclaredResources(t *testing.T) {
	for _, resource := range []struct {
		name, id, url, kind, srcdoc string
		body                        []byte
	}{
		{"image", "app/fixture/pixel", "/pixel.png", "image", `&lt;img src='/pixel.png'&gt;`, []byte("fixture pixel image!!!")},
		{"stylesheet", "app/fixture/style", "/style.css", "css", `&lt;link rel='stylesheet' href='/style.css'&gt;`, []byte(".fixture{color:blue}")},
	} {
		for _, sandbox := range []string{` sandbox`, ` sandbox=""`, ` sandbox="allow-forms allow-same-origin"`} {
			t.Run(resource.name+sandbox, func(t *testing.T) {
				document := []byte(`<iframe` + sandbox + ` srcdoc="` + resource.srcdoc + `"></iframe>`)
				graph := ReachabilityOptions{
					Graph: &buildmanifest.PerfAssetUses{Version: 1, Assets: []buildmanifest.PerfAssetUse{
						graphAsset("app/fixture/html", "/counter/", "html", "critical", "always", document),
						graphAsset(resource.id, resource.url, resource.kind, "dormant", "always", resource.body),
					}},
					Bodies: map[string][]byte{"app/fixture/html": document, resource.id: resource.body},
					Route:  FixtureRoute{RouteTemplate: "/counter/", CriticalAssetIDs: []string{"app/fixture/html"}},
				}
				opts, requests := testMeasuredResourceGraph(t, graph, nil)
				report, err := measureApp(context.Background(), opts, testBodyNormalizer)
				if err != nil {
					t.Fatal(err)
				}
				docSizes, _ := testBodyNormalizer(document)
				resourceSizes, _ := testBodyNormalizer(resource.body)
				if len(report.Rows) != 1 || len(report.Assets) != 2 {
					t.Fatal("route or resource inventory missing", report)
				}
				row := report.Rows[0]
				if report.Coverage.Reachability != "unknown" || row.ReasonCode != "unknown-reachability" || row.PhaseBytes.Critical != docSizes.Brotli || row.PhaseBytes.Startup != resourceSizes.Brotli || row.PhaseBytes.Dormant != 0 || row.NormalizedBytes != docSizes.Brotli+resourceSizes.Brotli || row.AppBytes != row.NormalizedBytes || row.FrameworkBytes != 0 || row.Requests != 2 || row.WireBytes != int64(len(document)+len(resource.body)) || requests[resource.url].Load() != 1 || report.Assets[1].Phase != "startup" {
					t.Fatalf("sandboxed srcdoc lost declarative load: coverage=%s row=%+v asset=%+v fetches=%d", report.Coverage.Reachability, row, report.Assets[1], requests[resource.url].Load())
				}
			})
		}
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
