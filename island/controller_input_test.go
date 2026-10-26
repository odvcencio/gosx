package island

import (
	"strings"
	"testing"

	"m31labs.dev/gosx/buildmanifest"
	"m31labs.dev/gosx/controller"
)

func TestControllerInputUsesHashedRuntimeAssetOnlyWhenNeeded(t *testing.T) {
	for _, kind := range []string{"ordinary", "projection", "storage", "workbench"} {
		r := NewRenderer("input")
		r.ApplyBuildManifest(&buildmanifest.Manifest{Runtime: buildmanifest.RuntimeAssets{
			BootstrapControllerInput: buildmanifest.HashedAsset{File: "bootstrap-controller-input.12345678.js", Hash: "12345678"},
		}}, "/cdn")
		config := controller.Config{Events: []controller.Event{{Type: "click", Output: "$click"}}}
		if kind == "projection" {
			config.Events[0].Project = &controller.Projection{}
		}
		if kind == "storage" {
			config.Storage = &controller.Storage{}
		}
		if kind == "workbench" {
			if err := r.RequireFeature("workbench"); err != nil {
				t.Fatal(err)
			}
		} else {
			r.RegisterController(config)
		}
		path := r.Summary().BootstrapControllerInputPath
		if kind != "ordinary" && !strings.Contains(path, "/cdn/runtime/bootstrap-controller-input.12345678.js") {
			t.Fatalf("input chunk path = %q", path)
		}
		if kind == "ordinary" && path != "" {
			t.Fatalf("ordinary controller exposes input chunk: %q", path)
		}
	}
}
