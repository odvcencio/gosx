package budget

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"m31labs.dev/gosx/buildmanifest"
	"m31labs.dev/gosx/internal/pagecaps"
)

func graphAsset(id, assetURL, kind, phase, condition string, body []byte, deps ...string) buildmanifest.PerfAssetUse {
	owner := "framework"
	if len(id) > 4 && id[:4] == "app/" {
		owner = "app"
	}
	return buildmanifest.PerfAssetUse{ID: id, URL: assetURL, Kind: kind, Phase: phase, Condition: condition, Owner: owner, SHA256: testMeasureHash(body), Dependencies: append([]string{}, deps...)}
}
func testResourceGraph() ReachabilityOptions {
	doc := []byte(`<link rel="stylesheet" href="/css/site.css"><script defer src="/js/entry.js"></script><img loading="lazy" src="/img/hero.png">
<script id="gosx-manifest" type="application/json">{"version":"0.1.0","runtime":{"path":"/core.wasm"},"islands":[{"programRef":"/counter.bin"}],"bundles":{"dormant":{"path":"/full.wasm"}}}</script>`)
	bodies := map[string][]byte{
		"app/fixture/html": doc, "app/fixture/css": []byte(`@import "./base.css";@font-face{src:url("../fonts/a.woff2")} .a{background:url("../img/hero.png")}`),
		"app/fixture/base-css": []byte(".a{color:blue}"), "app/fixture/font": []byte("fixture font"), "app/fixture/hero": []byte("fixture image"),
		"framework/js/entry": []byte(`import("./lazy.js")`), "framework/js/lazy": []byte("const fixture=1"),
		"framework/runtime/core": []byte("fixture core"), "framework/runtime/full": []byte("fixture full"), "app/fixture/program": []byte("fixture program"),
	}
	assets := []buildmanifest.PerfAssetUse{
		graphAsset("app/fixture/html", "/counter/", "html", "critical", "always", doc),
		graphAsset("app/fixture/css", "/css/site.css", "css", "dormant", "always", bodies["app/fixture/css"], "app/fixture/base-css", "app/fixture/font", "app/fixture/hero"),
		graphAsset("app/fixture/base-css", "/css/base.css", "css", "dormant", "always", bodies["app/fixture/base-css"]),
		graphAsset("app/fixture/font", "/fonts/a.woff2", "font", "dormant", "always", bodies["app/fixture/font"]),
		graphAsset("app/fixture/hero", "/img/hero.png", "image", "dormant", "always", bodies["app/fixture/hero"]),
		graphAsset("framework/js/entry", "/js/entry.js", "js", "after-ready", "interaction", bodies["framework/js/entry"], "framework/js/lazy"),
		graphAsset("framework/js/lazy", "/js/lazy.js", "js", "dormant", "always", bodies["framework/js/lazy"]),
		graphAsset("framework/runtime/core", "/core.wasm", "wasm", "dormant", "always", bodies["framework/runtime/core"]),
		graphAsset("framework/runtime/full", "/full.wasm", "wasm", "dormant", "always", bodies["framework/runtime/full"]),
		graphAsset("app/fixture/program", "/counter.bin", "program", "dormant", "always", bodies["app/fixture/program"]),
	}
	return ReachabilityOptions{Graph: &buildmanifest.PerfAssetUses{Version: 1, Assets: assets}, Bodies: bodies, Route: FixtureRoute{RouteTemplate: "/counter/", CriticalAssetIDs: []string{"app/fixture/html", "app/fixture/font", "app/fixture/hero"}}, Backend: "none"}
}
func planPhases(plan ResourcePlan) map[string]string {
	out := map[string]string{}
	for _, asset := range plan.Assets {
		out[asset.ID] = asset.Phase
	}
	return out
}

func TestMeasureReachabilityTraversesDeclaredCSSAndModuleGraph(t *testing.T) {
	opts := testResourceGraph()
	plan, err := ResolveReachability(opts)
	want := map[string]string{
		"app/fixture/html": "critical", "app/fixture/css": "startup", "app/fixture/base-css": "startup", "app/fixture/font": "critical", "app/fixture/hero": "critical",
		"framework/js/entry": "startup", "framework/js/lazy": "startup", "framework/runtime/core": "startup", "framework/runtime/full": "dormant", "app/fixture/program": "startup",
	}
	if err != nil || plan.Reachability != "known" || !reflect.DeepEqual(planPhases(plan), want) {
		t.Fatal("declared closure or phases differ", plan, err)
	}
	// defer and an after-ready label cannot move an active module out of startup.
	if planPhases(plan)["framework/js/entry"] != "startup" {
		t.Fatal("first milestone script discounted")
	}
	plan.Assets[0].Phase = "changed"
	second, err := ResolveReachability(opts)
	if err != nil || !reflect.DeepEqual(planPhases(second), want) {
		t.Fatal("returned plan mutated graph")
	}
}

func TestMeasureReachabilityUnknownRetainsPotentialInventory(t *testing.T) {
	for _, cause := range []string{"legacy", "computed"} {
		t.Run(cause, func(t *testing.T) {
			opts := testResourceGraph()
			if cause == "legacy" {
				opts.Inventory = opts.Graph.Assets
				opts.Graph = nil
			} else {
				body := []byte("import(selected)")
				opts.Bodies["framework/js/entry"] = body
				opts.Graph.Assets[5].SHA256 = testMeasureHash(body)
			}
			plan, err := ResolveReachability(opts)
			if err != nil || plan.Reachability != "unknown" {
				t.Fatal("unknown graph certified", plan, err)
			}
			for _, asset := range plan.Assets {
				if asset.ID == "app/fixture/html" || asset.ID == "app/fixture/font" || asset.ID == "app/fixture/hero" {
					continue
				}
				if asset.Phase != "startup" {
					t.Fatal("potential body excluded or deferred", asset)
				}
			}
		})
	}
}

func TestMeasureReachabilitySceneBothBackendsAndRecovery(t *testing.T) {
	for _, beforeReady := range []bool{false, true} {
		opts := testResourceGraph()
		opts.Route.Capabilities = pagecaps.Capabilities{Scene3D: true}
		opts.Backend = "webgpu"
		glPhase := "after-ready"
		if beforeReady {
			glPhase = "startup"
		}
		for _, entry := range []struct{ id, phase, condition string }{
			{"gpu", "startup", "webgpu"}, {"gl", glPhase, "device-loss"}, {"recovery", "dormant", "pipeline-recovery"},
		} {
			id := "framework/scene/" + entry.id
			body := []byte("const fixture=1")
			opts.Bodies[id] = body
			opts.Graph.Assets = append(opts.Graph.Assets, graphAsset(id, "/"+entry.id+".js", "js", entry.phase, entry.condition, body))
		}
		plan, err := ResolveReachability(opts)
		phases := planPhases(plan)
		if err != nil || plan.Reachability != "known" || phases["framework/scene/gpu"] != "startup" || phases["framework/scene/gl"] != glPhase || phases["framework/scene/recovery"] != "dormant" {
			t.Fatal("first-frame/backend loss or dormant recovery cost differs", plan, err)
		}
		// A compatibility consumer that really needs recovery before its first
		// frame must retain that body despite its inventory declaration.
		opts.Graph.Assets[len(opts.Graph.Assets)-3].Dependencies = []string{"framework/scene/recovery"}
		plan, err = ResolveReachability(opts)
		if err != nil || planPhases(plan)["framework/scene/recovery"] != "startup" {
			t.Fatal("reachable recovery discarded", plan, err)
		}
		opts.Backend = "none"
		plan, err = ResolveReachability(opts)
		if err != nil || plan.Reachability != "unknown" || planPhases(plan)["framework/scene/gpu"] != "startup" || planPhases(plan)["framework/scene/gl"] != "startup" {
			t.Fatal("unspecified backend certified", plan, err)
		}
	}
}

func TestMeasureReachabilityRejectsFalseGraphDeclarations(t *testing.T) {
	for _, cause := range []string{"css-edge", "module-edge", "hash", "missing-body", "missing-critical", "cycle", "wrong-backend", "undeclared", "malformed", "version", "negative-backend"} {
		t.Run(cause, func(t *testing.T) {
			opts := testResourceGraph()
			switch cause {
			case "css-edge":
				opts.Graph.Assets[1].Dependencies = []string{}
			case "module-edge":
				opts.Graph.Assets[5].Dependencies = []string{}
			case "hash":
				opts.Bodies["framework/js/entry"] = []byte("changed")
			case "missing-body":
				delete(opts.Bodies, "app/fixture/font")
			case "missing-critical":
				opts.Route.CriticalAssetIDs = append(opts.Route.CriticalAssetIDs, "app/fixture/missing")
			case "cycle":
				opts.Graph.Assets[6].Dependencies = []string{"framework/js/entry"}
			case "wrong-backend":
				opts.Graph.Assets[5].Condition = "webgpu"
			case "undeclared":
				body := []byte(`import("/undeclared.js")`)
				opts.Bodies["framework/js/entry"] = body
				opts.Graph.Assets[5].SHA256 = testMeasureHash(body)
			case "malformed":
				body := []byte("import 'unfinished")
				opts.Bodies["framework/js/entry"] = body
				opts.Graph.Assets[5].SHA256 = testMeasureHash(body)
			case "version":
				opts.Graph.Version = 2
			case "negative-backend":
				opts.Backend = "other"
			}
			_, err := ResolveReachability(opts)
			var typed *InputError
			if !errors.As(err, &typed) || typed.Reference != "measure" || typed.Pointer == "" {
				t.Fatal("false closure accepted or error lacked location", err)
			}
		})
	}
}

func TestMeasureReachabilityPhysicalAliasesKeepEarliestPhase(t *testing.T) {
	opts := testResourceGraph()
	document := []byte(strings.ReplaceAll(string(opts.Bodies["app/fixture/html"]), `<script defer src="/js/entry.js"></script>`, ""))
	opts.Bodies["app/fixture/html"] = document
	opts.Graph.Assets[0].SHA256 = testMeasureHash(document)
	alias := opts.Graph.Assets[5]
	alias.ID = "app/fixture/entry"
	alias.Owner = "app"
	alias.Phase = "dormant"
	alias.Dependencies = append(alias.Dependencies, "framework/runtime/full")
	opts.Graph.Assets = append(opts.Graph.Assets, alias)
	opts.Bodies[alias.ID] = opts.Bodies["framework/js/entry"]
	plan, err := ResolveReachability(opts)
	if err != nil || planPhases(plan)[alias.ID] != "after-ready" || planPhases(plan)["framework/runtime/full"] != "after-ready" {
		t.Fatal("physical alias split between phases", plan, err)
	}
}
