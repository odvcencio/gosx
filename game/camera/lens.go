package camera

import "math"

// Basis is a camera's view frame with no roll: Forward points at the target,
// Right lies level (no Y), and Up completes the frame.
type Basis struct{ Forward, Right, Up Vec3 }

// ViewBasis returns the frame of a camera at eye looking at target, and the
// distance between them. The camera must not look straight up or down.
func ViewBasis(eye, target Vec3) (b Basis, distance float64) {
	d := Vec3{target.X - eye.X, target.Y - eye.Y, target.Z - eye.Z}
	distance = math.Sqrt(d.X*d.X + d.Y*d.Y + d.Z*d.Z)
	f := Vec3{d.X / distance, d.Y / distance, d.Z / distance}
	flat := math.Hypot(f.X, f.Z)
	right := Vec3{-f.Z / flat, 0, f.X / flat}
	return Basis{Forward: f, Right: right, Up: Vec3{-right.Z * f.Y, flat, right.X * f.Y}}, distance
}

// Lens projects world points onto a picture, for labels that follow a point
// in the world. Build one per frame from the pose the camera was given.
type Lens struct {
	eye                  Vec3
	basis                Basis
	focal, width, height float64
}

// Lens returns the pose's lens on a picture width by height pixels. The
// pose's field of view is vertical; roll is ignored.
func (p Pose) Lens(width, height float64) Lens {
	b, _ := ViewBasis(p.Position, p.Target)
	fov := p.FOV
	if fov == 0 {
		fov = DefaultFOV
	}
	return Lens{eye: p.Position, basis: b, focal: height / 2 / math.Tan(fov*math.Pi/360), width: width, height: height}
}

// Project returns where a world point falls on the picture (pixels from the
// top left), how many pixels one world unit covers at its distance, and its
// depth along the view: a point with depth at or below zero is behind the
// camera.
func (l Lens) Project(p Vec3) (x, y, scale, depth float64) {
	d := Vec3{p.X - l.eye.X, p.Y - l.eye.Y, p.Z - l.eye.Z}
	f, r, u := l.basis.Forward, l.basis.Right, l.basis.Up
	depth = d.X*f.X + d.Y*f.Y + d.Z*f.Z
	scale = l.focal / depth
	x = l.width/2 + (d.X*r.X+d.Z*r.Z)*scale
	y = l.height/2 - (d.X*u.X+d.Y*u.Y+d.Z*u.Z)*scale
	return x, y, scale, depth
}

// ClampX keeps a label's x at least margin pixels inside the picture. side is
// -1 when it was held at the left edge, 1 at the right and 0 when it fitted,
// so the label can point toward what it names.
func (l Lens) ClampX(x, margin float64) (clamped float64, side int) {
	switch {
	case x < margin:
		return margin, -1
	case x > l.width-margin:
		return l.width - margin, 1
	}
	return x, 0
}

// FitDistance is how far from its target the camera must stand, looking along
// the same direction with the same lens, for every point to lie inside the
// share marginX of the picture's half width and marginY of its half height.
// aspect is width over height. It is never less than the pose's own distance.
func FitDistance(p Pose, points []Vec3, aspect, marginX, marginY float64) float64 {
	b, distance := ViewBasis(p.Position, p.Target)
	fov := p.FOV
	if fov == 0 {
		fov = DefaultFOV
	}
	tangent := math.Tan(fov * math.Pi / 360)
	dot := func(a, c Vec3) float64 { return a.X*c.X + a.Y*c.Y + a.Z*c.Z }
	need := distance
	for _, q := range points {
		diff := Vec3{q.X - p.Target.X, q.Y - p.Target.Y, q.Z - p.Target.Z}
		along := dot(b.Forward, diff)
		need = math.Max(need, math.Max(
			math.Abs(dot(b.Up, diff))/(tangent*marginY)-along,
			math.Abs(dot(b.Right, diff))/(tangent*aspect*marginX)-along))
	}
	return need
}
