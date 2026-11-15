package camera

import "math"

// Source produces a camera pose each frame. A Director blends between
// Sources. dt is in seconds.
type Source interface {
	Update(dt float64) Pose
}

// SourceFunc adapts a function to Source.
type SourceFunc func(dt float64) Pose

// Update implements Source.
func (f SourceFunc) Update(dt float64) Pose { return f(dt) }

// Static is a Source that always returns the same pose.
type Static Pose

// Update implements Source.
func (s Static) Update(float64) Pose { return Pose(s) }

// Follow keeps a moving subject in view from a fixed angle. The focus point
// chases the subject with a critically damped spring, so the camera trails
// smoothly and never overshoots. Set the fields, then call SetTarget and
// Update each frame.
type Follow struct {
	// Yaw and Pitch (radians) place the camera around the focus. See Direction.
	Yaw, Pitch float64
	// Distance from the focus to the camera.
	Distance float64
	// Offset is added to the subject position to get the focus, for example
	// to aim at a character's chest instead of its feet.
	Offset Vec3
	// SmoothTime is how long the focus takes to catch up, in seconds.
	SmoothTime float64
	// LookAhead is how many seconds of subject velocity to lead the focus by.
	LookAhead float64
	// Deadzone is a radius around the focus inside which subject movement
	// does not pull the camera.
	Deadzone float64
	FOV      float64

	target   Vec3
	prev     Vec3
	focus    Vec3
	vel      Vec3
	subjVel  Vec3
	started  bool
	hasPrev  bool
	snapNext bool
}

// SetTarget sets the subject position for the next Update.
func (f *Follow) SetTarget(p Vec3) {
	if p.finite() {
		f.target = p
	}
}

// Snap puts the focus on the subject at the next Update, with no smoothing.
// Use it after a teleport or a scene change.
func (f *Follow) Snap() { f.snapNext = true }

// Update advances the spring and returns the pose.
func (f *Follow) Update(dt float64) Pose {
	if !finite(dt) || dt < 0 {
		dt = 0
	}
	if f.Distance <= 0 {
		f.Distance = 10
	}
	if f.hasPrev && dt > 0 {
		inst := f.target.Sub(f.prev).Scale(1 / dt)
		f.subjVel, _ = SmoothDamp3(f.subjVel, inst, Vec3{}, 0.1, dt)
	}
	f.prev, f.hasPrev = f.target, true
	want := f.target.Add(f.Offset).Add(f.subjVel.Scale(f.LookAhead))
	if !f.started || f.snapNext {
		f.focus, f.vel, f.started, f.snapNext = want, Vec3{}, true, false
	} else {
		if f.Deadzone > 0 {
			d := want.Sub(f.focus)
			if l := d.Len(); l <= f.Deadzone {
				want = f.focus
			} else {
				want = want.Sub(d.Scale(f.Deadzone / l))
			}
		}
		f.focus, f.vel = SmoothDamp3(f.focus, want, f.vel, f.SmoothTime, dt)
	}
	return Pose{
		Position: f.focus.Add(Direction(f.Yaw, f.Pitch).Scale(f.Distance)),
		Target:   f.focus,
		FOV:      f.FOV,
	}
}

// Frame keeps several points in view. It looks from a fixed angle at the
// middle of the points' screen-space bounds and backs away until all of them
// fit inside the margin. Center and distance are smoothed.
type Frame struct {
	Yaw, Pitch float64
	// FOV is the vertical field of view in degrees; zero means DefaultFOV.
	FOV float64
	// Aspect is width over height; zero means 16/9.
	Aspect float64
	// Margin is the fraction of the screen to keep empty on every side, 0..0.45.
	Margin float64
	// MinDistance and MaxDistance bound how close and far the camera goes.
	// MaxDistance zero means no upper bound.
	MinDistance, MaxDistance float64
	// SmoothTime is the spring time for center and distance, in seconds.
	SmoothTime float64

	points  []Vec3
	center  Vec3
	cvel    Vec3
	dist    float64
	dvel    float64
	started bool
	snap    bool
}

// SetPoints replaces the points to frame. The slice is copied; non-finite
// points are skipped.
func (f *Frame) SetPoints(points ...Vec3) {
	f.points = f.points[:0]
	for _, p := range points {
		if p.finite() {
			f.points = append(f.points, p)
		}
	}
}

// Snap skips smoothing at the next Update.
func (f *Frame) Snap() { f.snap = true }

// Fit returns the unsmoothed center and distance that frame the points.
func (f *Frame) Fit() (center Vec3, distance float64) {
	fov := f.FOV
	if fov <= 0 || fov >= 179 {
		fov = DefaultFOV
	}
	aspect := f.Aspect
	if aspect <= 0 {
		aspect = 16.0 / 9.0
	}
	margin := math.Max(0, math.Min(0.45, f.Margin))
	tanV := math.Tan(fov*math.Pi/360) * (1 - 2*margin)
	tanH := tanV * aspect
	back := Direction(f.Yaw, f.Pitch)
	right := Vec3{0, 1, 0}.Cross(back).Norm()
	if right == (Vec3{}) { // looking straight down or up
		right = Vec3{1, 0, 0}
	}
	up := back.Cross(right)
	if len(f.points) == 0 {
		return f.center, math.Max(f.MinDistance, f.dist)
	}
	minX, maxX := math.Inf(1), math.Inf(-1)
	minY, maxY := math.Inf(1), math.Inf(-1)
	for _, p := range f.points {
		x, y := p.Dot(right), p.Dot(up)
		minX, maxX = math.Min(minX, x), math.Max(maxX, x)
		minY, maxY = math.Min(minY, y), math.Max(maxY, y)
	}
	cx, cy := (minX+maxX)/2, (minY+maxY)/2
	// Depth of the center plane along back: use the points' mean depth.
	var cz float64
	for _, p := range f.points {
		cz += p.Dot(back)
	}
	cz /= float64(len(f.points))
	center = right.Scale(cx).Add(up.Scale(cy)).Add(back.Scale(cz))
	// A point at offset (x,y,z) from the center, z toward the camera, is
	// inside the frustum when |x| <= tanH*(D-z) and |y| <= tanV*(D-z).
	for _, p := range f.points {
		x, y, z := p.Dot(right)-cx, p.Dot(up)-cy, p.Dot(back)-cz
		distance = math.Max(distance, math.Abs(x)/tanH+z)
		distance = math.Max(distance, math.Abs(y)/tanV+z)
	}
	distance = math.Max(distance, f.MinDistance)
	if f.MaxDistance > 0 {
		distance = math.Min(distance, f.MaxDistance)
	}
	return center, distance
}

// Update smooths toward the fit and returns the pose.
func (f *Frame) Update(dt float64) Pose {
	if !finite(dt) || dt < 0 {
		dt = 0
	}
	center, dist := f.Fit()
	if len(f.points) == 0 && !f.started {
		dist = math.Max(f.MinDistance, 10)
	}
	if !f.started || f.snap {
		f.center, f.dist, f.cvel, f.dvel, f.started, f.snap = center, dist, Vec3{}, 0, true, false
	} else {
		f.center, f.cvel = SmoothDamp3(f.center, center, f.cvel, f.SmoothTime, dt)
		f.dist, f.dvel = SmoothDamp(f.dist, dist, f.dvel, f.SmoothTime, dt)
	}
	return Pose{
		Position: f.center.Add(Direction(f.Yaw, f.Pitch).Scale(f.dist)),
		Target:   f.center,
		FOV:      f.FOV,
	}
}

// Path is a Source that moves through poses on a Catmull-Rom curve over
// Duration seconds and then holds the last pose. Use it for a tour or a
// fly-through.
type Path struct {
	Poses    []Pose
	Duration float64
	Ease     Ease
	elapsed  float64
}

// Reset restarts the path.
func (p *Path) Reset() { p.elapsed = 0 }

// Done reports whether the path has reached its end.
func (p *Path) Done() bool { return p.Duration <= 0 || p.elapsed >= p.Duration }

// Update implements Source.
func (p *Path) Update(dt float64) Pose {
	n := len(p.Poses)
	if n == 0 {
		return Pose{}
	}
	if finite(dt) && dt > 0 {
		p.elapsed += dt
	}
	if n == 1 || p.Duration <= 0 {
		return p.Poses[n-1]
	}
	t := p.Ease.at(math.Min(1, p.elapsed/p.Duration)) * float64(n-1)
	i := int(math.Min(t, float64(n-2)))
	u := t - float64(i)
	at := func(k int) Pose {
		if k < 0 {
			k = 0
		}
		if k >= n {
			k = n - 1
		}
		return p.Poses[k]
	}
	p0, p1, p2, p3 := at(i-1), at(i), at(i+1), at(i+2)
	cr := func(a, b, c, d float64) float64 {
		return 0.5 * (2*b + (-a+c)*u + (2*a-5*b+4*c-d)*u*u + (-a+3*b-3*c+d)*u*u*u)
	}
	cr3 := func(a, b, c, d Vec3) Vec3 {
		return Vec3{cr(a.X, b.X, c.X, d.X), cr(a.Y, b.Y, c.Y, d.Y), cr(a.Z, b.Z, c.Z, d.Z)}
	}
	fov := func(q Pose) float64 {
		if q.FOV == 0 {
			return DefaultFOV
		}
		return q.FOV
	}
	return Pose{
		Position: cr3(p0.Position, p1.Position, p2.Position, p3.Position),
		Target:   cr3(p0.Target, p1.Target, p2.Target, p3.Target),
		FOV:      cr(fov(p0), fov(p1), fov(p2), fov(p3)),
		Roll:     cr(p0.Roll, p1.Roll, p2.Roll, p3.Roll),
	}
}
