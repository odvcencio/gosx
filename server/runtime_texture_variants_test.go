package server

import (
	"strings"
	"testing"

	"m31labs.dev/gosx/assetpipe"
	"m31labs.dev/gosx/hydrate"
)

func TestPageRuntimePublishesTextureVariants(t *testing.T) {
	runtime := NewPageRuntime()
	runtime.BindHub("test", "/ws", nil)
	runtime.SetTextureVariants(assetpipe.VariantManifest{
		SchemaVersion: assetpipe.VariantManifestSchemaVersion,
		Generator:     "gosx assetpipe",
		Assets: []assetpipe.ManifestAsset{{
			Path: "public/tabletop/stone.png",
			Kind: "texture",
			Variants: []assetpipe.ManifestVariant{{
				URI:                  "/tabletop/stone.bc7.ktx2",
				Kind:                 "texture",
				Quality:              "low",
				RequiredCapabilities: []string{"ktx2", "bc7"},
			}},
		}},
	})

	if !runtime.Active() || !runtime.Summary().Manifest {
		t.Fatal("texture variants must be included in an active page manifest")
	}
	manifest, err := runtime.renderer.ManifestJSON()
	if err != nil {
		t.Fatalf("render manifest: %v", err)
	}
	for _, want := range []string{
		`"textureVariants"`,
		`"public/tabletop/stone.png"`,
		`"/tabletop/stone.bc7.ktx2"`,
		`"bc7"`,
	} {
		if !strings.Contains(manifest, want) {
			t.Errorf("manifest does not contain %s: %s", want, manifest)
		}
	}
}

func TestPageRuntimeBindsHubRoundTrip(t *testing.T) {
	runtime := NewPageRuntime()
	runtime.BindHubWithRoundTrip("tabletop", "/ws/tabletop", []hydrate.HubBinding{
		{Event: "scene:update", SceneMountID: "tabletop-scene", SceneCommands: true},
	}, hydrate.HubRoundTripConfig{Signal: "$tabletop.rtt", PingEvent: "room:ping", PongEvent: "room:pong", IntervalMS: 1000})
	if !runtime.Active() || !runtime.Summary().Manifest {
		t.Fatal("hub round-trip telemetry must activate a page manifest")
	}
	manifest, err := runtime.renderer.ManifestJSON()
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"roundTrip"`, `"$tabletop.rtt"`, `"room:pong"`} {
		if !strings.Contains(manifest, want) {
			t.Errorf("manifest does not contain %s: %s", want, manifest)
		}
	}
}
