package animgraph

import (
	"errors"
	"fmt"
	"math"
)

// Paired animation: two characters play clips authored together (a tackle,
// a grapple, a handoff, a contested catch). The simulation decides the
// outcome, where the two meet and when; the animation shows it. PairedMove
// plays each role's clip as an action, warps playback time so the authored
// contact frame lands exactly at the decided contact time, warps each
// character's root from where it actually is to where the authored pair
// meets, and drives contact IK (a hand to the other's shoulder).

// Pose is a ground-plane transform: a position and a yaw (radians about +Y,
// 0 facing +Z).
type Pose struct {
	X, Y, Z, Yaw float64
}

// RootKey is one key of an authored root path: the character's root (hips)
// position on the ground plane relative to the pair's alignment root, at clip
// time T in seconds. gltfclip.RootPath reads these from a glTF clip.
type RootKey struct {
	T, X, Z float64
}

// PairRole is one side of a paired clip.
type PairRole struct {
	Name string
	// Clip is the clip this role plays (declared in its own Program).
	Clip string
	// Facing is the role's authored yaw relative to the alignment root.
	Facing float64
	// Path is the authored root path relative to the alignment root, sorted
	// by T. Empty means the root stays at the alignment root.
	Path []RootKey
}

// Placed returns the role with a character-space root path (for example
// from gltfclip.RootPath, where the character faces +Z at its own origin)
// moved into the pair's alignment space: at is where this role's character
// origin stood, relative to the alignment root, when the pair was authored.
// Facing becomes at.Yaw.
func (r PairRole) Placed(at Pose) PairRole {
	out := r
	out.Facing = at.Yaw
	out.Path = make([]RootKey, len(r.Path))
	for i, k := range r.Path {
		x, z := rotateYaw(k.X, k.Z, at.Yaw)
		out.Path[i] = RootKey{T: k.T, X: at.X + x, Z: at.Z + z}
	}
	return out
}

// ContactIK reaches one role's IK chain to a bone of the other role during
// part of the clip.
type ContactIK struct {
	// Role reaches; the other role owns Bone.
	Role int
	// Chain names a TwoBoneIK chain in Role's Program.
	Chain string
	// Bone is a canonical bone of the other role; Offset is in its frame.
	Bone   string
	Offset [3]float64
	// The weight rises from 0 at In to 1 at Full, holds until Out, and
	// falls to 0 at End (clip seconds).
	In, Full, Out, End float64
}

// PairedClip is a two-character animation authored in one shared space.
type PairedClip struct {
	Name string
	// Outcome is the contest result this clip shows, for PairedLibrary.Pick.
	Outcome string
	// Approach is the authored direction role 1 comes from, as a yaw
	// relative to role 0's facing (0: from the front, Pi: from behind).
	Approach float64
	// Contact is the clip time, in seconds, at which the two meet.
	Contact float64
	Roles   [2]PairRole
	IK      []ContactIK
	// Layer receives both actions; BlendIn and BlendOut crossfade them.
	Layer             int
	BlendIn, BlendOut float64
}

// PairedLibrary holds paired clips for selection by contest result.
type PairedLibrary struct {
	Clips []PairedClip
}

// Pick returns the clip for an outcome whose authored approach is closest to
// approach (the actual direction role 1 comes from, relative to role 0's
// facing). Ties, within 0.1 radians, are broken by seed, so a simulation that
// passes its own deterministic seed gets the same pick on every client.
func (l *PairedLibrary) Pick(outcome string, approach float64, seed uint32) (*PairedClip, bool) {
	best, bestDiff := -1, math.Inf(1)
	var ties [16]int
	n := 0
	for i := range l.Clips {
		c := &l.Clips[i]
		if c.Outcome != outcome {
			continue
		}
		diff := math.Abs(angleDiff(approach, c.Approach))
		switch {
		case diff < bestDiff-0.1:
			best, bestDiff = i, diff
			ties[0], n = i, 1
		case diff <= bestDiff+0.1:
			if diff < bestDiff {
				best, bestDiff = i, diff
			}
			if n < len(ties) {
				ties[n] = i
				n++
			}
		}
	}
	if best < 0 {
		return nil, false
	}
	// Keep only ties within 0.1 of the final best.
	m := 0
	for k := 0; k < n; k++ {
		if math.Abs(angleDiff(approach, l.Clips[ties[k]].Approach)) <= bestDiff+0.1 {
			ties[m] = ties[k]
			m++
		}
	}
	return &l.Clips[ties[int(seed)%m]], true
}

// PairStart is what the simulation decided.
type PairStart struct {
	// Poses are the two characters' current world root poses, by role.
	Poses [2]Pose
	// Align is where the alignment root sits in the world at contact.
	Align Pose
	// In is the time, in seconds, from now until contact.
	In float64
	// MinRate and MaxRate bound the time warp. Zero means 0.5 and 2.
	MinRate, MaxRate float64
}

// PairedMove plays one paired clip on two instances.
type PairedMove struct {
	clip      *PairedClip
	inst      [2]*Instance
	start     [2]Pose // alignment frame at clip time 0, per role
	align     Pose
	in        float64
	elapsed   float64
	minRate   float64
	maxRate   float64
	chains    []int
	contacted bool
	done      bool
	time      float64
}

// StartPaired starts c on a (role 0) and b (role 1). Each instance's Program
// must declare its role's clip and the chains c.IK names.
func StartPaired(c *PairedClip, a, b *Instance, s PairStart) (*PairedMove, error) {
	if c == nil || a == nil || b == nil || a == b {
		return nil, errors.New("animgraph: StartPaired needs a clip and two distinct instances")
	}
	if !(c.Contact >= 0) || !finite(s.In) {
		return nil, errors.New("animgraph: paired clip needs a nonnegative Contact and a finite In")
	}
	m := &PairedMove{clip: c, inst: [2]*Instance{a, b}, align: s.Align, in: math.Max(0, s.In), minRate: s.MinRate, maxRate: s.MaxRate}
	if m.minRate <= 0 {
		m.minRate = 0.5
	}
	if m.maxRate <= 0 {
		m.maxRate = 2
	}
	for r := 0; r < 2; r++ {
		role := &c.Roles[r]
		in := m.inst[r]
		if in.prog.ClipID(role.Clip) < 0 {
			return nil, fmt.Errorf("animgraph: role %d clip %q is not declared in its program", r, role.Clip)
		}
		// The alignment frame that puts this role's authored start pose on
		// its actual pose: yaw = actual - facing, position = actual - R(yaw)
		// path(0).
		yaw := s.Poses[r].Yaw - role.Facing
		px, pz := pathAt(role.Path, 0)
		rx, rz := rotateYaw(px, pz, yaw)
		m.start[r] = Pose{X: s.Poses[r].X - rx, Y: s.Poses[r].Y, Z: s.Poses[r].Z - rz, Yaw: yaw}
	}
	m.chains = make([]int, len(c.IK))
	for i, k := range c.IK {
		if k.Role != 0 && k.Role != 1 {
			return nil, fmt.Errorf("animgraph: contact IK %d has role %d", i, k.Role)
		}
		chain := m.inst[k.Role].prog.IKID(k.Chain)
		if chain < 0 {
			return nil, fmt.Errorf("animgraph: contact IK chain %q is not declared for role %d", k.Chain, k.Role)
		}
		if HumanoidBoneID(k.Bone) < 0 {
			return nil, fmt.Errorf("animgraph: contact IK bone %q is not a canonical humanoid bone", k.Bone)
		}
		m.chains[i] = chain
	}
	rate := m.rate(0)
	for r := 0; r < 2; r++ {
		m.inst[r].PlayAction(c.Roles[r].Clip, ActionOptions{Layer: c.Layer, BlendIn: c.BlendIn, BlendOut: c.BlendOut, Rate: rate})
	}
	return m, nil
}

// rate is the playback rate that lands the contact frame on time.
func (m *PairedMove) rate(clipTime float64) float64 {
	if clipTime >= m.clip.Contact {
		return 1
	}
	remaining := m.in - m.elapsed
	if remaining <= 1e-6 {
		return m.maxRate
	}
	return math.Min(m.maxRate, math.Max(m.minRate, (m.clip.Contact-clipTime)/remaining))
}

// Step advances the move by dt seconds. Call it once per frame before the
// instances' Step: it sets both actions' rates and the contact IK.
func (m *PairedMove) Step(dt float64) {
	if m.done || !finite(dt) || dt < 0 {
		return
	}
	a := m.inst[0].Action(m.clip.Layer)
	if !a.Playing && m.elapsed > 0 {
		m.finish()
		return
	}
	m.time = a.Time
	r := m.rate(m.time)
	// Predict this step's clip time so the rate does not overshoot contact.
	if m.time < m.clip.Contact && m.time+dt*r > m.clip.Contact && m.in-m.elapsed > 0 {
		r = math.Min(r, (m.clip.Contact-m.time)/dt)
	}
	for i := range m.inst {
		m.inst[i].SetActionRate(m.clip.Layer, r)
	}
	m.elapsed += dt
	if m.time >= m.clip.Contact-timeEpsilon {
		m.contacted = true
	}
	for i, k := range m.clip.IK {
		in := m.inst[k.Role]
		w := rampWeight(m.time, k.In, k.Full, k.Out, k.End)
		in.SetIKWeight(m.chains[i], w)
		if w > 0 {
			in.SetIKTargetBone(m.chains[i], m.inst[1-k.Role], k.Bone, k.Offset)
		} else {
			in.ClearIKTarget(m.chains[i])
		}
	}
}

func (m *PairedMove) finish() {
	m.done = true
	for i, k := range m.clip.IK {
		m.inst[k.Role].SetIKWeight(m.chains[i], 0)
		m.inst[k.Role].ClearIKTarget(m.chains[i])
	}
}

// Cancel stops both actions (blending out over blendOut seconds) and clears
// the contact IK.
func (m *PairedMove) Cancel(blendOut float64) {
	for i := range m.inst {
		m.inst[i].StopAction(m.clip.Layer, blendOut)
	}
	m.finish()
}

// Done reports that both clips finished.
func (m *PairedMove) Done() bool { return m.done }

// Contacted reports that playback reached the contact frame.
func (m *PairedMove) Contacted() bool { return m.contacted }

// ClipTime returns the paired clips' current time in seconds.
func (m *PairedMove) ClipTime() float64 { return m.inst[0].Action(m.clip.Layer).Time }

// Root returns where role's model belongs this frame: its authored root path
// carried by an alignment frame that moves from the role's actual start to
// the decided alignment root, arriving exactly at contact. Use it as the
// model's position and yaw (models keep scene.Model.RootMotion off, so the
// renderer strips the clip's horizontal root motion).
func (m *PairedMove) Root(role int) Pose {
	if role != 0 && role != 1 {
		return Pose{}
	}
	t := m.inst[0].Action(m.clip.Layer).Time
	if m.done || !m.inst[0].Action(m.clip.Layer).Playing {
		t = m.time
	}
	u := 1.0
	if m.clip.Contact > 0 {
		u = clamp01(t / m.clip.Contact)
	}
	u = u * u * (3 - 2*u) // smoothstep: the warp starts and ends gently
	s, e := m.start[role], m.align
	frame := Pose{
		X:   s.X + (e.X-s.X)*u,
		Y:   s.Y + (e.Y-s.Y)*u,
		Z:   s.Z + (e.Z-s.Z)*u,
		Yaw: s.Yaw + angleDiff(e.Yaw, s.Yaw)*u,
	}
	r := &m.clip.Roles[role]
	px, pz := pathAt(r.Path, t)
	rx, rz := rotateYaw(px, pz, frame.Yaw)
	return Pose{X: frame.X + rx, Y: frame.Y, Z: frame.Z + rz, Yaw: frame.Yaw + r.Facing}
}

func pathAt(path []RootKey, t float64) (float64, float64) {
	if len(path) == 0 {
		return 0, 0
	}
	if t <= path[0].T {
		return path[0].X, path[0].Z
	}
	last := len(path) - 1
	if t >= path[last].T {
		return path[last].X, path[last].Z
	}
	lo, hi := 0, last
	for hi-lo > 1 {
		mid := (lo + hi) / 2
		if path[mid].T <= t {
			lo = mid
		} else {
			hi = mid
		}
	}
	span := path[hi].T - path[lo].T
	if span <= 0 {
		return path[hi].X, path[hi].Z
	}
	f := (t - path[lo].T) / span
	return path[lo].X + (path[hi].X-path[lo].X)*f, path[lo].Z + (path[hi].Z-path[lo].Z)*f
}

// rotateYaw rotates (x, z) by yaw about +Y.
func rotateYaw(x, z, yaw float64) (float64, float64) {
	s, c := math.Sincos(yaw)
	return c*x + s*z, -s*x + c*z
}

// angleDiff returns a - b wrapped to (-Pi, Pi].
func angleDiff(a, b float64) float64 {
	d := math.Mod(a-b, 2*math.Pi)
	if d > math.Pi {
		d -= 2 * math.Pi
	} else if d <= -math.Pi {
		d += 2 * math.Pi
	}
	return d
}

func rampWeight(t, in, full, out, end float64) float64 {
	switch {
	case t <= in || t >= end:
		return 0
	case t < full:
		if full <= in {
			return 1
		}
		return (t - in) / (full - in)
	case t <= out:
		return 1
	default:
		if end <= out {
			return 0
		}
		return (end - t) / (end - out)
	}
}
