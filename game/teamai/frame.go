package teamai

// Side says how a slot, landmark or path reacts to a frame's strength and
// mirror.
type Side int8

const (
	// SideFixed keeps the authored lateral sign. Only Frame.Mirror flips it.
	SideFixed Side = iota
	// SideStrong puts the lateral offset on the frame's strength side.
	SideStrong
	// SideWeak puts the lateral offset on the side away from the strength.
	SideWeak
)

// String returns the data-file name of s.
func (s Side) String() string {
	switch s {
	case SideStrong:
		return "strong"
	case SideWeak:
		return "weak"
	default:
		return "fixed"
	}
}

// Frame is a movable reference for formations, landmarks and paths: an origin
// spot, the direction the team faces, the side its strength is on, an optional
// mirror, and a lateral band.
//
// Local coordinates in a frame are (lateral, depth): lateral runs along
// Right(), depth along Forward. A frame facing +Y has Right() = +X.
type Frame struct {
	// Origin is the reference spot, such as where play restarts.
	Origin Vec2
	// Forward is the unit direction the team faces. Zero means +Y.
	Forward Vec2
	// Strength is +1 when the strength is on the frame's right, -1 when on
	// its left. Zero means +1.
	Strength int8
	// Mirror flips every lateral offset, strong and weak included.
	Mirror bool
	// Band is the lateral band the origin is in: an index into Spec.Bands, or
	// -1 for none. Slots can carry a per-band offset.
	Band int
}

// Fwd returns the frame's unit forward direction.
func (f *Frame) Fwd() Vec2 {
	if f.Forward.IsZero() {
		return Vec2{Y: 1}
	}
	return f.Forward
}

// Right returns the frame's unit lateral direction: Forward rotated 90 degrees
// clockwise.
func (f *Frame) Right() Vec2 {
	fw := f.Fwd()
	return Vec2{X: fw.Y, Y: -fw.X}
}

// Sign returns the lateral multiplier, +1 or -1, for an offset on side s.
func (f *Frame) Sign(s Side) float64 {
	sign := 1.0
	switch s {
	case SideStrong:
		if f.Strength < 0 {
			sign = -1
		}
	case SideWeak:
		if f.Strength >= 0 {
			sign = -1
		}
	}
	if f.Mirror {
		sign = -sign
	}
	return sign
}

// ToWorld converts local (lateral, depth) coordinates, already signed, to a
// world point.
func (f *Frame) ToWorld(local Vec2) Vec2 {
	return f.Origin.Add(f.Rotate(local))
}

// Rotate converts a local (lateral, depth) direction or offset to world axes
// without moving it to the origin.
func (f *Frame) Rotate(local Vec2) Vec2 {
	fw := f.Fwd()
	right := Vec2{X: fw.Y, Y: -fw.X}
	return Vec2{
		X: float64(right.X*local.X) + float64(fw.X*local.Y),
		Y: float64(right.Y*local.X) + float64(fw.Y*local.Y),
	}
}

// ToLocal converts a world point to local (lateral, depth) coordinates.
func (f *Frame) ToLocal(world Vec2) Vec2 {
	d := world.Sub(f.Origin)
	return Vec2{X: d.Dot(f.Right()), Y: d.Dot(f.Fwd())}
}

// ValidFrame reports whether f can place slots: a finite origin and a unit
// (or zero) forward direction.
func ValidFrame(f *Frame) bool {
	if !f.Origin.finite() || !f.Forward.finite() {
		return false
	}
	if f.Forward.IsZero() {
		return true
	}
	l2 := f.Forward.Len2()
	return l2 > 0.999 && l2 < 1.001
}
