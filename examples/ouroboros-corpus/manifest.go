package main

type RouteRecord struct {
	ID                    string   `json:"id"`
	Route                 string   `json:"route"`
	FixtureApp            string   `json:"fixtureApp"`
	Purpose               string   `json:"purpose,omitempty"`
	ExpectedRuntime       string   `json:"expectedRuntime,omitempty"`
	ExpectedCapabilities  []string `json:"expectedCapabilities"`
	ExpectedTinyGoCurrent string   `json:"expectedTinyGoCurrent"`
	ExpectedTinyGoFuture  string   `json:"expectedTinyGoFuture"`
	ServerBuildMode       string   `json:"serverBuildMode"`
	RequiredInteractions  []string `json:"requiredInteractions"`
	RequiredScreenshots   []string `json:"requiredScreenshots"`
	RoutePlanAssertions   []string `json:"routePlanAssertions"`
	DisallowedAssets      []string `json:"disallowedRuntimeAssets"`
	External              bool     `json:"external,omitempty"`
	Notes                 string   `json:"notes,omitempty"`
}

type RouteManifest struct {
	SchemaVersion   string        `json:"schemaVersion"`
	ContractVersion string        `json:"contractVersion"`
	CorpusID        string        `json:"corpusID"`
	FixtureApp      string        `json:"fixtureApp"`
	Authoring       []string      `json:"authoring"`
	Routes          []RouteRecord `json:"routes"`
}

const fixtureApp = "examples/ouroboros-corpus"

var corpusManifest = RouteManifest{
	SchemaVersion:   "gosx.ouroboros.fixtures.v1",
	ContractVersion: "O0.2",
	CorpusID:        "gosx-ouroboros-o0.2-v1",
	FixtureApp:      fixtureApp,
	Authoring:       []string{"go", "gsx-compatible-node-api"},
	Routes: []RouteRecord{
		route("R00", "/static", "SSR only", "none", []string{"ssr"}, "none", "none", []string{"no bootstrap", "no manifest"}),
		route("R01", "/lite", "declarative regions/actions without WASM", "lite", []string{"regions", "actions"}, "none", "core", []string{"bootstrap mode lite", "no wasm"}),
		route("R02", "/island/counter", "one visual island and one action", "islands", []string{"island", "action"}, "islands", "core", []string{"one island", "patch runtime", "valid action endpoint"}),
		route("R03", "/islands/kitchen", "multiple islands and shared signals", "islands", []string{"islands", "signals"}, "islands", "core", []string{"five islands", "shared signal program", "shared signal manifest entry"}),
		route("R04", "/action/form", "server action with validation and redirect", "action bridge", []string{"actions", "redirect"}, "none", "core", []string{"declarative action marker", "bootstrap mode lite", "validation response", "redirect response"}),
		route("R05", "/canvas-board", "CanvasBoard engine surface", "engine", []string{"canvas", "engine"}, "runtime", "engine", []string{"CanvasBoard marker", "canvas2d surface"}),
		route("R06", "/hub/echo", "hub bind, fanout, and shared signal update", "collab", []string{"hub", "signals"}, "runtime", "collab", []string{"hub manifest", "echo binding", "no wasm runtime path"}),
		route("R07", "/video-sync", "video engine and drift bridge", "video", []string{"video", "engine"}, "runtime", "engine", []string{"video engine", "follow sync props", "local media endpoint", "same-origin sync socket", "no wasm runtime path"}),
		sceneRoute("R08", "/scene/basic", "bounded Scene3D PBR scene", "scene3d", []string{"scene3d", "webgpu", "webgl"}, "runtime", "engine", []string{"Scene3D engine", "scene3d feature path", "WebGPU remains selectable", "no wasm runtime path"}),
		route("R09A", "/navigation/a", "client navigation entry route", "navigation", []string{"navigation", "dispose"}, "none", "core", []string{"data-gosx-link", "same-document navigation", "island dispose/remount evidence"}),
		route("R09B", "/navigation/b", "client navigation target route", "navigation", []string{"navigation", "dispose"}, "none", "core", []string{"data-gosx-link", "same-document navigation", "island dispose/remount evidence"}),
		{
			ID:                    "R10",
			Route:                 "/demos/water",
			FixtureApp:            "examples/gosx-docs",
			Purpose:               "flagship heavy Scene3D route",
			ExpectedRuntime:       "water scene3d",
			ExpectedCapabilities:  []string{"scene3d", "webgpu", "webgl", "water"},
			ExpectedTinyGoCurrent: "runtime",
			ExpectedTinyGoFuture:  "engine",
			ServerBuildMode:       "dev,production",
			RequiredInteractions:  []string{"load water route", "profile with scripts/water-profile-evidence.mjs"},
			RequiredScreenshots:   sceneScreenshots(),
			RoutePlanAssertions:   []string{"external reference only"},
			DisallowedAssets:      []string{"fixture-local-copy"},
			External:              true,
			Notes:                 "The heavy water route stays external to this fixture.",
		},
	},
}

func route(id, routePath, purpose, expectedRuntime string, caps []string, tinyCurrent, tinyFuture string, assertions []string) RouteRecord {
	return RouteRecord{
		ID:                    id,
		Route:                 routePath,
		FixtureApp:            fixtureApp,
		Purpose:               purpose,
		ExpectedRuntime:       expectedRuntime,
		ExpectedCapabilities:  caps,
		ExpectedTinyGoCurrent: tinyCurrent,
		ExpectedTinyGoFuture:  tinyFuture,
		ServerBuildMode:       "dev,production",
		RequiredInteractions:  []string{"GET " + routePath},
		RequiredScreenshots:   []string{"desktop"},
		RoutePlanAssertions:   assertions,
		DisallowedAssets:      []string{"app-side-js", "app-side-ts", "package-json", "bundler-config"},
	}
}

func sceneRoute(id, routePath, purpose, expectedRuntime string, caps []string, tinyCurrent, tinyFuture string, assertions []string) RouteRecord {
	record := route(id, routePath, purpose, expectedRuntime, caps, tinyCurrent, tinyFuture, assertions)
	record.RequiredScreenshots = sceneScreenshots()
	return record
}

func sceneScreenshots() []string {
	return []string{"webgpu-initial", "webgpu-settled", "webgl-initial", "webgl-settled"}
}
