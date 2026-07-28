package scene

import (
	"encoding/json"
	"reflect"
	"testing"

	"m31labs.dev/gosx/scene/capability"
)

func TestSceneIRCarriesExplicitTextureColorRoles(t *testing.T) {
	props := Props{
		Graph: NewGraph(Mesh{
			ID:       "hero",
			Geometry: BoxGeometry{Width: 1, Height: 1, Depth: 1},
			Material: StandardMaterial{
				Texture:      "/maps/base.png",
				NormalMap:    "/maps/normal.png",
				RoughnessMap: "/maps/metal-rough.png",
				MetalnessMap: "/maps/metal-rough.png",
				OcclusionMap: "/maps/ao.png",
				EmissiveMap:  "/maps/emissive.png",
			},
		}),
	}

	compat := props.SceneIR()
	if len(compat.Objects) != 1 {
		t.Fatalf("objects = %d, want 1", len(compat.Objects))
	}
	got := compat.Objects[0].TextureDescriptors
	assertTextureDescriptor(t, got.BaseColor, "/maps/base.png", TextureRoleBaseColor, TextureColorSpaceSRGB, "rgba")
	assertTextureDescriptor(t, got.Normal, "/maps/normal.png", TextureRoleNormal, TextureColorSpaceLinear, "rgb")
	assertTextureDescriptor(t, got.Roughness, "/maps/metal-rough.png", TextureRoleRoughness, TextureColorSpaceLinear, "g")
	assertTextureDescriptor(t, got.Metalness, "/maps/metal-rough.png", TextureRoleMetalness, TextureColorSpaceLinear, "b")
	assertTextureDescriptor(t, got.Occlusion, "/maps/ao.png", TextureRoleAmbientOcclusion, TextureColorSpaceLinear, "r")
	assertTextureDescriptor(t, got.Emissive, "/maps/emissive.png", TextureRoleEmissive, TextureColorSpaceSRGB, "rgb")

	canonical := props.CanonicalIR()
	if len(canonical.Materials) != 1 {
		t.Fatalf("canonical materials = %d, want 1", len(canonical.Materials))
	}
	if !reflect.DeepEqual(canonical.Materials[0].TextureDescriptors, got) {
		t.Fatalf("canonical texture descriptors drifted:\n got %#v\nwant %#v", canonical.Materials[0].TextureDescriptors, got)
	}

	wire, err := json.Marshal(compat)
	if err != nil {
		t.Fatal(err)
	}
	var decoded struct {
		Objects []struct {
			TextureDescriptors MaterialTextureDescriptors `json:"textureDescriptors"`
		} `json:"objects"`
	}
	if err := json.Unmarshal(wire, &decoded); err != nil {
		t.Fatal(err)
	}
	if len(decoded.Objects) != 1 || decoded.Objects[0].TextureDescriptors.BaseColor.ColorSpace != TextureColorSpaceSRGB {
		t.Fatalf("wire lost texture role metadata: %s", wire)
	}
}

func TestSceneIRCarriesNormalizedHDRIBLProductsWithoutCapabilityClaim(t *testing.T) {
	props := Props{
		Environment: Environment{
			EnvironmentMap: "/env/studio.hdr",
			IBL: EnvironmentIBL{
				Source: "/env/studio.hdr",
				Radiance: TextureDescriptor{
					URI:       " /env/studio.ibl.ktx2 ",
					Format:    "rgba16f",
					MipLevels: 9,
				},
				Irradiance: TextureDescriptor{
					URI:    "/env/studio.irradiance.ktx2",
					Format: "rgba16f",
				},
				BRDFLUT: TextureDescriptor{
					URI:    "/env/studio.brdf-lut.ktx2",
					Format: "rg16f",
				},
				BRDFModel:         "ggx-test",
				RoughnessPerLevel: []float64{0, 0.5, 1},
				SphericalHarmonics: [][3]float64{
					{0.1, 0.2, 0.3},
				},
			},
		},
	}

	compat := props.SceneIR().Environment.IBL
	assertTextureDescriptor(t, compat.Radiance, "/env/studio.ibl.ktx2", TextureRoleEnvironmentRadiance, TextureColorSpaceLinear, "rgba")
	assertTextureDescriptor(t, compat.Irradiance, "/env/studio.irradiance.ktx2", TextureRoleEnvironmentIrradiance, TextureColorSpaceLinear, "rgba")
	assertTextureDescriptor(t, compat.BRDFLUT, "/env/studio.brdf-lut.ktx2", TextureRoleBRDFLUT, TextureColorSpaceLinear, "rg")
	if compat.Radiance.View != "cube" || compat.Irradiance.View != "cube" || compat.BRDFLUT.View != "2d" {
		t.Fatalf("unexpected IBL views: %#v", compat)
	}
	if compat.BRDFModel != "ggx-test" || len(compat.SphericalHarmonics) != 1 {
		t.Fatalf("IBL convention metadata was lost: %#v", compat)
	}

	canonical := props.CanonicalIR().Environment.IBL
	if canonical.Radiance.URI != compat.Radiance.URI || canonical.BRDFLUT.Role != TextureRoleBRDFLUT {
		t.Fatalf("canonical IBL contract drifted: %#v", canonical)
	}

	if capability.Supports(capability.BackendWebGPU, capability.FeatureIBL) ||
		capability.Supports(capability.BackendWebGL, capability.FeatureIBL) {
		t.Fatal("carrying IBL descriptors must not advertise renderer capability")
	}
}

func TestMaterialDataTexturesAreExplicitlyLinear(t *testing.T) {
	material := IRMaterial{
		TextureDescriptors: MaterialTextureDescriptors{
			Data: map[string]TextureDescriptor{
				"thickness": {
					URI:        "/maps/thickness.exr",
					Role:       TextureRoleData,
					ColorSpace: TextureColorSpaceLinear,
					Channels:   "r",
					View:       "2d",
				},
			},
		},
	}
	wire, err := json.Marshal(material)
	if err != nil {
		t.Fatal(err)
	}
	var decoded IRMaterial
	if err := json.Unmarshal(wire, &decoded); err != nil {
		t.Fatal(err)
	}
	got := decoded.TextureDescriptors.Data["thickness"]
	assertTextureDescriptor(t, got, "/maps/thickness.exr", TextureRoleData, TextureColorSpaceLinear, "r")
}

func assertTextureDescriptor(t *testing.T, got TextureDescriptor, uri string, role TextureRole, colorSpace TextureColorSpace, channels string) {
	t.Helper()
	if got.URI != uri || got.Role != role || got.ColorSpace != colorSpace || got.Channels != channels {
		t.Fatalf("texture descriptor = %#v, want uri=%q role=%q colorSpace=%q channels=%q", got, uri, role, colorSpace, channels)
	}
}
