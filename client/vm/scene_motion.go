package vm

import "m31labs.dev/gosx/motion"

// spinQuatForObject computes the spin orientation for a scene object at time t
// (seconds) by routing the object's per-axis spin rate through the canonical
// motion evaluator (a GenSpin generator track). The result is a unit quaternion
// equivalent to QuatFromEuler(spinX*t, spinY*t, spinZ*t).
//
// The orientation is identical for every vertex of the object, so callers MUST
// compute it once per object per frame (not per vertex) and thread the result
// into translatePoint / sceneObjectWorldNormal.
//
// When the object has no spin (all axes zero), GenSpin yields the identity
// quaternion {0,0,0,1}, which RotateVec3 treats as a no-op.
func spinQuatForObject(o sceneObject, t float64) motion.Quat {
	tl := &motion.Timeline{
		Children: []motion.Positioned{
			{
				At: motion.Position{Kind: motion.PosAbs},
				Track: &motion.Track{
					Target: motion.Target{Kind: motion.TargetSceneNode, Ref: o.ID},
					Prop:   "rotation",
					Gen: &motion.Generator{
						Kind: motion.GenSpin,
						Spin: [3]float64{o.SpinX, o.SpinY, o.SpinZ},
					},
				},
			},
		},
	}

	var buf motion.WriteBuf
	motion.Eval(tl, t, motion.Policy{}, &buf)

	// A single GenSpin track emits exactly one ArityQuat write packed as
	// [targetID, propID, arity, x, y, z, w]. If for any reason nothing was
	// written, fall back to identity.
	f := buf.Writes()
	if len(f) < 7 {
		return motion.Quat{X: 0, Y: 0, Z: 0, W: 1}
	}
	return motion.Quat{X: f[3], Y: f[4], Z: f[5], W: f[6]}
}
