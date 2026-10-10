package island

import "m31labs.dev/gosx/buildmanifest"

// NewPerfFixtureRenderer creates a renderer for an explicit production fixture.
// Compatibility modes reproduce missing configuration; they preserve the full
// asset graph and never change the supplied build manifest or global preview.
func NewPerfFixtureRenderer(manifest *buildmanifest.Manifest, compatibility string) (*Renderer, error) {
	if manifest == nil || manifest.PerfAssetUses == nil {
		return nil, &buildmanifest.PerfAssetError{Code: "invalid-input", Pointer: "/fixture/manifest"}
	}
	switch compatibility {
	case "configured", "preview", "lite-missing", "selective-missing", "full-unconfigured":
	default:
		return nil, &buildmanifest.PerfAssetError{Code: "invalid-input", Pointer: "/fixture/compatibility"}
	}
	r := NewRenderer("budget-fixture")
	if err := r.ApplyBuildManifest(manifest, "/gosx/assets"); err != nil {
		return nil, err
	}
	switch compatibility {
	case "preview":
		r.fixturePreview = true
	case "lite-missing":
		r.bootstrapLitePath = ""
	case "selective-missing":
		r.bootstrapRuntimePath = ""
	case "full-unconfigured":
		r.bootstrapRuntimePath, r.islandRuntime.Path = "", ""
		r.runtimeVariants = nil
	}
	return r, nil
}
