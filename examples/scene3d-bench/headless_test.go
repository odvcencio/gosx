//go:build !js || !wasm

package main

import (
	"crypto/sha256"
	"math"
	"testing"

	"github.com/odvcencio/gosx/engine"
	"github.com/odvcencio/gosx/render/bundle"
	"github.com/odvcencio/gosx/render/gpu/headless"
)

func TestScene3DBenchHeadlessFrameIsDeterministic(t *testing.T) {
	firstHash, firstDistinct := renderHeadlessBenchFrame(t)
	secondHash, secondDistinct := renderHeadlessBenchFrame(t)
	if firstHash != secondHash {
		t.Fatalf("headless scene3d bench frame hash mismatch: %x vs %x", firstHash, secondHash)
	}
	if firstDistinct < 32 || secondDistinct < 32 {
		t.Fatalf("expected bench frame to cover many pixels, got %d and %d", firstDistinct, secondDistinct)
	}
}

func renderHeadlessBenchFrame(t *testing.T) ([32]byte, int) {
	t.Helper()
	d, surface := headless.New(64, 64)
	r, err := bundle.New(bundle.Config{Device: d, Surface: surface})
	if err != nil {
		t.Fatalf("bundle.New: %v", err)
	}
	defer r.Destroy()

	if err := r.Frame(headlessBenchBundle(1.25), 64, 64, 1.25); err != nil {
		t.Fatalf("Frame: %v", err)
	}
	img := d.Framebuffer()
	bg := img.RGBAAt(0, 0)
	distinct := 0
	for y := 0; y < img.Bounds().Dy(); y++ {
		for x := 0; x < img.Bounds().Dx(); x++ {
			if img.RGBAAt(x, y) != bg {
				distinct++
			}
		}
	}
	return sha256.Sum256(img.Pix), distinct
}

func headlessBenchBundle(t float64) engine.RenderBundle {
	lightAngle := t * 0.2
	ldx := math.Cos(lightAngle)
	ldy := -1.4
	ldz := math.Sin(lightAngle) * 0.5
	invLen := 1 / math.Sqrt(ldx*ldx+ldy*ldy+ldz*ldz)
	ldx *= invLen
	ldy *= invLen
	ldz *= invLen

	return engine.RenderBundle{
		Background: "#06101f",
		Camera: engine.RenderCamera{
			Z:    22,
			FOV:  math.Pi / 3,
			Near: 0.3,
			Far:  120,
		},
		Environment: engine.RenderEnvironment{
			SkyColor:         "#a6c7ff",
			SkyIntensity:     1,
			GroundColor:      "#2a2420",
			GroundIntensity:  1,
			AmbientColor:     "#8090a0",
			AmbientIntensity: 0.35,
		},
		Lights: []engine.RenderLight{{
			Kind:       "directional",
			Color:      "#fff0c8",
			Intensity:  1.1,
			DirectionX: ldx,
			DirectionY: ldy,
			DirectionZ: ldz,
			CastShadow: true,
		}},
		Materials: []engine.RenderMaterial{{
			Color:     "#b8d7ff",
			Roughness: 0.55,
		}},
		InstancedMeshes: []engine.RenderInstancedMesh{
			{
				Kind:          "plane",
				VertexCount:   6,
				InstanceCount: 1,
				Transforms:    headlessBenchFloorTransform(),
				ReceiveShadow: true,
			},
			{
				Kind:          "cube",
				MaterialIndex: 0,
				VertexCount:   36,
				InstanceCount: 6 * 3 * 5,
				Transforms:    headlessBenchCubeTransforms(t),
				CastShadow:    true,
				ReceiveShadow: true,
			},
		},
	}
}

func headlessBenchCubeTransforms(t float64) []float64 {
	const (
		gridX    = 6
		gridY    = 3
		gridZ    = 5
		spacing  = 2.6
		cubeSize = 0.65
	)
	out := make([]float64, gridX*gridY*gridZ*16)
	i := 0
	for ix := 0; ix < gridX; ix++ {
		for iy := 0; iy < gridY; iy++ {
			for iz := 0; iz < gridZ; iz++ {
				x := (float64(ix) - float64(gridX-1)/2) * spacing
				y := (float64(iy) - float64(gridY-1)/2) * spacing
				z := (float64(iz) - float64(gridZ-1)/2) * spacing
				phase := t*0.5 + float64(ix+iy+iz)*0.11
				c := math.Cos(phase)
				s := math.Sin(phase)
				out[i*16+0] = c * cubeSize
				out[i*16+5] = cubeSize
				out[i*16+10] = c * cubeSize
				out[i*16+2] = -s * cubeSize
				out[i*16+8] = s * cubeSize
				out[i*16+12] = x
				out[i*16+13] = y
				out[i*16+14] = z
				out[i*16+15] = 1
				i++
			}
		}
	}
	return out
}

func headlessBenchFloorTransform() []float64 {
	const floorScale = 34.0
	return []float64{
		floorScale, 0, 0, 0,
		0, 1, 0, 0,
		0, 0, floorScale, 0,
		0, -7, 0, 1,
	}
}
