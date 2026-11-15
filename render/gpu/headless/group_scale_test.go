package headless_test

import (
	"image"
	"math"
	"testing"
	"time"

	"m31labs.dev/gosx/render/bundle"
	"m31labs.dev/gosx/render/gpu/headless"
	"m31labs.dev/gosx/scene"
	"m31labs.dev/gosx/scene/preview"
)

// These tests run nested Group.Scale scenes through the path the native preview
// uses: preview.Bundle lowers the scene, bundle.Renderer draws it on the
// headless device. They check three things the lit shader in render/bundle/lit.go
// promises under a non-uniform scale, a reflection and a nested uniform scale:
//
//   - the shading normal is the inverse transpose of the model matrix, times
//     the sign of its determinant;
//   - a fragment draws only when its screen winding agrees with that sign;
//   - the pick result agrees with scene.RaycastGraph.
//
// Each expected value comes from a matrix worked out in the test, not from the
// device code under test.

const (
	scaleTestSize = 65 // odd, so the screen centre is one pixel
	scaleTestTilt = 40 * math.Pi / 180
)

func scaleTestCamera() *scene.PerspectiveCamera {
	// The camera sits above the origin and looks down -Y. A scene.PlaneGeometry
	// lies in the XZ plane with its normal along +Y, so the camera sees its front.
	return &scene.PerspectiveCamera{Position: scene.Vec3(0, 6, 0), Rotation: scene.Euler{X: math.Pi / 2}, FOV: 50, Near: 0.1, Far: 60}
}

func scaleTestLight() scene.DirectionalLight {
	return scene.DirectionalLight{ID: "key", Intensity: 1.5, Direction: scene.Vec3(-0.5, -1, -0.3)}
}

func scaleTestProps(node scene.Node) scene.Props {
	return scene.Props{
		Background: "#000000",
		Environment: scene.Environment{
			AmbientColor: "#000000", SkyColor: "#000000", GroundColor: "#000000",
		},
		Graph: scene.Graph{Nodes: []scene.Node{scaleTestLight(), node}},
	}
}

func scaleTestPlane() scene.Mesh {
	return scene.Mesh{
		ID:       "probe",
		Geometry: scene.PlaneGeometry{Width: 2, Height: 2},
		Material: scene.StandardMaterial{Color: "#c0c0c0", Roughness: 0.8},
	}
}

// scaleTestGlowPlane is the same plane with an emissive term, so a face that the
// light does not reach still draws a non-black pixel when it is not discarded.
// The winding cases need this: a back face faces away from the light and would
// be black whether or not the shader discarded it.
func scaleTestGlowPlane() scene.Mesh {
	plane := scaleTestPlane()
	plane.Material = scene.StandardMaterial{Color: "#c0c0c0", Roughness: 0.8, Emissive: 0.6}
	return plane
}

// renderScaleScene draws one frame and, when pick is non-nil, queues a pick at
// that pixel first. The pick result is the zero value when pick is nil.
func renderScaleScene(t *testing.T, props scene.Props, pick *image.Point) (*image.RGBA, bundle.PickResult) {
	t.Helper()
	opts := preview.Options{
		Width: scaleTestSize, Height: scaleTestSize, Background: "#000000",
		Camera: scaleTestCamera(), DisableShadows: true, DisablePostFX: true,
	}
	frame := preview.Bundle(props, opts)
	device, surface := headless.New(scaleTestSize, scaleTestSize)
	renderer, err := bundle.New(bundle.Config{Device: device, Surface: surface})
	if err != nil {
		t.Fatalf("bundle.New: %v", err)
	}
	defer renderer.Destroy()
	done := make(chan bundle.PickResult, 1)
	if pick != nil {
		renderer.QueuePickResult(pick.X, pick.Y, func(r bundle.PickResult) { done <- r })
	}
	if err := renderer.Frame(frame, scaleTestSize, scaleTestSize, 0); err != nil {
		t.Fatalf("Frame: %v", err)
	}
	var result bundle.PickResult
	if pick != nil {
		select {
		case result = <-done:
		case <-time.After(2 * time.Second):
			t.Fatal("pick callback never fired")
		}
	}
	src := device.Framebuffer()
	img := image.NewRGBA(src.Bounds())
	copy(img.Pix, src.Pix)
	return img, result
}

func centreRGB(img *image.RGBA) [3]int {
	c := img.RGBAAt(scaleTestSize/2, scaleTestSize/2)
	return [3]int{int(c.R), int(c.G), int(c.B)}
}

func rgbDistance(a, b [3]int) int {
	d := 0
	for i := range a {
		if v := a[i] - b[i]; v > d {
			d = v
		} else if -v > d {
			d = -v
		}
	}
	return d
}

// referenceCentre draws the same plane with a plain rotation about Z, which has
// orientation +1. Rotation turns the +Y normal to (-sin a, cos a, 0), so the
// angle that gives the wanted normal (normalX, normalY, 0) is atan2(-normalX,
// normalY). The centre pixel is then the brightness a surface with that world
// normal must have here.
func referenceCentre(t *testing.T, normalX, normalY float64) [3]int {
	t.Helper()
	angle := math.Atan2(-normalX, normalY)
	img, _ := renderScaleScene(t, scaleTestProps(scene.Group{
		Rotation: scene.Euler{Z: angle},
		Children: []scene.Node{scaleTestPlane()},
	}), nil)
	return centreRGB(img)
}

// TestHeadlessNestedGroupScaleNormalsWindingAndPick is the native-preview proof
// for the "nested-group-scale-pick" corpus row.
func TestHeadlessNestedGroupScaleNormalsWindingAndPick(t *testing.T) {
	sinT, cosT := math.Sin(scaleTestTilt), math.Cos(scaleTestTilt)
	background := [3]int{0, 0, 0}

	// The plane's local normal is +Y. The inner group turns it about Z, so the
	// normal before the outer scale is (-sinT, cosT, 0).
	t.Run("normals follow the inverse transpose", func(t *testing.T) {
		for _, tc := range []struct {
			name   string
			outerX float64 // outer Group.Scale.X; the other axes are 1
			inner  float64 // inner uniform scale
		}{
			{"non-uniform", 3, 1},
			{"non-uniform and mirrored, inner uniform", -3, 1.5},
		} {
			t.Run(tc.name, func(t *testing.T) {
				props := scaleTestProps(scene.Group{
					Scale: scene.Vec3(tc.outerX, 1, 1),
					Children: []scene.Node{scene.Group{
						Scale:    scene.Vec3(tc.inner, tc.inner, tc.inner),
						Rotation: scene.Euler{Z: scaleTestTilt},
						Children: []scene.Node{scaleTestPlane()},
					}},
				})
				img, _ := renderScaleScene(t, props, nil)
				got := centreRGB(img)
				if rgbDistance(got, background) == 0 {
					t.Fatalf("the plane did not draw: centre pixel %v", got)
				}
				// World matrix M = S*R*k. Normals move by M^-T, which for this
				// S (diagonal) and R (rotation) is S^-1*R up to a positive
				// scale. The model matrix itself would give S*R*n.
				want := referenceCentre(t, -sinT/tc.outerX, cosT)
				old := referenceCentre(t, -sinT*tc.outerX, cosT)
				if d := rgbDistance(got, want); d > 3 {
					t.Errorf("centre pixel %v differs from the inverse-transpose reference %v by %d", got, want, d)
				}
				if d := rgbDistance(got, old); d < 15 {
					t.Errorf("centre pixel %v is within %d of the model-matrix reference %v; the test cannot tell the two apart", got, d, old)
				}
			})
		}
	})

	// Winding. The plane is wound counter-clockwise seen from +Y. The lit shader
	// keeps a fragment when its screen winding is counter-clockwise exactly if
	// the model determinant is positive.
	t.Run("only front faces draw", func(t *testing.T) {
		wrap := func(scale scene.Vector3, rotZ float64) scene.Props {
			return scaleTestProps(scene.Group{
				Scale: scale, Rotation: scene.Euler{Z: rotZ},
				Children: []scene.Node{scaleTestGlowPlane()},
			})
		}
		for _, tc := range []struct {
			name  string
			props scene.Props
			draws bool
		}{
			{"positive orientation, front", wrap(scene.Vec3(1, 1, 1), 0), true},
			{"positive orientation, seen from behind", wrap(scene.Vec3(1, 1, 1), math.Pi), false},
			{"x mirror: winding flips with the sign, so it draws", wrap(scene.Vec3(-1, 1, 1), 0), true},
			{"y mirror: winding stays, the sign flips, so it is discarded", wrap(scene.Vec3(1, -1, 1), 0), false},
			{"nested uniform scale, front", scaleTestProps(scene.Group{
				Scale: scene.Vec3(2, 2, 2),
				Children: []scene.Node{scene.Group{
					Scale:    scene.Vec3(0.75, 0.75, 0.75),
					Children: []scene.Node{scaleTestGlowPlane()},
				}},
			}), true},
		} {
			t.Run(tc.name, func(t *testing.T) {
				img, _ := renderScaleScene(t, tc.props, nil)
				got := centreRGB(img)
				drew := rgbDistance(got, background) > 0
				if drew != tc.draws {
					t.Fatalf("centre pixel %v: drew = %v, want %v", got, drew, tc.draws)
				}
			})
		}
	})

	// Extreme scale. The inner scale (0.001, 10, 0.001) stretches the plane's
	// normal axis, so the cofactor product for that normal is about 1e-8 long. A
	// normalize that treats anything under 1e-6 as zero would answer "up" and
	// ignore the outer tilt. The plane is 2000 units wide so that it measures 2 by
	// 2 after the 0.001 scale. The outer group tilts it 50 degrees about Z, and a
	// rotation does not change the cofactor's length, so the true world normal is
	// (-sin 50, cos 50, 0), which is the same face under a uniform scale. The
	// "up" answer would differ from it by well over 15 levels (checked below), and
	// the tolerance of 3 levels is the one the other normal cases use.
	t.Run("an extreme scale keeps the normal direction", func(t *testing.T) {
		tilt := 50 * math.Pi / 180
		props := scaleTestProps(scene.Group{
			Rotation: scene.Euler{Z: tilt},
			Children: []scene.Node{scene.Group{
				Scale: scene.Vec3(0.001, 10, 0.001),
				Children: []scene.Node{scene.Mesh{
					ID:       "probe",
					Geometry: scene.PlaneGeometry{Width: 2000, Height: 2000},
					Material: scene.StandardMaterial{Color: "#c0c0c0", Roughness: 0.8},
				}},
			}},
		})
		img, _ := renderScaleScene(t, props, nil)
		got := centreRGB(img)
		if rgbDistance(got, background) == 0 {
			t.Fatalf("the plane did not draw: centre pixel %v", got)
		}
		want := referenceCentre(t, -math.Sin(tilt), math.Cos(tilt))
		up := referenceCentre(t, 0, 1)
		if d := rgbDistance(got, want); d > 3 {
			t.Errorf("centre pixel %v differs from the uniform-scale reference %v by %d", got, want, d)
		}
		if d := rgbDistance(got, up); d < 15 {
			t.Errorf("centre pixel %v is within %d of the straight-up reference %v; the test cannot tell the two apart", got, d, up)
		}
	})

	// A closed box under the x mirror. Seen from above, the near face is the top
	// (world y = +0.75, lit by the key light) and the far face is the bottom
	// (world y = -0.75, facing away from the light, so only its emissive glow
	// shows). The centre pixel is the lit top when the rule keeps front faces and
	// the dark bottom when it is inverted, which would leave only the inner faces.
	// The expected value is a lone plane at y = +0.75 with the same material, which
	// draws in front whatever the winding rule does to the box.
	t.Run("a mirrored closed mesh shows its near face", func(t *testing.T) {
		material := scene.StandardMaterial{Color: "#c0c0c0", Roughness: 0.8, Emissive: 0.6}
		box := scene.Mesh{ID: "probe", Geometry: scene.BoxGeometry{Width: 1.5, Height: 1.5, Depth: 1.5}, Material: material}
		centre := func(sx float64) [3]int {
			img, _ := renderScaleScene(t, scaleTestProps(scene.Group{
				Scale: scene.Vec3(sx, 1, 1), Children: []scene.Node{box},
			}), nil)
			return centreRGB(img)
		}
		topFace := scene.Mesh{ID: "probe", Geometry: scene.PlaneGeometry{Width: 1.5, Height: 1.5}, Material: material}
		refImg, _ := renderScaleScene(t, scaleTestProps(scene.Group{
			Position: scene.Vec3(0, 0.75, 0), Children: []scene.Node{topFace},
		}), nil)
		want := centreRGB(refImg)
		if rgbDistance(want, background) == 0 {
			t.Fatalf("the reference top face did not draw: %v", want)
		}
		for _, sx := range []float64{1, -1} {
			if got := centre(sx); rgbDistance(got, want) > 3 {
				t.Errorf("scale x %v: centre pixel %v differs from the lit top face %v; the far face may be showing", sx, got, want)
			}
		}
	})

	// Pick. Outer scale (2,1,1) and inner scale (1,1,-1.5) put the box's top face
	// at world x in [-1, 1] and z in [-0.75, 0.75]. A pixel right of centre lands
	// at world x near 0.8, which an unscaled 1-unit box (half width 0.5) does not
	// cover.
	t.Run("pick agrees with the ray cast", func(t *testing.T) {
		box := scene.Mesh{ID: "probe", Geometry: scene.BoxGeometry{Width: 1, Height: 1, Depth: 1},
			Material: scene.StandardMaterial{Color: "#c0c0c0", Roughness: 0.8}}
		props := scaleTestProps(scene.Group{
			Scale: scene.Vec3(2, 1, 1),
			Children: []scene.Node{scene.Group{
				Scale:    scene.Vec3(1, 1, -1.5),
				Children: []scene.Node{box},
			}},
		})
		at := image.Pt(scaleTestSize/2+10, scaleTestSize/2-6)
		img, result := renderScaleScene(t, props, &at)
		if c := img.RGBAAt(at.X, at.Y); c.R|c.G|c.B == 0 {
			t.Fatalf("pixel %v is background", at)
		}
		if result.ObjectID != "probe" {
			t.Fatalf("pick object = %q, want probe; result %+v", result.ObjectID, result)
		}
		ray := scene.Ray{
			Origin:    scene.Vec3(float64(result.RayOrigin[0]), float64(result.RayOrigin[1]), float64(result.RayOrigin[2])),
			Direction: scene.Vec3(float64(result.RayDirection[0]), float64(result.RayDirection[1]), float64(result.RayDirection[2])),
		}
		hit, ok := scene.RaycastGraph(props.Graph, ray)
		if !ok {
			t.Fatalf("scene.RaycastGraph missed the ray %+v", ray)
		}
		if hit.Point.X <= 0.55 {
			t.Fatalf("the ray hits at world x = %.3f; the test needs x > 0.5 so an ignored scale would miss", hit.Point.X)
		}
		const tolerance = 1e-3
		for i, want := range []float64{hit.Point.X, hit.Point.Y, hit.Point.Z} {
			if got := float64(result.WorldPosition[i]); math.Abs(got-want) > tolerance {
				t.Errorf("pick world position %v differs from RaycastGraph point %v on axis %d", result.WorldPosition, hit.Point, i)
			}
		}
		if math.Abs(float64(result.Depth)-hit.Distance) > tolerance {
			t.Errorf("pick depth %v, RaycastGraph distance %v", result.Depth, hit.Distance)
		}
	})
}
