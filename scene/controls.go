package scene

import "math"

const (
	ControlRotateModeViewport     = "viewport"
	ControlRotateModePixelDegrees = "pixel-degrees"
	ControlRotateDirectionOrbit   = "orbit"
	ControlRotateDirectionGrab    = "grab"
)

const (
	defaultOrbitPitchLimit = 1.4
	maximumOrbitPitchLimit = math.Pi/2 - (math.Pi/180)*0.001
)

// OrbitState is the backend-independent angular state of an orbit camera.
type OrbitState struct {
	Yaw   float64 `json:"yaw"`
	Pitch float64 `json:"pitch"`
}

// OrbitDragInput describes one pointer delta in the same units accepted by a
// Scene3D control contract. It is intentionally DOM-free so native harnesses
// and agents can certify interaction semantics without a browser.
type OrbitDragInput struct {
	DeltaX          float64 `json:"deltaX"`
	DeltaY          float64 `json:"deltaY"`
	ViewportWidth   float64 `json:"viewportWidth,omitempty"`
	ViewportHeight  float64 `json:"viewportHeight,omitempty"`
	RotateMode      string  `json:"rotateMode,omitempty"`
	RotateDirection string  `json:"rotateDirection,omitempty"`
	RotateSpeed     float64 `json:"rotateSpeed,omitempty"`
	PitchLimit      float64 `json:"pitchLimit,omitempty"`
}

// OrbitDragResult is exact semantic evidence for one applied drag.
type OrbitDragResult struct {
	Before     OrbitState `json:"before"`
	After      OrbitState `json:"after"`
	DeltaYaw   float64    `json:"deltaYaw"`
	DeltaPitch float64    `json:"deltaPitch"`
}

// RayPlaneHit is a deterministic world-space intersection used by direct
// manipulation controls. Distance is measured in units of the supplied ray
// direction, matching the browser runtime's interaction math.
type RayPlaneHit struct {
	Distance float64 `json:"distance"`
	Point    Vector3 `json:"point"`
}

// ObjectDragState is the persistent portion of a camera-facing object drag.
// PreviousHit advances after every accepted sample, exactly like the managed
// Scene3D fluid-object controller.
type ObjectDragState struct {
	Position    Vector3 `json:"position"`
	PreviousHit Vector3 `json:"previousHit"`
	PlaneNormal Vector3 `json:"planeNormal"`
}

// ObjectDragBounds describes the reusable pool/volume clamp contract. Radii
// reserve enough space to keep the manipulated object inside the volume.
type ObjectDragBounds struct {
	Width          float64 `json:"width"`
	Height         float64 `json:"height"`
	Length         float64 `json:"length"`
	XLimitRadius   float64 `json:"xLimitRadius,omitempty"`
	ZLimitRadius   float64 `json:"zLimitRadius,omitempty"`
	FloorClearance float64 `json:"floorClearance,omitempty"`
	MaxY           float64 `json:"maxY,omitempty"`
}

// ObjectDragInput is one pointer ray sample against the drag plane.
type ObjectDragInput struct {
	Ray    Ray              `json:"ray"`
	Bounds ObjectDragBounds `json:"bounds"`
}

// ObjectDragResult is complete browser-independent evidence for an attempted
// direct-manipulation sample.
type ObjectDragResult struct {
	Before     ObjectDragState `json:"before"`
	After      ObjectDragState `json:"after"`
	NextHit    *RayPlaneHit    `json:"nextHit,omitempty"`
	Delta      Vector3         `json:"delta,omitempty"`
	Unclamped  Vector3         `json:"unclamped,omitempty"`
	Applied    bool            `json:"applied"`
	WasClamped bool            `json:"wasClamped,omitempty"`
}

// ApplyOrbitDrag applies the public Scene3D orbit contract in pure Go. "grab"
// tracks a grabbed scene and therefore subtracts pointer deltas, matching the
// JeantimeX/Evan Wallace water interaction. The historical "orbit" direction
// remains the default for existing scenes.
func ApplyOrbitDrag(state OrbitState, input OrbitDragInput) OrbitDragResult {
	speed := input.RotateSpeed
	if speed < 0.1 {
		speed = 1
	}
	direction := 1.0
	switch input.RotateDirection {
	case ControlRotateDirectionGrab, "track", "direct":
		direction = -1
	}

	var deltaYaw, deltaPitch float64
	if input.RotateMode == ControlRotateModePixelDegrees {
		pixelRadians := math.Pi / 180 * speed
		deltaYaw = input.DeltaX * pixelRadians * direction
		deltaPitch = input.DeltaY * pixelRadians
	} else {
		width := math.Max(1, input.ViewportWidth)
		height := math.Max(1, input.ViewportHeight)
		deltaYaw = input.DeltaX / width * math.Pi * speed * direction
		deltaPitch = input.DeltaY / height * math.Pi * speed
	}

	pitchLimit := input.PitchLimit
	if pitchLimit == 0 {
		pitchLimit = defaultOrbitPitchLimit
	}
	pitchLimit = math.Max(0.1, math.Min(maximumOrbitPitchLimit, pitchLimit))
	after := OrbitState{
		Yaw:   state.Yaw + deltaYaw,
		Pitch: math.Max(-pitchLimit, math.Min(pitchLimit, state.Pitch+deltaPitch)),
	}
	return OrbitDragResult{Before: state, After: after, DeltaYaw: deltaYaw, DeltaPitch: deltaPitch}
}

// IntersectRayPlane mirrors Scene3D's client ray-plane intersection contract.
func IntersectRayPlane(ray Ray, point, normal Vector3) (RayPlaneHit, bool) {
	denom := dotVector(normal, ray.Direction)
	if math.Abs(denom) < 1e-9 {
		return RayPlaneHit{}, false
	}
	distance := dotVector(normal, subVectors(point, ray.Origin)) / denom
	if math.IsNaN(distance) || math.IsInf(distance, 0) || distance < 0 {
		return RayPlaneHit{}, false
	}
	return RayPlaneHit{
		Distance: distance,
		Point:    addVectors(ray.Origin, multiplyVector(ray.Direction, Vector3{X: distance, Y: distance, Z: distance})),
	}, true
}

// IntersectRaySphere exposes the analytic sphere probe used by direct
// manipulation. Center is world-space and direction need not be normalized.
func IntersectRaySphere(ray Ray, center Vector3, radius float64) (RayHit, bool) {
	direction := normalizeVector(ray.Direction)
	if direction == (Vector3{}) || radius <= 0 {
		return RayHit{}, false
	}
	localRay := Ray{Origin: subVectors(ray.Origin, center), Direction: direction}
	hit, ok := intersectSphere(localRay, radius)
	if !ok {
		return RayHit{}, false
	}
	hit.Point = addVectors(hit.Point, center)
	hit.Normal = normalizeVector(subVectors(hit.Point, center))
	hit.Method = "analytic-sphere"
	return hit, true
}

// OrbitCameraForTarget derives the effective camera rotation produced when
// orbit controls first adopt an authored camera. Scene3D's render contract
// stores camera Z opposite world Z, so this mirrors the runtime conversion.
func OrbitCameraForTarget(camera PerspectiveCamera, target Vector3) PerspectiveCamera {
	worldPosition := Vector3{X: camera.Position.X, Y: camera.Position.Y, Z: -camera.Position.Z}
	forward := subVectors(target, worldPosition)
	horizontal := math.Max(0.0001, math.Hypot(forward.X, forward.Z))
	camera.Rotation = Euler{
		X: -math.Atan2(forward.Y, horizontal),
		Y: math.Atan2(forward.X, forward.Z),
	}
	return camera
}

// ScreenToRay converts a CSS-pixel pointer sample into the exact world-space
// perspective ray used by Scene3D picking and managed interactions.
func ScreenToRay(pointerX, pointerY, width, height float64, camera PerspectiveCamera) Ray {
	width = math.Max(1, width)
	height = math.Max(1, height)
	fov := camera.FOV
	if fov == 0 {
		fov = 75
	}
	focal := (height / 2) / math.Tan(fov*math.Pi/360)
	localDirection := Vector3{
		X: (pointerX - width/2) / focal,
		Y: (height/2 - pointerY) / focal,
		Z: 1,
	}
	return Ray{
		Origin:    Vector3{X: camera.Position.X, Y: camera.Position.Y, Z: -camera.Position.Z},
		Direction: normalizeVector(rotateControlPoint(localDirection, camera.Rotation)),
	}
}

// CameraFacingDragPlaneNormal rotates camera-local +Z into world space using
// the same X→Y→Z Euler order as the browser runtime.
func CameraFacingDragPlaneNormal(rotation Euler) Vector3 {
	normal := rotateControlPoint(Vector3{Z: 1}, rotation)
	return normalizeVector(normal)
}

// ApplyObjectDrag advances one camera-facing direct-manipulation sample and
// applies the same pool-volume clamping used by the managed water controls.
func ApplyObjectDrag(state ObjectDragState, input ObjectDragInput) ObjectDragResult {
	result := ObjectDragResult{Before: state, After: state}
	hit, ok := IntersectRayPlane(input.Ray, state.PreviousHit, state.PlaneNormal)
	if !ok {
		return result
	}
	delta := subVectors(hit.Point, state.PreviousHit)
	unclamped := addVectors(state.Position, delta)
	afterPosition := clampObjectDragPosition(unclamped, input.Bounds)
	result.NextHit = &hit
	result.Delta = delta
	result.Unclamped = unclamped
	result.Applied = true
	result.WasClamped = afterPosition != unclamped
	result.After.Position = afterPosition
	result.After.PreviousHit = hit.Point
	return result
}

func clampObjectDragPosition(position Vector3, bounds ObjectDragBounds) Vector3 {
	limitX := math.Max(0, bounds.Width-math.Max(0, bounds.XLimitRadius))
	limitZ := math.Max(0, bounds.Length-math.Max(0, bounds.ZLimitRadius))
	maxY := bounds.MaxY
	if maxY == 0 {
		maxY = 10
	}
	position.X = math.Max(-limitX, math.Min(limitX, position.X))
	position.Y = math.Max(bounds.FloorClearance-bounds.Height, math.Min(maxY, position.Y))
	position.Z = math.Max(-limitZ, math.Min(limitZ, position.Z))
	return position
}

func rotateControlPoint(point Vector3, rotation Euler) Vector3 {
	sinX, cosX := math.Sin(rotation.X), math.Cos(rotation.X)
	point.Y, point.Z = point.Y*cosX-point.Z*sinX, point.Y*sinX+point.Z*cosX
	sinY, cosY := math.Sin(rotation.Y), math.Cos(rotation.Y)
	point.X, point.Z = point.X*cosY+point.Z*sinY, -point.X*sinY+point.Z*cosY
	sinZ, cosZ := math.Sin(rotation.Z), math.Cos(rotation.Z)
	point.X, point.Y = point.X*cosZ-point.Y*sinZ, point.X*sinZ+point.Y*cosZ
	return point
}

// AxisDragParameter returns the parameter t along an axis line (origin +
// t*direction) of the point closest to the pointer ray — the core of a
// translate/scale gizmo drag. It reports false when the ray is parallel to
// the axis and no unique parameter exists. Mirrored by the client gizmo drag
// controller so interactive drags and headless tests share one definition.
func AxisDragParameter(ray Ray, axisOrigin, axisDirection Vector3) (float64, bool) {
	axis := normalizeVector(axisDirection)
	dir := normalizeVector(ray.Direction)
	if vectorLength(axis) == 0 || vectorLength(dir) == 0 {
		return 0, false
	}
	// Closest points between two lines: solve for t on the axis line.
	w := subVectors(ray.Origin, axisOrigin)
	b := dotVector(axis, dir)
	denominator := 1 - b*b
	if math.Abs(denominator) < 1e-12 {
		return 0, false
	}
	d := dotVector(axis, w)
	e := dotVector(dir, w)
	return (b*e - d) / denominator * -1, true
}

// RingDragAngle intersects the pointer ray with the ring's plane and returns
// the angle of the hit around the plane normal, measured from the plane's
// reference X direction. Reports false when the ray is parallel to the plane.
func RingDragAngle(ray Ray, center, normal Vector3) (float64, bool) {
	hit, ok := IntersectRayPlane(ray, center, normal)
	if !ok {
		return 0, false
	}
	point := hit.Point
	n := normalizeVector(normal)
	reference := Vector3{X: 1}
	if math.Abs(dotVector(reference, n)) > 0.999 {
		reference = Vector3{Y: 1}
	}
	tangentX := normalizeVector(subVectors(reference, scaleVector(n, dotVector(reference, n))))
	tangentY := Vector3{
		X: n.Y*tangentX.Z - n.Z*tangentX.Y,
		Y: n.Z*tangentX.X - n.X*tangentX.Z,
		Z: n.X*tangentX.Y - n.Y*tangentX.X,
	}
	local := subVectors(point, center)
	return math.Atan2(dotVector(local, tangentY), dotVector(local, tangentX)), true
}
