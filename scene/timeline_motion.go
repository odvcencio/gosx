package scene

import (
	"fmt"
	"strings"

	"m31labs.dev/gosx/motion"
)

type timelineMotionLeaf struct {
	at    float64
	value motion.SignalRef
}

// BindMotion binds this plan's node and camera properties to a motion program.
// Input supplies absolute seconds, usually from Program.Playhead. Existing
// MotionScope and MotionControl constructs own playback, seeking and disposal;
// no imperative scene playback or application script is needed.
//
// The compiler reuses ordinary track, map, step and mix signals. Gaps hold the
// preceding value, cuts take effect at their exact start, and reverse seeks
// reconstruct the pose. Names are scoped under "<timeline ID>.motion"; a name
// collision or invalid program leaves program unchanged. The plan is not mutated.
func (t *Timeline) BindMotion(program *motion.Program, input motion.SignalRef, sceneSelector string) error {
	if program == nil {
		return fmt.Errorf("scene timeline motion requires a program")
	}
	if strings.TrimSpace(string(input)) == "" || strings.TrimSpace(sceneSelector) == "" {
		return fmt.Errorf("scene timeline motion requires an input and scene selector")
	}
	player, err := NewTimelinePlayer(t)
	if err != nil {
		return err
	}
	prefix := strings.TrimSpace(t.ID) + ".motion"
	if prefix == ".motion" {
		return fmt.Errorf("scene timeline motion requires a nonempty timeline ID")
	}
	// Stage append-only authoring work so validation errors cannot partially
	// alter the caller's program or its backing slices.
	next := *program
	next.Signals = append([]motion.SignalSpec(nil), program.Signals...)
	next.Bindings = append([]motion.Binding(nil), program.Bindings...)
	type group struct {
		target Tween
		leaves []timelineMotionLeaf
	}
	var groups []group
	indices := make(map[string]int)
	for i, tween := range player.tracks {
		id := fmt.Sprintf("%s.%d", prefix, i)
		start := tween.At.Seconds()
		track := motion.Track{Ease: tween.Ease}
		clock := input
		if tween.Duration == 0 {
			track.Interp = motion.InterpStep
			track.Keys = []motion.Key{
				{T: start - 1, Value: motion.ScalarV(tween.From)},
				{T: start, Value: motion.ScalarV(tween.To)},
			}
		} else {
			// Translate seconds before sampling, preserving the authored duration
			// even when a very short tween starts late in a long presentation.
			clock = next.Map(id+".time", input, 0, 1, -start, 1-start)
			track.Keys = []motion.Key{
				{T: 0, Value: motion.ScalarV(tween.From)},
				{T: tween.Duration.Seconds(), Value: motion.ScalarV(tween.To)},
			}
		}
		value, err := next.SampleTrack(id+".value", clock, track)
		if err != nil {
			return err
		}
		key := tweenKey(tween)
		index, exists := indices[key]
		if !exists {
			index = len(groups)
			indices[key] = index
			groups = append(groups, group{target: tween})
		}
		groups[index].leaves = append(groups[index].leaves, timelineMotionLeaf{at: start, value: value})
	}
	selection := 0
	var selectValue func([]timelineMotionLeaf) (motion.SignalRef, error)
	selectValue = func(leaves []timelineMotionLeaf) (motion.SignalRef, error) {
		if len(leaves) == 1 {
			return leaves[0].value, nil
		}
		// A balanced selection tree keeps long same-property sequences shallow.
		mid := len(leaves) / 2
		left, err := selectValue(leaves[:mid])
		if err != nil {
			return "", err
		}
		right, err := selectValue(leaves[mid:])
		if err != nil {
			return "", err
		}
		id := fmt.Sprintf("%s.select.%d", prefix, selection)
		selection++
		at := leaves[mid].at
		gate, err := next.SampleTrack(id+".gate", input, motion.Track{
			Interp: motion.InterpStep,
			Keys: []motion.Key{
				{T: at - 1, Value: motion.ScalarV(0)},
				{T: at, Value: motion.ScalarV(1)},
			},
		})
		if err != nil {
			return "", err
		}
		return next.Mix(id+".value", left, right, gate), nil
	}
	for _, group := range groups {
		value, err := selectValue(group.leaves)
		if err != nil {
			return err
		}
		property := timelineMotionProperty(group.target.Property)
		if group.target.Camera {
			next.BindCamera(value, strings.TrimSpace(sceneSelector), property)
		} else {
			next.BindSceneNode(value, strings.TrimSpace(sceneSelector), group.target.Node, property)
		}
	}
	if _, err := next.Marshal(); err != nil {
		return fmt.Errorf("scene timeline motion: %w", err)
	}
	*program = next
	return nil
}

func timelineMotionProperty(property string) string {
	switch property {
	case "x", "y", "z":
		return "position." + property
	case "rotationX", "rotationY", "rotationZ":
		return "rotation." + strings.ToLower(property[len(property)-1:])
	case "scaleX", "scaleY", "scaleZ":
		return "scale." + strings.ToLower(property[len(property)-1:])
	default:
		return property
	}
}
