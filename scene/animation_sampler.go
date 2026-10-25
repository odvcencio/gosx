package scene

import "sort"

// AnimationSampler owns a validated snapshot of procedural transform tracks
// and their settled poses. Prepare it once per playback, then sample without
// rescanning the scene, validating keys or sorting targets on each frame.
// It retains no object geometry. Samples are independent and may run concurrently.
// Changes to authored tracks or settled objects require a new sampler.
type AnimationSampler struct {
	targets []animationSampleTarget
}

type animationSampleTarget struct {
	id     string
	base   AnimationPose
	tracks []animationSampleTrack
}

type animationSampleTrack struct {
	channel AnimationChannelIR
	offset  [3]float64
}

// NewAnimationSampler preserves SampleAnimationCommands' composition order,
// relative translation/rotation and absolute scale. Unknown targets and
// malformed channels are ignored. Key streams are copied so later authoring
// changes cannot alter an in-flight playback.
func NewAnimationSampler(clips []AnimationClipIR, objects map[string]ObjectIR) *AnimationSampler {
	targets := map[string]*animationSampleTarget{}
	for _, clip := range clips {
		for _, track := range clip.Channels {
			object, ok := objects[track.TargetID]
			if !ok || !validTransformTrack(track) {
				continue
			}
			target := targets[object.ID]
			if target == nil {
				target = &animationSampleTarget{id: object.ID, base: ObjectAnimationPose(object)}
				targets[object.ID] = target
			}
			owned := AnimationChannelIR{
				Property: track.Property, Interpolation: track.Interpolation,
				Times: append([]float64(nil), track.Times...), Values: append([]float64(nil), track.Values...),
			}
			offset := [3]float64{}
			switch track.Property {
			case "translation":
				offset = [3]float64{object.X, object.Y, object.Z}
			case "rotation":
				offset = [3]float64{object.RotationX, object.RotationY, object.RotationZ}
			}
			target.tracks = append(target.tracks, animationSampleTrack{channel: owned, offset: offset})
		}
	}
	sampler := &AnimationSampler{targets: make([]animationSampleTarget, 0, len(targets))}
	//gosx:allow map-range targets are sorted by id on the next line
	for _, target := range targets {
		sampler.targets = append(sampler.targets, *target)
	}
	sort.Slice(sampler.targets, func(i, j int) bool { return sampler.targets[i].id < sampler.targets[j].id })
	return sampler
}

// Sample returns complete, sorted transform commands. Final reaches every
// channel's exact final key, including bounded or skipped playback. Nonfinite
// sample times produce no commands, matching SampleAnimationCommands.
func (s *AnimationSampler) Sample(at float64, final bool) []Command {
	if s == nil || !animationFinite(at) {
		return nil
	}
	commands := make([]Command, 0, len(s.targets))
	for _, target := range s.targets {
		pose := target.base
		for _, track := range target.tracks {
			t := at
			if final {
				t = track.channel.Times[len(track.channel.Times)-1]
			}
			v := sampleTransformTrack(track.channel, t)
			switch track.channel.Property {
			case "translation":
				pose.X, pose.Y, pose.Z = track.offset[0]+v[0], track.offset[1]+v[1], track.offset[2]+v[2]
			case "rotation":
				pose.RotationX, pose.RotationY, pose.RotationZ = track.offset[0]+v[0], track.offset[1]+v[1], track.offset[2]+v[2]
			case "scale":
				pose.ScaleX, pose.ScaleY, pose.ScaleZ = v[0], v[1], v[2]
			}
		}
		commands = append(commands, Command{Kind: CommandSetTransform, ObjectID: target.id, Data: pose})
	}
	return commands
}
