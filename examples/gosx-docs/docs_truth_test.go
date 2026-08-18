package main

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

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
			required: []string{"gosx version"},
			forbidden: []string{
				"gosx --version",
				"produces a deployable binary with everything included",
			},
		},
		{
			page: "components",
			required: []string{
				"A strict component places the markup its caller wrote",
				"Children are not a prop",
				"gosx export .",
			},
			forbidden: []string{
				// Children shipped. A doc that still says they are rejected
				// would be worse than no doc at all.
				"Positional child content stays rejected either way",
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
				"Prepared split-sum IBL is faithful on WebGPU",
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
	}
	return joined.String()
}
