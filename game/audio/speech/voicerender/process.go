package voicerender

import (
	"errors"

	"m31labs.dev/gosx/game/audio/speech/loudness"
)

// ProcessOptions are the per-clip cleanup settings.
type ProcessOptions struct {
	Rate        int
	TrimDB      float64
	PadMs       float64
	TargetLUFS  float64
	CeilingDBTP float64
}

// ProcessClip is the one cleanup path every clip takes, whether a provider
// spoke it or a person recorded it: resample, remove DC, repair isolated
// pops, trim silence, normalize loudness and hold the true peak, then fade
// the edges so the cut never clicks. The report is measured on the result.
func ProcessClip(a loudness.Audio, o ProcessOptions) (loudness.Audio, loudness.Report, error) {
	a = loudness.Resample(a, o.Rate)
	a = loudness.RemoveDC(a)
	a, _ = loudness.DeClick(a)
	a = loudness.Trim(a, o.TrimDB, o.PadMs)
	if len(a.Samples) == 0 {
		return a, loudness.Report{}, errors.New("the clip is silent after trimming")
	}
	a, _ = loudness.Normalize(a, o.TargetLUFS, o.CeilingDBTP)
	a = loudness.Fade(a, 1.5)
	return a, loudness.Measure(a), nil
}
