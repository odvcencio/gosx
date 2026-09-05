package preview_test

import (
	"encoding/binary"
	"image"
	"image/color"
	"math"
	"testing"
	"time"

	"m31labs.dev/gosx/render/bundle"
	"m31labs.dev/gosx/render/gpu"
	"m31labs.dev/gosx/render/gpu/headless"
	"m31labs.dev/gosx/scene"
	"m31labs.dev/gosx/scene/preview"
)

func affinePlane(id string, z, tilt float64) scene.Mesh {
	return scene.Mesh{ID: id, Pickable: scene.Bool(true), Geometry: scene.PlaneGeometry{Width: 2, Height: 2}, Material: scene.StandardMaterial{Color: "#ffffff", Roughness: 1}, Position: scene.Vec3(0, 0, z), Rotation: scene.Euler{X: tilt}}
}

func affineProps(sx, angle float64, back bool) scene.Props {
	tilt := math.Pi / 2
	if back {
		tilt = -tilt
	}
	return scene.Props{Width: 65, Height: 65, Background: "#000000", Camera: scene.PerspectiveCamera{Position: scene.Vec3(0, 0, 5)}, Graph: scene.NewGraph(
		scene.DirectionalLight{Color: "#ffffff", Intensity: 1, Direction: scene.Vec3(-math.Copysign(1, sx), 0, -.2)},
		scene.Group{Scale: scene.Vec3(sx, 1, .5), Children: []scene.Node{scene.Group{Rotation: scene.Euler{Y: angle}, Children: []scene.Node{affinePlane("near", 0, tilt)}}}},
	)}
}

func affineFrame(t *testing.T, props scene.Props) (color.RGBA, bundle.PickResult) {
	image, hit := affineImage(t, props)
	return image.RGBAAt(32, 32), hit
}

func affineImage(t *testing.T, props scene.Props) (*image.RGBA, bundle.PickResult) {
	img, hit, _ := affineReadback(t, props)
	return img, hit
}

type affineCaptureDevice struct {
	*headless.Device
	depth gpu.Texture
}

func (d *affineCaptureDevice) CreateTexture(desc gpu.TextureDesc) (gpu.Texture, error) {
	if desc.Label == "bundle.depth" {
		desc.Usage |= gpu.TextureUsageCopySrc
	}
	texture, err := d.Device.CreateTexture(desc)
	if desc.Label == "bundle.depth" {
		d.depth = texture
	}
	return texture, err
}

func affineReadback(t *testing.T, props scene.Props) (*image.RGBA, bundle.PickResult, float64) {
	t.Helper()
	frame := preview.Bundle(props, preview.Options{Width: 65, Height: 65})
	baseDevice, surface := headless.New(65, 65)
	device := &affineCaptureDevice{Device: baseDevice}
	renderer, err := bundle.New(bundle.Config{Device: device, Surface: surface})
	if err != nil {
		t.Fatal(err)
	}
	defer renderer.Destroy()
	done := make(chan bundle.PickResult, 1)
	renderer.QueuePickResult(32, 32, func(result bundle.PickResult) { done <- result })
	if err := renderer.Frame(frame, 65, 65, 0); err != nil {
		t.Fatal(err)
	}
	staging, err := device.CreateBuffer(gpu.BufferDesc{Size: 256, Usage: gpu.BufferUsageMapRead | gpu.BufferUsageCopyDst})
	if err != nil {
		t.Fatal(err)
	}
	defer staging.Destroy()
	encoder := device.CreateCommandEncoder()
	encoder.CopyTextureToBuffer(gpu.TextureCopyInfo{Texture: device.depth, Origin: [3]int{32, 32, 0}}, gpu.BufferCopyInfo{Buffer: staging, BytesPerRow: 256, RowsPerImage: 1}, 1, 1, 1)
	device.Queue().Submit(encoder.Finish())
	data, err := staging.ReadAsync(4)
	if err != nil {
		t.Fatal(err)
	}
	bits := binary.LittleEndian.Uint32(data)
	depth := float64(bits&0xffffff) / 0xffffff
	if device.depth.Format() == gpu.FormatDepth32Float {
		depth = float64(math.Float32frombits(bits))
	}
	// The rasterizer maps OpenGL NDC through ndcToDepth before attachment writes.
	distance := frame.Camera.Near * frame.Camera.Far / (frame.Camera.Far - depth*(frame.Camera.Far-frame.Camera.Near))
	select {
	case hit := <-done:
		return device.Framebuffer(), hit, distance
	case <-time.After(2 * time.Second):
		t.Fatal("pick did not settle")
	}
	return nil, bundle.PickResult{}, 0
}

func TestTypedNestedAffinePlacementAndParentRemoval(t *testing.T) {
	width := func(props scene.Props) int {
		img, _ := affineImage(t, props)
		left, right := 65, 0
		for y := 25; y < 40; y++ {
			for x := 0; x < 65; x++ {
				p := img.RGBAAt(x, y)
				if p.R > 40 {
					left = min(left, x)
					right = max(right, x)
				}
			}
		}
		return right - left
	}
	positive := affineProps(3, .7, false)
	mirrored := affineProps(-3, .7, false)
	rotationOnly := positive
	rotationOnly.Graph = scene.NewGraph(scene.Group{Rotation: scene.Euler{Y: .7}, Children: []scene.Node{affinePlane("near", 0, math.Pi/2)}})
	p, m, r := width(positive), width(mirrored), width(rotationOnly)
	t.Logf("consumed placement widths positive=%d mirrored=%d removed-parent=%d", p, m, r)
	if p <= 2*r || math.Abs(float64(p-m)) > 1 {
		t.Fatalf("parent scale placement insensitive: %d %d %d", p, m, r)
	}
}

func TestTypedNestedAffineConsumedNormal(t *testing.T) {
	for _, sx := range []float64{3, -3} {
		t.Run(map[bool]string{true: "mirrored", false: "positive"}[sx < 0], func(t *testing.T) {
			props := affineProps(sx, .7, false)
			pixel, hit := affineFrame(t, props)
			// A=S*Ry(.7): inverse transpose transforms +Z to (sin(.7)/sx,0,cos(.7)/.5).
			// A rotation-only authored plane with that analytic normal is an independent
			// lighting oracle at the same world-space center, not a helper under test.
			expectedAngle := math.Atan2(math.Sin(.7)/sx, math.Cos(.7)/.5)
			reference := affineProps(1, expectedAngle, false)
			rotationReference := func(angle float64) scene.Props {
				p := reference
				p.Graph = scene.NewGraph(scene.DirectionalLight{Color: "#ffffff", Intensity: 1, Direction: scene.Vec3(-math.Copysign(1, sx), 0, -.2)}, scene.Group{Rotation: scene.Euler{Y: angle}, Children: []scene.Node{affinePlane("reference", 0, math.Pi/2)}})
				return p
			}
			reference = rotationReference(expectedAngle)
			want, _ := affineFrame(t, reference)
			wrong, _ := affineFrame(t, rotationReference(math.Atan2(math.Sin(.7)*sx, math.Cos(.7)*.5)))
			if math.Abs(float64(want.R)-float64(wrong.R)) < 5 {
				t.Fatalf("ordinary-vector negative is insensitive: correct=%v wrong=%v", want, wrong)
			}
			t.Logf("typed normal pixel=%v analytic rotation pixel=%v hit=%+v", pixel, want, hit)
			if math.Abs(float64(pixel.R)-float64(want.R)) > 2 {
				t.Errorf("consumed affine normal lighting=%v want=%v", pixel, want)
			}
			if hit.ObjectID != "near" || hit.InstanceIndex != 0 || hit.Depth < 4.8 || hit.Depth > 5.2 {
				t.Errorf("typed pick=%+v", hit)
			}
		})
	}
}

func TestTypedNestedAffineNearestVisiblePick(t *testing.T) {
	for _, reverse := range []bool{false, true} {
		props := affineProps(-3, .7, false)
		near := scene.Group{Scale: scene.Vec3(-3, 1, .5), Children: []scene.Node{scene.Group{Rotation: scene.Euler{Y: .7}, Children: []scene.Node{affinePlane("near", 0, math.Pi/2)}}}}
		far := affinePlane("far", -1, math.Pi/2)
		back := affinePlane("invisible-front", 1, -math.Pi/2)
		nodes := []scene.Node{near, far, back}
		if reverse {
			nodes = []scene.Node{back, far, near}
		}
		props.Graph = scene.NewGraph(nodes...)
		_, hit := affineFrame(t, props)
		if hit.ObjectID != "near" || hit.InstanceIndex != 0 || math.Abs(float64(hit.Depth)-5) > 1e-5 || hit.WorldPosition != [3]float32{} || hit.LocalPosition != [3]float32{} {
			t.Fatalf("reverse=%v nearest visible metadata=%+v", reverse, hit)
		}
	}
}

func TestTypedNestedAffineSameObjectVisibleDepth(t *testing.T) {
	for _, sx := range []float64{3, -3} {
		props := affineProps(sx, .7, false)
		props.Camera.Position = scene.Vec3(0, 0, 1)
		props.Graph = scene.NewGraph(scene.Group{Scale: scene.Vec3(sx, 1, .5), Children: []scene.Node{scene.Group{Rotation: scene.Euler{Y: .7}, Children: []scene.Node{scene.Mesh{ID: "ring", Geometry: scene.TorusGeometry{Radius: 2, Tube: .5, RadialSegments: 64, TubularSegments: 32}, Material: scene.StandardMaterial{Color: "#ffffff"}}}}}})
		img, hit, renderedDepth := affineReadback(t, props)
		t.Logf("inside near ring sees opposite front surface: pixel=%v actual depth attachment=%g hit=%+v", img.RGBAAt(32, 32), renderedDepth, hit)
		if renderedDepth < 1.7 || renderedDepth > 1.8 {
			t.Fatalf("rendered depth=%g", renderedDepth)
		}
		if hit.ObjectID != "ring" || hit.Depth < 1.7 || hit.Depth > 1.8 {
			t.Fatalf("visible far-ring depth expected near1.75, got %+v", hit)
		}
		// Existing CPU raster projection uses width-1, while pick rays use
		// (pixel+.5)/width. Bound the subpixel depth difference, not the 1.5-unit
		// hidden backface error. Do not change viewport conventions in this slice.
		if math.Abs(float64(hit.Depth)-renderedDepth) > .002 {
			t.Errorf("rendered depth=%g enriched=%g", renderedDepth, hit.Depth)
		}
	}
}

func TestTypedNestedAffineClippedFacing(t *testing.T) {
	for _, sx := range []float64{3, -3} {
		for _, back := range []bool{false, true} {
			props := affineProps(sx, .7, back)
			props.Camera.Position.Z = .2
			img, _ := affineImage(t, props)
			lit := 0
			for y := 0; y < 65; y++ {
				for x := 0; x < 65; x++ {
					if img.RGBAAt(x, y).R > 40 {
						lit++
					}
				}
			}
			if back && lit != 0 || !back && lit < 100 {
				t.Errorf("clipped facing sx=%g back=%v lit=%d", sx, back, lit)
			}
		}
	}
}

func TestTypedNestedAffineRejectsBackfaceBeforePick(t *testing.T) {
	for _, sx := range []float64{3, -3} {
		props := affineProps(sx, .7, true)
		pixel, hit := affineFrame(t, props)
		if pixel.R != 0 || pixel.G != 0 || pixel.B != 0 || hit.ObjectID != "" {
			t.Errorf("backface sx=%v wrote color/pick: %v %+v", sx, pixel, hit)
		}
	}
}
