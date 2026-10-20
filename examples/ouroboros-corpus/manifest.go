package main

type RouteRecord struct {
	ID                     string   `json:"id"`
	Route                  string   `json:"route"`
	FixtureApp             string   `json:"fixtureApp"`
	ExpectedCapabilities   []string `json:"expectedCapabilities"`
	ExpectedTinyGoCurrent  string   `json:"expectedTinyGoCurrent"`
	ExpectedTinyGoFuture   string   `json:"expectedTinyGoFuture"`
	ServerBuildMode        string   `json:"serverBuildMode"`
	RequiredInteractions   []string `json:"requiredInteractions"`
	RequiredScreenshots    []string `json:"requiredScreenshots"`
	RoutePlanAssertions    []string `json:"routePlanAssertions"`
	AllowedOptionalMetrics []string `json:"allowedOptionalMetrics,omitempty"`
	DisallowedAssets       []string `json:"disallowedRuntimeAssets"`
	External               bool     `json:"external,omitempty"`
	Notes                  string   `json:"notes,omitempty"`
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
		route("R00", "/static", []string{"server"}, "none", "none", []string{"no bootstrap", "no manifest"}),
		route("R01", "/lite", []string{"lite", "actions"}, "none", "core", []string{"bootstrap mode lite", "no wasm"}),
		route("R02", "/island/counter", []string{"island", "action"}, "full", "core", []string{"one island", "patch runtime", "valid action endpoint"}),
		route("R03", "/islands/kitchen", []string{"islands", "shared-signals"}, "full", "core", []string{"five islands", "shared signal program", "shared signal manifest entry"}),
		route("R04", "/action/form", []string{"action", "lite"}, "none", "core", []string{"declarative action marker", "bootstrap mode lite", "validation response", "redirect response"}),
		route("R05", "/canvas-board", []string{"canvas", "engine"}, "full", "engine", []string{"CanvasBoard marker", "canvas2d surface"}),
		route("R06", "/hub/echo", []string{"hub", "shared-signals"}, "none", "collab", []string{"hub manifest", "echo binding", "no wasm runtime path"}),
		route("R07", "/video-sync", []string{"video", "fetch", "audio", "local-media", "local-sync"}, "none", "engine", []string{"video engine", "follow sync props", "local media endpoint", "same-origin sync socket", "no wasm runtime path"}),
		sceneRoute("R08", "/scene/basic", []string{"canvas", "webgpu", "webgl", "animation"}, "none", "engine", []string{"Scene3D engine", "scene3d feature path", "WebGPU remains selectable", "no wasm runtime path"}),
		route("R09A", "/navigation/a", []string{"navigation", "island"}, "full", "core", []string{"data-gosx-link", "same-document navigation", "island dispose/remount evidence"}),
		route("R09B", "/navigation/b", []string{"navigation", "island"}, "full", "core", []string{"data-gosx-link", "same-document navigation", "island dispose/remount evidence"}),
		{
			ID:                    "R10",
			Route:                 "examples/gosx-docs:/demos/water",
			FixtureApp:            "examples/gosx-docs",
			ExpectedCapabilities:  []string{"scene3d", "webgpu", "webgl", "water"},
			ExpectedTinyGoCurrent: "full",
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

func route(id, routePath string, caps []string, tinyCurrent, tinyFuture string, assertions []string) RouteRecord {
	return RouteRecord{
		ID:                    id,
		Route:                 routePath,
		FixtureApp:            fixtureApp,
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

func sceneRoute(id, routePath string, caps []string, tinyCurrent, tinyFuture string, assertions []string) RouteRecord {
	record := route(id, routePath, caps, tinyCurrent, tinyFuture, assertions)
	record.RequiredScreenshots = sceneScreenshots()
	return record
}

func sceneScreenshots() []string {
	return []string{"webgpu-initial", "webgpu-settled", "webgl-initial", "webgl-settled"}
}
