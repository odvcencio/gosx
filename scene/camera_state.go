package scene

import (
	"math"
	"time"

	"m31labs.dev/gosx/motion"
)

// CameraIR lowers a typed perspective camera to the Scene3D wire convention.
func CameraIR(camera PerspectiveCamera) IRCamera { return cameraToIR(camera) }

// Perspective returns the typed perspective lens and pose of a camera snapshot.
func (camera IRCamera) Perspective() PerspectiveCamera {
	return PerspectiveCamera{Position: Vec3(camera.X, camera.Y, camera.Z), Rotation: Rotate(camera.RotationX, camera.RotationY, camera.RotationZ), FOV: camera.FOV, PortraitFOV: camera.PortraitFOV, Near: camera.Near, Far: camera.Far, TransitionMS: camera.TransitionMS}
}

// SameCameraPose compares effective poses, excluding transition metadata.
func SameCameraPose(a, b IRCamera, epsilon float64) bool {
	if a.Kind != b.Kind || math.IsNaN(epsilon) || epsilon < 0 {
		return false
	}
	pairs := [][2]float64{{a.X, b.X}, {a.Y, b.Y}, {a.Z, b.Z}, {a.RotationX, b.RotationX}, {a.RotationY, b.RotationY}, {a.RotationZ, b.RotationZ}, {a.Near, b.Near}, {a.Far, b.Far}}
	if a.Kind == "orthographic" {
		pairs = append(pairs, [2]float64{a.Left, b.Left}, [2]float64{a.Right, b.Right}, [2]float64{a.Top, b.Top}, [2]float64{a.Bottom, b.Bottom}, [2]float64{a.Zoom, b.Zoom})
	} else {
		pairs = append(pairs, [2]float64{a.FOV, b.FOV}, [2]float64{a.PortraitFOV, b.PortraitFOV})
	}
	for _, pair := range pairs {
		if math.IsNaN(pair[0]) || math.IsNaN(pair[1]) || math.IsInf(pair[0], 0) || math.IsInf(pair[1], 0) || math.Abs(pair[0]-pair[1]) > epsilon {
			return false
		}
	}
	return true
}

// CameraTransition is a finite camera move. Start an interrupted move from its
// effective sampled pose. FOVArc optionally widens the lens at the midpoint;
// choosing that framing margin belongs to the application, not the renderer.
type CameraTransition struct {
	From, To IRCamera
	Duration time.Duration
	FOVArc   float64
}

// Sample returns an exact endpoint once complete, with no active transition.
func (move CameraTransition) Sample(elapsed time.Duration) (IRCamera, bool) {
	target := move.To
	target.TransitionMS = 0
	if move.Duration <= 0 || elapsed >= move.Duration || move.From.Kind != move.To.Kind {
		return target, false
	}
	t := (motion.Ease{Kind: motion.EaseCubicBezier, Args: []float64{.4, 0, .2, 1}}).Apply(math.Max(0, float64(elapsed)/float64(move.Duration)))
	mix := func(a, b float64) float64 { return a + (b-a)*t }
	c := target
	c.X, c.Y, c.Z = mix(move.From.X, c.X), mix(move.From.Y, c.Y), mix(move.From.Z, c.Z)
	c.RotationX, c.RotationY, c.RotationZ = mix(move.From.RotationX, c.RotationX), mix(move.From.RotationY, c.RotationY), mix(move.From.RotationZ, c.RotationZ)
	c.FOV = mix(move.From.FOV, c.FOV) + math.Max(0, move.FOVArc)*4*t*(1-t)
	c.PortraitFOV = mix(move.From.PortraitFOV, c.PortraitFOV)
	c.Left, c.Right = mix(move.From.Left, c.Left), mix(move.From.Right, c.Right)
	c.Top, c.Bottom = mix(move.From.Top, c.Top), mix(move.From.Bottom, c.Bottom)
	c.Zoom = mix(move.From.Zoom, c.Zoom)
	c.Near, c.Far = mix(move.From.Near, c.Near), mix(move.From.Far, c.Far)
	return c, true
}

// completeCamera preserves zero-value resets in commands. IRCamera remains a
// compact scene record; sparse maps remain sparse patches for timeline callers.
func completeCamera(c IRCamera) map[string]any {
	if c.Kind == "" {
		c.Kind = "perspective"
	}
	if c.FOV == 0 {
		c.FOV = 75
	}
	if c.Zoom == 0 {
		c.Zoom = 1
	}
	if c.Near == 0 {
		c.Near = .05
	}
	if c.Far == 0 {
		c.Far = 128
	}
	return map[string]any{"kind": c.Kind, "x": c.X, "y": c.Y, "z": c.Z, "rotationX": c.RotationX, "rotationY": c.RotationY, "rotationZ": c.RotationZ, "fov": c.FOV, "portraitFOV": c.PortraitFOV, "left": c.Left, "right": c.Right, "top": c.Top, "bottom": c.Bottom, "zoom": c.Zoom, "near": c.Near, "far": c.Far, "transitionMS": c.TransitionMS}
}
