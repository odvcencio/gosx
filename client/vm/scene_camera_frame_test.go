package vm

import (
	"math"
	"reflect"
	"testing"

	rootengine "m31labs.dev/gosx/engine"
)

// Freeze the original projection/clipping arithmetic independently of the frame
// helpers. Near-plane interpolation must keep its world-space rounding, in both
// segment directions, and screen and clip focal lengths have different clamps.
func TestSceneCameraFramePreservesProjectionAndClipping(t *testing.T) {
	cameras := []sceneCamera{
		{Z: 10, FOV: 50, Near: .1, Far: 100},
		{X: 2, Y: -1, Z: 8, RotationX: .3, RotationY: -.6, RotationZ: .1, FOV: 75, Near: .2, Far: 40},
		{X: -3, Y: 2, Z: 12, RotationX: -.7, RotationY: 1.2, RotationZ: .4, FOV: .001, Near: .01, Far: 500},
		{Kind: "orthographic", Z: 10, FOV: 50, Near: .1, Far: 100, Left: -6, Right: 6, Top: 4, Bottom: -4, Zoom: 2},
	}
	points := append(trigHoistSamplePoints(), point3{X: -1000}, point3{X: 1000}, point3{Y: -1000}, point3{Y: 1000})
	for _, z := range []float64{-20, -10, -9.9 - 1e-8, -9.9, -9.9 + 1e-8, 90 - 1e-8, 90, 90 + 1e-8} {
		points = append(points, point3{X: .01, Y: -.01, Z: z})
	}
	accepted, rejected := 0, 0
	for _, camera := range cameras {
		for _, viewport := range [][2]int{{1280, 720}, {390, 844}, {1, 1}, {0, 0}, {-1, 720}} {
			width, height := viewport[0], viewport[1]
			frame := prepareSceneCameraFrame(camera, width, height)
			aspect := math.Max(.0001, float64(width)/math.Max(1, float64(height)))
			for _, from := range points {
				for _, to := range points {
					segment := sceneCameraSegment{from: from, to: to}
					gotOK := frame.clipSegment(&segment)
					wantFrom, wantTo, wantOK := referenceCameraClipSegment(from, to, camera, aspect)
					if gotOK != wantOK {
						t.Fatalf("camera=%+v viewport=%v segment=%v -> %v: accepted=%v, want %v", camera, viewport, from, to, gotOK, wantOK)
					}
					if !gotOK {
						rejected++
						continue
					}
					accepted++
					assertFramePointBits(t, segment.from, wantFrom)
					assertFramePointBits(t, segment.to, wantTo)
					for _, pair := range [][2]point3{{segment.localFrom, wantFrom}, {segment.localTo, wantTo}} {
						assertFramePointBits(t, pair[0], cameraLocalPoint(pair[1], camera))
						got := frame.projectLocalPoint(pair[0])
						want := referenceCameraProjectPoint(pair[1], camera, width, height)
						if (got == nil) != (want == nil) {
							t.Fatalf("screen depth rejection: got %v, want %v", got, want)
						}
						if got != nil {
							assertFramePointBits(t, point3{X: got.X, Y: got.Y}, point3{X: want.X, Y: want.Y})
						}
					}
				}
				bounds := rootengine.RenderBounds{MinX: from.X - .1, MinY: from.Y - .2, MinZ: from.Z - .3, MaxX: from.X + .1, MaxY: from.Y + .2, MaxZ: from.Z + .3}
				if got, want := frame.boundsOutsideFrustum(bounds), referenceCameraBoundsOutside(bounds, camera, aspect); got != want {
					t.Fatalf("bounds=%+v: culled=%v, want %v", bounds, got, want)
				}
			}
		}
	}
	if accepted == 0 || rejected == 0 {
		t.Fatalf("fixture missed acceptance or rejection: %d accepted, %d rejected", accepted, rejected)
	}
}

// Static geometry survives camera/viewport changes in the world-bake cache; the
// projection must come from the current frame rather than the cached geometry.
func TestSceneCameraFrameRefreshesWithWarmWorldBake(t *testing.T) {
	rt := NewSceneAdapter(staticBoxProgram(), `{}`)
	rt.Reconcile()
	_ = rt.RenderBundle(1280, 720, 0)
	misses := rt.bakeMisses
	for _, viewport := range [][2]int{{390, 844}, {1920, 1080}, {720, 1280}} {
		for _, fov := range []float64{30, 75, 120} {
			rt.props["camera"] = map[string]any{"x": 1.5, "y": .5, "rotationY": .2, "fov": fov}
			hits := rt.bakeHits
			got := rt.RenderBundle(viewport[0], viewport[1], 1)
			fresh := NewSceneAdapter(staticBoxProgram(), `{}`)
			fresh.Reconcile()
			fresh.props["camera"] = rt.props["camera"]
			want := fresh.RenderBundle(viewport[0], viewport[1], 1)
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("cached bundle differs from fresh at viewport=%v fov=%g", viewport, fov)
			}
			if rt.bakeMisses != misses || rt.bakeHits <= hits {
				t.Fatal("camera/viewport change invalidated the world bake")
			}
		}
	}
}

func referenceCameraProjectPoint(point point3, camera sceneCamera, width, height int) *rootengine.RenderPoint {
	local := cameraLocalPoint(point, camera)
	depth := local.Z
	if depth <= camera.Near || depth >= camera.Far {
		return nil
	}
	focal := (math.Min(float64(width), float64(height)) / 2) / math.Tan((camera.FOV*math.Pi)/360)
	return &rootengine.RenderPoint{X: float64(width)/2 + (local.X*focal)/depth, Y: float64(height)/2 - (local.Y*focal)/depth}
}

func referenceCameraClipPoint(point point3, camera sceneCamera, aspect float64) (float64, float64) {
	local := cameraLocalPoint(point, camera)
	focal := 1 / math.Max(.0001, math.Tan((camera.FOV*math.Pi)/360))
	return (local.X * focal / math.Max(local.Z, .0001)) / math.Max(aspect, .0001), local.Y * focal / math.Max(local.Z, .0001)
}

func referenceCameraClipSegment(from, to point3, camera sceneCamera, aspect float64) (point3, point3, bool) {
	depthFrom := cameraLocalPoint(from, camera).Z
	depthTo := cameraLocalPoint(to, camera).Z
	if depthFrom <= camera.Near && depthTo <= camera.Near {
		return point3{}, point3{}, false
	}
	clippedFrom, clippedTo := from, to
	if depthFrom <= camera.Near || depthTo <= camera.Near {
		t := (camera.Near - depthFrom) / math.Max(.000001, depthTo-depthFrom)
		if depthFrom <= camera.Near {
			clippedFrom = lerpPoint3(from, to, t)
		} else {
			clippedTo = lerpPoint3(from, to, t)
		}
	}
	depthFrom = cameraLocalPoint(clippedFrom, camera).Z
	depthTo = cameraLocalPoint(clippedTo, camera).Z
	if depthFrom >= camera.Far && depthTo >= camera.Far {
		return point3{}, point3{}, false
	}
	fromX, fromY := referenceCameraClipPoint(clippedFrom, camera, aspect)
	toX, toY := referenceCameraClipPoint(clippedTo, camera, aspect)
	if (fromX < -1 && toX < -1) || (fromX > 1 && toX > 1) || (fromY < -1 && toY < -1) || (fromY > 1 && toY > 1) {
		return point3{}, point3{}, false
	}
	return clippedFrom, clippedTo, true
}

func referenceCameraBoundsOutside(bounds rootengine.RenderBounds, camera sceneCamera, aspect float64) bool {
	allLeft, allRight, allBottom, allTop, allNear, allFar := true, true, true, true, true, true
	for _, corner := range renderBoundsCorners(bounds) {
		local := cameraLocalPoint(corner, camera)
		allNear = allNear && local.Z <= camera.Near
		allFar = allFar && local.Z >= camera.Far
		x, y := referenceCameraClipPoint(corner, camera, aspect)
		allLeft = allLeft && x < -1
		allRight = allRight && x > 1
		allBottom = allBottom && y < -1
		allTop = allTop && y > 1
	}
	return allNear || allFar || allLeft || allRight || allBottom || allTop
}
