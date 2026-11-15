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

	// A closed box under the x mirror covers the same pixels as the unmirrored
	// box, because only its outward faces draw.
	t.Run("a mirrored closed mesh keeps its coverage", func(t *testing.T) {
		box := scene.Mesh{ID: "probe", Geometry: scene.BoxGeometry{Width: 1.5, Height: 1.5, Depth: 1.5},
			Material: scene.StandardMaterial{Color: "#c0c0c0", Roughness: 0.8, Emissive: 0.6}}
		count := func(sx float64) int {
			img, _ := renderScaleScene(t, scaleTestProps(scene.Group{
				Scale: scene.Vec3(sx, 1, 1), Rotation: scene.Euler{Y: 0.3},
				Children: []scene.Node{box},
			}), nil)
			n := 0
			for y := 0; y < scaleTestSize; y++ {
				for x := 0; x < scaleTestSize; x++ {
					if c := img.RGBAAt(x, y); c.R|c.G|c.B != 0 {
						n++
					}
				}
			}
			return n
		}
		plain, mirrored := count(1), count(-1)
		if plain < 200 {
			t.Fatalf("the box drew %d pixels, want at least 200", plain)
		}
		// Mirroring turns the box about the Y axis, so the silhouette is the
		// mirror image and has the same area. A missing winding discard would
		// not change area here, but a wrong sign would leave holes.
		if diff := plain - mirrored; diff > plain/50 || -diff > plain/50 {
			t.Errorf("coverage: plain %d, mirrored %d", plain, mirrored)
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
