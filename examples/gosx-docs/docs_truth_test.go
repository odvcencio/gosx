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
