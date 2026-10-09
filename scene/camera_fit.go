package scene

import (
	"fmt"
	"math"
)

// PerspectiveFit defines a fixed-pitch perspective composition in normalized
// viewport coordinates. Applications supply only the points they want framed;
// excluded furniture, hidden meshes and private state need never enter the fit.
type PerspectiveFit struct {
	Aspect, Pitch, FOV       float64
	Left, Right, Top, Bottom float64
	Near, Far, TransitionMS  float64
	// Anchor, when present, is kept at AnchorY subject to the framing bounds.
	Anchor   *Vector3
	AnchorY  float64
	MinDepth float64
}

// FitPerspectivePoints finds the closest camera enclosing every supplied point
// with the requested screen margins. Pitch is camera elevation in radians.
func FitPerspectivePoints(points []Vector3, o PerspectiveFit) (PerspectiveCamera, error) {
	finite := func(v float64) bool { return !math.IsNaN(v) && !math.IsInf(v, 0) }
	for _, v := range []float64{o.Aspect, o.Pitch, o.FOV, o.Left, o.Right, o.Top, o.Bottom, o.Near, o.Far, o.MinDepth, o.AnchorY} {
		if !finite(v) {
			return PerspectiveCamera{}, fmt.Errorf("camera fit requires finite values")
		}
	}
	if len(points) == 0 || o.Aspect <= 0 || o.FOV <= 0 || o.FOV >= 180 || o.Left < 0 || o.Right > 1 || o.Left >= .5 || o.Right <= .5 || o.Top < 0 || o.Bottom > 1 || o.Top >= .5 || o.Bottom <= .5 {
		return PerspectiveCamera{}, fmt.Errorf("invalid perspective fit bounds")
	}
	sn, cs, tangent := math.Sin(o.Pitch), math.Cos(o.Pitch), math.Tan(o.FOV*math.Pi/360)
	a, b := 2*tangent*(.5-o.Top), 2*tangent*(o.Bottom-.5)
	left, right := 2*tangent*o.Aspect*(.5-o.Left), 2*tangent*o.Aspect*(o.Right-.5)
	minX, maxX := points[0].X, points[0].X
	vCamera, maxA, minB := math.Inf(-1), math.Inf(-1), math.Inf(1)
	maxLeft, minRight := math.Inf(-1), math.Inf(1)
	for _, p := range points {
		if !finite(p.X) || !finite(p.Y) || !finite(p.Z) {
			return PerspectiveCamera{}, fmt.Errorf("camera fit requires finite points")
		}
		minX, maxX = math.Min(minX, p.X), math.Max(maxX, p.X)
		u, v := p.Y*cs-p.Z*sn, p.Y*sn+p.Z*cs
		vCamera = math.Max(vCamera, v+math.Max(o.MinDepth, o.Near))
		maxLeft, minRight = math.Max(maxLeft, p.X+right*v), math.Min(minRight, p.X-left*v)
		maxA, minB = math.Max(maxA, u+a*v), math.Min(minB, u-b*v)
	}
	vCamera = math.Max(vCamera, (maxLeft-minRight)/(left+right))
	vCamera = math.Max(vCamera, (maxA-minB)/(a+b))
	x := math.Max(maxLeft-right*vCamera, math.Min(minRight+left*vCamera, (minX+maxX)/2))
	uMin, uMax := maxA-a*vCamera, minB+b*vCamera
	uCamera := (uMin + uMax) / 2
	if o.Anchor != nil {
		p := *o.Anchor
		if !finite(p.X) || !finite(p.Y) || !finite(p.Z) {
			return PerspectiveCamera{}, fmt.Errorf("camera fit requires finite anchor")
		}
		u, v := p.Y*cs-p.Z*sn, p.Y*sn+p.Z*cs
		uCamera = math.Max(uMin, math.Min(uMax, u+2*tangent*(o.AnchorY-.5)*(vCamera-v)))
	}
	return PerspectiveCamera{Position: Vec3(x, vCamera*sn+uCamera*cs, vCamera*cs-uCamera*sn), Rotation: Rotate(o.Pitch, 0, 0), FOV: o.FOV, Near: o.Near, Far: o.Far, TransitionMS: o.TransitionMS}, nil
}

// CameraRay projects CSS coordinates through an effective Scene3D camera in
// native world coordinates (looking along local -Z). Unlike compatibility
// ScreenToRay, this does not mirror the world's Z axis. Invalid/outside samples
// fail closed, which makes it suitable for pointer capture and drag release.
func CameraRay(c IRCamera, x, y, width, height float64) (Ray, error) {
	for _, v := range []float64{x, y, width, height, c.X, c.Y, c.Z, c.RotationX, c.RotationY, c.RotationZ, c.FOV, c.PortraitFOV} {
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return Ray{}, fmt.Errorf("non-finite camera ray")
		}
	}
	if width <= 0 || height <= 0 || x < 0 || y < 0 || x > width || y > height {
		return Ray{}, fmt.Errorf("pointer outside a valid viewport")
	}
	fov := c.FOV
	if height > width && c.PortraitFOV > 0 {
		fov = c.PortraitFOV
	}
	if fov <= 0 || fov >= 180 || c.Kind != "perspective" {
		return Ray{}, fmt.Errorf("camera ray requires a perspective lens")
	}
	t := math.Tan(fov * math.Pi / 360)
	v := rotateControlPoint(Vec3((2*x/width-1)*width/height*t, (1-2*y/height)*t, -1), Rotate(c.RotationX, c.RotationY, c.RotationZ))
	return Ray{Origin: Vec3(c.X, c.Y, c.Z), Direction: normalizeVector(v)}, nil
}
