//go:build js && wasm

// scene3d-spike proves the R1 rendering path end-to-end:
//
//   .gsx-shaped RenderBundle (synthesized here) → render/bundle.Renderer
//   → render/gpu → render/gpu/jsgpu (syscall/js) → navigator.gpu → pixels
//
// Zero JS renderer code. The only JS in this example is the minimal host
// page that loads wasm_exec.js and calls the Go-registered start function.
// A real .gsx Scene3D program would produce the same RenderBundle shape
// through the engine runtime once the __gosx_render_engine_to_canvas hook
// lands in Phase C.
package main

import (
	"fmt"
	"math"
	"syscall/js"

	"github.com/odvcencio/gosx/engine"
	"github.com/odvcencio/gosx/render/bundle"
	"github.com/odvcencio/gosx/render/gpu/jsgpu"
)

const (
	gridX     = 5
	gridY     = 3
	gridZ     = 5
	spacing   = 3.0
	cubeSize  = 0.6
)

// cubeTransforms builds the per-instance 4x4 transform matrices for a small
// grid of cubes. Each cube is scaled down, placed at a grid coordinate, and
// rotated by a time-based phase so the whole scene has motion.
//
// Transforms is laid out as column-major mat4 values concatenated into one
// float64 slice (same shape the engine runtime emits into
// RenderInstancedMesh.Transforms).
func cubeTransforms(t float64) []float64 {
	total := gridX * gridY * gridZ
	out := make([]float64, total*16)

	i := 0
	for ix := 0; ix < gridX; ix++ {
		for iy := 0; iy < gridY; iy++ {
			for iz := 0; iz < gridZ; iz++ {
				x := (float64(ix) - float64(gridX-1)/2) * spacing
				y := (float64(iy) - float64(gridY-1)/2) * spacing
				z := (float64(iz) - float64(gridZ-1)/2) * spacing

				// Per-cube phase so they don't all tumble identically.
				phase := t*0.7 + float64(ix+iy+iz)*0.35
				c := math.Cos(phase)
				s := math.Sin(phase)

				// Compose: Ry(phase) then translate (column-major mat4).
				// Column 0: c*scale, 0, -s*scale, 0
				out[i*16+0] = c * cubeSize
				out[i*16+1] = 0
				out[i*16+2] = -s * cubeSize
				out[i*16+3] = 0
				// Column 1: 0, scale, 0, 0
				out[i*16+4] = 0
				out[i*16+5] = cubeSize
				out[i*16+6] = 0
				out[i*16+7] = 0
				// Column 2: s*scale, 0, c*scale, 0
				out[i*16+8] = s * cubeSize
				out[i*16+9] = 0
				out[i*16+10] = c * cubeSize
				out[i*16+11] = 0
				// Column 3: tx, ty, tz, 1
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

// synthesizeBundle builds the per-frame RenderBundle that would otherwise
// come from the engine runtime. Camera orbits at a fixed radius, looking
// at origin; cube grid rotates per-instance.
func synthesizeBundle(t float64, width, height int) engine.RenderBundle {
	radius := 12.0
	camX := radius * math.Cos(t*0.3)
	camZ := radius * math.Sin(t*0.3)
	camY := 4.0 + math.Sin(t*0.2)*1.5

	return engine.RenderBundle{
		Background: "#0f1720",
		Camera: engine.RenderCamera{
			X:         camX,
			Y:         camY,
			Z:         camZ,
			RotationX: math.Atan2(-camY, radius),
			RotationY: -math.Atan2(camX, camZ),
			FOV:       math.Pi / 3,
			Near:      0.1,
			Far:       100,
		},
		InstancedMeshes: []engine.RenderInstancedMesh{{
			Kind:          "cube",
			VertexCount:   36,
			InstanceCount: gridX * gridY * gridZ,
			Transforms:    cubeTransforms(t),
		}},
	}
}

func runSpike(canvasID string) error {
	device, surface, err := jsgpu.Open(canvasID)
	if err != nil {
		return fmt.Errorf("open gpu device: %w", err)
	}

	renderer, err := bundle.New(bundle.Config{Device: device, Surface: surface})
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

		b := synthesizeBundle(t, width, height)
		if err := renderer.Frame(b, width, height, t); err != nil {
			// Don't spam the console once per frame; first-error wins.
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
	js.Global().Set("gosxScene3DSpikeStart", js.FuncOf(func(this js.Value, args []js.Value) any {
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
				resolve.Invoke(js.ValueOf(map[string]any{
					"ok":        true,
					"instances": gridX * gridY * gridZ,
				}))
			}()
			return nil
		}))
	}))

	select {}
}
