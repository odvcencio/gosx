package budget

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"os"
	"sort"
	"strconv"

	"m31labs.dev/gosx/buildmanifest"
	"strings"

	"m31labs.dev/gosx/client/runtime/host"
	"m31labs.dev/gosx/internal/assetmeasure"
	"m31labs.dev/gosx/internal/pagecaps"
)

// AppReport is a native measurement result, not an additional JSON root.
type AppReport struct {
	App      string
	Rows     []Row
	Assets   []AssetReport
	Coverage ByteCoverage
}
type MeasureOptions struct {
	App, DistDir, BaseURL string
	Routes                []string
	Client                *http.Client
	Pin                   assetmeasure.CompressorPin
	Public                PublicInfo
}

// Measure verifies production fixture bodies, fresh documents and declared
// resource closure. Unresolved reachability retains potential startup bytes;
// browser reconciliation and model checks remain separate.
func Measure(ctx context.Context, opts MeasureOptions) (AppReport, error) {
	if _, err := assetmeasure.Measure(nil, opts.Pin); err != nil {
		return AppReport{}, measureFailure("noncanonical", "/pin")
	}
	return measureApp(ctx, opts, func(body []byte) (assetmeasure.Sizes, error) { return assetmeasure.Measure(body, opts.Pin) })
}

func measureApp(ctx context.Context, opts MeasureOptions, normalize bodyNormalizer) (AppReport, error) {
	result := AppReport{App: opts.App, Rows: []Row{}, Assets: []AssetReport{}, Coverage: ByteCoverage{Reachability: "known"}}
	if validateInput(opts.App, inputDefinitions["ID"]) != nil {
		return result, measureFailure("invalid-input", "/app")
	}
	root, err := os.OpenRoot(opts.DistDir)
	if err != nil {
		return result, measureFailure("wrong-fixture", "/dist")
	}
	defer root.Close()
	data, err := readMeasureFile(root, "perf-fixtures.v1.json", maxInputBytes)
	if err != nil {
		return result, err
	}
	manifest, err := DecodeFixtureManifest(bytes.NewReader(data))
	if err != nil {
		return result, err
	}
	if manifest.SourceSHA != opts.Public.SHA || manifest.FixturesSHA256 != opts.Public.FixtureSHA256 {
		return result, measureFailure("wrong-fixture", "/manifest/provenance")
	}
	selected := map[string]bool{}
	for _, route := range opts.Routes {
		if selected[route] || !validRoute(route) {
			return result, measureFailure("invalid-input", "/routes")
		}
		selected[route] = true
	}
	if len(selected) == 0 {
		for _, route := range manifest.Routes {
			if route.App == opts.App {
				selected[route.RouteTemplate] = true
			}
		}
	}
	routes := make([]FixtureRoute, 0, len(selected))
	for _, route := range manifest.Routes {
		if route.App == opts.App && selected[route.RouteTemplate] {
			routes = append(routes, route)
			delete(selected, route.RouteTemplate)
		}
	}
	if len(selected) != 0 || len(routes) == 0 {
		return result, measureFailure("wrong-fixture", "/routes")
	}
	sort.Slice(routes, func(i, j int) bool { return routes[i].RouteTemplate < routes[j].RouteTemplate })
	result.Coverage.RoutesExpected = int64(len(routes))
	var fixtures []fixtureBody
	byURL := map[string]int{}
	uses := []buildmanifest.PerfAssetUse{}
	bodies := map[string][]byte{}
	for _, use := range manifest.Assets {
		if use.Owner == "app" && !strings.HasPrefix(use.ID, "app/"+opts.App+"/") {
			continue
		}
		body, representations, err := readFixtureBody(root, use.URL, use.Kind)
		if err != nil {
			return result, err
		}
		uses = append(uses, use)
		bodies[use.ID] = body
		if previous, ok := byURL[use.URL]; ok {
			if fixtures[previous].sha != use.SHA256 || fixtures[previous].kind != use.Kind {
				return result, measureFailure("wrong-fixture", "/manifest/assets")
			}
			// One physical identity gets framework ownership only when declared as such.
			if use.Owner == "framework" {
				fixtures[previous].owner = "framework"
				fixtures[previous].id = use.ID
			}
			continue
		}
		sizes, err := normalize(body)
		if err != nil || sizes.SHA256 != use.SHA256 {
			return result, measureFailure("wrong-fixture", "/manifest/assets/body")
		}
		byURL[use.URL] = len(fixtures)
		fixtures = append(fixtures, fixtureBody{id: use.ID, sha: use.SHA256, url: use.URL, owner: use.Owner, kind: use.Kind, condition: use.Condition, dependencies: append([]string{}, use.Dependencies...), body: body, representations: representations, sizes: sizes})
	}
	inlineHashes := map[string]bool{}
	for _, fixture := range fixtures {
		if fixture.owner == "framework" && fixture.kind == "js" {
			inlineHashes[fixture.sha] = true
		}
	}
	inlineFramework := make([]string, 0, len(inlineHashes))
	for hash := range inlineHashes {
		inlineFramework = append(inlineFramework, hash)
	}
	sort.Strings(inlineFramework)
	result.Coverage.AssetsExpected = int64(len(fixtures))
	for _, fixture := range fixtures {
		phase := "dormant"
		result.Assets = append(result.Assets, AssetReport{ID: fixture.id, SHA256: fixture.sha, Owner: fixture.owner, Phase: phase, Raw: fixture.sizes.Raw, Gzip: fixture.sizes.Gzip, Brotli: fixture.sizes.Brotli,
			ChangedSources: []string{}, App: opts.App, Kind: fixture.kind, Condition: fixture.condition, Dependencies: fixture.dependencies})
	}
	for _, route := range routes {
		index, ok := byURL[route.RouteTemplate]
		if !ok || fixtures[index].kind != "html" || fixtures[index].owner != "app" {
			return result, measureFailure("wrong-fixture", "/routes/document")
		}
		document := fixtures[index]
		fields := []HTMLField{{Element: "script", Attribute: "nonce"}, {Element: "style", Attribute: "nonce"}, {Element: "link", Attribute: "nonce"}}
		httpOpts := HTTPMeasureOptions{Client: opts.Client, BaseURL: opts.BaseURL, URL: document.url, Kind: "html", ExpectedSHA256: document.sha, ExpectedBody: document.body, Representations: document.representations, HTMLFields: fields, ServingCompressor: "go-brotli-4", Pin: opts.Pin}
		first, err := measureHTTP(ctx, httpOpts, normalize)
		if err != nil {
			return result, err
		}
		second, err := measureHTTP(ctx, httpOpts, normalize)
		if err != nil {
			return result, err
		}
		htmlOpts := HTMLMeasureOptions{Fields: fields, FrameworkScriptSHA256: inlineFramework}
		measuredHTML, err := measureHTML(first.body, htmlOpts, normalize)
		if err != nil {
			return result, err
		}
		repeatedHTML, err := measureHTML(second.body, htmlOpts, normalize)
		if err != nil {
			return result, err
		}
		if err := VerifyHTMLRenders(measuredHTML, repeatedHTML); err != nil {
			return result, err
		}
		caps, err := pagecaps.FromHTML(first.body)
		if err != nil {
			return result, measureFailure("capability", "/routes/capabilities")
		}
		observedCaps, _ := json.Marshal(caps)
		declaredCaps, _ := json.Marshal(route.Capabilities)
		if !bytes.Equal(observedCaps, declaredCaps) {
			return result, measureFailure("capability", "/routes/capabilities")
		}
		detected, err := pagecaps.Classify(caps, false)
		if err != nil || !fixtureCoversTypes(route.PageTypes, detected) {
			return result, measureFailure("capability", "/routes/pageTypes")
		}
		row := Row{App: opts.App, RouteTemplate: route.RouteTemplate, Scenario: "hard-cold", Status: "unavailable", ReasonCode: "unknown-reachability", Backend: opts.Public.Backend, ModelStatus: "unknown", Policies: append([]PolicyResult{}, first.Policies...)}
		if row.Backend == "" {
			row.Backend = "none"
		}
		plan, err := ResolveReachability(ReachabilityOptions{Graph: &buildmanifest.PerfAssetUses{Version: 1, Assets: uses}, Bodies: bodies, Route: route, Backend: row.Backend})
		if err != nil {
			return result, err
		}
		if plan.Reachability == "unknown" {
			result.Coverage.Reachability = "unknown"
		} else {
			row.ReasonCode = "insufficient-data"
		}
		phases := map[string]string{}
		for _, asset := range plan.Assets {
			phases[asset.ID] = asset.Phase
		}
		for i := range result.Assets {
			result.Assets[i].Phase = earlierPhase(result.Assets[i].Phase, phases[result.Assets[i].ID])
		}
		costs := []PhaseCost{{RequestIdentity: first.finalURL, Phase: "critical", Owner: "app", Sizes: measuredHTML.Sizes, WireBytes: first.finalWireBytes, Requests: 1}}
		for i, redirect := range first.RedirectSizes {
			costs = append(costs, PhaseCost{RequestIdentity: "document-redirect:" + strconv.Itoa(i), Phase: "critical", Owner: "app", Sizes: redirect, WireBytes: first.redirectWireBytes[i], Requests: 1})
		}
		inline := map[string]int64{first.finalURL: measuredHTML.Framework.Brotli}
		coldInline := map[string]bool{first.finalURL: true}
		noExecutableAssets := true
		for _, fixture := range fixtures {
			phase := phases[fixture.id]
			if phase != "dormant" && (fixture.kind == "js" || fixture.kind == "wasm" || fixture.kind == "program") {
				noExecutableAssets = false
			}
			if fixture.url == document.url {
				continue
			}
			if phase == "dormant" {
				costs = append(costs, PhaseCost{RequestIdentity: fixture.url, Phase: phase, Owner: fixture.owner, Sizes: fixture.sizes})
				continue
			}
			options := HTTPMeasureOptions{Client: opts.Client, BaseURL: opts.BaseURL, URL: fixture.url, Kind: fixture.kind, ExpectedSHA256: fixture.sha, ExpectedBody: fixture.body, Representations: fixture.representations, Pin: opts.Pin}
			if fixture.kind == "html" {
				options.HTMLFields = fields
				options.ServingCompressor = "go-brotli-4"
			}
			observed, err := measureHTTP(ctx, options, normalize)
			if err != nil {
				return result, err
			}
			if fixture.kind == "html" {
				repeat, err := measureHTTP(ctx, options, normalize)
				if err != nil {
					return result, err
				}
				html, err := measureHTML(observed.body, htmlOpts, normalize)
				if err != nil {
					return result, err
				}
				again, err := measureHTML(repeat.body, htmlOpts, normalize)
				if err != nil {
					return result, err
				}
				if err := VerifyHTMLRenders(html, again); err != nil {
					return result, err
				}
				if fixture.owner == "app" {
					inline[observed.finalURL] = html.Framework.Brotli
					coldInline[observed.finalURL] = coldInline[observed.finalURL] || phaseRank(phase) <= 1
				}
			}
			costs = append(costs, PhaseCost{RequestIdentity: observed.finalURL, Phase: phase, Owner: fixture.owner, Sizes: observed.Sizes, WireBytes: observed.finalWireBytes, Requests: 1})
			for i, redirect := range observed.RedirectSizes {
				costs = append(costs, PhaseCost{RequestIdentity: "asset-redirect:" + fixture.id + ":" + strconv.Itoa(i), Phase: phase, Owner: fixture.owner, Sizes: redirect, WireBytes: observed.redirectWireBytes[i], Requests: 1})
			}
			row.Policies = mergeMeasurePolicies(row.Policies, observed.Policies)
		}
		totals, err := SumPhases(costs)
		if err != nil {
			return result, err
		}
		row.NormalizedBytes, row.FrameworkBytes, row.AppBytes = totals.NormalizedBytes, totals.FrameworkBytes, totals.AppBytes
		row.WireBytes, row.Requests, row.PhaseBytes = totals.WireBytes, totals.Requests, totals.Phases
		frameworkBodies := map[string]bool{}
		for _, cost := range costs {
			if cost.Owner == "framework" {
				frameworkBodies[cost.RequestIdentity] = true
			}
		}
		for identity, n := range inline {
			if coldInline[identity] && !frameworkBodies[identity] {
				row.FrameworkBytes += n
				row.AppBytes -= n
			}
		}
		row.Policies = append(row.Policies, PolicyResult{Name: "zero-js", Passed: noExecutableAssets && measuredHTML.ExecutableScripts == 0 && !caps.WASM && caps.Runtime == "none" && !caps.Navigation && !caps.Motion}, PolicyResult{Name: "no-inline-runtime", Passed: measuredHTML.Framework.Raw == 0})
		row.HeadroomBytes = -row.NormalizedBytes
		for _, name := range route.PageTypes {
			copy := row
			copy.PageType = name
			copy.Policies = append([]PolicyResult{}, row.Policies...)
			result.Rows = append(result.Rows, copy)
		}
		result.Coverage.RoutesMeasured++
	}
	result.Coverage.AssetsMeasured = int64(len(fixtures))
	sort.Slice(result.Assets, func(i, j int) bool { return result.Assets[i].ID < result.Assets[j].ID })
	return result, nil
}

type fixtureBody struct {
	id, sha, url, owner, kind, condition string
	dependencies                         []string
	body                                 []byte
	representations                      map[string][]byte
	sizes                                assetmeasure.Sizes
}

func readFixtureBody(root *os.Root, assetURL, kind string) ([]byte, map[string][]byte, error) {
	if assetURL == host.NavigationRuntimePath {
		body := []byte(host.NavigationRuntime)
		return body, map[string][]byte{"gzip": host.NavigationRuntimeGzip, "br": host.NavigationRuntimeBrotli}, nil
	}
	file := strings.TrimPrefix(assetURL, "/")
	if kind == "html" {
		file = strings.Trim(file, "/")
		if file != "" {
			file += "/"
		}
		file += "index.html"
	} else if i := strings.Index(assetURL, "/gosx/assets/"); i >= 0 {
		file = "assets/" + assetURL[i+len("/gosx/assets/"):]
	}
	body, err := readMeasureFile(root, file, maxMeasureBody)
	if err != nil {
		return nil, nil, err
	}
	representations := map[string][]byte{}
	for _, sidecar := range []struct{ suffix, encoding string }{{".gz", "gzip"}, {".br", "br"}} {
		info, err := root.Stat(file + sidecar.suffix)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil || !info.Mode().IsRegular() {
			return nil, nil, measureFailure("stale-sidecar", "/file")
		}
		encoded, err := readMeasureFile(root, file+sidecar.suffix, maxMeasureBody)
		if err != nil || assetmeasure.VerifySidecar(body, encoded, sidecar.encoding) != nil {
			return nil, nil, measureFailure("stale-sidecar", "/file")
		}
		representations[sidecar.encoding] = encoded
	}
	return body, representations, nil
}

func fixtureCoversTypes(declared, detected []string) bool {
	present := map[string]bool{}
	for _, name := range declared {
		family, _, ok := pageTypeVariant(name)
		if !ok {
			return false
		}
		present[family] = true
	}
	for _, name := range detected {
		if !present[name] {
			return false
		}
	}
	return true
}
func mergeMeasurePolicies(a, b []PolicyResult) []PolicyResult {
	byName := map[string]bool{}
	for _, policy := range a {
		byName[policy.Name] = policy.Passed
	}
	for _, policy := range b {
		previous, found := byName[policy.Name]
		byName[policy.Name] = !found && policy.Passed || found && previous && policy.Passed
	}
	out := make([]PolicyResult, 0, len(byName))
	for name, passed := range byName {
		out = append(out, PolicyResult{Name: name, Passed: passed})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}
