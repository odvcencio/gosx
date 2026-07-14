package scene

import (
	"math"
	"testing"
)

func TestApplyOrbitDragGrabMatchesWaterReferenceDirection(t *testing.T) {
	result := ApplyOrbitDrag(OrbitState{}, OrbitDragInput{
		DeltaX: 10, DeltaY: 5,
		RotateMode: ControlRotateModePixelDegrees, RotateDirection: ControlRotateDirectionGrab,
		RotateSpeed: 1,
	})
	if math.Abs(result.DeltaYaw-(-10*math.Pi/180)) > 1e-12 {
		t.Fatalf("grab yaw = %v, want -10 degrees", result.DeltaYaw)
	}
	if math.Abs(result.DeltaPitch-(-5*math.Pi/180)) > 1e-12 {
		t.Fatalf("grab pitch = %v, want -5 degrees", result.DeltaPitch)
	}
	if result.After.Yaw >= result.Before.Yaw || result.After.Pitch >= result.Before.Pitch {
		t.Fatalf("grab drag is inverted: %#v", result)
	}
}

func TestApplyOrbitDragClampsPitch(t *testing.T) {
	result := ApplyOrbitDrag(OrbitState{}, OrbitDragInput{
		DeltaY: -1000, RotateMode: ControlRotateModePixelDegrees,
		RotateDirection: ControlRotateDirectionGrab, PitchLimit: 0.75,
	})
	if result.After.Pitch != 0.75 {
		t.Fatalf("pitch = %v, want 0.75", result.After.Pitch)
	}
}

func TestIntersectRayPlaneMatchesManagedRuntimeMath(t *testing.T) {
	hit, ok := IntersectRayPlane(
		Ray{Origin: Vec3(0.5, 0.25, -2), Direction: Vec3(0, 0, 1)},
		Vec3(0, 0, 0), Vec3(0, 0, 1),
	)
	if !ok || hit.Distance != 2 || hit.Point != (Vector3{X: 0.5, Y: 0.25}) {
		t.Fatalf("plane hit = %#v, %v", hit, ok)
	}
	if _, ok := IntersectRayPlane(
		Ray{Origin: Vec3(0, 0, 1), Direction: Vec3(1, 0, 0)},
		Vec3(0, 0, 0), Vec3(0, 0, 1),
	); ok {
		t.Fatal("parallel ray unexpectedly hit plane")
	}
}

func TestCameraFacingDragPlaneNormalMatchesClientEulerOrder(t *testing.T) {
	rotation := Euler{X: 0.4363323129985825, Y: -0.3577924966588374}
	got := CameraFacingDragPlaneNormal(rotation)
	want := Vector3{
		X: math.Cos(rotation.X) * math.Sin(rotation.Y),
		Y: -math.Sin(rotation.X),
		Z: math.Cos(rotation.X) * math.Cos(rotation.Y),
	}
	if vectorDistance(got, want) > 1e-12 {
		t.Fatalf("camera drag normal = %#v, want %#v", got, want)
	}
}

func TestApplyObjectDragAdvancesHitAndClampsToPoolVolume(t *testing.T) {
	state := ObjectDragState{
		Position: Vec3(0.7, -0.75, 0.7), PreviousHit: Vec3(0, 0, 0), PlaneNormal: Vec3(0, 0, 1),
	}
	result := ApplyObjectDrag(state, ObjectDragInput{
		Ray: Ray{Origin: Vec3(2, -2, -1), Direction: Vec3(0, 0, 1)},
		Bounds: ObjectDragBounds{
			Width: 1, Height: 1, Length: 1,
			XLimitRadius: 0.25, ZLimitRadius: 0.25, FloorClearance: 0.25,
		},
	})
	if !result.Applied || !result.WasClamped || result.NextHit == nil {
		t.Fatalf("object drag evidence = %#v", result)
	}
	if result.Delta != (Vector3{X: 2, Y: -2}) {
		t.Fatalf("drag delta = %#v", result.Delta)
	}
	if result.After.Position != (Vector3{X: 0.75, Y: -0.75, Z: 0.7}) {
		t.Fatalf("clamped position = %#v", result.After.Position)
	}
	if result.After.PreviousHit != result.NextHit.Point {
		t.Fatalf("previous hit did not advance: %#v", result)
	}
}

func TestWaterObjectDragMatchesLiveRuntimeSampleWithoutBrowser(t *testing.T) {
	const (
		width    = 960.0
		height   = 709.0
		pointerX = 420.7795323787576
		pointerY = 371.64719208985764
	)
	camera := OrbitCameraForTarget(PerspectiveCamera{
		Position: Vec3(1.2695827068526726, 1.1904730469627978, 3.395653196065958),
		FOV:      45, Near: 0.01, Far: 100,
	}, Vec3(0, -0.5, 0))
	startRay := ScreenToRay(pointerX, pointerY, width, height, camera)
	startHit, ok := IntersectRaySphere(startRay, Vec3(-0.4, -0.75, 0.2), 0.31)
	if !ok {
		t.Fatal("native pointer ray missed the managed interaction sphere")
	}
	result := ApplyObjectDrag(ObjectDragState{
		Position: Vec3(-0.4, -0.75, 0.2), PreviousHit: startHit.Point,
		PlaneNormal: CameraFacingDragPlaneNormal(camera.Rotation),
	}, ObjectDragInput{
		Ray: ScreenToRay(pointerX+40, pointerY, width, height, camera),
		Bounds: ObjectDragBounds{
			Width: 1, Height: 1, Length: 1,
			XLimitRadius: 0.25, ZLimitRadius: 0.25, FloorClearance: 0.25,
		},
	})
	// Captured from the same sample in the live managed runtime. This proves
	// screen projection, analytic hit testing, drag-plane movement, and bounds
	// semantics remain numerically aligned without requiring a browser in CI.
	want := Vec3(-0.22080812527741356, -0.7499999999999998, 0.2669970966469351)
	if !result.Applied || vectorDistance(result.After.Position, want) > 1e-9 {
		t.Fatalf("native drag position = %#v, want live runtime %#v (evidence %#v)", result.After.Position, want, result)
	}
}

func vectorDistance(a, b Vector3) float64 {
	return math.Hypot(math.Hypot(a.X-b.X, a.Y-b.Y), a.Z-b.Z)
}
