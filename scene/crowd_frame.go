package scene

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"
)

// CrowdFrame carries section changes and travelling waves for one mounted
// Crowd. A CrowdDirector builds frames; scene.DispatchCrowdFrame (js/wasm) or
// window.__gosx.scene3d.dispatchCrowdFrame(target, EncodeCrowdFrame(frame))
// applies them. The browser keeps every section's state, so a frame lists
// only the sections that changed.
//
// Times are on the sender's clock. The browser fixes the offset between that
// clock and the scene clock from the first frame a mount receives, and keeps
// it until the two drift apart by more than a quarter second.
type CrowdFrame struct {
	// Crowd is the Crowd.ID.
	Crowd string
	// Now is the sender's clock when the frame was built, in seconds.
	Now float64
	// Sections are the full states of the sections that changed.
	Sections []CrowdSectionFrame
	// Waves start travelling waves. A mount runs at most four at a time; a
	// new wave takes the slot of a finished one, or the oldest.
	Waves []CrowdWaveFrame
	// ReplaceWaves stops every running wave before Waves start.
	ReplaceWaves bool
}

// CrowdSectionFrame is one section's state: the GPU blends every fan from
// From to To over Blend seconds from BlendStart, each fan a little later than
// the last, and ramps the section's excitement from ExcitementFrom to
// ExcitementTo over ExcitementRamp seconds from ExcitementStart.
type CrowdSectionFrame struct {
	// Section is the crowd-wide section index (CrowdSectionLayout.Index).
	Section int
	From    CrowdAction
	To      CrowdAction
	// BlendStart and Blend time the change.
	BlendStart, Blend float64
	// ExcitementFrom and ExcitementTo, 0 to 1, decide how many fans join the
	// action; the rest idle.
	ExcitementFrom, ExcitementTo    float64
	ExcitementStart, ExcitementRamp float64
	// Rate is the playback rate; 1 plays each action at its library cycle.
	Rate float64
	// Synced runs every fan on one shared phase: cycle k starts at
	// Epoch + k * cycle / Rate. Jitter spreads fans by up to that many cycles.
	Synced bool
	Epoch  float64
	Jitter float64
	// Props shows the section's props.
	Props bool
}

// CrowdWaveFrame starts one travelling wave. Fans rise as the front passes
// them, in order of azimuth around the crowd origin (CrowdSeat.Wave).
type CrowdWaveFrame struct {
	// Start is when the front leaves Origin, on the sender's clock.
	Start float64
	// Origin is the front's starting azimuth in turns, 0 to 1, measured from
	// +X toward +Z.
	Origin float64
	// Speed is in turns per second; a negative speed runs the other way.
	Speed float64
	// Width is how far around the crowd, in turns, a fan stays risen.
	Width float64
	// Laps is how many times the front goes round. Zero means 1.
	Laps float64
}

var crowdFrameMagic = [4]byte{'G', 'S', 'C', '1'}

const (
	crowdSectionRecordBytes = 42
	crowdWaveRecordBytes    = 20
	crowdMaxWaves           = 4
	crowdFlagSynced         = 1
	crowdFlagProps          = 4
)

// EncodeCrowdFrame writes a GSC1 frame:
//
//	"GSC1"
//	u16 crowd ID length + UTF-8 bytes
//	f64 now
//	u16 section count, then per section (42 bytes):
//	    u16 section, u8 from action, u8 to action,
//	    f32 blend start - now, f32 blend seconds,
//	    f32 excitement from, f32 excitement to, f32 rate,
//	    f32 epoch - now, f32 jitter,
//	    f32 excitement ramp start - now, f32 excitement ramp seconds,
//	    u8 flags (1 synced, 4 props), u8 0
//	u8 wave count (at most 4), u8 replace waves (0 or 1),
//	then per wave (20 bytes): f32 start - now, f32 origin, f32 speed, f32 width, f32 laps
//
// All values are little-endian. Times travel as offsets from now, so a large
// sender clock keeps millisecond precision.
func EncodeCrowdFrame(f CrowdFrame) ([]byte, error) {
	if len(f.Crowd) == 0 || len(f.Crowd) > math.MaxUint16 {
		return nil, errors.New("scene: crowd frame needs a crowd ID")
	}
	if len(f.Sections) > math.MaxUint16 {
		return nil, errors.New("scene: crowd frame has too many sections")
	}
	if len(f.Waves) > crowdMaxWaves {
		return nil, fmt.Errorf("scene: crowd frame has %d waves; the limit is %d", len(f.Waves), crowdMaxWaves)
	}
	if !finite(f.Now) {
		return nil, errors.New("scene: crowd frame time is not finite")
	}
	out := make([]byte, 0, 4+2+len(f.Crowd)+10+len(f.Sections)*crowdSectionRecordBytes+2+len(f.Waves)*crowdWaveRecordBytes)
	out = append(out, crowdFrameMagic[:]...)
	out = binary.LittleEndian.AppendUint16(out, uint16(len(f.Crowd)))
	out = append(out, f.Crowd...)
	out = binary.LittleEndian.AppendUint64(out, math.Float64bits(f.Now))
	out = binary.LittleEndian.AppendUint16(out, uint16(len(f.Sections)))
	f32 := func(v float64) { out = binary.LittleEndian.AppendUint32(out, math.Float32bits(float32(v))) }
	for _, s := range f.Sections {
		from, ok1 := crowdActionIndex(s.From)
		to, ok2 := crowdActionIndex(s.To)
		if !ok1 || !ok2 {
			return nil, fmt.Errorf("scene: crowd frame section %d has an unknown action", s.Section)
		}
		if s.Section < 0 || s.Section >= CrowdMaxSections {
			return nil, fmt.Errorf("scene: crowd frame section index %d out of range", s.Section)
		}
		for _, v := range []float64{s.BlendStart, s.Blend, s.ExcitementFrom, s.ExcitementTo, s.Rate, s.Epoch, s.Jitter, s.ExcitementStart, s.ExcitementRamp} {
			if !finite(v) {
				return nil, fmt.Errorf("scene: crowd frame section %d has a non-finite value", s.Section)
			}
		}
		out = binary.LittleEndian.AppendUint16(out, uint16(s.Section))
		out = append(out, byte(from), byte(to))
		f32(s.BlendStart - f.Now)
		f32(s.Blend)
		f32(s.ExcitementFrom)
		f32(s.ExcitementTo)
		f32(s.Rate)
		f32(s.Epoch - f.Now)
		f32(s.Jitter)
		f32(s.ExcitementStart - f.Now)
		f32(s.ExcitementRamp)
		var flags byte
		if s.Synced {
			flags |= crowdFlagSynced
		}
		if s.Props {
			flags |= crowdFlagProps
		}
		out = append(out, flags, 0)
	}
	replace := byte(0)
	if f.ReplaceWaves {
		replace = 1
	}
	out = append(out, byte(len(f.Waves)), replace)
	for _, w := range f.Waves {
		for _, v := range []float64{w.Start, w.Origin, w.Speed, w.Width, w.Laps} {
			if !finite(v) {
				return nil, errors.New("scene: crowd frame wave has a non-finite value")
			}
		}
		f32(w.Start - f.Now)
		f32(w.Origin)
		f32(w.Speed)
		f32(w.Width)
		f32(w.Laps)
	}
	return out, nil
}

// DecodeCrowdFrame parses a GSC1 frame. Times come back on the sender's
// clock, rounded to float32 offsets from Now.
func DecodeCrowdFrame(data []byte) (CrowdFrame, error) {
	var f CrowdFrame
	at := 0
	need := func(n int) error {
		if at+n > len(data) {
			return errors.New("scene: crowd frame is truncated")
		}
		return nil
	}
	if err := need(6); err != nil {
		return f, err
	}
	if [4]byte(data[:4]) != crowdFrameMagic {
		return f, errors.New("scene: not a GSC1 crowd frame")
	}
	at = 4
	n := int(binary.LittleEndian.Uint16(data[at:]))
	at += 2
	if err := need(n + 10); err != nil {
		return f, err
	}
	f.Crowd = string(data[at : at+n])
	at += n
	f.Now = math.Float64frombits(binary.LittleEndian.Uint64(data[at:]))
	at += 8
	count := int(binary.LittleEndian.Uint16(data[at:]))
	at += 2
	f32 := func() float64 {
		v := float64(math.Float32frombits(binary.LittleEndian.Uint32(data[at:])))
		at += 4
		return v
	}
	for i := 0; i < count; i++ {
		if err := need(crowdSectionRecordBytes); err != nil {
			return f, err
		}
		s := CrowdSectionFrame{Section: int(binary.LittleEndian.Uint16(data[at:]))}
		from, to := int(data[at+2]), int(data[at+3])
		if from >= len(crowdActions) || to >= len(crowdActions) {
			return f, errors.New("scene: crowd frame names an unknown action")
		}
		s.From, s.To = crowdActions[from].Action, crowdActions[to].Action
		at += 4
		s.BlendStart = f.Now + f32()
		s.Blend = f32()
		s.ExcitementFrom = f32()
		s.ExcitementTo = f32()
		s.Rate = f32()
		s.Epoch = f.Now + f32()
		s.Jitter = f32()
		s.ExcitementStart = f.Now + f32()
		s.ExcitementRamp = f32()
		s.Synced = data[at]&crowdFlagSynced != 0
		s.Props = data[at]&crowdFlagProps != 0
		at += 2
		f.Sections = append(f.Sections, s)
	}
	if err := need(2); err != nil {
		return f, err
	}
	waves := int(data[at])
	f.ReplaceWaves = data[at+1] == 1
	at += 2
	if waves > crowdMaxWaves {
		return f, errors.New("scene: crowd frame has more than four waves")
	}
	for i := 0; i < waves; i++ {
		if err := need(crowdWaveRecordBytes); err != nil {
			return f, err
		}
		w := CrowdWaveFrame{Start: f.Now + f32()}
		w.Origin = f32()
		w.Speed = f32()
		w.Width = f32()
		w.Laps = f32()
		f.Waves = append(f.Waves, w)
	}
	if at != len(data) {
		return f, errors.New("scene: crowd frame has trailing bytes")
	}
	return f, nil
}

func finite(v float64) bool { return !math.IsNaN(v) && !math.IsInf(v, 0) }
