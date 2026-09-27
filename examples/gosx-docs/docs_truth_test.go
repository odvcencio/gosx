package main

import (
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
)

var sampleReference = regexp.MustCompile(`[A-Za-z]+\.DocSample\("([^"]+\.sample)"\)`)
var sampleBinding = regexp.MustCompile(`\bdata\.(sample[0-9]{3})\b`)
var sampleLoader = regexp.MustCompile(`"(sample[0-9]{3})"\s*:\s*docsapp\.DocSample\("([^"]+\.sample)"\)`)

func TestAPIDocsUseCurrentPublicSurfaces(t *testing.T) {
	_, thisFile, _, _ := runtime.Caller(0)
	root := filepath.Join(filepath.Dir(thisFile), "app", "docs")

	tests := []struct {
		page      string
		required  []string
		forbidden []string
	}{
		{
			page:     "engines",
			required: []string{"engine.Config", "RequiredCapabilities", "RuntimeGoWASM", "wasm.Register"},
			forbidden: []string{
				"engine.New(",
				"engine.Tier",
				"NewByName",
			},
		},
		{
			page:     "signals",
			required: []string{"signal.Derive", "signal.Watch", "effect.Dispose", "NewWithEqual"},
			forbidden: []string{
				"signal.Computed(",
				"signal.Effect(",
				".Peek()",
				".ReadOnly()",
			},
		},
		{
			page:     "hubs",
			required: []string{"ctx.Data", "ctx.Hub.Broadcast", "doc.Put", "GenerateSyncMessage", "SetBinaryAuthorizer"},
			forbidden: []string{
				"ctx.Payload",
				"ctx.Broadcast(",
				"crdt.Put(",
				"crdt.Apply(",
			},
		},
		{
			page:     "auth",
			required: []string{"authn.Require(adminHandler)", "RequireRole", "BaseURL", "Origin"},
			forbidden: []string{
				"authn.Require(\"admin\")",
				"GoSXWebAuthn",
			},
		},
		{
			page:     "routing",
			required: []string{"[...path]", "ctx.Param(\"path\")", "RegisterFileModuleHere", "route.config.json"},
			forbidden: []string{
				"__catch-all",
				"params[\"*\"]",
			},
		},
		{
			page:     "images",
			required: []string{"server.ImageProps", "server.ImageTransform", "server.ImageURL"},
			forbidden: []string{
				"ImageURLProps",
				"Format: \"webp\"",
			},
		},
		{
			page:     "text-layout",
			required: []string{"TextBlockModeNative", "ApproximateMeasurer", "WhiteSpacePreWrap"},
			forbidden: []string{
				"WhiteSpace: \"normal\"",
			},
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.page, func(t *testing.T) {
			body := readDocsPagePair(t, root, test.page)
			for _, required := range test.required {
				if !strings.Contains(body, required) {
					t.Errorf("docs/%s is missing current API %q", test.page, required)
				}
			}
			for _, forbidden := range test.forbidden {
				if strings.Contains(body, forbidden) {
					t.Errorf("docs/%s retains stale API %q", test.page, forbidden)
				}
			}
		})
	}
}

func TestChangedDocsPagesEmbedTheirExamples(t *testing.T) {
	_, thisFile, _, _ := runtime.Caller(0)
	root := filepath.Join(filepath.Dir(thisFile), "app", "docs")
	tests := []struct {
		page     string
		required string
	}{
		{"auth", "auth/sessionSample.go.sample"},
		{"compiler", "compiler/strictSample.gosx.sample"},
		{"components", "components/strictSample.gosx.sample"},
		{"debugging-scene3d", "debugging-scene3d/code-001.bash.sample"},
		{"deployment", "deployment/sampleBuildModes.bash.sample"},
		{"engines", "engines/mountSample.go.sample"},
		{"engines", "engines/liveConfig.go.sample"},
		{"forms", "forms/code-001.gsx.sample"},
		{"getting-started", "getting-started/quickstart.bash.sample"},
		{"hubs", "hubs/hubSample.go.sample"},
		{"images", "images/imageSample.go.sample"},
		{"islands", "islands/counterSample.gosx.sample"},
		{"islands", "islands/liveCounter.gosx.sample"},
		{"motion", "motion/motionSample.go.sample"},
		{"routing", "routing/treeSample.text.sample"},
		{"runtime", "runtime/code-001.gosx.sample"},
		{"scene3d", "scene3d/code-001.go.sample"},
		{"signals", "signals/basicSample.go.sample"},
		{"signals", "signals/liveExample.gosx.sample"},
		{"streaming", "streaming/deferSample.go.sample"},
		{"text-layout", "text-layout/blockSample.go.sample"},
		{"your-first-app", "tutorial/step-01-page-server.go.sample"},
	}
	for _, test := range tests {
		test := test
		t.Run(test.page, func(t *testing.T) {
			body := readDocsPagePair(t, root, test.page)
			assertDocsContract(t, body, []string{test.required}, []string{"CodeBlock(\"go\", `"})
		})
	}
}

func TestGoSXPagesPassEmbeddedSamplesThroughTheirLoader(t *testing.T) {
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve docs test location")
	}
	root := filepath.Join(filepath.Dir(thisFile), "app", "docs")
	var pages []string
	if err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info != nil && !info.IsDir() && filepath.Base(path) == "page.gsx" {
			pages = append(pages, path)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	for _, page := range pages {
		gsx, err := os.ReadFile(page)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(gsx), "docsapp.DocSample(") {
			t.Errorf("%s calls DocSample from .gsx; bind the sample in page.server.go so production pages render it", page)
		}
		bindings := sampleBinding.FindAllStringSubmatch(string(gsx), -1)
		if len(bindings) == 0 {
			continue
		}
		serverPath := filepath.Join(filepath.Dir(page), "page.server.go")
		server, err := os.ReadFile(serverPath)
		if err != nil {
			t.Fatalf("read %s: %v", serverPath, err)
		}
		loaders := make(map[string]string)
		for _, match := range sampleLoader.FindAllStringSubmatch(string(server), -1) {
			loaders[match[1]] = match[2]
		}
		used := make(map[string]bool)
		for _, binding := range bindings {
			field := binding[1]
			used[field] = true
			if _, ok := loaders[field]; !ok {
				t.Errorf("%s reads data.%s without a matching page.server.go sample loader", page, field)
			}
		}
		for field := range loaders {
			if !used[field] {
				t.Errorf("%s loads %s without a matching .gsx sample binding", serverPath, field)
			}
		}
	}
}

func TestDocsActiveNavigationContrastUsesDarkTextOnGold(t *testing.T) {
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve docs test location")
	}
	path := filepath.Join(filepath.Dir(thisFile), "app", "docs", "layout.css")
	css, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	lightRule := regexp.MustCompile(`(?s)\.docs-section\.light \.docs-guide-link\.is-current\s*\{([^}]+)\}`)
	match := lightRule.FindSubmatch(css)
	if len(match) != 2 {
		t.Fatal("light-theme active guide navigation rule is missing")
	}
	rule := string(match[1])
	for _, required := range []string{"color: #17140b;", "background: var(--accent);"} {
		if !strings.Contains(rule, required) {
			t.Errorf("active guide navigation rule is missing %q", required)
		}
	}
	if strings.Contains(rule, "color: #ffffff;") || strings.Contains(rule, "background: var(--accent-deep);") {
		t.Fatal("active guide navigation reverted to low-contrast white on gold")
	}
}

func TestChangedGuidesShowTheirWorkingExampleAndCurrentContract(t *testing.T) {
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve docs test location")
	}
	root := filepath.Join(filepath.Dir(thisFile), "app", "docs")
	tests := []struct {
		page      string
		required  []string
		forbidden []string
	}{
		{page: "getting-started", required: []string{"GoSX is a Go framework for server-rendered web apps.", "gosx init my-app", "quickstart-app.jpg", "75 seconds"}, forbidden: []string{"doc-scene", "remains necessary today only for loader-bound routes, islands, and engines", "GoSX is a Go framework for server-rendered pages, interactive islands, realtime hubs, and typed 3D scenes."}},
		{page: "your-first-app", required: []string{"Step 1 · Server data", "Step 2 · Island", "Step 3 · Hub", "shared signal updates the count", "Step 4 · Scene3D", "step-04.jpg"}, forbidden: []string{"doc-scene", "three.js", "refresh binding reruns"}},
		{page: "compiler", required: []string{"CompilerExample", "Typed component", "Compiled output"}, forbidden: []string{"doc-scene", "Calls stay within one declaration style in v0.39"}},
		{page: "components", required: []string{"Working typed component", "/docs/typed-live", "View the example source"}, forbidden: []string{"doc-scene"}},
		{page: "deployment", required: []string{"data.buildInfo.frameworkVersion", "/api/site", "docs-live-example"}, forbidden: []string{"doc-scene"}},
		{page: "auth", required: []string{"Live session-backed action", "View the session action source"}, forbidden: []string{"doc-scene"}},
		{page: "forms", required: []string{`actionPath("subscribe")`, "actions.subscribe.fieldErrors.email", "ctx.ValidationFailure"}, forbidden: []string{"doc-scene"}},
		{page: "hubs", required: []string{"ExampleHub", "docs-guide-presence", "ctx.Hub.Broadcast", "data.openTabs", "data-gosx-region-signal", "$docs.guidePresence"}, forbidden: []string{"doc-scene", "Refresh: true"}},
		{page: "images", required: []string{"data.liveImage", "server.Image", "View the image helper source"}, forbidden: []string{"doc-scene"}},
		{page: "islands", required: []string{"LiveCounter", "signal.New(props.Initial)", "data.liveCounterProps"}, forbidden: []string{"doc-scene", "Strict islands are not supported yet"}},
		{page: "motion", required: []string{"ctx.Runtime().Motion", "MotionPresetSlideUp", "motionExample"}, forbidden: []string{"doc-scene"}},
		{page: "routing", required: []string{"routing/examples/hello-world"}, forbidden: []string{"doc-scene", "not part of v0.39"}},
		{page: "runtime", required: []string{`data-gosx-link="true"`, `data-gosx-prefetch="render"`}, forbidden: []string{"doc-scene"}},
		{page: "signals", required: []string{"ReactiveExample", "signal.Derive", "doubled.Get()"}, forbidden: []string{"doc-scene"}},
		{page: "streaming", required: []string{"Live deferred response", "ctx.DeferWithOptions", "streamDemo"}, forbidden: []string{"doc-scene"}},
		{page: "text-layout", required: []string{"textLayoutExample", "TextBlockProps", "View the Go TextBlock source"}, forbidden: []string{"doc-scene"}},
		{page: "typed-live", required: []string{"strict typed component", "Read this route's source"}, forbidden: []string{"v0.39 strict component"}},
	}
	for _, test := range tests {
		test := test
		t.Run(test.page, func(t *testing.T) {
			body := readDocsPagePair(t, root, test.page)
			assertDocsContract(t, body, test.required, test.forbidden)
		})
	}

	routingExample := filepath.Join(root, "routing", "examples", "[slug]")
	page, err := os.ReadFile(filepath.Join(routingExample, "page.gsx"))
	if err != nil {
		t.Fatal(err)
	}
	assertDocsContract(t, string(page), []string{"params.slug"}, nil)
}

func TestRuntimeDeploymentSceneAndRelayDocsUseCurrentContracts(t *testing.T) {
	_, thisFile, _, _ := runtime.Caller(0)
	docsRoot := filepath.Join(filepath.Dir(thisFile), "app", "docs")

	tests := []struct {
		page      string
		required  []string
		forbidden []string
	}{
		{
			page:     "getting-started",
			required: []string{"gosx init my-app", "Go 1.26", "75 seconds", "quickstart-app.jpg", "quickstart.bash.sample"},
			forbidden: []string{
				"gosx --version",
				"produces a deployable binary with everything included",
				"It remains necessary today only for loader-bound routes, islands, and engines.",
				"doc-scene",
			},
		},
		{
			page:      "your-first-app",
			required:  []string{"Step 1 · Server data", "Step 2 · Island", "step-02-counter-props.go.sample", "step-02-page-server.go.sample", "Step 3 · Hub", "Step 4 · Scene3D", "step-04-page-server.go.sample", "step-04.jpg"},
			forbidden: []string{"doc-scene", "three.js"},
		},
		{
			page: "components",
			required: []string{
				"A strict component places the markup its caller wrote",
				"Children are not a prop",
				// The three component categories gosx#240 introduced. The
				// page has to name them, or it contradicts README.md.
				"typed legacy",
				"untyped legacy",
				"A strict component may hydrate client-side as an island",
				"gosx export .",
			},
			forbidden: []string{
				// Children shipped. A doc that still says they are rejected
				// would be worse than no doc at all.
				"Positional child content stays rejected either way",
				// gosx#240 replaced the two-category rule this sentence
				// stated. A typed legacy component now takes part in strict
				// calls in both directions.
				"v0.39 keeps component calls within the same style",
				"Strict islands are not supported yet",
				"Islands, engines, and loader-bound routes still need it",
			},
		},
		{
			page: "runtime",
			required: []string{
				"window.__gosx.navigation.navigate",
				"data-gosx-prefetch=\"render\"",
				"gosx-page-cache",
				"ManagedScriptRoleManaged",
			},
			forbidden: []string{
				"window.__gosx_page_nav",
				"window.__gosx_dispose_page",
				"window.__gosx_bootstrap_page",
				"data-gosx-lifecycle-script",
				"export function dispose",
				"300 ms",
			},
		},
		{
			page: "deployment",
			required: []string{
				"dist/server/app",
				"dist/edge/worker.js",
				"GOSX_ORIGIN",
				"redis.NewISRStore",
				"gosx build --prod --offline .",
			},
			forbidden: []string{
				"--target edge",
				"--out ./edge",
				"_gosx/css",
				"ctx.NoCache()",
				"wasi_snapshot_preview1",
				"No external files are required",
			},
		},
		{
			page: "debugging-scene3d",
			required: []string{
				"gosx scene check --strict",
				"gosx scene inspect --json --strict",
				"gosx scene validate --strict",
			},
			forbidden: []string{"gosx scene certify", "--cert"},
		},
		{
			page: "scene3d",
			required: []string{
				"RequiredCapabilities",
				"scene.RequireWebGPU",
				"environment-map",
				"Prepared split-sum IBL is supported on both GPU backends",
			},
			forbidden: []string{
				"environment map degrades on WebGPU",
				"Dashed lines draw on WebGL2 only",
				"151,301 raw bytes",
			},
		},
	}

	for _, test := range tests {
		t.Run(test.page, func(t *testing.T) {
			body := readDocsPagePair(t, docsRoot, test.page)
			assertDocsContract(t, body, test.required, test.forbidden)
		})
	}

	repoRoot := filepath.Clean(filepath.Join(filepath.Dir(thisFile), "..", ".."))
	relayPath := filepath.Join(repoRoot, "docs", "cross-frame-signals.md")
	relay, err := os.ReadFile(relayPath)
	if err != nil {
		t.Fatalf("read %s: %v", relayPath, err)
	}
	assertDocsContract(t, string(relay), []string{
		"window.__gosx.relay.configure",
		"window.__gosx.relay.registerPeer",
		"window.__gosx.relay.send",
		"window.__gosx.relay.flushInboundBuffer",
		"window.__gosx.host.relay.flushInbound",
	}, []string{
		"window.__gosx_relay_configure",
		"window.__gosx_relay_register_peer",
		"window.__gosx_relay_send",
		"window.__gosx_relay_flush_inbound",
		"~150 KB",
	})
}

func assertDocsContract(t *testing.T, body string, required, forbidden []string) {
	t.Helper()
	for _, value := range required {
		if !strings.Contains(body, value) {
			t.Errorf("documentation is missing current contract %q", value)
		}
	}
	for _, value := range forbidden {
		if strings.Contains(body, value) {
			t.Errorf("documentation retains stale contract %q", value)
		}
	}
}

func readDocsPagePair(t *testing.T, root, page string) string {
	t.Helper()
	var joined strings.Builder
	for _, name := range []string{"page.gsx", "page.server.go"} {
		body, err := os.ReadFile(filepath.Join(root, page, name))
		if err != nil {
			t.Fatalf("read docs/%s/%s: %v", page, name, err)
		}
		joined.Write(body)
		joined.WriteByte('\n')
		for _, match := range sampleReference.FindAllSubmatch(body, -1) {
			samplePath := filepath.Join(filepath.Dir(root), "..", "samples", filepath.FromSlash(string(match[1])))
			sample, err := os.ReadFile(samplePath)
			if err != nil {
				t.Fatalf("read documentation sample %s: %v", samplePath, err)
			}
			joined.Write(sample)
			joined.WriteByte('\n')
		}
	}
	return joined.String()
}
