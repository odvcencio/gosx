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

func transform(scale, x, y, z, ry float64) []float64 {
	c := math.Cos(ry)
	s := math.Sin(ry)
	return []float64{
		c * scale, 0, -s * scale, 0,
		0, scale, 0, 0,
		s * scale, 0, c * scale, 0,
		x, y, z, 1,
	}
}

func frameBundle(t float64) engine.RenderBundle {
	return engine.RenderBundle{
		Background: "#05070d",
		Camera: engine.RenderCamera{
			Y: 2.4, Z: 7.5,
			RotationX: -0.26,
			FOV:       math.Pi / 3,
			Near:      0.1,
			Far:       80,
		},
		Environment: engine.RenderEnvironment{
			AmbientColor:     "#ffffff",
			AmbientIntensity: 0.16,
			SkyColor:         "#7fb6ff",
			SkyIntensity:     0.9,
			GroundColor:      "#161c24",
			GroundIntensity:  0.65,
			EnvMap:           "procedural://r5-ibl-studio",
			EnvIntensity:     0.85,
			EnvRotation:      t * 0.18,
		},
		Lights: []engine.RenderLight{{
			Kind:       "directional",
			Color:      "#ffe4b8",
			Intensity:  0.75,
			DirectionX: -0.35,
			DirectionY: -1.0,
			DirectionZ: -0.25,
			CastShadow: true,
		}},
		Materials: []engine.RenderMaterial{
			{Color: "#f7fbff", Metalness: 0.9, Roughness: 0.18},
			{Color: "#3e4652", Metalness: 0.05, Roughness: 0.75},
		},
		InstancedMeshes: []engine.RenderInstancedMesh{
			{
				ID:            "reflective-sphere",
				Kind:          "sphere",
				MaterialIndex: 0,
				VertexCount:   0,
				InstanceCount: 1,
				Transforms:    transform(1.4, 0, 0.55, 0, t*0.4),
				CastShadow:    true,
				ReceiveShadow: true,
			},
			{
				ID:            "matte-floor",
				Kind:          "plane",
				MaterialIndex: 1,
				VertexCount:   6,
				InstanceCount: 1,
				Transforms:    transform(8.0, 0, -1, 0, 0),
				ReceiveShadow: true,
			},
		},
		PostEffects: []engine.RenderPostEffect{{
			Kind:      "bloom",
			Threshold: 0.95,
			Intensity: 0.18,
			Radius:    6,
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
	canvas := js.Global().Get("document").Call("getElementById", canvasID)
	start := js.Global().Get("performance").Call("now").Float()

	var raf js.Func
	raf = js.FuncOf(func(this js.Value, args []js.Value) any {
		now := js.Global().Get("performance").Call("now").Float()
		if len(args) > 0 && args[0].Type() == js.TypeNumber {
			now = args[0].Float()
		}
		t := (now - start) / 1000
		if err := renderer.Frame(frameBundle(t), canvas.Get("width").Int(), canvas.Get("height").Int(), t); err != nil {
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
	js.Global().Set("gosxCubemapIBLSpikeStart", js.FuncOf(func(this js.Value, args []js.Value) any {
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
