package vm

import (
	rootengine "m31labs.dev/gosx/engine"
	"m31labs.dev/gosx/motion"
)

// spinScratch holds the reusable structures for zero-alloc spin evaluation.
// One instance is allocated per SceneAdapter (lazily, on first spinning object)
// and reused across all objects in every frame. The caller must call
// spinQuatUsingScratch serially — no concurrent access.
type spinScratch struct {
	gen *motion.Generator
	tl  *motion.Timeline
	buf *motion.WriteBuf
	// clipBuf is the reusable WriteBuf for per-object clip TRS evaluation. Clips
	// are rare, so it is lazily allocated on the first clipped object and reused
	// across all objects and frames thereafter (zero per-frame alloc once warm).
	clipBuf *motion.WriteBuf
}

// clipWriteBuf returns the lazily-allocated, reusable clip WriteBuf on the
// scratch. A nil scratch yields a fresh standalone buffer (the test/standalone
// path). The buffer grows on demand inside motion.Eval; it is sized generously
// here to cover the common T+R+S write packet without reallocation.
func (sc *spinScratch) clipWriteBuf() *motion.WriteBuf {
	if sc == nil {
		return motion.NewWriteBuf(21)
	}
	if sc.clipBuf == nil {
		sc.clipBuf = motion.NewWriteBuf(21) // 3 writes * [tid,pid,arity] + up to 4 floats
	}
	return sc.clipBuf
}

// newSpinScratch builds the shared skeleton once. TargetID 0 is used as a
// sentinel — the scratch always has exactly one track targeting ID 0.
func newSpinScratch() *spinScratch {
	gen := &motion.Generator{Kind: motion.GenSpin}
	track := &motion.Track{
		Target:   motion.Target{Kind: motion.TargetSceneNode, Ref: "0"},
		Prop:     "rotation",
		Gen:      gen,
		TargetID: 0,
		PropID:   0,
	}
	tl := &motion.Timeline{
		Children: []motion.Positioned{
			{
				At:    motion.Position{Kind: motion.PosAbs},
				Track: track,
			},
		},
	}
	buf := motion.NewWriteBuf(7) // exactly [targetID, propID, arity, x, y, z, w]
	return &spinScratch{gen: gen, tl: tl, buf: buf}
}

// objectClipTRS builds the per-object clip timeline (across all clips) and
// evaluates it at time t, returning the decoded TRS. When no animation channel
// targets the object — the overwhelmingly common case — buildObjectClipTimeline
// returns nil and this returns the zero clipTRS (all Has* false) with no Eval
// call, so the render path stays byte-identical to the pre-clip behaviour.
//
// The reusable clip WriteBuf on the scratch is used (or a standalone buffer when
// sc is nil — the test/standalone path), so the warm path is per-frame alloc-free
// apart from the timeline build itself (clips are rare; building one short-lived
// timeline per animated object per frame is acceptable).
func objectClipTRS(o sceneObject, objIndex int, anims []rootengine.RenderAnimation, t float64, sc *spinScratch) clipTRS {
	if len(anims) == 0 {
		return clipTRS{}
	}
	tl, duration := buildObjectClipTimeline(anims, o.ID, objIndex)
	if tl == nil {
		return clipTRS{}
	}
	return evalClipTRS(tl, duration, t, sc.clipWriteBuf())
}

// spinQuatForObject computes the spin orientation for a scene object at time t
// (seconds) by routing the object's per-axis spin rate through the canonical
// motion evaluator (a GenSpin generator track). The result is a unit quaternion
// equivalent to QuatFromEuler(spinX*t, spinY*t, spinZ*t).
//
// Zero-spin fast path: returns identity immediately (zero alloc, no Eval call).
// Spinning path: reuses the cached scratch on sa (zero per-frame alloc once the
// scratch has been initialised).
func spinQuatForObject(o sceneObject, t float64) motion.Quat {
	// Zero-spin early-return: identity quaternion, no Eval, no alloc.
	if o.SpinX == 0 && o.SpinY == 0 && o.SpinZ == 0 {
		return motion.Quat{X: 0, Y: 0, Z: 0, W: 1}
	}
	return spinQuatUsingScratch(o, t, nil)
}

// spinQuatWithScratch is the zero-alloc variant used by the production bundle
// path. scratch must be non-nil (obtained from newSpinScratch, cached on the
// adapter or per-frame). It mutates the cached generator's Spin in-place and
// calls motion.Eval, which pushes one ArityQuat write to buf.
func spinQuatWithScratch(o sceneObject, t float64, sc *spinScratch) motion.Quat {
	// Zero-spin early-return: identity quaternion, no Eval, no alloc.
	if o.SpinX == 0 && o.SpinY == 0 && o.SpinZ == 0 {
		return motion.Quat{X: 0, Y: 0, Z: 0, W: 1}
	}
	return spinQuatUsingScratch(o, t, sc)
}

// spinQuatUsingScratch runs the evaluator using sc (or a temporary scratch if sc
// is nil — used by the standalone spinQuatForObject path in tests).
func spinQuatUsingScratch(o sceneObject, t float64, sc *spinScratch) motion.Quat {
	if sc == nil {
		// Test / standalone path: allocate a temporary scratch (as before).
		sc = newSpinScratch()
	}
	sc.gen.Spin = [3]float64{o.SpinX, o.SpinY, o.SpinZ}
	sc.buf.Reset()
	motion.Eval(sc.tl, t, motion.Policy{}, sc.buf)

	// A single GenSpin track emits exactly one ArityQuat write packed as
	// [targetID, propID, arity, x, y, z, w].
	f := sc.buf.Writes()
	if len(f) < 7 {
		return motion.Quat{X: 0, Y: 0, Z: 0, W: 1}
	}
	return motion.Quat{X: f[3], Y: f[4], Z: f[5], W: f[6]}
}
