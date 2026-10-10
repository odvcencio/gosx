package scene

import (
	"fmt"
	"math"
	"sort"
)

// SegmentAnimation extracts a time interval from a procedural transform clip,
// inserts sampled boundary keys and retimes it to duration. Channels use the
// graph AnimationClip convention: three-component relative translation/Euler
// rotation, absolute scale, with LINEAR or STEP interpolation. Asset quaternion
// and compressed tracks must be decoded by their asset animation player.
func SegmentAnimation(clip AnimationClipIR, start, end, duration float64) (AnimationClipIR, error) {
	if !animationFinite(start) || !animationFinite(end) || !animationFinite(duration) || start < 0 || end <= start || duration <= 0 {
		return AnimationClipIR{}, fmt.Errorf("invalid animation segment interval")
	}
	out := AnimationClipIR{Name: clip.Name, Duration: duration}
	for _, track := range clip.Channels {
		if !validTransformTrack(track) {
			return AnimationClipIR{}, fmt.Errorf("unsupported or invalid animation channel %q", track.Property)
		}
		next := track
		next.Times = nil
		next.Values = nil
		times := []float64{start}
		for _, at := range track.Times {
			if at > start && at < end {
				times = append(times, at)
			}
		}
		times = append(times, end)
		for _, at := range times {
			v := sampleTransformTrack(track, at)
			next.Times = append(next.Times, (at-start)*duration/(end-start))
			next.Values = append(next.Values, v[:]...)
		}
		out.Channels = append(out.Channels, next)
	}
	return out, nil
}

func animationFinite(v float64) bool { return !math.IsNaN(v) && !math.IsInf(v, 0) }
func validTransformTrack(track AnimationChannelIR) bool {
	if track.Property != "translation" && track.Property != "rotation" && track.Property != "scale" {
		return false
	}
	if track.Interpolation != "" && track.Interpolation != "LINEAR" && track.Interpolation != "STEP" {
		return false
	}
	if len(track.Times) == 0 || len(track.Values) != len(track.Times)*3 || len(track.CompressedTimes) > 0 || len(track.CompressedValues) > 0 {
		return false
	}
	for i, t := range track.Times {
		if !animationFinite(t) || t < 0 || (i > 0 && t <= track.Times[i-1]) {
			return false
		}
	}
	for _, v := range track.Values {
		if !animationFinite(v) {
			return false
		}
	}
	return true
}
func sampleTransformTrack(track AnimationChannelIR, at float64) (v [3]float64) {
	i := sort.Search(len(track.Times), func(i int) bool { return track.Times[i] >= at })
	if i == 0 {
		copy(v[:], track.Values[:3])
		return
	}
	if i == len(track.Times) {
		copy(v[:], track.Values[len(track.Values)-3:])
		return
	}
	if track.Times[i] == at {
		copy(v[:], track.Values[i*3:i*3+3])
		return
	}
	mix := (at - track.Times[i-1]) / (track.Times[i] - track.Times[i-1])
	if track.Interpolation == "STEP" {
		mix = 0
	}
	for j := range v {
		v[j] = track.Values[(i-1)*3+j]*(1-mix) + track.Values[i*3+j]*mix
	}
	return
}

// AnimationPose is a complete transform patch, including explicit zero resets.
type AnimationPose struct {
	X         float64 `json:"x"`
	Y         float64 `json:"y"`
	Z         float64 `json:"z"`
	RotationX float64 `json:"rotationX"`
	RotationY float64 `json:"rotationY"`
	RotationZ float64 `json:"rotationZ"`
	ScaleX    float64 `json:"scaleX"`
	ScaleY    float64 `json:"scaleY"`
	ScaleZ    float64 `json:"scaleZ"`
}

// ObjectAnimationPose supplies an object's settled transform for playback.
func ObjectAnimationPose(o ObjectIR) AnimationPose {
	scale := func(v float64) float64 {
		if v == 0 {
			return 1
		}
		return v
	}
	return AnimationPose{o.X, o.Y, o.Z, o.RotationX, o.RotationY, o.RotationZ, scale(o.ScaleX), scale(o.ScaleY), scale(o.ScaleZ)}
}

// SampleAnimationCommands composes procedural channels over immutable settled
// objects. Unknown targets and malformed channels are ignored. Final reaches
// each channel's exact final key, even when finite playback was skipped.
func SampleAnimationCommands(clips []AnimationClipIR, objects map[string]ObjectIR, at float64, final bool) []Command {
	if !animationFinite(at) {
		return nil
	}
	patches := map[string]AnimationPose{}
	for _, clip := range clips {
		for _, track := range clip.Channels {
			object, ok := objects[track.TargetID]
			if !ok || !validTransformTrack(track) {
				continue
			}
			t := at
			if final {
				t = track.Times[len(track.Times)-1]
			}
			v := sampleTransformTrack(track, t)
			p, ok := patches[object.ID]
			if !ok {
				p = ObjectAnimationPose(object)
			}
			switch track.Property {
			case "translation":
				p.X, p.Y, p.Z = object.X+v[0], object.Y+v[1], object.Z+v[2]
			case "rotation":
				p.RotationX, p.RotationY, p.RotationZ = object.RotationX+v[0], object.RotationY+v[1], object.RotationZ+v[2]
			case "scale":
				p.ScaleX, p.ScaleY, p.ScaleZ = v[0], v[1], v[2]
			}
			patches[object.ID] = p
		}
	}
	ids := make([]string, 0, len(patches))
	for id := range patches {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	commands := make([]Command, 0, len(ids))
	for _, id := range ids {
		commands = append(commands, Command{Kind: CommandSetTransform, ObjectID: id, Data: patches[id]})
	}
	return commands
}
