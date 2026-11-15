package animgraph_test

import (
	"fmt"

	"m31labs.dev/gosx/motion/animgraph"
)

// heroGraph is the worked example in docs/scene3d-animation.md: idle, a
// walk/run blend, a one-shot jump, an additive upper-body aim layer, foot IK
// on uneven ground, and a head look-at.
func heroGraph() *animgraph.Program {
	ik, look := animgraph.Humanoid(true)
	return animgraph.Graph{
		Params: []animgraph.Param{
			{Name: "speed", Kind: animgraph.Float},
			{Name: "grounded", Kind: animgraph.Bool, Default: 1},
			{Name: "jump", Kind: animgraph.Trigger},
			{Name: "aim", Kind: animgraph.Bool},
		},
		Clips: []animgraph.Clip{
			{Name: "Idle", Duration: 2, Loop: true,
				Curves: []animgraph.Curve{{Name: "leftFootIK", Keys: []animgraph.CurveKey{{T: 0, V: 1}}}, {Name: "rightFootIK", Keys: []animgraph.CurveKey{{T: 0, V: 1}}}}},
			{Name: "Walk", Duration: 1, Loop: true,
				Events: []animgraph.Event{{Name: "footstep", Time: 0.25}, {Name: "footstep", Time: 0.75}}},
			{Name: "Run", Duration: 0.6, Loop: true,
				Events: []animgraph.Event{{Name: "footstep", Time: 0.15}, {Name: "footstep", Time: 0.45}}},
			{Name: "Jump", Duration: 0.8, Events: []animgraph.Event{{Name: "land", Time: 0.7}}},
			{Name: "Aim", Duration: 1, Loop: true},
		},
		Masks: []animgraph.Mask{{Name: "upperBody", Include: []string{"upperChest"}, Exclude: []string{"neck"}}},
		Layers: []animgraph.Layer{
			{Name: "locomotion", Machine: animgraph.StateMachine{
				Entry: "Idle",
				States: []animgraph.State{
					{Name: "Idle", Clip: "Idle", Transitions: []animgraph.Transition{
						{To: "Move", When: []animgraph.Condition{animgraph.When("speed", animgraph.Greater, 0.1)}, Duration: 0.2},
					}},
					{Name: "Move",
						Blend1D: &animgraph.BlendSpace1D{Param: "speed", ScaleSpeed: true, Points: []animgraph.BlendPoint1D{
							{Clip: "Walk", At: 1.4}, {Clip: "Run", At: 4},
						}},
						Transitions: []animgraph.Transition{
							{To: "Idle", When: []animgraph.Condition{animgraph.When("speed", animgraph.Less, 0.1)}, Duration: 0.25},
						}},
					{Name: "Jump", Clip: "Jump", Transitions: []animgraph.Transition{
						{To: "Idle", When: []animgraph.Condition{animgraph.WhenTrue("grounded")}, ExitTime: 0.9, Duration: 0.15},
					}},
				},
				Any: []animgraph.Transition{
					{To: "Jump", When: []animgraph.Condition{animgraph.WhenTriggered("jump")}, Duration: 0.1},
				},
			}},
			{Name: "aim", Mask: "upperBody", Additive: true, Machine: animgraph.StateMachine{States: []animgraph.State{
				{Name: "Off", Transitions: []animgraph.Transition{{To: "Aim", When: []animgraph.Condition{animgraph.WhenTrue("aim")}, Duration: 0.2}}},
				{Name: "Aim", Clip: "Aim", Transitions: []animgraph.Transition{{To: "Off", When: []animgraph.Condition{animgraph.WhenFalse("aim")}, Duration: 0.2}}},
			}}},
		},
		IK:     ik,
		LookAt: look,
		Pelvis: animgraph.Pelvis{MaxDrop: 0.3},
	}.MustCompile()
}

func Example() {
	prog := heroGraph()
	hero := prog.New("hero") // the scene.Model ID, declared with ClipSampled: true
	leftFoot, rightFoot := prog.IKID("leftFoot"), prog.IKID("rightFoot")
	hero.SetIKWeight(leftFoot, 1)
	hero.SetIKWeight(rightFoot, 1)

	footsteps := 0
	// One second of walking at 60 frames per second, then a jump: the
	// character leaves the ground on frame 60 and is still airborne at 90.
	hero.SetFloat("speed", 1.4)
	for frame := 0; frame < 90; frame++ {
		if frame == 60 {
			hero.SetTrigger("jump")
			hero.SetBool("grounded", false)
		}
		hero.Step(1.0 / 60)
		for _, e := range hero.Events() {
			if e.Kind == animgraph.Notify && e.Name == "footstep" {
				footsteps++ // play a footstep sound, scaled by e.Weight
			}
		}
		// In a browser build, raycast below each foot and send the frame:
		//   if pos, ok := hero.Feedback().AnimatedEnd(leftFoot); ok { hero.SetGround(leftFoot, h, n) }
		//   _ = animgraph.Dispatch("scene", hero)
	}
	fmt.Println("state:", hero.State(0).Name)
	fmt.Println("footsteps:", footsteps)
	fmt.Println("foot IK weight while airborne:", hero.IKWeight(leftFoot))
	// Output:
	// state: Jump
	// footsteps: 2
	// foot IK weight while airborne: 0
}
