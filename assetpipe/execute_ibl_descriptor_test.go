package assetpipe

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"m31labs.dev/gosx/assetpipe/ibl"
	"m31labs.dev/gosx/render/bundle/ktx2"
	"m31labs.dev/gosx/scene"
)

func TestIBLProductsPublishBackendConsumableSemantics(t *testing.T) {
	dir := t.TempDir()
	mustWriteBytes(t, filepath.Join(dir, "env", "studio.hdr"), writeTestHDR(t, 16, 8, 2, 1, 0.5))

	report, err := Plan([]string{dir}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	executed, _, err := Execute(report, ExecuteOptions{
		Root: dir,
		IBL: IBLOptions{
			CubeSize:       8,
			Samples:        8,
			IrradianceSize: 4,
			BRDFLUTSize:    8,
			BRDFSamples:    16,
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	sidecarBytes, err := os.ReadFile(filepath.Join(dir, "env", "studio.ibl.json"))
	if err != nil {
		t.Fatal(err)
	}
	var sidecar iblSidecar
	if err := json.Unmarshal(sidecarBytes, &sidecar); err != nil {
		t.Fatal(err)
	}
	if sidecar.IBL.SchemaVersion != ibl.DescriptorSchemaVersion {
		t.Fatalf("IBL descriptor schema = %d, want %d", sidecar.IBL.SchemaVersion, ibl.DescriptorSchemaVersion)
	}
	assertIBLProduct(t, sidecar.IBL.Radiance, "env/studio.ibl.ktx2", ibl.ProductRoleRadiance, "linear", "rgba", "cube", "rgba16f", 4)
	assertIBLProduct(t, sidecar.IBL.Irradiance, "env/studio.irradiance.ktx2", ibl.ProductRoleIrradiance, "linear", "rgba", "cube", "rgba16f", 1)
	assertIBLProduct(t, sidecar.IBL.BRDFLUT, "env/studio.brdf-lut.ktx2", ibl.ProductRoleBRDFLUT, "linear", "rg", "2d", "rg16f", 1)
	if sidecar.IBL.BRDFModel != ibl.BRDFModel ||
		len(sidecar.IBL.RoughnessPerLevel) != sidecar.SpecularMipLevels ||
		len(sidecar.IBL.SphericalHarmonics) != 9 {
		t.Fatalf("IBL convention metadata is incomplete: %#v", sidecar.IBL)
	}

	// The sidecar block and SceneIR deliberately share one JSON shape. Prove
	// that a build product can cross that package seam without translation.
	var sidecarWire struct {
		IBL json.RawMessage `json:"ibl"`
	}
	if err := json.Unmarshal(sidecarBytes, &sidecarWire); err != nil {
		t.Fatal(err)
	}
	var sceneIBL scene.EnvironmentIBL
	if err := json.Unmarshal(sidecarWire.IBL, &sceneIBL); err != nil {
		t.Fatalf("unmarshal assetpipe IBL descriptor into SceneIR contract: %v", err)
	}
	sceneIR := (scene.Props{Environment: scene.Environment{IBL: sceneIBL}}).SceneIR()
	if sceneIR.Environment.IBL.SchemaVersion != ibl.DescriptorSchemaVersion ||
		sceneIR.Environment.IBL.Radiance.Role != scene.TextureRoleEnvironmentRadiance ||
		sceneIR.Environment.IBL.BRDFLUT.ColorSpace != scene.TextureColorSpaceLinear {
		t.Fatalf("assetpipe -> SceneIR metadata drift: %#v", sceneIR.Environment.IBL)
	}

	manifest := BuildVariantManifest(executed)
	if manifest.SchemaVersion != 2 {
		t.Fatalf("variant manifest schema = %d, want 2", manifest.SchemaVersion)
	}
	var studio *ManifestAsset
	for i := range manifest.Assets {
		if manifest.Assets[i].Path == "env/studio.hdr" {
			studio = &manifest.Assets[i]
			break
		}
	}
	if studio == nil {
		t.Fatalf("manifest lost studio environment: %#v", manifest.Assets)
	}
	if studio.MetadataURI != "env/studio.ibl.json" {
		t.Fatalf("metadata URI = %q", studio.MetadataURI)
	}
	roles := map[string]ManifestVariant{}
	for _, variant := range studio.Variants {
		roles[variant.Role] = variant
	}
	for role, want := range map[string]struct {
		colorSpace string
		channels   string
		view       string
		format     string
	}{
		string(ibl.ProductRoleRadiance):   {"linear", "rgba", "cube", "rgba16f"},
		string(ibl.ProductRoleIrradiance): {"linear", "rgba", "cube", "rgba16f"},
		string(ibl.ProductRoleBRDFLUT):    {"linear", "rg", "2d", "rg16f"},
	} {
		got, ok := roles[role]
		if !ok {
			t.Fatalf("manifest has no %s product: %#v", role, studio.Variants)
		}
		if got.ColorSpace != want.colorSpace || got.Channels != want.channels || got.View != want.view || got.Format != want.format || got.MipLevels < 1 {
			t.Fatalf("manifest %s descriptor = %#v", role, got)
		}
	}

	for name, role := range map[string]ibl.ProductRole{
		"studio.ibl.ktx2":        ibl.ProductRoleRadiance,
		"studio.irradiance.ktx2": ibl.ProductRoleIrradiance,
		"studio.brdf-lut.ktx2":   ibl.ProductRoleBRDFLUT,
	} {
		data, err := os.ReadFile(filepath.Join(dir, "env", name))
		if err != nil {
			t.Fatal(err)
		}
		keys, err := ktx2.KeyValues(data)
		if err != nil {
			t.Fatal(err)
		}
		if keys["GoSXiblRole"] != string(role) || keys["GoSXColorSpace"] != "linear" {
			t.Fatalf("%s metadata = %#v", name, keys)
		}
	}
}

func assertIBLProduct(t *testing.T, got ibl.ProductDescriptor, uri string, role ibl.ProductRole, colorSpace, channels, view, format string, mipLevels int) {
	t.Helper()
	if got.URI != uri || got.Role != role || got.ColorSpace != colorSpace || got.Channels != channels ||
		got.View != view || got.Format != format || got.MipLevels != mipLevels {
		t.Fatalf("IBL product = %#v", got)
	}
}
