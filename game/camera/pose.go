package camera

import (
	"math"

	"m31labs.dev/gosx/scene"
)

// Pose is a camera placement: where it is, what it looks at, and its lens.
type Pose struct {
	Position Vec3
	Target   Vec3
	// FOV is the vertical field of view in degrees. Zero means 60 when the
	// pose is converted for the scene.
	FOV float64
	// Roll tilts the camera around its view axis, in radians.
	Roll float64
}

// DefaultFOV is used when a Pose has no FOV.
const DefaultFOV = 60.0

// Valid reports whether every number in p is finite and the position differs
// from the target.
func (p Pose) Valid() bool {
	return p.Position.finite() && p.Target.finite() && finite(p.FOV, p.Roll) && p.Position.Sub(p.Target).Len() > 1e-9
}

// LerpPose blends two poses. t is not clamped.
func LerpPose(a, b Pose, t float64) Pose {
	fa, fb := a.FOV, b.FOV
	if fa == 0 {
		fa = DefaultFOV
	}
	if fb == 0 {
		fb = DefaultFOV
	}
	return Pose{
		Position: Lerp(a.Position, b.Position, t),
		Target:   Lerp(a.Target, b.Target, t),
		FOV:      lerp(fa, fb, t),
		Roll:     lerp(a.Roll, b.Roll, t),
	}
}

// Forward returns the unit view direction.
func (p Pose) Forward() Vec3 { return p.Target.Sub(p.Position).Norm() }

// Rotation returns the pitch (X) and yaw (Y) in radians that the Scene3D
// runtime uses for a camera looking from Position to Target: the same
// convention as the runtime's orbit camera, where positive X looks up.
func (p Pose) Rotation() (x, y float64) {
	f := p.Forward()
	h := math.Max(1e-4, math.Hypot(f.X, f.Z))
	return math.Atan2(f.Y, h), math.Atan2(-f.X, -f.Z)
}

// Perspective converts the pose to a scene camera. near and far are the clip
// distances; zero leaves them to the scene default. TransitionMS stays zero,
// because the camera director already moves the pose every frame and a
// renderer-side ease on top would lag it.
func (p Pose) Perspective(near, far float64) scene.PerspectiveCamera {
	rx, ry := p.Rotation()
	fov := p.FOV
	if fov == 0 {
		fov = DefaultFOV
	}
	return scene.PerspectiveCamera{
		Position: scene.Vec3(p.Position.X, p.Position.Y, p.Position.Z),
		Rotation: scene.Rotate(rx, ry, p.Roll),
		FOV:      fov,
		Near:     near,
		Far:      far,
	}
}
