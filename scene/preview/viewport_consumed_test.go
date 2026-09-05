package preview_test

import (
	"encoding/binary"
	"fmt"
	"m31labs.dev/gosx/engine"
	"m31labs.dev/gosx/render/bundle"
	"m31labs.dev/gosx/render/gpu"
	"m31labs.dev/gosx/render/gpu/headless"
	"m31labs.dev/gosx/scene"
	"m31labs.dev/gosx/scene/preview"
	"math"
	"testing"
	"time"
)

func TestViewportConsumedAttachmentPickSameRay(t *testing.T) {
	for _, mode := range []string{"perspective", "orthographic", "orthographic-asymmetric", "orthographic-default", "orthographic-reversed", "orthographic-rotated"} {
		for _, size := range [][2]int{{1, 1}, {2, 2}, {8, 6}, {9, 7}} {
			t.Run(fmt.Sprintf("%s/%dx%d", mode, size[0], size[1]), func(t *testing.T) {
				props := affineProps(3, .7, false)
				plane := affinePlane("viewport", 0, math.Pi/2)
				plane.Geometry = scene.PlaneGeometry{Width: 20, Height: 20}
				props.Graph = scene.NewGraph(scene.Group{Scale: scene.Vec3(3, 1, .5), Children: []scene.Node{scene.Group{Rotation: scene.Euler{Y: .7}, Children: []scene.Node{plane}}}})
				frame := preview.Bundle(props, preview.Options{Width: size[0], Height: size[1]})
				expectedBounds := [4]float64{-2, 2, -2, 2}
				if mode != "perspective" {
					// Typed scene lowering is real; native orthographic camera is an explicit
					// RenderBundle override because preview's native camera lowering
					// currently accepts only PerspectiveCamera (typed Props also has ortho).
					frame.Camera = engine.RenderCamera{Mode: "orthographic", Z: 5, Left: -2, Right: 2, Bottom: -2, Top: 2, Zoom: 1, Near: .1, Far: 100}
					if mode == "orthographic-asymmetric" || mode == "orthographic-rotated" {
						frame.Camera.Left, frame.Camera.Right = 0, 4
						frame.Camera.Bottom, frame.Camera.Top = -1, 3
						frame.Camera.Zoom = 2
						expectedBounds = [4]float64{1, 3, 0, 2}
					}
					if mode == "orthographic-default" {
						frame.Camera.Left, frame.Camera.Right, frame.Camera.Bottom, frame.Camera.Top, frame.Camera.Zoom = 0, 0, 0, 0, 0
						a := float64(size[0]) / float64(size[1])
						expectedBounds = [4]float64{-3 * a, 3 * a, -3, 3}
					}
					if mode == "orthographic-reversed" {
						frame.Camera.Left, frame.Camera.Right, frame.Camera.Bottom, frame.Camera.Top = 3, 1, 2, 0
						expectedBounds = [4]float64{1, 3, 0, 2}
					}
					if mode == "orthographic-rotated" {
						frame.Camera.RotationX, frame.Camera.RotationY = .12, -.18
					}
				}
				base, surface := headless.New(size[0], size[1])
				device := &affineCaptureDevice{Device: base}
				renderer, err := bundle.New(bundle.Config{Device: device, Surface: surface})
				if err != nil {
					t.Fatal(err)
				}
				defer renderer.Destroy()
				for _, p := range [][2]int{{0, 0}, {size[0] - 1, 0}, {0, size[1] - 1}, {size[0] - 1, size[1] - 1}, {size[0] / 2, size[1] / 2}} {
					done := make(chan bundle.PickResult, 1)
					renderer.QueuePickResult(p[0], p[1], func(hit bundle.PickResult) { done <- hit })
					if err := renderer.Frame(frame, size[0], size[1], 0); err != nil {
						t.Fatal(err)
					}
					var hit bundle.PickResult
					select {
					case hit = <-done:
					case <-time.After(time.Second):
						t.Fatal("pick did not settle")
					}
					if hit.ObjectID != "viewport" {
						t.Errorf("sample=%v missing visible pick %+v", p, hit)
						continue
					}
					staging, err := device.CreateBuffer(gpu.BufferDesc{Size: 256, Usage: gpu.BufferUsageMapRead | gpu.BufferUsageCopyDst})
					if err != nil {
						t.Fatal(err)
					}
					encoder := device.CreateCommandEncoder()
					encoder.CopyTextureToBuffer(gpu.TextureCopyInfo{Texture: device.depth, Origin: [3]int{p[0], p[1], 0}}, gpu.BufferCopyInfo{Buffer: staging, BytesPerRow: 256, RowsPerImage: 1}, 1, 1, 1)
					device.Queue().Submit(encoder.Finish())
					data, err := staging.ReadAsync(4)
					staging.Destroy()
					if err != nil {
						t.Fatal(err)
					}
					bits := binary.LittleEndian.Uint32(data)
					depth := float64(bits&0xffffff) / 0xffffff
					if device.depth.Format() == gpu.FormatDepth32Float {
						depth = float64(math.Float32frombits(bits))
					}
					n, f := frame.Camera.Near, frame.Camera.Far
					z := n * f / (f - depth*(f-n))
					if mode != "perspective" {
						z = n + depth*(f-n)
					}
					// Independent analytic plane: Rx(pi/2), Ry(.7), scale(3,1,.5)
					// gives z=-tan(.7)*x/6. No renderer/picker matrix helper is used.
					world, analyticZ, forward := viewportPlaneOracle(frame.Camera, expectedBounds, p, size)
					expectedZ := (float64(hit.WorldPosition[0])-frame.Camera.X)*forward[0] + (float64(hit.WorldPosition[1])-frame.Camera.Y)*forward[1] + (float64(hit.WorldPosition[2])-frame.Camera.Z)*forward[2]
					if math.Abs(z-expectedZ) > 1e-4 {
						t.Errorf("sample=%v attachment viewDepth=%.8f picked viewDepth=%.8f hit=%+v", p, z, expectedZ, hit)
					}
					if math.Abs(expectedZ-analyticZ) > 1e-4 || math.Abs(z-analyticZ) > 1e-4 {
						t.Errorf("sample=%v independent plane viewDepth=%.8f pick=%.8f attachment=%.8f", p, analyticZ, expectedZ, z)
					}
					for axis := range world {
						if math.Abs(float64(hit.WorldPosition[axis])-world[axis]) > 1e-4 {
							t.Errorf("sample=%v axis=%d picked=%g analytic=%g", p, axis, hit.WorldPosition[axis], world[axis])
						}
					}
				}
			})
		}
	}
}

func viewportPlaneOracle(cam engine.RenderCamera, bounds [4]float64, pixel, size [2]int) (world [3]float64, depth float64, forward [3]float64) {
	// Native camera convention is inverse X then inverse Y, without roll.
	rotate := func(v [3]float64) [3]float64 {
		sx, cx := math.Sincos(-cam.RotationX)
		sy, cy := math.Sincos(-cam.RotationY)
		y, z := cx*v[1]-sx*v[2], sx*v[1]+cx*v[2]
		return [3]float64{cy*v[0] + sy*z, y, -sy*v[0] + cy*z}
	}
	nx := 2*(float64(pixel[0])+.5)/float64(size[0]) - 1
	ny := 1 - 2*(float64(pixel[1])+.5)/float64(size[1])
	origin := [3]float64{cam.X, cam.Y, cam.Z}
	forward = rotate([3]float64{0, 0, -1})
	direction := forward
	if cam.Mode == "orthographic" {
		offset := rotate([3]float64{(bounds[0]+bounds[1])/2 + nx*(bounds[1]-bounds[0])/2, (bounds[2]+bounds[3])/2 + ny*(bounds[3]-bounds[2])/2, 0})
		for i := range origin {
			origin[i] += offset[i]
		}
	} else {
		direction = rotate([3]float64{nx * float64(size[0]) / float64(size[1]) * math.Tan(cam.FOV/2), ny * math.Tan(cam.FOV/2), -1})
	}
	k := math.Tan(.7) / 6
	depth = -(k*origin[0] + origin[2]) / (k*direction[0] + direction[2])
	for i := range world {
		world[i] = origin[i] + depth*direction[i]
	}
	return world, depth, forward
}
