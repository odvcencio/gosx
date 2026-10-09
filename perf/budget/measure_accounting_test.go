package budget

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"m31labs.dev/gosx/buildmanifest"
	"m31labs.dev/gosx/internal/assetmeasure"
	"m31labs.dev/gosx/internal/pagecaps"
)

func TestMeasureCriticalCSSPhase(t *testing.T) {
	opts, manifest, document, _ := testRouteMeasurement(t)
	css := []byte("body{color:red}")
	url := "/gosx/assets/critical.css"
	document = bytes.Replace(document, []byte("</head>"), []byte(`<link rel="stylesheet" href="`+url+`"></head>`), 1)
	manifest.Assets[0].SHA256 = testMeasureHash(document)
	manifest.Assets[1] = buildmanifest.PerfAssetUse{ID: "app/fixture/critical.css", SHA256: testMeasureHash(css), URL: url,
		Owner: "app", Kind: "css", Phase: "critical", Condition: "always", Dependencies: []string{}}
	manifest.Routes[0].CriticalAssetIDs = append(manifest.Routes[0].CriticalAssetIDs, manifest.Assets[1].ID)
	if err := os.WriteFile(filepath.Join(opts.DistDir, "counter/index.html"), document, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(opts.DistDir, "assets/critical.css"), css, 0600); err != nil {
		t.Fatal(err)
	}
	writeTestFixtureManifest(t, opts.DistDir, manifest)
	responses := map[string]referenceHTTPBody{
		"/counter/": {raw: document, wire: document, kind: "html"}, url: {raw: css, wire: css, kind: "css"},
	}
	opts.Client = referenceAccountingClient(t, responses)
	report, err := measureApp(context.Background(), opts, testBodyNormalizer)
	if err != nil {
		t.Fatal(err)
	}
	docSize, _ := testBodyNormalizer(document)
	cssSize, _ := testBodyNormalizer(css)
	if row := report.Rows[0]; row.PhaseBytes.Critical != docSize.Brotli+cssSize.Brotli || row.PhaseBytes.Startup != 0 {
		t.Fatalf("critical CSS misplaced: %+v", row.PhaseBytes)
	}
	for _, asset := range report.Assets {
		if asset.Phase != "critical" {
			t.Fatalf("critical asset reported as %s", asset.Phase)
		}
	}
}

type referenceHTTPBody struct {
	raw, wire                []byte
	kind, encoding, location string
}

type referencePhaseTotal struct {
	bytes, wire, requests, framework, app int64
}

type referenceAccounting struct {
	phases map[string]referencePhaseTotal
	assets map[string]string
}

// referenceRouteAccounting folds declarations into sets of response claims,
// then sums each physical (URL, raw SHA) once. It uses no measurement helpers,
// production deduplication, phase mutation or HTTP observations. Reachability
// is unproved in this slice, so later declarations contribute startup claims.
func referenceRouteAccounting(route FixtureRoute, assets []buildmanifest.PerfAssetUse, responses map[string]referenceHTTPBody, sizes map[string]assetmeasure.Sizes) referenceAccounting {
	type claim struct{ phase, owner string }
	claims := map[string][]claim{}
	final := map[string]string{}
	representative := map[string]buildmanifest.PerfAssetUse{}
	critical := map[string]bool{}
	for _, id := range route.CriticalAssetIDs {
		critical[id] = true
	}
	for _, asset := range assets {
		phase := "startup"
		if asset.Phase == "critical" || critical[asset.ID] || asset.URL == route.RouteTemplate {
			phase = "critical"
		}
		for url := asset.URL; ; {
			response := responses[url]
			claims[url] = append(claims[url], claim{phase, asset.Owner})
			if response.location == "" {
				final[asset.URL] = url
				break
			}
			url = response.location
		}
		previous, found := representative[asset.URL]
		if !found || asset.Owner == "framework" && previous.Owner == "app" || asset.Owner == previous.Owner && asset.ID < previous.ID {
			representative[asset.URL] = asset
		}
	}
	out := referenceAccounting{phases: map[string]referencePhaseTotal{}, assets: map[string]string{}}
	responsePhase := map[string]string{}
	for url, contributions := range claims {
		phase, owner := "startup", "app"
		for _, c := range contributions {
			if c.phase == "critical" {
				phase = "critical"
			}
			if c.owner == "framework" {
				owner = "framework"
			}
		}
		responsePhase[url] = phase
		response := responses[url]
		n := sizes[string(response.raw)].Brotli
		total := out.phases[phase]
		total.bytes += n
		total.wire += int64(len(response.wire))
		total.requests++
		if owner == "framework" {
			total.framework += n
		} else {
			total.app += n
		}
		out.phases[phase] = total
	}
	for url, asset := range representative {
		out.assets[asset.ID] = responsePhase[final[url]]
	}
	return out
}

func referenceAccountingClient(t *testing.T, responses map[string]referenceHTTPBody) *http.Client {
	t.Helper()
	return &http.Client{Transport: testRoundTrip(func(request *http.Request) (*http.Response, error) {
		response, ok := responses[request.URL.Path]
		if !ok {
			return nil, fmt.Errorf("unexpected synthetic request")
		}
		header := http.Header{"Content-Type": {"text/" + response.kind}}
		if response.encoding != "identity" && response.encoding != "" {
			header.Set("Content-Encoding", response.encoding)
		}
		status := http.StatusOK
		if response.location != "" {
			header.Set("Location", response.location)
			status = http.StatusFound
		}
		return &http.Response{StatusCode: status, Header: header, Request: request,
			ContentLength: int64(len(response.wire)), Body: io.NopCloser(bytes.NewReader(response.wire))}, nil
	})}
}

func TestMeasureReferenceAccountingCorpus(t *testing.T) {
	const seed = 53603
	rng := rand.New(rand.NewSource(seed))
	phases := []string{"critical", "startup", "after-ready", "dormant"}
	shapes := []string{"distinct", "same-url", "redirect", "shared-redirect", "document-redirect"}
	sizes := map[string]assetmeasure.Sizes{}
	encodings := map[string]map[string][]byte{}
	dir := t.TempDir()
	staged := map[string]string{}
	public := publicTestReport(t).Info
	normalize := func(raw []byte) (assetmeasure.Sizes, error) {
		key := string(raw)
		if s, found := sizes[key]; found {
			return s, nil
		}
		s, err := testBodyNormalizer(raw)
		sizes[key] = s
		gz, br := testMeasureEncodings(raw)
		encodings[key] = map[string][]byte{"identity": raw, "gzip": gz, "br": br}
		return s, err
	}
	corpus := 0
	for _, shape := range shapes {
		for _, p1 := range phases {
			for _, p2 := range phases {
				for owners := 0; owners < 4; owners++ {
					for _, encoding := range []string{"identity", "gzip", "br"} {
						criticalMask := rng.Intn(4)
						for _, reverse := range []bool{false, true} {
							name := fmt.Sprintf("%s/%s/%s/owners-%d/%s/reverse-%v", shape, p1, p2, owners, encoding, reverse)
							t.Run(name, func(t *testing.T) {
								corpus++
								document := []byte("<!doctype html><html><head><title>Fixture</title></head><body><p>Stable content</p></body></html>")
								first, second := []byte("body{color:red}"), []byte("body{color:blue}")
								if shape != "distinct" || criticalMask%2 == 0 {
									second = first
								}
								caps, err := pagecaps.FromHTML(document)
								if err != nil {
									t.Fatal(err)
								}
								manifest := &FixtureManifest{Schema: "gosx.perf-fixtures/v1", Version: 1, SourceSHA: strings.Repeat("a", 40), CatalogSHA256: strings.Repeat("2", 64),
									Routes: []FixtureRoute{{App: "fixture", RouteTemplate: "/counter/", SourcePath: "fixture/page.gsx", PageTypes: []string{"static"}, Capabilities: caps,
										CriticalAssetIDs: []string{"app/fixture/html"}, InputSequenceID: "counter-input"}}}
								makeAsset := func(id, url, phase, owner, kind string, raw []byte) buildmanifest.PerfAssetUse {
									return buildmanifest.PerfAssetUse{ID: id, URL: url, Phase: phase, Owner: owner, Kind: kind, SHA256: testMeasureHash(raw), Condition: "always", Dependencies: []string{}}
								}
								manifest.Assets = append(manifest.Assets, makeAsset("app/fixture/html", "/counter/", "critical", "app", "html", document))
								for i, raw := range [][]byte{first, second} {
									owner, prefix := "app", "app/fixture/"
									if owners&(1<<i) != 0 {
										owner, prefix = "framework", "framework/runtime/"
									}
									url := fmt.Sprintf("/gosx/assets/styles/style-%d.css", i)
									if shape == "same-url" {
										url = "/gosx/assets/styles/shared.css"
									}
									asset := makeAsset(fmt.Sprintf("%sstyle-%d.css", prefix, i), url, []string{p1, p2}[i], owner, "css", raw)
									manifest.Assets = append(manifest.Assets, asset)
									if criticalMask&(1<<i) != 0 {
										manifest.Routes[0].CriticalAssetIDs = append(manifest.Routes[0].CriticalAssetIDs, asset.ID)
									}
								}
								// Repeated declarations must cost no additional requests or bytes.
								manifest.Assets = append(manifest.Assets, manifest.Assets[0])
								if reverse {
									slices.Reverse(manifest.Assets)
								}
								responses := map[string]referenceHTTPBody{}
								addResponse := func(url, kind, location string, raw []byte) {
									if _, err := normalize(raw); err != nil {
										t.Fatal(err)
									}
									responses[url] = referenceHTTPBody{raw: raw, wire: encodings[string(raw)][encoding], kind: kind, encoding: encoding, location: location}
								}
								addResponse("/counter/", "html", "", document)
								for _, asset := range manifest.Assets {
									if asset.Kind == "css" {
										raw := first
										if strings.HasSuffix(asset.ID, "style-1.css") {
											raw = second
										}
										addResponse(asset.URL, "css", "", raw)
									}
								}
								switch shape {
								case "redirect":
									addResponse("/gosx/assets/styles/style-0.css", "css", "/gosx/assets/styles/style-1.css", []byte("first redirect"))
								case "shared-redirect":
									addResponse("/gosx/assets/styles/style-0.css", "css", "/middle.css", []byte("first redirect"))
									addResponse("/gosx/assets/styles/style-1.css", "css", "/middle.css", []byte("second redirect"))
									addResponse("/middle.css", "css", "/final.css", []byte("shared redirect"))
									addResponse("/final.css", "css", "", first)
								case "document-redirect":
									addResponse("/counter/", "html", "/final-document/", []byte("document redirect"))
									addResponse("/final-document/", "html", "", document)
								}
								// Cases run sequentially; unchanged fixture bodies and
								// sidecars can be staged once without changing the inputs.
								for _, asset := range manifest.Assets {
									url := asset.URL
									for responses[url].location != "" {
										url = responses[url].location
									}
									raw := responses[url].raw
									file := "counter/index.html"
									if asset.Kind != "html" {
										file = "assets/" + strings.TrimPrefix(asset.URL, "/gosx/assets/")
									}
									full := filepath.Join(dir, file)
									if staged[full] == asset.SHA256 {
										continue
									}
									if err := os.MkdirAll(filepath.Dir(full), 0700); err != nil {
										t.Fatal(err)
									}
									for suffix, body := range map[string][]byte{"": raw, ".gz": encodings[string(raw)]["gzip"], ".br": encodings[string(raw)]["br"]} {
										if err := os.WriteFile(full+suffix, body, 0600); err != nil {
											t.Fatal(err)
										}
									}
									staged[full] = asset.SHA256
								}
								writeTestFixtureManifest(t, dir, manifest)
								info := public
								info.FixtureSHA256, info.ArtifactSHA256 = manifest.CatalogSHA256, &manifest.FixturesSHA256
								got, err := measureApp(context.Background(), MeasureOptions{App: "fixture", DistDir: dir, BaseURL: "https://example.invalid",
									Client: referenceAccountingClient(t, responses), Public: info}, normalize)
								if err != nil {
									t.Fatal(err)
								}
								want := referenceRouteAccounting(manifest.Routes[0], manifest.Assets, responses, sizes)
								assertReferenceAccounting(t, got, want)
								if got.Coverage.Reachability != "unknown" {
									t.Fatal("uncertain declarations certified")
								}
							})
						}
					}
				}
			}
		}
	}
	t.Logf("seed=%d corpus=%d", seed, corpus)
}

func assertReferenceAccounting(t *testing.T, got AppReport, want referenceAccounting) {
	t.Helper()
	if len(got.Rows) != 1 {
		t.Fatalf("row count: %d", len(got.Rows))
	}
	row := got.Rows[0]
	phaseBytes := PhaseBytes{Critical: want.phases["critical"].bytes, Startup: want.phases["startup"].bytes,
		AfterReady: want.phases["after-ready"].bytes, Dormant: want.phases["dormant"].bytes}
	if row.PhaseBytes != phaseBytes {
		t.Errorf("phase-bytes: got %+v want %+v", row.PhaseBytes, phaseBytes)
	}
	// Public rows expose per-phase bytes; request and owner fields are
	// cumulative, so compare those fields to the phase totals' sums.
	var total referencePhaseTotal
	for _, phase := range want.phases {
		total.bytes += phase.bytes
		total.wire += phase.wire
		total.requests += phase.requests
		total.framework += phase.framework
		total.app += phase.app
	}
	if row.NormalizedBytes != total.bytes || row.WireBytes != total.wire || row.Requests != total.requests {
		t.Errorf("response-totals: got bytes/wire/requests %d/%d/%d want %d/%d/%d", row.NormalizedBytes, row.WireBytes, row.Requests, total.bytes, total.wire, total.requests)
	}
	if row.FrameworkBytes != total.framework || row.AppBytes != total.app {
		t.Errorf("owner-totals: got framework/app %d/%d want %d/%d", row.FrameworkBytes, row.AppBytes, total.framework, total.app)
	}
	assetPhases := map[string]string{}
	for _, asset := range got.Assets {
		assetPhases[asset.ID] = asset.Phase
	}
	if !reflect.DeepEqual(assetPhases, want.assets) {
		t.Errorf("asset-phases: got %v want %v", assetPhases, want.assets)
	}
}
