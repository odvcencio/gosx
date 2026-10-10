package budget

import (
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"m31labs.dev/gosx/buildmanifest"
	"m31labs.dev/gosx/internal/pagecaps"
)

type policyPlacement struct {
	name       string
	depth      int
	child      bool
	restricted bool
	template   bool
}

func policyPlacements() []policyPlacement {
	return []policyPlacement{
		{name: "root"},
		{name: "fetched-child", child: true},
		{name: "active-srcdoc", depth: 1},
		{name: "sandboxed-srcdoc", depth: 1, restricted: true},
		{name: "srcdoc-bound", depth: pagecaps.MaxSrcdocDepth},
		{name: "sandboxed-fetched-child", child: true, restricted: true},
		{name: "template", template: true},
		{name: "srcdoc-with-fetched-child", depth: 1, child: true},
	}
}

func TestCheckInlineRuntimeDocumentPolicies(t *testing.T) {
	for _, placement := range policyPlacements() {
		t.Run(placement.name, func(t *testing.T) {
			gate := policyPlacementGate(t, "runtime-hashed", "no-inline-runtime")
			measured, info, err := measurePolicyPlacement(t, gate.Head.Info, "runtime-hashed", placement, true)
			if err != nil {
				t.Fatal(err)
			}
			installPolicyMeasurement(t, &gate, measured, info)
			want := placement.restricted || placement.template
			for _, policy := range []string{"runtime-hashed", "no-inline-runtime"} {
				if got, exists := observedPolicy(gate.Head.Rows[0], policy); !exists || got != want {
					t.Errorf("%s=%v exists=%v want=%v", policy, got, exists, want)
				}
			}
			out, err := Check(gate)
			if err != nil || out.Passed != want || !want && !hasGateViolation(out, "policy") {
				t.Fatalf("verified framework code escaped document policy: report=%+v error=%v", out, err)
			}
			if !want && (len(out.Violations) != 1 || out.Violations[0].Count != 2) {
				t.Fatal("both runtime policy violations must survive", out.Violations)
			}
		})
	}
}

// The registry used by knownPolicyResult is the schema's policy enum. Derive
// the fixed executable guardrail from the projection used by measurement too.
// A new policy or enforcing guardrail needs a placement witness here.
func checkPolicyNames(t *testing.T) []string {
	t.Helper()
	props := inputDefinitions["PageType"].(map[string]any)["properties"].(map[string]any)
	items := props["requiredPolicies"].(map[string]any)["items"].(map[string]any)
	set := map[string]bool{}
	for _, name := range items["enum"].([]any) {
		set[name.(string)] = true
	}
	for _, policy := range htmlGuardrailPolicies(HTMLMeasurement{}) {
		set[policy.Name] = true
	}
	file, _, _ := deriveInputs(t)
	for _, guardrail := range file.Guardrails {
		if guardrail.Mode == "gate" && guardrail.Key != "inlineAppExecutableBytes" {
			t.Fatalf("enforcing guardrail has no placement witness: %s", guardrail.Key)
		}
	}
	names := []string{}
	for name := range set {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func TestCheckPolicyPlacementMatrix(t *testing.T) {
	names, placements := checkPolicyNames(t), policyPlacements()
	for _, name := range names {
		for _, placement := range placements {
			for _, violate := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/%s/violate=%v", name, placement.name, violate), func(t *testing.T) {
					gate := policyPlacementGate(t, name)
					measured, info, err := measurePolicyPlacement(t, gate.Head.Info, name, placement, violate)
					active := !placement.template
					executes := active && !placement.restricted
					wantFailure := violate && active
					switch name {
					case "zero-js", "runtime-hashed", "no-inline-runtime", "no-sync-script", inlineExecutablePolicy, "declared-fetches":
						wantFailure = violate && executes
					case "canonical-build", "complete-gpu-estimate":
						// Build and GPU completeness are report evidence, not
						// executable markup; sandboxing cannot supply missing proof.
						wantFailure = violate
					}
					if err != nil {
						var input *InputError
						wantCode := map[string]string{"served-matches-build": "wrong-fixture", "wasm-streaming": "policy"}[name]
						if !wantFailure || !errors.As(err, &input) || input.Code != wantCode || CheckExitCode(nil, err) != 2 {
							t.Fatalf("unexpected measurement rejection: %v", err)
						}
						return
					}
					installPolicyMeasurement(t, &gate, measured, info)
					if name == "complete-gpu-estimate" {
						// GPU collection supplies separate report evidence. Exercise
						// missing proof and a supplied public proof control at Check.
						if !violate {
							gate.Head.Rows[0].Policies = append(gate.Head.Rows[0].Policies, PolicyResult{Name: name, Passed: true})
						}
					} else if got, exists := observedPolicy(gate.Head.Rows[0], name); exists && got == wantFailure {
						t.Errorf("policy verdict=%v want=%v", got, !wantFailure)
					}
					for _, reportOnly := range []bool{false, true} {
						gate.ReportOnly = reportOnly
						out, err := Check(gate)
						if name == "canonical-build" && wantFailure {
							var input *InputError
							if !errors.As(err, &input) || input.Code != "noncanonical" || CheckExitCode(out, err) != 2 {
								t.Fatalf("noncanonical evidence accepted: %v", err)
							}
							continue
						}
						if err != nil || out.Passed == wantFailure || wantFailure && !hasGateViolation(out, "policy") {
							t.Fatalf("policy placement verdict differs: failure=%v report=%+v error=%v", wantFailure, out, err)
						}
						wantExit := 0
						if wantFailure && !reportOnly {
							wantExit = 1
						}
						if CheckExitCode(out, err) != wantExit {
							t.Fatal("incorrect policy exit code", CheckExitCode(out, err))
						}
					}
				})
			}
		}
	}
	t.Logf("policy placement matrix: %d policies, %d placements, %d violation/control measurements", len(names), len(placements), len(names)*len(placements)*2)
}

func observedPolicy(row Row, name string) (bool, bool) {
	passed, exists := true, false
	for _, policy := range row.Policies {
		if policy.Name == name {
			passed, exists = passed && policy.Passed, true
		}
	}
	return passed, exists
}

func policyPlacementGate(t *testing.T, policies ...string) CheckOptions {
	t.Helper()
	gate := gateOptions(t)
	file, profile, coefficients := workedFile(t)
	page := file.PageTypes["enhanced"]
	page.RequiredPolicies = []string{}
	for _, policy := range policies {
		if policy != inlineExecutablePolicy {
			page.RequiredPolicies = append(page.RequiredPolicies, policy)
		}
	}
	file.PageTypes = map[string]PageType{"enhanced": page}
	file.Routes[0].PageTypes = []string{"enhanced"}
	derived, err := Derive(file, profile, coefficients)
	if err != nil {
		t.Fatal(err)
	}
	gate.File, gate.Profile, gate.Coefficients = derived, profile, coefficients
	return gate
}

func installPolicyMeasurement(t *testing.T, gate *CheckOptions, report AppReport, info PublicInfo) {
	t.Helper()
	gate.Head.Info, gate.Head.Rows = info, nil
	for _, row := range report.Rows {
		if row.PageType == "enhanced" {
			gate.Head.Rows = append(gate.Head.Rows, row)
		}
	}
	if len(gate.Head.Rows) != 1 {
		t.Fatal("enhanced policy row missing", report.Rows)
	}
	gate.Head.Assets, gate.Head.Coverage = report.Assets, report.Coverage
	gate.Base.Rows = append([]Row{}, gate.Head.Rows...)
	gate.Base.Assets, gate.Base.Coverage = append([]AssetReport{}, report.Assets...), report.Coverage
}

func measurePolicyPlacement(t *testing.T, info PublicInfo, policy string, placement policyPlacement, violate bool) (AppReport, PublicInfo, error) {
	t.Helper()
	fragment := `<p>Policy control</p>`
	framework := []byte("/*" + strings.Repeat("x", 2218) + "*/")
	resource := []byte("fixture image")
	kind, resourceURL := "image", "/pixel."+testMeasureHash(resource)+".png"
	owner := "app"
	transport := false
	switch policy {
	case "zero-js", "no-sync-script":
		if violate {
			fragment = `<script>window.fixtureReady=1</script>`
		}
	case inlineExecutablePolicy:
		if violate {
			fragment = `<script type="module">` + strings.Repeat("x", 1025) + `</script>`
		}
	case "runtime-hashed", "no-inline-runtime":
		resource, kind, owner = framework, "js", "framework"
		resourceURL = "/runtime." + testMeasureHash(resource) + ".js"
		if violate {
			fragment = `<script type="module">` + string(framework) + `</script>`
		}
	case "declared-fetches":
		if violate {
			fragment = `<script type="module">fetch(window.fixtureURL)</script>`
		}
	case "assets-compressed", "immutable-hashed":
		fragment = `<img src="` + resourceURL + `">`
	case "wasm-streaming":
		resource, kind = []byte("fixture wasm"), "wasm"
		resourceURL = "/module." + testMeasureHash(resource) + ".wasm"
		fragment = `<link rel="preload" href="` + resourceURL + `">`
	case "html-compressed", "html-shareable", "no-cookie", "served-matches-build":
		transport = true
	case "canonical-build", "complete-gpu-estimate":
	default:
		t.Fatalf("registered policy has no placement witness: %s", policy)
	}
	root, child := fragment, `<p>Unused child</p>`
	attributes := ""
	if placement.restricted {
		attributes = "sandbox"
	}
	if placement.child {
		root, child = `<iframe `+attributes+` src="/child/"></iframe>`, fragment
	}
	if placement.depth > 0 {
		root = srcdocWrap(root, placement.depth, attributes)
	}
	if placement.template {
		root = `<template>` + root + `</template>`
	}
	wrap := func(s string) []byte { return []byte(`<!doctype html><html><body>` + s + `</body></html>`) }
	documents := map[string][]byte{"/counter/": wrap(root), "/child/": wrap(child), resourceURL: resource}
	caps, err := pagecaps.FromHTML(documents["/counter/"])
	if err != nil {
		t.Fatal(err)
	}
	resourceID := owner + "/fixture/resource"
	manifest := &FixtureManifest{Schema: "gosx.perf-fixtures/v1", Version: 1, SourceSHA: info.SHA, CatalogSHA256: info.FixtureSHA256,
		Routes: []FixtureRoute{{App: "fixture", RouteTemplate: "/counter/", SourcePath: "fixture/page.gsx", PageTypes: []string{"static", "enhanced"}, Capabilities: caps, CriticalAssetIDs: []string{"app/fixture/html"}, InputSequenceID: "counter-input"}},
		Assets: []buildmanifest.PerfAssetUse{
			graphAsset("app/fixture/html", "/counter/", "html", "critical", "always", documents["/counter/"]),
			graphAsset("app/fixture/child", "/child/", "html", "dormant", "always", documents["/child/"]),
			graphAsset(resourceID, resourceURL, kind, "dormant", "always", resource),
		}}
	dir := t.TempDir()
	encoded := map[string][]byte{}
	for _, asset := range manifest.Assets {
		file := strings.TrimPrefix(asset.URL, "/")
		if asset.Kind == "html" {
			file = strings.Trim(file, "/") + "/index.html"
		}
		file = filepath.Join(dir, file)
		if err := os.MkdirAll(filepath.Dir(file), 0700); err != nil {
			t.Fatal(err)
		}
		body := documents[asset.URL]
		var compressed bytes.Buffer
		writer, _ := gzip.NewWriterLevel(&compressed, gzip.BestCompression)
		writer.Write(body)
		writer.Close()
		encoded[asset.URL] = compressed.Bytes()
		for name, data := range map[string][]byte{file: body, file + ".gz": compressed.Bytes()} {
			if err := os.WriteFile(name, data, 0600); err != nil {
				t.Fatal(err)
			}
		}
	}
	writeTestFixtureManifest(t, dir, manifest)
	info.ArtifactSHA256 = &manifest.FixturesSHA256
	if policy == "canonical-build" && violate {
		info.Canonical = false
	}
	faultURL := "/counter/"
	if placement.child {
		faultURL = "/child/"
	}
	client := &http.Client{Transport: testRoundTrip(func(req *http.Request) (*http.Response, error) {
		body, found := documents[req.URL.Path]
		if !found {
			t.Fatalf("undeclared matrix request: %s", req.URL.Path)
		}
		mediaType := "text/html"
		if req.URL.Path == resourceURL {
			mediaType = map[string]string{"image": "image/png", "js": "text/javascript", "wasm": "application/wasm"}[kind]
		}
		header := http.Header{"Content-Type": {mediaType}, "Content-Encoding": {"gzip"}, "Cache-Control": {"public, max-age=31536000, immutable"}}
		wire := encoded[req.URL.Path]
		fault := violate && !placement.template && (transport && req.URL.Path == faultURL || !transport && req.URL.Path == resourceURL)
		if fault {
			switch policy {
			case "html-compressed", "assets-compressed":
				wire = body
				header.Del("Content-Encoding")
			case "html-shareable":
				header.Set("Cache-Control", "private, no-store")
			case "no-cookie":
				header.Set("Set-Cookie", "fixture=1")
			case "immutable-hashed":
				header.Set("Cache-Control", "public, max-age=60")
			case "served-matches-build":
				wire = []byte("changed fixture")
				header.Del("Content-Encoding")
			case "wasm-streaming":
				header.Set("Content-Type", "application/octet-stream")
			}
		}
		return &http.Response{StatusCode: 200, Header: header, Body: io.NopCloser(bytes.NewReader(wire)), ContentLength: int64(len(wire))}, nil
	})}
	report, err := measureApp(context.Background(), MeasureOptions{App: "fixture", DistDir: dir, BaseURL: "https://example.invalid", Client: client, Public: info}, executionCorpusEncoder)
	return report, info, err
}
