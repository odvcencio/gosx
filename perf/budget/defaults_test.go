package budget

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"m31labs.dev/gosx/internal/pagecaps"
)

func canonicalDefaults(t *testing.T) *Inputs {
	t.Helper()
	root := projectRoot(t)
	inputs, err := LoadInputs(filepath.Join(root, "perf/budgets/gosx.budget.json"), LoadOptions{RootDir: root})
	if err != nil {
		t.Fatal(err)
	}
	return inputs
}

func TestCanonicalDefaultsDerivationAndAppPromise(t *testing.T) {
	inputs := canonicalDefaults(t)
	if err := VerifyDerivation(inputs.File, inputs.Profile, inputs.Coefficients); err != nil {
		t.Fatal(err)
	}
	if len(inputs.File.PageTypes) != 16 || inputs.Profile.Reference != "desktop-cpu-proxy" || inputs.Profile.BenchmarkIndexTarget != nil {
		t.Fatal("incomplete or mislabeled defaults")
	}
	for name, page := range inputs.File.PageTypes {
		if page.Allocation.Status != "illustrative" || page.AfterReadyAllocation.Status != "illustrative" || page.MinAppPPM != 500000 {
			t.Fatal("uncertified allocation or app promise changed", name)
		}
		wantNetwork := "p75"
		if name == "static" || name == "enhanced" || name == "island" {
			wantNetwork = "slow4g"
		}
		if page.Network != wantNetwork {
			t.Fatal("network split changed", name)
		}
		for _, allocation := range []Derivation{page.Allocation, page.AfterReadyAllocation} {
			pool := allocation.TotalBytes - allocation.AppCriticalReserveBytes
			if allocation.MinAppBytes*2 < pool || allocation.FrameworkBytes+allocation.MinAppBytes != pool {
				t.Fatal("app promise not reconciled", name)
			}
			if allocation.TotalBytes%1024 != 0 {
				t.Fatal("allocation quantum changed", name)
			}
		}
		if strings.HasPrefix(name, "scene3d/") || strings.HasPrefix(name, "game/") {
			if page.Goals[0].Max != "3000" || page.PrimaryMetric != "fif" {
				t.Fatal("3D cold goal changed", name)
			}
			if page.Backend != "webgpu" && page.Backend != "webgl2" {
				t.Fatal("3D backend unspecified", name)
			}
		} else if page.Backend != "none" {
			t.Fatal("backend suffix leaked to another family", name)
		}
	}
	for name, want := range map[string]int64{"static": 124928, "enhanced": 148480, "island": 438272, "scene3d/js-webgl2": 563200, "scene3d/js-webgpu": 563200} {
		if inputs.File.PageTypes[name].Allocation.TotalBytes != want {
			t.Fatal("provisional arithmetic changed", name)
		}
	}
	island := inputs.File.PageTypes["island"]
	if island.Allocation.FrameworkBytes != 193536 || island.Allocation.MinAppBytes != 193536 || len(inputs.File.Exceptions) != 0 {
		t.Fatal("framework deficit was waived")
	}
	if inputs.File.PageTypes["static"].Allocation.FrameworkBytes != 0 {
		t.Fatal("static framework allocation changed")
	}
	for _, g := range inputs.File.Guardrails {
		if strings.HasSuffix(g.Key, "Requests") && g.Mode != "target" {
			t.Fatal("request target became a gate")
		}
	}
}

func TestCanonicalDefaultsKeepPilotsSeparateFromPriors(t *testing.T) {
	inputs := canonicalDefaults(t)
	for _, set := range inputs.Coefficients.Sets {
		for _, entry := range set.Entries {
			if entry.Status != "provisional" || entry.Method != "prior" || entry.NVisits != 0 || entry.NBlocks != 0 {
				t.Fatal("planning costs gained measurement authority")
			}
		}
	}
	pilot, err := LoadCoefficients(filepath.Join(inputs.RootDir(), "perf/profiles/mid-tier-mobile.v1.pilot.json"), LoadOptions{RootDir: inputs.RootDir()})
	if err != nil {
		t.Fatal(err)
	}
	if pilot.Reference != "desktop-cpu-proxy" || pilot.ProfileSHA256 != inputs.File.Profile.SHA256 || len(pilot.Sets) != 1 {
		t.Fatal("pilot binding changed")
	}
	for _, entry := range pilot.Sets[0].Entries {
		if entry.Name == "wasmCompileMicrosPerRawKB" {
			if entry.Status != "pilot" || entry.NVisits != 10 || entry.NBlocks != 10 || entry.Value == nil || *entry.Value != 2 || entry.CI95[0] == nil || *entry.CI95[0] != 1 || *entry.CI95[1] != 3 {
				t.Fatal("three-size API pilot changed")
			}
		} else if entry.Status != "unknown" || entry.Value != nil || entry.CI95[0] != nil || entry.NVisits != 0 {
			t.Fatal("unidentified pilot cost became zero or measured")
		}
	}
	for _, page := range inputs.File.PageTypes {
		if page.CoefficientSet == pilot.Sets[0].ID {
			t.Fatal("uncalibrated pilot selected for byte caps")
		}
	}
}

func TestCanonicalCatalogRetainsLegacyRoutesAndCapabilities(t *testing.T) {
	inputs := canonicalDefaults(t)
	data, err := os.ReadFile(filepath.Join(inputs.RootDir(), inputs.File.Fixtures.File))
	if err != nil {
		t.Fatal(err)
	}
	var catalog struct {
		Routes []FixtureRoute `json:"routes"`
	}
	if err := json.Unmarshal(data, &catalog); err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, route := range catalog.Routes {
		key := route.App + "|" + route.RouteTemplate
		if seen[key] {
			t.Fatal("duplicate registered route")
		}
		seen[key] = true
		families, err := pagecaps.Classify(route.Capabilities, false)
		if err != nil {
			t.Fatal(err)
		}
		for _, family := range families {
			found := false
			for _, name := range route.PageTypes {
				actual, _, _ := pageTypeVariant(name)
				found = found || actual == family
			}
			if !found {
				t.Fatal("declared capability lost an obligation", key, family)
			}
		}
	}
	data, err = os.ReadFile(filepath.Join(inputs.RootDir(), "perf/budgets/wire.json"))
	if err != nil {
		t.Fatal(err)
	}
	var legacy struct {
		Apps map[string]map[string]json.RawMessage `json:"apps"`
	}
	if err := json.Unmarshal(data, &legacy); err != nil {
		t.Fatal(err)
	}
	count := 0
	for app, routes := range legacy.Apps {
		for route := range routes {
			count++
			if !seen[app+"|"+route] {
				t.Fatal("legacy route missing", app, route)
			}
		}
	}
	if count != 11 || len(catalog.Routes) != count || len(inputs.File.Routes) != count {
		t.Fatal("required route coverage changed")
	}
}

func TestCanonicalPublicCatalogBindsTrackedIdentifiers(t *testing.T) {
	inputs := canonicalDefaults(t)
	validator, err := inputs.PublicValidator()
	if err != nil {
		t.Fatal(err)
	}
	if !validator.apps["docs"] || !validator.apps["scaffold"] || validator.apps["fixture"] {
		t.Fatal("public app membership changed")
	}
	if !validator.sources["perf/fixtures/catalog.v1.json"] || !validator.routes["scaffold|/counter/"]["island"] {
		t.Fatal("catalog or route is untracked")
	}
	if validator.assets["framework/runtime/navigation.js"] != "framework" || validator.assets["framework/runtime/core.wasm"] != "framework" || validator.assets["app/scaffold/islands/Counter"] != "app" {
		t.Fatal("public asset ownership missing")
	}
	if len(validator.routes) != 11 {
		t.Fatal("public route registration changed")
	}
}
