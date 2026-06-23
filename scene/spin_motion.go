package scene

import "m31labs.dev/gosx/motion"

// spinMotionTrack converts a scene Spin (Euler, radians/sec) into a MotionIR
// GenSpin Track targeting the "rotation" property of the named node.
// The Euler X/Y/Z axes map directly to the Generator's Spin[0/1/2] axes.
//
// This is the non-breaking facade over motion core: it is called alongside the
// existing SpinX/SpinY/SpinZ record emission and does NOT replace it.
func spinMotionTrack(spin Euler, targetRef string) motion.Track {
	return motion.Track{
		Target: motion.Target{
			Kind: motion.TargetSceneNode,
			Ref:  targetRef,
		},
		Prop: "rotation",
		Gen: &motion.Generator{
			Kind: motion.GenSpin,
			Spin: [3]float64{spin.X, spin.Y, spin.Z},
		},
	}
}

// SpinMotionTimeline returns a motion.Timeline containing one GenSpin Track for
// every mesh or points node in the scene that has a non-zero Spin. The timeline
// has Autoplay true and infinite loop so it mirrors the existing SpinX/Y/Z
// runtime semantics.
//
// The SpinTracks slice is populated during lowering (Graph.SceneIR / Props.SceneIR)
// and is NOT serialised to JSON (the field carries json:"-"). This is a
// deliberately deferred concern: wire serialisation of MotionIR tracks is Task 1.15+.
func (ir SceneIR) SpinMotionTimeline() *motion.Timeline {
	if len(ir.SpinTracks) == 0 {
		return nil
	}
	children := make([]motion.Positioned, len(ir.SpinTracks))
	for i := range ir.SpinTracks {
		children[i] = motion.Positioned{
			At:    motion.Position{Kind: motion.PosAbs, Val: 0},
			Track: &ir.SpinTracks[i],
		}
	}
	return &motion.Timeline{
		Children: children,
		Loop:     -1, // infinite
		Autoplay: true,
	}
}
