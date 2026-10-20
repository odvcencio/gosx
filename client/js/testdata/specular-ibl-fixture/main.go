// Command specular-ibl-fixture emits deterministic synthetic IBL KTX2
// fixtures for the browser specular F90 isolation test. It prints exactly
// one JSON object on stdout; errors go to stderr with a non-zero exit.
package main

import (
	"encoding/json"
	"fmt"
	"os"

	"m31labs.dev/gosx/assetpipe/ibl"
)

// radianceColor is the constant synthetic radiance texel. With F0 = 0 the
// specular response is B * F90, so the color must survive the bake exactly.
var radianceColor = ibl.Vec3{X: 0.75, Y: 0.875, Z: 1}

// payload is the JSON handoff: three base64-encoded KTX2 blobs plus the
// environment descriptor the browser test consumes.
type payload struct {
	Radiance   []byte                    `json:"radiance"`
	Irradiance []byte                    `json:"irradiance"`
	BRDFLUT    []byte                    `json:"brdfLUT"`
	Descriptor ibl.EnvironmentDescriptor `json:"descriptor"`
}

// buildFixture assembles the synthetic products. It performs no I/O and
// takes no arguments so the test can assert byte-for-byte determinism.
func buildFixture() (*payload, error) {
	// Radiance: constant RGB cube, mip chain 2x2 -> 1x1.
	radiance := make(ibl.Chain, 2)
	for level, size := range [...]int{2, 1} {
		cube := ibl.NewCube(size)
		for face := 0; face < 6; face++ {
			for y := 0; y < size; y++ {
				for x := 0; x < size; x++ {
					cube.Set(face, x, y, radianceColor)
				}
			}
		}
		radiance[level] = cube
	}

	// Irradiance: zero RGB cube at 1x1. Zero diffuse keeps only the
	// specular path alive in the shader under test.
	irradiance := ibl.Chain{ibl.NewCube(1)}

	// BRDF LUT: constant synthetic A/B. F = F0*A + B with F0 = 0 reduces
	// to B*F90, isolating the specular term under test.
	lut := &ibl.BRDFLUT{Size: 1, Data: []float32{0.5, 0.25}}

	meta := func(role ibl.ProductRole, model string) map[string]string {
		return map[string]string{
			"GoSXiblRole":    string(role),
			"GoSXColorSpace": "linear",
			"GoSXiblModel":   model,
		}
	}

	radianceKTX2, err := ibl.EncodeCubeKTX2(radiance, meta(ibl.ProductRoleRadiance, ibl.BRDFModel))
	if err != nil {
		return nil, fmt.Errorf("encode radiance: %w", err)
	}
	irradianceKTX2, err := ibl.EncodeCubeKTX2(irradiance, meta(ibl.ProductRoleIrradiance, "lambert-sh9"))
	if err != nil {
		return nil, fmt.Errorf("encode irradiance: %w", err)
	}
	lutKTX2, err := ibl.EncodeBRDFLUTKTX2(lut, meta(ibl.ProductRoleBRDFLUT, ibl.BRDFModel))
	if err != nil {
		return nil, fmt.Errorf("encode brdf lut: %w", err)
	}

	return &payload{
		Radiance:   radianceKTX2,
		Irradiance: irradianceKTX2,
		BRDFLUT:    lutKTX2,
		Descriptor: ibl.EnvironmentDescriptor{
			SchemaVersion: ibl.DescriptorSchemaVersion,
			Source:        "synthetic-specular-isolation",
			Radiance: ibl.ProductDescriptor{
				URI: "/ibl/spec-radiance.ktx2", Role: ibl.ProductRoleRadiance,
				ColorSpace: "linear", Channels: "rgba", View: "cube", Format: "rgba16f",
				MipLevels: 2, Width: 2, Height: 2, Faces: 6,
			},
			Irradiance: ibl.ProductDescriptor{
				URI: "/ibl/spec-irradiance.ktx2", Role: ibl.ProductRoleIrradiance,
				ColorSpace: "linear", Channels: "rgba", View: "cube", Format: "rgba16f",
				MipLevels: 1, Width: 1, Height: 1, Faces: 6,
			},
			BRDFLUT: ibl.ProductDescriptor{
				URI: "/ibl/spec-brdf-lut.ktx2", Role: ibl.ProductRoleBRDFLUT,
				ColorSpace: "linear", Channels: "rg", View: "2d", Format: "rg16f",
				MipLevels: 1, Width: 1, Height: 1, Faces: 1,
			},
			BRDFModel:         ibl.BRDFModel,
			RoughnessPerLevel: []float64{0, 1},
			SphericalHarmonics: [][3]float64{
				{0, 0, 0}, {0, 0, 0}, {0, 0, 0},
				{0, 0, 0}, {0, 0, 0}, {0, 0, 0},
				{0, 0, 0}, {0, 0, 0}, {0, 0, 0},
			},
		},
	}, nil
}

func main() {
	p, err := buildFixture()
	if err != nil {
		fmt.Fprintf(os.Stderr, "specular-ibl-fixture: %v\n", err)
		os.Exit(1)
	}
	enc := json.NewEncoder(os.Stdout)
	if err := enc.Encode(p); err != nil {
		fmt.Fprintf(os.Stderr, "specular-ibl-fixture: %v\n", err)
		os.Exit(1)
	}
}
