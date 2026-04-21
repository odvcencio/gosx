//go:build js && wasm

// scene3d-bench is the R3 exit demo: a 10k-instance scene rendered with
// cascaded shadow maps, GPU-driven frustum culling, and PBR materials.
//
// Every frame synthesizes a RenderBundle with ten thousand cubes arranged
// in a 40×10×25 grid under a directional light. The rendering path exercises
// the whole R1–R3 pipeline end-to-end:
//
//   - 3 shadow-map cascades rendered from the light's POV
//   - Compute pass frustum-culls instances against the camera view
//   - Indirect-draw reads the compacted visible-instance count
//   - Cook-Torrance lit pass samples the correct cascade per fragment
//
// FPS is reported on the host page so it's easy to verify the perf target.
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
	gridX    = 40
	gridY    = 10
	gridZ    = 25
	spacing  = 3.0
	cubeSize = 0.6
)

// bigCubeTransforms builds per-instance column-major mat4 transforms for a
// large 3D grid of rotating cubes. Allocated once per frame; for a 10k-cube
// scene the slice is 160 KB — sizable but negligible vs. GPU work.
func bigCubeTransforms(t float64) []float64 {
	total := gridX * gridY * gridZ
	out := make([]float64, total*16)

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
				out[i*16+1] = 0
				out[i*16+2] = -s * cubeSize
				out[i*16+3] = 0
				out[i*16+4] = 0
				out[i*16+5] = cubeSize
				out[i*16+6] = 0
				out[i*16+7] = 0
				out[i*16+8] = s * cubeSize
				out[i*16+9] = 0
				out[i*16+10] = c * cubeSize
				out[i*16+11] = 0
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

// floorTransform returns a single-instance mat4 for a huge ground plane
// that catches the shadow of the cube cloud from below.
func floorTransform() []float64 {
	const floorScale = 80.0
	return []float64{
		floorScale, 0, 0, 0,
		0, 1, 0, 0,
		0, 0, floorScale, 0,
		0, -15, 0, 1,
	}
}

// synthesizeBundle builds the per-frame RenderBundle: orbiting camera,
// slowly-rotating directional light, a big ground plane, and the cube grid.
func synthesizeBundle(t float64) engine.RenderBundle {
	camRadius := 50.0
	camX := camRadius * math.Cos(t*0.1)
	camZ := camRadius * math.Sin(t*0.1)
	camY := 25.0 + math.Sin(t*0.15)*6.0

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
			X:         camX,
			Y:         camY,
			Z:         camZ,
			RotationX: math.Atan2(-(camY - 0), camRadius),
			RotationY: -math.Atan2(camX, camZ),
			FOV:       math.Pi / 3,
			Near:      0.3,
			Far:       250,
		},
		Environment: engine.RenderEnvironment{
			SkyColor:         "#a6c7ff",
			SkyIntensity:     1.0,
			GroundColor:      "#2a2420",
			GroundIntensity:  1.0,
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
		InstancedMeshes: []engine.RenderInstancedMesh{
			{
				Kind:          "plane",
				VertexCount:   6,
				InstanceCount: 1,
				Transforms:    floorTransform(),
				ReceiveShadow: true,
			},
			{
				Kind:          "cube",
				VertexCount:   36,
				InstanceCount: gridX * gridY * gridZ,
				Transforms:    bigCubeTransforms(t),
				CastShadow:    true,
				ReceiveShadow: true,
			},
		},
	}
}

func runBench(canvasID string) error {
	device, surface, err := jsgpu.Open(canvasID)
	if err != nil {
		return fmt.Errorf("open gpu device: %w", err)
	}
	r, err := bundle.New(bundle.Config{Device: device, Surface: surface})
	if err != nil {
		return fmt.Errorf("new renderer: %w", err)
	}

	canvas := js.Global().Get("document").Call("getElementById", canvasID)
	statsEl := js.Global().Get("document").Call("getElementById", "stats")
	start := js.Global().Get("performance").Call("now").Float()

	// FPS rolling average over 30 frames.
	var frameBudget [30]float64
	frameIdx := 0
	prev := start

	var raf js.Func
	raf = js.FuncOf(func(this js.Value, args []js.Value) any {
		var tMs float64
		if len(args) > 0 && args[0].Type() == js.TypeNumber {
			tMs = args[0].Float()
		} else {
			tMs = js.Global().Get("performance").Call("now").Float()
		}
		t := (tMs - start) / 1000
		dt := tMs - prev
		prev = tMs
		frameBudget[frameIdx%len(frameBudget)] = dt
		frameIdx++

		width := canvas.Get("width").Int()
		height := canvas.Get("height").Int()

		b := synthesizeBundle(t)
		if err := r.Frame(b, width, height, t); err != nil {
			js.Global().Get("console").Call("error", "frame error:", err.Error())
			return nil
		}

		// Update the stats panel every ~10 frames.
		if frameIdx%10 == 0 && !statsEl.IsNull() && !statsEl.IsUndefined() {
			avg := 0.0
			for _, f := range frameBudget {
				avg += f
			}
			avg /= float64(len(frameBudget))
			fps := 1000.0 / avg
			statsEl.Set("textContent", fmt.Sprintf("%d instances · %.1f fps · %.2f ms/frame",
				gridX*gridY*gridZ, fps, avg))
		}

		js.Global().Call("requestAnimationFrame", raf)
		return nil
	})
	js.Global().Call("requestAnimationFrame", raf)
	return nil
}

func main() {
	js.Global().Set("gosxScene3DBenchStart", js.FuncOf(func(this js.Value, args []js.Value) any {
		if len(args) < 1 {
			return js.ValueOf(map[string]any{"ok": false, "error": "need canvasID"})
		}
		canvasID := args[0].String()
		promiseCtor := js.Global().Get("Promise")
		return promiseCtor.New(js.FuncOf(func(this js.Value, args []js.Value) any {
			resolve := args[0]
			reject := args[1]
			go func() {
				if err := runBench(canvasID); err != nil {
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
