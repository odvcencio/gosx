package replay

import (
	"math"
	"sort"

	"m31labs.dev/gosx/game/camera"
	"m31labs.dev/gosx/scene"
)

// CameraMode says who drives the replay camera.
type CameraMode uint8

const (
	// Broadcast follows the subject, or shows a named shot, through a
	// camera.Director.
	Broadcast CameraMode = iota
	// Orbit circles a point the viewer can move.
	Orbit
	// Fly moves freely, like a spectator camera.
	Fly
)

func (m CameraMode) String() string {
	switch m {
	case Orbit:
		return "orbit"
	case Fly:
		return "fly"
	default:
		return "broadcast"
	}
}

// RigConfig sets up a replay camera.
type RigConfig struct {
	// Subject returns the world point the broadcast camera follows. alpha is
	// the Player's interpolation fraction, so the point can be blended between
	// the last two ticks. It reports false when there is nothing to follow;
	// the camera then holds its last focus. Ignored when Base is set.
	Subject func(alpha float64) (camera.Vec3, bool)
	// Follow tunes the broadcast follow camera. Zero Distance means 14, zero
	// SmoothTime 0.25 s, zero FOV 60.
	Follow camera.Follow
	// Base replaces the follow camera as the broadcast base source, for
	// example a *camera.Frame that keeps several bodies in view. It must be a
	// comparable value such as a pointer, because the Director compares sources.
	Base camera.Source
	// Shots are named fixed poses the broadcast camera can cut or blend to.
	Shots map[string]camera.Pose
	// Blend is the default move between modes and shots. Zero Duration means
	// 0.6 s with the smooth ease. Set a negative Duration to cut.
	Blend camera.Blend
	// Director passes reduced motion and shake settings to the Director.
	Director camera.DirectorConfig
	// Near and Far are the clip distances for Camera.
	Near, Far float64
	// Sensitivity scales Look and Orbit input. Zero means 1.
	Sensitivity float64
	// MoveSpeed is the fly speed in world units per second. Zero means 8.
	MoveSpeed float64
}

// Rig is the replay camera: a free orbit or fly camera and the Director's
// broadcast shots, switchable at any time. Every switch blends from the pose
// on screen, so it never pops. It reads input as plain numbers, so a keyboard,
// mouse or gamepad layer (game/actions) can drive it.
//
// A Rig is not safe for concurrent use.
type Rig struct {
	cfg      RigConfig
	dir      *camera.Director
	follow   *camera.Follow
	base     camera.Source
	free     *freeSource
	mode     CameraMode
	shot     string // "" is the chase camera
	shotKeys []string
	alpha    float64
}

// NewRig builds a camera in Broadcast mode following the subject.
func NewRig(cfg RigConfig) *Rig {
	if cfg.Blend.Duration == 0 {
		cfg.Blend.Duration = 0.6
	}
	if cfg.Blend.Duration < 0 {
		cfg.Blend.Duration = 0
	}
	if cfg.Sensitivity == 0 {
		cfg.Sensitivity = 1
	}
	if cfg.MoveSpeed == 0 {
		cfg.MoveSpeed = 8
	}
	r := &Rig{cfg: cfg, free: &freeSource{}}
	if cfg.Base != nil {
		r.base = cfg.Base
	} else {
		f := cfg.Follow
		if f.Distance == 0 {
			f.Distance = 14
		}
		if f.SmoothTime == 0 {
			f.SmoothTime = 0.25
		}
		if f.Pitch == 0 {
			f.Pitch = 0.6
		}
		r.follow = &f
		r.base = (*chase)(r)
	}
	r.dir = camera.NewDirector(cfg.Director, r.base)
	for name, pose := range cfg.Shots {
		r.dir.Bookmark(name, pose)
		r.shotKeys = append(r.shotKeys, name)
	}
	sort.Strings(r.shotKeys)
	return r
}

// Director returns the Director, for sequences, shake and bookmarks beyond
// the configured shots.
func (r *Rig) Director() *camera.Director { return r.dir }

// Mode is the active camera mode.
func (r *Rig) Mode() CameraMode { return r.mode }

// Shot is the active named shot in Broadcast mode, or "" for the chase camera.
func (r *Rig) Shot() string { return r.shot }

// Shots lists the shot names in the order NextShot cycles them.
func (r *Rig) Shots() []string { return append([]string(nil), r.shotKeys...) }

// Snap makes the follow camera jump to the subject at the next Update. Call it
// after a seek, where the subject may have moved far from the camera.
func (r *Rig) Snap() {
	if r.follow != nil {
		r.follow.Snap()
	}
}

// SetMode switches mode, blending from the current pose. Orbit and Fly start
// where the camera is now: Orbit around the current target, Fly at the
// current position.
func (r *Rig) SetMode(m CameraMode) {
	if m == r.mode {
		return
	}
	cur := r.dir.Pose()
	prev := r.mode
	r.mode = m
	switch m {
	case Broadcast:
		r.shot = ""
		r.dir.Release(r.cfg.Blend)
	case Orbit, Fly:
		if prev == Broadcast || !r.free.set {
			r.free.fromPose(cur)
		} else {
			r.free.reframe(m == Orbit)
		}
		r.free.orbit = m == Orbit
		r.dir.Take(r.free, r.cfg.Blend)
	}
}

// ShowShot switches to Broadcast and blends to the named shot. An empty name
// or an unknown one returns to the chase camera; it reports whether the named
// shot exists.
func (r *Rig) ShowShot(name string) bool {
	if r.mode != Broadcast {
		r.mode = Broadcast
	}
	if name == "" {
		r.shot = ""
		r.dir.Release(r.cfg.Blend)
		return true
	}
	if !r.dir.GoTo(name, r.cfg.Blend) {
		r.shot = ""
		r.dir.Release(r.cfg.Blend)
		return false
	}
	r.shot = name
	return true
}

// NextShot steps through chase, then each shot, then chase again, and returns
// the name now shown ("" for chase). delta is 1 or -1.
func (r *Rig) NextShot(delta int) string {
	n := len(r.shotKeys) + 1
	i := 0
	for k, s := range r.shotKeys {
		if s == r.shot {
			i = k + 1
		}
	}
	i = ((i+delta)%n + n) % n
	name := ""
	if i > 0 {
		name = r.shotKeys[i-1]
	}
	r.ShowShot(name)
	return name
}

// Look turns the camera by yaw and pitch input (radians before sensitivity).
// In Orbit it moves around the target; in Fly it turns in place. In Broadcast
// it does nothing.
func (r *Rig) Look(dyaw, dpitch float64) {
	if r.mode == Broadcast {
		return
	}
	s := r.cfg.Sensitivity
	r.free.yaw += dyaw * s
	r.free.pitch = clampF(r.free.pitch+dpitch*s, -1.5, 1.5)
}

// Zoom moves the Orbit camera toward or away from its target by a fraction of
// its distance: positive zooms out. In Fly it changes the field of view.
func (r *Rig) Zoom(amount float64) {
	switch r.mode {
	case Orbit:
		r.free.dist = clampF(r.free.dist*math.Exp(amount), 0.5, 500)
	case Fly:
		r.free.fov = clampF(r.free.fov+amount*20, 20, 100)
	}
}

// Move sets the fly or pan velocity as fractions of MoveSpeed in the camera's
// frame: forward, right and up each in [-1, 1]. Call it every frame with the
// held input; the next Update applies it for that frame's dt. In Orbit it pans
// the target.
func (r *Rig) Move(forward, right, up float64) {
	if r.mode == Broadcast {
		r.free.mv = camera.Vec3{}
		return
	}
	r.free.mv = camera.Vec3{X: clampF(right, -1, 1), Y: clampF(up, -1, 1), Z: clampF(forward, -1, 1)}
	r.free.speed = r.cfg.MoveSpeed
}

// Update advances the camera by dt seconds of wall time and returns the pose.
// Use wall time, not playback time, so the camera stays live while the replay
// is paused or slowed. alpha is Player.Alpha, for the subject blend.
func (r *Rig) Update(dt, alpha float64) camera.Pose {
	r.alpha = alpha
	return r.dir.Update(dt)
}

// Pose is the pose from the last Update.
func (r *Rig) Pose() camera.Pose { return r.dir.Pose() }

// Camera converts the last pose for the Scene3D renderer.
func (r *Rig) Camera() scene.PerspectiveCamera {
	return r.dir.Pose().Perspective(r.cfg.Near, r.cfg.Far)
}

func clampF(v, lo, hi float64) float64 { return math.Max(lo, math.Min(hi, v)) }

// freeSource is the camera.Source behind Orbit and Fly.
type freeSource struct {
	set        bool
	orbit      bool
	yaw, pitch float64 // camera.Direction convention: the camera sits along it from the target
	dist       float64
	target     camera.Vec3 // orbit center, or fly position's look point
	pos        camera.Vec3 // fly position
	fov        float64
	mv         camera.Vec3
	speed      float64
}

// fromPose starts the free camera at pose.
func (f *freeSource) fromPose(p camera.Pose) {
	f.set = true
	back := p.Position.Sub(p.Target)
	d := back.Len()
	if d < 1e-6 {
		d, back = 1, camera.Vec3{Z: 1}
	}
	b := back.Scale(1 / d)
	f.yaw = math.Atan2(b.X, b.Z)
	f.pitch = math.Asin(clampF(b.Y, -1, 1))
	f.dist, f.target, f.pos, f.fov = d, p.Target, p.Position, p.FOV
	if f.fov == 0 {
		f.fov = camera.DefaultFOV
	}
}

// reframe keeps the view while switching between orbit and fly.
func (f *freeSource) reframe(toOrbit bool) {
	dir := camera.Direction(f.yaw, f.pitch)
	if toOrbit {
		f.target = f.pos.Sub(dir.Scale(f.dist))
	} else {
		f.pos = f.target.Add(dir.Scale(f.dist))
	}
}

// Update implements camera.Source.
func (f *freeSource) Update(dt float64) camera.Pose {
	if dt < 0 || math.IsNaN(dt) {
		dt = 0
	}
	dir := camera.Direction(f.yaw, f.pitch) // from the target toward the camera
	forward := dir.Scale(-1)
	right := forward.Cross(camera.Vec3{Y: 1}).Norm()
	up := right.Cross(forward).Norm()
	step := f.mv.Scale(f.speed * dt)
	move := right.Scale(step.X).Add(up.Scale(step.Y)).Add(forward.Scale(step.Z))
	if f.orbit {
		// Pan scales with distance so it feels the same near and far.
		f.target = f.target.Add(move.Scale(math.Max(0.2, f.dist/10)))
		f.pos = f.target.Add(dir.Scale(f.dist))
		return camera.Pose{Position: f.pos, Target: f.target, FOV: f.fov}
	}
	f.pos = f.pos.Add(move)
	f.target = f.pos.Add(forward.Scale(f.dist))
	return camera.Pose{Position: f.pos, Target: f.target, FOV: f.fov}
}

// chase is the broadcast base source. It is a pointer type because a Director
// compares sources and panics on uncomparable ones such as a func.
type chase Rig

// Update implements camera.Source.
func (c *chase) Update(dt float64) camera.Pose {
	r := (*Rig)(c)
	if r.cfg.Subject != nil {
		if p, ok := r.cfg.Subject(r.alpha); ok {
			r.follow.SetTarget(p)
		}
	}
	return r.follow.Update(dt)
}
