//go:build js && wasm

package main

import (
	"fmt"
	"math"
	"syscall/js"

	"github.com/odvcencio/gosx/engine"
	"github.com/odvcencio/gosx/render/bundle"
	"github.com/odvcencio/gosx/render/gpu/jsgpu"
)

func identity() []float32 {
	return []float32{
		1, 0, 0, 0,
		0, 1, 0, 0,
		0, 0, 1, 0,
		0, 0, 0, 1,
	}
}

func rotateZ(angle float64) []float32 {
	c := float32(math.Cos(angle))
	s := float32(math.Sin(angle))
	return []float32{
		c, s, 0, 0,
		-s, c, 0, 0,
		0, 0, 1, 0,
		0, 0, 0, 1,
	}
}

func instanceTransform(angle float64) []float64 {
	c := math.Cos(angle)
	s := math.Sin(angle)
	return []float64{
		c, 0, -s, 0,
		0, 1, 0, 0,
		s, 0, c, 0,
		0, 0, 0, 1,
	}
}

func skinnedCubeInputs(vertexCount int) ([]uint32, []float64) {
	joints := make([]uint32, vertexCount*4)
	weights := make([]float64, vertexCount*4)
	for i := 0; i < vertexCount; i++ {
		joints[i*4] = 1
		weights[i*4] = 1
	}
	return joints, weights
}

func scene(t float64) engine.RenderBundle {
	const vertexCount = 36
	joints, weights := skinnedCubeInputs(vertexCount)
	return engine.RenderBundle{
		Background: "#07111f",
		Camera: engine.RenderCamera{
			Y: 2.2, Z: 6,
			RotationX: -0.25,
			FOV:       math.Pi / 3,
			Near:      0.1,
			Far:       80,
		},
		Environment: engine.RenderEnvironment{
			SkyColor:         "#b9d4ff",
			SkyIntensity:     1.0,
			GroundColor:      "#28313d",
			GroundIntensity:  0.8,
			AmbientColor:     "#9fb3cc",
			AmbientIntensity: 0.25,
		},
		Lights: []engine.RenderLight{{
			Kind:       "directional",
			Color:      "#fff3d0",
			Intensity:  1.2,
			DirectionX: -0.45,
			DirectionY: -1,
			DirectionZ: -0.3,
			CastShadow: true,
		}},
		Materials: []engine.RenderMaterial{{
			Color:     "#75d7ff",
			Metalness: 0.05,
			Roughness: 0.35,
			Emissive:  0.04,
		}},
		InstancedMeshes: []engine.RenderInstancedMesh{{
			ID:            "rigged-cube",
			Kind:          "cube",
			MaterialIndex: 0,
			VertexCount:   vertexCount,
			InstanceCount: 1,
			Transforms:    instanceTransform(t * 0.35),
			SkinID:        "demo-rig",
			JointIndices:  joints,
			Weights:       weights,
			CastShadow:    true,
			ReceiveShadow: true,
		}},
		PostEffects: []engine.RenderPostEffect{{
			Kind:      "bloom",
			Threshold: 0.9,
			Intensity: 0.12,
			Radius:    5,
			Scale:     0.5,
		}},
	}
}

func run(canvasID string) error {
	device, surface, err := jsgpu.Open(canvasID)
	if err != nil {
		return fmt.Errorf("open gpu: %w", err)
	}
	renderer, err := bundle.New(bundle.Config{Device: device, Surface: surface})
	if err != nil {
		return fmt.Errorf("new renderer: %w", err)
	}
	palette, err := renderer.CreateBonePalette(2)
	if err != nil {
		return err
	}
	if err := renderer.RegisterBonePalette("demo-rig", palette); err != nil {
		return err
	}

	canvas := js.Global().Get("document").Call("getElementById", canvasID)
	start := js.Global().Get("performance").Call("now").Float()

	var raf js.Func
	raf = js.FuncOf(func(this js.Value, args []js.Value) any {
		now := js.Global().Get("performance").Call("now").Float()
		if len(args) > 0 && args[0].Type() == js.TypeNumber {
			now = args[0].Float()
		}
		t := (now - start) / 1000
		mats := append(identity(), rotateZ(math.Sin(t*2.0)*0.75)...)
		if err := renderer.UploadBonePalette(palette, mats); err != nil {
			js.Global().Get("console").Call("error", err.Error())
			return nil
		}
		if err := renderer.Frame(scene(t), canvas.Get("width").Int(), canvas.Get("height").Int(), t); err != nil {
			js.Global().Get("console").Call("error", err.Error())
			return nil
		}
		js.Global().Call("requestAnimationFrame", raf)
		return nil
	})
	js.Global().Call("requestAnimationFrame", raf)
	return nil
}

func main() {
	js.Global().Set("gosxSkinnedGLBSpikeStart", js.FuncOf(func(this js.Value, args []js.Value) any {
		if len(args) == 0 {
			return js.ValueOf(map[string]any{"ok": false, "error": "canvas id required"})
		}
		canvasID := args[0].String()
		promise := js.Global().Get("Promise")
		return promise.New(js.FuncOf(func(this js.Value, args []js.Value) any {
			resolve := args[0]
			reject := args[1]
			go func() {
				if err := run(canvasID); err != nil {
					reject.Invoke(js.ValueOf(err.Error()))
					return
				}
				resolve.Invoke(js.ValueOf(map[string]any{"ok": true}))
			}()
			return nil
		}))
	}))
	select {}
}
