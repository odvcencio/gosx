package scene

import (
	"sort"

	"m31labs.dev/gosx/det"
)

// CrowdAudio is what a crowd sounds like at one moment, for the game's audio
// mixer: how loud it is overall, each section's level and position, and
// where travelling waves are. CrowdDirector.Audio returns it. It carries no
// audio content; the game picks the sounds.
type CrowdAudio struct {
	// Loudness is the fan-weighted crowd noise level, 0 to 1: each fan
	// contributes its section's excitement times its action's loudness
	// (CrowdActionInfo.Loudness).
	Loudness float64
	// Sections lists every section in index order.
	Sections []CrowdSectionAudio
	// Waves lists the running waves' fronts.
	Waves []CrowdWaveAudio
}

// CrowdSectionAudio is one section's contribution.
type CrowdSectionAudio struct {
	ID         string
	Fans       int
	Excitement float64
	Action     CrowdAction
	// Loudness is the section's level, 0 to 1.
	Loudness float64
	// Center is the section's centre in the crowd frame, for panning.
	Center Vector3
	// Synced reports a shared-beat gesture; BPM is its tempo.
	Synced bool
	BPM    float64
}

// CrowdWaveAudio is a running wave's front.
type CrowdWaveAudio struct {
	// Front is the front's azimuth in turns, 0 to 1 (CrowdSeat.Wave).
	Front float64
	// Progress runs from 0 at the start to 1 when the last lap ends.
	Progress float64
}

// CrowdBeat is one audible moment of a synchronized gesture: hands meeting,
// feet landing, a shout.
type CrowdBeat struct {
	// Time is on the director's clock.
	Time    float64
	Section string
	Action  CrowdAction
	// Strength is the share of the crowd's fans making the sound at full
	// excitement, 0 to 1: section fans times excitement over all fans.
	Strength float64
}

// Audio returns the crowd's levels at t.
func (d *CrowdDirector) Audio(t float64) CrowdAudio {
	out := CrowdAudio{Sections: make([]CrowdSectionAudio, len(d.sections))}
	total, weighted := 0, 0.0
	for i, s := range d.sections {
		e := s.excitementAt(t)
		info := crowdActions[s.to]
		// Fans that do not join idle; their murmur still counts.
		joined := crowdJoinShare(e)
		level := crowdMul(joined, crowdMul(e, info.Loudness)) + crowdMul(1-joined, crowdMul(e, crowdActions[0].Loudness))
		sec := CrowdSectionAudio{ID: d.layout[i].ID, Fans: d.layout[i].Count, Excitement: e, Action: info.Action, Loudness: level, Center: d.layout[i].Center, Synced: s.synced}
		if s.synced && info.Cycle > 0 {
			sec.BPM = crowdMul(s.rate/info.Cycle, 60)
		}
		out.Sections[i] = sec
		total += sec.Fans
		weighted += crowdMul(float64(sec.Fans), level)
	}
	if total > 0 {
		out.Loudness = weighted / float64(total)
	}
	for _, w := range d.waves {
		end := crowdWaveEnd(w)
		if t < w.Start || t >= end {
			continue
		}
		front := w.Origin + crowdMul(w.Speed, t-w.Start)
		out.Waves = append(out.Waves, CrowdWaveAudio{Front: front - det.Floor(front), Progress: (t - w.Start) / (end - w.Start)})
	}
	return out
}

// crowdJoinShare is the share of fans that join an action at excitement e:
// the shaders let a fan join when its eagerness (0 to 255) is below
// 64 + 192*e.
func crowdJoinShare(e float64) float64 {
	return det.Min(1, (64+crowdMul(192, e))/256)
}

// Beats returns the synchronized gestures' beats in [from, to), in time
// order. Only sections on a shared beat (Sync) and actions with a sound
// (CrowdActionInfo.Beat >= 0) produce beats; free-running sections are a
// texture of sound, not beats.
func (d *CrowdDirector) Beats(from, to float64) []CrowdBeat {
	total := 0
	for _, l := range d.layout {
		total += l.Count
	}
	var out []CrowdBeat
	for i, s := range d.sections {
		info := crowdActions[s.to]
		if !s.synced || info.Beat < 0 || s.rate <= 0 || total == 0 {
			continue
		}
		period := info.Cycle / s.rate
		start := det.Max(from, s.blendStart)
		k := det.Ceil((start-s.epoch)/period - info.Beat)
		for ; ; k++ {
			at := s.epoch + crowdMul(k+info.Beat, period)
			if at >= to {
				break
			}
			if at < start {
				continue
			}
			e := s.excitementAt(at)
			out = append(out, CrowdBeat{Time: at, Section: d.layout[i].ID, Action: info.Action,
				Strength: float64(d.layout[i].Count) * crowdMul(e, crowdJoinShare(e)) / float64(total)})
		}
	}
	sort.SliceStable(out, func(a, b int) bool { return out[a].Time < out[b].Time })
	return out
}

// CrowdBus is the part of a mixer that crowd noise drives. *audio.Mixer
// (package game/audio) satisfies it, so scene does not import the audio
// package.
type CrowdBus interface {
	SetVolume(bus string, volume float64) bool
}

// CrowdNoise maps crowd loudness onto a mixer bus volume with separate rise
// and fall times, so a roar swells quickly and fades slowly. Use it once per
// frame:
//
//	noise := scene.CrowdNoise{Bus: "crowd", Quiet: 0.15, Loud: 1}
//	noise.Follow(mixer, director.Audio(now), dt)
type CrowdNoise struct {
	// Bus is the mixer bus the crowd bed plays on.
	Bus string
	// Quiet and Loud are the bus volumes at loudness 0 and 1. Loud zero
	// means 1.
	Quiet, Loud float64
	// Rise and Fall are the seconds to cover most of a change upward and
	// downward. Zero means 0.25 and 1.5.
	Rise, Fall float64

	level   float64
	started bool
}

// Follow moves the smoothed level toward audio.Loudness over dt seconds,
// sets the bus volume when bus is not nil, and returns the volume.
func (n *CrowdNoise) Follow(bus CrowdBus, audio CrowdAudio, dt float64) float64 {
	target := clampUnit(audio.Loudness)
	if !n.started {
		n.level, n.started = target, true
	} else {
		tau := n.Fall
		if target > n.level {
			tau = n.Rise
		}
		if tau <= 0 {
			if target > n.level {
				tau = 0.25
			} else {
				tau = 1.5
			}
		}
		k := 1 - det.Exp(-det.Max(dt, 0)/tau)
		n.level += crowdMul(target-n.level, k)
	}
	loud := n.Loud
	if loud == 0 {
		loud = 1
	}
	volume := n.Quiet + crowdMul(loud-n.Quiet, n.level)
	if bus != nil && n.Bus != "" {
		bus.SetVolume(n.Bus, volume)
	}
	return volume
}

// Level returns the smoothed loudness, 0 to 1.
func (n *CrowdNoise) Level() float64 { return n.level }
