//go:build js && wasm

// pbr-spike proves the R2 lit + shadowed render path end-to-end:
//
//   RenderBundle with a directional light and two instanced meshes
//     → render/bundle.Renderer (shadow pass + lit main pass)
//     → render/gpu/jsgpu (syscall/js)
//     → navigator.gpu → pixels with a visible shadow
//
// The scene is deliberately minimal: a large ground plane and a sphere,
// lit by an orbiting "sun" directional light. The sphere's shadow
// sweeps across the plane as the sun moves.
package main

import (
	"fmt"
	"math"
	"syscall/js"

	"github.com/odvcencio/gosx/engine"
	"github.com/odvcencio/gosx/render/bundle"
	"github.com/odvcencio/gosx/render/gpu/jsgpu"
)

// instanceMatrix builds a single per-instance mat4 = scale × rotateY × translate.
// Column-major layout (16 floats, same as RenderInstancedMesh.Transforms).
func instanceMatrix(tx, ty, tz, rotY, scale float64) []float64 {
	c := math.Cos(rotY)
	s := math.Sin(rotY)
	// Scale is uniform for R2. The math reduces to:
	//   [ scale*c   0      scale*s   0 ]
	//   [ 0         scale  0         0 ]
	//   [ -scale*s  0      scale*c   0 ]
	//   [ tx        ty     tz        1 ]
	return []float64{
		scale * c, 0, -scale * s, 0,
		0, scale, 0, 0,
		scale * s, 0, scale * c, 0,
		tx, ty, tz, 1,
	}
}

// synthesizeBundle builds a scene with a big plane + orbiting sphere under a
// rotating directional light. Everything is driven by the single time input.
func synthesizeBundle(t float64) engine.RenderBundle {
	// Camera orbit around origin, slightly above ground.
	camRadius := 10.0
	camX := camRadius * math.Cos(t*0.15)
	camZ := camRadius * math.Sin(t*0.15)
	camY := 5.5

	// Directional light orbits the scene so shadows visibly sweep.
	lightAngle := t * 0.4
	lightDirX := math.Cos(lightAngle)
	lightDirY := -1.2
	lightDirZ := math.Sin(lightAngle) * 0.5
	lenSq := lightDirX*lightDirX + lightDirY*lightDirY + lightDirZ*lightDirZ
	invLen := 1 / math.Sqrt(lenSq)
	lightDirX *= invLen
	lightDirY *= invLen
	lightDirZ *= invLen

	// Sphere bobs vertically to show the shadow distance-on-plane effect.
	sphereY := 1.5 + 0.4*math.Sin(t*1.2)
	sphereSpin := t * 0.6

	// Plane is a 12x12 unit floor (scale = 6 on a [-1,1] primitive plane).
	plane := instanceMatrix(0, 0, 0, 0, 6)
	// Sphere scaled down slightly so its shadow fits inside the plane.
	sphere := instanceMatrix(0, sphereY, 0, sphereSpin, 1.1)

	return engine.RenderBundle{
		Background: "#0a1220",
		Camera: engine.RenderCamera{
			X:         camX,
			Y:         camY,
			Z:         camZ,
			RotationX: math.Atan2(-camY, camRadius),
			RotationY: -math.Atan2(camX, camZ),
			FOV:       math.Pi / 3,
			Near:      0.1,
			Far:       200,
		},
		Environment: engine.RenderEnvironment{
			AmbientColor:     "#2a3a55",
			AmbientIntensity: 0.4,
		},
		Lights: []engine.RenderLight{{
			Kind:       "directional",
			Color:      "#fff1c8",
			Intensity:  1.2,
			DirectionX: lightDirX,
			DirectionY: lightDirY,
			DirectionZ: lightDirZ,
			CastShadow: true,
		}},
		InstancedMeshes: []engine.RenderInstancedMesh{
			{
				Kind:          "plane",
				VertexCount:   6,
				InstanceCount: 1,
				Transforms:    plane,
				ReceiveShadow: true,
			},
			{
				Kind:          "sphere",
				VertexCount:   0, // renderer infers from the primitive geometry
				InstanceCount: 1,
				Transforms:    sphere,
				CastShadow:    true,
			},
		},
	}
}

func runSpike(canvasID string) error {
	device, surface, err := jsgpu.Open(canvasID)
	if err != nil {
		return fmt.Errorf("open gpu device: %w", err)
	}
	r, err := bundle.New(bundle.Config{Device: device, Surface: surface})
	if err != nil {
		return fmt.Errorf("new renderer: %w", err)
	}

	canvas := js.Global().Get("document").Call("getElementById", canvasID)
	start := js.Global().Get("performance").Call("now").Float()

	var raf js.Func
	raf = js.FuncOf(func(this js.Value, args []js.Value) any {
		var tMs float64
		if len(args) > 0 && args[0].Type() == js.TypeNumber {
			tMs = args[0].Float()
		} else {
			tMs = js.Global().Get("performance").Call("now").Float()
		}
		t := (tMs - start) / 1000

		width := canvas.Get("width").Int()
		height := canvas.Get("height").Int()

		b := synthesizeBundle(t)
		if err := r.Frame(b, width, height, t); err != nil {
			js.Global().Get("console").Call("error", "frame error:", err.Error())
			return nil
		}
		js.Global().Call("requestAnimationFrame", raf)
		return nil
	})
	js.Global().Call("requestAnimationFrame", raf)
	return nil
}

func main() {
	js.Global().Set("gosxPBRSpikeStart", js.FuncOf(func(this js.Value, args []js.Value) any {
		if len(args) < 1 {
			return js.ValueOf(map[string]any{"ok": false, "error": "need canvasID"})
		}
		canvasID := args[0].String()
		promiseCtor := js.Global().Get("Promise")
		return promiseCtor.New(js.FuncOf(func(this js.Value, args []js.Value) any {
			resolve := args[0]
			reject := args[1]
			go func() {
				if err := runSpike(canvasID); err != nil {
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
